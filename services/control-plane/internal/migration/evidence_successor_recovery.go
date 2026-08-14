package migration

import (
	"context"
	"crypto/sha256"
	"sync"
	"sync/atomic"
)

// SuccessorGenerationRecoveryReady is the same-verifier recovery authority
// for the newly activated successor generation. It retains the inherited
// continuation semantics but is still not an EvidenceJournal or session.
type SuccessorGenerationRecoveryReady struct {
	self             *SuccessorGenerationRecoveryReady
	prior            *SuccessorGenerationReplayReady
	state            *successorAdmissionState
	candidateBinding *verifiedEvidenceRunBinding
	generation       generationIdentity
	cursor           JournalCursor
	recovery         *RecoverySnapshot
	factsDigest      [32]byte
	binding          *successorGenerationRecoveryBinding
	consumed         *atomic.Bool
}

type successorGenerationRecoveryBinding struct {
	ready            *SuccessorGenerationRecoveryReady
	prior            *SuccessorGenerationReplayReady
	state            *successorAdmissionState
	candidateBinding *verifiedEvidenceRunBinding
	canonical        [32]byte
}

type successorGenerationRecoveryRecord struct {
	ready            *SuccessorGenerationRecoveryReady
	binding          *successorGenerationRecoveryBinding
	prior            *SuccessorGenerationReplayReady
	state            *successorAdmissionState
	candidateBinding *verifiedEvidenceRunBinding
	cursorValid      *atomic.Bool
	canonical        [32]byte
}

var successorGenerationRecoveryRegistry sync.Map

