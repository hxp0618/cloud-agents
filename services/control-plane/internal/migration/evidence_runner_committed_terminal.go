package migration

import "context"

// runnerCommittedTerminalRecordBinder is the only runner-facing bridge from a
// sealed committed protocol outcome to an AttemptTerminal append authority.
// It consumes the post-Commit authority and exposes no database capability.
type runnerCommittedTerminalRecordBinder interface {
	bindRunnerCommittedTerminalRecord(context.Context, *runnerClosedCurrentCommit) (EvidenceJournal, JournalCursor, *OwnedEvidenceRecord, error)
	runnerCommittedTerminalRecordBinderSealed()
}

type runnerCommittedTerminalSeed struct {
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
	connectionCloseProven    bool
	oldLifecycleID           string
	lifecycleOrder           ownedLifecycleOrderAuthority
}

func claimRunnerCommittedTerminalSeed(closed *runnerClosedCurrentCommit) (runnerCommittedTerminalSeed, error) {
	if !validRunnerClosedCurrentCommit(closed) || closed.protocol.outcome != runnerCommitProtocolCommitted || closed.protocol.rejectionReason != "" || !closed.protocol.commitCalled || !closed.protocol.readyForQuery {
		return runnerCommittedTerminalSeed{}, fail(CodeEvidenceRecoveryRequired, "runner-committed-terminal-claim", "committed transaction outcome is unavailable or changed", nil)
	}
	plan, err := cloneRunnerStatementIntentPlan(closed.plan)
	if err != nil {
		return runnerCommittedTerminalSeed{}, fail(CodeUntrusted, "runner-committed-terminal-claim", "exact final statement plan is unavailable", nil)
	}
	registered, loaded := runnerClosedCurrentCommitRegistry.LoadAndDelete(closed)
	record, recordOK := registered.(runnerClosedCurrentCommitRegistryRecord)
	if !loaded || !recordOK || record.prepared != closed || record.binding != closed.binding || record.candidateBinding != closed.candidateBinding || record.cursorValid != closed.cursor.valid || record.canonical != closed.canonical || !sameRunnerOwnedPointer(record.evidence, closed.evidence) || !sameRunnerOwnedPointer(record.journal, closed.journal) {
		return runnerCommittedTerminalSeed{}, fail(CodeEvidenceRecoveryRequired, "runner-committed-terminal-claim", "committed transaction outcome could not be consumed exactly once", nil)
	}
	seed := runnerCommittedTerminalSeed{
		evidence: record.evidence, journal: record.journal, key: closed.key,
		candidateBinding: record.candidateBinding, generation: closed.generation,
		commitCanonical: record.canonical, recoveryDigest: closed.recoveryDigest,
		dispatch: closed.dispatch, database: closed.database, maxAttempts: closed.maxAttempts,
		policy: cloneProjectionValue(closed.policy), plan: plan,
		intent: cloneProjectionValue(closed.intent), intermediate: cloneProjectionValue(closed.intermediate),
		commit: cloneProjectionValue(closed.commit), cursor: closed.cursor.clone(),
		intentRecordDigest: closed.intentRecordDigest, intermediateRecordDigest: closed.intermediateRecordDigest,
		commitRecordDigest: closed.commitRecordDigest, checkpointDigest: closed.checkpointDigest,
		ledgerPrefixDigest: closed.ledgerPrefixDigest, connectionCloseProven: closed.connectionCloseProven,
		oldLifecycleID: closed.oldLifecycleID, lifecycleOrder: closed.lifecycleOrder,
	}
	closed.released = true
	closed.evidence = nil
	closed.journal = nil
	closed.binding = nil
	closed.policy = ExecutionPolicy{}
	closed.plan = StatementPlan{}
	closed.intent = StatementIntent{}
	closed.intermediate = StatementIntermediateEvidence{}
	closed.commit = CommitIntent{}
	closed.lifecycleOrder = ownedLifecycleOrderAuthority{}
	return seed, nil
}

