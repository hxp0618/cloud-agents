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
	internalstoragepolicy "github.com/hxp0618/cloud-agents/services/control-plane/internal/storagepolicy"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/store/postgres"
)

type storagePolicyStore interface {
	SetStoragePolicy(context.Context, string, *authn.VerifiedPrincipal, internalstoragepolicy.SetInput) (internalstoragepolicy.Snapshot, error)
	GetStoragePolicy(context.Context, string, *authn.VerifiedPrincipal, string, string) (internalstoragepolicy.Snapshot, error)
	ListStoragePolicies(context.Context, string, *authn.VerifiedPrincipal, string, string, int) (postgres.StoragePolicyPage, error)
	ListStoragePolicyAuditEvents(context.Context, string, *authn.VerifiedPrincipal, string, string, *time.Time, string, int) (postgres.StoragePolicyAuditPage, error)
}

type StoragePolicyHTTPServer struct {
	verifier AccessTokenVerifier
	store    storagePolicyStore
}

func NewStoragePolicyHTTPServer(verifier AccessTokenVerifier, store storagePolicyStore) (*StoragePolicyHTTPServer, error) {
	if verifier == nil || store == nil {
		return nil, errors.New("storage policy HTTP server configuration is invalid")
	}
	return &StoragePolicyHTTPServer{verifier: verifier, store: store}, nil
}

func (server *StoragePolicyHTTPServer) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	preparePublicRequestID(writer, request)
	if server == nil || server.verifier == nil || server.store == nil || request == nil {
		writePublicProblem(writer, http.StatusInternalServerError, "internal_error")
		return
	}
	tenantID, projectID, policyID, action, ok := storagePolicyPath(request.URL.Path)
	if !ok {
		writePublicProblem(writer, http.StatusNotFound, "route_not_found")
		return
	}
	projectPermission, policyPermission, allowed := storagePolicyPermission(action, request.Method)
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
	if _, err := server.verify(bearer, tenantID, projectID, policyPermission); err != nil {
		writePublicProblem(writer, http.StatusForbidden, "authorization_denied")
		return
	}
	switch action {
	case "list":
		server.list(writer, request, tenantID, projectID, requestID, principal)
	case "get-set":
		if request.Method == http.MethodPut {
			server.set(writer, request, tenantID, projectID, policyID, requestID, principal)
		} else {
			server.get(writer, request, tenantID, projectID, policyID, requestID, principal)
		}
	case "audit-events":
		server.listAuditEvents(writer, request, tenantID, projectID, policyID, requestID, principal)
	}
}

