package migration

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestRunnerTransactionCommitConsumesDurableIntentAndClosesExactOutcome(t *testing.T) {
	for _, test := range []struct {
		name        string
		err         error
		wantOutcome runnerCommitProtocolOutcome
		wantReason  string
	}{
		{"committed", nil, runnerCommitProtocolCommitted, ""},
		{"serialization-rejected", &pgconn.PgError{Code: "40001"}, runnerCommitProtocolRejected, runnerCommitRejectedSerialization},
		{"deadlock-rejected", &pgconn.PgError{Code: "40P01"}, runnerCommitProtocolRejected, runnerCommitRejectedDeadlock},
		{"other-rejected", &pgconn.PgError{Code: "23505"}, runnerCommitProtocolRejected, runnerCommitRejectedOther},
		{"ambiguous-eof", io.EOF, runnerCommitProtocolAmbiguous, ""},
		{"ambiguous-context", context.Canceled, runnerCommitProtocolAmbiguous, ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture, durable, runner := newRunnerTransactionCommitFixture(t)
			transaction := fixture.database.transaction
			transaction.commitErr = test.err
			transaction.commitStatusAfter = 'I'
			closed, err := runner.commitCurrentTransaction(context.Background(), durable)
			if err != nil || !validRunnerClosedCurrentCommit(closed) {
				t.Fatalf("commit transaction: closed=%+v err=%v", closed, err)
			}
			if validRunnerDurableCommitIntent(durable) || liveRunnerDurableCommitIntents() != 0 || liveRunnerCommitProtocolObservations() != 0 || liveRunnerClosedCurrentCommits() != 1 {
				t.Fatalf("commit authority was not atomically advanced: durable=%t live=%d/%d/%d", validRunnerDurableCommitIntent(durable), liveRunnerDurableCommitIntents(), liveRunnerCommitProtocolObservations(), liveRunnerClosedCurrentCommits())
			}
			if closed.protocol.outcome != test.wantOutcome || closed.protocol.rejectionReason != test.wantReason || !closed.protocol.commitCalled || closed.protocol.connectionClosed || !closed.connectionCloseProven || closed.oldLifecycleID == "" || closed.lifecycleOrder.token == nil || closed.lifecycleOrder.ordinal != 1 {
				t.Fatalf("closed commit facts differ: %+v", closed)
			}
			if transaction.commitCalls != 1 || transaction.rollbackCalls != 0 || transaction.active || transaction.status != 'I' || transaction.pendingLedger != nil || fixture.database.unlockCalls != 0 || fixture.database.closeCalls != 1 || !fixture.database.closed || fixture.database.backend.commitCalls != 1 || fixture.evidence.journal.appendCalls != 3 || fixture.evidence.snapshot.state != RecoveryDanglingCommitIntent {
				t.Fatalf("commit crossed closed lifecycle: transaction=%+v database=%+v evidence=%+v", transaction, fixture.database, fixture.evidence)
			}
			if replay, replayErr := runner.commitCurrentTransaction(context.Background(), durable); replay != nil || !IsCode(replayErr, CodeTransactionBoundary) || transaction.commitCalls != 1 || !validRunnerClosedCurrentCommit(closed) {
				t.Fatalf("durable commit replayed or damaged successor: replay=%+v err=%v", replay, replayErr)
			}
			if err := closeRunnerClosedCurrentCommit(closed, nil); err != nil || liveRunnerClosedCurrentCommits() != 0 {
				t.Fatalf("release closed commit: err=%v live=%d", err, liveRunnerClosedCurrentCommits())
			}
			if err := closeRunnerEvidenceOwnership(fixture.evidence, fixture.candidate); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestRunnerTransactionCommitPreCallFailuresRollbackWithoutCommit(t *testing.T) {
	for _, test := range []struct {
		name      string
		ctx       func() context.Context
		nilRunner bool
		configure func(*runnerPreparedCurrentSessionFixture)
		wantCode  ErrorCode
		wantOp    string
	}{
		{"nil-context", func() context.Context { return nil }, false, func(*runnerPreparedCurrentSessionFixture) {}, CodeTransactionBoundary, "runner-transaction-commit"},
		{"canceled", func() context.Context { ctx, cancel := context.WithCancel(context.Background()); cancel(); return ctx }, false, func(*runnerPreparedCurrentSessionFixture) {}, CodeContextCanceled, "runner-commit-protocol"},
		{"deadline", func() context.Context {
			ctx, cancel := context.WithDeadline(context.Background(), testExpiredTime())
			defer cancel()
			return ctx
		}, false, func(*runnerPreparedCurrentSessionFixture) {}, CodeDeadlineExceeded, "runner-commit-protocol"},
		{"nil-runner", func() context.Context { return context.Background() }, true, func(*runnerPreparedCurrentSessionFixture) {}, CodeTransactionBoundary, "runner-transaction-commit"},
		{"protocol-already-claimed", func() context.Context { return context.Background() }, false, func(f *runnerPreparedCurrentSessionFixture) { f.database.transaction.commitClaimed = true }, CodeTransactionBoundary, "runner-commit-protocol"},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture, durable, runner := newRunnerTransactionCommitFixture(t)
			test.configure(&fixture)
			active := runner
			if test.nilRunner {
				active = nil
			}
			closed, err := active.commitCurrentTransaction(test.ctx(), durable)
			assertRunnerTransactionCommitError(t, err, test.wantCode, test.wantOp)
			transaction := fixture.database.transaction
			if closed != nil || transaction.commitCalls != 0 || transaction.rollbackCalls != 1 || transaction.status != 'I' || transaction.active || fixture.database.unlockCalls != 1 || fixture.database.closeCalls != 1 || fixture.database.backend.commitCalls != 0 || fixture.evidence.journal.appendCalls != 3 || liveRunnerDurableCommitIntents() != 0 || liveRunnerCommitProtocolObservations() != 0 || liveRunnerClosedCurrentCommits() != 0 {
				t.Fatalf("pre-call failure crossed commit: closed=%+v err=%v transaction=%+v database=%+v", closed, err, transaction, fixture.database)
			}
			if !fixture.evidence.journal.cursor.Valid() || fixture.evidence.snapshot.state != RecoveryDanglingCommitIntent {
				t.Fatalf("pre-call failure changed durable evidence: cursor=%+v snapshot=%+v", fixture.evidence.journal.cursor, fixture.evidence.snapshot)
			}
			if cleanupErr := closeRunnerEvidenceOwnership(fixture.evidence, fixture.candidate); cleanupErr != nil {
				t.Fatal(cleanupErr)
			}
		})
	}
}

func TestRunnerClosedCurrentCommitRejectsLiteralCopyAndDrift(t *testing.T) {
	fixture, durable, runner := newRunnerTransactionCommitFixture(t)
	closed, err := runner.commitCurrentTransaction(context.Background(), durable)
	if err != nil || !validRunnerClosedCurrentCommit(closed) {
		t.Fatalf("commit transaction: closed=%+v err=%v", closed, err)
	}
	valueType := reflect.TypeOf(*closed)
	for index := 0; index < valueType.NumField(); index++ {
		if valueType.Field(index).PkgPath == "" {
			t.Fatalf("closed commit field %s became public", valueType.Field(index).Name)
		}
	}
	copyValue := *closed
	if err := closeRunnerClosedCurrentCommit(&copyValue, nil); !IsCode(err, CodeTransactionBoundary) || !validRunnerClosedCurrentCommit(closed) {
		t.Fatalf("copy changed original authority: err=%v", err)
	}
	if err := closeRunnerClosedCurrentCommit(&runnerClosedCurrentCommit{}, nil); !IsCode(err, CodeTransactionBoundary) {
		t.Fatalf("literal escaped: err=%v", err)
	}

	originalOutcome := closed.protocol.outcome
	closed.protocol.outcome = runnerCommitProtocolAmbiguous
	assertClosedCurrentCommitDrift(t, closed)
	closed.protocol.outcome = originalOutcome

	originalClose := closed.connectionCloseProven
	closed.connectionCloseProven = !originalClose
	assertClosedCurrentCommitDrift(t, closed)
	closed.connectionCloseProven = originalClose

	originalLifecycle := closed.oldLifecycleID
	closed.oldLifecycleID += "-drift"
	assertClosedCurrentCommitDrift(t, closed)
	closed.oldLifecycleID = originalLifecycle

	originalNonce := closed.lifecycleOrder.token.verifierNonce
	closed.lifecycleOrder.token.verifierNonce[0] ^= 0xff
	assertClosedCurrentCommitDrift(t, closed)
	closed.lifecycleOrder.token.verifierNonce = originalNonce

	originalRow := closed.commit.LedgerRow.MigrationName
	closed.commit.LedgerRow.MigrationName += "-drift"
	assertClosedCurrentCommitDrift(t, closed)
	closed.commit.LedgerRow.MigrationName = originalRow

	originalSnapshot := fixture.evidence.snapshot.state
	fixture.evidence.snapshot.state = RecoveryDivergent
	assertClosedCurrentCommitDrift(t, closed)
	fixture.evidence.snapshot.state = originalSnapshot
	if !validRunnerClosedCurrentCommit(closed) {
		t.Fatal("restored closed commit did not recover immutable binding")
	}

	closed.key++
	err = closeRunnerClosedCurrentCommit(closed, nil)
	if !IsCode(err, CodeTransactionBoundary) || liveRunnerClosedCurrentCommits() != 0 {
		t.Fatalf("drifted close did not poison registry authority: err=%v live=%d", err, liveRunnerClosedCurrentCommits())
	}
	if err := closeRunnerEvidenceOwnership(fixture.evidence, fixture.candidate); err != nil {
		t.Fatal(err)
	}
}

func TestRunnerTransactionCommitCloseProofIsExact(t *testing.T) {
	for _, test := range []struct {
		name                     string
		protocolAlreadyClosed    bool
		wantConnectionCloseProof bool
	}{
		{"close-failed", false, false},
		{"protocol-already-closed", true, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture, durable, runner := newRunnerTransactionCommitFixture(t)
			fixture.database.transaction.commitErr = io.EOF
			fixture.database.transaction.commitStatusAfter = 0
			fixture.database.transaction.commitClosedAfter = test.protocolAlreadyClosed
			fixture.database.closeErr = errors.New("secret-close")
			closed, err := runner.commitCurrentTransaction(context.Background(), durable)
			if err != nil || closed == nil || !validRunnerClosedCurrentCommit(closed) || closed.protocol.outcome != runnerCommitProtocolAmbiguous || closed.connectionCloseProven != test.wantConnectionCloseProof || fixture.database.closeCalls != 1 || fixture.database.transaction.rollbackCalls != 0 {
				t.Fatalf("close proof: closed=%+v err=%v database=%+v", closed, err, fixture.database)
			}
			closeErr := closeRunnerClosedCurrentCommit(closed, nil)
			if test.wantConnectionCloseProof && closeErr != nil || !test.wantConnectionCloseProof && !IsCode(closeErr, CodeTransactionBoundary) {
				t.Fatalf("release close proof=%t err=%v", test.wantConnectionCloseProof, closeErr)
			}
			if cleanupErr := closeRunnerEvidenceOwnership(fixture.evidence, fixture.candidate); cleanupErr != nil {
				t.Fatal(cleanupErr)
			}
		})
	}
}

