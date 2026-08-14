package migration

import (
	"bytes"
	"context"
	"crypto/sha256"
	"sync"
	"sync/atomic"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/evidencefs"
)

// HistoricalSuccessorGenerationHandoffResult is the closed transition from
// crash-recovered full-root activation to one retained generation lease.
type HistoricalSuccessorGenerationHandoffResult struct {
	outcome           evidencefs.AdmissionTransitionOutcome
	next              *HistoricalSuccessorGenerationHandoffReady
	candidateDigest   [32]byte
	candidateSequence uint64
	revision          uint64
}

func (r HistoricalSuccessorGenerationHandoffResult) Outcome() evidencefs.AdmissionTransitionOutcome {
	return r.outcome
}
func (r HistoricalSuccessorGenerationHandoffResult) Next() *HistoricalSuccessorGenerationHandoffReady {
	return r.next
}
func (r HistoricalSuccessorGenerationHandoffResult) CandidateKind() string {
	return "generation_handoff"
}
func (r HistoricalSuccessorGenerationHandoffResult) CandidateDigest() [32]byte {
	return r.candidateDigest
}
func (r HistoricalSuccessorGenerationHandoffResult) CandidateSequence() uint64 {
	return r.candidateSequence
}
func (r HistoricalSuccessorGenerationHandoffResult) Revision() uint64 { return r.revision }

// HistoricalSuccessorGenerationHandoffReady retains only the exact lineage
// and generation locks. It is non-runnable until strict replay and recovery.
type HistoricalSuccessorGenerationHandoffReady struct {
	self             *HistoricalSuccessorGenerationHandoffReady
	prior            *HistoricalSuccessorGenerationReadyPermit
	candidateBinding *verifiedEvidenceRunBinding
	lease            *evidencefs.GenerationLease
	target           [32]byte
	journal          Digest
	revision         uint64
	binding          *historicalSuccessorGenerationHandoffBinding
	consumed         *atomic.Bool
}

type historicalSuccessorGenerationHandoffBinding struct {
	ready            *HistoricalSuccessorGenerationHandoffReady
	prior            *HistoricalSuccessorGenerationReadyPermit
	candidateBinding *verifiedEvidenceRunBinding
	lease            *evidencefs.GenerationLease
	canonical        [32]byte
}

type historicalSuccessorGenerationHandoffRecord struct {
	ready            *HistoricalSuccessorGenerationHandoffReady
	binding          *historicalSuccessorGenerationHandoffBinding
	prior            *HistoricalSuccessorGenerationReadyPermit
	priorBinding     *historicalSuccessorGenerationReadyBinding
	priorCanonical   [32]byte
	candidateBinding *verifiedEvidenceRunBinding
	lease            *evidencefs.GenerationLease
	source           *verifiedAdmissionRegisteredGeneration
	planned          *verifiedAdmissionRegisteredGeneration
	authority        *VerifiedLineageSupersessionAuthority
	receipt          *verifiedHistoricalSupersessionReceipt
	canonical        [32]byte
}

var historicalSuccessorGenerationHandoffRegistry sync.Map

// HistoricalSuccessorGenerationReplayResult exposes read-only replay facts;
// only Next carries the sealed, still non-runnable recovery input.
type HistoricalSuccessorGenerationReplayResult struct {
	next             *HistoricalSuccessorGenerationReplayReady
	snapshotIdentity [32]byte
	indexRecords     uint64
	segmentCount     uint32
	journalRecords   uint64
	journalTail      Digest
}

func (r HistoricalSuccessorGenerationReplayResult) Next() *HistoricalSuccessorGenerationReplayReady {
	return r.next
}
func (r HistoricalSuccessorGenerationReplayResult) SnapshotIdentity() [32]byte {
	return r.snapshotIdentity
}
func (r HistoricalSuccessorGenerationReplayResult) IndexRecords() uint64   { return r.indexRecords }
func (r HistoricalSuccessorGenerationReplayResult) SegmentCount() uint32   { return r.segmentCount }
func (r HistoricalSuccessorGenerationReplayResult) JournalRecords() uint64 { return r.journalRecords }
func (r HistoricalSuccessorGenerationReplayResult) JournalTail() Digest    { return r.journalTail }

