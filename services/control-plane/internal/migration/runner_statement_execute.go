package migration

import (
	"context"
	"crypto/sha256"
	"errors"
	"strconv"
	"sync"
	"sync/atomic"
)

// runnerExecutedCurrentStatement proves that the exact SQL bytes referenced by
// a durable StatementIntent were submitted exactly once on the same owned
// migration transaction. It is deliberately not consumed by Runner.Run yet:
// statement-after projection and both success/failure evidence paths must be
// closed before the public runner may acquire this authority.
type runnerExecutedCurrentStatement struct {
	self                     *runnerExecutedCurrentStatement
	binding                  *runnerExecutedCurrentStatementBinding
	session                  DatabaseSession
	transaction              MigrationTransaction
	evidence                 EvidenceSession
	journal                  EvidenceJournal
	key                      int64
	candidateBinding         *verifiedEvidenceRunBinding
	generation               generationIdentity
	statementIntentCanonical [32]byte
	recoveryDigest           [32]byte
	dispatch                 runnerPreparedDispatch
	database                 runnerPreparedDatabaseIdentity
	maxAttempts              uint32
	plan                     StatementPlan
	intent                   StatementIntent
	cursor                   JournalCursor
	intentRecordDigest       Digest
	checkpointDigest         Digest
	executedStatementDigest  Digest
	canonical                [32]byte
	closed                   bool
}

type runnerExecutedCurrentStatementBinding struct {
	prepared         *runnerExecutedCurrentStatement
	session          DatabaseSession
	transaction      MigrationTransaction
	evidence         EvidenceSession
	journal          EvidenceJournal
	key              int64
	candidateBinding *verifiedEvidenceRunBinding
	cursorValid      *atomic.Bool
	canonical        [32]byte
}

type runnerExecutedCurrentStatementRegistryRecord struct {
	prepared         *runnerExecutedCurrentStatement
	binding          *runnerExecutedCurrentStatementBinding
	session          DatabaseSession
	transaction      MigrationTransaction
	evidence         EvidenceSession
	journal          EvidenceJournal
	key              int64
	candidateBinding *verifiedEvidenceRunBinding
	cursorValid      *atomic.Bool
	canonical        [32]byte
}

type runnerExecutedCurrentStatementSeed struct {
	session                  DatabaseSession
	transaction              MigrationTransaction
	evidence                 EvidenceSession
	journal                  EvidenceJournal
	key                      int64
	candidateBinding         *verifiedEvidenceRunBinding
	generation               generationIdentity
	statementIntentCanonical [32]byte
	recoveryDigest           [32]byte
	dispatch                 runnerPreparedDispatch
	database                 runnerPreparedDatabaseIdentity
	maxAttempts              uint32
	plan                     StatementPlan
	intent                   StatementIntent
	cursor                   JournalCursor
	intentRecordDigest       Digest
	checkpointDigest         Digest
}

var runnerExecutedCurrentStatementRegistry sync.Map

