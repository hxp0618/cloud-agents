package migration

import (
	"context"
	"crypto/sha256"
	"strconv"
	"sync"
	"sync/atomic"
)

// runnerProjectedCurrentStatementAfter proves the immediate statement-after
// authority and catalog state on the same transaction that executed the exact
// SQL. It remains non-runnable: a final statement must still pass pre-ledger
// equality, while a non-final statement must durably append its intermediate
// record before another SQL statement may be considered.
type runnerProjectedCurrentStatementAfter struct {
	self                     *runnerProjectedCurrentStatementAfter
	binding                  *runnerProjectedCurrentStatementAfterBinding
	session                  DatabaseSession
	transaction              MigrationTransaction
	evidence                 EvidenceSession
	journal                  EvidenceJournal
	key                      int64
	candidateBinding         *verifiedEvidenceRunBinding
	generation               generationIdentity
	executedCanonical        [32]byte
	recoveryDigest           [32]byte
	dispatch                 runnerPreparedDispatch
	database                 runnerPreparedDatabaseIdentity
	maxAttempts              uint32
	policy                   ExecutionPolicy
	plan                     StatementPlan
	intent                   StatementIntent
	cursor                   JournalCursor
	intentRecordDigest       Digest
	checkpointDigest         Digest
	executedStatementDigest  Digest
	snapshotDigest           [32]byte
	authorityAfterProjection AuthorityProjection
	catalogAfterProjection   CatalogStateProjection
	authorityAfter           ProjectionResultEvidence
	catalogAfter             ProjectionResultEvidence
	boundary                 BoundaryState
	state                    StatementIntermediateState
	finalStatement           bool
	canonical                [32]byte
	closed                   bool
}

type runnerProjectedCurrentStatementAfterBinding struct {
	prepared         *runnerProjectedCurrentStatementAfter
	session          DatabaseSession
	transaction      MigrationTransaction
	evidence         EvidenceSession
	journal          EvidenceJournal
	key              int64
	candidateBinding *verifiedEvidenceRunBinding
	cursorValid      *atomic.Bool
	canonical        [32]byte
}

type runnerProjectedCurrentStatementAfterRegistryRecord struct {
	prepared         *runnerProjectedCurrentStatementAfter
	binding          *runnerProjectedCurrentStatementAfterBinding
	session          DatabaseSession
	transaction      MigrationTransaction
	evidence         EvidenceSession
	journal          EvidenceJournal
	key              int64
	candidateBinding *verifiedEvidenceRunBinding
	cursorValid      *atomic.Bool
	canonical        [32]byte
}

type runnerProjectedCurrentStatementAfterSeed struct {
	session                  DatabaseSession
	transaction              MigrationTransaction
	evidence                 EvidenceSession
	journal                  EvidenceJournal
	key                      int64
	candidateBinding         *verifiedEvidenceRunBinding
	generation               generationIdentity
	executedCanonical        [32]byte
	recoveryDigest           [32]byte
	dispatch                 runnerPreparedDispatch
	database                 runnerPreparedDatabaseIdentity
	maxAttempts              uint32
	policy                   ExecutionPolicy
	plan                     StatementPlan
	intent                   StatementIntent
	cursor                   JournalCursor
	intentRecordDigest       Digest
	checkpointDigest         Digest
	executedStatementDigest  Digest
	verifiedAuthority        VerifiedAuthorityContract
	verifiedCatalog          VerifiedCatalogContract
	runnerProjectionDecision Digest
}

type runnerStatementAfterProjectionFacts struct {
	snapshotDigest [32]byte
	authority      ProjectionResult[AuthorityProjection]
	catalog        ProjectionResult[CatalogStateProjection]
}

var runnerProjectedCurrentStatementAfterRegistry sync.Map

