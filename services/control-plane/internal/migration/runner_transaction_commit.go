package migration

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"sync"
	"sync/atomic"
)

// runnerClosedCurrentCommit is the one-shot post-Commit boundary. It retains
// the exact dangling CommitIntent evidence and a closed protocol outcome, but
// no database session or transaction handle. It cannot append a terminal,
// reconnect, or mint a retry receipt by itself.
type runnerClosedCurrentCommit struct {
	self                     *runnerClosedCurrentCommit
	binding                  *runnerClosedCurrentCommitBinding
	evidence                 EvidenceSession
	journal                  EvidenceJournal
	key                      int64
	candidateBinding         *verifiedEvidenceRunBinding
	generation               generationIdentity
	commitCanonical          [32]byte
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
	protocol                 runnerCommitProtocolFacts
	connectionCloseProven    bool
	oldLifecycleID           string
	lifecycleOrder           ownedLifecycleOrderAuthority
	canonical                [32]byte
	released                 bool
}

type runnerClosedCurrentCommitBinding struct {
	prepared         *runnerClosedCurrentCommit
	evidence         EvidenceSession
	journal          EvidenceJournal
	candidateBinding *verifiedEvidenceRunBinding
	cursorValid      *atomic.Bool
	canonical        [32]byte
}

type runnerClosedCurrentCommitRegistryRecord struct {
	prepared         *runnerClosedCurrentCommit
	binding          *runnerClosedCurrentCommitBinding
	evidence         EvidenceSession
	journal          EvidenceJournal
	candidateBinding *verifiedEvidenceRunBinding
	cursorValid      *atomic.Bool
	canonical        [32]byte
}

type runnerCurrentCommitSeed struct {
	session                  DatabaseSession
	transaction              MigrationTransaction
	protocol                 runnerCommitProtocol
	evidence                 EvidenceSession
	journal                  EvidenceJournal
	key                      int64
	candidateBinding         *verifiedEvidenceRunBinding
	generation               generationIdentity
	commitCanonical          [32]byte
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
}

var runnerClosedCurrentCommitRegistry sync.Map

func (runner *Runner) commitCurrentTransaction(ctx context.Context, durable *runnerDurableCommitIntent) (*runnerClosedCurrentCommit, error) {
	seed, err := consumeRunnerDurableCommitIntent(durable)
	if err != nil {
		return nil, closeRunnerDurableCommitIntent(durable, err)
	}
	preCommitFailure := func(primary error) (*runnerClosedCurrentCommit, error) {
		return nil, closeRunnerCurrentTransactionResources(seed.session, seed.transaction, seed.key, primary)
	}
	if ctx == nil || runner == nil {
		return preCommitFailure(fail(CodeTransactionBoundary, "runner-transaction-commit", "transaction commit context or runner is unavailable", nil))
	}
	if contextErr := ctx.Err(); contextErr != nil {
		return preCommitFailure(mapRunnerCommitProtocolPreflightError(contextErr))
	}

	observation, commitCalled, protocolErr := invokeRunnerCommitProtocol(ctx, seed.transaction)
	if !commitCalled {
		if protocolErr == nil {
			protocolErr = fail(CodeTransactionBoundary, "runner-commit-protocol", "transaction commit protocol returned no pre-call result", nil)
		}
		return preCommitFailure(protocolErr)
	}
	if protocolErr != nil || observation == nil {
		_ = closeRunnerPostCommitSession(seed.session, seed.protocol.runnerCommitProtocolConnectionClosed())
		return nil, fail(CodeEvidenceRecoveryRequired, "runner-transaction-commit", "post-commit outcome authority is unavailable", nil)
	}
	facts, claimErr := consumeRunnerCommitProtocolObservation(observation, seed.protocol)
	if claimErr != nil {
		_ = closeRunnerPostCommitSession(seed.session, seed.protocol.runnerCommitProtocolConnectionClosed())
		return nil, fail(CodeEvidenceRecoveryRequired, "runner-transaction-commit", "post-commit outcome authority could not be consumed", nil)
	}
	closeProven := closeRunnerPostCommitSession(seed.session, facts.connectionClosed)
	closed, sealErr := bindRunnerClosedCurrentCommit(seed, facts, closeProven)
	if sealErr != nil {
		return nil, fail(CodeEvidenceRecoveryRequired, "runner-transaction-commit", "post-commit outcome could not be sealed", nil)
	}
	return closed, nil
}

