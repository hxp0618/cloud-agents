package migration

import (
	"context"
	"crypto/sha256"
	"sync"
)

// activeGenerationBinding makes ActiveGeneration an immutable copyable value.
// Copies retain the same opaque binding and are revoked together when the
// owning session closes; no address-of-value identity is used as authority.
type activeGenerationBinding struct {
	session          *generationEvidenceSession
	journal          *generationEvidenceJournal
	candidateBinding *verifiedEvidenceRunBinding
	runtimeBinding   *verifiedContentReceiptBinding
	recoveryBinding  *verifiedDecisionRecoveryReceiptBinding
	executionBinding *VerifiedRecoveryExecutionBindings
	canonical        [32]byte
}

type activeGenerationRegistryRecord struct {
	binding          *activeGenerationBinding
	session          *generationEvidenceSession
	journal          *generationEvidenceJournal
	candidateBinding *verifiedEvidenceRunBinding
	runtimeBinding   *verifiedContentReceiptBinding
	recoveryBinding  *verifiedDecisionRecoveryReceiptBinding
	executionBinding *VerifiedRecoveryExecutionBindings
	canonical        [32]byte
}

var activeGenerationRegistry sync.Map

// generationEvidenceSession is the first concrete normal-run session. It
// owns exactly one current candidate and one retained generation journal. It
// deliberately has no database, runner, successor reservation, or filesystem
// reacquisition authority in this slice.
type generationEvidenceSession struct {
	self      *generationEvidenceSession
	mu        sync.Mutex
	candidate OwnedCurrentCandidate
	journal   *generationEvidenceJournal
	active    ActiveGeneration
	binding   *generationEvidenceSessionBinding
	closed    bool
}

type generationEvidenceSessionBinding struct {
	session          *generationEvidenceSession
	journal          *generationEvidenceJournal
	candidateBinding *verifiedEvidenceRunBinding
	activeBinding    *activeGenerationBinding
	canonical        [32]byte
}

type generationEvidenceSessionRegistryRecord struct {
	session          *generationEvidenceSession
	binding          *generationEvidenceSessionBinding
	journal          *generationEvidenceJournal
	candidateBinding *verifiedEvidenceRunBinding
	activeBinding    *activeGenerationBinding
	canonical        [32]byte
}

var generationEvidenceSessionRegistry sync.Map

var _ EvidenceSession = (*generationEvidenceSession)(nil)

// BindSession consumes same-verifier recovery authority through BindJournal,
// verifies one current replay, and seals the current ActiveGeneration and
// EvidenceSession together. Any failure after journal construction closes the
// retained filesystem lease before returning.
func (r *GenerationRecoveryReady) BindSession(ctx context.Context, candidate OwnedCurrentCandidate) (EvidenceSession, error) {
	authority, err := r.BindJournal(ctx, candidate)
	if err != nil {
		return nil, err
	}
	return bindGenerationEvidenceSession(ctx, authority, candidate, candidate.verifiedRun.currentDecision, nil)
}

// BindSession consumes a registered-generation recovery handoff through the
// shared journal. A generation signed by the current decision remains current;
// an older decision can enter only with the exact same-verifier recovery
// execution binding minted during handoff.
func (r *RegisteredGenerationRecoveryReady) BindSession(ctx context.Context, candidate OwnedCurrentCandidate) (EvidenceSession, error) {
	if r == nil || !validRegisteredGenerationRecoveryReady(r, candidate) {
		return nil, fail(CodeEvidenceRecoveryRequired, "registered-evidence-session-bind", "registered generation recovery authority is unavailable", nil)
	}
	decision := ownedVerifiedDecisionCopy(r.registered.decision, r.registered.bindings)
	execution := cloneRecoveryExecutionBindings(r.executionBindings)
	authority, err := r.BindJournal(ctx, candidate)
	if err != nil {
		return nil, err
	}
	return bindGenerationEvidenceSession(ctx, authority, candidate, decision, execution)
}

