//go:build localdev

// Package localmigration provides a deliberately local-development-only
// migration path for a pre-provisioned PostgreSQL database. It does not create
// databases or roles and it has no partial-ledger recovery behavior.
package localmigration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/migration"
)

type Config struct {
	DatabaseURL    string
	RepositoryRoot string
	ManifestPath   string
	// ManifestSelector is an optional member of the generated r2 closed
	// selector set. An empty selector selects canonical 000013 only; the
	// successor requires its explicit generated selector.
	ManifestSelector string
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

type boundRunnerSelection struct {
	selector        generatedRunnerBindingSelector
	manifestRaw     []byte
	schemaBundleRaw []byte
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
	if strings.TrimSpace(config.RepositoryRoot) == "" {
		return nil, errors.New("repository root is required")
	}
	root, err := filepath.Abs(config.RepositoryRoot)
	if err != nil {
		return nil, errors.New("repository root is invalid")
	}
	selection, err := bindGeneratedRunnerSelection(root, config)
	if err != nil {
		return nil, err
	}
	raw := selection.manifestRaw
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
		if info.Mode() != 0o644 || entry.SQLArtifact.Mode != "100644" {
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
	raw := filepath.FromSlash(candidate)
	if raw == "" {
		return "", errors.New("empty paths are not allowed")
	}
	for _, part := range strings.Split(raw, string(filepath.Separator)) {
		if part == "" {
			return "", errors.New("empty path components are not allowed")
		}
		if part == "." || part == ".." {
			return "", errors.New("dot path components are not allowed")
		}
	}
	clean := filepath.Clean(raw)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", errors.New("path escapes repository root")
	}
	rootInfo, err := os.Lstat(root)
	if err != nil || !rootInfo.IsDir() || rootInfo.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("repository root is not a real directory")
	}
	resolved := filepath.Join(root, clean)
	relative, err := filepath.Rel(root, resolved)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", errors.New("path escapes repository root")
	}
	// Do not allow a symlink in any path component.  Checking only the final
	// file would let a caller redirect an otherwise allowlisted path outside
	// the frozen repository tree.
	current := root
	parts := strings.Split(relative, string(filepath.Separator))
	for index, part := range parts {
		if part == "" || part == "." {
			continue
		}
		current = filepath.Join(current, part)
		info, statErr := os.Lstat(current)
		if statErr != nil {
			if os.IsNotExist(statErr) {
				// The caller will report the missing final artifact.  Missing
				// intermediate components are still safe and need no lookup.
				break
			}
			return "", errors.New("path component cannot be inspected")
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", errors.New("symlink path components are not allowed")
		}
		if index < len(parts)-1 && !info.IsDir() {
			return "", errors.New("path component is not a directory")
		}
	}
	return resolved, nil
}

// bindGeneratedRunnerSelection verifies the immutable r2 profile/source and
// the selected canonical 000013 or successor 000014 descriptor before any
// connector call.  The generated profile blob is the authority; all runtime
// paths are closed over the generated selector table rather than caller data.
func bindGeneratedRunnerSelection(root string, config Config) (boundRunnerSelection, error) {
	if err := verifyBoundArtifact(root, generatedRunnerBindingSourceArtifact()); err != nil {
		return boundRunnerSelection{}, fmt.Errorf("runner binding source: %w", err)
	}
	if err := verifyBoundArtifact(root, generatedRunnerBindingProfileArtifact()); err != nil {
		return boundRunnerSelection{}, fmt.Errorf("runner binding profile: %w", err)
	}
	if err := verifyBoundArtifact(root, generatedRunnerBindingSourceSchemaArtifact()); err != nil {
		return boundRunnerSelection{}, fmt.Errorf("runner binding source schema: %w", err)
	}
	if err := verifyBoundArtifact(root, generatedRunnerBindingProfileSchemaArtifact()); err != nil {
		return boundRunnerSelection{}, fmt.Errorf("runner binding profile schema: %w", err)
	}
	for _, artifact := range generatedRunnerBindingR1Artifacts {
		if err := verifyBoundArtifact(root, artifact); err != nil {
			return boundRunnerSelection{}, fmt.Errorf("r1 immutable object: %w", err)
		}
	}

	profilePath, err := resolveWithin(root, runnerBindingProfilePath)
	if err != nil {
		return boundRunnerSelection{}, fmt.Errorf("runner binding profile path: %w", err)
	}
	profileRaw, err := os.ReadFile(profilePath)
	if err != nil {
		return boundRunnerSelection{}, errors.New("runner binding profile cannot be read")
	}
	profileValue, err := migration.ParseStrictJSON(profileRaw)
	if err != nil {
		return boundRunnerSelection{}, fmt.Errorf("runner binding profile JSON: %w", err)
	}
	if err := verifyRunnerBindingProfile(profileValue); err != nil {
		return boundRunnerSelection{}, err
	}

	selector, err := selectGeneratedRunnerBinding(config)
	if err != nil {
		return boundRunnerSelection{}, err
	}
	manifestRaw, err := readBoundArtifact(root, generatedRunnerBindingArtifact{
		path: selector.manifestPath, mode: "100644", sizeBytes: selector.manifestSizeBytes,
		rawDigest: selector.manifestRawDigest,
	})
	if err != nil {
		return boundRunnerSelection{}, fmt.Errorf("selected manifest: %w", err)
	}
	schemaRaw, err := readBoundArtifact(root, generatedRunnerBindingArtifact{
		path: selector.schemaBundlePath, mode: "100644", sizeBytes: selector.schemaBundleSizeBytes,
		rawDigest: selector.schemaBundleRawDigest,
	})
	if err != nil {
		return boundRunnerSelection{}, fmt.Errorf("selected schema bundle: %w", err)
	}
	if err := verifySelectedBundle(selector, manifestRaw, schemaRaw); err != nil {
		return boundRunnerSelection{}, err
	}
	return boundRunnerSelection{selector: selector, manifestRaw: manifestRaw, schemaBundleRaw: schemaRaw}, nil
}

