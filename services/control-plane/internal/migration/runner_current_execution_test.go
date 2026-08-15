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
	"time"
)

func TestRunnerCurrentExecutionScopeIsClosedBeforeAuthorityOrDatabaseUse(t *testing.T) {
	raw, decision := buildExactAdmissionRuntime(t)
	bundle, err := LoadRuntimeBundle(raw, decision)
	if err != nil {
		t.Fatal(err)
	}
	bindings, err := decision.runnerProjectionBindings()
	if err != nil {
		t.Fatal(err)
	}
	plans, err := buildExactStatementPlans(bundle, bindings, time.Now())
	if err != nil || len(plans) != 1 {
		t.Fatalf("scope fixture plans=%d err=%v", len(plans), err)
	}
	cloneInput := func() (*RuntimeBundle, []StatementPlan) {
		bundleCopy := *bundle
		manifest := cloneProjectionValue(*bundle.Manifest)
		bundleCopy.Manifest = &manifest
		plan, cloneErr := cloneRunnerStatementIntentPlan(plans[0])
		if cloneErr != nil {
			t.Fatal(cloneErr)
		}
		return &bundleCopy, []StatementPlan{plan}
	}
	for _, test := range []struct {
		name   string
		mutate func(**RuntimeBundle, *[]StatementPlan)
		code   ErrorCode
	}{
		{"exact", nil, ""},
		{"nil-bundle", func(bundle **RuntimeBundle, _ *[]StatementPlan) { *bundle = nil }, CodeInvalidManifest},
		{"nil-manifest", func(bundle **RuntimeBundle, _ *[]StatementPlan) { (*bundle).Manifest = nil }, CodeInvalidManifest},
		{"no-entry", func(bundle **RuntimeBundle, _ *[]StatementPlan) { (*bundle).Manifest.SchemaBundle.Migrations = nil }, CodeProjectionNotImplemented},
		{"two-entry", func(bundle **RuntimeBundle, _ *[]StatementPlan) {
			entry := cloneProjectionValue((*bundle).Manifest.SchemaBundle.Migrations[0])
			(*bundle).Manifest.SchemaBundle.Migrations = append((*bundle).Manifest.SchemaBundle.Migrations, entry)
		}, CodeProjectionNotImplemented},
		{"two-plan", func(_ **RuntimeBundle, plans *[]StatementPlan) { *plans = append(*plans, (*plans)[0]) }, CodeProjectionNotImplemented},
		{"migration-swap", func(_ **RuntimeBundle, plans *[]StatementPlan) { (*plans)[0].MigrationID = "000002" }, CodeUntrusted},
		{"statement-swap", func(_ **RuntimeBundle, plans *[]StatementPlan) { (*plans)[0].StatementIndex = 1 }, CodeUntrusted},
	} {
		t.Run(test.name, func(t *testing.T) {
			candidateBundle, candidatePlans := cloneInput()
			if test.mutate != nil {
				test.mutate(&candidateBundle, &candidatePlans)
			}
			err := validateRunnerCurrentExecutionScope(candidateBundle, candidatePlans)
			if test.code == "" && err != nil {
				t.Fatalf("exact scope rejected: %v", err)
			}
			if test.code != "" {
				var migrationErr *Error
				if !errors.As(err, &migrationErr) || migrationErr.Code != test.code || migrationErr.Op != "runner-current-execution-scope" || migrationErr.Err != nil {
					t.Fatalf("scope error=%#v want=%s", migrationErr, test.code)
				}
			}
		})
	}
}

