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
)

func TestBindBrandNewRunnerCommitIntentRecordOwnsExactEvidenceAndWitness(t *testing.T) {
	fixture := newCommitIntentRecordFixture(t)
	defer fixture.close(t)
	owned, err := bindBrandNewRunnerCommitIntentRecord(
		fixture.request, fixture.generation, fixture.cursor, fixture.recovery, fixture.header, fixture.chain,
	)
	if err != nil || owned == nil || owned.wire.CommitIntent == nil {
		t.Fatalf("bind commit intent: owned=%+v err=%v", owned, err)
	}
	want, err := buildRunnerCommitIntent(fixture.request)
	if err != nil || !canonicalEqual(*owned.wire.CommitIntent, want) {
		t.Fatalf("bound commit intent differs from exact request: want=%+v got=%+v err=%v", want, owned.wire.CommitIntent, err)
	}
	witness, ok := owned.witness.(ownedCommitIntentWitness)
	if !ok || witness.priorIntermediateStateDigest != fixture.request.intermediate.State.IntermediateStateDigest || witness.lastIntermediateRecordDigest != fixture.cursor.previousRecordDigestValue() || !sameCursorIdentity(witness.cursor, fixture.cursor) || len(witness.prefix) != 3 || witness.prefix[0].Record.Header == nil || witness.prefix[1].Record.StatementIntent == nil || witness.prefix[2].Record.Intermediate == nil || witness.priorIntermediate.RecordDigest != fixture.cursor.previousRecordDigestValue() {
		t.Fatalf("owned commit witness mismatch: witness=%+v", witness)
	}

	fixture.request.plan.sqlBytes[0] ^= 0xff
	fixture.request.intermediate.State.ControlPlaneStates.SchemaOwner = "mutated"
	fixture.request.ledgerRow.MigrationName += "-mutated"
	fixture.recovery.lastIntermediateEvidence.value.State.StatementIndex++
	delete(fixture.chain.plans, evidenceStatementKey(want.MigrationID, want.AttemptIndex, 0))
	if owned.wire.CommitIntent.LedgerRow.MigrationName == fixture.request.ledgerRow.MigrationName || witness.priorIntermediate.Record.Intermediate.State.ControlPlaneStates.SchemaOwner == "mutated" || len(witness.chain.plans) != 1 {
		t.Fatal("owned commit intent shared mutable request, recovery, or chain inputs")
	}
	if _, err := owned.consume(fixture.generation, fixture.cursor); err != nil {
		t.Fatal(err)
	}
	if _, err := owned.consume(fixture.generation, fixture.cursor); err == nil {
		t.Fatal("owned commit intent record was reusable")
	}
}

