package identity_test

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	api "github.com/hxp0618/cloud-agents/sdk/go/gen/openapi/v1alpha1"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authn"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/identity"
)

const (
	tokenTestIssuer        = "https://identity.example.test"
	tokenTestAdminAudience = "https://admin-api.example.test"
	tokenTestUserAudience  = "https://user-api.example.test"
	tokenTestKeyID         = "identity-key-1"
)

func TestTokenSignerProducesFrozenV1TokensForConfiguredVerifier(t *testing.T) {
	privateKey := tokenTestRSAKey(t, 2048)
	now := time.Date(2026, 10, 8, 12, 0, 0, 987654321, time.UTC)
	signer := tokenTestSigner(t, privateKey)
	authority := identity.TokenSigningAuthority{
		SecurityEpoch: 7,
		NotBefore:     now.Add(-time.Minute),
		ExpiresAt:     now.Add(time.Hour),
		KeyNotBefore:  now.Add(-time.Hour),
		KeyNotAfter:   now.Add(time.Hour),
	}
	request := tokenTestRequest(t, identity.TokenClientWeb, api.IdentityApplicationUser,
		"alpha", "tenant-1", "project-1", []string{"agents.get", "agents.update"}, authority, now)
	signed, err := signer.Sign(request)
	if err != nil {
		t.Fatal(err)
	}
	if signed.JTI == "" || signed.ExpiresAt != now.Truncate(time.Second).Add(15*time.Minute) {
		t.Fatalf("unexpected token metadata: %#v", signed)
	}
	digest := sha256.Sum256([]byte(signed.Token))
	if signed.SHA256 != "sha256:"+hex.EncodeToString(digest[:]) {
		t.Fatalf("token digest = %q", signed.SHA256)
	}
	header, claims := tokenTestDecode(t, signed.Token)
	if header["alg"] != "RS256" || header["kid"] != tokenTestKeyID || header["typ"] != "at+jwt" || len(header) != 3 {
		t.Fatalf("protected header = %#v", header)
	}
	if claims["iss"] != tokenTestIssuer || claims["sub"] != "user-alpha" || claims["aud"] != tokenTestUserAudience ||
		claims["client_id"] != identity.UserWebClientID || claims["scope"] != "agents.get agents.update" ||
		claims["https://schemas.cloud-agents.dev/claims/subject-kind"] != "user" ||
		claims["https://schemas.cloud-agents.dev/claims/tenant-id"] != "tenant-1" ||
		claims["https://schemas.cloud-agents.dev/claims/project-id"] != "project-1" ||
		claims["https://schemas.cloud-agents.dev/claims/token-profile"] != "cloud-agents-access-token/v1" {
		t.Fatalf("claims = %#v", claims)
	}
	if claims["jti"] != signed.JTI || claims["iat"] != float64(now.Unix()) || claims["exp"] != float64(signed.ExpiresAt.Unix()) ||
		claims["https://schemas.cloud-agents.dev/claims/security-epoch"] != float64(7) {
		t.Fatalf("claim metadata = %#v", claims)
	}

	userVerifier := tokenTestVerifier(t, privateKey, tokenTestUserAudience, authority, now)
	allowed := authn.VerificationRequest{TenantID: "tenant-1", ResourceLevel: "project", ResourceID: "project-1", RequiredPermission: "agents.get"}
	if principal, err := userVerifier.Verify(signed.Token, allowed); err != nil || principal == nil {
		t.Fatalf("configured verifier rejected signed token: %v", err)
	}
	for name, denied := range map[string]authn.VerificationRequest{
		"tenant":      {TenantID: "tenant-2", ResourceLevel: "project", ResourceID: "project-1", RequiredPermission: "agents.get"},
		"project":     {TenantID: "tenant-1", ResourceLevel: "project", ResourceID: "project-2", RequiredPermission: "agents.get"},
		"scope":       {TenantID: "tenant-1", ResourceLevel: "project", ResourceID: "project-1", RequiredPermission: "agents.delete"},
		"bound level": {TenantID: "tenant-1", ResourceLevel: "tenant", ResourceID: "tenant-1", RequiredPermission: "agents.get"},
	} {
		if principal, err := userVerifier.Verify(signed.Token, denied); err == nil || principal != nil {
			t.Fatalf("%s mismatch accepted", name)
		}
	}
	adminVerifier := tokenTestVerifier(t, privateKey, tokenTestAdminAudience, authority, now)
	if principal, err := adminVerifier.Verify(signed.Token, allowed); err == nil || principal != nil {
		t.Fatal("user token accepted for admin audience")
	}

	adminRequest := tokenTestRequest(t, identity.TokenClientWeb, api.IdentityApplicationAdmin,
		"alpha", "tenant-1", "", []string{"agents.get", "agents.update"}, authority, now)
	adminToken, err := signer.Sign(adminRequest)
	if err != nil {
		t.Fatal(err)
	}
	_, adminClaims := tokenTestDecode(t, adminToken.Token)
	if adminClaims["aud"] != tokenTestAdminAudience || adminClaims["client_id"] != identity.AdminWebClientID {
		t.Fatalf("admin purpose claims = %#v", adminClaims)
	}
	if _, exists := adminClaims["https://schemas.cloud-agents.dev/claims/project-id"]; exists {
		t.Fatal("unbound token includes a project claim")
	}
	if _, err := adminVerifier.Verify(adminToken.Token, authn.VerificationRequest{
		TenantID: "tenant-1", ResourceLevel: "tenant", ResourceID: "tenant-1", RequiredPermission: "agents.get",
	}); err != nil {
		t.Fatalf("admin verifier rejected tenant token: %v", err)
	}
	if adminToken.JTI == signed.JTI || adminToken.Token == signed.Token {
		t.Fatal("separate issuances reused token identity")
	}
}

