package identity

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/netip"
	"strings"

	common "github.com/hxp0618/cloud-agents/sdk/go/gen/common/v1alpha1"
	api "github.com/hxp0618/cloud-agents/sdk/go/gen/openapi/v1alpha1"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/browserauth"
)

var (
	ErrForbidden   = errors.New("identity operation forbidden")
	ErrRateLimited = errors.New("identity login rate limited")
)

// Store operations recheck durable user/session state and apply their writes atomically.
type Store interface {
	JWKS(context.Context) (api.IdentityJWKS, error)
	PasswordLogin(context.Context, api.IdentityApplication, netip.Addr, api.PasswordLoginRequest) (api.IdentityLoginResult, error)
	Session(context.Context, api.IdentityApplication, [32]byte) (api.BrowserSession, error)
	Tenants(context.Context, api.IdentityApplication, [32]byte, int, string) (api.BrowserTenantPage, error)
	GetEmailSuffixPolicy(context.Context, [32]byte, string) (api.EmailSuffixPolicy, error)
	UpdateEmailSuffixPolicy(context.Context, [32]byte, string, api.EmailSuffixPolicyUpdate) (api.EmailSuffixPolicy, error)
	CreateInvitation(context.Context, [32]byte, string, api.InvitationCreateRequest) (api.InvitationCreated, error)
	ListInvitations(context.Context, [32]byte, string, int, string) (api.InvitationPage, error)
	RevokeInvitation(context.Context, [32]byte, string, string) error
	AcceptInvitation(context.Context, api.IdentityApplication, *[32]byte, netip.Addr, api.InvitationAcceptRequest) error
	ListAccounts(context.Context, [32]byte, int, string) (api.IdentityAccountPage, error)
	ListTenantAccounts(context.Context, [32]byte, string, int, string) (api.IdentityAccountPage, error)
	DisableAccount(context.Context, [32]byte, string) error
	IssuePasswordReset(context.Context, [32]byte, string) (api.PasswordResetCreated, error)
	ChangePassword(context.Context, api.IdentityApplication, [32]byte, api.PasswordChangeRequest) error
	AcceptPasswordReset(context.Context, netip.Addr, api.PasswordResetAcceptRequest) error
	ListAuditEvents(context.Context, [32]byte, string, int, string) (api.IdentityAuditPage, error)
	ListControlPlaneAuditEvents(context.Context, [32]byte, string, int, string) (api.ControlPlaneAuditPage, error)
	Logout(context.Context, api.IdentityApplication, [32]byte, string) error
	IssueTenantToken(context.Context, api.IdentityApplication, [32]byte, api.TenantTokenIssueRequest) (api.TenantToken, error)
	TokenStatus(context.Context, api.TokenStatusRequest) (api.TokenStatus, error)
}

// These values are loaded from secret references, never from request fields.
type ServiceCredentials struct{ AdminWeb, UserWeb, ControlPlane string }

type correlationContextKey struct{}

func requestCorrelationID(ctx context.Context) (string, error) {
	if value, ok := ctx.Value(correlationContextKey{}).(string); ok {
		return value, nil
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", errors.New("identity request ID unavailable")
	}
	return "identity-" + hex.EncodeToString(random[:]), nil
}

type Server struct {
	store      Store
	providers  providerHTTPStore
	cli        cliHTTPStore
	automation automationHTTPStore
	services   map[[32]byte]api.IdentityApplication
}

func NewServer(store Store, credentials ServiceCredentials) (*Server, error) {
	if store == nil {
		return nil, errors.New("identity store is required")
	}
	services := make(map[[32]byte]api.IdentityApplication, 3)
	for _, entry := range []struct {
		credential  string
		application api.IdentityApplication
	}{
		{credentials.AdminWeb, api.IdentityApplicationAdmin}, {credentials.UserWeb, api.IdentityApplicationUser}, {credentials.ControlPlane, ""},
	} {
		digest, err := browserauth.ProofDigest(entry.credential)
		if err != nil {
			return nil, errors.New("identity service credential is invalid")
		}
		if _, exists := services[digest]; exists {
			return nil, errors.New("identity service credentials must be distinct")
		}
		services[digest] = entry.application
	}
	server := &Server{store: store, services: services}
	if providers, ok := store.(providerHTTPStore); ok {
		server.providers = providers
	}
	if cli, ok := store.(cliHTTPStore); ok {
		server.cli = cli
	}
	if automation, ok := store.(automationHTTPStore); ok {
		server.automation = automation
	}
	return server, nil
}

