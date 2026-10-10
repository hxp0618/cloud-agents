package browserauth

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"golang.org/x/oauth2"
)

const (
	githubIssuer       = "https://github.com"
	githubAuthorizeURL = "https://github.com/login/oauth/authorize"
	githubTokenURL     = "https://github.com/login/oauth/access_token"
	githubUserURL      = "https://api.github.com/user"
	githubEmailsURL    = "https://api.github.com/user/emails"

	feishuIssuer       = "https://open.feishu.cn"
	feishuAuthorizeURL = "https://accounts.feishu.cn/open-apis/authen/v1/authorize"
	// Feishu documents PKCE on the v1 authorization endpoint as compatible
	// with the v2 token endpoint: https://open.feishu.cn/document/common-capabilities/sso/api/obtain-oauth-code.md
	feishuTokenURL = "https://open.feishu.cn/open-apis/authen/v2/oauth/token"
	feishuUserURL  = "https://open.feishu.cn/open-apis/authen/v1/user_info"

	dingTalkIssuer       = "https://login.dingtalk.com"
	dingTalkAuthorizeURL = "https://login.dingtalk.com/oauth2/auth"
	dingTalkTokenURL     = "https://api.dingtalk.com/v1.0/oauth2/userAccessToken"
	dingTalkUserURL      = "https://api.dingtalk.com/v1.0/contact/users/me"
)

type OAuthClientConfig struct {
	ClientID     string
	ClientSecret string
	RedirectURL  string
	HTTPClient   *http.Client
}

type CorporateEmailTrust struct {
	Enabled                bool
	AllowedOrganizationIDs []string
}

// FixedProviderScopes returns the complete immutable scope policy for a
// non-OIDC provider. The returned slice is owned by the caller.
func FixedProviderScopes(kind string) ([]string, bool) {
	switch kind {
	case "github":
		return []string{"read:user", "user:email"}, true
	case "feishu":
		return []string{"contact:user.email:readonly"}, true
	case "dingtalk":
		return []string{"corpid", "openid"}, true
	case "wecom":
		return []string{}, true
	default:
		return nil, false
	}
}

type oauthProvider struct {
	kind       string
	clientID   string
	secret     string
	redirect   string
	httpClient *http.Client
	trust      CorporateEmailTrust
	oauth2     oauth2.Config
}

func NewGitHubProvider(config OAuthClientConfig) (LoginProvider, error) {
	provider, err := newOAuthProvider("github", config, CorporateEmailTrust{})
	if err != nil {
		return nil, err
	}
	scopes, _ := FixedProviderScopes("github")
	provider.oauth2 = oauth2.Config{
		ClientID: config.ClientID, ClientSecret: config.ClientSecret, RedirectURL: config.RedirectURL,
		Endpoint: oauth2.Endpoint{AuthURL: githubAuthorizeURL, TokenURL: githubTokenURL, AuthStyle: oauth2.AuthStyleInParams},
		Scopes:   scopes,
	}
	return provider, nil
}

func NewFeishuProvider(config OAuthClientConfig, trust CorporateEmailTrust) (LoginProvider, error) {
	return newOAuthProvider("feishu", config, trust)
}

func NewDingTalkProvider(config OAuthClientConfig, trust CorporateEmailTrust) (LoginProvider, error) {
	return newOAuthProvider("dingtalk", config, trust)
}

func newOAuthProvider(kind string, config OAuthClientConfig, trust CorporateEmailTrust) (*oauthProvider, error) {
	if !validProviderIdentifier(config.ClientID) || config.ClientSecret == "" || len(config.ClientSecret) > 4096 || !validHTTPSURL(config.RedirectURL, false) || !validCorporateTrust(trust) {
		return nil, errInvalidProvider
	}
	client, err := newProviderHTTPClient(config.HTTPClient)
	if err != nil {
		return nil, err
	}
	return &oauthProvider{kind: kind, clientID: config.ClientID, secret: config.ClientSecret, redirect: config.RedirectURL, httpClient: client, trust: trust}, nil
}

