package migration

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

var _ runnerLedgerRecoverySuccessEvidenceBinder = (*generationEvidenceSession)(nil)
var _ runnerLedgerRecoverySuccessEvidenceBinder = (*runnerLedgerPreflightEvidenceFake)(nil)

func (*runnerLedgerPreflightEvidenceFake) runnerLedgerRecoverySuccessEvidenceBinderSealed() {}

func (evidence *runnerLedgerPreflightEvidenceFake) bindRunnerLedgerRecoverySuccessRecord(ctx context.Context, request *runnerLedgerRecoverySuccessEvidenceRequest) (EvidenceJournal, JournalCursor, *OwnedEvidenceRecord, error) {
	evidence.successBindCalls++
	claimed, err := consumeRunnerLedgerRecoverySuccessEvidenceRequest(request, evidence)
	if err != nil {
		return nil, JournalCursor{}, nil, err
	}
	if ctx == nil {
		return nil, JournalCursor{}, nil, fail(CodeEvidenceRecoveryRequired, "runner-ledger-recovery-success-test-bind", "test context is unavailable", nil)
	}
	if err := ctx.Err(); err != nil {
		return nil, JournalCursor{}, nil, err
	}
	if evidence.successBindErr != nil {
		return nil, JournalCursor{}, nil, evidence.successBindErr
	}
	if err := evidence.successBindErrAt[evidence.successBindCalls]; err != nil {
		return nil, JournalCursor{}, nil, err
	}
	base := evidence.runnerEvidenceSessionFake
	if base == nil || base.closed || base.journal == nil || base.snapshot == nil ||
		claimed.candidateBinding != base.candidate.binding || !sameGenerationIdentity(claimed.generation, base.active.identity) ||
		generationJournalRecoveryDigest(base.snapshot) != claimed.recoveryDigest ||
		!sameCursorIdentity(claimed.cursor, base.journal.cursor) {
		return nil, JournalCursor{}, nil, fail(CodeEvidenceRecoveryRequired, "runner-ledger-recovery-success-test-bind", "test evidence boundary changed", nil)
	}
	cursor := base.journal.cursor.clone()
	witness := runnerLedgerEntrySuccessFakeWitness{
		recordKind: admissionEvidenceRecordKind(claimed.record), generation: claimed.generation, cursor: cursor.clone(),
	}
	owned := &OwnedEvidenceRecord{
		wire: cloneEvidenceRecord(claimed.record), witness: witness,
		generation: claimed.generation, cursor: cursor.clone(), consumed: &atomic.Bool{},
	}
	if evidence.mutateSuccessAuthority != nil {
		evidence.mutateSuccessAuthority(&cursor, owned)
	}
	base.journal.maxAttempts = claimed.maxAttempts
	return base.journal, cursor, owned, nil
}

type runnerLedgerRecoverySuccessFixture struct {
	admission *runnerLedgerRecoveryAdmissionFixture
	plans     []StatementPlan
	attempt   uint32
	previous  *Digest
}

func newRunnerLedgerRecoverySuccessAdmissionFixture(t *testing.T, disposition runnerLedgerPreflightDisposition, action RecoveryAction) *runnerLedgerRecoveryAdmissionFixture {
	t.Helper()
	service := newRunnerLedgerPreflightServiceFixture(t)
	return newRunnerLedgerRecoverySuccessAdmissionFixtureFromService(t, service, disposition, action)
}

