package migration

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
)

var _ runnerLedgerRetryHandoffBinder = (*generationEvidenceSession)(nil)
var _ runnerLedgerRetryHandoffBinder = (*runnerLedgerPreflightEvidenceFake)(nil)

func (*runnerLedgerPreflightEvidenceFake) runnerLedgerRetryHandoffBinderSealed() {}

func (evidence *runnerLedgerPreflightEvidenceFake) bindRunnerLedgerRetryHandoff(ctx context.Context, permit *runnerLedgerRetryHandoffPermit) (ActiveGeneration, *RecoverySnapshot, error) {
	evidence.retryHandoffBindCalls++
	claimed, err := consumeRunnerLedgerRetryHandoffPermit(permit, evidence)
	if err != nil {
		return ActiveGeneration{}, nil, err
	}
	if err := contextAdmissionError(ctx); err != nil {
		return ActiveGeneration{}, nil, err
	}
	if evidence.retryHandoffBindErr != nil {
		return ActiveGeneration{}, nil, evidence.retryHandoffBindErr
	}
	evidence.mu.Lock()
	defer evidence.mu.Unlock()
	base := evidence.runnerEvidenceSessionFake
	if base == nil || base.closed || base.journal == nil || base.snapshot == nil ||
		claimed.candidateBinding != base.candidate.binding || !sameGenerationIdentity(claimed.generation, base.active.identity) ||
		generationJournalRecoveryDigest(base.snapshot) != claimed.recoveryDigest ||
		!sameCursorIdentity(claimed.cursor, base.journal.cursor) {
		return ActiveGeneration{}, nil, fail(CodeEvidenceRecoveryRequired, "runner-ledger-retry-handoff-test-bind", "test ancestor boundary changed", nil)
	}
	claimed.cursor.valid.Store(false)
	oldJournal := base.journal
	oldJournal.closed = true
	oldJournal.closeCalls++

	current := base.candidate
	newGeneration := generationIdentity{
		owner: current.owner, executionLineageDigest: current.verifiedRun.executionLineageDigest,
		journalIdentityDigest:          testDigest("runner-ledger-retry-handoff-successor-journal"),
		runnerProjectionDecisionDigest: current.verifiedRun.runnerProjectionDecisionDigest,
		schemaBundleDigest:             current.verifiedRun.schemaBundleDigest,
	}
	tail := testDigest("runner-ledger-retry-handoff-successor-header")
	valid := &atomic.Bool{}
	valid.Store(true)
	cursor := JournalCursor{
		owner: current.owner, generation: newGeneration, segmentIndex: 0, nextSequence: 1,
		previousRecordDigest:             digestPointer(tail),
		lineageIndexNextSequence:         claimed.cursor.lineageIndexNextSequence + 3,
		lineageIndexPreviousRecordDigest: testDigest("runner-ledger-retry-handoff-successor-activated"),
		valid:                            valid,
	}
	continuation := cloneProjectionValue(claimed.boundary.continuation)
	snapshot := &RecoverySnapshot{
		owner: current.owner, generation: newGeneration, cursor: cursor.clone(), tailDigest: tail,
		state: RecoveryBrandNewInherited, nextPermittedAction: RecoveryBeginNextAttempt,
		migrationID: cloneStringPointer(&continuation.MigrationID), attemptIndex: cloneUint32Pointer(&continuation.AttemptIndex),
		previousAttemptTerminalDigest: cloneDigestPointer(continuation.PreviousAttemptTerminalDigest),
	}
	snapshot.lineageContinuation = &OwnedRecovered[LineageContinuationContext]{
		owner: current.owner, generation: newGeneration, cursor: cursor.clone(), tailDigest: tail,
		recordDigest: testDigest("runner-ledger-retry-handoff-successor-reserved"), value: continuation,
	}
	journal := &runnerEvidenceJournalFake{cursor: cursor.clone(), snapshot: snapshot, bundleComplete: false}
	journal.session = base
	active := ActiveGeneration{
		identity: newGeneration, kind: activeGenerationCurrent, journal: journal,
		ownedDecision: current.verifiedRun.currentDecision,
	}
	if evidence.mutateRetryHandoffResult != nil {
		evidence.mutateRetryHandoffResult(&active, snapshot)
	}
	journal.cursor = snapshot.cursor.clone()
	journal.snapshot = snapshot
	base.journal = journal
	base.snapshot = snapshot
	base.active = active
	evidence.recovery = cloneRecoverySnapshot(snapshot)
	evidence.schema.generation = active.identity
	return active, cloneRecoverySnapshot(snapshot), nil
}