// HistoricalSuccessorGenerationReplayReady retains the exact post-handoff
// snapshot after strict adjacent-index and header-only journal replay.
type HistoricalSuccessorGenerationReplayReady struct {
	self             *HistoricalSuccessorGenerationReplayReady
	prior            *HistoricalSuccessorGenerationHandoffReady
	candidateBinding *verifiedEvidenceRunBinding
	lease            *evidencefs.GenerationLease
	snapshot         *evidencefs.GenerationSnapshot
	target           [32]byte
	journal          Digest
	revision         uint64
	snapshotIdentity [32]byte
	indexFact        evidencefs.GenerationFileFact
	segmentFact      evidencefs.GenerationFileFact
	indexRecords     uint64
	segmentCount     uint32
	journalRecords   uint64
	journalTail      Digest
	binding          *historicalSuccessorGenerationReplayBinding
	consumed         *atomic.Bool
}

type historicalSuccessorGenerationReplayBinding struct {
	ready            *HistoricalSuccessorGenerationReplayReady
	prior            *HistoricalSuccessorGenerationHandoffReady
	candidateBinding *verifiedEvidenceRunBinding
	lease            *evidencefs.GenerationLease
	snapshot         *evidencefs.GenerationSnapshot
	canonical        [32]byte
}

type historicalSuccessorGenerationReplayRecord struct {
	ready            *HistoricalSuccessorGenerationReplayReady
	binding          *historicalSuccessorGenerationReplayBinding
	prior            *HistoricalSuccessorGenerationHandoffReady
	priorBinding     *historicalSuccessorGenerationHandoffBinding
	candidateBinding *verifiedEvidenceRunBinding
	lease            *evidencefs.GenerationLease
	snapshot         *evidencefs.GenerationSnapshot
	canonical        [32]byte
}

var historicalSuccessorGenerationReplayRegistry sync.Map

// Handoff irreversibly exchanges the full-root admission authority for one
// exact generation lease without minting runtime or cursor authority.
func (p *HistoricalSuccessorGenerationReadyPermit) Handoff(ctx context.Context, candidate OwnedCurrentCandidate) (HistoricalSuccessorGenerationHandoffResult, error) {
	pre := HistoricalSuccessorGenerationHandoffResult{outcome: evidencefs.AdmissionTransitionPreMutationFailure, candidateSequence: 4}
	if !validHistoricalSuccessorGenerationReadyPermit(p, candidate) {
		return pre, fail(CodeEvidenceRecoveryRequired, "historical-successor-handoff", "historical successor generation-ready authority is unavailable", nil)
	}
	if err := contextAdmissionError(ctx); err != nil {
		return pre, err
	}
	registered, ok := historicalSuccessorGenerationReadyRegistry.Load(p.binding)
	if !ok || registered != p.binding.canonical {
		return pre, fail(CodeEvidenceRecoveryRequired, "historical-successor-handoff", "immutable historical successor state is unavailable", nil)
	}
	pre.revision = p.revision
	pre.candidateDigest = historicalSuccessorHandoffCandidateDigest(p)
	if pre.candidateDigest == ([32]byte{}) || !p.consumed.CompareAndSwap(false, true) {
		return pre, fail(CodeEvidenceRecoveryRequired, "historical-successor-handoff", "historical successor handoff authority is invalid or consumed", nil)
	}
	lease, transitionErr := p.mutation.HandoffGeneration(ctx, p.inventory, digestRaw(p.journal))
	result := HistoricalSuccessorGenerationHandoffResult{outcome: evidencefs.AdmissionTransitionDurable, candidateDigest: pre.candidateDigest, candidateSequence: 4, revision: pre.revision}
	if transitionErr != nil || lease == nil || !lease.Active() {
		if lease == nil && p.mutation.ValidFor(p.inventory) {
			p.consumed.CompareAndSwap(true, false)
			result.outcome = evidencefs.AdmissionTransitionPreMutationFailure
			return result, mapAdmissionMutationError(transitionErr, "historical-successor-handoff")
		}
		if lease != nil {
			_ = lease.Close()
		}
		historicalSuccessorGenerationReadyRegistry.Delete(p.binding)
		revokeVerifiedAdmissionRegisteredGeneration(p.source)
		revokeVerifiedAdmissionRegisteredGeneration(p.planned)
		result.outcome = evidencefs.AdmissionTransitionUnknown
		return result, admissionPostMutationFailure("historical-successor-handoff")
	}
	target, targetErr := lease.Target()
	journal, journalErr := lease.Journal()
	if targetErr != nil || journalErr != nil || target != p.target || journal != digestRaw(p.journal) {
		_ = lease.Close()
		historicalSuccessorGenerationReadyRegistry.Delete(p.binding)
		revokeVerifiedAdmissionRegisteredGeneration(p.source)
		revokeVerifiedAdmissionRegisteredGeneration(p.planned)
		result.outcome = evidencefs.AdmissionTransitionUnknown
		return result, admissionPostMutationFailure("historical-successor-handoff-bind")
	}
	ready := &HistoricalSuccessorGenerationHandoffReady{
		prior: p, candidateBinding: candidate.binding, lease: lease,
		target: target, journal: p.journal, revision: p.revision, consumed: &atomic.Bool{},
	}
	ready.self = ready
	ready.binding = &historicalSuccessorGenerationHandoffBinding{ready: ready, prior: p, candidateBinding: candidate.binding, lease: lease}
	ready.binding.canonical = historicalSuccessorGenerationHandoffDigest(ready)
	historicalSuccessorGenerationHandoffRegistry.Store(ready, historicalSuccessorGenerationHandoffRecord{
		ready: ready, binding: ready.binding, prior: p, priorBinding: p.binding, priorCanonical: p.binding.canonical,
		candidateBinding: candidate.binding, lease: lease, source: p.source, planned: p.planned,
		authority: p.authority, receipt: p.receipt, canonical: ready.binding.canonical,
	})
	historicalSuccessorGenerationReadyRegistry.Delete(p.binding)
	if !validHistoricalSuccessorGenerationHandoffReady(ready, candidate) {
		historicalSuccessorGenerationHandoffRegistry.Delete(ready)
		_ = lease.Close()
		revokeVerifiedAdmissionRegisteredGeneration(p.source)
		revokeVerifiedAdmissionRegisteredGeneration(p.planned)
		result.outcome = evidencefs.AdmissionTransitionUnknown
		return result, admissionPostMutationFailure("historical-successor-handoff-seal")
	}
	result.next = ready
	return result, nil
}

