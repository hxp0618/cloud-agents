package migration

import (
	"context"
	"crypto/sha256"
	"sync"
	"sync/atomic"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/evidencefs"
)

type successorAdmissionStage string

const (
	successorAdmissionPrepared          successorAdmissionStage = "prepared"
	successorAdmissionRuntimePublished  successorAdmissionStage = "runtime_published"
	successorAdmissionRuntimeBound      successorAdmissionStage = "runtime_bound"
	successorAdmissionRecoveryPublished successorAdmissionStage = "recovery_published"
	successorAdmissionRecoveryBound     successorAdmissionStage = "recovery_bound"
	successorAdmissionReserveReady      successorAdmissionStage = "reserve_ready"
	successorAdmissionReceiptBound      successorAdmissionStage = "receipt_bound"
	successorAdmissionAdjacentReady     successorAdmissionStage = "adjacent_reserve_ready"
	successorAdmissionReservedDurable   successorAdmissionStage = "reserved_durable"
	successorAdmissionHeaderDurable     successorAdmissionStage = "header_durable"
	successorAdmissionGenerationReady   successorAdmissionStage = "generation_ready"
)

// SuccessorAdmissionTransitionResult is the closed migration projection of
// one evidencefs transition. Only durable results carry the next concrete
// authority type. Pre-mutation failure is retryable; unknown never is.
type SuccessorAdmissionTransitionResult[T any] struct {
	outcome           evidencefs.AdmissionTransitionOutcome
	next              T
	candidateKind     string
	candidateDigest   [32]byte
	candidateSequence uint64
	candidateRevision uint64
	previousRevision  uint64
}

func (r SuccessorAdmissionTransitionResult[T]) Outcome() evidencefs.AdmissionTransitionOutcome {
	return r.outcome
}
func (r SuccessorAdmissionTransitionResult[T]) Next() T                   { return r.next }
func (r SuccessorAdmissionTransitionResult[T]) CandidateKind() string     { return r.candidateKind }
func (r SuccessorAdmissionTransitionResult[T]) CandidateDigest() [32]byte { return r.candidateDigest }
func (r SuccessorAdmissionTransitionResult[T]) CandidateSequence() uint64 { return r.candidateSequence }
func (r SuccessorAdmissionTransitionResult[T]) CandidateRevision() uint64 { return r.candidateRevision }
func (r SuccessorAdmissionTransitionResult[T]) PreviousRevision() uint64  { return r.previousRevision }

type SuccessorAdmissionPermit struct {
	self  *SuccessorAdmissionPermit
	state *successorAdmissionState
}

type SuccessorRuntimePublishedPermit struct {
	self  *SuccessorRuntimePublishedPermit
	state *successorAdmissionState
}

type SuccessorRuntimeBoundPermit struct {
	self  *SuccessorRuntimeBoundPermit
	state *successorAdmissionState
}

type SuccessorRecoveryPublishedPermit struct {
	self  *SuccessorRecoveryPublishedPermit
	state *successorAdmissionState
}

type SuccessorRecoveryBoundPermit struct {
	self  *SuccessorRecoveryBoundPermit
	state *successorAdmissionState
}

type SuccessorReserveReady struct {
	self  *SuccessorReserveReady
	state *successorAdmissionState
}

type SuccessorReceiptBoundReady struct {
	self  *SuccessorReceiptBoundReady
	state *successorAdmissionState
}

// successorAdmissionState is immutable after sealing. Its concrete wrapper is
// retained in the binding and registry, so a copied or alternate wrapper
// cannot adopt the state. Old states remain diagnostic only after consumption;
// only the current inventory/token pair can authorize the next transition.
type successorAdmissionState struct {
	self                *successorAdmissionState
	stage               successorAdmissionStage
	prior               *successorAdmissionState
	plan                *VerifiedSuccessorAdmissionPlan
	history             *VerifiedAdmissionHistory
	candidateBinding    *verifiedEvidenceRunBinding
	inventory           *evidencefs.AdmissionInventory
	mutation            *evidencefs.AdmissionMutationToken
	runtimePublication  *evidencefs.Publication
	recoveryPublication *evidencefs.Publication
	fsPublication       evidencefs.AdmissionPublicationTransitionResult
	fsIndex             evidencefs.AdmissionTransitionResult
	fsJournal           evidencefs.AdmissionJournalTransitionResult
	target, fullSet     [32]byte
	revision            uint64
	runtimeDigest       Digest
	runtimeSize         uint64
	recoveryDigest      Digest
	recoverySize        uint64
	runtimeReused       bool
	recoveryReused      bool
	runtimeReceipt      VerifiedContentReceipt
	recoveryReceipt     VerifiedDecisionRecoveryReceipt
	indexPrefixDigest   [32]byte
	indexDigest         [32]byte
	framedDigest        [32]byte
	indexPrefixSize     uint64
	indexSize           uint64
	indexRecords        uint64
	indexTail           Digest
	supersededDigest    Digest
	reservedDigest      Digest
	journal             Digest
	headerDigest        Digest
	journalCount        uint64
	activationHeader    ownedActivationHeader
	headerFrame         EvidenceFrame
	headerBytes         []byte
	headerBytesHash     [32]byte
	fsJournalCandidate  [32]byte
	activatedFrame      LineageIndexFrame
	activatedBytes      []byte
	activationBytesHash [32]byte
	activationDigest    Digest
	binding             *successorAdmissionStateBinding
	consumed            *atomic.Bool
}

type successorAdmissionStateBinding struct {
	state                  *successorAdmissionState
	owner                  any
	prior                  *successorAdmissionState
	plan                   *VerifiedSuccessorAdmissionPlan
	history                *VerifiedAdmissionHistory
	candidateBinding       *verifiedEvidenceRunBinding
	inventory              *evidencefs.AdmissionInventory
	mutation               *evidencefs.AdmissionMutationToken
	runtimePublication     *evidencefs.Publication
	recoveryPublication    *evidencefs.Publication
	runtimeReceiptBinding  *verifiedContentReceiptBinding
	recoveryReceiptBinding *verifiedDecisionRecoveryReceiptBinding
	consumed               *atomic.Bool
	canonical              [32]byte
}

type successorAdmissionStateRecord struct {
	state                  *successorAdmissionState
	owner                  any
	prior                  *successorAdmissionState
	plan                   *VerifiedSuccessorAdmissionPlan
	history                *VerifiedAdmissionHistory
	candidateBinding       *verifiedEvidenceRunBinding
	inventory              *evidencefs.AdmissionInventory
	mutation               *evidencefs.AdmissionMutationToken
	runtimePublication     *evidencefs.Publication
	recoveryPublication    *evidencefs.Publication
	runtimeReceiptBinding  *verifiedContentReceiptBinding
	recoveryReceiptBinding *verifiedDecisionRecoveryReceiptBinding
	consumed               *atomic.Bool
	canonical              [32]byte
}

var successorAdmissionStateRegistry sync.Map

