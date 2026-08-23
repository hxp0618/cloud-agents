package migration

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

type runnerLedgerConsumerMatrixCase struct {
	name        string
	disposition runnerLedgerPreflightDisposition
	state       RecoveryState
	action      RecoveryAction
	wantAction  runnerLedgerConsumerAction
}

func runnerLedgerConsumerMatrixCases() []runnerLedgerConsumerMatrixCase {
	return []runnerLedgerConsumerMatrixCase{
		{"complete", runnerLedgerPreflightCompleteReturnSuccess, RecoveryCompleted, RecoveryReturnSuccess, runnerLedgerConsumerReturnSuccessNoop},
		{"empty-brand-new", runnerLedgerPreflightEmptyBrandNew, RecoveryBrandNew, RecoveryBeginFirstAttempt, runnerLedgerConsumerEntryNotImplemented},
		{"empty-inherited-first", runnerLedgerPreflightEmptyBrandNew, RecoveryBrandNewInherited, RecoveryBeginFirstAttempt, runnerLedgerConsumerEntryNotImplemented},
		{"empty-inherited-retry", runnerLedgerPreflightEmptyBrandNew, RecoveryBrandNewInherited, RecoveryBeginNextAttempt, runnerLedgerConsumerEntryNotImplemented},
		{"partial-next-inherited", runnerLedgerPreflightPartialNextEntry, RecoveryBrandNewInherited, RecoveryBeginFirstAttemptNextEntry, runnerLedgerConsumerEntryNotImplemented},
		{"partial-next-terminal", runnerLedgerPreflightPartialNextEntry, RecoveryTerminal, RecoveryBeginFirstAttemptNextEntry, runnerLedgerConsumerEntryNotImplemented},
		{"recovery-inherited-first", runnerLedgerPreflightPartialRetryOrRecovery, RecoveryBrandNewInherited, RecoveryBeginFirstAttempt, runnerLedgerConsumerRecoveryNotImplemented},
		{"recovery-inherited-retry", runnerLedgerPreflightPartialRetryOrRecovery, RecoveryBrandNewInherited, RecoveryBeginNextAttempt, runnerLedgerConsumerRecoveryNotImplemented},
		{"recovery-intent-retry", runnerLedgerPreflightPartialRetryOrRecovery, RecoveryDanglingStatementIntent, RecoveryAppendAbortedRetryable, runnerLedgerConsumerRecoveryNotImplemented},
		{"recovery-intent-terminal", runnerLedgerPreflightPartialRetryOrRecovery, RecoveryDanglingStatementIntent, RecoveryAppendAbortedTerminal, runnerLedgerConsumerRecoveryNotImplemented},
		{"recovery-intermediate-retry", runnerLedgerPreflightPartialRetryOrRecovery, RecoveryDanglingIntermediate, RecoveryAppendAbortedRetryable, runnerLedgerConsumerRecoveryNotImplemented},
		{"recovery-intermediate-terminal", runnerLedgerPreflightPartialRetryOrRecovery, RecoveryDanglingIntermediate, RecoveryAppendAbortedTerminal, runnerLedgerConsumerRecoveryNotImplemented},
		{"recovery-commit", runnerLedgerPreflightPartialRetryOrRecovery, RecoveryDanglingCommitIntent, RecoveryReconcileCommit, runnerLedgerConsumerRecoveryNotImplemented},
		{"recovery-ambiguous", runnerLedgerPreflightPartialRetryOrRecovery, RecoveryAmbiguousUnresolved, RecoveryReconcileCommit, runnerLedgerConsumerRecoveryNotImplemented},
		{"recovery-terminal-retry", runnerLedgerPreflightPartialRetryOrRecovery, RecoveryTerminal, RecoveryBeginNextAttempt, runnerLedgerConsumerRecoveryNotImplemented},
		{"recovery-terminal-failure", runnerLedgerPreflightPartialRetryOrRecovery, RecoveryTerminal, RecoveryReturnFailure, runnerLedgerConsumerRecoveryNotImplemented},
		{"recovery-divergent", runnerLedgerPreflightPartialRetryOrRecovery, RecoveryDivergent, RecoveryReturnFailure, runnerLedgerConsumerRecoveryNotImplemented},
	}
}

