package migration

import (
	"context"
	"crypto/sha256"
	"time"
)

const runnerLedgerReconciliationFactsDigestDomain = "cloud-agents/runner-ledger-reconciliation/observation/v1"

type runnerLedgerReconciliationOutcome string

const (
	runnerLedgerReconciliationExactCommitted runnerLedgerReconciliationOutcome = "exact_committed"
	runnerLedgerReconciliationExactPending   runnerLedgerReconciliationOutcome = "exact_pending"
	runnerLedgerReconciliationDivergent      runnerLedgerReconciliationOutcome = "divergent"
)

func (runner *Runner) projectRunnerLedgerReconciliationPreflight(ctx context.Context, dsn string, bundle *RuntimeBundle, plans []StatementPlan, evidence EvidenceSession, candidate OwnedCurrentCandidate, hint *runnerLedgerReconciliationHint) (*runnerLedgerCatalogPreflight, error) {
	if hint == nil || hint.canonical == ([32]byte{}) || hint.canonical != runnerLedgerReconciliationHintDigest(hint) {
		return nil, fail(CodeEvidenceRecoveryRequired, "runner-ledger-reconciliation-preflight", "reconciliation hint is unavailable", nil)
	}
	observation, err := runner.openRunnerLockedLedgerCatalogObservationWithReconciliation(ctx, dsn, bundle, plans, evidence, candidate, hint)
	if err != nil {
		return nil, err
	}
	if err := observation.close(nil); err != nil {
		return nil, err
	}
	if err := contextAdmissionError(ctx); err != nil {
		return nil, err
	}
	return observation.bind()
}

// runnerLedgerReconciliationHint is ordinary recovery-owned data used only to
// select a broader read-only observation. It is never accepted as append
// authority; the evidence claim binder later cross-checks the complete replay.
type runnerLedgerReconciliationHint struct {
	state                          RecoveryState
	action                         RecoveryAction
	migrationID                    string
	attemptIndex                   uint32
	targetIndex                    uint32
	commit                         CommitIntent
	commitRecordDigest             Digest
	commitBodyDigest               Digest
	pendingCatalogDigest           Digest
	committedCatalogDigest         Digest
	unresolvedTerminalDigest       *Digest
	unresolvedTerminalRecordDigest *Digest
	canonical                      [32]byte
}

type runnerLedgerReconciliationFacts struct {
	outcome                        runnerLedgerReconciliationOutcome
	state                          RecoveryState
	action                         RecoveryAction
	migrationID                    string
	attemptIndex                   uint32
	targetIndex                    uint32
	commitRecordDigest             Digest
	commitBodyDigest               Digest
	expectedLedgerRow              CommitIntentLedgerRow
	pendingCatalogDigest           Digest
	pendingCatalogScope            ProjectionScope
	committedCatalogDigest         Digest
	unresolvedTerminalDigest       *Digest
	unresolvedTerminalRecordDigest *Digest
	predecessorProjectionSubject   Digest
	observedProjectionSubject      Digest
	catalogProjectionObserved      bool
	catalogProjectionReportedDrift bool
	subjectDigest                  Digest
}

type runnerLedgerReconciliationFactsWire struct {
	Outcome                        runnerLedgerReconciliationOutcome `json:"outcome"`
	State                          RecoveryState                     `json:"state"`
	Action                         RecoveryAction                    `json:"action"`
	MigrationID                    string                            `json:"migration_id"`
	AttemptIndex                   uint32                            `json:"attempt_index"`
	TargetIndex                    uint32                            `json:"target_index"`
	CommitRecordDigest             Digest                            `json:"commit_record_digest"`
	CommitBodyDigest               Digest                            `json:"commit_body_digest"`
	ExpectedLedgerRow              CommitIntentLedgerRow             `json:"expected_ledger_row"`
	PendingCatalogDigest           Digest                            `json:"pending_catalog_digest"`
	PendingCatalogScope            ProjectionScope                   `json:"pending_catalog_scope"`
	CommittedCatalogDigest         Digest                            `json:"committed_catalog_digest"`
	UnresolvedTerminalDigest       *Digest                           `json:"unresolved_terminal_digest"`
	UnresolvedTerminalRecordDigest *Digest                           `json:"unresolved_terminal_record_digest"`
	PredecessorProjectionSubject   Digest                            `json:"predecessor_projection_subject"`
	ObservedProjectionSubject      Digest                            `json:"observed_projection_subject"`
	CatalogProjectionObserved      bool                              `json:"catalog_projection_observed"`
	CatalogProjectionReportedDrift bool                              `json:"catalog_projection_reported_drift"`
}

