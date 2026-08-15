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

func TestBindBrandNewRunnerStatementIntentRecordOwnsExactPlanAndWitness(t *testing.T) {
	fixture := newStatementIntentRecordFixture(t)
	defer fixture.close(t)
	owned, err := bindBrandNewRunnerStatementIntentRecord(
		fixture.plan, fixture.authority, fixture.catalog, fixture.catalogContract,
		fixture.generation, fixture.cursor, fixture.recovery, fixture.header, fixture.chain,
	)
	if err != nil || owned == nil || owned.wire.StatementIntent == nil {
		t.Fatalf("bind statement intent: owned=%+v err=%v", owned, err)
	}
	intent := owned.wire.StatementIntent
	if intent.MigrationID != fixture.plan.MigrationID || intent.AttemptIndex != 1 || intent.StatementIndex != 0 || intent.SQLPath != fixture.plan.SQLArtifactPath || intent.CatalogContractDigest != fixture.catalogContract || intent.AuthorityBeforeDigest != fixture.authority.Digest || intent.CatalogBeforeDigest != fixture.catalog.Digest || intent.PreviousAttemptTerminalDigest != nil || intent.PreviousIntermediateStateDigest != nil {
		t.Fatalf("statement intent did not bind exact first statement: %+v", intent)
	}
	witness, ok := owned.witness.(ownedStatementIntentWitness)
	if !ok || witness.plan.validateExact() != nil || !sameCursorIdentity(witness.cursor, fixture.cursor) || len(witness.prefix) != 1 || witness.prefix[0].Record.Header == nil || witness.prefix[0].RecordDigest != fixture.cursor.previousRecordDigestValue() {
		t.Fatalf("owned statement witness mismatch: witness=%+v", witness)
	}

	fixture.plan.sqlBytes[0] ^= 0xff
	fixture.plan.Classification.Command = "ALTER"
	delete(fixture.chain.plans, evidenceStatementKey(fixture.plan.MigrationID, 1, 0))
	fixture.authority.Metadata.Snapshot.DatabaseName = "mutated"
	if witness.plan.validateExact() != nil || len(witness.chain.plans) != 1 || owned.wire.StatementIntent.AuthorityBeforeResult.Metadata.Snapshot.DatabaseName == "mutated" {
		t.Fatal("owned statement record shared mutable plan, witness, or projection inputs")
	}
	if _, err := owned.consume(fixture.generation, fixture.cursor); err != nil {
		t.Fatal(err)
	}
	if _, err := owned.consume(fixture.generation, fixture.cursor); err == nil {
		t.Fatal("owned statement record was reusable")
	}
}

func TestBindBrandNewRunnerStatementIntentRecordRejectsEveryUnownedBoundary(t *testing.T) {
	faults := []struct {
		name   string
		mutate func(*statementIntentRecordFixture)
	}{
		{"plan", func(f *statementIntentRecordFixture) { f.plan.StatementIndex++ }},
		{"catalog-contract", func(f *statementIntentRecordFixture) { f.catalogContract = Digest("invalid") }},
		{"authority-index", func(f *statementIntentRecordFixture) { f.authority.Metadata.Snapshot.StatementIndex = nil }},
		{"catalog-scope", func(f *statementIntentRecordFixture) { f.catalog.Metadata.Scope.ScopeKind = "final" }},
		{"cursor-sequence", func(f *statementIntentRecordFixture) { f.cursor.nextSequence++ }},
		{"cursor-segment", func(f *statementIntentRecordFixture) { f.cursor.segmentIndex++ }},
		{"cursor-checkpoint", func(f *statementIntentRecordFixture) {
			f.cursor.latestCheckpointRecordDigest = digestPointer(testDigest("checkpoint"))
		}},
		{"cursor-owner", func(f *statementIntentRecordFixture) { f.cursor.generation.owner = &evidenceOwnerToken{} }},
		{"recovery-state", func(f *statementIntentRecordFixture) { f.recovery.state = RecoveryTerminal }},
		{"recovery-cursor", func(f *statementIntentRecordFixture) { f.recovery.cursor.valid = &atomic.Bool{} }},
		{"generation", func(f *statementIntentRecordFixture) {
			f.generation.journalIdentityDigest = testDigest("other-journal")
		}},
		{"header", func(f *statementIntentRecordFixture) { f.header.AuthorityBindingDigest = testDigest("other-binding") }},
		{"chain-plan", func(f *statementIntentRecordFixture) { f.chain.plans = map[string]exactStatementEvidenceWitness{} }},
		{"chain-receipt", func(f *statementIntentRecordFixture) { f.chain.runtimeReceipt.digest = testDigest("other-runtime") }},
	}
	for _, test := range faults {
		t.Run(test.name, func(t *testing.T) {
			fixture := newStatementIntentRecordFixture(t)
			defer fixture.close(t)
			test.mutate(&fixture)
			if owned, err := bindBrandNewRunnerStatementIntentRecord(
				fixture.plan, fixture.authority, fixture.catalog, fixture.catalogContract,
				fixture.generation, fixture.cursor, fixture.recovery, fixture.header, fixture.chain,
			); owned != nil || err == nil {
				t.Fatalf("fault escaped: owned=%+v err=%v", owned, err)
			}
		})
	}
}

