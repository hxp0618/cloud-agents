package migration

import (
	"bytes"
	"context"
	"crypto/sha256"
	"sync"
	"sync/atomic"
)

// HistoricalSuccessorGenerationRecoveryReady is same-verifier recovery
// authority for an activated crash-recovered successor. It can enter the
// normal journal/session path only while B is current; a historical B must be
// superseded again before current C can use a journal.
type HistoricalSuccessorGenerationRecoveryReady struct {
	self                 *HistoricalSuccessorGenerationRecoveryReady
	prior                *HistoricalSuccessorGenerationReplayReady
	planned              *verifiedAdmissionRegisteredGeneration
	candidateBinding     *verifiedEvidenceRunBinding
	generation           generationIdentity
	cursor               JournalCursor
	recovery             *RecoverySnapshot
	factsDigest          [32]byte
	executionBindings    *VerifiedRecoveryExecutionBindings
	requiresSupersession bool
	binding              *historicalSuccessorGenerationRecoveryBinding
	consumed             *atomic.Bool
}

type historicalSuccessorGenerationRecoveryBinding struct {
	ready            *HistoricalSuccessorGenerationRecoveryReady
	prior            *HistoricalSuccessorGenerationReplayReady
	planned          *verifiedAdmissionRegisteredGeneration
	candidateBinding *verifiedEvidenceRunBinding
	canonical        [32]byte
}

type historicalSuccessorGenerationRecoveryRecord struct {
	ready                *HistoricalSuccessorGenerationRecoveryReady
	binding              *historicalSuccessorGenerationRecoveryBinding
	prior                *HistoricalSuccessorGenerationReplayReady
	planned              *verifiedAdmissionRegisteredGeneration
	candidateBinding     *verifiedEvidenceRunBinding
	cursorValid          *atomic.Bool
	executionBindings    *VerifiedRecoveryExecutionBindings
	requiresSupersession bool
	canonical            [32]byte
}

var historicalSuccessorGenerationRecoveryRegistry sync.Map

// RequiresSupersession reports whether the activated B is still historical
// relative to current C. It is diagnostic and grants no mutation authority.
func (r *HistoricalSuccessorGenerationRecoveryReady) RequiresSupersession() bool {
	if r == nil || r.self != r || r.binding == nil || r.consumed == nil || r.consumed.Load() {
		return false
	}
	value, ok := historicalSuccessorGenerationRecoveryRegistry.Load(r)
	record, recordOK := value.(historicalSuccessorGenerationRecoveryRecord)
	return ok && recordOK && record.ready == r && record.binding == r.binding && record.requiresSupersession && record.requiresSupersession == r.requiresSupersession && record.canonical == r.binding.canonical
}