func runnerLedgerReconciliationHintFromSnapshot(snapshot *RecoverySnapshot) (*runnerLedgerReconciliationHint, error) {
	if snapshot == nil || snapshot.nextPermittedAction != RecoveryReconcileCommit ||
		(snapshot.state != RecoveryDanglingCommitIntent && snapshot.state != RecoveryAmbiguousUnresolved) {
		return nil, nil
	}
	if !validRecoverySnapshotForJournal(snapshot, snapshot.generation, snapshot.cursor) || snapshot.migrationID == nil ||
		snapshot.attemptIndex == nil || snapshot.commitIntent == nil || snapshot.lastCommitIntentRecordDigest == nil ||
		snapshot.lastIntermediateEvidence == nil || snapshot.lastIntermediateEvidence.value.PreledgerCatalogResult == nil {
		return nil, fail(CodeEvidenceRecoveryRequired, "runner-ledger-reconciliation-hint", "recovery commit boundary is unavailable", nil)
	}
	commit := cloneProjectionValue(snapshot.commitIntent.value)
	intermediate := snapshot.lastIntermediateEvidence.value
	if commit.Validate() != nil || commit.MigrationID != *snapshot.migrationID || commit.AttemptIndex != *snapshot.attemptIndex ||
		commit.ExpectedLedgerHead != commit.MigrationID ||
		commit.LastIntermediateStateDigest != intermediate.State.IntermediateStateDigest ||
		intermediate.PreledgerCatalogResult.Digest.Validate() != nil || snapshot.lastCommitIntentRecordDigest.Validate() != nil {
		return nil, fail(CodeEvidenceJournalCorrupt, "runner-ledger-reconciliation-hint", "recovery commit boundary is contradictory", nil)
	}
	// The immutable v1 writer profiles are registered only for the generated
	// partial_retry_or_recovery pair, which requires a non-empty durable
	// predecessor prefix. A valid first-entry commit remains unsupported and
	// must fall back to the existing close-only recovery path; it is not corrupt.
	if commit.ExpectedLedgerLength == 1 {
		return nil, nil
	}
	commitCanonical, err := canonicalContractKey(commit)
	if err != nil || commitCanonical == "" {
		return nil, fail(CodeEvidenceJournalCorrupt, "runner-ledger-reconciliation-hint", "commit intent is not canonical", nil)
	}
	hint := &runnerLedgerReconciliationHint{
		state: snapshot.state, action: snapshot.nextPermittedAction, migrationID: commit.MigrationID,
		attemptIndex: commit.AttemptIndex, targetIndex: commit.ExpectedLedgerLength - 1,
		commit: commit, commitRecordDigest: *snapshot.lastCommitIntentRecordDigest,
		commitBodyDigest:       DigestBytes([]byte("cloud-agents/runner-ledger-reconciliation/commit-intent/v1\x00" + commitCanonical)),
		pendingCatalogDigest:   commit.AttemptPredecessorCatalogDigest,
		committedCatalogDigest: intermediate.PreledgerCatalogResult.Digest,
	}
	if snapshot.state == RecoveryDanglingCommitIntent {
		if snapshot.tailDigest != hint.commitRecordDigest || snapshot.lastTerminal != nil || snapshot.lastResolution != nil {
			return nil, fail(CodeEvidenceJournalCorrupt, "runner-ledger-reconciliation-hint", "dangling commit contains a later durable record", nil)
		}
	} else {
		if snapshot.lastTerminal == nil || snapshot.lastTerminalDigest == nil || snapshot.lastResolution != nil ||
			snapshot.lastResolutionDigest != nil || snapshot.lastTerminal.recordDigest != snapshot.tailDigest {
			return nil, fail(CodeEvidenceJournalCorrupt, "runner-ledger-reconciliation-hint", "unresolved terminal boundary is unavailable", nil)
		}
		terminal := snapshot.lastTerminal.value
		if terminal.Validate() != nil || terminal.Outcome != "ambiguous_unresolved" || terminal.MigrationID != commit.MigrationID ||
			terminal.AttemptIndex != commit.AttemptIndex || terminal.TerminalDigest != *snapshot.lastTerminalDigest {
			return nil, fail(CodeEvidenceJournalCorrupt, "runner-ledger-reconciliation-hint", "unresolved terminal differs from the commit boundary", nil)
		}
		hint.unresolvedTerminalDigest = cloneDigestPointer(snapshot.lastTerminalDigest)
		hint.unresolvedTerminalRecordDigest = digestPointer(snapshot.lastTerminal.recordDigest)
	}
	hint.canonical = runnerLedgerReconciliationHintDigest(hint)
	if hint.canonical == ([32]byte{}) {
		return nil, fail(CodeEvidenceJournalCorrupt, "runner-ledger-reconciliation-hint", "reconciliation hint could not be identified", nil)
	}
	return hint, nil
}

