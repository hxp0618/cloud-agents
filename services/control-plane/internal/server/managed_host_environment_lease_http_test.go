package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	platformv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/platform/v1alpha1"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authn"
	internaldeploymenttarget "github.com/hxp0618/cloud-agents/services/control-plane/internal/deploymenttarget"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/dockertarget"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/kubernetestarget"
	internalmanagedhost "github.com/hxp0618/cloud-agents/services/control-plane/internal/managedhost"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/sshtarget"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/store/postgres"
)

type managedHostEnvironmentLeaseStoreFake struct {
	snapshot                                internalmanagedhost.Snapshot
	target                                  internaldeploymenttarget.Snapshot
	create                                  int
	get                                     int
	list                                    int
	workers                                 int
	after                                   string
	limit                                   int
	terminate                               int
	finalize                                int
	upgrade                                 int
	adminPreview                            internalmanagedhost.AdminEnvironmentLeaseUpgradePreview
	adminStart                              postgres.AdminEnvironmentLeaseUpgradeStart
	adminResult                             postgres.AdminEnvironmentLeaseUpgradeResult
	adminInput                              internalmanagedhost.AdminEnvironmentLeaseUpgradeInput
	adminCompletion                         internalmanagedhost.CompleteEnvironmentLeaseDeploymentInput
	previewAction, previewRelease           string
	previewCalls, adminBegin, adminComplete int
}

func (fake *managedHostEnvironmentLeaseStoreFake) CreateManagedHostEnvironmentLease(_ context.Context, _ string, _ *authn.VerifiedPrincipal, input internalmanagedhost.CreateEnvironmentLeaseInput) (internalmanagedhost.Snapshot, error) {
	fake.create++
	fake.snapshot.Scope = input.Scope
	fake.snapshot.LeaseID = input.LeaseID
	fake.snapshot.LeaseName = input.LeaseName
	fake.snapshot.ReleaseDigest = input.ReleaseDigest
	fake.snapshot.TargetID = input.TargetID
	fake.snapshot.TargetGeneration = input.ExpectedTargetGeneration
	fake.snapshot.ProviderCredentialRef = input.ProviderCredentialRef
	fake.snapshot.CPULimitMillis = input.CPULimitMillis
	fake.snapshot.MemoryLimitBytes = input.MemoryLimitBytes
	return fake.snapshot, nil
}

func (fake *managedHostEnvironmentLeaseStoreFake) CompleteManagedHostEnvironmentLeaseDeployment(_ context.Context, _ string, _ *authn.VerifiedPrincipal, input internalmanagedhost.CompleteEnvironmentLeaseDeploymentInput) (internalmanagedhost.Snapshot, error) {
	fake.snapshot.ObservedPhase = "failed"
	fake.snapshot.StableErrorCode = input.StableErrorCode
	if input.Succeeded {
		fake.snapshot.ObservedPhase = "ready"
		fake.snapshot.StableErrorCode = ""
		fake.snapshot.WorkerEndpoint = input.WorkerEndpoint
		fake.snapshot.WorkerSPIFFEID = input.WorkerSPIFFEID
	}
	return fake.snapshot, nil
}

func (fake *managedHostEnvironmentLeaseStoreFake) BeginManagedHostEnvironmentLeaseUpgrade(_ context.Context, _ string, _ *authn.VerifiedPrincipal, input internalmanagedhost.UpgradeEnvironmentLeaseInput) (internalmanagedhost.UpgradeStart, error) {
	fake.upgrade++
	fake.snapshot.Scope = input.Scope
	fake.snapshot.LeaseID = input.LeaseID
	fake.snapshot.ReleaseDigest = input.ReleaseDigest
	fake.snapshot.Generation = input.ExpectedGeneration + 1
	fake.snapshot.ObservedPhase = "provisioning"
	fake.snapshot.CleanupPhase = "none"
	fake.snapshot.DesiredPhase = "active"
	return internalmanagedhost.UpgradeStart{Snapshot: fake.snapshot, Execute: true}, nil
}

func (fake *managedHostEnvironmentLeaseStoreFake) GetDeploymentTarget(context.Context, string, *authn.VerifiedPrincipal, string, string) (internaldeploymenttarget.Snapshot, error) {
	return fake.target, nil
}

func (fake *managedHostEnvironmentLeaseStoreFake) GetManagedHostEnvironmentLease(context.Context, string, *authn.VerifiedPrincipal, string, string) (internalmanagedhost.Snapshot, error) {
	fake.get++
	return fake.snapshot, nil
}

func (fake *managedHostEnvironmentLeaseStoreFake) ListManagedHostEnvironmentLeases(_ context.Context, _ string, _ *authn.VerifiedPrincipal, _ string, afterLeaseID string, limit int) (postgres.ManagedHostEnvironmentLeasePage, error) {
	fake.list++
	fake.after = afterLeaseID
	fake.limit = limit
	return postgres.ManagedHostEnvironmentLeasePage{EnvironmentLeases: []internalmanagedhost.Snapshot{fake.snapshot}, NextLeaseID: fake.snapshot.LeaseID}, nil
}

