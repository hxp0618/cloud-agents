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

func TestRunnerPreparedCurrentTransactionRejectsLiteralCopyReplayAndFieldDrift(t *testing.T) {
	fixture := newRunnerPreparedCurrentSessionFixture(t)
	prepared, err := bindRunnerPreparedCurrentSession(
		fixture.database, fixture.evidence, fixture.key, fixture.candidate, fixture.snapshot,
		fixture.ledger, fixture.authority, fixture.precondition, fixture.bundle, fixture.plans,
	)
	if err != nil {
		t.Fatal(err)
	}
	factory := &runnerPreflightProjectorFactory{}
	factory.initialize()
	runner := Runner{projectionFactory: factory}
	transaction, err := runner.prepareCurrentTransaction(context.Background(), prepared, fixture.bundle, fixture.plans)
	if err != nil || !validRunnerPreparedCurrentTransaction(transaction) {
		t.Fatalf("transaction preflight: transaction=%+v err=%v", transaction, err)
	}
	if validRunnerPreparedCurrentSession(prepared) || liveRunnerPreparedCurrentSessions() != 0 || liveRunnerPreparedCurrentTransactions() != 1 {
		t.Fatalf("prepared authority was not atomically consumed: prepared=%t live=%d/%d", validRunnerPreparedCurrentSession(prepared), liveRunnerPreparedCurrentSessions(), liveRunnerPreparedCurrentTransactions())
	}
	if replay, replayErr := runner.prepareCurrentTransaction(context.Background(), prepared, fixture.bundle, fixture.plans); replay != nil || !IsCode(replayErr, CodeTransactionBoundary) || !validRunnerPreparedCurrentTransaction(transaction) {
		t.Fatalf("consumed prepared authority replayed or damaged its successor: replay=%+v err=%v", replay, replayErr)
	}

	valueType := reflect.TypeOf(*transaction)
	for index := 0; index < valueType.NumField(); index++ {
		if valueType.Field(index).PkgPath == "" {
			t.Fatalf("prepared transaction field %s became public", valueType.Field(index).Name)
		}
	}
	copyValue := *transaction
	if err := closeRunnerPreparedCurrentTransaction(&copyValue, nil); !IsCode(err, CodeTransactionBoundary) || fixture.database.transaction.rollbackCalls != 0 || fixture.database.closeCalls != 0 || !validRunnerPreparedCurrentTransaction(transaction) {
		t.Fatalf("transaction copy changed original authority: err=%v transaction=%+v database=%+v", err, fixture.database.transaction, fixture.database)
	}
	if err := closeRunnerPreparedCurrentTransaction(&runnerPreparedCurrentTransaction{}, nil); !IsCode(err, CodeTransactionBoundary) || fixture.database.transaction.rollbackCalls != 0 || fixture.database.closeCalls != 0 {
		t.Fatalf("transaction literal escaped: err=%v", err)
	}

	originalCatalog := transaction.catalogDigest
	transaction.catalogDigest = testDigest("transaction-catalog-drift")
	assertPreparedTransactionDrift(t, transaction)
	transaction.catalogDigest = originalCatalog

	originalSnapshot := transaction.snapshotDigest
	transaction.snapshotDigest[0] ^= 0xff
	assertPreparedTransactionDrift(t, transaction)
	transaction.snapshotDigest = originalSnapshot

	originalPlan := transaction.dispatch.planDigest
	transaction.dispatch.planDigest[0] ^= 0xff
	assertPreparedTransactionDrift(t, transaction)
	transaction.dispatch.planDigest = originalPlan

	originalSession := transaction.session
	transaction.session = newRunnerPreflightSession()
	assertPreparedTransactionDrift(t, transaction)
	transaction.session = originalSession

	fixture.database.transaction.status = '?'
	assertPreparedTransactionDrift(t, transaction)
	fixture.database.transaction.status = 'T'

	originalState := fixture.evidence.snapshot.state
	fixture.evidence.snapshot.state = RecoveryDivergent
	assertPreparedTransactionDrift(t, transaction)
	fixture.evidence.snapshot.state = originalState

	if !validRunnerPreparedCurrentTransaction(transaction) {
		t.Fatal("restored transaction authority did not recover its immutable binding")
	}
	originalKey := transaction.key
	originalTransaction := transaction.transaction
	rogueTransaction := newRunnerPreflightTransaction(fixture.database)
	rogueTransaction.active = true
	rogueTransaction.status = 'T'
	transaction.key++
	transaction.transaction = rogueTransaction
	err = closeRunnerPreparedCurrentTransaction(transaction, nil)
	if !IsCode(err, CodeTransactionBoundary) || originalTransaction != fixture.database.transaction || fixture.database.transaction.rollbackCalls != 1 || fixture.database.transaction.status != 'I' || rogueTransaction.rollbackCalls != 0 || fixture.database.unlockCalls != 1 || !reflect.DeepEqual(fixture.database.unlockKeys, []int64{originalKey}) || fixture.database.closeCalls != 1 || liveRunnerPreparedCurrentTransactions() != 0 {
		t.Fatalf("drifted transaction close did not use registry ownership: err=%v transaction=%+v database=%+v live=%d", err, fixture.database.transaction, fixture.database, liveRunnerPreparedCurrentTransactions())
	}
	if err := closeRunnerEvidenceOwnership(fixture.evidence, fixture.candidate); err != nil {
		t.Fatal(err)
	}
}