func TestTokenSignerClipsExpiryToPersistedAuthority(t *testing.T) {
	privateKey := tokenTestRSAKey(t, 2048)
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	signer := tokenTestSigner(t, privateKey)
	authority := identity.TokenSigningAuthority{
		SecurityEpoch: 7, NotBefore: now.Add(-time.Minute), ExpiresAt: now.Add(4 * time.Minute),
		KeyNotBefore: now.Add(-time.Minute), KeyNotAfter: now.Add(5 * time.Minute),
	}
	request := tokenTestRequest(t, identity.TokenClientWeb, api.IdentityApplicationUser,
		"alpha", "tenant-1", "", []string{"agents.get"}, authority, now)
	signed, err := signer.Sign(request)
	if err != nil || signed.ExpiresAt != now.Add(4*time.Minute) {
		t.Fatalf("authority-clipped token = %#v, %v", signed, err)
	}
	authority.ExpiresAt = now.Add(time.Hour)
	request = tokenTestRequest(t, identity.TokenClientWeb, api.IdentityApplicationUser,
		"alpha", "tenant-1", "", []string{"agents.get"}, authority, now)
	signed, err = signer.Sign(request)
	if err != nil || signed.ExpiresAt != now.Add(5*time.Minute) {
		t.Fatalf("key-clipped token = %#v, %v", signed, err)
	}
}