func (server *Server) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	writer.Header().Set("Cache-Control", "no-store")
	writer.Header().Set("Pragma", "no-cache")
	writer.Header().Set("Referrer-Policy", "no-referrer")
	writer.Header().Set("X-Content-Type-Options", "nosniff")
	var requestRandom [16]byte
	if _, err := rand.Read(requestRandom[:]); err != nil {
		writer.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	requestID := "identity-" + hex.EncodeToString(requestRandom[:])
	writer.Header().Set("X-Request-ID", requestID)
	if request.URL.RawPath != "" || request.URL.Opaque != "" {
		writeProblem(writer, requestID, http.StatusNotFound)
		return
	}
	if request.Method == http.MethodGet && request.URL.Path == "/.well-known/jwks.json" && request.URL.RawQuery == "" {
		document, err := server.store.JWKS(request.Context())
		var body []byte
		if err == nil {
			body, err = api.EncodeIdentityJWKSJSON(document)
		}
		if err != nil || len(body) > 1<<20 {
			writeProblem(writer, requestID, http.StatusServiceUnavailable)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write(body)
		return
	}
	application, authenticated := server.authenticate(request)
	if !authenticated {
		writeProblem(writer, requestID, http.StatusUnauthorized)
		return
	}
	serviceRequestID := request.Header.Get("X-Request-ID")
	if len(request.Header.Values("X-Request-ID")) != 1 || common.ValidateIdentifier(serviceRequestID, "/X-Request-ID") != nil {
		writeProblem(writer, requestID, http.StatusBadRequest)
		return
	}
	requestID = serviceRequestID
	writer.Header().Set("X-Request-ID", requestID)
	invitationTenantID, invitationID, invitationRoute := invitationPath(request.URL.Path)
	securityRoute, accountSecurityRoute := matchAccountSecurityRoute(request.URL.Path)
	providerRoute, providerMatched := matchProviderRoute(request.URL.Path)
	providerHandled := providerMatched && !(providerRoute.kind == "enable-password" && request.Method == http.MethodPut)
	cliRoute, cliMatched := matchCLIRoute(request.URL.Path)
	automationRoute := request.URL.Path == automationTenantTokenRoute
	securityPageRoute := accountSecurityRoute && request.Method == http.MethodGet && (securityRoute.kind == "accounts" || securityRoute.kind == "audit" || securityRoute.kind == "control-plane-audit")
	if request.URL.RawQuery != "" && request.URL.Path != "/v1/identity/me/tenants" && !(invitationRoute && invitationID == "" && request.Method == http.MethodGet) && !securityPageRoute && !(cliMatched && cliRoute.kind == "tenants" && request.Method == http.MethodGet) {
		writeProblem(writer, requestID, http.StatusBadRequest)
		return
	}
	statusRoute := request.Method == http.MethodPost && request.URL.Path == "/v1/identity/token-status"
	if (application == "") != statusRoute {
		writeProblem(writer, requestID, http.StatusForbidden)
		return
	}
	policyTenantID, policyRoute := emailPolicyTenantID(request.URL.Path)
	if policyRoute && common.ValidateIdentifier(policyTenantID, "/tenantId") != nil {
		writeProblem(writer, requestID, http.StatusBadRequest)
		return
	}
	if policyRoute && application != api.IdentityApplicationAdmin {
		writeProblem(writer, requestID, http.StatusForbidden)
		return
	}
	var body []byte
	if request.Method == http.MethodPost || request.Method == http.MethodPut {
		mediaType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
		if err != nil || mediaType != "application/json" || len(request.Header.Values("Content-Type")) != 1 {
			writeProblem(writer, requestID, http.StatusUnsupportedMediaType)
			return
		}
		maximumBodyBytes := int64(8192)
		if request.Method == http.MethodPut && (policyRoute || providerRoute.kind == "provider-client") {
			maximumBodyBytes = 32 << 10
		}
		body, err = io.ReadAll(http.MaxBytesReader(writer, request.Body, maximumBodyBytes))
		if err != nil {
			writeProblem(writer, requestID, http.StatusBadRequest)
			return
		}
	}
	ctx := context.WithValue(request.Context(), correlationContextKey{}, requestID)
	var encoded []byte
	var operationErr error
	var sessionHandle string
	responseHeaders := map[string]string{}
	responseStatus := http.StatusOK
	switch {
	case automationRoute:
		encoded, responseStatus, operationErr = server.automationOperation(ctx, request, body, application)
	case cliMatched:
		encoded, responseStatus, operationErr = server.cliOperation(ctx, request, body, application, cliRoute)
	case providerHandled:
		encoded, responseStatus, responseHeaders, operationErr = server.providerOperation(ctx, request, body, application, providerRoute)
	case accountSecurityRoute:
		encoded, responseStatus, operationErr = server.accountSecurityOperation(ctx, request, body, application, securityRoute)
	case invitationRoute || request.URL.Path == "/v1/identity/invitations/accept":
		encoded, responseStatus, operationErr = server.invitationOperation(ctx, request, body, application, invitationTenantID, invitationID)
	case statusRoute:
		input, err := api.DecodeTokenStatusRequestJSON(body)
		if err != nil {
			writeProblem(writer, requestID, http.StatusBadRequest)
			return
		}
		var result api.TokenStatus
		result, operationErr = server.store.TokenStatus(ctx, input)
		if operationErr == nil {
			encoded, operationErr = api.EncodeTokenStatusJSON(result)
		}
	case request.Method == http.MethodPost && request.URL.Path == "/v1/identity/login/password":
		input, err := api.DecodePasswordLoginRequestJSON(body)
		clientIP, ipErr := netip.ParseAddr(request.Header.Get("X-Cloud-Agents-Client-IP"))
		if err != nil || ipErr != nil || clientIP.Zone() != "" || clientIP.String() != request.Header.Get("X-Cloud-Agents-Client-IP") || len(request.Header.Values("X-Cloud-Agents-Client-IP")) != 1 {
			writeProblem(writer, requestID, http.StatusBadRequest)
			return
		}
		var result api.IdentityLoginResult
		result, operationErr = server.store.PasswordLogin(ctx, application, clientIP.Unmap(), input)
		if operationErr == nil {
			if _, err := browserauth.ProofDigest(result.SessionHandle); err != nil || result.Session.Application != application || result.Session.CSRFToken == result.SessionHandle {
				operationErr = errors.New("invalid identity session result")
			} else {
				encoded, operationErr = api.EncodeBrowserSessionJSON(result.Session)
				sessionHandle = result.SessionHandle
			}
		}
	default:
		if !sessionRoute(request) {
			writeProblem(writer, requestID, http.StatusNotFound)
			return
		}
		digest, err := browserauth.ProofDigest(request.Header.Get("X-Cloud-Agents-Session"))
		if err != nil || len(request.Header.Values("X-Cloud-Agents-Session")) != 1 {
			writeProblem(writer, requestID, http.StatusUnauthorized)
			return
		}
		switch {
		case request.Method == http.MethodGet && policyRoute:
			var result api.EmailSuffixPolicy
			result, operationErr = server.store.GetEmailSuffixPolicy(ctx, digest, policyTenantID)
			if operationErr == nil {
				encoded, operationErr = api.EncodeEmailSuffixPolicyJSON(result)
			}
		case request.Method == http.MethodPut && policyRoute:
			input, err := api.DecodeEmailSuffixPolicyUpdateJSON(body)
			if err != nil {
				writeProblem(writer, requestID, http.StatusBadRequest)
				return
			}
			operationErr = server.requireSessionCSRF(ctx, application, digest, request)
			if operationErr == nil {
				var result api.EmailSuffixPolicy
				result, operationErr = server.store.UpdateEmailSuffixPolicy(ctx, digest, policyTenantID, input)
				if operationErr == nil {
					encoded, operationErr = api.EncodeEmailSuffixPolicyJSON(result)
				}
			}
		case request.Method == http.MethodGet && request.URL.Path == "/v1/identity/me/tenants":
			input, err := identityPageRequest(request)
			if err != nil {
				writeProblem(writer, requestID, http.StatusBadRequest)
				return
			}
			var result api.BrowserTenantPage
			result, operationErr = server.store.Tenants(ctx, application, digest, input.PageSize, input.PageToken)
			if operationErr == nil && len(result.Tenants) > input.PageSize {
				operationErr = errors.New("invalid identity tenant page")
			}
			if operationErr == nil {
				encoded, operationErr = api.EncodeBrowserTenantPageJSON(result)
			}
		case request.Method == http.MethodDelete:
			proof := request.Header.Get("X-CSRF-Token")
			if _, err := browserauth.ProofDigest(proof); err != nil || len(request.Header.Values("X-CSRF-Token")) != 1 {
				writeProblem(writer, requestID, http.StatusForbidden)
				return
			}
			operationErr = server.store.Logout(ctx, application, digest, proof)
			if operationErr == nil {
				writer.WriteHeader(http.StatusNoContent)
				return
			}
		case request.Method == http.MethodPost:
			input, err := api.DecodeTenantTokenIssueRequestJSON(body)
			if err != nil {
				writeProblem(writer, requestID, http.StatusBadRequest)
				return
			}
			var result api.TenantToken
			result, operationErr = server.store.IssueTenantToken(ctx, application, digest, input)
			if operationErr == nil {
				encoded, operationErr = api.EncodeTenantTokenJSON(result)
			}
		default:
			var result api.BrowserSession
			result, operationErr = server.store.Session(ctx, application, digest)
			if operationErr == nil && result.Application != application {
				operationErr = errors.New("invalid identity session result")
			}
			if csrfDigest, err := browserauth.ProofDigest(result.CSRFToken); operationErr == nil && (err != nil || csrfDigest == digest) {
				operationErr = errors.New("invalid identity CSRF proof")
			}
			if operationErr == nil {
				switch request.URL.Path {
				case "/v1/identity/session":
					encoded, operationErr = api.EncodeBrowserSessionJSON(result)
				case "/v1/identity/me":
					encoded, operationErr = api.EncodeCurrentUserJSON(result.User)
				}
			}
		}
	}
	if operationErr != nil {
		status := http.StatusServiceUnavailable
		switch {
		case errors.Is(operationErr, browserauth.ErrUnauthorized):
			status = http.StatusUnauthorized
		case errors.Is(operationErr, ErrEmailPolicyInvalid), errors.Is(operationErr, ErrInvitationInvalid), errors.Is(operationErr, ErrAccountSecurityInvalid), errors.Is(operationErr, ErrProviderInput), errors.Is(operationErr, ErrCLIInvalid), errors.Is(operationErr, ErrAutomationInvalid):
			status = http.StatusBadRequest
		case errors.Is(operationErr, ErrForbidden):
			status = http.StatusForbidden
		case errors.Is(operationErr, ErrEmailPolicyConflict), errors.Is(operationErr, ErrInvitationConflict), errors.Is(operationErr, ErrAccountSecurityConflict), errors.Is(operationErr, ErrProviderConflict):
			status = http.StatusConflict
		case errors.Is(operationErr, ErrAccountSecurityNotFound):
			status = http.StatusNotFound
		case errors.Is(operationErr, ErrRateLimited):
			status = http.StatusTooManyRequests
		case errors.Is(operationErr, errIdentityRouteNotFound):
			status = http.StatusNotFound
		}
		writeProblem(writer, requestID, status)
		return
	}
	if len(encoded) > 2<<20 {
		writeProblem(writer, requestID, http.StatusServiceUnavailable)
		return
	}
	if sessionHandle != "" {
		writer.Header().Set("X-Cloud-Agents-Session", sessionHandle)
	}
	for name, value := range responseHeaders {
		writer.Header().Set(name, value)
	}
	if responseStatus == http.StatusNoContent {
		writer.WriteHeader(responseStatus)
		return
	}
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(responseStatus)
	_, _ = writer.Write(encoded)
}

func (server *Server) authenticate(request *http.Request) (api.IdentityApplication, bool) {
	authorization := request.Header.Get("Authorization")
	if len(request.Header.Values("Authorization")) != 1 || !strings.HasPrefix(authorization, "Bearer ") {
		return "", false
	}
	digest, err := browserauth.ProofDigest(strings.TrimPrefix(authorization, "Bearer "))
	if err != nil {
		return "", false
	}
	application, ok := server.services[digest]
	return application, ok
}

func sessionRoute(request *http.Request) bool {
	_, policyRoute := emailPolicyTenantID(request.URL.Path)
	switch request.Method {
	case http.MethodGet:
		return request.URL.Path == "/v1/identity/session" || request.URL.Path == "/v1/identity/me" || request.URL.Path == "/v1/identity/me/tenants" || policyRoute
	case http.MethodDelete:
		return request.URL.Path == "/v1/identity/session"
	case http.MethodPost:
		return request.URL.Path == "/v1/identity/tenant-token"
	case http.MethodPut:
		return policyRoute
	default:
		return false
	}
}

func emailPolicyTenantID(requestPath string) (string, bool) {
	const prefix = "/v1/identity/tenants/"
	const suffix = "/email-policy"
	if !strings.HasPrefix(requestPath, prefix) || !strings.HasSuffix(requestPath, suffix) {
		return "", false
	}
	return strings.TrimSuffix(strings.TrimPrefix(requestPath, prefix), suffix), true
}

func writeProblem(writer http.ResponseWriter, requestID string, status int) {
	code := "IDENTITY_UNAVAILABLE"
	switch status {
	case http.StatusBadRequest, http.StatusUnsupportedMediaType:
		code = "IDENTITY_INVALID_REQUEST"
	case http.StatusUnauthorized:
		code = "IDENTITY_UNAUTHORIZED"
	case http.StatusForbidden:
		code = "IDENTITY_FORBIDDEN"
	case http.StatusConflict:
		code = "IDENTITY_CONFLICT"
	case http.StatusNotFound:
		code = "IDENTITY_NOT_FOUND"
	case http.StatusTooManyRequests:
		code = "IDENTITY_RATE_LIMITED"
	}
	writer.Header().Set("Content-Type", "application/problem+json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(common.Problem{Type: "urn:cloud-agents:identity:error", Title: http.StatusText(status), Status: status, Error: common.StableError{Code: code, Retryable: status == http.StatusServiceUnavailable || status == http.StatusTooManyRequests}, RequestID: requestID})
}
