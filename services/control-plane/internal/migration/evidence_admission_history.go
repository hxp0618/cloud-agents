package migration

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"sync"

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
	canonical        [32]byte
}

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
	defer func() {
		if !retainTargetGeneration {
			revokeVerifiedAdmissionRegisteredGeneration(targetGeneration)
		}
	}()
	for lineageIndex := range transcript.lineages {
		lineage := &transcript.lineages[lineageIndex]
		for generationIndex := range lineage.generations {
			generation := &lineage.generations[generationIndex]
			generations := []*admissionReplayGeneration{generation}
			if generation.plannedSuccessor != nil {
				generations = append(generations, generation.plannedSuccessor)
			}
			for generationPosition, verifyGeneration := range generations {
				retainRegistered := lineage.id == target && generationIndex == len(lineage.generations)-1 && generationPosition == 0
				needsRecovery, verifiedGeneration, verifyErr := verifyAdmissionHistoryGeneration(ctx, *lineage, verifyGeneration, objects, current, currentFacts, candidate, retainRegistered)
				recoveryRequired = recoveryRequired || needsRecovery
				if verifyErr == nil {
					if retainRegistered {
						targetGeneration = verifiedGeneration
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
			// Historical supersession receipts are verifier/lifecycle authority,
			// not durable wire facts. The active target generation can now retain
			// registered receipts, but A -> B reconstruction and its adjacent
			// reservation authority remain a separate closed transition.
			if generation.supersessionRecordDigest != nil || generation.plannedSuccessor != nil {
				recoveryRequired = true
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
	if requiresTargetGeneration != (targetGeneration != nil) || targetGeneration != nil && digestRaw(targetGeneration.descriptor.identity.executionLineageDigest) != target {
		return nil, admissionFailed("admission-history", "target generation authority is incomplete or mismatched", nil)
	}
	history := &VerifiedAdmissionHistory{
		owner: current.owner, candidateBinding: candidate.binding, inventory: inventory, revision: revision,
		target: target, fullSet: fullSet, transcriptCanonical: transcript.canonical,
		targetState: targetState, targetHeader: targetHeader, targetIndexRecords: targetRecords, targetIndexTail: targetTail,
		currentFacts: cloneAdmissionHistoricalVerificationFacts(currentFacts),
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

func verifyAdmissionHistoryGeneration(ctx context.Context, lineage admissionReplayLineage, generation *admissionReplayGeneration, objects map[Digest]*evidencefs.AdmissionObjectView, current OwnedVerifiedDecision, currentFacts *admissionHistoricalVerificationFacts, candidate OwnedCurrentCandidate, retainRegistered bool) (bool, *verifiedAdmissionRegisteredGeneration, error) {
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
	if !retainRegistered {
		return false, nil, nil
	}
	runtimePublication, err := runtimeView.RegisterPublication(ctx)
	if err != nil {
		return false, nil, mapEvidenceAdmissionError(err, "admission-history-runtime-publication")
	}
	recoveryPublication, err := recoveryView.RegisterPublication(ctx)
	if err != nil {
		return false, nil, mapEvidenceAdmissionError(err, "admission-history-recovery-publication")
	}
	runtimeReceipt, recoveryReceipt, err := bindRegisteredReceiptPair(decision, bindings, descriptor, artifact, runtimePublication, recoveryPublication)
	if err != nil {
		return true, nil, nil
	}
	verified := &verifiedAdmissionRegisteredGeneration{
		descriptor: descriptor, decision: decision, bindings: bindings.ownedCopy(), policy: cloneHistoricalRecoveryPolicy(policy),
		bundle: bundle, recoveryArtifact: ownedDecisionRecoveryArtifactCopy(artifact), runtimeReceipt: runtimeReceipt, recoveryReceipt: recoveryReceipt,
	}
	verified.canonical = verifiedAdmissionRegisteredGenerationDigest(verified)
	if !validVerifiedAdmissionRegisteredGeneration(verified, current) {
		verifiedContentReceiptRegistry.Delete(runtimeReceipt.binding)
		verifiedDecisionRecoveryReceiptRegistry.Delete(recoveryReceipt.binding)
		return true, nil, nil
	}
	return false, verified, nil
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
	h.Write([]byte("cloud-agents-platform-verified-admission-history/v2\x00"))
	if history == nil {
		return [32]byte{}
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
	if history == nil || history.binding == nil || history.owner == nil || history.inventory == nil || !validOwnedCurrentCandidate(candidate) || history.owner != candidate.verifiedRun.currentDecision.owner || history.candidateBinding != candidate.binding || history.binding.owner != history.owner || history.binding.candidateBinding != candidate.binding || history.binding.inventory != history.inventory || history.binding.history != history || admissionRecoveryFactsDigest(history.currentFacts) == ([32]byte{}) || history.binding.canonical == ([32]byte{}) || history.binding.canonical != admissionHistoryDigest(history) || !history.rootFacts.valid() {
		return false
	}
	requiresTargetGeneration := history.targetState != "" && history.targetState != admissionLineageEmpty
	if requiresTargetGeneration != (history.targetGeneration != nil) || history.targetGeneration != nil && digestRaw(history.targetGeneration.descriptor.identity.executionLineageDigest) != history.target {
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
	return &result
}

func revokeVerifiedAdmissionRegisteredGeneration(value *verifiedAdmissionRegisteredGeneration) {
	if value == nil {
		return
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
	h.Write([]byte("cloud-agents-platform-verified-registered-generation/v1\x00"))
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
	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result
}

func validVerifiedAdmissionRegisteredGeneration(value *verifiedAdmissionRegisteredGeneration, current OwnedVerifiedDecision) bool {
	if value == nil || current.owner == nil || value.descriptor.identity.owner != current.owner.token || value.decision.owner != current.owner || value.decision.capability.owner != current.owner || value.bindings.runnerProjectionDecisionDigest != value.decision.digest || value.descriptor.identity.runnerProjectionDecisionDigest != value.decision.digest || value.descriptor.identity.executionLineageDigest != value.bindings.executionLineageDigest || value.descriptor.identity.schemaBundleDigest != value.bindings.schemaBundleDigest || value.descriptor.header.Validate() != nil || !sameGenerationHeader(value.descriptor.identity, value.descriptor.header) || value.bundle == nil || value.bundle.ownedInputs.canonical == ([32]byte{}) || value.bundle.ownedInputs.outerArtifactDigest != value.descriptor.header.OuterArtifactDigest || value.bundle.ownedInputs.outerArtifactSize != value.descriptor.header.OuterArtifactSizeBytes || !validRegisteredDecisionRecoveryArtifact(value.decision, value.bindings, value.descriptor, value.recoveryArtifact) || !validRegisteredRuntimeReceipt(value.runtimeReceipt, current.owner.token, value.descriptor.header.OuterArtifactDigest, value.descriptor.header.OuterArtifactSizeBytes) || !validRegisteredDecisionRecoveryReceipt(value.recoveryReceipt, current.owner.token, value.descriptor.header.DecisionRecoveryArtifactSHA256, value.descriptor.header.DecisionRecoveryArtifactSizeBytes) || !registeredReceiptsSameStore(value.runtimeReceipt, value.recoveryReceipt) || value.canonical == ([32]byte{}) || value.canonical != verifiedAdmissionRegisteredGenerationDigest(value) {
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
