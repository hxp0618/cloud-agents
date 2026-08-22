package migration

import (
	"context"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"sync/atomic"
	"testing"
)

var _ runnerLedgerCommitObservationRecordBinder = (*generationEvidenceSession)(nil)
var _ runnerLedgerCommitObservationRecordBinder = (*runnerLedgerPreflightEvidenceFake)(nil)
var _ runnerLedgerAmbiguousResolutionRecordBinder = (*generationEvidenceSession)(nil)
var _ runnerLedgerAmbiguousResolutionRecordBinder = (*runnerLedgerPreflightEvidenceFake)(nil)

func (*runnerLedgerPreflightEvidenceFake) runnerLedgerCommitObservationRecordBinderSealed() {}

func (*runnerLedgerPreflightEvidenceFake) runnerLedgerAmbiguousResolutionRecordBinderSealed() {}

func (evidence *runnerLedgerPreflightEvidenceFake) bindRunnerLedgerRecoveryCommitObservationRecord(ctx context.Context, permit *runnerLedgerCommitObservationWriterPermit) (EvidenceJournal, JournalCursor, *OwnedEvidenceRecord, error) {
	evidence.reconciliationBindCalls++
	claimed, err := consumeRunnerLedgerCommitObservationWriterPermit(permit, evidence)
	if err != nil {
		return nil, JournalCursor{}, nil, err
	}
	return evidence.bindRunnerLedgerReconciliationTestRecord(ctx, claimed.candidateBinding, claimed.generation, claimed.recoveryDigest, claimed.cursor, claimed.selection.maxAttempts, EvidenceRecord{AttemptTerminal: cloneAttemptTerminalPointer(&claimed.terminal)})
}

func (evidence *runnerLedgerPreflightEvidenceFake) bindRunnerLedgerRecoveryAmbiguousResolutionRecord(ctx context.Context, permit *runnerLedgerAmbiguousResolutionWriterPermit) (EvidenceJournal, JournalCursor, *OwnedEvidenceRecord, error) {
	evidence.reconciliationBindCalls++
	claimed, err := consumeRunnerLedgerAmbiguousResolutionWriterPermit(permit, evidence)
	if err != nil {
		return nil, JournalCursor{}, nil, err
	}
	return evidence.bindRunnerLedgerReconciliationTestRecord(ctx, claimed.candidateBinding, claimed.generation, claimed.recoveryDigest, claimed.cursor, claimed.selection.maxAttempts, EvidenceRecord{AmbiguousResolution: cloneAmbiguousResolutionPointer(&claimed.resolution)})
}

func (evidence *runnerLedgerPreflightEvidenceFake) bindRunnerLedgerReconciliationTestRecord(ctx context.Context, candidate *verifiedEvidenceRunBinding, generation generationIdentity, recoveryDigest [32]byte, claimedCursor JournalCursor, maxAttempts uint32, record EvidenceRecord) (EvidenceJournal, JournalCursor, *OwnedEvidenceRecord, error) {
	if err := contextAdmissionError(ctx); err != nil {
		return nil, JournalCursor{}, nil, err
	}
	if evidence.reconciliationBindErr != nil {
		return nil, JournalCursor{}, nil, evidence.reconciliationBindErr
	}
	base := evidence.runnerEvidenceSessionFake
	if base == nil || base.closed || base.journal == nil || base.snapshot == nil || candidate != base.candidate.binding ||
		!sameGenerationIdentity(generation, base.active.identity) || generationJournalRecoveryDigest(base.snapshot) != recoveryDigest ||
		!sameCursorIdentity(claimedCursor, base.journal.cursor) {
		return nil, JournalCursor{}, nil, fail(CodeEvidenceRecoveryRequired, "runner-ledger-reconciliation-test-bind", "test evidence boundary changed", nil)
	}
	cursor := base.journal.cursor.clone()
	if evidence.mutateReconciliationRecord != nil {
		evidence.mutateReconciliationRecord(&record)
	}
	witness := runnerLedgerEntrySuccessFakeWitness{recordKind: admissionEvidenceRecordKind(record), generation: generation, cursor: cursor.clone()}
	owned := &OwnedEvidenceRecord{
		wire: cloneEvidenceRecord(record), witness: witness, generation: generation, cursor: cursor.clone(), consumed: &atomic.Bool{},
	}
	if evidence.reconciliationNoRecord {
		owned = nil
	}
	if evidence.mutateReconciliationAuthority != nil && owned != nil {
		evidence.mutateReconciliationAuthority(&cursor, owned)
	}
	base.journal.maxAttempts = maxAttempts
	return base.journal, cursor, owned, nil
}

type runnerLedgerRecoveryReconciliationFixture struct {
	success      *runnerLedgerEntrySuccessFixture
	fact         runnerLedgerConsumerFact
	database     *runnerPreflightSession
	outcome      runnerLedgerReconciliationOutcome
	state        RecoveryState
	beforeCursor JournalCursor
}

func newRunnerLedgerRecoveryReconciliationFixture(t *testing.T, state RecoveryState, outcome runnerLedgerReconciliationOutcome, postgresMajor uint16) *runnerLedgerRecoveryReconciliationFixture {
	t.Helper()
	return newRunnerLedgerRecoveryReconciliationFixtureWithRows(t, state, outcome, postgresMajor, nil)
}

