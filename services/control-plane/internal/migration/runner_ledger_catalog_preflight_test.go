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

type runnerLedgerCatalogPreflightFixture struct {
	base      runnerPreparedCurrentSessionFixture
	runner    *Runner
	connector *runnerPreflightConnector
	database  *runnerPreflightSession
	factory   *runnerPreflightProjectorFactory
}

func newRunnerLedgerCatalogPreflightFixture(t *testing.T) runnerLedgerCatalogPreflightFixture {
	t.Helper()
	raw, decision := buildExactAdmissionRuntime(t)
	return newRunnerLedgerCatalogPreflightFixtureFromRuntime(t, raw, decision)
}

func newRunnerLedgerCatalogPreflightFixtureFromRuntime(t *testing.T, raw []byte, decision VerifiedTrustDecision) runnerLedgerCatalogPreflightFixture {
	t.Helper()
	base := newRunnerPreparedCurrentSessionFixtureFromRuntime(t, raw, decision)
	database := newRunnerPreflightSession()
	factory := &runnerPreflightProjectorFactory{allowMigrationRoleCatalog: true}
	factory.initialize()
	connector := &runnerPreflightConnector{session: database}
	return runnerLedgerCatalogPreflightFixture{
		base: base, database: database, factory: factory, connector: connector,
		runner: &Runner{Connector: connector, projectionFactory: factory},
	}
}

func (fixture runnerLedgerCatalogPreflightFixture) close(t *testing.T, prepared *runnerLedgerCatalogPreflight) {
	t.Helper()
	if fixture.base.database != nil && !fixture.base.database.closed {
		if err := closeRunnerDatabasePreflight(fixture.base.database, fixture.base.key, true, nil); err != nil {
			t.Fatal(err)
		}
	}
	if err := closeRunnerEvidenceOwnership(fixture.base.evidence, fixture.base.candidate); err != nil {
		t.Fatal(err)
	}
}

func TestRunnerLedgerCatalogPreflightProjectsPartialPrefixReadOnly(t *testing.T) {
	raw, decision := buildExactTwoMigrationAdmissionRuntime(t)
	fixture := newRunnerLedgerCatalogPreflightFixtureFromRuntime(t, raw, decision)
	first := fixture.base.bundle.Manifest.SchemaBundle.Migrations[0]
	row := ledgerRowFor(first, fixture.base.bundle.Manifest.SchemaBundleDigest)
	fixture.database.ledgerRowsByRead = [][]LedgerRow{{row}, {cloneProjectionValue(row)}}
	prepared, err := fixture.runner.projectRunnerLedgerCatalogPreflight(
		context.Background(), "test-only", fixture.base.bundle, fixture.base.plans,
		fixture.base.evidence, fixture.base.candidate,
	)
	defer fixture.close(t, prepared)
	if err != nil || !validRunnerLedgerCatalogPreflight(prepared) || prepared.state != runnerLedgerCatalogPartial {
		t.Fatalf("partial read-only projection: prepared=%+v err=%v", prepared, err)
	}
	if prepared.migrationCount != 2 || prepared.initialPredecessor != nil || prepared.cumulativeCatalog == nil || prepared.catalogContractDigest == nil || *prepared.catalogContractDigest != first.CatalogContract.SHA256 ||
		prepared.cumulativeCatalog.Projection.SchemaHead != first.ID || prepared.ledger.head != first.ID || len(prepared.ledger.rows) != 1 {
		t.Fatalf("partial projection shape=%+v", prepared)
	}
	wantSnapshots := []AuthorityPhase{AuthorityPhaseConnectedSession, AuthorityPhaseMigrationRole, AuthorityPhaseMigrationRole}
	if fixture.connector.attempts != 1 || fixture.database.setRoleCalls != 1 || fixture.database.lockCalls != 1 ||
		fixture.database.unlockCalls != 1 || fixture.database.closeCalls != 1 || fixture.database.ledgerReadCalls != 2 ||
		fixture.database.beginCalls != 0 || fixture.database.boundaryCalls != 0 || fixture.database.queryCalls != 0 ||
		fixture.database.serverMajorCalls != 0 || fixture.database.backend.ledgerInsertCalls != 0 ||
		fixture.database.backend.executeCalls != 0 || fixture.database.backend.commitCalls != 0 ||
		!fixture.database.closed || fixture.database.locked || fixture.database.roleConfigured || fixture.database.projectionActive ||
		!reflect.DeepEqual(fixture.database.snapshotPhases, wantSnapshots) ||
		!reflect.DeepEqual(fixture.database.snapshotClosePhases, wantSnapshots) ||
		!reflect.DeepEqual(fixture.factory.projectionPhases, []AuthorityPhase{AuthorityPhaseConnectedSession, AuthorityPhaseMigrationRole}) ||
		len(fixture.factory.preconditionPhases) != 0 || len(fixture.factory.catalogPhases) != 1 ||
		fixture.factory.catalogPhases[0] != AuthorityPhaseMigrationRole || len(fixture.factory.catalogScopes) != 1 ||
		fixture.factory.catalogScopes[0].SchemaHead == nil || *fixture.factory.catalogScopes[0].SchemaHead != first.ID {
		t.Fatalf("partial lifecycle escaped: connector=%+v database=%+v factory=%+v", fixture.connector, fixture.database, fixture.factory)
	}
}

