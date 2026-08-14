package migration

import (
	"context"
	"crypto/sha256"
	"sync"
	"sync/atomic"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/evidencefs"
)

// RegisteredGenerationHandoffResult is the closed transfer result for an
// already-registered target generation. Durable means the full-root admission
// lease was replaced by the exact retained generation lease and all pass-one
// file facts were re-bound; only Next carries continuing authority.
type RegisteredGenerationHandoffResult struct {
	outcome           evidencefs.AdmissionTransitionOutcome
	next              *RegisteredGenerationRecoveryReady
	candidateDigest   [32]byte
	candidateSequence uint64
	revision          uint64
}

func (r RegisteredGenerationHandoffResult) Outcome() evidencefs.AdmissionTransitionOutcome {
	return r.outcome
}
func (r RegisteredGenerationHandoffResult) Next() *RegisteredGenerationRecoveryReady {
	return r.next
}
func (r RegisteredGenerationHandoffResult) CandidateKind() string {
	return "registered_generation_handoff"
}
func (r RegisteredGenerationHandoffResult) CandidateDigest() [32]byte { return r.candidateDigest }
func (r RegisteredGenerationHandoffResult) CandidateSequence() uint64 { return r.candidateSequence }
func (r RegisteredGenerationHandoffResult) Revision() uint64          { return r.revision }

// RegisteredGenerationHandoffPermit is the migration/evidencefs composite
// authority for the transfer itself. It retains the one evidencefs mutation
// token so a genuine pre-transfer context failure can be retried without
// minting a second token or weakening the one-shot generation authority.
type RegisteredGenerationHandoffPermit struct {
	self             *RegisteredGenerationHandoffPermit
	history          *VerifiedAdmissionHistory
	registered       *verifiedAdmissionRegisteredGeneration
	candidateBinding *verifiedEvidenceRunBinding
	inventory        *evidencefs.AdmissionInventory
	mutation         *evidencefs.AdmissionMutationToken
	target           [32]byte
	journal          Digest
	revision         uint64
	binding          *registeredGenerationHandoffPermitBinding
	consumed         *atomic.Bool
}

type registeredGenerationHandoffPermitBinding struct {
	permit           *RegisteredGenerationHandoffPermit
	history          *VerifiedAdmissionHistory
	registered       *verifiedAdmissionRegisteredGeneration
	candidateBinding *verifiedEvidenceRunBinding
	inventory        *evidencefs.AdmissionInventory
	mutation         *evidencefs.AdmissionMutationToken
	canonical        [32]byte
}

type registeredGenerationHandoffPermitRegistryRecord struct {
	permit           *RegisteredGenerationHandoffPermit
	binding          *registeredGenerationHandoffPermitBinding
	history          *VerifiedAdmissionHistory
	registered       *verifiedAdmissionRegisteredGeneration
	candidateBinding *verifiedEvidenceRunBinding
	inventory        *evidencefs.AdmissionInventory
	mutation         *evidencefs.AdmissionMutationToken
	handoffConsumed  *atomic.Bool
	permitConsumed   *atomic.Bool
	oldCursorValid   *atomic.Bool
	runtimeBinding   *verifiedContentReceiptBinding
	recoveryBinding  *verifiedDecisionRecoveryReceiptBinding
	sourceCanonical  [32]byte
	replayCanonical  [32]byte
	canonical        [32]byte
}

var registeredGenerationHandoffPermitRegistry sync.Map

// RegisteredGenerationRecoveryReady owns the exact retained filesystem lease,
// a fresh current snapshot, and a renewed cursor/recovery pair. It is not an
// EvidenceJournal, session, runner, or database authority; the next slice must
// consume it through the existing sealed journal/session boundary.
type RegisteredGenerationRecoveryReady struct {
	self              *RegisteredGenerationRecoveryReady
	prior             *RegisteredGenerationHandoffPermit
	history           *VerifiedAdmissionHistory
	registered        *verifiedAdmissionRegisteredGeneration
	candidateBinding  *verifiedEvidenceRunBinding
	lease             *evidencefs.GenerationLease
	snapshot          *evidencefs.GenerationSnapshot
	replay            *verifiedAdmissionGenerationReplay
	generation        generationIdentity
	cursor            JournalCursor
	recovery          *RecoverySnapshot
	executionBindings *VerifiedRecoveryExecutionBindings
	snapshotIdentity  [32]byte
	binding           *registeredGenerationRecoveryReadyBinding
	consumed          *atomic.Bool
}

