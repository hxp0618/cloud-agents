package migration

import (
	"bytes"
	"context"
	"crypto/sha256"
	"sync"
	"sync/atomic"
)

// VerifiedSuccessorAdmissionPlan is the migration-owned, post-reacquire A -> B
// plan. It binds the fresh ALL-history authority, the consumed same-verifier
// supersession authority, and the exact adjacent Superseded/Reserved bytes. It
// is not filesystem mutation, receipt, or reservation authority.
type VerifiedSuccessorAdmissionPlan struct {
	self                 *VerifiedSuccessorAdmissionPlan
	history              *VerifiedAdmissionHistory
	registered           *verifiedAdmissionRegisteredGeneration
	candidateBinding     *verifiedEvidenceRunBinding
	authority            *VerifiedLineageSupersessionAuthority
	authoritySubject     lineageSupersessionAuthoritySubject
	authorityDigest      Digest
	executionDigest      Digest
	supersededFrame      LineageIndexFrame
	reservedFrame        LineageIndexFrame
	headerFrame          EvidenceFrame
	supersededFrameBytes []byte
	reservedFrameBytes   []byte
	binding              *verifiedSuccessorAdmissionPlanBinding
	consumed             *atomic.Bool
}

type verifiedSuccessorAdmissionPlanBinding struct {
	plan             *VerifiedSuccessorAdmissionPlan
	history          *VerifiedAdmissionHistory
	registered       *verifiedAdmissionRegisteredGeneration
	candidateBinding *verifiedEvidenceRunBinding
	authority        *VerifiedLineageSupersessionAuthority
	canonical        [32]byte
}

var verifiedSuccessorAdmissionPlanRegistry sync.Map

func bindVerifiedSuccessorAdmissionPlan(ctx context.Context, history *VerifiedAdmissionHistory, candidate OwnedCurrentCandidate, authority *VerifiedLineageSupersessionAuthority) (*VerifiedSuccessorAdmissionPlan, error) {
	if !validVerifiedAdmissionHistory(history, candidate) || history.targetGeneration == nil || history.targetGeneration.replay == nil || authority == nil {
		return nil, fail(CodeEvidenceRecoveryRequired, "successor-plan", "verified successor inputs are unavailable", nil)
	}
	if err := contextAdmissionError(ctx); err != nil {
		return nil, err
	}
	if err := history.inventory.Revalidate(ctx); err != nil {
		return nil, mapEvidenceAdmissionError(err, "successor-plan-revalidate")
	}
	registered := history.targetGeneration
	execution, subject, err := verifiedSuccessorAuthorityInputs(history, candidate, authority, false)
	if err != nil {
		return nil, err
	}
	superseded, reserved, header, supersededBytes, reservedBytes, err := buildSuccessorAdmissionFrames(history, candidate, subject, authority.digest, execution)
	if err != nil {
		return nil, err
	}
	if !validVerifiedAdmissionHistory(history, candidate) {
		return nil, admissionFailed("successor-plan", "admission history changed during successor planning", nil)
	}
	consumedSubject, err := authority.consume(candidate.owner, registered.descriptor.identity, registered.replay.recovery.tailDigest)
	if err != nil || !canonicalEqual(consumedSubject, subject) {
		return nil, fail(CodeEvidenceRecoveryRequired, "successor-plan", "supersession authority could not be consumed exactly", err)
	}
	plan := &VerifiedSuccessorAdmissionPlan{
		history: history, registered: registered, candidateBinding: candidate.binding, authority: authority,
		authoritySubject: cloneProjectionValue(subject), authorityDigest: authority.digest, executionDigest: execution.digest,
		supersededFrame: cloneProjectionValue(superseded), reservedFrame: cloneProjectionValue(reserved), headerFrame: cloneProjectionValue(header),
		supersededFrameBytes: append([]byte(nil), supersededBytes...), reservedFrameBytes: append([]byte(nil), reservedBytes...), consumed: &atomic.Bool{},
	}
	plan.self = plan
	plan.binding = &verifiedSuccessorAdmissionPlanBinding{
		plan: plan, history: history, registered: registered, candidateBinding: candidate.binding, authority: authority,
	}
	plan.binding.canonical = verifiedSuccessorAdmissionPlanDigest(plan)
	verifiedSuccessorAdmissionPlanRegistry.Store(plan.binding, plan.binding.canonical)
	if !validVerifiedSuccessorAdmissionPlan(plan, history, candidate) {
		verifiedSuccessorAdmissionPlanRegistry.Delete(plan.binding)
		return nil, admissionFailed("successor-plan", "successor plan authority could not be sealed", nil)
	}
	return plan, nil
}

