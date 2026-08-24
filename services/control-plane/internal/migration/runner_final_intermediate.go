package migration

import (
	"context"
	"crypto/sha256"
	"errors"
	"strconv"
	"sync"
	"sync/atomic"
)

// runnerDurableFinalIntermediate proves that the final statement intermediate
// and matching lineage checkpoint are durable. It still exposes no ledger
// insert, commit intent, or transaction commit capability.
type runnerDurableFinalIntermediate struct {
	self                     *runnerDurableFinalIntermediate
	binding                  *runnerDurableFinalIntermediateBinding
	session                  DatabaseSession
	transaction              MigrationTransaction
	evidence                 EvidenceSession
	journal                  EvidenceJournal
	key                      int64
	candidateBinding         *verifiedEvidenceRunBinding
	generation               generationIdentity
	preledgerCanonical       [32]byte
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
	boundary                 BoundaryState
	canonical                [32]byte
	closed                   bool
}

type runnerDurableFinalIntermediateBinding struct {
	prepared         *runnerDurableFinalIntermediate
	session          DatabaseSession
	transaction      MigrationTransaction
	evidence         EvidenceSession
	journal          EvidenceJournal
	key              int64
	candidateBinding *verifiedEvidenceRunBinding
	cursorValid      *atomic.Bool
	canonical        [32]byte
}

type runnerDurableFinalIntermediateRegistryRecord struct {
	prepared         *runnerDurableFinalIntermediate
	binding          *runnerDurableFinalIntermediateBinding
	session          DatabaseSession
	transaction      MigrationTransaction
	evidence         EvidenceSession
	journal          EvidenceJournal
	key              int64
	candidateBinding *verifiedEvidenceRunBinding
	cursorValid      *atomic.Bool
	canonical        [32]byte
}

type runnerFinalIntermediateSeed struct {
	session            DatabaseSession
	transaction        MigrationTransaction
	evidence           EvidenceSession
	journal            EvidenceJournal
	key                int64
	candidateBinding   *verifiedEvidenceRunBinding
	generation         generationIdentity
	preledgerCanonical [32]byte
	recoveryDigest     [32]byte
	dispatch           runnerPreparedDispatch
	database           runnerPreparedDatabaseIdentity
	maxAttempts        uint32
	policy             ExecutionPolicy
	plan               StatementPlan
	intent             StatementIntent
	state              StatementIntermediateState
	authorityAfter     ProjectionResultEvidence
	catalogAfter       ProjectionResultEvidence
	preledgerAuthority ProjectionResultEvidence
	preledgerCatalog   ProjectionResultEvidence
	intentRecordDigest Digest
}

var runnerDurableFinalIntermediateRegistry sync.Map