func (fake *managedHostEnvironmentLeaseStoreFake) ListAdminWorkers(_ context.Context, _ string, _ *authn.VerifiedPrincipal, _ string, afterWorkerID string, limit int) (postgres.AdminWorkerPage, error) {
	fake.workers++
	fake.after = afterWorkerID
	fake.limit = limit
	return postgres.AdminWorkerPage{Workers: []postgres.AdminWorkerSnapshot{{
		Scope: fake.snapshot.Scope, WorkerID: fake.snapshot.LeaseID, WorkerName: fake.snapshot.LeaseName,
		LeaseID: fake.snapshot.LeaseID, TargetID: "docker-alpha", TargetKind: "docker", TargetGeneration: 1,
		Generation: fake.snapshot.Generation, ReleaseDigest: fake.snapshot.ReleaseDigest, State: "ready", CleanupPhase: "none",
		CPULimitMillis: 1000, MemoryLimitBytes: 536870912, WorkerSPIFFEID: "spiffe://cloud-agents.test/worker/lease-alpha",
		WorkerServerName: "worker-alpha", LastHealthAt: &fake.snapshot.UpdatedAt, ReadyAt: &fake.snapshot.UpdatedAt,
		ResourceVersion: fake.snapshot.ResourceVersion, CreatedAt: fake.snapshot.CreatedAt, UpdatedAt: fake.snapshot.UpdatedAt,
	}}, NextWorkerID: fake.snapshot.LeaseID}, nil
}

func (fake *managedHostEnvironmentLeaseStoreFake) PreviewAdminEnvironmentLeaseUpgrade(_ context.Context, _ string, _ *authn.VerifiedPrincipal, _, _, action, releaseDigest string) (internalmanagedhost.AdminEnvironmentLeaseUpgradePreview, error) {
	fake.previewCalls++
	fake.previewAction, fake.previewRelease = action, releaseDigest
	return fake.adminPreview, nil
}

func (fake *managedHostEnvironmentLeaseStoreFake) BeginAdminEnvironmentLeaseUpgrade(_ context.Context, _ string, _ *authn.VerifiedPrincipal, input internalmanagedhost.AdminEnvironmentLeaseUpgradeInput) (postgres.AdminEnvironmentLeaseUpgradeStart, error) {
	fake.adminBegin++
	fake.adminInput = input
	return fake.adminStart, nil
}

func (fake *managedHostEnvironmentLeaseStoreFake) CompleteAdminEnvironmentLeaseUpgrade(_ context.Context, _ string, _ *authn.VerifiedPrincipal, input internalmanagedhost.CompleteAdminEnvironmentLeaseUpgradeInput) (postgres.AdminEnvironmentLeaseUpgradeResult, error) {
	fake.adminComplete++
	fake.adminInput = input.Upgrade
	fake.adminCompletion = input.Deployment
	return fake.adminResult, nil
}

func (fake *managedHostEnvironmentLeaseStoreFake) TerminateManagedHostEnvironmentLease(_ context.Context, _ string, _ *authn.VerifiedPrincipal, input internalmanagedhost.TerminateEnvironmentLeaseInput) (internalmanagedhost.Snapshot, error) {
	fake.terminate++
	fake.snapshot.Scope = input.Scope
	fake.snapshot.LeaseID = input.LeaseID
	fake.snapshot.DesiredPhase = "terminated"
	fake.snapshot.ObservedPhase = "terminating"
	fake.snapshot.CleanupPhase = "pending"
	fake.snapshot.StableErrorCode = ""
	fake.snapshot.Generation++
	return fake.snapshot, nil
}

func (fake *managedHostEnvironmentLeaseStoreFake) CompleteManagedHostEnvironmentLeaseTermination(_ context.Context, _ string, _ *authn.VerifiedPrincipal, input internalmanagedhost.CompleteEnvironmentLeaseTerminationInput) (internalmanagedhost.Snapshot, error) {
	fake.finalize++
	fake.snapshot.ObservedPhase = "terminated"
	fake.snapshot.CleanupPhase = "complete"
	fake.snapshot.WorkerEndpoint, fake.snapshot.WorkerSPIFFEID, fake.snapshot.WorkerServerName = "", "", ""
	return fake.snapshot, nil
}

