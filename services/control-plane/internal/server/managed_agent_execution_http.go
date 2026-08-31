package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	commonv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/common/v1alpha1"
	openapiv1 "github.com/hxp0618/cloud-agents/sdk/go/gen/openapi/v1alpha1"
	runtimeprotocol "github.com/hxp0618/cloud-agents/sdk/go/runtime"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authn"
	internalmanagedagent "github.com/hxp0618/cloud-agents/services/control-plane/internal/managedagent"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/store/postgres"
)

const ManagedAgentExecutionRoutePrefix = "/v1/tenants/"

var ErrInvalidManagedAgentExecutionHTTPServer = errors.New("managed agent execution HTTP server configuration is invalid")
var errManagedAgentExecutionAuthentication = errors.New("managed agent execution authentication failed")

type managedAgentExecutionStore interface {
	internalmanagedagent.DurableRuntimeExecutionStore
	GetManagedAgentExecution(context.Context, string, *authn.VerifiedPrincipal, string, string, string, string) (internalmanagedagent.ExecutionSnapshot, error)
	ListManagedAgentExecutions(context.Context, string, *authn.VerifiedPrincipal, string, string, string, int) (postgres.ManagedAgentExecutionPage, error)
}

type managedAgentExecutionRunner interface {
	Execute(context.Context, internalmanagedagent.VerifiedPrincipalSource, internalmanagedagent.DurableRuntimeExecutionInput) (internalmanagedagent.DurableRuntimeExecutionResult, error)
	Interrupt(context.Context, *authn.VerifiedPrincipal, internalmanagedagent.InterruptTurnInput) (internalmanagedagent.ExecutionTransitionResult, error)
	Cancel(context.Context, *authn.VerifiedPrincipal, internalmanagedagent.CancelTurnInput) (internalmanagedagent.ExecutionTransitionResult, error)
	ActiveInteractions(internalmanagedagent.RuntimeExecutionReference) []runtimeprotocol.Message
	ResolveApproval(context.Context, internalmanagedagent.RuntimeApprovalResolutionInput) error
	ResolveUserInput(context.Context, internalmanagedagent.RuntimeUserInputResolutionInput) error
	ReadArtifact(context.Context, internalmanagedagent.RuntimeArtifactReadInput) (internalmanagedagent.RuntimeArtifact, error)
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
	messageIndex := -1
	if artifactTenant, artifactProject, artifactSession, artifactTurn, artifactExecution, artifactIndex, artifactOK := managedAgentArtifactPath(request.URL.Path); artifactOK {
		tenantID, projectID, sessionID, turnID, executionID, action, ok = artifactTenant, artifactProject, artifactSession, artifactTurn, artifactExecution, "artifact", true
		messageIndex = artifactIndex
	}
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
	if action == "execute" && request.Method == http.MethodGet {
		server.list(writer, request, tenantID, projectID, sessionID, requestID, bearer)
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
	if action == "artifact" && request.Method == http.MethodGet {
		server.downloadArtifact(writer, request, tenantID, projectID, sessionID, turnID, executionID, requestID, bearer, messageIndex)
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
	if (action == "resolveApproval" || action == "resolveUserInput") && request.Method == http.MethodPost {
		server.resolveInteraction(writer, request, tenantID, projectID, sessionID, turnID, executionID, requestID, bearer, action)
		return
	}
	writer.Header().Set("Allow", http.MethodGet+", "+http.MethodPost)
	writeManagedAgentSessionError(writer, http.StatusMethodNotAllowed, "method_not_allowed")
}

func (server *ManagedAgentExecutionHTTPServer) list(writer http.ResponseWriter, request *http.Request, tenantID, projectID, sessionID, requestID, bearer string) {
	pageSize, pageToken, ok := managedAgentPagination(request)
	if !ok {
		writeManagedAgentSessionError(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	validated, err := openapiv1.ValidateListManagedAgentExecutionsServerRequest(tenantID, projectID, sessionID, requestID, pageSize, pageToken)
	if err != nil {
		writeManagedAgentSessionError(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	afterTurnID := ""
	if validated.PageToken != "" {
		if afterTurnID, ok = decodeManagedAgentExecutionPageToken(validated.TenantID, validated.ProjectID, validated.SessionID, validated.PageToken); !ok {
			writeManagedAgentSessionError(writer, http.StatusBadRequest, "invalid_request")
			return
		}
	}
	principal, err := server.verifier.Verify(bearer, authn.VerificationRequest{TenantID: validated.TenantID, ResourceLevel: "project", ResourceID: validated.ProjectID, RequiredPermission: "projects.get"})
	if err != nil {
		writeManagedAgentSessionError(writer, http.StatusUnauthorized, "authentication_failed")
		return
	}
	page, err := server.store.ListManagedAgentExecutions(request.Context(), validated.TenantID, principal, validated.ProjectID, validated.SessionID, afterTurnID, validated.PageSize)
	if err != nil {
		status, code := managedAgentExecutionErrorStatus(err)
		writeManagedAgentSessionError(writer, status, code)
		return
	}
	writeManagedAgentExecutionPage(writer, requestID, tenantID, projectID, sessionID, page)
}

func (server *ManagedAgentExecutionHTTPServer) execute(writer http.ResponseWriter, request *http.Request, tenantID, projectID, sessionID, requestID, bearer string) {
	idempotencyKey, ok := exactSingleHeader(request.Header, "Idempotency-Key")
	if !ok || commonv1alpha1.ValidateIdempotencyKey(idempotencyKey, "/Idempotency-Key") != nil {
		writeManagedAgentSessionError(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	fields, err := decodeManagedAgentJSON(request.Body, &managedAgentExecutionRequest{}, []string{"turnId", "executionId", "model", "runtimeMode", "interactionMode", "inputText"}, []string{"turnId", "executionId", "inputText"})
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
	inputText, err := managedAgentStringField(fields, "inputText", "/inputText", 1, managedAgentMaximumInputBytes)
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
	runtimeMode := "full-access"
	if _, present := fields["runtimeMode"]; present {
		runtimeMode, err = managedAgentStringField(fields, "runtimeMode", "/runtimeMode", 1, 32)
		if err != nil || runtimeMode != "approval-required" && runtimeMode != "full-access" {
			writeManagedAgentSessionError(writer, http.StatusBadRequest, "invalid_request")
			return
		}
	}
	interactionMode := "default"
	if _, present := fields["interactionMode"]; present {
		interactionMode, err = managedAgentStringField(fields, "interactionMode", "/interactionMode", 1, 16)
		if err != nil || interactionMode != "default" && interactionMode != "plan" {
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
		TurnID: turnID, ExecutionID: executionID, Model: model, RuntimeMode: runtimeMode, InteractionMode: interactionMode, InputText: inputText,
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
	messages := append([]runtimeprotocol.Message(nil), execution.Messages...)
	if execution.State == internalmanagedagent.ExecutionRunning {
		messages = append(messages, server.runner.ActiveInteractions(runtimeExecutionReference(tenantID, projectID, sessionID, turnID, executionID, execution.Generation))...)
	}
	writeManagedAgentExecution(writer, http.StatusOK, requestID, internalmanagedagent.ExecutionTransitionResult{Execution: execution}, messages)
}

func (server *ManagedAgentExecutionHTTPServer) downloadArtifact(writer http.ResponseWriter, request *http.Request, tenantID, projectID, sessionID, turnID, executionID, requestID, bearer string, messageIndex int) {
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
	if messageIndex < 0 || messageIndex >= len(execution.Messages) || execution.Messages[messageIndex].MessageType != "ArtifactCandidate" {
		writeManagedAgentSessionError(writer, http.StatusNotFound, "not_found")
		return
	}
	artifact, err := server.runner.ReadArtifact(request.Context(), internalmanagedagent.RuntimeArtifactReadInput{
		RuntimeExecutionReference: runtimeExecutionReference(tenantID, projectID, sessionID, turnID, executionID, execution.Generation),
		Message:                   execution.Messages[messageIndex],
	})
	if err != nil {
		status, code := managedAgentExecutionErrorStatus(err)
		writeManagedAgentSessionError(writer, status, code)
		return
	}
	writer.Header().Set("X-Request-ID", requestID)
	writer.Header().Set("Content-Type", artifact.ContentType)
	writer.Header().Set("Content-Length", strconv.Itoa(len(artifact.Data)))
	writer.Header().Set("X-Content-Type-Options", "nosniff")
	if artifact.SHA256 != "" {
		writer.Header().Set("ETag", `"sha256:`+artifact.SHA256+`"`)
	}
	writer.WriteHeader(http.StatusOK)
	_, _ = writer.Write(artifact.Data)
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

func (server *ManagedAgentExecutionHTTPServer) resolveInteraction(writer http.ResponseWriter, request *http.Request, tenantID, projectID, sessionID, turnID, executionID, requestID, bearer, action string) {
	principal, err := server.verifier.Verify(bearer, authn.VerificationRequest{TenantID: tenantID, ResourceLevel: "project", ResourceID: projectID, RequiredPermission: "projects.act"})
	if err != nil || principal == nil {
		writeManagedAgentSessionError(writer, http.StatusUnauthorized, "authentication_failed")
		return
	}
	reference := runtimeExecutionReference(tenantID, projectID, sessionID, turnID, executionID, 0)
	if action == "resolveApproval" {
		var body managedAgentApprovalResolutionBody
		if _, err := decodeManagedAgentJSON(request.Body, &body, []string{"generation", "requestId", "decision"}, []string{"generation", "requestId", "decision"}); err != nil || !validManagedAgentInteractionGenerationAndRequest(body.Generation, body.RequestID) || body.Decision != "accept" && body.Decision != "decline" {
			writeManagedAgentSessionError(writer, http.StatusBadRequest, "invalid_request")
			return
		}
		reference.Generation = body.Generation
		err = server.runner.ResolveApproval(request.Context(), internalmanagedagent.RuntimeApprovalResolutionInput{RuntimeExecutionReference: reference, RequestID: requestID, InteractionRequestID: body.RequestID, Decision: body.Decision})
	} else {
		var body managedAgentUserInputResolutionBody
		if _, err := decodeManagedAgentJSON(request.Body, &body, []string{"generation", "requestId", "answers"}, []string{"generation", "requestId", "answers"}); err != nil || !validManagedAgentInteractionGenerationAndRequest(body.Generation, body.RequestID) || !validManagedAgentInteractionAnswers(body.Answers) {
			writeManagedAgentSessionError(writer, http.StatusBadRequest, "invalid_request")
			return
		}
		reference.Generation = body.Generation
		err = server.runner.ResolveUserInput(request.Context(), internalmanagedagent.RuntimeUserInputResolutionInput{RuntimeExecutionReference: reference, RequestID: requestID, InteractionRequestID: body.RequestID, Answers: body.Answers})
	}
	if err != nil {
		status, code := managedAgentExecutionErrorStatus(err)
		writeManagedAgentSessionError(writer, status, code)
		return
	}
	writer.Header().Set("X-Request-ID", requestID)
	writer.WriteHeader(http.StatusNoContent)
}

func runtimeExecutionReference(tenantID, projectID, sessionID, turnID, executionID string, generation uint64) internalmanagedagent.RuntimeExecutionReference {
	return internalmanagedagent.RuntimeExecutionReference{Scope: internalmanagedagent.Scope{TenantID: tenantID, ProjectID: projectID}, SessionID: sessionID, TurnID: turnID, ExecutionID: executionID, Generation: generation}
}

type managedAgentExecutionRequest struct {
	TurnID          string `json:"turnId"`
	ExecutionID     string `json:"executionId"`
	Model           string `json:"model,omitempty"`
	RuntimeMode     string `json:"runtimeMode,omitempty"`
	InteractionMode string `json:"interactionMode,omitempty"`
	InputText       string `json:"inputText"`
}

type managedAgentExecutionCancelBody struct {
	Generation uint64 `json:"generation"`
}

type managedAgentExecutionInterruptBody struct {
	Generation uint64 `json:"generation"`
}

type managedAgentApprovalResolutionBody struct {
	Generation uint64 `json:"generation"`
	RequestID  string `json:"requestId"`
	Decision   string `json:"decision"`
}

type managedAgentUserInputResolutionBody struct {
	Generation uint64              `json:"generation"`
	RequestID  string              `json:"requestId"`
	Answers    map[string][]string `json:"answers"`
}

func validManagedAgentInteractionGenerationAndRequest(generation uint64, requestID string) bool {
	return generation > 0 && generation <= maxManagedAgentGeneration && validManagedAgentInteractionToken(requestID, 200)
}

func validManagedAgentInteractionAnswers(answers map[string][]string) bool {
	if len(answers) == 0 || len(answers) > 3 {
		return false
	}
	for questionID, values := range answers {
		if !validManagedAgentInteractionToken(questionID, 200) || len(values) == 0 || len(values) > 20 {
			return false
		}
		for _, value := range values {
			if value == "" || !utf8.ValidString(value) || utf8.RuneCountInString(value) > 2000 || strings.ContainsRune(value, '\x00') {
				return false
			}
		}
	}
	return true
}

func validManagedAgentInteractionToken(value string, maximum int) bool {
	if value == "" || !utf8.ValidString(value) || utf8.RuneCountInString(value) > maximum {
		return false
	}
	for _, character := range value {
		if character < 0x20 || character == 0x7f {
			return false
		}
	}
	return true
}

type managedAgentExecutionResource struct {
	APIVersion string                        `json:"apiVersion"`
	Kind       string                        `json:"kind"`
	Metadata   managedAgentExecutionMetadata `json:"metadata"`
	Spec       managedAgentExecutionSpec     `json:"spec"`
	Messages   []runtimeprotocol.Message     `json:"messages,omitempty"`
}

type managedAgentExecutionPageResource struct {
	APIVersion    string                          `json:"apiVersion"`
	Kind          string                          `json:"kind"`
	Executions    []managedAgentExecutionResource `json:"executions"`
	NextPageToken string                          `json:"nextPageToken,omitempty"`
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

func writeManagedAgentExecution(writer http.ResponseWriter, status int, requestID string, transition internalmanagedagent.ExecutionTransitionResult, messages []runtimeprotocol.Message) {
	execution := transition.Execution
	writer.Header().Set("X-Request-ID", requestID)
	writer.Header().Set("X-Resource-Version", strconv.FormatUint(execution.Version, 10))
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(managedAgentExecutionResourceFromSnapshot(execution, messages))
}

func managedAgentExecutionResourceFromSnapshot(execution internalmanagedagent.ExecutionSnapshot, messages []runtimeprotocol.Message) managedAgentExecutionResource {
	return managedAgentExecutionResource{
		APIVersion: "managed-agent.cloud-agents.dev/v1alpha1", Kind: "Execution",
		Metadata: managedAgentExecutionMetadata{UID: execution.ExecutionID, ProjectID: execution.Scope.ProjectID, SessionID: execution.SessionID, TurnID: execution.TurnID, ResourceVersion: strconv.FormatUint(execution.Version, 10), CreatedAt: execution.CreatedAt.UTC().Format(timeFormat), UpdatedAt: execution.UpdatedAt.UTC().Format(timeFormat)},
		Spec:     managedAgentExecutionSpec{Generation: execution.Generation, State: string(execution.State), ResultDigest: execution.ResultDigest, ErrorCode: execution.ErrorCode},
		Messages: messages,
	}
}

func writeManagedAgentExecutionPage(writer http.ResponseWriter, requestID, tenantID, projectID, sessionID string, page postgres.ManagedAgentExecutionPage) {
	executions := make([]managedAgentExecutionResource, 0, len(page.Executions))
	for _, snapshot := range page.Executions {
		executions = append(executions, managedAgentExecutionResourceFromSnapshot(snapshot, nil))
	}
	nextPageToken := ""
	if page.NextTurnID != "" {
		var ok bool
		nextPageToken, ok = encodeManagedAgentExecutionPageToken(tenantID, projectID, sessionID, page.NextTurnID)
		if !ok {
			writeManagedAgentSessionError(writer, http.StatusInternalServerError, "internal_error")
			return
		}
	}
	writer.Header().Set("X-Request-ID", requestID)
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(writer).Encode(managedAgentExecutionPageResource{
		APIVersion: "managed-agent.cloud-agents.dev/v1alpha1", Kind: "ExecutionPage", Executions: executions, NextPageToken: nextPageToken,
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
	if len(parts) == 9 && parts[1] == "projects" && parts[3] == "sessions" && parts[5] == "turns" && parts[7] == "executions" && parts[0] != "" && parts[2] != "" && parts[4] != "" && parts[6] != "" && !strings.Contains(parts[6], ":") {
		for _, candidate := range []struct{ suffix, action string }{{":cancel", "cancel"}, {":interrupt", "interrupt"}, {":resolveApproval", "resolveApproval"}, {":resolveUserInput", "resolveUserInput"}} {
			executionID = strings.TrimSuffix(parts[8], candidate.suffix)
			if executionID != parts[8] && executionID != "" {
				return parts[0], parts[2], parts[4], parts[6], executionID, candidate.action, true
			}
		}
	}
	return "", "", "", "", "", "", false
}

func managedAgentArtifactPath(value string) (tenantID, projectID, sessionID, turnID, executionID string, messageIndex int, ok bool) {
	if !strings.HasPrefix(value, ManagedAgentExecutionRoutePrefix) {
		return "", "", "", "", "", 0, false
	}
	parts := strings.Split(strings.TrimPrefix(value, ManagedAgentExecutionRoutePrefix), "/")
	if len(parts) != 12 || parts[1] != "projects" || parts[3] != "sessions" || parts[5] != "turns" || parts[7] != "executions" || parts[9] != "messages" || parts[11] != "artifact" {
		return "", "", "", "", "", 0, false
	}
	for _, index := range []int{0, 2, 4, 6, 8} {
		if parts[index] == "" || strings.Contains(parts[index], ":") {
			return "", "", "", "", "", 0, false
		}
	}
	parsed, err := strconv.Atoi(parts[10])
	if err != nil || parsed < 0 || parsed >= 64 || strconv.Itoa(parsed) != parts[10] {
		return "", "", "", "", "", 0, false
	}
	return parts[0], parts[2], parts[4], parts[6], parts[8], parsed, true
}

func encodeManagedAgentExecutionPageToken(tenantID, projectID, sessionID, turnID string) (string, bool) {
	if commonv1alpha1.ValidateIdentifier(tenantID, "/tenantId") != nil || commonv1alpha1.ValidateIdentifier(projectID, "/projectId") != nil ||
		commonv1alpha1.ValidateIdentifier(sessionID, "/sessionId") != nil || commonv1alpha1.ValidateIdentifier(turnID, "/turnId") != nil {
		return "", false
	}
	token := base64.RawURLEncoding.EncodeToString([]byte("execution/v1\x00" + tenantID + "\x00" + projectID + "\x00" + sessionID + "\x00" + turnID))
	return token, commonv1alpha1.ValidatePageToken(token, "/pageToken") == nil
}

func decodeManagedAgentExecutionPageToken(tenantID, projectID, sessionID, token string) (string, bool) {
	if commonv1alpha1.ValidatePageToken(token, "/pageToken") != nil {
		return "", false
	}
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(token)
	if err != nil {
		return "", false
	}
	parts := strings.Split(string(decoded), "\x00")
	if len(parts) != 5 || parts[0] != "execution/v1" || parts[1] != tenantID || parts[2] != projectID || parts[3] != sessionID ||
		commonv1alpha1.ValidateIdentifier(parts[1], "/tenantId") != nil || commonv1alpha1.ValidateIdentifier(parts[2], "/projectId") != nil ||
		commonv1alpha1.ValidateIdentifier(parts[3], "/sessionId") != nil || commonv1alpha1.ValidateIdentifier(parts[4], "/turnId") != nil {
		return "", false
	}
	return parts[4], true
}

func HandlesManagedAgentExecutionPath(path string) bool {
	_, _, _, _, _, _, ok := managedAgentExecutionPath(path)
	if ok {
		return true
	}
	_, _, _, _, _, _, ok = managedAgentArtifactPath(path)
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
	case errors.Is(err, internalmanagedagent.ErrRuntimeInteractionUnavailable), errors.Is(err, internalmanagedagent.ErrRuntimeInteractionConflict):
		return http.StatusConflict, "interaction_not_pending"
	case errors.Is(err, postgres.ErrCoordinationInvalidInput), errors.Is(err, internalmanagedagent.ErrInvalidInput):
		return http.StatusBadRequest, "invalid_request"
	case errors.Is(err, internalmanagedagent.ErrRuntimeInteractionFailed):
		return http.StatusBadGateway, "runtime_interaction_failed"
	case errors.Is(err, internalmanagedagent.ErrRuntimeArtifactUnavailable):
		return http.StatusBadGateway, "artifact_unavailable"
	case errors.Is(err, context.DeadlineExceeded):
		return http.StatusGatewayTimeout, "deadline_exceeded"
	case errors.Is(err, internalmanagedagent.ErrDurableRuntimeExecutionFailed):
		return http.StatusBadGateway, "runtime_failed"
	case errors.Is(err, context.Canceled):
		return 499, "cancelled"
	default:
		return http.StatusInternalServerError, "internal_error"
	}
}
