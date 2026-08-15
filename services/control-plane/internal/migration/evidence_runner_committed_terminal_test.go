package migration

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestBindBrandNewRunnerCommittedTerminalOwnsExactEvidence(t *testing.T) {
	fixture := newCommittedTerminalRecordFixture(t)
	defer fixture.close(t)
	owned, err := bindBrandNewRunnerCommittedTerminalRecord(
		fixture.seed, fixture.generation, fixture.cursor, fixture.recovery, fixture.header, fixture.chain,
	)
	if err != nil || owned == nil || owned.wire.AttemptTerminal == nil {
		t.Fatalf("bind committed terminal: owned=%+v err=%v", owned, err)
	}
	want, err := buildRunnerCommittedTerminal(fixture.seed)
	if err != nil || !canonicalEqual(*owned.wire.AttemptTerminal, want) {
		t.Fatalf("terminal differs: got=%+v want=%+v err=%v", owned.wire.AttemptTerminal, want, err)
	}
	witness, ok := owned.witness.(ownedAttemptTerminalWitness)
	if !ok || witness.retry != nil || witness.maxAttempts != fixture.seed.maxAttempts || witness.terminalDigest != want.TerminalDigest || !sameCursorIdentity(witness.cursor, fixture.cursor) || len(witness.prefix) != 4 || witness.prefix[0].Record.Header == nil || witness.prefix[1].Record.StatementIntent == nil || witness.prefix[2].Record.Intermediate == nil || witness.prefix[3].Record.CommitIntent == nil {
		t.Fatalf("terminal witness mismatch: %+v", witness)
	}

	fixture.seed.commit.LedgerRow.MigrationName += "-mutated"
	fixture.recovery.commitIntent.value.LedgerRow.MigrationName += "-mutated"
	delete(fixture.chain.plans, evidenceStatementKey(want.MigrationID, want.AttemptIndex, 0))
	if owned.wire.AttemptTerminal.Outcome != "committed" || witness.prefix[3].Record.CommitIntent.LedgerRow.MigrationName == fixture.seed.commit.LedgerRow.MigrationName || len(witness.chain.plans) != 1 {
		t.Fatal("terminal authority shared mutable seed, recovery, or chain")
	}
	if _, err := owned.consume(fixture.generation, fixture.cursor); err != nil {
		t.Fatal(err)
	}
	if _, err := owned.consume(fixture.generation, fixture.cursor); err == nil {
		t.Fatal("terminal append authority was reusable")
	}
}

func TestBindBrandNewRunnerCommittedTerminalRejectsEveryUnownedBoundary(t *testing.T) {
	faults := []struct {
		name   string
		mutate func(*committedTerminalRecordFixture)
	}{
		{"attempt-limit", func(f *committedTerminalRecordFixture) { f.seed.maxAttempts = 0 }},
		{"intent", func(f *committedTerminalRecordFixture) { f.seed.intent.StatementIndex++ }},
		{"intermediate", func(f *committedTerminalRecordFixture) { f.seed.intermediate.State.StatementIndex++ }},
		{"commit", func(f *committedTerminalRecordFixture) { f.seed.commit.LedgerRow.MigrationName += "-drift" }},
		{"cursor-sequence", func(f *committedTerminalRecordFixture) { f.cursor.nextSequence++ }},
		{"cursor-previous", func(f *committedTerminalRecordFixture) {
			f.cursor.previousRecordDigest = digestPointer(testDigest("other-commit"))
		}},
		{"cursor-owner", func(f *committedTerminalRecordFixture) { f.cursor.generation.owner = &evidenceOwnerToken{} }},
		{"generation", func(f *committedTerminalRecordFixture) {
			f.generation.journalIdentityDigest = testDigest("other-journal")
		}},
		{"recovery-state", func(f *committedTerminalRecordFixture) { f.recovery.state = RecoveryTerminal }},
		{"recovery-action", func(f *committedTerminalRecordFixture) { f.recovery.nextPermittedAction = RecoveryReturnFailure }},
		{"recovery-intent", func(f *committedTerminalRecordFixture) {
			f.recovery.lastStatementIntent.recordDigest = testDigest("other-intent")
		}},
		{"recovery-intermediate", func(f *committedTerminalRecordFixture) {
			f.recovery.lastIntermediateEvidence.owner = &evidenceOwnerToken{}
		}},
		{"recovery-commit", func(f *committedTerminalRecordFixture) { f.recovery.commitIntent.value.ExpectedLedgerLength++ }},
		{"header", func(f *committedTerminalRecordFixture) { f.header.AuthorityBindingDigest = testDigest("other-binding") }},
		{"chain-plan", func(f *committedTerminalRecordFixture) { f.chain.plans = map[string]exactStatementEvidenceWitness{} }},
		{"chain-final", func(f *committedTerminalRecordFixture) { f.chain.finalStatementIndex[f.seed.intent.MigrationID]++ }},
		{"chain-catalog", func(f *committedTerminalRecordFixture) {
			f.chain.finalCatalogDigest[f.seed.intent.MigrationID] = testDigest("other-final")
		}},
	}
	for _, test := range faults {
		t.Run(test.name, func(t *testing.T) {
			fixture := newCommittedTerminalRecordFixture(t)
			defer fixture.close(t)
			test.mutate(&fixture)
			if owned, err := bindBrandNewRunnerCommittedTerminalRecord(fixture.seed, fixture.generation, fixture.cursor, fixture.recovery, fixture.header, fixture.chain); owned != nil || err == nil {
				t.Fatalf("fault escaped: owned=%+v err=%v", owned, err)
			}
		})
	}
}

