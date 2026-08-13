package migration

import (
	"bytes"
	"context"
	"crypto/sha256"
	"sync"
	"sync/atomic"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/evidencefs"
)

// GenerationActivationTransitionResult is the closed projection of the final
// pre-DB durability barrier. CandidateDigest covers exact framed index bytes;
// the three C3 record digests remain distinct typed facts.
type GenerationActivationTransitionResult struct {
	outcome                evidencefs.AdmissionTransitionOutcome
	next                   *GenerationReadyPermit
	candidateDigest        [32]byte
	candidateSequence      uint64
	candidateRevision      uint64
	previousRevision       uint64
	activationRecordDigest Digest
	reservedRecordDigest   Digest
	headerRecordDigest     Digest
}

func (r GenerationActivationTransitionResult) Outcome() evidencefs.AdmissionTransitionOutcome {
	return r.outcome
}
func (r GenerationActivationTransitionResult) Next() *GenerationReadyPermit { return r.next }
func (r GenerationActivationTransitionResult) CandidateKind() string        { return "generation_activated" }
func (r GenerationActivationTransitionResult) CandidateDigest() [32]byte    { return r.candidateDigest }
func (r GenerationActivationTransitionResult) CandidateSequence() uint64    { return r.candidateSequence }
func (r GenerationActivationTransitionResult) CandidateRevision() uint64    { return r.candidateRevision }
func (r GenerationActivationTransitionResult) PreviousRevision() uint64     { return r.previousRevision }
func (r GenerationActivationTransitionResult) ActivationRecordDigest() Digest {
	return r.activationRecordDigest
}
func (r GenerationActivationTransitionResult) ReservedRecordDigest() Digest {
	return r.reservedRecordDigest
}
func (r GenerationActivationTransitionResult) HeaderRecordDigest() Digest {
	return r.headerRecordDigest
}

// GenerationReadyPermit proves reserve -> exact segment-0 header -> activate
// are all durable under the same admission epoch and retained lock chain. It is
// intentionally not ActiveGeneration and cannot connect, mint a JournalCursor,
// or perform normal-run work. Its only production consumer transfers exact lock
// ownership into the non-runnable handoff state.
type GenerationReadyPermit struct {
	self             *GenerationReadyPermit
	prior            *HeaderDurablePermit
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
	activatedFrame   LineageIndexFrame
	activatedBytes   []byte
	fsResult         evidencefs.AdmissionTransitionResult
	target, fullSet  [32]byte
	revision         uint64
	indexDigest      [32]byte
	activationBytes  [32]byte
	reservedDigest   Digest
	journal          Digest
	headerDigest     Digest
	activationDigest Digest
	binding          *generationReadyPermitBinding
	consumed         *atomic.Bool
}

type generationReadyPermitBinding struct {
	permit          *GenerationReadyPermit
	prior           *HeaderDurablePermit
	plan            *VerifiedAdmissionPlan
	history         *VerifiedAdmissionHistory
	inventory       *evidencefs.AdmissionInventory
	mutation        *evidencefs.AdmissionMutationToken
	runtimeBinding  *verifiedContentReceiptBinding
	recoveryBinding *verifiedDecisionRecoveryReceiptBinding
	canonical       [32]byte
}

type generationReadyPermitRegistryRecord struct {
	permit           *GenerationReadyPermit
	binding          *generationReadyPermitBinding
	prior            *HeaderDurablePermit
	plan             *VerifiedAdmissionPlan
	history          *VerifiedAdmissionHistory
	candidateBinding *verifiedEvidenceRunBinding
	inventory        *evidencefs.AdmissionInventory
	mutation         *evidencefs.AdmissionMutationToken
	runtimeBinding   *verifiedContentReceiptBinding
	recoveryBinding  *verifiedDecisionRecoveryReceiptBinding
	canonical        [32]byte
}

var generationReadyPermitRegistry sync.Map