func TestBindBrandNewRunnerCommitIntentRecordRejectsEveryUnownedBoundary(t *testing.T) {
	faults := []struct {
		name   string
		mutate func(*commitIntentRecordFixture)
	}{
		{"plan", func(f *commitIntentRecordFixture) { f.request.plan.StatementIndex++ }},
		{"plan-count", func(f *commitIntentRecordFixture) { f.request.planCount++ }},
		{"intent", func(f *commitIntentRecordFixture) { f.request.intent.StatementIndex++ }},
		{"intermediate", func(f *commitIntentRecordFixture) { f.request.intermediate.State.StatementIndex++ }},
		{"ledger-row", func(f *commitIntentRecordFixture) { f.request.ledgerRow.MigrationName += "-drift" }},
		{"ledger-prefix", func(f *commitIntentRecordFixture) { f.request.ledgerPrefixDigest = testDigest("other-prefix") }},
		{"ledger-head", func(f *commitIntentRecordFixture) { f.request.ledgerHead = "000002" }},
		{"ledger-length", func(f *commitIntentRecordFixture) { f.request.ledgerLength++ }},
		{"attempt-limit", func(f *commitIntentRecordFixture) { f.request.maxAttempts = 0 }},
		{"cursor-sequence", func(f *commitIntentRecordFixture) { f.cursor.nextSequence++ }},
		{"cursor-segment", func(f *commitIntentRecordFixture) { f.cursor.segmentIndex++ }},
		{"cursor-owner", func(f *commitIntentRecordFixture) { f.cursor.generation.owner = &evidenceOwnerToken{} }},
		{"cursor-checkpoint", func(f *commitIntentRecordFixture) { f.cursor.latestCheckpointRecordDigest = nil }},
		{"recovery-state", func(f *commitIntentRecordFixture) { f.recovery.state = RecoveryTerminal }},
		{"recovery-action", func(f *commitIntentRecordFixture) { f.recovery.nextPermittedAction = RecoveryReturnFailure }},
		{"recovery-intermediate", func(f *commitIntentRecordFixture) { f.recovery.lastIntermediateEvidence.value.State.StatementIndex++ }},
		{"recovery-record", func(f *commitIntentRecordFixture) {
			f.recovery.lastIntermediateEvidenceRecordDigest = digestPointer(testDigest("other-intermediate"))
		}},
		{"generation", func(f *commitIntentRecordFixture) { f.generation.journalIdentityDigest = testDigest("other-journal") }},
		{"header", func(f *commitIntentRecordFixture) { f.header.AuthorityBindingDigest = testDigest("other-binding") }},
		{"chain-plan", func(f *commitIntentRecordFixture) { f.chain.plans = map[string]exactStatementEvidenceWitness{} }},
		{"chain-receipt", func(f *commitIntentRecordFixture) { f.chain.runtimeReceipt.digest = testDigest("other-runtime") }},
	}
	for _, test := range faults {
		t.Run(test.name, func(t *testing.T) {
			fixture := newCommitIntentRecordFixture(t)
			defer fixture.close(t)
			test.mutate(&fixture)
			if owned, err := bindBrandNewRunnerCommitIntentRecord(fixture.request, fixture.generation, fixture.cursor, fixture.recovery, fixture.header, fixture.chain); owned != nil || err == nil {
				t.Fatalf("fault escaped: owned=%+v err=%v", owned, err)
			}
		})
	}
}

func TestRunnerCommitIntentVerifiedSubjectsBindCurrentDecisionAndLedger(t *testing.T) {
	fixture, durable, runner := newRunnerLedgerReadbackFixture(t)
	readback, err := runner.insertAndReadbackCurrentLedger(context.Background(), durable)
	if err != nil || !validRunnerReadbackCurrentLedger(readback) {
		t.Fatalf("ledger readback: readback=%+v err=%v", readback, err)
	}
	request := runnerCommitIntentRequestFromReadback(readback)
	bindings, err := fixture.candidate.verifiedRun.currentDecision.decision.runnerProjectionBindings()
	if err != nil || runnerCommitIntentVerifiedSubjects(bindings, request) != nil {
		t.Fatalf("verified commit intent subjects: err=%v", err)
	}
	faults := map[string]func(*runnerCommitIntentRecordRequest){
		"catalog contract": func(value *runnerCommitIntentRecordRequest) {
			value.intent.CatalogContractDigest = testDigest("other-contract")
		},
		"authority profile": func(value *runnerCommitIntentRecordRequest) {
			value.intent.AuthorityProfileDigest = testDigest("other-profile")
		},
		"final catalog": func(value *runnerCommitIntentRecordRequest) {
			value.intermediate.PreledgerCatalogResult.Digest = testDigest("other-final")
		},
		"predecessor": func(value *runnerCommitIntentRecordRequest) {
			value.plan.ExpectedTransition.CatalogBefore.Digest = testDigest("other-predecessor")
		},
		"ledger": func(value *runnerCommitIntentRecordRequest) { value.ledgerRow.MigrationName += "-drift" },
	}
	for name, mutate := range faults {
		t.Run(name, func(t *testing.T) {
			bad := cloneRunnerCommitIntentRecordRequest(t, request)
			mutate(&bad)
			if err := runnerCommitIntentVerifiedSubjects(bindings, bad); err == nil {
				t.Fatal("verified commit subject mutation was accepted")
			}
		})
	}
	if err := closeRunnerReadbackCurrentLedger(readback, nil); err != nil {
		t.Fatal(err)
	}
	if err := closeRunnerEvidenceOwnership(fixture.evidence, fixture.candidate); err != nil {
		t.Fatal(err)
	}
}

func TestRunnerCommitIntentRecordBinderRejectsLiteralSession(t *testing.T) {
	var _ runnerCommitIntentRecordBinder = (*generationEvidenceSession)(nil)
	journal, cursor, owned, err := (&generationEvidenceSession{}).bindRunnerCommitIntentRecord(context.Background(), runnerCommitIntentRecordRequest{})
	if journal != nil || cursor.Valid() || owned != nil || !IsCode(err, CodeEvidenceRecoveryRequired) {
		t.Fatalf("literal session minted commit intent: journal=%T cursor=%+v owned=%+v err=%v", journal, cursor, owned, err)
	}
}