type runnerLedgerRetryHandoffOutcomeCase struct {
	name    string
	outcome string
	open    func(*testing.T) runnerLedgerRetryHandoffFixtureView
}

type runnerLedgerRetryHandoffFixtureView struct {
	evidence  *runnerLedgerPreflightEvidenceFake
	runner    *Runner
	bundle    *RuntimeBundle
	plans     []StatementPlan
	candidate OwnedCurrentCandidate
	close     func()
}

func runnerLedgerRetryHandoffOutcomeCases() []runnerLedgerRetryHandoffOutcomeCase {
	return []runnerLedgerRetryHandoffOutcomeCase{
		{
			name: "precommit-aborted-retryable", outcome: "precommit_aborted_retryable",
			open: func(t *testing.T) runnerLedgerRetryHandoffFixtureView {
				fixture := newRunnerLedgerRecoveryAbortTerminalFixture(t, RecoveryDanglingStatementIntent, RecoveryAppendAbortedRetryable)
				if err := fixture.run(context.Background()); err != nil {
					fixture.close(t)
					t.Fatal(err)
				}
				base := fixture.success.execution.base.service.kernel.base
				return runnerLedgerRetryHandoffFixtureView{
					evidence: fixture.success.execution.base.service.evidence,
					runner:   fixture.success.execution.base.service.kernel.runner,
					bundle:   base.bundle, plans: base.plans, candidate: base.candidate,
					close: func() { fixture.close(t) },
				}
			},
		},
		{
			name: "exact-pending-terminal", outcome: "exact_pending",
			open: func(t *testing.T) runnerLedgerRetryHandoffFixtureView {
				fixture := newRunnerLedgerRecoveryReconciliationFixture(t, RecoveryDanglingCommitIntent, runnerLedgerReconciliationExactPending, 16)
				if err := fixture.run(context.Background()); err != nil {
					fixture.close(t)
					t.Fatal(err)
				}
				base := fixture.success.execution.base.service.kernel.base
				return runnerLedgerRetryHandoffFixtureView{
					evidence: fixture.success.execution.base.service.evidence,
					runner:   fixture.success.execution.base.service.kernel.runner,
					bundle:   base.bundle, plans: base.plans, candidate: base.candidate,
					close: func() { fixture.close(t) },
				}
			},
		},
		{
			name: "resolved-pending", outcome: "resolved_pending",
			open: func(t *testing.T) runnerLedgerRetryHandoffFixtureView {
				fixture := newRunnerLedgerRecoveryReconciliationFixture(t, RecoveryAmbiguousUnresolved, runnerLedgerReconciliationExactPending, 16)
				if err := fixture.run(context.Background()); err != nil {
					fixture.close(t)
					t.Fatal(err)
				}
				base := fixture.success.execution.base.service.kernel.base
				return runnerLedgerRetryHandoffFixtureView{
					evidence: fixture.success.execution.base.service.evidence,
					runner:   fixture.success.execution.base.service.kernel.runner,
					bundle:   base.bundle, plans: base.plans, candidate: base.candidate,
					close: func() { fixture.close(t) },
				}
			},
		},
	}
}

