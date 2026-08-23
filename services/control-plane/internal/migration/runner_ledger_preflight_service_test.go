package migration

import (
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
)

type runnerLedgerPreflightEvidenceFake struct {
	*runnerEvidenceSessionFake
	mu                            sync.Mutex
	schema                        verifiedRecoverySchemaWitness
	recovery                      *RecoverySnapshot
	sessionDigest                 [32]byte
	journalDigest                 [32]byte
	bindErr                       error
	consumeErr                    error
	bindCalls                     int
	consumeCalls                  int
	mutateBeforeConsume           func(*runnerLedgerPreflightEvidenceFake)
	afterBind                     func()
	afterConsume                  func()
	entryBindErr                  error
	entryConsumeErr               error
	entryBindCalls                int
	entryConsumeCalls             int
	mutateBeforeEntryConsume      func(*runnerLedgerPreflightEvidenceFake)
	afterEntryBind                func()
	afterEntryConsume             func()
	executionBindErr              error
	executionConsumeErr           error
	executionBindCalls            int
	executionConsumeCalls         int
	mutateBeforeExecutionConsume  func(*runnerLedgerPreflightEvidenceFake)
	afterExecutionBind            func()
	afterExecutionConsume         func()
	recoveryBindErr               error
	recoveryConsumeErr            error
	recoveryBindCalls             int
	recoveryConsumeCalls          int
	mutateBeforeRecoveryConsume   func(*runnerLedgerPreflightEvidenceFake)
	afterRecoveryBind             func()
	afterRecoveryConsume          func()
	successBindErr                error
	successBindErrAt              map[int]error
	successBindCalls              int
	mutateSuccessAuthority        func(*JournalCursor, *OwnedEvidenceRecord)
	reconciliationBindErr         error
	reconciliationBindCalls       int
	reconciliationNoRecord        bool
	mutateReconciliationRecord    func(*EvidenceRecord)
	mutateReconciliationAuthority func(*JournalCursor, *OwnedEvidenceRecord)
	retryHandoffBindErr           error
	retryHandoffBindCalls         int
	mutateRetryHandoffResult      func(*ActiveGeneration, *RecoverySnapshot)
}

var _ runnerLedgerPreflightClaimBinder = (*generationEvidenceSession)(nil)
var _ runnerLedgerPreflightClaimBinder = (*runnerLedgerPreflightEvidenceFake)(nil)
var _ runnerLedgerEntryAdmissionClaimBinder = (*generationEvidenceSession)(nil)
var _ runnerLedgerEntryAdmissionClaimBinder = (*runnerLedgerPreflightEvidenceFake)(nil)
var _ runnerLedgerEntryExecutionAdmissionClaimBinder = (*generationEvidenceSession)(nil)
var _ runnerLedgerEntryExecutionAdmissionClaimBinder = (*runnerLedgerPreflightEvidenceFake)(nil)
var _ runnerLedgerRecoveryAdmissionClaimBinder = (*generationEvidenceSession)(nil)
var _ runnerLedgerRecoveryAdmissionClaimBinder = (*runnerLedgerPreflightEvidenceFake)(nil)

func TestRunnerLedgerPreflightConcreteEvidenceBinderRejectsLiteral(t *testing.T) {
	session := &generationEvidenceSession{}
	if claim, err := session.bindRunnerLedgerPreflightClaim(context.Background(), runnerLedgerPreflightClaimRequest{}); claim != nil || !IsCode(err, CodeEvidenceRecoveryRequired) {
		t.Fatalf("literal concrete binder minted claim=%+v err=%v", claim, err)
	}
	if dispatch, err := session.consumeRunnerLedgerPreflightClaim(context.Background(), nil, OwnedCurrentCandidate{}); dispatch.valid() || !IsCode(err, CodeEvidenceRecoveryRequired) {
		t.Fatalf("literal concrete binder consumed dispatch=%+v err=%v", dispatch, err)
	}
}

func newRunnerLedgerPreflightEvidenceFake(t *testing.T, base runnerPreparedCurrentSessionFixture) *runnerLedgerPreflightEvidenceFake {
	t.Helper()
	bindings, err := base.candidate.verifiedRun.currentDecision.decision.runnerProjectionBindings()
	if err != nil {
		t.Fatal(err)
	}
	facts, err := buildHistoricalVerificationFacts(base.bundle, bindings)
	if err != nil {
		t.Fatal(err)
	}
	generation := base.evidence.active.identity
	_, schema, err := buildBrandNewRecoveryWitness(base.candidate, generation, facts)
	if err != nil {
		t.Fatal(err)
	}
	return &runnerLedgerPreflightEvidenceFake{
		runnerEvidenceSessionFake: base.evidence,
		schema:                    schema,
		recovery:                  cloneRecoverySnapshot(base.evidence.snapshot),
		sessionDigest:             digestRaw(testDigest("runner-ledger-preflight-test-session")),
		journalDigest:             digestRaw(testDigest("runner-ledger-preflight-test-journal")),
		successBindErrAt:          map[int]error{},
	}
}

func (evidence *runnerLedgerPreflightEvidenceFake) runnerLedgerPreflightClaimBinderSealed() {}

func (evidence *runnerLedgerPreflightEvidenceFake) runnerLedgerEntryAdmissionClaimBinderSealed() {}

func (evidence *runnerLedgerPreflightEvidenceFake) runnerLedgerEntryExecutionAdmissionClaimBinderSealed() {
}

func (evidence *runnerLedgerPreflightEvidenceFake) runnerLedgerRecoveryAdmissionClaimBinderSealed() {}

func (evidence *runnerLedgerPreflightEvidenceFake) bindRunnerLedgerPreflightClaim(ctx context.Context, request runnerLedgerPreflightClaimRequest) (*runnerLedgerPreflightClaim, error) {
	evidence.mu.Lock()
	defer evidence.mu.Unlock()
	evidence.bindCalls++
	if evidence.bindErr != nil {
		return nil, evidence.bindErr
	}
	if evidence.runnerEvidenceSessionFake == nil || evidence.closed || request.candidate.binding != evidence.candidate.binding {
		return nil, fail(CodeEvidenceRecoveryRequired, "runner-ledger-preflight-test-bind", "test evidence is unavailable", nil)
	}
	claim, err := bindRunnerLedgerPreflightClaimFromEvidence(ctx, request, evidence.factsLocked())
	if err == nil && evidence.afterBind != nil {
		afterBind := evidence.afterBind
		evidence.afterBind = nil
		afterBind()
	}
	return claim, err
}

func (evidence *runnerLedgerPreflightEvidenceFake) consumeRunnerLedgerPreflightClaim(ctx context.Context, claim *runnerLedgerPreflightClaim, candidate OwnedCurrentCandidate) (runnerLedgerPreflightDispatch, error) {
	evidence.mu.Lock()
	defer evidence.mu.Unlock()
	evidence.consumeCalls++
	if evidence.consumeErr != nil {
		return runnerLedgerPreflightDispatch{}, evidence.consumeErr
	}
	if evidence.mutateBeforeConsume != nil {
		mutate := evidence.mutateBeforeConsume
		evidence.mutateBeforeConsume = nil
		mutate(evidence)
	}
	if evidence.runnerEvidenceSessionFake == nil || evidence.closed || candidate.binding != evidence.candidate.binding {
		return runnerLedgerPreflightDispatch{}, fail(CodeEvidenceRecoveryRequired, "runner-ledger-preflight-test-claim", "test evidence is unavailable", nil)
	}
	dispatch, err := consumeRunnerLedgerPreflightClaimFromEvidence(ctx, claim, candidate, evidence.factsLocked())
	if err == nil && evidence.afterConsume != nil {
		afterConsume := evidence.afterConsume
		evidence.afterConsume = nil
		afterConsume()
	}
	return dispatch, err
}