func newRunnerLedgerRecoveryReconciliationFixtureWithRows(t *testing.T, state RecoveryState, outcome runnerLedgerReconciliationOutcome, postgresMajor uint16, rowsOverride func(*RuntimeBundle) []LedgerRow) *runnerLedgerRecoveryReconciliationFixture {
	t.Helper()
	raw, decision := buildExactTwoMigrationAdmissionRuntime(t)
	success := newRunnerLedgerEntrySuccessFixture(
		t, raw, decision, runnerLedgerPreflightPartialNextEntry, RecoveryTerminal, RecoveryBeginFirstAttemptNextEntry,
	)
	executionPermit := success.prepare(t)
	configureRunnerLedgerEntrySuccessExecution(t, success, executionPermit)
	runner := success.execution.base.service.kernel.runner
	base := success.execution.base.service.kernel.base
	writer, err := runner.prepareRunnerLedgerEntrySuccess(context.Background(), executionPermit, base.bundle, base.plans)
	if err == nil {
		writer, err = runner.beginRunnerLedgerEntrySuccessTransaction(context.Background(), writer)
	}
	if err == nil {
		writer, err = runner.prepareRunnerLedgerEntrySuccessStatement(context.Background(), writer)
	}
	for err == nil {
		writer, err = runner.appendRunnerLedgerEntrySuccessIntent(context.Background(), writer)
		if err != nil {
			break
		}
		writer, err = runner.executeRunnerLedgerEntrySuccessStatement(context.Background(), writer)
		if err != nil {
			break
		}
		writer, err = runner.appendRunnerLedgerEntrySuccessIntermediate(context.Background(), writer)
		if err != nil || writer.data.phase == runnerLedgerEntrySuccessFinalIntermediateDurable {
			break
		}
		writer, err = runner.advanceRunnerLedgerEntrySuccessStatement(context.Background(), writer)
	}
	if err == nil {
		writer, err = runner.insertRunnerLedgerEntrySuccessLedger(context.Background(), writer)
	}
	if err == nil {
		writer, err = runner.appendRunnerLedgerEntrySuccessCommitIntent(context.Background(), writer)
	}
	if err != nil || writer == nil {
		success.close(t)
		t.Fatalf("prepare dangling commit: writer=%+v err=%v", writer, err)
	}
	evidence := success.execution.base.service.evidence
	if err := closeRunnerLedgerEntrySuccessState(writer, nil); err != nil {
		success.close(t)
		t.Fatal(err)
	}
	if state == RecoveryAmbiguousUnresolved {
		appendRunnerLedgerReconciliationUnresolvedTerminal(t, evidence)
	}
	recovery := evidence.RecoverySnapshot()
	if recovery == nil || recovery.state != state || recovery.nextPermittedAction != RecoveryReconcileCommit {
		success.close(t)
		t.Fatalf("reconciliation predecessor=%+v want=%s", recovery, state)
	}
	evidence.mu.Lock()
	evidence.recovery = cloneRecoverySnapshot(recovery)
	evidence.mu.Unlock()
	rows := runnerLedgerReconciliationFixtureRows(base.bundle, outcome)
	if rowsOverride != nil {
		rows = cloneProjectionValue(rowsOverride(base.bundle))
	}
	preflight := newRunnerPreflightSession()
	preflight.serverMajor = int(postgresMajor)
	preflight.ledgerRowsByRead = [][]LedgerRow{cloneProjectionValue(rows), cloneProjectionValue(rows)}
	runner.Connector = &runnerPreflightConnector{session: preflight}
	if outcome == runnerLedgerReconciliationDivergent && rowsOverride == nil {
		success.execution.base.service.kernel.factory.mutateCatalog = func(result *ProjectionResult[CatalogProjection]) {
			result.Digest = testDigest("reconciliation-divergent-catalog")
		}
	}
	claim, err := runner.prepareRunnerLedgerPreflightClaim(context.Background(), "test-only", base.bundle, base.plans, evidence, base.candidate)
	if err != nil {
		success.close(t)
		t.Fatal(err)
	}
	defer revokeRunnerLedgerPreflightClaim(claim)
	dispatch, err := runner.claimRunnerLedgerPreflightDispatch(context.Background(), evidence, base.candidate, claim)
	if err != nil {
		success.close(t)
		t.Fatal(err)
	}
	fact, err := bindRunnerLedgerConsumerFact(generatedRunnerLedgerConsumerProfile, dispatch, base.bundle.Manifest.ManifestDigest)
	if err != nil || fact.action != runnerLedgerConsumerRecoveryNotImplemented {
		success.close(t)
		t.Fatalf("reconciliation fact=%+v err=%v", fact, err)
	}
	database := newRunnerPreflightSession()
	database.serverMajor = int(postgresMajor)
	for index := 0; index < 6; index++ {
		database.ledgerRowsByRead = append(database.ledgerRowsByRead, cloneProjectionValue(rows))
	}
	runner.Connector = &runnerPreflightConnector{session: database}
	journal := evidence.runnerEvidenceSessionFake.journal
	if journal.rotateAt == nil {
		journal.rotateAt = map[int]bool{}
	}
	if journal.appendOutcomeAt == nil {
		journal.appendOutcomeAt = map[int]appendOutcome{}
	}
	if journal.appendErrAt == nil {
		journal.appendErrAt = map[int]error{}
	}
	return &runnerLedgerRecoveryReconciliationFixture{
		success: success, fact: fact, database: database, outcome: outcome, state: state, beforeCursor: recovery.cursor.clone(),
	}
}

