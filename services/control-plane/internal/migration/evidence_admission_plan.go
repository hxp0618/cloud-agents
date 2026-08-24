package migration

import (
	"context"
	"crypto/sha256"
	"errors"
	"sync"
	"sync/atomic"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/evidencefs"
)

// VerifiedAdmissionPlan is migration-owned authority for the brand-new
// reservation path. It is still insufficient for filesystem mutation: the
// later composite binder must consume the matching evidencefs mutation token.
type VerifiedAdmissionPlan struct {
	history            *VerifiedAdmissionHistory
	candidateBinding   *verifiedEvidenceRunBinding
	lineageHeaderFrame LineageIndexFrame
	reservedFrame      LineageIndexFrame
	lineageHeaderBytes []byte
	reservedFrameBytes []byte
	binding            *verifiedAdmissionPlanBinding
	consumed           *atomic.Bool
}

type verifiedAdmissionPlanBinding struct {
	plan      *VerifiedAdmissionPlan
	history   *VerifiedAdmissionHistory
	candidate *verifiedEvidenceRunBinding
	canonical [32]byte
}

var verifiedAdmissionPlanRegistry sync.Map

// AdmissionPermit cross-binds migration verification with evidencefs-owned
// mutation authority. It performs no mutation and exposes no raw token.
type AdmissionPermit struct {
	self             *AdmissionPermit
	history          *VerifiedAdmissionHistory
	plan             *VerifiedAdmissionPlan
	candidateBinding *verifiedEvidenceRunBinding
	inventory        *evidencefs.AdmissionInventory
	mutation         *evidencefs.AdmissionMutationToken
	binding          *admissionPermitBinding
	consumed         *atomic.Bool
}

type admissionPermitBinding struct {
	permit    *AdmissionPermit
	history   *VerifiedAdmissionHistory
	plan      *VerifiedAdmissionPlan
	inventory *evidencefs.AdmissionInventory
	mutation  *evidencefs.AdmissionMutationToken
	canonical [32]byte
}

var admissionPermitRegistry sync.Map

// AdmissionPermitTransitionResult is the closed migration-side projection of
// one evidencefs mutation result. Only durable carries next authority.
type AdmissionPermitTransitionResult struct {
	outcome           evidencefs.AdmissionTransitionOutcome
	next              *RegisteredAdmissionPermit
	candidateKind     string
	candidateDigest   [32]byte
	candidateSequence uint64
	candidateRevision uint64
	previousRevision  uint64
}

func (r AdmissionPermitTransitionResult) Outcome() evidencefs.AdmissionTransitionOutcome {
	return r.outcome
}
func (r AdmissionPermitTransitionResult) Next() *RegisteredAdmissionPermit { return r.next }
func (r AdmissionPermitTransitionResult) CandidateKind() string            { return r.candidateKind }
func (r AdmissionPermitTransitionResult) CandidateDigest() [32]byte        { return r.candidateDigest }
func (r AdmissionPermitTransitionResult) CandidateSequence() uint64        { return r.candidateSequence }
func (r AdmissionPermitTransitionResult) CandidateRevision() uint64        { return r.candidateRevision }
func (r AdmissionPermitTransitionResult) PreviousRevision() uint64         { return r.previousRevision }

// RegisteredAdmissionPermit is the revision+1 authority returned only after
// durable target registration and exact migration/evidencefs cross-binding.
// It retains frozen verification facts from the consumed prior chain while
// all future filesystem mutation must consume its new evidencefs token.
type RegisteredAdmissionPermit struct {
	self             *RegisteredAdmissionPermit
	prior            *AdmissionPermit
	plan             *VerifiedAdmissionPlan
	candidateBinding *verifiedEvidenceRunBinding
	inventory        *evidencefs.AdmissionInventory
	mutation         *evidencefs.AdmissionMutationToken
	target, fullSet  [32]byte
	revision         uint64
	indexDigest      [32]byte
	reused           bool
	binding          *registeredAdmissionPermitBinding
	consumed         *atomic.Bool
}

type registeredAdmissionPermitBinding struct {
	permit    *RegisteredAdmissionPermit
	prior     *AdmissionPermit
	plan      *VerifiedAdmissionPlan
	inventory *evidencefs.AdmissionInventory
	mutation  *evidencefs.AdmissionMutationToken
	canonical [32]byte
}

var registeredAdmissionPermitRegistry sync.Map

type RuntimePublicationTransitionResult struct {
	outcome           evidencefs.AdmissionTransitionOutcome
	next              *RuntimePublishedPermit
	candidateDigest   [32]byte
	candidateSequence uint64
	candidateRevision uint64
	previousRevision  uint64
}

func (r RuntimePublicationTransitionResult) Outcome() evidencefs.AdmissionTransitionOutcome {
	return r.outcome
}
func (r RuntimePublicationTransitionResult) Next() *RuntimePublishedPermit { return r.next }
func (r RuntimePublicationTransitionResult) CandidateKind() string         { return "runtime_object" }
func (r RuntimePublicationTransitionResult) CandidateDigest() [32]byte     { return r.candidateDigest }
func (r RuntimePublicationTransitionResult) CandidateSequence() uint64     { return r.candidateSequence }
func (r RuntimePublicationTransitionResult) CandidateRevision() uint64     { return r.candidateRevision }
func (r RuntimePublicationTransitionResult) PreviousRevision() uint64      { return r.previousRevision }

// RuntimePublishedPermit is the only post-registration state. Its Publication
// remains transient and must be consumed by the immediate runtime bind step.
type RuntimePublishedPermit struct {
	self             *RuntimePublishedPermit
	prior            *RegisteredAdmissionPermit
	plan             *VerifiedAdmissionPlan
	candidateBinding *verifiedEvidenceRunBinding
	inventory        *evidencefs.AdmissionInventory
	mutation         *evidencefs.AdmissionMutationToken
	publication      *evidencefs.Publication
	fsResult         evidencefs.AdmissionPublicationTransitionResult
	target, fullSet  [32]byte
	revision         uint64
	digest           Digest
	size             uint64
	reused           bool
	binding          *runtimePublishedPermitBinding
	consumed         *atomic.Bool
}

type runtimePublishedPermitBinding struct {
	permit      *RuntimePublishedPermit
	prior       *RegisteredAdmissionPermit
	plan        *VerifiedAdmissionPlan
	inventory   *evidencefs.AdmissionInventory
	mutation    *evidencefs.AdmissionMutationToken
	publication *evidencefs.Publication
	canonical   [32]byte
}

var runtimePublishedPermitRegistry sync.Map

type RuntimeBindingTransitionResult struct {
	outcome           evidencefs.AdmissionTransitionOutcome
	next              *RuntimeBoundPermit
	candidateDigest   [32]byte
	candidateSequence uint64
	candidateRevision uint64
	previousRevision  uint64
}