func TestTokenSignerRejectsUnverifiableOrUnapprovedInputs(t *testing.T) {
	privateKey := tokenTestRSAKey(t, 2048)
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	validAuthority := identity.TokenSigningAuthority{
		SecurityEpoch: 7, NotBefore: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour),
		KeyNotBefore: now.Add(-time.Hour), KeyNotAfter: now.Add(time.Hour),
	}
	signer := tokenTestSigner(t, privateKey)
	type requestInput struct {
		purpose     identity.TokenClientPurpose
		application api.IdentityApplication
		subjectID   string
		tenantID    string
		projectID   string
		scopes      []string
		authority   identity.TokenSigningAuthority
		now         time.Time
	}
	valid := requestInput{identity.TokenClientWeb, api.IdentityApplicationUser, "alpha", "tenant-1", "project-1", []string{"agents.get", "agents.update"}, validAuthority, now}
	tests := map[string]func(*requestInput){
		"purpose":           func(v *requestInput) { v.purpose = "other" },
		"application":       func(v *requestInput) { v.application = "other" },
		"subject":           func(v *requestInput) { v.subjectID = "" },
		"tenant":            func(v *requestInput) { v.tenantID = "tenant/1" },
		"project":           func(v *requestInput) { v.projectID = "project/1" },
		"empty scopes":      func(v *requestInput) { v.scopes = nil },
		"unsorted scopes":   func(v *requestInput) { v.scopes = []string{"agents.update", "agents.get"} },
		"duplicate scopes":  func(v *requestInput) { v.scopes = []string{"agents.get", "agents.get"} },
		"invalid scope":     func(v *requestInput) { v.scopes = []string{"agents.read"} },
		"epoch":             func(v *requestInput) { v.authority.SecurityEpoch = 0 },
		"future authority":  func(v *requestInput) { v.authority.NotBefore = now.Add(time.Second) },
		"expired authority": func(v *requestInput) { v.authority.ExpiresAt = now },
		"future key":        func(v *requestInput) { v.authority.KeyNotBefore = now.Add(time.Second) },
		"expired key":       func(v *requestInput) { v.authority.KeyNotAfter = now },
		"zero clock":        func(v *requestInput) { v.now = time.Time{} },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			input := valid
			input.scopes = append([]string(nil), valid.scopes...)
			mutate(&input)
			request, requestErr := identity.NewTokenSigningRequest(
				input.purpose, input.application, input.subjectID, input.tenantID,
				input.projectID, input.scopes, input.authority, input.now,
			)
			if !errors.Is(requestErr, identity.ErrInvalidTokenSigningRequest) {
				t.Fatalf("request = %#v, error = %v", request, requestErr)
			}
		})
	}
	if result, err := signer.Sign(identity.TokenSigningRequest{}); !errors.Is(err, identity.ErrInvalidTokenSigningRequest) || result != (identity.SignedTenantToken{}) {
		t.Fatalf("zero request result = %#v, error = %v", result, err)
	}

	weakKey := tokenTestRSAKey(t, 1024)
	if signer, err := identity.NewTokenSigner(identity.TokenSignerConfig{
		Issuer: tokenTestIssuer, AdminAudience: tokenTestAdminAudience, UserAudience: tokenTestUserAudience,
		KeyID: tokenTestKeyID, PrivateKey: weakKey,
	}); !errors.Is(err, identity.ErrInvalidTokenSigner) || signer != nil {
		t.Fatalf("weak-key signer = %v, %v", signer, err)
	}
	for name, mutate := range map[string]func(*identity.TokenSignerConfig){
		"issuer":         func(v *identity.TokenSignerConfig) { v.Issuer = "identity issuer" },
		"admin audience": func(v *identity.TokenSignerConfig) { v.AdminAudience = "" },
		"same audience":  func(v *identity.TokenSignerConfig) { v.AdminAudience = v.UserAudience },
		"key id":         func(v *identity.TokenSignerConfig) { v.KeyID = "key/id" },
		"private key":    func(v *identity.TokenSignerConfig) { v.PrivateKey = nil },
	} {
		t.Run("config "+name, func(t *testing.T) {
			config := identity.TokenSignerConfig{
				Issuer: tokenTestIssuer, AdminAudience: tokenTestAdminAudience, UserAudience: tokenTestUserAudience,
				KeyID: tokenTestKeyID, PrivateKey: privateKey,
			}
			mutate(&config)
			if signer, err := identity.NewTokenSigner(config); !errors.Is(err, identity.ErrInvalidTokenSigner) || signer != nil {
				t.Fatalf("signer = %v, %v", signer, err)
			}
		})
	}
}