// BindRecovery consumes strict replay, rebinds B's own registered facts and
// receipts, and reconstructs its inherited header-only recovery snapshot.
func (r *HistoricalSuccessorGenerationReplayReady) BindRecovery(ctx context.Context, candidate OwnedCurrentCandidate) (*HistoricalSuccessorGenerationRecoveryReady, error) {
	if r == nil || !validHistoricalSuccessorGenerationReplayReady(r, candidate) {
		return nil, fail(CodeEvidenceRecoveryRequired, "historical-successor-recovery", "historical successor replay authority is unavailable", nil)
	}
	if err := contextAdmissionError(ctx); err != nil {
		return nil, err
	}
	if !r.consumed.CompareAndSwap(false, true) {
		return nil, fail(CodeEvidenceRecoveryRequired, "historical-successor-recovery", "historical successor replay authority is consumed", nil)
	}
	generationReady := r.prior.prior
	if generationReady == nil || generationReady.planned == nil {
		return r.failHistoricalSuccessorRecovery(nil, "historical-successor-recovery-plan")
	}
	headerRaw, err := r.snapshot.ReadSegment(ctx, 0)
	if err != nil {
		return r.failHistoricalSuccessorRecovery(err, "historical-successor-recovery-header")
	}
	frames, err := decodeGenerationRecoveryFrames(headerRaw)
	if err != nil {
		return r.failHistoricalSuccessorRecovery(err, "historical-successor-recovery-header")
	}
	if len(frames) != 1 || frames[0].RecordKind != EvidenceRecordHeader || frames[0].Record.Header == nil || frames[0].RecordDigest != generationReady.headerFrame.RecordDigest || !canonicalEqual(frames[0], generationReady.headerFrame) || !bytes.Equal(headerRaw, generationReady.headerBytes) {
		return r.failHistoricalSuccessorRecovery(admissionCorrupt("historical-successor-recovery-header", "historical successor replay is not exact header-only state", nil), "historical-successor-recovery-header")
	}
	planned := generationReady.planned
	_, _, receiptsOK := historicalSuccessorGenerationReplayReceipts(r, candidate, *frames[0].Record.Header)
	if !receiptsOK {
		return r.failHistoricalSuccessorRecovery(fail(CodeEvidenceRecoveryRequired, "historical-successor-recovery-receipts", "registered successor receipts are unavailable", nil), "historical-successor-recovery-receipts")
	}
	facts, chain, schema, err := buildRegisteredBrandNewRecoveryWitness(planned, candidate.verifiedRun.currentDecision, *frames[0].Record.Header)
	if err != nil {
		return r.failHistoricalSuccessorRecovery(err, "historical-successor-recovery-facts")
	}
	generation := planned.descriptor.identity
	if generation.owner != candidate.owner || !sameGenerationHeader(generation, *frames[0].Record.Header) || generation.journalIdentityDigest != r.journal || planned.descriptor.replayTailDigest != r.journalTail {
		return r.failHistoricalSuccessorRecovery(admissionCorrupt("historical-successor-recovery-header", "registered successor generation differs from strict replay", nil), "historical-successor-recovery-header")
	}
	if err := validateEvidenceChainWithWitness(frames, chain); err != nil {
		return r.failHistoricalSuccessorRecovery(admissionCorrupt("historical-successor-recovery-witness", "registered successor header witness differs", err), "historical-successor-recovery-witness")
	}
	previous := r.journalTail
	cursor := JournalCursor{
		owner: candidate.owner, generation: generation, segmentIndex: 0, nextSequence: r.journalRecords,
		previousRecordDigest: &previous, lineageIndexNextSequence: r.indexRecords,
		lineageIndexPreviousRecordDigest: generationReady.activatedFrame.RecordDigest, valid: &atomic.Bool{},
	}
	cursor.valid.Store(true)
	continuation, err := historicalSuccessorRecoveredContinuation(r, generation, cursor)
	if err != nil {
		cursor.valid.Store(false)
		return r.failHistoricalSuccessorRecovery(err, "historical-successor-recovery-continuation")
	}
	recovery, err := buildRecoverySnapshot(frames, cursor, generation, continuation, schema)
	if err != nil || recovery == nil || recovery.State() != RecoveryBrandNewInherited || recovery.TailDigest() != r.journalTail {
		cursor.valid.Store(false)
		if err == nil {
			err = fail(CodeEvidenceRecoveryRequired, "historical-successor-recovery-snapshot", "inherited successor recovery snapshot is unavailable", nil)
		}
		return r.failHistoricalSuccessorRecovery(err, "historical-successor-recovery-snapshot")
	}
	if err := validateHistoricalSuccessorRecoveryAction(r, recovery, generationReady.reservedFrame.Record.Reserved.Continuation); err != nil {
		cursor.valid.Store(false)
		return r.failHistoricalSuccessorRecovery(err, "historical-successor-recovery-snapshot")
	}
	execution, requiresSupersession, err := bindHistoricalSuccessorRecoveryExecution(planned, candidate.verifiedRun.currentDecision, recovery)
	if err != nil {
		cursor.valid.Store(false)
		return r.failHistoricalSuccessorRecovery(err, "historical-successor-recovery-execution")
	}
	if err := r.snapshot.Revalidate(ctx); err != nil {
		cursor.valid.Store(false)
		return r.failHistoricalSuccessorRecovery(err, "historical-successor-recovery-terminal")
	}
	ready := &HistoricalSuccessorGenerationRecoveryReady{
		prior: r, planned: planned, candidateBinding: candidate.binding, generation: generation,
		cursor: cursor, recovery: recovery, factsDigest: admissionRecoveryFactsDigest(facts),
		executionBindings: execution, requiresSupersession: requiresSupersession, consumed: &atomic.Bool{},
	}
	ready.self = ready
	ready.binding = &historicalSuccessorGenerationRecoveryBinding{ready: ready, prior: r, planned: planned, candidateBinding: candidate.binding}
	ready.binding.canonical = historicalSuccessorGenerationRecoveryDigest(ready)
	historicalSuccessorGenerationRecoveryRegistry.Store(ready, historicalSuccessorGenerationRecoveryRecord{
		ready: ready, binding: ready.binding, prior: r, planned: planned, candidateBinding: candidate.binding,
		cursorValid: cursor.valid, executionBindings: execution, requiresSupersession: requiresSupersession,
		canonical: ready.binding.canonical,
	})
	if !validHistoricalSuccessorGenerationRecoveryReady(ready, candidate) {
		historicalSuccessorGenerationRecoveryRegistry.Delete(ready)
		cursor.valid.Store(false)
		return r.failHistoricalSuccessorRecovery(fail(CodeEvidenceRecoveryRequired, "historical-successor-recovery-seal", "historical successor recovery authority could not be sealed", nil), "historical-successor-recovery-seal")
	}
	return ready, nil
}

