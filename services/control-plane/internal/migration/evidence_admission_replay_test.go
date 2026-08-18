package migration

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"hash"
	"math"
	"os"
	"sort"
	"strings"
	"testing"
	"unsafe"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/evidencefs"
)

func TestAdmissionDecodeGoldenFramesSameBits(t *testing.T) {
	t.Parallel()
	evidenceFixture := fixtureObject(t, migrationFixturePath(t, "golden/evidence-record-chain-v1.json"))
	evidence := decodeEvidenceFrames(t, evidenceFixture["frames"])
	var evidenceRaw []byte
	for _, frame := range evidence {
		encoded, err := EncodeCanonicalEvidenceFrame(frame)
		if err != nil {
			t.Fatal(err)
		}
		evidenceRaw = append(evidenceRaw, encoded...)
	}
	accumulator := newEvidenceStructuralAccumulator(nil, nil)
	if err := accumulator.beginSegment(); err != nil {
		t.Fatal(err)
	}
	records, tail, first, err := streamAdmissionEvidenceFrames(evidenceRaw, accumulator)
	if err != nil || records != uint64(len(evidence)) || tail != evidence[len(evidence)-1].RecordDigest || !canonicalEqual(first, evidence[0]) {
		t.Fatalf("evidence same-bits replay failed: %v", err)
	}

	lineageFixture := fixtureObject(t, migrationFixturePath(t, "golden/lineage-index-chain-v1.json"))
	lineage := decodeLineageFrames(t, lineageFixture["frames"])
	var lineageRaw []byte
	for _, frame := range lineage {
		encoded, err := EncodeCanonicalLineageFrame(frame)
		if err != nil {
			t.Fatal(err)
		}
		lineageRaw = append(lineageRaw, encoded...)
	}
	decodedLineage, err := decodeAdmissionLineageFrames(lineageRaw)
	if err != nil || !canonicalEqual(lineage, decodedLineage) {
		t.Fatalf("lineage same-bits replay failed: %v", err)
	}
}

func TestAdmissionLineageDecoderEnforcesSelectedCheckpointProfile(t *testing.T) {
	t.Parallel()
	fixture := fixtureObject(t, migrationFixturePath(t, "golden/lineage-index-chain-v1.json"))
	base := decodeLineageFrames(t, fixture["frames"])[:4]
	build := func(profile string) ([]byte, uint64) {
		frames := cloneProjectionValue(base)
		reserved := frames[1].Record.Reserved
		activated := frames[2].Record.Activated
		checkpoint := frames[3].Record.Checkpoint
		if reserved == nil || activated == nil || checkpoint == nil {
			t.Fatal("golden lineage fixture is missing the admission prefix")
		}

		header := cloneProjectionValue(reserved.PlannedSegment0Header)
		header.LimitsProfile = profile
		reserved.PlannedSegment0Header = header
		var err error
		reserved.QuotaReservationDigest, err = QuotaReservationDigest(*reserved)
		if err != nil {
			t.Fatal(err)
		}
		header.QuotaReservationDigest = reserved.QuotaReservationDigest
		reserved.PlannedSegment0Header = header
		headerFrame := EvidenceFrame{FormatVersion: EvidenceFrameFormat, Sequence: 0, RecordKind: EvidenceRecordHeader, Record: EvidenceRecord{Header: &header}}
		headerFrame.RecordDigest, err = headerFrame.ComputeDigest()
		if err != nil {
			t.Fatal(err)
		}
		reserved.ExpectedSegment0HeaderDigest = headerFrame.RecordDigest
		frames[1].RecordDigest, err = frames[1].ComputeDigest()
		if err != nil {
			t.Fatal(err)
		}
		if validateErr := frames[1].Validate(); validateErr != nil {
			t.Fatalf("%s reservation is invalid: %v", profile, validateErr)
		}

		activated.QuotaReservationDigest = reserved.QuotaReservationDigest
		activated.GenerationReservedRecordDigest = frames[1].RecordDigest
		activated.Segment0HeaderDigest = headerFrame.RecordDigest
		activated.InitialJournalTailDigest = headerFrame.RecordDigest
		frames[2].PreviousRecordDigest = digestPointer(frames[1].RecordDigest)
		frames[2].RecordDigest, err = frames[2].ComputeDigest()
		if err != nil {
			t.Fatal(err)
		}
		if validateErr := frames[2].Validate(); validateErr != nil {
			t.Fatalf("%s activation is invalid: %v", profile, validateErr)
		}

		checkpoint.RecoveryState = strings.Repeat("x", 5000)
		frames[3].PreviousRecordDigest = digestPointer(frames[2].RecordDigest)
		frames[3].RecordDigest, err = frames[3].ComputeDigest()
		if err != nil {
			t.Fatal(err)
		}
		if validateErr := frames[3].Validate(); validateErr != nil {
			t.Fatalf("%s checkpoint is not otherwise valid: %v", profile, validateErr)
		}

		var raw []byte
		var checkpointBytes uint64
		for _, frame := range frames {
			framed, encodeErr := EncodeCanonicalLineageFrame(frame)
			if encodeErr != nil {
				t.Fatal(encodeErr)
			}
			if frame.RecordKind == LineageRecordGenerationCheckpoint {
				checkpointBytes = uint64(len(framed))
			}
			raw = append(raw, framed...)
		}
		return raw, checkpointBytes
	}

	v1Raw, v1CheckpointBytes := build(EvidenceLimitsProfile)
	if v1CheckpointBytes <= v2GenerationCheckpointMaximum || v1CheckpointBytes > lineageRecordFrameLimits[LineageRecordGenerationCheckpoint] {
		t.Fatalf("checkpoint framed bytes=%d are outside the intended compatibility window", v1CheckpointBytes)
	}
	if frames, err := decodeAdmissionLineageFrames(v1Raw); err != nil || len(frames) != 4 {
		t.Fatalf("historical v1 checkpoint inside its 16 KiB maximum was rejected: %v", err)
	}

	v2Raw, v2CheckpointBytes := build(LineageQuotaProfileV2)
	if v2CheckpointBytes != v1CheckpointBytes {
		t.Fatalf("profile-only mutation changed checkpoint bytes: v1=%d v2=%d", v1CheckpointBytes, v2CheckpointBytes)
	}
	if _, err := decodeAdmissionLineageFrames(v2Raw); !IsCode(err, CodeEvidenceJournalCorrupt) {
		t.Fatalf("stored v2 checkpoint above its closed maximum was accepted: %v", err)
	}
}

func TestAdmissionFramedDecoderRejectsEveryPrefixBoundaryAndLengthFault(t *testing.T) {
	t.Parallel()
	fixture := fixtureObject(t, migrationFixturePath(t, "golden/evidence-record-chain-v1.json"))
	frame := decodeEvidenceFrames(t, fixture["frames"])[0]
	encoded, err := EncodeCanonicalEvidenceFrame(frame)
	if err != nil {
		t.Fatal(err)
	}
	for length := 0; length < len(encoded); length++ {
		if _, _, err := drainAdmissionEvidenceFrames(encoded[:length]); !IsCode(err, CodeEvidenceJournalCorrupt) {
			t.Fatalf("prefix %d accepted or wrong code: %v", length, err)
		}
	}
	if records, _, err := drainAdmissionEvidenceFrames(encoded); err != nil || records != 1 {
		t.Fatalf("exact frame rejected: %v", err)
	}
	for _, declared := range []uint64{maxEvidenceFrameBytes - 7, maxEvidenceFrameBytes - 8, math.MaxUint64} {
		fault := append([]byte(nil), encoded...)
		binary.BigEndian.PutUint64(fault[:8], declared)
		if _, _, err := drainAdmissionEvidenceFrames(fault); !IsCode(err, CodeEvidenceJournalCorrupt) {
			t.Fatalf("declared=%d accepted: %v", declared, err)
		}
	}
	trailing := append(append([]byte(nil), encoded...), 0)
	if _, _, err := drainAdmissionEvidenceFrames(trailing); !IsCode(err, CodeEvidenceJournalCorrupt) {
		t.Fatalf("trailing byte accepted: %v", err)
	}
	nonCanonical := append([]byte(nil), encoded...)
	nonCanonical = append(nonCanonical, ' ')
	binary.BigEndian.PutUint64(nonCanonical[:8], uint64(len(nonCanonical)-8))
	if _, _, err := drainAdmissionEvidenceFrames(nonCanonical); !IsCode(err, CodeEvidenceJournalCorrupt) {
		t.Fatalf("non-canonical JSON accepted: %v", err)
	}
	unknown := bytes.Replace(append([]byte(nil), encoded...), []byte(`"record_kind":"header"`), []byte(`"record_kind":"bogus!"`), 1)
	if bytes.Equal(unknown, encoded) {
		t.Fatal("fixture does not expose record kind")
	}
	if _, _, err := drainAdmissionEvidenceFrames(unknown); !IsCode(err, CodeEvidenceJournalCorrupt) {
		t.Fatalf("unknown record kind accepted: %v", err)
	}
	digestFault := append([]byte(nil), encoded...)
	digestFault[len(digestFault)-3] ^= 1
	if _, _, err := drainAdmissionEvidenceFrames(digestFault); !IsCode(err, CodeEvidenceJournalCorrupt) {
		t.Fatalf("digest/non-canonical fault accepted: %v", err)
	}
}

func TestAdmissionOrderingTargetAndObjectFactsAreClosed(t *testing.T) {
	t.Parallel()
	a, b := [32]byte{1}, [32]byte{2}
	if !strictRawDigestOrder([][32]byte{a, b}) || strictRawDigestOrder([][32]byte{b, a}) || strictRawDigestOrder([][32]byte{a, a}) {
		t.Fatal("strict raw digest ordering is not closed")
	}
	if rawDigestIndex([][32]byte{a, b}, b) != 1 || rawDigestIndex([][32]byte{a, b}, [32]byte{3}) != -1 {
		t.Fatal("target membership search mismatch")
	}
	if !rawDigestContains([][32]byte{b, a}, a) {
		t.Fatal("unordered corrupt inventory lost target membership")
	}
	if err := validateAdmissionTargetXOR(a, b, true, nil); err != nil {
		t.Fatal(err)
	}
	if err := validateAdmissionTargetXOR(a, b, false, nil); !IsCode(err, CodeEvidenceJournalCorrupt) {
		t.Fatalf("absent target without fact accepted: %v", err)
	}
	objects := []admissionReplayObject{{temporary: true, digest: digestString(b)}, {digest: digestString(b)}, {digest: digestString(a)}}
	sortAdmissionObjects(objects)
	if objects[0].temporary || objects[1].temporary || !objects[2].temporary || objects[0].digest >= objects[1].digest {
		t.Fatal("canonical object ordering mismatch")
	}
}

