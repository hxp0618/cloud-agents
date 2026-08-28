package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

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
	project postgres.Project
	err     error
	calls   int
}

func (fake *projectHTTPReaderFake) GetProject(_ context.Context, _ string, _ *authn.VerifiedPrincipal, _ string) (postgres.Project, error) {
	fake.calls++
	return fake.project, fake.err
}

func TestProjectHTTPServerReturnsGeneratedProjectResource(t *testing.T) {
	now := time.Date(2026, time.August, 28, 8, 0, 0, 0, time.UTC)
	verifier := &projectHTTPVerifierFake{}
	reader := &projectHTTPReaderFake{project: postgres.Project{
		UID: "project-alpha", Name: "project-alpha", TenantID: "tenant-alpha", OrganizationID: "organization-alpha",
		DisplayName: "Project Alpha", State: "active", ResourceVersion: 3, CreatedAt: now, UpdatedAt: now,
	}}
	server, err := NewProjectHTTPServer(verifier, reader)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/v1/tenants/tenant-alpha/projects/project-alpha", nil)
	request.Header.Set("Authorization", "Bearer access-token")
	request.Header.Set("X-Request-ID", "request-alpha")
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Header().Get("X-Request-ID") != "request-alpha" || response.Header().Get("X-Resource-Version") != "3" || reader.calls != 1 {
		t.Fatalf("status=%d headers=%v calls=%d body=%s", response.Code, response.Header(), reader.calls, response.Body.String())
	}
	if _, err := platformv1alpha1.DecodeProjectResponseJSON(response.Body.Bytes()); err != nil {
		t.Fatalf("response is not the generated project contract: %v", err)
	}
	if verifier.calls != 1 || verifier.token != "access-token" || verifier.seen.TenantID != "tenant-alpha" || verifier.seen.ResourceID != "project-alpha" || verifier.seen.RequiredPermission != "projects.get" || verifier.seen.ResourceLevel != "project" {
		t.Fatalf("verification request = %#v calls=%d token=%q", verifier.seen, verifier.calls, verifier.token)
	}
}

func TestProjectHTTPServerRejectsDuplicateAuthorizationBeforeReader(t *testing.T) {
	verifier := &projectHTTPVerifierFake{}
	reader := &projectHTTPReaderFake{err: errors.New("must not read")}
	server, err := NewProjectHTTPServer(verifier, reader)
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
