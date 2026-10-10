package identity

import (
	"context"
	"crypto/sha256"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"time"

	common "github.com/hxp0618/cloud-agents/sdk/go/gen/common/v1alpha1"
	api "github.com/hxp0618/cloud-agents/sdk/go/gen/openapi/v1alpha1"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/browserauth"
)

const (
	providerStateHeader  = "X-Cloud-Agents-OAuth-State"
	providerReauthHeader = "X-Cloud-Agents-Reauthentication"
)

type providerHTTPStore interface {
	ListPublicProviders(context.Context, api.IdentityApplication) ([]PublicProvider, error)
	StartAuthorization(context.Context, ProviderAuthorizationInput) (ProviderAuthorization, error)
	CompleteAuthorization(context.Context, ProviderCallbackInput) (ProviderCallbackResult, error)
	ListLoginMethods(context.Context, api.IdentityApplication, [sha256.Size]byte) (LoginMethodPage, error)
	PasswordReauthenticate(context.Context, api.IdentityApplication, [sha256.Size]byte, string) (Reauthentication, error)
	UnlinkLoginMethod(context.Context, api.IdentityApplication, [sha256.Size]byte, string, string) error
	EnablePassword(context.Context, api.IdentityApplication, [sha256.Size]byte, string, string) error
	ListProviderClients(context.Context, [sha256.Size]byte) ([]ProviderClient, error)
	UpsertProviderClient(context.Context, [sha256.Size]byte, ProviderClient, int64) (int64, error)
}

type providerRoute struct {
	kind, identifier string
	application      api.IdentityApplication
}

func matchProviderRoute(path string) (providerRoute, bool) {
	switch path {
	case "/v1/identity/login/providers":
		return providerRoute{kind: "public-providers"}, true
	case "/v1/identity/login/provider/start":
		return providerRoute{kind: "start"}, true
	case "/v1/identity/login/provider/callback":
		return providerRoute{kind: "callback"}, true
	case "/v1/identity/me/login-methods":
		return providerRoute{kind: "login-methods"}, true
	case "/v1/identity/me/reauthenticate/password":
		return providerRoute{kind: "password-reauth"}, true
	case "/v1/identity/me/password":
		return providerRoute{kind: "enable-password"}, true
	case "/v1/identity/providers":
		return providerRoute{kind: "provider-clients"}, true
	}
	parts := strings.Split(path, "/")
	if len(parts) == 6 && parts[0] == "" && parts[1] == "v1" && parts[2] == "identity" && parts[3] == "me" && parts[4] == "login-methods" && common.ValidateIdentifier(parts[5], "/loginMethodId") == nil {
		return providerRoute{kind: "unlink", identifier: parts[5]}, true
	}
	if len(parts) == 7 && parts[0] == "" && parts[1] == "v1" && parts[2] == "identity" && parts[3] == "providers" && common.ValidateIdentifier(parts[4], "/providerId") == nil && parts[5] == "applications" {
		application := api.IdentityApplication(parts[6])
		if application == api.IdentityApplicationAdmin || application == api.IdentityApplicationUser {
			return providerRoute{kind: "provider-client", identifier: parts[4], application: application}, true
		}
	}
	return providerRoute{}, false
}

