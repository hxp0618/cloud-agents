package migration

import (
	"bytes"
	"context"
	"crypto/sha256"
	"sync"
	"sync/atomic"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/evidencefs"
)

// GenerationHeaderTransitionResult is the closed migration projection of the
// deterministic generation-directory and segment-0 header transition.
// CandidateDigest is evidencefs-owned mutation diagnosis; HeaderBytesDigest
// and HeaderRecordDigest bind the distinct physical and C3 identities.
type GenerationHeaderTransitionResult struct {
	outcome            evidencefs.AdmissionTransitionOutcome
	next               *HeaderDurablePermit
	candidateDigest    [32]byte
	candidateSequence  uint64
	candidateRevision  uint64
	previousRevision   uint64
	journal            Digest
	headerRecordDigest Digest
	headerBytesDigest  [32]byte
	headerSize         uint64
}

func (r GenerationHeaderTransitionResult) Outcome() evidencefs.AdmissionTransitionOutcome {
	return r.outcome
}
func (r GenerationHeaderTransitionResult) Next() *HeaderDurablePermit  { return r.next }
func (r GenerationHeaderTransitionResult) CandidateKind() string       { return "generation_header" }
func (r GenerationHeaderTransitionResult) CandidateDigest() [32]byte   { return r.candidateDigest }
func (r GenerationHeaderTransitionResult) CandidateSequence() uint64   { return r.candidateSequence }
func (r GenerationHeaderTransitionResult) CandidateRevision() uint64   { return r.candidateRevision }
func (r GenerationHeaderTransitionResult) PreviousRevision() uint64    { return r.previousRevision }
func (r GenerationHeaderTransitionResult) Journal() Digest             { return r.journal }
func (r GenerationHeaderTransitionResult) HeaderRecordDigest() Digest  { return r.headerRecordDigest }
func (r GenerationHeaderTransitionResult) HeaderBytesDigest() [32]byte { return r.headerBytesDigest }
func (r GenerationHeaderTransitionResult) HeaderSize() uint64          { return r.headerSize }

// HeaderDurablePermit proves GenerationReserved remains the exact index tail
// while the planned segment-0 header is durable under a retained generation
// lock. It cannot connect or mint a runtime cursor; GenerationActivated must be
// durably appended first.
type HeaderDurablePermit struct {
	self             *HeaderDurablePermit
	prior            *ReservedDurablePermit
	plan             *VerifiedAdmissionPlan
	history          *VerifiedAdmissionHistory
	candidateBinding *verifiedEvidenceRunBinding
	inventory        *evidencefs.AdmissionInventory
	mutation         *evidencefs.AdmissionMutationToken
	runtimeReceipt   VerifiedContentReceipt
	recoveryReceipt  VerifiedDecisionRecoveryReceipt
	activationHeader ownedActivationHeader
	headerFrame      EvidenceFrame
	headerBytes      []byte
	fsResult         evidencefs.AdmissionJournalTransitionResult
	target, fullSet  [32]byte
	revision         uint64
	indexDigest      [32]byte
	fsCandidate      [32]byte
	headerBytesHash  [32]byte
	reservedDigest   Digest
	journal          Digest
	headerDigest     Digest
	binding          *headerDurablePermitBinding
	consumed         *atomic.Bool
}

type headerDurablePermitBinding struct {
	permit          *HeaderDurablePermit
	prior           *ReservedDurablePermit
	plan            *VerifiedAdmissionPlan
	history         *VerifiedAdmissionHistory
	inventory       *evidencefs.AdmissionInventory
	mutation        *evidencefs.AdmissionMutationToken
	runtimeBinding  *verifiedContentReceiptBinding
	recoveryBinding *verifiedDecisionRecoveryReceiptBinding
	canonical       [32]byte
}

var headerDurablePermitRegistry sync.Map

