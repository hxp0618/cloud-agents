package migration

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func TestEvidenceSessionRejectsLiteralAuthority(t *testing.T) {
	candidate := quotaCandidateForBundle(t, quotaAdmissionBundleForTest(t), []byte("session-literal"))
	if session, err := (&GenerationRecoveryReady{}).BindSession(context.Background(), candidate); session != nil || !IsCode(err, CodeEvidenceRecoveryRequired) {
		t.Fatalf("literal recovery entered session binder: session=%T err=%v", session, err)
	}
	if session, err := (&RegisteredGenerationRecoveryReady{}).BindSession(context.Background(), candidate); session != nil || !IsCode(err, CodeEvidenceRecoveryRequired) {
		t.Fatalf("literal registered recovery entered session binder: session=%T err=%v", session, err)
	}
	if session, err := (&HistoricalSuccessorGenerationRecoveryReady{}).BindSession(context.Background(), candidate); session != nil || !IsCode(err, CodeEvidenceRecoveryRequired) {
		t.Fatalf("literal historical successor recovery entered session binder: session=%T err=%v", session, err)
	}
	literal := &generationEvidenceSession{}
	if candidate := literal.CurrentCandidate(); candidate.binding != nil {
		t.Fatal("literal session returned a candidate")
	}
	if active := literal.ActiveGeneration(); active.binding != nil {
		t.Fatal("literal session returned an active generation")
	}
	if journal := literal.Journal(); journal != nil {
		t.Fatalf("literal session returned journal %T", journal)
	}
	if snapshot := literal.RecoverySnapshot(); snapshot != nil {
		t.Fatal("literal session returned a recovery snapshot")
	}
	authority := &VerifiedLineageSupersessionAuthority{}
	if active, snapshot, err := literal.ReserveAndActivateSuccessor(context.Background(), authority); active.binding != nil || snapshot != nil || !IsCode(err, CodeEvidenceJournalFailed) || authority.consumed.Load() {
		t.Fatalf("literal successor active=%+v snapshot=%+v err=%v consumed=%v", active, snapshot, err, authority.consumed.Load())
	}
	if err := literal.Close(context.Background()); !IsCode(err, CodeEvidenceJournalFailed) {
		t.Fatalf("literal close=%v", err)
	}
}