func (server *Server) providerOperation(ctx context.Context, request *http.Request, body []byte, application api.IdentityApplication, route providerRoute) ([]byte, int, map[string]string, error) {
	if server.providers == nil {
		return nil, 0, nil, ErrUnavailable
	}
	responseHeaders := map[string]string{}
	switch route.kind {
	case "public-providers":
		if request.Method != http.MethodGet || hasProviderPrivateHeaders(request) {
			return nil, 0, nil, errIdentityRouteNotFound
		}
		providers, err := server.providers.ListPublicProviders(ctx, application)
		if err != nil {
			return nil, 0, nil, err
		}
		page := api.LoginProviderPage{Providers: make([]api.LoginProvider, len(providers))}
		for index, provider := range providers {
			page.Providers[index] = api.LoginProvider{ID: provider.ID, Kind: provider.Kind, DisplayName: provider.DisplayName}
		}
		encoded, err := api.EncodeLoginProviderPageJSON(page)
		return encoded, http.StatusOK, responseHeaders, err
	case "start":
		if request.Method != http.MethodPost {
			return nil, 0, nil, errIdentityRouteNotFound
		}
		input, err := api.DecodeProviderAuthorizationRequestJSON(body)
		if err != nil {
			return nil, 0, nil, ErrProviderInput
		}
		start := ProviderAuthorizationInput{
			Application: application, ProviderID: input.ProviderID, Purpose: ProviderPurpose(input.Purpose),
			InvitationCode: input.InvitationCode, DisplayName: input.DisplayName,
		}
		switch start.Purpose {
		case ProviderPurposeLogin:
			if hasProviderPrivateHeaders(request) {
				return nil, 0, nil, ErrProviderInput
			}
		case ProviderPurposeInvitation:
			if len(request.Header.Values("X-Cloud-Agents-Session")) != 0 || len(request.Header.Values("X-CSRF-Token")) != 0 || len(request.Header.Values(providerReauthHeader)) != 0 {
				return nil, 0, nil, ErrProviderInput
			}
			start.ClientIP, err = canonicalProviderClientIP(request)
			if err != nil {
				return nil, 0, nil, err
			}
		case ProviderPurposeReauth, ProviderPurposeLink:
			if len(request.Header.Values("X-Cloud-Agents-Client-IP")) != 0 {
				return nil, 0, nil, ErrProviderInput
			}
			digest, digestErr := providerSessionDigest(request)
			if digestErr != nil {
				return nil, 0, nil, digestErr
			}
			if err := server.requireSessionCSRF(ctx, application, digest, request); err != nil {
				return nil, 0, nil, err
			}
			start.SessionDigest = &digest
			if start.Purpose == ProviderPurposeLink {
				proof, proofErr := singleProviderProofHeader(request, providerReauthHeader)
				if proofErr != nil {
					return nil, 0, nil, proofErr
				}
				start.ReauthProof = proof
			} else if len(request.Header.Values(providerReauthHeader)) != 0 {
				return nil, 0, nil, ErrProviderInput
			}
		default:
			return nil, 0, nil, ErrProviderInput
		}
		result, err := server.providers.StartAuthorization(ctx, start)
		if err != nil {
			return nil, 0, nil, err
		}
		encoded, err := api.EncodeProviderAuthorizationJSON(api.ProviderAuthorization{
			AuthorizationURL: result.AuthorizationURL, ExpiresAt: result.ExpiresAt.UTC().Format(time.RFC3339Nano),
		})
		if err == nil {
			responseHeaders[providerStateHeader] = result.State
		}
		return encoded, http.StatusOK, responseHeaders, err
	case "callback":
		if request.Method != http.MethodPost || hasProviderPrivateHeaders(request) {
			return nil, 0, nil, ErrProviderInput
		}
		input, err := api.DecodeProviderCallbackRequestJSON(body)
		if err != nil {
			return nil, 0, nil, ErrProviderInput
		}
		result, err := server.providers.CompleteAuthorization(ctx, ProviderCallbackInput{
			Application: application, State: input.State, Code: input.Code, Issuer: input.Issuer, SessionState: input.SessionState,
		})
		if err != nil {
			return nil, 0, nil, err
		}
		callback := api.ProviderCallback{Action: string(result.Purpose)}
		switch result.Purpose {
		case ProviderPurposeLogin, ProviderPurposeInvitation:
			if result.Login == nil {
				return nil, 0, nil, ErrUnavailable
			}
			callback.Session = &result.Login.Session
			responseHeaders["X-Cloud-Agents-Session"] = result.Login.SessionHandle
		case ProviderPurposeReauth:
			callback.ExpiresAt = result.ReauthExpiresAt.UTC().Format(time.RFC3339Nano)
			responseHeaders["X-Cloud-Agents-Session"] = result.SessionHandle
			responseHeaders[providerReauthHeader] = result.ReauthProof
		case ProviderPurposeLink:
			if result.LoginMethod == nil {
				return nil, 0, nil, ErrUnavailable
			}
			callback.LoginMethod = &api.LoginMethod{
				ID: result.LoginMethod.ID, ProviderID: result.LoginMethod.ProviderID, Issuer: result.LoginMethod.Issuer,
				Subject: result.LoginMethod.Subject, CreatedAt: result.LoginMethod.CreatedAt.UTC().Format(time.RFC3339Nano),
			}
		default:
			return nil, 0, nil, ErrUnavailable
		}
		encoded, err := api.EncodeProviderCallbackJSON(callback)
		return encoded, http.StatusOK, responseHeaders, err
	case "login-methods":
		if request.Method != http.MethodGet || hasProviderMutationHeaders(request) {
			return nil, 0, nil, errIdentityRouteNotFound
		}
		digest, err := providerSessionDigest(request)
		if err != nil {
			return nil, 0, nil, err
		}
		page, err := server.providers.ListLoginMethods(ctx, application, digest)
		if err != nil {
			return nil, 0, nil, err
		}
		result := api.LoginMethodList{PasswordEnabled: page.PasswordEnabled, LoginMethods: make([]api.LoginMethod, len(page.Methods))}
		for index, method := range page.Methods {
			result.LoginMethods[index] = providerAPILoginMethod(method.ProviderLoginMethod)
		}
		encoded, err := api.EncodeLoginMethodListJSON(result)
		return encoded, http.StatusOK, responseHeaders, err
	case "password-reauth":
		if request.Method != http.MethodPost || len(request.Header.Values(providerReauthHeader)) != 0 || len(request.Header.Values("X-Cloud-Agents-Client-IP")) != 0 {
			return nil, 0, nil, errIdentityRouteNotFound
		}
		digest, err := providerAuthenticatedMutation(ctx, server, application, request)
		if err != nil {
			return nil, 0, nil, err
		}
		input, err := api.DecodePasswordReauthRequestJSON(body)
		if err != nil {
			return nil, 0, nil, ErrProviderInput
		}
		result, err := server.providers.PasswordReauthenticate(ctx, application, digest, input.Password)
		if err != nil {
			return nil, 0, nil, err
		}
		encoded, err := api.EncodeReauthenticationJSON(api.Reauthentication{ExpiresAt: result.ExpiresAt.UTC().Format(time.RFC3339Nano)})
		if err == nil {
			responseHeaders["X-Cloud-Agents-Session"] = result.SessionHandle
			responseHeaders[providerReauthHeader] = result.Proof
		}
		return encoded, http.StatusOK, responseHeaders, err
	case "unlink", "enable-password":
		if (route.kind == "unlink" && request.Method != http.MethodDelete) || (route.kind == "enable-password" && request.Method != http.MethodPost) || len(request.Header.Values("X-Cloud-Agents-Client-IP")) != 0 {
			return nil, 0, nil, errIdentityRouteNotFound
		}
		digest, err := providerAuthenticatedMutation(ctx, server, application, request)
		if err != nil {
			return nil, 0, nil, err
		}
		proof, err := singleProviderProofHeader(request, providerReauthHeader)
		if err != nil {
			return nil, 0, nil, err
		}
		if route.kind == "unlink" {
			err = server.providers.UnlinkLoginMethod(ctx, application, digest, route.identifier, proof)
		} else {
			var input api.EnablePasswordRequest
			input, err = api.DecodeEnablePasswordRequestJSON(body)
			if err == nil {
				err = server.providers.EnablePassword(ctx, application, digest, proof, input.NewPassword)
			}
		}
		if err != nil {
			return nil, 0, nil, err
		}
		return nil, http.StatusNoContent, responseHeaders, nil
	case "provider-clients", "provider-client":
		if application != api.IdentityApplicationAdmin {
			return nil, 0, nil, ErrForbidden
		}
		if len(request.Header.Values(providerReauthHeader)) != 0 || len(request.Header.Values("X-Cloud-Agents-Client-IP")) != 0 {
			return nil, 0, nil, ErrProviderInput
		}
		digest, err := providerSessionDigest(request)
		if err != nil {
			return nil, 0, nil, err
		}
		if route.kind == "provider-clients" {
			if request.Method != http.MethodGet || len(request.Header.Values("X-CSRF-Token")) != 0 {
				return nil, 0, nil, errIdentityRouteNotFound
			}
			providers, err := server.providers.ListProviderClients(ctx, digest)
			if err != nil {
				return nil, 0, nil, err
			}
			page := api.ProviderClientPage{Providers: make([]api.ProviderClient, len(providers))}
			for index, provider := range providers {
				page.Providers[index] = providerAPIClient(provider)
			}
			encoded, err := api.EncodeProviderClientPageJSON(page)
			return encoded, http.StatusOK, responseHeaders, err
		}
		if request.Method != http.MethodPut {
			return nil, 0, nil, errIdentityRouteNotFound
		}
		if err := server.requireSessionCSRF(ctx, application, digest, request); err != nil {
			return nil, 0, nil, err
		}
		input, err := api.DecodeProviderClientUpdateJSON(body)
		if err != nil {
			return nil, 0, nil, ErrProviderInput
		}
		expectedRevision, err := strconv.ParseInt(input.ExpectedResourceVersion, 10, 64)
		if err != nil || expectedRevision < 0 {
			return nil, 0, nil, ErrProviderInput
		}
		provider := providerClientFromAPI(route.identifier, route.application, input, expectedRevision)
		revision, err := server.providers.UpsertProviderClient(ctx, digest, provider, expectedRevision)
		if err != nil {
			return nil, 0, nil, err
		}
		provider.Revision = revision
		encoded, err := api.EncodeProviderClientJSON(providerAPIClient(provider))
		return encoded, http.StatusOK, responseHeaders, err
	default:
		return nil, 0, nil, errIdentityRouteNotFound
	}
}

