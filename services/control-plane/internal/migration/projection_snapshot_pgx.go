package migration

import (
	"context"
	"errors"
	"reflect"
	"strconv"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type fixedQueryProjectionSnapshot struct {
	queryer  Queryer
	metadata SnapshotMetadata
	started  time.Time

	mu         sync.Mutex
	closed     bool
	queryCount uint32
	rowCount   uint64
	totalBytes uint64
}

func (snapshot *fixedQueryProjectionSnapshot) projectionSnapshot() {}

func (snapshot *fixedQueryProjectionSnapshot) Metadata() SnapshotMetadata {
	metadata := snapshot.metadata
	if metadata.MigrationID != nil {
		value := *metadata.MigrationID
		metadata.MigrationID = &value
	}
	if metadata.StatementIndex != nil {
		value := *metadata.StatementIndex
		metadata.StatementIndex = &value
	}
	return metadata
}

func (snapshot *fixedQueryProjectionSnapshot) projectionStats() projectionQueryStats {
	snapshot.mu.Lock()
	defer snapshot.mu.Unlock()
	return projectionQueryStats{QueryCount: snapshot.queryCount, RowCount: snapshot.rowCount, TotalBytes: snapshot.totalBytes}
}

func (snapshot *fixedQueryProjectionSnapshot) queryProjection(ctx context.Context, id projectionQueryID, args ...any) (Rows, error) {
	query, ok := projectionFixedQuery(id)
	if !ok || !projectionSnapshotQueryAllowed(id) {
		return nil, projectionFailure(CodeProjectionCatalogQueryFailed, "query", "query_id", snapshot.metadata.PostgresMajor, false, "projection query identifier is not available")
	}
	queryCtx, cancel, err := snapshot.queryContext(ctx)
	if err != nil {
		return nil, err
	}
	snapshot.mu.Lock()
	if snapshot.queryCount >= projectionMaxQueriesPerProjection {
		snapshot.mu.Unlock()
		cancel()
		return nil, projectionFailure(CodeProjectionLimitExceeded, "query", "query_count", snapshot.metadata.PostgresMajor, false, "projection query count limit was exceeded")
	}
	snapshot.queryCount++
	snapshot.mu.Unlock()
	rows, err := snapshot.queryer.Query(queryCtx, query, args...)
	if err != nil {
		cancel()
		return nil, snapshot.queryFailure(ctx, "query")
	}
	return &boundedProjectionRows{Rows: rows, snapshot: snapshot, cancel: cancel}, nil
}

func (snapshot *fixedQueryProjectionSnapshot) queryProjectionRow(ctx context.Context, id projectionQueryID, args ...any) Row {
	query, ok := projectionFixedQuery(id)
	if !ok || !projectionSnapshotQueryAllowed(id) {
		return projectionErrorRow{err: projectionFailure(CodeProjectionCatalogQueryFailed, "query", "query_id", snapshot.metadata.PostgresMajor, false, "projection query identifier is not available")}
	}
	queryCtx, cancel, err := snapshot.queryContext(ctx)
	if err != nil {
		return projectionErrorRow{err: err}
	}
	snapshot.mu.Lock()
	if snapshot.queryCount >= projectionMaxQueriesPerProjection {
		snapshot.mu.Unlock()
		cancel()
		return projectionErrorRow{err: projectionFailure(CodeProjectionLimitExceeded, "query", "query_count", snapshot.metadata.PostgresMajor, false, "projection query count limit was exceeded")}
	}
	snapshot.queryCount++
	snapshot.mu.Unlock()
	return &boundedProjectionRow{Row: snapshot.queryer.QueryRow(queryCtx, query, args...), snapshot: snapshot, cancel: cancel}
}

func projectionSnapshotQueryAllowed(id projectionQueryID) bool {
	switch id {
	case projectionQuerySnapshotMetadata, projectionQuerySnapshotConfigure, projectionQuerySnapshotReset, projectionQuerySnapshotSanitation,
		projectionQuerySnapshotSetMigrationRole, projectionQuerySnapshotRoleReadback:
		return false
	default:
		return true
	}
}

func (snapshot *fixedQueryProjectionSnapshot) queryContext(ctx context.Context) (context.Context, context.CancelFunc, error) {
	snapshot.mu.Lock()
	closed := snapshot.closed
	snapshot.mu.Unlock()
	if closed {
		return nil, nil, projectionFailure(CodeProjectionSnapshotInvalid, "query", "snapshot", snapshot.metadata.PostgresMajor, false, "projection snapshot is closed")
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, projectionFailure(CodeProjectionCatalogQueryFailed, "query", "context", snapshot.metadata.PostgresMajor, false, "projection query was canceled")
	}
	lifetimeDeadline := snapshot.started.Add(projectionSnapshotLifetime)
	if time.Now().After(lifetimeDeadline) {
		return nil, nil, projectionFailure(CodeProjectionSnapshotInvalid, "query", "snapshot_lifetime", snapshot.metadata.PostgresMajor, false, "projection snapshot lifetime was exceeded")
	}
	deadline := time.Now().Add(projectionQueryTimeout)
	if lifetimeDeadline.Before(deadline) {
		deadline = lifetimeDeadline
	}
	if callerDeadline, ok := ctx.Deadline(); ok && callerDeadline.Before(deadline) {
		deadline = callerDeadline
	}
	queryCtx, cancel := context.WithDeadline(ctx, deadline)
	return queryCtx, cancel, nil
}

func (snapshot *fixedQueryProjectionSnapshot) queryFailure(ctx context.Context, path string) error {
	message := "projection catalog query failed"
	if errors.Is(ctx.Err(), context.Canceled) {
		message = "projection query was canceled"
	} else if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		message = "projection query deadline was exceeded"
	}
	return projectionFailure(CodeProjectionCatalogQueryFailed, "query", path, snapshot.metadata.PostgresMajor, false, message)
}

