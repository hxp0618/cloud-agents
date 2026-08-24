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
	"strconv"
	"strings"
	"testing"
)

func TestRunnerProjectedCurrentStatementAfterConsumesExecutionAndSealsExactState(t *testing.T) {
	fixture, executed, runner, factory := newRunnerStatementAfterFixture(t)
	transaction := fixture.database.transaction
	beforeProfileEnter := transaction.profileEnterCalls
	beforeProfileRestore := transaction.profileRestoreCalls
	beforeMetadata := transaction.metadataReadCalls
	beforeBoundary := transaction.boundaryCalls
	projected, err := runner.projectCurrentStatementAfter(context.Background(), executed)
	if err != nil || !validRunnerProjectedCurrentStatementAfter(projected) {
		t.Fatalf("project statement after: projected=%+v err=%v", projected, err)
	}
	if validRunnerExecutedCurrentStatement(executed) || liveRunnerExecutedCurrentStatements() != 0 || liveRunnerProjectedCurrentStatementAfters() != 1 {
		t.Fatalf("executed authority was not atomically consumed: executed=%t live=%d/%d", validRunnerExecutedCurrentStatement(executed), liveRunnerExecutedCurrentStatements(), liveRunnerProjectedCurrentStatementAfters())
	}
	if transaction.executeCalls != 1 || transaction.profileEnterCalls != beforeProfileEnter+1 || transaction.profileRestoreCalls != beforeProfileRestore+1 || transaction.metadataReadCalls != beforeMetadata+1 || transaction.boundaryCalls != beforeBoundary+1 || transaction.profile != "execution" || transaction.status != 'T' || transaction.rollbackCalls != 0 || transaction.commitCalls != 0 || transaction.execCalls != 0 {
		t.Fatalf("statement-after transaction lifecycle mismatch: %+v", transaction)
	}
	if len(factory.transitionPhases) != 1 || factory.transitionPhases[0] != AuthorityPhaseMigrationTransaction || len(factory.transitionScopes) != 1 || !equalProjectionScopes(factory.transitionScopes[0], executedPlanAfterScope(projected.plan)) {
		t.Fatalf("transition projector did not consume exact scope: phases=%v scopes=%+v", factory.transitionPhases, factory.transitionScopes)
	}
	if projected.catalogAfter.Digest != projected.plan.ExpectedTransition.CatalogAfter.Digest || projected.authorityAfter.Digest != projected.intent.AuthorityBeforeDigest || projected.state.CatalogAfterDigest != projected.catalogAfter.Digest || projected.state.AuthorityAfterDigest != projected.authorityAfter.Digest || projected.state.IntermediateStateDigest.Validate() != nil || projected.state.Validate() != nil {
		t.Fatalf("statement-after state differs from exact projection: %+v", projected)
	}
	control := projected.state.ControlPlaneStates
	if control.TxStatus != "T" || control.CurrentUser != MigrationOwnerRole || control.MigrationRole != MigrationOwnerRole || !control.AdvisoryLock.Held || control.AdvisoryLock.KeyInt64Decimal != runnerStatementAfterKey(fixture.key) || control.VerifiedAuthorityDecisionDigest != projected.generation.runnerProjectionDecisionDigest || control.ExpectedTransitionDigest != projected.plan.ExpectedTransitionDigest {
		t.Fatalf("statement-after control-plane state mismatch: %+v", control)
	}
	if !projected.finalStatement || fixture.evidence.journal.appendCalls != 1 || fixture.evidence.snapshot.state != RecoveryDanglingStatementIntent {
		t.Fatalf("final statement crossed pre-ledger/evidence boundary: final=%t journal=%+v snapshot=%+v", projected.finalStatement, fixture.evidence.journal, fixture.evidence.snapshot)
	}
	if replay, replayErr := runner.projectCurrentStatementAfter(context.Background(), executed); replay != nil || !IsCode(replayErr, CodeTransactionBoundary) || transaction.profileEnterCalls != beforeProfileEnter+1 || !validRunnerProjectedCurrentStatementAfter(projected) {
		t.Fatalf("consumed execution replayed projection or damaged successor: replay=%+v err=%v", replay, replayErr)
	}
	if err := closeRunnerProjectedCurrentStatementAfter(projected, nil); err != nil || transaction.rollbackCalls != 1 || transaction.status != 'I' || fixture.database.unlockCalls != 1 || fixture.database.closeCalls != 1 || liveRunnerProjectedCurrentStatementAfters() != 0 {
		t.Fatalf("statement-after close did not release database ownership: err=%v transaction=%+v database=%+v", err, transaction, fixture.database)
	}
	if err := closeRunnerEvidenceOwnership(fixture.evidence, fixture.candidate); err != nil {
		t.Fatal(err)
	}
}