// bindSuccessorAdmissionPermit consumes a verified successor plan only after
// it has been cross-bound to the exact current evidencefs inventory and token.
// It performs no filesystem mutation and therefore restores plan authority if
// the in-memory seal cannot complete.
func bindSuccessorAdmissionPermit(ctx context.Context, inventory *evidencefs.AdmissionInventory, mutation *evidencefs.AdmissionMutationToken, plan *VerifiedSuccessorAdmissionPlan, candidate OwnedCurrentCandidate) (*SuccessorAdmissionPermit, error) {
	if inventory == nil || mutation == nil || plan == nil || !validVerifiedSuccessorAdmissionPlan(plan, plan.history, candidate) || inventory != plan.history.inventory || !mutation.ValidFor(inventory) {
		return nil, fail(CodeEvidenceRecoveryRequired, "successor-admission-permit", "verified successor plan or filesystem authority is unavailable", nil)
	}
	if err := contextAdmissionError(ctx); err != nil {
		return nil, err
	}
	if err := inventory.Revalidate(ctx); err != nil {
		return nil, mapEvidenceAdmissionError(err, "successor-admission-permit-revalidate")
	}
	revision, revisionErr := inventory.Revision()
	target, targetErr := inventory.Target()
	fullSet, fullSetErr := inventory.FullSetDigest()
	if revisionErr != nil || targetErr != nil || fullSetErr != nil || revision != plan.history.revision || target != plan.history.target || fullSet == ([32]byte{}) || fullSet != plan.history.fullSet || !mutation.ValidFor(inventory) || !validVerifiedSuccessorAdmissionPlan(plan, plan.history, candidate) {
		return nil, fail(CodeEvidenceRecoveryRequired, "successor-admission-permit", "successor inventory no longer matches the verified history", nil)
	}
	if !plan.consumed.CompareAndSwap(false, true) {
		return nil, fail(CodeEvidenceRecoveryRequired, "successor-admission-permit", "verified successor plan was already consumed", nil)
	}
	state := &successorAdmissionState{
		stage: successorAdmissionPrepared, plan: plan, history: plan.history, candidateBinding: candidate.binding,
		inventory: inventory, mutation: mutation, target: target, fullSet: fullSet, revision: revision,
		runtimeDigest: candidate.runtimeArtifact.digest, runtimeSize: candidate.runtimeArtifact.sizeBytes,
		recoveryDigest: candidate.decisionRecoveryArtifact.digest, recoverySize: candidate.decisionRecoveryArtifact.sizeBytes,
		consumed: &atomic.Bool{},
	}
	permit := &SuccessorAdmissionPermit{state: state}
	permit.self = permit
	if !sealSuccessorAdmissionState(permit, state) || !validSuccessorAdmissionState(permit, state, successorAdmissionPrepared, candidate) {
		if state.binding != nil {
			successorAdmissionStateRegistry.Delete(state.binding)
		}
		plan.consumed.CompareAndSwap(true, false)
		return nil, admissionFailed("successor-admission-permit", "successor admission permit could not be sealed", nil)
	}
	return permit, nil
}

func sealSuccessorAdmissionState(owner any, state *successorAdmissionState) bool {
	if state == nil || state.self != nil || state.binding != nil || state.consumed == nil {
		return false
	}
	state.self = state
	binding := &successorAdmissionStateBinding{
		state: state, owner: owner, prior: state.prior, plan: state.plan, history: state.history,
		candidateBinding: state.candidateBinding, inventory: state.inventory, mutation: state.mutation,
		runtimePublication: state.runtimePublication, recoveryPublication: state.recoveryPublication,
		runtimeReceiptBinding: state.runtimeReceipt.binding, recoveryReceiptBinding: state.recoveryReceipt.binding,
		consumed: state.consumed,
	}
	state.binding = binding
	binding.canonical = successorAdmissionStateDigest(state)
	if binding.canonical == ([32]byte{}) || !successorStageOwnerValid(owner, state) {
		return false
	}
	record := successorAdmissionStateRecord{
		state: state, owner: owner, prior: state.prior, plan: state.plan, history: state.history,
		candidateBinding: state.candidateBinding, inventory: state.inventory, mutation: state.mutation,
		runtimePublication: state.runtimePublication, recoveryPublication: state.recoveryPublication,
		runtimeReceiptBinding: state.runtimeReceipt.binding, recoveryReceiptBinding: state.recoveryReceipt.binding,
		consumed:  state.consumed,
		canonical: binding.canonical,
	}
	successorAdmissionStateRegistry.Store(binding, record)
	return validStoredSuccessorAdmissionState(state)
}

func successorStageOwnerValid(owner any, state *successorAdmissionState) bool {
	if state == nil {
		return false
	}
	switch value := owner.(type) {
	case *SuccessorAdmissionPermit:
		return state.stage == successorAdmissionPrepared && value != nil && value.self == value && value.state == state
	case *SuccessorRuntimePublishedPermit:
		return state.stage == successorAdmissionRuntimePublished && value != nil && value.self == value && value.state == state
	case *SuccessorRuntimeBoundPermit:
		return state.stage == successorAdmissionRuntimeBound && value != nil && value.self == value && value.state == state
	case *SuccessorRecoveryPublishedPermit:
		return state.stage == successorAdmissionRecoveryPublished && value != nil && value.self == value && value.state == state
	case *SuccessorRecoveryBoundPermit:
		return state.stage == successorAdmissionRecoveryBound && value != nil && value.self == value && value.state == state
	case *SuccessorReserveReady:
		return state.stage == successorAdmissionReserveReady && value != nil && value.self == value && value.state == state
	case *SuccessorReceiptBoundReady:
		return state.stage == successorAdmissionReceiptBound && value != nil && value.self == value && value.state == state
	case *SuccessorAdjacentReserveReady:
		return state.stage == successorAdmissionAdjacentReady && value != nil && value.self == value && value.state == state
	case *SuccessorReservedDurablePermit:
		return state.stage == successorAdmissionReservedDurable && value != nil && value.self == value && value.state == state
	case *SuccessorHeaderDurablePermit:
		return state.stage == successorAdmissionHeaderDurable && value != nil && value.self == value && value.state == state
	case *SuccessorGenerationReadyPermit:
		return state.stage == successorAdmissionGenerationReady && value != nil && value.self == value && value.state == state
	default:
		return false
	}
}

func validStoredSuccessorAdmissionState(state *successorAdmissionState) bool {
	if state == nil || state.self != state || state.binding == nil || state.binding.state != state || state.plan == nil || state.plan.binding == nil || state.history == nil || state.history.binding == nil || state.history != state.plan.history || state.candidateBinding == nil || state.candidateBinding != state.plan.candidateBinding || state.inventory == nil || state.mutation == nil || state.consumed == nil || state.binding.owner == nil || !successorStageOwnerValid(state.binding.owner, state) || state.binding.prior != state.prior || state.binding.plan != state.plan || state.binding.history != state.history || state.binding.candidateBinding != state.candidateBinding || state.binding.inventory != state.inventory || state.binding.mutation != state.mutation || state.binding.runtimePublication != state.runtimePublication || state.binding.recoveryPublication != state.recoveryPublication || state.binding.runtimeReceiptBinding != state.runtimeReceipt.binding || state.binding.recoveryReceiptBinding != state.recoveryReceipt.binding || state.binding.consumed != state.consumed || state.plan.consumed == nil || !state.plan.consumed.Load() || state.plan.binding.canonical == ([32]byte{}) || state.plan.binding.canonical != verifiedSuccessorAdmissionPlanDigest(state.plan) || !successorAdmissionPlanFramesExact(state.plan) || state.history.binding.canonical == ([32]byte{}) || state.history.binding.canonical != admissionHistoryDigest(state.history) || !state.history.rootFacts.valid() || state.runtimeDigest.Validate() != nil || state.recoveryDigest.Validate() != nil || state.runtimeSize == 0 || state.recoverySize == 0 || state.target != state.history.target || state.binding.canonical == ([32]byte{}) || state.binding.canonical != successorAdmissionStateDigest(state) {
		return false
	}
	reserved := state.plan.reservedFrame.Record.Reserved
	if reserved == nil || reserved.PlannedSegment0Header.OuterArtifactDigest != state.runtimeDigest || reserved.PlannedSegment0Header.OuterArtifactSizeBytes != state.runtimeSize || reserved.PlannedSegment0Header.DecisionRecoveryArtifactSHA256 != state.recoveryDigest || reserved.PlannedSegment0Header.DecisionRecoveryArtifactSizeBytes != state.recoverySize {
		return false
	}
	recordValue, ok := successorAdmissionStateRegistry.Load(state.binding)
	if !ok {
		return false
	}
	record, ok := recordValue.(successorAdmissionStateRecord)
	if !ok || record.state != state || record.owner != state.binding.owner || record.prior != state.prior || record.plan != state.plan || record.history != state.history || record.candidateBinding != state.candidateBinding || record.inventory != state.inventory || record.mutation != state.mutation || record.runtimePublication != state.runtimePublication || record.recoveryPublication != state.recoveryPublication || record.runtimeReceiptBinding != state.runtimeReceipt.binding || record.recoveryReceiptBinding != state.recoveryReceipt.binding || record.consumed != state.consumed || record.canonical != state.binding.canonical {
		return false
	}
	registeredHistory, historyOK := verifiedAdmissionHistoryRegistry.Load(state.history.binding)
	registeredPlan, planOK := verifiedSuccessorAdmissionPlanRegistry.Load(state.plan.binding)
	if !historyOK || registeredHistory != state.history.binding.canonical || !planOK || registeredPlan != state.plan.binding.canonical {
		return false
	}
	if state.prior != nil {
		if !validStoredSuccessorAdmissionState(state.prior) || state.prior.consumed == nil || !state.prior.consumed.Load() || state.plan != state.prior.plan || state.history != state.prior.history || state.candidateBinding != state.prior.candidateBinding || state.target != state.prior.target || state.runtimeDigest != state.prior.runtimeDigest || state.runtimeSize != state.prior.runtimeSize || state.recoveryDigest != state.prior.recoveryDigest || state.recoverySize != state.prior.recoverySize {
			return false
		}
	}
	return validSuccessorAdmissionStageShape(state)
}

