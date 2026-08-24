package migration

import (
	"context"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"sync/atomic"
	"testing"
)

var _ runnerLedgerAbortTerminalRecordBinder = (*generationEvidenceSession)(nil)
var _ runnerLedgerAbortTerminalRecordBinder = (*runnerLedgerPreflightEvidenceFake)(nil)

func (*runnerLedgerPreflightEvidenceFake) runnerLedgerAbortTerminalRecordBinderSealed() {}

func (evidence *runnerLedgerPreflightEvidenceFake) bindRunnerLedgerRecoveryAbortTerminalRecord(ctx context.Context, permit *runnerLedgerAbortTerminalWriterPermit) (EvidenceJournal, JournalCursor, *OwnedEvidenceRecord, error) {
	evidence.terminalBindCalls++
	claimed, err := consumeRunnerLedgerAbortTerminalWriterPermit(permit, evidence)
	if err != nil {
		return nil, JournalCursor{}, nil, err
	}
	if err := contextAdmissionError(ctx); err != nil {
		return nil, JournalCursor{}, nil, err
	}
	if evidence.terminalBindErr != nil {
		return nil, JournalCursor{}, nil, evidence.terminalBindErr
	}
	base := evidence.runnerEvidenceSessionFake
	if base == nil || base.closed || base.journal == nil || base.snapshot == nil ||
		claimed.candidateBinding != base.candidate.binding || !sameGenerationIdentity(claimed.generation, base.active.identity) ||
		generationJournalRecoveryDigest(base.snapshot) != claimed.recoveryDigest || !sameCursorIdentity(claimed.cursor, base.journal.cursor) {
		return nil, JournalCursor{}, nil, fail(CodeEvidenceRecoveryRequired, "runner-ledger-recovery-abort-terminal-test-bind", "test evidence boundary changed", nil)
	}
	if evidence.terminalNoJournal {
		return nil, base.journal.cursor.clone(), nil, nil
	}
	cursor := base.journal.cursor.clone()
	terminal := cloneProjectionValue(claimed.terminal)
	if evidence.mutateBoundTerminal != nil {
		evidence.mutateBoundTerminal(&terminal)
	}
	witness := runnerLedgerEntrySuccessFakeWitness{
		recordKind: EvidenceRecordAttemptTerminal, generation: claimed.generation, cursor: cursor.clone(),
	}
	owned := &OwnedEvidenceRecord{
		wire: EvidenceRecord{AttemptTerminal: cloneAttemptTerminalPointer(&terminal)}, witness: witness,
		generation: claimed.generation, cursor: cursor.clone(), consumed: &atomic.Bool{},
	}
	if evidence.terminalNoRecord {
		owned = nil
	}
	if evidence.mutateTerminalAuthority != nil && owned != nil {
		evidence.mutateTerminalAuthority(&cursor, owned)
	}
	base.journal.maxAttempts = claimed.selection.maxAttempts
	return base.journal, cursor, owned, nil
}

type runnerLedgerRecoveryAbortTerminalFixture struct {
	success      *runnerLedgerEntrySuccessFixture
	fact         runnerLedgerConsumerFact
	database     *runnerPreflightSession
	connector    *runnerPreflightConnector
	state        RecoveryState
	action       RecoveryAction
	beforeCursor JournalCursor
}