func (runner *Runner) appendCurrentFinalIntermediate(ctx context.Context, preledger *runnerProjectedCurrentPreledger) (*runnerDurableFinalIntermediate, error) {
	seed, err := consumeRunnerProjectedCurrentPreledger(preledger)
	if err != nil {
		return nil, closeRunnerProjectedCurrentPreledger(preledger, err)
	}
	failClosed := func(primary error) (*runnerDurableFinalIntermediate, error) {
		return nil, closeRunnerCurrentTransactionResources(seed.session, seed.transaction, seed.key, primary)
	}
	if ctx == nil || runner == nil {
		return failClosed(fail(CodeTransactionBoundary, "runner-final-intermediate", "final intermediate context or runner is unavailable", nil))
	}
	if contextErr := ctx.Err(); contextErr != nil {
		return failClosed(mapRunnerIntermediateError(contextErr, "runner-final-intermediate", "final intermediate append was interrupted"))
	}
	binder, ok := seed.evidence.(runnerIntermediateRecordBinder)
	if !ok {
		return failClosed(fail(CodeEvidenceRecoveryRequired, "runner-final-intermediate-bind", "evidence session cannot bind a final intermediate record", nil))
	}
	request := runnerIntermediateRequestFromSeed(seed)
	journal, cursor, owned, bindErr := binder.bindRunnerIntermediateRecord(ctx, request)
	if bindErr != nil || journal == nil || !runnerOwnedPointer(journal) || owned == nil || owned.wire.Intermediate == nil {
		if bindErr == nil {
			bindErr = fail(CodeEvidenceRecoveryRequired, "runner-final-intermediate-bind", "final intermediate record authority is unavailable", nil)
		}
		return failClosed(mapRunnerIntermediateError(bindErr, "runner-final-intermediate-bind", "final intermediate record could not be bound"))
	}
	if !validRunnerFinalIntermediateBoundRecord(seed, journal, cursor, owned) {
		return failClosed(fail(CodeEvidenceJournalFailed, "runner-final-intermediate-bind", "final intermediate record authority is contradictory", nil))
	}
	expectedFrame := EvidenceFrame{
		FormatVersion: EvidenceFrameFormat, Sequence: cursor.nextSequence,
		PreviousRecordDigest: cloneDigestPointer(cursor.previousRecordDigest),
		RecordKind:           EvidenceRecordIntermediate, Record: cloneEvidenceRecord(owned.wire),
	}
	expectedFrame.RecordDigest, err = expectedFrame.ComputeDigest()
	if err != nil || expectedFrame.Validate() != nil {
		return failClosed(fail(CodeEvidenceJournalFailed, "runner-final-intermediate-bind", "final intermediate frame identity could not be sealed", nil))
	}
	result, appendErr := journal.AppendDurable(ctx, cursor, owned)
	if appendErr != nil || result.outcome != appendOutcomeDurable {
		invalidateRunnerStatementIntentAppendResult(result)
		if appendErr == nil {
			appendErr = fail(CodeEvidenceJournalFailed, "runner-final-intermediate-append", "final intermediate durability is unknown", nil)
		}
		return failClosed(mapRunnerIntermediateError(appendErr, "runner-final-intermediate-append", "final intermediate was not proven durable"))
	}
	durableCursor, resultErr := validateRunnerFinalIntermediateAppendResult(cursor, seed.generation, expectedFrame.RecordDigest, result)
	if resultErr != nil {
		return failClosed(resultErr)
	}
	snapshot := seed.evidence.RecoverySnapshot()
	newRecoveryDigest, evidenceErr := runnerDurableFinalIntermediateEvidenceDigest(
		seed.evidence, journal, seed.candidateBinding, seed.generation, seed.maxAttempts,
		durableCursor, seed.intentRecordDigest, expectedFrame.RecordDigest, &seed.intent, expectedFrame.Record.Intermediate, snapshot,
	)
	if evidenceErr != nil {
		durableCursor.valid.Store(false)
		return failClosed(evidenceErr)
	}
	boundary, boundaryErr := seed.transaction.Boundary(ctx, seed.key)
	status, statusOK := migrationProjectionTxStatus(seed.transaction)
	if boundaryErr != nil || !statusOK || status != 'T' || boundary.TxStatus != 'T' || boundary.CurrentUser != MigrationOwnerRole || !boundary.LockHeld {
		return failClosed(mapRunnerDatabasePreflightError(boundaryErr, "runner-final-intermediate-boundary", "durable final intermediate escaped the exact role, status, or advisory lock boundary"))
	}
	durable, sealErr := bindRunnerDurableFinalIntermediate(seed, journal, durableCursor, expectedFrame, result.candidateCheckpointRecordDigest, newRecoveryDigest, boundary)
	if sealErr != nil {
		durableCursor.valid.Store(false)
		return failClosed(sealErr)
	}
	return durable, nil
}

