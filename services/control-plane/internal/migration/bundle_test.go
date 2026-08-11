package migration

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

func TestCheckedInManifestAndSQLDescriptors(t *testing.T) {
	t.Parallel()
	root := migrationRoot(t)
	manifestRaw := mustRead(t, filepath.Join(root, "manifest.json"))
	manifest, _, err := DecodeManifest(manifestRaw)
	if err != nil {
		t.Fatalf("%v; cause=%v", err, errors.Unwrap(err))
	}
	files := map[string][]byte{}
	for _, record := range manifest.RuntimeArtifacts {
		files[record.Path] = mustRead(t, filepath.Join(moduleRoot(t), filepath.FromSlash(record.Path[len("services/control-plane/"):])))
	}
	bundle := &RuntimeBundle{Manifest: manifest, Files: files}
	classifier, err := NewDescriptorClassifier(bundle)
	if err != nil {
		t.Fatalf("%v; cause=%v", err, errors.Unwrap(err))
	}
	for _, entry := range manifest.SchemaBundle.Migrations {
		statements, err := SplitPostgreSQLStatements(files[entry.SQLArtifact.Path])
		if err != nil {
			t.Fatalf("%s split: %v", entry.ID, err)
		}
		for _, statement := range statements {
			if _, err := classifier.Classify(entry, statement); err != nil {
				t.Fatalf("%s statement %d: %v", entry.ID, statement.Index, err)
			}
		}
	}
}

func TestDescriptorClassifierRejectsExtraCatalogTail(t *testing.T) {
	t.Parallel()
	_, manifest := buildCheckedInRuntimeTar(t)
	files := map[string][]byte{}
	for _, record := range manifest.RuntimeArtifacts {
		files[record.Path] = mustRead(t, filepath.Join(moduleRoot(t), filepath.FromSlash(record.Path[len("services/control-plane/"):])))
	}
	entry := manifest.SchemaBundle.Migrations[0]
	contract, err := DecodeCatalogContract(files[entry.CatalogContract.Path])
	if err != nil {
		t.Fatal(err)
	}
	for index := range contract.SourceDescriptors {
		if contract.SourceDescriptors[index].MigrationID == entry.ID {
			last := contract.SourceDescriptors[index].Statements[len(contract.SourceDescriptors[index].Statements)-1]
			last.Index++
			contract.SourceDescriptors[index].Statements = append(contract.SourceDescriptors[index].Statements, last)
		}
	}
	files[entry.CatalogContract.Path], err = json.Marshal(contract)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewDescriptorClassifier(&RuntimeBundle{Manifest: manifest, Files: files}); !IsCode(err, CodeInvalidManifest) {
		t.Fatalf("extra signed descriptor tail was accepted: %v", err)
	}
}

func TestDescriptorClassifierRejectsUnknownTargetIdentity(t *testing.T) {
	t.Parallel()
	_, manifest := buildCheckedInRuntimeTar(t)
	files := map[string][]byte{}
	for _, record := range manifest.RuntimeArtifacts {
		files[record.Path] = mustRead(t, filepath.Join(moduleRoot(t), filepath.FromSlash(record.Path[len("services/control-plane/"):])))
	}
	entry := manifest.SchemaBundle.Migrations[0]
	contract, err := DecodeCatalogContract(files[entry.CatalogContract.Path])
	if err != nil {
		t.Fatal(err)
	}
	contract.SourceDescriptors[0].Statements[0].Classification.TargetIdentity = "schema:unquoted:unknown"
	files[entry.CatalogContract.Path], err = json.Marshal(contract)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewDescriptorClassifier(&RuntimeBundle{Manifest: manifest, Files: files}); !IsCode(err, CodeInvalidSQL) {
		t.Fatalf("unknown target identity was accepted: %v", err)
	}
}

func TestManifestV1FixedBootstrapAndExecutionPolicy(t *testing.T) {
	t.Parallel()
	raw := mustRead(t, filepath.Join(migrationRoot(t), "manifest.json"))
	manifest, value, err := DecodeManifest(raw)
	if err != nil {
		t.Fatal(err)
	}
	badBootstrap := *manifest
	badBootstrap.BootstrapBundle.Artifacts = append([]ArtifactRecord(nil), manifest.BootstrapBundle.Artifacts...)
	badBootstrap.BootstrapBundle.Artifacts = append(badBootstrap.BootstrapBundle.Artifacts, manifest.BootstrapBundle.Artifacts[0])
	if err := badBootstrap.Validate(value); !IsCode(err, CodeInvalidManifest) {
		t.Fatalf("extra bootstrap artifact was accepted: %v", err)
	}
	badPolicy := *manifest
	badPolicy.ExecutionPolicy.StatementTimeoutMS++
	if err := badPolicy.Validate(value); !IsCode(err, CodeInvalidManifest) {
		t.Fatalf("mutable manifest-v1 timeout was accepted: %v", err)
	}
}

