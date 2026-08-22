package migration

import (
	"crypto/sha256"
	"sort"
	"sync"
	"sync/atomic"
)

// verifiedAdmissionLifecycleWitness is a one-shot bridge from a currently
// valid same-verifier journal to a fresh ALL-history admission replay. Stored
// retry and ambiguous records remain ordinary disk facts: only this retained
// live witness can satisfy their external lifecycle checks.
type verifiedAdmissionLifecycleWitness struct {
	self             *verifiedAdmissionLifecycleWitness
	candidateBinding *verifiedEvidenceRunBinding
	generation       generationIdentity
	stateCanonical   [32]byte
	chain            verifiedEvidenceChainWitness
	canonical        [32]byte
	consumed         *atomic.Bool
}

type verifiedAdmissionLifecycleWitnessRecord struct {
	witness          *verifiedAdmissionLifecycleWitness
	candidateBinding *verifiedEvidenceRunBinding
	generation       generationIdentity
	stateCanonical   [32]byte
	chainCanonical   [32]byte
	consumed         *atomic.Bool
	canonical        [32]byte
}

// admissionLifecycleEvidence is one-shot input to one admission-history replay.
// Its registry prevents an ordinary literal or copy from substituting disk
// facts for the retained live lifecycle witness.
type admissionLifecycleEvidence struct {
	self             *admissionLifecycleEvidence
	candidateBinding *verifiedEvidenceRunBinding
	generation       generationIdentity
	stateCanonical   [32]byte
	chain            verifiedEvidenceChainWitness
	chainCanonical   [32]byte
	canonical        [32]byte
	active           *atomic.Bool
}

type admissionLifecycleEvidenceRecord struct {
	evidence         *admissionLifecycleEvidence
	candidateBinding *verifiedEvidenceRunBinding
	generation       generationIdentity
	stateCanonical   [32]byte
	chainCanonical   [32]byte
	canonical        [32]byte
	active           *atomic.Bool
}

var verifiedAdmissionLifecycleWitnessRegistry sync.Map
var admissionLifecycleEvidenceRegistry sync.Map

func bindVerifiedAdmissionLifecycleWitness(candidateBinding *verifiedEvidenceRunBinding, generation generationIdentity, stateCanonical [32]byte, chain verifiedEvidenceChainWitness) (*verifiedAdmissionLifecycleWitness, error) {
	if candidateBinding == nil || generation.owner == nil || generation.owner != candidateBinding.owner || stateCanonical == ([32]byte{}) {
		return nil, fail(CodeEvidenceRecoveryRequired, "admission-lifecycle-witness", "live lifecycle witness identity is unavailable", nil)
	}
	owned := cloneAdmissionLifecycleChainWitness(chain)
	chainCanonical := verifiedAdmissionLifecycleChainDigest(owned)
	if chainCanonical == ([32]byte{}) {
		return nil, fail(CodeEvidenceRecoveryRequired, "admission-lifecycle-witness", "live lifecycle witness is incomplete", nil)
	}
	witness := &verifiedAdmissionLifecycleWitness{
		candidateBinding: candidateBinding, generation: generation, stateCanonical: stateCanonical,
		chain: owned, consumed: &atomic.Bool{},
	}
	witness.self = witness
	witness.canonical = verifiedAdmissionLifecycleWitnessDigest(witness)
	if witness.canonical == ([32]byte{}) {
		return nil, fail(CodeEvidenceRecoveryRequired, "admission-lifecycle-witness", "live lifecycle witness could not be identified", nil)
	}
	verifiedAdmissionLifecycleWitnessRegistry.Store(witness, verifiedAdmissionLifecycleWitnessRecord{
		witness: witness, candidateBinding: candidateBinding, generation: generation,
		stateCanonical: stateCanonical, chainCanonical: chainCanonical,
		consumed: witness.consumed, canonical: witness.canonical,
	})
	if !validVerifiedAdmissionLifecycleWitness(witness, candidateBinding) {
		verifiedAdmissionLifecycleWitnessRegistry.Delete(witness)
		witness.consumed.Store(true)
		return nil, fail(CodeEvidenceRecoveryRequired, "admission-lifecycle-witness", "live lifecycle witness could not be sealed", nil)
	}
	return witness, nil
}

