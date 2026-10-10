package identity_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
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
)

func TestCLIStorePostgresFlow(t *testing.T) {
	databaseURL := os.Getenv("CLOUD_AGENTS_IDENTITY_CLI_TEST_DATABASE_URL")
	bootstrapURL := os.Getenv("CLOUD_AGENTS_IDENTITY_CLI_TEST_BOOTSTRAP_DATABASE_URL")
	fixtureURL := os.Getenv("CLOUD_AGENTS_IDENTITY_CLI_TEST_FIXTURE_DATABASE_URL")
	if databaseURL == "" || bootstrapURL == "" || fixtureURL == "" {
		t.Skip("identity CLI PostgreSQL test requires a fresh database with service, bootstrap and fixture URLs")
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
		issuer   = "https://identity.cli.test.local"
		userID   = "cli-user"
		tenantID = "cli-tenant-a"
		password = "CLI correct horse battery staple"
	)
	prepareTokenStoreFixture(t, ctx, bootstrapPool, fixturePool, issuer, userID, tenantID, password)
	if _, err := bootstrapPool.Exec(ctx, `SELECT * FROM cloud_agents.bootstrap_tenant_administrator_v1(
		$1,$1,$1,$1||'-org',$1||'-org',$1||'-org','user',$2,$3,
		$1||'-member',$1||'-member',$1||'-binding',$1||'-binding',
		$1||'-audit-tenant',$1||'-audit-member',$1||'-audit-binding','identity-cli-test')`, "cli-tenant-b", issuer, "user-"+userID); err != nil {
		t.Fatal(err)
	}

	passwords, err := identity.NewPasswordStore(servicePool, []byte(strings.Repeat("c", 32)))
	if err != nil {
		t.Fatal(err)
	}
	login, err := passwords.PasswordLogin(ctx, api.IdentityApplicationAdmin, netip.MustParseAddr("192.0.2.71"), api.PasswordLoginRequest{Email: "token-user@example.com", Password: password})
	if err != nil {
		t.Fatal(err)
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
		Issuer: issuer, AdminAudience: "https://admin-api.cli.test.local", UserAudience: "https://user-api.cli.test.local",
		KeyID: "cli-key-1", PrivateKey: privateKey,
	})
	if err != nil {
		t.Fatal(err)
	}
	authorizer := &cliPrincipalAuthorizer{userID: userID, issuer: issuer, scopes: []string{"agents.get"}}
	store, err := identity.NewCLIStore(servicePool, passwords, signer, authorizer, func(context.Context) (identity.TokenSigningAuthority, error) {
		now := time.Now().UTC()
		return identity.TokenSigningAuthority{SecurityEpoch: 1, NotBefore: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), KeyNotBefore: now.Add(-time.Hour), KeyNotAfter: now.Add(time.Hour)}, nil
	})
	if err != nil {
		t.Fatal(err)
	}

	state, _, err := browserauth.NewProof()
	if err != nil {
		t.Fatal(err)
	}
	verifier, _, err := browserauth.NewProof()
	if err != nil {
		t.Fatal(err)
	}
	challengeDigest := sha256.Sum256([]byte(verifier))
	startRequest := api.CLIAuthorizationStartRequest{
		Application: api.IdentityApplicationAdmin, CallbackPort: 49171, State: state,
		CodeChallenge: base64.RawURLEncoding.EncodeToString(challengeDigest[:]),
	}
	authorization, err := store.StartCLIAuthorization(ctx, api.IdentityApplicationAdmin, netip.MustParseAddr("192.0.2.72"), startRequest)
	if err != nil || authorization.AuthorizationID == "" {
		t.Fatalf("start = %#v, %v", authorization, err)
	}
	if _, err := store.ApproveCLIAuthorization(ctx, api.IdentityApplicationUser, sessionDigest, authorization.AuthorizationID, api.CLIAuthorizationApproveRequest{State: state}); !errors.Is(err, browserauth.ErrUnauthorized) {
		t.Fatalf("cross-application approval error = %v", err)
	}
	approved, err := store.ApproveCLIAuthorization(ctx, api.IdentityApplicationAdmin, sessionDigest, authorization.AuthorizationID, api.CLIAuthorizationApproveRequest{State: state})
	if err != nil || approved.CallbackPort != startRequest.CallbackPort || approved.State != state || approved.AuthorizationCode == "" {
		t.Fatalf("approval = %#v, %v", approved, err)
	}
	exchangeRequest := api.CLIGrantExchangeRequest{AuthorizationID: authorization.AuthorizationID, AuthorizationCode: approved.AuthorizationCode, CodeVerifier: verifier}
	grant, err := store.ExchangeCLIGrant(ctx, api.IdentityApplicationAdmin, netip.MustParseAddr("192.0.2.72"), exchangeRequest)
	if err != nil || grant.Credential == "" || grant.Application != api.IdentityApplicationAdmin {
		t.Fatalf("exchange = %#v, %v", grant, err)
	}
	if replay, err := store.ExchangeCLIGrant(ctx, api.IdentityApplicationAdmin, netip.MustParseAddr("192.0.2.72"), exchangeRequest); replay.Credential != "" || !errors.Is(err, browserauth.ErrUnauthorized) {
		t.Fatalf("replay = %#v, %v", replay, err)
	}
	grantDigest, err := browserauth.ProofDigest(grant.Credential)
	if err != nil {
		t.Fatal(err)
	}
	firstPage, err := store.ListCLITenants(ctx, api.IdentityApplicationAdmin, grantDigest, 1, "")
	if err != nil || len(firstPage.Tenants) != 1 || firstPage.NextPageToken == "" {
		t.Fatalf("first page = %#v, %v", firstPage, err)
	}
	if _, err := store.ListCLITenants(ctx, api.IdentityApplicationUser, grantDigest, 1, firstPage.NextPageToken); !errors.Is(err, identity.ErrForbidden) {
		t.Fatalf("cross-application cursor error = %v", err)
	}
	secondPage, err := store.ListCLITenants(ctx, api.IdentityApplicationAdmin, grantDigest, 1, firstPage.NextPageToken)
	if err != nil || len(secondPage.Tenants) != 1 || secondPage.Tenants[0].ID == firstPage.Tenants[0].ID {
		t.Fatalf("second page = %#v, %v", secondPage, err)
	}

	token, err := store.IssueCLITenantToken(ctx, api.IdentityApplicationAdmin, grantDigest, api.TenantTokenIssueRequest{TenantID: tenantID})
	if err != nil || token.AccessToken == "" || authorizer.last.ClientID != identity.CLIClientID || strings.Contains(token.AccessToken, grant.Credential) {
		t.Fatalf("token = %#v, authorization = %#v, error = %v", token, authorizer.last, err)
	}
	tokenDigest := sha256.Sum256([]byte(token.AccessToken))
	tokenStore, err := identity.NewTokenStore(servicePool, signer, &tokenAuthorizer{}, func(context.Context) (identity.TokenSigningAuthority, error) {
		return identity.TokenSigningAuthority{}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	statusRequest := api.TokenStatusRequest{TokenSHA256: "sha256:" + hexDigest(tokenDigest[:]), ExpectedClientID: identity.CLIClientID, ExpectedApplication: api.IdentityApplicationAdmin, ExpectedTenantID: tenantID}
	assertTokenStatus(t, ctx, tokenStore, statusRequest, "active")
	if err := store.RevokeCLIGrant(ctx, api.IdentityApplicationUser, grantDigest); !errors.Is(err, browserauth.ErrUnauthorized) {
		t.Fatalf("cross-application revoke error = %v", err)
	}
	if err := store.RevokeCLIGrant(ctx, api.IdentityApplicationAdmin, grantDigest); err != nil {
		t.Fatal(err)
	}
	assertTokenStatus(t, ctx, tokenStore, statusRequest, "inactive")
	if _, err := store.ListCLITenants(ctx, api.IdentityApplicationAdmin, grantDigest, 1, ""); !errors.Is(err, browserauth.ErrUnauthorized) {
		t.Fatalf("revoked grant list error = %v", err)
	}

	issueFreshToken := func(port int, clientIP string) api.TokenStatusRequest {
		t.Helper()
		freshState, _, err := browserauth.NewProof()
		if err != nil {
			t.Fatal(err)
		}
		freshVerifier, _, err := browserauth.NewProof()
		if err != nil {
			t.Fatal(err)
		}
		freshChallenge := sha256.Sum256([]byte(freshVerifier))
		freshAuthorization, err := store.StartCLIAuthorization(ctx, api.IdentityApplicationAdmin, netip.MustParseAddr(clientIP), api.CLIAuthorizationStartRequest{
			Application: api.IdentityApplicationAdmin, CallbackPort: port, State: freshState,
			CodeChallenge: base64.RawURLEncoding.EncodeToString(freshChallenge[:]),
		})
		if err != nil {
			t.Fatal(err)
		}
		freshApproval, err := store.ApproveCLIAuthorization(ctx, api.IdentityApplicationAdmin, sessionDigest, freshAuthorization.AuthorizationID, api.CLIAuthorizationApproveRequest{State: freshState})
		if err != nil {
			t.Fatal(err)
		}
		freshGrant, err := store.ExchangeCLIGrant(ctx, api.IdentityApplicationAdmin, netip.MustParseAddr(clientIP), api.CLIGrantExchangeRequest{
			AuthorizationID: freshAuthorization.AuthorizationID, AuthorizationCode: freshApproval.AuthorizationCode, CodeVerifier: freshVerifier,
		})
		if err != nil {
			t.Fatal(err)
		}
		freshGrantDigest, err := browserauth.ProofDigest(freshGrant.Credential)
		if err != nil {
			t.Fatal(err)
		}
		freshToken, err := store.IssueCLITenantToken(ctx, api.IdentityApplicationAdmin, freshGrantDigest, api.TenantTokenIssueRequest{TenantID: tenantID})
		if err != nil {
			t.Fatal(err)
		}
		freshTokenDigest := sha256.Sum256([]byte(freshToken.AccessToken))
		freshStatus := api.TokenStatusRequest{TokenSHA256: "sha256:" + hexDigest(freshTokenDigest[:]), ExpectedClientID: identity.CLIClientID, ExpectedApplication: api.IdentityApplicationAdmin, ExpectedTenantID: tenantID}
		assertTokenStatus(t, ctx, tokenStore, freshStatus, "active")
		return freshStatus
	}

	passwordTokenStatus := issueFreshToken(49173, "192.0.2.73")
	replacementHash, err := browserauth.HashPassword("CLI replacement horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixturePool.Exec(ctx, `UPDATE cloud_agents_identity.password_credentials SET password_hash=$1 WHERE user_id=$2`, replacementHash, userID); err != nil {
		t.Fatal(err)
	}
	assertTokenStatus(t, ctx, tokenStore, passwordTokenStatus, "inactive")

	disabledTokenStatus := issueFreshToken(49174, "192.0.2.74")
	if _, err := fixturePool.Exec(ctx, `UPDATE cloud_agents_identity.users SET disabled_at=clock_timestamp() WHERE id=$1`, userID); err != nil {
		t.Fatal(err)
	}
	assertTokenStatus(t, ctx, tokenStore, disabledTokenStatus, "inactive")

	var rawColumns int
	if err := fixturePool.QueryRow(ctx, `SELECT count(*) FROM information_schema.columns
		WHERE table_schema='cloud_agents_identity' AND table_name IN ('cli_authorizations','cli_grants','cli_issued_tokens')
		AND column_name IN ('state','authorization_code','credential','token','access_token','raw_token')`).Scan(&rawColumns); err != nil || rawColumns != 0 {
		t.Fatal("CLI tables contain a raw proof or token column")
	}
	var leakedAudit int
	if err := fixturePool.QueryRow(ctx, `SELECT count(*) FROM cloud_agents_identity.audit_events
		WHERE event_kind LIKE 'cli_%' AND (correlation_id=$1 OR reason_code=$1)`, grant.Credential).Scan(&leakedAudit); err != nil || leakedAudit != 0 {
		t.Fatal("CLI grant leaked into audit data")
	}
}

func TestCLIStorePersistsAnonymousRateLimitAfterCancellation(t *testing.T) {
	databaseURL := os.Getenv("CLOUD_AGENTS_IDENTITY_CLI_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("identity CLI PostgreSQL test requires a fresh service database URL")
	}
	ctx := context.Background()
	pool := tokenTestPool(t, ctx, databaseURL)
	defer pool.Close()
	passwords, err := identity.NewPasswordStore(pool, []byte(strings.Repeat("r", 32)))
	if err != nil {
		t.Fatal(err)
	}
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := identity.NewTokenSigner(identity.TokenSignerConfig{Issuer: "https://rate.identity.test", AdminAudience: "https://rate.admin.test", UserAudience: "https://rate.user.test", KeyID: "rate-key", PrivateKey: key})
	if err != nil {
		t.Fatal(err)
	}
	store, err := identity.NewCLIStore(pool, passwords, signer, &cliPrincipalAuthorizer{}, func(context.Context) (identity.TokenSigningAuthority, error) {
		return identity.TokenSigningAuthority{}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	state, _, _ := browserauth.NewProof()
	verifier, _, _ := browserauth.NewProof()
	challenge := sha256.Sum256([]byte(verifier))
	request := api.CLIAuthorizationStartRequest{Application: api.IdentityApplicationAdmin, CallbackPort: 49172, State: state, CodeChallenge: base64.RawURLEncoding.EncodeToString(challenge[:])}
	clientIP := netip.MustParseAddr("192.0.2.199")
	for attempt := 0; attempt < 100; attempt++ {
		canceled, cancel := context.WithCancel(ctx)
		cancel()
		if _, err := store.StartCLIAuthorization(canceled, api.IdentityApplicationAdmin, clientIP, request); !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled attempt %d error = %v", attempt+1, err)
		}
	}
	if _, err := store.StartCLIAuthorization(ctx, api.IdentityApplicationAdmin, clientIP, request); !errors.Is(err, identity.ErrRateLimited) {
		t.Fatalf("rate limit error = %v", err)
	}
}

type cliPrincipalAuthorizer struct {
	userID string
	issuer string
	scopes []string
	last   api.PrincipalTokenAuthorizationRequest
}

func (authorizer *cliPrincipalAuthorizer) AuthorizePrincipalToken(_ context.Context, _ string, request api.PrincipalTokenAuthorizationRequest) (api.PrincipalTokenAuthorization, error) {
	authorizer.last = request
	return api.PrincipalTokenAuthorization{
		PrincipalID: authorizer.userID,
		Subject:     common.SubjectRef{Kind: "user", Issuer: authorizer.issuer, Subject: "user-" + authorizer.userID},
		Issuer:      authorizer.issuer, TenantID: request.TenantID, ProjectID: request.ProjectID,
		Application: request.Application, Scopes: append([]string(nil), authorizer.scopes...),
	}, nil
}
