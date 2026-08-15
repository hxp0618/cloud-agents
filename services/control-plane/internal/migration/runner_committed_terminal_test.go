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

func TestRunnerCommittedTerminalAppendsExactlyOnceAndClosesRecoveryState(t *testing.T) {
	for _, test := range []struct {
		name           string
		bundleComplete bool
		wantState      RecoveryState
		wantAction     RecoveryAction
	}{
		{"bundle-complete", true, RecoveryCompleted, RecoveryReturnSuccess},
		{"next-entry", false, RecoveryTerminal, RecoveryBeginFirstAttemptNextEntry},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture, closed, runner := newRunnerCommittedTerminalFixture(t)
			fixture.evidence.journal.bundleComplete = test.bundleComplete
			durable, err := runner.appendCommittedTerminal(context.Background(), closed)
			if err != nil || !validRunnerDurableCommittedTerminal(durable) {
				t.Fatalf("append committed terminal: durable=%+v err=%v", durable, err)
			}
			if validRunnerClosedCurrentCommit(closed) || liveRunnerClosedCurrentCommits() != 0 || liveRunnerDurableCommittedTerminals() != 1 || fixture.evidence.terminalBindCalls != 1 || fixture.evidence.journal.appendCalls != 4 || fixture.evidence.journal.appendedRecord.AttemptTerminal == nil || fixture.evidence.journal.appendedRecord.AttemptTerminal.Outcome != "committed" {
				t.Fatalf("committed terminal authority did not advance exactly once: closed=%t live=%d/%d evidence=%+v", validRunnerClosedCurrentCommit(closed), liveRunnerClosedCurrentCommits(), liveRunnerDurableCommittedTerminals(), fixture.evidence)
			}
			if durable.bundleComplete != test.bundleComplete || durable.nextAction != test.wantAction || fixture.evidence.snapshot.state != test.wantState || fixture.evidence.snapshot.nextPermittedAction != test.wantAction || fixture.evidence.snapshot.lastTerminal == nil || fixture.evidence.snapshot.lastTerminalDigest == nil || *fixture.evidence.snapshot.lastTerminalDigest != durable.terminal.TerminalDigest || !canonicalEqual(fixture.evidence.snapshot.lastTerminal.value, durable.terminal) {
				t.Fatalf("committed recovery state differs: durable=%+v snapshot=%+v", durable, fixture.evidence.snapshot)
			}
			transaction := fixture.database.transaction
			if transaction.commitCalls != 1 || transaction.rollbackCalls != 0 || fixture.database.unlockCalls != 0 || fixture.database.closeCalls != 1 || fixture.database.backend.commitCalls != 1 || fixture.evidence.journal.cursor.nextSequence != 5 {
				t.Fatalf("terminal append crossed database boundary: transaction=%+v database=%+v", transaction, fixture.database)
			}
			if replay, replayErr := runner.appendCommittedTerminal(context.Background(), closed); replay != nil || !IsCode(replayErr, CodeEvidenceRecoveryRequired) || fixture.evidence.journal.appendCalls != 4 || !validRunnerDurableCommittedTerminal(durable) {
				t.Fatalf("consumed outcome replayed terminal or damaged successor: replay=%+v err=%v", replay, replayErr)
			}
			if err := closeRunnerDurableCommittedTerminal(durable, nil); err != nil || liveRunnerDurableCommittedTerminals() != 0 {
				t.Fatalf("close durable terminal: err=%v live=%d", err, liveRunnerDurableCommittedTerminals())
			}
			if err := closeRunnerEvidenceOwnership(fixture.evidence, fixture.candidate); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestRunnerCommittedTerminalRejectsUnavailableContextBeforeBinding(t *testing.T) {
	for _, test := range []struct {
		name      string
		ctx       func() context.Context
		nilRunner bool
		wantCode  ErrorCode
		wantOp    string
	}{
		{"nil-context", func() context.Context { return nil }, false, CodeEvidenceJournalFailed, "runner-committed-terminal"},
		{"canceled", func() context.Context { ctx, cancel := context.WithCancel(context.Background()); cancel(); return ctx }, false, CodeContextCanceled, "runner-committed-terminal-bind"},
		{"deadline", func() context.Context {
			ctx, cancel := context.WithDeadline(context.Background(), testExpiredTime())
			defer cancel()
			return ctx
		}, false, CodeDeadlineExceeded, "runner-committed-terminal-bind"},
		{"nil-runner", func() context.Context { return context.Background() }, true, CodeEvidenceJournalFailed, "runner-committed-terminal"},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture, closed, runner := newRunnerCommittedTerminalFixture(t)
			active := runner
			if test.nilRunner {
				active = nil
			}
			durable, err := active.appendCommittedTerminal(test.ctx(), closed)
			assertRunnerCommittedTerminalError(t, err, test.wantCode, test.wantOp)
			if durable != nil || fixture.evidence.terminalBindCalls != 0 || fixture.evidence.journal.appendCalls != 3 || liveRunnerClosedCurrentCommits() != 0 || liveRunnerDurableCommittedTerminals() != 0 || fixture.database.transaction.commitCalls != 1 || fixture.database.transaction.rollbackCalls != 0 {
				t.Fatalf("unavailable context crossed terminal boundary: durable=%+v err=%v evidence=%+v", durable, err, fixture.evidence)
			}
			if cleanupErr := closeRunnerEvidenceOwnership(fixture.evidence, fixture.candidate); cleanupErr != nil {
				t.Fatal(cleanupErr)
			}
		})
	}
}

