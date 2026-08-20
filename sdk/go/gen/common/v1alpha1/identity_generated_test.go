package v1alpha1

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type canonicalFixture struct {
	Instance      json.RawMessage `json:"instance"`
	CanonicalUTF8 string          `json:"canonicalUtf8"`
	Digest        string          `json:"digest"`
	URN           string          `json:"urn"`
}

type identityFixtureManifest struct {
	Cases []struct {
		Name          string `json:"name"`
		Schema        string `json:"schema"`
		Instance      string `json:"instance"`
		Document      string `json:"document"`
		ExpectedError string `json:"expectedError"`
	} `json:"cases"`
}

type identityFixtureDocument struct {
	Instance               json.RawMessage `json:"instance"`
	CanonicalUTF8          string          `json:"canonicalUtf8"`
	CandidateCanonicalUTF8 string          `json:"candidateCanonicalUtf8"`
	Digest                 string          `json:"digest"`
	URN                    string          `json:"urn"`
}

func TestNamespaceRefGeneratedFixture(t *testing.T) {
	fixture := readCanonicalFixture(t, "golden/namespace-ref-canonical.json")
	ref, err := DecodeNamespaceRefJSON(fixture.Instance)
	if err != nil {
		t.Fatalf("DecodeNamespaceRefJSON: %v", err)
	}
	canonical, err := ref.CanonicalBytes()
	if err != nil || string(canonical) != fixture.CanonicalUTF8 {
		t.Fatalf("canonical = %q / %v", canonical, err)
	}
	digest, err := ref.Digest()
	if err != nil || digest != fixture.Digest {
		t.Fatalf("digest = %q / %v", digest, err)
	}
	urn, err := ref.URN()
	if err != nil || urn != fixture.URN {
		t.Fatalf("urn = %q / %v", urn, err)
	}
}

func TestSubjectRefGeneratedFixture(t *testing.T) {
	fixture := readCanonicalFixture(t, "golden/subject-ref-canonical.json")
	ref, err := DecodeSubjectRefJSON(fixture.Instance)
	if err != nil {
		t.Fatalf("DecodeSubjectRefJSON: %v", err)
	}
	canonical, err := ref.CanonicalBytes()
	if err != nil || string(canonical) != fixture.CanonicalUTF8 {
		t.Fatalf("canonical = %q / %v", canonical, err)
	}
	digest, err := ref.Digest()
	if err != nil || digest != fixture.Digest {
		t.Fatalf("digest = %q / %v", digest, err)
	}
}

func TestGeneratedIdentityFixtureManifest(t *testing.T) {
	expected := map[string]bool{
		"namespace-ref-canonical":                     true,
		"namespace-ref-nfc":                           true,
		"namespace-ref-extra-field":                   true,
		"namespace-ref-uppercase":                     true,
		"namespace-ref-decomposed":                    true,
		"namespace-ref-canonical-trailing-whitespace": true,
		"namespace-ref-canonical-escape":              true,
		"namespace-ref-lone-surrogate":                true,
		"subject-ref":                                 true,
		"subject-ref-canonical":                       true,
		"subject-ref-canonical-escape":                true,
		"subject-ref-digest-mismatch":                 true,
		"subject-ref-extra-field":                     true,
	}
	data := readFixtureBytes(t, "manifest.json")
	var manifest identityFixtureManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatalf("decode fixture manifest: %v", err)
	}
	seen := make(map[string]bool, len(expected))
	for _, fixture := range manifest.Cases {
		if fixture.Schema != "../schemas/namespace-ref.schema.json" && fixture.Schema != "../schemas/subject-ref.schema.json" {
			continue
		}
		fixture := fixture
		t.Run(fixture.Name, func(t *testing.T) {
			if !expected[fixture.Name] || seen[fixture.Name] {
				t.Fatalf("unexpected or duplicate identity fixture %q", fixture.Name)
			}
			seen[fixture.Name] = true
			raw, document := loadIdentityFixture(t, fixture.Instance, fixture.Document)
			if fixture.Schema == "../schemas/namespace-ref.schema.json" {
				replayNamespaceFixture(t, fixture.Name, fixture.ExpectedError, raw, document)
				return
			}
			replaySubjectFixture(t, fixture.Name, fixture.ExpectedError, raw, document)
		})
	}
	if len(seen) != len(expected) {
		t.Fatalf("identity fixture coverage = %d, want %d", len(seen), len(expected))
	}
}

