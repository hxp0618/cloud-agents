package localmigration

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/migration"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type PGXConnector struct{}

func (PGXConnector) Connect(ctx context.Context, databaseURL string) (Session, error) {
	config, err := parseLocalPGXConfig(databaseURL)
	if err != nil {
		return nil, err
	}
	connection, err := pgx.ConnectConfig(ctx, config)
	if err != nil {
		return nil, errors.New("cannot connect to PostgreSQL")
	}
	return &pgxSession{connection: connection}, nil
}

// ProductPGXConnector is the production connector. TLS and network policy
// remain PostgreSQL DSN concerns; the runner still requires the dedicated
// migration-owner role before it can execute any SQL.
type ProductPGXConnector struct{}

func (ProductPGXConnector) Connect(ctx context.Context, databaseURL string) (Session, error) {
	config, err := pgx.ParseConfig(databaseURL)
	if err != nil {
		return nil, errors.New("invalid PostgreSQL configuration")
	}
	connection, err := pgx.ConnectConfig(ctx, config)
	if err != nil {
		return nil, errors.New("cannot connect to PostgreSQL")
	}
	return &pgxSession{connection: connection}, nil
}

const productSchemaReadinessSQL = `SELECT
 count(*)::bigint,
 COALESCE(min(migration_id), ''),
 COALESCE(max(migration_id), ''),
 COALESCE((SELECT bundle_digest FROM cloud_agents.schema_migrations WHERE migration_id = $1), '')
FROM cloud_agents.schema_migrations`

// CheckProductSchemaReadiness verifies that the runtime database is exactly at
// the current independent-product migration head.
func CheckProductSchemaReadiness(ctx context.Context, queryer interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}) error {
	current := productRunnerBindingSelector("000041")
	var count int64
	var first, last, bundleDigest string
	if err := queryer.QueryRow(ctx, productSchemaReadinessSQL, current.schemaHead).Scan(&count, &first, &last, &bundleDigest); err != nil {
		return errors.New("product schema is unavailable")
	}
	if count != int64(current.migrationCount) || first != "000001" || last != current.schemaHead || bundleDigest != current.schemaBundleDigest {
		return errors.New("product schema is not current")
	}
	return nil
}

func parseLocalPGXConfig(databaseURL string) (*pgx.ConnConfig, error) {
	config, err := pgx.ParseConfig(databaseURL)
	if err != nil {
		return nil, errors.New("invalid PostgreSQL configuration")
	}
	if !isLocalPostgreSQLHost(config.Host) {
		return nil, errors.New("localdev migration refuses a non-loopback PostgreSQL host")
	}
	for _, fallback := range config.Fallbacks {
		if fallback == nil || !isLocalPostgreSQLHost(fallback.Host) {
			return nil, errors.New("localdev migration refuses a non-loopback PostgreSQL host")
		}
	}
	return config, nil
}