func runnerLedgerReconciliationHintDigest(hint *runnerLedgerReconciliationHint) [32]byte {
	if hint == nil || hint.action != RecoveryReconcileCommit ||
		(hint.state != RecoveryDanglingCommitIntent && hint.state != RecoveryAmbiguousUnresolved) ||
		!migrationIDPattern.MatchString(hint.migrationID) || hint.attemptIndex == 0 || hint.targetIndex == 0 ||
		hint.commit.Validate() != nil || hint.commit.MigrationID != hint.migrationID || hint.commit.AttemptIndex != hint.attemptIndex ||
		hint.commit.ExpectedLedgerLength != hint.targetIndex+1 || hint.commitRecordDigest.Validate() != nil ||
		hint.commitBodyDigest.Validate() != nil || hint.pendingCatalogDigest.Validate() != nil || hint.committedCatalogDigest.Validate() != nil ||
		(hint.state == RecoveryAmbiguousUnresolved) != (hint.unresolvedTerminalDigest != nil) ||
		(hint.unresolvedTerminalDigest == nil) != (hint.unresolvedTerminalRecordDigest == nil) {
		return [32]byte{}
	}
	commitCanonical, err := canonicalContractKey(hint.commit)
	if err != nil || commitCanonical == "" ||
		hint.commitBodyDigest != DigestBytes([]byte("cloud-agents/runner-ledger-reconciliation/commit-intent/v1\x00"+commitCanonical)) {
		return [32]byte{}
	}
	h := sha256.New()
	h.Write([]byte("cloud-agents/runner-ledger-reconciliation/hint/v1\x00"))
	for _, value := range []string{string(hint.state), string(hint.action), hint.migrationID, hint.commitRecordDigest.String(), hint.commitBodyDigest.String(), hint.pendingCatalogDigest.String(), hint.committedCatalogDigest.String()} {
		writeAdmissionString(h, value)
	}
	writeAdmissionUint(h, uint64(hint.attemptIndex))
	writeAdmissionUint(h, uint64(hint.targetIndex))
	if hint.unresolvedTerminalDigest == nil {
		writeAdmissionUint(h, 0)
	} else {
		writeAdmissionUint(h, 1)
		writeAdmissionString(h, hint.unresolvedTerminalDigest.String())
		writeAdmissionString(h, hint.unresolvedTerminalRecordDigest.String())
	}
	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result
}

func (runner *Runner) projectRunnerLedgerReconciliationForPrefix(ctx context.Context, session DatabaseSession, bindings RunnerProjectionBindings, bundle *RuntimeBundle, plans []StatementPlan, prefix runnerLedgerPrefix, migrationRole ProjectionResult[AuthorityProjection], hint runnerLedgerReconciliationHint) (runnerLedgerCatalogProjectionFacts, *runnerLedgerReconciliationFacts, error) {
	var empty runnerLedgerCatalogProjectionFacts
	if hint.canonical == ([32]byte{}) || hint.canonical != runnerLedgerReconciliationHintDigest(&hint) ||
		bundle == nil || int(hint.targetIndex) >= len(bundle.Manifest.SchemaBundle.Migrations) ||
		bundle.Manifest.SchemaBundle.Migrations[hint.targetIndex].ID != hint.migrationID {
		return empty, nil, fail(CodeEvidenceJournalCorrupt, "runner-ledger-reconciliation-observation", "recovery target differs from the verified runtime order", nil)
	}
	predecessorHead := bundle.Manifest.SchemaBundle.Migrations[hint.targetIndex-1].ID
	pendingScope, err := runnerLedgerReconciliationPendingScope(plans, hint.migrationID)
	if err != nil {
		return empty, nil, err
	}
	predecessor, ok := exactCatalogBindingForHead(bindings.executableCatalogs, predecessorHead)
	if !ok || predecessor.catalogContractDigest.Validate() != nil || predecessor.verifiedCatalog.validate() != nil {
		return empty, nil, fail(CodeUntrusted, "runner-ledger-reconciliation-observation", "recovery predecessor catalog subject is unavailable", nil)
	}
	projection, projectionErr := runner.projectRunnerLedgerCatalogForPrefix(ctx, session, bindings, bundle, plans, prefix, migrationRole)
	projectedStateDigest := Digest("")
	projectedCatalogDigest := Digest("")
	projectionObserved := projectionErr == nil
	projectionReportedDrift := IsCode(projectionErr, CodeCatalogDrift)
	if projectionErr != nil && !projectionReportedDrift {
		return empty, nil, projectionErr
	}
	if projectionObserved {
		var err error
		projectedStateDigest, err = runnerLedgerCatalogStateDigest(projection, pendingScope)
		if err != nil {
			return empty, nil, err
		}
		if projection.cumulative != nil {
			projectedCatalogDigest, err = runnerLedgerCatalogProjectionDigest(projection)
			if err != nil {
				return empty, nil, err
			}
		}
	}
	outcome := runnerLedgerReconciliationDivergent
	if projectionObserved && len(prefix.rows) == int(hint.targetIndex) && projectedStateDigest == hint.pendingCatalogDigest {
		outcome = runnerLedgerReconciliationExactPending
	} else if projectionObserved && len(prefix.rows) == int(hint.targetIndex)+1 &&
		runnerCanonicalEqual(prefix.rows[hint.targetIndex], hint.commit.LedgerRow) && projectedCatalogDigest == hint.committedCatalogDigest {
		outcome = runnerLedgerReconciliationExactCommitted
	}
	facts := &runnerLedgerReconciliationFacts{
		outcome: outcome, state: hint.state, action: hint.action, migrationID: hint.migrationID,
		attemptIndex: hint.attemptIndex, targetIndex: hint.targetIndex,
		commitRecordDigest: hint.commitRecordDigest, commitBodyDigest: hint.commitBodyDigest,
		expectedLedgerRow: cloneProjectionValue(hint.commit.LedgerRow), pendingCatalogDigest: hint.pendingCatalogDigest,
		pendingCatalogScope:            cloneProjectionValue(pendingScope),
		committedCatalogDigest:         hint.committedCatalogDigest,
		unresolvedTerminalDigest:       cloneDigestPointer(hint.unresolvedTerminalDigest),
		unresolvedTerminalRecordDigest: cloneDigestPointer(hint.unresolvedTerminalRecordDigest),
		predecessorProjectionSubject:   predecessor.verifiedCatalog.SubjectDigest(),
		observedProjectionSubject:      projection.projectionSubject,
		catalogProjectionObserved:      projectionObserved, catalogProjectionReportedDrift: projectionReportedDrift,
	}
	facts.subjectDigest = runnerLedgerReconciliationFactsDigest(facts)
	if !validRunnerLedgerReconciliationFacts(facts) {
		return empty, nil, fail(CodeEvidenceRecoveryRequired, "runner-ledger-reconciliation-observation", "reconciliation observation could not be sealed", nil)
	}
	return projection, facts, nil
}

