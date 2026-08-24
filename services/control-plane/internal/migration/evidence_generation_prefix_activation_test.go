package migration

import (
	"context"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/evidencefs"
)

func TestGenerationPrefixActivationUsesOneExistingHandoffBridge(t *testing.T) {
	raw, err := os.ReadFile("evidence_generation_prefix_activation.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(raw)
	for call, count := range map[string]int{
		".AppendTargetIndex(":              1,
		"bindVerifiedAdmissionHistory(":    1,
		"bindRegisteredGenerationHandoff(": 1,
	} {
		if actual := strings.Count(source, call); actual != count {
			t.Fatalf("activation bridge call %s count=%d want=%d", call, actual, count)
		}
	}
	for _, forbidden := range []string{".HandoffGeneration(", "Connect(", "Begin("} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("generation prefix activation crossed later boundary %s", forbidden)
		}
	}
	recoveryRaw, err := os.ReadFile("evidence_generation_prefix_recovery.go")
	if err != nil {
		t.Fatal(err)
	}
	recoverySource := string(recoveryRaw)
	if !strings.Contains(recoverySource, "!validRetiredGenerationPrefixHistory(permit.history, permit.registered, candidate)") || strings.Contains(recoverySource, "!validConsumedGenerationPrefixHistory(permit.history, permit.registered, candidate) || permit.input.canonical != permit.prior.input.canonical") {
		t.Fatal("post-header permit still requires the retired source inventory to remain current")
	}
}

func TestGenerationPrefixActivationRejectsLiteralAuthority(t *testing.T) {
	candidate := quotaCandidateForBundle(t, quotaAdmissionBundleForTest(t), []byte("recovery"))
	result, err := (&RecoveredHeaderDurablePermit{}).AppendGenerationActivated(context.Background(), candidate)
	if result.Next() != nil || result.Outcome() != evidencefs.AdmissionTransitionPreMutationFailure || result.CandidateKind() != "generation_activated" || result.CandidateSequence() != 8 || !IsCode(err, CodeEvidenceRecoveryRequired) {
		t.Fatalf("literal recovered header entered activation: result=%+v err=%v", result, err)
	}
	if _, ok := any(&RecoveredHeaderDurablePermit{}).(interface{ ActiveGeneration() ActiveGeneration }); ok {
		t.Fatal("recovered header exposed runtime authority")
	}
	diagnosis := GenerationPrefixActivationTransitionResult{
		outcome: evidencefs.AdmissionTransitionUnknown, candidateDigest: [32]byte{1}, candidateSequence: 8,
		candidateRevision: 3, previousRevision: 2, activationRecordDigest: testDigest("activation"),
		reservedRecordDigest: testDigest("reserved"), headerRecordDigest: testDigest("header"),
	}
	if diagnosis.Next() != nil || diagnosis.CandidateDigest() != ([32]byte{1}) || diagnosis.CandidateRevision() != 3 || diagnosis.PreviousRevision() != 2 || diagnosis.ActivationRecordDigest() != testDigest("activation") || diagnosis.ReservedRecordDigest() != testDigest("reserved") || diagnosis.HeaderRecordDigest() != testDigest("header") {
		t.Fatalf("closed activation diagnosis changed: %+v", diagnosis)
	}
}

func TestGenerationPrefixActivationFrameIsExactAdjacentRecord(t *testing.T) {
	candidate := quotaCandidateForBundle(t, quotaAdmissionBundleForTest(t), []byte("recovery"))
	input := generationPrefixRecoveryInputForTest(t, candidate)
	frame, raw, err := buildSuccessorActivatedFrame(input.reservedFrame, input.headerFrame)
	if err != nil {
		t.Fatal(err)
	}
	if frame.Sequence != input.indexRecords || frame.PreviousRecordDigest == nil || *frame.PreviousRecordDigest != input.indexTail || frame.Record.Activated == nil || frame.Record.Activated.GenerationReservedRecordDigest != input.reservedFrame.RecordDigest || frame.Record.Activated.Segment0HeaderDigest != input.headerFrame.RecordDigest || frame.Record.Activated.InitialJournalTailDigest != input.headerFrame.RecordDigest || string(raw) != string(mustEncodeLineageFrame(frame)) {
		t.Fatalf("activation frame is not exact adjacent record: %+v", frame)
	}
}