func (r RuntimeBindingTransitionResult) Outcome() evidencefs.AdmissionTransitionOutcome {
	return r.outcome
}
func (r RuntimeBindingTransitionResult) Next() *RuntimeBoundPermit { return r.next }
func (r RuntimeBindingTransitionResult) CandidateKind() string     { return "runtime_binding" }
func (r RuntimeBindingTransitionResult) CandidateDigest() [32]byte { return r.candidateDigest }
func (r RuntimeBindingTransitionResult) CandidateSequence() uint64 { return r.candidateSequence }
func (r RuntimeBindingTransitionResult) CandidateRevision() uint64 { return r.candidateRevision }
func (r RuntimeBindingTransitionResult) PreviousRevision() uint64  { return r.previousRevision }

type RuntimeBoundPermit struct {
	self             *RuntimeBoundPermit
	prior            *RuntimePublishedPermit
	plan             *VerifiedAdmissionPlan
	candidateBinding *verifiedEvidenceRunBinding
	inventory        *evidencefs.AdmissionInventory
	mutation         *evidencefs.AdmissionMutationToken
	publication      *evidencefs.Publication
	target, fullSet  [32]byte
	revision         uint64
	digest           Digest
	size             uint64
	binding          *runtimeBoundPermitBinding
	consumed         *atomic.Bool
}

type runtimeBoundPermitBinding struct {
	permit      *RuntimeBoundPermit
	prior       *RuntimePublishedPermit
	plan        *VerifiedAdmissionPlan
	inventory   *evidencefs.AdmissionInventory
	mutation    *evidencefs.AdmissionMutationToken
	publication *evidencefs.Publication
	canonical   [32]byte
}

var runtimeBoundPermitRegistry sync.Map

func bindVerifiedAdmissionPlan(ctx context.Context, history *VerifiedAdmissionHistory, candidate OwnedCurrentCandidate) (*VerifiedAdmissionPlan, error) {
	if !validVerifiedAdmissionHistory(history, candidate) {
		return nil, fail(CodeEvidenceRecoveryRequired, "admission-plan", "verified admission history is unavailable", nil)
	}
	if err := history.inventory.Revalidate(ctx); err != nil {
		return nil, mapEvidenceAdmissionError(err, "admission-plan-revalidate")
	}
	if !validVerifiedAdmissionHistory(history, candidate) {
		return nil, admissionFailed("admission-plan", "admission history changed before planning", nil)
	}
	lineageHeader, reserved, lineageBytes, reservedBytes, err := buildBrandNewAdmissionFrames(history, candidate)
	if err != nil {
		return nil, err
	}
	plan := &VerifiedAdmissionPlan{
		history: history, candidateBinding: candidate.binding, lineageHeaderFrame: lineageHeader, reservedFrame: reserved,
		lineageHeaderBytes: append([]byte(nil), lineageBytes...), reservedFrameBytes: append([]byte(nil), reservedBytes...), consumed: &atomic.Bool{},
	}
	binding := &verifiedAdmissionPlanBinding{plan: plan, history: history, candidate: candidate.binding}
	plan.binding = binding
	binding.canonical = admissionPlanDigest(plan)
	verifiedAdmissionPlanRegistry.Store(binding, binding.canonical)
	return plan, nil
}

func buildBrandNewAdmissionFrames(history *VerifiedAdmissionHistory, candidate OwnedCurrentCandidate) (LineageIndexFrame, LineageIndexFrame, []byte, []byte, error) {
	if history == nil || !validOwnedCurrentCandidate(candidate) || history.candidateBinding != candidate.binding || history.target != digestRaw(candidate.verifiedRun.executionLineageDigest) || history.reservation.ReservedRecords == 0 || history.reservation.ReservedJournalBytes == 0 || history.reservation.ReservedSegments == 0 || history.reservation.ReservedBytes != history.reservation.ReservedJournalBytes+history.reservation.ReservedIndexBytes {
		return LineageIndexFrame{}, LineageIndexFrame{}, nil, nil, fail(CodeEvidenceRecoveryRequired, "admission-plan", "brand-new admission facts are unavailable", nil)
	}
	profile := quotaReservationProfile(history.reservation)
	if history.quotaProfile != "" {
		if !validEvidenceLimitsProfile(history.quotaProfile) || history.quotaProfile != profile {
			return LineageIndexFrame{}, LineageIndexFrame{}, nil, nil, fail(CodeEvidenceRecoveryRequired, "admission-plan", "brand-new admission profile is inconsistent", nil)
		}
		profile = history.quotaProfile
	}
	if history.currentFacts != nil && history.currentFacts.lineageQuotaProfile != profile {
		return LineageIndexFrame{}, LineageIndexFrame{}, nil, nil, fail(CodeEvidenceRecoveryRequired, "admission-plan", "brand-new admission facts use a different quota profile", nil)
	}
	bindings, err := candidate.verifiedRun.currentDecision.decision.runnerProjectionBindings()
	if err != nil || !validOwnedCurrentDecision(candidate.verifiedRun.currentDecision, bindings) {
		return LineageIndexFrame{}, LineageIndexFrame{}, nil, nil, fail(CodeEvidenceRecoveryRequired, "admission-plan", "current verifier binding is unavailable", nil)
	}
	lineageHeader := LineageIndexHeader{
		FormatVersion: LineageIndexFormat, ExecutionLineageDigest: bindings.executionLineageDigest, DeploymentID: bindings.deploymentID,
		ExpectedDatabaseIdentity: LineageExpectedDatabaseIdentity{DatabaseName: bindings.expectedDatabaseName}, RepositoryIdentity: bindings.releaseSubject.RepositoryIdentity, LimitsProfile: LineageLimitsProfile,
	}
	wantLineage, err := ExecutionLineageDigest(lineageHeader)
	if err != nil || wantLineage != bindings.executionLineageDigest || lineageHeader.Validate() != nil {
		return LineageIndexFrame{}, LineageIndexFrame{}, nil, nil, fail(CodeEvidenceRecoveryRequired, "admission-plan", "current execution lineage binding is invalid", err)
	}
	lineageFrame := LineageIndexFrame{FormatVersion: LineageFrameFormat, Sequence: 0, RecordKind: LineageRecordHeader, Record: LineageIndexRecord{Header: &lineageHeader}}
	lineageFrame.RecordDigest, err = lineageFrame.ComputeDigest()
	if err != nil || lineageFrame.Validate() != nil {
		return LineageIndexFrame{}, LineageIndexFrame{}, nil, nil, fail(CodeEvidenceRecoveryRequired, "admission-plan", "lineage header cannot be planned", err)
	}
	lineageBytes, err := EncodeCanonicalLineageFrame(lineageFrame)
	if err != nil {
		return LineageIndexFrame{}, LineageIndexFrame{}, nil, nil, err
	}
	if history.rootFacts.targetIndexPresent {
		wantHeader := admissionReplayLineageHeader{bindings.executionLineageDigest, bindings.deploymentID, bindings.expectedDatabaseName, bindings.releaseSubject.RepositoryIdentity, LineageLimitsProfile}
		if history.targetState != admissionLineageEmpty || history.targetHeader != wantHeader || history.targetIndexRecords != 1 || history.targetIndexTail != lineageFrame.RecordDigest {
			return LineageIndexFrame{}, LineageIndexFrame{}, nil, nil, fail(CodeEvidenceRecoveryRequired, "admission-plan", "existing target is not registered-empty under the current lineage", nil)
		}
	} else if history.targetState != "" || history.targetIndexRecords != 0 || history.targetIndexTail != "" {
		return LineageIndexFrame{}, LineageIndexFrame{}, nil, nil, admissionCorrupt("admission-plan", "absent target carries an index boundary", nil)
	}
	reserved, _, err := buildPlannedGenerationReservation(history, candidate, nil)
	if err != nil {
		return LineageIndexFrame{}, LineageIndexFrame{}, nil, nil, fail(CodeEvidenceRecoveryRequired, "admission-plan", "generation reservation cannot be planned", err)
	}
	previous := lineageFrame.RecordDigest
	sequence := uint64(1)
	if history.rootFacts.targetIndexPresent {
		previous, sequence = history.targetIndexTail, history.targetIndexRecords
	}
	reservedFrame := LineageIndexFrame{FormatVersion: LineageFrameFormat, Sequence: sequence, PreviousRecordDigest: &previous, RecordKind: LineageRecordGenerationReserved, Record: LineageIndexRecord{Reserved: &reserved}}
	reservedFrame.RecordDigest, err = reservedFrame.ComputeDigest()
	if err != nil || reservedFrame.Validate() != nil {
		return LineageIndexFrame{}, LineageIndexFrame{}, nil, nil, fail(CodeEvidenceRecoveryRequired, "admission-plan", "generation reservation frame cannot be planned", err)
	}
	reservedBytes, err := EncodeCanonicalLineageFrame(reservedFrame)
	if err != nil {
		return LineageIndexFrame{}, LineageIndexFrame{}, nil, nil, err
	}
	plannedIndexBytes := uint64(len(reservedBytes))
	plannedIndexRecords := uint64(1)
	if !history.rootFacts.targetIndexPresent {
		plannedIndexBytes += uint64(len(lineageBytes))
		plannedIndexRecords++
	}
	if plannedIndexBytes > history.reservation.ReservedIndexBytes || plannedIndexRecords > history.reservation.ReservedIndexRecords {
		return LineageIndexFrame{}, LineageIndexFrame{}, nil, nil, fail(CodeEvidenceJournalLimitExceeded, "admission-plan", "planned index frames exceed candidate reservation", nil)
	}
	return lineageFrame, reservedFrame, lineageBytes, reservedBytes, nil
}

