package migration

import (
	"context"
	"crypto/sha256"
	"errors"
	"sync"
	"sync/atomic"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/evidencefs"
)

// GenerationHandoffResult is a closed in-memory transition. It records no new
// durable candidate: success only transfers exact filesystem lock ownership
// out of the full-root admission critical section.
type GenerationHandoffResult struct {
	outcome           evidencefs.AdmissionTransitionOutcome
	next              *GenerationHandoffReady
	candidateDigest   [32]byte
	candidateSequence uint64
	revision          uint64
}

func (r GenerationHandoffResult) Outcome() evidencefs.AdmissionTransitionOutcome { return r.outcome }
func (r GenerationHandoffResult) Next() *GenerationHandoffReady                  { return r.next }
func (r GenerationHandoffResult) CandidateKind() string                          { return "generation_handoff" }
func (r GenerationHandoffResult) CandidateDigest() [32]byte                      { return r.candidateDigest }
func (r GenerationHandoffResult) CandidateSequence() uint64                      { return r.candidateSequence }
func (r GenerationHandoffResult) Revision() uint64                               { return r.revision }

// GenerationHandoffReady proves the full-root admission lock is released and
// an opaque evidencefs lease retains only the target lineage and generation
// locks. It is deliberately not ActiveGeneration and cannot connect, append a
// journal record, mint a cursor, or authorize database progress.
type GenerationHandoffReady struct {
	self             *GenerationHandoffReady
	prior            *GenerationReadyPermit
	plan             *VerifiedAdmissionPlan
	history          *VerifiedAdmissionHistory
	candidateBinding *verifiedEvidenceRunBinding
	lease            *evidencefs.GenerationLease
	target           [32]byte
	journal          Digest
	revision         uint64
	reservedDigest   Digest
	headerDigest     Digest
	activationDigest Digest
	binding          *generationHandoffReadyBinding
	consumed         *atomic.Bool
}

type generationHandoffReadyBinding struct {
	ready            *GenerationHandoffReady
	prior            *GenerationReadyPermit
	plan             *VerifiedAdmissionPlan
	history          *VerifiedAdmissionHistory
	candidateBinding *verifiedEvidenceRunBinding
	lease            *evidencefs.GenerationLease
	canonical        [32]byte
}

type generationHandoffReadyRegistryRecord struct {
	ready            *GenerationHandoffReady
	binding          *generationHandoffReadyBinding
	prior            *GenerationReadyPermit
	plan             *VerifiedAdmissionPlan
	history          *VerifiedAdmissionHistory
	candidateBinding *verifiedEvidenceRunBinding
	lease            *evidencefs.GenerationLease
	canonical        [32]byte
}

var generationHandoffReadyRegistry sync.Map

// GenerationReplayResult is a closed, read-only transition from retained
// filesystem locks to a strictly replayed generation state. Success consumes
// the handoff value but still mints no EvidenceJournal, JournalCursor, runner,
// database, or append authority.
type GenerationReplayResult struct {
	next             *GenerationReplayReady
	snapshotIdentity [32]byte
	indexRecords     uint64
	segmentCount     uint32
	journalRecords   uint64
	journalTail      Digest
}

func (r GenerationReplayResult) Next() *GenerationReplayReady { return r.next }
func (r GenerationReplayResult) SnapshotIdentity() [32]byte   { return r.snapshotIdentity }
func (r GenerationReplayResult) IndexRecords() uint64         { return r.indexRecords }
func (r GenerationReplayResult) SegmentCount() uint32         { return r.segmentCount }
func (r GenerationReplayResult) JournalRecords() uint64       { return r.journalRecords }
func (r GenerationReplayResult) JournalTail() Digest          { return r.journalTail }

// GenerationReplayReady retains the exact GenerationLease plus an immutable
// evidencefs snapshot after strict C3 index/journal replay. It is deliberately
// non-runnable: a later same-verifier recovery binder must cross-bind the
// replay before any JournalCursor or normal-run EvidenceJournal can exist.
type GenerationReplayReady struct {
	self             *GenerationReplayReady
	prior            *GenerationHandoffReady
	plan             *VerifiedAdmissionPlan
	history          *VerifiedAdmissionHistory
	candidateBinding *verifiedEvidenceRunBinding
	lease            *evidencefs.GenerationLease
	snapshot         *evidencefs.GenerationSnapshot
	target           [32]byte
	journal          Digest
	revision         uint64
	reservedDigest   Digest
	headerDigest     Digest
	activationDigest Digest
	snapshotIdentity [32]byte
	indexDigest      [32]byte
	indexRecords     uint64
	segmentCount     uint32
	journalRecords   uint64
	journalTail      Digest
	binding          *generationReplayReadyBinding
	consumed         *atomic.Bool
}

