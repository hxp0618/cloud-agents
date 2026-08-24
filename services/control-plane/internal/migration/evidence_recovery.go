package migration

// RecoveryState and RecoveryAction are closed runtime enums. They are not wire
// DTOs and do not reuse evidenceJournalSummary.
type RecoveryState string
type RecoveryAction string

const (
	RecoveryBrandNew                RecoveryState = "brand_new"
	RecoveryBrandNewInherited       RecoveryState = "brand_new_inherited"
	RecoveryCompleted               RecoveryState = "completed"
	RecoveryDanglingStatementIntent RecoveryState = "dangling_statement_intent"
	RecoveryDanglingIntermediate    RecoveryState = "dangling_intermediate"
	RecoveryDanglingCommitIntent    RecoveryState = "dangling_commit_intent"
	RecoveryAmbiguousUnresolved     RecoveryState = "ambiguous_unresolved"
	RecoveryTerminal                RecoveryState = "terminal"
	RecoveryDivergent               RecoveryState = "divergent"

	RecoveryBeginFirstAttempt          RecoveryAction = "begin_first_attempt"
	RecoveryAppendAbortedRetryable     RecoveryAction = "append_aborted_retryable"
	RecoveryAppendAbortedTerminal      RecoveryAction = "append_aborted_terminal"
	RecoveryReconcileCommit            RecoveryAction = "reconcile_commit"
	RecoveryBeginNextAttempt           RecoveryAction = "begin_next_attempt"
	RecoveryBeginFirstAttemptNextEntry RecoveryAction = "begin_first_attempt_next_entry"
	RecoveryReturnSuccess              RecoveryAction = "return_success"
	RecoveryReturnFailure              RecoveryAction = "return_failure"
)

// OwnedRecovered is immutable replay-owned data. Value returns a defensive
// clone; its owner/generation/cursor/tail bindings never leave the package.
type OwnedRecovered[T any] struct {
	owner        *evidenceOwnerToken
	generation   generationIdentity
	cursor       JournalCursor
	tailDigest   Digest
	recordDigest Digest
	value        T
}

func (r OwnedRecovered[T]) Value() T             { return cloneProjectionValue(r.value) }
func (r OwnedRecovered[T]) RecordDigest() Digest { return r.recordDigest }

type RecoverySnapshot struct {
	owner                                *evidenceOwnerToken
	generation                           generationIdentity
	cursor                               JournalCursor
	tailDigest                           Digest
	state                                RecoveryState
	migrationID                          *string
	attemptIndex                         *uint32
	lineageContinuation                  *OwnedRecovered[LineageContinuationContext]
	lastStatementIntent                  *OwnedRecovered[StatementIntent]
	lastIntermediateEvidence             *OwnedRecovered[StatementIntermediateEvidence]
	commitIntent                         *OwnedRecovered[CommitIntent]
	lastTerminal                         *OwnedRecovered[AttemptTerminalState]
	lastResolution                       *OwnedRecovered[AmbiguousResolutionState]
	lastTerminalDigest                   *Digest
	lastResolutionDigest                 *Digest
	lastStatementIntentRecordDigest      *Digest
	lastIntermediateEvidenceRecordDigest *Digest
	lastCommitIntentRecordDigest         *Digest
	previousAttemptTerminalDigest        *Digest
	lastIntermediateStateDigest          *Digest
	nextPermittedAction                  RecoveryAction
}

func (s RecoverySnapshot) State() RecoveryState       { return s.state }
func (s RecoverySnapshot) NextAction() RecoveryAction { return s.nextPermittedAction }
func (s RecoverySnapshot) MigrationID() *string       { return cloneStringPointer(s.migrationID) }
func (s RecoverySnapshot) AttemptIndex() *uint32      { return cloneUint32Pointer(s.attemptIndex) }
func (s RecoverySnapshot) TailDigest() Digest         { return s.tailDigest }

func (s RecoverySnapshot) LineageContinuation() *OwnedRecovered[LineageContinuationContext] {
	return cloneOwnedRecovered(s.lineageContinuation)
}
func (s RecoverySnapshot) LastStatementIntent() *OwnedRecovered[StatementIntent] {
	return cloneOwnedRecovered(s.lastStatementIntent)
}
func (s RecoverySnapshot) LastIntermediateEvidence() *OwnedRecovered[StatementIntermediateEvidence] {
	return cloneOwnedRecovered(s.lastIntermediateEvidence)
}
func (s RecoverySnapshot) CommitIntent() *OwnedRecovered[CommitIntent] {
	return cloneOwnedRecovered(s.commitIntent)
}
func (s RecoverySnapshot) LastTerminal() *OwnedRecovered[AttemptTerminalState] {
	return cloneOwnedRecovered(s.lastTerminal)
}
func (s RecoverySnapshot) LastResolution() *OwnedRecovered[AmbiguousResolutionState] {
	return cloneOwnedRecovered(s.lastResolution)
}

