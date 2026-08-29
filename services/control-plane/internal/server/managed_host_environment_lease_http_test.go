package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authn"
	internalmanagedhost "github.com/hxp0618/cloud-agents/services/control-plane/internal/managedhost"
)

type managedHostEnvironmentLeaseStoreFake struct {
	snapshot  internalmanagedhost.Snapshot
	create    int
	get       int
	terminate int
}

func (fake *managedHostEnvironmentLeaseStoreFake) CreateManagedHostEnvironmentLease(_ context.Context, _ string, _ *authn.VerifiedPrincipal, input internalmanagedhost.CreateEnvironmentLeaseInput) (internalmanagedhost.Snapshot, error) {
	fake.create++
	fake.snapshot.Scope = input.Scope
	fake.snapshot.LeaseID = input.LeaseID
	fake.snapshot.LeaseName = input.LeaseName
	fake.snapshot.ReleaseDigest = input.ReleaseDigest
	return fake.snapshot, nil
}

func (fake *managedHostEnvironmentLeaseStoreFake) GetManagedHostEnvironmentLease(context.Context, string, *authn.VerifiedPrincipal, string, string) (internalmanagedhost.Snapshot, error) {
	fake.get++
	return fake.snapshot, nil
}

func (fake *managedHostEnvironmentLeaseStoreFake) TerminateManagedHostEnvironmentLease(_ context.Context, _ string, _ *authn.VerifiedPrincipal, input internalmanagedhost.TerminateEnvironmentLeaseInput) (internalmanagedhost.Snapshot, error) {
	fake.terminate++
	fake.snapshot.Scope = input.Scope
	fake.snapshot.LeaseID = input.LeaseID
	fake.snapshot.DesiredPhase = "terminated"
	fake.snapshot.ObservedPhase = "terminated"
	fake.snapshot.CleanupPhase = "complete"
	fake.snapshot.Generation++
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
	handler, err := NewManagedHostEnvironmentLeaseHTTPServer(verifier, store)
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
	created := request(http.MethodPost, "/v1/managed-host/tenants/tenant-alpha/projects/project-alpha/environment-leases", `{"leaseId":"lease-alpha","leaseName":"default","releaseDigest":"sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef","ttlSeconds":3600}`, "request-create", "create-key-123456")
	if created.Code != http.StatusCreated || store.create != 1 || verifier.seen.RequiredPermission != "projects.act" || !strings.Contains(created.Body.String(), `"observedPhase":"provisioning"`) {
		t.Fatalf("create status=%d calls=%d verification=%#v body=%s", created.Code, store.create, verifier.seen, created.Body.String())
	}
	got := request(http.MethodGet, "/v1/managed-host/tenants/tenant-alpha/projects/project-alpha/environment-leases/lease-alpha", "", "request-get", "")
	if got.Code != http.StatusOK || store.get != 1 || verifier.seen.RequiredPermission != "projects.get" {
		t.Fatalf("get status=%d calls=%d verification=%#v body=%s", got.Code, store.get, verifier.seen, got.Body.String())
	}
	terminated := request(http.MethodPost, "/v1/managed-host/tenants/tenant-alpha/projects/project-alpha/environment-leases/lease-alpha:terminate", `{"expectedGeneration":1}`, "request-terminate", "terminate-key-123456")
	if terminated.Code != http.StatusOK || store.terminate != 1 || verifier.seen.RequiredPermission != "projects.act" || !strings.Contains(terminated.Body.String(), `"cleanupPhase":"complete"`) {
		t.Fatalf("terminate status=%d calls=%d verification=%#v body=%s", terminated.Code, store.terminate, verifier.seen, terminated.Body.String())
	}
}
