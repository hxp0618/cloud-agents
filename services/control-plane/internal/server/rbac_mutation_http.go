package server

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	commonv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/common/v1alpha1"
	openapiv1 "github.com/hxp0618/cloud-agents/sdk/go/gen/openapi/v1alpha1"
	platformv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/platform/v1alpha1"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authn"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authz"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/store/postgres"
)

type ManagedAgentRBACMutator interface {
	CreateMembership(context.Context, string, *authn.VerifiedPrincipal, postgres.CreateMembershipInput) (postgres.MutationResult, error)
	SuspendMembership(context.Context, string, *authn.VerifiedPrincipal, postgres.MembershipTransitionInput) (postgres.MutationResult, error)
	RevokeMembership(context.Context, string, *authn.VerifiedPrincipal, postgres.MembershipTransitionInput) (postgres.MutationResult, error)
	BindRole(context.Context, string, *authn.VerifiedPrincipal, postgres.BindRoleInput) (postgres.MutationResult, error)
	RevokeRoleBinding(context.Context, string, *authn.VerifiedPrincipal, postgres.RevokeRoleBindingInput) (postgres.MutationResult, error)
}

func (server *RBACHTTPServer) mutate(writer http.ResponseWriter, request *http.Request) {
	tenantID, resourceID, kind, action, ok := rbacMutationPath(request.URL.Path)
	if !ok {
		writeRBACError(writer, http.StatusNotFound, "route_not_found")
		return
	}
	requestID, ok := exactSingleHeader(request.Header, "X-Request-ID")
	if !ok {
		writeRBACError(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	authorization, ok := exactSingleHeader(request.Header, "Authorization")
	if !ok {
		writeRBACError(writer, http.StatusUnauthorized, "authentication_failed")
		return
	}
	bearer, ok := bearerToken(authorization)
	if !ok {
		writeRBACError(writer, http.StatusUnauthorized, "authentication_failed")
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(writer, request.Body, 1<<20))
	if err != nil {
		writeRBACError(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	if kind == "membership" && action == "create" {
		server.createMembership(writer, request, tenantID, requestID, bearer, body)
		return
	}
	if kind == "role_binding" && action == "create" {
		server.bindRole(writer, request, tenantID, requestID, bearer, body)
		return
	}
	server.transition(writer, request, tenantID, resourceID, kind, action, requestID, bearer, body)
}

func (server *RBACHTTPServer) createMembership(writer http.ResponseWriter, request *http.Request, tenantID, requestID, bearer string, body []byte) {
	validated, err := openapiv1.ValidateCreateMembershipServerRequest(tenantID, requestID, body)
	if err != nil {
		writeRBACError(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	scope, err := mutationScope(tenantID, validated.Body.Scope)
	if err != nil {
		writeRBACError(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	principal, err := server.verifyMutation(bearer, tenantID, scope, "memberships.create")
	if err != nil {
		writeRBACError(writer, http.StatusUnauthorized, "authentication_failed")
		return
	}
	result, err := server.mutator.CreateMembership(request.Context(), tenantID, principal, postgres.CreateMembershipInput{
		ExpectedTenantRevision: validated.Body.ExpectedTenantRevision, MembershipUID: validated.Body.MembershipID, MembershipName: validated.Body.MembershipName,
		Subject: authz.SubjectRef{Kind: validated.Body.Subject.Kind, Issuer: validated.Body.Subject.Issuer, Subject: validated.Body.Subject.Subject}, Scope: scope,
		ExpiresAt: parseMutationTime(validated.Body.ExpiresAt), AuditFactUID: validated.Body.AuditFactUID, ReasonCode: validated.Body.ReasonCode,
	})
	server.writeMutationResult(writer, requestID, http.StatusCreated, result, err)
}

func (server *RBACHTTPServer) bindRole(writer http.ResponseWriter, request *http.Request, tenantID, requestID, bearer string, body []byte) {
	validated, err := openapiv1.ValidateBindRoleServerRequest(tenantID, requestID, body)
	if err != nil {
		writeRBACError(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	scope, err := mutationScope(tenantID, validated.Body.Scope)
	if err != nil {
		writeRBACError(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	principal, err := server.verifyMutation(bearer, tenantID, scope, "role-bindings.bind")
	if err != nil {
		writeRBACError(writer, http.StatusUnauthorized, "authentication_failed")
		return
	}
	result, err := server.mutator.BindRole(request.Context(), tenantID, principal, postgres.BindRoleInput{
		ExpectedTenantRevision: validated.Body.ExpectedTenantRevision, RoleBindingUID: validated.Body.RoleBindingID, RoleBindingName: validated.Body.RoleBindingName,
		Subject: authz.SubjectRef{Kind: validated.Body.Subject.Kind, Issuer: validated.Body.Subject.Issuer, Subject: validated.Body.Subject.Subject}, RoleName: validated.Body.RoleName,
		RoleVersion: validated.Body.RoleVersion, Scope: scope, ExpiresAt: parseMutationTime(validated.Body.ExpiresAt), AuditFactUID: validated.Body.AuditFactUID, ReasonCode: validated.Body.ReasonCode,
	})
	server.writeMutationResult(writer, requestID, http.StatusCreated, result, err)
}

func (server *RBACHTTPServer) transition(writer http.ResponseWriter, request *http.Request, tenantID, resourceID, kind, action, requestID, bearer string, body []byte) {
	var validated openapiv1.MembershipTransitionServerInput
	var err error
	if kind == "membership" {
		validated, err = openapiv1.ValidateMembershipTransitionServerRequest(tenantID, resourceID, requestID, body)
	} else {
		var revoke openapiv1.RoleBindingRevokeServerInput
		revoke, err = openapiv1.ValidateRoleBindingRevokeServerRequest(tenantID, resourceID, requestID, body)
		if err == nil {
			validated = openapiv1.MembershipTransitionServerInput{TenantID: revoke.TenantID, ResourceID: revoke.ResourceID, RequestID: revoke.RequestID, Body: platformv1alpha1.MembershipTransitionRequest{ExpectedTenantRevision: revoke.Body.ExpectedTenantRevision, ExpectedResourceVersion: revoke.Body.ExpectedResourceVersion, AuditFactUID: revoke.Body.AuditFactUID, ReasonCode: revoke.Body.ReasonCode}}
		}
	}
	if err != nil {
		writeRBACError(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	scope, err := server.resolveScope(request, tenantID, resourceID, kind)
	if err != nil {
		if errors.Is(err, postgres.ErrMembershipNotFound) || errors.Is(err, postgres.ErrRoleBindingNotFound) || errors.Is(err, postgres.ErrMutationTargetNotFound) {
			writeRBACError(writer, http.StatusUnauthorized, "authentication_failed")
			return
		}
		writeRBACError(writer, http.StatusInternalServerError, "internal_error")
		return
	}
	permission := "memberships.update"
	if kind == "membership" && action == "revoke" {
		permission = "memberships.delete"
	}
	if kind == "role_binding" {
		permission = "role-bindings.delete"
	}
	principal, err := server.verifyMutation(bearer, tenantID, scope, permission)
	if err != nil {
		writeRBACError(writer, http.StatusUnauthorized, "authentication_failed")
		return
	}
	var result postgres.MutationResult
	switch {
	case kind == "membership" && action == "suspend":
		result, err = server.mutator.SuspendMembership(request.Context(), tenantID, principal, postgres.MembershipTransitionInput{ExpectedTenantRevision: validated.Body.ExpectedTenantRevision, MembershipUID: resourceID, ExpectedResourceVersion: validated.Body.ExpectedResourceVersion, AuditFactUID: validated.Body.AuditFactUID, ReasonCode: validated.Body.ReasonCode})
	case kind == "membership" && action == "revoke":
		result, err = server.mutator.RevokeMembership(request.Context(), tenantID, principal, postgres.MembershipTransitionInput{ExpectedTenantRevision: validated.Body.ExpectedTenantRevision, MembershipUID: resourceID, ExpectedResourceVersion: validated.Body.ExpectedResourceVersion, AuditFactUID: validated.Body.AuditFactUID, ReasonCode: validated.Body.ReasonCode})
	case kind == "role_binding" && action == "revoke":
		result, err = server.mutator.RevokeRoleBinding(request.Context(), tenantID, principal, postgres.RevokeRoleBindingInput{ExpectedTenantRevision: validated.Body.ExpectedTenantRevision, RoleBindingUID: resourceID, ExpectedResourceVersion: validated.Body.ExpectedResourceVersion, AuditFactUID: validated.Body.AuditFactUID, ReasonCode: validated.Body.ReasonCode})
	default:
		writeRBACError(writer, http.StatusNotFound, "route_not_found")
		return
	}
	server.writeMutationResult(writer, requestID, http.StatusOK, result, err)
}

func (server *RBACHTTPServer) verifyMutation(bearer, tenantID string, scope authz.ScopeRef, permission string) (*authn.VerifiedPrincipal, error) {
	return server.verifier.Verify(bearer, authn.VerificationRequest{TenantID: tenantID, ResourceLevel: string(scope.Level), ResourceID: scope.ID, RequiredPermission: permission})
}

func (server *RBACHTTPServer) writeMutationResult(writer http.ResponseWriter, requestID string, status int, result postgres.MutationResult, err error) {
	if err != nil {
		writeRBACErrorFromMutation(writer, err)
		return
	}
	body, encodeErr := platformv1alpha1.EncodeRBACMutationResultResponseJSON(commonv1alpha1.ResponseEnvelope[platformv1alpha1.RBACMutationResult]{Value: platformv1alpha1.RBACMutationResult{ResourceUID: result.ResourceUID, ResourceVersion: strconv.FormatInt(result.ResourceVersion, 10), State: result.State}})
	if encodeErr != nil {
		writeRBACError(writer, http.StatusInternalServerError, "internal_error")
		return
	}
	writer.Header().Set("X-Request-ID", requestID)
	writer.Header().Set("X-Resource-Version", strconv.FormatInt(result.ResourceVersion, 10))
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_, _ = writer.Write(body)
}

func mutationScope(tenantID string, value commonv1alpha1.AuthorizationScope) (authz.ScopeRef, error) {
	if value.Level == string(authz.ScopePlatform) || value.Ref == nil {
		return authz.ScopeRef{}, errors.New("platform scope is not a tenant mutation scope")
	}
	var id string
	var err error
	switch value.Level {
	case string(authz.ScopeTenant):
		var ref commonv1alpha1.TenantRef
		ref, err = commonv1alpha1.DecodeTenantRefJSON(*value.Ref)
		id = ref.ID
	case string(authz.ScopeOrganization):
		var ref commonv1alpha1.OrganizationRef
		ref, err = commonv1alpha1.DecodeOrganizationRefJSON(*value.Ref)
		id = ref.ID
	case string(authz.ScopeProject):
		var ref commonv1alpha1.ProjectRef
		ref, err = commonv1alpha1.DecodeProjectRefJSON(*value.Ref)
		id = ref.ID
	default:
		return authz.ScopeRef{}, errors.New("unknown mutation scope")
	}
	scope := authz.ScopeRef{Level: authz.ScopeLevel(value.Level), ID: id}
	if err != nil || scope.Validate(tenantID) != nil {
		return authz.ScopeRef{}, errors.New("invalid mutation scope")
	}
	return scope, nil
}

func parseMutationTime(value string) *time.Time {
	if value == "" {
		return nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return nil
	}
	return &parsed
}

func rbacMutationPath(path string) (tenantID, resourceID, kind, action string, ok bool) {
	prefix := "/v1/tenants/"
	if !strings.HasPrefix(path, prefix) {
		return "", "", "", "", false
	}
	parts := strings.Split(strings.TrimPrefix(path, prefix), "/")
	if len(parts) == 2 && parts[1] == "memberships" {
		return parts[0], "", "membership", "create", parts[0] != ""
	}
	if len(parts) == 2 && parts[1] == "role-bindings" {
		return parts[0], "", "role_binding", "create", parts[0] != ""
	}
	if len(parts) != 3 || parts[0] == "" || parts[2] == "" {
		return "", "", "", "", false
	}
	for _, candidate := range []struct{ suffix, action, kind string }{{":suspend", "suspend", "membership"}, {":revoke", "revoke", "membership"}, {":revoke", "revoke", "role_binding"}} {
		if strings.HasSuffix(parts[2], candidate.suffix) && strings.TrimSuffix(parts[2], candidate.suffix) != "" {
			if (candidate.kind == "membership" && parts[1] == "memberships") || (candidate.kind == "role_binding" && parts[1] == "role-bindings") {
				return parts[0], strings.TrimSuffix(parts[2], candidate.suffix), candidate.kind, candidate.action, true
			}
		}
	}
	return "", "", "", "", false
}

func HandlesRBACPath(path string) bool {
	if _, _, _, ok := rbacPath(path); ok {
		return true
	}
	_, _, _, _, ok := rbacMutationPath(path)
	return ok
}

func writeRBACErrorFromMutation(writer http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, postgres.ErrMutationDenied):
		writeRBACError(writer, http.StatusForbidden, "authorization_denied")
	case errors.Is(err, postgres.ErrMutationTargetNotFound):
		writeRBACError(writer, http.StatusNotFound, "not_found")
	case errors.Is(err, postgres.ErrMutationInvalidInput):
		writeRBACError(writer, http.StatusBadRequest, "invalid_request")
	case errors.Is(err, postgres.ErrMutationConflict):
		writeRBACError(writer, http.StatusConflict, "mutation_conflict")
	case errors.Is(err, postgres.ErrMutationCommitUnknown):
		writeRBACError(writer, http.StatusInternalServerError, "commit_outcome_unknown")
	default:
		writeRBACError(writer, http.StatusInternalServerError, "internal_error")
	}
}