func buildPlannedGenerationReservation(history *VerifiedAdmissionHistory, candidate OwnedCurrentCandidate, continuation *LineageContinuationContext) (GenerationReserved, EvidenceFrame, error) {
	if history == nil || !validOwnedCurrentCandidate(candidate) || history.candidateBinding != candidate.binding || history.target != digestRaw(candidate.verifiedRun.executionLineageDigest) || history.reservation.ReservedRecords == 0 || history.reservation.ReservedJournalBytes == 0 || history.reservation.ReservedSegments == 0 || history.reservation.ReservedBytes != history.reservation.ReservedJournalBytes+history.reservation.ReservedIndexBytes {
		return GenerationReserved{}, EvidenceFrame{}, fail(CodeEvidenceRecoveryRequired, "admission-plan", "generation reservation facts are unavailable", nil)
	}
	profile := quotaReservationProfile(history.reservation)
	if history.quotaProfile != "" {
		if !validEvidenceLimitsProfile(history.quotaProfile) || history.quotaProfile != profile {
			return GenerationReserved{}, EvidenceFrame{}, fail(CodeEvidenceRecoveryRequired, "admission-plan", "generation reservation profile is inconsistent", nil)
		}
		profile = history.quotaProfile
	}
	if history.currentFacts != nil && history.currentFacts.lineageQuotaProfile != profile {
		return GenerationReserved{}, EvidenceFrame{}, fail(CodeEvidenceRecoveryRequired, "admission-plan", "generation reservation facts use a different quota profile", nil)
	}
	if continuation != nil && continuation.Validate() != nil {
		return GenerationReserved{}, EvidenceFrame{}, fail(CodeEvidenceRecoveryRequired, "admission-plan", "generation continuation is invalid", nil)
	}
	run := candidate.verifiedRun
	header := JournalHeader{
		FormatVersion: EvidenceJournalFormat, ReleaseTrustDecisionDigest: run.releaseTrustDecisionDigest, RunnerProjectionDecisionDigest: run.runnerProjectionDecisionDigest,
		ExecutionLineageDigest: run.executionLineageDigest, OuterArtifactDigest: run.outerArtifactDigest, OuterArtifactSizeBytes: run.outerArtifactSizeBytes,
		DecisionRecoveryArtifactSHA256: run.decisionRecoveryArtifactSHA256, DecisionRecoveryArtifactSizeBytes: run.decisionRecoveryArtifactSizeBytes,
		ManifestDigest: run.manifestDigest, RunnerReleaseDigest: run.runnerReleaseDigest, SchemaBundleDigest: run.schemaBundleDigest,
		AuthorityProfileDigest: run.authorityProfileDigest, AuthorityBindingDigest: run.authorityBindingDigest,
		LimitsProfile: profile, ReservedRecords: history.reservation.ReservedRecords, ReservedBytes: history.reservation.ReservedBytes, ReservedSegments: history.reservation.ReservedSegments,
	}
	var err error
	header.JournalIdentityDigest, err = JournalIdentityDigest(header)
	if err != nil {
		return GenerationReserved{}, EvidenceFrame{}, err
	}
	reserved := GenerationReserved{
		ExecutionLineageDigest: run.executionLineageDigest, JournalIdentityDigest: header.JournalIdentityDigest, RunnerProjectionDecisionDigest: run.runnerProjectionDecisionDigest,
		SchemaBundleDigest: run.schemaBundleDigest, ReservedRecords: history.reservation.ReservedRecords, ReservedBytes: history.reservation.ReservedBytes,
		ReservedSegments: history.reservation.ReservedSegments, Continuation: cloneProjectionValue(continuation),
	}
	reserved.PlannedSegment0Header = header
	reserved.QuotaReservationDigest, err = QuotaReservationDigest(reserved)
	if err != nil {
		return GenerationReserved{}, EvidenceFrame{}, err
	}
	header.QuotaReservationDigest = reserved.QuotaReservationDigest
	reserved.PlannedSegment0Header = header
	headerFrame := EvidenceFrame{FormatVersion: EvidenceFrameFormat, Sequence: 0, RecordKind: EvidenceRecordHeader, Record: EvidenceRecord{Header: &header}}
	reserved.ExpectedSegment0HeaderDigest, err = headerFrame.ComputeDigest()
	if err != nil || reserved.Validate() != nil {
		return GenerationReserved{}, EvidenceFrame{}, fail(CodeEvidenceRecoveryRequired, "admission-plan", "generation reservation cannot be planned", err)
	}
	headerFrame.RecordDigest = reserved.ExpectedSegment0HeaderDigest
	return reserved, headerFrame, nil
}

