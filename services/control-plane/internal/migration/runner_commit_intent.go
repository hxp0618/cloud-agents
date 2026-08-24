package migration

import (
	"context"
	"crypto/sha256"
	"errors"
	"strconv"
	"sync"
	"sync/atomic"
)

// runnerDurableCommitIntent proves that the exact post-ledger CommitIntent and
// matching lineage checkpoint are durable. It still exposes no transaction
// commit or terminal-evidence capability.
type runnerDurableCommitIntent struct {
	self                     *runnerDurableCommitIntent
	binding                  *runnerDurableCommitIntentBinding
	session                  DatabaseSession
	transaction              MigrationTransaction
	evidence                 EvidenceSession
	journal                  EvidenceJournal
	key                      int64
	candidateBinding         *verifiedEvidenceRunBinding
	generation               generationIdentity
	ledgerCanonical          [32]byte
	recoveryDigest           [32]byte
	dispatch                 runnerPreparedDispatch
	database                 runnerPreparedDatabaseIdentity
	maxAttempts              uint32
	policy                   ExecutionPolicy
	plan                     StatementPlan
	intent                   StatementIntent
	intermediate             StatementIntermediateEvidence
	commit                   CommitIntent
	cursor                   JournalCursor
	intentRecordDigest       Digest
	intermediateRecordDigest Digest
	commitRecordDigest       Digest
	checkpointDigest         Digest
	ledgerPrefixDigest       Digest
	boundary                 BoundaryState
	canonical                [32]byte
	closed                   bool
}

type runnerDurableCommitIntentBinding struct {
	prepared         *runnerDurableCommitIntent
	session          DatabaseSession
	transaction      MigrationTransaction
	evidence         EvidenceSession
	journal          EvidenceJournal
	key              int64
	candidateBinding *verifiedEvidenceRunBinding
	cursorValid      *atomic.Bool
	canonical        [32]byte
}

type runnerDurableCommitIntentRegistryRecord struct {
	prepared         *runnerDurableCommitIntent
	binding          *runnerDurableCommitIntentBinding
	session          DatabaseSession
	transaction      MigrationTransaction
	evidence         EvidenceSession
	journal          EvidenceJournal
	key              int64
	candidateBinding *verifiedEvidenceRunBinding
	cursorValid      *atomic.Bool
	canonical        [32]byte
}

type runnerCommitIntentSeed struct {
	session                  DatabaseSession
	transaction              MigrationTransaction
	evidence                 EvidenceSession
	journal                  EvidenceJournal
	key                      int64
	candidateBinding         *verifiedEvidenceRunBinding
	generation               generationIdentity
	ledgerCanonical          [32]byte
	recoveryDigest           [32]byte
	dispatch                 runnerPreparedDispatch
	database                 runnerPreparedDatabaseIdentity
	maxAttempts              uint32
	policy                   ExecutionPolicy
	plan                     StatementPlan
	intent                   StatementIntent
	intermediate             StatementIntermediateEvidence
	cursor                   JournalCursor
	intentRecordDigest       Digest
	intermediateRecordDigest Digest
	checkpointDigest         Digest
	ledgerRow                CommitIntentLedgerRow
	ledgerPrefixDigest       Digest
	ledgerHead               string
	ledgerLength             uint32
}

var runnerDurableCommitIntentRegistry sync.Map

