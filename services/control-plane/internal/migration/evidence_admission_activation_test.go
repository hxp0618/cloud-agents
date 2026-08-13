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

func TestGenerationActivationRejectsLiteralAndClosedDiagnosis(t *testing.T) {
	candidate := quotaCandidateForBundle(t, quotaAdmissionBundleForTest(t), []byte("recovery"))
	if result, err := (&HeaderDurablePermit{}).AppendGenerationActivated(context.Background(), candidate); result.Next() != nil || result.Outcome() != evidencefs.AdmissionTransitionPreMutationFailure || result.CandidateKind() != "generation_activated" || result.CandidateSequence() != 8 || !IsCode(err, CodeEvidenceRecoveryRequired) {
		t.Fatalf("literal header authority entered activation: result=%+v err=%v", result, err)
	}
	if validGenerationReadyPermit(&GenerationReadyPermit{}, &evidencefs.AdmissionInventory{}, candidate) {
		t.Fatal("literal generation-ready permit passed validation")
	}
	result := GenerationActivationTransitionResult{
		outcome: evidencefs.AdmissionTransitionUnknown, candidateDigest: [32]byte{1}, candidateSequence: 8,
		candidateRevision: 10, previousRevision: 9, activationRecordDigest: testDigest("activated"),
		reservedRecordDigest: testDigest("reserved"), headerRecordDigest: testDigest("header"),
	}
	if result.Next() != nil || result.CandidateKind() != "generation_activated" || result.CandidateSequence() != 8 || result.CandidateDigest() != ([32]byte{1}) || result.CandidateRevision() != 10 || result.PreviousRevision() != 9 || result.ActivationRecordDigest() != testDigest("activated") || result.ReservedRecordDigest() != testDigest("reserved") || result.HeaderRecordDigest() != testDigest("header") {
		t.Fatalf("activation diagnosis changed: %+v", result)
	}
}

func TestAdmissionActivatedFrameBindsReservedAndHeader(t *testing.T) {
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
	header := cloneProjectionValue(reserved.PlannedSegment0Header)
	headerFrame := EvidenceFrame{FormatVersion: EvidenceFrameFormat, Sequence: 0, RecordKind: EvidenceRecordHeader, Record: EvidenceRecord{Header: &header}}
	headerFrame.RecordDigest, err = headerFrame.ComputeDigest()
	if err != nil {
		t.Fatal(err)
	}
	activated, raw, err := buildAdmissionActivatedFrame(reservedFrame, headerFrame)
	if err != nil || activated.Sequence != 2 || activated.PreviousRecordDigest == nil || *activated.PreviousRecordDigest != reservedFrame.RecordDigest || activated.Record.Activated == nil || activated.Record.Activated.GenerationReservedRecordDigest != reservedFrame.RecordDigest || activated.Record.Activated.Segment0HeaderDigest != headerFrame.RecordDigest || activated.Record.Activated.InitialJournalTailDigest != headerFrame.RecordDigest || len(raw) == 0 {
		t.Fatalf("activated=%+v bytes=%d err=%v", activated, len(raw), err)
	}
	decoded, err := DecodeCanonicalLineageFrame(raw)
	if err != nil || !canonicalEqual(*decoded, activated) {
		t.Fatalf("decoded=%+v err=%v", decoded, err)
	}
	for name, mutate := range map[string]func(*LineageIndexFrame, *EvidenceFrame){
		"reserved digest": func(v *LineageIndexFrame, _ *EvidenceFrame) { v.RecordDigest = testDigest("wrong") },
		"reserved kind":   func(v *LineageIndexFrame, _ *EvidenceFrame) { v.RecordKind = LineageRecordGenerationActivated },
		"header digest":   func(_ *LineageIndexFrame, v *EvidenceFrame) { v.RecordDigest = testDigest("wrong") },
		"header body": func(_ *LineageIndexFrame, v *EvidenceFrame) {
			v.Record.Header.QuotaReservationDigest = testDigest("wrong")
		},
	} {
		t.Run(name, func(t *testing.T) {
			reservedFault := cloneProjectionValue(reservedFrame)
			headerFault := cloneProjectionValue(headerFrame)
			mutate(&reservedFault, &headerFault)
			if _, _, err := buildAdmissionActivatedFrame(reservedFault, headerFault); err == nil {
				t.Fatal("mismatched reserved/header entered activation")
			}
		})
	}
}

