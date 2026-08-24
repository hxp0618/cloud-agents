package migration

import (
	"bytes"
	"context"
	"crypto/sha256"
	"sync"
	"sync/atomic"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/evidencefs"
)

// SuccessorGenerationHandoffResult is the closed N+10 transition from the
// full-root admission critical section to one retained lineage/generation
// lease. It performs no journal append and mints no runtime authority.
type SuccessorGenerationHandoffResult struct {
	outcome           evidencefs.AdmissionTransitionOutcome
	next              *SuccessorGenerationHandoffReady
	candidateDigest   [32]byte
	candidateSequence uint64
	revision          uint64
}

func (r SuccessorGenerationHandoffResult) Outcome() evidencefs.AdmissionTransitionOutcome {
	return r.outcome
}
func (r SuccessorGenerationHandoffResult) Next() *SuccessorGenerationHandoffReady {
	return r.next
}
func (r SuccessorGenerationHandoffResult) CandidateKind() string { return "generation_handoff" }
func (r SuccessorGenerationHandoffResult) CandidateDigest() [32]byte {
	return r.candidateDigest
}
func (r SuccessorGenerationHandoffResult) CandidateSequence() uint64 {
	return r.candidateSequence
}
func (r SuccessorGenerationHandoffResult) Revision() uint64 { return r.revision }

// SuccessorGenerationHandoffReady proves that the full-root admission locks
// were released and only the exact successor lineage/generation locks remain.
// It is non-runnable and exposes no cursor, append, runner, or database seam.
type SuccessorGenerationHandoffReady struct {
	self             *SuccessorGenerationHandoffReady
	prior            *SuccessorGenerationReadyPermit
	state            *successorAdmissionState
	candidateBinding *verifiedEvidenceRunBinding
	lease            *evidencefs.GenerationLease
	target           [32]byte
	journal          Digest
	revision         uint64
	binding          *successorGenerationHandoffBinding
	consumed         *atomic.Bool
}

type successorGenerationHandoffBinding struct {
	ready            *SuccessorGenerationHandoffReady
	prior            *SuccessorGenerationReadyPermit
	state            *successorAdmissionState
	stateBinding     *successorAdmissionStateBinding
	candidateBinding *verifiedEvidenceRunBinding
	lease            *evidencefs.GenerationLease
	canonical        [32]byte
}

type successorGenerationHandoffRecord struct {
	ready            *SuccessorGenerationHandoffReady
	binding          *successorGenerationHandoffBinding
	prior            *SuccessorGenerationReadyPermit
	state            *successorAdmissionState
	stateBinding     *successorAdmissionStateBinding
	stateRecord      successorAdmissionStateRecord
	candidateBinding *verifiedEvidenceRunBinding
	lease            *evidencefs.GenerationLease
	runtimeReceipt   VerifiedContentReceipt
	recoveryReceipt  VerifiedDecisionRecoveryReceipt
	canonical        [32]byte
}

var successorGenerationHandoffRegistry sync.Map

// SuccessorGenerationReplayResult is read-only diagnostics for the strict
// replay transition. Only Next carries the sealed, still non-runnable value.
type SuccessorGenerationReplayResult struct {
	next             *SuccessorGenerationReplayReady
	snapshotIdentity [32]byte
	indexRecords     uint64
	segmentCount     uint32
	journalRecords   uint64
	journalTail      Digest
}

func (r SuccessorGenerationReplayResult) Next() *SuccessorGenerationReplayReady { return r.next }
func (r SuccessorGenerationReplayResult) SnapshotIdentity() [32]byte {
	return r.snapshotIdentity
}
func (r SuccessorGenerationReplayResult) IndexRecords() uint64   { return r.indexRecords }
func (r SuccessorGenerationReplayResult) SegmentCount() uint32   { return r.segmentCount }
func (r SuccessorGenerationReplayResult) JournalRecords() uint64 { return r.journalRecords }
func (r SuccessorGenerationReplayResult) JournalTail() Digest    { return r.journalTail }

// SuccessorGenerationReplayReady retains the exact GenerationSnapshot after
// strict index and current-journal replay. A later same-verifier recovery
// binder must consume it before a normal-run journal can exist.
type SuccessorGenerationReplayReady struct {
	self             *SuccessorGenerationReplayReady
	prior            *SuccessorGenerationHandoffReady
	state            *successorAdmissionState
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
	binding          *successorGenerationReplayBinding
	consumed         *atomic.Bool
}

