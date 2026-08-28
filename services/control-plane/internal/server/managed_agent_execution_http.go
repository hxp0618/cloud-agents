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
	"github.com/hxp0618/cloud-agents/services/worker/runtime"
)

const ManagedAgentExecutionRoutePrefix = "/v1/tenants/"

var ErrInvalidManagedAgentExecutionHTTPServer = errors.New("managed agent execution HTTP server configuration is invalid")

type managedAgentExecutionStore interface {
	internalmanagedagent.DurableRuntimeExecutionStore
	GetManagedAgentExecution(context.Context, string, *authn.VerifiedPrincipal, string, string, string, string) (internalmanagedagent.ExecutionSnapshot, error)
}

type managedAgentExecutionRunner interface {
	Execute(context.Context, *authn.VerifiedPrincipal, internalmanagedagent.DurableRuntimeExecutionInput) (internalmanagedagent.DurableRuntimeExecutionResult, error)
}

type ManagedAgentExecutionHTTPServer struct {
	verifier AccessTokenVerifier
	store    managedAgentExecutionStore
	runner   managedAgentExecutionRunner
}

func NewManagedAgentExecutionHTTPServer(verifier AccessTokenVerifier, store managedAgentExecutionStore, runner managedAgentExecutionRunner) (*ManagedAgentExecutionHTTPServer, error) {
	if verifier == nil || store == nil || runner == nil {
		return nil, ErrInvalidManagedAgentExecutionHTTPServer
	}
	return &ManagedAgentExecutionHTTPServer{verifier: verifier, store: store, runner: runner}, nil
}

func (server *ManagedAgentExecutionHTTPServer) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	if server == nil || server.verifier == nil || server.store == nil || server.runner == nil || request == nil {
		writeManagedAgentSessionError(writer, http.StatusInternalServerError, "internal_error")
		return
	}
	tenantID, projectID, sessionID, turnID, executionID, action, ok := managedAgentExecutionPath(request.URL.Path)
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
	if action == "execute" && request.Method == http.MethodPost {
		server.execute(writer, request, tenantID, projectID, sessionID, requestID, bearer)
		return
	}
	if action == "get" && request.Method == http.MethodGet {
		server.get(writer, request, tenantID, projectID, sessionID, turnID, executionID, requestID, bearer)
		return
	}
	writer.Header().Set("Allow", http.MethodGet+", "+http.MethodPost)
	writeManagedAgentSessionError(writer, http.StatusMethodNotAllowed, "method_not_allowed")
}

