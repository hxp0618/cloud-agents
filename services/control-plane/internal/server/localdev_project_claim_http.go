//go:build localdev

package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	openapiv1 "github.com/hxp0618/cloud-agents/sdk/go/gen/openapi/v1alpha1"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authn"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/store/postgres"
)

const (
	LocalProjectClaimRoutePrefix = "/v1alpha1/tenants/"
	LocalProjectClaimRouteSuffix = "/project-claims"
	localClaimAPIVersion         = "cloud-agents.local/v1alpha1"
	localClaimMaximumBodyBytes   = 1 << 20
)

var ErrInvalidLocalProjectClaimServer = errors.New("local project claim server configuration is invalid")

type localProjectClaimer interface {
	Claim(context.Context, *authn.VerifiedPrincipal, ManagedAgentCreateProjectRequest) (postgres.IdempotencyClaimResult, error)
}

// LocalProjectClaimHTTPServer exposes only a localdev, claim-only admission
// route. It never creates a project or operation and never invokes a provider.
type LocalProjectClaimHTTPServer struct {
	verifier *authn.LocalVerifier
	claimer  localProjectClaimer
}

func NewLocalProjectClaimHTTPServer(
	verifier *authn.LocalVerifier,
	claimer *ManagedAgentCreateProjectServer,
) (*LocalProjectClaimHTTPServer, error) {
	return newLocalProjectClaimHTTPServer(verifier, claimer)
}

func newLocalProjectClaimHTTPServer(
	verifier *authn.LocalVerifier,
	claimer localProjectClaimer,
) (*LocalProjectClaimHTTPServer, error) {
	if verifier == nil || claimer == nil {
		return nil, ErrInvalidLocalProjectClaimServer
	}
	return &LocalProjectClaimHTTPServer{verifier: verifier, claimer: claimer}, nil
}

type LocalProjectClaimEffects struct {
	ProjectCreated      bool `json:"projectCreated"`
	OperationCreated    bool `json:"operationCreated"`
	ProviderSideEffects bool `json:"providerSideEffects"`
}

// LocalProjectClaimResponse is intentionally distinct from the generated
// Project response used by POST /v1/tenants/{tenantId}/projects.
type LocalProjectClaimResponse struct {
	APIVersion          string                   `json:"apiVersion"`
	Kind                string                   `json:"kind"`
	TenantID            string                   `json:"tenantId"`
	RequestID           string                   `json:"requestId"`
	IdempotencyKey      string                   `json:"idempotencyKey"`
	DatabaseOutcome     postgres.DatabaseOutcome `json:"databaseOutcome"`
	Disposition         string                   `json:"disposition"`
	ReplayState         string                   `json:"replayState,omitempty"`
	OperationID         *string                  `json:"operationId,omitempty"`
	OperationGeneration *int64                   `json:"operationGeneration,omitempty"`
	ResourceKind        *string                  `json:"resourceKind,omitempty"`
	ResourceID          *string                  `json:"resourceId,omitempty"`
	ResourceVersion     *int64                   `json:"resourceVersion,omitempty"`
	StableErrorCode     *string                  `json:"stableErrorCode,omitempty"`
	ExpiresAt           string                   `json:"expiresAt,omitempty"`
	Effects             LocalProjectClaimEffects `json:"effects"`
}

type localProjectClaimErrorResponse struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Code       string `json:"code"`
}

