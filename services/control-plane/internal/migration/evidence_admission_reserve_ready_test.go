package migration

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/evidencefs"
)

func TestReserveReadyRejectsLiteralCopyAndMutation(t *testing.T) {
	candidate := quotaCandidateForBundle(t, quotaAdmissionBundleForTest(t), []byte("recovery"))
	inventory := &evidencefs.AdmissionInventory{}
	if result, err := (&RecoveryBoundPermit{}).SealReserveReady(context.Background(), candidate); result.Next() != nil || result.Outcome() != evidencefs.AdmissionTransitionPreMutationFailure || result.CandidateKind() != "reserve_ready" || result.CandidateSequence() != 5 || !IsCode(err, CodeEvidenceRecoveryRequired) {
		t.Fatalf("literal permit entered reserve-ready seal: result=%+v err=%v", result, err)
	}
	if validReserveReady(&ReserveReady{}, inventory, candidate) {
		t.Fatal("literal reserve-ready passed validation")
	}
	history := &VerifiedAdmissionHistory{binding: &verifiedAdmissionHistoryBinding{canonical: [32]byte{1}}}
	plan := &VerifiedAdmissionPlan{history: history, binding: &verifiedAdmissionPlanBinding{canonical: [32]byte{2}}}
	prior := &RecoveryBoundPermit{plan: plan, binding: &recoveryBoundPermitBinding{canonical: [32]byte{3}}}
	ready := &ReserveReady{
		prior: prior, plan: plan, history: history, candidateBinding: candidate.binding,
		target: [32]byte{4}, fullSet: [32]byte{5}, revision: 6,
		runtimeDigest: candidate.runtimeArtifact.digest, runtimeSize: candidate.runtimeArtifact.sizeBytes,
		recoveryDigest: candidate.decisionRecoveryArtifact.digest, recoverySize: candidate.decisionRecoveryArtifact.sizeBytes,
		lineageHeaderBytes: []byte("lineage"), reservedFrameBytes: []byte("reserved"), consumed: &atomic.Bool{},
	}
	ready.self = ready
	want := reserveReadyDigest(ready)
	if want == ([32]byte{}) {
		t.Fatal("reserve-ready digest is empty")
	}
	copyReady := *ready
	if reserveReadyDigest(&copyReady) != ([32]byte{}) {
		t.Fatal("reserve-ready copy retained self binding")
	}
	for name, mutate := range map[string]func(*ReserveReady){
		"target":          func(v *ReserveReady) { v.target[0]++ },
		"full set":        func(v *ReserveReady) { v.fullSet[0]++ },
		"revision":        func(v *ReserveReady) { v.revision++ },
		"runtime digest":  func(v *ReserveReady) { v.runtimeDigest = testDigest("other-runtime") },
		"runtime size":    func(v *ReserveReady) { v.runtimeSize++ },
		"recovery digest": func(v *ReserveReady) { v.recoveryDigest = testDigest("other-recovery") },
		"recovery size":   func(v *ReserveReady) { v.recoverySize++ },
		"lineage bytes":   func(v *ReserveReady) { v.lineageHeaderBytes = []byte("changed") },
		"reserved bytes":  func(v *ReserveReady) { v.reservedFrameBytes = []byte("changed") },
	} {
		t.Run(name, func(t *testing.T) {
			value := *ready
			value.self = &value
			value.lineageHeaderBytes = append([]byte(nil), ready.lineageHeaderBytes...)
			value.reservedFrameBytes = append([]byte(nil), ready.reservedFrameBytes...)
			mutate(&value)
			if reserveReadyDigest(&value) == want {
				t.Fatal("mutation did not change reserve-ready digest")
			}
		})
	}
	result := ReserveReadyTransitionResult{outcome: evidencefs.AdmissionTransitionUnknown, candidateDigest: [32]byte{6}, candidateSequence: 5, candidateRevision: 7, previousRevision: 6}
	if result.Next() != nil || result.CandidateSequence() != 5 || result.CandidateDigest() != ([32]byte{6}) || result.CandidateRevision() != 7 || result.PreviousRevision() != 6 {
		t.Fatalf("reserve-ready diagnosis changed: %+v", result)
	}
}