func TestRunnerLedgerCatalogPreflightProjectsEmptyAndCompletePrefixesReadOnly(t *testing.T) {
	for _, test := range []struct {
		name                 string
		rows                 func(runnerPreparedCurrentSessionFixture) [][]LedgerRow
		wantState            runnerLedgerCatalogState
		wantPrecondition     int
		wantCatalog          int
		wantProjectionPhases []AuthorityPhase
	}{
		{
			name: "empty-initial-predecessor",
			rows: func(runnerPreparedCurrentSessionFixture) [][]LedgerRow {
				return [][]LedgerRow{{}, {}}
			},
			wantState: runnerLedgerCatalogEmpty, wantPrecondition: 1,
			wantProjectionPhases: []AuthorityPhase{AuthorityPhaseConnectedSession, AuthorityPhaseMigrationRole},
		},
		{
			name: "complete-cumulative-catalog",
			rows: func(base runnerPreparedCurrentSessionFixture) [][]LedgerRow {
				row := ledgerRowFor(base.bundle.Manifest.SchemaBundle.Migrations[0], base.bundle.Manifest.SchemaBundleDigest)
				return [][]LedgerRow{{row}, {cloneProjectionValue(row)}}
			},
			wantState: runnerLedgerCatalogComplete, wantCatalog: 1,
			wantProjectionPhases: []AuthorityPhase{AuthorityPhaseConnectedSession, AuthorityPhaseMigrationRole},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newRunnerLedgerCatalogPreflightFixture(t)
			fixture.database.ledgerRowsByRead = test.rows(fixture.base)
			prepared, err := fixture.runner.projectRunnerLedgerCatalogPreflight(
				context.Background(), "test-only", fixture.base.bundle, fixture.base.plans,
				fixture.base.evidence, fixture.base.candidate,
			)
			defer fixture.close(t, prepared)
			if err != nil || !validRunnerLedgerCatalogPreflight(prepared) || prepared.state != test.wantState {
				t.Fatalf("read-only projection: prepared=%+v err=%v", prepared, err)
			}
			if test.wantState == runnerLedgerCatalogEmpty {
				if prepared.initialPredecessor == nil || prepared.cumulativeCatalog != nil || prepared.catalogContractDigest != nil || prepared.ledger.head != "" || len(prepared.ledger.rows) != 0 {
					t.Fatalf("empty projection shape=%+v", prepared)
				}
			} else if prepared.initialPredecessor != nil || prepared.cumulativeCatalog == nil || prepared.catalogContractDigest == nil || prepared.ledger.head != "000001" || len(prepared.ledger.rows) != 1 {
				t.Fatalf("cumulative projection shape=%+v", prepared)
			}
			wantSnapshots := []AuthorityPhase{AuthorityPhaseConnectedSession, AuthorityPhaseMigrationRole, AuthorityPhaseMigrationRole}
			if fixture.connector.attempts != 1 || fixture.database.setRoleCalls != 1 || fixture.database.lockCalls != 1 ||
				fixture.database.unlockCalls != 1 || fixture.database.closeCalls != 1 || fixture.database.ledgerReadCalls != 2 ||
				fixture.database.beginCalls != 0 || fixture.database.boundaryCalls != 0 || fixture.database.queryCalls != 0 ||
				fixture.database.serverMajorCalls != 0 || fixture.database.backend.ledgerInsertCalls != 0 ||
				fixture.database.backend.executeCalls != 0 || fixture.database.backend.commitCalls != 0 ||
				!fixture.database.closed || fixture.database.locked || fixture.database.roleConfigured || fixture.database.projectionActive ||
				!reflect.DeepEqual(fixture.database.snapshotPhases, wantSnapshots) ||
				!reflect.DeepEqual(fixture.database.snapshotClosePhases, wantSnapshots) ||
				!reflect.DeepEqual(fixture.factory.projectionPhases, test.wantProjectionPhases) ||
				len(fixture.factory.preconditionPhases) != test.wantPrecondition || len(fixture.factory.catalogPhases) != test.wantCatalog {
				t.Fatalf("read-only lifecycle escaped: connector=%+v database=%+v factory=%+v", fixture.connector, fixture.database, fixture.factory)
			}
			if test.wantCatalog == 1 && (fixture.factory.catalogPhases[0] != AuthorityPhaseMigrationRole || len(fixture.factory.catalogScopes) != 1 || fixture.factory.catalogScopes[0].SchemaHead == nil || *fixture.factory.catalogScopes[0].SchemaHead != "000001") {
				t.Fatalf("cumulative catalog scope=%+v phases=%+v", fixture.factory.catalogScopes, fixture.factory.catalogPhases)
			}
		})
	}
}