// BindRecovery consumes strict successor replay and reconstructs the current
// candidate's same-verifier witness. It does not mint journal/session/runner
// authority.
func (r *SuccessorGenerationReplayReady) BindRecovery(ctx context.Context, candidate OwnedCurrentCandidate) (*SuccessorGenerationRecoveryReady, error) {
	if r == nil || !validSuccessorGenerationReplayReady(r, candidate) {
		return nil, fail(CodeEvidenceRecoveryRequired, "successor-generation-recovery", "successor replay authority is unavailable", nil)
	}
	if err := contextAdmissionError(ctx); err != nil {
		return nil, err
	}
	if !r.consumed.CompareAndSwap(false, true) {
		return nil, fail(CodeEvidenceRecoveryRequired, "successor-generation-recovery", "successor replay authority is consumed", nil)
	}
	headerRaw, err := r.snapshot.ReadSegment(ctx, 0)
	if err != nil {
		return r.failSuccessorRecovery(err, "successor-generation-recovery-header")
	}
	frames, err := decodeGenerationRecoveryFrames(headerRaw)
	if err != nil {
		return r.failSuccessorRecovery(err, "successor-generation-recovery-header")
	}
	if len(frames) != 1 || frames[0].RecordKind != EvidenceRecordHeader || frames[0].Record.Header == nil || frames[0].RecordDigest != r.state.headerDigest || !canonicalEqual(frames[0], r.state.plan.headerFrame) {
		return r.failSuccessorRecovery(admissionCorrupt("successor-generation-recovery-header", "successor replay is not exact header-only state", nil), "successor-generation-recovery-header")
	}
	_, _, receiptsOK := successorGenerationReplayReceipts(r, candidate, *frames[0].Record.Header)
	if !receiptsOK {
		return r.failSuccessorRecovery(fail(CodeEvidenceRecoveryRequired, "successor-generation-recovery-receipts", "successor typed publication receipts are unavailable", nil), "successor-generation-recovery-receipts")
	}
	bindings, err := candidate.verifiedRun.currentDecision.decision.runnerProjectionBindings()
	if err != nil {
		return r.failSuccessorRecovery(fail(CodeEvidenceRecoveryRequired, "successor-generation-recovery-bindings", "current verifier bindings are unavailable", nil), "successor-generation-recovery-bindings")
	}
	facts := cloneAdmissionHistoricalVerificationFacts(r.state.history.currentFacts)
	if !validAdmissionRecoveryFacts(facts) || facts.runnerProjectionDecisionDigest != bindings.runnerProjectionDecisionDigest || facts.schemaBundleDigest != bindings.schemaBundleDigest || facts.manifestDigest != candidate.verifiedRun.manifestDigest || facts.authorityProfileDigest != bindings.authorityProfileDigest || facts.authorityBindingDigest != bindings.authorityBindingDigest {
		return r.failSuccessorRecovery(fail(CodeEvidenceRecoveryRequired, "successor-generation-recovery-facts", "successor history verifier facts are unavailable", nil), "successor-generation-recovery-facts")
	}
	reserved := r.state.plan.reservedFrame.Record.Reserved
	if reserved == nil {
		return r.failSuccessorRecovery(admissionCorrupt("successor-generation-recovery-plan", "successor reservation is unavailable", nil), "successor-generation-recovery-plan")
	}
	generation := generationIdentity{
		owner: candidate.owner, executionLineageDigest: reserved.ExecutionLineageDigest,
		journalIdentityDigest: reserved.JournalIdentityDigest, runnerProjectionDecisionDigest: bindings.runnerProjectionDecisionDigest,
		schemaBundleDigest: bindings.schemaBundleDigest,
	}
	if !sameGenerationHeader(generation, *frames[0].Record.Header) || generation.runnerProjectionDecisionDigest != candidate.verifiedRun.runnerProjectionDecisionDigest || generation.schemaBundleDigest != candidate.verifiedRun.schemaBundleDigest {
		return r.failSuccessorRecovery(admissionCorrupt("successor-generation-recovery-header", "successor header differs from same-verifier generation", nil), "successor-generation-recovery-header")
	}
	_, schema, err := buildBrandNewRecoveryWitness(candidate, generation, facts)
	if err != nil {
		return r.failSuccessorRecovery(err, "successor-generation-recovery-witness")
	}
	if err := validateEvidenceChainWithWitness(frames, schema.chainWitness); err != nil {
		return r.failSuccessorRecovery(admissionCorrupt("successor-generation-recovery-witness", "successor header witness differs", err), "successor-generation-recovery-witness")
	}
	previous := r.journalTail
	cursor := JournalCursor{
		owner: candidate.owner, generation: generation, segmentIndex: 0, nextSequence: r.journalRecords,
		previousRecordDigest: &previous, lineageIndexNextSequence: r.indexRecords,
		lineageIndexPreviousRecordDigest: r.state.activationDigest, valid: &atomic.Bool{},
	}
	cursor.valid.Store(true)
	continuation, err := successorRecoveredContinuation(r, generation, cursor)
	if err != nil {
		cursor.valid.Store(false)
		return r.failSuccessorRecovery(err, "successor-generation-recovery-continuation")
	}
	recovery, err := buildRecoverySnapshot(frames, cursor, generation, continuation, schema)
	if err != nil || recovery == nil || recovery.State() != RecoveryBrandNewInherited {
		cursor.valid.Store(false)
		if err == nil {
			err = fail(CodeEvidenceRecoveryRequired, "successor-generation-recovery-snapshot", "successor inherited recovery snapshot is unavailable", nil)
		}
		return r.failSuccessorRecovery(err, "successor-generation-recovery-snapshot")
	}
	if err := r.validateSuccessorRecoveryAction(recovery, reserved.Continuation); err != nil {
		cursor.valid.Store(false)
		return r.failSuccessorRecovery(err, "successor-generation-recovery-snapshot")
	}
	if err := r.snapshot.Revalidate(ctx); err != nil {
		cursor.valid.Store(false)
		return r.failSuccessorRecovery(err, "successor-generation-recovery-terminal")
	}
	ready := &SuccessorGenerationRecoveryReady{
		prior: r, state: r.state, candidateBinding: candidate.binding, generation: generation,
		cursor: cursor, recovery: recovery, factsDigest: admissionRecoveryFactsDigest(facts), consumed: &atomic.Bool{},
	}
	ready.self = ready
	ready.binding = &successorGenerationRecoveryBinding{ready: ready, prior: r, state: r.state, candidateBinding: candidate.binding}
	ready.binding.canonical = successorGenerationRecoveryDigest(ready)
	successorGenerationRecoveryRegistry.Store(ready, successorGenerationRecoveryRecord{
		ready: ready, binding: ready.binding, prior: r, state: r.state, candidateBinding: candidate.binding,
		cursorValid: cursor.valid, canonical: ready.binding.canonical,
	})
	if !validSuccessorGenerationRecoveryReady(ready, candidate) {
		successorGenerationRecoveryRegistry.Delete(ready)
		cursor.valid.Store(false)
		return r.failSuccessorRecovery(fail(CodeEvidenceRecoveryRequired, "successor-generation-recovery-seal", "successor recovery authority could not be sealed", nil), "successor-generation-recovery-seal")
	}
	return ready, nil
}

