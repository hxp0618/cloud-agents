package migration

import (
	"context"
	"crypto/sha256"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/evidencefs"
)

func TestGenerationPrefixRecoverySliceHasNoActivationHandoffOrDBConsumer(t *testing.T) {
	raw, err := os.ReadFile("evidence_generation_prefix_recovery.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(raw)
	if strings.Count(source, ".RecoverGenerationHeader(") != 1 {
		t.Fatal("generation prefix recovery must have one filesystem recovery call site")
	}
	for _, forbidden := range []string{".AppendTargetIndex(", ".HandoffGeneration(", "Connect(", "Begin("} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("generation prefix recovery crossed later boundary %s", forbidden)
		}
	}
}

func TestGenerationPrefixRecoveryRejectsLiteralAuthority(t *testing.T) {
	candidate := quotaCandidateForBundle(t, quotaAdmissionBundleForTest(t), []byte("recovery"))
	if permit, err := bindGenerationPrefixRecoveryPermit(context.Background(), &VerifiedAdmissionHistory{}, candidate); permit != nil || !IsCode(err, CodeEvidenceRecoveryRequired) {
		t.Fatalf("literal history minted prefix permit: permit=%v err=%v", permit, err)
	}
	result, err := (&GenerationPrefixRecoveryPermit{}).RecoverGenerationHeader(context.Background(), candidate)
	if result.Next() != nil || result.Outcome() != evidencefs.AdmissionTransitionPreMutationFailure || result.CandidateKind() != "generation_header_recovery" || result.CandidateSequence() != 7 || !IsCode(err, CodeEvidenceRecoveryRequired) {
		t.Fatalf("literal prefix permit entered recovery: result=%+v err=%v", result, err)
	}
	if validGenerationPrefixRecoveryPermit(&GenerationPrefixRecoveryPermit{}, candidate) || validRecoveredHeaderDurablePermit(&RecoveredHeaderDurablePermit{}, candidate) || validConsumedGenerationPrefixHistory(&VerifiedAdmissionHistory{}, &verifiedAdmissionRegisteredGeneration{}, candidate) {
		t.Fatal("literal recovery authority passed validation")
	}
	diagnosis := GenerationPrefixRecoveryTransitionResult{
		outcome: evidencefs.AdmissionTransitionUnknown, candidateDigest: [32]byte{1}, candidateSequence: 7,
		candidateRevision: 2, previousRevision: 1, journal: testDigest("prefix-journal"),
		headerRecordDigest: testDigest("prefix-header"), headerBytesDigest: [32]byte{2}, headerSize: 3,
	}
	if diagnosis.Next() != nil || diagnosis.CandidateDigest() != ([32]byte{1}) || diagnosis.CandidateRevision() != 2 || diagnosis.PreviousRevision() != 1 || diagnosis.Journal() != testDigest("prefix-journal") || diagnosis.HeaderRecordDigest() != testDigest("prefix-header") || diagnosis.HeaderBytesDigest() != ([32]byte{2}) || diagnosis.HeaderSize() != 3 {
		t.Fatalf("closed diagnosis changed: %+v", diagnosis)
	}
}

func TestGenerationPrefixRecoveryInputAndPermitDigestAreExact(t *testing.T) {
	candidate := quotaCandidateForBundle(t, quotaAdmissionBundleForTest(t), []byte("recovery"))
	input := generationPrefixRecoveryInputForTest(t, candidate)
	want := input.canonical
	if want == ([32]byte{}) || want != generationPrefixRecoveryInputDigest(input) {
		t.Fatal("valid recovery input has no canonical digest")
	}
	mutations := map[string]func(*generationPrefixRecoveryInput){
		"target":         func(v *generationPrefixRecoveryInput) { v.target[0] ^= 1 },
		"full set":       func(v *generationPrefixRecoveryInput) { v.fullSet[0] ^= 1 },
		"revision":       func(v *generationPrefixRecoveryInput) { v.revision++ },
		"index digest":   func(v *generationPrefixRecoveryInput) { v.indexDigest[0] ^= 1 },
		"index identity": func(v *generationPrefixRecoveryInput) { v.indexIdentity[0] ^= 1 },
		"index size":     func(v *generationPrefixRecoveryInput) { v.indexSize++ },
		"index records":  func(v *generationPrefixRecoveryInput) { v.indexRecords++ },
		"index tail":     func(v *generationPrefixRecoveryInput) { v.indexTail = testDigest("other-index-tail") },
		"reserved frame": func(v *generationPrefixRecoveryInput) { v.reservedFrame.Sequence++ },
		"header frame":   func(v *generationPrefixRecoveryInput) { v.headerFrame.Sequence++ },
		"header bytes":   func(v *generationPrefixRecoveryInput) { v.headerBytes[0] ^= 1 },
		"bytes digest":   func(v *generationPrefixRecoveryInput) { v.headerBytesDigest[0] ^= 1 },
		"journal":        func(v *generationPrefixRecoveryInput) { v.journal = testDigest("other-journal") },
		"physical":       func(v *generationPrefixRecoveryInput) { v.physical = generationPrefixPhysicalSegment },
		"generation": func(v *generationPrefixRecoveryInput) {
			v.activationHeader.generation.journalIdentityDigest = testDigest("other-generation")
		},
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			value := cloneGenerationPrefixRecoveryInput(input)
			mutate(&value)
			if generationPrefixRecoveryInputDigest(value) == want {
				t.Fatal("mutation did not change recovery input digest")
			}
		})
	}

	history := &VerifiedAdmissionHistory{binding: &verifiedAdmissionHistoryBinding{canonical: [32]byte{3}}}
	registered := &verifiedAdmissionRegisteredGeneration{canonical: [32]byte{4}}
	permit := &GenerationPrefixRecoveryPermit{
		history: history, registered: registered, candidateBinding: candidate.binding,
		inventory: &evidencefs.AdmissionInventory{}, mutation: &evidencefs.AdmissionMutationToken{},
		input: input, consumed: &atomic.Bool{}, binding: &generationPrefixRecoveryPermitBinding{},
	}
	permit.self = permit
	permit.binding.permit = permit
	permitDigest := generationPrefixRecoveryPermitDigest(permit)
	if permitDigest == ([32]byte{}) {
		t.Fatal("prefix recovery permit digest is empty")
	}
	copyPermit := *permit
	if generationPrefixRecoveryPermitDigest(&copyPermit) != ([32]byte{}) {
		t.Fatal("copied prefix recovery permit retained self authority")
	}
	permit.input.headerBytes = append([]byte(nil), permit.input.headerBytes...)
	permit.input.headerBytes[0] ^= 1
	if generationPrefixRecoveryPermitDigest(permit) == permitDigest {
		t.Fatal("permit digest ignored owned recovery input")
	}
}