func consumeVerifiedAdmissionLifecycleWitness(witness *verifiedAdmissionLifecycleWitness, candidate OwnedCurrentCandidate) (*admissionLifecycleEvidence, error) {
	if !validOwnedCurrentCandidate(candidate) || !validVerifiedAdmissionLifecycleWitness(witness, candidate.binding) ||
		!witness.consumed.CompareAndSwap(false, true) {
		revokeVerifiedAdmissionLifecycleWitness(witness)
		return nil, fail(CodeEvidenceRecoveryRequired, "admission-lifecycle-witness", "live lifecycle witness is unavailable or consumed", nil)
	}
	registered, ok := verifiedAdmissionLifecycleWitnessRegistry.LoadAndDelete(witness)
	record, recordOK := registered.(verifiedAdmissionLifecycleWitnessRecord)
	if !ok || !recordOK || record.witness != witness || record.candidateBinding != candidate.binding ||
		record.consumed != witness.consumed || record.canonical != witness.canonical ||
		record.chainCanonical != verifiedAdmissionLifecycleChainDigest(witness.chain) {
		return nil, fail(CodeEvidenceRecoveryRequired, "admission-lifecycle-witness", "immutable lifecycle witness record is unavailable", nil)
	}
	evidence := &admissionLifecycleEvidence{
		candidateBinding: candidate.binding, generation: record.generation,
		stateCanonical: record.stateCanonical, chain: cloneAdmissionLifecycleChainWitness(witness.chain),
		chainCanonical: record.chainCanonical, active: &atomic.Bool{},
	}
	evidence.self = evidence
	evidence.canonical = admissionLifecycleEvidenceDigest(evidence)
	if evidence.canonical == ([32]byte{}) {
		evidence.active.Store(true)
		return nil, fail(CodeEvidenceRecoveryRequired, "admission-lifecycle-evidence", "consumed lifecycle evidence could not be identified", nil)
	}
	admissionLifecycleEvidenceRegistry.Store(evidence, admissionLifecycleEvidenceRecord{
		evidence: evidence, candidateBinding: evidence.candidateBinding, generation: evidence.generation,
		stateCanonical: evidence.stateCanonical, chainCanonical: evidence.chainCanonical,
		canonical: evidence.canonical, active: evidence.active,
	})
	if !validAdmissionLifecycleEvidence(evidence, candidate.binding) {
		revokeAdmissionLifecycleEvidence(evidence)
		return nil, fail(CodeEvidenceRecoveryRequired, "admission-lifecycle-evidence", "consumed lifecycle evidence could not be sealed", nil)
	}
	return evidence, nil
}

func revokeVerifiedAdmissionLifecycleWitness(witness *verifiedAdmissionLifecycleWitness) {
	if witness == nil {
		return
	}
	verifiedAdmissionLifecycleWitnessRegistry.Delete(witness)
	if witness.consumed != nil {
		witness.consumed.Store(true)
	}
}

func revokeAdmissionLifecycleEvidence(evidence *admissionLifecycleEvidence) {
	if evidence == nil {
		return
	}
	admissionLifecycleEvidenceRegistry.Delete(evidence)
	if evidence.active != nil {
		evidence.active.Store(true)
	}
}