// CreateGenerationHeader consumes durable reservation authority, binds its
// planned header to both typed receipts, and writes exact canonical segment-0
// bytes through evidencefs. It does not append GenerationActivated.
func (p *ReservedDurablePermit) CreateGenerationHeader(ctx context.Context, candidate OwnedCurrentCandidate) (GenerationHeaderTransitionResult, error) {
	pre := GenerationHeaderTransitionResult{outcome: evidencefs.AdmissionTransitionPreMutationFailure, candidateSequence: 7}
	if p == nil || p.inventory == nil || !validReservedDurablePermit(p, p.inventory, candidate) || p.plan.reservedFrame.Record.Reserved == nil {
		return pre, fail(CodeEvidenceRecoveryRequired, "admission-generation-header", "durable reservation authority is unavailable", nil)
	}
	if err := contextAdmissionError(ctx); err != nil {
		return pre, err
	}
	reserved := cloneProjectionValue(*p.plan.reservedFrame.Record.Reserved)
	generation := generationIdentity{candidate.owner, reserved.ExecutionLineageDigest, reserved.JournalIdentityDigest, reserved.RunnerProjectionDecisionDigest, reserved.SchemaBundleDigest}
	activationHeader, err := bindActivationHeader(generation, reserved, p.runtimeReceipt, p.recoveryReceipt)
	if err != nil {
		return pre, fail(CodeEvidenceRecoveryRequired, "admission-generation-header", "activation header cannot be bound to reservation receipts", nil)
	}
	headerFrame, headerBytes, err := encodeAdmissionActivationHeader(activationHeader)
	if err != nil || headerFrame.RecordDigest != p.headerDigest || headerFrame.RecordDigest != reserved.ExpectedSegment0HeaderDigest {
		return pre, fail(CodeEvidenceRecoveryRequired, "admission-generation-header", "activation header does not match the durable reservation", nil)
	}
	headerBytes = append([]byte(nil), headerBytes...)
	pre.previousRevision = p.revision
	pre.candidateRevision = p.revision + 1
	pre.journal = p.journal
	pre.headerRecordDigest = headerFrame.RecordDigest
	pre.headerBytesDigest = sha256.Sum256(headerBytes)
	pre.headerSize = uint64(len(headerBytes))
	if pre.headerBytesDigest == ([32]byte{}) || pre.headerSize == 0 || requireDigest("admission-generation-header.journal", pre.journal) != nil {
		return pre, fail(CodeEvidenceRecoveryRequired, "admission-generation-header", "activation header identity is invalid", nil)
	}
	if err := p.inventory.Revalidate(ctx); err != nil {
		return pre, mapEvidenceAdmissionError(err, "admission-generation-header-revalidate")
	}
	indexDigest, err := validateReservedInventory(ctx, p.inventory, p.target, p.plan.lineageHeaderBytes, p.plan.reservedFrameBytes)
	if err != nil {
		return pre, err
	}
	if indexDigest != p.indexDigest || !validReservedDurablePermit(p, p.inventory, candidate) || !canonicalEqual(reserved, *p.plan.reservedFrame.Record.Reserved) {
		return pre, fail(CodeEvidenceRecoveryRequired, "admission-generation-header", "durable reservation changed before header creation", nil)
	}
	if !p.consumed.CompareAndSwap(false, true) {
		return pre, fail(CodeEvidenceRecoveryRequired, "admission-generation-header", "durable reservation authority was already consumed", nil)
	}
	fsResult, transitionErr := p.mutation.CreateGenerationHeader(ctx, p.inventory, digestRaw(p.journal), headerBytes)
	result := GenerationHeaderTransitionResult{
		outcome: fsResult.Outcome(), candidateDigest: fsResult.CandidateDigest(), candidateSequence: 7,
		candidateRevision: fsResult.CandidateRevision(), previousRevision: fsResult.PreviousRevision(), journal: p.journal,
		headerRecordDigest: headerFrame.RecordDigest, headerBytesDigest: pre.headerBytesDigest, headerSize: pre.headerSize,
	}
	if fsResult.Outcome() != evidencefs.AdmissionTransitionDurable {
		if fsResult.Outcome() == evidencefs.AdmissionTransitionPreMutationFailure {
			p.consumed.CompareAndSwap(true, false)
		}
		return result, mapAdmissionMutationError(transitionErr, "admission-generation-header")
	}
	if transitionErr != nil || fsResult.CandidateKind() != "generation_header" || fsResult.CandidateSequence() != 0 || fsResult.CandidateDigest() == ([32]byte{}) || fsResult.PreviousRevision() != pre.previousRevision || fsResult.CandidateRevision() != pre.candidateRevision || fsResult.Journal() != digestRaw(p.journal) || fsResult.HeaderDigest() != pre.headerBytesDigest || fsResult.HeaderSize() != pre.headerSize || fsResult.Inventory() == nil || !fsResult.ValidFor(fsResult.Inventory()) {
		_ = fsResult.Invalidate()
		return generationHeaderUnknown(result), admissionPostMutationFailure("admission-generation-header")
	}
	nextInventory := fsResult.Inventory()
	if err := nextInventory.Revalidate(ctx); err != nil {
		_ = fsResult.Invalidate()
		return generationHeaderUnknown(result), admissionPostMutationFailure("admission-generation-header-revalidate")
	}
	revision, err := nextInventory.Revision()
	if err != nil || revision != pre.candidateRevision {
		_ = fsResult.Invalidate()
		return generationHeaderUnknown(result), admissionPostMutationFailure("admission-generation-header-revision")
	}
	target, err := nextInventory.Target()
	if err != nil || target != p.target {
		_ = fsResult.Invalidate()
		return generationHeaderUnknown(result), admissionPostMutationFailure("admission-generation-header-target")
	}
	fullSet, err := nextInventory.FullSetDigest()
	if err != nil || fullSet == ([32]byte{}) || fullSet == p.fullSet {
		_ = fsResult.Invalidate()
		return generationHeaderUnknown(result), admissionPostMutationFailure("admission-generation-header-full-set")
	}
	nextIndexDigest, err := validateHeaderInventory(ctx, nextInventory, target, p.plan, digestRaw(p.journal), headerBytes)
	if err != nil || nextIndexDigest != p.indexDigest {
		_ = fsResult.Invalidate()
		return generationHeaderUnknown(result), admissionPostMutationFailure("admission-generation-header-inventory")
	}
	if !validRuntimeReceipt(p.runtimeReceipt, candidate.owner, activationHeader.header.OuterArtifactDigest, activationHeader.header.OuterArtifactSizeBytes) || !validDecisionRecoveryReceipt(p.recoveryReceipt, candidate.owner, activationHeader.header.DecisionRecoveryArtifactSHA256, activationHeader.header.DecisionRecoveryArtifactSizeBytes) || !p.runtimeReceipt.publication.SameStore(p.recoveryReceipt.publication) {
		_ = fsResult.Invalidate()
		return generationHeaderUnknown(result), admissionPostMutationFailure("admission-generation-header-receipts")
	}
	if err := nextInventory.Revalidate(ctx); err != nil {
		_ = fsResult.Invalidate()
		return generationHeaderUnknown(result), admissionPostMutationFailure("admission-generation-header-terminal-revalidate")
	}
	nextToken, err := nextInventory.MutationToken()
	if err != nil || !nextToken.ValidFor(nextInventory) {
		_ = fsResult.Invalidate()
		return generationHeaderUnknown(result), admissionPostMutationFailure("admission-generation-header-token")
	}
	next := &HeaderDurablePermit{
		prior: p, plan: p.plan, history: p.history, candidateBinding: candidate.binding,
		inventory: nextInventory, mutation: nextToken, runtimeReceipt: p.runtimeReceipt, recoveryReceipt: p.recoveryReceipt,
		activationHeader: activationHeader, headerFrame: headerFrame, headerBytes: headerBytes, fsResult: fsResult,
		target: target, fullSet: fullSet, revision: revision, indexDigest: nextIndexDigest,
		fsCandidate: fsResult.CandidateDigest(), headerBytesHash: pre.headerBytesDigest, reservedDigest: p.reservedDigest,
		journal: p.journal, headerDigest: headerFrame.RecordDigest, consumed: &atomic.Bool{},
	}
	next.self = next
	binding := &headerDurablePermitBinding{
		permit: next, prior: p, plan: p.plan, history: p.history, inventory: nextInventory, mutation: nextToken,
		runtimeBinding: p.runtimeReceipt.binding, recoveryBinding: p.recoveryReceipt.binding,
	}
	next.binding = binding
	binding.canonical = headerDurablePermitDigest(next)
	headerDurablePermitRegistry.Store(binding, binding.canonical)
	if !validHeaderDurablePermit(next, nextInventory, candidate) {
		headerDurablePermitRegistry.Delete(binding)
		_ = fsResult.Invalidate()
		return generationHeaderUnknown(result), admissionPostMutationFailure("admission-generation-header-seal")
	}
	result.next = next
	return result, nil
}

