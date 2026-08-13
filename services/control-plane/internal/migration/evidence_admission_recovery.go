package migration

import (
	"context"
	"crypto/sha256"
	"sync"
	"sync/atomic"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/evidencefs"
)

type RecoveryPublicationTransitionResult struct {
	outcome           evidencefs.AdmissionTransitionOutcome
	next              *RecoveryPublishedPermit
	candidateDigest   [32]byte
	candidateSequence uint64
	candidateRevision uint64
	previousRevision  uint64
}

func (r RecoveryPublicationTransitionResult) Outcome() evidencefs.AdmissionTransitionOutcome {
	return r.outcome
}
func (r RecoveryPublicationTransitionResult) Next() *RecoveryPublishedPermit { return r.next }
func (r RecoveryPublicationTransitionResult) CandidateKind() string {
	return "decision_recovery_object"
}
func (r RecoveryPublicationTransitionResult) CandidateDigest() [32]byte { return r.candidateDigest }
func (r RecoveryPublicationTransitionResult) CandidateSequence() uint64 { return r.candidateSequence }
func (r RecoveryPublicationTransitionResult) CandidateRevision() uint64 { return r.candidateRevision }
func (r RecoveryPublicationTransitionResult) PreviousRevision() uint64  { return r.previousRevision }

type RecoveryPublishedPermit struct {
	self                *RecoveryPublishedPermit
	prior               *RuntimeBoundPermit
	plan                *VerifiedAdmissionPlan
	candidateBinding    *verifiedEvidenceRunBinding
	inventory           *evidencefs.AdmissionInventory
	mutation            *evidencefs.AdmissionMutationToken
	runtimePublication  *evidencefs.Publication
	recoveryPublication *evidencefs.Publication
	fsResult            evidencefs.AdmissionPublicationTransitionResult
	target, fullSet     [32]byte
	revision            uint64
	runtimeDigest       Digest
	runtimeSize         uint64
	recoveryDigest      Digest
	recoverySize        uint64
	reused              bool
	binding             *recoveryPublishedPermitBinding
	consumed            *atomic.Bool
}

type recoveryPublishedPermitBinding struct {
	permit              *RecoveryPublishedPermit
	prior               *RuntimeBoundPermit
	plan                *VerifiedAdmissionPlan
	inventory           *evidencefs.AdmissionInventory
	mutation            *evidencefs.AdmissionMutationToken
	runtimePublication  *evidencefs.Publication
	recoveryPublication *evidencefs.Publication
	canonical           [32]byte
}

var recoveryPublishedPermitRegistry sync.Map