// AppendGenerationActivated consumes the exact header-durable authority and
// appends its matching GenerationActivated frame. Success stops at a sealed
// generation-ready authority; normal-run handoff remains a later transition.
func (p *HeaderDurablePermit) AppendGenerationActivated(ctx context.Context, candidate OwnedCurrentCandidate) (GenerationActivationTransitionResult, error) {
	pre := GenerationActivationTransitionResult{outcome: evidencefs.AdmissionTransitionPreMutationFailure, candidateSequence: 8}
	if p == nil || p.inventory == nil || !validHeaderDurablePermit(p, p.inventory, candidate) {
		return pre, fail(CodeEvidenceRecoveryRequired, "admission-generation-activate", "header-durable generation authority is unavailable", nil)
	}
	if err := contextAdmissionError(ctx); err != nil {
		return pre, err
	}
	activatedFrame, activatedBytes, err := buildAdmissionActivatedFrame(p.plan.reservedFrame, p.headerFrame)
	if err != nil {
		return pre, fail(CodeEvidenceRecoveryRequired, "admission-generation-activate", "generation activation frame cannot be constructed", nil)
	}
	activatedBytes = append([]byte(nil), activatedBytes...)
	pre.previousRevision = p.revision
	pre.candidateRevision = p.revision + 1
	pre.candidateDigest = sha256.Sum256(activatedBytes)
	pre.activationRecordDigest = activatedFrame.RecordDigest
	pre.reservedRecordDigest = p.reservedDigest
	pre.headerRecordDigest = p.headerDigest
	if pre.candidateDigest == ([32]byte{}) || requireDigest("admission-generation-activate.record", pre.activationRecordDigest) != nil {
		return pre, fail(CodeEvidenceRecoveryRequired, "admission-generation-activate", "generation activation identity is invalid", nil)
	}
	if err := p.inventory.Revalidate(ctx); err != nil {
		return pre, mapEvidenceAdmissionError(err, "admission-generation-activate-revalidate")
	}
	indexDigest, err := validateHeaderInventory(ctx, p.inventory, p.target, p.plan, digestRaw(p.journal), p.headerBytes)
	if err != nil {
		return pre, err
	}
	if indexDigest != p.indexDigest || !validHeaderDurablePermit(p, p.inventory, candidate) || !bytes.Equal(activatedBytes, mustEncodeLineageFrame(activatedFrame)) {
		return pre, fail(CodeEvidenceRecoveryRequired, "admission-generation-activate", "header-durable generation changed before activation", nil)
	}
	if !p.consumed.CompareAndSwap(false, true) {
		return pre, fail(CodeEvidenceRecoveryRequired, "admission-generation-activate", "header-durable generation authority was already consumed", nil)
	}
	fsResult, transitionErr := p.mutation.AppendTargetIndex(ctx, p.inventory, activatedBytes)
	result := GenerationActivationTransitionResult{
		outcome: fsResult.Outcome(), candidateDigest: fsResult.CandidateDigest(), candidateSequence: 8,
		candidateRevision: fsResult.CandidateRevision(), previousRevision: fsResult.PreviousRevision(),
		activationRecordDigest: pre.activationRecordDigest, reservedRecordDigest: pre.reservedRecordDigest, headerRecordDigest: pre.headerRecordDigest,
	}
	if fsResult.Outcome() != evidencefs.AdmissionTransitionDurable {
		if fsResult.Outcome() == evidencefs.AdmissionTransitionPreMutationFailure {
			p.consumed.CompareAndSwap(true, false)
		}
		return result, mapAdmissionMutationError(transitionErr, "admission-generation-activate")
	}
	if transitionErr != nil || fsResult.CandidateKind() != "target_index_append" || fsResult.CandidateSequence() != 0 || fsResult.CandidateDigest() != pre.candidateDigest || fsResult.PreviousRevision() != pre.previousRevision || fsResult.CandidateRevision() != pre.candidateRevision || fsResult.Inventory() == nil {
		_ = fsResult.Invalidate()
		return generationActivationUnknown(result), admissionPostMutationFailure("admission-generation-activate")
	}
	nextInventory := fsResult.Inventory()
	if err := nextInventory.Revalidate(ctx); err != nil {
		_ = fsResult.Invalidate()
		return generationActivationUnknown(result), admissionPostMutationFailure("admission-generation-activate-revalidate")
	}
	revision, err := nextInventory.Revision()
	if err != nil || revision != pre.candidateRevision {
		_ = fsResult.Invalidate()
		return generationActivationUnknown(result), admissionPostMutationFailure("admission-generation-activate-revision")
	}
	target, err := nextInventory.Target()
	if err != nil || target != p.target {
		_ = fsResult.Invalidate()
		return generationActivationUnknown(result), admissionPostMutationFailure("admission-generation-activate-target")
	}
	fullSet, err := nextInventory.FullSetDigest()
	if err != nil || fullSet == ([32]byte{}) || fullSet == p.fullSet {
		_ = fsResult.Invalidate()
		return generationActivationUnknown(result), admissionPostMutationFailure("admission-generation-activate-full-set")
	}
	nextIndexDigest, err := validateActivatedInventory(ctx, nextInventory, target, p.plan, activatedBytes, digestRaw(p.journal), p.headerBytes)
	if err != nil {
		_ = fsResult.Invalidate()
		return generationActivationUnknown(result), admissionPostMutationFailure("admission-generation-activate-inventory")
	}
	if !validRuntimeReceipt(p.runtimeReceipt, candidate.owner, p.activationHeader.header.OuterArtifactDigest, p.activationHeader.header.OuterArtifactSizeBytes) || !validDecisionRecoveryReceipt(p.recoveryReceipt, candidate.owner, p.activationHeader.header.DecisionRecoveryArtifactSHA256, p.activationHeader.header.DecisionRecoveryArtifactSizeBytes) || !p.runtimeReceipt.publication.SameStore(p.recoveryReceipt.publication) {
		_ = fsResult.Invalidate()
		return generationActivationUnknown(result), admissionPostMutationFailure("admission-generation-activate-receipts")
	}
	if err := nextInventory.Revalidate(ctx); err != nil {
		_ = fsResult.Invalidate()
		return generationActivationUnknown(result), admissionPostMutationFailure("admission-generation-activate-terminal-revalidate")
	}
	nextToken, err := nextInventory.MutationToken()
	if err != nil || !nextToken.ValidFor(nextInventory) {
		_ = fsResult.Invalidate()
		return generationActivationUnknown(result), admissionPostMutationFailure("admission-generation-activate-token")
	}
	next := &GenerationReadyPermit{
		prior: p, plan: p.plan, history: p.history, candidateBinding: candidate.binding,
		inventory: nextInventory, mutation: nextToken, runtimeReceipt: p.runtimeReceipt, recoveryReceipt: p.recoveryReceipt,
		activationHeader: p.activationHeader, headerFrame: cloneProjectionValue(p.headerFrame), headerBytes: append([]byte(nil), p.headerBytes...),
		activatedFrame: activatedFrame, activatedBytes: activatedBytes, fsResult: fsResult,
		target: target, fullSet: fullSet, revision: revision, indexDigest: nextIndexDigest, activationBytes: pre.candidateDigest,
		reservedDigest: p.reservedDigest, journal: p.journal, headerDigest: p.headerDigest, activationDigest: activatedFrame.RecordDigest, consumed: &atomic.Bool{},
	}
	next.self = next
	binding := &generationReadyPermitBinding{
		permit: next, prior: p, plan: p.plan, history: p.history, inventory: nextInventory, mutation: nextToken,
		runtimeBinding: p.runtimeReceipt.binding, recoveryBinding: p.recoveryReceipt.binding,
	}
	next.binding = binding
	binding.canonical = generationReadyPermitDigest(next)
	generationReadyPermitRegistry.Store(next, generationReadyPermitRegistryRecord{
		permit: next, binding: binding, prior: p, plan: p.plan, history: p.history, candidateBinding: candidate.binding,
		inventory: nextInventory, mutation: nextToken, runtimeBinding: p.runtimeReceipt.binding,
		recoveryBinding: p.recoveryReceipt.binding, canonical: binding.canonical,
	})
	if !validGenerationReadyPermit(next, nextInventory, candidate) {
		generationReadyPermitRegistry.Delete(next)
		_ = fsResult.Invalidate()
		return generationActivationUnknown(result), admissionPostMutationFailure("admission-generation-activate-seal")
	}
	result.next = next
	return result, nil
}