type registeredGenerationRecoveryReadyBinding struct {
	ready            *RegisteredGenerationRecoveryReady
	prior            *RegisteredGenerationHandoffPermit
	history          *VerifiedAdmissionHistory
	registered       *verifiedAdmissionRegisteredGeneration
	candidateBinding *verifiedEvidenceRunBinding
	lease            *evidencefs.GenerationLease
	snapshot         *evidencefs.GenerationSnapshot
	replay           *verifiedAdmissionGenerationReplay
	canonical        [32]byte
}

type registeredGenerationRecoveryReadyRegistryRecord struct {
	ready                 *RegisteredGenerationRecoveryReady
	binding               *registeredGenerationRecoveryReadyBinding
	prior                 *RegisteredGenerationHandoffPermit
	history               *VerifiedAdmissionHistory
	registered            *verifiedAdmissionRegisteredGeneration
	candidateBinding      *verifiedEvidenceRunBinding
	lease                 *evidencefs.GenerationLease
	snapshot              *evidencefs.GenerationSnapshot
	replay                *verifiedAdmissionGenerationReplay
	cursorValid           *atomic.Bool
	handoffConsumed       *atomic.Bool
	readyConsumed         *atomic.Bool
	oldCursorValid        *atomic.Bool
	runtimeBinding        *verifiedContentReceiptBinding
	recoveryBinding       *verifiedDecisionRecoveryReceiptBinding
	sourceCanonical       [32]byte
	sourceReplayCanonical [32]byte
	readyReplayCanonical  [32]byte
	canonical             [32]byte
}

var registeredGenerationRecoveryReadyRegistry sync.Map

// bindRegisteredGenerationHandoff consumes only the migration-side target
// generation one-shot and retains the evidencefs token in a sealed permit. It
// performs no filesystem ownership transfer.
func bindRegisteredGenerationHandoff(ctx context.Context, history *VerifiedAdmissionHistory, candidate OwnedCurrentCandidate) (*RegisteredGenerationHandoffPermit, error) {
	if !validVerifiedAdmissionHistory(history, candidate) || history.targetGeneration == nil || history.targetGeneration.replay == nil || !admissionStateRequiresGenerationReplay(history.targetState) {
		return nil, fail(CodeEvidenceRecoveryRequired, "registered-generation-handoff-bind", "registered target generation authority is unavailable", nil)
	}
	if err := contextAdmissionError(ctx); err != nil {
		return nil, err
	}
	registered := history.targetGeneration
	if registered.handoffConsumed == nil || !registered.handoffConsumed.CompareAndSwap(false, true) {
		return nil, fail(CodeEvidenceRecoveryRequired, "registered-generation-handoff-bind", "registered target generation authority is consumed", nil)
	}
	token, err := history.inventory.MutationToken()
	if err != nil {
		registered.handoffConsumed.CompareAndSwap(true, false)
		return nil, mapEvidenceAdmissionError(err, "registered-generation-handoff-bind")
	}
	permit := &RegisteredGenerationHandoffPermit{
		history: history, registered: registered, candidateBinding: candidate.binding,
		inventory: history.inventory, mutation: token, target: history.target,
		journal: registered.descriptor.identity.journalIdentityDigest, revision: history.revision,
		consumed: &atomic.Bool{},
	}
	permit.self = permit
	permit.binding = &registeredGenerationHandoffPermitBinding{
		permit: permit, history: history, registered: registered, candidateBinding: candidate.binding,
		inventory: history.inventory, mutation: token,
	}
	permit.binding.canonical = registeredGenerationHandoffPermitDigest(permit)
	registeredGenerationHandoffPermitRegistry.Store(permit, registeredGenerationHandoffPermitRegistryRecord{
		permit: permit, binding: permit.binding, history: history, registered: registered,
		candidateBinding: candidate.binding, inventory: history.inventory, mutation: token,
		handoffConsumed: registered.handoffConsumed, permitConsumed: permit.consumed,
		oldCursorValid: registered.replay.cursor.valid,
		runtimeBinding: registered.runtimeReceipt.binding, recoveryBinding: registered.recoveryReceipt.binding,
		sourceCanonical: registered.canonical, replayCanonical: registered.replay.canonical,
		canonical: permit.binding.canonical,
	})
	return permit, nil
}