func (runner *Runner) appendCurrentCommitIntent(ctx context.Context, readback *runnerReadbackCurrentLedger) (*runnerDurableCommitIntent, error) {
	seed, err := consumeRunnerReadbackCurrentLedger(readback)
	if err != nil {
		return nil, closeRunnerReadbackCurrentLedger(readback, err)
	}
	failClosed := func(primary error) (*runnerDurableCommitIntent, error) {
		return nil, closeRunnerCurrentTransactionResources(seed.session, seed.transaction, seed.key, primary)
	}
	if ctx == nil || runner == nil {
		return failClosed(fail(CodeTransactionBoundary, "runner-commit-intent", "commit intent context or runner is unavailable", nil))
	}
	if contextErr := ctx.Err(); contextErr != nil {
		return failClosed(mapRunnerCommitIntentError(contextErr, "runner-commit-intent", "commit intent append was interrupted"))
	}
	binder, ok := seed.evidence.(runnerCommitIntentRecordBinder)
	if !ok {
		return failClosed(fail(CodeEvidenceRecoveryRequired, "runner-commit-intent-bind", "evidence session cannot bind a commit intent record", nil))
	}
	request := runnerCommitIntentRequestFromSeed(seed)
	journal, cursor, owned, bindErr := binder.bindRunnerCommitIntentRecord(ctx, request)
	if bindErr != nil || journal == nil || !runnerOwnedPointer(journal) || owned == nil || owned.wire.CommitIntent == nil {
		if bindErr == nil {
			bindErr = fail(CodeEvidenceRecoveryRequired, "runner-commit-intent-bind", "commit intent record authority is unavailable", nil)
		}
		return failClosed(mapRunnerCommitIntentError(bindErr, "runner-commit-intent-bind", "commit intent record could not be bound"))
	}
	if !validRunnerCommitIntentBoundRecord(seed, journal, cursor, owned) {
		return failClosed(fail(CodeEvidenceJournalFailed, "runner-commit-intent-bind", "commit intent record authority is contradictory", nil))
	}
	expectedFrame := EvidenceFrame{
		FormatVersion: EvidenceFrameFormat, Sequence: cursor.nextSequence,
		PreviousRecordDigest: cloneDigestPointer(cursor.previousRecordDigest),
		RecordKind:           EvidenceRecordCommitIntent, Record: cloneEvidenceRecord(owned.wire),
	}
	expectedFrame.RecordDigest, err = expectedFrame.ComputeDigest()
	if err != nil || expectedFrame.Validate() != nil {
		return failClosed(fail(CodeEvidenceJournalFailed, "runner-commit-intent-bind", "commit intent frame identity could not be sealed", nil))
	}
	result, appendErr := journal.AppendDurable(ctx, cursor, owned)
	if appendErr != nil || result.outcome != appendOutcomeDurable {
		invalidateRunnerStatementIntentAppendResult(result)
		if appendErr == nil {
			appendErr = fail(CodeEvidenceJournalFailed, "runner-commit-intent-append", "commit intent durability is unknown", nil)
		}
		return failClosed(mapRunnerCommitIntentError(appendErr, "runner-commit-intent-append", "commit intent was not proven durable"))
	}
	durableCursor, resultErr := validateRunnerCommitIntentAppendResult(cursor, seed.generation, expectedFrame.RecordDigest, result)
	if resultErr != nil {
		return failClosed(resultErr)
	}
	snapshot := seed.evidence.RecoverySnapshot()
	newRecoveryDigest, evidenceErr := runnerDurableCommitIntentEvidenceDigest(
		seed.evidence, journal, seed.candidateBinding, seed.generation, seed.maxAttempts,
		durableCursor, seed.intentRecordDigest, seed.intermediateRecordDigest, expectedFrame.RecordDigest,
		&seed.intent, &seed.intermediate, expectedFrame.Record.CommitIntent, snapshot,
	)
	if evidenceErr != nil {
		durableCursor.valid.Store(false)
		return failClosed(evidenceErr)
	}
	boundary, boundaryErr := seed.transaction.Boundary(ctx, seed.key)
	status, statusOK := migrationProjectionTxStatus(seed.transaction)
	if boundaryErr != nil || !statusOK || status != 'T' || boundary.TxStatus != 'T' || boundary.CurrentUser != MigrationOwnerRole || !boundary.LockHeld {
		return failClosed(mapRunnerDatabasePreflightError(boundaryErr, "runner-commit-intent-boundary", "durable commit intent escaped the exact role, status, or advisory lock boundary"))
	}
	durable, sealErr := bindRunnerDurableCommitIntent(seed, journal, durableCursor, expectedFrame, result.candidateCheckpointRecordDigest, newRecoveryDigest, boundary)
	if sealErr != nil {
		durableCursor.valid.Store(false)
		return failClosed(sealErr)
	}
	return durable, nil
}