func TestRunnerLedgerCatalogPreflightUsesOnlyOwnedRuntimeAndPlans(t *testing.T) {
	fixture := newRunnerLedgerCatalogPreflightFixture(t)
	bundle := *fixture.base.bundle
	bundle.Manifest = cloneProjectionValue(fixture.base.bundle.Manifest)
	bundle.Manifest.SchemaBundle.Migrations[0].Name = "caller-mutated"
	bundle.Lineage = nil
	bundle.Files = nil
	prepared, err := fixture.runner.projectRunnerLedgerCatalogPreflight(
		context.Background(), "test-only", &bundle, []StatementPlan{{}}, fixture.base.evidence, fixture.base.candidate,
	)
	defer fixture.close(t, prepared)
	if err != nil || !validRunnerLedgerCatalogPreflight(prepared) || prepared.state != runnerLedgerCatalogEmpty || fixture.connector.attempts != 1 {
		t.Fatalf("owned runtime result=%+v err=%v attempts=%d", prepared, err, fixture.connector.attempts)
	}
}

func TestRunnerLedgerCatalogPreflightRejectsOwnedRuntimeDriftBeforeConnect(t *testing.T) {
	fixture := newRunnerLedgerCatalogPreflightFixture(t)
	bundle := *fixture.base.bundle
	bundle.ownedInputs.manifest = cloneProjectionValue(fixture.base.bundle.ownedInputs.manifest)
	bundle.ownedInputs.manifest.SchemaBundle.Migrations[0].Name = "owned-input-drift"
	prepared, err := fixture.runner.projectRunnerLedgerCatalogPreflight(
		context.Background(), "test-only", &bundle, fixture.base.plans, fixture.base.evidence, fixture.base.candidate,
	)
	defer fixture.close(t, prepared)
	if prepared != nil || !IsCode(err, CodeUntrusted) || fixture.connector.attempts != 0 || fixture.database.closeCalls != 0 || fixture.database.lockCalls != 0 {
		t.Fatalf("owned drift result=%+v err=%v connector=%+v database=%+v", prepared, err, fixture.connector, fixture.database)
	}
}