func TestActiveGenerationDigestBindsImmutableFacts(t *testing.T) {
	owner := &evidenceOwnerToken{nonce: [16]byte{41}}
	journal := &generationEvidenceJournal{generation: generationIdentity{
		owner: owner, executionLineageDigest: testDigest("session-lineage"), journalIdentityDigest: testDigest("session-journal"),
		runnerProjectionDecisionDigest: testDigest("session-decision"), schemaBundleDigest: testDigest("session-schema"),
	}}
	journal.self = journal
	journal.binding = &generationEvidenceJournalBinding{journal: journal, canonical: [32]byte{1}}
	decisionOwner := &recoveryVerifierOwner{token: owner}
	active := ActiveGeneration{
		identity: journal.generation, kind: activeGenerationCurrent, journal: journal,
		ownedDecision: OwnedVerifiedDecision{owner: decisionOwner, digest: journal.generation.runnerProjectionDecisionDigest},
		contentReceipt: VerifiedContentReceipt{
			owner: owner, kind: durableRuntimeContentObject, digest: testDigest("session-runtime"), sizeBytes: 7,
			binding: &verifiedContentReceiptBinding{},
		},
		decisionRecoveryReceipt: VerifiedDecisionRecoveryReceipt{
			owner: owner, kind: durableDecisionRecoveryContentObject, digest: testDigest("session-recovery"), sizeBytes: 9,
			binding: &verifiedDecisionRecoveryReceiptBinding{},
		},
	}
	baseline := activeGenerationDigest(active)
	if baseline == ([32]byte{}) {
		t.Fatal("active generation digest is empty")
	}
	faults := map[string]func(*ActiveGeneration){
		"kind":           func(v *ActiveGeneration) { v.kind = activeGenerationAncestorRecovery },
		"lineage":        func(v *ActiveGeneration) { v.identity.executionLineageDigest = testDigest("other-lineage") },
		"decision":       func(v *ActiveGeneration) { v.ownedDecision.digest = testDigest("other-decision") },
		"runtime digest": func(v *ActiveGeneration) { v.contentReceipt.digest = testDigest("other-runtime") },
		"runtime size":   func(v *ActiveGeneration) { v.contentReceipt.sizeBytes++ },
		"recovery":       func(v *ActiveGeneration) { v.decisionRecoveryReceipt.digest = testDigest("other-recovery") },
	}
	for name, mutate := range faults {
		t.Run(name, func(t *testing.T) {
			value := active
			mutate(&value)
			if got := activeGenerationDigest(value); got == baseline {
				t.Fatal("mutation did not change active generation digest")
			}
		})
	}
	historical := active
	historical.kind = activeGenerationAncestorRecovery
	historical.recoveryExecutionBindings = &VerifiedRecoveryExecutionBindings{digest: testDigest("session-execution")}
	historicalDigest := activeGenerationDigest(historical)
	if historicalDigest == ([32]byte{}) || historicalDigest == baseline {
		t.Fatal("historical recovery execution was not bound into active generation digest")
	}
	mutatedExecution := historical
	mutatedExecution.recoveryExecutionBindings = &VerifiedRecoveryExecutionBindings{digest: testDigest("other-execution")}
	if activeGenerationDigest(mutatedExecution) == historicalDigest {
		t.Fatal("recovery execution digest mutation did not change active generation digest")
	}
	invalidCurrent := active
	invalidCurrent.recoveryExecutionBindings = historical.recoveryExecutionBindings
	if activeGenerationDigest(invalidCurrent) != ([32]byte{}) {
		t.Fatal("current active generation accepted ancestor recovery execution")
	}
	invalidHistorical := historical
	invalidHistorical.recoveryExecutionBindings = nil
	if activeGenerationDigest(invalidHistorical) != ([32]byte{}) {
		t.Fatal("ancestor active generation accepted missing recovery execution")
	}
}

func TestRecoveryExecutionBindingsCloneAndMatchOwnedBoundary(t *testing.T) {
	generation, original := sessionRecoveryExecutionBindingsFixture(t)
	clone := cloneRecoveryExecutionBindings(original)
	baseline := cloneRecoveryExecutionBindings(original)
	currentDecision := original.subject.CurrentRunnerProjectionDecisionDigest
	if clone == original || clone.snapshot == original.snapshot || !sameRecoveryExecutionBindings(clone, baseline, generation, currentDecision) {
		t.Fatal("recovery execution clone did not retain an owned matching boundary")
	}
	original.snapshot.state = RecoveryTerminal
	original.policy.AllowedOutcomes[0] = "confirmed_abort_terminal"
	original.subject.ActionsProfile = "mutated"
	if !sameRecoveryExecutionBindings(clone, baseline, generation, currentDecision) {
		t.Fatal("owned recovery execution clone shared mutable nested state")
	}
	if sameRecoveryExecutionBindings(original, clone, generation, currentDecision) {
		t.Fatal("mutated recovery execution still matched its owned clone")
	}
	tailSwap := cloneRecoveryExecutionBindings(clone)
	tailSwap.snapshot.tailDigest = testDigest("other-snapshot-tail")
	if sameRecoveryExecutionBindings(tailSwap, clone, generation, currentDecision) {
		t.Fatal("snapshot tail swap matched recovery execution tail")
	}
	cursorSwap := cloneRecoveryExecutionBindings(clone)
	cursorSwap.snapshot.cursor.valid = &atomic.Bool{}
	if sameRecoveryExecutionBindings(cursorSwap, clone, generation, currentDecision) {
		t.Fatal("snapshot cursor authority swap matched recovery execution boundary")
	}
	if sameRecoveryExecutionBindings(clone, baseline, generation, testDigest("other-current-decision")) {
		t.Fatal("current decision swap matched recovery execution boundary")
	}
}

