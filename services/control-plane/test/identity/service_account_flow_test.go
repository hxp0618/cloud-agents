package identity_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"errors"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	api "github.com/hxp0618/cloud-agents/sdk/go/gen/openapi/v1alpha1"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/browserauth"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/identity"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/server"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/store/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestServiceAccountPostgresHTTPAutomationFlow(t *testing.T) {
	runtimeURL := os.Getenv("CLOUD_AGENTS_SERVICE_ACCOUNT_TEST_DATABASE_URL")
	serviceURL := os.Getenv("CLOUD_AGENTS_SERVICE_ACCOUNT_TEST_IDENTITY_DATABASE_URL")
	bootstrapURL := os.Getenv("CLOUD_AGENTS_SERVICE_ACCOUNT_TEST_BOOTSTRAP_DATABASE_URL")
	if runtimeURL == "" || serviceURL == "" || bootstrapURL == "" {
		t.Skip("service-account PostgreSQL test requires fresh runtime, identity, and bootstrap URLs")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	runtimePool := tokenTestPool(t, ctx, runtimeURL)
	defer runtimePool.Close()
	servicePool := tokenTestPool(t, ctx, serviceURL)
	defer servicePool.Close()
	bootstrapPool := tokenTestPool(t, ctx, bootstrapURL)
	defer bootstrapPool.Close()

	const (
		userID           = "sa-flow-admin"
		tenantID         = "sa-flow-tenant"
		serviceAccountID = "sa-flow-automation"
	)
	prepareServiceAccountFlowFixture(t, ctx, bootstrapPool, userID, tenantID)

	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	authority := identity.TokenSigningAuthority{
		SecurityEpoch: 1, NotBefore: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour),
		KeyNotBefore: now.Add(-time.Hour), KeyNotAfter: now.Add(time.Hour),
	}
	signer := tokenTestSigner(t, privateKey)
	request, err := identity.NewTokenSigningRequest(
		identity.TokenClientWeb, api.IdentityApplicationAdmin, userID, tenantID, "",
		[]string{"memberships.create", "memberships.delete", "memberships.list", "memberships.update", "role-bindings.bind"}, authority, now,
	)
	if err != nil {
		t.Fatal(err)
	}
	adminToken, err := signer.Sign(request)
	if err != nil {
		t.Fatal(err)
	}
	verifier := tokenTestVerifier(t, privateKey, tokenTestAdminAudience, authority, now)
	manager, err := postgres.NewServiceAccountStore(runtimePool)
	if err != nil {
		t.Fatal(err)
	}
	managementHandler, err := server.NewServiceAccountHTTPServer(verifier, manager)
	if err != nil {
		t.Fatal(err)
	}
	managementServer := httptest.NewTLSServer(managementHandler)
	defer managementServer.Close()
	managementClient, err := api.NewHTTPClientWithClient(managementServer.URL, adminToken.Token, managementServer.Client())
	if err != nil {
		t.Fatal(err)
	}

	created, err := managementClient.CreateAdminServiceAccount(ctx, tenantID, "request-sa-create", api.ServiceAccountCreateRequest{
		ServiceAccountID: serviceAccountID, DisplayName: "Service Account Flow", Application: api.IdentityApplicationAdmin,
		RoleName: "tenant.admin", ScopeLevel: "tenant", ScopeID: tenantID,
	})
	if err != nil || created.Credential == "" || created.ServiceAccount.State != "active" || created.ServiceAccount.ResourceVersion != "1" {
		t.Fatalf("created=%#v error=%v", created, err)
	}
	page, err := managementClient.ListAdminServiceAccounts(ctx, tenantID, "request-sa-list", 20, "")
	if err != nil || len(page.ServiceAccounts) != 1 || page.ServiceAccounts[0].ID != serviceAccountID {
		t.Fatalf("page=%#v error=%v", page, err)
	}
	if err := runtimePool.QueryRow(ctx, `SELECT count(*) FROM cloud_agents_identity.service_accounts`).Scan(new(int)); err == nil {
		t.Fatal("runtime role can directly read identity service-account table")
	}

	authorizationService, err := postgres.NewTokenAuthorizationService(runtimePool)
	if err != nil {
		t.Fatal(err)
	}
	controlPlaneCredential, _, err := browserauth.NewProof()
	if err != nil {
		t.Fatal(err)
	}
	authorizationHandler, err := server.NewIdentityPrincipalAuthorizationHTTPServer(authorizationService, controlPlaneCredential)
	if err != nil {
		t.Fatal(err)
	}
	authorizationServer := httptest.NewTLSServer(authorizationHandler)
	defer authorizationServer.Close()
	authorizer, err := api.NewIdentityAuthorizationHTTPClientWithClient(authorizationServer.URL, controlPlaneCredential, authorizationServer.Client())
	if err != nil {
		t.Fatal(err)
	}
	automation, err := identity.NewAutomationStore(servicePool, signer, authorizer, func(context.Context) (identity.TokenSigningAuthority, error) {
		return authority, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	statusStore, err := identity.NewTokenStore(servicePool, signer, &tokenAuthorizer{}, func(context.Context) (identity.TokenSigningAuthority, error) {
		return authority, nil
	})
	if err != nil {
		t.Fatal(err)
	}

	first := issueAutomationToken(t, ctx, automation, created.Credential, tenantID)
	assertAutomationTokenStatus(t, ctx, statusStore, first.AccessToken, tenantID, "active")
	_, claims := tokenTestDecode(t, first.AccessToken)
	if claims["client_id"] != identity.AutomationClientID || claims["sub"] != "service-"+serviceAccountID ||
		claims["https://schemas.cloud-agents.dev/claims/subject-kind"] != "serviceAccount" {
		t.Fatalf("automation claims=%#v", claims)
	}

	rotated, err := managementClient.RotateAdminServiceAccountCredential(ctx, tenantID, serviceAccountID, "request-sa-rotate", api.ServiceAccountRotateRequest{ExpectedResourceVersion: "1"})
	if err != nil || rotated.Credential == "" || rotated.Credential == created.Credential || rotated.ResourceVersion != "2" {
		t.Fatalf("rotated=%#v error=%v", rotated, err)
	}
	assertAutomationCredentialDenied(t, ctx, automation, created.Credential, tenantID)
	assertAutomationTokenStatus(t, ctx, statusStore, first.AccessToken, tenantID, "inactive")

	second := issueAutomationToken(t, ctx, automation, rotated.Credential, tenantID)
	assertAutomationTokenStatus(t, ctx, statusStore, second.AccessToken, tenantID, "active")
	disabledVersion, err := managementClient.DisableAdminServiceAccount(ctx, tenantID, serviceAccountID, "request-sa-disable", api.ServiceAccountDisableRequest{ExpectedResourceVersion: rotated.ResourceVersion})
	if err != nil || disabledVersion != "3" {
		t.Fatalf("disabled version=%q error=%v", disabledVersion, err)
	}
	assertAutomationCredentialDenied(t, ctx, automation, rotated.Credential, tenantID)
	assertAutomationTokenStatus(t, ctx, statusStore, second.AccessToken, tenantID, "inactive")
}

func prepareServiceAccountFlowFixture(t *testing.T, ctx context.Context, bootstrap *pgxpool.Pool, userID, tenantID string) {
	t.Helper()
	passwordHash, err := browserauth.HashPassword("service account fixture password")
	if err != nil {
		t.Fatal(err)
	}
	_, setupDigest, err := browserauth.NewProof()
	if err != nil {
		t.Fatal(err)
	}
	var initialized bool
	if err := bootstrap.QueryRow(ctx, `SELECT cloud_agents_identity.initialize_realm($1,$2,$3,$4,$5,$6,$7,$8)`,
		tokenTestIssuer, setupDigest[:], userID, "sa-flow-admin@example.com", "SA Flow Admin", passwordHash,
		newTestID(t), "sa-flow-init-request",
	).Scan(&initialized); err != nil || !initialized {
		t.Fatalf("initialize realm: %v, initialized=%v", err, initialized)
	}
	if _, err := bootstrap.Exec(ctx, `SELECT * FROM cloud_agents.bootstrap_tenant_administrator_v1(
		$1,$1,$1,$1||'-org',$1||'-org',$1||'-org','user',$2,$3,
		$1||'-member',$1||'-member',$1||'-binding',$1||'-binding',
		$1||'-audit-tenant',$1||'-audit-member',$1||'-audit-binding','service-account-flow')`,
		tenantID, tokenTestIssuer, "user-"+userID,
	); err != nil {
		t.Fatal(err)
	}
}

func issueAutomationToken(t *testing.T, ctx context.Context, store *identity.AutomationStore, credential, tenantID string) api.TenantToken {
	t.Helper()
	digest, err := browserauth.ProofDigest(credential)
	if err != nil {
		t.Fatal(err)
	}
	result, err := store.IssueAutomationTenantToken(ctx, api.IdentityApplicationAdmin, digest, api.TenantTokenIssueRequest{TenantID: tenantID})
	if err != nil || result.AccessToken == "" {
		t.Fatalf("issue automation token: %#v, %v", result, err)
	}
	return result
}

func assertAutomationCredentialDenied(t *testing.T, ctx context.Context, store *identity.AutomationStore, credential, tenantID string) {
	t.Helper()
	digest, err := browserauth.ProofDigest(credential)
	if err != nil {
		t.Fatal(err)
	}
	result, err := store.IssueAutomationTenantToken(ctx, api.IdentityApplicationAdmin, digest, api.TenantTokenIssueRequest{TenantID: tenantID})
	if result != (api.TenantToken{}) || !errors.Is(err, identity.ErrForbidden) {
		t.Fatalf("revoked credential result=%#v error=%v", result, err)
	}
}

func assertAutomationTokenStatus(t *testing.T, ctx context.Context, store *identity.TokenStore, token, tenantID, want string) {
	t.Helper()
	digest := sha256.Sum256([]byte(token))
	result, err := store.TokenStatus(ctx, api.TokenStatusRequest{
		TokenSHA256: "sha256:" + hexDigest(digest[:]), ExpectedClientID: identity.AutomationClientID,
		ExpectedApplication: api.IdentityApplicationAdmin, ExpectedTenantID: tenantID,
	})
	if err != nil || result.Status != want {
		t.Fatalf("token status=%#v error=%v want=%s", result, err, want)
	}
}
