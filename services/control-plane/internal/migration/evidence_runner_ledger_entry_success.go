package migration

import (
	"context"
	"crypto/sha256"
	"strconv"
	"sync"
	"sync/atomic"
)

const runnerLedgerEntrySuccessEvidenceRequestDomain = "cloud-agents/runner-ledger-entry-success-writer/evidence-request/v1"

// runnerLedgerEntrySuccessEvidenceBinder is the sole evidence-side mutation
// bridge for ADR-0022 Slice C. It is deliberately separate from the four
// historical single-statement binders and accepts only a registry-backed
// request minted by the closed success kernel.
type runnerLedgerEntrySuccessEvidenceBinder interface {
	EvidenceSession
	runnerLedgerEntryExecutionAdmissionClaimBinder
	bindRunnerLedgerEntrySuccessRecord(context.Context, *runnerLedgerEntrySuccessEvidenceRequest) (EvidenceJournal, JournalCursor, *OwnedEvidenceRecord, error)
	runnerLedgerEntrySuccessEvidenceBinderSealed()
}

type runnerLedgerEntrySuccessEvidenceRequest struct {
	self             *runnerLedgerEntrySuccessEvidenceRequest
	binder           runnerLedgerEntrySuccessEvidenceBinder
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

type runnerLedgerEntrySuccessEvidenceRequestRecord struct {
	request          *runnerLedgerEntrySuccessEvidenceRequest
	binder           runnerLedgerEntrySuccessEvidenceBinder
	candidateBinding *verifiedEvidenceRunBinding
	cursorValid      *atomic.Bool
	canonical        [32]byte
}

var runnerLedgerEntrySuccessEvidenceRequestRegistry sync.Map

func mintRunnerLedgerEntrySuccessEvidenceRequest(
	binder runnerLedgerEntrySuccessEvidenceBinder,
	candidateBinding *verifiedEvidenceRunBinding,
	generation generationIdentity,
	recoveryDigest [32]byte,
	cursor JournalCursor,
	record EvidenceRecord,
	plan StatementPlan,
	maxAttempts uint32,
) (*runnerLedgerEntrySuccessEvidenceRequest, error) {
	if binder == nil || !runnerOwnedPointer(binder) || candidateBinding == nil ||
		candidateBinding.owner == nil || generation.owner != candidateBinding.owner ||
		recoveryDigest == ([32]byte{}) || !cursor.Valid() ||
		!sameGenerationIdentity(cursor.generation, generation) || maxAttempts == 0 ||
		plan.validateExact() != nil || validateEvidenceRecord(record) != nil {
		return nil, fail(CodeEvidenceRecoveryRequired, "runner-ledger-entry-success-evidence-request", "success evidence request inputs are unavailable", nil)
	}
	ownedPlan, err := cloneRunnerStatementIntentPlan(plan)
	if err != nil {
		return nil, err
	}
	request := &runnerLedgerEntrySuccessEvidenceRequest{
		binder: binder, candidateBinding: candidateBinding, generation: generation,
		recoveryDigest: recoveryDigest, cursor: cursor.clone(), record: cloneEvidenceRecord(record),
		plan: ownedPlan, maxAttempts: maxAttempts,
	}
	request.self = request
	request.canonical = runnerLedgerEntrySuccessEvidenceRequestDigest(request)
	if request.canonical == ([32]byte{}) {
		return nil, fail(CodeEvidenceRecoveryRequired, "runner-ledger-entry-success-evidence-request", "success evidence request could not be identified", nil)
	}
	runnerLedgerEntrySuccessEvidenceRequestRegistry.Store(request, runnerLedgerEntrySuccessEvidenceRequestRecord{
		request: request, binder: binder, candidateBinding: candidateBinding,
		cursorValid: request.cursor.valid, canonical: request.canonical,
	})
	if !validRunnerLedgerEntrySuccessEvidenceRequest(request, binder) {
		runnerLedgerEntrySuccessEvidenceRequestRegistry.Delete(request)
		return nil, fail(CodeEvidenceRecoveryRequired, "runner-ledger-entry-success-evidence-request", "success evidence request could not be sealed", nil)
	}
	return request, nil
}

func validRunnerLedgerEntrySuccessEvidenceRequest(request *runnerLedgerEntrySuccessEvidenceRequest, binder runnerLedgerEntrySuccessEvidenceBinder) bool {
	if request == nil || request.self != request || request.consumed || request.binder == nil ||
		binder == nil || !sameRunnerOwnedPointer(request.binder, binder) || request.candidateBinding == nil ||
		request.generation.owner != request.candidateBinding.owner || request.recoveryDigest == ([32]byte{}) ||
		!request.cursor.Valid() || !sameGenerationIdentity(request.cursor.generation, request.generation) ||
		request.plan.validateExact() != nil || request.maxAttempts == 0 || validateEvidenceRecord(request.record) != nil ||
		request.canonical == ([32]byte{}) || request.canonical != runnerLedgerEntrySuccessEvidenceRequestDigest(request) {
		return false
	}
	registered, ok := runnerLedgerEntrySuccessEvidenceRequestRegistry.Load(request)
	record, recordOK := registered.(runnerLedgerEntrySuccessEvidenceRequestRecord)
	return ok && recordOK && record.request == request && record.candidateBinding == request.candidateBinding &&
		record.cursorValid == request.cursor.valid && record.canonical == request.canonical &&
		sameRunnerOwnedPointer(record.binder, request.binder)
}

func consumeRunnerLedgerEntrySuccessEvidenceRequest(request *runnerLedgerEntrySuccessEvidenceRequest, binder runnerLedgerEntrySuccessEvidenceBinder) (runnerLedgerEntrySuccessEvidenceRequest, error) {
	if request == nil || request.self != request {
		return runnerLedgerEntrySuccessEvidenceRequest{}, fail(CodeEvidenceRecoveryRequired, "runner-ledger-entry-success-evidence-claim", "success evidence request is unavailable", nil)
	}
	registered, loaded := runnerLedgerEntrySuccessEvidenceRequestRegistry.LoadAndDelete(request)
	record, recordOK := registered.(runnerLedgerEntrySuccessEvidenceRequestRecord)
	if !loaded || !recordOK || record.request != request {
		return runnerLedgerEntrySuccessEvidenceRequest{}, fail(CodeEvidenceRecoveryRequired, "runner-ledger-entry-success-evidence-claim", "success evidence request changed or was already consumed", nil)
	}
	valid := record.candidateBinding == request.candidateBinding &&
		record.cursorValid == request.cursor.valid && record.canonical == request.canonical &&
		sameRunnerOwnedPointer(record.binder, binder) && validRunnerLedgerEntrySuccessEvidenceRequestWithoutRegistry(request, binder)
	if !valid {
		request.consumed = true
		request.binder = nil
		return runnerLedgerEntrySuccessEvidenceRequest{}, fail(CodeEvidenceRecoveryRequired, "runner-ledger-entry-success-evidence-claim", "success evidence request changed or was already consumed", nil)
	}
	ownedPlan, err := cloneRunnerStatementIntentPlan(request.plan)
	if err != nil {
		request.consumed = true
		request.binder = nil
		return runnerLedgerEntrySuccessEvidenceRequest{}, err
	}
	claimed := runnerLedgerEntrySuccessEvidenceRequest{
		candidateBinding: request.candidateBinding, generation: request.generation,
		recoveryDigest: request.recoveryDigest, cursor: request.cursor.clone(),
		record: cloneEvidenceRecord(request.record), plan: ownedPlan,
		maxAttempts: request.maxAttempts, canonical: request.canonical,
	}
	request.consumed = true
	request.binder = nil
	return claimed, nil
}

func validRunnerLedgerEntrySuccessEvidenceRequestWithoutRegistry(request *runnerLedgerEntrySuccessEvidenceRequest, binder runnerLedgerEntrySuccessEvidenceBinder) bool {
	return request != nil && request.self == request && !request.consumed && request.binder != nil &&
		binder != nil && sameRunnerOwnedPointer(request.binder, binder) && request.candidateBinding != nil &&
		request.generation.owner == request.candidateBinding.owner && request.recoveryDigest != ([32]byte{}) &&
		request.cursor.Valid() && sameGenerationIdentity(request.cursor.generation, request.generation) &&
		request.plan.validateExact() == nil && request.maxAttempts > 0 && validateEvidenceRecord(request.record) == nil &&
		request.canonical != ([32]byte{}) && request.canonical == runnerLedgerEntrySuccessEvidenceRequestDigest(request)
}

func runnerLedgerEntrySuccessEvidenceRequestDigest(request *runnerLedgerEntrySuccessEvidenceRequest) [32]byte {
	if request == nil || request.self != request || request.consumed || request.binder == nil ||
		request.candidateBinding == nil || request.generation.owner != request.candidateBinding.owner ||
		request.recoveryDigest == ([32]byte{}) || !request.cursor.Valid() ||
		!sameGenerationIdentity(request.cursor.generation, request.generation) || request.maxAttempts == 0 ||
		request.plan.validateExact() != nil || validateEvidenceRecord(request.record) != nil {
		return [32]byte{}
	}
	recordCanonical, err := canonicalContractKey(request.record)
	if err != nil || recordCanonical == "" {
		return [32]byte{}
	}
	h := sha256.New()
	h.Write([]byte(runnerLedgerEntrySuccessEvidenceRequestDomain + "\x00"))
	h.Write(request.candidateBinding.canonical[:])
	h.Write(request.recoveryDigest[:])
	for _, value := range runnerLedgerEntrySuccessWriterIdentityStrings() {
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

func runnerLedgerEntrySuccessWriterIdentityStrings() []string {
	// The execution-admission identity closure already ends with the bound
	// success-writer profile. Return an owned copy rather than encoding that
	// profile twice under the evidence-request domain.
	return append([]string(nil), runnerLedgerEntryExecutionAdmissionProfileIdentityStrings()...)
}

func bindRunnerLedgerEntrySuccessOwnedRecord(claimed runnerLedgerEntrySuccessEvidenceRequest, prefix []EvidenceFrame, chain verifiedEvidenceChainWitness) (*OwnedEvidenceRecord, error) {
	if len(prefix) == 0 || claimed.plan.validateExact() != nil || claimed.maxAttempts == 0 ||
		!claimed.cursor.Valid() || claimed.cursor.previousRecordDigest == nil ||
		prefix[len(prefix)-1].RecordDigest != *claimed.cursor.previousRecordDigest {
		return nil, invalidEvidence("runner-ledger-entry-success-evidence", "candidate prefix or plan")
	}
	context := ownedAppendContext{
		generation: claimed.generation, cursor: claimed.cursor.clone(),
		prefix: cloneProjectionValue(prefix), chain: cloneRunnerEvidenceChainWitness(chain),
	}
	var witness ownedEvidenceWitness
	switch {
	case claimed.record.StatementIntent != nil:
		witness = ownedStatementIntentWitness{ownedAppendContext: context, plan: claimed.plan}
	case claimed.record.Intermediate != nil:
		prior := prefix[len(prefix)-1]
		if prior.Record.StatementIntent == nil || claimed.record.Intermediate.State.IntermediateStateDigest.Validate() != nil {
			return nil, invalidEvidence("runner-ledger-entry-success-evidence", "intermediate predecessor")
		}
		witness = ownedIntermediateWitness{
			ownedAppendContext: context, plan: claimed.plan,
			stateDigest: claimed.record.Intermediate.State.IntermediateStateDigest,
			priorIntent: cloneProjectionValue(prior),
		}
	case claimed.record.CommitIntent != nil:
		prior := prefix[len(prefix)-1]
		if prior.Record.Intermediate == nil {
			return nil, invalidEvidence("runner-ledger-entry-success-evidence", "commit predecessor")
		}
		witness = ownedCommitIntentWitness{
			ownedAppendContext:           context,
			priorIntermediateStateDigest: claimed.record.CommitIntent.LastIntermediateStateDigest,
			lastIntermediateRecordDigest: prior.RecordDigest,
			priorIntermediate:            cloneProjectionValue(prior),
		}
	case claimed.record.AttemptTerminal != nil:
		witness = ownedAttemptTerminalWitness{
			ownedAppendContext: context,
			terminalDigest:     claimed.record.AttemptTerminal.TerminalDigest,
			maxAttempts:        claimed.maxAttempts,
		}
	default:
		return nil, invalidEvidence("runner-ledger-entry-success-evidence", "unsupported success record kind")
	}
	return bindOwnedEvidenceRecord(claimed.record, witness)
}
