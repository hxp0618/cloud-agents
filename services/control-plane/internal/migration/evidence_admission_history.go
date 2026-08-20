package migration

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"sync"
	"sync/atomic"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/evidencefs"
)

// VerifiedAdmissionHistory is the first authority minted after ALL-history
// filesystem/C3 replay and same-verifier recovery. It does not contain a
// planned GenerationReserved and cannot authorize a filesystem mutation.
type VerifiedAdmissionHistory struct {
	owner               *recoveryVerifierOwner
	candidateBinding    *verifiedEvidenceRunBinding
	inventory           *evidencefs.AdmissionInventory
	revision            uint64
	target, fullSet     [32]byte
	transcriptCanonical [32]byte
	targetState         admissionReplayLineageState
	targetHeader        admissionReplayLineageHeader
	targetIndexRecords  uint64
	targetIndexTail     Digest
	currentFacts        *admissionHistoricalVerificationFacts
	quotaProfile        string
	rootFacts           rootQuotaUsageFacts
	reservation         evidenceQuotaReservation
	quotaAdmission      rootQuotaAdmission
	targetGeneration    *verifiedAdmissionRegisteredGeneration
	binding             *verifiedAdmissionHistoryBinding
}

type verifiedAdmissionRegisteredGeneration struct {
	descriptor       GenerationDescriptor
	decision         OwnedVerifiedDecision
	bindings         RunnerProjectionBindings
	policy           *VerifiedHistoricalRecoveryPolicy
	bundle           *RuntimeBundle
	recoveryArtifact VerifiedDecisionRecoveryArtifact
	runtimeReceipt   VerifiedContentReceipt
	recoveryReceipt  VerifiedDecisionRecoveryReceipt
	replay           *verifiedAdmissionGenerationReplay
	handoffConsumed  *atomic.Bool
	canonical        [32]byte
}

// admissionVerifiedGenerationEvidence is the owned, non-authorizing result of
// same-verifier verification for one stored generation. Registered
// publications and replay authority are minted only by the narrow retention
// binder after the caller has selected an exact lifecycle purpose.
type admissionVerifiedGenerationEvidence struct {
	descriptor       GenerationDescriptor
	decision         OwnedVerifiedDecision
	bindings         RunnerProjectionBindings
	policy           *VerifiedHistoricalRecoveryPolicy
	bundle           *RuntimeBundle
	facts            *admissionHistoricalVerificationFacts
	runtimeArtifact  VerifiedRuntimeArtifact
	recoveryArtifact VerifiedDecisionRecoveryArtifact
	runtimeView      *evidencefs.AdmissionObjectView
	recoveryView     *evidencefs.AdmissionObjectView
}

type admissionGenerationRetention uint8

const (
	admissionRetainActiveGeneration admissionGenerationRetention = iota + 1
	admissionRetainSupersededSource
	admissionRetainMaterializedSupersededSource
)

type verifiedAdmissionHistoryBinding struct {
	owner            *recoveryVerifierOwner
	candidateBinding *verifiedEvidenceRunBinding
	inventory        *evidencefs.AdmissionInventory
	history          *VerifiedAdmissionHistory
	canonical        [32]byte
}

var verifiedAdmissionHistoryRegistry sync.Map

