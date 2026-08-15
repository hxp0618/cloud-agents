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

func TestRunnerDurableCommitIntentConsumesLedgerAndAppendsExactEvidenceOnce(t *testing.T) {
	fixture, readback, runner := newRunnerCommitIntentFixture(t)
	transaction := fixture.database.transaction
	beforeBoundary := transaction.boundaryCalls
	want := runnerCommitIntentRequestFromReadback(readback)
	durable, err := runner.appendCurrentCommitIntent(context.Background(), readback)
	if err != nil || !validRunnerDurableCommitIntent(durable) {
		t.Fatalf("append commit intent: durable=%+v err=%v", durable, err)
	}
	if validRunnerReadbackCurrentLedger(readback) || liveRunnerReadbackCurrentLedgers() != 0 || liveRunnerDurableCommitIntents() != 1 {
		t.Fatalf("ledger authority was not atomically consumed: readback=%t live=%d/%d", validRunnerReadbackCurrentLedger(readback), liveRunnerReadbackCurrentLedgers(), liveRunnerDurableCommitIntents())
	}
	wantCommit, err := buildRunnerCommitIntent(want)
	if err != nil || fixture.evidence.commitBindCalls != 1 || fixture.evidence.journal.appendCalls != 3 || fixture.evidence.journal.appendedRecord.CommitIntent == nil || !canonicalEqual(*fixture.evidence.journal.appendedRecord.CommitIntent, wantCommit) || !canonicalEqual(durable.commit, wantCommit) {
		t.Fatalf("exact commit intent was not bound and appended once: durable=%+v evidence=%+v err=%v", durable, fixture.evidence, err)
	}
	if fixture.evidence.snapshot.state != RecoveryDanglingCommitIntent || fixture.evidence.snapshot.nextPermittedAction != RecoveryReconcileCommit || fixture.evidence.snapshot.commitIntent == nil || fixture.evidence.snapshot.lastCommitIntentRecordDigest == nil || *fixture.evidence.snapshot.lastCommitIntentRecordDigest != durable.commitRecordDigest || !canonicalEqual(fixture.evidence.snapshot.commitIntent.value, durable.commit) {
		t.Fatalf("durable recovery state is not exact dangling commit intent: %+v", fixture.evidence.snapshot)
	}
	if transaction.executeCalls != 1 || transaction.ledgerInsertCalls != 1 || transaction.ledgerReadCalls != 1 || transaction.boundaryCalls != beforeBoundary+1 || transaction.rollbackCalls != 0 || transaction.commitCalls != 0 || transaction.execCalls != 0 || fixture.database.backend.ledgerInsertCalls != 1 || fixture.database.backend.commitCalls != 0 || transaction.status != 'T' {
		t.Fatalf("commit intent crossed transaction commit boundary: transaction=%+v backend=%+v", transaction, fixture.database.backend)
	}
	if replay, replayErr := runner.appendCurrentCommitIntent(context.Background(), readback); replay != nil || !IsCode(replayErr, CodeTransactionBoundary) || fixture.evidence.journal.appendCalls != 3 || !validRunnerDurableCommitIntent(durable) {
		t.Fatalf("consumed ledger replayed append or damaged successor: replay=%+v err=%v", replay, replayErr)
	}
	if err := closeRunnerDurableCommitIntent(durable, nil); err != nil || transaction.rollbackCalls != 1 || transaction.pendingLedger != nil || transaction.status != 'I' || fixture.database.unlockCalls != 1 || fixture.database.closeCalls != 1 || liveRunnerDurableCommitIntents() != 0 {
		t.Fatalf("durable commit intent close did not release database ownership: err=%v transaction=%+v database=%+v", err, transaction, fixture.database)
	}
	if err := closeRunnerEvidenceOwnership(fixture.evidence, fixture.candidate); err != nil {
		t.Fatal(err)
	}
}

