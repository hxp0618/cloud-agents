package migration

import (
	"context"
	"crypto/sha256"
)

const runnerLedgerReturnFailureResultDigestDomain = "cloud-agents/runner-ledger-return-failure/result/v1"

type runnerLedgerRecoveryActionResultKind uint8

const (
	runnerLedgerRecoveryActionReenter runnerLedgerRecoveryActionResultKind = iota + 1
	runnerLedgerRecoveryActionEntryCommitted
)

type runnerLedgerRecoveryActionResult struct {
	kind               runnerLedgerRecoveryActionResultKind
	outcome            runnerLedgerEntrySuccessOutcome
	ambiguousRecovered bool
}

func (result runnerLedgerRecoveryActionResult) valid() bool {
	switch result.kind {
	case runnerLedgerRecoveryActionReenter:
		return !result.outcome.valid() && !result.ambiguousRecovered
	case runnerLedgerRecoveryActionEntryCommitted:
		return result.outcome.valid()
	default:
		return false
	}
}

// runnerLedgerReturnFailureResult is ordinary, replay-bound data. It carries
// no session, journal, permit, registry, or mutation capability. The typed
// public error is created only after this value reproduces its own canonical
// digest from the exact recovery permit and durable terminal boundary.
type runnerLedgerReturnFailureResult struct {
	state                  RecoveryState
	migrationID            string
	attemptIndex           uint32
	stableErrorCode        ErrorCode
	failure                StableFailureEvidence
	terminalOutcome        string
	terminalDigest         Digest
	terminalRecordDigest   Digest
	resolutionPresent      bool
	resolutionOutcome      string
	resolutionDigest       Digest
	resolutionRecordDigest Digest

	executionLineageDigest         Digest
	journalIdentityDigest          Digest
	runnerProjectionDecisionDigest Digest
	schemaBundleDigest             Digest
	recoveryDigest                 [32]byte
	recoveryTail                   Digest
	consumerFactSubject            Digest
	evidenceBoundary               [32]byte
	permitCanonical                [32]byte
	ledgerDigest                   Digest
	ledgerHead                     string
	ledgerLength                   uint32
	catalogDigest                  Digest
	runtimeInputs                  [32]byte
	canonical                      [32]byte
}

func (result runnerLedgerReturnFailureResult) valid() bool {
	return result.canonical != ([32]byte{}) && result.canonical == runnerLedgerReturnFailureResultDigest(result)
}

func runnerLedgerReturnFailureResultDigest(result runnerLedgerReturnFailureResult) [32]byte {
	if !stringIn(string(result.state), string(RecoveryTerminal), string(RecoveryDivergent)) ||
		!migrationIDPattern.MatchString(result.migrationID) || result.attemptIndex == 0 ||
		result.failure.Validate() != nil || result.failure.Code != result.stableErrorCode ||
		result.terminalDigest.Validate() != nil || result.terminalRecordDigest.Validate() != nil ||
		result.executionLineageDigest.Validate() != nil || result.journalIdentityDigest.Validate() != nil ||
		result.runnerProjectionDecisionDigest.Validate() != nil || result.schemaBundleDigest.Validate() != nil ||
		result.recoveryDigest == ([32]byte{}) || result.recoveryTail.Validate() != nil ||
		result.consumerFactSubject.Validate() != nil || result.evidenceBoundary == ([32]byte{}) ||
		result.permitCanonical == ([32]byte{}) || result.ledgerDigest.Validate() != nil ||
		result.catalogDigest.Validate() != nil || result.runtimeInputs == ([32]byte{}) {
		return [32]byte{}
	}
	if result.resolutionPresent {
		if result.resolutionDigest.Validate() != nil || result.resolutionRecordDigest.Validate() != nil || result.resolutionOutcome == "" {
			return [32]byte{}
		}
	} else if result.resolutionOutcome != "" || result.resolutionDigest != "" || result.resolutionRecordDigest != "" {
		return [32]byte{}
	}
	failureCanonical, err := canonicalContractKey(result.failure)
	if err != nil || failureCanonical == "" {
		return [32]byte{}
	}
	h := sha256.New()
	h.Write([]byte(runnerLedgerReturnFailureResultDigestDomain + "\x00"))
	for _, raw := range [][32]byte{
		result.recoveryDigest, result.evidenceBoundary, result.permitCanonical, result.runtimeInputs,
	} {
		h.Write(raw[:])
	}
	for _, value := range []string{
		string(result.state), result.migrationID, string(result.stableErrorCode), failureCanonical,
		result.terminalOutcome, result.terminalDigest.String(), result.terminalRecordDigest.String(),
		result.executionLineageDigest.String(), result.journalIdentityDigest.String(),
		result.runnerProjectionDecisionDigest.String(), result.schemaBundleDigest.String(),
		result.recoveryTail.String(), result.consumerFactSubject.String(), result.ledgerDigest.String(),
		result.ledgerHead, result.catalogDigest.String(),
	} {
		writeAdmissionString(h, value)
	}
	writeAdmissionUint(h, uint64(result.attemptIndex))
	writeAdmissionUint(h, uint64(result.ledgerLength))
	if result.resolutionPresent {
		writeAdmissionString(h, "resolution:present")
		writeAdmissionString(h, result.resolutionOutcome)
		writeAdmissionString(h, result.resolutionDigest.String())
		writeAdmissionString(h, result.resolutionRecordDigest.String())
	} else {
		writeAdmissionString(h, "resolution:absent")
	}
	var digest [32]byte
	copy(digest[:], h.Sum(nil))
	return digest
}

