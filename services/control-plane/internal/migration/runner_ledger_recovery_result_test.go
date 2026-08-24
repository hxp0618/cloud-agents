package migration

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func runnerLedgerReturnFailureTestSessions(bundle *RuntimeBundle, evidence *runnerLedgerPreflightEvidenceFake) (*runnerLedgerConsumerSequenceConnector, *runnerPreflightSession, *runnerPreflightSession) {
	rows := runnerLedgerConsumerPrefixRows(bundle, len(evidence.schema.durableObservedLedgerPrefix))
	preflight := newRunnerPreflightSession()
	preflight.ledgerRowsByRead = [][]LedgerRow{cloneProjectionValue(rows), cloneProjectionValue(rows)}
	admission := newRunnerPreflightSession()
	for index := 0; index < 6; index++ {
		admission.ledgerRowsByRead = append(admission.ledgerRowsByRead, cloneProjectionValue(rows))
	}
	return &runnerLedgerConsumerSequenceConnector{sessions: []*runnerPreflightSession{preflight, admission}}, preflight, admission
}

func assertRunnerLedgerTypedFailure(t *testing.T, err error, terminal AttemptTerminalState) {
	t.Helper()
	var projection *ProjectionError
	if !errors.As(err, &projection) || terminal.FailureEvidence == nil || terminal.StableErrorCode == nil {
		t.Fatalf("typed failure err=%#v terminal=%+v", err, terminal)
	}
	wantMajor := uint16(0)
	if terminal.FailureEvidence.Major != nil {
		wantMajor = *terminal.FailureEvidence.Major
	}
	if projection.Code != ErrorCode(*terminal.StableErrorCode) || projection.Phase != terminal.FailureEvidence.Phase ||
		projection.Path != terminal.FailureEvidence.Path || projection.PostgresMajor != wantMajor ||
		projection.Retryable != terminal.FailureEvidence.Retryable || projection.message == "" || projection.Unwrap() == nil {
		t.Fatalf("projection=%+v terminal=%+v", projection, terminal)
	}
	if containsErrorText(err, "secret-") {
		t.Fatalf("typed failure leaked raw text: %v", err)
	}
}

func refreshRunnerLedgerRecoveryTestFacts(evidence *runnerLedgerPreflightEvidenceFake) {
	fresh := evidence.RecoverySnapshot()
	evidence.mu.Lock()
	evidence.recovery = cloneRecoverySnapshot(fresh)
	evidence.mu.Unlock()
}

func prepareRunnerLedgerReturnFailureTestPermit(t *testing.T, fixture *runnerLedgerRecoveryAbortTerminalFixture) (*runnerLedgerReturnFailureAdmissionPermit, *runnerPreflightSession, *runnerPreflightSession) {
	t.Helper()
	evidence := fixture.success.execution.base.service.evidence
	base := fixture.success.execution.base.service.kernel.base
	sequence, preflight, admission := runnerLedgerReturnFailureTestSessions(base.bundle, evidence)
	runner := fixture.success.execution.base.service.kernel.runner
	runner.Connector = sequence
	claim, err := runner.prepareRunnerLedgerPreflightClaim(
		context.Background(), "test-only", base.bundle, base.plans, evidence, base.candidate,
	)
	if err != nil {
		t.Fatal(err)
	}
	defer revokeRunnerLedgerPreflightClaim(claim)
	dispatch, err := runner.claimRunnerLedgerPreflightDispatch(context.Background(), evidence, base.candidate, claim)
	if err != nil {
		t.Fatal(err)
	}
	fact, err := bindRunnerLedgerConsumerFact(generatedRunnerLedgerConsumerProfile, dispatch, base.bundle.Manifest.ManifestDigest)
	if err != nil || fact.action != runnerLedgerConsumerRecoveryNotImplemented {
		t.Fatalf("return-failure fact=%+v err=%v", fact, err)
	}
	owner, err := runner.prepareRunnerLedgerRecoveryAdmission(
		context.Background(), "test-only", base.bundle, base.plans, evidence, base.candidate, fact,
	)
	if err != nil {
		t.Fatal(err)
	}
	permit, ok := owner.(*runnerLedgerReturnFailureAdmissionPermit)
	if !ok || !validRunnerLedgerRecoveryAdmissionPermit(permit) {
		t.Fatalf("return-failure permit=%T valid=%t", owner, validRunnerLedgerRecoveryAdmissionPermit(owner))
	}
	return permit, preflight, admission
}