func generationHeaderUnknown(value GenerationHeaderTransitionResult) GenerationHeaderTransitionResult {
	value.outcome = evidencefs.AdmissionTransitionUnknown
	value.next = nil
	return value
}

func encodeAdmissionActivationHeader(value ownedActivationHeader) (EvidenceFrame, []byte, error) {
	if value.generation.owner == nil || value.reserved.Validate() != nil || !sameGenerationHeader(value.generation, value.header) || !canonicalEqual(value.header, value.reserved.PlannedSegment0Header) {
		return EvidenceFrame{}, nil, invalidEvidence("admission-activation-header", "owned header is invalid")
	}
	header := cloneProjectionValue(value.header)
	frame := EvidenceFrame{FormatVersion: EvidenceFrameFormat, Sequence: 0, RecordKind: EvidenceRecordHeader, Record: EvidenceRecord{Header: &header}}
	var err error
	frame.RecordDigest, err = frame.ComputeDigest()
	if err != nil || frame.RecordDigest != value.reserved.ExpectedSegment0HeaderDigest || frame.Validate() != nil {
		return EvidenceFrame{}, nil, invalidEvidence("admission-activation-header", "header digest is invalid")
	}
	raw, err := EncodeCanonicalEvidenceFrame(frame)
	if err != nil {
		return EvidenceFrame{}, nil, err
	}
	return cloneProjectionValue(frame), append([]byte(nil), raw...), nil
}