func (snapshot *fixedQueryProjectionSnapshot) addRow(rowBytes uint64, queryRows *uint64) error {
	(*queryRows)++
	if *queryRows > projectionMaxQueryRows {
		return projectionFailure(CodeProjectionLimitExceeded, "query", "rows", snapshot.metadata.PostgresMajor, false, "projection query row limit was exceeded")
	}
	if rowBytes > projectionMaxRowBytes {
		return projectionFailure(CodeProjectionLimitExceeded, "query", "row_bytes", snapshot.metadata.PostgresMajor, false, "projection row byte limit was exceeded")
	}
	snapshot.mu.Lock()
	defer snapshot.mu.Unlock()
	if snapshot.totalBytes > projectionMaxTotalResultBytes-rowBytes {
		return projectionFailure(CodeProjectionLimitExceeded, "query", "total_bytes", snapshot.metadata.PostgresMajor, false, "projection total result byte limit was exceeded")
	}
	snapshot.rowCount++
	snapshot.totalBytes += rowBytes
	return nil
}

type boundedProjectionRows struct {
	Rows
	snapshot  *fixedQueryProjectionSnapshot
	cancel    context.CancelFunc
	queryRows uint64
	err       error
	closed    bool
}

func (rows *boundedProjectionRows) Next() bool {
	if rows.err != nil || rows.closed {
		return false
	}
	if rows.queryRows >= projectionMaxQueryRows {
		if rows.Rows.Next() {
			rows.err = projectionFailure(CodeProjectionLimitExceeded, "query", "rows", rows.snapshot.metadata.PostgresMajor, false, "projection query row limit was exceeded")
			rows.Close()
			return false
		}
		return false
	}
	return rows.Rows.Next()
}

func (rows *boundedProjectionRows) Scan(targets ...any) error {
	if rows.err != nil {
		return rows.err
	}
	if err := rows.Rows.Scan(targets...); err != nil {
		rows.err = projectionFailure(CodeProjectionCatalogQueryFailed, "query", "scan", rows.snapshot.metadata.PostgresMajor, false, "projection row scan failed")
		return rows.err
	}
	rowBytes, ok := projectionScannedCanonicalSize(targets)
	if !ok {
		rows.err = projectionFailure(CodeProjectionCatalogQueryFailed, "query", "scan_type", rows.snapshot.metadata.PostgresMajor, false, "projection row scan type is unsupported")
		return rows.err
	}
	if err := rows.snapshot.addRow(rowBytes, &rows.queryRows); err != nil {
		rows.err = err
		return err
	}
	return nil
}

func (rows *boundedProjectionRows) Err() error {
	if rows.err != nil {
		return rows.err
	}
	if err := rows.Rows.Err(); err != nil {
		return projectionFailure(CodeProjectionCatalogQueryFailed, "query", "iteration", rows.snapshot.metadata.PostgresMajor, false, "projection row iteration failed")
	}
	return nil
}

func (rows *boundedProjectionRows) Close() {
	if rows.closed {
		return
	}
	rows.closed = true
	rows.Rows.Close()
	rows.cancel()
}

type boundedProjectionRow struct {
	Row
	snapshot *fixedQueryProjectionSnapshot
	cancel   context.CancelFunc
	scanned  bool
}

func (row *boundedProjectionRow) Scan(targets ...any) error {
	if row.scanned {
		return projectionFailure(CodeProjectionCatalogQueryFailed, "query", "row_reuse", row.snapshot.metadata.PostgresMajor, false, "projection row cannot be scanned more than once")
	}
	row.scanned = true
	defer row.cancel()
	if err := row.Row.Scan(targets...); err != nil {
		return projectionFailure(CodeProjectionCatalogQueryFailed, "query", "scan", row.snapshot.metadata.PostgresMajor, false, "projection row scan failed")
	}
	rowBytes, ok := projectionScannedCanonicalSize(targets)
	if !ok {
		return projectionFailure(CodeProjectionCatalogQueryFailed, "query", "scan_type", row.snapshot.metadata.PostgresMajor, false, "projection row scan type is unsupported")
	}
	var queryRows uint64
	return row.snapshot.addRow(rowBytes, &queryRows)
}

type projectionErrorRow struct{ err error }

func (row projectionErrorRow) Scan(...any) error { return row.err }

func projectionScannedCanonicalSize(targets []any) (uint64, bool) {
	values := make([]any, len(targets))
	for _, target := range targets {
		value := reflect.ValueOf(target)
		if value.Kind() != reflect.Pointer || value.IsNil() || !value.Elem().CanInterface() || !projectionValueUTF8Valid(value.Elem(), 0) {
			return 0, false
		}
	}
	for index, target := range targets {
		values[index] = reflect.ValueOf(target).Elem().Interface()
	}
	canonical, err := canonicalTyped(values)
	if err != nil {
		return 0, false
	}
	return uint64(len(canonical)), true
}

func projectionValueUTF8Valid(value reflect.Value, depth int) bool {
	if depth > 8 || !value.IsValid() {
		return depth <= 8
	}
	for value.Kind() == reflect.Interface || value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return true
		}
		value = value.Elem()
		depth++
		if depth > 8 {
			return false
		}
	}
	switch value.Kind() {
	case reflect.String:
		return utf8.ValidString(value.String())
	case reflect.Slice, reflect.Array:
		for index := 0; index < value.Len(); index++ {
			if !projectionValueUTF8Valid(value.Index(index), depth+1) {
				return false
			}
		}
		return true
	case reflect.Bool, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Float32, reflect.Float64:
		return true
	case reflect.Struct:
		for index := 0; index < value.NumField(); index++ {
			if !projectionValueUTF8Valid(value.Field(index), depth+1) {
				return false
			}
		}
		return true
	case reflect.Map:
		iterator := value.MapRange()
		for iterator.Next() {
			if !projectionValueUTF8Valid(iterator.Key(), depth+1) || !projectionValueUTF8Valid(iterator.Value(), depth+1) {
				return false
			}
		}
		return true
	default:
		return false
	}
}

