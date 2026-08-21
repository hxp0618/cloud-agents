package migration

import (
	"context"
	"crypto/sha256"
	"sync"
	"sync/atomic"
)

const runnerLedgerEntryAdmissionClaimDigestDomain = "cloud-agents/runner-ledger-entry-admission/claim/v1"

// runnerLedgerEntryAdmissionClaimBinder is implemented only by the retained
// same-verifier generation evidence session. It brackets the fresh database
// observation with two exact evidence reads without exposing an evidence
// mutation port to the runner service.
type runnerLedgerEntryAdmissionClaimBinder interface {
	bindRunnerLedgerEntryAdmissionClaim(context.Context, runnerLedgerEntryAdmissionClaimRequest) (*runnerLedgerEntryAdmissionClaim, error)
	consumeRunnerLedgerEntryAdmissionClaim(context.Context, *runnerLedgerEntryAdmissionClaim, OwnedCurrentCandidate) (runnerLedgerEntryAdmissionEvidenceBoundary, error)
	runnerLedgerEntryAdmissionClaimBinderSealed()
}

type runnerLedgerEntryAdmissionClaimRequest struct {
	fact      runnerLedgerConsumerFact
	candidate OwnedCurrentCandidate
}

type runnerLedgerEntryAdmissionEvidenceFacts struct {
	binder           runnerLedgerEntryAdmissionClaimBinder
	candidateBinding *verifiedEvidenceRunBinding
	generation       generationIdentity
	schema           verifiedRecoverySchemaWitness
	recovery         *RecoverySnapshot
	schemaDigest     [32]byte
	recoveryDigest   [32]byte
	sessionDigest    [32]byte
	journalDigest    [32]byte
}

// runnerLedgerEntryAdmissionEvidenceBoundary is ordinary immutable-by-
// convention data returned only inside permit binding after the one-shot claim
// has been consumed. It cannot open a session, transaction, or evidence writer.
type runnerLedgerEntryAdmissionEvidenceBoundary struct {
	candidateBinding *verifiedEvidenceRunBinding
	generation       generationIdentity
	factSubject      Digest
	schemaDigest     [32]byte
	recoveryDigest   [32]byte
	sessionDigest    [32]byte
	journalDigest    [32]byte
	recoveryTail     Digest
	canonical        [32]byte
}

type runnerLedgerEntryAdmissionClaim struct {
	self             *runnerLedgerEntryAdmissionClaim
	binding          *runnerLedgerEntryAdmissionClaimBinding
	candidateBinding *verifiedEvidenceRunBinding
	generation       generationIdentity
	fact             runnerLedgerConsumerFact
	schemaDigest     [32]byte
	recoveryDigest   [32]byte
	sessionDigest    [32]byte
	journalDigest    [32]byte
	use              *runnerLedgerEntryAdmissionUseRecord
	consumed         *atomic.Bool
	canonical        [32]byte
}

type runnerLedgerEntryAdmissionClaimBinding struct {
	claim            *runnerLedgerEntryAdmissionClaim
	binder           runnerLedgerEntryAdmissionClaimBinder
	candidateBinding *verifiedEvidenceRunBinding
	consumed         *atomic.Bool
	use              *runnerLedgerEntryAdmissionUseRecord
	canonical        [32]byte
}

type runnerLedgerEntryAdmissionClaimRegistryRecord struct {
	claim            *runnerLedgerEntryAdmissionClaim
	binding          *runnerLedgerEntryAdmissionClaimBinding
	binder           runnerLedgerEntryAdmissionClaimBinder
	candidateBinding *verifiedEvidenceRunBinding
	consumed         *atomic.Bool
	use              *runnerLedgerEntryAdmissionUseRecord
	canonical        [32]byte
}

type runnerLedgerEntryAdmissionUseRecord struct {
	mu          sync.Mutex
	factSubject Digest
	consumed    bool
	boundary    [32]byte
}

var (
	runnerLedgerEntryAdmissionClaimRegistry sync.Map
	// A use record is intentionally retained after claim failure, consumption,
	// and permit close. The earlier ordinary fact cannot drive a second attempt;
	// the record is removed only when its evidence session is closed.
	runnerLedgerEntryAdmissionUseByEvidenceBinder sync.Map
)