func (server *StoragePolicyHTTPServer) list(writer http.ResponseWriter, request *http.Request, tenantID, projectID, requestID string, principal *authn.VerifiedPrincipal) {
	pageSize, pageToken, ok := managedAgentPagination(request)
	if !ok {
		writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	validated, err := openapiv1alpha1.ValidateListAdminStoragePoliciesServerRequest(tenantID, projectID, requestID, pageSize, pageToken)
	if err != nil {
		writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	afterPolicyID := ""
	if validated.PageToken != "" {
		if afterPolicyID, ok = decodeStoragePolicyPageToken(tenantID, projectID, validated.PageToken); !ok {
			writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
			return
		}
	}
	page, err := server.store.ListStoragePolicies(request.Context(), tenantID, principal, projectID, afterPolicyID, validated.PageSize)
	if err != nil {
		writeStoragePolicyError(writer, err)
		return
	}
	policies := make([]platformv1alpha1.StoragePolicy, 0, len(page.StoragePolicies))
	for _, policy := range page.StoragePolicies {
		policies = append(policies, storagePolicyResource(policy))
	}
	nextPageToken := ""
	if page.NextStoragePolicyID != "" {
		if nextPageToken, ok = encodeStoragePolicyPageToken(tenantID, projectID, page.NextStoragePolicyID); !ok {
			writePublicProblem(writer, http.StatusInternalServerError, "internal_error")
			return
		}
	}
	body, err := platformv1alpha1.EncodeStoragePolicyPageResponseJSON(commonv1alpha1.ResponseEnvelope[platformv1alpha1.StoragePolicyPage]{Value: platformv1alpha1.StoragePolicyPage{
		APIVersion: platformv1alpha1.APIVersion, Kind: "StoragePolicyPage", StoragePolicies: policies, NextPageToken: nextPageToken,
	}})
	if err != nil {
		writePublicProblem(writer, http.StatusInternalServerError, "internal_error")
		return
	}
	writeJSONResponse(writer, http.StatusOK, requestID, body)
}

func (server *StoragePolicyHTTPServer) get(writer http.ResponseWriter, request *http.Request, tenantID, projectID, policyID, requestID string, principal *authn.VerifiedPrincipal) {
	if _, err := openapiv1alpha1.ValidateGetAdminStoragePolicyServerRequest(tenantID, projectID, policyID, requestID); err != nil {
		writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	policy, err := server.store.GetStoragePolicy(request.Context(), tenantID, principal, projectID, policyID)
	if err != nil {
		writeStoragePolicyError(writer, err)
		return
	}
	writeStoragePolicy(writer, requestID, policy)
}

func (server *StoragePolicyHTTPServer) set(writer http.ResponseWriter, request *http.Request, tenantID, projectID, policyID, requestID string, principal *authn.VerifiedPrincipal) {
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
	validated, err := openapiv1alpha1.ValidateSetAdminStoragePolicyServerRequest(tenantID, projectID, policyID, requestID, idempotencyKey, body)
	if err != nil {
		writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	expectedResourceVersion, err := strconv.ParseInt(validated.Body.ExpectedResourceVersion, 10, 64)
	if err != nil || expectedResourceVersion < 0 {
		writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	policy, err := server.store.SetStoragePolicy(request.Context(), tenantID, principal, internalstoragepolicy.SetInput{
		Scope:    internalstoragepolicy.Scope{TenantID: tenantID, ProjectID: projectID},
		PolicyID: policyID, PolicyName: validated.Body.PolicyName, UserSummary: validated.Body.UserSummary,
		WorkspaceType: validated.Body.WorkspaceType, WorkspaceCapacityBytes: validated.Body.WorkspaceCapacityBytes,
		RetentionSeconds: validated.Body.RetentionSeconds, CleanupOnLeaseTermination: validated.Body.CleanupOnLeaseTermination,
		SnapshotBackendRef: validated.Body.SnapshotBackendRef, ArtifactBackendRef: validated.Body.ArtifactBackendRef,
		AllowWorkspaceReuse: validated.Body.AllowWorkspaceReuse, ExpectedResourceVersion: expectedResourceVersion,
		Mutation: internalstoragepolicy.Mutation{RequestID: requestID, IdempotencyKey: idempotencyKey},
	})
	if err != nil {
		writeStoragePolicyError(writer, err)
		return
	}
	writeStoragePolicy(writer, requestID, policy)
}

func (server *StoragePolicyHTTPServer) listAuditEvents(writer http.ResponseWriter, request *http.Request, tenantID, projectID, policyID, requestID string, principal *authn.VerifiedPrincipal) {
	pageSize, pageToken, ok := managedAgentPagination(request)
	if !ok {
		writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	validated, err := openapiv1alpha1.ValidateListAdminStoragePolicyAuditEventsServerRequest(tenantID, projectID, policyID, requestID, pageSize, pageToken)
	if err != nil {
		writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	var after *time.Time
	afterEventID := ""
	if validated.PageToken != "" {
		if after, afterEventID, ok = decodeStoragePolicyAuditPageToken(tenantID, projectID, policyID, validated.PageToken); !ok {
			writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
			return
		}
	}
	page, err := server.store.ListStoragePolicyAuditEvents(request.Context(), tenantID, principal, projectID, policyID, after, afterEventID, validated.PageSize)
	if err != nil {
		writeStoragePolicyError(writer, err)
		return
	}
	events := make([]platformv1alpha1.AdminAuditEvent, 0, len(page.Events))
	for _, event := range page.Events {
		events = append(events, platformv1alpha1.AdminAuditEvent{
			APIVersion: platformv1alpha1.APIVersion, Kind: "AdminAuditEvent",
			EventID: event.EventID, Actor: event.Actor, Action: event.Action,
			ResourceKind: "StoragePolicy", ResourceID: event.PolicyID,
			ResourceGeneration: event.PolicyResourceVersion, Result: event.Result,
			OccurredAt: event.OccurredAt.UTC().Format(time.RFC3339Nano), RequestID: event.RequestID,
			OperationID: event.OperationID,
		})
	}
	nextPageToken := ""
	if page.NextOccurredAt != nil {
		if nextPageToken, ok = encodeStoragePolicyAuditPageToken(tenantID, projectID, policyID, *page.NextOccurredAt, page.NextEventID); !ok {
			writePublicProblem(writer, http.StatusInternalServerError, "internal_error")
			return
		}
	}
	body, err := platformv1alpha1.EncodeAdminAuditEventPageResponseJSON(commonv1alpha1.ResponseEnvelope[platformv1alpha1.AdminAuditEventPage]{Value: platformv1alpha1.AdminAuditEventPage{
		APIVersion: platformv1alpha1.APIVersion, Kind: "AdminAuditEventPage", Events: events, NextPageToken: nextPageToken,
	}})
	if err != nil {
		writePublicProblem(writer, http.StatusInternalServerError, "internal_error")
		return
	}
	writeJSONResponse(writer, http.StatusOK, requestID, body)
}

func (server *StoragePolicyHTTPServer) verify(bearer, tenantID, projectID, permission string) (*authn.VerifiedPrincipal, error) {
	return server.verifier.Verify(bearer, authn.VerificationRequest{TenantID: tenantID, ResourceLevel: "project", ResourceID: projectID, RequiredPermission: permission})
}

func storagePolicyResource(policy internalstoragepolicy.Snapshot) platformv1alpha1.StoragePolicy {
	return platformv1alpha1.StoragePolicy{
		ResourceBase: platformv1alpha1.ResourceBase{
			APIVersion: platformv1alpha1.APIVersion, Kind: "StoragePolicy",
			Metadata: commonv1alpha1.ResourceMetadata{
				UID: policy.PolicyID, Name: policy.PolicyName,
				TenantRef:       commonv1alpha1.TenantRef{Namespace: "cloud-agents", Kind: "tenant", ID: policy.Scope.TenantID},
				ResourceVersion: strconv.FormatInt(policy.ResourceVersion, 10),
				CreatedAt:       policy.CreatedAt.UTC().Format(time.RFC3339Nano), UpdatedAt: policy.UpdatedAt.UTC().Format(time.RFC3339Nano),
			},
		},
		Spec: platformv1alpha1.StoragePolicySpec{
			ProjectRef:  commonv1alpha1.ProjectRef{Namespace: "cloud-agents", Kind: "project", ID: policy.Scope.ProjectID},
			UserSummary: policy.UserSummary, WorkspaceType: policy.WorkspaceType,
			WorkspaceCapacityBytes: policy.WorkspaceCapacityBytes, RetentionSeconds: policy.RetentionSeconds,
			CleanupOnLeaseTermination: policy.CleanupOnLeaseTermination,
			SnapshotBackendRef:        policy.SnapshotBackendRef, ArtifactBackendRef: policy.ArtifactBackendRef,
			AllowWorkspaceReuse: policy.AllowWorkspaceReuse,
		},
	}
}

func writeStoragePolicy(writer http.ResponseWriter, requestID string, policy internalstoragepolicy.Snapshot) {
	body, err := platformv1alpha1.EncodeStoragePolicyResponseJSON(commonv1alpha1.ResponseEnvelope[platformv1alpha1.StoragePolicy]{Value: storagePolicyResource(policy)})
	if err != nil {
		writePublicProblem(writer, http.StatusInternalServerError, "internal_error")
		return
	}
	writer.Header().Set("X-Resource-Version", strconv.FormatInt(policy.ResourceVersion, 10))
	writeJSONResponse(writer, http.StatusOK, requestID, body)
}

func storagePolicyPath(path string) (tenantID, projectID, policyID, action string, ok bool) {
	if !strings.HasPrefix(path, adminEnvironmentProfileRoutePrefix) {
		return "", "", "", "", false
	}
	parts := strings.Split(strings.TrimPrefix(path, adminEnvironmentProfileRoutePrefix), "/")
	if len(parts) == 4 && parts[1] == "projects" && parts[3] == "storage-policies" && parts[0] != "" && parts[2] != "" {
		return parts[0], parts[2], "", "list", true
	}
	if len(parts) == 5 && parts[1] == "projects" && parts[3] == "storage-policies" && parts[0] != "" && parts[2] != "" && parts[4] != "" {
		return parts[0], parts[2], parts[4], "get-set", true
	}
	if len(parts) == 6 && parts[1] == "projects" && parts[3] == "storage-policies" && parts[4] != "" && parts[5] == "audit-events" && parts[0] != "" && parts[2] != "" {
		return parts[0], parts[2], parts[4], "audit-events", true
	}
	return "", "", "", "", false
}

func storagePolicyPermission(action, method string) (projectPermission, policyPermission string, ok bool) {
	switch {
	case action == "list" && method == http.MethodGet:
		return "projects.get", "storage-policies.list", true
	case action == "get-set" && method == http.MethodGet:
		return "projects.get", "storage-policies.get", true
	case action == "get-set" && method == http.MethodPut:
		return "projects.act", "storage-policies.update", true
	case action == "audit-events" && method == http.MethodGet:
		return "projects.get", "audit.list", true
	default:
		return "", "", false
	}
}

func HandlesStoragePolicyPath(path string) bool {
	_, _, _, _, ok := storagePolicyPath(path)
	return ok
}

func encodeStoragePolicyPageToken(tenantID, projectID, policyID string) (string, bool) {
	if commonv1alpha1.ValidateIdentifier(tenantID, "/tenantId") != nil || commonv1alpha1.ValidateIdentifier(projectID, "/projectId") != nil || commonv1alpha1.ValidateIdentifier(policyID, "/storagePolicyId") != nil {
		return "", false
	}
	token := base64.RawURLEncoding.EncodeToString([]byte("storage-policy/v1\x00" + tenantID + "\x00" + projectID + "\x00" + policyID))
	return token, commonv1alpha1.ValidatePageToken(token, "/pageToken") == nil
}

func decodeStoragePolicyPageToken(tenantID, projectID, token string) (string, bool) {
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(token)
	if err != nil || commonv1alpha1.ValidatePageToken(token, "/pageToken") != nil {
		return "", false
	}
	parts := strings.Split(string(decoded), "\x00")
	if len(parts) != 4 || parts[0] != "storage-policy/v1" || parts[1] != tenantID || parts[2] != projectID || commonv1alpha1.ValidateIdentifier(parts[3], "/storagePolicyId") != nil {
		return "", false
	}
	return parts[3], true
}

func encodeStoragePolicyAuditPageToken(tenantID, projectID, policyID string, occurredAt time.Time, eventID string) (string, bool) {
	if occurredAt.IsZero() || commonv1alpha1.ValidateIdentifier(tenantID, "/tenantId") != nil || commonv1alpha1.ValidateIdentifier(projectID, "/projectId") != nil || commonv1alpha1.ValidateIdentifier(policyID, "/storagePolicyId") != nil || commonv1alpha1.ValidateIdentifier(eventID, "/eventId") != nil {
		return "", false
	}
	token := base64.RawURLEncoding.EncodeToString([]byte("storage-policy-audit/v1\x00" + tenantID + "\x00" + projectID + "\x00" + policyID + "\x00" + occurredAt.UTC().Format(time.RFC3339Nano) + "\x00" + eventID))
	return token, commonv1alpha1.ValidatePageToken(token, "/pageToken") == nil
}

func decodeStoragePolicyAuditPageToken(tenantID, projectID, policyID, token string) (*time.Time, string, bool) {
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(token)
	if err != nil || commonv1alpha1.ValidatePageToken(token, "/pageToken") != nil {
		return nil, "", false
	}
	parts := strings.Split(string(decoded), "\x00")
	if len(parts) != 6 || parts[0] != "storage-policy-audit/v1" || parts[1] != tenantID || parts[2] != projectID || parts[3] != policyID || commonv1alpha1.ValidateIdentifier(parts[5], "/eventId") != nil {
		return nil, "", false
	}
	occurredAt, err := time.Parse(time.RFC3339Nano, parts[4])
	if err != nil {
		return nil, "", false
	}
	return &occurredAt, parts[5], true
}

func writeStoragePolicyError(writer http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, postgres.ErrStoragePolicyNotFound):
		writePublicProblem(writer, http.StatusNotFound, "not_found")
	case errors.Is(err, postgres.ErrMutationDenied):
		writePublicProblem(writer, http.StatusForbidden, "authorization_denied")
	case errors.Is(err, postgres.ErrStoragePolicyIdempotencyConflict):
		writePublicProblem(writer, http.StatusConflict, "idempotency_conflict")
	case errors.Is(err, postgres.ErrStoragePolicyResourceVersionConflict), errors.Is(err, postgres.ErrCoordinationRejected):
		writePublicProblem(writer, http.StatusConflict, "storage_policy_resource_version_conflict")
	case errors.Is(err, postgres.ErrStoragePolicyReferenced):
		writePublicProblem(writer, http.StatusConflict, "storage_policy_referenced")
	case errors.Is(err, postgres.ErrCoordinationInvalidInput):
		writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
	default:
		writePublicProblem(writer, http.StatusInternalServerError, "internal_error")
	}
}
