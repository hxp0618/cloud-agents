package migration

import (
	"context"
	"crypto/sha256"
	"errors"
	"sync"
	"sync/atomic"
)

// runnerDurableCommittedTerminal proves the unique committed terminal and its
// lineage checkpoint are durable. bundleComplete distinguishes final success
// from the exact next-entry continuation; neither case retains database power.
type runnerDurableCommittedTerminal struct {
	self                     *runnerDurableCommittedTerminal
	binding                  *runnerDurableCommittedTerminalBinding
	evidence                 EvidenceSession
	journal                  EvidenceJournal
	candidateBinding         *verifiedEvidenceRunBinding
	generation               generationIdentity
	commitCanonical          [32]byte
	prefixCanonical          [32]byte
	recoveryDigest           [32]byte
	connectionCloseProven    bool
	terminal                 AttemptTerminalState
	cursor                   JournalCursor
	terminalRecordDigest     Digest
	intentRecordDigest       Digest
	intermediateRecordDigest Digest
	commitRecordDigest       Digest
	checkpointDigest         Digest
	bundleComplete           bool
	nextAction               RecoveryAction
	canonical                [32]byte
	closed                   bool
}

type runnerDurableCommittedTerminalBinding struct {
	prepared         *runnerDurableCommittedTerminal
	evidence         EvidenceSession
	journal          EvidenceJournal
	candidateBinding *verifiedEvidenceRunBinding
	cursorValid      *atomic.Bool
	canonical        [32]byte
}

type runnerDurableCommittedTerminalRegistryRecord struct {
	prepared         *runnerDurableCommittedTerminal
	binding          *runnerDurableCommittedTerminalBinding
	evidence         EvidenceSession
	journal          EvidenceJournal
	candidateBinding *verifiedEvidenceRunBinding
	cursorValid      *atomic.Bool
	canonical        [32]byte
}

var runnerDurableCommittedTerminalRegistry sync.Map

func (runner *Runner) appendCommittedTerminal(ctx context.Context, closed *runnerClosedCurrentCommit) (*runnerDurableCommittedTerminal, error) {
	seed, err := snapshotRunnerCommittedTerminalSeed(closed)
	if err != nil {
		return nil, closeRunnerClosedCurrentCommitIfRegistered(closed, err)
	}
	if ctx == nil || runner == nil {
		return nil, closeRunnerClosedCurrentCommit(closed, fail(CodeEvidenceJournalFailed, "runner-committed-terminal", "committed terminal context or runner is unavailable", nil))
	}
	if contextErr := ctx.Err(); contextErr != nil {
		return nil, closeRunnerClosedCurrentCommit(closed, mapRunnerCommittedTerminalError(contextErr, "runner-committed-terminal-bind", "committed terminal append was interrupted"))
	}
	binder, ok := seed.evidence.(runnerCommittedTerminalRecordBinder)
	if !ok {
		return nil, closeRunnerClosedCurrentCommit(closed, fail(CodeEvidenceRecoveryRequired, "runner-committed-terminal-bind", "evidence session cannot bind a committed terminal", nil))
	}
	journal, cursor, owned, bindErr := binder.bindRunnerCommittedTerminalRecord(ctx, closed)
	if bindErr != nil || journal == nil || !runnerOwnedPointer(journal) || owned == nil || owned.wire.AttemptTerminal == nil {
		if bindErr == nil {
			bindErr = fail(CodeEvidenceRecoveryRequired, "runner-committed-terminal-bind", "committed terminal record authority is unavailable", nil)
		}
		mapped := mapRunnerCommittedTerminalError(bindErr, "runner-committed-terminal-bind", "committed terminal record could not be bound")
		return nil, closeRunnerClosedCurrentCommitIfRegistered(closed, mapped)
	}
	if !validRunnerCommittedTerminalBoundRecord(seed, journal, cursor, owned) {
		return nil, fail(CodeEvidenceJournalFailed, "runner-committed-terminal-bind", "committed terminal record authority is contradictory", nil)
	}
	expectedFrame := EvidenceFrame{
		FormatVersion: EvidenceFrameFormat, Sequence: cursor.nextSequence,
		PreviousRecordDigest: cloneDigestPointer(cursor.previousRecordDigest),
		RecordKind:           EvidenceRecordAttemptTerminal, Record: cloneEvidenceRecord(owned.wire),
	}
	expectedFrame.RecordDigest, err = expectedFrame.ComputeDigest()
	if err != nil || expectedFrame.Validate() != nil {
		return nil, fail(CodeEvidenceJournalFailed, "runner-committed-terminal-bind", "committed terminal frame identity could not be sealed", nil)
	}
	result, appendErr := journal.AppendDurable(ctx, cursor, owned)
	if appendErr != nil || result.outcome != appendOutcomeDurable {
		invalidateRunnerStatementIntentAppendResult(result)
		if appendErr == nil {
			appendErr = fail(CodeEvidenceJournalFailed, "runner-committed-terminal-append", "committed terminal durability is unknown", nil)
		}
		return nil, mapRunnerCommittedTerminalError(appendErr, "runner-committed-terminal-append", "committed terminal was not proven durable")
	}
	durableCursor, resultErr := validateRunnerCommittedTerminalAppendResult(cursor, seed.generation, expectedFrame.RecordDigest, result)
	if resultErr != nil {
		return nil, resultErr
	}
	snapshot := seed.evidence.RecoverySnapshot()
	prefixCanonical := runnerCommittedTerminalPrefixDigest(seed.intent, seed.intermediate, seed.commit, seed.intentRecordDigest, seed.intermediateRecordDigest, seed.commitRecordDigest)
	if prefixCanonical == ([32]byte{}) {
		durableCursor.valid.Store(false)
		return nil, fail(CodeEvidenceJournalFailed, "runner-committed-terminal-evidence", "committed terminal predecessor identity is unavailable", nil)
	}
	recoveryDigest, bundleComplete, nextAction, evidenceErr := runnerDurableCommittedTerminalEvidenceDigest(
		seed.evidence, journal, seed.candidateBinding, seed.generation, durableCursor,
		prefixCanonical, seed.intentRecordDigest, seed.intermediateRecordDigest, seed.commitRecordDigest,
		expectedFrame.RecordDigest, expectedFrame.Record.AttemptTerminal, snapshot,
	)
	if evidenceErr != nil {
		durableCursor.valid.Store(false)
		return nil, evidenceErr
	}
	prepared, sealErr := bindRunnerDurableCommittedTerminal(
		seed, journal, durableCursor, *expectedFrame.Record.AttemptTerminal,
		expectedFrame.RecordDigest, result.candidateCheckpointRecordDigest,
		prefixCanonical, recoveryDigest, bundleComplete, nextAction,
	)
	if sealErr != nil {
		durableCursor.valid.Store(false)
		return nil, sealErr
	}
	return prepared, nil
}

