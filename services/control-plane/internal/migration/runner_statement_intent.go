package migration

import (
	"context"
	"crypto/sha256"
	"errors"
	"strconv"
	"sync"
	"sync/atomic"
)

// runnerDurableCurrentStatementIntent proves that the exact statement-zero
// preflight was consumed and its StatementIntent plus matching lineage
// checkpoint are durable. It exposes no SQL, ledger, or commit method.
type runnerDurableCurrentStatementIntent struct {
	self               *runnerDurableCurrentStatementIntent
	binding            *runnerDurableCurrentStatementIntentBinding
	session            DatabaseSession
	transaction        MigrationTransaction
	evidence           EvidenceSession
	journal            EvidenceJournal
	key                int64
	candidateBinding   *verifiedEvidenceRunBinding
	generation         generationIdentity
	statementCanonical [32]byte
	recoveryDigest     [32]byte
	dispatch           runnerPreparedDispatch
	database           runnerPreparedDatabaseIdentity
	maxAttempts        uint32
	policy             ExecutionPolicy
	plan               StatementPlan
	intent             StatementIntent
	cursor             JournalCursor
	intentRecordDigest Digest
	checkpointDigest   Digest
	canonical          [32]byte
	closed             bool
}

type runnerDurableCurrentStatementIntentBinding struct {
	prepared         *runnerDurableCurrentStatementIntent
	session          DatabaseSession
	transaction      MigrationTransaction
	evidence         EvidenceSession
	journal          EvidenceJournal
	key              int64
	candidateBinding *verifiedEvidenceRunBinding
	cursorValid      *atomic.Bool
	canonical        [32]byte
}

type runnerDurableCurrentStatementIntentRegistryRecord struct {
	prepared         *runnerDurableCurrentStatementIntent
	binding          *runnerDurableCurrentStatementIntentBinding
	session          DatabaseSession
	transaction      MigrationTransaction
	evidence         EvidenceSession
	journal          EvidenceJournal
	key              int64
	candidateBinding *verifiedEvidenceRunBinding
	cursorValid      *atomic.Bool
	canonical        [32]byte
}

type runnerDurableCurrentStatementIntentSeed struct {
	session            DatabaseSession
	transaction        MigrationTransaction
	evidence           EvidenceSession
	key                int64
	candidateBinding   *verifiedEvidenceRunBinding
	generation         generationIdentity
	recoveryDigest     [32]byte
	statementCanonical [32]byte
	dispatch           runnerPreparedDispatch
	database           runnerPreparedDatabaseIdentity
	maxAttempts        uint32
	policy             ExecutionPolicy
	plan               StatementPlan
	authorityBefore    ProjectionResultEvidence
	catalogBefore      ProjectionResultEvidence
}

var runnerDurableCurrentStatementIntentRegistry sync.Map

