package migration

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"sync"
	"sync/atomic"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/evidencefs"
)

// HistoricalSupersessionAdjacentReserveReady is recovery-only authority for
// the one crash window where Superseded(A -> B) is durable but its byte-exact
// nested GenerationReserved(B) is not. It cannot authorize any other index,
// journal, runner, database, or publication operation.
type HistoricalSupersessionAdjacentReserveReady struct {
	self                 *HistoricalSupersessionAdjacentReserveReady
	owner                *recoveryVerifierOwner
	candidateBinding     *verifiedEvidenceRunBinding
	inventory            *evidencefs.AdmissionInventory
	mutation             *evidencefs.AdmissionMutationToken
	revision             uint64
	target, fullSet      [32]byte
	transcriptCanonical  [32]byte
	indexRecords         uint64
	indexTail            Digest
	indexDigest          [32]byte
	indexSize            uint64
	rootFacts            rootQuotaUsageFacts
	quotaAdmission       rootQuotaAdmission
	source               *verifiedAdmissionRegisteredGeneration
	planned              *verifiedAdmissionRegisteredGeneration
	plannedRuntime       VerifiedRuntimeArtifact
	authority            *VerifiedLineageSupersessionAuthority
	receipt              *verifiedHistoricalSupersessionReceipt
	supersededFrame      LineageIndexFrame
	reservedFrame        LineageIndexFrame
	headerFrame          EvidenceFrame
	supersededFrameBytes []byte
	reservedFrameBytes   []byte
	consumed             *atomic.Bool
	binding              *historicalSupersessionAdjacentBinding
}

type historicalSupersessionAdjacentBinding struct {
	ready            *HistoricalSupersessionAdjacentReserveReady
	candidateBinding *verifiedEvidenceRunBinding
	inventory        *evidencefs.AdmissionInventory
	mutation         *evidencefs.AdmissionMutationToken
	source           *verifiedAdmissionRegisteredGeneration
	planned          *verifiedAdmissionRegisteredGeneration
	authority        *VerifiedLineageSupersessionAuthority
	receipt          *verifiedHistoricalSupersessionReceipt
	canonical        [32]byte
}

var historicalSupersessionAdjacentRegistry sync.Map

type HistoricalSupersessionReservationTransitionResult struct {
	outcome              evidencefs.AdmissionTransitionOutcome
	next                 *HistoricalSuccessorReservedDurablePermit
	candidateDigest      [32]byte
	candidateSequence    uint64
	candidateRevision    uint64
	previousRevision     uint64
	reservedRecordDigest Digest
}

func (r HistoricalSupersessionReservationTransitionResult) Outcome() evidencefs.AdmissionTransitionOutcome {
	return r.outcome
}
func (r HistoricalSupersessionReservationTransitionResult) Next() *HistoricalSuccessorReservedDurablePermit {
	return r.next
}
func (r HistoricalSupersessionReservationTransitionResult) CandidateKind() string {
	return "generation_reserved"
}
func (r HistoricalSupersessionReservationTransitionResult) CandidateDigest() [32]byte {
	return r.candidateDigest
}
func (r HistoricalSupersessionReservationTransitionResult) CandidateSequence() uint64 {
	return r.candidateSequence
}
func (r HistoricalSupersessionReservationTransitionResult) CandidateRevision() uint64 {
	return r.candidateRevision
}
func (r HistoricalSupersessionReservationTransitionResult) PreviousRevision() uint64 {
	return r.previousRevision
}
func (r HistoricalSupersessionReservationTransitionResult) ReservedRecordDigest() Digest {
	return r.reservedRecordDigest
}

// HistoricalSuccessorReservedDurablePermit proves that the missing adjacent B
// reservation is now durable. It is a distinct recovery state; it cannot be
// substituted for the live successor chain's reserved permit.
type HistoricalSuccessorReservedDurablePermit struct {
	self               *HistoricalSuccessorReservedDurablePermit
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
	readyCanonical     [32]byte
	fsIndex            evidencefs.AdmissionTransitionResult
	consumed           *atomic.Bool
	binding            *historicalSuccessorReservedBinding
}

type historicalSuccessorReservedBinding struct {
	permit           *HistoricalSuccessorReservedDurablePermit
	candidateBinding *verifiedEvidenceRunBinding
	inventory        *evidencefs.AdmissionInventory
	mutation         *evidencefs.AdmissionMutationToken
	source           *verifiedAdmissionRegisteredGeneration
	planned          *verifiedAdmissionRegisteredGeneration
	authority        *VerifiedLineageSupersessionAuthority
	receipt          *verifiedHistoricalSupersessionReceipt
	canonical        [32]byte
}