func validRunnerCommittedTerminalBoundRecord(seed runnerCommittedTerminalSeed, journal EvidenceJournal, cursor JournalCursor, owned *OwnedEvidenceRecord) bool {
	if seed.evidence == nil || journal == nil || !sameRunnerOwnedPointer(seed.journal, journal) || !sameRunnerOwnedPointer(seed.evidence.Journal(), journal) || owned == nil || owned.consumed == nil || owned.consumed.Load() || owned.witness == nil || !cursor.Valid() || !sameCursorIdentity(cursor, seed.cursor) || !sameGenerationIdentity(cursor.generation, seed.generation) || cursor.segmentIndex != 0 || cursor.nextSequence != 4 || cursor.previousRecordDigest == nil || *cursor.previousRecordDigest != seed.commitRecordDigest || cursor.latestCheckpointRecordDigest == nil || !sameGenerationIdentity(owned.generation, seed.generation) || !sameCursorIdentity(owned.cursor, cursor) {
		return false
	}
	witness, ok := owned.witness.(ownedAttemptTerminalWitness)
	want, err := buildRunnerCommittedTerminal(seed)
	terminal := owned.wire.AttemptTerminal
	return ok && err == nil && terminal != nil && terminal.Validate(seed.maxAttempts) == nil && witness.retry == nil && witness.maxAttempts == seed.maxAttempts && witness.terminalDigest == terminal.TerminalDigest && sameGenerationIdentity(witness.generation, seed.generation) && sameCursorIdentity(witness.cursor, cursor) && canonicalEqual(*terminal, want)
}

