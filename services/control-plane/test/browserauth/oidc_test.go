package browserauth_test

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/browserauth"
)

func TestOIDCProviderVerifiesDiscoveryPKCENonceAndOptionalEmail(t *testing.T) {
	t.Parallel()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	state := newProof(t)
	verifier := newProof(t)
	nonce := newProof(t)
	challenge := proofChallenge(verifier)

	var server *httptest.Server
	server = httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/.well-known/openid-configuration":
			writeJSON(t, writer, map[string]any{
				"issuer": server.URL, "authorization_endpoint": server.URL + "/authorize",
				"token_endpoint": server.URL + "/token", "jwks_uri": server.URL + "/jwks",
				"id_token_signing_alg_values_supported": []string{"RS256"},
			})
		case "/jwks":
			writeJSON(t, writer, map[string]any{"keys": []any{map[string]any{
				"kty": "RSA", "kid": "fixture-key", "use": "sig", "alg": "RS256",
				"n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()), "e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes()),
			}}})
		case "/token":
			if err := request.ParseForm(); err != nil || request.Form.Get("code_verifier") != verifier {
				t.Errorf("token request did not bind PKCE verifier: %v %q", err, request.Form.Get("code_verifier"))
			}
			claims := map[string]any{
				"iss": server.URL, "aud": "client-1", "sub": "subject-1", "nonce": nonce,
				"iat": time.Now().Add(-time.Minute).Unix(), "exp": time.Now().Add(time.Minute).Unix(),
			}
			if request.Form.Get("code") == "with-email" {
				claims["email"] = "person@example.com"
				claims["email_verified"] = true
			}
			writeJSON(t, writer, map[string]any{
				"access_token": "fixture-access", "token_type": "Bearer", "expires_in": 60,
				"id_token": signFixtureJWT(t, key, claims),
			})
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	provider, err := browserauth.NewOIDCProvider(context.Background(), browserauth.OIDCProviderConfig{
		Issuer: server.URL, ClientID: "client-1", ClientSecret: "secret-1",
		RedirectURL: "https://identity.example.test/callback", HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	authorizationURL, err := provider.AuthorizationURL(browserauth.AuthorizationRequest{State: state, Nonce: nonce, CodeChallenge: challenge})
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(authorizationURL)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Path != "/authorize" || parsed.Query().Get("state") != state || parsed.Query().Get("nonce") != nonce || parsed.Query().Get("code_challenge") != challenge || parsed.Query().Get("code_challenge_method") != "S256" {
		t.Fatalf("authorization URL lost OIDC binding: %s", authorizationURL)
	}

	verified, err := provider.Exchange(context.Background(), browserauth.AuthorizationCallback{Code: "with-email", CodeVerifier: verifier, Nonce: nonce})
	if err != nil {
		t.Fatal(err)
	}
	if verified.Issuer != server.URL || verified.Subject != "subject-1" || verified.Email != "person@example.com" || !verified.EmailVerified {
		t.Fatalf("unexpected verified identity: %#v", verified)
	}
	withoutEmail, err := provider.Exchange(context.Background(), browserauth.AuthorizationCallback{Code: "without-email", CodeVerifier: verifier, Nonce: nonce})
	if err != nil {
		t.Fatal(err)
	}
	if withoutEmail.Subject != "subject-1" || withoutEmail.Email != "" || withoutEmail.EmailVerified {
		t.Fatalf("linked subject without email must remain usable: %#v", withoutEmail)
	}
	if _, err := provider.Exchange(context.Background(), browserauth.AuthorizationCallback{Code: "with-email", CodeVerifier: verifier, Nonce: newProof(t)}); !errors.Is(err, browserauth.ErrUnauthorized) {
		t.Fatalf("nonce mismatch was not rejected: %v", err)
	}
}

func TestOIDCProviderRejectsInsecureAndUnboundedDiscovery(t *testing.T) {
	t.Parallel()
	for name, config := range map[string]browserauth.OIDCProviderConfig{
		"http issuer":    {Issuer: "http://issuer.example.test", ClientID: "client", RedirectURL: "https://identity.example.test/callback"},
		"http redirect":  {Issuer: "https://issuer.example.test", ClientID: "client", RedirectURL: "http://identity.example.test/callback"},
		"redirect query": {Issuer: "https://issuer.example.test", ClientID: "client", RedirectURL: "https://identity.example.test/callback?next=x"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := browserauth.NewOIDCProvider(context.Background(), config); err == nil {
				t.Fatal("insecure provider configuration was accepted")
			}
		})
	}

	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(strings.Repeat(" ", (128<<10)+1)))
	}))
	defer server.Close()
	if _, err := browserauth.NewOIDCProvider(context.Background(), browserauth.OIDCProviderConfig{
		Issuer: server.URL, ClientID: "client", RedirectURL: "https://identity.example.test/callback", HTTPClient: server.Client(),
	}); err == nil || strings.Contains(err.Error(), "client") {
		t.Fatalf("unbounded discovery did not fail safely: %v", err)
	}
}

func newProof(t *testing.T) string {
	t.Helper()
	proof, _, err := browserauth.NewProof()
	if err != nil {
		t.Fatal(err)
	}
	return proof
}

func proofChallenge(verifier string) string {
	digest := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(digest[:])
}

func writeJSON(t *testing.T, writer http.ResponseWriter, value any) {
	t.Helper()
	if err := json.NewEncoder(writer).Encode(value); err != nil {
		t.Error(err)
	}
}

func signFixtureJWT(t *testing.T, key *rsa.PrivateKey, claims map[string]any) string {
	t.Helper()
	header, _ := json.Marshal(map[string]any{"alg": "RS256", "kid": "fixture-key", "typ": "JWT"})
	payload, _ := json.Marshal(claims)
	unsigned := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(payload)
	digest := sha256.Sum256([]byte(unsigned))
	signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	return unsigned + "." + base64.RawURLEncoding.EncodeToString(signature)
}