func TestRunnerLedgerReturnFailureUsesExactDurableTerminalWithoutMutation(t *testing.T) {
	fixture := newRunnerLedgerRecoveryAbortTerminalFixture(t, RecoveryDanglingStatementIntent, RecoveryAppendAbortedTerminal)
	defer fixture.close(t)
	if err := fixture.run(context.Background()); err != nil {
		t.Fatal(err)
	}
	evidence := fixture.success.execution.base.service.evidence
	refreshRunnerLedgerRecoveryTestFacts(evidence)
	terminal := evidence.RecoverySnapshot().lastTerminal.Value()
	journal := evidence.runnerEvidenceSessionFake.journal
	beforeAppends := journal.appendCalls
	base := fixture.success.execution.base.service.kernel.base
	sequence, preflight, admission := runnerLedgerReturnFailureTestSessions(base.bundle, evidence)
	runner := fixture.success.execution.base.service.kernel.runner
	runner.Connector = sequence
	step, err := runner.consumeRunnerLedgerPreflightStep(
		context.Background(), "test-only", base.bundle, base.plans, evidence, base.candidate,
	)
	assertRunnerLedgerTypedFailure(t, err, terminal)
	if !reflect.DeepEqual(step, runnerLedgerPreflightStep{}) || journal.appendCalls != beforeAppends || sequence.attempts != 2 ||
		!preflight.closed || !admission.closed || preflight.unlockCalls != 1 || admission.unlockCalls != 1 ||
		preflight.beginCalls != 0 || admission.beginCalls != 0 || preflight.backend.executeCalls != 0 ||
		admission.backend.executeCalls != 0 || preflight.backend.ledgerInsertCalls != 0 || admission.backend.ledgerInsertCalls != 0 {
		t.Fatalf("return failure escaped no-op boundary: step=%+v sequence=%+v preflight=%+v admission=%+v journal=%+v", step, sequence, preflight, admission, journal)
	}
}

func TestRunnerLedgerReturnFailureUsesExactDivergentResolutionWithoutMutation(t *testing.T) {
	fixture := newRunnerLedgerRecoveryReconciliationFixture(t, RecoveryAmbiguousUnresolved, runnerLedgerReconciliationDivergent, 16)
	defer fixture.close(t)
	if err := fixture.run(context.Background()); err != nil {
		t.Fatal(err)
	}
	fixture.success.execution.base.service.kernel.factory.mutateCatalog = nil
	evidence := fixture.success.execution.base.service.evidence
	refreshRunnerLedgerRecoveryTestFacts(evidence)
	snapshot := evidence.RecoverySnapshot()
	if snapshot == nil || snapshot.state != RecoveryDivergent || snapshot.lastResolution == nil {
		t.Fatalf("divergent snapshot=%+v", snapshot)
	}
	terminal := snapshot.lastTerminal.Value()
	journal := evidence.runnerEvidenceSessionFake.journal
	beforeAppends := journal.appendCalls
	base := fixture.success.execution.base.service.kernel.base
	sequence, preflight, admission := runnerLedgerReturnFailureTestSessions(base.bundle, evidence)
	runner := fixture.success.execution.base.service.kernel.runner
	runner.Connector = sequence
	step, err := runner.consumeRunnerLedgerPreflightStep(
		context.Background(), "test-only", base.bundle, base.plans, evidence, base.candidate,
	)
	assertRunnerLedgerTypedFailure(t, err, terminal)
	if !reflect.DeepEqual(step, runnerLedgerPreflightStep{}) || journal.appendCalls != beforeAppends || sequence.attempts != 2 ||
		!preflight.closed || !admission.closed || preflight.beginCalls != 0 || admission.beginCalls != 0 ||
		preflight.backend.executeCalls != 0 || admission.backend.executeCalls != 0 ||
		preflight.backend.ledgerInsertCalls != 0 || admission.backend.ledgerInsertCalls != 0 {
		t.Fatalf("divergent failure escaped no-op boundary: step=%+v sequence=%+v preflight=%+v admission=%+v journal=%+v", step, sequence, preflight, admission, journal)
	}
}