func buildRunnerCommittedTerminal(seed runnerCommittedTerminalSeed) (AttemptTerminalState, error) {
	if seed.maxAttempts == 0 || seed.intent.Validate() != nil || seed.intermediate.Validate() != nil || seed.commit.Validate() != nil || seed.intent.AttemptIndex == 0 || seed.intent.AttemptIndex > seed.maxAttempts || seed.commit.MigrationID != seed.intent.MigrationID || seed.commit.AttemptIndex != seed.intent.AttemptIndex || seed.commit.LastIntermediateStateDigest != seed.intermediate.State.IntermediateStateDigest || seed.commit.SchemaBundleDigest != seed.generation.schemaBundleDigest || seed.intent.CatalogContractDigest != seed.commit.CatalogContractDigest || seed.intent.AuthorityProfileDigest != seed.commit.AuthorityProfileDigest || seed.intent.AuthorityBindingDigest != seed.commit.AuthorityBindingDigest {
		return AttemptTerminalState{}, invalidEvidence("runner-committed-terminal", "commit, intermediate, or attempt binding")
	}
	lastIntermediate := seed.intermediate.State.IntermediateStateDigest
	terminal := AttemptTerminalState{
		SchemaBundleDigest: seed.generation.schemaBundleDigest, CatalogContractDigest: seed.commit.CatalogContractDigest,
		AuthorityProfileDigest: seed.commit.AuthorityProfileDigest, AuthorityBindingDigest: seed.commit.AuthorityBindingDigest,
		MigrationID: seed.commit.MigrationID, AttemptIndex: seed.commit.AttemptIndex,
		PreviousAttemptTerminalDigest: cloneDigestPointer(seed.commit.PreviousAttemptTerminalDigest),
		LastIntermediateStateDigest:   &lastIntermediate,
		Outcome:                       "committed", ReconcileResult: "not_run",
	}
	var err error
	terminal.TerminalDigest, err = terminal.ComputeDigest()
	if err != nil || terminal.Validate(seed.maxAttempts) != nil {
		return AttemptTerminalState{}, invalidEvidence("runner-committed-terminal", "terminal identity")
	}
	return terminal, nil
}

func bindBrandNewRunnerCommittedTerminalRecord(seed runnerCommittedTerminalSeed, generation generationIdentity, cursor JournalCursor, recovery *RecoverySnapshot, header JournalHeader, chain verifiedEvidenceChainWitness) (*OwnedEvidenceRecord, error) {
	terminal, err := buildRunnerCommittedTerminal(seed)
	if err != nil || !sameGenerationIdentity(seed.generation, generation) || !sameGenerationHeader(generation, header) || !sameCursorIdentity(seed.cursor, cursor) || seed.recoveryDigest == ([32]byte{}) || generationJournalRecoveryDigest(recovery) != seed.recoveryDigest {
		return nil, invalidEvidence("runner-committed-terminal", "terminal, generation, cursor, or recovery binding")
	}
	if !runnerCommittedTerminalRecoveryMatches(seed, generation, cursor, recovery) {
		return nil, invalidEvidence("runner-committed-terminal", "durable commit intent recovery boundary")
	}
	prefix, err := runnerCommittedTerminalPrefix(seed, header)
	if err != nil || len(prefix) != 4 || prefix[len(prefix)-1].RecordDigest != *cursor.previousRecordDigest {
		return nil, invalidEvidence("runner-committed-terminal", "durable evidence prefix")
	}
	witness := ownedAttemptTerminalWitness{
		ownedAppendContext: ownedAppendContext{
			generation: generation, cursor: cursor.clone(), prefix: cloneProjectionValue(prefix),
			chain: cloneRunnerEvidenceChainWitness(chain),
		},
		terminalDigest: terminal.TerminalDigest, maxAttempts: seed.maxAttempts,
	}
	return bindOwnedEvidenceRecord(EvidenceRecord{AttemptTerminal: &terminal}, witness)
}