func runnerCommittedTerminalPrefixDigest(intent StatementIntent, intermediate StatementIntermediateEvidence, commit CommitIntent, intentRecordDigest, intermediateRecordDigest, commitRecordDigest Digest) [32]byte {
	if intent.Validate() != nil || intermediate.Validate() != nil || commit.Validate() != nil || intentRecordDigest.Validate() != nil || intermediateRecordDigest.Validate() != nil || commitRecordDigest.Validate() != nil {
		return [32]byte{}
	}
	h := sha256.New()
	h.Write([]byte("cloud-agents-platform-runner-committed-terminal-prefix/v1\x00"))
	for _, value := range []any{intent, intermediate, commit} {
		canonical, err := canonicalContractKey(value)
		if err != nil || canonical == "" {
			return [32]byte{}
		}
		writeAdmissionString(h, canonical)
	}
	for _, digest := range []Digest{intentRecordDigest, intermediateRecordDigest, commitRecordDigest} {
		writeAdmissionString(h, digest.String())
	}
	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result
}

func validateRunnerCommittedTerminalAppendResult(cursor JournalCursor, generation generationIdentity, expectedRecord Digest, result AppendResult) (JournalCursor, error) {
	durable := result.DurableCursor()
	if result.outcome != appendOutcomeDurable || durable == nil || !durable.Valid() || cursor.valid == nil || durable.valid == nil || durable.valid == cursor.valid || cursor.Valid() || !sameGenerationIdentity(cursor.generation, generation) || !sameGenerationIdentity(durable.generation, generation) || cursor.segmentIndex != 0 || cursor.nextSequence != 4 || cursor.previousRecordDigest == nil || cursor.latestCheckpointRecordDigest == nil || result.candidateSequence != cursor.nextSequence || !equalDigestPointer(result.candidatePreviousRecordDigest, cursor.previousRecordDigest) || result.candidateRecordDigest != expectedRecord || result.candidateCheckpointRecordDigest.Validate() != nil || result.rotationHeaderRecordDigest != nil || result.rotationHeaderCheckpointRecordDigest != nil || durable.segmentIndex != cursor.segmentIndex || durable.nextSequence != cursor.nextSequence+1 || durable.previousRecordDigest == nil || *durable.previousRecordDigest != expectedRecord || durable.lineageIndexNextSequence != cursor.lineageIndexNextSequence+1 || durable.lineageIndexPreviousRecordDigest != result.candidateCheckpointRecordDigest || durable.latestCheckpointRecordDigest == nil || *durable.latestCheckpointRecordDigest != result.candidateCheckpointRecordDigest {
		if durable != nil && durable.valid != nil {
			durable.valid.Store(false)
		}
		return JournalCursor{}, fail(CodeEvidenceJournalFailed, "runner-committed-terminal-append", "durable committed terminal result is contradictory", nil)
	}
	return durable.clone(), nil
}