func (server *LocalProjectClaimHTTPServer) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	writer.Header().Set("Cache-Control", "no-store")
	if server == nil || server.verifier == nil || server.claimer == nil {
		writeLocalClaimError(writer, http.StatusInternalServerError, "internal_error")
		return
	}
	tenantID, routeOK := localProjectClaimTenant(request.URL.Path)
	if !routeOK {
		writeLocalClaimError(writer, http.StatusNotFound, "route_not_found")
		return
	}
	if request.Method != http.MethodPost {
		writer.Header().Set("Allow", http.MethodPost)
		writeLocalClaimError(writer, http.StatusMethodNotAllowed, "method_not_allowed")
		return
	}
	requestID, requestIDOK := exactSingleHeader(request.Header, "X-Request-ID")
	idempotencyKey, idempotencyOK := exactSingleHeader(request.Header, "Idempotency-Key")
	if !requestIDOK || !idempotencyOK {
		writeLocalClaimError(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(writer, request.Body, localClaimMaximumBodyBytes))
	if err != nil {
		writeLocalClaimError(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	validated, err := openapiv1.ValidateCreateProjectServerRequest(tenantID, requestID, idempotencyKey, body)
	if err != nil {
		writeLocalClaimError(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	bearer, bearerOK := localBearer(request.Header)
	if !bearerOK {
		writeLocalClaimError(writer, http.StatusUnauthorized, "authentication_failed")
		return
	}
	principal, err := server.verifier.Verify(bearer, authn.LocalVerificationRequest{
		TenantID:           validated.TenantID,
		ResourceLevel:      "organization",
		ResourceID:         validated.Body.OrganizationRef.ID,
		RequiredPermission: "projects.create",
	})
	if err != nil {
		writeLocalClaimError(writer, http.StatusUnauthorized, "authentication_failed")
		return
	}
	result, err := server.claimer.Claim(request.Context(), principal, ManagedAgentCreateProjectRequest{
		RouteTenantID:  validated.TenantID,
		RequestID:      validated.RequestID,
		IdempotencyKey: validated.IdempotencyKey,
		Body:           body,
	})
	if err != nil {
		status, code := localClaimErrorStatus(err)
		writeLocalClaimError(writer, status, code)
		return
	}
	status := http.StatusOK
	if result.DatabaseOutcome == postgres.DatabaseUnknown {
		writeLocalClaimError(writer, http.StatusInternalServerError, "commit_outcome_unknown")
		return
	}
	if result.DatabaseOutcome == postgres.DatabaseRejected || result.Disposition == "conflict" {
		status = http.StatusConflict
	}
	disposition := result.Disposition
	if disposition == "" && result.DatabaseOutcome == postgres.DatabaseRejected {
		disposition = "rejected"
	}
	expiresAt := ""
	if !result.ExpiresAt.IsZero() {
		expiresAt = result.ExpiresAt.UTC().Format(time.RFC3339Nano)
	}
	writer.Header().Set("X-Request-ID", validated.RequestID)
	writeLocalClaimJSON(writer, status, LocalProjectClaimResponse{
		APIVersion:          localClaimAPIVersion,
		Kind:                "ProjectClaim",
		TenantID:            validated.TenantID,
		RequestID:           validated.RequestID,
		IdempotencyKey:      validated.IdempotencyKey,
		DatabaseOutcome:     result.DatabaseOutcome,
		Disposition:         disposition,
		ReplayState:         result.ReplayState,
		OperationID:         result.OperationID,
		OperationGeneration: result.OperationGeneration,
		ResourceKind:        result.ResourceKind,
		ResourceID:          result.ResourceID,
		ResourceVersion:     result.ResourceVersion,
		StableErrorCode:     result.StableErrorCode,
		ExpiresAt:           expiresAt,
		Effects:             LocalProjectClaimEffects{},
	})
}

func localProjectClaimTenant(path string) (string, bool) {
	if !strings.HasPrefix(path, LocalProjectClaimRoutePrefix) || !strings.HasSuffix(path, LocalProjectClaimRouteSuffix) {
		return "", false
	}
	tenantID := strings.TrimSuffix(strings.TrimPrefix(path, LocalProjectClaimRoutePrefix), LocalProjectClaimRouteSuffix)
	return tenantID, tenantID != "" && !strings.Contains(tenantID, "/")
}

func exactSingleHeader(header http.Header, name string) (string, bool) {
	values := header.Values(name)
	return firstExactValue(values)
}

func firstExactValue(values []string) (string, bool) {
	if len(values) != 1 || values[0] == "" || strings.TrimSpace(values[0]) != values[0] {
		return "", false
	}
	return values[0], true
}

func localBearer(header http.Header) (string, bool) {
	value, ok := exactSingleHeader(header, "Authorization")
	if !ok || !strings.HasPrefix(value, "Bearer ") {
		return "", false
	}
	token := strings.TrimPrefix(value, "Bearer ")
	return token, token != "" && !strings.ContainsAny(token, " \t\r\n")
}

func localClaimErrorStatus(err error) (int, string) {
	switch {
	case errors.Is(err, postgres.ErrMutationDenied):
		return http.StatusForbidden, "authorization_denied"
	case errors.Is(err, postgres.ErrCoordinationInvalidInput):
		return http.StatusBadRequest, "invalid_request"
	case errors.Is(err, postgres.ErrCoordinationRejected):
		return http.StatusConflict, "claim_conflict"
	case errors.Is(err, postgres.ErrCoordinationCommitUnknown):
		return http.StatusInternalServerError, "commit_outcome_unknown"
	default:
		return http.StatusInternalServerError, "internal_error"
	}
}

func writeLocalClaimError(writer http.ResponseWriter, status int, code string) {
	writeLocalClaimJSON(writer, status, localProjectClaimErrorResponse{
		APIVersion: localClaimAPIVersion,
		Kind:       "Error",
		Code:       code,
	})
}

func writeLocalClaimJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}
