package migration

import (
	"bytes"
	"context"
	"crypto/sha256"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/evidencefs"
)

// GenerationPrefixActivationTransitionResult is the closed transition from a
// recovered exact segment-0 header to the adjacent GenerationActivated frame.
// Durable returns only the existing registered-generation handoff permit; no
// runtime, cursor, session, runner, or database authority is exposed here.
type GenerationPrefixActivationTransitionResult struct {
	outcome                evidencefs.AdmissionTransitionOutcome
	next                   *RegisteredGenerationHandoffPermit
	candidateDigest        [32]byte
	candidateSequence      uint64
	candidateRevision      uint64
	previousRevision       uint64
	activationRecordDigest Digest
	reservedRecordDigest   Digest
	headerRecordDigest     Digest
}

func (r GenerationPrefixActivationTransitionResult) Outcome() evidencefs.AdmissionTransitionOutcome {
	return r.outcome
}
func (r GenerationPrefixActivationTransitionResult) Next() *RegisteredGenerationHandoffPermit {
	return r.next
}
func (r GenerationPrefixActivationTransitionResult) CandidateKind() string {
	return "generation_activated"
}
func (r GenerationPrefixActivationTransitionResult) CandidateDigest() [32]byte {
	return r.candidateDigest
}
func (r GenerationPrefixActivationTransitionResult) CandidateSequence() uint64 {
	return r.candidateSequence
}
func (r GenerationPrefixActivationTransitionResult) CandidateRevision() uint64 {
	return r.candidateRevision
}
func (r GenerationPrefixActivationTransitionResult) PreviousRevision() uint64 {
	return r.previousRevision
}
func (r GenerationPrefixActivationTransitionResult) ActivationRecordDigest() Digest {
	return r.activationRecordDigest
}
func (r GenerationPrefixActivationTransitionResult) ReservedRecordDigest() Digest {
	return r.reservedRecordDigest
}
func (r GenerationPrefixActivationTransitionResult) HeaderRecordDigest() Digest {
	return r.headerRecordDigest
}