// Handoff consumes the retained evidencefs token, transfers the target locks,
// compares the fresh snapshot with every pass-one file fact, and renews the
// cursor/recovery authority under that exact retained lease.
func (p *RegisteredGenerationHandoffPermit) Handoff(ctx context.Context, candidate OwnedCurrentCandidate) (RegisteredGenerationHandoffResult, error) {
	pre := RegisteredGenerationHandoffResult{outcome: evidencefs.AdmissionTransitionPreMutationFailure, candidateSequence: 1}
	if p == nil || !validRegisteredGenerationHandoffPermit(p, candidate) {
		return pre, fail(CodeEvidenceRecoveryRequired, "registered-generation-handoff", "registered handoff permit is unavailable", nil)
	}
	if err := contextAdmissionError(ctx); err != nil {
		return pre, err
	}
	pre.revision = p.revision
	pre.candidateDigest = registeredGenerationHandoffCandidateDigest(p)
	if pre.candidateDigest == ([32]byte{}) || !p.consumed.CompareAndSwap(false, true) {
		return pre, fail(CodeEvidenceRecoveryRequired, "registered-generation-handoff", "registered handoff permit is invalid or consumed", nil)
	}
	lease, transitionErr := p.mutation.HandoffGeneration(ctx, p.inventory, digestRaw(p.journal))
	result := RegisteredGenerationHandoffResult{
		outcome: evidencefs.AdmissionTransitionDurable, candidateDigest: pre.candidateDigest,
		candidateSequence: pre.candidateSequence, revision: pre.revision,
	}
	if transitionErr != nil || lease == nil || !lease.Active() {
		if lease == nil && p.mutation.ValidFor(p.inventory) {
			p.consumed.CompareAndSwap(true, false)
			result.outcome = evidencefs.AdmissionTransitionPreMutationFailure
			return result, mapAdmissionMutationError(transitionErr, "registered-generation-handoff")
		}
		result.outcome = evidencefs.AdmissionTransitionUnknown
		return result, failRegisteredGenerationHandoff(p, lease, JournalCursor{}, transitionErr, "registered-generation-handoff")
	}
	target, targetErr := lease.Target()
	journal, journalErr := lease.Journal()
	if targetErr != nil || journalErr != nil || target != p.target || journal != digestRaw(p.journal) {
		result.outcome = evidencefs.AdmissionTransitionUnknown
		return result, failRegisteredGenerationHandoff(p, lease, JournalCursor{}, evidencefs.ErrLeaseInvalid, "registered-generation-handoff-bind")
	}
	snapshot, err := lease.Snapshot(ctx)
	if err != nil || snapshot == nil || !lease.OwnsSnapshot(snapshot) {
		result.outcome = evidencefs.AdmissionTransitionUnknown
		return result, failRegisteredGenerationHandoff(p, lease, JournalCursor{}, err, "registered-generation-handoff-snapshot")
	}
	identity, err := registeredGenerationSnapshotIdentity(snapshot, p.registered.replay)
	if err != nil {
		result.outcome = evidencefs.AdmissionTransitionUnknown
		return result, failRegisteredGenerationHandoff(p, lease, JournalCursor{}, err, "registered-generation-handoff-snapshot")
	}
	cursor, err := renewGenerationJournalCursor(p.registered.replay.cursor)
	if err != nil {
		result.outcome = evidencefs.AdmissionTransitionUnknown
		return result, failRegisteredGenerationHandoff(p, lease, JournalCursor{}, err, "registered-generation-handoff-cursor")
	}
	recovery, err := renewGenerationJournalRecovery(p.registered.replay.recovery, cursor, p.registered.descriptor.identity)
	if err != nil {
		result.outcome = evidencefs.AdmissionTransitionUnknown
		return result, failRegisteredGenerationHandoff(p, lease, cursor, err, "registered-generation-handoff-recovery")
	}
	renewedReplay := cloneVerifiedAdmissionGenerationReplay(p.registered.replay)
	renewedReplay.cursor = cursor.clone()
	renewedReplay.recovery = cloneRecoverySnapshot(recovery)
	renewedReplay.canonical = verifiedAdmissionGenerationReplayDigest(renewedReplay, p.registered.descriptor.identity)
	if !validVerifiedAdmissionGenerationReplay(renewedReplay, p.registered.descriptor.identity) {
		result.outcome = evidencefs.AdmissionTransitionUnknown
		return result, failRegisteredGenerationHandoff(p, lease, cursor, evidencefs.ErrLeaseInvalid, "registered-generation-handoff-replay")
	}
	var execution *VerifiedRecoveryExecutionBindings
	if p.registered.policy != nil {
		value, bindErr := bindRecoveryExecution(*p.registered.policy, candidate.verifiedRun.currentDecision, p.registered.decision, p.registered.bindings, p.registered.descriptor, recovery)
		if bindErr != nil {
			result.outcome = evidencefs.AdmissionTransitionUnknown
			return result, failRegisteredGenerationHandoff(p, lease, cursor, bindErr, "registered-generation-handoff-execution")
		}
		execution = &value
	}
	if err := snapshot.Revalidate(ctx); err != nil {
		result.outcome = evidencefs.AdmissionTransitionUnknown
		return result, failRegisteredGenerationHandoff(p, lease, cursor, err, "registered-generation-handoff-terminal")
	}
	ready := &RegisteredGenerationRecoveryReady{
		prior: p, history: p.history, registered: p.registered, candidateBinding: candidate.binding,
		lease: lease, snapshot: snapshot, replay: renewedReplay, generation: p.registered.descriptor.identity,
		cursor: cursor, recovery: recovery, executionBindings: execution,
		snapshotIdentity: identity, consumed: &atomic.Bool{},
	}
	ready.self = ready
	ready.binding = &registeredGenerationRecoveryReadyBinding{
		ready: ready, prior: p, history: p.history, registered: p.registered,
		candidateBinding: candidate.binding, lease: lease, snapshot: snapshot, replay: renewedReplay,
	}
	ready.binding.canonical = registeredGenerationRecoveryReadyDigest(ready)
	registeredGenerationRecoveryReadyRegistry.Store(ready, registeredGenerationRecoveryReadyRegistryRecord{
		ready: ready, binding: ready.binding, prior: p, history: p.history, registered: p.registered,
		candidateBinding: candidate.binding, lease: lease, snapshot: snapshot, replay: renewedReplay,
		cursorValid: cursor.valid, oldCursorValid: p.registered.replay.cursor.valid,
		handoffConsumed: p.registered.handoffConsumed, readyConsumed: ready.consumed,
		runtimeBinding: p.registered.runtimeReceipt.binding, recoveryBinding: p.registered.recoveryReceipt.binding,
		sourceCanonical: p.registered.canonical, sourceReplayCanonical: p.registered.replay.canonical,
		readyReplayCanonical: renewedReplay.canonical, canonical: ready.binding.canonical,
	})
	p.registered.replay.cursor.valid.Store(false)
	if !validRegisteredGenerationRecoveryReady(ready, candidate) {
		registeredGenerationRecoveryReadyRegistry.Delete(ready)
		result.outcome = evidencefs.AdmissionTransitionUnknown
		return result, failRegisteredGenerationHandoff(p, lease, cursor, evidencefs.ErrLeaseInvalid, "registered-generation-handoff-seal")
	}
	registeredGenerationHandoffPermitRegistry.Delete(p)
	result.next = ready
	return result, nil
}

