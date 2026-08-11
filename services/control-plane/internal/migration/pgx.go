package migration

import (
	"context"
	"fmt"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type DatabaseConnector interface {
	Connect(context.Context, string) (DatabaseSession, error)
}

type BoundaryState struct {
	TxStatus    byte
	CurrentUser string
	LockHeld    bool
}

type DatabaseSession interface {
	Queryer() Queryer
	ServerMajor(context.Context) (int, error)
	SetRoleAndSettings(context.Context, ExecutionPolicy) error
	AcquireAdvisoryLock(context.Context, int64) error
	Boundary(context.Context, int64) (BoundaryState, error)
	BeginMigration(context.Context) (MigrationTransaction, error)
	UnlockAndReset(context.Context, int64) error
	Close(context.Context) error
}

type MigrationTransaction interface {
	Queryer
	CommandExecutor
	ExecuteStatement(context.Context, []byte) error
	Boundary(context.Context, int64) (BoundaryState, error)
	Commit(context.Context) error
	Rollback(context.Context) error
}

type PGXConnector struct{}

func (PGXConnector) Connect(ctx context.Context, dsn string) (DatabaseSession, error) {
	config, err := pgx.ParseConfig(dsn)
	if err != nil {
		return nil, fail(CodeUnsupported, "connect", "target PostgreSQL configuration is invalid", err)
	}
	// QueryExecModeExec is extended protocol (Parse/Bind/Execute) and rejects a
	// statement string containing multiple server statements.
	config.DefaultQueryExecMode = pgx.QueryExecModeExec
	connection, err := pgx.ConnectConfig(ctx, config)
	if err != nil {
		return nil, fail(CodeUnsupported, "connect", "cannot establish dedicated PostgreSQL connection", err)
	}
	return &pgxSession{connection: connection}, nil
}

type pgxSession struct{ connection *pgx.Conn }

func (session *pgxSession) Queryer() Queryer { return pgxQueryer{queryer: session.connection} }

func (session *pgxSession) ServerMajor(ctx context.Context) (int, error) {
	var versionText string
	if err := session.connection.QueryRow(ctx, "SHOW server_version_num").Scan(&versionText); err != nil {
		return 0, fail(CodeUnsupported, "server-version", "cannot read server_version_num", err)
	}
	version, err := strconv.Atoi(versionText)
	if err != nil || version < 10000 {
		return 0, fail(CodeUnsupported, "server-version", "invalid server_version_num", err)
	}
	return version / 10000, nil
}

func (session *pgxSession) SetRoleAndSettings(ctx context.Context, policy ExecutionPolicy) error {
	if _, err := session.connection.Exec(ctx, "SET ROLE cloud_agents_migration_owner"); err != nil {
		return fail(CodeAuthorityDrift, "set-role", "cannot assume the dedicated migration role", err)
	}
	settings := []struct{ name, value string }{
		{"client_encoding", "UTF8"},
		{"standard_conforming_strings", "on"},
		{"TimeZone", "UTC"},
		{"search_path", "pg_catalog"},
		{"statement_timeout", strconv.FormatUint(policy.StatementTimeoutMS, 10) + "ms"},
		{"lock_timeout", strconv.FormatUint(policy.LockTimeoutMS, 10) + "ms"},
		{"idle_in_transaction_session_timeout", strconv.FormatUint(policy.IdleInTransactionSessionTimeoutMS, 10) + "ms"},
	}
	for _, setting := range settings {
		var readback string
		if err := session.connection.QueryRow(ctx, "SELECT pg_catalog.set_config($1, $2, false)", setting.name, setting.value).Scan(&readback); err != nil {
			return fail(CodeTransactionBoundary, "session-settings", "cannot set a required session setting", err)
		}
	}
	var currentUser, encoding, conforming, timezone, searchPath string
	var statementTimeoutMS, lockTimeoutMS, idleTimeoutMS int64
	if err := session.connection.QueryRow(ctx, `SELECT current_user,
current_setting('client_encoding'), current_setting('standard_conforming_strings'),
current_setting('TimeZone'), current_setting('search_path'),
(SELECT setting::bigint FROM pg_catalog.pg_settings WHERE name = 'statement_timeout' AND unit = 'ms'),
(SELECT setting::bigint FROM pg_catalog.pg_settings WHERE name = 'lock_timeout' AND unit = 'ms'),
(SELECT setting::bigint FROM pg_catalog.pg_settings WHERE name = 'idle_in_transaction_session_timeout' AND unit = 'ms')`).Scan(
		&currentUser, &encoding, &conforming, &timezone, &searchPath,
		&statementTimeoutMS, &lockTimeoutMS, &idleTimeoutMS,
	); err != nil {
		return fail(CodeTransactionBoundary, "session-settings", "cannot read back required session settings", err)
	}
	if currentUser != MigrationOwnerRole || encoding != "UTF8" || conforming != "on" || timezone != "UTC" || searchPath != "pg_catalog" || statementTimeoutMS != int64(policy.StatementTimeoutMS) || lockTimeoutMS != int64(policy.LockTimeoutMS) || idleTimeoutMS != int64(policy.IdleInTransactionSessionTimeoutMS) {
		return fail(CodeTransactionBoundary, "session-settings", "required role or settings did not survive exact readback", nil)
	}
	return nil
}

func (session *pgxSession) AcquireAdvisoryLock(ctx context.Context, key int64) error {
	var acquired any
	if err := session.connection.QueryRow(ctx, "SELECT pg_catalog.pg_advisory_lock($1)", key).Scan(&acquired); err != nil {
		return fail(CodeLockLost, "advisory-lock", "cannot acquire session advisory lock", err)
	}
	state, err := session.Boundary(ctx, key)
	if err != nil {
		return err
	}
	if !state.LockHeld {
		return fail(CodeLockLost, "advisory-lock", "lock was not visible after acquisition", nil)
	}
	return nil
}

func (session *pgxSession) Boundary(ctx context.Context, key int64) (BoundaryState, error) {
	state := BoundaryState{TxStatus: session.connection.PgConn().TxStatus()}
	if err := session.connection.QueryRow(ctx, boundaryQuery, key).Scan(&state.CurrentUser, &state.LockHeld); err != nil {
		return BoundaryState{}, fail(CodeTransactionBoundary, "boundary", "cannot read current user or advisory lock", err)
	}
	return state, nil
}

func (session *pgxSession) BeginMigration(ctx context.Context) (MigrationTransaction, error) {
	tx, err := session.connection.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable, AccessMode: pgx.ReadWrite, DeferrableMode: pgx.NotDeferrable})
	if err != nil {
		return nil, fail(CodeTransactionBoundary, "begin", "cannot begin serializable migration transaction", err)
	}
	return &pgxMigrationTx{tx: tx}, nil
}

