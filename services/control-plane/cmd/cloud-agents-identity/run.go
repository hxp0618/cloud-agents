package main

import (
	"bytes"
	"context"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"net"
	"net/http"
	"time"

	api "github.com/hxp0618/cloud-agents/sdk/go/gen/openapi/v1alpha1"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/browserauth"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/identity"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	identityDatabaseTimeout = 5 * time.Second
	identityShutdownTimeout = 10 * time.Second
	identityHTTPTimeout     = 10 * time.Second
	maximumCertificateBytes = 1 << 20
)

type runtimeIdentityStore struct {
	*identity.PasswordStore
	*identity.EmailPolicyStore
	*identity.InvitationStore
	*identity.AccountSecurityStore
	*identity.TokenStore
	*identity.SigningAuthorityStore
	*identity.ProviderStore
	*identity.ProviderAccountStore
	*identity.CLIStore
	*identity.AutomationStore
}

var _ identity.Store = (*runtimeIdentityStore)(nil)

func runIdentity(ctx context.Context, config runConfig) error {
	if ctx == nil {
		return errors.New("identity runtime context is required")
	}
	tlsConfig, err := loadServerTLSConfig(config)
	if err != nil {
		return err
	}
	signer, err := loadTokenSigner(config.Issuer, config.AdminAudience, config.UserAudience, config.SigningKeyID, config.SigningPrivateKeyFile)
	if err != nil {
		return err
	}
	csrfKey, err := readRegularFile(config.CSRFKeyFile, 32, true)
	if err != nil || len(csrfKey) != 32 {
		return errors.New("identity CSRF key is invalid")
	}
	providerFlowKey, err := readRegularFile(config.ProviderFlowKeyFile, 32, true)
	if err != nil || len(providerFlowKey) != 32 {
		return errors.New("identity provider flow key is invalid")
	}
	providerMaterials, err := newFileProviderMaterials(config.ProviderSecretFiles, config.ProviderRootCAFiles)
	if err != nil {
		return err
	}
	adminCredential, err := readCanonicalProof(config.AdminWebCredentialFile)
	if err != nil {
		return err
	}
	userCredential, err := readCanonicalProof(config.UserWebCredentialFile)
	if err != nil {
		return err
	}
	controlPlaneCredential, err := readCanonicalProof(config.ControlPlaneCredentialFile)
	if err != nil {
		return err
	}
	authorizationCredential, err := readCanonicalProof(config.ControlPlaneAuthorizationCredentialFile)
	if err != nil || !distinctProofs(adminCredential, userCredential, controlPlaneCredential, authorizationCredential) {
		return errors.New("identity service credentials are invalid")
	}
	authorizer, err := newAuthorizationClient(config.ControlPlaneURL, config.ControlPlaneRootCAFile, authorizationCredential)
	if err != nil {
		return err
	}
	databaseURL, err := readSecretText(config.DatabaseURLFile, maximumIdentitySecretBytes)
	if err != nil {
		return errors.New("identity database secret is invalid")
	}
	poolConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return errors.New("identity database configuration is invalid")
	}
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return errors.New("identity database is unavailable")
	}
	defer pool.Close()
	if err := verifyIdentityDatabaseAuthority(ctx, pool, "cloud_agents_identity_service"); err != nil {
		return err
	}
	passwordStore, err := identity.NewPasswordStore(pool, csrfKey)
	if err != nil {
		return errors.New("identity password store is unavailable")
	}
	emailPolicyStore, err := identity.NewEmailPolicyStore(pool)
	if err != nil {
		return errors.New("identity email policy store is unavailable")
	}
	invitationStore, err := identity.NewInvitationStore(pool, passwordStore)
	if err != nil {
		return errors.New("identity invitation store is unavailable")
	}
	accountSecurityStore, err := identity.NewAccountSecurityStore(pool, passwordStore)
	if err != nil {
		return errors.New("identity account security store is unavailable")
	}
	providerFactory, err := identity.NewRuntimeProviderFactory(providerMaterials)
	if err != nil {
		return errors.New("identity provider factory is unavailable")
	}
	providerStore, err := identity.NewProviderStore(passwordStore, providerFactory, providerFlowKey)
	if err != nil {
		return errors.New("identity provider store is unavailable")
	}
	providerAccountStore, err := identity.NewProviderAccountStore(pool, passwordStore)
	if err != nil {
		return errors.New("identity provider account store is unavailable")
	}
	signingStore, err := identity.NewSigningAuthorityStore(pool, signer)
	if err != nil {
		return errors.New("identity signing authority is unavailable")
	}
	tokenStore, err := identity.NewTokenStore(pool, signer, authorizer, signingStore.SigningAuthority)
	if err != nil {
		return errors.New("identity token store is unavailable")
	}
	cliStore, err := identity.NewCLIStore(pool, passwordStore, signer, authorizer, signingStore.SigningAuthority)
	if err != nil {
		return errors.New("identity CLI store is unavailable")
	}
	automationStore, err := identity.NewAutomationStore(pool, signer, authorizer, signingStore.SigningAuthority)
	if err != nil {
		return errors.New("identity automation store is unavailable")
	}
	store := &runtimeIdentityStore{
		PasswordStore: passwordStore, EmailPolicyStore: emailPolicyStore, InvitationStore: invitationStore,
		AccountSecurityStore: accountSecurityStore, TokenStore: tokenStore, SigningAuthorityStore: signingStore,
		ProviderStore: providerStore, ProviderAccountStore: providerAccountStore,
		CLIStore: cliStore, AutomationStore: automationStore,
	}
	startupContext, cancelStartup := context.WithTimeout(ctx, identityDatabaseTimeout)
	_, err = signingStore.JWKS(startupContext)
	cancelStartup()
	if err != nil {
		return errors.New("identity signing authority does not match the configured key")
	}
	handler, err := identity.NewServer(store, identity.ServiceCredentials{
		AdminWeb: adminCredential, UserWeb: userCredential, ControlPlane: controlPlaneCredential,
	})
	if err != nil {
		return errors.New("identity HTTP service configuration is invalid")
	}
	listener, err := net.Listen("tcp", config.Listen)
	if err != nil {
		return errors.New("identity HTTPS listener is unavailable")
	}
	defer listener.Close()
	httpServer := newIdentityHTTPServer(config.Listen, tlsConfig, handler, ctx)
	return serveIdentityHTTPS(ctx, listener, httpServer)
}

