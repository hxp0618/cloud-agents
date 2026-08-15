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

func TestRunnerDurableCurrentStatementIntentConsumesPreparedAuthorityExactlyOnce(t *testing.T) {
	fixture, prepared, runner := newRunnerPreparedCurrentStatementIntentFixture(t)
	durable, err := runner.appendCurrentStatementIntent(context.Background(), prepared)
	if err != nil || !validRunnerDurableCurrentStatementIntent(durable) {
		t.Fatalf("append statement intent: durable=%+v err=%v", durable, err)
	}
	if validRunnerPreparedCurrentStatement(prepared) || liveRunnerPreparedCurrentStatements() != 0 || liveRunnerDurableCurrentStatementIntents() != 1 {
		t.Fatalf("prepared authority was not atomically consumed: prepared=%t live=%d/%d", validRunnerPreparedCurrentStatement(prepared), liveRunnerPreparedCurrentStatements(), liveRunnerDurableCurrentStatementIntents())
	}
	journal := fixture.evidence.journal
	if fixture.evidence.bindCalls != 1 || journal.appendCalls != 1 || journal.appendedRecord.StatementIntent == nil || journal.appendedRecord.StatementIntent.MigrationID != fixture.plans[0].MigrationID || journal.appendedRecord.StatementIntent.StatementIndex != 0 {
		t.Fatalf("exact intent was not appended once: bind=%d append=%d record=%+v", fixture.evidence.bindCalls, journal.appendCalls, journal.appendedRecord)
	}
	if durable.intentRecordDigest != journal.snapshot.tailDigest || durable.checkpointDigest != journal.cursor.lineageIndexPreviousRecordDigest || !sameCursorIdentity(durable.cursor, journal.cursor) || durable.recoveryDigest != generationJournalRecoveryDigest(journal.snapshot) {
		t.Fatalf("durable cursor/recovery binding differs: durable=%+v cursor=%+v snapshot=%+v", durable, journal.cursor, journal.snapshot)
	}
	if journal.snapshot.state != RecoveryDanglingStatementIntent || journal.snapshot.nextPermittedAction != RecoveryAppendAbortedRetryable || journal.snapshot.lastStatementIntent == nil || !canonicalEqual(journal.snapshot.lastStatementIntent.value, durable.intent) {
		t.Fatalf("durable recovery state is not the exact dangling intent: %+v", journal.snapshot)
	}
	if fixture.database.transaction.executeCalls != 0 || fixture.database.transaction.execCalls != 0 || fixture.database.transaction.commitCalls != 0 || fixture.database.transaction.rollbackCalls != 0 {
		t.Fatalf("durable append crossed SQL/ledger/transaction boundary: %+v", fixture.database.transaction)
	}
	if replay, replayErr := runner.appendCurrentStatementIntent(context.Background(), prepared); replay != nil || !IsCode(replayErr, CodeTransactionBoundary) || !validRunnerDurableCurrentStatementIntent(durable) || journal.appendCalls != 1 {
		t.Fatalf("consumed prepared authority replayed or damaged durable successor: replay=%+v err=%v append=%d", replay, replayErr, journal.appendCalls)
	}
	if err := closeRunnerDurableCurrentStatementIntent(durable, nil); err != nil || fixture.database.transaction.rollbackCalls != 1 || fixture.database.transaction.status != 'I' || fixture.database.unlockCalls != 1 || fixture.database.closeCalls != 1 || liveRunnerDurableCurrentStatementIntents() != 0 {
		t.Fatalf("durable close did not release database ownership: err=%v transaction=%+v database=%+v", err, fixture.database.transaction, fixture.database)
	}
	if err := closeRunnerEvidenceOwnership(fixture.evidence, fixture.candidate); err != nil || journal.closeCalls != 1 {
		t.Fatalf("evidence close: err=%v journal=%+v", err, journal)
	}
}

