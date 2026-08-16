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

func TestRegisteredGenerationHandoffRejectsLiteralAndKeepsClosedDiagnosis(t *testing.T) {
	candidate := quotaCandidateForBundle(t, quotaAdmissionBundleForTest(t), []byte("registered-handoff"))
	if permit, err := bindRegisteredGenerationHandoff(context.Background(), &VerifiedAdmissionHistory{}, candidate); permit != nil || !IsCode(err, CodeEvidenceRecoveryRequired) {
		t.Fatalf("literal history permit=%+v err=%v", permit, err)
	}
	result, err := (&RegisteredGenerationHandoffPermit{}).Handoff(context.Background(), candidate)
	if result.Next() != nil || result.Outcome() != evidencefs.AdmissionTransitionPreMutationFailure || result.CandidateKind() != "registered_generation_handoff" || result.CandidateSequence() != 1 || !IsCode(err, CodeEvidenceRecoveryRequired) {
		t.Fatalf("literal permit result=%+v err=%v", result, err)
	}
	if validRegisteredGenerationRecoveryReady(&RegisteredGenerationRecoveryReady{}, candidate) {
		t.Fatal("literal registered recovery authority passed validation")
	}
}

func TestRegisteredGenerationHandoffPermitDigestRejectsCopyAndMutation(t *testing.T) {
	candidate := quotaCandidateForBundle(t, quotaAdmissionBundleForTest(t), []byte("registered-handoff-digest"))
	replay := &verifiedAdmissionGenerationReplay{canonical: [32]byte{2}}
	registered := &verifiedAdmissionRegisteredGeneration{replay: replay, canonical: [32]byte{3}}
	registered.descriptor.identity.journalIdentityDigest = testDigest("registered-handoff-journal")
	history := &VerifiedAdmissionHistory{target: [32]byte{4}, revision: 5, binding: &verifiedAdmissionHistoryBinding{canonical: [32]byte{6}}}
	permit := &RegisteredGenerationHandoffPermit{
		history: history, registered: registered, candidateBinding: candidate.binding,
		inventory: &evidencefs.AdmissionInventory{}, mutation: &evidencefs.AdmissionMutationToken{},
		target: history.target, journal: registered.descriptor.identity.journalIdentityDigest,
		revision: history.revision, consumed: &atomic.Bool{},
	}
	permit.self = permit
	want := registeredGenerationHandoffPermitDigest(permit)
	if want == ([32]byte{}) {
		t.Fatal("registered handoff permit digest is empty")
	}
	permit.binding = &registeredGenerationHandoffPermitBinding{canonical: want}
	if registeredGenerationHandoffCandidateDigest(permit) == ([32]byte{}) {
		t.Fatal("registered handoff candidate digest is empty")
	}
	copyPermit := *permit
	if registeredGenerationHandoffPermitDigest(&copyPermit) != ([32]byte{}) {
		t.Fatal("copied registered handoff permit retained digest authority")
	}
	for name, mutate := range map[string]func(*RegisteredGenerationHandoffPermit){
		"history":   func(v *RegisteredGenerationHandoffPermit) { v.history.binding.canonical[0]++ },
		"source":    func(v *RegisteredGenerationHandoffPermit) { v.registered.canonical[0]++ },
		"replay":    func(v *RegisteredGenerationHandoffPermit) { v.registered.replay.canonical[0]++ },
		"candidate": func(v *RegisteredGenerationHandoffPermit) { v.candidateBinding.canonical[0]++ },
		"target":    func(v *RegisteredGenerationHandoffPermit) { v.target[0]++ },
		"journal":   func(v *RegisteredGenerationHandoffPermit) { v.journal = testDigest("other-journal") },
		"revision":  func(v *RegisteredGenerationHandoffPermit) { v.revision++ },
	} {
		t.Run(name, func(t *testing.T) {
			value := *permit
			value.self = &value
			historyCopy, registeredCopy, replayCopy, bindingCopy := *history, *registered, *replay, *candidate.binding
			historyBinding := *history.binding
			historyCopy.binding = &historyBinding
			registeredCopy.replay = &replayCopy
			value.history, value.registered, value.candidateBinding = &historyCopy, &registeredCopy, &bindingCopy
			mutate(&value)
			if registeredGenerationHandoffPermitDigest(&value) == want {
				t.Fatal("permit mutation did not change its digest")
			}
		})
	}
}