func validSuccessorAdmissionStageShape(state *successorAdmissionState) bool {
	if state == nil {
		return false
	}
	emptyReceipts := state.runtimeReceipt.binding == nil && state.recoveryReceipt.binding == nil
	emptyHeader := emptySuccessorHeaderFacts(state)
	emptyActivation := emptySuccessorActivationFacts(state)
	switch state.stage {
	case successorAdmissionPrepared:
		return state.prior == nil && state.revision == state.history.revision && state.fullSet == state.history.fullSet && state.runtimePublication == nil && state.recoveryPublication == nil && emptyAdmissionPublicationResult(state.fsPublication) && emptySuccessorIndexFacts(state) && emptyHeader && emptyActivation && !state.runtimeReused && !state.recoveryReused && emptyReceipts
	case successorAdmissionRuntimePublished:
		return state.prior != nil && state.prior.stage == successorAdmissionPrepared && state.revision == state.prior.revision+1 && state.recoveryPublication == nil && state.runtimePublication != nil && validStoredSuccessorPublicationResult(state, state.runtimePublication, state.runtimeDigest, state.runtimeSize, state.runtimeReused) && emptySuccessorIndexFacts(state) && emptyHeader && emptyActivation && !state.recoveryReused && emptyReceipts && (state.fullSet == state.prior.fullSet) == state.runtimeReused
	case successorAdmissionRuntimeBound:
		return state.prior != nil && state.prior.stage == successorAdmissionRuntimePublished && state.revision == state.prior.revision+1 && state.runtimePublication == state.prior.runtimePublication && state.recoveryPublication == nil && emptyAdmissionPublicationResult(state.fsPublication) && emptySuccessorIndexFacts(state) && emptyHeader && emptyActivation && state.runtimeReused == state.prior.runtimeReused && !state.recoveryReused && emptyReceipts && state.fullSet == state.prior.fullSet
	case successorAdmissionRecoveryPublished:
		return state.prior != nil && state.prior.stage == successorAdmissionRuntimeBound && state.revision == state.prior.revision+1 && state.runtimePublication == state.prior.runtimePublication && state.recoveryPublication != nil && validStoredSuccessorPublicationResult(state, state.recoveryPublication, state.recoveryDigest, state.recoverySize, state.recoveryReused) && emptySuccessorIndexFacts(state) && emptyHeader && emptyActivation && state.runtimeReused == state.prior.runtimeReused && emptyReceipts && (state.fullSet == state.prior.fullSet) == state.recoveryReused
	case successorAdmissionRecoveryBound:
		return state.prior != nil && state.prior.stage == successorAdmissionRecoveryPublished && state.revision == state.prior.revision+1 && state.runtimePublication == state.prior.runtimePublication && state.recoveryPublication == state.prior.recoveryPublication && emptyAdmissionPublicationResult(state.fsPublication) && emptySuccessorIndexFacts(state) && emptyHeader && emptyActivation && state.runtimeReused == state.prior.runtimeReused && state.recoveryReused == state.prior.recoveryReused && emptyReceipts && state.fullSet == state.prior.fullSet
	case successorAdmissionReserveReady:
		return state.prior != nil && state.prior.stage == successorAdmissionRecoveryBound && state.revision == state.prior.revision+1 && state.runtimePublication == state.prior.runtimePublication && state.recoveryPublication == state.prior.recoveryPublication && emptyAdmissionPublicationResult(state.fsPublication) && emptySuccessorIndexFacts(state) && emptyHeader && emptyActivation && state.runtimeReused == state.prior.runtimeReused && state.recoveryReused == state.prior.recoveryReused && emptyReceipts && state.fullSet == state.prior.fullSet
	case successorAdmissionReceiptBound:
		return state.prior != nil && state.prior.stage == successorAdmissionReserveReady && state.revision == state.prior.revision && state.inventory == state.prior.inventory && state.mutation == state.prior.mutation && state.runtimePublication == state.prior.runtimePublication && state.recoveryPublication == state.prior.recoveryPublication && emptyAdmissionPublicationResult(state.fsPublication) && emptySuccessorIndexFacts(state) && emptyHeader && emptyActivation && state.runtimeReused == state.prior.runtimeReused && state.recoveryReused == state.prior.recoveryReused && state.runtimeReceipt.binding != nil && state.recoveryReceipt.binding != nil && state.fullSet == state.prior.fullSet
	case successorAdmissionAdjacentReady:
		return state.prior != nil && state.prior.stage == successorAdmissionReceiptBound && state.revision == state.prior.revision+1 && state.runtimePublication == state.prior.runtimePublication && state.recoveryPublication == state.prior.recoveryPublication && state.runtimeReceipt.binding == state.prior.runtimeReceipt.binding && state.recoveryReceipt.binding == state.prior.recoveryReceipt.binding && state.runtimeReused == state.prior.runtimeReused && state.recoveryReused == state.prior.recoveryReused && emptyAdmissionPublicationResult(state.fsPublication) && validStoredSuccessorIndexResult(state, state.plan.supersededFrameBytes, state.plan.supersededFrame.RecordDigest) && emptyHeader && emptyActivation && state.indexRecords == state.history.targetIndexRecords+1 && state.fullSet != state.prior.fullSet
	case successorAdmissionReservedDurable:
		return state.prior != nil && state.prior.stage == successorAdmissionAdjacentReady && state.revision == state.prior.revision+1 && state.runtimePublication == state.prior.runtimePublication && state.recoveryPublication == state.prior.recoveryPublication && state.runtimeReceipt.binding == state.prior.runtimeReceipt.binding && state.recoveryReceipt.binding == state.prior.recoveryReceipt.binding && state.runtimeReused == state.prior.runtimeReused && state.recoveryReused == state.prior.recoveryReused && emptyAdmissionPublicationResult(state.fsPublication) && validStoredSuccessorIndexResult(state, state.plan.reservedFrameBytes, state.plan.reservedFrame.RecordDigest) && emptyHeader && emptyActivation && state.indexPrefixDigest == state.prior.indexDigest && state.indexPrefixSize == state.prior.indexSize && state.indexRecords == state.prior.indexRecords+1 && state.supersededDigest == state.prior.supersededDigest && state.fullSet != state.prior.fullSet
	case successorAdmissionHeaderDurable:
		return state.prior != nil && state.prior.stage == successorAdmissionReservedDurable && state.revision == state.prior.revision+1 && successorStageCarriesReceipts(state) && emptyAdmissionPublicationResult(state.fsPublication) && emptyAdmissionTransitionResult(state.fsIndex) && validStoredSuccessorHeaderFacts(state) && emptyActivation && successorIndexFactsEqual(state, state.prior) && state.fullSet != state.prior.fullSet
	case successorAdmissionGenerationReady:
		return state.prior != nil && state.prior.stage == successorAdmissionHeaderDurable && state.revision == state.prior.revision+1 && successorStageCarriesReceipts(state) && emptyAdmissionPublicationResult(state.fsPublication) && validStoredSuccessorIndexResult(state, state.activatedBytes, state.activationDigest) && validStoredSuccessorHeaderFacts(state) && validStoredSuccessorActivationFacts(state) && state.indexPrefixDigest == state.prior.indexDigest && state.indexPrefixSize == state.prior.indexSize && state.indexRecords == state.prior.indexRecords+1 && state.supersededDigest == state.prior.supersededDigest && state.reservedDigest == state.prior.reservedDigest && state.journal == state.prior.journal && state.headerDigest == state.prior.headerDigest && state.journalCount == state.prior.journalCount && state.fullSet != state.prior.fullSet
	default:
		return false
	}
}

func successorStageCarriesReceipts(state *successorAdmissionState) bool {
	return state != nil && state.prior != nil && state.runtimePublication == state.prior.runtimePublication && state.recoveryPublication == state.prior.recoveryPublication && state.runtimeReceipt.binding == state.prior.runtimeReceipt.binding && state.recoveryReceipt.binding == state.prior.recoveryReceipt.binding && state.runtimeReused == state.prior.runtimeReused && state.recoveryReused == state.prior.recoveryReused
}

