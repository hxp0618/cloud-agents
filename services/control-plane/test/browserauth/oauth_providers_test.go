package browserauth_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/browserauth"
)

func TestOAuthProviderAdaptersReturnStableIdentityAndBoundEmailTrust(t *testing.T) {
	t.Parallel()
	const secret = "fixture-provider-secret"
	feishuUsers := map[string]map[string]any{
		"trusted": {
			"union_id": "union-feishu", "open_id": "open-feishu", "enterprise_email": "feishu@Example.COM", "tenant_key": "tenant-feishu",
		},
		"missing-email": {
			"union_id": "union-feishu", "open_id": "open-feishu", "tenant_key": "tenant-feishu",
		},
		"fallback-email": {
			"union_id": "union-feishu", "open_id": "open-feishu", "email": "fallback@Example.COM", "tenant_key": "tenant-feishu",
		},
		"open-id": {
			"open_id": "open-feishu", "enterprise_email": "feishu@example.com", "tenant_key": "tenant-feishu",
		},
		"missing-tenant": {
			"union_id": "union-feishu", "open_id": "open-feishu", "enterprise_email": "feishu@example.com",
		},
	}
	var feishuAuthorizationChallenge string
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch request.Host + request.URL.Path {
		case "github.com/login/oauth/access_token":
			if err := request.ParseForm(); err != nil || request.Form.Get("client_secret") != secret || request.Form.Get("code_verifier") == "" {
				t.Errorf("invalid GitHub token request")
			}
			writeJSON(t, writer, map[string]any{"access_token": "github-token", "token_type": "Bearer"})
		case "api.github.com/user":
			requireHeader(t, request, "Authorization", "Bearer github-token")
			writeJSON(t, writer, map[string]any{"id": 12345, "email": "public@example.com", "login": "fixture"})
		case "api.github.com/user/emails":
			writeJSON(t, writer, []any{
				map[string]any{"email": "secondary@example.com", "primary": false, "verified": true},
				map[string]any{"email": "primary@example.com", "primary": true, "verified": true},
			})
		case "open.feishu.cn/open-apis/authen/v2/oauth/token":
			if request.Method != http.MethodPost || request.Header.Get("Content-Type") != "application/json" || request.URL.RawQuery != "" {
				t.Error("invalid Feishu token request metadata")
			}
			var body map[string]string
			decodeJSON(t, request, &body)
			if len(body) != 6 || body["grant_type"] != "authorization_code" || body["client_id"] != "feishu-client" || body["client_secret"] != secret || body["code_verifier"] == "" || body["redirect_uri"] != "https://identity.example.test/feishu" {
				t.Error("invalid Feishu token request")
			}
			if proofChallenge(body["code_verifier"]) != feishuAuthorizationChallenge {
				t.Error("Feishu token verifier did not match the authorization challenge")
			}
			if _, ok := feishuUsers[body["code"]]; !ok {
				t.Errorf("unknown Feishu fixture: %q", body["code"])
			}
			writeJSON(t, writer, map[string]any{"code": 0, "access_token": "feishu-" + body["code"], "token_type": "Bearer"})
		case "open.feishu.cn/open-apis/authen/v1/user_info":
			fixture := strings.TrimPrefix(request.Header.Get("Authorization"), "Bearer feishu-")
			user, ok := feishuUsers[fixture]
			if !ok {
				t.Errorf("unknown Feishu access token fixture: %q", fixture)
			}
			writeJSON(t, writer, map[string]any{"code": 0, "data": user})
		case "api.dingtalk.com/v1.0/oauth2/userAccessToken":
			var body map[string]string
			decodeJSON(t, request, &body)
			if body["clientSecret"] != secret || body["grantType"] != "authorization_code" {
				t.Error("invalid DingTalk token request")
			}
			writeJSON(t, writer, map[string]any{"accessToken": "dingtalk-token", "corpId": "corp-dingtalk"})
		case "api.dingtalk.com/v1.0/contact/users/me":
			requireHeader(t, request, "x-acs-dingtalk-access-token", "dingtalk-token")
			writeJSON(t, writer, map[string]any{"unionId": "union-dingtalk", "openId": "open-dingtalk", "email": "dingtalk@example.com"})
		case "qyapi.weixin.qq.com/cgi-bin/gettoken":
			if request.URL.Query().Get("corpid") != "corp-wecom" || request.URL.Query().Get("corpsecret") != secret {
				t.Error("invalid WeCom token request")
			}
			writeJSON(t, writer, map[string]any{"errcode": 0, "access_token": "wecom-token"})
		case "qyapi.weixin.qq.com/cgi-bin/auth/getuserinfo":
			if request.URL.Query().Get("access_token") != "wecom-token" || request.URL.Query().Get("code") != "authorization-code" {
				t.Error("invalid WeCom identity request")
			}
			writeJSON(t, writer, map[string]any{"errcode": 0, "userid": "wecom-user"})
		case "qyapi.weixin.qq.com/cgi-bin/user/get":
			writeJSON(t, writer, map[string]any{"errcode": 0, "userid": "wecom-user", "biz_mail": "wecom@example.com"})
		default:
			t.Errorf("unexpected provider request: %s %s", request.Host, request.URL.Path)
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()
	client := &http.Client{Transport: &rewriteTransport{target: mustURL(t, server.URL), base: server.Client().Transport}}
	state := newProof(t)
	verifier := newProof(t)
	challenge := proofChallenge(verifier)

	github, err := browserauth.NewGitHubProvider(browserauth.OAuthClientConfig{
		ClientID: "github-client", ClientSecret: secret, RedirectURL: "https://identity.example.test/github", HTTPClient: client,
	})
	if err != nil {
		t.Fatal(err)
	}
	assertAuthorizationURL(t, github, browserauth.AuthorizationRequest{State: state, CodeChallenge: challenge}, "github.com", "/login/oauth/authorize", map[string]string{"state": state, "code_challenge": challenge, "scope": "read:user user:email"})
	githubIdentity, err := github.Exchange(context.Background(), browserauth.AuthorizationCallback{Code: "authorization-code", CodeVerifier: verifier})
	if err != nil {
		t.Fatal(err)
	}
	if githubIdentity.Issuer != "https://github.com" || githubIdentity.Subject != "12345" || githubIdentity.Email != "primary@example.com" || !githubIdentity.EmailVerified {
		t.Fatalf("unexpected GitHub identity: %#v", githubIdentity)
	}

	feishuTests := []struct {
		name             string
		fixture          string
		trust            browserauth.CorporateEmailTrust
		wantSubject      string
		wantEmail        string
		wantVerified     bool
		wantUnauthorized bool
	}{
		{name: "trusted exact organization", fixture: "trusted", trust: browserauth.CorporateEmailTrust{Enabled: true, AllowedOrganizationIDs: []string{"tenant-feishu"}}, wantSubject: "union:union-feishu", wantEmail: "feishu@example.com", wantVerified: true},
		{name: "email trust disabled", fixture: "trusted", trust: browserauth.CorporateEmailTrust{AllowedOrganizationIDs: []string{"tenant-feishu"}}, wantSubject: "union:union-feishu", wantEmail: "feishu@example.com"},
		{name: "organization mismatch", fixture: "trusted", trust: browserauth.CorporateEmailTrust{Enabled: true, AllowedOrganizationIDs: []string{"another-tenant"}}, wantSubject: "union:union-feishu", wantEmail: "feishu@example.com"},
		{name: "missing email", fixture: "missing-email", trust: browserauth.CorporateEmailTrust{Enabled: true, AllowedOrganizationIDs: []string{"tenant-feishu"}}, wantSubject: "union:union-feishu"},
		{name: "fallback email", fixture: "fallback-email", trust: browserauth.CorporateEmailTrust{Enabled: true, AllowedOrganizationIDs: []string{"tenant-feishu"}}, wantSubject: "union:union-feishu", wantEmail: "fallback@example.com", wantVerified: true},
		{name: "application bound open id", fixture: "open-id", trust: browserauth.CorporateEmailTrust{Enabled: true, AllowedOrganizationIDs: []string{"tenant-feishu"}}, wantSubject: "open:feishu-client:open-feishu", wantEmail: "feishu@example.com", wantVerified: true},
		{name: "missing tenant key", fixture: "missing-tenant", trust: browserauth.CorporateEmailTrust{Enabled: true, AllowedOrganizationIDs: []string{"tenant-feishu"}}, wantUnauthorized: true},
	}
	for index, test := range feishuTests {
		t.Run("Feishu "+test.name, func(t *testing.T) {
			feishu, err := browserauth.NewFeishuProvider(browserauth.OAuthClientConfig{
				ClientID: "feishu-client", ClientSecret: secret, RedirectURL: "https://identity.example.test/feishu", HTTPClient: client,
			}, test.trust)
			if err != nil {
				t.Fatal(err)
			}
			if index == 0 {
				feishuAuthorization := assertAuthorizationURL(t, feishu, browserauth.AuthorizationRequest{State: state, CodeChallenge: challenge}, "accounts.feishu.cn", "/open-apis/authen/v1/authorize", map[string]string{"state": state, "code_challenge": challenge, "code_challenge_method": "S256", "scope": "contact:user.email:readonly"})
				feishuAuthorizationChallenge = feishuAuthorization.Query().Get("code_challenge")
			}
			identity, err := feishu.Exchange(context.Background(), browserauth.AuthorizationCallback{Code: test.fixture, CodeVerifier: verifier})
			if test.wantUnauthorized {
				if !errors.Is(err, browserauth.ErrUnauthorized) {
					t.Fatalf("missing tenant key was not rejected: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if identity.Issuer != "https://open.feishu.cn" || identity.Subject != test.wantSubject || identity.OrganizationID != "tenant-feishu" || identity.Email != test.wantEmail || identity.EmailVerified != test.wantVerified {
				t.Fatalf("unexpected Feishu identity: %#v", identity)
			}
		})
	}

	dingTalk, err := browserauth.NewDingTalkProvider(browserauth.OAuthClientConfig{
		ClientID: "dingtalk-client", ClientSecret: secret, RedirectURL: "https://identity.example.test/dingtalk", HTTPClient: client,
	}, browserauth.CorporateEmailTrust{Enabled: true, AllowedOrganizationIDs: []string{"corp-dingtalk"}})
	if err != nil {
		t.Fatal(err)
	}
	assertAuthorizationURL(t, dingTalk, browserauth.AuthorizationRequest{State: state}, "login.dingtalk.com", "/oauth2/auth", map[string]string{"state": state, "scope": "corpid openid"})
	dingTalkIdentity, err := dingTalk.Exchange(context.Background(), browserauth.AuthorizationCallback{Code: "authorization-code"})
	if err != nil {
		t.Fatal(err)
	}
	if dingTalkIdentity.Subject != "union:union-dingtalk" || dingTalkIdentity.OrganizationID != "corp-dingtalk" || !dingTalkIdentity.EmailVerified {
		t.Fatalf("unexpected DingTalk identity: %#v", dingTalkIdentity)
	}

	weCom, err := browserauth.NewWeComProvider(browserauth.WeComProviderConfig{
		CorpID: "corp-wecom", AgentID: "agent-wecom", ClientSecret: secret,
		RedirectURL: "https://identity.example.test/wecom", HTTPClient: client,
		EmailTrust: browserauth.CorporateEmailTrust{Enabled: true, AllowedOrganizationIDs: []string{"corp-wecom"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	weComAuthorization := assertAuthorizationURL(t, weCom, browserauth.AuthorizationRequest{State: state}, "login.work.weixin.qq.com", "/wwlogin/sso/login", map[string]string{"state": state, "appid": "corp-wecom", "agentid": "agent-wecom"})
	if _, present := weComAuthorization.Query()["scope"]; present {
		t.Fatal("WeCom authorization URL included an OAuth scope")
	}
	weComIdentity, err := weCom.Exchange(context.Background(), browserauth.AuthorizationCallback{Code: "authorization-code"})
	if err != nil {
		t.Fatal(err)
	}
	if weComIdentity.Issuer != "https://qyapi.weixin.qq.com/corp/corp-wecom" || weComIdentity.Subject != "wecom-user" || weComIdentity.OrganizationID != "corp-wecom" || !weComIdentity.EmailVerified {
		t.Fatalf("unexpected WeCom identity: %#v", weComIdentity)
	}
}

func TestFixedProviderScopesAreClosedAndCallerOwned(t *testing.T) {
	for _, test := range []struct {
		kind string
		want []string
	}{
		{kind: "github", want: []string{"read:user", "user:email"}},
		{kind: "feishu", want: []string{"contact:user.email:readonly"}},
		{kind: "dingtalk", want: []string{"corpid", "openid"}},
		{kind: "wecom", want: []string{}},
	} {
		t.Run(test.kind, func(t *testing.T) {
			first, fixed := browserauth.FixedProviderScopes(test.kind)
			if !fixed || first == nil || !slices.Equal(first, test.want) {
				t.Fatalf("FixedProviderScopes(%q) = %#v, %v", test.kind, first, fixed)
			}
			if len(first) > 0 {
				first[0] = "mutated"
				second, _ := browserauth.FixedProviderScopes(test.kind)
				if !slices.Equal(second, test.want) {
					t.Fatalf("caller mutation escaped: %#v", second)
				}
			}
		})
	}
	for _, kind := range []string{"oidc", "gitlab"} {
		if scopes, fixed := browserauth.FixedProviderScopes(kind); fixed || scopes != nil {
			t.Fatalf("FixedProviderScopes(%q) = %#v, %v", kind, scopes, fixed)
		}
	}
}

func TestOAuthProvidersFailClosedWithoutLeakingProviderSecrets(t *testing.T) {
	t.Parallel()
	const marker = "secret-marker-must-not-escape"
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusBadRequest)
		_, _ = writer.Write([]byte(`{"error":"` + marker + `"}`))
	}))
	defer server.Close()
	client := &http.Client{Transport: &rewriteTransport{target: mustURL(t, server.URL), base: server.Client().Transport}}
	provider, err := browserauth.NewFeishuProvider(browserauth.OAuthClientConfig{
		ClientID: "client", ClientSecret: marker, RedirectURL: "https://identity.example.test/callback", HTTPClient: client,
	}, browserauth.CorporateEmailTrust{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = provider.Exchange(context.Background(), browserauth.AuthorizationCallback{Code: marker, CodeVerifier: newProof(t)})
	if !errors.Is(err, browserauth.ErrUnauthorized) || strings.Contains(err.Error(), marker) {
		t.Fatalf("provider failure was not closed and redacted: %v", err)
	}

	if _, err := browserauth.NewGitHubProvider(browserauth.OAuthClientConfig{ClientID: "client", ClientSecret: marker, RedirectURL: "http://identity.example.test/callback"}); err == nil || strings.Contains(err.Error(), marker) {
		t.Fatalf("insecure callback was accepted or leaked configuration: %v", err)
	}
	if _, err := browserauth.NewDingTalkProvider(browserauth.OAuthClientConfig{ClientID: "client", ClientSecret: marker, RedirectURL: "https://identity.example.test/callback"}, browserauth.CorporateEmailTrust{Enabled: true}); err == nil {
		t.Fatal("email trust without an organization allowlist was accepted")
	}
}

type rewriteTransport struct {
	target *url.URL
	base   http.RoundTripper
}

func (transport *rewriteTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	clone := request.Clone(request.Context())
	copyURL := *request.URL
	clone.URL = &copyURL
	clone.Host = request.URL.Host
	clone.URL.Scheme = transport.target.Scheme
	clone.URL.Host = transport.target.Host
	return transport.base.RoundTrip(clone)
}

func mustURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

func assertAuthorizationURL(t *testing.T, provider browserauth.LoginProvider, request browserauth.AuthorizationRequest, host, path string, values map[string]string) *url.URL {
	t.Helper()
	raw, err := provider.AuthorizationURL(request)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Scheme != "https" || parsed.Host != host || parsed.Path != path {
		t.Fatalf("unexpected authorization endpoint: %s", raw)
	}
	for name, value := range values {
		if parsed.Query().Get(name) != value {
			t.Fatalf("authorization parameter %s: got %q, want %q", name, parsed.Query().Get(name), value)
		}
	}
	return parsed
}

func requireHeader(t *testing.T, request *http.Request, name, value string) {
	t.Helper()
	if request.Header.Get(name) != value {
		t.Errorf("header %s: got %q, want %q", name, request.Header.Get(name), value)
	}
}

func decodeJSON(t *testing.T, request *http.Request, target any) {
	t.Helper()
	defer request.Body.Close()
	if err := json.NewDecoder(request.Body).Decode(target); err != nil {
		t.Error(err)
	}
}
