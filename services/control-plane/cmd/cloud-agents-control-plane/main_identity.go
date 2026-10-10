//go:build !localdev

package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	common "github.com/hxp0618/cloud-agents/sdk/go/gen/common/v1alpha1"
	api "github.com/hxp0618/cloud-agents/sdk/go/gen/openapi/v1alpha1"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authn"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authz"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/browserauth"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/identityaccess"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/identitytrust"
)

type productionIdentityConfig struct {
	Issuer                     string `json:"issuer"`
	UserAudience               string `json:"userAudience"`
	AdminAudience              string `json:"adminAudience"`
	IdentityBaseURL            string `json:"identityBaseUrl"`
	CAFile                     string `json:"caFile,omitempty"`
	ControlPlaneCredentialFile string `json:"controlPlaneCredentialFile"`
	IdentityCredentialFile     string `json:"identityCredentialFile"`
}

type productionIdentityAuthentication struct {
	mu                      sync.Mutex
	closed                  bool
	lastUnknownKeyRefresh   time.Time
	adapter                 *identitytrust.Adapter
	user, admin             *authn.ConfiguredVerifier
	userAccess, adminAccess *identityaccess.Verifier
	authorizationCredential string
	transport               *http.Transport
}

func loadProductionIdentityAuthentication(ctx context.Context, path string, checkpoint identitytrust.Checkpoint) (*productionIdentityAuthentication, error) {
	data, err := readProductionFile(path, maxAuthConfigBytes)
	if err != nil {
		return nil, errors.New("identity authentication configuration is unavailable")
	}
	if _, err := common.DecodeStrictObject(data,
		[]string{"issuer", "userAudience", "adminAudience", "identityBaseUrl", "caFile", "controlPlaneCredentialFile", "identityCredentialFile"},
		[]string{"issuer", "userAudience", "adminAudience", "identityBaseUrl", "controlPlaneCredentialFile", "identityCredentialFile"}); err != nil {
		return nil, errors.New("identity authentication configuration is invalid")
	}
	var config productionIdentityConfig
	if json.Unmarshal(data, &config) != nil || config.AdminAudience == config.UserAudience {
		return nil, errors.New("identity authentication configuration is invalid")
	}
	for _, value := range []string{config.Issuer, config.UserAudience, config.AdminAudience} {
		if (authz.SubjectRef{Kind: "user", Issuer: value, Subject: "user-validation"}).Validate() != nil {
			return nil, errors.New("identity authentication authority is invalid")
		}
	}
	endpoint, err := url.Parse(config.IdentityBaseURL)
	if err != nil || endpoint.Scheme != "https" || endpoint.Host == "" || endpoint.User != nil || endpoint.Fragment != "" ||
		endpoint.RawQuery != "" || endpoint.ForceQuery || endpoint.RawPath != "" || endpoint.Path != "" && endpoint.Path != "/" || strings.TrimSpace(config.IdentityBaseURL) != config.IdentityBaseURL {
		return nil, errors.New("identity endpoint must be a fixed HTTPS origin")
	}
	controlPlaneCredential, err := readProductionIdentityCredential(config.ControlPlaneCredentialFile)
	if err != nil {
		return nil, err
	}
	identityCredential, err := readProductionIdentityCredential(config.IdentityCredentialFile)
	if err != nil || identityCredential == controlPlaneCredential {
		return nil, errors.New("identity service credentials must be distinct valid secret references")
	}
	var roots *x509.CertPool
	if config.CAFile != "" {
		roots, err = readProductionCAPool(config.CAFile)
		if err != nil {
			return nil, errors.New("identity CA configuration is invalid")
		}
	}
	baseURL := strings.TrimSuffix(config.IdentityBaseURL, "/")
	adapter, err := identitytrust.New(identitytrust.Config{Issuer: config.Issuer, UserAudience: config.UserAudience,
		AdminAudience: config.AdminAudience, JWKSURL: baseURL + "/.well-known/jwks.json", Clock: time.Now,
		Checkpoint: checkpoint, Fetch: identitytrust.NewHTTPFetcher(roots)})
	if err != nil {
		return nil, errors.New("identity trust configuration is invalid")
	}
	pair, err := adapter.Start(ctx)
	if err != nil {
		return nil, errors.New("identity trust initialization failed")
	}
	user, err := authn.NewConfiguredVerifier(pair.User)
	if err != nil {
		return nil, errors.New("identity User verifier initialization failed")
	}
	admin, err := authn.NewConfiguredVerifier(pair.Admin)
	if err != nil {
		user.Invalidate()
		return nil, errors.New("identity Admin verifier initialization failed")
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots}
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	status, err := api.NewIdentityServiceHTTPClientWithClient(baseURL, controlPlaneCredential, client)
	if err != nil {
		user.Invalidate()
		admin.Invalidate()
		transport.CloseIdleConnections()
		return nil, errors.New("identity status client configuration is invalid")
	}
	authentication := &productionIdentityAuthentication{adapter: adapter, user: user, admin: admin,
		authorizationCredential: identityCredential, transport: transport}
	userAccess, err := identityaccess.NewVerifier(&productionRefreshingVerifier{authentication, user}, status, api.IdentityApplicationUser)
	if err != nil {
		authentication.close()
		return nil, err
	}
	adminAccess, err := identityaccess.NewVerifier(&productionRefreshingVerifier{authentication, admin}, status, api.IdentityApplicationAdmin)
	if err != nil {
		authentication.close()
		return nil, err
	}
	authentication.userAccess, authentication.adminAccess = userAccess, adminAccess
	return authentication, nil
}

