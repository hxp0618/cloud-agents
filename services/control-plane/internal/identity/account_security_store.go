package identity

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/netip"
	"strings"
	"time"

	common "github.com/hxp0618/cloud-agents/sdk/go/gen/common/v1alpha1"
	api "github.com/hxp0618/cloud-agents/sdk/go/gen/openapi/v1alpha1"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/browserauth"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

const accountSecurityCursorDomain = "cloud-agents.identity.account-security-cursor.v1\x00"

var (
	ErrAccountSecurityInvalid  = errors.New("identity account security input invalid")
	ErrAccountSecurityConflict = errors.New("identity account security conflict")
	ErrAccountSecurityNotFound = errors.New("identity account unavailable")
)

type AccountSecurityStore struct {
	pool      *pgxpool.Pool
	passwords *PasswordStore
}

func NewAccountSecurityStore(pool *pgxpool.Pool, passwords *PasswordStore) (*AccountSecurityStore, error) {
	if pool == nil || passwords == nil || passwords.pool != pool {
		return nil, errInvalidConfiguration
	}
	return &AccountSecurityStore{pool: pool, passwords: passwords}, nil
}

func (store *AccountSecurityStore) ListAccounts(ctx context.Context, sessionDigest [32]byte, pageSize int, pageToken string) (api.IdentityAccountPage, error) {
	return store.listAccounts(ctx, sessionDigest, "", pageSize, pageToken)
}

func (store *AccountSecurityStore) ListTenantAccounts(ctx context.Context, sessionDigest [32]byte, tenantID string, pageSize int, pageToken string) (api.IdentityAccountPage, error) {
	if common.ValidateIdentifier(tenantID, "/tenantId") != nil {
		return api.IdentityAccountPage{}, ErrAccountSecurityInvalid
	}
	return store.listAccounts(ctx, sessionDigest, tenantID, pageSize, pageToken)
}

func (store *AccountSecurityStore) listAccounts(ctx context.Context, sessionDigest [32]byte, tenantID string, pageSize int, pageToken string) (api.IdentityAccountPage, error) {
	scope := "accounts:" + tenantID
	after, err := store.cursorAfter(sessionDigest, scope, pageToken)
	if ctx == nil || pageSize < 1 || pageSize > 200 || err != nil || (after != "" && common.ValidateIdentifier(after, "/pageToken") != nil) {
		return api.IdentityAccountPage{}, ErrAccountSecurityInvalid
	}
	databaseCtx, cancel := context.WithTimeout(ctx, databaseOperationTimeout)
	defer cancel()
	query := `SELECT id,subject_issuer,email,display_name,state,platform_admin,email_verified_at,created_at FROM cloud_agents_identity.list_accounts($1,$2,$3)`
	arguments := []any{sessionDigest[:], after, pageSize + 1}
	if tenantID != "" {
		query = `SELECT id,subject_issuer,email,display_name,state,platform_admin,email_verified_at,created_at FROM cloud_agents_identity.list_tenant_accounts($1,$2,$3,$4)`
		arguments = []any{sessionDigest[:], tenantID, after, pageSize + 1}
	}
	rows, err := store.pool.Query(databaseCtx, query, arguments...)
	if err != nil {
		return api.IdentityAccountPage{}, accountSecurityError(ctx, err)
	}
	defer rows.Close()
	page := api.IdentityAccountPage{Accounts: make([]api.IdentityAccount, 0, pageSize)}
	for rows.Next() {
		var account api.IdentityAccount
		var subjectIssuer string
		var verifiedAt, createdAt time.Time
		if err := rows.Scan(&account.ID, &subjectIssuer, &account.Email, &account.DisplayName, &account.State, &account.PlatformAdmin, &verifiedAt, &createdAt); err != nil {
			return api.IdentityAccountPage{}, ErrUnavailable
		}
		if len(page.Accounts) == pageSize {
			page.NextPageToken = store.cursor(sessionDigest, scope, page.Accounts[len(page.Accounts)-1].ID)
			break
		}
		account.EmailVerifiedAt = verifiedAt.UTC().Format(time.RFC3339Nano)
		account.CreatedAt = createdAt.UTC().Format(time.RFC3339Nano)
		account.Subject = common.SubjectRef{Kind: "user", Issuer: subjectIssuer, Subject: "user-" + account.ID}
		page.Accounts = append(page.Accounts, account)
	}
	if err := rows.Err(); err != nil {
		return api.IdentityAccountPage{}, accountSecurityError(ctx, err)
	}
	if _, err := api.EncodeIdentityAccountPageJSON(page); err != nil {
		return api.IdentityAccountPage{}, ErrUnavailable
	}
	return page, nil
}