func (evidence *runnerLedgerPreflightEvidenceFake) factsLocked() runnerLedgerPreflightEvidenceFacts {
	generation := evidence.active.identity
	executionDigest := Digest("")
	if evidence.active.recoveryExecutionBindings != nil {
		executionDigest = evidence.active.recoveryExecutionBindings.digest
	}
	return runnerLedgerPreflightEvidenceFacts{
		binder: evidence, candidateBinding: evidence.candidate.binding, generation: generation,
		activeKind: evidence.active.kind, executionDigest: executionDigest,
		schema: cloneGenerationJournalSchema(evidence.schema), recovery: cloneRecoverySnapshot(evidence.recovery),
		schemaDigest:   generationJournalSchemaDigest(evidence.schema, generation),
		recoveryDigest: generationJournalRecoveryDigest(evidence.recovery),
		sessionDigest:  evidence.sessionDigest, journalDigest: evidence.journalDigest,
	}
}

func (evidence *runnerLedgerPreflightEvidenceFake) bindRunnerLedgerEntryAdmissionClaim(ctx context.Context, request runnerLedgerEntryAdmissionClaimRequest) (*runnerLedgerEntryAdmissionClaim, error) {
	evidence.mu.Lock()
	defer evidence.mu.Unlock()
	evidence.entryBindCalls++
	if evidence.entryBindErr != nil {
		return nil, evidence.entryBindErr
	}
	if evidence.runnerEvidenceSessionFake == nil || evidence.closed || request.candidate.binding != evidence.candidate.binding {
		return nil, fail(CodeEvidenceRecoveryRequired, "runner-ledger-entry-admission-test-bind", "test evidence is unavailable", nil)
	}
	claim, err := bindRunnerLedgerEntryAdmissionClaimFromEvidence(ctx, request, evidence.entryFactsLocked())
	if err == nil && evidence.afterEntryBind != nil {
		after := evidence.afterEntryBind
		evidence.afterEntryBind = nil
		after()
	}
	return claim, err
}

func (evidence *runnerLedgerPreflightEvidenceFake) consumeRunnerLedgerEntryAdmissionClaim(ctx context.Context, claim *runnerLedgerEntryAdmissionClaim, candidate OwnedCurrentCandidate) (runnerLedgerEntryAdmissionEvidenceBoundary, error) {
	evidence.mu.Lock()
	defer evidence.mu.Unlock()
	evidence.entryConsumeCalls++
	if evidence.entryConsumeErr != nil {
		return runnerLedgerEntryAdmissionEvidenceBoundary{}, evidence.entryConsumeErr
	}
	if evidence.mutateBeforeEntryConsume != nil {
		mutate := evidence.mutateBeforeEntryConsume
		evidence.mutateBeforeEntryConsume = nil
		mutate(evidence)
	}
	if evidence.runnerEvidenceSessionFake == nil || evidence.closed || candidate.binding != evidence.candidate.binding {
		return runnerLedgerEntryAdmissionEvidenceBoundary{}, fail(CodeEvidenceRecoveryRequired, "runner-ledger-entry-admission-test-claim", "test evidence is unavailable", nil)
	}
	boundary, err := consumeRunnerLedgerEntryAdmissionClaimFromEvidence(ctx, claim, candidate, evidence.entryFactsLocked())
	if err == nil && evidence.afterEntryConsume != nil {
		after := evidence.afterEntryConsume
		evidence.afterEntryConsume = nil
		after()
	}
	return boundary, err
}

func (evidence *runnerLedgerPreflightEvidenceFake) entryFactsLocked() runnerLedgerEntryAdmissionEvidenceFacts {
	generation := evidence.active.identity
	return runnerLedgerEntryAdmissionEvidenceFacts{
		binder: evidence, candidateBinding: evidence.candidate.binding, generation: generation,
		schema: cloneGenerationJournalSchema(evidence.schema), recovery: cloneRecoverySnapshot(evidence.recovery),
		schemaDigest:   generationJournalSchemaDigest(evidence.schema, generation),
		recoveryDigest: generationJournalRecoveryDigest(evidence.recovery),
		sessionDigest:  evidence.sessionDigest, journalDigest: evidence.journalDigest,
	}
}

func (evidence *runnerLedgerPreflightEvidenceFake) bindRunnerLedgerEntryExecutionAdmissionClaim(ctx context.Context, request runnerLedgerEntryExecutionAdmissionClaimRequest) (*runnerLedgerEntryExecutionAdmissionClaim, error) {
	evidence.mu.Lock()
	defer evidence.mu.Unlock()
	evidence.executionBindCalls++
	if evidence.executionBindErr != nil {
		return nil, evidence.executionBindErr
	}
	if evidence.runnerEvidenceSessionFake == nil || evidence.closed || request.candidate.binding != evidence.candidate.binding {
		return nil, fail(CodeEvidenceRecoveryRequired, "runner-ledger-entry-execution-admission-test-bind", "test evidence is unavailable", nil)
	}
	claim, err := bindRunnerLedgerEntryExecutionAdmissionClaimFromEvidence(ctx, request, evidence.executionFactsLocked())
	if err == nil && evidence.afterExecutionBind != nil {
		after := evidence.afterExecutionBind
		evidence.afterExecutionBind = nil
		after()
	}
	return claim, err
}

func (evidence *runnerLedgerPreflightEvidenceFake) consumeRunnerLedgerEntryExecutionAdmissionClaim(ctx context.Context, claim *runnerLedgerEntryExecutionAdmissionClaim, candidate OwnedCurrentCandidate) (runnerLedgerEntryExecutionAdmissionEvidenceBoundary, error) {
	evidence.mu.Lock()
	defer evidence.mu.Unlock()
	evidence.executionConsumeCalls++
	if evidence.executionConsumeErr != nil {
		return runnerLedgerEntryExecutionAdmissionEvidenceBoundary{}, evidence.executionConsumeErr
	}
	if evidence.mutateBeforeExecutionConsume != nil {
		mutate := evidence.mutateBeforeExecutionConsume
		evidence.mutateBeforeExecutionConsume = nil
		mutate(evidence)
	}
	if evidence.runnerEvidenceSessionFake == nil || evidence.closed || candidate.binding != evidence.candidate.binding {
		return runnerLedgerEntryExecutionAdmissionEvidenceBoundary{}, fail(CodeEvidenceRecoveryRequired, "runner-ledger-entry-execution-admission-test-claim", "test evidence is unavailable", nil)
	}
	boundary, err := consumeRunnerLedgerEntryExecutionAdmissionClaimFromEvidence(ctx, claim, candidate, evidence.executionFactsLocked())
	if err == nil && evidence.afterExecutionConsume != nil {
		after := evidence.afterExecutionConsume
		evidence.afterExecutionConsume = nil
		after()
	}
	return boundary, err
}

