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
		{name: "v3", id: LineageQuotaProfileV3, max: v2GenerationCheckpointMaximum},
		{name: "v4", id: LineageQuotaProfileV4, max: v2GenerationCheckpointMaximum},
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
	for _, profile := range []string{LineageQuotaProfileV2, LineageQuotaProfileV3, LineageQuotaProfileV4} {
		if _, err := encodeCanonicalLineageFrameForProfile(oversized, profile); !IsCode(err, CodeEvidenceJournalLimitExceeded) {
			t.Fatalf("%s oversized checkpoint was accepted: %v", profile, err)
		}
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
	for _, profile := range []string{LineageQuotaProfileV2, LineageQuotaProfileV3, LineageQuotaProfileV4} {
		encoded, err := encodeCanonicalLineageFrameForProfile(exact, profile)
		if err != nil || len(encoded) != int(v2GenerationCheckpointMaximum) {
			t.Fatalf("exact %s checkpoint was not accepted: len=%d err=%v", profile, len(encoded), err)
		}
	}

	plusOne := cloneProjectionValue(exact)
	plusOne.Record.Checkpoint.RecoveryState += "x"
	plusOneDigest, err := plusOne.ComputeDigest()
	if err != nil {
		t.Fatal(err)
	}
	plusOne.RecordDigest = plusOneDigest
	if framed, err := EncodeCanonicalLineageFrame(plusOne); err != nil || len(framed) != int(v2GenerationCheckpointMaximum)+1 {
		t.Fatalf("test +1 frame did not cross the boundary: len=%d err=%v", len(framed), err)
	}
	for _, profile := range []string{LineageQuotaProfileV2, LineageQuotaProfileV3, LineageQuotaProfileV4} {
		if _, err := encodeCanonicalLineageFrameForProfile(plusOne, profile); !IsCode(err, CodeEvidenceJournalLimitExceeded) {
			t.Fatalf("%s checkpoint boundary +1 was accepted: %v", profile, err)
		}
	}
}

func TestGeneratedV2V3AndV4CheckpointFullShapeFits4096(t *testing.T) {
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
	for _, profile := range []string{LineageQuotaProfileV2, LineageQuotaProfileV3, LineageQuotaProfileV4} {
		_, framed, err := buildGenerationJournalCheckpoint(gen, cursor, frame, checkpointSummary, profile)
		if err != nil {
			t.Fatal(err)
		}
		if uint64(len(framed)) > v2GenerationCheckpointMaximum {
			t.Fatalf("full generated %s checkpoint exceeds 4096 bytes: %d", profile, len(framed))
		}
		if len(framed) == 0 {
			t.Fatalf("generated %s checkpoint was empty", profile)
		}
	}
}

