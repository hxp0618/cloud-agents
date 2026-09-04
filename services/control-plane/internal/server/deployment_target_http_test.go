package server

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
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
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/dockertarget"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/kubernetestarget"
	internalmanagedhost "github.com/hxp0618/cloud-agents/services/control-plane/internal/managedhost"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/store/postgres"
)

type deploymentTargetStoreFake struct {
	snapshot           internaldeploymenttarget.Snapshot
	register           int
	list               int
	get                int
	begin              int
	complete           int
	completion         internaldeploymenttarget.ProbeCompletion
	cleanupBegin       int
	cleanupComplete    int
	cleanupExecute     bool
	cleanupInput       internaldeploymenttarget.CleanupInput
	cleanupCompletion  internaldeploymenttarget.CleanupCompletion
	cleanupOperation   internaldeploymenttarget.Operation
	cleanupBeginErr    error
	cleanupCompleteErr error
	operations         []internaldeploymenttarget.Operation
	audit              []internaldeploymenttarget.AuditEvent
	lease              internalmanagedhost.Snapshot
	leaseErr           error
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

func (fake *deploymentTargetStoreFake) ListDeploymentTargetOperations(context.Context, string, *authn.VerifiedPrincipal, string, string, *time.Time, string, int) (postgres.DeploymentTargetOperationPage, error) {
	return postgres.DeploymentTargetOperationPage{Operations: fake.operations}, nil
}

func (fake *deploymentTargetStoreFake) ListDeploymentTargetAuditEvents(context.Context, string, *authn.VerifiedPrincipal, string, string, *time.Time, string, int) (postgres.DeploymentTargetAuditPage, error) {
	return postgres.DeploymentTargetAuditPage{Events: fake.audit}, nil
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

func (fake *deploymentTargetStoreFake) BeginDeploymentTargetCleanup(_ context.Context, _ string, _ *authn.VerifiedPrincipal, input internaldeploymenttarget.CleanupInput) (internaldeploymenttarget.CleanupStart, error) {
	fake.cleanupBegin++
	fake.cleanupInput = input
	if fake.cleanupBeginErr != nil {
		return internaldeploymenttarget.CleanupStart{}, fake.cleanupBeginErr
	}
	operation := fake.cleanupOperation
	if operation.OperationID == "" {
		now := fake.snapshot.UpdatedAt
		operation = internaldeploymenttarget.Operation{
			Scope: input.Scope, OperationID: "cleanup-operation-alpha", IdempotencyKey: input.Mutation.IdempotencyKey,
			Action: "target.cleanup", TargetID: input.TargetID, TargetGeneration: input.ExpectedGeneration,
			RequestedBy: "sha256:" + strings.Repeat("a", 64), RequestID: input.Mutation.RequestID,
			RequestedAt: now, UpdatedAt: now, State: "running", CurrentStep: "cleanup",
			ImpactSummary: "Clean deployment target resources confirmed by preview",
		}
	}
	return internaldeploymenttarget.CleanupStart{Operation: operation, Execute: fake.cleanupExecute}, nil
}

func (fake *deploymentTargetStoreFake) CompleteDeploymentTargetCleanup(_ context.Context, _ string, _ *authn.VerifiedPrincipal, completion internaldeploymenttarget.CleanupCompletion) (internaldeploymenttarget.Operation, error) {
	fake.cleanupComplete++
	fake.cleanupCompletion = completion
	if fake.cleanupCompleteErr != nil {
		return internaldeploymenttarget.Operation{}, fake.cleanupCompleteErr
	}
	now := fake.snapshot.UpdatedAt
	state, step, retryable := "succeeded", "complete", false
	if !completion.Succeeded {
		state, step, retryable = "failed", "failed", true
	}
	operation := internaldeploymenttarget.Operation{
		Scope: completion.Input.Scope, OperationID: "cleanup-operation-alpha", IdempotencyKey: completion.Input.Mutation.IdempotencyKey,
		Action: "target.cleanup", TargetID: completion.Input.TargetID, TargetGeneration: completion.Input.ExpectedGeneration,
		RequestedBy: "sha256:" + strings.Repeat("a", 64), RequestID: completion.Input.Mutation.RequestID,
		RequestedAt: now, UpdatedAt: now, State: state, CurrentStep: step, StableErrorCode: completion.StableErrorCode,
		ImpactSummary: completion.ImpactSummary, Retryable: retryable,
	}
	fake.cleanupOperation = operation
	return operation, nil
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
	if probed.Code != http.StatusOK || store.begin != 1 || store.complete != 1 || verifier.calls != 13 || store.completion.Succeeded || store.completion.StableErrorCode != "ssh-probe-unconfigured" || !strings.Contains(probed.Body.String(), `"observedPhase":"unavailable"`) {
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
	if HandlesDeploymentTargetPath("/v1/admin/tenants/tenant-alpha/projects/project-alpha/deployment-targets") || HandlesDeploymentTargetPath("/v1/tenants/tenant-alpha/projects/project-alpha/deployment-targets/docker-alpha:cleanup-preview") || !HandlesAdminDeploymentTargetPath("/v1/admin/tenants/tenant-alpha/projects/project-alpha/deployment-targets/ssh-alpha:probe") || !HandlesAdminDeploymentTargetPath("/v1/admin/tenants/tenant-alpha/projects/project-alpha/deployment-targets/docker-alpha:cleanup-preview") || !HandlesAdminDeploymentTargetPath("/v1/admin/tenants/tenant-alpha/projects/project-alpha/deployment-targets/docker-alpha:cleanup") || !HandlesAdminDeploymentTargetPath("/v1/admin/tenants/tenant-alpha/projects/project-alpha/deployment-targets/docker-alpha/operations") || !HandlesAdminDeploymentTargetPath("/v1/admin/tenants/tenant-alpha/projects/project-alpha/deployment-targets/docker-alpha/audit-events") {
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

func TestAdminDeploymentTargetHTTPListsDurableOperationAndAudit(t *testing.T) {
	now := time.Date(2026, time.September, 3, 9, 0, 0, 0, time.UTC)
	digest := "sha256:" + strings.Repeat("a", 64)
	store := &deploymentTargetStoreFake{
		operations: []internaldeploymenttarget.Operation{{
			Scope: internaldeploymenttarget.Scope{TenantID: "tenant-alpha", ProjectID: "project-alpha"}, OperationID: "probe-op-alpha",
			IdempotencyKey: "probe-key-12345678", Action: "target.probe", TargetID: "docker-alpha", TargetGeneration: 1,
			RequestedBy: digest, RequestID: "request-probe", RequestedAt: now, UpdatedAt: now, State: "succeeded",
			CurrentStep: "probe-complete", ImpactSummary: "Probed deployment target docker-alpha",
		}},
		audit: []internaldeploymenttarget.AuditEvent{{
			Scope: internaldeploymenttarget.Scope{TenantID: "tenant-alpha", ProjectID: "project-alpha"}, EventID: "probe-event-alpha",
			Actor: digest, Action: "target.probe", TargetID: "docker-alpha", TargetGeneration: 1, Result: "succeeded",
			OccurredAt: now, RequestID: "request-probe", OperationID: "probe-op-alpha",
		}},
	}
	verifier := &deploymentTargetVerifierFake{}
	handler, err := NewAdminDeploymentTargetHTTPServer(verifier, store, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	request := func(path, requestID string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Authorization", "Bearer admin-token")
		req.Header.Set("X-Request-ID", requestID)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		return response
	}
	operations := request("/v1/admin/tenants/tenant-alpha/projects/project-alpha/deployment-targets/docker-alpha/operations?pageSize=50", "request-operations")
	operationPage, decodeErr := platformv1alpha1.DecodeMaintenanceOperationPageResponseJSON(operations.Body.Bytes())
	if operations.Code != http.StatusOK || decodeErr != nil || len(operationPage.Value.Operations) != 1 || operationPage.Value.Operations[0].OperationID != "probe-op-alpha" || verifier.requests[1].RequiredPermission != "operations.list" {
		t.Fatalf("status=%d page=%#v error=%v requests=%#v body=%s", operations.Code, operationPage, decodeErr, verifier.requests, operations.Body.String())
	}
	audit := request("/v1/admin/tenants/tenant-alpha/projects/project-alpha/deployment-targets/docker-alpha/audit-events?pageSize=50", "request-audit")
	auditPage, decodeErr := platformv1alpha1.DecodeAdminAuditEventPageResponseJSON(audit.Body.Bytes())
	if audit.Code != http.StatusOK || decodeErr != nil || len(auditPage.Value.Events) != 1 || auditPage.Value.Events[0].Actor != digest || verifier.requests[4].RequiredPermission != "audit.list" {
		t.Fatalf("status=%d page=%#v error=%v requests=%#v body=%s", audit.Code, auditPage, decodeErr, verifier.requests, audit.Body.String())
	}
	for _, body := range []string{operations.Body.String(), audit.Body.String()} {
		if strings.Contains(body, "credentialRef") || strings.Contains(body, "endpoint") {
			t.Fatalf("admin activity leaked target authority: %s", body)
		}
	}
	token, ok := encodeDeploymentTargetActivityPageToken("operation", "tenant-alpha", "project-alpha", "docker-alpha", now, "probe-op-alpha")
	decodedAt, decodedID, decoded := decodeDeploymentTargetActivityPageToken("operation", "tenant-alpha", "project-alpha", "docker-alpha", token)
	if !ok || !decoded || decodedAt == nil || !decodedAt.Equal(now) || decodedID != "probe-op-alpha" {
		t.Fatalf("token round trip failed: ok=%t decoded=%t at=%v id=%q", ok, decoded, decodedAt, decodedID)
	}
	if _, _, decoded = decodeDeploymentTargetActivityPageToken("operation", "tenant-alpha", "project-alpha", "other-target", token); decoded {
		t.Fatal("cross-target activity cursor was accepted")
	}
}

func TestDeploymentTargetAdminPermissions(t *testing.T) {
	tests := []struct{ action, method, permission string }{
		{"collection", http.MethodGet, "targets.list"},
		{"collection", http.MethodPost, "targets.create"},
		{"get", http.MethodGet, "targets.get"},
		{"cleanup-preview", http.MethodGet, "targets.get"},
		{"operations", http.MethodGet, "operations.list"},
		{"audit-events", http.MethodGet, "audit.list"},
		{"probe", http.MethodPost, "targets.act"},
		{"cleanup", http.MethodPost, "targets.act"},
	}
	for _, test := range tests {
		permission, ok := deploymentTargetAdminPermission(test.action, test.method)
		if !ok || permission != test.permission {
			t.Fatalf("action=%q method=%q permission=%q ok=%t", test.action, test.method, permission, ok)
		}
	}
	if _, ok := deploymentTargetAdminPermission("cleanup", http.MethodGet); ok {
		t.Fatal("admin cleanup accepted the wrong method")
	}
}

func TestAdminDeploymentTargetCleanupPreviewUsesLiveDockerAuthorityAndBlocksActiveLease(t *testing.T) {
	deployRequest := dockertarget.DeployRequest{
		TenantID: "tenant-alpha", ProjectID: "project-alpha", TargetID: "docker-alpha", LeaseID: "lease-alpha",
		TargetGeneration: 1, LeaseGeneration: 2, ReleaseDigest: "sha256:" + strings.Repeat("a", 64), ProviderCredentialRef: "provider-alpha",
		CPULimitMillis: 1000, MemoryLimitBytes: 512 << 20,
	}
	config := dockertarget.DeploymentConfig{WorkerImageRepository: "registry.example.test/cloud-agents/worker", WorkerCredentialRef: "worker-alpha", WorkerSPIFFEID: "spiffe://cloud-agents.test/workers/docker-alpha", WorkerServerName: "worker.example.test"}
	containerName := dockertarget.WorkerContainerName(deployRequest)
	workspaceName := strings.Repeat("b", 64)
	requests, containerDeletes, volumeDeletes := 0, 0, 0
	containerPresent, volumePresent := true, true
	docker := httptest.NewUnstartedServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests++
		switch {
		case request.Method == http.MethodGet && request.URL.Path == "/containers/json":
			if containerPresent && !strings.Contains(request.URL.Query().Get("filters"), `"volume"`) {
				_, _ = writer.Write([]byte(`[{"Id":"container-alpha"}]`))
			} else {
				_, _ = writer.Write([]byte(`[]`))
			}
		case request.Method == http.MethodGet && request.URL.Path == "/containers/container-alpha/json" && containerPresent:
			_ = json.NewEncoder(writer).Encode(map[string]any{
				"Name":   "/" + containerName,
				"Config": map[string]any{"Image": config.WorkerImageRepository + "@" + deployRequest.ReleaseDigest, "Labels": dockertarget.DeploymentLabels(deployRequest, config)},
				"Mounts": []map[string]any{{"Type": "volume", "Name": workspaceName, "Destination": "/workspace"}},
			})
		case request.Method == http.MethodDelete && request.URL.Path == "/containers/container-alpha":
			containerPresent = false
			containerDeletes++
			writer.WriteHeader(http.StatusNoContent)
		case request.Method == http.MethodGet && request.URL.Path == "/volumes/"+workspaceName && volumePresent:
			_ = json.NewEncoder(writer).Encode(map[string]any{"Name": workspaceName, "Labels": map[string]string{}})
		case request.Method == http.MethodDelete && request.URL.Path == "/volumes/"+workspaceName:
			volumePresent = false
			volumeDeletes++
			writer.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(writer, request)
		}
	}))
	docker.TLS = &tls.Config{ClientAuth: tls.RequireAnyClientCert}
	docker.StartTLS()
	t.Cleanup(docker.Close)

	credentialRoot := t.TempDir()
	credentialPath := filepath.Join(credentialRoot, "credential-alpha")
	if err := os.Mkdir(credentialPath, 0o700); err != nil {
		t.Fatal(err)
	}
	certificate := docker.TLS.Certificates[0]
	privateKey, err := x509.MarshalPKCS8PrivateKey(certificate.PrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificate.Certificate[0]})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateKey})
	for name, value := range map[string][]byte{"ca.pem": certPEM, "cert.pem": certPEM, "key.pem": keyPEM} {
		if err := os.WriteFile(filepath.Join(credentialPath, name), value, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	credentials, err := dockertarget.NewCredentialDirectory(credentialRoot)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, time.September, 3, 8, 0, 0, 0, time.UTC)
	store := &deploymentTargetStoreFake{
		snapshot: internaldeploymenttarget.Snapshot{Scope: internaldeploymenttarget.Scope{TenantID: deployRequest.TenantID, ProjectID: deployRequest.ProjectID}, TargetID: deployRequest.TargetID, TargetName: deployRequest.TargetID, Kind: "docker", Endpoint: docker.URL, CredentialRef: "credential-alpha", Generation: 1, ObservedPhase: "ready", ResourceVersion: 7, CreatedAt: now, UpdatedAt: now},
		lease:    internalmanagedhost.Snapshot{Scope: internalmanagedhost.Scope{TenantID: deployRequest.TenantID, ProjectID: deployRequest.ProjectID}, LeaseID: deployRequest.LeaseID, TargetID: deployRequest.TargetID, TargetGeneration: deployRequest.TargetGeneration, Generation: deployRequest.LeaseGeneration, DesiredPhase: "active"},
	}
	verifier := &deploymentTargetVerifierFake{}
	handler, err := NewAdminDeploymentTargetHTTPServer(verifier, store, credentials, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	call := func() *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodGet, "/v1/admin/tenants/tenant-alpha/projects/project-alpha/deployment-targets/docker-alpha:cleanup-preview", nil)
		request.Header.Set("Authorization", "Bearer admin-token")
		request.Header.Set("X-Request-ID", "request-cleanup-preview")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response
	}
	cleanup := func(key, impactDigest string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPost, "/v1/admin/tenants/tenant-alpha/projects/project-alpha/deployment-targets/docker-alpha:cleanup", strings.NewReader(`{"expectedGeneration":1,"expectedResourceVersion":"7","impactDigest":"`+impactDigest+`"}`))
		request.Header.Set("Authorization", "Bearer admin-token")
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("X-Request-ID", "request-"+key)
		request.Header.Set("Idempotency-Key", key)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response
	}
	response := call()
	preview, decodeErr := platformv1alpha1.DecodeDeploymentTargetCleanupPreviewResponseJSON(response.Body.Bytes())
	if response.Code != http.StatusOK || decodeErr != nil || preview.Value.Spec.CanCleanup || !strings.HasPrefix(preview.Value.Spec.ImpactDigest, "sha256:") || len(preview.Value.Spec.Workers) != 1 || preview.Value.Spec.Workers[0].Disposition != "blocked" || preview.Value.Spec.Workers[0].Resources[1].ResourceName != workspaceName || response.Header().Get("X-Resource-Version") != "7" || response.Header().Get("ETag") != `"7"` {
		t.Fatalf("status=%d preview=%#v error=%v headers=%#v body=%s", response.Code, preview, decodeErr, response.Header(), response.Body.String())
	}
	if len(verifier.requests) != 4 || verifier.requests[3].RequiredPermission != "projects.get" {
		t.Fatalf("cleanup preview did not obtain distinct target and Lease read authorities: %#v", verifier.requests)
	}
	store.cleanupExecute = true
	blocked := cleanup("cleanup-active-key", preview.Value.Spec.ImpactDigest)
	if blocked.Code != http.StatusConflict || !strings.Contains(blocked.Body.String(), `"code":"CLEANUP_ACTIVE_LEASE_CONFLICT"`) || store.cleanupComplete != 1 || store.cleanupCompletion.Succeeded || store.cleanupCompletion.StableErrorCode != "target-cleanup-active-lease" || containerDeletes != 0 {
		t.Fatalf("blocked status=%d completion=%#v deletes=%d body=%s", blocked.Code, store.cleanupCompletion, containerDeletes, blocked.Body.String())
	}
	store.cleanupOperation = internaldeploymenttarget.Operation{}
	store.leaseErr = postgres.ErrManagedHostEnvironmentLeaseNotFound
	response = call()
	preview, decodeErr = platformv1alpha1.DecodeDeploymentTargetCleanupPreviewResponseJSON(response.Body.Bytes())
	if response.Code != http.StatusOK || decodeErr != nil || !preview.Value.Spec.CanCleanup || preview.Value.Spec.Workers[0].Disposition != "cleanup" || requests != 6 {
		t.Fatalf("status=%d preview=%#v error=%v docker requests=%d", response.Code, preview, decodeErr, requests)
	}
	confirmedDigest := preview.Value.Spec.ImpactDigest
	store.cleanupExecute = true
	operationResponse := cleanup("cleanup-success-key", confirmedDigest)
	operation, operationErr := platformv1alpha1.DecodeMaintenanceOperationResponseJSON(operationResponse.Body.Bytes())
	if operationResponse.Code != http.StatusOK || operationErr != nil || operation.Value.State != "succeeded" || operation.Value.Action != "target.cleanup" || store.cleanupComplete != 2 || !store.cleanupCompletion.Succeeded || store.cleanupInput.ExpectedResourceVersion != 7 || store.cleanupInput.ImpactDigest != confirmedDigest || containerPresent || volumePresent || containerDeletes != 1 || volumeDeletes != 1 {
		t.Fatalf("status=%d operation=%#v error=%v input=%#v completion=%#v resources=%t/%t deletes=%d/%d body=%s", operationResponse.Code, operation, operationErr, store.cleanupInput, store.cleanupCompletion, containerPresent, volumePresent, containerDeletes, volumeDeletes, operationResponse.Body.String())
	}
	if verifier.requests[len(verifier.requests)-1].RequiredPermission != "projects.act" {
		t.Fatalf("cleanup completion reused an earlier authority: %#v", verifier.requests)
	}
	requestsBeforeReplay := requests
	store.cleanupExecute = false
	replay := cleanup("cleanup-success-key", confirmedDigest)
	if replay.Code != http.StatusOK || requests != requestsBeforeReplay || store.cleanupComplete != 2 {
		t.Fatalf("replay status=%d requests=%d/%d completions=%d body=%s", replay.Code, requests, requestsBeforeReplay, store.cleanupComplete, replay.Body.String())
	}
	store.cleanupOperation = internaldeploymenttarget.Operation{}
	store.cleanupExecute = true
	stale := cleanup("cleanup-stale-key", confirmedDigest)
	if stale.Code != http.StatusConflict || !strings.Contains(stale.Body.String(), `"code":"CLEANUP_IMPACT_CONFLICT"`) || store.cleanupComplete != 3 || store.cleanupCompletion.StableErrorCode != "target-cleanup-impact-conflict" {
		t.Fatalf("stale status=%d completion=%#v body=%s", stale.Code, store.cleanupCompletion, stale.Body.String())
	}
	for _, body := range []string{response.Body.String(), operationResponse.Body.String(), replay.Body.String(), stale.Body.String()} {
		for _, forbidden := range []string{docker.URL, "credential-alpha", "provider-alpha", "worker-alpha", "PRIVATE KEY"} {
			if strings.Contains(body, forbidden) {
				t.Fatalf("cleanup response leaked %q: %s", forbidden, body)
			}
		}
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

func TestUserDeploymentTargetRouteRequiresTargetScope(t *testing.T) {
	verifier := &deploymentTargetVerifierFake{failAt: 2}
	handler, err := NewDeploymentTargetHTTPServer(verifier, &deploymentTargetStoreFake{}, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/v1/tenants/tenant-alpha/projects/project-alpha/deployment-targets?pageSize=50", nil)
	request.Header.Set("Authorization", "Bearer user-token")
	request.Header.Set("X-Request-ID", "request-user-target-route")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden || len(verifier.requests) != 2 || verifier.requests[1].RequiredPermission != "targets.list" {
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
