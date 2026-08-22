package migration

import (
	"context"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

var _ runnerLedgerEntrySuccessEvidenceBinder = (*generationEvidenceSession)(nil)
var _ runnerLedgerEntrySuccessEvidenceBinder = (*runnerLedgerPreflightEvidenceFake)(nil)

type runnerLedgerEntrySuccessFakeWitness struct {
	recordKind EvidenceRecordKind
	generation generationIdentity
	cursor     JournalCursor
}

func (runnerLedgerEntrySuccessFakeWitness) evidenceWitnessSealed()     {}
func (w runnerLedgerEntrySuccessFakeWitness) kind() EvidenceRecordKind { return w.recordKind }
func (w runnerLedgerEntrySuccessFakeWitness) generationIdentity() generationIdentity {
	return w.generation
}
func (w runnerLedgerEntrySuccessFakeWitness) cursorIdentity() JournalCursor { return w.cursor }
func (runnerLedgerEntrySuccessFakeWitness) prefixAndChain() ([]EvidenceFrame, verifiedEvidenceChainWitness) {
	return nil, verifiedEvidenceChainWitness{}
}

func (*runnerLedgerPreflightEvidenceFake) runnerLedgerEntrySuccessEvidenceBinderSealed() {}

func (evidence *runnerLedgerPreflightEvidenceFake) bindRunnerLedgerEntrySuccessRecord(ctx context.Context, request *runnerLedgerEntrySuccessEvidenceRequest) (EvidenceJournal, JournalCursor, *OwnedEvidenceRecord, error) {
	evidence.successBindCalls++
	claimed, err := consumeRunnerLedgerEntrySuccessEvidenceRequest(request, evidence)
	if err != nil {
		return nil, JournalCursor{}, nil, err
	}
	if ctx == nil {
		return nil, JournalCursor{}, nil, fail(CodeEvidenceRecoveryRequired, "runner-ledger-entry-success-test-bind", "test context is unavailable", nil)
	}
	if err := ctx.Err(); err != nil {
		return nil, JournalCursor{}, nil, err
	}
	if evidence.successBindErr != nil {
		return nil, JournalCursor{}, nil, evidence.successBindErr
	}
	if err := evidence.successBindErrAt[evidence.successBindCalls]; err != nil {
		return nil, JournalCursor{}, nil, err
	}
	base := evidence.runnerEvidenceSessionFake
	if base == nil || base.closed || base.journal == nil || base.snapshot == nil ||
		claimed.candidateBinding != base.candidate.binding || !sameGenerationIdentity(claimed.generation, base.active.identity) ||
		generationJournalRecoveryDigest(base.snapshot) != claimed.recoveryDigest ||
		!sameCursorIdentity(claimed.cursor, base.journal.cursor) {
		return nil, JournalCursor{}, nil, fail(CodeEvidenceRecoveryRequired, "runner-ledger-entry-success-test-bind", "test evidence boundary changed", nil)
	}
	cursor := base.journal.cursor.clone()
	witness := runnerLedgerEntrySuccessFakeWitness{
		recordKind: admissionEvidenceRecordKind(claimed.record), generation: claimed.generation, cursor: cursor.clone(),
	}
	owned := &OwnedEvidenceRecord{
		wire: cloneEvidenceRecord(claimed.record), witness: witness,
		generation: claimed.generation, cursor: cursor.clone(), consumed: &atomic.Bool{},
	}
	if evidence.mutateSuccessAuthority != nil {
		evidence.mutateSuccessAuthority(&cursor, owned)
	}
	base.journal.maxAttempts = claimed.maxAttempts
	return base.journal, cursor, owned, nil
}

type runnerLedgerEntrySuccessFixture struct {
	execution *runnerLedgerEntryExecutionAdmissionFixture
}

func newRunnerLedgerEntrySuccessFixture(
	t *testing.T,
	raw []byte,
	decision VerifiedTrustDecision,
	disposition runnerLedgerPreflightDisposition,
	state RecoveryState,
	action RecoveryAction,
) *runnerLedgerEntrySuccessFixture {
	t.Helper()
	kernel := newRunnerLedgerCatalogPreflightFixtureFromRuntime(t, raw, decision)
	service := &runnerLedgerPreflightServiceFixture{kernel: kernel, evidence: newRunnerLedgerPreflightEvidenceFake(t, kernel.base)}
	service.configure(t, disposition, state, action, 16)
	base := service.kernel.base
	claim, err := service.kernel.runner.prepareRunnerLedgerPreflightClaim(
		context.Background(), "test-only", base.bundle, base.plans, service.evidence, base.candidate,
	)
	if err != nil {
		service.close(t)
		t.Fatal(err)
	}
	defer revokeRunnerLedgerPreflightClaim(claim)
	dispatch, err := service.kernel.runner.claimRunnerLedgerPreflightDispatch(context.Background(), service.evidence, base.candidate, claim)
	if err != nil {
		service.close(t)
		t.Fatal(err)
	}
	fact, err := bindRunnerLedgerConsumerFact(generatedRunnerLedgerConsumerProfile, dispatch, base.bundle.Manifest.ManifestDigest)
	if err != nil || fact.action != runnerLedgerConsumerEntryNotImplemented {
		service.close(t)
		t.Fatalf("entry consumer fact=%+v err=%v", fact, err)
	}
	rows := make([]LedgerRow, 0, fact.dispatch.fact.orderedMigrationPrefixLength)
	for index := uint32(0); index < fact.dispatch.fact.orderedMigrationPrefixLength; index++ {
		rows = append(rows, ledgerRowFor(base.bundle.Manifest.SchemaBundle.Migrations[index], base.bundle.Manifest.SchemaBundleDigest))
	}
	database := newRunnerPreflightSession()
	database.ledgerRowsByRead = [][]LedgerRow{
		cloneProjectionValue(rows), cloneProjectionValue(rows), cloneProjectionValue(rows), cloneProjectionValue(rows),
	}
	database.transaction.ledgerPrefix = cloneProjectionValue(rows)
	connector := &runnerPreflightConnector{session: database}
	service.kernel.runner.Connector = connector
	entry := &runnerLedgerEntryAdmissionFixture{
		service: service, fact: fact, database: database, connector: connector,
	}
	return &runnerLedgerEntrySuccessFixture{execution: &runnerLedgerEntryExecutionAdmissionFixture{base: entry}}
}

func (fixture *runnerLedgerEntrySuccessFixture) close(t *testing.T) {
	t.Helper()
	if fixture != nil && fixture.execution != nil {
		fixture.execution.close(t)
	}
}

func (fixture *runnerLedgerEntrySuccessFixture) prepare(t *testing.T) *runnerLedgerEntryExecutionPermit {
	t.Helper()
	evidence := fixture.execution.base.service.evidence
	evidence.mu.Lock()
	recovery := cloneRecoverySnapshot(evidence.recovery)
	evidence.runnerEvidenceSessionFake.snapshot = recovery
	evidence.runnerEvidenceSessionFake.journal.snapshot = recovery
	evidence.runnerEvidenceSessionFake.journal.cursor = recovery.cursor.clone()
	evidence.mu.Unlock()
	permit, err := fixture.execution.prepare(context.Background())
	if err != nil || !validRunnerLedgerEntryExecutionPermit(permit) {
		t.Fatalf("execution permit=%+v err=%v", permit, err)
	}
	return permit
}

func catalogStateForRunnerLedgerEntryPlan(t *testing.T, evidence EvidenceSession, plan StatementPlan, ref CatalogStateDigestRef) CatalogStateProjection {
	t.Helper()
	current := evidence.CurrentCandidate()
	bindings, err := runnerCurrentProjectionBindings(evidence, current)
	if err != nil {
		t.Fatal(err)
	}
	catalog, ok := exactCatalogBindingForHead(bindings.executableCatalogs, plan.MigrationID)
	if !ok {
		t.Fatal("exact catalog binding is unavailable")
	}
	var state CatalogStateProjection
	switch ref.StateKind {
	case "schema_absent":
		state.Absent = &SchemaAbsentProjection{State: "schema_absent", Scope: cloneProjectionValue(ref.Scope), Schema: "cloud_agents"}
	case "schema_present":
		expected := catalog.verifiedCatalog.ExpectedProjection()
		state.Present = &SchemaPresentProjection{State: "schema_present", Scope: cloneProjectionValue(ref.Scope), Body: cloneProjectionValue(expected.Body)}
	default:
		t.Fatalf("unsupported state kind %q", ref.StateKind)
	}
	digest, err := state.ComputeDigest()
	if err != nil || digest != ref.Digest {
		t.Fatalf("catalog state digest=%s want=%s err=%v", digest, ref.Digest, err)
	}
	return state
}

func configureRunnerLedgerEntrySuccessExecution(t *testing.T, fixture *runnerLedgerEntrySuccessFixture, permit *runnerLedgerEntryExecutionPermit) {
	t.Helper()
	base := fixture.execution.base.service.kernel.base
	plans := make([]StatementPlan, 0, permit.selection.planCount)
	for _, plan := range base.plans {
		if plan.MigrationID == permit.selection.migrationID {
			plans = append(plans, plan)
		}
	}
	if len(plans) == 0 {
		t.Fatal("selected success entry has no plans")
	}
	factory := fixture.execution.base.service.kernel.factory
	before := catalogStateForRunnerLedgerEntryPlan(t, fixture.execution.base.service.evidence, plans[0], plans[0].ExpectedTransition.CatalogBefore)
	factory.transitionState = &before
	transaction := fixture.execution.base.database.transaction
	transaction.executeAllowed = true
	transaction.executeMutate = func([]byte) {
		index := transaction.executeCalls - 1
		if index < 0 || index >= len(plans) {
			t.Fatalf("unexpected execute index %d", index)
		}
		after := catalogStateForRunnerLedgerEntryPlan(t, fixture.execution.base.service.evidence, plans[index], plans[index].ExpectedTransition.CatalogAfter)
		factory.transitionState = &after
	}
	fixture.execution.base.service.evidence.runnerEvidenceSessionFake.journal.bundleComplete =
		permit.selection.entryIndex+1 == uint32(len(base.bundle.Manifest.SchemaBundle.Migrations))
}

func advanceRunnerLedgerEntrySuccessToKnownCommit(t *testing.T, fixture *runnerLedgerEntrySuccessFixture, permit *runnerLedgerEntryExecutionPermit) *runnerLedgerEntrySuccessState {
	t.Helper()
	runner := fixture.execution.base.service.kernel.runner
	base := fixture.execution.base.service.kernel.base
	state, err := runner.prepareRunnerLedgerEntrySuccess(context.Background(), permit, base.bundle, base.plans)
	if err == nil {
		state, err = runner.beginRunnerLedgerEntrySuccessTransaction(context.Background(), state)
	}
	if err == nil {
		state, err = runner.prepareRunnerLedgerEntrySuccessStatement(context.Background(), state)
	}
	for err == nil {
		state, err = runner.appendRunnerLedgerEntrySuccessIntent(context.Background(), state)
		if err != nil {
			break
		}
		state, err = runner.executeRunnerLedgerEntrySuccessStatement(context.Background(), state)
		if err != nil {
			break
		}
		state, err = runner.appendRunnerLedgerEntrySuccessIntermediate(context.Background(), state)
		if err != nil || state.data.phase == runnerLedgerEntrySuccessFinalIntermediateDurable {
			break
		}
		state, err = runner.advanceRunnerLedgerEntrySuccessStatement(context.Background(), state)
	}
	if err == nil {
		state, err = runner.insertRunnerLedgerEntrySuccessLedger(context.Background(), state)
	}
	if err == nil {
		state, err = runner.appendRunnerLedgerEntrySuccessCommitIntent(context.Background(), state)
	}
	if err == nil {
		state, err = runner.commitRunnerLedgerEntrySuccess(context.Background(), state)
	}
	if err != nil || !validRunnerLedgerEntrySuccessState(state) || state.data.phase != runnerLedgerEntrySuccessCommitKnownCommitted {
		t.Fatalf("known-commit state=%+v err=%v", state, err)
	}
	return state
}

func cloneRunnerLedgerEntrySuccessRegistryRecordWithClaim(
	t *testing.T,
	record *runnerLedgerEntrySuccessStateRegistryRecord,
	claim *atomic.Bool,
) *runnerLedgerEntrySuccessStateRegistryRecord {
	t.Helper()
	if record == nil || claim == nil {
		t.Fatal("success-state registry record or claim is unavailable")
	}
	data, err := cloneRunnerLedgerEntrySuccessData(record.data)
	if err != nil {
		t.Fatal(err)
	}
	return &runnerLedgerEntrySuccessStateRegistryRecord{
		state: record.state, binding: cloneRunnerLedgerEntrySuccessStateBinding(record.binding),
		data: data, cleanup: bindRunnerLedgerEntrySuccessCleanupFacts(data, record.canonical), canonical: record.canonical, claimed: claim,
	}
}

func replaceRunnerLedgerEntrySuccessRegistryRecordForTest(
	t *testing.T,
	state *runnerLedgerEntrySuccessState,
	cleanupRegistry bool,
	mutate func(*runnerLedgerEntrySuccessStateRegistryRecord),
) func() {
	t.Helper()
	registry := &runnerLedgerEntrySuccessStateRegistry
	name := "primary"
	if cleanupRegistry {
		registry = &runnerLedgerEntrySuccessStateCleanupRegistry
		name = "cleanup"
	}
	value, ok := registry.Load(state)
	record, recordOK := value.(*runnerLedgerEntrySuccessStateRegistryRecord)
	if !ok || !recordOK || record == nil {
		t.Fatalf("%s success-state registry record is unavailable", name)
	}
	replacement := cloneRunnerLedgerEntrySuccessRegistryRecordWithClaim(t, record, record.claimed)
	mutate(replacement)
	if !validRunnerLedgerEntrySuccessCleanupRecord(replacement, state) ||
		replacement.cleanup.canonical != record.cleanup.canonical ||
		sameRunnerLedgerEntrySuccessCleanupFacts(replacement.cleanup, record.cleanup) {
		t.Fatalf("%s typed replacement did not isolate pointer-only cleanup drift", name)
	}
	registry.Store(state, replacement)
	return func() {
		registry.Store(state, record)
		if validRunnerLedgerEntrySuccessState(state) {
			t.Fatalf("restoring the %s registry revived a consumed authority", name)
		}
		registry.Delete(state)
	}
}

func buildExactMultiStatementAdmissionRuntime(t *testing.T) ([]byte, VerifiedTrustDecision) {
	t.Helper()
	return buildExactStatementCountAdmissionRuntime(t, 2)
}

func buildExactStatementCountAdmissionRuntime(t *testing.T, statementCount int) ([]byte, VerifiedTrustDecision) {
	t.Helper()
	if statementCount < 1 {
		t.Fatalf("invalid statement count %d", statementCount)
	}
	return buildExactAdmissionRuntimeForMigrationCountConfigured(t, 1, nil, func(t *testing.T, entry *MigrationEntry, contract *CatalogContract, files map[string][]byte) {
		var source strings.Builder
		for index := 0; index < statementCount; index++ {
			fmt.Fprintf(&source, "CREATE TABLE cloud_agents.t_%03d (id text);\n", index)
		}
		raw := []byte(source.String())
		statements, err := SplitPostgreSQLStatements(raw)
		if err != nil || len(statements) != statementCount {
			t.Fatalf("split multi-statement fixture: count=%d err=%v", len(statements), err)
		}
		entry.SQLArtifact.SizeBytes = uint64(len(raw))
		entry.SQLArtifact.SHA256 = DigestBytes(raw)
		files[entry.SQLArtifact.Path] = append([]byte(nil), raw...)
		original := cloneProjectionValue(contract.SourceDescriptors[0].Statements[0])
		descriptors := make([]SQLStatementDescriptor, len(statements))
		classifier := NarrowDDLClassifier{SpecialDO: map[SpecialStatementIdentity]Digest{}}
		for index := range statements {
			structural, classifyErr := classifier.Classify(*entry, statements[index])
			if classifyErr != nil {
				t.Fatal(classifyErr)
			}
			descriptors[index] = SQLStatementDescriptor{
				Index: uint64(index), Start: uint64(statements[index].Start), End: uint64(statements[index].End), SHA256: statements[index].SHA256,
				Classification: SQLClassificationDescriptor{
					Profile: "postgresql-ddl-v1", Command: structural.Command, ObjectKind: normalizeObjectKind(structural.ObjectKind),
					TargetIdentity: structural.TargetIdentity, Grantee: cloneStringPointer(structural.Grantee),
				},
				ExpectedTransition: cloneProjectionValue(original.ExpectedTransition),
			}
		}
		migrationID := entry.ID
		for index := 0; index+1 < len(descriptors); index++ {
			through := uint32(index)
			prefixScope := ProjectionScope{
				ScopeKind: "statement_prefix", MigrationID: &migrationID, ThroughStatementIndex: &through,
				DeclaredObjects: cloneProjectionValue(contract.ExpectedProjection.Body.DeclaredObjects),
			}
			prefix := CatalogStateProjection{Present: &SchemaPresentProjection{
				State: "schema_present", Scope: cloneProjectionValue(prefixScope), Body: cloneProjectionValue(contract.ExpectedProjection.Body),
			}}
			prefixDigest, digestErr := prefix.ComputeDigest()
			if digestErr != nil {
				t.Fatal(digestErr)
			}
			descriptors[index].ExpectedTransition.CatalogAfter = CatalogStateDigestRef{Scope: prefixScope, StateKind: "schema_present", Digest: prefixDigest}
			descriptors[index+1].ExpectedTransition.CatalogBefore = cloneProjectionValue(descriptors[index].ExpectedTransition.CatalogAfter)
		}
		contract.SourceDescriptors[0].SQLSHA256 = entry.SQLArtifact.SHA256
		contract.SourceDescriptors[0].Statements = descriptors
	})
}

func TestRunnerLedgerEntrySuccessPlanClosureSupportsCheckedInStatementCounts(t *testing.T) {
	for _, count := range []int{1, 20, 34, 46, 52, 71, 89, 161} {
		t.Run(fmt.Sprintf("count-%03d", count), func(t *testing.T) {
			raw, decision := buildExactStatementCountAdmissionRuntime(t, count)
			fixture := newRunnerLedgerCatalogPreflightFixtureFromRuntime(t, raw, decision)
			defer fixture.close(t, nil)
			digest, actual, err := runnerEntryPlanClosureDigest(fixture.base.plans, "000001")
			if err != nil || actual != uint32(count) || digest == ([32]byte{}) || len(fixture.base.plans) != count {
				t.Fatalf("plan closure count=%d actual=%d digest=%x err=%v", count, actual, digest, err)
			}
			for index := range fixture.base.plans {
				if fixture.base.plans[index].StatementIndex != uint32(index) || fixture.base.plans[index].validateExact() != nil {
					t.Fatalf("plan %d=%+v", index, fixture.base.plans[index])
				}
			}
		})
	}
}

func TestRunnerLedgerEntrySuccessExecutesOneEntryKnownSuccess(t *testing.T) {
	tests := []struct {
		name        string
		runtime     func(*testing.T) ([]byte, VerifiedTrustDecision)
		disposition runnerLedgerPreflightDisposition
		state       RecoveryState
		action      RecoveryAction
		wantPlans   int
		wantLength  uint32
		wantState   string
		rotateOn    int
	}{
		{"single-statement-first-and-complete", buildExactAdmissionRuntime, runnerLedgerPreflightEmptyBrandNew, RecoveryBrandNew, RecoveryBeginFirstAttempt, 1, 1, runnerLedgerEntrySuccessEntryCommittedComplete, 0},
		{"multi-statement-first-and-complete-with-rotation", buildExactMultiStatementAdmissionRuntime, runnerLedgerPreflightEmptyBrandNew, RecoveryBrandNew, RecoveryBeginFirstAttempt, 2, 1, runnerLedgerEntrySuccessEntryCommittedComplete, 2},
		{"first-entry-with-successor", buildExactTwoMigrationAdmissionRuntime, runnerLedgerPreflightEmptyBrandNew, RecoveryBrandNew, RecoveryBeginFirstAttempt, 1, 1, runnerLedgerEntrySuccessEntryCommittedNextEntry, 0},
		{"partial-next-entry", buildExactTwoMigrationAdmissionRuntime, runnerLedgerPreflightPartialNextEntry, RecoveryTerminal, RecoveryBeginFirstAttemptNextEntry, 1, 2, runnerLedgerEntrySuccessEntryCommittedComplete, 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			raw, decision := test.runtime(t)
			fixture := newRunnerLedgerEntrySuccessFixture(t, raw, decision, test.disposition, test.state, test.action)
			defer fixture.close(t)
			permit := fixture.prepare(t)
			configureRunnerLedgerEntrySuccessExecution(t, fixture, permit)
			if test.rotateOn > 0 {
				fixture.execution.base.service.evidence.runnerEvidenceSessionFake.journal.rotateAt = map[int]bool{test.rotateOn: true}
			}
			base := fixture.execution.base.service.kernel.base
			outcome, err := fixture.execution.base.service.kernel.runner.executeRunnerLedgerEntrySuccess(
				context.Background(), permit, base.bundle, base.plans,
			)
			if err != nil || !outcome.valid() || outcome.state != test.wantState || outcome.migrationID != permit.selection.migrationID ||
				outcome.ledgerHead != permit.selection.migrationID || outcome.ledgerLength != test.wantLength {
				t.Fatalf("outcome=%+v err=%v", outcome, err)
			}
			database := fixture.execution.base.database
			transaction := database.transaction
			evidence := fixture.execution.base.service.evidence
			journal := evidence.runnerEvidenceSessionFake.journal
			if transaction.executeCalls != test.wantPlans || transaction.ledgerInsertCalls != 1 || transaction.ledgerReadCalls != 1 ||
				transaction.commitCalls != 1 || transaction.rollbackCalls != 0 || transaction.active || transaction.status != 'I' ||
				database.beginCalls != 1 || database.closeCalls != 1 || !database.closed || database.locked ||
				database.backend.executeCalls != test.wantPlans || database.backend.ledgerInsertCalls != 1 || database.backend.commitCalls != 1 ||
				evidence.successBindCalls != 2*test.wantPlans+2 || journal.appendCalls != 2*test.wantPlans+2 ||
				len(journal.appendedRecords) != 2*test.wantPlans+2 || validRunnerLedgerEntryExecutionPermit(permit) {
				t.Fatalf("success lifecycle database=%+v transaction=%+v evidence=%+v journal=%+v", database, transaction, evidence, journal)
			}
			wantSegment := uint32(0)
			if test.rotateOn > 0 {
				wantSegment = 1
			}
			if journal.cursor.segmentIndex != wantSegment {
				t.Fatalf("final segment=%d want=%d", journal.cursor.segmentIndex, wantSegment)
			}
			selected := make([]StatementPlan, 0, test.wantPlans)
			for _, plan := range base.plans {
				if plan.MigrationID == permit.selection.migrationID {
					selected = append(selected, plan)
				}
			}
			for index := range selected {
				wantSQL, sqlErr := selected[index].exactSQLBytes()
				if sqlErr != nil || !reflect.DeepEqual(transaction.executedSQL[index], wantSQL) {
					t.Fatalf("statement %d SQL=%q want=%q err=%v", index, transaction.executedSQL[index], wantSQL, sqlErr)
				}
				intent := journal.appendedRecords[index*2]
				intermediate := journal.appendedRecords[index*2+1]
				if intent.StatementIntent == nil || intent.StatementIntent.StatementIndex != uint32(index) ||
					intermediate.Intermediate == nil || intermediate.Intermediate.State.StatementIndex != uint32(index) {
					t.Fatalf("statement %d evidence intent=%+v intermediate=%+v", index, intent, intermediate)
				}
				final := index+1 == len(selected)
				if (intermediate.Intermediate.PreledgerAuthorityResult != nil) != final ||
					(intermediate.Intermediate.PreledgerCatalogResult != nil) != final {
					t.Fatalf("statement %d preledger final=%t intermediate=%+v", index, final, intermediate.Intermediate)
				}
			}
			if journal.appendedRecords[len(journal.appendedRecords)-2].CommitIntent == nil ||
				journal.appendedRecords[len(journal.appendedRecords)-1].AttemptTerminal == nil {
				t.Fatalf("terminal evidence chain=%+v", journal.appendedRecords)
			}
		})
	}
}