func registeredGenerationHandoffPermitDigest(permit *RegisteredGenerationHandoffPermit) [32]byte {
	if permit == nil || permit.self != permit || permit.history == nil || permit.registered == nil || permit.registered.replay == nil || permit.candidateBinding == nil || permit.inventory == nil || permit.mutation == nil || permit.history.binding == nil || permit.registered.canonical == ([32]byte{}) || permit.registered.replay.canonical == ([32]byte{}) || permit.journal.Validate() != nil {
		return [32]byte{}
	}
	h := sha256.New()
	h.Write([]byte("cloud-agents-platform-registered-generation-handoff-permit/v1\x00"))
	h.Write(permit.history.binding.canonical[:])
	h.Write(permit.registered.canonical[:])
	h.Write(permit.registered.replay.canonical[:])
	h.Write(permit.candidateBinding.canonical[:])
	h.Write(permit.target[:])
	writeAdmissionString(h, permit.journal.String())
	writeAdmissionUint(h, permit.revision)
	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result
}

func registeredGenerationHandoffCandidateDigest(permit *RegisteredGenerationHandoffPermit) [32]byte {
	if permit == nil || permit.binding == nil || permit.binding.canonical == ([32]byte{}) {
		return [32]byte{}
	}
	h := sha256.New()
	h.Write([]byte("cloud-agents-platform-registered-generation-handoff-candidate/v1\x00"))
	h.Write(permit.binding.canonical[:])
	writeAdmissionUint(h, permit.revision)
	writeAdmissionString(h, permit.journal.String())
	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result
}