func consumeRunnerProjectedCurrentPreledger(preledger *runnerProjectedCurrentPreledger) (runnerFinalIntermediateSeed, error) {
	if !validRunnerProjectedCurrentPreledger(preledger) {
		return runnerFinalIntermediateSeed{}, fail(CodeTransactionBoundary, "runner-final-intermediate-claim", "pre-ledger authority is unavailable or changed", nil)
	}
	if _, ok := preledger.evidence.(runnerIntermediateRecordBinder); !ok {
		return runnerFinalIntermediateSeed{}, fail(CodeEvidenceRecoveryRequired, "runner-final-intermediate-claim", "evidence session lacks the sealed intermediate binder", nil)
	}
	plan, err := cloneRunnerStatementIntentPlan(preledger.plan)
	if err != nil {
		return runnerFinalIntermediateSeed{}, fail(CodeUntrusted, "runner-final-intermediate-claim", "exact final statement plan is unavailable", nil)
	}
	registered, ok := runnerProjectedCurrentPreledgerRegistry.LoadAndDelete(preledger)
	record, recordOK := registered.(runnerProjectedCurrentPreledgerRegistryRecord)
	if !ok || !recordOK || record.prepared != preledger || record.binding != preledger.binding || record.key != preledger.key || record.candidateBinding != preledger.candidateBinding || record.cursorValid != preledger.cursor.valid || record.canonical != preledger.canonical || !sameRunnerOwnedPointer(record.session, preledger.session) || !sameRunnerOwnedPointer(record.transaction, preledger.transaction) || !sameRunnerOwnedPointer(record.evidence, preledger.evidence) || !sameRunnerOwnedPointer(record.journal, preledger.journal) {
		return runnerFinalIntermediateSeed{}, fail(CodeTransactionBoundary, "runner-final-intermediate-claim", "pre-ledger authority could not be consumed exactly once", nil)
	}
	seed := runnerFinalIntermediateSeed{
		session: record.session, transaction: record.transaction, evidence: record.evidence, journal: record.journal,
		key: record.key, candidateBinding: record.candidateBinding, generation: preledger.generation,
		preledgerCanonical: record.canonical, recoveryDigest: preledger.recoveryDigest,
		dispatch: preledger.dispatch, database: preledger.database, maxAttempts: preledger.maxAttempts,
		policy: cloneProjectionValue(preledger.policy), plan: plan, intent: cloneProjectionValue(preledger.intent),
		state: cloneProjectionValue(preledger.state), authorityAfter: cloneProjectionValue(preledger.authorityAfter),
		catalogAfter: cloneProjectionValue(preledger.catalogAfter), preledgerAuthority: cloneProjectionValue(preledger.preledgerAuthority),
		preledgerCatalog: cloneProjectionValue(preledger.preledgerCatalog), intentRecordDigest: preledger.intentRecordDigest,
	}
	preledger.closed = true
	preledger.session = nil
	preledger.transaction = nil
	preledger.evidence = nil
	preledger.journal = nil
	preledger.binding = nil
	preledger.policy = ExecutionPolicy{}
	preledger.plan = StatementPlan{}
	preledger.intent = StatementIntent{}
	preledger.state = StatementIntermediateState{}
	preledger.authorityAfter = ProjectionResultEvidence{}
	preledger.catalogAfter = ProjectionResultEvidence{}
	preledger.preledgerAuthority = ProjectionResultEvidence{}
	preledger.preledgerCatalog = ProjectionResultEvidence{}
	preledger.preledgerCatalogBody = CatalogProjection{}
	return seed, nil
}

func runnerIntermediateRequestFromSeed(seed runnerFinalIntermediateSeed) runnerIntermediateRecordRequest {
	return runnerIntermediateRecordRequest{
		candidateBinding: seed.candidateBinding, generation: seed.generation, recoveryDigest: seed.recoveryDigest,
		maxAttempts: seed.maxAttempts, plan: seed.plan, intent: seed.intent, state: seed.state,
		authorityAfter: seed.authorityAfter, catalogAfter: seed.catalogAfter,
		preledgerAuthority: seed.preledgerAuthority, preledgerCatalog: seed.preledgerCatalog,
	}
}