func sessionRecoveryExecutionBindingsFixture(t *testing.T) (generationIdentity, *VerifiedRecoveryExecutionBindings) {
	t.Helper()
	owner := &evidenceOwnerToken{nonce: [16]byte{42}}
	generation := generationIdentity{
		owner: owner, executionLineageDigest: testDigest("session-execution-lineage"), journalIdentityDigest: testDigest("session-execution-journal"),
		runnerProjectionDecisionDigest: testDigest("session-execution-old-decision"), schemaBundleDigest: testDigest("session-execution-schema"),
	}
	tail := testDigest("session-execution-tail")
	valid := &atomic.Bool{}
	valid.Store(true)
	cursor := JournalCursor{
		owner: owner, generation: generation, segmentIndex: 1, nextSequence: 7, previousRecordDigest: &tail,
		lineageIndexNextSequence: 5, lineageIndexPreviousRecordDigest: testDigest("session-execution-index-tail"), valid: valid,
	}
	snapshot := &RecoverySnapshot{
		owner: owner, generation: generation, cursor: cursor.clone(), tailDigest: tail,
		state: RecoveryBrandNew, nextPermittedAction: RecoveryBeginFirstAttempt,
	}
	policy := recoveryPolicyFixtureSubject(generation)
	policyDigest, err := policy.ComputeDigest()
	if err != nil {
		t.Fatal(err)
	}
	subject := recoveryExecutionBindingsSubject{
		HistoricalRecoveryPolicyDigest: policyDigest, ExecutionLineageDigest: generation.executionLineageDigest,
		CurrentRunnerProjectionDecisionDigest: policy.SuccessorRunnerProjectionDecisionDigest, OldRunnerProjectionDecisionDigest: generation.runnerProjectionDecisionDigest,
		OldJournalIdentityDigest: generation.journalIdentityDigest, OldSchemaBundleDigest: generation.schemaBundleDigest,
		OldDecisionRecoveryArtifactSHA256: policy.OldDecisionRecoveryArtifactSHA256, OldDecisionRecoveryArtifactSizeBytes: policy.OldDecisionRecoveryArtifactSizeBytes,
		OldJournalReplayTailDigest: tail, OldRecoveryState: string(snapshot.state), ActionsProfile: oldAttemptRecoveryActionsProfile,
	}
	digest, err := subject.ComputeDigest()
	if err != nil {
		t.Fatal(err)
	}
	verifier := &recoveryVerifierOwner{token: owner}
	return generation, &VerifiedRecoveryExecutionBindings{
		owner: verifier, session: owner, generation: generation, tailDigest: tail, snapshot: snapshot,
		policy: policy, subject: subject, digest: digest,
	}
}

func TestCloneSessionCandidateOwnsArtifactBytes(t *testing.T) {
	candidate := quotaCandidateForBundle(t, quotaAdmissionBundleForTest(t), []byte("session-candidate-copy"))
	owned, err := cloneSessionCandidate(candidate)
	if err != nil || !validOwnedCurrentCandidate(owned) {
		t.Fatalf("owned candidate err=%v valid=%v", err, validOwnedCurrentCandidate(owned))
	}
	if len(candidate.runtimeArtifact.bytes) == 0 || len(candidate.decisionRecoveryArtifact.bytes) == 0 {
		t.Fatal("candidate fixture has empty artifacts")
	}
	candidate.runtimeArtifact.bytes[0] ^= 1
	candidate.decisionRecoveryArtifact.bytes[0] ^= 1
	candidate.verifiedRun.decisionRecoveryArtifact.bytes[0] ^= 1
	if !validOwnedCurrentCandidate(owned) {
		t.Fatal("owned session candidate shared mutable artifact bytes")
	}
}

