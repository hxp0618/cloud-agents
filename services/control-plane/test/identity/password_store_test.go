package identity_test

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/netip"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	api "github.com/hxp0618/cloud-agents/sdk/go/gen/openapi/v1alpha1"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/browserauth"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/identity"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPasswordStorePostgresFlow(t *testing.T) {
	databaseURL := os.Getenv("CLOUD_AGENTS_IDENTITY_TEST_DATABASE_URL")
	bootstrapURL := os.Getenv("CLOUD_AGENTS_IDENTITY_TEST_BOOTSTRAP_DATABASE_URL")
	fixtureURL := os.Getenv("CLOUD_AGENTS_IDENTITY_TEST_FIXTURE_DATABASE_URL")
	if databaseURL == "" || bootstrapURL == "" || fixtureURL == "" {
		t.Skip("identity PostgreSQL test requires a fresh database with service, bootstrap and fixture URLs")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	servicePool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal("identity service database unavailable")
	}
	defer servicePool.Close()
	bootstrapPool, err := pgxpool.New(ctx, bootstrapURL)
	if err != nil {
		t.Fatal("identity bootstrap database unavailable")
	}
	defer bootstrapPool.Close()
	fixturePool, err := pgxpool.New(ctx, fixtureURL)
	if err != nil {
		t.Fatal("identity fixture database unavailable")
	}
	defer fixturePool.Close()

	password := "correct horse battery staple!"
	passwordHash, err := browserauth.HashPassword(password)
	if err != nil {
		t.Fatal("password fixture could not be created")
	}
	_, setupDigest, err := browserauth.NewProof()
	if err != nil {
		t.Fatal("setup proof could not be created")
	}
	const (
		issuer      = "https://identity.test.local"
		userID      = "user-bootstrap"
		email       = "admin@example.com"
		displayName = "Bootstrap Admin"
	)
	receiptID := newTestID(t)
	correlationID := newTestID(t)
	var initialized bool
	if err := bootstrapPool.QueryRow(ctx, `
		SELECT cloud_agents_identity.initialize_realm($1, $2, $3, $4, $5, $6, $7, $8)
	`, issuer, setupDigest[:], userID, email, displayName, passwordHash, receiptID, correlationID).Scan(&initialized); err != nil {
		t.Fatal("identity realm could not be initialized")
	}
	if !initialized {
		t.Fatal("identity realm was already initialized")
	}
	freshReceiptID := newTestID(t)
	freshCorrelationID := newTestID(t)
	var retryInitialized bool
	if err := bootstrapPool.QueryRow(ctx, `
		SELECT cloud_agents_identity.initialize_realm($1, $2, $3, $4, $5, $6, $7, $8)
	`, issuer, setupDigest[:], userID, email, displayName, passwordHash, freshReceiptID, freshCorrelationID).Scan(&retryInitialized); err != nil || retryInitialized {
		t.Fatal("identical identity initialization was not idempotent")
	}
	if err := bootstrapPool.QueryRow(ctx, `
		SELECT cloud_agents_identity.initialize_realm($1, $2, $3, $4, $5, $6, $7, $8)
	`, issuer, setupDigest[:], userID, email, displayName+" changed", passwordHash, freshReceiptID, freshCorrelationID).Scan(&retryInitialized); err == nil {
		t.Fatal("changed identity initialization was accepted")
	}
	prepareTenantDiscovery(t, ctx, bootstrapPool, fixturePool, passwordHash)

	store, err := identity.NewPasswordStore(servicePool, []byte(strings.Repeat("k", 32)))
	if err != nil {
		t.Fatal("identity password store could not be configured")
	}
	clientIP := netip.MustParseAddr("192.0.2.10")
	assertPrepareLockTimeout(t, servicePool, store, email, clientIP, password)
	assertCanceledAfterPrepare(t, databaseURL, email, clientIP, password)
	login, err := store.PasswordLogin(ctx, api.IdentityApplicationAdmin, clientIP, api.PasswordLoginRequest{Email: email, Password: password})
	if err != nil {
		t.Fatal("password login failed")
	}
	if login.Session.Application != api.IdentityApplicationAdmin || login.Session.User.ID != userID || login.Session.CSRFToken == "" || login.SessionHandle == "" {
		t.Fatal("password login returned an incomplete session")
	}
	handleDigest, err := browserauth.ProofDigest(login.SessionHandle)
	if err != nil {
		t.Fatal("session handle is not canonical")
	}
	csrfDigest, err := browserauth.ProofDigest(login.Session.CSRFToken)
	if err != nil || csrfDigest == handleDigest {
		t.Fatal("session CSRF proof is invalid or reuses the handle")
	}
	assertTenantDiscovery(t, ctx, store, fixturePool, login, password)

	if _, err := store.Session(ctx, api.IdentityApplicationUser, handleDigest); !errors.Is(err, browserauth.ErrUnauthorized) {
		t.Fatal("cross-application session was accepted")
	}
	if err := store.Logout(ctx, api.IdentityApplicationAdmin, handleDigest, strings.Repeat("A", 43)); !errors.Is(err, identity.ErrForbidden) {
		t.Fatal("invalid CSRF proof was accepted")
	}
	if _, err := store.Session(ctx, api.IdentityApplicationAdmin, handleDigest); err != nil {
		t.Fatal("invalid CSRF proof changed the session")
	}
	if err := store.Logout(ctx, api.IdentityApplicationAdmin, handleDigest, login.Session.CSRFToken); err != nil {
		t.Fatal("session logout failed")
	}
	if _, err := store.Session(ctx, api.IdentityApplicationAdmin, handleDigest); !errors.Is(err, browserauth.ErrUnauthorized) {
		t.Fatal("revoked session remained usable")
	}

	clientIPLimit := netip.MustParseAddr("192.0.2.11")
	for attempt := 0; attempt < 100; attempt++ {
		unknownEmail := "unknown-" + strconv.Itoa(attempt) + "@example.com"
		_, err := store.PasswordLogin(ctx, api.IdentityApplicationAdmin, clientIPLimit, api.PasswordLoginRequest{Email: unknownEmail, Password: "unknown account password"})
		if !errors.Is(err, browserauth.ErrUnauthorized) {
			t.Fatalf("unknown IP failure attempt %d was not rejected", attempt+1)
		}
	}
	if _, err := store.PasswordLogin(ctx, api.IdentityApplicationAdmin, clientIPLimit, api.PasswordLoginRequest{Email: email, Password: password}); !errors.Is(err, identity.ErrRateLimited) {
		t.Fatal("IP lock did not rate-limit the correct account")
	}
	originalLogin, err := store.PasswordLogin(ctx, api.IdentityApplicationAdmin, clientIP, api.PasswordLoginRequest{Email: email, Password: password})
	if err != nil {
		t.Fatal("IP lock incorrectly locked the account")
	}
	originalDigest, err := browserauth.ProofDigest(originalLogin.SessionHandle)
	if err != nil {
		t.Fatal("post-IP-lock session handle is not canonical")
	}
	if err := store.Logout(ctx, api.IdentityApplicationAdmin, originalDigest, originalLogin.Session.CSRFToken); err != nil {
		t.Fatal("post-IP-lock session logout failed")
	}

	wrongPassword := "definitely the wrong password"
	for attempt := 0; attempt < 10; attempt++ {
		_, err := store.PasswordLogin(ctx, api.IdentityApplicationAdmin, clientIP, api.PasswordLoginRequest{Email: email, Password: wrongPassword})
		if !errors.Is(err, browserauth.ErrUnauthorized) {
			t.Fatalf("wrong password attempt %d was not rejected", attempt+1)
		}
		if strings.Contains(err.Error(), wrongPassword) || strings.Contains(err.Error(), passwordHash) || strings.Contains(err.Error(), login.SessionHandle) {
			t.Fatal("password login error disclosed a secret")
		}
	}
	if _, err := store.PasswordLogin(ctx, api.IdentityApplicationAdmin, clientIP, api.PasswordLoginRequest{Email: email, Password: password}); !errors.Is(err, identity.ErrRateLimited) {
		t.Fatal("locked account accepted a good password")
	}

	var auditRow int
	if err := servicePool.QueryRow(ctx, `SELECT 1 FROM cloud_agents_identity.audit_events LIMIT 1`).Scan(&auditRow); err == nil {
		t.Fatal("identity service role can read audit events")
	}
}