type ownedIdleProjectionSnapshot struct {
	*fixedQueryProjectionSnapshot
	connection idleSnapshotConnection
	once       sync.Once
	closeErr   error
}

func (*ownedIdleProjectionSnapshot) idleProjectionSnapshot() {}

func (snapshot *ownedIdleProjectionSnapshot) RollbackAndRelease(ctx context.Context) error {
	snapshot.once.Do(func() {
		snapshot.mu.Lock()
		snapshot.closed = true
		snapshot.mu.Unlock()
		if status := snapshot.connection.txStatus(); status != 'T' && status != 'E' {
			snapshot.connection.hijackAndClose(projectionCleanupContext(ctx))
			snapshot.closeErr = projectionFailure(CodeProjectionSnapshotInvalid, "snapshot-close", "tx_status", snapshot.metadata.PostgresMajor, false, "idle projection transaction status is unknown before rollback")
			return
		}
		rollbackCtx, cancel := projectionRollbackContext(ctx)
		defer cancel()
		if err := snapshot.connection.rollback(rollbackCtx); err != nil {
			snapshot.connection.hijackAndClose(projectionCleanupContext(ctx))
			snapshot.closeErr = projectionFailure(CodeProjectionSnapshotInvalid, "snapshot-close", "rollback", snapshot.metadata.PostgresMajor, false, "idle projection snapshot rollback failed")
			return
		}
		if snapshot.connection.txStatus() != 'I' {
			snapshot.connection.hijackAndClose(projectionCleanupContext(ctx))
			snapshot.closeErr = projectionFailure(CodeProjectionSnapshotInvalid, "snapshot-close", "tx_status", snapshot.metadata.PostgresMajor, false, "idle projection connection did not return to idle")
			return
		}
		if err := sanitizeIdleSnapshotConnection(rollbackCtx, snapshot.connection, "snapshot-close", snapshot.metadata.PostgresMajor); err != nil {
			snapshot.connection.hijackAndClose(projectionCleanupContext(ctx))
			snapshot.closeErr = err
			return
		}
		snapshot.connection.release()
	})
	return snapshot.closeErr
}

// BeginIdleReadSnapshot acquires one pgxpool connection, proves and sanitizes
// its idle session, opens the exact RR/RO/not-deferrable transaction, and reads
// metadata back before exposing the sealed fixed-query snapshot.
func BeginIdleReadSnapshot(ctx context.Context, pool *pgxpool.Pool, phase AuthorityPhase) (IdleProjectionSnapshot, error) {
	if pool == nil {
		return nil, projectionFailure(CodeProjectionSnapshotInvalid, "snapshot-begin", "pool", 0, false, "projection pool is unavailable")
	}
	return beginIdleReadSnapshot(ctx, pgxIdleSnapshotPool{pool: pool}, phase)
}

type runnerSessionProjectionSnapshotProvider interface {
	beginRunnerSessionProjectionSnapshot(context.Context, AuthorityPhase) (RunnerSessionProjectionSnapshot, error)
}

// BeginRunnerSessionProjectionSnapshot borrows the exact DatabaseSession
// already owned by Runner. The private provider method prevents callers from
// substituting a pool-backed or foreign connection implementation.
func BeginRunnerSessionProjectionSnapshot(ctx context.Context, session DatabaseSession, phase AuthorityPhase) (RunnerSessionProjectionSnapshot, error) {
	provider, ok := session.(runnerSessionProjectionSnapshotProvider)
	if !ok {
		return nil, projectionFailure(CodeProjectionSnapshotInvalid, "runner-snapshot-begin", "session", 0, false, "dedicated runner session does not support projection snapshots")
	}
	return provider.beginRunnerSessionProjectionSnapshot(ctx, phase)
}

type runnerSessionSnapshotConnection interface {
	Queryer
	txStatus() byte
	prepare(context.Context, AuthorityPhase) error
	begin(context.Context) error
	rollback(context.Context) error
	validateReturn(context.Context, AuthorityPhase) error
	invalidate(context.Context)
	returnToRunner()
}

type ownedRunnerSessionProjectionSnapshot struct {
	*fixedQueryProjectionSnapshot
	connection runnerSessionSnapshotConnection
	phase      AuthorityPhase
	once       sync.Once
	closeErr   error
}

func (*ownedRunnerSessionProjectionSnapshot) runnerSessionProjectionSnapshot() {}

func (snapshot *ownedRunnerSessionProjectionSnapshot) RollbackAndReturnToRunner(ctx context.Context) error {
	snapshot.once.Do(func() {
		defer snapshot.connection.returnToRunner()
		snapshot.mu.Lock()
		snapshot.closed = true
		snapshot.mu.Unlock()
		if status := snapshot.connection.txStatus(); status != 'T' && status != 'E' {
			snapshot.connection.invalidate(projectionCleanupContext(ctx))
			snapshot.closeErr = projectionFailure(CodeProjectionSnapshotInvalid, "runner-snapshot-close", "tx_status", snapshot.metadata.PostgresMajor, false, "runner projection transaction status is unknown before rollback")
			return
		}
		rollbackCtx, cancel := projectionRollbackContext(ctx)
		defer cancel()
		if err := snapshot.connection.rollback(rollbackCtx); err != nil {
			snapshot.connection.invalidate(projectionCleanupContext(ctx))
			snapshot.closeErr = projectionFailure(CodeProjectionSnapshotInvalid, "runner-snapshot-close", "rollback", snapshot.metadata.PostgresMajor, false, "runner projection snapshot rollback failed")
			return
		}
		if snapshot.connection.txStatus() != 'I' {
			snapshot.connection.invalidate(projectionCleanupContext(ctx))
			snapshot.closeErr = projectionFailure(CodeProjectionSnapshotInvalid, "runner-snapshot-close", "tx_status", snapshot.metadata.PostgresMajor, false, "runner session did not return to idle")
			return
		}
		if err := snapshot.connection.validateReturn(rollbackCtx, snapshot.phase); err != nil {
			snapshot.connection.invalidate(projectionCleanupContext(ctx))
			snapshot.closeErr = mapRunnerSnapshotLifecycleError(err, "runner-snapshot-close", "return", snapshot.metadata.PostgresMajor, "runner session return validation failed")
		}
	})
	return snapshot.closeErr
}

