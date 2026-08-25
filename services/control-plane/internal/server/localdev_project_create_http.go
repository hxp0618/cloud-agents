//go:build localdev

package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	openapiv1 "github.com/hxp0618/cloud-agents/sdk/go/gen/openapi/v1alpha1"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authn"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/store/postgres"
)

const (
	LocalDurableProjectCreateRoutePrefix = "/v1alpha1/tenants/"
	LocalDurableProjectCreateRouteSuffix = "/project-creations"
	localDurableProjectCreateAPIVersion  = "cloud-agents.local/v1alpha1"
	localDurableProjectMaximumBodyBytes  = 1 << 20
)

var ErrInvalidLocalDurableProjectCreateServer = errors.New("local durable project create server configuration is invalid")

type localDurableProjectCreator interface {
	Create(context.Context, *authn.VerifiedPrincipal, ManagedAgentCreateProjectRequest) (postgres.DurableProjectCreateResult, error)
}

// LocalDurableProjectCreateHTTPServer is the only adapter for the generated
// successor profile. It is localdev/loopback-only and has no provider port.
type LocalDurableProjectCreateHTTPServer struct {
	verifier *authn.LocalVerifier
	creator  localDurableProjectCreator
}

func NewLocalDurableProjectCreateHTTPServer(
	verifier *authn.LocalVerifier,
	creator *DurableProjectCreateServer,
) (*LocalDurableProjectCreateHTTPServer, error) {
	return newLocalDurableProjectCreateHTTPServer(verifier, creator)
}

func newLocalDurableProjectCreateHTTPServer(
	verifier *authn.LocalVerifier,
	creator localDurableProjectCreator,
) (*LocalDurableProjectCreateHTTPServer, error) {
	if verifier == nil || creator == nil {
		return nil, ErrInvalidLocalDurableProjectCreateServer
	}
	return &LocalDurableProjectCreateHTTPServer{verifier: verifier, creator: creator}, nil
}

type LocalDurableProjectCreateEffects struct {
	ProjectCreated      bool `json:"projectCreated"`
	OperationCreated    bool `json:"operationCreated"`
	ProviderSideEffects bool `json:"providerSideEffects"`
}

type LocalDurableProjectCreateResponse struct {
	APIVersion          string                           `json:"apiVersion"`
	Kind                string                           `json:"kind"`
	TenantID            string                           `json:"tenantId"`
	RequestID           string                           `json:"requestId"`
	IdempotencyKey      string                           `json:"idempotencyKey"`
	DatabaseOutcome     postgres.DatabaseOutcome         `json:"databaseOutcome"`
	Disposition         string                           `json:"disposition"`
	ReplayState         string                           `json:"replayState,omitempty"`
	OperationID         *string                          `json:"operationId,omitempty"`
	OperationGeneration *int64                           `json:"operationGeneration,omitempty"`
	ResourceKind        *string                          `json:"resourceKind,omitempty"`
	ResourceID          *string                          `json:"resourceId,omitempty"`
	ResourceVersion     *int64                           `json:"resourceVersion,omitempty"`
	StableErrorCode     *string                          `json:"stableErrorCode,omitempty"`
	OutboxEventID       *string                          `json:"outboxEventId,omitempty"`
	OutboxState         *string                          `json:"outboxState,omitempty"`
	ExpiresAt           string                           `json:"expiresAt,omitempty"`
	Effects             LocalDurableProjectCreateEffects `json:"effects"`
}

type localDurableProjectCreateErrorResponse struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Code       string `json:"code"`
}

