package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authn"
	internalmanagedagent "github.com/hxp0618/cloud-agents/services/control-plane/internal/managedagent"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/store/postgres"
)

var ErrInvalidManagedAgentTurnHTTPServer = errors.New("managed agent turn HTTP server configuration is invalid")

type managedAgentTurnStore interface {
	CreateManagedAgentTurn(context.Context, string, *authn.VerifiedPrincipal, internalmanagedagent.CreateTurnInput) (internalmanagedagent.TurnSnapshot, error)
	GetManagedAgentTurn(context.Context, string, *authn.VerifiedPrincipal, string, string, string) (internalmanagedagent.TurnSnapshot, error)
}

type ManagedAgentTurnHTTPServer struct {
	verifier AccessTokenVerifier
	store    managedAgentTurnStore
}

func NewManagedAgentTurnHTTPServer(verifier AccessTokenVerifier, store managedAgentTurnStore) (*ManagedAgentTurnHTTPServer, error) {
	if verifier == nil || store == nil {
		return nil, ErrInvalidManagedAgentTurnHTTPServer
	}
	return &ManagedAgentTurnHTTPServer{verifier: verifier, store: store}, nil
}

func (server *ManagedAgentTurnHTTPServer) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	preparePublicRequestID(writer, request)
	if server == nil || server.verifier == nil || server.store == nil || request == nil {
		writeManagedAgentSessionError(writer, http.StatusInternalServerError, "internal_error")
		return
	}
	tenantID, projectID, sessionID, turnID, action, ok := managedAgentTurnPath(request.URL.Path)
	if !ok {
		writeManagedAgentSessionError(writer, http.StatusNotFound, "route_not_found")
		return
	}
	requestID, requestIDOK := exactSingleHeader(request.Header, "X-Request-ID")
	if !requestIDOK {
		writeManagedAgentSessionError(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	authorization, authorizationOK := exactSingleHeader(request.Header, "Authorization")
	if !authorizationOK {
		writeManagedAgentSessionError(writer, http.StatusUnauthorized, "authentication_failed")
		return
	}
	bearer, bearerOK := bearerToken(authorization)
	if !bearerOK {
		writeManagedAgentSessionError(writer, http.StatusUnauthorized, "authentication_failed")
		return
	}
	if action == "create" && request.Method == http.MethodPost {
		server.create(writer, request, tenantID, projectID, sessionID, requestID, bearer)
		return
	}
	if action == "get" && request.Method == http.MethodGet {
		server.get(writer, request, tenantID, projectID, sessionID, turnID, requestID, bearer)
		return
	}
	writer.Header().Set("Allow", http.MethodGet+", "+http.MethodPost)
	writeManagedAgentSessionError(writer, http.StatusMethodNotAllowed, "method_not_allowed")
}

func (server *ManagedAgentTurnHTTPServer) create(writer http.ResponseWriter, request *http.Request, tenantID, projectID, sessionID, requestID, bearer string) {
	idempotencyKey, ok := exactSingleHeader(request.Header, "Idempotency-Key")
	if !ok {
		writeManagedAgentSessionError(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	var body managedAgentTurnCreateBody
	if err := decodeManagedAgentSessionJSON(request.Body, &body); err != nil || body.TurnID == "" {
		writeManagedAgentSessionError(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	principal, err := server.verifier.Verify(bearer, authn.VerificationRequest{TenantID: tenantID, ResourceLevel: "project", ResourceID: projectID, RequiredPermission: "projects.act"})
	if err != nil {
		writeManagedAgentSessionError(writer, http.StatusUnauthorized, "authentication_failed")
		return
	}
	snapshot, err := server.store.CreateManagedAgentTurn(request.Context(), tenantID, principal, internalmanagedagent.CreateTurnInput{
		Scope: internalmanagedagent.Scope{TenantID: tenantID, ProjectID: projectID}, SessionID: sessionID, TurnID: body.TurnID, InputText: body.InputText,
		Mutation: internalmanagedagent.Mutation{RequestID: requestID, IdempotencyKey: idempotencyKey},
	})
	if err != nil {
		status, code := managedAgentTurnErrorStatus(err)
		writeManagedAgentSessionError(writer, status, code)
		return
	}
	writeManagedAgentTurn(writer, http.StatusCreated, requestID, snapshot)
}

func (server *ManagedAgentTurnHTTPServer) get(writer http.ResponseWriter, request *http.Request, tenantID, projectID, sessionID, turnID, requestID, bearer string) {
	principal, err := server.verifier.Verify(bearer, authn.VerificationRequest{TenantID: tenantID, ResourceLevel: "project", ResourceID: projectID, RequiredPermission: "projects.get"})
	if err != nil {
		writeManagedAgentSessionError(writer, http.StatusUnauthorized, "authentication_failed")
		return
	}
	snapshot, err := server.store.GetManagedAgentTurn(request.Context(), tenantID, principal, projectID, sessionID, turnID)
	if err != nil {
		status, code := managedAgentTurnErrorStatus(err)
		writeManagedAgentSessionError(writer, status, code)
		return
	}
	writeManagedAgentTurn(writer, http.StatusOK, requestID, snapshot)
}

type managedAgentTurnCreateBody struct {
	TurnID    string `json:"turnId"`
	InputText string `json:"inputText"`
}

type managedAgentTurnResource struct {
	APIVersion string                           `json:"apiVersion"`
	Kind       string                           `json:"kind"`
	Metadata   managedAgentTurnResourceMetadata `json:"metadata"`
	Spec       managedAgentTurnResourceSpec     `json:"spec"`
}

type managedAgentTurnResourceMetadata struct {
	UID             string `json:"uid"`
	ProjectID       string `json:"projectId"`
	SessionID       string `json:"sessionId"`
	ResourceVersion string `json:"resourceVersion"`
	CreatedAt       string `json:"createdAt"`
	UpdatedAt       string `json:"updatedAt"`
}

type managedAgentTurnResourceSpec struct {
	InputDigest string `json:"inputDigest"`
	ExecutionID string `json:"executionId,omitempty"`
	State       string `json:"state"`
}

func writeManagedAgentTurn(writer http.ResponseWriter, status int, requestID string, snapshot internalmanagedagent.TurnSnapshot) {
	writer.Header().Set("X-Request-ID", requestID)
	writer.Header().Set("X-Resource-Version", strconv.FormatUint(snapshot.Version, 10))
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(managedAgentTurnResource{
		APIVersion: "managed-agent.cloud-agents.dev/v1alpha1", Kind: "Turn",
		Metadata: managedAgentTurnResourceMetadata{UID: snapshot.TurnID, ProjectID: snapshot.Scope.ProjectID, SessionID: snapshot.SessionID, ResourceVersion: strconv.FormatUint(snapshot.Version, 10), CreatedAt: snapshot.CreatedAt.UTC().Format("2006-01-02T15:04:05.999999999Z07:00"), UpdatedAt: snapshot.UpdatedAt.UTC().Format("2006-01-02T15:04:05.999999999Z07:00")},
		Spec:     managedAgentTurnResourceSpec{InputDigest: snapshot.InputDigest, ExecutionID: snapshot.ExecutionID, State: string(snapshot.State)},
	})
}

func managedAgentTurnPath(path string) (tenantID, projectID, sessionID, turnID, action string, ok bool) {
	if !strings.HasPrefix(path, ManagedAgentSessionRoutePrefix) {
		return "", "", "", "", "", false
	}
	parts := strings.Split(strings.TrimPrefix(path, ManagedAgentSessionRoutePrefix), "/")
	if len(parts) == 6 && parts[1] == "projects" && parts[3] == "sessions" && parts[5] == "turns" && parts[0] != "" && parts[2] != "" && parts[4] != "" {
		return parts[0], parts[2], parts[4], "", "create", true
	}
	if len(parts) == 7 && parts[1] == "projects" && parts[3] == "sessions" && parts[5] == "turns" && parts[0] != "" && parts[2] != "" && parts[4] != "" && parts[6] != "" {
		return parts[0], parts[2], parts[4], parts[6], "get", true
	}
	return "", "", "", "", "", false
}

func HandlesManagedAgentTurnPath(path string) bool {
	_, _, _, _, _, ok := managedAgentTurnPath(path)
	return ok
}

func managedAgentTurnErrorStatus(err error) (int, string) {
	switch {
	case errors.Is(err, postgres.ErrManagedAgentTurnNotFound):
		return http.StatusNotFound, "not_found"
	case errors.Is(err, postgres.ErrMutationDenied):
		return http.StatusForbidden, "authorization_denied"
	case errors.Is(err, postgres.ErrCoordinationRejected):
		return http.StatusConflict, "turn_conflict"
	case errors.Is(err, postgres.ErrCoordinationInvalidInput):
		return http.StatusBadRequest, "invalid_request"
	default:
		return http.StatusInternalServerError, "internal_error"
	}
}