func bindVerifiedAdmissionHistory(ctx context.Context, inventory *evidencefs.AdmissionInventory, candidate OwnedCurrentCandidate) (*VerifiedAdmissionHistory, error) {
	if inventory == nil || !validOwnedCurrentCandidate(candidate) {
		return nil, fail(CodeEvidenceRecoveryRequired, "admission-history", "current inventory or verifier authority is unavailable", nil)
	}
	target, err := inventory.Target()
	if err != nil {
		return nil, mapEvidenceAdmissionError(err, "admission-history-target")
	}
	transcript, err := replayAdmissionInventory(ctx, inventory, target)
	if err != nil {
		return nil, err
	}
	current := candidate.verifiedRun.currentDecision
	currentBindings, err := current.decision.runnerProjectionBindings()
	if err != nil || !validOwnedCurrentDecision(current, currentBindings) {
		return nil, fail(CodeEvidenceRecoveryRequired, "admission-history", "current verifier authority is unavailable", err)
	}
	currentBundle, err := LoadRuntimeBundle(candidate.runtimeArtifact.bytes, current.decision)
	if err != nil {
		return nil, fail(CodeEvidenceRecoveryRequired, "admission-history", "current runtime bundle is unavailable", nil)
	}
	currentFacts, err := buildHistoricalVerificationFacts(currentBundle, currentBindings)
	if err != nil {
		return nil, fail(CodeEvidenceRecoveryRequired, "admission-history", "current verification facts are unavailable", nil)
	}
	objects, err := admissionHistoryObjectViews(inventory, transcript)
	if err != nil {
		return nil, err
	}
	recoveryRequired := len(transcript.recoveryNeeds) != 0
	var firstCorrupt, fatal error
	var targetGeneration *verifiedAdmissionRegisteredGeneration
	retainTargetGeneration := false
	recordHistoryError := func(value error) {
		if value == nil {
			return
		}
		if IsCode(value, CodeEvidenceJournalCorrupt) {
			if firstCorrupt == nil {
				firstCorrupt = value
			}
			return
		}
		if IsCode(value, CodeEvidenceRecoveryRequired) {
			recoveryRequired = true
			return
		}
		fatal = value
	}
	defer func() {
		if !retainTargetGeneration {
			revokeVerifiedAdmissionRegisteredGeneration(targetGeneration)
		}
	}()
	for lineageIndex := range transcript.lineages {
		lineage := &transcript.lineages[lineageIndex]
		for generationIndex := range lineage.generations {
			generation := &lineage.generations[generationIndex]
			needsRecovery, generationEvidence, verifyErr := verifyAdmissionHistoryGenerationEvidence(ctx, *lineage, generation, objects, current, currentFacts, candidate)
			recoveryRequired = recoveryRequired || needsRecovery
			recordHistoryError(verifyErr)
			if fatal != nil {
				break
			}

			var plannedEvidence *admissionVerifiedGenerationEvidence
			if generation.plannedSuccessor != nil {
				plannedNeedsRecovery, evidence, plannedErr := verifyAdmissionHistoryGenerationEvidence(ctx, *lineage, generation.plannedSuccessor, objects, current, currentFacts, candidate)
				recoveryRequired = recoveryRequired || plannedNeedsRecovery
				plannedEvidence = evidence
				recordHistoryError(plannedErr)
				if fatal != nil {
					break
				}
			}

			retainRegistered := lineage.id == target && generationIndex == len(lineage.generations)-1 && generation.supersessionRecordDigest == nil && generationEvidence != nil
			if retainRegistered {
				registered, retainErr := retainAdmissionHistoryGeneration(ctx, *lineage, generation, generationEvidence, current, admissionRetainActiveGeneration)
				recordHistoryError(retainErr)
				if retainErr == nil {
					if targetGeneration != nil {
						revokeVerifiedAdmissionRegisteredGeneration(registered)
						recordHistoryError(admissionCorrupt("admission-history", "target generation authority is duplicated", nil))
					} else {
						targetGeneration = registered
					}
				}
				if fatal != nil {
					break
				}
			}

			hasSuperseded := generation.supersessionRecordDigest != nil
			hasPlanned := generation.plannedSuccessor != nil
			if hasSuperseded != hasPlanned {
				recordHistoryError(admissionCorrupt("admission-history", "historical supersession boundary is incomplete", nil))
				continue
			}
			if !hasSuperseded {
				continue
			}
			if generationIndex+1 >= len(lineage.generations) {
				// Superseded is durable but its byte-exact adjacent reservation is
				// not. Only the dedicated adjacent-reserve recovery path may resume.
				recoveryRequired = true
				continue
			}
			actualSuccessor := &lineage.generations[generationIndex+1]
			if !admissionSuccessorReservationMatches(lineage.id, generation.plannedSuccessor, actualSuccessor) {
				recordHistoryError(admissionCorrupt("admission-history", "materialized successor differs from stored supersession", nil))
				continue
			}
			if !materializedAdmissionSuccessorMatches(lineage.id, generation.plannedSuccessor, actualSuccessor) {
				// The adjacent reservation is byte-exact, but its registered
				// generation has not crossed the durable activation barrier yet.
				recoveryRequired = true
				continue
			}
			if generationEvidence == nil || plannedEvidence == nil {
				recoveryRequired = true
				continue
			}
			recordHistoryError(verifyMaterializedHistoricalSupersession(ctx, *lineage, generation, actualSuccessor, generationEvidence, plannedEvidence, current))
			if fatal != nil {
				break
			}
		}
		if fatal != nil {
			break
		}
	}
	if err := inventory.Revalidate(ctx); err != nil {
		return nil, mapEvidenceAdmissionError(err, "admission-history-revalidate")
	}
	if fatal != nil {
		return nil, fatal
	}
	if firstCorrupt != nil {
		return nil, firstCorrupt
	}
	if recoveryRequired {
		return nil, fail(CodeEvidenceRecoveryRequired, "admission-history", "historical admission authority is incomplete", nil)
	}
	rootFacts, err := rootQuotaUsageFactsFromAdmissionTranscript(transcript)
	if err != nil {
		return nil, err
	}
	quotaFacts, err := currentBundle.quotaFactsForAdmission()
	if err != nil {
		return nil, fail(CodeEvidenceRecoveryRequired, "admission-history", "current quota authority is unavailable", nil)
	}
	reservation, err := calculateEvidenceQuotaReservationForFacts(quotaFacts, rootFacts)
	if err != nil {
		return nil, err
	}
	quotaAdmission, err := calculateRootQuotaAdmission(rootFacts, currentBundle, candidate)
	if err != nil {
		return nil, err
	}
	revision, err := inventory.Revision()
	if err != nil {
		return nil, mapEvidenceAdmissionError(err, "admission-history-revision")
	}
	fullSet, err := inventory.FullSetDigest()
	if err != nil {
		return nil, mapEvidenceAdmissionError(err, "admission-history-full-set")
	}
	if revision != transcript.revision || fullSet != transcript.fullSetDigest {
		return nil, admissionFailed("admission-history", "inventory changed after historical verification", nil)
	}
	targetState, targetHeader, targetRecords, targetTail, err := admissionHistoryTargetFacts(transcript)
	if err != nil {
		return nil, err
	}
	requiresTargetGeneration := targetState != "" && targetState != admissionLineageEmpty
	requiresTargetReplay := admissionStateRequiresGenerationReplay(targetState)
	if requiresTargetGeneration != (targetGeneration != nil) || targetGeneration != nil && (digestRaw(targetGeneration.descriptor.identity.executionLineageDigest) != target || requiresTargetReplay != (targetGeneration.replay != nil)) {
		return nil, admissionFailed("admission-history", "target generation authority is incomplete or mismatched", nil)
	}
	history := &VerifiedAdmissionHistory{
		owner: current.owner, candidateBinding: candidate.binding, inventory: inventory, revision: revision,
		target: target, fullSet: fullSet, transcriptCanonical: transcript.canonical,
		targetState: targetState, targetHeader: targetHeader, targetIndexRecords: targetRecords, targetIndexTail: targetTail,
		currentFacts: cloneAdmissionHistoricalVerificationFacts(currentFacts),
		quotaProfile: quotaFacts.lineageQuotaProfile,
		rootFacts:    rootFacts, reservation: reservation, quotaAdmission: quotaAdmission,
		targetGeneration: cloneVerifiedAdmissionRegisteredGeneration(targetGeneration),
	}
	binding := &verifiedAdmissionHistoryBinding{owner: current.owner, candidateBinding: candidate.binding, inventory: inventory, history: history}
	history.binding = binding
	binding.canonical = admissionHistoryDigest(history)
	verifiedAdmissionHistoryRegistry.Store(binding, binding.canonical)
	if !validVerifiedAdmissionHistory(history, candidate) {
		verifiedAdmissionHistoryRegistry.Delete(binding)
		return nil, admissionFailed("admission-history", "admission history authority could not be sealed", nil)
	}
	retainTargetGeneration = true
	return history, nil
}

// verifyMaterializedHistoricalSupersession reconstructs stored A -> B
// verifier authority only long enough to prove that the durable Superseded
// frame and its now-materialized successor still bind to the same verifier and
// exact registered objects. No receipt, replay cursor, or append authority
// escapes this function.
func verifyMaterializedHistoricalSupersession(ctx context.Context, lineage admissionReplayLineage, sourceGeneration, actualSuccessor *admissionReplayGeneration, sourceEvidence, plannedEvidence *admissionVerifiedGenerationEvidence, current OwnedVerifiedDecision) error {
	if !materializedAdmissionSuccessorIsAdjacent(lineage, sourceGeneration, actualSuccessor) || sourceEvidence == nil || plannedEvidence == nil {
		return admissionCorrupt("admission-materialized-supersession", "stored supersession inputs are incomplete", nil)
	}
	source, err := retainMaterializedAdmissionHistoryGeneration(ctx, lineage, sourceGeneration, actualSuccessor, sourceEvidence, current)
	if err != nil {
		return err
	}
	planned, err := retainAdmissionHistoryGeneration(ctx, lineage, sourceGeneration.plannedSuccessor, plannedEvidence, current, admissionRetainActiveGeneration)
	if err != nil {
		revokeVerifiedAdmissionRegisteredGeneration(source)
		return err
	}
	defer revokeVerifiedAdmissionRegisteredGeneration(source)
	defer revokeVerifiedAdmissionRegisteredGeneration(planned)

	superseded, err := expandAdmissionGenerationSuperseded(lineage.id, *sourceGeneration)
	if err != nil {
		return err
	}
	wantSource := generationIdentity{current.owner.token, superseded.ExecutionLineageDigest, superseded.OldJournalIdentityDigest, superseded.OldRunnerProjectionDecisionDigest, superseded.OldSchemaBundleDigest}
	if !sameGenerationIdentity(source.descriptor.identity, wantSource) || superseded.PlannedGenerationReserved == nil || !sameGenerationHeader(planned.descriptor.identity, superseded.PlannedGenerationReserved.PlannedSegment0Header) {
		return admissionCorrupt("admission-materialized-supersession", "registered generations differ from stored supersession", nil)
	}
	authority, receipt, err := bindStoredHistoricalSupersession(ctx, current, source, planned, plannedEvidence.runtimeArtifact, superseded)
	if err != nil {
		return err
	}
	if authority == nil || receipt == nil || receipt.consume(current.owner, authority.digest) != nil {
		return admissionCorrupt("admission-materialized-supersession", "recovered supersession receipt could not be consumed", nil)
	}
	return nil
}