func runnerDurableCommittedTerminalEvidenceDigest(evidence EvidenceSession, journal EvidenceJournal, candidateBinding *verifiedEvidenceRunBinding, generation generationIdentity, cursor JournalCursor, prefixCanonical [32]byte, intentRecordDigest, intermediateRecordDigest, commitRecordDigest, recordDigest Digest, terminal *AttemptTerminalState, snapshot *RecoverySnapshot) ([32]byte, bool, RecoveryAction, error) {
	if evidence == nil || journal == nil || candidateBinding == nil || !cursor.Valid() || prefixCanonical == ([32]byte{}) || intentRecordDigest.Validate() != nil || intermediateRecordDigest.Validate() != nil || commitRecordDigest.Validate() != nil || recordDigest.Validate() != nil || terminal == nil || terminal.Validate() != nil || snapshot == nil {
		return [32]byte{}, false, "", fail(CodeEvidenceJournalFailed, "runner-committed-terminal-evidence", "durable committed terminal evidence is unavailable", nil)
	}
	current := evidence.CurrentCandidate()
	active := evidence.ActiveGeneration()
	currentJournal := evidence.Journal()
	recoveredTerminal := snapshot.lastTerminal
	recoveredIntent := snapshot.lastStatementIntent
	recoveredIntermediate := snapshot.lastIntermediateEvidence
	recoveredCommit := snapshot.commitIntent
	if !validOwnedCurrentCandidate(current) || current.binding != candidateBinding || active.kind != activeGenerationCurrent || !sameGenerationIdentity(active.identity, generation) || !sameRunnerOwnedPointer(active.journal, journal) || !sameRunnerOwnedPointer(currentJournal, journal) || !validRecoverySnapshotForJournal(snapshot, generation, cursor) || snapshot.migrationID == nil || *snapshot.migrationID != terminal.MigrationID || snapshot.attemptIndex == nil || *snapshot.attemptIndex != terminal.AttemptIndex || snapshot.tailDigest != recordDigest || snapshot.lastTerminalDigest == nil || *snapshot.lastTerminalDigest != terminal.TerminalDigest || recoveredTerminal == nil || recoveredTerminal.owner != generation.owner || !sameGenerationIdentity(recoveredTerminal.generation, generation) || !sameCursorIdentity(recoveredTerminal.cursor, cursor) || recoveredTerminal.tailDigest != recordDigest || recoveredTerminal.recordDigest != recordDigest || !canonicalEqual(recoveredTerminal.value, *terminal) || recoveredIntent == nil || recoveredIntermediate == nil || recoveredCommit == nil || recoveredIntent.owner != generation.owner || recoveredIntermediate.owner != generation.owner || recoveredCommit.owner != generation.owner || !sameGenerationIdentity(recoveredIntent.generation, generation) || !sameGenerationIdentity(recoveredIntermediate.generation, generation) || !sameGenerationIdentity(recoveredCommit.generation, generation) || !sameCursorIdentity(recoveredIntent.cursor, cursor) || !sameCursorIdentity(recoveredIntermediate.cursor, cursor) || !sameCursorIdentity(recoveredCommit.cursor, cursor) || recoveredIntent.tailDigest != recordDigest || recoveredIntermediate.tailDigest != recordDigest || recoveredCommit.tailDigest != recordDigest || snapshot.lastStatementIntentRecordDigest == nil || *snapshot.lastStatementIntentRecordDigest != intentRecordDigest || recoveredIntent.recordDigest != intentRecordDigest || snapshot.lastIntermediateEvidenceRecordDigest == nil || *snapshot.lastIntermediateEvidenceRecordDigest != intermediateRecordDigest || recoveredIntermediate.recordDigest != intermediateRecordDigest || snapshot.lastCommitIntentRecordDigest == nil || *snapshot.lastCommitIntentRecordDigest != commitRecordDigest || recoveredCommit.recordDigest != commitRecordDigest || runnerCommittedTerminalPrefixDigest(recoveredIntent.value, recoveredIntermediate.value, recoveredCommit.value, intentRecordDigest, intermediateRecordDigest, commitRecordDigest) != prefixCanonical || snapshot.lastResolution != nil || snapshot.lastResolutionDigest != nil || snapshot.lineageContinuation != nil || !equalDigestPointer(snapshot.previousAttemptTerminalDigest, terminal.PreviousAttemptTerminalDigest) {
		return [32]byte{}, false, "", fail(CodeEvidenceJournalFailed, "runner-committed-terminal-evidence", "durable committed terminal recovery boundary is invalid", nil)
	}
	bundleComplete := snapshot.state == RecoveryCompleted && snapshot.nextPermittedAction == RecoveryReturnSuccess
	nextEntry := snapshot.state == RecoveryTerminal && snapshot.nextPermittedAction == RecoveryBeginFirstAttemptNextEntry
	if bundleComplete == nextEntry {
		return [32]byte{}, false, "", fail(CodeEvidenceJournalFailed, "runner-committed-terminal-evidence", "committed terminal next action is contradictory", nil)
	}
	digest := generationJournalRecoveryDigest(snapshot)
	if digest == ([32]byte{}) {
		return [32]byte{}, false, "", fail(CodeEvidenceJournalFailed, "runner-committed-terminal-evidence", "durable committed terminal recovery identity is unavailable", nil)
	}
	return digest, bundleComplete, snapshot.nextPermittedAction, nil
}

