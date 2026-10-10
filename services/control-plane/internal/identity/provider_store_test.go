package identity

import (
	"context"
	"errors"
	"net/netip"
	"strings"
	"testing"
	"time"

	api "github.com/hxp0618/cloud-agents/sdk/go/gen/openapi/v1alpha1"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/browserauth"
)

type providerPersistenceFake struct {
	client       ProviderClientSnapshot
	created      providerFlowStart
	flow         providerFlow
	finished     providerFinishInput
	consumed     bool
	finishResult providerFinishResult
	page         api.BrowserTenantPage
}

func (fake *providerPersistenceFake) ReadProviderClient(context.Context, api.IdentityApplication, string) (ProviderClientSnapshot, error) {
	return fake.client, nil
}
func (fake *providerPersistenceFake) CreateProviderFlow(_ context.Context, client ProviderClientSnapshot, start providerFlowStart) error {
	fake.created = start
	fake.flow.ProviderClientSnapshot = client
	fake.flow.StateDigest = start.StateDigest
	fake.flow.Purpose = start.Purpose
	fake.flow.SessionDigest = start.SessionDigest
	fake.flow.InvitationDigest = start.InvitationDigest
	fake.flow.ReauthDigest = start.ReauthDigest
	fake.flow.DisplayName = start.DisplayName
	return nil
}
func (fake *providerPersistenceFake) ConsumeProviderFlow(_ context.Context, application api.IdentityApplication, state, verifier, nonce [32]byte) (providerFlow, error) {
	if fake.consumed || application != fake.flow.Application || state != fake.created.StateDigest || verifier != fake.created.VerifierDigest || nonce != fake.created.NonceDigest {
		return providerFlow{}, ErrProviderConflict
	}
	fake.consumed = true
	return fake.flow, nil
}
func (fake *providerPersistenceFake) FinishProviderCallback(_ context.Context, input providerFinishInput) (providerFinishResult, error) {
	if !fake.consumed {
		return providerFinishResult{}, errors.New("provider exchange preceded durable consumption")
	}
	fake.finished = input
	return fake.finishResult, nil
}
func (fake *providerPersistenceFake) ReadTenantPage(context.Context, api.IdentityApplication, [32]byte) (api.BrowserTenantPage, error) {
	return fake.page, nil
}

type providerFactoryFake struct{ provider *loginProviderFake }

func (factory providerFactoryFake) Provider(context.Context, ProviderClientSnapshot) (browserauth.LoginProvider, error) {
	return factory.provider, nil
}

type loginProviderFake struct {
	persistence *providerPersistenceFake
	request     browserauth.AuthorizationRequest
	callback    browserauth.AuthorizationCallback
	identity    browserauth.ProviderIdentity
	err         error
}

func (provider *loginProviderFake) AuthorizationURL(request browserauth.AuthorizationRequest) (string, error) {
	provider.request = request
	return "https://provider.example/authorize", nil
}
func (provider *loginProviderFake) Exchange(_ context.Context, callback browserauth.AuthorizationCallback) (browserauth.ProviderIdentity, error) {
	if provider.persistence != nil && !provider.persistence.consumed {
		return browserauth.ProviderIdentity{}, errors.New("provider exchange preceded durable consumption")
	}
	provider.callback = callback
	return provider.identity, provider.err
}