// bindHistoricalSupersessionAdjacentReserveReady performs the same bounded
// ALL-history replay as ordinary admission, but accepts exactly one different
// terminal state: the target lineage ends in a durable Superseded frame whose
// nested successor reservation is not yet present. It mints no normal history
// authority and performs no filesystem mutation.
func bindHistoricalSupersessionAdjacentReserveReady(ctx context.Context, inventory *evidencefs.AdmissionInventory, candidate OwnedCurrentCandidate) (*HistoricalSupersessionAdjacentReserveReady, error) {
	if inventory == nil || !validOwnedCurrentCandidate(candidate) {
		return nil, fail(CodeEvidenceRecoveryRequired, "historical-supersession-plan", "current inventory or verifier authority is unavailable", nil)
	}
	target, err := inventory.Target()
	if err != nil {
		return nil, mapEvidenceAdmissionError(err, "historical-supersession-target")
	}
	if target != digestRaw(candidate.verifiedRun.executionLineageDigest) {
		return nil, fail(CodeEvidenceRecoveryRequired, "historical-supersession-target", "candidate and inventory target lineages differ", nil)
	}
	transcript, err := replayAdmissionInventory(ctx, inventory, target)
	if err != nil {
		return nil, err
	}
	current := candidate.verifiedRun.currentDecision
	currentBindings, err := current.decision.runnerProjectionBindings()
	if err != nil || !validOwnedCurrentDecision(current, currentBindings) {
		return nil, fail(CodeEvidenceRecoveryRequired, "historical-supersession-plan", "current verifier authority is unavailable", nil)
	}
	currentBundle, err := LoadRuntimeBundle(candidate.runtimeArtifact.bytes, current.decision)
	if err != nil {
		return nil, fail(CodeEvidenceRecoveryRequired, "historical-supersession-plan", "current runtime bundle is unavailable", nil)
	}
	currentFacts, err := buildHistoricalVerificationFacts(currentBundle, currentBindings)
	if err != nil {
		return nil, fail(CodeEvidenceRecoveryRequired, "historical-supersession-plan", "current verification facts are unavailable", nil)
	}
	objects, err := admissionHistoryObjectViews(inventory, transcript)
	if err != nil {
		return nil, err
	}
	recoveryRequired := len(transcript.recoveryNeeds) != 0
	var firstCorrupt, fatal error
	var sourceEvidence, plannedEvidence *admissionVerifiedGenerationEvidence
	var sourceGeneration *admissionReplayGeneration
	var targetLineage *admissionReplayLineage
	for lineageIndex := range transcript.lineages {
		lineage := &transcript.lineages[lineageIndex]
		for generationIndex := range lineage.generations {
			generation := &lineage.generations[generationIndex]
			isPendingTarget := lineage.id == target && lineage.state == admissionLineageSuperseded && generationIndex == len(lineage.generations)-1 && generation.supersessionRecordDigest != nil && generation.plannedSuccessor != nil
			generations := []*admissionReplayGeneration{generation}
			if generation.plannedSuccessor != nil {
				generations = append(generations, generation.plannedSuccessor)
			}
			for generationPosition, verifyGeneration := range generations {
				needsRecovery, evidence, verifyErr := verifyAdmissionHistoryGenerationEvidence(ctx, *lineage, verifyGeneration, objects, current, currentFacts, candidate)
				recoveryRequired = recoveryRequired || needsRecovery
				if verifyErr == nil {
					if isPendingTarget && generationPosition == 0 {
						sourceEvidence, sourceGeneration, targetLineage = evidence, generation, lineage
					} else if isPendingTarget && generationPosition == 1 {
						plannedEvidence = evidence
					}
					continue
				}
				if IsCode(verifyErr, CodeEvidenceJournalCorrupt) {
					if firstCorrupt == nil {
						firstCorrupt = verifyErr
					}
					continue
				}
				fatal = verifyErr
				break
			}
			if (generation.supersessionRecordDigest != nil || generation.plannedSuccessor != nil) && !isPendingTarget {
				recoveryRequired = true
			}
			if fatal != nil {
				break
			}
		}
		if fatal != nil {
			break
		}
	}
	if err := inventory.Revalidate(ctx); err != nil {
		return nil, mapEvidenceAdmissionError(err, "historical-supersession-revalidate")
	}
	if fatal != nil {
		return nil, fatal
	}
	if firstCorrupt != nil {
		return nil, firstCorrupt
	}
	if recoveryRequired || targetLineage == nil || sourceGeneration == nil || sourceEvidence == nil || plannedEvidence == nil {
		return nil, fail(CodeEvidenceRecoveryRequired, "historical-supersession-plan", "historical supersession authority is incomplete", nil)
	}
	rootFacts, err := rootQuotaUsageFactsFromAdmissionTranscript(transcript)
	if err != nil {
		return nil, err
	}
	plannedQuotaFacts, err := plannedEvidence.bundle.quotaFactsForAdmission()
	if err != nil {
		return nil, fail(CodeEvidenceRecoveryRequired, "historical-supersession-plan", "planned runtime quota authority is unavailable", nil)
	}
	quotaAdmission, err := calculateRootQuotaAdmissionForArtifacts(rootFacts, plannedQuotaFacts, plannedEvidence.runtimeArtifact, plannedEvidence.recoveryArtifact)
	if err != nil {
		return nil, err
	}
	source, err := retainAdmissionHistoryGeneration(ctx, *targetLineage, sourceGeneration, sourceEvidence, current, admissionRetainSupersededSource)
	if err != nil {
		return nil, err
	}
	planned, err := retainAdmissionHistoryGeneration(ctx, *targetLineage, sourceGeneration.plannedSuccessor, plannedEvidence, current, admissionRetainActiveGeneration)
	if err != nil {
		revokeVerifiedAdmissionRegisteredGeneration(source)
		return nil, err
	}
	retained := false
	defer func() {
		if !retained {
			revokeVerifiedAdmissionRegisteredGeneration(source)
			revokeVerifiedAdmissionRegisteredGeneration(planned)
		}
	}()
	supersededFrame, reservedFrame, headerFrame, supersededBytes, reservedBytes, err := buildHistoricalSupersessionFrames(target, targetLineage.indexRecords, targetLineage.indexTailRecordDigest, *sourceGeneration)
	if err != nil {
		return nil, err
	}
	if !sameGenerationIdentity(source.descriptor.identity, generationIdentity{candidate.owner, supersededFrame.Record.Superseded.ExecutionLineageDigest, supersededFrame.Record.Superseded.OldJournalIdentityDigest, supersededFrame.Record.Superseded.OldRunnerProjectionDecisionDigest, supersededFrame.Record.Superseded.OldSchemaBundleDigest}) || !sameGenerationHeader(planned.descriptor.identity, reservedFrame.Record.Reserved.PlannedSegment0Header) {
		return nil, admissionCorrupt("historical-supersession-plan", "registered generations differ from the stored supersession", nil)
	}
	authority, receipt, err := bindStoredHistoricalSupersession(ctx, current, source, planned, plannedEvidence.runtimeArtifact, *supersededFrame.Record.Superseded)
	if err != nil {
		return nil, err
	}
	if err := inventory.Revalidate(ctx); err != nil {
		return nil, mapEvidenceAdmissionError(err, "historical-supersession-terminal-revalidate")
	}
	revision, revisionErr := inventory.Revision()
	fullSet, fullSetErr := inventory.FullSetDigest()
	currentTarget, targetErr := inventory.Target()
	if revisionErr != nil || fullSetErr != nil || targetErr != nil {
		for _, accessorErr := range []error{revisionErr, fullSetErr, targetErr} {
			if accessorErr != nil {
				return nil, mapEvidenceAdmissionError(accessorErr, "historical-supersession-plan")
			}
		}
	}
	if revision != transcript.revision || fullSet != transcript.fullSetDigest || currentTarget != target || targetLineage.index.size == 0 || targetLineage.index.digest == ([32]byte{}) {
		return nil, admissionFailed("historical-supersession-plan", "inventory changed after historical verification", nil)
	}
	mutation, err := inventory.MutationToken()
	if err != nil {
		return nil, mapEvidenceAdmissionError(err, "historical-supersession-token")
	}
	ready := &HistoricalSupersessionAdjacentReserveReady{
		owner: current.owner, candidateBinding: candidate.binding, inventory: inventory, mutation: mutation,
		revision: revision, target: target, fullSet: fullSet, transcriptCanonical: transcript.canonical,
		indexRecords: targetLineage.indexRecords, indexTail: targetLineage.indexTailRecordDigest,
		indexDigest: targetLineage.index.digest, indexSize: targetLineage.index.size,
		rootFacts: rootFacts, quotaAdmission: quotaAdmission, source: source, planned: planned,
		plannedRuntime: VerifiedRuntimeArtifact{owner: plannedEvidence.runtimeArtifact.owner, bytes: append([]byte(nil), plannedEvidence.runtimeArtifact.bytes...), digest: plannedEvidence.runtimeArtifact.digest, sizeBytes: plannedEvidence.runtimeArtifact.sizeBytes},
		authority:      authority, receipt: receipt,
		supersededFrame: cloneProjectionValue(supersededFrame), reservedFrame: cloneProjectionValue(reservedFrame), headerFrame: cloneProjectionValue(headerFrame),
		supersededFrameBytes: append([]byte(nil), supersededBytes...), reservedFrameBytes: append([]byte(nil), reservedBytes...), consumed: &atomic.Bool{},
	}
	ready.self = ready
	ready.binding = &historicalSupersessionAdjacentBinding{
		ready: ready, candidateBinding: candidate.binding, inventory: inventory, mutation: mutation,
		source: source, planned: planned, authority: authority, receipt: receipt,
	}
	ready.binding.canonical = historicalSupersessionAdjacentDigest(ready)
	historicalSupersessionAdjacentRegistry.Store(ready.binding, ready.binding.canonical)
	if !validHistoricalSupersessionAdjacentReady(ready, candidate) {
		historicalSupersessionAdjacentRegistry.Delete(ready.binding)
		return nil, admissionFailed("historical-supersession-plan", "historical adjacent reservation authority could not be sealed", nil)
	}
	retained = true
	return ready, nil
}

