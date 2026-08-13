package migration

import (
	"context"
	"crypto/sha256"
	"sync"
	"sync/atomic"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/evidencefs"
)

type ReserveReadyTransitionResult struct {
	outcome           evidencefs.AdmissionTransitionOutcome
	next              *ReserveReady
	candidateDigest   [32]byte
	candidateSequence uint64
	candidateRevision uint64
	previousRevision  uint64
}

func (r ReserveReadyTransitionResult) Outcome() evidencefs.AdmissionTransitionOutcome {
	return r.outcome
}
func (r ReserveReadyTransitionResult) Next() *ReserveReady       { return r.next }
func (r ReserveReadyTransitionResult) CandidateKind() string     { return "reserve_ready" }
func (r ReserveReadyTransitionResult) CandidateDigest() [32]byte { return r.candidateDigest }
func (r ReserveReadyTransitionResult) CandidateSequence() uint64 { return r.candidateSequence }
func (r ReserveReadyTransitionResult) CandidateRevision() uint64 { return r.candidateRevision }
func (r ReserveReadyTransitionResult) PreviousRevision() uint64  { return r.previousRevision }

// ReserveReady is package-private one-shot memory authority. It proves the
// whole admission chain reached N+5 with both content objects bound, but it is
// neither a receipt nor durable GenerationReserved authority.
type ReserveReady struct {
	self                *ReserveReady
	prior               *RecoveryBoundPermit
	plan                *VerifiedAdmissionPlan
	history             *VerifiedAdmissionHistory
	candidateBinding    *verifiedEvidenceRunBinding
	inventory           *evidencefs.AdmissionInventory
	mutation            *evidencefs.AdmissionMutationToken
	runtimePublication  *evidencefs.Publication
	recoveryPublication *evidencefs.Publication
	target, fullSet     [32]byte
	revision            uint64
	runtimeDigest       Digest
	runtimeSize         uint64
	recoveryDigest      Digest
	recoverySize        uint64
	lineageHeaderBytes  []byte
	reservedFrameBytes  []byte
	binding             *reserveReadyBinding
	consumed            *atomic.Bool
}

type reserveReadyBinding struct {
	ready               *ReserveReady
	prior               *RecoveryBoundPermit
	plan                *VerifiedAdmissionPlan
	history             *VerifiedAdmissionHistory
	inventory           *evidencefs.AdmissionInventory
	mutation            *evidencefs.AdmissionMutationToken
	runtimePublication  *evidencefs.Publication
	recoveryPublication *evidencefs.Publication
	canonical           [32]byte
}

var reserveReadyRegistry sync.Map

type ReceiptBoundReady struct {
	self             *ReceiptBoundReady
	prior            *ReserveReady
	plan             *VerifiedAdmissionPlan
	history          *VerifiedAdmissionHistory
	candidateBinding *verifiedEvidenceRunBinding
	inventory        *evidencefs.AdmissionInventory
	mutation         *evidencefs.AdmissionMutationToken
	runtimeReceipt   VerifiedContentReceipt
	recoveryReceipt  VerifiedDecisionRecoveryReceipt
	target, fullSet  [32]byte
	revision         uint64
	binding          *receiptBoundReadyBinding
	consumed         *atomic.Bool
}

type receiptBoundReadyBinding struct {
	ready           *ReceiptBoundReady
	prior           *ReserveReady
	plan            *VerifiedAdmissionPlan
	history         *VerifiedAdmissionHistory
	inventory       *evidencefs.AdmissionInventory
	mutation        *evidencefs.AdmissionMutationToken
	runtimeBinding  *verifiedContentReceiptBinding
	recoveryBinding *verifiedDecisionRecoveryReceiptBinding
	canonical       [32]byte
}

var receiptBoundReadyRegistry sync.Map