func TestRunnerLedgerReturnFailureCleanupUncertaintyDominatesTypedFailure(t *testing.T) {
	fixture := newRunnerLedgerRecoveryAbortTerminalFixture(t, RecoveryDanglingIntermediate, RecoveryAppendAbortedTerminal)
	defer fixture.close(t)
	if err := fixture.run(context.Background()); err != nil {
		t.Fatal(err)
	}
	evidence := fixture.success.execution.base.service.evidence
	refreshRunnerLedgerRecoveryTestFacts(evidence)
	base := fixture.success.execution.base.service.kernel.base
	sequence, _, admission := runnerLedgerReturnFailureTestSessions(base.bundle, evidence)
	admission.closeErr = errors.New("secret-close-uncertain")
	runner := fixture.success.execution.base.service.kernel.runner
	runner.Connector = sequence
	step, err := runner.consumeRunnerLedgerPreflightStep(
		context.Background(), "test-only", base.bundle, base.plans, evidence, base.candidate,
	)
	if !reflect.DeepEqual(step, runnerLedgerPreflightStep{}) || !IsCode(err, CodeTransactionBoundary) ||
		containsErrorText(err, "secret-") || admission.closeCalls != 1 {
		t.Fatalf("step=%+v err=%v admission=%+v", step, err, admission)
	}
	admission.closeErr = nil
}

func TestRunnerLedgerRecoveryLoopReentersFreshPreflightBeforeReturningFailure(t *testing.T) {
	fixture := newRunnerLedgerRecoveryAbortTerminalFixture(t, RecoveryDanglingStatementIntent, RecoveryAppendAbortedTerminal)
	defer fixture.close(t)
	evidence := fixture.success.execution.base.service.evidence
	base := fixture.success.execution.base.service.kernel.base
	rows := runnerLedgerConsumerPrefixRows(base.bundle, 1)
	newPreflight := func(reads int) *runnerPreflightSession {
		session := newRunnerPreflightSession()
		for index := 0; index < reads; index++ {
			session.ledgerRowsByRead = append(session.ledgerRowsByRead, cloneProjectionValue(rows))
		}
		return session
	}
	preflightAbort := newPreflight(2)
	admissionAbort := fixture.database
	preflightFailure := newPreflight(2)
	admissionFailure := newPreflight(6)
	sequence := &runnerLedgerConsumerSequenceConnector{sessions: []*runnerPreflightSession{
		preflightAbort, admissionAbort, preflightFailure, admissionFailure,
	}}
	evidence.runnerEvidenceSessionFake.journal.mutateAppendSnapshot = func(snapshot *RecoverySnapshot) {
		evidence.mu.Lock()
		evidence.recovery = cloneRecoverySnapshot(snapshot)
		evidence.mu.Unlock()
	}
	runner := fixture.success.execution.base.service.kernel.runner
	runner.Connector = sequence
	result, err := runner.consumeRunnerLedgerPreflight(
		context.Background(), "test-only", base.bundle, base.plans, evidence, base.candidate,
	)
	terminal := evidence.RecoverySnapshot().lastTerminal.Value()
	assertRunnerLedgerTypedFailure(t, err, terminal)
	if !reflect.DeepEqual(result, RunResult{}) || sequence.attempts != 4 || evidence.bindCalls != 4 || evidence.consumeCalls != 4 ||
		evidence.recoveryBindCalls != 2 || evidence.recoveryConsumeCalls != 2 ||
		evidence.runnerEvidenceSessionFake.journal.appendCalls == 0 || !preflightAbort.closed || !admissionAbort.closed ||
		!preflightFailure.closed || !admissionFailure.closed {
		t.Fatalf("fresh recovery loop result=%+v err=%v sequence=%+v evidence=%+v", result, err, sequence, evidence)
	}
	if _, live := runnerLedgerRecoveryAdmissionUseByEvidenceBind.Load(evidence); live {
		t.Fatal("fresh recovery loop retained a consumed admission use")
	}
}