func newRunnerLedgerRecoverySuccessAdmissionFixtureFromService(t *testing.T, service *runnerLedgerPreflightServiceFixture, disposition runnerLedgerPreflightDisposition, action RecoveryAction) *runnerLedgerRecoveryAdmissionFixture {
	t.Helper()
	service.configure(t, disposition, RecoveryBrandNewInherited, action, 16)
	evidence := service.evidence
	evidence.mu.Lock()
	if action == RecoveryBeginNextAttempt {
		previous := cloneDigestPointer(evidence.recovery.previousAttemptTerminalDigest)
		continuation := LineageContinuationContext{
			StartAction: "begin_next_attempt", MigrationID: *evidence.recovery.migrationID,
			AttemptIndex: *evidence.recovery.attemptIndex, PreviousAttemptTerminalDigest: previous,
			SourceJournalIdentityDigest:  evidence.recovery.generation.journalIdentityDigest,
			SourceCheckpointRecordDigest: testDigest("runner-ledger-recovery-success-source-checkpoint"),
			SourceTerminalDigest:         *previous,
		}
		evidence.recovery.lineageContinuation = &OwnedRecovered[LineageContinuationContext]{
			owner: evidence.recovery.generation.owner, generation: evidence.recovery.generation,
			cursor: evidence.recovery.cursor.clone(), tailDigest: evidence.recovery.tailDigest,
			recordDigest: testDigest("runner-ledger-recovery-success-continuation"), value: continuation,
		}
	}
	evidence.mu.Unlock()
	base := service.kernel.base
	claim, err := service.kernel.runner.prepareRunnerLedgerPreflightClaim(
		context.Background(), "test-only", base.bundle, base.plans, evidence, base.candidate,
	)
	if err != nil {
		service.close(t)
		t.Fatal(err)
	}
	defer revokeRunnerLedgerPreflightClaim(claim)
	dispatch, err := service.kernel.runner.claimRunnerLedgerPreflightDispatch(context.Background(), evidence, base.candidate, claim)
	if err != nil {
		service.close(t)
		t.Fatal(err)
	}
	fact, err := bindRunnerLedgerConsumerFact(generatedRunnerLedgerConsumerProfile, dispatch, base.bundle.Manifest.ManifestDigest)
	if err != nil {
		service.close(t)
		t.Fatal(err)
	}
	if _, ok := generatedRunnerLedgerRecoveryAdmissionAction(disposition, RecoveryBrandNewInherited, action); !ok {
		service.close(t)
		t.Fatalf("pair %s/%s/%s has no generated recovery action", disposition, RecoveryBrandNewInherited, action)
	}
	rows := make([]LedgerRow, 0, fact.dispatch.fact.orderedMigrationPrefixLength)
	for index := uint32(0); index < fact.dispatch.fact.orderedMigrationPrefixLength; index++ {
		rows = append(rows, ledgerRowFor(base.bundle.Manifest.SchemaBundle.Migrations[index], base.bundle.Manifest.SchemaBundleDigest))
	}
	database := newRunnerPreflightSession()
	database.ledgerRowsByRead = [][]LedgerRow{
		cloneProjectionValue(rows), cloneProjectionValue(rows), cloneProjectionValue(rows), cloneProjectionValue(rows),
	}
	connector := &runnerPreflightConnector{session: database}
	service.kernel.runner.Connector = connector
	return &runnerLedgerRecoveryAdmissionFixture{service: service, fact: fact, database: database, connector: connector}
}

func newRunnerLedgerRecoverySuccessFixture(t *testing.T, disposition runnerLedgerPreflightDisposition, action RecoveryAction) *runnerLedgerRecoverySuccessFixture {
	t.Helper()
	fixture := newRunnerLedgerRecoverySuccessAdmissionFixture(t, disposition, action)
	return prepareRunnerLedgerRecoverySuccessFixture(t, fixture, action)
}

func newRunnerLedgerRecoverySuccessFixtureFromService(t *testing.T, service *runnerLedgerPreflightServiceFixture, disposition runnerLedgerPreflightDisposition, action RecoveryAction) *runnerLedgerRecoverySuccessFixture {
	t.Helper()
	fixture := newRunnerLedgerRecoverySuccessAdmissionFixtureFromService(t, service, disposition, action)
	return prepareRunnerLedgerRecoverySuccessFixture(t, fixture, action)
}

