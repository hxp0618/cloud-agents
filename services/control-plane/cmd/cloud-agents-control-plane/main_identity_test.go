//go:build !localdev

package main

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	api "github.com/hxp0618/cloud-agents/sdk/go/gen/openapi/v1alpha1"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authn"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/browserauth"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/identity"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/identitytrust"
)

// These tests exercise private process configuration and reload wiring.
func TestProductionIdentityAuthenticationHTTPSRefreshAndRevocation(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	jwk := api.IdentityJWK{Alg: "RS256", E: "AQAB", KeyOps: []string{"verify"}, Kid: "key-1", Kty: "RSA", N: base64.RawURLEncoding.EncodeToString(key.N.Bytes()), Use: "sig"}
	const issuer = "https://identity.test"
	document := api.IdentityJWKS{Keys: []api.IdentityJWK{jwk}, CloudAgentsAuthority: api.IdentityJWKSAuthority{
		Issuer: issuer, Revision: "1", SecurityEpoch: "7", NotBefore: now.Add(-time.Minute).Unix(), ExpiresAt: now.Add(time.Hour).Unix(),
		Lineage: []api.IdentityJWKSLineageKey{{JWK: jwk, Enabled: true, NotBefore: now.Add(-time.Hour).Unix(), NotAfter: now.Add(24 * time.Hour).Unix()}},
	}}
	controlPlaneProof, _, _ := browserauth.NewProof()
	identityProof, _, _ := browserauth.NewProof()
	var mu sync.Mutex
	var stallJWKS atomic.Bool
	active, statusCalls, jwksCalls := true, 0, 0
	host := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/.well-known/jwks.json" && stallJWKS.Load() {
			<-request.Context().Done()
			return
		}
		mu.Lock()
		defer mu.Unlock()
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/.well-known/jwks.json":
			jwksCalls++
			if request.Header.Get("Authorization") != "" {
				t.Error("JWKS fetch sent a service credential")
			}
			_ = json.NewEncoder(writer).Encode(document)
		case "/v1/identity/token-status":
			if request.Header.Get("Authorization") != "Bearer "+controlPlaneProof {
				t.Error("status request used the wrong service credential")
				writer.WriteHeader(http.StatusUnauthorized)
				return
			}
			statusCalls++
			status := "inactive"
			if active {
				status = "active"
			}
			_ = json.NewEncoder(writer).Encode(api.TokenStatus{Status: status})
		default:
			http.NotFound(writer, request)
		}
	}))
	defer host.Close()
	directory := t.TempDir()
	write := func(name string, data []byte) string {
		path := directory + "/" + name
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	config := productionIdentityConfig{Issuer: issuer, UserAudience: "https://user.test", AdminAudience: "https://admin.test",
		IdentityBaseURL: host.URL, CAFile: write("ca.pem", pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: host.Certificate().Raw})),
		ControlPlaneCredentialFile: write("cp.proof", []byte(controlPlaneProof)), IdentityCredentialFile: write("identity.proof", []byte(identityProof))}
	body, _ := json.Marshal(config)
	path := write("identity.json", body)
	checkpoint := &productionIdentityTestCheckpoint{}
	authentication, err := loadProductionIdentityAuthentication(context.Background(), path, checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	defer authentication.close()
	signer, err := identity.NewTokenSigner(identity.TokenSignerConfig{Issuer: issuer, UserAudience: config.UserAudience, AdminAudience: config.AdminAudience, KeyID: jwk.Kid, PrivateKey: key})
	if err != nil {
		t.Fatal(err)
	}
	sign := func(epoch int64) string {
		request, err := identity.NewTokenSigningRequest(
			identity.TokenClientWeb, api.IdentityApplicationUser, "alpha", "tenant-a", "",
			[]string{"projects.get"}, identity.TokenSigningAuthority{
				SecurityEpoch: epoch, NotBefore: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour),
				KeyNotBefore: now.Add(-time.Hour), KeyNotAfter: now.Add(24 * time.Hour),
			}, now,
		)
		if err != nil {
			t.Fatal(err)
		}
		token, err := signer.Sign(request)
		if err != nil {
			t.Fatal(err)
		}
		return token.Token
	}
	request := authn.VerificationRequest{TenantID: "tenant-a", ResourceLevel: "tenant", ResourceID: "tenant-a", RequiredPermission: "projects.get"}
	token := sign(7)
	if _, err := authentication.userAccess.Verify(token, request); err != nil {
		t.Fatal("live token was rejected", err)
	}
	mu.Lock()
	active = false
	mu.Unlock()
	if _, err := authentication.userAccess.Verify(token, request); err == nil {
		t.Fatal("revoked token reused cached live status")
	}
	mu.Lock()
	if statusCalls != 2 {
		t.Error("each request must check status")
	}
	active = true
	mu.Unlock()

	rotatedKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	rotatedJWK := jwk
	rotatedJWK.Kid, rotatedJWK.N = "key-2", base64.RawURLEncoding.EncodeToString(rotatedKey.N.Bytes())
	mu.Lock()
	document.Keys = append(document.Keys, rotatedJWK)
	document.CloudAgentsAuthority.Lineage = append(document.CloudAgentsAuthority.Lineage, api.IdentityJWKSLineageKey{JWK: rotatedJWK, Enabled: true, NotBefore: now.Add(-time.Hour).Unix(), NotAfter: now.Add(24 * time.Hour).Unix()})
	document.CloudAgentsAuthority.Revision = "2"
	mu.Unlock()
	signer, err = identity.NewTokenSigner(identity.TokenSignerConfig{Issuer: issuer, UserAudience: config.UserAudience, AdminAudience: config.AdminAudience, KeyID: rotatedJWK.Kid, PrivateKey: rotatedKey})
	if err != nil {
		t.Fatal(err)
	}
	rotatedToken := sign(7)
	var requests sync.WaitGroup
	for range 16 {
		requests.Go(func() {
			if _, err := authentication.userAccess.Verify(rotatedToken, request); err != nil {
				t.Error("unknown key did not refresh and retry", err)
			}
		})
	}
	requests.Wait()
	signer, err = identity.NewTokenSigner(identity.TokenSignerConfig{Issuer: issuer, UserAudience: config.UserAudience, AdminAudience: config.AdminAudience, KeyID: "unpublished-key", PrivateKey: rotatedKey})
	if err != nil {
		t.Fatal(err)
	}
	unknownToken := sign(7)
	for range 3 {
		if _, err := authentication.userAccess.Verify(unknownToken, request); err == nil {
			t.Error("unpublished key was accepted")
		}
	}
	mu.Lock()
	if jwksCalls != 2 {
		t.Errorf("unknown key requests bypassed refresh coalescing/rate limit: %d fetches", jwksCalls)
	}
	mu.Unlock()
	authentication.mu.Lock()
	authentication.lastUnknownKeyRefresh = time.Time{}
	authentication.mu.Unlock()
	stallJWKS.Store(true)
	canceled, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := authentication.userAccess.VerifyContext(canceled, unknownToken, request); err == nil || !authentication.ready() {
		t.Fatal("canceled unknown-key refresh accepted a token or invalidated live authority")
	}
	stallJWKS.Store(false)
	mu.Lock()
	document.CloudAgentsAuthority.Revision, document.CloudAgentsAuthority.SecurityEpoch = "3", "8"
	mu.Unlock()
	signer, err = identity.NewTokenSigner(identity.TokenSignerConfig{Issuer: issuer, UserAudience: config.UserAudience, AdminAudience: config.AdminAudience, KeyID: rotatedJWK.Kid, PrivateKey: rotatedKey})
	if err != nil {
		t.Fatal(err)
	}
	if err := authentication.refresh(context.Background()); err != nil || !authentication.ready() {
		t.Fatal("automatic authority refresh failed", err)
	}
	if _, err := authentication.userAccess.Verify(token, request); err == nil {
		t.Fatal("old epoch remained valid after refresh")
	}
	if _, err := authentication.userAccess.Verify(sign(8), request); err != nil {
		t.Fatal("new epoch token was rejected", err)
	}
	mu.Lock()
	document.CloudAgentsAuthority.Revision = "1"
	mu.Unlock()
	if err := authentication.refresh(context.Background()); err == nil || authentication.ready() {
		t.Fatal("semantic rollback failed to close both verifier admissions")
	}
	host.Close()
	restarted, err := loadProductionIdentityAuthentication(context.Background(), path, checkpoint)
	if err != nil || !restarted.ready() {
		t.Fatal("unexpired durable checkpoint did not recover during transport outage", err)
	}
	defer restarted.close()
	if _, err := restarted.userAccess.Verify(sign(8), request); err == nil {
		t.Fatal("checkpoint recovery bypassed unavailable online status")
	}
	if err := restarted.refresh(context.Background()); !errors.Is(err, identitytrust.ErrTransportUnavailable) || !restarted.ready() {
		t.Fatal("transport outage discarded unexpired authority")
	}
}

