package migration

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const (
	ManifestFormatVersion     = "cloud-agents-platform-migration-manifest/v1"
	SchemaBundleFormatVersion = "cloud-agents-platform-schema-bundle/v1"
	SchemaBundleDomain        = "cloud-agents-platform-schema-bundle/v1"
	BootstrapBundleDomain     = "cloud-agents-platform-bootstrap-bundle/v1"
	AdvisoryLockDomain        = "cloud-agents-platform:migrations:v1"
	AdvisoryLockDerivation    = "sha256-first-8-bytes-signed-big-endian-int64"
	MigrationOwnerRole        = "cloud_agents_migration_owner"
	RuntimeRole               = "cloud_agents_runtime"
	BootstrapAdminRole        = "cloud_agents_bootstrap_admin"
	RuntimeManifestPath       = "services/control-plane/migrations/manifest.json"
	RuntimeSchemaBundlePath   = "services/control-plane/migrations/schema-bundle.json"
)

var (
	decimalInt64Pattern = regexp.MustCompile(`^-?(0|[1-9][0-9]*)$`)
	migrationIDPattern  = regexp.MustCompile(`^[0-9]{6}$`)
	artifactPathPattern = regexp.MustCompile(`^[\x21-\x7e]+$`)
)

type ArtifactRecord struct {
	Path      string `json:"path"`
	Mode      string `json:"mode"`
	SizeBytes uint64 `json:"size_bytes"`
	SHA256    Digest `json:"sha256"`
}

func (record ArtifactRecord) Validate() error {
	if err := validateArtifactPath(record.Path); err != nil {
		return err
	}
	if record.Mode != "100644" {
		return fail(CodeInvalidArtifact, record.Path, "artifact mode must be 100644", nil)
	}
	if record.SizeBytes == 0 {
		return fail(CodeInvalidArtifact, record.Path, "artifact size must be positive", nil)
	}
	if err := requireDigest(record.Path+".sha256", record.SHA256); err != nil {
		return err
	}
	return nil
}

type AdvisoryLock struct {
	Domain          string `json:"domain"`
	Derivation      string `json:"derivation"`
	KeyInt64Decimal string `json:"key_int64_decimal"`
}

func (lock AdvisoryLock) Key() (int64, error) {
	if lock.Domain != AdvisoryLockDomain || lock.Derivation != AdvisoryLockDerivation {
		return 0, fail(CodeInvalidManifest, "advisory_lock", "unsupported advisory lock identity", nil)
	}
	if !decimalInt64Pattern.MatchString(lock.KeyInt64Decimal) || lock.KeyInt64Decimal == "-0" {
		return 0, fail(CodeInvalidManifest, "advisory_lock", "invalid signed int64 decimal profile", nil)
	}
	key, err := strconv.ParseInt(lock.KeyInt64Decimal, 10, 64)
	if err != nil {
		return 0, fail(CodeInvalidManifest, "advisory_lock", "advisory key exceeds signed int64", err)
	}
	sum := sha256.Sum256([]byte(lock.Domain))
	expected := int64(binary.BigEndian.Uint64(sum[:8]))
	if key != expected {
		return 0, fail(CodeInvalidManifest, "advisory_lock", "advisory key does not match the signed derivation", nil)
	}
	return key, nil
}

type PredecessorSchemaBundle struct {
	SchemaBundleDigest Digest `json:"schema_bundle_digest"`
	Path               string `json:"path"`
	Mode               string `json:"mode"`
	SizeBytes          uint64 `json:"size_bytes"`
	SHA256             Digest `json:"sha256"`
}

func (p PredecessorSchemaBundle) Artifact() ArtifactRecord {
	return ArtifactRecord{Path: p.Path, Mode: p.Mode, SizeBytes: p.SizeBytes, SHA256: p.SHA256}
}

// CatalogPrecondition is an exact discriminated union: an artifact descriptor
// or the ADR-0010 scoped schema_absent/schema_present predecessor pair.
type CatalogPrecondition struct {
	Artifact       *ArtifactRecord
	AcceptedStates []CatalogStateProjection
}