func TestRunnerDurableCommitIntentRejectsLiteralCopyAndFieldDrift(t *testing.T) {
	fixture, readback, runner := newRunnerCommitIntentFixture(t)
	durable, err := runner.appendCurrentCommitIntent(context.Background(), readback)
	if err != nil || !validRunnerDurableCommitIntent(durable) {
		t.Fatalf("append commit intent: durable=%+v err=%v", durable, err)
	}
	valueType := reflect.TypeOf(*durable)
	for index := 0; index < valueType.NumField(); index++ {
		if valueType.Field(index).PkgPath == "" {
			t.Fatalf("durable commit intent field %s became public", valueType.Field(index).Name)
		}
	}
	copyValue := *durable
	if err := closeRunnerDurableCommitIntent(&copyValue, nil); !IsCode(err, CodeTransactionBoundary) || fixture.database.transaction.rollbackCalls != 0 || !validRunnerDurableCommitIntent(durable) {
		t.Fatalf("copy changed original authority: err=%v transaction=%+v", err, fixture.database.transaction)
	}
	if err := closeRunnerDurableCommitIntent(&runnerDurableCommitIntent{}, nil); !IsCode(err, CodeTransactionBoundary) || fixture.database.transaction.rollbackCalls != 0 {
		t.Fatalf("literal escaped: err=%v", err)
	}

	originalName := durable.commit.LedgerRow.MigrationName
	durable.commit.LedgerRow.MigrationName += "-drift"
	assertDurableCommitIntentDrift(t, durable)
	durable.commit.LedgerRow.MigrationName = originalName

	originalState := durable.commit.LastIntermediateStateDigest
	durable.commit.LastIntermediateStateDigest = testDigest("other-intermediate-state")
	assertDurableCommitIntentDrift(t, durable)
	durable.commit.LastIntermediateStateDigest = originalState

	originalPlanDigest := durable.dispatch.planDigest
	durable.dispatch.planDigest = sha256.Sum256([]byte("other-commit-dispatch"))
	assertDurableCommitIntentDrift(t, durable)
	durable.dispatch.planDigest = originalPlanDigest

	originalEvidenceState := fixture.evidence.snapshot.state
	fixture.evidence.snapshot.state = RecoveryDivergent
	assertDurableCommitIntentDrift(t, durable)
	fixture.evidence.snapshot.state = originalEvidenceState

	fixture.database.transaction.status = 'E'
	assertDurableCommitIntentDrift(t, durable)
	fixture.database.transaction.status = 'T'
	if !validRunnerDurableCommitIntent(durable) {
		t.Fatal("restored durable commit intent did not recover its immutable binding")
	}

	originalKey := durable.key
	durable.key++
	err = closeRunnerDurableCommitIntent(durable, nil)
	if !IsCode(err, CodeTransactionBoundary) || fixture.database.transaction.rollbackCalls != 1 || !reflect.DeepEqual(fixture.database.unlockKeys, []int64{originalKey}) || fixture.database.closeCalls != 1 || liveRunnerDurableCommitIntents() != 0 {
		t.Fatalf("drifted close did not use registry ownership: err=%v transaction=%+v database=%+v", err, fixture.database.transaction, fixture.database)
	}
	if err := closeRunnerEvidenceOwnership(fixture.evidence, fixture.candidate); err != nil {
		t.Fatal(err)
	}
}

func TestRunnerCommitIntentRejectsUnavailableContextBeforeBinding(t *testing.T) {
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
			fixture, readback, runner := newRunnerCommitIntentFixture(t)
			active := runner
			if test.nilRunner {
				active = nil
			}
			durable, err := active.appendCurrentCommitIntent(test.context(), readback)
			assertRunnerCommitIntentError(t, err, test.wantCode, "runner-commit-intent")
			if durable != nil || fixture.evidence.commitBindCalls != 0 || fixture.evidence.journal.appendCalls != 2 || fixture.database.transaction.rollbackCalls != 1 || fixture.database.unlockCalls != 1 || fixture.database.closeCalls != 1 || fixture.database.transaction.commitCalls != 0 || fixture.database.backend.commitCalls != 0 || liveRunnerReadbackCurrentLedgers() != 0 || liveRunnerDurableCommitIntents() != 0 {
				t.Fatalf("unavailable context crossed commit boundary: durable=%+v err=%v transaction=%+v", durable, err, fixture.database.transaction)
			}
			if !fixture.evidence.journal.cursor.Valid() {
				t.Fatal("pre-append failure revoked the durable intermediate cursor")
			}
			if cleanupErr := closeRunnerEvidenceOwnership(fixture.evidence, fixture.candidate); cleanupErr != nil {
				t.Fatal(cleanupErr)
			}
		})
	}
}