func verifiedSuccessorAuthorityInputs(history *VerifiedAdmissionHistory, candidate OwnedCurrentCandidate, authority *VerifiedLineageSupersessionAuthority, consumed bool) (VerifiedRecoveryExecutionBindings, lineageSupersessionAuthoritySubject, error) {
	if history == nil || history.targetGeneration == nil || history.targetGeneration.replay == nil || !validOwnedCurrentCandidate(candidate) || authority == nil || authority.owner == nil || authority.session != candidate.owner || authority.consumed.Load() != consumed {
		return VerifiedRecoveryExecutionBindings{}, lineageSupersessionAuthoritySubject{}, fail(CodeEvidenceRecoveryRequired, "successor-plan", "same-verifier supersession authority is unavailable", nil)
	}
	registered := history.targetGeneration
	if registered.policy == nil || registered.decision.digest == candidate.verifiedRun.currentDecision.digest || !sameGenerationIdentity(authority.generation, registered.descriptor.identity) || authority.tailDigest != registered.replay.recovery.tailDigest || authority.digest.Validate() != nil {
		return VerifiedRecoveryExecutionBindings{}, lineageSupersessionAuthoritySubject{}, fail(CodeEvidenceRecoveryRequired, "successor-plan", "historical generation or authority boundary is mismatched", nil)
	}
	execution, err := bindRecoveryExecution(*registered.policy, candidate.verifiedRun.currentDecision, registered.decision, registered.bindings, registered.descriptor, registered.replay.recovery)
	if err != nil {
		return VerifiedRecoveryExecutionBindings{}, lineageSupersessionAuthoritySubject{}, fail(CodeEvidenceRecoveryRequired, "successor-plan", "historical recovery execution cannot be rebound", err)
	}
	subject := cloneProjectionValue(authority.subject)
	digest, digestErr := subject.ComputeDigest()
	if digestErr != nil || digest != authority.digest || subject.RecoveryExecutionBindingsDigest != execution.digest || validateRecoveryAuthorityBindings(candidate.verifiedRun.currentDecision.digest, registered.policy.subject, execution.subject, subject) != nil {
		return VerifiedRecoveryExecutionBindings{}, lineageSupersessionAuthoritySubject{}, fail(CodeEvidenceRecoveryRequired, "successor-plan", "supersession authority does not match recovered execution", digestErr)
	}
	return execution, subject, nil
}

