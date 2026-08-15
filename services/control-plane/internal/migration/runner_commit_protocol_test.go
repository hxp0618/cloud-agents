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

func TestRunnerCommitProtocolClassifiesOnlyCompleteDriverOutcomes(t *testing.T) {
	tests := []struct {
		name          string
		err           error
		status        byte
		closed        bool
		wantOutcome   runnerCommitProtocolOutcome
		wantReason    string
		wantReady     bool
		wantConnClose bool
	}{
		{"committed", nil, 'I', false, runnerCommitProtocolCommitted, "", true, false},
		{"serialization", &pgconn.PgError{Code: "40001"}, 'I', false, runnerCommitProtocolRejected, runnerCommitRejectedSerialization, true, false},
		{"deadlock", &pgconn.PgError{Code: "40P01"}, 'I', false, runnerCommitProtocolRejected, runnerCommitRejectedDeadlock, true, false},
		{"other-postgres", &pgconn.PgError{Code: "23505"}, 'I', false, runnerCommitProtocolRejected, runnerCommitRejectedOther, true, false},
		{"postgres-no-ready", &pgconn.PgError{Code: "40001"}, 'T', false, runnerCommitProtocolAmbiguous, "", false, false},
		{"postgres-closed", &pgconn.PgError{Code: "40001"}, 'I', true, runnerCommitProtocolAmbiguous, "", false, true},
		{"connection-sqlstate", &pgconn.PgError{Code: "08006"}, 'I', false, runnerCommitProtocolAmbiguous, "", false, false},
		{"statement-unknown", &pgconn.PgError{Code: "40003"}, 'I', false, runnerCommitProtocolAmbiguous, "", false, false},
		{"server-timeout", &pgconn.PgError{Code: "57014"}, 'I', false, runnerCommitProtocolAmbiguous, "", false, false},
		{"admin-shutdown", &pgconn.PgError{Code: "57P01"}, 'I', false, runnerCommitProtocolAmbiguous, "", false, false},
		{"fatal-response", &pgconn.PgError{Code: "XX000", Severity: "FATAL"}, 'I', false, runnerCommitProtocolAmbiguous, "", false, false},
		{"canceled-with-idle", context.Canceled, 'I', false, runnerCommitProtocolAmbiguous, "", false, false},
		{"deadline-with-idle", context.DeadlineExceeded, 'I', false, runnerCommitProtocolAmbiguous, "", false, false},
		{"timeout-with-idle", commitProtocolTimeoutError{}, 'I', false, runnerCommitProtocolAmbiguous, "", false, false},
		{"eof-with-idle", io.EOF, 'I', false, runnerCommitProtocolAmbiguous, "", false, false},
		{"unexpected-eof", io.ErrUnexpectedEOF, 'I', false, runnerCommitProtocolAmbiguous, "", false, false},
		{"unknown-error", errors.New("secret-commit"), 'I', false, runnerCommitProtocolAmbiguous, "", false, false},
		{"nil-with-transaction-status", nil, 'T', false, runnerCommitProtocolAmbiguous, "", false, false},
		{"nil-with-closed-connection", nil, 'I', true, runnerCommitProtocolAmbiguous, "", false, true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			transaction := &commitProtocolFake{status: 'T', statusAfter: test.status, closedAfter: test.closed, commitErr: test.err}
			observation, commitCalled, err := invokeRunnerCommitProtocol(context.Background(), transaction)
			if err != nil || !commitCalled || !validRunnerCommitProtocolObservation(observation) || transaction.commitCalls != 1 {
				t.Fatalf("invoke result: observation=%+v transaction=%+v err=%v", observation, transaction, err)
			}
			facts, err := consumeRunnerCommitProtocolObservation(observation, transaction)
			if err != nil || facts.outcome != test.wantOutcome || facts.rejectionReason != test.wantReason || !facts.commitCalled || facts.readyForQuery != test.wantReady || facts.connectionClosed != test.wantConnClose || liveRunnerCommitProtocolObservations() != 0 {
				t.Fatalf("commit facts: got=%+v want=%s/%s/%t/%t err=%v", facts, test.wantOutcome, test.wantReason, test.wantReady, test.wantConnClose, err)
			}
			if replay, replayErr := consumeRunnerCommitProtocolObservation(observation, transaction); replay != (runnerCommitProtocolFacts{}) || !IsCode(replayErr, CodeTransactionBoundary) || transaction.commitCalls != 1 {
				t.Fatalf("commit observation replayed: facts=%+v err=%v transaction=%+v", replay, replayErr, transaction)
			}
		})
	}
}