func TestRunnerLedgerEntrySuccessRejectsCopyLiteralAndSecondTransition(t *testing.T) {
	raw, decision := buildExactAdmissionRuntime(t)
	fixture := newRunnerLedgerEntrySuccessFixture(t, raw, decision, runnerLedgerPreflightEmptyBrandNew, RecoveryBrandNew, RecoveryBeginFirstAttempt)
	defer fixture.close(t)
	permit := fixture.prepare(t)
	configureRunnerLedgerEntrySuccessExecution(t, fixture, permit)
	base := fixture.execution.base.service.kernel.base
	ready, err := fixture.execution.base.service.kernel.runner.prepareRunnerLedgerEntrySuccess(context.Background(), permit, base.bundle, base.plans)
	if err != nil || !validRunnerLedgerEntrySuccessState(ready) {
		t.Fatalf("ready=%+v err=%v", ready, err)
	}
	copyValue := *ready
	if next, copyErr := fixture.execution.base.service.kernel.runner.beginRunnerLedgerEntrySuccessTransaction(context.Background(), &copyValue); next != nil || !IsCode(copyErr, CodeTransactionBoundary) || !validRunnerLedgerEntrySuccessState(ready) {
		t.Fatalf("copy next=%+v err=%v original-valid=%t", next, copyErr, validRunnerLedgerEntrySuccessState(ready))
	}
	if next, literalErr := fixture.execution.base.service.kernel.runner.beginRunnerLedgerEntrySuccessTransaction(context.Background(), &runnerLedgerEntrySuccessState{}); next != nil || !IsCode(literalErr, CodeTransactionBoundary) {
		t.Fatalf("literal next=%+v err=%v", next, literalErr)
	}
	transaction, err := fixture.execution.base.service.kernel.runner.beginRunnerLedgerEntrySuccessTransaction(context.Background(), ready)
	if err != nil || !validRunnerLedgerEntrySuccessState(transaction) {
		t.Fatalf("transaction=%+v err=%v", transaction, err)
	}
	if second, secondErr := fixture.execution.base.service.kernel.runner.beginRunnerLedgerEntrySuccessTransaction(context.Background(), ready); second != nil || !IsCode(secondErr, CodeTransactionBoundary) {
		t.Fatalf("second=%+v err=%v", second, secondErr)
	}
	if closeErr := closeRunnerLedgerEntrySuccessState(transaction, errors.New("test close")); closeErr == nil {
		t.Fatal("explicit close unexpectedly erased primary error")
	}
}