func historicalSuccessorGenerationReplayReceipts(replay *HistoricalSuccessorGenerationReplayReady, candidate OwnedCurrentCandidate, header JournalHeader) (VerifiedContentReceipt, VerifiedDecisionRecoveryReceipt, bool) {
	if replay == nil || replay.prior == nil || replay.prior.prior == nil || replay.prior.prior.planned == nil || !validOwnedCurrentCandidate(candidate) || header.Validate() != nil || replay.prior.prior.headerFrame.Record.Header == nil || !canonicalEqual(header, *replay.prior.prior.headerFrame.Record.Header) {
		return VerifiedContentReceipt{}, VerifiedDecisionRecoveryReceipt{}, false
	}
	value, ok := historicalSuccessorGenerationHandoffRegistry.Load(replay.prior)
	record, recordOK := value.(historicalSuccessorGenerationHandoffRecord)
	planned := replay.prior.prior.planned
	if !ok || !recordOK || record.ready != replay.prior || record.binding != replay.prior.binding || record.prior != replay.prior.prior || record.planned != planned || record.candidateBinding != candidate.binding || record.lease != replay.lease || record.authority == nil || record.receipt == nil || record.receipt.owner != candidate.verifiedRun.currentDecision.owner || record.receipt.authorityDigest != record.authority.digest || !record.receipt.consumed.Load() || record.canonical == ([32]byte{}) || record.canonical != replay.prior.binding.canonical {
		return VerifiedContentReceipt{}, VerifiedDecisionRecoveryReceipt{}, false
	}
	runtime, recovery := planned.runtimeReceipt, planned.recoveryReceipt
	if !validRegisteredRuntimeReceipt(runtime, candidate.owner, header.OuterArtifactDigest, header.OuterArtifactSizeBytes) || !validRegisteredDecisionRecoveryReceipt(recovery, candidate.owner, header.DecisionRecoveryArtifactSHA256, header.DecisionRecoveryArtifactSizeBytes) || !registeredReceiptsSameStore(runtime, recovery) || !validRegisteredRuntimeReceipt(record.receipt.runtimeReceipt, candidate.owner, runtime.digest, runtime.sizeBytes) || !record.receipt.runtimeReceipt.registeredPublication.SameObject(runtime.registeredPublication) || !validRegisteredDecisionRecoveryReceipt(record.receipt.recoveryReceipt, candidate.owner, recovery.digest, recovery.sizeBytes) || !record.receipt.recoveryReceipt.registeredPublication.SameObject(recovery.registeredPublication) {
		return VerifiedContentReceipt{}, VerifiedDecisionRecoveryReceipt{}, false
	}
	return runtime, recovery, true
}