func (runner *Runner) executeCurrentStatement(ctx context.Context, durable *runnerDurableCurrentStatementIntent) (*runnerExecutedCurrentStatement, error) {
	seed, sql, err := consumeRunnerDurableCurrentStatementIntent(durable)
	if err != nil {
		return nil, closeRunnerDurableCurrentStatementIntent(durable, err)
	}
	failClosed := func(primary error) (*runnerExecutedCurrentStatement, error) {
		return nil, closeRunnerCurrentTransactionResources(seed.session, seed.transaction, seed.key, primary)
	}
	if ctx == nil {
		return failClosed(fail(CodeTransactionBoundary, "runner-statement-execute", "statement execution context is unavailable", nil))
	}
	if contextErr := ctx.Err(); contextErr != nil {
		return failClosed(mapRunnerStatementExecutionError(contextErr))
	}
	if runner == nil {
		return failClosed(fail(CodeTransactionBoundary, "runner-statement-execute", "runner execution authority is unavailable", nil))
	}
	runner.transition(StateMigrate)
	if executionErr := seed.transaction.ExecuteStatement(ctx, append([]byte(nil), sql...)); executionErr != nil {
		return failClosed(mapRunnerStatementExecutionError(executionErr))
	}
	status, statusOK := migrationProjectionTxStatus(seed.transaction)
	if !statusOK || status != 'T' {
		return failClosed(fail(CodeTransactionBoundary, "runner-statement-execute", "statement execution escaped the owned transaction", nil))
	}
	if !runnerExecutedStatementEvidenceMatches(seed) {
		invalidateRunnerExecutedStatementCursor(seed)
		return failClosed(fail(CodeEvidenceJournalFailed, "runner-statement-execute", "durable statement evidence changed during SQL execution", nil))
	}
	executed, sealErr := bindRunnerExecutedCurrentStatement(seed, DigestBytes(sql))
	if sealErr != nil {
		invalidateRunnerExecutedStatementCursor(seed)
		return failClosed(sealErr)
	}
	return executed, nil
}

func invalidateRunnerExecutedStatementCursor(seed runnerExecutedCurrentStatementSeed) {
	if seed.cursor.valid != nil {
		seed.cursor.valid.Store(false)
	}
}

func consumeRunnerDurableCurrentStatementIntent(durable *runnerDurableCurrentStatementIntent) (runnerExecutedCurrentStatementSeed, []byte, error) {
	if !validRunnerDurableCurrentStatementIntent(durable) {
		return runnerExecutedCurrentStatementSeed{}, nil, fail(CodeTransactionBoundary, "runner-statement-execute-claim", "durable statement intent authority is unavailable or changed", nil)
	}
	plan, err := cloneRunnerStatementIntentPlan(durable.plan)
	if err != nil {
		return runnerExecutedCurrentStatementSeed{}, nil, fail(CodeUntrusted, "runner-statement-execute-claim", "exact statement plan is unavailable", nil)
	}
	sql, err := plan.exactSQLBytes()
	if err != nil || DigestBytes(sql) != durable.intent.StatementSHA256 || durable.intent.StatementSHA256 != plan.StatementSHA256 {
		return runnerExecutedCurrentStatementSeed{}, nil, fail(CodeUntrusted, "runner-statement-execute-claim", "owned SQL bytes differ from the durable statement intent", nil)
	}
	registered, ok := runnerDurableCurrentStatementIntentRegistry.LoadAndDelete(durable)
	record, recordOK := registered.(runnerDurableCurrentStatementIntentRegistryRecord)
	if !ok || !recordOK || record.prepared != durable || record.binding != durable.binding || record.key != durable.key || record.candidateBinding != durable.candidateBinding || record.cursorValid != durable.cursor.valid || record.canonical != durable.canonical || !sameRunnerOwnedPointer(record.session, durable.session) || !sameRunnerOwnedPointer(record.transaction, durable.transaction) || !sameRunnerOwnedPointer(record.evidence, durable.evidence) || !sameRunnerOwnedPointer(record.journal, durable.journal) {
		return runnerExecutedCurrentStatementSeed{}, nil, fail(CodeTransactionBoundary, "runner-statement-execute-claim", "durable statement intent could not be consumed exactly once", nil)
	}
	seed := runnerExecutedCurrentStatementSeed{
		session: record.session, transaction: record.transaction, evidence: record.evidence, journal: record.journal,
		key: record.key, candidateBinding: record.candidateBinding, generation: durable.generation,
		statementIntentCanonical: record.canonical, recoveryDigest: durable.recoveryDigest,
		dispatch: durable.dispatch, database: durable.database, maxAttempts: durable.maxAttempts,
		plan: plan, intent: cloneProjectionValue(durable.intent), cursor: durable.cursor.clone(),
		intentRecordDigest: durable.intentRecordDigest, checkpointDigest: durable.checkpointDigest,
	}
	durable.closed = true
	durable.session = nil
	durable.transaction = nil
	durable.evidence = nil
	durable.journal = nil
	durable.binding = nil
	durable.plan = StatementPlan{}
	durable.intent = StatementIntent{}
	return seed, sql, nil
}