func TestRunnerStatementIntentVerifiedSubjectBindsCurrentDecision(t *testing.T) {
	fixture := newRunnerPreparedCurrentSessionFixture(t)
	defer func() {
		if err := closeRunnerEvidenceOwnership(fixture.evidence, fixture.candidate); err != nil {
			t.Fatal(err)
		}
	}()
	bindings, err := fixture.candidate.verifiedRun.currentDecision.decision.runnerProjectionBindings()
	if err != nil {
		t.Fatal(err)
	}
	expectedAuthority, err := bindings.verifiedAuthority.ExpectedProjection(AuthorityPhaseMigrationTransaction)
	if err != nil {
		t.Fatal(err)
	}
	authorityDigest, err := digestProjectionWrapper(AuthorityProjectionDigestDomain, expectedAuthority)
	if err != nil {
		t.Fatal(err)
	}
	authority := ProjectionResultEvidence{Digest: authorityDigest, Metadata: ProjectionMetadata{VerifiedSubjectDigest: bindings.verifiedAuthority.SubjectDigest()}}
	catalog := ProjectionResultEvidence{Digest: fixture.plans[0].ExpectedTransition.CatalogBefore.Digest, Metadata: ProjectionMetadata{VerifiedSubjectDigest: bindings.initialSchemaScope.SubjectDigest()}}
	want := fixture.bundle.Manifest.SchemaBundle.Migrations[0].CatalogContract.SHA256
	if got, err := runnerStatementIntentVerifiedSubject(bindings, fixture.plans[0], authority, catalog); err != nil || got != want {
		t.Fatalf("verified statement subject: got=%s want=%s err=%v", got, want, err)
	}
	for name, mutate := range map[string]func(*ProjectionResultEvidence, *ProjectionResultEvidence){
		"authority digest": func(a, _ *ProjectionResultEvidence) { a.Digest = testDigest("other-authority") },
		"authority subject": func(a, _ *ProjectionResultEvidence) {
			a.Metadata.VerifiedSubjectDigest = testDigest("other-authority-subject")
		},
		"catalog subject": func(_, c *ProjectionResultEvidence) {
			c.Metadata.VerifiedSubjectDigest = testDigest("other-catalog-subject")
		},
	} {
		t.Run(name, func(t *testing.T) {
			badAuthority, badCatalog := authority, catalog
			mutate(&badAuthority, &badCatalog)
			if got, err := runnerStatementIntentVerifiedSubject(bindings, fixture.plans[0], badAuthority, badCatalog); got != "" || !IsCode(err, CodeUntrusted) {
				t.Fatalf("verified subject fault: got=%s err=%v", got, err)
			}
		})
	}
}

func TestRunnerStatementIntentRecordBinderRejectsLiteralSession(t *testing.T) {
	var _ runnerStatementIntentRecordBinder = (*generationEvidenceSession)(nil)
	journal, cursor, owned, err := (&generationEvidenceSession{}).bindRunnerStatementIntentRecord(context.Background(), runnerStatementIntentRecordRequest{})
	if journal != nil || cursor.Valid() || owned != nil || !IsCode(err, CodeEvidenceRecoveryRequired) {
		t.Fatalf("literal session minted statement record: journal=%T cursor=%+v owned=%+v err=%v", journal, cursor, owned, err)
	}
}

