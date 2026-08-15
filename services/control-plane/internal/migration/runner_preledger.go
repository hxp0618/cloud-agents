package migration

import (
	"context"
	"crypto/sha256"
	"strconv"
	"sync"
	"sync/atomic"
)

// runnerProjectedCurrentPreledger proves that the final statement's immediate
// catalog-after body is byte-equal to a fresh statement_index=null final
// catalog projection on the same transaction. It exposes neither ledger insert
// nor commit; the final intermediate must become durable first.
type runnerProjectedCurrentPreledger struct {
	self                    *runnerProjectedCurrentPreledger
	binding                 *runnerProjectedCurrentPreledgerBinding
	session                 DatabaseSession
	transaction             MigrationTransaction
	evidence                EvidenceSession
	journal                 EvidenceJournal
	key                     int64
	candidateBinding        *verifiedEvidenceRunBinding
	generation              generationIdentity
	statementAfterCanonical [32]byte
	recoveryDigest          [32]byte
	dispatch                runnerPreparedDispatch
	database                runnerPreparedDatabaseIdentity
	maxAttempts             uint32
	policy                  ExecutionPolicy
	plan                    StatementPlan
	intent                  StatementIntent
	cursor                  JournalCursor
	intentRecordDigest      Digest
	checkpointDigest        Digest
	executedStatementDigest Digest
	state                   StatementIntermediateState
	authorityAfter          ProjectionResultEvidence
	catalogAfter            ProjectionResultEvidence
	preledgerSnapshotDigest [32]byte
	preledgerAuthority      ProjectionResultEvidence
	preledgerCatalog        ProjectionResultEvidence
	preledgerCatalogBody    CatalogProjection
	boundary                BoundaryState
	canonical               [32]byte
	closed                  bool
}

type runnerProjectedCurrentPreledgerBinding struct {
	prepared         *runnerProjectedCurrentPreledger
	session          DatabaseSession
	transaction      MigrationTransaction
	evidence         EvidenceSession
	journal          EvidenceJournal
	key              int64
	candidateBinding *verifiedEvidenceRunBinding
	cursorValid      *atomic.Bool
	canonical        [32]byte
}

type runnerProjectedCurrentPreledgerRegistryRecord struct {
	prepared         *runnerProjectedCurrentPreledger
	binding          *runnerProjectedCurrentPreledgerBinding
	session          DatabaseSession
	transaction      MigrationTransaction
	evidence         EvidenceSession
	journal          EvidenceJournal
	key              int64
	candidateBinding *verifiedEvidenceRunBinding
	cursorValid      *atomic.Bool
	canonical        [32]byte
}

type runnerProjectedCurrentPreledgerSeed struct {
	session                 DatabaseSession
	transaction             MigrationTransaction
	evidence                EvidenceSession
	journal                 EvidenceJournal
	key                     int64
	candidateBinding        *verifiedEvidenceRunBinding
	generation              generationIdentity
	statementAfterCanonical [32]byte
	recoveryDigest          [32]byte
	dispatch                runnerPreparedDispatch
	database                runnerPreparedDatabaseIdentity
	maxAttempts             uint32
	policy                  ExecutionPolicy
	plan                    StatementPlan
	intent                  StatementIntent
	cursor                  JournalCursor
	intentRecordDigest      Digest
	checkpointDigest        Digest
	executedStatementDigest Digest
	state                   StatementIntermediateState
	authorityAfter          ProjectionResultEvidence
	catalogAfter            ProjectionResultEvidence
	catalogAfterProjection  CatalogStateProjection
	verifiedAuthority       VerifiedAuthorityContract
	verifiedCatalog         VerifiedCatalogContract
}

type runnerPreledgerProjectionFacts struct {
	snapshotDigest [32]byte
	authority      ProjectionResult[AuthorityProjection]
	catalog        ProjectionResult[CatalogProjection]
}

var runnerProjectedCurrentPreledgerRegistry sync.Map

