package migration

import "context"

// runnerCommitIntentRecordBinder is the only runner-facing bridge that may
// mint a CommitIntent append authority. It accepts no caller-supplied cursor,
// prefix, header, schema witness, or transaction mutation capability.
type runnerCommitIntentRecordBinder interface {
	bindRunnerCommitIntentRecord(context.Context, runnerCommitIntentRecordRequest) (EvidenceJournal, JournalCursor, *OwnedEvidenceRecord, error)
	runnerCommitIntentRecordBinderSealed()
}

type runnerCommitIntentRecordRequest struct {
	candidateBinding   *verifiedEvidenceRunBinding
	generation         generationIdentity
	recoveryDigest     [32]byte
	maxAttempts        uint32
	planCount          uint32
	plan               StatementPlan
	intent             StatementIntent
	intermediate       StatementIntermediateEvidence
	ledgerRow          CommitIntentLedgerRow
	ledgerPrefixDigest Digest
	ledgerHead         string
	ledgerLength       uint32
}

func buildRunnerCommitIntent(request runnerCommitIntentRecordRequest) (CommitIntent, error) {
	plan, err := cloneRunnerStatementIntentPlan(request.plan)
	if err != nil || plan.StatementIndex != 0 || request.planCount != 1 || request.intent.Validate() != nil || request.intermediate.Validate() != nil || !runnerFinalIntermediateShapeMatches(plan, request.intent, request.intermediate) || request.intermediate.PreledgerCatalogResult == nil || request.ledgerRow.Validate() != nil || request.ledgerPrefixDigest.Validate() != nil || request.ledgerHead != request.intent.MigrationID || request.ledgerLength != 1 || request.ledgerRow.MigrationID != request.intent.MigrationID || request.ledgerRow.BundleDigest != request.generation.schemaBundleDigest || request.ledgerRow.SQLSHA256 != plan.SQLArtifactSHA256 || request.ledgerRow.SQLSizeBytes != plan.SQLArtifactSizeBytes {
		return CommitIntent{}, invalidEvidence("runner-commit-intent-record", "plan, intermediate, or ledger binding")
	}
	wantPrefix, err := LedgerPrefixDigest([]CommitIntentLedgerRow{request.ledgerRow})
	if err != nil || wantPrefix != request.ledgerPrefixDigest || request.intent.AttemptIndex != 1 || request.intent.PreviousAttemptTerminalDigest != nil || request.intent.PreviousIntermediateStateDigest != nil || request.intent.CatalogBeforeDigest != plan.ExpectedTransition.CatalogBefore.Digest {
		return CommitIntent{}, invalidEvidence("runner-commit-intent-record", "attempt predecessor or ledger prefix")
	}
	commit := CommitIntent{
		SchemaBundleDigest: request.generation.schemaBundleDigest, CatalogContractDigest: request.intent.CatalogContractDigest,
		AuthorityProfileDigest: request.intent.AuthorityProfileDigest, AuthorityBindingDigest: request.intent.AuthorityBindingDigest,
		MigrationID: request.intent.MigrationID, AttemptIndex: request.intent.AttemptIndex,
		PreviousAttemptTerminalDigest:   cloneDigestPointer(request.intent.PreviousAttemptTerminalDigest),
		AttemptPredecessorCatalogDigest: plan.ExpectedTransition.CatalogBefore.Digest,
		LastIntermediateStateDigest:     request.intermediate.State.IntermediateStateDigest,
		ExpectedLedgerLength:            request.ledgerLength, ExpectedLedgerHead: request.ledgerHead,
		LedgerRow: cloneProjectionValue(request.ledgerRow),
	}
	if err := commit.Validate(); err != nil {
		return CommitIntent{}, err
	}
	return commit, nil
}

func runnerCommitIntentVerifiedSubjects(bindings RunnerProjectionBindings, request runnerCommitIntentRecordRequest) error {
	if request.intermediate.PreledgerAuthorityResult == nil || request.intermediate.PreledgerCatalogResult == nil {
		return fail(CodeUntrusted, "runner-commit-intent-record", "final pre-ledger projection subjects are unavailable", nil)
	}
	intermediateRequest := runnerIntermediateRecordRequest{
		candidateBinding: request.candidateBinding, generation: request.generation, recoveryDigest: request.recoveryDigest,
		maxAttempts: request.maxAttempts, plan: request.plan, intent: request.intent,
		state: request.intermediate.State, authorityAfter: request.intermediate.AuthorityAfterResult,
		catalogAfter:       request.intermediate.CatalogAfterResult,
		preledgerAuthority: cloneProjectionValue(*request.intermediate.PreledgerAuthorityResult),
		preledgerCatalog:   cloneProjectionValue(*request.intermediate.PreledgerCatalogResult),
	}
	if err := runnerFinalIntermediateVerifiedSubjects(bindings, intermediateRequest); err != nil {
		return err
	}
	commit, err := buildRunnerCommitIntent(request)
	if err != nil {
		return err
	}
	if commit.SchemaBundleDigest != bindings.schemaBundleDigest || commit.AuthorityProfileDigest != bindings.authorityProfileDigest || commit.AuthorityBindingDigest != bindings.authorityBindingDigest || commit.AttemptPredecessorCatalogDigest != request.plan.ExpectedTransition.CatalogBefore.Digest || commit.LastIntermediateStateDigest != request.intermediate.State.IntermediateStateDigest {
		return fail(CodeUntrusted, "runner-commit-intent-record", "commit intent differs from verified projection subjects", nil)
	}
	return nil
}