func beginRunnerSessionProjectionSnapshot(ctx context.Context, connection runnerSessionSnapshotConnection, phase AuthorityPhase) (RunnerSessionProjectionSnapshot, error) {
	if connection == nil || phase != AuthorityPhaseConnectedSession && phase != AuthorityPhaseMigrationRole {
		if connection != nil {
			connection.returnToRunner()
		}
		return nil, projectionFailure(CodeProjectionMetadataMismatch, "runner-snapshot-begin", "phase", 0, false, "runner projection phase is invalid")
	}
	failClosed := func(err error) (RunnerSessionProjectionSnapshot, error) {
		status := connection.txStatus()
		if status == 'T' || status == 'E' {
			rollbackCtx, cancel := projectionRollbackContext(ctx)
			_ = connection.rollback(rollbackCtx)
			cancel()
		}
		connection.invalidate(projectionCleanupContext(ctx))
		connection.returnToRunner()
		return nil, err
	}
	if err := connection.prepare(ctx, phase); err != nil {
		return failClosed(mapRunnerSnapshotLifecycleError(err, "runner-snapshot-begin", "prepare", 0, "runner session projection preparation failed"))
	}
	if connection.txStatus() != 'I' {
		return failClosed(projectionFailure(CodeProjectionMetadataMismatch, "runner-snapshot-begin", "tx_status", 0, false, "runner session is not idle before projection"))
	}
	started := time.Now()
	if err := connection.begin(ctx); err != nil {
		return failClosed(projectionFailure(CodeProjectionSnapshotInvalid, "runner-snapshot-begin", "begin", 0, false, "runner projection transaction could not begin"))
	}
	if connection.txStatus() != 'T' {
		return failClosed(projectionFailure(CodeProjectionSnapshotInvalid, "runner-snapshot-begin", "tx_status", 0, false, "runner projection transaction status is invalid"))
	}
	if err := configureOwnedIdleSnapshot(ctx, connection); err != nil {
		return failClosed(err)
	}
	metadata, err := readProjectionSnapshotMetadata(ctx, connection, IdleReadSnapshot, OwnedIdleSnapshot, phase, nil, nil)
	if err != nil {
		return failClosed(err)
	}
	if connection.txStatus() != 'T' {
		return failClosed(projectionFailure(CodeProjectionSnapshotInvalid, "runner-snapshot-begin", "tx_status", metadata.PostgresMajor, false, "runner projection transaction status changed during validation"))
	}
	return &ownedRunnerSessionProjectionSnapshot{
		fixedQueryProjectionSnapshot: &fixedQueryProjectionSnapshot{queryer: connection, metadata: metadata, started: started},
		connection:                   connection,
		phase:                        phase,
	}, nil
}

func mapRunnerSnapshotLifecycleError(err error, op, path string, major uint16, message string) error {
	var stable *Error
	if errors.As(err, &stable) {
		return fail(stable.Code, op, message, nil)
	}
	return projectionFailure(CodeProjectionSnapshotInvalid, op, path, major, false, message)
}

func beginIdleReadSnapshot(ctx context.Context, pool idleSnapshotPool, phase AuthorityPhase) (IdleProjectionSnapshot, error) {
	if phase != AuthorityPhaseConnectedSession && phase != AuthorityPhaseMigrationRole {
		return nil, projectionFailure(CodeProjectionMetadataMismatch, "snapshot-begin", "authority_phase", 0, false, "owned idle snapshot authority phase is invalid")
	}
	connection, err := pool.acquire(ctx)
	if err != nil {
		return nil, projectionFailure(CodeProjectionCatalogQueryFailed, "snapshot-begin", "acquire", 0, false, "projection connection acquisition failed")
	}
	if connection.txStatus() != 'I' {
		connection.hijackAndClose(projectionCleanupContext(ctx))
		return nil, projectionFailure(CodeProjectionSnapshotInvalid, "snapshot-begin", "tx_status", 0, false, "projection connection was not idle")
	}
	if err := sanitizeIdleSnapshotConnection(ctx, connection, "snapshot-begin", 0); err != nil {
		connection.hijackAndClose(projectionCleanupContext(ctx))
		return nil, err
	}
	if phase == AuthorityPhaseMigrationRole {
		roleCtx, cancel := projectionRollbackContext(ctx)
		err := connection.setMigrationRole(roleCtx)
		cancel()
		if err != nil || connection.txStatus() != 'I' {
			connection.hijackAndClose(projectionCleanupContext(ctx))
			return nil, projectionFailure(CodeProjectionSnapshotInvalid, "snapshot-begin", "migration_role", 0, false, "fixed migration role could not be established")
		}
	}
	started := time.Now()
	if err := connection.begin(ctx); err != nil {
		cleanupIdleSnapshot(connection)
		return nil, projectionFailure(CodeProjectionSnapshotInvalid, "snapshot-begin", "begin", 0, false, "idle projection transaction could not begin")
	}
	if connection.txStatus() != 'T' {
		connection.hijackAndClose(projectionCleanupContext(ctx))
		return nil, projectionFailure(CodeProjectionSnapshotInvalid, "snapshot-begin", "tx_status", 0, false, "idle projection transaction status is invalid")
	}
	if err := configureOwnedIdleSnapshot(ctx, connection); err != nil {
		cleanupIdleSnapshot(connection)
		return nil, err
	}
	metadata, err := readProjectionSnapshotMetadata(ctx, connection, IdleReadSnapshot, OwnedIdleSnapshot, phase, nil, nil)
	if err != nil {
		cleanupIdleSnapshot(connection)
		return nil, err
	}
	if connection.txStatus() != 'T' {
		connection.hijackAndClose(projectionCleanupContext(ctx))
		return nil, projectionFailure(CodeProjectionSnapshotInvalid, "snapshot-begin", "tx_status", metadata.PostgresMajor, false, "idle projection transaction status changed during snapshot validation")
	}
	snapshot := &ownedIdleProjectionSnapshot{
		fixedQueryProjectionSnapshot: &fixedQueryProjectionSnapshot{queryer: connection, metadata: metadata, started: started},
		connection:                   connection,
	}
	return snapshot, nil
}

