package server

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	common "github.com/hxp0618/cloud-agents/sdk/go/gen/common/v1alpha1"
	openapi "github.com/hxp0618/cloud-agents/sdk/go/gen/openapi/v1alpha1"
	platform "github.com/hxp0618/cloud-agents/sdk/go/gen/platform/v1alpha1"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authn"
	internalcoordination "github.com/hxp0618/cloud-agents/services/control-plane/internal/coordination"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/store/postgres"
)

type foundationStore interface {
	CreateRuntimeProfile(context.Context, string, *authn.VerifiedPrincipal, internalcoordination.RuntimeProfileCreateInput) (internalcoordination.RuntimeProfileSnapshot, error)
	TransitionRuntimeProfile(context.Context, string, *authn.VerifiedPrincipal, internalcoordination.RuntimeProfileTransitionInput) (internalcoordination.RuntimeProfileSnapshot, error)
	GetRuntimeProfile(context.Context, string, *authn.VerifiedPrincipal, string, string, int64) (internalcoordination.RuntimeProfileSnapshot, error)
	ListRuntimeProfiles(context.Context, string, *authn.VerifiedPrincipal, string, string, int) (postgres.RuntimeProfilePage, error)
	ListPublishedRuntimeProfiles(context.Context, string, *authn.VerifiedPrincipal, string, string, int) (postgres.PublishedRuntimeProfilePage, error)
	CreateFoundationSandbox(context.Context, string, *authn.VerifiedPrincipal, internalcoordination.FoundationSandboxCreateInput) (internalcoordination.FoundationSandboxSnapshot, error)
	GetAdminSandbox(context.Context, string, *authn.VerifiedPrincipal, string, string) (postgres.AdminSandboxSnapshot, error)
	ListAdminSandboxes(context.Context, string, *authn.VerifiedPrincipal, string, string, int) (postgres.AdminSandboxPage, error)
}

type FoundationHTTPServer struct {
	verifier AccessTokenVerifier
	store    foundationStore
}

func NewFoundationHTTPServer(verifier AccessTokenVerifier, store foundationStore) (*FoundationHTTPServer, error) {
	if verifier == nil || store == nil {
		return nil, errors.New("foundation HTTP server configuration is invalid")
	}
	return &FoundationHTTPServer{verifier: verifier, store: store}, nil
}

func (server *FoundationHTTPServer) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	preparePublicRequestID(writer, request)
	if server == nil || server.verifier == nil || server.store == nil || request == nil {
		writePublicProblem(writer, http.StatusInternalServerError, "internal_error")
		return
	}
	admin, tenantID, projectID, profileID, version, action, ok := foundationPath(request.URL.Path)
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
	principal, err := server.verify(bearer, tenantID, projectID, projectPermission)
	if err != nil {
		writePublicProblem(writer, http.StatusUnauthorized, "authentication_failed")
		return
	}
	if _, err := server.verify(bearer, tenantID, projectID, permission); err != nil {
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
	case "admin-sandbox-collection":
		server.listAdminSandboxes(writer, request, tenantID, projectID, requestID, principal)
	case "get-admin-sandbox":
		server.getAdminSandbox(writer, request, tenantID, projectID, profileID, requestID, principal)
	}
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
	result, err := server.store.CreateRuntimeProfile(request.Context(), tenantID, principal, internalcoordination.RuntimeProfileCreateInput{
		Scope:     internalcoordination.FoundationScope{TenantID: tenantID, ProjectID: projectID},
		ProfileID: input.ProfileID, ProfileName: input.ProfileName, Version: input.Version,
		Description: input.Description, TargetID: input.TargetID, ImageURI: input.ImageURI,
		ReleaseDigest: input.ReleaseDigest, CPUMillis: input.CPUMillis, MemoryBytes: input.MemoryBytes,
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
		Mutation: internalcoordination.FoundationMutation{RequestID: requestID, IdempotencyKey: key},
	})
	if err != nil {
		writeFoundationError(writer, err)
		return
	}
	value := platform.SandboxSession{APIVersion: platform.APIVersion, Kind: "SandboxSession",
		ProjectRef:  common.ProjectRef{Namespace: "cloud-agents", Kind: "project", ID: projectID},
		OperationID: result.OperationID, WorkspaceID: result.WorkspaceID, SandboxID: result.SandboxID,
		RuntimeProfileID: result.RuntimeProfileID, RuntimeProfileVersion: result.RuntimeProfileVersion,
		Generation: result.Generation, DesiredState: result.DesiredState, ObservedState: result.ObservedState}
	body, err = platform.EncodeSandboxSessionResponseJSON(common.ResponseEnvelope[platform.SandboxSession]{Value: value})
	if err != nil {
		writePublicProblem(writer, http.StatusInternalServerError, "internal_error")
		return
	}
	writeJSONResponse(writer, http.StatusAccepted, requestID, body)
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