func TestManagedHostEnvironmentLeaseHTTPServerLifecycleRoutes(t *testing.T) {
	now := time.Date(2026, time.August, 29, 8, 0, 0, 0, time.UTC)
	verifier := &projectHTTPVerifierFake{}
	store := &managedHostEnvironmentLeaseStoreFake{snapshot: internalmanagedhost.Snapshot{
		LeaseID: "lease-alpha", LeaseName: "default", EnvironmentID: "lease-alpha", ReleaseDigest: "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		Generation: 1, DesiredPhase: "active", ObservedPhase: "provisioning", CleanupPhase: "none", ResourceVersion: 1,
		ExpiresAt: now.Add(time.Hour), CreatedAt: now, UpdatedAt: now,
	}}
	handler, err := NewManagedHostEnvironmentLeaseHTTPServer(verifier, store, nil, nil, nil, dockertarget.WorkerTrust{})
	if err != nil {
		t.Fatal(err)
	}
	request := func(method, path, body, requestID, idempotencyKey string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(method, path, strings.NewReader(body))
		request.Header.Set("Authorization", "Bearer access-token")
		request.Header.Set("X-Request-ID", requestID)
		if idempotencyKey != "" {
			request.Header.Set("Idempotency-Key", idempotencyKey)
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response
	}
	created := request(http.MethodPost, "/v1/managed-host/tenants/tenant-alpha/projects/project-alpha/environment-leases", `{"leaseId":"lease-alpha","leaseName":"default","releaseDigest":"sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef","targetId":"docker-alpha","expectedTargetGeneration":1,"providerCredentialRef":"provider-alpha","cpuLimitMillis":1000,"memoryLimitBytes":536870912,"ttlSeconds":3600}`, "request-create", "create-key-123456")
	if created.Code != http.StatusCreated || store.create != 1 || verifier.seen.RequiredPermission != "projects.act" || !strings.Contains(created.Body.String(), `"observedPhase":"provisioning"`) || !strings.Contains(created.Body.String(), `"targetId":"docker-alpha"`) || !strings.Contains(created.Body.String(), `"providerCredentialRef":"provider-alpha"`) || !strings.Contains(created.Body.String(), `"memoryLimitBytes":536870912`) {
		t.Fatalf("create status=%d calls=%d verification=%#v body=%s", created.Code, store.create, verifier.seen, created.Body.String())
	}
	got := request(http.MethodGet, "/v1/managed-host/tenants/tenant-alpha/projects/project-alpha/environment-leases/lease-alpha", "", "request-get", "")
	if got.Code != http.StatusOK || store.get != 1 || verifier.seen.RequiredPermission != "projects.get" {
		t.Fatalf("get status=%d calls=%d verification=%#v body=%s", got.Code, store.get, verifier.seen, got.Body.String())
	}
	listed := request(http.MethodGet, "/v1/managed-host/tenants/tenant-alpha/projects/project-alpha/environment-leases?pageSize=1", "", "request-list", "")
	var listedBody struct {
		Kind          string `json:"kind"`
		NextPageToken string `json:"nextPageToken"`
	}
	if err := json.Unmarshal(listed.Body.Bytes(), &listedBody); err != nil || listed.Code != http.StatusOK || store.list != 1 || store.limit != 1 || verifier.seen.RequiredPermission != "projects.get" || listedBody.Kind != "EnvironmentLeasePage" || listedBody.NextPageToken == "" {
		t.Fatalf("list status=%d calls=%d limit=%d verification=%#v body=%s error=%v", listed.Code, store.list, store.limit, verifier.seen, listed.Body.String(), err)
	}
	pageTwo := request(http.MethodGet, "/v1/managed-host/tenants/tenant-alpha/projects/project-alpha/environment-leases?pageSize=1&pageToken="+listedBody.NextPageToken, "", "request-list-two", "")
	if pageTwo.Code != http.StatusOK || store.list != 2 || store.after != "lease-alpha" {
		t.Fatalf("page two status=%d calls=%d after=%q body=%s", pageTwo.Code, store.list, store.after, pageTwo.Body.String())
	}
	crossProject := request(http.MethodGet, "/v1/managed-host/tenants/tenant-alpha/projects/project-other/environment-leases?pageToken="+listedBody.NextPageToken, "", "request-list-cross", "")
	if crossProject.Code != http.StatusBadRequest || store.list != 2 {
		t.Fatalf("cross-project status=%d calls=%d body=%s", crossProject.Code, store.list, crossProject.Body.String())
	}
	terminated := request(http.MethodPost, "/v1/managed-host/tenants/tenant-alpha/projects/project-alpha/environment-leases/lease-alpha:terminate", `{"expectedGeneration":1}`, "request-terminate", "terminate-key-123456")
	if terminated.Code != http.StatusOK || store.terminate != 1 || store.finalize != 1 || verifier.seen.RequiredPermission != "projects.act" || !strings.Contains(terminated.Body.String(), `"cleanupPhase":"complete"`) {
		t.Fatalf("terminate status=%d calls=%d finalize=%d verification=%#v body=%s", terminated.Code, store.terminate, store.finalize, verifier.seen, terminated.Body.String())
	}
}

type managedHostEnvironmentLeaseVerifierFake struct {
	requests []authn.VerificationRequest
	failAt   int
}

func (fake *managedHostEnvironmentLeaseVerifierFake) Verify(_ string, request authn.VerificationRequest) (*authn.VerifiedPrincipal, error) {
	fake.requests = append(fake.requests, request)
	if fake.failAt == len(fake.requests) {
		return nil, errors.New("verification failed")
	}
	return &authn.VerifiedPrincipal{}, nil
}

func TestAdminEnvironmentLeaseHTTPListsResourcesAndChecksAdminScope(t *testing.T) {
	now := time.Date(2026, time.September, 3, 9, 0, 0, 0, time.UTC)
	verifier := &managedHostEnvironmentLeaseVerifierFake{}
	store := &managedHostEnvironmentLeaseStoreFake{snapshot: internalmanagedhost.Snapshot{
		Scope:   internalmanagedhost.Scope{TenantID: "tenant-alpha", ProjectID: "project-alpha"},
		LeaseID: "lease-alpha", LeaseName: "Lease Alpha", EnvironmentID: "environment-alpha",
		ReleaseDigest: "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		Generation:    2, DesiredPhase: "active", ObservedPhase: "ready", CleanupPhase: "none", ResourceVersion: 3,
		ExpiresAt: now.Add(time.Hour), CreatedAt: now, UpdatedAt: now,
	}}
	handler, err := NewAdminEnvironmentLeaseHTTPServer(verifier, store, nil, nil, nil, dockertarget.WorkerTrust{})
	if err != nil {
		t.Fatal(err)
	}
	request := func(method, path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, nil)
		req.Header.Set("Authorization", "Bearer admin-token")
		req.Header.Set("X-Request-ID", "request-admin-lease")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		return response
	}
	listed := request(http.MethodGet, "/v1/admin/tenants/tenant-alpha/projects/project-alpha/environment-leases?pageSize=50")
	if listed.Code != http.StatusOK || store.list != 1 || len(verifier.requests) != 3 || verifier.requests[0].RequiredPermission != "projects.get" || verifier.requests[1].RequiredPermission != "leases.list" || verifier.requests[2].RequiredPermission != "projects.get" {
		t.Fatalf("list status=%d calls=%d requests=%#v body=%s", listed.Code, store.list, verifier.requests, listed.Body.String())
	}
	got := request(http.MethodGet, "/v1/admin/tenants/tenant-alpha/projects/project-alpha/environment-leases/lease-alpha")
	if got.Code != http.StatusOK || store.get != 1 || len(verifier.requests) != 6 || verifier.requests[4].RequiredPermission != "leases.get" {
		t.Fatalf("get status=%d calls=%d requests=%#v body=%s", got.Code, store.get, verifier.requests, got.Body.String())
	}
	created := request(http.MethodPost, "/v1/admin/tenants/tenant-alpha/projects/project-alpha/environment-leases")
	if created.Code != http.StatusMethodNotAllowed || store.create != 0 {
		t.Fatalf("admin create status=%d calls=%d body=%s", created.Code, store.create, created.Body.String())
	}
	workers := request(http.MethodGet, "/v1/admin/tenants/tenant-alpha/projects/project-alpha/workers?pageSize=1")
	if workers.Code != http.StatusOK || store.workers != 1 || len(verifier.requests) != 9 || verifier.requests[7].RequiredPermission != "workers.list" || !strings.Contains(workers.Body.String(), `"kind":"WorkerPage"`) || strings.Contains(workers.Body.String(), "providerCredentialRef") || strings.Contains(workers.Body.String(), "workerEndpoint") {
		t.Fatalf("workers status=%d calls=%d requests=%#v body=%s", workers.Code, store.workers, verifier.requests, workers.Body.String())
	}
}