func TestRunnerDurableCurrentStatementIntentRejectsLiteralCopyAndFieldDrift(t *testing.T) {
	fixture, prepared, runner := newRunnerPreparedCurrentStatementIntentFixture(t)
	durable, err := runner.appendCurrentStatementIntent(context.Background(), prepared)
	if err != nil || !validRunnerDurableCurrentStatementIntent(durable) {
		t.Fatalf("append statement intent: durable=%+v err=%v", durable, err)
	}
	valueType := reflect.TypeOf(*durable)
	for index := 0; index < valueType.NumField(); index++ {
		if valueType.Field(index).PkgPath == "" {
			t.Fatalf("durable statement intent field %s became public", valueType.Field(index).Name)
		}
	}
	copyValue := *durable
	if err := closeRunnerDurableCurrentStatementIntent(&copyValue, nil); !IsCode(err, CodeTransactionBoundary) || fixture.database.transaction.rollbackCalls != 0 || !validRunnerDurableCurrentStatementIntent(durable) {
		t.Fatalf("copy changed original authority: err=%v transaction=%+v", err, fixture.database.transaction)
	}
	if err := closeRunnerDurableCurrentStatementIntent(&runnerDurableCurrentStatementIntent{}, nil); !IsCode(err, CodeTransactionBoundary) || fixture.database.transaction.rollbackCalls != 0 {
		t.Fatalf("literal escaped: err=%v", err)
	}

	originalRecord := durable.intentRecordDigest
	durable.intentRecordDigest = testDigest("drifted-intent-record")
	assertDurableStatementIntentDrift(t, durable)
	durable.intentRecordDigest = originalRecord

	originalPlan := durable.plan.sqlBytes[0]
	durable.plan.sqlBytes[0] ^= 0xff
	assertDurableStatementIntentDrift(t, durable)
	durable.plan.sqlBytes[0] = originalPlan

	originalMetadata := durable.intent.AuthorityBeforeResult.Metadata.Snapshot.DatabaseName
	durable.intent.AuthorityBeforeResult.Metadata.Snapshot.DatabaseName = "drifted_database"
	assertDurableStatementIntentDrift(t, durable)
	durable.intent.AuthorityBeforeResult.Metadata.Snapshot.DatabaseName = originalMetadata

	originalCursor := durable.cursor.nextSequence
	durable.cursor.nextSequence++
	assertDurableStatementIntentDrift(t, durable)
	durable.cursor.nextSequence = originalCursor

	originalState := fixture.evidence.snapshot.state
	fixture.evidence.snapshot.state = RecoveryDivergent
	assertDurableStatementIntentDrift(t, durable)
	fixture.evidence.snapshot.state = originalState

	if !validRunnerDurableCurrentStatementIntent(durable) {
		t.Fatal("restored durable authority did not recover its immutable binding")
	}
	originalKey := durable.key
	originalTransaction := durable.transaction
	rogueTransaction := newRunnerPreflightTransaction(fixture.database)
	rogueTransaction.active = true
	rogueTransaction.status = 'T'
	durable.key++
	durable.transaction = rogueTransaction
	err = closeRunnerDurableCurrentStatementIntent(durable, nil)
	if !IsCode(err, CodeTransactionBoundary) || originalTransaction != fixture.database.transaction || fixture.database.transaction.rollbackCalls != 1 || rogueTransaction.rollbackCalls != 0 || !reflect.DeepEqual(fixture.database.unlockKeys, []int64{originalKey}) || fixture.database.closeCalls != 1 || liveRunnerDurableCurrentStatementIntents() != 0 {
		t.Fatalf("drifted close did not use registry ownership: err=%v transaction=%+v database=%+v", err, fixture.database.transaction, fixture.database)
	}
	if err := closeRunnerEvidenceOwnership(fixture.evidence, fixture.candidate); err != nil {
		t.Fatal(err)
	}
}