func TestRunnerTransactionCommitHasOneProtocolCallAndNoTerminalOrDatabaseMutation(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "runner_transaction_commit.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	protocolCalls, closeCalls := 0, 0
	forbidden := map[string]bool{
		"AppendDurable": true, "Commit": true, "Rollback": true, "UnlockAndReset": true,
		"BeginMigration": true, "Connect": true, "ExecuteStatement": true, "Insert": true,
		"Query": true, "QueryRow": true, "ProjectAuthority": true, "ProjectCatalog": true,
	}
	ast.Inspect(file, func(node ast.Node) bool {
		if call, ok := node.(*ast.CallExpr); ok {
			if identifier, ok := call.Fun.(*ast.Ident); ok && identifier.Name == "invokeRunnerCommitProtocol" {
				protocolCalls++
			}
		}
		selector, ok := node.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if selector.Sel.Name == "Close" {
			closeCalls++
		}
		if forbidden[selector.Sel.Name] {
			t.Fatalf("transaction commit acquired forbidden %s call edge", selector.Sel.Name)
		}
		return true
	})
	if protocolCalls != 1 || closeCalls != 1 {
		t.Fatalf("transaction commit call edges: protocol=%d close=%d", protocolCalls, closeCalls)
	}
}

func TestRunnerClosedCurrentCommitHasNoProductionConsumer(t *testing.T) {
	paths, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	symbols := map[string]bool{
		"runnerClosedCurrentCommit": true, "runnerClosedCurrentCommitBinding": true,
		"runnerClosedCurrentCommitRegistryRecord": true, "runnerClosedCurrentCommitRegistry": true,
		"runnerCurrentCommitSeed": true, "commitCurrentTransaction": true,
		"consumeRunnerDurableCommitIntent": true, "bindRunnerClosedCurrentCommit": true,
		"validRunnerClosedCurrentCommit": true, "closeRunnerClosedCurrentCommit": true,
	}
	committedTerminalConsumers := map[string]map[string]bool{
		"evidence_runner_committed_terminal.go": {
			"runnerClosedCurrentCommit": true, "runnerClosedCurrentCommitRegistryRecord": true,
			"runnerClosedCurrentCommitRegistry": true, "validRunnerClosedCurrentCommit": true,
		},
		"evidence_session.go": {
			"runnerClosedCurrentCommit": true, "validRunnerClosedCurrentCommit": true,
		},
	}
	for _, path := range paths {
		name := filepath.Base(path)
		if strings.HasSuffix(name, "_test.go") || name == "runner_transaction_commit.go" {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			identifier, ok := node.(*ast.Ident)
			if ok && symbols[identifier.Name] && !committedTerminalConsumers[name][identifier.Name] {
				t.Fatalf("closed transaction commit %s acquired unreviewed production consumer %s", identifier.Name, name)
			}
			return true
		})
	}
}