func validateHeaderInventory(ctx context.Context, inventory *evidencefs.AdmissionInventory, target [32]byte, plan *VerifiedAdmissionPlan, journalID [32]byte, headerBytes []byte) ([32]byte, error) {
	var zero [32]byte
	if inventory == nil || !brandNewReservationPlanExact(plan) || journalID == ([32]byte{}) || len(headerBytes) == 0 {
		return zero, admissionCorrupt("admission-generation-header", "header inventory expectation is invalid", nil)
	}
	lineage, err := inventory.Lineage(target)
	if err != nil {
		return zero, mapEvidenceAdmissionError(err, "admission-generation-header-lineage")
	}
	lineageID, lineageIDErr := lineage.ID()
	index, indexErr := lineage.Index()
	journals, journalsErr := lineage.Journals()
	registrations, registrationsErr := lineage.GenerationRegistrations()
	absent, absentErr := inventory.TargetAbsent()
	for _, accessorErr := range []error{lineageIDErr, indexErr, journalsErr, registrationsErr, absentErr} {
		if accessorErr != nil {
			return zero, mapEvidenceAdmissionError(accessorErr, "admission-generation-header-inventory")
		}
	}
	if lineageID != target || absent != nil || len(journals) != 1 || len(registrations) != 0 {
		return zero, admissionCorrupt("admission-generation-header", "target header inventory shape is invalid", nil)
	}
	indexRaw, err := index.ReadAll(ctx)
	if err != nil {
		return zero, mapEvidenceAdmissionError(err, "admission-generation-header-index-read")
	}
	wantIndex := make([]byte, 0, len(plan.lineageHeaderBytes)+len(plan.reservedFrameBytes))
	wantIndex = append(wantIndex, plan.lineageHeaderBytes...)
	wantIndex = append(wantIndex, plan.reservedFrameBytes...)
	indexDigest, indexDigestErr := index.Digest()
	indexSize, indexSizeErr := index.Size()
	journal, journalIDErr := journals[0].ID()
	segments, segmentsErr := journals[0].Segments()
	for _, accessorErr := range []error{indexDigestErr, indexSizeErr, journalIDErr, segmentsErr} {
		if accessorErr != nil {
			return zero, mapEvidenceAdmissionError(accessorErr, "admission-generation-header-inventory")
		}
	}
	if !bytes.Equal(indexRaw, wantIndex) || indexSize != uint64(len(wantIndex)) || indexDigest != sha256.Sum256(wantIndex) || journal != journalID || len(segments) != 1 {
		return zero, admissionCorrupt("admission-generation-header", "reserved index or journal identity differs", nil)
	}
	ordinal, ordinalErr := segments[0].Ordinal()
	segmentSize, sizeErr := segments[0].Size()
	segmentDigest, digestErr := segments[0].Digest()
	segmentRaw, readErr := segments[0].ReadAll(ctx)
	for _, accessorErr := range []error{ordinalErr, sizeErr, digestErr, readErr} {
		if accessorErr != nil {
			return zero, mapEvidenceAdmissionError(accessorErr, "admission-generation-header-segment")
		}
	}
	if ordinal != 0 || segmentSize != uint64(len(headerBytes)) || segmentDigest != sha256.Sum256(headerBytes) || !bytes.Equal(segmentRaw, headerBytes) {
		return zero, admissionCorrupt("admission-generation-header", "segment-0 differs from the exact activation header", nil)
	}
	return indexDigest, nil
}