func historicalSuccessorRecoveredContinuation(replay *HistoricalSuccessorGenerationReplayReady, generation generationIdentity, cursor JournalCursor) (recoveredContinuation, error) {
	if replay == nil || replay.prior == nil || replay.prior.prior == nil || replay.prior.prior.reservedFrame.Record.Reserved == nil {
		return recoveredContinuation{}, fail(CodeEvidenceRecoveryRequired, "historical-successor-recovery-continuation", "registered successor continuation is unavailable", nil)
	}
	reserved := replay.prior.prior.reservedFrame
	continuation := reserved.Record.Reserved.Continuation
	if continuation == nil {
		return recoveredContinuation{inheritedWithoutContext: true}, nil
	}
	value := cloneProjectionValue(*continuation)
	if err := value.Validate(); err != nil {
		return recoveredContinuation{}, admissionCorrupt("historical-successor-recovery-continuation", "registered successor continuation is invalid", err)
	}
	return recoveredContinuation{owned: recoveredValue(generation, cursor, replay.journalTail, reserved.RecordDigest, value)}, nil
}

func validateHistoricalSuccessorRecoveryAction(replay *HistoricalSuccessorGenerationReplayReady, recovery *RecoverySnapshot, continuation *LineageContinuationContext) error {
	if replay == nil || replay.prior == nil || replay.prior.prior == nil || recovery == nil || recovery.State() != RecoveryBrandNewInherited {
		return fail(CodeEvidenceRecoveryRequired, "historical-successor-recovery-snapshot", "historical successor recovery state is not inherited brand-new", nil)
	}
	reservedDigest := replay.prior.prior.reservedFrame.RecordDigest
	if continuation == nil {
		if recovery.NextAction() != RecoveryBeginFirstAttempt || recovery.LineageContinuation() != nil {
			return fail(CodeEvidenceRecoveryRequired, "historical-successor-recovery-snapshot", "header-only successor recovery action differs", nil)
		}
		return nil
	}
	recovered := recovery.LineageContinuation()
	if recovered == nil || recovered.RecordDigest() != reservedDigest || !canonicalEqual(recovered.Value(), *continuation) {
		return fail(CodeEvidenceRecoveryRequired, "historical-successor-recovery-snapshot", "successor continuation differs from durable reservation", nil)
	}
	switch continuation.StartAction {
	case "begin_next_attempt":
		if recovery.NextAction() != RecoveryBeginNextAttempt {
			return fail(CodeEvidenceRecoveryRequired, "historical-successor-recovery-snapshot", "successor retry action differs", nil)
		}
	case "begin_first_attempt_next_entry":
		if recovery.NextAction() != RecoveryBeginFirstAttemptNextEntry {
			return fail(CodeEvidenceRecoveryRequired, "historical-successor-recovery-snapshot", "successor next-entry action differs", nil)
		}
	default:
		return admissionCorrupt("historical-successor-recovery-snapshot", "successor continuation action is invalid", nil)
	}
	return nil
}

func bindHistoricalSuccessorRecoveryExecution(planned *verifiedAdmissionRegisteredGeneration, current OwnedVerifiedDecision, recovery *RecoverySnapshot) (*VerifiedRecoveryExecutionBindings, bool, error) {
	if planned == nil || recovery == nil || current.owner == nil || planned.decision.owner != current.owner {
		return nil, false, fail(CodeEvidenceRecoveryRequired, "historical-successor-recovery-execution", "registered successor execution inputs are unavailable", nil)
	}
	if planned.decision.digest == current.digest {
		if planned.policy != nil {
			return nil, false, fail(CodeEvidenceRecoveryRequired, "historical-successor-recovery-execution", "current successor carried a historical policy", nil)
		}
		return nil, false, nil
	}
	if planned.policy == nil {
		return nil, true, fail(CodeEvidenceRecoveryRequired, "historical-successor-recovery-execution", "historical successor policy is unavailable", nil)
	}
	execution, err := bindRecoveryExecution(*planned.policy, current, planned.decision, planned.bindings, planned.descriptor, recovery)
	if err != nil {
		return nil, true, fail(CodeEvidenceRecoveryRequired, "historical-successor-recovery-execution", "historical successor execution cannot be rebound", nil)
	}
	return &execution, true, nil
}

