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

func TestBindBrandNewRunnerFinalIntermediateRecordOwnsExactEvidenceAndWitness(t *testing.T) {
	fixture := newIntermediateRecordFixture(t)
	defer fixture.close(t)
	owned, err := bindBrandNewRunnerFinalIntermediateRecord(
		fixture.request, fixture.generation, fixture.cursor, fixture.recovery, fixture.header, fixture.chain,
	)
	if err != nil || owned == nil || owned.wire.Intermediate == nil {
		t.Fatalf("bind final intermediate: owned=%+v err=%v", owned, err)
	}
	want, err := buildRunnerFinalIntermediateEvidence(fixture.request)
	if err != nil || !canonicalEqual(*owned.wire.Intermediate, want) {
		t.Fatalf("bound intermediate differs from exact request: want=%+v got=%+v err=%v", want, owned.wire.Intermediate, err)
	}
	witness, ok := owned.witness.(ownedIntermediateWitness)
	if !ok || witness.plan.validateExact() != nil || witness.stateDigest != want.State.IntermediateStateDigest || !sameCursorIdentity(witness.cursor, fixture.cursor) || len(witness.prefix) != 2 || witness.prefix[0].Record.Header == nil || witness.prefix[1].Record.StatementIntent == nil || witness.priorIntent.RecordDigest != fixture.cursor.previousRecordDigestValue() {
		t.Fatalf("owned intermediate witness mismatch: witness=%+v", witness)
	}

	fixture.request.plan.sqlBytes[0] ^= 0xff
	fixture.request.state.ControlPlaneStates.SchemaOwner = "mutated"
	fixture.recovery.lastStatementIntent.value.StatementIndex++
	delete(fixture.chain.plans, evidenceStatementKey(want.State.MigrationID, want.State.AttemptIndex, want.State.StatementIndex))
	if witness.plan.validateExact() != nil || owned.wire.Intermediate.State.ControlPlaneStates.SchemaOwner == "mutated" || witness.priorIntent.Record.StatementIntent.StatementIndex != 0 || len(witness.chain.plans) != 1 {
		t.Fatal("owned intermediate shared mutable plan, evidence, recovery, or chain inputs")
	}
	if _, err := owned.consume(fixture.generation, fixture.cursor); err != nil {
		t.Fatal(err)
	}
	if _, err := owned.consume(fixture.generation, fixture.cursor); err == nil {
		t.Fatal("owned intermediate record was reusable")
	}
}

func TestBindBrandNewRunnerFinalIntermediateRecordRejectsEveryUnownedBoundary(t *testing.T) {
	faults := []struct {
		name   string
		mutate func(*intermediateRecordFixture)
	}{
		{"plan", func(f *intermediateRecordFixture) { f.request.plan.StatementIndex++ }},
		{"state", func(f *intermediateRecordFixture) { f.request.state.StatementIndex++ }},
		{"authority-after", func(f *intermediateRecordFixture) { f.request.authorityAfter.Digest = testDigest("other-authority") }},
		{"catalog-after", func(f *intermediateRecordFixture) { f.request.catalogAfter.Metadata.Scope.ScopeKind = "predecessor" }},
		{"preledger-authority", func(f *intermediateRecordFixture) {
			f.request.preledgerAuthority.Digest = testDigest("other-preledger-authority")
		}},
		{"preledger-catalog", func(f *intermediateRecordFixture) { f.request.preledgerCatalog = ProjectionResultEvidence{} }},
		{"attempt-limit", func(f *intermediateRecordFixture) { f.request.maxAttempts = 0 }},
		{"cursor-sequence", func(f *intermediateRecordFixture) { f.cursor.nextSequence++ }},
		{"cursor-segment", func(f *intermediateRecordFixture) { f.cursor.segmentIndex++ }},
		{"cursor-owner", func(f *intermediateRecordFixture) { f.cursor.generation.owner = &evidenceOwnerToken{} }},
		{"cursor-checkpoint", func(f *intermediateRecordFixture) { f.cursor.latestCheckpointRecordDigest = nil }},
		{"recovery-state", func(f *intermediateRecordFixture) { f.recovery.state = RecoveryTerminal }},
		{"recovery-action", func(f *intermediateRecordFixture) { f.recovery.nextPermittedAction = RecoveryReturnFailure }},
		{"recovery-intent", func(f *intermediateRecordFixture) { f.recovery.lastStatementIntent.value.StatementIndex++ }},
		{"recovery-record", func(f *intermediateRecordFixture) {
			f.recovery.lastStatementIntentRecordDigest = digestPointer(testDigest("other-intent"))
		}},
		{"generation", func(f *intermediateRecordFixture) { f.generation.journalIdentityDigest = testDigest("other-journal") }},
		{"header", func(f *intermediateRecordFixture) { f.header.AuthorityBindingDigest = testDigest("other-binding") }},
		{"chain-plan", func(f *intermediateRecordFixture) { f.chain.plans = map[string]exactStatementEvidenceWitness{} }},
		{"chain-receipt", func(f *intermediateRecordFixture) { f.chain.runtimeReceipt.digest = testDigest("other-runtime") }},
	}
	for _, test := range faults {
		t.Run(test.name, func(t *testing.T) {
			fixture := newIntermediateRecordFixture(t)
			defer fixture.close(t)
			test.mutate(&fixture)
			if owned, err := bindBrandNewRunnerFinalIntermediateRecord(fixture.request, fixture.generation, fixture.cursor, fixture.recovery, fixture.header, fixture.chain); owned != nil || err == nil {
				t.Fatalf("fault escaped: owned=%+v err=%v", owned, err)
			}
		})
	}
}