func runnerLedgerCatalogStateDigest(projection runnerLedgerCatalogProjectionFacts, scope ProjectionScope) (Digest, error) {
	if projection.initial != nil && projection.cumulative == nil {
		return projection.initial.Digest, nil
	}
	if projection.initial != nil || projection.cumulative == nil || scope.Validate() != nil {
		return "", fail(CodeCatalogDrift, "runner-ledger-reconciliation-catalog", "catalog projection is unavailable", nil)
	}
	state := CatalogStateProjection{Present: &SchemaPresentProjection{
		State: "schema_present", Scope: cloneProjectionValue(scope),
		Body: cloneProjectionValue(projection.cumulative.Projection.Body),
	}}
	digest, err := state.ComputeDigest()
	if err != nil || digest.Validate() != nil {
		return "", fail(CodeCatalogDrift, "runner-ledger-reconciliation-catalog", "catalog projection digest is unavailable", nil)
	}
	return digest, nil
}

func runnerLedgerReconciliationPendingScope(plans []StatementPlan, migrationID string) (ProjectionScope, error) {
	for _, plan := range plans {
		if plan.MigrationID == migrationID && plan.StatementIndex == 0 {
			if plan.validateExact() != nil || plan.ExpectedTransition.CatalogBefore.Scope.Validate() != nil {
				break
			}
			return cloneProjectionValue(plan.ExpectedTransition.CatalogBefore.Scope), nil
		}
	}
	return ProjectionScope{}, fail(CodeUntrusted, "runner-ledger-reconciliation-catalog", "verified predecessor catalog scope is unavailable", nil)
}

func runnerLedgerCatalogProjectionDigest(projection runnerLedgerCatalogProjectionFacts) (Digest, error) {
	if projection.initial != nil || projection.cumulative == nil || projection.cumulative.Digest.Validate() != nil {
		return "", fail(CodeCatalogDrift, "runner-ledger-reconciliation-catalog", "catalog projection digest is unavailable", nil)
	}
	return projection.cumulative.Digest, nil
}

func (facts *runnerLedgerReconciliationFacts) wire() runnerLedgerReconciliationFactsWire {
	if facts == nil {
		return runnerLedgerReconciliationFactsWire{}
	}
	return runnerLedgerReconciliationFactsWire{
		Outcome: facts.outcome, State: facts.state, Action: facts.action, MigrationID: facts.migrationID,
		AttemptIndex: facts.attemptIndex, TargetIndex: facts.targetIndex,
		CommitRecordDigest: facts.commitRecordDigest, CommitBodyDigest: facts.commitBodyDigest,
		ExpectedLedgerRow:    cloneProjectionValue(facts.expectedLedgerRow),
		PendingCatalogDigest: facts.pendingCatalogDigest, CommittedCatalogDigest: facts.committedCatalogDigest,
		PendingCatalogScope:            cloneProjectionValue(facts.pendingCatalogScope),
		UnresolvedTerminalDigest:       cloneDigestPointer(facts.unresolvedTerminalDigest),
		UnresolvedTerminalRecordDigest: cloneDigestPointer(facts.unresolvedTerminalRecordDigest),
		PredecessorProjectionSubject:   facts.predecessorProjectionSubject,
		ObservedProjectionSubject:      facts.observedProjectionSubject,
		CatalogProjectionObserved:      facts.catalogProjectionObserved,
		CatalogProjectionReportedDrift: facts.catalogProjectionReportedDrift,
	}
}