func successorIndexFactsEqual(state, prior *successorAdmissionState) bool {
	return state != nil && prior != nil && state.indexPrefixDigest == prior.indexPrefixDigest && state.indexDigest == prior.indexDigest && state.framedDigest == prior.framedDigest && state.indexPrefixSize == prior.indexPrefixSize && state.indexSize == prior.indexSize && state.indexRecords == prior.indexRecords && state.indexTail == prior.indexTail && state.supersededDigest == prior.supersededDigest && state.reservedDigest == prior.reservedDigest
}

func validStoredSuccessorPublicationResult(state *successorAdmissionState, publication *evidencefs.Publication, digest Digest, size uint64, reused bool) bool {
	result := state.fsPublication
	return result.Outcome() == evidencefs.AdmissionTransitionDurable && result.Inventory() == state.inventory && result.Publication() == publication && result.CandidateKind() == "content_object" && result.CandidateSequence() == 0 && result.CandidateDigest() == digestRaw(digest) && result.CandidateRevision() == state.revision && result.PreviousRevision()+1 == state.revision && result.Size() == size && result.Reused() == reused
}

func emptyAdmissionPublicationResult(result evidencefs.AdmissionPublicationTransitionResult) bool {
	return result.Outcome() == "" && result.Inventory() == nil && result.Publication() == nil && result.CandidateDigest() == ([32]byte{}) && result.CandidateSequence() == 0 && result.CandidateRevision() == 0 && result.PreviousRevision() == 0 && result.Size() == 0 && !result.Reused()
}

func emptySuccessorIndexFacts(state *successorAdmissionState) bool {
	return state != nil && emptyAdmissionTransitionResult(state.fsIndex) && state.indexPrefixDigest == ([32]byte{}) && state.indexDigest == ([32]byte{}) && state.framedDigest == ([32]byte{}) && state.indexPrefixSize == 0 && state.indexSize == 0 && state.indexRecords == 0 && state.indexTail == "" && state.supersededDigest == "" && state.reservedDigest == ""
}

func emptyAdmissionTransitionResult(result evidencefs.AdmissionTransitionResult) bool {
	return result.Outcome() == "" && result.Inventory() == nil && result.CandidateKind() == "" && result.CandidateDigest() == ([32]byte{}) && result.CandidateSequence() == 0 && result.CandidateRevision() == 0 && result.PreviousRevision() == 0
}

func validStoredSuccessorIndexResult(state *successorAdmissionState, framed []byte, tail Digest) bool {
	if state == nil || len(framed) == 0 || tail.Validate() != nil || state.indexPrefixSize > ^uint64(0)-uint64(len(framed)) || state.fsIndex.Outcome() != evidencefs.AdmissionTransitionDurable || state.fsIndex.Inventory() != state.inventory || state.fsIndex.CandidateKind() != "target_index_append" || state.fsIndex.CandidateSequence() != 0 || state.fsIndex.CandidateDigest() != sha256.Sum256(framed) || state.fsIndex.CandidateRevision() != state.revision || state.fsIndex.PreviousRevision()+1 != state.revision || state.indexPrefixDigest == ([32]byte{}) || state.indexDigest == ([32]byte{}) || state.framedDigest != sha256.Sum256(framed) || state.indexPrefixSize == 0 || state.indexSize != state.indexPrefixSize+uint64(len(framed)) || state.indexTail != tail || state.supersededDigest != state.plan.supersededFrame.RecordDigest || state.reservedDigest != state.plan.reservedFrame.RecordDigest {
		return false
	}
	return true
}

func emptySuccessorHeaderFacts(state *successorAdmissionState) bool {
	return state != nil && state.fsJournal.Outcome() == "" && state.fsJournal.Inventory() == nil && state.fsJournal.CandidateDigest() == ([32]byte{}) && state.fsJournal.CandidateSequence() == 0 && state.fsJournal.CandidateRevision() == 0 && state.fsJournal.PreviousRevision() == 0 && state.fsJournal.Journal() == ([32]byte{}) && state.fsJournal.HeaderDigest() == ([32]byte{}) && state.fsJournal.HeaderSize() == 0 && state.journal == "" && state.headerDigest == "" && state.journalCount == 0 && state.activationHeader.generation.owner == nil && state.activationHeader.generation.executionLineageDigest == "" && state.activationHeader.generation.journalIdentityDigest == "" && state.activationHeader.generation.runnerProjectionDecisionDigest == "" && state.activationHeader.generation.schemaBundleDigest == "" && canonicalEqual(state.activationHeader.header, JournalHeader{}) && canonicalEqual(state.activationHeader.reserved, GenerationReserved{}) && canonicalEqual(state.headerFrame, EvidenceFrame{}) && len(state.headerBytes) == 0 && state.headerBytesHash == ([32]byte{}) && state.fsJournalCandidate == ([32]byte{})
}

func emptySuccessorActivationFacts(state *successorAdmissionState) bool {
	return state != nil && canonicalEqual(state.activatedFrame, LineageIndexFrame{}) && len(state.activatedBytes) == 0 && state.activationBytesHash == ([32]byte{}) && state.activationDigest == ""
}

func validSuccessorAdmissionState(owner any, state *successorAdmissionState, stage successorAdmissionStage, candidate OwnedCurrentCandidate) bool {
	if !validStoredSuccessorAdmissionState(state) || state.stage != stage || state.binding.owner != owner || state.consumed.Load() || !validOwnedCurrentCandidate(candidate) || state.candidateBinding != candidate.binding || state.runtimeDigest != candidate.runtimeArtifact.digest || state.runtimeSize != candidate.runtimeArtifact.sizeBytes || state.recoveryDigest != candidate.decisionRecoveryArtifact.digest || state.recoverySize != candidate.decisionRecoveryArtifact.sizeBytes || !validConsumedVerifiedSuccessorAdmissionPlan(state.plan, candidate) || !state.mutation.ValidFor(state.inventory) {
		return false
	}
	revision, revisionErr := state.inventory.Revision()
	target, targetErr := state.inventory.Target()
	fullSet, fullSetErr := state.inventory.FullSetDigest()
	if revisionErr != nil || targetErr != nil || fullSetErr != nil || revision != state.revision || target != state.target || fullSet != state.fullSet {
		return false
	}
	switch stage {
	case successorAdmissionPrepared:
		return true
	case successorAdmissionRuntimePublished:
		return state.fsPublication.ValidFor(state.inventory)
	case successorAdmissionRuntimeBound:
		return state.runtimePublication.Matches(digestRaw(state.runtimeDigest), state.runtimeSize)
	case successorAdmissionRecoveryPublished:
		return state.runtimePublication.Matches(digestRaw(state.runtimeDigest), state.runtimeSize) && state.fsPublication.ValidFor(state.inventory)
	case successorAdmissionRecoveryBound, successorAdmissionReserveReady:
		return state.runtimePublication.Matches(digestRaw(state.runtimeDigest), state.runtimeSize) && state.recoveryPublication.Matches(digestRaw(state.recoveryDigest), state.recoverySize) && state.runtimePublication.SameStore(state.recoveryPublication)
	case successorAdmissionReceiptBound:
		return validRuntimeReceipt(state.runtimeReceipt, candidate.owner, state.runtimeDigest, state.runtimeSize) && validDecisionRecoveryReceipt(state.recoveryReceipt, candidate.owner, state.recoveryDigest, state.recoverySize) && state.runtimeReceipt.publication == state.runtimePublication && state.recoveryReceipt.publication == state.recoveryPublication && state.runtimePublication.SameStore(state.recoveryPublication)
	case successorAdmissionAdjacentReady, successorAdmissionReservedDurable:
		return validRuntimeReceipt(state.runtimeReceipt, candidate.owner, state.runtimeDigest, state.runtimeSize) && validDecisionRecoveryReceipt(state.recoveryReceipt, candidate.owner, state.recoveryDigest, state.recoverySize) && state.runtimeReceipt.publication == state.runtimePublication && state.recoveryReceipt.publication == state.recoveryPublication && state.runtimePublication.SameStore(state.recoveryPublication) && validSuccessorInventoryIndex(state)
	case successorAdmissionHeaderDurable:
		return successorReceiptsExact(state, candidate) && state.fsJournal.ValidFor(state.inventory) && validSuccessorInventoryHeader(state)
	case successorAdmissionGenerationReady:
		return successorReceiptsExact(state, candidate) && validSuccessorInventoryHeader(state) && validSuccessorInventoryIndex(state)
	default:
		return false
	}
}