type successorGenerationReplayBinding struct {
	ready            *SuccessorGenerationReplayReady
	prior            *SuccessorGenerationHandoffReady
	state            *successorAdmissionState
	stateBinding     *successorAdmissionStateBinding
	candidateBinding *verifiedEvidenceRunBinding
	lease            *evidencefs.GenerationLease
	snapshot         *evidencefs.GenerationSnapshot
	canonical        [32]byte
}

type successorGenerationReplayRecord struct {
	ready            *SuccessorGenerationReplayReady
	binding          *successorGenerationReplayBinding
	prior            *SuccessorGenerationHandoffReady
	state            *successorAdmissionState
	stateBinding     *successorAdmissionStateBinding
	candidateBinding *verifiedEvidenceRunBinding
	lease            *evidencefs.GenerationLease
	snapshot         *evidencefs.GenerationSnapshot
	canonical        [32]byte
}

var successorGenerationReplayRegistry sync.Map

// Handoff consumes successor generation-ready authority and irreversibly
// transfers filesystem ownership to one retained generation lease.
func (p *SuccessorGenerationReadyPermit) Handoff(ctx context.Context, candidate OwnedCurrentCandidate) (SuccessorGenerationHandoffResult, error) {
	pre := SuccessorGenerationHandoffResult{outcome: evidencefs.AdmissionTransitionPreMutationFailure, candidateSequence: 10}
	if p == nil || p.self != p || p.state == nil || !validSuccessorAdmissionState(p, p.state, successorAdmissionGenerationReady, candidate) {
		return pre, fail(CodeEvidenceRecoveryRequired, "successor-generation-handoff", "successor generation-ready authority is unavailable", nil)
	}
	if err := contextAdmissionError(ctx); err != nil {
		return pre, err
	}
	state := p.state
	registered, ok := successorAdmissionStateRegistry.Load(state.binding)
	source, sourceOK := registered.(successorAdmissionStateRecord)
	if !ok || !sourceOK || !successorAdmissionSourceRecordMatches(source, p, state) {
		return pre, fail(CodeEvidenceRecoveryRequired, "successor-generation-handoff", "immutable successor generation state is unavailable", nil)
	}
	pre.revision = state.revision
	pre.candidateDigest = successorGenerationHandoffCandidateDigest(p)
	if pre.candidateDigest == ([32]byte{}) || !state.consumed.CompareAndSwap(false, true) {
		return pre, fail(CodeEvidenceRecoveryRequired, "successor-generation-handoff", "successor generation-ready authority is invalid or consumed", nil)
	}
	fsLease, transitionErr := state.mutation.HandoffGeneration(ctx, state.inventory, digestRaw(state.journal))
	result := SuccessorGenerationHandoffResult{
		outcome: evidencefs.AdmissionTransitionDurable, candidateDigest: pre.candidateDigest,
		candidateSequence: pre.candidateSequence, revision: pre.revision,
	}
	if transitionErr != nil || fsLease == nil || !fsLease.Active() {
		if fsLease != nil {
			_ = fsLease.Close()
			successorAdmissionStateRegistry.Delete(state.binding)
			result.outcome = evidencefs.AdmissionTransitionUnknown
			return result, admissionPostMutationFailure("successor-generation-handoff")
		}
		if state.mutation.ValidFor(state.inventory) {
			state.consumed.CompareAndSwap(true, false)
			result.outcome = evidencefs.AdmissionTransitionPreMutationFailure
			return result, mapAdmissionMutationError(transitionErr, "successor-generation-handoff")
		}
		successorAdmissionStateRegistry.Delete(state.binding)
		result.outcome = evidencefs.AdmissionTransitionUnknown
		return result, admissionPostMutationFailure("successor-generation-handoff")
	}
	target, targetErr := fsLease.Target()
	journal, journalErr := fsLease.Journal()
	if targetErr != nil || journalErr != nil || target != state.target || journal != digestRaw(state.journal) {
		_ = fsLease.Close()
		successorAdmissionStateRegistry.Delete(state.binding)
		result.outcome = evidencefs.AdmissionTransitionUnknown
		return result, admissionPostMutationFailure("successor-generation-handoff-bind")
	}
	ready := &SuccessorGenerationHandoffReady{
		prior: p, state: state, candidateBinding: candidate.binding, lease: fsLease,
		target: target, journal: state.journal, revision: state.revision, consumed: &atomic.Bool{},
	}
	ready.self = ready
	ready.binding = &successorGenerationHandoffBinding{
		ready: ready, prior: p, state: state, stateBinding: state.binding, candidateBinding: candidate.binding, lease: fsLease,
	}
	ready.binding.canonical = successorGenerationHandoffDigest(ready)
	successorGenerationHandoffRegistry.Store(ready, successorGenerationHandoffRecord{
		ready: ready, binding: ready.binding, prior: p, state: state, stateBinding: state.binding, stateRecord: source,
		candidateBinding: candidate.binding, lease: fsLease, runtimeReceipt: state.runtimeReceipt,
		recoveryReceipt: state.recoveryReceipt, canonical: ready.binding.canonical,
	})
	// The old full-root state is diagnostic only after the exact retained lease
	// exists. Removing its registry entry makes revival impossible.
	successorAdmissionStateRegistry.Delete(state.binding)
	if !validSuccessorGenerationHandoffReady(ready, candidate) {
		successorGenerationHandoffRegistry.Delete(ready)
		_ = fsLease.Close()
		result.outcome = evidencefs.AdmissionTransitionUnknown
		return result, admissionPostMutationFailure("successor-generation-handoff-seal")
	}
	result.next = ready
	return result, nil
}