func bindGenerationEvidenceSession(ctx context.Context, authority EvidenceJournal, candidate OwnedCurrentCandidate, decision OwnedVerifiedDecision, execution *VerifiedRecoveryExecutionBindings) (EvidenceSession, error) {
	journal, ok := authority.(*generationEvidenceJournal)
	if !ok || journal == nil {
		if authority != nil {
			if cleanupErr := authority.Close(context.Background()); cleanupErr != nil {
				return nil, cleanupErr
			}
		}
		return nil, admissionFailed("evidence-session-bind", "concrete journal authority is unavailable", nil)
	}
	if _, _, err := journal.Replay(ctx); err != nil {
		if cleanupErr := journal.Close(context.Background()); cleanupErr != nil {
			return nil, cleanupErr
		}
		return nil, err
	}
	ownedCandidate, err := cloneSessionCandidate(candidate)
	if err != nil {
		if cleanupErr := journal.Close(context.Background()); cleanupErr != nil {
			return nil, cleanupErr
		}
		return nil, err
	}
	activeKind := activeGenerationCurrent
	if decision.digest == ownedCandidate.verifiedRun.currentDecision.digest {
		if execution != nil || !decision.decision.exactlyMatches(ownedCandidate.verifiedRun.currentDecision.decision) {
			if cleanupErr := journal.Close(context.Background()); cleanupErr != nil {
				return nil, cleanupErr
			}
			return nil, fail(CodeEvidenceRecoveryRequired, "evidence-session-bind", "current generation decision binding is invalid", nil)
		}
		decision = ownedCandidate.verifiedRun.currentDecision
	} else {
		activeKind = activeGenerationAncestorRecovery
		if execution == nil || journal.registeredPrior == nil || !sameRecoveryExecutionBindings(execution, journal.registeredPrior.executionBindings, journal.generation, ownedCandidate.verifiedRun.currentDecision.digest) {
			if cleanupErr := journal.Close(context.Background()); cleanupErr != nil {
				return nil, cleanupErr
			}
			return nil, fail(CodeEvidenceRecoveryRequired, "evidence-session-bind", "ancestor recovery execution binding is unavailable", nil)
		}
	}
	session := &generationEvidenceSession{candidate: ownedCandidate, journal: journal}
	session.self = session
	active := ActiveGeneration{
		identity: journal.generation, kind: activeKind, journal: journal,
		ownedDecision:  decision,
		contentReceipt: journal.runtimeReceipt, decisionRecoveryReceipt: journal.recoveryReceipt,
		recoveryExecutionBindings: cloneRecoveryExecutionBindings(execution),
	}
	activeBinding := &activeGenerationBinding{
		session: session, journal: journal, candidateBinding: ownedCandidate.binding,
		runtimeBinding: journal.runtimeReceipt.binding, recoveryBinding: journal.recoveryReceipt.binding,
		executionBinding: active.recoveryExecutionBindings,
	}
	active.binding = activeBinding
	activeBinding.canonical = activeGenerationDigest(active)
	session.active = active
	session.binding = &generationEvidenceSessionBinding{
		session: session, journal: journal, candidateBinding: ownedCandidate.binding, activeBinding: activeBinding,
	}
	session.binding.canonical = generationEvidenceSessionDigest(session)
	activeGenerationRegistry.Store(activeBinding, activeGenerationRegistryRecord{
		binding: activeBinding, session: session, journal: journal, candidateBinding: ownedCandidate.binding,
		runtimeBinding: journal.runtimeReceipt.binding, recoveryBinding: journal.recoveryReceipt.binding,
		executionBinding: active.recoveryExecutionBindings,
		canonical:        activeBinding.canonical,
	})
	generationEvidenceSessionRegistry.Store(session, generationEvidenceSessionRegistryRecord{
		session: session, binding: session.binding, journal: journal, candidateBinding: ownedCandidate.binding,
		activeBinding: activeBinding, canonical: session.binding.canonical,
	})
	session.mu.Lock()
	valid := session.validLocked()
	session.mu.Unlock()
	if !valid {
		activeGenerationRegistry.Delete(activeBinding)
		generationEvidenceSessionRegistry.Delete(session)
		session.closed = true
		if cleanupErr := journal.Close(context.Background()); cleanupErr != nil {
			return nil, cleanupErr
		}
		return nil, admissionFailed("evidence-session-bind", "session authority could not be sealed", nil)
	}
	return session, nil
}