func TestAdminEnvironmentLeaseUpgradePreviewIsServerAuthoritative(t *testing.T) {
	now := time.Date(2026, time.September, 4, 8, 0, 0, 0, time.UTC)
	current := "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	target := "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	impact := "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	lease := internalmanagedhost.Snapshot{
		Scope: internalmanagedhost.Scope{TenantID: "tenant-alpha", ProjectID: "project-alpha"}, LeaseID: "lease-alpha", LeaseName: "lease-alpha",
		TargetID: "docker-alpha", TargetGeneration: 7, ReleaseDigest: current, Generation: 2, ResourceVersion: 3,
		DesiredPhase: "active", ObservedPhase: "ready", CleanupPhase: "none", CreatedAt: now, UpdatedAt: now,
	}
	store := &managedHostEnvironmentLeaseStoreFake{adminPreview: internalmanagedhost.AdminEnvironmentLeaseUpgradePreview{
		Lease: lease, Action: "upgrade", TargetKind: "docker", TargetReleaseDigest: target,
		RollbackReleaseDigest: current, RollbackGeneration: 2, ImpactDigest: impact,
	}}
	handler, err := NewAdminEnvironmentLeaseHTTPServer(&managedHostEnvironmentLeaseVerifierFake{}, store, nil, nil, nil, dockertarget.WorkerTrust{})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/v1/admin/tenants/tenant-alpha/projects/project-alpha/environment-leases/lease-alpha:upgrade-preview?releaseDigest="+target, nil)
	request.Header.Set("Authorization", "Bearer admin-token")
	request.Header.Set("X-Request-ID", "request-upgrade-preview")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	decoded, decodeErr := platformv1alpha1.DecodeEnvironmentLeaseUpgradePreviewResponseJSON(response.Body.Bytes())
	if response.Code != http.StatusOK || decodeErr != nil || store.previewCalls != 1 || store.previewAction != "upgrade" || store.previewRelease != target ||
		decoded.Value.Spec.ExpectedGeneration != 2 || decoded.Value.Spec.ExpectedResourceVersion != "3" || decoded.Value.Spec.AffectedTargets != 1 || decoded.Value.Spec.AffectedWorkers != 1 || decoded.Value.Spec.AffectedLeases != 1 || decoded.Value.Spec.ImpactDigest != impact ||
		strings.Contains(response.Body.String(), "providerCredentialRef") || strings.Contains(response.Body.String(), "workerEndpoint") {
		t.Fatalf("status=%d calls=%d action=%q release=%q preview=%#v decode=%v body=%s", response.Code, store.previewCalls, store.previewAction, store.previewRelease, decoded.Value, decodeErr, response.Body.String())
	}
	invalid := httptest.NewRequest(http.MethodGet, "/v1/admin/tenants/tenant-alpha/projects/project-alpha/environment-leases/lease-alpha:upgrade-preview?releaseDigest="+target+"&extra=true", nil)
	invalid.Header.Set("Authorization", "Bearer admin-token")
	invalid.Header.Set("X-Request-ID", "request-invalid-preview")
	invalidResponse := httptest.NewRecorder()
	handler.ServeHTTP(invalidResponse, invalid)
	if invalidResponse.Code != http.StatusBadRequest || store.previewCalls != 1 {
		t.Fatalf("invalid status=%d calls=%d body=%s", invalidResponse.Code, store.previewCalls, invalidResponse.Body.String())
	}
}