type generationReplayReadyBinding struct {
	ready            *GenerationReplayReady
	prior            *GenerationHandoffReady
	plan             *VerifiedAdmissionPlan
	history          *VerifiedAdmissionHistory
	candidateBinding *verifiedEvidenceRunBinding
	lease            *evidencefs.GenerationLease
	snapshot         *evidencefs.GenerationSnapshot
	canonical        [32]byte
}

type generationReplayReadyRegistryRecord struct {
	ready            *GenerationReplayReady
	binding          *generationReplayReadyBinding
	prior            *GenerationHandoffReady
	plan             *VerifiedAdmissionPlan
	history          *VerifiedAdmissionHistory
	candidateBinding *verifiedEvidenceRunBinding
	lease            *evidencefs.GenerationLease
	snapshot         *evidencefs.GenerationSnapshot
	canonical        [32]byte
}

var generationReplayReadyRegistry sync.Map

// Handoff consumes generation-ready admission authority and transfers its
// exact target lineage/generation lock pair to a non-runnable sealed value.
func (p *GenerationReadyPermit) Handoff(ctx context.Context, candidate OwnedCurrentCandidate) (GenerationHandoffResult, error) {
	pre := GenerationHandoffResult{outcome: evidencefs.AdmissionTransitionPreMutationFailure, candidateSequence: 9}
	if p == nil || p.inventory == nil || !validGenerationReadyPermit(p, p.inventory, candidate) {
		return pre, fail(CodeEvidenceRecoveryRequired, "admission-generation-handoff", "generation-ready authority is unavailable", nil)
	}
	if err := contextAdmissionError(ctx); err != nil {
		return pre, err
	}
	pre.revision = p.revision
	pre.candidateDigest = generationHandoffCandidateDigest(p)
	if pre.candidateDigest == ([32]byte{}) || !p.consumed.CompareAndSwap(false, true) {
		return pre, fail(CodeEvidenceRecoveryRequired, "admission-generation-handoff", "generation-ready authority is invalid or consumed", nil)
	}
	fsLease, transitionErr := p.mutation.HandoffGeneration(ctx, p.inventory, digestRaw(p.journal))
	result := GenerationHandoffResult{
		outcome: evidencefs.AdmissionTransitionDurable, candidateDigest: pre.candidateDigest,
		candidateSequence: pre.candidateSequence, revision: pre.revision,
	}
	if transitionErr != nil || fsLease == nil || !fsLease.Active() {
		// A canceled/preflight evidencefs handoff leaves the token valid; restore
		// the upper one-shot authority only in that exact case. Once ownership
		// transfer begins, evidencefs consumes the token and never returns a lease
		// on cleanup uncertainty.
		if fsLease != nil {
			_ = fsLease.Close()
			generationReadyPermitRegistry.Delete(p)
			result.outcome = evidencefs.AdmissionTransitionUnknown
			return result, admissionPostMutationFailure("admission-generation-handoff")
		}
		if p.mutation.ValidFor(p.inventory) {
			p.consumed.CompareAndSwap(true, false)
			result.outcome = evidencefs.AdmissionTransitionPreMutationFailure
			return result, mapAdmissionMutationError(transitionErr, "admission-generation-handoff")
		}
		generationReadyPermitRegistry.Delete(p)
		result.outcome = evidencefs.AdmissionTransitionUnknown
		return result, admissionPostMutationFailure("admission-generation-handoff")
	}
	target, targetErr := fsLease.Target()
	journal, journalErr := fsLease.Journal()
	if targetErr != nil || journalErr != nil || target != p.target || journal != digestRaw(p.journal) {
		_ = fsLease.Close()
		generationReadyPermitRegistry.Delete(p)
		result.outcome = evidencefs.AdmissionTransitionUnknown
		return result, admissionPostMutationFailure("admission-generation-handoff-bind")
	}
	ready := &GenerationHandoffReady{
		prior: p, plan: p.plan, history: p.history, candidateBinding: candidate.binding, lease: fsLease,
		target: target, journal: p.journal, revision: p.revision, reservedDigest: p.reservedDigest,
		headerDigest: p.headerDigest, activationDigest: p.activationDigest, consumed: &atomic.Bool{},
	}
	ready.self = ready
	binding := &generationHandoffReadyBinding{
		ready: ready, prior: p, plan: p.plan, history: p.history, candidateBinding: candidate.binding, lease: fsLease,
	}
	ready.binding = binding
	binding.canonical = generationHandoffReadyDigest(ready)
	generationHandoffReadyRegistry.Store(ready, generationHandoffReadyRegistryRecord{
		ready: ready, binding: binding, prior: p, plan: p.plan, history: p.history, candidateBinding: candidate.binding,
		lease: fsLease, canonical: binding.canonical,
	})
	if !validGenerationHandoffReady(ready, candidate) {
		generationHandoffReadyRegistry.Delete(ready)
		generationReadyPermitRegistry.Delete(p)
		_ = fsLease.Close()
		result.outcome = evidencefs.AdmissionTransitionUnknown
		return result, admissionPostMutationFailure("admission-generation-handoff-seal")
	}
	generationReadyPermitRegistry.Delete(p)
	result.next = ready
	return result, nil
}

