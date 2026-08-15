package migration

import (
	"context"
	"errors"
)

const runnerAuthorityProjectionQueryCount uint32 = 5

// runnerAuthorityProjectorFactory is deliberately package-private and sealed.
// Production always uses PGProjector; tests may replace it without exporting a
// caller-supplied authority or raw-query seam.
type runnerAuthorityProjectorFactory interface {
	newRunnerAuthorityProjector(context.Context, ProjectionSnapshot) (runnerAuthorityProjector, error)
	runnerAuthorityProjectorFactorySealed()
}

type runnerAuthorityProjector interface {
	ProjectAuthority(context.Context, ProjectionSnapshot, VerifiedAuthorityContract, AuthorityPhase) (ProjectionResult[AuthorityProjection], error)
	ProjectPrecondition(context.Context, ProjectionSnapshot, VerifiedSchemaBundleScope, CatalogPrecondition) (ProjectionResult[CatalogStateProjection], error)
	ProjectTransitionState(context.Context, ProjectionSnapshot, VerifiedCatalogContract, ProjectionScope) (ProjectionResult[CatalogStateProjection], error)
}

type pgRunnerAuthorityProjectorFactory struct{}

func (pgRunnerAuthorityProjectorFactory) newRunnerAuthorityProjector(ctx context.Context, snapshot ProjectionSnapshot) (runnerAuthorityProjector, error) {
	return NewPGProjector(ctx, snapshot)
}

func (pgRunnerAuthorityProjectorFactory) runnerAuthorityProjectorFactorySealed() {}

type runnerLedgerPrefixReader interface {
	readRunnerLedgerPrefix(context.Context) ([]LedgerRow, error)
}

type runnerLedgerPrefix struct {
	rows   []CommitIntentLedgerRow
	digest Digest
	head   string
}