func (condition *CatalogPrecondition) UnmarshalJSON(data []byte) error {
	value, err := ParseStrictJSON(data)
	if err != nil {
		return err
	}
	object, ok := value.(map[string]JSONValue)
	if !ok {
		return fail(CodeInvalidManifest, "catalog_precondition", "must be an object", nil)
	}
	if accepted, hasAccepted := object["accepted_states"]; hasAccepted {
		array, ok := accepted.([]JSONValue)
		if !ok || len(array) != 2 {
			return fail(CodeInvalidManifest, "catalog_precondition", "accepted_states must contain exactly scoped absent and present states", nil)
		}
		var envelope struct {
			AcceptedStates []json.RawMessage `json:"accepted_states"`
		}
		if _, err := decodeStrictShape(data, &envelope); err != nil {
			return err
		}
		condition.Artifact = nil
		condition.AcceptedStates = make([]CatalogStateProjection, 0, len(envelope.AcceptedStates))
		for _, raw := range envelope.AcceptedStates {
			var state CatalogStateProjection
			if _, err := decodeStrictShape(raw, &state); err != nil {
				return err
			}
			if err := state.Validate(); err != nil {
				return err
			}
			condition.AcceptedStates = append(condition.AcceptedStates, state)
		}
		return nil
	}
	var record ArtifactRecord
	if _, err := decodeStrictShape(data, &record); err != nil {
		return err
	}
	condition.Artifact = &record
	return nil
}

func (condition CatalogPrecondition) MarshalJSON() ([]byte, error) {
	switch {
	case condition.Artifact != nil && len(condition.AcceptedStates) == 0:
		return json.Marshal(condition.Artifact)
	case condition.Artifact == nil && len(condition.AcceptedStates) == 2:
		return json.Marshal(struct {
			AcceptedStates []CatalogStateProjection `json:"accepted_states"`
		}{AcceptedStates: condition.AcceptedStates})
	default:
		return nil, fail(CodeInvalidManifest, "catalog_precondition", "union must contain exactly one branch", nil)
	}
}

type MigrationEntry struct {
	ID                            string              `json:"id"`
	Name                          string              `json:"name"`
	PredecessorID                 *string             `json:"predecessor_id"`
	Phase                         string              `json:"phase"`
	SchemaFrom                    string              `json:"schema_from"`
	SchemaTo                      string              `json:"schema_to"`
	CompatibleControlPlaneMin     string              `json:"compatible_control_plane_min"`
	CompatibleControlPlaneMax     string              `json:"compatible_control_plane_max"`
	CompatibleWorkerMin           string              `json:"compatible_worker_min"`
	CompatibleWorkerMax           string              `json:"compatible_worker_max"`
	SQLArtifact                   ArtifactRecord      `json:"sql_artifact"`
	TransactionMode               string              `json:"transaction_mode"`
	Reentrancy                    string              `json:"reentrancy"`
	RollbackBoundary              string              `json:"rollback_boundary"`
	RequiresLiveInstancePreflight bool                `json:"requires_live_instance_preflight"`
	RequiresPITRPreflight         bool                `json:"requires_pitr_preflight"`
	PredecessorCatalogContract    CatalogPrecondition `json:"predecessor_catalog_contract"`
	CatalogContract               ArtifactRecord      `json:"catalog_contract"`
}

type SchemaBundle struct {
	Lineage                  string                   `json:"lineage"`
	SchemaHead               string                   `json:"schema_head"`
	AdvisoryLock             AdvisoryLock             `json:"advisory_lock"`
	GlobalTableAuthority     ArtifactRecord           `json:"global_table_authority"`
	ProjectionScopeAuthority ProjectionScopeAuthority `json:"projection_scope_authority"`
	PredecessorSchemaBundle  *PredecessorSchemaBundle `json:"predecessor_schema_bundle"`
	Migrations               []MigrationEntry         `json:"migrations"`
}

// ProjectionScopeAuthority is an exact signed member of schema_bundle. Its
// slices must only cross the verified-schema-bundle seam through the copy
// accessors below so callers cannot mutate the decoded signed subject.
type ProjectionScopeAuthority struct {
	DefaultACLOwners     []string `json:"default_acl_owners"`
	ObjectCreatorClosure []string `json:"object_creator_closure"`
}

func (authority ProjectionScopeAuthority) DefaultACLOwnersCopy() []string {
	return append([]string(nil), authority.DefaultACLOwners...)
}