func (runner *Runner) returnRunnerLedgerRecoveryFailure(ctx context.Context, permit *runnerLedgerReturnFailureAdmissionPermit, evidence EvidenceSession, retirement runnerLedgerRecoveryAdmissionRetirement) error {
	result, err := buildRunnerLedgerReturnFailureResult(ctx, permit, evidence)
	if err != nil {
		if permit == nil {
			return err
		}
		return permit.closeWithoutMutation(err)
	}
	major := uint16(0)
	if result.failure.Major != nil {
		major = *result.failure.Major
	}
	message := "verified migration attempt terminated"
	if result.state == RecoveryDivergent {
		message = "verified migration state diverged"
	}
	primary := projectionFailure(
		result.stableErrorCode, result.failure.Phase, result.failure.Path, major, result.failure.Retryable, message,
	)
	closed := permit.closeWithoutMutation(primary)
	if closed != primary {
		return closed
	}
	if !retirement.retire() {
		return fail(CodeEvidenceRecoveryRequired, "runner-ledger-return-failure", "consumed return-failure admission could not be retired", nil)
	}
	return primary
}

func buildRunnerLedgerReturnFailureResult(ctx context.Context, permit *runnerLedgerReturnFailureAdmissionPermit, evidence EvidenceSession) (runnerLedgerReturnFailureResult, error) {
	var result runnerLedgerReturnFailureResult
	if ctx == nil || permit == nil || evidence == nil || !validRunnerLedgerRecoveryAdmissionPermit(permit) {
		return result, fail(CodeEvidenceRecoveryRequired, "runner-ledger-return-failure", "return-failure permit or evidence is unavailable", nil)
	}
	if err := contextAdmissionError(ctx); err != nil {
		return result, err
	}
	core := runnerLedgerRecoveryPermitCore(permit)
	if core == nil || core.action != generatedRunnerLedgerRecoveryProfiles[7].action || core.selection.profileIndex != 7 ||
		core.selection.recoveryAction != RecoveryReturnFailure ||
		(core.selection.recoveryState != RecoveryTerminal && core.selection.recoveryState != RecoveryDivergent) ||
		!sameRunnerOwnedPointer(core.evidenceBinder, evidence) {
		return result, fail(CodeEvidenceRecoveryRequired, "runner-ledger-return-failure", "return-failure permit differs from the generated transition", nil)
	}
	current := evidence.CurrentCandidate()
	active := evidence.ActiveGeneration()
	journal := evidence.Journal()
	snapshot := evidence.RecoverySnapshot()
	if !validOwnedCurrentCandidate(current) || current.binding != core.candidateBinding || active.kind != activeGenerationCurrent ||
		!sameGenerationIdentity(active.identity, core.generation) || !sameRunnerOwnedPointer(active.journal, journal) ||
		!validRecoverySnapshotForJournal(snapshot, core.generation, snapshotCursor(snapshot)) ||
		generationJournalRecoveryDigest(snapshot) != core.recoveryDigest || snapshot.tailDigest != core.recoveryTail ||
		snapshot.state != core.selection.recoveryState || snapshot.nextPermittedAction != RecoveryReturnFailure ||
		snapshot.migrationID == nil || *snapshot.migrationID != core.selection.migrationID ||
		snapshot.attemptIndex == nil || *snapshot.attemptIndex != core.selection.attemptIndex {
		return result, fail(CodeEvidenceRecoveryRequired, "runner-ledger-return-failure", "durable failure boundary changed after admission", nil)
	}
	terminal := snapshot.lastTerminal
	if terminal == nil || terminal.owner != core.generation.owner || !sameGenerationIdentity(terminal.generation, core.generation) ||
		!sameCursorIdentity(terminal.cursor, snapshot.cursor) || terminal.tailDigest != snapshot.tailDigest ||
		terminal.recordDigest.Validate() != nil || terminal.value.Validate(core.selection.maxAttempts) != nil ||
		terminal.value.MigrationID != core.selection.migrationID || terminal.value.AttemptIndex != core.selection.attemptIndex ||
		terminal.value.SchemaBundleDigest != core.generation.schemaBundleDigest || terminal.value.StableErrorCode == nil ||
		terminal.value.FailureEvidence == nil || terminal.value.TerminalDigest.Validate() != nil ||
		snapshot.lastTerminalDigest == nil || *snapshot.lastTerminalDigest != terminal.value.TerminalDigest {
		return result, fail(CodeEvidenceRecoveryRequired, "runner-ledger-return-failure", "durable terminal failure is unavailable or changed", nil)
	}
	if !runnerLedgerReturnFailureShape(snapshot.state, terminal.value, snapshot.lastResolution) {
		return result, fail(CodeEvidenceRecoveryRequired, "runner-ledger-return-failure", "durable terminal and resolution do not encode a return-failure state", nil)
	}
	result = runnerLedgerReturnFailureResult{
		state: snapshot.state, migrationID: terminal.value.MigrationID, attemptIndex: terminal.value.AttemptIndex,
		stableErrorCode: ErrorCode(*terminal.value.StableErrorCode), failure: cloneProjectionValue(*terminal.value.FailureEvidence),
		terminalOutcome: terminal.value.Outcome, terminalDigest: terminal.value.TerminalDigest,
		terminalRecordDigest:           terminal.recordDigest,
		executionLineageDigest:         core.generation.executionLineageDigest,
		journalIdentityDigest:          core.generation.journalIdentityDigest,
		runnerProjectionDecisionDigest: core.generation.runnerProjectionDecisionDigest,
		schemaBundleDigest:             core.generation.schemaBundleDigest,
		recoveryDigest:                 core.recoveryDigest, recoveryTail: core.recoveryTail,
		consumerFactSubject: core.consumerFactSubject, evidenceBoundary: core.evidenceBoundary,
		permitCanonical: core.canonical, ledgerDigest: core.ledgerDigest, ledgerHead: core.ledgerHead,
		ledgerLength: core.ledgerLength, catalogDigest: core.catalogDigest, runtimeInputs: core.runtimeInputs,
	}
	if resolution := snapshot.lastResolution; resolution != nil {
		result.resolutionPresent = true
		result.resolutionOutcome = resolution.value.Outcome
		result.resolutionDigest = resolution.value.ResolutionDigest
		result.resolutionRecordDigest = resolution.recordDigest
	}
	result.canonical = runnerLedgerReturnFailureResultDigest(result)
	if !result.valid() {
		return runnerLedgerReturnFailureResult{}, fail(CodeEvidenceRecoveryRequired, "runner-ledger-return-failure", "typed failure result could not be reproduced", nil)
	}
	if err := contextAdmissionError(ctx); err != nil {
		return runnerLedgerReturnFailureResult{}, err
	}
	return result, nil
}

