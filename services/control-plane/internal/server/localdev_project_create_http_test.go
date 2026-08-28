//go:build localdev

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
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/store/postgres"
)

const localDurableProjectCreateRequestBody = `{"name":"project-alpha","organizationRef":{"namespace":"cloud-agents","kind":"organization","id":"organization-alpha"},"displayName":"Project Alpha"}`

type localDurableProjectCreatorFake struct {
	result postgres.DurableProjectCreateResult
	err    error
	calls  int
	input  ManagedAgentCreateProjectRequest
}

func (fake *localDurableProjectCreatorFake) Create(
	_ context.Context,
	_ *authn.VerifiedPrincipal,
	request ManagedAgentCreateProjectRequest,
) (postgres.DurableProjectCreateResult, error) {
	fake.calls++
	fake.input = request
	return fake.result, fake.err
}

func TestLocalDurableProjectCreateHTTPServerCreatedReplayAndConflict(t *testing.T) {
	operationID := "operation-project-alpha"
	resourceKind := "project"
	resourceID := "project-alpha"
	resourceVersion := int64(7)
	outboxID := "event-project-alpha"
	outboxState := "pending"
	tests := []struct {
		name       string
		result     postgres.DurableProjectCreateResult
		wantStatus int
		wantDisp   string
	}{
		{
			name: "created",
			result: postgres.DurableProjectCreateResult{
				DatabaseOutcome: postgres.DatabaseCommitted, Disposition: "created", ReplayState: "succeeded",
				OperationID: &operationID, OperationGeneration: &resourceVersion, ResourceKind: &resourceKind,
				ResourceID: &resourceID, ResourceVersion: &resourceVersion, OutboxEventID: &outboxID, OutboxState: &outboxState,
			},
			wantStatus: http.StatusCreated, wantDisp: "created",
		},
		{
			name: "replay",
			result: postgres.DurableProjectCreateResult{
				DatabaseOutcome: postgres.DatabaseCommitted, Disposition: "replay", ReplayState: "succeeded",
				ResourceKind: &resourceKind, ResourceID: &resourceID, ResourceVersion: &resourceVersion,
			},
			wantStatus: http.StatusOK, wantDisp: "replay",
		},
		{
			name:       "conflict",
			result:     postgres.DurableProjectCreateResult{DatabaseOutcome: postgres.DatabaseRejected, Disposition: "conflict"},
			wantStatus: http.StatusConflict, wantDisp: "conflict",
		},
		{
			name:       "database rejected",
			result:     postgres.DurableProjectCreateResult{DatabaseOutcome: postgres.DatabaseRejected},
			wantStatus: http.StatusConflict, wantDisp: "rejected",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			verifier, token := localVerifierAndToken(t, testLocalHTTPNow())
			fake := &localDurableProjectCreatorFake{result: test.result}
			server, err := newLocalDurableProjectCreateHTTPServer(verifier, fake)
			if err != nil {
				t.Fatal(err)
			}
			response := httptest.NewRecorder()
			server.ServeHTTP(response, localDurableProjectCreateHTTPRequest(token, localDurableProjectCreateRequestBody))
			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
			}
			var got LocalDurableProjectCreateResponse
			if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			if got.Disposition != test.wantDisp || got.TenantID != "tenant-alpha" || got.RequestID != "request-alpha" ||
				got.IdempotencyKey != "idem-01JZ4X7PGQFHZ2YJR37QRYZ9R2" || got.Effects.ProviderSideEffects {
				t.Fatalf("response = %#v", got)
			}
			if fake.calls != 1 || fake.input.RouteTenantID != "tenant-alpha" || fake.input.RequestID != "request-alpha" {
				t.Fatalf("creator calls/input = %d / %#v", fake.calls, fake.input)
			}
		})
	}
}

func TestLocalDurableProjectCreateHTTPServerAdmissionAndRedaction(t *testing.T) {
	verifier, token := localVerifierAndToken(t, testLocalHTTPNow())
	tests := []struct {
		name       string
		request    func() *http.Request
		creatorErr error
		wantStatus int
		wantCode   string
		wantCalls  int
	}{
		{name: "route", request: func() *http.Request {
			return httptest.NewRequest(http.MethodPost, "/v1alpha1/tenants/tenant-alpha/project-claims", nil)
		}, wantStatus: http.StatusNotFound, wantCode: "route_not_found"},
		{name: "method", request: func() *http.Request {
			request := localDurableProjectCreateHTTPRequest(token, localDurableProjectCreateRequestBody)
			request.Method = http.MethodGet
			return request
		}, wantStatus: http.StatusMethodNotAllowed, wantCode: "method_not_allowed"},
		{name: "validation before auth", request: func() *http.Request {
			request := localDurableProjectCreateHTTPRequest("", `{}`)
			request.Header.Del("Authorization")
			return request
		}, wantStatus: http.StatusBadRequest, wantCode: "invalid_request"},
		{name: "authentication", request: func() *http.Request {
			return localDurableProjectCreateHTTPRequest("not-a-token", localDurableProjectCreateRequestBody)
		}, wantStatus: http.StatusUnauthorized, wantCode: "authentication_failed"},
		{name: "authorization", request: func() *http.Request {
			return localDurableProjectCreateHTTPRequest(token, localDurableProjectCreateRequestBody)
		}, creatorErr: postgres.ErrMutationDenied, wantStatus: http.StatusForbidden, wantCode: "authorization_denied", wantCalls: 1},
		{name: "typed database invalid input", request: func() *http.Request {
			return localDurableProjectCreateHTTPRequest(token, localDurableProjectCreateRequestBody)
		}, creatorErr: postgres.ErrMutationInvalidInput, wantStatus: http.StatusBadRequest, wantCode: "invalid_request", wantCalls: 1},
		{name: "database detail redacted", request: func() *http.Request {
			return localDurableProjectCreateHTTPRequest(token, localDurableProjectCreateRequestBody)
		}, creatorErr: errors.New("secret database address"), wantStatus: http.StatusInternalServerError, wantCode: "internal_error", wantCalls: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fake := &localDurableProjectCreatorFake{err: test.creatorErr}
			server, err := newLocalDurableProjectCreateHTTPServer(verifier, fake)
			if err != nil {
				t.Fatal(err)
			}
			response := httptest.NewRecorder()
			server.ServeHTTP(response, test.request())
			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
			}
			var got localDurableProjectCreateErrorResponse
			if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			if got.Code != test.wantCode || strings.Contains(response.Body.String(), "secret") || fake.calls != test.wantCalls {
				t.Fatalf("error response/calls = %#v / %d", got, fake.calls)
			}
		})
	}
}

func localDurableProjectCreateHTTPRequest(token, body string) *http.Request {
	request := httptest.NewRequest(http.MethodPost, LocalDurableProjectCreateRoutePrefix+"tenant-alpha"+LocalDurableProjectCreateRouteSuffix, strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("X-Request-ID", "request-alpha")
	request.Header.Set("Idempotency-Key", "idem-01JZ4X7PGQFHZ2YJR37QRYZ9R2")
	request.Header.Set("Content-Type", "application/json")
	return request
}

func TestLocalDurableProjectCreateHTTPServerNilRequestFailsClosed(t *testing.T) {
	verifier, _ := localVerifierAndToken(t, testLocalHTTPNow())
	server, err := newLocalDurableProjectCreateHTTPServer(verifier, &localDurableProjectCreatorFake{})
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	server.ServeHTTP(response, nil)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
}

func testLocalHTTPNow() (now time.Time) {
	return time.Date(2026, 8, 25, 8, 0, 0, 0, time.UTC)
}
