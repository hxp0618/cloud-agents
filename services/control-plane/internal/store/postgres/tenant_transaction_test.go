package postgres

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestTenantReadSuccessNullClearAndSavedHandle(t *testing.T) {
	tenantID := "tenant-001"
	now := time.Date(2026, time.August, 11, 1, 2, 3, 0, time.UTC)
	transaction := &fakeTransaction{rows: []rowScanner{
		rowValues(tenantID),
		rowValues(tenantID),
		rowValues(
			tenantID,
			tenantID,
			"tenant-one",
			"Tenant One",
			"active",
			int64(1),
			now,
			now,
		),
	}}
	connection := newFakeConnection(transaction)
	connection.outsideRows = []rowScanner{rowValues((*string)(nil))}
	runner := newTenantTransactionRunner(&fakePool{connection: connection}, time.Second)

	var saved TenantReadCapability
	err := runner.WithTenantRead(context.Background(), tenantID, func(
		ctx context.Context,
		capability TenantReadCapability,
	) error {
		saved = capability
		tenant, readErr := capability.GetPlatformTenant(ctx)
		if readErr != nil {
			return readErr
		}
		if tenant.TenantID != tenantID || tenant.ResourceVersion != 1 {
			t.Fatalf("unexpected tenant projection: %#v", tenant)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WithTenantRead() error = %v", err)
	}

	if _, err := saved.GetPlatformTenant(context.Background()); !errors.Is(err, ErrTenantCapabilityClosed) {
		t.Fatalf("saved capability error = %v, want ErrTenantCapabilityClosed", err)
	}

	assertConnectionDisposition(t, connection, 1, 0)
	if transaction.commitCalls != 1 || transaction.rollbackCalls != 0 {
		t.Fatalf("commit/rollback calls = %d/%d", transaction.commitCalls, transaction.rollbackCalls)
	}
	assertReadOnlyOptions(t, connection.beginOptions)
	if len(transaction.queries) != 3 {
		t.Fatalf("transaction query count = %d, want 3", len(transaction.queries))
	}
	if got := transaction.queries[0].arguments; len(got) != 1 || got[0] != tenantID {
		t.Fatalf("set_config arguments = %#v", got)
	}
	if transaction.queries[2].sql != getPlatformTenantSQL || len(transaction.queries[2].arguments) != 0 {
		t.Fatalf("typed query leaked arguments or changed SQL: %#v", transaction.queries[2])
	}
	if !strings.Contains(transaction.queries[2].sql, "cloud_agents.platform_tenants") ||
		!strings.Contains(transaction.queries[2].sql, "cloud_agents.require_tenant_id()") {
		t.Fatalf("typed query is not fully-qualified and GUC-bound: %s", transaction.queries[2].sql)
	}
}

func TestTenantReadCapabilitySerializesConcurrentReads(t *testing.T) {
	tenantID := "tenant-001"
	now := time.Date(2026, time.August, 11, 1, 2, 3, 0, time.UTC)
	scanStarted := make(chan struct{})
	releaseScan := make(chan struct{})
	queryStarted := make(chan int, 8)
	tenantValues := []any{
		tenantID,
		tenantID,
		"tenant-one",
		"Tenant One",
		"active",
		int64(1),
		now,
		now,
	}
	transaction := &fakeTransaction{
		rows: []rowScanner{
			rowValues(tenantID),
			rowValues(tenantID),
			&blockingRow{
				started: scanStarted,
				release: releaseScan,
				row:     rowValues(tenantValues...),
			},
			rowValues(tenantValues...),
		},
		queryStarted: queryStarted,
	}
	connection := newFakeConnection(transaction)
	connection.outsideRows = []rowScanner{rowValues((*string)(nil))}
	runner := newTenantTransactionRunner(&fakePool{connection: connection}, time.Second)

	err := runner.WithTenantRead(context.Background(), tenantID, func(
		ctx context.Context,
		capability TenantReadCapability,
	) error {
		firstResult := make(chan error, 1)
		go func() {
			_, readErr := capability.GetPlatformTenant(ctx)
			firstResult <- readErr
		}()
		<-scanStarted
		for expected := 1; expected <= 3; expected++ {
			if got := <-queryStarted; got != expected {
				t.Fatalf("query start = %d, want %d", got, expected)
			}
		}

		secondInvoked := make(chan struct{})
		secondResult := make(chan error, 1)
		go func() {
			close(secondInvoked)
			_, readErr := capability.GetPlatformTenant(ctx)
			secondResult <- readErr
		}()
		<-secondInvoked
		select {
		case got := <-queryStarted:
			t.Fatalf("concurrent pgx query entered before prior Scan completed: %d", got)
		case <-time.After(50 * time.Millisecond):
		}

		close(releaseScan)
		if readErr := <-firstResult; readErr != nil {
			return readErr
		}
		if got := <-queryStarted; got != 4 {
			t.Fatalf("serialized query start = %d, want 4", got)
		}
		return <-secondResult
	})
	if err != nil {
		t.Fatalf("WithTenantRead() error = %v", err)
	}
	assertConnectionDisposition(t, connection, 1, 0)
}

func TestTenantReadReusesPhysicalConnectionForNullAndEmptyClear(t *testing.T) {
	first := successfulFakeTransaction("tenant-001")
	second := successfulFakeTransaction("tenant-002")
	connection := newFakeConnection(first, second)
	empty := ""
	connection.outsideRows = []rowScanner{
		rowValues((*string)(nil)),
		rowValues(&empty),
	}
	pool := &fakePool{connection: connection}
	runner := newTenantTransactionRunner(pool, time.Second)

	for _, tenantID := range []string{"tenant-001", "tenant-002"} {
		if err := runner.WithTenantRead(
			context.Background(),
			tenantID,
			func(context.Context, TenantReadCapability) error { return nil },
		); err != nil {
			t.Fatalf("WithTenantRead(%q) error = %v", tenantID, err)
		}
	}

	if pool.acquireCalls != 2 {
		t.Fatalf("pool acquire calls = %d, want 2", pool.acquireCalls)
	}
	assertConnectionDisposition(t, connection, 2, 0)
	if len(connection.outsideQueries) != 2 {
		t.Fatalf("outside clear checks = %d, want 2", len(connection.outsideQueries))
	}
	for _, query := range connection.outsideQueries {
		if query.sql != readClearedTenantSQL || len(query.arguments) != 0 {
			t.Fatalf("unexpected outside clear query: %#v", query)
		}
	}
}

func TestTenantReadCallbackErrorRollsBackAndPreservesError(t *testing.T) {
	callbackErr := errors.New("callback failed")
	transaction := successfulFakeTransaction("tenant-001")
	connection := newFakeConnection(transaction)
	connection.outsideRows = []rowScanner{rowValues((*string)(nil))}
	runner := newTenantTransactionRunner(&fakePool{connection: connection}, time.Second)

	err := runner.WithTenantRead(
		context.Background(),
		"tenant-001",
		func(context.Context, TenantReadCapability) error { return callbackErr },
	)
	if err != callbackErr {
		t.Fatalf("WithTenantRead() error = %v, want exact callback error", err)
	}
	if transaction.rollbackCalls != 1 || transaction.commitCalls != 0 {
		t.Fatalf("commit/rollback calls = %d/%d", transaction.commitCalls, transaction.rollbackCalls)
	}
	assertConnectionDisposition(t, connection, 1, 0)
}

func TestTenantReadPanicCleansUpAndRepanicsOriginalValue(t *testing.T) {
	panicValue := &struct{ message string }{message: "panic-value"}
	transaction := successfulFakeTransaction("tenant-001")
	connection := newFakeConnection(transaction)
	connection.outsideRows = []rowScanner{rowValues((*string)(nil))}
	runner := newTenantTransactionRunner(&fakePool{connection: connection}, time.Second)

	var recovered any
	func() {
		defer func() { recovered = recover() }()
		_ = runner.WithTenantRead(
			context.Background(),
			"tenant-001",
			func(context.Context, TenantReadCapability) error { panic(panicValue) },
		)
	}()

	if recovered != panicValue {
		t.Fatalf("recovered panic = %#v, want original %#v", recovered, panicValue)
	}
	if transaction.rollbackCalls != 1 {
		t.Fatalf("rollback calls = %d, want 1", transaction.rollbackCalls)
	}
	assertConnectionDisposition(t, connection, 1, 0)
}

func TestTenantReadCancellationWithKnownRollbackReleases(t *testing.T) {
	transaction := successfulFakeTransaction("tenant-001")
	connection := newFakeConnection(transaction)
	connection.outsideRows = []rowScanner{rowValues((*string)(nil))}
	runner := newTenantTransactionRunner(&fakePool{connection: connection}, time.Second)
	ctx, cancel := context.WithCancel(context.Background())

	err := runner.WithTenantRead(ctx, "tenant-001", func(
		context.Context,
		TenantReadCapability,
	) error {
		cancel()
		return ctx.Err()
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("WithTenantRead() error = %v, want context.Canceled", err)
	}
	if transaction.rollbackCalls != 1 {
		t.Fatalf("rollback calls = %d, want 1", transaction.rollbackCalls)
	}
	assertConnectionDisposition(t, connection, 1, 0)
}

func TestTenantReadCommitAcknowledgementUnknownHijacks(t *testing.T) {
	commitErr := errors.New("commit acknowledgement lost")
	transaction := successfulFakeTransaction("tenant-001")
	transaction.commitErr = commitErr
	connection := newFakeConnection(transaction)
	runner := newTenantTransactionRunner(&fakePool{connection: connection}, time.Second)

	err := runner.WithTenantRead(
		context.Background(),
		"tenant-001",
		func(context.Context, TenantReadCapability) error { return nil },
	)
	if !errors.Is(err, commitErr) {
		t.Fatalf("WithTenantRead() error = %v, want commit error", err)
	}
	assertConnectionDisposition(t, connection, 0, 1)
}

func TestTenantReadRollbackFailureHijacksAndPreservesCallbackError(t *testing.T) {
	callbackErr := errors.New("callback failed")
	transaction := successfulFakeTransaction("tenant-001")
	transaction.rollbackErr = errors.New("rollback acknowledgement lost")
	connection := newFakeConnection(transaction)
	runner := newTenantTransactionRunner(&fakePool{connection: connection}, time.Second)

	err := runner.WithTenantRead(
		context.Background(),
		"tenant-001",
		func(context.Context, TenantReadCapability) error { return callbackErr },
	)
	if err != callbackErr {
		t.Fatalf("WithTenantRead() error = %v, want exact callback error", err)
	}
	assertConnectionDisposition(t, connection, 0, 1)
}

func TestTenantReadCancellationWithUnknownRollbackHijacks(t *testing.T) {
	transaction := successfulFakeTransaction("tenant-001")
	transaction.rollbackErr = errors.New("rollback unknown")
	connection := newFakeConnection(transaction)
	runner := newTenantTransactionRunner(&fakePool{connection: connection}, time.Second)
	ctx, cancel := context.WithCancel(context.Background())

	err := runner.WithTenantRead(ctx, "tenant-001", func(
		context.Context,
		TenantReadCapability,
	) error {
		cancel()
		return ctx.Err()
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("WithTenantRead() error = %v, want context.Canceled", err)
	}
	assertConnectionDisposition(t, connection, 0, 1)
}

func TestTenantReadAcquireAndBeginFailuresDoNotLeakConnections(t *testing.T) {
	acquireErr := errors.New("acquire failed")
	runner := newTenantTransactionRunner(&fakePool{acquireErr: acquireErr}, time.Second)
	err := runner.WithTenantRead(
		context.Background(),
		"tenant-001",
		func(context.Context, TenantReadCapability) error { return nil },
	)
	if !errors.Is(err, acquireErr) {
		t.Fatalf("acquire error = %v, want %v", err, acquireErr)
	}

	beginErr := errors.New("begin failed")
	connection := newFakeConnection()
	connection.beginErr = beginErr
	runner = newTenantTransactionRunner(&fakePool{connection: connection}, time.Second)
	err = runner.WithTenantRead(
		context.Background(),
		"tenant-001",
		func(context.Context, TenantReadCapability) error { return nil },
	)
	if !errors.Is(err, beginErr) {
		t.Fatalf("begin error = %v, want %v", err, beginErr)
	}
	assertConnectionDisposition(t, connection, 0, 1)
}

func TestTenantReadPanicWithUnknownRollbackHijacksAndRepanics(t *testing.T) {
	panicValue := &struct{ message string }{message: "panic-with-rollback-loss"}
	transaction := successfulFakeTransaction("tenant-001")
	transaction.rollbackErr = errors.New("rollback acknowledgement lost")
	connection := newFakeConnection(transaction)
	runner := newTenantTransactionRunner(&fakePool{connection: connection}, time.Second)

	var recovered any
	func() {
		defer func() { recovered = recover() }()
		_ = runner.WithTenantRead(
			context.Background(),
			"tenant-001",
			func(context.Context, TenantReadCapability) error { panic(panicValue) },
		)
	}()
	if recovered != panicValue {
		t.Fatalf("recovered panic = %#v, want original %#v", recovered, panicValue)
	}
	assertConnectionDisposition(t, connection, 0, 1)
}

func TestTenantReadCallbackErrorPreservedWhenPostRollbackCleanupFails(t *testing.T) {
	callbackErr := errors.New("callback failed")
	tests := []struct {
		name      string
		configure func(*fakeConnection, *fakeTransaction)
	}{
		{
			name: "clear check",
			configure: func(connection *fakeConnection, _ *fakeTransaction) {
				connection.outsideRows = []rowScanner{rowError(errors.New("clear check failed"))}
			},
		},
		{
			name: "non idle",
			configure: func(_ *fakeConnection, transaction *fakeTransaction) {
				transaction.rollbackStatus = 'T'
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			transaction := successfulFakeTransaction("tenant-001")
			connection := newFakeConnection(transaction)
			test.configure(connection, transaction)
			runner := newTenantTransactionRunner(&fakePool{connection: connection}, time.Second)

			err := runner.WithTenantRead(
				context.Background(),
				"tenant-001",
				func(context.Context, TenantReadCapability) error { return callbackErr },
			)
			if err != callbackErr {
				t.Fatalf("callback error = %v, want exact %v", err, callbackErr)
			}
			assertConnectionDisposition(t, connection, 0, 1)
		})
	}
}

func TestTenantReadNonIdleStatusHijacks(t *testing.T) {
	transaction := successfulFakeTransaction("tenant-001")
	transaction.commitStatus = 'T'
	connection := newFakeConnection(transaction)
	runner := newTenantTransactionRunner(&fakePool{connection: connection}, time.Second)

	err := runner.WithTenantRead(
		context.Background(),
		"tenant-001",
		func(context.Context, TenantReadCapability) error { return nil },
	)
	if !errors.Is(err, ErrConnectionNotReusable) {
		t.Fatalf("WithTenantRead() error = %v, want ErrConnectionNotReusable", err)
	}
	assertConnectionDisposition(t, connection, 0, 1)
}

func TestTenantReadNonEmptyPostTransactionGUCHijacks(t *testing.T) {
	transaction := successfulFakeTransaction("tenant-001")
	connection := newFakeConnection(transaction)
	leaked := "tenant-001"
	connection.outsideRows = []rowScanner{rowValues(&leaked)}
	runner := newTenantTransactionRunner(&fakePool{connection: connection}, time.Second)

	err := runner.WithTenantRead(
		context.Background(),
		"tenant-001",
		func(context.Context, TenantReadCapability) error { return nil },
	)
	if !errors.Is(err, ErrConnectionNotReusable) {
		t.Fatalf("WithTenantRead() error = %v, want ErrConnectionNotReusable", err)
	}
	assertConnectionDisposition(t, connection, 0, 1)
}

func TestTenantReadTypedReadPropagates22023AndRollsBack(t *testing.T) {
	pgErr := &pgconn.PgError{
		Code:    "22023",
		Message: "cloud_agents.tenant_id is missing",
	}
	transaction := &fakeTransaction{rows: []rowScanner{
		rowValues("tenant-001"),
		rowValues("tenant-001"),
		rowError(pgErr),
	}}
	connection := newFakeConnection(transaction)
	connection.outsideRows = []rowScanner{rowValues((*string)(nil))}
	runner := newTenantTransactionRunner(&fakePool{connection: connection}, time.Second)

	err := runner.WithTenantRead(context.Background(), "tenant-001", func(
		ctx context.Context,
		capability TenantReadCapability,
	) error {
		_, readErr := capability.GetPlatformTenant(ctx)
		return readErr
	})
	var actualPGError *pgconn.PgError
	if !errors.As(err, &actualPGError) || actualPGError.Code != "22023" {
		t.Fatalf("WithTenantRead() error = %v, want PgError 22023", err)
	}
	if transaction.rollbackCalls != 1 {
		t.Fatalf("rollback calls = %d, want 1", transaction.rollbackCalls)
	}
	assertConnectionDisposition(t, connection, 1, 0)
}

func TestTenantReadBindingMismatchRollsBack(t *testing.T) {
	transaction := &fakeTransaction{rows: []rowScanner{
		rowValues("tenant-001"),
		rowValues("tenant-other"),
	}}
	connection := newFakeConnection(transaction)
	connection.outsideRows = []rowScanner{rowValues((*string)(nil))}
	runner := newTenantTransactionRunner(&fakePool{connection: connection}, time.Second)

	err := runner.WithTenantRead(
		context.Background(),
		"tenant-001",
		func(context.Context, TenantReadCapability) error {
			t.Fatal("callback must not run after binding mismatch")
			return nil
		},
	)
	if !errors.Is(err, ErrTenantBindingMismatch) {
		t.Fatalf("WithTenantRead() error = %v, want ErrTenantBindingMismatch", err)
	}
	if transaction.rollbackCalls != 1 {
		t.Fatalf("rollback calls = %d, want 1", transaction.rollbackCalls)
	}
	assertConnectionDisposition(t, connection, 1, 0)
}

func TestTenantReadInputValidation(t *testing.T) {
	if _, err := NewTenantTransactionRunner(nil); !errors.Is(err, ErrNilPool) {
		t.Fatalf("NewTenantTransactionRunner(nil) error = %v", err)
	}

	runner := newTenantTransactionRunner(&fakePool{}, time.Second)
	if err := runner.WithTenantRead(nil, "tenant-001", func(context.Context, TenantReadCapability) error {
		return nil
	}); !errors.Is(err, ErrNilContext) {
		t.Fatalf("nil context error = %v", err)
	}
	if err := runner.WithTenantRead(context.Background(), "tenant-001", nil); !errors.Is(err, ErrNilCallback) {
		t.Fatalf("nil callback error = %v", err)
	}
}

func assertReadOnlyOptions(t *testing.T, options []pgx.TxOptions) {
	t.Helper()
	if len(options) != 1 {
		t.Fatalf("BeginTx options count = %d, want 1", len(options))
	}
	actual := options[0]
	if actual.IsoLevel != pgx.ReadCommitted ||
		actual.AccessMode != pgx.ReadOnly ||
		actual.DeferrableMode != pgx.NotDeferrable {
		t.Fatalf("BeginTx options = %#v", actual)
	}
}

func assertConnectionDisposition(
	t *testing.T,
	connection *fakeConnection,
	wantReleases int,
	wantHijacks int,
) {
	t.Helper()
	connection.mutex.Lock()
	defer connection.mutex.Unlock()
	if connection.releaseCalls != wantReleases || connection.hijackCalls != wantHijacks {
		t.Fatalf(
			"release/hijack calls = %d/%d, want %d/%d",
			connection.releaseCalls,
			connection.hijackCalls,
			wantReleases,
			wantHijacks,
		)
	}
	if connection.closeCalls != wantHijacks {
		t.Fatalf("close calls = %d, want %d", connection.closeCalls, wantHijacks)
	}
}

func successfulFakeTransaction(tenantID string) *fakeTransaction {
	return &fakeTransaction{rows: []rowScanner{
		rowValues(tenantID),
		rowValues(tenantID),
	}}
}

type queryCall struct {
	sql       string
	arguments []any
}

type fakePool struct {
	mutex        sync.Mutex
	connection   physicalConnection
	acquireErr   error
	acquireCalls int
}

func (pool *fakePool) acquire(context.Context) (physicalConnection, error) {
	pool.mutex.Lock()
	defer pool.mutex.Unlock()
	pool.acquireCalls++
	if pool.acquireErr != nil {
		return nil, pool.acquireErr
	}
	return pool.connection, nil
}

type fakeConnection struct {
	mutex          sync.Mutex
	transactions   []*fakeTransaction
	outsideRows    []rowScanner
	outsideQueries []queryCall
	beginOptions   []pgx.TxOptions
	beginErr       error
	status         byte
	releaseCalls   int
	hijackCalls    int
	closeCalls     int
}

func newFakeConnection(transactions ...*fakeTransaction) *fakeConnection {
	return &fakeConnection{
		transactions: transactions,
		status:       idleTransactionStatus,
	}
}

func (connection *fakeConnection) beginTx(
	_ context.Context,
	options pgx.TxOptions,
) (tenantTransaction, error) {
	connection.mutex.Lock()
	defer connection.mutex.Unlock()
	connection.beginOptions = append(connection.beginOptions, options)
	if connection.beginErr != nil {
		return nil, connection.beginErr
	}
	if len(connection.transactions) == 0 {
		return nil, errors.New("fake transaction queue exhausted")
	}
	transaction := connection.transactions[0]
	connection.transactions = connection.transactions[1:]
	transaction.connection = connection
	connection.status = 'T'
	return transaction, nil
}

func (connection *fakeConnection) queryRow(
	_ context.Context,
	sql string,
	arguments ...any,
) rowScanner {
	connection.mutex.Lock()
	defer connection.mutex.Unlock()
	connection.outsideQueries = append(connection.outsideQueries, queryCall{
		sql:       sql,
		arguments: append([]any(nil), arguments...),
	})
	if len(connection.outsideRows) == 0 {
		return rowError(errors.New("fake outside row queue exhausted"))
	}
	row := connection.outsideRows[0]
	connection.outsideRows = connection.outsideRows[1:]
	return row
}

func (connection *fakeConnection) txStatus() byte {
	connection.mutex.Lock()
	defer connection.mutex.Unlock()
	return connection.status
}

func (connection *fakeConnection) release() {
	connection.mutex.Lock()
	defer connection.mutex.Unlock()
	connection.releaseCalls++
}

func (connection *fakeConnection) hijackAndClose(context.Context) error {
	connection.mutex.Lock()
	defer connection.mutex.Unlock()
	connection.hijackCalls++
	connection.closeCalls++
	return nil
}

type fakeTransaction struct {
	mutex          sync.Mutex
	connection     *fakeConnection
	rows           []rowScanner
	queries        []queryCall
	commitErr      error
	rollbackErr    error
	commitStatus   byte
	rollbackStatus byte
	commitCalls    int
	rollbackCalls  int
	queryStarted   chan int
}

func (transaction *fakeTransaction) queryRow(
	_ context.Context,
	sql string,
	arguments ...any,
) rowScanner {
	transaction.mutex.Lock()
	defer transaction.mutex.Unlock()
	transaction.queries = append(transaction.queries, queryCall{
		sql:       sql,
		arguments: append([]any(nil), arguments...),
	})
	if transaction.queryStarted != nil {
		transaction.queryStarted <- len(transaction.queries)
	}
	if len(transaction.rows) == 0 {
		return rowError(errors.New("fake transaction row queue exhausted"))
	}
	row := transaction.rows[0]
	transaction.rows = transaction.rows[1:]
	return row
}

func (transaction *fakeTransaction) commit(context.Context) error {
	transaction.mutex.Lock()
	defer transaction.mutex.Unlock()
	transaction.commitCalls++
	if transaction.commitErr != nil {
		return transaction.commitErr
	}
	status := transaction.commitStatus
	if status == 0 {
		status = idleTransactionStatus
	}
	transaction.setConnectionStatus(status)
	return nil
}

func (transaction *fakeTransaction) rollback(context.Context) error {
	transaction.mutex.Lock()
	defer transaction.mutex.Unlock()
	transaction.rollbackCalls++
	if transaction.rollbackErr != nil {
		return transaction.rollbackErr
	}
	status := transaction.rollbackStatus
	if status == 0 {
		status = idleTransactionStatus
	}
	transaction.setConnectionStatus(status)
	return nil
}

func (transaction *fakeTransaction) setConnectionStatus(status byte) {
	transaction.connection.mutex.Lock()
	defer transaction.connection.mutex.Unlock()
	transaction.connection.status = status
}

type fakeRow struct {
	values []any
	err    error
}

type blockingRow struct {
	started chan<- struct{}
	release <-chan struct{}
	row     rowScanner
}

func (row *blockingRow) Scan(destinations ...any) error {
	close(row.started)
	<-row.release
	return row.row.Scan(destinations...)
}

func rowValues(values ...any) rowScanner {
	return &fakeRow{values: values}
}

func rowError(err error) rowScanner {
	return &fakeRow{err: err}
}

func (row *fakeRow) Scan(destinations ...any) error {
	if row.err != nil {
		return row.err
	}
	if len(destinations) != len(row.values) {
		return errors.New("fake row destination count mismatch")
	}
	for index, destination := range destinations {
		destinationValue := reflect.ValueOf(destination)
		if destinationValue.Kind() != reflect.Pointer || destinationValue.IsNil() {
			return errors.New("fake row destination is not a non-nil pointer")
		}
		value := reflect.ValueOf(row.values[index])
		if !value.IsValid() {
			destinationValue.Elem().Set(reflect.Zero(destinationValue.Elem().Type()))
			continue
		}
		if !value.Type().AssignableTo(destinationValue.Elem().Type()) {
			return errors.New("fake row value type mismatch")
		}
		destinationValue.Elem().Set(value)
	}
	return nil
}