func validAdmissionLifecycleEvidence(evidence *admissionLifecycleEvidence, candidateBinding *verifiedEvidenceRunBinding) bool {
	if evidence == nil || evidence.self != evidence || evidence.candidateBinding == nil || evidence.candidateBinding != candidateBinding ||
		evidence.generation.owner == nil || evidence.generation.owner != candidateBinding.owner || evidence.stateCanonical == ([32]byte{}) ||
		evidence.chainCanonical == ([32]byte{}) || evidence.chainCanonical != verifiedAdmissionLifecycleChainDigest(evidence.chain) ||
		evidence.active == nil || evidence.active.Load() || evidence.canonical == ([32]byte{}) ||
		evidence.canonical != admissionLifecycleEvidenceDigest(evidence) {
		return false
	}
	value, ok := admissionLifecycleEvidenceRegistry.Load(evidence)
	record, recordOK := value.(admissionLifecycleEvidenceRecord)
	return ok && recordOK && record.evidence == evidence && record.candidateBinding == candidateBinding &&
		sameGenerationIdentity(record.generation, evidence.generation) && record.stateCanonical == evidence.stateCanonical &&
		record.chainCanonical == evidence.chainCanonical && record.canonical == evidence.canonical && record.active == evidence.active
}

func validVerifiedAdmissionLifecycleWitness(witness *verifiedAdmissionLifecycleWitness, candidateBinding *verifiedEvidenceRunBinding) bool {
	if witness == nil || witness.self != witness || witness.candidateBinding == nil || witness.candidateBinding != candidateBinding ||
		witness.generation.owner == nil || witness.generation.owner != candidateBinding.owner || witness.stateCanonical == ([32]byte{}) ||
		witness.consumed == nil || witness.consumed.Load() || witness.canonical == ([32]byte{}) ||
		witness.canonical != verifiedAdmissionLifecycleWitnessDigest(witness) {
		return false
	}
	value, ok := verifiedAdmissionLifecycleWitnessRegistry.Load(witness)
	record, recordOK := value.(verifiedAdmissionLifecycleWitnessRecord)
	return ok && recordOK && record.witness == witness && record.candidateBinding == candidateBinding &&
		sameGenerationIdentity(record.generation, witness.generation) && record.stateCanonical == witness.stateCanonical &&
		record.chainCanonical == verifiedAdmissionLifecycleChainDigest(witness.chain) && record.consumed == witness.consumed &&
		record.canonical == witness.canonical
}

func verifiedAdmissionLifecycleWitnessDigest(witness *verifiedAdmissionLifecycleWitness) [32]byte {
	if witness == nil || witness.self != witness || witness.candidateBinding == nil || witness.generation.owner == nil ||
		witness.generation.owner != witness.candidateBinding.owner || witness.stateCanonical == ([32]byte{}) || witness.consumed == nil {
		return [32]byte{}
	}
	chain := verifiedAdmissionLifecycleChainDigest(witness.chain)
	return admissionLifecycleIdentityDigest(witness.candidateBinding, witness.generation, witness.stateCanonical, chain, "cloud-agents-platform-admission-lifecycle-witness/v1\x00")
}

func admissionLifecycleEvidenceDigest(evidence *admissionLifecycleEvidence) [32]byte {
	if evidence == nil || evidence.self != evidence || evidence.active == nil {
		return [32]byte{}
	}
	return admissionLifecycleIdentityDigest(evidence.candidateBinding, evidence.generation, evidence.stateCanonical, evidence.chainCanonical, "cloud-agents-platform-admission-lifecycle-evidence/v1\x00")
}

func admissionLifecycleIdentityDigest(candidateBinding *verifiedEvidenceRunBinding, generation generationIdentity, stateCanonical, chain [32]byte, domain string) [32]byte {
	if candidateBinding == nil || generation.owner == nil || generation.owner != candidateBinding.owner || stateCanonical == ([32]byte{}) || chain == ([32]byte{}) {
		return [32]byte{}
	}
	h := sha256.New()
	h.Write([]byte(domain))
	h.Write(candidateBinding.canonical[:])
	for _, value := range []Digest{
		generation.executionLineageDigest, generation.journalIdentityDigest,
		generation.runnerProjectionDecisionDigest, generation.schemaBundleDigest,
	} {
		if value.Validate() != nil {
			return [32]byte{}
		}
		writeAdmissionString(h, value.String())
	}
	h.Write(stateCanonical[:])
	h.Write(chain[:])
	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result
}

