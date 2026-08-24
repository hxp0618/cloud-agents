package migration

import (
	"bytes"
	"context"
	"crypto/sha256"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/evidencefs"
)

type SuccessorSupersessionTransitionResult struct {
	outcome                evidencefs.AdmissionTransitionOutcome
	next                   *SuccessorAdjacentReserveReady
	candidateDigest        [32]byte
	candidateSequence      uint64
	candidateRevision      uint64
	previousRevision       uint64
	supersededRecordDigest Digest
	plannedReservedDigest  Digest
}

func (r SuccessorSupersessionTransitionResult) Outcome() evidencefs.AdmissionTransitionOutcome {
	return r.outcome
}
func (r SuccessorSupersessionTransitionResult) Next() *SuccessorAdjacentReserveReady {
	return r.next
}
func (r SuccessorSupersessionTransitionResult) CandidateKind() string {
	return "generation_superseded"
}
func (r SuccessorSupersessionTransitionResult) CandidateDigest() [32]byte {
	return r.candidateDigest
}
func (r SuccessorSupersessionTransitionResult) CandidateSequence() uint64 {
	return r.candidateSequence
}
func (r SuccessorSupersessionTransitionResult) CandidateRevision() uint64 {
	return r.candidateRevision
}
func (r SuccessorSupersessionTransitionResult) PreviousRevision() uint64 {
	return r.previousRevision
}
func (r SuccessorSupersessionTransitionResult) SupersededRecordDigest() Digest {
	return r.supersededRecordDigest
}
func (r SuccessorSupersessionTransitionResult) PlannedReservedRecordDigest() Digest {
	return r.plannedReservedDigest
}

type SuccessorAdjacentReserveReady struct {
	self  *SuccessorAdjacentReserveReady
	state *successorAdmissionState
}

type SuccessorReservationTransitionResult struct {
	outcome              evidencefs.AdmissionTransitionOutcome
	next                 *SuccessorReservedDurablePermit
	candidateDigest      [32]byte
	candidateSequence    uint64
	candidateRevision    uint64
	previousRevision     uint64
	reservedRecordDigest Digest
}

func (r SuccessorReservationTransitionResult) Outcome() evidencefs.AdmissionTransitionOutcome {
	return r.outcome
}
func (r SuccessorReservationTransitionResult) Next() *SuccessorReservedDurablePermit {
	return r.next
}
func (r SuccessorReservationTransitionResult) CandidateKind() string {
	return "generation_reserved"
}
func (r SuccessorReservationTransitionResult) CandidateDigest() [32]byte {
	return r.candidateDigest
}
func (r SuccessorReservationTransitionResult) CandidateSequence() uint64 {
	return r.candidateSequence
}
func (r SuccessorReservationTransitionResult) CandidateRevision() uint64 {
	return r.candidateRevision
}
func (r SuccessorReservationTransitionResult) PreviousRevision() uint64 {
	return r.previousRevision
}
func (r SuccessorReservationTransitionResult) ReservedRecordDigest() Digest {
	return r.reservedRecordDigest
}

type SuccessorReservedDurablePermit struct {
	self  *SuccessorReservedDurablePermit
	state *successorAdmissionState
}