func TestRunnerStatementIntentAppendFaultsFailClosedBeforeSQLAndReleaseDatabase(t *testing.T) {
	raw, decision := buildExactAdmissionRuntime(t)
	postMutationRevoked := map[string]bool{
		"append-unknown": true, "append-unknown-error": true, "durable-with-error": true,
		"result-sequence": true, "result-record": true, "result-checkpoint": true, "result-rotation": true, "result-cursor": true,
		"recovery-state": true, "recovery-action": true, "recovery-record": true, "recovery-body": true, "recovery-digest": true,
	}
	tests := []struct {
		name       string
		configure  func(*runnerPreparedCurrentSessionFixture)
		wantCode   ErrorCode
		wantOp     string
		wantAppend int
	}{
		{"bind-error", func(f *runnerPreparedCurrentSessionFixture) { f.evidence.bindErr = errors.New("secret-bind") }, CodeEvidenceJournalFailed, "runner-statement-intent-bind", 0},
		{"bind-stable", func(f *runnerPreparedCurrentSessionFixture) {
			f.evidence.bindErr = fail(CodeEvidenceRecoveryRequired, "fake", "secret", errors.New("secret-bind"))
		}, CodeEvidenceRecoveryRequired, "runner-statement-intent-bind", 0},
		{"bind-canceled", func(f *runnerPreparedCurrentSessionFixture) { f.evidence.bindErr = context.Canceled }, CodeContextCanceled, "runner-statement-intent-bind", 0},
		{"bind-deadline", func(f *runnerPreparedCurrentSessionFixture) { f.evidence.bindErr = context.DeadlineExceeded }, CodeDeadlineExceeded, "runner-statement-intent-bind", 0},
		{"bind-missing-journal", func(f *runnerPreparedCurrentSessionFixture) { f.evidence.bindNoJournal = true }, CodeEvidenceRecoveryRequired, "runner-statement-intent-bind", 0},
		{"bind-missing-record", func(f *runnerPreparedCurrentSessionFixture) { f.evidence.bindNoRecord = true }, CodeEvidenceRecoveryRequired, "runner-statement-intent-bind", 0},
		{"bind-invalid-record", func(f *runnerPreparedCurrentSessionFixture) {
			f.evidence.mutateBoundIntent = func(intent *StatementIntent) { intent.StatementIndex++ }
		}, CodeEvidenceJournalFailed, "runner-statement-intent-bind", 0},
		{"bind-cursor-swap", func(f *runnerPreparedCurrentSessionFixture) {
			f.evidence.mutateBoundAuthority = func(cursor *JournalCursor, _ *OwnedEvidenceRecord) { cursor.nextSequence++ }
		}, CodeEvidenceJournalFailed, "runner-statement-intent-bind", 0},
		{"bind-consumed-record", func(f *runnerPreparedCurrentSessionFixture) {
			f.evidence.mutateBoundAuthority = func(_ *JournalCursor, owned *OwnedEvidenceRecord) { owned.consumed.Store(true) }
		}, CodeEvidenceJournalFailed, "runner-statement-intent-bind", 0},
		{"append-error", func(f *runnerPreparedCurrentSessionFixture) {
			f.evidence.journal.appendErr = errors.New("secret-append")
		}, CodeEvidenceJournalFailed, "runner-statement-intent-append", 1},
		{"append-canceled", func(f *runnerPreparedCurrentSessionFixture) { f.evidence.journal.appendErr = context.Canceled }, CodeContextCanceled, "runner-statement-intent-append", 1},
		{"append-deadline", func(f *runnerPreparedCurrentSessionFixture) { f.evidence.journal.appendErr = context.DeadlineExceeded }, CodeDeadlineExceeded, "runner-statement-intent-append", 1},
		{"append-unknown", func(f *runnerPreparedCurrentSessionFixture) { f.evidence.journal.appendOutcome = appendOutcomeUnknown }, CodeEvidenceJournalFailed, "runner-statement-intent-append", 1},
		{"append-unknown-error", func(f *runnerPreparedCurrentSessionFixture) {
			f.evidence.journal.appendOutcome = appendOutcomeUnknown
			f.evidence.journal.appendErr = fail(CodeEvidenceJournalFailed, "fake", "secret", errors.New("secret-unknown"))
			f.evidence.journal.appendValuesWithError = true
		}, CodeEvidenceJournalFailed, "runner-statement-intent-append", 1},
		{"durable-with-error", func(f *runnerPreparedCurrentSessionFixture) {
			f.evidence.journal.appendErr = errors.New("secret-durable-error")
			f.evidence.journal.appendValuesWithError = true
		}, CodeEvidenceJournalFailed, "runner-statement-intent-append", 1},
		{"result-sequence", func(f *runnerPreparedCurrentSessionFixture) {
			f.evidence.journal.mutateAppendResult = func(result *AppendResult) { result.candidateSequence++ }
		}, CodeEvidenceJournalFailed, "runner-statement-intent-append", 1},
		{"result-record", func(f *runnerPreparedCurrentSessionFixture) {
			f.evidence.journal.mutateAppendResult = func(result *AppendResult) { result.candidateRecordDigest = testDigest("other-intent") }
		}, CodeEvidenceJournalFailed, "runner-statement-intent-append", 1},
		{"result-checkpoint", func(f *runnerPreparedCurrentSessionFixture) {
			f.evidence.journal.mutateAppendResult = func(result *AppendResult) { result.candidateCheckpointRecordDigest = Digest("invalid") }
		}, CodeEvidenceJournalFailed, "runner-statement-intent-append", 1},
		{"result-rotation", func(f *runnerPreparedCurrentSessionFixture) {
			f.evidence.journal.mutateAppendResult = func(result *AppendResult) {
				result.rotationHeaderRecordDigest = digestPointer(testDigest("rotation"))
				result.rotationHeaderCheckpointRecordDigest = digestPointer(testDigest("rotation-checkpoint"))
			}
		}, CodeEvidenceJournalFailed, "runner-statement-intent-append", 1},
		{"result-cursor", func(f *runnerPreparedCurrentSessionFixture) {
			f.evidence.journal.mutateAppendResult = func(result *AppendResult) { result.durableCursor.nextSequence++ }
		}, CodeEvidenceJournalFailed, "runner-statement-intent-append", 1},
		{"recovery-state", func(f *runnerPreparedCurrentSessionFixture) {
			f.evidence.journal.mutateAppendSnapshot = func(snapshot *RecoverySnapshot) { snapshot.state = RecoveryDivergent }
		}, CodeEvidenceJournalFailed, "runner-statement-intent-evidence", 1},
		{"recovery-action", func(f *runnerPreparedCurrentSessionFixture) {
			f.evidence.journal.mutateAppendSnapshot = func(snapshot *RecoverySnapshot) { snapshot.nextPermittedAction = RecoveryReturnFailure }
		}, CodeEvidenceJournalFailed, "runner-statement-intent-evidence", 1},
		{"recovery-record", func(f *runnerPreparedCurrentSessionFixture) {
			f.evidence.journal.mutateAppendSnapshot = func(snapshot *RecoverySnapshot) { snapshot.lastStatementIntent = nil }
		}, CodeEvidenceJournalFailed, "runner-statement-intent-evidence", 1},
		{"recovery-body", func(f *runnerPreparedCurrentSessionFixture) {
			f.evidence.journal.mutateAppendSnapshot = func(snapshot *RecoverySnapshot) { snapshot.lastStatementIntent.value.StatementIndex++ }
		}, CodeEvidenceJournalFailed, "runner-statement-intent-evidence", 1},
		{"recovery-digest", func(f *runnerPreparedCurrentSessionFixture) {
			f.evidence.journal.mutateAppendSnapshot = func(snapshot *RecoverySnapshot) {
				snapshot.lastStatementIntentRecordDigest = digestPointer(testDigest("other-record"))
			}
		}, CodeEvidenceJournalFailed, "runner-statement-intent-evidence", 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture, prepared, runner := newRunnerPreparedCurrentStatementIntentFixtureFromRuntime(t, raw, decision)
			test.configure(&fixture)
			durable, err := runner.appendCurrentStatementIntent(context.Background(), prepared)
			if durable != nil {
				t.Fatalf("fault minted durable authority: %+v", durable)
			}
			assertRunnerStatementIntentError(t, err, test.wantCode, test.wantOp)
			if containsErrorText(err, "secret-") {
				t.Fatalf("raw cause leaked: %v", err)
			}
			if postMutationRevoked[test.name] && fixture.evidence.journal.cursor.Valid() {
				t.Fatal("post-mutation fault left the durable evidence cursor active")
			}
			if fixture.evidence.bindCalls != 1 || fixture.evidence.journal.appendCalls != test.wantAppend || fixture.database.transaction.rollbackCalls != 1 || fixture.database.transaction.executeCalls != 0 || fixture.database.transaction.execCalls != 0 || fixture.database.transaction.commitCalls != 0 || fixture.database.unlockCalls != 1 || fixture.database.closeCalls != 1 || liveRunnerPreparedCurrentStatements() != 0 || liveRunnerDurableCurrentStatementIntents() != 0 {
				t.Fatalf("fault escaped cleanup or mutation boundary: evidence=%+v journal=%+v transaction=%+v database=%+v", fixture.evidence, fixture.evidence.journal, fixture.database.transaction, fixture.database)
			}
			if cleanupErr := closeRunnerEvidenceOwnership(fixture.evidence, fixture.candidate); cleanupErr != nil {
				t.Fatal(cleanupErr)
			}
		})
	}
}

