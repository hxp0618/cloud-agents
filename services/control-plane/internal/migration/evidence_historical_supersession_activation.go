package migration

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"sync"
	"sync/atomic"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/evidencefs"
)

type HistoricalSuccessorGenerationHeaderTransitionResult struct {
	outcome            evidencefs.AdmissionTransitionOutcome
	next               *HistoricalSuccessorHeaderDurablePermit
	candidateDigest    [32]byte
	candidateSequence  uint64
	candidateRevision  uint64
	previousRevision   uint64
	journal            Digest
	headerRecordDigest Digest
	headerBytesDigest  [32]byte
	headerSize         uint64
}

func (r HistoricalSuccessorGenerationHeaderTransitionResult) Outcome() evidencefs.AdmissionTransitionOutcome {
	return r.outcome
}
func (r HistoricalSuccessorGenerationHeaderTransitionResult) Next() *HistoricalSuccessorHeaderDurablePermit {
	return r.next
}
func (r HistoricalSuccessorGenerationHeaderTransitionResult) CandidateKind() string {
	return "generation_header"
}
func (r HistoricalSuccessorGenerationHeaderTransitionResult) CandidateDigest() [32]byte {
	return r.candidateDigest
}
func (r HistoricalSuccessorGenerationHeaderTransitionResult) CandidateSequence() uint64 {
	return r.candidateSequence
}
func (r HistoricalSuccessorGenerationHeaderTransitionResult) CandidateRevision() uint64 {
	return r.candidateRevision
}
func (r HistoricalSuccessorGenerationHeaderTransitionResult) PreviousRevision() uint64 {
	return r.previousRevision
}
func (r HistoricalSuccessorGenerationHeaderTransitionResult) Journal() Digest { return r.journal }
func (r HistoricalSuccessorGenerationHeaderTransitionResult) HeaderRecordDigest() Digest {
	return r.headerRecordDigest
}
func (r HistoricalSuccessorGenerationHeaderTransitionResult) HeaderBytesDigest() [32]byte {
	return r.headerBytesDigest
}
func (r HistoricalSuccessorGenerationHeaderTransitionResult) HeaderSize() uint64 {
	return r.headerSize
}

type HistoricalSuccessorHeaderDurablePermit struct {
	self               *HistoricalSuccessorHeaderDurablePermit
	owner              *recoveryVerifierOwner
	candidateBinding   *verifiedEvidenceRunBinding
	inventory          *evidencefs.AdmissionInventory
	mutation           *evidencefs.AdmissionMutationToken
	revision           uint64
	target, fullSet    [32]byte
	indexRecords       uint64
	indexTail          Digest
	indexDigest        [32]byte
	indexSize          uint64
	source             *verifiedAdmissionRegisteredGeneration
	planned            *verifiedAdmissionRegisteredGeneration
	plannedRuntime     VerifiedRuntimeArtifact
	authority          *VerifiedLineageSupersessionAuthority
	receipt            *verifiedHistoricalSupersessionReceipt
	reservedFrame      LineageIndexFrame
	headerFrame        EvidenceFrame
	reservedFrameBytes []byte
	activationHeader   ownedActivationHeader
	headerBytes        []byte
	headerBytesHash    [32]byte
	journal            Digest
	journalCount       uint64
	fsIndex            evidencefs.AdmissionTransitionResult
	fsJournal          evidencefs.AdmissionJournalTransitionResult
	priorCanonical     [32]byte
	consumed           *atomic.Bool
	binding            *historicalSuccessorHeaderBinding
}

type historicalSuccessorHeaderBinding struct {
	permit           *HistoricalSuccessorHeaderDurablePermit
	candidateBinding *verifiedEvidenceRunBinding
	inventory        *evidencefs.AdmissionInventory
	mutation         *evidencefs.AdmissionMutationToken
	source           *verifiedAdmissionRegisteredGeneration
	planned          *verifiedAdmissionRegisteredGeneration
	authority        *VerifiedLineageSupersessionAuthority
	receipt          *verifiedHistoricalSupersessionReceipt
	canonical        [32]byte
}

var historicalSuccessorHeaderRegistry sync.Map

type HistoricalSuccessorGenerationActivationTransitionResult struct {
	outcome                evidencefs.AdmissionTransitionOutcome
	next                   *HistoricalSuccessorGenerationReadyPermit
	candidateDigest        [32]byte
	candidateSequence      uint64
	candidateRevision      uint64
	previousRevision       uint64
	activationRecordDigest Digest
	reservedRecordDigest   Digest
	headerRecordDigest     Digest
}

func (r HistoricalSuccessorGenerationActivationTransitionResult) Outcome() evidencefs.AdmissionTransitionOutcome {
	return r.outcome
}
func (r HistoricalSuccessorGenerationActivationTransitionResult) Next() *HistoricalSuccessorGenerationReadyPermit {
	return r.next
}
func (r HistoricalSuccessorGenerationActivationTransitionResult) CandidateKind() string {
	return "generation_activated"
}
func (r HistoricalSuccessorGenerationActivationTransitionResult) CandidateDigest() [32]byte {
	return r.candidateDigest
}
func (r HistoricalSuccessorGenerationActivationTransitionResult) CandidateSequence() uint64 {
	return r.candidateSequence
}
func (r HistoricalSuccessorGenerationActivationTransitionResult) CandidateRevision() uint64 {
	return r.candidateRevision
}
func (r HistoricalSuccessorGenerationActivationTransitionResult) PreviousRevision() uint64 {
	return r.previousRevision
}
func (r HistoricalSuccessorGenerationActivationTransitionResult) ActivationRecordDigest() Digest {
	return r.activationRecordDigest
}
func (r HistoricalSuccessorGenerationActivationTransitionResult) ReservedRecordDigest() Digest {
	return r.reservedRecordDigest
}
func (r HistoricalSuccessorGenerationActivationTransitionResult) HeaderRecordDigest() Digest {
	return r.headerRecordDigest
}

