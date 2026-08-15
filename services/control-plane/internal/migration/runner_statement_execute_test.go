package migration

import (
	"bytes"
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestRunnerExecutedCurrentStatementConsumesDurableIntentAndExecutesExactBytesOnce(t *testing.T) {
	fixture, durable, runner := newRunnerDurableStatementExecutionFixture(t)
	transaction := fixture.database.transaction
	transaction.executeAllowed = true
	beforeBoundaryCalls := transaction.boundaryCalls
	wantSQL, err := durable.plan.exactSQLBytes()
	if err != nil {
		t.Fatal(err)
	}
	executed, err := runner.executeCurrentStatement(context.Background(), durable)
	if err != nil || !validRunnerExecutedCurrentStatement(executed) {
		t.Fatalf("execute statement: executed=%+v err=%v", executed, err)
	}
	if validRunnerDurableCurrentStatementIntent(durable) || liveRunnerDurableCurrentStatementIntents() != 0 || liveRunnerExecutedCurrentStatements() != 1 {
		t.Fatalf("durable intent was not atomically consumed: durable=%t live=%d/%d", validRunnerDurableCurrentStatementIntent(durable), liveRunnerDurableCurrentStatementIntents(), liveRunnerExecutedCurrentStatements())
	}
	if transaction.executeCalls != 1 || fixture.database.backend.executeCalls != 1 || len(transaction.executedSQL) != 1 || !bytes.Equal(transaction.executedSQL[0], wantSQL) || executed.executedStatementDigest != DigestBytes(wantSQL) || transaction.boundaryCalls != beforeBoundaryCalls || transaction.execCalls != 0 || transaction.commitCalls != 0 || transaction.rollbackCalls != 0 || transaction.status != 'T' {
		t.Fatalf("exact execution boundary mismatch: executed=%+v transaction=%+v", executed, transaction)
	}
	if fixture.evidence.snapshot.state != RecoveryDanglingStatementIntent || fixture.evidence.snapshot.tailDigest != executed.intentRecordDigest || executed.recoveryDigest != generationJournalRecoveryDigest(fixture.evidence.snapshot) {
		t.Fatalf("SQL execution changed durable evidence: executed=%+v snapshot=%+v", executed, fixture.evidence.snapshot)
	}
	if executed.policy.Validate() != nil || executed.policy.MaxAttempts != uint64(executed.maxAttempts) || !runnerCanonicalEqual(executed.policy, fixture.bundle.Manifest.ExecutionPolicy) {
		t.Fatalf("executed authority lost the exact execution policy: executed=%+v policy=%+v", executed, executed.policy)
	}
	if replay, replayErr := runner.executeCurrentStatement(context.Background(), durable); replay != nil || !IsCode(replayErr, CodeTransactionBoundary) || transaction.executeCalls != 1 || !validRunnerExecutedCurrentStatement(executed) {
		t.Fatalf("consumed durable intent replayed execution or damaged successor: replay=%+v err=%v transaction=%+v", replay, replayErr, transaction)
	}
	if err := closeRunnerExecutedCurrentStatement(executed, nil); err != nil || transaction.rollbackCalls != 1 || transaction.status != 'I' || fixture.database.unlockCalls != 1 || fixture.database.closeCalls != 1 || liveRunnerExecutedCurrentStatements() != 0 {
		t.Fatalf("executed close did not release database ownership: err=%v transaction=%+v database=%+v", err, transaction, fixture.database)
	}
	if err := closeRunnerEvidenceOwnership(fixture.evidence, fixture.candidate); err != nil {
		t.Fatal(err)
	}
}

func TestRunnerExecutedCurrentStatementRejectsLiteralCopyAndFieldDrift(t *testing.T) {
	fixture, durable, runner := newRunnerDurableStatementExecutionFixture(t)
	fixture.database.transaction.executeAllowed = true
	executed, err := runner.executeCurrentStatement(context.Background(), durable)
	if err != nil || !validRunnerExecutedCurrentStatement(executed) {
		t.Fatalf("execute statement: executed=%+v err=%v", executed, err)
	}
	valueType := reflect.TypeOf(*executed)
	for index := 0; index < valueType.NumField(); index++ {
		if valueType.Field(index).PkgPath == "" {
			t.Fatalf("executed statement field %s became public", valueType.Field(index).Name)
		}
	}
	copyValue := *executed
	if err := closeRunnerExecutedCurrentStatement(&copyValue, nil); !IsCode(err, CodeTransactionBoundary) || fixture.database.transaction.rollbackCalls != 0 || !validRunnerExecutedCurrentStatement(executed) {
		t.Fatalf("copy changed original authority: err=%v transaction=%+v", err, fixture.database.transaction)
	}
	if err := closeRunnerExecutedCurrentStatement(&runnerExecutedCurrentStatement{}, nil); !IsCode(err, CodeTransactionBoundary) || fixture.database.transaction.rollbackCalls != 0 {
		t.Fatalf("literal escaped: err=%v", err)
	}

	originalDigest := executed.executedStatementDigest
	executed.executedStatementDigest = testDigest("other-executed-statement")
	assertExecutedStatementDrift(t, executed)
	executed.executedStatementDigest = originalDigest

	originalSQL := executed.plan.sqlBytes[0]
	executed.plan.sqlBytes[0] ^= 0xff
	assertExecutedStatementDrift(t, executed)
	executed.plan.sqlBytes[0] = originalSQL

	originalIdleTimeout := executed.policy.IdleInTransactionSessionTimeoutMS
	executed.policy.IdleInTransactionSessionTimeoutMS++
	assertExecutedStatementDrift(t, executed)
	executed.policy.IdleInTransactionSessionTimeoutMS = originalIdleTimeout

	originalRecord := executed.intentRecordDigest
	executed.intentRecordDigest = testDigest("other-intent-record")
	assertExecutedStatementDrift(t, executed)
	executed.intentRecordDigest = originalRecord

	originalState := fixture.evidence.snapshot.state
	fixture.evidence.snapshot.state = RecoveryDivergent
	assertExecutedStatementDrift(t, executed)
	fixture.evidence.snapshot.state = originalState

	fixture.database.transaction.status = 'E'
	assertExecutedStatementDrift(t, executed)
	fixture.database.transaction.status = 'T'
	if !validRunnerExecutedCurrentStatement(executed) {
		t.Fatal("restored executed authority did not recover its immutable binding")
	}

	originalKey := executed.key
	originalTransaction := executed.transaction
	rogueTransaction := newRunnerPreflightTransaction(fixture.database)
	rogueTransaction.active = true
	rogueTransaction.status = 'T'
	executed.key++
	executed.transaction = rogueTransaction
	err = closeRunnerExecutedCurrentStatement(executed, nil)
	if !IsCode(err, CodeTransactionBoundary) || originalTransaction != fixture.database.transaction || fixture.database.transaction.rollbackCalls != 1 || rogueTransaction.rollbackCalls != 0 || !reflect.DeepEqual(fixture.database.unlockKeys, []int64{originalKey}) || fixture.database.closeCalls != 1 || liveRunnerExecutedCurrentStatements() != 0 {
		t.Fatalf("drifted close did not use registry ownership: err=%v transaction=%+v database=%+v", err, fixture.database.transaction, fixture.database)
	}
	if err := closeRunnerEvidenceOwnership(fixture.evidence, fixture.candidate); err != nil {
		t.Fatal(err)
	}
}

func TestRunnerStatementExecutionFaultsNeverMintSuccessAndReleaseDatabase(t *testing.T) {
	tests := []struct {
		name        string
		context     func() context.Context
		configure   func(*runnerPreparedCurrentSessionFixture)
		nilRunner   bool
		wantCode    ErrorCode
		wantExecute int
		wantCursor  bool
	}{
		{"nil-context", func() context.Context { return nil }, nil, false, CodeTransactionBoundary, 0, true},
		{"pre-canceled", func() context.Context { ctx, cancel := context.WithCancel(context.Background()); cancel(); return ctx }, nil, false, CodeContextCanceled, 0, true},
		{"pre-deadline", func() context.Context {
			ctx, cancel := context.WithDeadline(context.Background(), testExpiredTime())
			defer cancel()
			return ctx
		}, nil, false, CodeDeadlineExceeded, 0, true},
		{"nil-runner", func() context.Context { return context.Background() }, nil, true, CodeTransactionBoundary, 0, true},
		{"database-error", func() context.Context { return context.Background() }, func(f *runnerPreparedCurrentSessionFixture) {
			f.database.transaction.executeErr = errors.New("secret-execute")
		}, false, CodeInvalidSQL, 1, true},
		{"stable-error", func() context.Context { return context.Background() }, func(f *runnerPreparedCurrentSessionFixture) {
			f.database.transaction.executeErr = fail(CodeInvalidSQL, "fake", "secret", errors.New("secret-execute"))
		}, false, CodeInvalidSQL, 1, true},
		{"serialization", func() context.Context { return context.Background() }, func(f *runnerPreparedCurrentSessionFixture) {
			f.database.transaction.executeErr = fail(CodeInvalidSQL, "fake", "secret", &pgconn.PgError{Code: "40001"})
		}, false, CodeTransactionBoundary, 1, true},
		{"deadlock", func() context.Context { return context.Background() }, func(f *runnerPreparedCurrentSessionFixture) {
			f.database.transaction.executeErr = fail(CodeInvalidSQL, "fake", "secret", &pgconn.PgError{Code: "40P01"})
		}, false, CodeTransactionBoundary, 1, true},
		{"returned-canceled", func() context.Context { return context.Background() }, func(f *runnerPreparedCurrentSessionFixture) { f.database.transaction.executeErr = context.Canceled }, false, CodeContextCanceled, 1, true},
		{"returned-deadline", func() context.Context { return context.Background() }, func(f *runnerPreparedCurrentSessionFixture) {
			f.database.transaction.executeErr = context.DeadlineExceeded
		}, false, CodeDeadlineExceeded, 1, true},
		{"status-escaped", func() context.Context { return context.Background() }, func(f *runnerPreparedCurrentSessionFixture) {
			f.database.transaction.executeMutate = func([]byte) { f.database.transaction.status = 'E' }
		}, false, CodeTransactionBoundary, 1, true},
		{"evidence-drift", func() context.Context { return context.Background() }, func(f *runnerPreparedCurrentSessionFixture) {
			f.database.transaction.executeMutate = func([]byte) { f.evidence.snapshot.state = RecoveryDivergent }
		}, false, CodeEvidenceJournalFailed, 1, false},
		{"cursor-revoked", func() context.Context { return context.Background() }, func(f *runnerPreparedCurrentSessionFixture) {
			f.database.transaction.executeMutate = func([]byte) { f.evidence.journal.cursor.valid.Store(false) }
		}, false, CodeEvidenceJournalFailed, 1, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture, durable, runner := newRunnerDurableStatementExecutionFixture(t)
			fixture.database.transaction.executeAllowed = true
			if test.configure != nil {
				test.configure(&fixture)
			}
			var activeRunner *Runner = runner
			if test.nilRunner {
				activeRunner = nil
			}
			executed, err := activeRunner.executeCurrentStatement(test.context(), durable)
			assertRunnerStatementExecutionError(t, err, test.wantCode, "runner-statement-execute")
			if executed != nil || containsErrorText(err, "secret-") || fixture.database.transaction.executeCalls != test.wantExecute || fixture.database.transaction.execCalls != 0 || fixture.database.transaction.commitCalls != 0 || fixture.database.transaction.rollbackCalls != 1 || fixture.database.transaction.status != 'I' || fixture.database.unlockCalls != 1 || fixture.database.closeCalls != 1 || liveRunnerDurableCurrentStatementIntents() != 0 || liveRunnerExecutedCurrentStatements() != 0 {
				t.Fatalf("execution fault escaped boundary: executed=%+v err=%v transaction=%+v database=%+v", executed, err, fixture.database.transaction, fixture.database)
			}
			if fixture.evidence.journal.cursor.Valid() != test.wantCursor {
				t.Fatalf("execution fault cursor validity=%t want=%t", fixture.evidence.journal.cursor.Valid(), test.wantCursor)
			}
			if cleanupErr := closeRunnerEvidenceOwnership(fixture.evidence, fixture.candidate); cleanupErr != nil {
				t.Fatal(cleanupErr)
			}
		})
	}
}