func (evidence *admissionLifecycleEvidence) matches(lineage admissionReplayLineage, generation *admissionReplayGeneration) bool {
	return validAdmissionLifecycleEvidence(evidence, evidenceCandidateBinding(evidence)) &&
		evidence.stateCanonical != ([32]byte{}) && evidence.chainCanonical != ([32]byte{}) &&
		lineage.id == digestRaw(evidence.generation.executionLineageDigest) && generation != nil &&
		generation.journalID == evidence.generation.journalIdentityDigest &&
		generation.runnerProjectionDecisionDigest == evidence.generation.runnerProjectionDecisionDigest &&
		generation.schemaBundleDigest == evidence.generation.schemaBundleDigest &&
		verifiedAdmissionLifecycleChainDigest(evidence.chain) == evidence.chainCanonical
}

func evidenceCandidateBinding(evidence *admissionLifecycleEvidence) *verifiedEvidenceRunBinding {
	if evidence == nil {
		return nil
	}
	return evidence.candidateBinding
}

func admissionLifecycleRetryMatches(evidence *admissionLifecycleEvidence, generation *admissionReplayGeneration, event admissionReplayTerminalEvent, compact admissionReplayTerminalRetry, commit *admissionReplayTerminalCommit) bool {
	if !validAdmissionLifecycleEvidence(evidence, evidenceCandidateBinding(evidence)) || generation == nil || event.terminalDigest == ([32]byte{}) || compact.ordinal >= uint32(len(generation.verificationTerminals)) {
		return false
	}
	terminal := digestString(event.terminalDigest)
	receipt, ok := evidence.chain.retryReceipts[terminal]
	if !ok || receipt == nil {
		return false
	}
	migration := admissionMigrationString(event.migrationID)
	matchIdentity := func(identity ownedRetryIdentity) bool {
		return identity.migrationID == migration && identity.attemptIndex == event.attemptIndex &&
			identity.executionLineageDigest == evidence.generation.executionLineageDigest &&
			identity.journalIdentityDigest == evidence.generation.journalIdentityDigest
	}
	matchPredecessor := func(value ownedRecoveryPredecessorReceipt) bool {
		return matchIdentity(value.identity) && digestRaw(value.attemptPredecessorCatalog) == compact.attemptPredecessorCatalog &&
			digestRaw(value.observedCatalogDigest) == compact.observedCatalog && digestRaw(value.ledgerPrefixDigest) == compact.ledgerPrefix &&
			digestRaw(value.authorityResultDigest) == compact.authorityResult
	}
	switch value := receipt.(type) {
	case verifiedRollbackRetry:
		code, _, err := admissionRetryProofCodes(RetryProofEvidence{
			ProofKind: value.proofKind, AttemptPredecessorCatalogDigest: value.predecessor.attemptPredecessorCatalog,
			ObservedCatalogDigest: value.predecessor.observedCatalogDigest, LedgerPrefixDigest: value.predecessor.ledgerPrefixDigest,
			AuthorityResultDigest: value.predecessor.authorityResultDigest,
		})
		return err == nil && compact.proofKind == code && compact.commitRejectedReason == 0 && matchIdentity(value.old.identity) && matchPredecessor(value.predecessor)
	case verifiedPrecommitTerminatedRetry:
		return compact.proofKind == 3 && compact.commitRejectedReason == 0 && matchIdentity(value.old.identity) && matchPredecessor(value.predecessor)
	case verifiedCommitRejectedRetry:
		reason := uint8(0)
		for index, candidate := range []string{"serialization_failure", "deadlock_detected", "other_confirmed_postgres_error"} {
			if value.old.commitRejectedReason == candidate {
				reason = uint8(index + 1)
				break
			}
		}
		return compact.proofKind == 4 && reason != 0 && compact.commitRejectedReason == reason && commit != nil &&
			commit.commitRecord == digestRaw(value.old.commitIntentRecordDigest) && matchIdentity(value.old.identity) && matchPredecessor(value.predecessor)
	default:
		return false
	}
}

