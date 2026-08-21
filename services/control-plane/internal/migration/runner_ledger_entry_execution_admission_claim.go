package migration

import (
	"context"
	"crypto/sha256"
	"sync"
	"sync/atomic"
)

const runnerLedgerEntryExecutionAdmissionClaimDigestDomain = "cloud-agents/runner-ledger-entry-execution-admission/claim/v1"

// runnerLedgerEntryExecutionAdmissionClaimBinder is implemented only by the
// retained same-verifier generation evidence session. It brackets the fresh
// execution-admission database observation with two exact evidence reads. It
// is deliberately distinct from the immutable ADR-0021 close-only authority.
type runnerLedgerEntryExecutionAdmissionClaimBinder interface {
	bindRunnerLedgerEntryExecutionAdmissionClaim(context.Context, runnerLedgerEntryExecutionAdmissionClaimRequest) (*runnerLedgerEntryExecutionAdmissionClaim, error)
	consumeRunnerLedgerEntryExecutionAdmissionClaim(context.Context, *runnerLedgerEntryExecutionAdmissionClaim, OwnedCurrentCandidate) (runnerLedgerEntryExecutionAdmissionEvidenceBoundary, error)
	runnerLedgerEntryExecutionAdmissionClaimBinderSealed()
}

type runnerLedgerEntryExecutionAdmissionClaimRequest struct {
	fact      runnerLedgerConsumerFact
	candidate OwnedCurrentCandidate
}

type runnerLedgerEntryExecutionAdmissionEvidenceFacts struct {
	binder           runnerLedgerEntryExecutionAdmissionClaimBinder
	candidateBinding *verifiedEvidenceRunBinding
	generation       generationIdentity
	schema           verifiedRecoverySchemaWitness
	recovery         *RecoverySnapshot
	schemaDigest     [32]byte
	recoveryDigest   [32]byte
	sessionDigest    [32]byte
	journalDigest    [32]byte
}

// runnerLedgerEntryExecutionAdmissionEvidenceBoundary is ordinary data used
// only while sealing the execution permit. It carries no database or evidence
// mutation port and cannot be used as the ADR-0021 close-only permit.
type runnerLedgerEntryExecutionAdmissionEvidenceBoundary struct {
	candidateBinding *verifiedEvidenceRunBinding
	generation       generationIdentity
	factSubject      Digest
	schemaDigest     [32]byte
	recoveryDigest   [32]byte
	sessionDigest    [32]byte
	journalDigest    [32]byte
	recoveryTail     Digest
	claimDigest      [32]byte
	canonical        [32]byte
}

type runnerLedgerEntryExecutionAdmissionClaim struct {
	self             *runnerLedgerEntryExecutionAdmissionClaim
	binding          *runnerLedgerEntryExecutionAdmissionClaimBinding
	candidateBinding *verifiedEvidenceRunBinding
	generation       generationIdentity
	fact             runnerLedgerConsumerFact
	schemaDigest     [32]byte
	recoveryDigest   [32]byte
	sessionDigest    [32]byte
	journalDigest    [32]byte
	use              *runnerLedgerEntryExecutionAdmissionUseRecord
	consumed         *atomic.Bool
	canonical        [32]byte
}

type runnerLedgerEntryExecutionAdmissionClaimBinding struct {
	claim            *runnerLedgerEntryExecutionAdmissionClaim
	binder           runnerLedgerEntryExecutionAdmissionClaimBinder
	candidateBinding *verifiedEvidenceRunBinding
	consumed         *atomic.Bool
	use              *runnerLedgerEntryExecutionAdmissionUseRecord
	canonical        [32]byte
}

type runnerLedgerEntryExecutionAdmissionClaimRegistryRecord struct {
	claim            *runnerLedgerEntryExecutionAdmissionClaim
	binding          *runnerLedgerEntryExecutionAdmissionClaimBinding
	binder           runnerLedgerEntryExecutionAdmissionClaimBinder
	candidateBinding *verifiedEvidenceRunBinding
	consumed         *atomic.Bool
	use              *runnerLedgerEntryExecutionAdmissionUseRecord
	canonical        [32]byte
}