func TestRunnerFinalIntermediateVerifiedSubjectsBindCurrentDecisionAndFinalCatalog(t *testing.T) {
	fixture, after, runner, _ := newRunnerPreledgerFixture(t)
	preledger, err := runner.projectCurrentPreledger(context.Background(), after)
	if err != nil || !validRunnerProjectedCurrentPreledger(preledger) {
		t.Fatalf("project pre-ledger: preledger=%+v err=%v", preledger, err)
	}
	request := runnerIntermediateRequestFromPreledger(preledger)
	bindings, err := fixture.candidate.verifiedRun.currentDecision.decision.runnerProjectionBindings()
	if err != nil || runnerFinalIntermediateVerifiedSubjects(bindings, request) != nil {
		t.Fatalf("verified final intermediate subjects: err=%v", err)
	}
	faults := map[string]func(*runnerIntermediateRecordRequest){
		"catalog contract": func(value *runnerIntermediateRecordRequest) {
			value.intent.CatalogContractDigest = testDigest("other-contract")
		},
		"authority subject": func(value *runnerIntermediateRecordRequest) {
			value.authorityAfter.Metadata.VerifiedSubjectDigest = testDigest("other-authority-subject")
		},
		"catalog subject": func(value *runnerIntermediateRecordRequest) {
			value.catalogAfter.Metadata.VerifiedSubjectDigest = testDigest("other-catalog-subject")
		},
		"preledger catalog": func(value *runnerIntermediateRecordRequest) {
			value.preledgerCatalog.Digest = testDigest("other-final-catalog")
		},
		"preledger scope": func(value *runnerIntermediateRecordRequest) { value.preledgerCatalog.Metadata.Scope.SchemaHead = nil },
		"non-final plan":  func(value *runnerIntermediateRecordRequest) { value.plan.StatementIndex++ },
	}
	for name, mutate := range faults {
		t.Run(name, func(t *testing.T) {
			bad := cloneRunnerIntermediateRecordRequest(t, request)
			mutate(&bad)
			if err := runnerFinalIntermediateVerifiedSubjects(bindings, bad); err == nil {
				t.Fatal("verified subject mutation was accepted")
			}
		})
	}
	if err := closeRunnerProjectedCurrentPreledger(preledger, nil); err != nil {
		t.Fatal(err)
	}
	if err := closeRunnerEvidenceOwnership(fixture.evidence, fixture.candidate); err != nil {
		t.Fatal(err)
	}
}

func TestRunnerIntermediateRecordBinderRejectsLiteralSession(t *testing.T) {
	var _ runnerIntermediateRecordBinder = (*generationEvidenceSession)(nil)
	journal, cursor, owned, err := (&generationEvidenceSession{}).bindRunnerIntermediateRecord(context.Background(), runnerIntermediateRecordRequest{})
	if journal != nil || cursor.Valid() || owned != nil || !IsCode(err, CodeEvidenceRecoveryRequired) {
		t.Fatalf("literal session minted intermediate record: journal=%T cursor=%+v owned=%+v err=%v", journal, cursor, owned, err)
	}
}