func TestRunnerLedgerConsumerServiceCoversGeneratedMatrix(t *testing.T) {
	cases := runnerLedgerConsumerMatrixCases()
	if len(cases) != generatedRunnerLedgerConsumerPairCount {
		t.Fatalf("matrix cases=%d want=%d", len(cases), generatedRunnerLedgerConsumerPairCount)
	}
	counts := map[runnerLedgerConsumerAction]int{}
	executed := 0
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			executed++
			action, recoveryAction := generatedRunnerLedgerRecoveryAdmissionAction(test.disposition, test.state, test.action)
			if recoveryAction &&
				action == generatedRunnerLedgerRecoveryProfiles[1].action {
				fixture := newRunnerLedgerRecoveryAbortTerminalFixture(t, test.state, test.action)
				defer fixture.close(t)
				base := fixture.success.execution.base.service.kernel.base
				rows := runnerLedgerConsumerPrefixRows(base.bundle, 1)
				preflight := newRunnerPreflightSession()
				preflight.ledgerRowsByRead = [][]LedgerRow{cloneProjectionValue(rows), cloneProjectionValue(rows)}
				sequence := &runnerLedgerConsumerSequenceConnector{sessions: []*runnerPreflightSession{preflight, fixture.database}}
				baseRunner := fixture.success.execution.base.service.kernel.runner
				baseRunner.Connector = sequence
				step, err := baseRunner.consumeRunnerLedgerPreflightStep(
					context.Background(), "test-only", base.bundle, base.plans,
					fixture.success.execution.base.service.evidence, base.candidate,
				)
				counts[test.wantAction]++
				journal := fixture.success.execution.base.service.evidence.runnerEvidenceSessionFake.journal
				if err != nil || step.kind != runnerLedgerPreflightStepReenter || step.prefixLength != 1 ||
					sequence.attempts != 2 || preflight.ledgerReadCalls != 2 ||
					preflight.unlockCalls != 1 || preflight.closeCalls != 1 || fixture.database.ledgerReadCalls != 6 ||
					fixture.database.unlockCalls != 1 || fixture.database.closeCalls != 1 || fixture.database.beginCalls != 0 ||
					journal.appendedRecord.AttemptTerminal == nil || fixture.beforeCursor.Valid() {
					t.Fatalf("abort consumer matrix did not close and append exactly once: step=%+v sequence=%+v preflight=%+v admission=%+v journal=%+v", step, sequence, preflight, fixture.database, journal.appendedRecord)
				}
				return
			}
			if recoveryAction &&
				(action == generatedRunnerLedgerRecoveryProfiles[2].action || action == generatedRunnerLedgerRecoveryProfiles[3].action) {
				fixture := newRunnerLedgerRecoveryReconciliationFixture(t, test.state, runnerLedgerReconciliationExactPending, 16)
				defer fixture.close(t)
				base := fixture.success.execution.base.service.kernel.base
				rows := runnerLedgerReconciliationFixtureRows(base.bundle, runnerLedgerReconciliationExactPending)
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
				counts[test.wantAction]++
				if err != nil || step.kind != runnerLedgerPreflightStepReenter || step.prefixLength != 1 ||
					sequence.attempts != 2 || preflight.ledgerReadCalls != 2 ||
					preflight.unlockCalls != 1 || preflight.closeCalls != 1 || fixture.database.ledgerReadCalls != 6 ||
					fixture.database.unlockCalls != 1 || fixture.database.closeCalls != 1 || fixture.database.beginCalls != 0 ||
					journal.appendCalls != beforeAppends+1 || fixture.beforeCursor.Valid() {
					t.Fatalf("reconciliation consumer matrix did not append exactly once: step=%+v sequence=%+v preflight=%+v admission=%+v journal=%+v", step, sequence, preflight, fixture.database, journal)
				}
				if (test.state == RecoveryDanglingCommitIntent && journal.appendedRecord.AttemptTerminal == nil) ||
					(test.state == RecoveryAmbiguousUnresolved && journal.appendedRecord.AmbiguousResolution == nil) {
					t.Fatalf("reconciliation consumer appended wrong record: %+v", journal.appendedRecord)
				}
				return
			}
			if recoveryAction &&
				action == generatedRunnerLedgerRecoveryProfiles[4].action {
				fixture := runnerLedgerRetryHandoffOutcomeCases()[0].open(t)
				defer fixture.close()
				configureRunnerLedgerRetryHandoffAncestor(t, fixture.evidence)
				rows := runnerLedgerRetryHandoffDatabaseRows(fixture.bundle)
				preflight := runnerLedgerRetryHandoffDatabaseSession(rows, 2)
				admission := runnerLedgerRetryHandoffDatabaseSession(rows, 8)
				sequence := &runnerLedgerConsumerSequenceConnector{sessions: []*runnerPreflightSession{preflight, admission}}
				fixture.runner.Connector = sequence
				oldCursor := fixture.evidence.RecoverySnapshot().cursor.clone()
				step, err := fixture.runner.consumeRunnerLedgerPreflightStep(
					context.Background(), "test-only", fixture.bundle, fixture.plans, fixture.evidence, fixture.candidate,
				)
				counts[test.wantAction]++
				if err != nil || step.kind != runnerLedgerPreflightStepReenter || step.prefixLength != 1 || sequence.attempts != 2 ||
					fixture.evidence.retryHandoffBindCalls != 1 || oldCursor.Valid() ||
					!preflight.closed || !admission.closed || preflight.unlockCalls != 1 || admission.unlockCalls != 1 ||
					preflight.beginCalls != 0 || admission.beginCalls != 0 ||
					preflight.backend.executeCalls != 0 || admission.backend.executeCalls != 0 ||
					preflight.backend.ledgerInsertCalls != 0 || admission.backend.ledgerInsertCalls != 0 {
					t.Fatalf("retry-handoff consumer matrix escaped its boundary: step=%+v sequence=%+v preflight=%+v admission=%+v", step, sequence, preflight, admission)
				}
				return
			}
			if recoveryAction && action == generatedRunnerLedgerRecoveryProfiles[5].action {
				fixture := newRunnerLedgerRecoverySuccessAdmissionFixture(t, test.disposition, test.action)
				defer fixture.close(t)
				base := fixture.service.kernel.base
				prefixLength := len(fixture.service.evidence.schema.durableObservedLedgerPrefix)
				planCount := configureRunnerLedgerConsumerRecoveryExecution(t, fixture, prefixLength)
				rows := runnerLedgerConsumerPrefixRows(base.bundle, prefixLength)
				preflight := newRunnerPreflightSession()
				preflight.ledgerRowsByRead = [][]LedgerRow{cloneProjectionValue(rows), cloneProjectionValue(rows)}
				sequence := &runnerLedgerConsumerSequenceConnector{sessions: []*runnerPreflightSession{preflight, fixture.database}}
				fixture.service.kernel.runner.Connector = sequence
				step, err := fixture.service.kernel.runner.consumeRunnerLedgerPreflightStep(
					context.Background(), "test-only", base.bundle, base.plans, fixture.service.evidence, base.candidate,
				)
				counts[test.wantAction]++
				entries := base.bundle.Manifest.SchemaBundle.Migrations
				wantState := runnerLedgerEntrySuccessEntryCommittedNextEntry
				if prefixLength+1 == len(entries) {
					wantState = runnerLedgerEntrySuccessEntryCommittedComplete
				}
				if err != nil || step.kind != runnerLedgerPreflightStepEntryCommitted || step.ambiguousRecovered ||
					step.prefixLength != uint32(prefixLength) || step.nextEntryID != entries[prefixLength].ID ||
					step.outcome.state != wantState || step.outcome.migrationID != entries[prefixLength].ID ||
					step.outcome.ledgerLength != uint32(prefixLength+1) || sequence.attempts != 2 ||
					preflight.closed != true || fixture.database.closed != true || fixture.database.beginCalls != 1 ||
					fixture.database.transaction.executeCalls != planCount || fixture.database.transaction.ledgerInsertCalls != 1 ||
					fixture.database.transaction.commitCalls != 1 {
					t.Fatalf("recovery execution matrix step=%+v err=%v sequence=%+v preflight=%+v admission=%+v", step, err, sequence, preflight, fixture.database)
				}
				return
			}
			if recoveryAction && action == generatedRunnerLedgerRecoveryProfiles[7].action {
				assertRunnerLedgerConsumerMatrixFailure(t, test.state)
				counts[test.wantAction]++
				return
			}
			fixture := newRunnerLedgerPreflightServiceFixture(t)
			defer fixture.close(t)
			fixture.configure(t, test.disposition, test.state, test.action, 16)
			var admission *runnerPreflightSession
			var sequence *runnerLedgerConsumerSequenceConnector
			entrySupported := false
			recoverySupported := false
			planCount := 0
			if recoveryAction {
				recoverySupported = true
			}
			if test.wantAction == runnerLedgerConsumerEntryNotImplemented || recoverySupported {
				prefixLength := len(fixture.evidence.schema.durableObservedLedgerPrefix)
				admission = newRunnerPreflightSession()
				if test.wantAction == runnerLedgerConsumerEntryNotImplemented && !recoverySupported {
					_, entrySupported = generatedRunnerLedgerEntryExecutionAdmissionAction(test.disposition, test.state, test.action)
				}
				if entrySupported {
					entrySupported = true
					planCount = configureRunnerLedgerConsumerStepExecution(t, fixture, admission, prefixLength)
				} else {
					rows := runnerLedgerConsumerPrefixRows(fixture.kernel.base.bundle, prefixLength)
					admission.ledgerRowsByRead = [][]LedgerRow{
						cloneProjectionValue(rows), cloneProjectionValue(rows), cloneProjectionValue(rows), cloneProjectionValue(rows),
					}
				}
				sequence = &runnerLedgerConsumerSequenceConnector{sessions: []*runnerPreflightSession{fixture.kernel.database, admission}}
				fixture.kernel.runner.Connector = sequence
			}
			step, err := fixture.kernel.runner.consumeRunnerLedgerPreflightStep(
				context.Background(), "test-only", fixture.kernel.base.bundle, fixture.kernel.base.plans,
				fixture.evidence, fixture.kernel.base.candidate,
			)
			counts[test.wantAction]++
			switch test.wantAction {
			case runnerLedgerConsumerReturnSuccessNoop:
				entries := fixture.kernel.base.bundle.Manifest.SchemaBundle.Migrations
				if err != nil || step.kind != runnerLedgerPreflightStepComplete ||
					step.prefixLength != uint32(len(entries)) ||
					step.result.SchemaBundleDigest != fixture.kernel.base.bundle.Manifest.SchemaBundleDigest ||
					step.result.ManifestDigest != fixture.kernel.base.bundle.Manifest.ManifestDigest ||
					step.result.FinalHead != entries[len(entries)-1].ID || step.result.Applied == nil ||
					step.result.AmbiguousRecovered == nil || len(step.result.Applied) != 0 ||
					len(step.result.AmbiguousRecovered) != 0 {
					t.Fatalf("complete step=%+v err=%v", step, err)
				}
			case runnerLedgerConsumerEntryNotImplemented:
				if !entrySupported {
					assertRunnerLedgerConsumerNotImplemented(t, RunResult{}, err, "runner-ledger-consumer-entry")
					break
				}
				entries := fixture.kernel.base.bundle.Manifest.SchemaBundle.Migrations
				prefixLength := len(fixture.evidence.schema.durableObservedLedgerPrefix)
				wantState := runnerLedgerEntrySuccessEntryCommittedNextEntry
				if prefixLength+1 == len(entries) {
					wantState = runnerLedgerEntrySuccessEntryCommittedComplete
				}
				if err != nil || step.kind != runnerLedgerPreflightStepEntryCommitted || !step.outcome.valid() ||
					step.prefixLength != uint32(prefixLength) || step.nextEntryID != entries[prefixLength].ID ||
					step.outcome.migrationID != entries[prefixLength].ID || step.outcome.ledgerLength != uint32(prefixLength+1) ||
					step.outcome.state != wantState {
					t.Fatalf("entry step=%+v err=%v", step, err)
				}
			case runnerLedgerConsumerRecoveryNotImplemented:
				assertRunnerLedgerConsumerNotImplemented(t, RunResult{}, err, "runner-ledger-consumer-recovery")
			default:
				t.Fatalf("unexpected action %s", test.wantAction)
			}
			wantExecutionCalls := 0
			if entrySupported {
				wantExecutionCalls = 1
			}
			wantRecoveryCalls := 0
			if recoverySupported {
				wantRecoveryCalls = 1
			}
			if fixture.evidence.bindCalls != 1 || fixture.evidence.consumeCalls != 1 ||
				fixture.evidence.entryBindCalls != 0 || fixture.evidence.entryConsumeCalls != 0 ||
				fixture.evidence.executionBindCalls != wantExecutionCalls || fixture.evidence.executionConsumeCalls != wantExecutionCalls ||
				fixture.evidence.recoveryBindCalls != wantRecoveryCalls || fixture.evidence.recoveryConsumeCalls != wantRecoveryCalls {
				t.Fatalf("claim lifecycle bind=%d consume=%d entry-bind=%d entry-consume=%d execution-bind=%d execution-consume=%d recovery-bind=%d recovery-consume=%d", fixture.evidence.bindCalls, fixture.evidence.consumeCalls, fixture.evidence.entryBindCalls, fixture.evidence.entryConsumeCalls, fixture.evidence.executionBindCalls, fixture.evidence.executionConsumeCalls, fixture.evidence.recoveryBindCalls, fixture.evidence.recoveryConsumeCalls)
			}
			if _, live := runnerLedgerPreflightClaimByEvidenceBinder.Load(fixture.evidence); live {
				t.Fatal("consumer left a live one-shot claim")
			}
			if admission == nil {
				assertRunnerLedgerPreflightReadOnlyLifecycle(t, fixture)
			} else if entrySupported {
				if sequence.attempts != 2 || fixture.kernel.database.ledgerReadCalls != 2 || fixture.kernel.database.unlockCalls != 1 ||
					fixture.kernel.database.closeCalls != 1 || admission.ledgerReadCalls != 4 || admission.unlockCalls != 0 ||
					admission.closeCalls != 1 || admission.beginCalls != 1 || admission.transaction.executeCalls != planCount ||
					admission.transaction.ledgerInsertCalls != 1 || admission.transaction.commitCalls != 1 ||
					admission.backend.executeCalls != planCount || admission.backend.ledgerInsertCalls != 1 ||
					admission.backend.commitCalls != 1 || !admission.closed {
					t.Fatalf("entry matrix did not consume one fresh session: sequence=%+v preflight=%+v admission=%+v", sequence, fixture.kernel.database, admission)
				}
			} else if recoverySupported && (sequence.attempts != 2 || fixture.kernel.database.ledgerReadCalls != 2 ||
				fixture.kernel.database.unlockCalls != 1 || fixture.kernel.database.closeCalls != 1 ||
				admission.ledgerReadCalls != 4 || admission.unlockCalls != 1 || admission.closeCalls != 1 ||
				admission.beginCalls != 0 || admission.backend.executeCalls != 0 ||
				admission.backend.ledgerInsertCalls != 0 || !admission.closed) {
				t.Fatalf("recovery admission escaped close-only boundary: sequence=%+v preflight=%+v admission=%+v", sequence, fixture.kernel.database, admission)
			}
		})
	}
	if executed == len(cases) && (counts[runnerLedgerConsumerReturnSuccessNoop] != 1 || counts[runnerLedgerConsumerEntryNotImplemented] != 5 || counts[runnerLedgerConsumerRecoveryNotImplemented] != 11) {
		t.Fatalf("consumer counts=%v", counts)
	}
}