func successorAdmissionStateDigest(state *successorAdmissionState) [32]byte {
	if state == nil || state.self != state || state.plan == nil || state.plan.binding == nil || state.history == nil || state.history.binding == nil || state.candidateBinding == nil || state.runtimeDigest.Validate() != nil || state.recoveryDigest.Validate() != nil {
		return [32]byte{}
	}
	h := sha256.New()
	h.Write([]byte("cloud-agents-platform-successor-admission-state/v1\x00"))
	writeAdmissionString(h, string(state.stage))
	if state.prior == nil || state.prior.binding == nil {
		h.Write([]byte{0})
	} else {
		h.Write([]byte{1})
		h.Write(state.prior.binding.canonical[:])
	}
	h.Write(state.plan.binding.canonical[:])
	h.Write(state.history.binding.canonical[:])
	h.Write(state.candidateBinding.canonical[:])
	h.Write(state.target[:])
	h.Write(state.fullSet[:])
	writeAdmissionUint(h, state.revision)
	writeAdmissionString(h, state.runtimeDigest.String())
	writeAdmissionUint(h, state.runtimeSize)
	writeAdmissionString(h, state.recoveryDigest.String())
	writeAdmissionUint(h, state.recoverySize)
	writeAdmissionBool(h, state.runtimeReused)
	writeAdmissionBool(h, state.recoveryReused)
	writeAdmissionBool(h, state.runtimePublication != nil)
	writeAdmissionBool(h, state.recoveryPublication != nil)
	writeAdmissionString(h, string(state.fsPublication.Outcome()))
	publicationDigest := state.fsPublication.CandidateDigest()
	h.Write(publicationDigest[:])
	writeAdmissionUint(h, state.fsPublication.CandidateSequence())
	writeAdmissionUint(h, state.fsPublication.CandidateRevision())
	writeAdmissionUint(h, state.fsPublication.PreviousRevision())
	writeAdmissionUint(h, state.fsPublication.Size())
	writeAdmissionBool(h, state.fsPublication.Reused())
	writeAdmissionString(h, string(state.fsIndex.Outcome()))
	writeAdmissionString(h, state.fsIndex.CandidateKind())
	indexCandidateDigest := state.fsIndex.CandidateDigest()
	h.Write(indexCandidateDigest[:])
	writeAdmissionUint(h, state.fsIndex.CandidateSequence())
	writeAdmissionUint(h, state.fsIndex.CandidateRevision())
	writeAdmissionUint(h, state.fsIndex.PreviousRevision())
	writeAdmissionString(h, string(state.fsJournal.Outcome()))
	fsJournalCandidate := state.fsJournal.CandidateDigest()
	h.Write(fsJournalCandidate[:])
	writeAdmissionUint(h, state.fsJournal.CandidateSequence())
	writeAdmissionUint(h, state.fsJournal.CandidateRevision())
	writeAdmissionUint(h, state.fsJournal.PreviousRevision())
	fsJournalID := state.fsJournal.Journal()
	fsHeaderDigest := state.fsJournal.HeaderDigest()
	h.Write(fsJournalID[:])
	h.Write(fsHeaderDigest[:])
	writeAdmissionUint(h, state.fsJournal.HeaderSize())
	if state.runtimeReceipt.binding == nil {
		h.Write([]byte{0})
	} else {
		h.Write([]byte{1})
		writeAdmissionString(h, state.runtimeReceipt.digest.String())
		writeAdmissionUint(h, state.runtimeReceipt.sizeBytes)
	}
	if state.recoveryReceipt.binding == nil {
		h.Write([]byte{0})
	} else {
		h.Write([]byte{1})
		writeAdmissionString(h, state.recoveryReceipt.digest.String())
		writeAdmissionUint(h, state.recoveryReceipt.sizeBytes)
	}
	h.Write(state.indexPrefixDigest[:])
	h.Write(state.indexDigest[:])
	h.Write(state.framedDigest[:])
	writeAdmissionUint(h, state.indexPrefixSize)
	writeAdmissionUint(h, state.indexSize)
	writeAdmissionUint(h, state.indexRecords)
	writeAdmissionString(h, state.indexTail.String())
	writeAdmissionString(h, state.supersededDigest.String())
	writeAdmissionString(h, state.reservedDigest.String())
	writeAdmissionString(h, state.journal.String())
	writeAdmissionString(h, state.headerDigest.String())
	writeAdmissionUint(h, state.journalCount)
	writeAdmissionBool(h, state.activationHeader.generation.owner != nil)
	if state.activationHeader.generation.owner != nil {
		for _, value := range []Digest{state.activationHeader.generation.executionLineageDigest, state.activationHeader.generation.journalIdentityDigest, state.activationHeader.generation.runnerProjectionDecisionDigest, state.activationHeader.generation.schemaBundleDigest} {
			writeAdmissionString(h, value.String())
		}
		headerCanonical, headerErr := canonicalContractKey(state.activationHeader.header)
		reservedCanonical, reservedErr := canonicalContractKey(state.activationHeader.reserved)
		frameCanonical, frameErr := canonicalContractKey(state.headerFrame)
		if headerErr != nil || reservedErr != nil || frameErr != nil {
			return [32]byte{}
		}
		writeAdmissionString(h, headerCanonical)
		writeAdmissionString(h, reservedCanonical)
		writeAdmissionString(h, frameCanonical)
	}
	writeAdmissionString(h, string(state.headerBytes))
	h.Write(state.headerBytesHash[:])
	h.Write(state.fsJournalCandidate[:])
	writeAdmissionBool(h, len(state.activatedBytes) != 0)
	if len(state.activatedBytes) != 0 {
		activatedCanonical, err := canonicalContractKey(state.activatedFrame)
		if err != nil {
			return [32]byte{}
		}
		writeAdmissionString(h, activatedCanonical)
		writeAdmissionString(h, string(state.activatedBytes))
	}
	h.Write(state.activationBytesHash[:])
	writeAdmissionString(h, state.activationDigest.String())
	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result
}

func writeAdmissionBool(h interface{ Write([]byte) (int, error) }, value bool) {
	if value {
		_, _ = h.Write([]byte{1})
	} else {
		_, _ = h.Write([]byte{0})
	}
}

type successorAdmissionStep struct {
	inventory   *evidencefs.AdmissionInventory
	mutation    *evidencefs.AdmissionMutationToken
	target      [32]byte
	fullSet     [32]byte
	revision    uint64
	publication *evidencefs.Publication
	reused      bool
}

func nextSuccessorAdmissionState(prior *successorAdmissionState, stage successorAdmissionStage, step successorAdmissionStep) *successorAdmissionState {
	return &successorAdmissionState{
		stage: stage, prior: prior, plan: prior.plan, history: prior.history, candidateBinding: prior.candidateBinding,
		inventory: step.inventory, mutation: step.mutation, target: step.target, fullSet: step.fullSet, revision: step.revision,
		runtimePublication: prior.runtimePublication, recoveryPublication: prior.recoveryPublication,
		runtimeDigest: prior.runtimeDigest, runtimeSize: prior.runtimeSize, recoveryDigest: prior.recoveryDigest, recoverySize: prior.recoverySize,
		runtimeReused: prior.runtimeReused, recoveryReused: prior.recoveryReused,
		runtimeReceipt: prior.runtimeReceipt, recoveryReceipt: prior.recoveryReceipt,
		indexPrefixDigest: prior.indexPrefixDigest, indexDigest: prior.indexDigest, framedDigest: prior.framedDigest,
		indexPrefixSize: prior.indexPrefixSize, indexSize: prior.indexSize, indexRecords: prior.indexRecords,
		indexTail: prior.indexTail, supersededDigest: prior.supersededDigest, reservedDigest: prior.reservedDigest,
		fsJournal: prior.fsJournal, journal: prior.journal, headerDigest: prior.headerDigest, journalCount: prior.journalCount,
		activationHeader: cloneSuccessorActivationHeader(prior.activationHeader), headerFrame: cloneProjectionValue(prior.headerFrame),
		headerBytes: append([]byte(nil), prior.headerBytes...), headerBytesHash: prior.headerBytesHash, fsJournalCandidate: prior.fsJournalCandidate,
		activatedFrame: cloneProjectionValue(prior.activatedFrame), activatedBytes: append([]byte(nil), prior.activatedBytes...),
		activationBytesHash: prior.activationBytesHash, activationDigest: prior.activationDigest,
		consumed: &atomic.Bool{},
	}
}

