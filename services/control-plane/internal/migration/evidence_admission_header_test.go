package migration

import (
	"context"
	"crypto/sha256"
	"sync/atomic"
	"testing"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/evidencefs"
)

func TestGenerationHeaderRejectsLiteralAndClosedDiagnosis(t *testing.T) {
	candidate := quotaCandidateForBundle(t, quotaAdmissionBundleForTest(t), []byte("recovery"))
	if result, err := (&ReservedDurablePermit{}).CreateGenerationHeader(context.Background(), candidate); result.Next() != nil || result.Outcome() != evidencefs.AdmissionTransitionPreMutationFailure || result.CandidateKind() != "generation_header" || result.CandidateSequence() != 7 || !IsCode(err, CodeEvidenceRecoveryRequired) {
		t.Fatalf("literal reserved authority entered header transition: result=%+v err=%v", result, err)
	}
	if validHeaderDurablePermit(&HeaderDurablePermit{}, &evidencefs.AdmissionInventory{}, candidate) {
		t.Fatal("literal header-durable permit passed validation")
	}
	result := GenerationHeaderTransitionResult{
		outcome: evidencefs.AdmissionTransitionUnknown, candidateDigest: [32]byte{1}, candidateSequence: 7,
		candidateRevision: 9, previousRevision: 8, journal: testDigest("journal"), headerRecordDigest: testDigest("header"),
		headerBytesDigest: [32]byte{2}, headerSize: 3,
	}
	if result.Next() != nil || result.CandidateKind() != "generation_header" || result.CandidateSequence() != 7 || result.CandidateDigest() != ([32]byte{1}) || result.CandidateRevision() != 9 || result.PreviousRevision() != 8 || result.Journal() != testDigest("journal") || result.HeaderRecordDigest() != testDigest("header") || result.HeaderBytesDigest() != ([32]byte{2}) || result.HeaderSize() != 3 {
		t.Fatalf("generation header diagnosis changed: %+v", result)
	}
}

func TestAdmissionActivationHeaderEncodingIsExact(t *testing.T) {
	candidate := quotaCandidateForBundle(t, quotaAdmissionBundleForTest(t), []byte("recovery"))
	bindings, err := candidate.verifiedRun.currentDecision.decision.runnerProjectionBindings()
	if err != nil {
		t.Fatal(err)
	}
	history := &VerifiedAdmissionHistory{
		candidateBinding: candidate.binding, target: digestRaw(bindings.executionLineageDigest), rootFacts: rootFactsForTest(t, nil),
		reservation: evidenceQuotaReservation{ReservedRecords: 2, ReservedJournalBytes: 3, ReservedSegments: 1, ReservedIndexRecords: 4, ReservedIndexBytes: lineageRecordFrameLimits[LineageRecordHeader] + lineageRecordFrameLimits[LineageRecordGenerationReserved], ReservedBytes: 3 + lineageRecordFrameLimits[LineageRecordHeader] + lineageRecordFrameLimits[LineageRecordGenerationReserved]},
	}
	_, reservedFrame, _, _, err := buildBrandNewAdmissionFrames(history, candidate)
	if err != nil {
		t.Fatal(err)
	}
	reserved := *reservedFrame.Record.Reserved
	generation := generationIdentity{candidate.owner, reserved.ExecutionLineageDigest, reserved.JournalIdentityDigest, reserved.RunnerProjectionDecisionDigest, reserved.SchemaBundleDigest}
	owned := ownedActivationHeader{header: cloneProjectionValue(reserved.PlannedSegment0Header), generation: generation, reserved: cloneProjectionValue(reserved)}
	frame, raw, err := encodeAdmissionActivationHeader(owned)
	if err != nil || frame.Sequence != 0 || frame.PreviousRecordDigest != nil || frame.RecordKind != EvidenceRecordHeader || frame.Record.Header == nil || frame.RecordDigest != reserved.ExpectedSegment0HeaderDigest || len(raw) == 0 {
		t.Fatalf("frame=%+v bytes=%d err=%v", frame, len(raw), err)
	}
	decoded, err := DecodeCanonicalEvidenceFrame(raw)
	if err != nil || !canonicalEqual(*decoded, frame) {
		t.Fatalf("decoded=%+v err=%v", decoded, err)
	}
	fault := owned
	fault.header.QuotaReservationDigest = testDigest("wrong")
	if _, _, err := encodeAdmissionActivationHeader(fault); err == nil {
		t.Fatal("mismatched header entered exact encoding")
	}
	fault = owned
	fault.reserved.ExpectedSegment0HeaderDigest = testDigest("wrong")
	if _, _, err := encodeAdmissionActivationHeader(fault); err == nil {
		t.Fatal("mismatched expected header digest entered exact encoding")
	}
}