func readProductionIdentityCredential(path string) (string, error) {
	if path == "" || strings.TrimSpace(path) != path {
		return "", errors.New("identity credential reference is invalid")
	}
	data, err := readProductionFile(path, 128)
	if err != nil {
		return "", errors.New("identity credential is unavailable")
	}
	value := strings.TrimSuffix(string(data), "\n")
	if _, err := browserauth.ProofDigest(value); err != nil {
		return "", errors.New("identity credential is invalid")
	}
	return value, nil
}

func (authentication *productionIdentityAuthentication) ready() bool {
	return authentication.user.Ready() && authentication.admin.Ready()
}

func (authentication *productionIdentityAuthentication) close() {
	authentication.mu.Lock()
	defer authentication.mu.Unlock()
	authentication.closeLocked()
}

func (authentication *productionIdentityAuthentication) closeLocked() {
	authentication.closed = true
	authentication.user.InvalidateTogether(authentication.admin)
	authentication.transport.CloseIdleConnections()
}

func (authentication *productionIdentityAuthentication) refresh(ctx context.Context) error {
	authentication.mu.Lock()
	defer authentication.mu.Unlock()
	return authentication.refreshLocked(ctx)
}

func (authentication *productionIdentityAuthentication) refreshLocked(ctx context.Context) error {
	if authentication.closed {
		return identitytrust.ErrInvalidAuthority
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	pair, changed, err := authentication.adapter.Refresh(ctx)
	if err != nil {
		if (errors.Is(err, identitytrust.ErrTransportUnavailable) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)) && authentication.ready() {
			return err
		}
		authentication.closeLocked()
		return identitytrust.ErrInvalidAuthority
	}
	if changed {
		if err := authentication.user.ReloadTogether(pair.User, authentication.admin, pair.Admin); err != nil {
			authentication.closeLocked()
			return identitytrust.ErrInvalidAuthority
		}
	}
	return nil
}

type productionRefreshingVerifier struct {
	authentication *productionIdentityAuthentication
	offline        *authn.ConfiguredVerifier
}

func (verifier *productionRefreshingVerifier) Verify(token string, request authn.VerificationRequest) (*authn.VerifiedPrincipal, error) {
	return verifier.VerifyContext(context.Background(), token, request)
}

func (verifier *productionRefreshingVerifier) VerifyContext(ctx context.Context, token string, request authn.VerificationRequest) (*authn.VerifiedPrincipal, error) {
	principal, err := verifier.offline.Verify(token, request)
	// The frozen verifier exposes only stable redacted categories, never a token-provided URL.
	if err == nil || err.Error() != "unknown_key" || ctx.Err() != nil {
		return principal, err
	}
	authentication := verifier.authentication
	authentication.mu.Lock()
	defer authentication.mu.Unlock()
	if ctx.Err() != nil || authentication.closed {
		return nil, identityaccess.ErrTokenRejected
	}
	if time.Since(authentication.lastUnknownKeyRefresh) >= time.Minute {
		authentication.lastUnknownKeyRefresh = time.Now()
		if err := authentication.refreshLocked(ctx); err != nil {
			return nil, identityaccess.ErrTokenRejected
		}
	}
	return verifier.offline.Verify(token, request)
}
