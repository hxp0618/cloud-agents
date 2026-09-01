package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authn"
	internalmanagedagent "github.com/hxp0618/cloud-agents/services/control-plane/internal/managedagent"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/store/postgres"
)

type managedAgentSessionStoreFake struct {
	snapshot internalmanagedagent.SessionSnapshot
	err      error
	create   int
	close    int
	get      int
	list     int
	page     postgres.ManagedAgentSessionPage
	after    string
	limit    int
}

func (fake *managedAgentSessionStoreFake) CreateManagedAgentSession(_ context.Context, _ string, _ *authn.VerifiedPrincipal, input internalmanagedagent.CreateSessionInput) (internalmanagedagent.SessionSnapshot, error) {
	fake.create++
	if fake.err != nil {
		return internalmanagedagent.SessionSnapshot{}, fake.err
	}
	fake.snapshot.Scope = input.Scope
	fake.snapshot.SessionID = input.SessionID
	fake.snapshot.ProviderKind = input.ProviderKind
	fake.snapshot.EnvironmentLeaseID = input.EnvironmentLeaseID
	return fake.snapshot, nil
}

func (fake *managedAgentSessionStoreFake) CloseManagedAgentSession(_ context.Context, _ string, _ *authn.VerifiedPrincipal, _ internalmanagedagent.CloseSessionInput) (internalmanagedagent.SessionSnapshot, error) {
	fake.close++
	return fake.snapshot, fake.err
}

func (fake *managedAgentSessionStoreFake) GetManagedAgentSession(_ context.Context, _ string, _ *authn.VerifiedPrincipal, _, _ string) (internalmanagedagent.SessionSnapshot, error) {
	fake.get++
	return fake.snapshot, fake.err
}

func (fake *managedAgentSessionStoreFake) ListManagedAgentSessions(_ context.Context, _ string, _ *authn.VerifiedPrincipal, _ string, after string, limit int) (postgres.ManagedAgentSessionPage, error) {
	fake.list++
	fake.after = after
	fake.limit = limit
	return fake.page, fake.err
}

