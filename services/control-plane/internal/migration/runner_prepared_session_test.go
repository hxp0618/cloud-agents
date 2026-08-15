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

func TestRunnerPreparedCurrentSessionRejectsLiteralCopyAndFieldDrift(t *testing.T) {
	fixture := newRunnerPreparedCurrentSessionFixture(t)
	prepared, err := bindRunnerPreparedCurrentSession(
		fixture.database, fixture.evidence, fixture.key, fixture.candidate, fixture.snapshot,
		fixture.ledger, fixture.authority, fixture.precondition, fixture.bundle, fixture.plans,
	)
	if err != nil || !validRunnerPreparedCurrentSession(prepared) {
		t.Fatalf("prepared session bind: prepared=%+v err=%v", prepared, err)
	}
	if count := liveRunnerPreparedCurrentSessions(); count != 1 {
		t.Fatalf("live prepared sessions=%d want=1", count)
	}
	valueType := reflect.TypeOf(*prepared)
	for index := 0; index < valueType.NumField(); index++ {
		if valueType.Field(index).PkgPath == "" {
			t.Fatalf("prepared session field %s became public", valueType.Field(index).Name)
		}
	}

	copyValue := *prepared
	if err := closeRunnerPreparedCurrentSession(&copyValue, nil); !IsCode(err, CodeTransactionBoundary) || fixture.database.closeCalls != 0 || !validRunnerPreparedCurrentSession(prepared) {
		t.Fatalf("prepared copy close changed original authority: err=%v close=%d", err, fixture.database.closeCalls)
	}
	if err := closeRunnerPreparedCurrentSession(&runnerPreparedCurrentSession{}, nil); !IsCode(err, CodeTransactionBoundary) || fixture.database.closeCalls != 0 {
		t.Fatalf("literal prepared close escaped: err=%v close=%d", err, fixture.database.closeCalls)
	}

	originalCatalog := prepared.catalogDigest
	prepared.catalogDigest = testDigest("prepared-catalog-drift")
	assertPreparedSessionDrift(t, prepared)
	prepared.catalogDigest = originalCatalog

	originalPlan := prepared.dispatch.planDigest
	prepared.dispatch.planDigest[0] ^= 0xff
	assertPreparedSessionDrift(t, prepared)
	prepared.dispatch.planDigest = originalPlan

	originalAction := prepared.dispatch.action
	prepared.dispatch.action = RecoveryBeginNextAttempt
	assertPreparedSessionDrift(t, prepared)
	prepared.dispatch.action = originalAction

	originalKey := prepared.key
	prepared.key++
	assertPreparedSessionDrift(t, prepared)
	prepared.key = originalKey

	originalDecisionDigest := fixture.evidence.active.ownedDecision.digest
	fixture.evidence.active.ownedDecision.digest = testDigest("prepared-decision-drift")
	assertPreparedSessionDrift(t, prepared)
	fixture.evidence.active.ownedDecision.digest = originalDecisionDigest

	originalState := fixture.evidence.snapshot.state
	fixture.evidence.snapshot.state = RecoveryDivergent
	assertPreparedSessionDrift(t, prepared)
	fixture.evidence.snapshot.state = originalState

	originalSession := prepared.session
	prepared.session = newRunnerPreflightSession()
	assertPreparedSessionDrift(t, prepared)
	prepared.session = originalSession

	if !validRunnerPreparedCurrentSession(prepared) {
		t.Fatal("restored prepared session did not recover its immutable binding")
	}
	if err := closeRunnerPreparedCurrentSession(prepared, nil); err != nil || fixture.database.unlockCalls != 1 || !reflect.DeepEqual(fixture.database.unlockKeys, []int64{fixture.key}) || fixture.database.closeCalls != 1 || liveRunnerPreparedCurrentSessions() != 0 {
		t.Fatalf("prepared close: err=%v database=%+v live=%d", err, fixture.database, liveRunnerPreparedCurrentSessions())
	}
	if validRunnerPreparedCurrentSession(prepared) || !IsCode(closeRunnerPreparedCurrentSession(prepared, nil), CodeTransactionBoundary) {
		t.Fatal("closed prepared session remained reusable")
	}
	if err := closeRunnerEvidenceOwnership(fixture.evidence, fixture.candidate); err != nil {
		t.Fatal(err)
	}
}