func newIdentityHTTPServer(address string, tlsConfig *tls.Config, handler http.Handler, base context.Context) *http.Server {
	return &http.Server{
		Addr: address, Handler: handler, TLSConfig: tlsConfig,
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second,
		IdleTimeout: 60 * time.Second, MaxHeaderBytes: 32 << 10,
		BaseContext: func(net.Listener) context.Context { return base },
	}
}

func serveIdentityHTTPS(ctx context.Context, listener net.Listener, server *http.Server) error {
	if ctx == nil || listener == nil || server == nil || server.TLSConfig == nil || server.Handler == nil {
		return errors.New("identity HTTPS server configuration is invalid")
	}
	errorsChannel := make(chan error, 1)
	go func() { errorsChannel <- server.Serve(tls.NewListener(listener, server.TLSConfig)) }()
	select {
	case <-ctx.Done():
		shutdownContext, cancel := context.WithTimeout(context.Background(), identityShutdownTimeout)
		defer cancel()
		if err := server.Shutdown(shutdownContext); err != nil {
			return errors.New("identity HTTPS shutdown failed")
		}
		if err := <-errorsChannel; err != nil && !errors.Is(err, http.ErrServerClosed) {
			return errors.New("identity HTTPS server stopped")
		}
		return nil
	case err := <-errorsChannel:
		if errors.Is(err, http.ErrServerClosed) && ctx.Err() != nil {
			return nil
		}
		return errors.New("identity HTTPS server stopped")
	}
}

func loadServerTLSConfig(config runConfig) (*tls.Config, error) {
	certificatePEM, err := readRegularFile(config.TLSCertificateFile, maximumCertificateBytes, false)
	if err != nil {
		return nil, errors.New("identity TLS certificate is invalid")
	}
	privateKeyPEM, err := readRegularFile(config.TLSPrivateKeyFile, maximumIdentitySecretBytes, true)
	if err != nil {
		return nil, errors.New("identity TLS private key is invalid")
	}
	pair, err := tls.X509KeyPair(certificatePEM, privateKeyPEM)
	if err != nil || len(pair.Certificate) == 0 {
		return nil, errors.New("identity TLS identity is invalid")
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return nil, errors.New("identity TLS identity is invalid")
	}
	pair.Leaf = leaf
	return &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{pair}}, nil
}

