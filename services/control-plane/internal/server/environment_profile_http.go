package server

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	commonv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/common/v1alpha1"
	openapiv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/openapi/v1alpha1"
	platformv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/platform/v1alpha1"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authn"
	internalenvironmentprofile "github.com/hxp0618/cloud-agents/services/control-plane/internal/environmentprofile"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/store/postgres"
)

const adminEnvironmentProfileRoutePrefix = "/v1/admin/tenants/"

type environmentProfileStore interface {
	CreateEnvironmentProfile(context.Context, string, *authn.VerifiedPrincipal, internalenvironmentprofile.CreateInput) (internalenvironmentprofile.Snapshot, error)
	TransitionEnvironmentProfile(context.Context, string, *authn.VerifiedPrincipal, internalenvironmentprofile.TransitionInput) (internalenvironmentprofile.Snapshot, error)
	GetEnvironmentProfile(context.Context, string, *authn.VerifiedPrincipal, string, string, int64) (internalenvironmentprofile.Snapshot, error)
	ListEnvironmentProfiles(context.Context, string, *authn.VerifiedPrincipal, string, string, int) (postgres.EnvironmentProfilePage, error)
	ListEnvironmentProfileAuditEvents(context.Context, string, *authn.VerifiedPrincipal, string, string, *time.Time, string, int) (postgres.EnvironmentProfileAuditPage, error)
}

type EnvironmentProfileHTTPServer struct {
	verifier AccessTokenVerifier
	store    environmentProfileStore
}

func NewAdminEnvironmentProfileHTTPServer(verifier AccessTokenVerifier, store environmentProfileStore) (*EnvironmentProfileHTTPServer, error) {
	if verifier == nil || store == nil {
		return nil, errors.New("environment profile HTTP server configuration is invalid")
	}
	return &EnvironmentProfileHTTPServer{verifier: verifier, store: store}, nil
}

func (server *EnvironmentProfileHTTPServer) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	preparePublicRequestID(writer, request)
	if server == nil || server.verifier == nil || server.store == nil || request == nil {
		writePublicProblem(writer, http.StatusInternalServerError, "internal_error")
		return
	}
	tenantID, projectID, profileID, version, action, ok := adminEnvironmentProfilePath(request.URL.Path)
	if !ok {
		writePublicProblem(writer, http.StatusNotFound, "route_not_found")
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
	permission, allowed := environmentProfileAdminPermission(action, request.Method)
	if !allowed {
		writePublicProblem(writer, http.StatusMethodNotAllowed, "method_not_allowed")
		return
	}
	projectPermission := "projects.get"
	if request.Method == http.MethodPost {
		projectPermission = "projects.act"
	}
	if _, err := server.verify(bearer, tenantID, projectID, projectPermission); err != nil {
		writePublicProblem(writer, http.StatusUnauthorized, "authentication_failed")
		return
	}
	if _, err := server.verify(bearer, tenantID, projectID, permission); err != nil {
		writePublicProblem(writer, http.StatusForbidden, "authorization_denied")
		return
	}
	switch action {
	case "collection":
		if request.Method == http.MethodPost {
			server.create(writer, request, tenantID, projectID, requestID, bearer)
		} else {
			server.list(writer, request, tenantID, projectID, requestID, bearer)
		}
	case "get":
		server.get(writer, request, tenantID, projectID, profileID, version, requestID, bearer)
	case "publish", "disable":
		server.transition(writer, request, tenantID, projectID, profileID, version, action, requestID, bearer)
	case "audit-events":
		server.listAuditEvents(writer, request, tenantID, projectID, profileID, version, requestID, bearer)
	}
}