func newRunnerLedgerRecoveryAbortTerminalFixture(t *testing.T, state RecoveryState, action RecoveryAction) *runnerLedgerRecoveryAbortTerminalFixture {
	t.Helper()
	raw, decision := buildExactTwoMigrationAdmissionRuntime(t)
	success := newRunnerLedgerEntrySuccessFixture(
		t, raw, decision, runnerLedgerPreflightPartialNextEntry, RecoveryTerminal, RecoveryBeginFirstAttemptNextEntry,
	)
	executionPermit := success.prepare(t)
	configureRunnerLedgerEntrySuccessExecution(t, success, executionPermit)
	runner := success.execution.base.service.kernel.runner
	base := success.execution.base.service.kernel.base
	writer, err := runner.prepareRunnerLedgerEntrySuccess(context.Background(), executionPermit, base.bundle, base.plans)
	if err == nil {
		writer, err = runner.beginRunnerLedgerEntrySuccessTransaction(context.Background(), writer)
	}
	if err == nil {
		writer, err = runner.prepareRunnerLedgerEntrySuccessStatement(context.Background(), writer)
	}
	if err == nil {
		writer, err = runner.appendRunnerLedgerEntrySuccessIntent(context.Background(), writer)
	}
	if err == nil && state == RecoveryDanglingIntermediate {
		writer, err = runner.executeRunnerLedgerEntrySuccessStatement(context.Background(), writer)
		if err == nil {
			writer, err = runner.appendRunnerLedgerEntrySuccessIntermediate(context.Background(), writer)
		}
	}
	if err != nil || writer == nil {
		success.close(t)
		t.Fatalf("prepare durable %s boundary: writer=%+v err=%v", state, writer, err)
	}
	evidence := success.execution.base.service.evidence
	before := evidence.runnerEvidenceSessionFake.RecoverySnapshot()
	if before == nil || before.state != state {
		_ = closeRunnerLedgerEntrySuccessState(writer, nil)
		success.close(t)
		t.Fatalf("durable predecessor state=%v want=%v", stateOrEmpty(before), state)
	}
	if err := closeRunnerLedgerEntrySuccessState(writer, nil); err != nil {
		success.close(t)
		t.Fatal(err)
	}
	evidence.mu.Lock()
	recovery := cloneRecoverySnapshot(evidence.runnerEvidenceSessionFake.snapshot)
	recovery.nextPermittedAction = action
	evidence.runnerEvidenceSessionFake.snapshot = recovery
	evidence.runnerEvidenceSessionFake.journal.snapshot = recovery
	evidence.runnerEvidenceSessionFake.journal.cursor = recovery.cursor.clone()
	evidence.recovery = cloneRecoverySnapshot(recovery)
	evidence.mu.Unlock()

	rows := []LedgerRow{ledgerRowFor(base.bundle.Manifest.SchemaBundle.Migrations[0], base.bundle.Manifest.SchemaBundleDigest)}
	preflight := newRunnerPreflightSession()
	preflight.ledgerRowsByRead = [][]LedgerRow{cloneProjectionValue(rows), cloneProjectionValue(rows)}
	runner.Connector = &runnerPreflightConnector{session: preflight}
	claim, err := runner.prepareRunnerLedgerPreflightClaim(
		context.Background(), "test-only", base.bundle, base.plans, evidence, base.candidate,
	)
	if err != nil {
		success.close(t)
		t.Fatal(err)
	}
	defer revokeRunnerLedgerPreflightClaim(claim)
	dispatch, err := runner.claimRunnerLedgerPreflightDispatch(context.Background(), evidence, base.candidate, claim)
	if err != nil {
		success.close(t)
		t.Fatal(err)
	}
	fact, err := bindRunnerLedgerConsumerFact(generatedRunnerLedgerConsumerProfile, dispatch, base.bundle.Manifest.ManifestDigest)
	if err != nil || fact.action != runnerLedgerConsumerRecoveryNotImplemented {
		success.close(t)
		t.Fatalf("abort recovery fact=%+v err=%v", fact, err)
	}
	database := newRunnerPreflightSession()
	for index := 0; index < 6; index++ {
		database.ledgerRowsByRead = append(database.ledgerRowsByRead, cloneProjectionValue(rows))
	}
	connector := &runnerPreflightConnector{session: database}
	runner.Connector = connector
	journal := evidence.runnerEvidenceSessionFake.journal
	if journal.rotateAt == nil {
		journal.rotateAt = map[int]bool{}
	}
	if journal.appendOutcomeAt == nil {
		journal.appendOutcomeAt = map[int]appendOutcome{}
	}
	if journal.appendErrAt == nil {
		journal.appendErrAt = map[int]error{}
	}
	return &runnerLedgerRecoveryAbortTerminalFixture{
		success: success, fact: fact, database: database, connector: connector,
		state: state, action: action, beforeCursor: recovery.cursor.clone(),
	}
}

func stateOrEmpty(snapshot *RecoverySnapshot) RecoveryState {
	if snapshot == nil {
		return ""
	}
	return snapshot.state
}

func (fixture *runnerLedgerRecoveryAbortTerminalFixture) run(ctx context.Context) error {
	base := fixture.success.execution.base.service.kernel.base
	return fixture.success.execution.base.service.kernel.runner.admitRunnerLedgerRecoveryAction(
		ctx, "test-only", base.bundle, base.plans, fixture.success.execution.base.service.evidence, base.candidate, fixture.fact,
	)
}