func (authority ProjectionScopeAuthority) ObjectCreatorClosureCopy() []string {
	return append([]string(nil), authority.ObjectCreatorClosure...)
}

func (authority ProjectionScopeAuthority) Validate() error {
	if err := validateProjectionScopeRoles("default_acl_owners", authority.DefaultACLOwners); err != nil {
		return err
	}
	if err := validateProjectionScopeRoles("object_creator_closure", authority.ObjectCreatorClosure); err != nil {
		return err
	}
	creators := make(map[string]struct{}, len(authority.ObjectCreatorClosure))
	for _, role := range authority.ObjectCreatorClosure {
		creators[role] = struct{}{}
	}
	for _, owner := range authority.DefaultACLOwners {
		if _, ok := creators[owner]; !ok {
			return fail(CodeInvalidManifest, "projection_scope_authority.default_acl_owners", "default ACL owner is outside object creator closure", nil)
		}
	}
	return nil
}

func validateProjectionScopeRoles(field string, roles []string) error {
	switch checkPrincipalClosureShape(roles) {
	case principalClosureValid:
		return nil
	case principalClosureEmpty:
		return fail(CodeInvalidManifest, "projection_scope_authority."+field, "signed principal closure must be nonempty", nil)
	case principalClosureLimit:
		return fail(CodeInvalidManifest, "projection_scope_authority."+field, "signed principal closure exceeds the fixed limit", nil)
	case principalClosureInvalidIdentity:
		return fail(CodeInvalidManifest, "projection_scope_authority."+field, "signed principal closure contains an invalid identity", nil)
	case principalClosureNonCanonicalOrder:
		return fail(CodeInvalidManifest, "projection_scope_authority."+field, "signed principal closure is duplicate or unsorted", nil)
	default:
		return fail(CodeInvalidManifest, "projection_scope_authority."+field, "signed principal closure shape is unknown", nil)
	}
}

type BootstrapBundle struct {
	Artifacts []ArtifactRecord `json:"artifacts"`
}

type ExecutionPolicy struct {
	StatementProfile                  string         `json:"statement_profile"`
	CatalogProfile                    string         `json:"catalog_profile"`
	AuthorityContract                 ArtifactRecord `json:"authority_contract"`
	IsolationLevel                    string         `json:"isolation_level"`
	AccessMode                        string         `json:"access_mode"`
	PostgresMajorMin                  uint64         `json:"postgres_major_min"`
	PostgresMajorMax                  uint64         `json:"postgres_major_max"`
	StatementTimeoutMS                uint64         `json:"statement_timeout_ms"`
	LockTimeoutMS                     uint64         `json:"lock_timeout_ms"`
	IdleInTransactionSessionTimeoutMS uint64         `json:"idle_in_transaction_session_timeout_ms"`
	MaxAttempts                       uint64         `json:"max_attempts"`
}

type Manifest struct {
	FormatVersion         string           `json:"format_version"`
	SchemaBundle          SchemaBundle     `json:"schema_bundle"`
	SchemaBundleDigest    Digest           `json:"schema_bundle_digest"`
	BootstrapBundle       BootstrapBundle  `json:"bootstrap_bundle"`
	BootstrapBundleDigest Digest           `json:"bootstrap_bundle_digest"`
	ExecutionPolicy       ExecutionPolicy  `json:"execution_policy"`
	RuntimeArtifacts      []ArtifactRecord `json:"runtime_artifacts"`
	ManifestDigest        Digest           `json:"manifest_digest"`
}

type SchemaBundleDocument struct {
	FormatVersion      string       `json:"format_version"`
	SchemaBundle       SchemaBundle `json:"schema_bundle"`
	SchemaBundleDigest Digest       `json:"schema_bundle_digest"`
}

func DecodeManifest(data []byte) (*Manifest, JSONValue, error) {
	var manifest Manifest
	value, err := DecodeStrict(data, &manifest)
	if err != nil {
		return nil, nil, err
	}
	if err := manifest.Validate(value); err != nil {
		return nil, nil, err
	}
	return &manifest, value, nil
}