func TestRunnerProjectedCurrentStatementAfterRejectsLiteralCopyAndFieldDrift(t *testing.T) {
	fixture, executed, runner, _ := newRunnerStatementAfterFixture(t)
	projected, err := runner.projectCurrentStatementAfter(context.Background(), executed)
	if err != nil || !validRunnerProjectedCurrentStatementAfter(projected) {
		t.Fatalf("project statement after: projected=%+v err=%v", projected, err)
	}
	valueType := reflect.TypeOf(*projected)
	for index := 0; index < valueType.NumField(); index++ {
		if valueType.Field(index).PkgPath == "" {
			t.Fatalf("statement-after field %s became public", valueType.Field(index).Name)
		}
	}
	copyValue := *projected
	if err := closeRunnerProjectedCurrentStatementAfter(&copyValue, nil); !IsCode(err, CodeTransactionBoundary) || fixture.database.transaction.rollbackCalls != 0 || !validRunnerProjectedCurrentStatementAfter(projected) {
		t.Fatalf("copy changed original authority: err=%v transaction=%+v", err, fixture.database.transaction)
	}
	if err := closeRunnerProjectedCurrentStatementAfter(&runnerProjectedCurrentStatementAfter{}, nil); !IsCode(err, CodeTransactionBoundary) || fixture.database.transaction.rollbackCalls != 0 {
		t.Fatalf("literal escaped: err=%v", err)
	}

	originalState := projected.state.IntermediateStateDigest
	projected.state.IntermediateStateDigest = testDigest("other-intermediate-state")
	assertProjectedStatementAfterDrift(t, projected)
	projected.state.IntermediateStateDigest = originalState

	originalOwner := projected.catalogAfterProjection.Present.Body.Schema.Owner
	projected.catalogAfterProjection.Present.Body.Schema.Owner = "other_owner"
	assertProjectedStatementAfterDrift(t, projected)
	projected.catalogAfterProjection.Present.Body.Schema.Owner = originalOwner

	originalAuthority := projected.authorityAfter.Digest
	projected.authorityAfter.Digest = testDigest("other-after-authority")
	assertProjectedStatementAfterDrift(t, projected)
	projected.authorityAfter.Digest = originalAuthority

	originalPolicy := projected.policy.LockTimeoutMS
	projected.policy.LockTimeoutMS++
	assertProjectedStatementAfterDrift(t, projected)
	projected.policy.LockTimeoutMS = originalPolicy

	originalPlanDigest := projected.dispatch.planDigest
	projected.dispatch.planDigest = sha256.Sum256([]byte("other-statement-after-dispatch"))
	assertProjectedStatementAfterDrift(t, projected)
	projected.dispatch.planDigest = originalPlanDigest

	originalDatabase := projected.database.databaseName
	projected.database.databaseName = "other_database"
	assertProjectedStatementAfterDrift(t, projected)
	projected.database.databaseName = originalDatabase

	originalEvidenceState := fixture.evidence.snapshot.state
	fixture.evidence.snapshot.state = RecoveryDivergent
	assertProjectedStatementAfterDrift(t, projected)
	fixture.evidence.snapshot.state = originalEvidenceState

	fixture.database.transaction.status = 'E'
	assertProjectedStatementAfterDrift(t, projected)
	fixture.database.transaction.status = 'T'
	if !validRunnerProjectedCurrentStatementAfter(projected) {
		t.Fatal("restored statement-after authority did not recover its immutable binding")
	}

	originalKey := projected.key
	originalTransaction := projected.transaction
	rogueTransaction := newRunnerPreflightTransaction(fixture.database)
	rogueTransaction.active = true
	rogueTransaction.status = 'T'
	projected.key++
	projected.transaction = rogueTransaction
	err = closeRunnerProjectedCurrentStatementAfter(projected, nil)
	if !IsCode(err, CodeTransactionBoundary) || originalTransaction != fixture.database.transaction || fixture.database.transaction.rollbackCalls != 1 || rogueTransaction.rollbackCalls != 0 || !reflect.DeepEqual(fixture.database.unlockKeys, []int64{originalKey}) || fixture.database.closeCalls != 1 || liveRunnerProjectedCurrentStatementAfters() != 0 {
		t.Fatalf("drifted close did not use registry ownership: err=%v transaction=%+v database=%+v", err, fixture.database.transaction, fixture.database)
	}
	if err := closeRunnerEvidenceOwnership(fixture.evidence, fixture.candidate); err != nil {
		t.Fatal(err)
	}
}