// AppendGenerationSuperseded durably appends only the old generation's
// supersession frame. Its next authority can do exactly one thing: append the
// byte-exact nested GenerationReserved body at the adjacent index position.
func (r *SuccessorReceiptBoundReady) AppendGenerationSuperseded(ctx context.Context, candidate OwnedCurrentCandidate) (SuccessorSupersessionTransitionResult, error) {
	pre := SuccessorSupersessionTransitionResult{outcome: evidencefs.AdmissionTransitionPreMutationFailure, candidateSequence: 6}
	if r == nil || r.self != r || r.state == nil || !validSuccessorAdmissionState(r, r.state, successorAdmissionReceiptBound, candidate) || !successorPlannedGenerationExact(r.state, candidate) {
		return pre, fail(CodeEvidenceRecoveryRequired, "successor-generation-supersede", "successor receipt-bound authority is unavailable", nil)
	}
	if err := contextAdmissionError(ctx); err != nil {
		return pre, err
	}
	planned := append([]byte(nil), r.state.plan.supersededFrameBytes...)
	pre.previousRevision = r.state.revision
	pre.candidateRevision = r.state.revision + 1
	pre.candidateDigest = sha256.Sum256(planned)
	pre.supersededRecordDigest = r.state.plan.supersededFrame.RecordDigest
	pre.plannedReservedDigest = r.state.plan.reservedFrame.RecordDigest
	if pre.candidateDigest == ([32]byte{}) || pre.supersededRecordDigest.Validate() != nil || pre.plannedReservedDigest.Validate() != nil {
		return pre, fail(CodeEvidenceRecoveryRequired, "successor-generation-supersede", "planned supersession bytes are invalid", nil)
	}
	prefix, err := readSuccessorCurrentIndex(ctx, r.state, r.state.history.targetIndexRecords, r.state.history.targetIndexTail, [32]byte{}, 0)
	if err != nil {
		return pre, err
	}
	frame := r.state.plan.supersededFrame
	if frame.Sequence != uint64(len(prefix.frames)) || frame.PreviousRecordDigest == nil || *frame.PreviousRecordDigest != prefix.tail || !bytes.Equal(planned, r.state.plan.supersededFrameBytes) || !validSuccessorAdmissionState(r, r.state, successorAdmissionReceiptBound, candidate) || !successorPlannedGenerationExact(r.state, candidate) {
		return pre, fail(CodeEvidenceRecoveryRequired, "successor-generation-supersede", "supersession authority changed before append", nil)
	}
	if !r.state.consumed.CompareAndSwap(false, true) {
		return pre, fail(CodeEvidenceRecoveryRequired, "successor-generation-supersede", "successor receipt-bound authority was already consumed", nil)
	}
	fsResult, transitionErr := r.state.mutation.AppendTargetIndex(ctx, r.state.inventory, planned)
	result := SuccessorSupersessionTransitionResult{
		outcome: fsResult.Outcome(), candidateDigest: fsResult.CandidateDigest(), candidateSequence: 6,
		candidateRevision: fsResult.CandidateRevision(), previousRevision: fsResult.PreviousRevision(),
		supersededRecordDigest: pre.supersededRecordDigest, plannedReservedDigest: pre.plannedReservedDigest,
	}
	if fsResult.Outcome() != evidencefs.AdmissionTransitionDurable {
		if fsResult.Outcome() == evidencefs.AdmissionTransitionPreMutationFailure {
			r.state.consumed.CompareAndSwap(true, false)
		}
		return result, mapAdmissionMutationError(transitionErr, "successor-generation-supersede")
	}
	postFailure := func(suffix string) (SuccessorSupersessionTransitionResult, error) {
		_ = fsResult.Invalidate()
		return successorSupersessionUnknown(result), admissionPostMutationFailure("successor-generation-supersede" + suffix)
	}
	if transitionErr != nil || fsResult.CandidateKind() != "target_index_append" || fsResult.CandidateSequence() != 0 || fsResult.CandidateDigest() != pre.candidateDigest || fsResult.PreviousRevision() != pre.previousRevision || fsResult.CandidateRevision() != pre.candidateRevision || fsResult.Inventory() == nil {
		return postFailure("")
	}
	nextInventory := fsResult.Inventory()
	if err := nextInventory.Revalidate(ctx); err != nil {
		return postFailure("-revalidate")
	}
	revision, revisionErr := nextInventory.Revision()
	target, targetErr := nextInventory.Target()
	fullSet, fullSetErr := nextInventory.FullSetDigest()
	if revisionErr != nil || revision != pre.candidateRevision {
		return postFailure("-revision")
	}
	if targetErr != nil || target != r.state.target {
		return postFailure("-target")
	}
	if fullSetErr != nil || fullSet == ([32]byte{}) || fullSet == r.state.fullSet {
		return postFailure("-full-set")
	}
	verified, verifyErr := validateSuccessorIndexAppend(ctx, nextInventory, target, prefix.raw, planned, r.state.history.targetIndexRecords+1, pre.supersededRecordDigest)
	if verifyErr != nil {
		return postFailure("-index")
	}
	if !successorReceiptsExact(r.state, candidate) {
		return postFailure("-receipts")
	}
	nextToken, tokenErr := nextInventory.MutationToken()
	if tokenErr != nil || !nextToken.ValidFor(nextInventory) {
		return postFailure("-token")
	}
	step := successorAdmissionStep{inventory: nextInventory, mutation: nextToken, target: target, fullSet: fullSet, revision: revision}
	nextState := nextSuccessorAdmissionState(r.state, successorAdmissionAdjacentReady, step)
	nextState.fsIndex = fsResult
	nextState.indexPrefixDigest = prefix.digest
	nextState.indexDigest = verified.digest
	nextState.framedDigest = pre.candidateDigest
	nextState.indexPrefixSize = prefix.size
	nextState.indexSize = verified.size
	nextState.indexRecords = uint64(len(verified.frames))
	nextState.indexTail = pre.supersededRecordDigest
	nextState.supersededDigest = pre.supersededRecordDigest
	nextState.reservedDigest = pre.plannedReservedDigest
	next := &SuccessorAdjacentReserveReady{state: nextState}
	next.self = next
	if !sealSuccessorAdmissionState(next, nextState) || !validSuccessorAdmissionState(next, nextState, successorAdmissionAdjacentReady, candidate) {
		if nextState.binding != nil {
			successorAdmissionStateRegistry.Delete(nextState.binding)
		}
		return postFailure("-seal")
	}
	result.next = next
	return result, nil
}