func TestRunnerStatementExecutionCleanupFailuresDominateAndAttemptEveryRelease(t *testing.T) {
	for _, test := range []struct {
		name      string
		configure func(*runnerPreparedCurrentSessionFixture)
		wantOp    string
	}{
		{"rollback", func(f *runnerPreparedCurrentSessionFixture) {
			f.database.transaction.executeErr = errors.New("secret-execute")
			f.database.transaction.rollbackErr = errors.New("secret-rollback")
		}, "runner-transaction-rollback"},
		{"rollback-status", func(f *runnerPreparedCurrentSessionFixture) {
			f.database.transaction.executeErr = errors.New("secret-execute")
			f.database.transaction.rollbackLeavesOpen = true
		}, "runner-transaction-rollback"},
		{"close-dominates", func(f *runnerPreparedCurrentSessionFixture) {
			f.database.transaction.executeErr = errors.New("secret-execute")
			f.database.transaction.rollbackErr = errors.New("secret-rollback")
			f.database.transaction.rollbackLeavesOpen = true
			f.database.unlockErr = errors.New("secret-unlock")
			f.database.closeErr = errors.New("secret-close")
		}, "runner-database-close"},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture, durable, runner := newRunnerDurableStatementExecutionFixture(t)
			fixture.database.transaction.executeAllowed = true
			test.configure(&fixture)
			executed, err := runner.executeCurrentStatement(context.Background(), durable)
			assertRunnerStatementExecutionError(t, err, CodeTransactionBoundary, test.wantOp)
			if executed != nil || containsErrorText(err, "secret-") || fixture.database.transaction.executeCalls != 1 || fixture.database.transaction.rollbackCalls != 1 || fixture.database.unlockCalls != 1 || fixture.database.closeCalls != 1 || liveRunnerExecutedCurrentStatements() != 0 {
				t.Fatalf("cleanup precedence/attempts: executed=%+v err=%v transaction=%+v database=%+v", executed, err, fixture.database.transaction, fixture.database)
			}
			if cleanupErr := closeRunnerEvidenceOwnership(fixture.evidence, fixture.candidate); cleanupErr != nil {
				t.Fatal(cleanupErr)
			}
		})
	}
}