// HistoricalSuccessorGenerationReadyPermit proves the crash-recovered B
// reservation, exact segment-0 header, and activation are all durable. It is
// still non-runnable; the only next operation is lock handoff.
type HistoricalSuccessorGenerationReadyPermit struct {
	self               *HistoricalSuccessorGenerationReadyPermit
	owner              *recoveryVerifierOwner
	candidateBinding   *verifiedEvidenceRunBinding
	inventory          *evidencefs.AdmissionInventory
	mutation           *evidencefs.AdmissionMutationToken
	revision           uint64
	target, fullSet    [32]byte
	indexRecords       uint64
	indexTail          Digest
	indexDigest        [32]byte
	indexSize          uint64
	source             *verifiedAdmissionRegisteredGeneration
	planned            *verifiedAdmissionRegisteredGeneration
	plannedRuntime     VerifiedRuntimeArtifact
	authority          *VerifiedLineageSupersessionAuthority
	receipt            *verifiedHistoricalSupersessionReceipt
	reservedFrame      LineageIndexFrame
	headerFrame        EvidenceFrame
	activatedFrame     LineageIndexFrame
	reservedFrameBytes []byte
	activatedBytes     []byte
	activationHeader   ownedActivationHeader
	headerBytes        []byte
	headerBytesHash    [32]byte
	journal            Digest
	journalCount       uint64
	fsIndexReservation evidencefs.AdmissionTransitionResult
	fsJournal          evidencefs.AdmissionJournalTransitionResult
	fsIndexActivation  evidencefs.AdmissionTransitionResult
	priorCanonical     [32]byte
	consumed           *atomic.Bool
	binding            *historicalSuccessorGenerationReadyBinding
}

type historicalSuccessorGenerationReadyBinding struct {
	permit           *HistoricalSuccessorGenerationReadyPermit
	candidateBinding *verifiedEvidenceRunBinding
	inventory        *evidencefs.AdmissionInventory
	mutation         *evidencefs.AdmissionMutationToken
	source           *verifiedAdmissionRegisteredGeneration
	planned          *verifiedAdmissionRegisteredGeneration
	authority        *VerifiedLineageSupersessionAuthority
	receipt          *verifiedHistoricalSupersessionReceipt
	canonical        [32]byte
}

var historicalSuccessorGenerationReadyRegistry sync.Map

func bindRegisteredActivationHeader(generation generationIdentity, reserved GenerationReserved, runtime VerifiedContentReceipt, recovery VerifiedDecisionRecoveryReceipt) (ownedActivationHeader, error) {
	return bindRegisteredActivationHeaderForOperation("historical-successor-header", generation, reserved, runtime, recovery)
}

func bindRegisteredActivationHeaderForOperation(operation string, generation generationIdentity, reserved GenerationReserved, runtime VerifiedContentReceipt, recovery VerifiedDecisionRecoveryReceipt) (ownedActivationHeader, error) {
	header := reserved.PlannedSegment0Header
	if operation == "" || reserved.Validate() != nil || !sameGenerationHeader(generation, header) || !validRegisteredRuntimeReceipt(runtime, generation.owner, header.OuterArtifactDigest, header.OuterArtifactSizeBytes) || !validRegisteredDecisionRecoveryReceipt(recovery, generation.owner, header.DecisionRecoveryArtifactSHA256, header.DecisionRecoveryArtifactSizeBytes) || !registeredReceiptsSameStore(runtime, recovery) {
		return ownedActivationHeader{}, fail(CodeEvidenceRecoveryRequired, operation, "registered reservation receipts are unavailable or mismatched", nil)
	}
	return ownedActivationHeader{header: cloneProjectionValue(header), generation: generation, reserved: cloneProjectionValue(reserved)}, nil
}