func consumeRunnerReadbackCurrentLedger(readback *runnerReadbackCurrentLedger) (runnerCommitIntentSeed, error) {
	if !validRunnerReadbackCurrentLedger(readback) {
		return runnerCommitIntentSeed{}, fail(CodeTransactionBoundary, "runner-commit-intent-claim", "ledger readback authority is unavailable or changed", nil)
	}
	if _, ok := readback.evidence.(runnerCommitIntentRecordBinder); !ok {
		return runnerCommitIntentSeed{}, fail(CodeEvidenceRecoveryRequired, "runner-commit-intent-claim", "evidence session lacks the sealed commit-intent binder", nil)
	}
	plan, err := cloneRunnerStatementIntentPlan(readback.plan)
	if err != nil {
		return runnerCommitIntentSeed{}, fail(CodeUntrusted, "runner-commit-intent-claim", "exact final statement plan is unavailable", nil)
	}
	registered, ok := runnerReadbackCurrentLedgerRegistry.LoadAndDelete(readback)
	record, recordOK := registered.(runnerReadbackCurrentLedgerRegistryRecord)
	if !ok || !recordOK || record.prepared != readback || record.binding != readback.binding || record.key != readback.key || record.candidateBinding != readback.candidateBinding || record.cursorValid != readback.cursor.valid || record.canonical != readback.canonical || !sameRunnerOwnedPointer(record.session, readback.session) || !sameRunnerOwnedPointer(record.transaction, readback.transaction) || !sameRunnerOwnedPointer(record.evidence, readback.evidence) || !sameRunnerOwnedPointer(record.journal, readback.journal) {
		return runnerCommitIntentSeed{}, fail(CodeTransactionBoundary, "runner-commit-intent-claim", "ledger readback authority could not be consumed exactly once", nil)
	}
	seed := runnerCommitIntentSeed{
		session: record.session, transaction: record.transaction, evidence: record.evidence, journal: record.journal,
		key: record.key, candidateBinding: record.candidateBinding, generation: readback.generation,
		ledgerCanonical: record.canonical, recoveryDigest: readback.recoveryDigest,
		dispatch: readback.dispatch, database: readback.database, maxAttempts: readback.maxAttempts,
		policy: cloneProjectionValue(readback.policy), plan: plan, intent: cloneProjectionValue(readback.intent),
		intermediate: cloneProjectionValue(readback.intermediate), cursor: readback.cursor.clone(),
		intentRecordDigest: readback.intentRecordDigest, intermediateRecordDigest: readback.intermediateRecordDigest,
		checkpointDigest: readback.checkpointDigest, ledgerRow: cloneProjectionValue(readback.ledgerRow),
		ledgerPrefixDigest: readback.ledgerPrefixDigest, ledgerHead: readback.ledgerHead, ledgerLength: readback.ledgerLength,
	}
	readback.closed = true
	readback.session = nil
	readback.transaction = nil
	readback.evidence = nil
	readback.journal = nil
	readback.binding = nil
	readback.policy = ExecutionPolicy{}
	readback.plan = StatementPlan{}
	readback.intent = StatementIntent{}
	readback.intermediate = StatementIntermediateEvidence{}
	readback.ledgerRow = CommitIntentLedgerRow{}
	return seed, nil
}

func runnerCommitIntentRequestFromSeed(seed runnerCommitIntentSeed) runnerCommitIntentRecordRequest {
	return runnerCommitIntentRecordRequest{
		candidateBinding: seed.candidateBinding, generation: seed.generation, recoveryDigest: seed.recoveryDigest,
		maxAttempts: seed.maxAttempts, planCount: seed.dispatch.planCount, plan: seed.plan,
		intent: seed.intent, intermediate: seed.intermediate, ledgerRow: seed.ledgerRow,
		ledgerPrefixDigest: seed.ledgerPrefixDigest, ledgerHead: seed.ledgerHead, ledgerLength: seed.ledgerLength,
	}
}