func (server *LocalDurableProjectCreateHTTPServer) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	writer.Header().Set("Cache-Control", "no-store")
	if server == nil || server.verifier == nil || server.creator == nil {
		writeLocalDurableProjectCreateError(writer, http.StatusInternalServerError, "internal_error")
		return
	}
	tenantID, routeOK := localDurableProjectCreateTenant(request.URL.Path)
	if !routeOK {
		writeLocalDurableProjectCreateError(writer, http.StatusNotFound, "route_not_found")
		return
	}
	if request.Method != http.MethodPost {
		writer.Header().Set("Allow", http.MethodPost)
		writeLocalDurableProjectCreateError(writer, http.StatusMethodNotAllowed, "method_not_allowed")
		return
	}
	requestID, requestIDOK := exactSingleHeader(request.Header, "X-Request-ID")
	idempotencyKey, idempotencyOK := exactSingleHeader(request.Header, "Idempotency-Key")
	if !requestIDOK || !idempotencyOK {
		writeLocalDurableProjectCreateError(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(writer, request.Body, localDurableProjectMaximumBodyBytes))
	if err != nil {
		writeLocalDurableProjectCreateError(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	validated, err := openapiv1.ValidateCreateProjectServerRequest(tenantID, requestID, idempotencyKey, body)
	if err != nil {
		writeLocalDurableProjectCreateError(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	bearer, bearerOK := localBearer(request.Header)
	if !bearerOK {
		writeLocalDurableProjectCreateError(writer, http.StatusUnauthorized, "authentication_failed")
		return
	}
	principal, err := server.verifier.Verify(bearer, authn.LocalVerificationRequest{
		TenantID:           validated.TenantID,
		ResourceLevel:      "organization",
		ResourceID:         validated.Body.OrganizationRef.ID,
		RequiredPermission: "projects.create",
	})
	if err != nil {
		writeLocalDurableProjectCreateError(writer, http.StatusUnauthorized, "authentication_failed")
		return
	}
	result, err := server.creator.Create(request.Context(), principal, ManagedAgentCreateProjectRequest{
		RouteTenantID: validated.TenantID, RequestID: validated.RequestID,
		IdempotencyKey: validated.IdempotencyKey, Body: body,
	})
	if err != nil {
		status, code := localDurableProjectCreateErrorStatus(err)
		writeLocalDurableProjectCreateError(writer, status, code)
		return
	}
	if result.DatabaseOutcome == postgres.DatabaseUnknown {
		writeLocalDurableProjectCreateError(writer, http.StatusInternalServerError, "commit_outcome_unknown")
		return
	}
	status := http.StatusCreated
	if result.DatabaseOutcome == postgres.DatabaseRejected || result.Disposition == "conflict" {
		status = http.StatusConflict
	} else if result.Disposition == "replay" {
		status = http.StatusOK
	}
	disposition := result.Disposition
	if disposition == "" && result.DatabaseOutcome == postgres.DatabaseRejected {
		disposition = "rejected"
	}
	writer.Header().Set("X-Request-ID", validated.RequestID)
	writeLocalDurableProjectCreateJSON(writer, status, LocalDurableProjectCreateResponse{
		APIVersion: localDurableProjectCreateAPIVersion,
		Kind:       "DurableProjectCreateResult", TenantID: validated.TenantID,
		RequestID: validated.RequestID, IdempotencyKey: validated.IdempotencyKey,
		DatabaseOutcome: result.DatabaseOutcome, Disposition: disposition,
		ReplayState: result.ReplayState, OperationID: result.OperationID,
		OperationGeneration: result.OperationGeneration, ResourceKind: result.ResourceKind,
		ResourceID: result.ResourceID, ResourceVersion: result.ResourceVersion,
		StableErrorCode: result.StableErrorCode, OutboxEventID: result.OutboxEventID,
		OutboxState: result.OutboxState,
		Effects: LocalDurableProjectCreateEffects{
			ProjectCreated:      result.Disposition == "created",
			OperationCreated:    result.Disposition == "created",
			ProviderSideEffects: false,
		},
	})
}

func localDurableProjectCreateTenant(path string) (string, bool) {
	if !strings.HasPrefix(path, LocalDurableProjectCreateRoutePrefix) || !strings.HasSuffix(path, LocalDurableProjectCreateRouteSuffix) {
		return "", false
	}
	tenantID := strings.TrimSuffix(strings.TrimPrefix(path, LocalDurableProjectCreateRoutePrefix), LocalDurableProjectCreateRouteSuffix)
	return tenantID, tenantID != "" && !strings.Contains(tenantID, "/")
}

func localDurableProjectCreateErrorStatus(err error) (int, string) {
	switch {
	case errors.Is(err, postgres.ErrMutationDenied):
		return http.StatusForbidden, "authorization_denied"
	case errors.Is(err, postgres.ErrCoordinationInvalidInput), errors.Is(err, postgres.ErrMutationInvalidInput):
		return http.StatusBadRequest, "invalid_request"
	case errors.Is(err, postgres.ErrCoordinationRejected):
		return http.StatusConflict, "create_conflict"
	case errors.Is(err, postgres.ErrCoordinationCommitUnknown):
		return http.StatusInternalServerError, "commit_outcome_unknown"
	default:
		return http.StatusInternalServerError, "internal_error"
	}
}

func writeLocalDurableProjectCreateError(writer http.ResponseWriter, status int, code string) {
	writeLocalDurableProjectCreateJSON(writer, status, localDurableProjectCreateErrorResponse{
		APIVersion: localDurableProjectCreateAPIVersion, Kind: "Error", Code: code,
	})
}

func writeLocalDurableProjectCreateJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}