func admissionPlanDigest(plan *VerifiedAdmissionPlan) [32]byte {
	if plan == nil || plan.history == nil || plan.candidateBinding == nil || plan.history.binding == nil {
		return [32]byte{}
	}
	h := sha256.New()
	h.Write([]byte("cloud-agents-platform-verified-admission-plan/v1\x00"))
	h.Write(plan.history.binding.canonical[:])
	h.Write(plan.candidateBinding.canonical[:])
	h.Write(plan.lineageHeaderBytes)
	h.Write(plan.reservedFrameBytes)
	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result
}

func validVerifiedAdmissionPlan(plan *VerifiedAdmissionPlan, history *VerifiedAdmissionHistory, candidate OwnedCurrentCandidate) bool {
	if plan == nil || history == nil || plan.history != history || plan.candidateBinding != candidate.binding || plan.binding == nil || plan.binding.plan != plan || plan.binding.history != history || plan.binding.candidate != candidate.binding || plan.consumed == nil || plan.consumed.Load() || !validVerifiedAdmissionHistory(history, candidate) || plan.binding.canonical == ([32]byte{}) || plan.binding.canonical != admissionPlanDigest(plan) || !admissionPlanFramesExact(plan) {
		return false
	}
	registered, ok := verifiedAdmissionPlanRegistry.Load(plan.binding)
	return ok && registered == plan.binding.canonical
}

func admissionPlanFramesExact(plan *VerifiedAdmissionPlan) bool {
	if plan == nil || plan.lineageHeaderFrame.Validate() != nil || plan.reservedFrame.Validate() != nil {
		return false
	}
	lineageBytes, lineageErr := EncodeCanonicalLineageFrame(plan.lineageHeaderFrame)
	reservedBytes, reservedErr := EncodeCanonicalLineageFrame(plan.reservedFrame)
	return lineageErr == nil && reservedErr == nil && string(lineageBytes) == string(plan.lineageHeaderBytes) && string(reservedBytes) == string(plan.reservedFrameBytes)
}

func bindAdmissionPermit(ctx context.Context, inventory *evidencefs.AdmissionInventory, token *evidencefs.AdmissionMutationToken, history *VerifiedAdmissionHistory, plan *VerifiedAdmissionPlan, candidate OwnedCurrentCandidate) (*AdmissionPermit, error) {
	if inventory == nil || token == nil || history == nil || plan == nil || history.inventory != inventory || !token.ValidFor(inventory) || !validVerifiedAdmissionPlan(plan, history, candidate) {
		return nil, fail(CodeEvidenceRecoveryRequired, "admission-permit", "migration and filesystem admission authority do not match", nil)
	}
	if err := inventory.Revalidate(ctx); err != nil {
		return nil, mapEvidenceAdmissionError(err, "admission-permit-revalidate")
	}
	if !token.ValidFor(inventory) || !validVerifiedAdmissionPlan(plan, history, candidate) || !plan.consumed.CompareAndSwap(false, true) {
		return nil, fail(CodeEvidenceRecoveryRequired, "admission-permit", "admission authority changed or was already consumed", nil)
	}
	permit := &AdmissionPermit{
		history: history, plan: plan, candidateBinding: candidate.binding, inventory: inventory, mutation: token, consumed: &atomic.Bool{},
	}
	permit.self = permit
	binding := &admissionPermitBinding{permit: permit, history: history, plan: plan, inventory: inventory, mutation: token}
	permit.binding = binding
	binding.canonical = admissionPermitDigest(permit)
	admissionPermitRegistry.Store(binding, binding.canonical)
	return permit, nil
}

func admissionPermitDigest(permit *AdmissionPermit) [32]byte {
	if permit == nil || permit.self != permit || permit.history == nil || permit.plan == nil || permit.candidateBinding == nil || permit.history.binding == nil || permit.plan.binding == nil {
		return [32]byte{}
	}
	h := sha256.New()
	h.Write([]byte("cloud-agents-platform-admission-permit/v1\x00"))
	h.Write(permit.history.binding.canonical[:])
	h.Write(permit.plan.binding.canonical[:])
	h.Write(permit.candidateBinding.canonical[:])
	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result
}

func validAdmissionPermit(permit *AdmissionPermit, inventory *evidencefs.AdmissionInventory, candidate OwnedCurrentCandidate) bool {
	if permit == nil || permit.self != permit || permit.binding == nil || permit.binding.permit != permit || permit.history == nil || permit.plan == nil || permit.inventory != inventory || permit.mutation == nil || permit.candidateBinding != candidate.binding || permit.binding.history != permit.history || permit.binding.plan != permit.plan || permit.binding.inventory != inventory || permit.binding.mutation != permit.mutation || permit.consumed == nil || permit.consumed.Load() || !permit.mutation.ValidFor(inventory) || !validOwnedCurrentCandidate(candidate) || permit.plan.history != permit.history || permit.plan.candidateBinding != candidate.binding || permit.plan.binding == nil || permit.plan.binding.plan != permit.plan || permit.plan.binding.history != permit.history || permit.plan.binding.candidate != candidate.binding || permit.plan.consumed == nil || !permit.plan.consumed.Load() || !admissionPlanFramesExact(permit.plan) || permit.plan.binding.canonical != admissionPlanDigest(permit.plan) || permit.binding.canonical == ([32]byte{}) || permit.binding.canonical != admissionPermitDigest(permit) {
		return false
	}
	registeredPlan, planOK := verifiedAdmissionPlanRegistry.Load(permit.plan.binding)
	if !planOK || registeredPlan != permit.plan.binding.canonical {
		return false
	}
	registered, ok := admissionPermitRegistry.Load(permit.binding)
	return ok && registered == permit.binding.canonical
}