func validRunnerCommitIntentBoundRecord(seed runnerCommitIntentSeed, journal EvidenceJournal, cursor JournalCursor, owned *OwnedEvidenceRecord) bool {
	if seed.evidence == nil || journal == nil || !sameRunnerOwnedPointer(seed.evidence.Journal(), journal) || owned == nil || owned.consumed == nil || owned.consumed.Load() || owned.witness == nil || !cursor.Valid() || !sameGenerationIdentity(cursor.generation, seed.generation) || cursor.segmentIndex != 0 || cursor.nextSequence != 3 || cursor.previousRecordDigest == nil || *cursor.previousRecordDigest != seed.intermediateRecordDigest || cursor.latestCheckpointRecordDigest == nil || !sameGenerationIdentity(owned.generation, seed.generation) || !sameCursorIdentity(owned.cursor, cursor) {
		return false
	}
	witness, ok := owned.witness.(ownedCommitIntentWitness)
	commit := owned.wire.CommitIntent
	want, err := buildRunnerCommitIntent(runnerCommitIntentRequestFromSeed(seed))
	if !ok || err != nil || commit == nil || commit.Validate() != nil || witness.priorIntermediate.Record.Intermediate == nil || witness.priorIntermediateStateDigest != seed.intermediate.State.IntermediateStateDigest || witness.lastIntermediateRecordDigest != seed.intermediateRecordDigest || witness.priorIntermediate.RecordDigest != seed.intermediateRecordDigest || !canonicalEqual(*witness.priorIntermediate.Record.Intermediate, seed.intermediate) || !canonicalEqual(*commit, want) || !sameGenerationIdentity(witness.generation, seed.generation) || !sameCursorIdentity(witness.cursor, cursor) {
		return false
	}
	return true
}

func validateRunnerCommitIntentAppendResult(cursor JournalCursor, generation generationIdentity, expectedRecord Digest, result AppendResult) (JournalCursor, error) {
	durable := result.DurableCursor()
	if result.outcome != appendOutcomeDurable || durable == nil || !durable.Valid() || cursor.valid == nil || durable.valid == nil || durable.valid == cursor.valid || cursor.Valid() || !sameGenerationIdentity(cursor.generation, generation) || !sameGenerationIdentity(durable.generation, generation) || cursor.segmentIndex != 0 || cursor.nextSequence != 3 || cursor.previousRecordDigest == nil || cursor.latestCheckpointRecordDigest == nil || result.candidateSequence != cursor.nextSequence || !equalDigestPointer(result.candidatePreviousRecordDigest, cursor.previousRecordDigest) || result.candidateRecordDigest != expectedRecord || result.candidateCheckpointRecordDigest.Validate() != nil || result.rotationHeaderRecordDigest != nil || result.rotationHeaderCheckpointRecordDigest != nil || durable.segmentIndex != cursor.segmentIndex || durable.nextSequence != cursor.nextSequence+1 || durable.previousRecordDigest == nil || *durable.previousRecordDigest != expectedRecord || durable.lineageIndexNextSequence != cursor.lineageIndexNextSequence+1 || durable.lineageIndexPreviousRecordDigest != result.candidateCheckpointRecordDigest || durable.latestCheckpointRecordDigest == nil || *durable.latestCheckpointRecordDigest != result.candidateCheckpointRecordDigest {
		if durable != nil && durable.valid != nil {
			durable.valid.Store(false)
		}
		return JournalCursor{}, fail(CodeEvidenceJournalFailed, "runner-commit-intent-append", "durable commit intent result is contradictory", nil)
	}
	return durable.clone(), nil
}

