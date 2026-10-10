package identity_test

import (
	"context"
	"errors"
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

type postgresProviderFactory struct{}

func (postgresProviderFactory) Provider(_ context.Context, client identity.ProviderClientSnapshot) (browserauth.LoginProvider, error) {
	return postgresLoginProvider{issuer: client.Issuer, subject: "subject-" + client.ID}, nil
}

type postgresLoginProvider struct{ issuer, subject string }

func (provider postgresLoginProvider) AuthorizationURL(browserauth.AuthorizationRequest) (string, error) {
	return provider.issuer + "/authorize", nil
}

func (provider postgresLoginProvider) Exchange(context.Context, browserauth.AuthorizationCallback) (browserauth.ProviderIdentity, error) {
	return browserauth.ProviderIdentity{Issuer: provider.issuer, Subject: provider.subject}, nil
}

func TestProviderPostgresLoginLinkAndSessionRotation(t *testing.T) {
	serviceURL := os.Getenv("CLOUD_AGENTS_IDENTITY_PROVIDER_TEST_DATABASE_URL")
	bootstrapURL := os.Getenv("CLOUD_AGENTS_IDENTITY_PROVIDER_TEST_BOOTSTRAP_DATABASE_URL")
	fixtureURL := os.Getenv("CLOUD_AGENTS_IDENTITY_PROVIDER_TEST_FIXTURE_DATABASE_URL")
	if serviceURL == "" || bootstrapURL == "" || fixtureURL == "" {
		t.Skip("provider PostgreSQL test requires service, bootstrap, and fixture database URLs")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	service := providerPool(t, ctx, serviceURL)
	defer service.Close()
	bootstrap := providerPool(t, ctx, bootstrapURL)
	defer bootstrap.Close()
	fixture := providerPool(t, ctx, fixtureURL)
	defer fixture.Close()

	const (
		issuer   = "https://identity.provider.test"
		password = "provider rotation correct password"
	)
	passwordHash, err := browserauth.HashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	_, setupDigest, err := browserauth.NewProof()
	if err != nil {
		t.Fatal(err)
	}
	var initialized bool
	if err := bootstrap.QueryRow(ctx, `SELECT cloud_agents_identity.initialize_realm($1,$2,$3,$4,$5,$6,$7,$8)`,
		issuer, setupDigest[:], "provider-admin", "provider-admin@example.com", "Provider Admin", passwordHash,
		newTestID(t), newTestID(t)).Scan(&initialized); err != nil || !initialized {
		t.Fatalf("initialize provider realm: %v", err)
	}
	passwords, err := identity.NewPasswordStore(service, []byte(strings.Repeat("c", 32)))
	if err != nil {
		t.Fatal(err)
	}
	accounts, err := identity.NewProviderAccountStore(service, passwords)
	if err != nil {
		t.Fatal(err)
	}
	providers, err := identity.NewProviderStore(passwords, postgresProviderFactory{}, []byte(strings.Repeat("f", 32)))
	if err != nil {
		t.Fatal(err)
	}
	admin, err := passwords.PasswordLogin(ctx, api.IdentityApplicationAdmin, netip.MustParseAddr("192.0.2.170"), api.PasswordLoginRequest{Email: "provider-admin@example.com", Password: password})
	if err != nil {
		t.Fatal(err)
	}
	adminDigest, _ := browserauth.ProofDigest(admin.SessionHandle)
	for _, provider := range []struct {
		id          string
		application api.IdentityApplication
	}{{"provider-one", api.IdentityApplicationAdmin}, {"provider-two", api.IdentityApplicationAdmin}, {"provider-only", api.IdentityApplicationUser}} {
		configured := identity.ProviderClient{ProviderClientSnapshot: identity.ProviderClientSnapshot{
			ID: provider.id, Application: provider.application, Kind: "oidc", Issuer: "https://" + provider.id + ".example.test",
			ClientID: "client-" + provider.id, RedirectURL: "https://web.example.test/auth/provider/callback",
			SecretRef: "secret-" + provider.id, Scopes: []string{"email", "openid"}, AllowedOrganizationIDs: []string{}, Revision: 1,
		}, DisplayName: provider.id, Enabled: true}
		revision, err := accounts.UpsertProviderClient(ctx, adminDigest, configured, 0)
		if err != nil || revision != 1 {
			t.Fatalf("create provider %s revision=%d err=%v", provider.id, revision, err)
		}
	}
	if _, err := fixture.Exec(ctx, `INSERT INTO cloud_agents_identity.login_methods(id,user_id,provider_id,issuer,subject)
		VALUES ('provider-admin-method','provider-admin','provider-one','https://provider-one.example.test','subject-provider-one')`); err != nil {
		t.Fatal(err)
	}

	loggedIn := completeProviderFlow(t, ctx, providers, identity.ProviderAuthorizationInput{
		Application: api.IdentityApplicationAdmin, ProviderID: "provider-one", Purpose: identity.ProviderPurposeLogin,
	})
	if loggedIn.Login == nil || loggedIn.Login.Session.User.ID != "provider-admin" {
		t.Fatalf("provider login = %#v", loggedIn)
	}
	providerLoginDigest, _ := browserauth.ProofDigest(loggedIn.Login.SessionHandle)
	providerReauth := completeProviderFlow(t, ctx, providers, identity.ProviderAuthorizationInput{
		Application: api.IdentityApplicationAdmin, ProviderID: "provider-one", Purpose: identity.ProviderPurposeReauth,
		SessionDigest: &providerLoginDigest,
	})
	assertProviderSessionRotated(t, ctx, fixture, passwords, api.IdentityApplicationAdmin, loggedIn.Login.SessionHandle, providerReauth.SessionHandle)

	providerReauthDigest, _ := browserauth.ProofDigest(providerReauth.SessionHandle)
	passwordReauth, err := accounts.PasswordReauthenticate(ctx, api.IdentityApplicationAdmin, providerReauthDigest, password)
	if err != nil || passwordReauth.Proof == "" || passwordReauth.SessionHandle == "" {
		t.Fatalf("password reauth = %#v, %v", passwordReauth, err)
	}
	assertProviderSessionRotated(t, ctx, fixture, passwords, api.IdentityApplicationAdmin, providerReauth.SessionHandle, passwordReauth.SessionHandle)

	passwordSessionDigest, _ := browserauth.ProofDigest(passwordReauth.SessionHandle)
	linked := completeProviderFlow(t, ctx, providers, identity.ProviderAuthorizationInput{
		Application: api.IdentityApplicationAdmin, ProviderID: "provider-two", Purpose: identity.ProviderPurposeLink,
		SessionDigest: &passwordSessionDigest, ReauthProof: passwordReauth.Proof,
	})
	if linked.LoginMethod == nil || linked.LoginMethod.ProviderID != "provider-two" || linked.LoginMethod.CreatedAt.IsZero() {
		t.Fatalf("linked method = %#v", linked.LoginMethod)
	}
	methods, err := accounts.ListLoginMethods(ctx, api.IdentityApplicationAdmin, passwordSessionDigest)
	if err != nil || !methods.PasswordEnabled || len(methods.Methods) != 2 {
		t.Fatalf("login methods = %#v, %v", methods, err)
	}
	secondReauth, err := accounts.PasswordReauthenticate(ctx, api.IdentityApplicationAdmin, passwordSessionDigest, password)
	if err != nil {
		t.Fatal(err)
	}
	secondDigest, _ := browserauth.ProofDigest(secondReauth.SessionHandle)
	if err := accounts.UnlinkLoginMethod(ctx, api.IdentityApplicationAdmin, secondDigest, linked.LoginMethod.ID, secondReauth.Proof); err != nil {
		t.Fatal(err)
	}

	providerOnlyHandle, providerOnlyDigest, err := browserauth.NewProof()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.Exec(ctx, `WITH account AS (
		INSERT INTO cloud_agents_identity.users(id,email,display_name,email_verified_at)
		VALUES ('provider-only-user','provider-only@example.com','Provider Only',clock_timestamp()) RETURNING id
	), method AS (
		INSERT INTO cloud_agents_identity.login_methods(id,user_id,provider_id,issuer,subject)
		SELECT 'provider-only-method',id,'provider-only','https://provider-only.example.test','subject-provider-only' FROM account RETURNING user_id
	)
	INSERT INTO cloud_agents_identity.sessions(digest,user_id,application,expires_at)
	SELECT $1,user_id,'user',clock_timestamp()+interval '12 hours' FROM method`, providerOnlyDigest[:]); err != nil {
		t.Fatal(err)
	}
	providerOnlyReauth := completeProviderFlow(t, ctx, providers, identity.ProviderAuthorizationInput{
		Application: api.IdentityApplicationUser, ProviderID: "provider-only", Purpose: identity.ProviderPurposeReauth,
		SessionDigest: &providerOnlyDigest,
	})
	rotatedOnlyDigest, _ := browserauth.ProofDigest(providerOnlyReauth.SessionHandle)
	if err := accounts.EnablePassword(ctx, api.IdentityApplicationUser, rotatedOnlyDigest, providerOnlyReauth.ReauthProof, "provider only new password"); err != nil {
		t.Fatal(err)
	}
	if _, err := passwords.Session(ctx, api.IdentityApplicationUser, rotatedOnlyDigest); !errors.Is(err, browserauth.ErrUnauthorized) {
		t.Fatalf("password enable retained session: %v", err)
	}
	if _, err := passwords.PasswordLogin(ctx, api.IdentityApplicationUser, netip.MustParseAddr("192.0.2.171"), api.PasswordLoginRequest{Email: "provider-only@example.com", Password: "provider only new password"}); err != nil {
		t.Fatalf("enabled password login: %v", err)
	}
	_ = providerOnlyHandle

	var directAccess bool
	if err := fixture.QueryRow(ctx, `SELECT has_table_privilege('identity_service_test','cloud_agents_identity.provider_flows','SELECT')`).Scan(&directAccess); err != nil || directAccess {
		t.Fatal("identity service received direct provider flow table access")
	}
}

func completeProviderFlow(t *testing.T, ctx context.Context, store *identity.ProviderStore, input identity.ProviderAuthorizationInput) identity.ProviderCallbackResult {
	t.Helper()
	authorization, err := store.StartAuthorization(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	result, err := store.CompleteAuthorization(ctx, identity.ProviderCallbackInput{
		Application: input.Application, State: authorization.State, Code: "provider-code",
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func assertProviderSessionRotated(t *testing.T, ctx context.Context, fixture *pgxpool.Pool, passwords *identity.PasswordStore, application api.IdentityApplication, oldHandle, newHandle string) {
	t.Helper()
	oldDigest, _ := browserauth.ProofDigest(oldHandle)
	newDigest, _ := browserauth.ProofDigest(newHandle)
	if oldDigest == newDigest {
		t.Fatal("reauthentication reused the session handle")
	}
	if _, err := passwords.Session(ctx, application, oldDigest); !errors.Is(err, browserauth.ErrUnauthorized) {
		t.Fatalf("old session remained active: %v", err)
	}
	if _, err := passwords.Session(ctx, application, newDigest); err != nil {
		t.Fatalf("rotated session is inactive: %v", err)
	}
	var oldCreated, oldExpires, newCreated, newExpires time.Time
	if err := fixture.QueryRow(ctx, `SELECT old_session.created_at,old_session.expires_at,new_session.created_at,new_session.expires_at
		FROM cloud_agents_identity.sessions AS old_session
		JOIN cloud_agents_identity.sessions AS new_session ON true
		WHERE old_session.digest=$1 AND new_session.digest=$2`, oldDigest[:], newDigest[:]).Scan(&oldCreated, &oldExpires, &newCreated, &newExpires); err != nil {
		t.Fatal(err)
	}
	if !oldCreated.Equal(newCreated) || !oldExpires.Equal(newExpires) {
		t.Fatalf("rotation changed absolute lifetime: old=%s..%s new=%s..%s", oldCreated, oldExpires, newCreated, newExpires)
	}
}

func providerPool(t *testing.T, ctx context.Context, url string) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	return pool
}
