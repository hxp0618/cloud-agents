package migration

import "fmt"

// All witness types are package-private and contain typed owned facts. There
// is intentionally no JSON decoder or exported constructor for any of them.
type exactStatementEvidenceWitness struct {
	migrationID              string
	attemptIndex             uint32
	statementIndex           uint32
	sqlArtifactSHA256        Digest
	sqlArtifactSizeBytes     uint64
	startOffset              uint64
	endOffset                uint64
	statementSHA256          Digest
	classificationCanonical  string
	expectedTransitionDigest Digest
}

type ownedContentReceiptWitness struct {
	kind      string
	digest    Digest
	sizeBytes uint64
}
type ownedRetryIdentity struct {
	migrationID            string
	attemptIndex           uint32
	executionLineageDigest Digest
	journalIdentityDigest  Digest
}
type retryLifecycleOrderToken struct{ verifierNonce [16]byte }
type ownedLifecycleOrderAuthority struct {
	token   *retryLifecycleOrderToken
	ordinal uint64
}
type ownedRecoveryPredecessorReceipt struct {
	identity                  ownedRetryIdentity
	newLifecycleID            string
	order                     ownedLifecycleOrderAuthority
	ledgerRows                []CommitIntentLedgerRow
	ledgerPrefixDigest        Digest
	attemptPredecessorCatalog Digest
	observedCatalogDigest     Digest
	authorityResultDigest     Digest
}
type ownedRollbackReceipt struct {
	identity          ownedRetryIdentity
	oldLifecycleID    string
	order             ownedLifecycleOrderAuthority
	rollbackSucceeded bool
	oldHandleClosed   bool
}
type ownedPrecommitTerminatedReceipt struct {
	identity        ownedRetryIdentity
	oldLifecycleID  string
	order           ownedLifecycleOrderAuthority
	oldHandleClosed bool
}
type ownedCommitRejectedReceipt struct {
	identity                 ownedRetryIdentity
	oldLifecycleID           string
	order                    ownedLifecycleOrderAuthority
	oldHandleClosed          bool
	readyForQuery            bool
	commitRejectedReason     string
	commitIntentRecordDigest Digest
}
type verifiedRetryReceipt interface {
	validateRetryProof(RetryProofEvidence, AttemptTerminalState, *evidenceAttemptState, JournalHeader) error
	retryReceiptSealed()
}
type verifiedRollbackRetry struct {
	proofKind   string
	old         ownedRollbackReceipt
	predecessor ownedRecoveryPredecessorReceipt
}
type verifiedPrecommitTerminatedRetry struct {
	old         ownedPrecommitTerminatedReceipt
	predecessor ownedRecoveryPredecessorReceipt
}
type verifiedCommitRejectedRetry struct {
	old         ownedCommitRejectedReceipt
	predecessor ownedRecoveryPredecessorReceipt
}
type ownedAmbiguousBoundaryWitness struct {
	migrationID                   string
	attemptIndex                  uint32
	commitCalled                  bool
	finalIntermediateRecordDigest Digest
	commitIntentRecordDigest      Digest
}
type verifiedEvidenceChainWitness struct {
	maxAttempts         map[string]uint32
	finalStatementIndex map[string]uint32
	finalCatalogDigest  map[string]Digest
	plans               map[string]exactStatementEvidenceWitness
	runtimeReceipt      ownedContentReceiptWitness
	recoveryReceipt     ownedContentReceiptWitness
	retryReceipts       map[Digest]verifiedRetryReceipt
	ambiguousBoundaries map[Digest]ownedAmbiguousBoundaryWitness
}

type evidenceAttemptState struct {
	lastIntent       *EvidenceFrame
	lastIntermediate *EvidenceFrame
	commit           *EvidenceFrame
	terminal         *EvidenceFrame
	resolution       *EvidenceFrame
}

