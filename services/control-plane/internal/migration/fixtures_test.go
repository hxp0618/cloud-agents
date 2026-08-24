package migration

import (
	"encoding/json"
	"path/filepath"
	"sort"
	"testing"
)

type rawFixtureEnvelope struct {
	Payload       string `json:"payload"`
	RawSHA256     Digest `json:"raw_sha256"`
	Expected      string `json:"expected"`
	ExpectedError string `json:"expected_error"`
}

func TestSharedFixtureManifestIsStrictAndFullyConsumed(t *testing.T) {
	t.Parallel()
	root := filepath.Join(migrationRoot(t), "fixtures", "bundle")
	raw := mustRead(t, filepath.Join(root, "manifest.json"))
	value, err := ParseStrictJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	manifest := requiredFixtureObject(t, value)
	requireFixtureKeys(t, manifest, "format_version", "cases")
	if manifest["format_version"] != "cloud-agents-platform-migration-fixtures/v1" {
		t.Fatalf("unexpected fixture format: %v", manifest["format_version"])
	}
	cases, ok := manifest["cases"].([]JSONValue)
	if !ok {
		t.Fatal("fixture cases is not an array")
	}
	expected := map[string]struct {
		kind, path, outcome, errorCode string
	}{
		"rfc8785":                   {"golden_json", "golden/rfc8785.json", "accept", ""},
		"signed-int64":              {"golden_json", "golden/signed-int64.json", "accept", ""},
		"sql-split":                 {"golden_sql_descriptor", "golden/sql-split.json", "accept", ""},
		"ustar":                     {"golden_ustar_descriptor", "golden/ustar.json", "accept", ""},
		"ancestor-ledger":           {"golden_lineage", "golden/ancestor-ledger.json", "accept", ""},
		"duplicate-key":             {"negative_raw_json", "negative/duplicate-key.case.json", "reject", "DUPLICATE_JSON_KEY"},
		"escaped-equivalent-key":    {"negative_raw_json", "negative/escaped-equivalent-key.case.json", "reject", "DUPLICATE_JSON_KEY"},
		"unicode-whitespace":        {"negative_raw_json", "negative/unicode-whitespace.case.json", "reject", "INVALID_JSON"},
		"ancestor-cycle":            {"negative_lineage", "negative/ancestor-cycle.json", "reject", "ANCESTOR_CYCLE"},
		"ancestor-descriptor-cases": {"negative_ancestor_descriptors", "negative/ancestor-descriptor-cases.json", "reject", "ANCESTOR_DESCRIPTOR"},
		"ledger-rollback":           {"negative_ledger", "negative/ledger-rollback.json", "reject", "LEDGER_BUNDLE_ROLLBACK"},
	}
	seen := make(map[string]struct{}, len(cases))
	for _, rawCase := range cases {
		fixture := requiredFixtureObject(t, rawCase)
		name, ok := fixture["name"].(string)
		if !ok {
			t.Fatal("fixture name is not a string")
		}
		spec, known := expected[name]
		if !known {
			t.Fatalf("unknown fixture %q", name)
		}
		if _, duplicate := seen[name]; duplicate {
			t.Fatalf("duplicate fixture %q", name)
		}
		seen[name] = struct{}{}
		if spec.errorCode == "" {
			requireFixtureKeys(t, fixture, "name", "kind", "path", "expected")
		} else {
			requireFixtureKeys(t, fixture, "name", "kind", "path", "expected", "expected_error")
		}
		if fixture["kind"] != spec.kind || fixture["path"] != spec.path || fixture["expected"] != spec.outcome || (spec.errorCode != "" && fixture["expected_error"] != spec.errorCode) {
			t.Fatalf("fixture %q differs from the shared routing contract: %#v", name, fixture)
		}
		if err := validateFixtureRelativePath(spec.path); err != nil {
			t.Fatalf("fixture %q path: %v", name, err)
		}
		caseRaw := mustRead(t, filepath.Join(root, filepath.FromSlash(spec.path)))
		caseValue, err := ParseStrictJSON(caseRaw)
		if err != nil {
			t.Fatalf("fixture envelope %q is not strict JSON: %v", name, err)
		}
		consumeSharedFixture(t, name, caseValue)
		if spec.kind == "negative_raw_json" {
			var envelope rawFixtureEnvelope
			if _, err := DecodeStrict(caseRaw, &envelope); err != nil {
				t.Fatal(err)
			}
			if envelope.Expected != "reject" || envelope.ExpectedError != spec.errorCode {
				t.Fatalf("raw fixture %q outcome mismatch", name)
			}
			if err := validateFixtureRelativePath(filepath.ToSlash(filepath.Join(filepath.Dir(spec.path), envelope.Payload))); err != nil {
				t.Fatal(err)
			}
			payload := mustRead(t, filepath.Join(root, filepath.Dir(spec.path), envelope.Payload))
			if DigestBytes(payload) != envelope.RawSHA256 {
				t.Fatalf("raw fixture %q digest mismatch", name)
			}
			if _, err := ParseStrictJSON(payload); err == nil {
				t.Fatalf("negative raw fixture %q was accepted", name)
			}
		}
	}
	if len(seen) != len(expected) {
		names := make([]string, 0, len(seen))
		for name := range seen {
			names = append(names, name)
		}
		sort.Strings(names)
		t.Fatalf("fixture inventory is incomplete: %v", names)
	}
}