func runnerLedgerReconciliationFactsWirePointer(facts *runnerLedgerReconciliationFacts) *runnerLedgerReconciliationFactsWire {
	if facts == nil {
		return nil
	}
	wire := facts.wire()
	return &wire
}

func runnerLedgerReconciliationFactsDigest(facts *runnerLedgerReconciliationFacts) Digest {
	if facts == nil {
		return ""
	}
	canonical, err := canonicalContractKey(facts.wire())
	if err != nil || canonical == "" {
		return ""
	}
	return DigestBytes([]byte(runnerLedgerReconciliationFactsDigestDomain + "\x00" + canonical))
}

func validRunnerLedgerReconciliationFacts(facts *runnerLedgerReconciliationFacts) bool {
	if facts == nil || !stringIn(string(facts.outcome), string(runnerLedgerReconciliationExactCommitted), string(runnerLedgerReconciliationExactPending), string(runnerLedgerReconciliationDivergent)) ||
		facts.action != RecoveryReconcileCommit || (facts.state != RecoveryDanglingCommitIntent && facts.state != RecoveryAmbiguousUnresolved) ||
		!migrationIDPattern.MatchString(facts.migrationID) || facts.attemptIndex == 0 || facts.targetIndex == 0 ||
		facts.commitRecordDigest.Validate() != nil || facts.commitBodyDigest.Validate() != nil || facts.expectedLedgerRow.Validate() != nil ||
		facts.expectedLedgerRow.MigrationID != facts.migrationID || facts.pendingCatalogDigest.Validate() != nil ||
		facts.pendingCatalogScope.Validate() != nil ||
		facts.committedCatalogDigest.Validate() != nil || facts.predecessorProjectionSubject.Validate() != nil ||
		facts.observedProjectionSubject.Validate() != nil || facts.subjectDigest.Validate() != nil ||
		facts.subjectDigest != runnerLedgerReconciliationFactsDigest(facts) ||
		(facts.state == RecoveryAmbiguousUnresolved) != (facts.unresolvedTerminalDigest != nil) ||
		(facts.unresolvedTerminalDigest == nil) != (facts.unresolvedTerminalRecordDigest == nil) ||
		facts.catalogProjectionObserved == facts.catalogProjectionReportedDrift {
		return false
	}
	if facts.unresolvedTerminalDigest != nil && (facts.unresolvedTerminalDigest.Validate() != nil || facts.unresolvedTerminalRecordDigest.Validate() != nil) {
		return false
	}
	return facts.catalogProjectionObserved || facts.outcome == runnerLedgerReconciliationDivergent
}

func cloneRunnerLedgerReconciliationFacts(facts *runnerLedgerReconciliationFacts) *runnerLedgerReconciliationFacts {
	if facts == nil {
		return nil
	}
	owned := *facts
	owned.expectedLedgerRow = cloneProjectionValue(facts.expectedLedgerRow)
	owned.pendingCatalogScope = cloneProjectionValue(facts.pendingCatalogScope)
	owned.unresolvedTerminalDigest = cloneDigestPointer(facts.unresolvedTerminalDigest)
	owned.unresolvedTerminalRecordDigest = cloneDigestPointer(facts.unresolvedTerminalRecordDigest)
	return &owned
}

func cloneRunnerLedgerReconciliationHint(hint *runnerLedgerReconciliationHint) *runnerLedgerReconciliationHint {
	if hint == nil {
		return nil
	}
	owned := *hint
	owned.commit = cloneProjectionValue(hint.commit)
	owned.unresolvedTerminalDigest = cloneDigestPointer(hint.unresolvedTerminalDigest)
	owned.unresolvedTerminalRecordDigest = cloneDigestPointer(hint.unresolvedTerminalRecordDigest)
	return &owned
}

func sameRunnerLedgerReconciliationFacts(left, right *runnerLedgerReconciliationFacts) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return validRunnerLedgerReconciliationFacts(left) && validRunnerLedgerReconciliationFacts(right) &&
		left.subjectDigest == right.subjectDigest && runnerCanonicalEqual(left.wire(), right.wire())
}