func generationHandoffCandidateDigest(permit *GenerationReadyPermit) [32]byte {
	if permit == nil || permit.binding == nil || permit.self != permit {
		return [32]byte{}
	}
	h := sha256.New()
	h.Write([]byte("cloud-agents-platform-generation-handoff-candidate/v1\x00"))
	h.Write(permit.binding.canonical[:])
	writeAdmissionUint(h, permit.revision)
	writeAdmissionString(h, permit.journal.String())
	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result
}

func generationHandoffReadyDigest(ready *GenerationHandoffReady) [32]byte {
	if ready == nil || ready.self != ready || ready.prior == nil || ready.plan == nil || ready.history == nil || ready.candidateBinding == nil || ready.lease == nil || ready.prior.binding == nil || ready.plan.binding == nil || ready.history.binding == nil {
		return [32]byte{}
	}
	h := sha256.New()
	h.Write([]byte("cloud-agents-platform-generation-handoff-ready/v1\x00"))
	h.Write(ready.prior.binding.canonical[:])
	h.Write(ready.plan.binding.canonical[:])
	h.Write(ready.history.binding.canonical[:])
	h.Write(ready.candidateBinding.canonical[:])
	h.Write(ready.target[:])
	writeAdmissionUint(h, ready.revision)
	for _, value := range []Digest{ready.journal, ready.reservedDigest, ready.headerDigest, ready.activationDigest} {
		writeAdmissionString(h, value.String())
	}
	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result
}

func validGenerationHandoffReady(ready *GenerationHandoffReady, candidate OwnedCurrentCandidate) bool {
	if ready == nil || ready.self != ready || ready.binding == nil || ready.binding.ready != ready || ready.prior == nil || ready.plan == nil || ready.history == nil || ready.candidateBinding != candidate.binding || ready.lease == nil || ready.binding.prior != ready.prior || ready.binding.plan != ready.plan || ready.binding.history != ready.history || ready.binding.candidateBinding != candidate.binding || ready.binding.lease != ready.lease || ready.consumed == nil || ready.consumed.Load() || !validOwnedCurrentCandidate(candidate) || !validGenerationReadyShape(ready.prior, ready.plan, candidate) || ready.plan != ready.prior.plan || ready.history != ready.prior.history || ready.candidateBinding != ready.prior.candidateBinding || ready.revision != ready.prior.revision || ready.target != ready.prior.target || ready.journal != ready.prior.journal || ready.reservedDigest != ready.prior.reservedDigest || ready.headerDigest != ready.prior.headerDigest || ready.activationDigest != ready.prior.activationDigest || !ready.lease.Active() || ready.binding.canonical == ([32]byte{}) || ready.binding.canonical != generationHandoffReadyDigest(ready) {
		return false
	}
	if ready.prior.mutation.ValidFor(ready.prior.inventory) {
		return false
	}
	target, targetErr := ready.lease.Target()
	journal, journalErr := ready.lease.Journal()
	if targetErr != nil || journalErr != nil || target != ready.target || journal != digestRaw(ready.journal) {
		return false
	}
	registered, ok := generationHandoffReadyRegistry.Load(ready)
	record, recordOK := registered.(generationHandoffReadyRegistryRecord)
	return ok && recordOK && record.ready == ready && record.binding == ready.binding && record.prior == ready.prior && record.plan == ready.plan &&
		record.history == ready.history && record.candidateBinding == ready.candidateBinding && record.lease == ready.lease &&
		record.canonical == ready.binding.canonical
}

