package migration

import (
	"context"
	"sync/atomic"
)

// EvidenceSink is the sealed runtime boundary frozen by ADR-0010. There is no
// production implementation in this slice: the filesystem implementation is
// admitted only after its platform and dependency gates have passed.
type EvidenceSink interface {
	Open(context.Context, VerifiedEvidenceRun, VerifiedRuntimeArtifact) (EvidenceSession, *RecoverySnapshot, error)
	evidenceSinkSealed()
}

type EvidenceSession interface {
	CurrentCandidate() OwnedCurrentCandidate
	ActiveGeneration() ActiveGeneration
	Journal() EvidenceJournal
	RecoverySnapshot() *RecoverySnapshot
	ReserveAndActivateSuccessor(context.Context, *VerifiedLineageSupersessionAuthority) (ActiveGeneration, *RecoverySnapshot, error)
	Close(context.Context) error
	evidenceSessionSealed()
}

type EvidenceJournal interface {
	Replay(context.Context) (JournalCursor, *RecoverySnapshot, error)
	AppendDurable(context.Context, JournalCursor, *OwnedEvidenceRecord) (AppendResult, error)
	Close(context.Context) error
	evidenceJournalSealed()
}

// NewEvidenceSink deliberately remains rejecting until the real filesystem
// slice is admitted. It cannot fall back to an in-memory or best-effort sink.
func NewEvidenceSink() (EvidenceSink, error) {
	return nil, fail(CodeProjectionNotImplemented, "evidence-sink", "production evidence filesystem is not implemented", nil)
}

type evidenceOwnerToken struct{ nonce [16]byte }

type VerifiedRuntimeArtifact struct {
	owner     *evidenceOwnerToken
	bytes     []byte
	digest    Digest
	sizeBytes uint64
}

type VerifiedContentReceipt struct {
	owner     *evidenceOwnerToken
	kind      durableContentObjectKind
	digest    Digest
	sizeBytes uint64
	identity  Digest
}

type VerifiedDecisionRecoveryReceipt struct {
	owner     *evidenceOwnerToken
	kind      durableContentObjectKind
	digest    Digest
	sizeBytes uint64
	identity  Digest
}

type durableContentObjectKind string

const (
	durableRuntimeContentObject          durableContentObjectKind = "runtime"
	durableDecisionRecoveryContentObject durableContentObjectKind = "decision_recovery"
)

// verifiedDurableContentObject is the reserved non-wire input for the future
// content-store publication result. This slice deliberately has no constructor
// or self-authenticating seal: without a separate FS-owned publication handle,
// a same-package seal would be forgeable and must not mint receipt authority.
type verifiedDurableContentObject struct {
	publicationAuthority any
}

func bindRuntimeContentReceipt(*evidenceOwnerToken, VerifiedRuntimeArtifact, verifiedDurableContentObject) (VerifiedContentReceipt, error) {
	return VerifiedContentReceipt{}, fail(CodeProjectionNotImplemented, "runtime-receipt", "durable content publication authority is not implemented", nil)
}

func bindDecisionRecoveryReceipt(*evidenceOwnerToken, VerifiedDecisionRecoveryArtifact, verifiedDurableContentObject) (VerifiedDecisionRecoveryReceipt, error) {
	return VerifiedDecisionRecoveryReceipt{}, fail(CodeProjectionNotImplemented, "decision-recovery-receipt", "durable content publication authority is not implemented", nil)
}

func validRuntimeReceipt(VerifiedContentReceipt, *evidenceOwnerToken, Digest, uint64) bool {
	// No receipt is valid until a separately owned durable publication result
	// exists. Accepting a structurally self-consistent literal here would bypass
	// the rejecting binder and recreate digest-only authority.
	return false
}
func validDecisionRecoveryReceipt(VerifiedDecisionRecoveryReceipt, *evidenceOwnerToken, Digest, uint64) bool {
	return false
}

