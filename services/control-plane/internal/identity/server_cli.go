package identity

import (
	"context"
	"crypto/sha256"
	"net/http"
	"net/netip"
	"strings"

	common "github.com/hxp0618/cloud-agents/sdk/go/gen/common/v1alpha1"
	api "github.com/hxp0618/cloud-agents/sdk/go/gen/openapi/v1alpha1"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/browserauth"
)

const cliGrantHeader = "X-Cloud-Agents-CLI-Grant"

type cliHTTPStore interface {
	StartCLIAuthorization(context.Context, api.IdentityApplication, netip.Addr, api.CLIAuthorizationStartRequest) (api.CLIAuthorization, error)
	ApproveCLIAuthorization(context.Context, api.IdentityApplication, [sha256.Size]byte, string, api.CLIAuthorizationApproveRequest) (api.CLIAuthorizationApproved, error)
	ExchangeCLIGrant(context.Context, api.IdentityApplication, netip.Addr, api.CLIGrantExchangeRequest) (api.CLIGrant, error)
	ListCLITenants(context.Context, api.IdentityApplication, [sha256.Size]byte, int, string) (api.BrowserTenantPage, error)
	IssueCLITenantToken(context.Context, api.IdentityApplication, [sha256.Size]byte, api.TenantTokenIssueRequest) (api.TenantToken, error)
	RevokeCLIGrant(context.Context, api.IdentityApplication, [sha256.Size]byte) error
}

type cliRoute struct{ kind, authorizationID string }

func matchCLIRoute(path string) (cliRoute, bool) {
	switch path {
	case "/v1/identity/cli/authorizations":
		return cliRoute{kind: "start"}, true
	case "/v1/identity/cli/grants/exchange":
		return cliRoute{kind: "exchange"}, true
	case "/v1/identity/cli/tenants":
		return cliRoute{kind: "tenants"}, true
	case "/v1/identity/cli/tenant-token":
		return cliRoute{kind: "tenant-token"}, true
	case "/v1/identity/cli/grant":
		return cliRoute{kind: "grant"}, true
	}
	const prefix = "/v1/identity/cli/authorizations/"
	const suffix = "/approve"
	if strings.HasPrefix(path, prefix) && strings.HasSuffix(path, suffix) {
		identifier := strings.TrimSuffix(strings.TrimPrefix(path, prefix), suffix)
		if common.ValidateIdentifier(identifier, "/cliAuthorizationId") == nil {
			return cliRoute{kind: "approve", authorizationID: identifier}, true
		}
	}
	return cliRoute{}, false
}