func (session *pgxSession) UnlockAndReset(ctx context.Context, key int64) error {
	var unlocked bool
	if err := session.connection.QueryRow(ctx, "SELECT pg_catalog.pg_advisory_unlock($1)", key).Scan(&unlocked); err != nil || !unlocked {
		return fail(CodeLockLost, "unlock", "cannot release exact advisory lock", err)
	}
	if _, err := session.connection.Exec(ctx, "RESET ROLE"); err != nil {
		return fail(CodeAuthorityDrift, "reset-role", "cannot reset migration role", err)
	}
	var same bool
	if err := session.connection.QueryRow(ctx, "SELECT current_user = session_user").Scan(&same); err != nil || !same {
		return fail(CodeAuthorityDrift, "reset-role", "current_user did not return to session_user", err)
	}
	return nil
}

func (session *pgxSession) Close(ctx context.Context) error { return session.connection.Close(ctx) }

type pgxMigrationTx struct{ tx pgx.Tx }

func (transaction *pgxMigrationTx) Query(ctx context.Context, sql string, args ...any) (Rows, error) {
	rows, err := transaction.tx.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	return pgxRows{rows: rows}, nil
}

func (transaction *pgxMigrationTx) QueryRow(ctx context.Context, sql string, args ...any) Row {
	return transaction.tx.QueryRow(ctx, sql, args...)
}

func (transaction *pgxMigrationTx) Exec(ctx context.Context, sql string, args ...any) (CommandTag, error) {
	return transaction.tx.Exec(ctx, sql, args...)
}

func (transaction *pgxMigrationTx) ExecuteStatement(ctx context.Context, raw []byte) error {
	result := transaction.tx.Conn().PgConn().ExecParams(ctx, string(raw), nil, nil, nil, nil)
	if outcome := result.Read(); outcome.Err != nil {
		return fail(CodeInvalidSQL, "execute", "PostgreSQL rejected the extended-protocol statement", outcome.Err)
	}
	return nil
}

func (transaction *pgxMigrationTx) Boundary(ctx context.Context, key int64) (BoundaryState, error) {
	state := BoundaryState{TxStatus: transaction.tx.Conn().PgConn().TxStatus()}
	if err := transaction.tx.QueryRow(ctx, boundaryQuery, key).Scan(&state.CurrentUser, &state.LockHeld); err != nil {
		return BoundaryState{}, fail(CodeTransactionBoundary, "boundary", "cannot read transaction boundary", err)
	}
	return state, nil
}

func (transaction *pgxMigrationTx) Commit(ctx context.Context) error {
	return transaction.tx.Commit(ctx)
}
func (transaction *pgxMigrationTx) Rollback(ctx context.Context) error {
	return transaction.tx.Rollback(ctx)
}

const boundaryQuery = `SELECT current_user, EXISTS (
  SELECT 1 FROM pg_catalog.pg_locks
  WHERE locktype = 'advisory' AND pid = pg_catalog.pg_backend_pid()
    AND classid = (($1::bigint >> 32) & 4294967295)::oid
    AND objid = ($1::bigint & 4294967295)::oid
    AND objsubid = 1 AND granted
)`

type pgxQuery interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

type pgxQueryer struct{ queryer pgxQuery }

func (queryer pgxQueryer) Query(ctx context.Context, sql string, args ...any) (Rows, error) {
	rows, err := queryer.queryer.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	return pgxRows{rows: rows}, nil
}

func (queryer pgxQueryer) QueryRow(ctx context.Context, sql string, args ...any) Row {
	return queryer.queryer.QueryRow(ctx, sql, args...)
}

type pgxRows struct{ rows pgx.Rows }

func (rows pgxRows) Next() bool                { return rows.rows.Next() }
func (rows pgxRows) Scan(targets ...any) error { return rows.rows.Scan(targets...) }
func (rows pgxRows) Err() error                { return rows.rows.Err() }
func (rows pgxRows) Close()                    { rows.rows.Close() }

var _ CommandTag = pgconn.CommandTag{}

func describePGXError(err error) string {
	if pgError, ok := err.(*pgconn.PgError); ok {
		return fmt.Sprintf("SQLSTATE %s", pgError.Code)
	}
	return "PostgreSQL operation failed"
}