func generationActivationUnknown(value GenerationActivationTransitionResult) GenerationActivationTransitionResult {
	value.outcome = evidencefs.AdmissionTransitionUnknown
	value.next = nil
	return value
}

func buildAdmissionActivatedFrame(reservedFrame LineageIndexFrame, headerFrame EvidenceFrame) (LineageIndexFrame, []byte, error) {
	if reservedFrame.Validate() != nil || reservedFrame.Sequence != 1 || reservedFrame.PreviousRecordDigest == nil || reservedFrame.RecordKind != LineageRecordGenerationReserved || reservedFrame.Record.Reserved == nil || headerFrame.Validate() != nil || headerFrame.Sequence != 0 || headerFrame.PreviousRecordDigest != nil || headerFrame.RecordKind != EvidenceRecordHeader || headerFrame.Record.Header == nil {
		return LineageIndexFrame{}, nil, invalidEvidence("admission-generation-activate", "reserved or header frame is invalid")
	}
	reserved := reservedFrame.Record.Reserved
	if !canonicalEqual(*headerFrame.Record.Header, reserved.PlannedSegment0Header) || headerFrame.RecordDigest != reserved.ExpectedSegment0HeaderDigest || reservedFrame.Sequence == maxJSONInteger {
		return LineageIndexFrame{}, nil, invalidEvidence("admission-generation-activate", "reserved and header frames differ")
	}
	activated := GenerationActivated{
		ExecutionLineageDigest: reserved.ExecutionLineageDigest, JournalIdentityDigest: reserved.JournalIdentityDigest,
		RunnerProjectionDecisionDigest: reserved.RunnerProjectionDecisionDigest, SchemaBundleDigest: reserved.SchemaBundleDigest,
		QuotaReservationDigest: reserved.QuotaReservationDigest, GenerationReservedRecordDigest: reservedFrame.RecordDigest,
		Segment0HeaderDigest: headerFrame.RecordDigest, InitialJournalTailDigest: headerFrame.RecordDigest,
	}
	previous := reservedFrame.RecordDigest
	frame := LineageIndexFrame{
		FormatVersion: LineageFrameFormat, Sequence: reservedFrame.Sequence + 1, PreviousRecordDigest: &previous,
		RecordKind: LineageRecordGenerationActivated, Record: LineageIndexRecord{Activated: &activated},
	}
	var err error
	frame.RecordDigest, err = frame.ComputeDigest()
	if err != nil || frame.Validate() != nil || activated.Validate() != nil {
		return LineageIndexFrame{}, nil, invalidEvidence("admission-generation-activate", "activation frame is invalid")
	}
	raw, err := EncodeCanonicalLineageFrame(frame)
	if err != nil {
		return LineageIndexFrame{}, nil, err
	}
	return cloneProjectionValue(frame), append([]byte(nil), raw...), nil
}