func snapshotCursor(snapshot *RecoverySnapshot) JournalCursor {
	if snapshot == nil {
		return JournalCursor{}
	}
	return snapshot.cursor
}

func runnerLedgerReturnFailureShape(state RecoveryState, terminal AttemptTerminalState, resolution *OwnedRecovered[AmbiguousResolutionState]) bool {
	switch state {
	case RecoveryTerminal:
		if resolution == nil {
			return terminal.Outcome == "aborted_terminal" || terminal.Outcome == "ambiguous_reconciled_pending"
		}
		value := resolution.value
		return terminal.Outcome == "ambiguous_unresolved" && value.Validate() == nil &&
			value.MigrationID == terminal.MigrationID && value.AttemptIndex == terminal.AttemptIndex &&
			value.UnresolvedTerminalDigest == terminal.TerminalDigest && value.Outcome == "resolved_pending" &&
			value.StableErrorCode == ErrorCode(*terminal.StableErrorCode) &&
			resolution.recordDigest.Validate() == nil
	case RecoveryDivergent:
		if resolution == nil {
			return terminal.Outcome == "ambiguous_divergent"
		}
		value := resolution.value
		return terminal.Outcome == "ambiguous_unresolved" && value.Validate() == nil &&
			value.MigrationID == terminal.MigrationID && value.AttemptIndex == terminal.AttemptIndex &&
			value.UnresolvedTerminalDigest == terminal.TerminalDigest && value.Outcome == "resolved_divergent" &&
			value.StableErrorCode == ErrorCode(*terminal.StableErrorCode) &&
			resolution.recordDigest.Validate() == nil
	default:
		return false
	}
}