func (runner *Runner) appendCurrentStatementIntent(ctx context.Context, prepared *runnerPreparedCurrentStatement) (*runnerDurableCurrentStatementIntent, error) {
	seed, err := consumeRunnerPreparedCurrentStatement(prepared)
	if err != nil {
		return nil, closeRunnerPreparedCurrentStatement(prepared, err)
	}
	failClosed := func(primary error) (*runnerDurableCurrentStatementIntent, error) {
		return nil, closeRunnerCurrentTransactionResources(seed.session, seed.transaction, seed.key, primary)
	}
	binder, ok := seed.evidence.(runnerStatementIntentRecordBinder)
	if !ok {
		return failClosed(fail(CodeEvidenceRecoveryRequired, "runner-statement-intent-bind", "evidence session cannot bind a statement intent record", nil))
	}
	journal, cursor, owned, bindErr := binder.bindRunnerStatementIntentRecord(ctx, runnerStatementIntentRecordRequest{
		candidateBinding: seed.candidateBinding, generation: seed.generation, recoveryDigest: seed.recoveryDigest,
		maxAttempts: seed.maxAttempts, plan: seed.plan, authorityBefore: seed.authorityBefore, catalogBefore: seed.catalogBefore,
	})
	if bindErr != nil || journal == nil || !runnerOwnedPointer(journal) || owned == nil || owned.wire.StatementIntent == nil {
		if bindErr == nil {
			bindErr = fail(CodeEvidenceRecoveryRequired, "runner-statement-intent-bind", "statement intent record authority is unavailable", nil)
		}
		return failClosed(mapRunnerStatementIntentError(bindErr, "runner-statement-intent-bind", "statement intent record could not be bound"))
	}
	if !validRunnerStatementIntentBoundRecord(seed, journal, cursor, owned) {
		return failClosed(fail(CodeEvidenceJournalFailed, "runner-statement-intent-bind", "statement intent record authority is contradictory", nil))
	}
	expectedFrame := EvidenceFrame{
		FormatVersion: EvidenceFrameFormat, Sequence: cursor.nextSequence,
		PreviousRecordDigest: cloneDigestPointer(cursor.previousRecordDigest),
		RecordKind:           EvidenceRecordStatementIntent, Record: cloneEvidenceRecord(owned.wire),
	}
	expectedFrame.RecordDigest, err = expectedFrame.ComputeDigest()
	if err != nil || expectedFrame.Validate() != nil {
		return failClosed(fail(CodeEvidenceJournalFailed, "runner-statement-intent-bind", "statement intent frame identity could not be sealed", nil))
	}
	result, appendErr := journal.AppendDurable(ctx, cursor, owned)
	if appendErr != nil || result.outcome != appendOutcomeDurable {
		invalidateRunnerStatementIntentAppendResult(result)
		if appendErr == nil {
			appendErr = fail(CodeEvidenceJournalFailed, "runner-statement-intent-append", "statement intent durability is unknown", nil)
		}
		return failClosed(mapRunnerStatementIntentError(appendErr, "runner-statement-intent-append", "statement intent was not proven durable"))
	}
	durableCursor, resultErr := validateRunnerStatementIntentAppendResult(cursor, seed.generation, expectedFrame.RecordDigest, result)
	if resultErr != nil {
		return failClosed(resultErr)
	}
	snapshot := seed.evidence.RecoverySnapshot()
	newRecoveryDigest, evidenceErr := runnerDurableStatementIntentEvidenceDigest(seed.evidence, journal, seed.candidateBinding, seed.generation, seed.maxAttempts, durableCursor, expectedFrame.RecordDigest, expectedFrame.Record.StatementIntent, snapshot)
	if evidenceErr != nil {
		durableCursor.valid.Store(false)
		return failClosed(evidenceErr)
	}
	durable, sealErr := bindRunnerDurableCurrentStatementIntent(seed, journal, durableCursor, expectedFrame, result.candidateCheckpointRecordDigest, newRecoveryDigest)
	if sealErr != nil {
		durableCursor.valid.Store(false)
		return failClosed(sealErr)
	}
	return durable, nil
}

func invalidateRunnerStatementIntentAppendResult(result AppendResult) {
	if cursor := result.DurableCursor(); cursor != nil && cursor.valid != nil {
		cursor.valid.Store(false)
	}
}

