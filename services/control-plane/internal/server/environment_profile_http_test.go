package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	platformv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/platform/v1alpha1"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authn"
	internalenvironmentprofile "github.com/hxp0618/cloud-agents/services/control-plane/internal/environmentprofile"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/store/postgres"
)

type environmentProfileStoreFake struct {
	snapshot       internalenvironmentprofile.Snapshot
	input          internalenvironmentprofile.CreateInput
	audit          []internalenvironmentprofile.AuditEvent
	getPrincipal   *authn.VerifiedPrincipal
	auditPrincipal *authn.VerifiedPrincipal
	created        int
	listed         int
	got            int
}

func (fake *environmentProfileStoreFake) CreateEnvironmentProfile(_ context.Context, _ string, _ *authn.VerifiedPrincipal, input internalenvironmentprofile.CreateInput) (internalenvironmentprofile.Snapshot, error) {
	fake.created++
	fake.input = input
	return fake.snapshot, nil
}

func (fake *environmentProfileStoreFake) GetEnvironmentProfile(_ context.Context, _ string, principal *authn.VerifiedPrincipal, _ string, _ string, _ int64) (internalenvironmentprofile.Snapshot, error) {
	fake.got++
	fake.getPrincipal = principal
	return fake.snapshot, nil
}

func (fake *environmentProfileStoreFake) ListEnvironmentProfiles(context.Context, string, *authn.VerifiedPrincipal, string, string, int) (postgres.EnvironmentProfilePage, error) {
	fake.listed++
	return postgres.EnvironmentProfilePage{EnvironmentProfiles: []internalenvironmentprofile.Snapshot{fake.snapshot}}, nil
}

func (fake *environmentProfileStoreFake) ListEnvironmentProfileAuditEvents(_ context.Context, _ string, principal *authn.VerifiedPrincipal, _ string, _ string, _ *time.Time, _ string, _ int) (postgres.EnvironmentProfileAuditPage, error) {
	fake.auditPrincipal = principal
	return postgres.EnvironmentProfileAuditPage{Events: fake.audit}, nil
}

type environmentProfileVerifierFake struct {
	requests       []authn.VerificationRequest
	failPermission string
}

func (fake *environmentProfileVerifierFake) Verify(_ string, request authn.VerificationRequest) (*authn.VerifiedPrincipal, error) {
	fake.requests = append(fake.requests, request)
	if request.RequiredPermission == fake.failPermission {
		return nil, errors.New("verification failed")
	}
	return &authn.VerifiedPrincipal{}, nil
}