func cloneAdmissionHistoricalVerificationFacts(facts *admissionHistoricalVerificationFacts) *admissionHistoricalVerificationFacts {
	if facts == nil {
		return nil
	}
	owned := *facts
	owned.orderedMigrations = append([]string(nil), facts.orderedMigrations...)
	owned.statementSubjects = make(map[string][][32]byte, len(facts.statementSubjects))
	for key, values := range facts.statementSubjects {
		owned.statementSubjects[key] = append([][32]byte(nil), values...)
	}
	owned.finalCatalogDigest = make(map[string][32]byte, len(facts.finalCatalogDigest))
	for key, value := range facts.finalCatalogDigest {
		owned.finalCatalogDigest[key] = value
	}
	owned.catalogContractDigest = make(map[string][32]byte, len(facts.catalogContractDigest))
	for key, value := range facts.catalogContractDigest {
		owned.catalogContractDigest[key] = value
	}
	owned.attemptPredecessorCatalog = make(map[string][32]byte, len(facts.attemptPredecessorCatalog))
	for key, value := range facts.attemptPredecessorCatalog {
		owned.attemptPredecessorCatalog[key] = value
	}
	owned.ledgerRows = cloneProjectionValue(facts.ledgerRows)
	return &owned
}

func admissionHistoryTargetFacts(transcript *admissionReplayTranscript) (admissionReplayLineageState, admissionReplayLineageHeader, uint64, Digest, error) {
	if transcript == nil {
		return "", admissionReplayLineageHeader{}, 0, "", admissionCorrupt("admission-history", "target transcript is unavailable", nil)
	}
	if transcript.targetAbsent {
		return "", admissionReplayLineageHeader{}, 0, "", nil
	}
	for index := range transcript.lineages {
		lineage := &transcript.lineages[index]
		if lineage.id == transcript.target {
			if lineage.indexRecords == 0 || requireDigest("admission-history.target-tail", lineage.indexTailRecordDigest) != nil {
				return "", admissionReplayLineageHeader{}, 0, "", admissionCorrupt("admission-history", "target index boundary is invalid", nil)
			}
			return lineage.state, lineage.header, lineage.indexRecords, lineage.indexTailRecordDigest, nil
		}
	}
	return "", admissionReplayLineageHeader{}, 0, "", admissionCorrupt("admission-history", "present target lineage is absent", nil)
}