func (store *AccountSecurityStore) DisableAccount(ctx context.Context, sessionDigest [32]byte, userID string) error {
	if ctx == nil || common.ValidateIdentifier(userID, "/userId") != nil {
		return ErrAccountSecurityInvalid
	}
	eventID, correlationID, err := securityAuditIDs(ctx)
	if err != nil {
		return err
	}
	databaseCtx, cancel := context.WithTimeout(ctx, databaseOperationTimeout)
	defer cancel()
	var disabled bool
	err = store.pool.QueryRow(databaseCtx, `SELECT cloud_agents_identity.disable_account($1,$2,$3,$4)`, sessionDigest[:], userID, eventID, correlationID).Scan(&disabled)
	if err != nil {
		return accountSecurityError(ctx, err)
	}
	if !disabled {
		return ErrUnavailable
	}
	return nil
}

func (store *AccountSecurityStore) IssuePasswordReset(ctx context.Context, sessionDigest [32]byte, userID string) (api.PasswordResetCreated, error) {
	if ctx == nil || common.ValidateIdentifier(userID, "/userId") != nil {
		return api.PasswordResetCreated{}, ErrAccountSecurityInvalid
	}
	code, digest, err := browserauth.NewProof()
	if err != nil {
		return api.PasswordResetCreated{}, ErrUnavailable
	}
	eventID, correlationID, err := securityAuditIDs(ctx)
	if err != nil {
		return api.PasswordResetCreated{}, err
	}
	databaseCtx, cancel := context.WithTimeout(ctx, databaseOperationTimeout)
	defer cancel()
	var expiresAt time.Time
	err = store.pool.QueryRow(databaseCtx, `SELECT cloud_agents_identity.issue_password_reset($1,$2,$3,$4,$5)`, sessionDigest[:], userID, digest[:], eventID, correlationID).Scan(&expiresAt)
	if err != nil {
		return api.PasswordResetCreated{}, accountSecurityError(ctx, err)
	}
	result := api.PasswordResetCreated{UserID: userID, ResetCode: code, ExpiresAt: expiresAt.UTC().Format(time.RFC3339Nano)}
	if _, err := api.EncodePasswordResetCreatedJSON(result); err != nil {
		return api.PasswordResetCreated{}, ErrUnavailable
	}
	return result, nil
}

func (store *AccountSecurityStore) ChangePassword(ctx context.Context, application api.IdentityApplication, sessionDigest [32]byte, request api.PasswordChangeRequest) error {
	if ctx == nil || !validApplication(application) {
		return ErrAccountSecurityInvalid
	}
	encoded, err := api.EncodePasswordChangeRequestJSON(request)
	if err != nil || len(encoded) == 0 {
		return ErrAccountSecurityInvalid
	}
	if err := store.passwords.acquireArgon2(ctx); err != nil {
		return err
	}
	defer store.passwords.releaseArgon2()
	databaseCtx, cancel := context.WithTimeout(ctx, databaseOperationTimeout)
	defer cancel()
	tx, err := store.pool.BeginTx(databaseCtx, pgx.TxOptions{})
	if err != nil {
		return ErrUnavailable
	}
	defer rollbackTransaction(tx)
	var userID, currentHash string
	var locked bool
	if err := tx.QueryRow(databaseCtx, `SELECT user_id,password_hash,locked FROM cloud_agents_identity.prepare_password_change($1,$2)`, sessionDigest[:], string(application)).Scan(&userID, &currentHash, &locked); err != nil {
		return accountSecurityError(ctx, err)
	}
	success := !locked && browserauth.VerifyPassword(currentHash, request.CurrentPassword)
	var newHash any
	if success {
		hashed, hashErr := browserauth.HashPassword(request.NewPassword)
		if hashErr != nil {
			return ErrAccountSecurityInvalid
		}
		newHash = hashed
	}
	eventID, correlationID, err := securityAuditIDs(ctx)
	if err != nil {
		return err
	}
	finishCtx := databaseCtx
	finishCancel := func() {}
	if !success {
		finishCtx, finishCancel = context.WithTimeout(context.WithoutCancel(ctx), passwordAttemptTimeout)
	}
	defer finishCancel()
	var outcome string
	if err := tx.QueryRow(finishCtx, `SELECT cloud_agents_identity.finish_password_change($1,$2,$3,$4,$5,$6,$7)`, sessionDigest[:], string(application), currentHash, success, newHash, eventID, correlationID).Scan(&outcome); err != nil {
		return accountSecurityError(ctx, err)
	}
	if err := tx.Commit(finishCtx); err != nil {
		return accountSecurityError(ctx, err)
	}
	if outcome == "changed" {
		return nil
	}
	if outcome == "locked" {
		return ErrRateLimited
	}
	return browserauth.ErrUnauthorized
}

