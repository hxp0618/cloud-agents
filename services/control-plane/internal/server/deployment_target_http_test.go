package server

import (
	"context"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	openapiv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/openapi/v1alpha1"
	platformv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/platform/v1alpha1"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authn"
	internaldeploymenttarget "github.com/hxp0618/cloud-agents/services/control-plane/internal/deploymenttarget"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/kubernetestarget"
	internalmanagedhost "github.com/hxp0618/cloud-agents/services/control-plane/internal/managedhost"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/store/postgres"
)

type deploymentTargetStoreFake struct {
	snapshot   internaldeploymenttarget.Snapshot
	register   int
	list       int
	get        int
	begin      int
	complete   int
	completion internaldeploymenttarget.ProbeCompletion
	lease      internalmanagedhost.Snapshot
	leaseErr   error
}

type deploymentTargetVerifierFake struct {
	requests []authn.VerificationRequest
	failAt   int
}

func (fake *deploymentTargetVerifierFake) Verify(_ string, request authn.VerificationRequest) (*authn.VerifiedPrincipal, error) {
	fake.requests = append(fake.requests, request)
	if fake.failAt == len(fake.requests) {
		return nil, errors.New("verification failed")
	}
	return &authn.VerifiedPrincipal{}, nil
}

func TestDeploymentTargetHTTPProbesKubernetesTarget(t *testing.T) {
	cluster := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/version" || request.Header.Get("Authorization") != "Bearer service-account-token" {
			t.Fatalf("cluster request path=%q authorization=%q", request.URL.Path, request.Header.Get("Authorization"))
		}
		_, _ = writer.Write([]byte(`{"major":"1","minor":"34","gitVersion":"v1.34.2","platform":"linux/arm64"}`))
	}))
	defer cluster.Close()
	directory := t.TempDir()
	certificate := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cluster.Certificate().Raw})
	for name, value := range map[string][]byte{"cluster-alpha.ca.crt": certificate, "cluster-alpha.token": []byte("service-account-token\n")} {
		if err := os.WriteFile(filepath.Join(directory, name), value, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	credentials, err := kubernetestarget.NewCredentialDirectory(directory)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, time.September, 1, 10, 0, 0, 0, time.UTC)
	store := &deploymentTargetStoreFake{snapshot: internaldeploymenttarget.Snapshot{
		Scope:    internaldeploymenttarget.Scope{TenantID: "tenant-alpha", ProjectID: "project-alpha"},
		TargetID: "kubernetes-alpha", TargetName: "kubernetes-alpha", Kind: "kubernetes", Endpoint: cluster.URL, CredentialRef: "cluster-alpha",
		Generation: 1, ObservedPhase: "unprobed", ResourceVersion: 1, CreatedAt: now, UpdatedAt: now,
	}}
	verifier := &projectHTTPVerifierFake{}
	handler, err := NewDeploymentTargetHTTPServer(verifier, store, nil, credentials, nil)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/v1/tenants/tenant-alpha/projects/project-alpha/deployment-targets/kubernetes-alpha:probe", strings.NewReader(`{"expectedGeneration":1}`))
	request.Header.Set("Authorization", "Bearer access-token")
	request.Header.Set("X-Request-ID", "request-kubernetes-probe")
	request.Header.Set("Idempotency-Key", "kubernetes-probe-key")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !store.completion.Succeeded || store.completion.APIVersion != "1.34" || store.completion.EngineVersion != "v1.34.2" || store.completion.OS != "linux" || store.completion.Arch != "arm64" || !strings.Contains(response.Body.String(), `"targetKind":"kubernetes"`) || !strings.Contains(response.Body.String(), `"observedPhase":"ready"`) {
		t.Fatalf("status=%d completion=%#v body=%s", response.Code, store.completion, response.Body.String())
	}
}

func (fake *deploymentTargetStoreFake) RegisterDeploymentTarget(_ context.Context, _ string, _ *authn.VerifiedPrincipal, input internaldeploymenttarget.RegisterInput) (internaldeploymenttarget.Snapshot, error) {
	fake.register++
	fake.snapshot.Scope = input.Scope
	fake.snapshot.TargetID = input.TargetID
	fake.snapshot.TargetName = input.TargetName
	fake.snapshot.Kind = input.Kind
	fake.snapshot.Endpoint = input.Endpoint
	fake.snapshot.CredentialRef = input.CredentialRef
	return fake.snapshot, nil
}