func successorGenerationHandoffCandidateDigest(permit *SuccessorGenerationReadyPermit) [32]byte {
	if permit == nil || permit.self != permit || permit.state == nil || permit.state.binding == nil {
		return [32]byte{}
	}
	h := sha256.New()
	h.Write([]byte("cloud-agents-platform-successor-generation-handoff-candidate/v1\x00"))
	h.Write(permit.state.binding.canonical[:])
	writeAdmissionUint(h, permit.state.revision)
	writeAdmissionString(h, permit.state.journal.String())
	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result
}

func successorGenerationHandoffDigest(ready *SuccessorGenerationHandoffReady) [32]byte {
	if ready == nil || ready.self != ready || ready.prior == nil || ready.state == nil || ready.state.binding == nil || ready.candidateBinding == nil || ready.lease == nil || ready.target == ([32]byte{}) || ready.journal.Validate() != nil {
		return [32]byte{}
	}
	h := sha256.New()
	h.Write([]byte("cloud-agents-platform-successor-generation-handoff-ready/v1\x00"))
	h.Write(ready.state.binding.canonical[:])
	h.Write(ready.candidateBinding.canonical[:])
	h.Write(ready.target[:])
	writeAdmissionString(h, ready.journal.String())
	writeAdmissionUint(h, ready.revision)
	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result
}

func successorAdmissionSourceRecordMatches(record successorAdmissionStateRecord, owner *SuccessorGenerationReadyPermit, state *successorAdmissionState) bool {
	return owner != nil && owner.self == owner && owner.state == state && state != nil && state.binding != nil &&
		record.state == state && record.owner == owner && record.prior == state.prior && record.plan == state.plan &&
		record.history == state.history && record.candidateBinding == state.candidateBinding && record.inventory == state.inventory &&
		record.mutation == state.mutation && record.runtimePublication == state.runtimePublication &&
		record.recoveryPublication == state.recoveryPublication && record.runtimeReceiptBinding == state.runtimeReceipt.binding &&
		record.recoveryReceiptBinding == state.recoveryReceipt.binding && record.consumed == state.consumed &&
		record.canonical != ([32]byte{}) && record.canonical == state.binding.canonical
}

func validConsumedSuccessorGenerationReadyState(owner *SuccessorGenerationReadyPermit, state *successorAdmissionState, source successorAdmissionStateRecord, candidate OwnedCurrentCandidate) bool {
	if !successorAdmissionSourceRecordMatches(source, owner, state) || state.self != state || state.stage != successorAdmissionGenerationReady || state.binding.state != state || state.binding.owner != owner || state.binding.prior != state.prior || state.binding.plan != state.plan || state.binding.history != state.history || state.binding.inventory != state.inventory || state.binding.mutation != state.mutation || state.binding.candidateBinding != state.candidateBinding || state.consumed == nil || !state.consumed.Load() || !validOwnedCurrentCandidate(candidate) || state.candidateBinding != candidate.binding || state.binding.candidateBinding != candidate.binding || state.runtimeDigest != candidate.runtimeArtifact.digest || state.runtimeSize != candidate.runtimeArtifact.sizeBytes || state.recoveryDigest != candidate.decisionRecoveryArtifact.digest || state.recoverySize != candidate.decisionRecoveryArtifact.sizeBytes || state.binding.canonical != successorAdmissionStateDigest(state) || !validSuccessorAdmissionStageShape(state) || !validConsumedVerifiedSuccessorAdmissionPlan(state.plan, candidate) || !successorAdmissionPlanFramesExact(state.plan) || !successorReceiptsExact(state, candidate) || state.inventory == nil || state.mutation == nil || state.mutation.ValidFor(state.inventory) {
		return false
	}
	_, stillRegistered := successorAdmissionStateRegistry.Load(state.binding)
	return !stillRegistered
}

