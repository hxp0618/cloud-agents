package migration

import (
	"context"
	"crypto/sha256"
	"sync"
	"sync/atomic"
	"time"
)

const (
	runnerLedgerRetryHandoffBoundaryDigestDomain = "cloud-agents/runner-ledger-retry-handoff/boundary/v1"
	runnerLedgerRetryHandoffReceiptDigestDomain  = "cloud-agents/runner-ledger-retry-handoff/closed-receipt/v1"
	runnerLedgerRetryHandoffPermitDigestDomain   = "cloud-agents/runner-ledger-retry-handoff/permit/v1"
)

// runnerLedgerRetryHandoffBinder is the only mutation port opened by Slice E.
// It consumes the generated retry-handoff permit and delegates to the already
// closed successor-generation transition. It exposes neither SQL nor an entry
// or recovery-execution writer.
type runnerLedgerRetryHandoffBinder interface {
	EvidenceSession
	runnerLedgerRecoveryAdmissionClaimBinder
	bindRunnerLedgerRetryHandoff(context.Context, *runnerLedgerRetryHandoffPermit) (ActiveGeneration, *RecoverySnapshot, error)
	runnerLedgerRetryHandoffBinderSealed()
}

type runnerLedgerRetryHandoffBoundary struct {
	outcome          string
	checkpointDigest Digest
	terminalDigest   Digest
	resolutionDigest *Digest
	continuation     LineageContinuationContext
	canonical        [32]byte
}

// The receipt is minted only after the retained read-only PostgreSQL session
// has been revalidated, unlocked, reset, and closed. It owns no database or
// evidence handle and cannot itself initiate the successor transition.
type runnerLedgerRetryHandoffClosedReceipt struct {
	self                     *runnerLedgerRetryHandoffClosedReceipt
	token                    *runnerLedgerRetryHandoffReceiptToken
	candidateBinding         *verifiedEvidenceRunBinding
	generation               generationIdentity
	recoveryDigest           [32]byte
	recoveryTail             Digest
	selection                runnerLedgerRecoveryAdmissionSelection
	boundary                 runnerLedgerRetryHandoffBoundary
	consumerFactSubject      Digest
	evidenceBoundary         [32]byte
	admissionPermitCanonical [32]byte
	ledgerDigest             Digest
	ledgerHead               string
	ledgerLength             uint32
	projectionSubject        Digest
	database                 runnerPreparedDatabaseIdentity
	canonical                [32]byte
}

type runnerLedgerRetryHandoffReceiptToken struct{}

type runnerLedgerRetryHandoffPermit struct {
	self                     *runnerLedgerRetryHandoffPermit
	binder                   runnerLedgerRetryHandoffBinder
	candidateBinding         *verifiedEvidenceRunBinding
	generation               generationIdentity
	recoveryDigest           [32]byte
	recoveryTail             Digest
	cursor                   JournalCursor
	selection                runnerLedgerRecoveryAdmissionSelection
	boundary                 runnerLedgerRetryHandoffBoundary
	receipt                  *runnerLedgerRetryHandoffClosedReceipt
	consumerFactSubject      Digest
	evidenceBoundary         [32]byte
	admissionPermitCanonical [32]byte
	consumed                 *atomic.Bool
	canonical                [32]byte
}

type runnerLedgerRetryHandoffPermitRecord struct {
	permit           *runnerLedgerRetryHandoffPermit
	binder           runnerLedgerRetryHandoffBinder
	candidateBinding *verifiedEvidenceRunBinding
	cursorValid      *atomic.Bool
	consumed         *atomic.Bool
	canonical        [32]byte
}

type runnerLedgerRetryHandoffClaim struct {
	binder           runnerLedgerRetryHandoffBinder
	candidateBinding *verifiedEvidenceRunBinding
	generation       generationIdentity
	recoveryDigest   [32]byte
	recoveryTail     Digest
	cursor           JournalCursor
	selection        runnerLedgerRecoveryAdmissionSelection
	boundary         runnerLedgerRetryHandoffBoundary
	receipt          *runnerLedgerRetryHandoffClosedReceipt
	canonical        [32]byte
}

var runnerLedgerRetryHandoffPermitRegistry sync.Map

func runnerLedgerRetryHandoffEvidenceSession(evidence EvidenceSession, candidate OwnedCurrentCandidate, snapshot *RecoverySnapshot) bool {
	if evidence == nil || !validOwnedCurrentCandidate(candidate) || snapshot == nil {
		return false
	}
	current := evidence.CurrentCandidate()
	active := evidence.ActiveGeneration()
	return validOwnedCurrentCandidate(current) && current.binding == candidate.binding &&
		runnerLedgerRetryHandoffActiveMatches(active, candidate, snapshot)
}

func runnerLedgerRetryHandoffActiveMatches(active ActiveGeneration, candidate OwnedCurrentCandidate, snapshot *RecoverySnapshot) bool {
	return validOwnedCurrentCandidate(candidate) && active.kind == activeGenerationAncestorRecovery &&
		active.identity.owner == candidate.owner && active.identity.executionLineageDigest == candidate.verifiedRun.executionLineageDigest &&
		active.identity.runnerProjectionDecisionDigest != candidate.verifiedRun.runnerProjectionDecisionDigest &&
		active.ownedDecision.owner == candidate.verifiedRun.currentDecision.owner &&
		active.ownedDecision.digest == active.identity.runnerProjectionDecisionDigest &&
		active.recoveryExecutionBindings != nil &&
		active.recoveryExecutionBindings.policy.SuccessorSchemaBundleDigest == candidate.verifiedRun.schemaBundleDigest &&
		sameRecoveryExecutionBindings(active.recoveryExecutionBindings, active.recoveryExecutionBindings, active.identity, candidate.verifiedRun.currentDecision.digest) &&
		snapshot != nil && validRecoverySnapshotForJournal(snapshot, active.identity, snapshot.cursor) &&
		generationJournalRecoveryDigest(snapshot) == generationJournalRecoveryDigest(active.recoveryExecutionBindings.snapshot) &&
		sameCursorIdentity(snapshot.cursor, active.recoveryExecutionBindings.snapshot.cursor) &&
		snapshot.state == RecoveryTerminal && snapshot.nextPermittedAction == RecoveryBeginNextAttempt
}

