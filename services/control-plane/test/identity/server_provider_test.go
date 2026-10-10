package identity_test

import (
	"context"
	"crypto/sha256"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
	"time"

	api "github.com/hxp0618/cloud-agents/sdk/go/gen/openapi/v1alpha1"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/browserauth"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/identity"
)

type providerHTTPStore struct {
	*store
	start    identity.ProviderAuthorizationInput
	callback identity.ProviderCallbackResult
}

func (*providerHTTPStore) ListPublicProviders(context.Context, api.IdentityApplication) ([]identity.PublicProvider, error) {
	return []identity.PublicProvider{{ID: "provider-one", Kind: "oidc", DisplayName: "Example Login"}}, nil
}

func (store *providerHTTPStore) StartAuthorization(_ context.Context, input identity.ProviderAuthorizationInput) (identity.ProviderAuthorization, error) {
	store.start = input
	return identity.ProviderAuthorization{State: providerProof(), AuthorizationURL: "https://login.example.test/authorize", ExpiresAt: time.Unix(1_800_000_000, 0).UTC()}, nil
}

func (store *providerHTTPStore) CompleteAuthorization(context.Context, identity.ProviderCallbackInput) (identity.ProviderCallbackResult, error) {
	return store.callback, nil
}

func (*providerHTTPStore) ListLoginMethods(context.Context, api.IdentityApplication, [sha256.Size]byte) (identity.LoginMethodPage, error) {
	method := identity.LoginMethod{ProviderLoginMethod: identity.ProviderLoginMethod{
		ID: "method-one", ProviderID: "provider-one", Issuer: "https://login.example.test", Subject: "subject-one", CreatedAt: time.Unix(1_700_000_000, 0).UTC(),
	}}
	return identity.LoginMethodPage{PasswordEnabled: false, Methods: []identity.LoginMethod{method}}, nil
}

func (*providerHTTPStore) PasswordReauthenticate(context.Context, api.IdentityApplication, [sha256.Size]byte, string) (identity.Reauthentication, error) {
	return identity.Reauthentication{Proof: providerProof(), SessionHandle: providerProof(), ExpiresAt: time.Unix(1_800_000_000, 0).UTC()}, nil
}

func (*providerHTTPStore) UnlinkLoginMethod(context.Context, api.IdentityApplication, [sha256.Size]byte, string, string) error {
	return nil
}

func (*providerHTTPStore) EnablePassword(context.Context, api.IdentityApplication, [sha256.Size]byte, string, string) error {
	return nil
}

func (*providerHTTPStore) ListProviderClients(context.Context, [sha256.Size]byte) ([]identity.ProviderClient, error) {
	return []identity.ProviderClient{providerClient(2)}, nil
}

func (*providerHTTPStore) UpsertProviderClient(context.Context, [sha256.Size]byte, identity.ProviderClient, int64) (int64, error) {
	return 3, nil
}