func assertPrepareLockTimeout(t *testing.T, servicePool *pgxpool.Pool, store *identity.PasswordStore, email string, clientIP netip.Addr, password string) {
	t.Helper()
	accountBucket := sha256.Sum256([]byte(email))
	ipBucket := sha256.Sum256([]byte("ip:" + clientIP.Unmap().String()))
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	lockTx, err := servicePool.Begin(ctx)
	if err != nil {
		t.Fatal("identity lock transaction could not start")
	}
	releaseLock := func() {
		if lockTx == nil {
			return
		}
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), time.Second)
		defer cleanupCancel()
		_ = lockTx.Rollback(cleanupCtx)
		lockTx = nil
	}
	defer releaseLock()

	var matchedUser, matchedPassword pgtype.Text
	var locked bool
	if err := lockTx.QueryRow(ctx, `
		SELECT user_id, password_hash, locked
		FROM cloud_agents_identity.prepare_password_login($1, $2, $3)
	`, email, accountBucket[:], ipBucket[:]).Scan(&matchedUser, &matchedPassword, &locked); err != nil {
		t.Fatal("identity lock transaction could not acquire prepare locks")
	}

	started := time.Now()
	result := make(chan error, 1)
	go func() {
		_, err := store.PasswordLogin(context.Background(), api.IdentityApplicationAdmin, clientIP, api.PasswordLoginRequest{Email: email, Password: password})
		result <- err
	}()
	var loginErr error
	select {
	case loginErr = <-result:
	case <-time.After(8 * time.Second):
		releaseLock()
		select {
		case <-result:
		case <-time.After(2 * time.Second):
		}
		t.Fatal("password login lock wait exceeded the bound")
	}
	elapsed := time.Since(started)
	if !errors.Is(loginErr, identity.ErrUnavailable) || elapsed < 4*time.Second || elapsed > 8*time.Second {
		t.Fatal("password login lock wait did not fail at the bounded database phase")
	}
}

