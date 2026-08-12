package migration

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"path/filepath"
	"sort"
	"testing"
)

func TestEvidenceAndLineageGoldenFramesDecodeStrictly(t *testing.T) {
	t.Parallel()
	root := filepath.Join(migrationRoot(t), "fixtures", "projection", "golden")
	for _, fixture := range []struct {
		path    string
		lineage bool
	}{
		{"evidence-record-chain-v1.json", false},
		{"evidence-ambiguous-chain-v1.json", false},
		{"lineage-index-chain-v1.json", true},
	} {
		t.Run(fixture.path, func(t *testing.T) {
			for index, raw := range fixtureArrayMembers(t, filepath.Join(root, fixture.path), "frames") {
				if fixture.lineage {
					var frame LineageIndexFrame
					if _, err := DecodeStrict(raw, &frame); err != nil {
						t.Fatalf("frame %d strict decode: %v", index, err)
					}
					if err := frame.Validate(); err != nil {
						t.Fatalf("frame %d validate: %v", index, err)
					}
					assertCanonicalLineageFrameDecode(t, raw, frame)
					continue
				}
				var frame EvidenceFrame
				if _, err := DecodeStrict(raw, &frame); err != nil {
					t.Fatalf("frame %d strict decode: %v", index, err)
				}
				if err := frame.Validate(); err != nil {
					t.Fatalf("frame %d validate: %v", index, err)
				}
				assertCanonicalEvidenceFrameDecode(t, raw, frame)
			}
		})
	}
}

type framingFaultFixture struct {
	Name            string  `json:"name"`
	RawHex          string  `json:"raw_hex"`
	ExpectedError   *string `json:"expected_error"`
	CanonicalSize   *uint64 `json:"expected_canonical_size_bytes"`
	LengthPrefixHex *string `json:"expected_length_prefix_hex"`
	FramedSize      *uint64 `json:"expected_framed_size_bytes"`
	FramedSHA256    *Digest `json:"expected_framed_sha256"`
}

func TestFramingFaultFixtureRoutesEveryCase(t *testing.T) {
	t.Parallel()
	fixture := fixtureObject(t, migrationFixturePath(t, "negative/evidence-framing-faults-v1.json"))
	cases, ok := fixture["cases"].([]JSONValue)
	if !ok || len(cases) != 7 {
		t.Fatal("framing fixture must contain seven cases")
	}
	for _, rawCase := range cases {
		caseObject := fixtureObjectValue(t, rawCase, "framing case")
		testCase := framingFaultFixture{}
		testCase.Name, _ = caseObject["name"].(string)
		testCase.RawHex, _ = caseObject["raw_hex"].(string)
		if value, ok := caseObject["expected_error"].(string); ok {
			testCase.ExpectedError = &value
		}
		if value, ok := caseObject["expected_canonical_size_bytes"].(uint64); ok {
			testCase.CanonicalSize = &value
		}
		if value, ok := caseObject["expected_length_prefix_hex"].(string); ok {
			testCase.LengthPrefixHex = &value
		}
		if value, ok := caseObject["expected_framed_size_bytes"].(uint64); ok {
			testCase.FramedSize = &value
		}
		if value, ok := caseObject["expected_framed_sha256"].(string); ok {
			digest, err := ParseDigest(value)
			if err != nil {
				t.Fatal(err)
			}
			testCase.FramedSHA256 = &digest
		}
		t.Run(testCase.Name, func(t *testing.T) {
			framed, err := hex.DecodeString(testCase.RawHex)
			if err != nil {
				t.Fatal(err)
			}
			frame, decodeErr := DecodeCanonicalEvidenceFrame(framed)
			if testCase.ExpectedError != nil {
				if decodeErr == nil {
					t.Fatal("negative framing case was accepted")
				}
				return
			}
			if decodeErr != nil || frame == nil {
				t.Fatalf("canonical framing reference: %v", decodeErr)
			}
			if testCase.CanonicalSize == nil || testCase.LengthPrefixHex == nil || testCase.FramedSize == nil || testCase.FramedSHA256 == nil || uint64(len(framed)) != *testCase.FramedSize || uint64(len(framed)-8) != *testCase.CanonicalSize || hex.EncodeToString(framed[:8]) != *testCase.LengthPrefixHex || DigestBytes(framed) != *testCase.FramedSHA256 {
				t.Fatal("canonical framing same-bits metadata mismatch")
			}
		})
	}
}

func TestLineageCheckpointFramedLimitIsExact16KiB(t *testing.T) {
	t.Parallel()
	maximum := lineageRecordFrameLimits[LineageRecordGenerationCheckpoint]
	if maximum != 16<<10 {
		t.Fatalf("checkpoint framed maximum = %d", maximum)
	}
	if err := validateFramedSizeLimit(maximum, maxLineageFrameBytes, maximum); err != nil {
		t.Fatalf("exact 16 KiB rejected: %v", err)
	}
	if err := validateFramedSizeLimit(maximum+1, maxLineageFrameBytes, maximum); err == nil {
		t.Fatal("16 KiB + 1 accepted")
	}
}