func configureRunnerLedgerConsumerRecoveryExecution(t *testing.T, fixture *runnerLedgerRecoveryAdmissionFixture, prefixLength int) int {
	t.Helper()
	base := fixture.service.kernel.base
	evidence := fixture.service.evidence
	snapshot := cloneRecoverySnapshot(evidence.recovery)
	evidence.runnerEvidenceSessionFake.snapshot = snapshot
	evidence.runnerEvidenceSessionFake.journal.snapshot = snapshot
	evidence.runnerEvidenceSessionFake.journal.cursor = snapshot.cursor.clone()
	entries := base.bundle.Manifest.SchemaBundle.Migrations
	if prefixLength < 0 || prefixLength >= len(entries) {
		t.Fatalf("recovery execution prefix=%d entries=%d", prefixLength, len(entries))
	}
	selected := make([]StatementPlan, 0)
	for _, plan := range base.plans {
		if plan.MigrationID == entries[prefixLength].ID {
			selected = append(selected, plan)
		}
	}
	if len(selected) == 0 {
		t.Fatal("recovery execution plans are unavailable")
	}
	factory := fixture.service.kernel.factory
	before := catalogStateForRunnerLedgerEntryPlan(t, evidence, selected[0], selected[0].ExpectedTransition.CatalogBefore)
	factory.transitionState = &before
	transaction := fixture.database.transaction
	transaction.ledgerPrefix = runnerLedgerConsumerPrefixRows(base.bundle, prefixLength)
	transaction.executeAllowed = true
	transaction.executeMutate = func([]byte) {
		index := transaction.executeCalls - 1
		if index < 0 || index >= len(selected) {
			t.Fatalf("unexpected recovery execute index %d", index)
		}
		after := catalogStateForRunnerLedgerEntryPlan(t, evidence, selected[index], selected[index].ExpectedTransition.CatalogAfter)
		factory.transitionState = &after
	}
	fixture.service.evidence.runnerEvidenceSessionFake.journal.bundleComplete = prefixLength+1 == len(entries)
	return len(selected)
}

func assertRunnerLedgerConsumerMatrixFailure(t *testing.T, state RecoveryState) {
	t.Helper()
	if state == RecoveryTerminal {
		fixture := newRunnerLedgerRecoveryAbortTerminalFixture(t, RecoveryDanglingStatementIntent, RecoveryAppendAbortedTerminal)
		defer fixture.close(t)
		if err := fixture.run(context.Background()); err != nil {
			t.Fatal(err)
		}
		evidence := fixture.success.execution.base.service.evidence
		refreshRunnerLedgerRecoveryTestFacts(evidence)
		base := fixture.success.execution.base.service.kernel.base
		sequence, _, _ := runnerLedgerReturnFailureTestSessions(base.bundle, evidence)
		fixture.success.execution.base.service.kernel.runner.Connector = sequence
		step, err := fixture.success.execution.base.service.kernel.runner.consumeRunnerLedgerPreflightStep(
			context.Background(), "test-only", base.bundle, base.plans, evidence, base.candidate,
		)
		assertRunnerLedgerTypedFailure(t, err, evidence.RecoverySnapshot().lastTerminal.Value())
		if !reflect.DeepEqual(step, runnerLedgerPreflightStep{}) {
			t.Fatalf("terminal failure step=%+v", step)
		}
		return
	}
	fixture := newRunnerLedgerRecoveryReconciliationFixture(t, RecoveryAmbiguousUnresolved, runnerLedgerReconciliationDivergent, 16)
	defer fixture.close(t)
	if err := fixture.run(context.Background()); err != nil {
		t.Fatal(err)
	}
	fixture.success.execution.base.service.kernel.factory.mutateCatalog = nil
	evidence := fixture.success.execution.base.service.evidence
	refreshRunnerLedgerRecoveryTestFacts(evidence)
	base := fixture.success.execution.base.service.kernel.base
	sequence, _, _ := runnerLedgerReturnFailureTestSessions(base.bundle, evidence)
	fixture.success.execution.base.service.kernel.runner.Connector = sequence
	step, err := fixture.success.execution.base.service.kernel.runner.consumeRunnerLedgerPreflightStep(
		context.Background(), "test-only", base.bundle, base.plans, evidence, base.candidate,
	)
	assertRunnerLedgerTypedFailure(t, err, evidence.RecoverySnapshot().lastTerminal.Value())
	if !reflect.DeepEqual(step, runnerLedgerPreflightStep{}) {
		t.Fatalf("divergent failure step=%+v", step)
	}
}

func TestRunnerLedgerConsumerFailedKernelDoesNotRetireExecutionAdmissionUse(t *testing.T) {
	fixture := newRunnerLedgerPreflightServiceFixture(t)
	defer fixture.close(t)
	fixture.configure(t, runnerLedgerPreflightEmptyBrandNew, RecoveryBrandNew, RecoveryBeginFirstAttempt, 16)
	admission := newRunnerPreflightSession()
	configureRunnerLedgerConsumerStepExecution(t, fixture, admission, 0)
	admission.transaction.executeErr = errors.New("closed test statement failure")
	sequence := &runnerLedgerConsumerSequenceConnector{sessions: []*runnerPreflightSession{fixture.kernel.database, admission}}
	fixture.kernel.runner.Connector = sequence

	step, err := fixture.kernel.runner.consumeRunnerLedgerPreflightStep(
		context.Background(), "test-only", fixture.kernel.base.bundle, fixture.kernel.base.plans,
		fixture.evidence, fixture.kernel.base.candidate,
	)
	if !reflect.DeepEqual(step, runnerLedgerPreflightStep{}) || err == nil || sequence.attempts != 2 ||
		admission.transaction.executeCalls != 1 || admission.transaction.rollbackCalls != 1 ||
		admission.transaction.commitCalls != 0 || !admission.closed {
		t.Fatalf("step=%+v err=%v sequence=%+v admission=%+v", step, err, sequence, admission)
	}
	value, live := runnerLedgerEntryExecutionAdmissionUseByEvidenceBinder.Load(fixture.evidence)
	use, typed := value.(*runnerLedgerEntryExecutionAdmissionUseRecord)
	if !live || !typed || use == nil {
		t.Fatalf("failed kernel use live=%v typed=%v value=%T", live, typed, value)
	}
	use.mu.Lock()
	consumed, retired := use.consumed, use.retired
	use.mu.Unlock()
	if !consumed || retired {
		t.Fatalf("failed kernel use consumed=%v retired=%v", consumed, retired)
	}
}

func runnerLedgerConsumerPrefixRows(bundle *RuntimeBundle, prefixLength int) []LedgerRow {
	rows := make([]LedgerRow, 0, prefixLength)
	for index := 0; index < prefixLength; index++ {
		rows = append(rows, ledgerRowFor(bundle.Manifest.SchemaBundle.Migrations[index], bundle.Manifest.SchemaBundleDigest))
	}
	return rows
}

func configureRunnerLedgerConsumerStepExecution(t *testing.T, fixture *runnerLedgerPreflightServiceFixture, admission *runnerPreflightSession, prefixLength int) int {
	t.Helper()
	base := fixture.kernel.base
	planCount := configureRunnerLedgerConsumerAdmissionSession(
		t, fixture.evidence, fixture.kernel.factory, base.bundle, base.plans, admission, prefixLength,
	)
	entries := base.bundle.Manifest.SchemaBundle.Migrations
	fixture.evidence.runnerEvidenceSessionFake.journal.bundleComplete = prefixLength+1 == len(entries)
	fixture.evidence.mu.Lock()
	recovery := cloneRecoverySnapshot(fixture.evidence.recovery)
	fixture.evidence.runnerEvidenceSessionFake.snapshot = recovery
	fixture.evidence.runnerEvidenceSessionFake.journal.snapshot = recovery
	fixture.evidence.runnerEvidenceSessionFake.journal.cursor = recovery.cursor.clone()
	fixture.evidence.mu.Unlock()
	return planCount
}

func configureRunnerLedgerConsumerAdmissionSession(
	t *testing.T,
	evidence EvidenceSession,
	factory *runnerPreflightProjectorFactory,
	bundle *RuntimeBundle,
	allPlans []StatementPlan,
	admission *runnerPreflightSession,
	prefixLength int,
) int {
	t.Helper()
	entries := bundle.Manifest.SchemaBundle.Migrations
	if prefixLength < 0 || prefixLength >= len(entries) {
		t.Fatalf("entry prefix=%d entries=%d", prefixLength, len(entries))
	}
	rows := runnerLedgerConsumerPrefixRows(bundle, prefixLength)
	admission.ledgerRowsByRead = [][]LedgerRow{
		cloneProjectionValue(rows), cloneProjectionValue(rows), cloneProjectionValue(rows), cloneProjectionValue(rows),
	}
	admission.transaction.ledgerPrefix = cloneProjectionValue(rows)
	plans := make([]StatementPlan, 0)
	for _, plan := range allPlans {
		if plan.MigrationID == entries[prefixLength].ID {
			plans = append(plans, plan)
		}
	}
	if len(plans) == 0 {
		t.Fatal("selected entry has no plans")
	}
	before := catalogStateForRunnerLedgerEntryPlan(t, evidence, plans[0], plans[0].ExpectedTransition.CatalogBefore)
	factory.transitionState = &before
	admission.afterLedgerRead[4] = func() {
		// Every entry owns a fresh execution session. Restore that entry's exact
		// catalog-before projection after its final admission reread and before
		// the success kernel opens the transaction. This keeps the shared test
		// projector faithful to independent database snapshots across entries.
		state := cloneProjectionValue(before)
		factory.transitionState = &state
	}
	admission.transaction.executeAllowed = true
	admission.transaction.executeMutate = func([]byte) {
		index := admission.transaction.executeCalls - 1
		if index < 0 || index >= len(plans) {
			t.Fatalf("unexpected execute index %d", index)
		}
		after := catalogStateForRunnerLedgerEntryPlan(t, evidence, plans[index], plans[index].ExpectedTransition.CatalogAfter)
		factory.transitionState = &after
	}
	return len(plans)
}