func isLocalPostgreSQLHost(host string) bool {
	if strings.HasPrefix(host, string(os.PathSeparator)) {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

type pgxSession struct {
	connection *pgx.Conn
	locked     bool
}

func (session *pgxSession) SetMigrationRole(ctx context.Context) error {
	if _, err := session.connection.Exec(ctx, "SET ROLE cloud_agents_migration_owner"); err != nil {
		return errors.New("dedicated migration owner role is unavailable")
	}
	var currentUser string
	if err := session.connection.QueryRow(ctx, "SELECT current_user").Scan(&currentUser); err != nil || currentUser != migration.MigrationOwnerRole {
		return errors.New("dedicated migration owner role readback failed")
	}
	return nil
}

func (session *pgxSession) AcquireAdvisoryLock(ctx context.Context, key int64) error {
	if session.locked {
		return errors.New("advisory lock already held")
	}
	if _, err := session.connection.Exec(ctx, "SELECT pg_catalog.pg_advisory_lock($1)", key); err != nil {
		return errors.New("advisory lock acquisition failed")
	}
	var held bool
	if err := session.connection.QueryRow(ctx, `SELECT EXISTS (
 SELECT 1 FROM pg_catalog.pg_locks
 WHERE locktype = 'advisory' AND pid = pg_catalog.pg_backend_pid()
   AND classid = (($1::bigint >> 32) & 4294967295)::oid
   AND objid = ($1::bigint & 4294967295)::oid
   AND objsubid = 1 AND granted
)`, key).Scan(&held); err != nil || !held {
		return errors.New("advisory lock readback failed")
	}
	session.locked = true
	return nil
}

func (session *pgxSession) ReadLedger(ctx context.Context) ([]migration.LedgerRow, error) {
	var schemaExists, ledgerExists bool
	if err := session.connection.QueryRow(ctx, `SELECT EXISTS (
 SELECT 1 FROM pg_catalog.pg_namespace WHERE nspname = 'cloud_agents'
), EXISTS (
 SELECT 1 FROM pg_catalog.pg_class relation
 JOIN pg_catalog.pg_namespace namespace_row ON namespace_row.oid = relation.relnamespace
 WHERE namespace_row.nspname = 'cloud_agents' AND relation.relname = 'schema_migrations'
)`).Scan(&schemaExists, &ledgerExists); err != nil {
		return nil, errors.New("cannot inspect local migration schema")
	}
	if !schemaExists {
		return []migration.LedgerRow{}, nil
	}
	if !ledgerExists {
		var objects int64
		if err := session.connection.QueryRow(ctx, `SELECT count(*) FROM pg_catalog.pg_class relation
JOIN pg_catalog.pg_namespace namespace_row ON namespace_row.oid = relation.relnamespace
WHERE namespace_row.nspname = 'cloud_agents'`).Scan(&objects); err != nil {
			return nil, errors.New("cannot inspect local migration schema objects")
		}
		if objects != 0 {
			return nil, errors.New("cloud_agents schema exists without migration ledger")
		}
		return []migration.LedgerRow{}, nil
	}
	rows, err := (migration.SQLLedgerStore{}).Read(ctx, queryAdapter{queryer: session.connection})
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, errors.New("migration ledger table exists but is empty")
	}
	return rows, nil
}

func (session *pgxSession) Apply(ctx context.Context, entry migration.MigrationEntry, sql []byte, bundle migration.Digest) error {
	if !session.locked {
		return errors.New("manifest advisory lock is not held")
	}
	transaction, err := session.connection.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return errors.New("migration transaction cannot begin")
	}
	committed := false
	defer func() {
		if !committed {
			_ = transaction.Rollback(context.Background())
		}
	}()

	results, err := transaction.Conn().PgConn().Exec(ctx, string(sql)).ReadAll()
	if err != nil {
		return errors.New("PostgreSQL rejected migration SQL")
	}
	for _, result := range results {
		if result.Err != nil {
			return errors.New("PostgreSQL rejected migration SQL")
		}
	}
	if err := (migration.SQLLedgerStore{}).Insert(ctx, execAdapter{executor: transaction}, entry, bundle); err != nil {
		return fmt.Errorf("insert migration ledger row: %w", err)
	}
	if err := transaction.Commit(ctx); err != nil {
		return errors.New("migration transaction commit failed")
	}
	committed = true
	return nil
}

func (session *pgxSession) ReleaseAdvisoryLock(ctx context.Context, key int64) error {
	if !session.locked {
		return errors.New("advisory lock is not held")
	}
	var released bool
	if err := session.connection.QueryRow(ctx, "SELECT pg_catalog.pg_advisory_unlock($1)", key).Scan(&released); err != nil || !released {
		return errors.New("advisory lock release failed")
	}
	session.locked = false
	return nil
}

func (session *pgxSession) Close(ctx context.Context) error {
	return session.connection.Close(ctx)
}

type pgxQuery interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

type queryAdapter struct{ queryer pgxQuery }

func (adapter queryAdapter) Query(ctx context.Context, sql string, arguments ...any) (migration.Rows, error) {
	rows, err := adapter.queryer.Query(ctx, sql, arguments...)
	if err != nil {
		return nil, err
	}
	return rowsAdapter{rows: rows}, nil
}

func (adapter queryAdapter) QueryRow(ctx context.Context, sql string, arguments ...any) migration.Row {
	return adapter.queryer.QueryRow(ctx, sql, arguments...)
}

type rowsAdapter struct{ rows pgx.Rows }

func (adapter rowsAdapter) Next() bool                { return adapter.rows.Next() }
func (adapter rowsAdapter) Scan(targets ...any) error { return adapter.rows.Scan(targets...) }
func (adapter rowsAdapter) Err() error                { return adapter.rows.Err() }
func (adapter rowsAdapter) Close()                    { adapter.rows.Close() }

type pgxExecutor interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

type execAdapter struct{ executor pgxExecutor }

func (adapter execAdapter) Exec(ctx context.Context, sql string, arguments ...any) (migration.CommandTag, error) {
	return adapter.executor.Exec(ctx, sql, arguments...)
}

var _ Connector = PGXConnector{}
var _ Connector = ProductPGXConnector{}
var _ Session = (*pgxSession)(nil)