func validateEvidenceChainWithWitness(frames []EvidenceFrame, witness verifiedEvidenceChainWitness) error {
	replay, err := validateEvidenceChainStructure(frames)
	if err != nil {
		return err
	}
	for _, header := range replay.headers {
		if !receiptMatches(witness.runtimeReceipt, "runtime", header.OuterArtifactDigest, header.OuterArtifactSizeBytes) || !receiptMatches(witness.recoveryReceipt, "decision_recovery", header.DecisionRecoveryArtifactSHA256, header.DecisionRecoveryArtifactSizeBytes) {
			return invalidEvidence("chain", "content receipt")
		}
	}
	for _, intent := range replay.intents {
		key := evidenceStatementKey(intent.MigrationID, intent.AttemptIndex, intent.StatementIndex)
		plan, ok := witness.plans[key]
		if !ok || !planMatchesIntent(plan, intent) {
			return invalidEvidence("chain", "statement plan")
		}
	}
	for _, terminal := range replay.terminals {
		state := cloneEvidenceAttemptState(&terminal.state)
		if err := validateTerminalWitness(*terminal.frame.Record.AttemptTerminal, terminal.frame, &state, terminal.header, witness); err != nil {
			return err
		}
	}
	return nil
}

func sameJournalGeneration(a, b JournalHeader) bool {
	return a.JournalIdentityDigest == b.JournalIdentityDigest && a.ReleaseTrustDecisionDigest == b.ReleaseTrustDecisionDigest && a.RunnerProjectionDecisionDigest == b.RunnerProjectionDecisionDigest && a.ExecutionLineageDigest == b.ExecutionLineageDigest && a.OuterArtifactDigest == b.OuterArtifactDigest && a.OuterArtifactSizeBytes == b.OuterArtifactSizeBytes && a.DecisionRecoveryArtifactSHA256 == b.DecisionRecoveryArtifactSHA256 && a.DecisionRecoveryArtifactSizeBytes == b.DecisionRecoveryArtifactSizeBytes && a.ManifestDigest == b.ManifestDigest && a.RunnerReleaseDigest == b.RunnerReleaseDigest && a.SchemaBundleDigest == b.SchemaBundleDigest && a.AuthorityProfileDigest == b.AuthorityProfileDigest && a.AuthorityBindingDigest == b.AuthorityBindingDigest && a.LimitsProfile == b.LimitsProfile && a.QuotaReservationDigest == b.QuotaReservationDigest && a.ReservedRecords == b.ReservedRecords && a.ReservedBytes == b.ReservedBytes && a.ReservedSegments == b.ReservedSegments
}

func validateTerminalWitness(t AttemptTerminalState, frame EvidenceFrame, state *evidenceAttemptState, header JournalHeader, w verifiedEvidenceChainWitness) error {
	migration := t.MigrationID
	final, hasFinal := w.finalStatementIndex[migration]
	maxAttempts, hasMax := w.maxAttempts[migration]
	if !hasFinal || !hasMax || maxAttempts == 0 {
		return invalidEvidence("chain", "terminal external bounds")
	}
	requiresFinal := t.Outcome == "committed" || len(t.Outcome) >= 10 && t.Outcome[:10] == "ambiguous_"
	if requiresFinal {
		if state.lastIntermediate == nil || state.lastIntermediate.Record.Intermediate.State.StatementIndex != final {
			return invalidEvidence("chain", "final intermediate")
		}
		catalog := state.lastIntermediate.Record.Intermediate.PreledgerCatalogResult
		if catalog == nil || catalog.Digest != w.finalCatalogDigest[migration] {
			return invalidEvidence("chain", "final catalog")
		}
	}
	if t.RetryProof != nil {
		receipt, ok := w.retryReceipts[t.TerminalDigest]
		if !ok {
			return invalidEvidence("chain", "missing retry receipt")
		}
		if err := receipt.validateRetryProof(*t.RetryProof, t, state, header); err != nil {
			return err
		}
	} else if _, ok := w.retryReceipts[t.TerminalDigest]; ok {
		return invalidEvidence("chain", "unexpected retry receipt")
	}
	if len(t.Outcome) >= 10 && t.Outcome[:10] == "ambiguous_" {
		owned, ok := w.ambiguousBoundaries[t.TerminalDigest]
		if !ok || state.lastIntermediate == nil || state.commit == nil || !owned.commitCalled || owned.migrationID != migration || owned.attemptIndex != t.AttemptIndex || owned.finalIntermediateRecordDigest != state.lastIntermediate.RecordDigest || owned.commitIntentRecordDigest != state.commit.RecordDigest {
			return invalidEvidence("chain", "ambiguous boundary")
		}
	}
	if t.Outcome == "aborted_retryable" && t.AttemptIndex >= maxAttempts {
		return invalidEvidence("chain", "retry budget")
	}
	_ = frame
	return nil
}

func (verifiedRollbackRetry) retryReceiptSealed()            {}
func (verifiedPrecommitTerminatedRetry) retryReceiptSealed() {}
func (verifiedCommitRejectedRetry) retryReceiptSealed()      {}