func (runner *Runner) projectCurrentPreledger(ctx context.Context, after *runnerProjectedCurrentStatementAfter) (*runnerProjectedCurrentPreledger, error) {
	seed, err := consumeRunnerProjectedCurrentStatementAfter(after)
	if err != nil {
		return nil, closeRunnerProjectedCurrentStatementAfter(after, err)
	}
	failClosed := func(primary error) (*runnerProjectedCurrentPreledger, error) {
		return nil, closeRunnerCurrentTransactionResources(seed.session, seed.transaction, seed.key, primary)
	}
	if ctx == nil || runner == nil {
		return failClosed(fail(CodeTransactionBoundary, "runner-preledger", "pre-ledger projection context or runner is unavailable", nil))
	}
	if contextErr := ctx.Err(); contextErr != nil {
		return failClosed(mapRunnerDatabasePreflightError(contextErr, "runner-preledger", "pre-ledger projection was interrupted"))
	}
	facts, projectionErr := runner.projectRunnerCurrentPreledger(ctx, seed)
	if projectionErr != nil {
		return failClosed(projectionErr)
	}
	boundary, boundaryErr := seed.transaction.Boundary(ctx, seed.key)
	status, statusOK := migrationProjectionTxStatus(seed.transaction)
	if boundaryErr != nil || !statusOK || status != 'T' || boundary.TxStatus != 'T' || boundary.CurrentUser != MigrationOwnerRole || !boundary.LockHeld {
		return failClosed(mapRunnerDatabasePreflightError(boundaryErr, "runner-preledger-boundary", "pre-ledger projection escaped the exact role, status, or advisory lock boundary"))
	}
	if !runnerProjectedPreledgerEvidenceMatches(seed) {
		invalidateRunnerProjectedPreledgerCursor(seed)
		return failClosed(fail(CodeEvidenceJournalFailed, "runner-preledger-evidence", "durable statement evidence changed during pre-ledger projection", nil))
	}
	state, stateErr := buildRunnerPreledgerState(seed, facts, boundary)
	if stateErr != nil {
		return failClosed(stateErr)
	}
	prepared, sealErr := bindRunnerProjectedCurrentPreledger(seed, facts, boundary, state)
	if sealErr != nil {
		invalidateRunnerProjectedPreledgerCursor(seed)
		return failClosed(sealErr)
	}
	return prepared, nil
}