func providerSessionDigest(request *http.Request) ([sha256.Size]byte, error) {
	if len(request.Header.Values("X-Cloud-Agents-Session")) != 1 {
		return [sha256.Size]byte{}, browserauth.ErrUnauthorized
	}
	digest, err := browserauth.ProofDigest(request.Header.Get("X-Cloud-Agents-Session"))
	if err != nil {
		return [sha256.Size]byte{}, browserauth.ErrUnauthorized
	}
	return digest, nil
}

func providerAuthenticatedMutation(ctx context.Context, server *Server, application api.IdentityApplication, request *http.Request) ([sha256.Size]byte, error) {
	digest, err := providerSessionDigest(request)
	if err != nil {
		return [sha256.Size]byte{}, err
	}
	if err := server.requireSessionCSRF(ctx, application, digest, request); err != nil {
		return [sha256.Size]byte{}, err
	}
	return digest, nil
}

func singleProviderProofHeader(request *http.Request, name string) (string, error) {
	if len(request.Header.Values(name)) != 1 {
		return "", browserauth.ErrUnauthorized
	}
	proof := request.Header.Get(name)
	if _, err := browserauth.ProofDigest(proof); err != nil {
		return "", browserauth.ErrUnauthorized
	}
	return proof, nil
}

func canonicalProviderClientIP(request *http.Request) (netip.Addr, error) {
	if len(request.Header.Values("X-Cloud-Agents-Client-IP")) != 1 {
		return netip.Addr{}, ErrProviderInput
	}
	raw := request.Header.Get("X-Cloud-Agents-Client-IP")
	address, err := netip.ParseAddr(raw)
	if err != nil || address.Zone() != "" || address.String() != raw {
		return netip.Addr{}, ErrProviderInput
	}
	return address.Unmap(), nil
}