// AppendGenerationActivated consumes the recovered-header one-shot, appends
// the byte-exact adjacent activation frame, reruns ALL-history verification on
// the new revision, and returns the already-reviewed retained-lock handoff
// permit. Only a genuine pre-mutation context failure preserves this permit.
func (p *RecoveredHeaderDurablePermit) AppendGenerationActivated(ctx context.Context, candidate OwnedCurrentCandidate) (GenerationPrefixActivationTransitionResult, error) {
	pre := GenerationPrefixActivationTransitionResult{outcome: evidencefs.AdmissionTransitionPreMutationFailure, candidateSequence: 8}
	if p == nil || !validRecoveredHeaderDurablePermit(p, candidate) {
		return pre, fail(CodeEvidenceRecoveryRequired, "generation-prefix-activate", "recovered header authority is unavailable", nil)
	}
	if err := contextAdmissionError(ctx); err != nil {
		return pre, err
	}
	activatedFrame, activatedBytes, err := buildSuccessorActivatedFrame(p.input.reservedFrame, p.input.headerFrame)
	if err != nil {
		failRecoveredHeaderDurablePermit(p)
		return pre, fail(CodeEvidenceRecoveryRequired, "generation-prefix-activate", "generation activation frame cannot be constructed", nil)
	}
	activatedBytes = append([]byte(nil), activatedBytes...)
	pre.previousRevision = p.revision
	pre.candidateRevision = p.revision + 1
	pre.candidateDigest = sha256.Sum256(activatedBytes)
	pre.activationRecordDigest = activatedFrame.RecordDigest
	pre.reservedRecordDigest = p.input.reservedFrame.RecordDigest
	pre.headerRecordDigest = p.input.headerFrame.RecordDigest
	reserved := p.input.reservedFrame.Record.Reserved
	if reserved == nil || pre.candidateDigest == ([32]byte{}) || pre.activationRecordDigest.Validate() != nil || pre.reservedRecordDigest != p.input.indexTail || pre.headerRecordDigest != reserved.ExpectedSegment0HeaderDigest {
		failRecoveredHeaderDurablePermit(p)
		return pre, fail(CodeEvidenceRecoveryRequired, "generation-prefix-activate", "generation activation identity is invalid", nil)
	}
	if err := p.inventory.Revalidate(ctx); err != nil {
		mapped := mapEvidenceAdmissionError(err, "generation-prefix-activate-revalidate")
		if !generationPrefixContextError(mapped) {
			failRecoveredHeaderDurablePermit(p)
		}
		return pre, mapped
	}
	if err := validateRecoveredHeaderInventory(ctx, p.inventory, p.input); err != nil {
		if !generationPrefixContextError(err) {
			failRecoveredHeaderDurablePermit(p)
		}
		return pre, err
	}
	prefix, err := readSuccessorInventoryIndex(ctx, p.inventory, p.input.target, p.input.indexRecords, p.input.indexTail, p.input.indexDigest, p.input.indexSize, "generation-prefix-activate-index")
	if err != nil {
		if !generationPrefixContextError(err) {
			failRecoveredHeaderDurablePermit(p)
		}
		return pre, err
	}
	journalCount, err := validateSuccessorJournalInventory(ctx, p.inventory, p.input.target, p.input.journal, p.input.headerBytes, 0, true, "generation-prefix-activate-journal")
	if err != nil {
		if !generationPrefixContextError(err) {
			failRecoveredHeaderDurablePermit(p)
		}
		return pre, err
	}
	if journalCount == 0 || activatedFrame.Sequence != p.input.indexRecords || activatedFrame.PreviousRecordDigest == nil || *activatedFrame.PreviousRecordDigest != p.input.indexTail || !bytes.Equal(activatedBytes, mustEncodeLineageFrame(activatedFrame)) || !validRecoveredHeaderDurablePermit(p, candidate) {
		failRecoveredHeaderDurablePermit(p)
		return pre, fail(CodeEvidenceRecoveryRequired, "generation-prefix-activate", "recovered header changed before activation", nil)
	}
	if !p.consumed.CompareAndSwap(false, true) {
		return pre, fail(CodeEvidenceRecoveryRequired, "generation-prefix-activate", "recovered header authority is consumed", nil)
	}
	fsResult, transitionErr := p.mutation.AppendTargetIndex(ctx, p.inventory, activatedBytes)
	result := GenerationPrefixActivationTransitionResult{
		outcome: fsResult.Outcome(), candidateDigest: fsResult.CandidateDigest(), candidateSequence: 8,
		candidateRevision: fsResult.CandidateRevision(), previousRevision: fsResult.PreviousRevision(),
		activationRecordDigest: pre.activationRecordDigest, reservedRecordDigest: pre.reservedRecordDigest,
		headerRecordDigest: pre.headerRecordDigest,
	}
	if fsResult.Outcome() != evidencefs.AdmissionTransitionDurable {
		mapped := mapAdmissionMutationError(transitionErr, "generation-prefix-activate")
		if mapped == nil {
			mapped = admissionFailed("generation-prefix-activate", "filesystem transition returned no durable authority", nil)
		}
		if fsResult.Outcome() == evidencefs.AdmissionTransitionPreMutationFailure && generationPrefixContextError(mapped) {
			p.consumed.CompareAndSwap(true, false)
		} else {
			failRecoveredHeaderDurablePermit(p)
		}
		return result, mapped
	}
	var freshHistory *VerifiedAdmissionHistory
	var handoff *RegisteredGenerationHandoffPermit
	postFailure := func(suffix string) (GenerationPrefixActivationTransitionResult, error) {
		if handoff != nil {
			registeredGenerationHandoffPermitRegistry.Delete(handoff)
		}
		revokeGenerationPrefixFreshHistory(freshHistory)
		_ = fsResult.Invalidate()
		retireRecoveredHeaderDurablePermit(p)
		result.outcome, result.next = evidencefs.AdmissionTransitionUnknown, nil
		return result, admissionPostMutationFailure("generation-prefix-activate" + suffix)
	}
	if transitionErr != nil || fsResult.CandidateKind() != "target_index_append" || fsResult.CandidateSequence() != 0 || fsResult.CandidateDigest() != pre.candidateDigest || fsResult.PreviousRevision() != pre.previousRevision || fsResult.CandidateRevision() != pre.candidateRevision || fsResult.Inventory() == nil {
		return postFailure("-result")
	}
	nextInventory := fsResult.Inventory()
	revision, revisionErr := nextInventory.Revision()
	target, targetErr := nextInventory.Target()
	fullSet, fullSetErr := nextInventory.FullSetDigest()
	if revisionErr != nil || targetErr != nil || fullSetErr != nil || revision != pre.candidateRevision || target != p.input.target || fullSet == ([32]byte{}) || fullSet == p.fullSet {
		return postFailure("-boundary")
	}
	verified, verifyErr := validateSuccessorIndexAppend(ctx, nextInventory, target, prefix.raw, activatedBytes, p.input.indexRecords+1, pre.activationRecordDigest)
	if verifyErr != nil || verified.digest == ([32]byte{}) || uint64(len(verified.frames)) != p.input.indexRecords+1 {
		return postFailure("-index")
	}
	nextJournalCount, journalErr := validateSuccessorJournalInventory(ctx, nextInventory, target, p.input.journal, p.input.headerBytes, journalCount, true, "generation-prefix-activate-inventory")
	if journalErr != nil || nextJournalCount != journalCount {
		return postFailure("-inventory")
	}
	freshHistory, err = bindVerifiedAdmissionHistory(ctx, nextInventory, candidate)
	if err != nil || !recoveredActivatedHistoryExact(freshHistory, p, activatedFrame, revision, fullSet, candidate) {
		return postFailure("-history")
	}
	handoff, err = bindRegisteredGenerationHandoff(ctx, freshHistory, candidate)
	if err != nil || !validRegisteredGenerationHandoffPermit(handoff, candidate) {
		return postFailure("-handoff")
	}
	if err := nextInventory.Revalidate(ctx); err != nil || !validRegisteredGenerationHandoffPermit(handoff, candidate) {
		return postFailure("-terminal")
	}
	retireRecoveredHeaderDurablePermit(p)
	result.next = handoff
	return result, nil
}