func TestAdminEnvironmentLeaseUpgradeConfirmsExactFencesAndReturnsOperation(t *testing.T) {
	now := time.Date(2026, time.September, 4, 9, 0, 0, 0, time.UTC)
	targetDigest := "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	impactDigest := "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	requestedBy := "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	startedOperation := internaldeploymenttarget.Operation{
		Scope: internaldeploymenttarget.Scope{TenantID: "tenant-alpha", ProjectID: "project-alpha"}, OperationID: "operation-upgrade", IdempotencyKey: "upgrade-key-123456", Action: "target.upgrade", TargetID: "docker-alpha",
		TargetGeneration: 7, RequestedBy: requestedBy, RequestID: "request-admin-upgrade", State: "running", CurrentStep: "deploy-worker", ImpactSummary: "Upgrade 1 Worker and 1 Lease from generation 2", RequestedAt: now, UpdatedAt: now,
	}
	failedOperation := startedOperation
	failedOperation.State, failedOperation.CurrentStep, failedOperation.StableErrorCode, failedOperation.Retryable = "failed", "complete", "docker-actuator-unconfigured", true
	lease := internalmanagedhost.Snapshot{
		Scope: internalmanagedhost.Scope{TenantID: "tenant-alpha", ProjectID: "project-alpha"}, LeaseID: "lease-alpha", LeaseName: "Lease Alpha", EnvironmentID: "environment-alpha",
		TargetID: "docker-alpha", TargetGeneration: 7, ProviderCredentialRef: "provider-alpha", CPULimitMillis: 1000, MemoryLimitBytes: 512 << 20,
		ReleaseDigest: targetDigest, Generation: 3, ResourceVersion: 4, DesiredPhase: "active", ObservedPhase: "provisioning", CleanupPhase: "none", ExpiresAt: now.Add(time.Hour), CreatedAt: now, UpdatedAt: now,
	}
	store := &managedHostEnvironmentLeaseStoreFake{
		target:      internaldeploymenttarget.Snapshot{Scope: internaldeploymenttarget.Scope{TenantID: "tenant-alpha", ProjectID: "project-alpha"}, TargetID: "docker-alpha", Kind: "docker", Generation: 7, SchedulingState: "drained", ObservedPhase: "ready"},
		adminStart:  postgres.AdminEnvironmentLeaseUpgradeStart{Snapshot: lease, Operation: startedOperation, Execute: true},
		adminResult: postgres.AdminEnvironmentLeaseUpgradeResult{Snapshot: lease, Operation: failedOperation},
	}
	verifier := &managedHostEnvironmentLeaseVerifierFake{}
	handler, err := NewAdminEnvironmentLeaseHTTPServer(verifier, store, nil, nil, nil, dockertarget.WorkerTrust{})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/v1/admin/tenants/tenant-alpha/projects/project-alpha/environment-leases/lease-alpha:upgrade", strings.NewReader(`{"releaseDigest":"`+targetDigest+`","expectedGeneration":2,"expectedResourceVersion":"3","impactDigest":"`+impactDigest+`"}`))
	request.Header.Set("Authorization", "Bearer admin-token")
	request.Header.Set("X-Request-ID", "request-admin-upgrade")
	request.Header.Set("Idempotency-Key", "upgrade-key-123456")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	operation, decodeErr := platformv1alpha1.DecodeMaintenanceOperationResponseJSON(response.Body.Bytes())
	if response.Code != http.StatusOK || decodeErr != nil || operation.Value.Action != "target.upgrade" || operation.Value.State != "failed" || store.adminBegin != 1 || store.adminComplete != 1 ||
		store.adminInput.Action != "upgrade" || store.adminInput.ReleaseDigest != targetDigest || store.adminInput.ExpectedGeneration != 2 || store.adminInput.ExpectedResourceVersion != 3 || store.adminInput.ImpactDigest != impactDigest ||
		store.adminCompletion.ExpectedGeneration != 3 || store.adminCompletion.StableErrorCode != "docker-actuator-unconfigured" {
		t.Fatalf("status=%d begin=%d complete=%d input=%#v completion=%#v operation=%#v decode=%v body=%s", response.Code, store.adminBegin, store.adminComplete, store.adminInput, store.adminCompletion, operation.Value, decodeErr, response.Body.String())
	}
	wantPermissions := []string{"projects.act", "leases.act", "projects.get", "projects.act", "projects.act"}
	if len(verifier.requests) != len(wantPermissions) {
		t.Fatalf("verifications=%#v", verifier.requests)
	}
	for index, permission := range wantPermissions {
		if verifier.requests[index].RequiredPermission != permission {
			t.Fatalf("verification %d permission=%q, want %q", index, verifier.requests[index].RequiredPermission, permission)
		}
	}
}