func TestNamespaceRefStrictJSONAndNormalization(t *testing.T) {
	tests := []struct {
		name string
		json string
		base error
		code string
	}{
		{"unknown", `{"namespace":"cloud-agents","kind":"project","id":"alpha","tenant":"secret"}`, ErrInvalidIdentityJSON, "UNKNOWN_FIELD"},
		{"duplicate", `{"namespace":"cloud-agents","kind":"project","id":"alpha","id":"beta"}`, ErrInvalidIdentityJSON, "DUPLICATE_FIELD"},
		{"missing", `{"namespace":"cloud-agents","kind":"project"}`, ErrInvalidIdentityJSON, "MISSING_FIELD"},
		{"non-string", `{"namespace":"cloud-agents","kind":"project","id":1}`, ErrInvalidIdentityJSON, "INVALID_FIELD_TYPE"},
		{"trailing", `{"namespace":"cloud-agents","kind":"project","id":"alpha"}[]`, ErrInvalidIdentityJSON, "TRAILING_JSON"},
		{"uppercase", `{"namespace":"Cloud-Agents","kind":"project","id":"alpha"}`, ErrInvalidNamespaceRef, "INVALID_NAMESPACE_REF_GRAMMAR"},
		{"decomposed", `{"namespace":"cloud-agents","kind":"project","id":"cafe\u0301"}`, ErrInvalidNamespaceRef, "NON_NFC_NAMESPACE_REF_ID"},
		{"lone surrogate", `{"namespace":"cloud-agents","kind":"project","id":"\ud800"}`, ErrInvalidIdentityJSON, "INVALID_UNICODE_SCALAR"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := DecodeNamespaceRefJSON([]byte(test.json))
			var validation *ValidationError
			if !errors.Is(err, test.base) || !errors.As(err, &validation) || validation.Code != test.code {
				t.Fatalf("error = %#v", err)
			}
		})
	}

	normalized, err := NormalizeNamespaceRef(NamespaceRef{
		Namespace: "cloud-agents",
		Kind:      "project",
		ID:        " cafe\u0301 ",
	})
	if err != nil || normalized.ID != " café " {
		t.Fatalf("normalized = %#v / %v", normalized, err)
	}

	maximum := NamespaceRef{Namespace: "cloud-agents", Kind: "project", ID: strings.Repeat("😀", 256)}
	if err := maximum.Validate(); err != nil {
		t.Fatalf("256-code-point id rejected: %v", err)
	}
	maximum.ID += "😀"
	if err := maximum.Validate(); err == nil {
		t.Fatal("257-code-point id accepted")
	}
	paired, err := DecodeNamespaceRefJSON([]byte(`{"namespace":"cloud-agents","kind":"project","id":"\ud83d\ude00"}`))
	if err != nil || paired.ID != "😀" {
		t.Fatalf("paired surrogate = %#v / %v", paired, err)
	}
}

func TestSubjectRefExactIdentityAndStrictJSON(t *testing.T) {
	input := `{"kind":"user","issuer":"https://Issuer.Example/%7Etenant","subject":"Jose\u0301/用户"}`
	ref, err := DecodeSubjectRefJSON([]byte(input))
	if err != nil {
		t.Fatalf("DecodeSubjectRefJSON: %v", err)
	}
	canonical, _ := ref.CanonicalBytes()
	if string(canonical) != `{"issuer":"https://Issuer.Example/%7Etenant","kind":"user","subject":"José/用户"}` {
		t.Fatalf("canonical = %q", canonical)
	}
	changed := ref
	changed.Issuer = "https://issuer.example/%7etenant"
	left, _ := ref.Digest()
	right, _ := changed.Digest()
	if left == right {
		t.Fatal("issuer normalization changed exact SubjectRef identity")
	}
	for _, invalid := range []string{
		`{"kind":"user","issuer":"relative","subject":"alpha"}`,
		`{"kind":"user","issuer":"https://issuer.example/%zz","subject":"alpha"}`,
		`{"kind":"admin","issuer":"https://issuer.example/","subject":"alpha"}`,
		`{"kind":"user","issuer":"https://issuer.example/","subject":"alpha","extra":"value"}`,
	} {
		if _, err := DecodeSubjectRefJSON([]byte(invalid)); err == nil {
			t.Fatalf("accepted invalid SubjectRef %s", invalid)
		}
	}
	controls := SubjectRef{
		Kind:    SubjectKindUser,
		Issuer:  "https://identity.example.test/",
		Subject: "a\x00\b\t\n\f\r\x1f\"\\中",
	}
	controlCanonical, err := controls.CanonicalBytes()
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(controlCanonical), `{"issuer":"https://identity.example.test/","kind":"user","subject":"a\u0000\b\t\n\f\r\u001f\"\\中"}`; got != want {
		t.Fatalf("canonical controls = %q, want %q", got, want)
	}
}

