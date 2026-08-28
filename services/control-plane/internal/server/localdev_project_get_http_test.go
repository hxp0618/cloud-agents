//go:build localdev

package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	commonv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/common/v1alpha1"
	platformv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/platform/v1alpha1"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authn"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/store/postgres"
)

type localProjectReaderFake struct {
	project postgres.Project
	err     error
	calls   int
}

func openapiProject(raw []byte) (platformv1alpha1.Project, error) {
	response, err := platformv1alpha1.DecodeProjectResponseJSON(raw)
	if err != nil {
		return platformv1alpha1.Project{}, err
	}
	if len(response.Unknown) != 0 {
		return platformv1alpha1.Project{}, commonv1alpha1.ContractError("UNKNOWN_FIELDS", "/")
	}
	return response.Value, nil
}

func (fake *localProjectReaderFake) GetProject(_ context.Context, _ string, _ *authn.VerifiedPrincipal, _ string) (postgres.Project, error) {
	fake.calls++
	return fake.project, fake.err
}

func TestLocalProjectGetHTTPServerReturnsContractResource(t *testing.T) {
	now := time.Date(2026, time.August, 28, 8, 0, 0, 0, time.UTC)
	verifier, token := localVerifierAndToken(t, now)
	reader := &localProjectReaderFake{project: postgres.Project{
		UID: "project-alpha", Name: "project-alpha", TenantID: "tenant-alpha", OrganizationID: "organization-alpha",
		DisplayName: "Project Alpha", State: "active", ResourceVersion: 3, CreatedAt: now, UpdatedAt: now,
	}}
	server, err := NewLocalProjectGetHTTPServer(verifier, reader)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/v1/tenants/tenant-alpha/projects/project-alpha", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("X-Request-ID", "request-alpha")
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Header().Get("X-Resource-Version") != "3" || reader.calls != 1 {
		t.Fatalf("status=%d headers=%v calls=%d body=%s", response.Code, response.Header(), reader.calls, response.Body.String())
	}
	if _, err := openapiProject(response.Body.Bytes()); err != nil {
		t.Fatalf("response is not the generated project contract: %v", err)
	}
}

func TestLocalProjectGetHTTPServerRejectsBeforeReader(t *testing.T) {
	verifier, _ := localVerifierAndToken(t, testLocalHTTPNow())
	reader := &localProjectReaderFake{}
	server, err := NewLocalProjectGetHTTPServer(verifier, reader)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/v1/tenants/tenant-alpha/projects/project-alpha", nil)
	request.Header.Set("X-Request-ID", "request-alpha")
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized || reader.calls != 0 {
		t.Fatalf("status=%d calls=%d body=%s", response.Code, reader.calls, response.Body.String())
	}
}

func TestLocalProjectGetHTTPServerMapsReadErrors(t *testing.T) {
	verifier, token := localVerifierAndToken(t, testLocalHTTPNow())
	reader := &localProjectReaderFake{err: postgres.ErrProjectNotFound}
	server, err := NewLocalProjectGetHTTPServer(verifier, reader)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/v1/tenants/tenant-alpha/projects/project-alpha", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("X-Request-ID", "request-alpha")
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	reader.err = errors.New("database secret")
	response = httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusInternalServerError || reader.calls != 2 {
		t.Fatalf("status=%d calls=%d body=%s", response.Code, reader.calls, response.Body.String())
	}
}