func selectGeneratedRunnerBinding(config Config) (generatedRunnerBindingSelector, error) {
	// Selector and path are identity inputs.  Do not normalize whitespace or
	// path spelling, otherwise a caller-controlled near miss could be treated
	// as one of the frozen generated identities.
	requestedSelector := config.ManifestSelector
	requestedPath := config.ManifestPath
	if requestedSelector == "" {
		// The canonical selector is the only implicit default.  A successor
		// path must be accompanied by its exact generated selector; path-only
		// inference would let a caller choose a lineage by spelling a path.
		if requestedPath != "" && requestedPath != generatedRunnerBindingSelectors[0].manifestPath {
			return generatedRunnerBindingSelector{}, errors.New("successor requires its exact generated selector")
		}
		requestedSelector = generatedRunnerBindingSelectors[0].selectorID
	}
	var selected *generatedRunnerBindingSelector
	for index := range generatedRunnerBindingSelectors {
		candidate := &generatedRunnerBindingSelectors[index]
		if requestedSelector != "" && candidate.selectorID == requestedSelector {
			selected = candidate
			break
		}
		if requestedSelector == "" && candidate.manifestPath == requestedPath {
			selected = candidate
			break
		}
	}
	if selected == nil {
		return generatedRunnerBindingSelector{}, errors.New("manifest selector is outside the generated closed set")
	}
	if requestedPath != "" && requestedPath != selected.manifestPath {
		return generatedRunnerBindingSelector{}, errors.New("manifest path does not match the generated selector")
	}
	return *selected, nil
}

func verifyRunnerBindingProfile(value migration.JSONValue) error {
	object, ok := value.(map[string]migration.JSONValue)
	if !ok {
		return errors.New("runner binding profile must be an object")
	}
	if stringValue(object, "$schema") != runnerBindingProfileSchemaURL ||
		stringValue(object, "formatVersion") != runnerBindingProfileFormatVersion ||
		stringValue(object, "authorityId") != runnerBindingAuthorityID ||
		stringValue(object, "revision") != runnerBindingRevision ||
		stringValue(object, "profileId") != runnerBindingProfileID ||
		stringValue(object, "inputScope") != runnerBindingInputScope {
		return errors.New("runner binding profile identity differs from generated authority")
	}
	digestValue, ok := object["profileDigest"].(string)
	if !ok || digestValue != runnerBindingProfileDigest {
		return errors.New("runner binding profile digest differs from generated authority")
	}
	withoutDigest := make(map[string]migration.JSONValue, len(object)-1)
	for key, entry := range object {
		if key != "profileDigest" {
			withoutDigest[key] = entry
		}
	}
	canonical, err := migration.CanonicalJSON(withoutDigest)
	if err != nil {
		return fmt.Errorf("runner binding profile canonicalization: %w", err)
	}
	if digestDomain(runnerBindingProfileDomain, canonical) != digestValue {
		return errors.New("runner binding profile logical digest mismatch")
	}
	if !artifactValueMatches(object["source"], generatedRunnerBindingSourceArtifact()) ||
		!artifactValueMatches(object["sourceSchema"], generatedRunnerBindingSourceSchemaArtifact()) ||
		!artifactValueMatches(object["profileSchema"], generatedRunnerBindingProfileSchemaArtifact()) {
		return errors.New("runner binding profile descriptor mismatch")
	}
	if err := verifyRunnerBindingPolicy(object); err != nil {
		return err
	}
	return nil
}