func loadTokenSigner(issuer, adminAudience, userAudience, keyID, path string) (*identity.TokenSigner, error) {
	privateKeyPEM, err := readRegularFile(path, maximumIdentitySecretBytes, true)
	if err != nil {
		return nil, errors.New("identity signing private key is invalid")
	}
	block, rest := pem.Decode(privateKeyPEM)
	if block == nil || block.Type != "PRIVATE KEY" || len(bytes.TrimSpace(rest)) != 0 {
		return nil, errors.New("identity signing private key is invalid")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	privateKey, ok := parsed.(*rsa.PrivateKey)
	if err != nil || !ok {
		return nil, errors.New("identity signing private key is invalid")
	}
	signer, err := identity.NewTokenSigner(identity.TokenSignerConfig{
		Issuer: issuer, AdminAudience: adminAudience, UserAudience: userAudience, KeyID: keyID, PrivateKey: privateKey,
	})
	if err != nil {
		return nil, errors.New("identity token signer configuration is invalid")
	}
	return signer, nil
}

func newAuthorizationClient(controlPlaneURL, rootCAPath, credential string) (*api.IdentityAuthorizationClient, error) {
	if !validControlPlaneURL(controlPlaneURL) {
		return nil, errors.New("Control Plane authorization endpoint is invalid")
	}
	rootPEM, err := readRegularFile(rootCAPath, maximumCertificateBytes, false)
	if err != nil {
		return nil, errors.New("Control Plane root CAs are invalid")
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(rootPEM) {
		return nil, errors.New("Control Plane root CAs are invalid")
	}
	if _, err := browserauth.ProofDigest(credential); err != nil {
		return nil, errors.New("Control Plane authorization credential is invalid")
	}
	transport := &http.Transport{
		Proxy: nil, ForceAttemptHTTP2: true, IdleConnTimeout: 60 * time.Second,
		TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots},
		DialContext:     (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
	}
	client := &http.Client{Transport: transport, Timeout: identityHTTPTimeout}
	result, err := api.NewIdentityAuthorizationHTTPClientWithClient(controlPlaneURL, credential, client)
	if err != nil {
		return nil, errors.New("Control Plane authorization client is invalid")
	}
	return result, nil
}

func readCanonicalProof(path string) (string, error) {
	value, err := readSecretText(path, 128)
	if err != nil {
		return "", errors.New("identity service credential is invalid")
	}
	if _, err := browserauth.ProofDigest(value); err != nil {
		return "", errors.New("identity service credential is invalid")
	}
	return value, nil
}

func distinctProofs(values ...string) bool {
	digests := make(map[[32]byte]struct{}, len(values))
	for _, value := range values {
		digest, err := browserauth.ProofDigest(value)
		if err != nil {
			return false
		}
		if _, exists := digests[digest]; exists {
			return false
		}
		digests[digest] = struct{}{}
	}
	return true
}

func verifyIdentityDatabaseAuthority(ctx context.Context, pool *pgxpool.Pool, requiredRole string) error {
	checkContext, cancel := context.WithTimeout(ctx, identityDatabaseTimeout)
	defer cancel()
	var safe bool
	err := pool.QueryRow(checkContext, `SELECT NOT rolsuper AND NOT rolcreatedb AND NOT rolcreaterole
		AND NOT rolreplication AND NOT rolbypassrls
		AND pg_has_role(current_user, $1, 'USAGE')
		AND NOT pg_has_role(current_user, 'cloud_agents_runtime', 'USAGE')
		AND NOT pg_has_role(current_user, 'cloud_agents_bootstrap_admin', 'USAGE')
		AND NOT pg_has_role(current_user, 'cloud_agents_migration_owner', 'USAGE')
		FROM pg_roles WHERE rolname = current_user`, requiredRole).Scan(&safe)
	if err != nil || !safe {
		return errors.New("identity database authority is unsafe")
	}
	return nil
}
