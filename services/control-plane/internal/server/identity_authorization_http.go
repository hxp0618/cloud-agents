package server

import (
	"context"
	"crypto/subtle"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"

	common "github.com/hxp0618/cloud-agents/sdk/go/gen/common/v1alpha1"
	api "github.com/hxp0618/cloud-agents/sdk/go/gen/openapi/v1alpha1"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/browserauth"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/store/postgres"
)

const (
	IdentityTenantTokenAuthorizationRoute = "/v1/identity/authorize-tenant-token"
	identityAuthorizationMaxBodyBytes     = 4 << 10
)

var ErrInvalidIdentityAuthorizationHTTPServer = errors.New("identity authorization HTTP server configuration is invalid")

// IdentityTenantTokenAuthorizer is the Control Plane authority consulted before
// the Identity Service signs one tenant-bound human-session token.
type IdentityTenantTokenAuthorizer interface {
	AuthorizeTenantToken(context.Context, api.TenantTokenAuthorizationRequest) (api.TenantTokenAuthorization, error)
}

// IdentityAuthorizationHTTPServer exposes only the authenticated Identity
// Service to Control Plane authorization operation.
type IdentityAuthorizationHTTPServer struct {
	authorizer       IdentityTenantTokenAuthorizer
	credentialDigest [32]byte
}

func NewIdentityAuthorizationHTTPServer(authorizer IdentityTenantTokenAuthorizer, serviceCredential string) (*IdentityAuthorizationHTTPServer, error) {
	if authorizer == nil {
		return nil, ErrInvalidIdentityAuthorizationHTTPServer
	}
	digest, err := browserauth.ProofDigest(serviceCredential)
	if err != nil {
		return nil, ErrInvalidIdentityAuthorizationHTTPServer
	}
	return &IdentityAuthorizationHTTPServer{authorizer: authorizer, credentialDigest: digest}, nil
}

func (server *IdentityAuthorizationHTTPServer) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	preparePublicRequestID(writer, request)
	writer.Header().Set("X-Content-Type-Options", "nosniff")
	if server == nil || server.authorizer == nil || request == nil {
		writePublicProblem(writer, http.StatusServiceUnavailable, "IDENTITY_AUTHORIZATION_UNAVAILABLE")
		return
	}
	if request.URL.Path != IdentityTenantTokenAuthorizationRoute {
		writePublicProblem(writer, http.StatusNotFound, "ROUTE_NOT_FOUND")
		return
	}
	if request.Method != http.MethodPost {
		writer.Header().Set("Allow", http.MethodPost)
		writePublicProblem(writer, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED")
		return
	}
	if request.URL.RawQuery != "" {
		writePublicProblem(writer, http.StatusBadRequest, "INVALID_REQUEST")
		return
	}
	if !server.authenticated(request) {
		writePublicProblem(writer, http.StatusUnauthorized, "AUTHENTICATION_FAILED")
		return
	}
	requestID, ok := exactSingleHeader(request.Header, "X-Request-ID")
	if !ok || common.ValidateIdentifier(requestID, "/X-Request-ID") != nil {
		writer.Header().Set("X-Request-ID", publicFallbackRequestID)
		writePublicProblem(writer, http.StatusBadRequest, "INVALID_REQUEST")
		return
	}
	writer.Header().Set("X-Request-ID", requestID)
	contentType, ok := exactSingleHeader(request.Header, "Content-Type")
	mediaType, _, mediaErr := mime.ParseMediaType(contentType)
	if !ok || mediaErr != nil || mediaType != "application/json" {
		writePublicProblem(writer, http.StatusUnsupportedMediaType, "UNSUPPORTED_MEDIA_TYPE")
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(writer, request.Body, identityAuthorizationMaxBodyBytes))
	if err != nil {
		writePublicProblem(writer, http.StatusBadRequest, "INVALID_REQUEST")
		return
	}
	input, err := api.DecodeTenantTokenAuthorizationRequestJSON(body)
	if err != nil {
		writePublicProblem(writer, http.StatusBadRequest, "INVALID_REQUEST")
		return
	}
	result, err := server.authorizer.AuthorizeTenantToken(request.Context(), input)
	if err != nil {
		if errors.Is(err, postgres.ErrTokenAuthorizationDenied) {
			writePublicProblem(writer, http.StatusForbidden, "AUTHORIZATION_DENIED")
			return
		}
		writePublicProblem(writer, http.StatusServiceUnavailable, "IDENTITY_AUTHORIZATION_UNAVAILABLE")
		return
	}
	if result.Application != input.Application || result.TenantID != input.TenantID || result.ProjectID != input.ProjectID {
		writePublicProblem(writer, http.StatusServiceUnavailable, "IDENTITY_AUTHORIZATION_UNAVAILABLE")
		return
	}
	encoded, err := api.EncodeTenantTokenAuthorizationJSON(result)
	if err != nil {
		writePublicProblem(writer, http.StatusServiceUnavailable, "IDENTITY_AUTHORIZATION_UNAVAILABLE")
		return
	}
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(http.StatusOK)
	_, _ = writer.Write(encoded)
}

func (server *IdentityAuthorizationHTTPServer) authenticated(request *http.Request) bool {
	authorization, ok := exactSingleHeader(request.Header, "Authorization")
	if !ok || !strings.HasPrefix(authorization, "Bearer ") {
		return false
	}
	provided, err := browserauth.ProofDigest(strings.TrimPrefix(authorization, "Bearer "))
	return err == nil && subtle.ConstantTimeCompare(provided[:], server.credentialDigest[:]) == 1
}