func TestRunnerLedgerEntrySuccessSealFailureRequiresReopenAfterMutation(t *testing.T) {
	for _, test := range []struct {
		name            string
		mutationAttempt bool
		wantCode        ErrorCode
		wantCursor      bool
	}{
		{name: "pre-mutation", wantCode: CodeTransactionBoundary, wantCursor: true},
		{name: "post-mutation", mutationAttempt: true, wantCode: CodeEvidenceRecoveryRequired},
	} {
		t.Run(test.name, func(t *testing.T) {
			valid := &atomic.Bool{}
			valid.Store(true)
			data := runnerLedgerEntrySuccessData{
				mutationAttempted: test.mutationAttempt,
				cursor:            JournalCursor{valid: valid},
			}
			state, err := sealRunnerLedgerEntrySuccessState(data, "invalid", "invalid")
			if state != nil || !IsCode(err, test.wantCode) || valid.Load() != test.wantCursor {
				t.Fatalf("state=%+v err=%v cursor-valid=%t", state, err, valid.Load())
			}
		})
	}
}

func TestRunnerLedgerEntrySuccessEvidenceRequestIsNonCopyableOneShotAndBinderBound(t *testing.T) {
	prepare := func(t *testing.T) (*runnerLedgerEntrySuccessFixture, *runnerLedgerEntrySuccessState) {
		t.Helper()
		raw, decision := buildExactAdmissionRuntime(t)
		fixture := newRunnerLedgerEntrySuccessFixture(t, raw, decision, runnerLedgerPreflightEmptyBrandNew, RecoveryBrandNew, RecoveryBeginFirstAttempt)
		permit := fixture.prepare(t)
		configureRunnerLedgerEntrySuccessExecution(t, fixture, permit)
		base := fixture.execution.base.service.kernel.base
		ready, err := fixture.execution.base.service.kernel.runner.prepareRunnerLedgerEntrySuccess(context.Background(), permit, base.bundle, base.plans)
		if err != nil {
			fixture.close(t)
			t.Fatal(err)
		}
		transaction, err := fixture.execution.base.service.kernel.runner.beginRunnerLedgerEntrySuccessTransaction(context.Background(), ready)
		if err != nil {
			fixture.close(t)
			t.Fatal(err)
		}
		prepared, err := fixture.execution.base.service.kernel.runner.prepareRunnerLedgerEntrySuccessStatement(context.Background(), transaction)
		if err != nil {
			fixture.close(t)
			t.Fatal(err)
		}
		return fixture, prepared
	}
	mint := func(t *testing.T, state *runnerLedgerEntrySuccessState) *runnerLedgerEntrySuccessEvidenceRequest {
		t.Helper()
		data := state.data
		request, err := mintRunnerLedgerEntrySuccessEvidenceRequest(
			data.evidence, data.candidateBinding, data.generation, data.recoveryDigest, data.cursor,
			EvidenceRecord{StatementIntent: cloneStatementIntentPointer(&data.intent)}, data.plans[data.statementIndex], data.maxAttempts,
		)
		if err != nil || !validRunnerLedgerEntrySuccessEvidenceRequest(request, data.evidence) {
			t.Fatalf("request=%+v err=%v", request, err)
		}
		return request
	}

	t.Run("copy-and-one-shot", func(t *testing.T) {
		fixture, prepared := prepare(t)
		defer fixture.close(t)
		defer func() { _ = closeRunnerLedgerEntrySuccessState(prepared, errors.New("test cleanup")) }()
		request := mint(t, prepared)
		copyValue := *request
		if _, err := consumeRunnerLedgerEntrySuccessEvidenceRequest(&copyValue, prepared.data.evidence); !IsCode(err, CodeEvidenceRecoveryRequired) || !validRunnerLedgerEntrySuccessEvidenceRequest(request, prepared.data.evidence) {
			t.Fatalf("copy err=%v original-valid=%t", err, validRunnerLedgerEntrySuccessEvidenceRequest(request, prepared.data.evidence))
		}
		if claimed, err := consumeRunnerLedgerEntrySuccessEvidenceRequest(request, prepared.data.evidence); err != nil || claimed.record.StatementIntent == nil {
			t.Fatalf("claim=%+v err=%v", claimed, err)
		}
		if _, err := consumeRunnerLedgerEntrySuccessEvidenceRequest(request, prepared.data.evidence); !IsCode(err, CodeEvidenceRecoveryRequired) {
			t.Fatalf("second consume err=%v", err)
		}
	})

	t.Run("concurrent-one-shot", func(t *testing.T) {
		fixture, prepared := prepare(t)
		defer fixture.close(t)
		defer func() { _ = closeRunnerLedgerEntrySuccessState(prepared, errors.New("test cleanup")) }()
		request := mint(t, prepared)
		type claimResult struct {
			claimed runnerLedgerEntrySuccessEvidenceRequest
			err     error
		}
		start := make(chan struct{})
		results := make(chan claimResult, 2)
		for range 2 {
			go func() {
				<-start
				claimed, err := consumeRunnerLedgerEntrySuccessEvidenceRequest(request, prepared.data.evidence)
				results <- claimResult{claimed: claimed, err: err}
			}()
		}
		close(start)
		successes := 0
		rejected := 0
		for range 2 {
			result := <-results
			switch {
			case result.err == nil && result.claimed.record.StatementIntent != nil:
				successes++
			case IsCode(result.err, CodeEvidenceRecoveryRequired):
				rejected++
			default:
				t.Fatalf("claim=%+v err=%v", result.claimed, result.err)
			}
		}
		if successes != 1 || rejected != 1 || validRunnerLedgerEntrySuccessEvidenceRequest(request, prepared.data.evidence) {
			t.Fatalf("successes=%d rejected=%d request-valid=%t", successes, rejected, validRunnerLedgerEntrySuccessEvidenceRequest(request, prepared.data.evidence))
		}
	})

	t.Run("literal", func(t *testing.T) {
		literal := &runnerLedgerEntrySuccessEvidenceRequest{}
		literal.self = literal
		if _, err := consumeRunnerLedgerEntrySuccessEvidenceRequest(literal, &runnerLedgerPreflightEvidenceFake{}); !IsCode(err, CodeEvidenceRecoveryRequired) {
			t.Fatalf("literal err=%v", err)
		}
	})

	t.Run("foreign-binder", func(t *testing.T) {
		fixture, prepared := prepare(t)
		defer fixture.close(t)
		defer func() { _ = closeRunnerLedgerEntrySuccessState(prepared, errors.New("test cleanup")) }()
		request := mint(t, prepared)
		if _, err := consumeRunnerLedgerEntrySuccessEvidenceRequest(request, &runnerLedgerPreflightEvidenceFake{}); !IsCode(err, CodeEvidenceRecoveryRequired) || validRunnerLedgerEntrySuccessEvidenceRequest(request, prepared.data.evidence) {
			t.Fatalf("foreign binder err=%v request-valid=%t", err, validRunnerLedgerEntrySuccessEvidenceRequest(request, prepared.data.evidence))
		}
	})

	t.Run("tamper", func(t *testing.T) {
		fixture, prepared := prepare(t)
		defer fixture.close(t)
		defer func() { _ = closeRunnerLedgerEntrySuccessState(prepared, errors.New("test cleanup")) }()
		request := mint(t, prepared)
		request.maxAttempts++
		if _, err := consumeRunnerLedgerEntrySuccessEvidenceRequest(request, prepared.data.evidence); !IsCode(err, CodeEvidenceRecoveryRequired) {
			t.Fatalf("tamper err=%v", err)
		}
	})
}

