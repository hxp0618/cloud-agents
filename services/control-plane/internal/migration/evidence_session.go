package migration

import (
	"context"
	"crypto/sha256"
	"errors"
	"sync"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/evidencefs"
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

// generationEvidenceSession is the concrete normal-run session. It owns one
// current candidate and one retained generation journal. Its successor method
// is the only reviewed migration-side orchestration seam for the irreversible
// generation-lease -> full-root admission -> successor-generation handoff; it
// still owns no database, runner, connection, or transaction capability.
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

// BindSession consumes an activated crash-recovered successor. A historical B
// first takes the dedicated header-only B -> C supersession graph; a current B
// enters the existing journal/session binder directly.
func (r *HistoricalSuccessorGenerationRecoveryReady) BindSession(ctx context.Context, candidate OwnedCurrentCandidate) (EvidenceSession, error) {
	if r == nil || !validHistoricalSuccessorGenerationRecoveryReady(r, candidate) {
		return nil, fail(CodeEvidenceRecoveryRequired, "historical-successor-evidence-session-bind", "historical successor recovery authority is unavailable", nil)
	}
	if r.requiresSupersession {
		return r.bindSupersededSession(ctx, candidate)
	}
	authority, err := r.BindJournal(ctx, candidate)
	if err != nil {
		return nil, err
	}
	return bindGenerationEvidenceSession(ctx, authority, candidate, candidate.verifiedRun.currentDecision, nil)
}

// bindSupersededSession is the sole production consumer of the private
// crash-reopen B -> C bridge. Each step consumes the prior concrete owner, so
// neither a partially reacquired admission nor an intermediate authority can
// escape this call.
func (r *HistoricalSuccessorGenerationRecoveryReady) bindSupersededSession(ctx context.Context, candidate OwnedCurrentCandidate) (EvidenceSession, error) {
	supersession, err := r.bindHeaderOnlySupersession(candidate)
	if err != nil {
		return nil, err
	}
	if supersession == nil {
		return nil, admissionFailed("historical-successor-session-supersession", "historical successor supersession authority is unavailable", nil)
	}
	admission, err := supersession.reacquireAdmission(ctx, candidate)
	if err != nil {
		return nil, err
	}
	if admission == nil {
		return nil, admissionFailed("historical-successor-session-reacquire", "historical successor admission authority is unavailable", nil)
	}
	plan, err := admission.bindSuccessorPlan(ctx, candidate)
	if err != nil {
		return nil, err
	}
	if plan == nil {
		return nil, admissionFailed("historical-successor-session-plan", "historical successor plan authority is unavailable", nil)
	}
	permit, err := plan.bindPermit(ctx, candidate)
	if err != nil {
		return nil, err
	}
	if permit == nil {
		return nil, admissionFailed("historical-successor-session-permit", "historical successor permit authority is unavailable", nil)
	}
	generation, err := permit.materializeSuccessor(ctx, candidate)
	if err != nil {
		return nil, err
	}
	if generation == nil {
		return nil, admissionFailed("historical-successor-session-generation", "historical successor generation authority is unavailable", nil)
	}
	return generation.bindSession(ctx, candidate)
}

// bindSession consumes the crash-reopen generation-ready owner through the
// already-reviewed successor handoff, replay, same-verifier recovery, journal,
// and session graph. It never exposes an intermediate retained-lock authority.
func (r *historicalSuccessorAdmissionGenerationReady) bindSession(ctx context.Context, candidate OwnedCurrentCandidate) (session EvidenceSession, resultErr error) {
	if !validHistoricalSuccessorAdmissionGenerationReady(r, candidate) {
		return nil, fail(CodeEvidenceRecoveryRequired, "historical-successor-session", "historical successor generation authority is unavailable", nil)
	}
	if err := contextAdmissionError(ctx); err != nil {
		return nil, err
	}
	if !r.consumed.CompareAndSwap(false, true) {
		return nil, fail(CodeEvidenceRecoveryRequired, "historical-successor-session", "historical successor generation authority was already consumed", nil)
	}
	historicalSuccessorAdmissionGenerationRegistry.Delete(r)
	cleanup := sessionSuccessorCleanup{admission: r.admission, history: r.history, plan: r.plan, state: r.state}
	defer func() {
		if cleanup.committed {
			return
		}
		if cleanupErr := cleanup.close(); cleanupErr != nil {
			resultErr = cleanupErr
		}
	}()

	handoffResult, err := r.generation.Handoff(ctx, candidate)
	if err != nil {
		return nil, err
	}
	handoff := handoffResult.Next()
	if handoffResult.Outcome() != evidencefs.AdmissionTransitionDurable || handoff == nil {
		return nil, admissionFailed("historical-successor-session-handoff", "durable successor handoff authority is unavailable", nil)
	}
	cleanup.admission = nil
	cleanup.handoff = handoff

	replayResult, err := handoff.Replay(ctx, candidate)
	if err != nil {
		return nil, err
	}
	replay := replayResult.Next()
	if replay == nil {
		_, cleanupErr := handoff.failSuccessorReplay(evidencefs.ErrUnknown, "historical-successor-session-replay-cleanup")
		if cleanupErr != nil {
			return nil, cleanupErr
		}
		cleanup.handoff = nil
		return nil, admissionFailed("historical-successor-session-replay", "successor replay authority is unavailable", nil)
	}
	cleanup.replay = replay

	recovery, err := replay.BindRecovery(ctx, candidate)
	if err != nil {
		return nil, err
	}
	if recovery == nil {
		if cleanupErr := closeSuccessorGenerationReplay(replay, "historical-successor-session-recovery-cleanup"); cleanupErr != nil {
			return nil, cleanupErr
		}
		cleanup.replay = nil
		return nil, admissionFailed("historical-successor-session-recovery", "successor recovery authority is unavailable", nil)
	}
	cleanup.recovery = recovery

	journalAuthority, err := recovery.BindJournal(ctx, candidate)
	if err != nil {
		return nil, err
	}
	journal, ok := journalAuthority.(*generationEvidenceJournal)
	if !ok || journal == nil {
		if journalAuthority == nil {
			if cleanupErr := closeConsumedSuccessorGenerationRecovery(recovery, "historical-successor-session-journal-cleanup"); cleanupErr != nil {
				return nil, cleanupErr
			}
			cleanup.recovery = nil
		} else if cleanupErr := journalAuthority.Close(context.Background()); cleanupErr != nil {
			return nil, cleanupErr
		}
		return nil, admissionFailed("historical-successor-session-journal", "concrete successor journal authority is unavailable", nil)
	}
	cleanup.journal = journal

	session, err = bindGenerationEvidenceSession(ctx, journal, candidate, candidate.verifiedRun.currentDecision, nil)
	if err != nil {
		return nil, err
	}
	if session == nil {
		return nil, admissionFailed("historical-successor-session-bind", "successor evidence session is unavailable", nil)
	}
	cleanup.committed = true
	return session, nil
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

func (s *generationEvidenceSession) bindRunnerStatementIntentRecord(ctx context.Context, request runnerStatementIntentRecordRequest) (EvidenceJournal, JournalCursor, *OwnedEvidenceRecord, error) {
	if err := contextAdmissionError(ctx); err != nil {
		return nil, JournalCursor{}, nil, err
	}
	if s == nil || s.self != s {
		return nil, JournalCursor{}, nil, fail(CodeEvidenceRecoveryRequired, "runner-statement-intent-record", "current evidence session authority is unavailable", nil)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.validLocked() || request.candidateBinding == nil || request.candidateBinding != s.candidate.binding || s.active.kind != activeGenerationCurrent || s.active.recoveryExecutionBindings != nil || !sameGenerationIdentity(request.generation, s.active.identity) {
		return nil, JournalCursor{}, nil, fail(CodeEvidenceRecoveryRequired, "runner-statement-intent-record", "current evidence session differs from the prepared statement", nil)
	}
	bindings, err := s.candidate.verifiedRun.currentDecision.decision.runnerProjectionBindings()
	if err != nil || bindings.runnerProjectionDecisionDigest != request.generation.runnerProjectionDecisionDigest || bindings.schemaBundleDigest != request.generation.schemaBundleDigest {
		return nil, JournalCursor{}, nil, fail(CodeEvidenceRecoveryRequired, "runner-statement-intent-record", "current verifier bindings differ from the active generation", nil)
	}
	catalogContractDigest, err := runnerStatementIntentVerifiedSubject(bindings, request.plan, request.authorityBefore, request.catalogBefore)
	if err != nil {
		return nil, JournalCursor{}, nil, err
	}

	journal := s.journal
	journal.mu.Lock()
	defer journal.mu.Unlock()
	if !journal.validLocked() || journal.state == nil || journal.state.unknown != nil || request.maxAttempts == 0 || journal.schema.maxAttempts[request.plan.MigrationID] != request.maxAttempts || generationJournalRecoveryDigest(journal.state.recovery) != request.recoveryDigest {
		return nil, JournalCursor{}, nil, fail(CodeEvidenceRecoveryRequired, "runner-statement-intent-record", "current journal boundary differs from the prepared statement", nil)
	}
	header, ok := generationJournalHeader(journal)
	if !ok {
		return nil, JournalCursor{}, nil, fail(CodeEvidenceRecoveryRequired, "runner-statement-intent-record", "current generation header authority is unavailable", nil)
	}
	owned, err := bindBrandNewRunnerStatementIntentRecord(
		request.plan, request.authorityBefore, request.catalogBefore, catalogContractDigest,
		journal.generation, journal.state.cursor, journal.state.recovery, header, journal.schema.chainWitness,
	)
	if err != nil {
		return nil, JournalCursor{}, nil, err
	}
	return journal, journal.state.cursor.clone(), owned, nil
}

func (*generationEvidenceSession) runnerStatementIntentRecordBinderSealed() {}

func (s *generationEvidenceSession) bindRunnerIntermediateRecord(ctx context.Context, request runnerIntermediateRecordRequest) (EvidenceJournal, JournalCursor, *OwnedEvidenceRecord, error) {
	if err := contextAdmissionError(ctx); err != nil {
		return nil, JournalCursor{}, nil, err
	}
	if s == nil || s.self != s {
		return nil, JournalCursor{}, nil, fail(CodeEvidenceRecoveryRequired, "runner-intermediate-record", "current evidence session authority is unavailable", nil)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.validLocked() || request.candidateBinding == nil || request.candidateBinding != s.candidate.binding || s.active.kind != activeGenerationCurrent || s.active.recoveryExecutionBindings != nil || !sameGenerationIdentity(request.generation, s.active.identity) {
		return nil, JournalCursor{}, nil, fail(CodeEvidenceRecoveryRequired, "runner-intermediate-record", "current evidence session differs from the projected intermediate", nil)
	}
	bindings, err := s.candidate.verifiedRun.currentDecision.decision.runnerProjectionBindings()
	if err != nil || bindings.runnerProjectionDecisionDigest != request.generation.runnerProjectionDecisionDigest || bindings.schemaBundleDigest != request.generation.schemaBundleDigest {
		return nil, JournalCursor{}, nil, fail(CodeEvidenceRecoveryRequired, "runner-intermediate-record", "current verifier bindings differ from the active generation", nil)
	}
	if err := runnerFinalIntermediateVerifiedSubjects(bindings, request); err != nil {
		return nil, JournalCursor{}, nil, err
	}

	journal := s.journal
	journal.mu.Lock()
	defer journal.mu.Unlock()
	if !journal.validLocked() || journal.state == nil || journal.state.unknown != nil || request.maxAttempts == 0 || journal.schema.maxAttempts[request.plan.MigrationID] != request.maxAttempts || journal.schema.finalStatementIndex[request.plan.MigrationID] != request.plan.StatementIndex || journal.schema.finalCatalogDigest != request.preledgerCatalog.Digest || generationJournalRecoveryDigest(journal.state.recovery) != request.recoveryDigest {
		return nil, JournalCursor{}, nil, fail(CodeEvidenceRecoveryRequired, "runner-intermediate-record", "current journal boundary differs from the projected intermediate", nil)
	}
	header, ok := generationJournalHeader(journal)
	if !ok {
		return nil, JournalCursor{}, nil, fail(CodeEvidenceRecoveryRequired, "runner-intermediate-record", "current generation header authority is unavailable", nil)
	}
	owned, err := bindBrandNewRunnerFinalIntermediateRecord(
		request, journal.generation, journal.state.cursor, journal.state.recovery,
		header, journal.schema.chainWitness,
	)
	if err != nil {
		return nil, JournalCursor{}, nil, err
	}
	return journal, journal.state.cursor.clone(), owned, nil
}

func (*generationEvidenceSession) runnerIntermediateRecordBinderSealed() {}

func (s *generationEvidenceSession) bindRunnerCommitIntentRecord(ctx context.Context, request runnerCommitIntentRecordRequest) (EvidenceJournal, JournalCursor, *OwnedEvidenceRecord, error) {
	if err := contextAdmissionError(ctx); err != nil {
		return nil, JournalCursor{}, nil, err
	}
	if s == nil || s.self != s {
		return nil, JournalCursor{}, nil, fail(CodeEvidenceRecoveryRequired, "runner-commit-intent-record", "current evidence session authority is unavailable", nil)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.validLocked() || request.candidateBinding == nil || request.candidateBinding != s.candidate.binding || s.active.kind != activeGenerationCurrent || s.active.recoveryExecutionBindings != nil || !sameGenerationIdentity(request.generation, s.active.identity) {
		return nil, JournalCursor{}, nil, fail(CodeEvidenceRecoveryRequired, "runner-commit-intent-record", "current evidence session differs from the ledger readback", nil)
	}
	bindings, err := s.candidate.verifiedRun.currentDecision.decision.runnerProjectionBindings()
	if err != nil || bindings.runnerProjectionDecisionDigest != request.generation.runnerProjectionDecisionDigest || bindings.schemaBundleDigest != request.generation.schemaBundleDigest {
		return nil, JournalCursor{}, nil, fail(CodeEvidenceRecoveryRequired, "runner-commit-intent-record", "current verifier bindings differ from the active generation", nil)
	}
	if err := runnerCommitIntentVerifiedSubjects(bindings, request); err != nil {
		return nil, JournalCursor{}, nil, err
	}

	journal := s.journal
	journal.mu.Lock()
	defer journal.mu.Unlock()
	emptyLedgerDigest, emptyErr := LedgerPrefixDigest([]CommitIntentLedgerRow{})
	if !journal.validLocked() || journal.state == nil || journal.state.unknown != nil || request.maxAttempts == 0 || request.planCount != 1 || journal.schema.maxAttempts[request.plan.MigrationID] != request.maxAttempts || journal.schema.finalStatementIndex[request.plan.MigrationID] != request.plan.StatementIndex || journal.schema.finalCatalogDigest != request.intermediate.PreledgerCatalogResult.Digest || len(journal.schema.orderedMigrations) == 0 || journal.schema.orderedMigrations[0] != request.intent.MigrationID || len(journal.schema.signedExpectedLedgerRows) == 0 || !canonicalEqual(journal.schema.signedExpectedLedgerRows[0], request.ledgerRow) || len(journal.schema.durableObservedLedgerPrefix) != 0 || emptyErr != nil || journal.schema.durableObservedLedgerDigest != emptyLedgerDigest || generationJournalRecoveryDigest(journal.state.recovery) != request.recoveryDigest {
		return nil, JournalCursor{}, nil, fail(CodeEvidenceRecoveryRequired, "runner-commit-intent-record", "current journal or signed ledger boundary differs from the readback", nil)
	}
	header, ok := generationJournalHeader(journal)
	if !ok {
		return nil, JournalCursor{}, nil, fail(CodeEvidenceRecoveryRequired, "runner-commit-intent-record", "current generation header authority is unavailable", nil)
	}
	owned, err := bindBrandNewRunnerCommitIntentRecord(
		request, journal.generation, journal.state.cursor, journal.state.recovery,
		header, journal.schema.chainWitness,
	)
	if err != nil {
		return nil, JournalCursor{}, nil, err
	}
	return journal, journal.state.cursor.clone(), owned, nil
}

func (*generationEvidenceSession) runnerCommitIntentRecordBinderSealed() {}

func (s *generationEvidenceSession) bindRunnerCommittedTerminalRecord(ctx context.Context, closed *runnerClosedCurrentCommit) (EvidenceJournal, JournalCursor, *OwnedEvidenceRecord, error) {
	if err := contextAdmissionError(ctx); err != nil {
		return nil, JournalCursor{}, nil, err
	}
	if s == nil || s.self != s || !validRunnerClosedCurrentCommit(closed) || !sameRunnerOwnedPointer(closed.evidence, s) {
		return nil, JournalCursor{}, nil, fail(CodeEvidenceRecoveryRequired, "runner-committed-terminal-record", "current evidence or committed outcome authority is unavailable", nil)
	}
	seed, err := claimRunnerCommittedTerminalSeed(closed)
	if err != nil {
		return nil, JournalCursor{}, nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.validLocked() || seed.candidateBinding == nil || seed.candidateBinding != s.candidate.binding || seed.evidence != s || !sameRunnerOwnedPointer(seed.journal, s.journal) || s.active.kind != activeGenerationCurrent || s.active.recoveryExecutionBindings != nil || !sameGenerationIdentity(seed.generation, s.active.identity) {
		return nil, JournalCursor{}, nil, fail(CodeEvidenceRecoveryRequired, "runner-committed-terminal-record", "current evidence session differs from the committed outcome", nil)
	}
	bindings, err := s.candidate.verifiedRun.currentDecision.decision.runnerProjectionBindings()
	if err != nil || bindings.runnerProjectionDecisionDigest != seed.generation.runnerProjectionDecisionDigest || bindings.schemaBundleDigest != seed.generation.schemaBundleDigest || bindings.authorityProfileDigest != seed.commit.AuthorityProfileDigest || bindings.authorityBindingDigest != seed.commit.AuthorityBindingDigest {
		return nil, JournalCursor{}, nil, fail(CodeEvidenceRecoveryRequired, "runner-committed-terminal-record", "current verifier bindings differ from the committed outcome", nil)
	}

	journal := s.journal
	journal.mu.Lock()
	defer journal.mu.Unlock()
	entryIndex := int(seed.dispatch.entryIndex)
	if !journal.validLocked() || journal.state == nil || journal.state.unknown != nil || seed.maxAttempts == 0 || entryIndex < 0 || entryIndex >= len(journal.schema.orderedMigrations) || journal.schema.orderedMigrations[entryIndex] != seed.intent.MigrationID || journal.schema.maxAttempts[seed.intent.MigrationID] != seed.maxAttempts || journal.schema.finalStatementIndex[seed.intent.MigrationID] != seed.plan.StatementIndex || seed.intermediate.PreledgerCatalogResult == nil || journal.schema.finalCatalogDigest != seed.intermediate.PreledgerCatalogResult.Digest || generationJournalRecoveryDigest(journal.state.recovery) != seed.recoveryDigest || !sameCursorIdentity(journal.state.cursor, seed.cursor) {
		return nil, JournalCursor{}, nil, fail(CodeEvidenceRecoveryRequired, "runner-committed-terminal-record", "current journal boundary differs from the committed outcome", nil)
	}
	header, ok := generationJournalHeader(journal)
	if !ok {
		return nil, JournalCursor{}, nil, fail(CodeEvidenceRecoveryRequired, "runner-committed-terminal-record", "current generation header authority is unavailable", nil)
	}
	owned, err := bindBrandNewRunnerCommittedTerminalRecord(seed, journal.generation, journal.state.cursor, journal.state.recovery, header, journal.schema.chainWitness)
	if err != nil {
		return nil, JournalCursor{}, nil, err
	}
	return journal, journal.state.cursor.clone(), owned, nil
}

func (*generationEvidenceSession) runnerCommittedTerminalRecordBinderSealed() {}

func (*generationEvidenceSession) runnerLedgerEntrySuccessEvidenceBinderSealed() {}

func (s *generationEvidenceSession) bindRunnerLedgerEntrySuccessRecord(ctx context.Context, request *runnerLedgerEntrySuccessEvidenceRequest) (EvidenceJournal, JournalCursor, *OwnedEvidenceRecord, error) {
	claimed, err := consumeRunnerLedgerEntrySuccessEvidenceRequest(request, s)
	if err != nil {
		return nil, JournalCursor{}, nil, err
	}
	if err := contextAdmissionError(ctx); err != nil {
		return nil, JournalCursor{}, nil, err
	}
	if s == nil || s.self != s {
		return nil, JournalCursor{}, nil, fail(CodeEvidenceRecoveryRequired, "runner-ledger-entry-success-evidence", "evidence session is unavailable", nil)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.validLocked() || s.candidate.binding != claimed.candidateBinding || s.active.kind != activeGenerationCurrent ||
		s.active.recoveryExecutionBindings != nil || !sameGenerationIdentity(s.active.identity, claimed.generation) {
		return nil, JournalCursor{}, nil, fail(CodeEvidenceRecoveryRequired, "runner-ledger-entry-success-evidence", "current same-verifier evidence session changed", nil)
	}
	journal := s.journal
	journal.mu.Lock()
	defer journal.mu.Unlock()
	if !journal.validLocked() || journal.state == nil || journal.state.unknown != nil ||
		!sameCursorIdentity(journal.state.cursor, claimed.cursor) ||
		generationJournalRecoveryDigest(journal.state.recovery) != claimed.recoveryDigest ||
		journal.schema.maxAttempts[claimed.plan.MigrationID] != claimed.maxAttempts {
		return nil, JournalCursor{}, nil, fail(CodeEvidenceRecoveryRequired, "runner-ledger-entry-success-evidence", "current journal boundary changed", nil)
	}
	prefix, prefixErr := readRunnerLedgerEntrySuccessPrefixLocked(ctx, journal)
	if prefixErr != nil {
		return nil, JournalCursor{}, nil, prefixErr
	}
	if len(prefix) == 0 || claimed.cursor.previousRecordDigest == nil ||
		prefix[len(prefix)-1].RecordDigest != *claimed.cursor.previousRecordDigest ||
		prefix[len(prefix)-1].Sequence+1 != claimed.cursor.nextSequence {
		return nil, JournalCursor{}, nil, admissionCorrupt("runner-ledger-entry-success-evidence", "stored evidence prefix differs from the current cursor", nil)
	}
	owned, bindErr := bindRunnerLedgerEntrySuccessOwnedRecord(claimed, prefix, journal.schema.chainWitness)
	if bindErr != nil {
		return nil, JournalCursor{}, nil, admissionCorrupt("runner-ledger-entry-success-evidence", "candidate evidence record is invalid", bindErr)
	}
	return journal, journal.state.cursor.clone(), owned, nil
}

func readRunnerLedgerEntrySuccessPrefixLocked(ctx context.Context, journal *generationEvidenceJournal) ([]EvidenceFrame, error) {
	if journal == nil || journal.state == nil || journal.state.snapshot == nil {
		return nil, admissionFailed("runner-ledger-entry-success-evidence-read", "generation snapshot is unavailable", nil)
	}
	count, err := journal.state.snapshot.SegmentCount()
	if err != nil {
		return nil, mapEvidenceAdmissionError(err, "runner-ledger-entry-success-evidence-read")
	}
	if count == 0 || count > maxSupportedEvidenceReservedSegments {
		return nil, admissionCorrupt("runner-ledger-entry-success-evidence-read", "stored segment count is invalid", nil)
	}
	frames := make([]EvidenceFrame, 0, journal.state.journalRecords)
	for ordinal := uint32(0); ordinal < count; ordinal++ {
		raw, readErr := journal.state.snapshot.ReadSegment(ctx, ordinal)
		if readErr != nil {
			return nil, mapEvidenceAdmissionError(readErr, "runner-ledger-entry-success-evidence-read")
		}
		decodeErr := decodeAdmissionFramedBytes(raw, evidenceSegmentMaximumBytes, evidenceSegmentMaximumRecords, maxEvidenceFrameBytes, func(framed []byte) error {
			frame, frameErr := DecodeCanonicalEvidenceFrame(framed)
			if frameErr != nil {
				return frameErr
			}
			frames = append(frames, cloneProjectionValue(*frame))
			return nil
		})
		if decodeErr != nil {
			return nil, admissionCorrupt("runner-ledger-entry-success-evidence-read", "stored evidence segment is invalid", decodeErr)
		}
	}
	if err := journal.state.snapshot.Revalidate(ctx); err != nil {
		return nil, mapEvidenceAdmissionError(err, "runner-ledger-entry-success-evidence-revalidate")
	}
	if uint64(len(frames)) != journal.state.journalRecords {
		return nil, admissionCorrupt("runner-ledger-entry-success-evidence-read", "stored evidence record count changed", nil)
	}
	if err := validateEvidenceChainWithWitness(frames, journal.schema.chainWitness); err != nil {
		return nil, admissionCorrupt("runner-ledger-entry-success-evidence-read", "stored evidence chain is invalid", err)
	}
	return frames, nil
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

func (s *generationEvidenceSession) ReserveAndActivateSuccessor(ctx context.Context, authority *VerifiedLineageSupersessionAuthority) (active ActiveGeneration, snapshot *RecoverySnapshot, resultErr error) {
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
	detached, err := s.detachForSuccessorLocked(authority)
	if err != nil {
		return ActiveGeneration{}, nil, err
	}
	cleanup := sessionSuccessorCleanup{}
	defer func() {
		if cleanup.committed {
			return
		}
		authority.consumed.CompareAndSwap(false, true)
		if cleanupErr := cleanup.close(); cleanupErr != nil {
			resultErr = cleanupErr
		}
	}()

	reacquired, err := detached.lease.ReacquireAdmission(ctx)
	detached.revokeSource()
	if err != nil {
		return ActiveGeneration{}, nil, mapEvidenceAdmissionError(err, "evidence-session-successor-reacquire")
	}
	if !reacquired.Valid() || reacquired.PreviousTarget() != detached.target || reacquired.PreviousJournal() != detached.journal || reacquired.PreviousLeaseDigest() == ([32]byte{}) {
		if lease, _, admissionErr := reacquired.Admission(); admissionErr == nil {
			cleanup.admission = lease
		}
		return ActiveGeneration{}, nil, admissionFailed("evidence-session-successor-reacquire", "reacquired admission authority differs from the closed generation lease", nil)
	}
	lease, inventory, err := reacquired.Admission()
	if err != nil || lease == nil || inventory == nil {
		return ActiveGeneration{}, nil, mapEvidenceAdmissionError(err, "evidence-session-successor-reacquire")
	}
	cleanup.admission = lease
	mutation, err := inventory.MutationToken()
	if err != nil {
		return ActiveGeneration{}, nil, mapEvidenceAdmissionError(err, "evidence-session-successor-token")
	}
	history, err := bindVerifiedAdmissionHistory(ctx, inventory, detached.candidate)
	if err != nil {
		return ActiveGeneration{}, nil, err
	}
	if history == nil {
		return ActiveGeneration{}, nil, admissionFailed("evidence-session-successor-history", "verified admission history is unavailable", nil)
	}
	cleanup.history = history
	plan, err := bindVerifiedSuccessorAdmissionPlan(ctx, history, detached.candidate, authority)
	if err != nil {
		return ActiveGeneration{}, nil, err
	}
	if plan == nil {
		return ActiveGeneration{}, nil, admissionFailed("evidence-session-successor-plan", "verified successor plan is unavailable", nil)
	}
	cleanup.plan = plan
	permit, err := bindSuccessorAdmissionPermit(ctx, inventory, mutation, plan, detached.candidate)
	if err != nil {
		return ActiveGeneration{}, nil, err
	}
	if permit == nil {
		return ActiveGeneration{}, nil, admissionFailed("evidence-session-successor-permit", "successor admission permit is unavailable", nil)
	}
	cleanup.state = permit.state

	runtimePublishedResult, err := permit.PublishRuntime(ctx, detached.candidate)
	if err != nil {
		return ActiveGeneration{}, nil, err
	}
	runtimePublished := runtimePublishedResult.Next()
	if runtimePublishedResult.Outcome() != evidencefs.AdmissionTransitionDurable || runtimePublished == nil {
		return ActiveGeneration{}, nil, admissionFailed("evidence-session-successor-runtime-publish", "durable runtime publication authority is unavailable", nil)
	}
	cleanup.state = runtimePublished.state
	runtimeBoundResult, err := runtimePublished.BindRuntime(ctx, detached.candidate)
	if err != nil {
		return ActiveGeneration{}, nil, err
	}
	runtimeBound := runtimeBoundResult.Next()
	if runtimeBoundResult.Outcome() != evidencefs.AdmissionTransitionDurable || runtimeBound == nil {
		return ActiveGeneration{}, nil, admissionFailed("evidence-session-successor-runtime-bind", "durable runtime binding authority is unavailable", nil)
	}
	cleanup.state = runtimeBound.state
	recoveryPublishedResult, err := runtimeBound.PublishDecisionRecovery(ctx, detached.candidate)
	if err != nil {
		return ActiveGeneration{}, nil, err
	}
	recoveryPublished := recoveryPublishedResult.Next()
	if recoveryPublishedResult.Outcome() != evidencefs.AdmissionTransitionDurable || recoveryPublished == nil {
		return ActiveGeneration{}, nil, admissionFailed("evidence-session-successor-recovery-publish", "durable decision-recovery publication authority is unavailable", nil)
	}
	cleanup.state = recoveryPublished.state
	recoveryBoundResult, err := recoveryPublished.BindDecisionRecovery(ctx, detached.candidate)
	if err != nil {
		return ActiveGeneration{}, nil, err
	}
	recoveryBound := recoveryBoundResult.Next()
	if recoveryBoundResult.Outcome() != evidencefs.AdmissionTransitionDurable || recoveryBound == nil {
		return ActiveGeneration{}, nil, admissionFailed("evidence-session-successor-recovery-bind", "durable decision-recovery binding authority is unavailable", nil)
	}
	cleanup.state = recoveryBound.state
	reserveReadyResult, err := recoveryBound.SealReserveReady(ctx, detached.candidate)
	if err != nil {
		return ActiveGeneration{}, nil, err
	}
	reserveReady := reserveReadyResult.Next()
	if reserveReadyResult.Outcome() != evidencefs.AdmissionTransitionDurable || reserveReady == nil {
		return ActiveGeneration{}, nil, admissionFailed("evidence-session-successor-reserve-ready", "reserve-ready authority is unavailable", nil)
	}
	cleanup.state = reserveReady.state
	receiptBound, err := reserveReady.BindReceiptPair(detached.candidate)
	if err != nil {
		return ActiveGeneration{}, nil, err
	}
	if receiptBound == nil {
		return ActiveGeneration{}, nil, admissionFailed("evidence-session-successor-receipts", "successor receipt-pair authority is unavailable", nil)
	}
	cleanup.state = receiptBound.state
	supersededResult, err := receiptBound.AppendGenerationSuperseded(ctx, detached.candidate)
	if err != nil {
		return ActiveGeneration{}, nil, err
	}
	adjacentReady := supersededResult.Next()
	if supersededResult.Outcome() != evidencefs.AdmissionTransitionDurable || adjacentReady == nil {
		return ActiveGeneration{}, nil, admissionFailed("evidence-session-successor-supersede", "adjacent successor reservation authority is unavailable", nil)
	}
	cleanup.state = adjacentReady.state
	reservedResult, err := adjacentReady.AppendGenerationReserved(ctx, detached.candidate)
	if err != nil {
		return ActiveGeneration{}, nil, err
	}
	reservedReady := reservedResult.Next()
	if reservedResult.Outcome() != evidencefs.AdmissionTransitionDurable || reservedReady == nil {
		return ActiveGeneration{}, nil, admissionFailed("evidence-session-successor-reserve", "durable successor reservation authority is unavailable", nil)
	}
	cleanup.state = reservedReady.state
	headerResult, err := reservedReady.CreateGenerationHeader(ctx, detached.candidate)
	if err != nil {
		return ActiveGeneration{}, nil, err
	}
	headerReady := headerResult.Next()
	if headerResult.Outcome() != evidencefs.AdmissionTransitionDurable || headerReady == nil {
		return ActiveGeneration{}, nil, admissionFailed("evidence-session-successor-header", "durable successor header authority is unavailable", nil)
	}
	cleanup.state = headerReady.state
	activationResult, err := headerReady.AppendGenerationActivated(ctx, detached.candidate)
	if err != nil {
		return ActiveGeneration{}, nil, err
	}
	generationReady := activationResult.Next()
	if activationResult.Outcome() != evidencefs.AdmissionTransitionDurable || generationReady == nil {
		return ActiveGeneration{}, nil, admissionFailed("evidence-session-successor-activate", "durable successor activation authority is unavailable", nil)
	}
	cleanup.state = generationReady.state
	handoffResult, err := generationReady.Handoff(ctx, detached.candidate)
	if err != nil {
		return ActiveGeneration{}, nil, err
	}
	handoff := handoffResult.Next()
	if handoffResult.Outcome() != evidencefs.AdmissionTransitionDurable || handoff == nil {
		return ActiveGeneration{}, nil, admissionFailed("evidence-session-successor-handoff", "successor generation handoff authority is unavailable", nil)
	}
	cleanup.admission = nil
	cleanup.handoff = handoff
	replayResult, err := handoff.Replay(ctx, detached.candidate)
	if err != nil {
		return ActiveGeneration{}, nil, err
	}
	replay := replayResult.Next()
	if replay == nil {
		_, cleanupErr := handoff.failSuccessorReplay(evidencefs.ErrUnknown, "evidence-session-successor-replay-cleanup")
		if cleanupErr != nil {
			return ActiveGeneration{}, nil, cleanupErr
		}
		cleanup.handoff = nil
		return ActiveGeneration{}, nil, admissionFailed("evidence-session-successor-replay", "successor generation replay authority is unavailable", nil)
	}
	cleanup.replay = replay
	recovery, err := replay.BindRecovery(ctx, detached.candidate)
	if err != nil {
		return ActiveGeneration{}, nil, err
	}
	if recovery == nil {
		if cleanupErr := closeSuccessorGenerationReplay(replay, "evidence-session-successor-recovery-cleanup"); cleanupErr != nil {
			return ActiveGeneration{}, nil, cleanupErr
		}
		cleanup.replay = nil
		return ActiveGeneration{}, nil, admissionFailed("evidence-session-successor-recovery", "successor generation recovery authority is unavailable", nil)
	}
	cleanup.recovery = recovery
	journalAuthority, err := recovery.BindJournal(ctx, detached.candidate)
	if err != nil {
		return ActiveGeneration{}, nil, err
	}
	journal, ok := journalAuthority.(*generationEvidenceJournal)
	if !ok || journal == nil {
		if journalAuthority == nil {
			if cleanupErr := closeConsumedSuccessorGenerationRecovery(recovery, "evidence-session-successor-journal-cleanup"); cleanupErr != nil {
				return ActiveGeneration{}, nil, cleanupErr
			}
			cleanup.recovery = nil
		} else if cleanupErr := journalAuthority.Close(context.Background()); cleanupErr != nil {
			return ActiveGeneration{}, nil, cleanupErr
		}
		return ActiveGeneration{}, nil, admissionFailed("evidence-session-successor-journal", "concrete successor journal authority is unavailable", nil)
	}
	cleanup.journal = journal
	active, snapshot, err = s.installSuccessorLocked(ctx, journal, detached.candidate)
	if err != nil {
		return ActiveGeneration{}, nil, err
	}
	cleanup.committed = true
	return active, snapshot, nil
}

// detachedGenerationSession is the immutable pre-reacquire cleanup record. It
// deliberately retains the genuine GenerationLease only long enough to invoke
// its one-way ReacquireAdmission transition; it cannot revive the old session,
// journal, cursor, or migration authority registries.
type detachedGenerationSession struct {
	candidate OwnedCurrentCandidate
	lease     *evidencefs.GenerationLease
	target    [32]byte
	journal   [32]byte
	source    generationEvidenceJournalRegistryRecord
}

type runnerLedgerRecoveryRefreshSource struct {
	detached         detachedGenerationSession
	witness          *verifiedAdmissionLifecycleWitness
	chain            verifiedEvidenceChainWitness
	generation       generationIdentity
	activeKind       activeGenerationKind
	decisionDigest   Digest
	executionDigest  Digest
	stateCanonical   [32]byte
	schemaDigest     [32]byte
	recoveryDigest   [32]byte
	oldSessionDigest [32]byte
	oldJournalDigest [32]byte
}

type runnerLedgerRecoveryRefreshCleanup struct {
	admission *evidencefs.AdmissionLease
	history   *VerifiedAdmissionHistory
	permit    *RegisteredGenerationHandoffPermit
	ready     *RegisteredGenerationRecoveryReady
	journal   *generationEvidenceJournal
	committed bool
}

func (c *runnerLedgerRecoveryRefreshCleanup) close() error {
	if c == nil || c.committed {
		return nil
	}
	var cleanupErr error
	switch {
	case c.journal != nil:
		cleanupErr = c.journal.Close(context.Background())
	case c.ready != nil && c.ready.consumed != nil && !c.ready.consumed.Load():
		cleanupErr = c.ready.Close()
	case c.admission != nil:
		cleanupErr = c.admission.Close()
		if errors.Is(cleanupErr, evidencefs.ErrLeaseInvalid) && !c.admission.Active() {
			cleanupErr = nil
		} else if cleanupErr != nil {
			cleanupErr = mapEvidenceAdmissionError(cleanupErr, "runner-ledger-recovery-refresh-cleanup")
		}
	}
	if c.permit != nil {
		revokeRegisteredGenerationHandoffPermit(c.permit)
	} else if c.history != nil {
		revokeEvidenceSinkHistory(c.history)
	}
	return cleanupErr
}

// refreshRunnerLedgerRecoveryEvidence irreversibly replaces the retained
// generation lease with a fresh ALL-history replay and then reinstalls the
// exact same logical generation on this concrete session. It performs no
// database access, evidence append, reservation, or writer transition.
func (s *generationEvidenceSession) refreshRunnerLedgerRecoveryEvidence(ctx context.Context, candidate OwnedCurrentCandidate) (facts runnerLedgerRecoveryEvidenceFacts, resultErr error) {
	if err := contextAdmissionError(ctx); err != nil {
		return facts, err
	}
	if s == nil || s.self != s || !validOwnedCurrentCandidate(candidate) {
		return facts, fail(CodeEvidenceRecoveryRequired, "runner-ledger-recovery-refresh", "same-verifier evidence session is unavailable", nil)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.validLocked() || s.candidate.binding != candidate.binding {
		return facts, fail(CodeEvidenceRecoveryRequired, "runner-ledger-recovery-refresh", "same-verifier evidence session changed", nil)
	}
	source, err := s.detachForRunnerLedgerRecoveryLocked()
	if err != nil {
		return facts, err
	}
	defer revokeVerifiedAdmissionLifecycleWitness(source.witness)
	cleanup := runnerLedgerRecoveryRefreshCleanup{}
	defer func() {
		if cleanup.committed {
			return
		}
		if cleanupErr := cleanup.close(); cleanupErr != nil {
			resultErr = cleanupErr
		}
	}()

	reacquired, err := source.detached.lease.ReacquireAdmission(ctx)
	source.detached.revokeSource()
	if err != nil {
		return facts, mapEvidenceAdmissionError(err, "runner-ledger-recovery-refresh-reacquire")
	}
	if !reacquired.Valid() || reacquired.PreviousTarget() != source.detached.target || reacquired.PreviousJournal() != source.detached.journal || reacquired.PreviousLeaseDigest() == ([32]byte{}) {
		if lease, _, admissionErr := reacquired.Admission(); admissionErr == nil {
			cleanup.admission = lease
		}
		return facts, admissionFailed("runner-ledger-recovery-refresh-reacquire", "reacquired admission authority differs from the closed generation lease", nil)
	}
	lease, inventory, err := reacquired.Admission()
	if err != nil || lease == nil || inventory == nil {
		return facts, mapEvidenceAdmissionError(err, "runner-ledger-recovery-refresh-reacquire")
	}
	cleanup.admission = lease
	history, err := bindVerifiedAdmissionHistoryForRunnerRecovery(ctx, inventory, source.detached.candidate, source.witness)
	if err != nil {
		return facts, err
	}
	cleanup.history = history
	if history == nil || history.target != source.detached.target || history.targetGeneration == nil ||
		!sameGenerationIdentity(history.targetGeneration.descriptor.identity, source.generation) {
		return facts, fail(CodeEvidenceRecoveryRequired, "runner-ledger-recovery-refresh-history", "full-root replay did not retain the exact active generation", nil)
	}
	permit, err := bindRegisteredGenerationHandoff(ctx, history, source.detached.candidate)
	if err != nil {
		return facts, err
	}
	cleanup.permit = permit
	handoff, err := permit.Handoff(ctx, source.detached.candidate)
	if err != nil {
		return facts, err
	}
	ready := handoff.Next()
	if handoff.Outcome() != evidencefs.AdmissionTransitionDurable || ready == nil {
		return facts, admissionFailed("runner-ledger-recovery-refresh-handoff", "registered generation handoff authority is unavailable", nil)
	}
	cleanup.admission = nil
	cleanup.ready = ready
	authority, err := ready.BindJournal(ctx, source.detached.candidate)
	if err != nil {
		return facts, err
	}
	journal, ok := authority.(*generationEvidenceJournal)
	if !ok || journal == nil {
		if authority != nil {
			_ = authority.Close(context.Background())
		}
		return facts, admissionFailed("runner-ledger-recovery-refresh-journal", "concrete registered journal authority is unavailable", nil)
	}
	cleanup.journal = journal
	journal.schema.chainWitness = cloneAdmissionLifecycleChainWitness(source.chain)
	if verifiedAdmissionLifecycleChainDigest(journal.schema.chainWitness) != verifiedAdmissionLifecycleChainDigest(source.chain) {
		return facts, fail(CodeEvidenceRecoveryRequired, "runner-ledger-recovery-refresh-journal", "live lifecycle witness was not preserved", nil)
	}
	facts, err = s.installRunnerLedgerRecoveryLocked(ctx, journal, source, history)
	if err != nil {
		return runnerLedgerRecoveryEvidenceFacts{}, err
	}
	cleanup.committed = true
	return facts, nil
}

func (s *generationEvidenceSession) detachForRunnerLedgerRecoveryLocked() (runnerLedgerRecoveryRefreshSource, error) {
	var source runnerLedgerRecoveryRefreshSource
	if s == nil || s.self != s || !s.validLocked() {
		return source, fail(CodeEvidenceRecoveryRequired, "runner-ledger-recovery-refresh-detach", "same-verifier evidence session is unavailable", nil)
	}
	journal := s.journal
	journal.mu.Lock()
	defer journal.mu.Unlock()
	if !journal.validLocked() || journal.state == nil || journal.state.unknown != nil || journal.state.recovery == nil {
		return source, fail(CodeEvidenceRecoveryRequired, "runner-ledger-recovery-refresh-detach", "current journal has no stable recovery boundary", nil)
	}
	registered, ok := generationEvidenceJournalRegistry.Load(journal)
	record, recordOK := registered.(generationEvidenceJournalRegistryRecord)
	if !ok || !recordOK || record.journal != journal || record.binding != journal.binding || record.lease != journal.lease || record.state != journal.state || record.canonical != journal.binding.canonical || record.stateCanonical != journal.state.canonical || generationJournalRecordSourceCount(record) != 1 {
		return source, admissionFailed("runner-ledger-recovery-refresh-detach", "immutable journal source is unavailable", nil)
	}
	target, targetErr := journal.lease.Target()
	journalIdentity, journalErr := journal.lease.Journal()
	if targetErr != nil || journalErr != nil || target != digestRaw(journal.generation.executionLineageDigest) || journalIdentity != digestRaw(journal.generation.journalIdentityDigest) {
		return source, fail(CodeEvidenceRecoveryRequired, "runner-ledger-recovery-refresh-detach", "generation filesystem identity differs", nil)
	}
	candidate, err := cloneSessionCandidate(s.candidate)
	if err != nil {
		return source, err
	}
	witness, err := bindVerifiedAdmissionLifecycleWitness(s.candidate.binding, journal.generation, journal.state.canonical, journal.schema.chainWitness)
	if err != nil {
		return source, err
	}
	executionDigest := Digest("")
	if s.active.recoveryExecutionBindings != nil {
		executionDigest = s.active.recoveryExecutionBindings.digest
	}
	source = runnerLedgerRecoveryRefreshSource{
		detached: detachedGenerationSession{candidate: candidate, lease: journal.lease, target: target, journal: journalIdentity, source: record},
		witness:  witness, chain: cloneAdmissionLifecycleChainWitness(journal.schema.chainWitness),
		generation: journal.generation, activeKind: s.active.kind, decisionDigest: s.active.ownedDecision.digest,
		executionDigest: executionDigest, stateCanonical: journal.state.canonical,
		schemaDigest:     generationJournalSchemaDigest(journal.schema, journal.generation),
		recoveryDigest:   generationJournalRecoveryDigest(journal.state.recovery),
		oldSessionDigest: s.binding.canonical, oldJournalDigest: journal.binding.canonical,
	}
	if source.schemaDigest == ([32]byte{}) || source.recoveryDigest == ([32]byte{}) || source.decisionDigest.Validate() != nil {
		revokeVerifiedAdmissionLifecycleWitness(witness)
		return runnerLedgerRecoveryRefreshSource{}, fail(CodeEvidenceRecoveryRequired, "runner-ledger-recovery-refresh-detach", "current journal verifier facts are incomplete", nil)
	}

	// Failure is irreversible after this point. Revoke the public graph before
	// the old generation lease releases either lock.
	s.closed = true
	journal.closed = true
	generationEvidenceSessionRegistry.Delete(s)
	activeGenerationRegistry.Delete(s.active.binding)
	generationEvidenceJournalRegistry.Delete(journal)
	journal.state.cursor.valid.Store(false)
	return source, nil
}

func (s *generationEvidenceSession) installRunnerLedgerRecoveryLocked(ctx context.Context, journal *generationEvidenceJournal, source runnerLedgerRecoveryRefreshSource, history *VerifiedAdmissionHistory) (runnerLedgerRecoveryEvidenceFacts, error) {
	var facts runnerLedgerRecoveryEvidenceFacts
	if s == nil || s.self != s || !s.closed || journal == nil || journal.self != journal || journal.registeredPrior == nil || history == nil {
		return facts, admissionFailed("runner-ledger-recovery-refresh-install", "registered recovery session inputs are unavailable", nil)
	}
	if _, _, err := journal.Replay(ctx); err != nil {
		return facts, err
	}
	ownedCandidate, err := cloneSessionCandidate(source.detached.candidate)
	if err != nil {
		return facts, err
	}
	journal.mu.Lock()
	if !journal.validLocked() || !sameGenerationIdentity(journal.generation, source.generation) || journal.state == nil ||
		journal.state.canonical != source.stateCanonical || generationJournalSchemaDigest(journal.schema, journal.generation) != source.schemaDigest ||
		generationJournalRecoveryDigest(journal.state.recovery) != source.recoveryDigest {
		journal.mu.Unlock()
		return facts, fail(CodeEvidenceRecoveryRequired, "runner-ledger-recovery-refresh-install", "fresh full-root replay differs from the detached generation", nil)
	}
	recovery := cloneRecoverySnapshot(journal.state.recovery)
	stateCanonical := journal.state.canonical
	journal.mu.Unlock()

	registered := journal.registeredPrior.registered
	if registered == nil || !sameGenerationIdentity(registered.descriptor.identity, source.generation) {
		return facts, fail(CodeEvidenceRecoveryRequired, "runner-ledger-recovery-refresh-install", "registered generation authority differs from the detached generation", nil)
	}
	decision := ownedVerifiedDecisionCopy(registered.decision, registered.bindings)
	execution := cloneRecoveryExecutionBindings(journal.registeredPrior.executionBindings)
	switch source.activeKind {
	case activeGenerationCurrent:
		if execution != nil || registered.policy != nil || decision.digest != ownedCandidate.verifiedRun.currentDecision.digest || source.decisionDigest != decision.digest {
			return facts, fail(CodeEvidenceRecoveryRequired, "runner-ledger-recovery-refresh-install", "current generation verifier binding changed", nil)
		}
		decision = ownedCandidate.verifiedRun.currentDecision
	case activeGenerationAncestorRecovery:
		if execution == nil || registered.policy == nil || decision.digest != source.decisionDigest || execution.digest != source.executionDigest {
			return facts, fail(CodeEvidenceRecoveryRequired, "runner-ledger-recovery-refresh-install", "historical generation verifier binding changed", nil)
		}
	default:
		return facts, fail(CodeEvidenceRecoveryRequired, "runner-ledger-recovery-refresh-install", "active generation kind is unavailable", nil)
	}

	active := ActiveGeneration{
		identity: journal.generation, kind: source.activeKind, journal: journal, ownedDecision: decision,
		contentReceipt: journal.runtimeReceipt, decisionRecoveryReceipt: journal.recoveryReceipt,
		recoveryExecutionBindings: execution,
	}
	activeBinding := &activeGenerationBinding{
		session: s, journal: journal, candidateBinding: ownedCandidate.binding,
		runtimeBinding: journal.runtimeReceipt.binding, recoveryBinding: journal.recoveryReceipt.binding,
		executionBinding: execution,
	}
	active.binding = activeBinding
	activeBinding.canonical = activeGenerationDigest(active)
	s.candidate, s.journal, s.active, s.closed = ownedCandidate, journal, active, false
	s.binding = &generationEvidenceSessionBinding{session: s, journal: journal, candidateBinding: ownedCandidate.binding, activeBinding: activeBinding}
	s.binding.canonical = generationEvidenceSessionDigest(s)
	activeGenerationRegistry.Store(activeBinding, activeGenerationRegistryRecord{
		binding: activeBinding, session: s, journal: journal, candidateBinding: ownedCandidate.binding,
		runtimeBinding: journal.runtimeReceipt.binding, recoveryBinding: journal.recoveryReceipt.binding,
		executionBinding: execution, canonical: activeBinding.canonical,
	})
	generationEvidenceSessionRegistry.Store(s, generationEvidenceSessionRegistryRecord{
		session: s, binding: s.binding, journal: journal, candidateBinding: ownedCandidate.binding,
		activeBinding: activeBinding, canonical: s.binding.canonical,
	})
	if !s.validLocked() {
		activeGenerationRegistry.Delete(activeBinding)
		generationEvidenceSessionRegistry.Delete(s)
		s.closed = true
		return facts, admissionFailed("runner-ledger-recovery-refresh-install", "refreshed session authority could not be sealed", nil)
	}
	facts = runnerLedgerRecoveryEvidenceFacts{
		binder: s, candidateBinding: ownedCandidate.binding, generation: journal.generation,
		schema: cloneGenerationJournalSchema(journal.schema), recovery: recovery,
		schemaDigest: source.schemaDigest, recoveryDigest: source.recoveryDigest, stateCanonical: stateCanonical,
		sessionDigest: s.binding.canonical, journalDigest: journal.binding.canonical,
		fullSet: history.fullSet, transcriptCanonical: history.transcriptCanonical, revision: history.revision,
		target: history.target, indexRecords: history.targetIndexRecords, indexTail: history.targetIndexTail,
	}
	return facts, nil
}

func (s *generationEvidenceSession) detachForSuccessorLocked(authority *VerifiedLineageSupersessionAuthority) (detachedGenerationSession, error) {
	var detached detachedGenerationSession
	if s == nil || s.self != s || !s.validLocked() || authority == nil {
		return detached, admissionFailed("evidence-session-successor-detach", "session authority is unavailable", nil)
	}
	journal := s.journal
	journal.mu.Lock()
	defer journal.mu.Unlock()
	if !journal.validLocked() || journal.state == nil || journal.state.unknown != nil || !validSessionSuccessorAuthorityLocked(s, journal, authority) {
		return detached, fail(CodeEvidenceRecoveryRequired, "evidence-session-successor-detach", "current generation or supersession boundary is unavailable", nil)
	}
	registered, ok := generationEvidenceJournalRegistry.Load(journal)
	record, recordOK := registered.(generationEvidenceJournalRegistryRecord)
	if !ok || !recordOK || record.journal != journal || record.binding != journal.binding || record.lease != journal.lease || record.state != journal.state || record.canonical != journal.binding.canonical || record.stateCanonical != journal.state.canonical || generationJournalRecordSourceCount(record) != 1 {
		return detached, admissionFailed("evidence-session-successor-detach", "immutable journal source is unavailable", nil)
	}
	target, targetErr := journal.lease.Target()
	journalIdentity, journalErr := journal.lease.Journal()
	if targetErr != nil || journalErr != nil || target != digestRaw(journal.generation.executionLineageDigest) || journalIdentity != digestRaw(journal.generation.journalIdentityDigest) {
		return detached, fail(CodeEvidenceRecoveryRequired, "evidence-session-successor-detach", "generation filesystem identity differs", nil)
	}
	candidate, err := cloneSessionCandidate(s.candidate)
	if err != nil {
		return detached, err
	}
	detached = detachedGenerationSession{candidate: candidate, lease: journal.lease, target: target, journal: journalIdentity, source: record}

	// From this point onward failure is irreversible. Revoke the public session,
	// active generation, journal, and cursor before releasing the old locks, so
	// no concurrent caller can observe two generations as current.
	s.closed = true
	journal.closed = true
	generationEvidenceSessionRegistry.Delete(s)
	activeGenerationRegistry.Delete(s.active.binding)
	generationEvidenceJournalRegistry.Delete(journal)
	journal.state.cursor.valid.Store(false)
	return detached, nil
}

func validSessionSuccessorAuthorityLocked(s *generationEvidenceSession, journal *generationEvidenceJournal, authority *VerifiedLineageSupersessionAuthority) bool {
	if s == nil || journal == nil || authority == nil || s.active.kind != activeGenerationAncestorRecovery || s.active.recoveryExecutionBindings == nil || s.active.journal != journal || journal.state == nil || journal.state.recovery == nil || journal.state.cursor.valid == nil || journal.state.unknown != nil || authority.owner == nil || authority.owner != s.candidate.verifiedRun.currentDecision.owner || authority.session == nil || authority.session != s.candidate.owner || authority.consumed.Load() || authority.digest.Validate() != nil || authority.tailDigest.Validate() != nil || authority.tailDigest != journal.state.recovery.tailDigest || !sameGenerationIdentity(authority.generation, s.active.identity) || !sameGenerationIdentity(journal.generation, s.active.identity) {
		return false
	}
	execution := s.active.recoveryExecutionBindings
	if execution.owner != authority.owner || execution.session != authority.session || !sameGenerationIdentity(execution.generation, authority.generation) || execution.tailDigest != authority.tailDigest || execution.snapshot == nil || execution.snapshot.tailDigest != authority.tailDigest || !sameCursorIdentity(execution.snapshot.cursor, journal.state.recovery.cursor) || generationJournalRecoveryDigest(execution.snapshot) != generationJournalRecoveryDigest(journal.state.recovery) {
		return false
	}
	subjectDigest, digestErr := authority.subject.ComputeDigest()
	if digestErr != nil || subjectDigest != authority.digest || authority.subject.RecoveryExecutionBindingsDigest != execution.digest || authority.subject.HistoricalRecoveryPolicyDigest != execution.subject.HistoricalRecoveryPolicyDigest || validateRecoveryAuthorityBindings(s.candidate.verifiedRun.currentDecision.digest, execution.policy, execution.subject, authority.subject) != nil || !equalDigestPointer(authority.subject.OldTerminalDigest, journal.state.recovery.lastTerminalDigest) || !equalDigestPointer(authority.subject.OldResolutionDigest, journal.state.recovery.lastResolutionDigest) {
		return false
	}
	if authority.subject.ObservedOutcome == "activated_no_migration_progress" {
		return journal.state.cursor.latestCheckpointRecordDigest == nil && authority.subject.OldActivationRecordDigest != nil && *authority.subject.OldActivationRecordDigest == journal.state.cursor.lineageIndexPreviousRecordDigest && authority.subject.OldInitialJournalTailDigest != nil && *authority.subject.OldInitialJournalTailDigest == journal.state.recovery.tailDigest
	}
	return journal.state.cursor.latestCheckpointRecordDigest != nil && authority.subject.OldCheckpointRecordDigest != nil && *authority.subject.OldCheckpointRecordDigest == *journal.state.cursor.latestCheckpointRecordDigest && *authority.subject.OldCheckpointRecordDigest == journal.state.cursor.lineageIndexPreviousRecordDigest
}

// revokeSource removes the migration-side authority graph after evidencefs has
// already closed the old GenerationLease. Calling the ordinary Close helpers
// here would attempt to close the same descriptor authority twice.
func (d detachedGenerationSession) revokeSource() {
	record := d.source
	if record.state != nil && record.state.cursor.valid != nil {
		record.state.cursor.valid.Store(false)
	}
	switch {
	case record.registeredPrior != nil:
		ready := record.registeredPrior
		registeredGenerationRecoveryReadyRegistry.Delete(ready)
		if ready.prior != nil {
			registeredGenerationHandoffPermitRegistry.Delete(ready.prior)
		}
		if ready.history != nil && ready.history.binding != nil {
			verifiedAdmissionHistoryRegistry.Delete(ready.history.binding)
		}
		if ready.cursor.valid != nil {
			ready.cursor.valid.Store(false)
		}
		revokeVerifiedAdmissionRegisteredGeneration(ready.registered)
	case record.successorPrior != nil:
		ready := record.successorPrior
		successorGenerationRecoveryRegistry.Delete(ready)
		if ready.cursor.valid != nil {
			ready.cursor.valid.Store(false)
		}
		if ready.prior != nil {
			successorGenerationReplayRegistry.Delete(ready.prior)
			if ready.prior.prior != nil {
				successorGenerationHandoffRegistry.Delete(ready.prior.prior)
			}
			revokeSuccessorAdmissionStateChain(ready.prior.state)
		}
	case record.replay != nil:
		generationRecoveryReadyRegistry.Delete(record.prior)
		generationReplayReadyRegistry.Delete(record.replay)
		if record.replay.prior != nil {
			generationHandoffReadyRegistry.Delete(record.replay.prior)
		}
	}
	if record.runtimeBinding != nil {
		verifiedContentReceiptRegistry.Delete(record.runtimeBinding)
	}
	if record.recoveryBinding != nil {
		verifiedDecisionRecoveryReceiptRegistry.Delete(record.recoveryBinding)
	}
	if record.journal != nil {
		if record.journal.history != nil && record.journal.history.binding != nil {
			verifiedAdmissionHistoryRegistry.Delete(record.journal.history.binding)
		}
		if record.journal.plan != nil && record.journal.plan.binding != nil {
			verifiedAdmissionPlanRegistry.Delete(record.journal.plan.binding)
		}
		if record.journal.successorPrior != nil && record.journal.successorPrior.state != nil {
			state := record.journal.successorPrior.state
			if state.plan != nil && state.plan.binding != nil {
				verifiedSuccessorAdmissionPlanRegistry.Delete(state.plan.binding)
			}
		}
	}
}

type sessionSuccessorCleanup struct {
	admission *evidencefs.AdmissionLease
	history   *VerifiedAdmissionHistory
	plan      *VerifiedSuccessorAdmissionPlan
	state     *successorAdmissionState
	handoff   *SuccessorGenerationHandoffReady
	replay    *SuccessorGenerationReplayReady
	recovery  *SuccessorGenerationRecoveryReady
	journal   *generationEvidenceJournal
	committed bool
}

func (c *sessionSuccessorCleanup) close() error {
	if c == nil {
		return nil
	}
	var cleanupErr error
	switch {
	case c.journal != nil:
		c.journal.mu.Lock()
		closed := c.journal.closed
		c.journal.mu.Unlock()
		if !closed {
			cleanupErr = c.journal.Close(context.Background())
		}
	case c.recovery != nil && c.recovery.consumed != nil && !c.recovery.consumed.Load():
		cleanupErr = c.recovery.Close()
	case c.replay != nil && c.replay.consumed != nil && !c.replay.consumed.Load():
		cleanupErr = c.replay.Close()
	case c.handoff != nil && c.handoff.consumed != nil && !c.handoff.consumed.Load():
		cleanupErr = c.handoff.Close()
	case c.admission != nil:
		cleanupErr = c.admission.Close()
		if errors.Is(cleanupErr, evidencefs.ErrLeaseInvalid) && !c.admission.Active() {
			cleanupErr = nil
		} else if cleanupErr != nil {
			cleanupErr = mapEvidenceAdmissionError(cleanupErr, "evidence-session-successor-cleanup")
		}
	}
	c.revokeInMemory()
	return cleanupErr
}

func (c *sessionSuccessorCleanup) revokeInMemory() {
	revokeSuccessorAdmissionStateChain(c.state)
	if c.plan != nil && c.plan.binding != nil {
		verifiedSuccessorAdmissionPlanRegistry.Delete(c.plan.binding)
	}
	if c.history != nil {
		if c.history.binding != nil {
			verifiedAdmissionHistoryRegistry.Delete(c.history.binding)
		}
		revokeVerifiedAdmissionRegisteredGeneration(c.history.targetGeneration)
	}
}

func revokeSuccessorAdmissionStateChain(state *successorAdmissionState) {
	for current := state; current != nil; current = current.prior {
		if current.binding != nil {
			successorAdmissionStateRegistry.Delete(current.binding)
		}
		if current.runtimeReceipt.binding != nil {
			verifiedContentReceiptRegistry.Delete(current.runtimeReceipt.binding)
		}
		if current.recoveryReceipt.binding != nil {
			verifiedDecisionRecoveryReceiptRegistry.Delete(current.recoveryReceipt.binding)
		}
	}
}

func (s *generationEvidenceSession) installSuccessorLocked(ctx context.Context, journal *generationEvidenceJournal, candidate OwnedCurrentCandidate) (ActiveGeneration, *RecoverySnapshot, error) {
	if s == nil || s.self != s || !s.closed || journal == nil || journal.self != journal || journal.successorPrior == nil || journal.successorReplay == nil || journal.registeredPrior != nil || journal.prior != nil {
		return ActiveGeneration{}, nil, admissionFailed("evidence-session-successor-install", "successor session inputs are unavailable", nil)
	}
	_, snapshot, err := journal.Replay(ctx)
	if err != nil {
		return ActiveGeneration{}, nil, err
	}
	if snapshot == nil {
		return ActiveGeneration{}, nil, admissionFailed("evidence-session-successor-install", "successor recovery snapshot is unavailable", nil)
	}
	ownedCandidate, err := cloneSessionCandidate(candidate)
	if err != nil {
		return ActiveGeneration{}, nil, err
	}
	active := ActiveGeneration{
		identity: journal.generation, kind: activeGenerationCurrent, journal: journal,
		ownedDecision:  ownedCandidate.verifiedRun.currentDecision,
		contentReceipt: journal.runtimeReceipt, decisionRecoveryReceipt: journal.recoveryReceipt,
	}
	activeBinding := &activeGenerationBinding{
		session: s, journal: journal, candidateBinding: ownedCandidate.binding,
		runtimeBinding: journal.runtimeReceipt.binding, recoveryBinding: journal.recoveryReceipt.binding,
	}
	active.binding = activeBinding
	activeBinding.canonical = activeGenerationDigest(active)
	s.candidate, s.journal, s.active, s.closed = ownedCandidate, journal, active, false
	s.binding = &generationEvidenceSessionBinding{
		session: s, journal: journal, candidateBinding: ownedCandidate.binding, activeBinding: activeBinding,
	}
	s.binding.canonical = generationEvidenceSessionDigest(s)
	activeGenerationRegistry.Store(activeBinding, activeGenerationRegistryRecord{
		binding: activeBinding, session: s, journal: journal, candidateBinding: ownedCandidate.binding,
		runtimeBinding: journal.runtimeReceipt.binding, recoveryBinding: journal.recoveryReceipt.binding,
		canonical: activeBinding.canonical,
	})
	generationEvidenceSessionRegistry.Store(s, generationEvidenceSessionRegistryRecord{
		session: s, binding: s.binding, journal: journal, candidateBinding: ownedCandidate.binding,
		activeBinding: activeBinding, canonical: s.binding.canonical,
	})
	if !s.validLocked() {
		activeGenerationRegistry.Delete(activeBinding)
		generationEvidenceSessionRegistry.Delete(s)
		s.closed = true
		return ActiveGeneration{}, nil, admissionFailed("evidence-session-successor-install", "successor session authority could not be sealed", nil)
	}
	return s.active, cloneRecoverySnapshot(snapshot), nil
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
	revokeRunnerLedgerPreflightClaims(s)
	revokeRunnerLedgerEntryAdmissionClaims(s)
	revokeRunnerLedgerEntryExecutionAdmissionClaims(s)
	revokeRunnerLedgerRecoveryAdmissionClaims(s)
	return record.journal.Close(ctx)
}

func (s *generationEvidenceSession) evidenceSessionSealed() {}

func (s *generationEvidenceSession) runnerLedgerPreflightClaimBinderSealed() {}

func (s *generationEvidenceSession) runnerLedgerEntryAdmissionClaimBinderSealed() {}

func (s *generationEvidenceSession) runnerLedgerEntryExecutionAdmissionClaimBinderSealed() {}

func (s *generationEvidenceSession) runnerLedgerRecoveryAdmissionClaimBinderSealed() {}

func (s *generationEvidenceSession) bindRunnerLedgerRecoveryAdmissionClaim(ctx context.Context, request runnerLedgerRecoveryAdmissionClaimRequest) (*runnerLedgerRecoveryAdmissionClaim, error) {
	if s == nil || s.self != s || !validOwnedCurrentCandidate(request.candidate) {
		return nil, fail(CodeEvidenceRecoveryRequired, "runner-ledger-recovery-admission-evidence", "same-verifier evidence session is unavailable", nil)
	}
	facts, err := s.refreshRunnerLedgerRecoveryEvidence(ctx, request.candidate)
	if err != nil {
		return nil, err
	}
	return bindRunnerLedgerRecoveryAdmissionClaimFromEvidence(ctx, request, facts)
}

func (s *generationEvidenceSession) consumeRunnerLedgerRecoveryAdmissionClaim(ctx context.Context, claim *runnerLedgerRecoveryAdmissionClaim, candidate OwnedCurrentCandidate) (runnerLedgerRecoveryAdmissionEvidenceBoundary, error) {
	if s == nil || s.self != s || !validOwnedCurrentCandidate(candidate) {
		revokeRunnerLedgerRecoveryAdmissionClaim(claim)
		return runnerLedgerRecoveryAdmissionEvidenceBoundary{}, fail(CodeEvidenceRecoveryRequired, "runner-ledger-recovery-admission-evidence", "same-verifier evidence session is unavailable", nil)
	}
	facts, err := s.refreshRunnerLedgerRecoveryEvidence(ctx, candidate)
	if err != nil {
		revokeRunnerLedgerRecoveryAdmissionClaim(claim)
		return runnerLedgerRecoveryAdmissionEvidenceBoundary{}, err
	}
	return consumeRunnerLedgerRecoveryAdmissionClaimFromEvidence(ctx, claim, candidate, facts)
}

func (s *generationEvidenceSession) bindRunnerLedgerEntryExecutionAdmissionClaim(ctx context.Context, request runnerLedgerEntryExecutionAdmissionClaimRequest) (*runnerLedgerEntryExecutionAdmissionClaim, error) {
	if err := contextAdmissionError(ctx); err != nil {
		return nil, err
	}
	if s == nil || s.self != s || !validOwnedCurrentCandidate(request.candidate) {
		return nil, fail(CodeEvidenceRecoveryRequired, "runner-ledger-entry-execution-admission-evidence", "evidence session is unavailable", nil)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.validLocked() || s.candidate.binding != request.candidate.binding || s.active.kind != activeGenerationCurrent {
		return nil, fail(CodeEvidenceRecoveryRequired, "runner-ledger-entry-execution-admission-evidence", "current same-verifier evidence session is unavailable", nil)
	}
	j := s.journal
	j.mu.Lock()
	defer j.mu.Unlock()
	if !j.validLocked() || j.state == nil || j.state.unknown != nil || j.state.recovery == nil {
		return nil, fail(CodeEvidenceJournalFailed, "runner-ledger-entry-execution-admission-evidence", "current evidence journal has no stable recovery boundary", nil)
	}
	facts := runnerLedgerEntryExecutionAdmissionEvidenceFacts{
		binder: s, candidateBinding: s.candidate.binding, generation: j.generation,
		schema: cloneGenerationJournalSchema(j.schema), recovery: cloneRecoverySnapshot(j.state.recovery),
		schemaDigest: generationJournalSchemaDigest(j.schema, j.generation), recoveryDigest: generationJournalRecoveryDigest(j.state.recovery),
		sessionDigest: s.binding.canonical, journalDigest: j.binding.canonical,
	}
	return bindRunnerLedgerEntryExecutionAdmissionClaimFromEvidence(ctx, request, facts)
}

func (s *generationEvidenceSession) consumeRunnerLedgerEntryExecutionAdmissionClaim(ctx context.Context, claim *runnerLedgerEntryExecutionAdmissionClaim, candidate OwnedCurrentCandidate) (runnerLedgerEntryExecutionAdmissionEvidenceBoundary, error) {
	if err := contextAdmissionError(ctx); err != nil {
		return runnerLedgerEntryExecutionAdmissionEvidenceBoundary{}, err
	}
	if s == nil || s.self != s || candidate.binding == nil {
		return runnerLedgerEntryExecutionAdmissionEvidenceBoundary{}, fail(CodeEvidenceRecoveryRequired, "runner-ledger-entry-execution-admission-evidence", "evidence session is unavailable", nil)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.validLocked() || s.candidate.binding != candidate.binding || s.active.kind != activeGenerationCurrent {
		revokeRunnerLedgerEntryExecutionAdmissionClaim(claim)
		return runnerLedgerEntryExecutionAdmissionEvidenceBoundary{}, fail(CodeEvidenceRecoveryRequired, "runner-ledger-entry-execution-admission-evidence", "current same-verifier evidence session is unavailable", nil)
	}
	j := s.journal
	j.mu.Lock()
	defer j.mu.Unlock()
	if !j.validLocked() || j.state == nil || j.state.unknown != nil || j.state.recovery == nil {
		revokeRunnerLedgerEntryExecutionAdmissionClaim(claim)
		return runnerLedgerEntryExecutionAdmissionEvidenceBoundary{}, fail(CodeEvidenceJournalFailed, "runner-ledger-entry-execution-admission-evidence", "current evidence journal has no stable recovery boundary", nil)
	}
	facts := runnerLedgerEntryExecutionAdmissionEvidenceFacts{
		binder: s, candidateBinding: s.candidate.binding, generation: j.generation,
		schema: cloneGenerationJournalSchema(j.schema), recovery: cloneRecoverySnapshot(j.state.recovery),
		schemaDigest: generationJournalSchemaDigest(j.schema, j.generation), recoveryDigest: generationJournalRecoveryDigest(j.state.recovery),
		sessionDigest: s.binding.canonical, journalDigest: j.binding.canonical,
	}
	return consumeRunnerLedgerEntryExecutionAdmissionClaimFromEvidence(ctx, claim, candidate, facts)
}

func (s *generationEvidenceSession) bindRunnerLedgerEntryAdmissionClaim(ctx context.Context, request runnerLedgerEntryAdmissionClaimRequest) (*runnerLedgerEntryAdmissionClaim, error) {
	if err := contextAdmissionError(ctx); err != nil {
		return nil, err
	}
	if s == nil || s.self != s || !validOwnedCurrentCandidate(request.candidate) {
		return nil, fail(CodeEvidenceRecoveryRequired, "runner-ledger-entry-admission-evidence", "evidence session is unavailable", nil)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.validLocked() || s.candidate.binding != request.candidate.binding || s.active.kind != activeGenerationCurrent {
		return nil, fail(CodeEvidenceRecoveryRequired, "runner-ledger-entry-admission-evidence", "current same-verifier evidence session is unavailable", nil)
	}
	j := s.journal
	j.mu.Lock()
	defer j.mu.Unlock()
	if !j.validLocked() || j.state == nil || j.state.unknown != nil || j.state.recovery == nil {
		return nil, fail(CodeEvidenceJournalFailed, "runner-ledger-entry-admission-evidence", "current evidence journal has no stable recovery boundary", nil)
	}
	facts := runnerLedgerEntryAdmissionEvidenceFacts{
		binder: s, candidateBinding: s.candidate.binding, generation: j.generation,
		schema: cloneGenerationJournalSchema(j.schema), recovery: cloneRecoverySnapshot(j.state.recovery),
		schemaDigest: generationJournalSchemaDigest(j.schema, j.generation), recoveryDigest: generationJournalRecoveryDigest(j.state.recovery),
		sessionDigest: s.binding.canonical, journalDigest: j.binding.canonical,
	}
	return bindRunnerLedgerEntryAdmissionClaimFromEvidence(ctx, request, facts)
}

func (s *generationEvidenceSession) consumeRunnerLedgerEntryAdmissionClaim(ctx context.Context, claim *runnerLedgerEntryAdmissionClaim, candidate OwnedCurrentCandidate) (runnerLedgerEntryAdmissionEvidenceBoundary, error) {
	if err := contextAdmissionError(ctx); err != nil {
		return runnerLedgerEntryAdmissionEvidenceBoundary{}, err
	}
	if s == nil || s.self != s || candidate.binding == nil {
		return runnerLedgerEntryAdmissionEvidenceBoundary{}, fail(CodeEvidenceRecoveryRequired, "runner-ledger-entry-admission-evidence", "evidence session is unavailable", nil)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.validLocked() || s.candidate.binding != candidate.binding || s.active.kind != activeGenerationCurrent {
		revokeRunnerLedgerEntryAdmissionClaim(claim)
		return runnerLedgerEntryAdmissionEvidenceBoundary{}, fail(CodeEvidenceRecoveryRequired, "runner-ledger-entry-admission-evidence", "current same-verifier evidence session is unavailable", nil)
	}
	j := s.journal
	j.mu.Lock()
	defer j.mu.Unlock()
	if !j.validLocked() || j.state == nil || j.state.unknown != nil || j.state.recovery == nil {
		revokeRunnerLedgerEntryAdmissionClaim(claim)
		return runnerLedgerEntryAdmissionEvidenceBoundary{}, fail(CodeEvidenceJournalFailed, "runner-ledger-entry-admission-evidence", "current evidence journal has no stable recovery boundary", nil)
	}
	facts := runnerLedgerEntryAdmissionEvidenceFacts{
		binder: s, candidateBinding: s.candidate.binding, generation: j.generation,
		schema: cloneGenerationJournalSchema(j.schema), recovery: cloneRecoverySnapshot(j.state.recovery),
		schemaDigest: generationJournalSchemaDigest(j.schema, j.generation), recoveryDigest: generationJournalRecoveryDigest(j.state.recovery),
		sessionDigest: s.binding.canonical, journalDigest: j.binding.canonical,
	}
	return consumeRunnerLedgerEntryAdmissionClaimFromEvidence(ctx, claim, candidate, facts)
}

func (s *generationEvidenceSession) bindRunnerLedgerPreflightClaim(ctx context.Context, request runnerLedgerPreflightClaimRequest) (*runnerLedgerPreflightClaim, error) {
	if err := contextAdmissionError(ctx); err != nil {
		return nil, err
	}
	if s == nil || s.self != s || !validOwnedCurrentCandidate(request.candidate) {
		return nil, fail(CodeEvidenceRecoveryRequired, "runner-ledger-preflight-evidence", "evidence session is unavailable", nil)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.validLocked() || s.candidate.binding != request.candidate.binding {
		return nil, fail(CodeEvidenceRecoveryRequired, "runner-ledger-preflight-evidence", "same-verifier evidence session is unavailable", nil)
	}
	j := s.journal
	j.mu.Lock()
	defer j.mu.Unlock()
	if !j.validLocked() || j.state == nil || j.state.unknown != nil || j.state.recovery == nil {
		return nil, fail(CodeEvidenceJournalFailed, "runner-ledger-preflight-evidence", "current evidence journal has no stable recovery boundary", nil)
	}
	activeKind, executionDigest, ok := runnerLedgerPreflightActiveIdentity(s.active, j.state.recovery, s.candidate)
	if !ok {
		return nil, fail(CodeEvidenceRecoveryRequired, "runner-ledger-preflight-evidence", "active generation cannot enter the selected preflight", nil)
	}
	facts := runnerLedgerPreflightEvidenceFacts{
		binder: s, candidateBinding: s.candidate.binding, generation: j.generation,
		activeKind: activeKind, executionDigest: executionDigest,
		schema: cloneGenerationJournalSchema(j.schema), recovery: cloneRecoverySnapshot(j.state.recovery),
		schemaDigest:   generationJournalSchemaDigest(j.schema, j.generation),
		recoveryDigest: generationJournalRecoveryDigest(j.state.recovery),
		sessionDigest:  s.binding.canonical, journalDigest: j.binding.canonical,
	}
	return bindRunnerLedgerPreflightClaimFromEvidence(ctx, request, facts)
}

func (s *generationEvidenceSession) consumeRunnerLedgerPreflightClaim(ctx context.Context, claim *runnerLedgerPreflightClaim, candidate OwnedCurrentCandidate) (runnerLedgerPreflightDispatch, error) {
	if err := contextAdmissionError(ctx); err != nil {
		return runnerLedgerPreflightDispatch{}, err
	}
	if s == nil || s.self != s || candidate.binding == nil {
		return runnerLedgerPreflightDispatch{}, fail(CodeEvidenceRecoveryRequired, "runner-ledger-preflight-evidence", "evidence session is unavailable", nil)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.validLocked() || s.candidate.binding != candidate.binding {
		return runnerLedgerPreflightDispatch{}, fail(CodeEvidenceRecoveryRequired, "runner-ledger-preflight-evidence", "same-verifier evidence session is unavailable", nil)
	}
	j := s.journal
	j.mu.Lock()
	defer j.mu.Unlock()
	if !j.validLocked() || j.state == nil || j.state.unknown != nil || j.state.recovery == nil {
		revokeRunnerLedgerPreflightClaim(claim)
		return runnerLedgerPreflightDispatch{}, fail(CodeEvidenceJournalFailed, "runner-ledger-preflight-evidence", "current evidence journal has no stable recovery boundary", nil)
	}
	activeKind, executionDigest, ok := runnerLedgerPreflightActiveIdentity(s.active, j.state.recovery, s.candidate)
	if !ok {
		revokeRunnerLedgerPreflightClaim(claim)
		return runnerLedgerPreflightDispatch{}, fail(CodeEvidenceRecoveryRequired, "runner-ledger-preflight-evidence", "active generation cannot enter the selected preflight", nil)
	}
	facts := runnerLedgerPreflightEvidenceFacts{
		binder: s, candidateBinding: s.candidate.binding, generation: j.generation,
		activeKind: activeKind, executionDigest: executionDigest,
		schema: cloneGenerationJournalSchema(j.schema), recovery: cloneRecoverySnapshot(j.state.recovery),
		schemaDigest:   generationJournalSchemaDigest(j.schema, j.generation),
		recoveryDigest: generationJournalRecoveryDigest(j.state.recovery),
		sessionDigest:  s.binding.canonical, journalDigest: j.binding.canonical,
	}
	return consumeRunnerLedgerPreflightClaimFromEvidence(ctx, claim, candidate, facts)
}

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