func admissionLifecycleAmbiguousMatches(evidence *admissionLifecycleEvidence, generation *admissionReplayGeneration, event admissionReplayTerminalEvent, final *admissionReplayTerminalFinal, commit *admissionReplayTerminalCommit) bool {
	if !validAdmissionLifecycleEvidence(evidence, evidenceCandidateBinding(evidence)) || generation == nil || event.terminalDigest == ([32]byte{}) || event.outcome < 4 || final == nil || commit == nil {
		return false
	}
	boundary, ok := evidence.chain.ambiguousBoundaries[digestString(event.terminalDigest)]
	return ok && boundary.commitCalled && boundary.migrationID == admissionMigrationString(event.migrationID) &&
		boundary.attemptIndex == event.attemptIndex && digestRaw(boundary.finalIntermediateRecordDigest) == final.lastIntermediateRecord &&
		digestRaw(boundary.commitIntentRecordDigest) == commit.commitRecord
}

func cloneAdmissionLifecycleChainWitness(chain verifiedEvidenceChainWitness) verifiedEvidenceChainWitness {
	owned := cloneRunnerEvidenceChainWitness(chain)
	owned.retryReceipts = make(map[Digest]verifiedRetryReceipt, len(chain.retryReceipts))
	for key, receipt := range chain.retryReceipts {
		owned.retryReceipts[key] = cloneAdmissionRetryReceipt(receipt)
	}
	return owned
}

func cloneAdmissionRetryReceipt(receipt verifiedRetryReceipt) verifiedRetryReceipt {
	clonePair := func(old ownedLifecycleOrderAuthority, predecessor ownedRecoveryPredecessorReceipt) (ownedLifecycleOrderAuthority, ownedRecoveryPredecessorReceipt) {
		var tokens map[*retryLifecycleOrderToken]*retryLifecycleOrderToken
		cloneToken := func(value *retryLifecycleOrderToken) *retryLifecycleOrderToken {
			if value == nil {
				return nil
			}
			if tokens == nil {
				tokens = make(map[*retryLifecycleOrderToken]*retryLifecycleOrderToken, 1)
			}
			if owned := tokens[value]; owned != nil {
				return owned
			}
			owned := *value
			tokens[value] = &owned
			return &owned
		}
		old.token = cloneToken(old.token)
		predecessor.order.token = cloneToken(predecessor.order.token)
		predecessor.ledgerRows = cloneProjectionValue(predecessor.ledgerRows)
		return old, predecessor
	}
	switch value := receipt.(type) {
	case verifiedRollbackRetry:
		value.old.order, value.predecessor = clonePair(value.old.order, value.predecessor)
		return value
	case verifiedPrecommitTerminatedRetry:
		value.old.order, value.predecessor = clonePair(value.old.order, value.predecessor)
		return value
	case verifiedCommitRejectedRetry:
		value.old.order, value.predecessor = clonePair(value.old.order, value.predecessor)
		return value
	default:
		return nil
	}
}

