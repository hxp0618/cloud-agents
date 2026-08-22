package migration

import (
	"context"
	"crypto/sha256"
	"sync"
	"sync/atomic"
)

const runnerLedgerRecoveryAdmissionClaimDigestDomain = "cloud-agents/runner-ledger-recovery-admission/claim/v1"

// runnerLedgerRecoveryAdmissionClaimBinder is implemented only by the
// concrete retained generation session. Both transitions perform a fresh
// generation-lease -> full-root admission replay before returning ordinary
// boundary facts.
type runnerLedgerRecoveryAdmissionClaimBinder interface {
	bindRunnerLedgerRecoveryAdmissionClaim(context.Context, runnerLedgerRecoveryAdmissionClaimRequest) (*runnerLedgerRecoveryAdmissionClaim, error)
	consumeRunnerLedgerRecoveryAdmissionClaim(context.Context, *runnerLedgerRecoveryAdmissionClaim, OwnedCurrentCandidate) (runnerLedgerRecoveryAdmissionEvidenceBoundary, error)
	runnerLedgerRecoveryAdmissionClaimBinderSealed()
}

type runnerLedgerRecoveryAdmissionClaimRequest struct {
	fact      runnerLedgerConsumerFact
	candidate OwnedCurrentCandidate
}

type runnerLedgerRecoveryEvidenceFacts struct {
	binder              runnerLedgerRecoveryAdmissionClaimBinder
	candidateBinding    *verifiedEvidenceRunBinding
	generation          generationIdentity
	schema              verifiedRecoverySchemaWitness
	recovery            *RecoverySnapshot
	schemaDigest        [32]byte
	recoveryDigest      [32]byte
	stateCanonical      [32]byte
	sessionDigest       [32]byte
	journalDigest       [32]byte
	fullSet             [32]byte
	transcriptCanonical [32]byte
	revision            uint64
	target              [32]byte
	indexRecords        uint64
	indexTail           Digest
}

type runnerLedgerRecoveryAdmissionEvidenceBoundary struct {
	candidateBinding    *verifiedEvidenceRunBinding
	generation          generationIdentity
	factSubject         Digest
	action              runnerLedgerRecoveryAction
	schemaDigest        [32]byte
	recoveryDigest      [32]byte
	stateCanonical      [32]byte
	sessionDigest       [32]byte
	journalDigest       [32]byte
	fullSet             [32]byte
	transcriptCanonical [32]byte
	revision            uint64
	target              [32]byte
	indexRecords        uint64
	indexTail           Digest
	recoveryTail        Digest
	claimDigest         [32]byte
	canonical           [32]byte
}

type runnerLedgerRecoveryAdmissionClaim struct {
	self                *runnerLedgerRecoveryAdmissionClaim
	binding             *runnerLedgerRecoveryAdmissionClaimBinding
	candidateBinding    *verifiedEvidenceRunBinding
	generation          generationIdentity
	fact                runnerLedgerConsumerFact
	action              runnerLedgerRecoveryAction
	schemaDigest        [32]byte
	recoveryDigest      [32]byte
	stateCanonical      [32]byte
	sessionDigest       [32]byte
	journalDigest       [32]byte
	fullSet             [32]byte
	transcriptCanonical [32]byte
	revision            uint64
	target              [32]byte
	indexRecords        uint64
	indexTail           Digest
	use                 *runnerLedgerRecoveryAdmissionUseRecord
	consumed            *atomic.Bool
	canonical           [32]byte
}

type runnerLedgerRecoveryAdmissionClaimBinding struct {
	claim            *runnerLedgerRecoveryAdmissionClaim
	binder           runnerLedgerRecoveryAdmissionClaimBinder
	candidateBinding *verifiedEvidenceRunBinding
	consumed         *atomic.Bool
	use              *runnerLedgerRecoveryAdmissionUseRecord
	canonical        [32]byte
}

