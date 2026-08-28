package server

import (
	"encoding/json"
	"errors"
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

const (
	MembershipRoute  = "/v1/tenants/{tenantId}/memberships/{membershipId}"
	RoleBindingRoute = "/v1/tenants/{tenantId}/role-bindings/{roleBindingId}"
)

var ErrInvalidRBACHTTPServer = errors.New("RBAC HTTP server configuration is invalid")

type RBACHTTPServer struct {
	verifier AccessTokenVerifier
	reader   ManagedAgentRBACReader
}

func NewRBACHTTPServer(verifier AccessTokenVerifier, reader ManagedAgentRBACReader) (*RBACHTTPServer, error) {
	if verifier == nil || reader == nil {
		return nil, ErrInvalidRBACHTTPServer
	}
	return &RBACHTTPServer{verifier: verifier, reader: reader}, nil
}

func (server *RBACHTTPServer) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	writer.Header().Set("Cache-Control", "no-store")
	if server == nil || server.verifier == nil || server.reader == nil || request == nil {
		writeRBACError(writer, http.StatusInternalServerError, "internal_error")
		return
	}
	if request.Method != http.MethodGet {
		writer.Header().Set("Allow", http.MethodGet)
		writeRBACError(writer, http.StatusMethodNotAllowed, "method_not_allowed")
		return
	}
	tenantID, resourceID, kind, ok := rbacPath(request.URL.Path)
	if !ok {
		writeRBACError(writer, http.StatusNotFound, "route_not_found")
		return
	}
	requestID, ok := exactSingleHeader(request.Header, "X-Request-ID")
	if !ok {
		writeRBACError(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	validated, err := openapiv1.ValidateGetServerRequest(tenantID, resourceID, requestID)
	if err != nil {
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
	scope, err := server.resolveScope(request, validated.TenantID, validated.ResourceID, kind)
	if err != nil {
		if errors.Is(err, postgres.ErrMembershipNotFound) || errors.Is(err, postgres.ErrRoleBindingNotFound) {
			// Do not turn an unauthenticated scope probe into a resource oracle.
			writeRBACError(writer, http.StatusUnauthorized, "authentication_failed")
			return
		}
		writeRBACError(writer, http.StatusInternalServerError, "internal_error")
		return
	}
	principal, err := server.verifier.Verify(bearer, authn.VerificationRequest{
		TenantID: validated.TenantID, ResourceLevel: string(scope.Level), ResourceID: scope.ID, RequiredPermission: rbacPermission(kind),
	})
	if err != nil {
		writeRBACError(writer, http.StatusUnauthorized, "authentication_failed")
		return
	}
	if kind == "membership" {
		membership, getErr := GetMembership(request.Context(), server.reader, principal, ManagedAgentGetMembershipRequest{TenantID: validated.TenantID, MembershipID: validated.ResourceID, RequestID: validated.RequestID, Scope: scope})
		if getErr != nil {
			writeRBACErrorFromStore(writer, getErr, postgres.ErrMembershipNotFound)
			return
		}
		body, encodeErr := platformv1alpha1.EncodeMembershipResponseJSON(commonv1alpha1.ResponseEnvelope[platformv1alpha1.Membership]{Value: membershipResource(membership)})
		if encodeErr != nil {
			writeRBACError(writer, http.StatusInternalServerError, "internal_error")
			return
		}
		writeRBACResource(writer, validated.RequestID, membership.ResourceVersion, body)
		return
	}
	roleBinding, getErr := GetRoleBinding(request.Context(), server.reader, principal, ManagedAgentGetRoleBindingRequest{TenantID: validated.TenantID, RoleBindingID: validated.ResourceID, RequestID: validated.RequestID, Scope: scope})
	if getErr != nil {
		writeRBACErrorFromStore(writer, getErr, postgres.ErrRoleBindingNotFound)
		return
	}
	body, encodeErr := platformv1alpha1.EncodeRoleBindingResponseJSON(commonv1alpha1.ResponseEnvelope[platformv1alpha1.RoleBinding]{Value: roleBindingResource(roleBinding)})
	if encodeErr != nil {
		writeRBACError(writer, http.StatusInternalServerError, "internal_error")
		return
	}
	writeRBACResource(writer, validated.RequestID, roleBinding.ResourceVersion, body)
}

func (server *RBACHTTPServer) resolveScope(request *http.Request, tenantID, resourceID, kind string) (authz.ScopeRef, error) {
	if kind == "membership" {
		return server.reader.ResolveMembershipScope(request.Context(), tenantID, resourceID)
	}
	return server.reader.ResolveRoleBindingScope(request.Context(), tenantID, resourceID)
}

func rbacPath(path string) (string, string, string, bool) {
	const prefix = "/v1/tenants/"
	if !strings.HasPrefix(path, prefix) {
		return "", "", "", false
	}
	rest := strings.TrimPrefix(path, prefix)
	for suffix, kind := range map[string]string{"/memberships/": "membership", "/role-bindings/": "role_binding"} {
		parts := strings.Split(rest, suffix)
		if len(parts) == 2 && parts[0] != "" && parts[1] != "" && !strings.Contains(parts[0], "/") && !strings.Contains(parts[1], "/") {
			return parts[0], parts[1], kind, true
		}
	}
	return "", "", "", false
}

func rbacPermission(kind string) string {
	if kind == "membership" {
		return "memberships.get"
	}
	return "role-bindings.get"
}

func scopeResource(scope authz.ScopeRef, tenantID string) commonv1alpha1.AuthorizationScope {
	result := commonv1alpha1.AuthorizationScope{Level: string(scope.Level)}
	if scope.Level == authz.ScopePlatform {
		return result
	}
	var ref any
	switch scope.Level {
	case authz.ScopeTenant:
		ref = commonv1alpha1.TenantRef{Namespace: "cloud-agents", Kind: "tenant", ID: tenantID}
	case authz.ScopeOrganization:
		ref = commonv1alpha1.OrganizationRef{Namespace: "cloud-agents", Kind: "organization", ID: scope.ID}
	case authz.ScopeProject:
		ref = commonv1alpha1.ProjectRef{Namespace: "cloud-agents", Kind: "project", ID: scope.ID}
	default:
		return result
	}
	raw, _ := json.Marshal(ref)
	message := json.RawMessage(raw)
	result.Ref = &message
	return result
}

func subjectResource(subject authz.SubjectRef) commonv1alpha1.SubjectRef {
	return commonv1alpha1.SubjectRef{Kind: subject.Kind, Issuer: subject.Issuer, Subject: subject.Subject}
}

func membershipResource(value postgres.Membership) platformv1alpha1.Membership {
	tenant := commonv1alpha1.TenantRef{Namespace: "cloud-agents", Kind: "tenant", ID: value.TenantID}
	return platformv1alpha1.Membership{ResourceBase: platformv1alpha1.ResourceBase{APIVersion: projectAPIVersion, Kind: "Membership", Metadata: commonv1alpha1.ResourceMetadata{
		UID: value.UID, Name: value.Name, TenantRef: tenant, ResourceVersion: strconv.FormatInt(value.ResourceVersion, 10), CreatedAt: value.CreatedAt.UTC().Format(time.RFC3339Nano), UpdatedAt: value.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}}, Spec: platformv1alpha1.MembershipSpec{TenantRef: tenant, Subject: subjectResource(value.Subject), Scope: scopeResource(value.Scope, value.TenantID), State: value.State, ExpiresAt: optionalResourceTime(value.ExpiresAt)}}
}

func roleBindingResource(value postgres.RoleBinding) platformv1alpha1.RoleBinding {
	tenant := commonv1alpha1.TenantRef{Namespace: "cloud-agents", Kind: "tenant", ID: value.TenantID}
	return platformv1alpha1.RoleBinding{ResourceBase: platformv1alpha1.ResourceBase{APIVersion: projectAPIVersion, Kind: "RoleBinding", Metadata: commonv1alpha1.ResourceMetadata{
		UID: value.UID, Name: value.Name, TenantRef: tenant, ResourceVersion: strconv.FormatInt(value.ResourceVersion, 10), CreatedAt: value.CreatedAt.UTC().Format(time.RFC3339Nano), UpdatedAt: value.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}}, Spec: platformv1alpha1.RoleBindingSpec{TenantRef: tenant, Subject: subjectResource(value.Subject), RoleName: value.RoleName, RoleVersion: int(value.RoleVersion), Scope: scopeResource(value.Scope, value.TenantID), State: value.State, ExpiresAt: optionalResourceTime(value.ExpiresAt)}}
}

func optionalResourceTime(value *time.Time) string {
	if value == nil {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func writeRBACResource(writer http.ResponseWriter, requestID string, version int64, body []byte) {
	writer.Header().Set("X-Request-ID", requestID)
	writer.Header().Set("X-Resource-Version", strconv.FormatInt(version, 10))
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(http.StatusOK)
	_, _ = writer.Write(body)
}

func writeRBACErrorFromStore(writer http.ResponseWriter, err, notFound error) {
	switch {
	case errors.Is(err, notFound):
		writeRBACError(writer, http.StatusNotFound, "not_found")
	case errors.Is(err, postgres.ErrMutationDenied):
		writeRBACError(writer, http.StatusForbidden, "authorization_denied")
	case errors.Is(err, postgres.ErrCoordinationInvalidInput):
		writeRBACError(writer, http.StatusBadRequest, "invalid_request")
	default:
		writeRBACError(writer, http.StatusInternalServerError, "internal_error")
	}
}

func writeRBACError(writer http.ResponseWriter, status int, code string) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(projectErrorResponse{APIVersion: projectAPIVersion, Kind: "Error", Code: code})
}