func TestClaimRunnerCommittedTerminalSeedConsumesOnlyCommittedOutcome(t *testing.T) {
	for _, test := range []struct {
		name        string
		outcome     runnerCommitProtocolOutcome
		wantSuccess bool
	}{
		{"committed", runnerCommitProtocolCommitted, true},
		{"rejected", runnerCommitProtocolRejected, false},
		{"ambiguous", runnerCommitProtocolAmbiguous, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture, durable, runner := newRunnerTransactionCommitFixture(t)
			switch test.outcome {
			case runnerCommitProtocolRejected:
				fixture.database.transaction.commitErr = testPGError("40001")
			case runnerCommitProtocolAmbiguous:
				fixture.database.transaction.commitErr = context.Canceled
			}
			closed, err := runner.commitCurrentTransaction(context.Background(), durable)
			if err != nil || !validRunnerClosedCurrentCommit(closed) {
				t.Fatalf("commit outcome: closed=%+v err=%v", closed, err)
			}
			seed, claimErr := claimRunnerCommittedTerminalSeed(closed)
			if test.wantSuccess {
				if claimErr != nil || seed.commitCanonical == ([32]byte{}) || seed.evidence != fixture.evidence || seed.candidateBinding != fixture.candidate.binding || seed.commit.MigrationID == "" || validRunnerClosedCurrentCommit(closed) || liveRunnerClosedCurrentCommits() != 0 {
					t.Fatalf("committed claim: seed=%+v closed=%+v err=%v", seed, closed, claimErr)
				}
			} else {
				if claimErr == nil || !validRunnerClosedCurrentCommit(closed) || liveRunnerClosedCurrentCommits() != 1 {
					t.Fatalf("non-committed outcome was consumed: seed=%+v closed=%+v err=%v", seed, closed, claimErr)
				}
				if closeErr := closeRunnerClosedCurrentCommit(closed, nil); closeErr != nil {
					t.Fatal(closeErr)
				}
			}
			if cleanupErr := closeRunnerEvidenceOwnership(fixture.evidence, fixture.candidate); cleanupErr != nil {
				t.Fatal(cleanupErr)
			}
		})
	}
}

func TestRunnerCommittedTerminalBinderRejectsLiteralSession(t *testing.T) {
	var _ runnerCommittedTerminalRecordBinder = (*generationEvidenceSession)(nil)
	journal, cursor, owned, err := (&generationEvidenceSession{}).bindRunnerCommittedTerminalRecord(context.Background(), &runnerClosedCurrentCommit{})
	if journal != nil || cursor.Valid() || owned != nil || !IsCode(err, CodeEvidenceRecoveryRequired) {
		t.Fatalf("literal session minted committed terminal: journal=%T cursor=%+v owned=%+v err=%v", journal, cursor, owned, err)
	}
}

