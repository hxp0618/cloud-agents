package browserauth

import (
	"context"
	"net/http"
	"net/url"
	"strings"
)

const (
	weComAuthorizeURL = "https://login.work.weixin.qq.com/wwlogin/sso/login"
	weComTokenURL     = "https://qyapi.weixin.qq.com/cgi-bin/gettoken"
	weComIdentityURL  = "https://qyapi.weixin.qq.com/cgi-bin/auth/getuserinfo"
	weComUserURL      = "https://qyapi.weixin.qq.com/cgi-bin/user/get"
)

type WeComProviderConfig struct {
	CorpID       string
	AgentID      string
	ClientSecret string
	RedirectURL  string
	EmailTrust   CorporateEmailTrust
	HTTPClient   *http.Client
}

type weComProvider struct {
	corpID     string
	agentID    string
	secret     string
	redirect   string
	issuer     string
	trust      CorporateEmailTrust
	httpClient *http.Client
}

func NewWeComProvider(config WeComProviderConfig) (LoginProvider, error) {
	if !validProviderIdentifier(config.CorpID) || len(config.CorpID) > 255 || !validProviderIdentifier(config.AgentID) || len(config.AgentID) > 255 || config.ClientSecret == "" || len(config.ClientSecret) > 4096 || !validHTTPSURL(config.RedirectURL, false) || !validCorporateTrust(config.EmailTrust) {
		return nil, errInvalidProvider
	}
	client, err := newProviderHTTPClient(config.HTTPClient)
	if err != nil {
		return nil, err
	}
	return &weComProvider{
		corpID: config.CorpID, agentID: config.AgentID, secret: config.ClientSecret,
		redirect: config.RedirectURL, issuer: "https://qyapi.weixin.qq.com/corp/" + url.PathEscape(config.CorpID),
		trust: config.EmailTrust, httpClient: client,
	}, nil
}

func (provider *weComProvider) AuthorizationURL(request AuthorizationRequest) (string, error) {
	if !validProof(request.State) {
		return "", errInvalidProviderRequest
	}
	return queryURL(weComAuthorizeURL, url.Values{
		"login_type": {"CorpApp"}, "appid": {provider.corpID}, "agentid": {provider.agentID},
		"redirect_uri": {provider.redirect}, "state": {request.State},
	}), nil
}

func (provider *weComProvider) Exchange(ctx context.Context, callback AuthorizationCallback) (ProviderIdentity, error) {
	if ctx == nil || !validAuthorizationCode(callback.Code) {
		return ProviderIdentity{}, ErrUnauthorized
	}
	var token struct {
		ErrorCode   int    `json:"errcode"`
		AccessToken string `json:"access_token"`
	}
	if err := getProviderJSON(ctx, provider.httpClient, queryURL(weComTokenURL, url.Values{"corpid": {provider.corpID}, "corpsecret": {provider.secret}}), nil, &token); err != nil || token.ErrorCode != 0 || token.AccessToken == "" {
		return ProviderIdentity{}, ErrUnauthorized
	}
	var identity struct {
		ErrorCode int    `json:"errcode"`
		UserID    string `json:"userid"`
	}
	if err := getProviderJSON(ctx, provider.httpClient, queryURL(weComIdentityURL, url.Values{"access_token": {token.AccessToken}, "code": {callback.Code}}), nil, &identity); err != nil || identity.ErrorCode != 0 || !validProviderIdentifier(identity.UserID) || strings.Contains(identity.UserID, "/") {
		return ProviderIdentity{}, ErrUnauthorized
	}
	var user struct {
		ErrorCode int    `json:"errcode"`
		UserID    string `json:"userid"`
		Email     string `json:"email"`
		Business  string `json:"biz_mail"`
	}
	if err := getProviderJSON(ctx, provider.httpClient, queryURL(weComUserURL, url.Values{"access_token": {token.AccessToken}, "userid": {identity.UserID}}), nil, &user); err != nil || user.ErrorCode != 0 || user.UserID != identity.UserID {
		return ProviderIdentity{}, ErrUnauthorized
	}
	email := user.Business
	if email == "" {
		email = user.Email
	}
	email, verified := providerEmail(email, provider.trust.trusts(provider.corpID))
	return ProviderIdentity{
		Issuer: provider.issuer, Subject: identity.UserID, Email: email,
		EmailVerified: verified, OrganizationID: provider.corpID,
	}, nil
}