func TestRunnerLedgerCatalogPreflightRejectsDriftBeforeSealing(t *testing.T) {
	for _, test := range []struct {
		name     string
		prepare  func(*runnerLedgerCatalogPreflightFixture)
		wantCode ErrorCode
	}{
		{
			name: "ledger-second-read",
			prepare: func(fixture *runnerLedgerCatalogPreflightFixture) {
				row := ledgerRowFor(fixture.base.bundle.Manifest.SchemaBundle.Migrations[0], fixture.base.bundle.Manifest.SchemaBundleDigest)
				fixture.database.ledgerRowsByRead = [][]LedgerRow{{}, {row}}
			},
			wantCode: CodeInvalidLedger,
		},
		{
			name: "catalog-body",
			prepare: func(fixture *runnerLedgerCatalogPreflightFixture) {
				row := ledgerRowFor(fixture.base.bundle.Manifest.SchemaBundle.Migrations[0], fixture.base.bundle.Manifest.SchemaBundleDigest)
				fixture.database.ledgerRowsByRead = [][]LedgerRow{{row}, {row}}
				fixture.factory.mutateCatalog = func(result *ProjectionResult[CatalogProjection]) {
					result.Digest = testDigest("wrong-cumulative-catalog")
				}
			},
			wantCode: CodeCatalogDrift,
		},
		{
			name: "catalog-session-identity",
			prepare: func(fixture *runnerLedgerCatalogPreflightFixture) {
				row := ledgerRowFor(fixture.base.bundle.Manifest.SchemaBundle.Migrations[0], fixture.base.bundle.Manifest.SchemaBundleDigest)
				fixture.database.ledgerRowsByRead = [][]LedgerRow{{row}, {row}}
				fixture.database.snapshotMetadataNth[3] = func(metadata *SnapshotMetadata) {
					metadata.DatabaseName = "foreign_database"
				}
			},
			wantCode: CodeProjectionMetadataMismatch,
		},
		{
			name: "authority-session-identity",
			prepare: func(fixture *runnerLedgerCatalogPreflightFixture) {
				fixture.database.snapshotMetadataNth[2] = func(metadata *SnapshotMetadata) {
					metadata.ServerVersionNum++
				}
			},
			wantCode: CodeProjectionMetadataMismatch,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newRunnerLedgerCatalogPreflightFixture(t)
			test.prepare(&fixture)
			prepared, err := fixture.runner.projectRunnerLedgerCatalogPreflight(
				context.Background(), "test-only", fixture.base.bundle, fixture.base.plans,
				fixture.base.evidence, fixture.base.candidate,
			)
			defer fixture.close(t, prepared)
			if prepared != nil || !IsCode(err, test.wantCode) {
				t.Fatalf("drift result=%+v err=%v", prepared, err)
			}
			if fixture.database.unlockCalls != 1 || fixture.database.closeCalls != 1 || !fixture.database.closed || fixture.database.beginCalls != 0 || fixture.database.backend.ledgerInsertCalls != 0 {
				t.Fatalf("drift cleanup escaped: database=%+v", fixture.database)
			}
		})
	}
}

