package migration

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"sync"
	"sync/atomic"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/evidencefs"
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

// historicalSuccessorSupersessionReady is the only bridge from an activated
// crash-recovered B that is historical relative to current C into the
// header-only B -> C supersession path. It owns the consumed B recovery chain
// and its retained generation lease until a later admission reacquire consumes
// it. It is not a journal, session, filesystem mutation, or runner authority.
type historicalSuccessorSupersessionReady struct {
	self             *historicalSuccessorSupersessionReady
	prior            *HistoricalSuccessorGenerationRecoveryReady
	candidateBinding *verifiedEvidenceRunBinding
	authority        *VerifiedLineageSupersessionAuthority
	activation       Digest
	initialTail      Digest
	continuation     *LineageContinuationContext
	consumed         *atomic.Bool
	binding          *historicalSuccessorSupersessionBinding
}

type historicalSuccessorSupersessionBinding struct {
	ready            *historicalSuccessorSupersessionReady
	prior            *HistoricalSuccessorGenerationRecoveryReady
	candidateBinding *verifiedEvidenceRunBinding
	authority        *VerifiedLineageSupersessionAuthority
	canonical        [32]byte
}

type historicalSuccessorSupersessionRecord struct {
	ready            *historicalSuccessorSupersessionReady
	binding          *historicalSuccessorSupersessionBinding
	prior            *HistoricalSuccessorGenerationRecoveryReady
	priorBinding     *historicalSuccessorGenerationRecoveryBinding
	candidateBinding *verifiedEvidenceRunBinding
	authority        *VerifiedLineageSupersessionAuthority
	canonical        [32]byte
}

var historicalSuccessorSupersessionRegistry sync.Map

// historicalSuccessorAdmissionReady proves that the historical B generation
// lease was irreversibly released before the same private evidencefs Store
// reacquired full-root admission for the exact lineage. It retains the B -> C
// authority but cannot mutate the filesystem until ALL-history replay binds a
// new successor plan.
type historicalSuccessorAdmissionReady struct {
	self             *historicalSuccessorAdmissionReady
	prior            *historicalSuccessorSupersessionReady
	candidateBinding *verifiedEvidenceRunBinding
	authority        *VerifiedLineageSupersessionAuthority
	reacquired       evidencefs.GenerationAdmissionReacquireResult
	admission        *evidencefs.AdmissionLease
	inventory        *evidencefs.AdmissionInventory
	target           [32]byte
	previousJournal  [32]byte
	previousLease    [32]byte
	revision         uint64
	fullSet          [32]byte
	consumed         *atomic.Bool
	binding          *historicalSuccessorAdmissionBinding
}

type historicalSuccessorAdmissionBinding struct {
	ready            *historicalSuccessorAdmissionReady
	prior            *historicalSuccessorSupersessionReady
	candidateBinding *verifiedEvidenceRunBinding
	authority        *VerifiedLineageSupersessionAuthority
	admission        *evidencefs.AdmissionLease
	inventory        *evidencefs.AdmissionInventory
	canonical        [32]byte
}

type historicalSuccessorAdmissionRecord struct {
	ready            *historicalSuccessorAdmissionReady
	binding          *historicalSuccessorAdmissionBinding
	prior            *historicalSuccessorSupersessionReady
	priorBinding     *historicalSuccessorSupersessionBinding
	candidateBinding *verifiedEvidenceRunBinding
	authority        *VerifiedLineageSupersessionAuthority
	reacquired       evidencefs.GenerationAdmissionReacquireResult
	admission        *evidencefs.AdmissionLease
	inventory        *evidencefs.AdmissionInventory
	canonical        [32]byte
}

var historicalSuccessorAdmissionRegistry sync.Map

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