func runnerDurableCommitIntentEvidenceDigest(evidence EvidenceSession, journal EvidenceJournal, candidateBinding *verifiedEvidenceRunBinding, generation generationIdentity, maxAttempts uint32, cursor JournalCursor, intentRecordDigest, intermediateRecordDigest, commitRecordDigest Digest, intent *StatementIntent, intermediate *StatementIntermediateEvidence, commit *CommitIntent, snapshot *RecoverySnapshot) ([32]byte, error) {
	if evidence == nil || journal == nil || candidateBinding == nil || intent == nil || intermediate == nil || commit == nil || maxAttempts == 0 || intent.AttemptIndex == 0 || intent.AttemptIndex > maxAttempts || intentRecordDigest.Validate() != nil || intermediateRecordDigest.Validate() != nil || commitRecordDigest.Validate() != nil || !cursor.Valid() || snapshot == nil || intermediate.Validate() != nil || commit.Validate() != nil {
		return [32]byte{}, fail(CodeEvidenceJournalFailed, "runner-commit-intent-evidence", "durable commit intent evidence is unavailable", nil)
	}
	current := evidence.CurrentCandidate()
	active := evidence.ActiveGeneration()
	currentJournal := evidence.Journal()
	recoveredIntent := snapshot.lastStatementIntent
	recoveredIntermediate := snapshot.lastIntermediateEvidence
	recoveredCommit := snapshot.commitIntent
	if !validOwnedCurrentCandidate(current) || current.binding != candidateBinding || active.kind != activeGenerationCurrent || !sameGenerationIdentity(active.identity, generation) || !sameRunnerOwnedPointer(active.journal, journal) || !sameRunnerOwnedPointer(currentJournal, journal) || !validRecoverySnapshotForJournal(snapshot, generation, cursor) || snapshot.state != RecoveryDanglingCommitIntent || snapshot.nextPermittedAction != RecoveryReconcileCommit || snapshot.migrationID == nil || *snapshot.migrationID != intent.MigrationID || snapshot.attemptIndex == nil || *snapshot.attemptIndex != intent.AttemptIndex || snapshot.tailDigest != commitRecordDigest || snapshot.lastStatementIntentRecordDigest == nil || *snapshot.lastStatementIntentRecordDigest != intentRecordDigest || snapshot.lastIntermediateEvidenceRecordDigest == nil || *snapshot.lastIntermediateEvidenceRecordDigest != intermediateRecordDigest || snapshot.lastIntermediateStateDigest == nil || *snapshot.lastIntermediateStateDigest != intermediate.State.IntermediateStateDigest || snapshot.lastCommitIntentRecordDigest == nil || *snapshot.lastCommitIntentRecordDigest != commitRecordDigest || recoveredIntent == nil || recoveredIntermediate == nil || recoveredCommit == nil || recoveredIntent.owner != generation.owner || recoveredIntermediate.owner != generation.owner || recoveredCommit.owner != generation.owner || !sameGenerationIdentity(recoveredIntent.generation, generation) || !sameGenerationIdentity(recoveredIntermediate.generation, generation) || !sameGenerationIdentity(recoveredCommit.generation, generation) || !sameCursorIdentity(recoveredIntent.cursor, cursor) || !sameCursorIdentity(recoveredIntermediate.cursor, cursor) || !sameCursorIdentity(recoveredCommit.cursor, cursor) || recoveredIntent.tailDigest != commitRecordDigest || recoveredIntermediate.tailDigest != commitRecordDigest || recoveredCommit.tailDigest != commitRecordDigest || recoveredIntent.recordDigest != intentRecordDigest || recoveredIntermediate.recordDigest != intermediateRecordDigest || recoveredCommit.recordDigest != commitRecordDigest || !canonicalEqual(recoveredIntent.value, *intent) || !canonicalEqual(recoveredIntermediate.value, *intermediate) || !canonicalEqual(recoveredCommit.value, *commit) || snapshot.lineageContinuation != nil || snapshot.lastTerminal != nil || snapshot.lastTerminalDigest != nil || snapshot.lastResolution != nil || snapshot.lastResolutionDigest != nil || !equalDigestPointer(snapshot.previousAttemptTerminalDigest, intent.PreviousAttemptTerminalDigest) {
		return [32]byte{}, fail(CodeEvidenceJournalFailed, "runner-commit-intent-evidence", "durable commit intent recovery boundary is invalid", nil)
	}
	digest := generationJournalRecoveryDigest(snapshot)
	if digest == ([32]byte{}) {
		return [32]byte{}, fail(CodeEvidenceJournalFailed, "runner-commit-intent-evidence", "durable commit intent recovery identity is unavailable", nil)
	}
	return digest, nil
}