func (p *AdmissionPermit) CreateTargetLineage(ctx context.Context, candidate OwnedCurrentCandidate) (AdmissionPermitTransitionResult, error) {
	pre := AdmissionPermitTransitionResult{outcome: evidencefs.AdmissionTransitionPreMutationFailure, candidateKind: "target_lineage"}
	if p == nil || p.inventory == nil || !validAdmissionPermit(p, p.inventory, candidate) || p.plan.lineageHeaderFrame.Record.Header == nil {
		return pre, fail(CodeEvidenceRecoveryRequired, "admission-target-register", "admission permit cannot register a target lineage", nil)
	}
	if p.history.rootFacts.targetIndexPresent {
		pre.candidateKind = "target_lineage_reuse"
		if p.history.targetState != admissionLineageEmpty || p.history.targetIndexRecords != 1 || p.history.targetIndexTail != p.plan.lineageHeaderFrame.RecordDigest {
			return pre, fail(CodeEvidenceRecoveryRequired, "admission-target-register", "present target is not registered-empty", nil)
		}
	}
	pre.previousRevision = p.history.revision
	pre.candidateRevision = p.history.revision + 1
	pre.candidateDigest = sha256.Sum256(p.plan.lineageHeaderBytes)
	if !p.consumed.CompareAndSwap(false, true) {
		return pre, fail(CodeEvidenceRecoveryRequired, "admission-target-register", "admission permit was already consumed", nil)
	}
	var fsResult evidencefs.AdmissionTransitionResult
	var transitionErr error
	if p.history.rootFacts.targetIndexPresent {
		fsResult, transitionErr = p.mutation.ReuseTargetLineage(ctx, p.inventory, p.plan.lineageHeaderBytes)
	} else {
		fsResult, transitionErr = p.mutation.CreateTargetLineage(ctx, p.inventory, p.plan.lineageHeaderBytes)
	}
	result := admissionPermitTransitionFromFS(fsResult)
	if fsResult.Outcome() != evidencefs.AdmissionTransitionDurable {
		if fsResult.Outcome() == evidencefs.AdmissionTransitionPreMutationFailure {
			p.consumed.CompareAndSwap(true, false)
		}
		return result, mapAdmissionMutationError(transitionErr, "admission-target-register")
	}
	if transitionErr != nil || fsResult.Inventory() == nil || fsResult.CandidateKind() != pre.candidateKind || fsResult.CandidateSequence() != 0 || fsResult.CandidateDigest() != pre.candidateDigest || fsResult.PreviousRevision() != pre.previousRevision || fsResult.CandidateRevision() != pre.candidateRevision {
		_ = fsResult.Invalidate()
		return admissionMutationUnknown(result), admissionFailed("admission-target-register", "durable target registration result is inconsistent", nil)
	}
	nextInventory := fsResult.Inventory()
	if err := nextInventory.Revalidate(ctx); err != nil {
		_ = fsResult.Invalidate()
		return admissionMutationUnknown(result), admissionPostMutationFailure("admission-target-register-revalidate")
	}
	revision, err := nextInventory.Revision()
	if err != nil || revision != pre.candidateRevision {
		_ = fsResult.Invalidate()
		return admissionMutationUnknown(result), admissionPostMutationFailure("admission-target-register-revision")
	}
	target, err := nextInventory.Target()
	if err != nil || target != p.history.target {
		_ = fsResult.Invalidate()
		return admissionMutationUnknown(result), admissionPostMutationFailure("admission-target-register-target")
	}
	fullSet, err := nextInventory.FullSetDigest()
	fullSetChanged := fullSet != p.history.fullSet
	if err != nil || fullSet == ([32]byte{}) || fullSetChanged == p.history.rootFacts.targetIndexPresent {
		_ = fsResult.Invalidate()
		return admissionMutationUnknown(result), admissionPostMutationFailure("admission-target-register-full-set")
	}
	lineage, err := nextInventory.Lineage(target)
	if err != nil {
		_ = fsResult.Invalidate()
		return admissionMutationUnknown(result), admissionPostMutationFailure("admission-target-register-lineage")
	}
	index, err := lineage.Index()
	if err != nil {
		_ = fsResult.Invalidate()
		return admissionMutationUnknown(result), admissionPostMutationFailure("admission-target-register-index")
	}
	indexBytes, err := index.ReadAll(ctx)
	if err != nil || string(indexBytes) != string(p.plan.lineageHeaderBytes) || sha256.Sum256(indexBytes) != pre.candidateDigest {
		_ = fsResult.Invalidate()
		return admissionMutationUnknown(result), admissionPostMutationFailure("admission-target-register-index-read")
	}
	if absent, err := nextInventory.TargetAbsent(); err != nil || absent != nil {
		_ = fsResult.Invalidate()
		return admissionMutationUnknown(result), admissionPostMutationFailure("admission-target-register-absence")
	}
	nextToken, err := nextInventory.MutationToken()
	if err != nil || !nextToken.ValidFor(nextInventory) {
		_ = fsResult.Invalidate()
		return admissionMutationUnknown(result), admissionPostMutationFailure("admission-target-register-token")
	}
	next := &RegisteredAdmissionPermit{
		prior: p, plan: p.plan, candidateBinding: candidate.binding, inventory: nextInventory, mutation: nextToken,
		target: target, fullSet: fullSet, revision: revision, indexDigest: pre.candidateDigest, reused: p.history.rootFacts.targetIndexPresent, consumed: &atomic.Bool{},
	}
	next.self = next
	binding := &registeredAdmissionPermitBinding{permit: next, prior: p, plan: p.plan, inventory: nextInventory, mutation: nextToken}
	next.binding = binding
	binding.canonical = registeredAdmissionPermitDigest(next)
	registeredAdmissionPermitRegistry.Store(binding, binding.canonical)
	if !validRegisteredAdmissionPermit(next, nextInventory, candidate) {
		_ = fsResult.Invalidate()
		return admissionMutationUnknown(result), admissionFailed("admission-target-register", "next admission permit could not be sealed", nil)
	}
	result.next = next
	return result, nil
}

func admissionPermitTransitionFromFS(value evidencefs.AdmissionTransitionResult) AdmissionPermitTransitionResult {
	result := AdmissionPermitTransitionResult{
		outcome: value.Outcome(), candidateKind: value.CandidateKind(), candidateDigest: value.CandidateDigest(),
		candidateSequence: value.CandidateSequence(), candidateRevision: value.CandidateRevision(), previousRevision: value.PreviousRevision(),
	}
	if result.outcome != evidencefs.AdmissionTransitionDurable {
		result.next = nil
	}
	return result
}

func admissionMutationUnknown(value AdmissionPermitTransitionResult) AdmissionPermitTransitionResult {
	value.outcome = evidencefs.AdmissionTransitionUnknown
	value.next = nil
	return value
}

func mapAdmissionMutationError(err error, op string) error {
	switch {
	case errors.Is(err, evidencefs.ErrUnknown):
		return admissionFailed(op, "admission mutation outcome is unknown", nil)
	case err == nil:
		return admissionFailed(op, "admission mutation returned no error", nil)
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return mapEvidenceAdmissionError(err, op)
	case errors.Is(err, evidencefs.ErrLimit):
		return fail(CodeEvidenceJournalLimitExceeded, op, "admission mutation exceeds a fixed limit", nil)
	case errors.Is(err, evidencefs.ErrCorrupt):
		return admissionCorrupt(op, "admission mutation observed corrupt stored state", nil)
	default:
		return admissionFailed(op, "admission mutation could not start", nil)
	}
}

func admissionPostMutationFailure(op string) error {
	return admissionFailed(op, "admission mutation outcome is unknown", nil)
}

func registeredAdmissionPermitDigest(permit *RegisteredAdmissionPermit) [32]byte {
	if permit == nil || permit.self != permit || permit.prior == nil || permit.plan == nil || permit.candidateBinding == nil || permit.prior.binding == nil || permit.plan.binding == nil {
		return [32]byte{}
	}
	h := sha256.New()
	h.Write([]byte("cloud-agents-platform-registered-admission-permit/v1\x00"))
	h.Write(permit.prior.binding.canonical[:])
	h.Write(permit.plan.binding.canonical[:])
	h.Write(permit.candidateBinding.canonical[:])
	h.Write(permit.target[:])
	h.Write(permit.fullSet[:])
	h.Write(permit.indexDigest[:])
	writeAdmissionUint(h, permit.revision)
	if permit.reused {
		h.Write([]byte{1})
	} else {
		h.Write([]byte{0})
	}
	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result
}