type runnerLedgerEntryExecutionAdmissionUseRecord struct {
	mu          sync.Mutex
	factSubject Digest
	consumed    bool
	boundary    [32]byte
}

var (
	runnerLedgerEntryExecutionAdmissionClaimRegistry sync.Map
	// The terminal use record survives claim failure, permit close, and a
	// rejected retry selection. Only evidence-session close removes it, so an
	// earlier ordinary consumer fact can never drive a second admission attempt.
	runnerLedgerEntryExecutionAdmissionUseByEvidenceBinder sync.Map
)

func bindRunnerLedgerEntryExecutionAdmissionClaimFromEvidence(ctx context.Context, request runnerLedgerEntryExecutionAdmissionClaimRequest, facts runnerLedgerEntryExecutionAdmissionEvidenceFacts) (*runnerLedgerEntryExecutionAdmissionClaim, error) {
	if err := contextAdmissionError(ctx); err != nil {
		return nil, err
	}
	if !validOwnedCurrentCandidate(request.candidate) ||
		!validRunnerLedgerEntryExecutionAdmissionEvidenceFacts(facts, request.candidate.binding) ||
		!runnerLedgerEntryExecutionAdmissionFactMatchesEvidence(request.fact, request.candidate, facts) {
		return nil, fail(CodeEvidenceRecoveryRequired, "runner-ledger-entry-execution-admission-claim", "same-verifier entry evidence is unavailable or changed", nil)
	}
	use := &runnerLedgerEntryExecutionAdmissionUseRecord{factSubject: request.fact.subjectDigest}
	if _, loaded := runnerLedgerEntryExecutionAdmissionUseByEvidenceBinder.LoadOrStore(facts.binder, use); loaded {
		return nil, fail(CodeEvidenceRecoveryRequired, "runner-ledger-entry-execution-admission-claim", "entry consumer fact already entered execution admission", nil)
	}
	claim := &runnerLedgerEntryExecutionAdmissionClaim{
		candidateBinding: request.candidate.binding,
		generation:       facts.generation,
		fact:             request.fact.clone(),
		schemaDigest:     facts.schemaDigest,
		recoveryDigest:   facts.recoveryDigest,
		sessionDigest:    facts.sessionDigest,
		journalDigest:    facts.journalDigest,
		use:              use,
		consumed:         &atomic.Bool{},
	}
	claim.self = claim
	claim.binding = &runnerLedgerEntryExecutionAdmissionClaimBinding{
		claim: claim, binder: facts.binder, candidateBinding: request.candidate.binding,
		consumed: claim.consumed, use: use,
	}
	claim.canonical = runnerLedgerEntryExecutionAdmissionClaimDigest(claim)
	claim.binding.canonical = claim.canonical
	if claim.canonical == ([32]byte{}) {
		revokeRunnerLedgerEntryExecutionAdmissionClaim(claim)
		return nil, fail(CodeEvidenceRecoveryRequired, "runner-ledger-entry-execution-admission-claim", "execution admission claim could not be identified", nil)
	}
	runnerLedgerEntryExecutionAdmissionClaimRegistry.Store(claim, runnerLedgerEntryExecutionAdmissionClaimRegistryRecord{
		claim: claim, binding: claim.binding, binder: facts.binder, candidateBinding: request.candidate.binding,
		consumed: claim.consumed, use: use, canonical: claim.canonical,
	})
	if !validRunnerLedgerEntryExecutionAdmissionClaim(claim, facts.binder, request.candidate.binding) {
		revokeRunnerLedgerEntryExecutionAdmissionClaim(claim)
		return nil, fail(CodeEvidenceRecoveryRequired, "runner-ledger-entry-execution-admission-claim", "execution admission claim could not be sealed", nil)
	}
	if err := contextAdmissionError(ctx); err != nil {
		revokeRunnerLedgerEntryExecutionAdmissionClaim(claim)
		return nil, err
	}
	return claim, nil
}

