package migration

import (
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
		if entry.IsDir() || len(name) < 3 || name[len(name)-3:] != ".go" || name == "evidence_admission_handoff.go" || name == "evidence_generation_recovery.go" || isTest {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			identifier, ok := node.(*ast.Ident)
			if ok && (identifier.Name == "GenerationHandoffReady" || identifier.Name == "validGenerationHandoffReady") && !reviewedEvidenceSinkAuthorityUse(name, identifier.Name) {
				t.Fatalf("handoff-ready authority has unreviewed consumer: %s", name)
			}
			return true
		})
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

func TestGenerationReplayStrictBytesBindsBrandNewHandoffChain(t *testing.T) {
	_, ready, indexRaw, segments := generationReplayFixture(t)
	indexFrames, err := decodeAdmissionLineageFrames(indexRaw)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := scanLineageChainStructure(indexFrames)
	if err != nil {
		t.Fatal(err)
	}
	if err := ready.validateReplayIndex(indexFrames); err != nil {
		t.Fatal(err)
	}
	stream, registered := openEvidenceJournalStructuralStream(plan, ready.journal, nil)
	if !registered {
		t.Fatal("journal is not registered")
	}
	if err := stream.beginSegment(); err != nil {
		t.Fatal(err)
	}
	_, _, _, err = streamGenerationReplaySegment(segments[0], stream)
	if err == nil {
		err = stream.endSegment()
	}
	var replay *evidenceStructuralReplay
	if err == nil {
		replay, err = stream.finish()
	}
	if err != nil || replay == nil || replay.records != 1 || replay.tailDigest != ready.headerDigest {
		t.Fatalf("replay=%+v err=%v", replay, err)
	}
	standalone, _ := openEvidenceJournalStructuralStream(nil, "", nil)
	if err := standalone.beginSegment(); err != nil {
		t.Fatal(err)
	}
	first, _, _, err := streamGenerationReplaySegment(segments[0], standalone)
	if err != nil || first != 1 {
		t.Fatalf("segment records=%d err=%v", first, err)
	}
	actual := map[Digest]EvidenceFrame{ready.journal: replay.firstFrame}
	if err := plan.acceptJournal(ready.journal, replay); err != nil {
		t.Fatal(err)
	}
	if _, err := plan.finish(actual, map[Digest]bool{ready.journal: true}); err != nil {
		t.Fatal(err)
	}
	generationHandoffReadyRegistry.Delete(ready)
}

func TestGenerationReplayStrictBytesRejectsEveryBoundMutation(t *testing.T) {
	_, ready, indexRaw, segments := generationReplayFixture(t)
	indexFrames, err := decodeAdmissionLineageFrames(indexRaw)
	if err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func([]LineageIndexFrame){
		"reserved":   func(frames []LineageIndexFrame) { frames[1].RecordDigest = testDigest("other-reserved") },
		"activation": func(frames []LineageIndexFrame) { frames[2].RecordDigest = testDigest("other-activation") },
		"journal": func(frames []LineageIndexFrame) {
			frames[1].Record.Reserved.JournalIdentityDigest = testDigest("other-journal")
		},
		"header": func(frames []LineageIndexFrame) {
			frames[1].Record.Reserved.ExpectedSegment0HeaderDigest = testDigest("other-header")
		},
	} {
		t.Run(name, func(t *testing.T) {
			fault := cloneProjectionValue(indexFrames)
			mutate(fault)
			if err := ready.validateReplayIndex(fault); !IsCode(err, CodeEvidenceJournalCorrupt) {
				t.Fatalf("fault accepted: %v", err)
			}
		})
	}
	fault := append([]byte(nil), segments[0]...)
	fault[len(fault)-1] ^= 1
	standalone, _ := openEvidenceJournalStructuralStream(nil, "", nil)
	if err := standalone.beginSegment(); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := streamGenerationReplaySegment(fault, standalone); !IsCode(err, CodeEvidenceJournalCorrupt) {
		t.Fatalf("corrupt segment drain=%v", err)
	}
	generationHandoffReadyRegistry.Delete(ready)
}

