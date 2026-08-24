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

func TestRunnerPreparedCurrentStatementRejectsLiteralCopyReplayAndFieldDrift(t *testing.T) {
	fixture, transaction, factory := newRunnerPreparedCurrentTransactionFixture(t)
	transactionWideSnapshot := transaction.snapshotDigest
	statement, err := (&Runner{projectionFactory: factory}).prepareCurrentStatement(context.Background(), transaction, fixture.bundle, fixture.plans)
	if err != nil || !validRunnerPreparedCurrentStatement(statement) {
		t.Fatalf("statement preflight: statement=%+v err=%v", statement, err)
	}
	if validRunnerPreparedCurrentTransaction(transaction) || liveRunnerPreparedCurrentTransactions() != 0 || liveRunnerPreparedCurrentStatements() != 1 {
		t.Fatalf("transaction authority was not atomically consumed: transaction=%t live=%d/%d", validRunnerPreparedCurrentTransaction(transaction), liveRunnerPreparedCurrentTransactions(), liveRunnerPreparedCurrentStatements())
	}
	if statement.statementIndex != 0 || statement.statementPlanDigest != runnerStatementPlanDigest(fixture.plans[0]) || statement.snapshotDigest == transactionWideSnapshot {
		t.Fatalf("statement identity did not bind index zero and its distinct snapshot: statement=%+v", statement)
	}
	if replay, replayErr := (&Runner{projectionFactory: factory}).prepareCurrentStatement(context.Background(), transaction, fixture.bundle, fixture.plans); replay != nil || !IsCode(replayErr, CodeTransactionBoundary) || !validRunnerPreparedCurrentStatement(statement) {
		t.Fatalf("consumed transaction authority replayed or damaged successor: replay=%+v err=%v", replay, replayErr)
	}
	valueType := reflect.TypeOf(*statement)
	for index := 0; index < valueType.NumField(); index++ {
		if valueType.Field(index).PkgPath == "" {
			t.Fatalf("prepared statement field %s became public", valueType.Field(index).Name)
		}
	}
	copyValue := *statement
	if err := closeRunnerPreparedCurrentStatement(&copyValue, nil); !IsCode(err, CodeTransactionBoundary) || fixture.database.transaction.rollbackCalls != 0 || fixture.database.closeCalls != 0 || !validRunnerPreparedCurrentStatement(statement) {
		t.Fatalf("statement copy changed original authority: err=%v transaction=%+v database=%+v", err, fixture.database.transaction, fixture.database)
	}
	if err := closeRunnerPreparedCurrentStatement(&runnerPreparedCurrentStatement{}, nil); !IsCode(err, CodeTransactionBoundary) || fixture.database.transaction.rollbackCalls != 0 || fixture.database.closeCalls != 0 {
		t.Fatalf("statement literal escaped: err=%v", err)
	}

	originalPlan := statement.statementPlanDigest
	statement.statementPlanDigest[0] ^= 0xff
	assertPreparedStatementDrift(t, statement)
	statement.statementPlanDigest = originalPlan

	originalSnapshot := statement.snapshotDigest
	statement.snapshotDigest[0] ^= 0xff
	assertPreparedStatementDrift(t, statement)
	statement.snapshotDigest = originalSnapshot

	originalIndex := statement.statementIndex
	statement.statementIndex++
	assertPreparedStatementDrift(t, statement)
	statement.statementIndex = originalIndex

	originalMaxAttempts := statement.maxAttempts
	statement.maxAttempts++
	assertPreparedStatementDrift(t, statement)
	statement.maxAttempts = originalMaxAttempts

	originalLockTimeout := statement.policy.LockTimeoutMS
	statement.policy.LockTimeoutMS++
	assertPreparedStatementDrift(t, statement)
	statement.policy.LockTimeoutMS = originalLockTimeout

	originalSQL := statement.plan.sqlBytes[0]
	statement.plan.sqlBytes[0] ^= 0xff
	assertPreparedStatementDrift(t, statement)
	statement.plan.sqlBytes[0] = originalSQL

	originalAuthorityDatabase := statement.authorityBefore.Metadata.Snapshot.DatabaseName
	statement.authorityBefore.Metadata.Snapshot.DatabaseName = "drifted_authority_database"
	assertPreparedStatementDrift(t, statement)
	statement.authorityBefore.Metadata.Snapshot.DatabaseName = originalAuthorityDatabase

	originalCatalogScope := statement.catalogBefore.Metadata.Scope.ScopeKind
	statement.catalogBefore.Metadata.Scope.ScopeKind = "drifted_catalog_scope"
	assertPreparedStatementDrift(t, statement)
	statement.catalogBefore.Metadata.Scope.ScopeKind = originalCatalogScope

	originalEvidenceState := fixture.evidence.snapshot.state
	fixture.evidence.snapshot.state = RecoveryDivergent
	assertPreparedStatementDrift(t, statement)
	fixture.evidence.snapshot.state = originalEvidenceState

	fixture.database.transaction.status = '?'
	assertPreparedStatementDrift(t, statement)
	fixture.database.transaction.status = 'T'

	if !validRunnerPreparedCurrentStatement(statement) {
		t.Fatal("restored statement authority did not recover its immutable binding")
	}
	originalKey := statement.key
	originalTransaction := statement.transaction
	rogueTransaction := newRunnerPreflightTransaction(fixture.database)
	rogueTransaction.active = true
	rogueTransaction.status = 'T'
	statement.key++
	statement.transaction = rogueTransaction
	err = closeRunnerPreparedCurrentStatement(statement, nil)
	if !IsCode(err, CodeTransactionBoundary) || originalTransaction != fixture.database.transaction || fixture.database.transaction.rollbackCalls != 1 || fixture.database.transaction.status != 'I' || rogueTransaction.rollbackCalls != 0 || fixture.database.unlockCalls != 1 || !reflect.DeepEqual(fixture.database.unlockKeys, []int64{originalKey}) || fixture.database.closeCalls != 1 || liveRunnerPreparedCurrentStatements() != 0 {
		t.Fatalf("drifted statement close did not use registry ownership: err=%v transaction=%+v database=%+v live=%d", err, fixture.database.transaction, fixture.database, liveRunnerPreparedCurrentStatements())
	}
	if err := closeRunnerEvidenceOwnership(fixture.evidence, fixture.candidate); err != nil {
		t.Fatal(err)
	}
}