func validSuccessorGenerationHandoffReady(ready *SuccessorGenerationHandoffReady, candidate OwnedCurrentCandidate) bool {
	return validSuccessorGenerationHandoffShape(ready, candidate, false)
}

func validConsumedSuccessorGenerationHandoffReady(ready *SuccessorGenerationHandoffReady, candidate OwnedCurrentCandidate) bool {
	return validSuccessorGenerationHandoffShape(ready, candidate, true)
}

func validSuccessorGenerationHandoffShape(ready *SuccessorGenerationHandoffReady, candidate OwnedCurrentCandidate, consumed bool) bool {
	if ready == nil || ready.self != ready || ready.binding == nil || ready.binding.ready != ready || ready.prior == nil || ready.state == nil || ready.candidateBinding != candidate.binding || ready.lease == nil || ready.binding.prior != ready.prior || ready.binding.state != ready.state || ready.binding.stateBinding != ready.state.binding || ready.binding.candidateBinding != ready.candidateBinding || ready.binding.lease != ready.lease || ready.consumed == nil || ready.consumed.Load() != consumed || ready.target != ready.state.target || ready.journal != ready.state.journal || ready.revision != ready.state.revision || !ready.lease.Active() || ready.binding.canonical == ([32]byte{}) || ready.binding.canonical != successorGenerationHandoffDigest(ready) {
		return false
	}
	registered, ok := successorGenerationHandoffRegistry.Load(ready)
	record, recordOK := registered.(successorGenerationHandoffRecord)
	if !ok || !recordOK || record.ready != ready || record.binding != ready.binding || record.prior != ready.prior || record.state != ready.state || record.stateBinding != ready.state.binding || record.candidateBinding != ready.candidateBinding || record.lease != ready.lease || record.runtimeReceipt.binding != ready.state.runtimeReceipt.binding || record.recoveryReceipt.binding != ready.state.recoveryReceipt.binding || record.runtimeReceipt.digest != ready.state.runtimeReceipt.digest || record.runtimeReceipt.sizeBytes != ready.state.runtimeReceipt.sizeBytes || record.runtimeReceipt.publication != ready.state.runtimeReceipt.publication || record.recoveryReceipt.digest != ready.state.recoveryReceipt.digest || record.recoveryReceipt.sizeBytes != ready.state.recoveryReceipt.sizeBytes || record.recoveryReceipt.publication != ready.state.recoveryReceipt.publication || record.canonical != ready.binding.canonical || !validConsumedSuccessorGenerationReadyState(ready.prior, ready.state, record.stateRecord, candidate) {
		return false
	}
	target, targetErr := ready.lease.Target()
	journal, journalErr := ready.lease.Journal()
	return targetErr == nil && journalErr == nil && target == ready.target && journal == digestRaw(ready.journal)
}

// Replay consumes the retained-lock handoff and strictly verifies the full
// lineage index plus the new successor's still-header-only journal.
func (r *SuccessorGenerationHandoffReady) Replay(ctx context.Context, candidate OwnedCurrentCandidate) (SuccessorGenerationReplayResult, error) {
	var result SuccessorGenerationReplayResult
	if r == nil || !validSuccessorGenerationHandoffReady(r, candidate) {
		return result, fail(CodeEvidenceRecoveryRequired, "successor-generation-replay", "successor handoff authority is unavailable", nil)
	}
	if err := contextAdmissionError(ctx); err != nil {
		return result, err
	}
	if !r.consumed.CompareAndSwap(false, true) {
		return result, fail(CodeEvidenceRecoveryRequired, "successor-generation-replay", "successor handoff authority is consumed", nil)
	}
	snapshot, err := r.lease.Snapshot(ctx)
	if err != nil || snapshot == nil {
		if isPreMutationReplayError(err) && r.lease.Active() {
			r.consumed.CompareAndSwap(true, false)
			return result, mapEvidenceAdmissionError(err, "successor-generation-replay-snapshot")
		}
		return r.failSuccessorReplay(err, "successor-generation-replay-snapshot")
	}
	return r.replaySuccessorSnapshot(ctx, candidate, snapshot)
}