func validRegisteredGenerationHandoffPermit(permit *RegisteredGenerationHandoffPermit, candidate OwnedCurrentCandidate) bool {
	if permit == nil || permit.self != permit || permit.binding == nil || permit.binding.permit != permit || permit.history == nil || permit.registered == nil || permit.registered.replay == nil || permit.candidateBinding != candidate.binding || permit.inventory == nil || permit.mutation == nil || permit.binding.history != permit.history || permit.binding.registered != permit.registered || permit.binding.candidateBinding != candidate.binding || permit.binding.inventory != permit.inventory || permit.binding.mutation != permit.mutation || permit.consumed == nil || permit.consumed.Load() || !validOwnedCurrentCandidate(candidate) || permit.history.targetGeneration != permit.registered || permit.history.inventory != permit.inventory || permit.history.candidateBinding != candidate.binding || permit.registered.handoffConsumed == nil || !permit.registered.handoffConsumed.Load() || !validVerifiedAdmissionRegisteredGenerationFacts(permit.registered, candidate.verifiedRun.currentDecision) || permit.target != permit.history.target || permit.target != digestRaw(permit.registered.descriptor.identity.executionLineageDigest) || permit.journal != permit.registered.descriptor.identity.journalIdentityDigest || permit.revision != permit.history.revision || !permit.mutation.ValidFor(permit.inventory) || permit.binding.canonical == ([32]byte{}) || permit.binding.canonical != registeredGenerationHandoffPermitDigest(permit) {
		return false
	}
	registeredHistory, historyOK := verifiedAdmissionHistoryRegistry.Load(permit.history.binding)
	if !historyOK || registeredHistory != permit.history.binding.canonical || permit.history.binding.canonical != admissionHistoryDigest(permit.history) {
		return false
	}
	registered, ok := registeredGenerationHandoffPermitRegistry.Load(permit)
	record, recordOK := registered.(registeredGenerationHandoffPermitRegistryRecord)
	return ok && recordOK && record.permit == permit && record.binding == permit.binding && record.history == permit.history && record.registered == permit.registered && record.candidateBinding == candidate.binding && record.inventory == permit.inventory && record.mutation == permit.mutation && record.handoffConsumed == permit.registered.handoffConsumed && record.permitConsumed == permit.consumed && record.oldCursorValid == permit.registered.replay.cursor.valid && record.runtimeBinding == permit.registered.runtimeReceipt.binding && record.recoveryBinding == permit.registered.recoveryReceipt.binding && record.sourceCanonical == permit.registered.canonical && record.replayCanonical == permit.registered.replay.canonical && record.canonical == permit.binding.canonical
}