func (runner *Runner) projectCurrentStatementAfter(ctx context.Context, executed *runnerExecutedCurrentStatement) (*runnerProjectedCurrentStatementAfter, error) {
	seed, err := consumeRunnerExecutedCurrentStatement(executed)
	if err != nil {
		return nil, closeRunnerExecutedCurrentStatement(executed, err)
	}
	failClosed := func(primary error) (*runnerProjectedCurrentStatementAfter, error) {
		return nil, closeRunnerCurrentTransactionResources(seed.session, seed.transaction, seed.key, primary)
	}
	if ctx == nil || runner == nil {
		return failClosed(fail(CodeTransactionBoundary, "runner-statement-after", "statement-after projection context or runner is unavailable", nil))
	}
	if contextErr := ctx.Err(); contextErr != nil {
		return failClosed(mapRunnerDatabasePreflightError(contextErr, "runner-statement-after", "statement-after projection was interrupted"))
	}
	facts, projectionErr := runner.projectRunnerStatementAfter(ctx, seed)
	if projectionErr != nil {
		return failClosed(projectionErr)
	}
	boundary, boundaryErr := seed.transaction.Boundary(ctx, seed.key)
	status, statusOK := migrationProjectionTxStatus(seed.transaction)
	if boundaryErr != nil || !statusOK || status != 'T' || boundary.TxStatus != 'T' || boundary.CurrentUser != MigrationOwnerRole || !boundary.LockHeld {
		return failClosed(mapRunnerDatabasePreflightError(boundaryErr, "runner-statement-after-boundary", "statement-after projection escaped the exact role, status, or advisory lock boundary"))
	}
	if !runnerProjectedStatementAfterEvidenceMatches(seed) {
		invalidateRunnerProjectedStatementAfterCursor(seed)
		return failClosed(fail(CodeEvidenceJournalFailed, "runner-statement-after-evidence", "durable statement evidence changed during after projection", nil))
	}
	state, stateErr := buildRunnerStatementAfterState(seed, facts, boundary)
	if stateErr != nil {
		return failClosed(stateErr)
	}
	projected, sealErr := bindRunnerProjectedCurrentStatementAfter(seed, facts, boundary, state)
	if sealErr != nil {
		invalidateRunnerProjectedStatementAfterCursor(seed)
		return failClosed(sealErr)
	}
	return projected, nil
}

func consumeRunnerExecutedCurrentStatement(executed *runnerExecutedCurrentStatement) (runnerProjectedCurrentStatementAfterSeed, error) {
	if !validRunnerExecutedCurrentStatement(executed) {
		return runnerProjectedCurrentStatementAfterSeed{}, fail(CodeTransactionBoundary, "runner-statement-after-claim", "executed statement authority is unavailable or changed", nil)
	}
	current := executed.evidence.CurrentCandidate()
	if !validOwnedCurrentCandidate(current) || current.binding != executed.candidateBinding {
		return runnerProjectedCurrentStatementAfterSeed{}, fail(CodeEvidenceJournalFailed, "runner-statement-after-claim", "current evidence candidate differs from the executed statement", nil)
	}
	bindings, err := runnerCurrentProjectionBindings(executed.evidence, current)
	if err != nil || bindings.runnerProjectionDecisionDigest != executed.generation.runnerProjectionDecisionDigest || bindings.schemaBundleDigest != executed.generation.schemaBundleDigest {
		return runnerProjectedCurrentStatementAfterSeed{}, fail(CodeUntrusted, "runner-statement-after-claim", "verified projection bindings differ from the executed statement", nil)
	}
	catalog, ok := exactCatalogBindingForHead(bindings.executableCatalogs, executed.plan.MigrationID)
	if !ok || !runnerStatementAfterCatalogMatchesPlan(catalog, executed.plan, executed.intent) {
		return runnerProjectedCurrentStatementAfterSeed{}, fail(CodeUntrusted, "runner-statement-after-claim", "statement catalog authority differs from the exact plan", nil)
	}
	plan, err := cloneRunnerStatementIntentPlan(executed.plan)
	if err != nil {
		return runnerProjectedCurrentStatementAfterSeed{}, fail(CodeUntrusted, "runner-statement-after-claim", "exact statement plan is unavailable", nil)
	}
	registered, ok := runnerExecutedCurrentStatementRegistry.LoadAndDelete(executed)
	record, recordOK := registered.(runnerExecutedCurrentStatementRegistryRecord)
	if !ok || !recordOK || record.prepared != executed || record.binding != executed.binding || record.key != executed.key || record.candidateBinding != executed.candidateBinding || record.cursorValid != executed.cursor.valid || record.canonical != executed.canonical || !sameRunnerOwnedPointer(record.session, executed.session) || !sameRunnerOwnedPointer(record.transaction, executed.transaction) || !sameRunnerOwnedPointer(record.evidence, executed.evidence) || !sameRunnerOwnedPointer(record.journal, executed.journal) {
		return runnerProjectedCurrentStatementAfterSeed{}, fail(CodeTransactionBoundary, "runner-statement-after-claim", "executed statement could not be consumed exactly once", nil)
	}
	seed := runnerProjectedCurrentStatementAfterSeed{
		session: record.session, transaction: record.transaction, evidence: record.evidence, journal: record.journal,
		key: record.key, candidateBinding: record.candidateBinding, generation: executed.generation,
		executedCanonical: record.canonical, recoveryDigest: executed.recoveryDigest,
		dispatch: executed.dispatch, database: executed.database, maxAttempts: executed.maxAttempts,
		policy: cloneProjectionValue(executed.policy), plan: plan, intent: cloneProjectionValue(executed.intent), cursor: executed.cursor.clone(),
		intentRecordDigest: executed.intentRecordDigest, checkpointDigest: executed.checkpointDigest,
		executedStatementDigest:  executed.executedStatementDigest,
		verifiedAuthority:        cloneVerifiedAuthorityContract(bindings.verifiedAuthority),
		verifiedCatalog:          cloneVerifiedCatalogContract(catalog.verifiedCatalog),
		runnerProjectionDecision: bindings.runnerProjectionDecisionDigest,
	}
	executed.closed = true
	executed.session = nil
	executed.transaction = nil
	executed.evidence = nil
	executed.journal = nil
	executed.binding = nil
	executed.policy = ExecutionPolicy{}
	executed.plan = StatementPlan{}
	executed.intent = StatementIntent{}
	return seed, nil
}