func (p *HistoricalSuccessorReservedDurablePermit) CreateGenerationHeader(ctx context.Context, candidate OwnedCurrentCandidate) (HistoricalSuccessorGenerationHeaderTransitionResult, error) {
	pre := HistoricalSuccessorGenerationHeaderTransitionResult{outcome: evidencefs.AdmissionTransitionPreMutationFailure, candidateSequence: 2}
	if !validHistoricalSuccessorReservedPermit(p, candidate) {
		return pre, fail(CodeEvidenceRecoveryRequired, "historical-successor-header", "historical successor reservation authority is unavailable", nil)
	}
	if err := contextAdmissionError(ctx); err != nil {
		return pre, err
	}
	reserved := cloneProjectionValue(*p.reservedFrame.Record.Reserved)
	activationHeader, bindErr := bindRegisteredActivationHeader(p.planned.descriptor.identity, reserved, p.planned.runtimeReceipt, p.planned.recoveryReceipt)
	headerFrame, headerBytes, encodeErr := encodeAdmissionActivationHeader(activationHeader)
	if bindErr != nil || encodeErr != nil || !canonicalEqual(headerFrame, p.headerFrame) || headerFrame.RecordDigest != reserved.ExpectedSegment0HeaderDigest {
		return pre, fail(CodeEvidenceRecoveryRequired, "historical-successor-header", "historical successor header differs from registered receipts", nil)
	}
	headerBytes = append([]byte(nil), headerBytes...)
	pre.previousRevision = p.revision
	pre.candidateRevision = p.revision + 1
	pre.journal = reserved.JournalIdentityDigest
	pre.headerRecordDigest = headerFrame.RecordDigest
	pre.headerBytesDigest = sha256.Sum256(headerBytes)
	pre.headerSize = uint64(len(headerBytes))
	if pre.journal.Validate() != nil || pre.headerRecordDigest.Validate() != nil || pre.headerBytesDigest == ([32]byte{}) || pre.headerSize == 0 {
		return pre, fail(CodeEvidenceRecoveryRequired, "historical-successor-header", "historical successor header identity is invalid", nil)
	}
	if _, err := readSuccessorInventoryIndex(ctx, p.inventory, p.target, p.indexRecords, p.indexTail, p.indexDigest, p.indexSize, "historical-successor-header-index"); err != nil {
		return pre, err
	}
	journalCount, err := validateSuccessorJournalInventory(ctx, p.inventory, p.target, pre.journal, nil, 0, false, "historical-successor-header-prefix")
	if err != nil {
		return pre, err
	}
	if !validHistoricalSuccessorReservedPermit(p, candidate) || !canonicalEqual(reserved, *p.reservedFrame.Record.Reserved) {
		return pre, fail(CodeEvidenceRecoveryRequired, "historical-successor-header", "historical successor reservation changed before header creation", nil)
	}
	if !p.consumed.CompareAndSwap(false, true) {
		return pre, fail(CodeEvidenceRecoveryRequired, "historical-successor-header", "historical successor reservation authority was already consumed", nil)
	}
	fsResult, transitionErr := p.mutation.CreateGenerationHeader(ctx, p.inventory, digestRaw(pre.journal), headerBytes)
	result := HistoricalSuccessorGenerationHeaderTransitionResult{
		outcome: fsResult.Outcome(), candidateDigest: fsResult.CandidateDigest(), candidateSequence: 2,
		candidateRevision: fsResult.CandidateRevision(), previousRevision: fsResult.PreviousRevision(), journal: pre.journal,
		headerRecordDigest: pre.headerRecordDigest, headerBytesDigest: pre.headerBytesDigest, headerSize: pre.headerSize,
	}
	if fsResult.Outcome() != evidencefs.AdmissionTransitionDurable {
		revokeHistoricalSuccessorReservedPermit(p)
		return result, mapAdmissionMutationError(transitionErr, "historical-successor-header")
	}
	postFailure := func(suffix string) (HistoricalSuccessorGenerationHeaderTransitionResult, error) {
		_ = fsResult.Invalidate()
		revokeHistoricalSuccessorReservedPermit(p)
		result.outcome = evidencefs.AdmissionTransitionUnknown
		result.next = nil
		return result, admissionPostMutationFailure("historical-successor-header" + suffix)
	}
	if transitionErr != nil || fsResult.CandidateKind() != "generation_header" || fsResult.CandidateSequence() != 0 || fsResult.CandidateDigest() == ([32]byte{}) || fsResult.PreviousRevision() != pre.previousRevision || fsResult.CandidateRevision() != pre.candidateRevision || fsResult.Journal() != digestRaw(pre.journal) || fsResult.HeaderDigest() != pre.headerBytesDigest || fsResult.HeaderSize() != pre.headerSize || fsResult.Inventory() == nil || !fsResult.ValidFor(fsResult.Inventory()) {
		return postFailure("")
	}
	nextInventory := fsResult.Inventory()
	revision, revisionErr := nextInventory.Revision()
	target, targetErr := nextInventory.Target()
	fullSet, fullSetErr := nextInventory.FullSetDigest()
	if revisionErr != nil || revision != pre.candidateRevision {
		return postFailure("-revision")
	}
	if targetErr != nil || target != p.target {
		return postFailure("-target")
	}
	if fullSetErr != nil || fullSet == ([32]byte{}) || fullSet == p.fullSet {
		return postFailure("-full-set")
	}
	index, indexErr := readSuccessorInventoryIndex(ctx, nextInventory, target, p.indexRecords, p.indexTail, p.indexDigest, p.indexSize, "historical-successor-header-index-after")
	if indexErr != nil || index.digest != p.indexDigest {
		return postFailure("-index")
	}
	nextJournalCount, journalErr := validateSuccessorJournalInventory(ctx, nextInventory, target, pre.journal, headerBytes, journalCount+1, true, "historical-successor-header-inventory")
	if journalErr != nil || nextJournalCount != journalCount+1 {
		return postFailure("-inventory")
	}
	nextToken, tokenErr := nextInventory.MutationToken()
	if tokenErr != nil || !nextToken.ValidFor(nextInventory) {
		return postFailure("-token")
	}
	next := &HistoricalSuccessorHeaderDurablePermit{
		owner: p.owner, candidateBinding: candidate.binding, inventory: nextInventory, mutation: nextToken,
		revision: revision, target: target, fullSet: fullSet, indexRecords: p.indexRecords, indexTail: p.indexTail, indexDigest: p.indexDigest, indexSize: p.indexSize,
		source: p.source, planned: p.planned,
		plannedRuntime: VerifiedRuntimeArtifact{owner: p.plannedRuntime.owner, bytes: append([]byte(nil), p.plannedRuntime.bytes...), digest: p.plannedRuntime.digest, sizeBytes: p.plannedRuntime.sizeBytes},
		authority:      p.authority, receipt: p.receipt,
		reservedFrame: cloneProjectionValue(p.reservedFrame), headerFrame: cloneProjectionValue(headerFrame), reservedFrameBytes: append([]byte(nil), p.reservedFrameBytes...),
		activationHeader: cloneSuccessorActivationHeader(activationHeader), headerBytes: headerBytes, headerBytesHash: pre.headerBytesDigest,
		journal: pre.journal, journalCount: nextJournalCount, fsIndex: p.fsIndex, fsJournal: fsResult,
		priorCanonical: p.binding.canonical, consumed: &atomic.Bool{},
	}
	next.self = next
	next.binding = &historicalSuccessorHeaderBinding{
		permit: next, candidateBinding: candidate.binding, inventory: nextInventory, mutation: nextToken,
		source: p.source, planned: p.planned, authority: p.authority, receipt: p.receipt,
	}
	next.binding.canonical = historicalSuccessorHeaderDigest(next)
	historicalSuccessorHeaderRegistry.Store(next.binding, next.binding.canonical)
	if !validHistoricalSuccessorHeaderPermit(next, candidate) {
		historicalSuccessorHeaderRegistry.Delete(next.binding)
		return postFailure("-seal")
	}
	historicalSuccessorReservedRegistry.Delete(p.binding)
	result.next = next
	return result, nil
}