// Replay consumes the non-runnable handoff value, snapshots the retained
// generation, and strictly replays the exact brand-new lineage/index/journal
// shape. It still cannot construct a runtime JournalCursor: same-verifier
// recovery and schema witness binding remain a later transition.
func (r *GenerationHandoffReady) Replay(ctx context.Context, candidate OwnedCurrentCandidate) (GenerationReplayResult, error) {
	var result GenerationReplayResult
	if r == nil || !validGenerationHandoffReady(r, candidate) {
		return result, fail(CodeEvidenceRecoveryRequired, "generation-replay", "generation handoff authority is unavailable", nil)
	}
	if err := contextAdmissionError(ctx); err != nil {
		return result, err
	}
	if !r.consumed.CompareAndSwap(false, true) {
		return result, fail(CodeEvidenceRecoveryRequired, "generation-replay", "generation handoff authority is consumed", nil)
	}
	snapshot, err := r.lease.Snapshot(ctx)
	if err != nil || snapshot == nil {
		if isPreMutationReplayError(err) && r.lease.Active() {
			r.consumed.CompareAndSwap(true, false)
			return result, mapEvidenceAdmissionError(err, "generation-replay-snapshot")
		}
		return r.failReplay(err, "generation-replay-snapshot")
	}
	return r.replaySnapshot(ctx, candidate, snapshot)
}