func runSuccessorPublication(ctx context.Context, state *successorAdmissionState, digest Digest, size uint64, source []byte, op string) (successorAdmissionStep, evidencefs.AdmissionPublicationTransitionResult, bool, error) {
	fsResult, transitionErr := state.mutation.PublishObject(ctx, state.inventory, digestRaw(digest), source)
	if fsResult.Outcome() != evidencefs.AdmissionTransitionDurable {
		return successorAdmissionStep{}, fsResult, false, mapAdmissionMutationError(transitionErr, op)
	}
	postFailure := func(suffix string) (successorAdmissionStep, evidencefs.AdmissionPublicationTransitionResult, bool, error) {
		_ = fsResult.Invalidate()
		return successorAdmissionStep{}, fsResult, true, admissionPostMutationFailure(op + suffix)
	}
	if transitionErr != nil || fsResult.CandidateKind() != "content_object" || fsResult.CandidateSequence() != 0 || fsResult.CandidateDigest() != digestRaw(digest) || fsResult.Size() != size || fsResult.PreviousRevision() != state.revision || fsResult.CandidateRevision() != state.revision+1 || !fsResult.ValidFor(fsResult.Inventory()) {
		return postFailure("")
	}
	nextInventory := fsResult.Inventory()
	if err := nextInventory.Revalidate(ctx); err != nil {
		return postFailure("-revalidate")
	}
	revision, revisionErr := nextInventory.Revision()
	target, targetErr := nextInventory.Target()
	fullSet, fullSetErr := nextInventory.FullSetDigest()
	if revisionErr != nil || revision != state.revision+1 {
		return postFailure("-revision")
	}
	if targetErr != nil || target != state.target {
		return postFailure("-target")
	}
	if fullSetErr != nil || fullSet == ([32]byte{}) || (fullSet == state.fullSet) != fsResult.Reused() || !successorExistingBoundPublicationsExact(state) {
		return postFailure("-full-set")
	}
	nextToken, tokenErr := nextInventory.MutationToken()
	if tokenErr != nil || !nextToken.ValidFor(nextInventory) {
		return postFailure("-token")
	}
	return successorAdmissionStep{inventory: nextInventory, mutation: nextToken, target: target, fullSet: fullSet, revision: revision, publication: fsResult.Publication(), reused: fsResult.Reused()}, fsResult, false, nil
}

func runSuccessorBinding(ctx context.Context, state *successorAdmissionState, publication *evidencefs.Publication, digest Digest, size uint64, op string) (successorAdmissionStep, evidencefs.AdmissionBindingTransitionResult, bool, error) {
	fsResult, transitionErr := state.mutation.BindPublishedObject(ctx, state.inventory, publication, digestRaw(digest), size)
	if fsResult.Outcome() != evidencefs.AdmissionTransitionDurable {
		return successorAdmissionStep{}, fsResult, false, mapAdmissionMutationError(transitionErr, op)
	}
	postFailure := func(suffix string) (successorAdmissionStep, evidencefs.AdmissionBindingTransitionResult, bool, error) {
		_ = fsResult.Invalidate()
		return successorAdmissionStep{}, fsResult, true, admissionPostMutationFailure(op + suffix)
	}
	if transitionErr != nil || fsResult.CandidateKind() != "content_binding" || fsResult.CandidateSequence() != 0 || fsResult.CandidateDigest() != digestRaw(digest) || fsResult.Size() != size || fsResult.PreviousRevision() != state.revision || fsResult.CandidateRevision() != state.revision+1 || fsResult.Publication() != publication || !fsResult.ValidFor(fsResult.Inventory()) {
		return postFailure("")
	}
	nextInventory := fsResult.Inventory()
	if err := nextInventory.Revalidate(ctx); err != nil {
		return postFailure("-revalidate")
	}
	revision, revisionErr := nextInventory.Revision()
	target, targetErr := nextInventory.Target()
	fullSet, fullSetErr := nextInventory.FullSetDigest()
	if revisionErr != nil || revision != state.revision+1 {
		return postFailure("-revision")
	}
	if targetErr != nil || target != state.target {
		return postFailure("-target")
	}
	if fullSetErr != nil || fullSet != state.fullSet || !publication.Matches(digestRaw(digest), size) || !successorExistingBoundPublicationsExact(state) {
		return postFailure("-full-set")
	}
	nextToken, tokenErr := nextInventory.MutationToken()
	if tokenErr != nil || !nextToken.ValidFor(nextInventory) {
		return postFailure("-token")
	}
	return successorAdmissionStep{inventory: nextInventory, mutation: nextToken, target: target, fullSet: fullSet, revision: revision, publication: publication}, fsResult, false, nil
}

func runSuccessorAdvance(ctx context.Context, state *successorAdmissionState, op string) (successorAdmissionStep, evidencefs.AdmissionTransitionResult, bool, error) {
	fsResult, transitionErr := state.mutation.Advance(ctx, state.inventory)
	if fsResult.Outcome() != evidencefs.AdmissionTransitionDurable {
		return successorAdmissionStep{}, fsResult, false, mapAdmissionMutationError(transitionErr, op)
	}
	postFailure := func(suffix string) (successorAdmissionStep, evidencefs.AdmissionTransitionResult, bool, error) {
		_ = fsResult.Invalidate()
		return successorAdmissionStep{}, fsResult, true, admissionPostMutationFailure(op + suffix)
	}
	if transitionErr != nil || fsResult.CandidateKind() != "inventory_advance" || fsResult.CandidateSequence() != 0 || fsResult.CandidateDigest() != ([32]byte{}) || fsResult.PreviousRevision() != state.revision || fsResult.CandidateRevision() != state.revision+1 || fsResult.Inventory() == nil {
		return postFailure("")
	}
	nextInventory := fsResult.Inventory()
	if err := nextInventory.Revalidate(ctx); err != nil {
		return postFailure("-revalidate")
	}
	revision, revisionErr := nextInventory.Revision()
	target, targetErr := nextInventory.Target()
	fullSet, fullSetErr := nextInventory.FullSetDigest()
	if revisionErr != nil || revision != state.revision+1 {
		return postFailure("-revision")
	}
	if targetErr != nil || target != state.target {
		return postFailure("-target")
	}
	if fullSetErr != nil || fullSet != state.fullSet || !successorExistingBoundPublicationsExact(state) {
		return postFailure("-full-set")
	}
	nextToken, tokenErr := nextInventory.MutationToken()
	if tokenErr != nil || !nextToken.ValidFor(nextInventory) {
		return postFailure("-token")
	}
	return successorAdmissionStep{inventory: nextInventory, mutation: nextToken, target: target, fullSet: fullSet, revision: revision}, fsResult, false, nil
}

func successorExistingBoundPublicationsExact(state *successorAdmissionState) bool {
	if state == nil {
		return false
	}
	switch state.stage {
	case successorAdmissionPrepared, successorAdmissionRuntimePublished:
		return true
	case successorAdmissionRuntimeBound, successorAdmissionRecoveryPublished:
		return state.runtimePublication != nil && state.runtimePublication.Matches(digestRaw(state.runtimeDigest), state.runtimeSize)
	case successorAdmissionRecoveryBound, successorAdmissionReserveReady, successorAdmissionReceiptBound, successorAdmissionAdjacentReady, successorAdmissionReservedDurable, successorAdmissionHeaderDurable, successorAdmissionGenerationReady:
		return state.runtimePublication != nil && state.recoveryPublication != nil && state.runtimePublication.Matches(digestRaw(state.runtimeDigest), state.runtimeSize) && state.recoveryPublication.Matches(digestRaw(state.recoveryDigest), state.recoverySize) && state.runtimePublication.SameStore(state.recoveryPublication)
	default:
		return false
	}
}

func successorUnknownResult[T any](value SuccessorAdmissionTransitionResult[T]) SuccessorAdmissionTransitionResult[T] {
	var zero T
	value.outcome = evidencefs.AdmissionTransitionUnknown
	value.next = zero
	return value
}