func TestRunnerLedgerEntrySuccessContextStateAndSecondStatementFailuresCloseAuthority(t *testing.T) {
	t.Run("pre-canceled", func(t *testing.T) {
		raw, decision := buildExactAdmissionRuntime(t)
		fixture := newRunnerLedgerEntrySuccessFixture(t, raw, decision, runnerLedgerPreflightEmptyBrandNew, RecoveryBrandNew, RecoveryBeginFirstAttempt)
		defer fixture.close(t)
		permit := fixture.prepare(t)
		configureRunnerLedgerEntrySuccessExecution(t, fixture, permit)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		base := fixture.execution.base.service.kernel.base
		outcome, err := fixture.execution.base.service.kernel.runner.executeRunnerLedgerEntrySuccess(ctx, permit, base.bundle, base.plans)
		if outcome.valid() || !IsCode(err, CodeContextCanceled) || fixture.execution.base.database.beginCalls != 0 ||
			fixture.execution.base.database.closeCalls != 1 || validRunnerLedgerEntryExecutionPermit(permit) {
			t.Fatalf("outcome=%+v err=%v database=%+v", outcome, err, fixture.execution.base.database)
		}
	})

	t.Run("state-tamper", func(t *testing.T) {
		raw, decision := buildExactAdmissionRuntime(t)
		fixture := newRunnerLedgerEntrySuccessFixture(t, raw, decision, runnerLedgerPreflightEmptyBrandNew, RecoveryBrandNew, RecoveryBeginFirstAttempt)
		defer fixture.close(t)
		permit := fixture.prepare(t)
		configureRunnerLedgerEntrySuccessExecution(t, fixture, permit)
		base := fixture.execution.base.service.kernel.base
		ready, err := fixture.execution.base.service.kernel.runner.prepareRunnerLedgerEntrySuccess(context.Background(), permit, base.bundle, base.plans)
		if err != nil {
			t.Fatal(err)
		}
		ready.data.selection.migrationID = "000999"
		next, err := fixture.execution.base.service.kernel.runner.beginRunnerLedgerEntrySuccessTransaction(context.Background(), ready)
		if next != nil || !IsCode(err, CodeTransactionBoundary) || fixture.execution.base.database.beginCalls != 0 ||
			fixture.execution.base.database.closeCalls != 1 || validRunnerLedgerEntrySuccessState(ready) {
			t.Fatalf("next=%+v err=%v database=%+v", next, err, fixture.execution.base.database)
		}
	})

	t.Run("second-statement", func(t *testing.T) {
		raw, decision := buildExactMultiStatementAdmissionRuntime(t)
		fixture := newRunnerLedgerEntrySuccessFixture(t, raw, decision, runnerLedgerPreflightEmptyBrandNew, RecoveryBrandNew, RecoveryBeginFirstAttempt)
		defer fixture.close(t)
		permit := fixture.prepare(t)
		configureRunnerLedgerEntrySuccessExecution(t, fixture, permit)
		fixture.execution.base.database.transaction.executeErrAt[2] = errors.New("second statement failed")
		base := fixture.execution.base.service.kernel.base
		outcome, err := fixture.execution.base.service.kernel.runner.executeRunnerLedgerEntrySuccess(context.Background(), permit, base.bundle, base.plans)
		snapshot := fixture.execution.base.service.evidence.RecoverySnapshot()
		journal := fixture.execution.base.service.evidence.runnerEvidenceSessionFake.journal
		if outcome.valid() || !IsCode(err, CodeEvidenceRecoveryRequired) || snapshot == nil || snapshot.state != RecoveryDanglingStatementIntent ||
			journal.appendCalls != 3 || fixture.execution.base.database.transaction.executeCalls != 2 ||
			fixture.execution.base.database.transaction.rollbackCalls != 1 || fixture.execution.base.database.backend.executeCalls != 2 {
			t.Fatalf("outcome=%+v err=%v snapshot=%+v transaction=%+v journal=%+v", outcome, err, snapshot, fixture.execution.base.database.transaction, journal)
		}
	})
}

