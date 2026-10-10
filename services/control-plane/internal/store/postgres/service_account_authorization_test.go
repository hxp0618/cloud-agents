package postgres

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authn"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authz"
	"github.com/jackc/pgx/v5"
)

func TestExecuteServiceAccountCreateUsesOneSnapshotAndExactActor(t *testing.T) {
	now := time.Date(2026, time.October, 9, 9, 0, 0, 0, time.UTC)
	const tenantID = "tenant-alpha"
	scope := authz.ScopeRef{Level: authz.ScopeTenant, ID: tenantID}
	membershipPrincipal, bindingPrincipal, _, actor := testServiceAccountPrincipals(t, now, tenantID, "user-operator")
	digest, err := actor.Digest()
	if err != nil {
		t.Fatal(err)
	}
	transaction := &fakeTransaction{rows: []rowScanner{
		rowValues("tenant", tenantID, (*string)(nil), (*string)(nil)),
		rowValues(databaseCatalogVersionFixture(t, 2)),
		rowValues(serviceAccountTenantAdminCandidate(t, actor, digest)),
		rowError(pgx.ErrNoRows),
	}}
	handle := &tenantReadHandle{active: true, transaction: transaction, tenantID: tenantID, application: "admin", clock: func() time.Time { return now }}
	called := 0
	err = withServiceAccountOperations(membershipPrincipal, bindingPrincipal, tenantID, scope, func(membership, binding *authz.VerifiedOperation) error {
		return executeServiceAccountCreate(context.Background(), handle, membership, binding, scope, func(got authz.SubjectRef) error {
			called++
			if got != actor {
				t.Fatalf("actor = %#v", got)
			}
			return nil
		})
	})
	if err != nil || called != 1 {
		t.Fatalf("error/called = %v/%d", err, called)
	}
	if len(transaction.queries) != 4 || transaction.queries[0].sql != resolveAuthorizationScopeSQL ||
		transaction.queries[1].sql != readBuiltinRoleCatalogSQL || transaction.queries[2].sql != readAuthorizationCandidatesSQL {
		t.Fatalf("authorization snapshot queries = %#v", transaction.queries)
	}
}

func TestExecuteServiceAccountCreateRejectsDifferentActorsBeforeDatabase(t *testing.T) {
	now := time.Date(2026, time.October, 9, 9, 0, 0, 0, time.UTC)
	const tenantID = "tenant-alpha"
	scope := authz.ScopeRef{Level: authz.ScopeTenant, ID: tenantID}
	membershipPrincipal, _, _, _ := testServiceAccountPrincipals(t, now, tenantID, "user-operator")
	_, bindingPrincipal, _, _ := testServiceAccountPrincipals(t, now, tenantID, "user-other")
	transaction := &fakeTransaction{}
	handle := &tenantReadHandle{active: true, transaction: transaction, tenantID: tenantID, application: "admin", clock: func() time.Time { return now }}
	called := false
	err := withServiceAccountOperations(membershipPrincipal, bindingPrincipal, tenantID, scope, func(membership, binding *authz.VerifiedOperation) error {
		return executeServiceAccountCreate(context.Background(), handle, membership, binding, scope, func(authz.SubjectRef) error {
			called = true
			return nil
		})
	})
	if !errors.Is(err, authz.ErrOperationDenied) || called || len(transaction.queries) != 0 {
		t.Fatalf("error/called/queries = %v/%v/%#v", err, called, transaction.queries)
	}
}

func withServiceAccountOperations(
	membershipPrincipal, bindingPrincipal *authn.VerifiedPrincipal,
	tenantID string,
	scope authz.ScopeRef,
	callback func(*authz.VerifiedOperation, *authz.VerifiedOperation) error,
) error {
	return authz.WithVerifiedOperation(membershipPrincipal, func(membershipBinder *authz.VerifiedOperationBinder) error {
		membership, err := membershipBinder.Bind(tenantID, scope, permissionMembershipCreate)
		if err != nil {
			return err
		}
		return authz.WithVerifiedOperation(bindingPrincipal, func(bindingBinder *authz.VerifiedOperationBinder) error {
			binding, err := bindingBinder.Bind(tenantID, scope, permissionRoleBindingBind)
			if err != nil {
				return err
			}
			return callback(membership, binding)
		})
	})
}

func testServiceAccountPrincipals(t *testing.T, now time.Time, tenantID, subject string) (*authn.VerifiedPrincipal, *authn.VerifiedPrincipal, *authn.VerifiedPrincipal, authz.SubjectRef) {
	return testServiceAccountPrincipalsForKind(t, now, tenantID, "user", subject)
}