func bindRollbackRetryReceipt(kind string, old ownedRollbackReceipt, predecessor ownedRecoveryPredecessorReceipt) (verifiedRetryReceipt, error) {
	if !stringIn(kind, "projection_transient_exact_predecessor", "precommit_rollback_exact_predecessor") || !old.rollbackSucceeded || !old.oldHandleClosed {
		return nil, invalidEvidence("retry-receipt", "rollback receipt")
	}
	if err := validateOwnedRetryInputs(old.identity, old.oldLifecycleID, old.order, predecessor); err != nil {
		return nil, err
	}
	return verifiedRollbackRetry{kind, old, predecessor}, nil
}
func bindPrecommitTerminatedRetryReceipt(old ownedPrecommitTerminatedReceipt, predecessor ownedRecoveryPredecessorReceipt) (verifiedRetryReceipt, error) {
	if !old.oldHandleClosed {
		return nil, invalidEvidence("retry-receipt", "terminated receipt")
	}
	if err := validateOwnedRetryInputs(old.identity, old.oldLifecycleID, old.order, predecessor); err != nil {
		return nil, err
	}
	return verifiedPrecommitTerminatedRetry{old, predecessor}, nil
}
func bindCommitRejectedRetryReceipt(old ownedCommitRejectedReceipt, predecessor ownedRecoveryPredecessorReceipt) (verifiedRetryReceipt, error) {
	if !old.oldHandleClosed || !old.readyForQuery || !stringIn(old.commitRejectedReason, "serialization_failure", "deadlock_detected", "other_confirmed_postgres_error") {
		return nil, invalidEvidence("retry-receipt", "commit rejected receipt")
	}
	if err := old.commitIntentRecordDigest.Validate(); err != nil {
		return nil, err
	}
	if err := validateOwnedRetryInputs(old.identity, old.oldLifecycleID, old.order, predecessor); err != nil {
		return nil, err
	}
	return verifiedCommitRejectedRetry{old, predecessor}, nil
}
func validateOwnedRetryInputs(identity ownedRetryIdentity, oldLifecycleID string, oldOrder ownedLifecycleOrderAuthority, p ownedRecoveryPredecessorReceipt) error {
	if identity != p.identity || !migrationIDPattern.MatchString(identity.migrationID) || identity.attemptIndex == 0 || oldLifecycleID == "" || p.newLifecycleID == "" || len(oldLifecycleID) > 128 || len(p.newLifecycleID) > 128 || oldLifecycleID == p.newLifecycleID {
		return invalidEvidence("retry-receipt", "lifecycle")
	}
	if oldOrder.token == nil || p.order.token == nil || oldOrder.token != p.order.token || oldOrder.ordinal == maxJSONInteger || p.order.ordinal != oldOrder.ordinal+1 {
		return invalidEvidence("retry-receipt", "verifier lifecycle order")
	}
	if err := requireEvidenceDigests(identity.executionLineageDigest, identity.journalIdentityDigest, p.ledgerPrefixDigest, p.attemptPredecessorCatalog, p.observedCatalogDigest, p.authorityResultDigest); err != nil {
		return err
	}
	ledger, err := LedgerPrefixDigest(p.ledgerRows)
	if err != nil || ledger != p.ledgerPrefixDigest || p.attemptPredecessorCatalog != p.observedCatalogDigest {
		return invalidEvidence("retry-receipt", "recovery predecessor")
	}
	return nil
}
func validateRetryCommon(identity ownedRetryIdentity, predecessor ownedRecoveryPredecessorReceipt, proof RetryProofEvidence, terminal AttemptTerminalState, header JournalHeader) error {
	if identity != predecessor.identity || identity.migrationID != terminal.MigrationID || identity.attemptIndex != terminal.AttemptIndex || identity.executionLineageDigest != header.ExecutionLineageDigest || identity.journalIdentityDigest != header.JournalIdentityDigest || predecessor.ledgerPrefixDigest != proof.LedgerPrefixDigest || predecessor.attemptPredecessorCatalog != proof.AttemptPredecessorCatalogDigest || predecessor.observedCatalogDigest != proof.ObservedCatalogDigest || predecessor.authorityResultDigest != proof.AuthorityResultDigest {
		return invalidEvidence("retry-receipt", "proof binding")
	}
	return nil
}
func (r verifiedRollbackRetry) validateRetryProof(p RetryProofEvidence, t AttemptTerminalState, _ *evidenceAttemptState, h JournalHeader) error {
	if p.ProofKind != r.proofKind || p.CommitRejectedReason != nil {
		return invalidEvidence("retry-receipt", "rollback proof kind")
	}
	return validateRetryCommon(r.old.identity, r.predecessor, p, t, h)
}
func (r verifiedPrecommitTerminatedRetry) validateRetryProof(p RetryProofEvidence, t AttemptTerminalState, _ *evidenceAttemptState, h JournalHeader) error {
	if p.ProofKind != "precommit_connection_terminated_exact_predecessor" || p.CommitRejectedReason != nil {
		return invalidEvidence("retry-receipt", "terminated proof kind")
	}
	return validateRetryCommon(r.old.identity, r.predecessor, p, t, h)
}
func (r verifiedCommitRejectedRetry) validateRetryProof(p RetryProofEvidence, t AttemptTerminalState, state *evidenceAttemptState, h JournalHeader) error {
	if p.ProofKind != "commit_rejected_exact_predecessor" || p.CommitRejectedReason == nil || *p.CommitRejectedReason != r.old.commitRejectedReason || state.commit == nil || state.commit.RecordDigest != r.old.commitIntentRecordDigest {
		return invalidEvidence("retry-receipt", "commit proof binding")
	}
	return validateRetryCommon(r.old.identity, r.predecessor, p, t, h)
}

