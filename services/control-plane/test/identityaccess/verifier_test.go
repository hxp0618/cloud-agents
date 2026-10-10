package identityaccess_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	api "github.com/hxp0618/cloud-agents/sdk/go/gen/openapi/v1alpha1"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authn"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/identity"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/identityaccess"
)

const (
	accessIssuer        = "https://identity.example.test"
	accessAdminAudience = "https://admin-api.example.test"
	accessUserAudience  = "https://user-api.example.test"
	accessKeyID         = "identity-key-1"
)

func TestVerifierRequiresOfflinePurposeAndLiveStatusOnEveryRequest(t *testing.T) {
	fixture := newAccessFixture(t)
	status := &statusEndpoint{t: t, mode: "active", expected: api.TokenStatusRequest{
		TokenSHA256: fixture.token.SHA256, ExpectedClientID: identity.UserWebClientID, ExpectedApplication: api.IdentityApplicationUser,
		ExpectedTenantID: "tenant-1", ExpectedProjectID: "project-1",
	}}
	server := httptest.NewTLSServer(status)
	t.Cleanup(server.Close)
	client, err := api.NewIdentityServiceHTTPClientWithClient(server.URL, "identity-service-credential", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := identityaccess.NewVerifier(fixture.userVerifier, client, api.IdentityApplicationUser)
	if err != nil {
		t.Fatal(err)
	}

	for range 2 {
		principal, err := verifier.Verify(fixture.token.Token, fixture.request)
		if err != nil || principal == nil {
			t.Fatalf("active token rejected: %v", err)
		}
	}
	if calls := status.callCount(); calls != 2 {
		t.Fatalf("live status calls = %d, want 2", calls)
	}
	status.setMode("inactive")
	assertVerificationRejected(t, func() (*authn.VerifiedPrincipal, error) { return verifier.Verify(fixture.token.Token, fixture.request) })
	status.setMode("unavailable")
	assertVerificationRejected(t, func() (*authn.VerifiedPrincipal, error) { return verifier.Verify(fixture.token.Token, fixture.request) })
	status.setMode("invalid")
	assertVerificationRejected(t, func() (*authn.VerifiedPrincipal, error) { return verifier.Verify(fixture.token.Token, fixture.request) })
	if calls := status.callCount(); calls != 5 {
		t.Fatalf("live status calls = %d, want 5", calls)
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	assertVerificationRejected(t, func() (*authn.VerifiedPrincipal, error) {
		return verifier.VerifyContext(canceled, fixture.token.Token, fixture.request)
	})
	if status.callCount() != 5 {
		t.Fatal("canceled verification reached identity service")
	}
}

func TestVerifierRejectsOfflineAndPurposeMismatchBeforeLiveStatus(t *testing.T) {
	fixture := newAccessFixture(t)
	checker := &recordingChecker{status: api.TokenStatus{Status: "active"}}
	verifier, err := identityaccess.NewVerifier(fixture.userVerifier, checker, api.IdentityApplicationUser)
	if err != nil {
		t.Fatal(err)
	}

	wrongTenant := fixture.request
	wrongTenant.TenantID = "tenant-2"
	assertVerificationRejected(t, func() (*authn.VerifiedPrincipal, error) { return verifier.Verify(fixture.token.Token, wrongTenant) })

	adminToken := fixture.sign(t, api.IdentityApplicationAdmin)
	assertVerificationRejected(t, func() (*authn.VerifiedPrincipal, error) { return verifier.Verify(adminToken.Token, fixture.request) })
	if checker.calls != 0 {
		t.Fatalf("offline rejection made %d status calls", checker.calls)
	}

	adminAudienceVerifier := configuredAccessVerifier(t, fixture.privateKey, accessAdminAudience, fixture.authority, fixture.now)
	wrongPurpose, err := identityaccess.NewVerifier(adminAudienceVerifier, checker, api.IdentityApplicationUser)
	if err != nil {
		t.Fatal(err)
	}
	assertVerificationRejected(t, func() (*authn.VerifiedPrincipal, error) {
		return wrongPurpose.Verify(adminToken.Token, fixture.request)
	})
	if checker.calls != 0 {
		t.Fatal("client_id mismatch reached identity service")
	}
}

func TestVerifierBoundsLiveCheckAndRejectsInvalidConfiguration(t *testing.T) {
	fixture := newAccessFixture(t)
	checker := &recordingChecker{status: api.TokenStatus{Status: "active"}, inspectDeadline: true}
	verifier, err := identityaccess.NewVerifier(fixture.userVerifier, checker, api.IdentityApplicationUser)
	if err != nil {
		t.Fatal(err)
	}
	if principal, err := verifier.VerifyContext(context.Background(), fixture.token.Token, fixture.request); err != nil || principal == nil {
		t.Fatalf("bounded verification failed: %v", err)
	}
	if checker.deadlineRemaining <= 4*time.Second || checker.deadlineRemaining > 5*time.Second {
		t.Fatalf("status deadline remaining = %v", checker.deadlineRemaining)
	}
	checker.status.Status = "unexpected"
	assertVerificationRejected(t, func() (*authn.VerifiedPrincipal, error) { return verifier.Verify(fixture.token.Token, fixture.request) })

	for name, construct := range map[string]func() (*identityaccess.Verifier, error){
		"offline": func() (*identityaccess.Verifier, error) {
			return identityaccess.NewVerifier(nil, checker, api.IdentityApplicationUser)
		},
		"status": func() (*identityaccess.Verifier, error) {
			return identityaccess.NewVerifier(fixture.userVerifier, nil, api.IdentityApplicationUser)
		},
		"application": func() (*identityaccess.Verifier, error) {
			return identityaccess.NewVerifier(fixture.userVerifier, checker, "other")
		},
	} {
		t.Run(name, func(t *testing.T) {
			candidate, err := construct()
			if candidate != nil || !errors.Is(err, identityaccess.ErrInvalidVerifier) {
				t.Fatalf("verifier = %v, error = %v", candidate, err)
			}
		})
	}
	assertVerificationRejected(t, func() (*authn.VerifiedPrincipal, error) {
		return (*identityaccess.Verifier)(nil).Verify(fixture.token.Token, fixture.request)
	})
}

type accessFixture struct {
	privateKey   *rsa.PrivateKey
	now          time.Time
	authority    identity.TokenSigningAuthority
	signer       *identity.TokenSigner
	token        identity.SignedTenantToken
	userVerifier *authn.ConfiguredVerifier
	request      authn.VerificationRequest
}

func newAccessFixture(t *testing.T) accessFixture {
	t.Helper()
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 8, 13, 0, 0, 0, time.UTC)
	authority := identity.TokenSigningAuthority{
		SecurityEpoch: 7, NotBefore: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour),
		KeyNotBefore: now.Add(-time.Hour), KeyNotAfter: now.Add(time.Hour),
	}
	signer, err := identity.NewTokenSigner(identity.TokenSignerConfig{
		Issuer: accessIssuer, AdminAudience: accessAdminAudience, UserAudience: accessUserAudience,
		KeyID: accessKeyID, PrivateKey: privateKey,
	})
	if err != nil {
		t.Fatal(err)
	}
	fixture := accessFixture{
		privateKey: privateKey, now: now, authority: authority, signer: signer,
		request: authn.VerificationRequest{TenantID: "tenant-1", ResourceLevel: "project", ResourceID: "project-1", RequiredPermission: "agents.get"},
	}
	fixture.token = fixture.sign(t, api.IdentityApplicationUser)
	fixture.userVerifier = configuredAccessVerifier(t, privateKey, accessUserAudience, authority, now)
	return fixture
}