func historicalSuccessorHandoffCandidateDigest(permit *HistoricalSuccessorGenerationReadyPermit) [32]byte {
	if permit == nil || permit.self != permit || permit.binding == nil || permit.binding.canonical == ([32]byte{}) || permit.journal.Validate() != nil {
		return [32]byte{}
	}
	h := sha256.New()
	h.Write([]byte("cloud-agents-platform-historical-successor-handoff-candidate/v1\x00"))
	h.Write(permit.binding.canonical[:])
	writeAdmissionUint(h, permit.revision)
	writeAdmissionString(h, permit.journal.String())
	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result
}

func historicalSuccessorGenerationHandoffDigest(ready *HistoricalSuccessorGenerationHandoffReady) [32]byte {
	if ready == nil || ready.self != ready || ready.prior == nil || ready.prior.binding == nil || ready.candidateBinding == nil || ready.lease == nil || ready.target == ([32]byte{}) || ready.journal.Validate() != nil {
		return [32]byte{}
	}
	h := sha256.New()
	h.Write([]byte("cloud-agents-platform-historical-successor-handoff-ready/v1\x00"))
	h.Write(ready.prior.binding.canonical[:])
	h.Write(ready.candidateBinding.canonical[:])
	h.Write(ready.target[:])
	writeAdmissionString(h, ready.journal.String())
	writeAdmissionUint(h, ready.revision)
	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result
}

func validHistoricalSuccessorGenerationHandoffReady(ready *HistoricalSuccessorGenerationHandoffReady, candidate OwnedCurrentCandidate) bool {
	return validHistoricalSuccessorGenerationHandoffShape(ready, candidate, false)
}

func validConsumedHistoricalSuccessorGenerationHandoffReady(ready *HistoricalSuccessorGenerationHandoffReady, candidate OwnedCurrentCandidate) bool {
	return validHistoricalSuccessorGenerationHandoffShape(ready, candidate, true)
}

