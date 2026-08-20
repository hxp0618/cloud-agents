package postgres

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authz"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	defaultCleanupTimeout = 5 * time.Second
	idleTransactionStatus = byte('I')

	bindTenantSQL        = "SELECT pg_catalog.set_config('cloud_agents.tenant_id', $1, true)"
	readTenantSQL        = "SELECT cloud_agents.require_tenant_id()"
	readBoundTenantSQL   = "SELECT pg_catalog.current_setting('cloud_agents.tenant_id', true)"
	readClearedTenantSQL = "SELECT pg_catalog.current_setting('cloud_agents.tenant_id', true)"
	getPlatformTenantSQL = `SELECT
    tenant.tenant_id,
    tenant.tenant_uid,
    tenant.tenant_name,
    tenant.display_name,
    tenant.state,
    tenant.resource_version,
    tenant.created_at,
    tenant.updated_at
FROM cloud_agents.platform_tenants AS tenant
WHERE tenant.tenant_id = cloud_agents.require_tenant_id()
    AND tenant.tenant_uid = tenant.tenant_id`
)

var (
	ErrNilPool                = errors.New("postgres tenant transaction pool is nil")
	ErrNilCallback            = errors.New("postgres tenant transaction callback is nil")
	ErrNilContext             = errors.New("postgres tenant transaction context is nil")
	ErrTenantBindingMismatch  = errors.New("postgres tenant binding readback mismatch")
	ErrTenantCapabilityClosed = errors.New("postgres tenant read capability is closed")
	ErrConnectionNotReusable  = errors.New("postgres tenant connection is not reusable")
)

// PlatformTenant is the typed, tenant-scoped projection exposed by the P1-A2.1
// read capability. It deliberately contains no persistence or pgx handles.
type PlatformTenant struct {
	TenantID        string
	TenantUID       string
	TenantName      string
	DisplayName     string
	State           string
	ResourceVersion int64
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// TenantReadCapability exposes only store-owned, tenant-scoped read methods.
// Implementations become invalid as soon as the callback returns.
type TenantReadCapability interface {
	GetPlatformTenant(context.Context) (PlatformTenant, error)
	Authorize(context.Context, authz.Request) (authz.Decision, error)
}

// TenantReadCallback runs inside one read-only, tenant-bound transaction.
type TenantReadCallback func(context.Context, TenantReadCapability) error

// TenantTransactionRunner owns the physical-connection and transaction
// lifecycle for tenant-scoped reads.
type TenantTransactionRunner struct {
	pool           physicalPool
	cleanupTimeout time.Duration
	clock          func() time.Time
}

// NewTenantTransactionRunner binds the runtime helper to a pgxpool. Each call
// acquires one physical connection from this pool.
func NewTenantTransactionRunner(pool *pgxpool.Pool) (*TenantTransactionRunner, error) {
	if pool == nil {
		return nil, ErrNilPool
	}

	return newTenantTransactionRunner(newPGXPool(pool), defaultCleanupTimeout), nil
}

func newTenantTransactionRunner(pool physicalPool, cleanupTimeout time.Duration) *TenantTransactionRunner {
	if cleanupTimeout <= 0 {
		cleanupTimeout = defaultCleanupTimeout
	}

	return &TenantTransactionRunner{
		pool:           pool,
		cleanupTimeout: cleanupTimeout,
		clock:          time.Now,
	}
}

// WithTenantRead executes callback once inside a tenant-bound read-only
// transaction. It never retries callback and never exposes raw SQL or pgx
// transaction capabilities to callback.
func (runner *TenantTransactionRunner) WithTenantRead(
	ctx context.Context,
	tenantID string,
	callback TenantReadCallback,
) error {
	return runner.withTenantReadBinder(ctx, tenantID, callback, bindTenant)
}

type tenantBinder func(context.Context, tenantTransaction, string) error

func (runner *TenantTransactionRunner) withTenantReadBinder(
	ctx context.Context,
	tenantID string,
	callback TenantReadCallback,
	binder tenantBinder,
) error {
	if ctx == nil {
		return ErrNilContext
	}
	if callback == nil || binder == nil {
		return ErrNilCallback
	}

	connection, err := runner.pool.acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire tenant physical connection: %w", err)
	}

	settled := false
	defer func() {
		if settled {
			return
		}

		cleanupContext, cancel := runner.cleanupContext()
		defer cancel()
		_ = connection.hijackAndClose(cleanupContext)
	}()

	transaction, err := connection.beginTx(ctx, pgx.TxOptions{
		IsoLevel:       pgx.ReadCommitted,
		AccessMode:     pgx.ReadOnly,
		DeferrableMode: pgx.NotDeferrable,
	})
	if err != nil {
		runner.discard(connection, &settled)
		return fmt.Errorf("begin tenant read transaction: %w", err)
	}

	if err := binder(ctx, transaction, tenantID); err != nil {
		runner.rollbackAndSettle(connection, transaction, &settled)
		return err
	}

	handle := &tenantReadHandle{
		active:      true,
		transaction: transaction,
		tenantID:    tenantID,
		clock:       runner.clock,
	}

	callbackErr, panicValue, panicked := invokeTenantCallback(ctx, callback, handle)
	handle.invalidate()

	if panicked {
		runner.rollbackAndSettle(connection, transaction, &settled)
		panic(panicValue)
	}

	if callbackErr != nil {
		runner.rollbackAndSettle(connection, transaction, &settled)
		return callbackErr
	}

	if err := transaction.commit(ctx); err != nil {
		runner.discard(connection, &settled)
		return fmt.Errorf("commit tenant read transaction: %w", err)
	}

	if err := runner.releaseIfReusable(connection, &settled); err != nil {
		return err
	}

	return nil
}