func (runner *Runner) prepareCurrentDatabaseSession(ctx context.Context, dsn string, bundle *RuntimeBundle, plans []StatementPlan, evidence EvidenceSession, openedSnapshot *RecoverySnapshot, candidate OwnedCurrentCandidate) (*runnerPreparedCurrentSession, error) {
	bindings, err := runnerCurrentProjectionBindings(evidence, candidate)
	if err != nil {
		return nil, err
	}
	if runner.Connector == nil {
		return nil, fail(CodeProjectionNotImplemented, "runner-database-connector", "evidence session is active but no database connector is configured", nil)
	}
	if bundle == nil || dsn == "" || bindings.schemaBundleDigest != bundle.Manifest.SchemaBundleDigest {
		return nil, fail(CodeUntrusted, "runner-database-preflight", "database preflight inputs differ from the active evidence generation", nil)
	}
	key, err := bundle.Manifest.SchemaBundle.AdvisoryLock.Key()
	if err != nil {
		return nil, err
	}
	runner.transition(StateConnect)
	session, connectErr := runner.Connector.Connect(ctx, dsn)
	if connectErr != nil || session == nil {
		primary := connectErr
		if primary == nil {
			primary = fail(CodeTransactionBoundary, "runner-connect", "database connector returned no dedicated session", nil)
		} else {
			primary = mapRunnerDatabasePreflightError(primary, "runner-connect", "dedicated database connection failed")
		}
		return nil, closeRunnerDatabasePreflight(session, 0, false, primary)
	}
	locked := false
	failClosed := func(primary error) (*runnerPreparedCurrentSession, error) {
		return nil, closeRunnerDatabasePreflight(session, key, locked, primary)
	}

	connected, err := runner.projectRunnerAuthorityPhase(ctx, session, bindings.verifiedAuthority, AuthorityPhaseConnectedSession)
	if err != nil {
		return failClosed(err)
	}
	major := connected.Metadata.Snapshot.PostgresMajor
	policy := bundle.Manifest.ExecutionPolicy
	if uint64(major) < policy.PostgresMajorMin || uint64(major) > policy.PostgresMajorMax {
		return failClosed(fail(CodeUnsupported, "runner-server-version", "PostgreSQL major is outside the signed execution policy", nil))
	}
	if err := session.SetRoleAndSettings(ctx, policy); err != nil {
		return failClosed(mapRunnerDatabasePreflightError(err, "runner-session-settings", "dedicated session role or settings could not be configured"))
	}
	if err := session.AcquireAdvisoryLock(ctx, key); err != nil {
		return failClosed(mapRunnerDatabasePreflightError(err, "runner-advisory-lock", "signed advisory lock could not be acquired"))
	}
	locked = true
	migrationRole, err := runner.projectRunnerAuthorityPhase(ctx, session, bindings.verifiedAuthority, AuthorityPhaseMigrationRole)
	if err != nil {
		return failClosed(err)
	}
	if connected.Metadata.Snapshot.PostgresMajor != migrationRole.Metadata.Snapshot.PostgresMajor ||
		connected.Metadata.Snapshot.ServerVersionNum != migrationRole.Metadata.Snapshot.ServerVersionNum ||
		connected.Metadata.Snapshot.DatabaseName != migrationRole.Metadata.Snapshot.DatabaseName ||
		connected.Metadata.Snapshot.SessionUser != migrationRole.Metadata.Snapshot.SessionUser {
		return failClosed(fail(CodeProjectionMetadataMismatch, "runner-authority-preflight", "authority phases do not describe the same dedicated database session", nil))
	}
	runner.transition(StateLocked)
	before, err := readRunnerLedgerPrefix(ctx, session, bundle)
	if err != nil {
		return failClosed(err)
	}
	if len(before.rows) != 0 {
		if len(before.rows) == len(bundle.Manifest.SchemaBundle.Migrations) {
			return failClosed(fail(CodeProjectionNotImplemented, "runner-complete-ledger-preflight", "final catalog verification for a complete ledger is not implemented", nil))
		}
		return failClosed(fail(CodeProjectionNotImplemented, "runner-existing-ledger-preflight", "non-empty ledger catalog preflight is not implemented", nil))
	}
	firstPlan, err := firstRunnerStatementPlan(bundle, plans)
	if err != nil {
		return failClosed(err)
	}
	precondition, err := runner.projectRunnerInitialPrecondition(ctx, session, bindings.initialSchemaScope, firstPlan)
	if err != nil {
		return failClosed(err)
	}
	if !sameRunnerDatabaseIdentity(migrationRole.Metadata.Snapshot, precondition.Metadata.Snapshot) {
		return failClosed(fail(CodeProjectionMetadataMismatch, "runner-initial-precondition", "initial catalog projection does not describe the locked database session", nil))
	}
	after, err := readRunnerLedgerPrefix(ctx, session, bundle)
	if err != nil {
		return failClosed(err)
	}
	if !sameRunnerLedgerPrefix(before, after) {
		return failClosed(fail(CodeInvalidLedger, "runner-ledger-preflight", "ledger prefix changed across the initial catalog projection", nil))
	}
	prepared, err := bindRunnerPreparedCurrentSession(session, evidence, key, candidate, openedSnapshot, before, migrationRole, precondition, bundle, plans)
	if err != nil {
		return failClosed(err)
	}
	return prepared, nil
}

func sameRunnerDatabaseIdentity(left, right SnapshotMetadata) bool {
	return left.validate() == nil && right.validate() == nil && left.AuthorityPhase == AuthorityPhaseMigrationRole && right.AuthorityPhase == AuthorityPhaseMigrationRole && runnerCanonicalEqual(left, right)
}

