package migration

import (
	"context"
	"crypto/sha256"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestRunnerDurableFinalIntermediateConsumesPreledgerAndAppendsExactEvidenceOnce(t *testing.T) {
	fixture, preledger, runner := newRunnerFinalIntermediateFixture(t)
	transaction := fixture.database.transaction
	beforeBoundary := transaction.boundaryCalls
	want := runnerIntermediateRequestFromPreledger(preledger)
	durable, err := runner.appendCurrentFinalIntermediate(context.Background(), preledger)
	if err != nil || !validRunnerDurableFinalIntermediate(durable) {
		t.Fatalf("append final intermediate: durable=%+v err=%v", durable, err)
	}
	if validRunnerProjectedCurrentPreledger(preledger) || liveRunnerProjectedCurrentPreledgers() != 0 || liveRunnerDurableFinalIntermediates() != 1 {
		t.Fatalf("pre-ledger authority was not atomically consumed: preledger=%t live=%d/%d", validRunnerProjectedCurrentPreledger(preledger), liveRunnerProjectedCurrentPreledgers(), liveRunnerDurableFinalIntermediates())
	}
	if fixture.evidence.intermediateBindCalls != 1 || fixture.evidence.journal.appendCalls != 2 || fixture.evidence.journal.appendedRecord.Intermediate == nil || !canonicalEqual(*fixture.evidence.journal.appendedRecord.Intermediate, durable.intermediate) {
		t.Fatalf("exact intermediate was not bound and appended once: evidence=%+v journal=%+v", fixture.evidence, fixture.evidence.journal)
	}
	if !canonicalEqual(durable.intermediate.State, want.state) || durable.intermediate.PreledgerAuthorityResult == nil || durable.intermediate.PreledgerCatalogResult == nil || !projectionEvidenceEqual(*durable.intermediate.PreledgerAuthorityResult, want.preledgerAuthority) || !projectionEvidenceEqual(*durable.intermediate.PreledgerCatalogResult, want.preledgerCatalog) {
		t.Fatalf("durable final intermediate differs from pre-ledger authority: %+v", durable.intermediate)
	}
	if fixture.evidence.snapshot.state != RecoveryDanglingIntermediate || fixture.evidence.snapshot.nextPermittedAction != recoveryAbortAction(durable.intent.AttemptIndex, durable.maxAttempts) || fixture.evidence.snapshot.lastIntermediateEvidence == nil || fixture.evidence.snapshot.lastIntermediateEvidenceRecordDigest == nil || *fixture.evidence.snapshot.lastIntermediateEvidenceRecordDigest != durable.intermediateRecordDigest || fixture.evidence.snapshot.lastIntermediateStateDigest == nil || *fixture.evidence.snapshot.lastIntermediateStateDigest != durable.intermediate.State.IntermediateStateDigest {
		t.Fatalf("durable recovery state is not exact dangling intermediate: %+v", fixture.evidence.snapshot)
	}
	if transaction.executeCalls != 1 || transaction.boundaryCalls != beforeBoundary+1 || transaction.rollbackCalls != 0 || transaction.commitCalls != 0 || transaction.execCalls != 0 || fixture.database.backend.ledgerInsertCalls != 0 || fixture.database.backend.commitCalls != 0 || transaction.status != 'T' {
		t.Fatalf("final intermediate crossed ledger/commit boundary: transaction=%+v backend=%+v", transaction, fixture.database.backend)
	}
	if replay, replayErr := runner.appendCurrentFinalIntermediate(context.Background(), preledger); replay != nil || !IsCode(replayErr, CodeTransactionBoundary) || fixture.evidence.journal.appendCalls != 2 || !validRunnerDurableFinalIntermediate(durable) {
		t.Fatalf("consumed pre-ledger replayed append or damaged successor: replay=%+v err=%v", replay, replayErr)
	}
	if err := closeRunnerDurableFinalIntermediate(durable, nil); err != nil || transaction.rollbackCalls != 1 || transaction.status != 'I' || fixture.database.unlockCalls != 1 || fixture.database.closeCalls != 1 || liveRunnerDurableFinalIntermediates() != 0 {
		t.Fatalf("durable final intermediate close did not release database ownership: err=%v transaction=%+v database=%+v", err, transaction, fixture.database)
	}
	if err := closeRunnerEvidenceOwnership(fixture.evidence, fixture.candidate); err != nil {
		t.Fatal(err)
	}
}

func TestRunnerDurableFinalIntermediateRejectsLiteralCopyAndFieldDrift(t *testing.T) {
	fixture, preledger, runner := newRunnerFinalIntermediateFixture(t)
	durable, err := runner.appendCurrentFinalIntermediate(context.Background(), preledger)
	if err != nil || !validRunnerDurableFinalIntermediate(durable) {
		t.Fatalf("append final intermediate: durable=%+v err=%v", durable, err)
	}
	valueType := reflect.TypeOf(*durable)
	for index := 0; index < valueType.NumField(); index++ {
		if valueType.Field(index).PkgPath == "" {
			t.Fatalf("durable final intermediate field %s became public", valueType.Field(index).Name)
		}
	}
	copyValue := *durable
	if err := closeRunnerDurableFinalIntermediate(&copyValue, nil); !IsCode(err, CodeTransactionBoundary) || fixture.database.transaction.rollbackCalls != 0 || !validRunnerDurableFinalIntermediate(durable) {
		t.Fatalf("copy changed original authority: err=%v transaction=%+v", err, fixture.database.transaction)
	}
	if err := closeRunnerDurableFinalIntermediate(&runnerDurableFinalIntermediate{}, nil); !IsCode(err, CodeTransactionBoundary) || fixture.database.transaction.rollbackCalls != 0 {
		t.Fatalf("literal escaped: err=%v", err)
	}

	originalState := durable.intermediate.State.IntermediateStateDigest
	durable.intermediate.State.IntermediateStateDigest = testDigest("other-durable-intermediate")
	assertDurableFinalIntermediateDrift(t, durable)
	durable.intermediate.State.IntermediateStateDigest = originalState

	originalCatalog := durable.intermediate.PreledgerCatalogResult.Digest
	durable.intermediate.PreledgerCatalogResult.Digest = testDigest("other-preledger-catalog")
	assertDurableFinalIntermediateDrift(t, durable)
	durable.intermediate.PreledgerCatalogResult.Digest = originalCatalog

	originalPlanDigest := durable.dispatch.planDigest
	durable.dispatch.planDigest = sha256.Sum256([]byte("other-final-intermediate-dispatch"))
	assertDurableFinalIntermediateDrift(t, durable)
	durable.dispatch.planDigest = originalPlanDigest

	originalEvidenceState := fixture.evidence.snapshot.state
	fixture.evidence.snapshot.state = RecoveryDivergent
	assertDurableFinalIntermediateDrift(t, durable)
	fixture.evidence.snapshot.state = originalEvidenceState

	fixture.database.transaction.status = 'E'
	assertDurableFinalIntermediateDrift(t, durable)
	fixture.database.transaction.status = 'T'
	if !validRunnerDurableFinalIntermediate(durable) {
		t.Fatal("restored durable final intermediate did not recover its immutable binding")
	}

	originalKey := durable.key
	originalTransaction := durable.transaction
	rogueTransaction := newRunnerPreflightTransaction(fixture.database)
	rogueTransaction.active = true
	rogueTransaction.status = 'T'
	durable.key++
	durable.transaction = rogueTransaction
	err = closeRunnerDurableFinalIntermediate(durable, nil)
	if !IsCode(err, CodeTransactionBoundary) || originalTransaction != fixture.database.transaction || fixture.database.transaction.rollbackCalls != 1 || rogueTransaction.rollbackCalls != 0 || !reflect.DeepEqual(fixture.database.unlockKeys, []int64{originalKey}) || fixture.database.closeCalls != 1 || liveRunnerDurableFinalIntermediates() != 0 {
		t.Fatalf("drifted close did not use registry ownership: err=%v transaction=%+v database=%+v", err, fixture.database.transaction, fixture.database)
	}
	if err := closeRunnerEvidenceOwnership(fixture.evidence, fixture.candidate); err != nil {
		t.Fatal(err)
	}
}

func TestRunnerFinalIntermediateRejectsUnavailableContextBeforeBinding(t *testing.T) {
	for _, test := range []struct {
		name      string
		context   func() context.Context
		nilRunner bool
		wantCode  ErrorCode
	}{
		{"nil-context", func() context.Context { return nil }, false, CodeTransactionBoundary},
		{"canceled", func() context.Context { ctx, cancel := context.WithCancel(context.Background()); cancel(); return ctx }, false, CodeContextCanceled},
		{"deadline", func() context.Context {
			ctx, cancel := context.WithDeadline(context.Background(), testExpiredTime())
			defer cancel()
			return ctx
		}, false, CodeDeadlineExceeded},
		{"nil-runner", func() context.Context { return context.Background() }, true, CodeTransactionBoundary},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture, preledger, runner := newRunnerFinalIntermediateFixture(t)
			active := runner
			if test.nilRunner {
				active = nil
			}
			durable, err := active.appendCurrentFinalIntermediate(test.context(), preledger)
			assertRunnerFinalIntermediateError(t, err, test.wantCode, "runner-final-intermediate")
			if durable != nil || fixture.evidence.intermediateBindCalls != 0 || fixture.evidence.journal.appendCalls != 1 || fixture.database.transaction.rollbackCalls != 1 || fixture.database.unlockCalls != 1 || fixture.database.closeCalls != 1 || fixture.database.backend.ledgerInsertCalls != 0 || liveRunnerProjectedCurrentPreledgers() != 0 || liveRunnerDurableFinalIntermediates() != 0 {
				t.Fatalf("unavailable context crossed intermediate boundary: durable=%+v err=%v transaction=%+v", durable, err, fixture.database.transaction)
			}
			if !fixture.evidence.journal.cursor.Valid() {
				t.Fatal("pre-append failure revoked the durable intent cursor")
			}
			if cleanupErr := closeRunnerEvidenceOwnership(fixture.evidence, fixture.candidate); cleanupErr != nil {
				t.Fatal(cleanupErr)
			}
		})
	}
}

