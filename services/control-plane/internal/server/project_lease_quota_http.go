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
	internalprojectleasequota "github.com/hxp0618/cloud-agents/services/control-plane/internal/projectleasequota"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/store/postgres"
)

type projectLeaseQuotaStore interface {
	SetProjectLeaseQuota(context.Context, string, *authn.VerifiedPrincipal, internalprojectleasequota.SetInput) (internalprojectleasequota.Snapshot, error)
	GetProjectLeaseQuota(context.Context, string, *authn.VerifiedPrincipal, string) (internalprojectleasequota.Snapshot, error)
	ListProjectLeaseQuotaAuditEvents(context.Context, string, *authn.VerifiedPrincipal, string, *time.Time, string, int) (postgres.ProjectLeaseQuotaAuditPage, error)
}

type ProjectLeaseQuotaHTTPServer struct {
	verifier AccessTokenVerifier
	store    projectLeaseQuotaStore
}

func NewProjectLeaseQuotaHTTPServer(verifier AccessTokenVerifier, store projectLeaseQuotaStore) (*ProjectLeaseQuotaHTTPServer, error) {
	if verifier == nil || store == nil {
		return nil, errors.New("project lease quota HTTP server configuration is invalid")
	}
	return &ProjectLeaseQuotaHTTPServer{verifier: verifier, store: store}, nil
}

func (server *ProjectLeaseQuotaHTTPServer) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	preparePublicRequestID(writer, request)
	if server == nil || server.verifier == nil || server.store == nil || request == nil {
		writePublicProblem(writer, http.StatusInternalServerError, "internal_error")
		return
	}
	tenantID, projectID, admin, action, ok := projectLeaseQuotaPath(request.URL.Path)
	if !ok {
		writePublicProblem(writer, http.StatusNotFound, "route_not_found")
		return
	}
	projectPermission, quotaPermission, allowed := projectLeaseQuotaPermission(admin, action, request.Method)
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
	if _, err := server.verify(bearer, tenantID, projectID, projectPermission); err != nil {
		writePublicProblem(writer, http.StatusUnauthorized, "authentication_failed")
		return
	}
	if _, err := server.verify(bearer, tenantID, projectID, quotaPermission); err != nil {
		writePublicProblem(writer, http.StatusForbidden, "authorization_denied")
		return
	}
	if action == "audit-events" {
		server.listAuditEvents(writer, request, tenantID, projectID, requestID, bearer)
		return
	}
	if request.Method == http.MethodPut {
		server.set(writer, request, tenantID, projectID, requestID, bearer)
		return
	}
	server.get(writer, request, tenantID, projectID, requestID, bearer, admin)
}