func (r *GenerationHandoffReady) replaySnapshot(ctx context.Context, candidate OwnedCurrentCandidate, snapshot *evidencefs.GenerationSnapshot) (GenerationReplayResult, error) {
	var result GenerationReplayResult
	if r == nil || snapshot == nil || r.consumed == nil || !r.consumed.Load() || !validConsumedGenerationHandoffReady(r, candidate) {
		return result, fail(CodeEvidenceRecoveryRequired, "generation-replay", "consumed handoff authority is unavailable", nil)
	}
	if !r.lease.OwnsSnapshot(snapshot) {
		return r.failReplay(evidencefs.ErrLeaseInvalid, "generation-replay-snapshot-bind")
	}
	indexRaw, err := snapshot.ReadIndex(ctx)
	if err != nil {
		return r.failReplay(err, "generation-replay-index")
	}
	indexFact, err := snapshot.IndexFact()
	if err != nil {
		return r.failReplay(err, "generation-replay-index")
	}
	if indexFact.ContentDigest != sha256.Sum256(indexRaw) || indexFact.Size != uint64(len(indexRaw)) {
		return r.failReplay(admissionCorrupt("generation-replay-index", "index fact and bytes differ", nil), "generation-replay-index")
	}
	indexFrames, err := decodeAdmissionLineageFrames(indexRaw)
	if err != nil {
		return r.failReplay(err, "generation-replay-index")
	}
	plan, err := scanLineageChainStructure(indexFrames)
	if err != nil {
		return r.failReplay(admissionCorrupt("generation-replay-index", "lineage structure is invalid", err), "generation-replay-index")
	}
	if err := r.validateReplayIndex(indexFrames); err != nil {
		return r.failReplay(err, "generation-replay-index")
	}
	count, err := snapshot.SegmentCount()
	if err != nil {
		return r.failReplay(err, "generation-replay-segments")
	}
	if err := validateBrandNewReplayBoundary(count, 1, r.headerDigest, r.headerDigest); err != nil {
		return r.failReplay(err, "generation-replay-segments")
	}
	stream, registered := openEvidenceJournalStructuralStream(plan, r.journal, nil)
	if !registered || stream == nil {
		return r.failReplay(admissionCorrupt("generation-replay-journal", "journal is not registered by the lineage index", nil), "generation-replay-journal")
	}
	var journalRecords uint64
	var journalTail Digest
	var segmentZero EvidenceFrame
	for ordinal := uint32(0); ordinal < count; ordinal++ {
		raw, readErr := snapshot.ReadSegment(ctx, ordinal)
		if readErr != nil {
			return r.failReplay(readErr, "generation-replay-segment")
		}
		fact, factErr := snapshot.SegmentFact(ordinal)
		if factErr != nil {
			return r.failReplay(factErr, "generation-replay-segment")
		}
		if fact.Ordinal != ordinal || fact.ContentDigest != sha256.Sum256(raw) || fact.Size != uint64(len(raw)) {
			return r.failReplay(admissionCorrupt("generation-replay-segment", "segment fact and bytes differ", nil), "generation-replay-segment")
		}
		if beginErr := stream.beginSegment(); beginErr != nil {
			return r.failReplay(admissionCorrupt("generation-replay-journal", "journal segment cannot begin", beginErr), "generation-replay-journal")
		}
		records, tail, first, streamErr := streamGenerationReplaySegment(raw, stream)
		if streamErr != nil {
			return r.failReplay(streamErr, "generation-replay-journal")
		}
		if endErr := stream.endSegment(); endErr != nil {
			return r.failReplay(admissionCorrupt("generation-replay-journal", "journal segment cannot end", endErr), "generation-replay-journal")
		}
		if ordinal == 0 {
			segmentZero = first
		}
		journalRecords, err = admissionCheckedAdd(journalRecords, records)
		if err != nil {
			return r.failReplay(err, "generation-replay-journal")
		}
		journalTail = tail
	}
	replay, err := stream.finish()
	if err != nil || replay == nil {
		return r.failReplay(admissionCorrupt("generation-replay-journal", "journal structure is invalid", err), "generation-replay-journal")
	}
	if err := validateBrandNewReplayBoundary(count, journalRecords, journalTail, r.headerDigest); err != nil || replay.records != journalRecords || replay.tailDigest != journalTail {
		if err == nil {
			err = admissionCorrupt("generation-replay-journal", "streaming and structural replay differ", nil)
		}
		return r.failReplay(err, "generation-replay-journal")
	}
	if err := plan.acceptJournal(r.journal, replay); err != nil {
		return r.failReplay(admissionCorrupt("generation-replay-journal", "journal differs from lineage registration", err), "generation-replay-journal")
	}
	actual := map[Digest]EvidenceFrame{r.journal: segmentZero}
	journalIDs := map[Digest]bool{r.journal: true}
	if _, err := plan.finish(actual, journalIDs); err != nil {
		return r.failReplay(admissionCorrupt("generation-replay-journal", "lineage and journal replay differ", err), "generation-replay-journal")
	}
	if err := snapshot.Revalidate(ctx); err != nil {
		return r.failReplay(err, "generation-replay-terminal")
	}
	identity, err := snapshot.IdentityDigest()
	if err != nil {
		return r.failReplay(err, "generation-replay-terminal")
	}
	if identity == ([32]byte{}) || journalRecords != replay.records || journalTail != replay.tailDigest {
		return r.failReplay(admissionCorrupt("generation-replay-terminal", "snapshot replay identity is inconsistent", nil), "generation-replay-terminal")
	}
	ready := &GenerationReplayReady{
		prior: r, plan: r.plan, history: r.history, candidateBinding: r.candidateBinding, lease: r.lease, snapshot: snapshot,
		target: r.target, journal: r.journal, revision: r.revision, reservedDigest: r.reservedDigest,
		headerDigest: r.headerDigest, activationDigest: r.activationDigest, snapshotIdentity: identity,
		indexDigest: indexFact.ContentDigest, indexRecords: uint64(len(indexFrames)), segmentCount: count,
		journalRecords: journalRecords, journalTail: journalTail,
		consumed: &atomic.Bool{},
	}
	ready.self = ready
	ready.binding = &generationReplayReadyBinding{
		ready: ready, prior: r, plan: r.plan, history: r.history, candidateBinding: r.candidateBinding,
		lease: r.lease, snapshot: snapshot,
	}
	ready.binding.canonical = generationReplayReadyDigest(ready)
	generationReplayReadyRegistry.Store(ready, generationReplayReadyRegistryRecord{
		ready: ready, binding: ready.binding, prior: r, plan: r.plan, history: r.history,
		candidateBinding: r.candidateBinding, lease: r.lease, snapshot: snapshot, canonical: ready.binding.canonical,
	})
	if !validGenerationReplayReady(ready, candidate) {
		generationReplayReadyRegistry.Delete(ready)
		generationHandoffReadyRegistry.Delete(r)
		_ = r.lease.Close()
		return result, admissionFailed("generation-replay-seal", "replayed generation authority could not be sealed", nil)
	}
	generationHandoffReadyRegistry.Delete(r)
	result.next = ready
	result.snapshotIdentity = identity
	result.indexRecords = uint64(len(indexFrames))
	result.segmentCount = count
	result.journalRecords = journalRecords
	result.journalTail = journalTail
	return result, nil
}

