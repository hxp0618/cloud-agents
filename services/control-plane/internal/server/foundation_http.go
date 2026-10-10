package server

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	common "github.com/hxp0618/cloud-agents/sdk/go/gen/common/v1alpha1"
	openapi "github.com/hxp0618/cloud-agents/sdk/go/gen/openapi/v1alpha1"
	platform "github.com/hxp0618/cloud-agents/sdk/go/gen/platform/v1alpha1"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/accessgrant"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authn"
	internalcoordination "github.com/hxp0618/cloud-agents/services/control-plane/internal/coordination"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/opensandbox"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/store/postgres"
)

type foundationStore interface {
	CreateRuntimeProfile(context.Context, string, *authn.VerifiedPrincipal, internalcoordination.RuntimeProfileCreateInput) (internalcoordination.RuntimeProfileSnapshot, error)
	TransitionRuntimeProfile(context.Context, string, *authn.VerifiedPrincipal, internalcoordination.RuntimeProfileTransitionInput) (internalcoordination.RuntimeProfileSnapshot, error)
	GetRuntimeProfile(context.Context, string, *authn.VerifiedPrincipal, string, string, int64) (internalcoordination.RuntimeProfileSnapshot, error)
	ListRuntimeProfiles(context.Context, string, *authn.VerifiedPrincipal, string, string, int) (postgres.RuntimeProfilePage, error)
	ListPublishedRuntimeProfiles(context.Context, string, *authn.VerifiedPrincipal, string, string, int) (postgres.PublishedRuntimeProfilePage, error)
	CreateFoundationSandbox(context.Context, string, *authn.VerifiedPrincipal, internalcoordination.FoundationSandboxCreateInput) (internalcoordination.FoundationSandboxSnapshot, error)
	PrepareFoundationSandboxExec(context.Context, string, *authn.VerifiedPrincipal, string, string, int64, string, string, string, int64) (postgres.FoundationSandboxExec, error)
	WaitRemoteWorkerSandboxExec(context.Context, postgres.FoundationSandboxExec) (postgres.FoundationSandboxExec, error)
	TransitionFoundationSandbox(context.Context, string, *authn.VerifiedPrincipal, internalcoordination.FoundationSandboxLifecycleInput) (internalcoordination.FoundationSandboxLifecycleOperation, error)
	GetAdminSandbox(context.Context, string, *authn.VerifiedPrincipal, string, string) (postgres.AdminSandboxSnapshot, error)
	ListAdminSandboxes(context.Context, string, *authn.VerifiedPrincipal, string, string, int) (postgres.AdminSandboxPage, error)
	CreateSandboxUsageCorrection(context.Context, string, *authn.VerifiedPrincipal, postgres.SandboxUsageCorrectionInput) (postgres.AdminSandboxSnapshot, error)
	CreateWorkspaceSnapshot(context.Context, string, *authn.VerifiedPrincipal, postgres.WorkspaceSnapshotCreateInput) (postgres.WorkspaceSnapshot, error)
	CleanupWorkspaceSnapshot(context.Context, string, *authn.VerifiedPrincipal, postgres.WorkspaceSnapshotCleanupInput) (postgres.WorkspaceSnapshot, error)
	RestoreWorkspaceSnapshot(context.Context, string, *authn.VerifiedPrincipal, postgres.WorkspaceSnapshotRestoreInput) (internalcoordination.FoundationSandboxSnapshot, error)
	GetWorkspaceSnapshot(context.Context, string, *authn.VerifiedPrincipal, string, string) (postgres.WorkspaceSnapshot, error)
	ListWorkspaceSnapshots(context.Context, string, *authn.VerifiedPrincipal, string, string, int) (postgres.WorkspaceSnapshotPage, error)
	IssueSandboxAccessGrant(context.Context, string, *authn.VerifiedPrincipal, postgres.SandboxAccessGrantIssueInput) (postgres.SandboxAccessGrantSnapshot, error)
	ListSandboxAccessGrants(context.Context, string, *authn.VerifiedPrincipal, string, string, string, int) (postgres.SandboxAccessGrantPage, error)
	RevokeSandboxAccessGrant(context.Context, string, *authn.VerifiedPrincipal, postgres.SandboxAccessGrantRevokeInput) (postgres.SandboxAccessGrantSnapshot, error)
}

type FoundationHTTPServer struct {
	verifier AccessTokenVerifier
	store    foundationStore
	access   *opensandbox.CredentialDirectory
	grants   *accessgrant.Codec
}

func NewFoundationHTTPServer(verifier AccessTokenVerifier, store foundationStore, access *opensandbox.CredentialDirectory, grants *accessgrant.Codec) (*FoundationHTTPServer, error) {
	if verifier == nil || store == nil {
		return nil, errors.New("foundation HTTP server configuration is invalid")
	}
	return &FoundationHTTPServer{verifier: verifier, store: store, access: access, grants: grants}, nil
}