func runnerLedgerPreflightActiveIdentity(active ActiveGeneration, snapshot *RecoverySnapshot, candidate OwnedCurrentCandidate) (activeGenerationKind, Digest, bool) {
	if !validOwnedCurrentCandidate(candidate) || active.identity.owner != candidate.owner ||
		active.identity.executionLineageDigest != candidate.verifiedRun.executionLineageDigest {
		return "", "", false
	}
	if active.kind == activeGenerationCurrent {
		if active.recoveryExecutionBindings != nil || active.identity.schemaBundleDigest != candidate.verifiedRun.schemaBundleDigest ||
			active.identity.runnerProjectionDecisionDigest != candidate.verifiedRun.runnerProjectionDecisionDigest ||
			active.ownedDecision.digest != candidate.verifiedRun.currentDecision.digest ||
			!active.ownedDecision.decision.exactlyMatches(candidate.verifiedRun.currentDecision.decision) {
			return "", "", false
		}
		return activeGenerationCurrent, "", true
	}
	if runnerLedgerRetryHandoffActiveMatches(active, candidate, snapshot) {
		return activeGenerationAncestorRecovery, active.recoveryExecutionBindings.digest, true
	}
	return "", "", false
}

func runnerLedgerRetryHandoffProjectionBindings(evidence EvidenceSession, candidate OwnedCurrentCandidate) (RunnerProjectionBindings, error) {
	if evidence == nil || !validOwnedCurrentCandidate(candidate) {
		return RunnerProjectionBindings{}, fail(CodeEvidenceRecoveryRequired, "runner-ledger-retry-handoff-projection", "ancestor recovery evidence is unavailable", nil)
	}
	current := evidence.CurrentCandidate()
	active := evidence.ActiveGeneration()
	snapshot := evidence.RecoverySnapshot()
	if !validOwnedCurrentCandidate(current) || current.binding != candidate.binding ||
		!runnerLedgerRetryHandoffActiveMatches(active, candidate, snapshot) {
		return RunnerProjectionBindings{}, fail(CodeEvidenceRecoveryRequired, "runner-ledger-retry-handoff-projection", "ancestor recovery evidence differs from the current candidate", nil)
	}
	bindings, err := candidate.verifiedRun.currentDecision.decision.runnerProjectionBindings()
	if err != nil || !validOwnedCurrentDecision(candidate.verifiedRun.currentDecision, bindings) ||
		bindings.executionLineageDigest != active.identity.executionLineageDigest ||
		bindings.schemaBundleDigest != active.recoveryExecutionBindings.policy.SuccessorSchemaBundleDigest ||
		bindings.runnerProjectionDecisionDigest != active.recoveryExecutionBindings.subject.CurrentRunnerProjectionDecisionDigest {
		return RunnerProjectionBindings{}, fail(CodeUntrusted, "runner-ledger-retry-handoff-projection", "current projection is not the verified successor of the active ancestor", err)
	}
	return bindings, nil
}

func runnerLedgerPreflightProjectionMatchesEvidence(projection *runnerLedgerCatalogPreflight, facts runnerLedgerPreflightEvidenceFacts, candidate OwnedCurrentCandidate) bool {
	if projection == nil || !validOwnedCurrentCandidate(candidate) ||
		projection.schemaBundleDigest != candidate.verifiedRun.schemaBundleDigest ||
		projection.executionLineageDigest != candidate.verifiedRun.executionLineageDigest ||
		projection.runnerProjectionDecisionDigest != candidate.verifiedRun.runnerProjectionDecisionDigest {
		return false
	}
	if facts.activeKind == activeGenerationCurrent {
		return projection.schemaBundleDigest == facts.generation.schemaBundleDigest &&
			projection.runnerProjectionDecisionDigest == facts.generation.runnerProjectionDecisionDigest &&
			uint64(projection.migrationCount) == uint64(len(facts.schema.signedExpectedLedgerRows))
	}
	return facts.activeKind == activeGenerationAncestorRecovery && projection.reconciliation == nil &&
		facts.executionDigest.Validate() == nil && facts.generation.runnerProjectionDecisionDigest != projection.runnerProjectionDecisionDigest &&
		facts.generation.executionLineageDigest == projection.executionLineageDigest &&
		facts.recovery != nil && facts.recovery.state == RecoveryTerminal &&
		facts.recovery.nextPermittedAction == RecoveryBeginNextAttempt &&
		len(facts.schema.signedExpectedLedgerRows) != 0 && uint64(projection.migrationCount) >= uint64(len(facts.schema.signedExpectedLedgerRows))
}

func runnerLedgerConsumerDispatchMatchesCandidate(dispatch runnerLedgerPreflightDispatch, evidence EvidenceSession, candidate OwnedCurrentCandidate) bool {
	if evidence == nil || !dispatch.valid() || !validOwnedCurrentCandidate(candidate) ||
		dispatch.fact.executionLineageDigest != candidate.verifiedRun.executionLineageDigest {
		return false
	}
	if dispatch.runnerProjectionDecisionDigest == candidate.verifiedRun.runnerProjectionDecisionDigest {
		return dispatch.fact.schemaBundleDigest == candidate.verifiedRun.schemaBundleDigest
	}
	active := evidence.ActiveGeneration()
	snapshot := evidence.RecoverySnapshot()
	return runnerLedgerRetryHandoffEvidenceSession(evidence, candidate, snapshot) &&
		dispatch.fact.disposition == runnerLedgerPreflightPartialRetryOrRecovery &&
		dispatch.fact.recovery != nil && dispatch.fact.recovery.State == RecoveryTerminal &&
		dispatch.fact.recovery.Action == RecoveryBeginNextAttempt &&
		dispatch.runnerProjectionDecisionDigest == active.identity.runnerProjectionDecisionDigest &&
		dispatch.fact.schemaBundleDigest == active.identity.schemaBundleDigest &&
		dispatch.journalIdentityDigest == active.identity.journalIdentityDigest
}

func runnerLedgerConsumerFactMatchesBundle(fact runnerLedgerConsumerFact, evidence EvidenceSession, candidate OwnedCurrentCandidate, bundle *RuntimeBundle) bool {
	if !fact.valid() || bundle == nil || bundle.Manifest == nil ||
		bundle.Manifest.SchemaBundleDigest != candidate.verifiedRun.schemaBundleDigest {
		return false
	}
	return runnerLedgerConsumerDispatchMatchesCandidate(fact.dispatch, evidence, candidate)
}

