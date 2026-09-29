package migrationcore

import (
	"bytes"
	"encoding/json"
	"math"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestObjectIdentityProjectionIsClosedUnion(t *testing.T) {
	t.Parallel()
	valid := []string{
		`{"kind":"schema","name":"cloud_agents"}`,
		`{"kind":"relation","identity":{"schema":"cloud_agents","name":"jobs"}}`,
		`{"kind":"function","identity":{"schema":"cloud_agents","name":"f","arguments":[{"schema":"pg_catalog","name":"text"}]}}`,
		`{"kind":"trigger","relation":{"schema":"cloud_agents","name":"jobs"},"name":"jobs_trigger","owning_constraint":null}`,
		`{"kind":"internal","semantic_kind":"row_type","owning_object":{"kind":"relation","identity":{"schema":"cloud_agents","name":"jobs"}}}`,
	}
	for _, raw := range valid {
		var identity ObjectIdentityProjection
		if _, err := DecodeStrict([]byte(raw), &identity); err != nil {
			t.Errorf("valid identity %s: %v", raw, err)
			continue
		}
		if err := identity.Validate(); err != nil {
			t.Errorf("validated identity %s: %v", raw, err)
		}
	}
	invalid := []string{
		`{"kind":"schema","name":"cloud_agents","oid":1}`,
		`{"kind":"unknown","name":"cloud_agents"}`,
		`{"kind":"function","identity":{"schema":"cloud_agents","name":"f","arguments":[]},"arguments":[]}`,
		`{"kind":"internal","semantic_kind":"nested","owning_object":{"kind":"internal","semantic_kind":"row_type","owning_object":{"kind":"schema","name":"cloud_agents"}}}`,
		`{"kind":"trigger","relation":{"schema":"cloud_agents","name":"jobs"},"name":"jobs_trigger","owning_constraint":{"kind":"schema","name":"cloud_agents"}}`,
	}
	for _, raw := range invalid {
		var identity ObjectIdentityProjection
		_, err := DecodeStrict([]byte(raw), &identity)
		if err == nil {
			err = identity.Validate()
		}
		if err == nil {
			t.Errorf("accepted invalid identity %s", raw)
		}
	}
}

func TestCatalogStateStrictShapes(t *testing.T) {
	t.Parallel()
	migrationID := "000001"
	head := "000001"
	prefix := uint32(0)
	predecessor := ProjectionScope{ScopeKind: "predecessor", SchemaHead: nil, MigrationID: &migrationID, ThroughStatementIndex: nil, DeclaredObjects: []ObjectIdentityProjection{}}
	statementPrefix := ProjectionScope{ScopeKind: "statement_prefix", SchemaHead: nil, MigrationID: &migrationID, ThroughStatementIndex: &prefix, DeclaredObjects: []ObjectIdentityProjection{}}
	final := ProjectionScope{ScopeKind: "final", SchemaHead: &head, MigrationID: nil, ThroughStatementIndex: nil, DeclaredObjects: []ObjectIdentityProjection{}}

	absent := CatalogStateProjection{Absent: &SchemaAbsentProjection{State: "schema_absent", Scope: predecessor, Schema: "cloud_agents"}}
	raw, err := json.Marshal(absent)
	if err != nil {
		t.Fatal(err)
	}
	var decoded CatalogStateProjection
	if _, err := DecodeStrict(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if err := decoded.Validate(); err != nil {
		t.Fatal(err)
	}

	present := CatalogStateProjection{Present: &SchemaPresentProjection{State: "schema_present", Scope: final, Body: minimalCatalogBody()}}
	raw, err = json.Marshal(present)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeStrict(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if _, err := present.ComputeDigest(); err != nil {
		t.Fatal(err)
	}

	if err := statementPrefix.Validate(); err != nil {
		t.Fatal(err)
	}
	badScope := statementPrefix
	badScope.ThroughStatementIndex = nil
	if err := badScope.Validate(); err == nil {
		t.Fatal("statement prefix without through_statement_index was accepted")
	}
	maxUint32Scope := []byte(`{"scope_kind":"statement_prefix","schema_head":null,"migration_id":"000001","through_statement_index":4294967295,"declared_objects":[]}`)
	var decodedScope ProjectionScope
	if _, err := DecodeStrict(maxUint32Scope, &decodedScope); err != nil {
		t.Fatalf("uint32 maximum scope was rejected: %v", err)
	}
	overflowScope := bytes.Replace(maxUint32Scope, []byte("4294967295"), []byte("4294967296"), 1)
	if _, err := DecodeStrict(overflowScope, &decodedScope); !IsCode(err, CodeInvalidJSON) {
		t.Fatalf("uint32 overflow scope was accepted: %v", err)
	}
}

func TestCatalogPreconditionRejectsLegacyUnknownAndMixedStates(t *testing.T) {
	t.Parallel()
	root := filepath.Join(migrationRoot(t), "fixtures", "projection", "golden")
	absent := string(mustRead(t, filepath.Join(root, "catalog-state-schema-absent-v1.json")))
	present := string(mustRead(t, filepath.Join(root, "catalog-state-schema-present-v1.json")))
	valid := []byte(`{"accepted_states":[` + absent + `,` + present + `]}`)
	var condition CatalogPrecondition
	if _, err := DecodeStrict(valid, &condition); err != nil {
		t.Fatal(err)
	}
	if condition.Artifact != nil || len(condition.AcceptedStates) != 2 || !validInitialCatalogStates("000001", condition.AcceptedStates) {
		t.Fatal("scoped absent/present predecessor pair lost its typed union")
	}

	legacy := strings.Replace(string(valid), `"schema_present"`, `"empty_schema"`, 1)
	if _, err := DecodeStrict([]byte(legacy), &condition); err == nil {
		t.Fatal("legacy empty_schema predecessor was accepted")
	}
	unknown := strings.Replace(string(valid), `"schema": "cloud_agents"`, `"schema": "cloud_agents","unknown":true`, 1)
	if _, err := DecodeStrict([]byte(unknown), &condition); !IsCode(err, CodeInvalidJSON) {
		t.Fatalf("unknown predecessor field was accepted: %v", err)
	}
	mixed := strings.Replace(string(valid), `"schema": "cloud_agents"`, `"schema": "cloud_agents","body":{}`, 1)
	if _, err := DecodeStrict([]byte(mixed), &condition); !IsCode(err, CodeInvalidJSON) {
		t.Fatalf("mixed absent/present predecessor branch was accepted: %v", err)
	}
}

func TestACLSetAndReachabilityPreserveDistinctFacts(t *testing.T) {
	t.Parallel()
	if err := (ACLSetProjection{CatalogValue: "null", Entries: []ACLProjection{}}).Validate(); err != nil {
		t.Fatal(err)
	}
	if err := (ACLSetProjection{CatalogValue: "null", Entries: []ACLProjection{{Grantor: "owner", Grantee: "role", Privileges: []string{"USAGE"}, Grantable: []string{}, Origin: "catalog_explicit"}}}).Validate(); err == nil {
		t.Fatal("null ACL with entries was accepted")
	}
	if err := (ACLSetProjection{CatalogValue: "explicit", Entries: []ACLProjection{{Grantee: "role", Privileges: []string{"USAGE"}, Grantable: []string{}, Origin: "catalog_explicit"}}}).Validate(); err == nil {
		t.Fatal("ACL without grantor provenance was accepted")
	}
}

func TestProjectionNumericProfiles(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		value string
		bits  int
	}{
		{"0", 16}, {"-32768", 16}, {"32767", 16},
		{"-2147483648", 32}, {"2147483647", 32},
		{"-9223372036854775808", 64}, {"9223372036854775807", 64},
	} {
		if _, err := ValidateSignedIntegerDecimal(test.value, test.bits); err != nil {
			t.Errorf("valid int%d %q: %v", test.bits, test.value, err)
		}
	}
	for _, value := range []string{"-0", "+1", "01", "-01", "32768", "1.0", "9223372036854775808"} {
		bits := 64
		if value == "32768" {
			bits = 16
		}
		if _, err := ValidateSignedIntegerDecimal(value, bits); err == nil {
			t.Errorf("accepted invalid int%d %q", bits, value)
		}
	}

	for _, value := range []string{"0", "1", "-1", "1.23", "0.01", "-0.125", "-999999999999999999999.0001"} {
		if err := ValidateExactNumeric(value); err != nil {
			t.Errorf("valid numeric %q: %v", value, err)
		}
	}
	for _, value := range []string{"-0", "0.0", "1.20", "1.", ".1", "+1", "1e2", strings.Repeat("1", 129)} {
		if err := ValidateExactNumeric(value); err == nil {
			t.Errorf("accepted invalid numeric %q", value)
		}
	}
	for input, expected := range map[string]string{"0.0": "0", "1.2300": "1.23", "-10.500": "-10.5"} {
		canonical, err := CanonicalExactNumeric(input)
		if err != nil || canonical != expected {
			t.Errorf("canonical numeric %q: got %q, err=%v", input, canonical, err)
		}
	}
	if _, err := CanonicalExactNumeric("-0.0"); err == nil {
		t.Fatal("canonicalizer accepted negative zero")
	}

	float32Values := []string{"0", "-0.5", "1.0000001", normalizeRyuExponent(strconv.FormatFloat(math.SmallestNonzeroFloat32, 'g', -1, 32)), normalizeRyuExponent(strconv.FormatFloat(math.MaxFloat32, 'g', -1, 32))}
	for _, value := range float32Values {
		if err := ValidateRyuFloat32(value); err != nil {
			t.Errorf("valid float32 %q: %v", value, err)
		}
	}
	float64Values := []string{"0", "-0.5", "-0.125", "1e20", normalizeRyuExponent(strconv.FormatFloat(math.SmallestNonzeroFloat64, 'g', -1, 64)), normalizeRyuExponent(strconv.FormatFloat(math.MaxFloat64, 'g', -1, 64))}
	for _, value := range float64Values {
		if err := ValidateRyuFloat64(value); err != nil {
			t.Errorf("valid float64 %q: %v", value, err)
		}
	}
	for _, value := range []string{"-0", "1.0", "1e+20", "1e020", "NaN", "Infinity", "5e-325", "0.000"} {
		if err := ValidateRyuFloat64(value); err == nil {
			t.Errorf("accepted invalid Ryu float %q", value)
		}
	}
}
