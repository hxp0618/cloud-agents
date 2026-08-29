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

type managedAgentEventsStoreFake struct {
	page      internalmanagedagent.EventPage
	tenantID  string
	projectID string
	sessionID string
	limit     int
	called    int
}

func (fake *managedAgentEventsStoreFake) GetManagedAgentEvents(_ context.Context, tenantID string, _ *authn.VerifiedPrincipal, projectID, sessionID string, _ internalmanagedagent.EventCursor, limit int) (internalmanagedagent.EventPage, error) {
	fake.called++
	fake.tenantID, fake.projectID, fake.sessionID, fake.limit = tenantID, projectID, sessionID, limit
	return fake.page, nil
}

func TestManagedAgentEventsHTTPServerReturnsBoundPage(t *testing.T) {
	now := time.Date(2026, time.August, 29, 8, 0, 0, 0, time.UTC)
	verifier := &projectHTTPVerifierFake{}
	store := &managedAgentEventsStoreFake{page: internalmanagedagent.EventPage{Events: []internalmanagedagent.LifecycleEvent{{
		EventID: "managed-agent-event-1", Sequence: 1, Scope: internalmanagedagent.Scope{TenantID: "tenant-alpha", ProjectID: "project-alpha"},
		Operation: "session.create", Resource: internalmanagedagent.ResourceSession, OccurredAt: now,
		MutationDigest: "sha256:" + strings.Repeat("a", 64), Changes: []internalmanagedagent.LifecycleStateChange{{Resource: internalmanagedagent.ResourceSession, To: "active", Version: 1}},
	}}}}
	handler, err := NewManagedAgentEventsHTTPServer(verifier, store)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/v1/tenants/tenant-alpha/projects/project-alpha/sessions/session-alpha/events?limit=1", nil)
	request.Header.Set("Authorization", "Bearer access-token")
	request.Header.Set("X-Request-ID", "request-events")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || store.called != 1 || store.tenantID != "tenant-alpha" || store.projectID != "project-alpha" || store.sessionID != "session-alpha" || store.limit != 1 {
		t.Fatalf("status=%d calls=%d scope=%s/%s/%s limit=%d body=%s", response.Code, store.called, store.tenantID, store.projectID, store.sessionID, store.limit, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"kind":"EventPage"`) || !strings.Contains(response.Body.String(), `"resource":"Session"`) || response.Header().Get("X-Request-ID") != "request-events" {
		t.Fatalf("response headers=%v body=%s", response.Header(), response.Body.String())
	}
	if verifier.seen.RequiredPermission != "projects.get" || verifier.seen.ResourceID != "project-alpha" {
		t.Fatalf("verification request=%#v", verifier.seen)
	}
}
