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

func TestRunnerProjectedCurrentPreledgerConsumesFinalStatementAfterAndSealsEquality(t *testing.T) {
	fixture, after, runner, factory := newRunnerPreledgerFixture(t)
	transaction := fixture.database.transaction
	beforeProfileEnter := transaction.profileEnterCalls
	beforeProfileRestore := transaction.profileRestoreCalls
	beforeMetadata := transaction.metadataReadCalls
	beforeBoundary := transaction.boundaryCalls
	beforeAuthority := len(factory.projectionPhases)
	wantCatalog := expectedRunnerPreledgerCatalog(t, fixture.evidence, after.plan)
	wantImmediateBody := cloneProjectionValue(after.catalogAfterProjection.Present.Body)
	wantImmediateState := cloneProjectionValue(after.state)

	preledger, err := runner.projectCurrentPreledger(context.Background(), after)
	if err != nil || !validRunnerProjectedCurrentPreledger(preledger) {
		t.Fatalf("project pre-ledger: preledger=%+v err=%v", preledger, err)
	}
	if validRunnerProjectedCurrentStatementAfter(after) || liveRunnerProjectedCurrentStatementAfters() != 0 || liveRunnerProjectedCurrentPreledgers() != 1 {
		t.Fatalf("statement-after authority was not atomically consumed: after=%t live=%d/%d", validRunnerProjectedCurrentStatementAfter(after), liveRunnerProjectedCurrentStatementAfters(), liveRunnerProjectedCurrentPreledgers())
	}
	if transaction.executeCalls != 1 || transaction.profileEnterCalls != beforeProfileEnter+1 || transaction.profileRestoreCalls != beforeProfileRestore+1 || transaction.metadataReadCalls != beforeMetadata+1 || transaction.boundaryCalls != beforeBoundary+1 || transaction.profile != "execution" || transaction.status != 'T' || transaction.rollbackCalls != 0 || transaction.commitCalls != 0 || transaction.execCalls != 0 {
		t.Fatalf("pre-ledger transaction lifecycle mismatch: %+v", transaction)
	}
	if len(factory.projectionPhases) != beforeAuthority+1 || factory.projectionPhases[len(factory.projectionPhases)-1] != AuthorityPhaseMigrationTransaction || len(factory.catalogPhases) != 1 || factory.catalogPhases[0] != AuthorityPhaseMigrationTransaction || len(factory.catalogScopes) != 1 || !equalProjectionScopes(factory.catalogScopes[0], wantCatalogScope(wantCatalog)) {
		t.Fatalf("pre-ledger projector did not consume exact final scope: authority=%v catalog=%v scopes=%+v", factory.projectionPhases, factory.catalogPhases, factory.catalogScopes)
	}
	if preledger.preledgerAuthority.Digest != preledger.authorityAfter.Digest || preledger.preledgerCatalog.Digest.Validate() != nil || !runnerCanonicalEqual(preledger.preledgerCatalogBody, wantCatalog) || !runnerCanonicalEqual(preledger.preledgerCatalogBody.Body, wantImmediateBody) || !runnerCanonicalEqual(preledger.state, wantImmediateState) || preledger.preledgerCatalogBody.SchemaHead != preledger.plan.MigrationID {
		t.Fatalf("pre-ledger equality differs from verified final state: %+v", preledger)
	}
	if !runnerCanonicalEqual(preledger.policy, fixture.bundle.Manifest.ExecutionPolicy) || preledger.policy.MaxAttempts != uint64(preledger.maxAttempts) {
		t.Fatalf("pre-ledger authority lost exact execution policy: %+v", preledger.policy)
	}
	if fixture.evidence.journal.appendCalls != 1 || fixture.evidence.snapshot.state != RecoveryDanglingStatementIntent || transaction.session.backend.ledgerInsertCalls != 0 || transaction.session.backend.commitCalls != 0 {
		t.Fatalf("pre-ledger crossed evidence or ledger boundary: journal=%+v backend=%+v", fixture.evidence.journal, transaction.session.backend)
	}
	if replay, replayErr := runner.projectCurrentPreledger(context.Background(), after); replay != nil || !IsCode(replayErr, CodeTransactionBoundary) || transaction.profileEnterCalls != beforeProfileEnter+1 || !validRunnerProjectedCurrentPreledger(preledger) {
		t.Fatalf("consumed statement-after replayed pre-ledger or damaged successor: replay=%+v err=%v", replay, replayErr)
	}
	if err := closeRunnerProjectedCurrentPreledger(preledger, nil); err != nil || transaction.rollbackCalls != 1 || transaction.status != 'I' || fixture.database.unlockCalls != 1 || fixture.database.closeCalls != 1 || liveRunnerProjectedCurrentPreledgers() != 0 {
		t.Fatalf("pre-ledger close did not release database ownership: err=%v transaction=%+v database=%+v", err, transaction, fixture.database)
	}
	if err := closeRunnerEvidenceOwnership(fixture.evidence, fixture.candidate); err != nil {
		t.Fatal(err)
	}
}