func TestRunnerLedgerCatalogPreflightPreservesCleanupErrorPrecedence(t *testing.T) {
	primary := fail(CodeInvalidLedger, "fixture-primary", "fixture primary", nil)
	for _, test := range []struct {
		name       string
		primary    error
		unlock     error
		close      error
		wantCode   ErrorCode
		wantOp     string
		wantReads  int
		wantUnlock int
	}{
		{"primary", primary, nil, nil, CodeInvalidLedger, "runner-ledger-preflight", 1, 1},
		{"unlock", nil, errors.New("unlock"), nil, CodeTransactionBoundary, "runner-advisory-unlock", 2, 1},
		{"close", nil, nil, errors.New("close"), CodeTransactionBoundary, "runner-database-close", 2, 1},
		{"primary-over-unlock", primary, errors.New("unlock"), nil, CodeInvalidLedger, "runner-ledger-preflight", 1, 1},
		{"close-over-primary", primary, nil, errors.New("close"), CodeTransactionBoundary, "runner-database-close", 1, 1},
		{"close-over-unlock", nil, errors.New("unlock"), errors.New("close"), CodeTransactionBoundary, "runner-database-close", 2, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newRunnerLedgerCatalogPreflightFixture(t)
			if test.primary != nil {
				fixture.database.ledgerReadErr[1] = test.primary
			}
			fixture.database.unlockErr = test.unlock
			fixture.database.closeErr = test.close
			prepared, err := fixture.runner.projectRunnerLedgerCatalogPreflight(
				context.Background(), "test-only", fixture.base.bundle, fixture.base.plans,
				fixture.base.evidence, fixture.base.candidate,
			)
			defer fixture.close(t, prepared)
			var migrationErr *Error
			if prepared != nil || !errors.As(err, &migrationErr) || migrationErr.Code != test.wantCode || migrationErr.Op != test.wantOp || migrationErr.Err != nil ||
				fixture.database.ledgerReadCalls != test.wantReads || fixture.database.unlockCalls != test.wantUnlock || fixture.database.closeCalls != 1 ||
				fixture.database.beginCalls != 0 {
				t.Fatalf("cleanup precedence: result=%+v err=%#v database=%+v", prepared, migrationErr, fixture.database)
			}
		})
	}
}

func TestRunnerLedgerCatalogPreflightPreservesOrdinaryCopyAndRejectsLiteralOrTamper(t *testing.T) {
	fixture := newRunnerLedgerCatalogPreflightFixture(t)
	prepared, err := fixture.runner.projectRunnerLedgerCatalogPreflight(
		context.Background(), "test-only", fixture.base.bundle, fixture.base.plans,
		fixture.base.evidence, fixture.base.candidate,
	)
	defer fixture.close(t, prepared)
	if err != nil || !validRunnerLedgerCatalogPreflight(prepared) {
		t.Fatalf("bind: prepared=%+v err=%v", prepared, err)
	}
	valueType := reflect.TypeOf(*prepared)
	for index := 0; index < valueType.NumField(); index++ {
		if valueType.Field(index).PkgPath == "" {
			t.Fatalf("sealed result field %s became public", valueType.Field(index).Name)
		}
	}
	copyValue := *prepared
	if !validRunnerLedgerCatalogPreflight(&copyValue) || validRunnerLedgerCatalogPreflight(&runnerLedgerCatalogPreflight{}) {
		t.Fatal("ordinary copy or literal validation mismatch")
	}

	originalState := prepared.state
	prepared.state = runnerLedgerCatalogComplete
	assertRunnerLedgerCatalogPreflightInvalid(t, prepared)
	prepared.state = originalState

	originalLedger := prepared.ledger.digest
	prepared.ledger.digest = testDigest("tampered-ledger")
	assertRunnerLedgerCatalogPreflightInvalid(t, prepared)
	prepared.ledger.digest = originalLedger

	originalProjection := prepared.initialPredecessor.Digest
	prepared.initialPredecessor.Digest = testDigest("tampered-predecessor")
	assertRunnerLedgerCatalogPreflightInvalid(t, prepared)
	prepared.initialPredecessor.Digest = originalProjection

	originalSubject := prepared.subjectDigest
	prepared.subjectDigest = testDigest("tampered-subject")
	assertRunnerLedgerCatalogPreflightInvalid(t, prepared)
	prepared.subjectDigest = originalSubject

	originalProfile := prepared.profileDigest
	prepared.profileDigest = string(testDigest("foreign-profile"))
	assertRunnerLedgerCatalogPreflightInvalid(t, prepared)
	prepared.profileDigest = originalProfile

	wire := prepared.wire()
	wire.LedgerDigest = testDigest("caller-wire-drift")
	if !validRunnerLedgerCatalogPreflight(prepared) {
		t.Fatal("owned wire copy mutated the sealed result")
	}
}