func equalStringPointer(a, b *string) bool {
	return a == nil && b == nil || a != nil && b != nil && *a == *b
}

func predecessorAllowsNextAttempt(t AttemptTerminalState, r *AmbiguousResolutionState) bool {
	if t.Outcome == "aborted_retryable" || t.Outcome == "ambiguous_reconciled_pending" {
		return true
	}
	return t.Outcome == "ambiguous_unresolved" && r != nil && r.Outcome == "resolved_pending" && r.UnresolvedTerminalDigest == t.TerminalDigest
}
func receiptMatches(r ownedContentReceiptWitness, kind string, d Digest, size uint64) bool {
	return r.kind == kind && r.digest == d && r.sizeBytes == size
}
func projectionEvidenceEqual(a, b ProjectionResultEvidence) bool {
	left, e1 := canonicalContractKey(a)
	right, e2 := canonicalContractKey(b)
	return e1 == nil && e2 == nil && left == right
}
func planMatchesIntent(p exactStatementEvidenceWitness, i StatementIntent) bool {
	classification, e := canonicalContractKey(i.Classification)
	return e == nil && p.migrationID == i.MigrationID && p.attemptIndex == i.AttemptIndex && p.statementIndex == i.StatementIndex && p.sqlArtifactSHA256 == i.SQLArtifactSHA256 && p.sqlArtifactSizeBytes == i.SQLArtifactSizeBytes && p.startOffset == i.StartOffset && p.endOffset == i.EndOffset && p.statementSHA256 == i.StatementSHA256 && p.classificationCanonical == classification && p.expectedTransitionDigest == i.ExpectedTransitionDigest
}
func evidenceStatementKey(m string, a, s uint32) string { return fmt.Sprintf("%s:%d:%d", m, a, s) }
func equalDigestPointer(a, b *Digest) bool {
	return a == nil && b == nil || a != nil && b != nil && *a == *b
}
func evidenceAttemptIdentity(r EvidenceRecord) (string, string, uint32, error) {
	switch {
	case r.StatementIntent != nil:
		return fmt.Sprintf("%s:%d", r.StatementIntent.MigrationID, r.StatementIntent.AttemptIndex), r.StatementIntent.MigrationID, r.StatementIntent.AttemptIndex, nil
	case r.Intermediate != nil:
		s := r.Intermediate.State
		return fmt.Sprintf("%s:%d", s.MigrationID, s.AttemptIndex), s.MigrationID, s.AttemptIndex, nil
	case r.CommitIntent != nil:
		return fmt.Sprintf("%s:%d", r.CommitIntent.MigrationID, r.CommitIntent.AttemptIndex), r.CommitIntent.MigrationID, r.CommitIntent.AttemptIndex, nil
	case r.AttemptTerminal != nil:
		return fmt.Sprintf("%s:%d", r.AttemptTerminal.MigrationID, r.AttemptTerminal.AttemptIndex), r.AttemptTerminal.MigrationID, r.AttemptTerminal.AttemptIndex, nil
	case r.AmbiguousResolution != nil:
		return fmt.Sprintf("%s:%d", r.AmbiguousResolution.MigrationID, r.AmbiguousResolution.AttemptIndex), r.AmbiguousResolution.MigrationID, r.AmbiguousResolution.AttemptIndex, nil
	default:
		return "", "", 0, invalidEvidence("chain", "record identity")
	}
}
func recordMatchesHeader(r EvidenceRecord, h JournalHeader) error {
	var s, c, a, b Digest
	switch {
	case r.StatementIntent != nil:
		s, c, a, b = r.StatementIntent.SchemaBundleDigest, r.StatementIntent.CatalogContractDigest, r.StatementIntent.AuthorityProfileDigest, r.StatementIntent.AuthorityBindingDigest
	case r.Intermediate != nil:
		s, c, a, b = r.Intermediate.State.SchemaBundleDigest, r.Intermediate.State.CatalogContractDigest, r.Intermediate.State.AuthorityProfileDigest, r.Intermediate.State.AuthorityBindingDigest
	case r.CommitIntent != nil:
		s, c, a, b = r.CommitIntent.SchemaBundleDigest, r.CommitIntent.CatalogContractDigest, r.CommitIntent.AuthorityProfileDigest, r.CommitIntent.AuthorityBindingDigest
	case r.AttemptTerminal != nil:
		s, c, a, b = r.AttemptTerminal.SchemaBundleDigest, r.AttemptTerminal.CatalogContractDigest, r.AttemptTerminal.AuthorityProfileDigest, r.AttemptTerminal.AuthorityBindingDigest
	case r.AmbiguousResolution != nil:
		s, c, a, b = r.AmbiguousResolution.SchemaBundleDigest, r.AmbiguousResolution.CatalogContractDigest, r.AmbiguousResolution.AuthorityProfileDigest, r.AmbiguousResolution.AuthorityBindingDigest
	}
	_ = c
	if s != h.SchemaBundleDigest || a != h.AuthorityProfileDigest || b != h.AuthorityBindingDigest {
		return invalidEvidence("chain", "header binding")
	}
	return nil
}