var historicalSuccessorReservedRegistry sync.Map

func buildHistoricalSupersessionFrames(lineage [32]byte, indexRecords uint64, indexTail Digest, source admissionReplayGeneration) (LineageIndexFrame, LineageIndexFrame, EvidenceFrame, []byte, []byte, error) {
	if lineage == ([32]byte{}) || indexRecords < 2 || indexRecords >= maxJSONInteger || indexTail.Validate() != nil || source.supersessionRecordDigest == nil || *source.supersessionRecordDigest != indexTail {
		return LineageIndexFrame{}, LineageIndexFrame{}, EvidenceFrame{}, nil, nil, admissionCorrupt("historical-supersession-frames", "stored supersession index boundary is incomplete", nil)
	}
	superseded, err := expandAdmissionGenerationSuperseded(lineage, source)
	if err != nil {
		return LineageIndexFrame{}, LineageIndexFrame{}, EvidenceFrame{}, nil, nil, err
	}
	var previous *Digest
	if superseded.OldCheckpointRecordDigest != nil {
		previous = cloneDigestPointer(superseded.OldCheckpointRecordDigest)
	} else {
		previous = cloneDigestPointer(superseded.OldActivationRecordDigest)
	}
	if previous == nil || previous.Validate() != nil {
		return LineageIndexFrame{}, LineageIndexFrame{}, EvidenceFrame{}, nil, nil, admissionCorrupt("historical-supersession-frames", "stored supersession predecessor is unavailable", nil)
	}
	supersededFrame := LineageIndexFrame{
		FormatVersion: LineageFrameFormat, Sequence: indexRecords - 1, PreviousRecordDigest: previous,
		RecordKind: LineageRecordGenerationSuperseded, Record: LineageIndexRecord{Superseded: &superseded},
	}
	supersededFrame.RecordDigest, err = supersededFrame.ComputeDigest()
	if err != nil || supersededFrame.RecordDigest != indexTail || supersededFrame.Validate() != nil {
		return LineageIndexFrame{}, LineageIndexFrame{}, EvidenceFrame{}, nil, nil, admissionCorrupt("historical-supersession-frames", "stored supersession frame is not byte-exact", err)
	}
	supersededBytes, err := EncodeCanonicalLineageFrame(supersededFrame)
	if err != nil {
		return LineageIndexFrame{}, LineageIndexFrame{}, EvidenceFrame{}, nil, nil, admissionCorrupt("historical-supersession-frames", "stored supersession frame cannot be encoded", err)
	}
	planned := cloneProjectionValue(*superseded.PlannedGenerationReserved)
	reservedFrame := LineageIndexFrame{
		FormatVersion: LineageFrameFormat, Sequence: indexRecords, PreviousRecordDigest: digestPointer(indexTail),
		RecordKind: LineageRecordGenerationReserved, Record: LineageIndexRecord{Reserved: &planned},
	}
	reservedFrame.RecordDigest, err = reservedFrame.ComputeDigest()
	if err != nil || reservedFrame.Validate() != nil {
		return LineageIndexFrame{}, LineageIndexFrame{}, EvidenceFrame{}, nil, nil, admissionCorrupt("historical-supersession-frames", "planned adjacent reservation frame is invalid", err)
	}
	reservedBytes, err := EncodeCanonicalLineageFrame(reservedFrame)
	if err != nil {
		return LineageIndexFrame{}, LineageIndexFrame{}, EvidenceFrame{}, nil, nil, admissionCorrupt("historical-supersession-frames", "planned adjacent reservation frame cannot be encoded", err)
	}
	header := cloneProjectionValue(planned.PlannedSegment0Header)
	headerFrame := EvidenceFrame{FormatVersion: EvidenceFrameFormat, Sequence: 0, RecordKind: EvidenceRecordHeader, Record: EvidenceRecord{Header: &header}}
	headerFrame.RecordDigest, err = headerFrame.ComputeDigest()
	if err != nil || headerFrame.RecordDigest != planned.ExpectedSegment0HeaderDigest || headerFrame.Validate() != nil {
		return LineageIndexFrame{}, LineageIndexFrame{}, EvidenceFrame{}, nil, nil, admissionCorrupt("historical-supersession-frames", "planned segment-zero header is invalid", err)
	}
	return supersededFrame, reservedFrame, headerFrame, append([]byte(nil), supersededBytes...), append([]byte(nil), reservedBytes...), nil
}