func TestRunnerStatementIntentCleanupFailuresDominateAndAttemptEveryRelease(t *testing.T) {
	raw, decision := buildExactAdmissionRuntime(t)
	for _, test := range []struct {
		name      string
		configure func(*runnerPreparedCurrentSessionFixture)
		wantOp    string
	}{
		{"rollback", func(f *runnerPreparedCurrentSessionFixture) {
			f.evidence.journal.appendErr = errors.New("secret-append")
			f.database.transaction.rollbackErr = errors.New("secret-rollback")
		}, "runner-transaction-rollback"},
		{"rollback-status", func(f *runnerPreparedCurrentSessionFixture) {
			f.evidence.journal.appendErr = errors.New("secret-append")
			f.database.transaction.rollbackLeavesOpen = true
		}, "runner-transaction-rollback"},
		{"close-dominates", func(f *runnerPreparedCurrentSessionFixture) {
			f.evidence.journal.appendErr = errors.New("secret-append")
			f.database.transaction.rollbackErr = errors.New("secret-rollback")
			f.database.transaction.rollbackLeavesOpen = true
			f.database.unlockErr = errors.New("secret-unlock")
			f.database.closeErr = errors.New("secret-close")
		}, "runner-database-close"},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture, prepared, runner := newRunnerPreparedCurrentStatementIntentFixtureFromRuntime(t, raw, decision)
			test.configure(&fixture)
			durable, err := runner.appendCurrentStatementIntent(context.Background(), prepared)
			if durable != nil {
				t.Fatalf("cleanup fault minted durable authority: %+v", durable)
			}
			assertRunnerStatementIntentError(t, err, CodeTransactionBoundary, test.wantOp)
			if containsErrorText(err, "secret-") || fixture.database.transaction.rollbackCalls != 1 || fixture.database.unlockCalls != 1 || fixture.database.closeCalls != 1 || liveRunnerDurableCurrentStatementIntents() != 0 {
				t.Fatalf("cleanup precedence/attempts: err=%v transaction=%+v database=%+v", err, fixture.database.transaction, fixture.database)
			}
			if cleanupErr := closeRunnerEvidenceOwnership(fixture.evidence, fixture.candidate); cleanupErr != nil {
				t.Fatal(cleanupErr)
			}
		})
	}
}