func validRunnerStatementIntentBoundRecord(seed runnerDurableCurrentStatementIntentSeed, journal EvidenceJournal, cursor JournalCursor, owned *OwnedEvidenceRecord) bool {
	if seed.evidence == nil || journal == nil || !sameRunnerOwnedPointer(seed.evidence.Journal(), journal) || owned == nil || owned.consumed == nil || owned.consumed.Load() || owned.witness == nil || !cursor.Valid() || !sameGenerationIdentity(cursor.generation, seed.generation) || cursor.segmentIndex != 0 || cursor.nextSequence != 1 || cursor.previousRecordDigest == nil || cursor.latestCheckpointRecordDigest != nil || !sameGenerationIdentity(owned.generation, seed.generation) || !sameCursorIdentity(owned.cursor, cursor) {
		return false
	}
	witness, ok := owned.witness.(ownedStatementIntentWitness)
	intent := owned.wire.StatementIntent
	if !ok || intent == nil || intent.Validate() != nil || witness.plan.validateExact() != nil || witness.plan.exactCanonical != seed.plan.exactCanonical || !sameGenerationIdentity(witness.generation, seed.generation) || !sameCursorIdentity(witness.cursor, cursor) || intent.SchemaBundleDigest != seed.generation.schemaBundleDigest || intent.AttemptIndex != 1 || intent.PreviousAttemptTerminalDigest != nil || intent.PreviousIntermediateStateDigest != nil || !planMatchesIntent(exactStatementWitnessFromPlan(seed.plan, 1), *intent) || !projectionEvidenceEqual(intent.AuthorityBeforeResult, seed.authorityBefore) || !projectionEvidenceEqual(intent.CatalogBeforeResult, seed.catalogBefore) {
		return false
	}
	return true
}

func consumeRunnerPreparedCurrentStatement(prepared *runnerPreparedCurrentStatement) (runnerDurableCurrentStatementIntentSeed, error) {
	if !validRunnerPreparedCurrentStatement(prepared) {
		return runnerDurableCurrentStatementIntentSeed{}, fail(CodeTransactionBoundary, "runner-statement-intent-claim", "statement preflight authority is unavailable or changed", nil)
	}
	if _, ok := prepared.evidence.(runnerStatementIntentRecordBinder); !ok {
		return runnerDurableCurrentStatementIntentSeed{}, fail(CodeEvidenceRecoveryRequired, "runner-statement-intent-claim", "evidence session lacks the sealed statement binder", nil)
	}
	plan, err := cloneRunnerStatementIntentPlan(prepared.plan)
	if err != nil || runnerStatementPlanDigest(plan) != prepared.statementPlanDigest || plan.StatementIndex != prepared.statementIndex {
		return runnerDurableCurrentStatementIntentSeed{}, fail(CodeUntrusted, "runner-statement-intent-claim", "exact statement plan differs from the prepared preflight", nil)
	}
	registered, ok := runnerPreparedCurrentStatementRegistry.LoadAndDelete(prepared)
	record, recordOK := registered.(runnerPreparedCurrentStatementRegistryRecord)
	if !ok || !recordOK || record.prepared != prepared || record.binding != prepared.binding || record.key != prepared.key || record.candidateBinding != prepared.candidateBinding || record.canonical != prepared.canonical || !sameRunnerOwnedPointer(record.session, prepared.session) || !sameRunnerOwnedPointer(record.transaction, prepared.transaction) || !sameRunnerOwnedPointer(record.evidence, prepared.evidence) {
		return runnerDurableCurrentStatementIntentSeed{}, fail(CodeTransactionBoundary, "runner-statement-intent-claim", "statement preflight authority could not be consumed exactly once", nil)
	}
	seed := runnerDurableCurrentStatementIntentSeed{
		session: record.session, transaction: record.transaction, evidence: record.evidence, key: record.key,
		candidateBinding: record.candidateBinding, generation: prepared.generation, recoveryDigest: prepared.recoveryDigest,
		statementCanonical: record.canonical, dispatch: prepared.dispatch, database: prepared.database,
		maxAttempts: prepared.maxAttempts, policy: cloneProjectionValue(prepared.policy), plan: plan,
		authorityBefore: cloneProjectionValue(prepared.authorityBefore), catalogBefore: cloneProjectionValue(prepared.catalogBefore),
	}
	prepared.closed = true
	prepared.session = nil
	prepared.transaction = nil
	prepared.evidence = nil
	prepared.binding = nil
	prepared.policy = ExecutionPolicy{}
	prepared.plan = StatementPlan{}
	prepared.authorityBefore = ProjectionResultEvidence{}
	prepared.catalogBefore = ProjectionResultEvidence{}
	return seed, nil
}

