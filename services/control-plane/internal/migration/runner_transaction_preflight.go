package migration

import (
	"context"
	"crypto/sha256"
	"strconv"
	"sync"
)

// runnerPreparedCurrentTransaction is the one-shot authority proving that the
// exact prepared session entered a SERIALIZABLE/READ WRITE transaction and
// that authority plus predecessor projections were repeated inside that same
// transaction. This slice deliberately exposes no execution or commit method.
type runnerPreparedCurrentTransaction struct {
	self              *runnerPreparedCurrentTransaction
	binding           *runnerPreparedCurrentTransactionBinding
	session           DatabaseSession
	transaction       MigrationTransaction
	evidence          EvidenceSession
	key               int64
	candidateBinding  *verifiedEvidenceRunBinding
	generation        generationIdentity
	recoveryDigest    [32]byte
	preparedCanonical [32]byte
	dispatch          runnerPreparedDispatch
	database          runnerPreparedDatabaseIdentity
	snapshotDigest    [32]byte
	authorityDigest   Digest
	catalogDigest     Digest
	canonical         [32]byte
	closed            bool
}

type runnerPreparedCurrentTransactionBinding struct {
	prepared         *runnerPreparedCurrentTransaction
	session          DatabaseSession
	transaction      MigrationTransaction
	evidence         EvidenceSession
	key              int64
	candidateBinding *verifiedEvidenceRunBinding
	canonical        [32]byte
}

type runnerPreparedCurrentTransactionRegistryRecord struct {
	prepared         *runnerPreparedCurrentTransaction
	binding          *runnerPreparedCurrentTransactionBinding
	session          DatabaseSession
	transaction      MigrationTransaction
	evidence         EvidenceSession
	key              int64
	candidateBinding *verifiedEvidenceRunBinding
	canonical        [32]byte
}

type runnerPreparedCurrentTransactionSeed struct {
	session           DatabaseSession
	evidence          EvidenceSession
	key               int64
	candidateBinding  *verifiedEvidenceRunBinding
	generation        generationIdentity
	recoveryDigest    [32]byte
	preparedCanonical [32]byte
	preparedCatalog   Digest
	database          runnerPreparedDatabaseIdentity
	dispatch          runnerPreparedDispatch
	policy            ExecutionPolicy
	bindings          RunnerProjectionBindings
	firstPlan         StatementPlan
}

var runnerPreparedCurrentTransactionRegistry sync.Map

