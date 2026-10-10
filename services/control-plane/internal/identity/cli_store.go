package identity

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/http"
	"net/netip"
	"time"

	common "github.com/hxp0618/cloud-agents/sdk/go/gen/common/v1alpha1"
	api "github.com/hxp0618/cloud-agents/sdk/go/gen/openapi/v1alpha1"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/browserauth"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const cliTenantCursorDomain = "cloud-agents.identity.cli-tenant-cursor.v1\x00"

var ErrCLIInvalid = errors.New("identity CLI request invalid")

type CLIStore struct {
	pool       *pgxpool.Pool
	signer     *TokenSigner
	authorizer PrincipalTokenAuthorizer
	authority  TokenSigningAuthorityProvider
	cursorKey  []byte
}

func NewCLIStore(pool *pgxpool.Pool, passwords *PasswordStore, signer *TokenSigner, authorizer PrincipalTokenAuthorizer, authority TokenSigningAuthorityProvider) (*CLIStore, error) {
	if pool == nil || passwords == nil || signer == nil || authorizer == nil || authority == nil || len(passwords.csrfKey) != sha256.Size {
		return nil, errInvalidConfiguration
	}
	return &CLIStore{pool: pool, signer: signer, authorizer: authorizer, authority: authority, cursorKey: append([]byte(nil), passwords.csrfKey...)}, nil
}

func (store *CLIStore) StartCLIAuthorization(ctx context.Context, application api.IdentityApplication, clientIP netip.Addr, request api.CLIAuthorizationStartRequest) (api.CLIAuthorization, error) {
	if ctx == nil || store == nil || store.pool == nil || application != request.Application || !validApplication(application) ||
		request.CallbackPort < 1024 || request.CallbackPort > 65535 || !validCLIClientIP(clientIP) {
		return api.CLIAuthorization{}, ErrCLIInvalid
	}
	stateDigest, stateErr := browserauth.ProofDigest(request.State)
	if stateErr != nil || !validCLIProof(request.CodeChallenge) {
		return api.CLIAuthorization{}, ErrCLIInvalid
	}
	if err := store.recordAnonymousAttempt(ctx, "start", clientIP); err != nil {
		return api.CLIAuthorization{}, err
	}
	authorizationID, err := newCLIIdentifier("cli-auth-")
	if err != nil {
		return api.CLIAuthorization{}, ErrUnavailable
	}
	databaseCtx, cancel := context.WithTimeout(ctx, databaseOperationTimeout)
	defer cancel()
	var expiresAt time.Time
	if err := store.pool.QueryRow(databaseCtx, `SELECT cloud_agents_identity.start_cli_authorization($1,$2,$3,$4,$5)`,
		authorizationID, string(application), request.CallbackPort, stateDigest[:], request.CodeChallenge).Scan(&expiresAt); err != nil {
		return api.CLIAuthorization{}, cliStoreError(ctx, err)
	}
	return api.CLIAuthorization{AuthorizationID: authorizationID, ExpiresAt: expiresAt.UTC().Format(time.RFC3339)}, nil
}

func (store *CLIStore) ApproveCLIAuthorization(ctx context.Context, application api.IdentityApplication, sessionDigest [sha256.Size]byte, authorizationID string, request api.CLIAuthorizationApproveRequest) (api.CLIAuthorizationApproved, error) {
	if ctx == nil || store == nil || store.pool == nil || !validApplication(application) || common.ValidateIdentifier(authorizationID, "/authorizationId") != nil {
		return api.CLIAuthorizationApproved{}, ErrCLIInvalid
	}
	stateDigest, err := browserauth.ProofDigest(request.State)
	if err != nil {
		return api.CLIAuthorizationApproved{}, ErrCLIInvalid
	}
	authorizationCode, codeDigest, err := browserauth.NewProof()
	if err != nil {
		return api.CLIAuthorizationApproved{}, ErrUnavailable
	}
	eventID, err := newAuditEventID()
	if err != nil {
		return api.CLIAuthorizationApproved{}, ErrUnavailable
	}
	correlationID, err := requestCorrelationID(ctx)
	if err != nil {
		return api.CLIAuthorizationApproved{}, ErrUnavailable
	}
	databaseCtx, cancel := context.WithTimeout(ctx, databaseOperationTimeout)
	defer cancel()
	var callbackPort int
	var approvedApplication string
	var expiresAt time.Time
	if err := store.pool.QueryRow(databaseCtx, `SELECT callback_port,application,expires_at
		FROM cloud_agents_identity.approve_cli_authorization($1,$2,$3,$4,$5,$6,$7)`,
		sessionDigest[:], string(application), authorizationID, stateDigest[:], codeDigest[:], eventID, correlationID,
	).Scan(&callbackPort, &approvedApplication, &expiresAt); err != nil {
		return api.CLIAuthorizationApproved{}, cliStoreError(ctx, err)
	}
	if approvedApplication != string(application) || callbackPort < 1024 || callbackPort > 65535 {
		return api.CLIAuthorizationApproved{}, ErrUnavailable
	}
	return api.CLIAuthorizationApproved{CallbackPort: callbackPort, State: request.State, AuthorizationCode: authorizationCode, ExpiresAt: expiresAt.UTC().Format(time.RFC3339)}, nil
}