func TestRunnerCommittedTerminalBinderHasNoUnreviewedConsumerOrMutationEdge(t *testing.T) {
	paths, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	symbols := map[string]bool{
		"runnerCommittedTerminalRecordBinder": true, "bindRunnerCommittedTerminalRecord": true,
		"runnerCommittedTerminalRecordBinderSealed": true, "runnerCommittedTerminalSeed": true,
		"claimRunnerCommittedTerminalSeed": true, "buildRunnerCommittedTerminal": true,
		"bindBrandNewRunnerCommittedTerminalRecord": true,
	}
	allowed := map[string]map[string]bool{
		"evidence_runner_committed_terminal.go": nil,
		"evidence_session.go": {
			"runnerCommittedTerminalRecordBinder": true, "bindRunnerCommittedTerminalRecord": true,
			"runnerCommittedTerminalRecordBinderSealed": true, "claimRunnerCommittedTerminalSeed": true,
			"bindBrandNewRunnerCommittedTerminalRecord": true,
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
			if ok && symbols[identifier.Name] && !allowed[name][identifier.Name] && name != "evidence_runner_committed_terminal.go" {
				t.Fatalf("committed terminal binder %s acquired unreviewed production consumer %s", identifier.Name, name)
			}
			return true
		})
	}
	forbidden := map[string]bool{
		"AppendDurable": true, "Commit": true, "Rollback": true, "Close": true,
		"Connect": true, "BeginMigration": true, "ExecuteStatement": true,
		"Insert": true, "Exec": true, "Query": true, "QueryRow": true,
	}
	for _, name := range []string{"evidence_runner_committed_terminal.go", "evidence_session.go"} {
		file, err := parser.ParseFile(token.NewFileSet(), name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || name == "evidence_session.go" && function.Name.Name != "bindRunnerCommittedTerminalRecord" {
				continue
			}
			ast.Inspect(function, func(node ast.Node) bool {
				selector, ok := node.(*ast.SelectorExpr)
				if ok && forbidden[selector.Sel.Name] {
					t.Fatalf("committed terminal binder acquired forbidden %s mutation edge in %s", selector.Sel.Name, name)
				}
				return true
			})
		}
	}
}

type committedTerminalRecordFixture struct {
	seed       runnerCommittedTerminalSeed
	generation generationIdentity
	cursor     JournalCursor
	recovery   *RecoverySnapshot
	header     JournalHeader
	chain      verifiedEvidenceChainWitness
	runner     runnerPreparedCurrentSessionFixture
}