type runnerLedgerRecoveryAdmissionClaimRegistryRecord struct {
	claim            *runnerLedgerRecoveryAdmissionClaim
	binding          *runnerLedgerRecoveryAdmissionClaimBinding
	binder           runnerLedgerRecoveryAdmissionClaimBinder
	candidateBinding *verifiedEvidenceRunBinding
	consumed         *atomic.Bool
	use              *runnerLedgerRecoveryAdmissionUseRecord
	canonical        [32]byte
}

type runnerLedgerRecoveryAdmissionUseRecord struct {
	mu          sync.Mutex
	factSubject Digest
	action      runnerLedgerRecoveryAction
	consumed    bool
	boundary    [32]byte
}

var (
	runnerLedgerRecoveryAdmissionClaimRegistry     sync.Map
	runnerLedgerRecoveryAdmissionUseByEvidenceBind sync.Map
)

func bindRunnerLedgerRecoveryAdmissionClaimFromEvidence(ctx context.Context, request runnerLedgerRecoveryAdmissionClaimRequest, facts runnerLedgerRecoveryEvidenceFacts) (*runnerLedgerRecoveryAdmissionClaim, error) {
	if err := contextAdmissionError(ctx); err != nil {
		return nil, err
	}
	action, ok := runnerLedgerRecoveryFactAction(request.fact, request.candidate, facts)
	if !ok || !validRunnerLedgerRecoveryEvidenceFacts(facts, request.candidate.binding) {
		return nil, fail(CodeEvidenceRecoveryRequired, "runner-ledger-recovery-admission-claim", "same-verifier recovery evidence is unavailable or changed", nil)
	}
	use := &runnerLedgerRecoveryAdmissionUseRecord{factSubject: request.fact.subjectDigest, action: action}
	if _, loaded := runnerLedgerRecoveryAdmissionUseByEvidenceBind.LoadOrStore(facts.binder, use); loaded {
		return nil, fail(CodeEvidenceRecoveryRequired, "runner-ledger-recovery-admission-claim", "recovery consumer fact already entered admission", nil)
	}
	claim := &runnerLedgerRecoveryAdmissionClaim{
		candidateBinding: request.candidate.binding, generation: facts.generation, fact: request.fact.clone(), action: action,
		schemaDigest: facts.schemaDigest, recoveryDigest: facts.recoveryDigest, stateCanonical: facts.stateCanonical,
		sessionDigest: facts.sessionDigest, journalDigest: facts.journalDigest, fullSet: facts.fullSet,
		transcriptCanonical: facts.transcriptCanonical, revision: facts.revision, target: facts.target,
		indexRecords: facts.indexRecords, indexTail: facts.indexTail, use: use, consumed: &atomic.Bool{},
	}
	claim.self = claim
	claim.binding = &runnerLedgerRecoveryAdmissionClaimBinding{
		claim: claim, binder: facts.binder, candidateBinding: request.candidate.binding, consumed: claim.consumed, use: use,
	}
	claim.canonical = runnerLedgerRecoveryAdmissionClaimDigest(claim)
	claim.binding.canonical = claim.canonical
	if claim.canonical == ([32]byte{}) {
		revokeRunnerLedgerRecoveryAdmissionClaim(claim)
		return nil, fail(CodeEvidenceRecoveryRequired, "runner-ledger-recovery-admission-claim", "recovery admission claim could not be identified", nil)
	}
	runnerLedgerRecoveryAdmissionClaimRegistry.Store(claim, runnerLedgerRecoveryAdmissionClaimRegistryRecord{
		claim: claim, binding: claim.binding, binder: facts.binder, candidateBinding: request.candidate.binding,
		consumed: claim.consumed, use: use, canonical: claim.canonical,
	})
	if !validRunnerLedgerRecoveryAdmissionClaim(claim, facts.binder, request.candidate.binding) {
		revokeRunnerLedgerRecoveryAdmissionClaim(claim)
		return nil, fail(CodeEvidenceRecoveryRequired, "runner-ledger-recovery-admission-claim", "recovery admission claim could not be sealed", nil)
	}
	return claim, nil
}