func validConsumedGenerationHandoffReady(ready *GenerationHandoffReady, candidate OwnedCurrentCandidate) bool {
	if ready == nil || ready.self != ready || ready.binding == nil || ready.binding.ready != ready || ready.prior == nil || ready.plan == nil || ready.history == nil || ready.candidateBinding != candidate.binding || ready.lease == nil || ready.binding.prior != ready.prior || ready.binding.plan != ready.plan || ready.binding.history != ready.history || ready.binding.candidateBinding != candidate.binding || ready.binding.lease != ready.lease || ready.consumed == nil || !ready.consumed.Load() || !validOwnedCurrentCandidate(candidate) || !validGenerationReadyShape(ready.prior, ready.plan, candidate) || ready.plan != ready.prior.plan || ready.history != ready.prior.history || ready.candidateBinding != ready.prior.candidateBinding || ready.revision != ready.prior.revision || ready.target != ready.prior.target || ready.journal != ready.prior.journal || ready.reservedDigest != ready.prior.reservedDigest || ready.headerDigest != ready.prior.headerDigest || ready.activationDigest != ready.prior.activationDigest || ready.prior.mutation.ValidFor(ready.prior.inventory) || !ready.lease.Active() || ready.binding.canonical == ([32]byte{}) || ready.binding.canonical != generationHandoffReadyDigest(ready) {
		return false
	}
	target, targetErr := ready.lease.Target()
	journal, journalErr := ready.lease.Journal()
	if targetErr != nil || journalErr != nil || target != ready.target || journal != digestRaw(ready.journal) {
		return false
	}
	registered, ok := generationHandoffReadyRegistry.Load(ready)
	record, recordOK := registered.(generationHandoffReadyRegistryRecord)
	return ok && recordOK && record.ready == ready && record.binding == ready.binding && record.prior == ready.prior && record.plan == ready.plan && record.history == ready.history && record.candidateBinding == ready.candidateBinding && record.lease == ready.lease && record.canonical == ready.binding.canonical
}