func verifyAdmissionHistoryGenerationEvidence(ctx context.Context, lineage admissionReplayLineage, generation *admissionReplayGeneration, objects map[Digest]*evidencefs.AdmissionObjectView, current OwnedVerifiedDecision, currentFacts *admissionHistoricalVerificationFacts, candidate OwnedCurrentCandidate) (bool, *admissionVerifiedGenerationEvidence, error) {
	if generation == nil || generation.header == nil {
		return false, nil, admissionCorrupt("admission-history", "generation header is unavailable", nil)
	}
	runtimeView, runtimeOK := objects[generation.header.outerArtifactDigest]
	recoveryView, recoveryOK := objects[generation.header.recoveryArtifactDigest]
	if !runtimeOK || !recoveryOK {
		return true, nil, nil
	}
	recoveryRaw, err := readAdmissionHistoryObject(ctx, recoveryView, generation.header.recoveryArtifactDigest, generation.header.recoveryArtifactSize)
	if err != nil {
		return false, nil, err
	}
	runtimeRaw, err := readAdmissionHistoryObject(ctx, runtimeView, generation.header.outerArtifactDigest, generation.header.outerArtifactSize)
	if err != nil {
		return false, nil, err
	}
	descriptor, err := admissionGenerationDescriptor(current.owner.token, lineage, *generation)
	if err != nil {
		return false, nil, err
	}
	facts := currentFacts
	var decision OwnedVerifiedDecision
	var bindings RunnerProjectionBindings
	var policy *VerifiedHistoricalRecoveryPolicy
	var artifact VerifiedDecisionRecoveryArtifact
	var bundle *RuntimeBundle
	if generation.runnerProjectionDecisionDigest == current.digest {
		if generation.header.outerArtifactDigest != candidate.runtimeArtifact.digest || generation.header.outerArtifactSize != candidate.runtimeArtifact.sizeBytes || generation.header.recoveryArtifactDigest != candidate.decisionRecoveryArtifact.digest || generation.header.recoveryArtifactSize != candidate.decisionRecoveryArtifact.sizeBytes || string(runtimeRaw) != string(candidate.runtimeArtifact.bytes) || string(recoveryRaw) != string(candidate.decisionRecoveryArtifact.bytes) {
			return false, nil, admissionCorrupt("admission-history", "current registered generation differs from current candidate", nil)
		}
		bindings, err = current.decision.runnerProjectionBindings()
		if err != nil || !validOwnedCurrentDecision(current, bindings) {
			return true, nil, nil
		}
		bundle, err = LoadRuntimeBundle(runtimeRaw, current.decision)
		if err != nil {
			return false, nil, admissionCorrupt("admission-history", "current registered runtime bundle is invalid", err)
		}
		decision, artifact = current, ownedDecisionRecoveryArtifactCopy(candidate.decisionRecoveryArtifact)
	} else {
		var bindErr error
		artifact, bindErr = bindHistoricalRecoveryVerifierInput(current, descriptor, recoveryRaw)
		if bindErr != nil {
			return true, nil, nil
		}
		old, oldBindings, recoveredPolicy, recoverErr := current.recoverHistoricalDecision(ctx, descriptor, artifact)
		if recoverErr != nil {
			if errors.Is(recoverErr, context.Canceled) || errors.Is(recoverErr, context.DeadlineExceeded) || IsCode(recoverErr, CodeContextCanceled) || IsCode(recoverErr, CodeDeadlineExceeded) {
				return false, nil, mapEvidenceAdmissionError(recoverErr, "admission-history-verifier")
			}
			return true, nil, nil
		}
		loadedBundle, loadErr := loadHistoricalRuntimeBundle(current, old, oldBindings, recoveredPolicy, descriptor, runtimeRaw)
		if loadErr != nil {
			if IsCode(loadErr, CodeEvidenceJournalCorrupt) {
				return false, nil, loadErr
			}
			return true, nil, nil
		}
		facts, err = buildHistoricalVerificationFacts(loadedBundle, oldBindings)
		if err != nil {
			return false, nil, admissionCorrupt("admission-history", "historical runtime differs from recovered projection facts", err)
		}
		decision, bindings, bundle = old, oldBindings.ownedCopy(), loadedBundle
		policyValue := recoveredPolicy
		policy = &policyValue
	}
	if err := verifyAdmissionGeneration(generation, facts); err != nil {
		if IsCode(err, CodeEvidenceJournalCorrupt) {
			return false, nil, err
		}
		return true, nil, nil
	}
	runtimeArtifact := VerifiedRuntimeArtifact{
		owner: current.owner.token, bytes: append([]byte(nil), runtimeRaw...),
		digest: generation.header.outerArtifactDigest, sizeBytes: generation.header.outerArtifactSize,
	}
	if runtimeArtifact.sizeBytes == 0 || runtimeArtifact.sizeBytes > maxRuntimeTarSize || uint64(len(runtimeArtifact.bytes)) != runtimeArtifact.sizeBytes || DigestBytes(runtimeArtifact.bytes) != runtimeArtifact.digest {
		return false, nil, admissionCorrupt("admission-history", "registered runtime artifact is invalid", nil)
	}
	evidence := &admissionVerifiedGenerationEvidence{
		descriptor: descriptor, decision: decision, bindings: bindings.ownedCopy(), policy: cloneHistoricalRecoveryPolicy(policy),
		bundle: bundle, facts: cloneAdmissionHistoricalVerificationFacts(facts), runtimeArtifact: runtimeArtifact, recoveryArtifact: ownedDecisionRecoveryArtifactCopy(artifact),
		runtimeView: runtimeView, recoveryView: recoveryView,
	}
	return false, evidence, nil
}

func retainAdmissionHistoryGeneration(ctx context.Context, lineage admissionReplayLineage, generation *admissionReplayGeneration, evidence *admissionVerifiedGenerationEvidence, current OwnedVerifiedDecision, retention admissionGenerationRetention) (*verifiedAdmissionRegisteredGeneration, error) {
	return retainAdmissionHistoryGenerationMode(ctx, lineage, generation, nil, evidence, current, retention)
}

func retainMaterializedAdmissionHistoryGeneration(ctx context.Context, lineage admissionReplayLineage, generation, actualSuccessor *admissionReplayGeneration, evidence *admissionVerifiedGenerationEvidence, current OwnedVerifiedDecision) (*verifiedAdmissionRegisteredGeneration, error) {
	return retainAdmissionHistoryGenerationMode(ctx, lineage, generation, actualSuccessor, evidence, current, admissionRetainMaterializedSupersededSource)
}