func TestRunnerProjectedCurrentPreledgerRejectsLiteralCopyAndFieldDrift(t *testing.T) {
	fixture, after, runner, _ := newRunnerPreledgerFixture(t)
	preledger, err := runner.projectCurrentPreledger(context.Background(), after)
	if err != nil || !validRunnerProjectedCurrentPreledger(preledger) {
		t.Fatalf("project pre-ledger: preledger=%+v err=%v", preledger, err)
	}
	valueType := reflect.TypeOf(*preledger)
	for index := 0; index < valueType.NumField(); index++ {
		if valueType.Field(index).PkgPath == "" {
			t.Fatalf("pre-ledger field %s became public", valueType.Field(index).Name)
		}
	}
	copyValue := *preledger
	if err := closeRunnerProjectedCurrentPreledger(&copyValue, nil); !IsCode(err, CodeTransactionBoundary) || fixture.database.transaction.rollbackCalls != 0 || !validRunnerProjectedCurrentPreledger(preledger) {
		t.Fatalf("copy changed original authority: err=%v transaction=%+v", err, fixture.database.transaction)
	}
	if err := closeRunnerProjectedCurrentPreledger(&runnerProjectedCurrentPreledger{}, nil); !IsCode(err, CodeTransactionBoundary) || fixture.database.transaction.rollbackCalls != 0 {
		t.Fatalf("literal escaped: err=%v", err)
	}

	originalState := preledger.state.IntermediateStateDigest
	preledger.state.IntermediateStateDigest = testDigest("other-preledger-state")
	assertProjectedPreledgerDrift(t, preledger)
	preledger.state.IntermediateStateDigest = originalState

	originalOwner := preledger.preledgerCatalogBody.Body.Schema.Owner
	preledger.preledgerCatalogBody.Body.Schema.Owner = "other_owner"
	assertProjectedPreledgerDrift(t, preledger)
	preledger.preledgerCatalogBody.Body.Schema.Owner = originalOwner

	originalCatalog := preledger.preledgerCatalog.Digest
	preledger.preledgerCatalog.Digest = testDigest("other-preledger-catalog")
	assertProjectedPreledgerDrift(t, preledger)
	preledger.preledgerCatalog.Digest = originalCatalog

	originalPolicy := preledger.policy.StatementTimeoutMS
	preledger.policy.StatementTimeoutMS++
	assertProjectedPreledgerDrift(t, preledger)
	preledger.policy.StatementTimeoutMS = originalPolicy

	originalPlanDigest := preledger.dispatch.planDigest
	preledger.dispatch.planDigest = sha256.Sum256([]byte("other-preledger-dispatch"))
	assertProjectedPreledgerDrift(t, preledger)
	preledger.dispatch.planDigest = originalPlanDigest

	originalDatabase := preledger.database.databaseName
	preledger.database.databaseName = "other_database"
	assertProjectedPreledgerDrift(t, preledger)
	preledger.database.databaseName = originalDatabase

	originalSnapshot := cloneProjectionValue(preledger.preledgerCatalog.Metadata.Snapshot)
	statementIndex := uint32(0)
	preledger.preledgerCatalog.Metadata.Snapshot.StatementIndex = &statementIndex
	assertProjectedPreledgerDrift(t, preledger)
	preledger.preledgerCatalog.Metadata.Snapshot = originalSnapshot

	originalEvidenceState := fixture.evidence.snapshot.state
	fixture.evidence.snapshot.state = RecoveryDivergent
	assertProjectedPreledgerDrift(t, preledger)
	fixture.evidence.snapshot.state = originalEvidenceState

	fixture.database.transaction.status = 'E'
	assertProjectedPreledgerDrift(t, preledger)
	fixture.database.transaction.status = 'T'
	if !validRunnerProjectedCurrentPreledger(preledger) {
		t.Fatal("restored pre-ledger authority did not recover its immutable binding")
	}

	originalKey := preledger.key
	originalTransaction := preledger.transaction
	rogueTransaction := newRunnerPreflightTransaction(fixture.database)
	rogueTransaction.active = true
	rogueTransaction.status = 'T'
	preledger.key++
	preledger.transaction = rogueTransaction
	err = closeRunnerProjectedCurrentPreledger(preledger, nil)
	if !IsCode(err, CodeTransactionBoundary) || originalTransaction != fixture.database.transaction || fixture.database.transaction.rollbackCalls != 1 || rogueTransaction.rollbackCalls != 0 || !reflect.DeepEqual(fixture.database.unlockKeys, []int64{originalKey}) || fixture.database.closeCalls != 1 || liveRunnerProjectedCurrentPreledgers() != 0 {
		t.Fatalf("drifted close did not use registry ownership: err=%v transaction=%+v database=%+v", err, fixture.database.transaction, fixture.database)
	}
	if err := closeRunnerEvidenceOwnership(fixture.evidence, fixture.candidate); err != nil {
		t.Fatal(err)
	}
}