func runnerLedgerRecoveryObservationIdentityMatches(action runnerLedgerRecoveryAction, fact runnerLedgerConsumerFact, projection *runnerLedgerCatalogPreflight) bool {
	if projection == nil || fact.dispatch.fact.executionLineageDigest != projection.executionLineageDigest {
		return false
	}
	if action == generatedRunnerLedgerRecoveryProfiles[4].action {
		return fact.dispatch.fact.schemaBundleDigest.Validate() == nil &&
			fact.dispatch.runnerProjectionDecisionDigest.Validate() == nil &&
			fact.dispatch.runnerProjectionDecisionDigest != projection.runnerProjectionDecisionDigest
	}
	return fact.dispatch.fact.schemaBundleDigest == projection.schemaBundleDigest &&
		fact.dispatch.runnerProjectionDecisionDigest == projection.runnerProjectionDecisionDigest
}

func runnerLedgerRecoveryGenerationMatchesBindings(action runnerLedgerRecoveryAction, generation generationIdentity, bindings RunnerProjectionBindings) bool {
	if generation.owner == nil || generation.executionLineageDigest.Validate() != nil || generation.journalIdentityDigest.Validate() != nil ||
		generation.runnerProjectionDecisionDigest.Validate() != nil || generation.schemaBundleDigest.Validate() != nil ||
		generation.executionLineageDigest != bindings.executionLineageDigest {
		return false
	}
	if action == generatedRunnerLedgerRecoveryProfiles[4].action {
		return generation.runnerProjectionDecisionDigest != bindings.runnerProjectionDecisionDigest
	}
	return generation.runnerProjectionDecisionDigest == bindings.runnerProjectionDecisionDigest &&
		generation.schemaBundleDigest == bindings.schemaBundleDigest
}

func runnerLedgerRecoveryPartialProjectionAllowed(action runnerLedgerRecoveryAction, projection *runnerLedgerCatalogPreflight) bool {
	if projection == nil {
		return false
	}
	if action == generatedRunnerLedgerRecoveryProfiles[2].action || action == generatedRunnerLedgerRecoveryProfiles[3].action {
		return projection.reconciliation != nil
	}
	if action == generatedRunnerLedgerRecoveryProfiles[4].action {
		return projection.reconciliation == nil && (projection.state == runnerLedgerCatalogEmpty || projection.state == runnerLedgerCatalogPartial)
	}
	return projection.reconciliation == nil && projection.state == runnerLedgerCatalogPartial && len(projection.ledger.rows) != 0
}

func runnerLedgerRetryHandoffSelection(selection runnerLedgerRecoveryAdmissionSelection) bool {
	return runnerLedgerRecoverySelectionAllowed(selection) && selection.action == generatedRunnerLedgerRecoveryProfiles[4].action &&
		selection.profileIndex == 4 && selection.recoveryState == RecoveryTerminal &&
		selection.recoveryAction == RecoveryBeginNextAttempt && selection.planCount != 0 &&
		selection.planDigest != ([32]byte{}) && selection.entryDigest.Validate() == nil &&
		migrationIDPattern.MatchString(selection.migrationID) && selection.attemptIndex != 0 &&
		selection.maxAttempts != 0 && selection.attemptIndex < selection.maxAttempts
}

func runnerLedgerRetryHandoffBoundaryFromSnapshot(snapshot *RecoverySnapshot, generation generationIdentity, recoveryDigest [32]byte, recoveryTail Digest, selection runnerLedgerRecoveryAdmissionSelection) (runnerLedgerRetryHandoffBoundary, bool) {
	var boundary runnerLedgerRetryHandoffBoundary
	if snapshot == nil || !runnerLedgerRetryHandoffSelection(selection) ||
		!validRecoverySnapshotForJournal(snapshot, generation, snapshot.cursor) ||
		generationJournalRecoveryDigest(snapshot) != recoveryDigest || snapshot.tailDigest != recoveryTail ||
		snapshot.state != RecoveryTerminal || snapshot.nextPermittedAction != RecoveryBeginNextAttempt ||
		snapshot.migrationID == nil || *snapshot.migrationID != selection.migrationID ||
		snapshot.attemptIndex == nil || *snapshot.attemptIndex != selection.attemptIndex ||
		snapshot.cursor.latestCheckpointRecordDigest == nil ||
		*snapshot.cursor.latestCheckpointRecordDigest != snapshot.cursor.lineageIndexPreviousRecordDigest ||
		snapshot.lastTerminal == nil || snapshot.lastTerminalDigest == nil ||
		snapshot.lastTerminal.value.Validate() != nil || snapshot.lastTerminal.value.MigrationID != selection.migrationID ||
		snapshot.lastTerminal.value.AttemptIndex != selection.attemptIndex ||
		snapshot.lastTerminal.value.TerminalDigest != *snapshot.lastTerminalDigest ||
		snapshot.lastTerminal.owner != generation.owner || !sameGenerationIdentity(snapshot.lastTerminal.generation, generation) ||
		!sameCursorIdentity(snapshot.lastTerminal.cursor, snapshot.cursor) || snapshot.lastTerminal.tailDigest != recoveryTail {
		return boundary, false
	}
	terminal := snapshot.lastTerminal.value
	boundary.outcome = ""
	switch terminal.Outcome {
	case "aborted_retryable":
		if snapshot.lastResolution != nil || snapshot.lastResolutionDigest != nil {
			return boundary, false
		}
		boundary.outcome = "precommit_aborted_retryable"
	case "ambiguous_reconciled_pending":
		if snapshot.lastResolution != nil || snapshot.lastResolutionDigest != nil {
			return boundary, false
		}
		boundary.outcome = "exact_pending"
	case "ambiguous_unresolved":
		if snapshot.lastResolution == nil || snapshot.lastResolutionDigest == nil ||
			snapshot.lastResolution.value.Validate() != nil || snapshot.lastResolution.value.Outcome != "resolved_pending" ||
			snapshot.lastResolution.value.UnresolvedTerminalDigest != terminal.TerminalDigest ||
			snapshot.lastResolution.value.ResolutionDigest != *snapshot.lastResolutionDigest ||
			snapshot.lastResolution.owner != generation.owner || !sameGenerationIdentity(snapshot.lastResolution.generation, generation) ||
			!sameCursorIdentity(snapshot.lastResolution.cursor, snapshot.cursor) || snapshot.lastResolution.tailDigest != recoveryTail {
			return boundary, false
		}
		boundary.outcome = "resolved_pending"
		boundary.resolutionDigest = cloneDigestPointer(snapshot.lastResolutionDigest)
	default:
		return boundary, false
	}
	boundary.checkpointDigest = *snapshot.cursor.latestCheckpointRecordDigest
	boundary.terminalDigest = terminal.TerminalDigest
	boundary.continuation = LineageContinuationContext{
		StartAction: "begin_next_attempt", MigrationID: selection.migrationID, AttemptIndex: selection.attemptIndex + 1,
		PreviousAttemptTerminalDigest: digestPointer(terminal.TerminalDigest),
		SourceJournalIdentityDigest:   generation.journalIdentityDigest,
		SourceCheckpointRecordDigest:  boundary.checkpointDigest, SourceTerminalDigest: terminal.TerminalDigest,
	}
	if boundary.continuation.Validate() != nil {
		return runnerLedgerRetryHandoffBoundary{}, false
	}
	boundary.canonical = runnerLedgerRetryHandoffBoundaryDigest(generation, recoveryDigest, recoveryTail, selection, boundary)
	return boundary, boundary.canonical != ([32]byte{})
}