func TestRunnerFinalIntermediateFaultsRollbackWithoutLedgerOrCommit(t *testing.T) {
	postMutationRevoked := map[string]bool{
		"append-unknown": true, "append-unknown-error": true, "durable-with-error": true,
		"result-sequence": true, "result-record": true, "result-checkpoint": true, "result-rotation": true, "result-cursor": true,
		"recovery-state": true, "recovery-action": true, "recovery-record": true, "recovery-body": true, "recovery-digest": true,
		"evidence-drift": true,
	}
	tests := []struct {
		name       string
		configure  func(*runnerPreparedCurrentSessionFixture)
		wantCode   ErrorCode
		wantOp     string
		wantAppend int
	}{
		{"bind-error", func(f *runnerPreparedCurrentSessionFixture) {
			f.evidence.intermediateBindErr = errors.New("secret-bind")
		}, CodeEvidenceJournalFailed, "runner-final-intermediate-bind", 1},
		{"bind-stable", func(f *runnerPreparedCurrentSessionFixture) {
			f.evidence.intermediateBindErr = fail(CodeEvidenceRecoveryRequired, "fake", "secret", errors.New("secret-bind"))
		}, CodeEvidenceRecoveryRequired, "runner-final-intermediate-bind", 1},
		{"bind-canceled", func(f *runnerPreparedCurrentSessionFixture) { f.evidence.intermediateBindErr = context.Canceled }, CodeContextCanceled, "runner-final-intermediate-bind", 1},
		{"bind-deadline", func(f *runnerPreparedCurrentSessionFixture) {
			f.evidence.intermediateBindErr = context.DeadlineExceeded
		}, CodeDeadlineExceeded, "runner-final-intermediate-bind", 1},
		{"bind-missing-journal", func(f *runnerPreparedCurrentSessionFixture) { f.evidence.intermediateNoJournal = true }, CodeEvidenceRecoveryRequired, "runner-final-intermediate-bind", 1},
		{"bind-missing-record", func(f *runnerPreparedCurrentSessionFixture) { f.evidence.intermediateNoRecord = true }, CodeEvidenceRecoveryRequired, "runner-final-intermediate-bind", 1},
		{"bind-invalid-record", func(f *runnerPreparedCurrentSessionFixture) {
			f.evidence.mutateBoundIntermediate = func(value *StatementIntermediateEvidence) { value.State.StatementIndex++ }
		}, CodeEvidenceJournalFailed, "runner-final-intermediate-bind", 1},
		{"bind-cursor-swap", func(f *runnerPreparedCurrentSessionFixture) {
			f.evidence.mutateIntermediateAuthority = func(cursor *JournalCursor, _ *OwnedEvidenceRecord) { cursor.nextSequence++ }
		}, CodeEvidenceJournalFailed, "runner-final-intermediate-bind", 1},
		{"bind-consumed-record", func(f *runnerPreparedCurrentSessionFixture) {
			f.evidence.mutateIntermediateAuthority = func(_ *JournalCursor, owned *OwnedEvidenceRecord) { owned.consumed.Store(true) }
		}, CodeEvidenceJournalFailed, "runner-final-intermediate-bind", 1},
		{"append-error", func(f *runnerPreparedCurrentSessionFixture) {
			f.evidence.journal.appendErr = errors.New("secret-append")
		}, CodeEvidenceJournalFailed, "runner-final-intermediate-append", 2},
		{"append-canceled", func(f *runnerPreparedCurrentSessionFixture) { f.evidence.journal.appendErr = context.Canceled }, CodeContextCanceled, "runner-final-intermediate-append", 2},
		{"append-deadline", func(f *runnerPreparedCurrentSessionFixture) { f.evidence.journal.appendErr = context.DeadlineExceeded }, CodeDeadlineExceeded, "runner-final-intermediate-append", 2},
		{"append-unknown", func(f *runnerPreparedCurrentSessionFixture) { f.evidence.journal.appendOutcome = appendOutcomeUnknown }, CodeEvidenceJournalFailed, "runner-final-intermediate-append", 2},
		{"append-unknown-error", func(f *runnerPreparedCurrentSessionFixture) {
			f.evidence.journal.appendOutcome = appendOutcomeUnknown
			f.evidence.journal.appendErr = errors.New("secret-unknown")
			f.evidence.journal.appendValuesWithError = true
		}, CodeEvidenceJournalFailed, "runner-final-intermediate-append", 2},
		{"durable-with-error", func(f *runnerPreparedCurrentSessionFixture) {
			f.evidence.journal.appendErr = errors.New("secret-durable")
			f.evidence.journal.appendValuesWithError = true
		}, CodeEvidenceJournalFailed, "runner-final-intermediate-append", 2},
		{"result-sequence", func(f *runnerPreparedCurrentSessionFixture) {
			f.evidence.journal.mutateAppendResult = func(result *AppendResult) { result.candidateSequence++ }
		}, CodeEvidenceJournalFailed, "runner-final-intermediate-append", 2},
		{"result-record", func(f *runnerPreparedCurrentSessionFixture) {
			f.evidence.journal.mutateAppendResult = func(result *AppendResult) { result.candidateRecordDigest = testDigest("other-intermediate") }
		}, CodeEvidenceJournalFailed, "runner-final-intermediate-append", 2},
		{"result-checkpoint", func(f *runnerPreparedCurrentSessionFixture) {
			f.evidence.journal.mutateAppendResult = func(result *AppendResult) { result.candidateCheckpointRecordDigest = Digest("invalid") }
		}, CodeEvidenceJournalFailed, "runner-final-intermediate-append", 2},
		{"result-rotation", func(f *runnerPreparedCurrentSessionFixture) {
			f.evidence.journal.mutateAppendResult = func(result *AppendResult) {
				result.rotationHeaderRecordDigest = digestPointer(testDigest("rotation"))
				result.rotationHeaderCheckpointRecordDigest = digestPointer(testDigest("rotation-checkpoint"))
			}
		}, CodeEvidenceJournalFailed, "runner-final-intermediate-append", 2},
		{"result-cursor", func(f *runnerPreparedCurrentSessionFixture) {
			f.evidence.journal.mutateAppendResult = func(result *AppendResult) { result.durableCursor.nextSequence++ }
		}, CodeEvidenceJournalFailed, "runner-final-intermediate-append", 2},
		{"recovery-state", func(f *runnerPreparedCurrentSessionFixture) {
			f.evidence.journal.mutateAppendSnapshot = func(snapshot *RecoverySnapshot) { snapshot.state = RecoveryDivergent }
		}, CodeEvidenceJournalFailed, "runner-final-intermediate-evidence", 2},
		{"recovery-action", func(f *runnerPreparedCurrentSessionFixture) {
			f.evidence.journal.mutateAppendSnapshot = func(snapshot *RecoverySnapshot) { snapshot.nextPermittedAction = RecoveryReturnFailure }
		}, CodeEvidenceJournalFailed, "runner-final-intermediate-evidence", 2},
		{"recovery-record", func(f *runnerPreparedCurrentSessionFixture) {
			f.evidence.journal.mutateAppendSnapshot = func(snapshot *RecoverySnapshot) { snapshot.lastIntermediateEvidence = nil }
		}, CodeEvidenceJournalFailed, "runner-final-intermediate-evidence", 2},
		{"recovery-body", func(f *runnerPreparedCurrentSessionFixture) {
			f.evidence.journal.mutateAppendSnapshot = func(snapshot *RecoverySnapshot) { snapshot.lastIntermediateEvidence.value.State.StatementIndex++ }
		}, CodeEvidenceJournalFailed, "runner-final-intermediate-evidence", 2},
		{"recovery-digest", func(f *runnerPreparedCurrentSessionFixture) {
			f.evidence.journal.mutateAppendSnapshot = func(snapshot *RecoverySnapshot) {
				snapshot.lastIntermediateStateDigest = digestPointer(testDigest("other-state"))
			}
		}, CodeEvidenceJournalFailed, "runner-final-intermediate-evidence", 2},
		{"boundary", func(f *runnerPreparedCurrentSessionFixture) {
			f.database.transaction.boundaryErr = errors.New("secret-boundary")
		}, CodeTransactionBoundary, "runner-final-intermediate-boundary", 2},
		{"boundary-role", func(f *runnerPreparedCurrentSessionFixture) {
			f.database.transaction.boundaryMutate = func(boundary *BoundaryState) { boundary.CurrentUser = "other_role" }
		}, CodeTransactionBoundary, "runner-final-intermediate-boundary", 2},
		{"status-drift", func(f *runnerPreparedCurrentSessionFixture) {
			f.database.transaction.boundaryMutate = func(*BoundaryState) { f.database.transaction.status = 'E' }
		}, CodeTransactionBoundary, "runner-final-intermediate-boundary", 2},
		{"evidence-drift", func(f *runnerPreparedCurrentSessionFixture) {
			f.database.transaction.boundaryMutate = func(*BoundaryState) { f.evidence.snapshot.state = RecoveryDivergent }
		}, CodeEvidenceJournalFailed, "runner-final-intermediate-seal", 2},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture, preledger, runner := newRunnerFinalIntermediateFixture(t)
			test.configure(&fixture)
			durable, err := runner.appendCurrentFinalIntermediate(context.Background(), preledger)
			assertRunnerFinalIntermediateError(t, err, test.wantCode, test.wantOp)
			if durable != nil || containsErrorText(err, "secret-") || fixture.evidence.intermediateBindCalls != 1 || fixture.evidence.journal.appendCalls != test.wantAppend || fixture.database.transaction.executeCalls != 1 || fixture.database.transaction.rollbackCalls != 1 || fixture.database.transaction.commitCalls != 0 || fixture.database.transaction.execCalls != 0 || fixture.database.unlockCalls != 1 || fixture.database.closeCalls != 1 || fixture.database.backend.ledgerInsertCalls != 0 || fixture.database.backend.commitCalls != 0 || liveRunnerProjectedCurrentPreledgers() != 0 || liveRunnerDurableFinalIntermediates() != 0 {
				t.Fatalf("final intermediate fault escaped boundary: durable=%+v err=%v transaction=%+v database=%+v", durable, err, fixture.database.transaction, fixture.database)
			}
			wantCursor := true
			if postMutationRevoked[test.name] {
				wantCursor = false
			}
			if fixture.evidence.journal.cursor.Valid() != wantCursor {
				t.Fatalf("final intermediate fault cursor validity=%t want=%t", fixture.evidence.journal.cursor.Valid(), wantCursor)
			}
			if cleanupErr := closeRunnerEvidenceOwnership(fixture.evidence, fixture.candidate); cleanupErr != nil {
				t.Fatal(cleanupErr)
			}
		})
	}
}