func bindRunnerDurableCommitIntent(seed runnerCommitIntentSeed, journal EvidenceJournal, cursor JournalCursor, frame EvidenceFrame, checkpoint Digest, recoveryDigest [32]byte, boundary BoundaryState) (*runnerDurableCommitIntent, error) {
	commit := frame.Record.CommitIntent
	if journal == nil || !runnerOwnedPointer(journal) || !cursor.Valid() || commit == nil || commit.Validate() != nil || frame.RecordDigest.Validate() != nil || checkpoint.Validate() != nil || recoveryDigest == ([32]byte{}) {
		return nil, fail(CodeEvidenceJournalFailed, "runner-commit-intent-seal", "durable commit intent inputs are unavailable", nil)
	}
	plan, err := cloneRunnerStatementIntentPlan(seed.plan)
	if err != nil {
		return nil, fail(CodeUntrusted, "runner-commit-intent-seal", "durable final statement plan is unavailable", nil)
	}
	prepared := &runnerDurableCommitIntent{
		session: seed.session, transaction: seed.transaction, evidence: seed.evidence, journal: journal,
		key: seed.key, candidateBinding: seed.candidateBinding, generation: seed.generation,
		ledgerCanonical: seed.ledgerCanonical, recoveryDigest: recoveryDigest,
		dispatch: seed.dispatch, database: seed.database, maxAttempts: seed.maxAttempts,
		policy: cloneProjectionValue(seed.policy), plan: plan, intent: cloneProjectionValue(seed.intent),
		intermediate: cloneProjectionValue(seed.intermediate), commit: cloneProjectionValue(*commit), cursor: cursor.clone(),
		intentRecordDigest: seed.intentRecordDigest, intermediateRecordDigest: seed.intermediateRecordDigest,
		commitRecordDigest: frame.RecordDigest, checkpointDigest: checkpoint,
		ledgerPrefixDigest: seed.ledgerPrefixDigest, boundary: boundary,
	}
	prepared.self = prepared
	binding := &runnerDurableCommitIntentBinding{
		prepared: prepared, session: seed.session, transaction: seed.transaction, evidence: seed.evidence,
		journal: journal, key: seed.key, candidateBinding: seed.candidateBinding, cursorValid: cursor.valid,
	}
	prepared.binding = binding
	prepared.canonical = runnerDurableCommitIntentDigest(prepared)
	binding.canonical = prepared.canonical
	if prepared.canonical == ([32]byte{}) {
		return nil, fail(CodeEvidenceJournalFailed, "runner-commit-intent-seal", "durable commit intent could not be identified", nil)
	}
	runnerDurableCommitIntentRegistry.Store(prepared, runnerDurableCommitIntentRegistryRecord{
		prepared: prepared, binding: binding, session: seed.session, transaction: seed.transaction,
		evidence: seed.evidence, journal: journal, key: seed.key, candidateBinding: seed.candidateBinding,
		cursorValid: cursor.valid, canonical: prepared.canonical,
	})
	if !validRunnerDurableCommitIntent(prepared) {
		runnerDurableCommitIntentRegistry.Delete(prepared)
		return nil, fail(CodeEvidenceJournalFailed, "runner-commit-intent-seal", "durable commit intent could not be sealed", nil)
	}
	return prepared, nil
}

func validRunnerDurableCommitIntent(prepared *runnerDurableCommitIntent) bool {
	if prepared == nil || prepared.self != prepared || prepared.closed || prepared.binding == nil || prepared.binding.prepared != prepared || prepared.binding.key != prepared.key || prepared.binding.candidateBinding != prepared.candidateBinding || prepared.binding.cursorValid != prepared.cursor.valid || !sameRunnerOwnedPointer(prepared.binding.session, prepared.session) || !sameRunnerOwnedPointer(prepared.binding.transaction, prepared.transaction) || !sameRunnerOwnedPointer(prepared.binding.evidence, prepared.evidence) || !sameRunnerOwnedPointer(prepared.binding.journal, prepared.journal) || prepared.canonical == ([32]byte{}) || prepared.canonical != prepared.binding.canonical || prepared.canonical != runnerDurableCommitIntentDigest(prepared) {
		return false
	}
	registered, ok := runnerDurableCommitIntentRegistry.Load(prepared)
	record, recordOK := registered.(runnerDurableCommitIntentRegistryRecord)
	if !ok || !recordOK || record.prepared != prepared || record.binding != prepared.binding || record.key != prepared.key || record.candidateBinding != prepared.candidateBinding || record.cursorValid != prepared.cursor.valid || record.canonical != prepared.canonical || !sameRunnerOwnedPointer(record.session, prepared.session) || !sameRunnerOwnedPointer(record.transaction, prepared.transaction) || !sameRunnerOwnedPointer(record.evidence, prepared.evidence) || !sameRunnerOwnedPointer(record.journal, prepared.journal) {
		return false
	}
	status, statusOK := migrationProjectionTxStatus(prepared.transaction)
	if !statusOK || status != 'T' {
		return false
	}
	digest, err := runnerDurableCommitIntentEvidenceDigest(
		prepared.evidence, prepared.journal, prepared.candidateBinding, prepared.generation, prepared.maxAttempts,
		prepared.cursor, prepared.intentRecordDigest, prepared.intermediateRecordDigest, prepared.commitRecordDigest,
		&prepared.intent, &prepared.intermediate, &prepared.commit, prepared.evidence.RecoverySnapshot(),
	)
	return err == nil && digest == prepared.recoveryDigest
}