func firstRunnerStatementPlan(bundle *RuntimeBundle, plans []StatementPlan) (StatementPlan, error) {
	if bundle == nil || len(bundle.Manifest.SchemaBundle.Migrations) == 0 || len(plans) == 0 {
		return StatementPlan{}, fail(CodeInvalidManifest, "runner-initial-precondition", "first migration statement plan is unavailable", nil)
	}
	entry := bundle.Manifest.SchemaBundle.Migrations[0]
	plan := plans[0]
	condition := entry.PredecessorCatalogContract
	if len(condition.AcceptedStates) == 0 || plan.validateExact() != nil || plan.MigrationID != entry.ID || plan.StatementIndex != 0 || !equalProjectionScopes(plan.ExpectedTransition.CatalogBefore.Scope, acceptedScope(condition.AcceptedStates[0])) {
		return StatementPlan{}, fail(CodeUntrusted, "runner-initial-precondition", "first migration statement plan differs from the signed predecessor", nil)
	}
	return plan, nil
}

func readRunnerLedgerPrefix(ctx context.Context, session DatabaseSession, bundle *RuntimeBundle) (runnerLedgerPrefix, error) {
	reader, ok := session.(runnerLedgerPrefixReader)
	if !ok {
		return runnerLedgerPrefix{}, fail(CodeInvalidLedger, "runner-ledger-preflight", "dedicated runner session cannot read the closed ledger prefix", nil)
	}
	rows, err := reader.readRunnerLedgerPrefix(ctx)
	if err != nil {
		return runnerLedgerPrefix{}, mapRunnerDatabasePreflightError(err, "runner-ledger-preflight", "migration ledger prefix could not be read")
	}
	owned := cloneProjectionValue(rows)
	snapshot, err := ValidateLedger(owned, bundle.Lineage)
	if err != nil {
		return runnerLedgerPrefix{}, mapRunnerDatabasePreflightError(err, "runner-ledger-preflight", "migration ledger prefix is invalid")
	}
	facts := runnerLedgerPrefix{rows: make([]CommitIntentLedgerRow, len(owned)), head: snapshot.Head}
	for index := range owned {
		facts.rows[index], err = commitIntentLedgerRowFromObserved(owned[index])
		if err != nil {
			return runnerLedgerPrefix{}, err
		}
	}
	facts.digest, err = LedgerPrefixDigest(facts.rows)
	if err != nil {
		return runnerLedgerPrefix{}, err
	}
	return facts, nil
}

func commitIntentLedgerRowFromObserved(row LedgerRow) (CommitIntentLedgerRow, error) {
	if row.SQLSizeBytes < 0 {
		return CommitIntentLedgerRow{}, fail(CodeInvalidLedger, row.MigrationID, "ledger SQL size is negative", nil)
	}
	result := CommitIntentLedgerRow{
		MigrationID: row.MigrationID, MigrationName: row.MigrationName, PredecessorID: cloneProjectionValue(row.PredecessorID),
		Phase: row.Phase, SchemaFrom: row.SchemaFrom, SchemaTo: row.SchemaTo,
		CompatibleBinaryMin: row.CompatibleBinaryMin, CompatibleBinaryMax: row.CompatibleBinaryMax,
		SQLPath: row.SQLPath, SQLSizeBytes: uint64(row.SQLSizeBytes), SQLSHA256: row.SQLSHA256,
		BundleDigest: row.BundleDigest, TransactionMode: row.TransactionMode, Reentrancy: row.Reentrancy,
		RollbackBoundary: row.RollbackBoundary, RequiresLiveInstancePreflight: row.RequiresLiveInstancePreflight,
		RequiresPITRPreflight: row.RequiresPITRPreflight,
	}
	if err := result.Validate(); err != nil {
		return CommitIntentLedgerRow{}, fail(CodeInvalidLedger, row.MigrationID, "ledger identity cannot enter the exact prefix", nil)
	}
	return result, nil
}

func sameRunnerLedgerPrefix(left, right runnerLedgerPrefix) bool {
	return left.digest.Validate() == nil && right.digest.Validate() == nil && left.digest == right.digest && left.head == right.head && runnerCanonicalEqual(left.rows, right.rows)
}