func prepareRunnerLedgerRecoverySuccessFixture(t *testing.T, fixture *runnerLedgerRecoveryAdmissionFixture, action RecoveryAction) *runnerLedgerRecoverySuccessFixture {
	t.Helper()
	evidence := fixture.service.evidence
	permit, err := fixture.prepare(context.Background())
	if err != nil {
		fixture.close(t)
		t.Fatal(err)
	}
	_, ok := permit.(*runnerLedgerRecoveryExecutionAdmissionPermit)
	core := runnerLedgerRecoveryPermitCore(permit)
	if !ok || core == nil || !validRunnerLedgerRecoveryAdmissionPermit(permit) ||
		core.selection.action != generatedRunnerLedgerRecoveryProfiles[5].action {
		fixture.close(t)
		t.Fatalf("recovery execution permit=%T core=%+v", permit, core)
	}
	snapshot := cloneRecoverySnapshot(evidence.recovery)
	evidence.runnerEvidenceSessionFake.snapshot = snapshot
	evidence.runnerEvidenceSessionFake.journal.snapshot = snapshot
	evidence.runnerEvidenceSessionFake.journal.cursor = snapshot.cursor.clone()
	base := fixture.service.kernel.base
	plans := make([]StatementPlan, 0, core.selection.planCount)
	for _, plan := range base.plans {
		if plan.MigrationID == core.selection.migrationID {
			plans = append(plans, plan)
		}
	}
	if uint32(len(plans)) != core.selection.planCount {
		fixture.close(t)
		t.Fatalf("recovery plans=%d want=%d", len(plans), core.selection.planCount)
	}
	factory := fixture.service.kernel.factory
	before := catalogStateForRunnerLedgerEntryPlan(t, fixture.service.evidence, plans[0], plans[0].ExpectedTransition.CatalogBefore)
	factory.transitionState = &before
	transaction := fixture.database.transaction
	rows := make([]LedgerRow, core.selection.entryIndex)
	for index := range rows {
		rows[index] = ledgerRowFor(base.bundle.Manifest.SchemaBundle.Migrations[index], base.bundle.Manifest.SchemaBundleDigest)
	}
	transaction.ledgerPrefix = cloneProjectionValue(rows)
	transaction.executeAllowed = true
	transaction.executeMutate = func([]byte) {
		index := transaction.executeCalls - 1
		if index < 0 || index >= len(plans) {
			t.Fatalf("unexpected recovery execute index %d", index)
		}
		after := catalogStateForRunnerLedgerEntryPlan(t, fixture.service.evidence, plans[index], plans[index].ExpectedTransition.CatalogAfter)
		factory.transitionState = &after
	}
	fixture.service.evidence.runnerEvidenceSessionFake.journal.bundleComplete =
		core.selection.entryIndex+1 == uint32(len(base.bundle.Manifest.SchemaBundle.Migrations))
	return &runnerLedgerRecoverySuccessFixture{
		admission: fixture, plans: plans, attempt: core.selection.attemptIndex,
		previous: cloneDigestPointer(snapshot.previousAttemptTerminalDigest),
	}
}

func (fixture *runnerLedgerRecoverySuccessFixture) close(t *testing.T) {
	t.Helper()
	if fixture != nil && fixture.admission != nil {
		fixture.admission.close(t)
	}
}