func TestRunnerLedgerRetryHandoffRecognizesExactDurableOutcomes(t *testing.T) {
	for _, test := range runnerLedgerRetryHandoffOutcomeCases() {
		t.Run(test.name, func(t *testing.T) {
			fixture := test.open(t)
			defer fixture.close()
			evidence := fixture.evidence
			snapshot := evidence.RecoverySnapshot()
			selection := runnerLedgerRetryHandoffSelectionFixture(t, evidence, snapshot)
			recoveryDigest := generationJournalRecoveryDigest(snapshot)
			boundary, ok := runnerLedgerRetryHandoffBoundaryFromSnapshot(snapshot, evidence.active.identity, recoveryDigest, snapshot.tailDigest, selection)
			if !ok || boundary.outcome != test.outcome || boundary.canonical == ([32]byte{}) ||
				boundary.continuation.StartAction != "begin_next_attempt" || boundary.continuation.AttemptIndex != selection.attemptIndex+1 ||
				boundary.continuation.PreviousAttemptTerminalDigest == nil ||
				*boundary.continuation.PreviousAttemptTerminalDigest != boundary.terminalDigest {
				t.Fatalf("boundary=%+v selection=%+v", boundary, selection)
			}
			if test.outcome == "resolved_pending" && boundary.resolutionDigest == nil ||
				test.outcome != "resolved_pending" && boundary.resolutionDigest != nil {
				t.Fatalf("outcome=%s resolution=%+v", test.outcome, boundary.resolutionDigest)
			}
			wrongTail := testDigest("runner-ledger-retry-handoff-wrong-tail")
			if _, accepted := runnerLedgerRetryHandoffBoundaryFromSnapshot(snapshot, evidence.active.identity, recoveryDigest, wrongTail, selection); accepted {
				t.Fatal("wrong recovery tail was accepted")
			}
			atLimit := selection
			atLimit.maxAttempts = atLimit.attemptIndex
			if _, accepted := runnerLedgerRetryHandoffBoundaryFromSnapshot(snapshot, evidence.active.identity, recoveryDigest, snapshot.tailDigest, atLimit); accepted {
				t.Fatal("attempt at max-attempt budget was accepted")
			}
			mutated := boundary
			mutated.continuation.AttemptIndex++
			if runnerLedgerRetryHandoffBoundaryDigest(evidence.active.identity, recoveryDigest, snapshot.tailDigest, selection, mutated) == boundary.canonical {
				t.Fatal("continuation mutation preserved boundary canonical")
			}
		})
	}
}

func TestRunnerLedgerRetryHandoffConsumesClosedPermitAndActivatesExactSuccessor(t *testing.T) {
	for _, test := range runnerLedgerRetryHandoffOutcomeCases() {
		t.Run(test.name, func(t *testing.T) {
			fixture := test.open(t)
			defer fixture.close()
			evidence := fixture.evidence
			configureRunnerLedgerRetryHandoffAncestor(t, evidence)
			rows := runnerLedgerRetryHandoffDatabaseRows(fixture.bundle)
			preflight := runnerLedgerRetryHandoffDatabaseSession(rows, 2)
			admission := runnerLedgerRetryHandoffDatabaseSession(rows, 8)
			fixture.runner.Connector = &runnerLedgerConsumerSequenceConnector{sessions: []*runnerPreflightSession{preflight, admission}}
			oldCursor := evidence.RecoverySnapshot().cursor.clone()

			_, err := fixture.runner.consumeRunnerLedgerPreflightStep(context.Background(), "test-only", fixture.bundle, fixture.plans, evidence, fixture.candidate)
			if !IsCode(err, CodeProjectionNotImplemented) {
				t.Fatalf("post-handoff Slice F boundary err=%v", err)
			}
			current := evidence.CurrentCandidate()
			active := evidence.ActiveGeneration()
			snapshot := evidence.RecoverySnapshot()
			if evidence.retryHandoffBindCalls != 1 || oldCursor.Valid() || !validOwnedCurrentCandidate(current) ||
				active.kind != activeGenerationCurrent || active.recoveryExecutionBindings != nil ||
				active.identity.runnerProjectionDecisionDigest != current.verifiedRun.runnerProjectionDecisionDigest ||
				active.identity.schemaBundleDigest != current.verifiedRun.schemaBundleDigest || snapshot == nil || !snapshot.cursor.Valid() ||
				snapshot.state != RecoveryBrandNewInherited || snapshot.nextPermittedAction != RecoveryBeginNextAttempt ||
				snapshot.attemptIndex == nil || *snapshot.attemptIndex != 2 || snapshot.previousAttemptTerminalDigest == nil ||
				preflight.beginCalls != 0 || preflight.backend.executeCalls != 0 || preflight.backend.ledgerInsertCalls != 0 ||
				admission.beginCalls != 0 || admission.backend.executeCalls != 0 || admission.backend.ledgerInsertCalls != 0 ||
				!preflight.closed || !admission.closed || preflight.unlockCalls != 1 || admission.unlockCalls != 1 {
				t.Fatalf("handoff result active=%+v snapshot=%+v preflight=%+v admission=%+v", active, snapshot, preflight, admission)
			}
		})
	}
}

