//go:build localdev

// Package localmigration provides a deliberately local-development-only
// migration path for a pre-provisioned PostgreSQL database. It does not create
// databases or roles and it has no partial-ledger recovery behavior.
package localmigration

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/migration"
)

type Config struct {
	DatabaseURL    string
	RepositoryRoot string
	ManifestPath   string
}

type Result struct {
	SchemaHead string `json:"schema_head"`
	Applied    int    `json:"applied"`
	NoOp       bool   `json:"no_op"`
}

type Connector interface {
	Connect(context.Context, string) (Session, error)
}

type Session interface {
	SetMigrationRole(context.Context) error
	AcquireAdvisoryLock(context.Context, int64) error
	ReadLedger(context.Context) ([]migration.LedgerRow, error)
	Apply(context.Context, migration.MigrationEntry, []byte, migration.Digest) error
	ReleaseAdvisoryLock(context.Context, int64) error
	Close(context.Context) error
}

type loadedBundle struct {
	manifest *migration.Manifest
	sql      map[string][]byte
}

func Run(ctx context.Context, config Config, connector Connector) (result Result, err error) {
	if connector == nil {
		return Result{}, errors.New("local migration connector is required")
	}
	if strings.TrimSpace(config.DatabaseURL) == "" {
		return Result{}, errors.New("local migration database URL is required")
	}
	bundle, err := loadAndVerify(config)
	if err != nil {
		return Result{}, err
	}
	result.SchemaHead = bundle.manifest.SchemaBundle.SchemaHead

	session, err := connector.Connect(ctx, config.DatabaseURL)
	if err != nil {
		return Result{}, errors.New("local PostgreSQL connection failed")
	}
	defer func() {
		if closeErr := session.Close(ctx); err == nil && closeErr != nil {
			err = errors.New("local PostgreSQL session close failed")
		}
	}()
	if err := session.SetMigrationRole(ctx); err != nil {
		return Result{}, fmt.Errorf("assume migration owner role: %w", err)
	}
	key, err := bundle.manifest.SchemaBundle.AdvisoryLock.Key()
	if err != nil {
		return Result{}, err
	}
	if err := session.AcquireAdvisoryLock(ctx, key); err != nil {
		return Result{}, fmt.Errorf("acquire manifest advisory lock: %w", err)
	}
	locked := true
	defer func() {
		if !locked {
			return
		}
		if unlockErr := session.ReleaseAdvisoryLock(ctx, key); err == nil && unlockErr != nil {
			err = fmt.Errorf("release manifest advisory lock: %w", unlockErr)
		}
	}()

	rows, err := session.ReadLedger(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("read migration ledger: %w", err)
	}
	entries := bundle.manifest.SchemaBundle.Migrations
	expectedLength, supportedHead := supportedManifestLength(bundle.manifest.SchemaBundle.SchemaHead)
	if !supportedHead || len(entries) != expectedLength || len(entries) == 0 || bundle.manifest.SchemaBundle.SchemaHead != entries[len(entries)-1].ID {
		return Result{}, errors.New("localdev runner requires a supported closed manifest schema head")
	}
	for index, entry := range entries {
		if entry.ID != fmt.Sprintf("%06d", index+1) {
			return Result{}, errors.New("localdev runner requires contiguous supported migrations from 000001")
		}
	}
	switch {
	case len(rows) == 0:
		for _, entry := range entries {
			if err := session.Apply(ctx, entry, bundle.sql[entry.ID], bundle.manifest.SchemaBundleDigest); err != nil {
				return Result{}, fmt.Errorf("apply migration %s: %w", entry.ID, err)
			}
			result.Applied++
		}
		return result, nil
	case len(rows) != len(entries):
		return Result{}, errors.New("partial migration ledger is not supported by localdev runner")
	default:
		for index := range rows {
			if !ledgerRowMatches(rows[index], entries[index], bundle.manifest.SchemaBundleDigest) {
				return Result{}, fmt.Errorf("migration ledger differs from manifest at index %d", index)
			}
		}
		result.NoOp = true
		return result, nil
	}
}

func supportedManifestLength(schemaHead string) (int, bool) {
	switch schemaHead {
	case "000013":
		return 13, true
	case "000014":
		return 14, true
	default:
		return 0, false
	}
}