func (fixture accessFixture) sign(t *testing.T, application api.IdentityApplication) identity.SignedTenantToken {
	t.Helper()
	request, err := identity.NewTokenSigningRequest(
		identity.TokenClientWeb, application, "alpha", "tenant-1", "project-1",
		[]string{"agents.get"}, fixture.authority, fixture.now,
	)
	if err != nil {
		t.Fatal(err)
	}
	token, err := fixture.signer.Sign(request)
	if err != nil {
		t.Fatal(err)
	}
	return token
}

func configuredAccessVerifier(t *testing.T, privateKey *rsa.PrivateKey, audience string, authority identity.TokenSigningAuthority, now time.Time) *authn.ConfiguredVerifier {
	t.Helper()
	jwk, err := json.Marshal(struct {
		Algorithm string   `json:"alg"`
		Exponent  string   `json:"e"`
		KeyOps    []string `json:"key_ops"`
		KeyID     string   `json:"kid"`
		KeyType   string   `json:"kty"`
		Modulus   string   `json:"n"`
		Use       string   `json:"use"`
	}{
		Algorithm: "RS256", Exponent: "AQAB", KeyOps: []string{"verify"}, KeyID: accessKeyID, KeyType: "RSA",
		Modulus: base64.RawURLEncoding.EncodeToString(privateKey.PublicKey.N.Bytes()), Use: "sig",
	})
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := authn.NewConfiguredVerifier(authn.ConfiguredVerifierConfig{
		Issuer: accessIssuer, Audience: audience, Generation: 1, SecurityEpoch: authority.SecurityEpoch,
		NotBefore: authority.NotBefore.Unix(), ExpiresAt: authority.ExpiresAt.Unix(),
		Keys: []authn.ConfiguredVerifierKey{{
			JWK: jwk, Enabled: true, NotBefore: authority.KeyNotBefore.Unix(), NotAfter: authority.KeyNotAfter.Unix(),
		}},
		Clock: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(verifier.Invalidate)
	return verifier
}

type statusEndpoint struct {
	t        *testing.T
	mu       sync.Mutex
	mode     string
	calls    int
	expected api.TokenStatusRequest
}

func (endpoint *statusEndpoint) setMode(mode string) {
	endpoint.mu.Lock()
	defer endpoint.mu.Unlock()
	endpoint.mode = mode
}

func (endpoint *statusEndpoint) callCount() int {
	endpoint.mu.Lock()
	defer endpoint.mu.Unlock()
	return endpoint.calls
}

func (endpoint *statusEndpoint) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	endpoint.mu.Lock()
	defer endpoint.mu.Unlock()
	endpoint.calls++
	if request.Method != http.MethodPost || request.URL.Path != "/v1/identity/token-status" ||
		request.Header.Get("Authorization") != "Bearer identity-service-credential" || request.Header.Get("X-Request-ID") == "" {
		endpoint.t.Errorf("unexpected status request: %s %s", request.Method, request.URL.Path)
		writer.WriteHeader(http.StatusBadRequest)
		return
	}
	defer request.Body.Close()
	body, err := io.ReadAll(request.Body)
	if err != nil {
		endpoint.t.Error(err)
		writer.WriteHeader(http.StatusBadRequest)
		return
	}
	input, err := api.DecodeTokenStatusRequestJSON(body)
	if err != nil || input != endpoint.expected {
		endpoint.t.Errorf("status input = %#v, %v", input, err)
		writer.WriteHeader(http.StatusBadRequest)
		return
	}
	switch endpoint.mode {
	case "active", "inactive":
		_, _ = writer.Write([]byte(`{"status":"` + endpoint.mode + `"}`))
	case "unavailable":
		writer.WriteHeader(http.StatusServiceUnavailable)
		_, _ = writer.Write([]byte(`{}`))
	default:
		_, _ = writer.Write([]byte(`{"status":"unexpected"}`))
	}
}

type recordingChecker struct {
	status            api.TokenStatus
	calls             int
	inspectDeadline   bool
	deadlineRemaining time.Duration
}

func (checker *recordingChecker) CheckTokenStatus(ctx context.Context, _ string, _ api.TokenStatusRequest) (api.TokenStatus, error) {
	checker.calls++
	if checker.inspectDeadline {
		deadline, ok := ctx.Deadline()
		if !ok {
			return api.TokenStatus{}, errors.New("status context has no deadline")
		}
		checker.deadlineRemaining = time.Until(deadline)
	}
	return checker.status, nil
}

func assertVerificationRejected(t *testing.T, verify func() (*authn.VerifiedPrincipal, error)) {
	t.Helper()
	principal, err := verify()
	if principal != nil || !errors.Is(err, identityaccess.ErrTokenRejected) || err.Error() != identityaccess.ErrTokenRejected.Error() {
		t.Fatalf("principal = %v, error = %v", principal, err)
	}
}