func TestRunnerCommittedTerminalFaultsNeverTouchDatabaseOrDuplicateTerminal(t *testing.T) {
	postMutationRevoked := map[string]bool{
		"append-unknown": true, "append-error-values": true, "result-record": true,
		"result-cursor": true, "recovery-state": true, "recovery-terminal": true, "recovery-prefix": true,
	}
	tests := []struct {
		name       string
		configure  func(*runnerPreparedCurrentSessionFixture)
		wantCode   ErrorCode
		wantOp     string
		wantAppend int
	}{
		{"bind-error", func(f *runnerPreparedCurrentSessionFixture) { f.evidence.terminalBindErr = errors.New("secret-bind") }, CodeEvidenceJournalFailed, "runner-committed-terminal-bind", 3},
		{"bind-stable", func(f *runnerPreparedCurrentSessionFixture) {
			f.evidence.terminalBindErr = fail(CodeEvidenceRecoveryRequired, "fake", "secret", errors.New("secret-bind"))
		}, CodeEvidenceRecoveryRequired, "runner-committed-terminal-bind", 3},
		{"bind-canceled", func(f *runnerPreparedCurrentSessionFixture) { f.evidence.terminalBindErr = context.Canceled }, CodeContextCanceled, "runner-committed-terminal-bind", 3},
		{"bind-missing-journal", func(f *runnerPreparedCurrentSessionFixture) { f.evidence.terminalNoJournal = true }, CodeEvidenceRecoveryRequired, "runner-committed-terminal-bind", 3},
		{"bind-missing-record", func(f *runnerPreparedCurrentSessionFixture) { f.evidence.terminalNoRecord = true }, CodeEvidenceRecoveryRequired, "runner-committed-terminal-bind", 3},
		{"bind-invalid-record", func(f *runnerPreparedCurrentSessionFixture) {
			f.evidence.mutateBoundTerminal = func(v *AttemptTerminalState) { v.Outcome = "aborted_terminal" }
		}, CodeEvidenceJournalFailed, "runner-committed-terminal-bind", 3},
		{"bind-cursor", func(f *runnerPreparedCurrentSessionFixture) {
			f.evidence.mutateTerminalAuthority = func(cursor *JournalCursor, _ *OwnedEvidenceRecord) { cursor.nextSequence++ }
		}, CodeEvidenceJournalFailed, "runner-committed-terminal-bind", 3},
		{"bind-consumed", func(f *runnerPreparedCurrentSessionFixture) {
			f.evidence.mutateTerminalAuthority = func(_ *JournalCursor, owned *OwnedEvidenceRecord) { owned.consumed.Store(true) }
		}, CodeEvidenceJournalFailed, "runner-committed-terminal-bind", 3},
		{"append-error", func(f *runnerPreparedCurrentSessionFixture) {
			f.evidence.journal.appendErr = errors.New("secret-append")
		}, CodeEvidenceJournalFailed, "runner-committed-terminal-append", 4},
		{"append-canceled", func(f *runnerPreparedCurrentSessionFixture) { f.evidence.journal.appendErr = context.Canceled }, CodeContextCanceled, "runner-committed-terminal-append", 4},
		{"append-unknown", func(f *runnerPreparedCurrentSessionFixture) { f.evidence.journal.appendOutcome = appendOutcomeUnknown }, CodeEvidenceJournalFailed, "runner-committed-terminal-append", 4},
		{"append-error-values", func(f *runnerPreparedCurrentSessionFixture) {
			f.evidence.journal.appendErr = errors.New("secret-values")
			f.evidence.journal.appendValuesWithError = true
		}, CodeEvidenceJournalFailed, "runner-committed-terminal-append", 4},
		{"result-record", func(f *runnerPreparedCurrentSessionFixture) {
			f.evidence.journal.mutateAppendResult = func(v *AppendResult) { v.candidateRecordDigest = testDigest("other-terminal") }
		}, CodeEvidenceJournalFailed, "runner-committed-terminal-append", 4},
		{"result-cursor", func(f *runnerPreparedCurrentSessionFixture) {
			f.evidence.journal.mutateAppendResult = func(v *AppendResult) { v.durableCursor.nextSequence++ }
		}, CodeEvidenceJournalFailed, "runner-committed-terminal-append", 4},
		{"recovery-state", func(f *runnerPreparedCurrentSessionFixture) {
			f.evidence.journal.mutateAppendSnapshot = func(v *RecoverySnapshot) { v.state = RecoveryDivergent }
		}, CodeEvidenceJournalFailed, "runner-committed-terminal-evidence", 4},
		{"recovery-terminal", func(f *runnerPreparedCurrentSessionFixture) {
			f.evidence.journal.mutateAppendSnapshot = func(v *RecoverySnapshot) { v.lastTerminal.value.Outcome = "aborted_terminal" }
		}, CodeEvidenceJournalFailed, "runner-committed-terminal-evidence", 4},
		{"recovery-prefix", func(f *runnerPreparedCurrentSessionFixture) {
			f.evidence.journal.mutateAppendSnapshot = func(v *RecoverySnapshot) { v.commitIntent.value.ExpectedLedgerLength++ }
		}, CodeEvidenceJournalFailed, "runner-committed-terminal-evidence", 4},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture, closed, runner := newRunnerCommittedTerminalFixture(t)
			test.configure(&fixture)
			durable, err := runner.appendCommittedTerminal(context.Background(), closed)
			assertRunnerCommittedTerminalError(t, err, test.wantCode, test.wantOp)
			transaction := fixture.database.transaction
			if durable != nil || containsErrorText(err, "secret-") || fixture.evidence.terminalBindCalls != 1 || fixture.evidence.journal.appendCalls != test.wantAppend || transaction.commitCalls != 1 || transaction.rollbackCalls != 0 || fixture.database.unlockCalls != 0 || fixture.database.closeCalls != 1 || fixture.database.backend.commitCalls != 1 || liveRunnerClosedCurrentCommits() != 0 || liveRunnerDurableCommittedTerminals() != 0 {
				t.Fatalf("terminal fault crossed boundary: durable=%+v err=%v transaction=%+v evidence=%+v", durable, err, transaction, fixture.evidence)
			}
			wantCursor := true
			if postMutationRevoked[test.name] {
				wantCursor = false
			}
			if fixture.evidence.journal.cursor.Valid() != wantCursor {
				t.Fatalf("terminal fault cursor validity=%t want=%t", fixture.evidence.journal.cursor.Valid(), wantCursor)
			}
			if cleanupErr := closeRunnerEvidenceOwnership(fixture.evidence, fixture.candidate); cleanupErr != nil {
				t.Fatal(cleanupErr)
			}
		})
	}
}

