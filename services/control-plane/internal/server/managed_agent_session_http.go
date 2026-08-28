package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authn"
	internalmanagedagent "github.com/hxp0618/cloud-agents/services/control-plane/internal/managedagent"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/store/postgres"
)

const ManagedAgentSessionRoutePrefix = "/v1/tenants/"

var ErrInvalidManagedAgentSessionHTTPServer = errors.New("managed agent session HTTP server configuration is invalid")

type managedAgentSessionStore interface {
	CreateManagedAgentSession(context.Context, string, *authn.VerifiedPrincipal, internalmanagedagent.CreateSessionInput) (internalmanagedagent.SessionSnapshot, error)
	CloseManagedAgentSession(context.Context, string, *authn.VerifiedPrincipal, internalmanagedagent.CloseSessionInput) (internalmanagedagent.SessionSnapshot, error)
	GetManagedAgentSession(context.Context, string, *authn.VerifiedPrincipal, string, string) (internalmanagedagent.SessionSnapshot, error)
}

type ManagedAgentSessionHTTPServer struct {
	verifier AccessTokenVerifier
	store    managedAgentSessionStore
}

func NewManagedAgentSessionHTTPServer(verifier AccessTokenVerifier, store managedAgentSessionStore) (*ManagedAgentSessionHTTPServer, error) {
	if verifier == nil || store == nil {
		return nil, ErrInvalidManagedAgentSessionHTTPServer
	}
	return &ManagedAgentSessionHTTPServer{verifier: verifier, store: store}, nil
}

func (server *ManagedAgentSessionHTTPServer) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	if server == nil || server.verifier == nil || server.store == nil || request == nil {
		writeManagedAgentSessionError(writer, http.StatusInternalServerError, "internal_error")
		return
	}
	tenantID, projectID, sessionID, action, ok := managedAgentSessionPath(request.URL.Path)
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

	switch {
	case action == "create" && request.Method == http.MethodPost:
		server.create(writer, request, tenantID, projectID, requestID, bearer)
	case action == "close" && request.Method == http.MethodPost:
		server.close(writer, request, tenantID, projectID, sessionID, requestID, bearer)
	case action == "get" && request.Method == http.MethodGet:
		server.get(writer, request, tenantID, projectID, sessionID, requestID, bearer)
	default:
		writer.Header().Set("Allow", http.MethodGet+", "+http.MethodPost)
		writeManagedAgentSessionError(writer, http.StatusMethodNotAllowed, "method_not_allowed")
	}
}