func retainAdmissionHistoryGenerationMode(ctx context.Context, lineage admissionReplayLineage, generation, actualSuccessor *admissionReplayGeneration, evidence *admissionVerifiedGenerationEvidence, current OwnedVerifiedDecision, retention admissionGenerationRetention) (*verifiedAdmissionRegisteredGeneration, error) {
	if evidence == nil || evidence.runtimeView == nil || evidence.recoveryView == nil || evidence.bundle == nil || !validAdmissionRecoveryFacts(evidence.facts) || evidence.decision.owner != current.owner || evidence.runtimeArtifact.owner != current.owner.token || uint64(len(evidence.runtimeArtifact.bytes)) != evidence.runtimeArtifact.sizeBytes || DigestBytes(evidence.runtimeArtifact.bytes) != evidence.runtimeArtifact.digest {
		return nil, fail(CodeEvidenceRecoveryRequired, "admission-history-retain", "verified generation evidence is unavailable", nil)
	}
	var replay *verifiedAdmissionGenerationReplay
	var err error
	switch retention {
	case admissionRetainActiveGeneration:
		replay, err = bindVerifiedAdmissionGenerationReplay(lineage, generation, evidence.descriptor, evidence.facts)
	case admissionRetainSupersededSource:
		replay, err = bindVerifiedSupersededAdmissionGenerationReplay(lineage, generation, evidence.descriptor, evidence.facts)
	case admissionRetainMaterializedSupersededSource:
		replay, err = bindVerifiedMaterializedSupersededAdmissionGenerationReplay(lineage, generation, actualSuccessor, evidence.descriptor, evidence.facts)
	default:
		return nil, fail(CodeEvidenceRecoveryRequired, "admission-history-retain", "generation retention purpose is unavailable", nil)
	}
	if err != nil {
		return nil, err
	}
	runtimePublication, err := evidence.runtimeView.RegisterPublication(ctx)
	if err != nil {
		return nil, mapEvidenceAdmissionError(err, "admission-history-runtime-publication")
	}
	recoveryPublication, err := evidence.recoveryView.RegisterPublication(ctx)
	if err != nil {
		return nil, mapEvidenceAdmissionError(err, "admission-history-recovery-publication")
	}
	runtimeReceipt, recoveryReceipt, err := bindRegisteredReceiptPair(evidence.decision, evidence.bindings, evidence.descriptor, evidence.recoveryArtifact, runtimePublication, recoveryPublication)
	if err != nil {
		return nil, err
	}
	verified := &verifiedAdmissionRegisteredGeneration{
		descriptor: evidence.descriptor, decision: evidence.decision, bindings: evidence.bindings.ownedCopy(), policy: cloneHistoricalRecoveryPolicy(evidence.policy),
		bundle: evidence.bundle, recoveryArtifact: ownedDecisionRecoveryArtifactCopy(evidence.recoveryArtifact), runtimeReceipt: runtimeReceipt, recoveryReceipt: recoveryReceipt, replay: replay,
		handoffConsumed: &atomic.Bool{},
	}
	verified.canonical = verifiedAdmissionRegisteredGenerationDigest(verified)
	if !validVerifiedAdmissionRegisteredGeneration(verified, current) {
		verifiedContentReceiptRegistry.Delete(runtimeReceipt.binding)
		verifiedDecisionRecoveryReceiptRegistry.Delete(recoveryReceipt.binding)
		return nil, fail(CodeEvidenceRecoveryRequired, "admission-history-retain", "registered generation authority could not be sealed", nil)
	}
	return verified, nil
}

func admissionHistoryObjectViews(inventory *evidencefs.AdmissionInventory, transcript *admissionReplayTranscript) (map[Digest]*evidencefs.AdmissionObjectView, error) {
	views, err := inventory.Objects()
	if err != nil {
		return nil, mapEvidenceAdmissionError(err, "admission-history-objects")
	}
	finals := make(map[Digest]admissionReplayObject)
	for _, object := range transcript.objects {
		if !object.temporary {
			finals[object.digest] = object
		}
	}
	wanted := make(map[Digest]admissionReplayObject)
	for _, ref := range transcript.references {
		if !ref.present {
			continue
		}
		object, ok := finals[ref.digest]
		if !ok || object.size != ref.sizeBytes || object.identity != ref.objectIdentity {
			return nil, admissionCorrupt("admission-history-object", "referenced object differs from pass-one transcript", nil)
		}
		wanted[ref.digest] = object
	}
	result := make(map[Digest]*evidencefs.AdmissionObjectView, len(wanted))
	for _, view := range views {
		digestRawValue, err := view.Digest()
		if err != nil {
			return nil, mapEvidenceAdmissionError(err, "admission-history-object")
		}
		digest := digestString(digestRawValue)
		size, sizeErr := view.Size()
		identity, identityErr := view.IdentityDigest()
		temporary, temporaryErr := view.Temporary()
		if sizeErr != nil || identityErr != nil || temporaryErr != nil {
			for _, accessorErr := range []error{sizeErr, identityErr, temporaryErr} {
				if accessorErr != nil {
					return nil, mapEvidenceAdmissionError(accessorErr, "admission-history-object")
				}
			}
		}
		expected, ok := wanted[digest]
		if !ok {
			continue
		}
		if temporary || expected.size != size || expected.identity != identity {
			return nil, admissionCorrupt("admission-history-object", "registered object differs from pass-one transcript", nil)
		}
		result[digest] = view
	}
	if len(result) != len(wanted) {
		return nil, admissionCorrupt("admission-history-object", "registered object catalog is incomplete", nil)
	}
	return result, nil
}

func readAdmissionHistoryObject(ctx context.Context, view *evidencefs.AdmissionObjectView, digest Digest, size uint64) ([]byte, error) {
	if view == nil || requireDigest("admission-history-object", digest) != nil || size == 0 || size > rootTempMaximumEachBytes {
		return nil, admissionCorrupt("admission-history-object", "referenced object identity is invalid", nil)
	}
	raw, err := view.ReadAll(ctx)
	if err != nil {
		return nil, mapEvidenceAdmissionError(err, "admission-history-object-read")
	}
	if uint64(len(raw)) != size || DigestBytes(raw) != digest {
		return nil, admissionCorrupt("admission-history-object", "registered object bytes differ from pass-one transcript", nil)
	}
	return raw, nil
}

func admissionGenerationDescriptor(owner *evidenceOwnerToken, lineage admissionReplayLineage, generation admissionReplayGeneration) (GenerationDescriptor, error) {
	if owner == nil || generation.header == nil {
		return GenerationDescriptor{}, admissionCorrupt("admission-history", "generation descriptor input is incomplete", nil)
	}
	header, err := expandAdmissionHeaderFacts(*generation.header)
	if err != nil {
		return GenerationDescriptor{}, err
	}
	tail := generation.expectedSegment0HeaderDigest
	for _, journal := range lineage.journals {
		if journal.id == digestRaw(generation.journalID) {
			tail = journal.tail
			break
		}
	}
	if requireDigest("admission-history.tail", tail) != nil {
		return GenerationDescriptor{}, admissionCorrupt("admission-history", "generation replay tail is invalid", nil)
	}
	identity := generationIdentity{owner, header.ExecutionLineageDigest, header.JournalIdentityDigest, header.RunnerProjectionDecisionDigest, header.SchemaBundleDigest}
	return GenerationDescriptor{identity: identity, header: header, replayTailDigest: tail, recoveryArtifactDigest: header.DecisionRecoveryArtifactSHA256, recoveryArtifactSize: header.DecisionRecoveryArtifactSizeBytes}, nil
}