func (p *HistoricalSuccessorHeaderDurablePermit) AppendGenerationActivated(ctx context.Context, candidate OwnedCurrentCandidate) (HistoricalSuccessorGenerationActivationTransitionResult, error) {
	pre := HistoricalSuccessorGenerationActivationTransitionResult{outcome: evidencefs.AdmissionTransitionPreMutationFailure, candidateSequence: 3}
	if !validHistoricalSuccessorHeaderPermit(p, candidate) {
		return pre, fail(CodeEvidenceRecoveryRequired, "historical-successor-activate", "historical successor header authority is unavailable", nil)
	}
	if err := contextAdmissionError(ctx); err != nil {
		return pre, err
	}
	activatedFrame, activatedBytes, err := buildSuccessorActivatedFrame(p.reservedFrame, p.headerFrame)
	if err != nil {
		return pre, fail(CodeEvidenceRecoveryRequired, "historical-successor-activate", "historical successor activation frame cannot be constructed", nil)
	}
	activatedBytes = append([]byte(nil), activatedBytes...)
	pre.previousRevision = p.revision
	pre.candidateRevision = p.revision + 1
	pre.candidateDigest = sha256.Sum256(activatedBytes)
	pre.activationRecordDigest = activatedFrame.RecordDigest
	pre.reservedRecordDigest = p.reservedFrame.RecordDigest
	pre.headerRecordDigest = p.headerFrame.RecordDigest
	prefix, err := readSuccessorInventoryIndex(ctx, p.inventory, p.target, p.indexRecords, p.indexTail, p.indexDigest, p.indexSize, "historical-successor-activate-prefix")
	if err != nil {
		return pre, err
	}
	journalCount, err := validateSuccessorJournalInventory(ctx, p.inventory, p.target, p.journal, p.headerBytes, p.journalCount, true, "historical-successor-activate-journal")
	if err != nil || journalCount != p.journalCount {
		return pre, err
	}
	if activatedFrame.Sequence != uint64(len(prefix.frames)) || activatedFrame.PreviousRecordDigest == nil || *activatedFrame.PreviousRecordDigest != prefix.tail || !bytes.Equal(activatedBytes, mustEncodeLineageFrame(activatedFrame)) || !validHistoricalSuccessorHeaderPermit(p, candidate) {
		return pre, fail(CodeEvidenceRecoveryRequired, "historical-successor-activate", "historical successor header changed before activation", nil)
	}
	if !p.consumed.CompareAndSwap(false, true) {
		return pre, fail(CodeEvidenceRecoveryRequired, "historical-successor-activate", "historical successor header authority was already consumed", nil)
	}
	fsResult, transitionErr := p.mutation.AppendTargetIndex(ctx, p.inventory, activatedBytes)
	result := HistoricalSuccessorGenerationActivationTransitionResult{
		outcome: fsResult.Outcome(), candidateDigest: fsResult.CandidateDigest(), candidateSequence: 3,
		candidateRevision: fsResult.CandidateRevision(), previousRevision: fsResult.PreviousRevision(),
		activationRecordDigest: pre.activationRecordDigest, reservedRecordDigest: pre.reservedRecordDigest, headerRecordDigest: pre.headerRecordDigest,
	}
	if fsResult.Outcome() != evidencefs.AdmissionTransitionDurable {
		revokeHistoricalSuccessorHeaderPermit(p)
		return result, mapAdmissionMutationError(transitionErr, "historical-successor-activate")
	}
	postFailure := func(suffix string) (HistoricalSuccessorGenerationActivationTransitionResult, error) {
		_ = fsResult.Invalidate()
		revokeHistoricalSuccessorHeaderPermit(p)
		result.outcome = evidencefs.AdmissionTransitionUnknown
		result.next = nil
		return result, admissionPostMutationFailure("historical-successor-activate" + suffix)
	}
	if transitionErr != nil || fsResult.CandidateKind() != "target_index_append" || fsResult.CandidateSequence() != 0 || fsResult.CandidateDigest() != pre.candidateDigest || fsResult.PreviousRevision() != pre.previousRevision || fsResult.CandidateRevision() != pre.candidateRevision || fsResult.Inventory() == nil {
		return postFailure("")
	}
	nextInventory := fsResult.Inventory()
	revision, revisionErr := nextInventory.Revision()
	target, targetErr := nextInventory.Target()
	fullSet, fullSetErr := nextInventory.FullSetDigest()
	if revisionErr != nil || revision != pre.candidateRevision {
		return postFailure("-revision")
	}
	if targetErr != nil || target != p.target {
		return postFailure("-target")
	}
	if fullSetErr != nil || fullSet == ([32]byte{}) || fullSet == p.fullSet {
		return postFailure("-full-set")
	}
	verified, verifyErr := validateSuccessorIndexAppend(ctx, nextInventory, target, prefix.raw, activatedBytes, p.indexRecords+1, pre.activationRecordDigest)
	if verifyErr != nil {
		return postFailure("-index")
	}
	nextJournalCount, journalErr := validateSuccessorJournalInventory(ctx, nextInventory, target, p.journal, p.headerBytes, p.journalCount, true, "historical-successor-activate-inventory")
	if journalErr != nil || nextJournalCount != p.journalCount {
		return postFailure("-inventory")
	}
	nextToken, tokenErr := nextInventory.MutationToken()
	if tokenErr != nil || !nextToken.ValidFor(nextInventory) {
		return postFailure("-token")
	}
	next := &HistoricalSuccessorGenerationReadyPermit{
		owner: p.owner, candidateBinding: candidate.binding, inventory: nextInventory, mutation: nextToken,
		revision: revision, target: target, fullSet: fullSet, indexRecords: uint64(len(verified.frames)), indexTail: pre.activationRecordDigest, indexDigest: verified.digest, indexSize: verified.size,
		source: p.source, planned: p.planned,
		plannedRuntime: VerifiedRuntimeArtifact{owner: p.plannedRuntime.owner, bytes: append([]byte(nil), p.plannedRuntime.bytes...), digest: p.plannedRuntime.digest, sizeBytes: p.plannedRuntime.sizeBytes},
		authority:      p.authority, receipt: p.receipt,
		reservedFrame: cloneProjectionValue(p.reservedFrame), headerFrame: cloneProjectionValue(p.headerFrame), activatedFrame: cloneProjectionValue(activatedFrame),
		reservedFrameBytes: append([]byte(nil), p.reservedFrameBytes...), activatedBytes: activatedBytes,
		activationHeader: cloneSuccessorActivationHeader(p.activationHeader), headerBytes: append([]byte(nil), p.headerBytes...), headerBytesHash: p.headerBytesHash,
		journal: p.journal, journalCount: p.journalCount,
		fsIndexReservation: p.fsIndex, fsJournal: p.fsJournal, fsIndexActivation: fsResult,
		priorCanonical: p.binding.canonical, consumed: &atomic.Bool{},
	}
	next.self = next
	next.binding = &historicalSuccessorGenerationReadyBinding{
		permit: next, candidateBinding: candidate.binding, inventory: nextInventory, mutation: nextToken,
		source: p.source, planned: p.planned, authority: p.authority, receipt: p.receipt,
	}
	next.binding.canonical = historicalSuccessorGenerationReadyDigest(next)
	historicalSuccessorGenerationReadyRegistry.Store(next.binding, next.binding.canonical)
	if !validHistoricalSuccessorGenerationReadyPermit(next, candidate) {
		historicalSuccessorGenerationReadyRegistry.Delete(next.binding)
		return postFailure("-seal")
	}
	historicalSuccessorHeaderRegistry.Delete(p.binding)
	result.next = next
	return result, nil
}