func TestProviderHTTPPrivateProofAndPurposeBoundary(t *testing.T) {
	credentials := identity.ServiceCredentials{AdminWeb: providerProof(), UserWeb: providerProof(), ControlPlane: providerProof()}
	base := &store{handle: providerProof(), csrf: providerProof()}
	backend := &providerHTTPStore{store: base}
	handler, err := identity.NewServer(backend, credentials)
	if err != nil {
		t.Fatal(err)
	}
	upstream := httptest.NewTLSServer(handler)
	defer upstream.Close()
	user, err := api.NewIdentityServiceHTTPClientWithClient(upstream.URL, credentials.UserWeb, upstream.Client())
	if err != nil {
		t.Fatal(err)
	}
	page, err := user.ListLoginProviders(context.Background(), "provider-list")
	if err != nil || len(page.Providers) != 1 || page.Providers[0].ID != "provider-one" {
		t.Fatalf("providers = %#v, %v", page, err)
	}
	started, err := user.StartProviderAuthorization(context.Background(), "", "provider-start", "", "", "", api.ProviderAuthorizationRequest{ProviderID: "provider-one", Purpose: "login"})
	if err != nil || started.State == "" || backend.start.Purpose != identity.ProviderPurposeLogin {
		t.Fatalf("login start = %#v, %#v, %v", started, backend.start, err)
	}
	_, err = user.StartProviderAuthorization(context.Background(), "", "provider-invite", "192.0.2.20", "", "", api.ProviderAuthorizationRequest{
		ProviderID: "provider-one", Purpose: "invitation", InvitationCode: providerProof(), DisplayName: "Invited Person",
	})
	if err != nil || backend.start.ClientIP != netip.MustParseAddr("192.0.2.20") {
		t.Fatalf("invitation start = %#v, %v", backend.start, err)
	}
	_, err = user.StartProviderAuthorization(context.Background(), base.handle, "provider-link", "", base.csrf, providerProof(), api.ProviderAuthorizationRequest{ProviderID: "provider-one", Purpose: "link"})
	if err != nil || backend.start.SessionDigest == nil || backend.start.ReauthProof == "" {
		t.Fatalf("link start = %#v, %v", backend.start, err)
	}
	methods, err := user.ListLoginMethods(context.Background(), base.handle, "provider-methods")
	if err != nil || methods.PasswordEnabled || len(methods.LoginMethods) != 1 {
		t.Fatalf("methods = %#v, %v", methods, err)
	}
	reauth, err := user.PasswordReauthenticate(context.Background(), base.handle, "provider-reauth", base.csrf, api.PasswordReauthRequest{Password: "a sufficiently long password"})
	if err != nil || reauth.ReauthProof == "" || reauth.SessionHandle == "" || reauth.Reauthentication.ExpiresAt == "" {
		t.Fatalf("reauth = %#v, %v", reauth, err)
	}

	backend.callback = identity.ProviderCallbackResult{Purpose: identity.ProviderPurposeLogin, Login: &api.IdentityLoginResult{Session: base.session(api.IdentityApplicationUser), SessionHandle: base.handle}}
	callback, err := user.CompleteProviderAuthorization(context.Background(), "provider-callback", api.ProviderCallbackRequest{State: providerProof(), Code: providerProof()})
	if err != nil || callback.Callback.Action != "login" || callback.SessionHandle != base.handle || callback.ReauthProof != "" {
		t.Fatalf("login callback = %#v, %v", callback, err)
	}
	backend.callback = identity.ProviderCallbackResult{Purpose: identity.ProviderPurposeLink, LoginMethod: &identity.ProviderLoginMethod{
		ID: "method-two", ProviderID: "provider-one", Issuer: "https://login.example.test", Subject: "subject-two", CreatedAt: time.Unix(1_700_000_000, 0).UTC(),
	}}
	callback, err = user.CompleteProviderAuthorization(context.Background(), "provider-link-callback", api.ProviderCallbackRequest{State: providerProof(), Code: providerProof()})
	if err != nil || callback.Callback.Action != "link" || callback.Callback.LoginMethod == nil || callback.SessionHandle != "" || callback.ReauthProof != "" {
		t.Fatalf("link callback = %#v, %v", callback, err)
	}
	backend.callback = identity.ProviderCallbackResult{
		Purpose: identity.ProviderPurposeReauth, SessionHandle: providerProof(), ReauthProof: providerProof(),
		ReauthExpiresAt: time.Unix(1_800_000_000, 0).UTC(),
	}
	callback, err = user.CompleteProviderAuthorization(context.Background(), "provider-reauth-callback", api.ProviderCallbackRequest{State: providerProof(), Code: providerProof()})
	if err != nil || callback.Callback.Action != "reauth" || callback.SessionHandle == "" || callback.ReauthProof == "" {
		t.Fatalf("reauth callback = %#v, %v", callback, err)
	}
}

func TestProviderHTTPRejectsAmbiguousPrivateHeadersAndUserAdminAccess(t *testing.T) {
	credentials := identity.ServiceCredentials{AdminWeb: providerProof(), UserWeb: providerProof(), ControlPlane: providerProof()}
	base := &store{handle: providerProof(), csrf: providerProof()}
	handler, err := identity.NewServer(&providerHTTPStore{store: base}, credentials)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/v1/identity/login/provider/callback", nil)
	request.Header.Set("Authorization", "Bearer "+credentials.UserWeb)
	request.Header.Set("X-Request-ID", "provider-private-header")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Cloud-Agents-Session", base.handle)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("callback with session status = %d", response.Code)
	}

	upstream := httptest.NewTLSServer(handler)
	defer upstream.Close()
	user, err := api.NewIdentityServiceHTTPClientWithClient(upstream.URL, credentials.UserWeb, upstream.Client())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := user.ListProviderClients(context.Background(), base.handle, "provider-admin-list"); err == nil {
		t.Fatal("User application listed provider configuration")
	}
	admin, err := api.NewIdentityServiceHTTPClientWithClient(upstream.URL, credentials.AdminWeb, upstream.Client())
	if err != nil {
		t.Fatal(err)
	}
	providers, err := admin.ListProviderClients(context.Background(), base.handle, "provider-admin-list")
	if err != nil || len(providers.Providers) != 1 || providers.Providers[0].SecretRef != "provider-secret" {
		t.Fatalf("admin providers = %#v, %v", providers, err)
	}
}

func providerProof() string {
	value, _, err := browserauth.NewProof()
	if err != nil {
		panic(err)
	}
	return value
}

func providerClient(revision int64) identity.ProviderClient {
	return identity.ProviderClient{ProviderClientSnapshot: identity.ProviderClientSnapshot{
		ID: "provider-one", Application: api.IdentityApplicationAdmin, Kind: "oidc", Issuer: "https://login.example.test",
		ClientID: "client-one", RedirectURL: "https://admin.example.test/oauth/callback", SecretRef: "provider-secret",
		Scopes: []string{"email", "openid"}, AllowedOrganizationIDs: []string{}, Revision: revision,
	}, DisplayName: "Example Login", Enabled: true}
}