// AppendGenerationReserved consumes the adjacent-only authority. No other
// operation can intervene between the already durable Superseded frame and the
// exact preplanned Reserved frame carried by the consumed successor plan.
func (r *SuccessorAdjacentReserveReady) AppendGenerationReserved(ctx context.Context, candidate OwnedCurrentCandidate) (SuccessorReservationTransitionResult, error) {
	pre := SuccessorReservationTransitionResult{outcome: evidencefs.AdmissionTransitionPreMutationFailure, candidateSequence: 7}
	if r == nil || r.self != r || r.state == nil || !validSuccessorAdmissionState(r, r.state, successorAdmissionAdjacentReady, candidate) || !successorPlannedGenerationExact(r.state, candidate) {
		return pre, fail(CodeEvidenceRecoveryRequired, "successor-generation-reserve", "adjacent reservation authority is unavailable", nil)
	}
	if err := contextAdmissionError(ctx); err != nil {
		return pre, err
	}
	planned := append([]byte(nil), r.state.plan.reservedFrameBytes...)
	pre.previousRevision = r.state.revision
	pre.candidateRevision = r.state.revision + 1
	pre.candidateDigest = sha256.Sum256(planned)
	pre.reservedRecordDigest = r.state.plan.reservedFrame.RecordDigest
	prefix, err := readSuccessorCurrentIndex(ctx, r.state, r.state.indexRecords, r.state.supersededDigest, r.state.indexDigest, r.state.indexSize)
	if err != nil {
		return pre, err
	}
	frame := r.state.plan.reservedFrame
	if pre.candidateDigest == ([32]byte{}) || pre.reservedRecordDigest.Validate() != nil || frame.Sequence != uint64(len(prefix.frames)) || frame.PreviousRecordDigest == nil || *frame.PreviousRecordDigest != r.state.supersededDigest || !bytes.HasSuffix(prefix.raw, r.state.plan.supersededFrameBytes) || sha256.Sum256(prefix.raw[:len(prefix.raw)-len(r.state.plan.supersededFrameBytes)]) != r.state.indexPrefixDigest || !bytes.Equal(planned, r.state.plan.reservedFrameBytes) || !validSuccessorAdmissionState(r, r.state, successorAdmissionAdjacentReady, candidate) || !successorPlannedGenerationExact(r.state, candidate) {
		return pre, fail(CodeEvidenceRecoveryRequired, "successor-generation-reserve", "adjacent reservation authority changed before append", nil)
	}
	if !r.state.consumed.CompareAndSwap(false, true) {
		return pre, fail(CodeEvidenceRecoveryRequired, "successor-generation-reserve", "adjacent reservation authority was already consumed", nil)
	}
	fsResult, transitionErr := r.state.mutation.AppendTargetIndex(ctx, r.state.inventory, planned)
	result := SuccessorReservationTransitionResult{
		outcome: fsResult.Outcome(), candidateDigest: fsResult.CandidateDigest(), candidateSequence: 7,
		candidateRevision: fsResult.CandidateRevision(), previousRevision: fsResult.PreviousRevision(), reservedRecordDigest: pre.reservedRecordDigest,
	}
	if fsResult.Outcome() != evidencefs.AdmissionTransitionDurable {
		if fsResult.Outcome() == evidencefs.AdmissionTransitionPreMutationFailure {
			r.state.consumed.CompareAndSwap(true, false)
		}
		return result, mapAdmissionMutationError(transitionErr, "successor-generation-reserve")
	}
	postFailure := func(suffix string) (SuccessorReservationTransitionResult, error) {
		_ = fsResult.Invalidate()
		return successorReservationUnknown(result), admissionPostMutationFailure("successor-generation-reserve" + suffix)
	}
	if transitionErr != nil || fsResult.CandidateKind() != "target_index_append" || fsResult.CandidateSequence() != 0 || fsResult.CandidateDigest() != pre.candidateDigest || fsResult.PreviousRevision() != pre.previousRevision || fsResult.CandidateRevision() != pre.candidateRevision || fsResult.Inventory() == nil {
		return postFailure("")
	}
	nextInventory := fsResult.Inventory()
	if err := nextInventory.Revalidate(ctx); err != nil {
		return postFailure("-revalidate")
	}
	revision, revisionErr := nextInventory.Revision()
	target, targetErr := nextInventory.Target()
	fullSet, fullSetErr := nextInventory.FullSetDigest()
	if revisionErr != nil || revision != pre.candidateRevision {
		return postFailure("-revision")
	}
	if targetErr != nil || target != r.state.target {
		return postFailure("-target")
	}
	if fullSetErr != nil || fullSet == ([32]byte{}) || fullSet == r.state.fullSet {
		return postFailure("-full-set")
	}
	verified, verifyErr := validateSuccessorIndexAppend(ctx, nextInventory, target, prefix.raw, planned, r.state.indexRecords+1, pre.reservedRecordDigest)
	if verifyErr != nil {
		return postFailure("-index")
	}
	if !successorReceiptsExact(r.state, candidate) {
		return postFailure("-receipts")
	}
	nextToken, tokenErr := nextInventory.MutationToken()
	if tokenErr != nil || !nextToken.ValidFor(nextInventory) {
		return postFailure("-token")
	}
	step := successorAdmissionStep{inventory: nextInventory, mutation: nextToken, target: target, fullSet: fullSet, revision: revision}
	nextState := nextSuccessorAdmissionState(r.state, successorAdmissionReservedDurable, step)
	nextState.fsIndex = fsResult
	nextState.indexPrefixDigest = prefix.digest
	nextState.indexDigest = verified.digest
	nextState.framedDigest = pre.candidateDigest
	nextState.indexPrefixSize = prefix.size
	nextState.indexSize = verified.size
	nextState.indexRecords = uint64(len(verified.frames))
	nextState.indexTail = pre.reservedRecordDigest
	nextState.supersededDigest = r.state.supersededDigest
	nextState.reservedDigest = pre.reservedRecordDigest
	next := &SuccessorReservedDurablePermit{state: nextState}
	next.self = next
	if !sealSuccessorAdmissionState(next, nextState) || !validSuccessorAdmissionState(next, nextState, successorAdmissionReservedDurable, candidate) {
		if nextState.binding != nil {
			successorAdmissionStateRegistry.Delete(nextState.binding)
		}
		return postFailure("-seal")
	}
	result.next = next
	return result, nil
}

