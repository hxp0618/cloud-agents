package browserauth

import (
	"context"
	"errors"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

type OIDCProviderConfig struct {
	ID           string
	Name         string
	Issuer       string
	ClientID     string
	ClientSecret string
	RedirectURL  string
	Scopes       []string
}

type oidcIdentity struct {
	Issuer  string
	Subject string
	Email   string
}

type loginProvider interface {
	authorizationURL(state, nonce, challenge string) string
	exchange(context.Context, string, string, string) (oidcIdentity, error)
}

type discoveredProvider struct {
	issuer   string
	oauth2   oauth2.Config
	verifier *oidc.IDTokenVerifier
}

func discoverProvider(ctx context.Context, config OIDCProviderConfig) (*discoveredProvider, error) {
	if config.ID == "" || config.Name == "" || config.Issuer == "" || config.ClientID == "" || config.RedirectURL == "" {
		return nil, errors.New("invalid OIDC provider")
	}
	provider, err := oidc.NewProvider(ctx, config.Issuer)
	if err != nil {
		return nil, errors.New("OIDC discovery failed")
	}
	scopes := append([]string{oidc.ScopeOpenID, "email", "profile"}, config.Scopes...)
	return &discoveredProvider{
		issuer: config.Issuer,
		oauth2: oauth2.Config{ClientID: config.ClientID, ClientSecret: config.ClientSecret, Endpoint: provider.Endpoint(), RedirectURL: config.RedirectURL, Scopes: scopes},
		verifier: provider.Verifier(&oidc.Config{ClientID: config.ClientID}),
	}, nil
}

func (provider *discoveredProvider) authorizationURL(state, nonce, challenge string) string {
	return provider.oauth2.AuthCodeURL(state, oauth2.AccessTypeOffline, oauth2.SetAuthURLParam("nonce", nonce), oauth2.SetAuthURLParam("code_challenge", challenge), oauth2.SetAuthURLParam("code_challenge_method", "S256"))
}

func (provider *discoveredProvider) exchange(ctx context.Context, code, codeVerifier, nonce string) (oidcIdentity, error) {
	token, err := provider.oauth2.Exchange(ctx, code, oauth2.SetAuthURLParam("code_verifier", codeVerifier))
	if err != nil {
		return oidcIdentity{}, ErrUnauthorized
	}
	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok || rawIDToken == "" {
		return oidcIdentity{}, ErrUnauthorized
	}
	idToken, err := provider.verifier.Verify(ctx, rawIDToken)
	if err != nil || idToken.Nonce != nonce || idToken.Subject == "" {
		return oidcIdentity{}, ErrUnauthorized
	}
	var claims struct {
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
	}
	if err := idToken.Claims(&claims); err != nil || !claims.EmailVerified || claims.Email == "" {
		return oidcIdentity{}, ErrUnauthorized
	}
	return oidcIdentity{Issuer: idToken.Issuer, Subject: idToken.Subject, Email: claims.Email}, nil
}