func recoveredActivatedHistoryExact(history *VerifiedAdmissionHistory, prior *RecoveredHeaderDurablePermit, activated LineageIndexFrame, revision uint64, fullSet [32]byte, candidate OwnedCurrentCandidate) bool {
	if history == nil || prior == nil || prior.registered == nil || activated.Record.Activated == nil || !validVerifiedAdmissionHistory(history, candidate) || history.inventory == prior.inventory || history.target != prior.input.target || history.fullSet != fullSet || history.revision != revision || history.targetState != admissionLineageActiveInitial || history.targetIndexRecords != prior.input.indexRecords+1 || history.targetIndexTail != activated.RecordDigest || history.targetGeneration == nil || history.targetGeneration.replay == nil {
		return false
	}
	fresh := history.targetGeneration
	old := prior.registered
	if !recoveredGenerationRegistrationFactsExact(fresh, old, prior.input.headerFrame.RecordDigest) || fresh.runtimeReceipt.registeredPublication == nil || old.runtimeReceipt.registeredPublication == nil || fresh.recoveryReceipt.registeredPublication == nil || old.recoveryReceipt.registeredPublication == nil || !fresh.runtimeReceipt.registeredPublication.SameObject(old.runtimeReceipt.registeredPublication) || !fresh.recoveryReceipt.registeredPublication.SameObject(old.recoveryReceipt.registeredPublication) {
		return false
	}
	replay := fresh.replay
	return replay.cursor.lineageIndexNextSequence == history.targetIndexRecords && replay.cursor.lineageIndexPreviousRecordDigest == activated.RecordDigest && replay.cursor.segmentIndex == 0 && replay.cursor.nextSequence == 1 && replay.cursor.previousRecordDigest != nil && *replay.cursor.previousRecordDigest == prior.input.headerFrame.RecordDigest && replay.cursor.latestCheckpointRecordDigest == nil && replay.journalRecords == 1 && len(replay.segmentFacts) == 1 && len(replay.segmentRecords) == 1 && replay.segmentRecords[0] == 1
}

func recoveredGenerationRegistrationFactsExact(fresh, old *verifiedAdmissionRegisteredGeneration, headerTail Digest) bool {
	if fresh == nil || old == nil || fresh.replay == nil || old.replay != nil || headerTail.Validate() != nil || fresh.descriptor.replayTailDigest != headerTail || old.descriptor.replayTailDigest != headerTail || !sameGenerationIdentity(fresh.descriptor.identity, old.descriptor.identity) || !canonicalEqual(fresh.descriptor.header, old.descriptor.header) || fresh.descriptor.recoveryArtifactDigest != old.descriptor.recoveryArtifactDigest || fresh.descriptor.recoveryArtifactSize != old.descriptor.recoveryArtifactSize || fresh.decision.owner != old.decision.owner || fresh.decision.digest != old.decision.digest || !fresh.decision.decision.exactlyMatches(old.decision.decision) || fresh.bindings.expectedCanonical != old.bindings.expectedCanonical || fresh.bundle == nil || old.bundle == nil || fresh.bundle.ownedInputs.canonical != old.bundle.ownedInputs.canonical || fresh.recoveryArtifact.digest != old.recoveryArtifact.digest || fresh.recoveryArtifact.sizeBytes != old.recoveryArtifact.sizeBytes || fresh.runtimeReceipt.digest != old.runtimeReceipt.digest || fresh.runtimeReceipt.sizeBytes != old.runtimeReceipt.sizeBytes || fresh.recoveryReceipt.digest != old.recoveryReceipt.digest || fresh.recoveryReceipt.sizeBytes != old.recoveryReceipt.sizeBytes {
		return false
	}
	if (fresh.policy == nil) != (old.policy == nil) {
		return false
	}
	return fresh.policy == nil || fresh.policy.digest == old.policy.digest && canonicalEqual(fresh.policy.subject, old.policy.subject)
}

func failRecoveredHeaderDurablePermit(permit *RecoveredHeaderDurablePermit) {
	if permit == nil {
		return
	}
	_ = permit.fsResult.Invalidate()
	retireRecoveredHeaderDurablePermit(permit)
}

func retireRecoveredHeaderDurablePermit(permit *RecoveredHeaderDurablePermit) {
	if permit == nil {
		return
	}
	recoveredHeaderDurablePermitRegistry.Delete(permit)
	if permit.prior != nil {
		generationPrefixRecoveryPermitRegistry.Delete(permit.prior)
	}
	if permit.history != nil && permit.history.binding != nil {
		verifiedAdmissionHistoryRegistry.Delete(permit.history.binding)
	}
	revokeVerifiedAdmissionRegisteredGeneration(permit.registered)
}

func revokeGenerationPrefixFreshHistory(history *VerifiedAdmissionHistory) {
	if history == nil {
		return
	}
	if history.binding != nil {
		verifiedAdmissionHistoryRegistry.Delete(history.binding)
	}
	revokeVerifiedAdmissionRegisteredGeneration(history.targetGeneration)
}
