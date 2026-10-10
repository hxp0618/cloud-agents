package identity_test

import (
	"context"
	"errors"
	"net/http/httptest"
	"net/netip"
	"os"
	"strings"
	"testing"
	"time"

	api "github.com/hxp0618/cloud-agents/sdk/go/gen/openapi/v1alpha1"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/browserauth"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/identity"
	"github.com/jackc/pgx/v5/pgxpool"
)

type emailPolicyHTTPStore struct {
	*identity.PasswordStore
	*identity.EmailPolicyStore
	*identity.InvitationStore
	*identity.AccountSecurityStore
}

func (*emailPolicyHTTPStore) JWKS(context.Context) (api.IdentityJWKS, error) {
	return api.IdentityJWKS{}, identity.ErrUnavailable
}

func (*emailPolicyHTTPStore) IssueTenantToken(context.Context, api.IdentityApplication, [32]byte, api.TenantTokenIssueRequest) (api.TenantToken, error) {
	return api.TenantToken{}, identity.ErrUnavailable
}

func (*emailPolicyHTTPStore) TokenStatus(context.Context, api.TokenStatusRequest) (api.TokenStatus, error) {
	return api.TokenStatus{}, identity.ErrUnavailable
}

func TestEmailPolicyStorePostgresAuthorizationAndCAS(t *testing.T) {
	serviceURL := os.Getenv("CLOUD_AGENTS_IDENTITY_EMAIL_POLICY_TEST_DATABASE_URL")
	bootstrapURL := os.Getenv("CLOUD_AGENTS_IDENTITY_EMAIL_POLICY_TEST_BOOTSTRAP_DATABASE_URL")
	fixtureURL := os.Getenv("CLOUD_AGENTS_IDENTITY_EMAIL_POLICY_TEST_FIXTURE_DATABASE_URL")
	if serviceURL == "" || bootstrapURL == "" || fixtureURL == "" {
		t.Skip("email policy PostgreSQL test requires fresh service, bootstrap, and fixture database URLs")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	service := emailPolicyPool(t, ctx, serviceURL)
	defer service.Close()
	bootstrap := emailPolicyPool(t, ctx, bootstrapURL)
	defer bootstrap.Close()
	fixture := emailPolicyPool(t, ctx, fixtureURL)
	defer fixture.Close()

	const issuer = "https://identity.email-policy.test"
	prepareEmailPolicyFixture(t, ctx, bootstrap, fixture, issuer)
	passwords, err := identity.NewPasswordStore(service, []byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	policies, err := identity.NewEmailPolicyStore(service)
	if err != nil {
		t.Fatal(err)
	}
	login := func(email string, ip string) api.IdentityLoginResult {
		result, err := passwords.PasswordLogin(ctx, api.IdentityApplicationAdmin, netip.MustParseAddr(ip), api.PasswordLoginRequest{Email: email, Password: "email policy correct horse battery staple"})
		if err != nil {
			t.Fatalf("login %s failed: %v", email, err)
		}
		return result
	}
	global := login("policy-global@example.com", "192.0.2.61")
	tenantAdmin := login("policy-admin@example.com", "192.0.2.62")
	tenantUser, err := passwords.PasswordLogin(ctx, api.IdentityApplicationUser, netip.MustParseAddr("192.0.2.63"), api.PasswordLoginRequest{Email: "policy-admin@example.com", Password: "email policy correct horse battery staple"})
	if err != nil {
		t.Fatal(err)
	}
	globalDigest, _ := browserauth.ProofDigest(global.SessionHandle)
	tenantDigest, _ := browserauth.ProofDigest(tenantAdmin.SessionHandle)
	tenantUserDigest, _ := browserauth.ProofDigest(tenantUser.SessionHandle)
	if _, err := policies.GetEmailSuffixPolicy(ctx, tenantUserDigest, "policy-tenant-a"); !errors.Is(err, browserauth.ErrUnauthorized) {
		t.Fatalf("User application session error = %v", err)
	}

	missing, err := policies.GetEmailSuffixPolicy(ctx, globalDigest, "policy-tenant-b")
	if err != nil || missing.ResourceVersion != "0" || len(missing.AllowedDomains) != 0 {
		t.Fatalf("missing policy = %#v, %v", missing, err)
	}
	created, err := policies.UpdateEmailSuffixPolicy(ctx, globalDigest, "policy-tenant-b", api.EmailSuffixPolicyUpdate{
		ExpectedResourceVersion: "0", AllowedDomains: []string{"BÜCHER.EXAMPLE", "Example.COM"},
	})
	if err != nil || created.ResourceVersion != "1" || len(created.AllowedDomains) != 2 || created.AllowedDomains[0] != "example.com" || created.AllowedDomains[1] != "xn--bcher-kva.example" {
		t.Fatalf("created policy = %#v, %v", created, err)
	}
	if _, err := policies.UpdateEmailSuffixPolicy(ctx, globalDigest, "policy-tenant-b", api.EmailSuffixPolicyUpdate{
		ExpectedResourceVersion: "0", AllowedDomains: []string{},
	}); !errors.Is(err, identity.ErrEmailPolicyConflict) {
		t.Fatalf("stale create error = %v", err)
	}
	if _, err := policies.UpdateEmailSuffixPolicy(ctx, globalDigest, "policy-tenant-b", api.EmailSuffixPolicyUpdate{
		ExpectedResourceVersion: "1", AllowedDomains: []string{"EXAMPLE.com", "example.com"},
	}); !errors.Is(err, identity.ErrEmailPolicyInvalid) {
		t.Fatalf("canonical duplicate error = %v", err)
	}

	if _, err := policies.UpdateEmailSuffixPolicy(ctx, tenantDigest, "policy-tenant-a", api.EmailSuffixPolicyUpdate{
		ExpectedResourceVersion: "0", AllowedDomains: []string{"example.com"},
	}); err != nil {
		t.Fatalf("tenant administrator update failed: %v", err)
	}
	if _, err := policies.GetEmailSuffixPolicy(ctx, tenantDigest, "policy-tenant-b"); !errors.Is(err, identity.ErrForbidden) {
		t.Fatalf("cross-tenant read error = %v", err)
	}
	adminProof, _, err := browserauth.NewProof()
	if err != nil {
		t.Fatal(err)
	}
	userProof, _, err := browserauth.NewProof()
	if err != nil {
		t.Fatal(err)
	}
	controlPlaneProof, _, err := browserauth.NewProof()
	if err != nil {
		t.Fatal(err)
	}
	invitations, err := identity.NewInvitationStore(service, passwords)
	if err != nil {
		t.Fatal(err)
	}
	handler, err := identity.NewServer(&emailPolicyHTTPStore{PasswordStore: passwords, EmailPolicyStore: policies, InvitationStore: invitations}, identity.ServiceCredentials{
		AdminWeb: adminProof, UserWeb: userProof, ControlPlane: controlPlaneProof,
	})
	if err != nil {
		t.Fatal(err)
	}
	upstream := httptest.NewTLSServer(handler)
	defer upstream.Close()
	adminClient, err := api.NewIdentityServiceHTTPClientWithClient(upstream.URL, adminProof, upstream.Client())
	if err != nil {
		t.Fatal(err)
	}
	adminLogin, err := adminClient.PasswordLogin(ctx, "policy-http-login", "192.0.2.64", api.PasswordLoginRequest{Email: "policy-global@example.com", Password: "email policy correct horse battery staple"})
	if err != nil {
		t.Fatal(err)
	}
	httpPolicy, err := adminClient.GetEmailSuffixPolicy(ctx, adminLogin.SessionHandle, "policy-tenant-b", "policy-http-read")
	if err != nil || httpPolicy.ResourceVersion != "1" {
		t.Fatalf("HTTPS email policy read = %#v, %v", httpPolicy, err)
	}
	httpPolicy, err = adminClient.UpdateEmailSuffixPolicy(ctx, adminLogin.SessionHandle, "policy-tenant-b", "policy-http-update", adminLogin.Session.CSRFToken, api.EmailSuffixPolicyUpdate{
		ExpectedResourceVersion: "1", AllowedDomains: []string{"example.com"},
	})
	if err != nil || httpPolicy.ResourceVersion != "2" || len(httpPolicy.AllowedDomains) != 1 {
		t.Fatalf("HTTPS email policy update = %#v, %v", httpPolicy, err)
	}
	assertEmailPolicyHTTPStatus(t, func() error {
		_, err := adminClient.UpdateEmailSuffixPolicy(ctx, adminLogin.SessionHandle, "policy-tenant-b", "policy-http-stale", adminLogin.Session.CSRFToken, api.EmailSuffixPolicyUpdate{ExpectedResourceVersion: "1", AllowedDomains: []string{}})
		return err
	}(), 409)
	assertEmailPolicyHTTPStatus(t, func() error {
		_, err := adminClient.UpdateEmailSuffixPolicy(ctx, adminLogin.SessionHandle, "policy-tenant-b", "policy-http-csrf", strings.Repeat("a", 43), api.EmailSuffixPolicyUpdate{ExpectedResourceVersion: "2", AllowedDomains: []string{}})
		return err
	}(), 403)
	tenantAdminLogin, err := adminClient.PasswordLogin(ctx, "policy-http-tenant-login", "192.0.2.65", api.PasswordLoginRequest{Email: "policy-admin@example.com", Password: "email policy correct horse battery staple"})
	if err != nil {
		t.Fatal(err)
	}
	assertEmailPolicyHTTPStatus(t, func() error {
		_, err := adminClient.GetEmailSuffixPolicy(ctx, tenantAdminLogin.SessionHandle, "policy-tenant-b", "policy-http-cross-tenant")
		return err
	}(), 403)
	userClient, err := api.NewIdentityServiceHTTPClientWithClient(upstream.URL, userProof, upstream.Client())
	if err != nil {
		t.Fatal(err)
	}
	userLogin, err := userClient.PasswordLogin(ctx, "policy-http-user-login", "192.0.2.66", api.PasswordLoginRequest{Email: "policy-admin@example.com", Password: "email policy correct horse battery staple"})
	if err != nil {
		t.Fatal(err)
	}
	assertEmailPolicyHTTPStatus(t, func() error {
		_, err := userClient.GetEmailSuffixPolicy(ctx, userLogin.SessionHandle, "policy-tenant-a", "policy-http-user-purpose")
		return err
	}(), 403)
	adminHTTPDigest, err := browserauth.ProofDigest(adminLogin.SessionHandle)
	if err != nil {
		t.Fatal(err)
	}
	if err := passwords.Logout(ctx, api.IdentityApplicationAdmin, adminHTTPDigest, adminLogin.Session.CSRFToken); err != nil {
		t.Fatal(err)
	}
	assertEmailPolicyHTTPStatus(t, func() error {
		_, err := adminClient.GetEmailSuffixPolicy(ctx, adminLogin.SessionHandle, "policy-tenant-b", "policy-http-revoked")
		return err
	}(), 401)

	for _, transition := range []struct{ deny, restore string }{
		{`UPDATE cloud_agents.role_bindings SET state='revoked' WHERE tenant_id='policy-tenant-a'`, `UPDATE cloud_agents.role_bindings SET state='active' WHERE tenant_id='policy-tenant-a'`},
		{`UPDATE cloud_agents.role_bindings SET expires_at=clock_timestamp()-interval '1 second' WHERE tenant_id='policy-tenant-a'`, `UPDATE cloud_agents.role_bindings SET expires_at=NULL WHERE tenant_id='policy-tenant-a'`},
		{`UPDATE cloud_agents.memberships SET state='suspended' WHERE tenant_id='policy-tenant-a'`, `UPDATE cloud_agents.memberships SET state='active' WHERE tenant_id='policy-tenant-a'`},
		{`UPDATE cloud_agents.memberships SET expires_at=clock_timestamp()-interval '1 second' WHERE tenant_id='policy-tenant-a'`, `UPDATE cloud_agents.memberships SET expires_at=NULL WHERE tenant_id='policy-tenant-a'`},
		{`UPDATE cloud_agents.resource_changes SET change_kind='updated' WHERE tenant_id='policy-tenant-a' AND resource_kind='membership'`, `UPDATE cloud_agents.resource_changes SET change_kind='created' WHERE tenant_id='policy-tenant-a' AND resource_kind='membership'`},
	} {
		if _, err := fixture.Exec(ctx, transition.deny); err != nil {
			t.Fatal(err)
		}
		if _, err := policies.GetEmailSuffixPolicy(ctx, tenantDigest, "policy-tenant-a"); !errors.Is(err, identity.ErrForbidden) {
			t.Fatalf("revoked tenant authority error = %v", err)
		}
		if _, err := fixture.Exec(ctx, transition.restore); err != nil {
			t.Fatal(err)
		}
	}
	for _, transition := range []struct{ deny, restore string }{
		{`UPDATE cloud_agents_identity.sessions SET revoked_at=clock_timestamp() WHERE user_id='policy-admin' AND application='admin'`, `UPDATE cloud_agents_identity.sessions SET revoked_at=NULL WHERE user_id='policy-admin' AND application='admin'`},
		{`UPDATE cloud_agents_identity.sessions SET last_seen_at=clock_timestamp()-interval '31 minutes' WHERE user_id='policy-admin' AND application='admin'`, `UPDATE cloud_agents_identity.sessions SET last_seen_at=clock_timestamp() WHERE user_id='policy-admin' AND application='admin'`},
	} {
		if _, err := fixture.Exec(ctx, transition.deny); err != nil {
			t.Fatal(err)
		}
		if _, err := policies.GetEmailSuffixPolicy(ctx, tenantDigest, "policy-tenant-a"); !errors.Is(err, browserauth.ErrUnauthorized) {
			t.Fatalf("inactive administrator session error = %v", err)
		}
		if _, err := fixture.Exec(ctx, transition.restore); err != nil {
			t.Fatal(err)
		}
	}

	updated, err := policies.UpdateEmailSuffixPolicy(ctx, tenantDigest, "policy-tenant-a", api.EmailSuffixPolicyUpdate{
		ExpectedResourceVersion: "1", AllowedDomains: []string{},
	})
	if err != nil || updated.ResourceVersion != "2" || len(updated.AllowedDomains) != 0 {
		t.Fatalf("empty policy update = %#v, %v", updated, err)
	}
	var audits, memberships int
	if err := fixture.QueryRow(ctx, `SELECT count(*) FROM cloud_agents_identity.audit_events WHERE event_kind='policy_updated' AND tenant_id IN ('policy-tenant-a','policy-tenant-b')`).Scan(&audits); err != nil || audits != 4 {
		t.Fatalf("policy audit count = %d, %v", audits, err)
	}
	if err := fixture.QueryRow(ctx, `SELECT count(*) FROM cloud_agents.memberships WHERE tenant_id IN ('policy-tenant-a','policy-tenant-b')`).Scan(&memberships); err != nil || memberships != 2 {
		t.Fatalf("policy update changed memberships: %d, %v", memberships, err)
	}
	var directTableAccess bool
	if err := fixture.QueryRow(ctx, `SELECT has_table_privilege('identity_service_test','cloud_agents_identity.tenant_email_policies','SELECT')`).Scan(&directTableAccess); err != nil || directTableAccess {
		t.Fatal("identity service received direct email policy table access")
	}
	var directGateAccess bool
	if err := fixture.QueryRow(ctx, `SELECT has_function_privilege('identity_service_test','cloud_agents_identity.require_tenant_admin(bytea,text)','EXECUTE')`).Scan(&directGateAccess); err != nil || directGateAccess {
		t.Fatal("identity service received direct reusable administrator-gate access")
	}
	if _, err := fixture.Exec(ctx, `UPDATE cloud_agents_identity.users SET disabled_at=clock_timestamp() WHERE id='policy-admin'`); err != nil {
		t.Fatal(err)
	}
	if _, err := policies.GetEmailSuffixPolicy(ctx, tenantDigest, "policy-tenant-a"); !errors.Is(err, browserauth.ErrUnauthorized) {
		t.Fatalf("disabled administrator error = %v", err)
	}
}

func assertEmailPolicyHTTPStatus(t *testing.T, err error, status int) {
	t.Helper()
	var clientError *api.ClientError
	if !errors.As(err, &clientError) || clientError.Status != status {
		t.Fatalf("email policy HTTP error = %v, want status %d", err, status)
	}
}

func prepareEmailPolicyFixture(t *testing.T, ctx context.Context, bootstrap, fixture *pgxpool.Pool, issuer string) {
	t.Helper()
	passwordHash, err := browserauth.HashPassword("email policy correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	_, setupDigest, err := browserauth.NewProof()
	if err != nil {
		t.Fatal(err)
	}
	var initialized bool
	if err := bootstrap.QueryRow(ctx, `SELECT cloud_agents_identity.initialize_realm($1,$2,$3,$4,$5,$6,$7,$8)`,
		issuer, setupDigest[:], "policy-global", "policy-global@example.com", "Policy Global", passwordHash,
		newTestID(t), newTestID(t)).Scan(&initialized); err != nil || !initialized {
		t.Fatalf("realm initialization failed: %v", err)
	}
	if _, err := fixture.Exec(ctx, `INSERT INTO cloud_agents_identity.users(id,email,display_name,email_verified_at)
		VALUES ('policy-admin','policy-admin@example.com','Policy Admin',clock_timestamp())`); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.Exec(ctx, `INSERT INTO cloud_agents_identity.password_credentials(user_id,password_hash) VALUES ('policy-admin',$1)`, passwordHash); err != nil {
		t.Fatal(err)
	}
	for _, tenant := range []struct{ id, subject string }{{"policy-tenant-a", "user-policy-admin"}, {"policy-tenant-b", "user-unrelated"}} {
		if _, err := bootstrap.Exec(ctx, `SELECT * FROM cloud_agents.bootstrap_tenant_administrator_v1(
			$1,$1,$1,$1||'-org',$1||'-org',$1||'-org','user',$2,$3,
			$1||'-member',$1||'-member',$1||'-binding',$1||'-binding',
			$1||'-audit-tenant',$1||'-audit-member',$1||'-audit-binding','email-policy-test')`, tenant.id, issuer, tenant.subject); err != nil {
			t.Fatal(err)
		}
	}
}

func emailPolicyPool(t *testing.T, ctx context.Context, url string) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	return pool
}