type VerifiedEvidenceRun struct {
	owner                             *evidenceOwnerToken
	currentDecision                   OwnedVerifiedDecision
	decisionRecoveryArtifact          VerifiedDecisionRecoveryArtifact
	releaseTrustDecisionDigest        Digest
	runnerProjectionDecisionDigest    Digest
	executionLineageDigest            Digest
	outerArtifactDigest               Digest
	outerArtifactSizeBytes            uint64
	decisionRecoveryArtifactSHA256    Digest
	decisionRecoveryArtifactSizeBytes uint64
	manifestDigest                    Digest
	runnerReleaseDigest               Digest
	schemaBundleDigest                Digest
	authorityProfileDigest            Digest
	authorityBindingDigest            Digest
}

type OwnedCurrentCandidate struct {
	owner                    *evidenceOwnerToken
	verifiedRun              VerifiedEvidenceRun
	runtimeArtifact          VerifiedRuntimeArtifact
	decisionRecoveryArtifact VerifiedDecisionRecoveryArtifact
}

type activeGenerationKind string

const (
	activeGenerationCurrent          activeGenerationKind = "current"
	activeGenerationAncestorRecovery activeGenerationKind = "ancestor_recovery"
)

type generationIdentity struct {
	owner                          *evidenceOwnerToken
	executionLineageDigest         Digest
	journalIdentityDigest          Digest
	runnerProjectionDecisionDigest Digest
	schemaBundleDigest             Digest
}

type ActiveGeneration struct {
	identity                  generationIdentity
	kind                      activeGenerationKind
	journal                   EvidenceJournal
	ownedDecision             OwnedVerifiedDecision
	contentReceipt            VerifiedContentReceipt
	decisionRecoveryReceipt   VerifiedDecisionRecoveryReceipt
	recoveryExecutionBindings *VerifiedRecoveryExecutionBindings
}

type GenerationDescriptor struct {
	identity               generationIdentity
	header                 JournalHeader
	replayTailDigest       Digest
	recoveryArtifactDigest Digest
	recoveryArtifactSize   uint64
}

type JournalCursor struct {
	owner                            *evidenceOwnerToken
	generation                       generationIdentity
	segmentIndex                     uint32
	nextSequence                     uint64
	previousRecordDigest             *Digest
	lineageIndexNextSequence         uint64
	lineageIndexPreviousRecordDigest Digest
	latestCheckpointRecordDigest     *Digest
	valid                            *atomic.Bool
}

func (c JournalCursor) Valid() bool { return c.valid != nil && c.valid.Load() }
func (c JournalCursor) clone() JournalCursor {
	c.previousRecordDigest = cloneDigestPointer(c.previousRecordDigest)
	c.latestCheckpointRecordDigest = cloneDigestPointer(c.latestCheckpointRecordDigest)
	return c
}

type appendOutcome string

const (
	appendOutcomeDurable appendOutcome = "durable"
	appendOutcomeUnknown appendOutcome = "unknown"
)

type AppendResult struct {
	outcome                              appendOutcome
	durableCursor                        *JournalCursor
	candidateSequence                    uint64
	candidatePreviousRecordDigest        *Digest
	candidateRecordDigest                Digest
	rotationHeaderRecordDigest           *Digest
	rotationHeaderCheckpointRecordDigest *Digest
	candidateCheckpointRecordDigest      Digest
}

// finishAppend freezes composite journal+checkpoint semantics without doing
// I/O. Unknown invalidates the input cursor; only a fully durable composite
// result can mint the next cursor.
func finishAppend(cursor JournalCursor, record *OwnedEvidenceRecord, generation generationIdentity, outcome appendOutcome, durable *JournalCursor, candidateDigest, checkpointDigest Digest) (AppendResult, error) {
	if !sameGenerationIdentity(cursor.generation, generation) || !cursor.Valid() || candidateDigest.Validate() != nil || checkpointDigest.Validate() != nil {
		return AppendResult{}, invalidEvidence("append-result", "cursor or candidate identity")
	}
	// Reject contradictions which are knowable before append authority is
	// consumed. Once consumed below, every failure invalidates the cursor.
	switch outcome {
	case appendOutcomeDurable:
		if durable == nil || !durable.Valid() || !sameGenerationIdentity(durable.generation, generation) || durable.nextSequence <= cursor.nextSequence || durable.previousRecordDigest == nil || *durable.previousRecordDigest != candidateDigest {
			return AppendResult{}, invalidEvidence("append-result", "durable composite cursor")
		}
	case appendOutcomeUnknown:
		if durable != nil {
			return AppendResult{}, invalidEvidence("append-result", "unknown carries durable cursor")
		}
	default:
		return AppendResult{}, invalidEvidence("append-result", "unknown outcome kind")
	}
	if _, err := record.consume(generation, cursor); err != nil {
		return AppendResult{}, err
	}
	invalidate := true
	defer func() {
		if invalidate && cursor.valid != nil {
			cursor.valid.Store(false)
		}
	}()
	result := AppendResult{outcome: outcome, candidateSequence: cursor.nextSequence, candidatePreviousRecordDigest: cloneDigestPointer(cursor.previousRecordDigest), candidateRecordDigest: candidateDigest, candidateCheckpointRecordDigest: checkpointDigest}
	switch outcome {
	case appendOutcomeDurable:
		copy := durable.clone()
		result.durableCursor = &copy
	case appendOutcomeUnknown:
	}
	return result, nil
}