// AppendGenerationReserved consumes both the migration plan and the
// verifier-returned historical supersession receipt before attempting the one
// allowed filesystem append. Any receipt consumption or append uncertainty is
// fail-closed and requires a fresh full-root Open.
func (r *HistoricalSupersessionAdjacentReserveReady) AppendGenerationReserved(ctx context.Context, candidate OwnedCurrentCandidate) (HistoricalSupersessionReservationTransitionResult, error) {
	pre := HistoricalSupersessionReservationTransitionResult{outcome: evidencefs.AdmissionTransitionPreMutationFailure, candidateSequence: 1}
	if !validHistoricalSupersessionAdjacentReady(r, candidate) {
		return pre, fail(CodeEvidenceRecoveryRequired, "historical-supersession-reserve", "historical adjacent reservation authority is unavailable", nil)
	}
	if err := contextAdmissionError(ctx); err != nil {
		return pre, err
	}
	planned := append([]byte(nil), r.reservedFrameBytes...)
	pre.previousRevision = r.revision
	pre.candidateRevision = r.revision + 1
	pre.candidateDigest = sha256.Sum256(planned)
	pre.reservedRecordDigest = r.reservedFrame.RecordDigest
	prefix, err := readSuccessorInventoryIndex(ctx, r.inventory, r.target, r.indexRecords, r.indexTail, r.indexDigest, r.indexSize, "historical-supersession-reserve-prefix")
	if err != nil {
		return pre, err
	}
	frame := r.reservedFrame
	if pre.candidateDigest == ([32]byte{}) || pre.reservedRecordDigest.Validate() != nil || frame.Sequence != uint64(len(prefix.frames)) || frame.PreviousRecordDigest == nil || *frame.PreviousRecordDigest != r.indexTail || !bytes.HasSuffix(prefix.raw, r.supersededFrameBytes) || !bytes.Equal(planned, r.reservedFrameBytes) || !validHistoricalSupersessionAdjacentReady(r, candidate) {
		return pre, fail(CodeEvidenceRecoveryRequired, "historical-supersession-reserve", "historical adjacent reservation changed before append", nil)
	}
	if !r.consumed.CompareAndSwap(false, true) {
		return pre, fail(CodeEvidenceRecoveryRequired, "historical-supersession-reserve", "historical adjacent reservation authority was already consumed", nil)
	}
	if err := r.receipt.consume(r.owner, r.authority.digest); err != nil {
		revokeHistoricalSupersessionAdjacentReady(r)
		return pre, fail(CodeEvidenceRecoveryRequired, "historical-supersession-reserve", "historical supersession receipt could not be consumed", nil)
	}
	fsResult, transitionErr := r.mutation.AppendTargetIndex(ctx, r.inventory, planned)
	result := HistoricalSupersessionReservationTransitionResult{
		outcome: fsResult.Outcome(), candidateDigest: fsResult.CandidateDigest(), candidateSequence: 1,
		candidateRevision: fsResult.CandidateRevision(), previousRevision: fsResult.PreviousRevision(), reservedRecordDigest: pre.reservedRecordDigest,
	}
	if fsResult.Outcome() != evidencefs.AdmissionTransitionDurable {
		revokeHistoricalSupersessionAdjacentReady(r)
		return result, mapAdmissionMutationError(transitionErr, "historical-supersession-reserve")
	}
	postFailure := func(suffix string) (HistoricalSupersessionReservationTransitionResult, error) {
		_ = fsResult.Invalidate()
		revokeHistoricalSupersessionAdjacentReady(r)
		result.outcome = evidencefs.AdmissionTransitionUnknown
		result.next = nil
		return result, admissionPostMutationFailure("historical-supersession-reserve" + suffix)
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
	if targetErr != nil || target != r.target {
		return postFailure("-target")
	}
	if fullSetErr != nil || fullSet == ([32]byte{}) || fullSet == r.fullSet {
		return postFailure("-full-set")
	}
	verified, verifyErr := validateSuccessorIndexAppend(ctx, nextInventory, target, prefix.raw, planned, r.indexRecords+1, pre.reservedRecordDigest)
	if verifyErr != nil {
		return postFailure("-index")
	}
	nextToken, tokenErr := nextInventory.MutationToken()
	if tokenErr != nil || !nextToken.ValidFor(nextInventory) {
		return postFailure("-token")
	}
	next := &HistoricalSuccessorReservedDurablePermit{
		owner: r.owner, candidateBinding: candidate.binding, inventory: nextInventory, mutation: nextToken,
		revision: revision, target: target, fullSet: fullSet, indexRecords: uint64(len(verified.frames)), indexTail: pre.reservedRecordDigest,
		indexDigest: verified.digest, indexSize: verified.size,
		source: r.source, planned: r.planned,
		plannedRuntime: VerifiedRuntimeArtifact{owner: r.plannedRuntime.owner, bytes: append([]byte(nil), r.plannedRuntime.bytes...), digest: r.plannedRuntime.digest, sizeBytes: r.plannedRuntime.sizeBytes},
		authority:      r.authority, receipt: r.receipt,
		reservedFrame: cloneProjectionValue(r.reservedFrame), headerFrame: cloneProjectionValue(r.headerFrame), reservedFrameBytes: append([]byte(nil), r.reservedFrameBytes...),
		readyCanonical: r.binding.canonical, fsIndex: fsResult, consumed: &atomic.Bool{},
	}
	next.self = next
	next.binding = &historicalSuccessorReservedBinding{
		permit: next, candidateBinding: candidate.binding, inventory: nextInventory, mutation: nextToken,
		source: r.source, planned: r.planned, authority: r.authority, receipt: r.receipt,
	}
	next.binding.canonical = historicalSuccessorReservedDigest(next)
	historicalSuccessorReservedRegistry.Store(next.binding, next.binding.canonical)
	if !validHistoricalSuccessorReservedPermit(next, candidate) {
		historicalSuccessorReservedRegistry.Delete(next.binding)
		return postFailure("-seal")
	}
	historicalSupersessionAdjacentRegistry.Delete(r.binding)
	result.next = next
	return result, nil
}

func bindStoredHistoricalSupersession(ctx context.Context, current OwnedVerifiedDecision, source, planned *verifiedAdmissionRegisteredGeneration, plannedRuntime VerifiedRuntimeArtifact, superseded GenerationSuperseded) (*VerifiedLineageSupersessionAuthority, *verifiedHistoricalSupersessionReceipt, error) {
	if source == nil || planned == nil || source.policy == nil || source.replay == nil || !source.replay.supersessionDebited || planned.replay != nil || !validVerifiedAdmissionRegisteredGeneration(source, current) || !validVerifiedAdmissionRegisteredGeneration(planned, current) || plannedRuntime.owner != current.owner.token || plannedRuntime.digest != planned.descriptor.header.OuterArtifactDigest || plannedRuntime.sizeBytes != planned.descriptor.header.OuterArtifactSizeBytes || uint64(len(plannedRuntime.bytes)) != plannedRuntime.sizeBytes || DigestBytes(plannedRuntime.bytes) != plannedRuntime.digest {
		return nil, nil, fail(CodeEvidenceRecoveryRequired, "historical-supersession-bind", "same-verifier registered generation inputs are unavailable", nil)
	}
	execution, err := bindRecoveryExecution(*source.policy, current, source.decision, source.bindings, source.descriptor, source.replay.recovery)
	if err != nil {
		return nil, nil, fail(CodeEvidenceRecoveryRequired, "historical-supersession-bind", "source recovery execution cannot be rebound", nil)
	}
	continuation := cloneProjectionValue(superseded.PlannedGenerationReserved.Continuation)
	var boundary ownedSupersessionEvidence
	if superseded.Outcome == "activated_no_migration_progress" {
		if superseded.OldActivationRecordDigest == nil || superseded.OldInitialJournalTailDigest == nil {
			return nil, nil, admissionCorrupt("historical-supersession-bind", "header-only supersession boundary is incomplete", nil)
		}
		boundary = &ownedHeaderOnlySupersessionEvidence{
			owner: current.owner.token, generation: source.descriptor.identity, tailDigest: source.replay.recovery.tailDigest,
			activationDigest: *superseded.OldActivationRecordDigest, initialTailDigest: *superseded.OldInitialJournalTailDigest,
			continuation: continuation,
		}
	} else {
		if superseded.OldCheckpointRecordDigest == nil {
			return nil, nil, admissionCorrupt("historical-supersession-bind", "checkpoint supersession boundary is incomplete", nil)
		}
		boundary = &ownedCheckpointSupersessionEvidence{
			owner: current.owner.token, generation: source.descriptor.identity, tailDigest: source.replay.recovery.tailDigest,
			checkpointDigest: *superseded.OldCheckpointRecordDigest, terminalDigest: cloneDigestPointer(source.replay.recovery.lastTerminalDigest),
			resolutionDigest: cloneDigestPointer(source.replay.recovery.lastResolutionDigest), outcome: superseded.Outcome,
			continuation: continuation,
		}
	}
	authority, err := bindLineageSupersession(*source.policy, execution, boundary)
	if err != nil {
		return nil, nil, fail(CodeEvidenceRecoveryRequired, "historical-supersession-bind", "stored supersession authority cannot be reconstructed", nil)
	}
	if authority.digest != superseded.LineageSupersessionAuthorityDigest {
		return nil, nil, admissionCorrupt("historical-supersession-bind", "stored supersession authority digest differs from recovered authority", nil)
	}
	receipt, err := current.recoverHistoricalSupersession(ctx, authority, superseded, source.recoveryArtifact, plannedRuntime, planned.runtimeReceipt, planned.recoveryArtifact, planned.recoveryReceipt)
	if err != nil {
		if errorsIsContext(err) {
			return nil, nil, mapEvidenceAdmissionError(err, "historical-supersession-verifier")
		}
		return nil, nil, fail(CodeEvidenceRecoveryRequired, "historical-supersession-verifier", "current verifier cannot authorize the stored supersession", nil)
	}
	return authority, receipt, nil
}

func errorsIsContext(err error) bool {
	return err != nil && (IsCode(err, CodeContextCanceled) || IsCode(err, CodeDeadlineExceeded) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded))
}