func validHistoricalSuccessorGenerationHandoffShape(ready *HistoricalSuccessorGenerationHandoffReady, candidate OwnedCurrentCandidate, consumed bool) bool {
	if ready == nil || ready.self != ready || ready.binding == nil || ready.binding.ready != ready || ready.prior == nil || ready.prior.binding == nil || ready.candidateBinding != candidate.binding || ready.binding.prior != ready.prior || ready.binding.candidateBinding != ready.candidateBinding || ready.lease == nil || ready.binding.lease != ready.lease || ready.consumed == nil || ready.consumed.Load() != consumed || ready.target != ready.prior.target || ready.journal != ready.prior.journal || ready.revision != ready.prior.revision || !ready.lease.Active() || ready.binding.canonical == ([32]byte{}) || ready.binding.canonical != historicalSuccessorGenerationHandoffDigest(ready) {
		return false
	}
	value, ok := historicalSuccessorGenerationHandoffRegistry.Load(ready)
	record, recordOK := value.(historicalSuccessorGenerationHandoffRecord)
	if !ok || !recordOK || record.ready != ready || record.binding != ready.binding || record.prior != ready.prior || record.priorBinding != ready.prior.binding || record.priorCanonical != ready.prior.binding.canonical || record.candidateBinding != ready.candidateBinding || record.lease != ready.lease || record.source != ready.prior.source || record.planned != ready.prior.planned || record.authority != ready.prior.authority || record.receipt != ready.prior.receipt || record.canonical != ready.binding.canonical || !validConsumedHistoricalSuccessorGenerationReadyPermit(ready.prior, record, candidate) {
		return false
	}
	target, targetErr := ready.lease.Target()
	journal, journalErr := ready.lease.Journal()
	return targetErr == nil && journalErr == nil && target == ready.target && journal == digestRaw(ready.journal)
}

func validConsumedHistoricalSuccessorGenerationReadyPermit(permit *HistoricalSuccessorGenerationReadyPermit, record historicalSuccessorGenerationHandoffRecord, candidate OwnedCurrentCandidate) bool {
	if !validOwnedCurrentCandidate(candidate) || permit == nil || permit.self != permit || permit.binding == nil || permit.binding.permit != permit || permit.owner == nil || permit.owner != candidate.verifiedRun.currentDecision.owner || permit.candidateBinding != candidate.binding || permit.binding.candidateBinding != candidate.binding || permit.inventory == nil || permit.binding.inventory != permit.inventory || permit.mutation == nil || permit.binding.mutation != permit.mutation || permit.source == nil || permit.binding.source != permit.source || permit.planned == nil || permit.binding.planned != permit.planned || permit.authority == nil || permit.binding.authority != permit.authority || permit.receipt == nil || permit.binding.receipt != permit.receipt || permit.consumed == nil || !permit.consumed.Load() || permit.mutation.ValidFor(permit.inventory) || permit.binding.canonical == ([32]byte{}) || permit.binding.canonical != record.priorCanonical || historicalSuccessorGenerationReadyDigest(permit) != record.priorCanonical {
		return false
	}
	if _, stillRegistered := historicalSuccessorGenerationReadyRegistry.Load(permit.binding); stillRegistered {
		return false
	}
	return record.prior == permit && record.priorBinding == permit.binding && record.source == permit.source && record.planned == permit.planned && record.authority == permit.authority && record.receipt == permit.receipt && validHistoricalSuccessorRegisteredInputs(permit.target, record.source, record.planned, permit.plannedRuntime, record.authority, record.receipt, candidate)
}

// Replay consumes the retained handoff and verifies the exact durable
// Superseded/Reserved/Activated suffix plus the header-only generation.
func (r *HistoricalSuccessorGenerationHandoffReady) Replay(ctx context.Context, candidate OwnedCurrentCandidate) (HistoricalSuccessorGenerationReplayResult, error) {
	var result HistoricalSuccessorGenerationReplayResult
	if !validHistoricalSuccessorGenerationHandoffReady(r, candidate) {
		return result, fail(CodeEvidenceRecoveryRequired, "historical-successor-replay", "historical successor handoff authority is unavailable", nil)
	}
	if err := contextAdmissionError(ctx); err != nil {
		return result, err
	}
	if !r.consumed.CompareAndSwap(false, true) {
		return result, fail(CodeEvidenceRecoveryRequired, "historical-successor-replay", "historical successor handoff authority is consumed", nil)
	}
	snapshot, err := r.lease.Snapshot(ctx)
	if err != nil || snapshot == nil {
		if isPreMutationReplayError(err) && r.lease.Active() {
			r.consumed.CompareAndSwap(true, false)
			return result, mapEvidenceAdmissionError(err, "historical-successor-replay-snapshot")
		}
		return r.failHistoricalSuccessorReplay(err, "historical-successor-replay-snapshot")
	}
	return r.replayHistoricalSuccessorSnapshot(ctx, candidate, snapshot)
}