func (fixture *runnerLedgerRecoveryAbortTerminalFixture) prepareWriterPermit(t *testing.T) *runnerLedgerAbortTerminalWriterPermit {
	t.Helper()
	base := fixture.success.execution.base.service.kernel.base
	runner := fixture.success.execution.base.service.kernel.runner
	owner, err := runner.prepareRunnerLedgerRecoveryAdmission(
		context.Background(), "test-only", base.bundle, base.plans,
		fixture.success.execution.base.service.evidence, base.candidate, fixture.fact,
	)
	if err != nil {
		t.Fatal(err)
	}
	admission, ok := owner.(*runnerLedgerAbortTerminalAdmissionPermit)
	if !ok {
		t.Fatalf("abort admission type=%T", owner)
	}
	seed, err := claimRunnerLedgerAbortTerminalAdmissionPermit(admission)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := runner.revalidateAndCloseRunnerLedgerAbortTerminalAdmission(context.Background(), seed, base.bundle)
	if err != nil {
		t.Fatal(err)
	}
	permit, err := mintRunnerLedgerAbortTerminalWriterPermit(seed, receipt)
	if err != nil || !validRunnerLedgerAbortTerminalWriterPermit(permit) {
		t.Fatalf("writer permit=%+v err=%v", permit, err)
	}
	return permit
}

func (fixture *runnerLedgerRecoveryAbortTerminalFixture) close(t *testing.T) {
	t.Helper()
	if fixture == nil {
		return
	}
	if fixture.database != nil && !fixture.database.closed {
		if err := closeRunnerDatabasePreflight(fixture.database, fixture.success.execution.base.service.kernel.base.key, fixture.database.locked, nil); err != nil {
			t.Fatal(err)
		}
	}
	fixture.success.close(t)
}

func TestRunnerLedgerRecoveryAbortTerminalAppendsExactlyFourGeneratedPairs(t *testing.T) {
	tests := []struct {
		state       RecoveryState
		action      RecoveryAction
		wantOutcome string
		wantNext    RecoveryAction
		rotate      bool
	}{
		{RecoveryDanglingStatementIntent, RecoveryAppendAbortedRetryable, "aborted_retryable", RecoveryBeginNextAttempt, false},
		{RecoveryDanglingStatementIntent, RecoveryAppendAbortedTerminal, "aborted_terminal", RecoveryReturnFailure, true},
		{RecoveryDanglingIntermediate, RecoveryAppendAbortedRetryable, "aborted_retryable", RecoveryBeginNextAttempt, true},
		{RecoveryDanglingIntermediate, RecoveryAppendAbortedTerminal, "aborted_terminal", RecoveryReturnFailure, false},
	}
	for _, test := range tests {
		name := fmt.Sprintf("%s/%s", test.state, test.action)
		t.Run(name, func(t *testing.T) {
			fixture := newRunnerLedgerRecoveryAbortTerminalFixture(t, test.state, test.action)
			defer fixture.close(t)
			journal := fixture.success.execution.base.service.evidence.runnerEvidenceSessionFake.journal
			if test.rotate {
				journal.rotateAt[journal.appendCalls+1] = true
			}
			if err := fixture.run(context.Background()); err != nil {
				t.Fatal(err)
			}
			snapshot := fixture.success.execution.base.service.evidence.RecoverySnapshot()
			terminal := journal.appendedRecord.AttemptTerminal
			if terminal == nil || terminal.Outcome != test.wantOutcome || terminal.RetryProof == nil ||
				terminal.RetryProof.ProofKind != "precommit_connection_terminated_exact_predecessor" ||
				snapshot == nil || snapshot.state != RecoveryTerminal || snapshot.nextPermittedAction != test.wantNext ||
				snapshot.lastTerminal == nil || !runnerCanonicalEqual(snapshot.lastTerminal.value, *terminal) ||
				fixture.beforeCursor.Valid() || !snapshot.cursor.Valid() {
				t.Fatalf("terminal=%+v snapshot=%+v", terminal, snapshot)
			}
			if (test.state == RecoveryDanglingIntermediate) != (terminal.LastIntermediateStateDigest != nil) ||
				fixture.database.ledgerReadCalls != 6 || fixture.database.beginCalls != 0 ||
				fixture.database.backend.executeCalls != 0 || fixture.database.backend.ledgerInsertCalls != 0 ||
				fixture.database.backend.commitCalls != 0 || fixture.database.unlockCalls != 1 || fixture.database.closeCalls != 1 ||
				!fixture.database.closed || fixture.database.locked {
				t.Fatalf("abort writer escaped boundary: terminal=%+v database=%+v", terminal, fixture.database)
			}
		})
	}
}