func runnerStatementAfterCatalogMatchesPlan(catalog ExecutableCatalogBinding, plan StatementPlan, intent StatementIntent) bool {
	if plan.validateExact() != nil || intent.Validate() != nil || catalog.schemaHead != plan.MigrationID || catalog.catalogContractDigest != intent.CatalogContractDigest || catalog.verifiedCatalog.SubjectDigest() != intent.CatalogContractDigest || catalog.verifiedCatalog.validate() != nil {
		return false
	}
	source, err := exactMigrationSource(catalog.catalogContract.SourceDescriptors, plan.MigrationID)
	if err != nil || source.SQLSHA256 != plan.SQLArtifactSHA256 || int(plan.StatementIndex) >= len(source.Statements) {
		return false
	}
	descriptor := source.Statements[plan.StatementIndex]
	return descriptor.Index == uint64(plan.StatementIndex) && descriptor.Start == plan.StartOffset && descriptor.End == plan.EndOffset && descriptor.SHA256 == plan.StatementSHA256 && runnerCanonicalEqual(descriptor.Classification, plan.Classification) && runnerCanonicalEqual(descriptor.ExpectedTransition, plan.ExpectedTransition)
}

func (runner *Runner) projectRunnerStatementAfter(ctx context.Context, seed runnerProjectedCurrentStatementAfterSeed) (runnerStatementAfterProjectionFacts, error) {
	profile, profileOK := seed.transaction.(runnerTransactionProjectionProfile)
	if !profileOK {
		return runnerStatementAfterProjectionFacts{}, fail(CodeTransactionBoundary, "runner-statement-after-profile", "migration transaction cannot enter the closed projection profile", nil)
	}
	if profileErr := profile.enterRunnerProjectionProfile(ctx); profileErr != nil {
		return runnerStatementAfterProjectionFacts{}, mapRunnerDatabasePreflightError(profileErr, "runner-statement-after-profile", "statement-after projection profile could not be configured")
	}
	statementIndex := seed.plan.StatementIndex
	snapshot, snapshotErr := BorrowMigrationProjectionSnapshot(ctx, seed.transaction, seed.plan.MigrationID, &statementIndex)
	if snapshotErr != nil {
		return runnerStatementAfterProjectionFacts{}, mapRunnerDatabasePreflightError(snapshotErr, "runner-statement-after-snapshot", "statement-after projection snapshot could not be borrowed")
	}
	if snapshot == nil {
		return runnerStatementAfterProjectionFacts{}, fail(CodeProjectionSnapshotInvalid, "runner-statement-after-snapshot", "statement-after projection snapshot is unavailable", nil)
	}
	metadata := snapshot.Metadata()
	if !sameRunnerPreparedDatabaseIdentity(seed.database, metadata) || metadata.MigrationID == nil || *metadata.MigrationID != seed.plan.MigrationID || !sameRunnerStatementIndex(metadata.StatementIndex, &statementIndex) {
		return runnerStatementAfterProjectionFacts{}, fail(CodeProjectionMetadataMismatch, "runner-statement-after-snapshot", "statement-after snapshot differs from the executed database identity or statement", nil)
	}
	factory := runner.projectionFactory
	if factory == nil {
		factory = pgRunnerAuthorityProjectorFactory{}
	}
	projector, factoryErr := factory.newRunnerAuthorityProjector(ctx, snapshot)
	if factoryErr != nil {
		return runnerStatementAfterProjectionFacts{}, mapRunnerDatabasePreflightError(factoryErr, "runner-statement-after-projector", "statement-after projector could not be constructed")
	}
	if projector == nil {
		return runnerStatementAfterProjectionFacts{}, fail(CodeProjectionSnapshotInvalid, "runner-statement-after-projector", "statement-after projector is unavailable", nil)
	}
	authority, authorityErr := projector.ProjectAuthority(ctx, snapshot, seed.verifiedAuthority, AuthorityPhaseMigrationTransaction)
	if authorityErr != nil {
		return runnerStatementAfterProjectionFacts{}, mapRunnerDatabasePreflightError(authorityErr, "runner-statement-after-authority", "statement-after authority projection failed")
	}
	if err := validateRunnerAuthorityProjectionResult(authority, metadata, seed.verifiedAuthority, AuthorityPhaseMigrationTransaction); err != nil {
		return runnerStatementAfterProjectionFacts{}, mapRunnerDatabasePreflightError(err, "runner-statement-after-authority", "statement-after authority projection result is invalid")
	}
	scope := cloneProjectionValue(seed.plan.ExpectedTransition.CatalogAfter.Scope)
	catalog, catalogErr := projector.ProjectTransitionState(ctx, snapshot, seed.verifiedCatalog, scope)
	if catalogErr != nil {
		return runnerStatementAfterProjectionFacts{}, mapRunnerDatabasePreflightError(catalogErr, "runner-statement-after-catalog", "statement-after catalog projection failed")
	}
	if err := validateRunnerStatementAfterCatalogResult(catalog, metadata, seed.verifiedCatalog, seed.plan.ExpectedTransition.CatalogAfter); err != nil {
		return runnerStatementAfterProjectionFacts{}, err
	}
	if authority.Digest != seed.intent.AuthorityBeforeDigest || seed.plan.ExpectedTransition.AuthorityRelation != "unchanged_relative_to_verified_binding" || !runnerCanonicalEqual(authority.Metadata.Snapshot, catalog.Metadata.Snapshot) {
		return runnerStatementAfterProjectionFacts{}, fail(CodeAuthorityDrift, "runner-statement-after-authority", "statement-after authority differs from the durable before binding", nil)
	}
	if restoreErr := profile.restoreRunnerExecutionProfile(ctx, seed.policy); restoreErr != nil {
		return runnerStatementAfterProjectionFacts{}, mapRunnerDatabasePreflightError(restoreErr, "runner-statement-after-execution-profile", "verified migration execution profile could not be restored")
	}
	facts := runnerStatementAfterProjectionFacts{snapshotDigest: runnerTransactionSnapshotDigest(metadata), authority: authority, catalog: catalog}
	if facts.snapshotDigest == ([32]byte{}) {
		return runnerStatementAfterProjectionFacts{}, fail(CodeProjectionMetadataMismatch, "runner-statement-after-snapshot", "statement-after snapshot identity could not be sealed", nil)
	}
	return facts, nil
}