func TestTokenSignerUsesClosedCLIAndAutomationSubjects(t *testing.T) {
	privateKey := tokenTestRSAKey(t, 2048)
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	authority := identity.TokenSigningAuthority{
		SecurityEpoch: 7, NotBefore: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour),
		KeyNotBefore: now.Add(-time.Minute), KeyNotAfter: now.Add(time.Hour),
	}
	signer := tokenTestSigner(t, privateKey)
	verifier := tokenTestVerifier(t, privateKey, tokenTestAdminAudience, authority, now)
	for _, test := range []struct {
		name, subjectID, subject, kind, clientID string
		purpose                                  identity.TokenClientPurpose
	}{
		{"CLI", "alpha", "user-alpha", "user", identity.CLIClientID, identity.TokenClientCLI},
		{"automation", "service-alpha", "service-service-alpha", "serviceAccount", identity.AutomationClientID, identity.TokenClientAutomation},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := tokenTestRequest(t, test.purpose, api.IdentityApplicationAdmin,
				test.subjectID, "tenant-1", "", []string{"agents.get"}, authority, now)
			signed, err := signer.Sign(request)
			if err != nil {
				t.Fatal(err)
			}
			_, claims := tokenTestDecode(t, signed.Token)
			if claims["sub"] != test.subject || claims["client_id"] != test.clientID ||
				claims["https://schemas.cloud-agents.dev/claims/subject-kind"] != test.kind {
				t.Fatalf("claims = %#v", claims)
			}
			if principal, err := verifier.Verify(signed.Token, authn.VerificationRequest{
				TenantID: "tenant-1", ResourceLevel: "tenant", ResourceID: "tenant-1",
				RequiredPermission: "agents.get",
			}); err != nil || principal == nil {
				t.Fatalf("configured verifier rejected %s token: %v", test.name, err)
			}
		})
	}
}

func tokenTestSigner(t *testing.T, privateKey *rsa.PrivateKey) *identity.TokenSigner {
	t.Helper()
	signer, err := identity.NewTokenSigner(identity.TokenSignerConfig{
		Issuer: tokenTestIssuer, AdminAudience: tokenTestAdminAudience, UserAudience: tokenTestUserAudience,
		KeyID: tokenTestKeyID, PrivateKey: privateKey,
	})
	if err != nil {
		t.Fatal(err)
	}
	return signer
}

func tokenTestRequest(
	t *testing.T,
	purpose identity.TokenClientPurpose,
	application api.IdentityApplication,
	subjectID, tenantID, projectID string,
	scopes []string,
	authority identity.TokenSigningAuthority,
	now time.Time,
) identity.TokenSigningRequest {
	t.Helper()
	request, err := identity.NewTokenSigningRequest(
		purpose, application, subjectID, tenantID, projectID, scopes, authority, now,
	)
	if err != nil {
		t.Fatal(err)
	}
	return request
}

func tokenTestRSAKey(t *testing.T, bits int) *rsa.PrivateKey {
	t.Helper()
	privateKey, err := rsa.GenerateKey(rand.Reader, bits)
	if err != nil {
		t.Fatal(err)
	}
	return privateKey
}

func tokenTestVerifier(t *testing.T, privateKey *rsa.PrivateKey, audience string, authority identity.TokenSigningAuthority, now time.Time) *authn.ConfiguredVerifier {
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
		Algorithm: "RS256", Exponent: "AQAB", KeyOps: []string{"verify"}, KeyID: tokenTestKeyID, KeyType: "RSA",
		Modulus: base64.RawURLEncoding.EncodeToString(privateKey.PublicKey.N.Bytes()), Use: "sig",
	})
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := authn.NewConfiguredVerifier(authn.ConfiguredVerifierConfig{
		Issuer: tokenTestIssuer, Audience: audience, Generation: 1, SecurityEpoch: authority.SecurityEpoch,
		NotBefore: authority.NotBefore.Unix(), ExpiresAt: authority.ExpiresAt.Unix(),
		Keys:  []authn.ConfiguredVerifierKey{{JWK: jwk, Enabled: true, NotBefore: authority.KeyNotBefore.Unix(), NotAfter: authority.KeyNotAfter.Unix()}},
		Clock: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(verifier.Invalidate)
	return verifier
}

func tokenTestDecode(t *testing.T, token string) (map[string]any, map[string]any) {
	t.Helper()
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("compact token has %d parts", len(parts))
	}
	decode := func(segment string) map[string]any {
		raw, err := base64.RawURLEncoding.Strict().DecodeString(segment)
		if err != nil {
			t.Fatal(err)
		}
		var value map[string]any
		if err := json.Unmarshal(raw, &value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	return decode(parts[0]), decode(parts[1])
}