func historicalSuccessorGenerationRecoveryDigest(ready *HistoricalSuccessorGenerationRecoveryReady) [32]byte {
	if ready == nil || ready.self != ready || ready.prior == nil || ready.prior.binding == nil || ready.planned == nil || ready.candidateBinding == nil || ready.generation.owner == nil || ready.cursor.valid == nil || !ready.cursor.Valid() || ready.recovery == nil || !validRecoverySnapshotForJournal(ready.recovery, ready.generation, ready.cursor) || ready.recovery.State() != RecoveryBrandNewInherited || ready.factsDigest == ([32]byte{}) || ready.requiresSupersession != (ready.executionBindings != nil) {
		return [32]byte{}
	}
	h := sha256.New()
	h.Write([]byte("cloud-agents-platform-historical-successor-generation-recovery-ready/v1\x00"))
	h.Write(ready.prior.binding.canonical[:])
	h.Write(ready.planned.canonical[:])
	h.Write(ready.candidateBinding.canonical[:])
	h.Write(ready.factsDigest[:])
	for _, value := range []Digest{ready.generation.executionLineageDigest, ready.generation.journalIdentityDigest, ready.generation.runnerProjectionDecisionDigest, ready.generation.schemaBundleDigest} {
		writeAdmissionString(h, value.String())
	}
	writeGenerationJournalCursor(h, ready.cursor)
	recoveryDigest := generationJournalRecoveryDigest(ready.recovery)
	h.Write(recoveryDigest[:])
	if ready.requiresSupersession {
		h.Write([]byte{1})
		if ready.executionBindings == nil || ready.executionBindings.digest.Validate() != nil {
			return [32]byte{}
		}
		writeAdmissionString(h, ready.executionBindings.digest.String())
	} else {
		h.Write([]byte{0})
	}
	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result
}

func validHistoricalSuccessorGenerationRecoveryReady(ready *HistoricalSuccessorGenerationRecoveryReady, candidate OwnedCurrentCandidate) bool {
	return validHistoricalSuccessorGenerationRecoveryShape(ready, candidate, false)
}

func validConsumedHistoricalSuccessorGenerationRecoveryReady(ready *HistoricalSuccessorGenerationRecoveryReady, candidate OwnedCurrentCandidate) bool {
	return validHistoricalSuccessorGenerationRecoveryShape(ready, candidate, true)
}