func (p *RuntimeBoundPermit) PublishDecisionRecovery(ctx context.Context, candidate OwnedCurrentCandidate) (RecoveryPublicationTransitionResult, error) {
	pre := RecoveryPublicationTransitionResult{outcome: evidencefs.AdmissionTransitionPreMutationFailure, candidateSequence: 3, candidateDigest: digestRaw(candidate.decisionRecoveryArtifact.digest)}
	if p == nil || p.inventory == nil || !validRuntimeBoundPermit(p, p.inventory, candidate) {
		return pre, fail(CodeEvidenceRecoveryRequired, "admission-recovery-publish", "runtime-bound admission permit is unavailable", nil)
	}
	recovery := candidate.decisionRecoveryArtifact
	pre.previousRevision = p.revision
	pre.candidateRevision = p.revision + 1
	if !p.consumed.CompareAndSwap(false, true) {
		return pre, fail(CodeEvidenceRecoveryRequired, "admission-recovery-publish", "runtime-bound admission permit was already consumed", nil)
	}
	fsResult, transitionErr := p.mutation.PublishObject(ctx, p.inventory, pre.candidateDigest, recovery.bytes)
	result := RecoveryPublicationTransitionResult{
		outcome: fsResult.Outcome(), candidateDigest: fsResult.CandidateDigest(), candidateSequence: 3,
		candidateRevision: fsResult.CandidateRevision(), previousRevision: fsResult.PreviousRevision(),
	}
	if fsResult.Outcome() != evidencefs.AdmissionTransitionDurable {
		if fsResult.Outcome() == evidencefs.AdmissionTransitionPreMutationFailure {
			p.consumed.CompareAndSwap(true, false)
		}
		return result, mapAdmissionMutationError(transitionErr, "admission-recovery-publish")
	}
	if transitionErr != nil || fsResult.CandidateKind() != "content_object" || fsResult.CandidateSequence() != 0 || fsResult.CandidateDigest() != pre.candidateDigest || fsResult.Size() != recovery.sizeBytes || fsResult.PreviousRevision() != pre.previousRevision || fsResult.CandidateRevision() != pre.candidateRevision || !fsResult.ValidFor(fsResult.Inventory()) {
		_ = fsResult.Invalidate()
		return recoveryPublicationUnknown(result), admissionPostMutationFailure("admission-recovery-publish")
	}
	nextInventory := fsResult.Inventory()
	if err := nextInventory.Revalidate(ctx); err != nil {
		_ = fsResult.Invalidate()
		return recoveryPublicationUnknown(result), admissionPostMutationFailure("admission-recovery-publish-revalidate")
	}
	revision, err := nextInventory.Revision()
	if err != nil || revision != pre.candidateRevision {
		_ = fsResult.Invalidate()
		return recoveryPublicationUnknown(result), admissionPostMutationFailure("admission-recovery-publish-revision")
	}
	target, err := nextInventory.Target()
	if err != nil || target != p.target {
		_ = fsResult.Invalidate()
		return recoveryPublicationUnknown(result), admissionPostMutationFailure("admission-recovery-publish-target")
	}
	fullSet, err := nextInventory.FullSetDigest()
	if err != nil || fullSet == ([32]byte{}) || (fullSet == p.fullSet) != fsResult.Reused() || !p.publication.Matches(digestRaw(p.digest), p.size) {
		_ = fsResult.Invalidate()
		return recoveryPublicationUnknown(result), admissionPostMutationFailure("admission-recovery-publish-full-set")
	}
	nextToken, err := nextInventory.MutationToken()
	if err != nil || !nextToken.ValidFor(nextInventory) {
		_ = fsResult.Invalidate()
		return recoveryPublicationUnknown(result), admissionPostMutationFailure("admission-recovery-publish-token")
	}
	next := &RecoveryPublishedPermit{
		prior: p, plan: p.plan, candidateBinding: candidate.binding, inventory: nextInventory, mutation: nextToken,
		runtimePublication: p.publication, recoveryPublication: fsResult.Publication(), fsResult: fsResult,
		target: target, fullSet: fullSet, revision: revision, runtimeDigest: p.digest, runtimeSize: p.size,
		recoveryDigest: recovery.digest, recoverySize: recovery.sizeBytes, reused: fsResult.Reused(), consumed: &atomic.Bool{},
	}
	next.self = next
	binding := &recoveryPublishedPermitBinding{
		permit: next, prior: p, plan: p.plan, inventory: nextInventory, mutation: nextToken,
		runtimePublication: p.publication, recoveryPublication: next.recoveryPublication,
	}
	next.binding = binding
	binding.canonical = recoveryPublishedPermitDigest(next)
	recoveryPublishedPermitRegistry.Store(binding, binding.canonical)
	if !validRecoveryPublishedPermit(next, nextInventory, candidate) {
		_ = fsResult.Invalidate()
		return recoveryPublicationUnknown(result), admissionPostMutationFailure("admission-recovery-publish-seal")
	}
	result.next = next
	return result, nil
}

func recoveryPublicationUnknown(value RecoveryPublicationTransitionResult) RecoveryPublicationTransitionResult {
	value.outcome = evidencefs.AdmissionTransitionUnknown
	value.next = nil
	return value
}