func (server *FoundationHTTPServer) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	preparePublicRequestID(writer, request)
	if server == nil || server.verifier == nil || server.store == nil || request == nil {
		writePublicProblem(writer, http.StatusInternalServerError, "internal_error")
		return
	}
	admin, tenantID, projectID, profileID, secondaryID, version, action, ok := foundationPath(request.URL.Path)
	if !ok {
		writePublicProblem(writer, http.StatusNotFound, "route_not_found")
		return
	}
	projectPermission, permission, allowed := foundationPermission(admin, action, request.Method)
	if !allowed {
		writePublicProblem(writer, http.StatusMethodNotAllowed, "method_not_allowed")
		return
	}
	requestID, ok := exactSingleHeader(request.Header, "X-Request-ID")
	if !ok {
		writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	authorization, ok := exactSingleHeader(request.Header, "Authorization")
	if !ok {
		writePublicProblem(writer, http.StatusUnauthorized, "authentication_failed")
		return
	}
	bearer, ok := bearerToken(authorization)
	if !ok {
		writePublicProblem(writer, http.StatusUnauthorized, "authentication_failed")
		return
	}
	principal, err := server.verify(request.Context(), bearer, tenantID, projectID, projectPermission)
	if err != nil {
		writePublicProblem(writer, http.StatusUnauthorized, "authentication_failed")
		return
	}
	if _, err := server.verify(request.Context(), bearer, tenantID, projectID, permission); err != nil {
		writePublicProblem(writer, http.StatusForbidden, "authorization_denied")
		return
	}
	switch action {
	case "admin-collection":
		if request.Method == http.MethodPost {
			server.createProfile(writer, request, tenantID, projectID, requestID, principal)
		} else {
			server.listAdmin(writer, request, tenantID, projectID, requestID, principal)
		}
	case "public-list":
		server.listPublished(writer, request, tenantID, projectID, requestID, principal)
	case "create-profile":
		server.createProfile(writer, request, tenantID, projectID, requestID, principal)
	case "get-profile":
		server.getProfile(writer, request, tenantID, projectID, profileID, version, requestID, principal)
	case internalcoordination.RuntimeProfilePublish, internalcoordination.RuntimeProfileDisable:
		server.transitionProfile(writer, request, tenantID, projectID, profileID, version, action, requestID, principal)
	case "create-sandbox":
		server.createSandbox(writer, request, tenantID, projectID, requestID, principal)
	case "exec-sandbox":
		server.execSandbox(writer, request, tenantID, projectID, profileID, requestID, principal)
	case "issue-access-grant":
		server.issueSandboxAccessGrant(writer, request, tenantID, projectID, profileID, requestID, principal)
	case "admin-sandbox-collection":
		server.listAdminSandboxes(writer, request, tenantID, projectID, requestID, principal)
	case "get-admin-sandbox":
		server.getAdminSandbox(writer, request, tenantID, projectID, profileID, requestID, principal)
	case "correct-sandbox-usage":
		server.correctSandboxUsage(writer, request, tenantID, projectID, profileID, requestID, principal)
	case "admin-snapshot-collection":
		if request.Method == http.MethodPost {
			server.createWorkspaceSnapshot(writer, request, tenantID, projectID, requestID, principal)
		} else {
			server.listWorkspaceSnapshots(writer, request, tenantID, projectID, requestID, principal)
		}
	case "get-workspace-snapshot":
		server.getWorkspaceSnapshot(writer, request, tenantID, projectID, profileID, requestID, principal)
	case "restore-workspace-snapshot":
		server.restoreWorkspaceSnapshot(writer, request, tenantID, projectID, profileID, requestID, principal)
	case "cleanup-workspace-snapshot":
		server.cleanupWorkspaceSnapshot(writer, request, tenantID, projectID, profileID, requestID, principal)
	case "admin-access-grant-collection":
		server.listSandboxAccessGrants(writer, request, tenantID, projectID, profileID, requestID, principal)
	case "revoke-access-grant":
		server.revokeSandboxAccessGrant(writer, request, tenantID, projectID, profileID, secondaryID, requestID, principal)
	case internalcoordination.FoundationSandboxStop, internalcoordination.FoundationSandboxRebuild:
		server.transitionAdminSandbox(writer, request, tenantID, projectID, profileID, action, requestID, principal)
	}
}