func TestRunnerCommitIntentFaultsRollbackWithoutTransactionCommit(t *testing.T) {
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
		{"bind-error", func(f *runnerPreparedCurrentSessionFixture) { f.evidence.commitBindErr = errors.New("secret-bind") }, CodeEvidenceJournalFailed, "runner-commit-intent-bind", 2},
		{"bind-stable", func(f *runnerPreparedCurrentSessionFixture) {
			f.evidence.commitBindErr = fail(CodeEvidenceRecoveryRequired, "fake", "secret", errors.New("secret-bind"))
		}, CodeEvidenceRecoveryRequired, "runner-commit-intent-bind", 2},
		{"bind-canceled", func(f *runnerPreparedCurrentSessionFixture) { f.evidence.commitBindErr = context.Canceled }, CodeContextCanceled, "runner-commit-intent-bind", 2},
		{"bind-deadline", func(f *runnerPreparedCurrentSessionFixture) { f.evidence.commitBindErr = context.DeadlineExceeded }, CodeDeadlineExceeded, "runner-commit-intent-bind", 2},
		{"bind-missing-journal", func(f *runnerPreparedCurrentSessionFixture) { f.evidence.commitNoJournal = true }, CodeEvidenceRecoveryRequired, "runner-commit-intent-bind", 2},
		{"bind-missing-record", func(f *runnerPreparedCurrentSessionFixture) { f.evidence.commitNoRecord = true }, CodeEvidenceRecoveryRequired, "runner-commit-intent-bind", 2},
		{"bind-invalid-record", func(f *runnerPreparedCurrentSessionFixture) {
			f.evidence.mutateBoundCommit = func(value *CommitIntent) { value.ExpectedLedgerLength++ }
		}, CodeEvidenceJournalFailed, "runner-commit-intent-bind", 2},
		{"bind-cursor-swap", func(f *runnerPreparedCurrentSessionFixture) {
			f.evidence.mutateCommitAuthority = func(cursor *JournalCursor, _ *OwnedEvidenceRecord) { cursor.nextSequence++ }
		}, CodeEvidenceJournalFailed, "runner-commit-intent-bind", 2},
		{"bind-consumed-record", func(f *runnerPreparedCurrentSessionFixture) {
			f.evidence.mutateCommitAuthority = func(_ *JournalCursor, owned *OwnedEvidenceRecord) { owned.consumed.Store(true) }
		}, CodeEvidenceJournalFailed, "runner-commit-intent-bind", 2},
		{"append-error", func(f *runnerPreparedCurrentSessionFixture) {
			f.evidence.journal.appendErr = errors.New("secret-append")
		}, CodeEvidenceJournalFailed, "runner-commit-intent-append", 3},
		{"append-canceled", func(f *runnerPreparedCurrentSessionFixture) { f.evidence.journal.appendErr = context.Canceled }, CodeContextCanceled, "runner-commit-intent-append", 3},
		{"append-deadline", func(f *runnerPreparedCurrentSessionFixture) { f.evidence.journal.appendErr = context.DeadlineExceeded }, CodeDeadlineExceeded, "runner-commit-intent-append", 3},
		{"append-unknown", func(f *runnerPreparedCurrentSessionFixture) { f.evidence.journal.appendOutcome = appendOutcomeUnknown }, CodeEvidenceJournalFailed, "runner-commit-intent-append", 3},
		{"append-unknown-error", func(f *runnerPreparedCurrentSessionFixture) {
			f.evidence.journal.appendOutcome = appendOutcomeUnknown
			f.evidence.journal.appendErr = errors.New("secret-unknown")
			f.evidence.journal.appendValuesWithError = true
		}, CodeEvidenceJournalFailed, "runner-commit-intent-append", 3},
		{"durable-with-error", func(f *runnerPreparedCurrentSessionFixture) {
			f.evidence.journal.appendErr = errors.New("secret-durable")
			f.evidence.journal.appendValuesWithError = true
		}, CodeEvidenceJournalFailed, "runner-commit-intent-append", 3},
		{"result-sequence", func(f *runnerPreparedCurrentSessionFixture) {
			f.evidence.journal.mutateAppendResult = func(result *AppendResult) { result.candidateSequence++ }
		}, CodeEvidenceJournalFailed, "runner-commit-intent-append", 3},
		{"result-record", func(f *runnerPreparedCurrentSessionFixture) {
			f.evidence.journal.mutateAppendResult = func(result *AppendResult) { result.candidateRecordDigest = testDigest("other-commit") }
		}, CodeEvidenceJournalFailed, "runner-commit-intent-append", 3},
		{"result-checkpoint", func(f *runnerPreparedCurrentSessionFixture) {
			f.evidence.journal.mutateAppendResult = func(result *AppendResult) { result.candidateCheckpointRecordDigest = Digest("invalid") }
		}, CodeEvidenceJournalFailed, "runner-commit-intent-append", 3},
		{"result-rotation", func(f *runnerPreparedCurrentSessionFixture) {
			f.evidence.journal.mutateAppendResult = func(result *AppendResult) {
				result.rotationHeaderRecordDigest = digestPointer(testDigest("rotation"))
				result.rotationHeaderCheckpointRecordDigest = digestPointer(testDigest("rotation-checkpoint"))
			}
		}, CodeEvidenceJournalFailed, "runner-commit-intent-append", 3},
		{"result-cursor", func(f *runnerPreparedCurrentSessionFixture) {
			f.evidence.journal.mutateAppendResult = func(result *AppendResult) { result.durableCursor.nextSequence++ }
		}, CodeEvidenceJournalFailed, "runner-commit-intent-append", 3},
		{"recovery-state", func(f *runnerPreparedCurrentSessionFixture) {
			f.evidence.journal.mutateAppendSnapshot = func(snapshot *RecoverySnapshot) { snapshot.state = RecoveryDivergent }
		}, CodeEvidenceJournalFailed, "runner-commit-intent-evidence", 3},
		{"recovery-action", func(f *runnerPreparedCurrentSessionFixture) {
			f.evidence.journal.mutateAppendSnapshot = func(snapshot *RecoverySnapshot) { snapshot.nextPermittedAction = RecoveryReturnFailure }
		}, CodeEvidenceJournalFailed, "runner-commit-intent-evidence", 3},
		{"recovery-record", func(f *runnerPreparedCurrentSessionFixture) {
			f.evidence.journal.mutateAppendSnapshot = func(snapshot *RecoverySnapshot) { snapshot.commitIntent = nil }
		}, CodeEvidenceJournalFailed, "runner-commit-intent-evidence", 3},
		{"recovery-body", func(f *runnerPreparedCurrentSessionFixture) {
			f.evidence.journal.mutateAppendSnapshot = func(snapshot *RecoverySnapshot) { snapshot.commitIntent.value.ExpectedLedgerLength++ }
		}, CodeEvidenceJournalFailed, "runner-commit-intent-evidence", 3},
		{"recovery-digest", func(f *runnerPreparedCurrentSessionFixture) {
			f.evidence.journal.mutateAppendSnapshot = func(snapshot *RecoverySnapshot) {
				snapshot.lastCommitIntentRecordDigest = digestPointer(testDigest("other-commit"))
			}
		}, CodeEvidenceJournalFailed, "runner-commit-intent-evidence", 3},
		{"boundary", func(f *runnerPreparedCurrentSessionFixture) {
			f.database.transaction.boundaryErr = errors.New("secret-boundary")
		}, CodeTransactionBoundary, "runner-commit-intent-boundary", 3},
		{"boundary-role", func(f *runnerPreparedCurrentSessionFixture) {
			f.database.transaction.boundaryMutate = func(boundary *BoundaryState) { boundary.CurrentUser = "other_role" }
		}, CodeTransactionBoundary, "runner-commit-intent-boundary", 3},
		{"status-drift", func(f *runnerPreparedCurrentSessionFixture) {
			f.database.transaction.boundaryMutate = func(*BoundaryState) { f.database.transaction.status = 'E' }
		}, CodeTransactionBoundary, "runner-commit-intent-boundary", 3},
		{"evidence-drift", func(f *runnerPreparedCurrentSessionFixture) {
			f.database.transaction.boundaryMutate = func(*BoundaryState) { f.evidence.snapshot.state = RecoveryDivergent }
		}, CodeEvidenceJournalFailed, "runner-commit-intent-seal", 3},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture, readback, runner := newRunnerCommitIntentFixture(t)
			test.configure(&fixture)
			durable, err := runner.appendCurrentCommitIntent(context.Background(), readback)
			assertRunnerCommitIntentError(t, err, test.wantCode, test.wantOp)
			transaction := fixture.database.transaction
			if durable != nil || containsErrorText(err, "secret-") || fixture.evidence.commitBindCalls != 1 || fixture.evidence.journal.appendCalls != test.wantAppend || transaction.executeCalls != 1 || transaction.ledgerInsertCalls != 1 || transaction.ledgerReadCalls != 1 || transaction.rollbackCalls != 1 || transaction.commitCalls != 0 || transaction.execCalls != 0 || fixture.database.unlockCalls != 1 || fixture.database.closeCalls != 1 || fixture.database.backend.ledgerInsertCalls != 1 || fixture.database.backend.commitCalls != 0 || liveRunnerReadbackCurrentLedgers() != 0 || liveRunnerDurableCommitIntents() != 0 {
				t.Fatalf("commit intent fault escaped boundary: durable=%+v err=%v transaction=%+v database=%+v", durable, err, transaction, fixture.database)
			}
			wantCursor := true
			if postMutationRevoked[test.name] {
				wantCursor = false
			}
			if fixture.evidence.journal.cursor.Valid() != wantCursor {
				t.Fatalf("commit intent fault cursor validity=%t want=%t", fixture.evidence.journal.cursor.Valid(), wantCursor)
			}
			if cleanupErr := closeRunnerEvidenceOwnership(fixture.evidence, fixture.candidate); cleanupErr != nil {
				t.Fatal(cleanupErr)
			}
		})
	}
}