func buildSuccessorAdmissionFrames(history *VerifiedAdmissionHistory, candidate OwnedCurrentCandidate, subject lineageSupersessionAuthoritySubject, authorityDigest Digest, execution VerifiedRecoveryExecutionBindings) (LineageIndexFrame, LineageIndexFrame, EvidenceFrame, []byte, []byte, error) {
	if history == nil || history.targetGeneration == nil || history.targetGeneration.replay == nil || !validOwnedCurrentCandidate(candidate) || authorityDigest.Validate() != nil || history.target != digestRaw(candidate.verifiedRun.executionLineageDigest) || history.targetIndexRecords == 0 || history.targetIndexRecords >= maxJSONInteger || history.targetIndexTail.Validate() != nil || execution.digest.Validate() != nil || execution.digest != subject.RecoveryExecutionBindingsDigest {
		return LineageIndexFrame{}, LineageIndexFrame{}, EvidenceFrame{}, nil, nil, fail(CodeEvidenceRecoveryRequired, "successor-plan", "successor frame inputs are incomplete", nil)
	}
	registered := history.targetGeneration
	replay := registered.replay
	subjectDigest, subjectErr := subject.ComputeDigest()
	policyDigest, policyErr := execution.policy.ComputeDigest()
	executionDigest, executionErr := execution.subject.ComputeDigest()
	if subjectErr != nil || policyErr != nil || executionErr != nil || subjectDigest != authorityDigest || policyDigest != execution.subject.HistoricalRecoveryPolicyDigest || executionDigest != execution.digest || execution.owner == nil || execution.session != candidate.owner || !sameGenerationIdentity(execution.generation, registered.descriptor.identity) || validateRecoveryAuthorityBindings(candidate.verifiedRun.currentDecision.digest, execution.policy, execution.subject, subject) != nil || registered.descriptor.identity.runnerProjectionDecisionDigest == candidate.verifiedRun.runnerProjectionDecisionDigest || subject.ExecutionLineageDigest != registered.descriptor.identity.executionLineageDigest || subject.OldJournalIdentityDigest != registered.descriptor.identity.journalIdentityDigest || subject.OldRunnerProjectionDecisionDigest != registered.descriptor.identity.runnerProjectionDecisionDigest || subject.OldSchemaBundleDigest != registered.descriptor.identity.schemaBundleDigest || subject.SuccessorRunnerProjectionDecisionDigest != candidate.verifiedRun.runnerProjectionDecisionDigest || subject.SuccessorSchemaBundleDigest != candidate.verifiedRun.schemaBundleDigest || replay.recovery == nil || replay.recovery.tailDigest != execution.tailDigest || !equalDigestPointer(subject.OldTerminalDigest, replay.recovery.lastTerminalDigest) || !equalDigestPointer(subject.OldResolutionDigest, replay.recovery.lastResolutionDigest) {
		return LineageIndexFrame{}, LineageIndexFrame{}, EvidenceFrame{}, nil, nil, fail(CodeEvidenceRecoveryRequired, "successor-plan", "successor identities or recovery boundary differ", nil)
	}
	if subject.ObservedOutcome == "activated_no_migration_progress" {
		if history.targetState != admissionLineageActiveInitial || replay.cursor.latestCheckpointRecordDigest != nil || subject.OldActivationRecordDigest == nil || *subject.OldActivationRecordDigest != history.targetIndexTail || subject.OldInitialJournalTailDigest == nil || *subject.OldInitialJournalTailDigest != replay.recovery.tailDigest {
			return LineageIndexFrame{}, LineageIndexFrame{}, EvidenceFrame{}, nil, nil, fail(CodeEvidenceRecoveryRequired, "successor-plan", "header-only supersession boundary differs", nil)
		}
	} else if history.targetState != admissionLineageActiveCheckpointed || replay.cursor.latestCheckpointRecordDigest == nil || subject.OldCheckpointRecordDigest == nil || *subject.OldCheckpointRecordDigest != *replay.cursor.latestCheckpointRecordDigest || *subject.OldCheckpointRecordDigest != history.targetIndexTail {
		return LineageIndexFrame{}, LineageIndexFrame{}, EvidenceFrame{}, nil, nil, fail(CodeEvidenceRecoveryRequired, "successor-plan", "checkpoint supersession boundary differs", nil)
	}
	reserved, header, err := buildPlannedGenerationReservation(history, candidate, subject.Continuation)
	if err != nil {
		return LineageIndexFrame{}, LineageIndexFrame{}, EvidenceFrame{}, nil, nil, err
	}
	if reserved.JournalIdentityDigest == registered.descriptor.identity.journalIdentityDigest {
		return LineageIndexFrame{}, LineageIndexFrame{}, EvidenceFrame{}, nil, nil, fail(CodeEvidenceRecoveryRequired, "successor-plan", "successor journal identity did not advance", nil)
	}
	supersededBody := GenerationSuperseded{
		ExecutionLineageDigest: subject.ExecutionLineageDigest, OldJournalIdentityDigest: subject.OldJournalIdentityDigest,
		OldRunnerProjectionDecisionDigest: subject.OldRunnerProjectionDecisionDigest, OldSchemaBundleDigest: subject.OldSchemaBundleDigest,
		OldCheckpointRecordDigest: cloneDigestPointer(subject.OldCheckpointRecordDigest), OldActivationRecordDigest: cloneDigestPointer(subject.OldActivationRecordDigest),
		OldInitialJournalTailDigest: cloneDigestPointer(subject.OldInitialJournalTailDigest), LineageSupersessionAuthorityDigest: authorityDigest,
		Outcome: subject.ObservedOutcome, PlannedGenerationReserved: &reserved,
	}
	if supersededBody.Validate() != nil || !canonicalEqual(subject.Continuation, reserved.Continuation) {
		return LineageIndexFrame{}, LineageIndexFrame{}, EvidenceFrame{}, nil, nil, fail(CodeEvidenceRecoveryRequired, "successor-plan", "supersession outcome cannot reserve a successor", nil)
	}
	previous := history.targetIndexTail
	superseded := LineageIndexFrame{FormatVersion: LineageFrameFormat, Sequence: history.targetIndexRecords, PreviousRecordDigest: &previous, RecordKind: LineageRecordGenerationSuperseded, Record: LineageIndexRecord{Superseded: &supersededBody}}
	superseded.RecordDigest, err = superseded.ComputeDigest()
	if err != nil || superseded.Validate() != nil {
		return LineageIndexFrame{}, LineageIndexFrame{}, EvidenceFrame{}, nil, nil, fail(CodeEvidenceRecoveryRequired, "successor-plan", "supersession frame cannot be planned", err)
	}
	supersededBytes, err := EncodeCanonicalLineageFrame(superseded)
	if err != nil {
		return LineageIndexFrame{}, LineageIndexFrame{}, EvidenceFrame{}, nil, nil, err
	}
	reservedPrevious := superseded.RecordDigest
	reservedFrame := LineageIndexFrame{FormatVersion: LineageFrameFormat, Sequence: history.targetIndexRecords + 1, PreviousRecordDigest: &reservedPrevious, RecordKind: LineageRecordGenerationReserved, Record: LineageIndexRecord{Reserved: &reserved}}
	reservedFrame.RecordDigest, err = reservedFrame.ComputeDigest()
	if err != nil || reservedFrame.Validate() != nil {
		return LineageIndexFrame{}, LineageIndexFrame{}, EvidenceFrame{}, nil, nil, fail(CodeEvidenceRecoveryRequired, "successor-plan", "adjacent reservation frame cannot be planned", err)
	}
	reservedBytes, err := EncodeCanonicalLineageFrame(reservedFrame)
	if err != nil {
		return LineageIndexFrame{}, LineageIndexFrame{}, EvidenceFrame{}, nil, nil, err
	}
	if replay.indexDebitRecords >= replay.reservation.ReservedIndexRecords || replay.indexDebitBytes >= replay.reservation.ReservedIndexBytes {
		return LineageIndexFrame{}, LineageIndexFrame{}, EvidenceFrame{}, nil, nil, fail(CodeEvidenceJournalLimitExceeded, "successor-plan", "old generation has no supersession reservation", nil)
	}
	remainingRecords := replay.reservation.ReservedIndexRecords - replay.indexDebitRecords
	remainingBytes := replay.reservation.ReservedIndexBytes - replay.indexDebitBytes
	if remainingRecords < 1 || uint64(len(supersededBytes)) > remainingBytes || history.reservation.ReservedIndexRecords < 1 || uint64(len(reservedBytes)) > history.reservation.ReservedIndexBytes {
		return LineageIndexFrame{}, LineageIndexFrame{}, EvidenceFrame{}, nil, nil, fail(CodeEvidenceJournalLimitExceeded, "successor-plan", "adjacent successor frames exceed their generation reservations", nil)
	}
	return superseded, reservedFrame, header, append([]byte(nil), supersededBytes...), append([]byte(nil), reservedBytes...), nil
}