func TestAdmissionObjectReferencePurposeDedupeAndConflict(t *testing.T) {
	t.Parallel()
	digest := DigestBytes([]byte("same-content"))
	common := admissionObjectReference{headerDigest: projectionTestDigest, decision: projectionTestDigest, manifest: projectionTestDigest, schema: projectionTestDigest, records: 1, bytes: 1, segments: 1}
	references := []admissionObjectReference{
		common,
		common,
	}
	references[0].kind, references[0].digest, references[0].sizeBytes = durableRuntimeContentObject, digest, 12
	references[1].kind, references[1].digest, references[1].sizeBytes = durableDecisionRecoveryContentObject, digest, 12
	_, replayRefs, needs, err := replayAdmissionObjects(t.Context(), nil, nil, references)
	if err != nil || len(replayRefs) != 2 || len(needs) != 2 || needs[0].kind == needs[1].kind {
		t.Fatalf("same digest dual purpose was not retained: needs=%v err=%v", needs, err)
	}
	conflict := cloneProjectionValue(references)
	conflict[1].sizeBytes++
	if _, _, _, err := replayAdmissionObjects(t.Context(), nil, nil, conflict); !IsCode(err, CodeEvidenceJournalCorrupt) {
		t.Fatalf("same digest conflicting size accepted: %v", err)
	}
}

func TestAdmissionRuntimeObjectInspectionBindsClosedBundleAndReservation(t *testing.T) {
	t.Parallel()
	raw, manifest := buildCheckedInRuntimeTar(t)
	inspection, err := inspectAdmissionRuntimeObject(raw)
	if err != nil {
		t.Fatal(err)
	}
	if inspection.manifestDigest != manifest.ManifestDigest || inspection.schemaBundleDigest != manifest.SchemaBundleDigest || inspection.maxAttempts != manifest.ExecutionPolicy.MaxAttempts || len(inspection.statementCounts) != len(manifest.SchemaBundle.Migrations) || inspection.reservation.ReservedJournalBytes == 0 || inspection.reservation.ReservedIndexBytes == 0 || inspection.digest() == ([32]byte{}) {
		t.Fatalf("runtime inspection is incomplete: %+v", inspection)
	}
	mutated := append([]byte(nil), raw...)
	mutated[len(mutated)/2] ^= 1
	if _, err := inspectAdmissionRuntimeObject(mutated); !IsCode(err, CodeEvidenceJournalCorrupt) {
		t.Fatalf("mutated registered runtime accepted: %v", err)
	}

	generation := admissionReplayGeneration{journalID: DigestBytes([]byte("journal")), schemaBundleDigest: manifest.SchemaBundleDigest, reservedRecords: inspection.reservation.ReservedRecords, reservedBytes: inspection.reservation.ReservedBytes, reservedSegments: inspection.reservation.ReservedSegments, header: &admissionReplayHeaderFacts{manifestDigest: manifest.ManifestDigest}, indexDebits: []admissionReplayIndexDebit{{framedBytes: 8}}}
	lineage := [32]byte{1}
	journal := digestRaw(generation.journalID)
	ref := admissionReplayReference{lineageID: lineage, journalID: journal, kind: durableRuntimeContentObject, present: true, runtime: &inspection}
	newTranscript := func() *admissionReplayTranscript {
		ownedGeneration := cloneAdmissionGeneration(generation)
		return &admissionReplayTranscript{lineages: []admissionReplayLineage{{id: lineage, journals: []admissionReplayJournal{{id: journal}}, generations: []admissionReplayGeneration{ownedGeneration}}}, references: []admissionReplayReference{ref}}
	}
	transcript := newTranscript()
	if err := attachAdmissionInspections(transcript); err != nil {
		t.Fatal(err)
	}
	got := transcript.lineages[0].generations[0]
	if got.runtimeInspection == nil || got.indexHeaderDebited || got.remainingIndexBytes != inspection.reservation.ReservedIndexBytes-8 || got.remainingIndexRecords != inspection.reservation.ReservedIndexRecords-1 || transcript.journalReservedBytes != inspection.reservation.ReservedJournalBytes {
		t.Fatalf("runtime inspection did not bind generation: %+v", got)
	}
	t.Run("brand-new-lineage-header-debit", func(t *testing.T) {
		withHeader, err := calculateEvidenceQuotaReservationFromArithmeticFacts(quotaBundleArithmeticFacts{lineageQuotaProfile: inspection.lineageQuotaProfile, maxAttempts: inspection.maxAttempts, statementCounts: inspection.statementCounts}, true)
		if err != nil {
			t.Fatal(err)
		}
		brandNew := cloneAdmissionGeneration(generation)
		brandNew.reservedRecords, brandNew.reservedBytes, brandNew.reservedSegments = withHeader.ReservedRecords, withHeader.ReservedBytes, withHeader.ReservedSegments
		brandNew.indexDebits = []admissionReplayIndexDebit{{framedBytes: 8}}
		value := &admissionReplayTranscript{
			lineages: []admissionReplayLineage{{
				id: lineage, indexHeaderFramedBytes: 17, journals: []admissionReplayJournal{{id: journal}}, generations: []admissionReplayGeneration{brandNew},
			}},
			references: []admissionReplayReference{ref},
		}
		if err := attachAdmissionInspections(value); err != nil {
			t.Fatal(err)
		}
		got := value.lineages[0].generations[0]
		if got.runtimeInspection == nil || !got.indexHeaderDebited || got.runtimeInspection.reservation != withHeader || got.remainingIndexRecords != withHeader.ReservedIndexRecords-2 || got.remainingIndexBytes != withHeader.ReservedIndexBytes-25 {
			t.Fatalf("lineage header reservation/debit was not restored: %+v", got)
		}
	})
	t.Run("later-generation-cannot-claim-lineage-header", func(t *testing.T) {
		withHeader, err := calculateEvidenceQuotaReservationFromArithmeticFacts(quotaBundleArithmeticFacts{lineageQuotaProfile: inspection.lineageQuotaProfile, maxAttempts: inspection.maxAttempts, statementCounts: inspection.statementCounts}, true)
		if err != nil {
			t.Fatal(err)
		}
		first := cloneAdmissionGeneration(generation)
		second := cloneAdmissionGeneration(generation)
		second.journalID = DigestBytes([]byte("later-journal"))
		second.reservedRecords, second.reservedBytes, second.reservedSegments = withHeader.ReservedRecords, withHeader.ReservedBytes, withHeader.ReservedSegments
		laterJournal := digestRaw(second.journalID)
		value := &admissionReplayTranscript{
			lineages: []admissionReplayLineage{{
				id: lineage, indexHeaderFramedBytes: 17,
				journals:    []admissionReplayJournal{{id: journal}, {id: laterJournal}},
				generations: []admissionReplayGeneration{first, second},
			}},
			references: []admissionReplayReference{ref, {lineageID: lineage, journalID: laterJournal, kind: durableRuntimeContentObject, present: true, runtime: &inspection}},
		}
		if err := attachAdmissionInspections(value); !IsCode(err, CodeEvidenceJournalCorrupt) {
			t.Fatalf("later generation claimed lineage-header reservation: %v", err)
		}
	})
	for name, mutate := range map[string]func(*admissionReplayGeneration){
		"manifest": func(g *admissionReplayGeneration) { g.header.manifestDigest = DigestBytes([]byte("other-manifest")) },
		"schema":   func(g *admissionReplayGeneration) { g.schemaBundleDigest = DigestBytes([]byte("other-schema")) },
		"records":  func(g *admissionReplayGeneration) { g.reservedRecords++ },
		"bytes":    func(g *admissionReplayGeneration) { g.reservedBytes++ },
		"segments": func(g *admissionReplayGeneration) { g.reservedSegments++ },
	} {
		t.Run(name, func(t *testing.T) {
			bad := newTranscript()
			mutate(&bad.lineages[0].generations[0])
			if err := attachAdmissionInspections(bad); !IsCode(err, CodeEvidenceJournalCorrupt) {
				t.Fatalf("stored reservation mismatch accepted: %v", err)
			}
		})
	}

	t.Run("index-debit-records-plus-one", func(t *testing.T) {
		bad := newTranscript()
		bad.lineages[0].generations[0].indexDebits = make([]admissionReplayIndexDebit, inspection.reservation.ReservedIndexRecords+1)
		if err := attachAdmissionInspections(bad); !IsCode(err, CodeEvidenceJournalCorrupt) {
			t.Fatalf("index record debit above reservation accepted: %v", err)
		}
	})
	t.Run("index-debit-bytes-plus-one", func(t *testing.T) {
		bad := newTranscript()
		bad.lineages[0].generations[0].indexDebits = []admissionReplayIndexDebit{{framedBytes: inspection.reservation.ReservedIndexBytes + 1}}
		if err := attachAdmissionInspections(bad); !IsCode(err, CodeEvidenceJournalCorrupt) {
			t.Fatalf("index byte debit above reservation accepted: %v", err)
		}
	})
	t.Run("journal-bytes-plus-one", func(t *testing.T) {
		bad := newTranscript()
		bad.lineages[0].journals[0].segments = []admissionReplaySegment{{file: admissionReplayFile{size: inspection.reservation.ReservedJournalBytes + 1}}}
		if err := attachAdmissionInspections(bad); !IsCode(err, CodeEvidenceJournalCorrupt) {
			t.Fatalf("physical journal above reservation accepted: %v", err)
		}
	})

	nested := generation
	nested.journalID = DigestBytes([]byte("planned-successor"))
	nestedJournal := digestRaw(nested.journalID)
	withNested := newTranscript()
	withNested.lineages[0].generations[0].plannedSuccessor = &nested
	withNested.references = append(withNested.references, admissionReplayReference{lineageID: lineage, journalID: nestedJournal, kind: durableRuntimeContentObject, present: true, runtime: &inspection})
	if err := attachAdmissionInspections(withNested); err != nil {
		t.Fatal(err)
	}
	if withNested.lineages[0].generations[0].plannedSuccessor.runtimeInspection == nil || withNested.journalReservedBytes != inspection.reservation.ReservedJournalBytes {
		t.Fatal("planned successor inspection was lost or double-counted before durable reservation")
	}

	before := admissionReplayCanonicalDigest(transcript)
	transcript.lineages[0].indexHeaderFramedBytes++
	if before == admissionReplayCanonicalDigest(transcript) {
		t.Fatal("lineage header framed-byte mutation did not change transcript digest")
	}
	transcript.lineages[0].indexHeaderFramedBytes--
	before = admissionReplayCanonicalDigest(transcript)
	transcript.lineages[0].generations[0].indexHeaderDebited = true
	if before == admissionReplayCanonicalDigest(transcript) {
		t.Fatal("lineage header debit mutation did not change transcript digest")
	}
	transcript.lineages[0].generations[0].indexHeaderDebited = false
	before = admissionReplayCanonicalDigest(transcript)
	transcript.lineages[0].generations[0].runtimeInspection.maxAttempts++
	if before == admissionReplayCanonicalDigest(transcript) {
		t.Fatal("runtime inspection mutation did not change transcript digest")
	}
	transcript.lineages[0].generations[0].runtimeInspection.maxAttempts--
	before = admissionReplayCanonicalDigest(transcript)
	transcript.journalReservedBytes++
	if before == admissionReplayCanonicalDigest(transcript) {
		t.Fatal("reserved aggregate mutation did not change transcript digest")
	}
}