func configureOwnedIdleSnapshot(ctx context.Context, queryer Queryer) error {
	query, ok := projectionFixedQuery(projectionQuerySnapshotConfigure)
	if !ok {
		return projectionFailure(CodeProjectionSnapshotInvalid, "snapshot-configure", "query_id", 0, false, "snapshot transaction-local settings query is unavailable")
	}
	configureCtx, cancel := context.WithTimeout(ctx, projectionQueryTimeout)
	defer cancel()
	var statementTimeout, lockTimeout, idleTimeout string
	if err := queryer.QueryRow(configureCtx, query).Scan(&statementTimeout, &lockTimeout, &idleTimeout); err != nil {
		return projectionFailure(CodeProjectionSnapshotInvalid, "snapshot-configure", "set_config", 0, false, "snapshot transaction-local settings could not be configured")
	}
	return nil
}

// BorrowMigrationProjectionSnapshot reuses the existing migration transaction.
// The returned interface cannot begin, commit, rollback, set role, savepoint,
// execute SQL, release, or close the transaction.
func BorrowMigrationProjectionSnapshot(ctx context.Context, transaction MigrationTransaction, migrationID string, statementIndex *uint32) (ProjectionSnapshot, error) {
	if transaction == nil || !migrationIDPattern.MatchString(migrationID) {
		return nil, projectionFailure(CodeProjectionMetadataMismatch, "snapshot-borrow", "migration_id", 0, false, "migration snapshot identity is invalid")
	}
	started := time.Now()
	status, ok := migrationProjectionTxStatus(transaction)
	if !ok || status != 'T' {
		return nil, projectionFailure(CodeProjectionSnapshotInvalid, "snapshot-borrow", "tx_status", 0, false, "migration transaction status is invalid")
	}
	index := cloneUint32Pointer(statementIndex)
	metadata, err := readProjectionSnapshotMetadata(ctx, transaction, MigrationSnapshot, BorrowedMigrationSnapshot, AuthorityPhaseMigrationTransaction, &migrationID, index)
	if err != nil {
		return nil, err
	}
	status, ok = migrationProjectionTxStatus(transaction)
	if !ok || status != 'T' {
		return nil, projectionFailure(CodeProjectionSnapshotInvalid, "snapshot-borrow", "tx_status", metadata.PostgresMajor, false, "migration transaction status changed during snapshot validation")
	}
	return &fixedQueryProjectionSnapshot{queryer: transaction, metadata: metadata, started: started}, nil
}

func readProjectionSnapshotMetadata(ctx context.Context, queryer Queryer, mode SnapshotMode, ownership SnapshotOwnership, phase AuthorityPhase, migrationID *string, statementIndex *uint32) (SnapshotMetadata, error) {
	query, ok := projectionFixedQuery(projectionQuerySnapshotMetadata)
	if !ok {
		return SnapshotMetadata{}, projectionFailure(CodeProjectionCatalogQueryFailed, "snapshot-metadata", "query_id", 0, false, "snapshot metadata query is unavailable")
	}
	readCtx, cancel := context.WithTimeout(ctx, projectionQueryTimeout)
	defer cancel()
	var versionText, databaseName, sessionUser, currentUser, isolation, readOnly, deferrable string
	var statementTimeoutMS, lockTimeoutMS, idleTimeoutMS int64
	if err := queryer.QueryRow(readCtx, query).Scan(
		&versionText, &databaseName, &sessionUser, &currentUser, &isolation, &readOnly, &deferrable,
		&statementTimeoutMS, &lockTimeoutMS, &idleTimeoutMS,
	); err != nil {
		return SnapshotMetadata{}, projectionFailure(CodeProjectionCatalogQueryFailed, "snapshot-metadata", "readback", 0, false, "snapshot metadata readback failed")
	}
	version, err := strconv.ParseUint(versionText, 10, 32)
	if err != nil || version < 10000 {
		return SnapshotMetadata{}, projectionFailure(CodeProjectionSnapshotInvalid, "snapshot-metadata", "server_version_num", 0, false, "server version metadata is invalid")
	}
	major := uint16(version / 10000)
	if !utf8.ValidString(databaseName) || !utf8.ValidString(sessionUser) || !utf8.ValidString(currentUser) {
		return SnapshotMetadata{}, projectionFailure(CodeProjectionCatalogQueryFailed, "snapshot-metadata", "utf8", major, false, "snapshot metadata text is not valid UTF-8")
	}
	if statementTimeoutMS != projectionQueryTimeout.Milliseconds() || lockTimeoutMS != projectionLockTimeout.Milliseconds() || idleTimeoutMS != projectionIdleInTransactionTimeout.Milliseconds() {
		return SnapshotMetadata{}, projectionFailure(CodeProjectionSnapshotInvalid, "snapshot-metadata", "transaction_local_settings", major, false, "snapshot transaction-local settings differ from the fixed projection profile")
	}
	metadata := SnapshotMetadata{
		Mode: mode, Ownership: ownership, PostgresMajor: major, ServerVersionNum: uint32(version),
		DatabaseName: databaseName, AuthorityPhase: phase,
		SessionUser: sessionUser, CurrentUser: currentUser, TxStatus: "T",
		MigrationID: cloneStringPointer(migrationID), StatementIndex: cloneUint32Pointer(statementIndex),
	}
	switch isolation {
	case "repeatable read":
		metadata.IsolationLevel = "repeatable_read"
	case "serializable":
		metadata.IsolationLevel = "serializable"
	default:
		return SnapshotMetadata{}, projectionFailure(CodeProjectionSnapshotInvalid, "snapshot-metadata", "isolation_level", major, false, "snapshot isolation level is invalid")
	}
	switch readOnly {
	case "on":
		metadata.AccessMode = "read_only"
	case "off":
		metadata.AccessMode = "read_write"
	default:
		return SnapshotMetadata{}, projectionFailure(CodeProjectionSnapshotInvalid, "snapshot-metadata", "access_mode", major, false, "snapshot access mode is invalid")
	}
	switch deferrable {
	case "on":
		metadata.Deferrable = true
	case "off":
		metadata.Deferrable = false
	default:
		return SnapshotMetadata{}, projectionFailure(CodeProjectionSnapshotInvalid, "snapshot-metadata", "deferrable", major, false, "snapshot deferrable mode is invalid")
	}
	if err := metadata.validate(); err != nil {
		return SnapshotMetadata{}, err
	}
	return metadata, nil
}