func TestProviderFlowDerivesProofsAndConsumesBeforeExchange(t *testing.T) {
	client := ProviderClientSnapshot{
		ID: "provider-main", Application: api.IdentityApplicationAdmin, Kind: "oidc",
		Issuer: "https://issuer.example", ClientID: "client", RedirectURL: "https://admin.example/callback",
		SecretRef: "provider-secret", Revision: 7,
	}
	persistence := &providerPersistenceFake{client: client, finishResult: providerFinishResult{
		Purpose: ProviderPurposeLogin, UserID: "account-1", Email: "person@example.com", DisplayName: "Person",
	}, page: api.BrowserTenantPage{Tenants: []api.BrowserTenant{}}}
	provider := &loginProviderFake{persistence: persistence, identity: browserauth.ProviderIdentity{
		Issuer: client.Issuer, Subject: "subject-1", Email: "person@example.com", EmailVerified: true,
	}}
	store, err := newProviderStore(&PasswordStore{csrfKey: []byte(strings.Repeat("c", 32))}, persistence, providerFactoryFake{provider}, []byte(strings.Repeat("f", 32)))
	if err != nil {
		t.Fatal(err)
	}
	store.now = func() time.Time { return time.Unix(1000, 0).UTC() }

	authorization, err := store.StartAuthorization(context.Background(), ProviderAuthorizationInput{
		Application: api.IdentityApplicationAdmin, ProviderID: client.ID, Purpose: ProviderPurposeLogin,
	})
	if err != nil {
		t.Fatal(err)
	}
	if authorization.State == "" || authorization.AuthorizationURL != "https://provider.example/authorize" || !authorization.ExpiresAt.Equal(time.Unix(1000, 0).UTC().Add(providerFlowLifetime)) {
		t.Fatalf("unexpected authorization: %#v", authorization)
	}
	if provider.request.State != authorization.State || provider.request.Nonce == "" || provider.request.CodeChallenge == "" {
		t.Fatalf("incomplete provider request: %#v", provider.request)
	}
	if provider.request.Nonce == provider.request.State || provider.request.CodeChallenge == provider.request.State {
		t.Fatal("derived provider proofs must be domain-separated from state")
	}

	result, err := store.CompleteAuthorization(context.Background(), ProviderCallbackInput{
		Application: api.IdentityApplicationAdmin, State: authorization.State, Code: "provider-code",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Purpose != ProviderPurposeLogin || result.Login == nil || result.Login.SessionHandle == "" || result.ReauthProof != "" || result.LoginMethod != nil {
		t.Fatalf("unexpected callback result: %#v", result)
	}
	if provider.callback.CodeVerifier == "" || provider.callback.Nonce != provider.request.Nonce {
		t.Fatalf("unexpected callback proofs: %#v", provider.callback)
	}
	if persistence.finished.Flow.StateDigest != persistence.created.StateDigest || persistence.finished.Identity.Subject != "subject-1" {
		t.Fatalf("unexpected durable finish: %#v", persistence.finished)
	}
	if _, err := store.CompleteAuthorization(context.Background(), ProviderCallbackInput{
		Application: api.IdentityApplicationAdmin, State: authorization.State, Code: "provider-code",
	}); !errors.Is(err, ErrProviderConflict) {
		t.Fatalf("replayed callback error = %v", err)
	}
}

func TestProviderExchangeFailureStillConsumesFlow(t *testing.T) {
	client := ProviderClientSnapshot{ID: "provider-main", Application: api.IdentityApplicationUser, Kind: "oidc", Issuer: "https://issuer.example", ClientID: "client", RedirectURL: "https://user.example/callback", SecretRef: "secret", Revision: 1}
	persistence := &providerPersistenceFake{client: client}
	provider := &loginProviderFake{persistence: persistence, err: browserauth.ErrUnauthorized}
	store, err := newProviderStore(&PasswordStore{csrfKey: []byte(strings.Repeat("c", 32))}, persistence, providerFactoryFake{provider}, []byte(strings.Repeat("f", 32)))
	if err != nil {
		t.Fatal(err)
	}
	authorization, err := store.StartAuthorization(context.Background(), ProviderAuthorizationInput{Application: api.IdentityApplicationUser, ProviderID: client.ID, Purpose: ProviderPurposeLogin})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CompleteAuthorization(context.Background(), ProviderCallbackInput{Application: api.IdentityApplicationUser, State: authorization.State, Code: "bad-code"}); !errors.Is(err, browserauth.ErrUnauthorized) {
		t.Fatalf("callback error = %v", err)
	}
	if !persistence.consumed {
		t.Fatal("failed provider exchange left flow reusable")
	}
}

func TestProviderPurposeBindingsAreClosed(t *testing.T) {
	session := [32]byte{1}
	cases := []ProviderAuthorizationInput{
		{Application: api.IdentityApplicationAdmin, ProviderID: "p", Purpose: ProviderPurposeLogin, SessionDigest: &session},
		{Application: api.IdentityApplicationUser, ProviderID: "p", Purpose: ProviderPurposeInvitation},
		{Application: api.IdentityApplicationUser, ProviderID: "p", Purpose: ProviderPurposeInvitation, InvitationCode: strings.Repeat("a", 43), ClientIP: netip.MustParseAddr("192.0.2.10")},
		{Application: api.IdentityApplicationUser, ProviderID: "p", Purpose: ProviderPurposeReauth},
		{Application: api.IdentityApplicationUser, ProviderID: "p", Purpose: ProviderPurposeLink, SessionDigest: &session},
		{Application: api.IdentityApplication("other"), ProviderID: "p", Purpose: ProviderPurposeLogin},
	}
	for _, input := range cases {
		if validProviderAuthorizationInput(input) {
			t.Fatalf("unexpected valid input: %#v", input)
		}
	}
}

func TestFixedProviderScopesAreRequiredByProviderClients(t *testing.T) {
	base := ProviderClient{
		ProviderClientSnapshot: ProviderClientSnapshot{
			ID: "provider-main", Application: api.IdentityApplicationAdmin,
			Issuer: "https://issuer.example", ClientID: "client", RedirectURL: "https://admin.example/callback",
			SecretRef: "secret", Revision: 1,
		},
		DisplayName: "Provider",
		Enabled:     true,
	}
	for _, test := range []struct {
		kind  string
		exact []string
		wrong []string
		agent string
	}{
		{kind: "github", exact: []string{"read:user", "user:email"}, wrong: []string{"openid"}},
		{kind: "feishu", exact: []string{"contact:user.email:readonly"}, wrong: []string{"email", "openid", "profile"}},
		{kind: "dingtalk", exact: []string{"corpid", "openid"}, wrong: []string{"openid"}},
		{kind: "wecom", exact: []string{}, wrong: []string{"openid"}, agent: "agent-id"},
	} {
		t.Run(test.kind, func(t *testing.T) {
			client := base
			client.Kind = test.kind
			client.AgentID = test.agent
			client.Scopes = append([]string(nil), test.exact...)
			if !validProviderClient(client) || !validProviderClientSnapshot(client.ProviderClientSnapshot) {
				t.Fatal("exact fixed scopes were rejected")
			}
			client.Scopes = append([]string(nil), test.wrong...)
			if validProviderClient(client) || validProviderClientSnapshot(client.ProviderClientSnapshot) {
				t.Fatal("wrong fixed scopes were accepted")
			}
			client.Scopes = append(append([]string(nil), test.exact...), "zz-extra")
			if validProviderClient(client) || validProviderClientSnapshot(client.ProviderClientSnapshot) {
				t.Fatal("additional fixed scope was accepted")
			}
		})
	}
}

func TestProviderCallbackIssuerIsPinnedAndCorporateProvidersRejectIt(t *testing.T) {
	for _, test := range []struct {
		name           string
		kind           string
		clientIssuer   string
		callbackIssuer string
		scopes         []string
		wantErr        bool
	}{
		{name: "OIDC mismatch", kind: "oidc", clientIssuer: "https://issuer.example", callbackIssuer: "https://other.example", wantErr: true},
		{name: "GitHub callback issuer", kind: "github", clientIssuer: "https://github.com", callbackIssuer: "https://github.com/login/oauth", scopes: []string{"read:user", "user:email"}},
		{name: "GitHub issuer omitted", kind: "github", clientIssuer: "https://github.com", scopes: []string{"read:user", "user:email"}},
		{name: "GitHub mismatch", kind: "github", clientIssuer: "https://github.com", callbackIssuer: "https://other.example", scopes: []string{"read:user", "user:email"}, wantErr: true},
		{name: "corporate issuer parameter", kind: "feishu", clientIssuer: "https://issuer.example", callbackIssuer: feishuProviderIssuer, scopes: []string{"contact:user.email:readonly"}, wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := ProviderClientSnapshot{
				ID: "provider-main", Application: api.IdentityApplicationUser, Kind: test.kind,
				Issuer: test.clientIssuer, ClientID: "client", RedirectURL: "https://user.example/callback",
				SecretRef: "secret", Scopes: test.scopes, Revision: 1,
			}
			persistence := &providerPersistenceFake{client: client, finishResult: providerFinishResult{
				Purpose: ProviderPurposeLogin, UserID: "account-1", Email: "person@example.com", DisplayName: "Person",
			}}
			provider := &loginProviderFake{persistence: persistence, identity: browserauth.ProviderIdentity{Issuer: client.Issuer, Subject: "subject"}}
			store, err := newProviderStore(&PasswordStore{csrfKey: []byte(strings.Repeat("c", 32))}, persistence, providerFactoryFake{provider}, []byte(strings.Repeat("f", 32)))
			if err != nil {
				t.Fatal(err)
			}
			authorization, err := store.StartAuthorization(context.Background(), ProviderAuthorizationInput{Application: api.IdentityApplicationUser, ProviderID: client.ID, Purpose: ProviderPurposeLogin})
			if err != nil {
				t.Fatal(err)
			}
			_, err = store.CompleteAuthorization(context.Background(), ProviderCallbackInput{
				Application: api.IdentityApplicationUser, State: authorization.State,
				Code: "authorization-code", Issuer: test.callbackIssuer, SessionState: "keycloak-session",
			})
			if test.wantErr && !errors.Is(err, browserauth.ErrUnauthorized) {
				t.Fatalf("callback error = %v", err)
			}
			if !test.wantErr && err != nil {
				t.Fatalf("callback error = %v", err)
			}
			if !persistence.consumed {
				t.Fatal("callback must consume the durable flow")
			}
			if test.wantErr && provider.callback.Code != "" {
				t.Fatal("issuer mismatch must reject before provider exchange")
			}
			if _, replayErr := store.CompleteAuthorization(context.Background(), ProviderCallbackInput{
				Application: api.IdentityApplicationUser, State: authorization.State,
				Code: "authorization-code", Issuer: test.callbackIssuer,
			}); !errors.Is(replayErr, ErrProviderConflict) {
				t.Fatalf("replayed callback error = %v", replayErr)
			}
		})
	}
}
