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
	internalprojectleasequota "github.com/hxp0618/cloud-agents/services/control-plane/internal/projectleasequota"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/store/postgres"
)

type projectLeaseQuotaStoreFake struct {
	snapshot internalprojectleasequota.Snapshot
	input    internalprojectleasequota.SetInput
	audit    []internalprojectleasequota.AuditEvent
	sets     int
	gets     int
}

func (fake *projectLeaseQuotaStoreFake) SetProjectLeaseQuota(_ context.Context, _ string, _ *authn.VerifiedPrincipal, input internalprojectleasequota.SetInput) (internalprojectleasequota.Snapshot, error) {
	fake.input, fake.sets = input, fake.sets+1
	return fake.snapshot, nil
}

func (fake *projectLeaseQuotaStoreFake) GetProjectLeaseQuota(context.Context, string, *authn.VerifiedPrincipal, string) (internalprojectleasequota.Snapshot, error) {
	fake.gets++
	return fake.snapshot, nil
}

func (fake *projectLeaseQuotaStoreFake) ListProjectLeaseQuotaAuditEvents(context.Context, string, *authn.VerifiedPrincipal, string, *time.Time, string, int) (postgres.ProjectLeaseQuotaAuditPage, error) {
	return postgres.ProjectLeaseQuotaAuditPage{Events: fake.audit}, nil
}

func TestProjectLeaseQuotaHTTPAdminUserAndAuditBoundaries(t *testing.T) {
	now := time.Date(2026, time.September, 5, 1, 0, 0, 0, time.UTC)
	store := &projectLeaseQuotaStoreFake{snapshot: internalprojectleasequota.Snapshot{
		Scope:   internalprojectleasequota.Scope{TenantID: "tenant-alpha", ProjectID: "project-alpha"},
		QuotaID: "quota-project-alpha", QuotaName: "project-lease-quota",
		MaxConcurrentLeases: 2, MaxCPUMillis: 4000, MaxMemoryBytes: 8589934592, MaxLeaseTTLSeconds: 3600,
		ActiveLeases: 1, UsedCPUMillis: 2000, UsedMemoryBytes: 4294967296,
		ResourceVersion: 1, CreatedAt: now, UpdatedAt: now,
	}, audit: []internalprojectleasequota.AuditEvent{{
		Scope:   internalprojectleasequota.Scope{TenantID: "tenant-alpha", ProjectID: "project-alpha"},
		EventID: "event-quota-alpha", OperationID: "operation-quota-alpha", Actor: "sha256:" + strings.Repeat("a", 64),
		Action: "quota.set", QuotaID: "quota-project-alpha", QuotaResourceVersion: 1,
		Result: "succeeded", RequestID: "request-quota-set", OccurredAt: now,
	}}}
	verifier := &environmentProfileVerifierFake{}
	handler, err := NewProjectLeaseQuotaHTTPServer(verifier, store)
	if err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodPut, "/v1/admin/tenants/tenant-alpha/projects/project-alpha/lease-quota", strings.NewReader(`{"expectedResourceVersion":"0","maxConcurrentLeases":2,"maxCpuMillis":4000,"maxMemoryBytes":8589934592,"maxLeaseTtlSeconds":3600}`))
	request.Header.Set("Authorization", "Bearer admin-token")
	request.Header.Set("X-Request-ID", "request-quota-set")
	request.Header.Set("Idempotency-Key", "quota-set-key-0001")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	quota, decodeErr := platformv1alpha1.DecodeProjectLeaseQuotaResponseJSON(response.Body.Bytes())
	if response.Code != http.StatusOK || decodeErr != nil || quota.Value.Status.ActiveLeases != 1 || store.sets != 1 || store.input.ExpectedResourceVersion != 0 {
		t.Fatalf("status=%d decodeErr=%v body=%s store=%#v", response.Code, decodeErr, response.Body.String(), store)
	}

	request = httptest.NewRequest(http.MethodGet, "/v1/tenants/tenant-alpha/projects/project-alpha/lease-quota", nil)
	request.Header.Set("Authorization", "Bearer user-token")
	request.Header.Set("X-Request-ID", "request-user-quota")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	summary, decodeErr := platformv1alpha1.DecodeProjectLeaseQuotaSummaryResponseJSON(response.Body.Bytes())
	if response.Code != http.StatusOK || decodeErr != nil || summary.Value.MaxConcurrentLeases != 2 || strings.Contains(response.Body.String(), "credential") || strings.Contains(response.Body.String(), "target") || strings.Contains(response.Body.String(), "release") {
		t.Fatalf("status=%d decodeErr=%v body=%s", response.Code, decodeErr, response.Body.String())
	}

	request = httptest.NewRequest(http.MethodGet, "/v1/admin/tenants/tenant-alpha/projects/project-alpha/lease-quota/audit-events?pageSize=50", nil)
	request.Header.Set("Authorization", "Bearer admin-token")
	request.Header.Set("X-Request-ID", "request-quota-audit")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	audit, decodeErr := platformv1alpha1.DecodeAdminAuditEventPageResponseJSON(response.Body.Bytes())
	if response.Code != http.StatusOK || decodeErr != nil || len(audit.Value.Events) != 1 || audit.Value.Events[0].Action != "quota.set" {
		t.Fatalf("status=%d decodeErr=%v body=%s", response.Code, decodeErr, response.Body.String())
	}

	verifier.failPermission = "quotas.get"
	gets := store.gets
	request = httptest.NewRequest(http.MethodGet, "/v1/admin/tenants/tenant-alpha/projects/project-alpha/lease-quota", nil)
	request.Header.Set("Authorization", "Bearer user-token")
	request.Header.Set("X-Request-ID", "request-user-admin-quota")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden || store.gets != gets || !HandlesProjectLeaseQuotaPath(request.URL.Path) {
		t.Fatalf("status=%d gets=%d", response.Code, store.gets)
	}
}