func runnerLedgerReconciliationFixtureRows(bundle *RuntimeBundle, outcome runnerLedgerReconciliationOutcome) []LedgerRow {
	rows := []LedgerRow{ledgerRowFor(bundle.Manifest.SchemaBundle.Migrations[0], bundle.Manifest.SchemaBundleDigest)}
	if outcome == runnerLedgerReconciliationExactCommitted {
		rows = append(rows, ledgerRowFor(bundle.Manifest.SchemaBundle.Migrations[1], bundle.Manifest.SchemaBundleDigest))
	}
	return rows
}

func appendRunnerLedgerReconciliationUnresolvedTerminal(t *testing.T, evidence *runnerLedgerPreflightEvidenceFake) {
	t.Helper()
	snapshot := evidence.RecoverySnapshot()
	if snapshot == nil || snapshot.commitIntent == nil || snapshot.lastIntermediateStateDigest == nil ||
		snapshot.lastIntermediateEvidenceRecordDigest == nil || snapshot.lastCommitIntentRecordDigest == nil {
		t.Fatal("dangling commit boundary is unavailable")
	}
	commit := snapshot.commitIntent.value
	code := string(CodeAmbiguousCommit)
	major := uint16(16)
	terminal := AttemptTerminalState{
		SchemaBundleDigest: commit.SchemaBundleDigest, CatalogContractDigest: commit.CatalogContractDigest,
		AuthorityProfileDigest: commit.AuthorityProfileDigest, AuthorityBindingDigest: commit.AuthorityBindingDigest,
		MigrationID: commit.MigrationID, AttemptIndex: commit.AttemptIndex,
		PreviousAttemptTerminalDigest: cloneDigestPointer(commit.PreviousAttemptTerminalDigest),
		LastIntermediateStateDigest:   cloneDigestPointer(snapshot.lastIntermediateStateDigest),
		Outcome:                       "ambiguous_unresolved", StableErrorCode: &code,
		FailureEvidence: &StableFailureEvidence{Code: CodeAmbiguousCommit, Phase: "commit", Path: "transaction", Major: &major, Retryable: false},
		ReconcileResult: "unresolved",
	}
	var err error
	terminal.TerminalDigest, err = terminal.ComputeDigest()
	if err != nil || terminal.Validate() != nil {
		t.Fatalf("unresolved terminal=%+v err=%v", terminal, err)
	}
	generation := evidence.active.identity
	cursor := evidence.runnerEvidenceSessionFake.journal.cursor.clone()
	owned := &OwnedEvidenceRecord{
		wire:       EvidenceRecord{AttemptTerminal: cloneAttemptTerminalPointer(&terminal)},
		witness:    runnerLedgerEntrySuccessFakeWitness{recordKind: EvidenceRecordAttemptTerminal, generation: generation, cursor: cursor.clone()},
		generation: generation, cursor: cursor.clone(), consumed: &atomic.Bool{},
	}
	evidence.runnerEvidenceSessionFake.journal.maxAttempts = evidence.schema.maxAttempts[commit.MigrationID]
	result, err := evidence.runnerEvidenceSessionFake.journal.AppendDurable(context.Background(), cursor, owned)
	if err != nil || result.outcome != appendOutcomeDurable {
		t.Fatalf("append unresolved terminal result=%+v err=%v", result, err)
	}
	evidence.schema.chainWitness.ambiguousBoundaries[terminal.TerminalDigest] = ownedAmbiguousBoundaryWitness{
		migrationID: commit.MigrationID, attemptIndex: commit.AttemptIndex, commitCalled: true,
		finalIntermediateRecordDigest: *snapshot.lastIntermediateEvidenceRecordDigest,
		commitIntentRecordDigest:      *snapshot.lastCommitIntentRecordDigest,
	}
	evidence.recovery = evidence.RecoverySnapshot()
}

func (fixture *runnerLedgerRecoveryReconciliationFixture) run(ctx context.Context) error {
	base := fixture.success.execution.base.service.kernel.base
	return fixture.success.execution.base.service.kernel.runner.admitRunnerLedgerRecoveryAction(
		ctx, "test-only", base.bundle, base.plans, fixture.success.execution.base.service.evidence, base.candidate, fixture.fact,
	)
}

func (fixture *runnerLedgerRecoveryReconciliationFixture) prepareWriterPermit(t *testing.T) any {
	t.Helper()
	base := fixture.success.execution.base.service.kernel.base
	runner := fixture.success.execution.base.service.kernel.runner
	owner, err := runner.prepareRunnerLedgerRecoveryAdmission(
		context.Background(), "test-only", base.bundle, base.plans,
		fixture.success.execution.base.service.evidence, base.candidate, fixture.fact,
	)
	if err != nil {
		t.Fatal(err)
	}
	switch admission := owner.(type) {
	case *runnerLedgerCommitObservationAdmissionPermit:
		seed, err := claimRunnerLedgerCommitObservationAdmissionPermit(admission)
		if err != nil {
			t.Fatal(err)
		}
		receipt, err := runner.revalidateAndCloseRunnerLedgerReconciliationAdmission(context.Background(), seed, base.bundle)
		if err != nil {
			t.Fatal(err)
		}
		permit, err := mintRunnerLedgerCommitObservationWriterPermit(seed, receipt)
		if err != nil || !validRunnerLedgerCommitObservationWriterPermit(permit) {
			t.Fatalf("commit-observation writer permit=%+v err=%v", permit, err)
		}
		return permit
	case *runnerLedgerAmbiguousResolutionAdmissionPermit:
		seed, err := claimRunnerLedgerAmbiguousResolutionAdmissionPermit(admission)
		if err != nil {
			t.Fatal(err)
		}
		receipt, err := runner.revalidateAndCloseRunnerLedgerReconciliationAdmission(context.Background(), seed, base.bundle)
		if err != nil {
			t.Fatal(err)
		}
		permit, err := mintRunnerLedgerAmbiguousResolutionWriterPermit(seed, receipt)
		if err != nil || !validRunnerLedgerAmbiguousResolutionWriterPermit(permit) {
			t.Fatalf("ambiguous-resolution writer permit=%+v err=%v", permit, err)
		}
		return permit
	default:
		t.Fatalf("reconciliation admission type=%T", owner)
		return nil
	}
}