func TestGenerationReadyPermitDigestRejectsCopyAndMutation(t *testing.T) {
	candidate := quotaCandidateForBundle(t, quotaAdmissionBundleForTest(t), []byte("recovery"))
	history := &VerifiedAdmissionHistory{binding: &verifiedAdmissionHistoryBinding{canonical: [32]byte{1}}}
	plan := &VerifiedAdmissionPlan{history: history, binding: &verifiedAdmissionPlanBinding{canonical: [32]byte{2}}}
	prior := &HeaderDurablePermit{plan: plan, history: history, binding: &headerDurablePermitBinding{canonical: [32]byte{3}}}
	runtimeBinding := &verifiedContentReceiptBinding{}
	recoveryBinding := &verifiedDecisionRecoveryReceiptBinding{}
	permit := &GenerationReadyPermit{
		prior: prior, plan: plan, history: history, candidateBinding: candidate.binding,
		target: [32]byte{4}, fullSet: [32]byte{5}, revision: 9, indexDigest: [32]byte{6}, activationBytes: [32]byte{7},
		reservedDigest: testDigest("reserved"), journal: testDigest("journal"), headerDigest: testDigest("header"), activationDigest: testDigest("activated"),
		headerBytes: []byte("header"), activatedBytes: []byte("activated"),
		runtimeReceipt:   VerifiedContentReceipt{digest: candidate.runtimeArtifact.digest, sizeBytes: candidate.runtimeArtifact.sizeBytes, binding: runtimeBinding},
		recoveryReceipt:  VerifiedDecisionRecoveryReceipt{digest: candidate.decisionRecoveryArtifact.digest, sizeBytes: candidate.decisionRecoveryArtifact.sizeBytes, binding: recoveryBinding},
		activationHeader: ownedActivationHeader{header: JournalHeader{FormatVersion: "header"}, reserved: GenerationReserved{ReservedRecords: 1}},
		headerFrame:      EvidenceFrame{FormatVersion: EvidenceFrameFormat, RecordKind: EvidenceRecordHeader, Record: EvidenceRecord{Header: &JournalHeader{FormatVersion: "header"}}, RecordDigest: testDigest("frame")},
		activatedFrame:   LineageIndexFrame{FormatVersion: LineageFrameFormat, RecordKind: LineageRecordGenerationActivated, Record: LineageIndexRecord{Activated: &GenerationActivated{ExecutionLineageDigest: testDigest("lineage")}}, RecordDigest: testDigest("activated-frame")},
		consumed:         &atomic.Bool{},
	}
	permit.self = permit
	want := generationReadyPermitDigest(permit)
	if want == ([32]byte{}) {
		t.Fatal("generation-ready permit digest is empty")
	}
	copyPermit := *permit
	if generationReadyPermitDigest(&copyPermit) != ([32]byte{}) {
		t.Fatal("generation-ready copy retained self binding")
	}
	for name, mutate := range map[string]func(*GenerationReadyPermit){
		"target":          func(v *GenerationReadyPermit) { v.target[0]++ },
		"full set":        func(v *GenerationReadyPermit) { v.fullSet[0]++ },
		"revision":        func(v *GenerationReadyPermit) { v.revision++ },
		"index":           func(v *GenerationReadyPermit) { v.indexDigest[0]++ },
		"activation hash": func(v *GenerationReadyPermit) { v.activationBytes[0]++ },
		"reserved":        func(v *GenerationReadyPermit) { v.reservedDigest = testDigest("other-reserved") },
		"journal":         func(v *GenerationReadyPermit) { v.journal = testDigest("other-journal") },
		"header":          func(v *GenerationReadyPermit) { v.headerDigest = testDigest("other-header") },
		"activation":      func(v *GenerationReadyPermit) { v.activationDigest = testDigest("other-activated") },
		"header bytes":    func(v *GenerationReadyPermit) { v.headerBytes = []byte("changed") },
		"activated bytes": func(v *GenerationReadyPermit) { v.activatedBytes = []byte("changed") },
		"header frame":    func(v *GenerationReadyPermit) { v.headerFrame.Sequence++ },
		"activated frame": func(v *GenerationReadyPermit) { v.activatedFrame.Sequence++ },
		"generation": func(v *GenerationReadyPermit) {
			v.activationHeader.generation.schemaBundleDigest = testDigest("other-generation")
		},
		"runtime size":  func(v *GenerationReadyPermit) { v.runtimeReceipt.sizeBytes++ },
		"recovery size": func(v *GenerationReadyPermit) { v.recoveryReceipt.sizeBytes++ },
	} {
		t.Run(name, func(t *testing.T) {
			value := *permit
			value.self = &value
			value.headerBytes = append([]byte(nil), permit.headerBytes...)
			value.activatedBytes = append([]byte(nil), permit.activatedBytes...)
			mutate(&value)
			if generationReadyPermitDigest(&value) == want {
				t.Fatal("mutation did not change generation-ready digest")
			}
		})
	}
	if permit.consumed.Load() {
		t.Fatal("generation-ready authority was consumed without a handoff transition")
	}
}