func validHistoricalSuccessorRegisteredInputs(target [32]byte, source, planned *verifiedAdmissionRegisteredGeneration, plannedRuntime VerifiedRuntimeArtifact, authority *VerifiedLineageSupersessionAuthority, receipt *verifiedHistoricalSupersessionReceipt, candidate OwnedCurrentCandidate) bool {
	return validOwnedCurrentCandidate(candidate) && target == digestRaw(candidate.verifiedRun.executionLineageDigest) && source != nil && planned != nil && source.replay != nil && source.replay.supersessionDebited && planned.replay == nil && validVerifiedAdmissionRegisteredGeneration(source, candidate.verifiedRun.currentDecision) && validVerifiedAdmissionRegisteredGeneration(planned, candidate.verifiedRun.currentDecision) && validHistoricalSupersessionAuthority(source, authority, candidate) && receipt != nil && receipt.owner == candidate.verifiedRun.currentDecision.owner && receipt.authorityDigest == authority.digest && receipt.consumed.Load() && plannedRuntime.owner == candidate.owner && plannedRuntime.digest == planned.descriptor.header.OuterArtifactDigest && plannedRuntime.sizeBytes == planned.descriptor.header.OuterArtifactSizeBytes && uint64(len(plannedRuntime.bytes)) == plannedRuntime.sizeBytes && DigestBytes(plannedRuntime.bytes) == plannedRuntime.digest && validRegisteredRuntimeReceipt(receipt.runtimeReceipt, candidate.owner, plannedRuntime.digest, plannedRuntime.sizeBytes) && receipt.runtimeReceipt.registeredPublication.SameObject(planned.runtimeReceipt.registeredPublication) && validRegisteredDecisionRecoveryReceipt(receipt.recoveryReceipt, candidate.owner, planned.recoveryArtifact.digest, planned.recoveryArtifact.sizeBytes) && receipt.recoveryReceipt.registeredPublication.SameObject(planned.recoveryReceipt.registeredPublication)
}

