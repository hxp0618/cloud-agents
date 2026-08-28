//go:build localdev

package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authn"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/store/postgres"
)

const localClaimRequestBody = `{"name":"project-alpha","organizationRef":{"namespace":"cloud-agents","kind":"organization","id":"organization-alpha"},"displayName":"Project Alpha"}`

type localProjectClaimerFake struct {
	result  postgres.IdempotencyClaimResult
	err     error
	calls   int
	request ManagedAgentCreateProjectRequest
}

func (fake *localProjectClaimerFake) Claim(
	_ context.Context,
	_ *authn.VerifiedPrincipal,
	request ManagedAgentCreateProjectRequest,
) (postgres.IdempotencyClaimResult, error) {
	fake.calls++
	fake.request = request
	return fake.result, fake.err
}

func TestLocalProjectClaimHTTPServerCreatedReplayAndConflict(t *testing.T) {
	now := time.Date(2026, time.August, 25, 8, 0, 0, 123456000, time.UTC)
	tests := []struct {
		name       string
		result     postgres.IdempotencyClaimResult
		wantStatus int
		want       LocalProjectClaimResponse
	}{
		{
			name: "created",
			result: postgres.IdempotencyClaimResult{
				DatabaseOutcome: postgres.DatabaseCommitted,
				Disposition:     "created",
				ReplayState:     "pending",
				ExpiresAt:       now.Add(24 * time.Hour),
			},
			wantStatus: http.StatusOK,
			want: LocalProjectClaimResponse{
				APIVersion:      localClaimAPIVersion,
				Kind:            "ProjectClaim",
				TenantID:        "tenant-alpha",
				RequestID:       "request-alpha",
				IdempotencyKey:  "idem-01JZ4X7PGQFHZ2YJR37QRYZ9R2",
				DatabaseOutcome: postgres.DatabaseCommitted,
				Disposition:     "created",
				ReplayState:     "pending",
				ExpiresAt:       "2026-08-26T08:00:00.123456Z",
			},
		},
		{
			name: "replay pending",
			result: postgres.IdempotencyClaimResult{
				DatabaseOutcome: postgres.DatabaseCommitted,
				Disposition:     "replay",
				ReplayState:     "pending",
				ExpiresAt:       now.Add(24 * time.Hour),
			},
			wantStatus: http.StatusOK,
			want: LocalProjectClaimResponse{
				APIVersion:      localClaimAPIVersion,
				Kind:            "ProjectClaim",
				TenantID:        "tenant-alpha",
				RequestID:       "request-alpha",
				IdempotencyKey:  "idem-01JZ4X7PGQFHZ2YJR37QRYZ9R2",
				DatabaseOutcome: postgres.DatabaseCommitted,
				Disposition:     "replay",
				ReplayState:     "pending",
				ExpiresAt:       "2026-08-26T08:00:00.123456Z",
			},
		},
		{
			name: "replay succeeded",
			result: postgres.IdempotencyClaimResult{
				DatabaseOutcome: postgres.DatabaseCommitted,
				Disposition:     "replay",
				ReplayState:     "succeeded",
				ResourceKind:    localStringPointer("project"),
				ResourceID:      localStringPointer("project-alpha"),
				ResourceVersion: localInt64Pointer(7),
				ExpiresAt:       now.Add(24 * time.Hour),
			},
			wantStatus: http.StatusOK,
			want: LocalProjectClaimResponse{
				APIVersion:      localClaimAPIVersion,
				Kind:            "ProjectClaim",
				TenantID:        "tenant-alpha",
				RequestID:       "request-alpha",
				IdempotencyKey:  "idem-01JZ4X7PGQFHZ2YJR37QRYZ9R2",
				DatabaseOutcome: postgres.DatabaseCommitted,
				Disposition:     "replay",
				ReplayState:     "succeeded",
				ResourceKind:    localStringPointer("project"),
				ResourceID:      localStringPointer("project-alpha"),
				ResourceVersion: localInt64Pointer(7),
				ExpiresAt:       "2026-08-26T08:00:00.123456Z",
			},
		},
		{
			name: "replay failed",
			result: postgres.IdempotencyClaimResult{
				DatabaseOutcome: postgres.DatabaseCommitted,
				Disposition:     "replay",
				ReplayState:     "failed",
				StableErrorCode: localStringPointer("project_rejected"),
				ExpiresAt:       now.Add(24 * time.Hour),
			},
			wantStatus: http.StatusOK,
			want: LocalProjectClaimResponse{
				APIVersion:      localClaimAPIVersion,
				Kind:            "ProjectClaim",
				TenantID:        "tenant-alpha",
				RequestID:       "request-alpha",
				IdempotencyKey:  "idem-01JZ4X7PGQFHZ2YJR37QRYZ9R2",
				DatabaseOutcome: postgres.DatabaseCommitted,
				Disposition:     "replay",
				ReplayState:     "failed",
				StableErrorCode: localStringPointer("project_rejected"),
				ExpiresAt:       "2026-08-26T08:00:00.123456Z",
			},
		},
		{
			name:       "database rejected",
			result:     postgres.IdempotencyClaimResult{DatabaseOutcome: postgres.DatabaseRejected},
			wantStatus: http.StatusConflict,
			want: LocalProjectClaimResponse{
				APIVersion:      localClaimAPIVersion,
				Kind:            "ProjectClaim",
				TenantID:        "tenant-alpha",
				RequestID:       "request-alpha",
				IdempotencyKey:  "idem-01JZ4X7PGQFHZ2YJR37QRYZ9R2",
				DatabaseOutcome: postgres.DatabaseRejected,
				Disposition:     "rejected",
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			verifier, token := localVerifierAndToken(t, now)
			fake := &localProjectClaimerFake{result: test.result}
			server, err := newLocalProjectClaimHTTPServer(verifier, fake)
			if err != nil {
				t.Fatal(err)
			}
			response := httptest.NewRecorder()
			server.ServeHTTP(response, localClaimHTTPRequest(token, localClaimRequestBody))
			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
			}
			var got LocalProjectClaimResponse
			if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("response = %#v, want %#v", got, test.want)
			}
			if got.Effects != (LocalProjectClaimEffects{}) {
				t.Fatalf("effects = %#v", got.Effects)
			}
			if fake.calls != 1 || fake.request.RouteTenantID != "tenant-alpha" || fake.request.RequestID != "request-alpha" {
				t.Fatalf("claim calls = %d, request = %#v", fake.calls, fake.request)
			}
		})
	}
}

