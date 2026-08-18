package migration

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"path/filepath"
	"sort"
	"strings"
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

func TestLineageCheckpointFramedLimitIsProfileBound(t *testing.T) {
	t.Parallel()
	profiles := []struct {
		name string
		id   string
		max  uint64
	}{
		{name: "v1", id: EvidenceLimitsProfile, max: 16 << 10},
		{name: "v2", id: LineageQuotaProfileV2, max: v2GenerationCheckpointMaximum},
	}
	for _, profile := range profiles {
		t.Run(profile.name, func(t *testing.T) {
			got, err := checkpointMaximumForProfile(profile.id)
			if err != nil || got != profile.max {
				t.Fatalf("profile maximum=%d err=%v, want %d", got, err, profile.max)
			}
			if err := validateFramedSizeLimit(profile.max, maxLineageFrameBytes, profile.max); err != nil {
				t.Fatalf("exact profile maximum rejected: %v", err)
			}
			if err := validateFramedSizeLimit(profile.max+1, maxLineageFrameBytes, profile.max); err == nil {
				t.Fatal("profile maximum + 1 accepted")
			}
		})
	}

	// The generic decoder retains the historical 16 KiB physical ceiling. A
	// v2 writer must still reject a valid checkpoint that fits that ceiling but
	// exceeds the selected 4 KiB closed quota.
	frames := fixtureArrayMembers(t, migrationFixturePath(t, "golden/lineage-index-chain-v1.json"), "frames")
	var oversized LineageIndexFrame
	for _, raw := range frames {
		var frame LineageIndexFrame
		if _, err := DecodeStrict(raw, &frame); err != nil {
			t.Fatal(err)
		}
		if frame.RecordKind == LineageRecordGenerationCheckpoint {
			oversized = frame
			break
		}
	}
	if oversized.Record.Checkpoint == nil {
		t.Fatal("golden lineage fixture has no checkpoint")
	}
	oversized.Record.Checkpoint.RecoveryState = strings.Repeat("x", 5000)
	var err error
	oversized.RecordDigest, err = oversized.ComputeDigest()
	if err != nil {
		t.Fatal(err)
	}
	v1Framed, err := EncodeCanonicalLineageFrame(oversized)
	if err != nil {
		t.Fatalf("historical 16 KiB encoder rejected fixture: %v", err)
	}
	if uint64(len(v1Framed)) <= v2GenerationCheckpointMaximum || uint64(len(v1Framed)) > lineageRecordFrameLimits[LineageRecordGenerationCheckpoint] {
		t.Fatalf("test checkpoint does not straddle profile ceilings: %d", len(v1Framed))
	}
	if _, err := encodeCanonicalLineageFrameForProfile(oversized, LineageQuotaProfileV2); !IsCode(err, CodeEvidenceJournalLimitExceeded) {
		t.Fatalf("v2 oversized checkpoint was accepted: %v", err)
	}
}

