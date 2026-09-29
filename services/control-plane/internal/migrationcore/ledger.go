package migrationcore

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

type LedgerRow struct {
	MigrationID                   string
	MigrationName                 string
	PredecessorID                 *string
	Phase                         string
	SchemaFrom                    string
	SchemaTo                      string
	CompatibleBinaryMin           string
	CompatibleBinaryMax           string
	SQLPath                       string
	SQLSizeBytes                  int64
	SQLSHA256                     Digest
	BundleDigest                  Digest
	TransactionMode               string
	Reentrancy                    string
	RollbackBoundary              string
	RequiresLiveInstancePreflight bool
	RequiresPITRPreflight         bool
	AppliedAt                     time.Time
	AppliedBy                     string
}

type CommandExecutor interface {
	Exec(context.Context, string, ...any) (CommandTag, error)
}

type CommandTag interface{ RowsAffected() int64 }

// SQLLedgerStore uses only parameterized SQL and exact columns frozen by 000001.
type SQLLedgerStore struct{}

func (SQLLedgerStore) Read(ctx context.Context, queryer Queryer) ([]LedgerRow, error) {
	rows, err := queryer.Query(ctx, `
SELECT migration_id, migration_name, predecessor_id, phase, schema_from, schema_to,
       compatible_binary_min, compatible_binary_max, sql_path, sql_size_bytes,
       sql_sha256, bundle_digest, transaction_mode, reentrancy, rollback_boundary,
       requires_live_instance_preflight, requires_pitr_preflight, applied_at, applied_by
FROM cloud_agents.schema_migrations
ORDER BY migration_id`)
	if err != nil {
		var pgError *pgconn.PgError
		if errors.As(err, &pgError) && pgError.Code == "42P01" {
			return []LedgerRow{}, nil
		}
		return nil, fail(CodeInvalidLedger, "read", "cannot read migration ledger", err)
	}
	defer rows.Close()
	result := make([]LedgerRow, 0)
	for rows.Next() {
		var row LedgerRow
		var sqlDigestText, bundleDigestText string
		if err := rows.Scan(
			&row.MigrationID, &row.MigrationName, &row.PredecessorID, &row.Phase,
			&row.SchemaFrom, &row.SchemaTo, &row.CompatibleBinaryMin, &row.CompatibleBinaryMax,
			&row.SQLPath, &row.SQLSizeBytes, &sqlDigestText, &bundleDigestText,
			&row.TransactionMode, &row.Reentrancy, &row.RollbackBoundary,
			&row.RequiresLiveInstancePreflight, &row.RequiresPITRPreflight,
			&row.AppliedAt, &row.AppliedBy,
		); err != nil {
			return nil, fail(CodeInvalidLedger, "read", "cannot decode migration ledger", err)
		}
		row.SQLSHA256, err = ParseDigest(sqlDigestText)
		if err != nil {
			return nil, fail(CodeInvalidLedger, row.MigrationID, "ledger SQL digest is invalid", err)
		}
		row.BundleDigest, err = ParseDigest(bundleDigestText)
		if err != nil {
			return nil, fail(CodeInvalidLedger, row.MigrationID, "ledger bundle digest is invalid", err)
		}
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		var pgError *pgconn.PgError
		if len(result) == 0 && errors.As(err, &pgError) && pgError.Code == "42P01" {
			return []LedgerRow{}, nil
		}
		return nil, fail(CodeInvalidLedger, "read", "migration ledger stream failed", err)
	}
	return result, nil
}

func (SQLLedgerStore) Insert(ctx context.Context, executor CommandExecutor, entry MigrationEntry, bundleDigest Digest) error {
	tag, err := executor.Exec(ctx, `
INSERT INTO cloud_agents.schema_migrations (
  migration_id, migration_name, predecessor_id, phase, schema_from, schema_to,
  compatible_binary_min, compatible_binary_max, sql_path, sql_size_bytes,
  sql_sha256, bundle_digest, transaction_mode, reentrancy, rollback_boundary,
  requires_live_instance_preflight, requires_pitr_preflight
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)`,
		entry.ID, entry.Name, entry.PredecessorID, entry.Phase, entry.SchemaFrom, entry.SchemaTo,
		entry.CompatibleControlPlaneMin, entry.CompatibleControlPlaneMax, entry.SQLArtifact.Path,
		entry.SQLArtifact.SizeBytes, entry.SQLArtifact.SHA256.String(), bundleDigest.String(), entry.TransactionMode,
		entry.Reentrancy, entry.RollbackBoundary, entry.RequiresLiveInstancePreflight,
		entry.RequiresPITRPreflight,
	)
	if err != nil {
		return fail(CodeInvalidLedger, entry.ID, "ledger insert failed", err)
	}
	if tag.RowsAffected() != 1 {
		return fail(CodeInvalidLedger, entry.ID, fmt.Sprintf("ledger insert affected %d rows", tag.RowsAffected()), nil)
	}
	return nil
}

// Queryer is the minimal catalog-query boundary implemented by pgx.Conn and pgx.Tx adapters.
type Queryer interface {
	Query(context.Context, string, ...any) (Rows, error)
	QueryRow(context.Context, string, ...any) Row
}

type Rows interface {
	Next() bool
	Scan(...any) error
	Err() error
	Close()
}

type Row interface{ Scan(...any) error }