func consumeSharedFixture(t *testing.T, name string, value JSONValue) {
	t.Helper()
	object := requiredFixtureObject(t, value)
	switch name {
	case "rfc8785":
		canonical, err := CanonicalJSON(object["input"])
		if err != nil {
			t.Fatal(err)
		}
		if string(canonical) != object["canonical_utf8"] || DigestBytes(canonical).String() != object["digest"] || object["nfc_equivalent_is_identical"] != false {
			t.Fatal("shared RFC8785 fixture differs from Go canonicalization")
		}
	case "signed-int64":
		lock := AdvisoryLock{Domain: fixtureString(t, object, "domain"), Derivation: AdvisoryLockDerivation, KeyInt64Decimal: fixtureString(t, object, "key_int64_decimal")}
		if _, err := lock.Key(); err != nil || object["minimum"] != "-9223372036854775808" || object["maximum"] != "9223372036854775807" {
			t.Fatalf("shared signed-int64 fixture failed: %v", err)
		}
	case "sql-split":
		migrationID := fixtureString(t, object, "migration_id")
		statements, err := SplitPostgreSQLStatements(mustRead(t, filepath.Join(migrationRoot(t), "000001_expand_migration_kernel.sql")))
		if err != nil {
			t.Fatal(err)
		}
		index := int(object["statement_index"].(uint64))
		statement := statements[index]
		if migrationID != "000001" || uint64(statement.Start) != object["start"] || uint64(statement.End) != object["end"] || statement.SHA256.String() != object["sha256"] {
			t.Fatal("shared SQL split fixture differs from Go offsets or digest")
		}
	case "ustar":
		entriesValue := object["entries"].([]JSONValue)
		entries := make([]tarMember, 0, len(entriesValue))
		for _, entryValue := range entriesValue {
			entry := requiredFixtureObject(t, entryValue)
			entries = append(entries, tarMember{Path: fixtureString(t, entry, "path"), Data: []byte(fixtureString(t, entry, "utf8"))})
		}
		sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
		raw := writeTestUSTAR(t, entries)
		if uint64(len(raw)) != object["size_bytes"] || DigestBytes(raw).String() != object["sha256"] {
			t.Fatal("shared ustar fixture differs from Go same-bits output")
		}
	case "ancestor-ledger":
		lineage, digestMap := productionFixtureLineage(t, object)
		rows := productionFixtureLedgerRows(t, object["ledger"], lineage, digestMap)
		if _, err := ValidateLedger(rows, lineage); err != nil {
			t.Fatalf("shared ancestor ledger failed production validation: %v", err)
		}
	case "ledger-rollback":
		golden := requiredFixtureObject(t, mustParseFixture(t, filepath.Join(migrationRoot(t), "fixtures", "bundle", "golden", "ancestor-ledger.json")))
		lineage, digestMap := productionFixtureLineage(t, golden)
		rows := productionFixtureLedgerRows(t, object["ledger"], lineage, digestMap)
		if _, err := ValidateLedger(rows, lineage); !IsCode(err, CodeInvalidLedger) {
			t.Fatalf("shared rollback fixture bypassed production ledger validation: %v", err)
		}
	case "ancestor-cycle":
		bundle := requiredFixtureObject(t, object["schema_bundle"])
		predecessor := requiredFixtureObject(t, bundle["predecessor_schema_bundle"])
		if object["schema_bundle_digest"] != predecessor["schema_bundle_digest"] {
			t.Fatal("shared ancestor cycle fixture no longer contains a cycle")
		}
		_, manifest := buildCheckedInRuntimeTar(t)
		document, _ := makeSchemaDocument(t, manifest.SchemaBundle)
		document.SchemaBundle.PredecessorSchemaBundle = &PredecessorSchemaBundle{SchemaBundleDigest: document.SchemaBundleDigest, Path: "services/control-plane/migrations/archive/" + document.SchemaBundleDigest.Hex() + ".schema-bundle.json", Mode: "100644", SizeBytes: 1, SHA256: DigestBytes([]byte{0})}
		if _, err := validateSchemaLineage(document, map[string][]byte{}); !IsCode(err, CodeInvalidLineage) {
			t.Fatalf("shared cycle fixture bypassed production lineage validation: %v", err)
		}
	case "ancestor-descriptor-cases":
		cases := object["cases"].([]JSONValue)
		if len(cases) != 5 {
			t.Fatalf("ancestor descriptor fixture count changed: %d", len(cases))
		}
		exerciseProductionAncestorDescriptorCases(t, cases)
	case "duplicate-key", "escaped-equivalent-key", "unicode-whitespace":
		// Raw payload and raw SHA are consumed by the caller after this envelope.
	default:
		t.Fatalf("fixture %q has no Go consumer", name)
	}
}