func (fake *deploymentTargetStoreFake) GetDeploymentTarget(context.Context, string, *authn.VerifiedPrincipal, string, string) (internaldeploymenttarget.Snapshot, error) {
	fake.get++
	return fake.snapshot, nil
}

func (fake *deploymentTargetStoreFake) ListDeploymentTargets(context.Context, string, *authn.VerifiedPrincipal, string, string, int) (postgres.DeploymentTargetPage, error) {
	fake.list++
	return postgres.DeploymentTargetPage{DeploymentTargets: []internaldeploymenttarget.Snapshot{fake.snapshot}}, nil
}

func (fake *deploymentTargetStoreFake) GetManagedHostEnvironmentLease(context.Context, string, *authn.VerifiedPrincipal, string, string) (internalmanagedhost.Snapshot, error) {
	return fake.lease, fake.leaseErr
}

func (fake *deploymentTargetStoreFake) BeginDeploymentTargetProbe(_ context.Context, _ string, _ *authn.VerifiedPrincipal, _ internaldeploymenttarget.ProbeInput) (internaldeploymenttarget.ProbeStart, error) {
	fake.begin++
	fake.snapshot.ObservedPhase = "probing"
	fake.snapshot.ResourceVersion++
	return internaldeploymenttarget.ProbeStart{Target: fake.snapshot, Execute: true}, nil
}

func (fake *deploymentTargetStoreFake) CompleteDeploymentTargetProbe(_ context.Context, _ string, _ *authn.VerifiedPrincipal, completion internaldeploymenttarget.ProbeCompletion) (internaldeploymenttarget.Snapshot, error) {
	fake.complete++
	fake.completion = completion
	now := fake.snapshot.UpdatedAt.Add(time.Second)
	fake.snapshot.LastProbeAt = &now
	fake.snapshot.UpdatedAt = now
	fake.snapshot.ResourceVersion++
	if completion.Succeeded {
		fake.snapshot.ObservedPhase = "ready"
		fake.snapshot.APIVersion = completion.APIVersion
		fake.snapshot.EngineVersion = completion.EngineVersion
		fake.snapshot.OS = completion.OS
		fake.snapshot.Arch = completion.Arch
	} else {
		fake.snapshot.ObservedPhase = "unavailable"
		fake.snapshot.StableErrorCode = completion.StableErrorCode
	}
	return fake.snapshot, nil
}