func TestRunnerIntermediateRecordBinderHasNoUnreviewedConsumerOrMutationEdge(t *testing.T) {
	paths, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	symbols := map[string]bool{
		"runnerIntermediateRecordBinder": true, "runnerIntermediateRecordRequest": true,
		"bindRunnerIntermediateRecord": true, "runnerIntermediateRecordBinderSealed": true,
		"bindBrandNewRunnerFinalIntermediateRecord": true, "buildRunnerFinalIntermediateEvidence": true,
		"runnerFinalIntermediateVerifiedSubjects": true,
	}
	allowed := map[string]map[string]bool{
		"evidence_runner_intermediate.go": nil,
		"evidence_session.go": {
			"runnerIntermediateRecordRequest": true, "bindRunnerIntermediateRecord": true,
			"runnerIntermediateRecordBinderSealed": true, "bindBrandNewRunnerFinalIntermediateRecord": true,
			"runnerFinalIntermediateVerifiedSubjects": true,
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
			if ok && symbols[identifier.Name] && !allowed[name][identifier.Name] && name != "evidence_runner_intermediate.go" {
				t.Fatalf("runner intermediate binder %s acquired unreviewed production consumer %s", identifier.Name, name)
			}
			return true
		})
	}
	forbidden := map[string]bool{
		"AppendDurable": true, "ExecuteStatement": true, "BeginMigration": true,
		"Commit": true, "Rollback": true, "Exec": true, "Query": true, "QueryRow": true,
		"AppendExistingSegmentComposite": true, "AppendRotatedSegmentComposite": true,
	}
	for _, name := range []string{"evidence_runner_intermediate.go", "evidence_session.go"} {
		file, err := parser.ParseFile(token.NewFileSet(), name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			selector, ok := node.(*ast.SelectorExpr)
			if ok && forbidden[selector.Sel.Name] {
				t.Fatalf("intermediate binder acquired forbidden %s mutation edge in %s", selector.Sel.Name, name)
			}
			return true
		})
	}
}

type intermediateRecordFixture struct {
	request    runnerIntermediateRecordRequest
	generation generationIdentity
	cursor     JournalCursor
	recovery   *RecoverySnapshot
	header     JournalHeader
	chain      verifiedEvidenceChainWitness
	runner     runnerPreparedCurrentSessionFixture
}

func newIntermediateRecordFixture(t *testing.T) intermediateRecordFixture {
	t.Helper()
	base := newStatementIntentRecordFixture(t)
	intent, err := buildBrandNewRunnerStatementIntent(
		base.plan, base.authority, base.catalog, base.catalogContract,
		base.header.SchemaBundleDigest, base.header.AuthorityProfileDigest, base.header.AuthorityBindingDigest,
	)
	if err != nil {
		t.Fatal(err)
	}
	headerFrame := EvidenceFrame{FormatVersion: EvidenceFrameFormat, Sequence: 0, RecordKind: EvidenceRecordHeader, Record: EvidenceRecord{Header: &base.header}}
	headerFrame.RecordDigest, err = headerFrame.ComputeDigest()
	if err != nil || headerFrame.RecordDigest != base.cursor.previousRecordDigestValue() {
		t.Fatalf("intermediate fixture header: digest=%s err=%v", headerFrame.RecordDigest, err)
	}
	intentFrame := EvidenceFrame{
		FormatVersion: EvidenceFrameFormat, Sequence: 1, PreviousRecordDigest: digestPointer(headerFrame.RecordDigest),
		RecordKind: EvidenceRecordStatementIntent, Record: EvidenceRecord{StatementIntent: &intent},
	}
	intentFrame.RecordDigest, err = intentFrame.ComputeDigest()
	if err != nil || intentFrame.Validate() != nil {
		t.Fatalf("intermediate fixture intent: frame=%+v err=%v", intentFrame, err)
	}
	valid := &atomic.Bool{}
	valid.Store(true)
	cursor := base.cursor.clone()
	cursor.valid = valid
	cursor.nextSequence = 2
	cursor.previousRecordDigest = digestPointer(intentFrame.RecordDigest)
	cursor.lineageIndexNextSequence++
	cursor.lineageIndexPreviousRecordDigest = testDigest("intermediate-checkpoint")
	cursor.latestCheckpointRecordDigest = digestPointer(cursor.lineageIndexPreviousRecordDigest)
	maxAttempts := base.chain.maxAttempts[base.plan.MigrationID]
	recovery := &RecoverySnapshot{
		owner: base.generation.owner, generation: base.generation, cursor: cursor.clone(), tailDigest: intentFrame.RecordDigest,
		state: RecoveryDanglingStatementIntent, migrationID: cloneStringPointer(&intent.MigrationID), attemptIndex: cloneUint32Pointer(&intent.AttemptIndex),
		lastStatementIntent:             recoveredValue(base.generation, cursor, intentFrame.RecordDigest, intentFrame.RecordDigest, intent),
		lastStatementIntentRecordDigest: digestPointer(intentFrame.RecordDigest), nextPermittedAction: recoveryAbortAction(intent.AttemptIndex, maxAttempts),
	}
	document := fixtureObject(t, migrationFixturePath(t, "golden/evidence-record-chain-v1.json"))
	frames := decodeEvidenceFrames(t, document["frames"])
	template := cloneProjectionValue(*frames[2].Record.Intermediate)
	authorityAfter := cloneProjectionValue(base.authority)
	catalogAfter := cloneProjectionValue(template.CatalogAfterResult)
	catalogAfter.Digest = base.plan.ExpectedTransition.CatalogAfter.Digest
	catalogAfter.Metadata.Snapshot = cloneProjectionValue(base.authority.Metadata.Snapshot)
	catalogAfter.Metadata.VerifiedSubjectDigest = template.CatalogAfterResult.Metadata.VerifiedSubjectDigest
	catalogAfter.Metadata.Scope = cloneProjectionValue(&base.plan.ExpectedTransition.CatalogAfter.Scope)
	preledgerAuthority := cloneProjectionValue(authorityAfter)
	preledgerAuthority.Metadata.Snapshot.StatementIndex = nil
	preledgerCatalog := cloneProjectionValue(*template.PreledgerCatalogResult)
	preledgerCatalog.Digest = base.chain.finalCatalogDigest[base.plan.MigrationID]
	preledgerCatalog.Metadata.Snapshot = cloneProjectionValue(preledgerAuthority.Metadata.Snapshot)
	state := cloneProjectionValue(template.State)
	state.SchemaBundleDigest = base.header.SchemaBundleDigest
	state.CatalogContractDigest = intent.CatalogContractDigest
	state.AuthorityProfileDigest = base.header.AuthorityProfileDigest
	state.AuthorityBindingDigest = base.header.AuthorityBindingDigest
	state.MigrationID = base.plan.MigrationID
	state.AttemptIndex = intent.AttemptIndex
	state.StatementIndex = base.plan.StatementIndex
	state.StatementSHA256 = base.plan.StatementSHA256
	state.PreviousAttemptTerminalDigest = nil
	state.PreviousIntermediateStateDigest = nil
	state.ControlPlaneStates.VerifiedAuthorityDecisionDigest = base.header.RunnerProjectionDecisionDigest
	state.ControlPlaneStates.ExpectedTransitionDigest = base.plan.ExpectedTransitionDigest
	state.AuthorityBeforeDigest = intent.AuthorityBeforeDigest
	state.AuthorityAfterDigest = authorityAfter.Digest
	state.CatalogBeforeDigest = intent.CatalogBeforeDigest
	state.CatalogAfterDigest = catalogAfter.Digest
	state.IntermediateStateDigest, err = state.ComputeDigest()
	if err != nil || state.Validate() != nil {
		t.Fatalf("intermediate fixture state: state=%+v err=%v", state, err)
	}
	request := runnerIntermediateRecordRequest{
		generation: base.generation, maxAttempts: maxAttempts, plan: base.plan, intent: intent, state: state,
		authorityAfter: authorityAfter, catalogAfter: catalogAfter,
		preledgerAuthority: preledgerAuthority, preledgerCatalog: preledgerCatalog,
	}
	if _, err := buildRunnerFinalIntermediateEvidence(request); err != nil {
		t.Fatalf("intermediate fixture evidence: %v", err)
	}
	return intermediateRecordFixture{request, base.generation, cursor, recovery, base.header, base.chain, base.runner}
}