func TestRunnerCommitProtocolRejectsUnavailablePreflightWithoutCommit(t *testing.T) {
	for _, test := range []struct {
		name     string
		ctx      func() context.Context
		build    func() MigrationTransaction
		wantCode ErrorCode
	}{
		{"nil-context", func() context.Context { return nil }, func() MigrationTransaction { return &commitProtocolFake{status: 'T'} }, CodeTransactionBoundary},
		{"canceled", func() context.Context { ctx, cancel := context.WithCancel(context.Background()); cancel(); return ctx }, func() MigrationTransaction { return &commitProtocolFake{status: 'T'} }, CodeContextCanceled},
		{"deadline", func() context.Context {
			ctx, cancel := context.WithDeadline(context.Background(), testExpiredTime())
			defer cancel()
			return ctx
		}, func() MigrationTransaction { return &commitProtocolFake{status: 'T'} }, CodeDeadlineExceeded},
		{"nil-transaction", func() context.Context { return context.Background() }, func() MigrationTransaction { return nil }, CodeTransactionBoundary},
		{"unsealed-transaction", func() context.Context { return context.Background() }, func() MigrationTransaction { return commitProtocolUnsealed{} }, CodeTransactionBoundary},
		{"already-claimed", func() context.Context { return context.Background() }, func() MigrationTransaction { return &commitProtocolFake{status: 'T', claimed: true} }, CodeTransactionBoundary},
		{"not-in-transaction", func() context.Context { return context.Background() }, func() MigrationTransaction { return &commitProtocolFake{status: 'I'} }, CodeTransactionBoundary},
		{"connection-closed", func() context.Context { return context.Background() }, func() MigrationTransaction { return &commitProtocolFake{status: 'T', closed: true} }, CodeTransactionBoundary},
	} {
		t.Run(test.name, func(t *testing.T) {
			transaction := test.build()
			observation, commitCalled, err := invokeRunnerCommitProtocol(test.ctx(), transaction)
			var stable *Error
			if observation != nil || commitCalled || !errors.As(err, &stable) || stable.Code != test.wantCode || stable.Op != "runner-commit-protocol" || stable.Err != nil || liveRunnerCommitProtocolObservations() != 0 {
				t.Fatalf("preflight result: observation=%+v err=%#v", observation, stable)
			}
			if fake, ok := transaction.(*commitProtocolFake); ok && fake.commitCalls != 0 {
				t.Fatalf("preflight invoked commit: %+v", fake)
			}
		})
	}
}

func TestRunnerCommitProtocolObservationRejectsLiteralCopyAndDrift(t *testing.T) {
	transaction := &commitProtocolFake{status: 'T', statusAfter: 'I'}
	observation, commitCalled, err := invokeRunnerCommitProtocol(context.Background(), transaction)
	if err != nil || !commitCalled || !validRunnerCommitProtocolObservation(observation) {
		t.Fatalf("invoke: observation=%+v err=%v", observation, err)
	}
	valueType := reflect.TypeOf(*observation)
	for index := 0; index < valueType.NumField(); index++ {
		if valueType.Field(index).PkgPath == "" {
			t.Fatalf("commit observation field %s became public", valueType.Field(index).Name)
		}
	}
	copyValue := *observation
	if validRunnerCommitProtocolObservation(&copyValue) {
		t.Fatal("commit observation copy remained valid")
	}
	if validRunnerCommitProtocolObservation(&runnerCommitProtocolObservation{}) {
		t.Fatal("commit observation literal remained valid")
	}
	original := observation.outcome
	observation.outcome = runnerCommitProtocolAmbiguous
	if validRunnerCommitProtocolObservation(observation) {
		t.Fatal("mutated commit observation remained valid")
	}
	observation.outcome = original
	if !validRunnerCommitProtocolObservation(observation) {
		t.Fatal("restored commit observation did not recover")
	}
	facts, err := consumeRunnerCommitProtocolObservation(observation, transaction)
	if err != nil || facts.outcome != runnerCommitProtocolCommitted || liveRunnerCommitProtocolObservations() != 0 {
		t.Fatalf("consume restored observation: facts=%+v err=%v", facts, err)
	}
}