func TestAuthorityContractsStrictDecodeBeforeDatabaseConnect(t *testing.T) {
	t.Parallel()
	root := migrationRoot(t)
	authorityRaw := mustRead(t, filepath.Join(root, "catalog", "authority-v1.json"))
	globalRaw := mustRead(t, filepath.Join(root, "catalog", "global-table-authority-v1.json"))
	if _, err := DecodeAuthorityContract(authorityRaw); err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeGlobalTableAuthorityContract(globalRaw); err != nil {
		t.Fatal(err)
	}
	for name, raw := range map[string][]byte{"authority": authorityRaw, "global": globalRaw} {
		object := map[string]any{}
		if err := json.Unmarshal(raw, &object); err != nil {
			t.Fatal(err)
		}
		object["unknown"] = true
		mutated, err := json.Marshal(object)
		if err != nil {
			t.Fatal(err)
		}
		if name == "authority" {
			_, err = DecodeAuthorityContract(mutated)
		} else {
			_, err = DecodeGlobalTableAuthorityContract(mutated)
		}
		if !IsCode(err, CodeInvalidJSON) {
			t.Fatalf("%s contract accepted unknown field: %v", name, err)
		}
	}
}

func TestRunnerRejectsMalformedAuthorityArtifactBeforeConnect(t *testing.T) {
	t.Parallel()
	raw, manifest := buildCheckedInRuntimeTar(t)
	members, err := parseDeterministicUSTAR(raw)
	if err != nil {
		t.Fatal(err)
	}
	files := make(map[string][]byte, len(members))
	for _, member := range members {
		files[member.Path] = member.Data
	}
	authorityPath := manifest.ExecutionPolicy.AuthorityContract.Path
	authority := map[string]any{}
	if err := json.Unmarshal(files[authorityPath], &authority); err != nil {
		t.Fatal(err)
	}
	authority["unknown"] = true
	files[authorityPath], err = json.Marshal(authority)
	if err != nil {
		t.Fatal(err)
	}
	updated := ArtifactRecord{Path: authorityPath, Mode: "100644", SizeBytes: uint64(len(files[authorityPath])), SHA256: DigestBytes(files[authorityPath])}
	manifest.ExecutionPolicy.AuthorityContract = updated
	for index := range manifest.RuntimeArtifacts {
		if manifest.RuntimeArtifacts[index].Path == authorityPath {
			manifest.RuntimeArtifacts[index] = updated
		}
	}
	files[RuntimeManifestPath] = encodeTestManifest(t, manifest)
	entries := make([]tarMember, 0, len(files))
	for path, data := range files {
		entries = append(entries, tarMember{Path: path, Data: data})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	mutatedTar := writeTestUSTAR(t, entries)
	connector := &fakeConnector{}
	runner := Runner{Trust: testTrustVerifier{decision: testTrustDecision(mutatedTar, manifest)}, Connector: connector, Ledger: &fakeLedgerStore{}, Authority: acceptingAuthority{}, Catalog: acceptingCatalog{}, Intermediate: acceptingIntermediate{}}
	_, err = runner.Run(context.Background(), RunRequest{Artifact: &memoryArtifactSource{data: mutatedTar}, TargetDSN: "test-only"})
	if !IsCode(err, CodeInvalidJSON) || connector.attempts != 0 {
		t.Fatalf("malformed authority reached database connect: err=%v attempts=%d", err, connector.attempts)
	}
}

func encodeTestManifest(t *testing.T, manifest *Manifest) []byte {
	t.Helper()
	manifest.ManifestDigest = Digest("sha256:" + strings.Repeat("0", 64))
	raw, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	value, err := ParseStrictJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	object := value.(map[string]JSONValue)
	delete(object, "manifest_digest")
	canonical, err := CanonicalJSON(object)
	if err != nil {
		t.Fatal(err)
	}
	manifest.ManifestDigest = DigestBytes(canonical)
	raw, err = json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestUnicodeStringDoesNotHideTerminator(t *testing.T) {
	t.Parallel()
	statements, err := SplitPostgreSQLStatements([]byte(`DO U&'x\'; COMMIT; --';`))
	if err != nil {
		t.Fatal(err)
	}
	if len(statements) != 2 || tokenWords(statements[1].Tokens)[0] != "COMMIT" {
		t.Fatalf("Unicode string hid top-level terminator: %#v", statements)
	}
}

func TestStrictDDLGrammarRejectsAuthorityAndTailSmuggling(t *testing.T) {
	t.Parallel()
	entry := MigrationEntry{ID: "000999"}
	classifier := NarrowDDLClassifier{}
	for _, sql := range []string{
		"ALTER TABLE cloud_agents.t DROP CONSTRAINT x ADD CONSTRAINT y CHECK (true);",
		"ALTER TABLE cloud_agents.t DROP CONSTRAINT x, ADD CONSTRAINT y CHECK (true);",
		"CREATE TABLE cloud_agents.t (id text) AS SELECT 1;",
		"GRANT SELECT ON TABLE cloud_agents.t TO cloud_agents_runtime, cloud_agents_bootstrap_admin;",
		"GRANT SELECT ON TABLE cloud_agents.t TO cloud_agents_runtime WITH GRANT OPTION;",
		"GRANT SELECT ON TABLE cloud_agents.t TO cloud_agents_runtime EXTRA;",
		"ALTER TABLE cloud_agents.t OWNER TO cloud_agents_migration_owner DROP CONSTRAINT x;",
	} {
		statements, err := SplitPostgreSQLStatements([]byte(sql))
		if err != nil {
			t.Fatalf("split %q: %v", sql, err)
		}
		if len(statements) != 1 {
			t.Fatalf("expected one statement for %q", sql)
		}
		if _, err := classifier.Classify(entry, statements[0]); !IsCode(err, CodeInvalidSQL) {
			t.Errorf("strict DDL grammar accepted %q: %v", sql, err)
		}
	}
	allowed, err := SplitPostgreSQLStatements([]byte("GRANT SELECT ON TABLE cloud_agents.t TO cloud_agents_runtime;"))
	if err != nil {
		t.Fatal(err)
	}
	plan, err := classifier.Classify(entry, allowed[0])
	if err != nil || plan.TargetIdentity != "table:unquoted:cloud_agents/unquoted:t" {
		t.Fatalf("strict grammar rejected exact allowed form: plan=%+v err=%v", plan, err)
	}
}

func TestDeterministicUSTARConsumerAndBundleClosure(t *testing.T) {
	t.Parallel()
	raw, manifest := buildCheckedInRuntimeTar(t)
	decision := testTrustDecision(raw, manifest)
	bundle, err := LoadRuntimeBundle(raw, decision)
	if err != nil {
		t.Fatal(err)
	}
	if bundle.Manifest.SchemaBundle.SchemaHead != "000002" || len(bundle.Files) != len(manifest.RuntimeArtifacts)+1 {
		t.Fatalf("unexpected bundle projection: head=%s files=%d", bundle.Manifest.SchemaBundle.SchemaHead, len(bundle.Files))
	}
	if _, err := parseDeterministicUSTAR(append(bytes.Clone(raw), make([]byte, 512)...)); err == nil {
		t.Error("accepted trailing block after exact two-block terminator")
	}
	mutated := bytes.Clone(raw)
	mutated[100] = '6'
	if _, err := parseDeterministicUSTAR(mutated); err == nil {
		t.Error("accepted non-canonical mode")
	}
}

func TestDeterministicUSTARGoldenDigest(t *testing.T) {
	t.Parallel()
	raw := writeTestUSTAR(t, []tarMember{
		{Path: "services/control-plane/migrations/a.sql", Data: []byte("a\n")},
		{Path: "services/control-plane/migrations/b.sql", Data: []byte("b\n")},
	})
	if len(raw) != 3072 || DigestBytes(raw) != "sha256:f368ddf7902767b86261240a76b06c9f153c7ea9232a98cc802f4a2bbe9bb0dd" {
		t.Fatalf("Go ustar differs from shared TS golden: size=%d digest=%s", len(raw), DigestBytes(raw))
	}
	if _, err := parseDeterministicUSTAR(raw); err != nil {
		t.Fatal(err)
	}
}

func FuzzSQLSplitterNeverPanics(f *testing.F) {
	f.Add([]byte("CREATE TABLE cloud_agents.t (id text);"))
	f.Add([]byte("DO $x$ BEGIN PERFORM ';'; END $x$;"))
	f.Add([]byte("/* outer /* nested */ end */ SELECT 1;"))
	f.Add([]byte(`E'\'';`))
	f.Fuzz(func(t *testing.T, input []byte) {
		statements, err := SplitPostgreSQLStatements(input)
		if err != nil {
			return
		}
		for _, statement := range statements {
			if statement.Start < 0 || statement.End > len(input) || statement.Start >= statement.End || !bytes.Equal(statement.Raw, input[statement.Start:statement.End]) {
				t.Fatalf("splitter returned an invalid raw boundary: %+v", statement)
			}
		}
	})
}

func TestLedgerValidationUsesDigestChain(t *testing.T) {
	t.Parallel()
	raw, manifest := buildCheckedInRuntimeTar(t)
	bundle, err := LoadRuntimeBundle(raw, testTrustDecision(raw, manifest))
	if err != nil {
		t.Fatal(err)
	}
	row := ledgerRowFor(manifest.SchemaBundle.Migrations[0], manifest.SchemaBundleDigest)
	snapshot, err := ValidateLedger([]LedgerRow{row}, bundle.Lineage)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Head != "000001" {
		t.Fatalf("wrong head %q", snapshot.Head)
	}
	row.AppliedAt = time.Time{}
	row.AppliedBy = ""
	if _, err := ValidateLedger([]LedgerRow{row}, bundle.Lineage); err != nil {
		t.Fatalf("applied_at/applied_by must not participate in identity: %v", err)
	}
	drifted := row
	drifted.SQLPath += ".drift"
	if _, err := ValidateLedger([]LedgerRow{drifted}, bundle.Lineage); !IsCode(err, CodeInvalidLedger) {
		t.Fatalf("accepted ledger identity drift: %v", err)
	}
	row.BundleDigest = Digest("sha256:" + strings.Repeat("f", 64))
	if _, err := ValidateLedger([]LedgerRow{row}, bundle.Lineage); !IsCode(err, CodeInvalidLedger) {
		t.Fatalf("accepted unknown bundle digest: %v", err)
	}
}

func TestAncestorDescriptorAndRawIdentityFailClosed(t *testing.T) {
	t.Parallel()
	_, manifest := buildCheckedInRuntimeTar(t)
	olderBundle := manifest.SchemaBundle
	olderBundle.SchemaHead = "000001"
	olderBundle.Migrations = append([]MigrationEntry(nil), manifest.SchemaBundle.Migrations[:1]...)
	olderBundle.PredecessorSchemaBundle = nil
	older, olderRaw := makeSchemaDocument(t, olderBundle)

	currentBundle := manifest.SchemaBundle
	archivePath := "services/control-plane/migrations/archive/" + older.SchemaBundleDigest.Hex() + ".schema-bundle.json"
	currentBundle.PredecessorSchemaBundle = &PredecessorSchemaBundle{
		SchemaBundleDigest: older.SchemaBundleDigest,
		Path:               archivePath,
		Mode:               "100644",
		SizeBytes:          uint64(len(olderRaw)),
		SHA256:             DigestBytes(olderRaw),
	}
	current, _ := makeSchemaDocument(t, currentBundle)
	files := map[string][]byte{archivePath: olderRaw}
	if _, err := validateSchemaLineage(current, files); err != nil {
		t.Fatalf("valid ancestor rejected: %v", err)
	}

	badRaw := append(bytes.Clone(olderRaw), '\n')
	if _, err := validateSchemaLineage(current, map[string][]byte{archivePath: badRaw}); !IsCode(err, CodeInvalidLineage) {
		t.Fatalf("raw archive mutation accepted: %v", err)
	}
	badSize := *current
	badSize.SchemaBundle.PredecessorSchemaBundle = clonePredecessor(current.SchemaBundle.PredecessorSchemaBundle)
	badSize.SchemaBundle.PredecessorSchemaBundle.SizeBytes++
	if _, err := validateSchemaLineage(&badSize, files); !IsCode(err, CodeInvalidLineage) {
		t.Fatalf("descriptor size mutation accepted: %v", err)
	}

	mutatedOlderBundle := olderBundle
	mutatedOlderBundle.Migrations = append([]MigrationEntry(nil), olderBundle.Migrations...)
	mutatedOlderBundle.Migrations[0].Name = "mutated_name"
	mutatedOlder, mutatedRaw := makeSchemaDocument(t, mutatedOlderBundle)
	mutatedPath := "services/control-plane/migrations/archive/" + mutatedOlder.SchemaBundleDigest.Hex() + ".schema-bundle.json"
	mutatedCurrentBundle := currentBundle
	mutatedCurrentBundle.PredecessorSchemaBundle = &PredecessorSchemaBundle{SchemaBundleDigest: mutatedOlder.SchemaBundleDigest, Path: mutatedPath, Mode: "100644", SizeBytes: uint64(len(mutatedRaw)), SHA256: DigestBytes(mutatedRaw)}
	mutatedCurrent, _ := makeSchemaDocument(t, mutatedCurrentBundle)
	if _, err := validateSchemaLineage(mutatedCurrent, map[string][]byte{mutatedPath: mutatedRaw}); !IsCode(err, CodeInvalidLineage) {
		t.Fatalf("ancestor strict-prefix mutation accepted: %v", err)
	}

	wrongMode := *current.SchemaBundle.PredecessorSchemaBundle
	wrongMode.Mode = "100600"
	badModeBundle := current.SchemaBundle
	badModeBundle.PredecessorSchemaBundle = &wrongMode
	if err := badModeBundle.Validate(); !IsCode(err, CodeInvalidArtifact) {
		t.Fatalf("ancestor mode mutation accepted: %v", err)
	}
}

func makeSchemaDocument(t *testing.T, bundle SchemaBundle) (*SchemaBundleDocument, []byte) {
	t.Helper()
	bundleRaw, err := json.Marshal(bundle)
	if err != nil {
		t.Fatal(err)
	}
	bundleValue, err := ParseStrictJSON(bundleRaw)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := digestDomainObject(SchemaBundleDomain, "schema_bundle", bundleValue)
	if err != nil {
		t.Fatal(err)
	}
	document := &SchemaBundleDocument{FormatVersion: SchemaBundleFormatVersion, SchemaBundle: bundle, SchemaBundleDigest: digest}
	raw, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeSchemaBundleDocument(raw)
	if err != nil {
		t.Fatal(err)
	}
	return decoded, raw
}

func clonePredecessor(value *PredecessorSchemaBundle) *PredecessorSchemaBundle {
	if value == nil {
		return nil
	}
	copyValue := *value
	return &copyValue
}

func buildCheckedInRuntimeTar(t *testing.T) ([]byte, *Manifest) {
	t.Helper()
	manifestRaw := mustRead(t, filepath.Join(migrationRoot(t), "manifest.json"))
	manifest, _, err := DecodeManifest(manifestRaw)
	if err != nil {
		t.Fatal(err)
	}
	entries := []tarMember{{Path: RuntimeManifestPath, Data: manifestRaw}}
	for _, record := range manifest.RuntimeArtifacts {
		relative := record.Path[len("services/control-plane/"):]
		entries = append(entries, tarMember{Path: record.Path, Data: mustRead(t, filepath.Join(moduleRoot(t), filepath.FromSlash(relative)))})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	return writeTestUSTAR(t, entries), manifest
}

func writeTestUSTAR(t *testing.T, entries []tarMember) []byte {
	t.Helper()
	var output bytes.Buffer
	for _, entry := range entries {
		prefix, name, err := splitUSTARPath(entry.Path)
		if err != nil {
			t.Fatal(err)
		}
		header := make([]byte, 512)
		copy(header[0:100], name)
		copy(header[100:108], []byte("0000644\x00"))
		copy(header[108:116], []byte("0000000\x00"))
		copy(header[116:124], []byte("0000000\x00"))
		copy(header[124:136], []byte(fmt.Sprintf("%011o\x00", len(entry.Data))))
		copy(header[136:148], []byte("00000000000\x00"))
		for index := 148; index < 156; index++ {
			header[index] = ' '
		}
		header[156] = '0'
		copy(header[257:263], []byte("ustar\x00"))
		copy(header[263:265], []byte("00"))
		copy(header[345:500], prefix)
		checksum := 0
		for _, value := range header {
			checksum += int(value)
		}
		copy(header[148:156], []byte(fmt.Sprintf("%06o\x00 ", checksum)))
		output.Write(header)
		output.Write(entry.Data)
		if remainder := len(entry.Data) % 512; remainder != 0 {
			output.Write(make([]byte, 512-remainder))
		}
	}
	output.Write(make([]byte, 1024))
	return output.Bytes()
}

func testTrustDecision(raw []byte, manifest *Manifest) VerifiedTrustDecision {
	return VerifiedTrustDecision{
		verified:                      true,
		expectedSchemaBundleDigest:    manifest.SchemaBundleDigest,
		expectedBootstrapBundleDigest: manifest.BootstrapBundleDigest,
		expectedManifestDigest:        manifest.ManifestDigest,
		expectedOuterArtifactDigest:   DigestBytes(raw),
		expectedRunnerReleaseDigest:   DigestBytes([]byte("runner-release-test")),
		repositoryIdentity:            "hxp0618/cloud-agents",
		releaseIdentity:               "test-only",
		securityEpoch:                 1,
	}
}

func moduleRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func migrationRoot(t *testing.T) string { return filepath.Join(moduleRoot(t), "migrations") }

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func ledgerRowFor(entry MigrationEntry, digest Digest) LedgerRow {
	return LedgerRow{
		MigrationID: entry.ID, MigrationName: entry.Name, PredecessorID: entry.PredecessorID,
		Phase: entry.Phase, SchemaFrom: entry.SchemaFrom, SchemaTo: entry.SchemaTo,
		CompatibleBinaryMin: entry.CompatibleControlPlaneMin, CompatibleBinaryMax: entry.CompatibleControlPlaneMax,
		SQLPath: entry.SQLArtifact.Path, SQLSizeBytes: int64(entry.SQLArtifact.SizeBytes), SQLSHA256: entry.SQLArtifact.SHA256,
		BundleDigest: digest, TransactionMode: entry.TransactionMode, Reentrancy: entry.Reentrancy,
		RollbackBoundary: entry.RollbackBoundary, RequiresLiveInstancePreflight: entry.RequiresLiveInstancePreflight,
		RequiresPITRPreflight: entry.RequiresPITRPreflight, AppliedAt: time.Unix(1, 0), AppliedBy: MigrationOwnerRole,
	}
}

type testTrustVerifier struct{ decision VerifiedTrustDecision }

func (verifier testTrustVerifier) Verify(context.Context, CandidateEnvelope) (VerifiedTrustDecision, error) {
	return verifier.decision, nil
}

type memoryArtifactSource struct {
	data  []byte
	read  bool
	reads int
}

func (source *memoryArtifactSource) Read(_ context.Context, expected Digest) ([]byte, error) {
	source.read = true
	source.reads++
	if DigestBytes(source.data) != expected {
		return nil, fmt.Errorf("digest mismatch")
	}
	return bytes.Clone(source.data), nil
}

func TestRejectingTrustHappensBeforeArtifactRead(t *testing.T) {
	t.Parallel()
	source := &memoryArtifactSource{data: []byte("not a tar")}
	runner := Runner{Trust: RejectingTrustVerifier{}, Connector: &fakeConnector{}, Ledger: &fakeLedgerStore{}, Authority: acceptingAuthority{}, Catalog: acceptingCatalog{}, Intermediate: acceptingIntermediate{}}
	_, err := runner.Run(context.Background(), RunRequest{Artifact: source, TargetDSN: "secret"})
	if !IsCode(err, CodeUntrusted) || source.read {
		t.Fatalf("trust-before-read violated: err=%v read=%v", err, source.read)
	}
}

func TestFileArtifactSourceNeverFollowsSymlink(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	realPath := filepath.Join(directory, "bundle.tar")
	linkPath := filepath.Join(directory, "bundle-link.tar")
	data := []byte("bounded-test-artifact")
	if err := os.WriteFile(realPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(realPath, linkPath); err != nil {
		t.Fatal(err)
	}
	if _, err := (FileArtifactSource{Path: linkPath}).Read(context.Background(), DigestBytes(data)); !IsCode(err, CodeInvalidArtifact) {
		t.Fatalf("symlink source was followed: %v", err)
	}
	readback, err := (FileArtifactSource{Path: realPath}).Read(context.Background(), DigestBytes(data))
	if err != nil || !bytes.Equal(readback, data) {
		t.Fatalf("regular source failed: data=%q err=%v", readback, err)
	}
}