type verifiedLineageChainWitness struct {
	header             LineageIndexHeader
	actualSegment0     map[Digest]EvidenceFrame
	journals           map[Digest][]EvidenceFrame
	historicalRecovery verifiedHistoricalRecoveryChain
}

func validateLineageChainWithWitness(frames []LineageIndexFrame, w verifiedLineageChainWitness) error {
	grouped := make(map[Digest][][]EvidenceFrame, len(w.journals))
	for digest, journal := range w.journals {
		segments, groupErr := logicalEvidenceSegments(journal)
		if groupErr != nil {
			return groupErr
		}
		grouped[digest] = segments
	}
	replay, err := validateLineageChainStructure(frames, w.actualSegment0, grouped)
	if err != nil {
		return err
	}
	if !lineageHeaderEqual(replay.header, w.header) {
		return invalidEvidence("lineage-chain", "header authority")
	}
	for _, structural := range replay.supersessions {
		superseded := structural.frame.Record.Superseded
		authority, ok := w.historicalRecovery.authorities[superseded.LineageSupersessionAuthorityDigest]
		if !ok {
			return invalidEvidence("lineage-chain", "authority missing")
		}
		digest, err := authority.ComputeDigest()
		var plannedContinuation *LineageContinuationContext
		if superseded.PlannedGenerationReserved != nil {
			plannedContinuation = superseded.PlannedGenerationReserved.Continuation
		}
		if err != nil || digest != superseded.LineageSupersessionAuthorityDigest || authority.ObservedOutcome != superseded.Outcome || authority.ExecutionLineageDigest != superseded.ExecutionLineageDigest || authority.OldJournalIdentityDigest != superseded.OldJournalIdentityDigest || authority.OldRunnerProjectionDecisionDigest != superseded.OldRunnerProjectionDecisionDigest || authority.OldSchemaBundleDigest != superseded.OldSchemaBundleDigest || !equalDigestPointer(authority.OldCheckpointRecordDigest, superseded.OldCheckpointRecordDigest) || !equalDigestPointer(authority.OldActivationRecordDigest, superseded.OldActivationRecordDigest) || !equalDigestPointer(authority.OldInitialJournalTailDigest, superseded.OldInitialJournalTailDigest) || !canonicalEqual(authority.Continuation, plannedContinuation) {
			return invalidEvidence("lineage-chain", "authority mismatch")
		}
		if superseded.Outcome != "activated_no_migration_progress" && (structural.checkpoint == nil || !equalDigestPointer(authority.OldTerminalDigest, structural.checkpoint.Record.Checkpoint.LastTerminalDigest) || !equalDigestPointer(authority.OldResolutionDigest, structural.checkpoint.Record.Checkpoint.LastResolutionDigest)) {
			return invalidEvidence("lineage-chain", "checkpoint authority boundary")
		}
	}
	return nil
}

