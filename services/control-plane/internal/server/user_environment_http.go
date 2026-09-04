package server

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"

	commonv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/common/v1alpha1"
	openapiv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/openapi/v1alpha1"
	platformv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/platform/v1alpha1"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authn"
	internalmanagedhost "github.com/hxp0618/cloud-agents/services/control-plane/internal/managedhost"
)

const UserEnvironmentRoutePrefix = "/v1/tenants/"

type userEnvironmentStore interface {
	CreateUserEnvironment(context.Context, string, *authn.VerifiedPrincipal, internalmanagedhost.CreateEnvironmentFromProfileInput) (internalmanagedhost.ProfileEnvironmentSnapshot, error)
	GetUserEnvironment(context.Context, string, *authn.VerifiedPrincipal, string, string) (internalmanagedhost.ProfileEnvironmentSnapshot, error)
}

type UserEnvironmentHTTPServer struct {
	verifier AccessTokenVerifier
	store    userEnvironmentStore
	actuator *ManagedHostEnvironmentLeaseHTTPServer
}

func NewUserEnvironmentHTTPServer(verifier AccessTokenVerifier, store userEnvironmentStore, actuator *ManagedHostEnvironmentLeaseHTTPServer) (*UserEnvironmentHTTPServer, error) {
	if verifier == nil || store == nil || actuator == nil {
		return nil, errors.New("user environment HTTP server configuration is invalid")
	}
	return &UserEnvironmentHTTPServer{verifier: verifier, store: store, actuator: actuator}, nil
}

func (server *UserEnvironmentHTTPServer) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	preparePublicRequestID(writer, request)
	if server == nil || server.verifier == nil || server.store == nil || server.actuator == nil || request == nil {
		writePublicProblem(writer, http.StatusInternalServerError, "internal_error")
		return
	}
	tenantID, projectID, environmentID, action, ok := userEnvironmentPath(request.URL.Path)
	if !ok {
		writePublicProblem(writer, http.StatusNotFound, "route_not_found")
		return
	}
	projectPermission, environmentPermission, allowed := userEnvironmentPermission(action, request.Method)
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
	principal, err := server.verifier.Verify(bearer, authn.VerificationRequest{TenantID: tenantID, ResourceLevel: "project", ResourceID: projectID, RequiredPermission: projectPermission})
	if err != nil {
		writePublicProblem(writer, http.StatusUnauthorized, "authentication_failed")
		return
	}
	if _, err := server.verifier.Verify(bearer, authn.VerificationRequest{TenantID: tenantID, ResourceLevel: "project", ResourceID: projectID, RequiredPermission: environmentPermission}); err != nil {
		writePublicProblem(writer, http.StatusForbidden, "authorization_denied")
		return
	}
	if action == "collection" {
		server.create(writer, request, tenantID, projectID, requestID, bearer, principal)
		return
	}
	server.get(writer, request, tenantID, projectID, environmentID, requestID, principal)
}

func (server *UserEnvironmentHTTPServer) create(writer http.ResponseWriter, request *http.Request, tenantID, projectID, requestID, bearer string, principal *authn.VerifiedPrincipal) {
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
	validated, err := openapiv1alpha1.ValidateCreateUserEnvironmentServerRequest(tenantID, projectID, requestID, idempotencyKey, body)
	if err != nil {
		writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	result, err := server.store.CreateUserEnvironment(request.Context(), tenantID, principal, internalmanagedhost.CreateEnvironmentFromProfileInput{
		Scope: internalmanagedhost.Scope{TenantID: tenantID, ProjectID: projectID}, ProfileID: validated.Body.ProfileID,
		ProfileVersion: validated.Body.ProfileVersion, Mutation: internalmanagedhost.Mutation{RequestID: requestID, IdempotencyKey: idempotencyKey},
	})
	if err != nil {
		writeUserEnvironmentError(writer, err)
		return
	}
	result.Lease, err = server.actuator.deployEnvironment(request.Context(), tenantID, projectID, bearer, result.Lease)
	if err != nil {
		if errors.Is(err, errEnvironmentActuationAuthentication) {
			writePublicProblem(writer, http.StatusUnauthorized, "authentication_failed")
			return
		}
		writeUserEnvironmentError(writer, err)
		return
	}
	writeUserEnvironment(writer, http.StatusCreated, requestID, result)
}

func (server *UserEnvironmentHTTPServer) get(writer http.ResponseWriter, request *http.Request, tenantID, projectID, environmentID, requestID string, principal *authn.VerifiedPrincipal) {
	if _, err := openapiv1alpha1.ValidateGetUserEnvironmentServerRequest(tenantID, projectID, environmentID, requestID); err != nil {
		writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	result, err := server.store.GetUserEnvironment(request.Context(), tenantID, principal, projectID, environmentID)
	if err != nil {
		writeUserEnvironmentError(writer, err)
		return
	}
	writeUserEnvironment(writer, http.StatusOK, requestID, result)
}

func writeUserEnvironment(writer http.ResponseWriter, status int, requestID string, snapshot internalmanagedhost.ProfileEnvironmentSnapshot) {
	value := platformv1alpha1.UserEnvironment{
		APIVersion: platformv1alpha1.APIVersion, Kind: "UserEnvironment",
		ProjectRef:    commonv1alpha1.ProjectRef{Namespace: "cloud-agents", Kind: "project", ID: snapshot.Lease.Scope.ProjectID},
		EnvironmentID: snapshot.Lease.LeaseID, ProfileID: snapshot.ProfileID, ProfileVersion: snapshot.ProfileVersion,
		ObservedPhase: snapshot.Lease.ObservedPhase, StableErrorCode: snapshot.Lease.StableErrorCode,
		ExpiresAt: snapshot.Lease.ExpiresAt.UTC().Format("2006-01-02T15:04:05.999999999Z07:00"),
	}
	body, err := platformv1alpha1.EncodeUserEnvironmentResponseJSON(commonv1alpha1.ResponseEnvelope[platformv1alpha1.UserEnvironment]{Value: value})
	if err != nil {
		writePublicProblem(writer, http.StatusInternalServerError, "internal_error")
		return
	}
	writer.Header().Set("X-Request-ID", requestID)
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_, _ = writer.Write(body)
}

func userEnvironmentPath(path string) (tenantID, projectID, environmentID, action string, ok bool) {
	if !strings.HasPrefix(path, UserEnvironmentRoutePrefix) {
		return "", "", "", "", false
	}
	parts := strings.Split(strings.TrimPrefix(path, UserEnvironmentRoutePrefix), "/")
	if len(parts) == 4 && parts[1] == "projects" && parts[3] == "environments" && parts[0] != "" && parts[2] != "" {
		return parts[0], parts[2], "", "collection", true
	}
	if len(parts) == 5 && parts[1] == "projects" && parts[3] == "environments" && parts[0] != "" && parts[2] != "" && parts[4] != "" {
		return parts[0], parts[2], parts[4], "get", true
	}
	return "", "", "", "", false
}

func userEnvironmentPermission(action, method string) (projectPermission, environmentPermission string, ok bool) {
	if action == "collection" && method == http.MethodPost {
		return "projects.act", "environments.create", true
	}
	if action == "get" && method == http.MethodGet {
		return "projects.get", "environments.get", true
	}
	return "", "", false
}

func HandlesUserEnvironmentPath(path string) bool {
	_, _, _, _, ok := userEnvironmentPath(path)
	return ok
}

func writeUserEnvironmentError(writer http.ResponseWriter, err error) {
	status, code := managedHostEnvironmentLeaseErrorStatus(err)
	writePublicProblem(writer, status, code)
}