func successorGenerationReplayReceipts(replay *SuccessorGenerationReplayReady, candidate OwnedCurrentCandidate, header JournalHeader) (VerifiedContentReceipt, VerifiedDecisionRecoveryReceipt, bool) {
	if replay == nil || replay.prior == nil || replay.state == nil || replay.state.plan == nil || replay.state.plan.headerFrame.Record.Header == nil || !validOwnedCurrentCandidate(candidate) || header.Validate() != nil || !canonicalEqual(header, *replay.state.plan.headerFrame.Record.Header) {
		return VerifiedContentReceipt{}, VerifiedDecisionRecoveryReceipt{}, false
	}
	registered, ok := successorGenerationHandoffRegistry.Load(replay.prior)
	record, recordOK := registered.(successorGenerationHandoffRecord)
	if !ok || !recordOK || record.ready != replay.prior || record.binding != replay.prior.binding || record.state != replay.state || record.stateBinding != replay.state.binding || record.candidateBinding != candidate.binding || record.lease != replay.lease || record.canonical == ([32]byte{}) || record.canonical != replay.prior.binding.canonical {
		return VerifiedContentReceipt{}, VerifiedDecisionRecoveryReceipt{}, false
	}
	if !validRuntimeReceipt(record.runtimeReceipt, candidate.owner, header.OuterArtifactDigest, header.OuterArtifactSizeBytes) || !validDecisionRecoveryReceipt(record.recoveryReceipt, candidate.owner, header.DecisionRecoveryArtifactSHA256, header.DecisionRecoveryArtifactSizeBytes) || record.runtimeReceipt.publication == nil || record.recoveryReceipt.publication == nil || !record.runtimeReceipt.publication.SameStore(record.recoveryReceipt.publication) {
		return VerifiedContentReceipt{}, VerifiedDecisionRecoveryReceipt{}, false
	}
	return record.runtimeReceipt, record.recoveryReceipt, true
}

func successorRecoveredContinuation(replay *SuccessorGenerationReplayReady, generation generationIdentity, cursor JournalCursor) (recoveredContinuation, error) {
	if replay == nil || replay.state == nil || replay.state.plan == nil || replay.state.plan.reservedFrame.Record.Reserved == nil {
		return recoveredContinuation{}, fail(CodeEvidenceRecoveryRequired, "successor-generation-recovery-continuation", "successor continuation source is unavailable", nil)
	}
	continuation := replay.state.plan.reservedFrame.Record.Reserved.Continuation
	if continuation == nil {
		return recoveredContinuation{inheritedWithoutContext: true}, nil
	}
	value := cloneProjectionValue(*continuation)
	if err := value.Validate(); err != nil {
		return recoveredContinuation{}, admissionCorrupt("successor-generation-recovery-continuation", "successor continuation is invalid", err)
	}
	return recoveredContinuation{owned: recoveredValue(generation, cursor, replay.journalTail, replay.state.reservedDigest, value)}, nil
}

func (r *SuccessorGenerationReplayReady) validateSuccessorRecoveryAction(recovery *RecoverySnapshot, continuation *LineageContinuationContext) error {
	if r == nil || r.state == nil || recovery == nil || recovery.State() != RecoveryBrandNewInherited {
		return fail(CodeEvidenceRecoveryRequired, "successor-generation-recovery-snapshot", "successor recovery state is not inherited brand-new", nil)
	}
	if continuation == nil {
		if recovery.NextAction() != RecoveryBeginFirstAttempt || recovery.LineageContinuation() != nil {
			return fail(CodeEvidenceRecoveryRequired, "successor-generation-recovery-snapshot", "header-only successor recovery action differs", nil)
		}
		return nil
	}
	recovered := recovery.LineageContinuation()
	if recovered == nil || recovered.RecordDigest() != r.state.reservedDigest || !canonicalEqual(recovered.Value(), *continuation) {
		return fail(CodeEvidenceRecoveryRequired, "successor-generation-recovery-snapshot", "successor continuation body differs from durable reservation", nil)
	}
	switch continuation.StartAction {
	case "begin_next_attempt":
		if recovery.NextAction() != RecoveryBeginNextAttempt {
			return fail(CodeEvidenceRecoveryRequired, "successor-generation-recovery-snapshot", "successor retry continuation action differs", nil)
		}
	case "begin_first_attempt_next_entry":
		if recovery.NextAction() != RecoveryBeginFirstAttemptNextEntry {
			return fail(CodeEvidenceRecoveryRequired, "successor-generation-recovery-snapshot", "successor next-entry continuation action differs", nil)
		}
	default:
		return admissionCorrupt("successor-generation-recovery-snapshot", "successor continuation action is invalid", nil)
	}
	return nil
}