func TestRunnerLedgerRetryHandoffCloseUncertaintyMintsNoSuccessor(t *testing.T) {
	fixture := runnerLedgerRetryHandoffOutcomeCases()[0].open(t)
	defer fixture.close()
	configureRunnerLedgerRetryHandoffAncestor(t, fixture.evidence)
	rows := runnerLedgerRetryHandoffDatabaseRows(fixture.bundle)
	preflight := runnerLedgerRetryHandoffDatabaseSession(rows, 2)
	admission := runnerLedgerRetryHandoffDatabaseSession(rows, 8)
	admission.closeErr = errors.New("secret-retry-handoff-close")
	fixture.runner.Connector = &runnerLedgerConsumerSequenceConnector{sessions: []*runnerPreflightSession{preflight, admission}}
	oldCursor := fixture.evidence.RecoverySnapshot().cursor.clone()

	_, err := fixture.runner.consumeRunnerLedgerPreflightStep(
		context.Background(), "test-only", fixture.bundle, fixture.plans, fixture.evidence, fixture.candidate,
	)
	if !IsCode(err, CodeTransactionBoundary) || fixture.evidence.retryHandoffBindCalls != 0 || !oldCursor.Valid() ||
		!preflight.closed || !admission.closed || preflight.beginCalls != 0 || admission.beginCalls != 0 ||
		preflight.backend.executeCalls != 0 || admission.backend.executeCalls != 0 ||
		preflight.backend.ledgerInsertCalls != 0 || admission.backend.ledgerInsertCalls != 0 {
		t.Fatalf("err=%v old-valid=%t preflight=%+v admission=%+v", err, oldCursor.Valid(), preflight, admission)
	}
}

