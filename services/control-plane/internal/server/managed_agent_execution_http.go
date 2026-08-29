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
	"github.com/hxp0618/cloud-agents/services/worker/runtime"
)

const ManagedAgentExecutionRoutePrefix = "/v1/tenants/"

var ErrInvalidManagedAgentExecutionHTTPServer = errors.New("managed agent execution HTTP server configuration is invalid")
var errManagedAgentExecutionAuthentication = errors.New("managed agent execution authentication failed")

type managedAgentExecutionStore interface {
	internalmanagedagent.DurableRuntimeExecutionStore
	GetManagedAgentExecution(context.Context, string, *authn.VerifiedPrincipal, string, string, string, string) (internalmanagedagent.ExecutionSnapshot, error)
}

type managedAgentExecutionRunner interface {
	Execute(context.Context, internalmanagedagent.VerifiedPrincipalSource, internalmanagedagent.DurableRuntimeExecutionInput) (internalmanagedagent.DurableRuntimeExecutionResult, error)
	Interrupt(context.Context, *authn.VerifiedPrincipal, internalmanagedagent.InterruptTurnInput) (internalmanagedagent.ExecutionTransitionResult, error)
	Cancel(context.Context, *authn.VerifiedPrincipal, internalmanagedagent.CancelTurnInput) (internalmanagedagent.ExecutionTransitionResult, error)
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
	preparePublicRequestID(writer, request)
	if server == nil || server.verifier == nil || server.store == nil || server.runner == nil || request == nil {
		writeManagedAgentSessionError(writer, http.StatusInternalServerError, "internal_error")
		return
	}
	tenantID, projectID, sessionID, turnID, executionID, action, ok := managedAgentExecutionPath(request.URL.Path)
	if !ok {
		writeManagedAgentSessionError(writer, http.StatusNotFound, "route_not_found")
		return
	}
	requestID, requestIDOK := validatedManagedAgentRequestID(writer, request)
	if !requestIDOK {
		writeManagedAgentSessionError(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	if err := validateManagedAgentScope(tenantID, projectID); err != nil || sessionID == "" || commonv1alpha1.ValidateIdentifier(sessionID, "/sessionId") != nil || turnID != "" && commonv1alpha1.ValidateIdentifier(turnID, "/turnId") != nil || executionID != "" && commonv1alpha1.ValidateIdentifier(executionID, "/executionId") != nil {
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
	if action == "cancel" && request.Method == http.MethodPost {
		server.cancel(writer, request, tenantID, projectID, sessionID, turnID, executionID, requestID, bearer)
		return
	}
	if action == "interrupt" && request.Method == http.MethodPost {
		server.interrupt(writer, request, tenantID, projectID, sessionID, turnID, executionID, requestID, bearer)
		return
	}
	writer.Header().Set("Allow", http.MethodGet+", "+http.MethodPost)
	writeManagedAgentSessionError(writer, http.StatusMethodNotAllowed, "method_not_allowed")
}

func (server *ManagedAgentExecutionHTTPServer) execute(writer http.ResponseWriter, request *http.Request, tenantID, projectID, sessionID, requestID, bearer string) {
	idempotencyKey, ok := exactSingleHeader(request.Header, "Idempotency-Key")
	if !ok || commonv1alpha1.ValidateIdempotencyKey(idempotencyKey, "/Idempotency-Key") != nil {
		writeManagedAgentSessionError(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	fields, err := decodeManagedAgentJSON(request.Body, &managedAgentExecutionRequest{}, []string{"turnId", "executionId", "model", "inputText"}, []string{"turnId", "executionId", "inputText"})
	if err != nil {
		writeManagedAgentSessionError(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	turnID, err := managedAgentIdentifierField(fields, "turnId", "/turnId", 128)
	if err != nil {
		writeManagedAgentSessionError(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	executionID, err := managedAgentIdentifierField(fields, "executionId", "/executionId", 128)
	if err != nil {
		writeManagedAgentSessionError(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	inputText, err := managedAgentStringField(fields, "inputText", "/inputText", 1, 1<<20)
	if err != nil {
		writeManagedAgentSessionError(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	model := ""
	if _, present := fields["model"]; present {
		model, err = managedAgentStringField(fields, "model", "/model", 1, 128)
		if err != nil {
			writeManagedAgentSessionError(writer, http.StatusBadRequest, "invalid_request")
			return
		}
	}
	principal, err := server.verifier.Verify(bearer, authn.VerificationRequest{TenantID: tenantID, ResourceLevel: "project", ResourceID: projectID, RequiredPermission: "projects.act"})
	if err != nil {
		writeManagedAgentSessionError(writer, http.StatusUnauthorized, "authentication_failed")
		return
	}
	verificationRequest := authn.VerificationRequest{TenantID: tenantID, ResourceLevel: "project", ResourceID: projectID, RequiredPermission: "projects.act"}
	principalSource := internalmanagedagent.VerifiedPrincipalSource(func() (*authn.VerifiedPrincipal, error) {
		if principal != nil {
			next := principal
			principal = nil
			return next, nil
		}
		next, verifyErr := server.verifier.Verify(bearer, verificationRequest)
		if verifyErr != nil {
			return nil, errManagedAgentExecutionAuthentication
		}
		return next, nil
	})
	result, err := server.runner.Execute(request.Context(), principalSource, internalmanagedagent.DurableRuntimeExecutionInput{
		Scope: internalmanagedagent.Scope{TenantID: tenantID, ProjectID: projectID}, SessionID: sessionID,
		TurnID: turnID, ExecutionID: executionID, Model: model, InputText: inputText,
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

func (server *ManagedAgentExecutionHTTPServer) cancel(writer http.ResponseWriter, request *http.Request, tenantID, projectID, sessionID, turnID, executionID, requestID, bearer string) {
	idempotencyKey, ok := exactSingleHeader(request.Header, "Idempotency-Key")
	if !ok || commonv1alpha1.ValidateIdempotencyKey(idempotencyKey, "/Idempotency-Key") != nil {
		writeManagedAgentSessionError(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	var body managedAgentExecutionCancelBody
	if _, err := decodeManagedAgentJSON(request.Body, &body, []string{"generation"}, []string{"generation"}); err != nil || body.Generation == 0 || body.Generation > maxManagedAgentGeneration {
		writeManagedAgentSessionError(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	principal, err := server.verifier.Verify(bearer, authn.VerificationRequest{TenantID: tenantID, ResourceLevel: "project", ResourceID: projectID, RequiredPermission: "projects.act"})
	if err != nil {
		writeManagedAgentSessionError(writer, http.StatusUnauthorized, "authentication_failed")
		return
	}
	result, err := server.runner.Cancel(request.Context(), principal, internalmanagedagent.CancelTurnInput{
		Scope: internalmanagedagent.Scope{TenantID: tenantID, ProjectID: projectID}, SessionID: sessionID, TurnID: turnID, TargetExecutionID: executionID,
		Generation: body.Generation, Mutation: internalmanagedagent.Mutation{RequestID: requestID, IdempotencyKey: idempotencyKey},
	})
	if err != nil {
		status, code := managedAgentExecutionErrorStatus(err)
		writeManagedAgentSessionError(writer, status, code)
		return
	}
	writeManagedAgentExecution(writer, http.StatusOK, requestID, result, nil)
}

func (server *ManagedAgentExecutionHTTPServer) interrupt(writer http.ResponseWriter, request *http.Request, tenantID, projectID, sessionID, turnID, executionID, requestID, bearer string) {
	idempotencyKey, ok := exactSingleHeader(request.Header, "Idempotency-Key")
	if !ok || commonv1alpha1.ValidateIdempotencyKey(idempotencyKey, "/Idempotency-Key") != nil {
		writeManagedAgentSessionError(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	var body managedAgentExecutionInterruptBody
	if _, err := decodeManagedAgentJSON(request.Body, &body, []string{"generation"}, []string{"generation"}); err != nil || body.Generation == 0 || body.Generation > maxManagedAgentGeneration {
		writeManagedAgentSessionError(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	principal, err := server.verifier.Verify(bearer, authn.VerificationRequest{TenantID: tenantID, ResourceLevel: "project", ResourceID: projectID, RequiredPermission: "projects.act"})
	if err != nil {
		writeManagedAgentSessionError(writer, http.StatusUnauthorized, "authentication_failed")
		return
	}
	result, err := server.runner.Interrupt(request.Context(), principal, internalmanagedagent.InterruptTurnInput{
		Scope: internalmanagedagent.Scope{TenantID: tenantID, ProjectID: projectID}, SessionID: sessionID, TurnID: turnID, TargetExecutionID: executionID,
		Generation: body.Generation, Mutation: internalmanagedagent.Mutation{RequestID: requestID, IdempotencyKey: idempotencyKey},
	})
	if err != nil {
		status, code := managedAgentExecutionErrorStatus(err)
		writeManagedAgentSessionError(writer, status, code)
		return
	}
	writeManagedAgentExecution(writer, http.StatusOK, requestID, result, nil)
}

type managedAgentExecutionRequest struct {
	TurnID      string `json:"turnId"`
	ExecutionID string `json:"executionId"`
	Model       string `json:"model,omitempty"`
	InputText   string `json:"inputText"`
}

type managedAgentExecutionCancelBody struct {
	Generation uint64 `json:"generation"`
}

type managedAgentExecutionInterruptBody struct {
	Generation uint64 `json:"generation"`
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

const maxManagedAgentGeneration = uint64(9223372036854775807)

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
	if len(parts) == 9 && parts[1] == "projects" && parts[3] == "sessions" && parts[5] == "turns" && parts[7] == "executions" && strings.HasSuffix(parts[8], ":cancel") && parts[0] != "" && parts[2] != "" && parts[4] != "" && parts[6] != "" && !strings.Contains(parts[6], ":") {
		executionID = strings.TrimSuffix(parts[8], ":cancel")
		if executionID != "" {
			return parts[0], parts[2], parts[4], parts[6], executionID, "cancel", true
		}
	}
	if len(parts) == 9 && parts[1] == "projects" && parts[3] == "sessions" && parts[5] == "turns" && parts[7] == "executions" && strings.HasSuffix(parts[8], ":interrupt") && parts[0] != "" && parts[2] != "" && parts[4] != "" && parts[6] != "" && !strings.Contains(parts[6], ":") {
		executionID = strings.TrimSuffix(parts[8], ":interrupt")
		if executionID != "" {
			return parts[0], parts[2], parts[4], parts[6], executionID, "interrupt", true
		}
	}
	return "", "", "", "", "", "", false
}

func HandlesManagedAgentExecutionPath(path string) bool {
	_, _, _, _, _, _, ok := managedAgentExecutionPath(path)
	return ok
}

func managedAgentExecutionErrorStatus(err error) (int, string) {
	switch {
	case errors.Is(err, errManagedAgentExecutionAuthentication):
		return http.StatusUnauthorized, "authentication_failed"
	case errors.Is(err, postgres.ErrManagedAgentExecutionNotFound):
		return http.StatusNotFound, "not_found"
	case errors.Is(err, postgres.ErrMutationDenied):
		return http.StatusForbidden, "authorization_denied"
	case errors.Is(err, postgres.ErrCoordinationRejected):
		return http.StatusConflict, "execution_conflict"
	case errors.Is(err, internalmanagedagent.ErrDurableRuntimeExecutionConflict):
		return http.StatusConflict, "execution_in_progress"
	case errors.Is(err, postgres.ErrCoordinationInvalidInput), errors.Is(err, internalmanagedagent.ErrInvalidInput):
		return http.StatusBadRequest, "invalid_request"
	case errors.Is(err, internalmanagedagent.ErrDurableRuntimeExecutionFailed):
		return http.StatusBadGateway, "runtime_failed"
	case errors.Is(err, context.Canceled):
		return 499, "cancelled"
	default:
		return http.StatusInternalServerError, "internal_error"
	}
}