type evidenceJournalSummary struct {
	recoveryState                        string
	migrationID                          *string
	attemptIndex                         *uint32
	lastStatementIntentRecordDigest      *Digest
	lastIntermediateEvidenceRecordDigest *Digest
	lastCommitIntentRecordDigest         *Digest
	lastTerminalDigest                   *Digest
	lastResolutionDigest                 *Digest
	previousAttemptTerminalDigest        *Digest
	lastIntermediateStateDigest          *Digest
}

func summarizeEvidenceJournal(frames []EvidenceFrame) (evidenceJournalSummary, error) {
	replay, err := validateEvidenceChainStructure(frames)
	if err != nil {
		return evidenceJournalSummary{}, err
	}
	return summarizeStructuralEvidenceJournal(replay)
}

func summarizeStructuralEvidenceJournal(replay *evidenceStructuralReplay) (evidenceJournalSummary, error) {
	if replay == nil || len(replay.frames) == 0 {
		return evidenceJournalSummary{}, invalidEvidence("journal-summary", "empty")
	}
	var lastIntentFrame, lastIntermediateFrame, lastCommitFrame, lastTerminalFrame, lastResolutionFrame, tailFrame *EvidenceFrame
	for index := len(replay.frames) - 1; index >= 0; index-- {
		frame := &replay.frames[index]
		if frame.RecordKind == EvidenceRecordHeader {
			continue
		}
		migration, attempt := structuralRecordAttempt(frame.Record)
		if migration == "" {
			continue
		}
		tailFrame = frame
		for scan := 0; scan <= index; scan++ {
			candidate := &replay.frames[scan]
			candidateMigration, candidateAttempt := structuralRecordAttempt(candidate.Record)
			if candidateMigration != migration || candidateAttempt != attempt {
				continue
			}
			switch candidate.RecordKind {
			case EvidenceRecordStatementIntent:
				lastIntentFrame = candidate
			case EvidenceRecordIntermediate:
				lastIntermediateFrame = candidate
			case EvidenceRecordCommitIntent:
				lastCommitFrame = candidate
			case EvidenceRecordAttemptTerminal:
				lastTerminalFrame = candidate
			case EvidenceRecordAmbiguousResolution:
				lastResolutionFrame = candidate
			}
		}
		break
	}
	summary := evidenceJournalSummary{recoveryState: "brand_new"}
	if tailFrame == nil {
		return summary, nil
	}
	if lastIntentFrame != nil {
		summary.lastStatementIntentRecordDigest = digestPointer(lastIntentFrame.RecordDigest)
	}
	if lastIntermediateFrame != nil {
		summary.lastIntermediateEvidenceRecordDigest = digestPointer(lastIntermediateFrame.RecordDigest)
		summary.lastIntermediateStateDigest = digestPointer(lastIntermediateFrame.Record.Intermediate.State.IntermediateStateDigest)
	}
	if lastCommitFrame != nil {
		summary.lastCommitIntentRecordDigest = digestPointer(lastCommitFrame.RecordDigest)
	}
	var migration string
	var attempt uint32
	switch tailFrame.RecordKind {
	case EvidenceRecordAmbiguousResolution:
		lastResolution := lastResolutionFrame.Record.AmbiguousResolution
		migration, attempt = lastResolution.MigrationID, lastResolution.AttemptIndex
		summary.lastResolutionDigest = digestPointer(lastResolution.ResolutionDigest)
		summary.previousAttemptTerminalDigest = digestPointer(lastResolution.UnresolvedTerminalDigest)
		switch lastResolution.Outcome {
		case "resolved_committed":
			summary.recoveryState = "completed"
		case "resolved_divergent":
			summary.recoveryState = "divergent"
		default:
			summary.recoveryState = "terminal"
		}
	case EvidenceRecordAttemptTerminal:
		lastTerminal := lastTerminalFrame.Record.AttemptTerminal
		migration, attempt = lastTerminal.MigrationID, lastTerminal.AttemptIndex
		summary.lastTerminalDigest = digestPointer(lastTerminal.TerminalDigest)
		summary.previousAttemptTerminalDigest = cloneDigestPointer(lastTerminal.PreviousAttemptTerminalDigest)
		switch lastTerminal.Outcome {
		case "committed", "ambiguous_reconciled_committed":
			summary.recoveryState = "completed"
		case "ambiguous_divergent":
			summary.recoveryState = "divergent"
		case "ambiguous_unresolved":
			summary.recoveryState = "ambiguous_unresolved"
		default:
			summary.recoveryState = "terminal"
		}
	case EvidenceRecordCommitIntent:
		migration, attempt = lastCommitFrame.Record.CommitIntent.MigrationID, lastCommitFrame.Record.CommitIntent.AttemptIndex
		summary.previousAttemptTerminalDigest = cloneDigestPointer(lastCommitFrame.Record.CommitIntent.PreviousAttemptTerminalDigest)
		summary.recoveryState = "dangling_commit_intent"
	case EvidenceRecordIntermediate:
		state := lastIntermediateFrame.Record.Intermediate.State
		migration, attempt = state.MigrationID, state.AttemptIndex
		summary.previousAttemptTerminalDigest = cloneDigestPointer(state.PreviousAttemptTerminalDigest)
		summary.recoveryState = "dangling_intermediate"
	case EvidenceRecordStatementIntent:
		intent := lastIntentFrame.Record.StatementIntent
		migration, attempt = intent.MigrationID, intent.AttemptIndex
		summary.previousAttemptTerminalDigest = cloneDigestPointer(intent.PreviousAttemptTerminalDigest)
		summary.recoveryState = "dangling_statement_intent"
	}
	if migration != "" {
		summary.migrationID = &migration
		summary.attemptIndex = &attempt
	}
	return summary, nil
}

