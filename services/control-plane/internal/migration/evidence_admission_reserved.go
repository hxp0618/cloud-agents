package migration

import (
	"bytes"
	"context"
	"crypto/sha256"
	"sync"
	"sync/atomic"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/evidencefs"
)

// GenerationReservationTransitionResult is the closed migration projection of
// the durable GenerationReserved index append. The filesystem candidate digest
// covers exact framed bytes; ReservedRecordDigest is the distinct C3 record
// digest referenced by the later activation record.
type GenerationReservationTransitionResult struct {
	outcome              evidencefs.AdmissionTransitionOutcome
	next                 *ReservedDurablePermit
	candidateDigest      [32]byte
	candidateSequence    uint64
	candidateRevision    uint64
	previousRevision     uint64
	reservedRecordDigest Digest
}

func (r GenerationReservationTransitionResult) Outcome() evidencefs.AdmissionTransitionOutcome {
	return r.outcome
}
func (r GenerationReservationTransitionResult) Next() *ReservedDurablePermit { return r.next }
func (r GenerationReservationTransitionResult) CandidateKind() string        { return "generation_reserved" }
func (r GenerationReservationTransitionResult) CandidateDigest() [32]byte    { return r.candidateDigest }
func (r GenerationReservationTransitionResult) CandidateSequence() uint64    { return r.candidateSequence }
func (r GenerationReservationTransitionResult) CandidateRevision() uint64    { return r.candidateRevision }
func (r GenerationReservationTransitionResult) PreviousRevision() uint64     { return r.previousRevision }
func (r GenerationReservationTransitionResult) ReservedRecordDigest() Digest {
	return r.reservedRecordDigest
}

// ReservedDurablePermit proves exact GenerationReserved bytes are durable in
// the target index. It is still insufficient to activate or connect: the next
// transition must create the planned segment-0 header under the retained root,
// lineage, and generation lock chain.
type ReservedDurablePermit struct {
	self             *ReservedDurablePermit
	prior            *ReceiptBoundReady
	plan             *VerifiedAdmissionPlan
	history          *VerifiedAdmissionHistory
	candidateBinding *verifiedEvidenceRunBinding
	inventory        *evidencefs.AdmissionInventory
	mutation         *evidencefs.AdmissionMutationToken
	runtimeReceipt   VerifiedContentReceipt
	recoveryReceipt  VerifiedDecisionRecoveryReceipt
	target, fullSet  [32]byte
	revision         uint64
	indexDigest      [32]byte
	framedDigest     [32]byte
	reservedDigest   Digest
	journal          Digest
	headerDigest     Digest
	binding          *reservedDurablePermitBinding
	consumed         *atomic.Bool
}

type reservedDurablePermitBinding struct {
	permit          *ReservedDurablePermit
	prior           *ReceiptBoundReady
	plan            *VerifiedAdmissionPlan
	history         *VerifiedAdmissionHistory
	inventory       *evidencefs.AdmissionInventory
	mutation        *evidencefs.AdmissionMutationToken
	runtimeBinding  *verifiedContentReceiptBinding
	recoveryBinding *verifiedDecisionRecoveryReceiptBinding
	canonical       [32]byte
}

var reservedDurablePermitRegistry sync.Map

