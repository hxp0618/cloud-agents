package identity

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/netip"
	"strings"
	"time"

	api "github.com/hxp0618/cloud-agents/sdk/go/gen/openapi/v1alpha1"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/browserauth"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	databaseOperationTimeout  = 5 * time.Second
	passwordAttemptTimeout    = 5 * time.Second
	transactionCleanupTimeout = time.Second
	argon2ConcurrencyLimit    = 8
	ipBucketPrefix            = "ip:"
	csrfDomain                = "cloud-agents.identity.csrf.v1"

	// This is a valid profile-conforming hash used to keep unknown, disabled,
	// and passwordless accounts on the same Argon2 verification path. It is
	// deliberately a fixed value and is never persisted or returned.
	dummyPasswordHash = "$argon2id$v=19$m=19456,t=2,p=1$AAAAAAAAAAAAAAAAAAAAAA$AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
)

var (
	ErrUnavailable          = errors.New("identity store unavailable")
	errInvalidConfiguration = errors.New("identity store configuration invalid")
)

// PasswordStore owns password-backed browser sessions. It only uses the
// narrow SECURITY DEFINER functions exposed by the identity schema.
type PasswordStore struct {
	pool      *pgxpool.Pool
	csrfKey   []byte
	argon2Sem chan struct{}
}

func NewPasswordStore(pool *pgxpool.Pool, csrfKey []byte) (*PasswordStore, error) {
	if pool == nil || len(csrfKey) != 32 {
		return nil, errInvalidConfiguration
	}
	return &PasswordStore{
		pool:      pool,
		csrfKey:   append([]byte(nil), csrfKey...),
		argon2Sem: make(chan struct{}, argon2ConcurrencyLimit),
	}, nil
}

func (store *PasswordStore) PasswordLogin(ctx context.Context, application api.IdentityApplication, clientIP netip.Addr, request api.PasswordLoginRequest) (api.IdentityLoginResult, error) {
	if err := ctx.Err(); err != nil {
		return api.IdentityLoginResult{}, err
	}
	if !validApplication(application) {
		return api.IdentityLoginResult{}, browserauth.ErrUnauthorized
	}
	correlationID, err := requestCorrelationID(ctx)
	if err != nil {
		return api.IdentityLoginResult{}, ErrUnavailable
	}
	normalizedDomain, err := browserauth.NormalizeEmailDomain(request.Email)
	if err != nil {
		return api.IdentityLoginResult{}, browserauth.ErrUnauthorized
	}
	at := strings.LastIndexByte(request.Email, '@')
	if at < 1 || at == len(request.Email)-1 {
		return api.IdentityLoginResult{}, browserauth.ErrUnauthorized
	}
	canonicalEmail := request.Email[:at] + "@" + normalizedDomain
	accountBucket, ipBucket, ok := loginBuckets(canonicalEmail, clientIP)
	if !ok {
		return api.IdentityLoginResult{}, browserauth.ErrUnauthorized
	}
	if err := store.acquireArgon2(ctx); err != nil {
		return api.IdentityLoginResult{}, err
	}
	defer store.releaseArgon2()

	requestCtx, cancelRequest := context.WithTimeout(ctx, databaseOperationTimeout)
	defer cancelRequest()
	tx, err := store.pool.BeginTx(requestCtx, pgx.TxOptions{})
	if err != nil {
		return api.IdentityLoginResult{}, ErrUnavailable
	}
	defer rollbackTransaction(tx)

	var matchedUser, matchedPassword pgtype.Text
	var locked bool
	if err := tx.QueryRow(requestCtx, `
		SELECT user_id, password_hash, locked
		FROM cloud_agents_identity.prepare_password_login($1, $2, $3)
	`, canonicalEmail, accountBucket[:], ipBucket[:]).Scan(&matchedUser, &matchedPassword, &locked); err != nil {
		return api.IdentityLoginResult{}, ErrUnavailable
	}

	passwordHash := dummyPasswordHash
	if matchedPassword.Valid {
		passwordHash = matchedPassword.String
	}
	success := !locked && browserauth.VerifyPassword(passwordHash, request.Password)
	var finishCtx context.Context
	var cancelFinish context.CancelFunc
	if success {
		if err := ctx.Err(); err != nil {
			return api.IdentityLoginResult{}, err
		}
		if err := requestCtx.Err(); err != nil {
			return api.IdentityLoginResult{}, err
		}
		finishCtx = requestCtx
		cancelFinish = func() {}
	} else {
		finishCtx, cancelFinish = context.WithTimeout(context.WithoutCancel(ctx), passwordAttemptTimeout)
	}
	defer cancelFinish()

	var sessionHandle string
	var sessionDigest [32]byte
	if success {
		sessionHandle, sessionDigest, err = browserauth.NewProof()
		if err != nil {
			return api.IdentityLoginResult{}, ErrUnavailable
		}
	}
	eventID, err := newAuditEventID()
	if err != nil {
		return api.IdentityLoginResult{}, ErrUnavailable
	}
	var outcome string
	if err := tx.QueryRow(finishCtx, `
		SELECT cloud_agents_identity.finish_password_login($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`, canonicalEmail, accountBucket[:], ipBucket[:], passwordHash, success, string(application), sessionDigest[:], eventID, correlationID).Scan(&outcome); err != nil {
		if success && ctx.Err() != nil {
			return api.IdentityLoginResult{}, ctx.Err()
		}
		return api.IdentityLoginResult{}, ErrUnavailable
	}

	if outcome == "authenticated" {
		var userID, email, displayName string
		var platformAdmin bool
		if err := tx.QueryRow(finishCtx, `
			SELECT user_id, email, display_name, platform_admin
			FROM cloud_agents_identity.read_session($1, $2)
		`, sessionDigest[:], string(application)).Scan(&userID, &email, &displayName, &platformAdmin); err != nil {
			if ctx.Err() != nil {
				return api.IdentityLoginResult{}, ctx.Err()
			}
			return api.IdentityLoginResult{}, ErrUnavailable
		}
		page, err := store.readTenantPage(finishCtx, tx, application, sessionDigest, initialTenantPageSize, "")
		if err != nil {
			return api.IdentityLoginResult{}, err
		}
		if err := tx.Commit(finishCtx); err != nil {
			if ctx.Err() != nil {
				return api.IdentityLoginResult{}, ctx.Err()
			}
			return api.IdentityLoginResult{}, ErrUnavailable
		}
		session := browserSession(application, userID, email, displayName, platformAdmin, store.csrfProof(sessionDigest, application))
		session.Tenants, session.NextPageToken = page.Tenants, page.NextPageToken
		return api.IdentityLoginResult{
			Session:       session,
			SessionHandle: sessionHandle,
		}, nil
	}

	if outcome != "denied" && outcome != "locked" {
		return api.IdentityLoginResult{}, ErrUnavailable
	}
	if err := tx.Commit(finishCtx); err != nil {
		return api.IdentityLoginResult{}, ErrUnavailable
	}
	if outcome == "locked" {
		return api.IdentityLoginResult{}, ErrRateLimited
	}
	return api.IdentityLoginResult{}, browserauth.ErrUnauthorized
}