func consumeRunnerProjectedCurrentStatementAfter(after *runnerProjectedCurrentStatementAfter) (runnerProjectedCurrentPreledgerSeed, error) {
	if !validRunnerProjectedCurrentStatementAfter(after) || !after.finalStatement || after.catalogAfterProjection.Present == nil || !equalProjectionScopes(after.plan.ExpectedTransition.CatalogAfter.Scope, after.catalogAfterProjection.Present.Scope) {
		return runnerProjectedCurrentPreledgerSeed{}, fail(CodeTransactionBoundary, "runner-preledger-claim", "final statement-after authority is unavailable or changed", nil)
	}
	current := after.evidence.CurrentCandidate()
	if !validOwnedCurrentCandidate(current) || current.binding != after.candidateBinding {
		return runnerProjectedCurrentPreledgerSeed{}, fail(CodeEvidenceJournalFailed, "runner-preledger-claim", "current evidence candidate differs from the statement-after authority", nil)
	}
	bindings, err := runnerCurrentProjectionBindings(after.evidence, current)
	if err != nil || bindings.runnerProjectionDecisionDigest != after.generation.runnerProjectionDecisionDigest || bindings.schemaBundleDigest != after.generation.schemaBundleDigest {
		return runnerProjectedCurrentPreledgerSeed{}, fail(CodeUntrusted, "runner-preledger-claim", "verified projection bindings differ from the statement-after authority", nil)
	}
	catalog, ok := exactCatalogBindingForHead(bindings.executableCatalogs, after.plan.MigrationID)
	if !ok || !runnerStatementAfterCatalogMatchesPlan(catalog, after.plan, after.intent) || !equalProjectionScopes(catalog.verifiedCatalog.Scope(), after.plan.ExpectedTransition.CatalogAfter.Scope) {
		return runnerProjectedCurrentPreledgerSeed{}, fail(CodeUntrusted, "runner-preledger-claim", "final catalog authority differs from the exact statement plan", nil)
	}
	plan, err := cloneRunnerStatementIntentPlan(after.plan)
	if err != nil {
		return runnerProjectedCurrentPreledgerSeed{}, fail(CodeUntrusted, "runner-preledger-claim", "exact final statement plan is unavailable", nil)
	}
	registered, ok := runnerProjectedCurrentStatementAfterRegistry.LoadAndDelete(after)
	record, recordOK := registered.(runnerProjectedCurrentStatementAfterRegistryRecord)
	if !ok || !recordOK || record.prepared != after || record.binding != after.binding || record.key != after.key || record.candidateBinding != after.candidateBinding || record.cursorValid != after.cursor.valid || record.canonical != after.canonical || !sameRunnerOwnedPointer(record.session, after.session) || !sameRunnerOwnedPointer(record.transaction, after.transaction) || !sameRunnerOwnedPointer(record.evidence, after.evidence) || !sameRunnerOwnedPointer(record.journal, after.journal) {
		return runnerProjectedCurrentPreledgerSeed{}, fail(CodeTransactionBoundary, "runner-preledger-claim", "statement-after authority could not be consumed exactly once", nil)
	}
	seed := runnerProjectedCurrentPreledgerSeed{
		session: record.session, transaction: record.transaction, evidence: record.evidence, journal: record.journal,
		key: record.key, candidateBinding: record.candidateBinding, generation: after.generation,
		statementAfterCanonical: record.canonical, recoveryDigest: after.recoveryDigest,
		dispatch: after.dispatch, database: after.database, maxAttempts: after.maxAttempts,
		policy: cloneProjectionValue(after.policy), plan: plan, intent: cloneProjectionValue(after.intent), cursor: after.cursor.clone(),
		intentRecordDigest: after.intentRecordDigest, checkpointDigest: after.checkpointDigest,
		executedStatementDigest: after.executedStatementDigest, state: cloneProjectionValue(after.state),
		authorityAfter: cloneProjectionValue(after.authorityAfter), catalogAfter: cloneProjectionValue(after.catalogAfter),
		catalogAfterProjection: cloneProjectionValue(after.catalogAfterProjection),
		verifiedAuthority:      cloneVerifiedAuthorityContract(bindings.verifiedAuthority),
		verifiedCatalog:        cloneVerifiedCatalogContract(catalog.verifiedCatalog),
	}
	after.closed = true
	after.session = nil
	after.transaction = nil
	after.evidence = nil
	after.journal = nil
	after.binding = nil
	after.policy = ExecutionPolicy{}
	after.plan = StatementPlan{}
	after.intent = StatementIntent{}
	after.authorityAfterProjection = AuthorityProjection{}
	after.catalogAfterProjection = CatalogStateProjection{}
	after.authorityAfter = ProjectionResultEvidence{}
	after.catalogAfter = ProjectionResultEvidence{}
	after.state = StatementIntermediateState{}
	return seed, nil
}