func runnerExecutedStatementEvidenceMatches(seed runnerExecutedCurrentStatementSeed) bool {
	snapshot := seed.evidence.RecoverySnapshot()
	digest, err := runnerDurableStatementIntentEvidenceDigest(
		seed.evidence, seed.journal, seed.candidateBinding, seed.generation, seed.maxAttempts,
		seed.cursor, seed.intentRecordDigest, &seed.intent, snapshot,
	)
	return err == nil && digest == seed.recoveryDigest
}

func bindRunnerExecutedCurrentStatement(seed runnerExecutedCurrentStatementSeed, executedDigest Digest) (*runnerExecutedCurrentStatement, error) {
	plan, err := cloneRunnerStatementIntentPlan(seed.plan)
	if err != nil || executedDigest.Validate() != nil || executedDigest != plan.StatementSHA256 || !runnerExecutedStatementEvidenceMatches(seed) {
		return nil, fail(CodeTransactionBoundary, "runner-statement-execute-seal", "executed statement inputs are unavailable or changed", nil)
	}
	prepared := &runnerExecutedCurrentStatement{
		session: seed.session, transaction: seed.transaction, evidence: seed.evidence, journal: seed.journal,
		key: seed.key, candidateBinding: seed.candidateBinding, generation: seed.generation,
		statementIntentCanonical: seed.statementIntentCanonical, recoveryDigest: seed.recoveryDigest,
		dispatch: seed.dispatch, database: seed.database, maxAttempts: seed.maxAttempts,
		plan: plan, intent: cloneProjectionValue(seed.intent), cursor: seed.cursor.clone(),
		intentRecordDigest: seed.intentRecordDigest, checkpointDigest: seed.checkpointDigest,
		executedStatementDigest: executedDigest,
	}
	prepared.self = prepared
	binding := &runnerExecutedCurrentStatementBinding{
		prepared: prepared, session: seed.session, transaction: seed.transaction, evidence: seed.evidence,
		journal: seed.journal, key: seed.key, candidateBinding: seed.candidateBinding, cursorValid: seed.cursor.valid,
	}
	prepared.binding = binding
	prepared.canonical = runnerExecutedCurrentStatementDigest(prepared)
	binding.canonical = prepared.canonical
	if prepared.canonical == ([32]byte{}) {
		return nil, fail(CodeTransactionBoundary, "runner-statement-execute-seal", "executed statement could not be identified", nil)
	}
	runnerExecutedCurrentStatementRegistry.Store(prepared, runnerExecutedCurrentStatementRegistryRecord{
		prepared: prepared, binding: binding, session: seed.session, transaction: seed.transaction,
		evidence: seed.evidence, journal: seed.journal, key: seed.key, candidateBinding: seed.candidateBinding,
		cursorValid: seed.cursor.valid, canonical: prepared.canonical,
	})
	if !validRunnerExecutedCurrentStatement(prepared) {
		runnerExecutedCurrentStatementRegistry.Delete(prepared)
		return nil, fail(CodeTransactionBoundary, "runner-statement-execute-seal", "executed statement could not be sealed", nil)
	}
	return prepared, nil
}