func validRunnerFinalIntermediateBoundRecord(seed runnerFinalIntermediateSeed, journal EvidenceJournal, cursor JournalCursor, owned *OwnedEvidenceRecord) bool {
	if seed.evidence == nil || journal == nil || !sameRunnerOwnedPointer(seed.evidence.Journal(), journal) || owned == nil || owned.consumed == nil || owned.consumed.Load() || owned.witness == nil || !cursor.Valid() || !sameGenerationIdentity(cursor.generation, seed.generation) || cursor.segmentIndex != 0 || cursor.nextSequence != 2 || cursor.previousRecordDigest == nil || *cursor.previousRecordDigest != seed.intentRecordDigest || cursor.latestCheckpointRecordDigest == nil || !sameGenerationIdentity(owned.generation, seed.generation) || !sameCursorIdentity(owned.cursor, cursor) {
		return false
	}
	witness, ok := owned.witness.(ownedIntermediateWitness)
	intermediate := owned.wire.Intermediate
	want, err := buildRunnerFinalIntermediateEvidence(runnerIntermediateRequestFromSeed(seed))
	if !ok || err != nil || intermediate == nil || intermediate.Validate() != nil || witness.plan.validateExact() != nil || witness.plan.exactCanonical != seed.plan.exactCanonical || witness.stateDigest != seed.state.IntermediateStateDigest || witness.priorIntent.Record.StatementIntent == nil || witness.priorIntent.RecordDigest != seed.intentRecordDigest || !canonicalEqual(*witness.priorIntent.Record.StatementIntent, seed.intent) || !canonicalEqual(*intermediate, want) || !sameGenerationIdentity(witness.generation, seed.generation) || !sameCursorIdentity(witness.cursor, cursor) {
		return false
	}
	return true
}

func validateRunnerFinalIntermediateAppendResult(cursor JournalCursor, generation generationIdentity, expectedRecord Digest, result AppendResult) (JournalCursor, error) {
	durable := result.DurableCursor()
	if result.outcome != appendOutcomeDurable || durable == nil || !durable.Valid() || cursor.valid == nil || durable.valid == nil || durable.valid == cursor.valid || cursor.Valid() || !sameGenerationIdentity(cursor.generation, generation) || !sameGenerationIdentity(durable.generation, generation) || cursor.segmentIndex != 0 || cursor.nextSequence != 2 || cursor.previousRecordDigest == nil || cursor.latestCheckpointRecordDigest == nil || result.candidateSequence != cursor.nextSequence || !equalDigestPointer(result.candidatePreviousRecordDigest, cursor.previousRecordDigest) || result.candidateRecordDigest != expectedRecord || result.candidateCheckpointRecordDigest.Validate() != nil || result.rotationHeaderRecordDigest != nil || result.rotationHeaderCheckpointRecordDigest != nil || durable.segmentIndex != cursor.segmentIndex || durable.nextSequence != cursor.nextSequence+1 || durable.previousRecordDigest == nil || *durable.previousRecordDigest != expectedRecord || durable.lineageIndexNextSequence != cursor.lineageIndexNextSequence+1 || durable.lineageIndexPreviousRecordDigest != result.candidateCheckpointRecordDigest || durable.latestCheckpointRecordDigest == nil || *durable.latestCheckpointRecordDigest != result.candidateCheckpointRecordDigest {
		if durable != nil && durable.valid != nil {
			durable.valid.Store(false)
		}
		return JournalCursor{}, fail(CodeEvidenceJournalFailed, "runner-final-intermediate-append", "durable final intermediate result is contradictory", nil)
	}
	return durable.clone(), nil
}

