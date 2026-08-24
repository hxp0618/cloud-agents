package migration

import "context"

// runnerStatementIntentRecordBinder is the only runner-facing bridge that may
// mint an OwnedEvidenceRecord for the first statement of a brand-new active
// generation. It deliberately exposes no append method and accepts no caller
// supplied cursor, header, prefix, or chain witness.
type runnerStatementIntentRecordBinder interface {
	bindRunnerStatementIntentRecord(context.Context, runnerStatementIntentRecordRequest) (EvidenceJournal, JournalCursor, *OwnedEvidenceRecord, error)
	runnerStatementIntentRecordBinderSealed()
}

type runnerStatementIntentRecordRequest struct {
	candidateBinding *verifiedEvidenceRunBinding
	generation       generationIdentity
	recoveryDigest   [32]byte
	maxAttempts      uint32
	plan             StatementPlan
	authorityBefore  ProjectionResultEvidence
	catalogBefore    ProjectionResultEvidence
}

func runnerStatementIntentVerifiedSubject(bindings RunnerProjectionBindings, plan StatementPlan, authorityBefore, catalogBefore ProjectionResultEvidence) (Digest, error) {
	if plan.validateExact() != nil {
		return "", fail(CodeUntrusted, "runner-statement-intent-record", "exact statement plan is unavailable", nil)
	}
	catalog, ok := exactCatalogBindingForHead(bindings.executableCatalogs, plan.MigrationID)
	if !ok || catalog.catalogContractDigest.Validate() != nil {
		return "", fail(CodeUntrusted, "runner-statement-intent-record", "statement catalog authority is unavailable", nil)
	}
	expectedAuthority, expectedAuthorityErr := bindings.verifiedAuthority.ExpectedProjection(AuthorityPhaseMigrationTransaction)
	expectedAuthorityDigest, expectedAuthorityDigestErr := digestProjectionWrapper(AuthorityProjectionDigestDomain, expectedAuthority)
	if expectedAuthorityErr != nil || expectedAuthorityDigestErr != nil || authorityBefore.Digest != expectedAuthorityDigest || authorityBefore.Metadata.VerifiedSubjectDigest != bindings.verifiedAuthority.SubjectDigest() || catalogBefore.Metadata.VerifiedSubjectDigest != bindings.initialSchemaScope.SubjectDigest() {
		return "", fail(CodeUntrusted, "runner-statement-intent-record", "statement projection evidence differs from the verified subjects", nil)
	}
	return catalog.catalogContractDigest, nil
}

func bindBrandNewRunnerStatementIntentRecord(plan StatementPlan, authorityBefore, catalogBefore ProjectionResultEvidence, catalogContractDigest Digest, generation generationIdentity, cursor JournalCursor, recovery *RecoverySnapshot, header JournalHeader, chain verifiedEvidenceChainWitness) (*OwnedEvidenceRecord, error) {
	ownedPlan, err := cloneRunnerStatementIntentPlan(plan)
	if err != nil || ownedPlan.StatementIndex != 0 || catalogContractDigest.Validate() != nil || !sameGenerationHeader(generation, header) || !runnerStatementIntentProjectionEvidenceMatches(ownedPlan, authorityBefore, catalogBefore) {
		return nil, invalidEvidence("runner-statement-intent-record", "plan, projection, catalog, or generation binding")
	}
	if !cursor.Valid() || !sameGenerationIdentity(cursor.generation, generation) || cursor.segmentIndex != 0 || cursor.nextSequence != 1 || cursor.previousRecordDigest == nil || cursor.latestCheckpointRecordDigest != nil || !validRecoverySnapshotForJournal(recovery, generation, cursor) || !runnerBrandNewRecoverySnapshot(recovery) {
		return nil, invalidEvidence("runner-statement-intent-record", "brand-new cursor or recovery boundary")
	}
	headerFrame := EvidenceFrame{
		FormatVersion: EvidenceFrameFormat,
		Sequence:      0,
		RecordKind:    EvidenceRecordHeader,
		Record:        EvidenceRecord{Header: &header},
	}
	headerFrame.RecordDigest, err = headerFrame.ComputeDigest()
	if err != nil || headerFrame.Validate() != nil || headerFrame.RecordDigest != *cursor.previousRecordDigest || recovery.tailDigest != headerFrame.RecordDigest {
		return nil, invalidEvidence("runner-statement-intent-record", "segment-zero header differs from the active cursor")
	}
	intent, err := buildBrandNewRunnerStatementIntent(
		ownedPlan, authorityBefore, catalogBefore, catalogContractDigest,
		header.SchemaBundleDigest, header.AuthorityProfileDigest, header.AuthorityBindingDigest,
	)
	if err != nil {
		return nil, err
	}
	witness := ownedStatementIntentWitness{ownedAppendContext{
		generation: generation,
		cursor:     cursor.clone(),
		prefix:     []EvidenceFrame{cloneProjectionValue(headerFrame)},
		chain:      cloneRunnerEvidenceChainWitness(chain),
	}, ownedPlan}
	return bindOwnedEvidenceRecord(EvidenceRecord{StatementIntent: &intent}, witness)
}