func successorPublicationResult[T any](kind string, sequence uint64, fsResult evidencefs.AdmissionPublicationTransitionResult) SuccessorAdmissionTransitionResult[T] {
	return SuccessorAdmissionTransitionResult[T]{
		outcome: fsResult.Outcome(), candidateKind: kind, candidateDigest: fsResult.CandidateDigest(), candidateSequence: sequence,
		candidateRevision: fsResult.CandidateRevision(), previousRevision: fsResult.PreviousRevision(),
	}
}

func successorBindingResult[T any](kind string, sequence uint64, fsResult evidencefs.AdmissionBindingTransitionResult) SuccessorAdmissionTransitionResult[T] {
	return SuccessorAdmissionTransitionResult[T]{
		outcome: fsResult.Outcome(), candidateKind: kind, candidateDigest: fsResult.CandidateDigest(), candidateSequence: sequence,
		candidateRevision: fsResult.CandidateRevision(), previousRevision: fsResult.PreviousRevision(),
	}
}

func (p *SuccessorAdmissionPermit) PublishRuntime(ctx context.Context, candidate OwnedCurrentCandidate) (SuccessorAdmissionTransitionResult[*SuccessorRuntimePublishedPermit], error) {
	pre := SuccessorAdmissionTransitionResult[*SuccessorRuntimePublishedPermit]{outcome: evidencefs.AdmissionTransitionPreMutationFailure, candidateKind: "runtime_object", candidateDigest: digestRaw(candidate.runtimeArtifact.digest), candidateSequence: 1}
	if p == nil || p.self != p || p.state == nil || !validSuccessorAdmissionState(p, p.state, successorAdmissionPrepared, candidate) {
		return pre, fail(CodeEvidenceRecoveryRequired, "successor-runtime-publish", "successor admission permit is unavailable", nil)
	}
	pre.previousRevision = p.state.revision
	pre.candidateRevision = p.state.revision + 1
	if !p.state.consumed.CompareAndSwap(false, true) {
		return pre, fail(CodeEvidenceRecoveryRequired, "successor-runtime-publish", "successor admission permit was already consumed", nil)
	}
	step, fsResult, postUnknown, err := runSuccessorPublication(ctx, p.state, p.state.runtimeDigest, p.state.runtimeSize, candidate.runtimeArtifact.bytes, "successor-runtime-publish")
	result := successorPublicationResult[*SuccessorRuntimePublishedPermit]("runtime_object", 1, fsResult)
	if err != nil {
		if fsResult.Outcome() == evidencefs.AdmissionTransitionPreMutationFailure {
			p.state.consumed.CompareAndSwap(true, false)
		}
		if postUnknown {
			result = successorUnknownResult(result)
		}
		return result, err
	}
	nextState := nextSuccessorAdmissionState(p.state, successorAdmissionRuntimePublished, step)
	nextState.runtimePublication = step.publication
	nextState.fsPublication = fsResult
	nextState.runtimeReused = step.reused
	next := &SuccessorRuntimePublishedPermit{state: nextState}
	next.self = next
	if !sealSuccessorAdmissionState(next, nextState) || !validSuccessorAdmissionState(next, nextState, successorAdmissionRuntimePublished, candidate) {
		_ = fsResult.Invalidate()
		if nextState.binding != nil {
			successorAdmissionStateRegistry.Delete(nextState.binding)
		}
		return successorUnknownResult(result), admissionPostMutationFailure("successor-runtime-publish-seal")
	}
	result.next = next
	return result, nil
}

func (p *SuccessorRuntimePublishedPermit) BindRuntime(ctx context.Context, candidate OwnedCurrentCandidate) (SuccessorAdmissionTransitionResult[*SuccessorRuntimeBoundPermit], error) {
	pre := SuccessorAdmissionTransitionResult[*SuccessorRuntimeBoundPermit]{outcome: evidencefs.AdmissionTransitionPreMutationFailure, candidateKind: "runtime_binding", candidateDigest: digestRaw(candidate.runtimeArtifact.digest), candidateSequence: 2}
	if p == nil || p.self != p || p.state == nil || !validSuccessorAdmissionState(p, p.state, successorAdmissionRuntimePublished, candidate) {
		return pre, fail(CodeEvidenceRecoveryRequired, "successor-runtime-bind", "successor runtime publication is unavailable", nil)
	}
	pre.previousRevision = p.state.revision
	pre.candidateRevision = p.state.revision + 1
	if !p.state.consumed.CompareAndSwap(false, true) {
		return pre, fail(CodeEvidenceRecoveryRequired, "successor-runtime-bind", "successor runtime publication was already consumed", nil)
	}
	step, fsResult, postUnknown, err := runSuccessorBinding(ctx, p.state, p.state.runtimePublication, p.state.runtimeDigest, p.state.runtimeSize, "successor-runtime-bind")
	result := successorBindingResult[*SuccessorRuntimeBoundPermit]("runtime_binding", 2, fsResult)
	if err != nil {
		if fsResult.Outcome() == evidencefs.AdmissionTransitionPreMutationFailure {
			p.state.consumed.CompareAndSwap(true, false)
		}
		if postUnknown {
			result = successorUnknownResult(result)
		}
		return result, err
	}
	nextState := nextSuccessorAdmissionState(p.state, successorAdmissionRuntimeBound, step)
	nextState.runtimePublication = p.state.runtimePublication
	next := &SuccessorRuntimeBoundPermit{state: nextState}
	next.self = next
	if !sealSuccessorAdmissionState(next, nextState) || !validSuccessorAdmissionState(next, nextState, successorAdmissionRuntimeBound, candidate) {
		_ = fsResult.Invalidate()
		if nextState.binding != nil {
			successorAdmissionStateRegistry.Delete(nextState.binding)
		}
		return successorUnknownResult(result), admissionPostMutationFailure("successor-runtime-bind-seal")
	}
	result.next = next
	return result, nil
}

func (p *SuccessorRuntimeBoundPermit) PublishDecisionRecovery(ctx context.Context, candidate OwnedCurrentCandidate) (SuccessorAdmissionTransitionResult[*SuccessorRecoveryPublishedPermit], error) {
	pre := SuccessorAdmissionTransitionResult[*SuccessorRecoveryPublishedPermit]{outcome: evidencefs.AdmissionTransitionPreMutationFailure, candidateKind: "decision_recovery_object", candidateDigest: digestRaw(candidate.decisionRecoveryArtifact.digest), candidateSequence: 3}
	if p == nil || p.self != p || p.state == nil || !validSuccessorAdmissionState(p, p.state, successorAdmissionRuntimeBound, candidate) {
		return pre, fail(CodeEvidenceRecoveryRequired, "successor-recovery-publish", "successor runtime binding is unavailable", nil)
	}
	pre.previousRevision = p.state.revision
	pre.candidateRevision = p.state.revision + 1
	if !p.state.consumed.CompareAndSwap(false, true) {
		return pre, fail(CodeEvidenceRecoveryRequired, "successor-recovery-publish", "successor runtime binding was already consumed", nil)
	}
	step, fsResult, postUnknown, err := runSuccessorPublication(ctx, p.state, p.state.recoveryDigest, p.state.recoverySize, candidate.decisionRecoveryArtifact.bytes, "successor-recovery-publish")
	result := successorPublicationResult[*SuccessorRecoveryPublishedPermit]("decision_recovery_object", 3, fsResult)
	if err != nil {
		if fsResult.Outcome() == evidencefs.AdmissionTransitionPreMutationFailure {
			p.state.consumed.CompareAndSwap(true, false)
		}
		if postUnknown {
			result = successorUnknownResult(result)
		}
		return result, err
	}
	nextState := nextSuccessorAdmissionState(p.state, successorAdmissionRecoveryPublished, step)
	nextState.recoveryPublication = step.publication
	nextState.fsPublication = fsResult
	nextState.recoveryReused = step.reused
	next := &SuccessorRecoveryPublishedPermit{state: nextState}
	next.self = next
	if !sealSuccessorAdmissionState(next, nextState) || !validSuccessorAdmissionState(next, nextState, successorAdmissionRecoveryPublished, candidate) {
		_ = fsResult.Invalidate()
		if nextState.binding != nil {
			successorAdmissionStateRegistry.Delete(nextState.binding)
		}
		return successorUnknownResult(result), admissionPostMutationFailure("successor-recovery-publish-seal")
	}
	result.next = next
	return result, nil
}