func (r AppendResult) Outcome() string { return string(r.outcome) }
func (r AppendResult) DurableCursor() *JournalCursor {
	if r.durableCursor == nil {
		return nil
	}
	c := r.durableCursor.clone()
	return &c
}

type ownedEvidenceWitness interface {
	evidenceWitnessSealed()
	kind() EvidenceRecordKind
	generationIdentity() generationIdentity
	cursorIdentity() JournalCursor
	prefixAndChain() ([]EvidenceFrame, verifiedEvidenceChainWitness)
}

type ownedAppendContext struct {
	generation generationIdentity
	cursor     JournalCursor
	prefix     []EvidenceFrame
	chain      verifiedEvidenceChainWitness
}

type ownedStatementIntentWitness struct {
	ownedAppendContext
	plan StatementPlan
}
type ownedIntermediateWitness struct {
	ownedAppendContext
	plan        StatementPlan
	stateDigest Digest
	priorIntent EvidenceFrame
}
type ownedCommitIntentWitness struct {
	ownedAppendContext
	priorIntermediateStateDigest Digest
	lastIntermediateRecordDigest Digest
	priorIntermediate            EvidenceFrame
}
type ownedAttemptTerminalWitness struct {
	ownedAppendContext
	terminalDigest Digest
	retry          verifiedRetryReceipt
	maxAttempts    uint32
}
type ownedAmbiguousResolutionWitness struct {
	ownedAppendContext
	unresolvedTerminalDigest Digest
	priorTerminal            EvidenceFrame
}

func (ownedStatementIntentWitness) evidenceWitnessSealed()     {}
func (ownedIntermediateWitness) evidenceWitnessSealed()        {}
func (ownedCommitIntentWitness) evidenceWitnessSealed()        {}
func (ownedAttemptTerminalWitness) evidenceWitnessSealed()     {}
func (ownedAmbiguousResolutionWitness) evidenceWitnessSealed() {}
func (w ownedStatementIntentWitness) kind() EvidenceRecordKind { return EvidenceRecordStatementIntent }
func (w ownedIntermediateWitness) kind() EvidenceRecordKind    { return EvidenceRecordIntermediate }
func (w ownedCommitIntentWitness) kind() EvidenceRecordKind    { return EvidenceRecordCommitIntent }
func (w ownedAttemptTerminalWitness) kind() EvidenceRecordKind { return EvidenceRecordAttemptTerminal }
func (w ownedAmbiguousResolutionWitness) kind() EvidenceRecordKind {
	return EvidenceRecordAmbiguousResolution
}
func (w ownedStatementIntentWitness) generationIdentity() generationIdentity     { return w.generation }
func (w ownedIntermediateWitness) generationIdentity() generationIdentity        { return w.generation }
func (w ownedCommitIntentWitness) generationIdentity() generationIdentity        { return w.generation }
func (w ownedAttemptTerminalWitness) generationIdentity() generationIdentity     { return w.generation }
func (w ownedAmbiguousResolutionWitness) generationIdentity() generationIdentity { return w.generation }
func (w ownedStatementIntentWitness) cursorIdentity() JournalCursor              { return w.cursor }
func (w ownedIntermediateWitness) cursorIdentity() JournalCursor                 { return w.cursor }
func (w ownedCommitIntentWitness) cursorIdentity() JournalCursor                 { return w.cursor }
func (w ownedAttemptTerminalWitness) cursorIdentity() JournalCursor              { return w.cursor }
func (w ownedAmbiguousResolutionWitness) cursorIdentity() JournalCursor          { return w.cursor }
func (w ownedStatementIntentWitness) prefixAndChain() ([]EvidenceFrame, verifiedEvidenceChainWitness) {
	return w.prefix, w.chain
}
func (w ownedIntermediateWitness) prefixAndChain() ([]EvidenceFrame, verifiedEvidenceChainWitness) {
	return w.prefix, w.chain
}
func (w ownedCommitIntentWitness) prefixAndChain() ([]EvidenceFrame, verifiedEvidenceChainWitness) {
	return w.prefix, w.chain
}
func (w ownedAttemptTerminalWitness) prefixAndChain() ([]EvidenceFrame, verifiedEvidenceChainWitness) {
	return w.prefix, w.chain
}
func (w ownedAmbiguousResolutionWitness) prefixAndChain() ([]EvidenceFrame, verifiedEvidenceChainWitness) {
	return w.prefix, w.chain
}