func TestHeaderDurablePermitDigestRejectsCopyAndMutation(t *testing.T) {
	candidate := quotaCandidateForBundle(t, quotaAdmissionBundleForTest(t), []byte("recovery"))
	bindings, err := candidate.verifiedRun.currentDecision.decision.runnerProjectionBindings()
	if err != nil {
		t.Fatal(err)
	}
	frameHistory := &VerifiedAdmissionHistory{
		candidateBinding: candidate.binding, target: digestRaw(bindings.executionLineageDigest), rootFacts: rootFactsForTest(t, nil),
		reservation: evidenceQuotaReservation{ReservedRecords: 2, ReservedJournalBytes: 3, ReservedSegments: 1, ReservedIndexRecords: 4, ReservedIndexBytes: lineageRecordFrameLimits[LineageRecordHeader] + lineageRecordFrameLimits[LineageRecordGenerationReserved], ReservedBytes: 3 + lineageRecordFrameLimits[LineageRecordHeader] + lineageRecordFrameLimits[LineageRecordGenerationReserved]},
	}
	_, reservedFrame, _, _, err := buildBrandNewAdmissionFrames(frameHistory, candidate)
	if err != nil {
		t.Fatal(err)
	}
	reserved := *reservedFrame.Record.Reserved
	generation := generationIdentity{candidate.owner, reserved.ExecutionLineageDigest, reserved.JournalIdentityDigest, reserved.RunnerProjectionDecisionDigest, reserved.SchemaBundleDigest}
	activation := ownedActivationHeader{header: cloneProjectionValue(reserved.PlannedSegment0Header), generation: generation, reserved: cloneProjectionValue(reserved)}
	headerFrame, headerBytes, err := encodeAdmissionActivationHeader(activation)
	if err != nil {
		t.Fatal(err)
	}
	history := &VerifiedAdmissionHistory{binding: &verifiedAdmissionHistoryBinding{canonical: [32]byte{1}}}
	plan := &VerifiedAdmissionPlan{history: history, binding: &verifiedAdmissionPlanBinding{canonical: [32]byte{2}}}
	prior := &ReservedDurablePermit{plan: plan, history: history, binding: &reservedDurablePermitBinding{canonical: [32]byte{3}}}
	runtimeBinding := &verifiedContentReceiptBinding{}
	recoveryBinding := &verifiedDecisionRecoveryReceiptBinding{}
	permit := &HeaderDurablePermit{
		prior: prior, plan: plan, history: history, candidateBinding: candidate.binding,
		target: [32]byte{4}, fullSet: [32]byte{5}, revision: 8, indexDigest: [32]byte{6}, fsCandidate: [32]byte{7}, headerBytesHash: sha256.Sum256(headerBytes),
		reservedDigest: reservedFrame.RecordDigest, journal: reserved.JournalIdentityDigest, headerDigest: headerFrame.RecordDigest, headerBytes: headerBytes,
		runtimeReceipt:   VerifiedContentReceipt{digest: candidate.runtimeArtifact.digest, sizeBytes: candidate.runtimeArtifact.sizeBytes, binding: runtimeBinding},
		recoveryReceipt:  VerifiedDecisionRecoveryReceipt{digest: candidate.decisionRecoveryArtifact.digest, sizeBytes: candidate.decisionRecoveryArtifact.sizeBytes, binding: recoveryBinding},
		activationHeader: activation, headerFrame: headerFrame, consumed: &atomic.Bool{},
	}
	permit.self = permit
	want := headerDurablePermitDigest(permit)
	if want == ([32]byte{}) {
		t.Fatal("header-durable permit digest is empty")
	}
	copyPermit := *permit
	if headerDurablePermitDigest(&copyPermit) != ([32]byte{}) {
		t.Fatal("header-durable copy retained self binding")
	}
	for name, mutate := range map[string]func(*HeaderDurablePermit){
		"target":        func(v *HeaderDurablePermit) { v.target[0]++ },
		"full set":      func(v *HeaderDurablePermit) { v.fullSet[0]++ },
		"revision":      func(v *HeaderDurablePermit) { v.revision++ },
		"index":         func(v *HeaderDurablePermit) { v.indexDigest[0]++ },
		"fs candidate":  func(v *HeaderDurablePermit) { v.fsCandidate[0]++ },
		"bytes hash":    func(v *HeaderDurablePermit) { v.headerBytesHash[0]++ },
		"reserved":      func(v *HeaderDurablePermit) { v.reservedDigest = testDigest("other-reserved") },
		"journal":       func(v *HeaderDurablePermit) { v.journal = testDigest("other-journal") },
		"header digest": func(v *HeaderDurablePermit) { v.headerDigest = testDigest("other-header") },
		"header bytes":  func(v *HeaderDurablePermit) { v.headerBytes = []byte("changed") },
		"header frame":  func(v *HeaderDurablePermit) { v.headerFrame.Sequence++ },
		"header body":   func(v *HeaderDurablePermit) { v.activationHeader.header.FormatVersion = "changed" },
		"reserved body": func(v *HeaderDurablePermit) { v.activationHeader.reserved.ReservedRecords++ },
		"generation": func(v *HeaderDurablePermit) {
			v.activationHeader.generation.journalIdentityDigest = testDigest("other-generation")
		},
		"runtime size":  func(v *HeaderDurablePermit) { v.runtimeReceipt.sizeBytes++ },
		"recovery size": func(v *HeaderDurablePermit) { v.recoveryReceipt.sizeBytes++ },
	} {
		t.Run(name, func(t *testing.T) {
			value := *permit
			value.self = &value
			value.headerBytes = append([]byte(nil), permit.headerBytes...)
			mutate(&value)
			if headerDurablePermitDigest(&value) == want {
				t.Fatal("mutation did not change header-durable digest")
			}
		})
	}
}

func TestConsumedReservedDurablePermitRejectsLiteral(t *testing.T) {
	candidate := quotaCandidateForBundle(t, quotaAdmissionBundleForTest(t), []byte("recovery"))
	consumed := &atomic.Bool{}
	consumed.Store(true)
	if validConsumedReservedDurablePermit(&ReservedDurablePermit{consumed: consumed}, &VerifiedAdmissionPlan{}, candidate) {
		t.Fatal("literal consumed reserved authority passed validation")
	}
}
