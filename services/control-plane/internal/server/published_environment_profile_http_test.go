package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	platformv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/platform/v1alpha1"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authn"
	internalenvironmentprofile "github.com/hxp0618/cloud-agents/services/control-plane/internal/environmentprofile"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/store/postgres"
)

type publishedEnvironmentProfileStoreFake struct {
	principal *authn.VerifiedPrincipal
	after     string
	listed    int
}

func (fake *publishedEnvironmentProfileStoreFake) ListPublishedEnvironmentProfiles(_ context.Context, _ string, principal *authn.VerifiedPrincipal, _ string, after string, _ int) (postgres.PublishedEnvironmentProfilePage, error) {
	fake.principal, fake.after = principal, after
	fake.listed++
	return postgres.PublishedEnvironmentProfilePage{
		EnvironmentProfiles: []internalenvironmentprofile.Summary{{
			Scope:             internalenvironmentprofile.Scope{TenantID: "tenant-alpha", ProjectID: "project-alpha"},
			ProfileVersionUID: "ep-alpha-v1", ProfileID: "profile-alpha", ProfileName: "profile-alpha",
			Version: 1, Description: "Codex and Claude development", ProviderKinds: []string{"codex", "claudeAgent"},
			CPULimitMillis: 2000, MemoryLimitBytes: 4294967296, StorageSummary: "20 GiB managed workspace", NetworkSummary: "Public internet access",
		}},
		NextProfileVersionID: "ep-alpha-v1",
	}, nil
}

func TestPublishedEnvironmentProfileHTTPReturnsOnlySafeSummaries(t *testing.T) {
	store := &publishedEnvironmentProfileStoreFake{}
	verifier := &environmentProfileVerifierFake{}
	handler, err := NewPublishedEnvironmentProfileHTTPServer(verifier, store)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/v1/tenants/tenant-alpha/projects/project-alpha/environment-profiles?pageSize=50", nil)
	request.Header.Set("Authorization", "Bearer user-token")
	request.Header.Set("X-Request-ID", "request-profile-summary-list")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	page, decodeErr := platformv1alpha1.DecodeEnvironmentProfileSummaryPageResponseJSON(response.Body.Bytes())
	if response.Code != http.StatusOK || decodeErr != nil || len(page.Value.EnvironmentProfiles) != 1 || store.listed != 1 || store.principal == nil {
		t.Fatalf("status=%d decodeErr=%v body=%s store=%#v", response.Code, decodeErr, response.Body.String(), store)
	}
	profile := page.Value.EnvironmentProfiles[0]
	if profile.Status != "published" || profile.Availability != "available" || profile.ProjectRef.ID != "project-alpha" || profile.StorageSummary != "20 GiB managed workspace" || page.Value.NextPageToken == "" {
		t.Fatalf("profile=%#v page=%#v", profile, page.Value)
	}
	for _, forbidden := range []string{"targetRef", "credentialRef", "releaseDigest", "storagePolicyRef", "networkPolicyRef", "endpoint"} {
		if strings.Contains(response.Body.String(), forbidden) {
			t.Fatalf("response contains forbidden field %q: %s", forbidden, response.Body.String())
		}
	}
	permissions := []string{verifier.requests[0].RequiredPermission, verifier.requests[1].RequiredPermission}
	if strings.Join(permissions, ",") != "projects.get,environment-profiles.list" {
		t.Fatalf("permissions=%v", permissions)
	}

	next := httptest.NewRequest(http.MethodGet, "/v1/tenants/tenant-alpha/projects/project-alpha/environment-profiles?pageSize=50&pageToken="+page.Value.NextPageToken, nil)
	next.Header.Set("Authorization", "Bearer user-token")
	next.Header.Set("X-Request-ID", "request-profile-summary-next")
	handler.ServeHTTP(httptest.NewRecorder(), next)
	if store.after != "ep-alpha-v1" {
		t.Fatalf("cursor=%q", store.after)
	}
}

func TestPublishedEnvironmentProfileHTTPRequiresUserScope(t *testing.T) {
	store := &publishedEnvironmentProfileStoreFake{}
	verifier := &environmentProfileVerifierFake{failPermission: "environment-profiles.list"}
	handler, err := NewPublishedEnvironmentProfileHTTPServer(verifier, store)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/v1/tenants/tenant-alpha/projects/project-alpha/environment-profiles?pageSize=50", nil)
	request.Header.Set("Authorization", "Bearer user-token")
	request.Header.Set("X-Request-ID", "request-profile-summary-forbidden")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden || store.listed != 0 || len(verifier.requests) != 2 {
		t.Fatalf("status=%d listed=%d requests=%#v", response.Code, store.listed, verifier.requests)
	}
}
