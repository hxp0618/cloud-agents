package migration

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestRunnerReadbackCurrentLedgerConsumesIntermediateAndReadsExactRowOnce(t *testing.T) {
	fixture, durable, runner := newRunnerLedgerReadbackFixture(t)
	transaction := fixture.database.transaction
	beforeBoundary := transaction.boundaryCalls
	bundle, err := LoadRuntimeBundle(fixture.candidate.runtimeArtifact.bytes, fixture.candidate.verifiedRun.currentDecision.decision)
	if err != nil {
		t.Fatal(err)
	}
	wantRow := commitIntentLedgerRow(bundle.Manifest.SchemaBundle.Migrations[0], bundle.Manifest.SchemaBundleDigest)
	readback, err := runner.insertAndReadbackCurrentLedger(context.Background(), durable)
	if err != nil || !validRunnerReadbackCurrentLedger(readback) {
		t.Fatalf("insert/read ledger: readback=%+v err=%v", readback, err)
	}
	if validRunnerDurableFinalIntermediate(durable) || liveRunnerDurableFinalIntermediates() != 0 || liveRunnerReadbackCurrentLedgers() != 1 {
		t.Fatalf("intermediate authority was not atomically consumed: durable=%t live=%d/%d", validRunnerDurableFinalIntermediate(durable), liveRunnerDurableFinalIntermediates(), liveRunnerReadbackCurrentLedgers())
	}
	if transaction.ledgerInsertCalls != 1 || transaction.ledgerReadCalls != 1 || fixture.database.backend.ledgerInsertCalls != 1 || transaction.pendingLedger == nil || !runnerCanonicalEqual(readback.ledgerRow, wantRow) || !runnerCanonicalEqual(*transaction.pendingLedger, ledgerRowFor(bundle.Manifest.SchemaBundle.Migrations[0], bundle.Manifest.SchemaBundleDigest)) {
		t.Fatalf("ledger mutation/readback was not exact: readback=%+v transaction=%+v", readback, transaction)
	}
	wantPrefix, prefixErr := LedgerPrefixDigest([]CommitIntentLedgerRow{wantRow})
	if prefixErr != nil || readback.ledgerPrefixDigest != wantPrefix || readback.ledgerHead != wantRow.MigrationID || readback.ledgerLength != 1 {
		t.Fatalf("ledger prefix differs: readback=%+v err=%v", readback, prefixErr)
	}
	if transaction.boundaryCalls != beforeBoundary+1 || transaction.executeCalls != 1 || fixture.evidence.journal.appendCalls != 2 || transaction.commitCalls != 0 || transaction.execCalls != 0 || fixture.database.backend.commitCalls != 0 || transaction.status != 'T' {
		t.Fatalf("ledger readback crossed append/commit boundary: transaction=%+v journal=%+v", transaction, fixture.evidence.journal)
	}
	if len(transaction.steps) < 3 || !reflect.DeepEqual(transaction.steps[len(transaction.steps)-3:], []string{"ledger-insert", "ledger-readback", "boundary"}) {
		t.Fatalf("ledger mutation order differs: %v", transaction.steps)
	}
	if replay, replayErr := runner.insertAndReadbackCurrentLedger(context.Background(), durable); replay != nil || !IsCode(replayErr, CodeTransactionBoundary) || transaction.ledgerInsertCalls != 1 || !validRunnerReadbackCurrentLedger(readback) {
		t.Fatalf("consumed intermediate replayed ledger mutation or damaged successor: replay=%+v err=%v", replay, replayErr)
	}
	if err := closeRunnerReadbackCurrentLedger(readback, nil); err != nil || transaction.rollbackCalls != 1 || transaction.pendingLedger != nil || transaction.status != 'I' || fixture.database.unlockCalls != 1 || fixture.database.closeCalls != 1 || liveRunnerReadbackCurrentLedgers() != 0 {
		t.Fatalf("ledger readback close did not release database ownership: err=%v transaction=%+v database=%+v", err, transaction, fixture.database)
	}
	if err := closeRunnerEvidenceOwnership(fixture.evidence, fixture.candidate); err != nil {
		t.Fatal(err)
	}
}