func TestRunnerStatementPreflightFaultsAlwaysRollbackAndClose(t *testing.T) {
	for _, test := range []struct {
		name      string
		configure func(*runnerPreparedCurrentSessionFixture, *runnerPreflightProjectorFactory)
		wantCode  ErrorCode
		wantOp    string
	}{
		{"projection-profile", func(f *runnerPreparedCurrentSessionFixture, _ *runnerPreflightProjectorFactory) {
			f.database.transaction.profileEnterErr = errors.New("secret-statement-profile")
		}, CodeTransactionBoundary, "runner-statement-profile"},
		{"snapshot-read", func(f *runnerPreparedCurrentSessionFixture, _ *runnerPreflightProjectorFactory) {
			f.database.transaction.metadataReadErr = errors.New("secret-statement-snapshot")
		}, CodeProjectionCatalogQueryFailed, "runner-statement-snapshot"},
		{"snapshot-status-change", func(f *runnerPreparedCurrentSessionFixture, _ *runnerPreflightProjectorFactory) {
			f.database.transaction.afterMetadataScan = func() { f.database.transaction.status = '?' }
		}, CodeProjectionSnapshotInvalid, "runner-statement-snapshot"},
		{"snapshot-identity", func(f *runnerPreparedCurrentSessionFixture, _ *runnerPreflightProjectorFactory) {
			f.database.transaction.metadata[1] = "other_statement_database"
		}, CodeProjectionMetadataMismatch, "runner-statement-snapshot"},
		{"projector", func(_ *runnerPreparedCurrentSessionFixture, factory *runnerPreflightProjectorFactory) {
			factory.factoryErr[AuthorityPhaseMigrationTransaction] = errors.New("secret-statement-projector")
		}, CodeTransactionBoundary, "runner-statement-projector"},
		{"authority", func(_ *runnerPreparedCurrentSessionFixture, factory *runnerPreflightProjectorFactory) {
			factory.projectionErr[AuthorityPhaseMigrationTransaction] = fail(CodeAuthorityDrift, "fake", "secret", errors.New("secret-statement-authority"))
		}, CodeAuthorityDrift, "runner-statement-authority"},
		{"authority-result", func(_ *runnerPreparedCurrentSessionFixture, factory *runnerPreflightProjectorFactory) {
			factory.mutateResult[AuthorityPhaseMigrationTransaction] = func(result *ProjectionResult[AuthorityProjection]) {
				result.Digest = testDigest("wrong-statement-authority")
			}
		}, CodeAuthorityDrift, "runner-statement-authority"},
		{"precondition", func(_ *runnerPreparedCurrentSessionFixture, factory *runnerPreflightProjectorFactory) {
			factory.preconditionErr = fail(CodeCatalogDrift, "fake", "secret", errors.New("secret-statement-precondition"))
		}, CodeCatalogDrift, "runner-statement-precondition"},
		{"precondition-result", func(_ *runnerPreparedCurrentSessionFixture, factory *runnerPreflightProjectorFactory) {
			factory.mutatePrecondition = func(result *ProjectionResult[CatalogStateProjection]) {
				result.Digest = testDigest("wrong-statement-precondition")
			}
		}, CodeCatalogDrift, "runner-statement-precondition"},
		{"execution-profile", func(f *runnerPreparedCurrentSessionFixture, _ *runnerPreflightProjectorFactory) {
			f.database.transaction.profileRestoreErr = errors.New("secret-statement-execution-profile")
		}, CodeTransactionBoundary, "runner-statement-execution-profile"},
		{"boundary", func(f *runnerPreparedCurrentSessionFixture, _ *runnerPreflightProjectorFactory) {
			f.database.transaction.boundaryErr = errors.New("secret-statement-boundary")
		}, CodeTransactionBoundary, "runner-statement-boundary"},
		{"boundary-role", func(f *runnerPreparedCurrentSessionFixture, _ *runnerPreflightProjectorFactory) {
			f.database.transaction.boundaryMutate = func(boundary *BoundaryState) { boundary.CurrentUser = "other_role" }
		}, CodeTransactionBoundary, "runner-statement-boundary"},
		{"evidence-drift", func(f *runnerPreparedCurrentSessionFixture, _ *runnerPreflightProjectorFactory) {
			f.database.transaction.boundaryMutate = func(*BoundaryState) { f.evidence.snapshot.state = RecoveryDivergent }
		}, CodeEvidenceJournalFailed, "runner-statement-evidence"},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture, transaction, factory := newRunnerPreparedCurrentTransactionFixture(t)
			test.configure(&fixture, factory)
			statement, err := (&Runner{projectionFactory: factory}).prepareCurrentStatement(context.Background(), transaction, fixture.bundle, fixture.plans)
			var migrationErr *Error
			if statement != nil || !errors.As(err, &migrationErr) || migrationErr.Code != test.wantCode || migrationErr.Op != test.wantOp || migrationErr.Err != nil || containsErrorText(err, "secret-") {
				t.Fatalf("statement fault mapping: statement=%+v err=%#v", statement, migrationErr)
			}
			if fixture.database.beginCalls != 1 || fixture.database.transaction.rollbackCalls != 1 || fixture.database.transaction.executeCalls != 0 || fixture.database.transaction.execCalls != 0 || fixture.database.transaction.commitCalls != 0 || fixture.database.unlockCalls != 1 || fixture.database.closeCalls != 1 || liveRunnerPreparedCurrentSessions() != 0 || liveRunnerPreparedCurrentTransactions() != 0 || liveRunnerPreparedCurrentStatements() != 0 {
				t.Fatalf("statement fault escaped cleanup boundary: database=%+v transaction=%+v live=%d/%d/%d", fixture.database, fixture.database.transaction, liveRunnerPreparedCurrentSessions(), liveRunnerPreparedCurrentTransactions(), liveRunnerPreparedCurrentStatements())
			}
			if cleanupErr := closeRunnerEvidenceOwnership(fixture.evidence, fixture.candidate); cleanupErr != nil {
				t.Fatal(cleanupErr)
			}
		})
	}
}