func validRunnerExecutedCurrentStatement(prepared *runnerExecutedCurrentStatement) bool {
	if prepared == nil || prepared.self != prepared || prepared.closed || prepared.binding == nil || prepared.binding.prepared != prepared || prepared.binding.key != prepared.key || prepared.binding.candidateBinding != prepared.candidateBinding || prepared.binding.cursorValid != prepared.cursor.valid || !sameRunnerOwnedPointer(prepared.binding.session, prepared.session) || !sameRunnerOwnedPointer(prepared.binding.transaction, prepared.transaction) || !sameRunnerOwnedPointer(prepared.binding.evidence, prepared.evidence) || !sameRunnerOwnedPointer(prepared.binding.journal, prepared.journal) || prepared.canonical == ([32]byte{}) || prepared.canonical != prepared.binding.canonical || prepared.canonical != runnerExecutedCurrentStatementDigest(prepared) {
		return false
	}
	registered, ok := runnerExecutedCurrentStatementRegistry.Load(prepared)
	record, recordOK := registered.(runnerExecutedCurrentStatementRegistryRecord)
	if !ok || !recordOK || record.prepared != prepared || record.binding != prepared.binding || record.key != prepared.key || record.candidateBinding != prepared.candidateBinding || record.cursorValid != prepared.cursor.valid || record.canonical != prepared.canonical || !sameRunnerOwnedPointer(record.session, prepared.session) || !sameRunnerOwnedPointer(record.transaction, prepared.transaction) || !sameRunnerOwnedPointer(record.evidence, prepared.evidence) || !sameRunnerOwnedPointer(record.journal, prepared.journal) {
		return false
	}
	status, statusOK := migrationProjectionTxStatus(prepared.transaction)
	if !statusOK || status != 'T' {
		return false
	}
	seed := runnerExecutedCurrentStatementSeed{
		session: prepared.session, transaction: prepared.transaction, evidence: prepared.evidence, journal: prepared.journal,
		key: prepared.key, candidateBinding: prepared.candidateBinding, generation: prepared.generation,
		statementIntentCanonical: prepared.statementIntentCanonical, recoveryDigest: prepared.recoveryDigest,
		dispatch: prepared.dispatch, database: prepared.database, maxAttempts: prepared.maxAttempts,
		plan: prepared.plan, intent: prepared.intent, cursor: prepared.cursor,
		intentRecordDigest: prepared.intentRecordDigest, checkpointDigest: prepared.checkpointDigest,
	}
	return runnerExecutedStatementEvidenceMatches(seed)
}