func consumeRunnerDurableCommitIntent(durable *runnerDurableCommitIntent) (runnerCurrentCommitSeed, error) {
	if !validRunnerDurableCommitIntent(durable) {
		return runnerCurrentCommitSeed{}, fail(CodeTransactionBoundary, "runner-transaction-commit-claim", "durable commit intent authority is unavailable or changed", nil)
	}
	protocol, ok := durable.transaction.(runnerCommitProtocol)
	if !ok || protocol == nil || !runnerOwnedPointer(protocol) {
		return runnerCurrentCommitSeed{}, fail(CodeTransactionBoundary, "runner-transaction-commit-claim", "migration transaction lacks the sealed commit protocol", nil)
	}
	plan, err := cloneRunnerStatementIntentPlan(durable.plan)
	if err != nil {
		return runnerCurrentCommitSeed{}, fail(CodeUntrusted, "runner-transaction-commit-claim", "exact final statement plan is unavailable", nil)
	}
	registered, loaded := runnerDurableCommitIntentRegistry.LoadAndDelete(durable)
	record, recordOK := registered.(runnerDurableCommitIntentRegistryRecord)
	if !loaded || !recordOK || record.prepared != durable || record.binding != durable.binding || record.key != durable.key || record.candidateBinding != durable.candidateBinding || record.cursorValid != durable.cursor.valid || record.canonical != durable.canonical || !sameRunnerOwnedPointer(record.session, durable.session) || !sameRunnerOwnedPointer(record.transaction, durable.transaction) || !sameRunnerOwnedPointer(record.evidence, durable.evidence) || !sameRunnerOwnedPointer(record.journal, durable.journal) {
		return runnerCurrentCommitSeed{}, fail(CodeTransactionBoundary, "runner-transaction-commit-claim", "durable commit intent could not be consumed exactly once", nil)
	}
	seed := runnerCurrentCommitSeed{
		session: record.session, transaction: record.transaction, protocol: protocol,
		evidence: record.evidence, journal: record.journal, key: record.key,
		candidateBinding: record.candidateBinding, generation: durable.generation,
		commitCanonical: record.canonical, recoveryDigest: durable.recoveryDigest,
		dispatch: durable.dispatch, database: durable.database, maxAttempts: durable.maxAttempts,
		policy: cloneProjectionValue(durable.policy), plan: plan,
		intent: cloneProjectionValue(durable.intent), intermediate: cloneProjectionValue(durable.intermediate),
		commit: cloneProjectionValue(durable.commit), cursor: durable.cursor.clone(),
		intentRecordDigest: durable.intentRecordDigest, intermediateRecordDigest: durable.intermediateRecordDigest,
		commitRecordDigest: durable.commitRecordDigest, checkpointDigest: durable.checkpointDigest,
		ledgerPrefixDigest: durable.ledgerPrefixDigest,
	}
	durable.closed = true
	durable.session = nil
	durable.transaction = nil
	durable.evidence = nil
	durable.journal = nil
	durable.binding = nil
	durable.policy = ExecutionPolicy{}
	durable.plan = StatementPlan{}
	durable.intent = StatementIntent{}
	durable.intermediate = StatementIntermediateEvidence{}
	durable.commit = CommitIntent{}
	return seed, nil
}

func closeRunnerPostCommitSession(session DatabaseSession, alreadyClosed bool) bool {
	if session == nil {
		return alreadyClosed
	}
	cleanupCtx, cancel := cleanupContext()
	closeErr := session.Close(cleanupCtx)
	cancel()
	return alreadyClosed || closeErr == nil
}

