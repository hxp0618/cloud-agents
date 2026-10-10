package identity_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/netip"
	"os"
	"strings"
	"testing"
	"time"

	common "github.com/hxp0618/cloud-agents/sdk/go/gen/common/v1alpha1"
	api "github.com/hxp0618/cloud-agents/sdk/go/gen/openapi/v1alpha1"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/browserauth"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/identity"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestTokenStorePreservesAuthorizationDenial(t *testing.T) {
	pool := tokenTestPool(t, context.Background(), "postgres://unused@127.0.0.1:1/unused?sslmode=disable")
	defer pool.Close()
	signer := tokenTestSigner(t, tokenTestRSAKey(t, 2048))
	for name, test := range map[string]struct {
		err  error
		want error
	}{
		"revoked membership": {&api.ClientError{Status: 403, Problem: &common.Problem{Status: 403, Error: common.StableError{Code: "AUTHORIZATION_DENIED"}}}, identity.ErrForbidden},
		"invalid problem":    {&api.ClientError{Status: 403, Cause: errors.New("invalid problem")}, identity.ErrUnavailable},
		"service credential": {&api.ClientError{Status: 401}, identity.ErrUnavailable},
		"transport failure":  {errors.New("transport unavailable"), identity.ErrUnavailable},
	} {
		t.Run(name, func(t *testing.T) {
			authorizer := &tokenAuthorizer{err: test.err}
			store, err := identity.NewTokenStore(pool, signer, authorizer, func(context.Context) (identity.TokenSigningAuthority, error) {
				t.Fatal("failed authorization reached signing authority")
				return identity.TokenSigningAuthority{}, identity.ErrUnavailable
			})
			if err != nil {
				t.Fatal(err)
			}
			token, err := store.IssueTenantToken(context.Background(), api.IdentityApplicationUser, [32]byte{}, api.TenantTokenIssueRequest{TenantID: "denied-tenant"})
			if !errors.Is(err, test.want) || token.AccessToken != "" || authorizer.calls != 1 {
				t.Fatalf("authorization error = %v, want %v; calls = %d", err, test.want, authorizer.calls)
			}
		})
	}
}