func (runner *Runner) prepareCurrentTransaction(ctx context.Context, prepared *runnerPreparedCurrentSession, bundle *RuntimeBundle, plans []StatementPlan) (*runnerPreparedCurrentTransaction, error) {
	seed, err := consumeRunnerPreparedCurrentSession(prepared, bundle, plans)
	if err != nil {
		return nil, closeRunnerPreparedCurrentSession(prepared, err)
	}
	failClosed := func(transaction MigrationTransaction, primary error) (*runnerPreparedCurrentTransaction, error) {
		return nil, closeRunnerCurrentTransactionResources(seed.session, transaction, seed.key, primary)
	}

	transaction, beginErr := seed.session.BeginMigration(ctx)
	if beginErr != nil {
		return failClosed(transaction, mapRunnerDatabasePreflightError(beginErr, "runner-transaction-begin", "serializable migration transaction could not be opened"))
	}
	if transaction == nil || !runnerOwnedPointer(transaction) {
		return failClosed(transaction, fail(CodeTransactionBoundary, "runner-transaction-begin", "migration transaction ownership is unavailable", nil))
	}
	profile, profileOK := transaction.(runnerTransactionProjectionProfile)
	if !profileOK {
		return failClosed(transaction, fail(CodeTransactionBoundary, "runner-transaction-profile", "migration transaction cannot enter the closed projection profile", nil))
	}
	if profileErr := profile.enterRunnerProjectionProfile(ctx); profileErr != nil {
		return failClosed(transaction, mapRunnerDatabasePreflightError(profileErr, "runner-transaction-profile", "migration transaction projection profile could not be configured"))
	}
	snapshot, snapshotErr := BorrowMigrationProjectionSnapshot(ctx, transaction, seed.dispatch.migrationID, nil)
	if snapshotErr != nil {
		return failClosed(transaction, mapRunnerDatabasePreflightError(snapshotErr, "runner-transaction-snapshot", "transaction-wide projection snapshot could not be borrowed"))
	}
	if snapshot == nil {
		return failClosed(transaction, fail(CodeProjectionSnapshotInvalid, "runner-transaction-snapshot", "transaction-wide projection snapshot is unavailable", nil))
	}
	metadata := snapshot.Metadata()
	if !sameRunnerPreparedDatabaseIdentity(seed.database, metadata) || metadata.MigrationID == nil || *metadata.MigrationID != seed.dispatch.migrationID || metadata.StatementIndex != nil {
		return failClosed(transaction, fail(CodeProjectionMetadataMismatch, "runner-transaction-snapshot", "transaction snapshot differs from the prepared database identity or dispatch", nil))
	}
	factory := runner.projectionFactory
	if factory == nil {
		factory = pgRunnerAuthorityProjectorFactory{}
	}
	projector, factoryErr := factory.newRunnerAuthorityProjector(ctx, snapshot)
	if factoryErr != nil {
		return failClosed(transaction, mapRunnerDatabasePreflightError(factoryErr, "runner-transaction-projector", "transaction-wide projector could not be constructed"))
	}
	if projector == nil {
		return failClosed(transaction, fail(CodeProjectionSnapshotInvalid, "runner-transaction-projector", "transaction-wide projector is unavailable", nil))
	}
	authority, authorityErr := projector.ProjectAuthority(ctx, snapshot, seed.bindings.verifiedAuthority, AuthorityPhaseMigrationTransaction)
	if authorityErr != nil {
		return failClosed(transaction, mapRunnerDatabasePreflightError(authorityErr, "runner-transaction-authority", "transaction authority projection failed"))
	}
	if err := validateRunnerAuthorityProjectionResult(authority, metadata, seed.bindings.verifiedAuthority, AuthorityPhaseMigrationTransaction); err != nil {
		return failClosed(transaction, mapRunnerDatabasePreflightError(err, "runner-transaction-authority", "transaction authority projection result is invalid"))
	}
	condition := seed.bindings.initialSchemaScope.BoundPrecondition()
	precondition, preconditionErr := projector.ProjectPrecondition(ctx, snapshot, seed.bindings.initialSchemaScope, condition)
	if preconditionErr != nil {
		return failClosed(transaction, mapRunnerDatabasePreflightError(preconditionErr, "runner-transaction-precondition", "transaction predecessor projection failed"))
	}
	if err := validateRunnerPreconditionResult(precondition, metadata, seed.bindings.initialSchemaScope, condition, seed.firstPlan.ExpectedTransition.CatalogBefore, AuthorityPhaseMigrationTransaction, "runner-transaction-precondition"); err != nil {
		return failClosed(transaction, err)
	}
	if precondition.Digest != seed.firstPlan.ExpectedTransition.CatalogBefore.Digest || precondition.Digest != seed.preparedCatalog || !runnerCanonicalEqual(authority.Metadata.Snapshot, precondition.Metadata.Snapshot) {
		return failClosed(transaction, fail(CodeProjectionMetadataMismatch, "runner-transaction-precondition", "transaction authority or predecessor projection changed before dispatch", nil))
	}
	if restoreErr := profile.restoreRunnerExecutionProfile(ctx, seed.policy); restoreErr != nil {
		return failClosed(transaction, mapRunnerDatabasePreflightError(restoreErr, "runner-transaction-execution-profile", "verified migration execution profile could not be restored"))
	}
	boundary, boundaryErr := transaction.Boundary(ctx, seed.key)
	status, statusOK := migrationProjectionTxStatus(transaction)
	if boundaryErr != nil || !statusOK || status != 'T' || boundary.TxStatus != 'T' || boundary.CurrentUser != MigrationOwnerRole || !boundary.LockHeld {
		return failClosed(transaction, mapRunnerDatabasePreflightError(boundaryErr, "runner-transaction-boundary", "transaction escaped the exact role, status, or advisory lock boundary"))
	}
	if !runnerPreparedEvidenceMatches(seed.evidence, seed.candidateBinding, seed.generation, seed.recoveryDigest) {
		return failClosed(transaction, fail(CodeEvidenceJournalFailed, "runner-transaction-evidence", "current evidence authority changed during transaction-wide preflight", nil))
	}
	preparedTransaction, bindErr := bindRunnerPreparedCurrentTransaction(seed, transaction, metadata, authority, precondition)
	if bindErr != nil {
		return failClosed(transaction, bindErr)
	}
	return preparedTransaction, nil
}