func TestGenerationReplayReadyDigestRejectsCopyAndMutation(t *testing.T) {
	candidate, prior, _, _ := generationReplayFixture(t)
	replay := &GenerationReplayReady{
		prior: prior, plan: prior.plan, history: prior.history, candidateBinding: candidate.binding,
		lease: prior.lease, snapshot: &evidencefs.GenerationSnapshot{}, target: prior.target, journal: prior.journal,
		revision: prior.revision, reservedDigest: prior.reservedDigest, headerDigest: prior.headerDigest,
		activationDigest: prior.activationDigest, snapshotIdentity: [32]byte{1}, indexDigest: [32]byte{2},
		indexRecords: 3, segmentCount: 1, journalRecords: 1, journalTail: prior.headerDigest, consumed: &atomic.Bool{},
	}
	replay.self = replay
	want := generationReplayReadyDigest(replay)
	if want == ([32]byte{}) {
		t.Fatal("replay-ready digest is empty")
	}
	copyReplay := *replay
	if generationReplayReadyDigest(&copyReplay) != ([32]byte{}) {
		t.Fatal("copy retained replay-ready digest")
	}
	for name, mutate := range map[string]func(*GenerationReplayReady){
		"target":      func(v *GenerationReplayReady) { v.target[0]++ },
		"journal":     func(v *GenerationReplayReady) { v.journal = testDigest("other-journal") },
		"snapshot":    func(v *GenerationReplayReady) { v.snapshotIdentity[0]++ },
		"index":       func(v *GenerationReplayReady) { v.indexDigest[0]++ },
		"index count": func(v *GenerationReplayReady) { v.indexRecords++ },
		"segments":    func(v *GenerationReplayReady) { v.segmentCount++ },
		"records":     func(v *GenerationReplayReady) { v.journalRecords++ },
		"tail":        func(v *GenerationReplayReady) { v.journalTail = testDigest("other-tail") },
		"activation":  func(v *GenerationReplayReady) { v.activationDigest = testDigest("other-activation") },
	} {
		t.Run(name, func(t *testing.T) {
			value := *replay
			value.self = &value
			mutate(&value)
			if generationReplayReadyDigest(&value) == want {
				t.Fatal("mutation did not change replay-ready digest")
			}
		})
	}
	generationHandoffReadyRegistry.Delete(prior)
}

func TestGenerationReplayReadyIsNotRuntimeAuthority(t *testing.T) {
	value := any(&GenerationReplayReady{})
	if _, ok := value.(EvidenceJournal); ok {
		t.Fatal("replay-ready implemented EvidenceJournal")
	}
	if _, ok := value.(interface{ Cursor() JournalCursor }); ok {
		t.Fatal("replay-ready exposed JournalCursor")
	}
	if _, ok := value.(interface{ Connect(context.Context) error }); ok {
		t.Fatal("replay-ready exposed Connect")
	}
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		name := entry.Name()
		isTest := len(name) >= len("_test.go") && name[len(name)-len("_test.go"):] == "_test.go"
		if entry.IsDir() || len(name) < 3 || name[len(name)-3:] != ".go" || name == "evidence_admission_handoff.go" || name == "evidence_generation_recovery.go" || name == "evidence_generation_journal.go" || isTest {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			identifier, ok := node.(*ast.Ident)
			if ok && (identifier.Name == "GenerationReplayReady" || identifier.Name == "validGenerationReplayReady") && !reviewedEvidenceSinkAuthorityUse(name, identifier.Name) {
				t.Fatalf("replay-ready authority has unreviewed consumer: %s", name)
			}
			return true
		})
	}
}

func TestGenerationReplayRejectsProgressBeyondBrandNewHeader(t *testing.T) {
	header := testDigest("header")
	if err := validateBrandNewReplayBoundary(1, 1, header, header); err != nil {
		t.Fatal(err)
	}
	for name, fault := range map[string]struct {
		count   uint32
		records uint64
		tail    Digest
	}{
		"extra segment": {2, 1, header},
		"extra record":  {1, 2, testDigest("progress")},
		"wrong tail":    {1, 1, testDigest("other-tail")},
	} {
		t.Run(name, func(t *testing.T) {
			if err := validateBrandNewReplayBoundary(fault.count, fault.records, fault.tail, header); !IsCode(err, CodeEvidenceJournalCorrupt) {
				t.Fatalf("progress accepted: %v", err)
			}
		})
	}
}

func TestGenerationReplayReadyCloseRequiresImmutableRegistry(t *testing.T) {
	ready := &GenerationReplayReady{consumed: &atomic.Bool{}}
	ready.self = ready
	if err := ready.Close(); !IsCode(err, CodeEvidenceJournalFailed) {
		t.Fatalf("literal replay close=%v", err)
	}
	if err := ready.Close(); !IsCode(err, CodeEvidenceJournalFailed) {
		t.Fatalf("double replay close=%v", err)
	}
}