func registeredGenerationSnapshotIdentity(snapshot *evidencefs.GenerationSnapshot, replay *verifiedAdmissionGenerationReplay) ([32]byte, error) {
	if snapshot == nil || replay == nil {
		return [32]byte{}, evidencefs.ErrLeaseInvalid
	}
	index, err := snapshot.IndexFact()
	if err != nil {
		return [32]byte{}, err
	}
	count, err := snapshot.SegmentCount()
	if err != nil || uint64(count) != uint64(len(replay.segmentFacts)) {
		if err == nil {
			err = evidencefs.ErrCorrupt
		}
		return [32]byte{}, err
	}
	segments := make([]evidencefs.GenerationFileFact, count)
	for ordinal := uint32(0); ordinal < count; ordinal++ {
		segments[ordinal], err = snapshot.SegmentFact(ordinal)
		if err != nil {
			return [32]byte{}, err
		}
	}
	if !registeredGenerationFileFactsMatch(replay, index, segments) {
		return [32]byte{}, evidencefs.ErrCorrupt
	}
	identity, err := snapshot.IdentityDigest()
	if err != nil || identity == ([32]byte{}) {
		if err == nil {
			err = evidencefs.ErrCorrupt
		}
		return [32]byte{}, err
	}
	return identity, nil
}

func registeredGenerationFileFactsMatch(replay *verifiedAdmissionGenerationReplay, index evidencefs.GenerationFileFact, segments []evidencefs.GenerationFileFact) bool {
	if replay == nil || index != replay.indexFact || len(segments) != len(replay.segmentFacts) {
		return false
	}
	for ordinal := range segments {
		if segments[ordinal] != replay.segmentFacts[ordinal] || segments[ordinal].Ordinal != uint32(ordinal) {
			return false
		}
	}
	return true
}

func registeredGenerationRecoveryReadyDigest(ready *RegisteredGenerationRecoveryReady) [32]byte {
	if ready == nil || ready.self != ready || ready.prior == nil || ready.prior.binding == nil || ready.history == nil || ready.history.binding == nil || ready.registered == nil || ready.registered.replay == nil || ready.replay == nil || ready.candidateBinding == nil || ready.lease == nil || ready.snapshot == nil || ready.snapshotIdentity == ([32]byte{}) || !ready.cursor.Valid() || !validRecoverySnapshotForJournal(ready.recovery, ready.generation, ready.cursor) || !validVerifiedAdmissionGenerationReplay(ready.replay, ready.generation) {
		return [32]byte{}
	}
	h := sha256.New()
	h.Write([]byte("cloud-agents-platform-registered-generation-recovery-ready/v1\x00"))
	h.Write(ready.prior.binding.canonical[:])
	h.Write(ready.history.binding.canonical[:])
	h.Write(ready.registered.canonical[:])
	h.Write(ready.registered.replay.canonical[:])
	h.Write(ready.replay.canonical[:])
	h.Write(ready.candidateBinding.canonical[:])
	h.Write(ready.snapshotIdentity[:])
	writeGenerationJournalCursor(h, ready.cursor)
	recovery := generationJournalRecoveryDigest(ready.recovery)
	schema := generationJournalSchemaDigest(ready.replay.schema, ready.generation)
	if recovery == ([32]byte{}) || schema == ([32]byte{}) {
		return [32]byte{}
	}
	h.Write(recovery[:])
	h.Write(schema[:])
	if ready.executionBindings == nil {
		h.Write([]byte{0})
	} else {
		h.Write([]byte{1})
		writeAdmissionString(h, ready.executionBindings.digest.String())
	}
	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result
}