type successorIndexSnapshot struct {
	raw    []byte
	frames []LineageIndexFrame
	digest [32]byte
	size   uint64
	tail   Digest
}

func readSuccessorCurrentIndex(ctx context.Context, state *successorAdmissionState, records uint64, tail Digest, expectedDigest [32]byte, expectedSize uint64) (successorIndexSnapshot, error) {
	if state == nil {
		return successorIndexSnapshot{}, fail(CodeEvidenceRecoveryRequired, "successor-index-prefix", "successor index prefix authority is unavailable", nil)
	}
	return readSuccessorInventoryIndex(ctx, state.inventory, state.target, records, tail, expectedDigest, expectedSize, "successor-index-prefix")
}

func readSuccessorInventoryIndex(ctx context.Context, inventory *evidencefs.AdmissionInventory, expectedTarget [32]byte, records uint64, tail Digest, expectedDigest [32]byte, expectedSize uint64, op string) (successorIndexSnapshot, error) {
	var zero successorIndexSnapshot
	if inventory == nil || expectedTarget == ([32]byte{}) || records == 0 || tail.Validate() != nil {
		return zero, fail(CodeEvidenceRecoveryRequired, op, "successor index prefix authority is unavailable", nil)
	}
	if err := inventory.Revalidate(ctx); err != nil {
		return zero, mapEvidenceAdmissionError(err, op+"-revalidate")
	}
	target, targetErr := inventory.Target()
	lineage, lineageErr := inventory.Lineage(expectedTarget)
	absent, absentErr := inventory.TargetAbsent()
	if targetErr != nil || lineageErr != nil || absentErr != nil {
		for _, accessorErr := range []error{targetErr, lineageErr, absentErr} {
			if accessorErr != nil {
				return zero, mapEvidenceAdmissionError(accessorErr, op)
			}
		}
	}
	if target != expectedTarget || absent != nil {
		return zero, admissionCorrupt(op, "successor target registration changed", nil)
	}
	lineageID, idErr := lineage.ID()
	index, indexErr := lineage.Index()
	if idErr != nil || indexErr != nil {
		for _, accessorErr := range []error{idErr, indexErr} {
			if accessorErr != nil {
				return zero, mapEvidenceAdmissionError(accessorErr, op)
			}
		}
	}
	if lineageID != expectedTarget {
		return zero, admissionCorrupt(op, "successor lineage identity changed", nil)
	}
	raw, readErr := index.ReadAll(ctx)
	digest, digestErr := index.Digest()
	size, sizeErr := index.Size()
	if readErr != nil || digestErr != nil || sizeErr != nil {
		for _, accessorErr := range []error{readErr, digestErr, sizeErr} {
			if accessorErr != nil {
				return zero, mapEvidenceAdmissionError(accessorErr, op)
			}
		}
	}
	frames, decodeErr := decodeAdmissionLineageFrames(raw)
	if decodeErr != nil {
		return zero, decodeErr
	}
	if _, structuralErr := scanLineageChainStructure(frames); structuralErr != nil {
		return zero, admissionCorrupt(op, "successor index prefix is structurally invalid", structuralErr)
	}
	actualDigest := sha256.Sum256(raw)
	if len(frames) == 0 || uint64(len(frames)) != records || frames[len(frames)-1].RecordDigest != tail || uint64(len(raw)) != size || actualDigest != digest || expectedDigest != ([32]byte{}) && digest != expectedDigest || expectedSize != 0 && size != expectedSize {
		return zero, admissionCorrupt(op, "successor index prefix differs from verified history", nil)
	}
	if err := inventory.Revalidate(ctx); err != nil {
		return zero, mapEvidenceAdmissionError(err, op+"-terminal-revalidate")
	}
	return successorIndexSnapshot{raw: append([]byte(nil), raw...), frames: frames, digest: digest, size: size, tail: tail}, nil
}