func historicalSupersessionFramesExact(ready *HistoricalSupersessionAdjacentReserveReady) bool {
	if ready == nil || ready.source == nil || ready.planned == nil || ready.source.replay == nil || !ready.source.replay.supersessionDebited || ready.planned.replay != nil || ready.supersededFrame.Validate() != nil || ready.reservedFrame.Validate() != nil || ready.headerFrame.Validate() != nil || ready.supersededFrame.Record.Superseded == nil || ready.reservedFrame.Record.Reserved == nil || ready.headerFrame.Record.Header == nil || ready.supersededFrame.Sequence+1 != ready.reservedFrame.Sequence || ready.supersededFrame.RecordDigest != ready.indexTail || ready.reservedFrame.PreviousRecordDigest == nil || *ready.reservedFrame.PreviousRecordDigest != ready.indexTail || !canonicalEqual(ready.supersededFrame.Record.Superseded.PlannedGenerationReserved, ready.reservedFrame.Record.Reserved) || ready.reservedFrame.Record.Reserved.ExpectedSegment0HeaderDigest != ready.headerFrame.RecordDigest {
		return false
	}
	supersededBytes, supersededErr := EncodeCanonicalLineageFrame(ready.supersededFrame)
	reservedBytes, reservedErr := EncodeCanonicalLineageFrame(ready.reservedFrame)
	return supersededErr == nil && reservedErr == nil && bytes.Equal(supersededBytes, ready.supersededFrameBytes) && bytes.Equal(reservedBytes, ready.reservedFrameBytes)
}

