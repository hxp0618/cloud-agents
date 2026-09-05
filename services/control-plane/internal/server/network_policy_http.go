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
	internalnetworkpolicy "github.com/hxp0618/cloud-agents/services/control-plane/internal/networkpolicy"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/store/postgres"
)

type networkPolicyStore interface {
	SetNetworkPolicy(context.Context, string, *authn.VerifiedPrincipal, internalnetworkpolicy.SetInput) (internalnetworkpolicy.Snapshot, error)
	GetNetworkPolicy(context.Context, string, *authn.VerifiedPrincipal, string, string) (internalnetworkpolicy.Snapshot, error)
	ListNetworkPolicies(context.Context, string, *authn.VerifiedPrincipal, string, string, int) (postgres.NetworkPolicyPage, error)
	ListNetworkPolicyAuditEvents(context.Context, string, *authn.VerifiedPrincipal, string, string, *time.Time, string, int) (postgres.NetworkPolicyAuditPage, error)
}

type NetworkPolicyHTTPServer struct {
	verifier AccessTokenVerifier
	store    networkPolicyStore
}

func NewNetworkPolicyHTTPServer(verifier AccessTokenVerifier, store networkPolicyStore) (*NetworkPolicyHTTPServer, error) {
	if verifier == nil || store == nil {
		return nil, errors.New("network policy HTTP server configuration is invalid")
	}
	return &NetworkPolicyHTTPServer{verifier: verifier, store: store}, nil
}

func (server *NetworkPolicyHTTPServer) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	preparePublicRequestID(writer, request)
	if server == nil || server.verifier == nil || server.store == nil || request == nil {
		writePublicProblem(writer, http.StatusInternalServerError, "internal_error")
		return
	}
	tenantID, projectID, policyID, action, ok := networkPolicyPath(request.URL.Path)
	if !ok {
		writePublicProblem(writer, http.StatusNotFound, "route_not_found")
		return
	}
	projectPermission, policyPermission, allowed := networkPolicyPermission(action, request.Method)
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

