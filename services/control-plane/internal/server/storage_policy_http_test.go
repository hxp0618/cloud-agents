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
	internalstoragepolicy "github.com/hxp0618/cloud-agents/services/control-plane/internal/storagepolicy"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/store/postgres"
)

type storagePolicyStoreFake struct {
	snapshot internalstoragepolicy.Snapshot
	input    internalstoragepolicy.SetInput
	audit    []internalstoragepolicy.AuditEvent
	sets     int
	lists    int
}

func (fake *storagePolicyStoreFake) SetStoragePolicy(_ context.Context, _ string, _ *authn.VerifiedPrincipal, input internalstoragepolicy.SetInput) (internalstoragepolicy.Snapshot, error) {
	fake.input, fake.sets = input, fake.sets+1
	return fake.snapshot, nil
}

func (fake *storagePolicyStoreFake) GetStoragePolicy(context.Context, string, *authn.VerifiedPrincipal, string, string) (internalstoragepolicy.Snapshot, error) {
	return fake.snapshot, nil
}

func (fake *storagePolicyStoreFake) ListStoragePolicies(context.Context, string, *authn.VerifiedPrincipal, string, string, int) (postgres.StoragePolicyPage, error) {
	fake.lists++
	return postgres.StoragePolicyPage{StoragePolicies: []internalstoragepolicy.Snapshot{fake.snapshot}, NextStoragePolicyID: fake.snapshot.PolicyID}, nil
}

func (fake *storagePolicyStoreFake) ListStoragePolicyAuditEvents(context.Context, string, *authn.VerifiedPrincipal, string, string, *time.Time, string, int) (postgres.StoragePolicyAuditPage, error) {
	return postgres.StoragePolicyAuditPage{Events: fake.audit}, nil
}

func TestStoragePolicyHTTPAuthorityLifecycleAndUserBoundary(t *testing.T) {
	now := time.Date(2026, time.September, 5, 3, 0, 0, 0, time.UTC)
	store := &storagePolicyStoreFake{snapshot: internalstoragepolicy.Snapshot{
		Scope:    internalstoragepolicy.Scope{TenantID: "tenant-alpha", ProjectID: "project-alpha"},
		PolicyID: "storage-standard", PolicyName: "storage-standard",
		UserSummary: "20 GiB managed workspace", WorkspaceType: "managed-volume",
		WorkspaceCapacityBytes: 21474836480, CleanupOnLeaseTermination: true, AllowWorkspaceReuse: true,
		ResourceVersion: 1, CreatedAt: now, UpdatedAt: now,
	}, audit: []internalstoragepolicy.AuditEvent{{
		Scope:   internalstoragepolicy.Scope{TenantID: "tenant-alpha", ProjectID: "project-alpha"},
		EventID: "event-storage-set", OperationID: "operation-storage-set", Actor: "sha256:" + strings.Repeat("a", 64),
		Action: "storage-policy.set", PolicyID: "storage-standard", PolicyResourceVersion: 1,
		Result: "succeeded", RequestID: "request-storage-set", OccurredAt: now,
	}}}
	verifier := &environmentProfileVerifierFake{}
	handler, err := NewStoragePolicyHTTPServer(verifier, store)
	if err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodPut, "/v1/admin/tenants/tenant-alpha/projects/project-alpha/storage-policies/storage-standard", strings.NewReader(`{"expectedResourceVersion":"0","policyName":"storage-standard","userSummary":"20 GiB managed workspace","workspaceType":"managed-volume","workspaceCapacityBytes":21474836480,"retentionSeconds":0,"cleanupOnLeaseTermination":true,"allowWorkspaceReuse":true}`))
	request.Header.Set("Authorization", "Bearer admin-token")
	request.Header.Set("X-Request-ID", "request-storage-set")
	request.Header.Set("Idempotency-Key", "storage-set-key-0001")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	policy, decodeErr := platformv1alpha1.DecodeStoragePolicyResponseJSON(response.Body.Bytes())
	if response.Code != http.StatusOK || decodeErr != nil || policy.Value.Spec.WorkspaceCapacityBytes != 21474836480 || store.sets != 1 || store.input.ExpectedResourceVersion != 0 {
		t.Fatalf("status=%d decodeErr=%v body=%s store=%#v", response.Code, decodeErr, response.Body.String(), store)
	}

	request = httptest.NewRequest(http.MethodGet, "/v1/admin/tenants/tenant-alpha/projects/project-alpha/storage-policies?pageSize=50", nil)
	request.Header.Set("Authorization", "Bearer admin-token")
	request.Header.Set("X-Request-ID", "request-storage-list")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	page, decodeErr := platformv1alpha1.DecodeStoragePolicyPageResponseJSON(response.Body.Bytes())
	if response.Code != http.StatusOK || decodeErr != nil || len(page.Value.StoragePolicies) != 1 || page.Value.NextPageToken == "" || store.lists != 1 {
		t.Fatalf("status=%d decodeErr=%v body=%s", response.Code, decodeErr, response.Body.String())
	}

	request = httptest.NewRequest(http.MethodGet, "/v1/admin/tenants/tenant-alpha/projects/project-alpha/storage-policies/storage-standard/audit-events?pageSize=50", nil)
	request.Header.Set("Authorization", "Bearer admin-token")
	request.Header.Set("X-Request-ID", "request-storage-audit")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	audit, decodeErr := platformv1alpha1.DecodeAdminAuditEventPageResponseJSON(response.Body.Bytes())
	if response.Code != http.StatusOK || decodeErr != nil || len(audit.Value.Events) != 1 || audit.Value.Events[0].ResourceKind != "StoragePolicy" {
		t.Fatalf("status=%d decodeErr=%v body=%s", response.Code, decodeErr, response.Body.String())
	}

	verifier.failPermission = "storage-policies.get"
	request = httptest.NewRequest(http.MethodGet, "/v1/admin/tenants/tenant-alpha/projects/project-alpha/storage-policies/storage-standard", nil)
	request.Header.Set("Authorization", "Bearer user-token")
	request.Header.Set("X-Request-ID", "request-storage-user-denied")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden || !HandlesStoragePolicyPath(request.URL.Path) {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}