func assertCanceledAfterPrepare(t *testing.T, databaseURL, email string, clientIP netip.Addr, password string) {
	t.Helper()
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatal("identity cancellation pool configuration failed")
	}
	requestCtx, cancelRequest := context.WithCancel(context.Background())
	tracer := &cancelAfterPrepareTracer{cancel: cancelRequest}
	config.ConnConfig.Tracer = tracer
	pool, err := pgxpool.NewWithConfig(requestCtx, config)
	if err != nil {
		cancelRequest()
		t.Fatal("identity cancellation pool could not start")
	}
	defer pool.Close()
	store, err := identity.NewPasswordStore(pool, []byte(strings.Repeat("k", 32)))
	if err != nil {
		cancelRequest()
		t.Fatal("identity cancellation store could not be configured")
	}
	login, err := store.PasswordLogin(requestCtx, api.IdentityApplicationAdmin, clientIP, api.PasswordLoginRequest{Email: email, Password: password})
	if !errors.Is(err, context.Canceled) || login.SessionHandle != "" {
		cancelRequest()
		t.Fatal("request cancellation after prepare did not prevent session issuance")
	}
	if !tracer.prepareEnded.Load() || tracer.finishCalls.Load() != 0 {
		t.Fatal("request cancellation after prepare did not stop before finish")
	}
}

type cancelAfterPrepareTracer struct {
	cancel       context.CancelFunc
	prepareEnded atomic.Bool
	finishCalls  atomic.Int32
}

type prepareTraceMarker struct{}

func (tracer *cancelAfterPrepareTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if strings.Contains(data.SQL, "finish_password_login") {
		tracer.finishCalls.Add(1)
	}
	if strings.Contains(data.SQL, "prepare_password_login") {
		return context.WithValue(ctx, prepareTraceMarker{}, true)
	}
	return ctx
}

func (tracer *cancelAfterPrepareTracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	if data.Err == nil {
		if _, ok := ctx.Value(prepareTraceMarker{}).(bool); ok {
			tracer.prepareEnded.Store(true)
			tracer.cancel()
		}
	}
}

func newTestID(t *testing.T) string {
	t.Helper()
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		t.Fatal("test identifier could not be created")
	}
	return hex.EncodeToString(value[:])
}