// bindHeaderOnlySupersession consumes historical B recovery authority and
// reconstructs the exact activated-no-migration-progress B -> C authority. The
// continuation is copied only from B's durable GenerationReserved body; it is
// never guessed from current runtime inputs.
func (r *HistoricalSuccessorGenerationRecoveryReady) bindHeaderOnlySupersession(candidate OwnedCurrentCandidate) (*historicalSuccessorSupersessionReady, error) {
	if r == nil || !validHistoricalSuccessorGenerationRecoveryReady(r, candidate) || !r.requiresSupersession || r.executionBindings == nil || r.planned == nil || r.planned.policy == nil || r.prior == nil || r.prior.prior == nil || r.prior.prior.prior == nil || r.prior.prior.prior.reservedFrame.Record.Reserved == nil {
		return nil, fail(CodeEvidenceRecoveryRequired, "historical-successor-resupersession", "historical successor supersession inputs are unavailable", nil)
	}
	reserved := r.prior.prior.prior.reservedFrame.Record.Reserved
	continuation := cloneProjectionValue(reserved.Continuation)
	activation := r.cursor.lineageIndexPreviousRecordDigest
	initialTail := r.recovery.tailDigest
	evidence := &ownedHeaderOnlySupersessionEvidence{
		owner: candidate.owner, generation: r.generation, tailDigest: initialTail,
		activationDigest: activation, initialTailDigest: initialTail, continuation: continuation,
	}
	if !r.consumed.CompareAndSwap(false, true) {
		return nil, fail(CodeEvidenceRecoveryRequired, "historical-successor-resupersession", "historical successor recovery authority was already consumed", nil)
	}
	authority, err := bindLineageSupersession(*r.planned.policy, *r.executionBindings, evidence)
	if err != nil {
		return failHistoricalSuccessorSupersessionBind(r, err)
	}
	ready := &historicalSuccessorSupersessionReady{
		prior: r, candidateBinding: candidate.binding, authority: authority,
		activation: activation, initialTail: initialTail, continuation: continuation, consumed: &atomic.Bool{},
	}
	ready.self = ready
	ready.binding = &historicalSuccessorSupersessionBinding{
		ready: ready, prior: r, candidateBinding: candidate.binding, authority: authority,
	}
	ready.binding.canonical = historicalSuccessorSupersessionDigest(ready)
	historicalSuccessorSupersessionRegistry.Store(ready, historicalSuccessorSupersessionRecord{
		ready: ready, binding: ready.binding, prior: r, priorBinding: r.binding,
		candidateBinding: candidate.binding, authority: authority, canonical: ready.binding.canonical,
	})
	if !validHistoricalSuccessorSupersessionReady(ready, candidate) {
		historicalSuccessorSupersessionRegistry.Delete(ready)
		authority.consumed.CompareAndSwap(false, true)
		return failHistoricalSuccessorSupersessionBind(r, fail(CodeEvidenceRecoveryRequired, "historical-successor-resupersession", "historical successor supersession authority could not be sealed", nil))
	}
	return ready, nil
}

func failHistoricalSuccessorSupersessionBind(r *HistoricalSuccessorGenerationRecoveryReady, cause error) (*historicalSuccessorSupersessionReady, error) {
	if cleanupErr := closeConsumedHistoricalSuccessorGenerationRecovery(r, "historical-successor-resupersession-cleanup"); cleanupErr != nil {
		return nil, cleanupErr
	}
	if errorsIsContext(cause) {
		return nil, cause
	}
	return nil, fail(CodeEvidenceRecoveryRequired, "historical-successor-resupersession", "historical successor supersession authority cannot be reconstructed", nil)
}

func historicalSuccessorSupersessionDigest(ready *historicalSuccessorSupersessionReady) [32]byte {
	if ready == nil || ready.self != ready || ready.prior == nil || ready.prior.binding == nil || ready.candidateBinding == nil || ready.authority == nil || ready.consumed == nil || ready.activation.Validate() != nil || ready.initialTail.Validate() != nil {
		return [32]byte{}
	}
	continuation, err := canonicalContractKey(ready.continuation)
	if err != nil {
		return [32]byte{}
	}
	h := sha256.New()
	h.Write([]byte("cloud-agents-platform-historical-successor-resupersession-ready/v1\x00"))
	h.Write(ready.prior.binding.canonical[:])
	h.Write(ready.candidateBinding.canonical[:])
	writeAdmissionString(h, ready.authority.digest.String())
	writeAdmissionString(h, ready.activation.String())
	writeAdmissionString(h, ready.initialTail.String())
	writeAdmissionString(h, continuation)
	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result
}