func TestRunnerStatementExecutionHasOneExactSQLCallAndNoLaterMutationEdges(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "runner_statement_execute.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	executeCalls := 0
	forbidden := map[string]bool{
		"BeginMigration": true, "Boundary": true, "AppendDurable": true,
		"ReserveAndActivateSuccessor": true, "Insert": true, "Commit": true,
		"Exec": true, "Query": true, "QueryRow": true,
		"ProjectAuthority": true, "ProjectTransitionState": true, "ProjectCatalog": true,
	}
	ast.Inspect(file, func(node ast.Node) bool {
		selector, ok := node.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if selector.Sel.Name == "ExecuteStatement" {
			executeCalls++
		}
		if forbidden[selector.Sel.Name] {
			t.Fatalf("statement execution acquired forbidden %s call edge", selector.Sel.Name)
		}
		return true
	})
	if executeCalls != 1 {
		t.Fatalf("statement execution call edges=%d want=1", executeCalls)
	}
}

func TestRunnerExecutedStatementHasNoProductionConsumer(t *testing.T) {
	paths, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	symbols := map[string]bool{
		"runnerExecutedCurrentStatement": true, "runnerExecutedCurrentStatementBinding": true,
		"runnerExecutedCurrentStatementRegistryRecord": true, "runnerExecutedCurrentStatementRegistry": true,
		"runnerExecutedCurrentStatementSeed": true, "executeCurrentStatement": true,
		"consumeRunnerDurableCurrentStatementIntent": true, "bindRunnerExecutedCurrentStatement": true,
		"validRunnerExecutedCurrentStatement": true, "closeRunnerExecutedCurrentStatement": true,
	}
	for _, path := range paths {
		name := filepath.Base(path)
		if strings.HasSuffix(name, "_test.go") || name == "runner_statement_execute.go" || name == "runner_statement_after.go" {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			identifier, ok := node.(*ast.Ident)
			if ok && symbols[identifier.Name] {
				t.Fatalf("executed statement authority %s acquired unreviewed production consumer %s", identifier.Name, name)
			}
			return true
		})
	}
}