func validHistoricalSuccessorGenerationRecoveryShape(ready *HistoricalSuccessorGenerationRecoveryReady, candidate OwnedCurrentCandidate, consumed bool) bool {
	if !validOwnedCurrentCandidate(candidate) || ready == nil || ready.self != ready || ready.binding == nil || ready.binding.ready != ready || ready.prior == nil || ready.planned == nil || ready.candidateBinding != candidate.binding || ready.binding.prior != ready.prior || ready.binding.planned != ready.planned || ready.binding.candidateBinding != ready.candidateBinding || ready.consumed == nil || ready.consumed.Load() != consumed || !validConsumedHistoricalSuccessorGenerationReplayReady(ready.prior, candidate) || ready.prior.prior == nil || ready.prior.prior.prior == nil || ready.planned != ready.prior.prior.prior.planned || !validVerifiedAdmissionRegisteredGeneration(ready.planned, candidate.verifiedRun.currentDecision) || ready.generation.owner != candidate.owner || !sameGenerationIdentity(ready.generation, ready.planned.descriptor.identity) || ready.generation.journalIdentityDigest != ready.prior.journal || ready.planned.descriptor.replayTailDigest != ready.prior.journalTail || ready.cursor.valid == nil || !ready.cursor.Valid() || !sameGenerationIdentity(ready.cursor.generation, ready.generation) || ready.cursor.segmentIndex != 0 || ready.cursor.nextSequence != ready.prior.journalRecords || ready.cursor.previousRecordDigest == nil || *ready.cursor.previousRecordDigest != ready.prior.journalTail || ready.cursor.lineageIndexNextSequence != ready.prior.indexRecords || ready.cursor.lineageIndexPreviousRecordDigest != ready.prior.prior.prior.activatedFrame.RecordDigest || ready.recovery == nil || ready.recovery.TailDigest() != ready.prior.journalTail || !validRecoverySnapshotForJournal(ready.recovery, ready.generation, ready.cursor) || ready.recovery.State() != RecoveryBrandNewInherited || ready.factsDigest == ([32]byte{}) || ready.binding.canonical == ([32]byte{}) || ready.binding.canonical != historicalSuccessorGenerationRecoveryDigest(ready) || !validHistoricalSuccessorRecoveryExecution(ready, candidate) {
		return false
	}
	facts, _, _, err := buildRegisteredBrandNewRecoveryWitness(ready.planned, candidate.verifiedRun.currentDecision, ready.planned.descriptor.header)
	if err != nil || ready.factsDigest != admissionRecoveryFactsDigest(facts) || validateHistoricalSuccessorRecoveryAction(ready.prior, ready.recovery, ready.prior.prior.prior.reservedFrame.Record.Reserved.Continuation) != nil {
		return false
	}
	value, ok := historicalSuccessorGenerationRecoveryRegistry.Load(ready)
	record, recordOK := value.(historicalSuccessorGenerationRecoveryRecord)
	return ok && recordOK && record.ready == ready && record.binding == ready.binding && record.prior == ready.prior && record.planned == ready.planned && record.candidateBinding == ready.candidateBinding && record.cursorValid == ready.cursor.valid && record.executionBindings == ready.executionBindings && record.requiresSupersession == ready.requiresSupersession && record.canonical == ready.binding.canonical
}

func validHistoricalSuccessorRecoveryExecution(ready *HistoricalSuccessorGenerationRecoveryReady, candidate OwnedCurrentCandidate) bool {
	if ready == nil || ready.planned == nil || !validOwnedCurrentCandidate(candidate) {
		return false
	}
	wantHistorical := ready.planned.decision.digest != candidate.verifiedRun.currentDecision.digest
	if ready.requiresSupersession != wantHistorical {
		return false
	}
	if !wantHistorical {
		return ready.executionBindings == nil && ready.planned.policy == nil && ready.generation.runnerProjectionDecisionDigest == candidate.verifiedRun.runnerProjectionDecisionDigest && ready.generation.schemaBundleDigest == candidate.verifiedRun.schemaBundleDigest
	}
	if ready.executionBindings == nil || ready.planned.policy == nil {
		return false
	}
	expected, err := bindRecoveryExecution(*ready.planned.policy, candidate.verifiedRun.currentDecision, ready.planned.decision, ready.planned.bindings, ready.planned.descriptor, ready.recovery)
	actual := ready.executionBindings
	return err == nil && actual.owner == expected.owner && actual.session == expected.session && sameGenerationIdentity(actual.generation, expected.generation) && actual.tailDigest == expected.tailDigest && actual.digest == expected.digest && canonicalEqual(actual.policy, expected.policy) && canonicalEqual(actual.subject, expected.subject) && validRecoverySnapshotForJournal(actual.snapshot, ready.generation, ready.cursor) && generationJournalRecoveryDigest(actual.snapshot) == generationJournalRecoveryDigest(ready.recovery)
}