func historicalSupersessionAdjacentDigest(ready *HistoricalSupersessionAdjacentReserveReady) [32]byte {
	if ready == nil || ready.self != ready || ready.candidateBinding == nil || ready.source == nil || ready.planned == nil || ready.authority == nil || ready.receipt == nil || ready.consumed == nil || ready.mutation == nil || !historicalSupersessionFramesExact(ready) {
		return [32]byte{}
	}
	h := sha256.New()
	h.Write([]byte("cloud-agents-platform-historical-supersession-adjacent-ready/v1\x00"))
	h.Write(ready.candidateBinding.canonical[:])
	h.Write(ready.target[:])
	h.Write(ready.fullSet[:])
	h.Write(ready.transcriptCanonical[:])
	h.Write(ready.indexDigest[:])
	h.Write(ready.source.canonical[:])
	h.Write(ready.planned.canonical[:])
	rootDigest := rootQuotaFactsDigest(ready.rootFacts)
	h.Write(rootDigest[:])
	var encoded [8]byte
	for _, value := range []uint64{
		ready.revision, ready.indexRecords, ready.indexSize,
		ready.quotaAdmission.finalObjectCount, ready.quotaAdmission.finalObjectBytes,
		ready.quotaAdmission.journalCount, ready.quotaAdmission.journalReservedBytes,
		ready.quotaAdmission.indexCount, ready.quotaAdmission.indexReservedBytes,
		ready.quotaAdmission.targetIndexRecords, ready.quotaAdmission.targetIndexReservedBytes,
		ready.plannedRuntime.sizeBytes,
	} {
		binary.BigEndian.PutUint64(encoded[:], value)
		h.Write(encoded[:])
	}
	writeAdmissionString(h, ready.indexTail.String())
	writeAdmissionString(h, ready.plannedRuntime.digest.String())
	writeAdmissionString(h, ready.authority.digest.String())
	writeAdmissionString(h, ready.receipt.authorityDigest.String())
	writeAdmissionString(h, ready.receipt.runtimeReceipt.digest.String())
	writeAdmissionString(h, ready.receipt.recoveryReceipt.digest.String())
	for _, value := range []uint64{ready.receipt.runtimeReceipt.sizeBytes, ready.receipt.recoveryReceipt.sizeBytes} {
		binary.BigEndian.PutUint64(encoded[:], value)
		h.Write(encoded[:])
	}
	h.Write(ready.supersededFrameBytes)
	h.Write(ready.reservedFrameBytes)
	headerBytes, err := EncodeCanonicalEvidenceFrame(ready.headerFrame)
	if err != nil {
		return [32]byte{}
	}
	h.Write(headerBytes)
	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result
}