func validateSuccessorIndexAppend(ctx context.Context, inventory *evidencefs.AdmissionInventory, target [32]byte, prefix, framed []byte, records uint64, tail Digest) (successorIndexSnapshot, error) {
	var zero successorIndexSnapshot
	if inventory == nil || len(prefix) == 0 || len(framed) == 0 || records == 0 || tail.Validate() != nil {
		return zero, admissionPostMutationFailure("successor-index-append-input")
	}
	lineage, lineageErr := inventory.Lineage(target)
	absent, absentErr := inventory.TargetAbsent()
	if lineageErr != nil || absentErr != nil {
		for _, accessorErr := range []error{lineageErr, absentErr} {
			if accessorErr != nil {
				return zero, mapEvidenceAdmissionError(accessorErr, "successor-index-append")
			}
		}
	}
	if absent != nil {
		return zero, admissionCorrupt("successor-index-append", "successor target became absent", nil)
	}
	index, indexErr := lineage.Index()
	if indexErr != nil {
		return zero, mapEvidenceAdmissionError(indexErr, "successor-index-append")
	}
	raw, readErr := index.ReadAll(ctx)
	digest, digestErr := index.Digest()
	size, sizeErr := index.Size()
	if readErr != nil || digestErr != nil || sizeErr != nil {
		for _, accessorErr := range []error{readErr, digestErr, sizeErr} {
			if accessorErr != nil {
				return zero, mapEvidenceAdmissionError(accessorErr, "successor-index-append")
			}
		}
	}
	want := make([]byte, 0, len(prefix)+len(framed))
	want = append(want, prefix...)
	want = append(want, framed...)
	frames, decodeErr := decodeAdmissionLineageFrames(raw)
	if decodeErr != nil {
		return zero, decodeErr
	}
	if _, structuralErr := scanLineageChainStructure(frames); structuralErr != nil {
		return zero, admissionCorrupt("successor-index-append", "successor index append is structurally invalid", structuralErr)
	}
	wantDigest := sha256.Sum256(want)
	if !bytes.Equal(raw, want) || uint64(len(want)) != size || digest != wantDigest || uint64(len(frames)) != records || len(frames) == 0 || frames[len(frames)-1].RecordDigest != tail {
		return zero, admissionCorrupt("successor-index-append", "durable successor index differs from exact planned bytes", nil)
	}
	if err := inventory.Revalidate(ctx); err != nil {
		return zero, mapEvidenceAdmissionError(err, "successor-index-append-terminal-revalidate")
	}
	return successorIndexSnapshot{raw: append([]byte(nil), raw...), frames: frames, digest: digest, size: size, tail: tail}, nil
}