func admissionHistoryDigest(history *VerifiedAdmissionHistory) [32]byte {
	h := sha256.New()
	if history == nil {
		return [32]byte{}
	}
	profile := history.quotaProfile
	if profile == "" {
		// A pre-profile in-memory v1 history had no field at all. Keep its
		// historical subject, but still bind the normalized reservation profile
		// so a later v2 swap cannot leave the canonical digest unchanged.
		profile = EvidenceLimitsProfile
	}
	if !validEvidenceLimitsProfile(profile) || quotaReservationProfile(history.reservation) != profile {
		return [32]byte{}
	}
	if profile == LineageQuotaProfileV2 {
		h.Write([]byte("cloud-agents-platform-verified-admission-history/v3\x00"))
		writeAdmissionString(h, profile)
		writeAdmissionString(h, quotaReservationProfile(history.reservation))
	} else if profile == LineageQuotaProfileV3 {
		h.Write([]byte("cloud-agents-platform-verified-admission-history/v4\x00"))
		writeAdmissionString(h, profile)
		writeAdmissionString(h, quotaReservationProfile(history.reservation))
	} else if profile == LineageQuotaProfileV4 {
		h.Write([]byte("cloud-agents-platform-verified-admission-history/v5\x00"))
		writeAdmissionString(h, profile)
		writeAdmissionString(h, quotaReservationProfile(history.reservation))
	} else {
		// Historical v1 history retained the v2 subject before the explicit
		// profile field was introduced; do not rewrite that in-memory digest.
		h.Write([]byte("cloud-agents-platform-verified-admission-history/v2\x00"))
	}
	h.Write(history.target[:])
	h.Write(history.fullSet[:])
	h.Write(history.transcriptCanonical[:])
	writeAdmissionString(h, string(history.targetState))
	for _, value := range []string{history.targetHeader.executionLineageDigest.String(), history.targetHeader.deploymentID, history.targetHeader.databaseName, history.targetHeader.repositoryIdentity, history.targetHeader.limitsProfile} {
		writeAdmissionString(h, value)
	}
	if history.targetIndexTail == "" {
		h.Write([]byte{0})
	} else {
		h.Write([]byte{1})
		value := history.targetIndexTail
		writeAdmissionOptionalDigest(h, &value)
	}
	facts := admissionRecoveryFactsDigest(history.currentFacts)
	h.Write(facts[:])
	root := rootQuotaFactsDigest(history.rootFacts)
	h.Write(root[:])
	writeHistoryUint := func(value uint64) {
		var raw [8]byte
		binary.BigEndian.PutUint64(raw[:], value)
		h.Write(raw[:])
	}
	writeHistoryUint(history.revision)
	writeHistoryUint(history.targetIndexRecords)
	for _, value := range []uint64{
		history.reservation.ReservedRecords, history.reservation.ReservedJournalBytes, uint64(history.reservation.ReservedSegments), history.reservation.ReservedCheckpointRecords, history.reservation.ReservedIndexRecords, history.reservation.ReservedIndexBytes, history.reservation.ReservedBytes,
		history.quotaAdmission.finalObjectCount, history.quotaAdmission.finalObjectBytes, history.quotaAdmission.journalCount, history.quotaAdmission.journalReservedBytes,
		history.quotaAdmission.indexCount, history.quotaAdmission.indexReservedBytes, history.quotaAdmission.targetIndexRecords, history.quotaAdmission.targetIndexReservedBytes,
	} {
		writeHistoryUint(value)
	}
	if history.owner != nil && history.candidateBinding != nil {
		h.Write(history.candidateBinding.canonical[:])
	}
	if history.targetGeneration == nil {
		h.Write([]byte{0})
	} else {
		h.Write([]byte{1})
		h.Write(history.targetGeneration.canonical[:])
	}
	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result
}

func validVerifiedAdmissionHistory(history *VerifiedAdmissionHistory, candidate OwnedCurrentCandidate) bool {
	if history == nil || history.binding == nil || history.owner == nil || history.inventory == nil || !validOwnedCurrentCandidate(candidate) || !validEvidenceLimitsProfile(history.quotaProfile) || quotaReservationProfile(history.reservation) != history.quotaProfile || history.currentFacts == nil || history.currentFacts.lineageQuotaProfile != history.quotaProfile || history.owner != candidate.verifiedRun.currentDecision.owner || history.candidateBinding != candidate.binding || history.binding.owner != history.owner || history.binding.candidateBinding != candidate.binding || history.binding.inventory != history.inventory || history.binding.history != history || admissionRecoveryFactsDigest(history.currentFacts) == ([32]byte{}) || history.binding.canonical == ([32]byte{}) || history.binding.canonical != admissionHistoryDigest(history) || !history.rootFacts.valid() {
		return false
	}
	requiresTargetGeneration := history.targetState != "" && history.targetState != admissionLineageEmpty
	requiresTargetReplay := admissionStateRequiresGenerationReplay(history.targetState)
	if requiresTargetGeneration != (history.targetGeneration != nil) || history.targetGeneration != nil && (digestRaw(history.targetGeneration.descriptor.identity.executionLineageDigest) != history.target || requiresTargetReplay != (history.targetGeneration.replay != nil) || history.targetGeneration.replay != nil && (history.targetGeneration.replay.cursor.lineageIndexNextSequence != history.targetIndexRecords || history.targetGeneration.replay.cursor.lineageIndexPreviousRecordDigest != history.targetIndexTail)) {
		return false
	}
	registered, ok := verifiedAdmissionHistoryRegistry.Load(history.binding)
	if !ok || registered != history.binding.canonical {
		return false
	}
	if history.targetGeneration != nil && !validVerifiedAdmissionRegisteredGeneration(history.targetGeneration, candidate.verifiedRun.currentDecision) {
		return false
	}
	revision, err := history.inventory.Revision()
	if err != nil || revision != history.revision {
		return false
	}
	target, err := history.inventory.Target()
	if err != nil || target != history.target {
		return false
	}
	fullSet, err := history.inventory.FullSetDigest()
	return err == nil && fullSet == history.fullSet
}

func cloneHistoricalRecoveryPolicy(policy *VerifiedHistoricalRecoveryPolicy) *VerifiedHistoricalRecoveryPolicy {
	if policy == nil {
		return nil
	}
	result := *policy
	result.subject = cloneProjectionValue(policy.subject)
	return &result
}

func cloneVerifiedAdmissionRegisteredGeneration(value *verifiedAdmissionRegisteredGeneration) *verifiedAdmissionRegisteredGeneration {
	if value == nil {
		return nil
	}
	result := *value
	result.descriptor.header = cloneProjectionValue(value.descriptor.header)
	result.decision = ownedVerifiedDecisionCopy(value.decision, value.bindings)
	result.bindings = value.bindings.ownedCopy()
	result.policy = cloneHistoricalRecoveryPolicy(value.policy)
	result.recoveryArtifact = ownedDecisionRecoveryArtifactCopy(value.recoveryArtifact)
	result.replay = cloneVerifiedAdmissionGenerationReplay(value.replay)
	return &result
}