func (p *RecoveryBoundPermit) SealReserveReady(ctx context.Context, candidate OwnedCurrentCandidate) (ReserveReadyTransitionResult, error) {
	pre := ReserveReadyTransitionResult{outcome: evidencefs.AdmissionTransitionPreMutationFailure, candidateSequence: 5}
	if p == nil || p.inventory == nil || !validRecoveryBoundPermit(p, p.inventory, candidate) {
		return pre, fail(CodeEvidenceRecoveryRequired, "admission-reserve-ready", "recovery-bound admission permit is unavailable", nil)
	}
	pre.previousRevision = p.revision
	pre.candidateRevision = p.revision + 1
	pre.candidateDigest = reserveReadyCandidateDigest(p)
	if pre.candidateDigest == ([32]byte{}) || !p.consumed.CompareAndSwap(false, true) {
		return pre, fail(CodeEvidenceRecoveryRequired, "admission-reserve-ready", "reserve-ready input is invalid or consumed", nil)
	}
	fsResult, transitionErr := p.mutation.Advance(ctx, p.inventory)
	result := ReserveReadyTransitionResult{
		outcome: fsResult.Outcome(), candidateDigest: pre.candidateDigest, candidateSequence: 5,
		candidateRevision: fsResult.CandidateRevision(), previousRevision: fsResult.PreviousRevision(),
	}
	if fsResult.Outcome() != evidencefs.AdmissionTransitionDurable {
		if fsResult.Outcome() == evidencefs.AdmissionTransitionPreMutationFailure {
			p.consumed.CompareAndSwap(true, false)
		}
		return result, mapAdmissionMutationError(transitionErr, "admission-reserve-ready")
	}
	if transitionErr != nil || fsResult.CandidateKind() != "inventory_advance" || fsResult.CandidateSequence() != 0 || fsResult.CandidateDigest() != ([32]byte{}) || fsResult.PreviousRevision() != pre.previousRevision || fsResult.CandidateRevision() != pre.candidateRevision || fsResult.Inventory() == nil {
		_ = fsResult.Invalidate()
		return reserveReadyUnknown(result), admissionPostMutationFailure("admission-reserve-ready")
	}
	nextInventory := fsResult.Inventory()
	if err := nextInventory.Revalidate(ctx); err != nil {
		_ = fsResult.Invalidate()
		return reserveReadyUnknown(result), admissionPostMutationFailure("admission-reserve-ready-revalidate")
	}
	revision, err := nextInventory.Revision()
	if err != nil || revision != pre.candidateRevision {
		_ = fsResult.Invalidate()
		return reserveReadyUnknown(result), admissionPostMutationFailure("admission-reserve-ready-revision")
	}
	fullSet, err := nextInventory.FullSetDigest()
	if err != nil || fullSet != p.fullSet || !p.runtimePublication.Matches(digestRaw(p.runtimeDigest), p.runtimeSize) || !p.recoveryPublication.Matches(digestRaw(p.recoveryDigest), p.recoverySize) {
		_ = fsResult.Invalidate()
		return reserveReadyUnknown(result), admissionPostMutationFailure("admission-reserve-ready-full-set")
	}
	nextToken, err := nextInventory.MutationToken()
	if err != nil || !nextToken.ValidFor(nextInventory) {
		_ = fsResult.Invalidate()
		return reserveReadyUnknown(result), admissionPostMutationFailure("admission-reserve-ready-token")
	}
	ready := &ReserveReady{
		prior: p, plan: p.plan, history: p.plan.history, candidateBinding: candidate.binding,
		inventory: nextInventory, mutation: nextToken, runtimePublication: p.runtimePublication, recoveryPublication: p.recoveryPublication,
		target: p.target, fullSet: fullSet, revision: revision, runtimeDigest: p.runtimeDigest, runtimeSize: p.runtimeSize,
		recoveryDigest: p.recoveryDigest, recoverySize: p.recoverySize,
		lineageHeaderBytes: append([]byte(nil), p.plan.lineageHeaderBytes...), reservedFrameBytes: append([]byte(nil), p.plan.reservedFrameBytes...), consumed: &atomic.Bool{},
	}
	ready.self = ready
	binding := &reserveReadyBinding{
		ready: ready, prior: p, plan: p.plan, history: p.plan.history, inventory: nextInventory, mutation: nextToken,
		runtimePublication: p.runtimePublication, recoveryPublication: p.recoveryPublication,
	}
	ready.binding = binding
	binding.canonical = reserveReadyDigest(ready)
	reserveReadyRegistry.Store(binding, binding.canonical)
	if !validReserveReady(ready, nextInventory, candidate) {
		_ = fsResult.Invalidate()
		return reserveReadyUnknown(result), admissionPostMutationFailure("admission-reserve-ready-seal")
	}
	result.next = ready
	return result, nil
}

func reserveReadyUnknown(value ReserveReadyTransitionResult) ReserveReadyTransitionResult {
	value.outcome = evidencefs.AdmissionTransitionUnknown
	value.next = nil
	return value
}