func consumeRunnerLedgerEntryExecutionAdmissionClaimFromEvidence(ctx context.Context, claim *runnerLedgerEntryExecutionAdmissionClaim, candidate OwnedCurrentCandidate, facts runnerLedgerEntryExecutionAdmissionEvidenceFacts) (runnerLedgerEntryExecutionAdmissionEvidenceBoundary, error) {
	if err := contextAdmissionError(ctx); err != nil {
		return runnerLedgerEntryExecutionAdmissionEvidenceBoundary{}, err
	}
	if !validOwnedCurrentCandidate(candidate) ||
		!validRunnerLedgerEntryExecutionAdmissionEvidenceFacts(facts, candidate.binding) ||
		!validRunnerLedgerEntryExecutionAdmissionClaim(claim, facts.binder, candidate.binding) {
		if claim != nil && claim.self == claim {
			revokeRunnerLedgerEntryExecutionAdmissionClaim(claim)
		}
		return runnerLedgerEntryExecutionAdmissionEvidenceBoundary{}, fail(CodeEvidenceRecoveryRequired, "runner-ledger-entry-execution-admission-claim", "execution admission claim is unavailable or changed", nil)
	}
	if !runnerLedgerEntryExecutionAdmissionFactMatchesEvidence(claim.fact, candidate, facts) ||
		!sameGenerationIdentity(claim.generation, facts.generation) || claim.schemaDigest != facts.schemaDigest ||
		claim.recoveryDigest != facts.recoveryDigest || claim.sessionDigest != facts.sessionDigest || claim.journalDigest != facts.journalDigest {
		revokeRunnerLedgerEntryExecutionAdmissionClaim(claim)
		return runnerLedgerEntryExecutionAdmissionEvidenceBoundary{}, fail(CodeEvidenceRecoveryRequired, "runner-ledger-entry-execution-admission-claim", "evidence changed during fresh execution-admission revalidation", nil)
	}
	if !claim.consumed.CompareAndSwap(false, true) {
		revokeRunnerLedgerEntryExecutionAdmissionClaim(claim)
		return runnerLedgerEntryExecutionAdmissionEvidenceBoundary{}, fail(CodeEvidenceRecoveryRequired, "runner-ledger-entry-execution-admission-claim", "execution admission claim was already consumed", nil)
	}
	runnerLedgerEntryExecutionAdmissionClaimRegistry.Delete(claim)
	boundary := runnerLedgerEntryExecutionAdmissionEvidenceBoundary{
		candidateBinding: candidate.binding,
		generation:       facts.generation,
		factSubject:      claim.fact.subjectDigest,
		schemaDigest:     facts.schemaDigest,
		recoveryDigest:   facts.recoveryDigest,
		sessionDigest:    facts.sessionDigest,
		journalDigest:    facts.journalDigest,
		recoveryTail:     facts.recovery.tailDigest,
		claimDigest:      claim.canonical,
	}
	boundary.canonical = runnerLedgerEntryExecutionAdmissionEvidenceBoundaryDigest(boundary)
	if boundary.canonical == ([32]byte{}) {
		return runnerLedgerEntryExecutionAdmissionEvidenceBoundary{}, fail(CodeEvidenceRecoveryRequired, "runner-ledger-entry-execution-admission-claim", "consumed evidence boundary could not be identified", nil)
	}
	if !claim.use.consumeBoundary(claim.fact.subjectDigest, boundary.canonical) {
		return runnerLedgerEntryExecutionAdmissionEvidenceBoundary{}, fail(CodeEvidenceRecoveryRequired, "runner-ledger-entry-execution-admission-claim", "consumed evidence boundary could not be registered", nil)
	}
	return boundary, nil
}

func (record *runnerLedgerEntryExecutionAdmissionUseRecord) claimMatches(subject Digest) bool {
	if record == nil {
		return false
	}
	record.mu.Lock()
	defer record.mu.Unlock()
	return !record.consumed && record.boundary == ([32]byte{}) && record.factSubject == subject
}