func TestRunnerPreledgerRejectsUnavailableContextAndNonFinalAuthorityBeforeProjection(t *testing.T) {
	for _, test := range []struct {
		name      string
		context   func() context.Context
		nilRunner bool
		nonFinal  bool
		wantCode  ErrorCode
		wantOp    string
	}{
		{"nil-context", func() context.Context { return nil }, false, false, CodeTransactionBoundary, "runner-preledger"},
		{"canceled", func() context.Context { ctx, cancel := context.WithCancel(context.Background()); cancel(); return ctx }, false, false, CodeContextCanceled, "runner-preledger"},
		{"deadline", func() context.Context {
			ctx, cancel := context.WithDeadline(context.Background(), testExpiredTime())
			defer cancel()
			return ctx
		}, false, false, CodeDeadlineExceeded, "runner-preledger"},
		{"nil-runner", func() context.Context { return context.Background() }, true, false, CodeTransactionBoundary, "runner-preledger"},
		{"non-final-drift", func() context.Context { return context.Background() }, false, true, CodeTransactionBoundary, "runner-statement-after-close"},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture, after, runner, _ := newRunnerPreledgerFixture(t)
			beforeProfile := fixture.database.transaction.profileEnterCalls
			if test.nonFinal {
				after.finalStatement = false
			}
			active := runner
			if test.nilRunner {
				active = nil
			}
			preledger, err := active.projectCurrentPreledger(test.context(), after)
			assertRunnerPreledgerError(t, err, test.wantCode, test.wantOp)
			if preledger != nil || fixture.database.transaction.profileEnterCalls != beforeProfile || fixture.database.transaction.rollbackCalls != 1 || fixture.database.unlockCalls != 1 || fixture.database.closeCalls != 1 || fixture.evidence.journal.appendCalls != 1 || liveRunnerProjectedCurrentStatementAfters() != 0 || liveRunnerProjectedCurrentPreledgers() != 0 {
				t.Fatalf("unavailable pre-ledger input crossed projection boundary: preledger=%+v err=%v transaction=%+v", preledger, err, fixture.database.transaction)
			}
			if cleanupErr := closeRunnerEvidenceOwnership(fixture.evidence, fixture.candidate); cleanupErr != nil {
				t.Fatal(cleanupErr)
			}
		})
	}
}