func buildBrandNewRunnerStatementIntent(plan StatementPlan, authorityBefore, catalogBefore ProjectionResultEvidence, catalogContractDigest, schemaBundleDigest, authorityProfileDigest, authorityBindingDigest Digest) (StatementIntent, error) {
	ownedPlan, err := cloneRunnerStatementIntentPlan(plan)
	if err != nil || ownedPlan.StatementIndex != 0 || catalogContractDigest.Validate() != nil || schemaBundleDigest.Validate() != nil || authorityProfileDigest.Validate() != nil || authorityBindingDigest.Validate() != nil || !runnerStatementIntentProjectionEvidenceMatches(ownedPlan, authorityBefore, catalogBefore) {
		return StatementIntent{}, invalidEvidence("runner-statement-intent-record", "plan, projection, catalog, or generation binding")
	}
	intent := StatementIntent{
		SchemaBundleDigest:              schemaBundleDigest,
		CatalogContractDigest:           catalogContractDigest,
		AuthorityProfileDigest:          authorityProfileDigest,
		AuthorityBindingDigest:          authorityBindingDigest,
		MigrationID:                     ownedPlan.MigrationID,
		AttemptIndex:                    1,
		StatementIndex:                  ownedPlan.StatementIndex,
		SQLPath:                         ownedPlan.SQLArtifactPath,
		SQLArtifactSHA256:               ownedPlan.SQLArtifactSHA256,
		SQLArtifactSizeBytes:            ownedPlan.SQLArtifactSizeBytes,
		StartOffset:                     ownedPlan.StartOffset,
		EndOffset:                       ownedPlan.EndOffset,
		StatementSHA256:                 ownedPlan.StatementSHA256,
		Classification:                  cloneProjectionValue(ownedPlan.Classification),
		ExpectedTransitionDigest:        ownedPlan.ExpectedTransitionDigest,
		AuthorityBeforeDigest:           authorityBefore.Digest,
		CatalogBeforeDigest:             catalogBefore.Digest,
		AuthorityBeforeResult:           cloneProjectionValue(authorityBefore),
		CatalogBeforeResult:             cloneProjectionValue(catalogBefore),
		PreviousAttemptTerminalDigest:   nil,
		PreviousIntermediateStateDigest: nil,
	}
	if err := intent.Validate(); err != nil || intent.CatalogBeforeDigest != ownedPlan.ExpectedTransition.CatalogBefore.Digest {
		return StatementIntent{}, invalidEvidence("runner-statement-intent-record", "statement intent differs from the exact first-statement plan")
	}
	return intent, nil
}

func runnerStatementIntentProjectionEvidenceMatches(plan StatementPlan, authorityBefore, catalogBefore ProjectionResultEvidence) bool {
	if plan.validateExact() != nil || authorityBefore.Validate() != nil || catalogBefore.Validate() != nil || authorityBefore.Digest.Validate() != nil || catalogBefore.Digest != plan.ExpectedTransition.CatalogBefore.Digest || !runnerCanonicalEqual(authorityBefore.Metadata.Snapshot, catalogBefore.Metadata.Snapshot) {
		return false
	}
	snapshot := authorityBefore.Metadata.Snapshot
	return snapshot.Mode == MigrationSnapshot && snapshot.Ownership == BorrowedMigrationSnapshot && snapshot.AuthorityPhase == AuthorityPhaseMigrationTransaction && snapshot.MigrationID != nil && *snapshot.MigrationID == plan.MigrationID && snapshot.StatementIndex != nil && *snapshot.StatementIndex == plan.StatementIndex &&
		authorityBefore.Metadata.ProjectionKind == ProjectionKindAuthority && authorityBefore.Metadata.Scope == nil &&
		catalogBefore.Metadata.ProjectionKind == ProjectionKindCatalogState && catalogBefore.Metadata.Scope != nil && equalProjectionScopes(*catalogBefore.Metadata.Scope, plan.ExpectedTransition.CatalogBefore.Scope)
}

func cloneRunnerStatementIntentPlan(plan StatementPlan) (StatementPlan, error) {
	if err := plan.validateExact(); err != nil {
		return StatementPlan{}, err
	}
	owned := plan
	owned.Grantee = cloneProjectionValue(plan.Grantee)
	owned.Classification = cloneProjectionValue(plan.Classification)
	owned.ExpectedTransition = cloneProjectionValue(plan.ExpectedTransition)
	owned.sqlBytes = append([]byte(nil), plan.sqlBytes...)
	if err := owned.validateExact(); err != nil {
		return StatementPlan{}, err
	}
	return owned, nil
}

func cloneRunnerEvidenceChainWitness(chain verifiedEvidenceChainWitness) verifiedEvidenceChainWitness {
	owned := chain
	owned.maxAttempts = cloneUint32Map(chain.maxAttempts)
	owned.finalStatementIndex = cloneUint32Map(chain.finalStatementIndex)
	owned.finalCatalogDigest = make(map[string]Digest, len(chain.finalCatalogDigest))
	for key, value := range chain.finalCatalogDigest {
		owned.finalCatalogDigest[key] = value
	}
	owned.plans = make(map[string]exactStatementEvidenceWitness, len(chain.plans))
	for key, value := range chain.plans {
		owned.plans[key] = value
	}
	owned.retryReceipts = make(map[Digest]verifiedRetryReceipt, len(chain.retryReceipts))
	for key, value := range chain.retryReceipts {
		owned.retryReceipts[key] = value
	}
	owned.ambiguousBoundaries = make(map[Digest]ownedAmbiguousBoundaryWitness, len(chain.ambiguousBoundaries))
	for key, value := range chain.ambiguousBoundaries {
		owned.ambiguousBoundaries[key] = value
	}
	return owned
}
