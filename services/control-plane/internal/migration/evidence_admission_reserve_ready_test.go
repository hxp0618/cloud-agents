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
	if receipt, err := bindRuntimeContentReceipt(owner, runtime, verifiedDurableContentObject{}); receipt != (VerifiedContentReceipt{}) || !IsCode(err, CodeProjectionNotImplemented) {
		t.Fatalf("runtime receipt became implemented: receipt=%+v err=%v", receipt, err)
	}
	if validRuntimeReceipt(VerifiedContentReceipt{}, owner, runtime.digest, runtime.sizeBytes) {
		t.Fatal("reserve-ready work made literal runtime receipt valid")
	}
}