func validRegisteredAdmissionPermit(permit *RegisteredAdmissionPermit, inventory *evidencefs.AdmissionInventory, candidate OwnedCurrentCandidate) bool {
	if permit == nil || permit.self != permit || permit.binding == nil || permit.binding.permit != permit || permit.prior == nil || permit.plan == nil || permit.plan.lineageHeaderFrame.Record.Header == nil || permit.inventory != inventory || permit.mutation == nil || permit.candidateBinding != candidate.binding || permit.binding.prior != permit.prior || permit.binding.plan != permit.plan || permit.binding.inventory != inventory || permit.binding.mutation != permit.mutation || permit.consumed == nil || permit.consumed.Load() || !validConsumedAdmissionPermit(permit.prior, permit.plan, candidate) || permit.indexDigest != sha256.Sum256(permit.plan.lineageHeaderBytes) || permit.target != digestRaw(permit.plan.lineageHeaderFrame.Record.Header.ExecutionLineageDigest) || permit.revision != permit.prior.history.revision+1 || permit.reused != permit.prior.history.rootFacts.targetIndexPresent || (permit.fullSet == permit.prior.history.fullSet) != permit.reused || permit.binding.canonical == ([32]byte{}) || permit.binding.canonical != registeredAdmissionPermitDigest(permit) || !permit.mutation.ValidFor(inventory) {
		return false
	}
	registeredPlan, planOK := verifiedAdmissionPlanRegistry.Load(permit.plan.binding)
	registeredPrior, priorOK := admissionPermitRegistry.Load(permit.prior.binding)
	registered, ok := registeredAdmissionPermitRegistry.Load(permit.binding)
	if !planOK || registeredPlan != permit.plan.binding.canonical || !priorOK || registeredPrior != permit.prior.binding.canonical || !ok || registered != permit.binding.canonical {
		return false
	}
	revision, err := inventory.Revision()
	if err != nil || revision != permit.revision {
		return false
	}
	target, err := inventory.Target()
	if err != nil || target != permit.target {
		return false
	}
	fullSet, err := inventory.FullSetDigest()
	return err == nil && fullSet == permit.fullSet
}

func validConsumedAdmissionPermit(permit *AdmissionPermit, plan *VerifiedAdmissionPlan, candidate OwnedCurrentCandidate) bool {
	if permit == nil || permit.self != permit || permit.binding == nil || permit.binding.permit != permit || permit.history == nil || permit.history.binding == nil || permit.plan != plan || plan == nil || plan.binding == nil || permit.inventory == nil || permit.inventory != permit.history.inventory || permit.mutation == nil || permit.candidateBinding != candidate.binding || permit.binding.history != permit.history || permit.binding.plan != plan || permit.binding.inventory != permit.inventory || permit.binding.mutation != permit.mutation || permit.consumed == nil || !permit.consumed.Load() || plan.consumed == nil || !plan.consumed.Load() || permit.history.owner == nil || permit.history.owner != candidate.verifiedRun.currentDecision.owner || permit.history.candidateBinding != candidate.binding || permit.history.binding.owner != permit.history.owner || permit.history.binding.candidateBinding != candidate.binding || permit.history.binding.inventory != permit.history.inventory || permit.history.binding.history != permit.history || plan.history != permit.history || plan.candidateBinding != candidate.binding || plan.binding.plan != plan || plan.binding.history != permit.history || plan.binding.candidate != candidate.binding || permit.history.binding.canonical != admissionHistoryDigest(permit.history) || !permit.history.rootFacts.valid() || !admissionPlanFramesExact(plan) || plan.binding.canonical != admissionPlanDigest(plan) || permit.binding.canonical != admissionPermitDigest(permit) {
		return false
	}
	registeredHistory, historyOK := verifiedAdmissionHistoryRegistry.Load(permit.history.binding)
	registeredPlan, planOK := verifiedAdmissionPlanRegistry.Load(plan.binding)
	registeredPermit, permitOK := admissionPermitRegistry.Load(permit.binding)
	return historyOK && registeredHistory == permit.history.binding.canonical && planOK && registeredPlan == plan.binding.canonical && permitOK && registeredPermit == permit.binding.canonical
}

func (p *RegisteredAdmissionPermit) PublishRuntime(ctx context.Context, candidate OwnedCurrentCandidate) (RuntimePublicationTransitionResult, error) {
	pre := RuntimePublicationTransitionResult{outcome: evidencefs.AdmissionTransitionPreMutationFailure}
	if p == nil || p.inventory == nil || !validRegisteredAdmissionPermit(p, p.inventory, candidate) {
		return pre, fail(CodeEvidenceRecoveryRequired, "admission-runtime-publish", "registered admission permit is unavailable", nil)
	}
	runtime := candidate.runtimeArtifact
	pre.previousRevision = p.revision
	pre.candidateRevision = p.revision + 1
	pre.candidateDigest = digestRaw(runtime.digest)
	if !p.consumed.CompareAndSwap(false, true) {
		return pre, fail(CodeEvidenceRecoveryRequired, "admission-runtime-publish", "registered admission permit was already consumed", nil)
	}
	fsResult, transitionErr := p.mutation.PublishObject(ctx, p.inventory, pre.candidateDigest, runtime.bytes)
	result := RuntimePublicationTransitionResult{
		outcome: fsResult.Outcome(), candidateDigest: fsResult.CandidateDigest(),
		candidateSequence: 1, candidateRevision: fsResult.CandidateRevision(), previousRevision: fsResult.PreviousRevision(),
	}
	if fsResult.Outcome() != evidencefs.AdmissionTransitionDurable {
		if fsResult.Outcome() == evidencefs.AdmissionTransitionPreMutationFailure {
			p.consumed.CompareAndSwap(true, false)
		}
		return result, mapAdmissionMutationError(transitionErr, "admission-runtime-publish")
	}
	if transitionErr != nil || fsResult.CandidateKind() != "content_object" || fsResult.CandidateSequence() != 0 || fsResult.CandidateDigest() != pre.candidateDigest || fsResult.Size() != runtime.sizeBytes || fsResult.PreviousRevision() != pre.previousRevision || fsResult.CandidateRevision() != pre.candidateRevision || !fsResult.ValidFor(fsResult.Inventory()) {
		_ = fsResult.Invalidate()
		return runtimePublicationUnknown(result), admissionPostMutationFailure("admission-runtime-publish")
	}
	nextInventory := fsResult.Inventory()
	if err := nextInventory.Revalidate(ctx); err != nil {
		_ = fsResult.Invalidate()
		return runtimePublicationUnknown(result), admissionPostMutationFailure("admission-runtime-publish-revalidate")
	}
	revision, err := nextInventory.Revision()
	if err != nil || revision != pre.candidateRevision {
		_ = fsResult.Invalidate()
		return runtimePublicationUnknown(result), admissionPostMutationFailure("admission-runtime-publish-revision")
	}
	target, err := nextInventory.Target()
	if err != nil || target != p.target {
		_ = fsResult.Invalidate()
		return runtimePublicationUnknown(result), admissionPostMutationFailure("admission-runtime-publish-target")
	}
	fullSet, err := nextInventory.FullSetDigest()
	if err != nil || fullSet == ([32]byte{}) || (fullSet == p.fullSet) != fsResult.Reused() {
		_ = fsResult.Invalidate()
		return runtimePublicationUnknown(result), admissionPostMutationFailure("admission-runtime-publish-full-set")
	}
	nextToken, err := nextInventory.MutationToken()
	if err != nil || !nextToken.ValidFor(nextInventory) {
		_ = fsResult.Invalidate()
		return runtimePublicationUnknown(result), admissionPostMutationFailure("admission-runtime-publish-token")
	}
	next := &RuntimePublishedPermit{
		prior: p, plan: p.plan, candidateBinding: candidate.binding, inventory: nextInventory, mutation: nextToken,
		publication: fsResult.Publication(), fsResult: fsResult, target: target, fullSet: fullSet, revision: revision,
		digest: runtime.digest, size: runtime.sizeBytes, reused: fsResult.Reused(), consumed: &atomic.Bool{},
	}
	next.self = next
	binding := &runtimePublishedPermitBinding{permit: next, prior: p, plan: p.plan, inventory: nextInventory, mutation: nextToken, publication: next.publication}
	next.binding = binding
	binding.canonical = runtimePublishedPermitDigest(next)
	runtimePublishedPermitRegistry.Store(binding, binding.canonical)
	if !validRuntimePublishedPermit(next, nextInventory, candidate) {
		_ = fsResult.Invalidate()
		return runtimePublicationUnknown(result), admissionPostMutationFailure("admission-runtime-publish-seal")
	}
	result.next = next
	return result, nil
}