func validHistoricalSuccessorSupersessionReady(ready *historicalSuccessorSupersessionReady, candidate OwnedCurrentCandidate) bool {
	if ready == nil || ready.self != ready || ready.binding == nil || ready.binding.ready != ready || ready.prior == nil || ready.binding.prior != ready.prior || ready.prior.binding == nil || ready.prior.prior == nil || ready.prior.prior.prior == nil || ready.prior.prior.prior.prior == nil || ready.candidateBinding != candidate.binding || ready.binding.candidateBinding != ready.candidateBinding || ready.authority == nil || ready.binding.authority != ready.authority || ready.consumed == nil || ready.consumed.Load() || !validOwnedCurrentCandidate(candidate) || !ready.prior.requiresSupersession || ready.prior.executionBindings == nil || ready.prior.planned == nil || ready.prior.planned.policy == nil || !historicalSuccessorGenerationRecoveryReadyRecordMatches(ready.prior) || ready.prior.binding.canonical != historicalSuccessorGenerationRecoveryDigest(ready.prior) || ready.authority.owner != candidate.verifiedRun.currentDecision.owner || ready.authority.session != candidate.owner || ready.authority.consumed.Load() || !sameGenerationIdentity(ready.authority.generation, ready.prior.generation) || ready.authority.tailDigest != ready.prior.recovery.tailDigest || ready.activation != ready.prior.cursor.lineageIndexPreviousRecordDigest || ready.initialTail != ready.prior.recovery.tailDigest || ready.binding.canonical == ([32]byte{}) || ready.binding.canonical != historicalSuccessorSupersessionDigest(ready) {
		return false
	}
	reserved := ready.prior.prior.prior.prior.reservedFrame.Record.Reserved
	if reserved == nil || !canonicalEqual(ready.continuation, reserved.Continuation) {
		return false
	}
	subject := cloneProjectionValue(ready.authority.subject)
	digest, digestErr := subject.ComputeDigest()
	if digestErr != nil || digest != ready.authority.digest || subject.ObservedOutcome != "activated_no_migration_progress" || subject.OldActivationRecordDigest == nil || *subject.OldActivationRecordDigest != ready.activation || subject.OldInitialJournalTailDigest == nil || *subject.OldInitialJournalTailDigest != ready.initialTail || subject.OldCheckpointRecordDigest != nil || subject.OldTerminalDigest != nil || subject.OldResolutionDigest != nil || !canonicalEqual(subject.Continuation, ready.continuation) || validateRecoveryAuthorityBindings(candidate.verifiedRun.currentDecision.digest, ready.prior.planned.policy.subject, ready.prior.executionBindings.subject, subject) != nil {
		return false
	}
	value, ok := historicalSuccessorSupersessionRegistry.Load(ready)
	record, recordOK := value.(historicalSuccessorSupersessionRecord)
	return ok && recordOK && record.ready == ready && record.binding == ready.binding && record.prior == ready.prior && record.priorBinding == ready.prior.binding && record.candidateBinding == ready.candidateBinding && record.authority == ready.authority && record.canonical == ready.binding.canonical
}

func (r *historicalSuccessorSupersessionReady) close() error {
	if r == nil || r.self != r || r.consumed == nil || !r.consumed.CompareAndSwap(false, true) {
		return admissionFailed("historical-successor-resupersession-close", "historical successor supersession authority is unavailable", nil)
	}
	value, ok := historicalSuccessorSupersessionRegistry.Load(r)
	record, recordOK := value.(historicalSuccessorSupersessionRecord)
	historicalSuccessorSupersessionRegistry.Delete(r)
	validRecord := r.binding != nil && r.prior != nil && r.prior.binding != nil && ok && recordOK && record.ready == r && record.binding == r.binding && record.prior == r.prior && record.priorBinding == r.prior.binding && record.candidateBinding == r.candidateBinding && record.authority == r.authority && record.canonical != ([32]byte{}) && record.canonical == r.binding.canonical
	if r.authority != nil {
		r.authority.consumed.CompareAndSwap(false, true)
	}
	cleanupErr := closeConsumedHistoricalSuccessorGenerationRecovery(r.prior, "historical-successor-resupersession-close")
	if cleanupErr != nil {
		return cleanupErr
	}
	if !validRecord {
		return admissionFailed("historical-successor-resupersession-close", "immutable historical successor supersession authority is unavailable", nil)
	}
	return nil
}