// OwnedEvidenceRecord is a sealed one-shot append authority. Its wire body is
// clone-owned, while its witness and identity are never serialized.
type OwnedEvidenceRecord struct {
	wire       EvidenceRecord
	witness    ownedEvidenceWitness
	generation generationIdentity
	cursor     JournalCursor
	consumed   *atomic.Bool
}

func (r *OwnedEvidenceRecord) consume(generation generationIdentity, cursor JournalCursor) (EvidenceRecord, error) {
	if r == nil || r.consumed == nil || r.witness == nil || !sameGenerationIdentity(r.generation, generation) || !sameGenerationIdentity(r.witness.generationIdentity(), generation) || !sameCursorIdentity(r.cursor, cursor) || !sameCursorIdentity(r.witness.cursorIdentity(), cursor) || !cursor.Valid() {
		return EvidenceRecord{}, invalidEvidence("owned-record", "generation or cursor mismatch")
	}
	if !r.consumed.CompareAndSwap(false, true) {
		return EvidenceRecord{}, invalidEvidence("owned-record", "append authority already consumed")
	}
	return cloneEvidenceRecord(r.wire), nil
}

func sameGenerationIdentity(a, b generationIdentity) bool {
	return a.owner != nil && a.owner == b.owner && a.executionLineageDigest == b.executionLineageDigest && a.journalIdentityDigest == b.journalIdentityDigest && a.runnerProjectionDecisionDigest == b.runnerProjectionDecisionDigest && a.schemaBundleDigest == b.schemaBundleDigest
}

func sameCursorIdentity(a, b JournalCursor) bool {
	return a.owner != nil && a.owner == b.owner && sameGenerationIdentity(a.generation, b.generation) && a.segmentIndex == b.segmentIndex && a.nextSequence == b.nextSequence && equalDigestPointer(a.previousRecordDigest, b.previousRecordDigest) && a.lineageIndexNextSequence == b.lineageIndexNextSequence && a.lineageIndexPreviousRecordDigest == b.lineageIndexPreviousRecordDigest && equalDigestPointer(a.latestCheckpointRecordDigest, b.latestCheckpointRecordDigest) && a.valid != nil && a.valid == b.valid
}