func runtimePublicationUnknown(value RuntimePublicationTransitionResult) RuntimePublicationTransitionResult {
	value.outcome = evidencefs.AdmissionTransitionUnknown
	value.next = nil
	return value
}

func runtimePublishedPermitDigest(permit *RuntimePublishedPermit) [32]byte {
	if permit == nil || permit.self != permit || permit.prior == nil || permit.plan == nil || permit.candidateBinding == nil || permit.prior.binding == nil || permit.plan.binding == nil {
		return [32]byte{}
	}
	h := sha256.New()
	h.Write([]byte("cloud-agents-platform-runtime-published-permit/v1\x00"))
	h.Write(permit.prior.binding.canonical[:])
	h.Write(permit.plan.binding.canonical[:])
	h.Write(permit.candidateBinding.canonical[:])
	h.Write(permit.target[:])
	h.Write(permit.fullSet[:])
	writeAdmissionUint(h, permit.revision)
	writeAdmissionString(h, permit.digest.String())
	writeAdmissionUint(h, permit.size)
	if permit.reused {
		h.Write([]byte{1})
	} else {
		h.Write([]byte{0})
	}
	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result
}

func validRuntimePublishedPermit(permit *RuntimePublishedPermit, inventory *evidencefs.AdmissionInventory, candidate OwnedCurrentCandidate) bool {
	if permit == nil || permit.self != permit || permit.binding == nil || permit.binding.permit != permit || permit.prior == nil || permit.plan == nil || permit.inventory != inventory || permit.mutation == nil || permit.publication == nil || permit.candidateBinding != candidate.binding || permit.binding.prior != permit.prior || permit.binding.plan != permit.plan || permit.binding.inventory != inventory || permit.binding.mutation != permit.mutation || permit.binding.publication != permit.publication || permit.consumed == nil || permit.consumed.Load() || !validConsumedRegisteredAdmissionPermit(permit.prior, permit.plan, candidate) || permit.revision != permit.prior.revision+1 || permit.target != permit.prior.target || (permit.fullSet == permit.prior.fullSet) != permit.reused || permit.digest != candidate.runtimeArtifact.digest || permit.size != candidate.runtimeArtifact.sizeBytes || permit.fsResult.Publication() != permit.publication || permit.fsResult.Reused() != permit.reused || permit.fsResult.CandidateDigest() != digestRaw(permit.digest) || permit.fsResult.Size() != permit.size || !permit.fsResult.ValidFor(inventory) || !permit.mutation.ValidFor(inventory) || permit.binding.canonical == ([32]byte{}) || permit.binding.canonical != runtimePublishedPermitDigest(permit) {
		return false
	}
	registered, ok := runtimePublishedPermitRegistry.Load(permit.binding)
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

func validConsumedRegisteredAdmissionPermit(permit *RegisteredAdmissionPermit, plan *VerifiedAdmissionPlan, candidate OwnedCurrentCandidate) bool {
	if permit == nil || permit.self != permit || permit.binding == nil || permit.binding.permit != permit || permit.prior == nil || permit.plan != plan || plan == nil || plan.lineageHeaderFrame.Record.Header == nil || permit.inventory == nil || permit.mutation == nil || permit.candidateBinding != candidate.binding || permit.binding.prior != permit.prior || permit.binding.plan != plan || permit.binding.inventory != permit.inventory || permit.binding.mutation != permit.mutation || permit.consumed == nil || !permit.consumed.Load() || !validConsumedAdmissionPermit(permit.prior, plan, candidate) || permit.indexDigest != sha256.Sum256(plan.lineageHeaderBytes) || permit.target != digestRaw(plan.lineageHeaderFrame.Record.Header.ExecutionLineageDigest) || permit.revision != permit.prior.history.revision+1 || permit.reused != permit.prior.history.rootFacts.targetIndexPresent || (permit.fullSet == permit.prior.history.fullSet) != permit.reused || permit.binding.canonical != registeredAdmissionPermitDigest(permit) {
		return false
	}
	registered, ok := registeredAdmissionPermitRegistry.Load(permit.binding)
	return ok && registered == permit.binding.canonical
}

func (p *RuntimePublishedPermit) BindRuntime(ctx context.Context, candidate OwnedCurrentCandidate) (RuntimeBindingTransitionResult, error) {
	pre := RuntimeBindingTransitionResult{outcome: evidencefs.AdmissionTransitionPreMutationFailure, candidateDigest: digestRaw(candidate.runtimeArtifact.digest), candidateSequence: 2}
	if p == nil || p.inventory == nil || !validRuntimePublishedPermit(p, p.inventory, candidate) {
		return pre, fail(CodeEvidenceRecoveryRequired, "admission-runtime-bind", "runtime publication permit is unavailable", nil)
	}
	pre.previousRevision = p.revision
	pre.candidateRevision = p.revision + 1
	if !p.consumed.CompareAndSwap(false, true) {
		return pre, fail(CodeEvidenceRecoveryRequired, "admission-runtime-bind", "runtime publication permit was already consumed", nil)
	}
	fsResult, transitionErr := p.mutation.BindPublishedObject(ctx, p.inventory, p.publication, pre.candidateDigest, p.size)
	result := RuntimeBindingTransitionResult{outcome: fsResult.Outcome(), candidateDigest: fsResult.CandidateDigest(), candidateSequence: 2, candidateRevision: fsResult.CandidateRevision(), previousRevision: fsResult.PreviousRevision()}
	if fsResult.Outcome() != evidencefs.AdmissionTransitionDurable {
		if fsResult.Outcome() == evidencefs.AdmissionTransitionPreMutationFailure {
			p.consumed.CompareAndSwap(true, false)
		}
		return result, mapAdmissionMutationError(transitionErr, "admission-runtime-bind")
	}
	if transitionErr != nil || fsResult.CandidateKind() != "content_binding" || fsResult.CandidateSequence() != 0 || fsResult.CandidateDigest() != pre.candidateDigest || fsResult.Size() != p.size || fsResult.PreviousRevision() != pre.previousRevision || fsResult.CandidateRevision() != pre.candidateRevision || fsResult.Publication() != p.publication || !fsResult.ValidFor(fsResult.Inventory()) {
		_ = fsResult.Invalidate()
		return runtimeBindingUnknown(result), admissionPostMutationFailure("admission-runtime-bind")
	}
	nextInventory := fsResult.Inventory()
	if err := nextInventory.Revalidate(ctx); err != nil {
		_ = fsResult.Invalidate()
		return runtimeBindingUnknown(result), admissionPostMutationFailure("admission-runtime-bind-revalidate")
	}
	revision, err := nextInventory.Revision()
	if err != nil || revision != pre.candidateRevision {
		_ = fsResult.Invalidate()
		return runtimeBindingUnknown(result), admissionPostMutationFailure("admission-runtime-bind-revision")
	}
	fullSet, err := nextInventory.FullSetDigest()
	if err != nil || fullSet != p.fullSet || !p.publication.Matches(pre.candidateDigest, p.size) {
		_ = fsResult.Invalidate()
		return runtimeBindingUnknown(result), admissionPostMutationFailure("admission-runtime-bind-full-set")
	}
	nextToken, err := nextInventory.MutationToken()
	if err != nil || !nextToken.ValidFor(nextInventory) {
		_ = fsResult.Invalidate()
		return runtimeBindingUnknown(result), admissionPostMutationFailure("admission-runtime-bind-token")
	}
	next := &RuntimeBoundPermit{
		prior: p, plan: p.plan, candidateBinding: candidate.binding, inventory: nextInventory, mutation: nextToken,
		publication: p.publication, target: p.target, fullSet: fullSet, revision: revision, digest: p.digest, size: p.size, consumed: &atomic.Bool{},
	}
	next.self = next
	binding := &runtimeBoundPermitBinding{permit: next, prior: p, plan: p.plan, inventory: nextInventory, mutation: nextToken, publication: p.publication}
	next.binding = binding
	binding.canonical = runtimeBoundPermitDigest(next)
	runtimeBoundPermitRegistry.Store(binding, binding.canonical)
	if !validRuntimeBoundPermit(next, nextInventory, candidate) {
		_ = fsResult.Invalidate()
		return runtimeBindingUnknown(result), admissionPostMutationFailure("admission-runtime-bind-seal")
	}
	result.next = next
	return result, nil
}

func runtimeBindingUnknown(value RuntimeBindingTransitionResult) RuntimeBindingTransitionResult {
	value.outcome = evidencefs.AdmissionTransitionUnknown
	value.next = nil
	return value
}

func runtimeBoundPermitDigest(permit *RuntimeBoundPermit) [32]byte {
	if permit == nil || permit.self != permit || permit.prior == nil || permit.plan == nil || permit.candidateBinding == nil || permit.prior.binding == nil || permit.plan.binding == nil {
		return [32]byte{}
	}
	h := sha256.New()
	h.Write([]byte("cloud-agents-platform-runtime-bound-permit/v1\x00"))
	h.Write(permit.prior.binding.canonical[:])
	h.Write(permit.plan.binding.canonical[:])
	h.Write(permit.candidateBinding.canonical[:])
	h.Write(permit.target[:])
	h.Write(permit.fullSet[:])
	writeAdmissionUint(h, permit.revision)
	writeAdmissionString(h, permit.digest.String())
	writeAdmissionUint(h, permit.size)
	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result
}

func validRuntimeBoundPermit(permit *RuntimeBoundPermit, inventory *evidencefs.AdmissionInventory, candidate OwnedCurrentCandidate) bool {
	if permit == nil || permit.self != permit || permit.binding == nil || permit.binding.permit != permit || permit.prior == nil || permit.plan == nil || permit.inventory != inventory || permit.mutation == nil || permit.publication == nil || permit.candidateBinding != candidate.binding || permit.binding.prior != permit.prior || permit.binding.plan != permit.plan || permit.binding.inventory != inventory || permit.binding.mutation != permit.mutation || permit.binding.publication != permit.publication || permit.consumed == nil || permit.consumed.Load() || !validConsumedRuntimePublishedPermit(permit.prior, permit.plan, candidate) || permit.revision != permit.prior.revision+1 || permit.target != permit.prior.target || permit.fullSet != permit.prior.fullSet || permit.digest != candidate.runtimeArtifact.digest || permit.size != candidate.runtimeArtifact.sizeBytes || !permit.publication.Matches(digestRaw(permit.digest), permit.size) || !permit.mutation.ValidFor(inventory) || permit.binding.canonical == ([32]byte{}) || permit.binding.canonical != runtimeBoundPermitDigest(permit) {
		return false
	}
	registered, ok := runtimeBoundPermitRegistry.Load(permit.binding)
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

func validConsumedRuntimePublishedPermit(permit *RuntimePublishedPermit, plan *VerifiedAdmissionPlan, candidate OwnedCurrentCandidate) bool {
	if permit == nil || permit.self != permit || permit.binding == nil || permit.binding.permit != permit || permit.prior == nil || permit.plan != plan || plan == nil || permit.inventory == nil || permit.mutation == nil || permit.publication == nil || permit.candidateBinding != candidate.binding || permit.binding.prior != permit.prior || permit.binding.plan != plan || permit.binding.inventory != permit.inventory || permit.binding.mutation != permit.mutation || permit.binding.publication != permit.publication || permit.consumed == nil || !permit.consumed.Load() || !validConsumedRegisteredAdmissionPermit(permit.prior, plan, candidate) || permit.revision != permit.prior.revision+1 || permit.target != permit.prior.target || (permit.fullSet == permit.prior.fullSet) != permit.reused || permit.digest != candidate.runtimeArtifact.digest || permit.size != candidate.runtimeArtifact.sizeBytes || permit.fsResult.Publication() != permit.publication || permit.fsResult.CandidateDigest() != digestRaw(permit.digest) || permit.fsResult.Size() != permit.size || permit.binding.canonical != runtimePublishedPermitDigest(permit) {
		return false
	}
	registered, ok := runtimePublishedPermitRegistry.Load(permit.binding)
	return ok && registered == permit.binding.canonical
}
