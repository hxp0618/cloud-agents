package identity

import (
	"context"
	"net/http"
	"net/netip"
	"strings"

	common "github.com/hxp0618/cloud-agents/sdk/go/gen/common/v1alpha1"
	api "github.com/hxp0618/cloud-agents/sdk/go/gen/openapi/v1alpha1"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/browserauth"
)

type accountSecurityRoute struct{ kind, userID, tenantID string }

func matchAccountSecurityRoute(path string) (accountSecurityRoute, bool) {
	switch path {
	case "/v1/identity/accounts":
		return accountSecurityRoute{kind: "accounts"}, true
	case "/v1/identity/me/password":
		return accountSecurityRoute{kind: "change-password"}, true
	case "/v1/identity/password-resets/accept":
		return accountSecurityRoute{kind: "accept-reset"}, true
	case "/v1/identity/audit-events":
		return accountSecurityRoute{kind: "audit"}, true
	}
	parts := strings.Split(path, "/")
	if len(parts) == 7 && parts[0] == "" && parts[1] == "v1" && parts[2] == "identity" && parts[3] == "accounts" && common.ValidateIdentifier(parts[4], "/userId") == nil {
		if parts[5] == "disable" && parts[6] == "" {
			return accountSecurityRoute{}, false
		}
	}
	if len(parts) == 6 && parts[0] == "" && parts[1] == "v1" && parts[2] == "identity" && parts[3] == "accounts" && common.ValidateIdentifier(parts[4], "/userId") == nil {
		if parts[5] == "disable" {
			return accountSecurityRoute{kind: "disable", userID: parts[4]}, true
		}
		if parts[5] == "password-reset" {
			return accountSecurityRoute{kind: "issue-reset", userID: parts[4]}, true
		}
	}
	if len(parts) == 7 && parts[0] == "" && parts[1] == "v1" && parts[2] == "identity" && parts[3] == "tenants" && common.ValidateIdentifier(parts[4], "/tenantId") == nil && parts[5] == "audit-events" && parts[6] == "" {
		return accountSecurityRoute{}, false
	}
	if len(parts) == 6 && parts[0] == "" && parts[1] == "v1" && parts[2] == "identity" && parts[3] == "tenants" && common.ValidateIdentifier(parts[4], "/tenantId") == nil && parts[5] == "audit-events" {
		return accountSecurityRoute{kind: "audit", tenantID: parts[4]}, true
	}
	if len(parts) == 6 && parts[0] == "" && parts[1] == "v1" && parts[2] == "identity" && parts[3] == "tenants" && common.ValidateIdentifier(parts[4], "/tenantId") == nil && parts[5] == "accounts" {
		return accountSecurityRoute{kind: "accounts", tenantID: parts[4]}, true
	}
	if len(parts) == 6 && parts[0] == "" && parts[1] == "v1" && parts[2] == "identity" && parts[3] == "tenants" && common.ValidateIdentifier(parts[4], "/tenantId") == nil && parts[5] == "control-plane-audit-events" {
		return accountSecurityRoute{kind: "control-plane-audit", tenantID: parts[4]}, true
	}
	return accountSecurityRoute{}, false
}

func (server *Server) accountSecurityOperation(ctx context.Context, request *http.Request, body []byte, application api.IdentityApplication, route accountSecurityRoute) ([]byte, int, error) {
	if route.kind == "accept-reset" {
		if request.Method != http.MethodPost {
			return nil, 0, errIdentityRouteNotFound
		}
		if len(request.Header.Values("X-Cloud-Agents-Session")) != 0 || len(request.Header.Values("X-CSRF-Token")) != 0 {
			return nil, 0, ErrForbidden
		}
		clientIP, err := netip.ParseAddr(request.Header.Get("X-Cloud-Agents-Client-IP"))
		if err != nil || clientIP.Zone() != "" || clientIP.String() != request.Header.Get("X-Cloud-Agents-Client-IP") || len(request.Header.Values("X-Cloud-Agents-Client-IP")) != 1 {
			return nil, 0, ErrAccountSecurityInvalid
		}
		input, err := api.DecodePasswordResetAcceptRequestJSON(body)
		if err != nil {
			return nil, 0, ErrAccountSecurityInvalid
		}
		return nil, http.StatusNoContent, server.store.AcceptPasswordReset(ctx, clientIP.Unmap(), input)
	}
	if route.kind != "change-password" && application != api.IdentityApplicationAdmin {
		return nil, 0, ErrForbidden
	}
	digest, err := browserauth.ProofDigest(request.Header.Get("X-Cloud-Agents-Session"))
	if err != nil || len(request.Header.Values("X-Cloud-Agents-Session")) != 1 {
		return nil, 0, browserauth.ErrUnauthorized
	}
	if request.Method != http.MethodGet {
		if err := server.requireSessionCSRF(ctx, application, digest, request); err != nil {
			return nil, 0, err
		}
	}
	switch route.kind {
	case "accounts":
		if request.Method != http.MethodGet {
			return nil, 0, errIdentityRouteNotFound
		}
		input, err := identityPageRequest(request)
		if err != nil {
			return nil, 0, ErrAccountSecurityInvalid
		}
		var page api.IdentityAccountPage
		if route.tenantID == "" {
			page, err = server.store.ListAccounts(ctx, digest, input.PageSize, input.PageToken)
		} else {
			page, err = server.store.ListTenantAccounts(ctx, digest, route.tenantID, input.PageSize, input.PageToken)
		}
		if err != nil {
			return nil, 0, err
		}
		encoded, err := api.EncodeIdentityAccountPageJSON(page)
		return encoded, http.StatusOK, err
	case "disable":
		if request.Method != http.MethodPost {
			return nil, 0, errIdentityRouteNotFound
		}
		return nil, http.StatusNoContent, server.store.DisableAccount(ctx, digest, route.userID)
	case "issue-reset":
		if request.Method != http.MethodPost {
			return nil, 0, errIdentityRouteNotFound
		}
		result, err := server.store.IssuePasswordReset(ctx, digest, route.userID)
		if err != nil {
			return nil, 0, err
		}
		encoded, err := api.EncodePasswordResetCreatedJSON(result)
		return encoded, http.StatusCreated, err
	case "change-password":
		if request.Method != http.MethodPut {
			return nil, 0, errIdentityRouteNotFound
		}
		input, err := api.DecodePasswordChangeRequestJSON(body)
		if err != nil {
			return nil, 0, ErrAccountSecurityInvalid
		}
		return nil, http.StatusNoContent, server.store.ChangePassword(ctx, application, digest, input)
	case "audit", "control-plane-audit":
		if request.Method != http.MethodGet {
			return nil, 0, errIdentityRouteNotFound
		}
		input, err := identityPageRequest(request)
		if err != nil {
			return nil, 0, ErrAccountSecurityInvalid
		}
		if route.kind == "control-plane-audit" {
			page, err := server.store.ListControlPlaneAuditEvents(ctx, digest, route.tenantID, input.PageSize, input.PageToken)
			if err != nil {
				return nil, 0, err
			}
			encoded, err := api.EncodeControlPlaneAuditPageJSON(page)
			return encoded, http.StatusOK, err
		}
		page, err := server.store.ListAuditEvents(ctx, digest, route.tenantID, input.PageSize, input.PageToken)
		if err != nil {
			return nil, 0, err
		}
		encoded, err := api.EncodeIdentityAuditPageJSON(page)
		return encoded, http.StatusOK, err
	default:
		return nil, 0, errIdentityRouteNotFound
	}
}