func verifiedAdmissionLifecycleChainDigest(chain verifiedEvidenceChainWitness) [32]byte {
	if chain.runtimeReceipt.kind == "" || chain.runtimeReceipt.digest.Validate() != nil || chain.runtimeReceipt.sizeBytes == 0 ||
		chain.recoveryReceipt.kind == "" || chain.recoveryReceipt.digest.Validate() != nil || chain.recoveryReceipt.sizeBytes == 0 {
		return [32]byte{}
	}
	h := sha256.New()
	h.Write([]byte("cloud-agents-platform-admission-lifecycle-chain/v1\x00"))
	writeAdmissionString(h, chain.runtimeReceipt.kind)
	writeAdmissionString(h, chain.runtimeReceipt.digest.String())
	writeAdmissionUint(h, chain.runtimeReceipt.sizeBytes)
	writeAdmissionString(h, chain.recoveryReceipt.kind)
	writeAdmissionString(h, chain.recoveryReceipt.digest.String())
	writeAdmissionUint(h, chain.recoveryReceipt.sizeBytes)
	keys := make([]string, 0, len(chain.maxAttempts))
	for key := range chain.maxAttempts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		final, finalOK := chain.finalStatementIndex[key]
		catalog, catalogOK := chain.finalCatalogDigest[key]
		if !migrationIDPattern.MatchString(key) || !finalOK || !catalogOK || catalog.Validate() != nil || chain.maxAttempts[key] == 0 {
			return [32]byte{}
		}
		writeAdmissionString(h, key)
		writeAdmissionUint(h, uint64(chain.maxAttempts[key]))
		writeAdmissionUint(h, uint64(final))
		writeAdmissionString(h, catalog.String())
	}
	if len(chain.finalStatementIndex) != len(keys) || len(chain.finalCatalogDigest) != len(keys) {
		return [32]byte{}
	}
	planKeys := make([]string, 0, len(chain.plans))
	for key := range chain.plans {
		planKeys = append(planKeys, key)
	}
	sort.Strings(planKeys)
	for _, key := range planKeys {
		plan := chain.plans[key]
		if !migrationIDPattern.MatchString(plan.migrationID) || plan.attemptIndex == 0 || plan.sqlArtifactSHA256.Validate() != nil ||
			plan.sqlArtifactSizeBytes == 0 || plan.endOffset <= plan.startOffset || plan.statementSHA256.Validate() != nil ||
			plan.classificationCanonical == "" || plan.expectedTransitionDigest.Validate() != nil {
			return [32]byte{}
		}
		writeAdmissionString(h, key)
		writeAdmissionString(h, plan.migrationID)
		writeAdmissionUint(h, uint64(plan.attemptIndex))
		writeAdmissionUint(h, uint64(plan.statementIndex))
		writeAdmissionString(h, plan.sqlArtifactSHA256.String())
		writeAdmissionUint(h, plan.sqlArtifactSizeBytes)
		writeAdmissionUint(h, plan.startOffset)
		writeAdmissionUint(h, plan.endOffset)
		writeAdmissionString(h, plan.statementSHA256.String())
		writeAdmissionString(h, plan.classificationCanonical)
		writeAdmissionString(h, plan.expectedTransitionDigest.String())
	}
	retryKeys := sortedAdmissionDigestKeys(chain.retryReceipts)
	for _, key := range retryKeys {
		digest := verifiedAdmissionRetryReceiptDigest(chain.retryReceipts[key])
		if key.Validate() != nil || digest == ([32]byte{}) {
			return [32]byte{}
		}
		writeAdmissionString(h, key.String())
		h.Write(digest[:])
	}
	ambiguousKeys := sortedAdmissionDigestKeys(chain.ambiguousBoundaries)
	for _, key := range ambiguousKeys {
		value := chain.ambiguousBoundaries[key]
		if key.Validate() != nil || !migrationIDPattern.MatchString(value.migrationID) || value.attemptIndex == 0 || !value.commitCalled ||
			value.finalIntermediateRecordDigest.Validate() != nil || value.commitIntentRecordDigest.Validate() != nil {
			return [32]byte{}
		}
		writeAdmissionString(h, key.String())
		writeAdmissionString(h, value.migrationID)
		writeAdmissionUint(h, uint64(value.attemptIndex))
		writeAdmissionString(h, value.finalIntermediateRecordDigest.String())
		writeAdmissionString(h, value.commitIntentRecordDigest.String())
	}
	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result
}