func (server *ProjectLeaseQuotaHTTPServer) get(writer http.ResponseWriter, request *http.Request, tenantID, projectID, requestID, bearer string, admin bool) {
	var err error
	if admin {
		_, err = openapiv1alpha1.ValidateGetAdminProjectLeaseQuotaServerRequest(tenantID, projectID, requestID)
	} else {
		_, err = openapiv1alpha1.ValidateGetProjectLeaseQuotaServerRequest(tenantID, projectID, requestID)
	}
	if err != nil {
		writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	principal, err := server.verify(bearer, tenantID, projectID, "projects.get")
	if err != nil {
		writePublicProblem(writer, http.StatusUnauthorized, "authentication_failed")
		return
	}
	quota, err := server.store.GetProjectLeaseQuota(request.Context(), tenantID, principal, projectID)
	if err != nil {
		writeProjectLeaseQuotaError(writer, err)
		return
	}
	if admin {
		writeProjectLeaseQuota(writer, requestID, quota)
		return
	}
	writeProjectLeaseQuotaSummary(writer, requestID, quota)
}

func (server *ProjectLeaseQuotaHTTPServer) set(writer http.ResponseWriter, request *http.Request, tenantID, projectID, requestID, bearer string) {
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
	validated, err := openapiv1alpha1.ValidateSetAdminProjectLeaseQuotaServerRequest(tenantID, projectID, requestID, idempotencyKey, body)
	if err != nil {
		writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	expectedResourceVersion, err := strconv.ParseInt(validated.Body.ExpectedResourceVersion, 10, 64)
	if err != nil || expectedResourceVersion < 0 {
		writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	principal, err := server.verify(bearer, tenantID, projectID, "projects.act")
	if err != nil {
		writePublicProblem(writer, http.StatusUnauthorized, "authentication_failed")
		return
	}
	quota, err := server.store.SetProjectLeaseQuota(request.Context(), tenantID, principal, internalprojectleasequota.SetInput{
		Scope:                   internalprojectleasequota.Scope{TenantID: tenantID, ProjectID: projectID},
		ExpectedResourceVersion: expectedResourceVersion,
		MaxConcurrentLeases:     validated.Body.MaxConcurrentLeases,
		MaxCPUMillis:            validated.Body.MaxCPUMillis,
		MaxMemoryBytes:          validated.Body.MaxMemoryBytes,
		MaxLeaseTTLSeconds:      validated.Body.MaxLeaseTTLSeconds,
		Mutation:                internalprojectleasequota.Mutation{RequestID: requestID, IdempotencyKey: idempotencyKey},
	})
	if err != nil {
		writeProjectLeaseQuotaError(writer, err)
		return
	}
	writeProjectLeaseQuota(writer, requestID, quota)
}

func (server *ProjectLeaseQuotaHTTPServer) listAuditEvents(writer http.ResponseWriter, request *http.Request, tenantID, projectID, requestID, bearer string) {
	pageSize, pageToken, ok := managedAgentPagination(request)
	if !ok {
		writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	validated, err := openapiv1alpha1.ValidateListAdminProjectLeaseQuotaAuditEventsServerRequest(tenantID, projectID, requestID, pageSize, pageToken)
	if err != nil {
		writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	principal, err := server.verify(bearer, tenantID, projectID, "projects.get")
	if err != nil {
		writePublicProblem(writer, http.StatusUnauthorized, "authentication_failed")
		return
	}
	if _, err := server.store.GetProjectLeaseQuota(request.Context(), tenantID, principal, projectID); err != nil {
		writeProjectLeaseQuotaError(writer, err)
		return
	}
	var after *time.Time
	afterEventID := ""
	if validated.PageToken != "" {
		if after, afterEventID, ok = decodeProjectLeaseQuotaAuditPageToken(tenantID, projectID, validated.PageToken); !ok {
			writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
			return
		}
	}
	principal, err = server.verify(bearer, tenantID, projectID, "projects.get")
	if err != nil {
		writePublicProblem(writer, http.StatusUnauthorized, "authentication_failed")
		return
	}
	page, err := server.store.ListProjectLeaseQuotaAuditEvents(request.Context(), tenantID, principal, projectID, after, afterEventID, validated.PageSize)
	if err != nil {
		writeProjectLeaseQuotaError(writer, err)
		return
	}
	events := make([]platformv1alpha1.AdminAuditEvent, 0, len(page.Events))
	for _, event := range page.Events {
		events = append(events, platformv1alpha1.AdminAuditEvent{
			APIVersion: platformv1alpha1.APIVersion, Kind: "AdminAuditEvent",
			EventID: event.EventID, Actor: event.Actor, Action: event.Action,
			ResourceKind: "ProjectLeaseQuota", ResourceID: event.QuotaID,
			ResourceGeneration: event.QuotaResourceVersion, Result: event.Result,
			OccurredAt: event.OccurredAt.UTC().Format(time.RFC3339Nano), RequestID: event.RequestID,
			OperationID: event.OperationID,
		})
	}
	nextPageToken := ""
	if page.NextOccurredAt != nil {
		if nextPageToken, ok = encodeProjectLeaseQuotaAuditPageToken(tenantID, projectID, *page.NextOccurredAt, page.NextEventID); !ok {
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

func (server *ProjectLeaseQuotaHTTPServer) verify(bearer, tenantID, projectID, permission string) (*authn.VerifiedPrincipal, error) {
	return server.verifier.Verify(bearer, authn.VerificationRequest{TenantID: tenantID, ResourceLevel: "project", ResourceID: projectID, RequiredPermission: permission})
}

func writeProjectLeaseQuota(writer http.ResponseWriter, requestID string, quota internalprojectleasequota.Snapshot) {
	value := platformv1alpha1.ProjectLeaseQuota{
		ResourceBase: platformv1alpha1.ResourceBase{
			APIVersion: platformv1alpha1.APIVersion, Kind: "ProjectLeaseQuota",
			Metadata: commonv1alpha1.ResourceMetadata{
				UID: quota.QuotaID, Name: quota.QuotaName,
				TenantRef:       commonv1alpha1.TenantRef{Namespace: "cloud-agents", Kind: "tenant", ID: quota.Scope.TenantID},
				ResourceVersion: strconv.FormatInt(quota.ResourceVersion, 10),
				CreatedAt:       quota.CreatedAt.UTC().Format(time.RFC3339Nano), UpdatedAt: quota.UpdatedAt.UTC().Format(time.RFC3339Nano),
			},
		},
		Spec: platformv1alpha1.ProjectLeaseQuotaSpec{
			ProjectRef:          commonv1alpha1.ProjectRef{Namespace: "cloud-agents", Kind: "project", ID: quota.Scope.ProjectID},
			MaxConcurrentLeases: quota.MaxConcurrentLeases, MaxCPUMillis: quota.MaxCPUMillis,
			MaxMemoryBytes: quota.MaxMemoryBytes, MaxLeaseTTLSeconds: quota.MaxLeaseTTLSeconds,
		},
		Status: platformv1alpha1.ProjectLeaseQuotaStatus{
			ActiveLeases: quota.ActiveLeases, UsedCPUMillis: quota.UsedCPUMillis, UsedMemoryBytes: quota.UsedMemoryBytes,
		},
	}
	body, err := platformv1alpha1.EncodeProjectLeaseQuotaResponseJSON(commonv1alpha1.ResponseEnvelope[platformv1alpha1.ProjectLeaseQuota]{Value: value})
	if err != nil {
		writePublicProblem(writer, http.StatusInternalServerError, "internal_error")
		return
	}
	writer.Header().Set("X-Resource-Version", strconv.FormatInt(quota.ResourceVersion, 10))
	writeJSONResponse(writer, http.StatusOK, requestID, body)
}

func writeProjectLeaseQuotaSummary(writer http.ResponseWriter, requestID string, quota internalprojectleasequota.Snapshot) {
	value := platformv1alpha1.ProjectLeaseQuotaSummary{
		APIVersion: platformv1alpha1.APIVersion, Kind: "ProjectLeaseQuotaSummary",
		ProjectRef:          commonv1alpha1.ProjectRef{Namespace: "cloud-agents", Kind: "project", ID: quota.Scope.ProjectID},
		MaxConcurrentLeases: quota.MaxConcurrentLeases, ActiveLeases: quota.ActiveLeases,
		MaxCPUMillis: quota.MaxCPUMillis, UsedCPUMillis: quota.UsedCPUMillis,
		MaxMemoryBytes: quota.MaxMemoryBytes, UsedMemoryBytes: quota.UsedMemoryBytes,
		MaxLeaseTTLSeconds: quota.MaxLeaseTTLSeconds,
	}
	body, err := platformv1alpha1.EncodeProjectLeaseQuotaSummaryResponseJSON(commonv1alpha1.ResponseEnvelope[platformv1alpha1.ProjectLeaseQuotaSummary]{Value: value})
	if err != nil {
		writePublicProblem(writer, http.StatusInternalServerError, "internal_error")
		return
	}
	writeJSONResponse(writer, http.StatusOK, requestID, body)
}

func projectLeaseQuotaPath(path string) (tenantID, projectID string, admin bool, action string, ok bool) {
	prefix := UserEnvironmentRoutePrefix
	if strings.HasPrefix(path, adminEnvironmentProfileRoutePrefix) {
		prefix, admin = adminEnvironmentProfileRoutePrefix, true
	} else if !strings.HasPrefix(path, prefix) {
		return "", "", false, "", false
	}
	parts := strings.Split(strings.TrimPrefix(path, prefix), "/")
	if len(parts) == 4 && parts[1] == "projects" && parts[3] == "lease-quota" && parts[0] != "" && parts[2] != "" {
		return parts[0], parts[2], admin, "get-set", true
	}
	if admin && len(parts) == 5 && parts[1] == "projects" && parts[3] == "lease-quota" && parts[4] == "audit-events" && parts[0] != "" && parts[2] != "" {
		return parts[0], parts[2], true, "audit-events", true
	}
	return "", "", false, "", false
}

func projectLeaseQuotaPermission(admin bool, action, method string) (projectPermission, quotaPermission string, ok bool) {
	switch {
	case admin && action == "get-set" && method == http.MethodGet:
		return "projects.get", "quotas.get", true
	case admin && action == "get-set" && method == http.MethodPut:
		return "projects.act", "quotas.update", true
	case admin && action == "audit-events" && method == http.MethodGet:
		return "projects.get", "audit.list", true
	case !admin && action == "get-set" && method == http.MethodGet:
		return "projects.get", "environment-quotas.get", true
	default:
		return "", "", false
	}
}

func HandlesProjectLeaseQuotaPath(path string) bool {
	_, _, _, _, ok := projectLeaseQuotaPath(path)
	return ok
}

func encodeProjectLeaseQuotaAuditPageToken(tenantID, projectID string, occurredAt time.Time, eventID string) (string, bool) {
	if occurredAt.IsZero() || commonv1alpha1.ValidateIdentifier(tenantID, "/tenantId") != nil || commonv1alpha1.ValidateIdentifier(projectID, "/projectId") != nil || commonv1alpha1.ValidateIdentifier(eventID, "/eventId") != nil {
		return "", false
	}
	token := base64.RawURLEncoding.EncodeToString([]byte("project-lease-quota-audit/v1\x00" + tenantID + "\x00" + projectID + "\x00" + occurredAt.UTC().Format(time.RFC3339Nano) + "\x00" + eventID))
	return token, commonv1alpha1.ValidatePageToken(token, "/pageToken") == nil
}

func decodeProjectLeaseQuotaAuditPageToken(tenantID, projectID, token string) (*time.Time, string, bool) {
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(token)
	if err != nil || commonv1alpha1.ValidatePageToken(token, "/pageToken") != nil {
		return nil, "", false
	}
	parts := strings.Split(string(decoded), "\x00")
	if len(parts) != 5 || parts[0] != "project-lease-quota-audit/v1" || parts[1] != tenantID || parts[2] != projectID || commonv1alpha1.ValidateIdentifier(parts[4], "/eventId") != nil {
		return nil, "", false
	}
	occurredAt, err := time.Parse(time.RFC3339Nano, parts[3])
	if err != nil {
		return nil, "", false
	}
	return &occurredAt, parts[4], true
}

func writeProjectLeaseQuotaError(writer http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, postgres.ErrProjectLeaseQuotaNotFound):
		writePublicProblem(writer, http.StatusNotFound, "not_found")
	case errors.Is(err, postgres.ErrMutationDenied):
		writePublicProblem(writer, http.StatusForbidden, "authorization_denied")
	case errors.Is(err, postgres.ErrProjectLeaseQuotaIdempotencyConflict):
		writePublicProblem(writer, http.StatusConflict, "idempotency_conflict")
	case errors.Is(err, postgres.ErrProjectLeaseQuotaResourceVersionConflict), errors.Is(err, postgres.ErrCoordinationRejected):
		writePublicProblem(writer, http.StatusConflict, "quota_resource_version_conflict")
	case errors.Is(err, postgres.ErrCoordinationInvalidInput):
		writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
	default:
		writePublicProblem(writer, http.StatusInternalServerError, "internal_error")
	}
}
