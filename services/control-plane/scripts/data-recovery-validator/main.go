package main

import (
	"bufio"
	"crypto/sha256"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/migration"
)

const (
	modeLedgerTSV = "ledger-tsv"
	modeApplySQL  = "apply-sql"
)

type recoveryPlan struct {
	manifest *migration.Manifest
	repoRoot string
}

func main() {
	manifestPath := flag.String("manifest", "", "path to the checked-in migration manifest")
	repoRoot := flag.String("repo-root", "", "repository root used to verify migration artifacts")
	mode := flag.String("mode", "", "output mode: ledger-tsv or apply-sql")
	containerRepoRoot := flag.String("container-repo-root", "", "read-only repository mount used by apply-sql")
	containerLedgerPath := flag.String("container-ledger-path", "", "ledger TSV path used by apply-sql")
	flag.Parse()
	if flag.NArg() != 0 || *manifestPath == "" || *repoRoot == "" || (*mode != modeLedgerTSV && *mode != modeApplySQL) {
		usage()
	}
	if *mode == modeApplySQL && (!validAbsoluteContainerPath(*containerRepoRoot) || !validAbsoluteContainerPath(*containerLedgerPath)) {
		usage()
	}
	plan, err := loadRecoveryPlan(*manifestPath, *repoRoot)
	if err != nil {
		fmt.Fprintf(os.Stderr, "data recovery plan validation failed: %v\n", err)
		os.Exit(1)
	}
	writer := bufio.NewWriter(os.Stdout)
	switch *mode {
	case modeLedgerTSV:
		err = plan.writeLedgerTSV(writer)
	case modeApplySQL:
		err = plan.writeApplySQL(writer, *containerRepoRoot, *containerLedgerPath)
	}
	if closeErr := writer.Flush(); err == nil {
		err = closeErr
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "data recovery plan output failed: %v\n", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: data-recovery-validator --manifest PATH --repo-root PATH --mode ledger-tsv|apply-sql [--container-repo-root /PATH --container-ledger-path /PATH]")
	os.Exit(2)
}

func loadRecoveryPlan(manifestPath, repoRoot string) (*recoveryPlan, error) {
	root, err := filepath.Abs(repoRoot)
	if err != nil {
		return nil, fmt.Errorf("resolve repository root: %w", err)
	}
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("read migration manifest: %w", err)
	}
	manifest, _, err := migration.DecodeManifest(raw)
	if err != nil {
		return nil, fmt.Errorf("decode migration manifest: %w", err)
	}
	if len(manifest.SchemaBundle.Migrations) == 0 || manifest.SchemaBundle.SchemaHead != manifest.SchemaBundle.Migrations[len(manifest.SchemaBundle.Migrations)-1].ID {
		return nil, errors.New("migration manifest does not contain a closed lineage")
	}
	for _, entry := range manifest.SchemaBundle.Migrations {
		if err := validatePlainField(entry.ID, entry.Name, entry.Phase, entry.SchemaFrom, entry.SchemaTo,
			entry.CompatibleControlPlaneMin, entry.CompatibleControlPlaneMax, entry.SQLArtifact.Path,
			entry.TransactionMode, entry.Reentrancy, entry.RollbackBoundary); err != nil {
			return nil, fmt.Errorf("migration %s: %w", entry.ID, err)
		}
		if entry.PredecessorID != nil {
			if err := validatePlainField(*entry.PredecessorID); err != nil {
				return nil, fmt.Errorf("migration %s predecessor: %w", entry.ID, err)
			}
		}
		path := filepath.Join(root, filepath.FromSlash(entry.SQLArtifact.Path))
		if !pathWithin(root, path) {
			return nil, fmt.Errorf("migration %s artifact escapes repository root", entry.ID)
		}
		contents, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read migration %s artifact: %w", entry.ID, err)
		}
		actualDigest := fmt.Sprintf("sha256:%x", sha256.Sum256(contents))
		if uint64(len(contents)) != entry.SQLArtifact.SizeBytes || actualDigest != entry.SQLArtifact.SHA256.String() {
			return nil, fmt.Errorf("migration %s artifact size or digest drift", entry.ID)
		}
	}
	return &recoveryPlan{manifest: manifest, repoRoot: root}, nil
}

func (plan *recoveryPlan) writeLedgerTSV(writer io.Writer) error {
	for _, entry := range plan.manifest.SchemaBundle.Migrations {
		predecessor := `\N`
		if entry.PredecessorID != nil {
			predecessor = *entry.PredecessorID
		}
		fields := []string{
			entry.ID,
			entry.Name,
			predecessor,
			entry.Phase,
			entry.SchemaFrom,
			entry.SchemaTo,
			entry.CompatibleControlPlaneMin,
			entry.CompatibleControlPlaneMax,
			entry.SQLArtifact.Path,
			strconv.FormatUint(entry.SQLArtifact.SizeBytes, 10),
			entry.SQLArtifact.SHA256.String(),
			plan.manifest.SchemaBundleDigest.String(),
			entry.TransactionMode,
			entry.Reentrancy,
			entry.RollbackBoundary,
			postgresBool(entry.RequiresLiveInstancePreflight),
			postgresBool(entry.RequiresPITRPreflight),
		}
		if _, err := fmt.Fprintln(writer, strings.Join(fields, "\t")); err != nil {
			return err
		}
	}
	return nil
}

func (plan *recoveryPlan) writeApplySQL(writer io.Writer, containerRepoRoot, containerLedgerPath string) error {
	if _, err := fmt.Fprintln(writer, `\set ON_ERROR_STOP on`); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(writer, "SET ROLE cloud_agents_migration_owner;"); err != nil {
		return err
	}
	for _, entry := range plan.manifest.SchemaBundle.Migrations {
		artifactPath := strings.TrimRight(containerRepoRoot, "/") + "/" + entry.SQLArtifact.Path
		if !validAbsoluteContainerPath(artifactPath) {
			return fmt.Errorf("migration %s has an invalid container artifact path", entry.ID)
		}
		if _, err := fmt.Fprintf(writer, "BEGIN;\n\\i '%s'\nCOMMIT;\n", artifactPath); err != nil {
			return err
		}
	}
	columns := "migration_id, migration_name, predecessor_id, phase, schema_from, schema_to, compatible_binary_min, compatible_binary_max, sql_path, sql_size_bytes, sql_sha256, bundle_digest, transaction_mode, reentrancy, rollback_boundary, requires_live_instance_preflight, requires_pitr_preflight"
	if _, err := fmt.Fprintf(writer, "BEGIN;\n\\copy cloud_agents.schema_migrations (%s) FROM '%s' WITH (FORMAT text)\nCOMMIT;\nRESET ROLE;\n", columns, containerLedgerPath); err != nil {
		return err
	}
	return nil
}

func validatePlainField(values ...string) error {
	for _, value := range values {
		if value == "" || strings.ContainsAny(value, "\t\r\n") {
			return errors.New("manifest field is empty or contains a control delimiter")
		}
	}
	return nil
}

func pathWithin(root, candidate string) bool {
	relative, err := filepath.Rel(root, candidate)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative)
}

func validAbsoluteContainerPath(value string) bool {
	if value == "" || !strings.HasPrefix(value, "/") || strings.ContainsAny(value, "'\"\\\t\r\n") {
		return false
	}
	cleaned := filepath.ToSlash(filepath.Clean(value))
	return cleaned == value && !strings.Contains(value, "/../") && !strings.HasSuffix(value, "/..")
}

func postgresBool(value bool) string {
	if value {
		return "t"
	}
	return "f"
}