func cloneOwnedRecovered[T any](value *OwnedRecovered[T]) *OwnedRecovered[T] {
	if value == nil {
		return nil
	}
	copy := *value
	copy.value = cloneProjectionValue(value.value)
	copy.cursor = value.cursor.clone()
	return &copy
}

type verifiedRecoverySchemaWitness struct {
	owner                       *evidenceOwnerToken
	generation                  generationIdentity
	finalStatementIndex         map[string]uint32
	maxAttempts                 map[string]uint32
	orderedMigrations           []string
	signedExpectedLedgerRows    []CommitIntentLedgerRow
	signedExpectedLedgerDigest  Digest
	durableObservedLedgerPrefix []CommitIntentLedgerRow
	durableObservedLedgerDigest Digest
	finalCatalogDigest          Digest
	chainWitness                verifiedEvidenceChainWitness
}

// recoveredContinuation is created only from an already validated lineage
// replay. inheritedWithoutContext distinguishes the ADR's header-only carry
// from a true brand-new lineage.
type recoveredContinuation struct {
	owned                   *OwnedRecovered[LineageContinuationContext]
	inheritedWithoutContext bool
}

func buildRecoverySnapshot(frames []EvidenceFrame, cursor JournalCursor, generation generationIdentity, continuation recoveredContinuation, schema verifiedRecoverySchemaWitness) (*RecoverySnapshot, error) {
	if len(frames) == 0 || !cursor.Valid() || !sameGenerationIdentity(cursor.generation, generation) || schema.owner == nil || schema.owner != generation.owner || !sameGenerationIdentity(schema.generation, generation) {
		return nil, invalidEvidence("recovery-snapshot", "owner, generation, cursor, or frame set")
	}
	if err := validateEvidenceChainWithWitness(frames, schema.chainWitness); err != nil {
		return nil, err
	}
	if err := validateRecoverySchemaWitness(schema, frames); err != nil {
		return nil, err
	}
	header := frames[0].Record.Header
	if header == nil || !sameGenerationHeader(generation, *header) {
		return nil, invalidEvidence("recovery-snapshot", "header generation")
	}
	tail := frames[len(frames)-1].RecordDigest
	if cursor.nextSequence != frames[len(frames)-1].Sequence+1 || cursor.previousRecordDigest == nil || *cursor.previousRecordDigest != tail {
		return nil, invalidEvidence("recovery-snapshot", "cursor tail")
	}
	var compact *admissionReplayRecoveryTail
	for index := 1; index < len(frames); index++ {
		if frames[index].RecordKind == EvidenceRecordHeader {
			continue
		}
		if compact == nil {
			compact = &admissionReplayRecoveryTail{}
		}
		if err := compact.observe(frames[index]); err != nil {
			return nil, err
		}
	}
	return buildRecoverySnapshotFromTail(*header, compact, cursor, generation, continuation, schema)
}