func bindRunnerLedgerEntryAdmissionClaimFromEvidence(ctx context.Context, request runnerLedgerEntryAdmissionClaimRequest, facts runnerLedgerEntryAdmissionEvidenceFacts) (*runnerLedgerEntryAdmissionClaim, error) {
	if err := contextAdmissionError(ctx); err != nil {
		return nil, err
	}
	if !validOwnedCurrentCandidate(request.candidate) || !validRunnerLedgerEntryAdmissionEvidenceFacts(facts, request.candidate.binding) ||
		!runnerLedgerEntryAdmissionFactMatchesEvidence(request.fact, request.candidate, facts) {
		return nil, fail(CodeEvidenceRecoveryRequired, "runner-ledger-entry-admission-claim", "same-verifier entry evidence is unavailable or changed", nil)
	}
	use := &runnerLedgerEntryAdmissionUseRecord{factSubject: request.fact.subjectDigest}
	if _, loaded := runnerLedgerEntryAdmissionUseByEvidenceBinder.LoadOrStore(facts.binder, use); loaded {
		return nil, fail(CodeEvidenceRecoveryRequired, "runner-ledger-entry-admission-claim", "entry consumer fact already entered admission", nil)
	}
	claim := &runnerLedgerEntryAdmissionClaim{
		candidateBinding: request.candidate.binding, generation: facts.generation, fact: request.fact.clone(),
		schemaDigest: facts.schemaDigest, recoveryDigest: facts.recoveryDigest,
		sessionDigest: facts.sessionDigest, journalDigest: facts.journalDigest, use: use, consumed: &atomic.Bool{},
	}
	claim.self = claim
	claim.binding = &runnerLedgerEntryAdmissionClaimBinding{
		claim: claim, binder: facts.binder, candidateBinding: request.candidate.binding, consumed: claim.consumed, use: use,
	}
	claim.canonical = runnerLedgerEntryAdmissionClaimDigest(claim)
	claim.binding.canonical = claim.canonical
	if claim.canonical == ([32]byte{}) {
		revokeRunnerLedgerEntryAdmissionClaim(claim)
		return nil, fail(CodeEvidenceRecoveryRequired, "runner-ledger-entry-admission-claim", "entry admission claim could not be identified", nil)
	}
	runnerLedgerEntryAdmissionClaimRegistry.Store(claim, runnerLedgerEntryAdmissionClaimRegistryRecord{
		claim: claim, binding: claim.binding, binder: facts.binder, candidateBinding: request.candidate.binding,
		consumed: claim.consumed, use: use, canonical: claim.canonical,
	})
	if !validRunnerLedgerEntryAdmissionClaim(claim, facts.binder, request.candidate.binding) {
		revokeRunnerLedgerEntryAdmissionClaim(claim)
		return nil, fail(CodeEvidenceRecoveryRequired, "runner-ledger-entry-admission-claim", "entry admission claim could not be sealed", nil)
	}
	if err := contextAdmissionError(ctx); err != nil {
		revokeRunnerLedgerEntryAdmissionClaim(claim)
		return nil, err
	}
	return claim, nil
}