func newCommittedTerminalRecordFixture(t *testing.T) committedTerminalRecordFixture {
	t.Helper()
	base := newCommitIntentRecordFixture(t)
	commit, err := buildRunnerCommitIntent(base.request)
	if err != nil {
		t.Fatal(err)
	}
	prefix, err := runnerCommittedTerminalPrefix(runnerCommittedTerminalSeed{
		intent: base.request.intent, intermediate: base.request.intermediate, commit: commit,
		intentRecordDigest:       base.recovery.lastStatementIntent.recordDigest,
		intermediateRecordDigest: base.recovery.lastIntermediateEvidence.recordDigest,
		commitRecordDigest:       testDigest("placeholder"),
	}, base.header)
	if err == nil || prefix != nil {
		// The helper intentionally cannot accept an invented commit digest. The
		// real frame below supplies the exact value.
		t.Fatal("committed terminal prefix accepted an invented commit digest")
	}
	headerFrame := EvidenceFrame{FormatVersion: EvidenceFrameFormat, Sequence: 0, RecordKind: EvidenceRecordHeader, Record: EvidenceRecord{Header: &base.header}}
	headerFrame.RecordDigest, _ = headerFrame.ComputeDigest()
	intent := cloneProjectionValue(base.request.intent)
	intentFrame := EvidenceFrame{FormatVersion: EvidenceFrameFormat, Sequence: 1, PreviousRecordDigest: digestPointer(headerFrame.RecordDigest), RecordKind: EvidenceRecordStatementIntent, Record: EvidenceRecord{StatementIntent: &intent}}
	intentFrame.RecordDigest, _ = intentFrame.ComputeDigest()
	intermediate := cloneProjectionValue(base.request.intermediate)
	intermediateFrame := EvidenceFrame{FormatVersion: EvidenceFrameFormat, Sequence: 2, PreviousRecordDigest: digestPointer(intentFrame.RecordDigest), RecordKind: EvidenceRecordIntermediate, Record: EvidenceRecord{Intermediate: &intermediate}}
	intermediateFrame.RecordDigest, _ = intermediateFrame.ComputeDigest()
	commitBody := cloneProjectionValue(commit)
	commitFrame := EvidenceFrame{FormatVersion: EvidenceFrameFormat, Sequence: 3, PreviousRecordDigest: digestPointer(intermediateFrame.RecordDigest), RecordKind: EvidenceRecordCommitIntent, Record: EvidenceRecord{CommitIntent: &commitBody}}
	commitFrame.RecordDigest, err = commitFrame.ComputeDigest()
	if err != nil || commitFrame.Validate() != nil {
		t.Fatalf("commit frame: %+v err=%v", commitFrame, err)
	}
	valid := &atomic.Bool{}
	valid.Store(true)
	cursor := base.cursor.clone()
	cursor.valid = valid
	cursor.nextSequence = 4
	cursor.previousRecordDigest = digestPointer(commitFrame.RecordDigest)
	cursor.lineageIndexNextSequence++
	cursor.lineageIndexPreviousRecordDigest = testDigest("terminal-checkpoint")
	cursor.latestCheckpointRecordDigest = digestPointer(cursor.lineageIndexPreviousRecordDigest)
	recovery := &RecoverySnapshot{
		owner: base.generation.owner, generation: base.generation, cursor: cursor.clone(), tailDigest: commitFrame.RecordDigest,
		state: RecoveryDanglingCommitIntent, migrationID: cloneStringPointer(&intent.MigrationID), attemptIndex: cloneUint32Pointer(&intent.AttemptIndex),
		lastStatementIntent:                  recoveredValue(base.generation, cursor, commitFrame.RecordDigest, intentFrame.RecordDigest, intent),
		lastStatementIntentRecordDigest:      digestPointer(intentFrame.RecordDigest),
		lastIntermediateEvidence:             recoveredValue(base.generation, cursor, commitFrame.RecordDigest, intermediateFrame.RecordDigest, intermediate),
		lastIntermediateEvidenceRecordDigest: digestPointer(intermediateFrame.RecordDigest),
		lastIntermediateStateDigest:          digestPointer(intermediate.State.IntermediateStateDigest),
		commitIntent:                         recoveredValue(base.generation, cursor, commitFrame.RecordDigest, commitFrame.RecordDigest, commit),
		lastCommitIntentRecordDigest:         digestPointer(commitFrame.RecordDigest), nextPermittedAction: RecoveryReconcileCommit,
	}
	seed := runnerCommittedTerminalSeed{
		generation: base.generation, maxAttempts: base.request.maxAttempts, plan: base.request.plan,
		intent: intent, intermediate: intermediate, commit: commit, cursor: cursor.clone(),
		intentRecordDigest: intentFrame.RecordDigest, intermediateRecordDigest: intermediateFrame.RecordDigest,
		commitRecordDigest: commitFrame.RecordDigest, recoveryDigest: generationJournalRecoveryDigest(recovery),
	}
	if _, err := buildRunnerCommittedTerminal(seed); err != nil {
		t.Fatalf("terminal fixture: %v", err)
	}
	return committedTerminalRecordFixture{seed, base.generation, cursor, recovery, base.header, base.chain, base.runner}
}

func (f *committedTerminalRecordFixture) close(t *testing.T) {
	t.Helper()
	if err := closeRunnerEvidenceOwnership(f.runner.evidence, f.runner.candidate); err != nil {
		t.Fatal(err)
	}
}

func testPGError(code string) error { return &pgconn.PgError{Code: code} }