// AppendGenerationReserved consumes the receipt-pair authority and appends the
// exact preplanned GenerationReserved frame at the current target index EOF.
func (r *ReceiptBoundReady) AppendGenerationReserved(ctx context.Context, candidate OwnedCurrentCandidate) (GenerationReservationTransitionResult, error) {
	pre := GenerationReservationTransitionResult{outcome: evidencefs.AdmissionTransitionPreMutationFailure, candidateSequence: 6}
	if r == nil || r.inventory == nil || !validReceiptBoundReady(r, r.inventory, candidate) || r.plan.reservedFrame.Record.Reserved == nil {
		return pre, fail(CodeEvidenceRecoveryRequired, "admission-generation-reserve", "receipt-bound reservation authority is unavailable", nil)
	}
	if err := contextAdmissionError(ctx); err != nil {
		return pre, err
	}
	reserved := cloneProjectionValue(*r.plan.reservedFrame.Record.Reserved)
	plannedBytes := append([]byte(nil), r.plan.reservedFrameBytes...)
	pre.previousRevision = r.revision
	pre.candidateRevision = r.revision + 1
	pre.candidateDigest = sha256.Sum256(plannedBytes)
	pre.reservedRecordDigest = r.plan.reservedFrame.RecordDigest
	if pre.candidateDigest == ([32]byte{}) || requireDigest("admission-generation-reserve.record", pre.reservedRecordDigest) != nil || !brandNewReservationPlanExact(r.plan) {
		return pre, fail(CodeEvidenceRecoveryRequired, "admission-generation-reserve", "planned reservation bytes are invalid", nil)
	}
	generation := generationIdentity{
		owner: candidate.owner, executionLineageDigest: reserved.ExecutionLineageDigest,
		journalIdentityDigest: reserved.JournalIdentityDigest, runnerProjectionDecisionDigest: reserved.RunnerProjectionDecisionDigest,
		schemaBundleDigest: reserved.SchemaBundleDigest,
	}
	if err := reserved.Validate(); err != nil || !sameGenerationHeader(generation, reserved.PlannedSegment0Header) || !validRuntimeReceipt(r.runtimeReceipt, candidate.owner, reserved.PlannedSegment0Header.OuterArtifactDigest, reserved.PlannedSegment0Header.OuterArtifactSizeBytes) || !validDecisionRecoveryReceipt(r.recoveryReceipt, candidate.owner, reserved.PlannedSegment0Header.DecisionRecoveryArtifactSHA256, reserved.PlannedSegment0Header.DecisionRecoveryArtifactSizeBytes) || !r.runtimeReceipt.publication.SameStore(r.recoveryReceipt.publication) {
		return pre, fail(CodeEvidenceRecoveryRequired, "admission-generation-reserve", "planned activation header cannot be bound to typed receipts", nil)
	}
	if err := validateRegisteredEmptyReservationInput(ctx, r.inventory, r.target, r.plan); err != nil {
		return pre, err
	}
	if !validReceiptBoundReady(r, r.inventory, candidate) || !brandNewReservationPlanExact(r.plan) || !bytes.Equal(plannedBytes, r.plan.reservedFrameBytes) || r.plan.reservedFrame.Record.Reserved == nil || !canonicalEqual(reserved, *r.plan.reservedFrame.Record.Reserved) {
		return pre, fail(CodeEvidenceRecoveryRequired, "admission-generation-reserve", "receipt-bound reservation authority changed before append", nil)
	}
	if !r.consumed.CompareAndSwap(false, true) {
		return pre, fail(CodeEvidenceRecoveryRequired, "admission-generation-reserve", "receipt-bound reservation authority was already consumed", nil)
	}
	fsResult, transitionErr := r.mutation.AppendTargetIndex(ctx, r.inventory, plannedBytes)
	result := GenerationReservationTransitionResult{
		outcome: fsResult.Outcome(), candidateDigest: fsResult.CandidateDigest(), candidateSequence: 6,
		candidateRevision: fsResult.CandidateRevision(), previousRevision: fsResult.PreviousRevision(), reservedRecordDigest: pre.reservedRecordDigest,
	}
	if fsResult.Outcome() != evidencefs.AdmissionTransitionDurable {
		if fsResult.Outcome() == evidencefs.AdmissionTransitionPreMutationFailure {
			r.consumed.CompareAndSwap(true, false)
		}
		return result, mapAdmissionMutationError(transitionErr, "admission-generation-reserve")
	}
	if transitionErr != nil || fsResult.CandidateKind() != "target_index_append" || fsResult.CandidateSequence() != 0 || fsResult.CandidateDigest() != pre.candidateDigest || fsResult.PreviousRevision() != pre.previousRevision || fsResult.CandidateRevision() != pre.candidateRevision || fsResult.Inventory() == nil {
		_ = fsResult.Invalidate()
		return generationReservationUnknown(result), admissionPostMutationFailure("admission-generation-reserve")
	}
	nextInventory := fsResult.Inventory()
	if err := nextInventory.Revalidate(ctx); err != nil {
		_ = fsResult.Invalidate()
		return generationReservationUnknown(result), admissionPostMutationFailure("admission-generation-reserve-revalidate")
	}
	revision, err := nextInventory.Revision()
	if err != nil || revision != pre.candidateRevision {
		_ = fsResult.Invalidate()
		return generationReservationUnknown(result), admissionPostMutationFailure("admission-generation-reserve-revision")
	}
	target, err := nextInventory.Target()
	if err != nil || target != r.target {
		_ = fsResult.Invalidate()
		return generationReservationUnknown(result), admissionPostMutationFailure("admission-generation-reserve-target")
	}
	fullSet, err := nextInventory.FullSetDigest()
	if err != nil || fullSet == ([32]byte{}) || fullSet == r.fullSet {
		_ = fsResult.Invalidate()
		return generationReservationUnknown(result), admissionPostMutationFailure("admission-generation-reserve-full-set")
	}
	indexDigest, err := validateReservedInventory(ctx, nextInventory, target, r.plan.lineageHeaderBytes, plannedBytes)
	if err != nil {
		_ = fsResult.Invalidate()
		return generationReservationUnknown(result), admissionPostMutationFailure("admission-generation-reserve-index")
	}
	if !validRuntimeReceipt(r.runtimeReceipt, candidate.owner, reserved.PlannedSegment0Header.OuterArtifactDigest, reserved.PlannedSegment0Header.OuterArtifactSizeBytes) || !validDecisionRecoveryReceipt(r.recoveryReceipt, candidate.owner, reserved.PlannedSegment0Header.DecisionRecoveryArtifactSHA256, reserved.PlannedSegment0Header.DecisionRecoveryArtifactSizeBytes) || !r.runtimeReceipt.publication.SameStore(r.recoveryReceipt.publication) {
		_ = fsResult.Invalidate()
		return generationReservationUnknown(result), admissionPostMutationFailure("admission-generation-reserve-receipts")
	}
	nextToken, err := nextInventory.MutationToken()
	if err != nil || !nextToken.ValidFor(nextInventory) {
		_ = fsResult.Invalidate()
		return generationReservationUnknown(result), admissionPostMutationFailure("admission-generation-reserve-token")
	}
	next := &ReservedDurablePermit{
		prior: r, plan: r.plan, history: r.history, candidateBinding: candidate.binding,
		inventory: nextInventory, mutation: nextToken, runtimeReceipt: r.runtimeReceipt, recoveryReceipt: r.recoveryReceipt,
		target: target, fullSet: fullSet, revision: revision,
		indexDigest: indexDigest, framedDigest: pre.candidateDigest, reservedDigest: pre.reservedRecordDigest,
		journal: reserved.JournalIdentityDigest, headerDigest: reserved.ExpectedSegment0HeaderDigest, consumed: &atomic.Bool{},
	}
	next.self = next
	binding := &reservedDurablePermitBinding{
		permit: next, prior: r, plan: r.plan, history: r.history, inventory: nextInventory, mutation: nextToken,
		runtimeBinding: r.runtimeReceipt.binding, recoveryBinding: r.recoveryReceipt.binding,
	}
	next.binding = binding
	binding.canonical = reservedDurablePermitDigest(next)
	reservedDurablePermitRegistry.Store(binding, binding.canonical)
	if !validReservedDurablePermit(next, nextInventory, candidate) {
		reservedDurablePermitRegistry.Delete(binding)
		_ = fsResult.Invalidate()
		return generationReservationUnknown(result), admissionPostMutationFailure("admission-generation-reserve-seal")
	}
	result.next = next
	return result, nil
}