func TestRunnerStatementAfterRejectsUnavailableContextBeforeProjection(t *testing.T) {
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
			fixture, executed, runner, _ := newRunnerStatementAfterFixture(t)
			beforeProfile := fixture.database.transaction.profileEnterCalls
			var active *Runner = runner
			if test.nilRunner {
				active = nil
			}
			projected, err := active.projectCurrentStatementAfter(test.context(), executed)
			assertRunnerStatementAfterError(t, err, test.wantCode, "runner-statement-after")
			if projected != nil || fixture.database.transaction.profileEnterCalls != beforeProfile || fixture.database.transaction.rollbackCalls != 1 || fixture.database.unlockCalls != 1 || fixture.database.closeCalls != 1 || fixture.evidence.journal.appendCalls != 1 || liveRunnerExecutedCurrentStatements() != 0 || liveRunnerProjectedCurrentStatementAfters() != 0 {
				t.Fatalf("unavailable context crossed projection boundary: projected=%+v err=%v transaction=%+v", projected, err, fixture.database.transaction)
			}
			if cleanupErr := closeRunnerEvidenceOwnership(fixture.evidence, fixture.candidate); cleanupErr != nil {
				t.Fatal(cleanupErr)
			}
		})
	}
}

func TestRunnerStatementAfterProjectionFaultsRollbackWithoutAppendingIntermediate(t *testing.T) {
	tests := []struct {
		name       string
		configure  func(*runnerPreparedCurrentSessionFixture, *runnerPreflightProjectorFactory)
		wantCode   ErrorCode
		wantOp     string
		wantCursor bool
	}{
		{"profile", func(f *runnerPreparedCurrentSessionFixture, _ *runnerPreflightProjectorFactory) {
			f.database.transaction.profileEnterErr = errors.New("secret-profile")
		}, CodeTransactionBoundary, "runner-statement-after-profile", true},
		{"snapshot", func(f *runnerPreparedCurrentSessionFixture, _ *runnerPreflightProjectorFactory) {
			f.database.transaction.metadataReadErr = errors.New("secret-snapshot")
		}, CodeProjectionCatalogQueryFailed, "runner-statement-after-snapshot", true},
		{"snapshot-identity", func(f *runnerPreparedCurrentSessionFixture, _ *runnerPreflightProjectorFactory) {
			f.database.transaction.metadata[1] = "other_database"
		}, CodeProjectionMetadataMismatch, "runner-statement-after-snapshot", true},
		{"projector", func(_ *runnerPreparedCurrentSessionFixture, factory *runnerPreflightProjectorFactory) {
			factory.factoryErr[AuthorityPhaseMigrationTransaction] = errors.New("secret-projector")
		}, CodeTransactionBoundary, "runner-statement-after-projector", true},
		{"authority", func(_ *runnerPreparedCurrentSessionFixture, factory *runnerPreflightProjectorFactory) {
			factory.projectionErr[AuthorityPhaseMigrationTransaction] = fail(CodeAuthorityDrift, "fake", "secret", errors.New("secret-authority"))
		}, CodeAuthorityDrift, "runner-statement-after-authority", true},
		{"authority-result", func(_ *runnerPreparedCurrentSessionFixture, factory *runnerPreflightProjectorFactory) {
			factory.mutateResult[AuthorityPhaseMigrationTransaction] = func(result *ProjectionResult[AuthorityProjection]) {
				result.Digest = testDigest("wrong-after-authority")
			}
		}, CodeAuthorityDrift, "runner-statement-after-authority", true},
		{"catalog", func(_ *runnerPreparedCurrentSessionFixture, factory *runnerPreflightProjectorFactory) {
			factory.transitionErr = fail(CodeCatalogDrift, "fake", "secret", errors.New("secret-catalog"))
		}, CodeCatalogDrift, "runner-statement-after-catalog", true},
		{"catalog-digest", func(_ *runnerPreparedCurrentSessionFixture, factory *runnerPreflightProjectorFactory) {
			factory.mutateTransition = func(result *ProjectionResult[CatalogStateProjection]) {
				result.Digest = testDigest("wrong-after-catalog")
			}
		}, CodeCatalogDrift, "runner-statement-after-catalog", true},
		{"catalog-scope", func(_ *runnerPreparedCurrentSessionFixture, factory *runnerPreflightProjectorFactory) {
			factory.mutateTransition = func(result *ProjectionResult[CatalogStateProjection]) {
				result.Metadata.Scope.ScopeKind = "predecessor"
			}
		}, CodeProjectionMetadataMismatch, "runner-statement-after-catalog", true},
		{"catalog-subject", func(_ *runnerPreparedCurrentSessionFixture, factory *runnerPreflightProjectorFactory) {
			factory.mutateTransition = func(result *ProjectionResult[CatalogStateProjection]) {
				result.Metadata.VerifiedSubjectDigest = testDigest("wrong-after-subject")
			}
		}, CodeProjectionMetadataMismatch, "runner-statement-after-catalog", true},
		{"execution-profile", func(f *runnerPreparedCurrentSessionFixture, _ *runnerPreflightProjectorFactory) {
			f.database.transaction.profileRestoreErr = errors.New("secret-restore")
		}, CodeTransactionBoundary, "runner-statement-after-execution-profile", true},
		{"boundary", func(f *runnerPreparedCurrentSessionFixture, _ *runnerPreflightProjectorFactory) {
			f.database.transaction.boundaryErr = errors.New("secret-boundary")
		}, CodeTransactionBoundary, "runner-statement-after-boundary", true},
		{"boundary-role", func(f *runnerPreparedCurrentSessionFixture, _ *runnerPreflightProjectorFactory) {
			f.database.transaction.boundaryMutate = func(boundary *BoundaryState) { boundary.CurrentUser = "other_role" }
		}, CodeTransactionBoundary, "runner-statement-after-boundary", true},
		{"evidence-drift", func(f *runnerPreparedCurrentSessionFixture, _ *runnerPreflightProjectorFactory) {
			f.database.transaction.boundaryMutate = func(*BoundaryState) { f.evidence.snapshot.state = RecoveryDivergent }
		}, CodeEvidenceJournalFailed, "runner-statement-after-evidence", false},
		{"status-drift", func(f *runnerPreparedCurrentSessionFixture, _ *runnerPreflightProjectorFactory) {
			f.database.transaction.boundaryMutate = func(*BoundaryState) { f.database.transaction.status = 'E' }
		}, CodeTransactionBoundary, "runner-statement-after-boundary", true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture, executed, runner, factory := newRunnerStatementAfterFixture(t)
			test.configure(&fixture, factory)
			projected, err := runner.projectCurrentStatementAfter(context.Background(), executed)
			assertRunnerStatementAfterError(t, err, test.wantCode, test.wantOp)
			if projected != nil || containsErrorText(err, "secret-") || fixture.database.transaction.executeCalls != 1 || fixture.database.transaction.rollbackCalls != 1 || fixture.database.transaction.commitCalls != 0 || fixture.database.transaction.execCalls != 0 || fixture.database.unlockCalls != 1 || fixture.database.closeCalls != 1 || fixture.evidence.journal.appendCalls != 1 || liveRunnerExecutedCurrentStatements() != 0 || liveRunnerProjectedCurrentStatementAfters() != 0 {
				t.Fatalf("statement-after fault escaped boundary: projected=%+v err=%v transaction=%+v database=%+v", projected, err, fixture.database.transaction, fixture.database)
			}
			if fixture.evidence.journal.cursor.Valid() != test.wantCursor {
				t.Fatalf("statement-after fault cursor validity=%t want=%t", fixture.evidence.journal.cursor.Valid(), test.wantCursor)
			}
			if cleanupErr := closeRunnerEvidenceOwnership(fixture.evidence, fixture.candidate); cleanupErr != nil {
				t.Fatal(cleanupErr)
			}
		})
	}
}