func TestPublicRunnerStatementIntentFaultsCloseEvidenceAndDatabase(t *testing.T) {
	raw, decision := buildExactAdmissionRuntime(t)
	recoveryArtifact := runnerDecisionRecoveryArtifact(t, decision)
	for _, test := range []struct {
		name       string
		configure  func(*runnerEvidenceSessionFake)
		wantCode   ErrorCode
		wantOp     string
		wantAppend int
	}{
		{"bind", func(session *runnerEvidenceSessionFake) { session.bindErr = errors.New("secret-bind") }, CodeEvidenceJournalFailed, "runner-statement-intent-bind", 0},
		{"append", func(session *runnerEvidenceSessionFake) { session.journal.appendErr = errors.New("secret-append") }, CodeEvidenceJournalFailed, "runner-statement-intent-append", 1},
		{"unknown", func(session *runnerEvidenceSessionFake) { session.journal.appendOutcome = appendOutcomeUnknown }, CodeEvidenceJournalFailed, "runner-statement-intent-append", 1},
		{"recovery", func(session *runnerEvidenceSessionFake) {
			session.journal.mutateAppendSnapshot = func(snapshot *RecoverySnapshot) { snapshot.state = RecoveryDivergent }
		}, CodeEvidenceJournalFailed, "runner-statement-intent-evidence", 1},
		{"evidence-close-dominates", func(session *runnerEvidenceSessionFake) {
			session.journal.appendErr = errors.New("secret-append")
			session.closeErr = errors.New("secret-close")
		}, CodeEvidenceJournalFailed, "runner-evidence-close", 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			database := newRunnerPreflightSession()
			factory := &runnerPreflightProjectorFactory{}
			factory.initialize()
			sink := &runnerEvidenceSinkFake{configureSession: test.configure}
			verifier := &sequenceTrustVerifier{fallback: decision, recoveryArtifact: append([]byte(nil), recoveryArtifact...)}
			runner := Runner{Trust: verifier, Evidence: sink, Connector: &runnerPreflightConnector{session: database}, projectionFactory: factory}
			before := liveVerifiedEvidenceRunBindings()
			_, err := runner.Run(context.Background(), RunRequest{Artifact: &memoryArtifactSource{data: raw}, TargetDSN: "test-only"})
			assertRunnerStatementIntentError(t, err, test.wantCode, test.wantOp)
			if containsErrorText(err, "secret-") || sink.session == nil || sink.session.bindCalls != 1 || sink.session.journal.appendCalls != test.wantAppend || sink.session.closeCalls != 1 || sink.session.journal.closeCalls != 1 || database.transaction.rollbackCalls != 1 || database.transaction.executeCalls != 0 || database.transaction.execCalls != 0 || database.transaction.commitCalls != 0 || database.unlockCalls != 1 || database.closeCalls != 1 || liveVerifiedEvidenceRunBindings() != before || liveRunnerPreparedCurrentStatements() != 0 || liveRunnerDurableCurrentStatementIntents() != 0 {
				t.Fatalf("public append fault escaped cleanup: err=%v evidence=%+v database=%+v transaction=%+v live=%d/%d", err, sink.session, database, database.transaction, liveVerifiedEvidenceRunBindings(), before)
			}
		})
	}
}

