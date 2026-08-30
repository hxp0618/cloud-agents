package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	commonv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/common/v1alpha1"
	platformv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/platform/v1alpha1"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authn"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/store/postgres"
)

type projectHTTPVerifierFake struct {
	token string
	seen  authn.VerificationRequest
	calls int
}

func (fake *projectHTTPVerifierFake) Verify(token string, request authn.VerificationRequest) (*authn.VerifiedPrincipal, error) {
	fake.calls++
	fake.token = token
	fake.seen = request
	return &authn.VerifiedPrincipal{}, nil
}

type projectHTTPReaderFake struct {
	project  postgres.Project
	page     postgres.ProjectPage
	err      error
	calls    int
	afterUID string
	limit    int
}

type projectHTTPCreatorFake struct {
	result postgres.DurableProjectCreateResult
	err    error
	calls  int
}

func (fake *projectHTTPCreatorFake) Create(_ context.Context, _ *authn.VerifiedPrincipal, _ ManagedAgentCreateProjectRequest) (postgres.DurableProjectCreateResult, error) {
	fake.calls++
	return fake.result, fake.err
}

func (fake *projectHTTPReaderFake) GetProject(_ context.Context, _ string, _ *authn.VerifiedPrincipal, _ string) (postgres.Project, error) {
	fake.calls++
	return fake.project, fake.err
}

func (fake *projectHTTPReaderFake) ListProjects(_ context.Context, _ string, _ *authn.VerifiedPrincipal, _ string, afterUID string, limit int) (postgres.ProjectPage, error) {
	fake.calls++
	fake.afterUID = afterUID
	fake.limit = limit
	return fake.page, fake.err
}

func TestProjectHTTPServerReturnsGeneratedProjectResource(t *testing.T) {
	now := time.Date(2026, time.August, 28, 8, 0, 0, 0, time.UTC)
	verifier := &projectHTTPVerifierFake{}
	reader := &projectHTTPReaderFake{project: postgres.Project{
		UID: "project-alpha", Name: "project-alpha", TenantID: "tenant-alpha", OrganizationID: "organization-alpha",
		DisplayName: "Project Alpha", State: "active", ResourceVersion: 3, CreatedAt: now, UpdatedAt: now,
	}}
	server, err := NewProjectHTTPServer(verifier, reader, &projectHTTPCreatorFake{})
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		"/v1/tenants/tenant-alpha/projects/project-alpha",
		"/v1/managed-host/tenants/tenant-alpha/projects/project-alpha",
	} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		request.Header.Set("Authorization", "Bearer access-token")
		request.Header.Set("X-Request-ID", "request-alpha")
		response := httptest.NewRecorder()
		server.ServeHTTP(response, request)
		if response.Code != http.StatusOK || response.Header().Get("X-Request-ID") != "request-alpha" || response.Header().Get("X-Resource-Version") != "3" {
			t.Fatalf("path=%s status=%d headers=%v body=%s", path, response.Code, response.Header(), response.Body.String())
		}
		if _, err := platformv1alpha1.DecodeProjectResponseJSON(response.Body.Bytes()); err != nil {
			t.Fatalf("path=%s response is not the generated project contract: %v", path, err)
		}
		if verifier.token != "access-token" || verifier.seen.TenantID != "tenant-alpha" || verifier.seen.ResourceID != "project-alpha" || verifier.seen.RequiredPermission != "projects.get" || verifier.seen.ResourceLevel != "project" {
			t.Fatalf("path=%s verification request = %#v token=%q", path, verifier.seen, verifier.token)
		}
	}
	if verifier.calls != 2 || reader.calls != 2 {
		t.Fatalf("verifier calls=%d reader calls=%d", verifier.calls, reader.calls)
	}
}

func TestProjectHTTPServerRejectsDuplicateAuthorizationBeforeReader(t *testing.T) {
	verifier := &projectHTTPVerifierFake{}
	reader := &projectHTTPReaderFake{err: errors.New("must not read")}
	server, err := NewProjectHTTPServer(verifier, reader, &projectHTTPCreatorFake{})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/v1/tenants/tenant-alpha/projects/project-alpha", nil)
	request.Header.Add("Authorization", "Bearer one")
	request.Header.Add("Authorization", "Bearer two")
	request.Header.Set("X-Request-ID", "request-alpha")
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized || verifier.calls != 0 || reader.calls != 0 {
		t.Fatalf("status=%d verifier calls=%d reader calls=%d body=%s", response.Code, verifier.calls, reader.calls, response.Body.String())
	}
}

func TestProjectHTTPServerReturnsPublicProblemContract(t *testing.T) {
	server, err := NewProjectHTTPServer(&projectHTTPVerifierFake{}, &projectHTTPReaderFake{}, &projectHTTPCreatorFake{})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/v1/tenants/tenant-alpha/projects/project-alpha", nil)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest || response.Header().Get("Content-Type") != "application/problem+json" || response.Header().Get("X-Request-ID") != "request-unknown" {
		t.Fatalf("status=%d headers=%v body=%s", response.Code, response.Header(), response.Body.String())
	}
	problem, err := commonv1alpha1.DecodeProblemJSON(response.Body.Bytes())
	if err != nil {
		t.Fatalf("response is not the public Problem contract: %v", err)
	}
	if problem.Status != http.StatusBadRequest || problem.Error.Code != "INVALID_REQUEST" || problem.Error.Retryable || problem.RequestID != "request-unknown" {
		t.Fatalf("problem=%#v", problem)
	}
}