func (server *ManagedAgentSessionHTTPServer) create(writer http.ResponseWriter, request *http.Request, tenantID, projectID, requestID, bearer string) {
	idempotencyKey, ok := exactSingleHeader(request.Header, "Idempotency-Key")
	if !ok {
		writeManagedAgentSessionError(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	var body managedAgentSessionCreateBody
	if err := decodeManagedAgentSessionJSON(request.Body, &body); err != nil || body.SessionID == "" || body.ProviderKind == "" {
		writeManagedAgentSessionError(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	principal, err := server.verifier.Verify(bearer, authn.VerificationRequest{TenantID: tenantID, ResourceLevel: "project", ResourceID: projectID, RequiredPermission: "projects.act"})
	if err != nil {
		writeManagedAgentSessionError(writer, http.StatusUnauthorized, "authentication_failed")
		return
	}
	snapshot, err := server.store.CreateManagedAgentSession(request.Context(), tenantID, principal, internalmanagedagent.CreateSessionInput{
		Scope: internalmanagedagent.Scope{TenantID: tenantID, ProjectID: projectID}, SessionID: body.SessionID, ProviderKind: body.ProviderKind,
		Mutation: internalmanagedagent.Mutation{RequestID: requestID, IdempotencyKey: idempotencyKey},
	})
	if err != nil {
		status, code := managedAgentSessionErrorStatus(err)
		writeManagedAgentSessionError(writer, status, code)
		return
	}
	writeManagedAgentSession(writer, http.StatusCreated, requestID, snapshot)
}

func (server *ManagedAgentSessionHTTPServer) close(writer http.ResponseWriter, request *http.Request, tenantID, projectID, sessionID, requestID, bearer string) {
	idempotencyKey, ok := exactSingleHeader(request.Header, "Idempotency-Key")
	if !ok {
		writeManagedAgentSessionError(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	principal, err := server.verifier.Verify(bearer, authn.VerificationRequest{TenantID: tenantID, ResourceLevel: "project", ResourceID: projectID, RequiredPermission: "projects.act"})
	if err != nil {
		writeManagedAgentSessionError(writer, http.StatusUnauthorized, "authentication_failed")
		return
	}
	snapshot, err := server.store.CloseManagedAgentSession(request.Context(), tenantID, principal, internalmanagedagent.CloseSessionInput{
		Scope: internalmanagedagent.Scope{TenantID: tenantID, ProjectID: projectID}, SessionID: sessionID,
		Mutation: internalmanagedagent.Mutation{RequestID: requestID, IdempotencyKey: idempotencyKey},
	})
	if err != nil {
		status, code := managedAgentSessionErrorStatus(err)
		writeManagedAgentSessionError(writer, status, code)
		return
	}
	writeManagedAgentSession(writer, http.StatusOK, requestID, snapshot)
}

func (server *ManagedAgentSessionHTTPServer) get(writer http.ResponseWriter, request *http.Request, tenantID, projectID, sessionID, requestID, bearer string) {
	principal, err := server.verifier.Verify(bearer, authn.VerificationRequest{TenantID: tenantID, ResourceLevel: "project", ResourceID: projectID, RequiredPermission: "projects.get"})
	if err != nil {
		writeManagedAgentSessionError(writer, http.StatusUnauthorized, "authentication_failed")
		return
	}
	snapshot, err := server.store.GetManagedAgentSession(request.Context(), tenantID, principal, projectID, sessionID)
	if err != nil {
		status, code := managedAgentSessionErrorStatus(err)
		writeManagedAgentSessionError(writer, status, code)
		return
	}
	writeManagedAgentSession(writer, http.StatusOK, requestID, snapshot)
}

type managedAgentSessionCreateBody struct {
	SessionID    string `json:"sessionId"`
	ProviderKind string `json:"providerKind"`
}

type managedAgentSessionResource struct {
	APIVersion string                              `json:"apiVersion"`
	Kind       string                              `json:"kind"`
	Metadata   managedAgentSessionResourceMetadata `json:"metadata"`
	Spec       managedAgentSessionResourceSpec     `json:"spec"`
}

type managedAgentSessionResourceMetadata struct {
	UID             string `json:"uid"`
	ProjectID       string `json:"projectId"`
	ResourceVersion string `json:"resourceVersion"`
	CreatedAt       string `json:"createdAt"`
	UpdatedAt       string `json:"updatedAt"`
}

type managedAgentSessionResourceSpec struct {
	ProviderKind string `json:"providerKind"`
	State        string `json:"state"`
}

func writeManagedAgentSession(writer http.ResponseWriter, status int, requestID string, snapshot internalmanagedagent.SessionSnapshot) {
	writer.Header().Set("X-Request-ID", requestID)
	writer.Header().Set("X-Resource-Version", strconv.FormatUint(snapshot.Version, 10))
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(managedAgentSessionResource{
		APIVersion: "managed-agent.cloud-agents.dev/v1alpha1", Kind: "Session",
		Metadata: managedAgentSessionResourceMetadata{UID: snapshot.SessionID, ProjectID: snapshot.Scope.ProjectID, ResourceVersion: strconv.FormatUint(snapshot.Version, 10), CreatedAt: snapshot.CreatedAt.UTC().Format("2006-01-02T15:04:05.999999999Z07:00"), UpdatedAt: snapshot.UpdatedAt.UTC().Format("2006-01-02T15:04:05.999999999Z07:00")},
		Spec:     managedAgentSessionResourceSpec{ProviderKind: snapshot.ProviderKind, State: string(snapshot.State)},
	})
}

func decodeManagedAgentSessionJSON(reader io.Reader, value any) error {
	decoder := json.NewDecoder(io.LimitReader(reader, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return errors.New("trailing JSON")
	}
	return nil
}

func managedAgentSessionPath(path string) (tenantID, projectID, sessionID, action string, ok bool) {
	if !strings.HasPrefix(path, ManagedAgentSessionRoutePrefix) {
		return "", "", "", "", false
	}
	parts := strings.Split(strings.TrimPrefix(path, ManagedAgentSessionRoutePrefix), "/")
	if len(parts) == 4 && parts[1] == "projects" && parts[3] == "sessions" && parts[0] != "" && parts[2] != "" {
		return parts[0], parts[2], "", "create", true
	}
	if len(parts) == 5 && parts[1] == "projects" && parts[3] == "sessions" && parts[0] != "" && parts[2] != "" && parts[4] != "" && !strings.Contains(parts[4], ":") {
		return parts[0], parts[2], parts[4], "get", true
	}
	if len(parts) == 5 && parts[1] == "projects" && parts[3] == "sessions" && parts[0] != "" && parts[2] != "" && strings.HasSuffix(parts[4], ":close") {
		sessionID = strings.TrimSuffix(parts[4], ":close")
		if sessionID != "" {
			return parts[0], parts[2], sessionID, "close", true
		}
	}
	return "", "", "", "", false
}

func HandlesManagedAgentSessionPath(path string) bool {
	_, _, _, _, ok := managedAgentSessionPath(path)
	return ok
}

func managedAgentSessionErrorStatus(err error) (int, string) {
	switch {
	case errors.Is(err, postgres.ErrManagedAgentSessionNotFound):
		return http.StatusNotFound, "not_found"
	case errors.Is(err, postgres.ErrMutationDenied):
		return http.StatusForbidden, "authorization_denied"
	case errors.Is(err, postgres.ErrCoordinationRejected):
		return http.StatusConflict, "session_conflict"
	case errors.Is(err, postgres.ErrCoordinationInvalidInput):
		return http.StatusBadRequest, "invalid_request"
	default:
		return http.StatusInternalServerError, "internal_error"
	}
}

func writeManagedAgentSessionError(writer http.ResponseWriter, status int, code string) {
	if code == "" {
		status, code = http.StatusInternalServerError, "internal_error"
	}
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(map[string]string{"apiVersion": "managed-agent.cloud-agents.dev/v1alpha1", "kind": "Error", "code": code})
}
