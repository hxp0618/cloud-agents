package identity

import (
	"context"
	"crypto/sha256"
	"net/url"
	"slices"
	"time"

	api "github.com/hxp0618/cloud-agents/sdk/go/gen/openapi/v1alpha1"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/browserauth"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PublicProvider struct {
	ID, Kind, DisplayName string
}

type ProviderClient struct {
	ProviderClientSnapshot
	DisplayName string
	Enabled     bool
}

type LoginMethod struct {
	ProviderLoginMethod
}

type LoginMethodPage struct {
	PasswordEnabled bool
	Methods         []LoginMethod
}

type Reauthentication struct {
	Proof         string
	SessionHandle string
	ExpiresAt     time.Time
}

// ProviderAccountStore owns provider configuration and authenticated account
// operations. Its SQL functions retain all durable authorization decisions.
type ProviderAccountStore struct {
	pool      *pgxpool.Pool
	passwords *PasswordStore
}

func NewProviderAccountStore(pool *pgxpool.Pool, passwords *PasswordStore) (*ProviderAccountStore, error) {
	if pool == nil || passwords == nil || passwords.pool != pool {
		return nil, errInvalidConfiguration
	}
	return &ProviderAccountStore{pool: pool, passwords: passwords}, nil
}

func (store *ProviderAccountStore) ListPublicProviders(ctx context.Context, application api.IdentityApplication) ([]PublicProvider, error) {
	if ctx == nil || !validApplication(application) {
		return nil, ErrProviderInput
	}
	databaseCtx, cancel := context.WithTimeout(ctx, databaseOperationTimeout)
	defer cancel()
	rows, err := store.pool.Query(databaseCtx, `SELECT provider_id,provider_kind,display_name
		FROM cloud_agents_identity.list_public_provider_clients($1)`, string(application))
	if err != nil {
		return nil, postgresProviderError(ctx, err)
	}
	defer rows.Close()
	providers := make([]PublicProvider, 0)
	for rows.Next() {
		var provider PublicProvider
		if err := rows.Scan(&provider.ID, &provider.Kind, &provider.DisplayName); err != nil {
			return nil, ErrUnavailable
		}
		providers = append(providers, provider)
	}
	if err := rows.Err(); err != nil {
		return nil, postgresProviderError(ctx, err)
	}
	return providers, nil
}

func (store *ProviderAccountStore) ListProviderClients(ctx context.Context, sessionDigest [sha256.Size]byte) ([]ProviderClient, error) {
	if ctx == nil {
		return nil, ErrProviderInput
	}
	databaseCtx, cancel := context.WithTimeout(ctx, databaseOperationTimeout)
	defer cancel()
	rows, err := store.pool.Query(databaseCtx, `SELECT provider_id,application,display_name,provider_kind,
		issuer,client_id,redirect_uri,secret_ref,root_ca_ref,agent_id,scopes,
		trust_provider_email,allowed_organization_ids,enabled,resource_version
		FROM cloud_agents_identity.list_provider_clients($1)`, sessionDigest[:])
	if err != nil {
		return nil, postgresProviderError(ctx, err)
	}
	defer rows.Close()
	clients := make([]ProviderClient, 0)
	for rows.Next() {
		var client ProviderClient
		var rootCA, agentID pgtype.Text
		if err := rows.Scan(&client.ID, &client.Application, &client.DisplayName, &client.Kind,
			&client.Issuer, &client.ClientID, &client.RedirectURL, &client.SecretRef,
			&rootCA, &agentID, &client.Scopes, &client.TrustProviderEmail,
			&client.AllowedOrganizationIDs, &client.Enabled, &client.Revision); err != nil {
			return nil, ErrUnavailable
		}
		if rootCA.Valid {
			client.RootCARef = rootCA.String
		}
		if agentID.Valid {
			client.AgentID = agentID.String
		}
		clients = append(clients, client)
	}
	if err := rows.Err(); err != nil {
		return nil, postgresProviderError(ctx, err)
	}
	return clients, nil
}

func (store *ProviderAccountStore) UpsertProviderClient(ctx context.Context, sessionDigest [sha256.Size]byte, client ProviderClient, expectedRevision int64) (int64, error) {
	if ctx == nil || expectedRevision < 0 || !validProviderClient(client) {
		return 0, ErrProviderInput
	}
	eventID, correlationID, err := securityAuditIDs(ctx)
	if err != nil {
		return 0, err
	}
	databaseCtx, cancel := context.WithTimeout(ctx, databaseOperationTimeout)
	defer cancel()
	var rootCA, agentID any
	if client.RootCARef != "" {
		rootCA = client.RootCARef
	}
	if client.AgentID != "" {
		agentID = client.AgentID
	}
	var revision int64
	err = store.pool.QueryRow(databaseCtx, `SELECT cloud_agents_identity.upsert_provider_client(
		$1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18)`,
		sessionDigest[:], client.ID, string(client.Application), client.DisplayName,
		client.Kind, client.Issuer, client.ClientID, client.RedirectURL,
		client.SecretRef, rootCA, agentID, client.Scopes, client.TrustProviderEmail,
		client.AllowedOrganizationIDs, client.Enabled, expectedRevision, eventID, correlationID,
	).Scan(&revision)
	if err != nil {
		return 0, postgresProviderError(ctx, err)
	}
	return revision, nil
}