func (runner *Runner) projectRunnerCurrentPreledger(ctx context.Context, seed runnerProjectedCurrentPreledgerSeed) (runnerPreledgerProjectionFacts, error) {
	profile, profileOK := seed.transaction.(runnerTransactionProjectionProfile)
	if !profileOK {
		return runnerPreledgerProjectionFacts{}, fail(CodeTransactionBoundary, "runner-preledger-profile", "migration transaction cannot enter the closed projection profile", nil)
	}
	if profileErr := profile.enterRunnerProjectionProfile(ctx); profileErr != nil {
		return runnerPreledgerProjectionFacts{}, mapRunnerDatabasePreflightError(profileErr, "runner-preledger-profile", "pre-ledger projection profile could not be configured")
	}
	snapshot, snapshotErr := BorrowMigrationProjectionSnapshot(ctx, seed.transaction, seed.plan.MigrationID, nil)
	if snapshotErr != nil {
		return runnerPreledgerProjectionFacts{}, mapRunnerDatabasePreflightError(snapshotErr, "runner-preledger-snapshot", "pre-ledger projection snapshot could not be borrowed")
	}
	if snapshot == nil {
		return runnerPreledgerProjectionFacts{}, fail(CodeProjectionSnapshotInvalid, "runner-preledger-snapshot", "pre-ledger projection snapshot is unavailable", nil)
	}
	metadata := snapshot.Metadata()
	if !sameRunnerPreparedDatabaseIdentity(seed.database, metadata) || metadata.MigrationID == nil || *metadata.MigrationID != seed.plan.MigrationID || metadata.StatementIndex != nil {
		return runnerPreledgerProjectionFacts{}, fail(CodeProjectionMetadataMismatch, "runner-preledger-snapshot", "pre-ledger snapshot differs from the executed database identity or final boundary", nil)
	}
	factory := runner.projectionFactory
	if factory == nil {
		factory = pgRunnerAuthorityProjectorFactory{}
	}
	projector, factoryErr := factory.newRunnerAuthorityProjector(ctx, snapshot)
	if factoryErr != nil {
		return runnerPreledgerProjectionFacts{}, mapRunnerDatabasePreflightError(factoryErr, "runner-preledger-projector", "pre-ledger projector could not be constructed")
	}
	if projector == nil {
		return runnerPreledgerProjectionFacts{}, fail(CodeProjectionSnapshotInvalid, "runner-preledger-projector", "pre-ledger projector is unavailable", nil)
	}
	authority, authorityErr := projector.ProjectAuthority(ctx, snapshot, seed.verifiedAuthority, AuthorityPhaseMigrationTransaction)
	if authorityErr != nil {
		return runnerPreledgerProjectionFacts{}, mapRunnerDatabasePreflightError(authorityErr, "runner-preledger-authority", "pre-ledger authority projection failed")
	}
	if err := validateRunnerAuthorityProjectionResult(authority, metadata, seed.verifiedAuthority, AuthorityPhaseMigrationTransaction); err != nil {
		return runnerPreledgerProjectionFacts{}, mapRunnerDatabasePreflightError(err, "runner-preledger-authority", "pre-ledger authority projection result is invalid")
	}
	scope := seed.verifiedCatalog.Scope()
	catalog, catalogErr := projector.ProjectCatalog(ctx, snapshot, seed.verifiedCatalog, scope)
	if catalogErr != nil {
		return runnerPreledgerProjectionFacts{}, mapRunnerDatabasePreflightError(catalogErr, "runner-preledger-catalog", "pre-ledger final catalog projection failed")
	}
	if err := validateRunnerPreledgerCatalogResult(catalog, metadata, seed.verifiedCatalog); err != nil {
		return runnerPreledgerProjectionFacts{}, err
	}
	if authority.Digest != seed.authorityAfter.Digest || !runnerCanonicalEqual(authority.Metadata.Snapshot, catalog.Metadata.Snapshot) || seed.catalogAfterProjection.Present == nil || !runnerCanonicalEqual(catalog.Projection.Body, seed.catalogAfterProjection.Present.Body) || catalog.Projection.SchemaHead != seed.plan.MigrationID {
		return runnerPreledgerProjectionFacts{}, fail(CodeCatalogDrift, "runner-preledger-equality", "pre-ledger authority or final catalog differs from immediate statement-after", nil)
	}
	if restoreErr := profile.restoreRunnerExecutionProfile(ctx, seed.policy); restoreErr != nil {
		return runnerPreledgerProjectionFacts{}, mapRunnerDatabasePreflightError(restoreErr, "runner-preledger-execution-profile", "verified migration execution profile could not be restored")
	}
	facts := runnerPreledgerProjectionFacts{snapshotDigest: runnerTransactionSnapshotDigest(metadata), authority: authority, catalog: catalog}
	if facts.snapshotDigest == ([32]byte{}) {
		return runnerPreledgerProjectionFacts{}, fail(CodeProjectionMetadataMismatch, "runner-preledger-snapshot", "pre-ledger snapshot identity could not be sealed", nil)
	}
	return facts, nil
}