func TestRunnerStatementIntentRecordBinderHasNoUnreviewedConsumerOrMutationEdge(t *testing.T) {
	paths, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	symbols := map[string]bool{
		"runnerStatementIntentRecordBinder": true, "runnerStatementIntentRecordRequest": true,
		"bindRunnerStatementIntentRecord": true, "runnerStatementIntentRecordBinderSealed": true,
		"bindBrandNewRunnerStatementIntentRecord": true, "buildBrandNewRunnerStatementIntent": true,
		"runnerStatementIntentVerifiedSubject": true,
	}
	allowed := map[string]map[string]bool{
		"evidence_runner_statement_intent.go": nil,
		"evidence_session.go": {
			"runnerStatementIntentRecordRequest": true, "bindRunnerStatementIntentRecord": true,
			"runnerStatementIntentRecordBinderSealed": true, "bindBrandNewRunnerStatementIntentRecord": true,
			"runnerStatementIntentVerifiedSubject": true,
		},
		"runner_statement_intent.go": {
			"runnerStatementIntentRecordBinder": true, "runnerStatementIntentRecordRequest": true,
			"bindRunnerStatementIntentRecord": true,
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
			if ok && symbols[identifier.Name] && !allowed[name][identifier.Name] && name != "evidence_runner_statement_intent.go" {
				t.Fatalf("runner statement intent binder %s acquired unreviewed production consumer %s", identifier.Name, name)
			}
			return true
		})
	}
	file, err := parser.ParseFile(token.NewFileSet(), "evidence_runner_statement_intent.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	forbidden := map[string]bool{
		"AppendDurable": true, "ExecuteStatement": true, "BeginMigration": true,
		"Commit": true, "Rollback": true, "Exec": true, "Query": true, "QueryRow": true,
		"AppendExistingSegmentComposite": true, "AppendRotatedSegmentComposite": true,
	}
	ast.Inspect(file, func(node ast.Node) bool {
		selector, ok := node.(*ast.SelectorExpr)
		if ok && forbidden[selector.Sel.Name] {
			t.Fatalf("statement intent record binder acquired forbidden %s mutation edge", selector.Sel.Name)
		}
		return true
	})
	sessionFile, err := parser.ParseFile(token.NewFileSet(), "evidence_session.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, declaration := range sessionFile.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Name.Name != "bindRunnerStatementIntentRecord" {
			continue
		}
		found = true
		ast.Inspect(function.Body, func(node ast.Node) bool {
			selector, ok := node.(*ast.SelectorExpr)
			if ok && forbidden[selector.Sel.Name] {
				t.Fatalf("statement intent session binder acquired forbidden %s mutation edge", selector.Sel.Name)
			}
			return true
		})
	}
	if !found {
		t.Fatal("statement intent session binder is missing")
	}
}

type statementIntentRecordFixture struct {
	plan            StatementPlan
	authority       ProjectionResultEvidence
	catalog         ProjectionResultEvidence
	catalogContract Digest
	generation      generationIdentity
	cursor          JournalCursor
	recovery        *RecoverySnapshot
	header          JournalHeader
	chain           verifiedEvidenceChainWitness
	runner          runnerPreparedCurrentSessionFixture
}

func newStatementIntentRecordFixture(t *testing.T) statementIntentRecordFixture {
	t.Helper()
	runner := newRunnerPreparedCurrentSessionFixture(t)
	document := fixtureObject(t, migrationFixturePath(t, "golden/evidence-record-chain-v1.json"))
	frames := decodeEvidenceFrames(t, document["frames"])
	contextValue := fixtureObjectValue(t, document["validation_context"], "validation context")
	header := cloneProjectionValue(*frames[0].Record.Header)
	owner := &evidenceOwnerToken{nonce: [16]byte{73}}
	generation := recoveryFixtureGeneration(owner, header)
	cursor := runtimeCursorAt(generation, frames[0].RecordDigest, 1)
	schema := recoveryFixtureSchema(t, owner, generation, frames[:1], contextValue)
	plan := runner.plans[0]
	chain := schema.chainWitness
	chain.plans = map[string]exactStatementEvidenceWitness{
		evidenceStatementKey(plan.MigrationID, 1, plan.StatementIndex): exactStatementWitnessFromPlan(plan, 1),
	}
	schema.chainWitness = chain
	recovery, err := buildRecoverySnapshot(frames[:1], cursor, generation, recoveredContinuation{}, schema)
	if err != nil {
		t.Fatal(err)
	}
	wireIntent := frames[1].Record.StatementIntent
	authority := cloneProjectionValue(wireIntent.AuthorityBeforeResult)
	catalog := cloneProjectionValue(wireIntent.CatalogBeforeResult)
	catalog.Digest = plan.ExpectedTransition.CatalogBefore.Digest
	catalog.Metadata.Scope = cloneProjectionValue(&plan.ExpectedTransition.CatalogBefore.Scope)
	return statementIntentRecordFixture{
		plan: plan, authority: authority, catalog: catalog,
		catalogContract: runner.bundle.Manifest.SchemaBundle.Migrations[0].CatalogContract.SHA256,
		generation:      generation, cursor: cursor, recovery: recovery, header: header, chain: chain, runner: runner,
	}
}

func (f *statementIntentRecordFixture) close(t *testing.T) {
	t.Helper()
	if err := closeRunnerEvidenceOwnership(f.runner.evidence, f.runner.candidate); err != nil {
		t.Fatal(err)
	}
}

func (c JournalCursor) previousRecordDigestValue() Digest {
	if c.previousRecordDigest == nil {
		return ""
	}
	return *c.previousRecordDigest
}