// bindTenantSetting is restricted to typed SECURITY DEFINER consumers whose
// authority group deliberately cannot execute require_tenant_id directly. It
// proves the transaction-local GUC twice; the typed database function remains
// responsible for its own require_tenant_id check before reading or writing.
func bindTenantSetting(ctx context.Context, transaction tenantTransaction, tenantID string) error {
	var configuredTenant string
	if err := transaction.queryRow(ctx, bindTenantSQL, tenantID).Scan(&configuredTenant); err != nil {
		return fmt.Errorf("bind restricted tenant transaction context: %w", err)
	}
	if configuredTenant != tenantID {
		return fmt.Errorf("%w: set_config returned a different restricted value", ErrTenantBindingMismatch)
	}

	var readbackTenant string
	if err := transaction.queryRow(ctx, readBoundTenantSQL).Scan(&readbackTenant); err != nil {
		return fmt.Errorf("read restricted tenant transaction context: %w", err)
	}
	if readbackTenant != tenantID {
		return fmt.Errorf("%w: current_setting returned a different restricted value", ErrTenantBindingMismatch)
	}
	return nil
}

func bindTenant(ctx context.Context, transaction tenantTransaction, tenantID string) error {
	var configuredTenant string
	if err := transaction.queryRow(ctx, bindTenantSQL, tenantID).Scan(&configuredTenant); err != nil {
		return fmt.Errorf("bind tenant transaction context: %w", err)
	}
	if configuredTenant != tenantID {
		return fmt.Errorf("%w: set_config returned a different value", ErrTenantBindingMismatch)
	}

	var validatedTenant string
	if err := transaction.queryRow(ctx, readTenantSQL).Scan(&validatedTenant); err != nil {
		return fmt.Errorf("validate tenant transaction context: %w", err)
	}
	if validatedTenant != tenantID {
		return fmt.Errorf("%w: require_tenant_id returned a different value", ErrTenantBindingMismatch)
	}

	return nil
}