func historicalSuccessorHeaderDigest(permit *HistoricalSuccessorHeaderDurablePermit) [32]byte {
	if permit == nil || permit.self != permit || permit.candidateBinding == nil || permit.mutation == nil || permit.source == nil || permit.planned == nil || permit.authority == nil || permit.receipt == nil || permit.consumed == nil || permit.priorCanonical == ([32]byte{}) || permit.reservedFrame.Validate() != nil || permit.headerFrame.Validate() != nil || permit.reservedFrame.Record.Reserved == nil || permit.headerFrame.Record.Header == nil || permit.journal.Validate() != nil || len(permit.headerBytes) == 0 || permit.headerBytesHash != sha256.Sum256(permit.headerBytes) {
		return [32]byte{}
	}
	reservedBytes, reservedErr := EncodeCanonicalLineageFrame(permit.reservedFrame)
	headerBytes, headerErr := EncodeCanonicalEvidenceFrame(permit.headerFrame)
	if reservedErr != nil || headerErr != nil || !bytes.Equal(reservedBytes, permit.reservedFrameBytes) || !bytes.Equal(headerBytes, permit.headerBytes) {
		return [32]byte{}
	}
	h := sha256.New()
	h.Write([]byte("cloud-agents-platform-historical-successor-header/v1\x00"))
	h.Write(permit.priorCanonical[:])
	h.Write(permit.candidateBinding.canonical[:])
	h.Write(permit.target[:])
	h.Write(permit.fullSet[:])
	h.Write(permit.indexDigest[:])
	h.Write(permit.source.canonical[:])
	h.Write(permit.planned.canonical[:])
	h.Write(permit.headerBytesHash[:])
	var encoded [8]byte
	for _, value := range []uint64{permit.revision, permit.indexRecords, permit.indexSize, permit.journalCount, permit.plannedRuntime.sizeBytes} {
		binary.BigEndian.PutUint64(encoded[:], value)
		h.Write(encoded[:])
	}
	for _, digest := range []Digest{permit.indexTail, permit.journal, permit.plannedRuntime.digest, permit.authority.digest, permit.receipt.authorityDigest} {
		writeAdmissionString(h, digest.String())
	}
	h.Write(permit.reservedFrameBytes)
	h.Write(permit.headerBytes)
	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result
}

func validHistoricalSuccessorHeaderPermit(permit *HistoricalSuccessorHeaderDurablePermit, candidate OwnedCurrentCandidate) bool {
	if permit == nil || permit.self != permit || permit.binding == nil || permit.binding.permit != permit || permit.owner == nil || permit.owner != candidate.verifiedRun.currentDecision.owner || permit.candidateBinding != candidate.binding || permit.binding.candidateBinding != candidate.binding || permit.inventory == nil || permit.binding.inventory != permit.inventory || permit.mutation == nil || permit.binding.mutation != permit.mutation || permit.source == nil || permit.binding.source != permit.source || permit.planned == nil || permit.binding.planned != permit.planned || permit.authority == nil || permit.binding.authority != permit.authority || permit.receipt == nil || permit.binding.receipt != permit.receipt || permit.consumed == nil || permit.consumed.Load() || !permit.mutation.ValidFor(permit.inventory) || !validHistoricalSuccessorRegisteredInputs(permit.target, permit.source, permit.planned, permit.plannedRuntime, permit.authority, permit.receipt, candidate) || permit.reservedFrame.Record.Reserved == nil || permit.indexRecords != permit.reservedFrame.Sequence+1 || permit.indexTail != permit.reservedFrame.RecordDigest || !sameGenerationHeader(permit.planned.descriptor.identity, permit.reservedFrame.Record.Reserved.PlannedSegment0Header) || permit.fsIndex.Outcome() != evidencefs.AdmissionTransitionDurable || permit.fsIndex.CandidateKind() != "target_index_append" || permit.fsIndex.CandidateDigest() != sha256.Sum256(permit.reservedFrameBytes) || permit.fsJournal.Outcome() != evidencefs.AdmissionTransitionDurable || permit.fsJournal.Inventory() != permit.inventory || !permit.fsJournal.ValidFor(permit.inventory) || permit.fsJournal.CandidateKind() != "generation_header" || permit.fsJournal.Journal() != digestRaw(permit.journal) || permit.fsJournal.HeaderDigest() != permit.headerBytesHash || permit.fsJournal.HeaderSize() != uint64(len(permit.headerBytes)) || permit.fsJournal.CandidateRevision() != permit.revision || permit.fsJournal.PreviousRevision()+1 != permit.revision || permit.fsIndex.CandidateRevision() != permit.fsJournal.PreviousRevision() || permit.binding.canonical == ([32]byte{}) || permit.binding.canonical != historicalSuccessorHeaderDigest(permit) {
		return false
	}
	wantHeader, bindErr := bindRegisteredActivationHeader(permit.planned.descriptor.identity, *permit.reservedFrame.Record.Reserved, permit.planned.runtimeReceipt, permit.planned.recoveryReceipt)
	wantFrame, wantBytes, encodeErr := encodeAdmissionActivationHeader(wantHeader)
	if bindErr != nil || encodeErr != nil || !canonicalEqual(permit.activationHeader.header, wantHeader.header) || !sameGenerationIdentity(permit.activationHeader.generation, wantHeader.generation) || !canonicalEqual(permit.activationHeader.reserved, wantHeader.reserved) || !canonicalEqual(permit.headerFrame, wantFrame) || !bytes.Equal(permit.headerBytes, wantBytes) {
		return false
	}
	registered, ok := historicalSuccessorHeaderRegistry.Load(permit.binding)
	if !ok || registered != permit.binding.canonical {
		return false
	}
	return validHistoricalSuccessorInventory(permit.inventory, permit.target, permit.indexDigest, permit.indexSize, permit.journal, permit.headerBytesHash, uint64(len(permit.headerBytes)), permit.journalCount, permit.revision, permit.fullSet)
}