func reserveReadyCandidateDigest(permit *RecoveryBoundPermit) [32]byte {
	if permit == nil || permit.plan == nil || permit.plan.binding == nil || permit.binding == nil {
		return [32]byte{}
	}
	h := sha256.New()
	h.Write([]byte("cloud-agents-platform-reserve-ready-candidate/v1\x00"))
	h.Write(permit.binding.canonical[:])
	h.Write(permit.plan.binding.canonical[:])
	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result
}

func reserveReadyDigest(ready *ReserveReady) [32]byte {
	if ready == nil || ready.self != ready || ready.prior == nil || ready.plan == nil || ready.history == nil || ready.candidateBinding == nil || ready.prior.binding == nil || ready.plan.binding == nil || ready.history.binding == nil {
		return [32]byte{}
	}
	h := sha256.New()
	h.Write([]byte("cloud-agents-platform-reserve-ready/v1\x00"))
	h.Write(ready.prior.binding.canonical[:])
	h.Write(ready.plan.binding.canonical[:])
	h.Write(ready.history.binding.canonical[:])
	h.Write(ready.candidateBinding.canonical[:])
	h.Write(ready.target[:])
	h.Write(ready.fullSet[:])
	writeAdmissionUint(h, ready.revision)
	writeAdmissionString(h, ready.runtimeDigest.String())
	writeAdmissionUint(h, ready.runtimeSize)
	writeAdmissionString(h, ready.recoveryDigest.String())
	writeAdmissionUint(h, ready.recoverySize)
	h.Write(ready.lineageHeaderBytes)
	h.Write(ready.reservedFrameBytes)
	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result
}

func validReserveReady(ready *ReserveReady, inventory *evidencefs.AdmissionInventory, candidate OwnedCurrentCandidate) bool {
	if ready == nil || ready.self != ready || ready.binding == nil || ready.binding.ready != ready || ready.prior == nil || ready.plan == nil || ready.history == nil || ready.inventory != inventory || ready.mutation == nil || ready.runtimePublication == nil || ready.recoveryPublication == nil || ready.candidateBinding != candidate.binding || ready.binding.prior != ready.prior || ready.binding.plan != ready.plan || ready.binding.history != ready.history || ready.binding.inventory != inventory || ready.binding.mutation != ready.mutation || ready.binding.runtimePublication != ready.runtimePublication || ready.binding.recoveryPublication != ready.recoveryPublication || ready.consumed == nil || ready.consumed.Load() || !validConsumedRecoveryBoundPermit(ready.prior, ready.plan, candidate) || ready.history != ready.plan.history || ready.revision != ready.prior.revision+1 || ready.target != ready.prior.target || ready.fullSet != ready.prior.fullSet || ready.runtimeDigest != candidate.runtimeArtifact.digest || ready.runtimeSize != candidate.runtimeArtifact.sizeBytes || ready.recoveryDigest != candidate.decisionRecoveryArtifact.digest || ready.recoverySize != candidate.decisionRecoveryArtifact.sizeBytes || !ready.runtimePublication.Matches(digestRaw(ready.runtimeDigest), ready.runtimeSize) || !ready.recoveryPublication.Matches(digestRaw(ready.recoveryDigest), ready.recoverySize) || string(ready.lineageHeaderBytes) != string(ready.plan.lineageHeaderBytes) || string(ready.reservedFrameBytes) != string(ready.plan.reservedFrameBytes) || !admissionPlanFramesExact(ready.plan) || !ready.mutation.ValidFor(inventory) || ready.binding.canonical == ([32]byte{}) || ready.binding.canonical != reserveReadyDigest(ready) {
		return false
	}
	registered, ok := reserveReadyRegistry.Load(ready.binding)
	if !ok || registered != ready.binding.canonical {
		return false
	}
	revision, err := inventory.Revision()
	if err != nil || revision != ready.revision {
		return false
	}
	fullSet, err := inventory.FullSetDigest()
	return err == nil && fullSet == ready.fullSet
}

