package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authn"
	internaldeploymenttarget "github.com/hxp0618/cloud-agents/services/control-plane/internal/deploymenttarget"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/dockertarget"
	internalmanagedhost "github.com/hxp0618/cloud-agents/services/control-plane/internal/managedhost"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/store/postgres"
)

type managedHostEnvironmentLeaseStoreFake struct {
	snapshot  internalmanagedhost.Snapshot
	target    internaldeploymenttarget.Snapshot
	create    int
	get       int
	list      int
	after     string
	limit     int
	terminate int
	finalize  int
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
	handler, err := NewManagedHostEnvironmentLeaseHTTPServer(verifier, store, nil, dockertarget.WorkerTrust{})
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
}

func (fake *managedHostEnvironmentLeaseVerifierFake) Verify(_ string, request authn.VerificationRequest) (*authn.VerifiedPrincipal, error) {
	fake.requests = append(fake.requests, request)
	return &authn.VerifiedPrincipal{}, nil
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
	handler, err := NewManagedHostEnvironmentLeaseHTTPServer(verifier, store, credentials, dockertarget.WorkerTrust{})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/v1/managed-host/tenants/tenant-alpha/projects/project-alpha/environment-leases", strings.NewReader(`{"leaseId":"lease-alpha","leaseName":"lease-alpha","releaseDigest":"sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef","targetId":"docker-alpha","expectedTargetGeneration":1,"providerCredentialRef":"provider-alpha","cpuLimitMillis":1000,"memoryLimitBytes":536870912,"ttlSeconds":3600}`))
	request.Header.Set("Authorization", "Bearer access-token")
	request.Header.Set("X-Request-ID", "request-create-alpha")
	request.Header.Set("Idempotency-Key", "create-alpha-key-1234")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusCreated || store.snapshot.ObservedPhase != "failed" || len(verifier.requests) != 3 {
		t.Fatalf("status=%d phase=%q verifications=%#v body=%s", response.Code, store.snapshot.ObservedPhase, verifier.requests, response.Body.String())
	}
	for index, permission := range []string{"projects.act", "projects.get", "projects.act"} {
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
	if response.Code != http.StatusBadGateway || len(verifier.requests) != 2 || verifier.requests[0].RequiredPermission != "projects.act" || verifier.requests[1].RequiredPermission != "projects.get" {
		t.Fatalf("status=%d verifications=%#v body=%s", response.Code, verifier.requests, response.Body.String())
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
	handler, err := NewManagedHostEnvironmentLeaseHTTPServer(&projectHTTPVerifierFake{}, store, nil, dockertarget.WorkerTrust{})
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
