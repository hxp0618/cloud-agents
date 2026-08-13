package migration

import (
	"bytes"
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"sync/atomic"
	"testing"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/evidencefs"
)

func TestGenerationHandoffRejectsLiteralAndClosedDiagnosis(t *testing.T) {
	candidate := quotaCandidateForBundle(t, quotaAdmissionBundleForTest(t), []byte("recovery"))
	if result, err := (&GenerationReadyPermit{}).Handoff(context.Background(), candidate); result.Next() != nil || result.Outcome() != evidencefs.AdmissionTransitionPreMutationFailure || result.CandidateKind() != "generation_handoff" || result.CandidateSequence() != 9 || !IsCode(err, CodeEvidenceRecoveryRequired) {
		t.Fatalf("literal ready authority entered handoff: result=%+v err=%v", result, err)
	}
	if validGenerationHandoffReady(&GenerationHandoffReady{}, candidate) {
		t.Fatal("literal handoff authority passed validation")
	}
	result := GenerationHandoffResult{outcome: evidencefs.AdmissionTransitionUnknown, candidateDigest: [32]byte{1}, candidateSequence: 9, revision: 10}
	if result.Next() != nil || result.CandidateKind() != "generation_handoff" || result.CandidateDigest() != ([32]byte{1}) || result.CandidateSequence() != 9 || result.Revision() != 10 {
		t.Fatalf("diagnosis changed: %+v", result)
	}
}

func TestGenerationHandoffDigestsRejectCopyAndMutation(t *testing.T) {
	candidate := quotaCandidateForBundle(t, quotaAdmissionBundleForTest(t), []byte("recovery"))
	history := &VerifiedAdmissionHistory{binding: &verifiedAdmissionHistoryBinding{canonical: [32]byte{1}}}
	plan := &VerifiedAdmissionPlan{history: history, binding: &verifiedAdmissionPlanBinding{canonical: [32]byte{2}}}
	prior := &GenerationReadyPermit{
		plan: plan, history: history, candidateBinding: candidate.binding, binding: &generationReadyPermitBinding{canonical: [32]byte{3}},
		target: [32]byte{4}, journal: testDigest("journal"), revision: 9, reservedDigest: testDigest("reserved"),
		headerDigest: testDigest("header"), activationDigest: testDigest("activated"), consumed: &atomic.Bool{},
	}
	prior.self = prior
	if generationHandoffCandidateDigest(prior) == ([32]byte{}) {
		t.Fatal("candidate digest is empty")
	}
	ready := &GenerationHandoffReady{
		prior: prior, plan: plan, history: history, candidateBinding: candidate.binding, lease: &evidencefs.GenerationLease{}, target: prior.target,
		journal: prior.journal, revision: prior.revision, reservedDigest: prior.reservedDigest,
		headerDigest: prior.headerDigest, activationDigest: prior.activationDigest, consumed: &atomic.Bool{},
	}
	ready.self = ready
	want := generationHandoffReadyDigest(ready)
	if want == ([32]byte{}) {
		t.Fatal("handoff-ready digest is empty")
	}
	copyReady := *ready
	if generationHandoffReadyDigest(&copyReady) != ([32]byte{}) {
		t.Fatal("copy retained digest authority")
	}
	for name, mutate := range map[string]func(*GenerationHandoffReady){
		"target":     func(value *GenerationHandoffReady) { value.target[0]++ },
		"journal":    func(value *GenerationHandoffReady) { value.journal = testDigest("other-journal") },
		"revision":   func(value *GenerationHandoffReady) { value.revision++ },
		"reserved":   func(value *GenerationHandoffReady) { value.reservedDigest = testDigest("other-reserved") },
		"header":     func(value *GenerationHandoffReady) { value.headerDigest = testDigest("other-header") },
		"activation": func(value *GenerationHandoffReady) { value.activationDigest = testDigest("other-activation") },
	} {
		t.Run(name, func(t *testing.T) {
			value := *ready
			value.self = &value
			mutate(&value)
			if generationHandoffReadyDigest(&value) == want {
				t.Fatal("mutation did not change digest")
			}
		})
	}
}