func runnerCommittedTerminalRecoveryMatches(seed runnerCommittedTerminalSeed, generation generationIdentity, cursor JournalCursor, recovery *RecoverySnapshot) bool {
	if recovery == nil || !validRecoverySnapshotForJournal(recovery, generation, cursor) || recovery.state != RecoveryDanglingCommitIntent || recovery.nextPermittedAction != RecoveryReconcileCommit || recovery.migrationID == nil || *recovery.migrationID != seed.intent.MigrationID || recovery.attemptIndex == nil || *recovery.attemptIndex != seed.intent.AttemptIndex || recovery.tailDigest != seed.commitRecordDigest || recovery.lastStatementIntentRecordDigest == nil || *recovery.lastStatementIntentRecordDigest != seed.intentRecordDigest || recovery.lastIntermediateEvidenceRecordDigest == nil || *recovery.lastIntermediateEvidenceRecordDigest != seed.intermediateRecordDigest || recovery.lastIntermediateStateDigest == nil || *recovery.lastIntermediateStateDigest != seed.intermediate.State.IntermediateStateDigest || recovery.lastCommitIntentRecordDigest == nil || *recovery.lastCommitIntentRecordDigest != seed.commitRecordDigest || recovery.lastStatementIntent == nil || recovery.lastIntermediateEvidence == nil || recovery.commitIntent == nil || recovery.lastTerminal != nil || recovery.lastTerminalDigest != nil || recovery.lastResolution != nil || recovery.lastResolutionDigest != nil || recovery.lineageContinuation != nil {
		return false
	}
	recoveredIntent := recovery.lastStatementIntent
	recoveredIntermediate := recovery.lastIntermediateEvidence
	recoveredCommit := recovery.commitIntent
	return recoveredIntent.owner == generation.owner && recoveredIntermediate.owner == generation.owner && recoveredCommit.owner == generation.owner &&
		sameGenerationIdentity(recoveredIntent.generation, generation) && sameGenerationIdentity(recoveredIntermediate.generation, generation) && sameGenerationIdentity(recoveredCommit.generation, generation) &&
		sameCursorIdentity(recoveredIntent.cursor, cursor) && sameCursorIdentity(recoveredIntermediate.cursor, cursor) && sameCursorIdentity(recoveredCommit.cursor, cursor) &&
		recoveredIntent.tailDigest == seed.commitRecordDigest && recoveredIntermediate.tailDigest == seed.commitRecordDigest && recoveredCommit.tailDigest == seed.commitRecordDigest &&
		recoveredIntent.recordDigest == seed.intentRecordDigest && recoveredIntermediate.recordDigest == seed.intermediateRecordDigest && recoveredCommit.recordDigest == seed.commitRecordDigest &&
		canonicalEqual(recoveredIntent.value, seed.intent) && canonicalEqual(recoveredIntermediate.value, seed.intermediate) && canonicalEqual(recoveredCommit.value, seed.commit) &&
		equalDigestPointer(recovery.previousAttemptTerminalDigest, seed.intent.PreviousAttemptTerminalDigest)
}

func runnerCommittedTerminalPrefix(seed runnerCommittedTerminalSeed, header JournalHeader) ([]EvidenceFrame, error) {
	headerFrame := EvidenceFrame{FormatVersion: EvidenceFrameFormat, Sequence: 0, RecordKind: EvidenceRecordHeader, Record: EvidenceRecord{Header: &header}}
	var err error
	headerFrame.RecordDigest, err = headerFrame.ComputeDigest()
	if err != nil || headerFrame.Validate() != nil {
		return nil, invalidEvidence("runner-committed-terminal", "header frame")
	}
	intent := cloneProjectionValue(seed.intent)
	intentFrame := EvidenceFrame{FormatVersion: EvidenceFrameFormat, Sequence: 1, PreviousRecordDigest: digestPointer(headerFrame.RecordDigest), RecordKind: EvidenceRecordStatementIntent, Record: EvidenceRecord{StatementIntent: &intent}}
	intentFrame.RecordDigest, err = intentFrame.ComputeDigest()
	if err != nil || intentFrame.Validate() != nil || intentFrame.RecordDigest != seed.intentRecordDigest {
		return nil, invalidEvidence("runner-committed-terminal", "statement intent frame")
	}
	intermediate := cloneProjectionValue(seed.intermediate)
	intermediateFrame := EvidenceFrame{FormatVersion: EvidenceFrameFormat, Sequence: 2, PreviousRecordDigest: digestPointer(intentFrame.RecordDigest), RecordKind: EvidenceRecordIntermediate, Record: EvidenceRecord{Intermediate: &intermediate}}
	intermediateFrame.RecordDigest, err = intermediateFrame.ComputeDigest()
	if err != nil || intermediateFrame.Validate() != nil || intermediateFrame.RecordDigest != seed.intermediateRecordDigest {
		return nil, invalidEvidence("runner-committed-terminal", "intermediate frame")
	}
	commit := cloneProjectionValue(seed.commit)
	commitFrame := EvidenceFrame{FormatVersion: EvidenceFrameFormat, Sequence: 3, PreviousRecordDigest: digestPointer(intermediateFrame.RecordDigest), RecordKind: EvidenceRecordCommitIntent, Record: EvidenceRecord{CommitIntent: &commit}}
	commitFrame.RecordDigest, err = commitFrame.ComputeDigest()
	if err != nil || commitFrame.Validate() != nil || commitFrame.RecordDigest != seed.commitRecordDigest {
		return nil, invalidEvidence("runner-committed-terminal", "commit intent frame")
	}
	return []EvidenceFrame{headerFrame, intentFrame, intermediateFrame, commitFrame}, nil
}