func hasProviderPrivateHeaders(request *http.Request) bool {
	return len(request.Header.Values("X-Cloud-Agents-Session")) != 0 || hasProviderMutationHeaders(request)
}

func hasProviderMutationHeaders(request *http.Request) bool {
	return len(request.Header.Values("X-CSRF-Token")) != 0 || len(request.Header.Values(providerReauthHeader)) != 0 || len(request.Header.Values(providerStateHeader)) != 0 || len(request.Header.Values("X-Cloud-Agents-Client-IP")) != 0
}

func providerAPILoginMethod(method ProviderLoginMethod) api.LoginMethod {
	return api.LoginMethod{
		ID: method.ID, ProviderID: method.ProviderID, Issuer: method.Issuer, Subject: method.Subject,
		CreatedAt: method.CreatedAt.UTC().Format(time.RFC3339Nano),
	}
}

func providerAPIClient(provider ProviderClient) api.ProviderClient {
	return api.ProviderClient{
		ProviderID: provider.ID, Application: provider.Application, DisplayName: provider.DisplayName,
		ProviderKind: provider.Kind, Issuer: provider.Issuer, ClientID: provider.ClientID,
		RedirectURI: provider.RedirectURL, SecretRef: provider.SecretRef, RootCARef: provider.RootCARef,
		AgentID: provider.AgentID, Scopes: provider.Scopes, TrustProviderEmail: provider.TrustProviderEmail,
		AllowedOrganizationIDs: provider.AllowedOrganizationIDs, Enabled: provider.Enabled,
		ResourceVersion: strconv.FormatInt(provider.Revision, 10),
	}
}

func providerClientFromAPI(providerID string, application api.IdentityApplication, input api.ProviderClientUpdate, revision int64) ProviderClient {
	return ProviderClient{
		ProviderClientSnapshot: ProviderClientSnapshot{
			ID: providerID, Application: application, Kind: input.ProviderKind, Issuer: input.Issuer,
			ClientID: input.ClientID, RedirectURL: input.RedirectURI, SecretRef: input.SecretRef,
			RootCARef: input.RootCARef, AgentID: input.AgentID, Scopes: input.Scopes,
			TrustProviderEmail: input.TrustProviderEmail, AllowedOrganizationIDs: input.AllowedOrganizationIDs,
			Revision: revision,
		},
		DisplayName: input.DisplayName, Enabled: input.Enabled,
	}
}