func (fixture *runnerLedgerRecoveryReconciliationFixture) close(t *testing.T) {
	t.Helper()
	if fixture == nil {
		return
	}
	if fixture.database != nil && !fixture.database.closed {
		if err := closeRunnerDatabasePreflight(fixture.database, fixture.success.execution.base.service.kernel.base.key, fixture.database.locked, nil); err != nil {
			t.Fatal(err)
		}
	}
	fixture.success.close(t)
}

func TestRunnerLedgerRecoveryReconciliationAppendsDistinctKnownOutcomeRecords(t *testing.T) {
	states := []RecoveryState{RecoveryDanglingCommitIntent, RecoveryAmbiguousUnresolved}
	outcomes := []runnerLedgerReconciliationOutcome{
		runnerLedgerReconciliationExactCommitted,
		runnerLedgerReconciliationExactPending,
		runnerLedgerReconciliationDivergent,
	}
	for _, state := range states {
		for outcomeIndex, outcome := range outcomes {
			major := uint16(15 + outcomeIndex)
			name := fmt.Sprintf("%s/pg%d/%s", state, major, outcome)
			t.Run(name, func(t *testing.T) {
				fixture := newRunnerLedgerRecoveryReconciliationFixture(t, state, outcome, major)
				defer fixture.close(t)
				journal := fixture.success.execution.base.service.evidence.runnerEvidenceSessionFake.journal
				journal.rotateAt[journal.appendCalls+1] = outcomeIndex%2 == 0
				if err := fixture.run(context.Background()); err != nil {
					t.Fatal(err)
				}
				snapshot := fixture.success.execution.base.service.evidence.RecoverySnapshot()
				if snapshot == nil || fixture.beforeCursor.Valid() || !snapshot.cursor.Valid() || fixture.database.ledgerReadCalls != 6 ||
					fixture.database.beginCalls != 0 || fixture.database.backend.executeCalls != 0 || fixture.database.backend.ledgerInsertCalls != 0 ||
					fixture.database.backend.commitCalls != 0 || fixture.database.unlockCalls != 1 || fixture.database.closeCalls != 1 ||
					!fixture.database.closed || fixture.database.locked {
					t.Fatalf("reconciliation escaped read-only boundary: snapshot=%+v database=%+v", snapshot, fixture.database)
				}
				if state == RecoveryDanglingCommitIntent {
					terminal := journal.appendedRecord.AttemptTerminal
					if terminal == nil || journal.appendedRecord.AmbiguousResolution != nil || terminal.FailureEvidence == nil ||
						terminal.FailureEvidence.Major == nil || *terminal.FailureEvidence.Major != major ||
						terminal.ReconcileResult != string(outcome) || terminal.RetryProof != nil {
						gotMajor := uint16(0)
						if terminal != nil && terminal.FailureEvidence != nil && terminal.FailureEvidence.Major != nil {
							gotMajor = *terminal.FailureEvidence.Major
						}
						t.Fatalf("terminal=%+v major=%d want=%d resolution=%+v", terminal, gotMajor, major, journal.appendedRecord.AmbiguousResolution)
					}
					want := map[runnerLedgerReconciliationOutcome]string{
						runnerLedgerReconciliationExactCommitted: "ambiguous_reconciled_committed",
						runnerLedgerReconciliationExactPending:   "ambiguous_reconciled_pending",
						runnerLedgerReconciliationDivergent:      "ambiguous_divergent",
					}[outcome]
					if terminal.Outcome != want || snapshot.lastTerminal == nil || !runnerCanonicalEqual(snapshot.lastTerminal.value, *terminal) {
						t.Fatalf("terminal=%+v snapshot=%+v want=%s", terminal, snapshot, want)
					}
				} else {
					resolution := journal.appendedRecord.AmbiguousResolution
					if resolution == nil || journal.appendedRecord.AttemptTerminal != nil || resolution.ReconcileResult != string(outcome) ||
						snapshot.lastResolution == nil || !runnerCanonicalEqual(snapshot.lastResolution.value, *resolution) {
						t.Fatalf("resolution=%+v snapshot=%+v", resolution, snapshot)
					}
					want := map[runnerLedgerReconciliationOutcome]string{
						runnerLedgerReconciliationExactCommitted: "resolved_committed",
						runnerLedgerReconciliationExactPending:   "resolved_pending",
						runnerLedgerReconciliationDivergent:      "resolved_divergent",
					}[outcome]
					if resolution.Outcome != want {
						t.Fatalf("resolution outcome=%s want=%s", resolution.Outcome, want)
					}
				}
				switch outcome {
				case runnerLedgerReconciliationExactCommitted:
					if snapshot.state != RecoveryCompleted || snapshot.nextPermittedAction != RecoveryReturnSuccess {
						t.Fatalf("committed snapshot=%+v", snapshot)
					}
				case runnerLedgerReconciliationExactPending:
					if snapshot.state != RecoveryTerminal || snapshot.nextPermittedAction != RecoveryBeginNextAttempt {
						t.Fatalf("pending snapshot=%+v", snapshot)
					}
				case runnerLedgerReconciliationDivergent:
					if snapshot.state != RecoveryDivergent || snapshot.nextPermittedAction != RecoveryReturnFailure {
						t.Fatalf("divergent snapshot=%+v", snapshot)
					}
				}
			})
		}
	}
}