func TestRunnerStatementIntentAppendHasSingleEvidenceMutationAndNoSQLLedgerOrCommitEdge(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "runner_statement_intent.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	appendCalls := 0
	forbidden := map[string]bool{
		"ExecuteStatement": true, "BeginMigration": true, "ReserveAndActivateSuccessor": true,
		"Insert": true, "Commit": true, "Exec": true, "Query": true, "QueryRow": true,
		"AppendExistingSegmentComposite": true, "AppendRotatedSegmentComposite": true,
	}
	ast.Inspect(file, func(node ast.Node) bool {
		selector, ok := node.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if selector.Sel.Name == "AppendDurable" {
			appendCalls++
		}
		if forbidden[selector.Sel.Name] {
			t.Fatalf("statement intent append acquired forbidden %s call edge", selector.Sel.Name)
		}
		return true
	})
	if appendCalls != 1 {
		t.Fatalf("statement intent append call edges=%d want=1", appendCalls)
	}
}

func TestRunnerDurableStatementIntentHasOnlyReviewedProductionConsumers(t *testing.T) {
	paths, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	symbols := map[string]bool{
		"runnerDurableCurrentStatementIntent": true, "runnerDurableCurrentStatementIntentBinding": true,
		"runnerDurableCurrentStatementIntentRegistryRecord": true, "runnerDurableCurrentStatementIntentRegistry": true,
		"runnerDurableCurrentStatementIntentSeed": true, "appendCurrentStatementIntent": true,
		"consumeRunnerPreparedCurrentStatement": true, "bindRunnerDurableCurrentStatementIntent": true,
		"validRunnerDurableCurrentStatementIntent": true, "closeRunnerDurableCurrentStatementIntent": true,
	}
	allowed := map[string]map[string]bool{
		"runner_statement_intent.go": nil,
		"runner.go":                  {"appendCurrentStatementIntent": true, "closeRunnerDurableCurrentStatementIntent": true},
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
			if !ok || !symbols[identifier.Name] || name == "runner_statement_intent.go" || allowed[name][identifier.Name] {
				return true
			}
			t.Fatalf("durable statement intent authority %s acquired unreviewed production consumer %s", identifier.Name, name)
			return false
		})
	}
}