func validateRunnerPreledgerCatalogResult(result ProjectionResult[CatalogProjection], snapshot SnapshotMetadata, contract VerifiedCatalogContract) error {
	if snapshot.validate() != nil || snapshot.AuthorityPhase != AuthorityPhaseMigrationTransaction || contract.validate() != nil {
		return fail(CodeProjectionMetadataMismatch, "runner-preledger-catalog", "pre-ledger catalog inputs are invalid", nil)
	}
	scope := contract.Scope()
	if result.Metadata.validate() != nil || !runnerCanonicalEqual(result.Metadata.Snapshot, snapshot) || result.Metadata.VerifiedSubjectDigest != contract.SubjectDigest() || result.Metadata.QueryCount == 0 || result.Metadata.RowCount == 0 || result.Metadata.TotalBytes == 0 || result.Metadata.Scope == nil || !equalProjectionScopes(*result.Metadata.Scope, scope) {
		return fail(CodeProjectionMetadataMismatch, "runner-preledger-catalog", "pre-ledger catalog metadata is incomplete or mismatched", nil)
	}
	expected := contract.ExpectedProjection()
	digest, err := digestProjectionWrapper(CatalogProjectionDigestDomain, result.Projection)
	if err != nil || result.Projection.Validate() != nil || digest != result.Digest || !runnerCanonicalEqual(result.Projection, expected) {
		return fail(CodeCatalogDrift, "runner-preledger-catalog", "pre-ledger catalog differs from the verified final projection", nil)
	}
	return nil
}

func buildRunnerPreledgerState(seed runnerProjectedCurrentPreledgerSeed, facts runnerPreledgerProjectionFacts, boundary BoundaryState) (StatementIntermediateState, error) {
	scope := cloneProjectionValue(seed.plan.ExpectedTransition.CatalogAfter.Scope)
	projection := CatalogStateProjection{Present: &SchemaPresentProjection{State: "schema_present", Scope: scope, Body: cloneProjectionValue(facts.catalog.Projection.Body)}}
	digest, err := projection.ComputeDigest()
	if err != nil || digest != seed.catalogAfter.Digest {
		return StatementIntermediateState{}, fail(CodeIntermediateStateMismatch, "runner-preledger-state", "pre-ledger final catalog cannot reproduce the immediate statement-after state", nil)
	}
	afterSeed := runnerProjectedCurrentStatementAfterSeed{
		key: seed.key, generation: seed.generation, plan: seed.plan, intent: seed.intent,
		runnerProjectionDecision: seed.generation.runnerProjectionDecisionDigest,
	}
	state, err := buildRunnerStatementAfterState(afterSeed, runnerStatementAfterProjectionFacts{
		authority: facts.authority,
		catalog:   ProjectionResult[CatalogStateProjection]{Projection: projection, Digest: digest, Metadata: cloneProjectionValue(facts.catalog.Metadata)},
	}, boundary)
	if err != nil || !runnerCanonicalEqual(state, seed.state) {
		return StatementIntermediateState{}, fail(CodeIntermediateStateMismatch, "runner-preledger-state", "pre-ledger control-plane state differs from immediate statement-after", nil)
	}
	return state, nil
}

func runnerProjectedPreledgerEvidenceMatches(seed runnerProjectedCurrentPreledgerSeed) bool {
	snapshot := seed.evidence.RecoverySnapshot()
	digest, err := runnerDurableStatementIntentEvidenceDigest(
		seed.evidence, seed.journal, seed.candidateBinding, seed.generation, seed.maxAttempts,
		seed.cursor, seed.intentRecordDigest, &seed.intent, snapshot,
	)
	return err == nil && digest == seed.recoveryDigest
}

func invalidateRunnerProjectedPreledgerCursor(seed runnerProjectedCurrentPreledgerSeed) {
	if seed.cursor.valid != nil {
		seed.cursor.valid.Store(false)
	}
}