func TestRunnerLedgerRecoveryReconciliationClassifiesShortValidPrefixDivergent(t *testing.T) {
	fixture := newRunnerLedgerRecoveryReconciliationFixtureWithRows(
		t, RecoveryDanglingCommitIntent, runnerLedgerReconciliationDivergent, 16,
		func(*RuntimeBundle) []LedgerRow { return []LedgerRow{} },
	)
	defer fixture.close(t)
	if err := fixture.run(context.Background()); err != nil {
		t.Fatal(err)
	}
	snapshot := fixture.success.execution.base.service.evidence.RecoverySnapshot()
	terminal := fixture.success.execution.base.service.evidence.runnerEvidenceSessionFake.journal.appendedRecord.AttemptTerminal
	if terminal == nil || terminal.Outcome != "ambiguous_divergent" || snapshot == nil ||
		snapshot.state != RecoveryDivergent || snapshot.nextPermittedAction != RecoveryReturnFailure {
		t.Fatalf("short-prefix terminal=%+v snapshot=%+v", terminal, snapshot)
	}
}

func TestRunnerLedgerRecoveryReconciliationLeavesImmutableV1EmptyPrefixUnsupported(t *testing.T) {
	fixture, readback, runner := newRunnerCommitIntentFixture(t)
	durable, err := runner.appendCurrentCommitIntent(context.Background(), readback)
	if err != nil || !validRunnerDurableCommitIntent(durable) {
		t.Fatalf("first-entry commit intent: durable=%+v err=%v", durable, err)
	}
	snapshot := fixture.evidence.RecoverySnapshot()
	hint, err := runnerLedgerReconciliationHintFromSnapshot(snapshot)
	if err != nil || hint != nil || snapshot == nil || snapshot.commitIntent == nil ||
		snapshot.commitIntent.value.ExpectedLedgerLength != 1 || fixture.evidence.journal.appendCalls != 3 {
		t.Fatalf("empty-prefix hint=%+v err=%v snapshot=%+v append=%d", hint, err, snapshot, fixture.evidence.journal.appendCalls)
	}
	if err := closeRunnerDurableCommitIntent(durable, nil); err != nil {
		t.Fatal(err)
	}
	if err := closeRunnerEvidenceOwnership(fixture.evidence, fixture.candidate); err != nil {
		t.Fatal(err)
	}
}

func TestRunnerLedgerRecoveryReconciliationHintBindsCompleteCommitIntent(t *testing.T) {
	fixture := newRunnerLedgerRecoveryReconciliationFixture(t, RecoveryDanglingCommitIntent, runnerLedgerReconciliationExactPending, 16)
	defer fixture.close(t)
	hint, err := runnerLedgerReconciliationHintFromSnapshot(fixture.success.execution.base.service.evidence.RecoverySnapshot())
	if err != nil || hint == nil || hint.canonical == ([32]byte{}) || hint.canonical != runnerLedgerReconciliationHintDigest(hint) {
		t.Fatalf("hint=%+v err=%v", hint, err)
	}
	drift := cloneRunnerLedgerReconciliationHint(hint)
	drift.commit.LedgerRow.MigrationName += "-drift"
	if runnerLedgerReconciliationHintDigest(drift) != ([32]byte{}) {
		t.Fatal("commit-intent drift retained reconciliation hint identity")
	}
}

func TestRunnerLedgerRecoveryReconciliationOperationalUnknownNeverAppends(t *testing.T) {
	for index, state := range []RecoveryState{RecoveryDanglingCommitIntent, RecoveryAmbiguousUnresolved} {
		t.Run(string(state), func(t *testing.T) {
			fixture := newRunnerLedgerRecoveryReconciliationFixture(t, state, runnerLedgerReconciliationExactPending, uint16(15+index*2))
			defer fixture.close(t)
			journal := fixture.success.execution.base.service.evidence.runnerEvidenceSessionFake.journal
			beforeCalls := journal.appendCalls
			fixture.success.execution.base.service.kernel.factory.catalogErr = errors.New("secret-operational-unknown")
			err := fixture.run(context.Background())
			if err == nil || containsErrorText(err, "secret-") || journal.appendCalls != beforeCalls || !fixture.beforeCursor.Valid() ||
				fixture.database.beginCalls != 0 || fixture.database.backend.executeCalls != 0 || fixture.database.backend.ledgerInsertCalls != 0 ||
				fixture.database.backend.commitCalls != 0 || fixture.database.closeCalls != 1 || !fixture.database.closed {
				t.Fatalf("unknown err=%v append=%d/%d cursor=%v database=%+v", err, journal.appendCalls, beforeCalls, fixture.beforeCursor.Valid(), fixture.database)
			}
		})
	}
}