func assertRunnerLedgerConsumerNotImplemented(t *testing.T, result RunResult, err error, op string) {
	t.Helper()
	var stable *Error
	if !reflect.DeepEqual(result, RunResult{}) || !errors.As(err, &stable) || stable.Code != CodeProjectionNotImplemented || stable.Op != op || stable.Err != nil {
		t.Fatalf("result=%+v error=%#v", result, stable)
	}
}

func TestRunnerLedgerConsumerEligibilityIsExactCodeAndOperation(t *testing.T) {
	allowed := fail(CodeProjectionNotImplemented, "runner-current-execution-scope", "closed test", nil)
	if !runnerLedgerConsumerEligible(allowed, "runner-current-execution-scope") ||
		!runnerLedgerConsumerEligible(errors.Join(errors.New("outer"), allowed), "runner-current-execution-scope") {
		t.Fatal("exact projection boundary was not eligible")
	}
	for _, rejected := range []error{
		nil,
		fail(CodeProjectionNotImplemented, "runner-evidence-sink", "closed test", nil),
		fail(CodeEvidenceRecoveryRequired, "runner-current-execution-scope", "closed test", nil),
		errors.New("ordinary error"),
	} {
		if runnerLedgerConsumerEligible(rejected, "runner-current-execution-scope") {
			t.Fatalf("ineligible error entered consumer: %v", rejected)
		}
	}
}

func TestRunnerLedgerConsumerServiceContextAndClaimCleanupPrecedence(t *testing.T) {
	t.Run("pre-canceled", func(t *testing.T) {
		fixture := newRunnerLedgerPreflightServiceFixture(t)
		defer fixture.close(t)
		fixture.configure(t, runnerLedgerPreflightCompleteReturnSuccess, RecoveryCompleted, RecoveryReturnSuccess, 16)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		result, err := fixture.kernel.runner.consumeRunnerLedgerPreflight(ctx, "test-only", fixture.kernel.base.bundle, fixture.kernel.base.plans, fixture.evidence, fixture.kernel.base.candidate)
		if !reflect.DeepEqual(result, RunResult{}) || !IsCode(err, CodeContextCanceled) || fixture.kernel.connector.attempts != 0 || fixture.evidence.bindCalls != 0 || fixture.evidence.consumeCalls != 0 {
			t.Fatalf("result=%+v err=%v fixture=%+v", result, err, fixture)
		}
	})

	t.Run("pre-deadline", func(t *testing.T) {
		fixture := newRunnerLedgerPreflightServiceFixture(t)
		defer fixture.close(t)
		fixture.configure(t, runnerLedgerPreflightCompleteReturnSuccess, RecoveryCompleted, RecoveryReturnSuccess, 16)
		ctx, cancel := context.WithDeadline(context.Background(), time.Unix(1, 0))
		cancel()
		result, err := fixture.kernel.runner.consumeRunnerLedgerPreflight(ctx, "test-only", fixture.kernel.base.bundle, fixture.kernel.base.plans, fixture.evidence, fixture.kernel.base.candidate)
		if !reflect.DeepEqual(result, RunResult{}) || !IsCode(err, CodeDeadlineExceeded) || fixture.kernel.connector.attempts != 0 {
			t.Fatalf("result=%+v err=%v connector=%+v", result, err, fixture.kernel.connector)
		}
	})

	t.Run("canceled-after-bind-revokes", func(t *testing.T) {
		fixture := newRunnerLedgerPreflightServiceFixture(t)
		defer fixture.close(t)
		fixture.configure(t, runnerLedgerPreflightCompleteReturnSuccess, RecoveryCompleted, RecoveryReturnSuccess, 16)
		ctx, cancel := context.WithCancel(context.Background())
		fixture.evidence.afterBind = cancel
		result, err := fixture.kernel.runner.consumeRunnerLedgerPreflight(ctx, "test-only", fixture.kernel.base.bundle, fixture.kernel.base.plans, fixture.evidence, fixture.kernel.base.candidate)
		if !reflect.DeepEqual(result, RunResult{}) || !IsCode(err, CodeContextCanceled) || fixture.evidence.bindCalls != 1 || fixture.evidence.consumeCalls != 0 {
			t.Fatalf("result=%+v err=%v evidence=%+v", result, err, fixture.evidence)
		}
		if _, live := runnerLedgerPreflightClaimByEvidenceBinder.Load(fixture.evidence); live {
			t.Fatal("canceled service retained claim")
		}
		assertRunnerLedgerPreflightReadOnlyLifecycle(t, fixture)
	})

	t.Run("canceled-after-consume-cannot-return-success", func(t *testing.T) {
		fixture := newRunnerLedgerPreflightServiceFixture(t)
		defer fixture.close(t)
		fixture.configure(t, runnerLedgerPreflightCompleteReturnSuccess, RecoveryCompleted, RecoveryReturnSuccess, 16)
		ctx, cancel := context.WithCancel(context.Background())
		fixture.evidence.afterConsume = cancel
		result, err := fixture.kernel.runner.consumeRunnerLedgerPreflight(ctx, "test-only", fixture.kernel.base.bundle, fixture.kernel.base.plans, fixture.evidence, fixture.kernel.base.candidate)
		if !reflect.DeepEqual(result, RunResult{}) || !IsCode(err, CodeContextCanceled) || fixture.evidence.bindCalls != 1 || fixture.evidence.consumeCalls != 1 {
			t.Fatalf("result=%+v err=%v evidence=%+v", result, err, fixture.evidence)
		}
		if _, live := runnerLedgerPreflightClaimByEvidenceBinder.Load(fixture.evidence); live {
			t.Fatal("post-consume cancellation retained claim")
		}
		assertRunnerLedgerPreflightReadOnlyLifecycle(t, fixture)
	})

	t.Run("consume-failure-revokes", func(t *testing.T) {
		fixture := newRunnerLedgerPreflightServiceFixture(t)
		defer fixture.close(t)
		fixture.configure(t, runnerLedgerPreflightCompleteReturnSuccess, RecoveryCompleted, RecoveryReturnSuccess, 16)
		fixture.evidence.consumeErr = fail(CodeEvidenceJournalFailed, "test-consume", "closed test failure", nil)
		result, err := fixture.kernel.runner.consumeRunnerLedgerPreflight(context.Background(), "test-only", fixture.kernel.base.bundle, fixture.kernel.base.plans, fixture.evidence, fixture.kernel.base.candidate)
		if !reflect.DeepEqual(result, RunResult{}) || !IsCode(err, CodeEvidenceJournalFailed) || fixture.evidence.bindCalls != 1 || fixture.evidence.consumeCalls != 1 {
			t.Fatalf("result=%+v err=%v evidence=%+v", result, err, fixture.evidence)
		}
		if _, live := runnerLedgerPreflightClaimByEvidenceBinder.Load(fixture.evidence); live {
			t.Fatal("failed consumer retained claim")
		}
		assertRunnerLedgerPreflightReadOnlyLifecycle(t, fixture)
	})

	t.Run("public-bundle-view-drift-reloads-owned-inputs", func(t *testing.T) {
		fixture := newRunnerLedgerPreflightServiceFixture(t)
		defer fixture.close(t)
		fixture.configure(t, runnerLedgerPreflightCompleteReturnSuccess, RecoveryCompleted, RecoveryReturnSuccess, 16)
		wantManifest := fixture.kernel.base.bundle.Manifest.ManifestDigest
		fixture.evidence.mutateBeforeConsume = func(*runnerLedgerPreflightEvidenceFake) {
			fixture.kernel.base.bundle.Manifest.ManifestDigest = testDigest("caller-visible-drift")
		}
		result, err := fixture.kernel.runner.consumeRunnerLedgerPreflight(context.Background(), "test-only", fixture.kernel.base.bundle, fixture.kernel.base.plans, fixture.evidence, fixture.kernel.base.candidate)
		if err != nil || result.ManifestDigest != wantManifest {
			t.Fatalf("result=%+v err=%v want_manifest=%s", result, err, wantManifest)
		}
		assertRunnerLedgerPreflightReadOnlyLifecycle(t, fixture)
	})

	t.Run("owned-runtime-drift-after-claim-fails-closed", func(t *testing.T) {
		fixture := newRunnerLedgerPreflightServiceFixture(t)
		defer fixture.close(t)
		fixture.configure(t, runnerLedgerPreflightCompleteReturnSuccess, RecoveryCompleted, RecoveryReturnSuccess, 16)
		fixture.evidence.mutateBeforeConsume = func(*runnerLedgerPreflightEvidenceFake) {
			fixture.kernel.base.bundle.ownedInputs.files[RuntimeManifestPath][0] ^= 1
		}
		result, err := fixture.kernel.runner.consumeRunnerLedgerPreflight(context.Background(), "test-only", fixture.kernel.base.bundle, fixture.kernel.base.plans, fixture.evidence, fixture.kernel.base.candidate)
		if !reflect.DeepEqual(result, RunResult{}) || !IsCode(err, CodeUntrusted) {
			t.Fatalf("result=%+v err=%v", result, err)
		}
		if _, live := runnerLedgerPreflightClaimByEvidenceBinder.Load(fixture.evidence); live {
			t.Fatal("runtime drift retained claim")
		}
		assertRunnerLedgerPreflightReadOnlyLifecycle(t, fixture)
	})
}