func consumeRunnerLedgerEntryAdmissionClaimFromEvidence(ctx context.Context, claim *runnerLedgerEntryAdmissionClaim, candidate OwnedCurrentCandidate, facts runnerLedgerEntryAdmissionEvidenceFacts) (runnerLedgerEntryAdmissionEvidenceBoundary, error) {
	if err := contextAdmissionError(ctx); err != nil {
		return runnerLedgerEntryAdmissionEvidenceBoundary{}, err
	}
	if !validOwnedCurrentCandidate(candidate) || !validRunnerLedgerEntryAdmissionEvidenceFacts(facts, candidate.binding) ||
		!validRunnerLedgerEntryAdmissionClaim(claim, facts.binder, candidate.binding) {
		if claim != nil && claim.self == claim {
			revokeRunnerLedgerEntryAdmissionClaim(claim)
		}
		return runnerLedgerEntryAdmissionEvidenceBoundary{}, fail(CodeEvidenceRecoveryRequired, "runner-ledger-entry-admission-claim", "entry admission claim is unavailable or changed", nil)
	}
	if !runnerLedgerEntryAdmissionFactMatchesEvidence(claim.fact, candidate, facts) ||
		!sameGenerationIdentity(claim.generation, facts.generation) || claim.schemaDigest != facts.schemaDigest ||
		claim.recoveryDigest != facts.recoveryDigest || claim.sessionDigest != facts.sessionDigest || claim.journalDigest != facts.journalDigest {
		revokeRunnerLedgerEntryAdmissionClaim(claim)
		return runnerLedgerEntryAdmissionEvidenceBoundary{}, fail(CodeEvidenceRecoveryRequired, "runner-ledger-entry-admission-claim", "evidence changed during fresh-session revalidation", nil)
	}
	if !claim.consumed.CompareAndSwap(false, true) {
		revokeRunnerLedgerEntryAdmissionClaim(claim)
		return runnerLedgerEntryAdmissionEvidenceBoundary{}, fail(CodeEvidenceRecoveryRequired, "runner-ledger-entry-admission-claim", "entry admission claim was already consumed", nil)
	}
	runnerLedgerEntryAdmissionClaimRegistry.Delete(claim)
	boundary := runnerLedgerEntryAdmissionEvidenceBoundary{
		candidateBinding: candidate.binding, generation: facts.generation, factSubject: claim.fact.subjectDigest,
		schemaDigest: facts.schemaDigest, recoveryDigest: facts.recoveryDigest,
		sessionDigest: facts.sessionDigest, journalDigest: facts.journalDigest, recoveryTail: facts.recovery.tailDigest,
	}
	boundary.canonical = runnerLedgerEntryAdmissionEvidenceBoundaryDigest(boundary)
	if boundary.canonical == ([32]byte{}) {
		return runnerLedgerEntryAdmissionEvidenceBoundary{}, fail(CodeEvidenceRecoveryRequired, "runner-ledger-entry-admission-claim", "consumed evidence boundary could not be identified", nil)
	}
	if !claim.use.consumeBoundary(claim.fact.subjectDigest, boundary.canonical) {
		return runnerLedgerEntryAdmissionEvidenceBoundary{}, fail(CodeEvidenceRecoveryRequired, "runner-ledger-entry-admission-claim", "consumed evidence boundary could not be registered", nil)
	}
	return boundary, nil
}

func (record *runnerLedgerEntryAdmissionUseRecord) claimMatches(subject Digest) bool {
	if record == nil {
		return false
	}
	record.mu.Lock()
	defer record.mu.Unlock()
	return !record.consumed && record.boundary == ([32]byte{}) && record.factSubject == subject
}

func (record *runnerLedgerEntryAdmissionUseRecord) consumeBoundary(subject Digest, boundary [32]byte) bool {
	if record == nil || boundary == ([32]byte{}) {
		return false
	}
	record.mu.Lock()
	defer record.mu.Unlock()
	if record.consumed || record.boundary != ([32]byte{}) || record.factSubject != subject {
		return false
	}
	record.consumed = true
	record.boundary = boundary
	return true
}

func validRunnerLedgerEntryAdmissionUse(binder runnerLedgerEntryAdmissionClaimBinder, expected *runnerLedgerEntryAdmissionUseRecord, subject Digest, boundary [32]byte, consumed bool) bool {
	if binder == nil || expected == nil {
		return false
	}
	value, ok := runnerLedgerEntryAdmissionUseByEvidenceBinder.Load(binder)
	record, recordOK := value.(*runnerLedgerEntryAdmissionUseRecord)
	if !ok || !recordOK || record != expected {
		return false
	}
	record.mu.Lock()
	defer record.mu.Unlock()
	return record.factSubject == subject && record.consumed == consumed && record.boundary == boundary
}

func validRunnerLedgerEntryAdmissionEvidenceFacts(facts runnerLedgerEntryAdmissionEvidenceFacts, candidateBinding *verifiedEvidenceRunBinding) bool {
	return facts.binder != nil && runnerOwnedPointer(facts.binder) && candidateBinding != nil && facts.candidateBinding == candidateBinding &&
		facts.generation.owner != nil && facts.generation.owner == candidateBinding.owner &&
		facts.schemaDigest != ([32]byte{}) && facts.schemaDigest == generationJournalSchemaDigest(facts.schema, facts.generation) &&
		facts.recovery != nil && validRecoverySnapshotForJournal(facts.recovery, facts.generation, facts.recovery.cursor) &&
		facts.recoveryDigest != ([32]byte{}) && facts.recoveryDigest == generationJournalRecoveryDigest(facts.recovery) &&
		facts.sessionDigest != ([32]byte{}) && facts.journalDigest != ([32]byte{})
}