func TestManagedAgentSessionHTTPServerLifecycleRoutes(t *testing.T) {
	now := time.Date(2026, time.August, 29, 8, 0, 0, 0, time.UTC)
	verifier := &projectHTTPVerifierFake{}
	snapshot := internalmanagedagent.SessionSnapshot{Scope: internalmanagedagent.Scope{TenantID: "tenant-alpha", ProjectID: "project-alpha"}, SessionID: "session-alpha", ProviderKind: "codex", EnvironmentLeaseID: "lease-alpha", EnvironmentGeneration: 1, State: internalmanagedagent.SessionActive, Version: 1, CreatedAt: now, UpdatedAt: now}
	store := &managedAgentSessionStoreFake{snapshot: snapshot, page: postgres.ManagedAgentSessionPage{Sessions: []internalmanagedagent.SessionSnapshot{snapshot}, NextSessionID: "session-alpha"}}
	handler, err := NewManagedAgentSessionHTTPServer(verifier, store)
	if err != nil {
		t.Fatal(err)
	}

	create := httptest.NewRequest(http.MethodPost, "/v1/tenants/tenant-alpha/projects/project-alpha/sessions", strings.NewReader(`{"sessionId":"session-alpha","providerKind":"codex","environmentLeaseId":"lease-alpha"}`))
	create.Header.Set("Authorization", "Bearer access-token")
	create.Header.Set("X-Request-ID", "request-alpha")
	create.Header.Set("Idempotency-Key", "idem-01JZ4X7PGQFHZ2YJR37QRYZ9R2")
	created := httptest.NewRecorder()
	handler.ServeHTTP(created, create)
	if created.Code != http.StatusCreated || store.create != 1 || verifier.seen.RequiredPermission != "projects.act" {
		t.Fatalf("create status=%d calls=%d verification=%#v body=%s", created.Code, store.create, verifier.seen, created.Body.String())
	}
	if !strings.Contains(created.Body.String(), `"kind":"Session"`) || created.Header().Get("X-Resource-Version") != "1" || created.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("create response=%s headers=%v", created.Body.String(), created.Header())
	}

	get := httptest.NewRequest(http.MethodGet, "/v1/tenants/tenant-alpha/projects/project-alpha/sessions/session-alpha", nil)
	get.Header.Set("Authorization", "Bearer access-token")
	get.Header.Set("X-Request-ID", "request-get")
	got := httptest.NewRecorder()
	handler.ServeHTTP(got, get)
	if got.Code != http.StatusOK || store.get != 1 || verifier.seen.RequiredPermission != "projects.get" {
		t.Fatalf("get status=%d calls=%d verification=%#v body=%s", got.Code, store.get, verifier.seen, got.Body.String())
	}

	list := httptest.NewRequest(http.MethodGet, "/v1/tenants/tenant-alpha/projects/project-alpha/sessions?pageSize=1", nil)
	list.Header.Set("Authorization", "Bearer access-token")
	list.Header.Set("X-Request-ID", "request-list")
	listed := httptest.NewRecorder()
	handler.ServeHTTP(listed, list)
	var page managedAgentSessionPageResource
	if err := json.Unmarshal(listed.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if listed.Code != http.StatusOK || store.list != 1 || store.limit != 1 || store.after != "" || verifier.seen.RequiredPermission != "projects.get" || len(page.Sessions) != 1 || page.NextPageToken == "" {
		t.Fatalf("list status=%d calls=%d after=%q limit=%d verification=%#v page=%#v", listed.Code, store.list, store.after, store.limit, verifier.seen, page)
	}
	wrongProjectToken, ok := encodeManagedAgentSessionPageToken("tenant-alpha", "project-other", "session-alpha")
	if !ok {
		t.Fatal("failed to encode wrong-project token")
	}
	wrongProject := httptest.NewRequest(http.MethodGet, "/v1/tenants/tenant-alpha/projects/project-alpha/sessions?pageToken="+wrongProjectToken, nil)
	wrongProject.Header.Set("Authorization", "Bearer access-token")
	wrongProject.Header.Set("X-Request-ID", "request-list-wrong")
	rejected := httptest.NewRecorder()
	handler.ServeHTTP(rejected, wrongProject)
	if rejected.Code != http.StatusBadRequest || store.list != 1 {
		t.Fatalf("wrong-project token status=%d calls=%d body=%s", rejected.Code, store.list, rejected.Body.String())
	}

	closeRequest := httptest.NewRequest(http.MethodPost, "/v1/tenants/tenant-alpha/projects/project-alpha/sessions/session-alpha:close", nil)
	closeRequest.Header.Set("Authorization", "Bearer access-token")
	closeRequest.Header.Set("X-Request-ID", "request-close")
	closeRequest.Header.Set("Idempotency-Key", "idem-01JZ4X7PGQFHZ2YJR37QRYZ9R3")
	closed := httptest.NewRecorder()
	handler.ServeHTTP(closed, closeRequest)
	if closed.Code != http.StatusOK || store.close != 1 || verifier.seen.RequiredPermission != "projects.act" {
		t.Fatalf("close status=%d calls=%d verification=%#v body=%s", closed.Code, store.close, verifier.seen, closed.Body.String())
	}
}

func TestManagedAgentSessionHTTPServerMapsStoreErrors(t *testing.T) {
	verifier := &projectHTTPVerifierFake{}
	store := &managedAgentSessionStoreFake{err: errors.New("store failure")}
	handler, err := NewManagedAgentSessionHTTPServer(verifier, store)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/v1/tenants/tenant-alpha/projects/project-alpha/sessions/session-alpha", nil)
	request.Header.Set("Authorization", "Bearer access-token")
	request.Header.Set("X-Request-ID", "request-alpha")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusInternalServerError || store.get != 1 {
		t.Fatalf("status=%d calls=%d body=%s", response.Code, store.get, response.Body.String())
	}
}