func bindRunnerDurableCommittedTerminal(seed runnerCommittedTerminalSeed, journal EvidenceJournal, cursor JournalCursor, terminal AttemptTerminalState, recordDigest, checkpointDigest Digest, prefixCanonical, recoveryDigest [32]byte, bundleComplete bool, nextAction RecoveryAction) (*runnerDurableCommittedTerminal, error) {
	if journal == nil || !runnerOwnedPointer(journal) || !cursor.Valid() || terminal.Validate(seed.maxAttempts) != nil || recordDigest.Validate() != nil || checkpointDigest.Validate() != nil || prefixCanonical == ([32]byte{}) || recoveryDigest == ([32]byte{}) || bundleComplete != (nextAction == RecoveryReturnSuccess) || !stringIn(string(nextAction), string(RecoveryReturnSuccess), string(RecoveryBeginFirstAttemptNextEntry)) {
		return nil, fail(CodeEvidenceJournalFailed, "runner-committed-terminal-seal", "durable committed terminal inputs are unavailable", nil)
	}
	prepared := &runnerDurableCommittedTerminal{
		evidence: seed.evidence, journal: journal, candidateBinding: seed.candidateBinding,
		generation: seed.generation, commitCanonical: seed.commitCanonical,
		prefixCanonical: prefixCanonical, recoveryDigest: recoveryDigest, connectionCloseProven: seed.connectionCloseProven,
		terminal: cloneProjectionValue(terminal), cursor: cursor.clone(), terminalRecordDigest: recordDigest,
		intentRecordDigest: seed.intentRecordDigest, intermediateRecordDigest: seed.intermediateRecordDigest, commitRecordDigest: seed.commitRecordDigest,
		checkpointDigest: checkpointDigest, bundleComplete: bundleComplete, nextAction: nextAction,
	}
	prepared.self = prepared
	binding := &runnerDurableCommittedTerminalBinding{
		prepared: prepared, evidence: seed.evidence, journal: journal,
		candidateBinding: seed.candidateBinding, cursorValid: cursor.valid,
	}
	prepared.binding = binding
	prepared.canonical = runnerDurableCommittedTerminalDigest(prepared)
	binding.canonical = prepared.canonical
	if prepared.canonical == ([32]byte{}) {
		return nil, fail(CodeEvidenceJournalFailed, "runner-committed-terminal-seal", "durable committed terminal could not be identified", nil)
	}
	runnerDurableCommittedTerminalRegistry.Store(prepared, runnerDurableCommittedTerminalRegistryRecord{
		prepared: prepared, binding: binding, evidence: seed.evidence, journal: journal,
		candidateBinding: seed.candidateBinding, cursorValid: cursor.valid, canonical: prepared.canonical,
	})
	if !validRunnerDurableCommittedTerminal(prepared) {
		runnerDurableCommittedTerminalRegistry.Delete(prepared)
		return nil, fail(CodeEvidenceJournalFailed, "runner-committed-terminal-seal", "durable committed terminal could not be sealed", nil)
	}
	return prepared, nil
}

func validRunnerDurableCommittedTerminal(prepared *runnerDurableCommittedTerminal) bool {
	if prepared == nil || prepared.self != prepared || prepared.closed || prepared.binding == nil || prepared.binding.prepared != prepared || prepared.binding.candidateBinding != prepared.candidateBinding || prepared.binding.cursorValid != prepared.cursor.valid || !sameRunnerOwnedPointer(prepared.binding.evidence, prepared.evidence) || !sameRunnerOwnedPointer(prepared.binding.journal, prepared.journal) || prepared.canonical == ([32]byte{}) || prepared.canonical != prepared.binding.canonical || prepared.canonical != runnerDurableCommittedTerminalDigest(prepared) {
		return false
	}
	registered, loaded := runnerDurableCommittedTerminalRegistry.Load(prepared)
	record, recordOK := registered.(runnerDurableCommittedTerminalRegistryRecord)
	return loaded && recordOK && record.prepared == prepared && record.binding == prepared.binding && record.candidateBinding == prepared.candidateBinding && record.cursorValid == prepared.cursor.valid && record.canonical == prepared.canonical && sameRunnerOwnedPointer(record.evidence, prepared.evidence) && sameRunnerOwnedPointer(record.journal, prepared.journal)
}