func generationReservationUnknown(value GenerationReservationTransitionResult) GenerationReservationTransitionResult {
	value.outcome = evidencefs.AdmissionTransitionUnknown
	value.next = nil
	return value
}

func validateRegisteredEmptyReservationInput(ctx context.Context, inventory *evidencefs.AdmissionInventory, target [32]byte, plan *VerifiedAdmissionPlan) error {
	if inventory == nil || !brandNewReservationPlanExact(plan) {
		return fail(CodeEvidenceRecoveryRequired, "admission-generation-reserve", "registered-empty reservation input is unavailable", nil)
	}
	if err := inventory.Revalidate(ctx); err != nil {
		return mapEvidenceAdmissionError(err, "admission-generation-reserve-revalidate")
	}
	lineage, err := inventory.Lineage(target)
	if err != nil {
		return mapEvidenceAdmissionError(err, "admission-generation-reserve-lineage")
	}
	journals, err := lineage.Journals()
	if err != nil {
		return mapEvidenceAdmissionError(err, "admission-generation-reserve-journals")
	}
	registrations, err := lineage.GenerationRegistrations()
	if err != nil {
		return mapEvidenceAdmissionError(err, "admission-generation-reserve-registrations")
	}
	index, err := lineage.Index()
	if err != nil {
		return mapEvidenceAdmissionError(err, "admission-generation-reserve-index")
	}
	indexBytes, err := index.ReadAll(ctx)
	if err != nil {
		return mapEvidenceAdmissionError(err, "admission-generation-reserve-index-read")
	}
	absent, err := inventory.TargetAbsent()
	if err != nil {
		return mapEvidenceAdmissionError(err, "admission-generation-reserve-absence")
	}
	inventoryTarget, targetErr := inventory.Target()
	lineageID, lineageIDErr := lineage.ID()
	if targetErr != nil || lineageIDErr != nil {
		for _, accessorErr := range []error{targetErr, lineageIDErr} {
			if accessorErr != nil {
				return mapEvidenceAdmissionError(accessorErr, "admission-generation-reserve-target")
			}
		}
	}
	if inventoryTarget != target || lineageID != target || target != digestRaw(plan.lineageHeaderFrame.Record.Header.ExecutionLineageDigest) || absent != nil || len(journals) != 0 || len(registrations) != 0 || !bytes.Equal(indexBytes, plan.lineageHeaderBytes) || sha256.Sum256(indexBytes) != sha256.Sum256(plan.lineageHeaderBytes) {
		return admissionCorrupt("admission-generation-reserve", "target is not exact registered-empty state", nil)
	}
	return nil
}