func bindBrandNewRunnerCommitIntentRecord(request runnerCommitIntentRecordRequest, generation generationIdentity, cursor JournalCursor, recovery *RecoverySnapshot, header JournalHeader, chain verifiedEvidenceChainWitness) (*OwnedEvidenceRecord, error) {
	commit, err := buildRunnerCommitIntent(request)
	if err != nil || request.maxAttempts == 0 || request.intent.AttemptIndex == 0 || request.intent.AttemptIndex > request.maxAttempts || !sameGenerationHeader(generation, header) || recovery == nil {
		return nil, invalidEvidence("runner-commit-intent-record", "commit, generation, or attempt binding")
	}
	wantAction := recoveryAbortAction(request.intent.AttemptIndex, request.maxAttempts)
	recoveredIntent := recovery.lastStatementIntent
	recoveredIntermediate := recovery.lastIntermediateEvidence
	if !cursor.Valid() || !sameGenerationIdentity(cursor.generation, generation) || cursor.segmentIndex != 0 || cursor.nextSequence != 3 || cursor.previousRecordDigest == nil || cursor.latestCheckpointRecordDigest == nil || !validRecoverySnapshotForJournal(recovery, generation, cursor) || recovery.state != RecoveryDanglingIntermediate || recovery.nextPermittedAction != wantAction || recovery.migrationID == nil || *recovery.migrationID != request.intent.MigrationID || recovery.attemptIndex == nil || *recovery.attemptIndex != request.intent.AttemptIndex || recoveredIntent == nil || recoveredIntermediate == nil || recovery.lastStatementIntentRecordDigest == nil || recovery.lastIntermediateEvidenceRecordDigest == nil || recovery.lastIntermediateStateDigest == nil || *recovery.lastIntermediateStateDigest != request.intermediate.State.IntermediateStateDigest || recoveredIntent.owner != generation.owner || recoveredIntermediate.owner != generation.owner || !sameGenerationIdentity(recoveredIntent.generation, generation) || !sameGenerationIdentity(recoveredIntermediate.generation, generation) || !sameCursorIdentity(recoveredIntent.cursor, cursor) || !sameCursorIdentity(recoveredIntermediate.cursor, cursor) || recoveredIntent.tailDigest != *cursor.previousRecordDigest || recoveredIntermediate.tailDigest != *cursor.previousRecordDigest || !canonicalEqual(recoveredIntent.value, request.intent) || !canonicalEqual(recoveredIntermediate.value, request.intermediate) || recoveredIntermediate.recordDigest != *cursor.previousRecordDigest || *recovery.lastIntermediateEvidenceRecordDigest != recoveredIntermediate.recordDigest || *recovery.lastStatementIntentRecordDigest != recoveredIntent.recordDigest || recovery.lineageContinuation != nil || recovery.commitIntent != nil || recovery.lastCommitIntentRecordDigest != nil || recovery.lastTerminal != nil || recovery.lastTerminalDigest != nil || recovery.lastResolution != nil || recovery.lastResolutionDigest != nil || !equalDigestPointer(recovery.previousAttemptTerminalDigest, request.intent.PreviousAttemptTerminalDigest) {
		return nil, invalidEvidence("runner-commit-intent-record", "durable final intermediate boundary is unavailable")
	}
	headerFrame := EvidenceFrame{FormatVersion: EvidenceFrameFormat, Sequence: 0, RecordKind: EvidenceRecordHeader, Record: EvidenceRecord{Header: &header}}
	headerFrame.RecordDigest, err = headerFrame.ComputeDigest()
	if err != nil || headerFrame.Validate() != nil {
		return nil, invalidEvidence("runner-commit-intent-record", "generation header frame is invalid")
	}
	intentFrame := EvidenceFrame{
		FormatVersion: EvidenceFrameFormat, Sequence: 1, PreviousRecordDigest: digestPointer(headerFrame.RecordDigest),
		RecordKind: EvidenceRecordStatementIntent, Record: EvidenceRecord{StatementIntent: cloneStatementIntentPointer(&request.intent)},
	}
	intentFrame.RecordDigest, err = intentFrame.ComputeDigest()
	if err != nil || intentFrame.Validate() != nil || intentFrame.RecordDigest != recoveredIntent.recordDigest {
		return nil, invalidEvidence("runner-commit-intent-record", "durable statement intent frame is invalid")
	}
	intermediate := cloneProjectionValue(request.intermediate)
	intermediateFrame := EvidenceFrame{
		FormatVersion: EvidenceFrameFormat, Sequence: 2, PreviousRecordDigest: digestPointer(intentFrame.RecordDigest),
		RecordKind: EvidenceRecordIntermediate, Record: EvidenceRecord{Intermediate: &intermediate},
	}
	intermediateFrame.RecordDigest, err = intermediateFrame.ComputeDigest()
	if err != nil || intermediateFrame.Validate() != nil || intermediateFrame.RecordDigest != recoveredIntermediate.recordDigest || intermediateFrame.RecordDigest != *cursor.previousRecordDigest {
		return nil, invalidEvidence("runner-commit-intent-record", "durable final intermediate frame is invalid")
	}
	witness := ownedCommitIntentWitness{
		ownedAppendContext: ownedAppendContext{
			generation: generation, cursor: cursor.clone(),
			prefix: []EvidenceFrame{cloneProjectionValue(headerFrame), cloneProjectionValue(intentFrame), cloneProjectionValue(intermediateFrame)},
			chain:  cloneRunnerEvidenceChainWitness(chain),
		},
		priorIntermediateStateDigest: request.intermediate.State.IntermediateStateDigest,
		lastIntermediateRecordDigest: intermediateFrame.RecordDigest,
		priorIntermediate:            cloneProjectionValue(intermediateFrame),
	}
	return bindOwnedEvidenceRecord(EvidenceRecord{CommitIntent: &commit}, witness)
}