func TestAdminEnvironmentLeaseUpgradeReturnsForbiddenWithoutLeaseActScope(t *testing.T) {
	verifier := &managedHostEnvironmentLeaseVerifierFake{failAt: 2}
	store := &managedHostEnvironmentLeaseStoreFake{}
	handler, err := NewAdminEnvironmentLeaseHTTPServer(verifier, store, nil, nil, nil, dockertarget.WorkerTrust{})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/v1/admin/tenants/tenant-alpha/projects/project-alpha/environment-leases/lease-alpha:upgrade", nil)
	request.Header.Set("Authorization", "Bearer user-token")
	request.Header.Set("X-Request-ID", "request-user-admin-upgrade")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden || store.adminBegin != 0 || len(verifier.requests) != 2 || verifier.requests[1].RequiredPermission != "leases.act" {
		t.Fatalf("status=%d begin=%d requests=%#v body=%s", response.Code, store.adminBegin, verifier.requests, response.Body.String())
	}
}

func TestAdminEnvironmentLeaseHTTPReturnsForbiddenWithoutLeaseScope(t *testing.T) {
	verifier := &managedHostEnvironmentLeaseVerifierFake{failAt: 2}
	handler, err := NewAdminEnvironmentLeaseHTTPServer(verifier, &managedHostEnvironmentLeaseStoreFake{}, nil, nil, nil, dockertarget.WorkerTrust{})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/v1/admin/tenants/tenant-alpha/projects/project-alpha/environment-leases?pageSize=50", nil)
	request.Header.Set("Authorization", "Bearer user-token")
	request.Header.Set("X-Request-ID", "request-user-admin-lease")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), `"code":"AUTHORIZATION_DENIED"`) || len(verifier.requests) != 2 {
		t.Fatalf("status=%d requests=%#v body=%s", response.Code, verifier.requests, response.Body.String())
	}
}

func TestAdminWorkerHTTPReturnsForbiddenWithoutWorkerScope(t *testing.T) {
	verifier := &managedHostEnvironmentLeaseVerifierFake{failAt: 2}
	handler, err := NewAdminEnvironmentLeaseHTTPServer(verifier, &managedHostEnvironmentLeaseStoreFake{}, nil, nil, nil, dockertarget.WorkerTrust{})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/v1/admin/tenants/tenant-alpha/projects/project-alpha/workers?pageSize=50", nil)
	request.Header.Set("Authorization", "Bearer user-token")
	request.Header.Set("X-Request-ID", "request-user-admin-worker")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), `"code":"AUTHORIZATION_DENIED"`) || len(verifier.requests) != 2 || verifier.requests[1].RequiredPermission != "workers.list" {
		t.Fatalf("status=%d requests=%#v body=%s", response.Code, verifier.requests, response.Body.String())
	}
}

func TestManagedHostEnvironmentLeaseRouteRequiresLeaseScope(t *testing.T) {
	verifier := &managedHostEnvironmentLeaseVerifierFake{failAt: 2}
	handler, err := NewManagedHostEnvironmentLeaseHTTPServer(verifier, &managedHostEnvironmentLeaseStoreFake{}, nil, nil, nil, dockertarget.WorkerTrust{})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/v1/managed-host/tenants/tenant-alpha/projects/project-alpha/environment-leases?pageSize=50", nil)
	request.Header.Set("Authorization", "Bearer user-token")
	request.Header.Set("X-Request-ID", "request-user-lease-route")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden || len(verifier.requests) != 2 || verifier.requests[1].RequiredPermission != "leases.list" {
		t.Fatalf("status=%d requests=%#v body=%s", response.Code, verifier.requests, response.Body.String())
	}
}