func validateReservedInventory(ctx context.Context, inventory *evidencefs.AdmissionInventory, target [32]byte, lineageHeaderBytes, reservedFrameBytes []byte) ([32]byte, error) {
	var zero [32]byte
	lineage, err := inventory.Lineage(target)
	if err != nil {
		return zero, mapEvidenceAdmissionError(err, "admission-generation-reserve-lineage")
	}
	journals, err := lineage.Journals()
	if err != nil {
		return zero, mapEvidenceAdmissionError(err, "admission-generation-reserve-journals")
	}
	registrations, err := lineage.GenerationRegistrations()
	if err != nil {
		return zero, mapEvidenceAdmissionError(err, "admission-generation-reserve-registrations")
	}
	index, err := lineage.Index()
	if err != nil {
		return zero, mapEvidenceAdmissionError(err, "admission-generation-reserve-index")
	}
	raw, err := index.ReadAll(ctx)
	if err != nil {
		return zero, mapEvidenceAdmissionError(err, "admission-generation-reserve-index-read")
	}
	want := make([]byte, 0, len(lineageHeaderBytes)+len(reservedFrameBytes))
	want = append(want, lineageHeaderBytes...)
	want = append(want, reservedFrameBytes...)
	digest, digestErr := index.Digest()
	size, sizeErr := index.Size()
	absent, absentErr := inventory.TargetAbsent()
	if digestErr != nil || sizeErr != nil || absentErr != nil {
		for _, accessorErr := range []error{digestErr, sizeErr, absentErr} {
			if accessorErr != nil {
				return zero, mapEvidenceAdmissionError(accessorErr, "admission-generation-reserve-index")
			}
		}
	}
	wantDigest := sha256.Sum256(want)
	if absent != nil || len(journals) != 0 || len(registrations) != 0 || !bytes.Equal(raw, want) || uint64(len(want)) != size || digest != wantDigest {
		return zero, admissionCorrupt("admission-generation-reserve", "durable reservation index differs from the exact planned prefix", nil)
	}
	return digest, nil
}