func TestRunnerTransactionPreflightFaultsAlwaysRollbackAndClose(t *testing.T) {
	for _, test := range []struct {
		name         string
		configure    func(*runnerPreparedCurrentSessionFixture, *runnerPreflightProjectorFactory)
		wantCode     ErrorCode
		wantOp       string
		wantRollback int
	}{
		{"begin", func(f *runnerPreparedCurrentSessionFixture, _ *runnerPreflightProjectorFactory) {
			f.database.beginErr = errors.New("secret-begin")
		}, CodeTransactionBoundary, "runner-transaction-begin", 0},
		{"begin-closed-result", func(f *runnerPreparedCurrentSessionFixture, _ *runnerPreflightProjectorFactory) {
			f.database.beginErr = errors.New("secret-begin")
			f.database.beginReturnsOnError = true
		}, CodeTransactionBoundary, "runner-transaction-begin", 1},
		{"begin-nil-result", func(f *runnerPreparedCurrentSessionFixture, _ *runnerPreflightProjectorFactory) {
			f.database.beginReturnsNil = true
		}, CodeTransactionBoundary, "runner-transaction-begin", 0},
		{"projection-profile", func(f *runnerPreparedCurrentSessionFixture, _ *runnerPreflightProjectorFactory) {
			f.database.transaction.profileEnterErr = errors.New("secret-projection-profile")
		}, CodeTransactionBoundary, "runner-transaction-profile", 1},
		{"snapshot-read", func(f *runnerPreparedCurrentSessionFixture, _ *runnerPreflightProjectorFactory) {
			f.database.transaction.metadataReadErr = errors.New("secret-snapshot")
		}, CodeProjectionCatalogQueryFailed, "runner-transaction-snapshot", 1},
		{"snapshot-status-change", func(f *runnerPreparedCurrentSessionFixture, _ *runnerPreflightProjectorFactory) {
			f.database.transaction.afterMetadataScan = func() { f.database.transaction.status = '?' }
		}, CodeProjectionSnapshotInvalid, "runner-transaction-snapshot", 1},
		{"snapshot-identity", func(f *runnerPreparedCurrentSessionFixture, _ *runnerPreflightProjectorFactory) {
			f.database.transaction.metadata[1] = "other_database"
		}, CodeProjectionMetadataMismatch, "runner-transaction-snapshot", 1},
		{"projector", func(_ *runnerPreparedCurrentSessionFixture, factory *runnerPreflightProjectorFactory) {
			factory.factoryErr[AuthorityPhaseMigrationTransaction] = errors.New("secret-projector")
		}, CodeTransactionBoundary, "runner-transaction-projector", 1},
		{"authority", func(_ *runnerPreparedCurrentSessionFixture, factory *runnerPreflightProjectorFactory) {
			factory.projectionErr[AuthorityPhaseMigrationTransaction] = fail(CodeAuthorityDrift, "fake", "secret", errors.New("secret-authority"))
		}, CodeAuthorityDrift, "runner-transaction-authority", 1},
		{"authority-result", func(_ *runnerPreparedCurrentSessionFixture, factory *runnerPreflightProjectorFactory) {
			factory.mutateResult[AuthorityPhaseMigrationTransaction] = func(result *ProjectionResult[AuthorityProjection]) {
				result.Digest = testDigest("wrong-transaction-authority")
			}
		}, CodeAuthorityDrift, "runner-transaction-authority", 1},
		{"precondition", func(_ *runnerPreparedCurrentSessionFixture, factory *runnerPreflightProjectorFactory) {
			factory.preconditionErr = fail(CodeCatalogDrift, "fake", "secret", errors.New("secret-precondition"))
		}, CodeCatalogDrift, "runner-transaction-precondition", 1},
		{"precondition-result", func(_ *runnerPreparedCurrentSessionFixture, factory *runnerPreflightProjectorFactory) {
			factory.mutatePrecondition = func(result *ProjectionResult[CatalogStateProjection]) {
				result.Digest = testDigest("wrong-transaction-precondition")
			}
		}, CodeCatalogDrift, "runner-transaction-precondition", 1},
		{"execution-profile", func(f *runnerPreparedCurrentSessionFixture, _ *runnerPreflightProjectorFactory) {
			f.database.transaction.profileRestoreErr = errors.New("secret-execution-profile")
		}, CodeTransactionBoundary, "runner-transaction-execution-profile", 1},
		{"boundary", func(f *runnerPreparedCurrentSessionFixture, _ *runnerPreflightProjectorFactory) {
			f.database.transaction.boundaryErr = errors.New("secret-boundary")
		}, CodeTransactionBoundary, "runner-transaction-boundary", 1},
		{"boundary-role", func(f *runnerPreparedCurrentSessionFixture, _ *runnerPreflightProjectorFactory) {
			f.database.transaction.boundaryMutate = func(boundary *BoundaryState) { boundary.CurrentUser = "other_role" }
		}, CodeTransactionBoundary, "runner-transaction-boundary", 1},
		{"evidence-drift", func(f *runnerPreparedCurrentSessionFixture, _ *runnerPreflightProjectorFactory) {
			f.database.transaction.boundaryMutate = func(*BoundaryState) { f.evidence.snapshot.state = RecoveryDivergent }
		}, CodeEvidenceJournalFailed, "runner-transaction-evidence", 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newRunnerPreparedCurrentSessionFixture(t)
			prepared, err := bindRunnerPreparedCurrentSession(
				fixture.database, fixture.evidence, fixture.key, fixture.candidate, fixture.snapshot,
				fixture.ledger, fixture.authority, fixture.precondition, fixture.bundle, fixture.plans,
			)
			if err != nil {
				t.Fatal(err)
			}
			factory := &runnerPreflightProjectorFactory{}
			factory.initialize()
			test.configure(&fixture, factory)
			transaction, err := (&Runner{projectionFactory: factory}).prepareCurrentTransaction(context.Background(), prepared, fixture.bundle, fixture.plans)
			var migrationErr *Error
			if transaction != nil || !errors.As(err, &migrationErr) || migrationErr.Code != test.wantCode || migrationErr.Op != test.wantOp || migrationErr.Err != nil || containsErrorText(err, "secret-") {
				t.Fatalf("transaction fault mapping: transaction=%+v err=%#v", transaction, migrationErr)
			}
			if fixture.database.beginCalls != 1 || fixture.database.transaction.rollbackCalls != test.wantRollback || fixture.database.transaction.executeCalls != 0 || fixture.database.transaction.execCalls != 0 || fixture.database.transaction.commitCalls != 0 || fixture.database.unlockCalls != 1 || fixture.database.closeCalls != 1 || liveRunnerPreparedCurrentSessions() != 0 || liveRunnerPreparedCurrentTransactions() != 0 {
				t.Fatalf("transaction fault escaped cleanup boundary: database=%+v transaction=%+v live=%d/%d", fixture.database, fixture.database.transaction, liveRunnerPreparedCurrentSessions(), liveRunnerPreparedCurrentTransactions())
			}
			if cleanupErr := closeRunnerEvidenceOwnership(fixture.evidence, fixture.candidate); cleanupErr != nil {
				t.Fatal(cleanupErr)
			}
		})
	}
}