func replayNamespaceFixture(t *testing.T, name, expectedError string, raw json.RawMessage, document identityFixtureDocument) {
	t.Helper()
	ref, err := DecodeNamespaceRefJSON(raw)
	if expectedError == "" {
		if err != nil {
			t.Fatalf("DecodeNamespaceRefJSON: %v", err)
		}
		assertNamespaceFixtureOutputs(t, ref, document)
		return
	}
	if expectedError == "CANONICAL_NAMESPACE_REF_MISMATCH" {
		if err != nil {
			t.Fatalf("DecodeNamespaceRefJSON: %v", err)
		}
		canonical, canonicalErr := ref.CanonicalBytes()
		if canonicalErr != nil || string(canonical) == document.CandidateCanonicalUTF8 {
			t.Fatalf("%s candidate canonical unexpectedly matched: %q / %v", name, canonical, canonicalErr)
		}
		return
	}
	assertValidationCode(t, err, expectedError)
}

func replaySubjectFixture(t *testing.T, name, expectedError string, raw json.RawMessage, document identityFixtureDocument) {
	t.Helper()
	ref, err := DecodeSubjectRefJSON(raw)
	if expectedError == "" {
		if err != nil {
			t.Fatalf("DecodeSubjectRefJSON: %v", err)
		}
		assertSubjectFixtureOutputs(t, ref, document)
		return
	}
	if expectedError == "CANONICAL_SUBJECT_REF_MISMATCH" {
		if err != nil {
			t.Fatalf("DecodeSubjectRefJSON: %v", err)
		}
		canonical, canonicalErr := ref.CanonicalBytes()
		if canonicalErr != nil || string(canonical) == document.CanonicalUTF8 {
			t.Fatalf("%s candidate canonical unexpectedly matched: %q / %v", name, canonical, canonicalErr)
		}
		return
	}
	if expectedError == "CANONICAL_SUBJECT_REF_DIGEST_MISMATCH" {
		if err != nil {
			t.Fatalf("DecodeSubjectRefJSON: %v", err)
		}
		digest, digestErr := ref.Digest()
		if digestErr != nil || digest == document.Digest {
			t.Fatalf("%s candidate digest unexpectedly matched: %q / %v", name, digest, digestErr)
		}
		return
	}
	assertValidationCode(t, err, expectedError)
}

func assertNamespaceFixtureOutputs(t *testing.T, ref NamespaceRef, document identityFixtureDocument) {
	t.Helper()
	if document.CanonicalUTF8 != "" {
		canonical, err := ref.CanonicalBytes()
		if err != nil || string(canonical) != document.CanonicalUTF8 {
			t.Fatalf("canonical = %q / %v, want %q", canonical, err, document.CanonicalUTF8)
		}
	}
	if document.Digest != "" {
		digest, err := ref.Digest()
		if err != nil || digest != document.Digest {
			t.Fatalf("digest = %q / %v, want %q", digest, err, document.Digest)
		}
	}
	if document.URN != "" {
		urn, err := ref.URN()
		if err != nil || urn != document.URN {
			t.Fatalf("urn = %q / %v, want %q", urn, err, document.URN)
		}
	}
}

func assertSubjectFixtureOutputs(t *testing.T, ref SubjectRef, document identityFixtureDocument) {
	t.Helper()
	if document.CanonicalUTF8 != "" {
		canonical, err := ref.CanonicalBytes()
		if err != nil || string(canonical) != document.CanonicalUTF8 {
			t.Fatalf("canonical = %q / %v, want %q", canonical, err, document.CanonicalUTF8)
		}
	}
	if document.Digest != "" {
		digest, err := ref.Digest()
		if err != nil || digest != document.Digest {
			t.Fatalf("digest = %q / %v, want %q", digest, err, document.Digest)
		}
	}
}

func assertValidationCode(t *testing.T, err error, code string) {
	t.Helper()
	var validation *ValidationError
	if !errors.As(err, &validation) || validation.Code != code {
		t.Fatalf("error = %#v, want %s", err, code)
	}
}

func loadIdentityFixture(t *testing.T, instance, document string) (json.RawMessage, identityFixtureDocument) {
	t.Helper()
	if document == "" {
		return readFixtureBytes(t, instance), identityFixtureDocument{}
	}
	data := readFixtureBytes(t, document)
	var fixture identityFixtureDocument
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatalf("decode %s: %v", document, err)
	}
	return fixture.Instance, fixture
}

func readCanonicalFixture(t *testing.T, name string) canonicalFixture {
	t.Helper()
	data := readFixtureBytes(t, name)
	var fixture canonicalFixture
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatalf("decode %s: %v", name, err)
	}
	return fixture
}

func readFixtureBytes(t *testing.T, name string) []byte {
	t.Helper()
	path := filepath.Join("..", "..", "..", "..", "..", "contracts", "common", "v1alpha1", "fixtures", name)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return data
}