func consumeRunnerPreparedCurrentSession(prepared *runnerPreparedCurrentSession, bundle *RuntimeBundle, plans []StatementPlan) (runnerPreparedCurrentTransactionSeed, error) {
	if !validRunnerPreparedCurrentSession(prepared) {
		return runnerPreparedCurrentTransactionSeed{}, fail(CodeTransactionBoundary, "runner-transaction-claim", "prepared session authority is unavailable or changed", nil)
	}
	if bundle == nil || bundle.Manifest.ExecutionPolicy.Validate() != nil || bundle.Manifest.SchemaBundleDigest != prepared.generation.schemaBundleDigest || len(bundle.Manifest.SchemaBundle.Migrations) == 0 || bundle.Manifest.SchemaBundle.Migrations[0].ID != prepared.dispatch.migrationID {
		return runnerPreparedCurrentTransactionSeed{}, fail(CodeUntrusted, "runner-transaction-claim", "runtime bundle differs from the prepared dispatch", nil)
	}
	planDigest, planCount, err := runnerEntryPlanClosureDigest(plans, prepared.dispatch.migrationID)
	if err != nil || planDigest != prepared.dispatch.planDigest || planCount != prepared.dispatch.planCount {
		return runnerPreparedCurrentTransactionSeed{}, fail(CodeUntrusted, "runner-transaction-claim", "statement plan closure differs from the prepared dispatch", nil)
	}
	firstPlan, err := firstRunnerStatementPlan(bundle, plans)
	if err != nil {
		return runnerPreparedCurrentTransactionSeed{}, err
	}
	current := prepared.evidence.CurrentCandidate()
	if !validOwnedCurrentCandidate(current) || current.binding != prepared.candidateBinding {
		return runnerPreparedCurrentTransactionSeed{}, fail(CodeEvidenceJournalFailed, "runner-transaction-claim", "current evidence candidate differs from the prepared dispatch", nil)
	}
	bindings, err := current.verifiedRun.currentDecision.decision.runnerProjectionBindings()
	if err != nil || bindings.schemaBundleDigest != bundle.Manifest.SchemaBundleDigest || bindings.runnerProjectionDecisionDigest != prepared.generation.runnerProjectionDecisionDigest {
		return runnerPreparedCurrentTransactionSeed{}, fail(CodeUntrusted, "runner-transaction-claim", "verified projection bindings differ from the prepared dispatch", nil)
	}
	registered, ok := runnerPreparedCurrentSessionRegistry.LoadAndDelete(prepared)
	record, recordOK := registered.(runnerPreparedCurrentSessionRegistryRecord)
	if !ok || !recordOK || record.prepared != prepared || record.binding != prepared.binding || record.key != prepared.key || record.candidateBinding != prepared.candidateBinding || record.canonical != prepared.canonical || !sameRunnerOwnedPointer(record.session, prepared.session) || !sameRunnerOwnedPointer(record.evidence, prepared.evidence) {
		return runnerPreparedCurrentTransactionSeed{}, fail(CodeTransactionBoundary, "runner-transaction-claim", "prepared session authority could not be consumed exactly once", nil)
	}
	seed := runnerPreparedCurrentTransactionSeed{
		session: record.session, evidence: record.evidence, key: record.key, candidateBinding: record.candidateBinding,
		generation: prepared.generation, recoveryDigest: prepared.recoveryDigest, preparedCanonical: record.canonical, preparedCatalog: prepared.catalogDigest,
		database: prepared.database, dispatch: prepared.dispatch, policy: bundle.Manifest.ExecutionPolicy, bindings: bindings, firstPlan: firstPlan,
	}
	prepared.closed = true
	prepared.session = nil
	prepared.evidence = nil
	prepared.binding = nil
	return seed, nil
}

