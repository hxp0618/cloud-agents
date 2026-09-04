package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	platformv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/platform/v1alpha1"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authn"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/dockertarget"
	internalmanagedhost "github.com/hxp0618/cloud-agents/services/control-plane/internal/managedhost"
)

type userEnvironmentStoreFake struct {
	result internalmanagedhost.ProfileEnvironmentSnapshot
	input  internalmanagedhost.CreateEnvironmentFromProfileInput
	create int
	get    int
}

func (fake *userEnvironmentStoreFake) CreateUserEnvironment(_ context.Context, _ string, _ *authn.VerifiedPrincipal, input internalmanagedhost.CreateEnvironmentFromProfileInput) (internalmanagedhost.ProfileEnvironmentSnapshot, error) {
	fake.input = input
	fake.create++
	return fake.result, nil
}

func (fake *userEnvironmentStoreFake) GetUserEnvironment(_ context.Context, _ string, _ *authn.VerifiedPrincipal, _, _ string) (internalmanagedhost.ProfileEnvironmentSnapshot, error) {
	fake.get++
	return fake.result, nil
}

func TestUserEnvironmentHTTPUsesOnlyProfileInputAndReturnsSafeState(t *testing.T) {
	now := time.Date(2026, time.September, 4, 9, 0, 0, 0, time.UTC)
	result := internalmanagedhost.ProfileEnvironmentSnapshot{
		ProfileID: "profile-alpha", ProfileVersion: 3,
		Lease: internalmanagedhost.Snapshot{
			Scope:   internalmanagedhost.Scope{TenantID: "tenant-alpha", ProjectID: "project-alpha"},
			LeaseID: "environment-alpha", LeaseName: "environment-alpha", EnvironmentID: "environment-alpha",
			ReleaseDigest: "sha256:" + strings.Repeat("a", 64), TargetID: "docker-alpha", TargetGeneration: 7,
			ProviderCredentialRef: "provider-secret", CPULimitMillis: 2000, MemoryLimitBytes: 4294967296,
			Generation: 1, DesiredPhase: "active", ObservedPhase: "ready", CleanupPhase: "none",
			WorkerEndpoint: "https://worker.example.test", WorkerSPIFFEID: "spiffe://cloud-agents.dev/worker-alpha", WorkerServerName: "worker-alpha",
			ExpiresAt: now.Add(time.Hour), ResourceVersion: 2, CreatedAt: now, UpdatedAt: now,
		},
	}
	store := &userEnvironmentStoreFake{result: result}
	verifier := &environmentProfileVerifierFake{}
	leaseStore := &managedHostEnvironmentLeaseStoreFake{snapshot: result.Lease}
	actuator, err := NewManagedHostEnvironmentLeaseHTTPServer(verifier, leaseStore, nil, nil, nil, dockertarget.WorkerTrust{})
	if err != nil {
		t.Fatal(err)
	}
	handler, err := NewUserEnvironmentHTTPServer(verifier, store, actuator)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/v1/tenants/tenant-alpha/projects/project-alpha/environments", strings.NewReader(`{"profileId":"profile-alpha","profileVersion":3}`))
	request.Header.Set("Authorization", "Bearer user-token")
	request.Header.Set("X-Request-ID", "request-environment-create")
	request.Header.Set("Idempotency-Key", "idem-environment-create-01")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	decoded, decodeErr := platformv1alpha1.DecodeUserEnvironmentResponseJSON(response.Body.Bytes())
	if response.Code != http.StatusCreated || decodeErr != nil || store.create != 1 || store.input.ProfileID != "profile-alpha" || store.input.ProfileVersion != 3 {
		t.Fatalf("status=%d body=%s decoded=%#v err=%v store=%#v", response.Code, response.Body.String(), decoded, decodeErr, store)
	}
	if decoded.Value.EnvironmentID != "environment-alpha" || decoded.Value.ObservedPhase != "ready" || decoded.Value.ProjectRef.ID != "project-alpha" {
		t.Fatalf("environment=%#v", decoded.Value)
	}
	for _, forbidden := range []string{"target", "releaseDigest", "credential", "cpu", "memory", "worker", "endpoint", "spiffe"} {
		if strings.Contains(strings.ToLower(response.Body.String()), strings.ToLower(forbidden)) {
			t.Fatalf("response contains forbidden field %q: %s", forbidden, response.Body.String())
		}
	}
	if len(verifier.requests) != 2 || verifier.requests[0].RequiredPermission != "projects.act" || verifier.requests[1].RequiredPermission != "environments.create" {
		t.Fatalf("verification requests=%#v", verifier.requests)
	}

	verifier.requests = nil
	get := httptest.NewRequest(http.MethodGet, "/v1/tenants/tenant-alpha/projects/project-alpha/environments/environment-alpha", nil)
	get.Header.Set("Authorization", "Bearer user-token")
	get.Header.Set("X-Request-ID", "request-environment-get")
	getResponse := httptest.NewRecorder()
	handler.ServeHTTP(getResponse, get)
	if getResponse.Code != http.StatusOK || store.get != 1 || len(verifier.requests) != 2 || verifier.requests[0].RequiredPermission != "projects.get" || verifier.requests[1].RequiredPermission != "environments.get" {
		t.Fatalf("status=%d get=%d requests=%#v", getResponse.Code, store.get, verifier.requests)
	}
}

func TestUserEnvironmentHTTPRequiresDedicatedScope(t *testing.T) {
	store := &userEnvironmentStoreFake{}
	verifier := &environmentProfileVerifierFake{failPermission: "environments.create"}
	actuator, err := NewManagedHostEnvironmentLeaseHTTPServer(verifier, &managedHostEnvironmentLeaseStoreFake{}, nil, nil, nil, dockertarget.WorkerTrust{})
	if err != nil {
		t.Fatal(err)
	}
	handler, err := NewUserEnvironmentHTTPServer(verifier, store, actuator)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/v1/tenants/tenant-alpha/projects/project-alpha/environments", strings.NewReader(`{"profileId":"profile-alpha","profileVersion":1}`))
	request.Header.Set("Authorization", "Bearer user-token")
	request.Header.Set("X-Request-ID", "request-environment-forbidden")
	request.Header.Set("Idempotency-Key", "idem-environment-create-01")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden || store.create != 0 {
		t.Fatalf("status=%d body=%s creates=%d", response.Code, response.Body.String(), store.create)
	}
}