func (record *runnerLedgerEntryExecutionAdmissionUseRecord) consumeBoundary(subject Digest, boundary [32]byte) bool {
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

func validRunnerLedgerEntryExecutionAdmissionUse(binder runnerLedgerEntryExecutionAdmissionClaimBinder, expected *runnerLedgerEntryExecutionAdmissionUseRecord, subject Digest, boundary [32]byte, consumed bool) bool {
	if binder == nil || expected == nil {
		return false
	}
	value, ok := runnerLedgerEntryExecutionAdmissionUseByEvidenceBinder.Load(binder)
	record, recordOK := value.(*runnerLedgerEntryExecutionAdmissionUseRecord)
	if !ok || !recordOK || record != expected {
		return false
	}
	record.mu.Lock()
	defer record.mu.Unlock()
	return record.factSubject == subject && record.consumed == consumed && record.boundary == boundary
}

func validRunnerLedgerEntryExecutionAdmissionEvidenceFacts(facts runnerLedgerEntryExecutionAdmissionEvidenceFacts, candidateBinding *verifiedEvidenceRunBinding) bool {
	return facts.binder != nil && runnerOwnedPointer(facts.binder) && candidateBinding != nil && facts.candidateBinding == candidateBinding &&
		facts.generation.owner != nil && facts.generation.owner == candidateBinding.owner &&
		facts.schemaDigest != ([32]byte{}) && facts.schemaDigest == generationJournalSchemaDigest(facts.schema, facts.generation) &&
		facts.recovery != nil && validRecoverySnapshotForJournal(facts.recovery, facts.generation, facts.recovery.cursor) &&
		facts.recoveryDigest != ([32]byte{}) && facts.recoveryDigest == generationJournalRecoveryDigest(facts.recovery) &&
		facts.sessionDigest != ([32]byte{}) && facts.journalDigest != ([32]byte{})
}

// The claim proves a fresh same-verifier entry fact, not that the selected
// transition belongs to the execution profile. The fifth immutable ADR-0021
// retry pair is deliberately classified only after the locked database and
// final evidence rereads, so stored/operational failures retain precedence over
// NOT_IMPLEMENTED. Only the later permit binder applies the four-pair selector.
func runnerLedgerEntryExecutionAdmissionFactMatchesEvidence(fact runnerLedgerConsumerFact, candidate OwnedCurrentCandidate, facts runnerLedgerEntryExecutionAdmissionEvidenceFacts) bool {
	if !validOwnedCurrentCandidate(candidate) || !fact.valid() ||
		!validRunnerLedgerEntryExecutionAdmissionProfiles() ||
		fact.action != runnerLedgerConsumerEntryNotImplemented || fact.manifestDigest != candidate.verifiedRun.manifestDigest ||
		fact.dispatch.kind != runnerLedgerPreflightDispatchEntry || fact.dispatch.fact.nextEntry == nil ||
		fact.dispatch.fact.schemaBundleDigest != facts.generation.schemaBundleDigest ||
		fact.dispatch.fact.executionLineageDigest != facts.generation.executionLineageDigest ||
		fact.dispatch.journalIdentityDigest != facts.generation.journalIdentityDigest ||
		fact.dispatch.runnerProjectionDecisionDigest != facts.generation.runnerProjectionDecisionDigest ||
		fact.dispatch.recoverySnapshotDigest != digestString(facts.recoveryDigest) ||
		fact.dispatch.recoveryTailDigest != facts.recovery.tailDigest || fact.dispatch.fact.recovery == nil ||
		fact.dispatch.fact.recovery.State != facts.recovery.state ||
		fact.dispatch.fact.recovery.Action != facts.recovery.nextPermittedAction ||
		!sameOptionalString(fact.dispatch.recoveryMigrationID, facts.recovery.migrationID) ||
		!sameOptionalUint32(fact.dispatch.recoveryAttemptIndex, facts.recovery.attemptIndex) {
		return false
	}
	entryAction, ok := generatedRunnerLedgerEntryAdmissionAction(
		fact.dispatch.fact.disposition, facts.recovery.state, facts.recovery.nextPermittedAction,
	)
	if !ok || entryAction != runnerLedgerEntryAdmissionPrepare {
		return false
	}
	index := int(fact.dispatch.fact.orderedMigrationPrefixLength)
	want, err := runnerLedgerPreflightNextEntryFromSchema(facts.schema, index)
	return err == nil && *fact.dispatch.fact.nextEntry == want
}

func runnerLedgerEntryExecutionAdmissionClaimDigest(claim *runnerLedgerEntryExecutionAdmissionClaim) [32]byte {
	if claim == nil || claim.self != claim || claim.binding == nil || claim.candidateBinding == nil || claim.consumed == nil || claim.use == nil ||
		!claim.fact.valid() || claim.generation.owner == nil || claim.generation.owner != claim.candidateBinding.owner ||
		claim.schemaDigest == ([32]byte{}) || claim.recoveryDigest == ([32]byte{}) ||
		claim.sessionDigest == ([32]byte{}) || claim.journalDigest == ([32]byte{}) ||
		!validRunnerLedgerEntryExecutionAdmissionProfiles() {
		return [32]byte{}
	}
	h := sha256.New()
	h.Write([]byte(runnerLedgerEntryExecutionAdmissionClaimDigestDomain + "\x00"))
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
	for _, value := range runnerLedgerEntryExecutionAdmissionProfileIdentityStrings() {
		writeAdmissionString(h, value)
	}
	h.Write(claim.schemaDigest[:])
	h.Write(claim.recoveryDigest[:])
	h.Write(claim.sessionDigest[:])
	h.Write(claim.journalDigest[:])
	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result
}

func runnerLedgerEntryExecutionAdmissionEvidenceBoundaryDigest(boundary runnerLedgerEntryExecutionAdmissionEvidenceBoundary) [32]byte {
	if boundary.candidateBinding == nil || boundary.generation.owner == nil || boundary.generation.owner != boundary.candidateBinding.owner ||
		boundary.factSubject.Validate() != nil || boundary.recoveryTail.Validate() != nil || boundary.schemaDigest == ([32]byte{}) ||
		boundary.recoveryDigest == ([32]byte{}) || boundary.sessionDigest == ([32]byte{}) ||
		boundary.journalDigest == ([32]byte{}) || boundary.claimDigest == ([32]byte{}) ||
		!validRunnerLedgerEntryExecutionAdmissionProfiles() {
		return [32]byte{}
	}
	h := sha256.New()
	h.Write([]byte("cloud-agents/runner-ledger-entry-execution-admission/evidence-boundary/v1\x00"))
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
	for _, value := range runnerLedgerEntryExecutionAdmissionProfileIdentityStrings() {
		writeAdmissionString(h, value)
	}
	h.Write(boundary.schemaDigest[:])
	h.Write(boundary.recoveryDigest[:])
	h.Write(boundary.sessionDigest[:])
	h.Write(boundary.journalDigest[:])
	h.Write(boundary.claimDigest[:])
	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result
}

func runnerLedgerEntryExecutionAdmissionProfileIdentityStrings() []string {
	return []string{
		generatedRunnerLedgerPreflightProfile.profileID,
		generatedRunnerLedgerPreflightProfile.profileDigest,
		runnerLedgerPreflightRegistryDigest,
		runnerLedgerPreflightStateMachineDigest,
		runnerLedgerPreflightPolicyDigest,
		generatedRunnerLedgerConsumerProfile.profileID,
		generatedRunnerLedgerConsumerProfile.profileDigest,
		runnerLedgerConsumerRegistryDigest,
		runnerLedgerConsumerStateMachineDigest,
		runnerLedgerConsumerPolicyDigest,
		generatedRunnerLedgerEntryAdmissionProfile.profileID,
		generatedRunnerLedgerEntryAdmissionProfile.profileDigest,
		runnerLedgerEntryAdmissionRegistryDigest,
		runnerLedgerEntryAdmissionStateMachineDigest,
		runnerLedgerEntryAdmissionPolicyDigest,
		generatedRunnerLedgerEntryExecutionAdmissionProfile.profileID,
		generatedRunnerLedgerEntryExecutionAdmissionProfile.profileDigest,
		runnerLedgerEntryExecutionAdmissionRegistryDigest,
		runnerLedgerEntryExecutionAdmissionStateMachineDigest,
		runnerLedgerEntryExecutionAdmissionPolicyDigest,
		generatedRunnerLedgerEntrySuccessWriterProfile.profileID,
		generatedRunnerLedgerEntrySuccessWriterProfile.profileDigest,
		runnerLedgerEntrySuccessWriterRegistryDigest,
		runnerLedgerEntrySuccessWriterStateMachineDigest,
		runnerLedgerEntrySuccessWriterPolicyDigest,
	}
}

func validRunnerLedgerEntryExecutionAdmissionProfiles() bool {
	return generatedRunnerLedgerPreflightProfile.valid() &&
		generatedRunnerLedgerConsumerProfile.valid() &&
		generatedRunnerLedgerEntryAdmissionProfile.valid() &&
		generatedRunnerLedgerEntryExecutionAdmissionProfile.valid() &&
		generatedRunnerLedgerEntrySuccessWriterProfile.valid()
}

func validRunnerLedgerEntryExecutionAdmissionClaim(claim *runnerLedgerEntryExecutionAdmissionClaim, binder runnerLedgerEntryExecutionAdmissionClaimBinder, candidateBinding *verifiedEvidenceRunBinding) bool {
	if claim == nil || claim.self != claim || claim.binding == nil || claim.binding.claim != claim || claim.binding.binder == nil ||
		!sameRunnerOwnedPointer(claim.binding.binder, binder) || claim.binding.candidateBinding != candidateBinding ||
		claim.binding.consumed == nil || claim.binding.consumed != claim.consumed || claim.consumed.Load() ||
		claim.binding.use != claim.use || claim.candidateBinding != candidateBinding || claim.canonical == ([32]byte{}) ||
		claim.binding.canonical != claim.canonical || claim.canonical != runnerLedgerEntryExecutionAdmissionClaimDigest(claim) {
		return false
	}
	registered, ok := runnerLedgerEntryExecutionAdmissionClaimRegistry.Load(claim)
	record, recordOK := registered.(runnerLedgerEntryExecutionAdmissionClaimRegistryRecord)
	return ok && recordOK &&
		validRunnerLedgerEntryExecutionAdmissionUse(binder, claim.use, claim.fact.subjectDigest, [32]byte{}, false) &&
		record.claim == claim && record.binding == claim.binding && sameRunnerOwnedPointer(record.binder, binder) &&
		record.candidateBinding == candidateBinding && record.consumed == claim.consumed &&
		record.use == claim.use && record.canonical == claim.canonical
}

func revokeRunnerLedgerEntryExecutionAdmissionClaim(claim *runnerLedgerEntryExecutionAdmissionClaim) {
	if claim == nil {
		return
	}
	registered, ok := runnerLedgerEntryExecutionAdmissionClaimRegistry.LoadAndDelete(claim)
	record, recordOK := registered.(runnerLedgerEntryExecutionAdmissionClaimRegistryRecord)
	if recordOK && record.consumed != nil {
		record.consumed.Store(true)
	} else if claim.consumed != nil {
		claim.consumed.Store(true)
	}
	_ = ok
}

func revokeRunnerLedgerEntryExecutionAdmissionClaims(binder runnerLedgerEntryExecutionAdmissionClaimBinder) {
	if binder == nil || !runnerOwnedPointer(binder) {
		return
	}
	runnerLedgerEntryExecutionAdmissionUseByEvidenceBinder.Delete(binder)
	runnerLedgerEntryExecutionAdmissionClaimRegistry.Range(func(key, value any) bool {
		record, ok := value.(runnerLedgerEntryExecutionAdmissionClaimRegistryRecord)
		if ok && sameRunnerOwnedPointer(record.binder, binder) {
			runnerLedgerEntryExecutionAdmissionClaimRegistry.Delete(key)
			if record.consumed != nil {
				record.consumed.Store(true)
			}
		}
		return true
	})
}