func runnerLedgerEntryAdmissionFactMatchesEvidence(fact runnerLedgerConsumerFact, candidate OwnedCurrentCandidate, facts runnerLedgerEntryAdmissionEvidenceFacts) bool {
	if !validOwnedCurrentCandidate(candidate) || !fact.valid() || !generatedRunnerLedgerEntryAdmissionProfile.valid() ||
		fact.action != runnerLedgerConsumerEntryNotImplemented || fact.manifestDigest != candidate.verifiedRun.manifestDigest ||
		fact.dispatch.kind != runnerLedgerPreflightDispatchEntry || fact.dispatch.fact.nextEntry == nil ||
		fact.dispatch.fact.schemaBundleDigest != facts.generation.schemaBundleDigest ||
		fact.dispatch.fact.executionLineageDigest != facts.generation.executionLineageDigest ||
		fact.dispatch.journalIdentityDigest != facts.generation.journalIdentityDigest ||
		fact.dispatch.runnerProjectionDecisionDigest != facts.generation.runnerProjectionDecisionDigest ||
		fact.dispatch.recoverySnapshotDigest != digestString(facts.recoveryDigest) ||
		fact.dispatch.recoveryTailDigest != facts.recovery.tailDigest || fact.dispatch.fact.recovery == nil ||
		fact.dispatch.fact.recovery.State != facts.recovery.state || fact.dispatch.fact.recovery.Action != facts.recovery.nextPermittedAction ||
		!sameOptionalString(fact.dispatch.recoveryMigrationID, facts.recovery.migrationID) ||
		!sameOptionalUint32(fact.dispatch.recoveryAttemptIndex, facts.recovery.attemptIndex) {
		return false
	}
	action, ok := generatedRunnerLedgerEntryAdmissionAction(
		fact.dispatch.fact.disposition, facts.recovery.state, facts.recovery.nextPermittedAction,
	)
	if !ok || action != runnerLedgerEntryAdmissionPrepare {
		return false
	}
	index := int(fact.dispatch.fact.orderedMigrationPrefixLength)
	want, err := runnerLedgerPreflightNextEntryFromSchema(facts.schema, index)
	return err == nil && *fact.dispatch.fact.nextEntry == want
}

func sameOptionalString(left, right *string) bool {
	return left == nil && right == nil || left != nil && right != nil && *left == *right
}

func sameOptionalUint32(left, right *uint32) bool {
	return left == nil && right == nil || left != nil && right != nil && *left == *right
}

func runnerLedgerEntryAdmissionClaimDigest(claim *runnerLedgerEntryAdmissionClaim) [32]byte {
	if claim == nil || claim.self != claim || claim.binding == nil || claim.candidateBinding == nil || claim.consumed == nil || claim.use == nil ||
		!claim.fact.valid() || claim.generation.owner == nil || claim.generation.owner != claim.candidateBinding.owner ||
		claim.schemaDigest == ([32]byte{}) || claim.recoveryDigest == ([32]byte{}) || claim.sessionDigest == ([32]byte{}) || claim.journalDigest == ([32]byte{}) {
		return [32]byte{}
	}
	h := sha256.New()
	h.Write([]byte(runnerLedgerEntryAdmissionClaimDigestDomain + "\x00"))
	h.Write(claim.candidateBinding.canonical[:])
	for _, value := range []Digest{
		claim.generation.executionLineageDigest, claim.generation.journalIdentityDigest,
		claim.generation.runnerProjectionDecisionDigest, claim.generation.schemaBundleDigest, claim.fact.subjectDigest,
	} {
		if value.Validate() != nil {
			return [32]byte{}
		}
		writeAdmissionString(h, value.String())
	}
	h.Write(claim.schemaDigest[:])
	h.Write(claim.recoveryDigest[:])
	h.Write(claim.sessionDigest[:])
	h.Write(claim.journalDigest[:])
	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result
}

