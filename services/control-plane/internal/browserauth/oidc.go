package browserauth

import (
	"context"
	"errors"
	"net/http"
	"net/url"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

var ErrUnauthorized = errors.New("browser auth unauthorized")

type AuthorizationRequest struct {
	State         string
	Nonce         string
	CodeChallenge string
}

type AuthorizationCallback struct {
	Code         string
	CodeVerifier string
	Nonce        string
}

// ProviderIdentity is an authenticated provider subject. Email is an optional
// assertion and remains separate from the stable issuer/subject identity.
type ProviderIdentity struct {
	Issuer         string
	Subject        string
	Email          string
	EmailVerified  bool
	OrganizationID string
}

type LoginProvider interface {
	AuthorizationURL(AuthorizationRequest) (string, error)
	Exchange(context.Context, AuthorizationCallback) (ProviderIdentity, error)
}

type OIDCProviderConfig struct {
	Issuer       string
	ClientID     string
	ClientSecret string
	RedirectURL  string
	Scopes       []string
	HTTPClient   *http.Client
}

type oidcProvider struct {
	issuer     string
	oauth2     oauth2.Config
	verifier   *oidc.IDTokenVerifier
	httpClient *http.Client
}

func NewOIDCProvider(ctx context.Context, config OIDCProviderConfig) (LoginProvider, error) {
	if ctx == nil || !validHTTPSURL(config.Issuer, true) || config.ClientID == "" || !validHTTPSURL(config.RedirectURL, false) || !validScopes(config.Scopes) {
		return nil, errInvalidProvider
	}
	httpClient, err := newProviderHTTPClient(config.HTTPClient)
	if err != nil {
		return nil, err
	}
	provider, err := oidc.NewProvider(oidc.ClientContext(ctx, httpClient), config.Issuer)
	if err != nil {
		return nil, errors.New("OIDC discovery failed")
	}
	var metadata struct {
		Issuer                string `json:"issuer"`
		AuthorizationEndpoint string `json:"authorization_endpoint"`
		TokenEndpoint         string `json:"token_endpoint"`
		JWKSURI               string `json:"jwks_uri"`
	}
	if err := provider.Claims(&metadata); err != nil || metadata.Issuer != config.Issuer || !validHTTPSURL(metadata.AuthorizationEndpoint, false) || !validHTTPSURL(metadata.TokenEndpoint, false) || !validHTTPSURL(metadata.JWKSURI, false) {
		return nil, errors.New("OIDC discovery failed")
	}
	scopes := uniqueScopes(append([]string{oidc.ScopeOpenID, "email", "profile"}, config.Scopes...))
	return &oidcProvider{
		issuer: config.Issuer,
		oauth2: oauth2.Config{
			ClientID: config.ClientID, ClientSecret: config.ClientSecret,
			Endpoint: provider.Endpoint(), RedirectURL: config.RedirectURL, Scopes: scopes,
		},
		verifier:   provider.Verifier(&oidc.Config{ClientID: config.ClientID}),
		httpClient: httpClient,
	}, nil
}

// NewGitLabProvider uses GitLab's standard OIDC discovery contract. Issuer may
// be gitlab.com or the fixed HTTPS origin of a self-managed GitLab instance.
func NewGitLabProvider(ctx context.Context, config OIDCProviderConfig) (LoginProvider, error) {
	return NewOIDCProvider(ctx, config)
}

func (provider *oidcProvider) AuthorizationURL(request AuthorizationRequest) (string, error) {
	if !validProof(request.State) || !validProof(request.Nonce) || !validProof(request.CodeChallenge) {
		return "", errInvalidProviderRequest
	}
	return provider.oauth2.AuthCodeURL(
		request.State,
		oauth2.SetAuthURLParam("nonce", request.Nonce),
		oauth2.SetAuthURLParam("code_challenge", request.CodeChallenge),
		oauth2.SetAuthURLParam("code_challenge_method", "S256"),
	), nil
}

func (provider *oidcProvider) Exchange(ctx context.Context, callback AuthorizationCallback) (ProviderIdentity, error) {
	if ctx == nil || !validAuthorizationCode(callback.Code) || !validProof(callback.CodeVerifier) || !validProof(callback.Nonce) {
		return ProviderIdentity{}, ErrUnauthorized
	}
	ctx = oidc.ClientContext(ctx, provider.httpClient)
	token, err := provider.oauth2.Exchange(ctx, callback.Code, oauth2.VerifierOption(callback.CodeVerifier))
	if err != nil {
		return ProviderIdentity{}, ErrUnauthorized
	}
	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok || rawIDToken == "" {
		return ProviderIdentity{}, ErrUnauthorized
	}
	idToken, err := provider.verifier.Verify(ctx, rawIDToken)
	if err != nil || idToken.Issuer != provider.issuer || idToken.Nonce != callback.Nonce || !validProviderIdentifier(idToken.Subject) {
		return ProviderIdentity{}, ErrUnauthorized
	}
	var claims struct {
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
	}
	if err := idToken.Claims(&claims); err != nil {
		return ProviderIdentity{}, ErrUnauthorized
	}
	email, verified := providerEmail(claims.Email, claims.EmailVerified)
	return ProviderIdentity{Issuer: provider.issuer, Subject: idToken.Subject, Email: email, EmailVerified: verified}, nil
}

func validHTTPSURL(raw string, issuer bool) bool {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" || parsed.RawQuery != "" || parsed.String() != raw {
		return false
	}
	if issuer && parsed.Path != "" && parsed.Path != "/" && parsed.Path[len(parsed.Path)-1] == '/' {
		return false
	}
	return true
}