func TestRunnerReadbackCurrentLedgerRejectsLiteralCopyAndFieldDrift(t *testing.T) {
	fixture, durable, runner := newRunnerLedgerReadbackFixture(t)
	readback, err := runner.insertAndReadbackCurrentLedger(context.Background(), durable)
	if err != nil || !validRunnerReadbackCurrentLedger(readback) {
		t.Fatalf("insert/read ledger: readback=%+v err=%v", readback, err)
	}
	valueType := reflect.TypeOf(*readback)
	for index := 0; index < valueType.NumField(); index++ {
		if valueType.Field(index).PkgPath == "" {
			t.Fatalf("ledger readback field %s became public", valueType.Field(index).Name)
		}
	}
	copyValue := *readback
	if err := closeRunnerReadbackCurrentLedger(&copyValue, nil); !IsCode(err, CodeTransactionBoundary) || fixture.database.transaction.rollbackCalls != 0 || !validRunnerReadbackCurrentLedger(readback) {
		t.Fatalf("copy changed original authority: err=%v transaction=%+v", err, fixture.database.transaction)
	}
	if err := closeRunnerReadbackCurrentLedger(&runnerReadbackCurrentLedger{}, nil); !IsCode(err, CodeTransactionBoundary) || fixture.database.transaction.rollbackCalls != 0 {
		t.Fatalf("literal escaped: err=%v", err)
	}

	originalName := readback.ledgerRow.MigrationName
	readback.ledgerRow.MigrationName += "-drift"
	assertRunnerLedgerReadbackDrift(t, readback)
	readback.ledgerRow.MigrationName = originalName

	originalPrefix := readback.ledgerPrefixDigest
	readback.ledgerPrefixDigest = testDigest("other-ledger-prefix")
	assertRunnerLedgerReadbackDrift(t, readback)
	readback.ledgerPrefixDigest = originalPrefix

	originalBoundary := readback.boundary
	readback.boundary.CurrentUser = "other_role"
	assertRunnerLedgerReadbackDrift(t, readback)
	readback.boundary = originalBoundary

	originalState := fixture.evidence.snapshot.state
	fixture.evidence.snapshot.state = RecoveryDivergent
	assertRunnerLedgerReadbackDrift(t, readback)
	fixture.evidence.snapshot.state = originalState

	fixture.database.transaction.status = 'E'
	assertRunnerLedgerReadbackDrift(t, readback)
	fixture.database.transaction.status = 'T'
	if !validRunnerReadbackCurrentLedger(readback) {
		t.Fatal("restored ledger readback did not recover its immutable binding")
	}

	originalKey := readback.key
	readback.key++
	err = closeRunnerReadbackCurrentLedger(readback, nil)
	if !IsCode(err, CodeTransactionBoundary) || fixture.database.transaction.rollbackCalls != 1 || !reflect.DeepEqual(fixture.database.unlockKeys, []int64{originalKey}) || fixture.database.closeCalls != 1 || liveRunnerReadbackCurrentLedgers() != 0 {
		t.Fatalf("drifted close did not use registry ownership: err=%v transaction=%+v database=%+v", err, fixture.database.transaction, fixture.database)
	}
	if err := closeRunnerEvidenceOwnership(fixture.evidence, fixture.candidate); err != nil {
		t.Fatal(err)
	}
}