func invokeTenantCallback(
	ctx context.Context,
	callback TenantReadCallback,
	handle *tenantReadHandle,
) (callbackErr error, panicValue any, panicked bool) {
	completed := false
	func() {
		defer func() {
			if !completed {
				panicValue = recover()
				panicked = true
			}
		}()

		callbackErr = callback(ctx, handle)
		completed = true
	}()

	return callbackErr, panicValue, panicked
}

func (runner *TenantTransactionRunner) rollbackAndSettle(
	connection physicalConnection,
	transaction tenantTransaction,
	settled *bool,
) {
	cleanupContext, cancel := runner.cleanupContext()
	rollbackErr := transaction.rollback(cleanupContext)
	cancel()
	if rollbackErr != nil {
		runner.discard(connection, settled)
		return
	}

	_ = runner.releaseIfReusable(connection, settled)
}

func (runner *TenantTransactionRunner) releaseIfReusable(
	connection physicalConnection,
	settled *bool,
) error {
	if connection.txStatus() != idleTransactionStatus {
		runner.discard(connection, settled)
		return fmt.Errorf("%w: transaction status is not idle", ErrConnectionNotReusable)
	}

	cleanupContext, cancel := runner.cleanupContext()
	defer cancel()

	var tenantSetting *string
	if err := connection.queryRow(cleanupContext, readClearedTenantSQL).Scan(&tenantSetting); err != nil {
		runner.discard(connection, settled)
		return fmt.Errorf("%w: read cleared tenant setting: %v", ErrConnectionNotReusable, err)
	}

	if connection.txStatus() != idleTransactionStatus {
		runner.discard(connection, settled)
		return fmt.Errorf("%w: post-check transaction status is not idle", ErrConnectionNotReusable)
	}
	if tenantSetting != nil && *tenantSetting != "" {
		runner.discard(connection, settled)
		return fmt.Errorf("%w: tenant setting remained non-empty", ErrConnectionNotReusable)
	}

	connection.release()
	*settled = true
	return nil
}

func (runner *TenantTransactionRunner) discard(connection physicalConnection, settled *bool) {
	cleanupContext, cancel := runner.cleanupContext()
	defer cancel()
	_ = connection.hijackAndClose(cleanupContext)
	*settled = true
}

func (runner *TenantTransactionRunner) cleanupContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), runner.cleanupTimeout)
}

type tenantReadHandle struct {
	mutex       sync.Mutex
	active      bool
	transaction tenantTransaction
	tenantID    string
	clock       func() time.Time
}

func (handle *tenantReadHandle) GetPlatformTenant(ctx context.Context) (PlatformTenant, error) {
	handle.mutex.Lock()
	defer handle.mutex.Unlock()

	if !handle.active || handle.transaction == nil {
		return PlatformTenant{}, ErrTenantCapabilityClosed
	}

	var tenant PlatformTenant
	err := handle.transaction.queryRow(ctx, getPlatformTenantSQL).Scan(
		&tenant.TenantID,
		&tenant.TenantUID,
		&tenant.TenantName,
		&tenant.DisplayName,
		&tenant.State,
		&tenant.ResourceVersion,
		&tenant.CreatedAt,
		&tenant.UpdatedAt,
	)
	if err != nil {
		return PlatformTenant{}, err
	}

	return tenant, nil
}

func (handle *tenantReadHandle) invalidate() {
	handle.mutex.Lock()
	defer handle.mutex.Unlock()
	handle.active = false
	handle.transaction = nil
	handle.tenantID = ""
	handle.clock = nil
}

type rowScanner interface {
	Scan(...any) error
}

type tenantTransaction interface {
	queryRow(context.Context, string, ...any) rowScanner
	commit(context.Context) error
	rollback(context.Context) error
}

type physicalConnection interface {
	beginTx(context.Context, pgx.TxOptions) (tenantTransaction, error)
	queryRow(context.Context, string, ...any) rowScanner
	txStatus() byte
	release()
	hijackAndClose(context.Context) error
}

type physicalPool interface {
	acquire(context.Context) (physicalConnection, error)
}