func TestRunnerFinalIntermediateCleanupFailureDominatesAndAttemptsEveryRelease(t *testing.T) {
	fixture, preledger, runner := newRunnerFinalIntermediateFixture(t)
	fixture.evidence.intermediateBindErr = errors.New("secret-bind")
	fixture.database.transaction.rollbackErr = errors.New("secret-rollback")
	fixture.database.transaction.rollbackLeavesOpen = true
	fixture.database.unlockErr = errors.New("secret-unlock")
	fixture.database.closeErr = errors.New("secret-close")
	durable, err := runner.appendCurrentFinalIntermediate(context.Background(), preledger)
	assertRunnerFinalIntermediateError(t, err, CodeTransactionBoundary, "runner-database-close")
	if durable != nil || containsErrorText(err, "secret-") || fixture.database.transaction.rollbackCalls != 1 || fixture.database.unlockCalls != 1 || fixture.database.closeCalls != 1 || fixture.evidence.journal.appendCalls != 1 || fixture.database.backend.ledgerInsertCalls != 0 || liveRunnerDurableFinalIntermediates() != 0 {
		t.Fatalf("final intermediate cleanup precedence/attempts: durable=%+v err=%v transaction=%+v database=%+v", durable, err, fixture.database.transaction, fixture.database)
	}
	if cleanupErr := closeRunnerEvidenceOwnership(fixture.evidence, fixture.candidate); cleanupErr != nil {
		t.Fatal(cleanupErr)
	}
}