func bindRunnerProjectedCurrentPreledger(seed runnerProjectedCurrentPreledgerSeed, facts runnerPreledgerProjectionFacts, boundary BoundaryState, state StatementIntermediateState) (*runnerProjectedCurrentPreledger, error) {
	plan, err := cloneRunnerStatementIntentPlan(seed.plan)
	if err != nil || state.Validate() != nil || !runnerCanonicalEqual(state, seed.state) || !runnerProjectedPreledgerEvidenceMatches(seed) {
		return nil, fail(CodeTransactionBoundary, "runner-preledger-seal", "pre-ledger inputs are unavailable or changed", nil)
	}
	prepared := &runnerProjectedCurrentPreledger{
		session: seed.session, transaction: seed.transaction, evidence: seed.evidence, journal: seed.journal,
		key: seed.key, candidateBinding: seed.candidateBinding, generation: seed.generation,
		statementAfterCanonical: seed.statementAfterCanonical, recoveryDigest: seed.recoveryDigest,
		dispatch: seed.dispatch, database: seed.database, maxAttempts: seed.maxAttempts,
		policy: cloneProjectionValue(seed.policy), plan: plan, intent: cloneProjectionValue(seed.intent), cursor: seed.cursor.clone(),
		intentRecordDigest: seed.intentRecordDigest, checkpointDigest: seed.checkpointDigest,
		executedStatementDigest: seed.executedStatementDigest, state: cloneProjectionValue(state),
		authorityAfter: cloneProjectionValue(seed.authorityAfter), catalogAfter: cloneProjectionValue(seed.catalogAfter),
		preledgerSnapshotDigest: facts.snapshotDigest,
		preledgerAuthority:      ProjectionResultEvidence{Digest: facts.authority.Digest, Metadata: cloneProjectionValue(facts.authority.Metadata)},
		preledgerCatalog:        ProjectionResultEvidence{Digest: facts.catalog.Digest, Metadata: cloneProjectionValue(facts.catalog.Metadata)},
		preledgerCatalogBody:    cloneProjectionValue(facts.catalog.Projection), boundary: boundary,
	}
	prepared.self = prepared
	binding := &runnerProjectedCurrentPreledgerBinding{
		prepared: prepared, session: seed.session, transaction: seed.transaction, evidence: seed.evidence, journal: seed.journal,
		key: seed.key, candidateBinding: seed.candidateBinding, cursorValid: seed.cursor.valid,
	}
	prepared.binding = binding
	prepared.canonical = runnerProjectedCurrentPreledgerDigest(prepared)
	binding.canonical = prepared.canonical
	if prepared.canonical == ([32]byte{}) {
		return nil, fail(CodeTransactionBoundary, "runner-preledger-seal", "pre-ledger authority could not be identified", nil)
	}
	runnerProjectedCurrentPreledgerRegistry.Store(prepared, runnerProjectedCurrentPreledgerRegistryRecord{
		prepared: prepared, binding: binding, session: seed.session, transaction: seed.transaction,
		evidence: seed.evidence, journal: seed.journal, key: seed.key, candidateBinding: seed.candidateBinding,
		cursorValid: seed.cursor.valid, canonical: prepared.canonical,
	})
	if !validRunnerProjectedCurrentPreledger(prepared) {
		runnerProjectedCurrentPreledgerRegistry.Delete(prepared)
		return nil, fail(CodeTransactionBoundary, "runner-preledger-seal", "pre-ledger authority could not be sealed", nil)
	}
	return prepared, nil
}