func TestTokenStorePostgresFlow(t *testing.T) {
	databaseURL := os.Getenv("CLOUD_AGENTS_IDENTITY_TOKEN_TEST_DATABASE_URL")
	bootstrapURL := os.Getenv("CLOUD_AGENTS_IDENTITY_TOKEN_TEST_BOOTSTRAP_DATABASE_URL")
	fixtureURL := os.Getenv("CLOUD_AGENTS_IDENTITY_TOKEN_TEST_FIXTURE_DATABASE_URL")
	if databaseURL == "" || bootstrapURL == "" || fixtureURL == "" {
		t.Skip("identity token PostgreSQL test requires a fresh database with service, bootstrap and fixture URLs")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	servicePool := tokenTestPool(t, ctx, databaseURL)
	defer servicePool.Close()
	bootstrapPool := tokenTestPool(t, ctx, bootstrapURL)
	defer bootstrapPool.Close()
	fixturePool := tokenTestPool(t, ctx, fixtureURL)
	defer fixturePool.Close()

	const (
		issuer   = "https://identity.test.local"
		userID   = "token-user"
		tenantID = "token-tenant"
		password = "token store correct horse battery staple"
	)
	prepareTokenStoreFixture(t, ctx, bootstrapPool, fixturePool, issuer, userID, tenantID, password)
	passwords, err := identity.NewPasswordStore(servicePool, []byte(strings.Repeat("t", 32)))
	if err != nil {
		t.Fatal(err)
	}
	login, err := passwords.PasswordLogin(ctx, api.IdentityApplicationAdmin, netip.MustParseAddr("192.0.2.31"), api.PasswordLoginRequest{
		Email: "token-user@example.com", Password: password,
	})
	if err != nil {
		t.Fatal("token user login failed")
	}
	sessionDigest, err := browserauth.ProofDigest(login.SessionHandle)
	if err != nil {
		t.Fatal(err)
	}
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := identity.NewTokenSigner(identity.TokenSignerConfig{
		Issuer: issuer, AdminAudience: "https://admin-api.identity.test.local", UserAudience: "https://user-api.identity.test.local",
		KeyID: "token-key-1", PrivateKey: privateKey,
	})
	if err != nil {
		t.Fatal(err)
	}
	authorizer := &tokenAuthorizer{userID: userID, issuer: issuer, scopes: []string{"agents.get", "agents.update"}}
	store, err := identity.NewTokenStore(servicePool, signer, authorizer, func(context.Context) (identity.TokenSigningAuthority, error) {
		now := time.Now().UTC()
		return identity.TokenSigningAuthority{
			SecurityEpoch: 3, NotBefore: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour),
			KeyNotBefore: now.Add(-time.Hour), KeyNotAfter: now.Add(time.Hour),
		}, nil
	})
	if err != nil {
		t.Fatal(err)
	}

	issued, err := store.IssueTenantToken(ctx, api.IdentityApplicationAdmin, sessionDigest, api.TenantTokenIssueRequest{TenantID: tenantID})
	if err != nil || issued.AccessToken == "" || issued.TokenType != "Bearer" || strings.Contains(issued.AccessToken, login.SessionHandle) {
		t.Fatalf("token issuance failed: %#v, %v", issued, err)
	}
	if authorizer.calls != 1 || authorizer.last.Application != api.IdentityApplicationAdmin ||
		authorizer.last.SessionSHA256 != "sha256:"+hexDigest(sessionDigest[:]) || authorizer.last.TenantID != tenantID ||
		strings.Contains(authorizer.last.SessionSHA256, login.SessionHandle) {
		t.Fatalf("authorization request = %#v", authorizer.last)
	}
	tokenDigest := sha256.Sum256([]byte(issued.AccessToken))
	statusRequest := api.TokenStatusRequest{
		TokenSHA256: "sha256:" + hexDigest(tokenDigest[:]), ExpectedClientID: identity.AdminWebClientID, ExpectedApplication: api.IdentityApplicationAdmin, ExpectedTenantID: tenantID,
	}
	assertTokenStatus(t, ctx, store, statusRequest, "active")
	projectRequest := statusRequest
	projectRequest.ExpectedProjectID = "any-lower-project"
	assertTokenStatus(t, ctx, store, projectRequest, "active")
	wrongApplication := statusRequest
	wrongApplication.ExpectedApplication = api.IdentityApplicationUser
	wrongApplication.ExpectedClientID = identity.UserWebClientID
	assertTokenStatus(t, ctx, store, wrongApplication, "inactive")
	wrongTenant := statusRequest
	wrongTenant.ExpectedTenantID = "other-tenant"
	assertTokenStatus(t, ctx, store, wrongTenant, "inactive")
	unknown := statusRequest
	unknown.TokenSHA256 = "sha256:" + strings.Repeat("0", 64)
	assertTokenStatus(t, ctx, store, unknown, "inactive")

	beforeRejected := tokenRecordCount(t, ctx, fixturePool, userID)
	for name, test := range map[string]struct {
		application api.IdentityApplication
		request     api.TenantTokenIssueRequest
	}{
		"cross application": {api.IdentityApplicationUser, api.TenantTokenIssueRequest{TenantID: tenantID}},
		"cross tenant":      {api.IdentityApplicationAdmin, api.TenantTokenIssueRequest{TenantID: "other-tenant"}},
		"unknown project":   {api.IdentityApplicationAdmin, api.TenantTokenIssueRequest{TenantID: tenantID, ProjectID: "other-project"}},
	} {
		t.Run(name, func(t *testing.T) {
			result, err := store.IssueTenantToken(ctx, test.application, sessionDigest, test.request)
			if result.AccessToken != "" || !errors.Is(err, browserauth.ErrUnauthorized) {
				t.Fatalf("result = %#v, error = %v", result, err)
			}
		})
	}
	if count := tokenRecordCount(t, ctx, fixturePool, userID); count != beforeRejected {
		t.Fatalf("rejected issuance persisted %d records", count-beforeRejected)
	}
	authorizer.scopes = nil
	if result, err := store.IssueTenantToken(ctx, api.IdentityApplicationAdmin, sessionDigest, api.TenantTokenIssueRequest{TenantID: tenantID}); result.AccessToken != "" || !errors.Is(err, identity.ErrUnavailable) {
		t.Fatalf("empty-scope result = %#v, error = %v", result, err)
	}
	if count := tokenRecordCount(t, ctx, fixturePool, userID); count != beforeRejected {
		t.Fatal("empty CP scope fell back to locally selected scopes")
	}
	authorizer.scopes = []string{"agents.get"}

	leadingDigest := sha256.Sum256([]byte("leading punctuation jti"))
	var recorded bool
	if err := servicePool.QueryRow(ctx, `SELECT cloud_agents_identity.record_issued_token(
		$1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11
	)`, sessionDigest[:], userID, issuer, "admin", tenantID, nil, "-AAAAAAAAAAAAAAAAAAAAA", leadingDigest[:],
		time.Now().Add(5*time.Minute), newTestID(t), newTestID(t)).Scan(&recorded); err != nil || !recorded {
		t.Fatalf("Base64URL JTI beginning with punctuation was rejected: %v", err)
	}
	assertTokenStatus(t, ctx, store, api.TokenStatusRequest{
		TokenSHA256: "sha256:" + hexDigest(leadingDigest[:]), ExpectedClientID: identity.AdminWebClientID, ExpectedApplication: api.IdentityApplicationAdmin, ExpectedTenantID: tenantID,
	}, "active")

	if err := passwords.Logout(ctx, api.IdentityApplicationAdmin, sessionDigest, login.Session.CSRFToken); err != nil {
		t.Fatal(err)
	}
	assertTokenStatus(t, ctx, store, statusRequest, "inactive")

	second, err := passwords.PasswordLogin(ctx, api.IdentityApplicationAdmin, netip.MustParseAddr("192.0.2.32"), api.PasswordLoginRequest{
		Email: "token-user@example.com", Password: password,
	})
	if err != nil {
		t.Fatal(err)
	}
	secondDigest, _ := browserauth.ProofDigest(second.SessionHandle)
	secondToken, err := store.IssueTenantToken(ctx, api.IdentityApplicationAdmin, secondDigest, api.TenantTokenIssueRequest{TenantID: tenantID})
	if err != nil {
		t.Fatal(err)
	}
	secondTokenDigest := sha256.Sum256([]byte(secondToken.AccessToken))
	secondStatus := api.TokenStatusRequest{
		TokenSHA256: "sha256:" + hexDigest(secondTokenDigest[:]), ExpectedClientID: identity.AdminWebClientID, ExpectedApplication: api.IdentityApplicationAdmin, ExpectedTenantID: tenantID,
	}
	if _, err := fixturePool.Exec(ctx, `UPDATE cloud_agents_identity.users SET disabled_at=clock_timestamp() WHERE id=$1`, userID); err != nil {
		t.Fatal(err)
	}
	assertTokenStatus(t, ctx, store, secondStatus, "inactive")

	var tokenColumns int
	if err := fixturePool.QueryRow(ctx, `SELECT count(*) FROM information_schema.columns
		WHERE table_schema='cloud_agents_identity' AND table_name='issued_tokens'
		AND column_name IN ('token','access_token','raw_token')`).Scan(&tokenColumns); err != nil || tokenColumns != 0 {
		t.Fatal("issued token table contains a raw-token column")
	}
	var auditKind, auditDecision, auditReason string
	if err := fixturePool.QueryRow(ctx, `SELECT event_kind,decision,reason_code FROM cloud_agents_identity.audit_events
		WHERE user_id=$1 AND event_kind='token_issued' ORDER BY occurred_at LIMIT 1`, userID).Scan(&auditKind, &auditDecision, &auditReason); err != nil || auditKind != "token_issued" || auditDecision != "allow" || auditReason != "issued" {
		t.Fatal("token issuance audit is missing or malformed")
	}
}