func validateRunnerStatementAfterCatalogResult(result ProjectionResult[CatalogStateProjection], snapshot SnapshotMetadata, contract VerifiedCatalogContract, expected CatalogStateDigestRef) error {
	if snapshot.validate() != nil || snapshot.AuthorityPhase != AuthorityPhaseMigrationTransaction || contract.validate() != nil || expected.Validate() != nil {
		return fail(CodeProjectionMetadataMismatch, "runner-statement-after-catalog", "statement-after catalog inputs are invalid", nil)
	}
	if result.Metadata.validate() != nil || !runnerCanonicalEqual(result.Metadata.Snapshot, snapshot) || result.Metadata.VerifiedSubjectDigest != contract.SubjectDigest() || result.Metadata.QueryCount == 0 || result.Metadata.RowCount == 0 || result.Metadata.TotalBytes == 0 || result.Metadata.Scope == nil || !equalProjectionScopes(*result.Metadata.Scope, expected.Scope) {
		return fail(CodeProjectionMetadataMismatch, "runner-statement-after-catalog", "statement-after catalog metadata is incomplete or mismatched", nil)
	}
	if result.Projection.Validate() != nil || !equalProjectionScopes(verifiedCatalogStateScope(result.Projection), expected.Scope) {
		return fail(CodeCatalogDrift, "runner-statement-after-catalog", "statement-after catalog projection is invalid or has the wrong scope", nil)
	}
	kind := "schema_absent"
	if result.Projection.Present != nil {
		kind = "schema_present"
	}
	digest, err := result.Projection.ComputeDigest()
	if err != nil || digest != result.Digest || digest != expected.Digest || kind != expected.StateKind {
		return fail(CodeCatalogDrift, "runner-statement-after-catalog", "statement-after catalog differs from the signed transition", nil)
	}
	return nil
}