func bindRunnerPreparedCurrentTransaction(seed runnerPreparedCurrentTransactionSeed, transaction MigrationTransaction, metadata SnapshotMetadata, authority ProjectionResult[AuthorityProjection], precondition ProjectionResult[CatalogStateProjection]) (*runnerPreparedCurrentTransaction, error) {
	snapshotDigest := runnerTransactionSnapshotDigest(metadata)
	if snapshotDigest == ([32]byte{}) || transaction == nil || !runnerOwnedPointer(transaction) || authority.Digest.Validate() != nil || precondition.Digest.Validate() != nil || !runnerPreparedEvidenceMatches(seed.evidence, seed.candidateBinding, seed.generation, seed.recoveryDigest) {
		return nil, fail(CodeTransactionBoundary, "runner-transaction-seal", "transaction-wide preflight inputs are unavailable or changed", nil)
	}
	prepared := &runnerPreparedCurrentTransaction{
		session: seed.session, transaction: transaction, evidence: seed.evidence, key: seed.key,
		candidateBinding: seed.candidateBinding, generation: seed.generation, recoveryDigest: seed.recoveryDigest,
		preparedCanonical: seed.preparedCanonical, dispatch: seed.dispatch, database: seed.database,
		snapshotDigest: snapshotDigest, authorityDigest: authority.Digest, catalogDigest: precondition.Digest,
	}
	prepared.self = prepared
	binding := &runnerPreparedCurrentTransactionBinding{
		prepared: prepared, session: seed.session, transaction: transaction, evidence: seed.evidence,
		key: seed.key, candidateBinding: seed.candidateBinding,
	}
	prepared.binding = binding
	prepared.canonical = runnerPreparedCurrentTransactionDigest(prepared)
	binding.canonical = prepared.canonical
	if prepared.canonical == ([32]byte{}) {
		return nil, fail(CodeTransactionBoundary, "runner-transaction-seal", "transaction-wide preflight could not be identified", nil)
	}
	runnerPreparedCurrentTransactionRegistry.Store(prepared, runnerPreparedCurrentTransactionRegistryRecord{
		prepared: prepared, binding: binding, session: seed.session, transaction: transaction, evidence: seed.evidence,
		key: seed.key, candidateBinding: seed.candidateBinding, canonical: prepared.canonical,
	})
	if !validRunnerPreparedCurrentTransaction(prepared) {
		runnerPreparedCurrentTransactionRegistry.Delete(prepared)
		return nil, fail(CodeTransactionBoundary, "runner-transaction-seal", "transaction-wide preflight could not be sealed", nil)
	}
	return prepared, nil
}

func validRunnerPreparedCurrentTransaction(prepared *runnerPreparedCurrentTransaction) bool {
	if prepared == nil || prepared.self != prepared || prepared.closed || prepared.binding == nil || prepared.binding.prepared != prepared || prepared.key != prepared.binding.key || prepared.candidateBinding == nil || prepared.binding.candidateBinding != prepared.candidateBinding || !sameRunnerOwnedPointer(prepared.session, prepared.binding.session) || !sameRunnerOwnedPointer(prepared.transaction, prepared.binding.transaction) || !sameRunnerOwnedPointer(prepared.evidence, prepared.binding.evidence) || prepared.canonical == ([32]byte{}) || prepared.canonical != prepared.binding.canonical || prepared.canonical != runnerPreparedCurrentTransactionDigest(prepared) {
		return false
	}
	registered, ok := runnerPreparedCurrentTransactionRegistry.Load(prepared)
	record, recordOK := registered.(runnerPreparedCurrentTransactionRegistryRecord)
	if !ok || !recordOK || record.prepared != prepared || record.binding != prepared.binding || record.key != prepared.key || record.candidateBinding != prepared.candidateBinding || record.canonical != prepared.canonical || !sameRunnerOwnedPointer(record.session, prepared.session) || !sameRunnerOwnedPointer(record.transaction, prepared.transaction) || !sameRunnerOwnedPointer(record.evidence, prepared.evidence) {
		return false
	}
	status, statusOK := migrationProjectionTxStatus(prepared.transaction)
	return statusOK && status == 'T' && runnerPreparedEvidenceMatches(prepared.evidence, prepared.candidateBinding, prepared.generation, prepared.recoveryDigest)
}