func productionFixtureLineage(t *testing.T, fixture map[string]JSONValue) (*SchemaLineage, map[string]Digest) {
	t.Helper()
	oldestFixture := requiredFixtureObject(t, fixture["oldest"])
	currentFixture := requiredFixtureObject(t, fixture["current"])
	_, manifest := buildCheckedInRuntimeTar(t)
	olderBundle := manifest.SchemaBundle
	olderBundle.SchemaHead = "000001"
	olderBundle.Migrations = append([]MigrationEntry(nil), manifest.SchemaBundle.Migrations[:1]...)
	olderBundle.PredecessorSchemaBundle = nil
	older, olderRaw := makeSchemaDocument(t, olderBundle)
	currentBundle := manifest.SchemaBundle
	archivePath := "services/control-plane/migrations/archive/" + older.SchemaBundleDigest.Hex() + ".schema-bundle.json"
	currentBundle.PredecessorSchemaBundle = &PredecessorSchemaBundle{SchemaBundleDigest: older.SchemaBundleDigest, Path: archivePath, Mode: "100644", SizeBytes: uint64(len(olderRaw)), SHA256: DigestBytes(olderRaw)}
	current, _ := makeSchemaDocument(t, currentBundle)
	lineage, err := validateSchemaLineage(current, map[string][]byte{archivePath: olderRaw})
	if err != nil {
		t.Fatal(err)
	}
	return lineage, map[string]Digest{
		fixtureString(t, oldestFixture, "schema_bundle_digest"):  older.SchemaBundleDigest,
		fixtureString(t, currentFixture, "schema_bundle_digest"): current.SchemaBundleDigest,
	}
}

func productionFixtureLedgerRows(t *testing.T, value JSONValue, lineage *SchemaLineage, digestMap map[string]Digest) []LedgerRow {
	t.Helper()
	fixtureRows := value.([]JSONValue)
	rows := make([]LedgerRow, 0, len(fixtureRows))
	for _, rowValue := range fixtureRows {
		fixtureRow := requiredFixtureObject(t, rowValue)
		id := fixtureString(t, fixtureRow, "migration_id")
		digest, ok := digestMap[fixtureString(t, fixtureRow, "bundle_digest")]
		if !ok {
			t.Fatalf("fixture ledger references unmapped digest")
		}
		entry, err := lineage.EntryForDigest(digest, id)
		if err != nil {
			// Preserve the invalid digest/id claim in a full row so ValidateLedger,
			// not this adapter, remains the authority for the negative fixture.
			entry = &lineage.Current.SchemaBundle.Migrations[len(rows)]
		}
		rows = append(rows, ledgerRowFor(*entry, digest))
	}
	return rows
}