func TestRunnerLedgerRecoveryReconciliationFinalBarriersFailClosed(t *testing.T) {
	tests := []struct {
		name      string
		state     RecoveryState
		configure func(*runnerLedgerRecoveryReconciliationFixture)
		wantCode  ErrorCode
		revokes   bool
	}{
		{"final-ledger-drift", RecoveryDanglingCommitIntent, func(f *runnerLedgerRecoveryReconciliationFixture) {
			row := cloneProjectionValue(f.database.ledgerRowsByRead[5][0])
			row.MigrationName += " drift"
			f.database.ledgerRowsByRead[5][0] = row
		}, CodeInvalidLedger, false},
		{"final-role-drift", RecoveryDanglingCommitIntent, func(f *runnerLedgerRecoveryReconciliationFixture) {
			f.database.boundaryMutate[3] = func(boundary *BoundaryState) {
				boundary.CurrentUser = RuntimeRole
			}
		}, CodeAuthorityDrift, false},
		{"final-lock-lost", RecoveryAmbiguousUnresolved, func(f *runnerLedgerRecoveryReconciliationFixture) {
			f.database.boundaryMutate[3] = func(boundary *BoundaryState) {
				boundary.LockHeld = false
			}
		}, CodeLockLost, false},
		{"final-session-non-idle", RecoveryDanglingCommitIntent, func(f *runnerLedgerRecoveryReconciliationFixture) {
			f.database.boundaryMutate[4] = func(boundary *BoundaryState) {
				boundary.TxStatus = 'T'
			}
		}, CodeTransactionBoundary, false},
		{"final-close-uncertain", RecoveryAmbiguousUnresolved, func(f *runnerLedgerRecoveryReconciliationFixture) {
			f.database.closeErr = errors.New("secret-close")
		}, CodeTransactionBoundary, false},
		{"bind-raw-error", RecoveryDanglingCommitIntent, func(f *runnerLedgerRecoveryReconciliationFixture) {
			f.success.execution.base.service.evidence.reconciliationBindErr = errors.New("secret-bind")
		}, CodeEvidenceJournalFailed, false},
		{"bind-missing-record", RecoveryAmbiguousUnresolved, func(f *runnerLedgerRecoveryReconciliationFixture) {
			f.success.execution.base.service.evidence.reconciliationNoRecord = true
		}, CodeEvidenceRecoveryRequired, true},
		{"bind-record-body", RecoveryDanglingCommitIntent, func(f *runnerLedgerRecoveryReconciliationFixture) {
			f.success.execution.base.service.evidence.mutateReconciliationRecord = func(record *EvidenceRecord) {
				record.AttemptTerminal.ReconcileResult = "divergent"
			}
		}, CodeEvidenceRecoveryRequired, true},
		{"bind-foreign-cursor", RecoveryAmbiguousUnresolved, func(f *runnerLedgerRecoveryReconciliationFixture) {
			f.success.execution.base.service.evidence.mutateReconciliationAuthority = func(cursor *JournalCursor, _ *OwnedEvidenceRecord) {
				cursor.nextSequence++
			}
		}, CodeEvidenceRecoveryRequired, true},
		{"bind-consumed-record", RecoveryDanglingCommitIntent, func(f *runnerLedgerRecoveryReconciliationFixture) {
			f.success.execution.base.service.evidence.mutateReconciliationAuthority = func(_ *JournalCursor, owned *OwnedEvidenceRecord) {
				owned.consumed.Store(true)
			}
		}, CodeEvidenceRecoveryRequired, true},
		{"append-before-mutation", RecoveryDanglingCommitIntent, func(f *runnerLedgerRecoveryReconciliationFixture) {
			journal := f.success.execution.base.service.evidence.runnerEvidenceSessionFake.journal
			journal.appendErrAt[journal.appendCalls+1] = errors.New("secret-append")
		}, CodeEvidenceJournalFailed, false},
		{"append-canceled-before-mutation", RecoveryAmbiguousUnresolved, func(f *runnerLedgerRecoveryReconciliationFixture) {
			journal := f.success.execution.base.service.evidence.runnerEvidenceSessionFake.journal
			journal.appendErrAt[journal.appendCalls+1] = context.Canceled
		}, CodeContextCanceled, false},
		{"append-error-with-values", RecoveryDanglingCommitIntent, func(f *runnerLedgerRecoveryReconciliationFixture) {
			journal := f.success.execution.base.service.evidence.runnerEvidenceSessionFake.journal
			journal.appendErrAt[journal.appendCalls+1] = errors.New("secret-append")
			journal.appendValuesWithError = true
		}, CodeEvidenceRecoveryRequired, true},
		{"append-unknown", RecoveryAmbiguousUnresolved, func(f *runnerLedgerRecoveryReconciliationFixture) {
			journal := f.success.execution.base.service.evidence.runnerEvidenceSessionFake.journal
			journal.appendOutcomeAt[journal.appendCalls+1] = appendOutcomeUnknown
		}, CodeEvidenceRecoveryRequired, true},
		{"durable-result", RecoveryDanglingCommitIntent, func(f *runnerLedgerRecoveryReconciliationFixture) {
			f.success.execution.base.service.evidence.runnerEvidenceSessionFake.journal.mutateAppendResult = func(result *AppendResult) {
				result.candidateRecordDigest = testDigest("other-reconciliation-record")
			}
		}, CodeEvidenceRecoveryRequired, true},
		{"durable-checkpoint", RecoveryAmbiguousUnresolved, func(f *runnerLedgerRecoveryReconciliationFixture) {
			f.success.execution.base.service.evidence.runnerEvidenceSessionFake.journal.mutateAppendResult = func(result *AppendResult) {
				result.candidateCheckpointRecordDigest = testDigest("other-reconciliation-checkpoint")
			}
		}, CodeEvidenceRecoveryRequired, true},
		{"rotation-one-sided", RecoveryDanglingCommitIntent, func(f *runnerLedgerRecoveryReconciliationFixture) {
			journal := f.success.execution.base.service.evidence.runnerEvidenceSessionFake.journal
			journal.rotateAt[journal.appendCalls+1] = true
			journal.mutateAppendResult = func(result *AppendResult) {
				result.rotationHeaderCheckpointRecordDigest = nil
			}
		}, CodeEvidenceRecoveryRequired, true},
		{"durable-snapshot", RecoveryAmbiguousUnresolved, func(f *runnerLedgerRecoveryReconciliationFixture) {
			f.success.execution.base.service.evidence.runnerEvidenceSessionFake.journal.mutateAppendSnapshot = func(snapshot *RecoverySnapshot) {
				snapshot.lastCommitIntentRecordDigest = digestPointer(testDigest("other-reconciliation-commit"))
			}
		}, CodeEvidenceRecoveryRequired, true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newRunnerLedgerRecoveryReconciliationFixture(t, test.state, runnerLedgerReconciliationExactPending, 16)
			defer fixture.close(t)
			test.configure(fixture)
			err := fixture.run(context.Background())
			journal := fixture.success.execution.base.service.evidence.runnerEvidenceSessionFake.journal
			if !IsCode(err, test.wantCode) || containsErrorText(err, "secret-") || !fixture.database.closed ||
				fixture.database.beginCalls != 0 || fixture.database.backend.executeCalls != 0 ||
				fixture.database.backend.ledgerInsertCalls != 0 || fixture.database.backend.commitCalls != 0 {
				t.Fatalf("err=%v database=%+v journal=%+v", err, fixture.database, journal)
			}
			if test.revokes && fixture.beforeCursor.Valid() {
				t.Fatalf("%s retained old cursor", test.name)
			}
			fixture.database.closeErr = nil
		})
	}
}