func runnerLedgerRetryHandoffBoundaryDigest(generation generationIdentity, recoveryDigest [32]byte, recoveryTail Digest, selection runnerLedgerRecoveryAdmissionSelection, boundary runnerLedgerRetryHandoffBoundary) [32]byte {
	if !runnerLedgerRetryHandoffSelection(selection) || generation.owner == nil || recoveryDigest == ([32]byte{}) ||
		recoveryTail.Validate() != nil || boundary.checkpointDigest.Validate() != nil || boundary.terminalDigest.Validate() != nil ||
		boundary.continuation.Validate() != nil || !stringIn(boundary.outcome, "precommit_aborted_retryable", "exact_pending", "resolved_pending") ||
		(boundary.outcome == "resolved_pending") != (boundary.resolutionDigest != nil) {
		return [32]byte{}
	}
	if boundary.resolutionDigest != nil && boundary.resolutionDigest.Validate() != nil {
		return [32]byte{}
	}
	h := sha256.New()
	h.Write([]byte(runnerLedgerRetryHandoffBoundaryDigestDomain + "\x00"))
	h.Write(recoveryDigest[:])
	for _, value := range []string{
		generation.executionLineageDigest.String(), generation.journalIdentityDigest.String(),
		generation.runnerProjectionDecisionDigest.String(), generation.schemaBundleDigest.String(),
		recoveryTail.String(), selection.migrationID, selection.entryDigest.String(), boundary.outcome,
		boundary.checkpointDigest.String(), boundary.terminalDigest.String(),
	} {
		writeAdmissionString(h, value)
	}
	if boundary.resolutionDigest == nil {
		writeAdmissionString(h, "resolution:absent")
	} else {
		writeAdmissionString(h, "resolution:present")
		writeAdmissionString(h, boundary.resolutionDigest.String())
	}
	continuation, err := canonicalContractKey(boundary.continuation)
	if err != nil || continuation == "" {
		return [32]byte{}
	}
	writeAdmissionString(h, continuation)
	writeAdmissionUint(h, uint64(selection.profileIndex))
	writeAdmissionUint(h, uint64(selection.entryIndex))
	writeAdmissionUint(h, uint64(selection.attemptIndex))
	writeAdmissionUint(h, uint64(selection.maxAttempts))
	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result
}

func claimRunnerLedgerRetryHandoffAdmissionPermit(permit *runnerLedgerRetryHandoffAdmissionPermit) (runnerLedgerReconciliationAdmissionSeed, error) {
	if permit == nil {
		return runnerLedgerReconciliationAdmissionSeed{}, fail(CodeEvidenceRecoveryRequired, "runner-ledger-retry-handoff-claim", "retry-handoff admission permit is unavailable", nil)
	}
	return claimRunnerLedgerReconciliationAdmissionPermit(permit, generatedRunnerLedgerRecoveryProfiles[4].action)
}

func (runner *Runner) revalidateAndCloseRunnerLedgerRetryHandoffAdmission(ctx context.Context, seed runnerLedgerReconciliationAdmissionSeed, bundle *RuntimeBundle) (*runnerLedgerRetryHandoffClosedReceipt, runnerLedgerRetryHandoffBoundary, error) {
	var empty runnerLedgerRetryHandoffBoundary
	failClosed := func(primary error) (*runnerLedgerRetryHandoffClosedReceipt, runnerLedgerRetryHandoffBoundary, error) {
		return nil, empty, closeRunnerDatabasePreflight(seed.session, seed.key, true, primary)
	}
	binder, ok := seed.binder.(runnerLedgerRetryHandoffBinder)
	if !ok || binder == nil || seed.session == nil || seed.candidateBinding == nil || seed.projection == nil ||
		seed.projection.reconciliation != nil || seed.runtimeInputs == ([32]byte{}) || seed.admissionPermitCanonical == ([32]byte{}) ||
		!runnerLedgerRetryHandoffSelection(seed.selection) || !validRunnerLedgerCatalogPreflight(seed.projection) ||
		seed.bindings.validateAt(time.Now()) != nil || seed.bindings.expectedCanonical == "" ||
		!runnerLedgerRecoveryGenerationMatchesBindings(seed.selection.action, seed.generation, seed.bindings) ||
		!validRunnerLedgerRecoveryAdmissionUse(seed.binder, seed.use, seed.consumerFactSubject, seed.selection.action, seed.evidenceBoundary, true) {
		return failClosed(fail(CodeEvidenceRecoveryRequired, "runner-ledger-retry-handoff-revalidate", "retry-handoff admission facts are unavailable or changed", nil))
	}
	verifiedBundle, err := verifiedRunnerLedgerCatalogBundle(bundle)
	if err != nil || verifiedBundle.ownedInputs.canonical != seed.runtimeInputs ||
		verifiedBundle.Manifest.SchemaBundleDigest != seed.bindings.schemaBundleDigest {
		if err == nil {
			err = fail(CodeUntrusted, "runner-ledger-retry-handoff-runtime", "verified current runtime changed after recovery admission", nil)
		}
		return failClosed(err)
	}
	verifiedPlans, err := buildExactStatementPlans(verifiedBundle, seed.bindings, time.Now())
	if err != nil {
		return failClosed(err)
	}
	planDigest, planCount, err := runnerEntryPlanClosureDigest(verifiedPlans, seed.selection.migrationID)
	if err != nil || planDigest != seed.selection.planDigest || planCount != seed.selection.planCount {
		return failClosed(fail(CodeUntrusted, "runner-ledger-retry-handoff-plans", "verified statement-plan closure changed after recovery admission", err))
	}
	current := binder.CurrentCandidate()
	active := binder.ActiveGeneration()
	recovery := binder.RecoverySnapshot()
	if !validOwnedCurrentCandidate(current) || current.binding != seed.candidateBinding ||
		!runnerLedgerRetryHandoffActiveMatches(active, current, recovery) || !sameGenerationIdentity(active.identity, seed.generation) {
		return failClosed(fail(CodeEvidenceRecoveryRequired, "runner-ledger-retry-handoff-revalidate", "ancestor evidence boundary changed before final database revalidation", nil))
	}
	boundary, boundaryOK := runnerLedgerRetryHandoffBoundaryFromSnapshot(recovery, seed.generation, seed.recoveryDigest, seed.recoveryTail, seed.selection)
	if !boundaryOK {
		return failClosed(fail(CodeEvidenceRecoveryRequired, "runner-ledger-retry-handoff-revalidate", "retryable terminal boundary is unavailable or changed", nil))
	}
	projection := seed.projection
	observation := &runnerLockedLedgerCatalogObservation{
		session: seed.session, key: seed.key, bindings: seed.bindings.ownedCopy(), bundle: verifiedBundle,
		plans: verifiedPlans, ledger: cloneRunnerLedgerPrefix(projection.ledger),
		connected: cloneProjectionValue(projection.connectedAuthority), migrationRole: cloneProjectionValue(projection.migrationRoleAuthority),
		initial: cloneCatalogStateProjectionResultPointer(projection.initialPredecessor), cumulative: cloneCatalogProjectionResultPointer(projection.cumulativeCatalog),
		catalogContractDigest: cloneDigestPointer(projection.catalogContractDigest), projectionSubject: projection.projectionSubjectDigest,
	}
	observation.self = observation
	primary := observation.revalidateRecoveryAdmission(ctx, runner)
	if err := observation.close(primary); err != nil {
		return nil, empty, err
	}
	receipt := &runnerLedgerRetryHandoffClosedReceipt{
		candidateBinding: seed.candidateBinding, generation: seed.generation,
		recoveryDigest: seed.recoveryDigest, recoveryTail: seed.recoveryTail, selection: seed.selection,
		boundary: boundary, consumerFactSubject: seed.consumerFactSubject, evidenceBoundary: seed.evidenceBoundary,
		admissionPermitCanonical: seed.admissionPermitCanonical, ledgerDigest: seed.ledgerDigest,
		ledgerHead: seed.ledgerHead, ledgerLength: seed.ledgerLength, projectionSubject: seed.projectionSubject,
		database: seed.database, token: &runnerLedgerRetryHandoffReceiptToken{},
	}
	receipt.self = receipt
	receipt.canonical = runnerLedgerRetryHandoffReceiptDigest(receipt)
	if !validRunnerLedgerRetryHandoffReceipt(receipt) {
		return nil, empty, fail(CodeEvidenceRecoveryRequired, "runner-ledger-retry-handoff-receipt", "closed retry-handoff receipt could not be sealed", nil)
	}
	return receipt, boundary, nil
}