func successorGenerationRecoveryDigest(ready *SuccessorGenerationRecoveryReady) [32]byte {
	if ready == nil || ready.self != ready || ready.prior == nil || ready.prior.binding == nil || ready.state == nil || ready.state.binding == nil || ready.candidateBinding == nil || ready.generation.owner == nil || ready.cursor.valid == nil || !ready.cursor.Valid() || ready.recovery == nil || !validRecoverySnapshotForJournal(ready.recovery, ready.generation, ready.cursor) || ready.factsDigest == ([32]byte{}) {
		return [32]byte{}
	}
	h := sha256.New()
	h.Write([]byte("cloud-agents-platform-successor-generation-recovery-ready/v1\x00"))
	h.Write(ready.prior.binding.canonical[:])
	h.Write(ready.state.binding.canonical[:])
	h.Write(ready.candidateBinding.canonical[:])
	h.Write(ready.factsDigest[:])
	for _, value := range []Digest{ready.generation.executionLineageDigest, ready.generation.journalIdentityDigest, ready.generation.runnerProjectionDecisionDigest, ready.generation.schemaBundleDigest} {
		writeAdmissionString(h, value.String())
	}
	writeGenerationJournalCursor(h, ready.cursor)
	recoveryDigest := generationJournalRecoveryDigest(ready.recovery)
	h.Write(recoveryDigest[:])
	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result
}

func validSuccessorGenerationRecoveryReady(ready *SuccessorGenerationRecoveryReady, candidate OwnedCurrentCandidate) bool {
	return validSuccessorGenerationRecoveryShape(ready, candidate, false)
}

func validConsumedSuccessorGenerationRecoveryReady(ready *SuccessorGenerationRecoveryReady, candidate OwnedCurrentCandidate) bool {
	return validSuccessorGenerationRecoveryShape(ready, candidate, true)
}

func validSuccessorGenerationRecoveryShape(ready *SuccessorGenerationRecoveryReady, candidate OwnedCurrentCandidate, consumed bool) bool {
	if ready == nil || ready.self != ready || ready.binding == nil || ready.binding.ready != ready || ready.prior == nil || ready.state == nil || ready.candidateBinding != candidate.binding || ready.binding.prior != ready.prior || ready.binding.state != ready.state || ready.binding.candidateBinding != ready.candidateBinding || ready.consumed == nil || ready.consumed.Load() != consumed || !validConsumedSuccessorGenerationReplayReady(ready.prior, candidate) || ready.state != ready.prior.state || ready.generation.owner != candidate.owner || ready.generation.journalIdentityDigest != ready.state.journal || ready.cursor.valid == nil || !ready.cursor.Valid() || !sameGenerationIdentity(ready.cursor.generation, ready.generation) || ready.recovery == nil || !validRecoverySnapshotForJournal(ready.recovery, ready.generation, ready.cursor) || ready.factsDigest == ([32]byte{}) || ready.binding.canonical == ([32]byte{}) || ready.binding.canonical != successorGenerationRecoveryDigest(ready) {
		return false
	}
	registered, ok := successorGenerationRecoveryRegistry.Load(ready)
	record, recordOK := registered.(successorGenerationRecoveryRecord)
	return ok && recordOK && record.ready == ready && record.binding == ready.binding && record.prior == ready.prior && record.state == ready.state && record.candidateBinding == ready.candidateBinding && record.cursorValid == ready.cursor.valid && record.canonical == ready.binding.canonical
}