func TestRunnerCurrentLedgerFaultsRollbackWithoutEvidenceAppendOrCommit(t *testing.T) {
	tests := []struct {
		name       string
		context    func() context.Context
		nilRunner  bool
		configure  func(*runnerPreparedCurrentSessionFixture)
		wantCode   ErrorCode
		wantOp     string
		wantInsert int
		wantRead   int
		revoke     bool
	}{
		{"nil-context", func() context.Context { return nil }, false, nil, CodeTransactionBoundary, "runner-ledger-readback", 0, 0, false},
		{"canceled", func() context.Context { ctx, cancel := context.WithCancel(context.Background()); cancel(); return ctx }, false, nil, CodeContextCanceled, "runner-ledger-readback", 0, 0, false},
		{"deadline", func() context.Context {
			ctx, cancel := context.WithDeadline(context.Background(), testExpiredTime())
			defer cancel()
			return ctx
		}, false, nil, CodeDeadlineExceeded, "runner-ledger-readback", 0, 0, false},
		{"nil-runner", func() context.Context { return context.Background() }, true, nil, CodeTransactionBoundary, "runner-ledger-readback", 0, 0, false},
		{"insert-error", nil, false, func(f *runnerPreparedCurrentSessionFixture) {
			f.database.transaction.ledgerInsertErr = errors.New("secret-insert")
		}, CodeInvalidLedger, "runner-ledger-write", 1, 0, false},
		{"insert-stable", nil, false, func(f *runnerPreparedCurrentSessionFixture) {
			f.database.transaction.ledgerInsertErr = fail(CodeInvalidLedger, "fake", "secret", errors.New("secret-insert"))
		}, CodeInvalidLedger, "runner-ledger-write", 1, 0, false},
		{"insert-canceled", nil, false, func(f *runnerPreparedCurrentSessionFixture) {
			f.database.transaction.ledgerInsertErr = context.Canceled
		}, CodeContextCanceled, "runner-ledger-write", 1, 0, false},
		{"read-error", nil, false, func(f *runnerPreparedCurrentSessionFixture) {
			f.database.transaction.ledgerReadErr = errors.New("secret-read")
		}, CodeInvalidLedger, "runner-ledger-write", 1, 1, false},
		{"read-nil", nil, false, func(f *runnerPreparedCurrentSessionFixture) {
			f.database.transaction.ledgerReadMutate = func([]LedgerRow) []LedgerRow { return nil }
		}, CodeInvalidLedger, "runner-ledger-readback", 1, 1, false},
		{"read-extra", nil, false, func(f *runnerPreparedCurrentSessionFixture) {
			f.database.transaction.ledgerReadMutate = func(rows []LedgerRow) []LedgerRow { return append(rows, rows[0]) }
		}, CodeInvalidLedger, "runner-ledger-readback", 1, 1, false},
		{"read-identity", nil, false, func(f *runnerPreparedCurrentSessionFixture) {
			f.database.transaction.ledgerReadMutate = func(rows []LedgerRow) []LedgerRow { rows[0].MigrationName += "-drift"; return rows }
		}, CodeInvalidLedger, "runner-ledger-readback", 1, 1, false},
		{"read-size", nil, false, func(f *runnerPreparedCurrentSessionFixture) {
			f.database.transaction.ledgerReadMutate = func(rows []LedgerRow) []LedgerRow { rows[0].SQLSizeBytes = -1; return rows }
		}, CodeInvalidLedger, "runner-ledger-readback", 1, 1, false},
		{"boundary", nil, false, func(f *runnerPreparedCurrentSessionFixture) {
			f.database.transaction.boundaryErr = errors.New("secret-boundary")
		}, CodeTransactionBoundary, "runner-ledger-boundary", 1, 1, false},
		{"boundary-role", nil, false, func(f *runnerPreparedCurrentSessionFixture) {
			f.database.transaction.boundaryMutate = func(boundary *BoundaryState) { boundary.CurrentUser = "other_role" }
		}, CodeTransactionBoundary, "runner-ledger-boundary", 1, 1, false},
		{"status-drift", nil, false, func(f *runnerPreparedCurrentSessionFixture) {
			f.database.transaction.boundaryMutate = func(*BoundaryState) { f.database.transaction.status = 'E' }
		}, CodeTransactionBoundary, "runner-ledger-boundary", 1, 1, false},
		{"evidence-drift", nil, false, func(f *runnerPreparedCurrentSessionFixture) {
			f.database.transaction.boundaryMutate = func(*BoundaryState) { f.evidence.snapshot.state = RecoveryDivergent }
		}, CodeEvidenceJournalFailed, "runner-ledger-evidence", 1, 1, true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture, durable, runner := newRunnerLedgerReadbackFixture(t)
			if test.configure != nil {
				test.configure(&fixture)
			}
			active := runner
			if test.nilRunner {
				active = nil
			}
			ctx := context.Background()
			if test.context != nil {
				ctx = test.context()
			}
			readback, err := active.insertAndReadbackCurrentLedger(ctx, durable)
			assertRunnerCurrentLedgerError(t, err, test.wantCode, test.wantOp)
			transaction := fixture.database.transaction
			if readback != nil || containsErrorText(err, "secret-") || transaction.ledgerInsertCalls != test.wantInsert || transaction.ledgerReadCalls != test.wantRead || transaction.rollbackCalls != 1 || transaction.pendingLedger != nil || transaction.commitCalls != 0 || transaction.execCalls != 0 || fixture.database.unlockCalls != 1 || fixture.database.closeCalls != 1 || fixture.evidence.journal.appendCalls != 2 || fixture.database.backend.commitCalls != 0 || liveRunnerDurableFinalIntermediates() != 0 || liveRunnerReadbackCurrentLedgers() != 0 {
				t.Fatalf("ledger fault escaped boundary: readback=%+v err=%v transaction=%+v database=%+v", readback, err, transaction, fixture.database)
			}
			if fixture.evidence.journal.cursor.Valid() == test.revoke {
				t.Fatalf("ledger fault cursor validity=%t revoke=%t", fixture.evidence.journal.cursor.Valid(), test.revoke)
			}
			if cleanupErr := closeRunnerEvidenceOwnership(fixture.evidence, fixture.candidate); cleanupErr != nil {
				t.Fatal(cleanupErr)
			}
		})
	}
}