func TestRunnerCurrentExecutionRejectsNextEntryAfterDurableCommitWithoutLeak(t *testing.T) {
	fixture, prepared, runner := newRunnerPreparedCurrentStatementIntentFixture(t)
	fixture.database.transaction.executeAllowed = true
	factory, ok := runner.projectionFactory.(*runnerPreflightProjectorFactory)
	if !ok || factory == nil {
		t.Fatal("current execution fixture has no projection factory")
	}
	bindings, err := fixture.candidate.verifiedRun.currentDecision.decision.runnerProjectionBindings()
	if err != nil {
		t.Fatal(err)
	}
	catalog, ok := exactCatalogBindingForHead(bindings.executableCatalogs, fixture.plans[0].MigrationID)
	if !ok {
		t.Fatal("current execution catalog is unavailable")
	}
	expected := catalog.verifiedCatalog.ExpectedProjection()
	state := CatalogStateProjection{Present: &SchemaPresentProjection{
		State: "schema_present", Scope: cloneProjectionValue(fixture.plans[0].ExpectedTransition.CatalogAfter.Scope),
		Body: cloneProjectionValue(expected.Body),
	}}
	factory.transitionState = &state
	fixture.evidence.journal.bundleComplete = false
	durable, err := runner.appendCurrentStatementIntent(context.Background(), prepared)
	if err != nil {
		t.Fatal(err)
	}
	result, err := runner.runCurrentSingleEntry(context.Background(), durable, fixture.bundle, fixture.plans)
	var migrationErr *Error
	if !errors.As(err, &migrationErr) || migrationErr.Code != CodeEvidenceJournalFailed || migrationErr.Op != "runner-current-execution-result" || migrationErr.Err != nil || !reflect.DeepEqual(result, RunResult{}) {
		t.Fatalf("next-entry result=%+v err=%#v", result, migrationErr)
	}
	if fixture.database.transaction.commitCalls != 1 || fixture.database.transaction.rollbackCalls != 0 || fixture.database.closeCalls != 1 || fixture.evidence.journal.appendCalls != 4 || liveRunnerDurableCurrentStatementIntents() != 0 || liveRunnerExecutedCurrentStatements() != 0 || liveRunnerProjectedCurrentStatementAfters() != 0 || liveRunnerProjectedCurrentPreledgers() != 0 || liveRunnerDurableFinalIntermediates() != 0 || liveRunnerReadbackCurrentLedgers() != 0 || liveRunnerDurableCommitIntents() != 0 || liveRunnerClosedCurrentCommits() != 0 || liveRunnerDurableCommittedTerminals() != 0 {
		t.Fatalf("next-entry cleanup escaped: database=%+v transaction=%+v evidence=%+v", fixture.database, fixture.database.transaction, fixture.evidence)
	}
	if cleanupErr := closeRunnerEvidenceOwnership(fixture.evidence, fixture.candidate); cleanupErr != nil {
		t.Fatal(cleanupErr)
	}
}

func TestRunnerCurrentExecutionHasOneOrderedTransitionChainAndNoDirectMutation(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "runner_current_execution.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"executeCurrentStatement", "projectCurrentStatementAfter", "projectCurrentPreledger",
		"appendCurrentFinalIntermediate", "insertAndReadbackCurrentLedger", "appendCurrentCommitIntent",
		"commitCurrentTransaction", "appendCommittedTerminal",
	}
	forbidden := map[string]bool{
		"ExecuteStatement": true, "AppendDurable": true, "Commit": true, "Rollback": true,
		"Insert": true, "Exec": true, "Query": true, "QueryRow": true, "BeginMigration": true,
		"ReserveAndActivateSuccessor": true,
	}
	var got []string
	ast.Inspect(file, func(node ast.Node) bool {
		selector, ok := node.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if forbidden[selector.Sel.Name] {
			t.Fatalf("current execution acquired direct mutation edge %s", selector.Sel.Name)
		}
		for _, name := range want {
			if selector.Sel.Name == name {
				got = append(got, name)
			}
		}
		return true
	})
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("current execution chain=%v want=%v", got, want)
	}
}

func TestRunnerCurrentExecutionScopePrecedesAuthorityAndDatabaseSideEffects(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "runner.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	positions := map[string]token.Pos{}
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		name := ""
		switch function := call.Fun.(type) {
		case *ast.Ident:
			name = function.Name
		case *ast.SelectorExpr:
			name = function.Sel.Name
		}
		if _, tracked := map[string]bool{
			"validateRunnerCurrentExecutionScope": true, "bindVerifierOwnedDecision": true,
			"openRunnerEvidenceSession": true, "prepareCurrentDatabaseSession": true,
		}[name]; tracked && positions[name] == 0 {
			positions[name] = call.Pos()
		}
		return true
	})
	validation := positions["validateRunnerCurrentExecutionScope"]
	if validation == 0 || positions["bindVerifierOwnedDecision"] <= validation || positions["openRunnerEvidenceSession"] <= validation || positions["prepareCurrentDatabaseSession"] <= validation {
		t.Fatalf("current execution scope ordering=%v", positions)
	}
}

func TestRunnerCurrentExecutionHasOnlyPublicRunAsProductionConsumer(t *testing.T) {
	paths, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	symbols := map[string]bool{"runCurrentSingleEntry": true, "validateRunnerCurrentExecutionScope": true}
	allowed := map[string]bool{"runCurrentSingleEntry": true, "validateRunnerCurrentExecutionScope": true}
	for _, path := range paths {
		name := filepath.Base(path)
		if strings.HasSuffix(name, "_test.go") || name == "runner_current_execution.go" {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			identifier, ok := node.(*ast.Ident)
			if ok && symbols[identifier.Name] && !(name == "runner.go" && allowed[identifier.Name]) {
				t.Fatalf("current execution %s acquired unreviewed production consumer %s", identifier.Name, name)
			}
			return true
		})
	}
}