type runnerLedgerConsumerEvidenceSink struct {
	prefixLength int
	state        RecoveryState
	action       RecoveryAction
	closeErr     error
	session      *runnerLedgerPreflightEvidenceFake
	afterOpen    func(*runnerLedgerPreflightEvidenceFake)
}

func (sink *runnerLedgerConsumerEvidenceSink) Open(_ context.Context, run VerifiedEvidenceRun, runtime VerifiedRuntimeArtifact) (EvidenceSession, *RecoverySnapshot, error) {
	candidate, err := ownedCurrentCandidateFromEvidenceRun(run, runtime)
	if err != nil {
		return nil, nil, err
	}
	bundle, err := LoadRuntimeBundle(runtime.bytes, run.currentDecision.decision)
	if err != nil {
		return nil, nil, err
	}
	bindings, err := run.currentDecision.decision.runnerProjectionBindings()
	if err != nil {
		return nil, nil, err
	}
	facts, err := buildHistoricalVerificationFacts(bundle, bindings)
	if err != nil {
		return nil, nil, err
	}
	base := newRunnerEvidenceSessionFake(candidate)
	base.closeErr = sink.closeErr
	_, schema, err := buildBrandNewRecoveryWitness(candidate, base.active.identity, facts)
	if err != nil {
		return nil, nil, err
	}
	if sink.prefixLength < 0 || sink.prefixLength > len(schema.signedExpectedLedgerRows) {
		return nil, nil, errors.New("invalid test prefix length")
	}
	schema.durableObservedLedgerPrefix = cloneProjectionValue(schema.signedExpectedLedgerRows[:sink.prefixLength])
	schema.durableObservedLedgerDigest, err = LedgerPrefixDigest(schema.durableObservedLedgerPrefix)
	if err != nil {
		return nil, nil, err
	}
	recovery := cloneRecoverySnapshot(base.snapshot)
	recovery.state, recovery.nextPermittedAction = sink.state, sink.action
	recovery.migrationID, recovery.attemptIndex = nil, nil
	switch {
	case sink.prefixLength == 0:
		if sink.state == RecoveryBrandNewInherited && sink.action == RecoveryBeginNextAttempt {
			migration, attempt := schema.orderedMigrations[0], uint32(2)
			recovery.migrationID, recovery.attemptIndex = &migration, &attempt
			recovery.previousAttemptTerminalDigest = digestPointer(testDigest("runner-ledger-consumer-public-previous-terminal"))
		}
	case sink.prefixLength == len(schema.orderedMigrations):
		migration, attempt := schema.orderedMigrations[sink.prefixLength-1], uint32(1)
		recovery.migrationID, recovery.attemptIndex = &migration, &attempt
	case sink.state == RecoveryBrandNewInherited && sink.action == RecoveryBeginFirstAttemptNextEntry:
		migration, attempt := schema.orderedMigrations[sink.prefixLength], uint32(1)
		recovery.migrationID, recovery.attemptIndex = &migration, &attempt
	case sink.state == RecoveryTerminal && sink.action == RecoveryBeginFirstAttemptNextEntry:
		migration, attempt := schema.orderedMigrations[sink.prefixLength-1], uint32(1)
		recovery.migrationID, recovery.attemptIndex = &migration, &attempt
	case sink.state == RecoveryBrandNewInherited && sink.action == RecoveryBeginFirstAttempt:
		// The inherited first-attempt recovery state intentionally carries no
		// migration or attempt identity.
	default:
		migration, attempt := schema.orderedMigrations[sink.prefixLength], uint32(1)
		if sink.state == RecoveryBrandNewInherited && sink.action == RecoveryBeginNextAttempt {
			attempt = 2
			recovery.previousAttemptTerminalDigest = digestPointer(testDigest("runner-ledger-consumer-public-previous-terminal"))
		}
		recovery.migrationID, recovery.attemptIndex = &migration, &attempt
	}
	base.snapshot = cloneRecoverySnapshot(recovery)
	base.journal.snapshot = base.snapshot
	wrapper := &runnerLedgerPreflightEvidenceFake{
		runnerEvidenceSessionFake: base,
		schema:                    schema,
		recovery:                  cloneRecoverySnapshot(recovery),
		sessionDigest:             digestRaw(testDigest("runner-ledger-consumer-public-session")),
		journalDigest:             digestRaw(testDigest("runner-ledger-consumer-public-journal")),
	}
	wrapper.mutateSuccessAuthority = func(_ *JournalCursor, owned *OwnedEvidenceRecord) {
		if owned == nil || owned.wire.AttemptTerminal == nil {
			return
		}
		last := wrapper.schema.orderedMigrations[len(wrapper.schema.orderedMigrations)-1]
		base.journal.bundleComplete = owned.wire.AttemptTerminal.MigrationID == last
	}
	base.journal.mutateAppendSnapshot = func(snapshot *RecoverySnapshot) {
		wrapper.mu.Lock()
		defer wrapper.mu.Unlock()
		wrapper.recovery = cloneRecoverySnapshot(snapshot)
		terminal := base.journal.appendedRecord.AttemptTerminal
		if terminal == nil {
			return
		}
		committed := 0
		for index, migrationID := range wrapper.schema.orderedMigrations {
			if migrationID == terminal.MigrationID {
				committed = index + 1
				break
			}
		}
		if committed == 0 {
			wrapper.schema.durableObservedLedgerPrefix = nil
			wrapper.schema.durableObservedLedgerDigest = ""
			return
		}
		wrapper.schema.durableObservedLedgerPrefix = cloneProjectionValue(wrapper.schema.signedExpectedLedgerRows[:committed])
		digest, digestErr := LedgerPrefixDigest(wrapper.schema.durableObservedLedgerPrefix)
		if digestErr != nil {
			wrapper.schema.durableObservedLedgerDigest = ""
			return
		}
		wrapper.schema.durableObservedLedgerDigest = digest
	}
	sink.session = wrapper
	if sink.afterOpen != nil {
		sink.afterOpen(wrapper)
	}
	return wrapper, cloneRecoverySnapshot(recovery), nil
}

func (*runnerLedgerConsumerEvidenceSink) evidenceSinkSealed() {}

func TestPublicRunnerReturnsCompleteLedgerNoopWithoutWriterEffects(t *testing.T) {
	raw, decision := buildExactTwoMigrationAdmissionRuntime(t)
	bundle, err := LoadRuntimeBundle(raw, decision)
	if err != nil {
		t.Fatal(err)
	}
	database := newRunnerPreflightSession()
	rows := make([]LedgerRow, 0, len(bundle.Manifest.SchemaBundle.Migrations))
	for _, entry := range bundle.Manifest.SchemaBundle.Migrations {
		rows = append(rows, ledgerRowFor(entry, bundle.Manifest.SchemaBundleDigest))
	}
	database.ledgerRowsByRead = [][]LedgerRow{cloneProjectionValue(rows), cloneProjectionValue(rows)}
	connector := &runnerPreflightConnector{session: database}
	factory := &runnerPreflightProjectorFactory{allowMigrationRoleCatalog: true}
	factory.initialize()
	sink := &runnerLedgerConsumerEvidenceSink{prefixLength: len(rows), state: RecoveryCompleted, action: RecoveryReturnSuccess}
	observer := &recordingStateObserver{}
	runner := Runner{
		Trust:    &sequenceTrustVerifier{fallback: decision, recoveryArtifact: runnerDecisionRecoveryArtifact(t, decision)},
		Evidence: sink, Connector: connector, Observer: observer, projectionFactory: factory,
	}
	before := liveVerifiedEvidenceRunBindings()
	result, err := runner.Run(context.Background(), RunRequest{Artifact: &memoryArtifactSource{data: raw}, TargetDSN: "test-only"})
	if err != nil {
		t.Fatalf("complete no-op result=%+v err=%v", result, err)
	}
	last := bundle.Manifest.SchemaBundle.Migrations[len(bundle.Manifest.SchemaBundle.Migrations)-1].ID
	if result.SchemaBundleDigest != bundle.Manifest.SchemaBundleDigest || result.ManifestDigest != bundle.Manifest.ManifestDigest ||
		result.FinalHead != last || result.Applied == nil || result.AmbiguousRecovered == nil || len(result.Applied) != 0 || len(result.AmbiguousRecovered) != 0 {
		t.Fatalf("complete no-op result=%+v", result)
	}
	if sink.session == nil || sink.session.bindCalls != 1 || sink.session.consumeCalls != 1 ||
		sink.session.runnerEvidenceSessionFake.bindCalls != 0 || sink.session.intermediateBindCalls != 0 ||
		sink.session.commitBindCalls != 0 || sink.session.terminalBindCalls != 0 ||
		sink.session.journal.appendCalls != 0 || sink.session.closeCalls != 1 || sink.session.journal.closeCalls != 1 ||
		connector.attempts != 1 || database.beginCalls != 0 || database.transaction.executeCalls != 0 ||
		database.backend.executeCalls != 0 || database.backend.ledgerInsertCalls != 0 || database.backend.commitCalls != 0 ||
		database.ledgerReadCalls != 2 || database.unlockCalls != 1 || database.closeCalls != 1 || !database.closed ||
		liveVerifiedEvidenceRunBindings() != before || !reflect.DeepEqual(observer.transitions, []RunnerState{StateVerifyTrust, StateLoadBundle, StateComplete}) {
		t.Fatalf("complete no-op escaped boundary: sink=%+v database=%+v transaction=%+v transitions=%v live=%d/%d", sink.session, database, database.transaction, observer.transitions, liveVerifiedEvidenceRunBindings(), before)
	}
	if _, live := runnerLedgerPreflightClaimByEvidenceBinder.Load(sink.session); live {
		t.Fatal("public complete no-op retained a one-shot claim")
	}
}

