package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authn"
	internalmanagedagent "github.com/hxp0618/cloud-agents/services/control-plane/internal/managedagent"
)

type managedAgentTurnStoreFake struct {
	snapshot internalmanagedagent.TurnSnapshot
	create   int
	get      int
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

func TestManagedAgentTurnHTTPServerLifecycleRoutes(t *testing.T) {
	now := time.Date(2026, time.August, 29, 8, 0, 0, 0, time.UTC)
	verifier := &projectHTTPVerifierFake{}
	store := &managedAgentTurnStoreFake{snapshot: internalmanagedagent.TurnSnapshot{InputDigest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Version: 1, CreatedAt: now, UpdatedAt: now}}
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
}