func exerciseProductionAncestorDescriptorCases(t *testing.T, cases []JSONValue) {
	t.Helper()
	_, manifest := buildCheckedInRuntimeTar(t)
	olderBundle := manifest.SchemaBundle
	olderBundle.SchemaHead = "000001"
	olderBundle.Migrations = append([]MigrationEntry(nil), manifest.SchemaBundle.Migrations[:1]...)
	olderBundle.PredecessorSchemaBundle = nil
	older, olderRaw := makeSchemaDocument(t, olderBundle)
	basePath := "services/control-plane/migrations/archive/" + older.SchemaBundleDigest.Hex() + ".schema-bundle.json"
	base := PredecessorSchemaBundle{SchemaBundleDigest: older.SchemaBundleDigest, Path: basePath, Mode: "100644", SizeBytes: uint64(len(olderRaw)), SHA256: DigestBytes(olderRaw)}
	for _, caseValue := range cases {
		fixtureCase := requiredFixtureObject(t, caseValue)
		field := fixtureString(t, fixtureCase, "field")
		if field == "extra" {
			raw, err := json.Marshal(base)
			if err != nil {
				t.Fatal(err)
			}
			object := map[string]any{}
			if err := json.Unmarshal(raw, &object); err != nil {
				t.Fatal(err)
			}
			object["extra"] = fixtureCase["value"]
			raw, _ = json.Marshal(object)
			var decoded PredecessorSchemaBundle
			if _, err := DecodeStrict(raw, &decoded); !IsCode(err, CodeInvalidJSON) {
				t.Fatalf("shared extra-field descriptor bypassed strict production decode: %v", err)
			}
			continue
		}
		mutated := base
		switch field {
		case "path":
			mutated.Path = fixtureString(t, fixtureCase, "value")
		case "size_bytes":
			mutated.SizeBytes = fixtureCase["value"].(uint64)
		case "sha256":
			mutated.SHA256 = Digest(fixtureString(t, fixtureCase, "value"))
		default:
			t.Fatalf("unhandled descriptor fixture field %q", field)
		}
		currentBundle := manifest.SchemaBundle
		currentBundle.PredecessorSchemaBundle = &mutated
		current, _ := makeSchemaDocumentUnchecked(t, currentBundle)
		_, lineageErr := validateSchemaLineage(current, map[string][]byte{mutated.Path: olderRaw})
		if validationErr := currentBundle.Validate(); validationErr == nil && lineageErr == nil {
			t.Fatalf("shared descriptor case %q bypassed production validation", fixtureString(t, fixtureCase, "name"))
		}
	}
}

func makeSchemaDocumentUnchecked(t *testing.T, bundle SchemaBundle) (*SchemaBundleDocument, []byte) {
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
	return document, raw
}

func mustParseFixture(t *testing.T, path string) JSONValue {
	t.Helper()
	value, err := ParseStrictJSON(mustRead(t, path))
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func fixtureString(t *testing.T, object map[string]JSONValue, key string) string {
	t.Helper()
	value, ok := object[key].(string)
	if !ok {
		t.Fatalf("fixture field %q is not a string", key)
	}
	return value
}

func requiredFixtureObject(t *testing.T, value JSONValue) map[string]JSONValue {
	t.Helper()
	object, ok := value.(map[string]JSONValue)
	if !ok {
		t.Fatal("fixture value is not an object")
	}
	return object
}

func requireFixtureKeys(t *testing.T, object map[string]JSONValue, expected ...string) {
	t.Helper()
	actual := make([]string, 0, len(object))
	for key := range object {
		actual = append(actual, key)
	}
	sort.Strings(actual)
	sort.Strings(expected)
	if len(actual) != len(expected) {
		t.Fatalf("fixture keys differ: actual=%v expected=%v", actual, expected)
	}
	for index := range actual {
		if actual[index] != expected[index] {
			t.Fatalf("fixture keys differ: actual=%v expected=%v", actual, expected)
		}
	}
}

func validateFixtureRelativePath(value string) error {
	if value == "" || filepath.IsAbs(value) || filepath.ToSlash(filepath.Clean(value)) != value {
		return fail(CodeInvalidArtifact, "fixture", "fixture path is not a canonical relative path", nil)
	}
	return nil
}