func (server *EnvironmentProfileHTTPServer) transition(writer http.ResponseWriter, request *http.Request, tenantID, projectID, profileID string, version int64, action, requestID, bearer string) {
	idempotencyKey, ok := exactSingleHeader(request.Header, "Idempotency-Key")
	if !ok {
		writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(writer, request.Body, 1<<20))
	if err != nil {
		writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	var transition platformv1alpha1.EnvironmentProfileTransitionRequest
	if action == internalenvironmentprofile.TransitionPublish {
		validated, validateErr := openapiv1alpha1.ValidatePublishAdminEnvironmentProfileServerRequest(tenantID, projectID, profileID, version, requestID, idempotencyKey, body)
		if validateErr != nil {
			writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
			return
		}
		transition = validated.Body
	} else {
		validated, validateErr := openapiv1alpha1.ValidateDisableAdminEnvironmentProfileServerRequest(tenantID, projectID, profileID, version, requestID, idempotencyKey, body)
		if validateErr != nil {
			writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
			return
		}
		transition = validated.Body
	}
	expectedResourceVersion, err := strconv.ParseInt(transition.ExpectedResourceVersion, 10, 64)
	if err != nil || expectedResourceVersion < 1 {
		writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	principal, err := server.verify(bearer, tenantID, projectID, "projects.act")
	if err != nil {
		writePublicProblem(writer, http.StatusUnauthorized, "authentication_failed")
		return
	}
	result, err := server.store.TransitionEnvironmentProfile(request.Context(), tenantID, principal, internalenvironmentprofile.TransitionInput{
		Scope:     internalenvironmentprofile.Scope{TenantID: tenantID, ProjectID: projectID},
		ProfileID: profileID, Version: version, ExpectedResourceVersion: expectedResourceVersion, Action: action,
		Mutation: internalenvironmentprofile.Mutation{RequestID: requestID, IdempotencyKey: idempotencyKey},
	})
	if err != nil {
		writeEnvironmentProfileError(writer, err)
		return
	}
	writeEnvironmentProfile(writer, http.StatusOK, requestID, result)
}

func (server *EnvironmentProfileHTTPServer) create(writer http.ResponseWriter, request *http.Request, tenantID, projectID, requestID, bearer string) {
	idempotencyKey, ok := exactSingleHeader(request.Header, "Idempotency-Key")
	if !ok {
		writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(writer, request.Body, 1<<20))
	if err != nil {
		writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	validated, err := openapiv1alpha1.ValidateCreateAdminEnvironmentProfileServerRequest(tenantID, projectID, requestID, idempotencyKey, body)
	if err != nil {
		writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	principal, err := server.verify(bearer, tenantID, projectID, "projects.act")
	if err != nil {
		writePublicProblem(writer, http.StatusUnauthorized, "authentication_failed")
		return
	}
	requestBody := validated.Body
	result, err := server.store.CreateEnvironmentProfile(request.Context(), tenantID, principal, internalenvironmentprofile.CreateInput{
		Scope: internalenvironmentprofile.Scope{TenantID: tenantID, ProjectID: projectID}, ProfileID: requestBody.ProfileID,
		ProfileName: requestBody.ProfileName, Version: requestBody.Version, Description: requestBody.Description,
		ProviderKinds: requestBody.ProviderKinds, CPULimitMillis: requestBody.CPULimitMillis, MemoryLimitBytes: requestBody.MemoryLimitBytes,
		StoragePolicyRef: requestBody.StoragePolicyRef, NetworkPolicyRef: requestBody.NetworkPolicyRef,
		ReleaseDigest: requestBody.ReleaseDigest, TargetRefs: requestBody.TargetRefs, ProviderCredentialRef: requestBody.ProviderCredentialRef,
		Mutation: internalenvironmentprofile.Mutation{RequestID: requestID, IdempotencyKey: idempotencyKey},
	})
	if err != nil {
		writeEnvironmentProfileError(writer, err)
		return
	}
	writeEnvironmentProfile(writer, http.StatusCreated, requestID, result)
}

func (server *EnvironmentProfileHTTPServer) list(writer http.ResponseWriter, request *http.Request, tenantID, projectID, requestID, bearer string) {
	pageSize, pageToken, ok := managedAgentPagination(request)
	if !ok {
		writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	validated, err := openapiv1alpha1.ValidateListAdminEnvironmentProfilesServerRequest(tenantID, projectID, requestID, pageSize, pageToken)
	if err != nil {
		writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	afterProfileVersionID := ""
	if validated.PageToken != "" {
		if afterProfileVersionID, ok = decodeEnvironmentProfilePageToken(tenantID, projectID, validated.PageToken); !ok {
			writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
			return
		}
	}
	principal, err := server.verify(bearer, tenantID, projectID, "projects.get")
	if err != nil {
		writePublicProblem(writer, http.StatusUnauthorized, "authentication_failed")
		return
	}
	page, err := server.store.ListEnvironmentProfiles(request.Context(), tenantID, principal, projectID, afterProfileVersionID, validated.PageSize)
	if err != nil {
		writeEnvironmentProfileError(writer, err)
		return
	}
	profiles := make([]platformv1alpha1.EnvironmentProfile, 0, len(page.EnvironmentProfiles))
	for _, profile := range page.EnvironmentProfiles {
		profiles = append(profiles, environmentProfileResource(profile))
	}
	nextPageToken := ""
	if page.NextProfileVersionID != "" {
		if nextPageToken, ok = encodeEnvironmentProfilePageToken(tenantID, projectID, page.NextProfileVersionID); !ok {
			writePublicProblem(writer, http.StatusInternalServerError, "internal_error")
			return
		}
	}
	body, err := platformv1alpha1.EncodeEnvironmentProfilePageResponseJSON(commonv1alpha1.ResponseEnvelope[platformv1alpha1.EnvironmentProfilePage]{Value: platformv1alpha1.EnvironmentProfilePage{APIVersion: platformv1alpha1.APIVersion, Kind: "EnvironmentProfilePage", EnvironmentProfiles: profiles, NextPageToken: nextPageToken}})
	if err != nil {
		writePublicProblem(writer, http.StatusInternalServerError, "internal_error")
		return
	}
	writeJSONResponse(writer, http.StatusOK, requestID, body)
}

func (server *EnvironmentProfileHTTPServer) get(writer http.ResponseWriter, request *http.Request, tenantID, projectID, profileID string, version int64, requestID, bearer string) {
	if _, err := openapiv1alpha1.ValidateGetAdminEnvironmentProfileServerRequest(tenantID, projectID, profileID, version, requestID); err != nil {
		writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	principal, err := server.verify(bearer, tenantID, projectID, "projects.get")
	if err != nil {
		writePublicProblem(writer, http.StatusUnauthorized, "authentication_failed")
		return
	}
	result, err := server.store.GetEnvironmentProfile(request.Context(), tenantID, principal, projectID, profileID, version)
	if err != nil {
		writeEnvironmentProfileError(writer, err)
		return
	}
	writeEnvironmentProfile(writer, http.StatusOK, requestID, result)
}

func (server *EnvironmentProfileHTTPServer) listAuditEvents(writer http.ResponseWriter, request *http.Request, tenantID, projectID, profileID string, version int64, requestID, bearer string) {
	pageSize, pageToken, ok := managedAgentPagination(request)
	if !ok {
		writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	validated, err := openapiv1alpha1.ValidateListAdminEnvironmentProfileAuditEventsServerRequest(tenantID, projectID, profileID, version, requestID, pageSize, pageToken)
	if err != nil {
		writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	principal, err := server.verify(bearer, tenantID, projectID, "projects.get")
	if err != nil {
		writePublicProblem(writer, http.StatusUnauthorized, "authentication_failed")
		return
	}
	profile, err := server.store.GetEnvironmentProfile(request.Context(), tenantID, principal, projectID, profileID, version)
	if err != nil {
		writeEnvironmentProfileError(writer, err)
		return
	}
	principal, err = server.verify(bearer, tenantID, projectID, "projects.get")
	if err != nil {
		writePublicProblem(writer, http.StatusUnauthorized, "authentication_failed")
		return
	}
	var after *time.Time
	afterEventID := ""
	if validated.PageToken != "" {
		if after, afterEventID, ok = decodeEnvironmentProfileAuditPageToken(tenantID, projectID, profileID, version, validated.PageToken); !ok {
			writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
			return
		}
	}
	page, err := server.store.ListEnvironmentProfileAuditEvents(request.Context(), tenantID, principal, projectID, profile.ProfileVersionUID, after, afterEventID, validated.PageSize)
	if err != nil {
		writeEnvironmentProfileError(writer, err)
		return
	}
	events := make([]platformv1alpha1.AdminAuditEvent, 0, len(page.Events))
	for _, event := range page.Events {
		events = append(events, environmentProfileAuditEventResource(event))
	}
	nextPageToken := ""
	if page.NextOccurredAt != nil {
		if nextPageToken, ok = encodeEnvironmentProfileAuditPageToken(tenantID, projectID, profileID, version, *page.NextOccurredAt, page.NextEventID); !ok {
			writePublicProblem(writer, http.StatusInternalServerError, "internal_error")
			return
		}
	}
	body, err := platformv1alpha1.EncodeAdminAuditEventPageResponseJSON(commonv1alpha1.ResponseEnvelope[platformv1alpha1.AdminAuditEventPage]{Value: platformv1alpha1.AdminAuditEventPage{APIVersion: platformv1alpha1.APIVersion, Kind: "AdminAuditEventPage", Events: events, NextPageToken: nextPageToken}})
	if err != nil {
		writePublicProblem(writer, http.StatusInternalServerError, "internal_error")
		return
	}
	writeJSONResponse(writer, http.StatusOK, requestID, body)
}

func (server *EnvironmentProfileHTTPServer) verify(bearer, tenantID, projectID, permission string) (*authn.VerifiedPrincipal, error) {
	return server.verifier.Verify(bearer, authn.VerificationRequest{TenantID: tenantID, ResourceLevel: "project", ResourceID: projectID, RequiredPermission: permission})
}

func writeEnvironmentProfile(writer http.ResponseWriter, status int, requestID string, snapshot internalenvironmentprofile.Snapshot) {
	body, err := platformv1alpha1.EncodeEnvironmentProfileResponseJSON(commonv1alpha1.ResponseEnvelope[platformv1alpha1.EnvironmentProfile]{Value: environmentProfileResource(snapshot)})
	if err != nil {
		writePublicProblem(writer, http.StatusInternalServerError, "internal_error")
		return
	}
	writer.Header().Set("X-Resource-Version", strconv.FormatInt(snapshot.ResourceVersion, 10))
	writeJSONResponse(writer, status, requestID, body)
}

func writeJSONResponse(writer http.ResponseWriter, status int, requestID string, body []byte) {
	writer.Header().Set("X-Request-ID", requestID)
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_, _ = writer.Write(body)
}

func environmentProfileResource(snapshot internalenvironmentprofile.Snapshot) platformv1alpha1.EnvironmentProfile {
	publishedAt, disabledAt := "", ""
	if snapshot.PublishedAt != nil {
		publishedAt = snapshot.PublishedAt.UTC().Format(time.RFC3339Nano)
	}
	if snapshot.DisabledAt != nil {
		disabledAt = snapshot.DisabledAt.UTC().Format(time.RFC3339Nano)
	}
	return platformv1alpha1.EnvironmentProfile{
		ResourceBase: platformv1alpha1.ResourceBase{APIVersion: platformv1alpha1.APIVersion, Kind: "EnvironmentProfile", Metadata: commonv1alpha1.ResourceMetadata{
			UID: snapshot.ProfileVersionUID, Name: snapshot.ProfileName,
			TenantRef:       commonv1alpha1.TenantRef{Namespace: "cloud-agents", Kind: "tenant", ID: snapshot.Scope.TenantID},
			ResourceVersion: strconv.FormatInt(snapshot.ResourceVersion, 10), CreatedAt: snapshot.CreatedAt.UTC().Format(time.RFC3339Nano), UpdatedAt: snapshot.UpdatedAt.UTC().Format(time.RFC3339Nano),
		}},
		Spec: platformv1alpha1.EnvironmentProfileSpec{
			ProjectRef: commonv1alpha1.ProjectRef{Namespace: "cloud-agents", Kind: "project", ID: snapshot.Scope.ProjectID},
			ProfileID:  snapshot.ProfileID, Version: snapshot.Version, Description: snapshot.Description, Status: snapshot.Status,
			ProviderKinds: snapshot.ProviderKinds, CPULimitMillis: snapshot.CPULimitMillis, MemoryLimitBytes: snapshot.MemoryLimitBytes,
			StoragePolicyRef: snapshot.StoragePolicyRef, NetworkPolicyRef: snapshot.NetworkPolicyRef,
			ReleaseDigest: snapshot.ReleaseDigest, TargetRefs: snapshot.TargetRefs, ProviderCredentialRef: snapshot.ProviderCredentialRef,
			PublishedAt: publishedAt, DisabledAt: disabledAt,
		},
	}
}

func environmentProfileAuditEventResource(event internalenvironmentprofile.AuditEvent) platformv1alpha1.AdminAuditEvent {
	return platformv1alpha1.AdminAuditEvent{
		APIVersion: platformv1alpha1.APIVersion, Kind: "AdminAuditEvent", EventID: event.EventID, Actor: event.Actor,
		Action: event.Action, ResourceKind: "EnvironmentProfile", ResourceID: event.ProfileUID,
		ResourceGeneration: event.ProfileVersion, Result: event.Result, OccurredAt: event.OccurredAt.UTC().Format(time.RFC3339Nano),
		RequestID: event.RequestID, OperationID: event.OperationID,
	}
}

func adminEnvironmentProfilePath(path string) (tenantID, projectID, profileID string, version int64, action string, ok bool) {
	if !strings.HasPrefix(path, adminEnvironmentProfileRoutePrefix) {
		return "", "", "", 0, "", false
	}
	parts := strings.Split(strings.TrimPrefix(path, adminEnvironmentProfileRoutePrefix), "/")
	if len(parts) == 4 && parts[1] == "projects" && parts[3] == "environment-profiles" && parts[0] != "" && parts[2] != "" {
		return parts[0], parts[2], "", 0, "collection", true
	}
	if (len(parts) != 7 && len(parts) != 8) || parts[1] != "projects" || parts[3] != "environment-profiles" || parts[4] == "" || parts[5] != "versions" {
		return "", "", "", 0, "", false
	}
	versionPart, detailAction := parts[6], "get"
	if len(parts) == 8 {
		if parts[7] != "audit-events" {
			return "", "", "", 0, "", false
		}
		detailAction = "audit-events"
	} else if value, found := strings.CutSuffix(versionPart, ":publish"); found {
		versionPart, detailAction = value, internalenvironmentprofile.TransitionPublish
	} else if value, found := strings.CutSuffix(versionPart, ":disable"); found {
		versionPart, detailAction = value, internalenvironmentprofile.TransitionDisable
	}
	parsedVersion, err := strconv.ParseInt(versionPart, 10, 64)
	if err == nil && parsedVersion > 0 && parsedVersion <= 2147483647 {
		return parts[0], parts[2], parts[4], parsedVersion, detailAction, true
	}
	return "", "", "", 0, "", false
}

func environmentProfileAdminPermission(action, method string) (string, bool) {
	switch {
	case action == "collection" && method == http.MethodGet:
		return "profiles.list", true
	case action == "collection" && method == http.MethodPost:
		return "profiles.create", true
	case action == "get" && method == http.MethodGet:
		return "profiles.get", true
	case action == internalenvironmentprofile.TransitionPublish && method == http.MethodPost:
		return "profiles.act", true
	case action == internalenvironmentprofile.TransitionDisable && method == http.MethodPost:
		return "profiles.act", true
	case action == "audit-events" && method == http.MethodGet:
		return "audit.list", true
	default:
		return "", false
	}
}

func HandlesAdminEnvironmentProfilePath(path string) bool {
	_, _, _, _, _, ok := adminEnvironmentProfilePath(path)
	return ok
}

func encodeEnvironmentProfilePageToken(tenantID, projectID, profileVersionID string) (string, bool) {
	if commonv1alpha1.ValidateIdentifier(tenantID, "/tenantId") != nil || commonv1alpha1.ValidateIdentifier(projectID, "/projectId") != nil || commonv1alpha1.ValidateIdentifier(profileVersionID, "/profileVersionId") != nil {
		return "", false
	}
	token := base64.RawURLEncoding.EncodeToString([]byte("environment-profile/v1\x00" + tenantID + "\x00" + projectID + "\x00" + profileVersionID))
	return token, commonv1alpha1.ValidatePageToken(token, "/pageToken") == nil
}

func decodeEnvironmentProfilePageToken(tenantID, projectID, token string) (string, bool) {
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(token)
	if err != nil || commonv1alpha1.ValidatePageToken(token, "/pageToken") != nil {
		return "", false
	}
	parts := strings.Split(string(decoded), "\x00")
	if len(parts) != 4 || parts[0] != "environment-profile/v1" || parts[1] != tenantID || parts[2] != projectID || commonv1alpha1.ValidateIdentifier(parts[3], "/profileVersionId") != nil {
		return "", false
	}
	return parts[3], true
}

func encodeEnvironmentProfileAuditPageToken(tenantID, projectID, profileID string, version int64, occurredAt time.Time, eventID string) (string, bool) {
	if occurredAt.IsZero() || version < 1 || commonv1alpha1.ValidateIdentifier(tenantID, "/tenantId") != nil || commonv1alpha1.ValidateIdentifier(projectID, "/projectId") != nil || commonv1alpha1.ValidateIdentifier(profileID, "/profileId") != nil || commonv1alpha1.ValidateIdentifier(eventID, "/eventId") != nil {
		return "", false
	}
	token := base64.RawURLEncoding.EncodeToString([]byte("environment-profile-audit/v1\x00" + tenantID + "\x00" + projectID + "\x00" + profileID + "\x00" + strconv.FormatInt(version, 10) + "\x00" + occurredAt.UTC().Format(time.RFC3339Nano) + "\x00" + eventID))
	return token, commonv1alpha1.ValidatePageToken(token, "/pageToken") == nil
}

func decodeEnvironmentProfileAuditPageToken(tenantID, projectID, profileID string, version int64, token string) (*time.Time, string, bool) {
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(token)
	if err != nil || commonv1alpha1.ValidatePageToken(token, "/pageToken") != nil {
		return nil, "", false
	}
	parts := strings.Split(string(decoded), "\x00")
	if len(parts) != 7 || parts[0] != "environment-profile-audit/v1" || parts[1] != tenantID || parts[2] != projectID || parts[3] != profileID || parts[4] != strconv.FormatInt(version, 10) || commonv1alpha1.ValidateIdentifier(parts[6], "/eventId") != nil {
		return nil, "", false
	}
	occurredAt, err := time.Parse(time.RFC3339Nano, parts[5])
	if err != nil {
		return nil, "", false
	}
	return &occurredAt, parts[6], true
}

func writeEnvironmentProfileError(writer http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, postgres.ErrEnvironmentProfileNotFound):
		writePublicProblem(writer, http.StatusNotFound, "not_found")
	case errors.Is(err, postgres.ErrMutationDenied):
		writePublicProblem(writer, http.StatusForbidden, "authorization_denied")
	case errors.Is(err, postgres.ErrEnvironmentProfileIdempotencyConflict):
		writePublicProblem(writer, http.StatusConflict, "idempotency_conflict")
	case errors.Is(err, postgres.ErrEnvironmentProfileVersionConflict), errors.Is(err, postgres.ErrCoordinationRejected):
		writePublicProblem(writer, http.StatusConflict, "profile_version_conflict")
	case errors.Is(err, postgres.ErrEnvironmentProfileTransitionConflict):
		writePublicProblem(writer, http.StatusConflict, "profile_transition_conflict")
	case errors.Is(err, postgres.ErrEnvironmentProfileStoragePolicyUnavailable):
		writePublicProblem(writer, http.StatusConflict, "storage_policy_unavailable")
	case errors.Is(err, postgres.ErrEnvironmentProfileNetworkPolicyUnavailable):
		writePublicProblem(writer, http.StatusConflict, "network_policy_unavailable")
	case errors.Is(err, postgres.ErrCoordinationInvalidInput):
		writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
	default:
		writePublicProblem(writer, http.StatusInternalServerError, "internal_error")
	}
}
