package migration

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/evidencefs"
)

func TestSuccessorContentConcreteStagesRejectLiterals(t *testing.T) {
	candidate := quotaCandidateForBundle(t, quotaAdmissionBundleForTest(t), []byte("successor-content-literal"))

	runtimeResult, err := (&SuccessorAdmissionPermit{}).PublishRuntime(context.Background(), candidate)
	if runtimeResult.Next() != nil || runtimeResult.Outcome() != evidencefs.AdmissionTransitionPreMutationFailure || runtimeResult.CandidateKind() != "runtime_object" || runtimeResult.CandidateSequence() != 1 || !IsCode(err, CodeEvidenceRecoveryRequired) {
		t.Fatalf("literal prepared stage escaped: result=%+v err=%v", runtimeResult, err)
	}
	runtimeBindResult, err := (&SuccessorRuntimePublishedPermit{}).BindRuntime(context.Background(), candidate)
	if runtimeBindResult.Next() != nil || runtimeBindResult.Outcome() != evidencefs.AdmissionTransitionPreMutationFailure || runtimeBindResult.CandidateKind() != "runtime_binding" || runtimeBindResult.CandidateSequence() != 2 || !IsCode(err, CodeEvidenceRecoveryRequired) {
		t.Fatalf("literal runtime publication escaped: result=%+v err=%v", runtimeBindResult, err)
	}
	recoveryResult, err := (&SuccessorRuntimeBoundPermit{}).PublishDecisionRecovery(context.Background(), candidate)
	if recoveryResult.Next() != nil || recoveryResult.Outcome() != evidencefs.AdmissionTransitionPreMutationFailure || recoveryResult.CandidateKind() != "decision_recovery_object" || recoveryResult.CandidateSequence() != 3 || !IsCode(err, CodeEvidenceRecoveryRequired) {
		t.Fatalf("literal runtime binding escaped: result=%+v err=%v", recoveryResult, err)
	}
	recoveryBindResult, err := (&SuccessorRecoveryPublishedPermit{}).BindDecisionRecovery(context.Background(), candidate)
	if recoveryBindResult.Next() != nil || recoveryBindResult.Outcome() != evidencefs.AdmissionTransitionPreMutationFailure || recoveryBindResult.CandidateKind() != "decision_recovery_binding" || recoveryBindResult.CandidateSequence() != 4 || !IsCode(err, CodeEvidenceRecoveryRequired) {
		t.Fatalf("literal recovery publication escaped: result=%+v err=%v", recoveryBindResult, err)
	}
	readyResult, err := (&SuccessorRecoveryBoundPermit{}).SealReserveReady(context.Background(), candidate)
	if readyResult.Next() != nil || readyResult.Outcome() != evidencefs.AdmissionTransitionPreMutationFailure || readyResult.CandidateKind() != "reserve_ready" || readyResult.CandidateSequence() != 5 || !IsCode(err, CodeEvidenceRecoveryRequired) {
		t.Fatalf("literal recovery binding escaped: result=%+v err=%v", readyResult, err)
	}
	if ready, err := (&SuccessorReserveReady{}).BindReceiptPair(candidate); ready != nil || !IsCode(err, CodeEvidenceRecoveryRequired) {
		t.Fatalf("literal reserve-ready escaped: ready=%+v err=%v", ready, err)
	}
}

func TestSuccessorAdmissionPermitBinderRejectsLiteralInputsWithoutConsumption(t *testing.T) {
	candidate := quotaCandidateForBundle(t, quotaAdmissionBundleForTest(t), []byte("successor-content-binder-literal"))
	plan := &VerifiedSuccessorAdmissionPlan{consumed: &atomic.Bool{}}
	if permit, err := bindSuccessorAdmissionPermit(context.Background(), nil, nil, plan, candidate); permit != nil || !IsCode(err, CodeEvidenceRecoveryRequired) || plan.consumed.Load() {
		t.Fatalf("literal successor inputs entered permit binder: permit=%+v err=%v consumed=%v", permit, err, plan.consumed.Load())
	}
}

