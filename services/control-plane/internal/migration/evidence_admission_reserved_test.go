package migration

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/evidencefs"
)

func TestGenerationReservationRejectsLiteralAndClosedDiagnosis(t *testing.T) {
	candidate := quotaCandidateForBundle(t, quotaAdmissionBundleForTest(t), []byte("recovery"))
	if result, err := (&ReceiptBoundReady{}).AppendGenerationReserved(context.Background(), candidate); result.Next() != nil || result.Outcome() != evidencefs.AdmissionTransitionPreMutationFailure || result.CandidateKind() != "generation_reserved" || result.CandidateSequence() != 6 || !IsCode(err, CodeEvidenceRecoveryRequired) {
		t.Fatalf("literal receipt authority entered reservation: result=%+v err=%v", result, err)
	}
	if validReservedDurablePermit(&ReservedDurablePermit{}, &evidencefs.AdmissionInventory{}, candidate) {
		t.Fatal("literal reserved-durable permit passed validation")
	}
	result := GenerationReservationTransitionResult{
		outcome: evidencefs.AdmissionTransitionUnknown, candidateDigest: [32]byte{1}, candidateSequence: 6,
		candidateRevision: 8, previousRevision: 7, reservedRecordDigest: testDigest("reserved"),
	}
	if result.Next() != nil || result.CandidateKind() != "generation_reserved" || result.CandidateSequence() != 6 || result.CandidateDigest() != ([32]byte{1}) || result.CandidateRevision() != 8 || result.PreviousRevision() != 7 || result.ReservedRecordDigest() != testDigest("reserved") {
		t.Fatalf("generation reservation diagnosis changed: %+v", result)
	}
}

func TestReservedDurablePermitDigestRejectsCopyAndMutation(t *testing.T) {
	candidate := quotaCandidateForBundle(t, quotaAdmissionBundleForTest(t), []byte("recovery"))
	history := &VerifiedAdmissionHistory{binding: &verifiedAdmissionHistoryBinding{canonical: [32]byte{1}}}
	plan := &VerifiedAdmissionPlan{history: history, binding: &verifiedAdmissionPlanBinding{canonical: [32]byte{2}}, reservedFrameBytes: []byte("reserved")}
	prior := &ReceiptBoundReady{plan: plan, history: history, binding: &receiptBoundReadyBinding{canonical: [32]byte{3}}}
	runtimeBinding := &verifiedContentReceiptBinding{}
	recoveryBinding := &verifiedDecisionRecoveryReceiptBinding{}
	permit := &ReservedDurablePermit{
		prior: prior, plan: plan, history: history, candidateBinding: candidate.binding,
		target: [32]byte{4}, fullSet: [32]byte{5}, revision: 7, indexDigest: [32]byte{6}, framedDigest: [32]byte{7},
		reservedDigest: testDigest("reserved"), journal: testDigest("journal"), headerDigest: testDigest("header"),
		runtimeReceipt:  VerifiedContentReceipt{digest: candidate.runtimeArtifact.digest, sizeBytes: candidate.runtimeArtifact.sizeBytes, binding: runtimeBinding},
		recoveryReceipt: VerifiedDecisionRecoveryReceipt{digest: candidate.decisionRecoveryArtifact.digest, sizeBytes: candidate.decisionRecoveryArtifact.sizeBytes, binding: recoveryBinding},
		consumed:        &atomic.Bool{},
	}
	permit.self = permit
	want := reservedDurablePermitDigest(permit)
	if want == ([32]byte{}) {
		t.Fatal("reserved-durable permit digest is empty")
	}
	copyPermit := *permit
	if reservedDurablePermitDigest(&copyPermit) != ([32]byte{}) {
		t.Fatal("reserved-durable copy retained self binding")
	}
	for name, mutate := range map[string]func(*ReservedDurablePermit){
		"target":          func(v *ReservedDurablePermit) { v.target[0]++ },
		"full set":        func(v *ReservedDurablePermit) { v.fullSet[0]++ },
		"revision":        func(v *ReservedDurablePermit) { v.revision++ },
		"index digest":    func(v *ReservedDurablePermit) { v.indexDigest[0]++ },
		"framed digest":   func(v *ReservedDurablePermit) { v.framedDigest[0]++ },
		"reserved digest": func(v *ReservedDurablePermit) { v.reservedDigest = testDigest("other-reserved") },
		"journal":         func(v *ReservedDurablePermit) { v.journal = testDigest("other-journal") },
		"header digest":   func(v *ReservedDurablePermit) { v.headerDigest = testDigest("other-header") },
		"runtime size":    func(v *ReservedDurablePermit) { v.runtimeReceipt.sizeBytes++ },
		"recovery size":   func(v *ReservedDurablePermit) { v.recoveryReceipt.sizeBytes++ },
		"runtime digest":  func(v *ReservedDurablePermit) { v.runtimeReceipt.digest = testDigest("other-runtime") },
		"recovery digest": func(v *ReservedDurablePermit) { v.recoveryReceipt.digest = testDigest("other-recovery") },
	} {
		t.Run(name, func(t *testing.T) {
			value := *permit
			value.self = &value
			mutate(&value)
			if reservedDurablePermitDigest(&value) == want {
				t.Fatal("mutation did not change reserved-durable digest")
			}
		})
	}
}