func TestDeploymentTargetHTTPRegisterGetAndSettledProbe(t *testing.T) {
	now := time.Date(2026, time.September, 1, 10, 0, 0, 0, time.UTC)
	verifier := &projectHTTPVerifierFake{}
	store := &deploymentTargetStoreFake{snapshot: internaldeploymenttarget.Snapshot{Generation: 1, ObservedPhase: "unprobed", ResourceVersion: 1, CreatedAt: now, UpdatedAt: now}}
	handler, err := NewDeploymentTargetHTTPServer(verifier, store, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	request := func(method, path, body, requestID, idempotencyKey string) *httptest.ResponseRecorder {
		value := httptest.NewRequest(method, path, strings.NewReader(body))
		value.Header.Set("Authorization", "Bearer access-token")
		value.Header.Set("X-Request-ID", requestID)
		if body != "" {
			value.Header.Set("Content-Type", "application/json")
		}
		if idempotencyKey != "" {
			value.Header.Set("Idempotency-Key", idempotencyKey)
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, value)
		return response
	}
	created := request(http.MethodPost, "/v1/tenants/tenant-alpha/projects/project-alpha/deployment-targets", `{"targetId":"ssh-alpha","targetName":"ssh-alpha","targetKind":"ssh","endpoint":"ssh://ssh.example.test:22","credentialRef":"ssh-alpha"}`, "request-register", "register-key-123456")
	if created.Code != http.StatusCreated || store.register != 1 || verifier.seen.RequiredPermission != "projects.act" {
		t.Fatalf("register status=%d calls=%d verification=%#v body=%s", created.Code, store.register, verifier.seen, created.Body.String())
	}
	if _, err := openapiv1alpha1.DecodeDeploymentTargetResponseJSON(created.Body.Bytes()); err != nil {
		t.Fatalf("register response contract: %v", err)
	}
	listed := request(http.MethodGet, "/v1/tenants/tenant-alpha/projects/project-alpha/deployment-targets?pageSize=50", "", "request-list", "")
	page, pageErr := platformv1alpha1.DecodeDeploymentTargetPageResponseJSON(listed.Body.Bytes())
	if listed.Code != http.StatusOK || store.list != 1 || verifier.seen.RequiredPermission != "projects.get" || pageErr != nil || len(page.Value.DeploymentTargets) != 1 || page.Value.DeploymentTargets[0].Metadata.UID != "ssh-alpha" {
		t.Fatalf("list status=%d calls=%d verification=%#v page=%#v error=%v body=%s", listed.Code, store.list, verifier.seen, page, pageErr, listed.Body.String())
	}
	got := request(http.MethodGet, "/v1/tenants/tenant-alpha/projects/project-alpha/deployment-targets/ssh-alpha", "", "request-get", "")
	if got.Code != http.StatusOK || store.get != 1 || verifier.seen.RequiredPermission != "projects.get" {
		t.Fatalf("get status=%d calls=%d verification=%#v body=%s", got.Code, store.get, verifier.seen, got.Body.String())
	}
	probed := request(http.MethodPost, "/v1/tenants/tenant-alpha/projects/project-alpha/deployment-targets/ssh-alpha:probe", `{"expectedGeneration":1}`, "request-probe", "probe-key-12345678")
	if probed.Code != http.StatusOK || store.begin != 1 || store.complete != 1 || verifier.calls != 5 || store.completion.Succeeded || store.completion.StableErrorCode != "ssh-probe-unconfigured" || !strings.Contains(probed.Body.String(), `"observedPhase":"unavailable"`) {
		t.Fatalf("probe status=%d begin=%d complete=%d verifier=%d completion=%#v body=%s", probed.Code, store.begin, store.complete, verifier.calls, store.completion, probed.Body.String())
	}
	if strings.Contains(probed.Body.String(), "PRIVATE KEY") {
		t.Fatal("probe response leaked credential bytes")
	}
}

func TestDeploymentTargetPageTokenBindsTenantAndProject(t *testing.T) {
	token, ok := encodeDeploymentTargetPageToken("tenant-alpha", "project-alpha", "target-alpha")
	if !ok {
		t.Fatal("page token was not encoded")
	}
	if targetID, ok := decodeDeploymentTargetPageToken("tenant-alpha", "project-alpha", token); !ok || targetID != "target-alpha" {
		t.Fatalf("decoded target = %q / %v", targetID, ok)
	}
	if _, ok := decodeDeploymentTargetPageToken("tenant-alpha", "project-other", token); ok {
		t.Fatal("cross-project token was accepted")
	}
}

func TestDeploymentTargetPathDoesNotCaptureProjectRoutes(t *testing.T) {
	if HandlesDeploymentTargetPath("/v1/tenants/tenant-alpha/projects/project-alpha") || !HandlesDeploymentTargetPath("/v1/tenants/tenant-alpha/projects/project-alpha/deployment-targets/docker-alpha:probe") || !HandlesDeploymentTargetPath("/v1/tenants/tenant-alpha/projects/project-alpha/deployment-targets/kubernetes-alpha:cleanup") {
		t.Fatal("deployment target route ownership drifted")
	}
	if HandlesDeploymentTargetPath("/v1/admin/tenants/tenant-alpha/projects/project-alpha/deployment-targets") || !HandlesAdminDeploymentTargetPath("/v1/admin/tenants/tenant-alpha/projects/project-alpha/deployment-targets/ssh-alpha:probe") {
		t.Fatal("admin deployment target route ownership drifted")
	}
}

func TestAdminDeploymentTargetHTTPChecksProjectAndAdminAuthority(t *testing.T) {
	now := time.Date(2026, time.September, 3, 8, 0, 0, 0, time.UTC)
	verifier := &deploymentTargetVerifierFake{}
	store := &deploymentTargetStoreFake{snapshot: internaldeploymenttarget.Snapshot{
		Scope: internaldeploymenttarget.Scope{TenantID: "tenant-alpha", ProjectID: "project-alpha"}, TargetID: "docker-alpha", TargetName: "Docker Alpha", Kind: "docker",
		Endpoint: "unix:///var/run/docker.sock", CredentialRef: "docker-alpha", Generation: 1, ObservedPhase: "ready", ResourceVersion: 1, CreatedAt: now, UpdatedAt: now,
	}}
	handler, err := NewAdminDeploymentTargetHTTPServer(verifier, store, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/v1/admin/tenants/tenant-alpha/projects/project-alpha/deployment-targets?pageSize=50", nil)
	request.Header.Set("Authorization", "Bearer access-token")
	request.Header.Set("X-Request-ID", "request-admin-list")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || len(verifier.requests) != 3 || verifier.requests[0].RequiredPermission != "projects.get" || verifier.requests[1].RequiredPermission != "targets.list" || verifier.requests[2].RequiredPermission != "projects.get" {
		t.Fatalf("status=%d requests=%#v body=%s", response.Code, verifier.requests, response.Body.String())
	}
}

func TestDeploymentTargetAdminPermissions(t *testing.T) {
	tests := []struct{ action, method, permission string }{
		{"collection", http.MethodGet, "targets.list"},
		{"collection", http.MethodPost, "targets.create"},
		{"get", http.MethodGet, "targets.get"},
		{"probe", http.MethodPost, "targets.act"},
	}
	for _, test := range tests {
		permission, ok := deploymentTargetAdminPermission(test.action, test.method)
		if !ok || permission != test.permission {
			t.Fatalf("action=%q method=%q permission=%q ok=%t", test.action, test.method, permission, ok)
		}
	}
	if _, ok := deploymentTargetAdminPermission("cleanup", http.MethodPost); ok {
		t.Fatal("admin cleanup must remain closed until impact preview and operation audit exist")
	}
}

func TestAdminDeploymentTargetHTTPReturnsForbiddenWhenAdminScopeFails(t *testing.T) {
	verifier := &deploymentTargetVerifierFake{failAt: 2}
	handler, err := NewAdminDeploymentTargetHTTPServer(verifier, &deploymentTargetStoreFake{}, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/v1/admin/tenants/tenant-alpha/projects/project-alpha/deployment-targets?pageSize=50", nil)
	request.Header.Set("Authorization", "Bearer user-token")
	request.Header.Set("X-Request-ID", "request-user-admin-route")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), `"code":"AUTHORIZATION_DENIED"`) || len(verifier.requests) != 2 {
		t.Fatalf("status=%d requests=%#v body=%s", response.Code, verifier.requests, response.Body.String())
	}
}

func TestManagedDeploymentTargetWorkerActiveRequiresExactAuthority(t *testing.T) {
	worker := managedDeploymentTargetWorker{tenantID: "tenant-alpha", projectID: "project-alpha", targetID: "ssh-alpha", leaseID: "lease-alpha", targetGeneration: 2, leaseGeneration: 3}
	lease := internalmanagedhost.Snapshot{Scope: internalmanagedhost.Scope{TenantID: worker.tenantID, ProjectID: worker.projectID}, LeaseID: worker.leaseID, TargetID: worker.targetID, TargetGeneration: worker.targetGeneration, Generation: worker.leaseGeneration, DesiredPhase: "active"}
	if !managedDeploymentTargetWorkerActive(2, worker, lease) {
		t.Fatal("exact active lease was classified as orphaned")
	}
	lease.DesiredPhase = "terminated"
	if managedDeploymentTargetWorkerActive(2, worker, lease) {
		t.Fatal("terminated lease retained a managed worker")
	}
	lease.DesiredPhase = "active"
	if managedDeploymentTargetWorkerActive(3, worker, lease) {
		t.Fatal("stale target generation retained a managed worker")
	}
}