func validRunnerProjectedCurrentPreledger(prepared *runnerProjectedCurrentPreledger) bool {
	if prepared == nil || prepared.self != prepared || prepared.closed || prepared.binding == nil || prepared.binding.prepared != prepared || prepared.binding.key != prepared.key || prepared.binding.candidateBinding != prepared.candidateBinding || prepared.binding.cursorValid != prepared.cursor.valid || !sameRunnerOwnedPointer(prepared.binding.session, prepared.session) || !sameRunnerOwnedPointer(prepared.binding.transaction, prepared.transaction) || !sameRunnerOwnedPointer(prepared.binding.evidence, prepared.evidence) || !sameRunnerOwnedPointer(prepared.binding.journal, prepared.journal) || prepared.canonical == ([32]byte{}) || prepared.canonical != prepared.binding.canonical || prepared.canonical != runnerProjectedCurrentPreledgerDigest(prepared) {
		return false
	}
	registered, ok := runnerProjectedCurrentPreledgerRegistry.Load(prepared)
	record, recordOK := registered.(runnerProjectedCurrentPreledgerRegistryRecord)
	if !ok || !recordOK || record.prepared != prepared || record.binding != prepared.binding || record.key != prepared.key || record.candidateBinding != prepared.candidateBinding || record.cursorValid != prepared.cursor.valid || record.canonical != prepared.canonical || !sameRunnerOwnedPointer(record.session, prepared.session) || !sameRunnerOwnedPointer(record.transaction, prepared.transaction) || !sameRunnerOwnedPointer(record.evidence, prepared.evidence) || !sameRunnerOwnedPointer(record.journal, prepared.journal) {
		return false
	}
	status, statusOK := migrationProjectionTxStatus(prepared.transaction)
	if !statusOK || status != 'T' || prepared.boundary.TxStatus != 'T' || prepared.boundary.CurrentUser != MigrationOwnerRole || !prepared.boundary.LockHeld {
		return false
	}
	seed := runnerProjectedCurrentPreledgerSeed{
		session: prepared.session, transaction: prepared.transaction, evidence: prepared.evidence, journal: prepared.journal,
		key: prepared.key, candidateBinding: prepared.candidateBinding, generation: prepared.generation,
		statementAfterCanonical: prepared.statementAfterCanonical, recoveryDigest: prepared.recoveryDigest,
		dispatch: prepared.dispatch, database: prepared.database, maxAttempts: prepared.maxAttempts,
		policy: prepared.policy, plan: prepared.plan, intent: prepared.intent, cursor: prepared.cursor,
		intentRecordDigest: prepared.intentRecordDigest, checkpointDigest: prepared.checkpointDigest,
		executedStatementDigest: prepared.executedStatementDigest,
	}
	return runnerProjectedPreledgerEvidenceMatches(seed)
}

func runnerProjectedCurrentPreledgerDigest(prepared *runnerProjectedCurrentPreledger) [32]byte {
	if prepared == nil || prepared.self != prepared || prepared.closed || prepared.session == nil || prepared.transaction == nil || prepared.evidence == nil || prepared.journal == nil || prepared.candidateBinding == nil || prepared.candidateBinding.owner == nil || prepared.generation.owner != prepared.candidateBinding.owner || prepared.candidateBinding.canonical == ([32]byte{}) || prepared.statementAfterCanonical == ([32]byte{}) || prepared.recoveryDigest == ([32]byte{}) || prepared.preledgerSnapshotDigest == ([32]byte{}) || prepared.policy.Validate() != nil || prepared.policy.MaxAttempts != uint64(prepared.maxAttempts) || prepared.plan.validateExact() != nil || prepared.intent.Validate() != nil || prepared.state.Validate() != nil || prepared.authorityAfter.Validate() != nil || prepared.catalogAfter.Validate() != nil || prepared.preledgerAuthority.Validate() != nil || prepared.preledgerCatalog.Validate() != nil || prepared.preledgerCatalogBody.Validate() != nil || prepared.preledgerAuthority.Digest != prepared.authorityAfter.Digest || prepared.authorityAfter.Digest != prepared.state.AuthorityAfterDigest || prepared.catalogAfter.Digest != prepared.state.CatalogAfterDigest || prepared.dispatch.recoveryState != RecoveryBrandNew && prepared.dispatch.recoveryState != RecoveryBrandNewInherited || prepared.dispatch.action != RecoveryBeginFirstAttempt || prepared.dispatch.migrationID != prepared.intent.MigrationID || prepared.dispatch.attemptIndex != prepared.intent.AttemptIndex || prepared.dispatch.entryIndex != 0 || prepared.dispatch.planCount == 0 || prepared.plan.StatementIndex+1 != prepared.dispatch.planCount || prepared.dispatch.planDigest == ([32]byte{}) || !runnerPreledgerProjectionBindingMatches(prepared) || !prepared.cursor.Valid() || !sameGenerationIdentity(prepared.cursor.generation, prepared.generation) || prepared.cursor.previousRecordDigest == nil || *prepared.cursor.previousRecordDigest != prepared.intentRecordDigest || prepared.cursor.latestCheckpointRecordDigest == nil || *prepared.cursor.latestCheckpointRecordDigest != prepared.checkpointDigest || prepared.executedStatementDigest != prepared.plan.StatementSHA256 || prepared.boundary.TxStatus != 'T' || prepared.boundary.CurrentUser != MigrationOwnerRole || !prepared.boundary.LockHeld {
		return [32]byte{}
	}
	values := []any{prepared.policy, prepared.plan.exactSentinel(), prepared.intent, prepared.state, prepared.authorityAfter, prepared.catalogAfter, prepared.preledgerAuthority, prepared.preledgerCatalog, prepared.preledgerCatalogBody, prepared.boundary}
	h := sha256.New()
	h.Write([]byte("cloud-agents-platform-runner-projected-current-preledger/v1\x00"))
	h.Write(prepared.candidateBinding.canonical[:])
	h.Write(prepared.statementAfterCanonical[:])
	h.Write(prepared.recoveryDigest[:])
	h.Write(prepared.preledgerSnapshotDigest[:])
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
	h.Write(prepared.dispatch.planDigest[:])
	writeAdmissionUint(h, uint64(prepared.maxAttempts))
	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result
}