func headerDurablePermitDigest(permit *HeaderDurablePermit) [32]byte {
	if permit == nil || permit.self != permit || permit.prior == nil || permit.plan == nil || permit.history == nil || permit.candidateBinding == nil || permit.prior.binding == nil || permit.plan.binding == nil || permit.history.binding == nil || permit.runtimeReceipt.binding == nil || permit.recoveryReceipt.binding == nil {
		return [32]byte{}
	}
	h := sha256.New()
	h.Write([]byte("cloud-agents-platform-header-durable-permit/v1\x00"))
	h.Write(permit.prior.binding.canonical[:])
	h.Write(permit.plan.binding.canonical[:])
	h.Write(permit.history.binding.canonical[:])
	h.Write(permit.candidateBinding.canonical[:])
	h.Write(permit.target[:])
	h.Write(permit.fullSet[:])
	h.Write(permit.indexDigest[:])
	h.Write(permit.fsCandidate[:])
	h.Write(permit.headerBytesHash[:])
	writeAdmissionUint(h, permit.revision)
	for _, value := range []Digest{permit.reservedDigest, permit.journal, permit.headerDigest, permit.runtimeReceipt.digest, permit.recoveryReceipt.digest} {
		writeAdmissionString(h, value.String())
	}
	writeAdmissionUint(h, permit.runtimeReceipt.sizeBytes)
	writeAdmissionUint(h, permit.recoveryReceipt.sizeBytes)
	writeAdmissionString(h, string(permit.fsResult.Outcome()))
	writeAdmissionString(h, permit.fsResult.CandidateKind())
	fsCandidate := permit.fsResult.CandidateDigest()
	h.Write(fsCandidate[:])
	writeAdmissionUint(h, permit.fsResult.CandidateSequence())
	writeAdmissionUint(h, permit.fsResult.PreviousRevision())
	writeAdmissionUint(h, permit.fsResult.CandidateRevision())
	fsJournal := permit.fsResult.Journal()
	fsHeader := permit.fsResult.HeaderDigest()
	h.Write(fsJournal[:])
	h.Write(fsHeader[:])
	writeAdmissionUint(h, permit.fsResult.HeaderSize())
	writeAdmissionString(h, string(permit.headerBytes))
	frameCanonical, frameErr := canonicalContractKey(permit.headerFrame)
	headerCanonical, headerErr := canonicalContractKey(permit.activationHeader.header)
	reservedCanonical, reservedErr := canonicalContractKey(permit.activationHeader.reserved)
	if frameErr != nil || headerErr != nil || reservedErr != nil {
		return [32]byte{}
	}
	writeAdmissionString(h, frameCanonical)
	writeAdmissionString(h, headerCanonical)
	writeAdmissionString(h, reservedCanonical)
	for _, value := range []Digest{
		permit.activationHeader.generation.executionLineageDigest,
		permit.activationHeader.generation.journalIdentityDigest,
		permit.activationHeader.generation.runnerProjectionDecisionDigest,
		permit.activationHeader.generation.schemaBundleDigest,
	} {
		writeAdmissionString(h, value.String())
	}
	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result
}