func TestGenerationPrefixPhysicalInputClosedShape(t *testing.T) {
	candidate := quotaCandidateForBundle(t, quotaAdmissionBundleForTest(t), []byte("recovery"))
	base := generationPrefixRecoveryInputForTest(t, candidate)
	for _, state := range []generationPrefixPhysicalState{generationPrefixPhysicalAbsent, generationPrefixPhysicalDirectory, generationPrefixPhysicalLock} {
		value := cloneGenerationPrefixRecoveryInput(base)
		value.physical = state
		if !validGenerationPrefixPhysicalInput(value) {
			t.Fatalf("metadata-only state %q rejected", state)
		}
	}
	segment := cloneGenerationPrefixRecoveryInput(base)
	segment.physical = generationPrefixPhysicalSegment
	segment.prefixSize = 0
	segment.prefixDigest = sha256.Sum256(nil)
	segment.prefixIdentity = [32]byte{8}
	if !validGenerationPrefixPhysicalInput(segment) {
		t.Fatal("zero-byte segment prefix rejected")
	}
	complete := cloneGenerationPrefixRecoveryInput(base)
	complete.physical = generationPrefixPhysicalComplete
	complete.prefixSize = uint64(len(complete.headerBytes))
	complete.prefixDigest = complete.headerBytesDigest
	complete.prefixIdentity = [32]byte{9}
	if !validGenerationPrefixPhysicalInput(complete) {
		t.Fatal("complete segment state rejected")
	}
	for _, mutate := range []func(*generationPrefixRecoveryInput){
		func(v *generationPrefixRecoveryInput) { v.physical = "unknown" },
		func(v *generationPrefixRecoveryInput) { v.prefixSize = 1 },
		func(v *generationPrefixRecoveryInput) { v.prefixDigest = [32]byte{1} },
		func(v *generationPrefixRecoveryInput) { v.prefixIdentity = [32]byte{1} },
	} {
		value := cloneGenerationPrefixRecoveryInput(base)
		mutate(&value)
		if validGenerationPrefixPhysicalInput(value) {
			t.Fatal("invalid physical recovery shape accepted")
		}
	}
}

func generationPrefixRecoveryInputForTest(t *testing.T, candidate OwnedCurrentCandidate) generationPrefixRecoveryInput {
	t.Helper()
	bindings, err := candidate.verifiedRun.currentDecision.decision.runnerProjectionBindings()
	if err != nil {
		t.Fatal(err)
	}
	history := &VerifiedAdmissionHistory{
		candidateBinding: candidate.binding, target: digestRaw(bindings.executionLineageDigest), rootFacts: rootFactsForTest(t, nil),
		reservation: evidenceQuotaReservation{
			ReservedRecords: 2, ReservedJournalBytes: 3, ReservedSegments: 1, ReservedIndexRecords: 4,
			ReservedIndexBytes: lineageRecordFrameLimits[LineageRecordHeader] + lineageRecordFrameLimits[LineageRecordGenerationReserved],
			ReservedBytes:      3 + lineageRecordFrameLimits[LineageRecordHeader] + lineageRecordFrameLimits[LineageRecordGenerationReserved],
		},
	}
	_, reservedFrame, _, _, err := buildBrandNewAdmissionFrames(history, candidate)
	if err != nil {
		t.Fatal(err)
	}
	reserved := cloneProjectionValue(*reservedFrame.Record.Reserved)
	generation := generationIdentity{candidate.owner, reserved.ExecutionLineageDigest, reserved.JournalIdentityDigest, reserved.RunnerProjectionDecisionDigest, reserved.SchemaBundleDigest}
	activation := ownedActivationHeader{header: cloneProjectionValue(reserved.PlannedSegment0Header), generation: generation, reserved: cloneProjectionValue(reserved)}
	headerFrame, headerBytes, err := encodeAdmissionActivationHeader(activation)
	if err != nil {
		t.Fatal(err)
	}
	input := generationPrefixRecoveryInput{
		target: digestRaw(reserved.ExecutionLineageDigest), fullSet: [32]byte{5}, revision: 0,
		indexDigest: [32]byte{6}, indexIdentity: [32]byte{7}, indexSize: 11, indexRecords: 2,
		indexTail: reservedFrame.RecordDigest, reservedFrame: reservedFrame, activationHeader: activation,
		headerFrame: headerFrame, headerBytes: headerBytes, headerBytesDigest: sha256.Sum256(headerBytes),
		journal: reserved.JournalIdentityDigest, physical: generationPrefixPhysicalLock,
	}
	input.canonical = generationPrefixRecoveryInputDigest(input)
	return input
}