func TestSessionSuccessorAuthorityPrevalidationBindsExactCurrentBoundary(t *testing.T) {
	session, journal, authority := sessionSuccessorAuthorityFixture(t)
	if !validSessionSuccessorAuthorityLocked(session, journal, authority) {
		t.Fatal("exact successor authority did not match the current session boundary")
	}
	clone := func() *VerifiedLineageSupersessionAuthority {
		return &VerifiedLineageSupersessionAuthority{
			owner: authority.owner, session: authority.session, generation: authority.generation,
			tailDigest: authority.tailDigest, subject: cloneProjectionValue(authority.subject), digest: authority.digest,
		}
	}
	faults := map[string]func(*VerifiedLineageSupersessionAuthority){
		"owner":   func(v *VerifiedLineageSupersessionAuthority) { v.owner = &recoveryVerifierOwner{} },
		"session": func(v *VerifiedLineageSupersessionAuthority) { v.session = &evidenceOwnerToken{} },
		"generation": func(v *VerifiedLineageSupersessionAuthority) {
			v.generation.journalIdentityDigest = testDigest("other-journal")
		},
		"tail":   func(v *VerifiedLineageSupersessionAuthority) { v.tailDigest = testDigest("other-tail") },
		"digest": func(v *VerifiedLineageSupersessionAuthority) { v.digest = testDigest("other-authority") },
		"execution": func(v *VerifiedLineageSupersessionAuthority) {
			v.subject.RecoveryExecutionBindingsDigest = testDigest("other-execution")
		},
		"checkpoint": func(v *VerifiedLineageSupersessionAuthority) {
			v.subject.OldCheckpointRecordDigest = digestPointer(testDigest("other-checkpoint"))
		},
		"terminal": func(v *VerifiedLineageSupersessionAuthority) {
			v.subject.OldTerminalDigest = digestPointer(testDigest("other-terminal"))
		},
		"successor": func(v *VerifiedLineageSupersessionAuthority) {
			v.subject.SuccessorSchemaBundleDigest = testDigest("other-schema")
		},
		"continuation": func(v *VerifiedLineageSupersessionAuthority) {
			v.subject.Continuation = &LineageContinuationContext{StartAction: "begin_next_attempt"}
		},
	}
	for name, mutate := range faults {
		t.Run(name, func(t *testing.T) {
			value := clone()
			mutate(value)
			if validSessionSuccessorAuthorityLocked(session, journal, value) {
				t.Fatal("mutated successor authority matched the current session boundary")
			}
		})
	}
	consumed := clone()
	consumed.consumed.Store(true)
	if validSessionSuccessorAuthorityLocked(session, journal, consumed) {
		t.Fatal("consumed successor authority passed prevalidation")
	}
	current := session.active
	session.active.kind = activeGenerationCurrent
	if validSessionSuccessorAuthorityLocked(session, journal, clone()) {
		t.Fatal("current generation entered historical successor path")
	}
	session.active = current
	journal.state.cursor.latestCheckpointRecordDigest = digestPointer(testDigest("other-checkpoint"))
	if validSessionSuccessorAuthorityLocked(session, journal, clone()) {
		t.Fatal("stale checkpoint authority matched a changed journal boundary")
	}
}

