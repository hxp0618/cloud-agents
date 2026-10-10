package identity

import (
	"context"
	"errors"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"

	common "github.com/hxp0618/cloud-agents/sdk/go/gen/common/v1alpha1"
	api "github.com/hxp0618/cloud-agents/sdk/go/gen/openapi/v1alpha1"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/browserauth"
)

var errIdentityRouteNotFound = errors.New("identity route unavailable")

func invitationPath(path string) (tenantID, invitationID string, ok bool) {
	parts := strings.Split(path, "/")
	if len(parts) != 6 && len(parts) != 7 || parts[0] != "" || parts[1] != "v1" || parts[2] != "identity" || parts[3] != "tenants" || parts[5] != "invitations" {
		return "", "", false
	}
	if common.ValidateIdentifier(parts[4], "/tenantId") != nil {
		return "", "", false
	}
	if len(parts) == 7 {
		if common.ValidateIdentifier(parts[6], "/invitationId") != nil {
			return "", "", false
		}
		invitationID = parts[6]
	}
	return parts[4], invitationID, true
}

func (server *Server) invitationOperation(ctx context.Context, request *http.Request, body []byte, application api.IdentityApplication, tenantID, invitationID string) ([]byte, int, error) {
	accept := request.URL.Path == "/v1/identity/invitations/accept"
	if accept && request.Method != http.MethodPost || !accept && (invitationID != "" && request.Method != http.MethodDelete || invitationID == "" && request.Method != http.MethodGet && request.Method != http.MethodPost) {
		return nil, 0, errIdentityRouteNotFound
	}
	if !accept && application != api.IdentityApplicationAdmin {
		return nil, 0, ErrForbidden
	}
	var sessionDigest *[32]byte
	if len(request.Header.Values("X-Cloud-Agents-Session")) > 0 {
		digest, err := browserauth.ProofDigest(request.Header.Get("X-Cloud-Agents-Session"))
		if err != nil || len(request.Header.Values("X-Cloud-Agents-Session")) != 1 {
			return nil, 0, browserauth.ErrUnauthorized
		}
		sessionDigest = &digest
	} else if !accept {
		return nil, 0, browserauth.ErrUnauthorized
	}
	if sessionDigest != nil && request.Method != http.MethodGet {
		if err := server.requireSessionCSRF(ctx, application, *sessionDigest, request); err != nil {
			return nil, 0, err
		}
	}
	if accept {
		if sessionDigest == nil && len(request.Header.Values("X-CSRF-Token")) != 0 {
			return nil, 0, ErrForbidden
		}
		clientIP, err := netip.ParseAddr(request.Header.Get("X-Cloud-Agents-Client-IP"))
		if err != nil || clientIP.Zone() != "" || clientIP.String() != request.Header.Get("X-Cloud-Agents-Client-IP") || len(request.Header.Values("X-Cloud-Agents-Client-IP")) != 1 {
			return nil, 0, ErrInvitationInvalid
		}
		input, err := api.DecodeInvitationAcceptRequestJSON(body)
		if err != nil {
			return nil, 0, ErrInvitationInvalid
		}
		return nil, http.StatusNoContent, server.store.AcceptInvitation(ctx, application, sessionDigest, clientIP.Unmap(), input)
	}
	switch request.Method {
	case http.MethodGet:
		input, err := identityPageRequest(request)
		if err != nil {
			return nil, 0, ErrInvitationInvalid
		}
		page, err := server.store.ListInvitations(ctx, *sessionDigest, tenantID, input.PageSize, input.PageToken)
		if err != nil {
			return nil, 0, err
		}
		if len(page.Invitations) > input.PageSize {
			return nil, 0, ErrUnavailable
		}
		for _, invitation := range page.Invitations {
			if invitation.TenantID != tenantID {
				return nil, 0, ErrUnavailable
			}
		}
		encoded, err := api.EncodeInvitationPageJSON(page)
		return encoded, http.StatusOK, err
	case http.MethodPost:
		input, err := api.DecodeInvitationCreateRequestJSON(body)
		if err != nil {
			return nil, 0, ErrInvitationInvalid
		}
		created, err := server.store.CreateInvitation(ctx, *sessionDigest, tenantID, input)
		if err != nil {
			return nil, 0, err
		}
		if created.Invitation.TenantID != tenantID {
			return nil, 0, ErrUnavailable
		}
		encoded, err := api.EncodeInvitationCreatedJSON(created)
		return encoded, http.StatusCreated, err
	default:
		return nil, http.StatusNoContent, server.store.RevokeInvitation(ctx, *sessionDigest, tenantID, invitationID)
	}
}

func (server *Server) requireSessionCSRF(ctx context.Context, application api.IdentityApplication, digest [32]byte, request *http.Request) error {
	proof, err := browserauth.ProofDigest(request.Header.Get("X-CSRF-Token"))
	if err != nil || len(request.Header.Values("X-CSRF-Token")) != 1 {
		return ErrForbidden
	}
	session, err := server.store.Session(ctx, application, digest)
	if err != nil {
		return err
	}
	expected, err := browserauth.ProofDigest(session.CSRFToken)
	if err != nil || expected == digest || session.Application != application {
		return ErrUnavailable
	}
	if expected != proof {
		return ErrForbidden
	}
	return nil
}

func identityPageRequest(request *http.Request) (api.ListBrowserTenantsServerInput, error) {
	query, err := url.ParseQuery(request.URL.RawQuery)
	pageSize, sizeErr := strconv.Atoi(query.Get("pageSize"))
	if err != nil || sizeErr != nil || len(query["pageSize"]) != 1 || len(query["pageToken"]) > 1 || len(query) > 2 || (len(query) == 2 && !query.Has("pageToken")) {
		return api.ListBrowserTenantsServerInput{}, ErrInvitationInvalid
	}
	return api.ValidateListBrowserTenantsServerRequest(pageSize, query.Get("pageToken"))
}