func consumeRunnerLedgerRecoveryAdmissionClaimFromEvidence(ctx context.Context, claim *runnerLedgerRecoveryAdmissionClaim, candidate OwnedCurrentCandidate, facts runnerLedgerRecoveryEvidenceFacts) (runnerLedgerRecoveryAdmissionEvidenceBoundary, error) {
	var boundary runnerLedgerRecoveryAdmissionEvidenceBoundary
	if err := contextAdmissionError(ctx); err != nil {
		return boundary, err
	}
	if !validOwnedCurrentCandidate(candidate) || !validRunnerLedgerRecoveryEvidenceFacts(facts, candidate.binding) ||
		!validRunnerLedgerRecoveryAdmissionClaim(claim, facts.binder, candidate.binding) {
		revokeRunnerLedgerRecoveryAdmissionClaim(claim)
		return boundary, fail(CodeEvidenceRecoveryRequired, "runner-ledger-recovery-admission-claim", "recovery admission claim is unavailable or changed", nil)
	}
	action, ok := runnerLedgerRecoveryFactAction(claim.fact, candidate, facts)
	if !ok || action != claim.action || !sameGenerationIdentity(claim.generation, facts.generation) ||
		claim.schemaDigest != facts.schemaDigest || claim.recoveryDigest != facts.recoveryDigest ||
		claim.stateCanonical != facts.stateCanonical || claim.sessionDigest != facts.sessionDigest ||
		claim.journalDigest != facts.journalDigest || claim.fullSet != facts.fullSet ||
		claim.transcriptCanonical != facts.transcriptCanonical || claim.revision != facts.revision ||
		claim.target != facts.target || claim.indexRecords != facts.indexRecords || claim.indexTail != facts.indexTail {
		revokeRunnerLedgerRecoveryAdmissionClaim(claim)
		return boundary, fail(CodeEvidenceRecoveryRequired, "runner-ledger-recovery-admission-claim", "full-root evidence changed during recovery admission", nil)
	}
	if !claim.consumed.CompareAndSwap(false, true) {
		revokeRunnerLedgerRecoveryAdmissionClaim(claim)
		return boundary, fail(CodeEvidenceRecoveryRequired, "runner-ledger-recovery-admission-claim", "recovery admission claim was already consumed", nil)
	}
	runnerLedgerRecoveryAdmissionClaimRegistry.Delete(claim)
	boundary = runnerLedgerRecoveryAdmissionEvidenceBoundary{
		candidateBinding: candidate.binding, generation: facts.generation, factSubject: claim.fact.subjectDigest, action: action,
		schemaDigest: facts.schemaDigest, recoveryDigest: facts.recoveryDigest, stateCanonical: facts.stateCanonical,
		sessionDigest: facts.sessionDigest, journalDigest: facts.journalDigest, fullSet: facts.fullSet,
		transcriptCanonical: facts.transcriptCanonical, revision: facts.revision, target: facts.target,
		indexRecords: facts.indexRecords, indexTail: facts.indexTail, recoveryTail: facts.recovery.tailDigest,
		claimDigest: claim.canonical,
	}
	boundary.canonical = runnerLedgerRecoveryAdmissionEvidenceBoundaryDigest(boundary)
	if boundary.canonical == ([32]byte{}) || !claim.use.consume(claim.fact.subjectDigest, action, boundary.canonical) {
		return runnerLedgerRecoveryAdmissionEvidenceBoundary{}, fail(CodeEvidenceRecoveryRequired, "runner-ledger-recovery-admission-claim", "consumed recovery boundary could not be sealed", nil)
	}
	return boundary, nil
}