func TestRunnerLedgerEntrySuccessPrecommitLiveStateResourceDriftUsesRecordPairCleanup(t *testing.T) {
	phases := []struct {
		name        string
		transaction bool
	}{
		{name: "execution-ready"},
		{name: "transaction-ready", transaction: true},
	}
	for _, phase := range phases {
		drifts := []string{"session", "evidence", "journal", "cursor-validity-cell", "cursor-owner"}
		if phase.transaction {
			drifts = append(drifts, "transaction")
		}
		for _, drift := range drifts {
			phase := phase
			drift := drift
			t.Run(phase.name+"/"+drift, func(t *testing.T) {
				raw, decision := buildExactAdmissionRuntime(t)
				fixture := newRunnerLedgerEntrySuccessFixture(t, raw, decision, runnerLedgerPreflightEmptyBrandNew, RecoveryBrandNew, RecoveryBeginFirstAttempt)
				defer fixture.close(t)
				permit := fixture.prepare(t)
				configureRunnerLedgerEntrySuccessExecution(t, fixture, permit)
				runner := fixture.execution.base.service.kernel.runner
				base := fixture.execution.base.service.kernel.base
				state, err := runner.prepareRunnerLedgerEntrySuccess(context.Background(), permit, base.bundle, base.plans)
				if err == nil && phase.transaction {
					state, err = runner.beginRunnerLedgerEntrySuccessTransaction(context.Background(), state)
				}
				if err != nil || !validRunnerLedgerEntrySuccessState(state) {
					t.Fatalf("phase=%s state=%+v err=%v", phase.name, state, err)
				}

				database := fixture.execution.base.database
				journal := fixture.execution.base.service.evidence.runnerEvidenceSessionFake.journal
				var restore func()
				var foreignUntouched func() bool
				switch drift {
				case "session":
					original := state.data.session
					foreign := newRunnerPreflightSession()
					state.data.session = foreign
					restore = func() { state.data.session = original }
					foreignUntouched = func() bool {
						return foreign.closeCalls == 0 && foreign.unlockCalls == 0 && foreign.beginCalls == 0 && !foreign.closed
					}
				case "transaction":
					original := state.data.transaction
					foreignSession := newRunnerPreflightSession()
					foreign := foreignSession.transaction
					foreign.active = true
					foreign.status = 'T'
					state.data.transaction = foreign
					restore = func() { state.data.transaction = original }
					foreignUntouched = func() bool { return foreign.rollbackCalls == 0 && foreign.active && foreign.status == 'T' }
				case "evidence":
					original := state.data.evidence
					foreignJournal := &runnerEvidenceJournalFake{}
					foreignSession := &runnerEvidenceSessionFake{journal: foreignJournal}
					foreign := &runnerLedgerPreflightEvidenceFake{runnerEvidenceSessionFake: foreignSession}
					state.data.evidence = foreign
					restore = func() { state.data.evidence = original }
					foreignUntouched = func() bool { return foreignSession.closeCalls == 0 && foreignJournal.closeCalls == 0 }
				case "journal":
					original := state.data.journal
					foreign := &runnerEvidenceJournalFake{}
					state.data.journal = foreign
					restore = func() { state.data.journal = original }
					foreignUntouched = func() bool { return foreign.closeCalls == 0 }
				case "cursor-validity-cell":
					original := state.data.cursor.valid
					foreign := &atomic.Bool{}
					foreign.Store(true)
					state.data.cursor.valid = foreign
					restore = func() { state.data.cursor.valid = original }
					foreignUntouched = foreign.Load
				case "cursor-owner":
					original := state.data.cursor.owner
					foreign := &evidenceOwnerToken{nonce: [16]byte{0x6f}}
					state.data.cursor.owner = foreign
					restore = func() { state.data.cursor.owner = original }
					foreignUntouched = func() bool { return true }
				default:
					t.Fatalf("unknown drift %q", drift)
				}

				if phase.transaction {
					_, err = runner.prepareRunnerLedgerEntrySuccessStatement(context.Background(), state)
				} else {
					_, err = runner.beginRunnerLedgerEntrySuccessTransaction(context.Background(), state)
				}
				restore()
				wantRollbacks := 0
				if phase.transaction {
					wantRollbacks = 1
				}
				if !IsCode(err, CodeTransactionBoundary) || validRunnerLedgerEntrySuccessState(state) ||
					database.closeCalls != 1 || database.unlockCalls != 1 || !database.closed || database.locked ||
					database.transaction.rollbackCalls != wantRollbacks || database.transaction.active ||
					journal.cursor.Valid() || !foreignUntouched() {
					t.Fatalf("phase=%s drift=%s state=%+v err=%v database=%+v transaction=%+v cursorValid=%t",
						phase.name, drift, state, err, database, database.transaction, journal.cursor.Valid())
				}
				assertRunnerLedgerEntrySuccessStateRegistriesCleared(t, state)
			})
		}
	}
}