func TestRunnerTransactionClaimRejectsChangedPlanBeforeBeginAndClosesPreparedSession(t *testing.T) {
	fixture := newRunnerPreparedCurrentSessionFixture(t)
	prepared, err := bindRunnerPreparedCurrentSession(
		fixture.database, fixture.evidence, fixture.key, fixture.candidate, fixture.snapshot,
		fixture.ledger, fixture.authority, fixture.precondition, fixture.bundle, fixture.plans,
	)
	if err != nil {
		t.Fatal(err)
	}
	changed := append([]StatementPlan(nil), fixture.plans...)
	changed[0].sqlBytes = append([]byte(nil), changed[0].sqlBytes...)
	changed[0].sqlBytes[0] ^= 0xff
	transaction, err := (&Runner{}).prepareCurrentTransaction(context.Background(), prepared, fixture.bundle, changed)
	var migrationErr *Error
	if transaction != nil || !errors.As(err, &migrationErr) || migrationErr.Code != CodeUntrusted || migrationErr.Op != "runner-transaction-claim" || migrationErr.Err != nil {
		t.Fatalf("changed plan claim: transaction=%+v err=%#v", transaction, migrationErr)
	}
	if fixture.database.beginCalls != 0 || fixture.database.transaction.rollbackCalls != 0 || fixture.database.unlockCalls != 1 || fixture.database.closeCalls != 1 || liveRunnerPreparedCurrentSessions() != 0 || liveRunnerPreparedCurrentTransactions() != 0 {
		t.Fatalf("changed plan crossed begin or leaked prepared ownership: database=%+v transaction=%+v", fixture.database, fixture.database.transaction)
	}
	if cleanupErr := closeRunnerEvidenceOwnership(fixture.evidence, fixture.candidate); cleanupErr != nil {
		t.Fatal(cleanupErr)
	}
}