func (r *SuccessorGenerationHandoffReady) replaySuccessorSnapshot(ctx context.Context, candidate OwnedCurrentCandidate, snapshot *evidencefs.GenerationSnapshot) (SuccessorGenerationReplayResult, error) {
	var result SuccessorGenerationReplayResult
	if r == nil || snapshot == nil || !validConsumedSuccessorGenerationHandoffReady(r, candidate) || !r.lease.OwnsSnapshot(snapshot) {
		return result, fail(CodeEvidenceRecoveryRequired, "successor-generation-replay", "consumed successor handoff authority is unavailable", nil)
	}
	indexRaw, err := snapshot.ReadIndex(ctx)
	if err != nil {
		return r.failSuccessorReplay(err, "successor-generation-replay-index")
	}
	indexFact, err := snapshot.IndexFact()
	if err != nil {
		return r.failSuccessorReplay(err, "successor-generation-replay-index")
	}
	if indexFact.Size != uint64(len(indexRaw)) || indexFact.ContentDigest != sha256.Sum256(indexRaw) {
		return r.failSuccessorReplay(admissionCorrupt("successor-generation-replay-index", "index fact and bytes differ", nil), "successor-generation-replay-index")
	}
	indexFrames, err := decodeAdmissionLineageFrames(indexRaw)
	if err != nil {
		return r.failSuccessorReplay(err, "successor-generation-replay-index")
	}
	plan, err := scanLineageChainStructure(indexFrames)
	if err != nil {
		return r.failSuccessorReplay(admissionCorrupt("successor-generation-replay-index", "lineage structure is invalid", err), "successor-generation-replay-index")
	}
	if err := r.validateSuccessorReplayIndex(indexRaw, indexFrames, indexFact); err != nil {
		return r.failSuccessorReplay(err, "successor-generation-replay-index")
	}
	count, err := snapshot.SegmentCount()
	if err != nil {
		return r.failSuccessorReplay(err, "successor-generation-replay-segments")
	}
	if count != 1 {
		return r.failSuccessorReplay(admissionCorrupt("successor-generation-replay-segments", "successor journal progressed before recovery binding", nil), "successor-generation-replay-segments")
	}
	stream, registered := openEvidenceJournalStructuralStream(plan, r.journal, nil)
	if !registered || stream == nil {
		return r.failSuccessorReplay(admissionCorrupt("successor-generation-replay-journal", "successor journal is not registered", nil), "successor-generation-replay-journal")
	}
	raw, err := snapshot.ReadSegment(ctx, 0)
	if err != nil {
		return r.failSuccessorReplay(err, "successor-generation-replay-segment")
	}
	segmentFact, err := snapshot.SegmentFact(0)
	if err != nil {
		return r.failSuccessorReplay(err, "successor-generation-replay-segment")
	}
	if segmentFact.Ordinal != 0 || segmentFact.Size != uint64(len(raw)) || segmentFact.ContentDigest != sha256.Sum256(raw) {
		return r.failSuccessorReplay(admissionCorrupt("successor-generation-replay-segment", "segment fact and bytes differ", nil), "successor-generation-replay-segment")
	}
	if err := stream.beginSegment(); err != nil {
		return r.failSuccessorReplay(admissionCorrupt("successor-generation-replay-journal", "successor segment cannot begin", err), "successor-generation-replay-journal")
	}
	records, tail, first, err := streamGenerationReplaySegment(raw, stream)
	if err != nil {
		return r.failSuccessorReplay(err, "successor-generation-replay-journal")
	}
	if err := stream.endSegment(); err != nil {
		return r.failSuccessorReplay(admissionCorrupt("successor-generation-replay-journal", "successor segment cannot end", err), "successor-generation-replay-journal")
	}
	replay, err := stream.finish()
	if err != nil || replay == nil {
		return r.failSuccessorReplay(admissionCorrupt("successor-generation-replay-journal", "successor journal structure is invalid", err), "successor-generation-replay-journal")
	}
	if records != 1 || tail != r.state.headerDigest || replay.records != records || replay.segments != 1 || replay.tailDigest != tail || !canonicalEqual(first, r.state.plan.headerFrame) || !bytes.Equal(raw, r.state.headerBytes) {
		return r.failSuccessorReplay(admissionCorrupt("successor-generation-replay-journal", "successor journal is not exact header-only state", nil), "successor-generation-replay-journal")
	}
	journalPlan := plan.journals[r.journal]
	if journalPlan == nil || !journalPlan.activated || !journalPlan.active || journalPlan.supersededOutcome != "" || journalPlan.checkpointNext != 0 || len(journalPlan.requirements) != 0 || plan.finalReserved != nil {
		return r.failSuccessorReplay(admissionCorrupt("successor-generation-replay-journal", "successor journal is not the current activated generation", nil), "successor-generation-replay-journal")
	}
	if err := plan.acceptJournal(r.journal, replay); err != nil {
		return r.failSuccessorReplay(admissionCorrupt("successor-generation-replay-journal", "successor journal differs from lineage registration", err), "successor-generation-replay-journal")
	}
	if err := snapshot.Revalidate(ctx); err != nil {
		return r.failSuccessorReplay(err, "successor-generation-replay-terminal")
	}
	identity, err := snapshot.IdentityDigest()
	if err != nil || identity == ([32]byte{}) {
		if err == nil {
			err = evidencefs.ErrCorrupt
		}
		return r.failSuccessorReplay(err, "successor-generation-replay-terminal")
	}
	ready := &SuccessorGenerationReplayReady{
		prior: r, state: r.state, candidateBinding: r.candidateBinding, lease: r.lease, snapshot: snapshot,
		target: r.target, journal: r.journal, revision: r.revision, snapshotIdentity: identity,
		indexFact: indexFact, segmentFact: segmentFact, indexRecords: uint64(len(indexFrames)),
		segmentCount: count, journalRecords: records, journalTail: tail, consumed: &atomic.Bool{},
	}
	ready.self = ready
	ready.binding = &successorGenerationReplayBinding{
		ready: ready, prior: r, state: r.state, stateBinding: r.state.binding, candidateBinding: r.candidateBinding, lease: r.lease, snapshot: snapshot,
	}
	ready.binding.canonical = successorGenerationReplayDigest(ready)
	successorGenerationReplayRegistry.Store(ready, successorGenerationReplayRecord{
		ready: ready, binding: ready.binding, prior: r, state: r.state, stateBinding: r.state.binding, candidateBinding: r.candidateBinding,
		lease: r.lease, snapshot: snapshot, canonical: ready.binding.canonical,
	})
	if !validSuccessorGenerationReplayReady(ready, candidate) {
		successorGenerationReplayRegistry.Delete(ready)
		return r.failSuccessorReplay(evidencefs.ErrUnknown, "successor-generation-replay-seal")
	}
	result.next = ready
	result.snapshotIdentity = identity
	result.indexRecords = ready.indexRecords
	result.segmentCount = count
	result.journalRecords = records
	result.journalTail = tail
	return result, nil
}