func TestRunnerCurrentLedgerCleanupFailureDominatesAndAttemptsEveryRelease(t *testing.T) {
	fixture, durable, runner := newRunnerLedgerReadbackFixture(t)
	fixture.database.transaction.ledgerInsertErr = errors.New("secret-insert")
	fixture.database.transaction.rollbackErr = errors.New("secret-rollback")
	fixture.database.transaction.rollbackLeavesOpen = true
	fixture.database.unlockErr = errors.New("secret-unlock")
	fixture.database.closeErr = errors.New("secret-close")
	readback, err := runner.insertAndReadbackCurrentLedger(context.Background(), durable)
	assertRunnerCurrentLedgerError(t, err, CodeTransactionBoundary, "runner-database-close")
	if readback != nil || containsErrorText(err, "secret-") || fixture.database.transaction.rollbackCalls != 1 || fixture.database.unlockCalls != 1 || fixture.database.closeCalls != 1 || fixture.database.transaction.ledgerInsertCalls != 1 || fixture.evidence.journal.appendCalls != 2 || fixture.database.backend.commitCalls != 0 || liveRunnerReadbackCurrentLedgers() != 0 {
		t.Fatalf("ledger cleanup precedence/attempts: readback=%+v err=%v transaction=%+v database=%+v", readback, err, fixture.database.transaction, fixture.database)
	}
	if cleanupErr := closeRunnerEvidenceOwnership(fixture.evidence, fixture.candidate); cleanupErr != nil {
		t.Fatal(cleanupErr)
	}
}

func TestRunnerCurrentLedgerHasOneClosedMutationAndNoEvidenceOrCommitEdge(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "runner_ledger_readback.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	mutationCalls, boundaryCalls := 0, 0
	forbidden := map[string]bool{
		"AppendDurable": true, "Commit": true, "ExecuteStatement": true, "Exec": true, "Query": true, "QueryRow": true,
		"ProjectAuthority": true, "ProjectCatalog": true, "ProjectTransitionState": true, "ProjectPrecondition": true,
	}
	ast.Inspect(file, func(node ast.Node) bool {
		selector, ok := node.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		switch selector.Sel.Name {
		case "insertAndReadRunnerLedgerRow":
			mutationCalls++
		case "Boundary":
			boundaryCalls++
		}
		if forbidden[selector.Sel.Name] {
			t.Fatalf("ledger readback acquired forbidden %s call edge", selector.Sel.Name)
		}
		return true
	})
	if mutationCalls != 1 || boundaryCalls != 1 {
		t.Fatalf("ledger readback call edges: mutation=%d boundary=%d", mutationCalls, boundaryCalls)
	}
}