func buildRunnerStatementAfterState(seed runnerProjectedCurrentStatementAfterSeed, facts runnerStatementAfterProjectionFacts, boundary BoundaryState) (StatementIntermediateState, error) {
	if facts.authority.Projection.Validate() != nil || facts.catalog.Projection.Validate() != nil || facts.catalog.Projection.Present == nil || boundary.TxStatus != 'T' || boundary.CurrentUser != MigrationOwnerRole || !boundary.LockHeld || seed.runnerProjectionDecision != seed.generation.runnerProjectionDecisionDigest {
		return StatementIntermediateState{}, fail(CodeIntermediateStateMismatch, "runner-statement-after-state", "statement-after control-plane facts are incomplete", nil)
	}
	schema := facts.catalog.Projection.Present.Body.Schema
	explicit, err := computeSchemaExplicitACLDigest(schema)
	if err != nil {
		return StatementIntermediateState{}, fail(CodeIntermediateStateMismatch, "runner-statement-after-state", "schema explicit ACL digest could not be computed", nil)
	}
	effective, err := computeSchemaEffectiveACLDigest(schema)
	if err != nil {
		return StatementIntermediateState{}, fail(CodeIntermediateStateMismatch, "runner-statement-after-state", "schema effective ACL digest could not be computed", nil)
	}
	defaultACL, err := computeDefaultACLDigest(facts.catalog.Projection.Present.Body.DefaultACL)
	if err != nil {
		return StatementIntermediateState{}, fail(CodeIntermediateStateMismatch, "runner-statement-after-state", "default ACL digest could not be computed", nil)
	}
	states := ControlPlaneStates{
		TxStatus: "T", SessionUser: facts.authority.Metadata.Snapshot.SessionUser,
		CurrentUser: boundary.CurrentUser, MigrationRole: MigrationOwnerRole,
		AdvisoryLock:                    AdvisoryLockProjection{Domain: AdvisoryLockDomain, KeyInt64Decimal: strconv.FormatInt(seed.key, 10), Held: true},
		VerifiedAuthorityDecisionDigest: seed.runnerProjectionDecision, SchemaOwner: schema.Owner,
		SchemaExplicitACLDigest: explicit, SchemaEffectiveACLDigest: effective, DefaultACLDigest: defaultACL,
		ExpectedTransitionDigest: seed.plan.ExpectedTransitionDigest,
	}
	state := StatementIntermediateState{
		SchemaBundleDigest: seed.intent.SchemaBundleDigest, CatalogContractDigest: seed.intent.CatalogContractDigest,
		AuthorityProfileDigest: seed.intent.AuthorityProfileDigest, AuthorityBindingDigest: seed.intent.AuthorityBindingDigest,
		MigrationID: seed.intent.MigrationID, AttemptIndex: seed.intent.AttemptIndex, StatementIndex: seed.intent.StatementIndex,
		StatementSHA256: seed.intent.StatementSHA256, PreviousAttemptTerminalDigest: cloneDigestPointer(seed.intent.PreviousAttemptTerminalDigest),
		PreviousIntermediateStateDigest: cloneDigestPointer(seed.intent.PreviousIntermediateStateDigest), ControlPlaneStates: states,
		AuthorityBeforeDigest: seed.intent.AuthorityBeforeDigest, AuthorityAfterDigest: facts.authority.Digest,
		CatalogBeforeDigest: seed.intent.CatalogBeforeDigest, CatalogAfterDigest: facts.catalog.Digest,
	}
	state.IntermediateStateDigest, err = state.ComputeDigest()
	if err != nil || state.Validate() != nil {
		return StatementIntermediateState{}, fail(CodeIntermediateStateMismatch, "runner-statement-after-state", "statement-after intermediate state could not be sealed", nil)
	}
	return state, nil
}