func (r *SuccessorGenerationHandoffReady) validateSuccessorReplayIndex(raw []byte, frames []LineageIndexFrame, fact evidencefs.GenerationFileFact) error {
	if r == nil || r.state == nil || r.state.plan == nil || r.state.history == nil || len(raw) == 0 || len(frames) < 4 || fact.Ordinal != 0 || fact.Size != uint64(len(raw)) || fact.ContentDigest != sha256.Sum256(raw) || fact.ContentDigest != r.state.indexDigest || fact.Size != r.state.indexSize || uint64(len(frames)) != r.state.indexRecords || r.state.indexRecords != r.state.history.targetIndexRecords+3 || frames[len(frames)-1].RecordDigest != r.state.activationDigest || r.state.indexTail != r.state.activationDigest {
		return admissionCorrupt("successor-generation-replay-index", "successor index identity differs from handoff", nil)
	}
	want := []LineageIndexFrame{r.state.plan.supersededFrame, r.state.plan.reservedFrame, r.state.activatedFrame}
	start := len(frames) - len(want)
	for index := range want {
		if !canonicalEqual(frames[start+index], want[index]) {
			return admissionCorrupt("successor-generation-replay-index", "successor index tail differs from planned chain", nil)
		}
	}
	if r.state.indexPrefixSize == 0 || r.state.indexPrefixSize > uint64(len(raw)) || r.state.indexPrefixSize+uint64(len(r.state.activatedBytes)) != uint64(len(raw)) || sha256.Sum256(raw[:r.state.indexPrefixSize]) != r.state.indexPrefixDigest || !bytes.Equal(raw[r.state.indexPrefixSize:], r.state.activatedBytes) {
		return admissionCorrupt("successor-generation-replay-index", "successor activation boundary differs from durable prefix", nil)
	}
	plannedTail := make([]byte, 0, len(r.state.plan.supersededFrameBytes)+len(r.state.plan.reservedFrameBytes)+len(r.state.activatedBytes))
	plannedTail = append(plannedTail, r.state.plan.supersededFrameBytes...)
	plannedTail = append(plannedTail, r.state.plan.reservedFrameBytes...)
	plannedTail = append(plannedTail, r.state.activatedBytes...)
	if len(raw) < len(plannedTail) || !bytes.Equal(raw[len(raw)-len(plannedTail):], plannedTail) {
		return admissionCorrupt("successor-generation-replay-index", "successor adjacent chain bytes differ", nil)
	}
	return nil
}