func runnerPreparedCurrentTransactionDigest(prepared *runnerPreparedCurrentTransaction) [32]byte {
	if prepared == nil || prepared.self != prepared || prepared.closed || prepared.session == nil || prepared.transaction == nil || prepared.evidence == nil || prepared.candidateBinding == nil || prepared.candidateBinding.owner == nil || prepared.generation.owner != prepared.candidateBinding.owner || prepared.candidateBinding.canonical == ([32]byte{}) || prepared.recoveryDigest == ([32]byte{}) || prepared.preparedCanonical == ([32]byte{}) || prepared.snapshotDigest == ([32]byte{}) || prepared.authorityDigest.Validate() != nil || prepared.catalogDigest.Validate() != nil || prepared.database.postgresMajor == 0 || prepared.database.serverVersionNum == 0 || prepared.database.databaseName == "" || prepared.database.sessionUser == "" || prepared.database.currentUser != MigrationOwnerRole || prepared.dispatch.recoveryState != RecoveryBrandNew && prepared.dispatch.recoveryState != RecoveryBrandNewInherited || prepared.dispatch.action != RecoveryBeginFirstAttempt || !migrationIDPattern.MatchString(prepared.dispatch.migrationID) || prepared.dispatch.attemptIndex != 1 || prepared.dispatch.entryIndex != 0 || prepared.dispatch.planCount == 0 || prepared.dispatch.planDigest == ([32]byte{}) {
		return [32]byte{}
	}
	h := sha256.New()
	h.Write([]byte("cloud-agents-platform-runner-prepared-current-transaction/v1\x00"))
	h.Write(prepared.candidateBinding.canonical[:])
	h.Write(prepared.preparedCanonical[:])
	h.Write(prepared.recoveryDigest[:])
	h.Write(prepared.snapshotDigest[:])
	h.Write(prepared.dispatch.planDigest[:])
	for _, value := range []Digest{prepared.generation.executionLineageDigest, prepared.generation.journalIdentityDigest, prepared.generation.runnerProjectionDecisionDigest, prepared.generation.schemaBundleDigest, prepared.authorityDigest, prepared.catalogDigest} {
		if value.Validate() != nil {
			return [32]byte{}
		}
		writeAdmissionString(h, value.String())
	}
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
	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result
}

func runnerTransactionSnapshotDigest(metadata SnapshotMetadata) [32]byte {
	if err := metadata.validate(); err != nil || metadata.AuthorityPhase != AuthorityPhaseMigrationTransaction || metadata.Mode != MigrationSnapshot || metadata.Ownership != BorrowedMigrationSnapshot || metadata.MigrationID == nil || metadata.StatementIndex != nil {
		return [32]byte{}
	}
	canonical, err := canonicalContractKey(metadata)
	if err != nil || canonical == "" {
		return [32]byte{}
	}
	h := sha256.New()
	h.Write([]byte("cloud-agents-platform-runner-transaction-snapshot/v1\x00"))
	writeAdmissionString(h, canonical)
	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result
}

func sameRunnerPreparedDatabaseIdentity(prepared runnerPreparedDatabaseIdentity, metadata SnapshotMetadata) bool {
	return metadata.validate() == nil && metadata.AuthorityPhase == AuthorityPhaseMigrationTransaction && metadata.Mode == MigrationSnapshot && metadata.Ownership == BorrowedMigrationSnapshot && metadata.PostgresMajor == prepared.postgresMajor && metadata.ServerVersionNum == prepared.serverVersionNum && metadata.DatabaseName == prepared.databaseName && metadata.SessionUser == prepared.sessionUser && metadata.CurrentUser == prepared.currentUser && metadata.CurrentUser == MigrationOwnerRole
}

func closeRunnerPreparedCurrentTransaction(prepared *runnerPreparedCurrentTransaction, primary error) error {
	if prepared == nil {
		return primary
	}
	if prepared.self != prepared {
		return fail(CodeTransactionBoundary, "runner-transaction-close", "transaction-wide preflight copy cannot close database authority", nil)
	}
	registered, ok := runnerPreparedCurrentTransactionRegistry.Load(prepared)
	record, recordOK := registered.(runnerPreparedCurrentTransactionRegistryRecord)
	if !ok || !recordOK || record.prepared != prepared || record.binding == nil {
		return fail(CodeTransactionBoundary, "runner-transaction-close", "transaction-wide preflight authority is unavailable", nil)
	}
	valid := validRunnerPreparedCurrentTransaction(prepared)
	runnerPreparedCurrentTransactionRegistry.Delete(prepared)
	prepared.closed = true
	prepared.session = nil
	prepared.transaction = nil
	prepared.evidence = nil
	prepared.binding = nil
	if !valid {
		primary = fail(CodeTransactionBoundary, "runner-transaction-close", "transaction-wide preflight authority changed before close", nil)
	}
	return closeRunnerCurrentTransactionResources(record.session, record.transaction, record.key, primary)
}

func closeRunnerCurrentTransactionResources(session DatabaseSession, transaction MigrationTransaction, key int64, primary error) error {
	if transaction != nil {
		cleanupCtx, cancel := cleanupContext()
		rollbackErr := transaction.Rollback(cleanupCtx)
		cancel()
		status, statusOK := migrationProjectionTxStatus(transaction)
		if rollbackErr != nil || !statusOK || status != 'I' {
			primary = mapRunnerDatabasePreflightError(rollbackErr, "runner-transaction-rollback", "migration transaction rollback could not be proven")
		}
	}
	return closeRunnerDatabasePreflight(session, key, true, primary)
}