func TestRunnerCommitIntentRecordBinderHasNoUnreviewedConsumerOrMutationEdge(t *testing.T) {
	paths, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	symbols := map[string]bool{
		"runnerCommitIntentRecordBinder": true, "runnerCommitIntentRecordRequest": true,
		"bindRunnerCommitIntentRecord": true, "runnerCommitIntentRecordBinderSealed": true,
		"bindBrandNewRunnerCommitIntentRecord": true, "buildRunnerCommitIntent": true,
		"runnerCommitIntentVerifiedSubjects": true,
	}
	allowed := map[string]map[string]bool{
		"evidence_runner_commit_intent.go": nil,
		"evidence_session.go": {
			"runnerCommitIntentRecordRequest": true, "bindRunnerCommitIntentRecord": true,
			"runnerCommitIntentRecordBinderSealed": true, "bindBrandNewRunnerCommitIntentRecord": true,
			"runnerCommitIntentVerifiedSubjects": true,
		},
		"runner_commit_intent.go": {
			"runnerCommitIntentRecordBinder": true, "runnerCommitIntentRecordRequest": true,
			"bindRunnerCommitIntentRecord": true, "buildRunnerCommitIntent": true,
		},
		"runner_transaction_commit.go": {
			"runnerCommitIntentRecordRequest": true, "buildRunnerCommitIntent": true,
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
			if ok && symbols[identifier.Name] && !allowed[name][identifier.Name] && name != "evidence_runner_commit_intent.go" {
				t.Fatalf("runner commit binder %s acquired unreviewed production consumer %s", identifier.Name, name)
			}
			return true
		})
	}
	forbidden := map[string]bool{
		"AppendDurable": true, "ExecuteStatement": true, "BeginMigration": true,
		"Commit": true, "Rollback": true, "Exec": true, "Query": true, "QueryRow": true,
		"AppendExistingSegmentComposite": true, "AppendRotatedSegmentComposite": true,
	}
	for _, name := range []string{"evidence_runner_commit_intent.go", "evidence_session.go"} {
		file, err := parser.ParseFile(token.NewFileSet(), name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			selector, ok := node.(*ast.SelectorExpr)
			if ok && forbidden[selector.Sel.Name] {
				t.Fatalf("commit binder acquired forbidden %s mutation edge in %s", selector.Sel.Name, name)
			}
			return true
		})
	}
}

type commitIntentRecordFixture struct {
	request    runnerCommitIntentRecordRequest
	generation generationIdentity
	cursor     JournalCursor
	recovery   *RecoverySnapshot
	header     JournalHeader
	chain      verifiedEvidenceChainWitness
	runner     runnerPreparedCurrentSessionFixture
}