func TestRunnerDurableCommittedTerminalRejectsLiteralCopyAndDrift(t *testing.T) {
	fixture, closed, runner := newRunnerCommittedTerminalFixture(t)
	durable, err := runner.appendCommittedTerminal(context.Background(), closed)
	if err != nil || !validRunnerDurableCommittedTerminal(durable) {
		t.Fatalf("append terminal: durable=%+v err=%v", durable, err)
	}
	valueType := reflect.TypeOf(*durable)
	for index := 0; index < valueType.NumField(); index++ {
		if valueType.Field(index).PkgPath == "" {
			t.Fatalf("durable terminal field %s became public", valueType.Field(index).Name)
		}
	}
	copyValue := *durable
	if err := closeRunnerDurableCommittedTerminal(&copyValue, nil); !IsCode(err, CodeEvidenceJournalFailed) || !validRunnerDurableCommittedTerminal(durable) {
		t.Fatalf("copy changed original: err=%v", err)
	}
	if err := closeRunnerDurableCommittedTerminal(&runnerDurableCommittedTerminal{}, nil); !IsCode(err, CodeEvidenceJournalFailed) {
		t.Fatalf("literal escaped: err=%v", err)
	}

	originalOutcome := durable.terminal.Outcome
	durable.terminal.Outcome = "aborted_terminal"
	assertDurableCommittedTerminalDrift(t, durable)
	durable.terminal.Outcome = originalOutcome

	originalPrefix := durable.prefixCanonical
	durable.prefixCanonical[0] ^= 0xff
	assertDurableCommittedTerminalDrift(t, durable)
	durable.prefixCanonical = originalPrefix

	originalAction := durable.nextAction
	durable.nextAction = RecoveryReturnFailure
	assertDurableCommittedTerminalDrift(t, durable)
	durable.nextAction = originalAction

	originalSnapshot := fixture.evidence.snapshot.state
	fixture.evidence.snapshot.state = RecoveryDivergent
	assertDurableCommittedTerminalDrift(t, durable)
	fixture.evidence.snapshot.state = originalSnapshot
	if !validRunnerDurableCommittedTerminal(durable) {
		t.Fatal("restored durable terminal did not recover")
	}

	durable.checkpointDigest = testDigest("other-checkpoint")
	err = closeRunnerDurableCommittedTerminal(durable, nil)
	if !IsCode(err, CodeEvidenceJournalFailed) || liveRunnerDurableCommittedTerminals() != 0 {
		t.Fatalf("drifted close did not poison registry: err=%v live=%d", err, liveRunnerDurableCommittedTerminals())
	}
	if err := closeRunnerEvidenceOwnership(fixture.evidence, fixture.candidate); err != nil {
		t.Fatal(err)
	}
}