func TestDetachedRegisteredSourceRevocationDoesNotCloseLeaseTwice(t *testing.T) {
	valid := &atomic.Bool{}
	valid.Store(true)
	runtimeBinding := &verifiedContentReceiptBinding{}
	recoveryBinding := &verifiedDecisionRecoveryReceiptBinding{}
	registeredReplay := &verifiedAdmissionGenerationReplay{cursor: JournalCursor{valid: valid}}
	registered := &verifiedAdmissionRegisteredGeneration{
		replay:          registeredReplay,
		runtimeReceipt:  VerifiedContentReceipt{binding: runtimeBinding},
		recoveryReceipt: VerifiedDecisionRecoveryReceipt{binding: recoveryBinding},
	}
	historyBinding := &verifiedAdmissionHistoryBinding{}
	history := &VerifiedAdmissionHistory{binding: historyBinding}
	handoff := &RegisteredGenerationHandoffPermit{}
	ready := &RegisteredGenerationRecoveryReady{
		prior: handoff, history: history, registered: registered, cursor: JournalCursor{valid: valid},
	}
	state := &generationEvidenceJournalState{cursor: JournalCursor{valid: valid}}
	registeredGenerationRecoveryReadyRegistry.Store(ready, true)
	registeredGenerationHandoffPermitRegistry.Store(handoff, true)
	verifiedAdmissionHistoryRegistry.Store(historyBinding, true)
	verifiedContentReceiptRegistry.Store(runtimeBinding, runtimeBinding)
	verifiedDecisionRecoveryReceiptRegistry.Store(recoveryBinding, recoveryBinding)
	detachedGenerationSession{source: generationEvidenceJournalRegistryRecord{registeredPrior: ready, state: state}}.revokeSource()
	if valid.Load() {
		t.Fatal("detached registered cursor remained valid")
	}
	for _, entry := range []struct {
		name     string
		registry *sync.Map
		key      any
	}{
		{"recovery", &registeredGenerationRecoveryReadyRegistry, ready},
		{"handoff", &registeredGenerationHandoffPermitRegistry, handoff},
		{"history", &verifiedAdmissionHistoryRegistry, historyBinding},
		{"runtime receipt", &verifiedContentReceiptRegistry, runtimeBinding},
		{"recovery receipt", &verifiedDecisionRecoveryReceiptRegistry, recoveryBinding},
	} {
		if _, ok := entry.registry.Load(entry.key); ok {
			t.Fatalf("%s registry authority survived detached-source revocation", entry.name)
		}
	}
}

func TestSessionSuccessorCleanupRevokesEntireInMemoryAuthorityChain(t *testing.T) {
	runtimeBinding := &verifiedContentReceiptBinding{}
	recoveryBinding := &verifiedDecisionRecoveryReceiptBinding{}
	first := &successorAdmissionState{binding: &successorAdmissionStateBinding{}}
	second := &successorAdmissionState{
		prior: first, binding: &successorAdmissionStateBinding{},
		runtimeReceipt:  VerifiedContentReceipt{binding: runtimeBinding},
		recoveryReceipt: VerifiedDecisionRecoveryReceipt{binding: recoveryBinding},
	}
	plan := &VerifiedSuccessorAdmissionPlan{binding: &verifiedSuccessorAdmissionPlanBinding{}}
	history := &VerifiedAdmissionHistory{binding: &verifiedAdmissionHistoryBinding{}}
	for _, state := range []*successorAdmissionState{first, second} {
		successorAdmissionStateRegistry.Store(state.binding, true)
	}
	verifiedContentReceiptRegistry.Store(runtimeBinding, runtimeBinding)
	verifiedDecisionRecoveryReceiptRegistry.Store(recoveryBinding, recoveryBinding)
	verifiedSuccessorAdmissionPlanRegistry.Store(plan.binding, true)
	verifiedAdmissionHistoryRegistry.Store(history.binding, true)
	cleanup := sessionSuccessorCleanup{state: second, plan: plan, history: history}
	cleanup.revokeInMemory()
	for name, registry := range map[string]struct {
		registry *sync.Map
		key      any
	}{
		"first state":      {&successorAdmissionStateRegistry, first.binding},
		"second state":     {&successorAdmissionStateRegistry, second.binding},
		"runtime receipt":  {&verifiedContentReceiptRegistry, runtimeBinding},
		"recovery receipt": {&verifiedDecisionRecoveryReceiptRegistry, recoveryBinding},
		"plan":             {&verifiedSuccessorAdmissionPlanRegistry, plan.binding},
		"history":          {&verifiedAdmissionHistoryRegistry, history.binding},
	} {
		if _, ok := registry.registry.Load(registry.key); ok {
			t.Fatalf("%s authority survived successor cleanup", name)
		}
	}
}