func (evidence *runnerLedgerPreflightEvidenceFake) executionFactsLocked() runnerLedgerEntryExecutionAdmissionEvidenceFacts {
	generation := evidence.active.identity
	return runnerLedgerEntryExecutionAdmissionEvidenceFacts{
		binder: evidence, candidateBinding: evidence.candidate.binding, generation: generation,
		schema: cloneGenerationJournalSchema(evidence.schema), recovery: cloneRecoverySnapshot(evidence.recovery),
		schemaDigest:   generationJournalSchemaDigest(evidence.schema, generation),
		recoveryDigest: generationJournalRecoveryDigest(evidence.recovery),
		sessionDigest:  evidence.sessionDigest, journalDigest: evidence.journalDigest,
	}
}

func (evidence *runnerLedgerPreflightEvidenceFake) bindRunnerLedgerRecoveryAdmissionClaim(ctx context.Context, request runnerLedgerRecoveryAdmissionClaimRequest) (*runnerLedgerRecoveryAdmissionClaim, error) {
	evidence.mu.Lock()
	defer evidence.mu.Unlock()
	evidence.recoveryBindCalls++
	if evidence.recoveryBindErr != nil {
		return nil, evidence.recoveryBindErr
	}
	if evidence.runnerEvidenceSessionFake == nil || evidence.closed || request.candidate.binding != evidence.candidate.binding {
		return nil, fail(CodeEvidenceRecoveryRequired, "runner-ledger-recovery-admission-test-bind", "test evidence is unavailable", nil)
	}
	claim, err := bindRunnerLedgerRecoveryAdmissionClaimFromEvidence(ctx, request, evidence.recoveryFactsLocked())
	if err == nil && evidence.afterRecoveryBind != nil {
		after := evidence.afterRecoveryBind
		evidence.afterRecoveryBind = nil
		after()
	}
	return claim, err
}

func (evidence *runnerLedgerPreflightEvidenceFake) consumeRunnerLedgerRecoveryAdmissionClaim(ctx context.Context, claim *runnerLedgerRecoveryAdmissionClaim, candidate OwnedCurrentCandidate) (runnerLedgerRecoveryAdmissionEvidenceBoundary, error) {
	evidence.mu.Lock()
	defer evidence.mu.Unlock()
	evidence.recoveryConsumeCalls++
	if evidence.recoveryConsumeErr != nil {
		return runnerLedgerRecoveryAdmissionEvidenceBoundary{}, evidence.recoveryConsumeErr
	}
	if evidence.mutateBeforeRecoveryConsume != nil {
		mutate := evidence.mutateBeforeRecoveryConsume
		evidence.mutateBeforeRecoveryConsume = nil
		mutate(evidence)
	}
	if evidence.runnerEvidenceSessionFake == nil || evidence.closed || candidate.binding != evidence.candidate.binding {
		return runnerLedgerRecoveryAdmissionEvidenceBoundary{}, fail(CodeEvidenceRecoveryRequired, "runner-ledger-recovery-admission-test-claim", "test evidence is unavailable", nil)
	}
	boundary, err := consumeRunnerLedgerRecoveryAdmissionClaimFromEvidence(ctx, claim, candidate, evidence.recoveryFactsLocked())
	if err == nil && evidence.afterRecoveryConsume != nil {
		after := evidence.afterRecoveryConsume
		evidence.afterRecoveryConsume = nil
		after()
	}
	return boundary, err
}

func (evidence *runnerLedgerPreflightEvidenceFake) recoveryFactsLocked() runnerLedgerRecoveryEvidenceFacts {
	generation := evidence.active.identity
	return runnerLedgerRecoveryEvidenceFacts{
		binder: evidence, candidateBinding: evidence.candidate.binding, generation: generation,
		schema: cloneGenerationJournalSchema(evidence.schema), recovery: cloneRecoverySnapshot(evidence.recovery),
		schemaDigest:   generationJournalSchemaDigest(evidence.schema, generation),
		recoveryDigest: generationJournalRecoveryDigest(evidence.recovery),
		stateCanonical: digestRaw(testDigest("runner-ledger-recovery-test-state")),
		sessionDigest:  evidence.sessionDigest, journalDigest: evidence.journalDigest,
		fullSet:             digestRaw(testDigest("runner-ledger-recovery-test-full-set")),
		transcriptCanonical: digestRaw(testDigest("runner-ledger-recovery-test-transcript")),
		target:              digestRaw(generation.executionLineageDigest), indexRecords: 2,
		indexTail: testDigest("runner-ledger-recovery-test-index-tail"),
	}
}

func (evidence *runnerLedgerPreflightEvidenceFake) Close(ctx context.Context) error {
	if evidence == nil {
		return fail(CodeEvidenceJournalFailed, "runner-ledger-preflight-test-close", "test evidence is unavailable", nil)
	}
	evidence.mu.Lock()
	revokeRunnerLedgerPreflightClaims(evidence)
	revokeRunnerLedgerEntryAdmissionClaims(evidence)
	revokeRunnerLedgerEntryExecutionAdmissionClaims(evidence)
	revokeRunnerLedgerRecoveryAdmissionClaims(evidence)
	err := evidence.runnerEvidenceSessionFake.Close(ctx)
	evidence.mu.Unlock()
	return err
}

type runnerLedgerPreflightServiceFixture struct {
	kernel   runnerLedgerCatalogPreflightFixture
	evidence *runnerLedgerPreflightEvidenceFake
	closed   bool
}

func newRunnerLedgerPreflightServiceFixture(t *testing.T) *runnerLedgerPreflightServiceFixture {
	t.Helper()
	raw, decision := buildExactTwoMigrationAdmissionRuntime(t)
	kernel := newRunnerLedgerCatalogPreflightFixtureFromRuntime(t, raw, decision)
	return &runnerLedgerPreflightServiceFixture{kernel: kernel, evidence: newRunnerLedgerPreflightEvidenceFake(t, kernel.base)}
}