func TestRunnerCommittedTerminalPreservesDatabaseCloseFailure(t *testing.T) {
	fixture, durableIntent, runner := newRunnerTransactionCommitFixture(t)
	fixture.database.closeErr = errors.New("secret-close")
	closed, err := runner.commitCurrentTransaction(context.Background(), durableIntent)
	if err != nil || closed == nil || closed.connectionCloseProven {
		t.Fatalf("closed commit fixture: closed=%+v err=%v", closed, err)
	}
	durable, err := runner.appendCommittedTerminal(context.Background(), closed)
	if err != nil || !validRunnerDurableCommittedTerminal(durable) || durable.connectionCloseProven {
		t.Fatalf("append terminal after close failure: durable=%+v err=%v", durable, err)
	}
	if closeErr := closeRunnerDurableCommittedTerminal(durable, nil); !IsCode(closeErr, CodeTransactionBoundary) {
		t.Fatalf("database close failure was lost: %v", closeErr)
	}
	if cleanupErr := closeRunnerEvidenceOwnership(fixture.evidence, fixture.candidate); cleanupErr != nil {
		t.Fatal(cleanupErr)
	}
}

func TestRunnerCommittedTerminalHasOneAppendAndNoDatabaseEdge(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "runner_committed_terminal.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	appendCalls := 0
	forbidden := map[string]bool{
		"Commit": true, "Rollback": true, "Close": true, "UnlockAndReset": true,
		"Connect": true, "BeginMigration": true, "ExecuteStatement": true,
		"Insert": true, "Exec": true, "Query": true, "QueryRow": true,
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
			t.Fatalf("committed terminal acquired forbidden %s call edge", selector.Sel.Name)
		}
		return true
	})
	if appendCalls != 1 {
		t.Fatalf("committed terminal append calls=%d want=1", appendCalls)
	}
}

func TestRunnerDurableCommittedTerminalHasNoProductionConsumer(t *testing.T) {
	paths, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	symbols := map[string]bool{
		"runnerDurableCommittedTerminal": true, "runnerDurableCommittedTerminalBinding": true,
		"runnerDurableCommittedTerminalRegistryRecord": true, "runnerDurableCommittedTerminalRegistry": true,
		"appendCommittedTerminal": true, "bindRunnerDurableCommittedTerminal": true,
		"validRunnerDurableCommittedTerminal": true, "closeRunnerDurableCommittedTerminal": true,
	}
	for _, path := range paths {
		name := filepath.Base(path)
		if strings.HasSuffix(name, "_test.go") || name == "runner_committed_terminal.go" {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			identifier, ok := node.(*ast.Ident)
			if ok && symbols[identifier.Name] {
				t.Fatalf("durable committed terminal %s acquired unreviewed production consumer %s", identifier.Name, name)
			}
			return true
		})
	}
}

func newRunnerCommittedTerminalFixture(t *testing.T) (runnerPreparedCurrentSessionFixture, *runnerClosedCurrentCommit, *Runner) {
	t.Helper()
	fixture, durable, runner := newRunnerTransactionCommitFixture(t)
	closed, err := runner.commitCurrentTransaction(context.Background(), durable)
	if err != nil || !validRunnerClosedCurrentCommit(closed) {
		t.Fatalf("committed terminal fixture: closed=%+v err=%v", closed, err)
	}
	return fixture, closed, runner
}

func assertDurableCommittedTerminalDrift(t *testing.T, durable *runnerDurableCommittedTerminal) {
	t.Helper()
	if validRunnerDurableCommittedTerminal(durable) {
		t.Fatal("mutated durable committed terminal remained valid")
	}
}

func assertRunnerCommittedTerminalError(t *testing.T, err error, code ErrorCode, op string) {
	t.Helper()
	var stable *Error
	if !errors.As(err, &stable) || stable.Code != code || stable.Op != op || stable.Err != nil {
		t.Fatalf("committed terminal error: got=%#v want=%s/%s", stable, code, op)
	}
}

func liveRunnerDurableCommittedTerminals() int {
	count := 0
	runnerDurableCommittedTerminalRegistry.Range(func(_, _ any) bool {
		count++
		return true
	})
	return count
}