func TestRunnerCommitProtocolHasOneCommitAndNoProductionConsumer(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "runner_commit_protocol.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	commitCalls := 0
	forbidden := map[string]bool{
		"AppendDurable": true, "Rollback": true, "BeginMigration": true, "Connect": true,
		"ExecuteStatement": true, "Insert": true, "Query": true, "QueryRow": true,
		"ProjectAuthority": true, "ProjectCatalog": true,
	}
	ast.Inspect(file, func(node ast.Node) bool {
		selector, ok := node.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if selector.Sel.Name == "Commit" {
			commitCalls++
		}
		if forbidden[selector.Sel.Name] {
			t.Fatalf("commit protocol acquired forbidden %s call edge", selector.Sel.Name)
		}
		return true
	})
	if commitCalls != 1 {
		t.Fatalf("commit protocol call count=%d want=1", commitCalls)
	}

	paths, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	symbols := map[string]bool{
		"runnerCommitProtocolOutcome": true, "runnerCommitProtocolCommitted": true,
		"runnerCommitProtocolRejected": true, "runnerCommitProtocolAmbiguous": true,
		"runnerCommitProtocol": true, "runnerCommitProtocolObservation": true,
		"runnerCommitProtocolRegistryRecord": true, "runnerCommitProtocolFacts": true,
		"runnerCommitProtocolRegistry": true, "invokeRunnerCommitProtocol": true,
		"classifyRunnerCommitProtocol": true, "sealRunnerCommitProtocolObservation": true,
		"consumeRunnerCommitProtocolObservation": true, "validRunnerCommitProtocolObservation": true,
		"validRunnerCommitProtocolFacts": true,
		"claimRunnerCommitProtocol":      true, "runnerCommitProtocolStatus": true,
		"runnerCommitProtocolConnectionClosed": true, "runnerCommitProtocolSealed": true,
	}
	pgxAllowed := map[string]bool{
		"runnerCommitProtocol": true, "claimRunnerCommitProtocol": true,
		"runnerCommitProtocolStatus": true, "runnerCommitProtocolConnectionClosed": true,
		"runnerCommitProtocolSealed": true,
	}
	runnerAllowed := map[string]bool{
		"runnerCommitProtocol": true, "runnerCommitProtocolFacts": true,
		"invokeRunnerCommitProtocol": true, "consumeRunnerCommitProtocolObservation": true,
		"validRunnerCommitProtocolFacts": true, "runnerCommitProtocolConnectionClosed": true,
	}
	for _, path := range paths {
		name := filepath.Base(path)
		if strings.HasSuffix(name, "_test.go") || name == "runner_commit_protocol.go" {
			continue
		}
		production, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(production, func(node ast.Node) bool {
			identifier, ok := node.(*ast.Ident)
			if !ok || !symbols[identifier.Name] {
				return true
			}
			allowed := name == "pgx.go" && pgxAllowed[identifier.Name] || name == "runner_transaction_commit.go" && runnerAllowed[identifier.Name]
			if !allowed {
				t.Fatalf("commit protocol %s acquired unreviewed production consumer %s", identifier.Name, name)
			}
			return true
		})
	}
}

type commitProtocolFake struct {
	status        byte
	closed        bool
	claimed       bool
	statusAfter   byte
	closedAfter   bool
	commitErr     error
	commitCalls   int
	rollbackCalls int
}

func (*commitProtocolFake) Query(context.Context, string, ...any) (Rows, error) {
	return commitProtocolRows{}, nil
}
func (*commitProtocolFake) QueryRow(context.Context, string, ...any) Row { return commitProtocolRow{} }
func (*commitProtocolFake) Exec(context.Context, string, ...any) (CommandTag, error) {
	return commitProtocolTag(0), nil
}
func (*commitProtocolFake) ExecuteStatement(context.Context, []byte) error { return nil }
func (transaction *commitProtocolFake) Boundary(context.Context, int64) (BoundaryState, error) {
	return BoundaryState{TxStatus: transaction.status}, nil
}
func (transaction *commitProtocolFake) Commit(context.Context) error {
	transaction.commitCalls++
	transaction.status = transaction.statusAfter
	transaction.closed = transaction.closedAfter
	return transaction.commitErr
}
func (transaction *commitProtocolFake) Rollback(context.Context) error {
	transaction.rollbackCalls++
	return nil
}
func (transaction *commitProtocolFake) claimRunnerCommitProtocol() bool {
	if transaction.claimed {
		return false
	}
	transaction.claimed = true
	return true
}
func (transaction *commitProtocolFake) runnerCommitProtocolStatus() byte { return transaction.status }
func (transaction *commitProtocolFake) runnerCommitProtocolConnectionClosed() bool {
	return transaction.closed
}
func (*commitProtocolFake) runnerCommitProtocolSealed() {}

type commitProtocolUnsealed struct{}

func (commitProtocolUnsealed) Query(context.Context, string, ...any) (Rows, error) {
	return commitProtocolRows{}, nil
}
func (commitProtocolUnsealed) QueryRow(context.Context, string, ...any) Row {
	return commitProtocolRow{}
}
func (commitProtocolUnsealed) Exec(context.Context, string, ...any) (CommandTag, error) {
	return commitProtocolTag(0), nil
}
func (commitProtocolUnsealed) ExecuteStatement(context.Context, []byte) error { return nil }
func (commitProtocolUnsealed) Boundary(context.Context, int64) (BoundaryState, error) {
	return BoundaryState{}, nil
}
func (commitProtocolUnsealed) Commit(context.Context) error   { return nil }
func (commitProtocolUnsealed) Rollback(context.Context) error { return nil }

type commitProtocolRows struct{}

func (commitProtocolRows) Next() bool        { return false }
func (commitProtocolRows) Scan(...any) error { return errors.New("no row") }
func (commitProtocolRows) Err() error        { return nil }
func (commitProtocolRows) Close()            {}

type commitProtocolRow struct{}

func (commitProtocolRow) Scan(...any) error { return errors.New("no row") }

type commitProtocolTag int64

func (tag commitProtocolTag) RowsAffected() int64 { return int64(tag) }

type commitProtocolTimeoutError struct{}

func (commitProtocolTimeoutError) Error() string   { return "secret-timeout" }
func (commitProtocolTimeoutError) Timeout() bool   { return true }
func (commitProtocolTimeoutError) Temporary() bool { return true }

func liveRunnerCommitProtocolObservations() int {
	count := 0
	runnerCommitProtocolRegistry.Range(func(_, _ any) bool {
		count++
		return true
	})
	return count
}