func validateRunnerStatementIntentAppendResult(cursor JournalCursor, generation generationIdentity, expectedRecord Digest, result AppendResult) (JournalCursor, error) {
	durable := result.DurableCursor()
	if result.outcome != appendOutcomeDurable || durable == nil || !durable.Valid() || cursor.valid == nil || durable.valid == nil || durable.valid == cursor.valid || cursor.Valid() || !sameGenerationIdentity(cursor.generation, generation) || !sameGenerationIdentity(durable.generation, generation) || cursor.segmentIndex != 0 || cursor.nextSequence != 1 || cursor.previousRecordDigest == nil || cursor.latestCheckpointRecordDigest != nil || result.candidateSequence != cursor.nextSequence || !equalDigestPointer(result.candidatePreviousRecordDigest, cursor.previousRecordDigest) || result.candidateRecordDigest != expectedRecord || result.candidateCheckpointRecordDigest.Validate() != nil || result.rotationHeaderRecordDigest != nil || result.rotationHeaderCheckpointRecordDigest != nil || durable.segmentIndex != cursor.segmentIndex || durable.nextSequence != cursor.nextSequence+1 || durable.previousRecordDigest == nil || *durable.previousRecordDigest != expectedRecord || durable.lineageIndexNextSequence != cursor.lineageIndexNextSequence+1 || durable.lineageIndexPreviousRecordDigest != result.candidateCheckpointRecordDigest || durable.latestCheckpointRecordDigest == nil || *durable.latestCheckpointRecordDigest != result.candidateCheckpointRecordDigest {
		if durable != nil && durable.valid != nil {
			durable.valid.Store(false)
		}
		return JournalCursor{}, fail(CodeEvidenceJournalFailed, "runner-statement-intent-append", "durable statement intent result is contradictory", nil)
	}
	return durable.clone(), nil
}

func runnerDurableStatementIntentEvidenceDigest(evidence EvidenceSession, journal EvidenceJournal, candidateBinding *verifiedEvidenceRunBinding, generation generationIdentity, maxAttempts uint32, cursor JournalCursor, recordDigest Digest, intent *StatementIntent, snapshot *RecoverySnapshot) ([32]byte, error) {
	if evidence == nil || journal == nil || candidateBinding == nil || intent == nil || maxAttempts == 0 || intent.AttemptIndex == 0 || intent.AttemptIndex > maxAttempts || recordDigest.Validate() != nil || !cursor.Valid() || snapshot == nil {
		return [32]byte{}, fail(CodeEvidenceJournalFailed, "runner-statement-intent-evidence", "durable statement intent evidence is unavailable", nil)
	}
	current := evidence.CurrentCandidate()
	active := evidence.ActiveGeneration()
	currentJournal := evidence.Journal()
	wantAction := RecoveryAppendAbortedTerminal
	if intent.AttemptIndex < maxAttempts {
		wantAction = RecoveryAppendAbortedRetryable
	}
	recovered := snapshot.lastStatementIntent
	if !validOwnedCurrentCandidate(current) || current.binding != candidateBinding || active.kind != activeGenerationCurrent || !sameGenerationIdentity(active.identity, generation) || !sameRunnerOwnedPointer(active.journal, journal) || !sameRunnerOwnedPointer(currentJournal, journal) || !validRecoverySnapshotForJournal(snapshot, generation, cursor) || snapshot.state != RecoveryDanglingStatementIntent || snapshot.nextPermittedAction != wantAction || snapshot.migrationID == nil || *snapshot.migrationID != intent.MigrationID || snapshot.attemptIndex == nil || *snapshot.attemptIndex != intent.AttemptIndex || snapshot.tailDigest != recordDigest || snapshot.lastStatementIntentRecordDigest == nil || *snapshot.lastStatementIntentRecordDigest != recordDigest || recovered == nil || recovered.owner != generation.owner || !sameGenerationIdentity(recovered.generation, generation) || !sameCursorIdentity(recovered.cursor, cursor) || recovered.tailDigest != recordDigest || recovered.recordDigest != recordDigest || !canonicalEqual(recovered.value, *intent) || snapshot.lineageContinuation != nil || snapshot.lastIntermediateEvidence != nil || snapshot.commitIntent != nil || snapshot.lastTerminal != nil || snapshot.lastResolution != nil || snapshot.lastTerminalDigest != nil || snapshot.lastResolutionDigest != nil || snapshot.lastIntermediateEvidenceRecordDigest != nil || snapshot.lastCommitIntentRecordDigest != nil || snapshot.previousAttemptTerminalDigest != nil || snapshot.lastIntermediateStateDigest != nil {
		return [32]byte{}, fail(CodeEvidenceJournalFailed, "runner-statement-intent-evidence", "durable statement intent recovery boundary is invalid", nil)
	}
	digest := generationJournalRecoveryDigest(snapshot)
	if digest == ([32]byte{}) {
		return [32]byte{}, fail(CodeEvidenceJournalFailed, "runner-statement-intent-evidence", "durable statement intent recovery identity is unavailable", nil)
	}
	return digest, nil
}