func TestLineageQuotaProfilesV3AndV4UseClosedInclusiveReservationLimits(t *testing.T) {
	t.Parallel()
	frames := fixtureArrayMembers(t, migrationFixturePath(t, "golden/lineage-index-chain-v1.json"), "frames")
	var base GenerationReserved
	for _, raw := range frames {
		var frame LineageIndexFrame
		if _, err := DecodeStrict(raw, &frame); err != nil {
			t.Fatal(err)
		}
		if frame.RecordKind == LineageRecordGenerationReserved && frame.Record.Reserved != nil {
			base = cloneProjectionValue(*frame.Record.Reserved)
			break
		}
	}
	if base.JournalIdentityDigest == "" {
		t.Fatal("golden lineage fixture has no reservation")
	}
	build := func(profile string, records, bytes uint64, segments uint32) GenerationReserved {
		reserved := cloneProjectionValue(base)
		reserved.ReservedRecords = records
		reserved.ReservedBytes = bytes
		reserved.ReservedSegments = segments
		reserved.PlannedSegment0Header.LimitsProfile = profile
		reserved.PlannedSegment0Header.ReservedRecords = records
		reserved.PlannedSegment0Header.ReservedBytes = bytes
		reserved.PlannedSegment0Header.ReservedSegments = segments
		var err error
		reserved.QuotaReservationDigest, err = QuotaReservationDigest(reserved)
		if err != nil {
			t.Fatal(err)
		}
		reserved.PlannedSegment0Header.QuotaReservationDigest = reserved.QuotaReservationDigest
		header := reserved.PlannedSegment0Header
		headerFrame := EvidenceFrame{FormatVersion: EvidenceFrameFormat, Sequence: 0, RecordKind: EvidenceRecordHeader, Record: EvidenceRecord{Header: &header}}
		reserved.ExpectedSegment0HeaderDigest, err = headerFrame.ComputeDigest()
		if err != nil {
			t.Fatal(err)
		}
		return reserved
	}

	v3 := build(LineageQuotaProfileV3, maxEvidenceReservedRecords, maxSupportedEvidenceReservedBytes, maxSupportedEvidenceReservedSegments)
	if err := v3.Validate(); err != nil {
		t.Fatalf("exact v3 reservation limits were rejected: %v", err)
	}
	if maxV4EvidenceReservedBytes != uint64(528<<20) {
		t.Fatalf("v4 combined reservation maximum drifted: %d", maxV4EvidenceReservedBytes)
	}
	v4 := build(LineageQuotaProfileV4, maxEvidenceReservedRecords, maxV4EvidenceReservedBytes, maxSupportedEvidenceReservedSegments)
	if err := v4.Validate(); err != nil {
		t.Fatalf("exact v4 reservation limits were rejected: %v", err)
	}
	for name, fault := range map[string]GenerationReserved{
		"records":  build(LineageQuotaProfileV4, maxEvidenceReservedRecords+1, maxV4EvidenceReservedBytes, maxSupportedEvidenceReservedSegments),
		"bytes":    build(LineageQuotaProfileV4, maxEvidenceReservedRecords, maxV4EvidenceReservedBytes+1, maxSupportedEvidenceReservedSegments),
		"segments": build(LineageQuotaProfileV4, maxEvidenceReservedRecords, maxV4EvidenceReservedBytes, maxSupportedEvidenceReservedSegments+1),
	} {
		t.Run("v4_"+name+"_plus_one", func(t *testing.T) {
			if err := fault.Validate(); err == nil {
				t.Fatal("v4 inclusive maximum + 1 was accepted")
			}
		})
	}
	if err := build(LineageQuotaProfileV3, maxEvidenceReservedRecords, maxV4EvidenceReservedBytes, maxSupportedEvidenceReservedSegments).Validate(); err == nil {
		t.Fatal("v3 accepted v4-only combined reservation capacity")
	}
	for name, fault := range map[string]GenerationReserved{
		"records":  build(LineageQuotaProfileV3, maxEvidenceReservedRecords+1, maxSupportedEvidenceReservedBytes, maxSupportedEvidenceReservedSegments),
		"bytes":    build(LineageQuotaProfileV3, maxEvidenceReservedRecords, maxSupportedEvidenceReservedBytes+1, maxSupportedEvidenceReservedSegments),
		"segments": build(LineageQuotaProfileV3, maxEvidenceReservedRecords, maxSupportedEvidenceReservedBytes, maxSupportedEvidenceReservedSegments+1),
	} {
		t.Run("v3_"+name+"_plus_one", func(t *testing.T) {
			if err := fault.Validate(); err == nil {
				t.Fatal("v3 inclusive maximum + 1 was accepted")
			}
		})
	}

	v2 := build(LineageQuotaProfileV2, maxEvidenceReservedRecords, maxEvidenceReservedBytes, maxEvidenceReservedSegments)
	if err := v2.Validate(); err != nil {
		t.Fatalf("exact v2 reservation limits were rejected: %v", err)
	}
	for name, fault := range map[string]GenerationReserved{
		"segments": build(LineageQuotaProfileV2, maxEvidenceReservedRecords, maxEvidenceReservedBytes, maxEvidenceReservedSegments+1),
		"bytes":    build(LineageQuotaProfileV2, maxEvidenceReservedRecords, maxEvidenceReservedBytes+1, maxEvidenceReservedSegments),
	} {
		if err := fault.Validate(); err == nil {
			t.Fatalf("v2 accepted v3-only %s capacity", name)
		}
	}

	v2Digest, err := QuotaReservationDigest(build(LineageQuotaProfileV2, 64, 1<<20, 1))
	if err != nil {
		t.Fatal(err)
	}
	v3Digest, err := QuotaReservationDigest(build(LineageQuotaProfileV3, 64, 1<<20, 1))
	if err != nil {
		t.Fatal(err)
	}
	v4Digest, err := QuotaReservationDigest(build(LineageQuotaProfileV4, 64, 1<<20, 1))
	if err != nil {
		t.Fatal(err)
	}
	if v2Digest != "sha256:0581ed618246771293584fca2153c7407d02d70a1f0dc4c2a129b6b8c72a20c5" {
		t.Fatalf("historical v2 quota reservation digest drifted: %s", v2Digest)
	}
	if v3Digest != "sha256:861b72e16f8b5fa0946b5466a3930297530dd99d654631bb7e601dc9f503fc2f" {
		t.Fatalf("v3 quota reservation digest drifted: %s", v3Digest)
	}
	if v4Digest != "sha256:4f37c455b7814cd0c65d80ecb71d1d27de3b8a7ff289d640f896cc4616e9fab9" {
		t.Fatalf("v4 quota reservation digest drifted: %s", v4Digest)
	}
	if v2Digest == v3Digest || v2Digest == v4Digest || v3Digest == v4Digest {
		t.Fatal("versioned quota reservation domains collided")
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