func TestRunnerLedgerRetryHandoffPermitRejectsLiteralCopyAndSecondUse(t *testing.T) {
	if _, err := mintRunnerLedgerRetryHandoffPermit(runnerLedgerReconciliationAdmissionSeed{}, nil, runnerLedgerRetryHandoffBoundary{}); !IsCode(err, CodeEvidenceRecoveryRequired) {
		t.Fatalf("zero seed err=%v", err)
	}
	fixture := newRunnerLedgerRecoveryAbortTerminalFixture(t, RecoveryDanglingStatementIntent, RecoveryAppendAbortedRetryable)
	defer fixture.close(t)
	if err := fixture.run(context.Background()); err != nil {
		t.Fatal(err)
	}
	evidence := fixture.success.execution.base.service.evidence
	configureRunnerLedgerRetryHandoffAncestor(t, evidence)
	base := fixture.success.execution.base.service.kernel.base
	runner := fixture.success.execution.base.service.kernel.runner
	rows := runnerLedgerRetryHandoffDatabaseRows(base.bundle)
	preflight := runnerLedgerRetryHandoffDatabaseSession(rows, 2)
	admission := runnerLedgerRetryHandoffDatabaseSession(rows, 8)
	runner.Connector = &runnerLedgerConsumerSequenceConnector{sessions: []*runnerPreflightSession{preflight, admission}}

	claim, err := runner.prepareRunnerLedgerPreflightClaim(context.Background(), "test-only", base.bundle, base.plans, evidence, base.candidate)
	if err != nil {
		t.Fatal(err)
	}
	dispatch, err := runner.claimRunnerLedgerPreflightDispatch(context.Background(), evidence, base.candidate, claim)
	if err != nil {
		t.Fatal(err)
	}
	fact, err := bindRunnerLedgerConsumerFact(generatedRunnerLedgerConsumerProfile, dispatch, base.bundle.Manifest.ManifestDigest)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := runner.prepareRunnerLedgerRecoveryAdmission(context.Background(), "test-only", base.bundle, base.plans, evidence, base.candidate, fact)
	if err != nil {
		t.Fatal(err)
	}
	seed, err := claimRunnerLedgerRetryHandoffAdmissionPermit(owner.(*runnerLedgerRetryHandoffAdmissionPermit))
	if err != nil {
		t.Fatal(err)
	}
	receipt, boundary, err := runner.revalidateAndCloseRunnerLedgerRetryHandoffAdmission(context.Background(), seed, base.bundle)
	if err != nil {
		t.Fatal(err)
	}
	permit, err := mintRunnerLedgerRetryHandoffPermit(seed, receipt, boundary)
	if err != nil || !validRunnerLedgerRetryHandoffPermit(permit) || !admission.closed {
		t.Fatalf("permit=%+v admission=%+v err=%v", permit, admission, err)
	}
	invalidDatabase := *receipt
	invalidDatabase.database.postgresMajor = 14
	if runnerLedgerRetryHandoffReceiptDigest(&invalidDatabase) != ([32]byte{}) {
		t.Fatal("unsupported PostgreSQL major retained a closed retry-handoff receipt")
	}
	copyValue := *permit
	if _, copyErr := consumeRunnerLedgerRetryHandoffPermit(&copyValue, evidence); !IsCode(copyErr, CodeEvidenceRecoveryRequired) || !validRunnerLedgerRetryHandoffPermit(permit) {
		t.Fatalf("copy err=%v original-valid=%t", copyErr, validRunnerLedgerRetryHandoffPermit(permit))
	}
	if _, literalErr := consumeRunnerLedgerRetryHandoffPermit(&runnerLedgerRetryHandoffPermit{}, evidence); !IsCode(literalErr, CodeEvidenceRecoveryRequired) {
		t.Fatalf("literal err=%v", literalErr)
	}
	oldCursor := permit.cursor.clone()
	active, snapshot, err := evidence.bindRunnerLedgerRetryHandoff(context.Background(), permit)
	if err != nil || !runnerLedgerRetryHandoffResultMatches(evidence, active, snapshot, seed.candidateBinding, seed.generation, oldCursor, boundary) {
		t.Fatalf("transition active=%+v snapshot=%+v err=%v", active, snapshot, err)
	}
	if _, secondErr := consumeRunnerLedgerRetryHandoffPermit(permit, evidence); !IsCode(secondErr, CodeEvidenceRecoveryRequired) {
		t.Fatalf("second consume err=%v", secondErr)
	}
}