func validHeaderDurablePermit(permit *HeaderDurablePermit, inventory *evidencefs.AdmissionInventory, candidate OwnedCurrentCandidate) bool {
	if permit == nil || permit.self != permit || permit.binding == nil || permit.binding.permit != permit || permit.prior == nil || permit.plan == nil || permit.history == nil || permit.inventory != inventory || permit.mutation == nil || permit.candidateBinding != candidate.binding || permit.binding.prior != permit.prior || permit.binding.plan != permit.plan || permit.binding.history != permit.history || permit.binding.inventory != inventory || permit.binding.mutation != permit.mutation || permit.binding.runtimeBinding != permit.runtimeReceipt.binding || permit.binding.recoveryBinding != permit.recoveryReceipt.binding || permit.runtimeReceipt.binding != permit.prior.runtimeReceipt.binding || permit.recoveryReceipt.binding != permit.prior.recoveryReceipt.binding || permit.consumed == nil || permit.consumed.Load() || !validConsumedReservedDurablePermit(permit.prior, permit.plan, candidate) || permit.history != permit.plan.history || permit.revision != permit.prior.revision+1 || permit.target != permit.prior.target || permit.fullSet == permit.prior.fullSet || permit.indexDigest != permit.prior.indexDigest || permit.fsCandidate == ([32]byte{}) || permit.fsCandidate != permit.fsResult.CandidateDigest() || permit.fsResult.Inventory() != inventory || permit.fsResult.Journal() != digestRaw(permit.journal) || permit.fsResult.HeaderDigest() != permit.headerBytesHash || permit.fsResult.HeaderSize() != uint64(len(permit.headerBytes)) || permit.fsResult.PreviousRevision() != permit.prior.revision || permit.fsResult.CandidateRevision() != permit.revision || !permit.fsResult.ValidFor(inventory) || permit.headerBytesHash == ([32]byte{}) || permit.headerBytesHash != sha256.Sum256(permit.headerBytes) || permit.reservedDigest != permit.prior.reservedDigest || permit.journal != permit.prior.journal || permit.headerDigest != permit.prior.headerDigest || !validRuntimeReceipt(permit.runtimeReceipt, candidate.owner, permit.activationHeader.header.OuterArtifactDigest, permit.activationHeader.header.OuterArtifactSizeBytes) || !validDecisionRecoveryReceipt(permit.recoveryReceipt, candidate.owner, permit.activationHeader.header.DecisionRecoveryArtifactSHA256, permit.activationHeader.header.DecisionRecoveryArtifactSizeBytes) || !permit.runtimeReceipt.publication.SameStore(permit.recoveryReceipt.publication) || !permit.mutation.ValidFor(inventory) || permit.binding.canonical == ([32]byte{}) || permit.binding.canonical != headerDurablePermitDigest(permit) {
		return false
	}
	reserved := permit.plan.reservedFrame.Record.Reserved
	if reserved == nil {
		return false
	}
	generation := generationIdentity{candidate.owner, reserved.ExecutionLineageDigest, reserved.JournalIdentityDigest, reserved.RunnerProjectionDecisionDigest, reserved.SchemaBundleDigest}
	wantHeader, err := bindActivationHeader(generation, *reserved, permit.runtimeReceipt, permit.recoveryReceipt)
	wantFrame, wantBytes, encodeErr := encodeAdmissionActivationHeader(wantHeader)
	if err != nil || encodeErr != nil || !sameGenerationIdentity(permit.activationHeader.generation, generation) || !canonicalEqual(permit.activationHeader.header, wantHeader.header) || !canonicalEqual(permit.activationHeader.reserved, wantHeader.reserved) || !canonicalEqual(permit.headerFrame, wantFrame) || !bytes.Equal(permit.headerBytes, wantBytes) || permit.headerFrame.RecordDigest != permit.headerDigest {
		return false
	}
	registered, ok := headerDurablePermitRegistry.Load(permit.binding)
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

func validConsumedReservedDurablePermit(permit *ReservedDurablePermit, plan *VerifiedAdmissionPlan, candidate OwnedCurrentCandidate) bool {
	if permit == nil || permit.self != permit || permit.binding == nil || permit.binding.permit != permit || permit.prior == nil || permit.plan != plan || plan == nil || permit.history == nil || permit.inventory == nil || permit.mutation == nil || permit.candidateBinding != candidate.binding || permit.binding.prior != permit.prior || permit.binding.plan != plan || permit.binding.history != permit.history || permit.binding.inventory != permit.inventory || permit.binding.mutation != permit.mutation || permit.binding.runtimeBinding != permit.runtimeReceipt.binding || permit.binding.recoveryBinding != permit.recoveryReceipt.binding || permit.runtimeReceipt.binding != permit.prior.runtimeReceipt.binding || permit.recoveryReceipt.binding != permit.prior.recoveryReceipt.binding || permit.consumed == nil || !permit.consumed.Load() || !validConsumedReceiptBoundReady(permit.prior, plan, candidate) || !brandNewReservationPlanExact(plan) || permit.history != plan.history || permit.revision != permit.prior.revision+1 || permit.target != permit.prior.target || permit.fullSet == permit.prior.fullSet || permit.indexDigest != reservedPlanIndexDigest(plan) || permit.framedDigest != sha256.Sum256(plan.reservedFrameBytes) || plan.reservedFrame.Record.Reserved == nil || permit.reservedDigest != plan.reservedFrame.RecordDigest || permit.journal != plan.reservedFrame.Record.Reserved.JournalIdentityDigest || permit.headerDigest != plan.reservedFrame.Record.Reserved.ExpectedSegment0HeaderDigest || !validRuntimeReceipt(permit.runtimeReceipt, candidate.owner, plan.reservedFrame.Record.Reserved.PlannedSegment0Header.OuterArtifactDigest, plan.reservedFrame.Record.Reserved.PlannedSegment0Header.OuterArtifactSizeBytes) || !validDecisionRecoveryReceipt(permit.recoveryReceipt, candidate.owner, plan.reservedFrame.Record.Reserved.PlannedSegment0Header.DecisionRecoveryArtifactSHA256, plan.reservedFrame.Record.Reserved.PlannedSegment0Header.DecisionRecoveryArtifactSizeBytes) || !permit.runtimeReceipt.publication.SameStore(permit.recoveryReceipt.publication) || permit.binding.canonical == ([32]byte{}) || permit.binding.canonical != reservedDurablePermitDigest(permit) {
		return false
	}
	registered, ok := reservedDurablePermitRegistry.Load(permit.binding)
	return ok && registered == permit.binding.canonical
}