func bindRunnerDurableCurrentStatementIntent(seed runnerDurableCurrentStatementIntentSeed, journal EvidenceJournal, cursor JournalCursor, frame EvidenceFrame, checkpoint Digest, recoveryDigest [32]byte) (*runnerDurableCurrentStatementIntent, error) {
	if journal == nil || !runnerOwnedPointer(journal) || !cursor.Valid() || frame.Record.StatementIntent == nil || frame.RecordDigest.Validate() != nil || checkpoint.Validate() != nil || recoveryDigest == ([32]byte{}) {
		return nil, fail(CodeEvidenceJournalFailed, "runner-statement-intent-seal", "durable statement intent inputs are unavailable", nil)
	}
	plan, err := cloneRunnerStatementIntentPlan(seed.plan)
	if err != nil {
		return nil, fail(CodeUntrusted, "runner-statement-intent-seal", "durable statement plan is unavailable", nil)
	}
	prepared := &runnerDurableCurrentStatementIntent{
		session: seed.session, transaction: seed.transaction, evidence: seed.evidence, journal: journal, key: seed.key,
		candidateBinding: seed.candidateBinding, generation: seed.generation, statementCanonical: seed.statementCanonical,
		recoveryDigest: recoveryDigest, dispatch: seed.dispatch, database: seed.database, maxAttempts: seed.maxAttempts,
		policy: cloneProjectionValue(seed.policy),
		plan:   plan, intent: cloneProjectionValue(*frame.Record.StatementIntent), cursor: cursor.clone(),
		intentRecordDigest: frame.RecordDigest, checkpointDigest: checkpoint,
	}
	prepared.self = prepared
	binding := &runnerDurableCurrentStatementIntentBinding{
		prepared: prepared, session: seed.session, transaction: seed.transaction, evidence: seed.evidence, journal: journal,
		key: seed.key, candidateBinding: seed.candidateBinding, cursorValid: cursor.valid,
	}
	prepared.binding = binding
	prepared.canonical = runnerDurableCurrentStatementIntentDigest(prepared)
	binding.canonical = prepared.canonical
	if prepared.canonical == ([32]byte{}) {
		return nil, fail(CodeEvidenceJournalFailed, "runner-statement-intent-seal", "durable statement intent could not be identified", nil)
	}
	runnerDurableCurrentStatementIntentRegistry.Store(prepared, runnerDurableCurrentStatementIntentRegistryRecord{
		prepared: prepared, binding: binding, session: seed.session, transaction: seed.transaction, evidence: seed.evidence,
		journal: journal, key: seed.key, candidateBinding: seed.candidateBinding, cursorValid: cursor.valid, canonical: prepared.canonical,
	})
	if !validRunnerDurableCurrentStatementIntent(prepared) {
		runnerDurableCurrentStatementIntentRegistry.Delete(prepared)
		return nil, fail(CodeEvidenceJournalFailed, "runner-statement-intent-seal", "durable statement intent could not be sealed", nil)
	}
	return prepared, nil
}