func successorGenerationRecoveryReadyRecordMatches(ready *SuccessorGenerationRecoveryReady) bool {
	if ready == nil || ready.self != ready || ready.binding == nil || ready.binding.ready != ready || ready.prior == nil || ready.state == nil || ready.candidateBinding == nil || ready.binding.prior != ready.prior || ready.binding.state != ready.state || ready.binding.candidateBinding != ready.candidateBinding || ready.consumed == nil || !ready.consumed.Load() || ready.cursor.valid == nil || ready.recovery == nil || ready.factsDigest == ([32]byte{}) || ready.binding.canonical == ([32]byte{}) {
		return false
	}
	registered, ok := successorGenerationRecoveryRegistry.Load(ready)
	record, recordOK := registered.(successorGenerationRecoveryRecord)
	if !ok || !recordOK || record.ready != ready || record.binding != ready.binding || record.prior != ready.prior || record.state != ready.state || record.candidateBinding != ready.candidateBinding || record.cursorValid != ready.cursor.valid || record.canonical != ready.binding.canonical {
		return false
	}
	replayValue, replayOK := successorGenerationReplayRegistry.Load(ready.prior)
	replayRecord, replayRecordOK := replayValue.(successorGenerationReplayRecord)
	if !replayOK || !replayRecordOK || replayRecord.ready != ready.prior || replayRecord.binding != ready.prior.binding || replayRecord.state != ready.state || replayRecord.candidateBinding != ready.candidateBinding || replayRecord.lease != ready.prior.lease || replayRecord.snapshot != ready.prior.snapshot || replayRecord.canonical != ready.prior.binding.canonical {
		return false
	}
	handoffValue, handoffOK := successorGenerationHandoffRegistry.Load(ready.prior.prior)
	handoffRecord, handoffRecordOK := handoffValue.(successorGenerationHandoffRecord)
	return handoffOK && handoffRecordOK && handoffRecord.ready == ready.prior.prior && handoffRecord.binding == ready.prior.prior.binding && handoffRecord.state == ready.state && handoffRecord.stateBinding == ready.state.binding && handoffRecord.candidateBinding == ready.candidateBinding && handoffRecord.lease == ready.prior.lease && handoffRecord.canonical == ready.prior.prior.binding.canonical
}

func (r *SuccessorGenerationReplayReady) failSuccessorRecovery(cause error, operation string) (*SuccessorGenerationRecoveryReady, error) {
	cleanupErr := closeSuccessorGenerationReplay(r, operation+"-cleanup")
	if cleanupErr != nil {
		return nil, cleanupErr
	}
	if cause == nil {
		cause = fail(CodeEvidenceJournalFailed, operation, "successor recovery failed", nil)
	}
	if IsCode(cause, CodeEvidenceJournalCorrupt) || IsCode(cause, CodeEvidenceJournalFailed) || IsCode(cause, CodeEvidenceRecoveryRequired) || IsCode(cause, CodeContextCanceled) || IsCode(cause, CodeDeadlineExceeded) {
		return nil, cause
	}
	return nil, mapEvidenceAdmissionError(cause, operation)
}

// Close releases the successor generation lease and invalidates recovery
// authority. It cannot be copied into a journal or session.
func (r *SuccessorGenerationRecoveryReady) Close() error {
	if r == nil || r.self != r || r.consumed == nil || !r.consumed.CompareAndSwap(false, true) {
		return admissionFailed("successor-generation-recovery-close", "successor recovery authority is unavailable", nil)
	}
	if r.cursor.valid != nil {
		r.cursor.valid.Store(false)
	}
	return closeConsumedSuccessorGenerationRecovery(r, "successor-generation-recovery-close")
}

func closeConsumedSuccessorGenerationRecovery(r *SuccessorGenerationRecoveryReady, operation string) error {
	if r == nil || r.self != r || operation == "" {
		return admissionFailed(operation, "successor recovery authority is unavailable", nil)
	}
	registered, ok := successorGenerationRecoveryRegistry.Load(r)
	record, recordOK := registered.(successorGenerationRecoveryRecord)
	successorGenerationRecoveryRegistry.Delete(r)
	if !ok || !recordOK || record.ready != r || record.canonical == ([32]byte{}) || record.prior == nil || record.cursorValid == nil {
		return admissionFailed(operation, "immutable successor recovery authority is unavailable", nil)
	}
	record.cursorValid.Store(false)
	return closeSuccessorGenerationReplay(record.prior, operation)
}
