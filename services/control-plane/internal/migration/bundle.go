package migration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
)

type RuntimeBundle struct {
	Manifest             *Manifest
	Lineage              *SchemaLineage
	AuthorityContract    *AuthorityContract
	GlobalTableAuthority *GlobalTableAuthorityContract
	Files                map[string][]byte
}

func LoadRuntimeBundle(raw []byte, decision VerifiedTrustDecision) (*RuntimeBundle, error) {
	if err := decision.validate(); err != nil {
		return nil, err
	}
	if len(raw) > maxRuntimeTarSize || DigestBytes(raw) != decision.OuterArtifactDigest() {
		return nil, fail(CodeUntrusted, "artifact", "outer migration artifact digest mismatch", nil)
	}
	members, err := parseDeterministicUSTAR(raw)
	if err != nil {
		return nil, err
	}
	files := make(map[string][]byte, len(members))
	for _, member := range members {
		files[member.Path] = member.Data
	}
	manifestRaw, ok := files[RuntimeManifestPath]
	if !ok || len(manifestRaw) > 1<<20 {
		return nil, fail(CodeInvalidArtifact, RuntimeManifestPath, "manifest is missing or oversized", nil)
	}
	manifest, _, err := DecodeManifest(manifestRaw)
	if err != nil {
		return nil, err
	}
	if manifest.ManifestDigest != decision.ManifestDigest() || manifest.SchemaBundleDigest != decision.SchemaBundleDigest() || manifest.BootstrapBundleDigest != decision.BootstrapBundleDigest() {
		return nil, fail(CodeUntrusted, "manifest", "manifest identities differ from the verified candidate", nil)
	}
	if err := validateRuntimeRecords(manifest, files); err != nil {
		return nil, err
	}
	authorityRaw, ok := files[manifest.ExecutionPolicy.AuthorityContract.Path]
	if !ok {
		return nil, fail(CodeInvalidArtifact, manifest.ExecutionPolicy.AuthorityContract.Path, "authority contract is missing", nil)
	}
	authorityContract, err := DecodeAuthorityContract(authorityRaw)
	if err != nil {
		return nil, err
	}
	if manifest.SchemaBundle.GlobalTableAuthority.Path != "services/control-plane/migrations/catalog/global-table-authority-v1.json" {
		return nil, fail(CodeInvalidManifest, "global-table-authority", "manifest v1 global authority path differs from the fixed path", nil)
	}
	globalRaw, ok := files[manifest.SchemaBundle.GlobalTableAuthority.Path]
	if !ok {
		return nil, fail(CodeInvalidArtifact, manifest.SchemaBundle.GlobalTableAuthority.Path, "global table authority contract is missing", nil)
	}
	globalContract, err := DecodeGlobalTableAuthorityContract(globalRaw)
	if err != nil {
		return nil, err
	}
	schemaRaw := files[RuntimeSchemaBundlePath]
	if len(schemaRaw) == 0 || len(schemaRaw) > 1<<20 {
		return nil, fail(CodeInvalidArtifact, RuntimeSchemaBundlePath, "schema bundle is missing or oversized", nil)
	}
	document, err := DecodeSchemaBundleDocument(schemaRaw)
	if err != nil {
		return nil, err
	}
	if document.SchemaBundleDigest != manifest.SchemaBundleDigest || !reflect.DeepEqual(document.SchemaBundle, manifest.SchemaBundle) {
		return nil, fail(CodeInvalidManifest, RuntimeSchemaBundlePath, "file projection differs from inline manifest bundle", nil)
	}
	lineage, err := validateSchemaLineage(document, files)
	if err != nil {
		return nil, err
	}
	if err := validateRuntimeClosure(manifest, lineage); err != nil {
		return nil, err
	}
	return &RuntimeBundle{Manifest: manifest, Lineage: lineage, AuthorityContract: authorityContract, GlobalTableAuthority: globalContract, Files: files}, nil
}