func validRunnerDurableCurrentStatementIntent(prepared *runnerDurableCurrentStatementIntent) bool {
	if prepared == nil || prepared.self != prepared || prepared.closed || prepared.binding == nil || prepared.binding.prepared != prepared || prepared.binding.key != prepared.key || prepared.binding.candidateBinding != prepared.candidateBinding || prepared.binding.cursorValid != prepared.cursor.valid || !sameRunnerOwnedPointer(prepared.binding.session, prepared.session) || !sameRunnerOwnedPointer(prepared.binding.transaction, prepared.transaction) || !sameRunnerOwnedPointer(prepared.binding.evidence, prepared.evidence) || !sameRunnerOwnedPointer(prepared.binding.journal, prepared.journal) || prepared.canonical == ([32]byte{}) || prepared.canonical != prepared.binding.canonical || prepared.canonical != runnerDurableCurrentStatementIntentDigest(prepared) {
		return false
	}
	registered, ok := runnerDurableCurrentStatementIntentRegistry.Load(prepared)
	record, recordOK := registered.(runnerDurableCurrentStatementIntentRegistryRecord)
	if !ok || !recordOK || record.prepared != prepared || record.binding != prepared.binding || record.key != prepared.key || record.candidateBinding != prepared.candidateBinding || record.cursorValid != prepared.cursor.valid || record.canonical != prepared.canonical || !sameRunnerOwnedPointer(record.session, prepared.session) || !sameRunnerOwnedPointer(record.transaction, prepared.transaction) || !sameRunnerOwnedPointer(record.evidence, prepared.evidence) || !sameRunnerOwnedPointer(record.journal, prepared.journal) {
		return false
	}
	status, statusOK := migrationProjectionTxStatus(prepared.transaction)
	if !statusOK || status != 'T' {
		return false
	}
	snapshot := prepared.evidence.RecoverySnapshot()
	digest, err := runnerDurableStatementIntentEvidenceDigest(prepared.evidence, prepared.journal, prepared.candidateBinding, prepared.generation, prepared.maxAttempts, prepared.cursor, prepared.intentRecordDigest, &prepared.intent, snapshot)
	return err == nil && digest == prepared.recoveryDigest
}