func TestPublicRunnerDispatchesOnlyExactBrandNewRecovery(t *testing.T) {
	for _, test := range []struct {
		name     string
		mutate   func(*RecoverySnapshot)
		wantCode ErrorCode
		wantOp   string
	}{
		{"brand-new", nil, CodeProjectionNotImplemented, "runner-statement-intent"},
		{"inherited-header-only", func(snapshot *RecoverySnapshot) {
			snapshot.state = RecoveryBrandNewInherited
		}, CodeProjectionNotImplemented, "runner-statement-intent"},
		{"wrong-action", func(snapshot *RecoverySnapshot) {
			snapshot.nextPermittedAction = RecoveryBeginNextAttempt
		}, CodeProjectionNotImplemented, "runner-recovery-dispatch"},
		{"dangling", func(snapshot *RecoverySnapshot) {
			snapshot.state = RecoveryDanglingStatementIntent
			snapshot.nextPermittedAction = RecoveryAppendAbortedRetryable
		}, CodeProjectionNotImplemented, "runner-recovery-dispatch"},
		{"brand-new-identity", func(snapshot *RecoverySnapshot) {
			migration := "000001"
			snapshot.migrationID = &migration
		}, CodeProjectionNotImplemented, "runner-recovery-dispatch"},
	} {
		t.Run(test.name, func(t *testing.T) {
			raw, decision := buildExactAdmissionRuntime(t)
			sink := &runnerEvidenceSinkFake{mutateSnapshot: test.mutate}
			database := newRunnerPreflightSession()
			factory := &runnerPreflightProjectorFactory{}
			factory.initialize()
			verifier := &sequenceTrustVerifier{fallback: decision, recoveryArtifact: runnerDecisionRecoveryArtifact(t, decision)}
			runner := Runner{Trust: verifier, Evidence: sink, Connector: &runnerPreflightConnector{session: database}, projectionFactory: factory}
			_, err := runner.Run(context.Background(), RunRequest{Artifact: &memoryArtifactSource{data: raw}, TargetDSN: "test-only"})
			var migrationErr *Error
			if !errors.As(err, &migrationErr) || migrationErr.Code != test.wantCode || migrationErr.Op != test.wantOp || migrationErr.Err != nil {
				t.Fatalf("recovery dispatch=%#v", migrationErr)
			}
			wantBegin := 0
			if test.wantOp == "runner-statement-intent" {
				wantBegin = 1
			}
			if sink.session == nil || sink.session.closeCalls != 1 || database.ledgerReadCalls != 2 || len(factory.preconditionPhases) != 1+wantBegin || database.beginCalls != wantBegin || database.transaction.rollbackCalls != wantBegin || database.transaction.executeCalls != 0 || database.transaction.execCalls != 0 || database.transaction.commitCalls != 0 || database.queryCalls != 0 || database.backend.ledgerInsertCalls != 0 || database.backend.executeCalls != 0 || database.backend.commitCalls != 0 || database.unlockCalls != 1 || database.closeCalls != 1 || liveRunnerPreparedCurrentSessions() != 0 || liveRunnerPreparedCurrentTransactions() != 0 {
				t.Fatalf("recovery dispatch crossed write or cleanup boundary: sink=%+v database=%+v transaction=%+v factory=%+v live=%d/%d", sink.session, database, database.transaction, factory, liveRunnerPreparedCurrentSessions(), liveRunnerPreparedCurrentTransactions())
			}
		})
	}
}