func validateRuntimeRecords(manifest *Manifest, files map[string][]byte) error {
	records := make(map[string]ArtifactRecord, len(manifest.RuntimeArtifacts))
	for _, record := range manifest.RuntimeArtifacts {
		records[record.Path] = record
	}
	if len(files) != len(records)+1 {
		return fail(CodeInvalidArtifact, "runtime", "tar members and runtime_artifacts are not a one-to-one set", nil)
	}
	for path, data := range files {
		if !allowedRuntimePath(path) {
			return fail(CodeInvalidArtifact, path, "path is outside the runtime migration artifact allowlist", nil)
		}
		if path == RuntimeManifestPath {
			continue
		}
		record, ok := records[path]
		if !ok {
			return fail(CodeInvalidArtifact, path, "tar member is not declared", nil)
		}
		limit := 1 << 20
		if strings.HasSuffix(path, ".sql") {
			limit = 16 << 20
		}
		if len(data) == 0 || len(data) > limit || uint64(len(data)) != record.SizeBytes || DigestBytes(data) != record.SHA256 {
			return fail(CodeInvalidArtifact, path, "runtime member size or checksum mismatch", nil)
		}
	}
	return nil
}

func allowedRuntimePath(value string) bool {
	if value == RuntimeManifestPath || value == RuntimeSchemaBundlePath {
		return true
	}
	const root = "services/control-plane/migrations/"
	if !strings.HasPrefix(value, root) {
		return false
	}
	relative := strings.TrimPrefix(value, root)
	if strings.HasPrefix(relative, "archive/") {
		name := strings.TrimPrefix(relative, "archive/")
		return len(name) == 64+len(".schema-bundle.json") && strings.HasSuffix(name, ".schema-bundle.json") && digestPattern.MatchString("sha256:"+strings.TrimSuffix(name, ".schema-bundle.json"))
	}
	if strings.HasPrefix(relative, "catalog/") {
		name := strings.TrimPrefix(relative, "catalog/")
		return name != "" && !strings.Contains(name, "/") && strings.HasSuffix(name, ".json")
	}
	return !strings.Contains(relative, "/") && strings.HasSuffix(relative, ".sql")
}

func validateRuntimeClosure(manifest *Manifest, lineage *SchemaLineage) error {
	referenced := map[string]ArtifactRecord{RuntimeSchemaBundlePath: findRuntimeRecord(manifest.RuntimeArtifacts, RuntimeSchemaBundlePath)}
	add := func(record ArtifactRecord) error {
		actual := findRuntimeRecord(manifest.RuntimeArtifacts, record.Path)
		if !artifactEqual(record, actual) {
			return fail(CodeInvalidArtifact, record.Path, "descriptor differs from runtime_artifacts", nil)
		}
		referenced[record.Path] = record
		return nil
	}
	if err := add(manifest.ExecutionPolicy.AuthorityContract); err != nil {
		return err
	}
	documents := append([]*SchemaBundleDocument{lineage.Current}, lineage.Ancestors...)
	for _, document := range documents {
		if err := add(document.SchemaBundle.GlobalTableAuthority); err != nil {
			return err
		}
		if predecessor := document.SchemaBundle.PredecessorSchemaBundle; predecessor != nil {
			if err := add(predecessor.Artifact()); err != nil {
				return err
			}
		}
		for _, entry := range document.SchemaBundle.Migrations {
			if err := add(entry.SQLArtifact); err != nil {
				return err
			}
			if err := add(entry.CatalogContract); err != nil {
				return err
			}
			if entry.PredecessorCatalogContract.Artifact != nil {
				if err := add(*entry.PredecessorCatalogContract.Artifact); err != nil {
					return err
				}
			}
		}
	}
	if len(referenced) != len(manifest.RuntimeArtifacts) {
		unused := make([]string, 0)
		for _, record := range manifest.RuntimeArtifacts {
			if _, ok := referenced[record.Path]; !ok {
				unused = append(unused, record.Path)
			}
		}
		sort.Strings(unused)
		return fail(CodeInvalidArtifact, "runtime", fmt.Sprintf("unreferenced runtime artifacts: %v", unused), nil)
	}
	return nil
}

func findRuntimeRecord(records []ArtifactRecord, path string) ArtifactRecord {
	for _, record := range records {
		if record.Path == path {
			return record
		}
	}
	return ArtifactRecord{}
}

func canonicalTyped(value any) ([]byte, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, fail(CodeInvalidJSON, "canonicalize", "cannot marshal typed value", err)
	}
	parsed, err := ParseStrictJSON(raw)
	if err != nil {
		return nil, err
	}
	return CanonicalJSON(parsed)
}

func equalBytes(left, right []byte) bool { return bytes.Equal(left, right) }