func TestRunnerLedgerRecoveryReconciliationWriterPermitsAreDistinctOneShotAndDatabaseBound(t *testing.T) {
	for _, state := range []RecoveryState{RecoveryDanglingCommitIntent, RecoveryAmbiguousUnresolved} {
		t.Run(string(state), func(t *testing.T) {
			fixture := newRunnerLedgerRecoveryReconciliationFixture(t, state, runnerLedgerReconciliationExactPending, 16)
			defer fixture.close(t)
			evidence := fixture.success.execution.base.service.evidence
			switch permit := fixture.prepareWriterPermit(t).(type) {
			case *runnerLedgerCommitObservationWriterPermit:
				baseline := permit.canonical
				copyPermit := *permit
				copyPermit.self = &copyPermit
				if _, _, _, err := evidence.bindRunnerLedgerRecoveryCommitObservationRecord(context.Background(), &copyPermit); !IsCode(err, CodeEvidenceRecoveryRequired) {
					t.Fatalf("copied commit-observation permit err=%v", err)
				}
				if !validRunnerLedgerCommitObservationWriterPermit(permit) {
					t.Fatal("copied commit-observation permit changed original authority")
				}
				drift := *permit.receipt
				drift.self = &drift
				drift.database.databaseName += "_drift"
				if digest := runnerLedgerReconciliationClosedReceiptDigest(&drift); digest == ([32]byte{}) || digest == permit.receipt.canonical {
					t.Fatalf("database drift receipt digest=%x baseline=%x", digest, permit.receipt.canonical)
				}
				classificationDrift := *permit.receipt
				classificationDrift.self = &classificationDrift
				classificationDrift.classification = cloneRunnerLedgerReconciliationFacts(permit.receipt.classification)
				classificationDrift.classification.pendingCatalogDigest = testDigest("other-reconciliation-predecessor")
				classificationDrift.classification.subjectDigest = runnerLedgerReconciliationFactsDigest(classificationDrift.classification)
				if digest := runnerLedgerReconciliationClosedReceiptDigest(&classificationDrift); digest == ([32]byte{}) || digest == permit.receipt.canonical {
					t.Fatalf("classification drift receipt digest=%x baseline=%x", digest, permit.receipt.canonical)
				}
				journal, cursor, owned, err := evidence.bindRunnerLedgerRecoveryCommitObservationRecord(context.Background(), permit)
				if err != nil || journal == nil || owned == nil || !sameCursorIdentity(cursor, permit.cursor) || permit.canonical != baseline {
					t.Fatalf("commit-observation bind journal=%T cursor=%+v owned=%+v err=%v", journal, cursor, owned, err)
				}
				if _, _, _, err := evidence.bindRunnerLedgerRecoveryCommitObservationRecord(context.Background(), permit); !IsCode(err, CodeEvidenceRecoveryRequired) {
					t.Fatalf("second commit-observation consume err=%v", err)
				}
			case *runnerLedgerAmbiguousResolutionWriterPermit:
				copyPermit := *permit
				copyPermit.self = &copyPermit
				if _, _, _, err := evidence.bindRunnerLedgerRecoveryAmbiguousResolutionRecord(context.Background(), &copyPermit); !IsCode(err, CodeEvidenceRecoveryRequired) {
					t.Fatalf("copied ambiguous-resolution permit err=%v", err)
				}
				if !validRunnerLedgerAmbiguousResolutionWriterPermit(permit) {
					t.Fatal("copied ambiguous-resolution permit changed original authority")
				}
				journal, cursor, owned, err := evidence.bindRunnerLedgerRecoveryAmbiguousResolutionRecord(context.Background(), permit)
				if err != nil || journal == nil || owned == nil || !sameCursorIdentity(cursor, permit.cursor) {
					t.Fatalf("ambiguous-resolution bind journal=%T cursor=%+v owned=%+v err=%v", journal, cursor, owned, err)
				}
				if _, _, _, err := evidence.bindRunnerLedgerRecoveryAmbiguousResolutionRecord(context.Background(), permit); !IsCode(err, CodeEvidenceRecoveryRequired) {
					t.Fatalf("second ambiguous-resolution consume err=%v", err)
				}
			default:
				t.Fatalf("unexpected writer permit type %T", permit)
			}
		})
	}
	if _, _, _, err := (&runnerLedgerPreflightEvidenceFake{}).bindRunnerLedgerRecoveryCommitObservationRecord(context.Background(), &runnerLedgerCommitObservationWriterPermit{}); !IsCode(err, CodeEvidenceRecoveryRequired) {
		t.Fatalf("literal commit-observation permit err=%v", err)
	}
	if _, _, _, err := (&runnerLedgerPreflightEvidenceFake{}).bindRunnerLedgerRecoveryAmbiguousResolutionRecord(context.Background(), &runnerLedgerAmbiguousResolutionWriterPermit{}); !IsCode(err, CodeEvidenceRecoveryRequired) {
		t.Fatalf("literal ambiguous-resolution permit err=%v", err)
	}
}