func cloneRecoveryExecutionBindings(value *VerifiedRecoveryExecutionBindings) *VerifiedRecoveryExecutionBindings {
	if value == nil {
		return nil
	}
	result := *value
	result.snapshot = cloneRecoverySnapshot(value.snapshot)
	result.policy = cloneProjectionValue(value.policy)
	result.subject = cloneProjectionValue(value.subject)
	return &result
}

func sameRecoveryExecutionBindings(left, right *VerifiedRecoveryExecutionBindings, generation generationIdentity, currentDecision Digest) bool {
	if left == nil || right == nil || left.owner == nil || left.session == nil || left.digest.Validate() != nil || currentDecision.Validate() != nil || left.owner != right.owner || left.session != right.session || !sameGenerationIdentity(left.generation, generation) || !sameGenerationIdentity(right.generation, generation) || left.tailDigest != right.tailDigest || left.digest != right.digest || !canonicalEqual(left.policy, right.policy) || !canonicalEqual(left.subject, right.subject) || left.snapshot == nil || right.snapshot == nil || left.tailDigest != left.snapshot.tailDigest || right.tailDigest != right.snapshot.tailDigest || !sameCursorIdentity(left.snapshot.cursor, right.snapshot.cursor) || !validRecoverySnapshotForJournal(left.snapshot, generation, left.snapshot.cursor) || !validRecoverySnapshotForJournal(right.snapshot, generation, right.snapshot.cursor) || generationJournalRecoveryDigest(left.snapshot) != generationJournalRecoveryDigest(right.snapshot) {
		return false
	}
	leftPolicy, leftPolicyErr := left.policy.ComputeDigest()
	leftSubject, leftSubjectErr := left.subject.ComputeDigest()
	rightPolicy, rightPolicyErr := right.policy.ComputeDigest()
	rightSubject, rightSubjectErr := right.subject.ComputeDigest()
	return leftPolicyErr == nil && leftSubjectErr == nil && rightPolicyErr == nil && rightSubjectErr == nil &&
		leftPolicy == left.subject.HistoricalRecoveryPolicyDigest && rightPolicy == right.subject.HistoricalRecoveryPolicyDigest && leftSubject == left.digest && rightSubject == right.digest &&
		left.policy.ExecutionLineageDigest == generation.executionLineageDigest && left.policy.OldJournalIdentityDigest == generation.journalIdentityDigest && left.policy.OldRunnerProjectionDecisionDigest == generation.runnerProjectionDecisionDigest && left.policy.OldSchemaBundleDigest == generation.schemaBundleDigest && left.policy.SuccessorRunnerProjectionDecisionDigest == currentDecision &&
		left.subject.ExecutionLineageDigest == generation.executionLineageDigest && left.subject.CurrentRunnerProjectionDecisionDigest == currentDecision && left.subject.OldRunnerProjectionDecisionDigest == generation.runnerProjectionDecisionDigest && left.subject.OldJournalIdentityDigest == generation.journalIdentityDigest && left.subject.OldSchemaBundleDigest == generation.schemaBundleDigest && left.subject.OldDecisionRecoveryArtifactSHA256 == left.policy.OldDecisionRecoveryArtifactSHA256 && left.subject.OldDecisionRecoveryArtifactSizeBytes == left.policy.OldDecisionRecoveryArtifactSizeBytes && left.subject.OldJournalReplayTailDigest == left.tailDigest && left.subject.OldRecoveryState == string(left.snapshot.state)
}