func (store *PasswordStore) Session(ctx context.Context, application api.IdentityApplication, sessionDigest [32]byte) (api.BrowserSession, error) {
	if err := ctx.Err(); err != nil {
		return api.BrowserSession{}, err
	}
	if !validApplication(application) {
		return api.BrowserSession{}, browserauth.ErrUnauthorized
	}
	databaseCtx, cancelDatabase := context.WithTimeout(ctx, databaseOperationTimeout)
	defer cancelDatabase()
	tx, err := store.pool.BeginTx(databaseCtx, pgx.TxOptions{})
	if err != nil {
		return api.BrowserSession{}, ErrUnavailable
	}
	defer rollbackTransaction(tx)

	var userID, email, displayName string
	var platformAdmin bool
	if err := tx.QueryRow(databaseCtx, `
		SELECT user_id, email, display_name, platform_admin
		FROM cloud_agents_identity.read_session($1, $2)
	`, sessionDigest[:], string(application)).Scan(&userID, &email, &displayName, &platformAdmin); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return api.BrowserSession{}, browserauth.ErrUnauthorized
		}
		return api.BrowserSession{}, ErrUnavailable
	}
	page, err := store.readTenantPage(databaseCtx, tx, application, sessionDigest, initialTenantPageSize, "")
	if err != nil {
		return api.BrowserSession{}, err
	}
	if err := tx.Commit(databaseCtx); err != nil {
		return api.BrowserSession{}, ErrUnavailable
	}
	session := browserSession(application, userID, email, displayName, platformAdmin, store.csrfProof(sessionDigest, application))
	session.Tenants, session.NextPageToken = page.Tenants, page.NextPageToken
	return session, nil
}