func (store *CLIStore) ExchangeCLIGrant(ctx context.Context, application api.IdentityApplication, clientIP netip.Addr, request api.CLIGrantExchangeRequest) (api.CLIGrant, error) {
	if ctx == nil || store == nil || store.pool == nil || !validApplication(application) || !validCLIClientIP(clientIP) ||
		common.ValidateIdentifier(request.AuthorizationID, "/authorizationId") != nil {
		return api.CLIGrant{}, ErrCLIInvalid
	}
	codeDigest, codeErr := browserauth.ProofDigest(request.AuthorizationCode)
	if codeErr != nil || !validCLIProof(request.CodeVerifier) {
		return api.CLIGrant{}, ErrCLIInvalid
	}
	if err := store.recordAnonymousAttempt(ctx, "exchange", clientIP); err != nil {
		return api.CLIGrant{}, err
	}
	challengeDigest := sha256.Sum256([]byte(request.CodeVerifier))
	codeChallenge := base64.RawURLEncoding.EncodeToString(challengeDigest[:])
	grant, grantDigest, err := browserauth.NewProof()
	if err != nil {
		return api.CLIGrant{}, ErrUnavailable
	}
	grantID, err := newCLIIdentifier("cli-grant-")
	if err != nil {
		return api.CLIGrant{}, ErrUnavailable
	}
	eventID, err := newAuditEventID()
	if err != nil {
		return api.CLIGrant{}, ErrUnavailable
	}
	correlationID, err := requestCorrelationID(ctx)
	if err != nil {
		return api.CLIGrant{}, ErrUnavailable
	}
	databaseCtx, cancel := context.WithTimeout(ctx, databaseOperationTimeout)
	defer cancel()
	var userID, grantedApplication string
	var expiresAt time.Time
	if err := store.pool.QueryRow(databaseCtx, `SELECT user_id,application,expires_at
		FROM cloud_agents_identity.exchange_cli_authorization($1,$2,$3,$4,$5,$6,$7,$8)`,
		string(application), request.AuthorizationID, codeDigest[:], codeChallenge, grantID, grantDigest[:], eventID, correlationID,
	).Scan(&userID, &grantedApplication, &expiresAt); err != nil {
		return api.CLIGrant{}, cliStoreError(ctx, err)
	}
	if grantedApplication != string(application) || common.ValidateIdentifier(userID, "/userId") != nil {
		return api.CLIGrant{}, ErrUnavailable
	}
	return api.CLIGrant{Credential: grant, Application: application, ExpiresAt: expiresAt.UTC().Format(time.RFC3339)}, nil
}

func (store *CLIStore) ListCLITenants(ctx context.Context, application api.IdentityApplication, grantDigest [sha256.Size]byte, pageSize int, pageToken string) (api.BrowserTenantPage, error) {
	if ctx == nil || store == nil || store.pool == nil || !validApplication(application) || pageSize < 1 || pageSize > 200 {
		return api.BrowserTenantPage{}, ErrForbidden
	}
	after, err := store.cliTenantCursorAfter(grantDigest, application, pageToken)
	if err != nil {
		return api.BrowserTenantPage{}, err
	}
	databaseCtx, cancel := context.WithTimeout(ctx, databaseOperationTimeout)
	defer cancel()
	rows, err := store.pool.Query(databaseCtx, `SELECT tenant_id,tenant_name,tenant_admin
		FROM cloud_agents_identity.list_cli_tenants($1,$2,$3,$4)`, grantDigest[:], string(application), after, pageSize+1)
	if err != nil {
		return api.BrowserTenantPage{}, cliStoreError(ctx, err)
	}
	defer rows.Close()
	page := api.BrowserTenantPage{Tenants: make([]api.BrowserTenant, 0, pageSize)}
	for rows.Next() {
		var tenant api.BrowserTenant
		var tenantAdmin bool
		if err := rows.Scan(&tenant.ID, &tenant.Name, &tenantAdmin); err != nil {
			return api.BrowserTenantPage{}, ErrUnavailable
		}
		if len(page.Tenants) == pageSize {
			page.NextPageToken = store.cliTenantCursor(grantDigest, application, page.Tenants[len(page.Tenants)-1].ID)
			break
		}
		tenant.DisplayRoles = []string{}
		if tenantAdmin {
			tenant.DisplayRoles = append(tenant.DisplayRoles, "tenant.admin")
		}
		page.Tenants = append(page.Tenants, tenant)
	}
	if err := rows.Err(); err != nil {
		return api.BrowserTenantPage{}, cliStoreError(ctx, err)
	}
	return page, nil
}