func (server *NetworkPolicyHTTPServer) list(writer http.ResponseWriter, request *http.Request, tenantID, projectID, requestID string, principal *authn.VerifiedPrincipal) {
	pageSize, pageToken, ok := managedAgentPagination(request)
	if !ok {
		writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	validated, err := openapiv1alpha1.ValidateListAdminNetworkPoliciesServerRequest(tenantID, projectID, requestID, pageSize, pageToken)
	if err != nil {
		writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	afterPolicyID := ""
	if validated.PageToken != "" {
		if afterPolicyID, ok = decodeNetworkPolicyPageToken(tenantID, projectID, validated.PageToken); !ok {
			writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
			return
		}
	}
	page, err := server.store.ListNetworkPolicies(request.Context(), tenantID, principal, projectID, afterPolicyID, validated.PageSize)
	if err != nil {
		writeNetworkPolicyError(writer, err)
		return
	}
	policies := make([]platformv1alpha1.NetworkPolicy, 0, len(page.NetworkPolicies))
	for _, policy := range page.NetworkPolicies {
		policies = append(policies, networkPolicyResource(policy))
	}
	nextPageToken := ""
	if page.NextNetworkPolicyID != "" {
		if nextPageToken, ok = encodeNetworkPolicyPageToken(tenantID, projectID, page.NextNetworkPolicyID); !ok {
			writePublicProblem(writer, http.StatusInternalServerError, "internal_error")
			return
		}
	}
	body, err := platformv1alpha1.EncodeNetworkPolicyPageResponseJSON(commonv1alpha1.ResponseEnvelope[platformv1alpha1.NetworkPolicyPage]{Value: platformv1alpha1.NetworkPolicyPage{
		APIVersion: platformv1alpha1.APIVersion, Kind: "NetworkPolicyPage", NetworkPolicies: policies, NextPageToken: nextPageToken,
	}})
	if err != nil {
		writePublicProblem(writer, http.StatusInternalServerError, "internal_error")
		return
	}
	writeJSONResponse(writer, http.StatusOK, requestID, body)
}

func (server *NetworkPolicyHTTPServer) get(writer http.ResponseWriter, request *http.Request, tenantID, projectID, policyID, requestID string, principal *authn.VerifiedPrincipal) {
	if _, err := openapiv1alpha1.ValidateGetAdminNetworkPolicyServerRequest(tenantID, projectID, policyID, requestID); err != nil {
		writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	policy, err := server.store.GetNetworkPolicy(request.Context(), tenantID, principal, projectID, policyID)
	if err != nil {
		writeNetworkPolicyError(writer, err)
		return
	}
	writeNetworkPolicy(writer, requestID, policy)
}

func (server *NetworkPolicyHTTPServer) set(writer http.ResponseWriter, request *http.Request, tenantID, projectID, policyID, requestID string, principal *authn.VerifiedPrincipal) {
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
	validated, err := openapiv1alpha1.ValidateSetAdminNetworkPolicyServerRequest(tenantID, projectID, policyID, requestID, idempotencyKey, body)
	if err != nil {
		writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	expectedResourceVersion, err := strconv.ParseInt(validated.Body.ExpectedResourceVersion, 10, 64)
	if err != nil || expectedResourceVersion < 0 {
		writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	policy, err := server.store.SetNetworkPolicy(request.Context(), tenantID, principal, internalnetworkpolicy.SetInput{
		Scope:    internalnetworkpolicy.Scope{TenantID: tenantID, ProjectID: projectID},
		PolicyID: policyID, PolicyName: validated.Body.PolicyName, UserSummary: validated.Body.UserSummary,
		DefaultEgress: validated.Body.DefaultEgress, AllowlistPolicyRef: validated.Body.AllowlistPolicyRef,
		IngressEnabled: validated.Body.IngressEnabled, PreviewEnabled: validated.Body.PreviewEnabled,
		DNSPolicyRef: validated.Body.DNSPolicyRef, ProxyPolicyRef: validated.Body.ProxyPolicyRef,
		ExpectedResourceVersion: expectedResourceVersion,
		Mutation:                internalnetworkpolicy.Mutation{RequestID: requestID, IdempotencyKey: idempotencyKey},
	})
	if err != nil {
		writeNetworkPolicyError(writer, err)
		return
	}
	writeNetworkPolicy(writer, requestID, policy)
}

func (server *NetworkPolicyHTTPServer) listAuditEvents(writer http.ResponseWriter, request *http.Request, tenantID, projectID, policyID, requestID string, principal *authn.VerifiedPrincipal) {
	pageSize, pageToken, ok := managedAgentPagination(request)
	if !ok {
		writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	validated, err := openapiv1alpha1.ValidateListAdminNetworkPolicyAuditEventsServerRequest(tenantID, projectID, policyID, requestID, pageSize, pageToken)
	if err != nil {
		writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	var after *time.Time
	afterEventID := ""
	if validated.PageToken != "" {
		if after, afterEventID, ok = decodeNetworkPolicyAuditPageToken(tenantID, projectID, policyID, validated.PageToken); !ok {
			writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
			return
		}
	}
	page, err := server.store.ListNetworkPolicyAuditEvents(request.Context(), tenantID, principal, projectID, policyID, after, afterEventID, validated.PageSize)
	if err != nil {
		writeNetworkPolicyError(writer, err)
		return
	}
	events := make([]platformv1alpha1.AdminAuditEvent, 0, len(page.Events))
	for _, event := range page.Events {
		events = append(events, platformv1alpha1.AdminAuditEvent{
			APIVersion: platformv1alpha1.APIVersion, Kind: "AdminAuditEvent",
			EventID: event.EventID, Actor: event.Actor, Action: event.Action,
			ResourceKind: "NetworkPolicy", ResourceID: event.PolicyID,
			ResourceGeneration: event.PolicyResourceVersion, Result: event.Result,
			OccurredAt: event.OccurredAt.UTC().Format(time.RFC3339Nano), RequestID: event.RequestID,
			OperationID: event.OperationID,
		})
	}
	nextPageToken := ""
	if page.NextOccurredAt != nil {
		if nextPageToken, ok = encodeNetworkPolicyAuditPageToken(tenantID, projectID, policyID, *page.NextOccurredAt, page.NextEventID); !ok {
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

func (server *NetworkPolicyHTTPServer) verify(bearer, tenantID, projectID, permission string) (*authn.VerifiedPrincipal, error) {
	return server.verifier.Verify(bearer, authn.VerificationRequest{TenantID: tenantID, ResourceLevel: "project", ResourceID: projectID, RequiredPermission: permission})
}

func networkPolicyResource(policy internalnetworkpolicy.Snapshot) platformv1alpha1.NetworkPolicy {
	return platformv1alpha1.NetworkPolicy{
		ResourceBase: platformv1alpha1.ResourceBase{
			APIVersion: platformv1alpha1.APIVersion, Kind: "NetworkPolicy",
			Metadata: commonv1alpha1.ResourceMetadata{
				UID: policy.PolicyID, Name: policy.PolicyName,
				TenantRef:       commonv1alpha1.TenantRef{Namespace: "cloud-agents", Kind: "tenant", ID: policy.Scope.TenantID},
				ResourceVersion: strconv.FormatInt(policy.ResourceVersion, 10),
				CreatedAt:       policy.CreatedAt.UTC().Format(time.RFC3339Nano), UpdatedAt: policy.UpdatedAt.UTC().Format(time.RFC3339Nano),
			},
		},
		Spec: platformv1alpha1.NetworkPolicySpec{
			ProjectRef:  commonv1alpha1.ProjectRef{Namespace: "cloud-agents", Kind: "project", ID: policy.Scope.ProjectID},
			UserSummary: policy.UserSummary, DefaultEgress: policy.DefaultEgress,
			AllowlistPolicyRef: policy.AllowlistPolicyRef, IngressEnabled: policy.IngressEnabled,
			PreviewEnabled: policy.PreviewEnabled, DNSPolicyRef: policy.DNSPolicyRef, ProxyPolicyRef: policy.ProxyPolicyRef,
		},
	}
}

func writeNetworkPolicy(writer http.ResponseWriter, requestID string, policy internalnetworkpolicy.Snapshot) {
	body, err := platformv1alpha1.EncodeNetworkPolicyResponseJSON(commonv1alpha1.ResponseEnvelope[platformv1alpha1.NetworkPolicy]{Value: networkPolicyResource(policy)})
	if err != nil {
		writePublicProblem(writer, http.StatusInternalServerError, "internal_error")
		return
	}
	writer.Header().Set("X-Resource-Version", strconv.FormatInt(policy.ResourceVersion, 10))
	writeJSONResponse(writer, http.StatusOK, requestID, body)
}

func networkPolicyPath(path string) (tenantID, projectID, policyID, action string, ok bool) {
	if !strings.HasPrefix(path, adminEnvironmentProfileRoutePrefix) {
		return "", "", "", "", false
	}
	parts := strings.Split(strings.TrimPrefix(path, adminEnvironmentProfileRoutePrefix), "/")
	if len(parts) == 4 && parts[1] == "projects" && parts[3] == "network-policies" && parts[0] != "" && parts[2] != "" {
		return parts[0], parts[2], "", "list", true
	}
	if len(parts) == 5 && parts[1] == "projects" && parts[3] == "network-policies" && parts[0] != "" && parts[2] != "" && parts[4] != "" {
		return parts[0], parts[2], parts[4], "get-set", true
	}
	if len(parts) == 6 && parts[1] == "projects" && parts[3] == "network-policies" && parts[4] != "" && parts[5] == "audit-events" && parts[0] != "" && parts[2] != "" {
		return parts[0], parts[2], parts[4], "audit-events", true
	}
	return "", "", "", "", false
}

func networkPolicyPermission(action, method string) (projectPermission, policyPermission string, ok bool) {
	switch {
	case action == "list" && method == http.MethodGet:
		return "projects.get", "network-policies.list", true
	case action == "get-set" && method == http.MethodGet:
		return "projects.get", "network-policies.get", true
	case action == "get-set" && method == http.MethodPut:
		return "projects.act", "network-policies.update", true
	case action == "audit-events" && method == http.MethodGet:
		return "projects.get", "audit.list", true
	default:
		return "", "", false
	}
}

func HandlesNetworkPolicyPath(path string) bool {
	_, _, _, _, ok := networkPolicyPath(path)
	return ok
}

func encodeNetworkPolicyPageToken(tenantID, projectID, policyID string) (string, bool) {
	if commonv1alpha1.ValidateIdentifier(tenantID, "/tenantId") != nil || commonv1alpha1.ValidateIdentifier(projectID, "/projectId") != nil || commonv1alpha1.ValidateIdentifier(policyID, "/networkPolicyId") != nil {
		return "", false
	}
	token := base64.RawURLEncoding.EncodeToString([]byte("network-policy/v1\x00" + tenantID + "\x00" + projectID + "\x00" + policyID))
	return token, commonv1alpha1.ValidatePageToken(token, "/pageToken") == nil
}

func decodeNetworkPolicyPageToken(tenantID, projectID, token string) (string, bool) {
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(token)
	if err != nil || commonv1alpha1.ValidatePageToken(token, "/pageToken") != nil {
		return "", false
	}
	parts := strings.Split(string(decoded), "\x00")
	if len(parts) != 4 || parts[0] != "network-policy/v1" || parts[1] != tenantID || parts[2] != projectID || commonv1alpha1.ValidateIdentifier(parts[3], "/networkPolicyId") != nil {
		return "", false
	}
	return parts[3], true
}

func encodeNetworkPolicyAuditPageToken(tenantID, projectID, policyID string, occurredAt time.Time, eventID string) (string, bool) {
	if occurredAt.IsZero() || commonv1alpha1.ValidateIdentifier(tenantID, "/tenantId") != nil || commonv1alpha1.ValidateIdentifier(projectID, "/projectId") != nil || commonv1alpha1.ValidateIdentifier(policyID, "/networkPolicyId") != nil || commonv1alpha1.ValidateIdentifier(eventID, "/eventId") != nil {
		return "", false
	}
	token := base64.RawURLEncoding.EncodeToString([]byte("network-policy-audit/v1\x00" + tenantID + "\x00" + projectID + "\x00" + policyID + "\x00" + occurredAt.UTC().Format(time.RFC3339Nano) + "\x00" + eventID))
	return token, commonv1alpha1.ValidatePageToken(token, "/pageToken") == nil
}

func decodeNetworkPolicyAuditPageToken(tenantID, projectID, policyID, token string) (*time.Time, string, bool) {
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(token)
	if err != nil || commonv1alpha1.ValidatePageToken(token, "/pageToken") != nil {
		return nil, "", false
	}
	parts := strings.Split(string(decoded), "\x00")
	if len(parts) != 6 || parts[0] != "network-policy-audit/v1" || parts[1] != tenantID || parts[2] != projectID || parts[3] != policyID || commonv1alpha1.ValidateIdentifier(parts[5], "/eventId") != nil {
		return nil, "", false
	}
	occurredAt, err := time.Parse(time.RFC3339Nano, parts[4])
	if err != nil {
		return nil, "", false
	}
	return &occurredAt, parts[5], true
}

func writeNetworkPolicyError(writer http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, postgres.ErrNetworkPolicyNotFound):
		writePublicProblem(writer, http.StatusNotFound, "not_found")
	case errors.Is(err, postgres.ErrMutationDenied):
		writePublicProblem(writer, http.StatusForbidden, "authorization_denied")
	case errors.Is(err, postgres.ErrNetworkPolicyIdempotencyConflict):
		writePublicProblem(writer, http.StatusConflict, "idempotency_conflict")
	case errors.Is(err, postgres.ErrNetworkPolicyResourceVersionConflict), errors.Is(err, postgres.ErrCoordinationRejected):
		writePublicProblem(writer, http.StatusConflict, "network_policy_resource_version_conflict")
	case errors.Is(err, postgres.ErrNetworkPolicyReferenced):
		writePublicProblem(writer, http.StatusConflict, "network_policy_referenced")
	case errors.Is(err, postgres.ErrCoordinationInvalidInput):
		writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
	default:
		writePublicProblem(writer, http.StatusInternalServerError, "internal_error")
	}
}