// buildRecoverySnapshotFromTail is the compact post-validation constructor
// shared by live replay and ALL-history admission. Its caller must first prove
// the complete journal/index structure and the same-verifier statement/schema
// facts; this function owns only the bounded final attempt bodies.
func buildRecoverySnapshotFromTail(header JournalHeader, tail *admissionReplayRecoveryTail, cursor JournalCursor, generation generationIdentity, continuation recoveredContinuation, schema verifiedRecoverySchemaWitness) (*RecoverySnapshot, error) {
	if !cursor.Valid() || cursor.previousRecordDigest == nil || !sameGenerationIdentity(cursor.generation, generation) || schema.owner == nil || schema.owner != generation.owner || !sameGenerationIdentity(schema.generation, generation) || header.Validate() != nil || !sameGenerationHeader(generation, header) {
		return nil, invalidEvidence("recovery-snapshot", "owner, generation, cursor, header, or schema")
	}
	journalTail := *cursor.previousRecordDigest
	if continuation.owned != nil {
		if continuation.inheritedWithoutContext || continuation.owned.owner != generation.owner || !sameGenerationIdentity(continuation.owned.generation, generation) || continuation.owned.tailDigest != journalTail || !sameCursorIdentity(continuation.owned.cursor, cursor) {
			return nil, invalidEvidence("recovery-snapshot", "continuation injection")
		}
		if err := continuation.owned.value.Validate(); err != nil {
			return nil, err
		}
	}
	s := &RecoverySnapshot{owner: generation.owner, generation: generation, cursor: cursor.clone(), tailDigest: journalTail, state: RecoveryBrandNew, nextPermittedAction: RecoveryBeginFirstAttempt}
	if continuation.owned != nil || continuation.inheritedWithoutContext {
		s.state = RecoveryBrandNewInherited
		s.lineageContinuation = cloneOwnedRecovered(continuation.owned)
		if continuation.owned != nil {
			c := continuation.owned.value
			s.migrationID = recoveryStringPointer(c.MigrationID)
			s.attemptIndex = recoveryUint32Pointer(c.AttemptIndex)
			s.previousAttemptTerminalDigest = cloneDigestPointer(c.PreviousAttemptTerminalDigest)
			if c.StartAction == "begin_first_attempt_next_entry" {
				s.nextPermittedAction = RecoveryBeginFirstAttemptNextEntry
			} else if c.StartAction == "begin_next_attempt" {
				s.nextPermittedAction = RecoveryBeginNextAttempt
			} else {
				return nil, invalidEvidence("recovery-snapshot", "continuation action")
			}
		}
	}
	if tail == nil {
		return s, nil
	}
	if err := validateAdmissionRecoveryTail(tail); err != nil || tail.migrationID == "" || tail.attemptIndex == 0 || !admissionRecoveryTailOrderValid(tail) {
		return nil, invalidEvidence("recovery-snapshot", "compact recovery tail")
	}
	s.migrationID, s.attemptIndex = recoveryStringPointer(tail.migrationID), recoveryUint32Pointer(tail.attemptIndex)
	maxAttempts := schema.maxAttempts[tail.migrationID]
	if maxAttempts == 0 {
		return nil, invalidEvidence("recovery-snapshot", "missing max attempts")
	}
	if tail.intent != nil {
		intent := cloneProjectionValue(tail.intent.body)
		s.previousAttemptTerminalDigest = cloneDigestPointer(intent.PreviousAttemptTerminalDigest)
		s.lastStatementIntentRecordDigest = digestPointer(tail.intent.recordDigest)
		s.lastStatementIntent = recoveredValue(generation, cursor, journalTail, tail.intent.recordDigest, intent)
	}
	if tail.intermediate != nil {
		intermediate := cloneProjectionValue(tail.intermediate.body)
		s.lastIntermediateEvidenceRecordDigest = digestPointer(tail.intermediate.recordDigest)
		s.lastIntermediateStateDigest = digestPointer(intermediate.State.IntermediateStateDigest)
		s.lastIntermediateEvidence = recoveredValue(generation, cursor, journalTail, tail.intermediate.recordDigest, intermediate)
	}
	if tail.commit != nil {
		commit := cloneProjectionValue(tail.commit.body)
		s.lastCommitIntentRecordDigest = digestPointer(tail.commit.recordDigest)
		s.commitIntent = recoveredValue(generation, cursor, journalTail, tail.commit.recordDigest, commit)
	}
	if tail.terminal == nil {
		if tail.intent == nil {
			return nil, invalidEvidence("recovery-snapshot", "open recovery tail has no statement intent")
		}
		if tail.intermediate == nil {
			s.state = RecoveryDanglingStatementIntent
			s.nextPermittedAction = recoveryAbortAction(tail.attemptIndex, maxAttempts)
			return s, nil
		}
		if tail.commit == nil {
			s.state = RecoveryDanglingIntermediate
			s.nextPermittedAction = recoveryAbortAction(tail.attemptIndex, maxAttempts)
			return s, nil
		}
		s.state, s.nextPermittedAction = RecoveryDanglingCommitIntent, RecoveryReconcileCommit
		return s, nil
	}
	terminal := cloneProjectionValue(tail.terminal.body)
	s.previousAttemptTerminalDigest = cloneDigestPointer(terminal.PreviousAttemptTerminalDigest)
	s.lastTerminalDigest = digestPointer(terminal.TerminalDigest)
	s.lastTerminal = recoveredValue(generation, cursor, journalTail, tail.terminal.recordDigest, terminal)
	var resolution *AmbiguousResolutionState
	if tail.resolution != nil {
		value := cloneProjectionValue(tail.resolution.body)
		resolution = &value
		s.lastResolutionDigest = digestPointer(value.ResolutionDigest)
		s.lastResolution = recoveredValue(generation, cursor, journalTail, tail.resolution.recordDigest, value)
	}
	var intermediate *StatementIntermediateEvidence
	if tail.intermediate != nil {
		value := cloneProjectionValue(tail.intermediate.body)
		intermediate = &value
	}
	var commit *CommitIntent
	if tail.commit != nil {
		value := cloneProjectionValue(tail.commit.body)
		commit = &value
	}
	setTerminalRecoveryState(s, terminal, resolution, schema, maxAttempts, intermediate, commit)
	return s, nil
}

