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
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/store/postgres"
)

type myProjectsReaderFake struct {
	page   postgres.MyProjectsPage
	err    error
	cursor postgres.MyProjectsCursor
	limit  int
	calls  int
}

func (fake *myProjectsReaderFake) ListMyProjects(_ context.Context, _ string, _ *authn.VerifiedPrincipal, cursor postgres.MyProjectsCursor, limit int) (postgres.MyProjectsPage, error) {
	fake.calls++
	fake.cursor = cursor
	fake.limit = limit
	return fake.page, fake.err
}

type myProjectsVerifierFake struct {
	seen authn.VerificationRequest
}

func (fake *myProjectsVerifierFake) Verify(token string, request authn.VerificationRequest) (*authn.VerifiedPrincipal, error) {
	fake.seen = request
	if token != "user-token" {
		return nil, errors.New("wrong audience")
	}
	return &authn.VerifiedPrincipal{}, nil
}

func TestMyProjectsHTTPServerListsOnlyStoreSelectedProjects(t *testing.T) {
	now := time.Date(2026, time.October, 8, 9, 0, 0, 0, time.UTC)
	subjectKey := strings.Repeat("a", 64)
	reader := &myProjectsReaderFake{page: postgres.MyProjectsPage{
		Projects: []postgres.Project{{
			UID: "project-alpha", Name: "project-alpha", TenantID: "tenant-alpha", OrganizationID: "organization-alpha",
			DisplayName: "Project Alpha", State: "active", ResourceVersion: 1, CreatedAt: now, UpdatedAt: now,
		}},
		NextProjectUID: "project-alpha", SubjectKey: subjectKey,
	}}
	verifier := &myProjectsVerifierFake{}
	server, err := NewMyProjectsHTTPServer(verifier, reader)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/v1/tenants/tenant-alpha/my-projects?pageSize=1", nil)
	request.Header.Set("Authorization", "Bearer user-token")
	request.Header.Set("X-Request-ID", "request-alpha")
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusOK || reader.calls != 1 || reader.limit != 1 || reader.cursor != (postgres.MyProjectsCursor{}) {
		t.Fatalf("status=%d reader=%#v body=%s", response.Code, reader, response.Body.String())
	}
	if verifier.seen != (authn.VerificationRequest{TenantID: "tenant-alpha", ResourceLevel: "tenant", ResourceID: "tenant-alpha", RequiredPermission: "projects.list"}) {
		t.Fatalf("verification=%#v", verifier.seen)
	}
	page, err := platformv1alpha1.DecodeProjectPageResponseJSON(response.Body.Bytes())
	if err != nil || len(page.Value.Projects) != 1 || page.Value.Projects[0].Metadata.UID != "project-alpha" || page.Value.NextPageToken == "" {
		t.Fatalf("page=%#v err=%v", page, err)
	}
	decodedSubject, after, ok := decodeMyProjectsPageToken("tenant-alpha", page.Value.NextPageToken)
	if !ok || decodedSubject != subjectKey || after != "project-alpha" {
		t.Fatalf("cursor subject=%q after=%q ok=%v", decodedSubject, after, ok)
	}
}

func TestMyProjectsHTTPServerBindsCursorAndRejectsAmbiguousRoutes(t *testing.T) {
	subjectKey := strings.Repeat("b", 64)
	token, ok := encodeMyProjectsPageToken("tenant-alpha", subjectKey, "project-alpha")
	if !ok {
		t.Fatal("could not encode cursor")
	}
	reader := &myProjectsReaderFake{page: postgres.MyProjectsPage{Projects: []postgres.Project{}, SubjectKey: subjectKey}}
	server, err := NewMyProjectsHTTPServer(&myProjectsVerifierFake{}, reader)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/v1/tenants/tenant-alpha/my-projects?pageToken="+token, nil)
	request.Header.Set("Authorization", "Bearer user-token")
	request.Header.Set("X-Request-ID", "request-alpha")
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusOK || reader.cursor != (postgres.MyProjectsCursor{AfterProjectUID: "project-alpha", SubjectKey: subjectKey}) {
		t.Fatalf("status=%d cursor=%#v body=%s", response.Code, reader.cursor, response.Body.String())
	}

	for _, test := range []struct {
		name, target string
		raw          string
		want         int
	}{
		{name: "cross tenant cursor", target: "/v1/tenants/tenant-other/my-projects?pageToken=" + token, want: http.StatusBadRequest},
		{name: "unknown query", target: "/v1/tenants/tenant-alpha/my-projects?organizationId=organization-alpha", want: http.StatusBadRequest},
		{name: "trailing slash", target: "/v1/tenants/tenant-alpha/my-projects/", want: http.StatusNotFound},
		{name: "ambiguous raw path", target: "/v1/tenants/tenant-alpha/my-projects", raw: "/v1/tenants/tenant-alpha%2fmy-projects", want: http.StatusNotFound},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, test.target, nil)
			request.URL.RawPath = test.raw
			request.Header.Set("Authorization", "Bearer user-token")
			request.Header.Set("X-Request-ID", "request-alpha")
			response := httptest.NewRecorder()
			server.ServeHTTP(response, request)
			if response.Code != test.want {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
		})
	}
}

func TestMyProjectsHTTPServerRejectsAdminAudienceAndNilDependencies(t *testing.T) {
	reader := &myProjectsReaderFake{}
	server, err := NewMyProjectsHTTPServer(&myProjectsVerifierFake{}, reader)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/v1/tenants/tenant-alpha/my-projects", nil)
	request.Header.Set("Authorization", "Bearer admin-token")
	request.Header.Set("X-Request-ID", "request-alpha")
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized || reader.calls != 0 {
		t.Fatalf("status=%d calls=%d", response.Code, reader.calls)
	}
	if configured, err := NewMyProjectsHTTPServer(nil, reader); configured != nil || !errors.Is(err, ErrInvalidMyProjectsHTTPServer) {
		t.Fatalf("server=%v err=%v", configured, err)
	}
}