// reacquireAdmission consumes the header-only B -> C bridge and performs the
// evidencefs-owned release-before-reacquire transition. Even an already
// canceled context is passed through: evidencefs first invalidates B's old
// generation lease, then attempts the full-root acquisition.
func (r *historicalSuccessorSupersessionReady) reacquireAdmission(ctx context.Context, candidate OwnedCurrentCandidate) (*historicalSuccessorAdmissionReady, error) {
	if !validHistoricalSuccessorSupersessionReady(r, candidate) || r.prior == nil || r.prior.prior == nil || r.prior.prior.lease == nil {
		return nil, fail(CodeEvidenceRecoveryRequired, "historical-successor-reacquire", "historical successor supersession authority is unavailable", nil)
	}
	if !r.consumed.CompareAndSwap(false, true) {
		return nil, fail(CodeEvidenceRecoveryRequired, "historical-successor-reacquire", "historical successor supersession authority was already consumed", nil)
	}
	oldLease := r.prior.prior.lease
	reacquired, err := oldLease.ReacquireAdmission(ctx)
	oldReleased := oldLease.Released()
	retireErr := retireHistoricalSuccessorSupersessionSource(r, !oldReleased)
	if err != nil {
		r.authority.consumed.CompareAndSwap(false, true)
		if retireErr != nil {
			return nil, retireErr
		}
		return nil, mapEvidenceAdmissionError(err, "historical-successor-reacquire")
	}
	if retireErr != nil {
		r.authority.consumed.CompareAndSwap(false, true)
		if admission, _, admissionErr := reacquired.Admission(); admissionErr == nil && admission != nil {
			if cleanupErr := admission.Close(); cleanupErr != nil {
				return nil, mapEvidenceAdmissionError(cleanupErr, "historical-successor-reacquire-cleanup")
			}
		}
		return nil, retireErr
	}
	admission, inventory, err := reacquired.Admission()
	if err != nil || admission == nil || inventory == nil {
		r.authority.consumed.CompareAndSwap(false, true)
		return nil, mapEvidenceAdmissionError(err, "historical-successor-reacquire")
	}
	failAfterReacquire := func(cause error, operation string) (*historicalSuccessorAdmissionReady, error) {
		r.authority.consumed.CompareAndSwap(false, true)
		cleanupErr := admission.Close()
		if cleanupErr != nil && !(errors.Is(cleanupErr, evidencefs.ErrLeaseInvalid) && !admission.Active()) {
			return nil, mapEvidenceAdmissionError(cleanupErr, operation+"-cleanup")
		}
		if cause == nil {
			cause = evidencefs.ErrUnknown
		}
		return nil, mapEvidenceAdmissionError(cause, operation)
	}
	if !reacquired.Valid() || reacquired.PreviousTarget() != digestRaw(candidate.verifiedRun.executionLineageDigest) || reacquired.PreviousJournal() != digestRaw(r.prior.generation.journalIdentityDigest) || reacquired.PreviousLeaseDigest() == ([32]byte{}) {
		return failAfterReacquire(evidencefs.ErrUnknown, "historical-successor-reacquire-bind")
	}
	if err := inventory.Revalidate(ctx); err != nil {
		return failAfterReacquire(err, "historical-successor-reacquire-revalidate")
	}
	revision, revisionErr := inventory.Revision()
	target, targetErr := inventory.Target()
	fullSet, fullSetErr := inventory.FullSetDigest()
	if revisionErr != nil || targetErr != nil || fullSetErr != nil {
		for _, accessorErr := range []error{revisionErr, targetErr, fullSetErr} {
			if accessorErr != nil {
				return failAfterReacquire(accessorErr, "historical-successor-reacquire-inventory")
			}
		}
	}
	if revision != 0 || target != reacquired.PreviousTarget() || fullSet == ([32]byte{}) {
		return failAfterReacquire(evidencefs.ErrUnknown, "historical-successor-reacquire-inventory")
	}
	ready := &historicalSuccessorAdmissionReady{
		prior: r, candidateBinding: candidate.binding, authority: r.authority, reacquired: reacquired,
		admission: admission, inventory: inventory, target: target, previousJournal: reacquired.PreviousJournal(), previousLease: reacquired.PreviousLeaseDigest(),
		revision: revision, fullSet: fullSet, consumed: &atomic.Bool{},
	}
	ready.self = ready
	ready.binding = &historicalSuccessorAdmissionBinding{
		ready: ready, prior: r, candidateBinding: candidate.binding, authority: r.authority, admission: admission, inventory: inventory,
	}
	ready.binding.canonical = historicalSuccessorAdmissionDigest(ready)
	historicalSuccessorAdmissionRegistry.Store(ready, historicalSuccessorAdmissionRecord{
		ready: ready, binding: ready.binding, prior: r, priorBinding: r.binding, candidateBinding: candidate.binding,
		authority: r.authority, reacquired: reacquired, admission: admission, inventory: inventory, canonical: ready.binding.canonical,
	})
	if !validHistoricalSuccessorAdmissionReady(ready, candidate) {
		historicalSuccessorAdmissionRegistry.Delete(ready)
		return failAfterReacquire(evidencefs.ErrUnknown, "historical-successor-reacquire-seal")
	}
	historicalSuccessorSupersessionRegistry.Delete(r)
	return ready, nil
}

