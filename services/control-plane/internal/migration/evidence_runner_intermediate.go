package migration

import "context"

// runnerIntermediateRecordBinder is the only runner-facing bridge that may
// mint a final StatementIntermediateEvidence append authority. It accepts no
// caller-supplied cursor, prefix, header, or chain witness and exposes no
// append or database mutation method.
type runnerIntermediateRecordBinder interface {
	bindRunnerIntermediateRecord(context.Context, runnerIntermediateRecordRequest) (EvidenceJournal, JournalCursor, *OwnedEvidenceRecord, error)
	runnerIntermediateRecordBinderSealed()
}

type runnerIntermediateRecordRequest struct {
	candidateBinding   *verifiedEvidenceRunBinding
	generation         generationIdentity
	recoveryDigest     [32]byte
	maxAttempts        uint32
	plan               StatementPlan
	intent             StatementIntent
	state              StatementIntermediateState
	authorityAfter     ProjectionResultEvidence
	catalogAfter       ProjectionResultEvidence
	preledgerAuthority ProjectionResultEvidence
	preledgerCatalog   ProjectionResultEvidence
}

func buildRunnerFinalIntermediateEvidence(request runnerIntermediateRecordRequest) (StatementIntermediateEvidence, error) {
	plan, err := cloneRunnerStatementIntentPlan(request.plan)
	if err != nil || request.intent.Validate() != nil || request.state.Validate() != nil || !planMatchesIntent(exactStatementWitnessFromPlan(plan, request.intent.AttemptIndex), request.intent) {
		return StatementIntermediateEvidence{}, invalidEvidence("runner-intermediate-record", "plan, intent, or state binding")
	}
	preledgerAuthority := cloneProjectionValue(request.preledgerAuthority)
	preledgerCatalog := cloneProjectionValue(request.preledgerCatalog)
	intermediate := StatementIntermediateEvidence{
		State:                    cloneProjectionValue(request.state),
		AuthorityBeforeResult:    cloneProjectionValue(request.intent.AuthorityBeforeResult),
		CatalogBeforeResult:      cloneProjectionValue(request.intent.CatalogBeforeResult),
		AuthorityAfterResult:     cloneProjectionValue(request.authorityAfter),
		CatalogAfterResult:       cloneProjectionValue(request.catalogAfter),
		PreledgerAuthorityResult: &preledgerAuthority,
		PreledgerCatalogResult:   &preledgerCatalog,
	}
	if intermediate.Validate() != nil || !runnerFinalIntermediateShapeMatches(plan, request.intent, intermediate) {
		return StatementIntermediateEvidence{}, invalidEvidence("runner-intermediate-record", "final intermediate evidence is contradictory")
	}
	return intermediate, nil
}