func TestRunnerPreparedCloseUsesRegistryKeyAfterHandleDrift(t *testing.T) {
	fixture := newRunnerPreparedCurrentSessionFixture(t)
	prepared, err := bindRunnerPreparedCurrentSession(
		fixture.database, fixture.evidence, fixture.key, fixture.candidate, fixture.snapshot,
		fixture.ledger, fixture.authority, fixture.precondition, fixture.bundle, fixture.plans,
	)
	if err != nil {
		t.Fatal(err)
	}
	prepared.key++
	err = closeRunnerPreparedCurrentSession(prepared, nil)
	if !IsCode(err, CodeTransactionBoundary) || fixture.database.closeCalls != 1 || !reflect.DeepEqual(fixture.database.unlockKeys, []int64{fixture.key}) || liveRunnerPreparedCurrentSessions() != 0 {
		t.Fatalf("drifted close did not use registry-owned cleanup: err=%v database=%+v live=%d", err, fixture.database, liveRunnerPreparedCurrentSessions())
	}
	if err := closeRunnerEvidenceOwnership(fixture.evidence, fixture.candidate); err != nil {
		t.Fatal(err)
	}
}

func TestRunnerBrandNewRecoveryDispatchRejectsEveryNonHeaderFact(t *testing.T) {
	fixture := newRunnerPreparedCurrentSessionFixture(t)
	baseline := fixture.snapshot
	if !runnerBrandNewRecoverySnapshot(baseline) {
		t.Fatal("fixture recovery snapshot is not exact brand-new")
	}
	for name, mutate := range map[string]func(*RecoverySnapshot){
		"segment":  func(value *RecoverySnapshot) { value.cursor.segmentIndex++ },
		"sequence": func(value *RecoverySnapshot) { value.cursor.nextSequence++ },
		"checkpoint": func(value *RecoverySnapshot) {
			value.cursor.latestCheckpointRecordDigest = digestPointer(testDigest("checkpoint"))
		},
		"migration": func(value *RecoverySnapshot) {
			migration := "000001"
			value.migrationID = &migration
		},
		"attempt": func(value *RecoverySnapshot) {
			attempt := uint32(1)
			value.attemptIndex = &attempt
		},
		"continuation": func(value *RecoverySnapshot) {
			value.lineageContinuation = &OwnedRecovered[LineageContinuationContext]{}
		},
		"intent": func(value *RecoverySnapshot) { value.lastStatementIntent = &OwnedRecovered[StatementIntent]{} },
		"intermediate": func(value *RecoverySnapshot) {
			value.lastIntermediateEvidence = &OwnedRecovered[StatementIntermediateEvidence]{}
		},
		"commit":     func(value *RecoverySnapshot) { value.commitIntent = &OwnedRecovered[CommitIntent]{} },
		"terminal":   func(value *RecoverySnapshot) { value.lastTerminal = &OwnedRecovered[AttemptTerminalState]{} },
		"resolution": func(value *RecoverySnapshot) { value.lastResolution = &OwnedRecovered[AmbiguousResolutionState]{} },
		"terminal-digest": func(value *RecoverySnapshot) {
			value.lastTerminalDigest = digestPointer(testDigest("terminal"))
		},
		"resolution-digest": func(value *RecoverySnapshot) {
			value.lastResolutionDigest = digestPointer(testDigest("resolution"))
		},
		"intent-record": func(value *RecoverySnapshot) {
			value.lastStatementIntentRecordDigest = digestPointer(testDigest("intent-record"))
		},
		"intermediate-record": func(value *RecoverySnapshot) {
			value.lastIntermediateEvidenceRecordDigest = digestPointer(testDigest("intermediate-record"))
		},
		"commit-record": func(value *RecoverySnapshot) {
			value.lastCommitIntentRecordDigest = digestPointer(testDigest("commit-record"))
		},
		"previous-terminal": func(value *RecoverySnapshot) {
			value.previousAttemptTerminalDigest = digestPointer(testDigest("previous-terminal"))
		},
		"intermediate-state": func(value *RecoverySnapshot) {
			value.lastIntermediateStateDigest = digestPointer(testDigest("intermediate-state"))
		},
		"action": func(value *RecoverySnapshot) { value.nextPermittedAction = RecoveryBeginNextAttempt },
		"state":  func(value *RecoverySnapshot) { value.state = RecoveryTerminal },
	} {
		t.Run(name, func(t *testing.T) {
			fault := cloneRecoverySnapshot(baseline)
			mutate(fault)
			if runnerBrandNewRecoverySnapshot(fault) {
				t.Fatal("non-header recovery fact entered brand-new dispatch")
			}
		})
	}
	if err := closeRunnerEvidenceOwnership(fixture.evidence, fixture.candidate); err != nil {
		t.Fatal(err)
	}
}