func sortedAdmissionDigestKeys[T any](values map[Digest]T) []Digest {
	keys := make([]Digest, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(left, right int) bool { return keys[left] < keys[right] })
	return keys
}

func verifiedAdmissionRetryReceiptDigest(receipt verifiedRetryReceipt) [32]byte {
	h := sha256.New()
	h.Write([]byte("cloud-agents-platform-admission-retry-receipt/v1\x00"))
	writeIdentity := func(value ownedRetryIdentity) bool {
		if !migrationIDPattern.MatchString(value.migrationID) || value.attemptIndex == 0 ||
			value.executionLineageDigest.Validate() != nil || value.journalIdentityDigest.Validate() != nil {
			return false
		}
		writeAdmissionString(h, value.migrationID)
		writeAdmissionUint(h, uint64(value.attemptIndex))
		writeAdmissionString(h, value.executionLineageDigest.String())
		writeAdmissionString(h, value.journalIdentityDigest.String())
		return true
	}
	writeOrder := func(value ownedLifecycleOrderAuthority) bool {
		if value.token == nil || value.ordinal == 0 {
			return false
		}
		h.Write(value.token.verifierNonce[:])
		writeAdmissionUint(h, value.ordinal)
		return true
	}
	writePredecessor := func(value ownedRecoveryPredecessorReceipt) bool {
		if !writeIdentity(value.identity) || value.newLifecycleID == "" || !writeOrder(value.order) ||
			value.ledgerPrefixDigest.Validate() != nil || value.attemptPredecessorCatalog.Validate() != nil ||
			value.observedCatalogDigest.Validate() != nil || value.authorityResultDigest.Validate() != nil {
			return false
		}
		writeAdmissionString(h, value.newLifecycleID)
		writeAdmissionUint(h, uint64(len(value.ledgerRows)))
		for _, row := range value.ledgerRows {
			canonical, err := canonicalContractKey(row)
			if err != nil {
				return false
			}
			writeAdmissionString(h, canonical)
		}
		for _, digest := range []Digest{value.ledgerPrefixDigest, value.attemptPredecessorCatalog, value.observedCatalogDigest, value.authorityResultDigest} {
			writeAdmissionString(h, digest.String())
		}
		return true
	}
	valid := false
	switch value := receipt.(type) {
	case verifiedRollbackRetry:
		writeAdmissionString(h, "rollback")
		writeAdmissionString(h, value.proofKind)
		valid = writeIdentity(value.old.identity) && value.old.oldLifecycleID != "" && writeOrder(value.old.order) && value.old.rollbackSucceeded && value.old.oldHandleClosed
		writeAdmissionString(h, value.old.oldLifecycleID)
		valid = valid && writePredecessor(value.predecessor)
	case verifiedPrecommitTerminatedRetry:
		writeAdmissionString(h, "precommit-terminated")
		valid = writeIdentity(value.old.identity) && value.old.oldLifecycleID != "" && writeOrder(value.old.order) && value.old.oldHandleClosed
		writeAdmissionString(h, value.old.oldLifecycleID)
		valid = valid && writePredecessor(value.predecessor)
	case verifiedCommitRejectedRetry:
		writeAdmissionString(h, "commit-rejected")
		valid = writeIdentity(value.old.identity) && value.old.oldLifecycleID != "" && writeOrder(value.old.order) &&
			value.old.oldHandleClosed && value.old.readyForQuery && value.old.commitRejectedReason != "" && value.old.commitIntentRecordDigest.Validate() == nil
		writeAdmissionString(h, value.old.oldLifecycleID)
		writeAdmissionString(h, value.old.commitRejectedReason)
		writeAdmissionString(h, value.old.commitIntentRecordDigest.String())
		valid = valid && writePredecessor(value.predecessor)
	}
	if !valid {
		return [32]byte{}
	}
	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result
}