func revokeVerifiedAdmissionRegisteredGeneration(value *verifiedAdmissionRegisteredGeneration) {
	if value == nil {
		return
	}
	if value.replay != nil && value.replay.cursor.valid != nil {
		value.replay.cursor.valid.Store(false)
	}
	if value.runtimeReceipt.binding != nil {
		verifiedContentReceiptRegistry.Delete(value.runtimeReceipt.binding)
	}
	if value.recoveryReceipt.binding != nil {
		verifiedDecisionRecoveryReceiptRegistry.Delete(value.recoveryReceipt.binding)
	}
}

func verifiedAdmissionRegisteredGenerationDigest(value *verifiedAdmissionRegisteredGeneration) [32]byte {
	if value == nil || value.descriptor.identity.owner == nil || value.descriptor.header.Validate() != nil || value.decision.owner == nil || value.bindings.expectedCanonical == "" || value.bundle == nil || value.recoveryArtifact.digest.Validate() != nil || value.runtimeReceipt.binding == nil || value.recoveryReceipt.binding == nil {
		return [32]byte{}
	}
	header, err := canonicalContractKey(value.descriptor.header)
	if err != nil {
		return [32]byte{}
	}
	h := sha256.New()
	h.Write([]byte("cloud-agents-platform-verified-registered-generation/v2\x00"))
	h.Write([]byte(header))
	h.Write([]byte(value.bindings.expectedCanonical))
	h.Write(value.bundle.ownedInputs.canonical[:])
	for _, digest := range []Digest{value.descriptor.replayTailDigest, value.descriptor.recoveryArtifactDigest, value.decision.digest, value.recoveryArtifact.digest, value.runtimeReceipt.digest, value.recoveryReceipt.digest} {
		writeAdmissionString(h, digest.String())
	}
	writeAdmissionUint(h, value.descriptor.recoveryArtifactSize)
	writeAdmissionUint(h, value.recoveryArtifact.sizeBytes)
	writeAdmissionUint(h, value.runtimeReceipt.sizeBytes)
	writeAdmissionUint(h, value.recoveryReceipt.sizeBytes)
	if value.policy == nil {
		h.Write([]byte{0})
	} else {
		h.Write([]byte{1})
		writeAdmissionString(h, value.policy.digest.String())
	}
	if value.replay == nil {
		h.Write([]byte{0})
	} else {
		h.Write([]byte{1})
		h.Write(value.replay.canonical[:])
	}
	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result
}

func validVerifiedAdmissionRegisteredGeneration(value *verifiedAdmissionRegisteredGeneration, current OwnedVerifiedDecision) bool {
	return value != nil && value.handoffConsumed != nil && !value.handoffConsumed.Load() && validVerifiedAdmissionRegisteredGenerationFacts(value, current)
}

func validVerifiedAdmissionRegisteredGenerationFacts(value *verifiedAdmissionRegisteredGeneration, current OwnedVerifiedDecision) bool {
	if value == nil || current.owner == nil || value.descriptor.identity.owner != current.owner.token || value.decision.owner != current.owner || value.decision.capability.owner != current.owner || value.bindings.runnerProjectionDecisionDigest != value.decision.digest || value.descriptor.identity.runnerProjectionDecisionDigest != value.decision.digest || value.descriptor.identity.executionLineageDigest != value.bindings.executionLineageDigest || value.descriptor.identity.schemaBundleDigest != value.bindings.schemaBundleDigest || value.descriptor.header.Validate() != nil || !sameGenerationHeader(value.descriptor.identity, value.descriptor.header) || value.bundle == nil || value.bundle.ownedInputs.canonical == ([32]byte{}) || value.bundle.ownedInputs.outerArtifactDigest != value.descriptor.header.OuterArtifactDigest || value.bundle.ownedInputs.outerArtifactSize != value.descriptor.header.OuterArtifactSizeBytes || !validRegisteredDecisionRecoveryArtifact(value.decision, value.bindings, value.descriptor, value.recoveryArtifact) || !validRegisteredRuntimeReceipt(value.runtimeReceipt, current.owner.token, value.descriptor.header.OuterArtifactDigest, value.descriptor.header.OuterArtifactSizeBytes) || !validRegisteredDecisionRecoveryReceipt(value.recoveryReceipt, current.owner.token, value.descriptor.header.DecisionRecoveryArtifactSHA256, value.descriptor.header.DecisionRecoveryArtifactSizeBytes) || !registeredReceiptsSameStore(value.runtimeReceipt, value.recoveryReceipt) || value.replay != nil && (!validVerifiedAdmissionGenerationReplay(value.replay, value.descriptor.identity) || value.replay.cursor.previousRecordDigest == nil || *value.replay.cursor.previousRecordDigest != value.descriptor.replayTailDigest || value.replay.reservation.ReservedRecords != value.descriptor.header.ReservedRecords || value.replay.reservation.ReservedBytes != value.descriptor.header.ReservedBytes || value.replay.reservation.ReservedSegments != value.descriptor.header.ReservedSegments) || value.canonical == ([32]byte{}) || value.canonical != verifiedAdmissionRegisteredGenerationDigest(value) {
		return false
	}
	if _, _, err := value.bundle.ownedInputs.copyVerified(); err != nil {
		return false
	}
	currentBindings, err := current.decision.runnerProjectionBindings()
	if err != nil || !validOwnedCurrentDecision(current, currentBindings) {
		return false
	}
	if value.decision.digest == current.digest {
		return value.policy == nil && validOwnedCurrentDecision(value.decision, value.bindings)
	}
	if value.policy == nil || value.policy.owner != current.owner || value.policy.subject.OldRunnerProjectionDecisionDigest != value.decision.digest || value.policy.subject.SuccessorRunnerProjectionDecisionDigest != current.digest || value.policy.subject.ExecutionLineageDigest != value.descriptor.identity.executionLineageDigest || value.policy.subject.OldJournalIdentityDigest != value.descriptor.identity.journalIdentityDigest || value.policy.subject.OldSchemaBundleDigest != value.descriptor.identity.schemaBundleDigest || value.policy.subject.OldDecisionRecoveryArtifactSHA256 != value.recoveryArtifact.digest || value.policy.subject.OldDecisionRecoveryArtifactSizeBytes != value.recoveryArtifact.sizeBytes || value.decision.decision.validateHistorical(value.bindings) != nil {
		return false
	}
	policyDigest, err := value.policy.subject.ComputeDigest()
	return err == nil && policyDigest == value.policy.digest
}

func admissionStateRequiresGenerationReplay(state admissionReplayLineageState) bool {
	return state == admissionLineageActiveInitial || state == admissionLineageActiveCheckpointed || state == admissionLineageActiveUnknownExtension
}