func TestRunnerLedgerEntrySuccessFailureAndUnknownBoundaries(t *testing.T) {
	tests := []struct {
		name              string
		configure         func(*runnerLedgerEntrySuccessFixture)
		wantCode          ErrorCode
		wantAppends       int
		wantExecutes      int
		wantLedger        int
		wantCommits       int
		wantRollbacks     int
		wantRecovery      RecoveryState
		wantCursorRevoked bool
	}{
		{
			name: "transaction-open", wantCode: CodeTransactionBoundary,
			configure: func(f *runnerLedgerEntrySuccessFixture) {
				f.execution.base.database.beginErr = errors.New("begin failed")
			},
		},
		{
			name: "statement-before-projection", wantCode: CodeTransactionBoundary, wantRollbacks: 1,
			configure: func(f *runnerLedgerEntrySuccessFixture) {
				f.execution.base.service.kernel.factory.transitionErr = errors.New("projection failed")
			},
		},
		{
			name: "intent-binder", wantCode: CodeEvidenceJournalFailed, wantRollbacks: 1,
			configure: func(f *runnerLedgerEntrySuccessFixture) {
				f.execution.base.service.evidence.successBindErrAt[1] = fail(CodeEvidenceJournalFailed, "test-intent-bind", "intent bind failed", nil)
			},
		},
		{
			name: "intent-foreign-cursor", wantCode: CodeEvidenceRecoveryRequired, wantRollbacks: 1,
			configure: func(f *runnerLedgerEntrySuccessFixture) {
				f.execution.base.service.evidence.mutateSuccessAuthority = func(cursor *JournalCursor, _ *OwnedEvidenceRecord) {
					cursor.nextSequence++
				}
			},
		},
		{
			name: "intent-zero-write", wantCode: CodeEvidenceJournalFailed, wantAppends: 1, wantRollbacks: 1,
			configure: func(f *runnerLedgerEntrySuccessFixture) {
				f.execution.base.service.evidence.runnerEvidenceSessionFake.journal.appendErrAt = map[int]error{1: errors.New("zero write")}
			},
		},
		{
			name: "intent-durable-result-tamper", wantCode: CodeEvidenceRecoveryRequired, wantAppends: 1, wantRollbacks: 1, wantRecovery: RecoveryDanglingStatementIntent,
			configure: func(f *runnerLedgerEntrySuccessFixture) {
				f.execution.base.service.evidence.runnerEvidenceSessionFake.journal.mutateAppendResult = func(result *AppendResult) {
					result.candidateSequence++
				}
			},
		},
		{
			name: "statement-execute-after-intent", wantCode: CodeEvidenceRecoveryRequired, wantAppends: 1, wantExecutes: 1, wantRollbacks: 1, wantRecovery: RecoveryDanglingStatementIntent,
			configure: func(f *runnerLedgerEntrySuccessFixture) {
				f.execution.base.database.transaction.executeErr = errors.New("statement failed")
			},
		},
		{
			name: "intermediate-unknown", wantCode: CodeEvidenceRecoveryRequired, wantAppends: 2, wantExecutes: 1, wantRollbacks: 1, wantRecovery: RecoveryDanglingStatementIntent,
			configure: func(f *runnerLedgerEntrySuccessFixture) {
				f.execution.base.service.evidence.runnerEvidenceSessionFake.journal.appendOutcomeAt = map[int]appendOutcome{2: appendOutcomeUnknown}
			},
		},
		{
			name: "ledger-insert", wantCode: CodeEvidenceRecoveryRequired, wantAppends: 2, wantExecutes: 1, wantLedger: 1, wantRollbacks: 1, wantRecovery: RecoveryDanglingIntermediate,
			configure: func(f *runnerLedgerEntrySuccessFixture) {
				f.execution.base.database.transaction.ledgerInsertErr = errors.New("ledger insert failed")
			},
		},
		{
			name: "ledger-readback-contradiction", wantCode: CodeInvalidLedger, wantAppends: 2, wantExecutes: 1, wantLedger: 1, wantRollbacks: 1, wantRecovery: RecoveryDanglingIntermediate,
			configure: func(f *runnerLedgerEntrySuccessFixture) {
				f.execution.base.database.transaction.ledgerReadMutate = func([]LedgerRow) []LedgerRow { return nil }
			},
		},
		{
			name: "commit-intent-zero-write", wantCode: CodeEvidenceJournalFailed, wantAppends: 3, wantExecutes: 1, wantLedger: 1, wantRollbacks: 1, wantRecovery: RecoveryDanglingIntermediate,
			configure: func(f *runnerLedgerEntrySuccessFixture) {
				f.execution.base.service.evidence.runnerEvidenceSessionFake.journal.appendErrAt = map[int]error{3: errors.New("commit intent zero write")}
			},
		},
		{
			name: "commit-rejected", wantCode: CodeEvidenceRecoveryRequired, wantAppends: 3, wantExecutes: 1, wantLedger: 1, wantCommits: 1, wantRecovery: RecoveryDanglingCommitIntent, wantCursorRevoked: true,
			configure: func(f *runnerLedgerEntrySuccessFixture) {
				f.execution.base.database.transaction.commitErr = &pgconn.PgError{Code: "40001"}
			},
		},
		{
			name: "commit-ambiguous", wantCode: CodeEvidenceRecoveryRequired, wantAppends: 3, wantExecutes: 1, wantLedger: 1, wantCommits: 1, wantRecovery: RecoveryDanglingCommitIntent, wantCursorRevoked: true,
			configure: func(f *runnerLedgerEntrySuccessFixture) {
				f.execution.base.database.transaction.commitErr = context.DeadlineExceeded
			},
		},
		{
			name: "post-commit-close", wantCode: CodeEvidenceRecoveryRequired, wantAppends: 3, wantExecutes: 1, wantLedger: 1, wantCommits: 1, wantRecovery: RecoveryDanglingCommitIntent, wantCursorRevoked: true,
			configure: func(f *runnerLedgerEntrySuccessFixture) {
				f.execution.base.database.closeErr = errors.New("close failed")
			},
		},
		{
			name: "commit-observation-registry-tamper", wantCode: CodeEvidenceRecoveryRequired, wantAppends: 3, wantExecutes: 1, wantLedger: 1, wantCommits: 1, wantRecovery: RecoveryDanglingCommitIntent, wantCursorRevoked: true,
			configure: func(f *runnerLedgerEntrySuccessFixture) {
				transaction := f.execution.base.database.transaction
				transaction.commitObserveMutate = func() {
					runnerCommitProtocolRegistry.Range(func(key, value any) bool {
						record, ok := value.(runnerCommitProtocolRegistryRecord)
						if ok && sameRunnerOwnedPointer(record.source, transaction) {
							runnerCommitProtocolRegistry.Delete(key)
							return false
						}
						return true
					})
				}
			},
		},
		{
			name: "terminal-binder", wantCode: CodeEvidenceRecoveryRequired, wantAppends: 3, wantExecutes: 1, wantLedger: 1, wantCommits: 1, wantRecovery: RecoveryDanglingCommitIntent, wantCursorRevoked: true,
			configure: func(f *runnerLedgerEntrySuccessFixture) {
				f.execution.base.service.evidence.successBindErrAt[4] = fail(CodeEvidenceJournalFailed, "test-terminal-bind", "terminal bind failed", nil)
			},
		},
		{
			name: "terminal-unknown", wantCode: CodeEvidenceRecoveryRequired, wantAppends: 4, wantExecutes: 1, wantLedger: 1, wantCommits: 1, wantRecovery: RecoveryDanglingCommitIntent, wantCursorRevoked: true,
			configure: func(f *runnerLedgerEntrySuccessFixture) {
				f.execution.base.service.evidence.runnerEvidenceSessionFake.journal.appendOutcomeAt = map[int]appendOutcome{4: appendOutcomeUnknown}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			raw, decision := buildExactAdmissionRuntime(t)
			fixture := newRunnerLedgerEntrySuccessFixture(t, raw, decision, runnerLedgerPreflightEmptyBrandNew, RecoveryBrandNew, RecoveryBeginFirstAttempt)
			defer fixture.close(t)
			permit := fixture.prepare(t)
			configureRunnerLedgerEntrySuccessExecution(t, fixture, permit)
			test.configure(fixture)
			base := fixture.execution.base.service.kernel.base
			outcome, err := fixture.execution.base.service.kernel.runner.executeRunnerLedgerEntrySuccess(context.Background(), permit, base.bundle, base.plans)
			if outcome.valid() || !IsCode(err, test.wantCode) {
				t.Fatalf("outcome=%+v err=%v want=%s", outcome, err, test.wantCode)
			}
			database := fixture.execution.base.database
			transaction := database.transaction
			journal := fixture.execution.base.service.evidence.runnerEvidenceSessionFake.journal
			if journal.appendCalls != test.wantAppends || transaction.executeCalls != test.wantExecutes ||
				transaction.ledgerInsertCalls != test.wantLedger || transaction.commitCalls != test.wantCommits ||
				transaction.rollbackCalls != test.wantRollbacks || database.backend.executeCalls != test.wantExecutes ||
				database.backend.ledgerInsertCalls != test.wantLedger || database.backend.commitCalls != test.wantCommits ||
				!database.closed || database.locked || validRunnerLedgerEntryExecutionPermit(permit) {
				t.Fatalf("failure boundary database=%+v transaction=%+v journal=%+v", database, transaction, journal)
			}
			if test.wantRecovery != "" {
				snapshot := fixture.execution.base.service.evidence.RecoverySnapshot()
				if snapshot == nil || snapshot.state != test.wantRecovery {
					t.Fatalf("recovery=%+v want=%s", snapshot, test.wantRecovery)
				}
			}
			if test.wantCursorRevoked && journal.cursor.Valid() {
				t.Fatalf("post-commit failure retained a reusable cursor: %+v", journal.cursor)
			}
		})
	}
}