func newCommitIntentRecordFixture(t *testing.T) commitIntentRecordFixture {
	t.Helper()
	base := newIntermediateRecordFixture(t)
	intermediate, err := buildRunnerFinalIntermediateEvidence(base.request)
	if err != nil {
		t.Fatal(err)
	}
	headerFrame := EvidenceFrame{FormatVersion: EvidenceFrameFormat, Sequence: 0, RecordKind: EvidenceRecordHeader, Record: EvidenceRecord{Header: &base.header}}
	headerFrame.RecordDigest, err = headerFrame.ComputeDigest()
	if err != nil {
		t.Fatal(err)
	}
	intent := cloneProjectionValue(base.request.intent)
	intentFrame := EvidenceFrame{FormatVersion: EvidenceFrameFormat, Sequence: 1, PreviousRecordDigest: digestPointer(headerFrame.RecordDigest), RecordKind: EvidenceRecordStatementIntent, Record: EvidenceRecord{StatementIntent: &intent}}
	intentFrame.RecordDigest, err = intentFrame.ComputeDigest()
	if err != nil {
		t.Fatal(err)
	}
	intermediateFrame := EvidenceFrame{FormatVersion: EvidenceFrameFormat, Sequence: 2, PreviousRecordDigest: digestPointer(intentFrame.RecordDigest), RecordKind: EvidenceRecordIntermediate, Record: EvidenceRecord{Intermediate: &intermediate}}
	intermediateFrame.RecordDigest, err = intermediateFrame.ComputeDigest()
	if err != nil || intermediateFrame.Validate() != nil {
		t.Fatalf("commit fixture intermediate frame: frame=%+v err=%v", intermediateFrame, err)
	}
	valid := &atomic.Bool{}
	valid.Store(true)
	cursor := base.cursor.clone()
	cursor.valid = valid
	cursor.nextSequence = 3
	cursor.previousRecordDigest = digestPointer(intermediateFrame.RecordDigest)
	cursor.lineageIndexNextSequence++
	cursor.lineageIndexPreviousRecordDigest = testDigest("commit-checkpoint")
	cursor.latestCheckpointRecordDigest = digestPointer(cursor.lineageIndexPreviousRecordDigest)
	recovery := &RecoverySnapshot{
		owner: base.generation.owner, generation: base.generation, cursor: cursor.clone(), tailDigest: intermediateFrame.RecordDigest,
		state: RecoveryDanglingIntermediate, migrationID: cloneStringPointer(&intent.MigrationID), attemptIndex: cloneUint32Pointer(&intent.AttemptIndex),
		lastStatementIntent:                  recoveredValue(base.generation, cursor, intermediateFrame.RecordDigest, intentFrame.RecordDigest, intent),
		lastStatementIntentRecordDigest:      digestPointer(intentFrame.RecordDigest),
		lastIntermediateEvidence:             recoveredValue(base.generation, cursor, intermediateFrame.RecordDigest, intermediateFrame.RecordDigest, intermediate),
		lastIntermediateEvidenceRecordDigest: digestPointer(intermediateFrame.RecordDigest),
		lastIntermediateStateDigest:          digestPointer(intermediate.State.IntermediateStateDigest),
		nextPermittedAction:                  recoveryAbortAction(intent.AttemptIndex, base.request.maxAttempts),
	}
	entry := base.runner.bundle.Manifest.SchemaBundle.Migrations[0]
	row := commitIntentLedgerRow(entry, base.generation.schemaBundleDigest)
	prefix, err := LedgerPrefixDigest([]CommitIntentLedgerRow{row})
	if err != nil {
		t.Fatal(err)
	}
	request := runnerCommitIntentRecordRequest{
		generation: base.generation, maxAttempts: base.request.maxAttempts, planCount: 1,
		plan: base.request.plan, intent: intent, intermediate: intermediate,
		ledgerRow: row, ledgerPrefixDigest: prefix, ledgerHead: entry.ID, ledgerLength: 1,
	}
	if _, err := buildRunnerCommitIntent(request); err != nil {
		t.Fatalf("commit fixture body: %v", err)
	}
	return commitIntentRecordFixture{request, base.generation, cursor, recovery, base.header, base.chain, base.runner}
}

func (f *commitIntentRecordFixture) close(t *testing.T) {
	t.Helper()
	if err := closeRunnerEvidenceOwnership(f.runner.evidence, f.runner.candidate); err != nil {
		t.Fatal(err)
	}
}

func runnerCommitIntentRequestFromReadback(readback *runnerReadbackCurrentLedger) runnerCommitIntentRecordRequest {
	return runnerCommitIntentRecordRequest{
		candidateBinding: readback.candidateBinding, generation: readback.generation, recoveryDigest: readback.recoveryDigest,
		maxAttempts: readback.maxAttempts, planCount: readback.dispatch.planCount, plan: readback.plan,
		intent: readback.intent, intermediate: readback.intermediate, ledgerRow: readback.ledgerRow,
		ledgerPrefixDigest: readback.ledgerPrefixDigest, ledgerHead: readback.ledgerHead, ledgerLength: readback.ledgerLength,
	}
}

func cloneRunnerCommitIntentRecordRequest(t *testing.T, request runnerCommitIntentRecordRequest) runnerCommitIntentRecordRequest {
	t.Helper()
	plan, err := cloneRunnerStatementIntentPlan(request.plan)
	if err != nil {
		t.Fatal(err)
	}
	request.plan = plan
	request.intent = cloneProjectionValue(request.intent)
	request.intermediate = cloneProjectionValue(request.intermediate)
	request.ledgerRow = cloneProjectionValue(request.ledgerRow)
	return request
}