func runnerCurrentProjectionBindings(evidence EvidenceSession, candidate OwnedCurrentCandidate) (RunnerProjectionBindings, error) {
	if evidence == nil || !validOwnedCurrentCandidate(candidate) {
		return RunnerProjectionBindings{}, fail(CodeEvidenceJournalFailed, "runner-database-preflight", "active evidence authority is unavailable", nil)
	}
	current := evidence.CurrentCandidate()
	active := evidence.ActiveGeneration()
	if !validOwnedCurrentCandidate(current) || current.binding != candidate.binding || active.kind != activeGenerationCurrent || active.ownedDecision.owner != candidate.verifiedRun.currentDecision.owner || active.ownedDecision.digest != candidate.verifiedRun.currentDecision.digest || !active.ownedDecision.decision.exactlyMatches(candidate.verifiedRun.currentDecision.decision) {
		return RunnerProjectionBindings{}, fail(CodeEvidenceRecoveryRequired, "runner-database-preflight", "only the exact current evidence generation can enter database preflight", nil)
	}
	bindings, err := active.ownedDecision.decision.runnerProjectionBindings()
	candidateBindings, candidateErr := candidate.verifiedRun.currentDecision.decision.runnerProjectionBindings()
	if err != nil || candidateErr != nil || !bindings.exactlyMatches(candidateBindings) {
		return RunnerProjectionBindings{}, fail(CodeUntrusted, "runner-database-preflight", "active evidence projection bindings are unavailable or mismatched", nil)
	}
	return bindings, nil
}

func (runner *Runner) projectRunnerAuthorityPhase(ctx context.Context, session DatabaseSession, contract VerifiedAuthorityContract, phase AuthorityPhase) (ProjectionResult[AuthorityProjection], error) {
	snapshot, err := BeginRunnerSessionProjectionSnapshot(ctx, session, phase)
	if err != nil {
		return ProjectionResult[AuthorityProjection]{}, mapRunnerDatabasePreflightError(err, "runner-authority-snapshot", "runner authority snapshot could not be opened")
	}
	if snapshot == nil {
		return ProjectionResult[AuthorityProjection]{}, fail(CodeProjectionSnapshotInvalid, "runner-authority-snapshot", "runner authority snapshot is unavailable", nil)
	}
	metadata := snapshot.Metadata()
	factory := runner.projectionFactory
	if factory == nil {
		factory = pgRunnerAuthorityProjectorFactory{}
	}
	projector, factoryErr := factory.newRunnerAuthorityProjector(ctx, snapshot)
	var result ProjectionResult[AuthorityProjection]
	var projectionErr error
	if factoryErr == nil && projector != nil {
		result, projectionErr = projector.ProjectAuthority(ctx, snapshot, contract, phase)
	} else if factoryErr != nil {
		projectionErr = mapRunnerDatabasePreflightError(factoryErr, "runner-authority-projector", "runner authority projector could not be constructed")
	} else {
		projectionErr = fail(CodeProjectionSnapshotInvalid, "runner-authority-projector", "runner authority projector is unavailable", nil)
	}
	closeCtx, cancel := cleanupContext()
	closeErr := snapshot.RollbackAndReturnToRunner(closeCtx)
	cancel()
	if closeErr != nil {
		return ProjectionResult[AuthorityProjection]{}, mapRunnerDatabasePreflightError(closeErr, "runner-authority-snapshot-close", "runner authority snapshot could not return the dedicated session")
	}
	if projectionErr != nil {
		return ProjectionResult[AuthorityProjection]{}, mapRunnerDatabasePreflightError(projectionErr, "runner-authority-projection", "runner authority projection failed")
	}
	if err := validateRunnerAuthorityProjectionResult(result, metadata, contract, phase); err != nil {
		return ProjectionResult[AuthorityProjection]{}, err
	}
	return result, nil
}