func TestRunnerPreparedSessionRejectsEvidenceDriftAfterDatabaseSandwich(t *testing.T) {
	raw, decision := buildExactAdmissionRuntime(t)
	sink := &runnerEvidenceSinkFake{}
	database := newRunnerPreflightSession()
	database.afterLedgerRead[2] = func() {
		if sink.session != nil {
			sink.session.snapshot.state = RecoveryDivergent
			sink.session.snapshot.nextPermittedAction = RecoveryReturnFailure
		}
	}
	factory := &runnerPreflightProjectorFactory{}
	factory.initialize()
	verifier := &sequenceTrustVerifier{fallback: decision, recoveryArtifact: runnerDecisionRecoveryArtifact(t, decision)}
	runner := Runner{Trust: verifier, Evidence: sink, Connector: &runnerPreflightConnector{session: database}, projectionFactory: factory}
	_, err := runner.Run(context.Background(), RunRequest{Artifact: &memoryArtifactSource{data: raw}, TargetDSN: "test-only"})
	var migrationErr *Error
	if !errors.As(err, &migrationErr) || migrationErr.Code != CodeEvidenceJournalFailed || migrationErr.Op != "runner-prepared-session" || migrationErr.Err != nil {
		t.Fatalf("evidence drift mapping=%#v", migrationErr)
	}
	if database.beginCalls != 0 || database.unlockCalls != 1 || database.closeCalls != 1 || sink.session == nil || sink.session.closeCalls != 1 || liveRunnerPreparedCurrentSessions() != 0 {
		t.Fatalf("evidence drift cleanup crossed boundary: database=%+v sink=%+v", database, sink.session)
	}
}

func TestRunnerPreparedBinderRevalidatesEveryClosedInput(t *testing.T) {
	for _, test := range []struct {
		name     string
		mutate   func(*runnerPreparedCurrentSessionFixture)
		wantCode ErrorCode
	}{
		{"lock-key", func(value *runnerPreparedCurrentSessionFixture) { value.key++ }, CodeUntrusted},
		{"ledger", func(value *runnerPreparedCurrentSessionFixture) {
			value.ledger.digest = testDigest("wrong-empty-ledger")
		}, CodeInvalidLedger},
		{"nil-ledger", func(value *runnerPreparedCurrentSessionFixture) { value.ledger.rows = nil }, CodeInvalidLedger},
		{"authority", func(value *runnerPreparedCurrentSessionFixture) {
			value.authority.Digest = testDigest("wrong-authority")
		}, CodeAuthorityDrift},
		{"precondition", func(value *runnerPreparedCurrentSessionFixture) {
			value.precondition.Digest = testDigest("wrong-precondition")
		}, CodeCatalogDrift},
		{"plans", func(value *runnerPreparedCurrentSessionFixture) { value.plans = append(value.plans, value.plans[0]) }, CodeUntrusted},
		{"recovery", func(value *runnerPreparedCurrentSessionFixture) {
			value.snapshot.state = RecoveryDanglingStatementIntent
			value.snapshot.nextPermittedAction = RecoveryAppendAbortedRetryable
			value.evidence.snapshot.state = RecoveryDanglingStatementIntent
			value.evidence.snapshot.nextPermittedAction = RecoveryAppendAbortedRetryable
		}, CodeProjectionNotImplemented},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newRunnerPreparedCurrentSessionFixture(t)
			cleanupKey := fixture.key
			test.mutate(&fixture)
			prepared, err := bindRunnerPreparedCurrentSession(
				fixture.database, fixture.evidence, fixture.key, fixture.candidate, fixture.snapshot,
				fixture.ledger, fixture.authority, fixture.precondition, fixture.bundle, fixture.plans,
			)
			if prepared != nil || !IsCode(err, test.wantCode) || liveRunnerPreparedCurrentSessions() != 0 {
				t.Fatalf("mutated input entered prepared authority: prepared=%+v err=%v live=%d", prepared, err, liveRunnerPreparedCurrentSessions())
			}
			if cleanupErr := closeRunnerDatabasePreflight(fixture.database, cleanupKey, true, nil); cleanupErr != nil {
				t.Fatal(cleanupErr)
			}
			if cleanupErr := closeRunnerEvidenceOwnership(fixture.evidence, fixture.candidate); cleanupErr != nil {
				t.Fatal(cleanupErr)
			}
		})
	}
}