func TestRunnerLedgerRecoveryAbortTerminalUnknownAppendRequiresRecoveryAndRevokesCursor(t *testing.T) {
	fixture := newRunnerLedgerRecoveryAbortTerminalFixture(t, RecoveryDanglingStatementIntent, RecoveryAppendAbortedRetryable)
	defer fixture.close(t)
	journal := fixture.success.execution.base.service.evidence.runnerEvidenceSessionFake.journal
	journal.appendOutcomeAt[journal.appendCalls+1] = appendOutcomeUnknown
	err := fixture.run(context.Background())
	if !IsCode(err, CodeEvidenceRecoveryRequired) || fixture.beforeCursor.Valid() || !fixture.database.closed || journal.appendCalls < 2 {
		t.Fatalf("unknown append err=%v cursor=%v database=%+v", err, fixture.beforeCursor.Valid(), fixture.database)
	}
}

func TestRunnerLedgerRecoveryAbortTerminalFinalRevalidationAndCleanupDominate(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*runnerLedgerRecoveryAbortTerminalFixture)
		code   ErrorCode
	}{
		{"final-ledger-drift", func(f *runnerLedgerRecoveryAbortTerminalFixture) {
			row := cloneProjectionValue(f.database.ledgerRowsByRead[5][0])
			row.MigrationName += " drift"
			f.database.ledgerRowsByRead[5] = []LedgerRow{row}
		}, CodeInvalidLedger},
		{"final-close-uncertain", func(f *runnerLedgerRecoveryAbortTerminalFixture) {
			f.database.closeErr = errors.New("close uncertain")
		}, CodeTransactionBoundary},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newRunnerLedgerRecoveryAbortTerminalFixture(t, RecoveryDanglingIntermediate, RecoveryAppendAbortedTerminal)
			defer fixture.close(t)
			test.mutate(fixture)
			err := fixture.run(context.Background())
			journal := fixture.success.execution.base.service.evidence.runnerEvidenceSessionFake.journal
			if !IsCode(err, test.code) || journal.appendedRecord.AttemptTerminal != nil || fixture.database.closeCalls != 1 {
				t.Fatalf("err=%v journal=%+v database=%+v", err, journal.appendedRecord, fixture.database)
			}
			fixture.database.closeErr = nil
		})
	}
}

func TestRunnerLedgerRecoveryAbortTerminalWriterPermitIsOneShotAndNoncopyable(t *testing.T) {
	fixture := newRunnerLedgerRecoveryAbortTerminalFixture(t, RecoveryDanglingStatementIntent, RecoveryAppendAbortedRetryable)
	defer fixture.close(t)
	permit := fixture.prepareWriterPermit(t)
	evidence := fixture.success.execution.base.service.evidence

	copyPermit := *permit
	copyPermit.self = &copyPermit
	if _, _, _, err := evidence.bindRunnerLedgerRecoveryAbortTerminalRecord(context.Background(), &copyPermit); !IsCode(err, CodeEvidenceRecoveryRequired) {
		t.Fatalf("copied permit err=%v", err)
	}
	if !validRunnerLedgerAbortTerminalWriterPermit(permit) {
		t.Fatal("copied permit changed original registry authority")
	}
	journal, cursor, owned, err := evidence.bindRunnerLedgerRecoveryAbortTerminalRecord(context.Background(), permit)
	if err != nil || journal == nil || owned == nil || !sameCursorIdentity(cursor, permit.cursor) {
		t.Fatalf("original permit bind journal=%T cursor=%+v record=%+v err=%v", journal, cursor, owned, err)
	}
	if _, _, _, err := evidence.bindRunnerLedgerRecoveryAbortTerminalRecord(context.Background(), permit); !IsCode(err, CodeEvidenceRecoveryRequired) {
		t.Fatalf("second consume err=%v", err)
	}
	if _, _, _, err := evidence.bindRunnerLedgerRecoveryAbortTerminalRecord(context.Background(), &runnerLedgerAbortTerminalWriterPermit{}); !IsCode(err, CodeEvidenceRecoveryRequired) {
		t.Fatalf("literal consume err=%v", err)
	}
}