func (server *ManagedAgentExecutionHTTPServer) execute(writer http.ResponseWriter, request *http.Request, tenantID, projectID, sessionID, requestID, bearer string) {
	idempotencyKey, ok := exactSingleHeader(request.Header, "Idempotency-Key")
	if !ok {
		writeManagedAgentSessionError(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	var body managedAgentExecutionRequest
	if err := decodeManagedAgentExecutionJSON(request.Body, &body); err != nil || body.TurnID == "" || body.ExecutionID == "" || body.InputText == "" {
		writeManagedAgentSessionError(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	principal, err := server.verifier.Verify(bearer, authn.VerificationRequest{TenantID: tenantID, ResourceLevel: "project", ResourceID: projectID, RequiredPermission: "projects.act"})
	if err != nil {
		writeManagedAgentSessionError(writer, http.StatusUnauthorized, "authentication_failed")
		return
	}
	result, err := server.runner.Execute(request.Context(), principal, internalmanagedagent.DurableRuntimeExecutionInput{
		Scope: internalmanagedagent.Scope{TenantID: tenantID, ProjectID: projectID}, SessionID: sessionID,
		TurnID: body.TurnID, ExecutionID: body.ExecutionID, Model: body.Model, InputText: body.InputText,
		Mutation: internalmanagedagent.Mutation{RequestID: requestID, IdempotencyKey: idempotencyKey},
	})
	if err != nil {
		status, code := managedAgentExecutionErrorStatus(err)
		writeManagedAgentSessionError(writer, status, code)
		return
	}
	writeManagedAgentExecution(writer, http.StatusOK, requestID, result.Transition, result.Messages)
}

func (server *ManagedAgentExecutionHTTPServer) get(writer http.ResponseWriter, request *http.Request, tenantID, projectID, sessionID, turnID, executionID, requestID, bearer string) {
	principal, err := server.verifier.Verify(bearer, authn.VerificationRequest{TenantID: tenantID, ResourceLevel: "project", ResourceID: projectID, RequiredPermission: "projects.get"})
	if err != nil {
		writeManagedAgentSessionError(writer, http.StatusUnauthorized, "authentication_failed")
		return
	}
	execution, err := server.store.GetManagedAgentExecution(request.Context(), tenantID, principal, projectID, sessionID, turnID, executionID)
	if err != nil {
		status, code := managedAgentExecutionErrorStatus(err)
		writeManagedAgentSessionError(writer, status, code)
		return
	}
	writeManagedAgentExecution(writer, http.StatusOK, requestID, internalmanagedagent.ExecutionTransitionResult{Execution: execution}, nil)
}

type managedAgentExecutionRequest struct {
	TurnID      string `json:"turnId"`
	ExecutionID string `json:"executionId"`
	Model       string `json:"model,omitempty"`
	InputText   string `json:"inputText"`
}

type managedAgentExecutionResource struct {
	APIVersion string                        `json:"apiVersion"`
	Kind       string                        `json:"kind"`
	Metadata   managedAgentExecutionMetadata `json:"metadata"`
	Spec       managedAgentExecutionSpec     `json:"spec"`
	Messages   []runtime.Message             `json:"messages,omitempty"`
}

type managedAgentExecutionMetadata struct {
	UID             string `json:"uid"`
	ProjectID       string `json:"projectId"`
	SessionID       string `json:"sessionId"`
	TurnID          string `json:"turnId"`
	ResourceVersion string `json:"resourceVersion"`
	CreatedAt       string `json:"createdAt"`
	UpdatedAt       string `json:"updatedAt"`
}

type managedAgentExecutionSpec struct {
	Generation   uint64 `json:"generation"`
	State        string `json:"state"`
	ResultDigest string `json:"resultDigest,omitempty"`
	ErrorCode    string `json:"errorCode,omitempty"`
}

func writeManagedAgentExecution(writer http.ResponseWriter, status int, requestID string, transition internalmanagedagent.ExecutionTransitionResult, messages []runtime.Message) {
	execution := transition.Execution
	writer.Header().Set("X-Request-ID", requestID)
	writer.Header().Set("X-Resource-Version", strconv.FormatUint(execution.Version, 10))
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(managedAgentExecutionResource{
		APIVersion: "managed-agent.cloud-agents.dev/v1alpha1", Kind: "Execution",
		Metadata: managedAgentExecutionMetadata{UID: execution.ExecutionID, ProjectID: execution.Scope.ProjectID, SessionID: execution.SessionID, TurnID: execution.TurnID, ResourceVersion: strconv.FormatUint(execution.Version, 10), CreatedAt: execution.CreatedAt.UTC().Format(timeFormat), UpdatedAt: execution.UpdatedAt.UTC().Format(timeFormat)},
		Spec:     managedAgentExecutionSpec{Generation: execution.Generation, State: string(execution.State), ResultDigest: execution.ResultDigest, ErrorCode: execution.ErrorCode},
		Messages: messages,
	})
}

const timeFormat = "2006-01-02T15:04:05.999999999Z07:00"

func decodeManagedAgentExecutionJSON(reader io.Reader, value any) error {
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

func managedAgentExecutionPath(path string) (tenantID, projectID, sessionID, turnID, executionID, action string, ok bool) {
	if !strings.HasPrefix(path, ManagedAgentExecutionRoutePrefix) {
		return "", "", "", "", "", "", false
	}
	parts := strings.Split(strings.TrimPrefix(path, ManagedAgentExecutionRoutePrefix), "/")
	if len(parts) == 6 && parts[1] == "projects" && parts[3] == "sessions" && parts[5] == "executions" && parts[0] != "" && parts[2] != "" && parts[4] != "" {
		return parts[0], parts[2], parts[4], "", "", "execute", true
	}
	if len(parts) == 9 && parts[1] == "projects" && parts[3] == "sessions" && parts[5] == "turns" && parts[7] == "executions" && parts[8] != "" && parts[0] != "" && parts[2] != "" && parts[4] != "" && parts[6] != "" && !strings.Contains(parts[6], ":") && !strings.Contains(parts[8], ":") {
		return parts[0], parts[2], parts[4], parts[6], parts[8], "get", true
	}
	return "", "", "", "", "", "", false
}

func HandlesManagedAgentExecutionPath(path string) bool {
	_, _, _, _, _, _, ok := managedAgentExecutionPath(path)
	return ok
}

func managedAgentExecutionErrorStatus(err error) (int, string) {
	switch {
	case errors.Is(err, postgres.ErrManagedAgentExecutionNotFound):
		return http.StatusNotFound, "not_found"
	case errors.Is(err, postgres.ErrMutationDenied):
		return http.StatusForbidden, "authorization_denied"
	case errors.Is(err, postgres.ErrCoordinationRejected):
		return http.StatusConflict, "execution_conflict"
	case errors.Is(err, postgres.ErrCoordinationInvalidInput), errors.Is(err, internalmanagedagent.ErrInvalidInput):
		return http.StatusBadRequest, "invalid_request"
	case errors.Is(err, internalmanagedagent.ErrDurableRuntimeExecutionFailed):
		return http.StatusBadGateway, "runtime_failed"
	default:
		return http.StatusInternalServerError, "internal_error"
	}
}
