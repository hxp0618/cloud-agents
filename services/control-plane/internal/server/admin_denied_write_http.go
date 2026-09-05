package server

import (
	"context"
	"net/http"
	"strings"
	"time"

	common "github.com/hxp0618/cloud-agents/sdk/go/gen/common/v1alpha1"
	openapi "github.com/hxp0618/cloud-agents/sdk/go/gen/openapi/v1alpha1"
	platform "github.com/hxp0618/cloud-agents/sdk/go/gen/platform/v1alpha1"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authn"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/store/postgres"
)

// AdminDeniedWriteHandler is shared by local and production Admin routing. It does
// not grant or replace any handler permission; only a real forbidden response is recorded.
func AdminDeniedWriteHandler(verifier AccessTokenVerifier, store *postgres.DurableCoordinationService, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if tenant, project, ok := adminDeniedWriteListPath(r.URL.Path); ok {
			listAdminDeniedWrites(w, r, verifier, store, tenant, project)
			return
		}
		event, ok := adminDeniedWriteRoute(r)
		if !ok {
			next.ServeHTTP(w, r)
			return
		}
		authorization, ok := exactSingleHeader(r.Header, "Authorization")
		bearer, bearerOK := bearerToken(authorization)
		if !ok || !bearerOK {
			next.ServeHTTP(w, r)
			return
		}
		principal, err := verifier.Verify(bearer, authn.VerificationRequest{TenantID: event.TenantID, ResourceLevel: "project", ResourceID: event.ProjectID, RequiredPermission: "projects.act"})
		if err != nil {
			next.ServeHTTP(w, r)
			return
		}
		writer := &adminDeniedWriteResponse{ResponseWriter: w, record: func() error {
			// Rejection evidence survives a caller disconnect, with a bounded database deadline.
			ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 5*time.Second)
			defer cancel()
			return store.RecordAdminDeniedWrite(ctx, principal, event)
		}}
		next.ServeHTTP(writer, r)
	})
}

type adminDeniedWriteResponse struct {
	http.ResponseWriter
	record                func() error
	wroteHeader, replaced bool
}

func (w *adminDeniedWriteResponse) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w *adminDeniedWriteResponse) WriteHeader(status int) {
	if w.wroteHeader {
		return
	}
	w.wroteHeader = true
	if status == http.StatusForbidden && w.record() != nil {
		w.replaced = true
		w.Header().Del("Content-Length")
		writePublicProblem(w.ResponseWriter, http.StatusServiceUnavailable, "ADMIN_AUDIT_UNAVAILABLE")
		return
	}
	w.ResponseWriter.WriteHeader(status)
}
func (w *adminDeniedWriteResponse) Write(body []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	if w.replaced {
		return len(body), nil
	}
	return w.ResponseWriter.Write(body)
}

func adminDeniedWriteRoute(r *http.Request) (postgres.AdminDeniedWrite, bool) {
	var event postgres.AdminDeniedWrite
	if r.Method != http.MethodPost && r.Method != http.MethodPut {
		return event, false
	}
	if tenant, project, id, action, ok := adminDeploymentTargetPath(r.URL.Path); ok && r.Method == http.MethodPost {
		event.TenantID, event.ProjectID, event.ResourceID = tenant, project, id
		event.Action = map[string]string{"collection": "adminRegisterDeploymentTarget", "probe": "adminProbeDeploymentTarget", "scheduling": "adminTransitionDeploymentTargetScheduling", "cleanup": "adminCleanupDeploymentTarget"}[action]
	} else if tenant, project, id, action, ok := adminEnvironmentLeasePath(r.URL.Path); ok && r.Method == http.MethodPost {
		event.TenantID, event.ProjectID, event.ResourceID = tenant, project, id
		event.Action = map[string]string{"upgrade": "adminUpgradeEnvironmentLease", "rollback": "adminRollbackEnvironmentLease"}[action]
	} else if tenant, project, id, version, action, ok := adminEnvironmentProfilePath(r.URL.Path); ok && r.Method == http.MethodPost {
		event.TenantID, event.ProjectID, event.ResourceID, event.ProfileVersion = tenant, project, id, version
		event.Action = map[string]string{"collection": "adminCreateEnvironmentProfile", "publish": "adminPublishEnvironmentProfile", "disable": "adminDisableEnvironmentProfile"}[action]
	} else if tenant, project, ok := adminWorkerReleasePath(r.URL.Path); ok && r.Method == http.MethodPost {
		event.TenantID, event.ProjectID, event.Action = tenant, project, "adminRegisterWorkerRelease"
	} else if tenant, project, id, action, ok := storagePolicyPath(r.URL.Path); ok && action == "get-set" && r.Method == http.MethodPut {
		event.TenantID, event.ProjectID, event.ResourceID, event.Action = tenant, project, id, "adminSetStoragePolicy"
	} else if tenant, project, id, action, ok := networkPolicyPath(r.URL.Path); ok && action == "get-set" && r.Method == http.MethodPut {
		event.TenantID, event.ProjectID, event.ResourceID, event.Action = tenant, project, id, "adminSetNetworkPolicy"
	} else if tenant, project, admin, action, ok := projectLeaseQuotaPath(r.URL.Path); ok && admin && action == "get-set" && r.Method == http.MethodPut {
		event.TenantID, event.ProjectID, event.Action = tenant, project, "adminSetProjectLeaseQuota"
	}
	event.RequestID, _ = exactSingleHeader(r.Header, "X-Request-ID")
	// Invalid caller correlation data must not bypass denial evidence or enter storage.
	// Keep the original request untouched so its normal validation still applies.
	if common.ValidateIdentifier(event.RequestID, "/requestId") != nil {
		event.RequestID = publicFallbackRequestID
	}
	if event.Action == "" || common.ValidateIdentifier(event.TenantID, "/tenantId") != nil || common.ValidateIdentifier(event.ProjectID, "/projectId") != nil ||
		event.ResourceID != "" && common.ValidateIdentifier(event.ResourceID, "/resourceId") != nil {
		return postgres.AdminDeniedWrite{}, false
	}
	return event, true
}