func TestRecoveredGenerationRegistrationFactsAreExact(t *testing.T) {
	candidate := quotaCandidateForBundle(t, quotaAdmissionBundleForTest(t), []byte("recovery"))
	old := registeredGenerationDigestFixture(t, candidate)
	old.replay = nil
	fresh := cloneRegisteredGenerationDigestFixture(old)
	fresh.replay = &verifiedAdmissionGenerationReplay{canonical: [32]byte{9}}
	tail := old.descriptor.replayTailDigest
	if !recoveredGenerationRegistrationFactsExact(fresh, old, tail) {
		t.Fatal("exact recovered registration facts were rejected")
	}
	mutations := map[string]func(*verifiedAdmissionRegisteredGeneration){
		"identity": func(v *verifiedAdmissionRegisteredGeneration) {
			v.descriptor.identity.journalIdentityDigest = testDigest("other-journal")
		},
		"header": func(v *verifiedAdmissionRegisteredGeneration) {
			v.descriptor.header.QuotaReservationDigest = testDigest("other-quota")
		},
		"tail": func(v *verifiedAdmissionRegisteredGeneration) {
			v.descriptor.replayTailDigest = testDigest("other-tail")
		},
		"decision": func(v *verifiedAdmissionRegisteredGeneration) { v.decision.digest = testDigest("other-decision") },
		"bindings": func(v *verifiedAdmissionRegisteredGeneration) { v.bindings.expectedCanonical += "x" },
		"bundle":   func(v *verifiedAdmissionRegisteredGeneration) { v.bundle.ownedInputs.canonical[0] ^= 1 },
		"artifact": func(v *verifiedAdmissionRegisteredGeneration) {
			v.recoveryArtifact.digest = testDigest("other-artifact")
		},
		"runtime receipt": func(v *verifiedAdmissionRegisteredGeneration) {
			v.runtimeReceipt.digest = testDigest("other-runtime")
		},
		"recovery receipt": func(v *verifiedAdmissionRegisteredGeneration) { v.recoveryReceipt.sizeBytes++ },
		"policy": func(v *verifiedAdmissionRegisteredGeneration) {
			v.policy = &VerifiedHistoricalRecoveryPolicy{}
		},
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			value := cloneRegisteredGenerationDigestFixture(fresh)
			mutate(value)
			if recoveredGenerationRegistrationFactsExact(value, old, tail) {
				t.Fatal("registration mutation remained exact")
			}
		})
	}
	if recoveredGenerationRegistrationFactsExact(fresh, old, testDigest("wrong-tail")) {
		t.Fatal("wrong recovered header tail was accepted")
	}
}

func TestGenerationPrefixActivationCleanupRevokesOnlyOwnedGraphs(t *testing.T) {
	historyBinding := &verifiedAdmissionHistoryBinding{canonical: [32]byte{1}}
	runtimeBinding := &verifiedContentReceiptBinding{digest: testDigest("runtime"), sizeBytes: 1}
	recoveryBinding := &verifiedDecisionRecoveryReceiptBinding{digest: testDigest("recovery"), sizeBytes: 1}
	registered := &verifiedAdmissionRegisteredGeneration{
		runtimeReceipt:  VerifiedContentReceipt{binding: runtimeBinding},
		recoveryReceipt: VerifiedDecisionRecoveryReceipt{binding: recoveryBinding},
	}
	history := &VerifiedAdmissionHistory{binding: historyBinding, targetGeneration: registered}
	prior := &GenerationPrefixRecoveryPermit{consumed: &atomic.Bool{}}
	permit := &RecoveredHeaderDurablePermit{prior: prior, history: history, registered: registered}
	verifiedAdmissionHistoryRegistry.Store(historyBinding, historyBinding.canonical)
	verifiedContentReceiptRegistry.Store(runtimeBinding, runtimeBinding)
	verifiedDecisionRecoveryReceiptRegistry.Store(recoveryBinding, recoveryBinding)
	generationPrefixRecoveryPermitRegistry.Store(prior, generationPrefixRecoveryPermitRegistryRecord{permit: prior})
	recoveredHeaderDurablePermitRegistry.Store(permit, recoveredHeaderDurablePermitRegistryRecord{permit: permit})
	retireRecoveredHeaderDurablePermit(permit)
	if _, ok := verifiedAdmissionHistoryRegistry.Load(historyBinding); ok {
		t.Fatal("old history registry survived activation retirement")
	}
	if _, ok := verifiedContentReceiptRegistry.Load(runtimeBinding); ok {
		t.Fatal("old runtime receipt survived activation retirement")
	}
	if _, ok := verifiedDecisionRecoveryReceiptRegistry.Load(recoveryBinding); ok {
		t.Fatal("old recovery receipt survived activation retirement")
	}
	if _, ok := generationPrefixRecoveryPermitRegistry.Load(prior); ok {
		t.Fatal("old prefix permit survived activation retirement")
	}
	if _, ok := recoveredHeaderDurablePermitRegistry.Load(permit); ok {
		t.Fatal("old header permit survived activation retirement")
	}
}