func TestRunnerEntryPlanClosureRejectsDuplicateAndMutation(t *testing.T) {
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
	if err != nil {
		t.Fatal(err)
	}
	migrationID := bundle.Manifest.SchemaBundle.Migrations[0].ID
	digest, count, err := runnerEntryPlanClosureDigest(plans, migrationID)
	if err != nil || digest == ([32]byte{}) || count == 0 {
		t.Fatalf("valid plan closure: digest=%x count=%d err=%v", digest, count, err)
	}
	duplicate := append(append([]StatementPlan(nil), plans...), plans[0])
	if _, _, err := runnerEntryPlanClosureDigest(duplicate, migrationID); err == nil {
		t.Fatal("duplicate plan entered the prepared closure")
	}
	mutated := append([]StatementPlan(nil), plans...)
	mutated[0].sqlBytes = append([]byte(nil), plans[0].sqlBytes...)
	mutated[0].sqlBytes[0] ^= 0xff
	if _, _, err := runnerEntryPlanClosureDigest(mutated, migrationID); err == nil {
		t.Fatal("mutated statement bytes entered the prepared closure")
	}
}

func TestRunnerPreparedSessionHasOnlyReviewedProductionConsumers(t *testing.T) {
	paths, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	symbols := map[string]bool{
		"prepareCurrentDatabaseSession":              true,
		"runnerPreparedCurrentSession":               true,
		"runnerPreparedCurrentSessionBinding":        true,
		"runnerPreparedCurrentSessionRegistryRecord": true,
		"bindRunnerPreparedCurrentSession":           true,
		"validRunnerPreparedCurrentSession":          true,
		"closeRunnerPreparedCurrentSession":          true,
		"runnerPreparedCurrentSessionRegistry":       true,
	}
	allowed := map[string]map[string]bool{
		"runner_prepared_session.go": nil,
		"runner_database_preflight.go": {
			"prepareCurrentDatabaseSession": true, "runnerPreparedCurrentSession": true, "bindRunnerPreparedCurrentSession": true,
		},
		"runner.go": {"prepareCurrentDatabaseSession": true, "closeRunnerPreparedCurrentSession": true},
		"runner_transaction_preflight.go": {
			"runnerPreparedCurrentSession": true, "runnerPreparedCurrentSessionBinding": true,
			"runnerPreparedCurrentSessionRegistryRecord": true, "validRunnerPreparedCurrentSession": true,
			"closeRunnerPreparedCurrentSession": true, "runnerPreparedCurrentSessionRegistry": true,
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
			if !ok || !symbols[identifier.Name] || name == "runner_prepared_session.go" || allowed[name][identifier.Name] {
				return true
			}
			t.Fatalf("prepared session authority %s acquired unreviewed production consumer %s", identifier.Name, name)
			return false
		})
	}
}