func isPreMutationReplayError(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

func (r *GenerationHandoffReady) failReplay(cause error, operation string) (GenerationReplayResult, error) {
	generationHandoffReadyRegistry.Delete(r)
	cleanupErr := error(nil)
	if r != nil && r.lease != nil {
		cleanupErr = r.lease.Close()
	}
	if cleanupErr != nil {
		return GenerationReplayResult{}, mapEvidenceAdmissionError(cleanupErr, operation+"-cleanup")
	}
	if cause == nil {
		cause = evidencefs.ErrCorrupt
	}
	if IsCode(cause, CodeEvidenceJournalCorrupt) || IsCode(cause, CodeEvidenceJournalFailed) || IsCode(cause, CodeContextCanceled) || IsCode(cause, CodeDeadlineExceeded) {
		return GenerationReplayResult{}, cause
	}
	return GenerationReplayResult{}, mapEvidenceAdmissionError(cause, operation)
}

func (r *GenerationHandoffReady) validateReplayIndex(frames []LineageIndexFrame) error {
	if r == nil || r.prior == nil || r.prior.plan == nil || len(frames) != 3 {
		return admissionCorrupt("generation-replay-index", "brand-new activated index shape is invalid", nil)
	}
	want := []LineageIndexFrame{r.prior.plan.lineageHeaderFrame, r.prior.plan.reservedFrame, r.prior.activatedFrame}
	for index := range want {
		if !canonicalEqual(frames[index], want[index]) {
			return admissionCorrupt("generation-replay-index", "durable index differs from handoff chain", nil)
		}
	}
	if frames[1].RecordDigest != r.reservedDigest || frames[2].RecordDigest != r.activationDigest || frames[1].Record.Reserved == nil || frames[2].Record.Activated == nil || frames[1].Record.Reserved.ExpectedSegment0HeaderDigest != r.headerDigest || frames[2].Record.Activated.Segment0HeaderDigest != r.headerDigest || frames[1].Record.Reserved.JournalIdentityDigest != r.journal || frames[2].Record.Activated.JournalIdentityDigest != r.journal {
		return admissionCorrupt("generation-replay-index", "durable generation identity differs from handoff", nil)
	}
	return nil
}

func streamGenerationReplaySegment(raw []byte, stream *evidenceJournalStructuralStream) (uint64, Digest, EvidenceFrame, error) {
	if stream == nil {
		return 0, "", EvidenceFrame{}, admissionCorrupt("generation-replay-journal", "journal stream is unavailable", nil)
	}
	var records uint64
	var tail Digest
	var first EvidenceFrame
	err := decodeAdmissionFramedBytes(raw, 16<<20, 4096, maxEvidenceFrameBytes, func(framed []byte) error {
		frame, err := DecodeCanonicalEvidenceFrame(framed)
		if err != nil {
			return err
		}
		if records == 0 {
			first = cloneProjectionValue(*frame)
		}
		if err := stream.consumeFrame(*frame, uint64(len(framed))); err != nil {
			return err
		}
		records++
		tail = frame.RecordDigest
		return nil
	})
	if err != nil {
		return 0, "", EvidenceFrame{}, admissionCorrupt("generation-replay-journal", "stored evidence segment is invalid", err)
	}
	return records, tail, first, nil
}

func validateBrandNewReplayBoundary(segmentCount uint32, records uint64, tail, header Digest) error {
	if segmentCount != 1 || records != 1 || header.Validate() != nil || tail != header {
		return admissionCorrupt("generation-replay-journal", "brand-new journal progressed before runtime binding", nil)
	}
	return nil
}

func generationReplayReadyDigest(ready *GenerationReplayReady) [32]byte {
	if ready == nil || ready.self != ready || ready.prior == nil || ready.plan == nil || ready.history == nil || ready.candidateBinding == nil || ready.lease == nil || ready.snapshot == nil || ready.prior.binding == nil || ready.plan.binding == nil || ready.history.binding == nil || ready.snapshotIdentity == ([32]byte{}) || ready.indexDigest == ([32]byte{}) || ready.indexRecords == 0 || ready.segmentCount == 0 || ready.journalRecords == 0 || ready.journalTail.Validate() != nil {
		return [32]byte{}
	}
	h := sha256.New()
	h.Write([]byte("cloud-agents-platform-generation-replay-ready/v1\x00"))
	h.Write(ready.prior.binding.canonical[:])
	h.Write(ready.plan.binding.canonical[:])
	h.Write(ready.history.binding.canonical[:])
	h.Write(ready.candidateBinding.canonical[:])
	h.Write(ready.target[:])
	h.Write(ready.snapshotIdentity[:])
	h.Write(ready.indexDigest[:])
	writeAdmissionUint(h, ready.revision)
	writeAdmissionUint(h, ready.indexRecords)
	writeAdmissionUint(h, uint64(ready.segmentCount))
	writeAdmissionUint(h, ready.journalRecords)
	for _, value := range []Digest{ready.journal, ready.reservedDigest, ready.headerDigest, ready.activationDigest, ready.journalTail} {
		writeAdmissionString(h, value.String())
	}
	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result
}

func validGenerationReplayReady(ready *GenerationReplayReady, candidate OwnedCurrentCandidate) bool {
	if ready == nil || ready.self != ready || ready.binding == nil || ready.binding.ready != ready || ready.prior == nil || ready.plan == nil || ready.history == nil || ready.candidateBinding != candidate.binding || ready.lease == nil || ready.snapshot == nil || ready.binding.prior != ready.prior || ready.binding.plan != ready.plan || ready.binding.history != ready.history || ready.binding.candidateBinding != ready.candidateBinding || ready.binding.lease != ready.lease || ready.binding.snapshot != ready.snapshot || ready.consumed == nil || ready.consumed.Load() || !validOwnedCurrentCandidate(candidate) || ready.plan != ready.prior.plan || ready.history != ready.prior.history || ready.candidateBinding != ready.prior.candidateBinding || ready.target != ready.prior.target || ready.journal != ready.prior.journal || ready.revision != ready.prior.revision || ready.reservedDigest != ready.prior.reservedDigest || ready.headerDigest != ready.prior.headerDigest || ready.activationDigest != ready.prior.activationDigest || !ready.lease.Active() || ready.binding.canonical == ([32]byte{}) || ready.binding.canonical != generationReplayReadyDigest(ready) {
		return false
	}
	identity, err := ready.snapshot.IdentityDigest()
	indexFact, indexErr := ready.snapshot.IndexFact()
	segmentCount, countErr := ready.snapshot.SegmentCount()
	if err != nil || indexErr != nil || countErr != nil || identity != ready.snapshotIdentity || indexFact.ContentDigest != ready.indexDigest || segmentCount != ready.segmentCount || !ready.lease.OwnsSnapshot(ready.snapshot) {
		return false
	}
	registered, ok := generationReplayReadyRegistry.Load(ready)
	record, recordOK := registered.(generationReplayReadyRegistryRecord)
	return ok && recordOK && record.ready == ready && record.binding == ready.binding && record.prior == ready.prior && record.plan == ready.plan && record.history == ready.history && record.candidateBinding == ready.candidateBinding && record.lease == ready.lease && record.snapshot == ready.snapshot && record.canonical == ready.binding.canonical
}

// Close releases the retained generation/lineage locks and permanently
// invalidates the replay-ready value.
func (r *GenerationReplayReady) Close() error {
	if r == nil || r.self != r || r.consumed == nil || !r.consumed.CompareAndSwap(false, true) {
		return admissionFailed("generation-replay-close", "generation replay authority is unavailable", nil)
	}
	registered, ok := generationReplayReadyRegistry.Load(r)
	record, recordOK := registered.(generationReplayReadyRegistryRecord)
	if !ok || !recordOK || record.ready != r || record.binding == nil || record.lease == nil {
		return admissionFailed("generation-replay-close", "immutable generation filesystem lease is unavailable", nil)
	}
	generationReplayReadyRegistry.Delete(r)
	if err := record.lease.Close(); err != nil {
		return mapEvidenceAdmissionError(err, "generation-replay-close")
	}
	return nil
}

func validConsumedGenerationReadyPermit(permit *GenerationReadyPermit, plan *VerifiedAdmissionPlan, candidate OwnedCurrentCandidate) bool {
	if !validGenerationReadyShape(permit, plan, candidate) {
		return false
	}
	registered, ok := generationReadyPermitRegistry.Load(permit)
	record, recordOK := registered.(generationReadyPermitRegistryRecord)
	return ok && recordOK && generationReadyRegistryRecordMatches(record, permit)
}

func validGenerationReadyShape(permit *GenerationReadyPermit, plan *VerifiedAdmissionPlan, candidate OwnedCurrentCandidate) bool {
	return permit != nil && permit.self == permit && permit.binding != nil && permit.binding.permit == permit && permit.plan == plan && plan != nil && permit.history != nil && permit.candidateBinding == candidate.binding && permit.binding.prior == permit.prior && permit.binding.plan == plan && permit.binding.history == permit.history && permit.binding.inventory == permit.inventory && permit.binding.mutation == permit.mutation && permit.binding.runtimeBinding == permit.runtimeReceipt.binding && permit.binding.recoveryBinding == permit.recoveryReceipt.binding && permit.consumed != nil && permit.consumed.Load() && permit.binding.canonical != ([32]byte{}) && permit.binding.canonical == generationReadyPermitDigest(permit)
}

// Close only releases the retained filesystem locks. It cannot authorize
// runner or database progress and permanently invalidates this handoff value.
func (r *GenerationHandoffReady) Close() error {
	if r == nil || r.self != r || r.consumed == nil || !r.consumed.CompareAndSwap(false, true) {
		return fail(CodeEvidenceJournalFailed, "admission-generation-handoff-close", "generation handoff authority is unavailable", nil)
	}
	lease := r.lease
	var prior *GenerationReadyPermit
	if registered, ok := generationHandoffReadyRegistry.Load(r); ok {
		if record, recordOK := registered.(generationHandoffReadyRegistryRecord); recordOK && record.ready == r && record.lease != nil {
			lease = record.lease
			prior = record.prior
		}
	}
	generationHandoffReadyRegistry.Delete(r)
	if prior != nil {
		generationReadyPermitRegistry.Delete(prior)
	}
	if lease == nil {
		return fail(CodeEvidenceJournalFailed, "admission-generation-handoff-close", "generation filesystem lease is unavailable", nil)
	}
	if err := lease.Close(); err != nil {
		return mapEvidenceAdmissionError(err, "admission-generation-handoff-close")
	}
	return nil
}