func runnerLedgerRetryHandoffReceiptDigest(receipt *runnerLedgerRetryHandoffClosedReceipt) [32]byte {
	if receipt == nil || receipt.self != receipt || receipt.token == nil || receipt.candidateBinding == nil ||
		receipt.generation.owner == nil || receipt.generation.owner != receipt.candidateBinding.owner ||
		receipt.recoveryDigest == ([32]byte{}) || receipt.recoveryTail.Validate() != nil ||
		!runnerLedgerRetryHandoffSelection(receipt.selection) || receipt.boundary.canonical == ([32]byte{}) ||
		receipt.boundary.canonical != runnerLedgerRetryHandoffBoundaryDigest(receipt.generation, receipt.recoveryDigest, receipt.recoveryTail, receipt.selection, receipt.boundary) ||
		receipt.consumerFactSubject.Validate() != nil || receipt.evidenceBoundary == ([32]byte{}) ||
		receipt.admissionPermitCanonical == ([32]byte{}) || receipt.ledgerDigest.Validate() != nil ||
		receipt.projectionSubject.Validate() != nil || receipt.database.postgresMajor < 15 || receipt.database.postgresMajor > 17 ||
		receipt.database.serverVersionNum == 0 || receipt.database.databaseName == "" ||
		receipt.database.sessionUser == "" || receipt.database.currentUser != MigrationOwnerRole {
		return [32]byte{}
	}
	h := sha256.New()
	h.Write([]byte(runnerLedgerRetryHandoffReceiptDigestDomain + "\x00"))
	h.Write(receipt.candidateBinding.canonical[:])
	h.Write(receipt.recoveryDigest[:])
	h.Write(receipt.evidenceBoundary[:])
	h.Write(receipt.admissionPermitCanonical[:])
	h.Write(receipt.boundary.canonical[:])
	for _, value := range []string{
		receipt.generation.executionLineageDigest.String(), receipt.generation.journalIdentityDigest.String(),
		receipt.generation.runnerProjectionDecisionDigest.String(), receipt.generation.schemaBundleDigest.String(),
		receipt.recoveryTail.String(), receipt.consumerFactSubject.String(), receipt.ledgerDigest.String(),
		receipt.ledgerHead, receipt.projectionSubject.String(), receipt.database.databaseName,
		receipt.database.sessionUser, receipt.database.currentUser,
	} {
		writeAdmissionString(h, value)
	}
	writeAdmissionUint(h, uint64(receipt.ledgerLength))
	writeAdmissionUint(h, uint64(receipt.database.postgresMajor))
	writeAdmissionUint(h, uint64(receipt.database.serverVersionNum))
	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result
}

func validRunnerLedgerRetryHandoffReceipt(receipt *runnerLedgerRetryHandoffClosedReceipt) bool {
	return receipt != nil && receipt.self == receipt && receipt.token != nil && receipt.canonical != ([32]byte{}) &&
		receipt.canonical == runnerLedgerRetryHandoffReceiptDigest(receipt)
}

func runnerLedgerRetryHandoffReceiptMatchesSeed(receipt *runnerLedgerRetryHandoffClosedReceipt, seed runnerLedgerReconciliationAdmissionSeed) bool {
	return validRunnerLedgerRetryHandoffReceipt(receipt) && receipt.candidateBinding == seed.candidateBinding &&
		sameGenerationIdentity(receipt.generation, seed.generation) && receipt.recoveryDigest == seed.recoveryDigest &&
		receipt.recoveryTail == seed.recoveryTail && receipt.selection == seed.selection &&
		receipt.consumerFactSubject == seed.consumerFactSubject && receipt.evidenceBoundary == seed.evidenceBoundary &&
		receipt.admissionPermitCanonical == seed.admissionPermitCanonical && receipt.ledgerDigest == seed.ledgerDigest &&
		receipt.ledgerHead == seed.ledgerHead && receipt.ledgerLength == seed.ledgerLength &&
		receipt.projectionSubject == seed.projectionSubject && receipt.database == seed.database
}