func runnerExecutedCurrentStatementDigest(prepared *runnerExecutedCurrentStatement) [32]byte {
	if prepared == nil || prepared.self != prepared || prepared.closed || prepared.session == nil || prepared.transaction == nil || prepared.evidence == nil || prepared.journal == nil || prepared.candidateBinding == nil || prepared.candidateBinding.owner == nil || prepared.generation.owner != prepared.candidateBinding.owner || prepared.candidateBinding.canonical == ([32]byte{}) || prepared.statementIntentCanonical == ([32]byte{}) || prepared.recoveryDigest == ([32]byte{}) || prepared.maxAttempts == 0 || prepared.plan.validateExact() != nil || prepared.plan.StatementIndex != 0 || prepared.intent.Validate() != nil || prepared.intent.StatementIndex != prepared.plan.StatementIndex || prepared.intent.StatementSHA256 != prepared.plan.StatementSHA256 || prepared.intent.SchemaBundleDigest != prepared.generation.schemaBundleDigest || !planMatchesIntent(exactStatementWitnessFromPlan(prepared.plan, prepared.intent.AttemptIndex), prepared.intent) || !runnerStatementIntentProjectionEvidenceMatches(prepared.plan, prepared.intent.AuthorityBeforeResult, prepared.intent.CatalogBeforeResult) || prepared.executedStatementDigest.Validate() != nil || prepared.executedStatementDigest != prepared.plan.StatementSHA256 || !prepared.cursor.Valid() || !sameGenerationIdentity(prepared.cursor.generation, prepared.generation) || prepared.cursor.nextSequence != 2 || prepared.cursor.previousRecordDigest == nil || *prepared.cursor.previousRecordDigest != prepared.intentRecordDigest || prepared.intentRecordDigest.Validate() != nil || prepared.checkpointDigest.Validate() != nil || prepared.cursor.latestCheckpointRecordDigest == nil || *prepared.cursor.latestCheckpointRecordDigest != prepared.checkpointDigest || prepared.database.postgresMajor == 0 || prepared.database.serverVersionNum == 0 || prepared.database.databaseName == "" || prepared.database.sessionUser == "" || prepared.database.currentUser != MigrationOwnerRole || prepared.dispatch.recoveryState != RecoveryBrandNew && prepared.dispatch.recoveryState != RecoveryBrandNewInherited || prepared.dispatch.action != RecoveryBeginFirstAttempt || prepared.dispatch.migrationID != prepared.intent.MigrationID || prepared.dispatch.attemptIndex != prepared.intent.AttemptIndex || prepared.dispatch.entryIndex != 0 || prepared.dispatch.planCount == 0 || prepared.dispatch.planDigest == ([32]byte{}) {
		return [32]byte{}
	}
	intentCanonical, err := canonicalContractKey(prepared.intent)
	if err != nil || intentCanonical == "" {
		return [32]byte{}
	}
	h := sha256.New()
	h.Write([]byte("cloud-agents-platform-runner-executed-current-statement/v1\x00"))
	h.Write(prepared.candidateBinding.canonical[:])
	h.Write(prepared.statementIntentCanonical[:])
	h.Write(prepared.recoveryDigest[:])
	writeAdmissionString(h, prepared.plan.exactCanonical)
	writeAdmissionString(h, intentCanonical)
	for _, value := range []Digest{
		prepared.generation.executionLineageDigest, prepared.generation.journalIdentityDigest,
		prepared.generation.runnerProjectionDecisionDigest, prepared.generation.schemaBundleDigest,
		prepared.intentRecordDigest, prepared.checkpointDigest, prepared.executedStatementDigest,
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
	writeAdmissionUint(h, uint64(prepared.maxAttempts))
	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result
}

func closeRunnerExecutedCurrentStatement(prepared *runnerExecutedCurrentStatement, primary error) error {
	if prepared == nil {
		return primary
	}
	if prepared.self != prepared {
		return fail(CodeTransactionBoundary, "runner-statement-executed-close", "executed statement copy cannot close database authority", nil)
	}
	registered, ok := runnerExecutedCurrentStatementRegistry.Load(prepared)
	record, recordOK := registered.(runnerExecutedCurrentStatementRegistryRecord)
	if !ok || !recordOK || record.prepared != prepared || record.binding == nil {
		return fail(CodeTransactionBoundary, "runner-statement-executed-close", "executed statement authority is unavailable", nil)
	}
	valid := validRunnerExecutedCurrentStatement(prepared)
	runnerExecutedCurrentStatementRegistry.Delete(prepared)
	prepared.closed = true
	prepared.session = nil
	prepared.transaction = nil
	prepared.evidence = nil
	prepared.journal = nil
	prepared.binding = nil
	prepared.plan = StatementPlan{}
	prepared.intent = StatementIntent{}
	if !valid {
		primary = fail(CodeTransactionBoundary, "runner-statement-executed-close", "executed statement authority changed before close", nil)
	}
	return closeRunnerCurrentTransactionResources(record.session, record.transaction, record.key, primary)
}

func mapRunnerStatementExecutionError(err error) error {
	if errors.Is(err, context.Canceled) {
		return fail(CodeContextCanceled, "runner-statement-execute", "exact statement execution was canceled", nil)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return fail(CodeDeadlineExceeded, "runner-statement-execute", "exact statement execution exceeded its deadline", nil)
	}
	if classifyRetry(err) != retryNone {
		return fail(CodeTransactionBoundary, "runner-statement-execute", "exact statement execution lost its transaction boundary", nil)
	}
	var stable *Error
	if errors.As(err, &stable) {
		switch stable.Code {
		case CodeInvalidSQL, CodeTransactionBoundary, CodeLockLost:
			return fail(stable.Code, "runner-statement-execute", "exact statement execution failed", nil)
		}
	}
	return fail(CodeInvalidSQL, "runner-statement-execute", "PostgreSQL rejected the exact statement", nil)
}