func generationReplayFixture(t *testing.T) (OwnedCurrentCandidate, *GenerationHandoffReady, []byte, [][]byte) {
	t.Helper()
	candidate := quotaCandidateForBundle(t, quotaAdmissionBundleForTest(t), []byte("recovery"))
	bindings, err := candidate.verifiedRun.currentDecision.decision.runnerProjectionBindings()
	if err != nil {
		t.Fatal(err)
	}
	history := &VerifiedAdmissionHistory{
		candidateBinding: candidate.binding, target: digestRaw(bindings.executionLineageDigest), rootFacts: rootFactsForTest(t, nil),
		reservation: evidenceQuotaReservation{ReservedRecords: 2, ReservedJournalBytes: 3, ReservedSegments: 1, ReservedIndexRecords: 4, ReservedIndexBytes: lineageRecordFrameLimits[LineageRecordHeader] + lineageRecordFrameLimits[LineageRecordGenerationReserved], ReservedBytes: 3 + lineageRecordFrameLimits[LineageRecordHeader] + lineageRecordFrameLimits[LineageRecordGenerationReserved]},
		binding:     &verifiedAdmissionHistoryBinding{canonical: [32]byte{1}},
	}
	lineage, reserved, lineageRaw, reservedRaw, err := buildBrandNewAdmissionFrames(history, candidate)
	if err != nil {
		t.Fatal(err)
	}
	header := cloneProjectionValue(reserved.Record.Reserved.PlannedSegment0Header)
	headerFrame := EvidenceFrame{FormatVersion: EvidenceFrameFormat, Sequence: 0, RecordKind: EvidenceRecordHeader, Record: EvidenceRecord{Header: &header}}
	headerFrame.RecordDigest, err = headerFrame.ComputeDigest()
	if err != nil {
		t.Fatal(err)
	}
	headerRaw, err := EncodeCanonicalEvidenceFrame(headerFrame)
	if err != nil {
		t.Fatal(err)
	}
	activated, activatedRaw, err := buildAdmissionActivatedFrame(reserved, headerFrame)
	if err != nil {
		t.Fatal(err)
	}
	plan := &VerifiedAdmissionPlan{history: history, lineageHeaderFrame: lineage, reservedFrame: reserved, lineageHeaderBytes: lineageRaw, reservedFrameBytes: reservedRaw, binding: &verifiedAdmissionPlanBinding{canonical: [32]byte{2}}}
	consumedPermit := &GenerationReadyPermit{plan: plan, history: history, candidateBinding: candidate.binding, activatedFrame: activated, journal: reserved.Record.Reserved.JournalIdentityDigest, reservedDigest: reserved.RecordDigest, headerDigest: headerFrame.RecordDigest, activationDigest: activated.RecordDigest, binding: &generationReadyPermitBinding{canonical: [32]byte{3}}, consumed: &atomic.Bool{}}
	consumedPermit.self = consumedPermit
	consumedPermit.consumed.Store(true)
	ready := &GenerationHandoffReady{prior: consumedPermit, plan: plan, history: history, candidateBinding: candidate.binding, lease: &evidencefs.GenerationLease{}, target: history.target, journal: consumedPermit.journal, revision: 3, reservedDigest: consumedPermit.reservedDigest, headerDigest: consumedPermit.headerDigest, activationDigest: consumedPermit.activationDigest, consumed: &atomic.Bool{}}
	ready.self = ready
	ready.consumed.Store(true)
	ready.binding = &generationHandoffReadyBinding{ready: ready, prior: consumedPermit, plan: plan, history: history, candidateBinding: candidate.binding, lease: ready.lease}
	ready.binding.canonical = generationHandoffReadyDigest(ready)
	generationHandoffReadyRegistry.Store(ready, generationHandoffReadyRegistryRecord{ready: ready, binding: ready.binding, prior: consumedPermit, plan: plan, history: history, candidateBinding: candidate.binding, lease: ready.lease, runtimeReceipt: consumedPermit.runtimeReceipt, recoveryReceipt: consumedPermit.recoveryReceipt, canonical: ready.binding.canonical})
	indexRaw := append(append(append([]byte(nil), lineageRaw...), reservedRaw...), activatedRaw...)
	return candidate, ready, indexRaw, [][]byte{headerRaw}
}