func bindOwnedEvidenceRecord(record EvidenceRecord, witness ownedEvidenceWitness) (*OwnedEvidenceRecord, error) {
	if _, err := record.branch(); err != nil {
		return nil, err
	}
	if witness == nil || !witness.cursorIdentity().Valid() || !sameGenerationIdentity(witness.generationIdentity(), witness.cursorIdentity().generation) {
		return nil, invalidEvidence("owned-record", "unowned witness")
	}
	if !evidenceKindMatches(witness.kind(), record) || witness.kind() == EvidenceRecordHeader {
		return nil, invalidEvidence("owned-record", "record kind or header authority")
	}
	if err := validateEvidenceRecord(record); err != nil {
		return nil, err
	}
	if err := validateRuntimeWitness(record, witness); err != nil {
		return nil, err
	}
	prefix, chain := witness.prefixAndChain()
	cursor := witness.cursorIdentity()
	if len(prefix) == 0 || prefix[len(prefix)-1].RecordDigest != *cursor.previousRecordDigest || prefix[len(prefix)-1].Sequence+1 != cursor.nextSequence {
		return nil, invalidEvidence("owned-record", "candidate prefix does not match cursor")
	}
	candidate := EvidenceFrame{FormatVersion: EvidenceFrameFormat, Sequence: cursor.nextSequence, PreviousRecordDigest: cloneDigestPointer(cursor.previousRecordDigest), RecordKind: witness.kind(), Record: cloneEvidenceRecord(record)}
	candidate.RecordDigest, _ = candidate.ComputeDigest()
	frames := append(cloneProjectionValue(prefix), candidate)
	if err := validateEvidenceChainWithWitness(frames, chain); err != nil {
		return nil, err
	}
	cursor = witness.cursorIdentity().clone()
	return &OwnedEvidenceRecord{wire: cloneEvidenceRecord(record), witness: witness, generation: witness.generationIdentity(), cursor: cursor, consumed: &atomic.Bool{}}, nil
}

func cloneEvidenceRecord(r EvidenceRecord) EvidenceRecord {
	var cloned EvidenceRecord
	if r.Header != nil {
		v := cloneProjectionValue(*r.Header)
		cloned.Header = &v
	}
	if r.StatementIntent != nil {
		v := cloneProjectionValue(*r.StatementIntent)
		cloned.StatementIntent = &v
	}
	if r.Intermediate != nil {
		v := cloneProjectionValue(*r.Intermediate)
		cloned.Intermediate = &v
	}
	if r.CommitIntent != nil {
		v := cloneProjectionValue(*r.CommitIntent)
		cloned.CommitIntent = &v
	}
	if r.AttemptTerminal != nil {
		v := cloneProjectionValue(*r.AttemptTerminal)
		cloned.AttemptTerminal = &v
	}
	if r.AmbiguousResolution != nil {
		v := cloneProjectionValue(*r.AmbiguousResolution)
		cloned.AmbiguousResolution = &v
	}
	return cloned
}