func validConsumedRecoveryBoundPermit(permit *RecoveryBoundPermit, plan *VerifiedAdmissionPlan, candidate OwnedCurrentCandidate) bool {
	if permit == nil || permit.self != permit || permit.binding == nil || permit.binding.permit != permit || permit.prior == nil || permit.plan != plan || plan == nil || permit.inventory == nil || permit.mutation == nil || permit.runtimePublication == nil || permit.recoveryPublication == nil || permit.candidateBinding != candidate.binding || permit.binding.prior != permit.prior || permit.binding.plan != plan || permit.binding.inventory != permit.inventory || permit.binding.mutation != permit.mutation || permit.binding.runtimePublication != permit.runtimePublication || permit.binding.recoveryPublication != permit.recoveryPublication || permit.consumed == nil || !permit.consumed.Load() || !validConsumedRecoveryPublishedPermit(permit.prior, plan, candidate) || permit.revision != permit.prior.revision+1 || permit.target != permit.prior.target || permit.fullSet != permit.prior.fullSet || permit.runtimeDigest != candidate.runtimeArtifact.digest || permit.runtimeSize != candidate.runtimeArtifact.sizeBytes || permit.recoveryDigest != candidate.decisionRecoveryArtifact.digest || permit.recoverySize != candidate.decisionRecoveryArtifact.sizeBytes || !permit.runtimePublication.Matches(digestRaw(permit.runtimeDigest), permit.runtimeSize) || !permit.recoveryPublication.Matches(digestRaw(permit.recoveryDigest), permit.recoverySize) || permit.binding.canonical != recoveryBoundPermitDigest(permit) {
		return false
	}
	registered, ok := recoveryBoundPermitRegistry.Load(permit.binding)
	return ok && registered == permit.binding.canonical
}

// BindReceiptPair atomically mints both purpose-typed receipts. Neither
// receipt registry is updated until both artifact/publication checks and the
// same-store constraint succeed.
func (r *ReserveReady) BindReceiptPair(candidate OwnedCurrentCandidate) (*ReceiptBoundReady, error) {
	if r == nil || r.inventory == nil || !validReserveReady(r, r.inventory, candidate) || !r.consumed.CompareAndSwap(false, true) {
		return nil, fail(CodeEvidenceRecoveryRequired, "admission-receipt-pair", "reserve-ready authority is unavailable or consumed", nil)
	}
	runtimeReceipt, runtimeBinding, runtimeErr := mintRuntimeContentReceipt(candidate.owner, candidate.runtimeArtifact, r.runtimePublication)
	recoveryReceipt, recoveryBinding, recoveryErr := mintDecisionRecoveryReceipt(candidate.owner, candidate.decisionRecoveryArtifact, r.recoveryPublication)
	if runtimeErr != nil || recoveryErr != nil || runtimeBinding == nil || recoveryBinding == nil || !r.runtimePublication.SameStore(r.recoveryPublication) {
		r.consumed.CompareAndSwap(true, false)
		return nil, fail(CodeEvidenceRecoveryRequired, "admission-receipt-pair", "typed receipt pair cannot be bound", nil)
	}
	ready := &ReceiptBoundReady{
		prior: r, plan: r.plan, history: r.history, candidateBinding: candidate.binding,
		inventory: r.inventory, mutation: r.mutation, runtimeReceipt: runtimeReceipt, recoveryReceipt: recoveryReceipt,
		target: r.target, fullSet: r.fullSet, revision: r.revision, consumed: &atomic.Bool{},
	}
	ready.self = ready
	binding := &receiptBoundReadyBinding{
		ready: ready, prior: r, plan: r.plan, history: r.history, inventory: r.inventory, mutation: r.mutation,
		runtimeBinding: runtimeBinding, recoveryBinding: recoveryBinding,
	}
	ready.binding = binding
	binding.canonical = receiptBoundReadyDigest(ready)
	// Atomic at the authority layer: validators require all three registries.
	// No observable receipt escapes before these stores complete.
	verifiedContentReceiptRegistry.Store(runtimeBinding, runtimeBinding)
	verifiedDecisionRecoveryReceiptRegistry.Store(recoveryBinding, recoveryBinding)
	receiptBoundReadyRegistry.Store(binding, binding.canonical)
	if !validReceiptBoundReady(ready, r.inventory, candidate) {
		verifiedContentReceiptRegistry.Delete(runtimeBinding)
		verifiedDecisionRecoveryReceiptRegistry.Delete(recoveryBinding)
		receiptBoundReadyRegistry.Delete(binding)
		r.consumed.CompareAndSwap(true, false)
		return nil, fail(CodeEvidenceRecoveryRequired, "admission-receipt-pair", "typed receipt pair could not be sealed", nil)
	}
	return ready, nil
}