func admissionRecoveryTailOrderValid(tail *admissionReplayRecoveryTail) bool {
	if tail == nil {
		return true
	}
	type boundary struct {
		sequence uint64
		previous *Digest
		digest   Digest
	}
	var values []boundary
	appendBoundary := func(sequence uint64, previous *Digest, digest Digest, migration string, attempt uint32) bool {
		if migration != tail.migrationID || attempt != tail.attemptIndex || digest.Validate() != nil {
			return false
		}
		values = append(values, boundary{sequence, previous, digest})
		return true
	}
	if tail.intent != nil && !appendBoundary(tail.intent.sequence, tail.intent.previousRecordDigest, tail.intent.recordDigest, tail.intent.body.MigrationID, tail.intent.body.AttemptIndex) {
		return false
	}
	if tail.intermediate != nil && !appendBoundary(tail.intermediate.sequence, tail.intermediate.previousRecordDigest, tail.intermediate.recordDigest, tail.intermediate.body.State.MigrationID, tail.intermediate.body.State.AttemptIndex) {
		return false
	}
	if tail.commit != nil && !appendBoundary(tail.commit.sequence, tail.commit.previousRecordDigest, tail.commit.recordDigest, tail.commit.body.MigrationID, tail.commit.body.AttemptIndex) {
		return false
	}
	if tail.terminal != nil && !appendBoundary(tail.terminal.sequence, tail.terminal.previousRecordDigest, tail.terminal.recordDigest, tail.terminal.body.MigrationID, tail.terminal.body.AttemptIndex) {
		return false
	}
	if tail.resolution != nil && !appendBoundary(tail.resolution.sequence, tail.resolution.previousRecordDigest, tail.resolution.recordDigest, tail.resolution.body.MigrationID, tail.resolution.body.AttemptIndex) {
		return false
	}
	for index := 1; index < len(values); index++ {
		if values[index].sequence <= values[index-1].sequence || values[index].previous == nil || values[index].sequence == values[index-1].sequence+1 && *values[index].previous != values[index-1].digest {
			return false
		}
	}
	return len(values) != 0
}

func recoveredValue[T any](generation generationIdentity, cursor JournalCursor, tail, record Digest, value T) *OwnedRecovered[T] {
	return &OwnedRecovered[T]{generation.owner, generation, cursor.clone(), tail, record, cloneProjectionValue(value)}
}
func recoveryAbortAction(attempt, max uint32) RecoveryAction {
	if attempt < max {
		return RecoveryAppendAbortedRetryable
	}
	return RecoveryAppendAbortedTerminal
}
func setTerminalRecoveryState(s *RecoverySnapshot, terminal AttemptTerminalState, resolution *AmbiguousResolutionState, schema verifiedRecoverySchemaWitness, maxAttempts uint32, intermediate *StatementIntermediateEvidence, commit *CommitIntent) {
	s.state = RecoveryTerminal
	s.nextPermittedAction = RecoveryReturnFailure
	switch terminal.Outcome {
	case "committed", "ambiguous_reconciled_committed":
		if recoveryCommitIsFinal(terminal.MigrationID, schema, intermediate, commit) {
			s.state, s.nextPermittedAction = RecoveryCompleted, RecoveryReturnSuccess
		} else {
			s.nextPermittedAction = RecoveryBeginFirstAttemptNextEntry
		}
	case "aborted_retryable", "ambiguous_reconciled_pending":
		if terminal.AttemptIndex < maxAttempts {
			s.nextPermittedAction = RecoveryBeginNextAttempt
		}
	case "ambiguous_unresolved":
		if resolution == nil {
			s.state, s.nextPermittedAction = RecoveryAmbiguousUnresolved, RecoveryReconcileCommit
			return
		}
		switch resolution.Outcome {
		case "resolved_committed":
			if recoveryCommitIsFinal(terminal.MigrationID, schema, intermediate, commit) {
				s.state, s.nextPermittedAction = RecoveryCompleted, RecoveryReturnSuccess
			} else {
				s.nextPermittedAction = RecoveryBeginFirstAttemptNextEntry
			}
		case "resolved_pending":
			if terminal.AttemptIndex < maxAttempts {
				s.nextPermittedAction = RecoveryBeginNextAttempt
			}
		case "resolved_divergent":
			s.state = RecoveryDivergent
		default:
			s.state = RecoveryDivergent
		}
	case "ambiguous_divergent":
		s.state = RecoveryDivergent
	case "aborted_terminal":
	default:
		s.state = RecoveryDivergent
	}
}