func TestRunnerPreledgerProjectionFaultsRollbackWithoutAppendingEvidenceOrLedger(t *testing.T) {
	tests := []struct {
		name       string
		configure  func(*runnerPreparedCurrentSessionFixture, *runnerPreflightProjectorFactory)
		wantCode   ErrorCode
		wantOp     string
		wantCursor bool
	}{
		{"profile", func(f *runnerPreparedCurrentSessionFixture, _ *runnerPreflightProjectorFactory) {
			f.database.transaction.profileEnterErr = errors.New("secret-profile")
		}, CodeTransactionBoundary, "runner-preledger-profile", true},
		{"snapshot", func(f *runnerPreparedCurrentSessionFixture, _ *runnerPreflightProjectorFactory) {
			f.database.transaction.metadataReadErr = errors.New("secret-snapshot")
		}, CodeProjectionCatalogQueryFailed, "runner-preledger-snapshot", true},
		{"snapshot-identity", func(f *runnerPreparedCurrentSessionFixture, _ *runnerPreflightProjectorFactory) {
			f.database.transaction.metadata[1] = "other_database"
		}, CodeProjectionMetadataMismatch, "runner-preledger-snapshot", true},
		{"projector", func(_ *runnerPreparedCurrentSessionFixture, factory *runnerPreflightProjectorFactory) {
			factory.factoryErr[AuthorityPhaseMigrationTransaction] = errors.New("secret-projector")
		}, CodeTransactionBoundary, "runner-preledger-projector", true},
		{"authority", func(_ *runnerPreparedCurrentSessionFixture, factory *runnerPreflightProjectorFactory) {
			factory.projectionErr[AuthorityPhaseMigrationTransaction] = fail(CodeAuthorityDrift, "fake", "secret", errors.New("secret-authority"))
		}, CodeAuthorityDrift, "runner-preledger-authority", true},
		{"authority-result", func(_ *runnerPreparedCurrentSessionFixture, factory *runnerPreflightProjectorFactory) {
			factory.mutateResult[AuthorityPhaseMigrationTransaction] = func(result *ProjectionResult[AuthorityProjection]) {
				result.Digest = testDigest("wrong-preledger-authority")
			}
		}, CodeAuthorityDrift, "runner-preledger-authority", true},
		{"catalog", func(_ *runnerPreparedCurrentSessionFixture, factory *runnerPreflightProjectorFactory) {
			factory.catalogErr = fail(CodeCatalogDrift, "fake", "secret", errors.New("secret-catalog"))
		}, CodeCatalogDrift, "runner-preledger-catalog", true},
		{"catalog-digest", func(_ *runnerPreparedCurrentSessionFixture, factory *runnerPreflightProjectorFactory) {
			factory.mutateCatalog = func(result *ProjectionResult[CatalogProjection]) {
				result.Digest = testDigest("wrong-preledger-catalog")
			}
		}, CodeCatalogDrift, "runner-preledger-catalog", true},
		{"catalog-body", func(_ *runnerPreparedCurrentSessionFixture, factory *runnerPreflightProjectorFactory) {
			factory.mutateCatalog = func(result *ProjectionResult[CatalogProjection]) {
				result.Projection.Body.Schema.Owner = "other_owner"
				result.Digest, _ = digestProjectionWrapper(CatalogProjectionDigestDomain, result.Projection)
			}
		}, CodeCatalogDrift, "runner-preledger-catalog", true},
		{"catalog-scope", func(_ *runnerPreparedCurrentSessionFixture, factory *runnerPreflightProjectorFactory) {
			factory.mutateCatalog = func(result *ProjectionResult[CatalogProjection]) {
				result.Metadata.Scope.ScopeKind = "predecessor"
			}
		}, CodeProjectionMetadataMismatch, "runner-preledger-catalog", true},
		{"catalog-subject", func(_ *runnerPreparedCurrentSessionFixture, factory *runnerPreflightProjectorFactory) {
			factory.mutateCatalog = func(result *ProjectionResult[CatalogProjection]) {
				result.Metadata.VerifiedSubjectDigest = testDigest("wrong-preledger-subject")
			}
		}, CodeProjectionMetadataMismatch, "runner-preledger-catalog", true},
		{"execution-profile", func(f *runnerPreparedCurrentSessionFixture, _ *runnerPreflightProjectorFactory) {
			f.database.transaction.profileRestoreErr = errors.New("secret-restore")
		}, CodeTransactionBoundary, "runner-preledger-execution-profile", true},
		{"boundary", func(f *runnerPreparedCurrentSessionFixture, _ *runnerPreflightProjectorFactory) {
			f.database.transaction.boundaryErr = errors.New("secret-boundary")
		}, CodeTransactionBoundary, "runner-preledger-boundary", true},
		{"boundary-role", func(f *runnerPreparedCurrentSessionFixture, _ *runnerPreflightProjectorFactory) {
			f.database.transaction.boundaryMutate = func(boundary *BoundaryState) { boundary.CurrentUser = "other_role" }
		}, CodeTransactionBoundary, "runner-preledger-boundary", true},
		{"evidence-drift", func(f *runnerPreparedCurrentSessionFixture, _ *runnerPreflightProjectorFactory) {
			f.database.transaction.boundaryMutate = func(*BoundaryState) { f.evidence.snapshot.state = RecoveryDivergent }
		}, CodeEvidenceJournalFailed, "runner-preledger-evidence", false},
		{"status-drift", func(f *runnerPreparedCurrentSessionFixture, _ *runnerPreflightProjectorFactory) {
			f.database.transaction.boundaryMutate = func(*BoundaryState) { f.database.transaction.status = 'E' }
		}, CodeTransactionBoundary, "runner-preledger-boundary", true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture, after, runner, factory := newRunnerPreledgerFixture(t)
			test.configure(&fixture, factory)
			preledger, err := runner.projectCurrentPreledger(context.Background(), after)
			assertRunnerPreledgerError(t, err, test.wantCode, test.wantOp)
			if preledger != nil || containsErrorText(err, "secret-") || fixture.database.transaction.executeCalls != 1 || fixture.database.transaction.rollbackCalls != 1 || fixture.database.transaction.commitCalls != 0 || fixture.database.transaction.execCalls != 0 || fixture.database.unlockCalls != 1 || fixture.database.closeCalls != 1 || fixture.evidence.journal.appendCalls != 1 || fixture.database.backend.ledgerInsertCalls != 0 || fixture.database.backend.commitCalls != 0 || liveRunnerProjectedCurrentStatementAfters() != 0 || liveRunnerProjectedCurrentPreledgers() != 0 {
				t.Fatalf("pre-ledger fault escaped boundary: preledger=%+v err=%v transaction=%+v database=%+v", preledger, err, fixture.database.transaction, fixture.database)
			}
			if fixture.evidence.journal.cursor.Valid() != test.wantCursor {
				t.Fatalf("pre-ledger fault cursor validity=%t want=%t", fixture.evidence.journal.cursor.Valid(), test.wantCursor)
			}
			if cleanupErr := closeRunnerEvidenceOwnership(fixture.evidence, fixture.candidate); cleanupErr != nil {
				t.Fatal(cleanupErr)
			}
		})
	}
}

