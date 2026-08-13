package migration

import (
	"context"
	"testing"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/evidencefs"
)

func TestRecoveryPublishedPermitRejectsLiteralCopyAndMutation(t *testing.T) {
	candidate := quotaCandidateForBundle(t, quotaAdmissionBundleForTest(t), []byte("recovery"))
	inventory := &evidencefs.AdmissionInventory{}
	if result, err := (&RuntimeBoundPermit{}).PublishDecisionRecovery(context.Background(), candidate); result.Next() != nil || result.Outcome() != evidencefs.AdmissionTransitionPreMutationFailure || result.CandidateKind() != "decision_recovery_object" || result.CandidateSequence() != 3 || !IsCode(err, CodeEvidenceRecoveryRequired) {
		t.Fatalf("literal permit entered recovery publish: result=%+v err=%v", result, err)
	}
	if validRecoveryPublishedPermit(&RecoveryPublishedPermit{}, inventory, candidate) {
		t.Fatal("literal recovery-published permit passed validation")
	}
	prior := &RuntimeBoundPermit{binding: &runtimeBoundPermitBinding{canonical: [32]byte{1}}}
	plan := &VerifiedAdmissionPlan{binding: &verifiedAdmissionPlanBinding{canonical: [32]byte{2}}}
	permit := &RecoveryPublishedPermit{
		prior: prior, plan: plan, candidateBinding: candidate.binding, target: [32]byte{3}, fullSet: [32]byte{4}, revision: 4,
		runtimeDigest: candidate.runtimeArtifact.digest, runtimeSize: candidate.runtimeArtifact.sizeBytes,
		recoveryDigest: candidate.decisionRecoveryArtifact.digest, recoverySize: candidate.decisionRecoveryArtifact.sizeBytes,
	}
	permit.self = permit
	want := recoveryPublishedPermitDigest(permit)
	if want == ([32]byte{}) {
		t.Fatal("recovery-published permit digest is empty")
	}
	copyPermit := *permit
	if recoveryPublishedPermitDigest(&copyPermit) != ([32]byte{}) {
		t.Fatal("recovery-published permit copy retained self binding")
	}
	for name, mutate := range map[string]func(*RecoveryPublishedPermit){
		"target":          func(v *RecoveryPublishedPermit) { v.target[0]++ },
		"full set":        func(v *RecoveryPublishedPermit) { v.fullSet[0]++ },
		"revision":        func(v *RecoveryPublishedPermit) { v.revision++ },
		"runtime digest":  func(v *RecoveryPublishedPermit) { v.runtimeDigest = testDigest("other-runtime") },
		"runtime size":    func(v *RecoveryPublishedPermit) { v.runtimeSize++ },
		"recovery digest": func(v *RecoveryPublishedPermit) { v.recoveryDigest = testDigest("other-recovery") },
		"recovery size":   func(v *RecoveryPublishedPermit) { v.recoverySize++ },
		"reused":          func(v *RecoveryPublishedPermit) { v.reused = !v.reused },
	} {
		t.Run(name, func(t *testing.T) {
			value := *permit
			value.self = &value
			mutate(&value)
			if recoveryPublishedPermitDigest(&value) == want {
				t.Fatal("mutation did not change recovery-published permit digest")
			}
		})
	}
	result := RecoveryPublicationTransitionResult{outcome: evidencefs.AdmissionTransitionUnknown, candidateDigest: [32]byte{5}, candidateSequence: 3, candidateRevision: 5, previousRevision: 4}
	if result.Next() != nil || result.CandidateSequence() != 3 || result.CandidateDigest() != ([32]byte{5}) || result.CandidateRevision() != 5 || result.PreviousRevision() != 4 {
		t.Fatalf("recovery publication diagnosis changed: %+v", result)
	}
}