func TestRunnerCommitIntentCleanupFailureDominatesAndAttemptsEveryRelease(t *testing.T) {
	fixture, readback, runner := newRunnerCommitIntentFixture(t)
	fixture.evidence.commitBindErr = errors.New("secret-bind")
	fixture.database.transaction.rollbackErr = errors.New("secret-rollback")
	fixture.database.transaction.rollbackLeavesOpen = true
	fixture.database.unlockErr = errors.New("secret-unlock")
	fixture.database.closeErr = errors.New("secret-close")
	durable, err := runner.appendCurrentCommitIntent(context.Background(), readback)
	assertRunnerCommitIntentError(t, err, CodeTransactionBoundary, "runner-database-close")
	if durable != nil || containsErrorText(err, "secret-") || fixture.database.transaction.rollbackCalls != 1 || fixture.database.unlockCalls != 1 || fixture.database.closeCalls != 1 || fixture.evidence.journal.appendCalls != 2 || fixture.database.transaction.commitCalls != 0 || fixture.database.backend.commitCalls != 0 || liveRunnerDurableCommitIntents() != 0 {
		t.Fatalf("commit intent cleanup precedence/attempts: durable=%+v err=%v transaction=%+v database=%+v", durable, err, fixture.database.transaction, fixture.database)
	}
	if cleanupErr := closeRunnerEvidenceOwnership(fixture.evidence, fixture.candidate); cleanupErr != nil {
		t.Fatal(cleanupErr)
	}
}