func localStringPointer(value string) *string { return &value }

func localInt64Pointer(value int64) *int64 { return &value }

func TestLocalProjectClaimHTTPServerAdmissionAndErrorMapping(t *testing.T) {
	now := time.Date(2026, time.August, 25, 8, 0, 0, 0, time.UTC)
	verifier, token := localVerifierAndToken(t, now)
	tests := []struct {
		name       string
		request    func() *http.Request
		claimErr   error
		wantStatus int
		wantCode   string
		wantCalls  int
	}{
		{name: "route", request: func() *http.Request {
			return httptest.NewRequest(http.MethodPost, "/v1/tenants/tenant-alpha/projects", nil)
		}, wantStatus: http.StatusNotFound, wantCode: "route_not_found"},
		{name: "method", request: func() *http.Request {
			request := localClaimHTTPRequest(token, localClaimRequestBody)
			request.Method = http.MethodGet
			return request
		}, wantStatus: http.StatusMethodNotAllowed, wantCode: "method_not_allowed"},
		{name: "generated validation precedes authentication", request: func() *http.Request {
			request := localClaimHTTPRequest("", `{}`)
			request.Header.Del("Authorization")
			return request
		}, wantStatus: http.StatusBadRequest, wantCode: "invalid_request"},
		{name: "authentication", request: func() *http.Request { return localClaimHTTPRequest("not-a-token", localClaimRequestBody) }, wantStatus: http.StatusUnauthorized, wantCode: "authentication_failed"},
		{name: "authorization", request: func() *http.Request { return localClaimHTTPRequest(token, localClaimRequestBody) }, claimErr: postgres.ErrMutationDenied, wantStatus: http.StatusForbidden, wantCode: "authorization_denied", wantCalls: 1},
		{name: "database detail redacted", request: func() *http.Request { return localClaimHTTPRequest(token, localClaimRequestBody) }, claimErr: errors.New("secret database address"), wantStatus: http.StatusInternalServerError, wantCode: "internal_error", wantCalls: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fake := &localProjectClaimerFake{err: test.claimErr}
			server, err := newLocalProjectClaimHTTPServer(verifier, fake)
			if err != nil {
				t.Fatal(err)
			}
			response := httptest.NewRecorder()
			server.ServeHTTP(response, test.request())
			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
			}
			var got localProjectClaimErrorResponse
			if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			if got.Code != test.wantCode || got.APIVersion != localClaimAPIVersion || got.Kind != "Error" {
				t.Fatalf("error response = %#v", got)
			}
			if strings.Contains(response.Body.String(), "secret") || fake.calls != test.wantCalls {
				t.Fatalf("body = %q, calls = %d", response.Body.String(), fake.calls)
			}
		})
	}
}

func localVerifierAndToken(t *testing.T, now time.Time) (*authn.LocalVerifier, string) {
	t.Helper()
	verifier, err := authn.NewLocalVerifier(authn.LocalVerifierConfig{Clock: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(verifier.Invalidate)
	token, err := verifier.IssueToken(authn.LocalTokenClaims{TenantID: "tenant-alpha", Subject: "user-alpha"})
	if err != nil {
		t.Fatal(err)
	}
	return verifier, token
}

func TestLocalProjectClaimHTTPServerNilRequestFailsClosed(t *testing.T) {
	verifier, _ := localVerifierAndToken(t, testLocalHTTPNow())
	server, err := newLocalProjectClaimHTTPServer(verifier, &localProjectClaimerFake{})
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	server.ServeHTTP(response, nil)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
}

func localClaimHTTPRequest(token, body string) *http.Request {
	request := httptest.NewRequest(http.MethodPost, LocalProjectClaimRoutePrefix+"tenant-alpha"+LocalProjectClaimRouteSuffix, strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("X-Request-ID", "request-alpha")
	request.Header.Set("Idempotency-Key", "idem-01JZ4X7PGQFHZ2YJR37QRYZ9R2")
	request.Header.Set("Content-Type", "application/json")
	return request
}