func (server *FoundationHTTPServer) verify(bearer, tenantID, projectID, permission string) (*authn.VerifiedPrincipal, error) {
	return server.verifier.Verify(bearer, authn.VerificationRequest{TenantID: tenantID, ResourceLevel: "project", ResourceID: projectID, RequiredPermission: permission})
}

func runtimeProfileResource(snapshot internalcoordination.RuntimeProfileSnapshot) platform.RuntimeProfile {
	publishedAt, disabledAt := "", ""
	if snapshot.PublishedAt != nil {
		publishedAt = snapshot.PublishedAt.UTC().Format(time.RFC3339Nano)
	}
	if snapshot.DisabledAt != nil {
		disabledAt = snapshot.DisabledAt.UTC().Format(time.RFC3339Nano)
	}
	return platform.RuntimeProfile{ResourceBase: platform.ResourceBase{APIVersion: platform.APIVersion, Kind: "RuntimeProfile", Metadata: common.ResourceMetadata{
		UID: snapshot.ProfileVersionID, Name: snapshot.ProfileName,
		TenantRef:       common.TenantRef{Namespace: "cloud-agents", Kind: "tenant", ID: snapshot.Scope.TenantID},
		ResourceVersion: strconv.FormatInt(snapshot.ResourceVersion, 10), CreatedAt: snapshot.CreatedAt.UTC().Format(time.RFC3339Nano), UpdatedAt: snapshot.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}}, Spec: platform.RuntimeProfileSpec{
		ProjectRef: common.ProjectRef{Namespace: "cloud-agents", Kind: "project", ID: snapshot.Scope.ProjectID},
		ProfileID:  snapshot.ProfileID, Version: snapshot.Version, Description: snapshot.Description,
		Status: snapshot.Status, TargetID: snapshot.TargetID, ImageURI: snapshot.ImageURI,
		ReleaseDigest: snapshot.ReleaseDigest, CPUMillis: snapshot.CPUMillis, MemoryBytes: snapshot.MemoryBytes,
		PublishedAt: publishedAt, DisabledAt: disabledAt,
	}}
}

func runtimeProfileSummaryResource(summary internalcoordination.RuntimeProfileSummary) platform.RuntimeProfileSummary {
	return platform.RuntimeProfileSummary{APIVersion: platform.APIVersion, Kind: "RuntimeProfileSummary",
		ProjectRef: common.ProjectRef{Namespace: "cloud-agents", Kind: "project", ID: summary.Scope.ProjectID},
		ProfileID:  summary.ProfileID, Name: summary.ProfileName, Version: summary.Version,
		Description: summary.Description, Status: "published", Availability: "available",
		CPUMillis: summary.CPUMillis, MemoryBytes: summary.MemoryBytes, WorkspaceRetention: "retained"}
}