func validRegisteredGenerationRecoveryReady(ready *RegisteredGenerationRecoveryReady, candidate OwnedCurrentCandidate) bool {
	if ready == nil || ready.self != ready || ready.binding == nil || ready.binding.ready != ready || ready.prior == nil || ready.history == nil || ready.registered == nil || ready.registered.replay == nil || ready.registered.bundle == nil || ready.replay == nil || ready.candidateBinding != candidate.binding || ready.lease == nil || ready.snapshot == nil || ready.binding.prior != ready.prior || ready.binding.history != ready.history || ready.binding.registered != ready.registered || ready.binding.candidateBinding != candidate.binding || ready.binding.lease != ready.lease || ready.binding.snapshot != ready.snapshot || ready.binding.replay != ready.replay || ready.consumed == nil || ready.consumed.Load() || !validOwnedCurrentCandidate(candidate) || ready.prior.consumed == nil || !ready.prior.consumed.Load() || ready.prior.history != ready.history || ready.prior.registered != ready.registered || ready.history.targetGeneration != ready.registered || ready.registered.handoffConsumed == nil || !ready.registered.handoffConsumed.Load() || ready.registered.replay.cursor.valid == nil || ready.registered.replay.cursor.valid.Load() || ready.generation.owner != candidate.owner || !sameGenerationIdentity(ready.generation, ready.registered.descriptor.identity) || !ready.lease.Active() || !ready.lease.OwnsSnapshot(ready.snapshot) || !ready.cursor.Valid() || !sameGenerationIdentity(ready.cursor.generation, ready.generation) || !validRecoverySnapshotForJournal(ready.recovery, ready.generation, ready.cursor) || !validVerifiedAdmissionGenerationReplay(ready.replay, ready.generation) || !sameCursorIdentity(ready.replay.cursor, ready.cursor) || ready.replay.recovery == nil || generationJournalRecoveryDigest(ready.replay.recovery) != generationJournalRecoveryDigest(ready.recovery) || !validRegisteredRuntimeReceipt(ready.registered.runtimeReceipt, candidate.owner, ready.registered.descriptor.header.OuterArtifactDigest, ready.registered.descriptor.header.OuterArtifactSizeBytes) || !validRegisteredDecisionRecoveryReceipt(ready.registered.recoveryReceipt, candidate.owner, ready.registered.descriptor.header.DecisionRecoveryArtifactSHA256, ready.registered.descriptor.header.DecisionRecoveryArtifactSizeBytes) || !registeredReceiptsSameStore(ready.registered.runtimeReceipt, ready.registered.recoveryReceipt) || !validRegisteredGenerationExecutionBindings(ready, candidate) || ready.binding.canonical == ([32]byte{}) || ready.binding.canonical != registeredGenerationRecoveryReadyDigest(ready) {
		return false
	}
	identity, err := registeredGenerationSnapshotIdentity(ready.snapshot, ready.replay)
	if err != nil || identity != ready.snapshotIdentity {
		return false
	}
	registeredHistory, historyOK := verifiedAdmissionHistoryRegistry.Load(ready.history.binding)
	if !historyOK || registeredHistory != ready.history.binding.canonical || ready.history.binding.canonical != admissionHistoryDigest(ready.history) || ready.registered.canonical != verifiedAdmissionRegisteredGenerationDigest(ready.registered) {
		return false
	}
	registered, ok := registeredGenerationRecoveryReadyRegistry.Load(ready)
	record, recordOK := registered.(registeredGenerationRecoveryReadyRegistryRecord)
	if _, _, err := ready.registered.bundle.ownedInputs.copyVerified(); err != nil || !validRegisteredDecisionRecoveryArtifact(ready.registered.decision, ready.registered.bindings, ready.registered.descriptor, ready.registered.recoveryArtifact) {
		return false
	}
	return ok && recordOK && record.ready == ready && record.binding == ready.binding && record.prior == ready.prior && record.history == ready.history && record.registered == ready.registered && record.candidateBinding == candidate.binding && record.lease == ready.lease && record.snapshot == ready.snapshot && record.replay == ready.replay && record.cursorValid == ready.cursor.valid && record.handoffConsumed == ready.registered.handoffConsumed && record.readyConsumed == ready.consumed && record.oldCursorValid == ready.registered.replay.cursor.valid && record.runtimeBinding == ready.registered.runtimeReceipt.binding && record.recoveryBinding == ready.registered.recoveryReceipt.binding && record.sourceCanonical == ready.registered.canonical && record.sourceReplayCanonical == ready.registered.replay.canonical && record.readyReplayCanonical == ready.replay.canonical && record.canonical == ready.binding.canonical
}