func (server *Server) cliOperation(ctx context.Context, request *http.Request, body []byte, application api.IdentityApplication, route cliRoute) ([]byte, int, error) {
	if server.cli == nil || !validApplication(application) {
		return nil, 0, ErrForbidden
	}
	switch route.kind {
	case "start":
		if request.Method != http.MethodPost {
			return nil, 0, errIdentityRouteNotFound
		}
		clientIP, err := exactIdentityClientIP(request)
		if err != nil {
			return nil, 0, ErrCLIInvalid
		}
		input, err := api.DecodeCLIAuthorizationStartRequestJSON(body)
		if err != nil || input.Application != application {
			return nil, 0, ErrCLIInvalid
		}
		result, err := server.cli.StartCLIAuthorization(ctx, application, clientIP, input)
		if err != nil {
			return nil, 0, err
		}
		encoded, err := api.EncodeCLIAuthorizationJSON(result)
		return encoded, http.StatusOK, err
	case "approve":
		if request.Method != http.MethodPost {
			return nil, 0, errIdentityRouteNotFound
		}
		sessionDigest, err := exactIdentitySessionDigest(request)
		if err != nil {
			return nil, 0, browserauth.ErrUnauthorized
		}
		if err := server.requireSessionCSRF(ctx, application, sessionDigest, request); err != nil {
			return nil, 0, err
		}
		input, err := api.DecodeCLIAuthorizationApproveRequestJSON(body)
		if err != nil {
			return nil, 0, ErrCLIInvalid
		}
		result, err := server.cli.ApproveCLIAuthorization(ctx, application, sessionDigest, route.authorizationID, input)
		if err != nil {
			return nil, 0, err
		}
		encoded, err := api.EncodeCLIAuthorizationApprovedJSON(result)
		return encoded, http.StatusOK, err
	case "exchange":
		if request.Method != http.MethodPost {
			return nil, 0, errIdentityRouteNotFound
		}
		clientIP, err := exactIdentityClientIP(request)
		if err != nil {
			return nil, 0, ErrCLIInvalid
		}
		input, err := api.DecodeCLIGrantExchangeRequestJSON(body)
		if err != nil {
			return nil, 0, ErrCLIInvalid
		}
		result, err := server.cli.ExchangeCLIGrant(ctx, application, clientIP, input)
		if err != nil {
			return nil, 0, err
		}
		encoded, err := api.EncodeCLIGrantJSON(result)
		return encoded, http.StatusOK, err
	case "tenants":
		if request.Method != http.MethodGet {
			return nil, 0, errIdentityRouteNotFound
		}
		grantDigest, err := exactCLIGrantDigest(request)
		if err != nil {
			return nil, 0, browserauth.ErrUnauthorized
		}
		pageRequest, err := identityPageRequest(request)
		if err != nil {
			return nil, 0, ErrCLIInvalid
		}
		page, err := server.cli.ListCLITenants(ctx, application, grantDigest, pageRequest.PageSize, pageRequest.PageToken)
		if err != nil {
			return nil, 0, err
		}
		encoded, err := api.EncodeBrowserTenantPageJSON(page)
		return encoded, http.StatusOK, err
	case "tenant-token":
		if request.Method != http.MethodPost {
			return nil, 0, errIdentityRouteNotFound
		}
		grantDigest, err := exactCLIGrantDigest(request)
		if err != nil {
			return nil, 0, browserauth.ErrUnauthorized
		}
		input, err := api.DecodeTenantTokenIssueRequestJSON(body)
		if err != nil {
			return nil, 0, ErrCLIInvalid
		}
		result, err := server.cli.IssueCLITenantToken(ctx, application, grantDigest, input)
		if err != nil {
			return nil, 0, err
		}
		encoded, err := api.EncodeTenantTokenJSON(result)
		return encoded, http.StatusOK, err
	case "grant":
		if request.Method != http.MethodDelete {
			return nil, 0, errIdentityRouteNotFound
		}
		grantDigest, err := exactCLIGrantDigest(request)
		if err != nil {
			return nil, 0, browserauth.ErrUnauthorized
		}
		return nil, http.StatusNoContent, server.cli.RevokeCLIGrant(ctx, application, grantDigest)
	default:
		return nil, 0, errIdentityRouteNotFound
	}
}

func exactIdentityClientIP(request *http.Request) (netip.Addr, error) {
	value := request.Header.Get(api.HeaderIdentityClientIP)
	clientIP, err := netip.ParseAddr(value)
	if err != nil || clientIP.Zone() != "" || clientIP.String() != value || len(request.Header.Values(api.HeaderIdentityClientIP)) != 1 {
		return netip.Addr{}, ErrCLIInvalid
	}
	return clientIP.Unmap(), nil
}

func exactIdentitySessionDigest(request *http.Request) ([sha256.Size]byte, error) {
	if len(request.Header.Values("X-Cloud-Agents-Session")) != 1 {
		return [sha256.Size]byte{}, browserauth.ErrUnauthorized
	}
	return browserauth.ProofDigest(request.Header.Get("X-Cloud-Agents-Session"))
}

func exactCLIGrantDigest(request *http.Request) ([sha256.Size]byte, error) {
	if len(request.Header.Values(cliGrantHeader)) != 1 {
		return [sha256.Size]byte{}, browserauth.ErrUnauthorized
	}
	return browserauth.ProofDigest(request.Header.Get(cliGrantHeader))
}