func mintRunnerLedgerRetryHandoffPermit(seed runnerLedgerReconciliationAdmissionSeed, receipt *runnerLedgerRetryHandoffClosedReceipt, boundary runnerLedgerRetryHandoffBoundary) (*runnerLedgerRetryHandoffPermit, error) {
	if seed.binder == nil {
		return nil, fail(CodeEvidenceRecoveryRequired, "runner-ledger-retry-handoff-permit", "retry-handoff mutation authority is unavailable or changed", nil)
	}
	binder, ok := seed.binder.(runnerLedgerRetryHandoffBinder)
	if !ok || binder == nil {
		return nil, fail(CodeEvidenceRecoveryRequired, "runner-ledger-retry-handoff-permit", "retry-handoff mutation authority is unavailable or changed", nil)
	}
	recovery := binder.RecoverySnapshot()
	derived, derivedOK := runnerLedgerRetryHandoffBoundaryFromSnapshot(recovery, seed.generation, seed.recoveryDigest, seed.recoveryTail, seed.selection)
	if !runnerLedgerRetryHandoffReceiptMatchesSeed(receipt, seed) || !derivedOK ||
		boundary.canonical == ([32]byte{}) || derived.canonical != boundary.canonical || receipt.boundary.canonical != boundary.canonical ||
		recovery == nil || recovery.cursor.valid == nil || !recovery.cursor.Valid() {
		return nil, fail(CodeEvidenceRecoveryRequired, "runner-ledger-retry-handoff-permit", "retry-handoff mutation authority is unavailable or changed", nil)
	}
	permit := &runnerLedgerRetryHandoffPermit{
		binder: binder, candidateBinding: seed.candidateBinding, generation: seed.generation,
		recoveryDigest: seed.recoveryDigest, recoveryTail: seed.recoveryTail, cursor: recovery.cursor.clone(),
		selection: seed.selection, boundary: boundary, receipt: receipt,
		consumerFactSubject: seed.consumerFactSubject, evidenceBoundary: seed.evidenceBoundary,
		admissionPermitCanonical: seed.admissionPermitCanonical, consumed: &atomic.Bool{},
	}
	permit.self = permit
	permit.canonical = runnerLedgerRetryHandoffPermitDigest(permit)
	if permit.canonical == ([32]byte{}) {
		return nil, fail(CodeEvidenceRecoveryRequired, "runner-ledger-retry-handoff-permit", "retry-handoff permit could not be identified", nil)
	}
	runnerLedgerRetryHandoffPermitRegistry.Store(permit, runnerLedgerRetryHandoffPermitRecord{
		permit: permit, binder: binder, candidateBinding: seed.candidateBinding,
		cursorValid: permit.cursor.valid, consumed: permit.consumed, canonical: permit.canonical,
	})
	if !validRunnerLedgerRetryHandoffPermit(permit) {
		runnerLedgerRetryHandoffPermitRegistry.Delete(permit)
		permit.consumed.Store(true)
		return nil, fail(CodeEvidenceRecoveryRequired, "runner-ledger-retry-handoff-permit", "retry-handoff permit could not be sealed", nil)
	}
	return permit, nil
}

func runnerLedgerRetryHandoffPermitDigest(permit *runnerLedgerRetryHandoffPermit) [32]byte {
	if permit == nil || permit.self != permit || permit.binder == nil || permit.candidateBinding == nil ||
		permit.generation.owner == nil || permit.generation.owner != permit.candidateBinding.owner ||
		permit.recoveryDigest == ([32]byte{}) || permit.recoveryTail.Validate() != nil ||
		!permit.cursor.Valid() || !sameGenerationIdentity(permit.cursor.generation, permit.generation) ||
		!runnerLedgerRetryHandoffSelection(permit.selection) || permit.boundary.canonical == ([32]byte{}) ||
		permit.boundary.canonical != runnerLedgerRetryHandoffBoundaryDigest(permit.generation, permit.recoveryDigest, permit.recoveryTail, permit.selection, permit.boundary) ||
		!validRunnerLedgerRetryHandoffReceipt(permit.receipt) || permit.receipt.candidateBinding != permit.candidateBinding ||
		!sameGenerationIdentity(permit.receipt.generation, permit.generation) || permit.receipt.recoveryDigest != permit.recoveryDigest ||
		permit.receipt.recoveryTail != permit.recoveryTail || permit.receipt.selection != permit.selection ||
		permit.receipt.boundary.canonical != permit.boundary.canonical || permit.receipt.consumerFactSubject != permit.consumerFactSubject ||
		permit.receipt.evidenceBoundary != permit.evidenceBoundary || permit.receipt.admissionPermitCanonical != permit.admissionPermitCanonical ||
		permit.consumerFactSubject.Validate() != nil || permit.evidenceBoundary == ([32]byte{}) ||
		permit.admissionPermitCanonical == ([32]byte{}) || permit.consumed == nil || permit.consumed.Load() {
		return [32]byte{}
	}
	h := sha256.New()
	h.Write([]byte(runnerLedgerRetryHandoffPermitDigestDomain + "\x00"))
	h.Write(permit.candidateBinding.canonical[:])
	h.Write(permit.recoveryDigest[:])
	h.Write(permit.evidenceBoundary[:])
	h.Write(permit.admissionPermitCanonical[:])
	h.Write(permit.boundary.canonical[:])
	h.Write(permit.receipt.canonical[:])
	for _, value := range []string{
		permit.generation.executionLineageDigest.String(), permit.generation.journalIdentityDigest.String(),
		permit.generation.runnerProjectionDecisionDigest.String(), permit.generation.schemaBundleDigest.String(),
		permit.recoveryTail.String(), permit.consumerFactSubject.String(),
	} {
		writeAdmissionString(h, value)
	}
	writeRunnerLedgerRetryHandoffCursor(h, permit.cursor)
	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result
}