func TestEvidenceSessionSuccessorTransitionOrderIsClosed(t *testing.T) {
	raw, err := os.ReadFile("evidence_session.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(raw)
	start := strings.Index(source, "func (s *generationEvidenceSession) ReserveAndActivateSuccessor")
	end := strings.Index(source[start:], "// detachedGenerationSession")
	if start < 0 || end < 0 {
		t.Fatal("successor session method boundary is unavailable")
	}
	method := source[start : start+end]
	steps := []string{
		".ReacquireAdmission(", ".MutationToken(", "bindVerifiedAdmissionHistory(", "bindVerifiedSuccessorAdmissionPlan(",
		"bindSuccessorAdmissionPermit(", ".PublishRuntime(", ".BindRuntime(", ".PublishDecisionRecovery(",
		".BindDecisionRecovery(", ".SealReserveReady(", ".BindReceiptPair(", ".AppendGenerationSuperseded(",
		".AppendGenerationReserved(", ".CreateGenerationHeader(", ".AppendGenerationActivated(", ".Handoff(",
		".Replay(", ".BindRecovery(", ".BindJournal(", ".installSuccessorLocked(",
	}
	previous := -1
	for _, step := range steps {
		position := strings.Index(method, step)
		if position < 0 || position <= previous {
			t.Fatalf("successor transition %s is absent or out of order", step)
		}
		previous = position
	}
	if strings.Contains(method, "CodeProjectionNotImplemented") {
		t.Fatal("successor session still contains an unimplemented projection exit")
	}
}

func sessionSuccessorAuthorityFixture(t *testing.T) (*generationEvidenceSession, *generationEvidenceJournal, *VerifiedLineageSupersessionAuthority) {
	t.Helper()
	generation, execution := sessionRecoveryExecutionBindingsFixture(t)
	checkpoint := testDigest("session-successor-checkpoint")
	execution.snapshot.cursor.latestCheckpointRecordDigest = digestPointer(checkpoint)
	execution.snapshot.cursor.lineageIndexPreviousRecordDigest = checkpoint
	execution.snapshot.cursor.previousRecordDigest = digestPointer(execution.tailDigest)
	execution.snapshot.tailDigest = execution.tailDigest
	execution.snapshot.generation = generation
	execution.snapshot.owner = generation.owner
	execution.snapshot.cursor.owner = generation.owner
	execution.snapshot.cursor.generation = generation
	recovery := cloneRecoverySnapshot(execution.snapshot)
	journal := &generationEvidenceJournal{generation: generation, state: &generationEvidenceJournalState{cursor: recovery.cursor.clone(), recovery: recovery}}
	session := &generationEvidenceSession{
		candidate: OwnedCurrentCandidate{owner: generation.owner, verifiedRun: VerifiedEvidenceRun{currentDecision: OwnedVerifiedDecision{owner: execution.owner, digest: execution.subject.CurrentRunnerProjectionDecisionDigest}}},
		journal:   journal,
		active:    ActiveGeneration{identity: generation, kind: activeGenerationAncestorRecovery, journal: journal, recoveryExecutionBindings: execution},
	}
	subject := supersessionSubject(*execution, checkpoint, "", "", nil, nil, "exact_committed_bundle_complete", nil)
	digest, err := subject.ComputeDigest()
	if err != nil {
		t.Fatal(err)
	}
	authority := &VerifiedLineageSupersessionAuthority{
		owner: execution.owner, session: generation.owner, generation: generation,
		tailDigest: execution.tailDigest, subject: subject, digest: digest,
	}
	return session, journal, authority
}

func TestEvidenceSessionInternalsDoNotSpread(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || name == "evidence_runtime.go" || name == "evidence_session.go" || len(name) < 3 || name[len(name)-3:] != ".go" || len(name) >= 8 && name[len(name)-8:] == "_test.go" {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			identifier, ok := node.(*ast.Ident)
			if ok && (identifier.Name == "generationEvidenceSession" || identifier.Name == "generationEvidenceSessionBinding" || identifier.Name == "activeGenerationBinding") {
				t.Fatalf("sealed session internals spread into %s", name)
			}
			return true
		})
	}
}