func runnerLedgerEntryAdmissionEvidenceBoundaryDigest(boundary runnerLedgerEntryAdmissionEvidenceBoundary) [32]byte {
	if boundary.candidateBinding == nil || boundary.generation.owner == nil || boundary.generation.owner != boundary.candidateBinding.owner ||
		boundary.factSubject.Validate() != nil || boundary.recoveryTail.Validate() != nil || boundary.schemaDigest == ([32]byte{}) ||
		boundary.recoveryDigest == ([32]byte{}) || boundary.sessionDigest == ([32]byte{}) || boundary.journalDigest == ([32]byte{}) {
		return [32]byte{}
	}
	h := sha256.New()
	h.Write([]byte("cloud-agents/runner-ledger-entry-admission/evidence-boundary/v1\x00"))
	h.Write(boundary.candidateBinding.canonical[:])
	for _, value := range []Digest{
		boundary.generation.executionLineageDigest, boundary.generation.journalIdentityDigest,
		boundary.generation.runnerProjectionDecisionDigest, boundary.generation.schemaBundleDigest,
		boundary.factSubject, boundary.recoveryTail,
	} {
		if value.Validate() != nil {
			return [32]byte{}
		}
		writeAdmissionString(h, value.String())
	}
	h.Write(boundary.schemaDigest[:])
	h.Write(boundary.recoveryDigest[:])
	h.Write(boundary.sessionDigest[:])
	h.Write(boundary.journalDigest[:])
	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result
}

func validRunnerLedgerEntryAdmissionClaim(claim *runnerLedgerEntryAdmissionClaim, binder runnerLedgerEntryAdmissionClaimBinder, candidateBinding *verifiedEvidenceRunBinding) bool {
	if claim == nil || claim.self != claim || claim.binding == nil || claim.binding.claim != claim || claim.binding.binder == nil ||
		!sameRunnerOwnedPointer(claim.binding.binder, binder) || claim.binding.candidateBinding != candidateBinding ||
		claim.binding.consumed == nil || claim.binding.consumed != claim.consumed || claim.consumed.Load() ||
		claim.binding.use != claim.use ||
		claim.candidateBinding != candidateBinding || claim.canonical == ([32]byte{}) || claim.binding.canonical != claim.canonical ||
		claim.canonical != runnerLedgerEntryAdmissionClaimDigest(claim) {
		return false
	}
	registered, ok := runnerLedgerEntryAdmissionClaimRegistry.Load(claim)
	record, recordOK := registered.(runnerLedgerEntryAdmissionClaimRegistryRecord)
	return ok && recordOK && validRunnerLedgerEntryAdmissionUse(binder, claim.use, claim.fact.subjectDigest, [32]byte{}, false) &&
		record.claim == claim && record.binding == claim.binding && sameRunnerOwnedPointer(record.binder, binder) &&
		record.candidateBinding == candidateBinding && record.consumed == claim.consumed && record.use == claim.use && record.canonical == claim.canonical
}

func revokeRunnerLedgerEntryAdmissionClaim(claim *runnerLedgerEntryAdmissionClaim) {
	if claim == nil {
		return
	}
	registered, ok := runnerLedgerEntryAdmissionClaimRegistry.LoadAndDelete(claim)
	record, recordOK := registered.(runnerLedgerEntryAdmissionClaimRegistryRecord)
	if recordOK && record.consumed != nil {
		record.consumed.Store(true)
	} else if claim.consumed != nil {
		claim.consumed.Store(true)
	}
	_ = ok
}

func revokeRunnerLedgerEntryAdmissionClaims(binder runnerLedgerEntryAdmissionClaimBinder) {
	if binder == nil || !runnerOwnedPointer(binder) {
		return
	}
	runnerLedgerEntryAdmissionUseByEvidenceBinder.Delete(binder)
	runnerLedgerEntryAdmissionClaimRegistry.Range(func(key, value any) bool {
		record, ok := value.(runnerLedgerEntryAdmissionClaimRegistryRecord)
		if ok && sameRunnerOwnedPointer(record.binder, binder) {
			runnerLedgerEntryAdmissionClaimRegistry.Delete(key)
			if record.consumed != nil {
				record.consumed.Store(true)
			}
		}
		return true
	})
}