func runnerDurableFinalIntermediateEvidenceDigest(evidence EvidenceSession, journal EvidenceJournal, candidateBinding *verifiedEvidenceRunBinding, generation generationIdentity, maxAttempts uint32, cursor JournalCursor, intentRecordDigest, recordDigest Digest, intent *StatementIntent, intermediate *StatementIntermediateEvidence, snapshot *RecoverySnapshot) ([32]byte, error) {
	if evidence == nil || journal == nil || candidateBinding == nil || intent == nil || intermediate == nil || maxAttempts == 0 || intent.AttemptIndex == 0 || intent.AttemptIndex > maxAttempts || intentRecordDigest.Validate() != nil || recordDigest.Validate() != nil || !cursor.Valid() || snapshot == nil || intermediate.Validate() != nil {
		return [32]byte{}, fail(CodeEvidenceJournalFailed, "runner-final-intermediate-evidence", "durable final intermediate evidence is unavailable", nil)
	}
	current := evidence.CurrentCandidate()
	active := evidence.ActiveGeneration()
	currentJournal := evidence.Journal()
	wantAction := recoveryAbortAction(intent.AttemptIndex, maxAttempts)
	recoveredIntent := snapshot.lastStatementIntent
	recoveredIntermediate := snapshot.lastIntermediateEvidence
	if !validOwnedCurrentCandidate(current) || current.binding != candidateBinding || active.kind != activeGenerationCurrent || !sameGenerationIdentity(active.identity, generation) || !sameRunnerOwnedPointer(active.journal, journal) || !sameRunnerOwnedPointer(currentJournal, journal) || !validRecoverySnapshotForJournal(snapshot, generation, cursor) || snapshot.state != RecoveryDanglingIntermediate || snapshot.nextPermittedAction != wantAction || snapshot.migrationID == nil || *snapshot.migrationID != intent.MigrationID || snapshot.attemptIndex == nil || *snapshot.attemptIndex != intent.AttemptIndex || snapshot.tailDigest != recordDigest || snapshot.lastStatementIntentRecordDigest == nil || *snapshot.lastStatementIntentRecordDigest != intentRecordDigest || snapshot.lastIntermediateEvidenceRecordDigest == nil || *snapshot.lastIntermediateEvidenceRecordDigest != recordDigest || snapshot.lastIntermediateStateDigest == nil || *snapshot.lastIntermediateStateDigest != intermediate.State.IntermediateStateDigest || recoveredIntent == nil || recoveredIntent.owner != generation.owner || !sameGenerationIdentity(recoveredIntent.generation, generation) || !sameCursorIdentity(recoveredIntent.cursor, cursor) || recoveredIntent.tailDigest != recordDigest || recoveredIntent.recordDigest != intentRecordDigest || !canonicalEqual(recoveredIntent.value, *intent) || recoveredIntermediate == nil || recoveredIntermediate.owner != generation.owner || !sameGenerationIdentity(recoveredIntermediate.generation, generation) || !sameCursorIdentity(recoveredIntermediate.cursor, cursor) || recoveredIntermediate.tailDigest != recordDigest || recoveredIntermediate.recordDigest != recordDigest || !canonicalEqual(recoveredIntermediate.value, *intermediate) || snapshot.lineageContinuation != nil || snapshot.commitIntent != nil || snapshot.lastCommitIntentRecordDigest != nil || snapshot.lastTerminal != nil || snapshot.lastTerminalDigest != nil || snapshot.lastResolution != nil || snapshot.lastResolutionDigest != nil || !equalDigestPointer(snapshot.previousAttemptTerminalDigest, intent.PreviousAttemptTerminalDigest) {
		return [32]byte{}, fail(CodeEvidenceJournalFailed, "runner-final-intermediate-evidence", "durable final intermediate recovery boundary is invalid", nil)
	}
	digest := generationJournalRecoveryDigest(snapshot)
	if digest == ([32]byte{}) {
		return [32]byte{}, fail(CodeEvidenceJournalFailed, "runner-final-intermediate-evidence", "durable final intermediate recovery identity is unavailable", nil)
	}
	return digest, nil
}

