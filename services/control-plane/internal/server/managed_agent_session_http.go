package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	commonv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/common/v1alpha1"
	openapiv1 "github.com/hxp0618/cloud-agents/sdk/go/gen/openapi/v1alpha1"
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
	ListManagedAgentSessions(context.Context, string, *authn.VerifiedPrincipal, string, string, int) (postgres.ManagedAgentSessionPage, error)
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
	preparePublicRequestID(writer, request)
	if server == nil || server.verifier == nil || server.store == nil || request == nil {
		writeManagedAgentSessionError(writer, http.StatusInternalServerError, "internal_error")
		return
	}
	tenantID, projectID, sessionID, action, ok := managedAgentSessionPath(request.URL.Path)
	if !ok {
		writeManagedAgentSessionError(writer, http.StatusNotFound, "route_not_found")
		return
	}
	requestID, requestIDOK := validatedManagedAgentRequestID(writer, request)
	if !requestIDOK {
		writeManagedAgentSessionError(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	if err := validateManagedAgentScope(tenantID, projectID); err != nil || sessionID != "" && commonv1alpha1.ValidateIdentifier(sessionID, "/sessionId") != nil {
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
	case action == "collection" && request.Method == http.MethodGet:
		server.list(writer, request, tenantID, projectID, requestID, bearer)
	case action == "collection" && request.Method == http.MethodPost:
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

func (server *ManagedAgentSessionHTTPServer) list(writer http.ResponseWriter, request *http.Request, tenantID, projectID, requestID, bearer string) {
	pageSize, pageToken, ok := managedAgentSessionPagination(request)
	if !ok {
		writeManagedAgentSessionError(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	validated, err := openapiv1.ValidateListManagedAgentSessionsServerRequest(tenantID, projectID, requestID, pageSize, pageToken)
	if err != nil {
		writeManagedAgentSessionError(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	afterSessionID := ""
	if validated.PageToken != "" {
		if afterSessionID, ok = decodeManagedAgentSessionPageToken(validated.TenantID, validated.ProjectID, validated.PageToken); !ok {
			writeManagedAgentSessionError(writer, http.StatusBadRequest, "invalid_request")
			return
		}
	}
	principal, err := server.verifier.Verify(bearer, authn.VerificationRequest{TenantID: validated.TenantID, ResourceLevel: "project", ResourceID: validated.ProjectID, RequiredPermission: "projects.get"})
	if err != nil {
		writeManagedAgentSessionError(writer, http.StatusUnauthorized, "authentication_failed")
		return
	}
	page, err := server.store.ListManagedAgentSessions(request.Context(), validated.TenantID, principal, validated.ProjectID, afterSessionID, validated.PageSize)
	if err != nil {
		status, code := managedAgentSessionErrorStatus(err)
		writeManagedAgentSessionError(writer, status, code)
		return
	}
	writeManagedAgentSessionPage(writer, requestID, tenantID, projectID, page)
}

func (server *ManagedAgentSessionHTTPServer) create(writer http.ResponseWriter, request *http.Request, tenantID, projectID, requestID, bearer string) {
	idempotencyKey, ok := exactSingleHeader(request.Header, "Idempotency-Key")
	if !ok || commonv1alpha1.ValidateIdempotencyKey(idempotencyKey, "/Idempotency-Key") != nil {
		writeManagedAgentSessionError(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	fields, err := decodeManagedAgentJSON(request.Body, &managedAgentSessionCreateBody{}, []string{"sessionId", "providerKind"}, []string{"sessionId", "providerKind"})
	if err != nil {
		writeManagedAgentSessionError(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	sessionID, err := managedAgentIdentifierField(fields, "sessionId", "/sessionId", 128)
	if err != nil {
		writeManagedAgentSessionError(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	providerKind, err := managedAgentIdentifierField(fields, "providerKind", "/providerKind", 64)
	if err != nil {
		writeManagedAgentSessionError(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	principal, err := server.verifier.Verify(bearer, authn.VerificationRequest{TenantID: tenantID, ResourceLevel: "project", ResourceID: projectID, RequiredPermission: "projects.act"})
	if err != nil {
		writeManagedAgentSessionError(writer, http.StatusUnauthorized, "authentication_failed")
		return
	}
	snapshot, err := server.store.CreateManagedAgentSession(request.Context(), tenantID, principal, internalmanagedagent.CreateSessionInput{
		Scope: internalmanagedagent.Scope{TenantID: tenantID, ProjectID: projectID}, SessionID: sessionID, ProviderKind: providerKind,
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
	if !ok || commonv1alpha1.ValidateIdempotencyKey(idempotencyKey, "/Idempotency-Key") != nil {
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

type managedAgentSessionPageResource struct {
	APIVersion    string                        `json:"apiVersion"`
	Kind          string                        `json:"kind"`
	Sessions      []managedAgentSessionResource `json:"sessions"`
	NextPageToken string                        `json:"nextPageToken,omitempty"`
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

func writeManagedAgentSessionPage(writer http.ResponseWriter, requestID, tenantID, projectID string, page postgres.ManagedAgentSessionPage) {
	sessions := make([]managedAgentSessionResource, 0, len(page.Sessions))
	for _, snapshot := range page.Sessions {
		sessions = append(sessions, managedAgentSessionResource{
			APIVersion: "managed-agent.cloud-agents.dev/v1alpha1", Kind: "Session",
			Metadata: managedAgentSessionResourceMetadata{UID: snapshot.SessionID, ProjectID: snapshot.Scope.ProjectID, ResourceVersion: strconv.FormatUint(snapshot.Version, 10), CreatedAt: snapshot.CreatedAt.UTC().Format("2006-01-02T15:04:05.999999999Z07:00"), UpdatedAt: snapshot.UpdatedAt.UTC().Format("2006-01-02T15:04:05.999999999Z07:00")},
			Spec:     managedAgentSessionResourceSpec{ProviderKind: snapshot.ProviderKind, State: string(snapshot.State)},
		})
	}
	nextPageToken := ""
	if page.NextSessionID != "" {
		var ok bool
		nextPageToken, ok = encodeManagedAgentSessionPageToken(tenantID, projectID, page.NextSessionID)
		if !ok {
			writeManagedAgentSessionError(writer, http.StatusInternalServerError, "internal_error")
			return
		}
	}
	writer.Header().Set("X-Request-ID", requestID)
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(writer).Encode(managedAgentSessionPageResource{
		APIVersion: "managed-agent.cloud-agents.dev/v1alpha1", Kind: "SessionPage", Sessions: sessions, NextPageToken: nextPageToken,
	})
}

const (
	managedAgentMaximumInputBytes = 1 << 20
	// JSON escaping can expand a valid input string up to sixfold.
	managedAgentMaximumBodyBytes = 6*managedAgentMaximumInputBytes + 4*1024
)

func validatedManagedAgentRequestID(writer http.ResponseWriter, request *http.Request) (string, bool) {
	if request == nil {
		if writer != nil {
			writer.Header().Set("X-Request-ID", publicFallbackRequestID)
		}
		return "", false
	}
	requestID, ok := exactSingleHeader(request.Header, "X-Request-ID")
	if !ok || commonv1alpha1.ValidateIdentifier(requestID, "/Request-ID") != nil {
		if writer != nil {
			writer.Header().Set("X-Request-ID", publicFallbackRequestID)
		}
		return "", false
	}
	return requestID, true
}

func validateManagedAgentScope(tenantID, projectID string) error {
	if err := commonv1alpha1.ValidateIdentifier(tenantID, "/tenantId"); err != nil {
		return err
	}
	return commonv1alpha1.ValidateIdentifier(projectID, "/projectId")
}

func decodeManagedAgentJSON(reader io.Reader, value any, allowed, required []string) (map[string]json.RawMessage, error) {
	if reader == nil {
		return nil, errors.New("managed agent request body is nil")
	}
	body, err := io.ReadAll(io.LimitReader(reader, managedAgentMaximumBodyBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) > managedAgentMaximumBodyBytes {
		return nil, errors.New("managed agent request body exceeds limit")
	}
	fields, err := commonv1alpha1.DecodeStrictObject(body, allowed, required)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(body, value); err != nil {
		return nil, err
	}
	return fields, nil
}

func managedAgentStringField(fields map[string]json.RawMessage, key, path string, minimum, maximum int) (string, error) {
	raw, ok := fields[key]
	if !ok {
		return "", commonv1alpha1.ContractError("MISSING_FIELD", path)
	}
	var value *string
	if err := json.Unmarshal(raw, &value); err != nil || value == nil {
		return "", commonv1alpha1.ContractError("INVALID_FIELD_TYPE", path)
	}
	if err := commonv1alpha1.ValidateString(*value, minimum, maximum, path); err != nil {
		return "", err
	}
	return *value, nil
}

func managedAgentIdentifierField(fields map[string]json.RawMessage, key, path string, maximum int) (string, error) {
	value, err := managedAgentStringField(fields, key, path, 1, maximum)
	if err != nil {
		return "", err
	}
	if err := commonv1alpha1.ValidateIdentifier(value, path); err != nil {
		return "", err
	}
	return value, nil
}

func managedAgentSessionPath(path string) (tenantID, projectID, sessionID, action string, ok bool) {
	if !strings.HasPrefix(path, ManagedAgentSessionRoutePrefix) {
		return "", "", "", "", false
	}
	parts := strings.Split(strings.TrimPrefix(path, ManagedAgentSessionRoutePrefix), "/")
	if len(parts) == 4 && parts[1] == "projects" && parts[3] == "sessions" && parts[0] != "" && parts[2] != "" {
		return parts[0], parts[2], "", "collection", true
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

func managedAgentSessionPagination(request *http.Request) (int, string, bool) {
	pageSize := 50
	pageToken := ""
	for name, values := range request.URL.Query() {
		if len(values) != 1 || values[0] == "" {
			return 0, "", false
		}
		switch name {
		case "pageSize":
			value, err := strconv.Atoi(values[0])
			if err != nil {
				return 0, "", false
			}
			pageSize = value
		case "pageToken":
			pageToken = values[0]
		default:
			return 0, "", false
		}
	}
	return pageSize, pageToken, true
}

func encodeManagedAgentSessionPageToken(tenantID, projectID, sessionID string) (string, bool) {
	if commonv1alpha1.ValidateIdentifier(tenantID, "/tenantId") != nil || commonv1alpha1.ValidateIdentifier(projectID, "/projectId") != nil || commonv1alpha1.ValidateIdentifier(sessionID, "/sessionId") != nil {
		return "", false
	}
	token := base64.RawURLEncoding.EncodeToString([]byte("session/v1\x00" + tenantID + "\x00" + projectID + "\x00" + sessionID))
	return token, commonv1alpha1.ValidatePageToken(token, "/pageToken") == nil
}

func decodeManagedAgentSessionPageToken(tenantID, projectID, token string) (string, bool) {
	if commonv1alpha1.ValidatePageToken(token, "/pageToken") != nil {
		return "", false
	}
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(token)
	if err != nil {
		return "", false
	}
	parts := strings.Split(string(decoded), "\x00")
	if len(parts) != 4 || parts[0] != "session/v1" || parts[1] != tenantID || parts[2] != projectID ||
		commonv1alpha1.ValidateIdentifier(parts[1], "/tenantId") != nil || commonv1alpha1.ValidateIdentifier(parts[2], "/projectId") != nil || commonv1alpha1.ValidateIdentifier(parts[3], "/sessionId") != nil {
		return "", false
	}
	return parts[3], true
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
	writePublicProblem(writer, status, code)
}