func runnerDurableCommitIntentDigest(prepared *runnerDurableCommitIntent) [32]byte {
	if prepared == nil || prepared.self != prepared || prepared.closed || prepared.session == nil || prepared.transaction == nil || prepared.evidence == nil || prepared.journal == nil || prepared.candidateBinding == nil || prepared.candidateBinding.owner == nil || prepared.generation.owner != prepared.candidateBinding.owner || prepared.candidateBinding.canonical == ([32]byte{}) || prepared.ledgerCanonical == ([32]byte{}) || prepared.recoveryDigest == ([32]byte{}) || prepared.policy.Validate() != nil || prepared.policy.MaxAttempts != uint64(prepared.maxAttempts) || prepared.plan.validateExact() != nil || prepared.plan.StatementIndex != 0 || prepared.dispatch.planCount != 1 || prepared.intent.Validate() != nil || prepared.intermediate.Validate() != nil || !runnerFinalIntermediateShapeMatches(prepared.plan, prepared.intent, prepared.intermediate) || prepared.commit.Validate() != nil || prepared.intentRecordDigest.Validate() != nil || prepared.intermediateRecordDigest.Validate() != nil || prepared.commitRecordDigest.Validate() != nil || prepared.checkpointDigest.Validate() != nil || prepared.ledgerPrefixDigest.Validate() != nil || !prepared.cursor.Valid() || !sameGenerationIdentity(prepared.cursor.generation, prepared.generation) || prepared.cursor.segmentIndex != 0 || prepared.cursor.nextSequence != 4 || prepared.cursor.previousRecordDigest == nil || *prepared.cursor.previousRecordDigest != prepared.commitRecordDigest || prepared.cursor.latestCheckpointRecordDigest == nil || *prepared.cursor.latestCheckpointRecordDigest != prepared.checkpointDigest || prepared.commit.SchemaBundleDigest != prepared.generation.schemaBundleDigest || prepared.commit.MigrationID != prepared.dispatch.migrationID || prepared.commit.AttemptIndex != prepared.dispatch.attemptIndex || prepared.commit.LastIntermediateStateDigest != prepared.intermediate.State.IntermediateStateDigest || prepared.commit.LedgerRow.BundleDigest != prepared.generation.schemaBundleDigest || prepared.commit.LedgerRow.SQLSHA256 != prepared.plan.SQLArtifactSHA256 || prepared.commit.LedgerRow.SQLSizeBytes != prepared.plan.SQLArtifactSizeBytes || prepared.commit.ExpectedLedgerLength != 1 || prepared.commit.ExpectedLedgerHead != prepared.dispatch.migrationID || prepared.database.postgresMajor == 0 || prepared.database.serverVersionNum == 0 || prepared.database.databaseName == "" || prepared.database.sessionUser == "" || prepared.database.currentUser != MigrationOwnerRole || prepared.dispatch.recoveryState != RecoveryBrandNew && prepared.dispatch.recoveryState != RecoveryBrandNewInherited || prepared.dispatch.action != RecoveryBeginFirstAttempt || prepared.dispatch.entryIndex != 0 || prepared.dispatch.planDigest == ([32]byte{}) || prepared.boundary.TxStatus != 'T' || prepared.boundary.CurrentUser != MigrationOwnerRole || !prepared.boundary.LockHeld {
		return [32]byte{}
	}
	want, err := buildRunnerCommitIntent(runnerCommitIntentRecordRequest{
		generation: prepared.generation, maxAttempts: prepared.maxAttempts, planCount: prepared.dispatch.planCount,
		plan: prepared.plan, intent: prepared.intent, intermediate: prepared.intermediate,
		ledgerRow: prepared.commit.LedgerRow, ledgerPrefixDigest: prepared.ledgerPrefixDigest,
		ledgerHead: prepared.commit.ExpectedLedgerHead, ledgerLength: prepared.commit.ExpectedLedgerLength,
	})
	if err != nil || !canonicalEqual(want, prepared.commit) {
		return [32]byte{}
	}
	values := []any{prepared.policy, prepared.plan.exactSentinel(), prepared.intent, prepared.intermediate, prepared.commit, prepared.boundary}
	h := sha256.New()
	h.Write([]byte("cloud-agents-platform-runner-durable-commit-intent/v1\x00"))
	h.Write(prepared.candidateBinding.canonical[:])
	h.Write(prepared.ledgerCanonical[:])
	h.Write(prepared.recoveryDigest[:])
	for _, value := range values {
		canonical, err := canonicalContractKey(value)
		if err != nil || canonical == "" {
			return [32]byte{}
		}
		writeAdmissionString(h, canonical)
	}
	for _, value := range []Digest{
		prepared.generation.executionLineageDigest, prepared.generation.journalIdentityDigest,
		prepared.generation.runnerProjectionDecisionDigest, prepared.generation.schemaBundleDigest,
		prepared.intentRecordDigest, prepared.intermediateRecordDigest, prepared.commitRecordDigest,
		prepared.checkpointDigest, prepared.ledgerPrefixDigest,
	} {
		if value.Validate() != nil {
			return [32]byte{}
		}
		writeAdmissionString(h, value.String())
	}
	writeGenerationJournalCursor(h, prepared.cursor)
	writeAdmissionString(h, strconv.FormatInt(prepared.key, 10))
	writeAdmissionUint(h, uint64(prepared.database.postgresMajor))
	writeAdmissionUint(h, uint64(prepared.database.serverVersionNum))
	writeAdmissionString(h, prepared.database.databaseName)
	writeAdmissionString(h, prepared.database.sessionUser)
	writeAdmissionString(h, prepared.database.currentUser)
	writeAdmissionString(h, string(prepared.dispatch.recoveryState))
	writeAdmissionString(h, string(prepared.dispatch.action))
	writeAdmissionString(h, prepared.dispatch.migrationID)
	writeAdmissionUint(h, uint64(prepared.dispatch.attemptIndex))
	writeAdmissionUint(h, uint64(prepared.dispatch.entryIndex))
	writeAdmissionUint(h, uint64(prepared.dispatch.planCount))
	h.Write(prepared.dispatch.planDigest[:])
	writeAdmissionUint(h, uint64(prepared.maxAttempts))
	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result
}