func adminDeniedWriteListPath(path string) (string, string, bool) {
	if !strings.HasPrefix(path, adminDeploymentTargetRoutePrefix) {
		return "", "", false
	}
	parts := strings.Split(strings.TrimPrefix(path, adminDeploymentTargetRoutePrefix), "/")
	if len(parts) != 4 || parts[1] != "projects" || parts[3] != "denied-write-events" {
		return "", "", false
	}
	return parts[0], parts[2], true
}

func listAdminDeniedWrites(w http.ResponseWriter, r *http.Request, verifier AccessTokenVerifier, store *postgres.DurableCoordinationService, tenant, project string) {
	preparePublicRequestID(w, r)
	if r.Method != http.MethodGet {
		writePublicProblem(w, 405, "METHOD_NOT_ALLOWED")
		return
	}
	authorization, ok := exactSingleHeader(r.Header, "Authorization")
	bearer, bearerOK := bearerToken(authorization)
	if !ok || !bearerOK {
		writePublicProblem(w, 401, "AUTHENTICATION_FAILED")
		return
	}
	principal, err := verifier.Verify(bearer, authn.VerificationRequest{TenantID: tenant, ResourceLevel: "project", ResourceID: project, RequiredPermission: "projects.get"})
	if err != nil {
		writePublicProblem(w, 401, "AUTHENTICATION_FAILED")
		return
	}
	if _, err := verifier.Verify(bearer, authn.VerificationRequest{TenantID: tenant, ResourceLevel: "project", ResourceID: project, RequiredPermission: "audit.list"}); err != nil {
		writePublicProblem(w, 403, "AUTHORIZATION_DENIED")
		return
	}
	requestID, idOK := exactSingleHeader(r.Header, "X-Request-ID")
	size, token, ok := managedAgentPagination(r)
	input, err := openapi.ValidateListMaintenanceOperationsServerRequest(tenant, project, requestID, size, token)
	if !ok || !idOK || err != nil {
		writePublicProblem(w, 400, "INVALID_REQUEST")
		return
	}
	after := ""
	if input.PageToken != "" {
		after, ok = decodeProjectResourcePageToken("admin-denied-write/v1", tenant, project, input.PageToken)
		if !ok {
			writePublicProblem(w, 400, "INVALID_REQUEST")
			return
		}
	}
	page, err := store.ListAdminDeniedWrites(r.Context(), tenant, principal, project, after, input.PageSize)
	if err != nil {
		writeDeploymentTargetError(w, err)
		return
	}
	events := make([]platform.AdminDeniedWriteEvent, 0, len(page.Events))
	for _, event := range page.Events {
		events = append(events, platform.AdminDeniedWriteEvent{APIVersion: platform.APIVersion, Kind: "AdminDeniedWriteEvent",
			EventID: event.EventID, TenantID: event.TenantID, ProjectID: event.ProjectID, Actor: event.Actor,
			Action: event.Action, ResourceID: event.ResourceID, ProfileVersion: event.ProfileVersion,
			Result: "denied", StableErrorCode: "AUTHORIZATION_DENIED", RequestID: event.RequestID, OccurredAt: event.OccurredAt.UTC().Format(time.RFC3339Nano)})
	}
	next := ""
	if page.NextEventID != "" {
		next, ok = encodeProjectResourcePageToken("admin-denied-write/v1", tenant, project, page.NextEventID)
		if !ok {
			writePublicProblem(w, 500, "INTERNAL_ERROR")
			return
		}
	}
	body, err := platform.EncodeAdminDeniedWriteEventPageResponseJSON(common.ResponseEnvelope[platform.AdminDeniedWriteEventPage]{Value: platform.AdminDeniedWriteEventPage{
		APIVersion: platform.APIVersion, Kind: "AdminDeniedWriteEventPage", Events: events, NextPageToken: next}})
	if err != nil {
		writePublicProblem(w, 500, "INTERNAL_ERROR")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(body)
}