func TestRunnerFinalIntermediateHasOneEvidenceAppendAndNoSQLLedgerOrCommitEdge(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "runner_final_intermediate.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	appendCalls, boundaryCalls := 0, 0
	forbidden := map[string]bool{
		"ExecuteStatement": true, "BeginMigration": true, "ReserveAndActivateSuccessor": true,
		"Insert": true, "Commit": true, "Exec": true, "Query": true, "QueryRow": true,
		"ProjectAuthority": true, "ProjectCatalog": true, "ProjectTransitionState": true, "ProjectPrecondition": true,
	}
	ast.Inspect(file, func(node ast.Node) bool {
		selector, ok := node.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		switch selector.Sel.Name {
		case "AppendDurable":
			appendCalls++
		case "Boundary":
			boundaryCalls++
		}
		if forbidden[selector.Sel.Name] {
			t.Fatalf("final intermediate acquired forbidden %s call edge", selector.Sel.Name)
		}
		return true
	})
	if appendCalls != 1 || boundaryCalls != 1 {
		t.Fatalf("final intermediate call edges: append=%d boundary=%d", appendCalls, boundaryCalls)
	}
}

func TestRunnerDurableFinalIntermediateHasNoProductionConsumer(t *testing.T) {
	paths, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	symbols := map[string]bool{
		"runnerDurableFinalIntermediate": true, "runnerDurableFinalIntermediateBinding": true,
		"runnerDurableFinalIntermediateRegistryRecord": true, "runnerDurableFinalIntermediateRegistry": true,
		"runnerFinalIntermediateSeed": true, "appendCurrentFinalIntermediate": true,
		"consumeRunnerProjectedCurrentPreledger": true, "bindRunnerDurableFinalIntermediate": true,
		"validRunnerDurableFinalIntermediate": true, "closeRunnerDurableFinalIntermediate": true,
	}
	for _, path := range paths {
		name := filepath.Base(path)
		if strings.HasSuffix(name, "_test.go") || name == "runner_final_intermediate.go" {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			identifier, ok := node.(*ast.Ident)
			if ok && symbols[identifier.Name] {
				t.Fatalf("durable final intermediate %s acquired unreviewed production consumer %s", identifier.Name, name)
			}
			return true
		})
	}
}

