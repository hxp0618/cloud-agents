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
}

type pgRunnerAuthorityProjectorFactory struct{}

func (pgRunnerAuthorityProjectorFactory) newRunnerAuthorityProjector(ctx context.Context, snapshot ProjectionSnapshot) (runnerAuthorityProjector, error) {
	return NewPGProjector(ctx, snapshot)
}

func (pgRunnerAuthorityProjectorFactory) runnerAuthorityProjectorFactorySealed() {}

func (runner *Runner) runDatabaseAuthorityPreflight(ctx context.Context, dsn string, bundle *RuntimeBundle, evidence EvidenceSession, candidate OwnedCurrentCandidate) error {
	bindings, err := runnerCurrentProjectionBindings(evidence, candidate)
	if err != nil {
		return err
	}
	if runner.Connector == nil {
		return fail(CodeProjectionNotImplemented, "runner-database-connector", "evidence session is active but no database connector is configured", nil)
	}
	if bundle == nil || dsn == "" || bindings.schemaBundleDigest != bundle.Manifest.SchemaBundleDigest {
		return fail(CodeUntrusted, "runner-database-preflight", "database preflight inputs differ from the active evidence generation", nil)
	}
	key, err := bundle.Manifest.SchemaBundle.AdvisoryLock.Key()
	if err != nil {
		return err
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
		return closeRunnerDatabasePreflight(session, 0, false, primary)
	}
	locked := false
	failClosed := func(primary error) error {
		return closeRunnerDatabasePreflight(session, key, locked, primary)
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
	return failClosed(nil)
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