func TestRunnerPreledgerCleanupFailureDominatesAndAttemptsEveryRelease(t *testing.T) {
	fixture, after, runner, factory := newRunnerPreledgerFixture(t)
	factory.catalogErr = errors.New("secret-catalog")
	fixture.database.transaction.rollbackErr = errors.New("secret-rollback")
	fixture.database.transaction.rollbackLeavesOpen = true
	fixture.database.unlockErr = errors.New("secret-unlock")
	fixture.database.closeErr = errors.New("secret-close")
	preledger, err := runner.projectCurrentPreledger(context.Background(), after)
	assertRunnerPreledgerError(t, err, CodeTransactionBoundary, "runner-database-close")
	if preledger != nil || containsErrorText(err, "secret-") || fixture.database.transaction.rollbackCalls != 1 || fixture.database.unlockCalls != 1 || fixture.database.closeCalls != 1 || fixture.evidence.journal.appendCalls != 1 || fixture.database.backend.ledgerInsertCalls != 0 || liveRunnerProjectedCurrentPreledgers() != 0 {
		t.Fatalf("pre-ledger cleanup precedence/attempts: preledger=%+v err=%v transaction=%+v database=%+v", preledger, err, fixture.database.transaction, fixture.database)
	}
	if cleanupErr := closeRunnerEvidenceOwnership(fixture.evidence, fixture.candidate); cleanupErr != nil {
		t.Fatal(cleanupErr)
	}
}