func TestRunnerLedgerEntrySuccessPostCommitStateAndRegistryTamperRevokesCursor(t *testing.T) {
	type mutation struct {
		name   string
		mutate func(*testing.T, *runnerLedgerEntrySuccessState) func()
	}
	mutations := []mutation{
		{
			name: "state-self",
			mutate: func(_ *testing.T, state *runnerLedgerEntrySuccessState) func() {
				original := state.self
				state.self = nil
				return func() { state.self = original }
			},
		},
		{
			name: "state-canonical",
			mutate: func(_ *testing.T, state *runnerLedgerEntrySuccessState) func() {
				original := state.canonical
				state.canonical[0] ^= 0xff
				return func() { state.canonical = original }
			},
		},
		{
			name: "state-binding",
			mutate: func(t *testing.T, state *runnerLedgerEntrySuccessState) func() {
				t.Helper()
				if state.binding == nil {
					t.Fatal("success-state binding is unavailable")
				}
				binding := state.binding
				original := binding.canonical
				binding.canonical[0] ^= 0xff
				return func() { binding.canonical = original }
			},
		},
		{
			name: "state-data",
			mutate: func(_ *testing.T, state *runnerLedgerEntrySuccessState) func() {
				original := state.data.selection.migrationID
				state.data.selection.migrationID = "000999"
				return func() { state.data.selection.migrationID = original }
			},
		},
		{
			name: "runtime-bundle-public-projection",
			mutate: func(t *testing.T, state *runnerLedgerEntrySuccessState) func() {
				t.Helper()
				primary, cleanup := runnerLedgerEntrySuccessStateRecordsForTest(t, state)
				if state.data.bundle == nil || primary.data.bundle == nil || cleanup.data.bundle == nil ||
					state.data.bundle == primary.data.bundle || state.data.bundle == cleanup.data.bundle || primary.data.bundle == cleanup.data.bundle ||
					state.data.bundle.Manifest != nil || primary.data.bundle.Manifest != nil || cleanup.data.bundle.Manifest != nil {
					t.Fatal("runtime handles do not own independent zero-projection wrappers")
				}
				state.data.bundle.Manifest = &Manifest{ManifestDigest: testDigest("post-commit-public-projection-drift")}
				if validRunnerLedgerEntrySuccessState(state) || !validRunnerLedgerEntrySuccessRegistryRecord(primary, state) ||
					!validRunnerLedgerEntrySuccessRegistryRecord(cleanup, state) {
					t.Fatal("public runtime projection drift escaped its owning state")
				}
				return func() { state.data.bundle.Manifest = nil }
			},
		},
		{
			name: "frozen-runtime-policy",
			mutate: func(_ *testing.T, state *runnerLedgerEntrySuccessState) func() {
				original := state.data.runtimePolicy.StatementTimeoutMS
				state.data.runtimePolicy.StatementTimeoutMS++
				return func() { state.data.runtimePolicy.StatementTimeoutMS = original }
			},
		},
		{
			name: "frozen-runtime-entry-count",
			mutate: func(_ *testing.T, state *runnerLedgerEntrySuccessState) func() {
				original := state.data.runtimeEntryCount
				state.data.runtimeEntryCount++
				return func() { state.data.runtimeEntryCount = original }
			},
		},
		{
			name: "frozen-runtime-inputs",
			mutate: func(_ *testing.T, state *runnerLedgerEntrySuccessState) func() {
				original := state.data.runtimeInputs
				state.data.runtimeInputs[0] ^= 0xff
				return func() { state.data.runtimeInputs = original }
			},
		},
		{
			name: "shared-bundle-manifest-alias",
			mutate: func(t *testing.T, state *runnerLedgerEntrySuccessState) func() {
				t.Helper()
				primary, cleanup := runnerLedgerEntrySuccessStateRecordsForTest(t, state)
				shared := &Manifest{ManifestDigest: testDigest("post-commit-shared-bundle-drift")}
				state.data.bundle.Manifest = shared
				primary.data.bundle.Manifest = shared
				cleanup.data.bundle.Manifest = shared
				if validRunnerLedgerEntrySuccessRegistryRecord(primary, state) || validRunnerLedgerEntrySuccessRegistryRecord(cleanup, state) ||
					!validRunnerLedgerEntrySuccessCleanupRecord(primary, state) || !validRunnerLedgerEntrySuccessCleanupRecord(cleanup, state) {
					t.Fatal("shared runtime-bundle drift did not isolate recovery-only cleanup provenance")
				}
				return func() {
					state.data.bundle.Manifest = nil
					primary.data.bundle.Manifest = nil
					cleanup.data.bundle.Manifest = nil
				}
			},
		},
		{
			name: "nil-claim-and-shared-bundle-manifest-alias",
			mutate: func(t *testing.T, state *runnerLedgerEntrySuccessState) func() {
				t.Helper()
				primary, cleanup := runnerLedgerEntrySuccessStateRecordsForTest(t, state)
				originalClaim := state.claimed
				shared := &Manifest{ManifestDigest: testDigest("post-commit-combined-bundle-drift")}
				state.claimed = nil
				state.data.bundle.Manifest = shared
				primary.data.bundle.Manifest = shared
				cleanup.data.bundle.Manifest = shared
				if !validRunnerLedgerEntrySuccessCleanupRecord(primary, state) || !validRunnerLedgerEntrySuccessCleanupRecord(cleanup, state) {
					t.Fatal("combined state and runtime drift invalidated recovery-only cleanup provenance")
				}
				return func() {
					state.claimed = originalClaim
					state.data.bundle.Manifest = nil
					primary.data.bundle.Manifest = nil
					cleanup.data.bundle.Manifest = nil
				}
			},
		},
		{
			name: "state-claim",
			mutate: func(_ *testing.T, state *runnerLedgerEntrySuccessState) func() {
				original := state.claimed
				foreign := &atomic.Bool{}
				foreign.Store(true)
				state.claimed = foreign
				return func() { state.claimed = original }
			},
		},
		{
			name: "state-claim-nil",
			mutate: func(_ *testing.T, state *runnerLedgerEntrySuccessState) func() {
				original := state.claimed
				state.claimed = nil
				return func() { state.claimed = original }
			},
		},
		{
			name: "primary-registry-missing",
			mutate: func(_ *testing.T, state *runnerLedgerEntrySuccessState) func() {
				runnerLedgerEntrySuccessStateRegistry.Delete(state)
				return func() {}
			},
		},
		{
			name: "primary-registry-replaced",
			mutate: func(_ *testing.T, state *runnerLedgerEntrySuccessState) func() {
				runnerLedgerEntrySuccessStateRegistry.Store(state, "tampered")
				return func() { runnerLedgerEntrySuccessStateRegistry.Delete(state) }
			},
		},
		{
			name: "primary-registry-binding",
			mutate: func(t *testing.T, state *runnerLedgerEntrySuccessState) func() {
				t.Helper()
				value, ok := runnerLedgerEntrySuccessStateRegistry.Load(state)
				record, recordOK := value.(*runnerLedgerEntrySuccessStateRegistryRecord)
				if !ok || !recordOK || record == nil {
					t.Fatal("primary success-state registry record is unavailable")
				}
				original := record.binding
				record.binding = nil
				return func() { record.binding = original }
			},
		},
		{
			name: "primary-registry-foreign-claimed",
			mutate: func(t *testing.T, state *runnerLedgerEntrySuccessState) func() {
				t.Helper()
				value, ok := runnerLedgerEntrySuccessStateRegistry.Load(state)
				record, recordOK := value.(*runnerLedgerEntrySuccessStateRegistryRecord)
				if !ok || !recordOK || record == nil {
					t.Fatal("primary success-state registry record is unavailable")
				}
				foreign := &atomic.Bool{}
				foreign.Store(true)
				replacement := cloneRunnerLedgerEntrySuccessRegistryRecordWithClaim(t, record, foreign)
				runnerLedgerEntrySuccessStateRegistry.Store(state, replacement)
				return func() {
					runnerLedgerEntrySuccessStateRegistry.Store(state, record)
					if validRunnerLedgerEntrySuccessState(state) {
						t.Fatal("restoring the primary registry revived a consumed authority")
					}
					runnerLedgerEntrySuccessStateRegistry.Delete(state)
				}
			},
		},
		{
			name: "cleanup-registry-missing",
			mutate: func(_ *testing.T, state *runnerLedgerEntrySuccessState) func() {
				runnerLedgerEntrySuccessStateCleanupRegistry.Delete(state)
				return func() {}
			},
		},
		{
			name: "cleanup-registry-replaced",
			mutate: func(_ *testing.T, state *runnerLedgerEntrySuccessState) func() {
				runnerLedgerEntrySuccessStateCleanupRegistry.Store(state, "tampered")
				return func() { runnerLedgerEntrySuccessStateCleanupRegistry.Delete(state) }
			},
		},
		{
			name: "cleanup-registry-binding",
			mutate: func(t *testing.T, state *runnerLedgerEntrySuccessState) func() {
				t.Helper()
				value, ok := runnerLedgerEntrySuccessStateCleanupRegistry.Load(state)
				record, recordOK := value.(*runnerLedgerEntrySuccessStateRegistryRecord)
				if !ok || !recordOK || record == nil {
					t.Fatal("cleanup success-state registry record is unavailable")
				}
				original := record.binding
				record.binding = nil
				return func() { record.binding = original }
			},
		},
		{
			name: "cleanup-registry-foreign-claimed",
			mutate: func(t *testing.T, state *runnerLedgerEntrySuccessState) func() {
				t.Helper()
				value, ok := runnerLedgerEntrySuccessStateCleanupRegistry.Load(state)
				record, recordOK := value.(*runnerLedgerEntrySuccessStateRegistryRecord)
				if !ok || !recordOK || record == nil {
					t.Fatal("cleanup success-state registry record is unavailable")
				}
				foreign := &atomic.Bool{}
				foreign.Store(true)
				replacement := cloneRunnerLedgerEntrySuccessRegistryRecordWithClaim(t, record, foreign)
				runnerLedgerEntrySuccessStateCleanupRegistry.Store(state, replacement)
				return func() {
					runnerLedgerEntrySuccessStateCleanupRegistry.Store(state, record)
					if validRunnerLedgerEntrySuccessState(state) {
						t.Fatal("restoring the cleanup registry revived a consumed authority")
					}
					runnerLedgerEntrySuccessStateCleanupRegistry.Delete(state)
				}
			},
		},
	}
	pointerDrifts := []struct {
		name   string
		mutate func(*runnerLedgerEntrySuccessStateRegistryRecord)
	}{
		{
			name: "foreign-cursor-validity-cell",
			mutate: func(record *runnerLedgerEntrySuccessStateRegistryRecord) {
				foreign := &atomic.Bool{}
				foreign.Store(true)
				record.data.cursor.valid = foreign
				record.binding.cursorValid = foreign
				record.cleanup.cursor.valid = foreign
			},
		},
		{
			name: "foreign-cursor-owner",
			mutate: func(record *runnerLedgerEntrySuccessStateRegistryRecord) {
				foreign := &evidenceOwnerToken{nonce: [16]byte{0x7f}}
				record.data.cursor.owner = foreign
				record.cleanup.cursor.owner = foreign
			},
		},
		{
			name: "foreign-candidate-binding",
			mutate: func(record *runnerLedgerEntrySuccessStateRegistryRecord) {
				foreign := *record.data.candidateBinding
				record.data.candidateBinding = &foreign
				record.binding.candidateBinding = &foreign
				record.cleanup.candidateBinding = &foreign
			},
		},
		{
			name: "foreign-session",
			mutate: func(record *runnerLedgerEntrySuccessStateRegistryRecord) {
				foreign := newRunnerPreflightSession()
				record.data.session = foreign
				record.binding.session = foreign
				record.cleanup.session = foreign
			},
		},
		{
			name: "foreign-transaction",
			mutate: func(record *runnerLedgerEntrySuccessStateRegistryRecord) {
				foreign := newRunnerPreflightSession().transaction
				record.data.transaction = foreign
				record.binding.transaction = foreign
				record.cleanup.transaction = foreign
			},
		},
		{
			name: "foreign-evidence",
			mutate: func(record *runnerLedgerEntrySuccessStateRegistryRecord) {
				foreign := &runnerLedgerPreflightEvidenceFake{}
				record.data.evidence = foreign
				record.binding.evidence = foreign
				record.cleanup.evidence = foreign
			},
		},
		{
			name: "foreign-journal",
			mutate: func(record *runnerLedgerEntrySuccessStateRegistryRecord) {
				foreign := &runnerEvidenceJournalFake{}
				record.data.journal = foreign
				record.binding.journal = foreign
				record.cleanup.journal = foreign
			},
		},
	}
	for _, registry := range []struct {
		name    string
		cleanup bool
	}{{name: "primary"}, {name: "cleanup", cleanup: true}} {
		for _, drift := range pointerDrifts {
			registry := registry
			drift := drift
			mutations = append(mutations, mutation{
				name: registry.name + "-registry-" + drift.name,
				mutate: func(t *testing.T, state *runnerLedgerEntrySuccessState) func() {
					return replaceRunnerLedgerEntrySuccessRegistryRecordForTest(t, state, registry.cleanup, drift.mutate)
				},
			})
		}
	}
	phases := []struct {
		name   string
		invoke func(*testing.T, *Runner, *runnerLedgerEntrySuccessState) error
	}{
		{
			name: "commit-known",
			invoke: func(t *testing.T, runner *Runner, state *runnerLedgerEntrySuccessState) error {
				t.Helper()
				next, err := runner.appendRunnerLedgerEntrySuccessTerminal(context.Background(), state)
				if next != nil {
					t.Fatalf("tampered commit-known state returned next=%+v", next)
				}
				return err
			},
		},
		{
			name: "terminal-durable",
			invoke: func(t *testing.T, _ *Runner, state *runnerLedgerEntrySuccessState) error {
				t.Helper()
				outcome, err := finishRunnerLedgerEntrySuccess(state)
				if outcome.valid() {
					t.Fatalf("tampered terminal state returned outcome=%+v", outcome)
				}
				return err
			},
		},
	}
	for _, phase := range phases {
		for _, mutation := range mutations {
			t.Run(phase.name+"/"+mutation.name, func(t *testing.T) {
				raw, decision := buildExactAdmissionRuntime(t)
				fixture := newRunnerLedgerEntrySuccessFixture(t, raw, decision, runnerLedgerPreflightEmptyBrandNew, RecoveryBrandNew, RecoveryBeginFirstAttempt)
				defer fixture.close(t)
				permit := fixture.prepare(t)
				configureRunnerLedgerEntrySuccessExecution(t, fixture, permit)
				runner := fixture.execution.base.service.kernel.runner
				state := advanceRunnerLedgerEntrySuccessToKnownCommit(t, fixture, permit)
				if phase.name == "terminal-durable" {
					var err error
					state, err = runner.appendRunnerLedgerEntrySuccessTerminal(context.Background(), state)
					if err != nil || !validRunnerLedgerEntrySuccessState(state) {
						t.Fatalf("terminal state=%+v err=%v", state, err)
					}
				}
				journal := fixture.execution.base.service.evidence.runnerEvidenceSessionFake.journal
				if !journal.cursor.Valid() {
					t.Fatal("post-commit cursor was invalid before tamper")
				}
				restore := mutation.mutate(t, state)
				err := phase.invoke(t, runner, state)
				restore()
				if !IsCode(err, CodeEvidenceRecoveryRequired) || journal.cursor.Valid() || validRunnerLedgerEntrySuccessState(state) {
					t.Fatalf("state=%+v err=%v cursorValid=%t", state, err, journal.cursor.Valid())
				}
				assertRunnerLedgerEntrySuccessStateRegistriesCleared(t, state)
			})
		}
	}
}