func TestRunnerLedgerRecoveryIterationLimitIsDerivedFromVerifiedPolicy(t *testing.T) {
	raw, decision := buildExactTwoMigrationAdmissionRuntime(t)
	bundle, err := LoadRuntimeBundle(raw, decision)
	if err != nil {
		t.Fatal(err)
	}
	limit, err := runnerLedgerRecoveryIterationLimit(len(bundle.Manifest.SchemaBundle.Migrations), bundle.Manifest.ExecutionPolicy)
	if err != nil || limit != 23 {
		t.Fatalf("limit=%d err=%v", limit, err)
	}
	invalid := cloneProjectionValue(bundle.Manifest.ExecutionPolicy)
	invalid.MaxAttempts = 0
	if limit, err = runnerLedgerRecoveryIterationLimit(2, invalid); limit != 0 || !IsCode(err, CodeEvidenceRecoveryRequired) {
		t.Fatalf("invalid policy limit=%d err=%v", limit, err)
	}
	overflowEntries := int((maxJSONInteger-1)/(bundle.Manifest.ExecutionPolicy.MaxAttempts*3+2) + 1)
	if limit, err = runnerLedgerRecoveryIterationLimit(overflowEntries, bundle.Manifest.ExecutionPolicy); limit != 0 || !IsCode(err, CodeEvidenceRecoveryRequired) {
		t.Fatalf("overflow limit=%d err=%v", limit, err)
	}
}

func TestRunnerLedgerRecoveryCommittedReconciliationReturnsAmbiguousOutcome(t *testing.T) {
	fixture := newRunnerLedgerRecoveryReconciliationFixture(t, RecoveryDanglingCommitIntent, runnerLedgerReconciliationExactCommitted, 16)
	defer fixture.close(t)
	base := fixture.success.execution.base.service.kernel.base
	rows := runnerLedgerReconciliationFixtureRows(base.bundle, runnerLedgerReconciliationExactCommitted)
	preflight := newRunnerPreflightSession()
	preflight.ledgerRowsByRead = [][]LedgerRow{cloneProjectionValue(rows), cloneProjectionValue(rows)}
	sequence := &runnerLedgerConsumerSequenceConnector{sessions: []*runnerPreflightSession{preflight, fixture.database}}
	runner := fixture.success.execution.base.service.kernel.runner
	runner.Connector = sequence
	journal := fixture.success.execution.base.service.evidence.runnerEvidenceSessionFake.journal
	beforeAppends := journal.appendCalls

	step, err := runner.consumeRunnerLedgerPreflightStep(
		context.Background(), "test-only", base.bundle, base.plans,
		fixture.success.execution.base.service.evidence, base.candidate,
	)
	entries := base.bundle.Manifest.SchemaBundle.Migrations
	if err != nil || step.kind != runnerLedgerPreflightStepEntryCommitted || !step.ambiguousRecovered ||
		step.prefixLength != 1 || step.nextEntryID != entries[1].ID ||
		step.outcome.state != runnerLedgerEntrySuccessEntryCommittedComplete ||
		step.outcome.migrationID != entries[1].ID || step.outcome.ledgerHead != entries[1].ID ||
		step.outcome.ledgerLength != 2 || sequence.attempts != 2 || journal.appendCalls != beforeAppends+1 ||
		fixture.beforeCursor.Valid() || !preflight.closed || !fixture.database.closed {
		t.Fatalf("committed reconciliation step=%+v err=%v sequence=%+v journal=%+v preflight=%+v admission=%+v", step, err, sequence, journal, preflight, fixture.database)
	}
}