func loadAndVerify(config Config) (*loadedBundle, error) {
	if strings.TrimSpace(config.RepositoryRoot) == "" || strings.TrimSpace(config.ManifestPath) == "" {
		return nil, errors.New("repository root and manifest path are required")
	}
	root, err := filepath.Abs(config.RepositoryRoot)
	if err != nil {
		return nil, errors.New("repository root is invalid")
	}
	manifestPath, err := resolveWithin(root, config.ManifestPath)
	if err != nil {
		return nil, fmt.Errorf("manifest path: %w", err)
	}
	manifestInfo, err := os.Lstat(manifestPath)
	if err != nil || !manifestInfo.Mode().IsRegular() {
		return nil, errors.New("manifest is unavailable")
	}
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, errors.New("manifest cannot be read")
	}
	manifest, _, err := migration.DecodeManifest(raw)
	if err != nil {
		return nil, fmt.Errorf("decode manifest: %w", err)
	}
	files := make(map[string][]byte, len(manifest.SchemaBundle.Migrations))
	for _, entry := range manifest.SchemaBundle.Migrations {
		if entry.TransactionMode != "transactional" {
			return nil, fmt.Errorf("migration %s is not transactional", entry.ID)
		}
		artifactPath, err := resolveWithin(root, entry.SQLArtifact.Path)
		if err != nil {
			return nil, fmt.Errorf("migration %s path: %w", entry.ID, err)
		}
		info, err := os.Lstat(artifactPath)
		if err != nil || !info.Mode().IsRegular() {
			return nil, fmt.Errorf("migration %s SQL artifact is unavailable", entry.ID)
		}
		if info.Mode().Perm() != 0o644 || entry.SQLArtifact.Mode != "100644" {
			return nil, fmt.Errorf("migration %s SQL artifact mode differs from manifest", entry.ID)
		}
		data, err := os.ReadFile(artifactPath)
		if err != nil {
			return nil, fmt.Errorf("migration %s SQL artifact cannot be read", entry.ID)
		}
		if uint64(len(data)) != entry.SQLArtifact.SizeBytes || migration.DigestBytes(data) != entry.SQLArtifact.SHA256 {
			return nil, fmt.Errorf("migration %s SQL artifact differs from manifest", entry.ID)
		}
		files[entry.ID] = data
	}
	return &loadedBundle{manifest: manifest, sql: files}, nil
}

func resolveWithin(root, candidate string) (string, error) {
	if filepath.IsAbs(candidate) {
		return "", errors.New("absolute paths are not allowed")
	}
	clean := filepath.Clean(filepath.FromSlash(candidate))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", errors.New("path escapes repository root")
	}
	resolved := filepath.Join(root, clean)
	relative, err := filepath.Rel(root, resolved)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", errors.New("path escapes repository root")
	}
	return resolved, nil
}

func ledgerRowMatches(row migration.LedgerRow, entry migration.MigrationEntry, bundle migration.Digest) bool {
	return row.MigrationID == entry.ID &&
		row.MigrationName == entry.Name &&
		equalOptional(row.PredecessorID, entry.PredecessorID) &&
		row.Phase == entry.Phase && row.SchemaFrom == entry.SchemaFrom && row.SchemaTo == entry.SchemaTo &&
		row.CompatibleBinaryMin == entry.CompatibleControlPlaneMin && row.CompatibleBinaryMax == entry.CompatibleControlPlaneMax &&
		row.SQLPath == entry.SQLArtifact.Path && row.SQLSizeBytes == int64(entry.SQLArtifact.SizeBytes) && row.SQLSHA256 == entry.SQLArtifact.SHA256 &&
		row.BundleDigest == bundle && row.TransactionMode == entry.TransactionMode && row.Reentrancy == entry.Reentrancy &&
		row.RollbackBoundary == entry.RollbackBoundary && row.RequiresLiveInstancePreflight == entry.RequiresLiveInstancePreflight &&
		row.RequiresPITRPreflight == entry.RequiresPITRPreflight && !row.AppliedAt.IsZero() && strings.TrimSpace(row.AppliedBy) != ""
}

func equalOptional(left, right *string) bool {
	return left == nil && right == nil || left != nil && right != nil && *left == *right
}

func ledgerRow(entry migration.MigrationEntry, bundle migration.Digest, appliedBy string) migration.LedgerRow {
	return migration.LedgerRow{
		MigrationID: entry.ID, MigrationName: entry.Name, PredecessorID: entry.PredecessorID,
		Phase: entry.Phase, SchemaFrom: entry.SchemaFrom, SchemaTo: entry.SchemaTo,
		CompatibleBinaryMin: entry.CompatibleControlPlaneMin, CompatibleBinaryMax: entry.CompatibleControlPlaneMax,
		SQLPath: entry.SQLArtifact.Path, SQLSizeBytes: int64(entry.SQLArtifact.SizeBytes), SQLSHA256: entry.SQLArtifact.SHA256,
		BundleDigest: bundle, TransactionMode: entry.TransactionMode, Reentrancy: entry.Reentrancy,
		RollbackBoundary: entry.RollbackBoundary, RequiresLiveInstancePreflight: entry.RequiresLiveInstancePreflight,
		RequiresPITRPreflight: entry.RequiresPITRPreflight, AppliedAt: time.Now().UTC(), AppliedBy: appliedBy,
	}
}