func TestRunnerLedgerRecoveryAbortTerminalWriterPermitBindsPlanAndDatabase(t *testing.T) {
	fixture := newRunnerLedgerRecoveryAbortTerminalFixture(t, RecoveryDanglingIntermediate, RecoveryAppendAbortedTerminal)
	defer fixture.close(t)
	permit := fixture.prepareWriterPermit(t)
	baseline := permit.canonical

	planDrift := *permit
	planDrift.self = &planDrift
	planDrift.selection.planDigest[0] ^= 0xff
	if digest := runnerLedgerAbortTerminalWriterPermitDigest(&planDrift); digest == ([32]byte{}) || digest == baseline {
		t.Fatalf("plan drift digest=%x baseline=%x", digest, baseline)
	}
	databaseDrift := *permit
	databaseDrift.self = &databaseDrift
	databaseDrift.database.databaseName += "_drift"
	if digest := runnerLedgerAbortTerminalWriterPermitDigest(&databaseDrift); digest == ([32]byte{}) || digest == baseline {
		t.Fatalf("database drift digest=%x baseline=%x", digest, baseline)
	}
	invalidDatabase := *permit
	invalidDatabase.self = &invalidDatabase
	invalidDatabase.database.serverVersionNum = 0
	if digest := runnerLedgerAbortTerminalWriterPermitDigest(&invalidDatabase); digest != ([32]byte{}) {
		t.Fatalf("invalid database digest=%x", digest)
	}
	runnerLedgerAbortTerminalWriterPermitRegistry.Delete(permit)
}

func TestRunnerLedgerRecoveryAbortTerminalBoundAuthorityAndDurableSnapshotFailClosed(t *testing.T) {
	tests := []struct {
		name      string
		state     RecoveryState
		configure func(*runnerLedgerRecoveryAbortTerminalFixture)
		wantCode  ErrorCode
	}{
		{"bind-raw-error", RecoveryDanglingStatementIntent, func(f *runnerLedgerRecoveryAbortTerminalFixture) {
			f.success.execution.base.service.evidence.terminalBindErr = errors.New("secret-bind")
		}, CodeEvidenceJournalFailed},
		{"bind-missing-record", RecoveryDanglingStatementIntent, func(f *runnerLedgerRecoveryAbortTerminalFixture) {
			f.success.execution.base.service.evidence.terminalNoRecord = true
		}, CodeEvidenceRecoveryRequired},
		{"bind-terminal-body", RecoveryDanglingIntermediate, func(f *runnerLedgerRecoveryAbortTerminalFixture) {
			f.success.execution.base.service.evidence.mutateBoundTerminal = func(terminal *AttemptTerminalState) {
				terminal.FailureEvidence.Path = "statement"
			}
		}, CodeEvidenceRecoveryRequired},
		{"bind-consumed-record", RecoveryDanglingStatementIntent, func(f *runnerLedgerRecoveryAbortTerminalFixture) {
			f.success.execution.base.service.evidence.mutateTerminalAuthority = func(_ *JournalCursor, owned *OwnedEvidenceRecord) {
				owned.consumed.Store(true)
			}
		}, CodeEvidenceRecoveryRequired},
		{"bind-foreign-cursor", RecoveryDanglingIntermediate, func(f *runnerLedgerRecoveryAbortTerminalFixture) {
			f.success.execution.base.service.evidence.mutateTerminalAuthority = func(cursor *JournalCursor, _ *OwnedEvidenceRecord) {
				cursor.nextSequence++
			}
		}, CodeEvidenceRecoveryRequired},
		{"append-before-mutation", RecoveryDanglingStatementIntent, func(f *runnerLedgerRecoveryAbortTerminalFixture) {
			journal := f.success.execution.base.service.evidence.runnerEvidenceSessionFake.journal
			journal.appendErrAt[journal.appendCalls+1] = errors.New("secret-append")
		}, CodeEvidenceJournalFailed},
		{"append-canceled-before-mutation", RecoveryDanglingIntermediate, func(f *runnerLedgerRecoveryAbortTerminalFixture) {
			journal := f.success.execution.base.service.evidence.runnerEvidenceSessionFake.journal
			journal.appendErrAt[journal.appendCalls+1] = context.Canceled
		}, CodeContextCanceled},
		{"durable-result", RecoveryDanglingStatementIntent, func(f *runnerLedgerRecoveryAbortTerminalFixture) {
			f.success.execution.base.service.evidence.runnerEvidenceSessionFake.journal.mutateAppendResult = func(result *AppendResult) {
				result.candidateRecordDigest = testDigest("other-terminal")
			}
		}, CodeEvidenceRecoveryRequired},
		{"durable-intent-prefix", RecoveryDanglingStatementIntent, func(f *runnerLedgerRecoveryAbortTerminalFixture) {
			f.success.execution.base.service.evidence.runnerEvidenceSessionFake.journal.mutateAppendSnapshot = func(snapshot *RecoverySnapshot) {
				snapshot.lastStatementIntentRecordDigest = digestPointer(testDigest("other-intent"))
			}
		}, CodeEvidenceRecoveryRequired},
		{"durable-intent-body", RecoveryDanglingStatementIntent, func(f *runnerLedgerRecoveryAbortTerminalFixture) {
			f.success.execution.base.service.evidence.runnerEvidenceSessionFake.journal.mutateAppendSnapshot = func(snapshot *RecoverySnapshot) {
				snapshot.lastStatementIntent.value.StatementIndex++
			}
		}, CodeEvidenceRecoveryRequired},
		{"durable-intermediate-prefix", RecoveryDanglingIntermediate, func(f *runnerLedgerRecoveryAbortTerminalFixture) {
			f.success.execution.base.service.evidence.runnerEvidenceSessionFake.journal.mutateAppendSnapshot = func(snapshot *RecoverySnapshot) {
				snapshot.lastIntermediateEvidenceRecordDigest = digestPointer(testDigest("other-intermediate"))
			}
		}, CodeEvidenceRecoveryRequired},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newRunnerLedgerRecoveryAbortTerminalFixture(t, test.state, RecoveryAppendAbortedTerminal)
			defer fixture.close(t)
			test.configure(fixture)
			err := fixture.run(context.Background())
			journal := fixture.success.execution.base.service.evidence.runnerEvidenceSessionFake.journal
			if !IsCode(err, test.wantCode) || containsErrorText(err, "secret-") || !fixture.database.closed || fixture.database.beginCalls != 0 ||
				fixture.database.backend.executeCalls != 0 || fixture.database.backend.ledgerInsertCalls != 0 || fixture.database.backend.commitCalls != 0 {
				t.Fatalf("err=%v database=%+v journal=%+v", err, fixture.database, journal)
			}
			if stringIn(test.name, "bind-terminal-body", "bind-consumed-record", "bind-foreign-cursor", "durable-result", "durable-intent-prefix", "durable-intent-body", "durable-intermediate-prefix") && fixture.beforeCursor.Valid() {
				t.Fatalf("%s retained old cursor", test.name)
			}
		})
	}
}