func historicalSuccessorGenerationReadyDigest(permit *HistoricalSuccessorGenerationReadyPermit) [32]byte {
	if permit == nil || permit.self != permit || permit.candidateBinding == nil || permit.mutation == nil || permit.source == nil || permit.planned == nil || permit.authority == nil || permit.receipt == nil || permit.consumed == nil || permit.priorCanonical == ([32]byte{}) || permit.reservedFrame.Validate() != nil || permit.headerFrame.Validate() != nil || permit.activatedFrame.Validate() != nil || len(permit.headerBytes) == 0 || len(permit.activatedBytes) == 0 || permit.headerBytesHash != sha256.Sum256(permit.headerBytes) {
		return [32]byte{}
	}
	wantActivated, wantBytes, err := buildSuccessorActivatedFrame(permit.reservedFrame, permit.headerFrame)
	reservedBytes, reservedErr := EncodeCanonicalLineageFrame(permit.reservedFrame)
	headerBytes, headerErr := EncodeCanonicalEvidenceFrame(permit.headerFrame)
	if err != nil || reservedErr != nil || headerErr != nil || !canonicalEqual(permit.activatedFrame, wantActivated) || !bytes.Equal(permit.activatedBytes, wantBytes) || !bytes.Equal(permit.reservedFrameBytes, reservedBytes) || !bytes.Equal(permit.headerBytes, headerBytes) {
		return [32]byte{}
	}
	h := sha256.New()
	h.Write([]byte("cloud-agents-platform-historical-successor-generation-ready/v1\x00"))
	h.Write(permit.priorCanonical[:])
	h.Write(permit.candidateBinding.canonical[:])
	h.Write(permit.target[:])
	h.Write(permit.fullSet[:])
	h.Write(permit.indexDigest[:])
	h.Write(permit.source.canonical[:])
	h.Write(permit.planned.canonical[:])
	h.Write(permit.headerBytesHash[:])
	var encoded [8]byte
	for _, value := range []uint64{permit.revision, permit.indexRecords, permit.indexSize, permit.journalCount, permit.plannedRuntime.sizeBytes} {
		binary.BigEndian.PutUint64(encoded[:], value)
		h.Write(encoded[:])
	}
	for _, digest := range []Digest{permit.indexTail, permit.journal, permit.plannedRuntime.digest, permit.authority.digest, permit.receipt.authorityDigest, permit.activatedFrame.RecordDigest} {
		writeAdmissionString(h, digest.String())
	}
	h.Write(permit.reservedFrameBytes)
	h.Write(permit.headerBytes)
	h.Write(permit.activatedBytes)
	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result
}