func DecodeSchemaBundleDocument(data []byte) (*SchemaBundleDocument, error) {
	var document SchemaBundleDocument
	value, err := DecodeStrict(data, &document)
	if err != nil {
		return nil, err
	}
	if document.FormatVersion != SchemaBundleFormatVersion {
		return nil, fail(CodeInvalidManifest, "schema-bundle", "unsupported format_version", nil)
	}
	digest, err := digestSchemaBundleValue(value)
	if err != nil {
		return nil, err
	}
	if digest != document.SchemaBundleDigest {
		return nil, fail(CodeInvalidManifest, "schema-bundle", "schema_bundle_digest mismatch", nil)
	}
	if err := document.SchemaBundle.Validate(); err != nil {
		return nil, err
	}
	return &document, nil
}

func (manifest *Manifest) Validate(value JSONValue) error {
	if manifest.FormatVersion != ManifestFormatVersion {
		return fail(CodeInvalidManifest, "manifest", "unsupported format_version", nil)
	}
	if err := manifest.SchemaBundle.Validate(); err != nil {
		return err
	}
	if len(manifest.BootstrapBundle.Artifacts) != 2 || manifest.BootstrapBundle.Artifacts[0].Path != "services/control-plane/migrations/bootstrap/database.sql" || manifest.BootstrapBundle.Artifacts[1].Path != "services/control-plane/migrations/bootstrap/roles.sql" {
		return fail(CodeInvalidManifest, "bootstrap_bundle", "manifest v1 requires exactly database.sql then roles.sql", nil)
	}
	if err := validateArtifacts(manifest.BootstrapBundle.Artifacts, true); err != nil {
		return err
	}
	if err := validateArtifacts(manifest.RuntimeArtifacts, true); err != nil {
		return err
	}
	if err := manifest.ExecutionPolicy.Validate(); err != nil {
		return err
	}
	object := value.(map[string]JSONValue)
	schemaDigest, err := digestDomainObject(SchemaBundleDomain, "schema_bundle", object["schema_bundle"])
	if err != nil {
		return err
	}
	if schemaDigest != manifest.SchemaBundleDigest {
		return fail(CodeInvalidManifest, "manifest", "schema_bundle_digest mismatch", nil)
	}
	bootstrapDigest, err := digestDomainObject(BootstrapBundleDomain, "bootstrap_bundle", object["bootstrap_bundle"])
	if err != nil {
		return err
	}
	if bootstrapDigest != manifest.BootstrapBundleDigest {
		return fail(CodeInvalidManifest, "manifest", "bootstrap_bundle_digest mismatch", nil)
	}
	copyObject := cloneJSONObject(object)
	delete(copyObject, "manifest_digest")
	canonical, err := CanonicalJSON(copyObject)
	if err != nil {
		return err
	}
	if DigestBytes(canonical) != manifest.ManifestDigest {
		return fail(CodeInvalidManifest, "manifest", "manifest_digest mismatch", nil)
	}
	return nil
}

func (bundle SchemaBundle) Validate() error {
	if bundle.Lineage != "cloud-agents-platform" || !migrationIDPattern.MatchString(bundle.SchemaHead) {
		return fail(CodeInvalidManifest, "schema_bundle", "invalid lineage or schema_head", nil)
	}
	if _, err := bundle.AdvisoryLock.Key(); err != nil {
		return err
	}
	if err := bundle.GlobalTableAuthority.Validate(); err != nil {
		return err
	}
	if err := bundle.ProjectionScopeAuthority.Validate(); err != nil {
		return err
	}
	if bundle.PredecessorSchemaBundle != nil {
		if err := requireDigest("predecessor_schema_bundle.schema_bundle_digest", bundle.PredecessorSchemaBundle.SchemaBundleDigest); err != nil {
			return err
		}
		if err := bundle.PredecessorSchemaBundle.Artifact().Validate(); err != nil {
			return err
		}
		expected := "services/control-plane/migrations/archive/" + bundle.PredecessorSchemaBundle.SchemaBundleDigest.Hex() + ".schema-bundle.json"
		if bundle.PredecessorSchemaBundle.Path != expected {
			return fail(CodeInvalidLineage, "schema_bundle", "predecessor archive path does not match digest", nil)
		}
	}
	if len(bundle.Migrations) == 0 || len(bundle.Migrations) > 4096 {
		return fail(CodeInvalidManifest, "schema_bundle", "migration count outside allowed range", nil)
	}
	for index := range bundle.Migrations {
		entry := &bundle.Migrations[index]
		if err := entry.Validate(index, bundle.Migrations); err != nil {
			return err
		}
	}
	if bundle.Migrations[len(bundle.Migrations)-1].ID != bundle.SchemaHead {
		return fail(CodeInvalidLineage, "schema_bundle", "schema_head must equal the final migration id", nil)
	}
	return nil
}