func validVerifiedSuccessorAdmissionPlan(plan *VerifiedSuccessorAdmissionPlan, history *VerifiedAdmissionHistory, candidate OwnedCurrentCandidate) bool {
	if plan == nil || plan.self != plan || plan.binding == nil || plan.binding.plan != plan || plan.history != history || plan.registered == nil || history == nil || history.targetGeneration != plan.registered || plan.candidateBinding != candidate.binding || plan.authority == nil || plan.binding.history != history || plan.binding.registered != plan.registered || plan.binding.candidateBinding != candidate.binding || plan.binding.authority != plan.authority || plan.consumed == nil || plan.consumed.Load() || !plan.authority.consumed.Load() || !validVerifiedAdmissionHistory(history, candidate) || plan.binding.canonical == ([32]byte{}) || plan.binding.canonical != verifiedSuccessorAdmissionPlanDigest(plan) || !successorAdmissionPlanFramesExact(plan) {
		return false
	}
	execution, subject, err := verifiedSuccessorAuthorityInputs(history, candidate, plan.authority, true)
	if err != nil || execution.digest != plan.executionDigest || plan.authority.digest != plan.authorityDigest || !canonicalEqual(subject, plan.authoritySubject) {
		return false
	}
	registered, ok := verifiedSuccessorAdmissionPlanRegistry.Load(plan.binding)
	return ok && registered == plan.binding.canonical
}