func TestProfileAwareCheckpointEncoderUsesInclusiveWireBoundary(t *testing.T) {
	t.Parallel()
	var base LineageIndexFrame
	for _, raw := range fixtureArrayMembers(t, migrationFixturePath(t, "golden/lineage-index-chain-v1.json"), "frames") {
		var frame LineageIndexFrame
		if _, err := DecodeStrict(raw, &frame); err != nil {
			t.Fatal(err)
		}
		if frame.RecordKind == LineageRecordGenerationCheckpoint {
			base = frame
			break
		}
	}
	if base.Record.Checkpoint == nil {
		t.Fatal("golden lineage fixture has no checkpoint")
	}

	// RecoveryState is an unconstrained, canonical string field. It gives this
	// test a deterministic way to construct a valid frame at the exact wire
	// boundary without weakening the typed checkpoint validator.
	var exact LineageIndexFrame
	var exactFramed []byte
	for size := 0; size <= 16<<10; size++ {
		candidate := cloneProjectionValue(base)
		candidate.Record.Checkpoint.RecoveryState = strings.Repeat("x", size)
		var err error
		candidate.RecordDigest, err = candidate.ComputeDigest()
		if err != nil {
			t.Fatal(err)
		}
		framed, err := EncodeCanonicalLineageFrame(candidate)
		if err != nil {
			continue
		}
		if len(framed) == int(v2GenerationCheckpointMaximum) {
			exact, exactFramed = candidate, framed
			break
		}
	}
	if len(exactFramed) != int(v2GenerationCheckpointMaximum) {
		t.Fatalf("could not construct an exact v2 checkpoint boundary: %d", len(exactFramed))
	}
	encoded, err := encodeCanonicalLineageFrameForProfile(exact, LineageQuotaProfileV2)
	if err != nil || len(encoded) != int(v2GenerationCheckpointMaximum) {
		t.Fatalf("exact v2 checkpoint was not accepted: len=%d err=%v", len(encoded), err)
	}

	plusOne := cloneProjectionValue(exact)
	plusOne.Record.Checkpoint.RecoveryState += "x"
	plusOne.RecordDigest, err = plusOne.ComputeDigest()
	if err != nil {
		t.Fatal(err)
	}
	if framed, err := EncodeCanonicalLineageFrame(plusOne); err != nil || len(framed) != int(v2GenerationCheckpointMaximum)+1 {
		t.Fatalf("test +1 frame did not cross the boundary: len=%d err=%v", len(framed), err)
	}
	if _, err := encodeCanonicalLineageFrameForProfile(plusOne, LineageQuotaProfileV2); !IsCode(err, CodeEvidenceJournalLimitExceeded) {
		t.Fatalf("v2 checkpoint boundary +1 was accepted: %v", err)
	}
}

func TestGeneratedV2CheckpointFullShapeFits4096(t *testing.T) {
	t.Parallel()
	migration := "000006"
	attempt := uint32(3)
	digests := []Digest{
		testDigest("checkpoint-execution-lineage"), testDigest("checkpoint-journal"),
		testDigest("checkpoint-runner"), testDigest("checkpoint-schema"),
		testDigest("checkpoint-tail"), testDigest("checkpoint-intent"),
		testDigest("checkpoint-intermediate"), testDigest("checkpoint-commit"),
		testDigest("checkpoint-terminal"), testDigest("checkpoint-previous-attempt"),
		testDigest("checkpoint-state"), testDigest("checkpoint-previous-checkpoint"),
	}
	gen := generationIdentity{
		executionLineageDigest:         digests[0],
		journalIdentityDigest:          digests[1],
		runnerProjectionDecisionDigest: digests[2],
		schemaBundleDigest:             digests[3],
	}
	previousIndex := digests[11]
	checkpointSummary := evidenceJournalSummary{
		recoveryState:                        "ambiguous_unresolved",
		migrationID:                          &migration,
		attemptIndex:                         &attempt,
		lastStatementIntentRecordDigest:      digestPointer(digests[5]),
		lastIntermediateEvidenceRecordDigest: digestPointer(digests[6]),
		lastCommitIntentRecordDigest:         digestPointer(digests[7]),
		lastTerminalDigest:                   digestPointer(digests[8]),
		previousAttemptTerminalDigest:        digestPointer(digests[9]),
		lastIntermediateStateDigest:          digestPointer(digests[10]),
	}
	cursor := JournalCursor{
		generation:                       gen,
		nextSequence:                     2,
		previousRecordDigest:             digestPointer(digests[4]),
		lineageIndexNextSequence:         7,
		lineageIndexPreviousRecordDigest: previousIndex,
		latestCheckpointRecordDigest:     digestPointer(previousIndex),
	}
	frame := EvidenceFrame{Sequence: 2, PreviousRecordDigest: digestPointer(digests[4]), RecordDigest: digests[4]}
	_, framed, err := buildGenerationJournalCheckpoint(gen, cursor, frame, checkpointSummary, LineageQuotaProfileV2)
	if err != nil {
		t.Fatal(err)
	}
	if uint64(len(framed)) > v2GenerationCheckpointMaximum {
		t.Fatalf("full generated v2 checkpoint exceeds 4096 bytes: %d", len(framed))
	}
	if len(framed) == 0 {
		t.Fatal("generated v2 checkpoint was empty")
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