func verifyRunnerBindingPolicy(profile map[string]migration.JSONValue) error {
	// These fields are checked structurally as well as by the frozen blob
	// digest, so a future generated profile cannot silently widen the runner
	// boundary without changing this binding code and its review.
	runner, ok := profile["runner"].(map[string]migration.JSONValue)
	if !ok || stringValue(runner, "mode") != "localdev_only" ||
		stringValue(runner, "completeLedger") != "no-op" ||
		stringValue(runner, "entryWriter") != "NOT_IMPLEMENTED" ||
		stringValue(runner, "recoveryWriter") != "NOT_IMPLEMENTED" ||
		stringValue(runner, "externalEffects") != "forbidden" ||
		!boolValue(runner, "bindBeforeConnect") {
		return errors.New("runner binding policy is not fail-closed")
	}
	boundary, ok := profile["implementationBoundary"].(map[string]migration.JSONValue)
	if !ok || stringValue(boundary, "databaseWrites") != "not_authorized" ||
		stringValue(boundary, "productionRunner") != "forbidden" ||
		stringValue(boundary, "http") != "forbidden" ||
		stringValue(boundary, "p2") != "forbidden" ||
		stringValue(boundary, "provider") != "forbidden" ||
		stringValue(boundary, "deployment") != "forbidden" ||
		stringValue(boundary, "publication") != "forbidden" ||
		stringValue(boundary, "gateTransition") != "forbidden" {
		return errors.New("runner binding implementation boundary is not closed")
	}
	return nil
}

func verifySelectedBundle(selector generatedRunnerBindingSelector, manifestRaw, schemaRaw []byte) error {
	schemaDocument, err := migration.DecodeSchemaBundleDocument(schemaRaw)
	if err != nil {
		return fmt.Errorf("selected schema bundle decode: %w", err)
	}
	if string(schemaDocument.SchemaBundleDigest) != selector.schemaBundleDigest ||
		schemaDocument.SchemaBundle.SchemaHead != selector.schemaHead ||
		len(schemaDocument.SchemaBundle.Migrations) != selector.migrationCount {
		return errors.New("selected schema bundle identity differs from generated selector")
	}
	manifest, manifestValue, err := migration.DecodeManifest(manifestRaw)
	if err != nil {
		return fmt.Errorf("selected manifest decode: %w", err)
	}
	if string(manifest.ManifestDigest) != selector.manifestDigest ||
		string(manifest.SchemaBundleDigest) != selector.schemaBundleDigest ||
		manifest.SchemaBundle.SchemaHead != selector.schemaHead ||
		len(manifest.SchemaBundle.Migrations) != selector.migrationCount {
		return errors.New("selected manifest identity differs from generated selector")
	}
	// The manifest and external schema bundle must describe the same signed
	// schema object; comparing their canonical JSON prevents a self-consistent
	// pair from being substituted under an otherwise valid raw blob.
	manifestObject, ok := manifestValue.(map[string]migration.JSONValue)
	if !ok {
		return errors.New("selected manifest is not an object")
	}
	schemaValue, err := migration.ParseStrictJSON(schemaRaw)
	if err != nil {
		return fmt.Errorf("selected schema bundle JSON: %w", err)
	}
	schemaObject, ok := schemaValue.(map[string]migration.JSONValue)
	if !ok || !jsonValuesEqual(manifestObject["schema_bundle"], schemaObject["schema_bundle"]) {
		return errors.New("selected manifest/schema bundle payload differs")
	}
	return nil
}

func verifyBoundArtifact(root string, artifact generatedRunnerBindingArtifact) error {
	_, err := readBoundArtifact(root, artifact)
	return err
}

func readBoundArtifact(root string, artifact generatedRunnerBindingArtifact) ([]byte, error) {
	path, err := resolveWithin(root, artifact.path)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", artifact.path, err)
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("%s is unavailable or not a regular file", artifact.path)
	}
	if info.Mode() != 0o644 || artifact.mode != "100644" {
		return nil, fmt.Errorf("%s mode differs from generated authority", artifact.path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("%s cannot be read", artifact.path)
	}
	if len(data) != artifact.sizeBytes || string(migration.DigestBytes(data)) != artifact.rawDigest {
		return nil, fmt.Errorf("%s bytes differ from generated authority", artifact.path)
	}
	return data, nil
}

func artifactValueMatches(value migration.JSONValue, expected generatedRunnerBindingArtifact) bool {
	object, ok := value.(map[string]migration.JSONValue)
	if !ok {
		return false
	}
	size, ok := object["sizeBytes"].(uint64)
	return ok && stringValue(object, "path") == expected.path &&
		stringValue(object, "mode") == expected.mode && size == uint64(expected.sizeBytes) &&
		stringValue(object, "sha256") == expected.rawDigest
}

func stringValue(object map[string]migration.JSONValue, key string) string {
	value, _ := object[key].(string)
	return value
}

func boolValue(object map[string]migration.JSONValue, key string) bool {
	value, _ := object[key].(bool)
	return value
}

func jsonValuesEqual(left, right migration.JSONValue) bool {
	leftCanonical, leftErr := migration.CanonicalJSON(left)
	rightCanonical, rightErr := migration.CanonicalJSON(right)
	return leftErr == nil && rightErr == nil && reflect.DeepEqual(leftCanonical, rightCanonical)
}

func digestDomain(domain string, canonical []byte) string {
	hash := sha256.New()
	_, _ = hash.Write([]byte(domain))
	_, _ = hash.Write([]byte{0})
	_, _ = hash.Write(canonical)
	return "sha256:" + hex.EncodeToString(hash.Sum(nil))
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