func TestReservedPlanIndexDigestBindsExactPrefix(t *testing.T) {
	plan := &VerifiedAdmissionPlan{lineageHeaderBytes: []byte("lineage"), reservedFrameBytes: []byte("reserved")}
	want := reservedPlanIndexDigest(plan)
	if want == ([32]byte{}) {
		t.Fatal("reserved index digest is empty")
	}
	copyPlan := *plan
	copyPlan.lineageHeaderBytes = []byte("changed")
	if reservedPlanIndexDigest(&copyPlan) == want {
		t.Fatal("lineage prefix mutation did not change index digest")
	}
	copyPlan = *plan
	copyPlan.reservedFrameBytes = []byte("changed")
	if reservedPlanIndexDigest(&copyPlan) == want {
		t.Fatal("reserved frame mutation did not change index digest")
	}
	if reservedPlanIndexDigest(nil) != ([32]byte{}) || reservedPlanIndexDigest(&VerifiedAdmissionPlan{}) != ([32]byte{}) {
		t.Fatal("missing planned prefix minted an index digest")
	}
}

func TestBrandNewReservationPlanRequiresExactAdjacentFrames(t *testing.T) {
	candidate := quotaCandidateForBundle(t, quotaAdmissionBundleForTest(t), []byte("recovery"))
	bindings, err := candidate.verifiedRun.currentDecision.decision.runnerProjectionBindings()
	if err != nil {
		t.Fatal(err)
	}
	history := &VerifiedAdmissionHistory{
		candidateBinding: candidate.binding, target: digestRaw(bindings.executionLineageDigest), rootFacts: rootFactsForTest(t, nil),
		reservation: evidenceQuotaReservation{ReservedRecords: 2, ReservedJournalBytes: 3, ReservedSegments: 1, ReservedIndexRecords: 4, ReservedIndexBytes: lineageRecordFrameLimits[LineageRecordHeader] + lineageRecordFrameLimits[LineageRecordGenerationReserved], ReservedBytes: 3 + lineageRecordFrameLimits[LineageRecordHeader] + lineageRecordFrameLimits[LineageRecordGenerationReserved]},
	}
	lineage, reserved, lineageBytes, reservedBytes, err := buildBrandNewAdmissionFrames(history, candidate)
	if err != nil {
		t.Fatal(err)
	}
	plan := &VerifiedAdmissionPlan{lineageHeaderFrame: lineage, reservedFrame: reserved, lineageHeaderBytes: lineageBytes, reservedFrameBytes: reservedBytes}
	if !brandNewReservationPlanExact(plan) {
		t.Fatal("exact brand-new frames were rejected")
	}
	for name, mutate := range map[string]func(*VerifiedAdmissionPlan){
		"header sequence":   func(v *VerifiedAdmissionPlan) { v.lineageHeaderFrame.Sequence++ },
		"reserved sequence": func(v *VerifiedAdmissionPlan) { v.reservedFrame.Sequence++ },
		"previous": func(v *VerifiedAdmissionPlan) {
			wrong := testDigest("wrong")
			v.reservedFrame.PreviousRecordDigest = &wrong
		},
		"lineage": func(v *VerifiedAdmissionPlan) {
			v.reservedFrame.Record.Reserved.ExecutionLineageDigest = testDigest("wrong")
		},
	} {
		t.Run(name, func(t *testing.T) {
			value := *plan
			mutate(&value)
			if brandNewReservationPlanExact(&value) {
				t.Fatal("mutated plan remained exact")
			}
		})
	}
}

func TestConsumedReceiptBoundReadyRejectsLiteral(t *testing.T) {
	candidate := quotaCandidateForBundle(t, quotaAdmissionBundleForTest(t), []byte("recovery"))
	if validConsumedReceiptBoundReady(&ReceiptBoundReady{consumed: func() *atomic.Bool { value := &atomic.Bool{}; value.Store(true); return value }()}, &VerifiedAdmissionPlan{}, candidate) {
		t.Fatal("literal consumed receipt authority passed validation")
	}
}