func cleanupIdleSnapshot(connection idleSnapshotConnection) {
	ctx, cancel := context.WithTimeout(context.Background(), projectionQueryTimeout)
	defer cancel()
	switch connection.txStatus() {
	case 'I':
		// Sanitize below.
	case 'T', 'E':
		if err := connection.rollback(ctx); err != nil || connection.txStatus() != 'I' {
			connection.hijackAndClose(ctx)
			return
		}
	default:
		connection.hijackAndClose(ctx)
		return
	}
	if err := sanitizeIdleSnapshotConnection(ctx, connection, "snapshot-cleanup", 0); err != nil {
		connection.hijackAndClose(ctx)
		return
	}
	connection.release()
}

func sanitizeIdleSnapshotConnection(ctx context.Context, connection idleSnapshotConnection, phase string, major uint16) error {
	if connection.txStatus() != 'I' {
		return projectionFailure(CodeProjectionSnapshotInvalid, phase, "tx_status", major, false, "projection connection is not idle for session sanitation")
	}
	sanitizeCtx, cancel := projectionRollbackContext(ctx)
	defer cancel()
	if err := connection.sanitize(sanitizeCtx); err != nil {
		return projectionFailure(CodeProjectionSnapshotInvalid, phase, "session_sanitation", major, false, "projection connection session sanitation failed")
	}
	if connection.txStatus() != 'I' {
		return projectionFailure(CodeProjectionSnapshotInvalid, phase, "tx_status", major, false, "projection connection status changed during session sanitation")
	}
	return nil
}

func projectionCleanupContext(ctx context.Context) context.Context {
	if ctx != nil && ctx.Err() == nil {
		return ctx
	}
	return context.Background()
}

func projectionRollbackContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx == nil || ctx.Err() != nil {
		ctx = context.Background()
	}
	return context.WithTimeout(ctx, projectionQueryTimeout)
}

func cloneStringPointer(value *string) *string {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneUint32Pointer(value *uint32) *uint32 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

type projectionTxStatusProvider interface{ projectionTxStatus() byte }

func migrationProjectionTxStatus(transaction MigrationTransaction) (byte, bool) {
	if provider, ok := transaction.(projectionTxStatusProvider); ok {
		return provider.projectionTxStatus(), true
	}
	if transaction, ok := transaction.(*pgxMigrationTx); ok && transaction != nil && transaction.tx != nil {
		return transaction.tx.Conn().PgConn().TxStatus(), true
	}
	return 0, false
}

func (session *pgxSession) beginRunnerSessionProjectionSnapshot(ctx context.Context, phase AuthorityPhase) (RunnerSessionProjectionSnapshot, error) {
	if session == nil || session.connection == nil || phase != AuthorityPhaseConnectedSession && phase != AuthorityPhaseMigrationRole {
		return nil, projectionFailure(CodeProjectionMetadataMismatch, "runner-snapshot-begin", "phase", 0, false, "runner projection phase is invalid")
	}
	session.stateMu.Lock()
	validState := !session.closed && !session.projectionActive
	switch phase {
	case AuthorityPhaseConnectedSession:
		validState = validState && !session.roleConfigured && session.advisoryKey == nil
	case AuthorityPhaseMigrationRole:
		validState = validState && session.roleConfigured && session.advisoryKey != nil
	}
	if validState {
		session.projectionActive = true
	}
	session.stateMu.Unlock()
	if !validState {
		return nil, projectionFailure(CodeProjectionMetadataMismatch, "runner-snapshot-begin", "lifecycle", 0, false, "runner session lifecycle does not match the projection phase")
	}
	return beginRunnerSessionProjectionSnapshot(ctx, &pgxRunnerSessionSnapshotConnection{session: session}, phase)
}

type pgxRunnerSessionSnapshotConnection struct {
	session *pgxSession
	tx      pgx.Tx
}

func (connection *pgxRunnerSessionSnapshotConnection) Query(ctx context.Context, sql string, args ...any) (Rows, error) {
	if connection.tx == nil {
		return nil, errors.New("runner projection transaction is unavailable")
	}
	return pgxQueryer{queryer: connection.tx}.Query(ctx, sql, args...)
}

func (connection *pgxRunnerSessionSnapshotConnection) QueryRow(ctx context.Context, sql string, args ...any) Row {
	if connection.tx == nil {
		return projectionErrorRow{err: errors.New("runner projection transaction is unavailable")}
	}
	return pgxQueryer{queryer: connection.tx}.QueryRow(ctx, sql, args...)
}

func (connection *pgxRunnerSessionSnapshotConnection) txStatus() byte {
	return connection.session.connection.PgConn().TxStatus()
}

func (connection *pgxRunnerSessionSnapshotConnection) prepare(ctx context.Context, phase AuthorityPhase) error {
	if connection.txStatus() != 'I' {
		return projectionFailure(CodeProjectionMetadataMismatch, "runner-snapshot-begin", "tx_status", 0, false, "runner session is not idle")
	}
	switch phase {
	case AuthorityPhaseConnectedSession:
		if !connection.session.runnerConnectedProjectionState() {
			return projectionFailure(CodeProjectionMetadataMismatch, "runner-snapshot-begin", "lifecycle", 0, false, "runner connected-session lifecycle is invalid")
		}
		if err := sanitizePGXProjectionSession(ctx, connection.session.connection); err != nil {
			return projectionFailure(CodeProjectionSnapshotInvalid, "runner-snapshot-begin", "sanitation", 0, false, "runner connected session sanitation failed")
		}
	case AuthorityPhaseMigrationRole:
		key, policy, ok := connection.session.runnerProjectionBoundary()
		if !ok {
			return projectionFailure(CodeProjectionMetadataMismatch, "runner-snapshot-begin", "lifecycle", 0, false, "runner migration role or advisory lock is unavailable")
		}
		boundary, err := connection.session.Boundary(ctx, key)
		settingsErr := connection.session.validateTrackedRoleAndSettings(ctx, policy)
		if err != nil || settingsErr != nil || boundary.TxStatus != 'I' || boundary.CurrentUser != MigrationOwnerRole || !boundary.LockHeld {
			return projectionFailure(CodeProjectionSnapshotInvalid, "runner-snapshot-begin", "boundary", 0, false, "runner migration role boundary is invalid")
		}
	default:
		return projectionFailure(CodeProjectionMetadataMismatch, "runner-snapshot-begin", "phase", 0, false, "runner projection phase is invalid")
	}
	return nil
}

func (connection *pgxRunnerSessionSnapshotConnection) begin(ctx context.Context) error {
	tx, err := connection.session.connection.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly, DeferrableMode: pgx.NotDeferrable})
	if err != nil {
		return err
	}
	connection.tx = tx
	return nil
}