func TestRunnerLedgerRecoverySuccessExecutesInheritedFirstAndRetryAttempts(t *testing.T) {
	for _, test := range []struct {
		name        string
		disposition runnerLedgerPreflightDisposition
		action      RecoveryAction
	}{
		{"entry-retry", runnerLedgerPreflightEmptyBrandNew, RecoveryBeginNextAttempt},
		{"inherited-first", runnerLedgerPreflightPartialRetryOrRecovery, RecoveryBeginFirstAttempt},
		{"inherited-retry", runnerLedgerPreflightPartialRetryOrRecovery, RecoveryBeginNextAttempt},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newRunnerLedgerRecoverySuccessFixture(t, test.disposition, test.action)
			defer fixture.close(t)
			base := fixture.admission.service.kernel.base
			execution := fixture.admission.permit.(*runnerLedgerRecoveryExecutionAdmissionPermit)
			oldCursor := fixture.admission.service.evidence.RecoverySnapshot().cursor.clone()
			outcome, err := fixture.admission.service.kernel.runner.executeRunnerLedgerRecoverySuccess(
				context.Background(), execution, base.bundle, base.plans,
			)
			if err != nil || !outcome.valid() || oldCursor.Valid() {
				t.Fatalf("outcome=%+v err=%v old-valid=%t", outcome, err, oldCursor.Valid())
			}
			snapshot := fixture.admission.service.evidence.RecoverySnapshot()
			if snapshot == nil || snapshot.lastStatementIntent == nil || snapshot.commitIntent == nil || snapshot.lastTerminal == nil ||
				snapshot.attemptIndex == nil || *snapshot.attemptIndex != fixture.attempt ||
				snapshot.lastStatementIntent.value.AttemptIndex != fixture.attempt ||
				snapshot.commitIntent.value.AttemptIndex != fixture.attempt || snapshot.lastTerminal.value.AttemptIndex != fixture.attempt ||
				!equalDigestPointer(snapshot.lastStatementIntent.value.PreviousAttemptTerminalDigest, fixture.previous) ||
				!equalDigestPointer(snapshot.commitIntent.value.PreviousAttemptTerminalDigest, fixture.previous) ||
				!equalDigestPointer(snapshot.lastTerminal.value.PreviousAttemptTerminalDigest, fixture.previous) {
				t.Fatalf("attempt=%d previous=%v snapshot=%+v", fixture.attempt, fixture.previous, snapshot)
			}
			database := fixture.admission.database
			if database.beginCalls != 1 || database.transaction.executeCalls != len(fixture.plans) ||
				database.backend.ledgerInsertCalls != 1 || database.backend.commitCalls != 1 ||
				!database.closed || database.unlockCalls != 0 {
				t.Fatalf("recovery writer database=%+v transaction=%+v", database, database.transaction)
			}
		})
	}
}

func TestRunnerLedgerRecoverySuccessPreservesMultiStatementAttemptAndRotation(t *testing.T) {
	raw, decision := buildExactMultiStatementAdmissionRuntime(t)
	kernel := newRunnerLedgerCatalogPreflightFixtureFromRuntime(t, raw, decision)
	service := &runnerLedgerPreflightServiceFixture{kernel: kernel, evidence: newRunnerLedgerPreflightEvidenceFake(t, kernel.base)}
	fixture := newRunnerLedgerRecoverySuccessFixtureFromService(t, service, runnerLedgerPreflightEmptyBrandNew, RecoveryBeginNextAttempt)
	defer fixture.close(t)
	journal := service.evidence.runnerEvidenceSessionFake.journal
	journal.rotateAt = map[int]bool{2: true}
	base := service.kernel.base
	outcome, err := service.kernel.runner.executeRunnerLedgerRecoverySuccess(
		context.Background(), fixture.admission.permit.(*runnerLedgerRecoveryExecutionAdmissionPermit), base.bundle, base.plans,
	)
	snapshot := service.evidence.RecoverySnapshot()
	transaction := fixture.admission.database.transaction
	if err != nil || !outcome.valid() || len(fixture.plans) != 2 || transaction.executeCalls != 2 ||
		journal.appendCalls != 6 || snapshot == nil || snapshot.lastStatementIntent == nil ||
		snapshot.lastStatementIntent.value.StatementIndex != 1 || snapshot.lastStatementIntent.value.AttemptIndex != fixture.attempt ||
		!equalDigestPointer(snapshot.lastStatementIntent.value.PreviousAttemptTerminalDigest, fixture.previous) ||
		snapshot.cursor.segmentIndex == 0 {
		t.Fatalf("outcome=%+v err=%v plans=%d transaction=%+v appends=%d snapshot=%+v", outcome, err,
			len(fixture.plans), transaction, journal.appendCalls, snapshot)
	}
}

