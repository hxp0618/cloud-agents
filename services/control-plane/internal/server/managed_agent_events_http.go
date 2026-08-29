package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	commonv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/common/v1alpha1"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authn"
	internalmanagedagent "github.com/hxp0618/cloud-agents/services/control-plane/internal/managedagent"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/store/postgres"
)

const ManagedAgentEventsRouteSuffix = "/events"

var ErrInvalidManagedAgentEventsHTTPServer = errors.New("managed agent events HTTP server configuration is invalid")

type managedAgentEventsStore interface {
	GetManagedAgentEvents(context.Context, string, *authn.VerifiedPrincipal, string, string, internalmanagedagent.EventCursor, int) (internalmanagedagent.EventPage, error)
}

type ManagedAgentEventsHTTPServer struct {
	verifier AccessTokenVerifier
	store    managedAgentEventsStore
}

func NewManagedAgentEventsHTTPServer(verifier AccessTokenVerifier, store managedAgentEventsStore) (*ManagedAgentEventsHTTPServer, error) {
	if verifier == nil || store == nil {
		return nil, ErrInvalidManagedAgentEventsHTTPServer
	}
	return &ManagedAgentEventsHTTPServer{verifier: verifier, store: store}, nil
}

func (server *ManagedAgentEventsHTTPServer) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	preparePublicRequestID(writer, request)
	if server == nil || server.verifier == nil || server.store == nil || request == nil {
		writeManagedAgentSessionError(writer, http.StatusInternalServerError, "internal_error")
		return
	}
	tenantID, projectID, sessionID, ok := managedAgentEventsPath(request.URL.Path)
	if !ok {
		writeManagedAgentSessionError(writer, http.StatusNotFound, "route_not_found")
		return
	}
	if request.Method != http.MethodGet {
		writer.Header().Set("Allow", http.MethodGet)
		writeManagedAgentSessionError(writer, http.StatusMethodNotAllowed, "method_not_allowed")
		return
	}
	requestID, requestIDOK := validatedManagedAgentRequestID(writer, request)
	if !requestIDOK {
		writeManagedAgentSessionError(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	if err := validateManagedAgentScope(tenantID, projectID); err != nil || commonv1alpha1.ValidateIdentifier(sessionID, "/sessionId") != nil {
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
	query := request.URL.Query()
	if len(query["cursor"]) > 1 || len(query["limit"]) > 1 {
		writeManagedAgentSessionError(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	var cursor internalmanagedagent.EventCursor
	if token := query.Get("cursor"); token != "" {
		var err error
		cursor, err = internalmanagedagent.DecodeEventCursor(token)
		if err != nil {
			writeManagedAgentSessionError(writer, http.StatusBadRequest, "invalid_request")
			return
		}
	}
	limit := 64
	if value := query.Get("limit"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 1 || parsed > 64 {
			writeManagedAgentSessionError(writer, http.StatusBadRequest, "invalid_request")
			return
		}
		limit = parsed
	}
	principal, err := server.verifier.Verify(bearer, authn.VerificationRequest{TenantID: tenantID, ResourceLevel: "project", ResourceID: projectID, RequiredPermission: "projects.get"})
	if err != nil {
		writeManagedAgentSessionError(writer, http.StatusUnauthorized, "authentication_failed")
		return
	}
	page, err := server.store.GetManagedAgentEvents(request.Context(), tenantID, principal, projectID, sessionID, cursor, limit)
	if err != nil {
		status, code := managedAgentEventsErrorStatus(err)
		writeManagedAgentSessionError(writer, status, code)
		return
	}
	writeManagedAgentEvents(writer, requestID, sessionID, page)
}

func managedAgentEventsPath(path string) (tenantID, projectID, sessionID string, ok bool) {
	const prefix = ManagedAgentSessionRoutePrefix
	if !strings.HasPrefix(path, prefix) {
		return "", "", "", false
	}
	parts := strings.Split(strings.TrimPrefix(path, prefix), "/")
	if len(parts) != 6 || parts[1] != "projects" || parts[3] != "sessions" || parts[5] != "events" || parts[0] == "" || parts[2] == "" || parts[4] == "" {
		return "", "", "", false
	}
	return parts[0], parts[2], parts[4], true
}

func HandlesManagedAgentEventsPath(path string) bool {
	_, _, _, ok := managedAgentEventsPath(path)
	return ok
}

type managedAgentEventResponse struct {
	APIVersion string                    `json:"apiVersion"`
	Kind       string                    `json:"kind"`
	Metadata   managedAgentEventMetadata `json:"metadata"`
	Spec       managedAgentEventSpec     `json:"spec"`
}

type managedAgentEventPageResponse struct {
	APIVersion string                      `json:"apiVersion"`
	Kind       string                      `json:"kind"`
	Events     []managedAgentEventResponse `json:"events"`
	NextCursor string                      `json:"nextCursor"`
	HasMore    bool                        `json:"hasMore"`
}

type managedAgentEventMetadata struct {
	UID        string `json:"uid"`
	ProjectID  string `json:"projectId"`
	SessionID  string `json:"sessionId"`
	Sequence   string `json:"sequence"`
	OccurredAt string `json:"occurredAt"`
}

type managedAgentEventSpec struct {
	Operation      string                    `json:"operation"`
	Resource       string                    `json:"resource"`
	Generation     uint64                    `json:"generation"`
	MutationDigest string                    `json:"mutationDigest"`
	InputDigest    string                    `json:"inputDigest,omitempty"`
	ResultDigest   string                    `json:"resultDigest,omitempty"`
	ErrorCode      string                    `json:"errorCode,omitempty"`
	TurnID         string                    `json:"turnId,omitempty"`
	ExecutionID    string                    `json:"executionId,omitempty"`
	Changes        []managedAgentEventChange `json:"changes"`
}

type managedAgentEventChange struct {
	Resource string `json:"resource"`
	From     string `json:"from"`
	To       string `json:"to"`
	Version  uint64 `json:"version"`
}

func writeManagedAgentEvents(writer http.ResponseWriter, requestID, sessionID string, page internalmanagedagent.EventPage) {
	nextCursor := ""
	if page.NextCursor != (internalmanagedagent.EventCursor{}) {
		var err error
		nextCursor, err = internalmanagedagent.EncodeEventCursor(page.NextCursor)
		if err != nil {
			writeManagedAgentSessionError(writer, http.StatusInternalServerError, "internal_error")
			return
		}
	}
	events := make([]managedAgentEventResponse, 0, len(page.Events))
	for _, event := range page.Events {
		changes := make([]managedAgentEventChange, 0, len(event.Changes))
		for _, change := range event.Changes {
			changes = append(changes, managedAgentEventChange{Resource: managedAgentEventResourceName(change.Resource), From: change.From, To: change.To, Version: change.Version})
		}
		events = append(events, managedAgentEventResponse{APIVersion: "managed-agent.cloud-agents.dev/v1alpha1", Kind: "Event", Metadata: managedAgentEventMetadata{UID: event.EventID, ProjectID: event.Scope.ProjectID, SessionID: sessionID, Sequence: strconv.FormatUint(event.Sequence, 10), OccurredAt: event.OccurredAt.UTC().Format("2006-01-02T15:04:05.999999999Z07:00")}, Spec: managedAgentEventSpec{Operation: event.Operation, Resource: managedAgentEventResourceName(event.Resource), Generation: event.Generation, MutationDigest: event.MutationDigest, InputDigest: event.InputDigest, ResultDigest: event.ResultDigest, ErrorCode: event.ErrorCode, TurnID: event.TurnID, ExecutionID: event.ExecutionID, Changes: changes}})
	}
	writer.Header().Set("X-Request-ID", requestID)
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(writer).Encode(managedAgentEventPageResponse{APIVersion: "managed-agent.cloud-agents.dev/v1alpha1", Kind: "EventPage", Events: events, NextCursor: nextCursor, HasMore: page.HasMore})
}

func managedAgentEventResourceName(resource internalmanagedagent.ResourceKind) string {
	switch resource {
	case internalmanagedagent.ResourceSession:
		return "Session"
	case internalmanagedagent.ResourceTurn:
		return "Turn"
	case internalmanagedagent.ResourceExecution:
		return "Execution"
	default:
		return ""
	}
}

func managedAgentEventsErrorStatus(err error) (int, string) {
	switch {
	case errors.Is(err, postgres.ErrManagedAgentEventsNotFound):
		return http.StatusNotFound, "not_found"
	case errors.Is(err, postgres.ErrMutationDenied):
		return http.StatusForbidden, "authorization_denied"
	case errors.Is(err, postgres.ErrCoordinationInvalidInput):
		return http.StatusBadRequest, "invalid_request"
	default:
		return http.StatusInternalServerError, "internal_error"
	}
}