func validateRuntimeWitness(record EvidenceRecord, witness ownedEvidenceWitness) error {
	switch w := witness.(type) {
	case ownedStatementIntentWitness:
		if err := w.plan.validateExact(); err != nil || record.StatementIntent == nil || !planMatchesIntent(exactStatementWitnessFromPlan(w.plan, record.StatementIntent.AttemptIndex), *record.StatementIntent) {
			return invalidEvidence("owned-record", "statement plan mismatch")
		}
	case ownedIntermediateWitness:
		if err := w.plan.validateExact(); err != nil || record.Intermediate == nil || w.priorIntent.Record.StatementIntent == nil || w.priorIntent.RecordDigest != *w.cursor.previousRecordDigest || record.Intermediate.State.StatementIndex != w.plan.StatementIndex || record.Intermediate.State.StatementSHA256 != w.plan.StatementSHA256 || record.Intermediate.State.IntermediateStateDigest != w.stateDigest {
			return invalidEvidence("owned-record", "intermediate witness mismatch")
		}
	case ownedCommitIntentWitness:
		if record.CommitIntent == nil || w.priorIntermediate.Record.Intermediate == nil || w.priorIntermediate.RecordDigest != w.lastIntermediateRecordDigest || w.priorIntermediate.RecordDigest != *w.cursor.previousRecordDigest || record.CommitIntent.LastIntermediateStateDigest != w.priorIntermediateStateDigest || w.priorIntermediate.Record.Intermediate.State.IntermediateStateDigest != w.priorIntermediateStateDigest {
			return invalidEvidence("owned-record", "commit witness mismatch")
		}
	case ownedAttemptTerminalWitness:
		if record.AttemptTerminal == nil || w.maxAttempts == 0 || record.AttemptTerminal.Validate(w.maxAttempts) != nil || record.AttemptTerminal.TerminalDigest != w.terminalDigest || record.AttemptTerminal.RetryProof != nil && w.retry == nil || record.AttemptTerminal.RetryProof == nil && w.retry != nil {
			return invalidEvidence("owned-record", "terminal witness mismatch")
		}
		if record.AttemptTerminal.RetryProof != nil {
			prefix := w.prefix
			state := &evidenceAttemptState{}
			for i := range prefix {
				f := &prefix[i]
				switch f.RecordKind {
				case EvidenceRecordStatementIntent:
					state.lastIntent = f
				case EvidenceRecordIntermediate:
					state.lastIntermediate = f
				case EvidenceRecordCommitIntent:
					state.commit = f
				}
			}
			header := prefix[0].Record.Header
			if header == nil || w.retry.validateRetryProof(*record.AttemptTerminal.RetryProof, *record.AttemptTerminal, state, *header) != nil {
				return invalidEvidence("owned-record", "sealed retry receipt mismatch")
			}
		}
	case ownedAmbiguousResolutionWitness:
		if record.AmbiguousResolution == nil || w.priorTerminal.Record.AttemptTerminal == nil || w.priorTerminal.RecordDigest != *w.cursor.previousRecordDigest || record.AmbiguousResolution.UnresolvedTerminalDigest != w.unresolvedTerminalDigest || w.priorTerminal.Record.AttemptTerminal.TerminalDigest != w.unresolvedTerminalDigest || w.priorTerminal.Record.AttemptTerminal.Outcome != "ambiguous_unresolved" || record.AmbiguousResolution.MigrationID != w.priorTerminal.Record.AttemptTerminal.MigrationID || record.AmbiguousResolution.AttemptIndex != w.priorTerminal.Record.AttemptTerminal.AttemptIndex || record.AmbiguousResolution.StableErrorCode != ErrorCode(*w.priorTerminal.Record.AttemptTerminal.StableErrorCode) {
			return invalidEvidence("owned-record", "resolution witness mismatch")
		}
	default:
		return invalidEvidence("owned-record", "unknown witness")
	}
	return nil
}

func exactStatementWitnessFromPlan(plan StatementPlan, attempt uint32) exactStatementEvidenceWitness {
	classification, _ := canonicalContractKey(plan.Classification)
	return exactStatementEvidenceWitness{plan.MigrationID, attempt, plan.StatementIndex, plan.SQLArtifactSHA256, plan.SQLArtifactSizeBytes, plan.StartOffset, plan.EndOffset, plan.StatementSHA256, classification, plan.ExpectedTransitionDigest}
}

// Header construction has a distinct sealed type and never enters the caller
// OwnedEvidenceRecord union.
type ownedActivationHeader struct {
	header     JournalHeader
	generation generationIdentity
	reserved   GenerationReserved
}
type ownedRotationHeader struct {
	header     JournalHeader
	generation generationIdentity
	cursor     JournalCursor
}

func bindActivationHeader(generation generationIdentity, reserved GenerationReserved, runtime VerifiedContentReceipt, recovery VerifiedDecisionRecoveryReceipt) (ownedActivationHeader, error) {
	h := reserved.PlannedSegment0Header
	if err := reserved.Validate(); err != nil || !sameGenerationHeader(generation, h) || !validRuntimeReceipt(runtime, generation.owner, h.OuterArtifactDigest, h.OuterArtifactSizeBytes) || !validDecisionRecoveryReceipt(recovery, generation.owner, h.DecisionRecoveryArtifactSHA256, h.DecisionRecoveryArtifactSizeBytes) {
		return ownedActivationHeader{}, invalidEvidence("activation-header", "reservation, generation, or receipt mismatch")
	}
	return ownedActivationHeader{cloneProjectionValue(h), generation, cloneProjectionValue(reserved)}, nil
}

func sameGenerationHeader(g generationIdentity, h JournalHeader) bool {
	return g.owner != nil && g.executionLineageDigest == h.ExecutionLineageDigest && g.journalIdentityDigest == h.JournalIdentityDigest && g.runnerProjectionDecisionDigest == h.RunnerProjectionDecisionDigest && g.schemaBundleDigest == h.SchemaBundleDigest
}