func TestRunnerLedgerRecoveryCommittedOutcomeRequiresFreshCompletePreflight(t *testing.T) {
	fixture := newRunnerLedgerRecoveryReconciliationFixture(t, RecoveryDanglingCommitIntent, runnerLedgerReconciliationExactCommitted, 16)
	defer fixture.close(t)
	base := fixture.success.execution.base.service.kernel.base
	evidence := fixture.success.execution.base.service.evidence
	rows := runnerLedgerReconciliationFixtureRows(base.bundle, runnerLedgerReconciliationExactCommitted)
	newPreflight := func() *runnerPreflightSession {
		session := newRunnerPreflightSession()
		session.ledgerRowsByRead = [][]LedgerRow{cloneProjectionValue(rows), cloneProjectionValue(rows)}
		return session
	}
	preflightRecovery := newPreflight()
	preflightComplete := newPreflight()
	completePrefix := cloneProjectionValue(evidence.schema.signedExpectedLedgerRows)
	completeDigest, err := LedgerPrefixDigest(completePrefix)
	if err != nil {
		t.Fatal(err)
	}
	sequence := &runnerLedgerConsumerSequenceConnector{sessions: []*runnerPreflightSession{
		preflightRecovery, fixture.database, preflightComplete,
	}}
	evidence.runnerEvidenceSessionFake.journal.mutateAppendSnapshot = func(snapshot *RecoverySnapshot) {
		evidence.mu.Lock()
		evidence.recovery = cloneRecoverySnapshot(snapshot)
		evidence.schema.durableObservedLedgerPrefix = cloneProjectionValue(completePrefix)
		evidence.schema.durableObservedLedgerDigest = completeDigest
		evidence.mu.Unlock()
	}
	runner := fixture.success.execution.base.service.kernel.runner
	runner.Connector = sequence
	beforeAppends := evidence.runnerEvidenceSessionFake.journal.appendCalls
	result, err := runner.consumeRunnerLedgerPreflight(
		context.Background(), "test-only", base.bundle, base.plans, evidence, base.candidate,
	)
	entries := base.bundle.Manifest.SchemaBundle.Migrations
	if err != nil || result.SchemaBundleDigest != base.bundle.Manifest.SchemaBundleDigest ||
		result.ManifestDigest != base.bundle.Manifest.ManifestDigest || result.FinalHead != entries[1].ID ||
		len(result.Applied) != 0 || !reflect.DeepEqual(result.AmbiguousRecovered, []string{entries[1].ID}) ||
		sequence.attempts != 3 || evidence.bindCalls != 4 || evidence.consumeCalls != 4 ||
		evidence.recoveryBindCalls != 1 || evidence.recoveryConsumeCalls != 1 ||
		evidence.runnerEvidenceSessionFake.journal.appendCalls != beforeAppends+1 ||
		!preflightRecovery.closed || !fixture.database.closed || !preflightComplete.closed {
		t.Fatalf("fresh complete result=%+v err=%v sequence=%+v evidence=%+v", result, err, sequence, evidence)
	}
}