func runnerProjectedStatementAfterEvidenceMatches(seed runnerProjectedCurrentStatementAfterSeed) bool {
	executionSeed := runnerExecutedCurrentStatementSeed{
		session: seed.session, transaction: seed.transaction, evidence: seed.evidence, journal: seed.journal,
		key: seed.key, candidateBinding: seed.candidateBinding, generation: seed.generation,
		statementIntentCanonical: seed.executedCanonical, recoveryDigest: seed.recoveryDigest,
		dispatch: seed.dispatch, database: seed.database, maxAttempts: seed.maxAttempts,
		policy: seed.policy, plan: seed.plan, intent: seed.intent, cursor: seed.cursor,
		intentRecordDigest: seed.intentRecordDigest, checkpointDigest: seed.checkpointDigest,
	}
	return runnerExecutedStatementEvidenceMatches(executionSeed)
}

func invalidateRunnerProjectedStatementAfterCursor(seed runnerProjectedCurrentStatementAfterSeed) {
	if seed.cursor.valid != nil {
		seed.cursor.valid.Store(false)
	}
}

func bindRunnerProjectedCurrentStatementAfter(seed runnerProjectedCurrentStatementAfterSeed, facts runnerStatementAfterProjectionFacts, boundary BoundaryState, state StatementIntermediateState) (*runnerProjectedCurrentStatementAfter, error) {
	plan, err := cloneRunnerStatementIntentPlan(seed.plan)
	if err != nil || state.Validate() != nil || !runnerProjectedStatementAfterEvidenceMatches(seed) {
		return nil, fail(CodeTransactionBoundary, "runner-statement-after-seal", "statement-after inputs are unavailable or changed", nil)
	}
	prepared := &runnerProjectedCurrentStatementAfter{
		session: seed.session, transaction: seed.transaction, evidence: seed.evidence, journal: seed.journal,
		key: seed.key, candidateBinding: seed.candidateBinding, generation: seed.generation,
		executedCanonical: seed.executedCanonical, recoveryDigest: seed.recoveryDigest,
		dispatch: seed.dispatch, database: seed.database, maxAttempts: seed.maxAttempts,
		policy: cloneProjectionValue(seed.policy), plan: plan, intent: cloneProjectionValue(seed.intent), cursor: seed.cursor.clone(),
		intentRecordDigest: seed.intentRecordDigest, checkpointDigest: seed.checkpointDigest,
		executedStatementDigest: seed.executedStatementDigest, snapshotDigest: facts.snapshotDigest,
		authorityAfterProjection: cloneProjectionValue(facts.authority.Projection), catalogAfterProjection: cloneProjectionValue(facts.catalog.Projection),
		authorityAfter: ProjectionResultEvidence{Digest: facts.authority.Digest, Metadata: cloneProjectionValue(facts.authority.Metadata)},
		catalogAfter:   ProjectionResultEvidence{Digest: facts.catalog.Digest, Metadata: cloneProjectionValue(facts.catalog.Metadata)},
		boundary:       boundary, state: cloneProjectionValue(state), finalStatement: seed.plan.StatementIndex+1 == seed.dispatch.planCount,
	}
	prepared.self = prepared
	binding := &runnerProjectedCurrentStatementAfterBinding{
		prepared: prepared, session: seed.session, transaction: seed.transaction, evidence: seed.evidence, journal: seed.journal,
		key: seed.key, candidateBinding: seed.candidateBinding, cursorValid: seed.cursor.valid,
	}
	prepared.binding = binding
	prepared.canonical = runnerProjectedCurrentStatementAfterDigest(prepared)
	binding.canonical = prepared.canonical
	if prepared.canonical == ([32]byte{}) {
		return nil, fail(CodeTransactionBoundary, "runner-statement-after-seal", "statement-after authority could not be identified", nil)
	}
	runnerProjectedCurrentStatementAfterRegistry.Store(prepared, runnerProjectedCurrentStatementAfterRegistryRecord{
		prepared: prepared, binding: binding, session: seed.session, transaction: seed.transaction,
		evidence: seed.evidence, journal: seed.journal, key: seed.key, candidateBinding: seed.candidateBinding,
		cursorValid: seed.cursor.valid, canonical: prepared.canonical,
	})
	if !validRunnerProjectedCurrentStatementAfter(prepared) {
		runnerProjectedCurrentStatementAfterRegistry.Delete(prepared)
		return nil, fail(CodeTransactionBoundary, "runner-statement-after-seal", "statement-after authority could not be sealed", nil)
	}
	return prepared, nil
}