func TestRunnerCommitIntentHasOneEvidenceAppendAndNoSQLLedgerOrCommitEdge(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "runner_commit_intent.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	appendCalls, boundaryCalls := 0, 0
	forbidden := map[string]bool{
		"ExecuteStatement": true, "BeginMigration": true, "ReserveAndActivateSuccessor": true,
		"Insert": true, "Commit": true, "Exec": true, "Query": true, "QueryRow": true,
		"insertAndReadRunnerLedgerRow": true, "ProjectAuthority": true, "ProjectCatalog": true,
		"ProjectTransitionState": true, "ProjectPrecondition": true,
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
			t.Fatalf("commit intent acquired forbidden %s call edge", selector.Sel.Name)
		}
		return true
	})
	if appendCalls != 1 || boundaryCalls != 1 {
		t.Fatalf("commit intent call edges: append=%d boundary=%d", appendCalls, boundaryCalls)
	}
}

func TestRunnerDurableCommitIntentHasNoProductionConsumer(t *testing.T) {
	paths, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	symbols := map[string]bool{
		"runnerDurableCommitIntent": true, "runnerDurableCommitIntentBinding": true,
		"runnerDurableCommitIntentRegistryRecord": true, "runnerDurableCommitIntentRegistry": true,
		"runnerCommitIntentSeed": true, "appendCurrentCommitIntent": true,
		"consumeRunnerReadbackCurrentLedger": true, "bindRunnerDurableCommitIntent": true,
		"validRunnerDurableCommitIntent": true, "closeRunnerDurableCommitIntent": true,
	}
	commitConsumers := map[string]map[string]bool{
		"runner_transaction_commit.go": {
			"runnerDurableCommitIntent": true, "runnerDurableCommitIntentRegistryRecord": true,
			"runnerDurableCommitIntentRegistry": true, "validRunnerDurableCommitIntent": true,
			"closeRunnerDurableCommitIntent": true,
		},
		"runner_current_execution.go": {"appendCurrentCommitIntent": true},
	}
	for _, path := range paths {
		name := filepath.Base(path)
		if strings.HasSuffix(name, "_test.go") || name == "runner_commit_intent.go" {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			identifier, ok := node.(*ast.Ident)
			if ok && symbols[identifier.Name] && !commitConsumers[name][identifier.Name] {
				t.Fatalf("durable commit intent %s acquired unreviewed production consumer %s", identifier.Name, name)
			}
			return true
		})
	}
}