func historicalSuccessorGenerationRecoveryReadyRecordMatches(ready *HistoricalSuccessorGenerationRecoveryReady) bool {
	if ready == nil || ready.self != ready || ready.binding == nil || ready.binding.ready != ready || ready.prior == nil || ready.planned == nil || ready.candidateBinding == nil || ready.binding.prior != ready.prior || ready.binding.planned != ready.planned || ready.binding.candidateBinding != ready.candidateBinding || ready.consumed == nil || !ready.consumed.Load() || ready.cursor.valid == nil || ready.recovery == nil || ready.factsDigest == ([32]byte{}) || ready.binding.canonical == ([32]byte{}) {
		return false
	}
	value, ok := historicalSuccessorGenerationRecoveryRegistry.Load(ready)
	record, recordOK := value.(historicalSuccessorGenerationRecoveryRecord)
	if !ok || !recordOK || record.ready != ready || record.binding != ready.binding || record.prior != ready.prior || record.planned != ready.planned || record.candidateBinding != ready.candidateBinding || record.cursorValid != ready.cursor.valid || record.executionBindings != ready.executionBindings || record.requiresSupersession != ready.requiresSupersession || record.canonical != ready.binding.canonical {
		return false
	}
	replayValue, replayOK := historicalSuccessorGenerationReplayRegistry.Load(ready.prior)
	replayRecord, replayRecordOK := replayValue.(historicalSuccessorGenerationReplayRecord)
	if !replayOK || !replayRecordOK || replayRecord.ready != ready.prior || replayRecord.binding != ready.prior.binding || replayRecord.prior != ready.prior.prior || replayRecord.candidateBinding != ready.candidateBinding || replayRecord.lease != ready.prior.lease || replayRecord.snapshot != ready.prior.snapshot || replayRecord.canonical != ready.prior.binding.canonical {
		return false
	}
	handoffValue, handoffOK := historicalSuccessorGenerationHandoffRegistry.Load(ready.prior.prior)
	handoffRecord, handoffRecordOK := handoffValue.(historicalSuccessorGenerationHandoffRecord)
	return handoffOK && handoffRecordOK && handoffRecord.ready == ready.prior.prior && handoffRecord.binding == ready.prior.prior.binding && handoffRecord.prior == ready.prior.prior.prior && handoffRecord.planned == ready.planned && handoffRecord.candidateBinding == ready.candidateBinding && handoffRecord.lease == ready.prior.lease && handoffRecord.canonical == ready.prior.prior.binding.canonical
}

func (r *HistoricalSuccessorGenerationReplayReady) failHistoricalSuccessorRecovery(cause error, operation string) (*HistoricalSuccessorGenerationRecoveryReady, error) {
	cleanupErr := closeHistoricalSuccessorGenerationReplay(r, operation+"-cleanup")
	if cleanupErr != nil {
		return nil, cleanupErr
	}
	if cause == nil {
		cause = fail(CodeEvidenceJournalFailed, operation, "historical successor recovery failed", nil)
	}
	if IsCode(cause, CodeEvidenceJournalCorrupt) || IsCode(cause, CodeEvidenceJournalFailed) || IsCode(cause, CodeEvidenceRecoveryRequired) || IsCode(cause, CodeContextCanceled) || IsCode(cause, CodeDeadlineExceeded) {
		return nil, cause
	}
	return nil, mapEvidenceAdmissionError(cause, operation)
}

// Close invalidates unused recovery authority and releases the retained
// generation lease. A successfully consumed current-B authority is instead
// owned and closed by its concrete journal/session.
func (r *HistoricalSuccessorGenerationRecoveryReady) Close() error {
	if r == nil || r.self != r || r.consumed == nil || !r.consumed.CompareAndSwap(false, true) {
		return admissionFailed("historical-successor-recovery-close", "historical successor recovery authority is unavailable", nil)
	}
	if r.cursor.valid != nil {
		r.cursor.valid.Store(false)
	}
	return closeConsumedHistoricalSuccessorGenerationRecovery(r, "historical-successor-recovery-close")
}

func closeConsumedHistoricalSuccessorGenerationRecovery(r *HistoricalSuccessorGenerationRecoveryReady, operation string) error {
	if r == nil || r.self != r || operation == "" {
		return admissionFailed(operation, "historical successor recovery authority is unavailable", nil)
	}
	value, ok := historicalSuccessorGenerationRecoveryRegistry.Load(r)
	record, recordOK := value.(historicalSuccessorGenerationRecoveryRecord)
	historicalSuccessorGenerationRecoveryRegistry.Delete(r)
	if !ok || !recordOK || record.ready != r || record.prior == nil || record.cursorValid == nil || record.canonical == ([32]byte{}) {
		return admissionFailed(operation, "immutable historical successor recovery authority is unavailable", nil)
	}
	record.cursorValid.Store(false)
	return closeHistoricalSuccessorGenerationReplay(record.prior, operation)
}