func (fixture *runnerLedgerPreflightServiceFixture) configure(t *testing.T, disposition runnerLedgerPreflightDisposition, state RecoveryState, action RecoveryAction, major uint16) {
	t.Helper()
	entries := fixture.kernel.base.bundle.Manifest.SchemaBundle.Migrations
	prefixLength := 0
	switch disposition {
	case runnerLedgerPreflightEmptyBrandNew:
	case runnerLedgerPreflightPartialNextEntry, runnerLedgerPreflightPartialRetryOrRecovery:
		prefixLength = 1
	case runnerLedgerPreflightCompleteReturnSuccess:
		prefixLength = len(entries)
	default:
		t.Fatalf("unsupported disposition %s", disposition)
	}
	databaseRows := make([]LedgerRow, 0, prefixLength)
	for index := 0; index < prefixLength; index++ {
		databaseRows = append(databaseRows, ledgerRowFor(entries[index], fixture.kernel.base.bundle.Manifest.SchemaBundleDigest))
	}
	fixture.kernel.database.ledgerRowsByRead = [][]LedgerRow{cloneProjectionValue(databaseRows), cloneProjectionValue(databaseRows)}
	for _, phase := range []AuthorityPhase{AuthorityPhaseConnectedSession, AuthorityPhaseMigrationRole} {
		selected := major
		fixture.kernel.database.snapshotMetadataMutate[phase] = func(metadata *SnapshotMetadata) {
			metadata.PostgresMajor = selected
			metadata.ServerVersionNum = uint32(selected) * 10_000
		}
	}

	fixture.evidence.mu.Lock()
	fixture.evidence.schema.durableObservedLedgerPrefix = cloneProjectionValue(fixture.evidence.schema.signedExpectedLedgerRows[:prefixLength])
	digest, err := LedgerPrefixDigest(fixture.evidence.schema.durableObservedLedgerPrefix)
	if err != nil {
		fixture.evidence.mu.Unlock()
		t.Fatal(err)
	}
	fixture.evidence.schema.durableObservedLedgerDigest = digest
	recovery := cloneRecoverySnapshot(fixture.evidence.runnerEvidenceSessionFake.snapshot)
	recovery.state, recovery.nextPermittedAction = state, action
	recovery.migrationID, recovery.attemptIndex = nil, nil
	first := entries[0].ID
	switch disposition {
	case runnerLedgerPreflightEmptyBrandNew:
		if state == RecoveryBrandNewInherited && action == RecoveryBeginNextAttempt {
			recovery.migrationID, recovery.attemptIndex = &first, uint32Pointer(2)
		}
	case runnerLedgerPreflightPartialNextEntry:
		if len(entries) < 2 {
			fixture.evidence.mu.Unlock()
			t.Fatal("partial next-entry fixture has no successor")
		}
		second := entries[1].ID
		if state == RecoveryBrandNewInherited {
			recovery.migrationID, recovery.attemptIndex = &second, uint32Pointer(1)
		} else {
			recovery.migrationID, recovery.attemptIndex = &first, uint32Pointer(1)
		}
	case runnerLedgerPreflightPartialRetryOrRecovery:
		if len(entries) < 2 {
			fixture.evidence.mu.Unlock()
			t.Fatal("partial retry fixture has no successor")
		}
		second := entries[1].ID
		if state == RecoveryBrandNewInherited && action == RecoveryBeginFirstAttempt {
			recovery.migrationID, recovery.attemptIndex = nil, nil
		} else {
			attempt := uint32(1)
			if state == RecoveryBrandNewInherited && action == RecoveryBeginNextAttempt {
				attempt = 2
			}
			recovery.migrationID, recovery.attemptIndex = &second, uint32Pointer(attempt)
		}
	case runnerLedgerPreflightCompleteReturnSuccess:
		if len(entries) < 2 {
			recovery.migrationID, recovery.attemptIndex = &first, uint32Pointer(1)
			break
		}
		second := entries[1].ID
		recovery.migrationID, recovery.attemptIndex = &second, uint32Pointer(1)
	}
	if action == RecoveryBeginNextAttempt {
		recovery.previousAttemptTerminalDigest = digestPointer(testDigest("runner-ledger-preflight-previous-terminal"))
	}
	fixture.evidence.recovery = recovery
	fixture.evidence.mu.Unlock()
}

func (fixture *runnerLedgerPreflightServiceFixture) close(t *testing.T) {
	t.Helper()
	if fixture == nil || fixture.closed {
		return
	}
	fixture.closed = true
	revokeRunnerLedgerPreflightClaims(fixture.evidence)
	revokeRunnerLedgerEntryAdmissionClaims(fixture.evidence)
	revokeRunnerLedgerEntryExecutionAdmissionClaims(fixture.evidence)
	revokeRunnerLedgerRecoveryAdmissionClaims(fixture.evidence)
	if fixture.kernel.base.database != nil && !fixture.kernel.base.database.closed {
		if err := closeRunnerDatabasePreflight(fixture.kernel.base.database, fixture.kernel.base.key, true, nil); err != nil {
			t.Fatal(err)
		}
	}
	if fixture.kernel.base.evidence.closed {
		if !revokeOwnedCurrentCandidate(fixture.kernel.base.candidate) {
			if _, live := verifiedEvidenceRunBindingRegistry.Load(fixture.kernel.base.candidate.binding); live {
				t.Fatal("candidate remained live after evidence close")
			}
		}
		return
	}
	if err := closeRunnerEvidenceOwnership(fixture.kernel.base.evidence, fixture.kernel.base.candidate); err != nil {
		t.Fatal(err)
	}
}

func uint32Pointer(value uint32) *uint32 { return &value }