func mustEncodeLineageFrame(frame LineageIndexFrame) []byte {
	raw, err := EncodeCanonicalLineageFrame(frame)
	if err != nil {
		return nil
	}
	return raw
}

func validateActivatedInventory(ctx context.Context, inventory *evidencefs.AdmissionInventory, target [32]byte, plan *VerifiedAdmissionPlan, activatedBytes []byte, journalID [32]byte, headerBytes []byte) ([32]byte, error) {
	var zero [32]byte
	if inventory == nil || !brandNewReservationPlanExact(plan) || len(activatedBytes) == 0 {
		return zero, admissionCorrupt("admission-generation-activate", "activated inventory expectation is invalid", nil)
	}
	lineage, err := inventory.Lineage(target)
	if err != nil {
		return zero, mapEvidenceAdmissionError(err, "admission-generation-activate-lineage")
	}
	lineageID, lineageIDErr := lineage.ID()
	index, indexErr := lineage.Index()
	journals, journalsErr := lineage.Journals()
	absent, absentErr := inventory.TargetAbsent()
	for _, accessorErr := range []error{lineageIDErr, indexErr, journalsErr, absentErr} {
		if accessorErr != nil {
			return zero, mapEvidenceAdmissionError(accessorErr, "admission-generation-activate-inventory")
		}
	}
	if lineageID != target || absent != nil || len(journals) != 1 {
		return zero, admissionCorrupt("admission-generation-activate", "activated inventory shape is invalid", nil)
	}
	indexRaw, err := index.ReadAll(ctx)
	if err != nil {
		return zero, mapEvidenceAdmissionError(err, "admission-generation-activate-index-read")
	}
	wantIndex := make([]byte, 0, len(plan.lineageHeaderBytes)+len(plan.reservedFrameBytes)+len(activatedBytes))
	wantIndex = append(wantIndex, plan.lineageHeaderBytes...)
	wantIndex = append(wantIndex, plan.reservedFrameBytes...)
	wantIndex = append(wantIndex, activatedBytes...)
	indexDigest, indexDigestErr := index.Digest()
	indexSize, indexSizeErr := index.Size()
	journal, journalErr := journals[0].ID()
	segments, segmentsErr := journals[0].Segments()
	for _, accessorErr := range []error{indexDigestErr, indexSizeErr, journalErr, segmentsErr} {
		if accessorErr != nil {
			return zero, mapEvidenceAdmissionError(accessorErr, "admission-generation-activate-inventory")
		}
	}
	if !bytes.Equal(indexRaw, wantIndex) || indexSize != uint64(len(wantIndex)) || indexDigest != sha256.Sum256(wantIndex) || journal != journalID || len(segments) != 1 {
		return zero, admissionCorrupt("admission-generation-activate", "activated index or journal differs", nil)
	}
	ordinal, ordinalErr := segments[0].Ordinal()
	segmentSize, segmentSizeErr := segments[0].Size()
	segmentDigest, segmentDigestErr := segments[0].Digest()
	segmentRaw, segmentReadErr := segments[0].ReadAll(ctx)
	for _, accessorErr := range []error{ordinalErr, segmentSizeErr, segmentDigestErr, segmentReadErr} {
		if accessorErr != nil {
			return zero, mapEvidenceAdmissionError(accessorErr, "admission-generation-activate-segment")
		}
	}
	if ordinal != 0 || segmentSize != uint64(len(headerBytes)) || segmentDigest != sha256.Sum256(headerBytes) || !bytes.Equal(segmentRaw, headerBytes) {
		return zero, admissionCorrupt("admission-generation-activate", "segment-0 changed during activation", nil)
	}
	return indexDigest, nil
}