func adminSandboxResource(snapshot postgres.AdminSandboxSnapshot) platform.AdminSandboxSession {
	physicalVolumeID, runtimeID, stableErrorCode, observedAt := "", "", "", ""
	if snapshot.PhysicalVolumeID != nil {
		physicalVolumeID = *snapshot.PhysicalVolumeID
	}
	if snapshot.RuntimeID != nil {
		runtimeID = *snapshot.RuntimeID
	}
	if snapshot.StableErrorCode != nil {
		stableErrorCode = *snapshot.StableErrorCode
	}
	if snapshot.ObservedAt != nil {
		observedAt = snapshot.ObservedAt.UTC().Format(time.RFC3339Nano)
	}
	return platform.AdminSandboxSession{ResourceBase: platform.ResourceBase{APIVersion: platform.APIVersion, Kind: "AdminSandboxSession", Metadata: common.ResourceMetadata{
		UID: snapshot.SandboxID, Name: snapshot.SandboxID,
		TenantRef:       common.TenantRef{Namespace: "cloud-agents", Kind: "tenant", ID: snapshot.Scope.TenantID},
		ResourceVersion: strconv.FormatInt(snapshot.ResourceVersion, 10), CreatedAt: snapshot.CreatedAt.UTC().Format(time.RFC3339Nano), UpdatedAt: snapshot.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}}, Spec: platform.AdminSandboxSessionSpec{
		ProjectRef:  common.ProjectRef{Namespace: "cloud-agents", Kind: "project", ID: snapshot.Scope.ProjectID},
		OperationID: snapshot.OperationID, OperationState: snapshot.OperationState, CleanupPhase: snapshot.CleanupPhase,
		WorkspaceID: snapshot.WorkspaceID, WorkspaceName: snapshot.WorkspaceName, VolumeID: snapshot.VolumeID,
		PhysicalVolumeID: physicalVolumeID, WorkspaceRetention: "retained", WorkspaceObservedState: snapshot.WorkspaceObservedState,
		RuntimeProfileID: snapshot.RuntimeProfileID, RuntimeProfileVersion: snapshot.RuntimeProfileVersion,
		TargetID: snapshot.TargetID, Generation: snapshot.Generation, ObservedGeneration: snapshot.ObservedGeneration,
		DesiredState: snapshot.DesiredState, ObservedState: snapshot.ObservedState, WriterReleased: snapshot.WriterReleased,
		RuntimeID: runtimeID, RuntimeState: snapshot.RuntimeState, StableErrorCode: stableErrorCode, ObservedAt: observedAt,
	}}
}

func foundationPath(path string) (admin bool, tenantID, projectID, profileID string, version int64, action string, ok bool) {
	prefix := "/v1/tenants/"
	if strings.HasPrefix(path, "/v1/admin/tenants/") {
		admin, prefix = true, "/v1/admin/tenants/"
	} else if !strings.HasPrefix(path, prefix) {
		return false, "", "", "", 0, "", false
	}
	parts := strings.Split(strings.TrimPrefix(path, prefix), "/")
	if len(parts) == 4 && parts[0] != "" && parts[1] == "projects" && parts[2] != "" {
		switch {
		case parts[3] == "runtime-profiles" && admin:
			return true, parts[0], parts[2], "", 0, "admin-collection", true
		case parts[3] == "runtime-profiles":
			return false, parts[0], parts[2], "", 0, "public-list", true
		case parts[3] == "sandbox-sessions" && !admin:
			return false, parts[0], parts[2], "", 0, "create-sandbox", true
		case parts[3] == "sandbox-sessions" && admin:
			return true, parts[0], parts[2], "", 0, "admin-sandbox-collection", true
		}
	}
	if admin && len(parts) == 5 && parts[1] == "projects" && parts[3] == "sandbox-sessions" && parts[4] != "" {
		return true, parts[0], parts[2], parts[4], 0, "get-admin-sandbox", true
	}
	if !admin || len(parts) != 7 || parts[1] != "projects" || parts[3] != "runtime-profiles" || parts[4] == "" || parts[5] != "versions" {
		return false, "", "", "", 0, "", false
	}
	versionPart, detailAction := parts[6], "get-profile"
	if value, found := strings.CutSuffix(versionPart, ":publish"); found {
		versionPart, detailAction = value, internalcoordination.RuntimeProfilePublish
	} else if value, found := strings.CutSuffix(versionPart, ":disable"); found {
		versionPart, detailAction = value, internalcoordination.RuntimeProfileDisable
	}
	parsed, err := strconv.ParseInt(versionPart, 10, 64)
	if err != nil || parsed < 1 || parsed > 2147483647 {
		return false, "", "", "", 0, "", false
	}
	return true, parts[0], parts[2], parts[4], parsed, detailAction, true
}