func TestRunnerStatementAfterCleanupFailureDominatesAndAttemptsEveryRelease(t *testing.T) {
	fixture, executed, runner, factory := newRunnerStatementAfterFixture(t)
	factory.transitionErr = errors.New("secret-catalog")
	fixture.database.transaction.rollbackErr = errors.New("secret-rollback")
	fixture.database.transaction.rollbackLeavesOpen = true
	fixture.database.unlockErr = errors.New("secret-unlock")
	fixture.database.closeErr = errors.New("secret-close")
	projected, err := runner.projectCurrentStatementAfter(context.Background(), executed)
	assertRunnerStatementAfterError(t, err, CodeTransactionBoundary, "runner-database-close")
	if projected != nil || containsErrorText(err, "secret-") || fixture.database.transaction.rollbackCalls != 1 || fixture.database.unlockCalls != 1 || fixture.database.closeCalls != 1 || liveRunnerProjectedCurrentStatementAfters() != 0 {
		t.Fatalf("statement-after cleanup precedence/attempts: projected=%+v err=%v transaction=%+v database=%+v", projected, err, fixture.database.transaction, fixture.database)
	}
	if cleanupErr := closeRunnerEvidenceOwnership(fixture.evidence, fixture.candidate); cleanupErr != nil {
		t.Fatal(cleanupErr)
	}
}