func (entry MigrationEntry) Validate(index int, entries []MigrationEntry) error {
	expectedID := fmt.Sprintf("%06d", index+1)
	if !migrationIDPattern.MatchString(entry.ID) || entry.ID != expectedID || entry.Name == "" || entry.SchemaTo != entry.ID {
		return fail(CodeInvalidManifest, "migration", "invalid id, name, or schema_to", nil)
	}
	if index == 0 {
		if entry.PredecessorID != nil || entry.SchemaFrom != "absent" {
			return fail(CodeInvalidLineage, entry.ID, "first predecessor must be null", nil)
		}
	} else if entry.PredecessorID == nil || *entry.PredecessorID != entries[index-1].ID || entry.SchemaFrom != entries[index-1].SchemaTo {
		return fail(CodeInvalidLineage, entry.ID, "migration predecessor is not contiguous", nil)
	}
	if entry.Phase != "expand" && entry.Phase != "backfill" && entry.Phase != "contract" {
		return fail(CodeInvalidManifest, entry.ID, "invalid migration phase", nil)
	}
	if entry.TransactionMode != "transactional" || entry.Reentrancy != "ledger_guarded" || entry.RollbackBoundary == "" {
		return fail(CodeUnsupported, entry.ID, "runner only supports ledger-guarded transactional entries", nil)
	}
	if entry.CompatibleControlPlaneMin != "0.1.0-alpha.1" || entry.CompatibleControlPlaneMax != "0.2.0-0" || entry.CompatibleWorkerMin != "0.1.0-alpha.1" || entry.CompatibleWorkerMax != "0.2.0-0" {
		return fail(CodeInvalidManifest, entry.ID, "consumer compatibility range differs from manifest v1", nil)
	}
	if err := entry.SQLArtifact.Validate(); err != nil {
		return err
	}
	if err := entry.CatalogContract.Validate(); err != nil {
		return err
	}
	if entry.PredecessorCatalogContract.Artifact != nil && len(entry.PredecessorCatalogContract.AcceptedStates) == 0 {
		if err := entry.PredecessorCatalogContract.Artifact.Validate(); err != nil {
			return err
		}
	} else if entry.PredecessorCatalogContract.Artifact == nil && len(entry.PredecessorCatalogContract.AcceptedStates) == 2 {
		if !validInitialCatalogStates(entry.ID, entry.PredecessorCatalogContract.AcceptedStates) {
			return fail(CodeInvalidManifest, entry.ID, "invalid initial catalog predecessor states", nil)
		}
	} else {
		return fail(CodeInvalidManifest, entry.ID, "missing predecessor catalog contract", nil)
	}
	return nil
}

func validInitialCatalogStates(migrationID string, states []CatalogStateProjection) bool {
	if len(states) != 2 || states[0].Absent == nil || states[0].Present != nil || states[1].Absent != nil || states[1].Present == nil {
		return false
	}
	absent, present := states[0].Absent, states[1].Present
	if absent.State != "schema_absent" || absent.Schema != "cloud_agents" || !validInitialProjectionScope(absent.Scope, migrationID) || present.State != "schema_present" || !validInitialProjectionScope(present.Scope, migrationID) {
		return false
	}
	body := present.Body
	return body.Schema.Name == "cloud_agents" && body.Schema.Owner == MigrationOwnerRole && body.Schema.ExplicitACL.CatalogValue == "null" && len(body.Schema.ExplicitACL.Entries) == 0 && validInitialACL(body.Schema.EffectiveACL) && body.Schema.Comment == nil && len(body.Schema.SecurityLabels) == 0 && len(body.DefaultACL) == 0 && len(body.Relations) == 0 && len(body.Functions) == 0 && len(body.Dependencies) == 0 && body.ObjectCount == 0 && len(body.DeclaredObjects) == 0 && len(body.DeniedObjects) == 0
}