func TestRunnerLedgerRecoveryAbortTerminalRetryBudgetIsExact(t *testing.T) {
	fixture := newRunnerLedgerRecoveryAbortTerminalFixture(t, RecoveryDanglingStatementIntent, RecoveryAppendAbortedRetryable)
	defer fixture.close(t)
	permit := fixture.prepareWriterPermit(t)
	snapshot := fixture.success.execution.base.service.evidence.RecoverySnapshot()
	selection := permit.selection
	selection.maxAttempts = selection.attemptIndex
	if _, err := buildRunnerLedgerAbortTerminal(snapshot, permit.receipt, selection, permit.database.postgresMajor); !IsCode(err, CodeEvidenceRecoveryRequired) {
		t.Fatalf("retry at max attempt err=%v", err)
	}
	runnerLedgerAbortTerminalWriterPermitRegistry.Delete(permit)
}

func TestRunnerLedgerRecoveryAbortTerminalHasOneAppendAndNoDatabaseWriterEdge(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "runner_ledger_recovery_abort_terminal.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	appendCalls := 0
	forbidden := map[string]bool{
		"BeginMigration": true, "ExecuteStatement": true, "Commit": true, "Rollback": true,
		"Insert": true, "Exec": true, "Query": true, "QueryRow": true,
		"AppendGenerationSuperseded": true, "AppendGenerationReserved": true, "AppendGenerationActivated": true,
	}
	ast.Inspect(file, func(node ast.Node) bool {
		selector, ok := node.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if selector.Sel.Name == "AppendDurable" {
			appendCalls++
		}
		if forbidden[selector.Sel.Name] {
			t.Fatalf("abort-terminal writer acquired forbidden %s call edge", selector.Sel.Name)
		}
		return true
	})
	if appendCalls != 1 {
		t.Fatalf("abort-terminal append calls=%d want=1", appendCalls)
	}
}