func TestRunnerLedgerPreflightServiceDispatchesEveryGeneratedRecoveryPair(t *testing.T) {
	tests := []struct {
		name        string
		disposition runnerLedgerPreflightDisposition
		state       RecoveryState
		action      RecoveryAction
		kind        runnerLedgerPreflightDispatchKind
	}{
		{"complete", runnerLedgerPreflightCompleteReturnSuccess, RecoveryCompleted, RecoveryReturnSuccess, runnerLedgerPreflightDispatchReturnSuccess},
		{"empty-brand-new", runnerLedgerPreflightEmptyBrandNew, RecoveryBrandNew, RecoveryBeginFirstAttempt, runnerLedgerPreflightDispatchEntry},
		{"empty-inherited-first", runnerLedgerPreflightEmptyBrandNew, RecoveryBrandNewInherited, RecoveryBeginFirstAttempt, runnerLedgerPreflightDispatchEntry},
		{"empty-inherited-retry", runnerLedgerPreflightEmptyBrandNew, RecoveryBrandNewInherited, RecoveryBeginNextAttempt, runnerLedgerPreflightDispatchEntry},
		{"partial-next-inherited", runnerLedgerPreflightPartialNextEntry, RecoveryBrandNewInherited, RecoveryBeginFirstAttemptNextEntry, runnerLedgerPreflightDispatchEntry},
		{"partial-next-terminal", runnerLedgerPreflightPartialNextEntry, RecoveryTerminal, RecoveryBeginFirstAttemptNextEntry, runnerLedgerPreflightDispatchEntry},
		{"partial-inherited-first", runnerLedgerPreflightPartialRetryOrRecovery, RecoveryBrandNewInherited, RecoveryBeginFirstAttempt, runnerLedgerPreflightDispatchRecovery},
		{"partial-inherited-retry", runnerLedgerPreflightPartialRetryOrRecovery, RecoveryBrandNewInherited, RecoveryBeginNextAttempt, runnerLedgerPreflightDispatchRecovery},
		{"partial-intent-retry", runnerLedgerPreflightPartialRetryOrRecovery, RecoveryDanglingStatementIntent, RecoveryAppendAbortedRetryable, runnerLedgerPreflightDispatchRecovery},
		{"partial-intent-terminal", runnerLedgerPreflightPartialRetryOrRecovery, RecoveryDanglingStatementIntent, RecoveryAppendAbortedTerminal, runnerLedgerPreflightDispatchRecovery},
		{"partial-intermediate-retry", runnerLedgerPreflightPartialRetryOrRecovery, RecoveryDanglingIntermediate, RecoveryAppendAbortedRetryable, runnerLedgerPreflightDispatchRecovery},
		{"partial-intermediate-terminal", runnerLedgerPreflightPartialRetryOrRecovery, RecoveryDanglingIntermediate, RecoveryAppendAbortedTerminal, runnerLedgerPreflightDispatchRecovery},
		{"partial-commit", runnerLedgerPreflightPartialRetryOrRecovery, RecoveryDanglingCommitIntent, RecoveryReconcileCommit, runnerLedgerPreflightDispatchRecovery},
		{"partial-ambiguous", runnerLedgerPreflightPartialRetryOrRecovery, RecoveryAmbiguousUnresolved, RecoveryReconcileCommit, runnerLedgerPreflightDispatchRecovery},
		{"partial-terminal-retry", runnerLedgerPreflightPartialRetryOrRecovery, RecoveryTerminal, RecoveryBeginNextAttempt, runnerLedgerPreflightDispatchRecovery},
		{"partial-terminal-failure", runnerLedgerPreflightPartialRetryOrRecovery, RecoveryTerminal, RecoveryReturnFailure, runnerLedgerPreflightDispatchRecovery},
		{"partial-divergent", runnerLedgerPreflightPartialRetryOrRecovery, RecoveryDivergent, RecoveryReturnFailure, runnerLedgerPreflightDispatchRecovery},
	}
	if len(tests) != generatedRunnerLedgerPreflightRecoveryPairCount {
		t.Fatalf("matrix cases=%d want=%d", len(tests), generatedRunnerLedgerPreflightRecoveryPairCount)
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if test.action == RecoveryReconcileCommit {
				fixture := newRunnerLedgerRecoveryReconciliationFixture(t, test.state, runnerLedgerReconciliationExactPending, 16)
				defer fixture.close(t)
				if !fixture.fact.valid() || fixture.fact.dispatch.kind != test.kind ||
					fixture.fact.dispatch.fact.disposition != test.disposition || fixture.fact.dispatch.fact.recovery == nil ||
					fixture.fact.dispatch.fact.recovery.State != test.state || fixture.fact.dispatch.fact.recovery.Action != test.action {
					t.Fatalf("reconciliation dispatch=%+v", fixture.fact.dispatch)
				}
				return
			}
			fixture := newRunnerLedgerPreflightServiceFixture(t)
			defer fixture.close(t)
			fixture.configure(t, test.disposition, test.state, test.action, 16)
			claim, err := fixture.kernel.runner.prepareRunnerLedgerPreflightClaim(
				context.Background(), "test-only", fixture.kernel.base.bundle, fixture.kernel.base.plans,
				fixture.evidence, fixture.kernel.base.candidate,
			)
			if err != nil || !validRunnerLedgerPreflightClaim(claim, fixture.evidence, fixture.kernel.base.candidate.binding) {
				t.Fatalf("claim=%+v err=%v", claim, err)
			}
			dispatch, err := fixture.kernel.runner.claimRunnerLedgerPreflightDispatch(context.Background(), fixture.evidence, fixture.kernel.base.candidate, claim)
			if err != nil || !dispatch.valid() || dispatch.kind != test.kind || dispatch.fact.disposition != test.disposition ||
				dispatch.fact.recovery == nil || dispatch.fact.recovery.State != test.state || dispatch.fact.recovery.Action != test.action {
				t.Fatalf("dispatch=%+v err=%v", dispatch, err)
			}
			if test.kind == runnerLedgerPreflightDispatchEntry {
				prefixLength := int(dispatch.fact.orderedMigrationPrefixLength)
				want, wantErr := runnerLedgerPreflightNextEntryFromSchema(fixture.evidence.schema, prefixLength)
				if wantErr != nil || dispatch.fact.nextEntry == nil || *dispatch.fact.nextEntry != want {
					t.Fatalf("next=%+v want=%+v err=%v", dispatch.fact.nextEntry, want, wantErr)
				}
			} else if dispatch.fact.nextEntry != nil {
				t.Fatalf("non-entry dispatch retained next entry %+v", dispatch.fact.nextEntry)
			}
			if second, secondErr := fixture.kernel.runner.claimRunnerLedgerPreflightDispatch(context.Background(), fixture.evidence, fixture.kernel.base.candidate, claim); second.valid() || !IsCode(secondErr, CodeEvidenceRecoveryRequired) {
				t.Fatalf("claim reuse dispatch=%+v err=%v", second, secondErr)
			}
			assertRunnerLedgerPreflightReadOnlyLifecycle(t, fixture)
		})
	}
}

func TestRunnerLedgerPreflightServicePostgresMajorAndStateMatrixIsReadOnly(t *testing.T) {
	for _, major := range []uint16{15, 16, 17} {
		for _, disposition := range []runnerLedgerPreflightDisposition{
			runnerLedgerPreflightEmptyBrandNew,
			runnerLedgerPreflightPartialNextEntry,
			runnerLedgerPreflightPartialRetryOrRecovery,
			runnerLedgerPreflightCompleteReturnSuccess,
		} {
			name := fmt.Sprintf("pg%d-%s", major, disposition)
			t.Run(name, func(t *testing.T) {
				fixture := newRunnerLedgerPreflightServiceFixture(t)
				defer fixture.close(t)
				state, action := RecoveryBrandNew, RecoveryBeginFirstAttempt
				switch disposition {
				case runnerLedgerPreflightPartialNextEntry:
					state, action = RecoveryTerminal, RecoveryBeginFirstAttemptNextEntry
				case runnerLedgerPreflightPartialRetryOrRecovery:
					state, action = RecoveryTerminal, RecoveryBeginNextAttempt
				case runnerLedgerPreflightCompleteReturnSuccess:
					state, action = RecoveryCompleted, RecoveryReturnSuccess
				}
				fixture.configure(t, disposition, state, action, major)
				claim, err := fixture.kernel.runner.prepareRunnerLedgerPreflightClaim(context.Background(), "test-only", fixture.kernel.base.bundle, fixture.kernel.base.plans, fixture.evidence, fixture.kernel.base.candidate)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := fixture.kernel.runner.claimRunnerLedgerPreflightDispatch(context.Background(), fixture.evidence, fixture.kernel.base.candidate, claim); err != nil {
					t.Fatal(err)
				}
				assertRunnerLedgerPreflightReadOnlyLifecycle(t, fixture)
			})
		}
	}
}