func TestRunnerLedgerRetryHandoffUnknownOrContradictoryResultRevokesOldCursor(t *testing.T) {
	tests := []struct {
		name       string
		configure  func(*runnerLedgerPreflightEvidenceFake)
		wantCode   ErrorCode
		wantActive bool
	}{
		{
			name: "binder-unknown", wantCode: CodeEvidenceJournalFailed,
			configure: func(evidence *runnerLedgerPreflightEvidenceFake) {
				evidence.retryHandoffBindErr = errors.New("secret-successor-outcome")
			},
		},
		{
			name: "successor-identity-contradiction", wantCode: CodeEvidenceRecoveryRequired, wantActive: true,
			configure: func(evidence *runnerLedgerPreflightEvidenceFake) {
				evidence.mutateRetryHandoffResult = func(_ *ActiveGeneration, snapshot *RecoverySnapshot) {
					*snapshot.attemptIndex++
				}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newRunnerLedgerRecoveryAbortTerminalFixture(t, RecoveryDanglingStatementIntent, RecoveryAppendAbortedRetryable)
			defer fixture.close(t)
			if err := fixture.run(context.Background()); err != nil {
				t.Fatal(err)
			}
			evidence := fixture.success.execution.base.service.evidence
			configureRunnerLedgerRetryHandoffAncestor(t, evidence)
			test.configure(evidence)
			base := fixture.success.execution.base.service.kernel.base
			runner := fixture.success.execution.base.service.kernel.runner
			rows := runnerLedgerRetryHandoffDatabaseRows(base.bundle)
			preflight := runnerLedgerRetryHandoffDatabaseSession(rows, 2)
			admission := runnerLedgerRetryHandoffDatabaseSession(rows, 8)
			runner.Connector = &runnerLedgerConsumerSequenceConnector{sessions: []*runnerPreflightSession{preflight, admission}}
			oldCursor := evidence.RecoverySnapshot().cursor.clone()

			_, err := runner.consumeRunnerLedgerPreflightStep(context.Background(), "test-only", base.bundle, base.plans, evidence, base.candidate)
			if !IsCode(err, test.wantCode) || oldCursor.Valid() || evidence.retryHandoffBindCalls != 1 ||
				!preflight.closed || !admission.closed || preflight.beginCalls != 0 || admission.beginCalls != 0 ||
				preflight.backend.executeCalls != 0 || admission.backend.executeCalls != 0 ||
				preflight.backend.ledgerInsertCalls != 0 || admission.backend.ledgerInsertCalls != 0 {
				t.Fatalf("err=%v old-valid=%t preflight=%+v admission=%+v", err, oldCursor.Valid(), preflight, admission)
			}
			current := evidence.RecoverySnapshot()
			if test.wantActive && (current == nil || current.cursor.Valid()) {
				t.Fatalf("contradictory successor cursor remained valid: %+v", current)
			}
		})
	}
}

func runnerLedgerRetryHandoffSelectionFixture(t *testing.T, evidence *runnerLedgerPreflightEvidenceFake, snapshot *RecoverySnapshot) runnerLedgerRecoveryAdmissionSelection {
	t.Helper()
	if evidence == nil || snapshot == nil || snapshot.migrationID == nil || snapshot.attemptIndex == nil {
		t.Fatal("retry-handoff snapshot identity is unavailable")
	}
	entryIndex := -1
	for index, migration := range evidence.schema.orderedMigrations {
		if migration == *snapshot.migrationID {
			entryIndex = index
			break
		}
	}
	if entryIndex < 0 {
		t.Fatalf("migration %s is absent from schema", *snapshot.migrationID)
	}
	entry, err := runnerLedgerPreflightNextEntryFromSchema(evidence.schema, entryIndex)
	if err != nil {
		t.Fatal(err)
	}
	return runnerLedgerRecoveryAdmissionSelection{
		action: generatedRunnerLedgerRecoveryProfiles[4].action, recoveryState: RecoveryTerminal,
		recoveryAction: RecoveryBeginNextAttempt, profileIndex: 4, entryIndex: uint32(entryIndex),
		migrationID: *snapshot.migrationID, entryDigest: entry.EntryDigest, attemptIndex: *snapshot.attemptIndex,
		maxAttempts: evidence.schema.maxAttempts[*snapshot.migrationID], planCount: 1, planDigest: [32]byte{1},
	}
}