func reservedDurablePermitDigest(permit *ReservedDurablePermit) [32]byte {
	if permit == nil || permit.self != permit || permit.prior == nil || permit.plan == nil || permit.history == nil || permit.candidateBinding == nil || permit.prior.binding == nil || permit.plan.binding == nil || permit.history.binding == nil || permit.runtimeReceipt.binding == nil || permit.recoveryReceipt.binding == nil {
		return [32]byte{}
	}
	h := sha256.New()
	h.Write([]byte("cloud-agents-platform-reserved-durable-permit/v1\x00"))
	h.Write(permit.prior.binding.canonical[:])
	h.Write(permit.plan.binding.canonical[:])
	h.Write(permit.history.binding.canonical[:])
	h.Write(permit.candidateBinding.canonical[:])
	h.Write(permit.target[:])
	h.Write(permit.fullSet[:])
	h.Write(permit.indexDigest[:])
	h.Write(permit.framedDigest[:])
	writeAdmissionUint(h, permit.revision)
	for _, value := range []Digest{permit.reservedDigest, permit.journal, permit.headerDigest, permit.runtimeReceipt.digest, permit.recoveryReceipt.digest} {
		writeAdmissionString(h, value.String())
	}
	writeAdmissionUint(h, permit.runtimeReceipt.sizeBytes)
	writeAdmissionUint(h, permit.recoveryReceipt.sizeBytes)
	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result
}

func validReservedDurablePermit(permit *ReservedDurablePermit, inventory *evidencefs.AdmissionInventory, candidate OwnedCurrentCandidate) bool {
	if permit == nil || permit.self != permit || permit.binding == nil || permit.binding.permit != permit || permit.prior == nil || permit.plan == nil || permit.history == nil || permit.inventory != inventory || permit.mutation == nil || permit.candidateBinding != candidate.binding || permit.binding.prior != permit.prior || permit.binding.plan != permit.plan || permit.binding.history != permit.history || permit.binding.inventory != inventory || permit.binding.mutation != permit.mutation || permit.binding.runtimeBinding != permit.runtimeReceipt.binding || permit.binding.recoveryBinding != permit.recoveryReceipt.binding || permit.runtimeReceipt.binding != permit.prior.runtimeReceipt.binding || permit.recoveryReceipt.binding != permit.prior.recoveryReceipt.binding || permit.consumed == nil || permit.consumed.Load() || !validConsumedReceiptBoundReady(permit.prior, permit.plan, candidate) || !brandNewReservationPlanExact(permit.plan) || permit.history != permit.plan.history || permit.revision != permit.prior.revision+1 || permit.target != permit.prior.target || permit.target != digestRaw(permit.plan.lineageHeaderFrame.Record.Header.ExecutionLineageDigest) || permit.fullSet == permit.prior.fullSet || permit.indexDigest == ([32]byte{}) || permit.indexDigest != reservedPlanIndexDigest(permit.plan) || permit.framedDigest != sha256.Sum256(permit.plan.reservedFrameBytes) || permit.plan.reservedFrame.Record.Reserved == nil || permit.reservedDigest != permit.plan.reservedFrame.RecordDigest || permit.journal != permit.plan.reservedFrame.Record.Reserved.JournalIdentityDigest || permit.headerDigest != permit.plan.reservedFrame.Record.Reserved.ExpectedSegment0HeaderDigest || !validRuntimeReceipt(permit.runtimeReceipt, candidate.owner, permit.plan.reservedFrame.Record.Reserved.PlannedSegment0Header.OuterArtifactDigest, permit.plan.reservedFrame.Record.Reserved.PlannedSegment0Header.OuterArtifactSizeBytes) || !validDecisionRecoveryReceipt(permit.recoveryReceipt, candidate.owner, permit.plan.reservedFrame.Record.Reserved.PlannedSegment0Header.DecisionRecoveryArtifactSHA256, permit.plan.reservedFrame.Record.Reserved.PlannedSegment0Header.DecisionRecoveryArtifactSizeBytes) || !permit.runtimeReceipt.publication.SameStore(permit.recoveryReceipt.publication) || !permit.mutation.ValidFor(inventory) || permit.binding.canonical == ([32]byte{}) || permit.binding.canonical != reservedDurablePermitDigest(permit) {
		return false
	}
	reserved := permit.plan.reservedFrame.Record.Reserved
	generation := generationIdentity{candidate.owner, reserved.ExecutionLineageDigest, reserved.JournalIdentityDigest, reserved.RunnerProjectionDecisionDigest, reserved.SchemaBundleDigest}
	if reserved.Validate() != nil || !sameGenerationHeader(generation, reserved.PlannedSegment0Header) {
		return false
	}
	registered, ok := reservedDurablePermitRegistry.Load(permit.binding)
	if !ok || registered != permit.binding.canonical {
		return false
	}
	revision, err := inventory.Revision()
	if err != nil || revision != permit.revision {
		return false
	}
	fullSet, err := inventory.FullSetDigest()
	if err != nil || fullSet != permit.fullSet {
		return false
	}
	lineage, err := inventory.Lineage(permit.target)
	if err != nil {
		return false
	}
	index, err := lineage.Index()
	if err != nil {
		return false
	}
	digest, err := index.Digest()
	return err == nil && digest == permit.indexDigest
}