func writeRunnerLedgerRetryHandoffCursor(h interface{ Write([]byte) (int, error) }, cursor JournalCursor) {
	writeAdmissionUint(h, uint64(cursor.segmentIndex))
	writeAdmissionUint(h, cursor.nextSequence)
	writeAdmissionUint(h, cursor.lineageIndexNextSequence)
	writeAdmissionString(h, cursor.lineageIndexPreviousRecordDigest.String())
	if cursor.previousRecordDigest == nil {
		writeAdmissionString(h, "previous:absent")
	} else {
		writeAdmissionString(h, "previous:present")
		writeAdmissionString(h, cursor.previousRecordDigest.String())
	}
	if cursor.latestCheckpointRecordDigest == nil {
		writeAdmissionString(h, "checkpoint:absent")
	} else {
		writeAdmissionString(h, "checkpoint:present")
		writeAdmissionString(h, cursor.latestCheckpointRecordDigest.String())
	}
}

func validRunnerLedgerRetryHandoffPermit(permit *runnerLedgerRetryHandoffPermit) bool {
	if permit == nil || permit.canonical == ([32]byte{}) || permit.canonical != runnerLedgerRetryHandoffPermitDigest(permit) {
		return false
	}
	value, ok := runnerLedgerRetryHandoffPermitRegistry.Load(permit)
	record, recordOK := value.(runnerLedgerRetryHandoffPermitRecord)
	return ok && recordOK && record.permit == permit && sameRunnerOwnedPointer(record.binder, permit.binder) &&
		record.candidateBinding == permit.candidateBinding && record.cursorValid == permit.cursor.valid &&
		record.consumed == permit.consumed && record.canonical == permit.canonical
}

func consumeRunnerLedgerRetryHandoffPermit(permit *runnerLedgerRetryHandoffPermit, binder runnerLedgerRetryHandoffBinder) (runnerLedgerRetryHandoffClaim, error) {
	var claim runnerLedgerRetryHandoffClaim
	value, ok := runnerLedgerRetryHandoffPermitRegistry.LoadAndDelete(permit)
	record, recordOK := value.(runnerLedgerRetryHandoffPermitRecord)
	valid := ok && recordOK && permit != nil && binder != nil && record.permit == permit &&
		sameRunnerOwnedPointer(record.binder, binder) && validRunnerLedgerRetryHandoffPermitWithRecord(permit, record)
	if !valid || !record.consumed.CompareAndSwap(false, true) {
		if recordOK && record.cursorValid != nil {
			record.cursorValid.Store(false)
		}
		return claim, fail(CodeEvidenceRecoveryRequired, "runner-ledger-retry-handoff-consume", "retry-handoff permit is unavailable, changed, or already consumed", nil)
	}
	claim = runnerLedgerRetryHandoffClaim{
		binder: binder, candidateBinding: permit.candidateBinding, generation: permit.generation,
		recoveryDigest: permit.recoveryDigest, recoveryTail: permit.recoveryTail, cursor: permit.cursor.clone(),
		selection: permit.selection, boundary: permit.boundary, receipt: permit.receipt, canonical: permit.canonical,
	}
	return claim, nil
}

func validRunnerLedgerRetryHandoffPermitWithRecord(permit *runnerLedgerRetryHandoffPermit, record runnerLedgerRetryHandoffPermitRecord) bool {
	if permit == nil || permit.self != permit || permit.consumed == nil || permit.consumed.Load() ||
		record.permit != permit || record.candidateBinding != permit.candidateBinding || record.cursorValid != permit.cursor.valid ||
		record.consumed != permit.consumed || record.canonical != permit.canonical || permit.canonical == ([32]byte{}) {
		return false
	}
	canonical := runnerLedgerRetryHandoffPermitDigest(permit)
	return canonical != ([32]byte{}) && canonical == permit.canonical
}

func (runner *Runner) prepareRunnerLedgerRetryHandoff(ctx context.Context, admission *runnerLedgerRetryHandoffAdmissionPermit, bundle *RuntimeBundle, _ []StatementPlan) error {
	if runner == nil || ctx == nil {
		return fail(CodeEvidenceRecoveryRequired, "runner-ledger-retry-handoff", "retry-handoff service context is unavailable", nil)
	}
	if err := contextAdmissionError(ctx); err != nil {
		return err
	}
	seed, err := claimRunnerLedgerRetryHandoffAdmissionPermit(admission)
	if err != nil {
		return err
	}
	receipt, boundary, err := runner.revalidateAndCloseRunnerLedgerRetryHandoffAdmission(ctx, seed, bundle)
	if err != nil {
		return err
	}
	permit, err := mintRunnerLedgerRetryHandoffPermit(seed, receipt, boundary)
	if err != nil {
		return err
	}
	oldCursor := permit.cursor.clone()
	active, snapshot, err := permit.binder.bindRunnerLedgerRetryHandoff(ctx, permit)
	if err != nil {
		if oldCursor.valid != nil {
			oldCursor.valid.Store(false)
		}
		return mapRunnerEvidenceSessionError(err, "runner-ledger-retry-handoff-bind")
	}
	if !runnerLedgerRetryHandoffResultMatches(permit.binder, active, snapshot, seed.candidateBinding, seed.generation, oldCursor, boundary) {
		if oldCursor.valid != nil {
			oldCursor.valid.Store(false)
		}
		if snapshot != nil && snapshot.cursor.valid != nil {
			snapshot.cursor.valid.Store(false)
		}
		if current := permit.binder.RecoverySnapshot(); current != nil && current.cursor.valid != nil {
			current.cursor.valid.Store(false)
		}
		return fail(CodeEvidenceRecoveryRequired, "runner-ledger-retry-handoff-result", "successor generation differs from the exact retry continuation", nil)
	}
	return nil
}

