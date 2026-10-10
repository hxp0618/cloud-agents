package identity_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	api "github.com/hxp0618/cloud-agents/sdk/go/gen/openapi/v1alpha1"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authn"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authz"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/identity"
	storepostgres "github.com/hxp0618/cloud-agents/services/control-plane/internal/store/postgres"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestTokenAuthorizationServicePostgresFlow(t *testing.T) {
	runtimeURL := os.Getenv("CLOUD_AGENTS_IDENTITY_AUTHORIZATION_TEST_DATABASE_URL")
	serviceURL := os.Getenv("CLOUD_AGENTS_IDENTITY_AUTHORIZATION_TEST_SERVICE_DATABASE_URL")
	bootstrapURL := os.Getenv("CLOUD_AGENTS_IDENTITY_AUTHORIZATION_TEST_BOOTSTRAP_DATABASE_URL")
	fixtureURL := os.Getenv("CLOUD_AGENTS_IDENTITY_AUTHORIZATION_TEST_FIXTURE_DATABASE_URL")
	if runtimeURL == "" || serviceURL == "" || bootstrapURL == "" || fixtureURL == "" {
		t.Skip("identity authorization PostgreSQL test requires runtime, service, bootstrap and fixture URLs")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	runtime := authorizationTestPool(t, ctx, runtimeURL)
	defer runtime.Close()
	identityService := authorizationTestPool(t, ctx, serviceURL)
	defer identityService.Close()
	bootstrap := authorizationTestPool(t, ctx, bootstrapURL)
	defer bootstrap.Close()
	fixture := authorizationTestPool(t, ctx, fixtureURL)
	defer fixture.Close()

	prefix := "authz-" + newTestID(t)[:12]
	ids := authorizationFixtureIDs{
		prefix: prefix, tenantA: prefix + "-tenant-a", tenantB: prefix + "-tenant-b",
		projectA: prefix + "-project-a", projectB: prefix + "-project-b",
		globalUser: prefix + "-global", tenantAdmin: prefix + "-tenant-admin", projectMember: prefix + "-project-member",
	}
	t.Cleanup(func() { cleanupAuthorizationFixture(t, fixtureURL, ids) })
	issuer, digests := prepareAuthorizationFixture(t, ctx, bootstrap, fixture, ids)
	authorizer, err := storepostgres.NewTokenAuthorizationService(runtime)
	if err != nil {
		t.Fatal(err)
	}

	globalAdmin := authorizeToken(t, ctx, authorizer, api.IdentityApplicationAdmin, digests.globalAdmin, ids.tenantA, "")
	if globalAdmin.UserID != ids.globalUser || !authorizationContains(globalAdmin.Scopes, "tenants.update") {
		t.Fatalf("global administrator tenant A authorization = %#v", globalAdmin)
	}
	if second := authorizeToken(t, ctx, authorizer, api.IdentityApplicationAdmin, digests.globalAdmin, ids.tenantB, ""); second.UserID != ids.globalUser {
		t.Fatalf("global administrator tenant B authorization = %#v", second)
	}
	assertAuthorizationDenied(t, ctx, authorizer, api.IdentityApplicationUser, digests.globalUser, ids.tenantA, "")

	localAdmin := authorizeToken(t, ctx, authorizer, api.IdentityApplicationAdmin, digests.tenantAdmin, ids.tenantA, "")
	if localAdmin.UserID != ids.tenantAdmin || !authorizationContains(localAdmin.Scopes, "tenants.get") {
		t.Fatalf("tenant administrator authorization = %#v", localAdmin)
	}
	assertAuthorizationDenied(t, ctx, authorizer, api.IdentityApplicationAdmin, digests.tenantAdmin, ids.tenantB, "")

	projectMember := authorizeToken(t, ctx, authorizer, api.IdentityApplicationUser, digests.projectMember, ids.tenantA, ids.projectA)
	if projectMember.UserID != ids.projectMember || !authorizationContains(projectMember.Scopes, "projects.get") ||
		!authorizationContains(projectMember.Scopes, "environment-profiles.list") ||
		!authorizationContains(projectMember.Scopes, "environment-quotas.get") ||
		!authorizationContains(projectMember.Scopes, "environments.get") {
		t.Fatalf("project member authorization = %#v", projectMember)
	}
	for _, forbidden := range []string{"environments.create", "remote-worker-bootstrap.act", "sandboxes.update", "sessions.get"} {
		if authorizationContains(projectMember.Scopes, forbidden) {
			t.Fatalf("project member scopes %v contain %q", projectMember.Scopes, forbidden)
		}
	}
	assertAuthorizationDenied(t, ctx, authorizer, api.IdentityApplicationUser, digests.projectMember, ids.tenantA, ids.projectB)

	selector, err := storepostgres.NewDurableCoordinationService(runtime)
	if err != nil {
		t.Fatal(err)
	}
	principalFor := newMyProjectsPrincipalFactory(t, issuer)
	memberPage, err := selector.ListMyProjects(ctx, ids.tenantA, principalFor(ids.projectMember, ids.tenantA), storepostgres.MyProjectsCursor{}, 200)
	if err != nil || len(memberPage.Projects) != 1 || memberPage.Projects[0].UID != ids.projectA || memberPage.NextProjectUID != "" {
		t.Fatalf("project member selector page=%#v err=%v", memberPage, err)
	}
	if _, err := selector.ListMyProjects(ctx, ids.tenantB, principalFor(ids.projectMember, ids.tenantA), storepostgres.MyProjectsCursor{}, 200); err == nil {
		t.Fatal("tenant A principal selected projects in tenant B")
	}
	adminPage, err := selector.ListMyProjects(ctx, ids.tenantA, principalFor(ids.tenantAdmin, ids.tenantA), storepostgres.MyProjectsCursor{}, 1)
	if err != nil || len(adminPage.Projects) != 1 || adminPage.Projects[0].UID != ids.projectA || adminPage.NextProjectUID != ids.projectA {
		t.Fatalf("tenant administrator first selector page=%#v err=%v", adminPage, err)
	}
	adminPage, err = selector.ListMyProjects(ctx, ids.tenantA, principalFor(ids.tenantAdmin, ids.tenantA), storepostgres.MyProjectsCursor{
		AfterProjectUID: adminPage.NextProjectUID, SubjectKey: adminPage.SubjectKey,
	}, 1)
	if err != nil || len(adminPage.Projects) != 1 || adminPage.Projects[0].UID != ids.projectB || adminPage.NextProjectUID != "" {
		t.Fatalf("tenant administrator second selector page=%#v err=%v", adminPage, err)
	}
	if _, err := fixture.Exec(ctx, `UPDATE cloud_agents.role_bindings SET state='revoked',updated_at=clock_timestamp()
		WHERE tenant_id=$1 AND subject_value=$2 AND role_name='project.viewer'`, ids.tenantA, "user-"+ids.projectMember); err != nil {
		t.Fatal(err)
	}
	memberPage, err = selector.ListMyProjects(ctx, ids.tenantA, principalFor(ids.projectMember, ids.tenantA), storepostgres.MyProjectsCursor{}, 200)
	if err != nil || len(memberPage.Projects) != 0 {
		t.Fatalf("revoked project member selector page=%#v err=%v", memberPage, err)
	}

	var revoked bool
	if err := identityService.QueryRow(ctx, `SELECT cloud_agents_identity.revoke_session($1,$2,$3,$4)`,
		digests.globalAdmin[:], "admin", newTestID(t), newTestID(t)).Scan(&revoked); err != nil || !revoked {
		t.Fatalf("revoke global administrator session: revoked=%t err=%v", revoked, err)
	}
	assertAuthorizationDenied(t, ctx, authorizer, api.IdentityApplicationAdmin, digests.globalAdmin, ids.tenantA, "")

	if _, err := fixture.Exec(ctx, `UPDATE cloud_agents.role_bindings SET expires_at=clock_timestamp()-interval '1 second'
		WHERE tenant_id=$1 AND subject_value=$2`, ids.tenantA, "user-"+ids.tenantAdmin); err != nil {
		t.Fatal(err)
	}
	assertAuthorizationDenied(t, ctx, authorizer, api.IdentityApplicationAdmin, digests.tenantAdmin, ids.tenantA, "")

	if _, err := fixture.Exec(ctx, `UPDATE cloud_agents_identity.users SET disabled_at=clock_timestamp() WHERE id=$1`, ids.projectMember); err != nil {
		t.Fatal(err)
	}
	assertAuthorizationDenied(t, ctx, authorizer, api.IdentityApplicationUser, digests.projectMember, ids.tenantA, ids.projectA)

	var directRead int
	err = runtime.QueryRow(ctx, `SELECT count(*) FROM cloud_agents_identity.sessions WHERE user_id=$1`, ids.globalUser).Scan(&directRead)
	var databaseError *pgconn.PgError
	if !errors.As(err, &databaseError) || databaseError.Code != "42501" {
		t.Fatalf("runtime direct identity-table read error = %v, want SQLSTATE 42501", err)
	}

	if globalAdmin.Issuer != issuer || globalAdmin.Application != api.IdentityApplicationAdmin || globalAdmin.TenantID != ids.tenantA {
		t.Fatalf("authorization response identity drift = %#v", globalAdmin)
	}
}

func newMyProjectsPrincipalFactory(t *testing.T, issuer string) func(string, string) *authn.VerifiedPrincipal {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Second)
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	const audience = "https://my-projects.identity-test.example/user"
	const keyID = "my-projects-live"
	signer, err := identity.NewTokenSigner(identity.TokenSignerConfig{
		Issuer: issuer, AdminAudience: "https://my-projects.identity-test.example/admin", UserAudience: audience,
		KeyID: keyID, PrivateKey: privateKey,
	})
	if err != nil {
		t.Fatal(err)
	}
	authority := identity.TokenSigningAuthority{
		SecurityEpoch: 1, NotBefore: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour),
		KeyNotBefore: now.Add(-time.Minute), KeyNotAfter: now.Add(time.Hour),
	}
	jwk, err := json.Marshal(map[string]any{
		"alg": "RS256", "e": "AQAB", "key_ops": []string{"verify"}, "kid": keyID,
		"kty": "RSA", "n": base64.RawURLEncoding.EncodeToString(privateKey.PublicKey.N.Bytes()), "use": "sig",
	})
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := authn.NewConfiguredVerifier(authn.ConfiguredVerifierConfig{
		Issuer: issuer, Audience: audience, Generation: 1, SecurityEpoch: 1,
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
	return func(userID, tenantID string) *authn.VerifiedPrincipal {
		request, err := identity.NewTokenSigningRequest(
			identity.TokenClientWeb, api.IdentityApplicationUser, userID, tenantID, "",
			[]string{"projects.list"}, authority, now,
		)
		if err != nil {
			t.Fatal(err)
		}
		token, err := signer.Sign(request)
		if err != nil {
			t.Fatal(err)
		}
		principal, err := verifier.Verify(token.Token, authn.VerificationRequest{
			TenantID: tenantID, ResourceLevel: "tenant", ResourceID: tenantID, RequiredPermission: "projects.list",
		})
		if err != nil {
			t.Fatal(err)
		}
		return principal
	}
}

type authorizationFixtureIDs struct {
	prefix, tenantA, tenantB, projectA, projectB string
	globalUser, tenantAdmin, projectMember       string
}

type authorizationSessionDigests struct {
	globalAdmin, globalUser, tenantAdmin, projectMember [sha256.Size]byte
}

func prepareAuthorizationFixture(
	t *testing.T,
	ctx context.Context,
	bootstrap, fixture *pgxpool.Pool,
	ids authorizationFixtureIDs,
) (string, authorizationSessionDigests) {
	t.Helper()
	var issuer string
	if err := fixture.QueryRow(ctx, `SELECT issuer FROM cloud_agents_identity.realm WHERE singleton`).Scan(&issuer); err != nil {
		t.Fatal("identity realm fixture is absent")
	}
	users := []string{ids.globalUser, ids.tenantAdmin, ids.projectMember}
	for index, userID := range users {
		if _, err := fixture.Exec(ctx, `INSERT INTO cloud_agents_identity.users(id,email,display_name,email_verified_at)
			VALUES ($1,$2,$3,clock_timestamp())`, userID, fmt.Sprintf("%s-%d@example.test", ids.prefix, index), userID); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := fixture.Exec(ctx, `INSERT INTO cloud_agents_identity.platform_admins(user_id) VALUES ($1)`, ids.globalUser); err != nil {
		t.Fatal(err)
	}
	digests := authorizationSessionDigests{
		globalAdmin:   sha256.Sum256([]byte(ids.prefix + " global admin session")),
		globalUser:    sha256.Sum256([]byte(ids.prefix + " global user session")),
		tenantAdmin:   sha256.Sum256([]byte(ids.prefix + " tenant admin session")),
		projectMember: sha256.Sum256([]byte(ids.prefix + " project member session")),
	}
	for _, session := range []struct {
		digest      [sha256.Size]byte
		userID, app string
	}{
		{digests.globalAdmin, ids.globalUser, "admin"},
		{digests.globalUser, ids.globalUser, "user"},
		{digests.tenantAdmin, ids.tenantAdmin, "admin"},
		{digests.projectMember, ids.projectMember, "user"},
	} {
		if _, err := fixture.Exec(ctx, `INSERT INTO cloud_agents_identity.sessions(digest,user_id,application,expires_at)
			VALUES ($1,$2,$3,clock_timestamp()+interval '1 hour')`, session.digest[:], session.userID, session.app); err != nil {
			t.Fatal(err)
		}
	}
	bootstrapAuthorizationTenant(t, ctx, bootstrap, ids.tenantA, issuer, "user-"+ids.tenantAdmin)
	bootstrapAuthorizationTenant(t, ctx, bootstrap, ids.tenantB, issuer, "user-"+ids.prefix+"-unrelated")
	insertProjectMembershipFixture(t, ctx, fixture, ids, issuer)
	return issuer, digests
}

func bootstrapAuthorizationTenant(t *testing.T, ctx context.Context, bootstrap *pgxpool.Pool, tenantID, issuer, subject string) {
	t.Helper()
	if _, err := bootstrap.Exec(ctx, `SELECT * FROM cloud_agents.bootstrap_tenant_administrator_v1(
		$1,$1,$1,$1||'-org',$1||'-org',$1||'-org','user',$2,$3,
		$1||'-member',$1||'-member',$1||'-binding',$1||'-binding',
		$1||'-audit-tenant',$1||'-audit-member',$1||'-audit-binding','identity-authorization-test')`, tenantID, issuer, subject); err != nil {
		t.Fatal(err)
	}
}

func insertProjectMembershipFixture(t *testing.T, ctx context.Context, fixture *pgxpool.Pool, ids authorizationFixtureIDs, issuer string) {
	t.Helper()
	tx, err := fixture.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	var revision int64
	if err := tx.QueryRow(ctx, `SELECT current_revision FROM cloud_agents.tenant_resource_versions
		WHERE tenant_id=$1 AND tenant_uid=$1 FOR UPDATE`, ids.tenantA).Scan(&revision); err != nil {
		t.Fatal(err)
	}
	for _, projectID := range []string{ids.projectA, ids.projectB} {
		revision++
		if _, err := tx.Exec(ctx, `INSERT INTO cloud_agents.resource_changes(
			tenant_id,tenant_uid,resource_version,resource_kind,resource_uid,change_kind,actor_database_principal,occurred_at)
			VALUES ($1,$1,$2,'project',$3,'created',session_user,clock_timestamp())`, ids.tenantA, revision, projectID); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO cloud_agents.projects(
			tenant_id,tenant_ref_id,project_uid,project_name,organization_uid,display_name,state,resource_version,created_at,updated_at)
			VALUES ($1,$1,$2,$2,$3,$2,'active',$4,clock_timestamp(),clock_timestamp())`,
			ids.tenantA, projectID, ids.tenantA+"-org", revision); err != nil {
			t.Fatal(err)
		}
	}
	subject := authz.SubjectRef{Kind: "user", Issuer: issuer, Subject: "user-" + ids.projectMember}
	subjectDigest, err := subject.Digest()
	if err != nil {
		t.Fatal(err)
	}
	membershipID := ids.prefix + "-project-member-record"
	revision++
	if _, err := tx.Exec(ctx, `INSERT INTO cloud_agents.resource_changes(
		tenant_id,tenant_uid,resource_version,resource_kind,resource_uid,change_kind,actor_database_principal,occurred_at)
		VALUES ($1,$1,$2,'membership',$3,'created',session_user,clock_timestamp())`, ids.tenantA, revision, membershipID); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO cloud_agents.memberships(
		tenant_id,tenant_ref_id,membership_uid,membership_name,subject_kind,subject_issuer,subject_value,subject_digest,
		scope_level,scope_project_uid,state,resource_version,created_at,updated_at)
		VALUES ($1,$1,$2,$2,'user',$3,$4,$5,'project',$6,'active',$7,clock_timestamp(),clock_timestamp())`,
		ids.tenantA, membershipID, issuer, subject.Subject, subjectDigest, ids.projectA, revision); err != nil {
		t.Fatal(err)
	}
	bindingID := ids.prefix + "-project-viewer-binding"
	revision++
	if _, err := tx.Exec(ctx, `INSERT INTO cloud_agents.resource_changes(
		tenant_id,tenant_uid,resource_version,resource_kind,resource_uid,change_kind,actor_database_principal,occurred_at)
		VALUES ($1,$1,$2,'role_binding',$3,'created',session_user,clock_timestamp())`, ids.tenantA, revision, bindingID); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO cloud_agents.role_bindings(
		tenant_id,tenant_ref_id,role_binding_uid,role_binding_name,subject_kind,subject_issuer,subject_value,subject_digest,
		role_name,role_version,scope_level,scope_project_uid,state,resource_version,created_at,updated_at)
		VALUES ($1,$1,$2,$2,'user',$3,$4,$5,'project.viewer',1,'project',$6,'active',$7,clock_timestamp(),clock_timestamp())`,
		ids.tenantA, bindingID, issuer, subject.Subject, subjectDigest, ids.projectA, revision); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `UPDATE cloud_agents.tenant_resource_versions
		SET current_revision=$2,updated_at=clock_timestamp() WHERE tenant_id=$1 AND tenant_uid=$1`, ids.tenantA, revision); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
}

func authorizeToken(
	t *testing.T,
	ctx context.Context,
	service *storepostgres.TokenAuthorizationService,
	application api.IdentityApplication,
	digest [sha256.Size]byte,
	tenantID, projectID string,
) api.TenantTokenAuthorization {
	t.Helper()
	authorization, err := service.AuthorizeTenantToken(ctx, api.TenantTokenAuthorizationRequest{
		Application: application, SessionSHA256: "sha256:" + hex.EncodeToString(digest[:]), TenantID: tenantID, ProjectID: projectID,
	})
	if err != nil {
		t.Fatalf("authorize %s/%s/%s: %v", application, tenantID, projectID, err)
	}
	return authorization
}

func assertAuthorizationDenied(
	t *testing.T,
	ctx context.Context,
	service *storepostgres.TokenAuthorizationService,
	application api.IdentityApplication,
	digest [sha256.Size]byte,
	tenantID, projectID string,
) {
	t.Helper()
	_, err := service.AuthorizeTenantToken(ctx, api.TenantTokenAuthorizationRequest{
		Application: application, SessionSHA256: "sha256:" + hex.EncodeToString(digest[:]), TenantID: tenantID, ProjectID: projectID,
	})
	if !errors.Is(err, storepostgres.ErrTokenAuthorizationDenied) {
		t.Fatalf("authorization error = %v, want ErrTokenAuthorizationDenied", err)
	}
}

func authorizationContains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func authorizationTestPool(t *testing.T, ctx context.Context, databaseURL string) *pgxpool.Pool {
	t.Helper()
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	config.MinConns, config.MaxConns = 1, 2
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	return pool
}

func cleanupAuthorizationFixture(t *testing.T, fixtureURL string, ids authorizationFixtureIDs) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool := authorizationTestPool(t, ctx, fixtureURL)
	defer pool.Close()
	var issuedTokensTable *string
	if err := pool.QueryRow(ctx, `SELECT pg_catalog.to_regclass('cloud_agents_identity.issued_tokens')::text`).Scan(&issuedTokensTable); err != nil {
		t.Error(err)
	} else if issuedTokensTable != nil {
		if _, err := pool.Exec(ctx, `DELETE FROM cloud_agents_identity.issued_tokens WHERE user_id LIKE $1`, ids.prefix+"%"); err != nil {
			t.Error(err)
		}
	}
	for _, statement := range []string{
		`DELETE FROM cloud_agents_identity.audit_events WHERE user_id LIKE $1 OR actor_user_id LIKE $1 OR target_user_id LIKE $1`,
		`DELETE FROM cloud_agents_identity.sessions WHERE user_id LIKE $1`,
		`DELETE FROM cloud_agents_identity.platform_admins WHERE user_id LIKE $1`,
		`DELETE FROM cloud_agents_identity.users WHERE id LIKE $1`,
	} {
		if _, err := pool.Exec(ctx, statement, ids.prefix+"%"); err != nil {
			t.Error(err)
		}
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Error(err)
		return
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if _, err := tx.Exec(ctx, `SET CONSTRAINTS ALL DEFERRED`); err != nil {
		t.Error(err)
		return
	}
	for _, table := range []string{
		"audit_facts", "role_bindings", "memberships", "projects", "organizations",
		"tenant_resource_versions", "resource_changes", "platform_tenants",
	} {
		if _, err := tx.Exec(ctx, `DELETE FROM cloud_agents.`+table+` WHERE tenant_id IN ($1,$2)`, ids.tenantA, ids.tenantB); err != nil {
			t.Error(err)
			return
		}
	}
	if err := tx.Commit(ctx); err != nil {
		t.Error(err)
	}
}