func (store *AccountSecurityStore) AcceptPasswordReset(ctx context.Context, clientIP netip.Addr, request api.PasswordResetAcceptRequest) error {
	if ctx == nil || !clientIP.IsValid() || clientIP.Zone() != "" {
		return ErrAccountSecurityInvalid
	}
	encoded, err := api.EncodePasswordResetAcceptRequestJSON(request)
	if err != nil || len(encoded) == 0 {
		return ErrAccountSecurityInvalid
	}
	digest, err := browserauth.ProofDigest(request.ResetCode)
	if err != nil {
		return browserauth.ErrUnauthorized
	}
	ipBucket := sha256.Sum256([]byte("password-reset-ip:" + clientIP.Unmap().String()))
	prepareCtx, cancelPrepare := context.WithTimeout(context.WithoutCancel(ctx), databaseOperationTimeout)
	var userID pgtype.Text
	var permitted bool
	err = store.pool.QueryRow(prepareCtx, `SELECT user_id,permitted FROM cloud_agents_identity.prepare_password_reset($1,$2)`, digest[:], ipBucket[:]).Scan(&userID, &permitted)
	cancelPrepare()
	if err != nil {
		return accountSecurityError(ctx, err)
	}
	if !permitted || !userID.Valid {
		return browserauth.ErrUnauthorized
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := store.passwords.acquireArgon2(ctx); err != nil {
		return err
	}
	defer store.passwords.releaseArgon2()
	passwordHash, err := browserauth.HashPassword(request.NewPassword)
	if err != nil {
		return ErrAccountSecurityInvalid
	}
	eventID, correlationID, err := securityAuditIDs(ctx)
	if err != nil {
		return err
	}
	databaseCtx, cancel := context.WithTimeout(ctx, databaseOperationTimeout)
	defer cancel()
	var accepted bool
	err = store.pool.QueryRow(databaseCtx, `SELECT cloud_agents_identity.finish_password_reset($1,$2,$3,$4,$5,$6)`, digest[:], ipBucket[:], userID.String, passwordHash, eventID, correlationID).Scan(&accepted)
	if err != nil {
		return accountSecurityError(ctx, err)
	}
	if !accepted {
		return ErrUnavailable
	}
	return nil
}

func (store *AccountSecurityStore) ListAuditEvents(ctx context.Context, sessionDigest [32]byte, tenantID string, pageSize int, pageToken string) (api.IdentityAuditPage, error) {
	if ctx == nil || pageSize < 1 || pageSize > 200 || (tenantID != "" && common.ValidateIdentifier(tenantID, "/tenantId") != nil) {
		return api.IdentityAuditPage{}, ErrAccountSecurityInvalid
	}
	afterAt, afterID, err := store.auditCursorAfter(sessionDigest, tenantID, pageToken)
	if err != nil {
		return api.IdentityAuditPage{}, err
	}
	var tenant any
	if tenantID != "" {
		tenant = tenantID
	}
	var at any
	var id any
	if !afterAt.IsZero() {
		at, id = afterAt, afterID
	}
	databaseCtx, cancel := context.WithTimeout(ctx, databaseOperationTimeout)
	defer cancel()
	rows, err := store.pool.Query(databaseCtx, `SELECT id,event_kind,actor_user_id,target_user_id,tenant_id,application,decision,reason_code,correlation_id,occurred_at FROM cloud_agents_identity.list_audit_events($1,$2,$3,$4,$5)`, sessionDigest[:], tenant, at, id, pageSize+1)
	if err != nil {
		return api.IdentityAuditPage{}, accountSecurityError(ctx, err)
	}
	defer rows.Close()
	page := api.IdentityAuditPage{Events: make([]api.IdentityAuditEvent, 0, pageSize)}
	for rows.Next() {
		var event api.IdentityAuditEvent
		var actor, target, eventTenant, application pgtype.Text
		var occurredAt time.Time
		if err := rows.Scan(&event.ID, &event.EventKind, &actor, &target, &eventTenant, &application, &event.Decision, &event.ReasonCode, &event.CorrelationID, &occurredAt); err != nil {
			return api.IdentityAuditPage{}, ErrUnavailable
		}
		if len(page.Events) == pageSize {
			last := page.Events[len(page.Events)-1]
			page.NextPageToken = store.auditCursor(sessionDigest, tenantID, last.OccurredAt, last.ID)
			break
		}
		if actor.Valid {
			event.ActorUserID = actor.String
		}
		if target.Valid {
			event.TargetUserID = target.String
		}
		if eventTenant.Valid {
			event.TenantID = eventTenant.String
		}
		if application.Valid {
			event.Application = api.IdentityApplication(application.String)
		}
		event.OccurredAt = occurredAt.UTC().Format(time.RFC3339Nano)
		page.Events = append(page.Events, event)
	}
	if err := rows.Err(); err != nil {
		return api.IdentityAuditPage{}, accountSecurityError(ctx, err)
	}
	if _, err := api.EncodeIdentityAuditPageJSON(page); err != nil {
		return api.IdentityAuditPage{}, ErrUnavailable
	}
	return page, nil
}

func securityAuditIDs(ctx context.Context) (string, string, error) {
	eventID, err := newAuditEventID()
	if err != nil {
		return "", "", ErrUnavailable
	}
	correlationID, err := requestCorrelationID(ctx)
	if err != nil {
		return "", "", ErrUnavailable
	}
	return eventID, correlationID, nil
}

func (store *AccountSecurityStore) cursor(session [32]byte, scope, after string) string {
	raw := []byte(after)
	return base64.RawURLEncoding.EncodeToString(append(raw, store.cursorMAC(session, scope, after)...))
}
func (store *AccountSecurityStore) cursorAfter(session [32]byte, scope, value string) (string, error) {
	if value == "" {
		return "", nil
	}
	if len(value) > 2048 {
		return "", ErrAccountSecurityInvalid
	}
	raw, err := base64.RawURLEncoding.Strict().DecodeString(value)
	if err != nil || len(raw) <= sha256.Size || base64.RawURLEncoding.EncodeToString(raw) != value {
		return "", ErrAccountSecurityInvalid
	}
	after := string(raw[:len(raw)-sha256.Size])
	if !hmac.Equal(raw[len(raw)-sha256.Size:], store.cursorMAC(session, scope, after)) {
		return "", ErrAccountSecurityInvalid
	}
	return after, nil
}
func (store *AccountSecurityStore) cursorMAC(session [32]byte, scope, after string) []byte {
	mac := hmac.New(sha256.New, store.passwords.csrfKey)
	mac.Write([]byte(accountSecurityCursorDomain))
	mac.Write(session[:])
	mac.Write([]byte{0})
	mac.Write([]byte(scope))
	mac.Write([]byte{0})
	mac.Write([]byte(after))
	return mac.Sum(nil)
}
func (store *AccountSecurityStore) auditCursor(session [32]byte, tenantID, at, id string) string {
	return store.cursor(session, "audit:"+tenantID, at+"|"+id)
}
func (store *AccountSecurityStore) auditCursorAfter(session [32]byte, tenantID, value string) (time.Time, string, error) {
	raw, err := store.cursorAfter(session, "audit:"+tenantID, value)
	if err != nil || raw == "" {
		return time.Time{}, "", err
	}
	parts := strings.Split(raw, "|")
	if len(parts) != 2 || common.ValidateIdentifier(parts[1], "/pageToken") != nil {
		return time.Time{}, "", ErrAccountSecurityInvalid
	}
	at, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil {
		return time.Time{}, "", ErrAccountSecurityInvalid
	}
	return at, parts[1], nil
}

func accountSecurityError(ctx context.Context, err error) error {
	if ctx != nil && ctx.Err() != nil {
		return ctx.Err()
	}
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) {
		switch postgresError.Code {
		case "22023":
			return ErrAccountSecurityInvalid
		case "28000":
			return browserauth.ErrUnauthorized
		case "42501":
			return ErrForbidden
		case "40001", "23505":
			return ErrAccountSecurityConflict
		case "P0002":
			return ErrAccountSecurityNotFound
		case "53300":
			return ErrRateLimited
		}
	}
	return ErrUnavailable
}