func validRunnerLedgerRecoveryEvidenceFacts(facts runnerLedgerRecoveryEvidenceFacts, candidateBinding *verifiedEvidenceRunBinding) bool {
	return facts.binder != nil && runnerOwnedPointer(facts.binder) && candidateBinding != nil && facts.candidateBinding == candidateBinding &&
		facts.generation.owner != nil && facts.generation.owner == candidateBinding.owner && facts.target == digestRaw(facts.generation.executionLineageDigest) &&
		facts.schemaDigest != ([32]byte{}) && facts.schemaDigest == generationJournalSchemaDigest(facts.schema, facts.generation) &&
		facts.recovery != nil && validRecoverySnapshotForJournal(facts.recovery, facts.generation, facts.recovery.cursor) &&
		facts.recoveryDigest != ([32]byte{}) && facts.recoveryDigest == generationJournalRecoveryDigest(facts.recovery) &&
		facts.stateCanonical != ([32]byte{}) && facts.sessionDigest != ([32]byte{}) && facts.journalDigest != ([32]byte{}) &&
		facts.fullSet != ([32]byte{}) && facts.transcriptCanonical != ([32]byte{}) && facts.revision == 0 &&
		facts.indexRecords != 0 && facts.indexTail.Validate() == nil
}

func runnerLedgerRecoveryFactAction(fact runnerLedgerConsumerFact, candidate OwnedCurrentCandidate, facts runnerLedgerRecoveryEvidenceFacts) (runnerLedgerRecoveryAction, bool) {
	if !validOwnedCurrentCandidate(candidate) || !validRunnerLedgerRecoveryEvidenceFacts(facts, candidate.binding) || !fact.valid() ||
		!validGeneratedRunnerLedgerRecoveryProfiles() || fact.manifestDigest != candidate.verifiedRun.manifestDigest ||
		fact.dispatch.fact.recovery == nil || fact.dispatch.fact.schemaBundleDigest != facts.generation.schemaBundleDigest ||
		fact.dispatch.fact.executionLineageDigest != facts.generation.executionLineageDigest ||
		fact.dispatch.journalIdentityDigest != facts.generation.journalIdentityDigest ||
		fact.dispatch.runnerProjectionDecisionDigest != facts.generation.runnerProjectionDecisionDigest ||
		fact.dispatch.recoverySnapshotDigest != digestString(facts.recoveryDigest) ||
		fact.dispatch.recoveryTailDigest != facts.recovery.tailDigest ||
		fact.dispatch.fact.recovery.State != facts.recovery.state || fact.dispatch.fact.recovery.Action != facts.recovery.nextPermittedAction ||
		!sameOptionalString(fact.dispatch.recoveryMigrationID, facts.recovery.migrationID) ||
		!sameOptionalUint32(fact.dispatch.recoveryAttemptIndex, facts.recovery.attemptIndex) {
		return "", false
	}
	action, ok := generatedRunnerLedgerRecoveryAdmissionAction(fact.dispatch.fact.disposition, facts.recovery.state, facts.recovery.nextPermittedAction)
	if !ok {
		return "", false
	}
	wantConsumer := runnerLedgerConsumerRecoveryNotImplemented
	if fact.dispatch.kind == runnerLedgerPreflightDispatchEntry {
		wantConsumer = runnerLedgerConsumerEntryNotImplemented
	} else if fact.dispatch.kind != runnerLedgerPreflightDispatchRecovery {
		return "", false
	}
	return action, fact.action == wantConsumer
}

func (record *runnerLedgerRecoveryAdmissionUseRecord) consume(subject Digest, action runnerLedgerRecoveryAction, boundary [32]byte) bool {
	if record == nil || boundary == ([32]byte{}) {
		return false
	}
	record.mu.Lock()
	defer record.mu.Unlock()
	if record.consumed || record.factSubject != subject || record.action != action || record.boundary != ([32]byte{}) {
		return false
	}
	record.consumed = true
	record.boundary = boundary
	return true
}

func validRunnerLedgerRecoveryAdmissionUse(binder runnerLedgerRecoveryAdmissionClaimBinder, expected *runnerLedgerRecoveryAdmissionUseRecord, subject Digest, action runnerLedgerRecoveryAction, boundary [32]byte, consumed bool) bool {
	if binder == nil || expected == nil {
		return false
	}
	value, ok := runnerLedgerRecoveryAdmissionUseByEvidenceBind.Load(binder)
	record, recordOK := value.(*runnerLedgerRecoveryAdmissionUseRecord)
	if !ok || !recordOK || record != expected {
		return false
	}
	record.mu.Lock()
	defer record.mu.Unlock()
	return record.factSubject == subject && record.action == action && record.consumed == consumed && record.boundary == boundary
}