func bindRunnerDurableFinalIntermediate(seed runnerFinalIntermediateSeed, journal EvidenceJournal, cursor JournalCursor, frame EvidenceFrame, checkpoint Digest, recoveryDigest [32]byte, boundary BoundaryState) (*runnerDurableFinalIntermediate, error) {
	intermediate := frame.Record.Intermediate
	if journal == nil || !runnerOwnedPointer(journal) || !cursor.Valid() || intermediate == nil || intermediate.Validate() != nil || frame.RecordDigest.Validate() != nil || checkpoint.Validate() != nil || recoveryDigest == ([32]byte{}) {
		return nil, fail(CodeEvidenceJournalFailed, "runner-final-intermediate-seal", "durable final intermediate inputs are unavailable", nil)
	}
	plan, err := cloneRunnerStatementIntentPlan(seed.plan)
	if err != nil {
		return nil, fail(CodeUntrusted, "runner-final-intermediate-seal", "durable final statement plan is unavailable", nil)
	}
	prepared := &runnerDurableFinalIntermediate{
		session: seed.session, transaction: seed.transaction, evidence: seed.evidence, journal: journal,
		key: seed.key, candidateBinding: seed.candidateBinding, generation: seed.generation,
		preledgerCanonical: seed.preledgerCanonical, recoveryDigest: recoveryDigest,
		dispatch: seed.dispatch, database: seed.database, maxAttempts: seed.maxAttempts,
		policy: cloneProjectionValue(seed.policy), plan: plan, intent: cloneProjectionValue(seed.intent), intermediate: cloneProjectionValue(*intermediate),
		cursor: cursor.clone(), intentRecordDigest: seed.intentRecordDigest, intermediateRecordDigest: frame.RecordDigest,
		checkpointDigest: checkpoint, boundary: boundary,
	}
	prepared.self = prepared
	binding := &runnerDurableFinalIntermediateBinding{
		prepared: prepared, session: seed.session, transaction: seed.transaction, evidence: seed.evidence, journal: journal,
		key: seed.key, candidateBinding: seed.candidateBinding, cursorValid: cursor.valid,
	}
	prepared.binding = binding
	prepared.canonical = runnerDurableFinalIntermediateDigest(prepared)
	binding.canonical = prepared.canonical
	if prepared.canonical == ([32]byte{}) {
		return nil, fail(CodeEvidenceJournalFailed, "runner-final-intermediate-seal", "durable final intermediate could not be identified", nil)
	}
	runnerDurableFinalIntermediateRegistry.Store(prepared, runnerDurableFinalIntermediateRegistryRecord{
		prepared: prepared, binding: binding, session: seed.session, transaction: seed.transaction,
		evidence: seed.evidence, journal: journal, key: seed.key, candidateBinding: seed.candidateBinding,
		cursorValid: cursor.valid, canonical: prepared.canonical,
	})
	if !validRunnerDurableFinalIntermediate(prepared) {
		runnerDurableFinalIntermediateRegistry.Delete(prepared)
		return nil, fail(CodeEvidenceJournalFailed, "runner-final-intermediate-seal", "durable final intermediate could not be sealed", nil)
	}
	return prepared, nil
}

func validRunnerDurableFinalIntermediate(prepared *runnerDurableFinalIntermediate) bool {
	if prepared == nil || prepared.self != prepared || prepared.closed || prepared.binding == nil || prepared.binding.prepared != prepared || prepared.binding.key != prepared.key || prepared.binding.candidateBinding != prepared.candidateBinding || prepared.binding.cursorValid != prepared.cursor.valid || !sameRunnerOwnedPointer(prepared.binding.session, prepared.session) || !sameRunnerOwnedPointer(prepared.binding.transaction, prepared.transaction) || !sameRunnerOwnedPointer(prepared.binding.evidence, prepared.evidence) || !sameRunnerOwnedPointer(prepared.binding.journal, prepared.journal) || prepared.canonical == ([32]byte{}) || prepared.canonical != prepared.binding.canonical || prepared.canonical != runnerDurableFinalIntermediateDigest(prepared) {
		return false
	}
	registered, ok := runnerDurableFinalIntermediateRegistry.Load(prepared)
	record, recordOK := registered.(runnerDurableFinalIntermediateRegistryRecord)
	if !ok || !recordOK || record.prepared != prepared || record.binding != prepared.binding || record.key != prepared.key || record.candidateBinding != prepared.candidateBinding || record.cursorValid != prepared.cursor.valid || record.canonical != prepared.canonical || !sameRunnerOwnedPointer(record.session, prepared.session) || !sameRunnerOwnedPointer(record.transaction, prepared.transaction) || !sameRunnerOwnedPointer(record.evidence, prepared.evidence) || !sameRunnerOwnedPointer(record.journal, prepared.journal) {
		return false
	}
	status, statusOK := migrationProjectionTxStatus(prepared.transaction)
	if !statusOK || status != 'T' {
		return false
	}
	digest, err := runnerDurableFinalIntermediateEvidenceDigest(
		prepared.evidence, prepared.journal, prepared.candidateBinding, prepared.generation, prepared.maxAttempts,
		prepared.cursor, prepared.intentRecordDigest, prepared.intermediateRecordDigest, &prepared.intent, &prepared.intermediate, prepared.evidence.RecoverySnapshot(),
	)
	return err == nil && digest == prepared.recoveryDigest
}