func TestRunnerPreparedTransactionCleanupPrecedenceAndIndependentAttempts(t *testing.T) {
	for _, test := range []struct {
		name      string
		configure func(*runnerPreparedCurrentSessionFixture)
		wantOp    string
	}{
		{"rollback", func(f *runnerPreparedCurrentSessionFixture) {
			f.database.transaction.rollbackErr = errors.New("secret-rollback")
		}, "runner-transaction-rollback"},
		{"rollback-status", func(f *runnerPreparedCurrentSessionFixture) {
			f.database.transaction.rollbackLeavesOpen = true
		}, "runner-transaction-rollback"},
		{"close-dominates-rollback", func(f *runnerPreparedCurrentSessionFixture) {
			f.database.transaction.rollbackErr = errors.New("secret-rollback")
			f.database.transaction.rollbackLeavesOpen = true
			f.database.closeErr = errors.New("secret-close")
		}, "runner-database-close"},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newRunnerPreparedCurrentSessionFixture(t)
			prepared, err := bindRunnerPreparedCurrentSession(
				fixture.database, fixture.evidence, fixture.key, fixture.candidate, fixture.snapshot,
				fixture.ledger, fixture.authority, fixture.precondition, fixture.bundle, fixture.plans,
			)
			if err != nil {
				t.Fatal(err)
			}
			factory := &runnerPreflightProjectorFactory{}
			factory.initialize()
			transaction, err := (&Runner{projectionFactory: factory}).prepareCurrentTransaction(context.Background(), prepared, fixture.bundle, fixture.plans)
			if err != nil {
				t.Fatal(err)
			}
			test.configure(&fixture)
			err = closeRunnerPreparedCurrentTransaction(transaction, fail(CodeProjectionNotImplemented, "primary", "primary", nil))
			var migrationErr *Error
			if !errors.As(err, &migrationErr) || migrationErr.Code != CodeTransactionBoundary || migrationErr.Op != test.wantOp || migrationErr.Err != nil || containsErrorText(err, "secret-") {
				t.Fatalf("cleanup precedence=%#v", migrationErr)
			}
			if fixture.database.transaction.rollbackCalls != 1 || fixture.database.unlockCalls != 1 || fixture.database.closeCalls != 1 || liveRunnerPreparedCurrentTransactions() != 0 {
				t.Fatalf("cleanup did not attempt rollback/unlock/close independently: transaction=%+v database=%+v", fixture.database.transaction, fixture.database)
			}
			if cleanupErr := closeRunnerEvidenceOwnership(fixture.evidence, fixture.candidate); cleanupErr != nil {
				t.Fatal(cleanupErr)
			}
		})
	}
}