func TestManagedAgentSessionHTTPServerRejectsInvalidPublicInputs(t *testing.T) {
	tests := []struct {
		name        string
		path        string
		requestID   string
		idempotency string
		body        string
	}{
		{name: "request id", path: "/v1/tenants/tenant-alpha/projects/project-alpha/sessions", requestID: "request:invalid", idempotency: "idem-01JZ4X7PGQFHZ2YJR37QRYZ9R2", body: `{"sessionId":"session-alpha","providerKind":"codex","environmentLeaseId":"lease-alpha"}`},
		{name: "tenant identifier", path: "/v1/tenants/-tenant/projects/project-alpha/sessions", requestID: "request-alpha", idempotency: "idem-01JZ4X7PGQFHZ2YJR37QRYZ9R2", body: `{"sessionId":"session-alpha","providerKind":"codex","environmentLeaseId":"lease-alpha"}`},
		{name: "idempotency key", path: "/v1/tenants/tenant-alpha/projects/project-alpha/sessions", requestID: "request-alpha", idempotency: "short", body: `{"sessionId":"session-alpha","providerKind":"codex","environmentLeaseId":"lease-alpha"}`},
		{name: "session identifier", path: "/v1/tenants/tenant-alpha/projects/project-alpha/sessions", requestID: "request-alpha", idempotency: "idem-01JZ4X7PGQFHZ2YJR37QRYZ9R2", body: `{"sessionId":"-session","providerKind":"codex","environmentLeaseId":"lease-alpha"}`},
		{name: "provider length", path: "/v1/tenants/tenant-alpha/projects/project-alpha/sessions", requestID: "request-alpha", idempotency: "idem-01JZ4X7PGQFHZ2YJR37QRYZ9R2", body: `{"sessionId":"session-alpha","providerKind":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","environmentLeaseId":"lease-alpha"}`},
		{name: "environment lease identifier", path: "/v1/tenants/tenant-alpha/projects/project-alpha/sessions", requestID: "request-alpha", idempotency: "idem-01JZ4X7PGQFHZ2YJR37QRYZ9R2", body: `{"sessionId":"session-alpha","providerKind":"codex","environmentLeaseId":"-lease"}`},
		{name: "unknown field", path: "/v1/tenants/tenant-alpha/projects/project-alpha/sessions", requestID: "request-alpha", idempotency: "idem-01JZ4X7PGQFHZ2YJR37QRYZ9R2", body: `{"sessionId":"session-alpha","providerKind":"codex","environmentLeaseId":"lease-alpha","extra":true}`},
		{name: "duplicate field", path: "/v1/tenants/tenant-alpha/projects/project-alpha/sessions", requestID: "request-alpha", idempotency: "idem-01JZ4X7PGQFHZ2YJR37QRYZ9R2", body: `{"sessionId":"session-alpha","sessionId":"session-beta","providerKind":"codex","environmentLeaseId":"lease-alpha"}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			verifier := &projectHTTPVerifierFake{}
			store := &managedAgentSessionStoreFake{}
			handler, err := NewManagedAgentSessionHTTPServer(verifier, store)
			if err != nil {
				t.Fatal(err)
			}
			request := httptest.NewRequest(http.MethodPost, test.path, strings.NewReader(test.body))
			request.Header.Set("Authorization", "Bearer access-token")
			request.Header.Set("X-Request-ID", test.requestID)
			request.Header.Set("Idempotency-Key", test.idempotency)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusBadRequest || verifier.calls != 0 || store.create != 0 {
				t.Fatalf("status=%d verifierCalls=%d storeCreates=%d body=%s", response.Code, verifier.calls, store.create, response.Body.String())
			}
			if response.Header().Get("X-Request-ID") != publicFallbackRequestID && test.requestID == "request:invalid" {
				t.Fatalf("invalid request id was echoed: %q", response.Header().Get("X-Request-ID"))
			}
		})
	}
}