func TestRunnerLedgerRecoverySuccessConsumesFreshRetryHandoffSuccessorOnReentry(t *testing.T) {
	source := newRunnerLedgerRecoveryAbortTerminalFixture(t, RecoveryDanglingStatementIntent, RecoveryAppendAbortedRetryable)
	defer source.close(t)
	if err := source.run(context.Background()); err != nil {
		t.Fatal(err)
	}
	service := source.success.execution.base.service
	runner := service.kernel.runner
	evidence := service.evidence
	base := service.kernel.base
	configureRunnerLedgerRetryHandoffAncestor(t, evidence)
	rows := runnerLedgerRetryHandoffDatabaseRows(base.bundle)
	firstPreflight := runnerLedgerRetryHandoffDatabaseSession(rows, 2)
	handoffSession := runnerLedgerRetryHandoffDatabaseSession(rows, 8)
	runner.Connector = &runnerLedgerConsumerSequenceConnector{sessions: []*runnerPreflightSession{firstPreflight, handoffSession}}
	oldCursor := evidence.RecoverySnapshot().cursor.clone()
	if _, err := runner.consumeRunnerLedgerPreflightStep(
		context.Background(), "test-only", base.bundle, base.plans, evidence, base.candidate,
	); !IsCode(err, CodeProjectionNotImplemented) {
		t.Fatalf("handoff err=%v", err)
	}
	inherited := evidence.RecoverySnapshot()
	if inherited == nil || inherited.migrationID == nil || inherited.attemptIndex == nil ||
		inherited.state != RecoveryBrandNewInherited || inherited.nextPermittedAction != RecoveryBeginNextAttempt ||
		*inherited.attemptIndex != 2 || inherited.previousAttemptTerminalDigest == nil || oldCursor.Valid() {
		t.Fatalf("inherited=%+v old-valid=%t", inherited, oldCursor.Valid())
	}
	// A production re-entry opens a fresh generation evidence session. Mirror
	// that boundary here instead of reusing the already-consumed admission fact
	// registry attached to the ancestor session wrapper.
	revokeRunnerLedgerPreflightClaims(evidence)
	revokeRunnerLedgerEntryAdmissionClaims(evidence)
	revokeRunnerLedgerEntryExecutionAdmissionClaims(evidence)
	revokeRunnerLedgerRecoveryAdmissionClaims(evidence)
	freshEvidence := &runnerLedgerPreflightEvidenceFake{
		runnerEvidenceSessionFake: evidence.runnerEvidenceSessionFake,
		schema:                    cloneGenerationJournalSchema(evidence.schema),
		recovery:                  cloneRecoverySnapshot(evidence.recovery),
		sessionDigest:             digestRaw(testDigest("runner-ledger-recovery-success-reentry-session")),
		journalDigest:             digestRaw(testDigest("runner-ledger-recovery-success-reentry-journal")),
		successBindErrAt:          map[int]error{},
	}
	service.evidence = freshEvidence
	plans := make([]StatementPlan, 0, len(base.plans))
	for _, plan := range base.plans {
		if plan.MigrationID == *inherited.migrationID {
			plans = append(plans, plan)
		}
	}
	if len(plans) == 0 {
		t.Fatal("retry successor statement plans are unavailable")
	}
	factory := service.kernel.factory
	before := catalogStateForRunnerLedgerEntryPlan(t, freshEvidence, plans[0], plans[0].ExpectedTransition.CatalogBefore)
	factory.transitionState = &before
	secondPreflight := runnerLedgerRetryHandoffDatabaseSession(rows, 2)
	executionSession := runnerLedgerRetryHandoffDatabaseSession(rows, 4)
	executionSession.transaction.ledgerPrefix = cloneProjectionValue(rows)
	executionSession.transaction.executeAllowed = true
	executionSession.transaction.executeMutate = func([]byte) {
		index := executionSession.transaction.executeCalls - 1
		after := catalogStateForRunnerLedgerEntryPlan(t, freshEvidence, plans[index], plans[index].ExpectedTransition.CatalogAfter)
		factory.transitionState = &after
	}
	freshEvidence.runnerEvidenceSessionFake.journal.bundleComplete = true
	runner.Connector = &runnerLedgerConsumerSequenceConnector{sessions: []*runnerPreflightSession{secondPreflight, executionSession}}
	if _, err := runner.consumeRunnerLedgerPreflightStep(
		context.Background(), "test-only", base.bundle, base.plans, freshEvidence, base.candidate,
	); !IsCode(err, CodeProjectionNotImplemented) {
		t.Fatalf("recovery success re-entry err=%v", err)
	}
	completed := freshEvidence.RecoverySnapshot()
	if executionSession.beginCalls != 1 || executionSession.transaction.executeCalls != len(plans) ||
		executionSession.transaction.ledgerInsertCalls != 1 || executionSession.transaction.commitCalls != 1 ||
		!executionSession.closed || completed == nil || completed.state != RecoveryCompleted || completed.lastTerminal == nil ||
		completed.lastTerminal.value.AttemptIndex != 2 ||
		!equalDigestPointer(completed.lastTerminal.value.PreviousAttemptTerminalDigest, inherited.previousAttemptTerminalDigest) {
		t.Fatalf("execution=%+v transaction=%+v completed=%+v", executionSession, executionSession.transaction, completed)
	}
}