func (store *CLIStore) IssueCLITenantToken(ctx context.Context, application api.IdentityApplication, grantDigest [sha256.Size]byte, request api.TenantTokenIssueRequest) (api.TenantToken, error) {
	if ctx == nil || store == nil || store.pool == nil || store.signer == nil || store.authorizer == nil || store.authority == nil {
		return api.TenantToken{}, ErrUnavailable
	}
	if !validApplication(application) || common.ValidateIdentifier(request.TenantID, "/tenantId") != nil ||
		request.ProjectID != "" && common.ValidateIdentifier(request.ProjectID, "/projectId") != nil {
		return api.TenantToken{}, ErrForbidden
	}
	correlationID, err := requestCorrelationID(ctx)
	if err != nil {
		return api.TenantToken{}, ErrUnavailable
	}
	authorizationCtx, cancelAuthorization := context.WithTimeout(ctx, databaseOperationTimeout)
	authorization, err := store.authorizer.AuthorizePrincipalToken(authorizationCtx, correlationID, api.PrincipalTokenAuthorizationRequest{
		Application: application, ClientID: CLIClientID,
		CredentialSHA256: "sha256:" + hex.EncodeToString(grantDigest[:]), TenantID: request.TenantID, ProjectID: request.ProjectID,
	})
	cancelAuthorization()
	if err != nil {
		if ctx.Err() != nil {
			return api.TenantToken{}, ctx.Err()
		}
		var clientError *api.ClientError
		if errors.As(err, &clientError) && clientError.Status == http.StatusForbidden && clientError.Problem != nil && clientError.Problem.Error.Code == "AUTHORIZATION_DENIED" {
			return api.TenantToken{}, ErrForbidden
		}
		return api.TenantToken{}, ErrUnavailable
	}
	if authorization.Application != application || authorization.TenantID != request.TenantID || authorization.ProjectID != request.ProjectID ||
		authorization.Issuer != store.signer.issuer || common.ValidateIdentifier(authorization.PrincipalID, "/principalId") != nil ||
		authorization.Subject.Kind != "user" || authorization.Subject.Issuer != authorization.Issuer ||
		authorization.Subject.Subject != "user-"+authorization.PrincipalID || authorization.Subject.Validate() != nil {
		return api.TenantToken{}, ErrUnavailable
	}
	authority, err := store.authority(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return api.TenantToken{}, ctx.Err()
		}
		return api.TenantToken{}, ErrUnavailable
	}
	signingRequest, err := NewTokenSigningRequest(TokenClientCLI, application, authorization.PrincipalID, request.TenantID, request.ProjectID, authorization.Scopes, authority, time.Now().UTC())
	if err != nil {
		return api.TenantToken{}, ErrUnavailable
	}
	signed, err := store.signer.Sign(signingRequest)
	if err != nil {
		return api.TenantToken{}, ErrUnavailable
	}
	tokenDigest, ok := parseTokenSHA256(signed.SHA256)
	if !ok {
		return api.TenantToken{}, ErrUnavailable
	}
	eventID, err := newAuditEventID()
	if err != nil {
		return api.TenantToken{}, ErrUnavailable
	}
	databaseCtx, cancelDatabase := context.WithTimeout(ctx, databaseOperationTimeout)
	defer cancelDatabase()
	var projectID any
	if request.ProjectID != "" {
		projectID = request.ProjectID
	}
	var recorded bool
	if err := store.pool.QueryRow(databaseCtx, `SELECT cloud_agents_identity.record_cli_issued_token($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		grantDigest[:], authorization.PrincipalID, authorization.Issuer, string(application), request.TenantID, projectID,
		signed.JTI, tokenDigest[:], signed.ExpiresAt, eventID, correlationID).Scan(&recorded); err != nil {
		return api.TenantToken{}, cliStoreError(ctx, err)
	}
	if !recorded {
		return api.TenantToken{}, browserauth.ErrUnauthorized
	}
	if err := ctx.Err(); err != nil {
		return api.TenantToken{}, err
	}
	return api.TenantToken{AccessToken: signed.Token, TokenType: "Bearer", ExpiresAt: signed.ExpiresAt.Format(time.RFC3339)}, nil
}

func (store *CLIStore) RevokeCLIGrant(ctx context.Context, application api.IdentityApplication, grantDigest [sha256.Size]byte) error {
	if ctx == nil || store == nil || store.pool == nil || !validApplication(application) {
		return ErrCLIInvalid
	}
	eventID, err := newAuditEventID()
	if err != nil {
		return ErrUnavailable
	}
	correlationID, err := requestCorrelationID(ctx)
	if err != nil {
		return ErrUnavailable
	}
	databaseCtx, cancel := context.WithTimeout(ctx, databaseOperationTimeout)
	defer cancel()
	var revoked bool
	if err := store.pool.QueryRow(databaseCtx, `SELECT cloud_agents_identity.revoke_cli_grant($1,$2,$3,$4)`, grantDigest[:], string(application), eventID, correlationID).Scan(&revoked); err != nil {
		return cliStoreError(ctx, err)
	}
	if !revoked {
		return ErrUnavailable
	}
	return nil
}

func (store *CLIStore) recordAnonymousAttempt(ctx context.Context, kind string, clientIP netip.Addr) error {
	if !validCLIClientIP(clientIP) || kind != "start" && kind != "exchange" {
		return ErrCLIInvalid
	}
	bucket := sha256.Sum256([]byte("cloud-agents.identity.cli-ip.v1\x00" + clientIP.Unmap().String()))
	databaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), passwordAttemptTimeout)
	defer cancel()
	var permitted bool
	if err := store.pool.QueryRow(databaseCtx, `SELECT cloud_agents_identity.record_cli_anonymous_attempt($1,$2)`, kind, bucket[:]).Scan(&permitted); err != nil {
		return cliStoreError(ctx, err)
	}
	if !permitted {
		return ErrRateLimited
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return nil
}

func (store *CLIStore) cliTenantCursor(grant [sha256.Size]byte, application api.IdentityApplication, after string) string {
	raw := append([]byte(after), store.cliTenantCursorMAC(grant, application, after)...)
	return base64.RawURLEncoding.EncodeToString(raw)
}

func (store *CLIStore) cliTenantCursorAfter(grant [sha256.Size]byte, application api.IdentityApplication, value string) (string, error) {
	if value == "" {
		return "", nil
	}
	if len(value) > 256 {
		return "", ErrForbidden
	}
	raw, err := base64.RawURLEncoding.Strict().DecodeString(value)
	if err != nil || len(raw) <= sha256.Size || base64.RawURLEncoding.EncodeToString(raw) != value {
		return "", ErrForbidden
	}
	after := string(raw[:len(raw)-sha256.Size])
	if common.ValidateIdentifier(after, "/pageToken") != nil || !hmac.Equal(raw[len(raw)-sha256.Size:], store.cliTenantCursorMAC(grant, application, after)) {
		return "", ErrForbidden
	}
	return after, nil
}

func (store *CLIStore) cliTenantCursorMAC(grant [sha256.Size]byte, application api.IdentityApplication, after string) []byte {
	mac := hmac.New(sha256.New, store.cursorKey)
	_, _ = mac.Write([]byte(cliTenantCursorDomain))
	_, _ = mac.Write(grant[:])
	_, _ = mac.Write([]byte(application))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(after))
	return mac.Sum(nil)
}

func newCLIIdentifier(prefix string) (string, error) {
	id, err := newAuditEventID()
	if err != nil {
		return "", err
	}
	return prefix + id, nil
}

func validCLIProof(value string) bool {
	_, err := browserauth.ProofDigest(value)
	return err == nil
}

func validCLIClientIP(clientIP netip.Addr) bool {
	return clientIP.IsValid() && clientIP.Zone() == ""
}

func cliStoreError(ctx context.Context, err error) error {
	if ctx != nil && ctx.Err() != nil {
		return ctx.Err()
	}
	var databaseError *pgconn.PgError
	if errors.As(err, &databaseError) {
		switch databaseError.Code {
		case "22023":
			return ErrCLIInvalid
		case "28000":
			return browserauth.ErrUnauthorized
		case "53300":
			return ErrRateLimited
		}
	}
	return ErrUnavailable
}