func (server *FoundationHTTPServer) issueSandboxAccessGrant(writer http.ResponseWriter, request *http.Request, tenantID, projectID, sandboxID, requestID string, principal *authn.VerifiedPrincipal) {
	key, body, ok := foundationMutationBody(writer, request)
	if !ok {
		return
	}
	validated, err := openapi.ValidateCreateSandboxAccessGrantServerRequest(tenantID, projectID, sandboxID, requestID, key, body)
	if err != nil {
		writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	if server.grants == nil {
		writePublicProblem(writer, http.StatusServiceUnavailable, "sandbox_access_unavailable")
		return
	}
	grantID := accessgrant.GrantID(tenantID, projectID, sandboxID, key)
	token, err := server.grants.Token(grantID)
	if err != nil {
		writePublicProblem(writer, http.StatusServiceUnavailable, "sandbox_access_unavailable")
		return
	}
	digest, err := foundationRequestDigest(validated.Body)
	if err != nil {
		writePublicProblem(writer, http.StatusInternalServerError, "internal_error")
		return
	}
	result, err := server.store.IssueSandboxAccessGrant(request.Context(), tenantID, principal, postgres.SandboxAccessGrantIssueInput{
		Scope:     internalcoordination.FoundationScope{TenantID: tenantID, ProjectID: projectID},
		SandboxID: sandboxID, GrantID: grantID, ExpectedGeneration: validated.Body.ExpectedGeneration,
		TTLSeconds: int32(validated.Body.TTLSeconds), TokenDigest: accessgrant.Digest(token), RequestDigest: digest,
		Mutation: internalcoordination.FoundationMutation{RequestID: requestID, IdempotencyKey: key},
	})
	if err != nil {
		writeFoundationError(writer, err)
		return
	}
	sshUsername, err := accessgrant.SSHUsername(tenantID, projectID, result.GrantID)
	if err != nil {
		writePublicProblem(writer, http.StatusInternalServerError, "internal_error")
		return
	}
	value := platform.SandboxAccessGrant{APIVersion: platform.APIVersion, Kind: "SandboxAccessGrant",
		ProjectRef: common.ProjectRef{Namespace: "cloud-agents", Kind: "project", ID: projectID},
		GrantID:    result.GrantID, SandboxID: result.SandboxID, Generation: result.Generation,
		AccessKind: result.AccessKind, AccessToken: token, SSHUsername: sshUsername,
		ExpiresAt: result.ExpiresAt.UTC().Format(time.RFC3339Nano)}
	body, err = platform.EncodeSandboxAccessGrantResponseJSON(common.ResponseEnvelope[platform.SandboxAccessGrant]{Value: value})
	if err != nil {
		writePublicProblem(writer, http.StatusInternalServerError, "internal_error")
		return
	}
	writeJSONResponse(writer, http.StatusCreated, requestID, body)
}

func (server *FoundationHTTPServer) createProfile(writer http.ResponseWriter, request *http.Request, tenantID, projectID, requestID string, principal *authn.VerifiedPrincipal) {
	key, body, ok := foundationMutationBody(writer, request)
	if !ok {
		return
	}
	validated, err := openapi.ValidateCreateAdminRuntimeProfileServerRequest(tenantID, projectID, requestID, key, body)
	if err != nil {
		writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	input := validated.Body
	var selector *internalcoordination.RuntimeProfileTargetSelector
	if input.TargetSelector != nil {
		selector = &internalcoordination.RuntimeProfileTargetSelector{
			RegionID: input.TargetSelector.RegionID, ResourcePoolID: input.TargetSelector.ResourcePoolID,
			Runtime: input.TargetSelector.Runtime, Architecture: input.TargetSelector.Architecture,
		}
	}
	result, err := server.store.CreateRuntimeProfile(request.Context(), tenantID, principal, internalcoordination.RuntimeProfileCreateInput{
		Scope:     internalcoordination.FoundationScope{TenantID: tenantID, ProjectID: projectID},
		ProfileID: input.ProfileID, ProfileName: input.ProfileName, Version: input.Version,
		Description: input.Description, WorkloadTrust: input.WorkloadTrust, IsolationRuntime: input.IsolationRuntime,
		TargetID: input.TargetID, TargetSelector: selector, ImageURI: input.ImageURI,
		NetworkPolicyID: input.NetworkPolicyRef,
		ReleaseDigest:   input.ReleaseDigest, CPUMillis: input.CPUMillis, MemoryBytes: input.MemoryBytes,
		Mutation: internalcoordination.FoundationMutation{RequestID: requestID, IdempotencyKey: key},
	})
	if err != nil {
		writeFoundationError(writer, err)
		return
	}
	writeRuntimeProfile(writer, http.StatusCreated, requestID, result)
}

func (server *FoundationHTTPServer) transitionProfile(writer http.ResponseWriter, request *http.Request, tenantID, projectID, profileID string, version int64, action, requestID string, principal *authn.VerifiedPrincipal) {
	key, body, ok := foundationMutationBody(writer, request)
	if !ok {
		return
	}
	var transition platform.RuntimeProfileTransitionRequest
	var err error
	if action == internalcoordination.RuntimeProfilePublish {
		var validated openapi.TransitionAdminRuntimeProfileServerInput
		validated, err = openapi.ValidatePublishAdminRuntimeProfileServerRequest(tenantID, projectID, profileID, version, requestID, key, body)
		transition = validated.Body
	} else {
		var validated openapi.TransitionAdminRuntimeProfileServerInput
		validated, err = openapi.ValidateDisableAdminRuntimeProfileServerRequest(tenantID, projectID, profileID, version, requestID, key, body)
		transition = validated.Body
	}
	if err != nil {
		writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	expected, err := strconv.ParseInt(transition.ExpectedResourceVersion, 10, 64)
	if err != nil || expected < 1 {
		writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	result, err := server.store.TransitionRuntimeProfile(request.Context(), tenantID, principal, internalcoordination.RuntimeProfileTransitionInput{
		Scope:     internalcoordination.FoundationScope{TenantID: tenantID, ProjectID: projectID},
		ProfileID: profileID, Version: version, ExpectedResourceVersion: expected, Action: action,
		Mutation: internalcoordination.FoundationMutation{RequestID: requestID, IdempotencyKey: key},
	})
	if err != nil {
		writeFoundationError(writer, err)
		return
	}
	writeRuntimeProfile(writer, http.StatusOK, requestID, result)
}

func (server *FoundationHTTPServer) getProfile(writer http.ResponseWriter, request *http.Request, tenantID, projectID, profileID string, version int64, requestID string, principal *authn.VerifiedPrincipal) {
	if _, err := openapi.ValidateGetAdminRuntimeProfileServerRequest(tenantID, projectID, profileID, version, requestID); err != nil {
		writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	result, err := server.store.GetRuntimeProfile(request.Context(), tenantID, principal, projectID, profileID, version)
	if err != nil {
		writeFoundationError(writer, err)
		return
	}
	writeRuntimeProfile(writer, http.StatusOK, requestID, result)
}

func (server *FoundationHTTPServer) listAdmin(writer http.ResponseWriter, request *http.Request, tenantID, projectID, requestID string, principal *authn.VerifiedPrincipal) {
	pageSize, pageToken, ok := managedAgentPagination(request)
	if !ok {
		writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	validated, err := openapi.ValidateListAdminRuntimeProfilesServerRequest(tenantID, projectID, requestID, pageSize, pageToken)
	if err != nil {
		writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	after, ok := decodeFoundationPageToken("runtime-profile-admin/v1", tenantID, projectID, validated.PageToken)
	if !ok {
		writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	page, err := server.store.ListRuntimeProfiles(request.Context(), tenantID, principal, projectID, after, validated.PageSize)
	if err != nil {
		writeFoundationError(writer, err)
		return
	}
	profiles := make([]platform.RuntimeProfile, 0, len(page.Profiles))
	for _, profile := range page.Profiles {
		profiles = append(profiles, runtimeProfileResource(profile))
	}
	next, ok := encodeFoundationPageToken("runtime-profile-admin/v1", tenantID, projectID, page.NextProfileVersionID)
	if !ok {
		writePublicProblem(writer, http.StatusInternalServerError, "internal_error")
		return
	}
	body, err := platform.EncodeRuntimeProfilePageResponseJSON(common.ResponseEnvelope[platform.RuntimeProfilePage]{Value: platform.RuntimeProfilePage{APIVersion: platform.APIVersion, Kind: "RuntimeProfilePage", RuntimeProfiles: profiles, NextPageToken: next}})
	if err != nil {
		writePublicProblem(writer, http.StatusInternalServerError, "internal_error")
		return
	}
	writeJSONResponse(writer, http.StatusOK, requestID, body)
}

func (server *FoundationHTTPServer) listPublished(writer http.ResponseWriter, request *http.Request, tenantID, projectID, requestID string, principal *authn.VerifiedPrincipal) {
	pageSize, pageToken, ok := managedAgentPagination(request)
	if !ok {
		writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	validated, err := openapi.ValidateListRuntimeProfilesServerRequest(tenantID, projectID, requestID, pageSize, pageToken)
	if err != nil {
		writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	after, ok := decodeFoundationPageToken("runtime-profile-public/v1", tenantID, projectID, validated.PageToken)
	if !ok {
		writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	page, err := server.store.ListPublishedRuntimeProfiles(request.Context(), tenantID, principal, projectID, after, validated.PageSize)
	if err != nil {
		writeFoundationError(writer, err)
		return
	}
	profiles := make([]platform.RuntimeProfileSummary, 0, len(page.Profiles))
	for _, profile := range page.Profiles {
		profiles = append(profiles, runtimeProfileSummaryResource(profile))
	}
	next, ok := encodeFoundationPageToken("runtime-profile-public/v1", tenantID, projectID, page.NextProfileVersionID)
	if !ok {
		writePublicProblem(writer, http.StatusInternalServerError, "internal_error")
		return
	}
	body, err := platform.EncodeRuntimeProfileSummaryPageResponseJSON(common.ResponseEnvelope[platform.RuntimeProfileSummaryPage]{Value: platform.RuntimeProfileSummaryPage{APIVersion: platform.APIVersion, Kind: "RuntimeProfileSummaryPage", RuntimeProfiles: profiles, NextPageToken: next}})
	if err != nil {
		writePublicProblem(writer, http.StatusInternalServerError, "internal_error")
		return
	}
	writeJSONResponse(writer, http.StatusOK, requestID, body)
}

func (server *FoundationHTTPServer) createSandbox(writer http.ResponseWriter, request *http.Request, tenantID, projectID, requestID string, principal *authn.VerifiedPrincipal) {
	key, body, ok := foundationMutationBody(writer, request)
	if !ok {
		return
	}
	validated, err := openapi.ValidateCreateSandboxServerRequest(tenantID, projectID, requestID, key, body)
	if err != nil {
		writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	input := validated.Body
	result, err := server.store.CreateFoundationSandbox(request.Context(), tenantID, principal, internalcoordination.FoundationSandboxCreateInput{
		Scope:       internalcoordination.FoundationScope{TenantID: tenantID, ProjectID: projectID},
		WorkspaceID: input.WorkspaceID, WorkspaceName: input.WorkspaceName, SandboxID: input.SandboxID,
		RuntimeProfileID: input.RuntimeProfileID, RuntimeProfileVersion: input.RuntimeProfileVersion,
		TTLSeconds: input.TTLSeconds,
		Mutation:   internalcoordination.FoundationMutation{RequestID: requestID, IdempotencyKey: key},
	})
	if err != nil {
		writeFoundationError(writer, err)
		return
	}
	value := sandboxSessionResource(result)
	body, err = platform.EncodeSandboxSessionResponseJSON(common.ResponseEnvelope[platform.SandboxSession]{Value: value})
	if err != nil {
		writePublicProblem(writer, http.StatusInternalServerError, "internal_error")
		return
	}
	writeJSONResponse(writer, http.StatusAccepted, requestID, body)
}

func (server *FoundationHTTPServer) execSandbox(writer http.ResponseWriter, request *http.Request, tenantID, projectID, sandboxID, requestID string, principal *authn.VerifiedPrincipal) {
	body, err := io.ReadAll(http.MaxBytesReader(writer, request.Body, 16<<10))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writePublicProblem(writer, http.StatusRequestEntityTooLarge, "request_too_large")
		} else {
			writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
		}
		return
	}
	validated, err := openapi.ValidateExecSandboxServerRequest(tenantID, projectID, sandboxID, requestID, body)
	if err != nil {
		writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	requestDigest, err := foundationRequestDigest(validated.Body)
	if err != nil {
		writePublicProblem(writer, http.StatusInternalServerError, "internal_error")
		return
	}
	execution, err := server.store.PrepareFoundationSandboxExec(request.Context(), tenantID, principal, projectID, sandboxID,
		validated.Body.ExpectedGeneration, requestID, requestDigest, validated.Body.Command, validated.Body.TimeoutSeconds)
	if err != nil {
		writeFoundationError(writer, err)
		return
	}
	access := execution.Access
	result := opensandbox.ExecResult{}
	if access.TargetKind == "remote-worker" {
		execution, err = server.store.WaitRemoteWorkerSandboxExec(request.Context(), execution)
		if err == nil && execution.State == "failed" {
			err = remoteWorkerSandboxExecError(execution.StableErrorCode)
		}
		if err != nil {
			writeFoundationError(writer, err)
			return
		}
		result = opensandbox.ExecResult{ExitCode: execution.ExitCode, Stdout: execution.Stdout,
			Stderr: execution.Stderr, ExecutionTimeMillis: execution.ExecutionTimeMillis}
	} else {
		if server.access == nil {
			writePublicProblem(writer, http.StatusServiceUnavailable, "sandbox_access_unavailable")
			return
		}
		client, clientErr := server.access.Client(access.CredentialRef)
		if clientErr != nil {
			writeFoundationError(writer, clientErr)
			return
		}
		result, err = client.Exec(request.Context(), opensandbox.ExecInput{
			Identity: opensandbox.Identity{Tenant: access.Scope.TenantID, Project: access.Scope.ProjectID,
				Workspace: access.WorkspaceID, Sandbox: access.SandboxID, Operation: access.RuntimeOperationID,
				Generation: access.RuntimeGeneration, SpecDigest: access.RuntimeSpecDigest},
			RuntimeID: access.RuntimeID, Command: validated.Body.Command,
			Timeout: time.Duration(validated.Body.TimeoutSeconds) * time.Second,
		})
		if err != nil {
			writeFoundationError(writer, err)
			return
		}
	}
	value := platform.SandboxExecResult{APIVersion: platform.APIVersion, Kind: "SandboxExecResult",
		ProjectRef: common.ProjectRef{Namespace: "cloud-agents", Kind: "project", ID: projectID},
		SandboxID:  sandboxID, Generation: access.Generation, ExitCode: result.ExitCode,
		Stdout: result.Stdout, Stderr: result.Stderr, ExecutionTimeMillis: result.ExecutionTimeMillis}
	body, err = platform.EncodeSandboxExecResultResponseJSON(common.ResponseEnvelope[platform.SandboxExecResult]{Value: value})
	if err != nil {
		writePublicProblem(writer, http.StatusInternalServerError, "internal_error")
		return
	}
	writeJSONResponse(writer, http.StatusOK, requestID, body)
}

func remoteWorkerSandboxExecError(code string) error {
	switch code {
	case "sandbox_exec_output_limit":
		return opensandbox.ErrOutputLimit
	case "sandbox_exec_timeout":
		return context.DeadlineExceeded
	case "sandbox_runtime_unavailable":
		return opensandbox.ErrRuntimeFailed
	case "sandbox_access_unavailable":
		return opensandbox.ErrUnavailable
	default:
		return postgres.ErrCoordinationResultDrift
	}
}

func (server *FoundationHTTPServer) listAdminSandboxes(writer http.ResponseWriter, request *http.Request, tenantID, projectID, requestID string, principal *authn.VerifiedPrincipal) {
	pageSize, pageToken, ok := managedAgentPagination(request)
	if !ok {
		writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	validated, err := openapi.ValidateListAdminSandboxSessionsServerRequest(tenantID, projectID, requestID, pageSize, pageToken)
	if err != nil {
		writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	after, ok := decodeFoundationPageToken("admin-sandbox/v1", tenantID, projectID, validated.PageToken)
	if !ok {
		writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	page, err := server.store.ListAdminSandboxes(request.Context(), tenantID, principal, projectID, after, validated.PageSize)
	if err != nil {
		writeFoundationError(writer, err)
		return
	}
	values := make([]platform.AdminSandboxSession, 0, len(page.Sandboxes))
	for _, sandbox := range page.Sandboxes {
		values = append(values, adminSandboxResource(sandbox))
	}
	next, ok := encodeFoundationPageToken("admin-sandbox/v1", tenantID, projectID, page.NextSandboxID)
	if !ok {
		writePublicProblem(writer, http.StatusInternalServerError, "internal_error")
		return
	}
	body, err := platform.EncodeAdminSandboxSessionPageResponseJSON(common.ResponseEnvelope[platform.AdminSandboxSessionPage]{Value: platform.AdminSandboxSessionPage{APIVersion: platform.APIVersion, Kind: "AdminSandboxSessionPage", SandboxSessions: values, NextPageToken: next}})
	if err != nil {
		writePublicProblem(writer, http.StatusInternalServerError, "internal_error")
		return
	}
	writeJSONResponse(writer, http.StatusOK, requestID, body)
}

func (server *FoundationHTTPServer) getAdminSandbox(writer http.ResponseWriter, request *http.Request, tenantID, projectID, sandboxID, requestID string, principal *authn.VerifiedPrincipal) {
	if _, err := openapi.ValidateGetAdminSandboxSessionServerRequest(tenantID, projectID, sandboxID, requestID); err != nil {
		writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	value, err := server.store.GetAdminSandbox(request.Context(), tenantID, principal, projectID, sandboxID)
	if err != nil {
		writeFoundationError(writer, err)
		return
	}
	body, err := platform.EncodeAdminSandboxSessionResponseJSON(common.ResponseEnvelope[platform.AdminSandboxSession]{Value: adminSandboxResource(value)})
	if err != nil {
		writePublicProblem(writer, http.StatusInternalServerError, "internal_error")
		return
	}
	writer.Header().Set("X-Resource-Version", strconv.FormatInt(value.ResourceVersion, 10))
	writeJSONResponse(writer, http.StatusOK, requestID, body)
}

func (server *FoundationHTTPServer) correctSandboxUsage(writer http.ResponseWriter, request *http.Request, tenantID, projectID, sandboxID, requestID string, principal *authn.VerifiedPrincipal) {
	key, body, ok := foundationMutationBody(writer, request)
	if !ok {
		return
	}
	validated, err := openapi.ValidateCorrectAdminSandboxUsageServerRequest(tenantID, projectID, sandboxID, requestID, key, body)
	if err != nil {
		writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	expectedResourceVersion, err := strconv.ParseInt(validated.Body.ExpectedResourceVersion, 10, 64)
	if err != nil || expectedResourceVersion < 1 {
		writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	requestDigest, err := foundationRequestDigest(validated.Body)
	if err != nil {
		writePublicProblem(writer, http.StatusInternalServerError, "internal_error")
		return
	}
	value, err := server.store.CreateSandboxUsageCorrection(request.Context(), tenantID, principal, postgres.SandboxUsageCorrectionInput{
		Scope: internalcoordination.FoundationScope{TenantID: tenantID, ProjectID: projectID}, SandboxID: sandboxID,
		ConfirmedSandboxID: validated.Body.ConfirmedSandboxID, Metric: validated.Body.Metric,
		Adjustment: validated.Body.Adjustment, ReasonCode: validated.Body.ReasonCode, RequestDigest: requestDigest,
		ExpectedGeneration: validated.Body.ExpectedGeneration, ExpectedResourceVersion: expectedResourceVersion,
		Mutation: internalcoordination.FoundationMutation{RequestID: requestID, IdempotencyKey: key},
	})
	if err != nil {
		writeFoundationError(writer, err)
		return
	}
	body, err = platform.EncodeAdminSandboxSessionResponseJSON(common.ResponseEnvelope[platform.AdminSandboxSession]{Value: adminSandboxResource(value)})
	if err != nil {
		writePublicProblem(writer, http.StatusInternalServerError, "internal_error")
		return
	}
	writer.Header().Set("X-Resource-Version", strconv.FormatInt(value.ResourceVersion, 10))
	writeJSONResponse(writer, http.StatusOK, requestID, body)
}

func (server *FoundationHTTPServer) createWorkspaceSnapshot(writer http.ResponseWriter, request *http.Request, tenantID, projectID, requestID string, principal *authn.VerifiedPrincipal) {
	key, body, ok := foundationMutationBody(writer, request)
	if !ok {
		return
	}
	validated, err := openapi.ValidateCreateAdminWorkspaceSnapshotServerRequest(tenantID, projectID, requestID, key, body)
	if err != nil {
		writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	value, err := server.store.CreateWorkspaceSnapshot(request.Context(), tenantID, principal, postgres.WorkspaceSnapshotCreateInput{
		Scope: internalcoordination.FoundationScope{TenantID: tenantID, ProjectID: projectID}, SnapshotID: validated.Body.SnapshotID,
		SourceSandboxID: validated.Body.SourceSandboxID, ExpectedSandboxGeneration: validated.Body.ExpectedSandboxGeneration,
		RetentionSeconds: validated.Body.RetentionSeconds,
		Mutation:         internalcoordination.FoundationMutation{RequestID: requestID, IdempotencyKey: key},
	})
	if err != nil {
		writeFoundationError(writer, err)
		return
	}
	body, err = platform.EncodeWorkspaceSnapshotResponseJSON(common.ResponseEnvelope[platform.WorkspaceSnapshot]{Value: workspaceSnapshotResource(value)})
	if err != nil {
		writePublicProblem(writer, http.StatusInternalServerError, "internal_error")
		return
	}
	writer.Header().Set("X-Resource-Version", strconv.FormatInt(value.ResourceVersion, 10))
	writeJSONResponse(writer, http.StatusAccepted, requestID, body)
}

func (server *FoundationHTTPServer) cleanupWorkspaceSnapshot(writer http.ResponseWriter, request *http.Request, tenantID, projectID, snapshotID, requestID string, principal *authn.VerifiedPrincipal) {
	key, body, ok := foundationMutationBody(writer, request)
	if !ok {
		return
	}
	validated, err := openapi.ValidateCleanupAdminWorkspaceSnapshotServerRequest(tenantID, projectID, snapshotID, requestID, key, body)
	if err != nil {
		writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	expectedVersion, err := strconv.ParseInt(validated.Body.ExpectedSnapshotResourceVersion, 10, 64)
	if err != nil {
		writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	value, err := server.store.CleanupWorkspaceSnapshot(request.Context(), tenantID, principal, postgres.WorkspaceSnapshotCleanupInput{
		Scope: internalcoordination.FoundationScope{TenantID: tenantID, ProjectID: projectID}, SnapshotID: snapshotID,
		ExpectedSnapshotResourceVersion: expectedVersion, ConfirmedSnapshotID: validated.Body.ConfirmedSnapshotID,
		ConfirmedSourceWorkspaceID: validated.Body.ConfirmedSourceWorkspaceID,
		Mutation:                   internalcoordination.FoundationMutation{RequestID: requestID, IdempotencyKey: key},
	})
	if err != nil {
		writeFoundationError(writer, err)
		return
	}
	body, err = platform.EncodeWorkspaceSnapshotResponseJSON(common.ResponseEnvelope[platform.WorkspaceSnapshot]{Value: workspaceSnapshotResource(value)})
	if err != nil {
		writePublicProblem(writer, http.StatusInternalServerError, "internal_error")
		return
	}
	writer.Header().Set("X-Resource-Version", strconv.FormatInt(value.ResourceVersion, 10))
	writeJSONResponse(writer, http.StatusAccepted, requestID, body)
}

func (server *FoundationHTTPServer) getWorkspaceSnapshot(writer http.ResponseWriter, request *http.Request, tenantID, projectID, snapshotID, requestID string, principal *authn.VerifiedPrincipal) {
	if _, err := openapi.ValidateGetAdminWorkspaceSnapshotServerRequest(tenantID, projectID, snapshotID, requestID); err != nil {
		writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	value, err := server.store.GetWorkspaceSnapshot(request.Context(), tenantID, principal, projectID, snapshotID)
	if err != nil {
		writeFoundationError(writer, err)
		return
	}
	body, err := platform.EncodeWorkspaceSnapshotResponseJSON(common.ResponseEnvelope[platform.WorkspaceSnapshot]{Value: workspaceSnapshotResource(value)})
	if err != nil {
		writePublicProblem(writer, http.StatusInternalServerError, "internal_error")
		return
	}
	writer.Header().Set("X-Resource-Version", strconv.FormatInt(value.ResourceVersion, 10))
	writeJSONResponse(writer, http.StatusOK, requestID, body)
}

func (server *FoundationHTTPServer) restoreWorkspaceSnapshot(writer http.ResponseWriter, request *http.Request, tenantID, projectID, snapshotID, requestID string, principal *authn.VerifiedPrincipal) {
	key, body, ok := foundationMutationBody(writer, request)
	if !ok {
		return
	}
	validated, err := openapi.ValidateRestoreAdminWorkspaceSnapshotServerRequest(tenantID, projectID, snapshotID, requestID, key, body)
	if err != nil {
		writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	expectedVersion, err := strconv.ParseInt(validated.Body.ExpectedSnapshotResourceVersion, 10, 64)
	if err != nil {
		writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	result, err := server.store.RestoreWorkspaceSnapshot(request.Context(), tenantID, principal, postgres.WorkspaceSnapshotRestoreInput{
		Scope: internalcoordination.FoundationScope{TenantID: tenantID, ProjectID: projectID}, SnapshotID: snapshotID,
		ExpectedSnapshotResourceVersion: expectedVersion, WorkspaceID: validated.Body.WorkspaceID,
		WorkspaceName: validated.Body.WorkspaceName, SandboxID: validated.Body.SandboxID,
		RuntimeProfileID: validated.Body.RuntimeProfileID, RuntimeProfileVersion: validated.Body.RuntimeProfileVersion,
		TTLSeconds: validated.Body.TTLSeconds,
		Mutation:   internalcoordination.FoundationMutation{RequestID: requestID, IdempotencyKey: key},
	})
	if err != nil {
		writeFoundationError(writer, err)
		return
	}
	body, err = platform.EncodeSandboxSessionResponseJSON(common.ResponseEnvelope[platform.SandboxSession]{Value: sandboxSessionResource(result)})
	if err != nil {
		writePublicProblem(writer, http.StatusInternalServerError, "internal_error")
		return
	}
	writeJSONResponse(writer, http.StatusAccepted, requestID, body)
}

func (server *FoundationHTTPServer) listWorkspaceSnapshots(writer http.ResponseWriter, request *http.Request, tenantID, projectID, requestID string, principal *authn.VerifiedPrincipal) {
	pageSize, pageToken, ok := managedAgentPagination(request)
	if !ok {
		writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	validated, err := openapi.ValidateListAdminWorkspaceSnapshotsServerRequest(tenantID, projectID, requestID, pageSize, pageToken)
	if err != nil {
		writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	after, ok := decodeFoundationPageToken("workspace-snapshot/v1", tenantID, projectID, validated.PageToken)
	if !ok {
		writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	page, err := server.store.ListWorkspaceSnapshots(request.Context(), tenantID, principal, projectID, after, validated.PageSize)
	if err != nil {
		writeFoundationError(writer, err)
		return
	}
	values := make([]platform.WorkspaceSnapshot, 0, len(page.Snapshots))
	for _, snapshot := range page.Snapshots {
		values = append(values, workspaceSnapshotResource(snapshot))
	}
	next, ok := encodeFoundationPageToken("workspace-snapshot/v1", tenantID, projectID, page.NextSnapshotID)
	if !ok {
		writePublicProblem(writer, http.StatusInternalServerError, "internal_error")
		return
	}
	body, err := platform.EncodeWorkspaceSnapshotPageResponseJSON(common.ResponseEnvelope[platform.WorkspaceSnapshotPage]{Value: platform.WorkspaceSnapshotPage{
		APIVersion: platform.APIVersion, Kind: "WorkspaceSnapshotPage", WorkspaceSnapshots: values, NextPageToken: next,
	}})
	if err != nil {
		writePublicProblem(writer, http.StatusInternalServerError, "internal_error")
		return
	}
	writeJSONResponse(writer, http.StatusOK, requestID, body)
}

func (server *FoundationHTTPServer) listSandboxAccessGrants(writer http.ResponseWriter, request *http.Request, tenantID, projectID, sandboxID, requestID string, principal *authn.VerifiedPrincipal) {
	pageSize, pageToken, ok := managedAgentPagination(request)
	if !ok {
		writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	validated, err := openapi.ValidateListAdminSandboxAccessGrantsServerRequest(tenantID, projectID, sandboxID, requestID, pageSize, pageToken)
	if err != nil {
		writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	after, ok := decodeFoundationPageToken("admin-sandbox-access-grant/v1", tenantID, projectID, validated.PageToken)
	if !ok {
		writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	page, err := server.store.ListSandboxAccessGrants(request.Context(), tenantID, principal, projectID, sandboxID, after, validated.PageSize)
	if err != nil {
		writeFoundationError(writer, err)
		return
	}
	values := make([]platform.AdminSandboxAccessGrant, 0, len(page.Grants))
	for _, grant := range page.Grants {
		values = append(values, adminSandboxAccessGrantResource(grant))
	}
	next, ok := encodeFoundationPageToken("admin-sandbox-access-grant/v1", tenantID, projectID, page.NextGrantID)
	if !ok {
		writePublicProblem(writer, http.StatusInternalServerError, "internal_error")
		return
	}
	body, err := platform.EncodeAdminSandboxAccessGrantPageResponseJSON(common.ResponseEnvelope[platform.AdminSandboxAccessGrantPage]{Value: platform.AdminSandboxAccessGrantPage{
		APIVersion: platform.APIVersion, Kind: "AdminSandboxAccessGrantPage", AccessGrants: values, NextPageToken: next,
	}})
	if err != nil {
		writePublicProblem(writer, http.StatusInternalServerError, "internal_error")
		return
	}
	writeJSONResponse(writer, http.StatusOK, requestID, body)
}

func (server *FoundationHTTPServer) revokeSandboxAccessGrant(writer http.ResponseWriter, request *http.Request, tenantID, projectID, sandboxID, grantID, requestID string, principal *authn.VerifiedPrincipal) {
	key, body, ok := foundationMutationBody(writer, request)
	if !ok {
		return
	}
	validated, err := openapi.ValidateRevokeAdminSandboxAccessGrantServerRequest(tenantID, projectID, sandboxID, grantID, requestID, key, body)
	if err != nil {
		writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	expectedResourceVersion, err := strconv.ParseInt(validated.Body.ExpectedResourceVersion, 10, 64)
	if err != nil {
		writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	digest, err := foundationRequestDigest(validated.Body)
	if err != nil {
		writePublicProblem(writer, http.StatusInternalServerError, "internal_error")
		return
	}
	result, err := server.store.RevokeSandboxAccessGrant(request.Context(), tenantID, principal, postgres.SandboxAccessGrantRevokeInput{
		Scope:     internalcoordination.FoundationScope{TenantID: tenantID, ProjectID: projectID},
		SandboxID: sandboxID, GrantID: grantID, ConfirmedGrantID: validated.Body.ConfirmedGrantID,
		ExpectedGeneration: validated.Body.ExpectedGeneration, ExpectedResourceVersion: expectedResourceVersion,
		RequestDigest: digest, Mutation: internalcoordination.FoundationMutation{RequestID: requestID, IdempotencyKey: key},
	})
	if err != nil {
		writeFoundationError(writer, err)
		return
	}
	body, err = platform.EncodeAdminSandboxAccessGrantResponseJSON(common.ResponseEnvelope[platform.AdminSandboxAccessGrant]{Value: adminSandboxAccessGrantResource(result)})
	if err != nil {
		writePublicProblem(writer, http.StatusInternalServerError, "internal_error")
		return
	}
	writer.Header().Set("X-Resource-Version", strconv.FormatInt(result.ResourceVersion, 10))
	writeJSONResponse(writer, http.StatusOK, requestID, body)
}

func (server *FoundationHTTPServer) transitionAdminSandbox(writer http.ResponseWriter, request *http.Request, tenantID, projectID, sandboxID, action, requestID string, principal *authn.VerifiedPrincipal) {
	key, body, ok := foundationMutationBody(writer, request)
	if !ok {
		return
	}
	var transition platform.SandboxSessionLifecycleRequest
	var err error
	if action == internalcoordination.FoundationSandboxStop {
		var validated openapi.TransitionAdminSandboxSessionServerInput
		validated, err = openapi.ValidateStopAdminSandboxSessionServerRequest(tenantID, projectID, sandboxID, requestID, key, body)
		transition = validated.Body
	} else {
		var validated openapi.TransitionAdminSandboxSessionServerInput
		validated, err = openapi.ValidateRebuildAdminSandboxSessionServerRequest(tenantID, projectID, sandboxID, requestID, key, body)
		transition = validated.Body
	}
	if err != nil {
		writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	expectedResourceVersion, err := strconv.ParseInt(transition.ExpectedResourceVersion, 10, 64)
	if err != nil || expectedResourceVersion < 1 {
		writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	result, err := server.store.TransitionFoundationSandbox(request.Context(), tenantID, principal,
		internalcoordination.FoundationSandboxLifecycleInput{
			Scope:     internalcoordination.FoundationScope{TenantID: tenantID, ProjectID: projectID},
			SandboxID: sandboxID, Action: action, ConfirmedSandboxID: transition.ConfirmedSandboxID,
			ExpectedGeneration: transition.ExpectedGeneration, ExpectedResourceVersion: expectedResourceVersion,
			ComputeDisposition: transition.ComputeDisposition, WorkspaceDisposition: transition.WorkspaceDisposition,
			Mutation: internalcoordination.FoundationMutation{RequestID: requestID, IdempotencyKey: key},
		})
	if err != nil {
		writeFoundationError(writer, err)
		return
	}
	value := sandboxLifecycleOperationResource(result)
	body, err = platform.EncodeSandboxSessionLifecycleOperationResponseJSON(common.ResponseEnvelope[platform.SandboxSessionLifecycleOperation]{Value: value})
	if err != nil {
		writePublicProblem(writer, http.StatusInternalServerError, "internal_error")
		return
	}
	writeJSONResponse(writer, http.StatusAccepted, requestID, body)
}

func foundationMutationBody(writer http.ResponseWriter, request *http.Request) (string, []byte, bool) {
	key, ok := exactSingleHeader(request.Header, "Idempotency-Key")
	if !ok {
		writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
		return "", nil, false
	}
	body, err := io.ReadAll(http.MaxBytesReader(writer, request.Body, 1<<20))
	if err != nil {
		writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
		return "", nil, false
	}
	return key, body, true
}

func (server *FoundationHTTPServer) verify(ctx context.Context, bearer, tenantID, projectID, permission string) (*authn.VerifiedPrincipal, error) {
	return verifyHTTPRequestAccessToken(ctx, server.verifier, bearer, authn.VerificationRequest{TenantID: tenantID, ResourceLevel: "project", ResourceID: projectID, RequiredPermission: permission})
}

func writeFoundationError(writer http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, opensandbox.ErrOutputLimit):
		writePublicProblem(writer, http.StatusRequestEntityTooLarge, "sandbox_exec_output_limit")
	case errors.Is(err, context.DeadlineExceeded):
		writePublicProblem(writer, http.StatusGatewayTimeout, "sandbox_exec_timeout")
	case errors.Is(err, opensandbox.ErrConflict), errors.Is(err, opensandbox.ErrRuntimeFailed), errors.Is(err, opensandbox.ErrNotFound):
		writePublicProblem(writer, http.StatusConflict, "sandbox_runtime_unavailable")
	case errors.Is(err, opensandbox.ErrInvalid), errors.Is(err, opensandbox.ErrUnavailable):
		writePublicProblem(writer, http.StatusServiceUnavailable, "sandbox_access_unavailable")
	case errors.Is(err, internalcoordination.ErrRuntimeProfileNotFound), errors.Is(err, internalcoordination.ErrFoundationSandboxNotFound):
		writePublicProblem(writer, http.StatusNotFound, "not_found")
	case errors.Is(err, postgres.ErrWorkspaceSnapshotNotFound):
		writePublicProblem(writer, http.StatusNotFound, "not_found")
	case errors.Is(err, postgres.ErrMutationDenied):
		writePublicProblem(writer, http.StatusForbidden, "authorization_denied")
	case errors.Is(err, internalcoordination.ErrRuntimeProfileConflict), errors.Is(err, internalcoordination.ErrFoundationSandboxConflict), errors.Is(err, postgres.ErrCoordinationRejected):
		writePublicProblem(writer, http.StatusConflict, "resource_conflict")
	case errors.Is(err, postgres.ErrWorkspaceSnapshotConflict):
		writePublicProblem(writer, http.StatusConflict, "resource_conflict")
	case errors.Is(err, internalcoordination.ErrRuntimeProfileUnavailable):
		writePublicProblem(writer, http.StatusConflict, "runtime_profile_unavailable")
	case errors.Is(err, postgres.ErrCoordinationInvalidInput):
		writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
	default:
		writePublicProblem(writer, http.StatusInternalServerError, "internal_error")
	}
}