func runnerFinalIntermediateShapeMatches(plan StatementPlan, intent StatementIntent, intermediate StatementIntermediateEvidence) bool {
	if plan.validateExact() != nil || intent.Validate() != nil || intermediate.Validate() != nil || intermediate.PreledgerAuthorityResult == nil || intermediate.PreledgerCatalogResult == nil || !planMatchesIntent(exactStatementWitnessFromPlan(plan, intent.AttemptIndex), intent) || !runnerStatementIntentProjectionEvidenceMatches(plan, intent.AuthorityBeforeResult, intent.CatalogBeforeResult) || !projectionEvidenceEqual(intermediate.AuthorityBeforeResult, intent.AuthorityBeforeResult) || !projectionEvidenceEqual(intermediate.CatalogBeforeResult, intent.CatalogBeforeResult) {
		return false
	}
	state := intermediate.State
	if state.MigrationID != plan.MigrationID || state.AttemptIndex != intent.AttemptIndex || state.StatementIndex != plan.StatementIndex || state.StatementSHA256 != plan.StatementSHA256 || state.SchemaBundleDigest != intent.SchemaBundleDigest || state.CatalogContractDigest != intent.CatalogContractDigest || state.AuthorityProfileDigest != intent.AuthorityProfileDigest || state.AuthorityBindingDigest != intent.AuthorityBindingDigest || state.ControlPlaneStates.ExpectedTransitionDigest != plan.ExpectedTransitionDigest || !equalDigestPointer(state.PreviousAttemptTerminalDigest, intent.PreviousAttemptTerminalDigest) || !equalDigestPointer(state.PreviousIntermediateStateDigest, intent.PreviousIntermediateStateDigest) {
		return false
	}
	if state.AuthorityBeforeDigest != intent.AuthorityBeforeDigest || state.CatalogBeforeDigest != intent.CatalogBeforeDigest || state.AuthorityAfterDigest != intermediate.AuthorityAfterResult.Digest || state.CatalogAfterDigest != intermediate.CatalogAfterResult.Digest || intermediate.PreledgerAuthorityResult.Digest != state.AuthorityAfterDigest {
		return false
	}
	statementIndex := plan.StatementIndex
	if !runnerIntermediateSnapshotPair(intermediate.AuthorityAfterResult.Metadata, intermediate.CatalogAfterResult.Metadata, plan.MigrationID, &statementIndex) || !runnerIntermediateSnapshotPair(intermediate.PreledgerAuthorityResult.Metadata, intermediate.PreledgerCatalogResult.Metadata, plan.MigrationID, nil) || !runnerIntermediateSameTransactionIdentity(intent.AuthorityBeforeResult.Metadata.Snapshot, intermediate.AuthorityAfterResult.Metadata.Snapshot) || !runnerIntermediateSameTransactionIdentity(intermediate.AuthorityAfterResult.Metadata.Snapshot, intermediate.PreledgerAuthorityResult.Metadata.Snapshot) {
		return false
	}
	return intermediate.AuthorityAfterResult.Metadata.ProjectionKind == ProjectionKindAuthority && intermediate.AuthorityAfterResult.Metadata.Scope == nil &&
		intermediate.CatalogAfterResult.Metadata.ProjectionKind == ProjectionKindCatalogState && intermediate.CatalogAfterResult.Metadata.Scope != nil && equalProjectionScopes(*intermediate.CatalogAfterResult.Metadata.Scope, plan.ExpectedTransition.CatalogAfter.Scope) &&
		intermediate.PreledgerAuthorityResult.Metadata.ProjectionKind == ProjectionKindAuthority && intermediate.PreledgerAuthorityResult.Metadata.Scope == nil &&
		intermediate.PreledgerCatalogResult.Metadata.ProjectionKind == ProjectionKindCatalog && intermediate.PreledgerCatalogResult.Metadata.Scope != nil && equalProjectionScopes(*intermediate.PreledgerCatalogResult.Metadata.Scope, plan.ExpectedTransition.CatalogAfter.Scope)
}

func runnerIntermediateSameTransactionIdentity(left, right SnapshotMetadata) bool {
	if left.validate() != nil || right.validate() != nil {
		return false
	}
	left.StatementIndex = nil
	right.StatementIndex = nil
	return runnerCanonicalEqual(left, right)
}

func runnerIntermediateSnapshotPair(authority, catalog ProjectionMetadata, migrationID string, statementIndex *uint32) bool {
	if authority.validate() != nil || catalog.validate() != nil || !runnerCanonicalEqual(authority.Snapshot, catalog.Snapshot) {
		return false
	}
	snapshot := authority.Snapshot
	return snapshot.Mode == MigrationSnapshot && snapshot.Ownership == BorrowedMigrationSnapshot && snapshot.AuthorityPhase == AuthorityPhaseMigrationTransaction && snapshot.MigrationID != nil && *snapshot.MigrationID == migrationID && sameRunnerStatementIndex(snapshot.StatementIndex, statementIndex)
}