func newRunnerFinalIntermediateFixture(t *testing.T) (runnerPreparedCurrentSessionFixture, *runnerProjectedCurrentPreledger, *Runner) {
	t.Helper()
	fixture, after, runner, _ := newRunnerPreledgerFixture(t)
	preledger, err := runner.projectCurrentPreledger(context.Background(), after)
	if err != nil || !validRunnerProjectedCurrentPreledger(preledger) {
		t.Fatalf("final intermediate fixture pre-ledger: preledger=%+v err=%v", preledger, err)
	}
	return fixture, preledger, runner
}

func assertDurableFinalIntermediateDrift(t *testing.T, durable *runnerDurableFinalIntermediate) {
	t.Helper()
	if validRunnerDurableFinalIntermediate(durable) {
		t.Fatal("mutated durable final intermediate remained valid")
	}
}

func liveRunnerDurableFinalIntermediates() int {
	count := 0
	runnerDurableFinalIntermediateRegistry.Range(func(_, _ any) bool {
		count++
		return true
	})
	return count
}

func assertRunnerFinalIntermediateError(t *testing.T, err error, code ErrorCode, op string) {
	t.Helper()
	var migrationErr *Error
	if !errors.As(err, &migrationErr) || migrationErr.Code != code || migrationErr.Op != op || migrationErr.Err != nil {
		t.Fatalf("final intermediate error: got=%#v want=%s/%s", migrationErr, code, op)
	}
}