func (connection *pgxRunnerSessionSnapshotConnection) rollback(ctx context.Context) error {
	if connection.tx == nil {
		return errors.New("runner projection transaction is unavailable")
	}
	err := connection.tx.Rollback(ctx)
	if err == nil {
		connection.tx = nil
	}
	return err
}

func (connection *pgxRunnerSessionSnapshotConnection) validateReturn(ctx context.Context, phase AuthorityPhase) error {
	if connection.txStatus() != 'I' {
		return projectionFailure(CodeProjectionSnapshotInvalid, "runner-snapshot-close", "tx_status", 0, false, "runner session did not return to idle")
	}
	switch phase {
	case AuthorityPhaseConnectedSession:
		if !connection.session.runnerConnectedProjectionState() {
			return projectionFailure(CodeProjectionMetadataMismatch, "runner-snapshot-close", "lifecycle", 0, false, "runner connected-session lifecycle changed")
		}
		query, ok := projectionFixedQuery(projectionQuerySnapshotRoleReadback)
		if !ok {
			return projectionFailure(CodeProjectionSnapshotInvalid, "runner-snapshot-close", "query_id", 0, false, "runner role readback query is unavailable")
		}
		var sessionUser, currentUser string
		if err := connection.session.connection.QueryRow(ctx, query, pgx.QueryExecModeSimpleProtocol).Scan(&sessionUser, &currentUser); err != nil || sessionUser == "" || currentUser != sessionUser {
			return projectionFailure(CodeProjectionSnapshotInvalid, "runner-snapshot-close", "current_user", 0, false, "runner connected session role changed")
		}
	case AuthorityPhaseMigrationRole:
		key, policy, ok := connection.session.runnerProjectionBoundary()
		if !ok {
			return projectionFailure(CodeProjectionMetadataMismatch, "runner-snapshot-close", "lifecycle", 0, false, "runner migration role or advisory lock is unavailable")
		}
		boundary, err := connection.session.Boundary(ctx, key)
		settingsErr := connection.session.validateTrackedRoleAndSettings(ctx, policy)
		if err != nil || settingsErr != nil || boundary.TxStatus != 'I' || boundary.CurrentUser != MigrationOwnerRole || !boundary.LockHeld {
			return projectionFailure(CodeProjectionSnapshotInvalid, "runner-snapshot-close", "boundary", 0, false, "runner migration role boundary changed")
		}
	default:
		return projectionFailure(CodeProjectionMetadataMismatch, "runner-snapshot-close", "phase", 0, false, "runner projection phase is invalid")
	}
	return nil
}

func (connection *pgxRunnerSessionSnapshotConnection) invalidate(ctx context.Context) {
	connection.session.stateMu.Lock()
	connection.session.closed = true
	connection.session.stateMu.Unlock()
	_ = connection.session.connection.Close(ctx)
}

func (connection *pgxRunnerSessionSnapshotConnection) returnToRunner() {
	connection.session.stateMu.Lock()
	connection.session.projectionActive = false
	connection.session.stateMu.Unlock()
}

func (session *pgxSession) runnerProjectionBoundary() (int64, ExecutionPolicy, bool) {
	session.stateMu.Lock()
	defer session.stateMu.Unlock()
	if session.closed || !session.projectionActive || !session.roleConfigured || session.settingsPolicy == nil || session.advisoryKey == nil {
		return 0, ExecutionPolicy{}, false
	}
	return *session.advisoryKey, *session.settingsPolicy, true
}

func (session *pgxSession) runnerConnectedProjectionState() bool {
	session.stateMu.Lock()
	defer session.stateMu.Unlock()
	return !session.closed && session.projectionActive && !session.roleConfigured && session.settingsPolicy == nil && session.advisoryKey == nil
}

type idleSnapshotPool interface {
	acquire(context.Context) (idleSnapshotConnection, error)
}

type idleSnapshotConnection interface {
	Queryer
	txStatus() byte
	sanitize(context.Context) error
	setMigrationRole(context.Context) error
	begin(context.Context) error
	rollback(context.Context) error
	release()
	hijackAndClose(context.Context)
}

