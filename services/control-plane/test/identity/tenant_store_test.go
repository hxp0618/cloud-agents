package identity_test

import (
	"context"
	"errors"
	"net/netip"
	"testing"

	api "github.com/hxp0618/cloud-agents/sdk/go/gen/openapi/v1alpha1"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/browserauth"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/identity"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Fixture authority is used only to arrange revocation and expiry in a fresh test database.
func prepareTenantDiscovery(t *testing.T, ctx context.Context, bootstrap, fixture *pgxpool.Pool, passwordHash string) {
	t.Helper()
	if _, err := fixture.Exec(ctx, `
		INSERT INTO cloud_agents_identity.users(id,email,display_name,email_verified_at)
		VALUES ('tenant-member','member@example.com','Tenant Member',clock_timestamp());
	`); err != nil {
		t.Fatal("tenant member fixture failed")
	}
	if _, err := fixture.Exec(ctx, `INSERT INTO cloud_agents_identity.password_credentials(user_id,password_hash) VALUES ('tenant-member',$1)`, passwordHash); err != nil {
		t.Fatal("tenant password fixture failed")
	}
	for _, id := range []string{"tenant-a", "tenant-b", "tenant-c"} {
		subject := "user-tenant-member"
		if id == "tenant-c" {
			subject = "user-unrelated"
		}
		if _, err := bootstrap.Exec(ctx, `SELECT * FROM cloud_agents.bootstrap_tenant_administrator_v1(
			$1,$1,$1,$1||'-org',$1||'-org',$1||'-org','user','https://identity.test.local',$2,
			$1||'-member',$1||'-member',$1||'-binding',$1||'-binding',
			$1||'-audit-tenant',$1||'-audit-member',$1||'-audit-binding','identity-test')`, id, subject); err != nil {
			t.Fatal("tenant bootstrap fixture failed")
		}
	}
	if _, err := fixture.Exec(ctx, `UPDATE cloud_agents.memberships SET expires_at=clock_timestamp()-interval '1 second' WHERE tenant_id='tenant-b'`); err != nil {
		t.Fatal("expired membership fixture failed")
	}
}

func assertTenantDiscovery(t *testing.T, ctx context.Context, store *identity.PasswordStore, fixture *pgxpool.Pool, admin api.IdentityLoginResult, password string) {
	t.Helper()
	digest, _ := browserauth.ProofDigest(admin.SessionHandle)
	if len(admin.Session.Tenants) != 3 {
		t.Fatal("login did not include the authorized initial tenant page")
	}
	first, err := store.Tenants(ctx, api.IdentityApplicationAdmin, digest, 1, "")
	if err != nil || len(first.Tenants) != 1 || first.Tenants[0].ID != "tenant-a" || first.NextPageToken == "" {
		t.Fatal("platform admin first tenant page failed")
	}
	second, err := store.Tenants(ctx, api.IdentityApplicationAdmin, digest, 2, first.NextPageToken)
	if err != nil || len(second.Tenants) != 2 || second.Tenants[0].ID != "tenant-b" || second.Tenants[1].ID != "tenant-c" || second.NextPageToken != "" {
		t.Fatal("platform admin tenant cursor failed")
	}
	if _, err := store.Tenants(ctx, api.IdentityApplicationUser, digest, 1, first.NextPageToken); !errors.Is(err, identity.ErrForbidden) {
		t.Fatal("tenant cursor crossed application boundary")
	}
	request := api.PasswordLoginRequest{Email: "admin@example.com", Password: password}
	user, err := store.PasswordLogin(ctx, api.IdentityApplicationUser, netip.MustParseAddr("192.0.2.20"), request)
	if err != nil {
		t.Fatal("platform account user login failed")
	}
	userDigest, _ := browserauth.ProofDigest(user.SessionHandle)
	userTenants, err := store.Tenants(ctx, api.IdentityApplicationUser, userDigest, 10, "")
	if err != nil || len(userTenants.Tenants) != 0 {
		t.Fatal("global administration became user membership")
	}
	request.Email = "member@example.com"
	member, err := store.PasswordLogin(ctx, api.IdentityApplicationAdmin, netip.MustParseAddr("192.0.2.21"), request)
	if err != nil {
		t.Fatal("tenant administrator login failed")
	}
	memberDigest, _ := browserauth.ProofDigest(member.SessionHandle)
	memberTenants, err := store.Tenants(ctx, api.IdentityApplicationAdmin, memberDigest, 10, "")
	if err != nil || len(memberTenants.Tenants) != 1 || memberTenants.Tenants[0].ID != "tenant-a" || len(memberTenants.Tenants[0].DisplayRoles) != 1 {
		t.Fatal("tenant administrator saw unauthorized or expired tenant")
	}
	if _, err := store.Tenants(ctx, api.IdentityApplicationAdmin, memberDigest, 1, first.NextPageToken); !errors.Is(err, identity.ErrForbidden) {
		t.Fatal("tenant cursor crossed account boundary")
	}
	for _, transition := range []struct{ deny, restore string }{
		{`UPDATE cloud_agents.role_bindings SET state='revoked' WHERE tenant_id='tenant-a'`, `UPDATE cloud_agents.role_bindings SET state='active' WHERE tenant_id='tenant-a'`},
		{`UPDATE cloud_agents.role_bindings SET expires_at=clock_timestamp()-interval '1 second' WHERE tenant_id='tenant-a'`, `UPDATE cloud_agents.role_bindings SET expires_at=NULL WHERE tenant_id='tenant-a'`},
		{`UPDATE cloud_agents.resource_changes SET change_kind='updated' WHERE tenant_id='tenant-a' AND resource_kind='membership'`, `UPDATE cloud_agents.resource_changes SET change_kind='created' WHERE tenant_id='tenant-a' AND resource_kind='membership'`},
	} {
		if _, err := fixture.Exec(ctx, transition.deny); err != nil {
			t.Fatal("role binding rejection fixture failed")
		}
		page, err := store.Tenants(ctx, api.IdentityApplicationAdmin, memberDigest, 10, "")
		if err != nil || len(page.Tenants) != 0 {
			t.Fatal("inactive or unadmitted binding retained tenant administration")
		}
		if _, err := fixture.Exec(ctx, transition.restore); err != nil {
			t.Fatal("role binding fixture restoration failed")
		}
	}
	if _, err := fixture.Exec(ctx, `UPDATE cloud_agents_identity.platform_admins SET revoked_at=clock_timestamp() WHERE user_id='user-bootstrap'`); err != nil {
		t.Fatal("global binding revocation fixture failed")
	}
	afterRevocation, err := store.Tenants(ctx, api.IdentityApplicationAdmin, digest, 2, first.NextPageToken)
	if err != nil || len(afterRevocation.Tenants) != 0 {
		t.Fatal("tenant continuation retained revoked global authority")
	}
	if _, err := fixture.Exec(ctx, `UPDATE cloud_agents_identity.platform_admins SET revoked_at=NULL WHERE user_id='user-bootstrap';
		UPDATE cloud_agents.memberships SET state='suspended' WHERE tenant_id='tenant-a'`); err != nil {
		t.Fatal("membership suspension fixture failed")
	}
	afterSuspension, err := store.Tenants(ctx, api.IdentityApplicationAdmin, memberDigest, 10, "")
	if err != nil || len(afterSuspension.Tenants) != 0 {
		t.Fatal("suspended member retained tenant administration")
	}
	if _, err := fixture.Exec(ctx, `UPDATE cloud_agents_identity.users SET disabled_at=clock_timestamp() WHERE id='tenant-member'`); err != nil {
		t.Fatal("user disable fixture failed")
	}
	if _, err := store.Tenants(ctx, api.IdentityApplicationAdmin, memberDigest, 10, ""); !errors.Is(err, browserauth.ErrUnauthorized) {
		t.Fatal("disabled user retained tenant discovery")
	}
	if err := store.Logout(ctx, api.IdentityApplicationUser, userDigest, user.Session.CSRFToken); err != nil {
		t.Fatal("user fixture session logout failed")
	}
}