func validHistoricalSupersessionAdjacentReady(ready *HistoricalSupersessionAdjacentReserveReady, candidate OwnedCurrentCandidate) bool {
	if ready == nil || ready.self != ready || ready.binding == nil || ready.binding.ready != ready || ready.owner == nil || ready.owner != candidate.verifiedRun.currentDecision.owner || ready.candidateBinding != candidate.binding || ready.binding.candidateBinding != candidate.binding || ready.inventory == nil || ready.binding.inventory != ready.inventory || ready.mutation == nil || ready.binding.mutation != ready.mutation || ready.source == nil || ready.binding.source != ready.source || ready.planned == nil || ready.binding.planned != ready.planned || ready.authority == nil || ready.binding.authority != ready.authority || ready.receipt == nil || ready.binding.receipt != ready.receipt || ready.consumed == nil || ready.consumed.Load() || !validOwnedCurrentCandidate(candidate) || ready.target != digestRaw(candidate.verifiedRun.executionLineageDigest) || !ready.mutation.ValidFor(ready.inventory) || !validVerifiedAdmissionRegisteredGeneration(ready.source, candidate.verifiedRun.currentDecision) || !validVerifiedAdmissionRegisteredGeneration(ready.planned, candidate.verifiedRun.currentDecision) || !historicalSupersessionFramesExact(ready) || ready.source.replay.cursor.lineageIndexNextSequence != ready.indexRecords || ready.source.replay.cursor.lineageIndexPreviousRecordDigest != ready.indexTail || ready.plannedRuntime.owner != candidate.owner || ready.plannedRuntime.digest != ready.planned.descriptor.header.OuterArtifactDigest || ready.plannedRuntime.sizeBytes != ready.planned.descriptor.header.OuterArtifactSizeBytes || uint64(len(ready.plannedRuntime.bytes)) != ready.plannedRuntime.sizeBytes || DigestBytes(ready.plannedRuntime.bytes) != ready.plannedRuntime.digest || ready.receipt.owner != ready.owner || ready.receipt.authorityDigest != ready.authority.digest || ready.receipt.consumed.Load() || !validHistoricalSupersessionAuthority(ready.source, ready.authority, candidate) || !validRegisteredRuntimeReceipt(ready.receipt.runtimeReceipt, candidate.owner, ready.plannedRuntime.digest, ready.plannedRuntime.sizeBytes) || !ready.receipt.runtimeReceipt.registeredPublication.SameObject(ready.planned.runtimeReceipt.registeredPublication) || !validRegisteredDecisionRecoveryReceipt(ready.receipt.recoveryReceipt, candidate.owner, ready.planned.recoveryArtifact.digest, ready.planned.recoveryArtifact.sizeBytes) || !ready.receipt.recoveryReceipt.registeredPublication.SameObject(ready.planned.recoveryReceipt.registeredPublication) || !ready.rootFacts.valid() || ready.binding.canonical == ([32]byte{}) || ready.binding.canonical != historicalSupersessionAdjacentDigest(ready) {
		return false
	}
	facts, factsErr := ready.planned.bundle.quotaFactsForAdmission()
	quota, quotaErr := calculateRootQuotaAdmissionForArtifacts(ready.rootFacts, facts, ready.plannedRuntime, ready.planned.recoveryArtifact)
	if factsErr != nil || quotaErr != nil || quota != ready.quotaAdmission {
		return false
	}
	registered, ok := historicalSupersessionAdjacentRegistry.Load(ready.binding)
	if !ok || registered != ready.binding.canonical {
		return false
	}
	lineage, lineageErr := ready.inventory.Lineage(ready.target)
	var index *evidencefs.AdmissionFileView
	if lineageErr == nil {
		index, lineageErr = lineage.Index()
	}
	var indexDigest [32]byte
	var indexSize uint64
	if lineageErr == nil {
		indexDigest, lineageErr = index.Digest()
	}
	if lineageErr == nil {
		indexSize, lineageErr = index.Size()
	}
	if lineageErr != nil || indexDigest != ready.indexDigest || indexSize != ready.indexSize {
		return false
	}
	revision, revisionErr := ready.inventory.Revision()
	target, targetErr := ready.inventory.Target()
	fullSet, fullSetErr := ready.inventory.FullSetDigest()
	return revisionErr == nil && revision == ready.revision && targetErr == nil && target == ready.target && fullSetErr == nil && fullSet == ready.fullSet
}