func newRunnerPreparedCurrentStatementIntentFixture(t *testing.T) (runnerPreparedCurrentSessionFixture, *runnerPreparedCurrentStatement, *Runner) {
	t.Helper()
	fixture, transaction, factory := newRunnerPreparedCurrentTransactionFixture(t)
	return newRunnerPreparedCurrentStatementIntentFixtureFromTransaction(t, fixture, transaction, factory)
}

func newRunnerPreparedCurrentStatementIntentFixtureFromRuntime(t *testing.T, raw []byte, decision VerifiedTrustDecision) (runnerPreparedCurrentSessionFixture, *runnerPreparedCurrentStatement, *Runner) {
	t.Helper()
	fixture, transaction, factory := newRunnerPreparedCurrentTransactionFixtureFromRuntime(t, raw, decision)
	return newRunnerPreparedCurrentStatementIntentFixtureFromTransaction(t, fixture, transaction, factory)
}

func newRunnerPreparedCurrentStatementIntentFixtureFromTransaction(t *testing.T, fixture runnerPreparedCurrentSessionFixture, transaction *runnerPreparedCurrentTransaction, factory *runnerPreflightProjectorFactory) (runnerPreparedCurrentSessionFixture, *runnerPreparedCurrentStatement, *Runner) {
	t.Helper()
	runner := &Runner{projectionFactory: factory}
	prepared, err := runner.prepareCurrentStatement(context.Background(), transaction, fixture.bundle, fixture.plans)
	if err != nil || !validRunnerPreparedCurrentStatement(prepared) {
		t.Fatalf("statement intent fixture: prepared=%+v err=%v", prepared, err)
	}
	return fixture, prepared, runner
}

func assertDurableStatementIntentDrift(t *testing.T, durable *runnerDurableCurrentStatementIntent) {
	t.Helper()
	if validRunnerDurableCurrentStatementIntent(durable) {
		t.Fatal("mutated durable statement intent authority remained valid")
	}
}

func liveRunnerDurableCurrentStatementIntents() int {
	count := 0
	runnerDurableCurrentStatementIntentRegistry.Range(func(_, _ any) bool {
		count++
		return true
	})
	return count
}

func assertRunnerStatementIntentError(t *testing.T, err error, code ErrorCode, op string) {
	t.Helper()
	var migrationErr *Error
	if !errors.As(err, &migrationErr) || migrationErr.Code != code || migrationErr.Op != op || migrationErr.Err != nil {
		t.Fatalf("statement intent error: got=%#v want=%s/%s", migrationErr, code, op)
	}
}