func retireHistoricalSuccessorSupersessionSource(ready *historicalSuccessorSupersessionReady, closeLease bool) error {
	if ready == nil || ready.prior == nil {
		return admissionFailed("historical-successor-reacquire-retire", "historical successor source is unavailable", nil)
	}
	historicalSuccessorSupersessionRegistry.Delete(ready)
	if closeLease {
		return closeConsumedHistoricalSuccessorGenerationRecovery(ready.prior, "historical-successor-reacquire-retire")
	}
	recovery := ready.prior
	historicalSuccessorGenerationRecoveryRegistry.Delete(recovery)
	if recovery.cursor.valid != nil {
		recovery.cursor.valid.Store(false)
	}
	if recovery.prior != nil {
		historicalSuccessorGenerationReplayRegistry.Delete(recovery.prior)
		if recovery.prior.prior != nil {
			historicalSuccessorGenerationHandoffRegistry.Delete(recovery.prior.prior)
			if recovery.prior.prior.prior != nil {
				revokeVerifiedAdmissionRegisteredGeneration(recovery.prior.prior.prior.source)
				revokeVerifiedAdmissionRegisteredGeneration(recovery.prior.prior.prior.planned)
			}
		}
	}
	return nil
}

func historicalSuccessorAdmissionDigest(ready *historicalSuccessorAdmissionReady) [32]byte {
	if ready == nil || ready.self != ready || ready.prior == nil || ready.prior.binding == nil || ready.candidateBinding == nil || ready.authority == nil || ready.admission == nil || ready.inventory == nil || ready.consumed == nil || ready.target == ([32]byte{}) || ready.previousJournal == ([32]byte{}) || ready.previousLease == ([32]byte{}) || ready.fullSet == ([32]byte{}) {
		return [32]byte{}
	}
	h := sha256.New()
	h.Write([]byte("cloud-agents-platform-historical-successor-admission-ready/v1\x00"))
	h.Write(ready.prior.binding.canonical[:])
	h.Write(ready.candidateBinding.canonical[:])
	writeAdmissionString(h, ready.authority.digest.String())
	h.Write(ready.target[:])
	h.Write(ready.previousJournal[:])
	h.Write(ready.previousLease[:])
	h.Write(ready.fullSet[:])
	writeAdmissionUint(h, ready.revision)
	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result
}