type pgxIdleSnapshotPool struct{ pool *pgxpool.Pool }

func (pool pgxIdleSnapshotPool) acquire(ctx context.Context) (idleSnapshotConnection, error) {
	connection, err := pool.pool.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	return &pgxIdleSnapshotConnection{connection: connection}, nil
}

type pgxIdleSnapshotConnection struct {
	connection *pgxpool.Conn
	tx         pgx.Tx
}

func (connection *pgxIdleSnapshotConnection) Query(ctx context.Context, sql string, args ...any) (Rows, error) {
	if connection.tx == nil {
		return nil, errors.New("projection transaction is unavailable")
	}
	return pgxQueryer{queryer: connection.tx}.Query(ctx, sql, args...)
}

func (connection *pgxIdleSnapshotConnection) QueryRow(ctx context.Context, sql string, args ...any) Row {
	if connection.tx == nil {
		return projectionErrorRow{err: errors.New("projection transaction is unavailable")}
	}
	return pgxQueryer{queryer: connection.tx}.QueryRow(ctx, sql, args...)
}

func (connection *pgxIdleSnapshotConnection) txStatus() byte {
	return connection.connection.Conn().PgConn().TxStatus()
}

func (connection *pgxIdleSnapshotConnection) sanitize(ctx context.Context) error {
	if connection.txStatus() != 'I' {
		return errors.New("projection connection is not idle")
	}
	return sanitizePGXProjectionSession(ctx, connection.connection.Conn())
}

func sanitizePGXProjectionSession(ctx context.Context, raw *pgx.Conn) error {
	if raw == nil || raw.PgConn().TxStatus() != 'I' {
		return errors.New("projection connection is not idle")
	}
	resetQuery, resetOK := projectionFixedQuery(projectionQuerySnapshotReset)
	readbackQuery, readbackOK := projectionFixedQuery(projectionQuerySnapshotSanitation)
	if !resetOK || !readbackOK {
		return errors.New("projection session sanitation query is unavailable")
	}
	// Clear pgx's local prepared-statement cache before DISCARD ALL clears the
	// backend state. The reset itself uses the simple protocol and can therefore
	// never become a prepared statement that invalidates its own client cache.
	if err := raw.DeallocateAll(ctx); err != nil {
		return err
	}
	if _, err := raw.PgConn().Exec(ctx, resetQuery).ReadAll(); err != nil {
		return err
	}
	var sessionUser, currentUser string
	var searchPath, searchPathBaseline string
	var statementTimeout, statementTimeoutBaseline string
	var lockTimeout, lockTimeoutBaseline string
	var idleTimeout, idleTimeoutBaseline string
	var preparedCount int64
	if err := raw.QueryRow(ctx, readbackQuery, pgx.QueryExecModeSimpleProtocol).Scan(
		&sessionUser, &currentUser,
		&searchPath, &searchPathBaseline,
		&statementTimeout, &statementTimeoutBaseline,
		&lockTimeout, &lockTimeoutBaseline,
		&idleTimeout, &idleTimeoutBaseline,
		&preparedCount,
	); err != nil {
		return err
	}
	if sessionUser == "" || !utf8.ValidString(sessionUser) || currentUser != sessionUser ||
		searchPath != searchPathBaseline || statementTimeout != statementTimeoutBaseline ||
		lockTimeout != lockTimeoutBaseline || idleTimeout != idleTimeoutBaseline || preparedCount != 0 {
		return errors.New("projection session sanitation readback differs from baseline")
	}
	return nil
}

func (connection *pgxIdleSnapshotConnection) setMigrationRole(ctx context.Context) error {
	if connection.txStatus() != 'I' {
		return errors.New("projection connection is not idle")
	}
	setRoleQuery, setRoleOK := projectionFixedQuery(projectionQuerySnapshotSetMigrationRole)
	readbackQuery, readbackOK := projectionFixedQuery(projectionQuerySnapshotRoleReadback)
	if !setRoleOK || !readbackOK {
		return errors.New("projection migration role query is unavailable")
	}
	raw := connection.connection.Conn()
	if _, err := raw.PgConn().Exec(ctx, setRoleQuery).ReadAll(); err != nil {
		return err
	}
	var sessionUser, currentUser string
	if err := raw.QueryRow(ctx, readbackQuery, pgx.QueryExecModeSimpleProtocol).Scan(&sessionUser, &currentUser); err != nil {
		return err
	}
	if sessionUser == "" || !utf8.ValidString(sessionUser) || currentUser != MigrationOwnerRole {
		return errors.New("projection migration role readback differs from the fixed role")
	}
	return nil
}

func (connection *pgxIdleSnapshotConnection) begin(ctx context.Context) error {
	tx, err := connection.connection.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly, DeferrableMode: pgx.NotDeferrable})
	if err != nil {
		return err
	}
	connection.tx = tx
	return nil
}

func (connection *pgxIdleSnapshotConnection) rollback(ctx context.Context) error {
	if connection.tx == nil {
		return errors.New("projection transaction is unavailable")
	}
	err := connection.tx.Rollback(ctx)
	if err == nil {
		connection.tx = nil
	}
	return err
}

func (connection *pgxIdleSnapshotConnection) release() { connection.connection.Release() }

func (connection *pgxIdleSnapshotConnection) hijackAndClose(context.Context) {
	hijacked := connection.connection.Hijack()
	closeCtx, cancel := context.WithTimeout(context.Background(), projectionQueryTimeout)
	defer cancel()
	_ = hijacked.Close(closeCtx)
}

var _ IdleProjectionSnapshot = (*ownedIdleProjectionSnapshot)(nil)
var _ RunnerSessionProjectionSnapshot = (*ownedRunnerSessionProjectionSnapshot)(nil)
var _ runnerSessionProjectionSnapshotProvider = (*pgxSession)(nil)
var _ ProjectionSnapshot = (*fixedQueryProjectionSnapshot)(nil)