func (p *SuccessorRecoveryPublishedPermit) BindDecisionRecovery(ctx context.Context, candidate OwnedCurrentCandidate) (SuccessorAdmissionTransitionResult[*SuccessorRecoveryBoundPermit], error) {
	pre := SuccessorAdmissionTransitionResult[*SuccessorRecoveryBoundPermit]{outcome: evidencefs.AdmissionTransitionPreMutationFailure, candidateKind: "decision_recovery_binding", candidateDigest: digestRaw(candidate.decisionRecoveryArtifact.digest), candidateSequence: 4}
	if p == nil || p.self != p || p.state == nil || !validSuccessorAdmissionState(p, p.state, successorAdmissionRecoveryPublished, candidate) {
		return pre, fail(CodeEvidenceRecoveryRequired, "successor-recovery-bind", "successor recovery publication is unavailable", nil)
	}
	pre.previousRevision = p.state.revision
	pre.candidateRevision = p.state.revision + 1
	if !p.state.consumed.CompareAndSwap(false, true) {
		return pre, fail(CodeEvidenceRecoveryRequired, "successor-recovery-bind", "successor recovery publication was already consumed", nil)
	}
	step, fsResult, postUnknown, err := runSuccessorBinding(ctx, p.state, p.state.recoveryPublication, p.state.recoveryDigest, p.state.recoverySize, "successor-recovery-bind")
	result := successorBindingResult[*SuccessorRecoveryBoundPermit]("decision_recovery_binding", 4, fsResult)
	if err != nil {
		if fsResult.Outcome() == evidencefs.AdmissionTransitionPreMutationFailure {
			p.state.consumed.CompareAndSwap(true, false)
		}
		if postUnknown {
			result = successorUnknownResult(result)
		}
		return result, err
	}
	nextState := nextSuccessorAdmissionState(p.state, successorAdmissionRecoveryBound, step)
	nextState.recoveryPublication = p.state.recoveryPublication
	next := &SuccessorRecoveryBoundPermit{state: nextState}
	next.self = next
	if !sealSuccessorAdmissionState(next, nextState) || !validSuccessorAdmissionState(next, nextState, successorAdmissionRecoveryBound, candidate) {
		_ = fsResult.Invalidate()
		if nextState.binding != nil {
			successorAdmissionStateRegistry.Delete(nextState.binding)
		}
		return successorUnknownResult(result), admissionPostMutationFailure("successor-recovery-bind-seal")
	}
	result.next = next
	return result, nil
}

func (p *SuccessorRecoveryBoundPermit) SealReserveReady(ctx context.Context, candidate OwnedCurrentCandidate) (SuccessorAdmissionTransitionResult[*SuccessorReserveReady], error) {
	pre := SuccessorAdmissionTransitionResult[*SuccessorReserveReady]{outcome: evidencefs.AdmissionTransitionPreMutationFailure, candidateKind: "reserve_ready", candidateSequence: 5}
	if p == nil || p.self != p || p.state == nil || !validSuccessorAdmissionState(p, p.state, successorAdmissionRecoveryBound, candidate) {
		return pre, fail(CodeEvidenceRecoveryRequired, "successor-reserve-ready", "successor recovery binding is unavailable", nil)
	}
	pre.previousRevision = p.state.revision
	pre.candidateRevision = p.state.revision + 1
	pre.candidateDigest = successorReserveReadyCandidateDigest(p.state)
	if pre.candidateDigest == ([32]byte{}) || !p.state.consumed.CompareAndSwap(false, true) {
		return pre, fail(CodeEvidenceRecoveryRequired, "successor-reserve-ready", "successor reserve-ready input is invalid or consumed", nil)
	}
	step, fsResult, postUnknown, err := runSuccessorAdvance(ctx, p.state, "successor-reserve-ready")
	result := SuccessorAdmissionTransitionResult[*SuccessorReserveReady]{
		outcome: fsResult.Outcome(), candidateKind: "reserve_ready", candidateDigest: pre.candidateDigest, candidateSequence: 5,
		candidateRevision: fsResult.CandidateRevision(), previousRevision: fsResult.PreviousRevision(),
	}
	if err != nil {
		if fsResult.Outcome() == evidencefs.AdmissionTransitionPreMutationFailure {
			p.state.consumed.CompareAndSwap(true, false)
		}
		if postUnknown {
			result = successorUnknownResult(result)
		}
		return result, err
	}
	nextState := nextSuccessorAdmissionState(p.state, successorAdmissionReserveReady, step)
	next := &SuccessorReserveReady{state: nextState}
	next.self = next
	if !sealSuccessorAdmissionState(next, nextState) || !validSuccessorAdmissionState(next, nextState, successorAdmissionReserveReady, candidate) {
		_ = fsResult.Invalidate()
		if nextState.binding != nil {
			successorAdmissionStateRegistry.Delete(nextState.binding)
		}
		return successorUnknownResult(result), admissionPostMutationFailure("successor-reserve-ready-seal")
	}
	result.next = next
	return result, nil
}

func successorReserveReadyCandidateDigest(state *successorAdmissionState) [32]byte {
	if state == nil || state.self != state || state.binding == nil || state.plan == nil || state.plan.binding == nil || state.stage != successorAdmissionRecoveryBound {
		return [32]byte{}
	}
	h := sha256.New()
	h.Write([]byte("cloud-agents-platform-successor-reserve-ready-candidate/v1\x00"))
	h.Write(state.binding.canonical[:])
	h.Write(state.plan.binding.canonical[:])
	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result
}

// BindReceiptPair atomically seals both purpose-typed receipts at N+5. It
// performs no filesystem mutation; the reserve-ready authority is restored if
// either receipt or the composite seal fails.
func (r *SuccessorReserveReady) BindReceiptPair(candidate OwnedCurrentCandidate) (*SuccessorReceiptBoundReady, error) {
	if r == nil || r.self != r || r.state == nil || !validSuccessorAdmissionState(r, r.state, successorAdmissionReserveReady, candidate) || !r.state.consumed.CompareAndSwap(false, true) {
		return nil, fail(CodeEvidenceRecoveryRequired, "successor-receipt-pair", "successor reserve-ready authority is unavailable or consumed", nil)
	}
	runtimeReceipt, runtimeBinding, runtimeErr := mintRuntimeContentReceipt(candidate.owner, candidate.runtimeArtifact, r.state.runtimePublication)
	recoveryReceipt, recoveryBinding, recoveryErr := mintDecisionRecoveryReceipt(candidate.owner, candidate.decisionRecoveryArtifact, r.state.recoveryPublication)
	if runtimeErr != nil || recoveryErr != nil || runtimeBinding == nil || recoveryBinding == nil || !r.state.runtimePublication.SameStore(r.state.recoveryPublication) {
		r.state.consumed.CompareAndSwap(true, false)
		return nil, fail(CodeEvidenceRecoveryRequired, "successor-receipt-pair", "successor typed receipt pair cannot be bound", nil)
	}
	step := successorAdmissionStep{inventory: r.state.inventory, mutation: r.state.mutation, target: r.state.target, fullSet: r.state.fullSet, revision: r.state.revision}
	nextState := nextSuccessorAdmissionState(r.state, successorAdmissionReceiptBound, step)
	nextState.runtimeReceipt = runtimeReceipt
	nextState.recoveryReceipt = recoveryReceipt
	next := &SuccessorReceiptBoundReady{state: nextState}
	next.self = next
	verifiedContentReceiptRegistry.Store(runtimeBinding, runtimeBinding)
	verifiedDecisionRecoveryReceiptRegistry.Store(recoveryBinding, recoveryBinding)
	sealed := sealSuccessorAdmissionState(next, nextState)
	if !sealed || !validSuccessorAdmissionState(next, nextState, successorAdmissionReceiptBound, candidate) {
		verifiedContentReceiptRegistry.Delete(runtimeBinding)
		verifiedDecisionRecoveryReceiptRegistry.Delete(recoveryBinding)
		if nextState.binding != nil {
			successorAdmissionStateRegistry.Delete(nextState.binding)
		}
		r.state.consumed.CompareAndSwap(true, false)
		return nil, fail(CodeEvidenceRecoveryRequired, "successor-receipt-pair", "successor typed receipt pair could not be sealed", nil)
	}
	return next, nil
}