func validRunnerProjectedCurrentStatementAfter(prepared *runnerProjectedCurrentStatementAfter) bool {
	if prepared == nil || prepared.self != prepared || prepared.closed || prepared.binding == nil || prepared.binding.prepared != prepared || prepared.binding.key != prepared.key || prepared.binding.candidateBinding != prepared.candidateBinding || prepared.binding.cursorValid != prepared.cursor.valid || !sameRunnerOwnedPointer(prepared.binding.session, prepared.session) || !sameRunnerOwnedPointer(prepared.binding.transaction, prepared.transaction) || !sameRunnerOwnedPointer(prepared.binding.evidence, prepared.evidence) || !sameRunnerOwnedPointer(prepared.binding.journal, prepared.journal) || prepared.canonical == ([32]byte{}) || prepared.canonical != prepared.binding.canonical || prepared.canonical != runnerProjectedCurrentStatementAfterDigest(prepared) {
		return false
	}
	registered, ok := runnerProjectedCurrentStatementAfterRegistry.Load(prepared)
	record, recordOK := registered.(runnerProjectedCurrentStatementAfterRegistryRecord)
	if !ok || !recordOK || record.prepared != prepared || record.binding != prepared.binding || record.key != prepared.key || record.candidateBinding != prepared.candidateBinding || record.cursorValid != prepared.cursor.valid || record.canonical != prepared.canonical || !sameRunnerOwnedPointer(record.session, prepared.session) || !sameRunnerOwnedPointer(record.transaction, prepared.transaction) || !sameRunnerOwnedPointer(record.evidence, prepared.evidence) || !sameRunnerOwnedPointer(record.journal, prepared.journal) {
		return false
	}
	status, statusOK := migrationProjectionTxStatus(prepared.transaction)
	if !statusOK || status != 'T' || prepared.boundary.TxStatus != 'T' || prepared.boundary.CurrentUser != MigrationOwnerRole || !prepared.boundary.LockHeld {
		return false
	}
	seed := runnerProjectedCurrentStatementAfterSeed{
		session: prepared.session, transaction: prepared.transaction, evidence: prepared.evidence, journal: prepared.journal,
		key: prepared.key, candidateBinding: prepared.candidateBinding, generation: prepared.generation,
		executedCanonical: prepared.executedCanonical, recoveryDigest: prepared.recoveryDigest,
		dispatch: prepared.dispatch, database: prepared.database, maxAttempts: prepared.maxAttempts,
		policy: prepared.policy, plan: prepared.plan, intent: prepared.intent, cursor: prepared.cursor,
		intentRecordDigest: prepared.intentRecordDigest, checkpointDigest: prepared.checkpointDigest,
		executedStatementDigest: prepared.executedStatementDigest,
	}
	return runnerProjectedStatementAfterEvidenceMatches(seed)
}