func structuralRecordAttempt(record EvidenceRecord) (string, uint32) {
	switch {
	case record.StatementIntent != nil:
		return record.StatementIntent.MigrationID, record.StatementIntent.AttemptIndex
	case record.Intermediate != nil:
		return record.Intermediate.State.MigrationID, record.Intermediate.State.AttemptIndex
	case record.CommitIntent != nil:
		return record.CommitIntent.MigrationID, record.CommitIntent.AttemptIndex
	case record.AttemptTerminal != nil:
		return record.AttemptTerminal.MigrationID, record.AttemptTerminal.AttemptIndex
	case record.AmbiguousResolution != nil:
		return record.AmbiguousResolution.MigrationID, record.AmbiguousResolution.AttemptIndex
	default:
		return "", 0
	}
}

func checkpointSummaryEqual(c GenerationCheckpoint, s evidenceJournalSummary) bool {
	return c.RecoveryState == s.recoveryState && equalStringPointer(c.MigrationID, s.migrationID) && equalUint32Pointer(c.AttemptIndex, s.attemptIndex) && equalDigestPointer(c.LastStatementIntentRecordDigest, s.lastStatementIntentRecordDigest) && equalDigestPointer(c.LastIntermediateEvidenceRecordDigest, s.lastIntermediateEvidenceRecordDigest) && equalDigestPointer(c.LastCommitIntentRecordDigest, s.lastCommitIntentRecordDigest) && equalDigestPointer(c.LastTerminalDigest, s.lastTerminalDigest) && equalDigestPointer(c.LastResolutionDigest, s.lastResolutionDigest) && equalDigestPointer(c.PreviousAttemptTerminalDigest, s.previousAttemptTerminalDigest) && equalDigestPointer(c.LastIntermediateStateDigest, s.lastIntermediateStateDigest)
}

func equalUint32Pointer(a, b *uint32) bool {
	return a == nil && b == nil || a != nil && b != nil && *a == *b
}
func digestPointer(d Digest) *Digest { return &d }
func cloneDigestPointer(d *Digest) *Digest {
	if d == nil {
		return nil
	}
	copy := *d
	return &copy
}
func lineageHeaderEqual(a, b LineageIndexHeader) bool { return canonicalEqual(a, b) }
func canonicalEqual(a, b any) bool {
	left, e1 := canonicalContractKey(a)
	right, e2 := canonicalContractKey(b)
	return e1 == nil && e2 == nil && left == right
}