func TestRunnerLedgerRecoverySuccessRejectsCopyLiteralAndSecondUse(t *testing.T) {
	fixture := newRunnerLedgerRecoverySuccessFixture(t, runnerLedgerPreflightPartialRetryOrRecovery, RecoveryBeginNextAttempt)
	defer fixture.close(t)
	base := fixture.admission.service.kernel.base
	original := fixture.admission.permit.(*runnerLedgerRecoveryExecutionAdmissionPermit)
	copyValue := *original
	if _, err := fixture.admission.service.kernel.runner.prepareRunnerLedgerRecoverySuccess(context.Background(), &copyValue, base.bundle, base.plans); !IsCode(err, CodeTransactionBoundary) || !validRunnerLedgerRecoveryAdmissionPermit(original) {
		t.Fatalf("copy err=%v original-valid=%t", err, validRunnerLedgerRecoveryAdmissionPermit(original))
	}
	if _, err := fixture.admission.service.kernel.runner.prepareRunnerLedgerRecoverySuccess(context.Background(), &runnerLedgerRecoveryExecutionAdmissionPermit{}, base.bundle, base.plans); !IsCode(err, CodeTransactionBoundary) {
		t.Fatalf("literal err=%v", err)
	}
	state, err := fixture.admission.service.kernel.runner.prepareRunnerLedgerRecoverySuccess(context.Background(), original, base.bundle, base.plans)
	if err != nil || !validRunnerLedgerEntrySuccessState(state) || state.data.writerKind != runnerLedgerSuccessWriterRecoveryV1 {
		t.Fatalf("state=%+v err=%v", state, err)
	}
	defer func() { _ = closeRunnerLedgerEntrySuccessState(state, errors.New("test cleanup")) }()
	if _, err := fixture.admission.service.kernel.runner.prepareRunnerLedgerRecoverySuccess(context.Background(), original, base.bundle, base.plans); !IsCode(err, CodeEvidenceRecoveryRequired) {
		t.Fatalf("second use err=%v", err)
	}
}

func TestRunnerLedgerRecoverySuccessEvidenceRequestIsProfileSpecificAndOneShot(t *testing.T) {
	fixture := newRunnerLedgerRecoverySuccessFixture(t, runnerLedgerPreflightPartialRetryOrRecovery, RecoveryBeginNextAttempt)
	defer fixture.close(t)
	base := fixture.admission.service.kernel.base
	state, err := fixture.admission.service.kernel.runner.prepareRunnerLedgerRecoverySuccess(
		context.Background(), fixture.admission.permit.(*runnerLedgerRecoveryExecutionAdmissionPermit), base.bundle, base.plans,
	)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = closeRunnerLedgerEntrySuccessState(state, errors.New("test cleanup")) }()
	state, err = fixture.admission.service.kernel.runner.beginRunnerLedgerEntrySuccessTransaction(context.Background(), state)
	if err == nil {
		state, err = fixture.admission.service.kernel.runner.prepareRunnerLedgerEntrySuccessStatement(context.Background(), state)
	}
	if err != nil {
		t.Fatal(err)
	}
	binder := state.data.evidence.(runnerLedgerRecoverySuccessEvidenceBinder)
	request, err := mintRunnerLedgerRecoverySuccessEvidenceRequest(
		binder, state.data.candidateBinding, state.data.generation, state.data.recoveryDigest, state.data.cursor,
		EvidenceRecord{StatementIntent: cloneStatementIntentPointer(&state.data.intent)}, state.data.plans[0], state.data.maxAttempts,
	)
	if err != nil || !validRunnerLedgerRecoverySuccessEvidenceRequest(request, binder) {
		t.Fatalf("request=%+v err=%v", request, err)
	}
	copyValue := *request
	if _, err := consumeRunnerLedgerRecoverySuccessEvidenceRequest(&copyValue, binder); !IsCode(err, CodeEvidenceRecoveryRequired) ||
		!validRunnerLedgerRecoverySuccessEvidenceRequest(request, binder) {
		t.Fatalf("copy err=%v original-valid=%t", err, validRunnerLedgerRecoverySuccessEvidenceRequest(request, binder))
	}
	claim, err := consumeRunnerLedgerRecoverySuccessEvidenceRequest(request, binder)
	if err != nil || claim.record.StatementIntent == nil {
		t.Fatalf("claim=%+v err=%v", claim, err)
	}
	if _, err := consumeRunnerLedgerRecoverySuccessEvidenceRequest(request, binder); !IsCode(err, CodeEvidenceRecoveryRequired) {
		t.Fatalf("second consume err=%v", err)
	}
}