func closeRunnerDurableCommitIntent(prepared *runnerDurableCommitIntent, primary error) error {
	if prepared == nil {
		return primary
	}
	if prepared.self != prepared {
		return fail(CodeTransactionBoundary, "runner-commit-intent-close", "durable commit intent copy cannot close database authority", nil)
	}
	registered, ok := runnerDurableCommitIntentRegistry.Load(prepared)
	record, recordOK := registered.(runnerDurableCommitIntentRegistryRecord)
	if !ok || !recordOK || record.prepared != prepared || record.binding == nil {
		return fail(CodeTransactionBoundary, "runner-commit-intent-close", "durable commit intent authority is unavailable", nil)
	}
	valid := validRunnerDurableCommitIntent(prepared)
	runnerDurableCommitIntentRegistry.Delete(prepared)
	prepared.closed = true
	prepared.session = nil
	prepared.transaction = nil
	prepared.evidence = nil
	prepared.journal = nil
	prepared.binding = nil
	prepared.policy = ExecutionPolicy{}
	prepared.plan = StatementPlan{}
	prepared.intent = StatementIntent{}
	prepared.intermediate = StatementIntermediateEvidence{}
	prepared.commit = CommitIntent{}
	if !valid {
		primary = fail(CodeTransactionBoundary, "runner-commit-intent-close", "durable commit intent authority changed before close", nil)
	}
	return closeRunnerCurrentTransactionResources(record.session, record.transaction, record.key, primary)
}

func mapRunnerCommitIntentError(err error, op, message string) error {
	var stable *Error
	if errors.As(err, &stable) {
		return fail(stable.Code, op, message, nil)
	}
	if errors.Is(err, context.Canceled) {
		return fail(CodeContextCanceled, op, message, nil)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return fail(CodeDeadlineExceeded, op, message, nil)
	}
	return fail(CodeEvidenceJournalFailed, op, message, nil)
}