func (store *ProviderAccountStore) ListLoginMethods(ctx context.Context, application api.IdentityApplication, sessionDigest [sha256.Size]byte) (LoginMethodPage, error) {
	if ctx == nil || !validApplication(application) {
		return LoginMethodPage{}, ErrProviderInput
	}
	databaseCtx, cancel := context.WithTimeout(ctx, databaseOperationTimeout)
	defer cancel()
	rows, err := store.pool.Query(databaseCtx, `SELECT password_enabled,login_method_id,provider_id,issuer,subject,created_at
		FROM cloud_agents_identity.list_login_methods($1,$2)`, sessionDigest[:], string(application))
	if err != nil {
		return LoginMethodPage{}, postgresProviderError(ctx, err)
	}
	defer rows.Close()
	page := LoginMethodPage{Methods: []LoginMethod{}}
	seenPassword := false
	for rows.Next() {
		var id, providerID, issuer, subject pgtype.Text
		var createdAt pgtype.Timestamptz
		var passwordEnabled bool
		if err := rows.Scan(&passwordEnabled, &id, &providerID, &issuer, &subject, &createdAt); err != nil {
			return LoginMethodPage{}, ErrUnavailable
		}
		if seenPassword && page.PasswordEnabled != passwordEnabled {
			return LoginMethodPage{}, ErrUnavailable
		}
		page.PasswordEnabled, seenPassword = passwordEnabled, true
		if id.Valid || providerID.Valid || issuer.Valid || subject.Valid || createdAt.Valid {
			if !(id.Valid && providerID.Valid && issuer.Valid && subject.Valid && createdAt.Valid) {
				return LoginMethodPage{}, ErrUnavailable
			}
			page.Methods = append(page.Methods, LoginMethod{ProviderLoginMethod: ProviderLoginMethod{
				ID: id.String, ProviderID: providerID.String, Issuer: issuer.String, Subject: subject.String, CreatedAt: createdAt.Time.UTC(),
			}})
		}
	}
	if err := rows.Err(); err != nil {
		return LoginMethodPage{}, postgresProviderError(ctx, err)
	}
	if !seenPassword {
		return LoginMethodPage{}, ErrUnavailable
	}
	return page, nil
}

func (store *ProviderAccountStore) PasswordReauthenticate(ctx context.Context, application api.IdentityApplication, sessionDigest [sha256.Size]byte, password string) (Reauthentication, error) {
	if ctx == nil || !validApplication(application) {
		return Reauthentication{}, ErrProviderInput
	}
	if err := store.passwords.acquireArgon2(ctx); err != nil {
		return Reauthentication{}, err
	}
	defer store.passwords.releaseArgon2()
	databaseCtx, cancel := context.WithTimeout(ctx, databaseOperationTimeout)
	defer cancel()
	tx, err := store.pool.BeginTx(databaseCtx, pgx.TxOptions{})
	if err != nil {
		return Reauthentication{}, ErrUnavailable
	}
	defer rollbackTransaction(tx)
	var userID string
	var passwordHash pgtype.Text
	var locked bool
	if err := tx.QueryRow(databaseCtx, `SELECT user_id,password_hash,locked
		FROM cloud_agents_identity.prepare_password_reauth($1,$2)`, sessionDigest[:], string(application)).Scan(&userID, &passwordHash, &locked); err != nil {
		return Reauthentication{}, postgresProviderError(ctx, err)
	}
	encoded := dummyPasswordHash
	if passwordHash.Valid {
		encoded = passwordHash.String
	}
	success := passwordHash.Valid && !locked && browserauth.VerifyPassword(encoded, password)
	proof, proofDigest, err := browserauth.NewProof()
	if err != nil {
		return Reauthentication{}, ErrUnavailable
	}
	sessionHandle, newSessionDigest, err := browserauth.NewProof()
	if err != nil {
		return Reauthentication{}, ErrUnavailable
	}
	eventID, correlationID, err := securityAuditIDs(ctx)
	if err != nil {
		return Reauthentication{}, err
	}
	finishCtx := databaseCtx
	finishCancel := func() {}
	if !success {
		finishCtx, finishCancel = context.WithTimeout(context.WithoutCancel(ctx), passwordAttemptTimeout)
	}
	defer finishCancel()
	var proofArgument any
	var sessionArgument any
	if success {
		proofArgument = proofDigest[:]
		sessionArgument = newSessionDigest[:]
	}
	var outcome string
	var expiresAt pgtype.Timestamptz
	if err := tx.QueryRow(finishCtx, `SELECT outcome,expires_at FROM cloud_agents_identity.finish_password_reauth($1,$2,$3,$4,$5,$6,$7,$8)`,
		sessionDigest[:], string(application), encoded, success, proofArgument, sessionArgument, eventID, correlationID,
	).Scan(&outcome, &expiresAt); err != nil {
		return Reauthentication{}, postgresProviderError(ctx, err)
	}
	if err := tx.Commit(finishCtx); err != nil {
		return Reauthentication{}, postgresProviderError(ctx, err)
	}
	if outcome == "reauthenticated" {
		if !expiresAt.Valid {
			return Reauthentication{}, ErrUnavailable
		}
		return Reauthentication{Proof: proof, SessionHandle: sessionHandle, ExpiresAt: expiresAt.Time.UTC()}, nil
	}
	if outcome == "locked" {
		return Reauthentication{}, ErrRateLimited
	}
	return Reauthentication{}, browserauth.ErrUnauthorized
}