func newRunnerDurableStatementExecutionFixture(t *testing.T) (runnerPreparedCurrentSessionFixture, *runnerDurableCurrentStatementIntent, *Runner) {
	t.Helper()
	fixture, prepared, runner := newRunnerPreparedCurrentStatementIntentFixture(t)
	durable, err := runner.appendCurrentStatementIntent(context.Background(), prepared)
	if err != nil || !validRunnerDurableCurrentStatementIntent(durable) {
		t.Fatalf("execution fixture durable intent: durable=%+v err=%v", durable, err)
	}
	return fixture, durable, runner
}

func assertExecutedStatementDrift(t *testing.T, executed *runnerExecutedCurrentStatement) {
	t.Helper()
	if validRunnerExecutedCurrentStatement(executed) {
		t.Fatal("mutated executed statement authority remained valid")
	}
}

func liveRunnerExecutedCurrentStatements() int {
	count := 0
	runnerExecutedCurrentStatementRegistry.Range(func(_, _ any) bool {
		count++
		return true
	})
	return count
}

func assertRunnerStatementExecutionError(t *testing.T, err error, code ErrorCode, op string) {
	t.Helper()
	var migrationErr *Error
	if !errors.As(err, &migrationErr) || migrationErr.Code != code || migrationErr.Op != op || migrationErr.Err != nil {
		t.Fatalf("statement execution error: got=%#v want=%s/%s", migrationErr, code, op)
	}
}

func testExpiredTime() time.Time {
	return time.Unix(1, 0)
}