func (runner *Runner) projectRunnerInitialPrecondition(ctx context.Context, session DatabaseSession, scope VerifiedSchemaBundleScope, plan StatementPlan) (ProjectionResult[CatalogStateProjection], error) {
	snapshot, err := BeginRunnerSessionProjectionSnapshot(ctx, session, AuthorityPhaseMigrationRole)
	if err != nil {
		return ProjectionResult[CatalogStateProjection]{}, mapRunnerDatabasePreflightError(err, "runner-precondition-snapshot", "initial catalog snapshot could not be opened")
	}
	if snapshot == nil {
		return ProjectionResult[CatalogStateProjection]{}, fail(CodeProjectionSnapshotInvalid, "runner-precondition-snapshot", "initial catalog snapshot is unavailable", nil)
	}
	metadata := snapshot.Metadata()
	factory := runner.projectionFactory
	if factory == nil {
		factory = pgRunnerAuthorityProjectorFactory{}
	}
	projector, factoryErr := factory.newRunnerAuthorityProjector(ctx, snapshot)
	condition := scope.BoundPrecondition()
	var result ProjectionResult[CatalogStateProjection]
	var projectionErr error
	if factoryErr == nil && projector != nil {
		result, projectionErr = projector.ProjectPrecondition(ctx, snapshot, scope, condition)
	} else if factoryErr != nil {
		projectionErr = mapRunnerDatabasePreflightError(factoryErr, "runner-precondition-projector", "initial catalog projector could not be constructed")
	} else {
		projectionErr = fail(CodeProjectionSnapshotInvalid, "runner-precondition-projector", "initial catalog projector is unavailable", nil)
	}
	closeCtx, cancel := cleanupContext()
	closeErr := snapshot.RollbackAndReturnToRunner(closeCtx)
	cancel()
	if closeErr != nil {
		return ProjectionResult[CatalogStateProjection]{}, mapRunnerDatabasePreflightError(closeErr, "runner-precondition-snapshot-close", "initial catalog snapshot could not return the dedicated session")
	}
	if projectionErr != nil {
		return ProjectionResult[CatalogStateProjection]{}, mapRunnerDatabasePreflightError(projectionErr, "runner-initial-precondition", "initial catalog projection failed")
	}
	if err := validateRunnerInitialPreconditionResult(result, metadata, scope, condition, plan.ExpectedTransition.CatalogBefore); err != nil {
		return ProjectionResult[CatalogStateProjection]{}, err
	}
	return result, nil
}

func validateRunnerAuthorityProjectionResult(result ProjectionResult[AuthorityProjection], snapshot SnapshotMetadata, contract VerifiedAuthorityContract, phase AuthorityPhase) error {
	if err := snapshot.validate(); err != nil || snapshot.AuthorityPhase != phase {
		return fail(CodeProjectionMetadataMismatch, "runner-authority-result", "snapshot metadata does not match the requested authority phase", nil)
	}
	if err := result.Metadata.validate(); err != nil || !runnerCanonicalEqual(result.Metadata.Snapshot, snapshot) || result.Metadata.VerifiedSubjectDigest != contract.SubjectDigest() || result.Metadata.QueryCount != runnerAuthorityProjectionQueryCount || result.Metadata.RowCount == 0 || result.Metadata.TotalBytes == 0 {
		return fail(CodeProjectionMetadataMismatch, "runner-authority-result", "authority projection metadata is incomplete or mismatched", nil)
	}
	expected, err := contract.ExpectedProjection(phase)
	if err != nil || result.Projection.Validate() != nil || !runnerCanonicalEqual(result.Projection, expected) || result.Projection.DatabaseName != snapshot.DatabaseName || result.Projection.SessionUser != snapshot.SessionUser || result.Projection.CurrentUser != snapshot.CurrentUser {
		return fail(CodeAuthorityDrift, "runner-authority-result", "authority projection differs from the verified phase binding", nil)
	}
	digest, err := digestProjectionWrapper(AuthorityProjectionDigestDomain, result.Projection)
	if err != nil || digest != result.Digest {
		return fail(CodeAuthorityDrift, "runner-authority-result", "authority projection digest differs from its exact body", nil)
	}
	return nil
}