func validConsumedVerifiedSuccessorAdmissionPlan(plan *VerifiedSuccessorAdmissionPlan, candidate OwnedCurrentCandidate) bool {
	if plan == nil || plan.self != plan || plan.binding == nil || plan.binding.plan != plan || plan.history == nil || plan.registered == nil || plan.history.targetGeneration != plan.registered || plan.candidateBinding != candidate.binding || plan.authority == nil || plan.binding.history != plan.history || plan.binding.registered != plan.registered || plan.binding.candidateBinding != candidate.binding || plan.binding.authority != plan.authority || plan.consumed == nil || !plan.consumed.Load() || !plan.authority.consumed.Load() || !validOwnedCurrentCandidate(candidate) || plan.history.owner != candidate.verifiedRun.currentDecision.owner || plan.history.candidateBinding != candidate.binding || plan.history.binding == nil || plan.history.binding.owner != plan.history.owner || plan.history.binding.candidateBinding != candidate.binding || plan.history.binding.inventory != plan.history.inventory || plan.history.binding.history != plan.history || plan.history.binding.canonical == ([32]byte{}) || plan.history.binding.canonical != admissionHistoryDigest(plan.history) || !plan.history.rootFacts.valid() || !validVerifiedAdmissionRegisteredGeneration(plan.registered, candidate.verifiedRun.currentDecision) || plan.binding.canonical == ([32]byte{}) || plan.binding.canonical != verifiedSuccessorAdmissionPlanDigest(plan) || !successorAdmissionPlanFramesExact(plan) {
		return false
	}
	execution, subject, err := verifiedSuccessorAuthorityInputs(plan.history, candidate, plan.authority, true)
	if err != nil || execution.digest != plan.executionDigest || plan.authority.digest != plan.authorityDigest || !canonicalEqual(subject, plan.authoritySubject) {
		return false
	}
	registeredHistory, historyOK := verifiedAdmissionHistoryRegistry.Load(plan.history.binding)
	registeredPlan, planOK := verifiedSuccessorAdmissionPlanRegistry.Load(plan.binding)
	return historyOK && registeredHistory == plan.history.binding.canonical && planOK && registeredPlan == plan.binding.canonical
}

func successorAdmissionPlanFramesExact(plan *VerifiedSuccessorAdmissionPlan) bool {
	if plan == nil || plan.supersededFrame.Validate() != nil || plan.reservedFrame.Validate() != nil || plan.headerFrame.Validate() != nil || plan.supersededFrame.Record.Superseded == nil || plan.reservedFrame.Record.Reserved == nil || plan.headerFrame.Record.Header == nil || plan.reservedFrame.Sequence != plan.supersededFrame.Sequence+1 || plan.reservedFrame.PreviousRecordDigest == nil || *plan.reservedFrame.PreviousRecordDigest != plan.supersededFrame.RecordDigest || !canonicalEqual(plan.supersededFrame.Record.Superseded.PlannedGenerationReserved, plan.reservedFrame.Record.Reserved) || plan.reservedFrame.Record.Reserved.ExpectedSegment0HeaderDigest != plan.headerFrame.RecordDigest {
		return false
	}
	supersededBytes, supersededErr := EncodeCanonicalLineageFrame(plan.supersededFrame)
	reservedBytes, reservedErr := EncodeCanonicalLineageFrame(plan.reservedFrame)
	return supersededErr == nil && reservedErr == nil && bytes.Equal(supersededBytes, plan.supersededFrameBytes) && bytes.Equal(reservedBytes, plan.reservedFrameBytes)
}

func verifiedSuccessorAdmissionPlanDigest(plan *VerifiedSuccessorAdmissionPlan) [32]byte {
	if plan == nil || plan.self != plan || plan.history == nil || plan.history.binding == nil || plan.registered == nil || plan.candidateBinding == nil || plan.authority == nil || plan.authorityDigest.Validate() != nil || plan.executionDigest.Validate() != nil {
		return [32]byte{}
	}
	h := sha256.New()
	h.Write([]byte("cloud-agents-platform-verified-successor-admission-plan/v1\x00"))
	h.Write(plan.history.binding.canonical[:])
	h.Write(plan.registered.canonical[:])
	h.Write(plan.candidateBinding.canonical[:])
	writeAdmissionString(h, plan.authorityDigest.String())
	writeAdmissionString(h, plan.executionDigest.String())
	authorityCanonical, err := canonicalContractKey(plan.authoritySubject)
	if err != nil {
		return [32]byte{}
	}
	writeAdmissionString(h, authorityCanonical)
	h.Write(plan.supersededFrameBytes)
	h.Write(plan.reservedFrameBytes)
	headerBytes, err := EncodeCanonicalEvidenceFrame(plan.headerFrame)
	if err != nil {
		return [32]byte{}
	}
	h.Write(headerBytes)
	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result
}