func receiptBoundReadyDigest(ready *ReceiptBoundReady) [32]byte {
	if ready == nil || ready.self != ready || ready.prior == nil || ready.plan == nil || ready.history == nil || ready.candidateBinding == nil || ready.prior.binding == nil || ready.plan.binding == nil || ready.history.binding == nil || ready.runtimeReceipt.binding == nil || ready.recoveryReceipt.binding == nil {
		return [32]byte{}
	}
	h := sha256.New()
	h.Write([]byte("cloud-agents-platform-receipt-bound-ready/v1\x00"))
	h.Write(ready.prior.binding.canonical[:])
	h.Write(ready.plan.binding.canonical[:])
	h.Write(ready.history.binding.canonical[:])
	h.Write(ready.candidateBinding.canonical[:])
	h.Write(ready.target[:])
	h.Write(ready.fullSet[:])
	writeAdmissionUint(h, ready.revision)
	writeAdmissionString(h, ready.runtimeReceipt.digest.String())
	writeAdmissionUint(h, ready.runtimeReceipt.sizeBytes)
	writeAdmissionString(h, ready.recoveryReceipt.digest.String())
	writeAdmissionUint(h, ready.recoveryReceipt.sizeBytes)
	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result
}

func validReceiptBoundReady(ready *ReceiptBoundReady, inventory *evidencefs.AdmissionInventory, candidate OwnedCurrentCandidate) bool {
	if ready == nil || ready.self != ready || ready.binding == nil || ready.binding.ready != ready || ready.prior == nil || ready.plan == nil || ready.history == nil || ready.inventory != inventory || ready.mutation == nil || ready.candidateBinding != candidate.binding || ready.binding.prior != ready.prior || ready.binding.plan != ready.plan || ready.binding.history != ready.history || ready.binding.inventory != inventory || ready.binding.mutation != ready.mutation || ready.binding.runtimeBinding != ready.runtimeReceipt.binding || ready.binding.recoveryBinding != ready.recoveryReceipt.binding || ready.consumed == nil || ready.consumed.Load() || !validConsumedReserveReady(ready.prior, ready.plan, candidate) || ready.history != ready.plan.history || ready.revision != ready.prior.revision || ready.target != ready.prior.target || ready.fullSet != ready.prior.fullSet || !validRuntimeReceipt(ready.runtimeReceipt, candidate.owner, candidate.runtimeArtifact.digest, candidate.runtimeArtifact.sizeBytes) || !validDecisionRecoveryReceipt(ready.recoveryReceipt, candidate.owner, candidate.decisionRecoveryArtifact.digest, candidate.decisionRecoveryArtifact.sizeBytes) || !ready.runtimeReceipt.publication.SameStore(ready.recoveryReceipt.publication) || !ready.mutation.ValidFor(inventory) || ready.binding.canonical == ([32]byte{}) || ready.binding.canonical != receiptBoundReadyDigest(ready) {
		return false
	}
	registered, ok := receiptBoundReadyRegistry.Load(ready.binding)
	return ok && registered == ready.binding.canonical
}

func validConsumedReserveReady(ready *ReserveReady, plan *VerifiedAdmissionPlan, candidate OwnedCurrentCandidate) bool {
	if ready == nil || ready.self != ready || ready.binding == nil || ready.binding.ready != ready || ready.prior == nil || ready.plan != plan || plan == nil || ready.history == nil || ready.inventory == nil || ready.mutation == nil || ready.runtimePublication == nil || ready.recoveryPublication == nil || ready.candidateBinding != candidate.binding || ready.binding.prior != ready.prior || ready.binding.plan != plan || ready.binding.history != ready.history || ready.binding.inventory != ready.inventory || ready.binding.mutation != ready.mutation || ready.binding.runtimePublication != ready.runtimePublication || ready.binding.recoveryPublication != ready.recoveryPublication || ready.consumed == nil || !ready.consumed.Load() || !validConsumedRecoveryBoundPermit(ready.prior, plan, candidate) || ready.history != plan.history || ready.revision != ready.prior.revision+1 || ready.target != ready.prior.target || ready.fullSet != ready.prior.fullSet || ready.runtimeDigest != candidate.runtimeArtifact.digest || ready.runtimeSize != candidate.runtimeArtifact.sizeBytes || ready.recoveryDigest != candidate.decisionRecoveryArtifact.digest || ready.recoverySize != candidate.decisionRecoveryArtifact.sizeBytes || !ready.runtimePublication.Matches(digestRaw(ready.runtimeDigest), ready.runtimeSize) || !ready.recoveryPublication.Matches(digestRaw(ready.recoveryDigest), ready.recoverySize) || string(ready.lineageHeaderBytes) != string(plan.lineageHeaderBytes) || string(ready.reservedFrameBytes) != string(plan.reservedFrameBytes) || ready.binding.canonical != reserveReadyDigest(ready) {
		return false
	}
	registered, ok := reserveReadyRegistry.Load(ready.binding)
	return ok && registered == ready.binding.canonical
}
