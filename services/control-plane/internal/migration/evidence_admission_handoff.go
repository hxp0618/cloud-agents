package migration

import (
	"context"
	"crypto/sha256"
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