func validateRecoverySchemaWitness(schema verifiedRecoverySchemaWitness, frames []EvidenceFrame) error {
	if len(schema.orderedMigrations) == 0 || len(schema.signedExpectedLedgerRows) != len(schema.orderedMigrations) || len(schema.durableObservedLedgerPrefix) > len(schema.orderedMigrations) || schema.signedExpectedLedgerDigest.Validate() != nil || schema.durableObservedLedgerDigest.Validate() != nil || schema.finalCatalogDigest.Validate() != nil {
		return invalidEvidence("recovery-schema", "ordered migrations or durable witnesses")
	}
	seen := map[string]bool{}
	for i, id := range schema.orderedMigrations {
		if !migrationIDPattern.MatchString(id) || seen[id] || schema.maxAttempts[id] == 0 {
			return invalidEvidence("recovery-schema", "ordered identity")
		}
		seen[id] = true
		if schema.signedExpectedLedgerRows[i].MigrationID != id {
			return invalidEvidence("recovery-schema", "signed ledger rows missing or reordered")
		}
		if i < len(schema.durableObservedLedgerPrefix) && (!canonicalEqual(schema.durableObservedLedgerPrefix[i], schema.signedExpectedLedgerRows[i]) || schema.durableObservedLedgerPrefix[i].MigrationID != id) {
			return invalidEvidence("recovery-schema", "observed ledger prefix skipped, drifted, or reordered")
		}
	}
	signedDigest, err := LedgerPrefixDigest(schema.signedExpectedLedgerRows)
	if err != nil || signedDigest != schema.signedExpectedLedgerDigest {
		return invalidEvidence("recovery-schema", "signed ledger digest")
	}
	observedDigest, err := LedgerPrefixDigest(schema.durableObservedLedgerPrefix)
	if err != nil || observedDigest != schema.durableObservedLedgerDigest {
		return invalidEvidence("recovery-schema", "observed ledger prefix digest")
	}
	committed := map[string]bool{}
	for _, frame := range frames {
		if terminal := frame.Record.AttemptTerminal; terminal != nil && stringIn(terminal.Outcome, "committed", "ambiguous_reconciled_committed") {
			committed[terminal.MigrationID] = true
		}
		if resolution := frame.Record.AmbiguousResolution; resolution != nil && resolution.Outcome == "resolved_committed" {
			committed[resolution.MigrationID] = true
		}
	}
	for _, frame := range frames {
		if c := frame.Record.CommitIntent; c != nil {
			index := migrationOrderIndex(schema.orderedMigrations, c.MigrationID)
			expectedObserved := index
			if committed[c.MigrationID] {
				expectedObserved = index + 1
			}
			if index < 0 || len(schema.durableObservedLedgerPrefix) != expectedObserved || int(c.ExpectedLedgerLength) != index+1 || c.ExpectedLedgerHead != c.MigrationID || !canonicalEqual(c.LedgerRow, schema.signedExpectedLedgerRows[index]) {
				return invalidEvidence("recovery-schema", "commit intent ledger identity")
			}
		}
	}
	return nil
}

func recoveryCommitIsFinal(migration string, schema verifiedRecoverySchemaWitness, intermediate *StatementIntermediateEvidence, commit *CommitIntent) bool {
	index := migrationOrderIndex(schema.orderedMigrations, migration)
	if index < 0 || index != len(schema.orderedMigrations)-1 || len(schema.durableObservedLedgerPrefix) != len(schema.orderedMigrations) || commit == nil || int(commit.ExpectedLedgerLength) != len(schema.orderedMigrations) || intermediate == nil || intermediate.PreledgerCatalogResult == nil {
		return false
	}
	return intermediate.PreledgerCatalogResult.Digest == schema.finalCatalogDigest
}
func migrationOrderIndex(order []string, id string) int {
	for i := range order {
		if order[i] == id {
			return i
		}
	}
	return -1
}

func recoveryStringPointer(v string) *string { return &v }
func recoveryUint32Pointer(v uint32) *uint32 { return &v }