func (provider *oauthProvider) AuthorizationURL(request AuthorizationRequest) (string, error) {
	if !validProof(request.State) {
		return "", errInvalidProviderRequest
	}
	values := url.Values{"client_id": {provider.clientID}, "redirect_uri": {provider.redirect}, "response_type": {"code"}, "state": {request.State}}
	switch provider.kind {
	case "github":
		if !validProof(request.CodeChallenge) {
			return "", errInvalidProviderRequest
		}
		return provider.oauth2.AuthCodeURL(request.State, oauth2.SetAuthURLParam("code_challenge", request.CodeChallenge), oauth2.SetAuthURLParam("code_challenge_method", "S256")), nil
	case "feishu":
		if !validProof(request.CodeChallenge) {
			return "", errInvalidProviderRequest
		}
		scopes, _ := FixedProviderScopes(provider.kind)
		values.Set("scope", strings.Join(scopes, " "))
		values.Set("code_challenge", request.CodeChallenge)
		values.Set("code_challenge_method", "S256")
		return queryURL(feishuAuthorizeURL, values), nil
	case "dingtalk":
		scopes, _ := FixedProviderScopes(provider.kind)
		values.Set("scope", strings.Join(scopes, " "))
		values.Set("prompt", "consent")
		return queryURL(dingTalkAuthorizeURL, values), nil
	default:
		return "", errInvalidProvider
	}
}

func (provider *oauthProvider) Exchange(ctx context.Context, callback AuthorizationCallback) (ProviderIdentity, error) {
	if ctx == nil || !validAuthorizationCode(callback.Code) {
		return ProviderIdentity{}, ErrUnauthorized
	}
	switch provider.kind {
	case "github":
		return provider.exchangeGitHub(ctx, callback)
	case "feishu":
		return provider.exchangeFeishu(ctx, callback)
	case "dingtalk":
		return provider.exchangeDingTalk(ctx, callback)
	default:
		return ProviderIdentity{}, ErrUnauthorized
	}
}

func (provider *oauthProvider) exchangeGitHub(ctx context.Context, callback AuthorizationCallback) (ProviderIdentity, error) {
	if !validProof(callback.CodeVerifier) {
		return ProviderIdentity{}, ErrUnauthorized
	}
	ctx = context.WithValue(ctx, oauth2.HTTPClient, provider.httpClient)
	token, err := provider.oauth2.Exchange(ctx, callback.Code, oauth2.VerifierOption(callback.CodeVerifier))
	if err != nil || token.AccessToken == "" {
		return ProviderIdentity{}, ErrUnauthorized
	}
	headers := providerBearerHeaders(token.AccessToken)
	headers.Set("X-GitHub-Api-Version", "2022-11-28")
	headers.Set("User-Agent", "cloud-agents-identity")
	var user struct {
		ID    int64  `json:"id"`
		Email string `json:"email"`
	}
	if err := getProviderJSON(ctx, provider.httpClient, githubUserURL, headers, &user); err != nil || user.ID <= 0 {
		return ProviderIdentity{}, ErrUnauthorized
	}
	email, emailVerified := providerEmail(user.Email, false)
	verifiedEmail := ""
	var emails []struct {
		Email    string `json:"email"`
		Primary  bool   `json:"primary"`
		Verified bool   `json:"verified"`
	}
	if err := getProviderJSON(ctx, provider.httpClient, githubEmailsURL, headers, &emails); err == nil {
		for _, candidate := range emails {
			candidateEmail, verified := providerEmail(candidate.Email, candidate.Verified)
			if !verified {
				continue
			}
			if verifiedEmail == "" || candidate.Primary {
				verifiedEmail = candidateEmail
			}
			if candidate.Primary {
				break
			}
		}
	}
	if verifiedEmail != "" {
		email = verifiedEmail
		emailVerified = true
	}
	return ProviderIdentity{Issuer: githubIssuer, Subject: strconv.FormatInt(user.ID, 10), Email: email, EmailVerified: emailVerified}, nil
}