func validRunnerLedgerRecoveryAdmissionClaim(claim *runnerLedgerRecoveryAdmissionClaim, binder runnerLedgerRecoveryAdmissionClaimBinder, candidateBinding *verifiedEvidenceRunBinding) bool {
	if claim == nil || claim.self != claim || claim.binding == nil || claim.binding.claim != claim ||
		!sameRunnerOwnedPointer(claim.binding.binder, binder) || claim.binding.candidateBinding != candidateBinding ||
		claim.binding.consumed != claim.consumed || claim.binding.use != claim.use || claim.consumed == nil || claim.consumed.Load() ||
		claim.candidateBinding != candidateBinding || claim.canonical == ([32]byte{}) || claim.binding.canonical != claim.canonical ||
		claim.canonical != runnerLedgerRecoveryAdmissionClaimDigest(claim) {
		return false
	}
	value, ok := runnerLedgerRecoveryAdmissionClaimRegistry.Load(claim)
	record, recordOK := value.(runnerLedgerRecoveryAdmissionClaimRegistryRecord)
	return ok && recordOK && record.claim == claim && record.binding == claim.binding &&
		sameRunnerOwnedPointer(record.binder, binder) && record.candidateBinding == candidateBinding &&
		record.consumed == claim.consumed && record.use == claim.use && record.canonical == claim.canonical &&
		validRunnerLedgerRecoveryAdmissionUse(binder, claim.use, claim.fact.subjectDigest, claim.action, [32]byte{}, false)
}

func runnerLedgerRecoveryAdmissionClaimDigest(claim *runnerLedgerRecoveryAdmissionClaim) [32]byte {
	if claim == nil || claim.self != claim || claim.binding == nil || claim.candidateBinding == nil || claim.consumed == nil || claim.use == nil ||
		!claim.fact.valid() || claim.generation.owner == nil || claim.generation.owner != claim.candidateBinding.owner ||
		claim.schemaDigest == ([32]byte{}) || claim.recoveryDigest == ([32]byte{}) || claim.stateCanonical == ([32]byte{}) ||
		claim.sessionDigest == ([32]byte{}) || claim.journalDigest == ([32]byte{}) || claim.fullSet == ([32]byte{}) ||
		claim.transcriptCanonical == ([32]byte{}) || claim.revision != 0 || claim.target == ([32]byte{}) ||
		claim.indexRecords == 0 || claim.indexTail.Validate() != nil || !validGeneratedRunnerLedgerRecoveryProfiles() {
		return [32]byte{}
	}
	h := sha256.New()
	h.Write([]byte(runnerLedgerRecoveryAdmissionClaimDigestDomain + "\x00"))
	h.Write(claim.candidateBinding.canonical[:])
	writeRunnerLedgerRecoveryIdentity(h, claim.action)
	for _, value := range []Digest{claim.generation.executionLineageDigest, claim.generation.journalIdentityDigest, claim.generation.runnerProjectionDecisionDigest, claim.generation.schemaBundleDigest, claim.fact.subjectDigest, claim.indexTail} {
		if value.Validate() != nil {
			return [32]byte{}
		}
		writeAdmissionString(h, value.String())
	}
	for _, value := range [][32]byte{claim.schemaDigest, claim.recoveryDigest, claim.stateCanonical, claim.sessionDigest, claim.journalDigest, claim.fullSet, claim.transcriptCanonical, claim.target} {
		h.Write(value[:])
	}
	writeAdmissionUint(h, claim.revision)
	writeAdmissionUint(h, claim.indexRecords)
	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result
}