func validateRunnerInitialPreconditionResult(result ProjectionResult[CatalogStateProjection], snapshot SnapshotMetadata, scope VerifiedSchemaBundleScope, condition CatalogPrecondition, expected CatalogStateDigestRef) error {
	return validateRunnerPreconditionResult(result, snapshot, scope, condition, expected, AuthorityPhaseMigrationRole, "runner-initial-precondition")
}

func validateRunnerPreconditionResult(result ProjectionResult[CatalogStateProjection], snapshot SnapshotMetadata, scope VerifiedSchemaBundleScope, condition CatalogPrecondition, expected CatalogStateDigestRef, phase AuthorityPhase, op string) error {
	if err := snapshot.validate(); err != nil || snapshot.AuthorityPhase != phase || phase != AuthorityPhaseMigrationRole && phase != AuthorityPhaseMigrationTransaction {
		return fail(CodeProjectionMetadataMismatch, op, "catalog snapshot metadata is invalid", nil)
	}
	if err := scope.validatePrecondition(condition); err != nil || expected.Validate() != nil || !equalProjectionScopes(scope.Scope(), expected.Scope) {
		return fail(CodeUntrusted, op, "catalog scope differs from the exact first statement plan", nil)
	}
	if err := result.Metadata.validate(); err != nil || !runnerCanonicalEqual(result.Metadata.Snapshot, snapshot) || result.Metadata.VerifiedSubjectDigest != scope.SubjectDigest() || result.Metadata.QueryCount != 1 || result.Metadata.RowCount == 0 || result.Metadata.TotalBytes == 0 || result.Metadata.Scope == nil || !equalProjectionScopes(*result.Metadata.Scope, expected.Scope) {
		return fail(CodeProjectionMetadataMismatch, op, "catalog projection metadata is incomplete or mismatched", nil)
	}
	if err := result.Projection.Validate(); err != nil {
		return fail(CodeCatalogDrift, op, "catalog projection is invalid", nil)
	}
	stateKind := "schema_absent"
	if result.Projection.Present != nil {
		stateKind = "schema_present"
	}
	digest, err := result.Projection.ComputeDigest()
	if err != nil || digest != result.Digest || digest != expected.Digest || stateKind != expected.StateKind {
		return fail(CodeCatalogDrift, op, "catalog state differs from the first statement predecessor", nil)
	}
	actualKey, err := canonicalContractKey(result.Projection)
	if err != nil {
		return fail(CodeCatalogDrift, op, "catalog state cannot be canonicalized", nil)
	}
	matched := false
	for _, accepted := range condition.AcceptedStates {
		key, keyErr := canonicalContractKey(accepted)
		if keyErr == nil && key == actualKey {
			matched = true
			break
		}
	}
	if !matched {
		return fail(CodeCatalogDrift, op, "catalog state is outside the verified predecessor set", nil)
	}
	return nil
}

func closeRunnerDatabasePreflight(session DatabaseSession, key int64, locked bool, primary error) error {
	if session == nil {
		return primary
	}
	var unlockErr error
	if locked {
		cleanupCtx, cancel := cleanupContext()
		unlockErr = session.UnlockAndReset(cleanupCtx, key)
		cancel()
	}
	cleanupCtx, cancel := cleanupContext()
	closeErr := session.Close(cleanupCtx)
	cancel()
	if closeErr != nil {
		return mapRunnerDatabasePreflightError(closeErr, "runner-database-close", "dedicated database session close could not be proven")
	}
	if primary != nil {
		return primary
	}
	if unlockErr != nil {
		return mapRunnerDatabasePreflightError(unlockErr, "runner-advisory-unlock", "signed advisory lock release could not be proven")
	}
	return nil
}

func mapRunnerDatabasePreflightError(err error, op, message string) error {
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
	return fail(CodeTransactionBoundary, op, message, nil)
}

var _ runnerAuthorityProjectorFactory = pgRunnerAuthorityProjectorFactory{}