func validInitialProjectionScope(scope ProjectionScope, migrationID string) bool {
	return scope.ScopeKind == "predecessor" && scope.SchemaHead == nil && scope.MigrationID != nil && *scope.MigrationID == migrationID && scope.ThroughStatementIndex == nil && scope.DeclaredObjects != nil && len(scope.DeclaredObjects) == 0
}

func validInitialACL(acls []ACLProjection) bool {
	if len(acls) != 1 {
		return false
	}
	acl := acls[0]
	return acl.Grantor == MigrationOwnerRole && acl.Grantee == MigrationOwnerRole && acl.Origin == "owner_implicit" && equalStrings(acl.Privileges, []string{"CREATE", "USAGE"}) && equalStrings(acl.Grantable, []string{"CREATE", "USAGE"})
}

func (policy ExecutionPolicy) Validate() error {
	if policy.StatementProfile != "postgresql-ddl-v1" || policy.CatalogProfile != "cloud-agents-platform-catalog/v1" || policy.IsolationLevel != "serializable" || policy.AccessMode != "read_write" {
		return fail(CodeUnsupported, "execution_policy", "unsupported execution profile", nil)
	}
	if policy.AuthorityContract.Path != "services/control-plane/migrations/catalog/authority-v1.json" || policy.PostgresMajorMin != 15 || policy.PostgresMajorMax != 17 || policy.StatementTimeoutMS != 300000 || policy.LockTimeoutMS != 30000 || policy.IdleInTransactionSessionTimeoutMS != 60000 || policy.MaxAttempts != 3 {
		return fail(CodeInvalidManifest, "execution_policy", "manifest v1 authority, PostgreSQL range, timeout, or retry policy differs from the fixed profile", nil)
	}
	return policy.AuthorityContract.Validate()
}

func validateArtifacts(records []ArtifactRecord, sorted bool) error {
	seen := make(map[string]struct{}, len(records))
	previous := ""
	for _, record := range records {
		if err := record.Validate(); err != nil {
			return err
		}
		if _, exists := seen[record.Path]; exists {
			return fail(CodeInvalidArtifact, record.Path, "duplicate artifact record", nil)
		}
		if sorted && previous != "" && previous >= record.Path {
			return fail(CodeInvalidArtifact, record.Path, "artifact records are not in ASCII path order", nil)
		}
		seen[record.Path] = struct{}{}
		previous = record.Path
	}
	return nil
}

func validateArtifactPath(value string) error {
	if value == "" || len(value) > 256 || !artifactPathPattern.MatchString(value) || strings.Contains(value, `\`) || strings.HasPrefix(value, "/") || strings.HasSuffix(value, "/") || path.Clean(value) != value {
		return fail(CodeInvalidArtifact, "path", fmt.Sprintf("invalid artifact path %q", value), nil)
	}
	for _, segment := range strings.Split(value, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return fail(CodeInvalidArtifact, "path", "path contains an invalid segment", nil)
		}
	}
	return nil
}

func digestDomainObject(domain, field string, value JSONValue) (Digest, error) {
	wrapper := map[string]JSONValue{"domain": domain, field: value}
	canonical, err := CanonicalJSON(wrapper)
	if err != nil {
		return "", err
	}
	return DigestBytes(canonical), nil
}

func digestSchemaBundleValue(value JSONValue) (Digest, error) {
	object, ok := value.(map[string]JSONValue)
	if !ok {
		return "", fail(CodeInvalidManifest, "schema-bundle", "document must be an object", nil)
	}
	bundle, ok := object["schema_bundle"]
	if !ok {
		return "", fail(CodeInvalidManifest, "schema-bundle", "schema_bundle is missing", nil)
	}
	return digestDomainObject(SchemaBundleDomain, "schema_bundle", bundle)
}

func cloneJSONObject(value map[string]JSONValue) map[string]JSONValue {
	copyValue := make(map[string]JSONValue, len(value))
	for key, entry := range value {
		copyValue[key] = entry
	}
	return copyValue
}

func decodeStrictShape(data []byte, target any) (JSONValue, error) {
	return DecodeStrict(data, target)
}

func artifactEqual(left, right ArtifactRecord) bool { return left == right }

func sortedArtifactPaths(records []ArtifactRecord) []string {
	paths := make([]string, len(records))
	for i := range records {
		paths[i] = records[i].Path
	}
	sort.Strings(paths)
	return paths
}

func rawDigestHex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