func TestRunnerStatementAfterHasOnlyReviewedProjectionAndBoundaryCallEdges(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "runner_statement_after.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	calls := map[string]int{}
	allowed := map[string]bool{"ProjectAuthority": true, "ProjectTransitionState": true, "Boundary": true}
	forbidden := map[string]bool{
		"BeginMigration": true, "ExecuteStatement": true, "AppendDurable": true,
		"ReserveAndActivateSuccessor": true, "Insert": true, "Commit": true,
		"Exec": true, "Query": true, "QueryRow": true, "ProjectCatalog": true, "ProjectPrecondition": true,
	}
	ast.Inspect(file, func(node ast.Node) bool {
		selector, ok := node.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if allowed[selector.Sel.Name] {
			calls[selector.Sel.Name]++
		}
		if forbidden[selector.Sel.Name] {
			t.Fatalf("statement-after acquired forbidden %s call edge", selector.Sel.Name)
		}
		return true
	})
	for name, want := range map[string]int{"ProjectAuthority": 1, "ProjectTransitionState": 1, "Boundary": 1} {
		if calls[name] != want {
			t.Fatalf("statement-after %s call edges=%d want=%d", name, calls[name], want)
		}
	}
}

func TestRunnerProjectedStatementAfterHasNoProductionConsumer(t *testing.T) {
	paths, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	symbols := map[string]bool{
		"runnerProjectedCurrentStatementAfter": true, "runnerProjectedCurrentStatementAfterBinding": true,
		"runnerProjectedCurrentStatementAfterRegistryRecord": true, "runnerProjectedCurrentStatementAfterRegistry": true,
		"runnerProjectedCurrentStatementAfterSeed": true, "projectCurrentStatementAfter": true,
		"consumeRunnerExecutedCurrentStatement": true, "bindRunnerProjectedCurrentStatementAfter": true,
		"validRunnerProjectedCurrentStatementAfter": true, "closeRunnerProjectedCurrentStatementAfter": true,
	}
	consumer := map[string]bool{"projectCurrentStatementAfter": true}
	successKernelAllowed := map[string]bool{"runnerProjectedCurrentStatementAfterSeed": true}
	for _, path := range paths {
		name := filepath.Base(path)
		if strings.HasSuffix(name, "_test.go") || name == "runner_statement_after.go" || name == "runner_preledger.go" {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			identifier, ok := node.(*ast.Ident)
			if ok && symbols[identifier.Name] && !(name == "runner_current_execution.go" && consumer[identifier.Name]) &&
				!(name == "runner_ledger_entry_success_kernel.go" && successKernelAllowed[identifier.Name]) {
				t.Fatalf("statement-after authority %s acquired unreviewed production consumer %s", identifier.Name, name)
			}
			return true
		})
	}
}