func (provider *oauthProvider) exchangeFeishu(ctx context.Context, callback AuthorizationCallback) (ProviderIdentity, error) {
	if !validProof(callback.CodeVerifier) {
		return ProviderIdentity{}, ErrUnauthorized
	}
	var token struct {
		Code        int    `json:"code"`
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
		Error       string `json:"error"`
	}
	if err := postProviderJSON(ctx, provider.httpClient, feishuTokenURL, map[string]string{
		"grant_type": "authorization_code", "client_id": provider.clientID, "client_secret": provider.secret,
		"code": callback.Code, "redirect_uri": provider.redirect, "code_verifier": callback.CodeVerifier,
	}, &token); err != nil || token.Code != 0 || token.AccessToken == "" || token.Error != "" {
		return ProviderIdentity{}, ErrUnauthorized
	}
	var user struct {
		Code int `json:"code"`
		Data struct {
			OpenID          string `json:"open_id"`
			UnionID         string `json:"union_id"`
			Email           string `json:"email"`
			EnterpriseEmail string `json:"enterprise_email"`
			TenantKey       string `json:"tenant_key"`
		} `json:"data"`
	}
	if err := getProviderJSON(ctx, provider.httpClient, feishuUserURL, providerBearerHeaders(token.AccessToken), &user); err != nil || user.Code != 0 || !validProviderIdentifier(user.Data.TenantKey) {
		return ProviderIdentity{}, ErrUnauthorized
	}
	subject := user.Data.UnionID
	if subject != "" {
		subject = "union:" + subject
	} else {
		subject = "open:" + provider.clientID + ":" + user.Data.OpenID
	}
	if !validProviderIdentifier(subject) {
		return ProviderIdentity{}, ErrUnauthorized
	}
	email := user.Data.EnterpriseEmail
	if email == "" {
		email = user.Data.Email
	}
	email, verified := providerEmail(email, provider.trust.trusts(user.Data.TenantKey))
	return ProviderIdentity{Issuer: feishuIssuer, Subject: subject, Email: email, EmailVerified: verified, OrganizationID: user.Data.TenantKey}, nil
}

func (provider *oauthProvider) exchangeDingTalk(ctx context.Context, callback AuthorizationCallback) (ProviderIdentity, error) {
	var token struct {
		AccessToken string `json:"accessToken"`
		CorpID      string `json:"corpId"`
	}
	if err := postProviderJSON(ctx, provider.httpClient, dingTalkTokenURL, map[string]string{
		"clientId": provider.clientID, "clientSecret": provider.secret, "code": callback.Code, "grantType": "authorization_code",
	}, &token); err != nil || token.AccessToken == "" || !validProviderIdentifier(token.CorpID) {
		return ProviderIdentity{}, ErrUnauthorized
	}
	headers := make(http.Header)
	headers.Set("x-acs-dingtalk-access-token", token.AccessToken)
	var user struct {
		OpenID  string `json:"openId"`
		UnionID string `json:"unionId"`
		Email   string `json:"email"`
	}
	if err := getProviderJSON(ctx, provider.httpClient, dingTalkUserURL, headers, &user); err != nil {
		return ProviderIdentity{}, ErrUnauthorized
	}
	subject := user.UnionID
	if subject != "" {
		subject = "union:" + subject
	} else {
		subject = "open:" + provider.clientID + ":" + user.OpenID
	}
	if !validProviderIdentifier(subject) {
		return ProviderIdentity{}, ErrUnauthorized
	}
	email, verified := providerEmail(user.Email, provider.trust.trusts(token.CorpID))
	return ProviderIdentity{Issuer: dingTalkIssuer, Subject: subject, Email: email, EmailVerified: verified, OrganizationID: token.CorpID}, nil
}

func providerBearerHeaders(accessToken string) http.Header {
	headers := make(http.Header)
	headers.Set("Authorization", "Bearer "+accessToken)
	return headers
}

func validCorporateTrust(trust CorporateEmailTrust) bool {
	if len(trust.AllowedOrganizationIDs) > 32 || trust.Enabled && len(trust.AllowedOrganizationIDs) == 0 {
		return false
	}
	seen := make(map[string]struct{}, len(trust.AllowedOrganizationIDs))
	for _, organizationID := range trust.AllowedOrganizationIDs {
		if !validProviderIdentifier(organizationID) || len(organizationID) > 255 {
			return false
		}
		if _, duplicate := seen[organizationID]; duplicate {
			return false
		}
		seen[organizationID] = struct{}{}
	}
	return true
}

func (trust CorporateEmailTrust) trusts(organizationID string) bool {
	if !trust.Enabled {
		return false
	}
	for _, allowed := range trust.AllowedOrganizationIDs {
		if allowed == organizationID {
			return true
		}
	}
	return false
}