func TestActivationPlanIndexDigestBindsAllThreeFrames(t *testing.T) {
	plan := &VerifiedAdmissionPlan{lineageHeaderBytes: []byte("lineage"), reservedFrameBytes: []byte("reserved")}
	want := activationPlanIndexDigest(plan, []byte("activated"))
	if want == ([32]byte{}) {
		t.Fatal("activation index digest is empty")
	}
	if activationPlanIndexDigest(plan, []byte("changed")) == want {
		t.Fatal("activation bytes mutation did not change index digest")
	}
	if activationPlanIndexDigest(nil, []byte("activated")) != ([32]byte{}) || activationPlanIndexDigest(plan, nil) != ([32]byte{}) {
		t.Fatal("missing activation prefix minted an index digest")
	}
}

func TestConsumedHeaderDurablePermitRejectsLiteral(t *testing.T) {
	candidate := quotaCandidateForBundle(t, quotaAdmissionBundleForTest(t), []byte("recovery"))
	consumed := &atomic.Bool{}
	consumed.Store(true)
	if validConsumedHeaderDurablePermit(&HeaderDurablePermit{consumed: consumed}, &VerifiedAdmissionPlan{}, candidate) {
		t.Fatal("literal consumed header authority passed validation")
	}
}

func TestGenerationReadyIsNotActiveGeneration(t *testing.T) {
	permitType := any(&GenerationReadyPermit{})
	if _, ok := permitType.(interface{ ActiveGeneration() ActiveGeneration }); ok {
		t.Fatal("generation-ready permit exposed active generation authority")
	}
}

func TestGenerationReadyHasNoProductionConsumer(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		name := entry.Name()
		isTest := len(name) >= len("_test.go") && name[len(name)-len("_test.go"):] == "_test.go"
		if entry.IsDir() || len(name) < 3 || name[len(name)-3:] != ".go" || name == "evidence_admission_activation.go" || isTest {
			continue
		}
		raw, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(raw, []byte("GenerationReadyPermit")) || bytes.Contains(raw, []byte("AppendGenerationActivated")) || bytes.Contains(raw, []byte("validGenerationReadyPermit")) {
			t.Fatalf("generation-ready authority has an unreviewed production consumer: %s", name)
		}
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "evidence_admission_activation.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		if selector, ok := call.Fun.(*ast.SelectorExpr); ok && (selector.Sel.Name == "Connect" || selector.Sel.Name == "Open") {
			t.Fatalf("activation slice calls forbidden runtime/DB entrypoint %s", selector.Sel.Name)
		}
		return true
	})
}