func TestRegisteredGenerationSnapshotFactsRequireExactIndexAndOrderedSegments(t *testing.T) {
	replay := &verifiedAdmissionGenerationReplay{
		indexFact:    evidencefs.GenerationFileFact{Ordinal: 0, Size: 10, ContentDigest: [32]byte{1}, IdentityDigest: [32]byte{2}},
		segmentFacts: []evidencefs.GenerationFileFact{{Ordinal: 0, Size: 11, ContentDigest: [32]byte{3}, IdentityDigest: [32]byte{4}}, {Ordinal: 1, Size: 12, ContentDigest: [32]byte{5}, IdentityDigest: [32]byte{6}}},
	}
	if !registeredGenerationFileFactsMatch(replay, replay.indexFact, replay.segmentFacts) {
		t.Fatal("exact registered generation file facts were rejected")
	}
	for name, mutate := range map[string]func(*evidencefs.GenerationFileFact, []evidencefs.GenerationFileFact){
		"index": func(index *evidencefs.GenerationFileFact, _ []evidencefs.GenerationFileFact) { index.Size++ },
		"segment": func(_ *evidencefs.GenerationFileFact, segments []evidencefs.GenerationFileFact) {
			segments[0].ContentDigest[0]++
		},
		"order": func(_ *evidencefs.GenerationFileFact, segments []evidencefs.GenerationFileFact) {
			segments[0], segments[1] = segments[1], segments[0]
		},
	} {
		t.Run(name, func(t *testing.T) {
			index := replay.indexFact
			segments := append([]evidencefs.GenerationFileFact(nil), replay.segmentFacts...)
			mutate(&index, segments)
			if registeredGenerationFileFactsMatch(replay, index, segments) {
				t.Fatal("snapshot fact mutation was accepted")
			}
		})
	}
	if registeredGenerationFileFactsMatch(replay, replay.indexFact, replay.segmentFacts[:1]) || registeredGenerationFileFactsMatch(nil, replay.indexFact, replay.segmentFacts) {
		t.Fatal("incomplete registered snapshot facts were accepted")
	}
}

func TestRegisteredGenerationRecoveryReadyDigestBindsRenewedAuthority(t *testing.T) {
	replay, identity, _, _, _, _ := registeredGenerationReplayFixture(t, 5)
	cursor, err := renewGenerationJournalCursor(replay.cursor)
	if err != nil {
		t.Fatal(err)
	}
	recovery, err := renewGenerationJournalRecovery(replay.recovery, cursor, identity)
	if err != nil {
		t.Fatal(err)
	}
	candidateBinding := &verifiedEvidenceRunBinding{canonical: [32]byte{7}}
	registered := &verifiedAdmissionRegisteredGeneration{descriptor: GenerationDescriptor{identity: identity}, replay: replay, canonical: [32]byte{8}}
	history := &VerifiedAdmissionHistory{binding: &verifiedAdmissionHistoryBinding{canonical: [32]byte{9}}}
	permit := &RegisteredGenerationHandoffPermit{binding: &registeredGenerationHandoffPermitBinding{canonical: [32]byte{10}}}
	ready := &RegisteredGenerationRecoveryReady{
		prior: permit, history: history, registered: registered, candidateBinding: candidateBinding,
		lease: &evidencefs.GenerationLease{}, snapshot: &evidencefs.GenerationSnapshot{}, replay: replay, generation: identity,
		cursor: cursor, recovery: recovery, snapshotIdentity: [32]byte{11}, consumed: &atomic.Bool{},
	}
	ready.self = ready
	want := registeredGenerationRecoveryReadyDigest(ready)
	if want == ([32]byte{}) {
		t.Fatal("registered recovery-ready digest is empty")
	}
	copyReady := *ready
	if registeredGenerationRecoveryReadyDigest(&copyReady) != ([32]byte{}) {
		t.Fatal("copied registered recovery-ready value retained digest authority")
	}
	for name, mutate := range map[string]func(*RegisteredGenerationRecoveryReady){
		"source": func(v *RegisteredGenerationRecoveryReady) { v.registered.canonical[0]++ },
		"source replay": func(v *RegisteredGenerationRecoveryReady) {
			v.registered.replay.canonical[0]++
		},
		"renewed replay": func(v *RegisteredGenerationRecoveryReady) { v.replay.canonical[0]++ },
		"snapshot":       func(v *RegisteredGenerationRecoveryReady) { v.snapshotIdentity[0]++ },
		"cursor":         func(v *RegisteredGenerationRecoveryReady) { v.cursor.nextSequence++ },
		"recovery":       func(v *RegisteredGenerationRecoveryReady) { v.recovery.state = RecoveryDivergent },
		"execution": func(v *RegisteredGenerationRecoveryReady) {
			v.executionBindings = &VerifiedRecoveryExecutionBindings{digest: testDigest("execution")}
		},
	} {
		t.Run(name, func(t *testing.T) {
			value := *ready
			value.self = &value
			registeredCopy, replayCopy := *registered, *replay
			registeredCopy.replay = &replayCopy
			value.registered = &registeredCopy
			value.cursor = cursor.clone()
			value.recovery = cloneRecoverySnapshot(recovery)
			value.replay = cloneVerifiedAdmissionGenerationReplay(replay)
			mutate(&value)
			if registeredGenerationRecoveryReadyDigest(&value) == want {
				t.Fatal("recovery-ready mutation did not change its digest")
			}
		})
	}
	cursor.valid.Store(false)
}

