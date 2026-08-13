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
	rootFacts           rootQuotaUsageFacts
	reservation         evidenceQuotaReservation
	quotaAdmission      rootQuotaAdmission
	binding             *verifiedAdmissionHistoryBinding
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
	for lineageIndex := range transcript.lineages {
		lineage := &transcript.lineages[lineageIndex]
		for generationIndex := range lineage.generations {
			generation := &lineage.generations[generationIndex]
			generations := []*admissionReplayGeneration{generation}
			if generation.plannedSuccessor != nil {
				generations = append(generations, generation.plannedSuccessor)
			}
			for _, verifyGeneration := range generations {
				needsRecovery, verifyErr := verifyAdmissionHistoryGeneration(ctx, *lineage, verifyGeneration, objects, current, currentFacts, candidate)
				recoveryRequired = recoveryRequired || needsRecovery
				if verifyErr == nil {
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
			// not durable wire facts. Until registered publication receipts are
			// wired, any stored supersession keeps history recovery-only.
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
	history := &VerifiedAdmissionHistory{
		owner: current.owner, candidateBinding: candidate.binding, inventory: inventory, revision: revision,
		target: target, fullSet: fullSet, transcriptCanonical: transcript.canonical,
		rootFacts: rootFacts, reservation: reservation, quotaAdmission: quotaAdmission,
	}
	binding := &verifiedAdmissionHistoryBinding{owner: current.owner, candidateBinding: candidate.binding, inventory: inventory, history: history}
	history.binding = binding
	binding.canonical = admissionHistoryDigest(history)
	verifiedAdmissionHistoryRegistry.Store(binding, binding.canonical)
	return history, nil
}

func verifyAdmissionHistoryGeneration(ctx context.Context, lineage admissionReplayLineage, generation *admissionReplayGeneration, objects map[Digest]*evidencefs.AdmissionObjectView, current OwnedVerifiedDecision, currentFacts *admissionHistoricalVerificationFacts, candidate OwnedCurrentCandidate) (bool, error) {
	if generation == nil || generation.header == nil {
		return false, admissionCorrupt("admission-history", "generation header is unavailable", nil)
	}
	runtimeView, runtimeOK := objects[generation.header.outerArtifactDigest]
	recoveryView, recoveryOK := objects[generation.header.recoveryArtifactDigest]
	if !runtimeOK || !recoveryOK {
		return true, nil
	}
	recoveryRaw, err := readAdmissionHistoryObject(ctx, recoveryView, generation.header.recoveryArtifactDigest, generation.header.recoveryArtifactSize)
	if err != nil {
		return false, err
	}
	runtimeRaw, err := readAdmissionHistoryObject(ctx, runtimeView, generation.header.outerArtifactDigest, generation.header.outerArtifactSize)
	if err != nil {
		return false, err
	}
	descriptor, err := admissionGenerationDescriptor(current.owner.token, lineage, *generation)
	if err != nil {
		return false, err
	}
	facts := currentFacts
	if generation.runnerProjectionDecisionDigest == current.digest {
		if generation.header.outerArtifactDigest != candidate.runtimeArtifact.digest || generation.header.outerArtifactSize != candidate.runtimeArtifact.sizeBytes || generation.header.recoveryArtifactDigest != candidate.decisionRecoveryArtifact.digest || generation.header.recoveryArtifactSize != candidate.decisionRecoveryArtifact.sizeBytes || string(runtimeRaw) != string(candidate.runtimeArtifact.bytes) || string(recoveryRaw) != string(candidate.decisionRecoveryArtifact.bytes) {
			return false, admissionCorrupt("admission-history", "current registered generation differs from current candidate", nil)
		}
	} else {
		artifact, bindErr := bindHistoricalRecoveryVerifierInput(current, descriptor, recoveryRaw)
		if bindErr != nil {
			return true, nil
		}
		old, oldBindings, policy, recoverErr := current.recoverHistoricalDecision(ctx, descriptor, artifact)
		if recoverErr != nil {
			if errors.Is(recoverErr, context.Canceled) || errors.Is(recoverErr, context.DeadlineExceeded) || IsCode(recoverErr, CodeContextCanceled) || IsCode(recoverErr, CodeDeadlineExceeded) {
				return false, mapEvidenceAdmissionError(recoverErr, "admission-history-verifier")
			}
			return true, nil
		}
		bundle, loadErr := loadHistoricalRuntimeBundle(current, old, oldBindings, policy, descriptor, runtimeRaw)
		if loadErr != nil {
			if IsCode(loadErr, CodeEvidenceJournalCorrupt) {
				return false, loadErr
			}
			return true, nil
		}
		facts, err = buildHistoricalVerificationFacts(bundle, oldBindings)
		if err != nil {
			return false, admissionCorrupt("admission-history", "historical runtime differs from recovered projection facts", err)
		}
	}
	if err := verifyAdmissionGeneration(generation, facts); err != nil {
		if IsCode(err, CodeEvidenceJournalCorrupt) {
			return false, err
		}
		return true, nil
	}
	return false, nil
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
	h.Write([]byte("cloud-agents-platform-verified-admission-history/v1\x00"))
	if history == nil {
		return [32]byte{}
	}
	h.Write(history.target[:])
	h.Write(history.fullSet[:])
	h.Write(history.transcriptCanonical[:])
	root := rootQuotaFactsDigest(history.rootFacts)
	h.Write(root[:])
	writeHistoryUint := func(value uint64) {
		var raw [8]byte
		binary.BigEndian.PutUint64(raw[:], value)
		h.Write(raw[:])
	}
	writeHistoryUint(history.revision)
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
	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result
}

func validVerifiedAdmissionHistory(history *VerifiedAdmissionHistory, candidate OwnedCurrentCandidate) bool {
	if history == nil || history.binding == nil || history.owner == nil || history.inventory == nil || !validOwnedCurrentCandidate(candidate) || history.owner != candidate.verifiedRun.currentDecision.owner || history.candidateBinding != candidate.binding || history.binding.owner != history.owner || history.binding.candidateBinding != candidate.binding || history.binding.inventory != history.inventory || history.binding.history != history || history.binding.canonical == ([32]byte{}) || history.binding.canonical != admissionHistoryDigest(history) || !history.rootFacts.valid() {
		return false
	}
	registered, ok := verifiedAdmissionHistoryRegistry.Load(history.binding)
	if !ok || registered != history.binding.canonical {
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