func (store *PasswordStore) Logout(ctx context.Context, application api.IdentityApplication, sessionDigest [32]byte, csrfProof string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !validApplication(application) {
		return ErrForbidden
	}
	correlationID, err := requestCorrelationID(ctx)
	if err != nil {
		return ErrUnavailable
	}
	expected := store.csrfDigest(sessionDigest, application)
	provided, proofErr := decodeCSRFProof(csrfProof)
	if proofErr != nil {
		provided = [32]byte{}
	}
	if subtle.ConstantTimeCompare(provided[:], expected[:]) != 1 || proofErr != nil {
		return ErrForbidden
	}
	eventID, err := newAuditEventID()
	if err != nil {
		return ErrUnavailable
	}
	databaseCtx, cancelDatabase := context.WithTimeout(ctx, databaseOperationTimeout)
	defer cancelDatabase()
	tx, err := store.pool.BeginTx(databaseCtx, pgx.TxOptions{})
	if err != nil {
		return ErrUnavailable
	}
	defer rollbackTransaction(tx)
	var revoked bool
	if err := tx.QueryRow(databaseCtx, `
		SELECT cloud_agents_identity.revoke_session($1, $2, $3, $4)
	`, sessionDigest[:], string(application), eventID, correlationID).Scan(&revoked); err != nil {
		return ErrUnavailable
	}
	if !revoked {
		return browserauth.ErrUnauthorized
	}
	if err := tx.Commit(databaseCtx); err != nil {
		return ErrUnavailable
	}
	return nil
}

func rollbackTransaction(tx pgx.Tx) {
	cleanupCtx, cancel := context.WithTimeout(context.Background(), transactionCleanupTimeout)
	defer cancel()
	_ = tx.Rollback(cleanupCtx)
}

func (store *PasswordStore) acquireArgon2(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	select {
	case store.argon2Sem <- struct{}{}:
		return nil
	default:
		return ErrRateLimited
	}
}

func (store *PasswordStore) releaseArgon2() {
	<-store.argon2Sem
}

func (store *PasswordStore) csrfDigest(sessionDigest [32]byte, application api.IdentityApplication) [32]byte {
	mac := hmac.New(sha256.New, store.csrfKey)
	_, _ = mac.Write([]byte(csrfDomain))
	_, _ = mac.Write(sessionDigest[:])
	_, _ = mac.Write([]byte(application))
	var digest [32]byte
	copy(digest[:], mac.Sum(nil))
	return digest
}

func (store *PasswordStore) csrfProof(sessionDigest [32]byte, application api.IdentityApplication) string {
	digest := store.csrfDigest(sessionDigest, application)
	return base64.RawURLEncoding.EncodeToString(digest[:])
}

func decodeCSRFProof(raw string) ([32]byte, error) {
	if len(raw) != base64.RawURLEncoding.EncodedLen(sha256.Size) {
		return [32]byte{}, errors.New("invalid CSRF proof")
	}
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(raw)
	if err != nil || len(decoded) != sha256.Size || base64.RawURLEncoding.EncodeToString(decoded) != raw {
		return [32]byte{}, errors.New("invalid CSRF proof")
	}
	var digest [32]byte
	copy(digest[:], decoded)
	return digest, nil
}

func browserSession(application api.IdentityApplication, userID, email, displayName string, platformAdmin bool, csrfProof string) api.BrowserSession {
	roles := make([]string, 0, 1)
	if platformAdmin {
		roles = append(roles, "platform.admin")
	}
	return api.BrowserSession{
		Application: application,
		User: api.CurrentUser{
			ID:           userID,
			Email:        email,
			DisplayName:  displayName,
			DisplayRoles: roles,
		},
		Tenants:   []api.BrowserTenant{},
		CSRFToken: csrfProof,
	}
}

func loginBuckets(email string, clientIP netip.Addr) ([32]byte, [32]byte, bool) {
	var accountBucket, ipBucket [32]byte
	if !clientIP.IsValid() || clientIP.Zone() != "" {
		return accountBucket, ipBucket, false
	}
	if !strings.Contains(email, "@") {
		return accountBucket, ipBucket, false
	}
	accountBucket = sha256.Sum256([]byte(email))
	ipBucket = sha256.Sum256([]byte(ipBucketPrefix + clientIP.Unmap().String()))
	return accountBucket, ipBucket, true
}

func newAuditEventID() (string, error) {
	var randomID [16]byte
	if _, err := rand.Read(randomID[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(randomID[:]), nil
}

func validApplication(application api.IdentityApplication) bool {
	return application == api.IdentityApplicationAdmin || application == api.IdentityApplicationUser
}