func TestRegisteredGenerationRecoveryReadyCloseUsesImmutableRegistry(t *testing.T) {
	cursorValid, oldCursorValid := &atomic.Bool{}, &atomic.Bool{}
	cursorValid.Store(true)
	oldCursorValid.Store(true)
	runtimeBinding, recoveryBinding := &verifiedContentReceiptBinding{}, &verifiedDecisionRecoveryReceiptBinding{}
	verifiedContentReceiptRegistry.Store(runtimeBinding, runtimeBinding)
	verifiedDecisionRecoveryReceiptRegistry.Store(recoveryBinding, recoveryBinding)
	registered := &verifiedAdmissionRegisteredGeneration{
		runtimeReceipt: VerifiedContentReceipt{binding: runtimeBinding}, recoveryReceipt: VerifiedDecisionRecoveryReceipt{binding: recoveryBinding},
		replay: &verifiedAdmissionGenerationReplay{cursor: JournalCursor{valid: oldCursorValid}, canonical: [32]byte{3}}, handoffConsumed: &atomic.Bool{},
	}
	readyReplay := &verifiedAdmissionGenerationReplay{canonical: [32]byte{4}}
	history := &VerifiedAdmissionHistory{binding: &verifiedAdmissionHistoryBinding{canonical: [32]byte{1}}}
	verifiedAdmissionHistoryRegistry.Store(history.binding, history.binding.canonical)
	prior := &RegisteredGenerationHandoffPermit{}
	ready := &RegisteredGenerationRecoveryReady{prior: prior, registered: registered, history: history, lease: &evidencefs.GenerationLease{}, replay: readyReplay, cursor: JournalCursor{valid: cursorValid}, consumed: &atomic.Bool{}}
	ready.self = ready
	ready.binding = &registeredGenerationRecoveryReadyBinding{ready: ready, history: history, registered: registered, lease: ready.lease, replay: readyReplay, canonical: [32]byte{2}}
	registeredGenerationRecoveryReadyRegistry.Store(ready, registeredGenerationRecoveryReadyRegistryRecord{
		ready: ready, binding: ready.binding, prior: prior, registered: registered, history: history, lease: ready.lease, replay: readyReplay,
		cursorValid: cursorValid, handoffConsumed: registered.handoffConsumed, readyConsumed: ready.consumed,
		oldCursorValid: oldCursorValid, runtimeBinding: runtimeBinding, recoveryBinding: recoveryBinding,
		sourceReplayCanonical: registered.replay.canonical, readyReplayCanonical: readyReplay.canonical, canonical: ready.binding.canonical,
	})
	ready.registered, ready.history, ready.lease, ready.replay = nil, nil, nil, nil
	if err := ready.Close(); !IsCode(err, CodeEvidenceJournalFailed) {
		t.Fatalf("literal immutable lease close=%v", err)
	}
	if cursorValid.Load() || oldCursorValid.Load() {
		t.Fatal("close retained a registered generation cursor")
	}
	if _, ok := registeredGenerationRecoveryReadyRegistry.Load(ready); ok {
		t.Fatal("close retained recovery-ready registry entry")
	}
	if _, ok := verifiedAdmissionHistoryRegistry.Load(history.binding); ok {
		t.Fatal("close retained admission history registry entry")
	}
	if _, ok := verifiedContentReceiptRegistry.Load(runtimeBinding); ok {
		t.Fatal("close retained registered runtime receipt")
	}
	if _, ok := verifiedDecisionRecoveryReceiptRegistry.Load(recoveryBinding); ok {
		t.Fatal("close retained registered recovery receipt")
	}
	if err := ready.Close(); !IsCode(err, CodeEvidenceJournalFailed) {
		t.Fatalf("double close=%v", err)
	}
}

func TestRegisteredGenerationHandoffAuthorityDoesNotSpread(t *testing.T) {
	t.Parallel()
	guarded := map[string]bool{
		"RegisteredGenerationHandoffPermit":      true,
		"RegisteredGenerationRecoveryReady":      true,
		"bindRegisteredGenerationHandoff":        true,
		"validRegisteredGenerationHandoffPermit": true,
		"validRegisteredGenerationRecoveryReady": true,
	}
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") || name == "evidence_registered_generation_handoff.go" || name == "evidence_generation_prefix_activation.go" || name == "evidence_generation_journal.go" || name == "evidence_session.go" {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			identifier, ok := node.(*ast.Ident)
			if ok && guarded[identifier.Name] {
				t.Fatalf("registered generation handoff authority %s spread into %s", identifier.Name, name)
			}
			return true
		})
	}
}
