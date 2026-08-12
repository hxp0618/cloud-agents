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
	if len(frames) == 0 {
		return invalidEvidence("chain", "empty")
	}
	var previous *Digest
	var header *JournalHeader
	var firstHeader *JournalHeader
	var previousSegmentFinal *Digest
	var segmentBytes uint64
	var segmentRecords uint32
	var segmentCount uint32
	attempts := map[string]*evidenceAttemptState{}
	seenIntents := map[string]bool{}
	lastTerminal := map[string]*AttemptTerminalState{}
	lastResolution := map[string]*AmbiguousResolutionState{}
	for index := range frames {
		frame := &frames[index]
		if err := frame.Validate(); err != nil {
			return err
		}
		if frame.Sequence != uint64(index) || !equalDigestPointer(frame.PreviousRecordDigest, previous) {
			return invalidEvidence("chain", "sequence or previous")
		}
		canonical, err := canonicalContractKey(*frame)
		if err != nil {
			return err
		}
		frameBytes := uint64(len(canonical)) + 8
		if frame.RecordKind == EvidenceRecordHeader {
			segmentBytes = 0
			segmentRecords = 0
			segmentCount++
		}
		segmentBytes += frameBytes
		segmentRecords++
		if validateEvidenceSegmentUsage(uint64(segmentRecords), segmentBytes) != nil || segmentCount > 16 {
			return invalidEvidence("chain", "segment byte limit")
		}
		if frame.RecordKind == EvidenceRecordHeader {
			h := frame.Record.Header
			if index == 0 {
				if h.SegmentIndex != 0 {
					return invalidEvidence("chain", "initial segment")
				}
				firstHeader = h
			} else {
				if header == nil || firstHeader == nil || h.SegmentIndex != header.SegmentIndex+1 || h.PreviousSegmentRecordDigest == nil || previousSegmentFinal == nil || *h.PreviousSegmentRecordDigest != *previousSegmentFinal || !sameJournalGeneration(*firstHeader, *h) {
					return invalidEvidence("chain", "segment header")
				}
			}
			if !receiptMatches(witness.runtimeReceipt, "runtime", h.OuterArtifactDigest, h.OuterArtifactSizeBytes) || !receiptMatches(witness.recoveryReceipt, "decision_recovery", h.DecisionRecoveryArtifactSHA256, h.DecisionRecoveryArtifactSizeBytes) {
				return invalidEvidence("chain", "content receipt")
			}
			header = h
		} else {
			if header == nil {
				return invalidEvidence("chain", "record before header")
			}
			if err := recordMatchesHeader(frame.Record, *header); err != nil {
				return err
			}
			key, migration, attempt, err := evidenceAttemptIdentity(frame.Record)
			if err != nil {
				return err
			}
			state := attempts[key]
			if state == nil {
				state = &evidenceAttemptState{}
				attempts[key] = state
			}
			switch frame.RecordKind {
			case EvidenceRecordStatementIntent:
				intent := frame.Record.StatementIntent
				if state.terminal != nil || state.commit != nil {
					return invalidEvidence("chain", "intent after closed boundary")
				}
				statementKey := evidenceStatementKey(migration, attempt, intent.StatementIndex)
				if seenIntents[statementKey] {
					return invalidEvidence("chain", "duplicate statement intent")
				}
				plan, ok := witness.plans[statementKey]
				if !ok || !planMatchesIntent(plan, *intent) {
					return invalidEvidence("chain", "statement plan")
				}
				seenIntents[statementKey] = true
				if state.lastIntent == nil {
					if intent.StatementIndex != 0 {
						return invalidEvidence("chain", "first statement index")
					}
				} else if intent.StatementIndex != state.lastIntent.Record.StatementIntent.StatementIndex+1 {
					return invalidEvidence("chain", "statement index gap")
				}
				if intent.StatementIndex == 0 {
					predecessor := lastTerminal[migration]
					if predecessor == nil {
						if attempt != 1 || intent.PreviousAttemptTerminalDigest != nil {
							return invalidEvidence("chain", "first attempt")
						}
					} else {
						if attempt != predecessor.AttemptIndex+1 || intent.PreviousAttemptTerminalDigest == nil || *intent.PreviousAttemptTerminalDigest != predecessor.TerminalDigest || !predecessorAllowsNextAttempt(*predecessor, lastResolution[migration]) {
							return invalidEvidence("chain", "attempt predecessor")
						}
					}
				} else if state.lastIntermediate == nil || intent.PreviousIntermediateStateDigest == nil || *intent.PreviousIntermediateStateDigest != state.lastIntermediate.Record.Intermediate.State.IntermediateStateDigest {
					return invalidEvidence("chain", "statement predecessor")
				}
				state.lastIntent = frame
			case EvidenceRecordIntermediate:
				inter := frame.Record.Intermediate
				if state.lastIntent == nil || state.commit != nil || state.terminal != nil || state.lastIntermediate != nil && state.lastIntermediate.Record.Intermediate.State.StatementIndex == state.lastIntent.Record.StatementIntent.StatementIndex {
					return invalidEvidence("chain", "intermediate position")
				}
				intent := state.lastIntent.Record.StatementIntent
				if inter.State.StatementIndex != intent.StatementIndex || inter.State.StatementSHA256 != intent.StatementSHA256 || !projectionEvidenceEqual(inter.AuthorityBeforeResult, intent.AuthorityBeforeResult) || !projectionEvidenceEqual(inter.CatalogBeforeResult, intent.CatalogBeforeResult) {
					return invalidEvidence("chain", "intermediate intent")
				}
				state.lastIntermediate = frame
			case EvidenceRecordCommitIntent:
				commit := frame.Record.CommitIntent
				if state.commit != nil || state.terminal != nil || state.lastIntermediate == nil {
					return invalidEvidence("chain", "commit position")
				}
				if commit.LastIntermediateStateDigest != state.lastIntermediate.Record.Intermediate.State.IntermediateStateDigest {
					return invalidEvidence("chain", "commit intermediate")
				}
				state.commit = frame
			case EvidenceRecordAttemptTerminal:
				terminal := frame.Record.AttemptTerminal
				if state.terminal != nil || state.resolution != nil {
					return invalidEvidence("chain", "second terminal")
				}
				if err := validateTerminalWitness(*terminal, *frame, state, *header, witness); err != nil {
					return err
				}
				state.terminal = frame
				lastTerminal[migration] = terminal
			case EvidenceRecordAmbiguousResolution:
				resolution := frame.Record.AmbiguousResolution
				if state.terminal == nil || state.resolution != nil || index == 0 || frames[index-1].RecordKind != EvidenceRecordAttemptTerminal {
					return invalidEvidence("chain", "resolution adjacency")
				}
				terminal := state.terminal.Record.AttemptTerminal
				if terminal.Outcome != "ambiguous_unresolved" || resolution.UnresolvedTerminalDigest != terminal.TerminalDigest || string(resolution.StableErrorCode) != *terminal.StableErrorCode {
					return invalidEvidence("chain", "resolution terminal")
				}
				state.resolution = frame
				lastResolution[migration] = resolution
			}
		}
		d := frame.RecordDigest
		previous = &d
		previousSegmentFinal = &d
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
		if state.lastIntermediate == nil || state.lastIntermediate.Record.Intermediate.State.StatementIndex != final || t.LastIntermediateStateDigest == nil || *t.LastIntermediateStateDigest != state.lastIntermediate.Record.Intermediate.State.IntermediateStateDigest {
			return invalidEvidence("chain", "final intermediate")
		}
		catalog := state.lastIntermediate.Record.Intermediate.PreledgerCatalogResult
		if catalog == nil || catalog.Digest != w.finalCatalogDigest[migration] {
			return invalidEvidence("chain", "final catalog")
		}
		if state.commit == nil || frame.Sequence != state.commit.Sequence+1 || frame.PreviousRecordDigest == nil || *frame.PreviousRecordDigest != state.commit.RecordDigest {
			return invalidEvidence("chain", "terminal commit boundary")
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
		if !ok || state.lastIntermediate == nil || state.commit == nil || !owned.commitCalled || owned.migrationID != migration || owned.attemptIndex != t.AttemptIndex || owned.finalIntermediateRecordDigest != state.lastIntermediate.RecordDigest || owned.commitIntentRecordDigest != state.commit.RecordDigest || state.commit.Sequence != state.lastIntermediate.Sequence+1 || state.commit.PreviousRecordDigest == nil || *state.commit.PreviousRecordDigest != state.lastIntermediate.RecordDigest {
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
	if len(frames) == 0 || len(frames) > 16384 {
		return invalidEvidence("lineage-chain", "empty")
	}
	var previous *Digest
	var indexBytes uint64
	var header *LineageIndexHeader
	var reservedFrame *LineageIndexFrame
	var activatedFrame *LineageIndexFrame
	var checkpointFrame *LineageIndexFrame
	var supersededFrame *LineageIndexFrame
	for i := range frames {
		f := &frames[i]
		if err := f.Validate(); err != nil {
			return err
		}
		if f.Sequence != uint64(i) || !equalDigestPointer(f.PreviousRecordDigest, previous) {
			return invalidEvidence("lineage-chain", "sequence")
		}
		canonical, err := canonicalContractKey(*f)
		if err != nil {
			return err
		}
		indexBytes += uint64(len(canonical)) + 8
		if validateLineageIndexUsage(uint64(i+1), indexBytes) != nil {
			return invalidEvidence("lineage-chain", "index byte limit")
		}
		if i == 0 {
			if f.RecordKind != LineageRecordHeader || f.Record.Header == nil || !lineageHeaderEqual(*f.Record.Header, w.header) {
				return invalidEvidence("lineage-chain", "header")
			}
			lineage, err := ExecutionLineageDigest(*f.Record.Header)
			if err != nil || lineage != f.Record.Header.ExecutionLineageDigest {
				return invalidEvidence("lineage-chain", "constituent identity")
			}
			header = f.Record.Header
		} else {
			if f.RecordKind == LineageRecordHeader {
				return invalidEvidence("lineage-chain", "second header")
			}
			if header == nil {
				return invalidEvidence("lineage-chain", "missing header")
			}
			if supersededFrame != nil && f.RecordKind != LineageRecordGenerationReserved {
				return invalidEvidence("lineage-chain", "superseded generation is closed")
			}
			switch f.RecordKind {
			case LineageRecordGenerationReserved:
				r := f.Record.Reserved
				if r.ExecutionLineageDigest != header.ExecutionLineageDigest {
					return invalidEvidence("lineage-chain", "reserved lineage")
				}
				if supersededFrame != nil {
					planned := supersededFrame.Record.Superseded.PlannedGenerationReserved
					if planned == nil || f.Sequence != supersededFrame.Sequence+1 || !canonicalEqual(*planned, *r) {
						return invalidEvidence("lineage-chain", "planned reservation")
					}
				} else if reservedFrame != nil {
					return invalidEvidence("lineage-chain", "unplanned generation")
				}
				reservedFrame = f
				activatedFrame = nil
				checkpointFrame = nil
				supersededFrame = nil
			case LineageRecordGenerationActivated:
				a := f.Record.Activated
				if reservedFrame == nil || activatedFrame != nil {
					return invalidEvidence("lineage-chain", "activation position")
				}
				r := reservedFrame.Record.Reserved
				if a.GenerationReservedRecordDigest != reservedFrame.RecordDigest || a.ExecutionLineageDigest != r.ExecutionLineageDigest || a.JournalIdentityDigest != r.JournalIdentityDigest || a.RunnerProjectionDecisionDigest != r.RunnerProjectionDecisionDigest || a.SchemaBundleDigest != r.SchemaBundleDigest || a.QuotaReservationDigest != r.QuotaReservationDigest || a.Segment0HeaderDigest != r.ExpectedSegment0HeaderDigest {
					return invalidEvidence("lineage-chain", "activation binding")
				}
				actual, ok := w.actualSegment0[a.JournalIdentityDigest]
				expected := EvidenceFrame{FormatVersion: EvidenceFrameFormat, Sequence: 0, PreviousRecordDigest: nil, RecordKind: EvidenceRecordHeader, Record: EvidenceRecord{Header: &r.PlannedSegment0Header}, RecordDigest: r.ExpectedSegment0HeaderDigest}
				if !ok || actual.Sequence != 0 || actual.PreviousRecordDigest != nil || actual.RecordKind != EvidenceRecordHeader || actual.RecordDigest != a.Segment0HeaderDigest || !canonicalEqual(actual, expected) {
					return invalidEvidence("lineage-chain", "actual header")
				}
				activatedFrame = f
			case LineageRecordGenerationCheckpoint:
				c := f.Record.Checkpoint
				if activatedFrame == nil {
					return invalidEvidence("lineage-chain", "checkpoint before activation")
				}
				a := activatedFrame.Record.Activated
				if c.ExecutionLineageDigest != a.ExecutionLineageDigest || c.JournalIdentityDigest != a.JournalIdentityDigest || c.RunnerProjectionDecisionDigest != a.RunnerProjectionDecisionDigest || c.SchemaBundleDigest != a.SchemaBundleDigest {
					return invalidEvidence("lineage-chain", "checkpoint identity")
				}
				if checkpointFrame == nil {
					if c.PreviousCheckpointRecordDigest != nil {
						return invalidEvidence("lineage-chain", "first checkpoint previous")
					}
				} else if c.PreviousCheckpointRecordDigest == nil || *c.PreviousCheckpointRecordDigest != checkpointFrame.RecordDigest {
					return invalidEvidence("lineage-chain", "checkpoint previous")
				}
				journal := w.journals[c.JournalIdentityDigest]
				if uint64(len(journal)) != c.JournalNextSequence || len(journal) == 0 || journal[len(journal)-1].RecordDigest != c.JournalTailDigest {
					return invalidEvidence("lineage-chain", "checkpoint tail")
				}
				summary, err := summarizeEvidenceJournal(journal)
				if err != nil || !checkpointSummaryEqual(*c, summary) {
					return invalidEvidence("lineage-chain", "checkpoint summary")
				}
				checkpointFrame = f
			case LineageRecordGenerationSuperseded:
				s := f.Record.Superseded
				authority, ok := w.historicalRecovery.authorities[s.LineageSupersessionAuthorityDigest]
				if !ok {
					return invalidEvidence("lineage-chain", "authority missing")
				}
				digest, err := authority.ComputeDigest()
				plannedContinuation := (*LineageContinuationContext)(nil)
				if s.PlannedGenerationReserved != nil {
					plannedContinuation = s.PlannedGenerationReserved.Continuation
				}
				if err != nil || digest != s.LineageSupersessionAuthorityDigest || authority.ObservedOutcome != s.Outcome || authority.ExecutionLineageDigest != s.ExecutionLineageDigest || authority.OldJournalIdentityDigest != s.OldJournalIdentityDigest || authority.OldRunnerProjectionDecisionDigest != s.OldRunnerProjectionDecisionDigest || authority.OldSchemaBundleDigest != s.OldSchemaBundleDigest || !equalDigestPointer(authority.OldCheckpointRecordDigest, s.OldCheckpointRecordDigest) || !equalDigestPointer(authority.OldActivationRecordDigest, s.OldActivationRecordDigest) || !equalDigestPointer(authority.OldInitialJournalTailDigest, s.OldInitialJournalTailDigest) || !canonicalEqual(authority.Continuation, plannedContinuation) {
					return invalidEvidence("lineage-chain", "authority mismatch")
				}
				if s.Outcome == "activated_no_migration_progress" {
					if activatedFrame == nil || checkpointFrame != nil || s.OldActivationRecordDigest == nil || *s.OldActivationRecordDigest != activatedFrame.RecordDigest || s.OldInitialJournalTailDigest == nil || *s.OldInitialJournalTailDigest != activatedFrame.Record.Activated.InitialJournalTailDigest {
						return invalidEvidence("lineage-chain", "header boundary")
					}
				} else if checkpointFrame == nil || s.OldCheckpointRecordDigest == nil || *s.OldCheckpointRecordDigest != checkpointFrame.RecordDigest || !equalDigestPointer(authority.OldTerminalDigest, checkpointFrame.Record.Checkpoint.LastTerminalDigest) || !equalDigestPointer(authority.OldResolutionDigest, checkpointFrame.Record.Checkpoint.LastResolutionDigest) {
					return invalidEvidence("lineage-chain", "checkpoint boundary")
				}
				supersededFrame = f
			}
		}
		d := f.RecordDigest
		previous = &d
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
	if len(frames) == 0 {
		return evidenceJournalSummary{}, invalidEvidence("journal-summary", "empty")
	}
	var previous *Digest
	var lastIntentFrame, lastIntermediateFrame, lastCommitFrame *EvidenceFrame
	var lastTerminal *AttemptTerminalState
	var lastResolution *AmbiguousResolutionState
	for index := range frames {
		frame := &frames[index]
		if err := frame.Validate(); err != nil || frame.Sequence != uint64(index) || !equalDigestPointer(frame.PreviousRecordDigest, previous) || index == 0 && frame.RecordKind != EvidenceRecordHeader || index != 0 && frame.RecordKind == EvidenceRecordHeader {
			return evidenceJournalSummary{}, invalidEvidence("journal-summary", "ordered frames")
		}
		switch frame.RecordKind {
		case EvidenceRecordStatementIntent:
			lastIntentFrame = frame
		case EvidenceRecordIntermediate:
			lastIntermediateFrame = frame
		case EvidenceRecordCommitIntent:
			lastCommitFrame = frame
		case EvidenceRecordAttemptTerminal:
			lastTerminal = frame.Record.AttemptTerminal
		case EvidenceRecordAmbiguousResolution:
			lastResolution = frame.Record.AmbiguousResolution
		}
		d := frame.RecordDigest
		previous = &d
	}
	summary := evidenceJournalSummary{recoveryState: "brand_new"}
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
	switch {
	case lastResolution != nil:
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
	case lastTerminal != nil:
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
	case lastCommitFrame != nil:
		migration, attempt = lastCommitFrame.Record.CommitIntent.MigrationID, lastCommitFrame.Record.CommitIntent.AttemptIndex
		summary.previousAttemptTerminalDigest = cloneDigestPointer(lastCommitFrame.Record.CommitIntent.PreviousAttemptTerminalDigest)
		summary.recoveryState = "dangling_commit_intent"
	case lastIntermediateFrame != nil:
		state := lastIntermediateFrame.Record.Intermediate.State
		migration, attempt = state.MigrationID, state.AttemptIndex
		summary.previousAttemptTerminalDigest = cloneDigestPointer(state.PreviousAttemptTerminalDigest)
		summary.recoveryState = "dangling_intermediate"
	case lastIntentFrame != nil:
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