func (r *HistoricalSuccessorGenerationHandoffReady) replayHistoricalSuccessorSnapshot(ctx context.Context, candidate OwnedCurrentCandidate, snapshot *evidencefs.GenerationSnapshot) (HistoricalSuccessorGenerationReplayResult, error) {
	var result HistoricalSuccessorGenerationReplayResult
	if r == nil || snapshot == nil || !validConsumedHistoricalSuccessorGenerationHandoffReady(r, candidate) || !r.lease.OwnsSnapshot(snapshot) {
		return result, fail(CodeEvidenceRecoveryRequired, "historical-successor-replay", "consumed historical successor handoff authority is unavailable", nil)
	}
	indexRaw, err := snapshot.ReadIndex(ctx)
	if err != nil {
		return r.failHistoricalSuccessorReplay(err, "historical-successor-replay-index")
	}
	indexFact, err := snapshot.IndexFact()
	if err != nil || indexFact.Size != uint64(len(indexRaw)) || indexFact.ContentDigest != sha256.Sum256(indexRaw) {
		return r.failHistoricalSuccessorReplay(admissionCorrupt("historical-successor-replay-index", "index fact and bytes differ", err), "historical-successor-replay-index")
	}
	frames, err := decodeAdmissionLineageFrames(indexRaw)
	if err != nil {
		return r.failHistoricalSuccessorReplay(err, "historical-successor-replay-index")
	}
	plan, err := scanLineageChainStructure(frames)
	if err != nil {
		return r.failHistoricalSuccessorReplay(admissionCorrupt("historical-successor-replay-index", "lineage structure is invalid", err), "historical-successor-replay-index")
	}
	if err := validateHistoricalSuccessorReplayIndex(r.prior, indexRaw, frames, indexFact); err != nil {
		return r.failHistoricalSuccessorReplay(err, "historical-successor-replay-index")
	}
	count, err := snapshot.SegmentCount()
	if err != nil || count != 1 {
		return r.failHistoricalSuccessorReplay(admissionCorrupt("historical-successor-replay-segments", "historical successor journal is not header-only", err), "historical-successor-replay-segments")
	}
	stream, registered := openEvidenceJournalStructuralStream(plan, r.journal, nil)
	if !registered || stream == nil {
		return r.failHistoricalSuccessorReplay(admissionCorrupt("historical-successor-replay-journal", "historical successor journal is not registered", nil), "historical-successor-replay-journal")
	}
	raw, err := snapshot.ReadSegment(ctx, 0)
	if err != nil {
		return r.failHistoricalSuccessorReplay(err, "historical-successor-replay-segment")
	}
	segmentFact, err := snapshot.SegmentFact(0)
	if err != nil || segmentFact.Ordinal != 0 || segmentFact.Size != uint64(len(raw)) || segmentFact.ContentDigest != sha256.Sum256(raw) {
		return r.failHistoricalSuccessorReplay(admissionCorrupt("historical-successor-replay-segment", "segment fact and bytes differ", err), "historical-successor-replay-segment")
	}
	if err := stream.beginSegment(); err != nil {
		return r.failHistoricalSuccessorReplay(admissionCorrupt("historical-successor-replay-journal", "historical successor segment cannot begin", err), "historical-successor-replay-journal")
	}
	records, tail, first, err := streamGenerationReplaySegment(raw, stream)
	if err != nil {
		return r.failHistoricalSuccessorReplay(err, "historical-successor-replay-journal")
	}
	if err := stream.endSegment(); err != nil {
		return r.failHistoricalSuccessorReplay(admissionCorrupt("historical-successor-replay-journal", "historical successor segment cannot end", err), "historical-successor-replay-journal")
	}
	replay, err := stream.finish()
	if err != nil || replay == nil || records != 1 || tail != r.prior.headerFrame.RecordDigest || replay.records != records || replay.segments != 1 || replay.tailDigest != tail || !canonicalEqual(first, r.prior.headerFrame) || !bytes.Equal(raw, r.prior.headerBytes) {
		return r.failHistoricalSuccessorReplay(admissionCorrupt("historical-successor-replay-journal", "historical successor journal is not exact header-only state", err), "historical-successor-replay-journal")
	}
	journalPlan := plan.journals[r.journal]
	if journalPlan == nil || !journalPlan.activated || !journalPlan.active || journalPlan.supersededOutcome != "" || journalPlan.checkpointNext != 0 || len(journalPlan.requirements) != 0 || plan.finalReserved != nil || plan.acceptJournal(r.journal, replay) != nil {
		return r.failHistoricalSuccessorReplay(admissionCorrupt("historical-successor-replay-journal", "historical successor journal differs from active registration", nil), "historical-successor-replay-journal")
	}
	if err := snapshot.Revalidate(ctx); err != nil {
		return r.failHistoricalSuccessorReplay(err, "historical-successor-replay-terminal")
	}
	identity, err := snapshot.IdentityDigest()
	if err != nil || identity == ([32]byte{}) {
		return r.failHistoricalSuccessorReplay(evidencefs.ErrCorrupt, "historical-successor-replay-terminal")
	}
	ready := &HistoricalSuccessorGenerationReplayReady{
		prior: r, candidateBinding: r.candidateBinding, lease: r.lease, snapshot: snapshot,
		target: r.target, journal: r.journal, revision: r.revision, snapshotIdentity: identity,
		indexFact: indexFact, segmentFact: segmentFact, indexRecords: uint64(len(frames)), segmentCount: count,
		journalRecords: records, journalTail: tail, consumed: &atomic.Bool{},
	}
	ready.self = ready
	ready.binding = &historicalSuccessorGenerationReplayBinding{ready: ready, prior: r, candidateBinding: r.candidateBinding, lease: r.lease, snapshot: snapshot}
	ready.binding.canonical = historicalSuccessorGenerationReplayDigest(ready)
	historicalSuccessorGenerationReplayRegistry.Store(ready, historicalSuccessorGenerationReplayRecord{
		ready: ready, binding: ready.binding, prior: r, priorBinding: r.binding, candidateBinding: r.candidateBinding,
		lease: r.lease, snapshot: snapshot, canonical: ready.binding.canonical,
	})
	if !validHistoricalSuccessorGenerationReplayReady(ready, candidate) {
		historicalSuccessorGenerationReplayRegistry.Delete(ready)
		return r.failHistoricalSuccessorReplay(evidencefs.ErrUnknown, "historical-successor-replay-seal")
	}
	result.next = ready
	result.snapshotIdentity = identity
	result.indexRecords = ready.indexRecords
	result.segmentCount = count
	result.journalRecords = records
	result.journalTail = tail
	return result, nil
}

