package identity_test

import (
	"context"
	"crypto/sha256"
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
	"github.com/jackc/pgx/v5/pgtype"
)

func TestAccountSecurityPostgresAndHTTPSFlow(t *testing.T) {
	serviceURL := os.Getenv("CLOUD_AGENTS_IDENTITY_ACCOUNT_SECURITY_TEST_DATABASE_URL")
	bootstrapURL := os.Getenv("CLOUD_AGENTS_IDENTITY_ACCOUNT_SECURITY_TEST_BOOTSTRAP_DATABASE_URL")
	fixtureURL := os.Getenv("CLOUD_AGENTS_IDENTITY_ACCOUNT_SECURITY_TEST_FIXTURE_DATABASE_URL")
	if serviceURL == "" || bootstrapURL == "" || fixtureURL == "" {
		t.Skip("account security PostgreSQL test requires service, bootstrap, and fixture database URLs")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	service := emailPolicyPool(t, ctx, serviceURL)
	defer service.Close()
	bootstrap := emailPolicyPool(t, ctx, bootstrapURL)
	defer bootstrap.Close()
	fixture := emailPolicyPool(t, ctx, fixtureURL)
	defer fixture.Close()
	prepareEmailPolicyFixture(t, ctx, bootstrap, fixture, "https://identity.account-security.test")
	passwords, err := identity.NewPasswordStore(service, []byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	security, err := identity.NewAccountSecurityStore(service, passwords)
	if err != nil {
		t.Fatal(err)
	}
	login := func(app api.IdentityApplication, email, password, ip string) api.IdentityLoginResult {
		t.Helper()
		result, err := passwords.PasswordLogin(ctx, app, netip.MustParseAddr(ip), api.PasswordLoginRequest{Email: email, Password: password})
		if err != nil {
			t.Fatalf("login %s: %v", email, err)
		}
		return result
	}
	const oldPassword = "email policy correct horse battery staple"
	global := login(api.IdentityApplicationAdmin, "policy-global@example.com", oldPassword, "192.0.2.141")
	globalDigest, _ := browserauth.ProofDigest(global.SessionHandle)
	page, err := security.ListAccounts(ctx, globalDigest, 1, "")
	if err != nil || len(page.Accounts) != 1 || page.NextPageToken == "" {
		t.Fatalf("account page=%#v err=%v", page, err)
	}
	page2, err := security.ListAccounts(ctx, globalDigest, 200, page.NextPageToken)
	if err != nil || len(page2.Accounts) < 1 {
		t.Fatalf("second account page=%#v err=%v", page2, err)
	}
	if err := security.DisableAccount(ctx, globalDigest, "policy-global"); !errors.Is(err, identity.ErrAccountSecurityConflict) {
		t.Fatalf("last platform admin disable=%v", err)
	}
	if err := security.ChangePassword(ctx, api.IdentityApplicationAdmin, globalDigest, api.PasswordChangeRequest{CurrentPassword: "wrong password", NewPassword: "new account security password"}); !errors.Is(err, browserauth.ErrUnauthorized) {
		t.Fatalf("wrong current password=%v", err)
	}
	if err := security.ChangePassword(ctx, api.IdentityApplicationAdmin, globalDigest, api.PasswordChangeRequest{CurrentPassword: oldPassword, NewPassword: "new account security password"}); err != nil {
		t.Fatal(err)
	}
	if _, err := passwords.Session(ctx, api.IdentityApplicationAdmin, globalDigest); !errors.Is(err, browserauth.ErrUnauthorized) {
		t.Fatalf("changed-password session remained active: %v", err)
	}
	global = login(api.IdentityApplicationAdmin, "policy-global@example.com", "new account security password", "192.0.2.142")
	globalDigest, _ = browserauth.ProofDigest(global.SessionHandle)
	admin := login(api.IdentityApplicationUser, "policy-admin@example.com", oldPassword, "192.0.2.143")
	adminDigest, _ := browserauth.ProofDigest(admin.SessionHandle)
	reset, err := security.IssuePasswordReset(ctx, globalDigest, "policy-admin")
	if err != nil || reset.ResetCode == "" {
		t.Fatalf("reset=%#v err=%v", reset, err)
	}
	resetDigest, _ := browserauth.ProofDigest(reset.ResetCode)
	matchedIPBucket := sha256.Sum256([]byte("password-reset-ip:192.0.2.150"))
	canceledCtx, cancelAttempt := context.WithCancel(ctx)
	cancelAttempt()
	if err := security.AcceptPasswordReset(canceledCtx, netip.MustParseAddr("192.0.2.150"), api.PasswordResetAcceptRequest{ResetCode: reset.ResetCode, NewPassword: "reset account security password"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled matched reset attempt=%v", err)
	}
	for range 9 {
		var matchedUser pgtype.Text
		var permitted bool
		if err := service.QueryRow(ctx, `SELECT user_id,permitted FROM cloud_agents_identity.prepare_password_reset($1,$2)`, resetDigest[:], matchedIPBucket[:]).Scan(&matchedUser, &permitted); err != nil || !permitted || !matchedUser.Valid {
			t.Fatalf("matched reset attempt was not persisted: user=%#v permitted=%t err=%v", matchedUser, permitted, err)
		}
	}
	if err := security.AcceptPasswordReset(ctx, netip.MustParseAddr("192.0.2.150"), api.PasswordResetAcceptRequest{ResetCode: reset.ResetCode, NewPassword: "reset account security password"}); !errors.Is(err, browserauth.ErrUnauthorized) {
		t.Fatalf("matched reset attempt limit=%v", err)
	}
	reset, err = security.IssuePasswordReset(ctx, globalDigest, "policy-admin")
	if err != nil {
		t.Fatal(err)
	}
	if err := security.AcceptPasswordReset(ctx, netip.MustParseAddr("192.0.2.144"), api.PasswordResetAcceptRequest{ResetCode: reset.ResetCode, NewPassword: "reset account security password"}); err != nil {
		t.Fatal(err)
	}
	if err := security.AcceptPasswordReset(ctx, netip.MustParseAddr("192.0.2.144"), api.PasswordResetAcceptRequest{ResetCode: reset.ResetCode, NewPassword: "another account security password"}); !errors.Is(err, browserauth.ErrUnauthorized) {
		t.Fatalf("reset replay=%v", err)
	}
	unknownCode, _, _ := browserauth.NewProof()
	for attempt := 0; attempt < 100; attempt++ {
		if err := security.AcceptPasswordReset(ctx, netip.MustParseAddr("192.0.2.151"), api.PasswordResetAcceptRequest{ResetCode: unknownCode, NewPassword: "unknown reset security password"}); !errors.Is(err, browserauth.ErrUnauthorized) {
			t.Fatalf("unknown reset attempt %d=%v", attempt, err)
		}
	}
	if err := security.AcceptPasswordReset(ctx, netip.MustParseAddr("192.0.2.151"), api.PasswordResetAcceptRequest{ResetCode: unknownCode, NewPassword: "unknown reset security password"}); !errors.Is(err, identity.ErrRateLimited) {
		t.Fatalf("reset IP limit=%v", err)
	}
	if _, err := passwords.Session(ctx, api.IdentityApplicationUser, adminDigest); !errors.Is(err, browserauth.ErrUnauthorized) {
		t.Fatalf("reset session remained active: %v", err)
	}
	_ = login(api.IdentityApplicationUser, "policy-admin@example.com", "reset account security password", "192.0.2.145")
	tenantAdmin := login(api.IdentityApplicationAdmin, "policy-admin@example.com", "reset account security password", "192.0.2.149")
	tenantAdminDigest, _ := browserauth.ProofDigest(tenantAdmin.SessionHandle)
	tenantAccounts, err := security.ListTenantAccounts(ctx, tenantAdminDigest, "policy-tenant-a", 200, "")
	if err != nil || len(tenantAccounts.Accounts) != 1 || tenantAccounts.Accounts[0].ID != "policy-admin" || tenantAccounts.Accounts[0].Subject.Kind != "user" || tenantAccounts.Accounts[0].Subject.Issuer != "https://identity.account-security.test" || tenantAccounts.Accounts[0].Subject.Subject != "user-policy-admin" {
		t.Fatalf("tenant accounts=%#v err=%v", tenantAccounts, err)
	}
	if _, err := security.ListTenantAccounts(ctx, tenantAdminDigest, "policy-tenant-b", 200, ""); !errors.Is(err, identity.ErrForbidden) {
		t.Fatalf("cross-tenant accounts=%v", err)
	}
	policies, err := identity.NewEmailPolicyStore(service)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := policies.UpdateEmailSuffixPolicy(ctx, globalDigest, "policy-tenant-a", api.EmailSuffixPolicyUpdate{ExpectedResourceVersion: "0", AllowedDomains: []string{"example.com"}}); err != nil {
		t.Fatal(err)
	}
	audit, err := security.ListAuditEvents(ctx, globalDigest, "", 2, "")
	if err != nil || len(audit.Events) != 2 || audit.NextPageToken == "" {
		t.Fatalf("audit=%#v err=%v", audit, err)
	}
	if strings.Contains(auditJSON(t, audit), reset.ResetCode) {
		t.Fatal("audit exposed reset proof")
	}
	tenantAudit, err := security.ListAuditEvents(ctx, tenantAdminDigest, "policy-tenant-a", 200, "")
	if err != nil || len(tenantAudit.Events) != 1 || tenantAudit.Events[0].TenantID != "policy-tenant-a" {
		t.Fatalf("tenant audit=%v", err)
	}

	memberAudit, err := security.ListControlPlaneAuditEvents(ctx, tenantAdminDigest, "policy-tenant-a", 1, "")
	if err != nil || len(memberAudit.Events) != 1 || memberAudit.NextPageToken == "" || memberAudit.Events[0].Actor != nil || memberAudit.Events[0].CorrelationID != "" {
		t.Fatalf("historical member audit=%#v err=%v", memberAudit, err)
	}
	memberAuditNext, err := security.ListControlPlaneAuditEvents(ctx, tenantAdminDigest, "policy-tenant-a", 1, memberAudit.NextPageToken)
	if err != nil || len(memberAuditNext.Events) != 1 || memberAuditNext.Events[0].ID == memberAudit.Events[0].ID || memberAuditNext.NextPageToken != "" {
		t.Fatalf("member audit next page=%#v err=%v", memberAuditNext, err)
	}
	for _, boundary := range []struct {
		name, tenant, cursor string
		digest               [32]byte
		want                 error
	}{
		{"cross tenant", "policy-tenant-b", "", tenantAdminDigest, identity.ErrForbidden},
		{"cursor tenant", "policy-tenant-b", memberAudit.NextPageToken, tenantAdminDigest, identity.ErrAccountSecurityInvalid},
		{"cursor session", "policy-tenant-a", memberAudit.NextPageToken, globalDigest, identity.ErrAccountSecurityInvalid},
		{"cursor namespace", "policy-tenant-a", audit.NextPageToken, globalDigest, identity.ErrAccountSecurityInvalid},
	} {
		if _, err := security.ListControlPlaneAuditEvents(ctx, boundary.digest, boundary.tenant, 1, boundary.cursor); !errors.Is(err, boundary.want) {
			t.Fatalf("%s member audit error=%v want=%v", boundary.name, err, boundary.want)
		}
	}

	adminProof, _, _ := browserauth.NewProof()
	userProof, _, _ := browserauth.NewProof()
	cpProof, _, _ := browserauth.NewProof()
	invitations, _ := identity.NewInvitationStore(service, passwords)
	handler, err := identity.NewServer(&emailPolicyHTTPStore{PasswordStore: passwords, EmailPolicyStore: policies, InvitationStore: invitations, AccountSecurityStore: security}, identity.ServiceCredentials{AdminWeb: adminProof, UserWeb: userProof, ControlPlane: cpProof})
	if err != nil {
		t.Fatal(err)
	}
	upstream := httptest.NewTLSServer(handler)
	defer upstream.Close()
	client, err := api.NewIdentityServiceHTTPClientWithClient(upstream.URL, adminProof, upstream.Client())
	if err != nil {
		t.Fatal(err)
	}
	loginResult, err := client.PasswordLogin(ctx, "account-security-http-login", "192.0.2.146", api.PasswordLoginRequest{Email: "policy-global@example.com", Password: "new account security password"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.ListIdentityAccounts(ctx, loginResult.SessionHandle, "account-security-http-list", 10, ""); err != nil {
		t.Fatal(err)
	}
	if accounts, err := client.ListTenantIdentityAccounts(ctx, loginResult.SessionHandle, "policy-tenant-a", "account-security-http-tenant-list", 10, ""); err != nil || len(accounts.Accounts) != 1 {
		t.Fatalf("HTTPS tenant accounts=%#v err=%v", accounts, err)
	}
	httpReset, err := client.IssuePasswordReset(ctx, loginResult.SessionHandle, "policy-admin", "account-security-http-reset", loginResult.Session.CSRFToken)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.AcceptPasswordReset(ctx, "account-security-http-reset-accept", "192.0.2.152", api.PasswordResetAcceptRequest{ResetCode: httpReset.ResetCode, NewPassword: "http reset account security password"}); err != nil {
		t.Fatal(err)
	}
	if err := client.ChangePassword(ctx, loginResult.SessionHandle, "account-security-http-change", loginResult.Session.CSRFToken, api.PasswordChangeRequest{CurrentPassword: "new account security password", NewPassword: "final account security password"}); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.Exec(ctx, `INSERT INTO cloud_agents_identity.platform_admins(user_id) VALUES ('policy-admin')`); err != nil {
		t.Fatal(err)
	}
	left := login(api.IdentityApplicationAdmin, "policy-global@example.com", "final account security password", "192.0.2.147")
	right := login(api.IdentityApplicationAdmin, "policy-admin@example.com", "http reset account security password", "192.0.2.148")
	leftDigest, _ := browserauth.ProofDigest(left.SessionHandle)
	rightDigest, _ := browserauth.ProofDigest(right.SessionHandle)
	start, results := make(chan struct{}), make(chan error, 2)
	go func() { <-start; results <- security.DisableAccount(ctx, leftDigest, "policy-admin") }()
	go func() { <-start; results <- security.DisableAccount(ctx, rightDigest, "policy-global") }()
	close(start)
	first, second := <-results, <-results
	if (first == nil) == (second == nil) {
		t.Fatalf("parallel last-admin results=%v,%v", first, second)
	}
	var activeAdmins int
	if err := fixture.QueryRow(ctx, `SELECT count(*) FROM cloud_agents_identity.platform_admins AS admin JOIN cloud_agents_identity.users AS account ON account.id=admin.user_id WHERE admin.revoked_at IS NULL AND account.disabled_at IS NULL`).Scan(&activeAdmins); err != nil || activeAdmins != 1 {
		t.Fatalf("active admins=%d err=%v", activeAdmins, err)
	}
	var disabledID string
	if err := fixture.QueryRow(ctx, `SELECT id FROM cloud_agents_identity.users WHERE disabled_at IS NOT NULL ORDER BY id LIMIT 1`).Scan(&disabledID); err != nil {
		t.Fatal(err)
	}
	disabledDigest := leftDigest
	if disabledID == "policy-admin" {
		disabledDigest = rightDigest
	}
	if _, err := passwords.Session(ctx, api.IdentityApplicationAdmin, disabledDigest); !errors.Is(err, browserauth.ErrUnauthorized) {
		t.Fatalf("disabled session remained active: %v", err)
	}
}

func auditJSON(t *testing.T, value api.IdentityAuditPage) string {
	t.Helper()
	data, err := api.EncodeIdentityAuditPageJSON(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