func TestAdminEnvironmentProfileHTTPDraftLifecycle(t *testing.T) {
	now := time.Date(2026, time.September, 3, 7, 0, 0, 0, time.UTC)
	store := &environmentProfileStoreFake{snapshot: internalenvironmentprofile.Snapshot{
		Scope:             internalenvironmentprofile.Scope{TenantID: "tenant-alpha", ProjectID: "project-alpha"},
		ProfileVersionUID: "ep-alpha-v1", ProfileID: "profile-alpha", ProfileName: "profile-alpha", Version: 1,
		Description: "Codex and Claude development", Status: "draft", ProviderKinds: []string{"codex", "claudeAgent"},
		CPULimitMillis: 2000, MemoryLimitBytes: 4294967296, StoragePolicyRef: "storage-standard",
		NetworkPolicyRef: "network-public", ReleaseDigest: "sha256:" + strings.Repeat("a", 64),
		TargetRefs: []string{"docker-alpha"}, ProviderCredentialRef: "providers-alpha",
		ResourceVersion: 1, CreatedAt: now, UpdatedAt: now,
	}, audit: []internalenvironmentprofile.AuditEvent{{
		Scope:   internalenvironmentprofile.Scope{TenantID: "tenant-alpha", ProjectID: "project-alpha"},
		EventID: "event-profile-create", OperationID: "operation-profile-create", Actor: "sha256:" + strings.Repeat("b", 64),
		Action: "profile.create", ProfileUID: "ep-alpha-v1", ProfileVersion: 1, Result: "succeeded",
		RequestID: "request-profile-create", OccurredAt: now,
	}}}
	verifier := &environmentProfileVerifierFake{}
	handler, err := NewAdminEnvironmentProfileHTTPServer(verifier, store)
	if err != nil {
		t.Fatal(err)
	}

	create := httptest.NewRequest(http.MethodPost, "/v1/admin/tenants/tenant-alpha/projects/project-alpha/environment-profiles", strings.NewReader(`{"profileId":"profile-alpha","profileName":"profile-alpha","version":1,"description":"Codex and Claude development","providerKinds":["codex","claudeAgent"],"cpuLimitMillis":2000,"memoryLimitBytes":4294967296,"storagePolicyRef":"storage-standard","networkPolicyRef":"network-public","releaseDigest":"sha256:`+strings.Repeat("a", 64)+`","targetRefs":["docker-alpha"],"providerCredentialRef":"providers-alpha"}`))
	create.Header.Set("Authorization", "Bearer admin-token")
	create.Header.Set("X-Request-ID", "request-profile-create")
	create.Header.Set("Idempotency-Key", "profile-create-key")
	created := httptest.NewRecorder()
	handler.ServeHTTP(created, create)
	createdProfile, decodeErr := platformv1alpha1.DecodeEnvironmentProfileResponseJSON(created.Body.Bytes())
	if created.Code != http.StatusCreated || decodeErr != nil || createdProfile.Value.Spec.Status != "draft" || createdProfile.Value.Spec.ProviderCredentialRef != "providers-alpha" || store.created != 1 || store.input.Mutation.IdempotencyKey != "profile-create-key" {
		t.Fatalf("status=%d decodeErr=%v profile=%#v store=%#v", created.Code, decodeErr, createdProfile.Value, store)
	}

	request := func(path, requestID string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Authorization", "Bearer admin-token")
		req.Header.Set("X-Request-ID", requestID)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		return response
	}
	listed := request("/v1/admin/tenants/tenant-alpha/projects/project-alpha/environment-profiles?pageSize=50", "request-profile-list")
	page, decodeErr := platformv1alpha1.DecodeEnvironmentProfilePageResponseJSON(listed.Body.Bytes())
	if listed.Code != http.StatusOK || decodeErr != nil || len(page.Value.EnvironmentProfiles) != 1 || store.listed != 1 {
		t.Fatalf("list status=%d decodeErr=%v body=%s", listed.Code, decodeErr, listed.Body.String())
	}
	got := request("/v1/admin/tenants/tenant-alpha/projects/project-alpha/environment-profiles/profile-alpha/versions/1", "request-profile-get")
	if got.Code != http.StatusOK || store.got != 1 {
		t.Fatalf("get status=%d body=%s", got.Code, got.Body.String())
	}
	audit := request("/v1/admin/tenants/tenant-alpha/projects/project-alpha/environment-profiles/profile-alpha/versions/1/audit-events?pageSize=50", "request-profile-audit")
	auditPage, decodeErr := platformv1alpha1.DecodeAdminAuditEventPageResponseJSON(audit.Body.Bytes())
	if audit.Code != http.StatusOK || decodeErr != nil || len(auditPage.Value.Events) != 1 || auditPage.Value.Events[0].Action != "profile.create" || store.got != 2 {
		t.Fatalf("audit status=%d decodeErr=%v body=%s", audit.Code, decodeErr, audit.Body.String())
	}
	if store.getPrincipal == nil || store.auditPrincipal == nil || store.getPrincipal == store.auditPrincipal {
		t.Fatal("profile detail and audit reads reused one verified principal")
	}
	permissions := make([]string, 0, len(verifier.requests))
	for _, request := range verifier.requests {
		permissions = append(permissions, request.RequiredPermission)
	}
	for _, required := range []string{"profiles.create", "profiles.list", "profiles.get", "audit.list"} {
		if !strings.Contains(strings.Join(permissions, " "), required) {
			t.Fatalf("missing permission %q in %v", required, permissions)
		}
	}
}

func TestAdminEnvironmentProfileHTTPRejectsUserScope(t *testing.T) {
	store := &environmentProfileStoreFake{}
	verifier := &environmentProfileVerifierFake{failPermission: "profiles.list"}
	handler, err := NewAdminEnvironmentProfileHTTPServer(verifier, store)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/v1/admin/tenants/tenant-alpha/projects/project-alpha/environment-profiles?pageSize=50", nil)
	request.Header.Set("Authorization", "Bearer user-token")
	request.Header.Set("X-Request-ID", "request-user-profile-list")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden || store.listed != 0 || len(verifier.requests) != 2 || verifier.requests[1].RequiredPermission != "profiles.list" {
		t.Fatalf("status=%d listed=%d requests=%#v", response.Code, store.listed, verifier.requests)
	}
	if !HandlesAdminEnvironmentProfilePath("/v1/admin/tenants/tenant-alpha/projects/project-alpha/environment-profiles/profile-alpha/versions/1") || HandlesAdminEnvironmentProfilePath("/v1/admin/tenants/tenant-alpha/projects/project-alpha/environment-profiles/profile-alpha/versions/0") {
		t.Fatal("environment profile route classification drifted")
	}
}