func (store *ProviderAccountStore) UnlinkLoginMethod(ctx context.Context, application api.IdentityApplication, sessionDigest [sha256.Size]byte, methodID, reauthProof string) error {
	if ctx == nil || !validApplication(application) || !validProviderText(methodID, 128) {
		return ErrProviderInput
	}
	proofDigest, err := browserauth.ProofDigest(reauthProof)
	if err != nil {
		return browserauth.ErrUnauthorized
	}
	eventID, correlationID, err := securityAuditIDs(ctx)
	if err != nil {
		return err
	}
	databaseCtx, cancel := context.WithTimeout(ctx, databaseOperationTimeout)
	defer cancel()
	var unlinked bool
	if err := store.pool.QueryRow(databaseCtx, `SELECT cloud_agents_identity.unlink_login_method($1,$2,$3,$4,$5,$6)`,
		sessionDigest[:], string(application), methodID, proofDigest[:], eventID, correlationID,
	).Scan(&unlinked); err != nil {
		return postgresProviderError(ctx, err)
	}
	if !unlinked {
		return ErrUnavailable
	}
	return nil
}

func (store *ProviderAccountStore) EnablePassword(ctx context.Context, application api.IdentityApplication, sessionDigest [sha256.Size]byte, reauthProof, password string) error {
	if ctx == nil || !validApplication(application) {
		return ErrProviderInput
	}
	proofDigest, err := browserauth.ProofDigest(reauthProof)
	if err != nil {
		return browserauth.ErrUnauthorized
	}
	if err := store.passwords.acquireArgon2(ctx); err != nil {
		return err
	}
	defer store.passwords.releaseArgon2()
	passwordHash, err := browserauth.HashPassword(password)
	if err != nil {
		return ErrProviderInput
	}
	eventID, correlationID, err := securityAuditIDs(ctx)
	if err != nil {
		return err
	}
	databaseCtx, cancel := context.WithTimeout(ctx, databaseOperationTimeout)
	defer cancel()
	var enabled bool
	if err := store.pool.QueryRow(databaseCtx, `SELECT cloud_agents_identity.enable_password($1,$2,$3,$4,$5,$6)`,
		sessionDigest[:], string(application), proofDigest[:], passwordHash, eventID, correlationID,
	).Scan(&enabled); err != nil {
		return postgresProviderError(ctx, err)
	}
	if !enabled {
		return ErrUnavailable
	}
	return nil
}

func validProviderClient(client ProviderClient) bool {
	snapshot := client.ProviderClientSnapshot
	if snapshot.Revision == 0 {
		snapshot.Revision = 1
	}
	if !validProviderClientSnapshot(snapshot) || !validProviderText(client.DisplayName, 160) || !validProviderKind(client.Kind) ||
		!slices.IsSorted(client.Scopes) || slices.ContainsFunc(client.Scopes, func(scope string) bool { return !validProviderText(scope, 128) }) ||
		!slices.IsSorted(client.AllowedOrganizationIDs) || slices.ContainsFunc(client.AllowedOrganizationIDs, func(id string) bool { return !validProviderText(id, 255) }) ||
		hasAdjacentDuplicate(client.Scopes) || hasAdjacentDuplicate(client.AllowedOrganizationIDs) {
		return false
	}
	issuer, issuerErr := url.Parse(client.Issuer)
	redirect, redirectErr := url.Parse(client.RedirectURL)
	if issuerErr != nil || issuer.Scheme != "https" || issuer.Host == "" || issuer.User != nil || issuer.RawQuery != "" || issuer.Fragment != "" ||
		redirectErr != nil || redirect.Scheme != "https" || redirect.Host == "" || redirect.User != nil || redirect.Fragment != "" {
		return false
	}
	return (client.Kind == "wecom") == (client.AgentID != "") && (!client.TrustProviderEmail || len(client.AllowedOrganizationIDs) > 0)
}

func validProviderKind(kind string) bool {
	return slices.Contains([]string{"oidc", "github", "gitlab", "feishu", "dingtalk", "wecom"}, kind)
}

func hasAdjacentDuplicate(values []string) bool {
	for index := 1; index < len(values); index++ {
		if values[index] == values[index-1] {
			return true
		}
	}
	return false
}