func configureRunnerLedgerRetryHandoffAncestor(t *testing.T, evidence *runnerLedgerPreflightEvidenceFake) {
	t.Helper()
	// The retry handoff consumes an ancestor-recovery session opened after the
	// prior terminal append. The test reuses the in-process fake pointer, so it
	// explicitly retires the prior session's one-shot admission records before
	// installing the reopened ancestor boundary.
	revokeRunnerLedgerRecoveryAdmissionClaims(evidence)
	evidence.mu.Lock()
	defer evidence.mu.Unlock()
	base := evidence.runnerEvidenceSessionFake
	if base == nil || base.snapshot == nil || base.snapshot.state != RecoveryTerminal ||
		base.snapshot.nextPermittedAction != RecoveryBeginNextAttempt || base.snapshot.lastTerminal == nil {
		t.Fatal("retryable terminal fixture is unavailable")
	}
	candidate := base.candidate
	bindings, err := candidate.verifiedRun.currentDecision.decision.runnerProjectionBindings()
	if err != nil {
		t.Fatal(err)
	}
	old := base.active.identity
	old.runnerProjectionDecisionDigest = testDigest("runner-ledger-retry-handoff-old-decision")
	recovery := cloneRecoverySnapshot(base.snapshot)
	recovery.owner = old.owner
	recovery.generation = old
	recovery.cursor.owner = old.owner
	recovery.cursor.generation = old
	rebindRunnerLedgerRetryHandoffRecovered(recovery.lineageContinuation, old, recovery.cursor, recovery.tailDigest)
	rebindRunnerLedgerRetryHandoffRecovered(recovery.lastStatementIntent, old, recovery.cursor, recovery.tailDigest)
	rebindRunnerLedgerRetryHandoffRecovered(recovery.lastIntermediateEvidence, old, recovery.cursor, recovery.tailDigest)
	rebindRunnerLedgerRetryHandoffRecovered(recovery.commitIntent, old, recovery.cursor, recovery.tailDigest)
	rebindRunnerLedgerRetryHandoffRecovered(recovery.lastTerminal, old, recovery.cursor, recovery.tailDigest)
	rebindRunnerLedgerRetryHandoffRecovered(recovery.lastResolution, old, recovery.cursor, recovery.tailDigest)
	if !validRecoverySnapshotForJournal(recovery, old, recovery.cursor) {
		t.Fatal("rebound ancestor recovery snapshot is invalid")
	}
	continuation := lineageContinuationIdentity{
		StartAction: "begin_next_attempt", MigrationID: *recovery.migrationID,
		AttemptIndex: *recovery.attemptIndex + 1, PreviousAttempt: "owned_old_terminal",
	}
	selection := runnerLedgerRetryHandoffSelectionFixture(t, evidence, recovery)
	boundary, ok := runnerLedgerRetryHandoffBoundaryFromSnapshot(
		recovery, old, generationJournalRecoveryDigest(recovery), recovery.tailDigest, selection,
	)
	if !ok {
		t.Fatal("reopened ancestor does not expose an exact retry-handoff boundary")
	}
	policy := historicalRecoveryPolicySubject{
		RecoveryPolicySubjectDigest: bindings.recoveryPolicySubjectDigest, ExecutionLineageDigest: old.executionLineageDigest,
		OldJournalIdentityDigest: old.journalIdentityDigest, OldRunnerProjectionDecisionDigest: old.runnerProjectionDecisionDigest,
		OldSchemaBundleDigest: old.schemaBundleDigest, OldDecisionRecoveryArtifactSHA256: testDigest("runner-ledger-retry-handoff-old-recovery"),
		OldDecisionRecoveryArtifactSizeBytes:    32,
		SuccessorRunnerProjectionDecisionDigest: candidate.verifiedRun.runnerProjectionDecisionDigest,
		SuccessorSchemaBundleDigest:             candidate.verifiedRun.schemaBundleDigest,
		AllowedOutcomes:                         []string{boundary.outcome}, OutcomeConstraints: []historicalOutcomeConstraint{{
			Outcome: boundary.outcome, Continuation: historicalOutcomeContinuation{Kind: "exact_identity", Identity: &continuation},
		}},
	}
	policyDigest, err := policy.ComputeDigest()
	if err != nil {
		t.Fatal(err)
	}
	executionSubject := recoveryExecutionBindingsSubject{
		HistoricalRecoveryPolicyDigest: policyDigest, ExecutionLineageDigest: old.executionLineageDigest,
		CurrentRunnerProjectionDecisionDigest: candidate.verifiedRun.runnerProjectionDecisionDigest,
		OldRunnerProjectionDecisionDigest:     old.runnerProjectionDecisionDigest, OldJournalIdentityDigest: old.journalIdentityDigest,
		OldSchemaBundleDigest: old.schemaBundleDigest, OldDecisionRecoveryArtifactSHA256: policy.OldDecisionRecoveryArtifactSHA256,
		OldDecisionRecoveryArtifactSizeBytes: policy.OldDecisionRecoveryArtifactSizeBytes,
		OldJournalReplayTailDigest:           recovery.tailDigest, OldRecoveryState: string(recovery.state), ActionsProfile: oldAttemptRecoveryActionsProfile,
	}
	executionDigest, err := executionSubject.ComputeDigest()
	if err != nil {
		t.Fatal(err)
	}
	execution := &VerifiedRecoveryExecutionBindings{
		owner: candidate.verifiedRun.currentDecision.owner, session: candidate.owner, generation: old,
		tailDigest: recovery.tailDigest, snapshot: cloneRecoverySnapshot(recovery), policy: policy,
		subject: executionSubject, digest: executionDigest,
	}
	oldDecision := candidate.verifiedRun.currentDecision
	oldDecision.digest = old.runnerProjectionDecisionDigest
	base.active = ActiveGeneration{
		identity: old, kind: activeGenerationAncestorRecovery, journal: base.journal,
		ownedDecision: oldDecision, recoveryExecutionBindings: execution,
	}
	base.snapshot = recovery
	base.journal.cursor = recovery.cursor.clone()
	base.journal.snapshot = recovery
	evidence.recovery = cloneRecoverySnapshot(recovery)
	evidence.schema.generation = old
	if !runnerLedgerRetryHandoffActiveMatches(base.active, candidate, recovery) {
		t.Fatal("ancestor recovery fixture does not satisfy retry-handoff binding")
	}
}