func validHistoricalSuccessorGenerationReadyPermit(permit *HistoricalSuccessorGenerationReadyPermit, candidate OwnedCurrentCandidate) bool {
	if permit == nil || permit.self != permit || permit.binding == nil || permit.binding.permit != permit || permit.owner == nil || permit.owner != candidate.verifiedRun.currentDecision.owner || permit.candidateBinding != candidate.binding || permit.binding.candidateBinding != candidate.binding || permit.inventory == nil || permit.binding.inventory != permit.inventory || permit.mutation == nil || permit.binding.mutation != permit.mutation || permit.source == nil || permit.binding.source != permit.source || permit.planned == nil || permit.binding.planned != permit.planned || permit.authority == nil || permit.binding.authority != permit.authority || permit.receipt == nil || permit.binding.receipt != permit.receipt || permit.consumed == nil || permit.consumed.Load() || !permit.mutation.ValidFor(permit.inventory) || !validHistoricalSuccessorRegisteredInputs(permit.target, permit.source, permit.planned, permit.plannedRuntime, permit.authority, permit.receipt, candidate) || permit.reservedFrame.Record.Reserved == nil || permit.indexRecords != permit.activatedFrame.Sequence+1 || permit.indexTail != permit.activatedFrame.RecordDigest || !sameGenerationHeader(permit.planned.descriptor.identity, permit.reservedFrame.Record.Reserved.PlannedSegment0Header) || permit.fsIndexReservation.Outcome() != evidencefs.AdmissionTransitionDurable || permit.fsIndexReservation.CandidateKind() != "target_index_append" || permit.fsIndexReservation.CandidateDigest() != sha256.Sum256(permit.reservedFrameBytes) || permit.fsJournal.Outcome() != evidencefs.AdmissionTransitionDurable || permit.fsJournal.CandidateKind() != "generation_header" || permit.fsJournal.Journal() != digestRaw(permit.journal) || permit.fsJournal.HeaderDigest() != permit.headerBytesHash || permit.fsJournal.HeaderSize() != uint64(len(permit.headerBytes)) || permit.fsIndexActivation.Outcome() != evidencefs.AdmissionTransitionDurable || permit.fsIndexActivation.Inventory() != permit.inventory || permit.fsIndexActivation.CandidateKind() != "target_index_append" || permit.fsIndexActivation.CandidateDigest() != sha256.Sum256(permit.activatedBytes) || permit.fsIndexActivation.CandidateRevision() != permit.revision || permit.fsIndexActivation.PreviousRevision()+1 != permit.revision || permit.fsJournal.CandidateRevision() != permit.fsIndexActivation.PreviousRevision() || permit.binding.canonical == ([32]byte{}) || permit.binding.canonical != historicalSuccessorGenerationReadyDigest(permit) {
		return false
	}
	registered, ok := historicalSuccessorGenerationReadyRegistry.Load(permit.binding)
	if !ok || registered != permit.binding.canonical {
		return false
	}
	return validHistoricalSuccessorInventory(permit.inventory, permit.target, permit.indexDigest, permit.indexSize, permit.journal, permit.headerBytesHash, uint64(len(permit.headerBytes)), permit.journalCount, permit.revision, permit.fullSet)
}

func validHistoricalSuccessorInventory(inventory *evidencefs.AdmissionInventory, target [32]byte, indexDigest [32]byte, indexSize uint64, journal Digest, headerDigest [32]byte, headerSize, journalCount, revision uint64, fullSet [32]byte) bool {
	if inventory == nil || target == ([32]byte{}) || indexDigest == ([32]byte{}) || indexSize == 0 || journal.Validate() != nil || headerDigest == ([32]byte{}) || headerSize == 0 || journalCount == 0 {
		return false
	}
	lineage, err := inventory.Lineage(target)
	if err != nil {
		return false
	}
	index, err := lineage.Index()
	if err != nil {
		return false
	}
	digest, digestErr := index.Digest()
	size, sizeErr := index.Size()
	journals, journalsErr := lineage.Journals()
	if digestErr != nil || sizeErr != nil || journalsErr != nil || digest != indexDigest || size != indexSize || uint64(len(journals)) != journalCount {
		return false
	}
	found := false
	for _, value := range journals {
		id, idErr := value.ID()
		if idErr != nil {
			return false
		}
		if id != digestRaw(journal) {
			continue
		}
		if found {
			return false
		}
		found = true
		segments, segmentErr := value.Segments()
		if segmentErr != nil || len(segments) != 1 {
			return false
		}
		ordinal, ordinalErr := segments[0].Ordinal()
		segmentDigest, segmentDigestErr := segments[0].Digest()
		segmentSize, segmentSizeErr := segments[0].Size()
		if ordinalErr != nil || segmentDigestErr != nil || segmentSizeErr != nil || ordinal != 0 || segmentDigest != headerDigest || segmentSize != headerSize {
			return false
		}
	}
	actualRevision, revisionErr := inventory.Revision()
	actualTarget, targetErr := inventory.Target()
	actualFullSet, fullSetErr := inventory.FullSetDigest()
	return found && revisionErr == nil && actualRevision == revision && targetErr == nil && actualTarget == target && fullSetErr == nil && actualFullSet == fullSet
}

func revokeHistoricalSuccessorHeaderPermit(permit *HistoricalSuccessorHeaderDurablePermit) {
	if permit == nil {
		return
	}
	if permit.binding != nil {
		historicalSuccessorHeaderRegistry.Delete(permit.binding)
	}
	if permit.fsJournal.Outcome() == evidencefs.AdmissionTransitionDurable {
		_ = permit.fsJournal.Invalidate()
	}
	revokeVerifiedAdmissionRegisteredGeneration(permit.source)
	revokeVerifiedAdmissionRegisteredGeneration(permit.planned)
}

func revokeHistoricalSuccessorGenerationReadyPermit(permit *HistoricalSuccessorGenerationReadyPermit) {
	if permit == nil {
		return
	}
	if permit.binding != nil {
		historicalSuccessorGenerationReadyRegistry.Delete(permit.binding)
	}
	if permit.fsIndexActivation.Outcome() == evidencefs.AdmissionTransitionDurable {
		_ = permit.fsIndexActivation.Invalidate()
	}
	revokeVerifiedAdmissionRegisteredGeneration(permit.source)
	revokeVerifiedAdmissionRegisteredGeneration(permit.planned)
}