func TestCanonicalFrameDecoderRejectsRawJSONFaults(t *testing.T) {
	t.Parallel()
	root := filepath.Join(migrationRoot(t), "fixtures", "projection", "negative")
	for _, fixture := range []struct {
		path    string
		lineage bool
	}{
		{"evidence-frame-duplicate.raw", false},
		{"evidence-nested-record-duplicate.raw", false},
		{"lineage-frame-duplicate.raw", true},
	} {
		raw := mustRead(t, filepath.Join(root, fixture.path))
		framed := lengthPrefix(raw)
		var err error
		if fixture.lineage {
			_, err = DecodeCanonicalLineageFrame(framed)
		} else {
			_, err = DecodeCanonicalEvidenceFrame(framed)
		}
		if err == nil {
			t.Fatalf("accepted %s", fixture.path)
		}
	}

	canonical := fixtureArrayMembers(t, filepath.Join(migrationRoot(t), "fixtures", "projection", "golden", "evidence-record-chain-v1.json"), "frames")[0]
	for name, payload := range map[string][]byte{
		"negative":      bytes.Replace(canonical, []byte(`"sequence":0`), []byte(`"sequence":-1`), 1),
		"fraction":      bytes.Replace(canonical, []byte(`"sequence":0`), []byte(`"sequence":0.5`), 1),
		"exponent":      bytes.Replace(canonical, []byte(`"sequence":0`), []byte(`"sequence":0e0`), 1),
		"negative-zero": bytes.Replace(canonical, []byte(`"sequence":0`), []byte(`"sequence":-0`), 1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := DecodeCanonicalEvidenceFrame(lengthPrefix(payload)); err == nil {
				t.Fatal("accepted non-canonical numeric spelling")
			}
		})
	}
}

func assertCanonicalEvidenceFrameDecode(t *testing.T, canonical []byte, want EvidenceFrame) {
	t.Helper()
	got, err := DecodeCanonicalEvidenceFrame(lengthPrefix(canonical))
	if err != nil {
		t.Fatal(err)
	}
	if !canonicalEqual(*got, want) {
		t.Fatal("canonical evidence decoder changed frame bits")
	}
}

func assertCanonicalLineageFrameDecode(t *testing.T, canonical []byte, want LineageIndexFrame) {
	t.Helper()
	got, err := DecodeCanonicalLineageFrame(lengthPrefix(canonical))
	if err != nil {
		t.Fatal(err)
	}
	if !canonicalEqual(*got, want) {
		t.Fatal("canonical lineage decoder changed frame bits")
	}
}

func lengthPrefix(payload []byte) []byte {
	framed := make([]byte, len(payload)+8)
	binary.BigEndian.PutUint64(framed[:8], uint64(len(payload)))
	copy(framed[8:], payload)
	return framed
}

func fixtureArrayMembers(t *testing.T, path, field string) [][]byte {
	t.Helper()
	value, err := ParseStrictJSON(mustRead(t, path))
	if err != nil {
		t.Fatal(err)
	}
	object, ok := value.(map[string]JSONValue)
	if !ok {
		t.Fatal("fixture is not an object")
	}
	array, ok := object[field].([]JSONValue)
	if !ok {
		t.Fatalf("fixture field %s is not an array", field)
	}
	result := make([][]byte, len(array))
	for index, member := range array {
		result[index], err = CanonicalJSON(member)
		if err != nil {
			t.Fatal(err)
		}
	}
	return result
}

func migrationFixturePath(t *testing.T, relative string) string {
	t.Helper()
	return filepath.Join(migrationRoot(t), "fixtures", "projection", filepath.FromSlash(relative))
}

func fixtureObject(t *testing.T, path string) map[string]JSONValue {
	t.Helper()
	value, err := ParseStrictJSON(mustRead(t, path))
	if err != nil {
		t.Fatal(err)
	}
	return fixtureObjectValue(t, value, path)
}

func fixtureObjectValue(t *testing.T, value JSONValue, label string) map[string]JSONValue {
	t.Helper()
	object, ok := value.(map[string]JSONValue)
	if !ok {
		t.Fatalf("%s is not an object", label)
	}
	return object
}

func decodeFixtureValue(t *testing.T, value JSONValue, target any) []byte {
	t.Helper()
	canonical, err := CanonicalJSON(value)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeStrict(canonical, target); err != nil {
		t.Fatal(err)
	}
	return canonical
}

func fixtureDigest(t *testing.T, object map[string]JSONValue, field string) Digest {
	t.Helper()
	text, ok := object[field].(string)
	if !ok {
		t.Fatalf("%s is not a digest string", field)
	}
	digest, err := ParseDigest(text)
	if err != nil {
		t.Fatal(err)
	}
	return digest
}

func sortedJSONKeys(object map[string]JSONValue) []string {
	keys := make([]string, 0, len(object))
	for key := range object {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