func TestConsumedGenerationReadyRejectsLiteral(t *testing.T) {
	candidate := quotaCandidateForBundle(t, quotaAdmissionBundleForTest(t), []byte("recovery"))
	consumed := &atomic.Bool{}
	consumed.Store(true)
	if validConsumedGenerationReadyPermit(&GenerationReadyPermit{consumed: consumed}, &VerifiedAdmissionPlan{}, candidate) {
		t.Fatal("literal consumed generation-ready authority passed validation")
	}
}

func TestGenerationHandoffRegistryRejectsPairedFieldMutation(t *testing.T) {
	candidate := quotaCandidateForBundle(t, quotaAdmissionBundleForTest(t), []byte("recovery"))
	history := &VerifiedAdmissionHistory{binding: &verifiedAdmissionHistoryBinding{canonical: [32]byte{1}}}
	plan := &VerifiedAdmissionPlan{history: history, binding: &verifiedAdmissionPlanBinding{canonical: [32]byte{2}}}
	prior := &GenerationReadyPermit{
		plan: plan, history: history, candidateBinding: candidate.binding, target: [32]byte{4}, journal: testDigest("journal"),
		revision: 9, reservedDigest: testDigest("reserved"), headerDigest: testDigest("header"),
		activationDigest: testDigest("activated"), consumed: &atomic.Bool{},
	}
	prior.self = prior
	binding := &generationReadyPermitBinding{permit: prior, plan: plan, history: history}
	prior.binding = binding
	binding.canonical = [32]byte{8}
	record := generationReadyPermitRegistryRecord{
		permit: prior, binding: binding, plan: plan, history: history, candidateBinding: candidate.binding, canonical: binding.canonical,
	}
	if !generationReadyRegistryRecordMatches(record, prior) {
		t.Fatal("baseline registry record rejected")
	}
	otherPlan := &VerifiedAdmissionPlan{history: history, binding: &verifiedAdmissionPlanBinding{canonical: [32]byte{7}}}
	prior.plan, prior.binding.plan = otherPlan, otherPlan
	if generationReadyRegistryRecordMatches(record, prior) {
		t.Fatal("paired field mutation crossed immutable registry record")
	}
}

func TestGenerationHandoffReadyIsNotRuntimeAuthorityAndHasNoConsumer(t *testing.T) {
	value := any(&GenerationHandoffReady{})
	if _, ok := value.(interface{ ActiveGeneration() ActiveGeneration }); ok {
		t.Fatal("handoff-ready exposed active generation authority")
	}
	if _, ok := value.(interface{ Connect(context.Context) error }); ok {
		t.Fatal("handoff-ready exposed Connect")
	}
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		name := entry.Name()
		isTest := len(name) >= len("_test.go") && name[len(name)-len("_test.go"):] == "_test.go"
		if entry.IsDir() || len(name) < 3 || name[len(name)-3:] != ".go" || name == "evidence_admission_handoff.go" || isTest {
			continue
		}
		raw, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(raw, []byte("GenerationHandoffReady")) || bytes.Contains(raw, []byte("validGenerationHandoffReady")) {
			t.Fatalf("handoff-ready authority has unreviewed consumer: %s", name)
		}
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "evidence_admission_handoff.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		if selector, ok := call.Fun.(*ast.SelectorExpr); ok && (selector.Sel.Name == "Connect" || selector.Sel.Name == "AppendDurable" || selector.Sel.Name == "Open") {
			t.Fatalf("handoff slice calls forbidden runtime entrypoint %s", selector.Sel.Name)
		}
		return true
	})
}

func TestGenerationHandoffReadyCloseUsesImmutableRegistryLease(t *testing.T) {
	ready := &GenerationHandoffReady{consumed: &atomic.Bool{}}
	ready.self = ready
	generationHandoffReadyRegistry.Store(ready, generationHandoffReadyRegistryRecord{ready: ready})
	if err := ready.Close(); !IsCode(err, CodeEvidenceJournalFailed) {
		t.Fatalf("missing immutable lease close=%v", err)
	}
	if _, ok := generationHandoffReadyRegistry.Load(ready); ok {
		t.Fatal("close retained registry entry")
	}
	if err := ready.Close(); !IsCode(err, CodeEvidenceJournalFailed) {
		t.Fatalf("double close=%v", err)
	}
}