func validHistoricalSuccessorAdmissionReady(ready *historicalSuccessorAdmissionReady, candidate OwnedCurrentCandidate) bool {
	if ready == nil || ready.self != ready || ready.binding == nil || ready.binding.ready != ready || ready.prior == nil || ready.binding.prior != ready.prior || ready.prior.binding == nil || ready.prior.prior == nil || ready.candidateBinding != candidate.binding || ready.binding.candidateBinding != ready.candidateBinding || ready.authority == nil || ready.binding.authority != ready.authority || ready.admission == nil || ready.binding.admission != ready.admission || ready.inventory == nil || ready.binding.inventory != ready.inventory || ready.consumed == nil || ready.consumed.Load() || !validOwnedCurrentCandidate(candidate) || ready.authority != ready.prior.authority || ready.authority.consumed.Load() || ready.target != digestRaw(candidate.verifiedRun.executionLineageDigest) || ready.previousJournal != digestRaw(ready.prior.prior.generation.journalIdentityDigest) || ready.previousLease == ([32]byte{}) || ready.revision != 0 || ready.fullSet == ([32]byte{}) || !ready.reacquired.Valid() || ready.reacquired.PreviousTarget() != ready.target || ready.reacquired.PreviousJournal() != ready.previousJournal || ready.reacquired.PreviousLeaseDigest() != ready.previousLease || ready.binding.canonical == ([32]byte{}) || ready.binding.canonical != historicalSuccessorAdmissionDigest(ready) {
		return false
	}
	admission, inventory, err := ready.reacquired.Admission()
	if err != nil || admission != ready.admission || inventory != ready.inventory || !admission.Active() {
		return false
	}
	revision, revisionErr := inventory.Revision()
	target, targetErr := inventory.Target()
	fullSet, fullSetErr := inventory.FullSetDigest()
	if revisionErr != nil || revision != ready.revision || targetErr != nil || target != ready.target || fullSetErr != nil || fullSet != ready.fullSet {
		return false
	}
	value, ok := historicalSuccessorAdmissionRegistry.Load(ready)
	record, recordOK := value.(historicalSuccessorAdmissionRecord)
	return ok && recordOK && record.ready == ready && record.binding == ready.binding && record.prior == ready.prior && record.priorBinding == ready.prior.binding && record.candidateBinding == ready.candidateBinding && record.authority == ready.authority && record.reacquired.Valid() && record.admission == ready.admission && record.inventory == ready.inventory && record.canonical == ready.binding.canonical
}

func (r *historicalSuccessorAdmissionReady) close() error {
	if r == nil || r.self != r || r.consumed == nil || !r.consumed.CompareAndSwap(false, true) {
		return admissionFailed("historical-successor-admission-close", "historical successor admission authority is unavailable", nil)
	}
	value, ok := historicalSuccessorAdmissionRegistry.Load(r)
	record, recordOK := value.(historicalSuccessorAdmissionRecord)
	historicalSuccessorAdmissionRegistry.Delete(r)
	validRecord := r.binding != nil && r.prior != nil && r.prior.binding != nil && ok && recordOK && record.ready == r && record.binding == r.binding && record.prior == r.prior && record.priorBinding == r.prior.binding && record.candidateBinding == r.candidateBinding && record.authority == r.authority && record.admission == r.admission && record.inventory == r.inventory && record.canonical != ([32]byte{}) && record.canonical == r.binding.canonical
	if r.authority != nil {
		r.authority.consumed.CompareAndSwap(false, true)
	}
	var cleanupErr error
	if r.admission != nil {
		cleanupErr = r.admission.Close()
		if errors.Is(cleanupErr, evidencefs.ErrLeaseInvalid) && !r.admission.Active() {
			cleanupErr = nil
		}
	}
	if cleanupErr != nil {
		return mapEvidenceAdmissionError(cleanupErr, "historical-successor-admission-close")
	}
	if !validRecord {
		return admissionFailed("historical-successor-admission-close", "immutable historical successor admission authority is unavailable", nil)
	}
	return nil
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
