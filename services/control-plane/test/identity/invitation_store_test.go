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
)

func TestInvitationStorePostgresFlow(t *testing.T) {
	serviceURL := os.Getenv("CLOUD_AGENTS_IDENTITY_INVITATION_TEST_DATABASE_URL")
	bootstrapURL := os.Getenv("CLOUD_AGENTS_IDENTITY_INVITATION_TEST_BOOTSTRAP_DATABASE_URL")
	fixtureURL := os.Getenv("CLOUD_AGENTS_IDENTITY_INVITATION_TEST_FIXTURE_DATABASE_URL")
	if serviceURL == "" || bootstrapURL == "" || fixtureURL == "" {
		t.Skip("invitation PostgreSQL test requires fresh service, bootstrap, and fixture database URLs")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	service := emailPolicyPool(t, ctx, serviceURL)
	defer service.Close()
	bootstrap := emailPolicyPool(t, ctx, bootstrapURL)
	defer bootstrap.Close()
	fixture := emailPolicyPool(t, ctx, fixtureURL)
	defer fixture.Close()

	const (
		issuer   = "https://identity.invitation.test"
		password = "invitation correct horse battery staple"
	)
	prepareEmailPolicyFixture(t, ctx, bootstrap, fixture, issuer)
	passwords, err := identity.NewPasswordStore(service, []byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	invitations, err := identity.NewInvitationStore(service, passwords)
	if err != nil {
		t.Fatal(err)
	}
	policies, err := identity.NewEmailPolicyStore(service)
	if err != nil {
		t.Fatal(err)
	}
	login := func(application api.IdentityApplication, email, ip string) api.IdentityLoginResult {
		t.Helper()
		result, err := passwords.PasswordLogin(ctx, application, netip.MustParseAddr(ip), api.PasswordLoginRequest{
			Email: email, Password: "email policy correct horse battery staple",
		})
		if err != nil {
			t.Fatalf("login %s failed: %v", email, err)
		}
		return result
	}
	global := login(api.IdentityApplicationAdmin, "policy-global@example.com", "192.0.2.81")
	tenantAdmin := login(api.IdentityApplicationAdmin, "policy-admin@example.com", "192.0.2.82")
	globalDigest, _ := browserauth.ProofDigest(global.SessionHandle)
	tenantDigest, _ := browserauth.ProofDigest(tenantAdmin.SessionHandle)

	create := func(email, role, level, scope, verification string) api.InvitationCreated {
		t.Helper()
		created, err := invitations.CreateInvitation(ctx, globalDigest, "policy-tenant-a", api.InvitationCreateRequest{
			Email: email, RoleName: role, ScopeLevel: level, ScopeID: scope, Verification: verification,
		})
		if err != nil {
			t.Fatalf("create invitation failed: %v", err)
		}
		if created.InvitationCode == "" || created.Invitation.State != "pending" {
			t.Fatal("create invitation did not return the one-time code and pending state")
		}
		return created
	}

	if _, err := invitations.CreateInvitation(ctx, tenantDigest, "policy-tenant-b", api.InvitationCreateRequest{
		Email: "cross-tenant@example.com", RoleName: "tenant.admin", ScopeLevel: "tenant",
		ScopeID: "policy-tenant-b", Verification: "admin-attested",
	}); !errors.Is(err, identity.ErrForbidden) {
		t.Fatalf("cross-tenant administrator create error = %v", err)
	}
	if _, err := invitations.CreateInvitation(ctx, globalDigest, "policy-tenant-a", api.InvitationCreateRequest{
		Email: "bad-scope@example.com", RoleName: "tenant.admin", ScopeLevel: "tenant",
		ScopeID: "policy-tenant-b", Verification: "admin-attested",
	}); !errors.Is(err, identity.ErrInvitationInvalid) {
		t.Fatalf("tenant scope mismatch error = %v", err)
	}
	if _, err := invitations.CreateInvitation(ctx, globalDigest, "policy-tenant-a", api.InvitationCreateRequest{
		Email: "bad-org@example.com", RoleName: "organization.admin", ScopeLevel: "organization",
		ScopeID: "policy-tenant-b-org", Verification: "admin-attested",
	}); !errors.Is(err, identity.ErrForbidden) {
		t.Fatalf("cross-tenant scope error = %v", err)
	}

	newAccount := create("new.user@EXAMPLE.com", "tenant.admin", "tenant", "policy-tenant-a", "admin-attested")
	unknownCode, _, err := browserauth.NewProof()
	if err != nil {
		t.Fatal(err)
	}
	if err := invitations.AcceptInvitation(ctx, api.IdentityApplicationUser, nil, netip.MustParseAddr("192.0.2.90"), api.InvitationAcceptRequest{
		InvitationCode: unknownCode, Password: password, DisplayName: "Unknown",
	}); !errors.Is(err, browserauth.ErrUnauthorized) {
		t.Fatalf("unknown proof error = %v", err)
	}
	if err := invitations.AcceptInvitation(ctx, api.IdentityApplicationUser, nil, netip.MustParseAddr("192.0.2.90"), api.InvitationAcceptRequest{
		InvitationCode: newAccount.InvitationCode,
	}); !errors.Is(err, identity.ErrInvitationInvalid) {
		t.Fatalf("new account without credentials error = %v", err)
	}
	if err := invitations.AcceptInvitation(ctx, api.IdentityApplicationUser, nil, netip.MustParseAddr("192.0.2.90"), api.InvitationAcceptRequest{
		InvitationCode: newAccount.InvitationCode, Password: password, DisplayName: "New User",
	}); err != nil {
		t.Fatalf("new account acceptance failed: %v", err)
	}
	acceptedDigest, _ := browserauth.ProofDigest(newAccount.InvitationCode)
	acceptedIPBucket := sha256.Sum256([]byte("invitation-ip:192.0.2.90"))
	var acceptedAttempts, acceptedIPFailures int
	if err := fixture.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM cloud_agents_identity.invitation_accept_attempts WHERE code_digest=$1),
		(SELECT count(*) FROM cloud_agents_identity.invitation_accept_failures WHERE ip_bucket=$2)`,
		acceptedDigest[:], acceptedIPBucket[:]).Scan(&acceptedAttempts, &acceptedIPFailures); err != nil {
		t.Fatal(err)
	}
	if acceptedAttempts != 0 || acceptedIPFailures != 0 {
		t.Fatalf("successful acceptance retained attempt state: proof=%d ip=%d", acceptedAttempts, acceptedIPFailures)
	}
	if err := invitations.AcceptInvitation(ctx, api.IdentityApplicationUser, nil, netip.MustParseAddr("192.0.2.90"), api.InvitationAcceptRequest{
		InvitationCode: newAccount.InvitationCode, Password: password, DisplayName: "New User",
	}); !errors.Is(err, identity.ErrInvitationConflict) {
		t.Fatalf("invitation replay error = %v", err)
	}
	newLogin, err := passwords.PasswordLogin(ctx, api.IdentityApplicationUser, netip.MustParseAddr("192.0.2.83"), api.PasswordLoginRequest{
		Email: "new.user@example.com", Password: password,
	})
	if err != nil || len(newLogin.Session.Tenants) != 1 || newLogin.Session.Tenants[0].ID != "policy-tenant-a" {
		t.Fatalf("accepted account login/tenant = %#v, %v", newLogin.Session.Tenants, err)
	}

	proofLimited := create("proof.limit@example.com", "tenant.admin", "tenant", "policy-tenant-a", "admin-attested")
	proofLimitIP := netip.MustParseAddr("192.0.2.92")
	for attempt := 1; attempt <= 10; attempt++ {
		if err := invitations.AcceptInvitation(ctx, api.IdentityApplicationUser, nil, proofLimitIP, api.InvitationAcceptRequest{
			InvitationCode: proofLimited.InvitationCode,
		}); !errors.Is(err, identity.ErrInvitationInvalid) {
			t.Fatalf("matched proof attempt %d error = %v", attempt, err)
		}
	}
	if err := invitations.AcceptInvitation(ctx, api.IdentityApplicationUser, nil, proofLimitIP, api.InvitationAcceptRequest{
		InvitationCode: proofLimited.InvitationCode,
	}); !errors.Is(err, browserauth.ErrUnauthorized) {
		t.Fatalf("matched proof attempt limit error = %v", err)
	}

	unknownLimitedCode, _, err := browserauth.NewProof()
	if err != nil {
		t.Fatal(err)
	}
	unknownLimitIP := netip.MustParseAddr("192.0.2.93")
	for attempt := 1; attempt < 100; attempt++ {
		if err := invitations.AcceptInvitation(ctx, api.IdentityApplicationUser, nil, unknownLimitIP, api.InvitationAcceptRequest{
			InvitationCode: unknownLimitedCode, Password: password, DisplayName: "Unknown",
		}); !errors.Is(err, browserauth.ErrUnauthorized) {
			t.Fatalf("IP attempt %d error = %v", attempt, err)
		}
	}
	if err := invitations.AcceptInvitation(ctx, api.IdentityApplicationUser, nil, unknownLimitIP, api.InvitationAcceptRequest{
		InvitationCode: unknownLimitedCode, Password: password, DisplayName: "Unknown",
	}); !errors.Is(err, identity.ErrRateLimited) {
		t.Fatalf("IP attempt limit error = %v", err)
	}
	if err := invitations.AcceptInvitation(ctx, api.IdentityApplicationUser, nil, netip.MustParseAddr("192.0.2.94"), api.InvitationAcceptRequest{
		InvitationCode: unknownLimitedCode, Password: password, DisplayName: "Unknown",
	}); !errors.Is(err, browserauth.ErrUnauthorized) {
		t.Fatalf("independent IP inherited rate limit: %v", err)
	}

	existingHash, err := browserauth.HashPassword("email policy correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.Exec(ctx, `INSERT INTO cloud_agents_identity.users(id,email,display_name,email_verified_at)
		VALUES ('existing-invitee','existing@example.com','Existing Invitee',clock_timestamp())`); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.Exec(ctx, `INSERT INTO cloud_agents_identity.password_credentials(user_id,password_hash)
		VALUES ('existing-invitee',$1)`, existingHash); err != nil {
		t.Fatal(err)
	}
	existingLogin := login(api.IdentityApplicationUser, "existing@example.com", "192.0.2.84")
	existingDigest, _ := browserauth.ProofDigest(existingLogin.SessionHandle)
	existingInvitation := create("existing@example.com", "tenant.admin", "tenant", "policy-tenant-a", "admin-attested")
	if err := invitations.AcceptInvitation(ctx, api.IdentityApplicationUser, nil, netip.MustParseAddr("192.0.2.90"), api.InvitationAcceptRequest{
		InvitationCode: existingInvitation.InvitationCode, Password: password, DisplayName: "Replacement",
	}); !errors.Is(err, browserauth.ErrUnauthorized) {
		t.Fatalf("existing account unauthenticated acceptance error = %v", err)
	}
	if err := invitations.AcceptInvitation(ctx, api.IdentityApplicationUser, &existingDigest, netip.MustParseAddr("192.0.2.91"), api.InvitationAcceptRequest{
		InvitationCode: existingInvitation.InvitationCode,
	}); err != nil {
		t.Fatalf("existing account acceptance failed: %v", err)
	}
	var membershipsBefore, bindingsBefore int
	if err := fixture.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM cloud_agents.memberships WHERE tenant_id='policy-tenant-a' AND subject_value='user-existing-invitee'),
		(SELECT count(*) FROM cloud_agents.role_bindings WHERE tenant_id='policy-tenant-a' AND subject_value='user-existing-invitee')`).Scan(&membershipsBefore, &bindingsBefore); err != nil {
		t.Fatal(err)
	}
	reuse := create("existing@example.com", "tenant.admin", "tenant", "policy-tenant-a", "admin-attested")
	if err := invitations.AcceptInvitation(ctx, api.IdentityApplicationUser, &existingDigest, netip.MustParseAddr("192.0.2.91"), api.InvitationAcceptRequest{InvitationCode: reuse.InvitationCode}); err != nil {
		t.Fatalf("covering membership reuse failed: %v", err)
	}
	var membershipsAfter, bindingsAfter int
	if err := fixture.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM cloud_agents.memberships WHERE tenant_id='policy-tenant-a' AND subject_value='user-existing-invitee'),
		(SELECT count(*) FROM cloud_agents.role_bindings WHERE tenant_id='policy-tenant-a' AND subject_value='user-existing-invitee')`).Scan(&membershipsAfter, &bindingsAfter); err != nil {
		t.Fatal(err)
	}
	if membershipsAfter != membershipsBefore || bindingsAfter != bindingsBefore {
		t.Fatal("repeat invitation duplicated active membership or role binding")
	}

	blocked := create("blocked@other.example", "tenant.admin", "tenant", "policy-tenant-a", "admin-attested")
	if _, err := policies.UpdateEmailSuffixPolicy(ctx, globalDigest, "policy-tenant-a", api.EmailSuffixPolicyUpdate{
		ExpectedResourceVersion: "0", AllowedDomains: []string{"example.com"},
	}); err != nil {
		t.Fatal(err)
	}
	_, bypassDigest, err := browserauth.NewProof()
	if err != nil {
		t.Fatal(err)
	}
	var bypassRows int
	if err := service.QueryRow(ctx, `SELECT count(*) FROM cloud_agents_identity.create_invitation(
		$1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`, globalDigest[:], "policy-tenant-a",
		"invitation-domain-bypass", bypassDigest[:], "sql-bypass@other.example", "example.com",
		"tenant.admin", "tenant", "policy-tenant-a", "admin-attested", newTestID(t), "invitation-domain-bypass").Scan(&bypassRows); err == nil {
		t.Fatal("identity service function accepted an email/domain mismatch")
	}
	if err := invitations.AcceptInvitation(ctx, api.IdentityApplicationUser, nil, netip.MustParseAddr("192.0.2.90"), api.InvitationAcceptRequest{
		InvitationCode: blocked.InvitationCode, Password: password, DisplayName: "Blocked",
	}); !errors.Is(err, identity.ErrForbidden) {
		t.Fatalf("suffix change acceptance error = %v", err)
	}

	provider := create("provider@example.com", "tenant.admin", "tenant", "policy-tenant-a", "provider-required")
	if err := invitations.AcceptInvitation(ctx, api.IdentityApplicationUser, nil, netip.MustParseAddr("192.0.2.90"), api.InvitationAcceptRequest{
		InvitationCode: provider.InvitationCode, Password: password, DisplayName: "Provider",
	}); !errors.Is(err, identity.ErrForbidden) {
		t.Fatalf("provider-required password acceptance error = %v", err)
	}

	revoked := create("revoked@example.com", "tenant.admin", "tenant", "policy-tenant-a", "admin-attested")
	if err := invitations.RevokeInvitation(ctx, globalDigest, "policy-tenant-a", revoked.Invitation.ID); err != nil {
		t.Fatal(err)
	}
	if err := invitations.RevokeInvitation(ctx, globalDigest, "policy-tenant-a", revoked.Invitation.ID); err != nil {
		t.Fatalf("repeat revoke failed: %v", err)
	}
	if err := invitations.AcceptInvitation(ctx, api.IdentityApplicationUser, nil, netip.MustParseAddr("192.0.2.90"), api.InvitationAcceptRequest{
		InvitationCode: revoked.InvitationCode, Password: password, DisplayName: "Revoked",
	}); !errors.Is(err, identity.ErrInvitationConflict) {
		t.Fatalf("revoked invitation acceptance error = %v", err)
	}
	if err := invitations.RevokeInvitation(ctx, globalDigest, "policy-tenant-a", newAccount.Invitation.ID); !errors.Is(err, identity.ErrInvitationConflict) {
		t.Fatalf("accepted invitation revoke error = %v", err)
	}

	expired := create("expired@example.com", "tenant.admin", "tenant", "policy-tenant-a", "admin-attested")
	if _, err := fixture.Exec(ctx, `UPDATE cloud_agents_identity.invitations
		SET created_at=clock_timestamp()-interval '25 hours', expires_at=clock_timestamp()-interval '2 hours'
		WHERE id=$1`, expired.Invitation.ID); err != nil {
		t.Fatal(err)
	}
	if err := invitations.RevokeInvitation(ctx, globalDigest, "policy-tenant-a", expired.Invitation.ID); !errors.Is(err, identity.ErrInvitationConflict) {
		t.Fatalf("expired invitation revoke error = %v", err)
	}
	if err := invitations.AcceptInvitation(ctx, api.IdentityApplicationUser, nil, netip.MustParseAddr("192.0.2.90"), api.InvitationAcceptRequest{
		InvitationCode: expired.InvitationCode, Password: password, DisplayName: "Expired",
	}); !errors.Is(err, identity.ErrInvitationConflict) {
		t.Fatalf("expired invitation acceptance error = %v", err)
	}

	page, err := invitations.ListInvitations(ctx, globalDigest, "policy-tenant-a", 2, "")
	if err != nil || len(page.Invitations) != 2 || page.NextPageToken == "" {
		t.Fatalf("bounded invitation page = %#v, %v", page, err)
	}
	if _, err := invitations.ListInvitations(ctx, tenantDigest, "policy-tenant-a", 2, page.NextPageToken); !errors.Is(err, identity.ErrInvitationInvalid) {
		t.Fatalf("cross-session cursor error = %v", err)
	}
	if _, err := invitations.ListInvitations(ctx, globalDigest, "policy-tenant-b", 2, page.NextPageToken); !errors.Is(err, identity.ErrInvitationInvalid) {
		t.Fatalf("cross-tenant cursor error = %v", err)
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
	handler, err := identity.NewServer(&emailPolicyHTTPStore{
		PasswordStore: passwords, EmailPolicyStore: policies, InvitationStore: invitations,
	}, identity.ServiceCredentials{AdminWeb: adminProof, UserWeb: userProof, ControlPlane: controlPlaneProof})
	if err != nil {
		t.Fatal(err)
	}
	upstream := httptest.NewTLSServer(handler)
	defer upstream.Close()
	adminClient, err := api.NewIdentityServiceHTTPClientWithClient(upstream.URL, adminProof, upstream.Client())
	if err != nil {
		t.Fatal(err)
	}
	adminHTTPLogin, err := adminClient.PasswordLogin(ctx, "invitation-http-admin-login", "192.0.2.85", api.PasswordLoginRequest{
		Email: "policy-global@example.com", Password: "email policy correct horse battery staple",
	})
	if err != nil {
		t.Fatal(err)
	}
	httpCreated, err := adminClient.CreateInvitation(ctx, adminHTTPLogin.SessionHandle, "policy-tenant-a", "invitation-http-create", adminHTTPLogin.Session.CSRFToken, api.InvitationCreateRequest{
		Email: "http.accept@example.com", RoleName: "tenant.admin", ScopeLevel: "tenant",
		ScopeID: "policy-tenant-a", Verification: "admin-attested",
	})
	if err != nil || httpCreated.InvitationCode == "" {
		t.Fatalf("HTTPS invitation create failed: %v", err)
	}
	httpPage, err := adminClient.ListInvitations(ctx, adminHTTPLogin.SessionHandle, "policy-tenant-a", "invitation-http-list", 200, "")
	if err != nil {
		t.Fatal(err)
	}
	foundHTTP := false
	for _, invitation := range httpPage.Invitations {
		foundHTTP = foundHTTP || invitation.ID == httpCreated.Invitation.ID
	}
	if !foundHTTP {
		t.Fatal("HTTPS invitation list omitted the created invitation")
	}
	userClient, err := api.NewIdentityServiceHTTPClientWithClient(upstream.URL, userProof, upstream.Client())
	if err != nil {
		t.Fatal(err)
	}
	if err := userClient.AcceptInvitation(ctx, "", "invitation-http-accept", "192.0.2.87", "", api.InvitationAcceptRequest{
		InvitationCode: httpCreated.InvitationCode, Password: password, DisplayName: "HTTP Accepted",
	}); err != nil {
		t.Fatalf("HTTPS invitation acceptance failed: %v", err)
	}
	if _, err := userClient.PasswordLogin(ctx, "invitation-http-user-login", "192.0.2.86", api.PasswordLoginRequest{
		Email: "http.accept@example.com", Password: password,
	}); err != nil {
		t.Fatalf("HTTPS accepted account login failed: %v", err)
	}
	httpRevoked, err := adminClient.CreateInvitation(ctx, adminHTTPLogin.SessionHandle, "policy-tenant-a", "invitation-http-create-revoke", adminHTTPLogin.Session.CSRFToken, api.InvitationCreateRequest{
		Email: "http.revoke@example.com", RoleName: "tenant.admin", ScopeLevel: "tenant",
		ScopeID: "policy-tenant-a", Verification: "admin-attested",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := adminClient.RevokeInvitation(ctx, adminHTTPLogin.SessionHandle, "policy-tenant-a", httpRevoked.Invitation.ID, "invitation-http-revoke", adminHTTPLogin.Session.CSRFToken); err != nil {
		t.Fatalf("HTTPS invitation revoke failed: %v", err)
	}

	var blockedUsers, blockedMemberships, revokedAudits int
	if err := fixture.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM cloud_agents_identity.users WHERE email IN ('blocked@other.example','provider@example.com','revoked@example.com','expired@example.com')),
		(SELECT count(*) FROM cloud_agents.memberships WHERE subject_value IN ('user-blocked','user-provider','user-revoked','user-expired')),
		(SELECT count(*) FROM cloud_agents_identity.audit_events WHERE event_kind='invitation_revoked' AND tenant_id='policy-tenant-a')`).Scan(&blockedUsers, &blockedMemberships, &revokedAudits); err != nil {
		t.Fatal(err)
	}
	if blockedUsers != 0 || blockedMemberships != 0 || revokedAudits != 2 {
		t.Fatalf("failed/repeated operations left partial state: users=%d memberships=%d revokeAudits=%d", blockedUsers, blockedMemberships, revokedAudits)
	}
	var directTable, directGate, legacyDirectMembership, currentDirectMembership bool
	if err := fixture.QueryRow(ctx, `SELECT
		has_table_privilege('identity_service_test','cloud_agents_identity.invitations','SELECT'),
		has_function_privilege('identity_service_test','cloud_agents.accept_identity_invitation_v1(text,text,text,text,text,text)','EXECUTE'),
		has_function_privilege('identity_runtime_test','cloud_agents.create_membership(text,bigint,text,text,text,text,text,text,text,timestamptz,text,text)','EXECUTE'),
		has_function_privilege('identity_runtime_test','cloud_agents.create_membership_v2(text,bigint,text,text,text,text,text,text,text,timestamptz,text,text)','EXECUTE')`).Scan(
		&directTable, &directGate, &legacyDirectMembership, &currentDirectMembership,
	); err != nil {
		t.Fatal(err)
	}
	if directTable || directGate {
		t.Fatal("identity service received direct invitation table or Control Plane admission access")
	}
	if legacyDirectMembership || !currentDirectMembership {
		t.Fatal("runtime direct membership admission did not move exclusively to the non-human successor")
	}
	rows, err := fixture.Query(ctx, `SELECT to_jsonb(audit)::text FROM cloud_agents_identity.audit_events AS audit
		WHERE event_kind LIKE 'invitation_%'`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var audit string
		if err := rows.Scan(&audit); err != nil {
			t.Fatal(err)
		}
		for _, secret := range []string{newAccount.InvitationCode, existingInvitation.InvitationCode, httpCreated.InvitationCode, password} {
			if strings.Contains(audit, secret) {
				t.Fatal("invitation audit disclosed a credential")
			}
		}
	}
}