func TestEnvironmentLeaseActuatorReverifiesEachAuthorizedStoreOperation(t *testing.T) {
	now := time.Date(2026, time.September, 2, 8, 0, 0, 0, time.UTC)
	verifier := &managedHostEnvironmentLeaseVerifierFake{}
	store := &managedHostEnvironmentLeaseStoreFake{
		snapshot: internalmanagedhost.Snapshot{
			LeaseID: "lease-alpha", LeaseName: "lease-alpha", EnvironmentID: "lease-alpha",
			ReleaseDigest: "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
			Generation:    1, DesiredPhase: "active", ObservedPhase: "provisioning", CleanupPhase: "none", ResourceVersion: 1,
			ExpiresAt: now.Add(time.Hour), CreatedAt: now, UpdatedAt: now,
		},
		target: internaldeploymenttarget.Snapshot{TargetID: "docker-alpha", Kind: "docker", Generation: 1, ObservedPhase: "ready"},
	}
	credentials, err := dockertarget.NewCredentialDirectory(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	handler, err := NewManagedHostEnvironmentLeaseHTTPServer(verifier, store, credentials, nil, nil, dockertarget.WorkerTrust{})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/v1/managed-host/tenants/tenant-alpha/projects/project-alpha/environment-leases", strings.NewReader(`{"leaseId":"lease-alpha","leaseName":"lease-alpha","releaseDigest":"sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef","targetId":"docker-alpha","expectedTargetGeneration":1,"providerCredentialRef":"provider-alpha","cpuLimitMillis":1000,"memoryLimitBytes":536870912,"ttlSeconds":3600}`))
	request.Header.Set("Authorization", "Bearer access-token")
	request.Header.Set("X-Request-ID", "request-create-alpha")
	request.Header.Set("Idempotency-Key", "create-alpha-key-1234")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusCreated || store.snapshot.ObservedPhase != "failed" || len(verifier.requests) != 5 {
		t.Fatalf("status=%d phase=%q verifications=%#v body=%s", response.Code, store.snapshot.ObservedPhase, verifier.requests, response.Body.String())
	}
	for index, permission := range []string{"projects.act", "leases.act", "projects.act", "projects.get", "projects.act"} {
		if verifier.requests[index].RequiredPermission != permission {
			t.Fatalf("verification %d permission=%q, want %q", index, verifier.requests[index].RequiredPermission, permission)
		}
	}

	verifier.requests = nil
	request = httptest.NewRequest(http.MethodPost, "/v1/managed-host/tenants/tenant-alpha/projects/project-alpha/environment-leases/lease-alpha:terminate", strings.NewReader(`{"expectedGeneration":1}`))
	request.Header.Set("Authorization", "Bearer access-token")
	request.Header.Set("X-Request-ID", "request-terminate-alpha")
	request.Header.Set("Idempotency-Key", "terminate-alpha-key-1234")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadGateway || len(verifier.requests) != 4 || verifier.requests[0].RequiredPermission != "projects.act" || verifier.requests[1].RequiredPermission != "leases.act" || verifier.requests[2].RequiredPermission != "projects.act" || verifier.requests[3].RequiredPermission != "projects.get" {
		t.Fatalf("status=%d verifications=%#v body=%s", response.Code, verifier.requests, response.Body.String())
	}
}

func TestEnvironmentLeaseRoutesKubernetesTargetToKubernetesActuator(t *testing.T) {
	now := time.Date(2026, time.September, 2, 9, 0, 0, 0, time.UTC)
	store := &managedHostEnvironmentLeaseStoreFake{
		snapshot: internalmanagedhost.Snapshot{
			LeaseID: "lease-kubernetes", LeaseName: "lease-kubernetes", EnvironmentID: "lease-kubernetes",
			ReleaseDigest: "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
			Generation:    1, DesiredPhase: "active", ObservedPhase: "provisioning", CleanupPhase: "none", ResourceVersion: 1,
			ExpiresAt: now.Add(time.Hour), CreatedAt: now, UpdatedAt: now,
		},
		target: internaldeploymenttarget.Snapshot{TargetID: "kubernetes-alpha", Kind: "kubernetes", Endpoint: "https://kubernetes.example.test:6443", CredentialRef: "kubernetes-alpha", Generation: 1, ObservedPhase: "ready"},
	}
	credentials, err := kubernetestarget.NewCredentialDirectory(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	handler, err := NewManagedHostEnvironmentLeaseHTTPServer(&projectHTTPVerifierFake{}, store, nil, credentials, nil, dockertarget.WorkerTrust{})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/v1/managed-host/tenants/tenant-alpha/projects/project-alpha/environment-leases", strings.NewReader(`{"leaseId":"lease-kubernetes","leaseName":"lease-kubernetes","releaseDigest":"sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef","targetId":"kubernetes-alpha","expectedTargetGeneration":1,"providerCredentialRef":"provider-alpha","cpuLimitMillis":1000,"memoryLimitBytes":536870912,"ttlSeconds":3600}`))
	request.Header.Set("Authorization", "Bearer access-token")
	request.Header.Set("X-Request-ID", "request-create-kubernetes")
	request.Header.Set("Idempotency-Key", "create-kubernetes-key-1234")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusCreated || store.snapshot.ObservedPhase != "failed" || store.snapshot.StableErrorCode != "kubernetes-deployment-config-invalid" || strings.Contains(response.Body.String(), "docker-") {
		t.Fatalf("status=%d phase=%q stable=%q body=%s", response.Code, store.snapshot.ObservedPhase, store.snapshot.StableErrorCode, response.Body.String())
	}
}

func TestEnvironmentLeaseRoutesSSHTargetToSSHActuator(t *testing.T) {
	now := time.Date(2026, time.September, 2, 9, 0, 0, 0, time.UTC)
	store := &managedHostEnvironmentLeaseStoreFake{
		snapshot: internalmanagedhost.Snapshot{
			LeaseID: "lease-ssh", LeaseName: "lease-ssh", EnvironmentID: "lease-ssh",
			ReleaseDigest: "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
			Generation:    1, DesiredPhase: "active", ObservedPhase: "provisioning", CleanupPhase: "none", ResourceVersion: 1,
			ExpiresAt: now.Add(time.Hour), CreatedAt: now, UpdatedAt: now,
		},
		target: internaldeploymenttarget.Snapshot{TargetID: "ssh-alpha", Kind: "ssh", Endpoint: "ssh://ssh.example.test:22", CredentialRef: "ssh-alpha", Generation: 1, ObservedPhase: "ready"},
	}
	credentials, err := sshtarget.NewCredentialDirectory(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	handler, err := NewManagedHostEnvironmentLeaseHTTPServer(&projectHTTPVerifierFake{}, store, nil, nil, credentials, dockertarget.WorkerTrust{})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/v1/managed-host/tenants/tenant-alpha/projects/project-alpha/environment-leases", strings.NewReader(`{"leaseId":"lease-ssh","leaseName":"lease-ssh","releaseDigest":"sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef","targetId":"ssh-alpha","expectedTargetGeneration":1,"providerCredentialRef":"provider-alpha","cpuLimitMillis":1000,"memoryLimitBytes":536870912,"ttlSeconds":3600}`))
	request.Header.Set("Authorization", "Bearer access-token")
	request.Header.Set("X-Request-ID", "request-create-ssh")
	request.Header.Set("Idempotency-Key", "create-ssh-key-123456")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusCreated || store.snapshot.ObservedPhase != "failed" || store.snapshot.StableErrorCode != "ssh-deployment-config-invalid" || strings.Contains(response.Body.String(), "docker-") {
		t.Fatalf("status=%d phase=%q stable=%q body=%s", response.Code, store.snapshot.ObservedPhase, store.snapshot.StableErrorCode, response.Body.String())
	}
}

func TestTerminateReadyEnvironmentLeaseRequiresDockerCleanup(t *testing.T) {
	now := time.Date(2026, time.September, 2, 8, 0, 0, 0, time.UTC)
	store := &managedHostEnvironmentLeaseStoreFake{snapshot: internalmanagedhost.Snapshot{
		LeaseID: "lease-ready", LeaseName: "lease-ready", EnvironmentID: "lease-ready",
		ReleaseDigest: "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		TargetID:      "docker-alpha", TargetGeneration: 1, ProviderCredentialRef: "provider-alpha",
		CPULimitMillis: 1000, MemoryLimitBytes: 512 << 20, WorkerEndpoint: "https://docker.example.test:32768",
		WorkerSPIFFEID: "spiffe://cloud-agents.test/workers/docker-alpha", WorkerServerName: "worker.example.test",
		Generation: 1, DesiredPhase: "active", ObservedPhase: "ready", CleanupPhase: "none", ResourceVersion: 2,
		ExpiresAt: now.Add(time.Hour), CreatedAt: now, UpdatedAt: now,
	}}
	handler, err := NewManagedHostEnvironmentLeaseHTTPServer(&projectHTTPVerifierFake{}, store, nil, nil, nil, dockertarget.WorkerTrust{})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/v1/managed-host/tenants/tenant-alpha/projects/project-alpha/environment-leases/lease-ready:terminate", strings.NewReader(`{"expectedGeneration":1}`))
	request.Header.Set("Authorization", "Bearer access-token")
	request.Header.Set("X-Request-ID", "request-terminate-ready")
	request.Header.Set("Idempotency-Key", "terminate-ready-key-000034")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable || store.terminate != 1 || store.finalize != 0 || !strings.Contains(response.Body.String(), `"code":"ENVIRONMENT_CLEANUP_UNAVAILABLE"`) {
		t.Fatalf("status=%d terminate=%d finalize=%d body=%s", response.Code, store.terminate, store.finalize, response.Body.String())
	}
}