func (f *intermediateRecordFixture) close(t *testing.T) {
	t.Helper()
	if err := closeRunnerEvidenceOwnership(f.runner.evidence, f.runner.candidate); err != nil {
		t.Fatal(err)
	}
}

func runnerIntermediateRequestFromPreledger(preledger *runnerProjectedCurrentPreledger) runnerIntermediateRecordRequest {
	return runnerIntermediateRecordRequest{
		candidateBinding: preledger.candidateBinding, generation: preledger.generation, recoveryDigest: preledger.recoveryDigest,
		maxAttempts: preledger.maxAttempts, plan: preledger.plan, intent: preledger.intent, state: preledger.state,
		authorityAfter: preledger.authorityAfter, catalogAfter: preledger.catalogAfter,
		preledgerAuthority: preledger.preledgerAuthority, preledgerCatalog: preledger.preledgerCatalog,
	}
}

func cloneRunnerIntermediateRecordRequest(t *testing.T, request runnerIntermediateRecordRequest) runnerIntermediateRecordRequest {
	t.Helper()
	plan, err := cloneRunnerStatementIntentPlan(request.plan)
	if err != nil {
		t.Fatal(err)
	}
	request.plan = plan
	request.intent = cloneProjectionValue(request.intent)
	request.state = cloneProjectionValue(request.state)
	request.authorityAfter = cloneProjectionValue(request.authorityAfter)
	request.catalogAfter = cloneProjectionValue(request.catalogAfter)
	request.preledgerAuthority = cloneProjectionValue(request.preledgerAuthority)
	request.preledgerCatalog = cloneProjectionValue(request.preledgerCatalog)
	return request
}