func validateHistoricalSuccessorReplayIndex(prior *HistoricalSuccessorGenerationReadyPermit, raw []byte, frames []LineageIndexFrame, fact evidencefs.GenerationFileFact) error {
	if prior == nil || len(raw) == 0 || len(frames) < 4 || fact.Ordinal != 0 || fact.Size != uint64(len(raw)) || fact.ContentDigest != sha256.Sum256(raw) || fact.ContentDigest != prior.indexDigest || fact.Size != prior.indexSize || uint64(len(frames)) != prior.indexRecords || frames[len(frames)-1].RecordDigest != prior.activatedFrame.RecordDigest || prior.indexTail != prior.activatedFrame.RecordDigest {
		return admissionCorrupt("historical-successor-replay-index", "historical successor index identity differs from handoff", nil)
	}
	want := []LineageIndexFrame{prior.reservedFrame, prior.activatedFrame}
	start := len(frames) - len(want)
	for index := range want {
		if !canonicalEqual(frames[start+index], want[index]) {
			return admissionCorrupt("historical-successor-replay-index", "historical successor index tail differs from planned chain", nil)
		}
	}
	superseded := frames[start-1]
	if superseded.RecordKind != LineageRecordGenerationSuperseded || superseded.Record.Superseded == nil || prior.reservedFrame.PreviousRecordDigest == nil || superseded.RecordDigest != *prior.reservedFrame.PreviousRecordDigest || superseded.Record.Superseded.LineageSupersessionAuthorityDigest != prior.authority.digest || !canonicalEqual(superseded.Record.Superseded.PlannedGenerationReserved, prior.reservedFrame.Record.Reserved) {
		return admissionCorrupt("historical-successor-replay-index", "historical supersession predecessor differs", nil)
	}
	plannedTail := make([]byte, 0)
	for _, frame := range []LineageIndexFrame{superseded, prior.reservedFrame, prior.activatedFrame} {
		encoded, err := EncodeCanonicalLineageFrame(frame)
		if err != nil {
			return admissionCorrupt("historical-successor-replay-index", "historical successor tail cannot be encoded", err)
		}
		plannedTail = append(plannedTail, encoded...)
	}
	if len(raw) < len(plannedTail) || !bytes.Equal(raw[len(raw)-len(plannedTail):], plannedTail) {
		return admissionCorrupt("historical-successor-replay-index", "historical successor adjacent bytes differ", nil)
	}
	return nil
}