func TestRunnerLedgerCatalogPreflightHasNoWriterConsumerOrAuthorityHandle(t *testing.T) {
	resultType := reflect.TypeOf(runnerLedgerCatalogPreflight{})
	for index := 0; index < resultType.NumField(); index++ {
		field := resultType.Field(index)
		name := strings.ToLower(field.Name)
		for _, forbidden := range []string{"session", "transaction", "evidence", "receipt", "writer", "token"} {
			if strings.Contains(name, forbidden) {
				t.Fatalf("sealed result retained forbidden field %s", field.Name)
			}
		}
		for _, contract := range []reflect.Type{
			reflect.TypeOf((*DatabaseSession)(nil)).Elem(), reflect.TypeOf((*MigrationTransaction)(nil)).Elem(), reflect.TypeOf((*EvidenceSession)(nil)).Elem(),
		} {
			if field.Type.Implements(contract) || reflect.PointerTo(field.Type).Implements(contract) {
				t.Fatalf("sealed result field %s implements authority handle %s", field.Name, contract)
			}
		}
	}

	forbiddenCalls := map[string]bool{
		"BeginMigration": true, "ExecuteStatement": true, "Commit": true, "Append": true, "Insert": true,
		"runCurrentSingleEntry": true, "prepareCurrentDatabaseSession": true, "bindRunnerPreparedCurrentSession": true,
		"bindRunnerLedgerPreflightFact": true, "transition": true,
	}
	file, err := parser.ParseFile(token.NewFileSet(), "runner_ledger_catalog_preflight.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
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
		if forbiddenCalls[name] {
			t.Fatalf("read-only kernel acquired forbidden call edge %s", name)
		}
		return true
	})

	paths, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		production, parseErr := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if parseErr != nil {
			t.Fatal(parseErr)
		}
		ast.Inspect(production, func(node ast.Node) bool {
			selector, ok := node.(*ast.SelectorExpr)
			if ok && selector.Sel.Name == "projectRunnerLedgerCatalogPreflight" {
				t.Fatalf("%s added an unreviewed production kernel consumer", path)
			}
			return true
		})
	}
}

func TestRunnerLedgerCatalogPreflightPreCanceledContextHasNoDatabaseEffect(t *testing.T) {
	fixture := newRunnerLedgerCatalogPreflightFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	prepared, err := fixture.runner.projectRunnerLedgerCatalogPreflight(
		ctx, "test-only", fixture.base.bundle, fixture.base.plans, fixture.base.evidence, fixture.base.candidate,
	)
	defer fixture.close(t, prepared)
	if prepared != nil || !IsCode(err, CodeContextCanceled) || fixture.connector.attempts != 0 || fixture.database.closeCalls != 0 || fixture.database.lockCalls != 0 {
		t.Fatalf("pre-canceled result=%+v err=%v connector=%+v database=%+v", prepared, err, fixture.connector, fixture.database)
	}
}

func TestRunnerLedgerCatalogPreflightCancellationAfterFinalReadCleansBeforeRejecting(t *testing.T) {
	fixture := newRunnerLedgerCatalogPreflightFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	fixture.database.afterLedgerRead[2] = cancel
	prepared, err := fixture.runner.projectRunnerLedgerCatalogPreflight(
		ctx, "test-only", fixture.base.bundle, fixture.base.plans, fixture.base.evidence, fixture.base.candidate,
	)
	defer fixture.close(t, prepared)
	if prepared != nil || !IsCode(err, CodeContextCanceled) || fixture.database.ledgerReadCalls != 2 ||
		fixture.database.unlockCalls != 1 || fixture.database.closeCalls != 1 || !fixture.database.closed ||
		fixture.database.beginCalls != 0 || fixture.database.backend.ledgerInsertCalls != 0 {
		t.Fatalf("post-read cancellation: result=%+v err=%v database=%+v", prepared, err, fixture.database)
	}
}

func assertRunnerLedgerCatalogPreflightInvalid(t *testing.T, prepared *runnerLedgerCatalogPreflight) {
	t.Helper()
	if validRunnerLedgerCatalogPreflight(prepared) {
		t.Fatal("tampered read-only projection remained valid")
	}
}
