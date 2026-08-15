package migration

import (
	"context"
	"errors"
	"io"
	"net"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

type ArtifactSource interface {
	// Read is invoked only after trust verification. Implementations must return
	// no more than 64 MiB and bind the bytes to expectedOuterDigest.
	Read(context.Context, Digest) ([]byte, error)
}

type RunRequest struct {
	Candidate CandidateEnvelope
	Artifact  ArtifactSource
	TargetDSN string
}

type RunResult struct {
	SchemaBundleDigest Digest
	ManifestDigest     Digest
	FinalHead          string
	Applied            []string
	AmbiguousRecovered []string
}

type RunnerState string

const (
	StateVerifyTrust RunnerState = "verify_trust"
	StateLoadBundle  RunnerState = "load_bundle"
	StateConnect     RunnerState = "connect"
	StateLocked      RunnerState = "locked"
	StateMigrate     RunnerState = "migrate"
	StateReconcile   RunnerState = "reconcile_ambiguous_commit"
	StateComplete    RunnerState = "complete"
)

type StateObserver interface{ Transition(RunnerState) }

type Runner struct {
	Trust        TrustVerifier
	Evidence     EvidenceSink
	Connector    DatabaseConnector
	Ledger       LedgerStore
	Authority    AuthorityValidator
	Catalog      CatalogValidator
	Intermediate IntermediateValidator
	Classifier   StatementClassifier // nil builds the exact signed descriptor classifier
	Observer     StateObserver

	projectionFactory runnerAuthorityProjectorFactory
}

type preparedSession struct {
	session   DatabaseSession
	major     int
	key       int64
	authority AuthorityProjection
}

type databaseState struct {
	ledger  *LedgerSnapshot
	catalog CatalogProjection
}

// Run is the public production gate. It admits one evidence session, proves the
// connected-session, migration-role, and migration-transaction authority
// projections on the same dedicated database connection. It seals the exact
// header-only current dispatch, repeats its predecessor inside one borrowed
// SERIALIZABLE/READ WRITE snapshot, and then unconditionally rolls back. No
// statement intent, migration SQL, ledger row, evidence record, or commit is
// reachable from this slice.
func (runner *Runner) Run(ctx context.Context, request RunRequest) (RunResult, error) {
	if err := runner.validateAdmissionDependencies(request); err != nil {
		return RunResult{}, err
	}
	runner.transition(StateVerifyTrust)
	decision, recoveryArtifactBytes, err := verifyRunnerCurrentEvidence(ctx, runner.Trust, request.Candidate)
	if err != nil {
		return RunResult{}, err
	}
	if err := decision.validate(); err != nil {
		return RunResult{}, err
	}
	runner.transition(StateLoadBundle)
	raw, err := request.Artifact.Read(ctx, decision.OuterArtifactDigest())
	if err != nil {
		return RunResult{}, fail(CodeInvalidArtifact, "read", "cannot read verified runtime artifact", err)
	}
	bundle, err := LoadRuntimeBundle(raw, decision)
	if err != nil {
		return RunResult{}, err
	}
	bindings, err := decision.runnerProjectionBindings()
	if err != nil {
		return RunResult{}, err
	}
	plans, err := buildExactStatementPlans(bundle, bindings, time.Now())
	if err != nil {
		return RunResult{}, err
	}
	for _, plan := range plans {
		if _, err := plan.exactSQLBytes(); err != nil {
			return RunResult{}, err
		}
	}
	current, recoveryArtifact, err := bindVerifierOwnedDecision(runner.Trust, decision, bindings.runnerProjectionDecisionDigest, recoveryArtifactBytes)
	if err != nil {
		return RunResult{}, err
	}
	verifiedRun, runtimeArtifact, candidate, err := bindVerifiedEvidenceRun(decision, bindings, current, raw, recoveryArtifact)
	if err != nil {
		return RunResult{}, err
	}
	if runner.Evidence == nil {
		if cleanupErr := closeRunnerEvidenceOwnership(nil, candidate); cleanupErr != nil {
			return RunResult{}, cleanupErr
		}
		return RunResult{}, fail(CodeProjectionNotImplemented, "runner-evidence-sink", "verified evidence candidate admitted but no evidence sink is configured", nil)
	}
	session, snapshot, err := openRunnerEvidenceSession(ctx, runner.Evidence, verifiedRun, runtimeArtifact, candidate)
	if err != nil {
		return RunResult{}, err
	}
	prepared, preflightErr := runner.prepareCurrentDatabaseSession(ctx, request.TargetDSN, bundle, plans, session, snapshot, candidate)
	if preflightErr == nil {
		transaction, transactionErr := runner.prepareCurrentTransaction(ctx, prepared, bundle, plans)
		preflightErr = transactionErr
		if preflightErr == nil {
			preflightErr = closeRunnerPreparedCurrentTransaction(transaction, nil)
			if preflightErr == nil {
				preflightErr = fail(CodeProjectionNotImplemented, "runner-statement-intent", "transaction-wide current preflight is sealed but statement intent is not implemented", nil)
			}
		}
	} else {
		preflightErr = closeRunnerPreparedCurrentSession(prepared, preflightErr)
	}
	if cleanupErr := closeRunnerEvidenceOwnership(session, candidate); cleanupErr != nil {
		return RunResult{}, cleanupErr
	}
	return RunResult{}, preflightErr
}

func verifyRunnerCurrentEvidence(ctx context.Context, verifier TrustVerifier, candidate CandidateEnvelope) (VerifiedTrustDecision, []byte, error) {
	evidenceVerifier, ok := verifier.(currentEvidenceTrustVerifier)
	if !ok {
		return VerifiedTrustDecision{}, nil, fail(CodeEvidenceRecoveryRequired, "runner-decision-recovery", "trust verifier cannot mint current recovery authority", nil)
	}
	if len(candidate.Subject) > maxCandidateEnvelopeComponentBytes || len(candidate.DetachedEnvelope) > maxCandidateEnvelopeComponentBytes {
		return VerifiedTrustDecision{}, nil, fail(CodeUntrusted, "trust", "candidate envelope exceeds the verification bound", nil)
	}
	ownedCandidate := candidate
	ownedCandidate.Subject = append([]byte(nil), candidate.Subject...)
	ownedCandidate.DetachedEnvelope = append([]byte(nil), candidate.DetachedEnvelope...)
	decision, artifact, err := evidenceVerifier.verifyCurrentEvidence(ctx, ownedCandidate)
	if err != nil {
		return VerifiedTrustDecision{}, nil, fail(CodeUntrusted, "trust", "candidate verification failed", nil)
	}
	if uint64(len(artifact)) > maxDecisionRecoveryArtifactBytes {
		return VerifiedTrustDecision{}, nil, fail(CodeEvidenceRecoveryRequired, "runner-decision-recovery", "current recovery artifact exceeds the verification bound", nil)
	}
	return decision, append([]byte(nil), artifact...), nil
}

// runLegacyCharacterization preserves the ADR-0009 state-machine tests while
// impl-3 is assembled. Production Run has no call edge to this function.
func (runner *Runner) runLegacyCharacterization(ctx context.Context, request RunRequest) (RunResult, error) {
	if err := runner.validateLegacyDependencies(request); err != nil {
		return RunResult{}, err
	}
	runner.transition(StateVerifyTrust)
	decision, err := runner.Trust.Verify(ctx, request.Candidate)
	if err != nil {
		return RunResult{}, fail(CodeUntrusted, "trust", "candidate verification failed", err)
	}
	if err := decision.validate(); err != nil {
		return RunResult{}, err
	}
	runner.transition(StateLoadBundle)
	raw, err := request.Artifact.Read(ctx, decision.OuterArtifactDigest())
	if err != nil {
		return RunResult{}, fail(CodeInvalidArtifact, "read", "cannot read verified runtime artifact", err)
	}
	bundle, err := LoadRuntimeBundle(raw, decision)
	if err != nil {
		return RunResult{}, err
	}
	classifier := runner.Classifier
	if classifier == nil {
		classifier, err = NewDescriptorClassifier(bundle)
		if err != nil {
			return RunResult{}, err
		}
	}
	if err := validateExecutionClosure(bundle, classifier); err != nil {
		return RunResult{}, err
	}
	key, err := bundle.Manifest.SchemaBundle.AdvisoryLock.Key()
	if err != nil {
		return RunResult{}, err
	}
	prepared, err := runner.openPreparedWithRetries(ctx, request, decision, bundle, key, false)
	if err != nil {
		return RunResult{}, err
	}
	defer func() {
		if prepared == nil || prepared.session == nil {
			return
		}
		closeCtx, cancel := cleanupContext()
		defer cancel()
		_ = prepared.session.Close(closeCtx)
	}()
	state, err := runner.readDatabaseState(ctx, prepared, bundle)
	if err != nil {
		runner.cleanup(prepared)
		return RunResult{}, err
	}
	result := RunResult{SchemaBundleDigest: bundle.Manifest.SchemaBundleDigest, ManifestDigest: bundle.Manifest.ManifestDigest, FinalHead: state.ledger.Head}
	entries := bundle.Manifest.SchemaBundle.Migrations
	for len(state.ledger.Rows) < len(entries) {
		entryIndex := len(state.ledger.Rows)
		entry := entries[entryIndex]
		completed := false
		for attempt := uint64(1); attempt <= bundle.Manifest.ExecutionPolicy.MaxAttempts; attempt++ {
			runner.transition(StateMigrate)
			committed, ambiguous, applyErr := runner.applyEntry(ctx, prepared, bundle, classifier, entry, state)
			if applyErr == nil && committed {
				result.Applied = append(result.Applied, entry.ID)
				state, err = runner.readDatabaseState(ctx, prepared, bundle)
				if err != nil {
					runner.cleanup(prepared)
					return RunResult{}, err
				}
				completed = true
				break
			}
			if ambiguous {
				runner.transition(StateReconcile)
				runner.closeSession(prepared)
				prepared, state, committed, err = runner.reconcileAmbiguous(ctx, request, decision, bundle, key, entryIndex)
				if err != nil {
					return RunResult{}, err
				}
				if committed {
					result.AmbiguousRecovered = append(result.AmbiguousRecovered, entry.ID)
					completed = true
					break
				}
				if attempt == bundle.Manifest.ExecutionPolicy.MaxAttempts {
					runner.cleanup(prepared)
					return RunResult{}, fail(CodeAmbiguousCommit, entry.ID, "bounded ambiguous-commit attempts were exhausted", applyErr)
				}
				continue
			}
			retry := classifyRetry(applyErr)
			if retry == retryNone || attempt == bundle.Manifest.ExecutionPolicy.MaxAttempts {
				runner.cleanup(prepared)
				return RunResult{}, applyErr
			}
			if retry == retryConnectionLoss {
				runner.closeSession(prepared)
				prepared, err = runner.openPreparedWithRetries(ctx, request, decision, bundle, key, true)
				if err != nil {
					return RunResult{}, err
				}
			}
			state, err = runner.readDatabaseState(ctx, prepared, bundle)
			if err != nil {
				runner.cleanup(prepared)
				return RunResult{}, err
			}
			if len(state.ledger.Rows) != entryIndex {
				runner.cleanup(prepared)
				return RunResult{}, fail(CodeInvalidLedger, entry.ID, "retry preflight is not the exact predecessor state", nil)
			}
		}
		if !completed {
			runner.cleanup(prepared)
			return RunResult{}, fail(CodeTransactionBoundary, entry.ID, "bounded migration attempts were exhausted", nil)
		}
		result.FinalHead = state.ledger.Head
	}
	if result.FinalHead != bundle.Manifest.SchemaBundle.SchemaHead {
		runner.cleanup(prepared)
		return RunResult{}, fail(CodeInvalidLedger, "complete", "final ledger head is not the signed schema head", nil)
	}
	if err := prepared.session.UnlockAndReset(ctx, key); err != nil {
		closeCtx, cancel := cleanupContext()
		_ = prepared.session.Close(closeCtx)
		cancel()
		return RunResult{}, err
	}
	if err := prepared.session.Close(ctx); err != nil {
		return RunResult{}, fail(CodeTransactionBoundary, "close", "dedicated connection close failed", err)
	}
	runner.transition(StateComplete)
	return result, nil
}

func (runner *Runner) openPreparedWithRetries(ctx context.Context, request RunRequest, expected VerifiedTrustDecision, bundle *RuntimeBundle, key int64, reverifyFirst bool) (*preparedSession, error) {
	var lastErr error
	for attempt := uint64(1); attempt <= bundle.Manifest.ExecutionPolicy.MaxAttempts; attempt++ {
		if reverifyFirst || attempt > 1 {
			if err := runner.reverifyTrust(ctx, request.Candidate, expected); err != nil {
				return nil, err
			}
		}
		prepared, err := runner.openPrepared(ctx, request.TargetDSN, bundle, key)
		if err == nil {
			return prepared, nil
		}
		lastErr = err
		if classifyRetry(err) != retryConnectionLoss {
			return nil, err
		}
	}
	return nil, fail(CodeTransactionBoundary, "connect", "bounded connection attempts were exhausted", lastErr)
}

func (runner *Runner) reverifyTrust(ctx context.Context, candidate CandidateEnvelope, expected VerifiedTrustDecision) error {
	runner.transition(StateVerifyTrust)
	decision, err := runner.Trust.Verify(ctx, candidate)
	if err != nil {
		return fail(CodeUntrusted, "trust-reverify", "candidate re-verification failed", err)
	}
	if err := decision.validate(); err != nil {
		return err
	}
	if !decision.exactlyMatches(expected) {
		return fail(CodeUntrusted, "trust-reverify", "verified trust decision changed before reconnect", nil)
	}
	return nil
}

func validateExecutionClosure(bundle *RuntimeBundle, classifier StatementClassifier) error {
	for _, entry := range bundle.Manifest.SchemaBundle.Migrations {
		statements, err := SplitPostgreSQLStatements(bundle.Files[entry.SQLArtifact.Path])
		if err != nil {
			return err
		}
		for _, statement := range statements {
			if _, err := classifier.Classify(entry, statement); err != nil {
				return err
			}
		}
	}
	return nil
}

func (runner *Runner) validateAdmissionDependencies(request RunRequest) error {
	if runner.Trust == nil || request.Artifact == nil || request.TargetDSN == "" {
		return fail(CodeUnsupported, "runner", "trust verifier, artifact source, or target is missing", nil)
	}
	return nil
}

func (runner *Runner) validateLegacyDependencies(request RunRequest) error {
	if runner.Trust == nil || runner.Connector == nil || runner.Ledger == nil || runner.Authority == nil || runner.Catalog == nil || runner.Intermediate == nil || request.Artifact == nil || request.TargetDSN == "" {
		return fail(CodeUnsupported, "runner", "runner dependencies, artifact source, or target are missing", nil)
	}
	return nil
}

func (runner *Runner) openPrepared(ctx context.Context, dsn string, bundle *RuntimeBundle, key int64) (*preparedSession, error) {
	runner.transition(StateConnect)
	session, err := runner.Connector.Connect(ctx, dsn)
	if err != nil {
		return nil, err
	}
	failAndClose := func(err error) (*preparedSession, error) {
		closeCtx, cancel := cleanupContext()
		defer cancel()
		_ = session.Close(closeCtx)
		return nil, err
	}
	major, err := session.ServerMajor(ctx)
	if err != nil {
		return failAndClose(err)
	}
	policy := bundle.Manifest.ExecutionPolicy
	if major < int(policy.PostgresMajorMin) || major > int(policy.PostgresMajorMax) {
		return failAndClose(fail(CodeUnsupported, "server-version", "PostgreSQL major is outside the signed range", nil))
	}
	authorityRaw := bundle.Files[policy.AuthorityContract.Path]
	if _, err := runner.Authority.ValidateAuthority(ctx, session.Queryer(), major, authorityRaw); err != nil {
		return failAndClose(err)
	}
	if err := session.SetRoleAndSettings(ctx, policy); err != nil {
		return failAndClose(err)
	}
	if err := session.AcquireAdvisoryLock(ctx, key); err != nil {
		return failAndClose(err)
	}
	boundary, err := session.Boundary(ctx, key)
	if err != nil || boundary.TxStatus != 'I' || boundary.CurrentUser != MigrationOwnerRole || !boundary.LockHeld {
		return failAndClose(fail(CodeTransactionBoundary, "session", "prepared session boundary is invalid", err))
	}
	authority, err := runner.Authority.ValidateAuthority(ctx, session.Queryer(), major, authorityRaw)
	if err != nil {
		return failAndClose(err)
	}
	runner.transition(StateLocked)
	return &preparedSession{session: session, major: major, key: key, authority: authority}, nil
}

func (runner *Runner) readDatabaseState(ctx context.Context, prepared *preparedSession, bundle *RuntimeBundle) (*databaseState, error) {
	boundary, err := prepared.session.Boundary(ctx, prepared.key)
	if err != nil || boundary.TxStatus != 'I' || boundary.CurrentUser != MigrationOwnerRole || !boundary.LockHeld {
		return nil, fail(CodeTransactionBoundary, "preflight", "session boundary changed before ledger read", err)
	}
	rows, err := runner.Ledger.Read(ctx, prepared.session.Queryer())
	if err != nil {
		return nil, err
	}
	ledger, err := ValidateLedger(rows, bundle.Lineage)
	if err != nil {
		return nil, err
	}
	var catalog CatalogProjection
	if len(rows) == 0 {
		catalog, err = runner.Catalog.ValidatePredecessor(ctx, prepared.session.Queryer(), prepared.major, bundle.Manifest.SchemaBundle.Migrations[0].PredecessorCatalogContract, bundle.Files)
	} else {
		entry := bundle.Manifest.SchemaBundle.Migrations[len(rows)-1]
		catalog, err = runner.Catalog.ValidateCatalog(ctx, prepared.session.Queryer(), prepared.major, bundle.Files[entry.CatalogContract.Path], entry.ID)
	}
	if err != nil {
		return nil, err
	}
	authority, err := runner.Authority.ValidateAuthority(ctx, prepared.session.Queryer(), prepared.major, bundle.Files[bundle.Manifest.ExecutionPolicy.AuthorityContract.Path])
	if err != nil || !equalAuthorityProjection(prepared.authority, authority) {
		return nil, fail(CodeAuthorityDrift, "preflight", "authority projection changed while lock was held", err)
	}
	return &databaseState{ledger: ledger, catalog: catalog}, nil
}

func (runner *Runner) applyEntry(ctx context.Context, prepared *preparedSession, bundle *RuntimeBundle, classifier StatementClassifier, entry MigrationEntry, state *databaseState) (committed bool, ambiguous bool, err error) {
	transaction, err := prepared.session.BeginMigration(ctx)
	if err != nil {
		return false, false, err
	}
	finished := false
	defer func() {
		if !finished {
			cleanupCtx, cancel := cleanupContext()
			defer cancel()
			_ = transaction.Rollback(cleanupCtx)
		}
	}()
	statements, err := SplitPostgreSQLStatements(bundle.Files[entry.SQLArtifact.Path])
	if err != nil {
		return false, false, err
	}
	for _, statement := range statements {
		plan, err := classifier.Classify(entry, statement)
		if err != nil {
			return false, false, err
		}
		if err := transaction.ExecuteStatement(ctx, statement.Raw); err != nil {
			return false, false, err
		}
		boundary, err := transaction.Boundary(ctx, prepared.key)
		if err != nil || boundary.TxStatus != 'T' || boundary.CurrentUser != MigrationOwnerRole || !boundary.LockHeld {
			return false, false, fail(CodeTransactionBoundary, entry.ID, "statement escaped the transaction, role, or lock boundary", err)
		}
		authority, err := runner.Authority.ValidateAuthority(ctx, transaction, prepared.major, bundle.Files[bundle.Manifest.ExecutionPolicy.AuthorityContract.Path])
		if err != nil || !equalAuthorityProjection(prepared.authority, authority) {
			return false, false, fail(CodeAuthorityDrift, entry.ID, "authority changed after a migration statement", err)
		}
		if err := runner.Intermediate.ValidateIntermediate(ctx, transaction, prepared.major, entry, statement, plan, state.catalog); err != nil {
			return false, false, err
		}
	}
	if _, err := runner.Catalog.ValidateCatalog(ctx, transaction, prepared.major, bundle.Files[entry.CatalogContract.Path], entry.ID); err != nil {
		return false, false, err
	}
	authority, err := runner.Authority.ValidateAuthority(ctx, transaction, prepared.major, bundle.Files[bundle.Manifest.ExecutionPolicy.AuthorityContract.Path])
	if err != nil || !equalAuthorityProjection(prepared.authority, authority) {
		return false, false, fail(CodeAuthorityDrift, entry.ID, "authority changed before ledger insert", err)
	}
	if err := runner.Ledger.Insert(ctx, transaction, entry, bundle.Manifest.SchemaBundleDigest); err != nil {
		return false, false, err
	}
	if err := transaction.Commit(ctx); err != nil {
		finished = true
		retry := classifyRetry(err)
		if retry == retrySerialization || retry == retryDeadlock {
			return false, false, fail(CodeTransactionBoundary, entry.ID, "commit confirmed transaction abort", err)
		}
		if retry == retryConnectionLoss || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || pgconn.Timeout(err) {
			return false, true, fail(CodeAmbiguousCommit, entry.ID, "commit acknowledgement was not trustworthy", err)
		}
		return false, false, fail(CodeTransactionBoundary, entry.ID, "commit failed with a terminal database error", err)
	}
	finished = true
	return true, false, nil
}

func (runner *Runner) reconcileAmbiguous(ctx context.Context, request RunRequest, expected VerifiedTrustDecision, bundle *RuntimeBundle, key int64, entryIndex int) (*preparedSession, *databaseState, bool, error) {
	// Signature/epoch/expiry/revocation are re-evaluated by TrustVerifier before
	// any reconnect. Exact decision comparison prevents a valid-but-different
	// candidate from being substituted during ambiguous commit recovery.
	prepared, err := runner.openPreparedWithRetries(ctx, request, expected, bundle, key, true)
	if err != nil {
		return nil, nil, false, err
	}
	state, err := runner.readDatabaseState(ctx, prepared, bundle)
	if err != nil {
		runner.cleanup(prepared)
		return nil, nil, false, err
	}
	if len(state.ledger.Rows) > entryIndex {
		return prepared, state, true, nil
	}
	if len(state.ledger.Rows) == entryIndex {
		return prepared, state, false, nil
	}
	runner.cleanup(prepared)
	return nil, nil, false, fail(CodeAmbiguousCommit, bundle.Manifest.SchemaBundle.Migrations[entryIndex].ID, "ledger moved to an impossible position", nil)
}

type retryClassification uint8

const (
	retryNone retryClassification = iota
	retrySerialization
	retryDeadlock
	retryConnectionLoss
)

func classifyRetry(err error) retryClassification {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return retryNone
	}
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) {
		switch postgresError.Code {
		case "40001":
			return retrySerialization
		case "40P01":
			return retryDeadlock
		}
		if len(postgresError.Code) >= 2 && postgresError.Code[:2] == "08" {
			return retryConnectionLoss
		}
		return retryNone
	}
	var connectError *pgconn.ConnectError
	if errors.As(err, &connectError) || pgconn.SafeToRetry(err) || errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return retryConnectionLoss
	}
	var networkError net.Error
	if errors.As(err, &networkError) {
		return retryConnectionLoss
	}
	return retryNone
}

func (runner *Runner) cleanup(prepared *preparedSession) {
	if prepared == nil || prepared.session == nil {
		return
	}
	ctx, cancel := cleanupContext()
	defer cancel()
	_ = prepared.session.UnlockAndReset(ctx, prepared.key)
	_ = prepared.session.Close(ctx)
}

func (runner *Runner) closeSession(prepared *preparedSession) {
	if prepared == nil || prepared.session == nil {
		return
	}
	ctx, cancel := cleanupContext()
	defer cancel()
	_ = prepared.session.Close(ctx)
}

func cleanupContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 5*time.Second)
}

func (runner *Runner) transition(state RunnerState) {
	if runner.Observer != nil {
		runner.Observer.Transition(state)
	}
}