func successorGenerationReplayDigest(ready *SuccessorGenerationReplayReady) [32]byte {
	if ready == nil || ready.self != ready || ready.prior == nil || ready.prior.binding == nil || ready.state == nil || ready.state.binding == nil || ready.candidateBinding == nil || ready.lease == nil || ready.snapshot == nil || ready.snapshotIdentity == ([32]byte{}) || ready.indexFact.ContentDigest == ([32]byte{}) || ready.indexFact.IdentityDigest == ([32]byte{}) || ready.indexFact.Size == 0 || ready.segmentFact.ContentDigest == ([32]byte{}) || ready.segmentFact.IdentityDigest == ([32]byte{}) || ready.segmentFact.Size == 0 || ready.indexRecords == 0 || ready.segmentCount == 0 || ready.journalRecords == 0 || ready.journalTail.Validate() != nil {
		return [32]byte{}
	}
	h := sha256.New()
	h.Write([]byte("cloud-agents-platform-successor-generation-replay-ready/v1\x00"))
	h.Write(ready.prior.binding.canonical[:])
	h.Write(ready.state.binding.canonical[:])
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

func writeSuccessorGenerationFileFact(h interface{ Write([]byte) (int, error) }, fact evidencefs.GenerationFileFact) {
	writeAdmissionUint(h, uint64(fact.Ordinal))
	writeAdmissionUint(h, fact.Size)
	_, _ = h.Write(fact.ContentDigest[:])
	_, _ = h.Write(fact.IdentityDigest[:])
}

func validSuccessorGenerationReplayReady(ready *SuccessorGenerationReplayReady, candidate OwnedCurrentCandidate) bool {
	return validSuccessorGenerationReplayShape(ready, candidate, false)
}

func validConsumedSuccessorGenerationReplayReady(ready *SuccessorGenerationReplayReady, candidate OwnedCurrentCandidate) bool {
	return validSuccessorGenerationReplayShape(ready, candidate, true)
}

func validSuccessorGenerationReplayShape(ready *SuccessorGenerationReplayReady, candidate OwnedCurrentCandidate, consumed bool) bool {
	if ready == nil || ready.self != ready || ready.binding == nil || ready.binding.ready != ready || ready.prior == nil || ready.state == nil || ready.candidateBinding != candidate.binding || ready.lease == nil || ready.snapshot == nil || ready.binding.prior != ready.prior || ready.binding.state != ready.state || ready.binding.stateBinding != ready.state.binding || ready.binding.candidateBinding != ready.candidateBinding || ready.binding.lease != ready.lease || ready.binding.snapshot != ready.snapshot || ready.consumed == nil || ready.consumed.Load() != consumed || !validConsumedSuccessorGenerationHandoffReady(ready.prior, candidate) || ready.state != ready.prior.state || ready.target != ready.prior.target || ready.journal != ready.prior.journal || ready.revision != ready.prior.revision || ready.indexRecords != ready.state.indexRecords || ready.segmentCount != 1 || ready.journalRecords != 1 || ready.journalTail != ready.state.headerDigest || !ready.lease.Active() || ready.binding.canonical == ([32]byte{}) || ready.binding.canonical != successorGenerationReplayDigest(ready) {
		return false
	}
	identity, identityErr := ready.snapshot.IdentityDigest()
	indexFact, indexErr := ready.snapshot.IndexFact()
	segmentFact, segmentErr := ready.snapshot.SegmentFact(0)
	count, countErr := ready.snapshot.SegmentCount()
	if identityErr != nil || indexErr != nil || segmentErr != nil || countErr != nil || identity != ready.snapshotIdentity || indexFact != ready.indexFact || segmentFact != ready.segmentFact || count != ready.segmentCount || !ready.lease.OwnsSnapshot(ready.snapshot) {
		return false
	}
	registered, ok := successorGenerationReplayRegistry.Load(ready)
	record, recordOK := registered.(successorGenerationReplayRecord)
	return ok && recordOK && record.ready == ready && record.binding == ready.binding && record.prior == ready.prior && record.state == ready.state && record.stateBinding == ready.state.binding && record.candidateBinding == ready.candidateBinding && record.lease == ready.lease && record.snapshot == ready.snapshot && record.canonical == ready.binding.canonical
}

func (r *SuccessorGenerationHandoffReady) failSuccessorReplay(cause error, operation string) (SuccessorGenerationReplayResult, error) {
	successorGenerationHandoffRegistry.Delete(r)
	if r != nil && r.state != nil && r.state.binding != nil {
		successorAdmissionStateRegistry.Delete(r.state.binding)
	}
	cleanupErr := error(nil)
	if r != nil && r.lease != nil {
		cleanupErr = r.lease.Close()
	}
	if cleanupErr != nil {
		return SuccessorGenerationReplayResult{}, mapEvidenceAdmissionError(cleanupErr, operation+"-cleanup")
	}
	if cause == nil {
		cause = evidencefs.ErrCorrupt
	}
	if IsCode(cause, CodeEvidenceJournalCorrupt) || IsCode(cause, CodeEvidenceJournalFailed) || IsCode(cause, CodeContextCanceled) || IsCode(cause, CodeDeadlineExceeded) {
		return SuccessorGenerationReplayResult{}, cause
	}
	return SuccessorGenerationReplayResult{}, mapEvidenceAdmissionError(cause, operation)
}

// Close releases the retained successor lease before replay.
func (r *SuccessorGenerationHandoffReady) Close() error {
	if r == nil || r.self != r || r.consumed == nil || !r.consumed.CompareAndSwap(false, true) {
		return admissionFailed("successor-generation-handoff-close", "successor handoff authority is unavailable", nil)
	}
	registered, ok := successorGenerationHandoffRegistry.Load(r)
	record, recordOK := registered.(successorGenerationHandoffRecord)
	successorGenerationHandoffRegistry.Delete(r)
	if !ok || !recordOK || record.ready != r || record.lease == nil || record.canonical == ([32]byte{}) {
		return admissionFailed("successor-generation-handoff-close", "immutable successor filesystem lease is unavailable", nil)
	}
	if record.state != nil && record.state.binding != nil {
		successorAdmissionStateRegistry.Delete(record.state.binding)
	}
	if err := record.lease.Close(); err != nil {
		return mapEvidenceAdmissionError(err, "successor-generation-handoff-close")
	}
	return nil
}

// Close releases the retained successor lease after strict replay.
func (r *SuccessorGenerationReplayReady) Close() error {
	if r == nil || r.self != r || r.consumed == nil || !r.consumed.CompareAndSwap(false, true) {
		return admissionFailed("successor-generation-replay-close", "successor replay authority is unavailable", nil)
	}
	return closeSuccessorGenerationReplay(r, "successor-generation-replay-close")
}

func closeSuccessorGenerationReplay(r *SuccessorGenerationReplayReady, operation string) error {
	if r == nil || r.self != r || operation == "" {
		return admissionFailed(operation, "successor replay authority is unavailable", nil)
	}
	registered, ok := successorGenerationReplayRegistry.Load(r)
	record, recordOK := registered.(successorGenerationReplayRecord)
	if !ok || !recordOK || record.ready != r || record.lease == nil || record.canonical == ([32]byte{}) {
		return admissionFailed(operation, "immutable successor filesystem lease is unavailable", nil)
	}
	successorGenerationReplayRegistry.Delete(r)
	if record.prior != nil {
		successorGenerationHandoffRegistry.Delete(record.prior)
	}
	if record.state != nil && record.state.binding != nil {
		successorAdmissionStateRegistry.Delete(record.state.binding)
	}
	if err := record.lease.Close(); err != nil {
		return mapEvidenceAdmissionError(err, operation)
	}
	return nil
}