func TestPGXRunnerLedgerAdapterAlwaysInsertsThenReadsOnce(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "pgx.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	insertCalls, readCalls := 0, 0
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Name.Name != "insertAndReadRunnerLedgerRow" {
			continue
		}
		ast.Inspect(function.Body, func(node ast.Node) bool {
			selector, ok := node.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			switch selector.Sel.Name {
			case "Insert":
				insertCalls++
			case "Read":
				readCalls++
			}
			return true
		})
	}
	if insertCalls != 1 || readCalls != 1 {
		t.Fatalf("PGX ledger adapter call edges: insert=%d read=%d", insertCalls, readCalls)
	}
}

func TestRunnerTransactionLedgerHasNoUnreviewedProductionConsumer(t *testing.T) {
	paths, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	symbols := map[string]bool{
		"runnerTransactionLedger": true, "insertAndReadRunnerLedgerRow": true,
		"runnerTransactionLedgerSealed": true,
	}
	for _, path := range paths {
		name := filepath.Base(path)
		if strings.HasSuffix(name, "_test.go") || name == "pgx.go" || name == "runner_ledger_readback.go" {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			identifier, ok := node.(*ast.Ident)
			if ok && symbols[identifier.Name] {
				t.Fatalf("runner ledger adapter %s acquired unreviewed production consumer %s", identifier.Name, name)
			}
			return true
		})
	}
}

func TestRunnerReadbackCurrentLedgerHasNoProductionConsumer(t *testing.T) {
	paths, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	symbols := map[string]bool{
		"runnerReadbackCurrentLedger": true, "runnerReadbackCurrentLedgerBinding": true,
		"runnerReadbackCurrentLedgerRegistryRecord": true, "runnerReadbackCurrentLedgerRegistry": true,
		"runnerCurrentLedgerSeed": true, "insertAndReadbackCurrentLedger": true,
		"consumeRunnerDurableFinalIntermediate": true, "bindRunnerReadbackCurrentLedger": true,
		"validRunnerReadbackCurrentLedger": true, "closeRunnerReadbackCurrentLedger": true,
	}
	for _, path := range paths {
		name := filepath.Base(path)
		if strings.HasSuffix(name, "_test.go") || name == "runner_ledger_readback.go" {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			identifier, ok := node.(*ast.Ident)
			if ok && symbols[identifier.Name] {
				t.Fatalf("ledger readback %s acquired unreviewed production consumer %s", identifier.Name, name)
			}
			return true
		})
	}
}

func newRunnerLedgerReadbackFixture(t *testing.T) (runnerPreparedCurrentSessionFixture, *runnerDurableFinalIntermediate, *Runner) {
	t.Helper()
	fixture, preledger, runner := newRunnerFinalIntermediateFixture(t)
	durable, err := runner.appendCurrentFinalIntermediate(context.Background(), preledger)
	if err != nil || !validRunnerDurableFinalIntermediate(durable) {
		t.Fatalf("ledger readback fixture intermediate: durable=%+v err=%v", durable, err)
	}
	return fixture, durable, runner
}

func assertRunnerLedgerReadbackDrift(t *testing.T, readback *runnerReadbackCurrentLedger) {
	t.Helper()
	if validRunnerReadbackCurrentLedger(readback) {
		t.Fatal("mutated ledger readback remained valid")
	}
}

func liveRunnerReadbackCurrentLedgers() int {
	count := 0
	runnerReadbackCurrentLedgerRegistry.Range(func(_, _ any) bool {
		count++
		return true
	})
	return count
}

func assertRunnerCurrentLedgerError(t *testing.T, err error, code ErrorCode, op string) {
	t.Helper()
	var migrationErr *Error
	if !errors.As(err, &migrationErr) || migrationErr.Code != code || migrationErr.Op != op || migrationErr.Err != nil {
		t.Fatalf("ledger readback error: got=%#v want=%s/%s", migrationErr, code, op)
	}
}