func TestRunnerLedgerPreflightClaimRejectsCopyLiteralReuseAndEvidenceDrift(t *testing.T) {
	t.Run("copy-literal-and-pre-cancel", func(t *testing.T) {
		fixture := newRunnerLedgerPreflightServiceFixture(t)
		defer fixture.close(t)
		fixture.configure(t, runnerLedgerPreflightPartialRetryOrRecovery, RecoveryTerminal, RecoveryBeginNextAttempt, 16)
		claim, err := fixture.kernel.runner.prepareRunnerLedgerPreflightClaim(context.Background(), "test-only", fixture.kernel.base.bundle, fixture.kernel.base.plans, fixture.evidence, fixture.kernel.base.candidate)
		if err != nil {
			t.Fatal(err)
		}
		copyValue := *claim
		if dispatch, err := fixture.kernel.runner.claimRunnerLedgerPreflightDispatch(context.Background(), fixture.evidence, fixture.kernel.base.candidate, &copyValue); dispatch.valid() || !IsCode(err, CodeEvidenceRecoveryRequired) {
			t.Fatalf("copy dispatch=%+v err=%v", dispatch, err)
		}
		if dispatch, err := fixture.kernel.runner.claimRunnerLedgerPreflightDispatch(context.Background(), fixture.evidence, fixture.kernel.base.candidate, &runnerLedgerPreflightClaim{}); dispatch.valid() || !IsCode(err, CodeEvidenceRecoveryRequired) {
			t.Fatalf("literal dispatch=%+v err=%v", dispatch, err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if dispatch, err := fixture.kernel.runner.claimRunnerLedgerPreflightDispatch(ctx, fixture.evidence, fixture.kernel.base.candidate, claim); dispatch.valid() || !IsCode(err, CodeContextCanceled) || !validRunnerLedgerPreflightClaim(claim, fixture.evidence, fixture.kernel.base.candidate.binding) {
			t.Fatalf("canceled dispatch=%+v err=%v live=%v", dispatch, err, validRunnerLedgerPreflightClaim(claim, fixture.evidence, fixture.kernel.base.candidate.binding))
		}
		if dispatch, err := fixture.kernel.runner.claimRunnerLedgerPreflightDispatch(context.Background(), fixture.evidence, fixture.kernel.base.candidate, claim); err != nil || !dispatch.valid() {
			t.Fatalf("original dispatch=%+v err=%v", dispatch, err)
		}
	})

	t.Run("recovery-drift", func(t *testing.T) {
		fixture := newRunnerLedgerPreflightServiceFixture(t)
		defer fixture.close(t)
		fixture.configure(t, runnerLedgerPreflightPartialRetryOrRecovery, RecoveryTerminal, RecoveryBeginNextAttempt, 16)
		claim, err := fixture.kernel.runner.prepareRunnerLedgerPreflightClaim(context.Background(), "test-only", fixture.kernel.base.bundle, fixture.kernel.base.plans, fixture.evidence, fixture.kernel.base.candidate)
		if err != nil {
			t.Fatal(err)
		}
		fixture.evidence.mutateBeforeConsume = func(evidence *runnerLedgerPreflightEvidenceFake) {
			tail := testDigest("runner-ledger-preflight-drifted-tail")
			evidence.recovery.tailDigest = tail
			evidence.recovery.cursor.previousRecordDigest = &tail
		}
		dispatch, err := fixture.kernel.runner.claimRunnerLedgerPreflightDispatch(context.Background(), fixture.evidence, fixture.kernel.base.candidate, claim)
		if dispatch.valid() || !IsCode(err, CodeEvidenceRecoveryRequired) || !claim.consumed.Load() {
			t.Fatalf("drift dispatch=%+v err=%v consumed=%v", dispatch, err, claim.consumed.Load())
		}
	})

	t.Run("owned-claim-drift-revokes", func(t *testing.T) {
		fixture := newRunnerLedgerPreflightServiceFixture(t)
		defer fixture.close(t)
		fixture.configure(t, runnerLedgerPreflightPartialRetryOrRecovery, RecoveryTerminal, RecoveryBeginNextAttempt, 16)
		claim, err := fixture.kernel.runner.prepareRunnerLedgerPreflightClaim(context.Background(), "test-only", fixture.kernel.base.bundle, fixture.kernel.base.plans, fixture.evidence, fixture.kernel.base.candidate)
		if err != nil {
			t.Fatal(err)
		}
		claim.dispatch.kind = runnerLedgerPreflightDispatchKind("forged")
		if dispatch, consumeErr := fixture.kernel.runner.claimRunnerLedgerPreflightDispatch(context.Background(), fixture.evidence, fixture.kernel.base.candidate, claim); dispatch.valid() || !IsCode(consumeErr, CodeEvidenceRecoveryRequired) || !claim.consumed.Load() {
			t.Fatalf("drifted claim dispatch=%+v err=%v consumed=%v", dispatch, consumeErr, claim.consumed.Load())
		}
		if _, live := runnerLedgerPreflightClaimRegistry.Load(claim); live {
			t.Fatal("drifted claim remained registered")
		}
		if current, live := runnerLedgerPreflightClaimByEvidenceBinder.Load(fixture.evidence); live && current == claim {
			t.Fatal("drifted claim continued to pin its evidence binder")
		}
	})
}

func TestRunnerLedgerPreflightServiceRejectsRecoveryIdentityDrift(t *testing.T) {
	tests := []struct {
		name        string
		disposition runnerLedgerPreflightDisposition
		state       RecoveryState
		action      RecoveryAction
		mutate      func(*runnerLedgerPreflightServiceFixture)
	}{
		{
			name: "empty-inherited-first-with-identity", disposition: runnerLedgerPreflightEmptyBrandNew,
			state: RecoveryBrandNewInherited, action: RecoveryBeginFirstAttempt,
			mutate: func(fixture *runnerLedgerPreflightServiceFixture) {
				migration := fixture.kernel.base.bundle.Manifest.SchemaBundle.Migrations[0].ID
				fixture.evidence.recovery.migrationID, fixture.evidence.recovery.attemptIndex = &migration, uint32Pointer(1)
			},
		},
		{
			name: "empty-inherited-retry-attempt-one", disposition: runnerLedgerPreflightEmptyBrandNew,
			state: RecoveryBrandNewInherited, action: RecoveryBeginNextAttempt,
			mutate: func(fixture *runnerLedgerPreflightServiceFixture) {
				fixture.evidence.recovery.attemptIndex = uint32Pointer(1)
			},
		},
		{
			name: "partial-inherited-first-with-identity", disposition: runnerLedgerPreflightPartialRetryOrRecovery,
			state: RecoveryBrandNewInherited, action: RecoveryBeginFirstAttempt,
			mutate: func(fixture *runnerLedgerPreflightServiceFixture) {
				migration := fixture.kernel.base.bundle.Manifest.SchemaBundle.Migrations[1].ID
				fixture.evidence.recovery.migrationID, fixture.evidence.recovery.attemptIndex = &migration, uint32Pointer(1)
			},
		},
		{
			name: "partial-inherited-retry-attempt-one", disposition: runnerLedgerPreflightPartialRetryOrRecovery,
			state: RecoveryBrandNewInherited, action: RecoveryBeginNextAttempt,
			mutate: func(fixture *runnerLedgerPreflightServiceFixture) {
				fixture.evidence.recovery.attemptIndex = uint32Pointer(1)
			},
		},
		{
			name: "partial-next-wrong-migration", disposition: runnerLedgerPreflightPartialNextEntry,
			state: RecoveryTerminal, action: RecoveryBeginFirstAttemptNextEntry,
			mutate: func(fixture *runnerLedgerPreflightServiceFixture) {
				migration := fixture.kernel.base.bundle.Manifest.SchemaBundle.Migrations[1].ID
				fixture.evidence.recovery.migrationID = &migration
			},
		},
		{
			name: "complete-wrong-head", disposition: runnerLedgerPreflightCompleteReturnSuccess,
			state: RecoveryCompleted, action: RecoveryReturnSuccess,
			mutate: func(fixture *runnerLedgerPreflightServiceFixture) {
				migration := "000999"
				fixture.evidence.recovery.migrationID = &migration
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newRunnerLedgerPreflightServiceFixture(t)
			defer fixture.close(t)
			fixture.configure(t, test.disposition, test.state, test.action, 16)
			fixture.evidence.mu.Lock()
			test.mutate(fixture)
			fixture.evidence.mu.Unlock()
			claim, err := fixture.kernel.runner.prepareRunnerLedgerPreflightClaim(context.Background(), "test-only", fixture.kernel.base.bundle, fixture.kernel.base.plans, fixture.evidence, fixture.kernel.base.candidate)
			if claim != nil || !IsCode(err, CodeEvidenceRecoveryRequired) {
				t.Fatalf("claim=%+v err=%v", claim, err)
			}
			assertRunnerLedgerPreflightReadOnlyLifecycle(t, fixture)
		})
	}
}

func TestRunnerLedgerPreflightServiceCorruptPrecedenceAndNoBinderFailure(t *testing.T) {
	t.Run("ledger-evidence-drift-before-recovery-pair", func(t *testing.T) {
		fixture := newRunnerLedgerPreflightServiceFixture(t)
		defer fixture.close(t)
		fixture.configure(t, runnerLedgerPreflightPartialRetryOrRecovery, RecoveryCompleted, RecoveryReturnSuccess, 16)
		fixture.evidence.mu.Lock()
		fixture.evidence.schema.durableObservedLedgerPrefix[0].MigrationName = "drifted-evidence-row"
		fixture.evidence.schema.durableObservedLedgerDigest, _ = LedgerPrefixDigest(fixture.evidence.schema.durableObservedLedgerPrefix)
		fixture.evidence.mu.Unlock()
		claim, err := fixture.kernel.runner.prepareRunnerLedgerPreflightClaim(context.Background(), "test-only", fixture.kernel.base.bundle, fixture.kernel.base.plans, fixture.evidence, fixture.kernel.base.candidate)
		if claim != nil || !IsCode(err, CodeEvidenceJournalCorrupt) {
			t.Fatalf("claim=%+v err=%v", claim, err)
		}
		assertRunnerLedgerPreflightReadOnlyLifecycle(t, fixture)
	})

	t.Run("complete-catalog-drift", func(t *testing.T) {
		fixture := newRunnerLedgerPreflightServiceFixture(t)
		defer fixture.close(t)
		fixture.configure(t, runnerLedgerPreflightCompleteReturnSuccess, RecoveryCompleted, RecoveryReturnSuccess, 16)
		fixture.evidence.mu.Lock()
		fixture.evidence.schema.finalCatalogDigest = testDigest("wrong-final-catalog")
		fixture.evidence.mu.Unlock()
		claim, err := fixture.kernel.runner.prepareRunnerLedgerPreflightClaim(context.Background(), "test-only", fixture.kernel.base.bundle, fixture.kernel.base.plans, fixture.evidence, fixture.kernel.base.candidate)
		if claim != nil || !IsCode(err, CodeEvidenceJournalCorrupt) {
			t.Fatalf("claim=%+v err=%v", claim, err)
		}
	})

	t.Run("evidence-without-binder", func(t *testing.T) {
		raw, decision := buildExactTwoMigrationAdmissionRuntime(t)
		fixture := newRunnerLedgerCatalogPreflightFixtureFromRuntime(t, raw, decision)
		defer fixture.close(t, nil)
		fixture.database.ledgerRowsByRead = [][]LedgerRow{{}, {}}
		claim, err := fixture.runner.prepareRunnerLedgerPreflightClaim(context.Background(), "test-only", fixture.base.bundle, fixture.base.plans, fixture.base.evidence, fixture.base.candidate)
		if claim != nil || !IsCode(err, CodeEvidenceRecoveryRequired) || fixture.database.beginCalls != 0 || fixture.database.backend.ledgerInsertCalls != 0 || !fixture.database.closed {
			t.Fatalf("claim=%+v err=%v database=%+v", claim, err, fixture.database)
		}
	})
}

func TestRunnerLedgerPreflightClaimCloseRevokesWithoutDispatch(t *testing.T) {
	fixture := newRunnerLedgerPreflightServiceFixture(t)
	defer fixture.close(t)
	fixture.configure(t, runnerLedgerPreflightEmptyBrandNew, RecoveryBrandNew, RecoveryBeginFirstAttempt, 16)
	claim, err := fixture.kernel.runner.prepareRunnerLedgerPreflightClaim(context.Background(), "test-only", fixture.kernel.base.bundle, fixture.kernel.base.plans, fixture.evidence, fixture.kernel.base.candidate)
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.evidence.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if dispatch, err := fixture.kernel.runner.claimRunnerLedgerPreflightDispatch(context.Background(), fixture.evidence, fixture.kernel.base.candidate, claim); dispatch.valid() || !IsCode(err, CodeEvidenceRecoveryRequired) || !claim.consumed.Load() {
		t.Fatalf("closed dispatch=%+v err=%v consumed=%v", dispatch, err, claim.consumed.Load())
	}
}

func TestRunnerLedgerPreflightClaimIsConsumedExactlyOnceUnderRace(t *testing.T) {
	fixture := newRunnerLedgerPreflightServiceFixture(t)
	defer fixture.close(t)
	fixture.configure(t, runnerLedgerPreflightPartialRetryOrRecovery, RecoveryTerminal, RecoveryBeginNextAttempt, 16)
	claim, err := fixture.kernel.runner.prepareRunnerLedgerPreflightClaim(context.Background(), "test-only", fixture.kernel.base.bundle, fixture.kernel.base.plans, fixture.evidence, fixture.kernel.base.candidate)
	if err != nil {
		t.Fatal(err)
	}
	type result struct {
		dispatch runnerLedgerPreflightDispatch
		err      error
	}
	results := make(chan result, 2)
	var ready sync.WaitGroup
	ready.Add(2)
	start := make(chan struct{})
	for index := 0; index < 2; index++ {
		go func() {
			ready.Done()
			<-start
			dispatch, consumeErr := fixture.kernel.runner.claimRunnerLedgerPreflightDispatch(context.Background(), fixture.evidence, fixture.kernel.base.candidate, claim)
			results <- result{dispatch: dispatch, err: consumeErr}
		}()
	}
	ready.Wait()
	close(start)
	successes, rejected := 0, 0
	for index := 0; index < 2; index++ {
		result := <-results
		switch {
		case result.err == nil && result.dispatch.valid():
			successes++
		case !result.dispatch.valid() && IsCode(result.err, CodeEvidenceRecoveryRequired):
			rejected++
		default:
			t.Fatalf("unexpected concurrent claim result: dispatch=%+v err=%v", result.dispatch, result.err)
		}
	}
	if successes != 1 || rejected != 1 || !claim.consumed.Load() {
		t.Fatalf("concurrent consumption successes=%d rejected=%d consumed=%v", successes, rejected, claim.consumed.Load())
	}
	assertRunnerLedgerPreflightReadOnlyLifecycle(t, fixture)
}

func TestRunnerLedgerPreflightServiceFailsBeforeClaimOnLockOrCloseAmbiguity(t *testing.T) {
	tests := []struct {
		name       string
		configure  func(*runnerLedgerPreflightServiceFixture)
		wantLocks  int
		wantUnlock int
		wantReads  int
	}{
		{
			name: "advisory-lock-failure",
			configure: func(fixture *runnerLedgerPreflightServiceFixture) {
				fixture.kernel.database.lockErr = fmt.Errorf("lock unavailable")
			},
			wantLocks: 1,
		},
		{
			name: "session-close-response-lost",
			configure: func(fixture *runnerLedgerPreflightServiceFixture) {
				fixture.kernel.database.closeErr = fmt.Errorf("close response lost")
			},
			wantLocks: 1, wantUnlock: 1, wantReads: 2,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newRunnerLedgerPreflightServiceFixture(t)
			defer fixture.close(t)
			fixture.configure(t, runnerLedgerPreflightEmptyBrandNew, RecoveryBrandNew, RecoveryBeginFirstAttempt, 16)
			test.configure(fixture)
			claim, err := fixture.kernel.runner.prepareRunnerLedgerPreflightClaim(context.Background(), "test-only", fixture.kernel.base.bundle, fixture.kernel.base.plans, fixture.evidence, fixture.kernel.base.candidate)
			if claim != nil || !IsCode(err, CodeTransactionBoundary) || fixture.evidence.bindCalls != 0 ||
				fixture.kernel.database.lockCalls != test.wantLocks || fixture.kernel.database.unlockCalls != test.wantUnlock ||
				fixture.kernel.database.ledgerReadCalls != test.wantReads || fixture.kernel.database.beginCalls != 0 ||
				fixture.kernel.database.backend.ledgerInsertCalls != 0 || fixture.kernel.database.backend.executeCalls != 0 ||
				fixture.kernel.database.backend.commitCalls != 0 || !fixture.kernel.database.closed {
				t.Fatalf("claim=%+v err=%v evidence=%+v database=%+v", claim, err, fixture.evidence, fixture.kernel.database)
			}
		})
	}
}

func TestRunnerLedgerPreflightServiceRejectsCrossBundleEvidenceAfterReadOnlyProjection(t *testing.T) {
	fixture := newRunnerLedgerPreflightServiceFixture(t)
	defer fixture.close(t)
	fixture.configure(t, runnerLedgerPreflightEmptyBrandNew, RecoveryBrandNew, RecoveryBeginFirstAttempt, 16)
	fixture.evidence.mu.Lock()
	fixture.evidence.active.identity.schemaBundleDigest = testDigest("foreign-runner-ledger-preflight-schema")
	fixture.evidence.mu.Unlock()
	claim, err := fixture.kernel.runner.prepareRunnerLedgerPreflightClaim(context.Background(), "test-only", fixture.kernel.base.bundle, fixture.kernel.base.plans, fixture.evidence, fixture.kernel.base.candidate)
	if claim != nil || !IsCode(err, CodeEvidenceRecoveryRequired) || fixture.evidence.bindCalls != 1 {
		t.Fatalf("claim=%+v err=%v bind_calls=%d", claim, err, fixture.evidence.bindCalls)
	}
	assertRunnerLedgerPreflightReadOnlyLifecycle(t, fixture)
}

func TestRunnerLedgerPreflightDispatchHasNoAuthorityOrForbiddenConsumer(t *testing.T) {
	dispatchType := reflect.TypeOf(runnerLedgerPreflightDispatch{})
	for index := 0; index < dispatchType.NumField(); index++ {
		field := dispatchType.Field(index)
		name := strings.ToLower(field.Name)
		for _, forbidden := range []string{"session", "transaction", "evidence", "receipt", "writer", "token", "lease", "artifact"} {
			if strings.Contains(name, forbidden) {
				t.Fatalf("ordinary dispatch retained forbidden field %s", field.Name)
			}
		}
		for _, contract := range []reflect.Type{
			reflect.TypeOf((*DatabaseSession)(nil)).Elem(), reflect.TypeOf((*MigrationTransaction)(nil)).Elem(),
			reflect.TypeOf((*EvidenceSession)(nil)).Elem(), reflect.TypeOf((*EvidenceJournal)(nil)).Elem(),
		} {
			if field.Type.Implements(contract) || reflect.PointerTo(field.Type).Implements(contract) {
				t.Fatalf("ordinary dispatch field %s implements authority %s", field.Name, contract)
			}
		}
	}

	forbiddenCalls := map[string]bool{
		"BeginMigration": true, "ExecuteStatement": true, "Commit": true, "Append": true, "Insert": true,
		"runCurrentSingleEntry": true, "prepareCurrentDatabaseSession": true, "bindRunnerPreparedCurrentSession": true,
		"ReserveAndActivateSuccessor": true, "transition": true,
	}
	file, err := parser.ParseFile(token.NewFileSet(), "runner_ledger_preflight_service.go", nil, 0)
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
			t.Fatalf("Slice C acquired forbidden call edge %s", name)
		}
		return true
	})

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	serviceCalls, kernelCalls, concreteBinders := 0, 0, 0
	bindHelperCalls, consumeHelperCalls := 0, 0
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		production, parseErr := parser.ParseFile(token.NewFileSet(), filepath.Join(".", name), nil, 0)
		if parseErr != nil {
			t.Fatal(parseErr)
		}
		ast.Inspect(production, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if ok {
				switch function := call.Fun.(type) {
				case *ast.Ident:
					if function.Name == "prepareRunnerLedgerPreflightClaim" {
						serviceCalls++
					}
					if function.Name == "bindRunnerLedgerPreflightClaimFromEvidence" {
						bindHelperCalls++
						if name != "evidence_session.go" {
							t.Fatalf("unreviewed claim bind helper caller in %s", name)
						}
					}
					if function.Name == "consumeRunnerLedgerPreflightClaimFromEvidence" {
						consumeHelperCalls++
						if name != "evidence_session.go" {
							t.Fatalf("unreviewed claim consume helper caller in %s", name)
						}
					}
				case *ast.SelectorExpr:
					if function.Sel.Name == "prepareRunnerLedgerPreflightClaim" {
						if name != "runner_ledger_consumer_service.go" {
							t.Fatalf("unreviewed preflight service caller in %s", name)
						}
						serviceCalls++
					}
					if function.Sel.Name == "projectRunnerLedgerCatalogPreflight" {
						kernelCalls++
						if name != "runner_ledger_preflight_service.go" {
							t.Fatalf("unreviewed kernel caller in %s", name)
						}
					}
				}
			}
			function, ok := node.(*ast.FuncDecl)
			if ok && function.Recv != nil && (function.Name.Name == "bindRunnerLedgerPreflightClaim" || function.Name.Name == "consumeRunnerLedgerPreflightClaim") {
				concreteBinders++
				if name != "evidence_session.go" {
					t.Fatalf("unreviewed claim binder in %s", name)
				}
			}
			return true
		})
	}
	if serviceCalls != 1 || kernelCalls != 1 || concreteBinders != 2 || bindHelperCalls != 1 || consumeHelperCalls != 1 {
		t.Fatalf("consumer graph service=%d kernel=%d binders=%d bind_helpers=%d consume_helpers=%d", serviceCalls, kernelCalls, concreteBinders, bindHelperCalls, consumeHelperCalls)
	}
}

func assertRunnerLedgerPreflightReadOnlyLifecycle(t *testing.T, fixture *runnerLedgerPreflightServiceFixture) {
	t.Helper()
	database := fixture.kernel.database
	if fixture.kernel.connector.attempts != 1 || database.setRoleCalls != 1 || database.lockCalls != 1 ||
		database.unlockCalls != 1 || database.closeCalls != 1 || database.ledgerReadCalls != 2 ||
		database.beginCalls != 0 || database.boundaryCalls != 0 || database.queryCalls != 0 ||
		database.backend.ledgerInsertCalls != 0 || database.backend.executeCalls != 0 || database.backend.commitCalls != 0 ||
		!database.closed || database.locked || database.roleConfigured || database.projectionActive {
		t.Fatalf("Slice C escaped read-only lifecycle: connector=%+v database=%+v", fixture.kernel.connector, database)
	}
}