func newRunnerTransactionCommitFixture(t *testing.T) (runnerPreparedCurrentSessionFixture, *runnerDurableCommitIntent, *Runner) {
	t.Helper()
	fixture, readback, runner := newRunnerCommitIntentFixture(t)
	durable, err := runner.appendCurrentCommitIntent(context.Background(), readback)
	if err != nil || !validRunnerDurableCommitIntent(durable) {
		t.Fatalf("transaction commit fixture: durable=%+v err=%v", durable, err)
	}
	return fixture, durable, runner
}

func assertClosedCurrentCommitDrift(t *testing.T, closed *runnerClosedCurrentCommit) {
	t.Helper()
	if validRunnerClosedCurrentCommit(closed) {
		t.Fatal("mutated closed current commit remained valid")
	}
}

func assertRunnerTransactionCommitError(t *testing.T, err error, code ErrorCode, op string) {
	t.Helper()
	var stable *Error
	if !errors.As(err, &stable) || stable.Code != code || stable.Op != op || stable.Err != nil {
		t.Fatalf("transaction commit error: got=%#v want=%s/%s", stable, code, op)
	}
}

func liveRunnerClosedCurrentCommits() int {
	count := 0
	runnerClosedCurrentCommitRegistry.Range(func(_, _ any) bool {
		count++
		return true
	})
	return count
}