func TestAdmissionRecoveryObjectInspectionBindsDecision(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile(migrationFixturePath(t, "golden/decision-recovery-inputs-v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Same json.RawMessage `json:"same_bits_input"`
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	var inputs decisionRecoveryVerificationInputs
	if _, err := DecodeStrict(fixture.Same, &inputs); err != nil {
		t.Fatal(err)
	}
	canonical, err := canonicalContractKey(inputs)
	if err != nil {
		t.Fatal(err)
	}
	canonicalBytes := []byte(canonical)
	digest := DigestBytes(canonicalBytes)
	inspection, decision, profile, err := inspectAdmissionRecoveryObject(canonicalBytes, digest, uint64(len(canonicalBytes)))
	if err != nil || inspection == ([32]byte{}) || decision == "" || profile != decisionRecoveryArtifactProfileDigest {
		t.Fatalf("recovery inspection incomplete: decision=%s profile=%s err=%v", decision, profile, err)
	}
	mutated := append([]byte(" "), canonicalBytes...)
	if _, _, _, err := inspectAdmissionRecoveryObject(mutated, DigestBytes(mutated), uint64(len(mutated))); !IsCode(err, CodeEvidenceJournalCorrupt) {
		t.Fatalf("noncanonical registered recovery artifact accepted: %v", err)
	}
}

func TestAdmissionCheckedUsageRejectsOverflowAndExactPlusOne(t *testing.T) {
	t.Parallel()
	if value, err := admissionCheckedAdd(rootJournalMaximumBytes-1, 1); err != nil || value != rootJournalMaximumBytes {
		t.Fatalf("exact maximum rejected: value=%d err=%v", value, err)
	}
	if _, err := admissionCheckedAdd(math.MaxUint64, 1); !IsCode(err, CodeEvidenceJournalCorrupt) {
		t.Fatalf("overflow accepted: %v", err)
	}
	if value, err := admissionCheckedAdd(rootJournalMaximumBytes, 1); err != nil || value != rootJournalMaximumBytes+1 {
		t.Fatalf("checked add lost exact plus one: value=%d err=%v", value, err)
	}
}

func TestAdmissionReferenceBoundsAndCanonicalFindingOrder(t *testing.T) {
	t.Parallel()
	digest := DigestBytes([]byte("runtime"))
	base := admissionObjectReference{headerDigest: projectionTestDigest, decision: projectionTestDigest, manifest: projectionTestDigest, schema: projectionTestDigest, records: 1, bytes: 1, segments: 1}
	tooLarge := []admissionObjectReference{base}
	tooLarge[0].kind, tooLarge[0].digest, tooLarge[0].sizeBytes = durableRuntimeContentObject, digest, maxRuntimeTarSize+1
	if _, _, _, err := replayAdmissionObjects(t.Context(), nil, nil, tooLarge); !IsCode(err, CodeEvidenceJournalCorrupt) {
		t.Fatalf("oversized runtime reference accepted: %v", err)
	}
	recoveryTooLarge := []admissionObjectReference{base}
	recoveryTooLarge[0].kind, recoveryTooLarge[0].digest, recoveryTooLarge[0].sizeBytes = durableDecisionRecoveryContentObject, digest, maxDecisionRecoveryArtifactBytes+1
	if _, _, _, err := replayAdmissionObjects(t.Context(), nil, nil, recoveryTooLarge); !IsCode(err, CodeEvidenceJournalCorrupt) {
		t.Fatalf("oversized recovery reference accepted: %v", err)
	}
	accumulator := &admissionCorruptAccumulator{}
	late := admissionCorrupt("late", "late", nil)
	early := admissionCorrupt("early", "early", nil)
	accumulator.addAt("20", late)
	accumulator.addAt("10", early)
	if accumulator.key != "10" || accumulator.first == nil {
		t.Fatal("corrupt finding order depends on encounter order")
	}
}

func TestAdmissionCompactGenerationsRetainPass2Inputs(t *testing.T) {
	t.Parallel()
	fixture := fixtureObject(t, migrationFixturePath(t, "golden/lineage-index-chain-v1.json"))
	frames := decodeLineageFrames(t, fixture["frames"])
	generations, err := compactAdmissionGenerations(frames)
	if err != nil {
		t.Fatal(err)
	}
	if len(generations) == 0 {
		t.Fatal("generation summaries disappeared")
	}
	var debit uint64
	for _, generation := range generations {
		debit += uint64(len(generation.indexDebits))
		if generation.journalID == "" {
			t.Fatal("generation journal identity missing")
		}
	}
	if debit != uint64(len(frames)-1) {
		t.Fatalf("generation index debit=%d want=%d", debit, len(frames)-1)
	}
	for _, generation := range generations {
		for _, debit := range generation.indexDebits {
			if debit.recordDigest == "" || debit.framedBytes < 8 {
				t.Fatal("generation index debit lost exact framed fact")
			}
		}
	}
	if generations[0].reservedRecords != frames[1].Record.Reserved.ReservedRecords {
		t.Fatal("generation reservation facts drifted")
	}
}

func TestAdmissionGenerationSummaryChangesCanonicalDigest(t *testing.T) {
	t.Parallel()
	summary := evidenceJournalSummary{recoveryState: "brand_new"}
	generation := admissionReplayGeneration{journalID: projectionTestDigest, summary: &summary}
	transcript := &admissionReplayTranscript{lineages: []admissionReplayLineage{{generations: []admissionReplayGeneration{generation}}}}
	before := admissionReplayCanonicalDigest(transcript)
	transcript.lineages[0].generations[0].summary.recoveryState = "dangling_intent"
	after := admissionReplayCanonicalDigest(transcript)
	if before == after {
		t.Fatal("journal summary mutation did not change transcript digest")
	}
	checkpoint := summary
	transcript.lineages[0].generations[0].latestCheckpointSummary = &checkpoint
	before = admissionReplayCanonicalDigest(transcript)
	transcript.lineages[0].generations[0].latestCheckpointSummary.recoveryState = "terminal"
	if before == admissionReplayCanonicalDigest(transcript) {
		t.Fatal("checkpoint summary mutation did not change digest")
	}
	planned := admissionReplayGeneration{journalID: DigestBytes([]byte("planned")), reservedRecords: 1, reservedBytes: 1, reservedSegments: 1}
	transcript.lineages[0].generations[0].plannedSuccessor = &planned
	before = admissionReplayCanonicalDigest(transcript)
	transcript.lineages[0].generations[0].plannedSuccessor.reservedBytes++
	if before == admissionReplayCanonicalDigest(transcript) {
		t.Fatal("planned successor mutation did not change digest")
	}
}

func TestAdmissionLineageHeaderAndTailChangeCanonicalDigest(t *testing.T) {
	t.Parallel()
	lineage := admissionReplayLineage{header: admissionReplayLineageHeader{executionLineageDigest: projectionTestDigest, deploymentID: "deploy", databaseName: "db", repositoryIdentity: "repo", limitsProfile: LineageLimitsProfile}, indexHeaderRecordDigest: DigestBytes([]byte("header")), indexTailRecordDigest: DigestBytes([]byte("tail"))}
	transcript := &admissionReplayTranscript{lineages: []admissionReplayLineage{lineage}}
	before := admissionReplayCanonicalDigest(transcript)
	transcript.lineages[0].indexTailRecordDigest = DigestBytes([]byte("changed"))
	if before == admissionReplayCanonicalDigest(transcript) {
		t.Fatal("lineage index tail mutation did not change digest")
	}
}

func TestAdmissionReservedUnregisteredRetainsPlannedPurposeAndContinuation(t *testing.T) {
	t.Parallel()
	fixture := fixtureObject(t, migrationFixturePath(t, "golden/lineage-index-chain-v1.json"))
	frames := decodeLineageFrames(t, fixture["frames"])
	generations, err := compactAdmissionGenerations(frames[:2])
	if err != nil || len(generations) != 1 {
		t.Fatalf("generations=%v err=%v", generations, err)
	}
	g := generations[0]
	if g.header == nil || g.expectedSegment0HeaderDigest != frames[1].Record.Reserved.ExpectedSegment0HeaderDigest || g.header.outerArtifactDigest != frames[1].Record.Reserved.PlannedSegment0Header.OuterArtifactDigest {
		t.Fatal("planned segment0 purpose facts missing")
	}
	// Fixture is the initial generation and therefore correctly has no continuation.
	if g.continuation != nil {
		t.Fatal("initial generation acquired continuation")
	}
	continuation := LineageContinuationContext{StartAction: "begin_next_attempt", MigrationID: "000001", AttemptIndex: 2, PreviousAttemptTerminalDigest: digestPointer(projectionTestDigest), SourceJournalIdentityDigest: projectionTestDigest, SourceCheckpointRecordDigest: projectionTestDigest, SourceTerminalDigest: projectionTestDigest}
	mutated := cloneProjectionValue(frames[:2])
	mutated[1].Record.Reserved.Continuation = &continuation
	mutated[1].Record.Reserved.QuotaReservationDigest, _ = QuotaReservationDigest(*mutated[1].Record.Reserved)
	mutated[1].RecordDigest, _ = mutated[1].ComputeDigest()
	generations, err = compactAdmissionGenerations(mutated)
	if err != nil || generations[0].continuation == nil || generations[0].continuation.startAction != continuation.StartAction || generations[0].continuation.sourceTerminalDigest != continuation.SourceTerminalDigest {
		t.Fatalf("continuation compact facts missing: %+v err=%v", generations[0].continuation, err)
	}
	transcript := &admissionReplayTranscript{lineages: []admissionReplayLineage{{generations: generations}}}
	before := admissionReplayCanonicalDigest(transcript)
	generations[0].continuation.attemptIndex++
	after := admissionReplayCanonicalDigest(transcript)
	if before == after {
		t.Fatal("continuation mutation did not change transcript digest")
	}
}

func TestAdmissionCompactGenerationRoundTripsExactReservedBytes(t *testing.T) {
	fixture := fixtureObject(t, migrationFixturePath(t, "golden/lineage-index-chain-v1.json"))
	frames := decodeLineageFrames(t, fixture["frames"])
	generations, err := compactAdmissionGenerations(frames)
	if err != nil || len(generations) == 0 {
		t.Fatalf("generations=%d err=%v", len(generations), err)
	}
	lineage := digestRaw(frames[0].Record.Header.ExecutionLineageDigest)
	for index := range generations {
		expanded, err := expandAdmissionGenerationReserved(lineage, generations[index])
		if err != nil || !canonicalEqual(expanded, *frames[indexGenerationReservedFrame(frames, index)].Record.Reserved) {
			t.Fatalf("generation %d did not round-trip: err=%v", index, err)
		}
		if generations[index].plannedSuccessor != nil {
			planned, err := expandAdmissionGenerationReserved(lineage, *generations[index].plannedSuccessor)
			if err != nil || frames[indexGenerationSupersededFrame(frames, index)].Record.Superseded.PlannedGenerationReserved == nil || !canonicalEqual(planned, *frames[indexGenerationSupersededFrame(frames, index)].Record.Superseded.PlannedGenerationReserved) {
				t.Fatalf("planned successor %d did not round-trip: err=%v", index, err)
			}
		}
	}
	bad := cloneAdmissionGeneration(generations[0])
	bad.reservedBytes++
	if _, err := expandAdmissionGenerationReserved(lineage, bad); !IsCode(err, CodeEvidenceJournalCorrupt) {
		t.Fatalf("compact reservation/header mismatch accepted: %v", err)
	}
}

func indexGenerationReservedFrame(frames []LineageIndexFrame, generation int) int {
	seen := 0
	for index := range frames {
		if frames[index].RecordKind == LineageRecordGenerationReserved {
			if seen == generation {
				return index
			}
			seen++
		}
	}
	return -1
}

func indexGenerationSupersededFrame(frames []LineageIndexFrame, generation int) int {
	seen := 0
	for index := range frames {
		if frames[index].RecordKind == LineageRecordGenerationSuperseded {
			if seen == generation {
				return index
			}
			seen++
		}
	}
	return -1
}

func TestAdmissionStandaloneStreamingRejectsJournalWithoutLineagePlan(t *testing.T) {
	t.Parallel()
	fixture := fixtureObject(t, migrationFixturePath(t, "golden/lineage-index-chain-v1.json"))
	frames := decodeEvidenceFrames(t, fixture["journal_frames"])
	var raw []byte
	for _, frame := range frames {
		encoded, err := EncodeCanonicalEvidenceFrame(frame)
		if err != nil {
			t.Fatal(err)
		}
		raw = append(raw, encoded...)
	}
	accumulator := newEvidenceStructuralAccumulator(nil, nil)
	if err := accumulator.beginSegment(); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := streamAdmissionEvidenceFrames(raw, accumulator); err != nil {
		t.Fatal(err)
	}
	if err := accumulator.endSegment(); err != nil {
		t.Fatal(err)
	}
	if replay, err := accumulator.finish(); err != nil || replay.records != uint64(len(frames)) {
		t.Fatalf("standalone strict replay failed: records=%v err=%v", replay, err)
	}
	fault := append([]byte(nil), raw...)
	fault[len(fault)-1] ^= 1
	accumulator = newEvidenceStructuralAccumulator(nil, nil)
	_ = accumulator.beginSegment()
	if _, _, _, err := streamAdmissionEvidenceFrames(fault, accumulator); !IsCode(err, CodeEvidenceJournalCorrupt) {
		t.Fatalf("standalone corrupt journal accepted: %v", err)
	}
}

func TestAdmissionTypedRecoveryTailStateMatrix(t *testing.T) {
	t.Parallel()
	fixture := fixtureObject(t, migrationFixturePath(t, "golden/lineage-index-chain-v1.json"))
	frames := decodeEvidenceFrames(t, fixture["journal_frames"])
	states := map[EvidenceRecordKind]func(*admissionReplayRecoveryTail) bool{
		EvidenceRecordStatementIntent: func(tail *admissionReplayRecoveryTail) bool { return tail.intent != nil }, EvidenceRecordIntermediate: func(tail *admissionReplayRecoveryTail) bool { return tail.intermediate != nil }, EvidenceRecordCommitIntent: func(tail *admissionReplayRecoveryTail) bool { return tail.commit != nil }, EvidenceRecordAttemptTerminal: func(tail *admissionReplayRecoveryTail) bool { return tail.terminal != nil }, EvidenceRecordAmbiguousResolution: func(tail *admissionReplayRecoveryTail) bool { return tail.resolution != nil },
	}
	collector := &admissionReplayJournalCollector{}
	tail := &admissionReplayRecoveryTail{}
	for _, frame := range frames {
		if frame.RecordKind == EvidenceRecordHeader {
			continue
		}
		if err := collector.observe(frame); err != nil {
			t.Fatal(err)
		}
		if err := tail.observe(frame); err != nil {
			t.Fatal(err)
		}
		if check := states[frame.RecordKind]; check != nil && !check(tail) {
			t.Fatalf("typed tail missing %s", frame.RecordKind)
		}
	}
	if tail.terminal == nil {
		t.Fatal("terminal/completed tail missing")
	}
	if len(collector.terminals) == 0 {
		t.Fatal("historical folded terminal event missing")
	}
	if err := collector.validate(); err != nil {
		t.Fatal(err)
	}
	copyTail := *cloneAdmissionRecoveryTail(tail)
	if copyTail.terminal != nil {
		original := copyTail.terminal.body.TerminalDigest
		tail.terminal.body.TerminalDigest = projectionTestDigest
		if copyTail.terminal.body.TerminalDigest != original {
			t.Fatal("typed tail clone aliased body")
		}
	}
	transcript := &admissionReplayTranscript{lineages: []admissionReplayLineage{{generations: []admissionReplayGeneration{{currentTail: &copyTail}}}}}
	before := admissionReplayCanonicalDigest(transcript)
	if copyTail.intent != nil {
		copyTail.intent.recordDigest = projectionTestDigest
	} else if copyTail.terminal != nil {
		copyTail.terminal.recordDigest = projectionTestDigest
	} else if copyTail.resolution != nil {
		copyTail.resolution.recordDigest = projectionTestDigest
	} else {
		t.Fatal("fixture produced no typed recovery record")
	}
	if before == admissionReplayCanonicalDigest(transcript) {
		t.Fatal("typed recovery tail mutation did not change digest")
	}
}

func TestAdmissionTypedRecoveryTailRejectsEveryBodyAndPreviousMutation(t *testing.T) {
	t.Parallel()
	fixture := fixtureObject(t, migrationFixturePath(t, "golden/lineage-index-chain-v1.json"))
	frames := decodeEvidenceFrames(t, fixture["journal_frames"])
	var tail admissionReplayRecoveryTail
	for _, frame := range frames {
		if frame.RecordKind != EvidenceRecordHeader {
			if err := tail.observe(frame); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := validateAdmissionRecoveryTail(&tail); err != nil {
		t.Fatalf("valid tail rejected: %v", err)
	}
	tests := []func(*admissionReplayRecoveryTail){
		func(v *admissionReplayRecoveryTail) {
			if v.intent != nil {
				v.intent.body.StatementIndex++
			}
		},
		func(v *admissionReplayRecoveryTail) {
			if v.intermediate != nil {
				v.intermediate.body.State.StatementIndex++
			}
		},
		func(v *admissionReplayRecoveryTail) {
			if v.commit != nil {
				v.commit.body.ExpectedLedgerLength++
			}
		},
		func(v *admissionReplayRecoveryTail) {
			if v.terminal != nil {
				v.terminal.body.Outcome += "_drift"
			}
		},
		func(v *admissionReplayRecoveryTail) {
			if v.resolution != nil {
				v.resolution.body.Outcome += "_drift"
			}
		},
	}
	for _, mutate := range tests {
		v := cloneAdmissionRecoveryTail(&tail)
		before := cloneProjectionValue(*v)
		mutate(v)
		if canonicalEqual(before, *v) {
			continue
		}
		if err := validateAdmissionRecoveryTail(v); !IsCode(err, CodeEvidenceJournalCorrupt) {
			t.Fatalf("typed body mutation accepted: %v", err)
		}
	}
	v := cloneAdmissionRecoveryTail(&tail)
	var recordPrevious **Digest
	switch {
	case v.terminal != nil:
		recordPrevious = &v.terminal.previousRecordDigest
	case v.commit != nil:
		recordPrevious = &v.commit.previousRecordDigest
	case v.intermediate != nil:
		recordPrevious = &v.intermediate.previousRecordDigest
	case v.intent != nil:
		recordPrevious = &v.intent.previousRecordDigest
	}
	if recordPrevious == nil {
		t.Fatal("tail has no recovery record")
	}
	drift := projectionTestDigest
	*recordPrevious = &drift
	if err := validateAdmissionRecoveryTail(v); !IsCode(err, CodeEvidenceJournalCorrupt) {
		t.Fatalf("previous digest mutation accepted: %v", err)
	}
}

func TestAdmissionCommitSubjectBindsCompleteBody(t *testing.T) {
	t.Parallel()
	fixture := fixtureObject(t, migrationFixturePath(t, "golden/lineage-index-chain-v1.json"))
	frames := decodeEvidenceFrames(t, fixture["journal_frames"])
	var commit CommitIntent
	for _, frame := range frames {
		if frame.Record.CommitIntent != nil {
			commit = cloneProjectionValue(*frame.Record.CommitIntent)
			break
		}
	}
	if commit.MigrationID == "" {
		t.Fatal("fixture commit missing")
	}
	want, err := admissionCommitSubject(commit)
	if err != nil {
		t.Fatal(err)
	}
	mutations := []func(*CommitIntent){func(v *CommitIntent) { v.SchemaBundleDigest = projectionTestDigest }, func(v *CommitIntent) { v.AttemptPredecessorCatalogDigest = projectionTestDigest }, func(v *CommitIntent) { v.LastIntermediateStateDigest = projectionTestDigest }, func(v *CommitIntent) { v.ExpectedLedgerLength++ }, func(v *CommitIntent) { v.LedgerRow.MigrationName += " drift" }}
	for _, mutate := range mutations {
		v := cloneProjectionValue(commit)
		mutate(&v)
		got, err := admissionCommitSubject(v)
		if err != nil {
			t.Fatal(err)
		}
		if got == want {
			t.Fatal("commit subject omitted a body field")
		}
	}
}

func TestAdmissionCheckpointTailAndSupersessionCompactFacts(t *testing.T) {
	t.Parallel()
	fixture := fixtureObject(t, migrationFixturePath(t, "golden/lineage-index-chain-v1.json"))
	frames := decodeLineageFrames(t, fixture["frames"])
	generations, err := compactAdmissionGenerations(frames)
	if err != nil {
		t.Fatal(err)
	}
	var checked bool
	for _, g := range generations {
		if g.latestCheckpointRecordDigest != nil {
			if g.latestCheckpointTailDigest == nil {
				t.Fatal("checkpoint tail digest missing")
			}
			checked = true
		}
		if g.supersessionRecordDigest != nil {
			if g.supersessionAuthorityDigest == "" {
				t.Fatal("supersession authority digest missing")
			}
			if g.supersessionOutcome == "activated_no_migration_progress" && g.oldActivationRecordDigest == nil {
				t.Fatal("activation boundary missing")
			}
			if g.supersessionOutcome != "activated_no_migration_progress" && g.oldCheckpointRecordDigest == nil {
				t.Fatal("checkpoint boundary missing")
			}
		}
	}
	if !checked {
		t.Fatal("fixture checkpoint missing")
	}
	transcript := &admissionReplayTranscript{lineages: []admissionReplayLineage{{generations: generations}}}
	before := admissionReplayCanonicalDigest(transcript)
	for i := range generations {
		if generations[i].latestCheckpointTailDigest != nil {
			*generations[i].latestCheckpointTailDigest = projectionTestDigest
			break
		}
	}
	if before == admissionReplayCanonicalDigest(transcript) {
		t.Fatal("checkpoint tail mutation did not change digest")
	}
}

func TestAdmissionFoldedTerminalCanonicalCoversEveryField(t *testing.T) {
	t.Parallel()
	base := admissionReplayTerminalEvent{migrationID: 1, attemptIndex: 2, statementCount: 3, lastStatementIndex: 4, outcome: 5, resolutionOutcome: 2, flags: admissionTerminalHasFinal | admissionTerminalHasCommit | admissionTerminalHasRetry | admissionTerminalHasResolution, terminalDigest: [32]byte{1}, statementChain: [32]byte{2}}
	digest := func(v admissionReplayTerminalEvent) [32]byte {
		h := sha256.New()
		writeAdmissionTerminalEvent(h, v)
		var d [32]byte
		copy(d[:], h.Sum(nil))
		return d
	}
	want := digest(base)
	mutations := []func(*admissionReplayTerminalEvent){
		func(v *admissionReplayTerminalEvent) { v.migrationID++ }, func(v *admissionReplayTerminalEvent) { v.attemptIndex++ }, func(v *admissionReplayTerminalEvent) { v.statementCount++ }, func(v *admissionReplayTerminalEvent) { v.lastStatementIndex++ }, func(v *admissionReplayTerminalEvent) { v.outcome++ }, func(v *admissionReplayTerminalEvent) { v.resolutionOutcome++ }, func(v *admissionReplayTerminalEvent) { v.flags ^= admissionTerminalHasRetry }, func(v *admissionReplayTerminalEvent) { v.terminalDigest[0]++ }, func(v *admissionReplayTerminalEvent) { v.statementChain[0]++ },
	}
	for _, mutate := range mutations {
		v := base
		mutate(&v)
		if digest(v) == want {
			t.Fatal("folded terminal field missing from canonical digest")
		}
	}
}

func TestAdmissionCanonicalLengthPrefixesLineageHeaderStrings(t *testing.T) {
	t.Parallel()
	left := &admissionReplayTranscript{lineages: []admissionReplayLineage{{header: admissionReplayLineageHeader{deploymentID: "a\x00b", databaseName: "c", repositoryIdentity: "d", limitsProfile: "e"}}}}
	right := &admissionReplayTranscript{lineages: []admissionReplayLineage{{header: admissionReplayLineageHeader{deploymentID: "a", databaseName: "b\x00c", repositoryIdentity: "d", limitsProfile: "e"}}}}
	if admissionReplayCanonicalDigest(left) == admissionReplayCanonicalDigest(right) {
		t.Fatal("lineage header string boundaries collide in transcript digest")
	}
}

func TestAdmissionFoldedSparseCanonicalCoversEveryField(t *testing.T) {
	t.Parallel()
	digestOf := func(write func(hash.Hash)) [32]byte {
		h := sha256.New()
		write(h)
		var result [32]byte
		copy(result[:], h.Sum(nil))
		return result
	}
	final := admissionReplayTerminalFinal{ordinal: 1, lastIntermediateRecord: [32]byte{2}, preledgerCatalog: [32]byte{3}}
	want := digestOf(func(h hash.Hash) { writeAdmissionTerminalFinal(h, final) })
	final.preledgerCatalog[0]++
	if want == digestOf(func(h hash.Hash) { writeAdmissionTerminalFinal(h, final) }) {
		t.Fatal("terminal final mutation did not change canonical digest")
	}
	commit := admissionReplayTerminalCommit{ordinal: 1, expectedLedgerLength: 2, commitRecord: [32]byte{2}, commitBody: [32]byte{3}, previousAttemptTerminal: [32]byte{4}, attemptPredecessorCatalog: [32]byte{5}, lastIntermediateState: [32]byte{6}}
	want = digestOf(func(h hash.Hash) { writeAdmissionTerminalCommit(h, commit) })
	commit.attemptPredecessorCatalog[0]++
	if want == digestOf(func(h hash.Hash) { writeAdmissionTerminalCommit(h, commit) }) {
		t.Fatal("terminal commit mutation did not change canonical digest")
	}
	retry := admissionReplayTerminalRetry{ordinal: 1, proofKind: 4, commitRejectedReason: 1, attemptPredecessorCatalog: [32]byte{2}, observedCatalog: [32]byte{3}, ledgerPrefix: [32]byte{4}, authorityResult: [32]byte{5}}
	want = digestOf(func(h hash.Hash) { writeAdmissionTerminalRetry(h, retry) })
	retry.ledgerPrefix[0]++
	if want == digestOf(func(h hash.Hash) { writeAdmissionTerminalRetry(h, retry) }) {
		t.Fatal("terminal retry mutation did not change canonical digest")
	}
	resolution := admissionReplayTerminalResolution{ordinal: 1, resolutionDigest: [32]byte{2}}
	want = digestOf(func(h hash.Hash) { writeAdmissionTerminalResolution(h, resolution) })
	resolution.resolutionDigest[0]++
	if want == digestOf(func(h hash.Hash) { writeAdmissionTerminalResolution(h, resolution) }) {
		t.Fatal("terminal resolution mutation did not change canonical digest")
	}
}

func TestAdmissionVerificationEventBoundIsHonest(t *testing.T) {
	t.Parallel()
	for name, size := range map[string]uintptr{"terminal": unsafe.Sizeof(admissionReplayTerminalEvent{}), "final": unsafe.Sizeof(admissionReplayTerminalFinal{}), "commit": unsafe.Sizeof(admissionReplayTerminalCommit{}), "retry": unsafe.Sizeof(admissionReplayTerminalRetry{}), "resolution": unsafe.Sizeof(admissionReplayTerminalResolution{})} {
		if size > 176 {
			t.Fatalf("%s verification event grew to %d bytes", name, size)
		}
	}
	records := rootJournalMaximumCount * maxEvidenceReservedRecords
	// Model mutually exclusive legal terminal shapes instead of summing sparse
	// maxima that cannot coexist. A terminal without an intent closes that
	// journal, so there are at most 16; later precommit retry needs intent plus
	// terminal, commit/final shapes need four records, and resolution five.
	base := uint64(unsafe.Sizeof(admissionReplayTerminalEvent{}))
	retry := uint64(unsafe.Sizeof(admissionReplayTerminalRetry{}))
	commit := uint64(unsafe.Sizeof(admissionReplayTerminalCommit{}))
	final := uint64(unsafe.Sizeof(admissionReplayTerminalFinal{}))
	resolution := uint64(unsafe.Sizeof(admissionReplayTerminalResolution{}))
	worstPersistent := rootJournalMaximumCount*base + max((base+retry)*records/2, (base+commit+retry)*records/4, (base+commit+final)*records/4, (base+commit+final+resolution)*records/5)
	if worstPersistent >= 128<<20 {
		t.Fatalf("folded verification persistent upper bound is %d bytes", worstPersistent)
	}
}

func TestAdmissionCollectorCompactsEveryHistoricalTerminalShape(t *testing.T) {
	t.Parallel()
	document := fixtureObject(t, migrationFixturePath(t, "golden/evidence-retry-chains-v1.json"))
	chains := document["chains"].([]JSONValue)
	for _, raw := range chains {
		chain := fixtureObjectValue(t, raw, "retry chain")
		frames := decodeEvidenceFrames(t, chain["frames"])
		collector := &admissionReplayJournalCollector{}
		for _, frame := range frames {
			if frame.RecordKind != EvidenceRecordHeader {
				if err := collector.observe(frame); err != nil {
					t.Fatalf("%v: %v", chain["name"], err)
				}
			}
		}
		if err := collector.validate(); err != nil {
			t.Fatalf("%v: %v", chain["name"], err)
		}
		terminal := terminalFrame(t, frames).Record.AttemptTerminal
		if len(collector.terminals) != 1 || len(collector.retries) != 1 || collector.terminals[0].terminalDigest != digestRaw(terminal.TerminalDigest) || collector.terminals[0].flags&admissionTerminalHasRetry == 0 {
			t.Fatalf("%v: terminal/retry facts missing", chain["name"])
		}
		if terminal.RetryProof.ProofKind == "commit_rejected_exact_predecessor" {
			if len(collector.commits) != 1 || collector.commits[0].commitRecord == ([32]byte{}) || collector.commits[0].commitBody == ([32]byte{}) {
				t.Fatalf("%v: commit-rejected boundary missing", chain["name"])
			}
		} else if len(collector.commits) != 0 {
			t.Fatalf("%v: precommit retry retained impossible commit", chain["name"])
		}
	}

	ambiguous := fixtureObject(t, migrationFixturePath(t, "golden/evidence-ambiguous-chain-v1.json"))
	frames := decodeEvidenceFrames(t, ambiguous["frames"])
	collector := &admissionReplayJournalCollector{}
	for _, frame := range frames {
		if frame.RecordKind != EvidenceRecordHeader {
			if err := collector.observe(frame); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := collector.validate(); err != nil {
		t.Fatal(err)
	}
	if len(collector.terminals) != 1 || len(collector.finals) != 1 || len(collector.commits) != 1 || len(collector.resolutions) != 1 || collector.terminals[0].flags != admissionTerminalHasFinal|admissionTerminalHasCommit|admissionTerminalHasResolution|admissionTerminalHasStatements {
		t.Fatalf("ambiguous sparse facts incomplete: %+v", collector.terminals)
	}
}

func TestAdmissionCollectorUsesValidatedSuccessorContinuation(t *testing.T) {
	t.Parallel()
	fixture := fixtureObject(t, migrationFixturePath(t, "golden/evidence-record-chain-v1.json"))
	intent := cloneProjectionValue(*decodeEvidenceFrames(t, fixture["frames"])[1].Record.StatementIntent)
	previous := DigestBytes([]byte("previous-terminal"))
	continuation := &admissionReplayContinuation{startAction: "begin_next_attempt", migrationID: intent.MigrationID, attemptIndex: 2, previousAttemptTerminalDigest: &previous, sourceTerminalDigest: previous}
	intent.AttemptIndex = 2
	intent.StatementIndex = 0
	intent.PreviousAttemptTerminalDigest = &previous
	frame := EvidenceFrame{FormatVersion: EvidenceFrameFormat, Sequence: 1, RecordKind: EvidenceRecordStatementIntent, Record: EvidenceRecord{StatementIntent: &intent}}
	frame.RecordDigest, _ = frame.ComputeDigest()
	collector := &admissionReplayJournalCollector{initial: cloneAdmissionContinuation(continuation)}
	if err := collector.observe(frame); err != nil {
		t.Fatalf("valid successor intent rejected: %v", err)
	}
	if collector.initial != nil || collector.active == nil || collector.openAttempt() == nil {
		t.Fatal("successor continuation was not consumed into one open attempt")
	}
	fault := cloneProjectionValue(frame)
	fault.Record.StatementIntent.AttemptIndex++
	fault.RecordDigest, _ = fault.ComputeDigest()
	collector = &admissionReplayJournalCollector{initial: cloneAdmissionContinuation(continuation)}
	if err := collector.observe(fault); !IsCode(err, CodeEvidenceJournalCorrupt) {
		t.Fatalf("mismatched successor intent accepted: %v", err)
	}
}

func TestAdmissionCollectorAcceptsTerminalBeforeIntentOnlyAsClosedAttempt(t *testing.T) {
	t.Parallel()
	document := fixtureObject(t, migrationFixturePath(t, "golden/evidence-retry-chains-v1.json"))
	chain := fixtureObjectValue(t, document["chains"].([]JSONValue)[3], "commit rejected")
	frames := decodeEvidenceFrames(t, chain["frames"])
	terminal := cloneProjectionValue(terminalFrame(t, frames))
	terminal.Record.AttemptTerminal.LastIntermediateStateDigest = nil
	terminal.Record.AttemptTerminal.RetryProof = nil
	terminal.Record.AttemptTerminal.Outcome = "aborted_terminal"
	code := string(CodeUntrusted)
	terminal.Record.AttemptTerminal.StableErrorCode = &code
	terminal.Record.AttemptTerminal.FailureEvidence = &StableFailureEvidence{Code: CodeUntrusted, Phase: "preconnect", Path: "trust", Retryable: false}
	terminal.Record.AttemptTerminal.ReconcileResult = "not_run"
	terminal.Record.AttemptTerminal.TerminalDigest, _ = terminal.Record.AttemptTerminal.ComputeDigest()
	terminal.Sequence = 1
	terminal.PreviousRecordDigest = nil
	terminal.RecordDigest, _ = terminal.ComputeDigest()
	collector := &admissionReplayJournalCollector{}
	if err := collector.observe(terminal); err != nil {
		t.Fatalf("valid pre-intent terminal rejected: %v", err)
	}
	if err := collector.validate(); err != nil {
		t.Fatal(err)
	}
	if len(collector.terminals) != 1 || collector.terminals[0].flags&admissionTerminalHasStatements != 0 || collector.terminals[0].statementChain != ([32]byte{}) || collector.active != nil {
		t.Fatal("pre-intent terminal acquired a statement chain or open attempt")
	}
}

func TestAdmissionCollectorBindsOneCatalogContractPerJournal(t *testing.T) {
	t.Parallel()
	fixture := fixtureObject(t, migrationFixturePath(t, "golden/evidence-record-chain-v1.json"))
	frames := decodeEvidenceFrames(t, fixture["frames"])
	collector := &admissionReplayJournalCollector{}
	if err := collector.observe(frames[1]); err != nil {
		t.Fatal(err)
	}
	if collector.catalogContract != digestRaw(frames[1].Record.StatementIntent.CatalogContractDigest) {
		t.Fatal("journal catalog contract was not retained")
	}
	fault := cloneProjectionValue(frames[2])
	fault.Record.Intermediate.State.CatalogContractDigest = projectionTestDigest
	fault.Record.Intermediate.State.IntermediateStateDigest, _ = fault.Record.Intermediate.State.ComputeDigest()
	fault.RecordDigest, _ = fault.ComputeDigest()
	if err := collector.observe(fault); !IsCode(err, CodeEvidenceJournalCorrupt) {
		t.Fatalf("catalog contract drift accepted: %v", err)
	}
	generation := admissionReplayGeneration{verificationCatalogContract: collector.catalogContract}
	transcript := &admissionReplayTranscript{lineages: []admissionReplayLineage{{generations: []admissionReplayGeneration{generation}}}}
	before := admissionReplayCanonicalDigest(transcript)
	transcript.lineages[0].generations[0].verificationCatalogContract[0]++
	if before == admissionReplayCanonicalDigest(transcript) {
		t.Fatal("catalog contract mutation did not change transcript digest")
	}
}

func TestAdmissionCollectorRejectsInterleavedOpenAttempts(t *testing.T) {
	t.Parallel()
	fixture := fixtureObject(t, migrationFixturePath(t, "golden/lineage-index-chain-v1.json"))
	frames := decodeEvidenceFrames(t, fixture["journal_frames"])
	var intent StatementIntent
	for _, frame := range frames {
		if frame.Record.StatementIntent != nil {
			intent = cloneProjectionValue(*frame.Record.StatementIntent)
			break
		}
	}
	if intent.MigrationID == "" {
		t.Fatal("fixture intent missing")
	}
	makeIntent := func(id string, sequence uint64) EvidenceFrame {
		body := cloneProjectionValue(intent)
		body.MigrationID = id
		frame := EvidenceFrame{FormatVersion: EvidenceFrameFormat, Sequence: sequence, RecordKind: EvidenceRecordStatementIntent, Record: EvidenceRecord{StatementIntent: &body}}
		frame.RecordDigest, _ = frame.ComputeDigest()
		return frame
	}
	a, b := makeIntent("000001", 1), makeIntent("000002", 2)
	collector := &admissionReplayJournalCollector{}
	if err := collector.observe(a); err != nil {
		t.Fatal(err)
	}
	if err := collector.observe(b); !IsCode(err, CodeEvidenceJournalCorrupt) {
		t.Fatalf("interleaved attempt accepted: %v", err)
	}
	subject, err := admissionStatementPlanSubject(*a.Record.StatementIntent)
	if err != nil {
		t.Fatal(err)
	}
	want := admissionStatementChainStep([32]byte{}, "000001", 1, 0, subject)
	if collector.frontier.statementChain != want {
		t.Fatal("statement chain cannot be recomputed from recovered plan")
	}
}

func TestAdmissionErrorMappingIsClosed(t *testing.T) {
	t.Parallel()
	if err := contextAdmissionError(nil); !IsCode(err, CodeEvidenceJournalFailed) {
		t.Fatalf("nil context=%v", err)
	}
	tests := []struct {
		err  error
		code ErrorCode
	}{
		{context.Canceled, CodeContextCanceled}, {context.DeadlineExceeded, CodeDeadlineExceeded},
		{evidencefs.ErrLimit, CodeEvidenceJournalCorrupt}, {evidencefs.ErrLeaseInvalid, CodeEvidenceJournalFailed},
		{evidencefs.ErrFilesystem, CodeEvidenceJournalFailed}, {evidencefs.ErrUnknown, CodeEvidenceJournalFailed},
	}
	for _, test := range tests {
		if err := mapEvidenceAdmissionError(test.err, "test"); !IsCode(err, test.code) {
			t.Fatalf("input=%v got=%v want=%s", test.err, err, test.code)
		}
	}
	if !errors.Is(context.Canceled, context.Canceled) {
		t.Fatal("context sentinel drift")
	}
}

func TestAdmissionDrainedSegmentAlwaysPreservesPhysicalFact(t *testing.T) {
	t.Parallel()
	fixture := fixtureObject(t, migrationFixturePath(t, "golden/evidence-record-chain-v1.json"))
	frame := decodeEvidenceFrames(t, fixture["frames"])[0]
	raw, err := EncodeCanonicalEvidenceFrame(frame)
	if err != nil {
		t.Fatal(err)
	}
	file := admissionReplayFile{size: uint64(len(raw)), digest: [32]byte{1}, identity: [32]byte{2}}
	journal := admissionReplayJournal{}
	findings := &admissionCorruptAccumulator{}
	appendAdmissionDrainedSegment(file, raw, "10", &journal, findings)
	if findings.first != nil || len(journal.segments) != 1 || journal.segments[0].file != file || journal.records != 1 || journal.tail != frame.RecordDigest {
		t.Fatalf("valid drained physical fact lost: journal=%+v err=%v", journal, findings.first)
	}
	bad := append(append([]byte(nil), raw...), 0)
	appendAdmissionDrainedSegment(admissionReplayFile{size: uint64(len(bad))}, bad, "20", &journal, findings)
	if findings.first == nil || len(journal.segments) != 2 || journal.segments[1].file.size != uint64(len(bad)) {
		t.Fatalf("corrupt drained physical fact lost: journal=%+v err=%v", journal, findings.first)
	}
	overflow := admissionReplayJournal{records: math.MaxUint64}
	overflowFindings := &admissionCorruptAccumulator{}
	appendAdmissionDrainedSegment(file, raw, "10", &overflow, overflowFindings)
	if overflow.records != math.MaxUint64 || overflowFindings.first == nil || len(overflow.segments) != 1 {
		t.Fatal("drained overflow did not saturate and retain segment")
	}
}

func TestAdmissionStrictLineageStateKeepsOneUnknownExtension(t *testing.T) {
	t.Parallel()
	fixture := fixtureObject(t, migrationFixturePath(t, "golden/lineage-index-chain-v1.json"))
	index := decodeLineageFrames(t, fixture["frames"])
	journal := decodeEvidenceFrames(t, fixture["journal_frames"])
	id := journal[0].Record.Header.JournalIdentityDigest

	reserved := cloneProjectionValue(index[:2])
	if state, err := classifyAdmissionLineageStateCompact(reserved, nil); err != nil || state != admissionLineageReservedUnregistered {
		t.Fatalf("reserved unregistered state=%q err=%v", state, err)
	}
	if state, err := classifyAdmissionLineageStateCompact(reserved, []admissionReplayJournal{{id: digestRaw(id), records: 1}}); err != nil || state != admissionLineageReservedHeader {
		t.Fatalf("reserved header state=%q err=%v", state, err)
	}
	active := cloneProjectionValue(index[:3])
	for length, expected := range map[int]admissionReplayLineageState{1: admissionLineageActiveInitial, 2: admissionLineageActiveUnknownExtension} {
		state, err := classifyAdmissionLineageStateCompact(active, []admissionReplayJournal{{id: digestRaw(id), records: uint64(length)}})
		if err != nil || state != expected {
			t.Fatalf("active length=%d state=%q err=%v", length, state, err)
		}
	}
	if _, err := classifyAdmissionLineageStateCompact(active, []admissionReplayJournal{{id: digestRaw(id), records: 3}}); !IsCode(err, CodeEvidenceJournalCorrupt) {
		t.Fatalf("two unknown extensions accepted: %v", err)
	}

	checkpointed := cloneProjectionValue(index[:4])
	checkpoint := checkpointed[3].Record.Checkpoint
	checkpoint.JournalNextSequence = 3
	checkpoint.JournalTailDigest = journal[2].RecordDigest
	summary, err := summarizeEvidenceJournal(journal[:3])
	if err != nil {
		t.Fatal(err)
	}
	applySummaryToCheckpoint(checkpoint, summary)
	redigestStructuralLineageFrames(t, checkpointed)
	for length, expected := range map[int]admissionReplayLineageState{3: admissionLineageActiveCheckpointed, 4: admissionLineageActiveUnknownExtension} {
		state, err := classifyAdmissionLineageStateCompact(checkpointed, []admissionReplayJournal{{id: digestRaw(id), records: uint64(length)}})
		if err != nil || state != expected {
			t.Fatalf("checkpoint length=%d state=%q err=%v", length, state, err)
		}
	}
	if _, err := classifyAdmissionLineageStateCompact(checkpointed, []admissionReplayJournal{{id: digestRaw(id), records: uint64(len(journal))}}); !IsCode(err, CodeEvidenceJournalCorrupt) {
		t.Fatalf("checkpoint lag greater than one accepted: %v", err)
	}
}

func TestAdmissionRecoverableGenerationPrefixIsBoundToFinalReservation(t *testing.T) {
	t.Parallel()
	fixture := fixtureObject(t, migrationFixturePath(t, "golden/lineage-index-chain-v1.json"))
	index := decodeLineageFrames(t, fixture["frames"])
	reservedFrames := cloneProjectionValue(index[:2])
	generations, err := compactAdmissionGenerations(reservedFrames)
	if err != nil || len(generations) != 1 {
		t.Fatalf("generations=%d err=%v", len(generations), err)
	}
	generation := generations[0]
	frame, headerBytes, err := admissionReplayPlannedHeader(generation)
	if err != nil || frame.RecordDigest != generation.expectedSegment0HeaderDigest || len(headerBytes) < 2 {
		t.Fatalf("frame=%+v bytes=%d err=%v", frame, len(headerBytes), err)
	}
	journal := digestRaw(generation.journalID)
	segmentRaw := headerBytes[:len(headerBytes)-1]
	segment := admissionReplayFile{ordinal: 0, size: uint64(len(segmentRaw)), digest: sha256.Sum256(segmentRaw), identity: [32]byte{9}}
	states := []admissionReplayGenerationPrefix{
		{journalID: journal, state: admissionGenerationPrefixDirectory},
		{journalID: journal, state: admissionGenerationPrefixLock},
		{journalID: journal, state: admissionGenerationPrefixSegment, segment: &segment},
	}
	for _, prefix := range states {
		prefix := prefix
		t.Run(string(prefix.state), func(t *testing.T) {
			if err := validateAdmissionGenerationPrefixes(generations, []admissionReplayGenerationPrefix{prefix}); err != nil {
				t.Fatal(err)
			}
			state, err := classifyAdmissionLineageStateWithPrefixes(reservedFrames, nil, []admissionReplayGenerationPrefix{prefix})
			if err != nil || state != admissionLineageReservedUnregistered {
				t.Fatalf("state=%q err=%v", state, err)
			}
		})
	}
	bad := states[2]
	bad.journalID[0] ^= 1
	if err := validateAdmissionGenerationPrefixes(generations, []admissionReplayGenerationPrefix{bad}); !IsCode(err, CodeEvidenceJournalCorrupt) {
		t.Fatalf("foreign prefix accepted: %v", err)
	}
	if err := validateAdmissionGenerationPrefixes(generations, append(states[:1:1], states[1])); !IsCode(err, CodeEvidenceJournalCorrupt) {
		t.Fatalf("multiple prefixes accepted: %v", err)
	}
	activatedGenerations, err := compactAdmissionGenerations(cloneProjectionValue(index[:3]))
	if err != nil {
		t.Fatal(err)
	}
	if err := validateAdmissionGenerationPrefixes(activatedGenerations, states[:1]); !IsCode(err, CodeEvidenceJournalCorrupt) {
		t.Fatalf("active prefix accepted: %v", err)
	}
	if _, err := classifyAdmissionLineageStateWithPrefixes(cloneProjectionValue(index[:3]), []admissionReplayJournal{{id: journal, records: 1}}, states[:1]); !IsCode(err, CodeEvidenceJournalCorrupt) {
		t.Fatalf("active lineage prefix accepted: %v", err)
	}
}

func TestAdmissionGenerationPrefixCanonicalAndCloneAreExact(t *testing.T) {
	t.Parallel()
	segment := admissionReplayFile{ordinal: 0, size: 7, digest: [32]byte{3}, identity: [32]byte{4}, handoffIdentity: [32]byte{6}}
	transcript := &admissionReplayTranscript{
		revision: 0, fullSetDigest: [32]byte{1}, target: [32]byte{2},
		lineages: []admissionReplayLineage{{
			id: [32]byte{2}, prefixes: []admissionReplayGenerationPrefix{{journalID: [32]byte{5}, state: admissionGenerationPrefixSegment, segment: &segment}},
		}},
	}
	transcript.canonical = admissionReplayCanonicalDigest(transcript)
	owned := cloneAdmissionReplayTranscript(transcript)
	if owned == nil || owned.lineages[0].prefixes[0].segment == transcript.lineages[0].prefixes[0].segment || owned.canonical != admissionReplayCanonicalDigest(owned) {
		t.Fatal("generation prefix was not deeply owned")
	}
	before := transcript.canonical
	transcript.lineages[0].prefixes[0].state = admissionGenerationPrefixLock
	if before == admissionReplayCanonicalDigest(transcript) {
		t.Fatal("generation prefix state is absent from canonical digest")
	}
	transcript = cloneAdmissionReplayTranscript(owned)
	transcript.lineages[0].prefixes[0].segment.identity[0] ^= 1
	if before == admissionReplayCanonicalDigest(transcript) {
		t.Fatal("generation prefix file identity is absent from canonical digest")
	}
	transcript = cloneAdmissionReplayTranscript(owned)
	transcript.lineages[0].prefixes[0].segment.handoffIdentity[0] ^= 1
	if before == admissionReplayCanonicalDigest(transcript) {
		t.Fatal("generation handoff identity is absent from canonical digest")
	}
}

func TestAdmissionTranscriptIsOwnedAndHasNoAuthorityConsumer(t *testing.T) {
	t.Parallel()
	transcript := &admissionReplayTranscript{
		revision: 0, fullSetDigest: [32]byte{1}, target: [32]byte{2},
		lineages: []admissionReplayLineage{{journals: []admissionReplayJournal{{segments: []admissionReplaySegment{{records: 1}}}}}},
	}
	transcript.canonical = admissionReplayCanonicalDigest(transcript)
	owned := cloneAdmissionReplayTranscript(transcript)
	transcript.lineages[0].journals[0].segments[0].records = 2
	if owned.lineages[0].journals[0].segments[0].records != 1 || owned.canonical != admissionReplayCanonicalDigest(owned) {
		t.Fatal("returned transcript aliases input or canonical digest drifted")
	}

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		if name != "evidence_admission_replay.go" && name != "evidence_admission_history.go" {
			ast.Inspect(file, func(node ast.Node) bool {
				if call, ok := node.(*ast.CallExpr); ok {
					if ident, ok := call.Fun.(*ast.Ident); ok && ident.Name == "replayAdmissionInventory" {
						t.Fatalf("production consumer calls replayAdmissionInventory in %s", name)
					}
				}
				if ident, ok := node.(*ast.Ident); ok && (ident.Name == "admissionReplayTranscript" || ident.Name == "admissionReplayReference") {
					t.Fatalf("admission ordinary facts escaped into production consumer %s", name)
				}
				return true
			})
		}
		if name == "evidence_admission_history.go" {
			for _, declaration := range file.Decls {
				function, ok := declaration.(*ast.FuncDecl)
				if !ok {
					continue
				}
				mentions := false
				ast.Inspect(function, func(node ast.Node) bool {
					if ident, ok := node.(*ast.Ident); ok && ident.Name == "admissionReplayTranscript" {
						mentions = true
					}
					return true
				})
				if mentions && function.Name.Name != "admissionHistoryObjectViews" && function.Name.Name != "admissionHistoryTargetFacts" {
					t.Fatalf("admission transcript escaped history binder helper %s", function.Name.Name)
				}
			}
		}
		if name == "evidence_admission_replay.go" {
			for _, declaration := range file.Decls {
				function, ok := declaration.(*ast.FuncDecl)
				if !ok {
					continue
				}
				lower := strings.ToLower(function.Name.Name)
				if strings.Contains(lower, "bind") || strings.Contains(lower, "permit") || strings.Contains(lower, "receipt") || strings.Contains(lower, "verifiedadmissionplan") {
					mentions := false
					ast.Inspect(function, func(node ast.Node) bool {
						if ident, ok := node.(*ast.Ident); ok && (ident.Name == "admissionReplayTranscript" || ident.Name == "admissionReplayReference") {
							mentions = true
						}
						return true
					})
					if mentions {
						t.Fatalf("ordinary admission fact reached authority-like consumer %s", function.Name.Name)
					}
				}
			}
		}
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Type == nil || function.Name.Name == "cloneAdmissionReplayTranscript" || function.Name.Name == "admissionReplayCanonicalDigest" || function.Name.Name == "attachAdmissionInspections" || function.Name.Name == "rootQuotaUsageFactsFromAdmissionTranscript" || name == "evidence_admission_history.go" && (function.Name.Name == "admissionHistoryObjectViews" || function.Name.Name == "admissionHistoryTargetFacts") {
				continue
			}
			mentions := false
			ast.Inspect(function.Type, func(node ast.Node) bool {
				if ident, ok := node.(*ast.Ident); ok && ident.Name == "admissionReplayTranscript" {
					mentions = true
				}
				return true
			})
			if mentions && function.Name.Name != "replayAdmissionInventory" {
				t.Fatalf("admission transcript gained production consumer %s in %s", function.Name.Name, name)
			}
		}
	}
}

func TestAdmissionTranscriptRecomputesRootAndTargetQuotaFacts(t *testing.T) {
	target := [32]byte{1}
	finalA, finalB := DigestBytes([]byte("quota-final-a")), DigestBytes([]byte("quota-final-b"))
	transcript := &admissionReplayTranscript{
		revision: 0, fullSetDigest: [32]byte{2}, target: target,
		objects: []admissionReplayObject{
			{digest: finalB, size: 7, identity: [32]byte{3}},
			{temporary: true, digest: DigestBytes([]byte("quota-temp")), size: 11, identity: [32]byte{4}},
			{digest: finalA, size: 5, identity: [32]byte{5}},
		},
		lineages: []admissionReplayLineage{
			{id: target, index: admissionReplayFile{size: 13}, indexRecords: 2, journals: []admissionReplayJournal{{id: [32]byte{6}}}, generations: []admissionReplayGeneration{{remainingIndexRecords: 3, remainingIndexBytes: 17, runtimeInspection: &admissionReplayRuntimeInspection{reservation: evidenceQuotaReservation{ReservedJournalBytes: 12}}}}},
			{id: [32]byte{7}, index: admissionReplayFile{size: 19}, indexRecords: 4, journals: []admissionReplayJournal{{id: [32]byte{8}}, {id: [32]byte{9}}}, generations: []admissionReplayGeneration{{remainingIndexRecords: 5, remainingIndexBytes: 23, runtimeInspection: &admissionReplayRuntimeInspection{reservation: evidenceQuotaReservation{ReservedJournalBytes: 17}}}}},
		},
		journalReservedBytes: 29, indexBytes: 32, indexReservedBytes: 40,
	}
	transcript.canonical = admissionReplayCanonicalDigest(transcript)
	facts, err := rootQuotaUsageFactsFromAdmissionTranscript(transcript)
	if err != nil {
		t.Fatal(err)
	}
	if len(facts.finalObjects) != 2 || facts.finalObjectBytes != 12 || facts.tempCount != 1 || facts.tempBytes != 11 || facts.largestTempBytes != 11 || facts.journalCount != 3 || facts.journalReservedBytes != 29 || facts.indexCount != 2 || facts.indexActualBytes != 32 || facts.indexReservedBytes != 40 || !facts.targetIndexPresent || facts.targetIndexRecords != 2 || facts.targetIndexBytes != 13 || facts.targetIndexReservedRecords != 3 || facts.targetIndexReservedBytes != 17 || !facts.valid() {
		t.Fatalf("root quota facts are incomplete: %+v", facts)
	}
	if facts.finalObjects[0].digest >= facts.finalObjects[1].digest {
		t.Fatal("final objects are not canonical")
	}

	for name, mutate := range map[string]func(*admissionReplayTranscript){
		"cached actual":   func(v *admissionReplayTranscript) { v.indexBytes++ },
		"cached reserved": func(v *admissionReplayTranscript) { v.indexReservedBytes++ },
		"target swap":     func(v *admissionReplayTranscript) { v.target = [32]byte{99} },
		"object zero":     func(v *admissionReplayTranscript) { v.objects[0].size = 0 },
	} {
		t.Run(name, func(t *testing.T) {
			value := cloneAdmissionReplayTranscript(transcript)
			mutate(value)
			value.canonical = admissionReplayCanonicalDigest(value)
			if _, err := rootQuotaUsageFactsFromAdmissionTranscript(value); !IsCode(err, CodeEvidenceJournalCorrupt) {
				t.Fatalf("quota mismatch accepted: %v", err)
			}
		})
	}

	absent := cloneAdmissionReplayTranscript(transcript)
	absent.target, absent.targetAbsent = [32]byte{99}, true
	absent.canonical = admissionReplayCanonicalDigest(absent)
	absentFacts, err := rootQuotaUsageFactsFromAdmissionTranscript(absent)
	if err != nil || absentFacts.targetIndexPresent || absentFacts.targetIndexRecords != 0 || absentFacts.targetIndexReservedBytes != 0 {
		t.Fatalf("absent target quota facts differ: %+v err=%v", absentFacts, err)
	}
}

func TestAdmissionGenerationPrefixCountsAsPhysicalJournal(t *testing.T) {
	t.Parallel()
	target := [32]byte{1}
	transcript := &admissionReplayTranscript{
		revision: 0, fullSetDigest: [32]byte{2}, target: target,
		lineages: []admissionReplayLineage{{
			id: target, index: admissionReplayFile{size: 9}, indexRecords: 2,
			prefixes: []admissionReplayGenerationPrefix{{journalID: [32]byte{3}, state: admissionGenerationPrefixDirectory}},
			generations: []admissionReplayGeneration{{
				runtimeInspection:     &admissionReplayRuntimeInspection{reservation: evidenceQuotaReservation{ReservedJournalBytes: 17}},
				remainingIndexRecords: 1, remainingIndexBytes: 11,
			}},
		}},
		journalReservedBytes: 17, indexBytes: 9, indexReservedBytes: 11,
	}
	transcript.canonical = admissionReplayCanonicalDigest(transcript)
	facts, err := rootQuotaUsageFactsFromAdmissionTranscript(transcript)
	if err != nil || facts.journalCount != 1 || facts.journalReservedBytes != 17 || facts.targetIndexReservedRecords != 1 || facts.targetIndexReservedBytes != 11 || !facts.valid() {
		t.Fatalf("generation prefix quota facts=%+v err=%v", facts, err)
	}
}

func sortAdmissionObjects(objects []admissionReplayObject) {
	sort.Slice(objects, func(i, j int) bool { return admissionObjectLess(objects[i], objects[j]) })
}