func TestRunnerPreparedTransactionHasOnlyReviewedProductionConsumers(t *testing.T) {
	paths, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	symbols := map[string]bool{
		"runnerPreparedCurrentTransaction": true, "runnerPreparedCurrentTransactionBinding": true,
		"runnerPreparedCurrentTransactionRegistryRecord": true, "runnerPreparedCurrentTransactionRegistry": true,
		"prepareCurrentTransaction": true, "validRunnerPreparedCurrentTransaction": true,
		"closeRunnerPreparedCurrentTransaction": true,
	}
	allowed := map[string]map[string]bool{
		"runner_transaction_preflight.go": nil,
		"runner.go":                       {"prepareCurrentTransaction": true, "closeRunnerPreparedCurrentTransaction": true},
		"runner_statement_preflight.go": {
			"runnerPreparedCurrentTransaction": true, "runnerPreparedCurrentTransactionBinding": true,
			"runnerPreparedCurrentTransactionRegistryRecord": true, "runnerPreparedCurrentTransactionRegistry": true,
			"validRunnerPreparedCurrentTransaction": true, "closeRunnerPreparedCurrentTransaction": true,
		},
	}
	for _, path := range paths {
		name := filepath.Base(path)
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			identifier, ok := node.(*ast.Ident)
			if !ok || !symbols[identifier.Name] || name == "runner_transaction_preflight.go" || allowed[name][identifier.Name] {
				return true
			}
			t.Fatalf("prepared transaction authority %s acquired unreviewed production consumer %s", identifier.Name, name)
			return false
		})
	}
}

func TestRunnerTransactionPreflightHasNoExecutionOrCommitCallEdge(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "runner_transaction_preflight.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	forbidden := map[string]bool{
		"ExecuteStatement": true, "AppendDurable": true, "ReserveAndActivateSuccessor": true,
		"Insert": true, "Commit": true, "Exec": true, "Query": true, "QueryRow": true,
		"ProjectCatalog": true, "ProjectTransitionState": true, "StateMigrate": true,
	}
	beginCalls, rollbackCalls := 0, 0
	ast.Inspect(file, func(node ast.Node) bool {
		selector, ok := node.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if forbidden[selector.Sel.Name] {
			t.Fatalf("transaction preflight acquired forbidden %s call edge", selector.Sel.Name)
		}
		switch selector.Sel.Name {
		case "BeginMigration":
			beginCalls++
		case "Rollback":
			rollbackCalls++
		}
		return true
	})
	if beginCalls != 1 || rollbackCalls != 1 {
		t.Fatalf("transaction lifecycle call edges: begin=%d rollback=%d", beginCalls, rollbackCalls)
	}
}

func TestRunnerTransactionProjectionProfileHasOnlyReviewedProductionConsumers(t *testing.T) {
	paths, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	symbols := map[string]bool{
		"runnerTransactionProjectionProfile": true, "enterRunnerProjectionProfile": true,
		"restoreRunnerExecutionProfile": true, "runnerTransactionProjectionProfileSealed": true,
	}
	allowed := map[string]bool{"pgx.go": true, "runner_transaction_preflight.go": true, "runner_statement_after.go": true, "runner_preledger.go": true}
	successKernelAllowed := map[string]bool{
		"runnerTransactionProjectionProfile": true, "enterRunnerProjectionProfile": true,
		"restoreRunnerExecutionProfile": true,
	}
	for _, path := range paths {
		name := filepath.Base(path)
		if strings.HasSuffix(name, "_test.go") || allowed[name] {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			identifier, ok := node.(*ast.Ident)
			if ok && symbols[identifier.Name] && !(name == "runner_ledger_entry_success_kernel.go" && successKernelAllowed[identifier.Name]) {
				t.Fatalf("transaction projection profile %s acquired unreviewed production consumer %s", identifier.Name, name)
			}
			return true
		})
	}
	var _ runnerTransactionProjectionProfile = (*runnerPreflightTransaction)(nil)
}

func assertPreparedTransactionDrift(t *testing.T, transaction *runnerPreparedCurrentTransaction) {
	t.Helper()
	if validRunnerPreparedCurrentTransaction(transaction) {
		t.Fatal("mutated transaction authority remained valid")
	}
}

func liveRunnerPreparedCurrentTransactions() int {
	count := 0
	runnerPreparedCurrentTransactionRegistry.Range(func(_, _ any) bool {
		count++
		return true
	})
	return count
}