func TestSuccessorAdmissionStateDigestBindsClosedFacts(t *testing.T) {
	history := &VerifiedAdmissionHistory{binding: &verifiedAdmissionHistoryBinding{canonical: [32]byte{1}}}
	plan := &VerifiedSuccessorAdmissionPlan{history: history, binding: &verifiedSuccessorAdmissionPlanBinding{canonical: [32]byte{2}}}
	candidateBinding := &verifiedEvidenceRunBinding{canonical: [32]byte{3}}
	state := &successorAdmissionState{
		stage: successorAdmissionPrepared, plan: plan, history: history, candidateBinding: candidateBinding,
		target: [32]byte{4}, fullSet: [32]byte{5}, revision: 7,
		runtimeDigest: testDigest("successor-state-runtime"), runtimeSize: 11,
		recoveryDigest: testDigest("successor-state-recovery"), recoverySize: 13,
		consumed: &atomic.Bool{},
	}
	state.self = state
	baseline := successorAdmissionStateDigest(state)
	if baseline == ([32]byte{}) {
		t.Fatal("successor state fixture did not produce a canonical digest")
	}

	for name, mutate := range map[string]func(*successorAdmissionState){
		"stage":            func(value *successorAdmissionState) { value.stage = successorAdmissionReserveReady },
		"target":           func(value *successorAdmissionState) { value.target[0] ^= 1 },
		"full set":         func(value *successorAdmissionState) { value.fullSet[0] ^= 1 },
		"revision":         func(value *successorAdmissionState) { value.revision++ },
		"runtime digest":   func(value *successorAdmissionState) { value.runtimeDigest = testDigest("changed-runtime") },
		"runtime size":     func(value *successorAdmissionState) { value.runtimeSize++ },
		"recovery digest":  func(value *successorAdmissionState) { value.recoveryDigest = testDigest("changed-recovery") },
		"recovery size":    func(value *successorAdmissionState) { value.recoverySize++ },
		"runtime reused":   func(value *successorAdmissionState) { value.runtimeReused = true },
		"recovery reused":  func(value *successorAdmissionState) { value.recoveryReused = true },
		"runtime presence": func(value *successorAdmissionState) { value.runtimePublication = &evidencefs.Publication{} },
		"recovery presence": func(value *successorAdmissionState) {
			value.recoveryPublication = &evidencefs.Publication{}
		},
		"runtime receipt": func(value *successorAdmissionState) {
			value.runtimeReceipt = VerifiedContentReceipt{digest: value.runtimeDigest, sizeBytes: value.runtimeSize, binding: &verifiedContentReceiptBinding{}}
		},
		"recovery receipt": func(value *successorAdmissionState) {
			value.recoveryReceipt = VerifiedDecisionRecoveryReceipt{digest: value.recoveryDigest, sizeBytes: value.recoverySize, binding: &verifiedDecisionRecoveryReceiptBinding{}}
		},
		"index prefix":  func(value *successorAdmissionState) { value.indexPrefixDigest[0] = 1 },
		"index digest":  func(value *successorAdmissionState) { value.indexDigest[0] = 1 },
		"framed digest": func(value *successorAdmissionState) { value.framedDigest[0] = 1 },
		"index sizes": func(value *successorAdmissionState) {
			value.indexPrefixSize, value.indexSize = 1, 2
		},
		"index records": func(value *successorAdmissionState) { value.indexRecords = 1 },
		"index tail":    func(value *successorAdmissionState) { value.indexTail = testDigest("index-tail") },
		"superseded":    func(value *successorAdmissionState) { value.supersededDigest = testDigest("superseded") },
		"reserved":      func(value *successorAdmissionState) { value.reservedDigest = testDigest("reserved") },
		"journal":       func(value *successorAdmissionState) { value.journal = testDigest("journal") },
		"header digest": func(value *successorAdmissionState) { value.headerDigest = testDigest("header") },
		"journal count": func(value *successorAdmissionState) { value.journalCount = 1 },
		"header bytes":  func(value *successorAdmissionState) { value.headerBytes = []byte("header") },
		"header hash":   func(value *successorAdmissionState) { value.headerBytesHash[0] = 1 },
		"fs journal":    func(value *successorAdmissionState) { value.fsJournalCandidate[0] = 1 },
		"activation hash": func(value *successorAdmissionState) {
			value.activationBytesHash[0] = 1
		},
		"activation digest": func(value *successorAdmissionState) { value.activationDigest = testDigest("activation") },
	} {
		t.Run(name, func(t *testing.T) {
			value := *state
			value.self = &value
			mutate(&value)
			if successorAdmissionStateDigest(&value) == baseline {
				t.Fatal("successor state mutation did not change canonical digest")
			}
		})
	}
}