func runnerFinalIntermediateVerifiedSubjects(bindings RunnerProjectionBindings, request runnerIntermediateRecordRequest) error {
	intermediate, err := buildRunnerFinalIntermediateEvidence(request)
	if err != nil {
		return err
	}
	catalogSubject, err := runnerStatementIntentVerifiedSubject(bindings, request.plan, intermediate.AuthorityBeforeResult, intermediate.CatalogBeforeResult)
	if err != nil {
		return err
	}
	if catalogSubject != request.intent.CatalogContractDigest {
		return fail(CodeUntrusted, "runner-intermediate-record", "statement catalog subject differs from durable intent", nil)
	}
	catalog, ok := exactCatalogBindingForHead(bindings.executableCatalogs, request.plan.MigrationID)
	if !ok || !runnerStatementAfterCatalogMatchesPlan(catalog, request.plan, request.intent) {
		return fail(CodeUntrusted, "runner-intermediate-record", "final catalog binding differs from the exact statement plan", nil)
	}
	source, sourceErr := exactMigrationSource(catalog.catalogContract.SourceDescriptors, request.plan.MigrationID)
	expectedAuthority, authorityErr := bindings.verifiedAuthority.ExpectedProjection(AuthorityPhaseMigrationTransaction)
	expectedAuthorityDigest, authorityDigestErr := digestProjectionWrapper(AuthorityProjectionDigestDomain, expectedAuthority)
	expectedCatalog := catalog.verifiedCatalog.ExpectedProjection()
	expectedCatalogDigest, catalogDigestErr := digestProjectionWrapper(CatalogProjectionDigestDomain, expectedCatalog)
	finalState := CatalogStateProjection{Present: &SchemaPresentProjection{State: "schema_present", Scope: cloneProjectionValue(request.plan.ExpectedTransition.CatalogAfter.Scope), Body: cloneProjectionValue(expectedCatalog.Body)}}
	finalStateDigest, finalStateErr := finalState.ComputeDigest()
	if sourceErr != nil || uint64(request.plan.StatementIndex)+1 != uint64(len(source.Statements)) || authorityErr != nil || authorityDigestErr != nil || catalogDigestErr != nil || finalStateErr != nil || finalStateDigest != request.plan.ExpectedTransition.CatalogAfter.Digest {
		return fail(CodeUntrusted, "runner-intermediate-record", "verified final statement subjects are incomplete or contradictory", nil)
	}
	preledgerAuthority := intermediate.PreledgerAuthorityResult
	preledgerCatalog := intermediate.PreledgerCatalogResult
	if intermediate.AuthorityAfterResult.Digest != expectedAuthorityDigest || preledgerAuthority.Digest != expectedAuthorityDigest || intermediate.CatalogAfterResult.Digest != finalStateDigest || preledgerCatalog.Digest != expectedCatalogDigest || intermediate.AuthorityAfterResult.Metadata.VerifiedSubjectDigest != bindings.verifiedAuthority.SubjectDigest() || preledgerAuthority.Metadata.VerifiedSubjectDigest != bindings.verifiedAuthority.SubjectDigest() || intermediate.CatalogAfterResult.Metadata.VerifiedSubjectDigest != catalog.verifiedCatalog.SubjectDigest() || preledgerCatalog.Metadata.VerifiedSubjectDigest != catalog.verifiedCatalog.SubjectDigest() || !equalProjectionScopes(*preledgerCatalog.Metadata.Scope, catalog.verifiedCatalog.Scope()) {
		return fail(CodeUntrusted, "runner-intermediate-record", "intermediate projection evidence differs from verified subjects", nil)
	}
	return nil
}

