package identity

import (
	"context"
	"errors"
	"net/http"
	"net/url"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/browserauth"
)

const (
	githubProviderIssuer   = "https://github.com"
	feishuProviderIssuer   = "https://open.feishu.cn"
	dingTalkProviderIssuer = "https://login.dingtalk.com"
)

// ProviderMaterialResolver maps operator-controlled references to process-local
// material. Neither the references nor resolved client secrets come from a
// browser request.
type ProviderMaterialResolver interface {
	ClientSecret(string) (string, error)
	HTTPClient(string) (*http.Client, error)
}

type RuntimeProviderFactory struct{ materials ProviderMaterialResolver }

func NewRuntimeProviderFactory(materials ProviderMaterialResolver) (*RuntimeProviderFactory, error) {
	if materials == nil {
		return nil, errInvalidConfiguration
	}
	return &RuntimeProviderFactory{materials: materials}, nil
}

func (factory *RuntimeProviderFactory) Provider(ctx context.Context, client ProviderClientSnapshot) (browserauth.LoginProvider, error) {
	if ctx == nil || !validProviderClientSnapshot(client) {
		return nil, errInvalidConfiguration
	}
	secret, err := factory.materials.ClientSecret(client.SecretRef)
	if err != nil || secret == "" {
		return nil, errors.New("provider client material unavailable")
	}
	httpClient, err := factory.materials.HTTPClient(client.RootCARef)
	if err != nil || httpClient == nil {
		return nil, errors.New("provider root authority unavailable")
	}
	trust := browserauth.CorporateEmailTrust{
		Enabled: client.TrustProviderEmail, AllowedOrganizationIDs: append([]string(nil), client.AllowedOrganizationIDs...),
	}
	switch client.Kind {
	case "oidc":
		return browserauth.NewOIDCProvider(ctx, browserauth.OIDCProviderConfig{
			Issuer: client.Issuer, ClientID: client.ClientID, ClientSecret: secret,
			RedirectURL: client.RedirectURL, Scopes: append([]string(nil), client.Scopes...), HTTPClient: httpClient,
		})
	case "gitlab":
		return browserauth.NewGitLabProvider(ctx, browserauth.OIDCProviderConfig{
			Issuer: client.Issuer, ClientID: client.ClientID, ClientSecret: secret,
			RedirectURL: client.RedirectURL, Scopes: append([]string(nil), client.Scopes...), HTTPClient: httpClient,
		})
	case "github":
		if client.Issuer != githubProviderIssuer {
			return nil, errInvalidConfiguration
		}
		return browserauth.NewGitHubProvider(browserauth.OAuthClientConfig{
			ClientID: client.ClientID, ClientSecret: secret, RedirectURL: client.RedirectURL, HTTPClient: httpClient,
		})
	case "feishu":
		if client.Issuer != feishuProviderIssuer {
			return nil, errInvalidConfiguration
		}
		return browserauth.NewFeishuProvider(browserauth.OAuthClientConfig{
			ClientID: client.ClientID, ClientSecret: secret, RedirectURL: client.RedirectURL, HTTPClient: httpClient,
		}, trust)
	case "dingtalk":
		if client.Issuer != dingTalkProviderIssuer {
			return nil, errInvalidConfiguration
		}
		return browserauth.NewDingTalkProvider(browserauth.OAuthClientConfig{
			ClientID: client.ClientID, ClientSecret: secret, RedirectURL: client.RedirectURL, HTTPClient: httpClient,
		}, trust)
	case "wecom":
		expectedIssuer := "https://qyapi.weixin.qq.com/corp/" + url.PathEscape(client.ClientID)
		if client.Issuer != expectedIssuer || client.AgentID == "" {
			return nil, errInvalidConfiguration
		}
		return browserauth.NewWeComProvider(browserauth.WeComProviderConfig{
			CorpID: client.ClientID, AgentID: client.AgentID, ClientSecret: secret,
			RedirectURL: client.RedirectURL, EmailTrust: trust, HTTPClient: httpClient,
		})
	default:
		return nil, errInvalidConfiguration
	}
}