func TestRunnerStatementClaimRejectsChangedPlanBeforeSecondProjection(t *testing.T) {
	fixture, transaction, factory := newRunnerPreparedCurrentTransactionFixture(t)
	changed := append([]StatementPlan(nil), fixture.plans...)
	changed[0].sqlBytes = append([]byte(nil), changed[0].sqlBytes...)
	changed[0].sqlBytes[0] ^= 0xff
	statement, err := (&Runner{projectionFactory: factory}).prepareCurrentStatement(context.Background(), transaction, fixture.bundle, changed)
	var migrationErr *Error
	if statement != nil || !errors.As(err, &migrationErr) || migrationErr.Code != CodeUntrusted || migrationErr.Op != "runner-statement-claim" || migrationErr.Err != nil {
		t.Fatalf("changed plan statement claim: statement=%+v err=%#v", statement, migrationErr)
	}
	if fixture.database.transaction.profileEnterCalls != 1 || fixture.database.transaction.metadataReadCalls != 1 || fixture.database.transaction.boundaryCalls != 1 || fixture.database.transaction.rollbackCalls != 1 || fixture.database.transaction.executeCalls != 0 || fixture.database.transaction.commitCalls != 0 || fixture.database.closeCalls != 1 || liveRunnerPreparedCurrentTransactions() != 0 || liveRunnerPreparedCurrentStatements() != 0 {
		t.Fatalf("changed plan crossed second projection or leaked ownership: database=%+v transaction=%+v", fixture.database, fixture.database.transaction)
	}
	if cleanupErr := closeRunnerEvidenceOwnership(fixture.evidence, fixture.candidate); cleanupErr != nil {
		t.Fatal(cleanupErr)
	}
}