func TestSuccessorAdmissionStateOwnerRejectsCopyAndWrongConcreteStage(t *testing.T) {
	state := &successorAdmissionState{stage: successorAdmissionPrepared}
	permit := &SuccessorAdmissionPermit{state: state}
	permit.self = permit
	if !successorStageOwnerValid(permit, state) {
		t.Fatal("exact concrete owner was rejected")
	}
	copyPermit := *permit
	if successorStageOwnerValid(&copyPermit, state) {
		t.Fatal("copied concrete owner was accepted")
	}
	copyPermit.self = &copyPermit
	if !successorStageOwnerValid(&copyPermit, state) {
		t.Fatal("self-consistent wrapper shape should be distinguished by the registry binding")
	}
	wrong := &SuccessorRuntimePublishedPermit{state: state}
	wrong.self = wrong
	if successorStageOwnerValid(wrong, state) {
		t.Fatal("wrong concrete stage owner was accepted")
	}
}

func TestSuccessorContentAuthorityDoesNotSpreadBeforeAdjacentIndexSlice(t *testing.T) {
	authorityNames := map[string]bool{
		"SuccessorAdmissionPermit":         true,
		"SuccessorRuntimePublishedPermit":  true,
		"SuccessorRuntimeBoundPermit":      true,
		"SuccessorRecoveryPublishedPermit": true,
		"SuccessorRecoveryBoundPermit":     true,
		"SuccessorReserveReady":            true,
		"SuccessorReceiptBoundReady":       true,
		"SuccessorAdjacentReserveReady":    true,
		"SuccessorReservedDurablePermit":   true,
		"SuccessorHeaderDurablePermit":     true,
		"SuccessorGenerationReadyPermit":   true,
		"SuccessorGenerationHandoffReady":  true,
		"SuccessorGenerationReplayReady":   true,
		"SuccessorGenerationRecoveryReady": true,
		"successorAdmissionState":          true,
		"bindSuccessorAdmissionPermit":     true,
	}
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") || name == "evidence_successor_content.go" || name == "evidence_successor_index.go" || name == "evidence_successor_activation.go" || name == "evidence_successor_handoff.go" || name == "evidence_successor_recovery.go" {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			identifier, ok := node.(*ast.Ident)
			if ok && authorityNames[identifier.Name] {
				t.Fatalf("successor content authority spread into %s through %s", name, identifier.Name)
			}
			return true
		})
	}
}

func TestSuccessorContentConcreteTransitionGraphIsClosed(t *testing.T) {
	expected := map[string]string{
		"SuccessorAdmissionPermit":         "PublishRuntime",
		"SuccessorRuntimePublishedPermit":  "BindRuntime",
		"SuccessorRuntimeBoundPermit":      "PublishDecisionRecovery",
		"SuccessorRecoveryPublishedPermit": "BindDecisionRecovery",
		"SuccessorRecoveryBoundPermit":     "SealReserveReady",
		"SuccessorReserveReady":            "BindReceiptPair",
		"SuccessorReceiptBoundReady":       "AppendGenerationSuperseded",
		"SuccessorAdjacentReserveReady":    "AppendGenerationReserved",
		"SuccessorReservedDurablePermit":   "CreateGenerationHeader",
		"SuccessorHeaderDurablePermit":     "AppendGenerationActivated",
		"SuccessorGenerationReadyPermit":   "Handoff",
		"SuccessorGenerationHandoffReady":  "Replay",
		"SuccessorGenerationReplayReady":   "BindRecovery",
		"SuccessorGenerationRecoveryReady": "",
	}
	seen := make(map[string]bool)
	for _, name := range []string{"evidence_successor_content.go", "evidence_successor_index.go", "evidence_successor_activation.go", "evidence_successor_handoff.go", "evidence_successor_recovery.go"} {
		file, err := parser.ParseFile(token.NewFileSet(), name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Recv == nil || len(function.Recv.List) != 1 {
				continue
			}
			star, ok := function.Recv.List[0].Type.(*ast.StarExpr)
			if !ok {
				continue
			}
			receiver, ok := star.X.(*ast.Ident)
			if !ok {
				continue
			}
			method, tracked := expected[receiver.Name]
			if !tracked {
				continue
			}
			if !ast.IsExported(function.Name.Name) {
				continue
			}
			if function.Name.Name == "Close" && (receiver.Name == "SuccessorGenerationHandoffReady" || receiver.Name == "SuccessorGenerationReplayReady" || receiver.Name == "SuccessorGenerationRecoveryReady") {
				continue
			}
			if method == "" || function.Name.Name != method || seen[receiver.Name] {
				t.Fatalf("concrete successor stage %s exposes unexpected method %s", receiver.Name, function.Name.Name)
			}
			seen[receiver.Name] = true
		}
	}
	for receiver, method := range expected {
		if method != "" && !seen[receiver] {
			t.Fatalf("concrete successor stage %s lost transition %s", receiver, method)
		}
	}
}