func runnerDurableCurrentStatementIntentDigest(prepared *runnerDurableCurrentStatementIntent) [32]byte {
	if prepared == nil || prepared.self != prepared || prepared.closed || prepared.session == nil || prepared.transaction == nil || prepared.evidence == nil || prepared.journal == nil || prepared.candidateBinding == nil || prepared.candidateBinding.owner == nil || prepared.generation.owner != prepared.candidateBinding.owner || prepared.candidateBinding.canonical == ([32]byte{}) || prepared.statementCanonical == ([32]byte{}) || prepared.recoveryDigest == ([32]byte{}) || prepared.maxAttempts == 0 || prepared.policy.Validate() != nil || prepared.policy.MaxAttempts != uint64(prepared.maxAttempts) || prepared.plan.validateExact() != nil || prepared.plan.StatementIndex != 0 || prepared.intent.Validate() != nil || prepared.intent.AttemptIndex != 1 || !planMatchesIntent(exactStatementWitnessFromPlan(prepared.plan, prepared.intent.AttemptIndex), prepared.intent) || !runnerStatementIntentProjectionEvidenceMatches(prepared.plan, prepared.intent.AuthorityBeforeResult, prepared.intent.CatalogBeforeResult) || prepared.intent.SchemaBundleDigest != prepared.generation.schemaBundleDigest || !prepared.cursor.Valid() || !sameGenerationIdentity(prepared.cursor.generation, prepared.generation) || prepared.cursor.segmentIndex != 0 || prepared.cursor.nextSequence != 2 || prepared.cursor.previousRecordDigest == nil || *prepared.cursor.previousRecordDigest != prepared.intentRecordDigest || prepared.intentRecordDigest.Validate() != nil || prepared.checkpointDigest.Validate() != nil || prepared.cursor.latestCheckpointRecordDigest == nil || *prepared.cursor.latestCheckpointRecordDigest != prepared.checkpointDigest || prepared.database.postgresMajor == 0 || prepared.database.serverVersionNum == 0 || prepared.database.databaseName == "" || prepared.database.sessionUser == "" || prepared.database.currentUser != MigrationOwnerRole || prepared.dispatch.recoveryState != RecoveryBrandNew && prepared.dispatch.recoveryState != RecoveryBrandNewInherited || prepared.dispatch.action != RecoveryBeginFirstAttempt || !migrationIDPattern.MatchString(prepared.dispatch.migrationID) || prepared.dispatch.migrationID != prepared.intent.MigrationID || prepared.dispatch.attemptIndex != prepared.intent.AttemptIndex || prepared.dispatch.entryIndex != 0 || prepared.dispatch.planCount == 0 || prepared.dispatch.planDigest == ([32]byte{}) {
		return [32]byte{}
	}
	intentCanonical, err := canonicalContractKey(prepared.intent)
	if err != nil || intentCanonical == "" {
		return [32]byte{}
	}
	policyCanonical, err := canonicalContractKey(prepared.policy)
	if err != nil || policyCanonical == "" {
		return [32]byte{}
	}
	h := sha256.New()
	h.Write([]byte("cloud-agents-platform-runner-durable-statement-intent/v1\x00"))
	h.Write(prepared.candidateBinding.canonical[:])
	h.Write(prepared.statementCanonical[:])
	h.Write(prepared.recoveryDigest[:])
	writeAdmissionString(h, prepared.plan.exactCanonical)
	writeAdmissionString(h, intentCanonical)
	writeAdmissionString(h, policyCanonical)
	for _, value := range []Digest{prepared.generation.executionLineageDigest, prepared.generation.journalIdentityDigest, prepared.generation.runnerProjectionDecisionDigest, prepared.generation.schemaBundleDigest, prepared.intentRecordDigest, prepared.checkpointDigest} {
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
	writeAdmissionUint(h, uint64(prepared.maxAttempts))
	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result
}

func closeRunnerDurableCurrentStatementIntent(prepared *runnerDurableCurrentStatementIntent, primary error) error {
	if prepared == nil {
		return primary
	}
	if prepared.self != prepared {
		return fail(CodeTransactionBoundary, "runner-statement-intent-close", "durable statement intent copy cannot close database authority", nil)
	}
	registered, ok := runnerDurableCurrentStatementIntentRegistry.Load(prepared)
	record, recordOK := registered.(runnerDurableCurrentStatementIntentRegistryRecord)
	if !ok || !recordOK || record.prepared != prepared || record.binding == nil {
		return fail(CodeTransactionBoundary, "runner-statement-intent-close", "durable statement intent authority is unavailable", nil)
	}
	valid := validRunnerDurableCurrentStatementIntent(prepared)
	runnerDurableCurrentStatementIntentRegistry.Delete(prepared)
	prepared.closed = true
	prepared.session = nil
	prepared.transaction = nil
	prepared.evidence = nil
	prepared.journal = nil
	prepared.binding = nil
	prepared.policy = ExecutionPolicy{}
	prepared.plan = StatementPlan{}
	prepared.intent = StatementIntent{}
	if !valid {
		primary = fail(CodeTransactionBoundary, "runner-statement-intent-close", "durable statement intent authority changed before close", nil)
	}
	return closeRunnerCurrentTransactionResources(record.session, record.transaction, record.key, primary)
}

func mapRunnerStatementIntentError(err error, op, message string) error {
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