func runnerLedgerRecoveryAdmissionEvidenceBoundaryDigest(boundary runnerLedgerRecoveryAdmissionEvidenceBoundary) [32]byte {
	if boundary.candidateBinding == nil || boundary.generation.owner == nil || boundary.generation.owner != boundary.candidateBinding.owner ||
		boundary.factSubject.Validate() != nil || boundary.indexTail.Validate() != nil || boundary.recoveryTail.Validate() != nil ||
		boundary.schemaDigest == ([32]byte{}) || boundary.recoveryDigest == ([32]byte{}) || boundary.stateCanonical == ([32]byte{}) ||
		boundary.sessionDigest == ([32]byte{}) || boundary.journalDigest == ([32]byte{}) || boundary.fullSet == ([32]byte{}) ||
		boundary.transcriptCanonical == ([32]byte{}) || boundary.revision != 0 || boundary.target == ([32]byte{}) ||
		boundary.indexRecords == 0 || boundary.claimDigest == ([32]byte{}) || !validGeneratedRunnerLedgerRecoveryProfiles() {
		return [32]byte{}
	}
	h := sha256.New()
	h.Write([]byte("cloud-agents/runner-ledger-recovery-admission/evidence-boundary/v1\x00"))
	h.Write(boundary.candidateBinding.canonical[:])
	writeRunnerLedgerRecoveryIdentity(h, boundary.action)
	for _, value := range []Digest{boundary.generation.executionLineageDigest, boundary.generation.journalIdentityDigest, boundary.generation.runnerProjectionDecisionDigest, boundary.generation.schemaBundleDigest, boundary.factSubject, boundary.indexTail, boundary.recoveryTail} {
		if value.Validate() != nil {
			return [32]byte{}
		}
		writeAdmissionString(h, value.String())
	}
	for _, value := range [][32]byte{boundary.schemaDigest, boundary.recoveryDigest, boundary.stateCanonical, boundary.sessionDigest, boundary.journalDigest, boundary.fullSet, boundary.transcriptCanonical, boundary.target, boundary.claimDigest} {
		h.Write(value[:])
	}
	writeAdmissionUint(h, boundary.revision)
	writeAdmissionUint(h, boundary.indexRecords)
	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result
}

func writeRunnerLedgerRecoveryIdentity(h interface{ Write([]byte) (int, error) }, action runnerLedgerRecoveryAction) {
	writeAdmissionString(h, string(action))
	for index := range generatedRunnerLedgerRecoveryProfiles {
		profile := generatedRunnerLedgerRecoveryProfiles[index]
		for _, value := range []string{profile.family, string(profile.action), profile.registryID, profile.registryDigest, profile.profileID, profile.profileDigest, profile.stateMachineDigest, profile.policyDigest} {
			writeAdmissionString(h, value)
		}
	}
}

func revokeRunnerLedgerRecoveryAdmissionClaim(claim *runnerLedgerRecoveryAdmissionClaim) {
	if claim == nil {
		return
	}
	value, ok := runnerLedgerRecoveryAdmissionClaimRegistry.LoadAndDelete(claim)
	record, recordOK := value.(runnerLedgerRecoveryAdmissionClaimRegistryRecord)
	if ok && recordOK && record.consumed != nil {
		record.consumed.Store(true)
	} else if claim.consumed != nil {
		claim.consumed.Store(true)
	}
}

func revokeRunnerLedgerRecoveryAdmissionClaims(binder runnerLedgerRecoveryAdmissionClaimBinder) {
	if binder == nil || !runnerOwnedPointer(binder) {
		return
	}
	runnerLedgerRecoveryAdmissionUseByEvidenceBind.Delete(binder)
	runnerLedgerRecoveryAdmissionClaimRegistry.Range(func(key, value any) bool {
		record, ok := value.(runnerLedgerRecoveryAdmissionClaimRegistryRecord)
		if ok && sameRunnerOwnedPointer(record.binder, binder) {
			runnerLedgerRecoveryAdmissionClaimRegistry.Delete(key)
			if record.consumed != nil {
				record.consumed.Store(true)
			}
		}
		return true
	})
}