func validHistoricalSupersessionAuthority(source *verifiedAdmissionRegisteredGeneration, authority *VerifiedLineageSupersessionAuthority, candidate OwnedCurrentCandidate) bool {
	if source == nil || source.policy == nil || source.replay == nil || authority == nil || authority.owner != candidate.verifiedRun.currentDecision.owner || authority.session != candidate.owner || authority.consumed.Load() || !sameGenerationIdentity(authority.generation, source.descriptor.identity) || authority.tailDigest != source.replay.recovery.tailDigest {
		return false
	}
	digest, digestErr := authority.subject.ComputeDigest()
	execution, executionErr := bindRecoveryExecution(*source.policy, candidate.verifiedRun.currentDecision, source.decision, source.bindings, source.descriptor, source.replay.recovery)
	return digestErr == nil && executionErr == nil && digest == authority.digest && validateRecoveryAuthorityBindings(candidate.verifiedRun.currentDecision.digest, source.policy.subject, execution.subject, authority.subject) == nil
}

func historicalSuccessorReservedDigest(permit *HistoricalSuccessorReservedDurablePermit) [32]byte {
	if permit == nil || permit.self != permit || permit.candidateBinding == nil || permit.source == nil || permit.planned == nil || permit.authority == nil || permit.receipt == nil || permit.mutation == nil || permit.consumed == nil || permit.readyCanonical == ([32]byte{}) || permit.reservedFrame.Validate() != nil || permit.headerFrame.Validate() != nil || permit.reservedFrame.Record.Reserved == nil || permit.headerFrame.Record.Header == nil || permit.reservedFrame.Record.Reserved.ExpectedSegment0HeaderDigest != permit.headerFrame.RecordDigest {
		return [32]byte{}
	}
	framed, err := EncodeCanonicalLineageFrame(permit.reservedFrame)
	if err != nil || !bytes.Equal(framed, permit.reservedFrameBytes) {
		return [32]byte{}
	}
	h := sha256.New()
	h.Write([]byte("cloud-agents-platform-historical-successor-reserved/v1\x00"))
	h.Write(permit.readyCanonical[:])
	h.Write(permit.candidateBinding.canonical[:])
	h.Write(permit.target[:])
	h.Write(permit.fullSet[:])
	h.Write(permit.indexDigest[:])
	h.Write(permit.source.canonical[:])
	h.Write(permit.planned.canonical[:])
	var encoded [8]byte
	for _, value := range []uint64{permit.revision, permit.indexRecords, permit.indexSize, permit.plannedRuntime.sizeBytes} {
		binary.BigEndian.PutUint64(encoded[:], value)
		h.Write(encoded[:])
	}
	writeAdmissionString(h, permit.indexTail.String())
	writeAdmissionString(h, permit.plannedRuntime.digest.String())
	writeAdmissionString(h, permit.authority.digest.String())
	writeAdmissionString(h, permit.receipt.authorityDigest.String())
	h.Write(permit.reservedFrameBytes)
	headerBytes, err := EncodeCanonicalEvidenceFrame(permit.headerFrame)
	if err != nil {
		return [32]byte{}
	}
	h.Write(headerBytes)
	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result
}