func runnerLedgerRetryHandoffResultMatches(binder runnerLedgerRetryHandoffBinder, active ActiveGeneration, snapshot *RecoverySnapshot, candidateBinding *verifiedEvidenceRunBinding, oldGeneration generationIdentity, oldCursor JournalCursor, boundary runnerLedgerRetryHandoffBoundary) bool {
	if binder == nil || candidateBinding == nil {
		return false
	}
	current := binder.CurrentCandidate()
	if !validOwnedCurrentCandidate(current) || current.binding != candidateBinding || oldCursor.Valid() || snapshot == nil || !snapshot.cursor.Valid() ||
		active.kind != activeGenerationCurrent || active.recoveryExecutionBindings != nil || active.identity.owner != oldGeneration.owner ||
		active.identity.executionLineageDigest != oldGeneration.executionLineageDigest ||
		active.identity.journalIdentityDigest == oldGeneration.journalIdentityDigest ||
		active.identity.runnerProjectionDecisionDigest != current.verifiedRun.runnerProjectionDecisionDigest ||
		active.identity.schemaBundleDigest != current.verifiedRun.schemaBundleDigest ||
		active.ownedDecision.owner != current.verifiedRun.currentDecision.owner ||
		active.ownedDecision.digest != current.verifiedRun.currentDecision.digest ||
		active.ownedDecision.capability.owner != current.verifiedRun.currentDecision.capability.owner ||
		!active.ownedDecision.decision.exactlyMatches(current.verifiedRun.currentDecision.decision) ||
		!validRecoverySnapshotForJournal(snapshot, active.identity, snapshot.cursor) ||
		snapshot.cursor.segmentIndex != 0 || snapshot.cursor.nextSequence != 1 || snapshot.cursor.latestCheckpointRecordDigest != nil ||
		snapshot.state != RecoveryBrandNewInherited || snapshot.nextPermittedAction != RecoveryBeginNextAttempt ||
		snapshot.migrationID == nil || *snapshot.migrationID != boundary.continuation.MigrationID ||
		snapshot.attemptIndex == nil || *snapshot.attemptIndex != boundary.continuation.AttemptIndex ||
		!equalDigestPointer(snapshot.previousAttemptTerminalDigest, boundary.continuation.PreviousAttemptTerminalDigest) ||
		snapshot.lastStatementIntent != nil || snapshot.lastIntermediateEvidence != nil || snapshot.commitIntent != nil ||
		snapshot.lastTerminal != nil || snapshot.lastResolution != nil || snapshot.lineageContinuation == nil {
		return false
	}
	continuation := snapshot.lineageContinuation
	if continuation.owner != active.identity.owner || !sameGenerationIdentity(continuation.generation, active.identity) ||
		!sameCursorIdentity(continuation.cursor, snapshot.cursor) || continuation.tailDigest != snapshot.tailDigest ||
		!runnerCanonicalEqual(continuation.value, boundary.continuation) {
		return false
	}
	boundActive := binder.ActiveGeneration()
	boundSnapshot := binder.RecoverySnapshot()
	return sameGenerationIdentity(boundActive.identity, active.identity) && sameRunnerOwnedPointer(boundActive.journal, active.journal) &&
		boundSnapshot != nil && generationJournalRecoveryDigest(boundSnapshot) == generationJournalRecoveryDigest(snapshot) &&
		sameCursorIdentity(boundSnapshot.cursor, snapshot.cursor)
}

func (s *generationEvidenceSession) bindRunnerLedgerRetryHandoff(ctx context.Context, permit *runnerLedgerRetryHandoffPermit) (ActiveGeneration, *RecoverySnapshot, error) {
	claimed, err := consumeRunnerLedgerRetryHandoffPermit(permit, s)
	if err != nil {
		return ActiveGeneration{}, nil, err
	}
	if err := contextAdmissionError(ctx); err != nil {
		return ActiveGeneration{}, nil, err
	}
	if s == nil || s.self != s {
		return ActiveGeneration{}, nil, fail(CodeEvidenceRecoveryRequired, "runner-ledger-retry-handoff-evidence", "evidence session is unavailable", nil)
	}
	s.mu.Lock()
	if !s.validLocked() || s.candidate.binding != claimed.candidateBinding || s.active.kind != activeGenerationAncestorRecovery ||
		s.active.recoveryExecutionBindings == nil || !sameGenerationIdentity(s.active.identity, claimed.generation) {
		s.mu.Unlock()
		return ActiveGeneration{}, nil, fail(CodeEvidenceRecoveryRequired, "runner-ledger-retry-handoff-evidence", "ancestor same-verifier evidence session changed", nil)
	}
	journal := s.journal
	journal.mu.Lock()
	if !journal.validLocked() || journal.state == nil || journal.state.unknown != nil ||
		!sameCursorIdentity(journal.state.cursor, claimed.cursor) ||
		generationJournalRecoveryDigest(journal.state.recovery) != claimed.recoveryDigest {
		journal.mu.Unlock()
		s.mu.Unlock()
		return ActiveGeneration{}, nil, fail(CodeEvidenceRecoveryRequired, "runner-ledger-retry-handoff-evidence", "ancestor journal boundary changed", nil)
	}
	boundary, ok := runnerLedgerRetryHandoffBoundaryFromSnapshot(journal.state.recovery, claimed.generation, claimed.recoveryDigest, claimed.recoveryTail, claimed.selection)
	if !ok || boundary.canonical != claimed.boundary.canonical || journal.schema.maxAttempts[claimed.selection.migrationID] != claimed.selection.maxAttempts ||
		int(claimed.selection.entryIndex) >= len(journal.schema.orderedMigrations) ||
		journal.schema.orderedMigrations[claimed.selection.entryIndex] != claimed.selection.migrationID {
		journal.mu.Unlock()
		s.mu.Unlock()
		return ActiveGeneration{}, nil, fail(CodeEvidenceRecoveryRequired, "runner-ledger-retry-handoff-evidence", "retryable terminal no longer matches the same-verifier schema", nil)
	}
	execution := cloneRecoveryExecutionBindings(s.active.recoveryExecutionBindings)
	journal.mu.Unlock()
	s.mu.Unlock()
	policyDigest, err := execution.policy.ComputeDigest()
	if err != nil || policyDigest != execution.subject.HistoricalRecoveryPolicyDigest {
		return ActiveGeneration{}, nil, fail(CodeEvidenceRecoveryRequired, "runner-ledger-retry-handoff-authority", "historical recovery policy changed", err)
	}
	policy := VerifiedHistoricalRecoveryPolicy{owner: execution.owner, subject: cloneProjectionValue(execution.policy), digest: policyDigest}
	evidence := &ownedCheckpointSupersessionEvidence{
		owner: claimed.generation.owner, generation: claimed.generation, tailDigest: claimed.recoveryTail,
		checkpointDigest: claimed.boundary.checkpointDigest, terminalDigest: digestPointer(claimed.boundary.terminalDigest),
		resolutionDigest: cloneDigestPointer(claimed.boundary.resolutionDigest), outcome: claimed.boundary.outcome,
		continuation: &claimed.boundary.continuation,
	}
	authority, err := bindLineageSupersession(policy, *execution, evidence)
	if err != nil {
		return ActiveGeneration{}, nil, fail(CodeEvidenceRecoveryRequired, "runner-ledger-retry-handoff-authority", "retry supersession authority could not be bound", err)
	}
	return s.ReserveAndActivateSuccessor(ctx, authority)
}

func (*generationEvidenceSession) runnerLedgerRetryHandoffBinderSealed() {}