func cloneSessionCandidate(candidate OwnedCurrentCandidate) (OwnedCurrentCandidate, error) {
	if !validOwnedCurrentCandidate(candidate) {
		return OwnedCurrentCandidate{}, fail(CodeEvidenceRecoveryRequired, "evidence-session-bind", "current candidate authority is unavailable", nil)
	}
	bindings, err := candidate.verifiedRun.currentDecision.decision.runnerProjectionBindings()
	if err != nil {
		return OwnedCurrentCandidate{}, fail(CodeEvidenceRecoveryRequired, "evidence-session-bind", "current decision bindings are unavailable", nil)
	}
	result := candidate
	result.verifiedRun.currentDecision = ownedVerifiedDecisionCopy(candidate.verifiedRun.currentDecision, bindings)
	result.verifiedRun.decisionRecoveryArtifact = ownedDecisionRecoveryArtifactCopy(candidate.verifiedRun.decisionRecoveryArtifact)
	result.runtimeArtifact.bytes = append([]byte(nil), candidate.runtimeArtifact.bytes...)
	result.decisionRecoveryArtifact = ownedDecisionRecoveryArtifactCopy(candidate.decisionRecoveryArtifact)
	if !validOwnedCurrentCandidate(result) {
		return OwnedCurrentCandidate{}, fail(CodeEvidenceRecoveryRequired, "evidence-session-bind", "owned current candidate could not be copied", nil)
	}
	return result, nil
}