func runnerPreledgerProjectionBindingMatches(prepared *runnerProjectedCurrentPreledger) bool {
	if prepared == nil || prepared.preledgerCatalogBody.SchemaHead != prepared.plan.MigrationID || !sameRunnerPreparedDatabaseIdentity(prepared.database, prepared.preledgerAuthority.Metadata.Snapshot) || !runnerCanonicalEqual(prepared.preledgerAuthority.Metadata.Snapshot, prepared.preledgerCatalog.Metadata.Snapshot) || prepared.preledgerAuthority.Metadata.Snapshot.MigrationID == nil || *prepared.preledgerAuthority.Metadata.Snapshot.MigrationID != prepared.plan.MigrationID || prepared.preledgerAuthority.Metadata.Snapshot.StatementIndex != nil || prepared.preledgerSnapshotDigest != runnerTransactionSnapshotDigest(prepared.preledgerAuthority.Metadata.Snapshot) {
		return false
	}
	digest, err := digestProjectionWrapper(CatalogProjectionDigestDomain, prepared.preledgerCatalogBody)
	if err != nil || digest != prepared.preledgerCatalog.Digest || prepared.preledgerCatalog.Metadata.Scope == nil {
		return false
	}
	head := prepared.preledgerCatalogBody.SchemaHead
	scope := ProjectionScope{ScopeKind: "final", SchemaHead: &head, DeclaredObjects: cloneProjectionValue(prepared.preledgerCatalogBody.Body.DeclaredObjects)}
	return equalProjectionScopes(*prepared.preledgerCatalog.Metadata.Scope, scope)
}

func closeRunnerProjectedCurrentPreledger(prepared *runnerProjectedCurrentPreledger, primary error) error {
	if prepared == nil {
		return primary
	}
	if prepared.self != prepared {
		return fail(CodeTransactionBoundary, "runner-preledger-close", "pre-ledger copy cannot close database authority", nil)
	}
	registered, ok := runnerProjectedCurrentPreledgerRegistry.Load(prepared)
	record, recordOK := registered.(runnerProjectedCurrentPreledgerRegistryRecord)
	if !ok || !recordOK || record.prepared != prepared || record.binding == nil {
		return fail(CodeTransactionBoundary, "runner-preledger-close", "pre-ledger authority is unavailable", nil)
	}
	valid := validRunnerProjectedCurrentPreledger(prepared)
	runnerProjectedCurrentPreledgerRegistry.Delete(prepared)
	prepared.closed = true
	prepared.session = nil
	prepared.transaction = nil
	prepared.evidence = nil
	prepared.journal = nil
	prepared.binding = nil
	prepared.policy = ExecutionPolicy{}
	prepared.plan = StatementPlan{}
	prepared.intent = StatementIntent{}
	prepared.state = StatementIntermediateState{}
	prepared.authorityAfter = ProjectionResultEvidence{}
	prepared.catalogAfter = ProjectionResultEvidence{}
	prepared.preledgerAuthority = ProjectionResultEvidence{}
	prepared.preledgerCatalog = ProjectionResultEvidence{}
	prepared.preledgerCatalogBody = CatalogProjection{}
	if !valid {
		primary = fail(CodeTransactionBoundary, "runner-preledger-close", "pre-ledger authority changed before close", nil)
	}
	return closeRunnerCurrentTransactionResources(record.session, record.transaction, record.key, primary)
}