func TestRunnerLedgerEntrySuccessPostCommitStateIsOneShotUnderConcurrency(t *testing.T) {
	raw, decision := buildExactAdmissionRuntime(t)
	fixture := newRunnerLedgerEntrySuccessFixture(t, raw, decision, runnerLedgerPreflightEmptyBrandNew, RecoveryBrandNew, RecoveryBeginFirstAttempt)
	defer fixture.close(t)
	permit := fixture.prepare(t)
	configureRunnerLedgerEntrySuccessExecution(t, fixture, permit)
	runner := fixture.execution.base.service.kernel.runner
	committed := advanceRunnerLedgerEntrySuccessToKnownCommit(t, fixture, permit)
	type result struct {
		state *runnerLedgerEntrySuccessState
		err   error
	}
	results := make(chan result, 2)
	var wait sync.WaitGroup
	wait.Add(2)
	for range 2 {
		go func() {
			defer wait.Done()
			state, err := runner.appendRunnerLedgerEntrySuccessTerminal(context.Background(), committed)
			results <- result{state: state, err: err}
		}()
	}
	wait.Wait()
	close(results)
	var terminal *runnerLedgerEntrySuccessState
	failures := 0
	for result := range results {
		if result.err == nil && validRunnerLedgerEntrySuccessState(result.state) {
			if terminal != nil {
				t.Fatal("commit-known state produced more than one terminal authority")
			}
			terminal = result.state
			continue
		}
		if result.state != nil || !IsCode(result.err, CodeEvidenceRecoveryRequired) {
			t.Fatalf("unexpected concurrent result state=%+v err=%v", result.state, result.err)
		}
		failures++
	}
	journal := fixture.execution.base.service.evidence.runnerEvidenceSessionFake.journal
	if terminal == nil || failures != 1 || !journal.cursor.Valid() || validRunnerLedgerEntrySuccessState(committed) {
		t.Fatalf("terminal=%+v failures=%d cursorValid=%t", terminal, failures, journal.cursor.Valid())
	}
	assertRunnerLedgerEntrySuccessStateRegistriesCleared(t, committed)
	outcome, err := finishRunnerLedgerEntrySuccess(terminal)
	if err != nil || !outcome.valid() || !journal.cursor.Valid() {
		t.Fatalf("outcome=%+v err=%v cursorValid=%t", outcome, err, journal.cursor.Valid())
	}
	assertRunnerLedgerEntrySuccessStateRegistriesCleared(t, terminal)
}

func assertRunnerLedgerEntrySuccessStateRegistriesCleared(t *testing.T, state *runnerLedgerEntrySuccessState) {
	t.Helper()
	if _, ok := runnerLedgerEntrySuccessStateRegistry.Load(state); ok {
		t.Fatal("primary success-state registry retained a consumed authority")
	}
	if _, ok := runnerLedgerEntrySuccessStateCleanupRegistry.Load(state); ok {
		t.Fatal("cleanup success-state registry retained a consumed authority")
	}
}

func runnerLedgerEntrySuccessStateRecordsForTest(
	t *testing.T,
	state *runnerLedgerEntrySuccessState,
) (*runnerLedgerEntrySuccessStateRegistryRecord, *runnerLedgerEntrySuccessStateRegistryRecord) {
	t.Helper()
	primaryValue, primaryOK := runnerLedgerEntrySuccessStateRegistry.Load(state)
	primary, primaryRecordOK := primaryValue.(*runnerLedgerEntrySuccessStateRegistryRecord)
	cleanupValue, cleanupOK := runnerLedgerEntrySuccessStateCleanupRegistry.Load(state)
	cleanup, cleanupRecordOK := cleanupValue.(*runnerLedgerEntrySuccessStateRegistryRecord)
	if !primaryOK || !primaryRecordOK || primary == nil || !cleanupOK || !cleanupRecordOK || cleanup == nil {
		t.Fatal("success-state registry records are unavailable")
	}
	return primary, cleanup
}

func TestRunnerLedgerEntrySuccessProductionGraphIsDisconnectedAndSuccessOnly(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	newFiles := map[string]bool{
		"evidence_runner_ledger_entry_success.go": true,
		"runner_ledger_entry_success_kernel.go":   true,
	}
	forbiddenImports := map[string]bool{
		"database/sql": true,
		"net/http":     true,
	}
	forbiddenCalls := map[string]bool{
		"Run":                           true,
		"ReserveAndActivateSuccessor":   true,
		"prepareCurrentDatabaseSession": true,
		"prepareCurrentTransaction":     true,
		"prepareCurrentStatement":       true,
		"appendCurrentStatementIntent":  true,
		"runCurrentSingleEntry":         true,
		"reconcileAmbiguous":            true,
		"reconcileUnknownCommit":        true,
		"writeAbortedTerminal":          true,
		"writeAmbiguousResolution":      true,
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || filepath.Ext(name) != ".go" || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, parseErr := parser.ParseFile(token.NewFileSet(), name, nil, 0)
		if parseErr != nil {
			t.Fatalf("parse %s: %v", name, parseErr)
		}
		if newFiles[name] {
			for _, imported := range file.Imports {
				path := strings.Trim(imported.Path.Value, `"`)
				if forbiddenImports[path] || strings.Contains(path, "provider") {
					t.Fatalf("%s imports forbidden package %q", name, path)
				}
			}
		}
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			called := ""
			switch function := call.Fun.(type) {
			case *ast.Ident:
				called = function.Name
			case *ast.SelectorExpr:
				called = function.Sel.Name
			}
			if called == "executeRunnerLedgerEntrySuccess" {
				t.Fatalf("%s connected the Slice C kernel to a production caller", name)
			}
			if newFiles[name] && forbiddenCalls[called] {
				t.Fatalf("%s acquired non-success writer call edge %s", name, called)
			}
			return true
		})
	}
	permitType := reflect.TypeOf(runnerLedgerEntryExecutionPermit{})
	for index := 0; index < permitType.NumMethod(); index++ {
		t.Fatalf("execution permit unexpectedly exposes method %s", permitType.Method(index).Name)
	}
}