func validHistoricalSuccessorReservedPermit(permit *HistoricalSuccessorReservedDurablePermit, candidate OwnedCurrentCandidate) bool {
	if permit == nil || permit.self != permit || permit.binding == nil || permit.binding.permit != permit || permit.owner == nil || permit.owner != candidate.verifiedRun.currentDecision.owner || permit.candidateBinding != candidate.binding || permit.binding.candidateBinding != candidate.binding || permit.inventory == nil || permit.binding.inventory != permit.inventory || permit.mutation == nil || permit.binding.mutation != permit.mutation || permit.source == nil || permit.binding.source != permit.source || permit.planned == nil || permit.binding.planned != permit.planned || permit.authority == nil || permit.binding.authority != permit.authority || permit.receipt == nil || permit.binding.receipt != permit.receipt || permit.consumed == nil || permit.consumed.Load() || !validOwnedCurrentCandidate(candidate) || permit.target != digestRaw(candidate.verifiedRun.executionLineageDigest) || !permit.mutation.ValidFor(permit.inventory) || !validVerifiedAdmissionRegisteredGeneration(permit.source, candidate.verifiedRun.currentDecision) || !validVerifiedAdmissionRegisteredGeneration(permit.planned, candidate.verifiedRun.currentDecision) || !validHistoricalSupersessionAuthority(permit.source, permit.authority, candidate) || permit.receipt.owner != permit.owner || permit.receipt.authorityDigest != permit.authority.digest || !permit.receipt.consumed.Load() || !validRegisteredRuntimeReceipt(permit.receipt.runtimeReceipt, candidate.owner, permit.plannedRuntime.digest, permit.plannedRuntime.sizeBytes) || !permit.receipt.runtimeReceipt.registeredPublication.SameObject(permit.planned.runtimeReceipt.registeredPublication) || !validRegisteredDecisionRecoveryReceipt(permit.receipt.recoveryReceipt, candidate.owner, permit.planned.recoveryArtifact.digest, permit.planned.recoveryArtifact.sizeBytes) || !permit.receipt.recoveryReceipt.registeredPublication.SameObject(permit.planned.recoveryReceipt.registeredPublication) || permit.plannedRuntime.owner != candidate.owner || permit.plannedRuntime.digest != permit.planned.descriptor.header.OuterArtifactDigest || permit.plannedRuntime.sizeBytes != permit.planned.descriptor.header.OuterArtifactSizeBytes || uint64(len(permit.plannedRuntime.bytes)) != permit.plannedRuntime.sizeBytes || DigestBytes(permit.plannedRuntime.bytes) != permit.plannedRuntime.digest || permit.reservedFrame.Record.Reserved == nil || permit.indexRecords != permit.reservedFrame.Sequence+1 || permit.indexTail != permit.reservedFrame.RecordDigest || !sameGenerationHeader(permit.planned.descriptor.identity, permit.reservedFrame.Record.Reserved.PlannedSegment0Header) || permit.fsIndex.Outcome() != evidencefs.AdmissionTransitionDurable || permit.fsIndex.Inventory() != permit.inventory || permit.fsIndex.CandidateKind() != "target_index_append" || permit.fsIndex.CandidateDigest() != sha256.Sum256(permit.reservedFrameBytes) || permit.fsIndex.CandidateRevision() != permit.revision || permit.fsIndex.PreviousRevision()+1 != permit.revision || permit.binding.canonical == ([32]byte{}) || permit.binding.canonical != historicalSuccessorReservedDigest(permit) {
		return false
	}
	registered, ok := historicalSuccessorReservedRegistry.Load(permit.binding)
	if !ok || registered != permit.binding.canonical {
		return false
	}
	lineage, lineageErr := permit.inventory.Lineage(permit.target)
	var index *evidencefs.AdmissionFileView
	if lineageErr == nil {
		index, lineageErr = lineage.Index()
	}
	var indexDigest [32]byte
	var indexSize uint64
	if lineageErr == nil {
		indexDigest, lineageErr = index.Digest()
	}
	if lineageErr == nil {
		indexSize, lineageErr = index.Size()
	}
	revision, revisionErr := permit.inventory.Revision()
	target, targetErr := permit.inventory.Target()
	fullSet, fullSetErr := permit.inventory.FullSetDigest()
	return lineageErr == nil && indexDigest == permit.indexDigest && indexSize == permit.indexSize && revisionErr == nil && revision == permit.revision && targetErr == nil && target == permit.target && fullSetErr == nil && fullSet == permit.fullSet
}

func revokeHistoricalSuccessorReservedPermit(permit *HistoricalSuccessorReservedDurablePermit) {
	if permit == nil {
		return
	}
	if permit.binding != nil {
		historicalSuccessorReservedRegistry.Delete(permit.binding)
	}
	if permit.fsIndex.Outcome() == evidencefs.AdmissionTransitionDurable {
		_ = permit.fsIndex.Invalidate()
	}
	revokeVerifiedAdmissionRegisteredGeneration(permit.source)
	revokeVerifiedAdmissionRegisteredGeneration(permit.planned)
}

func revokeHistoricalSupersessionAdjacentReady(ready *HistoricalSupersessionAdjacentReserveReady) {
	if ready == nil {
		return
	}
	if ready.binding != nil {
		historicalSupersessionAdjacentRegistry.Delete(ready.binding)
	}
	if ready.source != nil {
		revokeVerifiedAdmissionRegisteredGeneration(ready.source)
	}
	if ready.planned != nil {
		revokeVerifiedAdmissionRegisteredGeneration(ready.planned)
	}
}