func validRegisteredGenerationExecutionBindings(ready *RegisteredGenerationRecoveryReady, candidate OwnedCurrentCandidate) bool {
	if ready == nil || ready.registered == nil || !validOwnedCurrentCandidate(candidate) {
		return false
	}
	registered := ready.registered
	current := candidate.verifiedRun.currentDecision
	if registered.policy == nil {
		return ready.executionBindings == nil && registered.decision.digest == current.digest && registered.decision.owner == current.owner && registered.decision.capability.owner == current.owner && registered.decision.decision.exactlyMatches(current.decision) && validOwnedCurrentDecision(registered.decision, registered.bindings)
	}
	if ready.executionBindings == nil {
		return false
	}
	expected, err := bindRecoveryExecution(*registered.policy, current, registered.decision, registered.bindings, registered.descriptor, ready.recovery)
	if err != nil {
		return false
	}
	actual := ready.executionBindings
	return actual.owner == expected.owner && actual.session == expected.session && sameGenerationIdentity(actual.generation, expected.generation) && actual.tailDigest == expected.tailDigest && actual.digest == expected.digest && canonicalEqual(actual.policy, expected.policy) && canonicalEqual(actual.subject, expected.subject) && validRecoverySnapshotForJournal(actual.snapshot, ready.generation, ready.cursor)
}

func failRegisteredGenerationHandoff(permit *RegisteredGenerationHandoffPermit, lease *evidencefs.GenerationLease, cursor JournalCursor, cause error, operation string) error {
	if cursor.valid != nil {
		cursor.valid.Store(false)
	}
	if permit != nil {
		registeredGenerationHandoffPermitRegistry.Delete(permit)
		if permit.history != nil && permit.history.binding != nil {
			verifiedAdmissionHistoryRegistry.Delete(permit.history.binding)
		}
		if permit.registered != nil {
			revokeVerifiedAdmissionRegisteredGeneration(permit.registered)
		}
	}
	if lease != nil {
		if cleanupErr := lease.Close(); cleanupErr != nil {
			return mapEvidenceAdmissionError(cleanupErr, operation+"-cleanup")
		}
	}
	if cause == nil {
		cause = evidencefs.ErrUnknown
	}
	if IsCode(cause, CodeEvidenceJournalCorrupt) || IsCode(cause, CodeEvidenceJournalFailed) || IsCode(cause, CodeEvidenceRecoveryRequired) || IsCode(cause, CodeContextCanceled) || IsCode(cause, CodeDeadlineExceeded) {
		return cause
	}
	return mapEvidenceAdmissionError(cause, operation)
}

// Close releases the retained target locks and revokes the renewed cursor and
// both registered receipt authorities. Cleanup uses only the immutable ready
// registry record, never mutable predecessor fields.
func (r *RegisteredGenerationRecoveryReady) Close() error {
	if r == nil || r.self != r || r.consumed == nil || !r.consumed.CompareAndSwap(false, true) {
		return admissionFailed("registered-generation-recovery-close", "registered generation recovery authority is unavailable", nil)
	}
	registered, ok := registeredGenerationRecoveryReadyRegistry.Load(r)
	record, recordOK := registered.(registeredGenerationRecoveryReadyRegistryRecord)
	if !ok || !recordOK || record.ready != r || record.binding == nil || record.prior == nil || record.history == nil || record.registered == nil || record.lease == nil || record.replay == nil || record.cursorValid == nil || record.canonical == ([32]byte{}) {
		return admissionFailed("registered-generation-recovery-close", "immutable registered generation lease is unavailable", nil)
	}
	registeredGenerationRecoveryReadyRegistry.Delete(r)
	registeredGenerationHandoffPermitRegistry.Delete(record.prior)
	if record.history != nil && record.history.binding != nil {
		verifiedAdmissionHistoryRegistry.Delete(record.history.binding)
	}
	record.cursorValid.Store(false)
	revokeVerifiedAdmissionRegisteredGeneration(record.registered)
	if err := record.lease.Close(); err != nil {
		return mapEvidenceAdmissionError(err, "registered-generation-recovery-close")
	}
	return nil
}