func testServiceAccountPrincipalsForKind(t *testing.T, now time.Time, tenantID, kind, subject string) (*authn.VerifiedPrincipal, *authn.VerifiedPrincipal, *authn.VerifiedPrincipal, authz.SubjectRef) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	const issuer = "https://service-account.test"
	const audience = "https://service-account.test/control-plane"
	jwk, _ := json.Marshal(map[string]any{
		"alg": "RS256", "e": "AQAB", "key_ops": []string{"verify"}, "kid": "service-account-key",
		"kty": "RSA", "n": base64.RawURLEncoding.EncodeToString(key.PublicKey.N.Bytes()), "use": "sig",
	})
	verifier, err := authn.NewConfiguredVerifier(authn.ConfiguredVerifierConfig{
		Issuer: issuer, Audience: audience, Generation: 1, SecurityEpoch: 1,
		NotBefore: now.Add(-time.Minute).Unix(), ExpiresAt: now.Add(10 * time.Minute).Unix(),
		Keys:  []authn.ConfiguredVerifierKey{{JWK: jwk, Enabled: true, NotBefore: now.Add(-time.Minute).Unix(), NotAfter: now.Add(10 * time.Minute).Unix()}},
		Clock: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(verifier.Invalidate)
	header, _ := json.Marshal(map[string]any{"alg": "RS256", "kid": "service-account-key", "typ": "at+jwt"})
	claims, _ := json.Marshal(map[string]any{
		"iss": issuer, "sub": subject, "aud": audience, "exp": now.Add(5 * time.Minute).Unix(), "iat": now.Unix(),
		"jti": "service-account-token", "client_id": "cloud-agents-admin-web", "scope": permissionMembershipCreate + " " + permissionRoleBindingBind + " tenants.get",
		"https://schemas.cloud-agents.dev/claims/subject-kind":   kind,
		"https://schemas.cloud-agents.dev/claims/tenant-id":      tenantID,
		"https://schemas.cloud-agents.dev/claims/security-epoch": int64(1),
		"https://schemas.cloud-agents.dev/claims/token-profile":  "cloud-agents-access-token/v1",
	})
	protected := base64.RawURLEncoding.EncodeToString(header)
	payload := base64.RawURLEncoding.EncodeToString(claims)
	digest := sha256.Sum256([]byte(protected + "." + payload))
	signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	token := protected + "." + payload + "." + base64.RawURLEncoding.EncodeToString(signature)
	membershipPrincipal, err := verifier.Verify(token, authn.VerificationRequest{
		TenantID: tenantID, ResourceLevel: string(authz.ScopeTenant), ResourceID: tenantID, RequiredPermission: permissionMembershipCreate,
	})
	if err != nil {
		t.Fatal(err)
	}
	bindingPrincipal, err := verifier.Verify(token, authn.VerificationRequest{
		TenantID: tenantID, ResourceLevel: string(authz.ScopeTenant), ResourceID: tenantID, RequiredPermission: permissionRoleBindingBind,
	})
	if err != nil {
		t.Fatal(err)
	}
	identityPrincipal, err := verifier.Verify(token, authn.VerificationRequest{
		TenantID: tenantID, ResourceLevel: string(authz.ScopeTenant), ResourceID: tenantID, RequiredPermission: "tenants.get",
	})
	if err != nil {
		t.Fatal(err)
	}
	return membershipPrincipal, bindingPrincipal, identityPrincipal, authz.SubjectRef{Kind: kind, Issuer: issuer, Subject: subject}
}

func serviceAccountTenantAdminCandidate(t *testing.T, subject authz.SubjectRef, digest string) []byte {
	t.Helper()
	scope := map[string]any{"level": "tenant", "tenant_id": "tenant-alpha", "organization_id": "", "project_id": ""}
	return mustJSON(t, []any{map[string]any{
		"membership": map[string]any{"uid": "membership-operator", "subject_kind": subject.Kind, "subject_issuer": subject.Issuer, "subject_value": subject.Subject, "subject_digest": digest, "scope": scope, "state": "active", "expires_at": nil},
		"binding":    map[string]any{"uid": "binding-operator", "subject_kind": subject.Kind, "subject_issuer": subject.Issuer, "subject_value": subject.Subject, "subject_digest": digest, "role_name": "tenant.admin", "role_version": int64(1), "scope": scope, "state": "active", "expires_at": nil},
	}})
}