func foundationPermission(admin bool, action, method string) (projectPermission, permission string, ok bool) {
	switch {
	case admin && action == "admin-collection" && method == http.MethodGet:
		return "projects.get", "profiles.list", true
	case admin && action == "admin-collection" && method == http.MethodPost:
		return "projects.act", "profiles.create", true
	case admin && action == "get-profile" && method == http.MethodGet:
		return "projects.get", "profiles.get", true
	case admin && (action == internalcoordination.RuntimeProfilePublish || action == internalcoordination.RuntimeProfileDisable) && method == http.MethodPost:
		return "projects.act", "profiles.act", true
	case !admin && action == "public-list" && method == http.MethodGet:
		return "projects.get", "environment-profiles.list", true
	case !admin && action == "create-sandbox" && method == http.MethodPost:
		return "projects.act", "environments.create", true
	case admin && action == "admin-sandbox-collection" && method == http.MethodGet:
		return "projects.get", "sandboxes.list", true
	case admin && action == "get-admin-sandbox" && method == http.MethodGet:
		return "projects.get", "sandboxes.get", true
	default:
		return "", "", false
	}
}

func encodeFoundationPageToken(kind, tenantID, projectID, value string) (string, bool) {
	if value == "" {
		return "", true
	}
	return encodeProjectResourcePageToken(kind, tenantID, projectID, value)
}

func decodeFoundationPageToken(kind, tenantID, projectID, token string) (string, bool) {
	if token == "" {
		return "", true
	}
	return decodeProjectResourcePageToken(kind, tenantID, projectID, token)
}

func HandlesFoundationPath(path string) bool {
	_, _, _, _, _, _, ok := foundationPath(path)
	return ok
}

func writeRuntimeProfile(writer http.ResponseWriter, status int, requestID string, snapshot internalcoordination.RuntimeProfileSnapshot) {
	body, err := platform.EncodeRuntimeProfileResponseJSON(common.ResponseEnvelope[platform.RuntimeProfile]{Value: runtimeProfileResource(snapshot)})
	if err != nil {
		writePublicProblem(writer, http.StatusInternalServerError, "internal_error")
		return
	}
	writer.Header().Set("X-Resource-Version", strconv.FormatInt(snapshot.ResourceVersion, 10))
	writeJSONResponse(writer, status, requestID, body)
}

func writeFoundationError(writer http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, internalcoordination.ErrRuntimeProfileNotFound), errors.Is(err, internalcoordination.ErrFoundationSandboxNotFound):
		writePublicProblem(writer, http.StatusNotFound, "not_found")
	case errors.Is(err, postgres.ErrMutationDenied):
		writePublicProblem(writer, http.StatusForbidden, "authorization_denied")
	case errors.Is(err, internalcoordination.ErrRuntimeProfileConflict), errors.Is(err, internalcoordination.ErrFoundationSandboxConflict), errors.Is(err, postgres.ErrCoordinationRejected):
		writePublicProblem(writer, http.StatusConflict, "resource_conflict")
	case errors.Is(err, internalcoordination.ErrRuntimeProfileUnavailable):
		writePublicProblem(writer, http.StatusConflict, "runtime_profile_unavailable")
	case errors.Is(err, postgres.ErrCoordinationInvalidInput):
		writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
	default:
		writePublicProblem(writer, http.StatusInternalServerError, "internal_error")
	}
}