func TestRunnerLedgerRecoverySuccessUnknownAndPostCommitBoundariesRequireRecovery(t *testing.T) {
	for _, test := range []struct {
		name      string
		configure func(*runnerLedgerRecoverySuccessFixture)
		appends   int
		executes  int
		commits   int
		state     RecoveryState
	}{
		{
			name: "intent-unknown", appends: 1, state: RecoveryBrandNewInherited,
			configure: func(f *runnerLedgerRecoverySuccessFixture) {
				f.admission.service.evidence.runnerEvidenceSessionFake.journal.appendOutcomeAt = map[int]appendOutcome{1: appendOutcomeUnknown}
			},
		},
		{
			name: "commit-rejected", appends: 3, executes: 1, commits: 1, state: RecoveryDanglingCommitIntent,
			configure: func(f *runnerLedgerRecoverySuccessFixture) {
				f.admission.database.transaction.commitErr = testPGError("40001")
			},
		},
		{
			name: "commit-ambiguous", appends: 3, executes: 1, commits: 1, state: RecoveryDanglingCommitIntent,
			configure: func(f *runnerLedgerRecoverySuccessFixture) {
				f.admission.database.transaction.commitErr = context.DeadlineExceeded
			},
		},
		{
			name: "post-commit-close", appends: 3, executes: 1, commits: 1, state: RecoveryDanglingCommitIntent,
			configure: func(f *runnerLedgerRecoverySuccessFixture) {
				f.admission.database.closeErr = errors.New("post-commit close failed")
			},
		},
		{
			name: "terminal-unknown", appends: 4, executes: 1, commits: 1, state: RecoveryDanglingCommitIntent,
			configure: func(f *runnerLedgerRecoverySuccessFixture) {
				f.admission.service.evidence.runnerEvidenceSessionFake.journal.appendOutcomeAt = map[int]appendOutcome{4: appendOutcomeUnknown}
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newRunnerLedgerRecoverySuccessFixture(t, runnerLedgerPreflightPartialRetryOrRecovery, RecoveryBeginNextAttempt)
			defer fixture.close(t)
			test.configure(fixture)
			base := fixture.admission.service.kernel.base
			outcome, err := fixture.admission.service.kernel.runner.executeRunnerLedgerRecoverySuccess(
				context.Background(), fixture.admission.permit.(*runnerLedgerRecoveryExecutionAdmissionPermit), base.bundle, base.plans,
			)
			snapshot := fixture.admission.service.evidence.RecoverySnapshot()
			journal := fixture.admission.service.evidence.runnerEvidenceSessionFake.journal
			transaction := fixture.admission.database.transaction
			if outcome.valid() || !IsCode(err, CodeEvidenceRecoveryRequired) || journal.appendCalls != test.appends ||
				transaction.executeCalls != test.executes || transaction.commitCalls != test.commits ||
				snapshot == nil || snapshot.state != test.state || snapshot.cursor.Valid() {
				t.Fatalf("outcome=%+v err=%v appends=%d executes=%d commits=%d snapshot=%+v", outcome, err,
					journal.appendCalls, transaction.executeCalls, transaction.commitCalls, snapshot)
			}
		})
	}
}