func (s *generationEvidenceSession) CurrentCandidate() OwnedCurrentCandidate {
	if s == nil || s.self != s {
		return OwnedCurrentCandidate{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.validLocked() {
		return OwnedCurrentCandidate{}
	}
	candidate, err := cloneSessionCandidate(s.candidate)
	if err != nil {
		return OwnedCurrentCandidate{}
	}
	return candidate
}

func (s *generationEvidenceSession) ActiveGeneration() ActiveGeneration {
	if s == nil || s.self != s {
		return ActiveGeneration{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.validLocked() {
		return ActiveGeneration{}
	}
	return s.active
}

func (s *generationEvidenceSession) Journal() EvidenceJournal {
	if s == nil || s.self != s {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.validLocked() {
		return nil
	}
	return s.journal
}

// RecoverySnapshot always clones the journal's current state. An append makes
// every older cursor invalid, so a session must never cache the snapshot that
// existed when it was first sealed.
func (s *generationEvidenceSession) RecoverySnapshot() *RecoverySnapshot {
	if s == nil || s.self != s {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.validLocked() {
		return nil
	}
	s.journal.mu.Lock()
	defer s.journal.mu.Unlock()
	if !s.journal.validLocked() || s.journal.state.unknown != nil {
		return nil
	}
	return cloneRecoverySnapshot(s.journal.state.recovery)
}

func (s *generationEvidenceSession) ReserveAndActivateSuccessor(ctx context.Context, authority *VerifiedLineageSupersessionAuthority) (ActiveGeneration, *RecoverySnapshot, error) {
	if s == nil || s.self != s {
		return ActiveGeneration{}, nil, admissionFailed("evidence-session-successor", "session authority is unavailable", nil)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.validLocked() {
		return ActiveGeneration{}, nil, admissionFailed("evidence-session-successor", "session authority is invalid", nil)
	}
	if err := contextAdmissionError(ctx); err != nil {
		return ActiveGeneration{}, nil, err
	}
	if authority == nil {
		return ActiveGeneration{}, nil, fail(CodeEvidenceRecoveryRequired, "evidence-session-successor", "lineage supersession authority is unavailable", nil)
	}
	// Do not consume the one-shot supersession authority until the Store-bound
	// full-root reacquisition and adjacent Superseded -> Reserved transition are
	// implemented as one closed operation.
	return ActiveGeneration{}, nil, fail(CodeProjectionNotImplemented, "evidence-session-successor", "successor filesystem handoff is not implemented", nil)
}

func (s *generationEvidenceSession) Close(ctx context.Context) error {
	if s == nil || s.self != s {
		return admissionFailed("evidence-session-close", "session authority is unavailable", nil)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	registered, ok := generationEvidenceSessionRegistry.Load(s)
	record, recordOK := registered.(generationEvidenceSessionRegistryRecord)
	if s.closed || !ok || !recordOK || record.session != s || record.binding == nil || record.journal == nil || record.activeBinding == nil || record.canonical == ([32]byte{}) {
		return admissionFailed("evidence-session-close", "immutable session authority is unavailable", nil)
	}
	s.closed = true
	generationEvidenceSessionRegistry.Delete(s)
	activeGenerationRegistry.Delete(record.activeBinding)
	return record.journal.Close(ctx)
}

func (s *generationEvidenceSession) evidenceSessionSealed() {}

func (s *generationEvidenceSession) validLocked() bool {
	if s == nil || s.self != s || s.closed || s.binding == nil || s.binding.session != s || s.journal == nil || s.binding.journal != s.journal || s.candidate.binding == nil || s.binding.candidateBinding != s.candidate.binding || s.active.binding == nil || s.binding.activeBinding != s.active.binding || !validOwnedCurrentCandidate(s.candidate) || s.binding.canonical == ([32]byte{}) || s.binding.canonical != generationEvidenceSessionDigest(s) {
		return false
	}
	registered, ok := generationEvidenceSessionRegistry.Load(s)
	record, recordOK := registered.(generationEvidenceSessionRegistryRecord)
	if !ok || !recordOK || record.session != s || record.binding != s.binding || record.journal != s.journal || record.candidateBinding != s.candidate.binding || record.activeBinding != s.active.binding || record.canonical != s.binding.canonical {
		return false
	}
	return validSessionActiveGeneration(s.active, s)
}

func validSessionActiveGeneration(active ActiveGeneration, session *generationEvidenceSession) bool {
	journal, ok := active.journal.(*generationEvidenceJournal)
	if !ok || session == nil || active.binding == nil || active.binding.session != session || active.binding.journal != journal || active.binding.candidateBinding != session.candidate.binding || active.binding.runtimeBinding != active.contentReceipt.binding || active.binding.recoveryBinding != active.decisionRecoveryReceipt.binding || active.binding.executionBinding != active.recoveryExecutionBindings || journal != session.journal || journal.registeredPrior != nil && journal.registeredPrior.registered == nil || !sameGenerationIdentity(active.identity, journal.generation) || active.identity.owner != session.candidate.owner || active.ownedDecision.owner != session.candidate.verifiedRun.currentDecision.owner || active.ownedDecision.capability.owner != session.candidate.verifiedRun.currentDecision.capability.owner || active.ownedDecision.digest != active.identity.runnerProjectionDecisionDigest || active.contentReceipt.binding != journal.runtimeReceipt.binding || active.decisionRecoveryReceipt.binding != journal.recoveryReceipt.binding || active.binding.canonical == ([32]byte{}) || active.binding.canonical != activeGenerationDigest(active) {
		return false
	}
	header, headerOK := generationJournalHeader(journal)
	if !headerOK || !validGenerationJournalReceiptPair(active.contentReceipt, active.decisionRecoveryReceipt, active.identity.owner, header) {
		return false
	}
	switch active.kind {
	case activeGenerationCurrent:
		bindings, err := session.candidate.verifiedRun.currentDecision.decision.runnerProjectionBindings()
		if err != nil || active.recoveryExecutionBindings != nil || journal.registeredPrior != nil && journal.registeredPrior.registered.policy != nil || active.ownedDecision.digest != session.candidate.verifiedRun.currentDecision.digest || !active.ownedDecision.decision.exactlyMatches(session.candidate.verifiedRun.currentDecision.decision) || !validOwnedCurrentDecision(active.ownedDecision, bindings) {
			return false
		}
	case activeGenerationAncestorRecovery:
		currentBindings, err := session.candidate.verifiedRun.currentDecision.decision.runnerProjectionBindings()
		if err != nil || journal.registeredPrior == nil || journal.registeredPrior.registered == nil || journal.registeredPrior.registered.policy == nil || active.recoveryExecutionBindings == nil || active.ownedDecision.digest == session.candidate.verifiedRun.currentDecision.digest || active.ownedDecision.digest != journal.registeredPrior.registered.decision.digest || !active.ownedDecision.decision.exactlyMatches(journal.registeredPrior.registered.decision.decision) || active.ownedDecision.decision.validateHistorical(journal.registeredPrior.registered.bindings) != nil || active.recoveryExecutionBindings.policy.RecoveryPolicySubjectDigest != currentBindings.recoveryPolicySubjectDigest || active.recoveryExecutionBindings.policy.SuccessorSchemaBundleDigest != session.candidate.verifiedRun.schemaBundleDigest || !sameRecoveryExecutionBindings(active.recoveryExecutionBindings, journal.registeredPrior.executionBindings, active.identity, session.candidate.verifiedRun.currentDecision.digest) {
			return false
		}
	default:
		return false
	}
	registered, registryOK := activeGenerationRegistry.Load(active.binding)
	record, recordOK := registered.(activeGenerationRegistryRecord)
	if !registryOK || !recordOK || record.binding != active.binding || record.session != session || record.journal != journal || record.candidateBinding != session.candidate.binding || record.runtimeBinding != active.contentReceipt.binding || record.recoveryBinding != active.decisionRecoveryReceipt.binding || record.executionBinding != active.recoveryExecutionBindings || record.canonical != active.binding.canonical {
		return false
	}
	journal.mu.Lock()
	defer journal.mu.Unlock()
	return journal.validLocked()
}

func activeGenerationDigest(active ActiveGeneration) [32]byte {
	journal, ok := active.journal.(*generationEvidenceJournal)
	if !ok || journal == nil || journal.binding == nil || journal.binding.canonical == ([32]byte{}) || active.identity.owner == nil || active.ownedDecision.owner == nil || active.ownedDecision.digest.Validate() != nil || active.contentReceipt.binding == nil || active.decisionRecoveryReceipt.binding == nil || active.contentReceipt.digest.Validate() != nil || active.decisionRecoveryReceipt.digest.Validate() != nil || active.contentReceipt.sizeBytes == 0 || active.decisionRecoveryReceipt.sizeBytes == 0 || active.kind == activeGenerationCurrent && active.recoveryExecutionBindings != nil || active.kind == activeGenerationAncestorRecovery && (active.recoveryExecutionBindings == nil || active.recoveryExecutionBindings.digest.Validate() != nil) || active.kind != activeGenerationCurrent && active.kind != activeGenerationAncestorRecovery {
		return [32]byte{}
	}
	h := sha256.New()
	h.Write([]byte("cloud-agents-platform-active-generation/v2\x00"))
	h.Write(journal.binding.canonical[:])
	writeAdmissionString(h, string(active.kind))
	for _, value := range []Digest{active.identity.executionLineageDigest, active.identity.journalIdentityDigest, active.identity.runnerProjectionDecisionDigest, active.identity.schemaBundleDigest, active.ownedDecision.digest, active.contentReceipt.digest, active.decisionRecoveryReceipt.digest} {
		writeAdmissionString(h, value.String())
	}
	writeAdmissionString(h, string(active.contentReceipt.kind))
	writeAdmissionUint(h, active.contentReceipt.sizeBytes)
	writeAdmissionString(h, string(active.decisionRecoveryReceipt.kind))
	writeAdmissionUint(h, active.decisionRecoveryReceipt.sizeBytes)
	if active.recoveryExecutionBindings == nil {
		writeAdmissionString(h, "current")
	} else {
		writeAdmissionString(h, "historical")
		writeAdmissionString(h, active.recoveryExecutionBindings.digest.String())
	}
	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result
}

func generationEvidenceSessionDigest(session *generationEvidenceSession) [32]byte {
	if session == nil || session.self != session || session.journal == nil || session.journal.binding == nil || session.journal.binding.canonical == ([32]byte{}) || session.candidate.binding == nil || session.active.binding == nil || session.active.binding.canonical == ([32]byte{}) {
		return [32]byte{}
	}
	h := sha256.New()
	h.Write([]byte("cloud-agents-platform-evidence-session/v1\x00"))
	h.Write(session.candidate.binding.canonical[:])
	h.Write(session.journal.binding.canonical[:])
	h.Write(session.active.binding.canonical[:])
	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result
}
