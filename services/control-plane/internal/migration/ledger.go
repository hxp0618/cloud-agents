package migration

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

type LedgerSnapshot struct {
	Rows []LedgerRow
	Head string
}

func ValidateLedger(rows []LedgerRow, lineage *SchemaLineage) (*LedgerSnapshot, error) {
	current := lineage.Current.SchemaBundle.Migrations
	if len(rows) > len(current) {
		return nil, fail(CodeInvalidLedger, "ledger", "ledger is longer than the current migration list", nil)
	}
	previousBundleIndex := -1
	for index, row := range rows {
		if row.MigrationID != current[index].ID {
			return nil, fail(CodeInvalidLedger, row.MigrationID, "ledger is not a continuous current migration prefix", nil)
		}
		bundleIndex, ok := lineage.BundleIndex(row.BundleDigest)
		if !ok {
			return nil, fail(CodeInvalidLedger, row.MigrationID, "ledger references an unknown schema bundle", nil)
		}
		if bundleIndex < previousBundleIndex {
			return nil, fail(CodeInvalidLedger, row.MigrationID, "ledger bundle chain moves backward", nil)
		}
		entry, err := lineage.EntryForDigest(row.BundleDigest, row.MigrationID)
		if err != nil {
			return nil, err
		}
		if err := validateLedgerRow(row, *entry); err != nil {
			return nil, err
		}
		previousBundleIndex = bundleIndex
	}
	snapshot := &LedgerSnapshot{Rows: rows}
	if len(rows) > 0 {
		snapshot.Head = rows[len(rows)-1].MigrationID
	}
	return snapshot, nil
}

func validateLedgerRow(row LedgerRow, entry MigrationEntry) error {
	if row.MigrationName != entry.Name || !equalOptionalString(row.PredecessorID, entry.PredecessorID) || row.Phase != entry.Phase || row.SchemaFrom != entry.SchemaFrom || row.SchemaTo != entry.SchemaTo || row.CompatibleBinaryMin != entry.CompatibleControlPlaneMin || row.CompatibleBinaryMax != entry.CompatibleControlPlaneMax || row.SQLPath != entry.SQLArtifact.Path || row.SQLSizeBytes != int64(entry.SQLArtifact.SizeBytes) || row.SQLSHA256 != entry.SQLArtifact.SHA256 || row.TransactionMode != entry.TransactionMode || row.Reentrancy != entry.Reentrancy || row.RollbackBoundary != entry.RollbackBoundary || row.RequiresLiveInstancePreflight != entry.RequiresLiveInstancePreflight || row.RequiresPITRPreflight != entry.RequiresPITRPreflight {
		return fail(CodeInvalidLedger, row.MigrationID, "ledger-backed identity differs from its signed bundle entry", nil)
	}
	return nil
}

func equalOptionalString(left, right *string) bool {
	return left == nil && right == nil || left != nil && right != nil && *left == *right
}

type LedgerStore interface {
	Read(context.Context, Queryer) ([]LedgerRow, error)
	Insert(context.Context, CommandExecutor, MigrationEntry, Digest) error
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