func validSuccessorInventoryIndex(state *successorAdmissionState) bool {
	if state == nil || state.inventory == nil || state.indexDigest == ([32]byte{}) || state.indexSize == 0 {
		return false
	}
	lineage, err := state.inventory.Lineage(state.target)
	if err != nil {
		return false
	}
	lineageID, idErr := lineage.ID()
	index, indexErr := lineage.Index()
	if idErr != nil || indexErr != nil || lineageID != state.target {
		return false
	}
	digest, digestErr := index.Digest()
	size, sizeErr := index.Size()
	absent, absentErr := state.inventory.TargetAbsent()
	return digestErr == nil && sizeErr == nil && absentErr == nil && absent == nil && digest == state.indexDigest && size == state.indexSize
}

func successorPlannedGenerationExact(state *successorAdmissionState, candidate OwnedCurrentCandidate) bool {
	if state == nil || state.plan == nil || !successorAdmissionPlanFramesExact(state.plan) || state.plan.supersededFrame.Record.Superseded == nil || state.plan.reservedFrame.Record.Reserved == nil || state.plan.reservedFrame.Record.Reserved.PlannedSegment0Header.Validate() != nil || state.plan.supersededFrame.Sequence != state.history.targetIndexRecords || state.plan.supersededFrame.PreviousRecordDigest == nil || *state.plan.supersededFrame.PreviousRecordDigest != state.history.targetIndexTail || state.plan.reservedFrame.Sequence != state.plan.supersededFrame.Sequence+1 || state.plan.reservedFrame.PreviousRecordDigest == nil || *state.plan.reservedFrame.PreviousRecordDigest != state.plan.supersededFrame.RecordDigest || !canonicalEqual(state.plan.supersededFrame.Record.Superseded.PlannedGenerationReserved, state.plan.reservedFrame.Record.Reserved) {
		return false
	}
	reserved := state.plan.reservedFrame.Record.Reserved
	generation := generationIdentity{
		owner: candidate.owner, executionLineageDigest: reserved.ExecutionLineageDigest,
		journalIdentityDigest: reserved.JournalIdentityDigest, runnerProjectionDecisionDigest: reserved.RunnerProjectionDecisionDigest,
		schemaBundleDigest: reserved.SchemaBundleDigest,
	}
	return reserved.Validate() == nil && sameGenerationHeader(generation, reserved.PlannedSegment0Header) && successorReceiptsExact(state, candidate)
}

func successorReceiptsExact(state *successorAdmissionState, candidate OwnedCurrentCandidate) bool {
	return state != nil && validRuntimeReceipt(state.runtimeReceipt, candidate.owner, state.runtimeDigest, state.runtimeSize) && validDecisionRecoveryReceipt(state.recoveryReceipt, candidate.owner, state.recoveryDigest, state.recoverySize) && state.runtimeReceipt.publication == state.runtimePublication && state.recoveryReceipt.publication == state.recoveryPublication && state.runtimePublication.SameStore(state.recoveryPublication)
}

func validConsumedSuccessorAdmissionState(owner any, state *successorAdmissionState, stage successorAdmissionStage, candidate OwnedCurrentCandidate) bool {
	if !validStoredSuccessorAdmissionState(state) || state.binding.owner != owner || state.stage != stage || !state.consumed.Load() || !validOwnedCurrentCandidate(candidate) || state.candidateBinding != candidate.binding || state.runtimeDigest != candidate.runtimeArtifact.digest || state.runtimeSize != candidate.runtimeArtifact.sizeBytes || state.recoveryDigest != candidate.decisionRecoveryArtifact.digest || state.recoverySize != candidate.decisionRecoveryArtifact.sizeBytes || !validConsumedVerifiedSuccessorAdmissionPlan(state.plan, candidate) {
		return false
	}
	return successorReceiptsExact(state, candidate)
}

func successorSupersessionUnknown(value SuccessorSupersessionTransitionResult) SuccessorSupersessionTransitionResult {
	value.outcome = evidencefs.AdmissionTransitionUnknown
	value.next = nil
	return value
}

func successorReservationUnknown(value SuccessorReservationTransitionResult) SuccessorReservationTransitionResult {
	value.outcome = evidencefs.AdmissionTransitionUnknown
	value.next = nil
	return value
}