func TestRunnerPreparedStatementHasOnlyReviewedProductionConsumers(t *testing.T) {
	paths, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	symbols := map[string]bool{
		"runnerPreparedCurrentStatement": true, "runnerPreparedCurrentStatementBinding": true,
		"runnerPreparedCurrentStatementRegistryRecord": true, "runnerPreparedCurrentStatementRegistry": true,
		"runnerPreparedCurrentStatementSeed": true, "consumeRunnerPreparedCurrentTransaction": true,
		"bindRunnerPreparedCurrentStatement": true,
		"prepareCurrentStatement":            true, "validRunnerPreparedCurrentStatement": true,
		"closeRunnerPreparedCurrentStatement": true,
	}
	allowed := map[string]map[string]bool{
		"runner_statement_preflight.go": nil,
		"runner.go":                     {"prepareCurrentStatement": true, "closeRunnerPreparedCurrentStatement": true},
		"runner_statement_intent.go": {
			"runnerPreparedCurrentStatement": true, "runnerPreparedCurrentStatementRegistryRecord": true,
			"runnerPreparedCurrentStatementRegistry": true, "validRunnerPreparedCurrentStatement": true,
			"closeRunnerPreparedCurrentStatement": true,
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
			if !ok || !symbols[identifier.Name] || name == "runner_statement_preflight.go" || allowed[name][identifier.Name] {
				return true
			}
			t.Fatalf("prepared statement authority %s acquired unreviewed production consumer %s", identifier.Name, name)
			return false
		})
	}
}

func TestRunnerStatementPreflightHasNoEvidenceExecutionOrCommitCallEdge(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "runner_statement_preflight.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	forbidden := map[string]bool{
		"BeginMigration": true, "ExecuteStatement": true, "AppendDurable": true,
		"ReserveAndActivateSuccessor": true, "Insert": true, "Commit": true,
		"Exec": true, "Query": true, "QueryRow": true, "ProjectCatalog": true,
		"ProjectTransitionState": true, "StateMigrate": true,
	}
	ast.Inspect(file, func(node ast.Node) bool {
		selector, ok := node.(*ast.SelectorExpr)
		if ok && forbidden[selector.Sel.Name] {
			t.Fatalf("statement preflight acquired forbidden %s call edge", selector.Sel.Name)
		}
		return true
	})
}

func newRunnerPreparedCurrentTransactionFixture(t *testing.T) (runnerPreparedCurrentSessionFixture, *runnerPreparedCurrentTransaction, *runnerPreflightProjectorFactory) {
	t.Helper()
	fixture := newRunnerPreparedCurrentSessionFixture(t)
	return newRunnerPreparedCurrentTransactionFixtureFromSession(t, fixture)
}

func newRunnerPreparedCurrentTransactionFixtureFromRuntime(t *testing.T, raw []byte, decision VerifiedTrustDecision) (runnerPreparedCurrentSessionFixture, *runnerPreparedCurrentTransaction, *runnerPreflightProjectorFactory) {
	t.Helper()
	return newRunnerPreparedCurrentTransactionFixtureFromSession(t, newRunnerPreparedCurrentSessionFixtureFromRuntime(t, raw, decision))
}

func newRunnerPreparedCurrentTransactionFixtureFromSession(t *testing.T, fixture runnerPreparedCurrentSessionFixture) (runnerPreparedCurrentSessionFixture, *runnerPreparedCurrentTransaction, *runnerPreflightProjectorFactory) {
	t.Helper()
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
	if err != nil || !validRunnerPreparedCurrentTransaction(transaction) {
		t.Fatalf("transaction fixture: transaction=%+v err=%v", transaction, err)
	}
	return fixture, transaction, factory
}

func assertPreparedStatementDrift(t *testing.T, statement *runnerPreparedCurrentStatement) {
	t.Helper()
	if validRunnerPreparedCurrentStatement(statement) {
		t.Fatal("mutated statement authority remained valid")
	}
}

func liveRunnerPreparedCurrentStatements() int {
	count := 0
	runnerPreparedCurrentStatementRegistry.Range(func(_, _ any) bool {
		count++
		return true
	})
	return count
}