func TestRunnerLedgerRecoverySuccessProductionGraphHasOnlyTypedAdmissionAndWriterEdges(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	recoveryFiles := map[string]bool{
		"evidence_runner_ledger_recovery_success.go": true,
		"runner_ledger_recovery_success_kernel.go":   true,
	}
	forbiddenImports := map[string]bool{"database/sql": true, "net/http": true}
	forbiddenCalls := map[string]bool{
		"Run": true, "executeRunnerLedgerEntrySuccess": true, "ReserveAndActivateSuccessor": true,
		"prepareRunnerLedgerRetryHandoff": true, "appendRunnerLedgerRecoveryAbortTerminal": true,
		"appendRunnerLedgerRecoveryCommitObservation": true, "appendRunnerLedgerRecoveryAmbiguousResolution": true,
	}
	callers := make([]string, 0, 1)
	requestMinters := 0
	binders := 0
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || filepath.Ext(name) != ".go" || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, parseErr := parser.ParseFile(token.NewFileSet(), name, nil, 0)
		if parseErr != nil {
			t.Fatalf("parse %s: %v", name, parseErr)
		}
		if recoveryFiles[name] {
			for _, imported := range file.Imports {
				path := strings.Trim(imported.Path.Value, `"`)
				if forbiddenImports[path] || strings.Contains(path, "provider") {
					t.Fatalf("%s imports forbidden package %q", name, path)
				}
			}
		}
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			called := ""
			switch function := call.Fun.(type) {
			case *ast.Ident:
				called = function.Name
			case *ast.SelectorExpr:
				called = function.Sel.Name
			}
			switch called {
			case "executeRunnerLedgerRecoverySuccess":
				callers = append(callers, name)
				if name != "runner_ledger_recovery_admission_permit.go" {
					t.Fatalf("%s connected an unreviewed recovery success caller", name)
				}
			case "mintRunnerLedgerRecoverySuccessEvidenceRequest":
				requestMinters++
				if name != "runner_ledger_entry_success_kernel.go" {
					t.Fatalf("%s minted recovery writer evidence outside the reviewed kernel", name)
				}
			case "bindRunnerLedgerRecoverySuccessRecord":
				binders++
				if name != "runner_ledger_entry_success_kernel.go" {
					t.Fatalf("%s called the recovery evidence binder outside the reviewed kernel", name)
				}
			}
			if recoveryFiles[name] && forbiddenCalls[called] {
				t.Fatalf("%s acquired forbidden call edge %s", name, called)
			}
			return true
		})
	}
	admission := generatedRunnerLedgerRecoveryProfiles[5]
	writer := generatedRunnerLedgerRecoveryProfiles[6]
	if len(callers) != 1 || callers[0] != "runner_ledger_recovery_admission_permit.go" || requestMinters != 1 || binders != 1 ||
		writer.pairCount != 0 || writer.predecessor != admission.registryBinding() || writer.permitFromProfileID != admission.profileID ||
		runnerLedgerRecoverySuccessEvidenceRequestDomain == runnerLedgerEntrySuccessEvidenceRequestDomain ||
		runnerLedgerRecoverySuccessStateDigestDomain == runnerLedgerEntrySuccessStateDigestDomain ||
		runnerLedgerRecoverySuccessCleanupDigestDomain == runnerLedgerEntrySuccessCleanupDigestDomain {
		t.Fatalf("recovery graph callers=%v minters=%d binders=%d admission=%+v writer=%+v", callers, requestMinters, binders, admission, writer)
	}
	if runnerLedgerEntrySuccessTransitionAllowed(runnerLedgerSuccessWriterEntryV1, "unclassified", "consume_recovery_execution_permit", runnerLedgerEntrySuccessExecutionReady) ||
		runnerLedgerEntrySuccessTransitionAllowed(runnerLedgerSuccessWriterRecoveryV1, "unclassified", "consume_execution_permit", runnerLedgerEntrySuccessExecutionReady) {
		t.Fatal("entry and recovery success transition identities are interchangeable")
	}
}