func historicalSuccessorGenerationReplayDigest(ready *HistoricalSuccessorGenerationReplayReady) [32]byte {
	if ready == nil || ready.self != ready || ready.prior == nil || ready.prior.binding == nil || ready.candidateBinding == nil || ready.lease == nil || ready.snapshot == nil || ready.snapshotIdentity == ([32]byte{}) || ready.indexFact.ContentDigest == ([32]byte{}) || ready.indexFact.IdentityDigest == ([32]byte{}) || ready.indexFact.Size == 0 || ready.segmentFact.ContentDigest == ([32]byte{}) || ready.segmentFact.IdentityDigest == ([32]byte{}) || ready.segmentFact.Size == 0 || ready.indexRecords == 0 || ready.segmentCount != 1 || ready.journalRecords != 1 || ready.journalTail.Validate() != nil {
		return [32]byte{}
	}
	h := sha256.New()
	h.Write([]byte("cloud-agents-platform-historical-successor-replay-ready/v1\x00"))
	h.Write(ready.prior.binding.canonical[:])
	h.Write(ready.candidateBinding.canonical[:])
	h.Write(ready.target[:])
	writeAdmissionString(h, ready.journal.String())
	writeAdmissionUint(h, ready.revision)
	h.Write(ready.snapshotIdentity[:])
	writeSuccessorGenerationFileFact(h, ready.indexFact)
	writeSuccessorGenerationFileFact(h, ready.segmentFact)
	writeAdmissionUint(h, ready.indexRecords)
	writeAdmissionUint(h, uint64(ready.segmentCount))
	writeAdmissionUint(h, ready.journalRecords)
	writeAdmissionString(h, ready.journalTail.String())
	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result
}

func validHistoricalSuccessorGenerationReplayReady(ready *HistoricalSuccessorGenerationReplayReady, candidate OwnedCurrentCandidate) bool {
	return validHistoricalSuccessorGenerationReplayShape(ready, candidate, false)
}

func validConsumedHistoricalSuccessorGenerationReplayReady(ready *HistoricalSuccessorGenerationReplayReady, candidate OwnedCurrentCandidate) bool {
	return validHistoricalSuccessorGenerationReplayShape(ready, candidate, true)
}

func validHistoricalSuccessorGenerationReplayShape(ready *HistoricalSuccessorGenerationReplayReady, candidate OwnedCurrentCandidate, consumed bool) bool {
	if ready == nil || ready.self != ready || ready.binding == nil || ready.binding.ready != ready || ready.prior == nil || ready.prior.binding == nil || ready.candidateBinding != candidate.binding || ready.binding.prior != ready.prior || ready.binding.candidateBinding != ready.candidateBinding || ready.lease == nil || ready.binding.lease != ready.lease || ready.snapshot == nil || ready.binding.snapshot != ready.snapshot || ready.consumed == nil || ready.consumed.Load() != consumed || !validConsumedHistoricalSuccessorGenerationHandoffReady(ready.prior, candidate) || ready.target != ready.prior.target || ready.journal != ready.prior.journal || ready.revision != ready.prior.revision || ready.indexRecords != ready.prior.prior.indexRecords || ready.segmentCount != 1 || ready.journalRecords != 1 || ready.journalTail != ready.prior.prior.headerFrame.RecordDigest || !ready.lease.Active() || ready.binding.canonical == ([32]byte{}) || ready.binding.canonical != historicalSuccessorGenerationReplayDigest(ready) {
		return false
	}
	identity, identityErr := ready.snapshot.IdentityDigest()
	indexFact, indexErr := ready.snapshot.IndexFact()
	segmentFact, segmentErr := ready.snapshot.SegmentFact(0)
	count, countErr := ready.snapshot.SegmentCount()
	if identityErr != nil || indexErr != nil || segmentErr != nil || countErr != nil || identity != ready.snapshotIdentity || indexFact != ready.indexFact || segmentFact != ready.segmentFact || count != ready.segmentCount || !ready.lease.OwnsSnapshot(ready.snapshot) {
		return false
	}
	value, ok := historicalSuccessorGenerationReplayRegistry.Load(ready)
	record, recordOK := value.(historicalSuccessorGenerationReplayRecord)
	return ok && recordOK && record.ready == ready && record.binding == ready.binding && record.prior == ready.prior && record.priorBinding == ready.prior.binding && record.candidateBinding == ready.candidateBinding && record.lease == ready.lease && record.snapshot == ready.snapshot && record.canonical == ready.binding.canonical
}