func reservedPlanIndexDigest(plan *VerifiedAdmissionPlan) [32]byte {
	if plan == nil || len(plan.lineageHeaderBytes) == 0 || len(plan.reservedFrameBytes) == 0 {
		return [32]byte{}
	}
	h := sha256.New()
	h.Write(plan.lineageHeaderBytes)
	h.Write(plan.reservedFrameBytes)
	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result
}

func brandNewReservationPlanExact(plan *VerifiedAdmissionPlan) bool {
	if plan == nil || plan.lineageHeaderFrame.Record.Header == nil || plan.reservedFrame.Record.Reserved == nil || !admissionPlanFramesExact(plan) {
		return false
	}
	header, reserved := plan.lineageHeaderFrame, plan.reservedFrame
	return header.RecordKind == LineageRecordHeader && header.Sequence == 0 && header.PreviousRecordDigest == nil &&
		reserved.RecordKind == LineageRecordGenerationReserved && reserved.Sequence == 1 && reserved.PreviousRecordDigest != nil && *reserved.PreviousRecordDigest == header.RecordDigest &&
		reserved.Record.Reserved.ExecutionLineageDigest == header.Record.Header.ExecutionLineageDigest
}

func validConsumedReceiptBoundReady(ready *ReceiptBoundReady, plan *VerifiedAdmissionPlan, candidate OwnedCurrentCandidate) bool {
	if ready == nil || ready.self != ready || ready.binding == nil || ready.binding.ready != ready || ready.prior == nil || ready.plan != plan || plan == nil || ready.history == nil || ready.inventory == nil || ready.mutation == nil || ready.candidateBinding != candidate.binding || ready.binding.prior != ready.prior || ready.binding.plan != plan || ready.binding.history != ready.history || ready.binding.inventory != ready.inventory || ready.binding.mutation != ready.mutation || ready.binding.runtimeBinding != ready.runtimeReceipt.binding || ready.binding.recoveryBinding != ready.recoveryReceipt.binding || ready.consumed == nil || !ready.consumed.Load() || !validConsumedReserveReady(ready.prior, plan, candidate) || !brandNewReservationPlanExact(plan) || ready.history != plan.history || ready.revision != ready.prior.revision || ready.target != ready.prior.target || ready.fullSet != ready.prior.fullSet || !validRuntimeReceipt(ready.runtimeReceipt, candidate.owner, candidate.runtimeArtifact.digest, candidate.runtimeArtifact.sizeBytes) || !validDecisionRecoveryReceipt(ready.recoveryReceipt, candidate.owner, candidate.decisionRecoveryArtifact.digest, candidate.decisionRecoveryArtifact.sizeBytes) || !ready.runtimeReceipt.publication.SameStore(ready.recoveryReceipt.publication) || ready.binding.canonical == ([32]byte{}) || ready.binding.canonical != receiptBoundReadyDigest(ready) {
		return false
	}
	registered, ok := receiptBoundReadyRegistry.Load(ready.binding)
	return ok && registered == ready.binding.canonical
}
