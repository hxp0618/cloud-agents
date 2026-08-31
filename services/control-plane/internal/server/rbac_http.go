package server

import (
	"encoding/base64"
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
	MembershipCollectionRoute   = "/v1/tenants/{tenantId}/memberships"
	MembershipRoute             = "/v1/tenants/{tenantId}/memberships/{membershipId}"
	RoleBindingCollectionRoute  = "/v1/tenants/{tenantId}/role-bindings"
	RoleBindingRoute            = "/v1/tenants/{tenantId}/role-bindings/{roleBindingId}"
	ManagedHostRoleBindingRoute = "/v1/managed-host/tenants/{tenantId}/role-bindings/{roleBindingId}"
	managedHostTenantPrefix     = "/v1/managed-host/tenants/"
)

var ErrInvalidRBACHTTPServer = errors.New("RBAC HTTP server configuration is invalid")

type RBACHTTPServer struct {
	verifier AccessTokenVerifier
	reader   ManagedAgentRBACReader
	mutator  ManagedAgentRBACMutator
}

func NewRBACHTTPServer(verifier AccessTokenVerifier, reader ManagedAgentRBACReader, mutator ManagedAgentRBACMutator) (*RBACHTTPServer, error) {
	if verifier == nil || reader == nil || mutator == nil {
		return nil, ErrInvalidRBACHTTPServer
	}
	return &RBACHTTPServer{verifier: verifier, reader: reader, mutator: mutator}, nil
}