func rebindRunnerLedgerRetryHandoffRecovered[T any](value *OwnedRecovered[T], generation generationIdentity, cursor JournalCursor, tail Digest) {
	if value == nil {
		return
	}
	value.owner = generation.owner
	value.generation = generation
	value.cursor = cursor.clone()
	value.tailDigest = tail
}

func runnerLedgerRetryHandoffDatabaseRows(bundle *RuntimeBundle) []LedgerRow {
	return []LedgerRow{ledgerRowFor(bundle.Manifest.SchemaBundle.Migrations[0], bundle.Manifest.SchemaBundleDigest)}
}

func prepareRunnerLedgerRetryHandoffFactFixture(t *testing.T, fixture runnerLedgerRetryHandoffFixtureView) (runnerLedgerConsumerFact, *runnerPreflightSession) {
	t.Helper()
	rows := runnerLedgerRetryHandoffDatabaseRows(fixture.bundle)
	preflight := runnerLedgerRetryHandoffDatabaseSession(rows, 2)
	fixture.runner.Connector = &runnerPreflightConnector{session: preflight}
	claim, err := fixture.runner.prepareRunnerLedgerPreflightClaim(
		context.Background(), "test-only", fixture.bundle, fixture.plans, fixture.evidence, fixture.candidate,
	)
	if err != nil {
		t.Fatal(err)
	}
	defer revokeRunnerLedgerPreflightClaim(claim)
	dispatch, err := fixture.runner.claimRunnerLedgerPreflightDispatch(context.Background(), fixture.evidence, fixture.candidate, claim)
	if err != nil {
		t.Fatal(err)
	}
	fact, err := bindRunnerLedgerConsumerFact(generatedRunnerLedgerConsumerProfile, dispatch, fixture.bundle.Manifest.ManifestDigest)
	if err != nil || fact.action != runnerLedgerConsumerRecoveryNotImplemented ||
		fact.dispatch.fact.recovery == nil || fact.dispatch.fact.recovery.State != RecoveryTerminal ||
		fact.dispatch.fact.recovery.Action != RecoveryBeginNextAttempt {
		t.Fatalf("retry-handoff fact=%+v err=%v", fact, err)
	}
	return fact, preflight
}

func runnerLedgerRetryHandoffDatabaseSession(rows []LedgerRow, reads int) *runnerPreflightSession {
	session := newRunnerPreflightSession()
	for index := 0; index < reads; index++ {
		session.ledgerRowsByRead = append(session.ledgerRowsByRead, cloneProjectionValue(rows))
	}
	return session
}