func runnerProjectedCurrentStatementAfterDigest(prepared *runnerProjectedCurrentStatementAfter) [32]byte {
	if prepared == nil || prepared.self != prepared || prepared.closed || prepared.session == nil || prepared.transaction == nil || prepared.evidence == nil || prepared.journal == nil || prepared.candidateBinding == nil || prepared.candidateBinding.owner == nil || prepared.generation.owner != prepared.candidateBinding.owner || prepared.candidateBinding.canonical == ([32]byte{}) || prepared.executedCanonical == ([32]byte{}) || prepared.recoveryDigest == ([32]byte{}) || prepared.snapshotDigest == ([32]byte{}) || prepared.policy.Validate() != nil || prepared.policy.MaxAttempts != uint64(prepared.maxAttempts) || prepared.plan.validateExact() != nil || prepared.intent.Validate() != nil || prepared.authorityAfterProjection.Validate() != nil || prepared.catalogAfterProjection.Validate() != nil || prepared.authorityAfter.Validate() != nil || prepared.catalogAfter.Validate() != nil || prepared.authorityAfter.Digest != prepared.state.AuthorityAfterDigest || prepared.catalogAfter.Digest != prepared.state.CatalogAfterDigest || prepared.state.Validate() != nil || prepared.state.IntermediateStateDigest == "" || prepared.finalStatement != (prepared.plan.StatementIndex+1 == prepared.dispatch.planCount) || !prepared.cursor.Valid() || !sameGenerationIdentity(prepared.cursor.generation, prepared.generation) || prepared.cursor.previousRecordDigest == nil || *prepared.cursor.previousRecordDigest != prepared.intentRecordDigest || prepared.cursor.latestCheckpointRecordDigest == nil || *prepared.cursor.latestCheckpointRecordDigest != prepared.checkpointDigest || prepared.executedStatementDigest != prepared.plan.StatementSHA256 || prepared.boundary.TxStatus != 'T' || prepared.boundary.CurrentUser != MigrationOwnerRole || !prepared.boundary.LockHeld {
		return [32]byte{}
	}
	values := []any{prepared.policy, prepared.plan.exactSentinel(), prepared.intent, prepared.authorityAfterProjection, prepared.catalogAfterProjection, prepared.authorityAfter, prepared.catalogAfter, prepared.boundary, prepared.state}
	h := sha256.New()
	h.Write([]byte("cloud-agents-platform-runner-projected-current-statement-after/v1\x00"))
	h.Write(prepared.candidateBinding.canonical[:])
	h.Write(prepared.executedCanonical[:])
	h.Write(prepared.recoveryDigest[:])
	h.Write(prepared.snapshotDigest[:])
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
	writeAdmissionUint(h, uint64(prepared.maxAttempts))
	if prepared.finalStatement {
		writeAdmissionUint(h, 1)
	} else {
		writeAdmissionUint(h, 0)
	}
	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result
}

func closeRunnerProjectedCurrentStatementAfter(prepared *runnerProjectedCurrentStatementAfter, primary error) error {
	if prepared == nil {
		return primary
	}
	if prepared.self != prepared {
		return fail(CodeTransactionBoundary, "runner-statement-after-close", "statement-after copy cannot close database authority", nil)
	}
	registered, ok := runnerProjectedCurrentStatementAfterRegistry.Load(prepared)
	record, recordOK := registered.(runnerProjectedCurrentStatementAfterRegistryRecord)
	if !ok || !recordOK || record.prepared != prepared || record.binding == nil {
		return fail(CodeTransactionBoundary, "runner-statement-after-close", "statement-after authority is unavailable", nil)
	}
	valid := validRunnerProjectedCurrentStatementAfter(prepared)
	runnerProjectedCurrentStatementAfterRegistry.Delete(prepared)
	prepared.closed = true
	prepared.session = nil
	prepared.transaction = nil
	prepared.evidence = nil
	prepared.journal = nil
	prepared.binding = nil
	prepared.policy = ExecutionPolicy{}
	prepared.plan = StatementPlan{}
	prepared.intent = StatementIntent{}
	prepared.authorityAfterProjection = AuthorityProjection{}
	prepared.catalogAfterProjection = CatalogStateProjection{}
	prepared.authorityAfter = ProjectionResultEvidence{}
	prepared.catalogAfter = ProjectionResultEvidence{}
	prepared.state = StatementIntermediateState{}
	if !valid {
		primary = fail(CodeTransactionBoundary, "runner-statement-after-close", "statement-after authority changed before close", nil)
	}
	return closeRunnerCurrentTransactionResources(record.session, record.transaction, record.key, primary)
}