func TestProductionIdentityAuthenticationRejectsManualOrUnsafeConfiguration(t *testing.T) {
	for _, body := range []string{
		`{"issuer":"https://identity.test","audience":"https://user.test","keys":[],"generation":1,"expiresAt":99}`,
		`{"issuer":"https://identity.test","userAudience":"https://user.test","adminAudience":"https://user.test","identityBaseUrl":"https://identity.test","controlPlaneCredentialFile":"cp","identityCredentialFile":"id"}`,
		`{"issuer":"https://identity.test","userAudience":"https://user.test","adminAudience":"https://admin.test","identityBaseUrl":"http://identity.test","controlPlaneCredentialFile":"cp","identityCredentialFile":"id"}`,
		`{"issuer":"https://identity.test","userAudience":"https://user.test","adminAudience":"https://admin.test","identityBaseUrl":"https://identity.test/other","controlPlaneCredentialFile":"cp","identityCredentialFile":"id"}`,
	} {
		path := t.TempDir() + "/identity.json"
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := loadProductionIdentityAuthentication(context.Background(), path, &productionIdentityTestCheckpoint{}); err == nil {
			t.Fatal("unsafe identity configuration was accepted")
		}
	}
}

type productionIdentityTestCheckpoint struct {
	state  identitytrust.State
	exists bool
}

func (checkpoint *productionIdentityTestCheckpoint) Load(context.Context, string) (identitytrust.State, bool, error) {
	return checkpoint.state, checkpoint.exists, nil
}

func (checkpoint *productionIdentityTestCheckpoint) CompareAndSwap(_ context.Context, _ string, expected string, state identitytrust.State) error {
	if checkpoint.state.AuthorityDigest != expected {
		return identitytrust.ErrCheckpoint
	}
	checkpoint.state, checkpoint.exists = state, true
	return nil
}
