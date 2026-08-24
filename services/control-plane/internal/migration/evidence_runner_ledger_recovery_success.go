package migration

import (
	"context"
	"crypto/sha256"
	"strconv"
	"sync"
	"sync/atomic"
)

const runnerLedgerRecoverySuccessEvidenceRequestDomain = "cloud-agents/runner-ledger-recovery-success-writer/evidence-request/v1"

// runnerLedgerRecoverySuccessEvidenceBinder is intentionally distinct from
// the immutable entry-v1 binder. Only a request minted after consuming the
// recovery execution-admission permit can enter this port.
type runnerLedgerRecoverySuccessEvidenceBinder interface {
	runnerLedgerSuccessEvidence
	runnerLedgerRecoveryAdmissionClaimBinder
	bindRunnerLedgerRecoverySuccessRecord(context.Context, *runnerLedgerRecoverySuccessEvidenceRequest) (EvidenceJournal, JournalCursor, *OwnedEvidenceRecord, error)
	runnerLedgerRecoverySuccessEvidenceBinderSealed()
}

type runnerLedgerRecoverySuccessEvidenceRequest struct {
	self             *runnerLedgerRecoverySuccessEvidenceRequest
	binder           runnerLedgerRecoverySuccessEvidenceBinder
	candidateBinding *verifiedEvidenceRunBinding
	generation       generationIdentity
	recoveryDigest   [32]byte
	cursor           JournalCursor
	record           EvidenceRecord
	plan             StatementPlan
	maxAttempts      uint32
	canonical        [32]byte
	consumed         bool
}

type runnerLedgerRecoverySuccessEvidenceRequestRecord struct {
	request          *runnerLedgerRecoverySuccessEvidenceRequest
	binder           runnerLedgerRecoverySuccessEvidenceBinder
	candidateBinding *verifiedEvidenceRunBinding
	cursorValid      *atomic.Bool
	canonical        [32]byte
}

var runnerLedgerRecoverySuccessEvidenceRequestRegistry sync.Map

func mintRunnerLedgerRecoverySuccessEvidenceRequest(
	binder runnerLedgerRecoverySuccessEvidenceBinder,
	candidateBinding *verifiedEvidenceRunBinding,
	generation generationIdentity,
	recoveryDigest [32]byte,
	cursor JournalCursor,
	record EvidenceRecord,
	plan StatementPlan,
	maxAttempts uint32,
) (*runnerLedgerRecoverySuccessEvidenceRequest, error) {
	if binder == nil || !runnerOwnedPointer(binder) || candidateBinding == nil ||
		candidateBinding.owner == nil || generation.owner != candidateBinding.owner ||
		recoveryDigest == ([32]byte{}) || !cursor.Valid() ||
		!sameGenerationIdentity(cursor.generation, generation) || maxAttempts == 0 ||
		plan.validateExact() != nil || validateEvidenceRecord(record) != nil ||
		!validGeneratedRunnerLedgerRecoveryProfiles() {
		return nil, fail(CodeEvidenceRecoveryRequired, "runner-ledger-recovery-success-evidence-request", "recovery success evidence request inputs are unavailable", nil)
	}
	ownedPlan, err := cloneRunnerStatementIntentPlan(plan)
	if err != nil {
		return nil, err
	}
	request := &runnerLedgerRecoverySuccessEvidenceRequest{
		binder: binder, candidateBinding: candidateBinding, generation: generation,
		recoveryDigest: recoveryDigest, cursor: cursor.clone(), record: cloneEvidenceRecord(record),
		plan: ownedPlan, maxAttempts: maxAttempts,
	}
	request.self = request
	request.canonical = runnerLedgerRecoverySuccessEvidenceRequestDigest(request)
	if request.canonical == ([32]byte{}) {
		return nil, fail(CodeEvidenceRecoveryRequired, "runner-ledger-recovery-success-evidence-request", "recovery success evidence request could not be identified", nil)
	}
	runnerLedgerRecoverySuccessEvidenceRequestRegistry.Store(request, runnerLedgerRecoverySuccessEvidenceRequestRecord{
		request: request, binder: binder, candidateBinding: candidateBinding,
		cursorValid: request.cursor.valid, canonical: request.canonical,
	})
	if !validRunnerLedgerRecoverySuccessEvidenceRequest(request, binder) {
		runnerLedgerRecoverySuccessEvidenceRequestRegistry.Delete(request)
		return nil, fail(CodeEvidenceRecoveryRequired, "runner-ledger-recovery-success-evidence-request", "recovery success evidence request could not be sealed", nil)
	}
	return request, nil
}

func validRunnerLedgerRecoverySuccessEvidenceRequest(request *runnerLedgerRecoverySuccessEvidenceRequest, binder runnerLedgerRecoverySuccessEvidenceBinder) bool {
	if !validRunnerLedgerRecoverySuccessEvidenceRequestWithoutRegistry(request, binder) {
		return false
	}
	registered, ok := runnerLedgerRecoverySuccessEvidenceRequestRegistry.Load(request)
	record, recordOK := registered.(runnerLedgerRecoverySuccessEvidenceRequestRecord)
	return ok && recordOK && record.request == request && record.candidateBinding == request.candidateBinding &&
		record.cursorValid == request.cursor.valid && record.canonical == request.canonical &&
		sameRunnerOwnedPointer(record.binder, request.binder)
}