func activationPlanIndexDigest(plan *VerifiedAdmissionPlan, activatedBytes []byte) [32]byte {
	if plan == nil || len(plan.lineageHeaderBytes) == 0 || len(plan.reservedFrameBytes) == 0 || len(activatedBytes) == 0 {
		return [32]byte{}
	}
	h := sha256.New()
	h.Write(plan.lineageHeaderBytes)
	h.Write(plan.reservedFrameBytes)
	h.Write(activatedBytes)
	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result
}

func generationReadyPermitDigest(permit *GenerationReadyPermit) [32]byte {
	if permit == nil || permit.self != permit || permit.prior == nil || permit.plan == nil || permit.history == nil || permit.candidateBinding == nil || permit.prior.binding == nil || permit.plan.binding == nil || permit.history.binding == nil || permit.runtimeReceipt.binding == nil || permit.recoveryReceipt.binding == nil {
		return [32]byte{}
	}
	h := sha256.New()
	h.Write([]byte("cloud-agents-platform-generation-ready-permit/v1\x00"))
	h.Write(permit.prior.binding.canonical[:])
	h.Write(permit.plan.binding.canonical[:])
	h.Write(permit.history.binding.canonical[:])
	h.Write(permit.candidateBinding.canonical[:])
	h.Write(permit.target[:])
	h.Write(permit.fullSet[:])
	h.Write(permit.indexDigest[:])
	h.Write(permit.activationBytes[:])
	writeAdmissionUint(h, permit.revision)
	for _, value := range []Digest{permit.reservedDigest, permit.journal, permit.headerDigest, permit.activationDigest, permit.runtimeReceipt.digest, permit.recoveryReceipt.digest} {
		writeAdmissionString(h, value.String())
	}
	writeAdmissionUint(h, permit.runtimeReceipt.sizeBytes)
	writeAdmissionUint(h, permit.recoveryReceipt.sizeBytes)
	writeAdmissionString(h, string(permit.headerBytes))
	writeAdmissionString(h, string(permit.activatedBytes))
	for _, value := range []any{permit.headerFrame, permit.activatedFrame, permit.activationHeader.header, permit.activationHeader.reserved} {
		canonical, err := canonicalContractKey(value)
		if err != nil {
			return [32]byte{}
		}
		writeAdmissionString(h, canonical)
	}
	for _, value := range []Digest{
		permit.activationHeader.generation.executionLineageDigest,
		permit.activationHeader.generation.journalIdentityDigest,
		permit.activationHeader.generation.runnerProjectionDecisionDigest,
		permit.activationHeader.generation.schemaBundleDigest,
	} {
		writeAdmissionString(h, value.String())
	}
	writeAdmissionString(h, string(permit.fsResult.Outcome()))
	writeAdmissionString(h, permit.fsResult.CandidateKind())
	fsCandidate := permit.fsResult.CandidateDigest()
	h.Write(fsCandidate[:])
	writeAdmissionUint(h, permit.fsResult.CandidateSequence())
	writeAdmissionUint(h, permit.fsResult.PreviousRevision())
	writeAdmissionUint(h, permit.fsResult.CandidateRevision())
	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result
}