func newRunnerCommitIntentFixture(t *testing.T) (runnerPreparedCurrentSessionFixture, *runnerReadbackCurrentLedger, *Runner) {
	t.Helper()
	fixture, durable, runner := newRunnerLedgerReadbackFixture(t)
	readback, err := runner.insertAndReadbackCurrentLedger(context.Background(), durable)
	if err != nil || !validRunnerReadbackCurrentLedger(readback) {
		t.Fatalf("commit intent fixture ledger: readback=%+v err=%v", readback, err)
	}
	return fixture, readback, runner
}

func assertDurableCommitIntentDrift(t *testing.T, durable *runnerDurableCommitIntent) {
	t.Helper()
	if validRunnerDurableCommitIntent(durable) {
		t.Fatal("mutated durable commit intent remained valid")
	}
}

func liveRunnerDurableCommitIntents() int {
	count := 0
	runnerDurableCommitIntentRegistry.Range(func(_, _ any) bool {
		count++
		return true
	})
	return count
}

func assertRunnerCommitIntentError(t *testing.T, err error, code ErrorCode, op string) {
	t.Helper()
	var migrationErr *Error
	if !errors.As(err, &migrationErr) || migrationErr.Code != code || migrationErr.Op != op || migrationErr.Err != nil {
		t.Fatalf("commit intent error: got=%#v want=%s/%s", migrationErr, code, op)
	}
}