func recoveryPublishedPermitDigest(permit *RecoveryPublishedPermit) [32]byte {
	if permit == nil || permit.self != permit || permit.prior == nil || permit.plan == nil || permit.candidateBinding == nil || permit.prior.binding == nil || permit.plan.binding == nil {
		return [32]byte{}
	}
	h := sha256.New()
	h.Write([]byte("cloud-agents-platform-recovery-published-permit/v1\x00"))
	h.Write(permit.prior.binding.canonical[:])
	h.Write(permit.plan.binding.canonical[:])
	h.Write(permit.candidateBinding.canonical[:])
	h.Write(permit.target[:])
	h.Write(permit.fullSet[:])
	writeAdmissionUint(h, permit.revision)
	writeAdmissionString(h, permit.runtimeDigest.String())
	writeAdmissionUint(h, permit.runtimeSize)
	writeAdmissionString(h, permit.recoveryDigest.String())
	writeAdmissionUint(h, permit.recoverySize)
	if permit.reused {
		h.Write([]byte{1})
	} else {
		h.Write([]byte{0})
	}
	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result
}

func validRecoveryPublishedPermit(permit *RecoveryPublishedPermit, inventory *evidencefs.AdmissionInventory, candidate OwnedCurrentCandidate) bool {
	if permit == nil || permit.self != permit || permit.binding == nil || permit.binding.permit != permit || permit.prior == nil || permit.plan == nil || permit.inventory != inventory || permit.mutation == nil || permit.runtimePublication == nil || permit.recoveryPublication == nil || permit.candidateBinding != candidate.binding || permit.binding.prior != permit.prior || permit.binding.plan != permit.plan || permit.binding.inventory != inventory || permit.binding.mutation != permit.mutation || permit.binding.runtimePublication != permit.runtimePublication || permit.binding.recoveryPublication != permit.recoveryPublication || permit.consumed == nil || permit.consumed.Load() || !validConsumedRuntimeBoundPermit(permit.prior, permit.plan, candidate) || permit.revision != permit.prior.revision+1 || permit.target != permit.prior.target || (permit.fullSet == permit.prior.fullSet) != permit.reused || permit.runtimeDigest != candidate.runtimeArtifact.digest || permit.runtimeSize != candidate.runtimeArtifact.sizeBytes || permit.recoveryDigest != candidate.decisionRecoveryArtifact.digest || permit.recoverySize != candidate.decisionRecoveryArtifact.sizeBytes || !permit.runtimePublication.Matches(digestRaw(permit.runtimeDigest), permit.runtimeSize) || permit.fsResult.Publication() != permit.recoveryPublication || permit.fsResult.CandidateDigest() != digestRaw(permit.recoveryDigest) || permit.fsResult.Size() != permit.recoverySize || permit.fsResult.Reused() != permit.reused || !permit.fsResult.ValidFor(inventory) || !permit.mutation.ValidFor(inventory) || permit.binding.canonical == ([32]byte{}) || permit.binding.canonical != recoveryPublishedPermitDigest(permit) {
		return false
	}
	registered, ok := recoveryPublishedPermitRegistry.Load(permit.binding)
	if !ok || registered != permit.binding.canonical {
		return false
	}
	revision, err := inventory.Revision()
	if err != nil || revision != permit.revision {
		return false
	}
	fullSet, err := inventory.FullSetDigest()
	return err == nil && fullSet == permit.fullSet
}

func validConsumedRuntimeBoundPermit(permit *RuntimeBoundPermit, plan *VerifiedAdmissionPlan, candidate OwnedCurrentCandidate) bool {
	if permit == nil || permit.self != permit || permit.binding == nil || permit.binding.permit != permit || permit.prior == nil || permit.plan != plan || plan == nil || permit.inventory == nil || permit.mutation == nil || permit.publication == nil || permit.candidateBinding != candidate.binding || permit.binding.prior != permit.prior || permit.binding.plan != plan || permit.binding.inventory != permit.inventory || permit.binding.mutation != permit.mutation || permit.binding.publication != permit.publication || permit.consumed == nil || !permit.consumed.Load() || !validConsumedRuntimePublishedPermit(permit.prior, plan, candidate) || permit.revision != permit.prior.revision+1 || permit.target != permit.prior.target || permit.fullSet != permit.prior.fullSet || permit.digest != candidate.runtimeArtifact.digest || permit.size != candidate.runtimeArtifact.sizeBytes || !permit.publication.Matches(digestRaw(permit.digest), permit.size) || permit.binding.canonical != runtimeBoundPermitDigest(permit) {
		return false
	}
	registered, ok := runtimeBoundPermitRegistry.Load(permit.binding)
	return ok && registered == permit.binding.canonical
}