func bindRunnerClosedCurrentCommit(seed runnerCurrentCommitSeed, facts runnerCommitProtocolFacts, closeProven bool) (*runnerClosedCurrentCommit, error) {
	if !validRunnerCommitProtocolFacts(facts) || seed.evidence == nil || seed.journal == nil || seed.candidateBinding == nil || !seed.cursor.Valid() || seed.commitCanonical == ([32]byte{}) || seed.recoveryDigest == ([32]byte{}) {
		return nil, fail(CodeEvidenceRecoveryRequired, "runner-transaction-commit-seal", "post-commit inputs are unavailable", nil)
	}
	plan, err := cloneRunnerStatementIntentPlan(seed.plan)
	if err != nil {
		return nil, fail(CodeUntrusted, "runner-transaction-commit-seal", "exact final statement plan is unavailable", nil)
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil || nonce == ([16]byte{}) {
		return nil, fail(CodeEvidenceRecoveryRequired, "runner-transaction-commit-seal", "connection lifecycle authority is unavailable", nil)
	}
	orderToken := &retryLifecycleOrderToken{verifierNonce: nonce}
	prepared := &runnerClosedCurrentCommit{
		evidence: seed.evidence, journal: seed.journal, key: seed.key, candidateBinding: seed.candidateBinding,
		generation: seed.generation, commitCanonical: seed.commitCanonical, recoveryDigest: seed.recoveryDigest,
		dispatch: seed.dispatch, database: seed.database, maxAttempts: seed.maxAttempts,
		policy: cloneProjectionValue(seed.policy), plan: plan, intent: cloneProjectionValue(seed.intent),
		intermediate: cloneProjectionValue(seed.intermediate), commit: cloneProjectionValue(seed.commit),
		cursor: seed.cursor.clone(), intentRecordDigest: seed.intentRecordDigest,
		intermediateRecordDigest: seed.intermediateRecordDigest, commitRecordDigest: seed.commitRecordDigest,
		checkpointDigest: seed.checkpointDigest, ledgerPrefixDigest: seed.ledgerPrefixDigest,
		protocol: facts, connectionCloseProven: closeProven,
		oldLifecycleID: "commit-" + hex.EncodeToString(nonce[:]),
		lifecycleOrder: ownedLifecycleOrderAuthority{token: orderToken, ordinal: 1},
	}
	prepared.self = prepared
	binding := &runnerClosedCurrentCommitBinding{
		prepared: prepared, evidence: seed.evidence, journal: seed.journal,
		candidateBinding: seed.candidateBinding, cursorValid: seed.cursor.valid,
	}
	prepared.binding = binding
	prepared.canonical = runnerClosedCurrentCommitDigest(prepared)
	binding.canonical = prepared.canonical
	if prepared.canonical == ([32]byte{}) {
		return nil, fail(CodeEvidenceRecoveryRequired, "runner-transaction-commit-seal", "post-commit outcome could not be identified", nil)
	}
	runnerClosedCurrentCommitRegistry.Store(prepared, runnerClosedCurrentCommitRegistryRecord{
		prepared: prepared, binding: binding, evidence: seed.evidence, journal: seed.journal,
		candidateBinding: seed.candidateBinding, cursorValid: seed.cursor.valid, canonical: prepared.canonical,
	})
	if !validRunnerClosedCurrentCommit(prepared) {
		runnerClosedCurrentCommitRegistry.Delete(prepared)
		return nil, fail(CodeEvidenceRecoveryRequired, "runner-transaction-commit-seal", "post-commit outcome could not be sealed", nil)
	}
	return prepared, nil
}

func validRunnerClosedCurrentCommit(prepared *runnerClosedCurrentCommit) bool {
	if prepared == nil || prepared.self != prepared || prepared.released || prepared.binding == nil || prepared.binding.prepared != prepared || prepared.binding.candidateBinding != prepared.candidateBinding || prepared.binding.cursorValid != prepared.cursor.valid || !sameRunnerOwnedPointer(prepared.binding.evidence, prepared.evidence) || !sameRunnerOwnedPointer(prepared.binding.journal, prepared.journal) || prepared.canonical == ([32]byte{}) || prepared.canonical != prepared.binding.canonical || prepared.canonical != runnerClosedCurrentCommitDigest(prepared) {
		return false
	}
	registered, loaded := runnerClosedCurrentCommitRegistry.Load(prepared)
	record, recordOK := registered.(runnerClosedCurrentCommitRegistryRecord)
	return loaded && recordOK && record.prepared == prepared && record.binding == prepared.binding && record.candidateBinding == prepared.candidateBinding && record.cursorValid == prepared.cursor.valid && record.canonical == prepared.canonical && sameRunnerOwnedPointer(record.evidence, prepared.evidence) && sameRunnerOwnedPointer(record.journal, prepared.journal)
}

func runnerClosedCurrentCommitDigest(prepared *runnerClosedCurrentCommit) [32]byte {
	if prepared == nil || prepared.self != prepared || prepared.released || prepared.evidence == nil || prepared.journal == nil || prepared.candidateBinding == nil || prepared.candidateBinding.owner == nil || prepared.generation.owner != prepared.candidateBinding.owner || prepared.candidateBinding.canonical == ([32]byte{}) || prepared.commitCanonical == ([32]byte{}) || prepared.recoveryDigest == ([32]byte{}) || prepared.policy.Validate() != nil || prepared.policy.MaxAttempts != uint64(prepared.maxAttempts) || prepared.plan.validateExact() != nil || prepared.plan.StatementIndex != 0 || prepared.dispatch.planCount != 1 || prepared.intent.Validate() != nil || prepared.intermediate.Validate() != nil || !runnerFinalIntermediateShapeMatches(prepared.plan, prepared.intent, prepared.intermediate) || prepared.commit.Validate() != nil || prepared.intentRecordDigest.Validate() != nil || prepared.intermediateRecordDigest.Validate() != nil || prepared.commitRecordDigest.Validate() != nil || prepared.checkpointDigest.Validate() != nil || prepared.ledgerPrefixDigest.Validate() != nil || !prepared.cursor.Valid() || !sameGenerationIdentity(prepared.cursor.generation, prepared.generation) || prepared.cursor.segmentIndex != 0 || prepared.cursor.nextSequence != 4 || prepared.cursor.previousRecordDigest == nil || *prepared.cursor.previousRecordDigest != prepared.commitRecordDigest || prepared.cursor.latestCheckpointRecordDigest == nil || *prepared.cursor.latestCheckpointRecordDigest != prepared.checkpointDigest || prepared.commit.SchemaBundleDigest != prepared.generation.schemaBundleDigest || prepared.commit.MigrationID != prepared.dispatch.migrationID || prepared.commit.AttemptIndex != prepared.dispatch.attemptIndex || prepared.commit.LastIntermediateStateDigest != prepared.intermediate.State.IntermediateStateDigest || prepared.commit.LedgerRow.BundleDigest != prepared.generation.schemaBundleDigest || prepared.commit.LedgerRow.SQLSHA256 != prepared.plan.SQLArtifactSHA256 || prepared.commit.LedgerRow.SQLSizeBytes != prepared.plan.SQLArtifactSizeBytes || prepared.commit.ExpectedLedgerLength != 1 || prepared.commit.ExpectedLedgerHead != prepared.dispatch.migrationID || prepared.database.postgresMajor == 0 || prepared.database.serverVersionNum == 0 || prepared.database.databaseName == "" || prepared.database.sessionUser == "" || prepared.database.currentUser != MigrationOwnerRole || prepared.dispatch.recoveryState != RecoveryBrandNew && prepared.dispatch.recoveryState != RecoveryBrandNewInherited || prepared.dispatch.action != RecoveryBeginFirstAttempt || prepared.dispatch.entryIndex != 0 || prepared.dispatch.planDigest == ([32]byte{}) || !validRunnerCommitProtocolFacts(prepared.protocol) || prepared.oldLifecycleID == "" || len(prepared.oldLifecycleID) > 128 || prepared.lifecycleOrder.token == nil || prepared.lifecycleOrder.token.verifierNonce == ([16]byte{}) || prepared.lifecycleOrder.ordinal != 1 {
		return [32]byte{}
	}
	if prepared.oldLifecycleID != "commit-"+hex.EncodeToString(prepared.lifecycleOrder.token.verifierNonce[:]) {
		return [32]byte{}
	}
	wantCommit, err := buildRunnerCommitIntent(runnerCommitIntentRecordRequest{
		generation: prepared.generation, maxAttempts: prepared.maxAttempts, planCount: prepared.dispatch.planCount,
		plan: prepared.plan, intent: prepared.intent, intermediate: prepared.intermediate,
		ledgerRow: prepared.commit.LedgerRow, ledgerPrefixDigest: prepared.ledgerPrefixDigest,
		ledgerHead: prepared.commit.ExpectedLedgerHead, ledgerLength: prepared.commit.ExpectedLedgerLength,
	})
	if err != nil || !canonicalEqual(wantCommit, prepared.commit) {
		return [32]byte{}
	}
	digest, err := runnerDurableCommitIntentEvidenceDigest(
		prepared.evidence, prepared.journal, prepared.candidateBinding, prepared.generation, prepared.maxAttempts,
		prepared.cursor, prepared.intentRecordDigest, prepared.intermediateRecordDigest, prepared.commitRecordDigest,
		&prepared.intent, &prepared.intermediate, &prepared.commit, prepared.evidence.RecoverySnapshot(),
	)
	if err != nil || digest != prepared.recoveryDigest {
		return [32]byte{}
	}
	values := []any{prepared.policy, prepared.plan.exactSentinel(), prepared.intent, prepared.intermediate, prepared.commit}
	h := sha256.New()
	h.Write([]byte("cloud-agents-platform-runner-closed-current-commit/v1\x00"))
	h.Write(prepared.candidateBinding.canonical[:])
	h.Write(prepared.commitCanonical[:])
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
	writeAdmissionString(h, string(prepared.protocol.outcome))
	writeAdmissionString(h, prepared.protocol.rejectionReason)
	writeAdmissionUint(h, boolUint64(prepared.protocol.commitCalled))
	writeAdmissionUint(h, boolUint64(prepared.protocol.readyForQuery))
	writeAdmissionUint(h, boolUint64(prepared.protocol.connectionClosed))
	writeAdmissionUint(h, boolUint64(prepared.connectionCloseProven))
	writeAdmissionString(h, prepared.oldLifecycleID)
	h.Write(prepared.lifecycleOrder.token.verifierNonce[:])
	writeAdmissionUint(h, prepared.lifecycleOrder.ordinal)
	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result
}

func closeRunnerClosedCurrentCommit(prepared *runnerClosedCurrentCommit, primary error) error {
	if prepared == nil {
		return primary
	}
	if prepared.self != prepared {
		return fail(CodeTransactionBoundary, "runner-transaction-commit-close", "post-commit authority copy cannot be released", nil)
	}
	registered, loaded := runnerClosedCurrentCommitRegistry.Load(prepared)
	record, recordOK := registered.(runnerClosedCurrentCommitRegistryRecord)
	if !loaded || !recordOK || record.prepared != prepared || record.binding == nil {
		return fail(CodeTransactionBoundary, "runner-transaction-commit-close", "post-commit authority is unavailable", nil)
	}
	valid := validRunnerClosedCurrentCommit(prepared)
	runnerClosedCurrentCommitRegistry.Delete(prepared)
	closeProven := prepared.connectionCloseProven
	prepared.released = true
	prepared.evidence = nil
	prepared.journal = nil
	prepared.binding = nil
	prepared.policy = ExecutionPolicy{}
	prepared.plan = StatementPlan{}
	prepared.intent = StatementIntent{}
	prepared.intermediate = StatementIntermediateEvidence{}
	prepared.commit = CommitIntent{}
	prepared.lifecycleOrder = ownedLifecycleOrderAuthority{}
	if !valid {
		return fail(CodeTransactionBoundary, "runner-transaction-commit-close", "post-commit authority changed before release", nil)
	}
	if !closeProven {
		return fail(CodeTransactionBoundary, "runner-transaction-commit-close", "old database connection close could not be proven", nil)
	}
	return primary
}