func consumeRunnerLedgerRecoverySuccessEvidenceRequest(request *runnerLedgerRecoverySuccessEvidenceRequest, binder runnerLedgerRecoverySuccessEvidenceBinder) (runnerLedgerSuccessEvidenceClaim, error) {
	if request == nil || request.self != request {
		return runnerLedgerSuccessEvidenceClaim{}, fail(CodeEvidenceRecoveryRequired, "runner-ledger-recovery-success-evidence-claim", "recovery success evidence request is unavailable", nil)
	}
	registered, loaded := runnerLedgerRecoverySuccessEvidenceRequestRegistry.LoadAndDelete(request)
	record, recordOK := registered.(runnerLedgerRecoverySuccessEvidenceRequestRecord)
	valid := loaded && recordOK && record.request == request && record.candidateBinding == request.candidateBinding &&
		record.cursorValid == request.cursor.valid && record.canonical == request.canonical &&
		sameRunnerOwnedPointer(record.binder, binder) && validRunnerLedgerRecoverySuccessEvidenceRequestWithoutRegistry(request, binder)
	if !valid {
		request.consumed = true
		request.binder = nil
		return runnerLedgerSuccessEvidenceClaim{}, fail(CodeEvidenceRecoveryRequired, "runner-ledger-recovery-success-evidence-claim", "recovery success evidence request changed or was already consumed", nil)
	}
	ownedPlan, err := cloneRunnerStatementIntentPlan(request.plan)
	if err != nil {
		request.consumed = true
		request.binder = nil
		return runnerLedgerSuccessEvidenceClaim{}, err
	}
	claimed := runnerLedgerSuccessEvidenceClaim{
		candidateBinding: request.candidateBinding, generation: request.generation,
		recoveryDigest: request.recoveryDigest, cursor: request.cursor.clone(),
		record: cloneEvidenceRecord(request.record), plan: ownedPlan, maxAttempts: request.maxAttempts,
	}
	request.consumed = true
	request.binder = nil
	return claimed, nil
}

func validRunnerLedgerRecoverySuccessEvidenceRequestWithoutRegistry(request *runnerLedgerRecoverySuccessEvidenceRequest, binder runnerLedgerRecoverySuccessEvidenceBinder) bool {
	return request != nil && request.self == request && !request.consumed && request.binder != nil &&
		binder != nil && sameRunnerOwnedPointer(request.binder, binder) && request.candidateBinding != nil &&
		request.generation.owner == request.candidateBinding.owner && request.recoveryDigest != ([32]byte{}) &&
		request.cursor.Valid() && sameGenerationIdentity(request.cursor.generation, request.generation) &&
		request.plan.validateExact() == nil && request.maxAttempts > 0 && validateEvidenceRecord(request.record) == nil &&
		request.canonical != ([32]byte{}) && request.canonical == runnerLedgerRecoverySuccessEvidenceRequestDigest(request)
}

func runnerLedgerRecoverySuccessEvidenceRequestDigest(request *runnerLedgerRecoverySuccessEvidenceRequest) [32]byte {
	if request == nil || request.self != request || request.consumed || request.binder == nil ||
		request.candidateBinding == nil || request.generation.owner != request.candidateBinding.owner ||
		request.recoveryDigest == ([32]byte{}) || !request.cursor.Valid() ||
		!sameGenerationIdentity(request.cursor.generation, request.generation) || request.maxAttempts == 0 ||
		request.plan.validateExact() != nil || validateEvidenceRecord(request.record) != nil ||
		!validGeneratedRunnerLedgerRecoveryProfiles() {
		return [32]byte{}
	}
	recordCanonical, err := canonicalContractKey(request.record)
	if err != nil || recordCanonical == "" {
		return [32]byte{}
	}
	h := sha256.New()
	h.Write([]byte(runnerLedgerRecoverySuccessEvidenceRequestDomain + "\x00"))
	h.Write(request.candidateBinding.canonical[:])
	h.Write(request.recoveryDigest[:])
	for _, value := range runnerLedgerRecoverySuccessWriterIdentityStrings() {
		writeAdmissionString(h, value)
	}
	for _, value := range []string{
		request.generation.executionLineageDigest.String(), request.generation.journalIdentityDigest.String(),
		request.generation.runnerProjectionDecisionDigest.String(), request.generation.schemaBundleDigest.String(),
		request.plan.exactCanonical, recordCanonical,
	} {
		writeAdmissionString(h, value)
	}
	writeAdmissionUint(h, uint64(request.maxAttempts))
	writeAdmissionUint(h, uint64(request.cursor.segmentIndex))
	writeAdmissionUint(h, request.cursor.nextSequence)
	writeAdmissionUint(h, request.cursor.lineageIndexNextSequence)
	writeAdmissionString(h, request.cursor.lineageIndexPreviousRecordDigest.String())
	writeAdmissionString(h, strconv.FormatBool(request.cursor.previousRecordDigest != nil))
	if request.cursor.previousRecordDigest != nil {
		writeAdmissionString(h, request.cursor.previousRecordDigest.String())
	}
	writeAdmissionString(h, strconv.FormatBool(request.cursor.latestCheckpointRecordDigest != nil))
	if request.cursor.latestCheckpointRecordDigest != nil {
		writeAdmissionString(h, request.cursor.latestCheckpointRecordDigest.String())
	}
	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result
}

func runnerLedgerRecoverySuccessWriterIdentityStrings() []string {
	common := generatedRunnerLedgerRecoveryProfiles[0]
	admission := generatedRunnerLedgerRecoveryProfiles[5]
	writer := generatedRunnerLedgerRecoveryProfiles[6]
	values := make([]string, 0, 30)
	for _, profile := range []runnerLedgerRecoveryProfile{common, admission, writer} {
		values = append(values, profile.registryID, profile.registryDigest, profile.profileID, profile.profileDigest, profile.stateMachineDigest, profile.policyDigest)
	}
	values = append(values,
		writer.predecessor.registryID, writer.predecessor.registryDigest, writer.predecessor.profileID,
		writer.predecessor.profileDigest, writer.predecessor.stateMachineDigest, writer.predecessor.policyDigest,
	)
	return values
}