func bindBrandNewRunnerFinalIntermediateRecord(request runnerIntermediateRecordRequest, generation generationIdentity, cursor JournalCursor, recovery *RecoverySnapshot, header JournalHeader, chain verifiedEvidenceChainWitness) (*OwnedEvidenceRecord, error) {
	intermediate, err := buildRunnerFinalIntermediateEvidence(request)
	if err != nil || request.maxAttempts == 0 || request.intent.AttemptIndex == 0 || request.intent.AttemptIndex > request.maxAttempts || !sameGenerationHeader(generation, header) || recovery == nil {
		return nil, invalidEvidence("runner-intermediate-record", "intermediate, generation, or attempt binding")
	}
	wantAction := recoveryAbortAction(request.intent.AttemptIndex, request.maxAttempts)
	prior := recovery.lastStatementIntent
	if !cursor.Valid() || !sameGenerationIdentity(cursor.generation, generation) || cursor.segmentIndex != 0 || cursor.nextSequence != 2 || cursor.previousRecordDigest == nil || cursor.latestCheckpointRecordDigest == nil || !validRecoverySnapshotForJournal(recovery, generation, cursor) || recovery.state != RecoveryDanglingStatementIntent || recovery.nextPermittedAction != wantAction || recovery.migrationID == nil || *recovery.migrationID != request.intent.MigrationID || recovery.attemptIndex == nil || *recovery.attemptIndex != request.intent.AttemptIndex || prior == nil || prior.owner != generation.owner || !sameGenerationIdentity(prior.generation, generation) || !sameCursorIdentity(prior.cursor, cursor) || prior.tailDigest != *cursor.previousRecordDigest || prior.recordDigest != *cursor.previousRecordDigest || !canonicalEqual(prior.value, request.intent) || recovery.lastStatementIntentRecordDigest == nil || *recovery.lastStatementIntentRecordDigest != prior.recordDigest || recovery.lineageContinuation != nil || recovery.lastIntermediateEvidence != nil || recovery.lastIntermediateEvidenceRecordDigest != nil || recovery.lastIntermediateStateDigest != nil || recovery.commitIntent != nil || recovery.lastCommitIntentRecordDigest != nil || recovery.lastTerminal != nil || recovery.lastTerminalDigest != nil || recovery.lastResolution != nil || recovery.lastResolutionDigest != nil || recovery.previousAttemptTerminalDigest != nil {
		return nil, invalidEvidence("runner-intermediate-record", "durable statement intent boundary is unavailable")
	}
	headerFrame := EvidenceFrame{FormatVersion: EvidenceFrameFormat, Sequence: 0, RecordKind: EvidenceRecordHeader, Record: EvidenceRecord{Header: &header}}
	headerFrame.RecordDigest, err = headerFrame.ComputeDigest()
	if err != nil || headerFrame.Validate() != nil {
		return nil, invalidEvidence("runner-intermediate-record", "generation header frame is invalid")
	}
	intentFrame := EvidenceFrame{
		FormatVersion: EvidenceFrameFormat, Sequence: 1, PreviousRecordDigest: digestPointer(headerFrame.RecordDigest),
		RecordKind: EvidenceRecordStatementIntent, Record: EvidenceRecord{StatementIntent: cloneStatementIntentPointer(&request.intent)},
	}
	intentFrame.RecordDigest, err = intentFrame.ComputeDigest()
	if err != nil || intentFrame.Validate() != nil || intentFrame.RecordDigest != *cursor.previousRecordDigest || intentFrame.RecordDigest != prior.recordDigest {
		return nil, invalidEvidence("runner-intermediate-record", "durable statement intent frame is invalid")
	}
	plan, err := cloneRunnerStatementIntentPlan(request.plan)
	if err != nil {
		return nil, err
	}
	witness := ownedIntermediateWitness{
		ownedAppendContext: ownedAppendContext{
			generation: generation, cursor: cursor.clone(),
			prefix: []EvidenceFrame{cloneProjectionValue(headerFrame), cloneProjectionValue(intentFrame)},
			chain:  cloneRunnerEvidenceChainWitness(chain),
		},
		plan: plan, stateDigest: intermediate.State.IntermediateStateDigest, priorIntent: cloneProjectionValue(intentFrame),
	}
	return bindOwnedEvidenceRecord(EvidenceRecord{Intermediate: &intermediate}, witness)
}

func cloneStatementIntentPointer(value *StatementIntent) *StatementIntent {
	if value == nil {
		return nil
	}
	owned := cloneProjectionValue(*value)
	return &owned
}