func TestReserveReadyDoesNotMintContentReceipts(t *testing.T) {
	owner := &evidenceOwnerToken{}
	runtime := VerifiedRuntimeArtifact{owner: owner, bytes: []byte("runtime"), digest: DigestBytes([]byte("runtime")), sizeBytes: 7}
	if receipt, err := bindRuntimeContentReceipt(owner, runtime, verifiedDurableContentObject{}); receipt != (VerifiedContentReceipt{}) || !IsCode(err, CodeEvidenceRecoveryRequired) {
		t.Fatalf("runtime receipt accepted missing publication: receipt=%+v err=%v", receipt, err)
	}
	if validRuntimeReceipt(VerifiedContentReceipt{}, owner, runtime.digest, runtime.sizeBytes) {
		t.Fatal("reserve-ready work made literal runtime receipt valid")
	}
}

func TestReceiptBoundReadyRejectsLiteralCopyAndMutation(t *testing.T) {
	candidate := quotaCandidateForBundle(t, quotaAdmissionBundleForTest(t), []byte("recovery"))
	inventory := &evidencefs.AdmissionInventory{}
	if ready, err := (&ReserveReady{}).BindReceiptPair(candidate); ready != nil || !IsCode(err, CodeEvidenceRecoveryRequired) {
		t.Fatalf("literal reserve-ready minted receipts: ready=%+v err=%v", ready, err)
	}
	if validReceiptBoundReady(&ReceiptBoundReady{}, inventory, candidate) {
		t.Fatal("literal receipt-bound ready passed validation")
	}
	history := &VerifiedAdmissionHistory{binding: &verifiedAdmissionHistoryBinding{canonical: [32]byte{1}}}
	plan := &VerifiedAdmissionPlan{history: history, binding: &verifiedAdmissionPlanBinding{canonical: [32]byte{2}}}
	prior := &ReserveReady{plan: plan, history: history, binding: &reserveReadyBinding{canonical: [32]byte{3}}}
	runtimeBinding := &verifiedContentReceiptBinding{}
	recoveryBinding := &verifiedDecisionRecoveryReceiptBinding{}
	ready := &ReceiptBoundReady{
		prior: prior, plan: plan, history: history, candidateBinding: candidate.binding,
		target: [32]byte{4}, fullSet: [32]byte{5}, revision: 6,
		runtimeReceipt:  VerifiedContentReceipt{digest: candidate.runtimeArtifact.digest, sizeBytes: candidate.runtimeArtifact.sizeBytes, binding: runtimeBinding},
		recoveryReceipt: VerifiedDecisionRecoveryReceipt{digest: candidate.decisionRecoveryArtifact.digest, sizeBytes: candidate.decisionRecoveryArtifact.sizeBytes, binding: recoveryBinding},
		consumed:        &atomic.Bool{},
	}
	ready.self = ready
	want := receiptBoundReadyDigest(ready)
	if want == ([32]byte{}) {
		t.Fatal("receipt-bound digest is empty")
	}
	copyReady := *ready
	if receiptBoundReadyDigest(&copyReady) != ([32]byte{}) {
		t.Fatal("receipt-bound copy retained self binding")
	}
	for name, mutate := range map[string]func(*ReceiptBoundReady){
		"target":          func(v *ReceiptBoundReady) { v.target[0]++ },
		"full set":        func(v *ReceiptBoundReady) { v.fullSet[0]++ },
		"revision":        func(v *ReceiptBoundReady) { v.revision++ },
		"runtime digest":  func(v *ReceiptBoundReady) { v.runtimeReceipt.digest = testDigest("other-runtime") },
		"runtime size":    func(v *ReceiptBoundReady) { v.runtimeReceipt.sizeBytes++ },
		"recovery digest": func(v *ReceiptBoundReady) { v.recoveryReceipt.digest = testDigest("other-recovery") },
		"recovery size":   func(v *ReceiptBoundReady) { v.recoveryReceipt.sizeBytes++ },
	} {
		t.Run(name, func(t *testing.T) {
			value := *ready
			value.self = &value
			mutate(&value)
			if receiptBoundReadyDigest(&value) == want {
				t.Fatal("mutation did not change receipt-bound digest")
			}
		})
	}
}
