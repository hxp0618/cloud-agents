package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authn"
	internalmanagedagent "github.com/hxp0618/cloud-agents/services/control-plane/internal/managedagent"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/store/postgres"
)

type managedAgentTurnStoreFake struct {
	snapshot internalmanagedagent.TurnSnapshot
	create   int
	get      int
	list     int
	page     postgres.ManagedAgentTurnPage
	after    string
	limit    int
}

func (fake *managedAgentTurnStoreFake) CreateManagedAgentTurn(_ context.Context, _ string, _ *authn.VerifiedPrincipal, input internalmanagedagent.CreateTurnInput) (internalmanagedagent.TurnSnapshot, error) {
	fake.create++
	fake.snapshot.Scope = input.Scope
	fake.snapshot.SessionID = input.SessionID
	fake.snapshot.TurnID = input.TurnID
	fake.snapshot.State = internalmanagedagent.TurnQueued
	return fake.snapshot, nil
}

func (fake *managedAgentTurnStoreFake) GetManagedAgentTurn(_ context.Context, _ string, _ *authn.VerifiedPrincipal, _, _, _ string) (internalmanagedagent.TurnSnapshot, error) {
	fake.get++
	return fake.snapshot, nil
}

func (fake *managedAgentTurnStoreFake) ListManagedAgentTurns(_ context.Context, _ string, _ *authn.VerifiedPrincipal, _, _ string, after string, limit int) (postgres.ManagedAgentTurnPage, error) {
	fake.list++
	fake.after = after
	fake.limit = limit
	return fake.page, nil
}