func prepareTokenStoreFixture(t *testing.T, ctx context.Context, bootstrap, fixture *pgxpool.Pool, issuer, userID, tenantID, password string) {
	t.Helper()
	var realmExists bool
	if err := fixture.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM cloud_agents_identity.realm WHERE singleton)`).Scan(&realmExists); err != nil {
		t.Fatal(err)
	}
	if !realmExists {
		passwordHash, err := browserauth.HashPassword("bootstrap password for token store")
		if err != nil {
			t.Fatal(err)
		}
		_, setupDigest, err := browserauth.NewProof()
		if err != nil {
			t.Fatal(err)
		}
		var initialized bool
		if err := bootstrap.QueryRow(ctx, `SELECT cloud_agents_identity.initialize_realm($1,$2,$3,$4,$5,$6,$7,$8)`,
			issuer, setupDigest[:], "token-bootstrap", "token-bootstrap@example.com", "Token Bootstrap", passwordHash,
			newTestID(t), newTestID(t)).Scan(&initialized); err != nil || !initialized {
			t.Fatalf("token realm initialization failed: %v", err)
		}
	}
	passwordHash, err := browserauth.HashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.Exec(ctx, `INSERT INTO cloud_agents_identity.users(id,email,display_name,email_verified_at)
		VALUES ($1,'token-user@example.com','Token User',clock_timestamp())`, userID); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.Exec(ctx, `INSERT INTO cloud_agents_identity.password_credentials(user_id,password_hash) VALUES ($1,$2)`, userID, passwordHash); err != nil {
		t.Fatal(err)
	}
	if _, err := bootstrap.Exec(ctx, `SELECT * FROM cloud_agents.bootstrap_tenant_administrator_v1(
		$1,$1,$1,$1||'-org',$1||'-org',$1||'-org','user',$2,$3,
		$1||'-member',$1||'-member',$1||'-binding',$1||'-binding',
		$1||'-audit-tenant',$1||'-audit-member',$1||'-audit-binding','identity-token-test')`, tenantID, issuer, "user-"+userID); err != nil {
		t.Fatal(err)
	}
}

type tokenAuthorizer struct {
	userID string
	issuer string
	scopes []string
	calls  int
	last   api.TenantTokenAuthorizationRequest
	err    error
}

func (authorizer *tokenAuthorizer) AuthorizeTenantToken(_ context.Context, _ string, request api.TenantTokenAuthorizationRequest) (api.TenantTokenAuthorization, error) {
	authorizer.calls++
	authorizer.last = request
	if authorizer.err != nil {
		return api.TenantTokenAuthorization{}, authorizer.err
	}
	return api.TenantTokenAuthorization{
		UserID: authorizer.userID, Issuer: authorizer.issuer, TenantID: request.TenantID, ProjectID: request.ProjectID,
		Application: request.Application, Scopes: append([]string(nil), authorizer.scopes...),
	}, nil
}

func tokenTestPool(t *testing.T, ctx context.Context, databaseURL string) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	return pool
}

func assertTokenStatus(t *testing.T, ctx context.Context, store *identity.TokenStore, request api.TokenStatusRequest, expected string) {
	t.Helper()
	status, err := store.TokenStatus(ctx, request)
	if err != nil || status.Status != expected {
		t.Fatalf("token status = %#v, error = %v, want %s", status, err, expected)
	}
}

func tokenRecordCount(t *testing.T, ctx context.Context, fixture *pgxpool.Pool, userID string) int {
	t.Helper()
	var count int
	if err := fixture.QueryRow(ctx, `SELECT count(*) FROM cloud_agents_identity.issued_tokens WHERE user_id=$1`, userID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func hexDigest(value []byte) string {
	return hex.EncodeToString(value)
}