func (server *RBACHTTPServer) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	preparePublicRequestID(writer, request)
	if server == nil || server.verifier == nil || server.reader == nil || server.mutator == nil || request == nil {
		writeRBACError(writer, http.StatusInternalServerError, "internal_error")
		return
	}
	if request.Method != http.MethodGet {
		if request.Method == http.MethodPost && server.mutator != nil {
			server.mutate(writer, request)
			return
		}
		writer.Header().Set("Allow", http.MethodGet+", "+http.MethodPost)
		writeRBACError(writer, http.StatusMethodNotAllowed, "method_not_allowed")
		return
	}
	tenantID, resourceID, kind, ok := rbacPath(request.URL.Path)
	if !ok {
		writeRBACError(writer, http.StatusNotFound, "route_not_found")
		return
	}
	if kind == "membership" && resourceID == "" {
		server.listMemberships(writer, request, tenantID)
		return
	}
	if kind == "role_binding" && resourceID == "" {
		server.listRoleBindings(writer, request, tenantID)
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

func (server *RBACHTTPServer) listMemberships(writer http.ResponseWriter, request *http.Request, tenantID string) {
	requestID, ok := exactSingleHeader(request.Header, "X-Request-ID")
	if !ok {
		writeRBACError(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	pageSize, pageToken, ok := managedAgentPagination(request)
	if !ok {
		writeRBACError(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	validated, err := openapiv1.ValidateListMembershipsServerRequest(tenantID, requestID, pageSize, pageToken)
	if err != nil {
		writeRBACError(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	afterMembershipID := ""
	if validated.PageToken != "" {
		if afterMembershipID, ok = decodeMembershipPageToken(validated.TenantID, validated.PageToken); !ok {
			writeRBACError(writer, http.StatusBadRequest, "invalid_request")
			return
		}
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
	principal, err := server.verifier.Verify(bearer, authn.VerificationRequest{
		TenantID: validated.TenantID, ResourceLevel: "tenant", ResourceID: validated.TenantID, RequiredPermission: "memberships.list",
	})
	if err != nil {
		writeRBACError(writer, http.StatusUnauthorized, "authentication_failed")
		return
	}
	page, err := server.reader.ListMemberships(request.Context(), validated.TenantID, principal, afterMembershipID, validated.PageSize)
	if err != nil {
		writeRBACErrorFromStore(writer, err, postgres.ErrMembershipNotFound)
		return
	}
	memberships := make([]platformv1alpha1.Membership, 0, len(page.Memberships))
	for _, membership := range page.Memberships {
		memberships = append(memberships, membershipResource(membership))
	}
	value := platformv1alpha1.MembershipPage{APIVersion: platformv1alpha1.APIVersion, Kind: "MembershipPage", Memberships: memberships}
	if page.NextMembershipID != "" {
		value.NextPageToken, ok = encodeMembershipPageToken(validated.TenantID, page.NextMembershipID)
		if !ok {
			writeRBACError(writer, http.StatusInternalServerError, "internal_error")
			return
		}
	}
	body, err := platformv1alpha1.EncodeMembershipPageResponseJSON(commonv1alpha1.ResponseEnvelope[platformv1alpha1.MembershipPage]{Value: value})
	if err != nil {
		writeRBACError(writer, http.StatusInternalServerError, "internal_error")
		return
	}
	writer.Header().Set("X-Request-ID", validated.RequestID)
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(http.StatusOK)
	_, _ = writer.Write(body)
}

func (server *RBACHTTPServer) listRoleBindings(writer http.ResponseWriter, request *http.Request, tenantID string) {
	requestID, ok := exactSingleHeader(request.Header, "X-Request-ID")
	if !ok {
		writeRBACError(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	pageSize, pageToken, ok := managedAgentPagination(request)
	if !ok {
		writeRBACError(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	validated, err := openapiv1.ValidateListRoleBindingsServerRequest(tenantID, requestID, pageSize, pageToken)
	if err != nil {
		writeRBACError(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	afterRoleBindingID := ""
	if validated.PageToken != "" {
		if afterRoleBindingID, ok = decodeRoleBindingPageToken(validated.TenantID, validated.PageToken); !ok {
			writeRBACError(writer, http.StatusBadRequest, "invalid_request")
			return
		}
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
	principal, err := server.verifier.Verify(bearer, authn.VerificationRequest{
		TenantID: validated.TenantID, ResourceLevel: "tenant", ResourceID: validated.TenantID, RequiredPermission: "role-bindings.list",
	})
	if err != nil {
		writeRBACError(writer, http.StatusUnauthorized, "authentication_failed")
		return
	}
	page, err := server.reader.ListRoleBindings(request.Context(), validated.TenantID, principal, afterRoleBindingID, validated.PageSize)
	if err != nil {
		writeRBACErrorFromStore(writer, err, postgres.ErrRoleBindingNotFound)
		return
	}
	roleBindings := make([]platformv1alpha1.RoleBinding, 0, len(page.RoleBindings))
	for _, roleBinding := range page.RoleBindings {
		roleBindings = append(roleBindings, roleBindingResource(roleBinding))
	}
	value := platformv1alpha1.RoleBindingPage{APIVersion: platformv1alpha1.APIVersion, Kind: "RoleBindingPage", RoleBindings: roleBindings}
	if page.NextRoleBindingID != "" {
		value.NextPageToken, ok = encodeRoleBindingPageToken(validated.TenantID, page.NextRoleBindingID)
		if !ok {
			writeRBACError(writer, http.StatusInternalServerError, "internal_error")
			return
		}
	}
	body, err := platformv1alpha1.EncodeRoleBindingPageResponseJSON(commonv1alpha1.ResponseEnvelope[platformv1alpha1.RoleBindingPage]{Value: value})
	if err != nil {
		writeRBACError(writer, http.StatusInternalServerError, "internal_error")
		return
	}
	writer.Header().Set("X-Request-ID", validated.RequestID)
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(http.StatusOK)
	_, _ = writer.Write(body)
}

func (server *RBACHTTPServer) resolveScope(request *http.Request, tenantID, resourceID, kind string) (authz.ScopeRef, error) {
	if kind == "membership" {
		return server.reader.ResolveMembershipScope(request.Context(), tenantID, resourceID)
	}
	return server.reader.ResolveRoleBindingScope(request.Context(), tenantID, resourceID)
}

func rbacPath(path string) (string, string, string, bool) {
	prefix := "/v1/tenants/"
	if strings.HasPrefix(path, managedHostTenantPrefix) {
		prefix = managedHostTenantPrefix
	}
	if !strings.HasPrefix(path, prefix) {
		return "", "", "", false
	}
	rest := strings.TrimPrefix(path, prefix)
	if prefix == "/v1/tenants/" && strings.HasSuffix(rest, "/memberships") {
		tenantID := strings.TrimSuffix(rest, "/memberships")
		return tenantID, "", "membership", tenantID != "" && !strings.Contains(tenantID, "/")
	}
	if prefix == "/v1/tenants/" && strings.HasSuffix(rest, "/role-bindings") {
		tenantID := strings.TrimSuffix(rest, "/role-bindings")
		return tenantID, "", "role_binding", tenantID != "" && !strings.Contains(tenantID, "/")
	}
	for suffix, kind := range map[string]string{"/memberships/": "membership", "/role-bindings/": "role_binding"} {
		parts := strings.Split(rest, suffix)
		if len(parts) == 2 && parts[0] != "" && parts[1] != "" && !strings.Contains(parts[0], "/") && !strings.Contains(parts[1], "/") {
			return parts[0], parts[1], kind, true
		}
	}
	return "", "", "", false
}

func encodeMembershipPageToken(tenantID, membershipID string) (string, bool) {
	if commonv1alpha1.ValidateIdentifier(tenantID, "/tenantId") != nil || commonv1alpha1.ValidateIdentifier(membershipID, "/membershipId") != nil {
		return "", false
	}
	token := base64.RawURLEncoding.EncodeToString([]byte("membership/v1\x00" + tenantID + "\x00" + membershipID))
	return token, commonv1alpha1.ValidatePageToken(token, "/pageToken") == nil
}

func decodeMembershipPageToken(tenantID, token string) (string, bool) {
	if commonv1alpha1.ValidatePageToken(token, "/pageToken") != nil {
		return "", false
	}
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(token)
	if err != nil {
		return "", false
	}
	parts := strings.Split(string(decoded), "\x00")
	if len(parts) != 3 || parts[0] != "membership/v1" || parts[1] != tenantID ||
		commonv1alpha1.ValidateIdentifier(parts[1], "/tenantId") != nil || commonv1alpha1.ValidateIdentifier(parts[2], "/membershipId") != nil {
		return "", false
	}
	return parts[2], true
}

func encodeRoleBindingPageToken(tenantID, roleBindingID string) (string, bool) {
	if commonv1alpha1.ValidateIdentifier(tenantID, "/tenantId") != nil || commonv1alpha1.ValidateIdentifier(roleBindingID, "/roleBindingId") != nil {
		return "", false
	}
	token := base64.RawURLEncoding.EncodeToString([]byte("role-binding/v1\x00" + tenantID + "\x00" + roleBindingID))
	return token, commonv1alpha1.ValidatePageToken(token, "/pageToken") == nil
}

func decodeRoleBindingPageToken(tenantID, token string) (string, bool) {
	if commonv1alpha1.ValidatePageToken(token, "/pageToken") != nil {
		return "", false
	}
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(token)
	if err != nil {
		return "", false
	}
	parts := strings.Split(string(decoded), "\x00")
	if len(parts) != 3 || parts[0] != "role-binding/v1" || parts[1] != tenantID ||
		commonv1alpha1.ValidateIdentifier(parts[1], "/tenantId") != nil || commonv1alpha1.ValidateIdentifier(parts[2], "/roleBindingId") != nil {
		return "", false
	}
	return parts[2], true
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
	writePublicProblem(writer, status, code)
}