func validateRunnerLedgerReconciliationAgainstEvidence(projection *runnerLedgerCatalogPreflight, evidence runnerLedgerPreflightEvidenceFacts) error {
	if projection == nil || !validRunnerLedgerCatalogPreflight(projection) || !validRunnerLedgerReconciliationFacts(projection.reconciliation) ||
		evidence.recovery == nil || evidence.recovery.commitIntent == nil || evidence.recovery.lastCommitIntentRecordDigest == nil {
		return fail(CodeEvidenceRecoveryRequired, "runner-ledger-reconciliation-bind", "reconciliation evidence is unavailable", nil)
	}
	facts := projection.reconciliation
	recovery := evidence.recovery
	commit := recovery.commitIntent.value
	commitCanonical, err := canonicalContractKey(commit)
	if err != nil || commitCanonical == "" || commit.Validate() != nil ||
		facts.state != recovery.state || facts.action != recovery.nextPermittedAction || facts.action != RecoveryReconcileCommit ||
		facts.migrationID != commit.MigrationID || facts.attemptIndex != commit.AttemptIndex {
		return fail(CodeEvidenceJournalCorrupt, "runner-ledger-reconciliation-bind", "reconciliation commit identity differs from same-verifier evidence", nil)
	}
	if facts.targetIndex != uint32(len(evidence.schema.durableObservedLedgerPrefix)) ||
		commit.ExpectedLedgerLength != facts.targetIndex+1 || facts.targetIndex == 0 {
		return fail(CodeEvidenceJournalCorrupt, "runner-ledger-reconciliation-bind", "reconciliation target differs from the durable ledger prefix", nil)
	}
	if facts.commitRecordDigest != *recovery.lastCommitIntentRecordDigest ||
		facts.commitBodyDigest != DigestBytes([]byte("cloud-agents/runner-ledger-reconciliation/commit-intent/v1\x00"+commitCanonical)) {
		return fail(CodeEvidenceJournalCorrupt, "runner-ledger-reconciliation-bind", "reconciliation commit digest differs from same-verifier evidence", nil)
	}
	if !runnerCanonicalEqual(facts.expectedLedgerRow, commit.LedgerRow) ||
		int(facts.targetIndex) >= len(evidence.schema.signedExpectedLedgerRows) ||
		!runnerCanonicalEqual(facts.expectedLedgerRow, evidence.schema.signedExpectedLedgerRows[facts.targetIndex]) {
		return fail(CodeEvidenceJournalCorrupt, "runner-ledger-reconciliation-bind", "reconciliation ledger row differs from the signed row", nil)
	}
	if facts.pendingCatalogDigest != commit.AttemptPredecessorCatalogDigest {
		return fail(CodeEvidenceJournalCorrupt, "runner-ledger-reconciliation-bind", "reconciliation predecessor catalog differs from the commit intent", nil)
	}
	if facts.committedCatalogDigest != evidence.schema.chainWitness.finalCatalogDigest[commit.MigrationID] {
		return fail(CodeEvidenceJournalCorrupt, "runner-ledger-reconciliation-bind", "reconciliation committed catalog differs from the same-verifier final catalog", nil)
	}
	if recovery.state == RecoveryDanglingCommitIntent {
		if facts.unresolvedTerminalDigest != nil || facts.unresolvedTerminalRecordDigest != nil || recovery.lastTerminal != nil || recovery.lastResolution != nil {
			return fail(CodeEvidenceJournalCorrupt, "runner-ledger-reconciliation-bind", "dangling commit contains a terminal or resolution", nil)
		}
	} else if recovery.state == RecoveryAmbiguousUnresolved {
		if recovery.lastTerminal == nil || recovery.lastTerminalDigest == nil || recovery.lastResolution != nil ||
			facts.unresolvedTerminalDigest == nil || facts.unresolvedTerminalRecordDigest == nil ||
			*facts.unresolvedTerminalDigest != *recovery.lastTerminalDigest ||
			*facts.unresolvedTerminalRecordDigest != recovery.lastTerminal.recordDigest {
			return fail(CodeEvidenceJournalCorrupt, "runner-ledger-reconciliation-bind", "unresolved terminal differs from same-verifier evidence", nil)
		}
	} else {
		return fail(CodeEvidenceJournalCorrupt, "runner-ledger-reconciliation-bind", "recovery state is not reconcilable", nil)
	}
	durable := runnerLedgerPrefix{
		rows:   cloneProjectionValue(evidence.schema.durableObservedLedgerPrefix),
		digest: evidence.schema.durableObservedLedgerDigest,
	}
	if len(durable.rows) > 0 {
		durable.head = durable.rows[len(durable.rows)-1].MigrationID
	}
	projectedStateDigest := Digest("")
	projectedCatalogDigest := Digest("")
	if facts.catalogProjectionObserved {
		observed := runnerLedgerCatalogProjectionFacts{
			initial: projection.initialPredecessor, cumulative: projection.cumulativeCatalog,
			catalogContractDigest: projection.catalogContractDigest, projectionSubject: projection.projectionSubjectDigest,
		}
		projectedStateDigest, err = runnerLedgerCatalogStateDigest(observed, facts.pendingCatalogScope)
		if err != nil {
			return err
		}
		if observed.cumulative != nil {
			projectedCatalogDigest, err = runnerLedgerCatalogProjectionDigest(observed)
			if err != nil {
				return err
			}
		}
	}
	wantOutcome := runnerLedgerReconciliationDivergent
	if facts.catalogProjectionObserved && sameRunnerLedgerPrefix(projection.ledger, durable) && projectedStateDigest == facts.pendingCatalogDigest {
		wantOutcome = runnerLedgerReconciliationExactPending
	} else if facts.catalogProjectionObserved && len(projection.ledger.rows) == len(durable.rows)+1 {
		predecessor := runnerLedgerPrefix{rows: cloneProjectionValue(projection.ledger.rows[:len(durable.rows)])}
		predecessor.digest, err = LedgerPrefixDigest(predecessor.rows)
		if len(predecessor.rows) > 0 {
			predecessor.head = predecessor.rows[len(predecessor.rows)-1].MigrationID
		}
		if err == nil && sameRunnerLedgerPrefix(predecessor, durable) &&
			runnerCanonicalEqual(projection.ledger.rows[len(durable.rows)], facts.expectedLedgerRow) &&
			projectedCatalogDigest == facts.committedCatalogDigest {
			wantOutcome = runnerLedgerReconciliationExactCommitted
		}
	}
	if facts.outcome != wantOutcome {
		return fail(CodeEvidenceJournalCorrupt, "runner-ledger-reconciliation-bind", "database reconciliation classification is contradictory", nil)
	}
	return nil
}