func TestProjectHTTPServerCreatesProjectWithGeneratedResponse(t *testing.T) {
	now := time.Date(2026, time.August, 28, 8, 0, 0, 0, time.UTC)
	verifier := &projectHTTPVerifierFake{}
	reader := &projectHTTPReaderFake{}
	creator := &projectHTTPCreatorFake{result: postgres.DurableProjectCreateResult{DatabaseOutcome: postgres.DatabaseCommitted, Disposition: "created", Project: postgres.Project{
		UID: "project-alpha", Name: "project-alpha", TenantID: "tenant-alpha", OrganizationID: "organization-alpha",
		DisplayName: "Project Alpha", State: "active", ResourceVersion: 3, CreatedAt: now, UpdatedAt: now,
	}}}
	server, err := NewProjectHTTPServer(verifier, reader, creator)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/v1/tenants/tenant-alpha/projects", strings.NewReader(`{"name":"project-alpha","organizationRef":{"namespace":"cloud-agents","kind":"organization","id":"organization-alpha"},"displayName":"Project Alpha"}`))
	request.Header.Set("Authorization", "Bearer access-token")
	request.Header.Set("X-Request-ID", "request-alpha")
	request.Header.Set("Idempotency-Key", "idem-01JZ4X7PGQFHZ2YJR37QRYZ9R2")
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusCreated || response.Header().Get("X-Resource-Version") != "3" || creator.calls != 1 || reader.calls != 0 {
		t.Fatalf("status=%d headers=%v creator calls=%d reader calls=%d body=%s", response.Code, response.Header(), creator.calls, reader.calls, response.Body.String())
	}
	if _, err := platformv1alpha1.DecodeProjectResponseJSON(response.Body.Bytes()); err != nil {
		t.Fatalf("response is not the generated project contract: %v", err)
	}
	if verifier.seen.ResourceLevel != "organization" || verifier.seen.ResourceID != "organization-alpha" || verifier.seen.RequiredPermission != "projects.create" {
		t.Fatalf("verification request = %#v", verifier.seen)
	}
}

func TestProjectHTTPServerListsProjectsAtOrganizationScope(t *testing.T) {
	now := time.Date(2026, time.August, 30, 9, 0, 0, 0, time.UTC)
	verifier := &projectHTTPVerifierFake{}
	reader := &projectHTTPReaderFake{page: postgres.ProjectPage{
		Projects: []postgres.Project{{
			UID: "project-alpha", Name: "project-alpha", TenantID: "tenant-alpha", OrganizationID: "organization-alpha",
			DisplayName: "Project Alpha", State: "active", ResourceVersion: 3, CreatedAt: now, UpdatedAt: now,
		}},
		NextProjectUID: "project-alpha",
	}}
	server, err := NewProjectHTTPServer(verifier, reader, &projectHTTPCreatorFake{})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/v1/tenants/tenant-alpha/projects?organizationId=organization-alpha", nil)
	request.Header.Set("Authorization", "Bearer access-token")
	request.Header.Set("X-Request-ID", "request-list")
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusOK || reader.calls != 1 || reader.afterUID != "" || reader.limit != 50 {
		t.Fatalf("status=%d calls=%d after=%q limit=%d body=%s", response.Code, reader.calls, reader.afterUID, reader.limit, response.Body.String())
	}
	page, err := platformv1alpha1.DecodeProjectPageResponseJSON(response.Body.Bytes())
	if err != nil || len(page.Value.Projects) != 1 || page.Value.NextPageToken == "" {
		t.Fatalf("project page = %#v / %v", page, err)
	}
	if after, ok := decodeProjectPageToken("tenant-alpha", "organization-alpha", page.Value.NextPageToken); !ok || after != "project-alpha" {
		t.Fatalf("next page token = %q / %q / %t", page.Value.NextPageToken, after, ok)
	}
	if verifier.seen != (authn.VerificationRequest{TenantID: "tenant-alpha", ResourceLevel: "organization", ResourceID: "organization-alpha", RequiredPermission: "projects.list"}) {
		t.Fatalf("verification request = %#v", verifier.seen)
	}
}

func TestProjectHTTPServerRejectsInvalidProjectPagination(t *testing.T) {
	reader := &projectHTTPReaderFake{}
	server, err := NewProjectHTTPServer(&projectHTTPVerifierFake{}, reader, &projectHTTPCreatorFake{})
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{
		"/v1/tenants/tenant-alpha/projects",
		"/v1/tenants/tenant-alpha/projects?organizationId=organization-alpha&pageSize=0",
		"/v1/tenants/tenant-alpha/projects?organizationId=organization-alpha&pageSize=1&pageSize=2",
		"/v1/tenants/tenant-alpha/projects?organizationId=organization-alpha&pageToken=short",
		"/v1/tenants/tenant-alpha/projects?organizationId=organization-alpha&unknown=true",
	} {
		request := httptest.NewRequest(http.MethodGet, target, nil)
		request.Header.Set("Authorization", "Bearer access-token")
		request.Header.Set("X-Request-ID", "request-list")
		response := httptest.NewRecorder()
		server.ServeHTTP(response, request)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("%s status=%d body=%s", target, response.Code, response.Body.String())
		}
	}
	if reader.calls != 0 {
		t.Fatalf("invalid pagination reached store %d times", reader.calls)
	}
	token, ok := encodeProjectPageToken("tenant-alpha", "organization-alpha", "project-alpha")
	if !ok {
		t.Fatal("valid project page token was not encoded")
	}
	if _, ok := decodeProjectPageToken("tenant-alpha", "organization-other", token); ok {
		t.Fatal("cross-organization project page token was accepted")
	}
}