func validGenerationReadyPermit(permit *GenerationReadyPermit, inventory *evidencefs.AdmissionInventory, candidate OwnedCurrentCandidate) bool {
	if permit == nil || permit.self != permit || permit.binding == nil || permit.binding.permit != permit || permit.prior == nil || permit.plan == nil || permit.history == nil || permit.inventory != inventory || permit.mutation == nil || permit.candidateBinding != candidate.binding || permit.binding.prior != permit.prior || permit.binding.plan != permit.plan || permit.binding.history != permit.history || permit.binding.inventory != inventory || permit.binding.mutation != permit.mutation || permit.binding.runtimeBinding != permit.runtimeReceipt.binding || permit.binding.recoveryBinding != permit.recoveryReceipt.binding || permit.runtimeReceipt.binding != permit.prior.runtimeReceipt.binding || permit.recoveryReceipt.binding != permit.prior.recoveryReceipt.binding || permit.consumed == nil || permit.consumed.Load() || !validConsumedHeaderDurablePermit(permit.prior, permit.plan, candidate) || permit.history != permit.plan.history || permit.revision != permit.prior.revision+1 || permit.target != permit.prior.target || permit.fullSet == permit.prior.fullSet || permit.indexDigest != activationPlanIndexDigest(permit.plan, permit.activatedBytes) || permit.activationBytes != sha256.Sum256(permit.activatedBytes) || permit.reservedDigest != permit.prior.reservedDigest || permit.journal != permit.prior.journal || permit.headerDigest != permit.prior.headerDigest || permit.activationDigest != permit.activatedFrame.RecordDigest || permit.fsResult.Outcome() != evidencefs.AdmissionTransitionDurable || permit.fsResult.Inventory() != inventory || permit.fsResult.CandidateKind() != "target_index_append" || permit.fsResult.CandidateDigest() != permit.activationBytes || permit.fsResult.CandidateSequence() != 0 || permit.fsResult.PreviousRevision() != permit.prior.revision || permit.fsResult.CandidateRevision() != permit.revision || !validRuntimeReceipt(permit.runtimeReceipt, candidate.owner, permit.activationHeader.header.OuterArtifactDigest, permit.activationHeader.header.OuterArtifactSizeBytes) || !validDecisionRecoveryReceipt(permit.recoveryReceipt, candidate.owner, permit.activationHeader.header.DecisionRecoveryArtifactSHA256, permit.activationHeader.header.DecisionRecoveryArtifactSizeBytes) || !permit.runtimeReceipt.publication.SameStore(permit.recoveryReceipt.publication) || !permit.mutation.ValidFor(inventory) || permit.binding.canonical == ([32]byte{}) || permit.binding.canonical != generationReadyPermitDigest(permit) {
		return false
	}
	reserved := permit.plan.reservedFrame.Record.Reserved
	if reserved == nil {
		return false
	}
	generation := generationIdentity{candidate.owner, reserved.ExecutionLineageDigest, reserved.JournalIdentityDigest, reserved.RunnerProjectionDecisionDigest, reserved.SchemaBundleDigest}
	wantHeader, headerErr := bindActivationHeader(generation, *reserved, permit.runtimeReceipt, permit.recoveryReceipt)
	wantHeaderFrame, wantHeaderBytes, headerEncodeErr := encodeAdmissionActivationHeader(wantHeader)
	wantActivated, wantActivatedBytes, activatedErr := buildAdmissionActivatedFrame(permit.plan.reservedFrame, wantHeaderFrame)
	if headerErr != nil || headerEncodeErr != nil || activatedErr != nil || !sameGenerationIdentity(permit.activationHeader.generation, generation) || !canonicalEqual(permit.activationHeader.header, wantHeader.header) || !canonicalEqual(permit.activationHeader.reserved, wantHeader.reserved) || !canonicalEqual(permit.headerFrame, wantHeaderFrame) || !bytes.Equal(permit.headerBytes, wantHeaderBytes) || !canonicalEqual(permit.activatedFrame, wantActivated) || !bytes.Equal(permit.activatedBytes, wantActivatedBytes) {
		return false
	}
	registered, ok := generationReadyPermitRegistry.Load(permit)
	record, recordOK := registered.(generationReadyPermitRegistryRecord)
	if !ok || !recordOK || !generationReadyRegistryRecordMatches(record, permit) {
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
	return generationReadyMetadataMatches(inventory, permit.target, digestRaw(permit.journal), permit.indexDigest, sha256.Sum256(permit.headerBytes), uint64(len(permit.headerBytes)))
}

func generationReadyRegistryRecordMatches(record generationReadyPermitRegistryRecord, permit *GenerationReadyPermit) bool {
	return permit != nil && record.permit == permit && record.binding == permit.binding && record.prior == permit.prior && record.plan == permit.plan &&
		record.history == permit.history && record.candidateBinding == permit.candidateBinding && record.inventory == permit.inventory &&
		record.mutation == permit.mutation && record.runtimeBinding == permit.runtimeReceipt.binding &&
		record.recoveryBinding == permit.recoveryReceipt.binding && record.canonical == permit.binding.canonical
}

func generationReadyMetadataMatches(inventory *evidencefs.AdmissionInventory, target, journalID [32]byte, indexDigest, headerBytesDigest [32]byte, headerSize uint64) bool {
	if inventory == nil || journalID == ([32]byte{}) || indexDigest == ([32]byte{}) || headerBytesDigest == ([32]byte{}) || headerSize == 0 {
		return false
	}
	lineage, err := inventory.Lineage(target)
	if err != nil {
		return false
	}
	index, indexErr := lineage.Index()
	journals, journalsErr := lineage.Journals()
	if indexErr != nil || journalsErr != nil || len(journals) != 1 {
		return false
	}
	digest, digestErr := index.Digest()
	journal, journalErr := journals[0].ID()
	segments, segmentsErr := journals[0].Segments()
	if digestErr != nil || journalErr != nil || segmentsErr != nil || digest != indexDigest || journal != journalID || len(segments) != 1 {
		return false
	}
	ordinal, ordinalErr := segments[0].Ordinal()
	size, sizeErr := segments[0].Size()
	headerDigest, headerErr := segments[0].Digest()
	return ordinalErr == nil && sizeErr == nil && headerErr == nil && ordinal == 0 && size == headerSize && headerDigest == headerBytesDigest
}

func validConsumedHeaderDurablePermit(permit *HeaderDurablePermit, plan *VerifiedAdmissionPlan, candidate OwnedCurrentCandidate) bool {
	if permit == nil || permit.self != permit || permit.binding == nil || permit.binding.permit != permit || permit.prior == nil || permit.plan != plan || plan == nil || permit.history == nil || permit.inventory == nil || permit.mutation == nil || permit.candidateBinding != candidate.binding || permit.binding.prior != permit.prior || permit.binding.plan != plan || permit.binding.history != permit.history || permit.binding.inventory != permit.inventory || permit.binding.mutation != permit.mutation || permit.binding.runtimeBinding != permit.runtimeReceipt.binding || permit.binding.recoveryBinding != permit.recoveryReceipt.binding || permit.runtimeReceipt.binding != permit.prior.runtimeReceipt.binding || permit.recoveryReceipt.binding != permit.prior.recoveryReceipt.binding || permit.consumed == nil || !permit.consumed.Load() || !validConsumedReservedDurablePermit(permit.prior, plan, candidate) || permit.history != plan.history || permit.revision != permit.prior.revision+1 || permit.target != permit.prior.target || permit.fullSet == permit.prior.fullSet || permit.indexDigest != permit.prior.indexDigest || permit.fsCandidate != permit.fsResult.CandidateDigest() || permit.fsResult.Outcome() != evidencefs.AdmissionTransitionDurable || permit.fsResult.Inventory() != permit.inventory || permit.fsResult.CandidateKind() != "generation_header" || permit.fsResult.CandidateSequence() != 0 || permit.fsResult.Journal() != digestRaw(permit.journal) || permit.fsResult.HeaderDigest() != permit.headerBytesHash || permit.fsResult.HeaderSize() != uint64(len(permit.headerBytes)) || permit.fsResult.PreviousRevision() != permit.prior.revision || permit.fsResult.CandidateRevision() != permit.revision || permit.headerBytesHash != sha256.Sum256(permit.headerBytes) || permit.reservedDigest != permit.prior.reservedDigest || permit.journal != permit.prior.journal || permit.headerDigest != permit.prior.headerDigest || !validRuntimeReceipt(permit.runtimeReceipt, candidate.owner, permit.activationHeader.header.OuterArtifactDigest, permit.activationHeader.header.OuterArtifactSizeBytes) || !validDecisionRecoveryReceipt(permit.recoveryReceipt, candidate.owner, permit.activationHeader.header.DecisionRecoveryArtifactSHA256, permit.activationHeader.header.DecisionRecoveryArtifactSizeBytes) || !permit.runtimeReceipt.publication.SameStore(permit.recoveryReceipt.publication) || permit.binding.canonical == ([32]byte{}) || permit.binding.canonical != headerDurablePermitDigest(permit) {
		return false
	}
	registered, ok := headerDurablePermitRegistry.Load(permit.binding)
	return ok && registered == permit.binding.canonical
}