func bindRunnerLedgerReconciliationPreflight(bindings RunnerProjectionBindings, bundle *RuntimeBundle, plans []StatementPlan, ledger runnerLedgerPrefix, connected, migrationRole ProjectionResult[AuthorityProjection], initial *ProjectionResult[CatalogStateProjection], cumulative *ProjectionResult[CatalogProjection], catalogContractDigest *Digest, projectionSubjectDigest Digest, reconciliation *runnerLedgerReconciliationFacts) (*runnerLedgerCatalogPreflight, error) {
	if !validRunnerLedgerReconciliationFacts(reconciliation) {
		return nil, fail(CodeEvidenceRecoveryRequired, "runner-ledger-reconciliation-bind", "reconciliation facts are unavailable", nil)
	}
	verifiedBundle, err := verifiedRunnerLedgerCatalogBundle(bundle)
	if err != nil {
		return nil, err
	}
	verifiedPlans, err := buildExactStatementPlans(verifiedBundle, bindings, time.Now())
	if err != nil {
		return nil, err
	}
	bundle, plans = verifiedBundle, verifiedPlans
	if int(reconciliation.targetIndex) >= len(bundle.Manifest.SchemaBundle.Migrations) ||
		bundle.Manifest.SchemaBundle.Migrations[reconciliation.targetIndex].ID != reconciliation.migrationID {
		return nil, fail(CodeUntrusted, "runner-ledger-reconciliation-bind", "reconciliation target changed", nil)
	}
	pendingScope, err := runnerLedgerReconciliationPendingScope(plans, reconciliation.migrationID)
	if err != nil || !equalProjectionScopes(pendingScope, reconciliation.pendingCatalogScope) {
		return nil, fail(CodeUntrusted, "runner-ledger-reconciliation-bind", "reconciliation predecessor catalog scope changed", err)
	}
	predecessorHead := bundle.Manifest.SchemaBundle.Migrations[reconciliation.targetIndex-1].ID
	predecessor, ok := exactCatalogBindingForHead(bindings.executableCatalogs, predecessorHead)
	if !ok || predecessor.verifiedCatalog.validate() != nil {
		return nil, fail(CodeUntrusted, "runner-ledger-reconciliation-bind", "reconciliation predecessor catalog binding changed", nil)
	}
	predecessorSubject := predecessor.verifiedCatalog.SubjectDigest()
	if predecessorSubject.Validate() != nil || predecessorSubject != reconciliation.predecessorProjectionSubject {
		return nil, fail(CodeUntrusted, "runner-ledger-reconciliation-bind", "reconciliation predecessor catalog subject changed", nil)
	}
	if reconciliation.catalogProjectionObserved {
		prepared, err := bindRunnerLedgerCatalogPreflight(
			bindings, bundle, plans, ledger, connected, migrationRole, initial, cumulative,
			catalogContractDigest, projectionSubjectDigest,
		)
		if err != nil {
			return nil, err
		}
		prepared.reconciliation = cloneRunnerLedgerReconciliationFacts(reconciliation)
		prepared.subjectDigest = runnerLedgerCatalogPreflightSubjectDigest(prepared)
		if !validRunnerLedgerCatalogPreflight(prepared) {
			return nil, fail(CodeEvidenceRecoveryRequired, "runner-ledger-reconciliation-bind", "reconciliation preflight could not be sealed", nil)
		}
		return prepared, nil
	}
	if !generatedRunnerLedgerPreflightProfile.valid() || bindings.validateAt(time.Now()) != nil ||
		bindings.schemaBundleDigest != bundle.Manifest.SchemaBundleDigest || len(bundle.Manifest.SchemaBundle.Migrations) == 0 ||
		uint64(len(bundle.Manifest.SchemaBundle.Migrations)) > uint64(^uint32(0)) ||
		int(reconciliation.targetIndex) >= len(bundle.Manifest.SchemaBundle.Migrations) ||
		bundle.Manifest.SchemaBundle.Migrations[reconciliation.targetIndex].ID != reconciliation.migrationID ||
		projectionSubjectDigest != reconciliation.observedProjectionSubject || initial != nil || cumulative != nil {
		return nil, fail(CodeUntrusted, "runner-ledger-reconciliation-bind", "verified runtime or drift observation changed", nil)
	}
	if err := validateRunnerLedgerPrefixForBundle(ledger, bundle); err != nil {
		return nil, err
	}
	if err := validateRunnerAuthorityProjectionResult(connected, connected.Metadata.Snapshot, bindings.verifiedAuthority, AuthorityPhaseConnectedSession); err != nil {
		return nil, err
	}
	if err := validateRunnerAuthorityProjectionResult(migrationRole, migrationRole.Metadata.Snapshot, bindings.verifiedAuthority, AuthorityPhaseMigrationRole); err != nil ||
		!sameRunnerDedicatedSessionIdentity(connected.Metadata.Snapshot, migrationRole.Metadata.Snapshot) {
		return nil, fail(CodeProjectionMetadataMismatch, "runner-ledger-reconciliation-bind", "authority projections describe different database sessions", err)
	}
	if len(ledger.rows) == 0 {
		if catalogContractDigest != nil || projectionSubjectDigest != bindings.initialSchemaScope.SubjectDigest() {
			return nil, fail(CodeUntrusted, "runner-ledger-reconciliation-bind", "empty divergent prefix has the wrong catalog subject", nil)
		}
	} else {
		catalog, ok := exactCatalogBindingForHead(bindings.executableCatalogs, ledger.head)
		if !ok || catalogContractDigest == nil || *catalogContractDigest != catalog.catalogContractDigest ||
			projectionSubjectDigest != catalog.verifiedCatalog.SubjectDigest() {
			return nil, fail(CodeUntrusted, "runner-ledger-reconciliation-bind", "divergent prefix has the wrong catalog subject", nil)
		}
	}
	state := runnerLedgerCatalogEmpty
	if len(ledger.rows) > 0 && len(ledger.rows) < len(bundle.Manifest.SchemaBundle.Migrations) {
		state = runnerLedgerCatalogPartial
	} else if len(ledger.rows) == len(bundle.Manifest.SchemaBundle.Migrations) {
		state = runnerLedgerCatalogComplete
	}
	prepared := &runnerLedgerCatalogPreflight{
		profileID: generatedRunnerLedgerPreflightProfile.profileID, profileDigest: generatedRunnerLedgerPreflightProfile.profileDigest,
		registryDigest: runnerLedgerPreflightRegistryDigest, stateMachineDigest: runnerLedgerPreflightStateMachineDigest,
		policyDigest: runnerLedgerPreflightPolicyDigest, state: state,
		schemaBundleDigest: bindings.schemaBundleDigest, executionLineageDigest: bindings.executionLineageDigest,
		runnerProjectionDecisionDigest: bindings.runnerProjectionDecisionDigest,
		authoritySubjectDigest:         bindings.verifiedAuthority.SubjectDigest(), projectionSubjectDigest: projectionSubjectDigest,
		catalogContractDigest: cloneDigestPointer(catalogContractDigest), migrationCount: uint32(len(bundle.Manifest.SchemaBundle.Migrations)),
		ledger: cloneRunnerLedgerPrefix(ledger), connectedAuthority: cloneProjectionValue(connected),
		migrationRoleAuthority: cloneProjectionValue(migrationRole), reconciliation: cloneRunnerLedgerReconciliationFacts(reconciliation),
	}
	prepared.subjectDigest = runnerLedgerCatalogPreflightSubjectDigest(prepared)
	if !validRunnerLedgerCatalogPreflight(prepared) {
		return nil, fail(CodeEvidenceRecoveryRequired, "runner-ledger-reconciliation-bind", "divergent reconciliation preflight could not be sealed", nil)
	}
	return prepared, nil
}