func TestManagedAgentTurnHTTPServerLifecycleRoutes(t *testing.T) {
	now := time.Date(2026, time.August, 29, 8, 0, 0, 0, time.UTC)
	verifier := &projectHTTPVerifierFake{}
	snapshot := internalmanagedagent.TurnSnapshot{Scope: internalmanagedagent.Scope{TenantID: "tenant-alpha", ProjectID: "project-alpha"}, SessionID: "session-alpha", TurnID: "turn-alpha", InputDigest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", State: internalmanagedagent.TurnQueued, Version: 1, CreatedAt: now, UpdatedAt: now}
	store := &managedAgentTurnStoreFake{snapshot: snapshot, page: postgres.ManagedAgentTurnPage{Turns: []internalmanagedagent.TurnSnapshot{snapshot}, NextTurnID: "turn-alpha"}}
	handler, err := NewManagedAgentTurnHTTPServer(verifier, store)
	if err != nil {
		t.Fatal(err)
	}

	create := httptest.NewRequest(http.MethodPost, "/v1/tenants/tenant-alpha/projects/project-alpha/sessions/session-alpha/turns", strings.NewReader(`{"turnId":"turn-alpha","inputText":"hello"}`))
	create.Header.Set("Authorization", "Bearer access-token")
	create.Header.Set("X-Request-ID", "request-alpha")
	create.Header.Set("Idempotency-Key", "idem-01JZ4X7PGQFHZ2YJR37QRYZ9R4")
	created := httptest.NewRecorder()
	handler.ServeHTTP(created, create)
	if created.Code != http.StatusCreated || store.create != 1 || verifier.seen.RequiredPermission != "projects.act" {
		t.Fatalf("create status=%d calls=%d verification=%#v body=%s", created.Code, store.create, verifier.seen, created.Body.String())
	}
	if !strings.Contains(created.Body.String(), `"kind":"Turn"`) || !strings.Contains(created.Body.String(), `"sessionId":"session-alpha"`) {
		t.Fatalf("create response=%s", created.Body.String())
	}

	get := httptest.NewRequest(http.MethodGet, "/v1/tenants/tenant-alpha/projects/project-alpha/sessions/session-alpha/turns/turn-alpha", nil)
	get.Header.Set("Authorization", "Bearer access-token")
	get.Header.Set("X-Request-ID", "request-get")
	got := httptest.NewRecorder()
	handler.ServeHTTP(got, get)
	if got.Code != http.StatusOK || store.get != 1 || verifier.seen.RequiredPermission != "projects.get" {
		t.Fatalf("get status=%d calls=%d verification=%#v body=%s", got.Code, store.get, verifier.seen, got.Body.String())
	}

	list := httptest.NewRequest(http.MethodGet, "/v1/tenants/tenant-alpha/projects/project-alpha/sessions/session-alpha/turns?pageSize=1", nil)
	list.Header.Set("Authorization", "Bearer access-token")
	list.Header.Set("X-Request-ID", "request-list")
	listed := httptest.NewRecorder()
	handler.ServeHTTP(listed, list)
	var page managedAgentTurnPageResource
	if err := json.Unmarshal(listed.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if listed.Code != http.StatusOK || store.list != 1 || store.limit != 1 || store.after != "" || verifier.seen.RequiredPermission != "projects.get" || len(page.Turns) != 1 || page.NextPageToken == "" {
		t.Fatalf("list status=%d calls=%d after=%q limit=%d verification=%#v page=%#v", listed.Code, store.list, store.after, store.limit, verifier.seen, page)
	}
	wrongSessionToken, ok := encodeManagedAgentTurnPageToken("tenant-alpha", "project-alpha", "session-other", "turn-alpha")
	if !ok {
		t.Fatal("failed to encode wrong-session token")
	}
	wrongSession := httptest.NewRequest(http.MethodGet, "/v1/tenants/tenant-alpha/projects/project-alpha/sessions/session-alpha/turns?pageToken="+wrongSessionToken, nil)
	wrongSession.Header.Set("Authorization", "Bearer access-token")
	wrongSession.Header.Set("X-Request-ID", "request-list-wrong")
	rejected := httptest.NewRecorder()
	handler.ServeHTTP(rejected, wrongSession)
	if rejected.Code != http.StatusBadRequest || store.list != 1 {
		t.Fatalf("wrong-session token status=%d calls=%d body=%s", rejected.Code, store.list, rejected.Body.String())
	}
}

func TestManagedAgentTurnHTTPServerRejectsInvalidPublicInputs(t *testing.T) {
	validPath := "/v1/tenants/tenant-alpha/projects/project-alpha/sessions/session-alpha/turns"
	validRequestID := "request-alpha"
	validIdempotencyKey := "idem-01JZ4X7PGQFHZ2YJR37QRYZ9R4"
	tests := []struct {
		name        string
		path        string
		requestID   string
		idempotency string
		body        string
	}{
		{name: "request id", path: validPath, requestID: "request:invalid", idempotency: validIdempotencyKey, body: `{"turnId":"turn-alpha","inputText":"hello"}`},
		{name: "project identifier", path: "/v1/tenants/tenant-alpha/projects/-project/sessions/session-alpha/turns", requestID: validRequestID, idempotency: validIdempotencyKey, body: `{"turnId":"turn-alpha","inputText":"hello"}`},
		{name: "idempotency key", path: validPath, requestID: validRequestID, idempotency: "short", body: `{"turnId":"turn-alpha","inputText":"hello"}`},
		{name: "turn identifier", path: validPath, requestID: validRequestID, idempotency: validIdempotencyKey, body: `{"turnId":"-turn","inputText":"hello"}`},
		{name: "null input", path: validPath, requestID: validRequestID, idempotency: validIdempotencyKey, body: `{"turnId":"turn-alpha","inputText":null}`},
		{name: "unknown field", path: validPath, requestID: validRequestID, idempotency: validIdempotencyKey, body: `{"turnId":"turn-alpha","inputText":"hello","extra":true}`},
		{name: "duplicate field", path: validPath, requestID: validRequestID, idempotency: validIdempotencyKey, body: `{"turnId":"turn-alpha","turnId":"turn-beta","inputText":"hello"}`},
		{name: "input too large", path: validPath, requestID: validRequestID, idempotency: validIdempotencyKey, body: `{"turnId":"turn-alpha","inputText":"` + strings.Repeat("a", managedAgentMaximumInputBytes+1) + `"}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			verifier := &projectHTTPVerifierFake{}
			store := &managedAgentTurnStoreFake{}
			handler, err := NewManagedAgentTurnHTTPServer(verifier, store)
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
			if test.requestID == "request:invalid" && response.Header().Get("X-Request-ID") != publicFallbackRequestID {
				t.Fatalf("invalid request id was echoed: %q", response.Header().Get("X-Request-ID"))
			}
		})
	}
}