func TestPublicRunnerExecutesGeneratedFirstAttemptEntryLoopWithFreshSessions(t *testing.T) {
	raw, decision := buildExactTwoMigrationAdmissionRuntime(t)
	bundle, err := LoadRuntimeBundle(raw, decision)
	if err != nil {
		t.Fatal(err)
	}
	bindings, err := decision.runnerProjectionBindings()
	if err != nil {
		t.Fatal(err)
	}
	plans, err := buildExactStatementPlans(bundle, bindings, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	entries := bundle.Manifest.SchemaBundle.Migrations
	if len(entries) != 2 {
		t.Fatalf("entry count=%d", len(entries))
	}
	preflightFirst := newRunnerPreflightSession()
	preflightFirst.ledgerRowsByRead = [][]LedgerRow{{}, {}}
	executeFirst := newRunnerPreflightSession()
	preflightSecond := newRunnerPreflightSession()
	firstRows := runnerLedgerConsumerPrefixRows(bundle, 1)
	preflightSecond.ledgerRowsByRead = [][]LedgerRow{cloneProjectionValue(firstRows), cloneProjectionValue(firstRows)}
	executeSecond := newRunnerPreflightSession()
	preflightComplete := newRunnerPreflightSession()
	completeRows := runnerLedgerConsumerPrefixRows(bundle, len(entries))
	preflightComplete.ledgerRowsByRead = [][]LedgerRow{cloneProjectionValue(completeRows), cloneProjectionValue(completeRows)}
	sessions := []*runnerPreflightSession{preflightFirst, executeFirst, preflightSecond, executeSecond, preflightComplete}
	connector := &runnerLedgerConsumerSequenceConnector{sessions: sessions}
	factory := &runnerPreflightProjectorFactory{allowMigrationRoleCatalog: true}
	factory.initialize()
	planCounts := make([]int, 2)
	sink := &runnerLedgerConsumerEvidenceSink{
		prefixLength: 0, state: RecoveryBrandNew, action: RecoveryBeginFirstAttempt,
		afterOpen: func(evidence *runnerLedgerPreflightEvidenceFake) {
			// Configure the successor first so the initial entry's before-state is
			// the final factory value when Run starts. Entry one then advances the
			// shared projection to the exact before-state of entry two.
			planCounts[1] = configureRunnerLedgerConsumerAdmissionSession(t, evidence, factory, bundle, plans, executeSecond, 1)
			planCounts[0] = configureRunnerLedgerConsumerAdmissionSession(t, evidence, factory, bundle, plans, executeFirst, 0)
		},
	}
	observer := &recordingStateObserver{}
	runner := Runner{
		Trust:    &sequenceTrustVerifier{fallback: decision, recoveryArtifact: runnerDecisionRecoveryArtifact(t, decision)},
		Evidence: sink, Connector: connector, Observer: observer, projectionFactory: factory,
	}
	before := liveVerifiedEvidenceRunBindings()
	result, runErr := runner.Run(context.Background(), RunRequest{Artifact: &memoryArtifactSource{data: raw}, TargetDSN: "test-only"})
	wantApplied := []string{entries[0].ID, entries[1].ID}
	if runErr != nil || result.SchemaBundleDigest != bundle.Manifest.SchemaBundleDigest ||
		result.ManifestDigest != bundle.Manifest.ManifestDigest || result.FinalHead != entries[1].ID ||
		!reflect.DeepEqual(result.Applied, wantApplied) || result.AmbiguousRecovered == nil ||
		len(result.AmbiguousRecovered) != 0 {
		t.Fatalf("entry-loop result=%+v err=%v", result, runErr)
	}
	if connector.attempts != len(sessions) || sink.session == nil || sink.session.bindCalls != 3 ||
		sink.session.consumeCalls != 3 || sink.session.executionBindCalls != 2 || sink.session.executionConsumeCalls != 2 ||
		sink.session.entryBindCalls != 0 || sink.session.entryConsumeCalls != 0 ||
		sink.session.successBindCalls != 2*(planCounts[0]+planCounts[1])+4 ||
		sink.session.journal.appendCalls != 2*(planCounts[0]+planCounts[1])+4 ||
		sink.session.closeCalls != 1 || sink.session.journal.closeCalls != 1 ||
		liveVerifiedEvidenceRunBindings() != before {
		t.Fatalf("entry-loop authority lifecycle mismatch: connector=%+v evidence=%+v plans=%v live=%d/%d", connector, sink.session, planCounts, liveVerifiedEvidenceRunBindings(), before)
	}
	for index, database := range sessions {
		if index%2 == 0 {
			if database.ledgerReadCalls != 2 || database.beginCalls != 0 || database.unlockCalls != 1 ||
				database.closeCalls != 1 || database.backend.executeCalls != 0 ||
				database.backend.ledgerInsertCalls != 0 || database.backend.commitCalls != 0 || !database.closed {
				t.Fatalf("preflight session %d escaped boundary: %+v", index, database)
			}
			continue
		}
		entryIndex := index / 2
		if database.ledgerReadCalls != 4 || database.beginCalls != 1 || database.unlockCalls != 0 ||
			database.closeCalls != 1 || database.transaction.executeCalls != planCounts[entryIndex] ||
			database.transaction.ledgerInsertCalls != 1 || database.transaction.commitCalls != 1 ||
			database.transaction.rollbackCalls != 0 || database.backend.executeCalls != planCounts[entryIndex] ||
			database.backend.ledgerInsertCalls != 1 || database.backend.commitCalls != 1 || !database.closed {
			t.Fatalf("entry session %d was not fresh and single-use: %+v", index, database)
		}
	}
	if len(observer.transitions) == 0 || observer.transitions[len(observer.transitions)-1] != StateComplete {
		t.Fatalf("entry loop did not reach one final completion: %v", observer.transitions)
	}
	if _, live := runnerLedgerPreflightClaimByEvidenceBinder.Load(sink.session); live {
		t.Fatal("entry loop retained a preflight claim")
	}
	if _, live := runnerLedgerEntryExecutionAdmissionUseByEvidenceBinder.Load(sink.session); live {
		t.Fatal("entry loop retained an execution-admission use record")
	}
}

func TestPublicRunnerExecutesPartialImmediateNextEntryOnOneFreshSession(t *testing.T) {
	raw, decision := buildExactTwoMigrationAdmissionRuntime(t)
	bundle, err := LoadRuntimeBundle(raw, decision)
	if err != nil {
		t.Fatal(err)
	}
	bindings, err := decision.runnerProjectionBindings()
	if err != nil {
		t.Fatal(err)
	}
	plans, err := buildExactStatementPlans(bundle, bindings, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	entries := bundle.Manifest.SchemaBundle.Migrations
	if len(entries) != 2 {
		t.Fatalf("entry count=%d", len(entries))
	}
	rows := runnerLedgerConsumerPrefixRows(bundle, 1)
	preflight := newRunnerPreflightSession()
	preflight.ledgerRowsByRead = [][]LedgerRow{cloneProjectionValue(rows), cloneProjectionValue(rows)}
	execution := newRunnerPreflightSession()
	complete := newRunnerPreflightSession()
	completeRows := runnerLedgerConsumerPrefixRows(bundle, len(entries))
	complete.ledgerRowsByRead = [][]LedgerRow{cloneProjectionValue(completeRows), cloneProjectionValue(completeRows)}
	connector := &runnerLedgerConsumerSequenceConnector{sessions: []*runnerPreflightSession{preflight, execution, complete}}
	factory := &runnerPreflightProjectorFactory{allowMigrationRoleCatalog: true}
	factory.initialize()
	planCount := 0
	sink := &runnerLedgerConsumerEvidenceSink{
		prefixLength: 1, state: RecoveryBrandNewInherited, action: RecoveryBeginFirstAttemptNextEntry,
		afterOpen: func(evidence *runnerLedgerPreflightEvidenceFake) {
			planCount = configureRunnerLedgerConsumerAdmissionSession(t, evidence, factory, bundle, plans, execution, 1)
		},
	}
	runner := Runner{
		Trust:    &sequenceTrustVerifier{fallback: decision, recoveryArtifact: runnerDecisionRecoveryArtifact(t, decision)},
		Evidence: sink, Connector: connector, projectionFactory: factory,
	}
	result, runErr := runner.Run(context.Background(), RunRequest{Artifact: &memoryArtifactSource{data: raw}, TargetDSN: "test-only"})
	if runErr != nil || result.SchemaBundleDigest != bundle.Manifest.SchemaBundleDigest ||
		result.ManifestDigest != bundle.Manifest.ManifestDigest || result.FinalHead != entries[1].ID ||
		!reflect.DeepEqual(result.Applied, []string{entries[1].ID}) || result.AmbiguousRecovered == nil ||
		len(result.AmbiguousRecovered) != 0 {
		t.Fatalf("partial entry-loop result=%+v err=%v", result, runErr)
	}
	if connector.attempts != 3 || preflight.ledgerReadCalls != 2 || preflight.beginCalls != 0 ||
		preflight.unlockCalls != 1 || preflight.closeCalls != 1 || !preflight.closed ||
		execution.ledgerReadCalls != 4 || execution.beginCalls != 1 || execution.transaction.executeCalls != planCount ||
		execution.transaction.ledgerInsertCalls != 1 || execution.transaction.commitCalls != 1 ||
		execution.transaction.rollbackCalls != 0 || execution.unlockCalls != 0 || execution.closeCalls != 1 ||
		!execution.closed || complete.ledgerReadCalls != 2 || complete.beginCalls != 0 || complete.unlockCalls != 1 ||
		complete.closeCalls != 1 || !complete.closed || sink.session == nil || sink.session.bindCalls != 2 || sink.session.consumeCalls != 2 ||
		sink.session.executionBindCalls != 1 || sink.session.executionConsumeCalls != 1 {
		t.Fatalf("partial entry-loop lifecycle: connector=%+v preflight=%+v execution=%+v evidence=%+v", connector, preflight, execution, sink.session)
	}
}

func TestPublicRunnerReentersFreshPreflightAfterCommitAndDoesNotReuseEntryOutcome(t *testing.T) {
	raw, decision := buildExactTwoMigrationAdmissionRuntime(t)
	bundle, err := LoadRuntimeBundle(raw, decision)
	if err != nil {
		t.Fatal(err)
	}
	bindings, err := decision.runnerProjectionBindings()
	if err != nil {
		t.Fatal(err)
	}
	plans, err := buildExactStatementPlans(bundle, bindings, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	preflightFirst := newRunnerPreflightSession()
	preflightFirst.ledgerRowsByRead = [][]LedgerRow{{}, {}}
	executionFirst := newRunnerPreflightSession()
	preflightSecond := newRunnerPreflightSession()
	preflightSecond.ledgerReadErr[1] = errors.New("closed test successor preflight failure")
	connector := &runnerLedgerConsumerSequenceConnector{sessions: []*runnerPreflightSession{preflightFirst, executionFirst, preflightSecond}}
	factory := &runnerPreflightProjectorFactory{allowMigrationRoleCatalog: true}
	factory.initialize()
	planCount := 0
	sink := &runnerLedgerConsumerEvidenceSink{
		prefixLength: 0, state: RecoveryBrandNew, action: RecoveryBeginFirstAttempt,
		afterOpen: func(evidence *runnerLedgerPreflightEvidenceFake) {
			planCount = configureRunnerLedgerConsumerAdmissionSession(t, evidence, factory, bundle, plans, executionFirst, 0)
		},
	}
	observer := &recordingStateObserver{}
	runner := Runner{
		Trust:    &sequenceTrustVerifier{fallback: decision, recoveryArtifact: runnerDecisionRecoveryArtifact(t, decision)},
		Evidence: sink, Connector: connector, Observer: observer, projectionFactory: factory,
	}
	result, runErr := runner.Run(context.Background(), RunRequest{Artifact: &memoryArtifactSource{data: raw}, TargetDSN: "test-only"})
	if !reflect.DeepEqual(result, RunResult{}) || runErr == nil || connector.attempts != 3 ||
		executionFirst.transaction.executeCalls != planCount || executionFirst.transaction.ledgerInsertCalls != 1 ||
		executionFirst.transaction.commitCalls != 1 || executionFirst.transaction.rollbackCalls != 0 ||
		preflightSecond.ledgerReadCalls != 1 || preflightSecond.beginCalls != 0 || preflightSecond.unlockCalls != 1 ||
		preflightSecond.closeCalls != 1 || !preflightSecond.closed || sink.session == nil ||
		sink.session.journal.appendCalls != 2*planCount+2 {
		t.Fatalf("result=%+v err=%v connector=%+v execution=%+v successor=%+v evidence=%+v", result, runErr, connector, executionFirst, preflightSecond, sink.session)
	}
	for _, state := range observer.transitions {
		if state == StateComplete {
			t.Fatalf("failed fresh preflight reused the committed outcome: %v", observer.transitions)
		}
	}
}

func TestPublicRunnerGeneratedRecoveryRequiresAuthenticDurableBoundary(t *testing.T) {
	raw, decision := buildExactTwoMigrationAdmissionRuntime(t)
	bundle, err := LoadRuntimeBundle(raw, decision)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name         string
		prefixLength int
		state        RecoveryState
		action       RecoveryAction
		consumeCalls int
	}{
		{"inherited-retry-without-continuation", 0, RecoveryBrandNewInherited, RecoveryBeginNextAttempt, 1},
		{"terminal-retry-without-terminal", 1, RecoveryTerminal, RecoveryBeginNextAttempt, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			database := newRunnerPreflightSession()
			rows := make([]LedgerRow, 0, test.prefixLength)
			for index := 0; index < test.prefixLength; index++ {
				rows = append(rows, ledgerRowFor(bundle.Manifest.SchemaBundle.Migrations[index], bundle.Manifest.SchemaBundleDigest))
			}
			database.ledgerRowsByRead = [][]LedgerRow{cloneProjectionValue(rows), cloneProjectionValue(rows)}
			admission := newRunnerPreflightSession()
			admission.ledgerRowsByRead = [][]LedgerRow{
				cloneProjectionValue(rows), cloneProjectionValue(rows),
				cloneProjectionValue(rows), cloneProjectionValue(rows),
			}
			databases := []*runnerPreflightSession{database, admission}
			var connector DatabaseConnector = &runnerLedgerConsumerSequenceConnector{sessions: databases}
			factory := &runnerPreflightProjectorFactory{allowMigrationRoleCatalog: true}
			factory.initialize()
			sink := &runnerLedgerConsumerEvidenceSink{prefixLength: test.prefixLength, state: test.state, action: test.action}
			observer := &recordingStateObserver{}
			runner := Runner{
				Trust:    &sequenceTrustVerifier{fallback: decision, recoveryArtifact: runnerDecisionRecoveryArtifact(t, decision)},
				Evidence: sink, Connector: connector, Observer: observer, projectionFactory: factory,
			}
			before := liveVerifiedEvidenceRunBindings()
			result, runErr := runner.Run(context.Background(), RunRequest{Artifact: &memoryArtifactSource{data: raw}, TargetDSN: "test-only"})
			var stable *Error
			if !reflect.DeepEqual(result, RunResult{}) || !errors.As(runErr, &stable) ||
				stable.Code != CodeEvidenceRecoveryRequired || stable.Err != nil {
				t.Fatalf("result=%+v error=%#v", result, stable)
			}
			if sink.session == nil || sink.session.bindCalls != 1 || sink.session.consumeCalls != 1 ||
				sink.session.entryBindCalls != 0 || sink.session.entryConsumeCalls != 0 ||
				sink.session.executionBindCalls != 0 || sink.session.executionConsumeCalls != 0 ||
				sink.session.recoveryBindCalls != 1 || sink.session.recoveryConsumeCalls != test.consumeCalls ||
				sink.session.runnerEvidenceSessionFake.bindCalls != 0 || sink.session.intermediateBindCalls != 0 ||
				sink.session.commitBindCalls != 0 || sink.session.terminalBindCalls != 0 ||
				sink.session.journal.appendCalls != 0 || sink.session.closeCalls != 1 || sink.session.journal.closeCalls != 1 ||
				liveVerifiedEvidenceRunBindings() != before || reflect.DeepEqual(observer.transitions, []RunnerState{StateVerifyTrust, StateLoadBundle, StateComplete}) {
				t.Fatalf("public %s escaped boundary: sink=%+v databases=%+v transitions=%v live=%d/%d", test.name, sink.session, databases, observer.transitions, liveVerifiedEvidenceRunBindings(), before)
			}
			for index, observed := range databases {
				wantReads := 2
				wantUnlocks := 1
				wantCloses := 1
				wantClosed := true
				if index == 1 {
					if test.consumeCalls == 0 {
						wantReads, wantUnlocks, wantCloses, wantClosed = 0, 0, 0, false
					} else {
						wantReads = 4
					}
				}
				if observed.ledgerReadCalls != wantReads || observed.beginCalls != 0 || observed.transaction.executeCalls != 0 ||
					observed.backend.executeCalls != 0 || observed.backend.ledgerInsertCalls != 0 || observed.backend.commitCalls != 0 ||
					observed.unlockCalls != wantUnlocks || observed.closeCalls != wantCloses || observed.closed != wantClosed {
					t.Fatalf("public %s database %d escaped boundary: %+v", test.name, index, observed)
				}
			}
			for _, state := range observer.transitions {
				if state == StateComplete {
					t.Fatalf("public %s reached complete: %v", test.name, observer.transitions)
				}
			}
			if _, live := runnerLedgerPreflightClaimByEvidenceBinder.Load(sink.session); live {
				t.Fatalf("public %s retained a one-shot claim", test.name)
			}
			if _, live := runnerLedgerEntryAdmissionUseByEvidenceBinder.Load(sink.session); live {
				t.Fatalf("public %s retained an entry-admission use record", test.name)
			}
			if _, live := runnerLedgerEntryExecutionAdmissionUseByEvidenceBinder.Load(sink.session); live {
				t.Fatalf("public %s retained an execution-admission use record", test.name)
			}
			if _, live := runnerLedgerRecoveryAdmissionUseByEvidenceBind.Load(sink.session); live {
				t.Fatalf("public %s retained a recovery-admission use record", test.name)
			}
		})
	}
}

func TestPublicRunnerCompleteNoopEvidenceCloseFailureDominatesSuccess(t *testing.T) {
	raw, decision := buildExactTwoMigrationAdmissionRuntime(t)
	bundle, err := LoadRuntimeBundle(raw, decision)
	if err != nil {
		t.Fatal(err)
	}
	database := newRunnerPreflightSession()
	rows := make([]LedgerRow, 0, len(bundle.Manifest.SchemaBundle.Migrations))
	for _, entry := range bundle.Manifest.SchemaBundle.Migrations {
		rows = append(rows, ledgerRowFor(entry, bundle.Manifest.SchemaBundleDigest))
	}
	database.ledgerRowsByRead = [][]LedgerRow{cloneProjectionValue(rows), cloneProjectionValue(rows)}
	connector := &runnerPreflightConnector{session: database}
	factory := &runnerPreflightProjectorFactory{allowMigrationRoleCatalog: true}
	factory.initialize()
	sink := &runnerLedgerConsumerEvidenceSink{
		prefixLength: len(rows), state: RecoveryCompleted, action: RecoveryReturnSuccess,
		closeErr: errors.New("secret-close-response-lost"),
	}
	observer := &recordingStateObserver{}
	runner := Runner{
		Trust:    &sequenceTrustVerifier{fallback: decision, recoveryArtifact: runnerDecisionRecoveryArtifact(t, decision)},
		Evidence: sink, Connector: connector, Observer: observer, projectionFactory: factory,
	}
	before := liveVerifiedEvidenceRunBindings()
	result, runErr := runner.Run(context.Background(), RunRequest{Artifact: &memoryArtifactSource{data: raw}, TargetDSN: "test-only"})
	var stable *Error
	if !reflect.DeepEqual(result, RunResult{}) || !errors.As(runErr, &stable) || stable.Code != CodeEvidenceJournalFailed ||
		stable.Op != "runner-evidence-close" || stable.Err != nil || strings.Contains(runErr.Error(), "secret-") {
		t.Fatalf("result=%+v err=%#v", result, stable)
	}
	if sink.session == nil || sink.session.closeCalls != 1 || sink.session.journal.closeCalls != 1 ||
		sink.session.journal.appendCalls != 0 || database.beginCalls != 0 || database.backend.executeCalls != 0 ||
		database.backend.ledgerInsertCalls != 0 || database.backend.commitCalls != 0 || database.closeCalls != 1 ||
		liveVerifiedEvidenceRunBindings() != before {
		t.Fatalf("close failure escaped boundary: sink=%+v database=%+v live=%d/%d", sink.session, database, liveVerifiedEvidenceRunBindings(), before)
	}
	for _, state := range observer.transitions {
		if state == StateComplete {
			t.Fatalf("close failure reached complete: %v", observer.transitions)
		}
	}
}

type runnerLedgerConsumerSequenceConnector struct {
	sessions []*runnerPreflightSession
	attempts int
}

func (connector *runnerLedgerConsumerSequenceConnector) Connect(context.Context, string) (DatabaseSession, error) {
	index := connector.attempts
	connector.attempts++
	if index >= len(connector.sessions) {
		return nil, errors.New("unexpected test connection")
	}
	return connector.sessions[index], nil
}

func TestPublicRunnerCompleteSingleEntryFallsFromWriterPreflightIntoNoopConsumer(t *testing.T) {
	raw, decision := buildExactAdmissionRuntime(t)
	bundle, err := LoadRuntimeBundle(raw, decision)
	if err != nil {
		t.Fatal(err)
	}
	row := ledgerRowFor(bundle.Manifest.SchemaBundle.Migrations[0], bundle.Manifest.SchemaBundleDigest)
	writerPreflight := newRunnerPreflightSession()
	writerPreflight.ledgerRowsByRead = [][]LedgerRow{{cloneProjectionValue(row)}}
	consumerPreflight := newRunnerPreflightSession()
	consumerPreflight.ledgerRowsByRead = [][]LedgerRow{{cloneProjectionValue(row)}, {cloneProjectionValue(row)}}
	connector := &runnerLedgerConsumerSequenceConnector{sessions: []*runnerPreflightSession{writerPreflight, consumerPreflight}}
	factory := &runnerPreflightProjectorFactory{allowMigrationRoleCatalog: true}
	factory.initialize()
	sink := &runnerLedgerConsumerEvidenceSink{prefixLength: 1, state: RecoveryCompleted, action: RecoveryReturnSuccess}
	observer := &recordingStateObserver{}
	runner := Runner{
		Trust:    &sequenceTrustVerifier{fallback: decision, recoveryArtifact: runnerDecisionRecoveryArtifact(t, decision)},
		Evidence: sink, Connector: connector, Observer: observer, projectionFactory: factory,
	}
	before := liveVerifiedEvidenceRunBindings()
	result, err := runner.Run(context.Background(), RunRequest{Artifact: &memoryArtifactSource{data: raw}, TargetDSN: "test-only"})
	if err != nil || result.FinalHead != row.MigrationID || result.SchemaBundleDigest != bundle.Manifest.SchemaBundleDigest ||
		result.ManifestDigest != bundle.Manifest.ManifestDigest || result.Applied == nil || result.AmbiguousRecovered == nil ||
		len(result.Applied) != 0 || len(result.AmbiguousRecovered) != 0 {
		t.Fatalf("single-entry complete result=%+v err=%v", result, err)
	}
	for index, database := range []*runnerPreflightSession{writerPreflight, consumerPreflight} {
		wantReads := index + 1
		if database.ledgerReadCalls != wantReads || database.beginCalls != 0 || database.backend.executeCalls != 0 ||
			database.backend.ledgerInsertCalls != 0 || database.backend.commitCalls != 0 || database.unlockCalls != 1 ||
			database.closeCalls != 1 || !database.closed {
			t.Fatalf("database %d escaped no-op boundary: %+v", index, database)
		}
	}
	if connector.attempts != 2 || sink.session == nil || sink.session.bindCalls != 1 || sink.session.consumeCalls != 1 ||
		sink.session.journal.appendCalls != 0 || sink.session.closeCalls != 1 || liveVerifiedEvidenceRunBindings() != before ||
		!reflect.DeepEqual(observer.transitions, []RunnerState{StateVerifyTrust, StateLoadBundle, StateConnect, StateLocked, StateComplete}) {
		t.Fatalf("single-entry fallback escaped: connector=%+v sink=%+v transitions=%v live=%d/%d", connector, sink.session, observer.transitions, liveVerifiedEvidenceRunBindings(), before)
	}
	if _, live := runnerLedgerPreflightClaimByEvidenceBinder.Load(sink.session); live {
		t.Fatal("single-entry fallback retained a one-shot claim")
	}
}

func TestRunnerLedgerConsumerProductionGraphHasOnlyReviewedWriterAndNoExternalEdge(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "runner_ledger_consumer_service.go", nil, parser.ImportsOnly)
	if err != nil {
		t.Fatal(err)
	}
	for _, imported := range file.Imports {
		path := strings.Trim(imported.Path.Value, `"`)
		if path == "database/sql" || path == "net/http" || strings.Contains(path, "pgx") {
			t.Fatalf("consumer service imports forbidden package %q", path)
		}
	}
	file, err = parser.ParseFile(token.NewFileSet(), "runner_ledger_consumer_service.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	forbidden := map[string]bool{
		"BeginMigration": true, "ExecuteStatement": true, "Commit": true, "Append": true, "AppendDurable": true,
		"Insert": true, "prepareCurrentDatabaseSession": true, "prepareCurrentTransaction": true,
		"prepareCurrentStatement": true, "appendCurrentStatementIntent": true, "runCurrentSingleEntry": true,
		"ReserveAndActivateSuccessor": true, "transition": true,
	}
	successCalls := 0
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		name := ""
		switch function := call.Fun.(type) {
		case *ast.Ident:
			name = function.Name
		case *ast.SelectorExpr:
			name = function.Sel.Name
		}
		if forbidden[name] {
			t.Fatalf("consumer service acquired forbidden edge %s", name)
		}
		if name == "executeRunnerLedgerEntrySuccess" {
			successCalls++
		}
		return true
	})
	if successCalls != 1 {
		t.Fatalf("consumer success-kernel calls=%d want=1", successCalls)
	}

	entries, err := migrationProductionGoFiles()
	if err != nil {
		t.Fatal(err)
	}
	serviceCalls, factCalls := 0, 0
	for name, production := range entries {
		ast.Inspect(production, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			switch function := call.Fun.(type) {
			case *ast.Ident:
				if function.Name == "bindRunnerLedgerConsumerFact" {
					factCalls++
					if name != "runner_ledger_consumer_service.go" {
						t.Fatalf("consumer fact caller in %s", name)
					}
				}
			case *ast.SelectorExpr:
				if function.Sel.Name == "consumeRunnerLedgerPreflight" {
					serviceCalls++
					if name != "runner.go" {
						t.Fatalf("consumer service caller in %s", name)
					}
				}
			}
			return true
		})
	}
	if serviceCalls != 2 || factCalls != 1 {
		// Runner.Run contains the two ADR-approved branches: wider current
		// scope, and prior writer preflight reporting non-empty/complete.
		t.Fatalf("production consumer graph service=%d fact=%d", serviceCalls, factCalls)
	}
}

func migrationProductionGoFiles() (map[string]*ast.File, error) {
	packages, err := parser.ParseDir(token.NewFileSet(), ".", func(info os.FileInfo) bool {
		return strings.HasSuffix(info.Name(), ".go") && !strings.HasSuffix(info.Name(), "_test.go")
	}, 0)
	if err != nil {
		return nil, err
	}
	files := map[string]*ast.File{}
	for _, pkg := range packages {
		for name, file := range pkg.Files {
			files[filepath.Base(name)] = file
		}
	}
	return files, nil
}