func TestRunnerLedgerReturnFailureResultCanonicalRejectsFieldMutation(t *testing.T) {
	fixture := newRunnerLedgerRecoveryAbortTerminalFixture(t, RecoveryDanglingStatementIntent, RecoveryAppendAbortedTerminal)
	defer fixture.close(t)
	if err := fixture.run(context.Background()); err != nil {
		t.Fatal(err)
	}
	evidence := fixture.success.execution.base.service.evidence
	refreshRunnerLedgerRecoveryTestFacts(evidence)
	permit, preflight, admission := prepareRunnerLedgerReturnFailureTestPermit(t, fixture)
	result, err := buildRunnerLedgerReturnFailureResult(context.Background(), permit, evidence)
	if err != nil || !result.valid() {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	mutations := []func(*runnerLedgerReturnFailureResult){
		func(value *runnerLedgerReturnFailureResult) { value.state = RecoveryDivergent },
		func(value *runnerLedgerReturnFailureResult) { value.migrationID = "999999" },
		func(value *runnerLedgerReturnFailureResult) { value.attemptIndex++ },
		func(value *runnerLedgerReturnFailureResult) { value.failure.Phase = "mutated" },
		func(value *runnerLedgerReturnFailureResult) { value.terminalOutcome = "mutated" },
		func(value *runnerLedgerReturnFailureResult) { value.terminalDigest = testDigest("mutated-terminal") },
		func(value *runnerLedgerReturnFailureResult) {
			value.terminalRecordDigest = testDigest("mutated-record")
		},
		func(value *runnerLedgerReturnFailureResult) {
			value.executionLineageDigest = testDigest("mutated-lineage")
		},
		func(value *runnerLedgerReturnFailureResult) { value.recoveryDigest[0] ^= 0xff },
		func(value *runnerLedgerReturnFailureResult) {
			value.consumerFactSubject = testDigest("mutated-subject")
		},
		func(value *runnerLedgerReturnFailureResult) { value.permitCanonical[0] ^= 0xff },
		func(value *runnerLedgerReturnFailureResult) { value.ledgerLength++ },
		func(value *runnerLedgerReturnFailureResult) { value.catalogDigest = testDigest("mutated-catalog") },
		func(value *runnerLedgerReturnFailureResult) { value.canonical[0] ^= 0xff },
	}
	for index, mutate := range mutations {
		copyValue := cloneProjectionValue(result)
		mutate(&copyValue)
		if copyValue.valid() {
			t.Fatalf("mutation %d retained canonical validity: %+v", index, copyValue)
		}
	}
	if err := permit.closeWithoutMutation(nil); err != nil || !preflight.closed || !admission.closed {
		t.Fatalf("close err=%v preflight=%+v admission=%+v", err, preflight, admission)
	}
}

func TestRunnerLedgerReturnFailureRejectsDurableTerminalDriftAfterPermit(t *testing.T) {
	fixture := newRunnerLedgerRecoveryAbortTerminalFixture(t, RecoveryDanglingIntermediate, RecoveryAppendAbortedTerminal)
	defer fixture.close(t)
	if err := fixture.run(context.Background()); err != nil {
		t.Fatal(err)
	}
	evidence := fixture.success.execution.base.service.evidence
	refreshRunnerLedgerRecoveryTestFacts(evidence)
	permit, _, admission := prepareRunnerLedgerReturnFailureTestPermit(t, fixture)
	evidence.mu.Lock()
	original := cloneRecoverySnapshot(evidence.runnerEvidenceSessionFake.snapshot)
	evidence.runnerEvidenceSessionFake.snapshot.lastTerminal.value.FailureEvidence.Phase = "mutated-after-permit"
	evidence.mu.Unlock()
	result, err := buildRunnerLedgerReturnFailureResult(context.Background(), permit, evidence)
	evidence.mu.Lock()
	evidence.runnerEvidenceSessionFake.snapshot = original
	evidence.mu.Unlock()
	if !reflect.DeepEqual(result, runnerLedgerReturnFailureResult{}) || !IsCode(err, CodeEvidenceRecoveryRequired) {
		t.Fatalf("drifted terminal result=%+v err=%v", result, err)
	}
	if err := permit.closeWithoutMutation(nil); err != nil || !admission.closed || admission.beginCalls != 0 ||
		admission.backend.executeCalls != 0 || admission.backend.ledgerInsertCalls != 0 {
		t.Fatalf("drift close err=%v admission=%+v", err, admission)
	}
}