func (r *HistoricalSuccessorGenerationHandoffReady) failHistoricalSuccessorReplay(cause error, operation string) (HistoricalSuccessorGenerationReplayResult, error) {
	if r != nil {
		historicalSuccessorGenerationHandoffRegistry.Delete(r)
	}
	cleanupErr := error(nil)
	if r != nil && r.lease != nil {
		cleanupErr = r.lease.Close()
	}
	if r != nil && r.prior != nil {
		revokeVerifiedAdmissionRegisteredGeneration(r.prior.source)
		revokeVerifiedAdmissionRegisteredGeneration(r.prior.planned)
	}
	if cleanupErr != nil {
		return HistoricalSuccessorGenerationReplayResult{}, mapEvidenceAdmissionError(cleanupErr, operation+"-cleanup")
	}
	if cause == nil {
		cause = evidencefs.ErrCorrupt
	}
	if IsCode(cause, CodeEvidenceJournalCorrupt) || IsCode(cause, CodeEvidenceJournalFailed) || IsCode(cause, CodeContextCanceled) || IsCode(cause, CodeDeadlineExceeded) {
		return HistoricalSuccessorGenerationReplayResult{}, cause
	}
	return HistoricalSuccessorGenerationReplayResult{}, mapEvidenceAdmissionError(cause, operation)
}

// Close releases the retained generation lease before replay.
func (r *HistoricalSuccessorGenerationHandoffReady) Close() error {
	if r == nil || r.self != r || r.consumed == nil || !r.consumed.CompareAndSwap(false, true) {
		return admissionFailed("historical-successor-handoff-close", "historical successor handoff authority is unavailable", nil)
	}
	value, ok := historicalSuccessorGenerationHandoffRegistry.Load(r)
	record, recordOK := value.(historicalSuccessorGenerationHandoffRecord)
	historicalSuccessorGenerationHandoffRegistry.Delete(r)
	if !ok || !recordOK || record.ready != r || record.lease == nil || record.canonical == ([32]byte{}) {
		return admissionFailed("historical-successor-handoff-close", "immutable historical successor lease is unavailable", nil)
	}
	revokeVerifiedAdmissionRegisteredGeneration(record.source)
	revokeVerifiedAdmissionRegisteredGeneration(record.planned)
	if err := record.lease.Close(); err != nil {
		return mapEvidenceAdmissionError(err, "historical-successor-handoff-close")
	}
	return nil
}

// Close releases the retained generation lease after strict replay.
func (r *HistoricalSuccessorGenerationReplayReady) Close() error {
	if r == nil || r.self != r || r.consumed == nil || !r.consumed.CompareAndSwap(false, true) {
		return admissionFailed("historical-successor-replay-close", "historical successor replay authority is unavailable", nil)
	}
	return closeHistoricalSuccessorGenerationReplay(r, "historical-successor-replay-close")
}

func closeHistoricalSuccessorGenerationReplay(r *HistoricalSuccessorGenerationReplayReady, operation string) error {
	if r == nil || r.self != r || operation == "" {
		return admissionFailed(operation, "historical successor replay authority is unavailable", nil)
	}
	value, ok := historicalSuccessorGenerationReplayRegistry.Load(r)
	record, recordOK := value.(historicalSuccessorGenerationReplayRecord)
	historicalSuccessorGenerationReplayRegistry.Delete(r)
	if !ok || !recordOK || record.ready != r || record.lease == nil || record.canonical == ([32]byte{}) {
		return admissionFailed(operation, "immutable historical successor lease is unavailable", nil)
	}
	if record.prior != nil {
		historicalSuccessorGenerationHandoffRegistry.Delete(record.prior)
		if record.prior.prior != nil {
			revokeVerifiedAdmissionRegisteredGeneration(record.prior.prior.source)
			revokeVerifiedAdmissionRegisteredGeneration(record.prior.prior.planned)
		}
	}
	if err := record.lease.Close(); err != nil {
		return mapEvidenceAdmissionError(err, operation)
	}
	return nil
}