func TestProductionRunCannotConsumePreparedSessionForWrites(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "runner.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	forbidden := map[string]bool{
		"session": true, "BeginMigration": true, "ExecuteStatement": true, "AppendDurable": true,
		"ReserveAndActivateSuccessor": true, "Insert": true, "Commit": true,
	}
	found := false
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Name.Name != "Run" || function.Recv == nil {
			continue
		}
		found = true
		ast.Inspect(function.Body, func(node ast.Node) bool {
			selector, ok := node.(*ast.SelectorExpr)
			if ok && forbidden[selector.Sel.Name] {
				t.Fatalf("production Run consumed prepared authority through forbidden %s edge", selector.Sel.Name)
			}
			return true
		})
	}
	if !found {
		t.Fatal("production Runner.Run was not found")
	}
}

type runnerPreparedCurrentSessionFixture struct {
	database     *runnerPreflightSession
	evidence     *runnerEvidenceSessionFake
	candidate    OwnedCurrentCandidate
	snapshot     *RecoverySnapshot
	ledger       runnerLedgerPrefix
	authority    ProjectionResult[AuthorityProjection]
	precondition ProjectionResult[CatalogStateProjection]
	bundle       *RuntimeBundle
	plans        []StatementPlan
	key          int64
}

func newRunnerPreparedCurrentSessionFixture(t *testing.T) runnerPreparedCurrentSessionFixture {
	t.Helper()
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
	if err != nil {
		t.Fatal(err)
	}
	verifier := &sequenceTrustVerifier{fallback: decision, recoveryArtifact: runnerDecisionRecoveryArtifact(t, decision)}
	current, recovery, err := bindVerifierOwnedDecision(verifier, decision, bindings.runnerProjectionDecisionDigest, verifier.recoveryArtifact)
	if err != nil {
		t.Fatal(err)
	}
	_, _, candidate, err := bindVerifiedEvidenceRun(decision, bindings, current, raw, recovery)
	if err != nil {
		t.Fatal(err)
	}
	evidence := newRunnerEvidenceSessionFake(candidate)
	database := newRunnerPreflightSession()
	database.roleConfigured, database.locked = true, true
	database.executionPolicy = bundle.Manifest.ExecutionPolicy
	metadata := runnerPreflightSnapshotMetadata(AuthorityPhaseMigrationRole)
	authority := runnerPreflightProjectionResult(t, metadata, bindings.verifiedAuthority, AuthorityPhaseMigrationRole)
	factory := &runnerPreflightProjectorFactory{}
	factory.initialize()
	snapshot := &runnerPreflightSnapshot{session: database, phase: AuthorityPhaseMigrationRole, metadata: metadata}
	precondition, err := (&runnerPreflightProjector{factory: factory, snapshot: snapshot}).ProjectPrecondition(context.Background(), snapshot, bindings.initialSchemaScope, bindings.initialSchemaScope.BoundPrecondition())
	if err != nil {
		t.Fatal(err)
	}
	ledgerDigest, err := LedgerPrefixDigest([]CommitIntentLedgerRow{})
	if err != nil {
		t.Fatal(err)
	}
	key, err := bundle.Manifest.SchemaBundle.AdvisoryLock.Key()
	if err != nil {
		t.Fatal(err)
	}
	return runnerPreparedCurrentSessionFixture{
		database: database, evidence: evidence, candidate: candidate, snapshot: cloneRecoverySnapshot(evidence.snapshot),
		ledger: runnerLedgerPrefix{rows: []CommitIntentLedgerRow{}, digest: ledgerDigest}, authority: authority, precondition: precondition,
		bundle: bundle, plans: plans, key: key,
	}
}

func assertPreparedSessionDrift(t *testing.T, prepared *runnerPreparedCurrentSession) {
	t.Helper()
	if validRunnerPreparedCurrentSession(prepared) {
		t.Fatal("mutated prepared session remained valid")
	}
}

func liveRunnerPreparedCurrentSessions() int {
	count := 0
	runnerPreparedCurrentSessionRegistry.Range(func(_, _ any) bool {
		count++
		return true
	})
	return count
}