func runnerDurableFinalIntermediateDigest(prepared *runnerDurableFinalIntermediate) [32]byte {
	if prepared == nil || prepared.self != prepared || prepared.closed || prepared.session == nil || prepared.transaction == nil || prepared.evidence == nil || prepared.journal == nil || prepared.candidateBinding == nil || prepared.candidateBinding.owner == nil || prepared.generation.owner != prepared.candidateBinding.owner || prepared.candidateBinding.canonical == ([32]byte{}) || prepared.preledgerCanonical == ([32]byte{}) || prepared.recoveryDigest == ([32]byte{}) || prepared.policy.Validate() != nil || prepared.policy.MaxAttempts != uint64(prepared.maxAttempts) || prepared.plan.validateExact() != nil || prepared.plan.StatementIndex+1 != prepared.dispatch.planCount || prepared.intent.Validate() != nil || prepared.intermediate.Validate() != nil || !runnerFinalIntermediateShapeMatches(prepared.plan, prepared.intent, prepared.intermediate) || prepared.intentRecordDigest.Validate() != nil || prepared.intermediateRecordDigest.Validate() != nil || prepared.checkpointDigest.Validate() != nil || !prepared.cursor.Valid() || !sameGenerationIdentity(prepared.cursor.generation, prepared.generation) || prepared.cursor.segmentIndex != 0 || prepared.cursor.nextSequence != 3 || prepared.cursor.previousRecordDigest == nil || *prepared.cursor.previousRecordDigest != prepared.intermediateRecordDigest || prepared.cursor.latestCheckpointRecordDigest == nil || *prepared.cursor.latestCheckpointRecordDigest != prepared.checkpointDigest || prepared.database.postgresMajor == 0 || prepared.database.serverVersionNum == 0 || prepared.database.databaseName == "" || prepared.database.sessionUser == "" || prepared.database.currentUser != MigrationOwnerRole || prepared.dispatch.recoveryState != RecoveryBrandNew && prepared.dispatch.recoveryState != RecoveryBrandNewInherited || prepared.dispatch.action != RecoveryBeginFirstAttempt || prepared.dispatch.migrationID != prepared.intent.MigrationID || prepared.dispatch.attemptIndex != prepared.intent.AttemptIndex || prepared.dispatch.entryIndex != 0 || prepared.dispatch.planCount == 0 || prepared.dispatch.planDigest == ([32]byte{}) || prepared.boundary.TxStatus != 'T' || prepared.boundary.CurrentUser != MigrationOwnerRole || !prepared.boundary.LockHeld {
		return [32]byte{}
	}
	values := []any{prepared.policy, prepared.plan.exactSentinel(), prepared.intent, prepared.intermediate, prepared.boundary}
	h := sha256.New()
	h.Write([]byte("cloud-agents-platform-runner-durable-final-intermediate/v1\x00"))
	h.Write(prepared.candidateBinding.canonical[:])
	h.Write(prepared.preledgerCanonical[:])
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
		prepared.intentRecordDigest, prepared.intermediateRecordDigest, prepared.checkpointDigest,
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

func closeRunnerDurableFinalIntermediate(prepared *runnerDurableFinalIntermediate, primary error) error {
	if prepared == nil {
		return primary
	}
	if prepared.self != prepared {
		return fail(CodeTransactionBoundary, "runner-final-intermediate-close", "durable final intermediate copy cannot close database authority", nil)
	}
	registered, ok := runnerDurableFinalIntermediateRegistry.Load(prepared)
	record, recordOK := registered.(runnerDurableFinalIntermediateRegistryRecord)
	if !ok || !recordOK || record.prepared != prepared || record.binding == nil {
		return fail(CodeTransactionBoundary, "runner-final-intermediate-close", "durable final intermediate authority is unavailable", nil)
	}
	valid := validRunnerDurableFinalIntermediate(prepared)
	runnerDurableFinalIntermediateRegistry.Delete(prepared)
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
	if !valid {
		primary = fail(CodeTransactionBoundary, "runner-final-intermediate-close", "durable final intermediate authority changed before close", nil)
	}
	return closeRunnerCurrentTransactionResources(record.session, record.transaction, record.key, primary)
}

func mapRunnerIntermediateError(err error, op, message string) error {
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