func TestRunnerPreledgerHasOnlyReviewedProjectionAndBoundaryCallEdges(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "runner_preledger.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	calls := map[string]int{}
	allowed := map[string]bool{"ProjectAuthority": true, "ProjectCatalog": true, "Boundary": true}
	forbidden := map[string]bool{
		"BeginMigration": true, "ExecuteStatement": true, "AppendDurable": true,
		"ReserveAndActivateSuccessor": true, "Insert": true, "Commit": true,
		"Exec": true, "Query": true, "QueryRow": true,
		"ProjectTransitionState": true, "ProjectPrecondition": true,
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
			t.Fatalf("pre-ledger acquired forbidden %s call edge", selector.Sel.Name)
		}
		return true
	})
	for name, want := range map[string]int{"ProjectAuthority": 1, "ProjectCatalog": 1, "Boundary": 1} {
		if calls[name] != want {
			t.Fatalf("pre-ledger %s call edges=%d want=%d", name, calls[name], want)
		}
	}
}

func TestRunnerProjectedPreledgerHasNoProductionConsumer(t *testing.T) {
	paths, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	symbols := map[string]bool{
		"runnerProjectedCurrentPreledger": true, "runnerProjectedCurrentPreledgerBinding": true,
		"runnerProjectedCurrentPreledgerRegistryRecord": true, "runnerProjectedCurrentPreledgerRegistry": true,
		"runnerProjectedCurrentPreledgerSeed": true, "projectCurrentPreledger": true,
		"consumeRunnerProjectedCurrentStatementAfter": true, "bindRunnerProjectedCurrentPreledger": true,
		"validRunnerProjectedCurrentPreledger": true, "closeRunnerProjectedCurrentPreledger": true,
	}
	consumer := map[string]bool{"projectCurrentPreledger": true}
	successKernelAllowed := map[string]bool{"runnerProjectedCurrentPreledgerSeed": true}
	for _, path := range paths {
		name := filepath.Base(path)
		if strings.HasSuffix(name, "_test.go") || name == "runner_preledger.go" || name == "runner_final_intermediate.go" {
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
				t.Fatalf("pre-ledger authority %s acquired unreviewed production consumer %s", identifier.Name, name)
			}
			return true
		})
	}
}

func newRunnerPreledgerFixture(t *testing.T) (runnerPreparedCurrentSessionFixture, *runnerProjectedCurrentStatementAfter, *Runner, *runnerPreflightProjectorFactory) {
	t.Helper()
	fixture, executed, runner, factory := newRunnerStatementAfterFixture(t)
	after, err := runner.projectCurrentStatementAfter(context.Background(), executed)
	if err != nil || !validRunnerProjectedCurrentStatementAfter(after) || !after.finalStatement {
		t.Fatalf("pre-ledger fixture statement-after: after=%+v err=%v", after, err)
	}
	return fixture, after, runner, factory
}

func expectedRunnerPreledgerCatalog(t *testing.T, evidence EvidenceSession, plan StatementPlan) CatalogProjection {
	t.Helper()
	current := evidence.CurrentCandidate()
	bindings, err := runnerCurrentProjectionBindings(evidence, current)
	if err != nil {
		t.Fatal(err)
	}
	catalog, ok := exactCatalogBindingForHead(bindings.executableCatalogs, plan.MigrationID)
	if !ok {
		t.Fatal("exact final catalog binding is unavailable")
	}
	return catalog.verifiedCatalog.ExpectedProjection()
}

func wantCatalogScope(projection CatalogProjection) ProjectionScope {
	head := projection.SchemaHead
	return ProjectionScope{ScopeKind: "final", SchemaHead: &head, DeclaredObjects: cloneProjectionValue(projection.Body.DeclaredObjects)}
}

func assertProjectedPreledgerDrift(t *testing.T, preledger *runnerProjectedCurrentPreledger) {
	t.Helper()
	if validRunnerProjectedCurrentPreledger(preledger) {
		t.Fatal("mutated pre-ledger authority remained valid")
	}
}

func liveRunnerProjectedCurrentPreledgers() int {
	count := 0
	runnerProjectedCurrentPreledgerRegistry.Range(func(_, _ any) bool {
		count++
		return true
	})
	return count
}

func assertRunnerPreledgerError(t *testing.T, err error, code ErrorCode, op string) {
	t.Helper()
	var migrationErr *Error
	if !errors.As(err, &migrationErr) || migrationErr.Code != code || migrationErr.Op != op || migrationErr.Err != nil {
		t.Fatalf("pre-ledger error: got=%#v want=%s/%s", migrationErr, code, op)
	}
}