func runnerDurableCommittedTerminalDigest(prepared *runnerDurableCommittedTerminal) [32]byte {
	if prepared == nil || prepared.self != prepared || prepared.closed || prepared.evidence == nil || prepared.journal == nil || prepared.candidateBinding == nil || prepared.candidateBinding.owner == nil || prepared.generation.owner != prepared.candidateBinding.owner || prepared.candidateBinding.canonical == ([32]byte{}) || prepared.commitCanonical == ([32]byte{}) || prepared.prefixCanonical == ([32]byte{}) || prepared.recoveryDigest == ([32]byte{}) || prepared.terminal.Validate() != nil || prepared.terminal.Outcome != "committed" || prepared.terminal.StableErrorCode != nil || prepared.terminal.FailureEvidence != nil || prepared.terminal.RetryProof != nil || prepared.terminal.ReconcileResult != "not_run" || prepared.intentRecordDigest.Validate() != nil || prepared.intermediateRecordDigest.Validate() != nil || prepared.commitRecordDigest.Validate() != nil || prepared.terminalRecordDigest.Validate() != nil || prepared.checkpointDigest.Validate() != nil || !prepared.cursor.Valid() || !sameGenerationIdentity(prepared.cursor.generation, prepared.generation) || prepared.cursor.previousRecordDigest == nil || *prepared.cursor.previousRecordDigest != prepared.terminalRecordDigest || prepared.cursor.latestCheckpointRecordDigest == nil || *prepared.cursor.latestCheckpointRecordDigest != prepared.checkpointDigest || prepared.bundleComplete != (prepared.nextAction == RecoveryReturnSuccess) || !stringIn(string(prepared.nextAction), string(RecoveryReturnSuccess), string(RecoveryBeginFirstAttemptNextEntry)) {
		return [32]byte{}
	}
	digest, bundleComplete, nextAction, err := runnerDurableCommittedTerminalEvidenceDigest(
		prepared.evidence, prepared.journal, prepared.candidateBinding, prepared.generation,
		prepared.cursor, prepared.prefixCanonical, prepared.intentRecordDigest, prepared.intermediateRecordDigest, prepared.commitRecordDigest,
		prepared.terminalRecordDigest, &prepared.terminal, prepared.evidence.RecoverySnapshot(),
	)
	if err != nil || digest != prepared.recoveryDigest || bundleComplete != prepared.bundleComplete || nextAction != prepared.nextAction {
		return [32]byte{}
	}
	terminalCanonical, err := canonicalContractKey(prepared.terminal)
	if err != nil || terminalCanonical == "" {
		return [32]byte{}
	}
	h := sha256.New()
	h.Write([]byte("cloud-agents-platform-runner-durable-committed-terminal/v1\x00"))
	h.Write(prepared.candidateBinding.canonical[:])
	h.Write(prepared.commitCanonical[:])
	h.Write(prepared.prefixCanonical[:])
	h.Write(prepared.recoveryDigest[:])
	writeAdmissionString(h, terminalCanonical)
	for _, value := range []Digest{
		prepared.generation.executionLineageDigest, prepared.generation.journalIdentityDigest,
		prepared.generation.runnerProjectionDecisionDigest, prepared.generation.schemaBundleDigest,
		prepared.intentRecordDigest, prepared.intermediateRecordDigest, prepared.commitRecordDigest,
		prepared.terminalRecordDigest, prepared.checkpointDigest,
	} {
		if value.Validate() != nil {
			return [32]byte{}
		}
		writeAdmissionString(h, value.String())
	}
	writeGenerationJournalCursor(h, prepared.cursor)
	writeAdmissionUint(h, boolUint64(prepared.connectionCloseProven))
	writeAdmissionUint(h, boolUint64(prepared.bundleComplete))
	writeAdmissionString(h, string(prepared.nextAction))
	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result
}

func closeRunnerDurableCommittedTerminal(prepared *runnerDurableCommittedTerminal, primary error) error {
	if prepared == nil {
		return primary
	}
	if prepared.self != prepared {
		return fail(CodeEvidenceJournalFailed, "runner-committed-terminal-close", "durable committed terminal copy cannot be closed", nil)
	}
	registered, loaded := runnerDurableCommittedTerminalRegistry.Load(prepared)
	record, recordOK := registered.(runnerDurableCommittedTerminalRegistryRecord)
	if !loaded || !recordOK || record.prepared != prepared || record.binding == nil {
		return fail(CodeEvidenceJournalFailed, "runner-committed-terminal-close", "durable committed terminal authority is unavailable", nil)
	}
	valid := validRunnerDurableCommittedTerminal(prepared)
	runnerDurableCommittedTerminalRegistry.Delete(prepared)
	closeProven := prepared.connectionCloseProven
	prepared.closed = true
	prepared.evidence = nil
	prepared.journal = nil
	prepared.binding = nil
	prepared.terminal = AttemptTerminalState{}
	if !valid {
		return fail(CodeEvidenceJournalFailed, "runner-committed-terminal-close", "durable committed terminal authority changed before close", nil)
	}
	if !closeProven {
		return fail(CodeTransactionBoundary, "runner-committed-terminal-close", "old database connection close could not be proven", nil)
	}
	return primary
}

func mapRunnerCommittedTerminalError(err error, op, message string) error {
	var stable *Error
	if errors.As(err, &stable) {
		return fail(stable.Code, op, message, nil)
	}
	if errors.Is(err, context.Canceled) {
		return fail(CodeContextCanceled, op, message, nil)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return fail(CodeDeadlineExceeded, op, message, nil)
	}
	return fail(CodeEvidenceJournalFailed, op, message, nil)
}
