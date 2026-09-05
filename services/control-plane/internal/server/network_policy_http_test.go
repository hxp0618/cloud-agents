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
	internalnetworkpolicy "github.com/hxp0618/cloud-agents/services/control-plane/internal/networkpolicy"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/store/postgres"
)

type networkPolicyStoreFake struct {
	snapshot internalnetworkpolicy.Snapshot
	input    internalnetworkpolicy.SetInput
	audit    []internalnetworkpolicy.AuditEvent
	sets     int
	lists    int
}

func (fake *networkPolicyStoreFake) SetNetworkPolicy(_ context.Context, _ string, _ *authn.VerifiedPrincipal, input internalnetworkpolicy.SetInput) (internalnetworkpolicy.Snapshot, error) {
	fake.input, fake.sets = input, fake.sets+1
	return fake.snapshot, nil
}

func (fake *networkPolicyStoreFake) GetNetworkPolicy(context.Context, string, *authn.VerifiedPrincipal, string, string) (internalnetworkpolicy.Snapshot, error) {
	return fake.snapshot, nil
}

func (fake *networkPolicyStoreFake) ListNetworkPolicies(context.Context, string, *authn.VerifiedPrincipal, string, string, int) (postgres.NetworkPolicyPage, error) {
	fake.lists++
	return postgres.NetworkPolicyPage{NetworkPolicies: []internalnetworkpolicy.Snapshot{fake.snapshot}, NextNetworkPolicyID: fake.snapshot.PolicyID}, nil
}

func (fake *networkPolicyStoreFake) ListNetworkPolicyAuditEvents(context.Context, string, *authn.VerifiedPrincipal, string, string, *time.Time, string, int) (postgres.NetworkPolicyAuditPage, error) {
	return postgres.NetworkPolicyAuditPage{Events: fake.audit}, nil
}

func TestNetworkPolicyHTTPAuthorityLifecycleAndUserBoundary(t *testing.T) {
	now := time.Date(2026, time.September, 5, 3, 0, 0, 0, time.UTC)
	store := &networkPolicyStoreFake{snapshot: internalnetworkpolicy.Snapshot{
		Scope:    internalnetworkpolicy.Scope{TenantID: "tenant-alpha", ProjectID: "project-alpha"},
		PolicyID: "network-standard", PolicyName: "network-standard",
		UserSummary: "Public internet access", DefaultEgress: "public",
		ResourceVersion: 1, CreatedAt: now, UpdatedAt: now,
	}, audit: []internalnetworkpolicy.AuditEvent{{
		Scope:   internalnetworkpolicy.Scope{TenantID: "tenant-alpha", ProjectID: "project-alpha"},
		EventID: "event-network-set", OperationID: "operation-network-set", Actor: "sha256:" + strings.Repeat("a", 64),
		Action: "network-policy.set", PolicyID: "network-standard", PolicyResourceVersion: 1,
		Result: "succeeded", RequestID: "request-network-set", OccurredAt: now,
	}}}
	verifier := &environmentProfileVerifierFake{}
	handler, err := NewNetworkPolicyHTTPServer(verifier, store)
	if err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodPut, "/v1/admin/tenants/tenant-alpha/projects/project-alpha/network-policies/network-standard", strings.NewReader(`{"expectedResourceVersion":"0","policyName":"network-standard","userSummary":"Public internet access","defaultEgress":"public","ingressEnabled":false,"previewEnabled":false}`))
	request.Header.Set("Authorization", "Bearer admin-token")
	request.Header.Set("X-Request-ID", "request-network-set")
	request.Header.Set("Idempotency-Key", "network-set-key-0001")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	policy, decodeErr := platformv1alpha1.DecodeNetworkPolicyResponseJSON(response.Body.Bytes())
	if response.Code != http.StatusOK || decodeErr != nil || policy.Value.Spec.DefaultEgress != "public" || store.sets != 1 || store.input.ExpectedResourceVersion != 0 {
		t.Fatalf("status=%d decodeErr=%v body=%s store=%#v", response.Code, decodeErr, response.Body.String(), store)
	}

	request = httptest.NewRequest(http.MethodGet, "/v1/admin/tenants/tenant-alpha/projects/project-alpha/network-policies?pageSize=50", nil)
	request.Header.Set("Authorization", "Bearer admin-token")
	request.Header.Set("X-Request-ID", "request-network-list")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	page, decodeErr := platformv1alpha1.DecodeNetworkPolicyPageResponseJSON(response.Body.Bytes())
	if response.Code != http.StatusOK || decodeErr != nil || len(page.Value.NetworkPolicies) != 1 || page.Value.NextPageToken == "" || store.lists != 1 {
		t.Fatalf("status=%d decodeErr=%v body=%s", response.Code, decodeErr, response.Body.String())
	}

	request = httptest.NewRequest(http.MethodGet, "/v1/admin/tenants/tenant-alpha/projects/project-alpha/network-policies/network-standard/audit-events?pageSize=50", nil)
	request.Header.Set("Authorization", "Bearer admin-token")
	request.Header.Set("X-Request-ID", "request-network-audit")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	audit, decodeErr := platformv1alpha1.DecodeAdminAuditEventPageResponseJSON(response.Body.Bytes())
	if response.Code != http.StatusOK || decodeErr != nil || len(audit.Value.Events) != 1 || audit.Value.Events[0].ResourceKind != "NetworkPolicy" {
		t.Fatalf("status=%d decodeErr=%v body=%s", response.Code, decodeErr, response.Body.String())
	}

	verifier.failPermission = "network-policies.get"
	request = httptest.NewRequest(http.MethodGet, "/v1/admin/tenants/tenant-alpha/projects/project-alpha/network-policies/network-standard", nil)
	request.Header.Set("Authorization", "Bearer user-token")
	request.Header.Set("X-Request-ID", "request-network-user-denied")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden || !HandlesNetworkPolicyPath(request.URL.Path) {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}