func TestRunnerLedgerRecoveryReconciliationAdmissionTamperClosesRegisteredSession(t *testing.T) {
	tests := []struct {
		name  string
		state RecoveryState
		drift func(runnerLedgerRecoveryCloseOnlyPermit)
		claim func(runnerLedgerRecoveryCloseOnlyPermit) error
	}{
		{
			name: "commit-observation-wrapper-self", state: RecoveryDanglingCommitIntent,
			drift: func(owner runnerLedgerRecoveryCloseOnlyPermit) {
				owner.(*runnerLedgerCommitObservationAdmissionPermit).self = nil
			},
			claim: func(owner runnerLedgerRecoveryCloseOnlyPermit) error {
				_, err := claimRunnerLedgerCommitObservationAdmissionPermit(owner.(*runnerLedgerCommitObservationAdmissionPermit))
				return err
			},
		},
		{
			name: "ambiguous-resolution-binder", state: RecoveryAmbiguousUnresolved,
			drift: func(owner runnerLedgerRecoveryCloseOnlyPermit) {
				owner.(*runnerLedgerAmbiguousResolutionAdmissionPermit).core.evidenceBinder = nil
			},
			claim: func(owner runnerLedgerRecoveryCloseOnlyPermit) error {
				_, err := claimRunnerLedgerAmbiguousResolutionAdmissionPermit(owner.(*runnerLedgerAmbiguousResolutionAdmissionPermit))
				return err
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newRunnerLedgerRecoveryReconciliationFixture(t, test.state, runnerLedgerReconciliationExactPending, 16)
			defer fixture.close(t)
			base := fixture.success.execution.base.service.kernel.base
			owner, err := fixture.success.execution.base.service.kernel.runner.prepareRunnerLedgerRecoveryAdmission(
				context.Background(), "test-only", base.bundle, base.plans,
				fixture.success.execution.base.service.evidence, base.candidate, fixture.fact,
			)
			if err != nil {
				t.Fatal(err)
			}
			test.drift(owner)
			err = test.claim(owner)
			if !IsCode(err, CodeEvidenceRecoveryRequired) || !fixture.database.closed || fixture.database.locked ||
				fixture.database.unlockCalls != 1 || fixture.database.closeCalls != 1 {
				t.Fatalf("claim err=%v database=%+v", err, fixture.database)
			}
		})
	}
}

func TestRunnerLedgerRecoveryReconciliationHasExactlyTwoAppendEdgesAndNoExternalWriter(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "runner_ledger_recovery_commit_reconciliation.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	writers := map[string]int{
		"appendRunnerLedgerRecoveryCommitObservation":   0,
		"appendRunnerLedgerRecoveryAmbiguousResolution": 0,
	}
	forbidden := map[string]bool{
		"BeginMigration": true, "ExecuteStatement": true, "Commit": true, "Rollback": true,
		"Insert": true, "Exec": true, "Query": true, "QueryRow": true,
		"AppendGenerationSuperseded": true, "AppendGenerationReserved": true, "AppendGenerationActivated": true,
	}
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Body == nil {
			continue
		}
		ast.Inspect(function.Body, func(node ast.Node) bool {
			selector, ok := node.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if selector.Sel.Name == "AppendDurable" {
				if _, approved := writers[function.Name.Name]; !approved {
					t.Fatalf("unapproved reconciliation append edge in %s", function.Name.Name)
				}
				writers[function.Name.Name]++
			}
			if forbidden[selector.Sel.Name] {
				t.Fatalf("reconciliation kernel acquired forbidden %s edge in %s", selector.Sel.Name, function.Name.Name)
			}
			return true
		})
	}
	for writer, calls := range writers {
		if calls != 1 {
			t.Fatalf("%s append calls=%d want=1", writer, calls)
		}
	}
}