func newRunnerStatementAfterFixture(t *testing.T) (runnerPreparedCurrentSessionFixture, *runnerExecutedCurrentStatement, *Runner, *runnerPreflightProjectorFactory) {
	t.Helper()
	fixture, durable, runner := newRunnerDurableStatementExecutionFixture(t)
	fixture.database.transaction.executeAllowed = true
	executed, err := runner.executeCurrentStatement(context.Background(), durable)
	if err != nil || !validRunnerExecutedCurrentStatement(executed) {
		t.Fatalf("statement-after fixture execution: executed=%+v err=%v", executed, err)
	}
	factory, ok := runner.projectionFactory.(*runnerPreflightProjectorFactory)
	if !ok || factory == nil {
		t.Fatal("statement-after fixture has no runner projector factory")
	}
	state := expectedRunnerStatementAfterCatalogState(t, fixture.evidence, executed.plan)
	factory.transitionState = &state
	return fixture, executed, runner, factory
}

func expectedRunnerStatementAfterCatalogState(t *testing.T, evidence EvidenceSession, plan StatementPlan) CatalogStateProjection {
	t.Helper()
	current := evidence.CurrentCandidate()
	bindings, err := runnerCurrentProjectionBindings(evidence, current)
	if err != nil {
		t.Fatal(err)
	}
	catalog, ok := exactCatalogBindingForHead(bindings.executableCatalogs, plan.MigrationID)
	if !ok {
		t.Fatal("exact catalog binding is unavailable")
	}
	expected := catalog.verifiedCatalog.ExpectedProjection()
	state := CatalogStateProjection{Present: &SchemaPresentProjection{State: "schema_present", Scope: cloneProjectionValue(plan.ExpectedTransition.CatalogAfter.Scope), Body: cloneProjectionValue(expected.Body)}}
	digest, err := state.ComputeDigest()
	if err != nil || digest != plan.ExpectedTransition.CatalogAfter.Digest {
		t.Fatalf("statement-after fixture digest: got=%s want=%s err=%v", digest, plan.ExpectedTransition.CatalogAfter.Digest, err)
	}
	return state
}

func executedPlanAfterScope(plan StatementPlan) ProjectionScope {
	return plan.ExpectedTransition.CatalogAfter.Scope
}

func runnerStatementAfterKey(key int64) string {
	return strconv.FormatInt(key, 10)
}

func assertProjectedStatementAfterDrift(t *testing.T, projected *runnerProjectedCurrentStatementAfter) {
	t.Helper()
	if validRunnerProjectedCurrentStatementAfter(projected) {
		t.Fatal("mutated statement-after authority remained valid")
	}
}

func liveRunnerProjectedCurrentStatementAfters() int {
	count := 0
	runnerProjectedCurrentStatementAfterRegistry.Range(func(_, _ any) bool {
		count++
		return true
	})
	return count
}

func assertRunnerStatementAfterError(t *testing.T, err error, code ErrorCode, op string) {
	t.Helper()
	var migrationErr *Error
	if !errors.As(err, &migrationErr) || migrationErr.Code != code || migrationErr.Op != op || migrationErr.Err != nil {
		t.Fatalf("statement-after error: got=%#v want=%s/%s", migrationErr, code, op)
	}
}
