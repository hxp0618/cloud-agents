package migration

// evidenceStructuralReplay contains only compact, ordinary facts. It is not a
// witness and cannot mint admission, recovery, or execution authority.
type evidenceStructuralReplay struct {
	firstFrame    EvidenceFrame
	header        JournalHeader
	records       uint64
	segments      uint32
	physicalBytes uint64
	tailDigest    Digest
	summary       evidenceJournalSummary
}

type evidenceStructuralObserver interface {
	observeHeader(JournalHeader) error
	observeIntent(StatementIntent) error
	observeTerminal(AttemptTerminalState, evidenceCompactAttemptState, JournalHeader) error
}

type evidenceCheckpointRequirement struct {
	nextSequence  uint64
	tailDigest    Digest
	summaryDigest Digest
}

// evidenceStructuralContinuationSeed is ordinary, validated lineage data. It
// lets structural replay consume the first durable intent of a successor
// generation without granting recovery or execution authority.
type evidenceStructuralContinuationSeed struct{ context LineageContinuationContext }

func newEvidenceStructuralContinuationSeed(value *LineageContinuationContext) (*evidenceStructuralContinuationSeed, error) {
	if value == nil {
		return nil, nil
	}
	if err := value.Validate(); err != nil {
		return nil, err
	}
	return &evidenceStructuralContinuationSeed{cloneProjectionValue(*value)}, nil
}

type evidenceMigrationStructuralState struct {
	attempt uint32
	state   evidenceCompactAttemptState
	summary evidenceJournalSummary
}

// Only bounded predecessor facts survive a consume call. No wire frame or
// variable-sized projection/control-plane payload is retained per migration.
type evidenceCompactAttemptState struct {
	lastIntent       *evidenceCompactIntent
	lastIntermediate *evidenceCompactIntermediate
	commit           *evidenceCompactCommit
	terminal         *evidenceCompactTerminal
	resolution       *evidenceCompactResolution
}

type evidenceCompactIntent struct {
	recordDigest          Digest
	statementIndex        uint32
	statementSHA256       Digest
	authorityBeforeDigest Digest
	catalogBeforeDigest   Digest
}

type evidenceCompactIntermediate struct {
	sequence                uint64
	recordDigest            Digest
	statementIndex          uint32
	intermediateStateDigest Digest
	preledgerCatalogDigest  *Digest
}

type evidenceCompactCommit struct {
	sequence     uint64
	recordDigest Digest
}
type evidenceCompactTerminal struct {
	recordDigest, terminalDigest Digest
	attempt                      uint32
	outcome                      string
	stableErrorCode              *string
}
type evidenceCompactResolution struct {
	outcome                  string
	unresolvedTerminalDigest Digest
}

// evidenceStructuralAccumulator consumes exactly one decoded physical segment
// at a time. Callers may discard a segment immediately after endSegment.
type evidenceStructuralAccumulator struct {
	requirements []evidenceCheckpointRequirement
	requirement  int
	observer     evidenceStructuralObserver
	headerErr    error
	intentErr    error
	terminalErr  error

	started                bool
	inSegment              bool
	segmentRecords         uint64
	segmentBytes           uint64
	physicalBytes          uint64
	records                uint64
	segments               uint32
	previous               *Digest
	header                 *JournalHeader
	firstHeader            *JournalHeader
	firstFrame             *EvidenceFrame
	previousSegmentFinal   *Digest
	migrations             map[string]*evidenceMigrationStructuralState
	summary                evidenceJournalSummary
	structuralSeed         *evidenceStructuralContinuationSeed
	structuralSeedConsumed bool
}

// evidenceJournalStructuralStream is the opaque streaming edge exposed to
// inventory replay. Continuation seeds remain owned by lineageStructuralPlan;
// callers can only feed the resulting journal stream.
type evidenceJournalStructuralStream struct {
	accumulator *evidenceStructuralAccumulator
}

func openEvidenceJournalStructuralStream(plan *lineageStructuralPlan, id Digest, observer evidenceStructuralObserver) (*evidenceJournalStructuralStream, bool) {
	if plan != nil {
		if accumulator, ok := plan.newJournalAccumulator(id, observer); ok {
			return &evidenceJournalStructuralStream{accumulator: accumulator}, true
		}
	}
	return &evidenceJournalStructuralStream{accumulator: newEvidenceStructuralAccumulator(nil, observer)}, false
}

func (s *evidenceJournalStructuralStream) beginSegment() error {
	if s == nil || s.accumulator == nil {
		return invalidEvidence("chain", "journal stream")
	}
	return s.accumulator.beginSegment()
}

func (s *evidenceJournalStructuralStream) consumeFrame(frame EvidenceFrame, framedBytes uint64) error {
	if s == nil || s.accumulator == nil {
		return invalidEvidence("chain", "journal stream")
	}
	return s.accumulator.consumeFrame(frame, framedBytes)
}

func (s *evidenceJournalStructuralStream) endSegment() error {
	if s == nil || s.accumulator == nil {
		return invalidEvidence("chain", "journal stream")
	}
	return s.accumulator.endSegment()
}

func (s *evidenceJournalStructuralStream) finish() (*evidenceStructuralReplay, error) {
	if s == nil || s.accumulator == nil {
		return nil, invalidEvidence("chain", "journal stream")
	}
	return s.accumulator.finish()
}

func newEvidenceStructuralAccumulator(requirements []evidenceCheckpointRequirement, observer evidenceStructuralObserver) *evidenceStructuralAccumulator {
	return &evidenceStructuralAccumulator{
		requirements: requirements,
		observer:     observer,
		migrations:   make(map[string]*evidenceMigrationStructuralState),
		summary:      evidenceJournalSummary{recoveryState: "brand_new"},
	}
}

func (a *evidenceStructuralAccumulator) beginSegment() error {
	if a.inSegment || a.segments >= maxEvidenceReservedSegments {
		return invalidEvidence("chain", "physical segment count")
	}
	a.inSegment = true
	a.segmentRecords, a.segmentBytes = 0, 0
	return nil
}

func (a *evidenceStructuralAccumulator) consumeFrame(frame EvidenceFrame, framedBytes uint64) error {
	if !a.inSegment {
		return invalidEvidence("chain", "physical segment state")
	}
	canonical, err := canonicalContractKey(frame)
	if err != nil {
		return err
	}
	exact := uint64(len(canonical)) + 8
	if framedBytes != exact {
		return invalidEvidence("chain", "physical framed bytes")
	}
	if a.segmentBytes > ^uint64(0)-framedBytes || a.physicalBytes > ^uint64(0)-framedBytes {
		return invalidEvidence("chain", "physical byte overflow")
	}
	if a.segmentRecords == 0 {
		if frame.RecordKind != EvidenceRecordHeader {
			return invalidEvidence("chain", "physical segment header")
		}
	} else if frame.RecordKind == EvidenceRecordHeader {
		return invalidEvidence("chain", "middle physical segment header")
	}
	if err := frame.Validate(); err != nil {
		return err
	}
	if frame.Sequence != a.records || !equalDigestPointer(frame.PreviousRecordDigest, a.previous) {
		return invalidEvidence("chain", "sequence or previous")
	}

	if frame.RecordKind == EvidenceRecordHeader {
		h := frame.Record.Header
		if a.records == 0 {
			if h.SegmentIndex != 0 {
				return invalidEvidence("chain", "initial segment")
			}
			ownedHeader := cloneProjectionValue(*h)
			a.firstHeader = &ownedHeader
			ownedFrame := cloneProjectionValue(frame)
			a.firstFrame = &ownedFrame
		} else if a.header == nil || a.firstHeader == nil || h.SegmentIndex != a.header.SegmentIndex+1 || h.PreviousSegmentRecordDigest == nil || a.previousSegmentFinal == nil || *h.PreviousSegmentRecordDigest != *a.previousSegmentFinal || !sameJournalGeneration(*a.firstHeader, *h) {
			return invalidEvidence("chain", "segment header")
		}
		owned := cloneProjectionValue(*h)
		a.header = &owned
		a.observeHeader(owned)
	} else {
		if a.header == nil {
			return invalidEvidence("chain", "record before header")
		}
		if err := recordMatchesHeader(frame.Record, *a.header); err != nil {
			return err
		}
		if err := a.consumeRecord(frame); err != nil {
			return err
		}
	}

	a.segmentRecords++
	a.segmentBytes += framedBytes
	a.physicalBytes += framedBytes
	a.records++
	digest := frame.RecordDigest
	a.previous = &digest
	a.previousSegmentFinal = &digest
	a.started = true
	if a.requirement < len(a.requirements) && a.records == a.requirements[a.requirement].nextSequence {
		requirement := a.requirements[a.requirement]
		if digest != requirement.tailDigest || evidenceJournalSummaryDigest(a.summary) != requirement.summaryDigest {
			return invalidEvidence("lineage-chain", "checkpoint summary")
		}
		a.requirement++
	}
	return nil
}

func (a *evidenceStructuralAccumulator) endSegment() error {
	if !a.inSegment || a.segmentRecords == 0 {
		return invalidEvidence("chain", "physical segment header")
	}
	if validateEvidenceSegmentUsage(a.segmentRecords, a.segmentBytes) != nil {
		return invalidEvidence("chain", "physical segment limit")
	}
	a.inSegment = false
	a.segments++
	return nil
}

func (a *evidenceStructuralAccumulator) finish() (*evidenceStructuralReplay, error) {
	if a.inSegment || !a.started || a.firstHeader == nil || a.firstFrame == nil || a.segments == 0 {
		return nil, invalidEvidence("chain", "empty")
	}
	if a.requirement != len(a.requirements) {
		return nil, invalidEvidence("lineage-chain", "checkpoint tail")
	}
	if a.records > a.firstHeader.ReservedRecords || a.segments > a.firstHeader.ReservedSegments || a.physicalBytes > a.firstHeader.ReservedBytes {
		return nil, invalidEvidence("chain", "observed journal exceeds reservation")
	}
	// Header-only successor journals are a legal reserved/activated state; the
	// seed remains unconsumed until the first non-header record is durable.
	if a.headerErr != nil {
		return nil, a.headerErr
	}
	if a.intentErr != nil {
		return nil, a.intentErr
	}
	if a.terminalErr != nil {
		return nil, a.terminalErr
	}
	return &evidenceStructuralReplay{
		firstFrame:    cloneProjectionValue(*a.firstFrame),
		header:        cloneProjectionValue(*a.firstHeader),
		records:       a.records,
		segments:      a.segments,
		physicalBytes: a.physicalBytes,
		tailDigest:    *a.previous,
		summary:       cloneEvidenceJournalSummary(a.summary),
	}, nil
}

func (a *evidenceStructuralAccumulator) observeHeader(header JournalHeader) {
	if a.observer != nil && a.headerErr == nil {
		a.headerErr = a.observer.observeHeader(cloneProjectionValue(header))
	}
}

func (a *evidenceStructuralAccumulator) observeIntent(intent StatementIntent) {
	if a.observer != nil && a.intentErr == nil {
		a.intentErr = a.observer.observeIntent(cloneProjectionValue(intent))
	}
}

func (a *evidenceStructuralAccumulator) observeTerminal(terminal AttemptTerminalState, state evidenceCompactAttemptState) {
	if a.observer != nil && a.terminalErr == nil {
		a.terminalErr = a.observer.observeTerminal(cloneProjectionValue(terminal), state, cloneProjectionValue(*a.header))
	}
}

func (a *evidenceStructuralAccumulator) consumeRecord(frame EvidenceFrame) error {
	key, migration, attempt, err := evidenceAttemptIdentity(frame.Record)
	if err != nil {
		return err
	}
	_ = key
	if a.structuralSeed != nil && !a.structuralSeedConsumed {
		if frame.RecordKind != EvidenceRecordStatementIntent || frame.Record.StatementIntent == nil {
			return invalidEvidence("chain", "continuation first record")
		}
		intent := frame.Record.StatementIntent
		seed := a.structuralSeed.context
		if intent.StatementIndex != 0 || intent.MigrationID != seed.MigrationID || intent.AttemptIndex != seed.AttemptIndex || !equalDigestPointer(intent.PreviousAttemptTerminalDigest, seed.PreviousAttemptTerminalDigest) {
			return invalidEvidence("chain", "continuation intent")
		}
		a.structuralSeedConsumed = true
	}
	migrationState := a.migrations[migration]
	if migrationState == nil {
		migrationState = &evidenceMigrationStructuralState{}
		a.migrations[migration] = migrationState
	}
	if migrationState.attempt != 0 && attempt != migrationState.attempt {
		if frame.RecordKind != EvidenceRecordStatementIntent || frame.Record.StatementIntent.StatementIndex != 0 || attempt != migrationState.attempt+1 || migrationState.state.terminal == nil || !compactPredecessorAllowsNextAttempt(*migrationState.state.terminal, migrationState.state.resolution) {
			return invalidEvidence("chain", "attempt predecessor")
		}
		if frame.Record.StatementIntent.PreviousAttemptTerminalDigest == nil || *frame.Record.StatementIntent.PreviousAttemptTerminalDigest != migrationState.state.terminal.terminalDigest {
			return invalidEvidence("chain", "attempt predecessor")
		}
		predecessor := migrationState.state.terminal.terminalDigest
		migrationState.state = evidenceCompactAttemptState{}
		migrationState.summary = evidenceJournalSummary{previousAttemptTerminalDigest: &predecessor}
		migrationState.attempt = attempt
	}
	if migrationState.attempt == 0 {
		migrationState.attempt = attempt
		if a.structuralSeedConsumed && a.structuralSeed != nil && migration == a.structuralSeed.context.MigrationID && attempt == a.structuralSeed.context.AttemptIndex {
			migrationState.summary.previousAttemptTerminalDigest = cloneDigestPointer(a.structuralSeed.context.PreviousAttemptTerminalDigest)
		}
	}
	if attempt != migrationState.attempt {
		return invalidEvidence("chain", "attempt predecessor")
	}
	state := &migrationState.state
	switch frame.RecordKind {
	case EvidenceRecordStatementIntent:
		intent := frame.Record.StatementIntent
		if state.terminal != nil || state.commit != nil {
			return invalidEvidence("chain", "intent after closed boundary")
		}
		if state.lastIntent == nil {
			if intent.StatementIndex != 0 {
				return invalidEvidence("chain", "first statement index")
			}
			if attempt == 1 {
				if intent.PreviousAttemptTerminalDigest != nil {
					return invalidEvidence("chain", "first attempt")
				}
			} else if migrationState.summary.previousAttemptTerminalDigest == nil || intent.PreviousAttemptTerminalDigest == nil || *intent.PreviousAttemptTerminalDigest != *migrationState.summary.previousAttemptTerminalDigest {
				return invalidEvidence("chain", "attempt predecessor")
			}
		} else if intent.StatementIndex != state.lastIntent.statementIndex+1 {
			return invalidEvidence("chain", "statement index gap")
		} else if state.lastIntermediate == nil || intent.PreviousIntermediateStateDigest == nil || *intent.PreviousIntermediateStateDigest != state.lastIntermediate.intermediateStateDigest {
			return invalidEvidence("chain", "statement predecessor")
		}
		state.lastIntent = &evidenceCompactIntent{frame.RecordDigest, intent.StatementIndex, intent.StatementSHA256, projectionResultCompactDigest(intent.AuthorityBeforeResult), projectionResultCompactDigest(intent.CatalogBeforeResult)}
		a.observeIntent(*intent)
	case EvidenceRecordIntermediate:
		intermediate := frame.Record.Intermediate
		if state.lastIntent == nil || state.commit != nil || state.terminal != nil || state.lastIntermediate != nil && state.lastIntermediate.statementIndex == state.lastIntent.statementIndex {
			return invalidEvidence("chain", "intermediate position")
		}
		intent := state.lastIntent
		if intermediate.State.StatementIndex != intent.statementIndex || intermediate.State.StatementSHA256 != intent.statementSHA256 || projectionResultCompactDigest(intermediate.AuthorityBeforeResult) != intent.authorityBeforeDigest || projectionResultCompactDigest(intermediate.CatalogBeforeResult) != intent.catalogBeforeDigest {
			return invalidEvidence("chain", "intermediate intent")
		}
		var catalog *Digest
		if intermediate.PreledgerCatalogResult != nil {
			digest := intermediate.PreledgerCatalogResult.Digest
			catalog = &digest
		}
		state.lastIntermediate = &evidenceCompactIntermediate{frame.Sequence, frame.RecordDigest, intermediate.State.StatementIndex, intermediate.State.IntermediateStateDigest, catalog}
	case EvidenceRecordCommitIntent:
		commit := frame.Record.CommitIntent
		if state.commit != nil || state.terminal != nil || state.lastIntermediate == nil {
			return invalidEvidence("chain", "commit position")
		}
		if commit.LastIntermediateStateDigest != state.lastIntermediate.intermediateStateDigest {
			return invalidEvidence("chain", "commit intermediate")
		}
		state.commit = &evidenceCompactCommit{frame.Sequence, frame.RecordDigest}
	case EvidenceRecordAttemptTerminal:
		terminal := frame.Record.AttemptTerminal
		if state.terminal != nil || state.resolution != nil {
			return invalidEvidence("chain", "second terminal")
		}
		if err := validateStructuralTerminalCompact(*terminal, frame, state); err != nil {
			return err
		}
		state.terminal = &evidenceCompactTerminal{frame.RecordDigest, terminal.TerminalDigest, terminal.AttemptIndex, terminal.Outcome, cloneStringPointer(terminal.StableErrorCode)}
		a.observeTerminal(*terminal, *state)
	case EvidenceRecordAmbiguousResolution:
		resolution := frame.Record.AmbiguousResolution
		if state.terminal == nil || state.resolution != nil || a.records == 0 || a.previous == nil || state.terminal.recordDigest != *a.previous {
			return invalidEvidence("chain", "resolution adjacency")
		}
		terminal := state.terminal
		if terminal.outcome != "ambiguous_unresolved" || resolution.UnresolvedTerminalDigest != terminal.terminalDigest || terminal.stableErrorCode == nil || string(resolution.StableErrorCode) != *terminal.stableErrorCode {
			return invalidEvidence("chain", "resolution terminal")
		}
		state.resolution = &evidenceCompactResolution{resolution.Outcome, resolution.UnresolvedTerminalDigest}
	}
	migrationState.summary = summarizeAttemptState(frame, state, migrationState.summary)
	a.summary = cloneEvidenceJournalSummary(migrationState.summary)
	if frame.RecordKind == EvidenceRecordAttemptTerminal || frame.RecordKind == EvidenceRecordAmbiguousResolution {
		a.compactClosedAttempt(migrationState)
	}
	return nil
}

func (a *evidenceStructuralAccumulator) compactClosedAttempt(state *evidenceMigrationStructuralState) {
	state.state.lastIntent = nil
	state.state.lastIntermediate = nil
	state.state.commit = nil
}

func summarizeAttemptState(tail EvidenceFrame, state *evidenceCompactAttemptState, prior evidenceJournalSummary) evidenceJournalSummary {
	summary := evidenceJournalSummary{recoveryState: "brand_new"}
	if state.lastIntent != nil {
		summary.lastStatementIntentRecordDigest = digestPointer(state.lastIntent.recordDigest)
	}
	if state.lastIntermediate != nil {
		summary.lastIntermediateEvidenceRecordDigest = digestPointer(state.lastIntermediate.recordDigest)
		summary.lastIntermediateStateDigest = digestPointer(state.lastIntermediate.intermediateStateDigest)
	}
	if state.commit != nil {
		summary.lastCommitIntentRecordDigest = digestPointer(state.commit.recordDigest)
	}
	// Closed attempts have discarded their large predecessor frames. Preserve
	// their already-computed compact digest facts while resolving ambiguity.
	if tail.RecordKind == EvidenceRecordAmbiguousResolution {
		summary.lastStatementIntentRecordDigest = cloneDigestPointer(prior.lastStatementIntentRecordDigest)
		summary.lastIntermediateEvidenceRecordDigest = cloneDigestPointer(prior.lastIntermediateEvidenceRecordDigest)
		summary.lastIntermediateStateDigest = cloneDigestPointer(prior.lastIntermediateStateDigest)
		summary.lastCommitIntentRecordDigest = cloneDigestPointer(prior.lastCommitIntentRecordDigest)
	}
	migration, attempt := structuralRecordAttempt(tail.Record)
	summary.migrationID, summary.attemptIndex = &migration, &attempt
	switch tail.RecordKind {
	case EvidenceRecordAmbiguousResolution:
		resolution := tail.Record.AmbiguousResolution
		summary.lastResolutionDigest = digestPointer(resolution.ResolutionDigest)
		summary.previousAttemptTerminalDigest = digestPointer(resolution.UnresolvedTerminalDigest)
		switch resolution.Outcome {
		case "resolved_committed":
			summary.recoveryState = "completed"
		case "resolved_divergent":
			summary.recoveryState = "divergent"
		default:
			summary.recoveryState = "terminal"
		}
	case EvidenceRecordAttemptTerminal:
		terminal := tail.Record.AttemptTerminal
		summary.lastTerminalDigest = digestPointer(terminal.TerminalDigest)
		summary.previousAttemptTerminalDigest = cloneDigestPointer(terminal.PreviousAttemptTerminalDigest)
		switch terminal.Outcome {
		case "committed", "ambiguous_reconciled_committed":
			summary.recoveryState = "completed"
		case "ambiguous_divergent":
			summary.recoveryState = "divergent"
		case "ambiguous_unresolved":
			summary.recoveryState = "ambiguous_unresolved"
		default:
			summary.recoveryState = "terminal"
		}
	case EvidenceRecordCommitIntent:
		summary.previousAttemptTerminalDigest = cloneDigestPointer(tail.Record.CommitIntent.PreviousAttemptTerminalDigest)
		summary.recoveryState = "dangling_commit_intent"
	case EvidenceRecordIntermediate:
		summary.previousAttemptTerminalDigest = cloneDigestPointer(tail.Record.Intermediate.State.PreviousAttemptTerminalDigest)
		summary.recoveryState = "dangling_intermediate"
	case EvidenceRecordStatementIntent:
		summary.previousAttemptTerminalDigest = cloneDigestPointer(tail.Record.StatementIntent.PreviousAttemptTerminalDigest)
		summary.recoveryState = "dangling_statement_intent"
	}
	return summary
}

func cloneEvidenceJournalSummary(value evidenceJournalSummary) evidenceJournalSummary {
	value.migrationID = cloneStringPointer(value.migrationID)
	value.attemptIndex = cloneUint32Pointer(value.attemptIndex)
	value.lastStatementIntentRecordDigest = cloneDigestPointer(value.lastStatementIntentRecordDigest)
	value.lastIntermediateEvidenceRecordDigest = cloneDigestPointer(value.lastIntermediateEvidenceRecordDigest)
	value.lastCommitIntentRecordDigest = cloneDigestPointer(value.lastCommitIntentRecordDigest)
	value.lastTerminalDigest = cloneDigestPointer(value.lastTerminalDigest)
	value.lastResolutionDigest = cloneDigestPointer(value.lastResolutionDigest)
	value.previousAttemptTerminalDigest = cloneDigestPointer(value.previousAttemptTerminalDigest)
	value.lastIntermediateStateDigest = cloneDigestPointer(value.lastIntermediateStateDigest)
	return value
}

type evidenceJournalSummaryRecord struct {
	RecoveryState                        string
	MigrationID                          *string
	AttemptIndex                         *uint32
	LastStatementIntentRecordDigest      *Digest
	LastIntermediateEvidenceRecordDigest *Digest
	LastCommitIntentRecordDigest         *Digest
	LastTerminalDigest                   *Digest
	LastResolutionDigest                 *Digest
	PreviousAttemptTerminalDigest        *Digest
	LastIntermediateStateDigest          *Digest
}

func evidenceJournalSummaryDigest(value evidenceJournalSummary) Digest {
	record := evidenceJournalSummaryRecord{value.recoveryState, value.migrationID, value.attemptIndex, value.lastStatementIntentRecordDigest, value.lastIntermediateEvidenceRecordDigest, value.lastCommitIntentRecordDigest, value.lastTerminalDigest, value.lastResolutionDigest, value.previousAttemptTerminalDigest, value.lastIntermediateStateDigest}
	canonical, err := canonicalContractKey(record)
	if err != nil {
		return ""
	}
	return DigestBytes([]byte(canonical))
}

func projectionResultCompactDigest(value ProjectionResultEvidence) Digest {
	canonical, err := canonicalContractKey(value)
	if err != nil {
		return ""
	}
	return DigestBytes([]byte(canonical))
}

func compactPredecessorAllowsNextAttempt(terminal evidenceCompactTerminal, resolution *evidenceCompactResolution) bool {
	if terminal.outcome == "aborted_retryable" || terminal.outcome == "ambiguous_reconciled_pending" {
		return true
	}
	return terminal.outcome == "ambiguous_unresolved" && resolution != nil && resolution.outcome == "resolved_pending" && resolution.unresolvedTerminalDigest == terminal.terminalDigest
}

func validateEvidenceChainStructure(frames []EvidenceFrame) (*evidenceStructuralReplay, error) {
	if len(frames) == 0 {
		return nil, invalidEvidence("chain", "empty")
	}
	segments, err := logicalEvidenceSegments(frames)
	if err != nil {
		return nil, err
	}
	return validateEvidenceChainStructureSegments(segments)
}

func logicalEvidenceSegments(frames []EvidenceFrame) ([][]EvidenceFrame, error) {
	var segments [][]EvidenceFrame
	start := -1
	for index := range frames {
		if frames[index].RecordKind != EvidenceRecordHeader {
			if start < 0 {
				return nil, invalidEvidence("chain", "record before header")
			}
			continue
		}
		if start >= 0 {
			segments = append(segments, frames[start:index])
		}
		start = index
	}
	if start < 0 {
		if len(frames) != 0 {
			return nil, invalidEvidence("chain", "record before header")
		}
		return nil, nil
	}
	segments = append(segments, frames[start:])
	return segments, nil
}

func validateEvidenceChainStructureSegments(segments [][]EvidenceFrame) (*evidenceStructuralReplay, error) {
	return validateEvidenceChainStructureSegmentsObserved(segments, nil, nil)
}

func validateEvidenceChainStructureSegmentsObserved(segments [][]EvidenceFrame, requirements []evidenceCheckpointRequirement, observer evidenceStructuralObserver) (*evidenceStructuralReplay, error) {
	if len(segments) == 0 || len(segments) > int(maxEvidenceReservedSegments) {
		return nil, invalidEvidence("chain", "physical segment count")
	}
	accumulator := newEvidenceStructuralAccumulator(requirements, observer)
	for _, segment := range segments {
		if err := accumulator.beginSegment(); err != nil {
			return nil, err
		}
		for _, frame := range segment {
			canonical, err := canonicalContractKey(frame)
			if err != nil {
				return nil, err
			}
			if err := accumulator.consumeFrame(frame, uint64(len(canonical))+8); err != nil {
				return nil, err
			}
		}
		if err := accumulator.endSegment(); err != nil {
			return nil, err
		}
	}
	return accumulator.finish()
}

func validateStructuralTerminalCompact(terminal AttemptTerminalState, frame EvidenceFrame, state *evidenceCompactAttemptState) error {
	if terminal.RetryProof != nil {
		switch terminal.RetryProof.ProofKind {
		case "commit_rejected_exact_predecessor":
			if state.commit == nil || frame.Sequence != state.commit.sequence+1 || frame.PreviousRecordDigest == nil || *frame.PreviousRecordDigest != state.commit.recordDigest {
				return invalidEvidence("chain", "commit-rejected boundary")
			}
		case "projection_transient_exact_predecessor", "precommit_rollback_exact_predecessor", "precommit_connection_terminated_exact_predecessor":
			if state.commit != nil {
				return invalidEvidence("chain", "precommit retry after commit intent")
			}
		}
	}
	requiresCommit := terminal.Outcome == "committed" || len(terminal.Outcome) >= 10 && terminal.Outcome[:10] == "ambiguous_"
	if !requiresCommit {
		return nil
	}
	if state.lastIntermediate == nil || terminal.LastIntermediateStateDigest == nil || *terminal.LastIntermediateStateDigest != state.lastIntermediate.intermediateStateDigest {
		return invalidEvidence("chain", "terminal intermediate boundary")
	}
	if state.commit == nil || frame.Sequence != state.commit.sequence+1 || frame.PreviousRecordDigest == nil || *frame.PreviousRecordDigest != state.commit.recordDigest || state.commit.sequence != state.lastIntermediate.sequence+1 {
		return invalidEvidence("chain", "terminal commit boundary")
	}
	return nil
}

type lineageStructuralReplay struct {
	header        LineageIndexHeader
	supersessions []lineageStructuralSupersession
}
type lineageStructuralSupersession struct {
	frame      LineageIndexFrame
	checkpoint *LineageIndexFrame
}

type lineageJournalPlan struct {
	expected          EvidenceFrame
	requirements      []evidenceCheckpointRequirement
	activated         bool
	headerOnly        bool
	active            bool
	checkpointNext    uint64
	supersededOutcome string
	continuation      *evidenceStructuralContinuationSeed
}
type lineageStructuralPlan struct {
	header        LineageIndexHeader
	journals      map[Digest]*lineageJournalPlan
	finalReserved *GenerationReserved
	results       map[Digest]*evidenceStructuralReplay
	supersessions []lineageStructuralSupersession
}

func scanLineageChainStructure(frames []LineageIndexFrame) (*lineageStructuralPlan, error) {
	if len(frames) == 0 || len(frames) > 16384 {
		return nil, invalidEvidence("lineage-chain", "empty")
	}
	plan := &lineageStructuralPlan{journals: make(map[Digest]*lineageJournalPlan), results: make(map[Digest]*evidenceStructuralReplay)}
	var previous *Digest
	var indexBytes uint64
	var header *LineageIndexHeader
	var reservedFrame, activatedFrame, checkpointFrame, supersededFrame *LineageIndexFrame
	var checkpointNext uint64
	for index := range frames {
		frame := &frames[index]
		if err := frame.Validate(); err != nil {
			return nil, err
		}
		if frame.Sequence != uint64(index) || !equalDigestPointer(frame.PreviousRecordDigest, previous) {
			return nil, invalidEvidence("lineage-chain", "sequence")
		}
		canonical, err := canonicalContractKey(*frame)
		if err != nil {
			return nil, err
		}
		if indexBytes > ^uint64(0)-(uint64(len(canonical))+8) {
			return nil, invalidEvidence("lineage-chain", "index byte limit")
		}
		indexBytes += uint64(len(canonical)) + 8
		if validateLineageIndexUsage(uint64(index+1), indexBytes) != nil {
			return nil, invalidEvidence("lineage-chain", "index byte limit")
		}
		if index == 0 {
			if frame.RecordKind != LineageRecordHeader || frame.Record.Header == nil {
				return nil, invalidEvidence("lineage-chain", "header")
			}
			lineage, err := ExecutionLineageDigest(*frame.Record.Header)
			if err != nil || lineage != frame.Record.Header.ExecutionLineageDigest {
				return nil, invalidEvidence("lineage-chain", "constituent identity")
			}
			owned := cloneProjectionValue(*frame.Record.Header)
			header = &owned
			plan.header = owned
		} else {
			if frame.RecordKind == LineageRecordHeader || header == nil {
				return nil, invalidEvidence("lineage-chain", "second or missing header")
			}
			if supersededFrame != nil && frame.RecordKind != LineageRecordGenerationReserved {
				return nil, invalidEvidence("lineage-chain", "superseded generation is closed")
			}
			switch frame.RecordKind {
			case LineageRecordGenerationReserved:
				reserved := frame.Record.Reserved
				if reserved.ExecutionLineageDigest != header.ExecutionLineageDigest {
					return nil, invalidEvidence("lineage-chain", "reserved lineage")
				}
				if supersededFrame != nil {
					planned := supersededFrame.Record.Superseded.PlannedGenerationReserved
					if planned == nil || frame.Sequence != supersededFrame.Sequence+1 || !canonicalEqual(*planned, *reserved) {
						return nil, invalidEvidence("lineage-chain", "planned reservation")
					}
				} else if reservedFrame != nil {
					return nil, invalidEvidence("lineage-chain", "unplanned generation")
				} else if reserved.Continuation != nil {
					return nil, invalidEvidence("lineage-chain", "initial generation continuation")
				}
				reservedFrame, activatedFrame, checkpointFrame, supersededFrame, checkpointNext = frame, nil, nil, nil, 0
				owned := cloneProjectionValue(*reserved)
				plan.finalReserved = &owned
			case LineageRecordGenerationActivated:
				activated := frame.Record.Activated
				if reservedFrame == nil || activatedFrame != nil {
					return nil, invalidEvidence("lineage-chain", "activation position")
				}
				reserved := reservedFrame.Record.Reserved
				if activated.GenerationReservedRecordDigest != reservedFrame.RecordDigest || activated.ExecutionLineageDigest != reserved.ExecutionLineageDigest || activated.JournalIdentityDigest != reserved.JournalIdentityDigest || activated.RunnerProjectionDecisionDigest != reserved.RunnerProjectionDecisionDigest || activated.SchemaBundleDigest != reserved.SchemaBundleDigest || activated.QuotaReservationDigest != reserved.QuotaReservationDigest || activated.Segment0HeaderDigest != reserved.ExpectedSegment0HeaderDigest {
					return nil, invalidEvidence("lineage-chain", "activation binding")
				}
				if _, exists := plan.journals[activated.JournalIdentityDigest]; exists {
					return nil, invalidEvidence("lineage-chain", "registered journal missing")
				}
				expected := EvidenceFrame{FormatVersion: EvidenceFrameFormat, Sequence: 0, RecordKind: EvidenceRecordHeader, Record: EvidenceRecord{Header: &reserved.PlannedSegment0Header}, RecordDigest: reserved.ExpectedSegment0HeaderDigest}
				seed, err := newEvidenceStructuralContinuationSeed(reserved.Continuation)
				if err != nil {
					return nil, invalidEvidence("lineage-chain", "continuation seed")
				}
				plan.journals[activated.JournalIdentityDigest] = &lineageJournalPlan{expected: cloneProjectionValue(expected), activated: true, continuation: seed}
				activatedFrame = frame
				plan.finalReserved = nil
			case LineageRecordGenerationCheckpoint:
				checkpoint := frame.Record.Checkpoint
				if activatedFrame == nil {
					return nil, invalidEvidence("lineage-chain", "checkpoint before activation")
				}
				activated := activatedFrame.Record.Activated
				if checkpoint.ExecutionLineageDigest != activated.ExecutionLineageDigest || checkpoint.JournalIdentityDigest != activated.JournalIdentityDigest || checkpoint.RunnerProjectionDecisionDigest != activated.RunnerProjectionDecisionDigest || checkpoint.SchemaBundleDigest != activated.SchemaBundleDigest {
					return nil, invalidEvidence("lineage-chain", "checkpoint identity")
				}
				if checkpointFrame == nil {
					if checkpoint.PreviousCheckpointRecordDigest != nil {
						return nil, invalidEvidence("lineage-chain", "first checkpoint previous")
					}
				} else if checkpoint.PreviousCheckpointRecordDigest == nil || *checkpoint.PreviousCheckpointRecordDigest != checkpointFrame.RecordDigest {
					return nil, invalidEvidence("lineage-chain", "checkpoint previous")
				}
				if checkpoint.JournalNextSequence == 0 || checkpoint.JournalNextSequence <= checkpointNext {
					return nil, invalidEvidence("lineage-chain", "checkpoint tail")
				}
				journalPlan := plan.journals[checkpoint.JournalIdentityDigest]
				if journalPlan == nil {
					return nil, invalidEvidence("lineage-chain", "checkpoint identity")
				}
				journalPlan.requirements = append(journalPlan.requirements, evidenceCheckpointRequirement{checkpoint.JournalNextSequence, checkpoint.JournalTailDigest, evidenceJournalSummaryDigest(summaryFromCheckpoint(*checkpoint))})
				checkpointFrame, checkpointNext = frame, checkpoint.JournalNextSequence
			case LineageRecordGenerationSuperseded:
				superseded := frame.Record.Superseded
				if reservedFrame == nil || activatedFrame == nil || superseded.ExecutionLineageDigest != header.ExecutionLineageDigest || superseded.OldJournalIdentityDigest != activatedFrame.Record.Activated.JournalIdentityDigest || superseded.OldRunnerProjectionDecisionDigest != activatedFrame.Record.Activated.RunnerProjectionDecisionDigest || superseded.OldSchemaBundleDigest != activatedFrame.Record.Activated.SchemaBundleDigest {
					return nil, invalidEvidence("lineage-chain", "superseded generation identity")
				}
				if superseded.Outcome == "activated_no_migration_progress" {
					if checkpointFrame != nil || superseded.OldActivationRecordDigest == nil || *superseded.OldActivationRecordDigest != activatedFrame.RecordDigest || superseded.OldInitialJournalTailDigest == nil || *superseded.OldInitialJournalTailDigest != activatedFrame.Record.Activated.InitialJournalTailDigest {
						return nil, invalidEvidence("lineage-chain", "header boundary")
					}
				} else if checkpointFrame == nil || superseded.OldCheckpointRecordDigest == nil || *superseded.OldCheckpointRecordDigest != checkpointFrame.RecordDigest {
					return nil, invalidEvidence("lineage-chain", "checkpoint boundary")
				}
				if err := validateStructuralSupersessionContinuation(*superseded, *reservedFrame.Record.Reserved, checkpointFrame); err != nil {
					return nil, err
				}
				journalPlan := plan.journals[superseded.OldJournalIdentityDigest]
				journalPlan.supersededOutcome = superseded.Outcome
				journalPlan.checkpointNext = checkpointNext
				plan.supersessions = append(plan.supersessions, lineageStructuralSupersession{cloneProjectionValue(*frame), cloneLineageFrame(checkpointFrame)})
				supersededFrame = frame
			}
		}
		digest := frame.RecordDigest
		previous = &digest
	}
	if activatedFrame != nil && supersededFrame == nil {
		journalPlan := plan.journals[activatedFrame.Record.Activated.JournalIdentityDigest]
		journalPlan.active = true
		journalPlan.checkpointNext = checkpointNext
	}
	return plan, nil
}

func summaryFromCheckpoint(c GenerationCheckpoint) evidenceJournalSummary {
	return evidenceJournalSummary{c.RecoveryState, cloneStringPointer(c.MigrationID), cloneUint32Pointer(c.AttemptIndex), cloneDigestPointer(c.LastStatementIntentRecordDigest), cloneDigestPointer(c.LastIntermediateEvidenceRecordDigest), cloneDigestPointer(c.LastCommitIntentRecordDigest), cloneDigestPointer(c.LastTerminalDigest), cloneDigestPointer(c.LastResolutionDigest), cloneDigestPointer(c.PreviousAttemptTerminalDigest), cloneDigestPointer(c.LastIntermediateStateDigest)}
}

// newJournalAccumulator is the only production bridge from a strict lineage
// plan to a continuation-seeded journal replay. The seed never leaves this
// file or crosses the plan boundary as a caller-provided value.
func (p *lineageStructuralPlan) newJournalAccumulator(id Digest, observer evidenceStructuralObserver) (*evidenceStructuralAccumulator, bool) {
	build := func(requirements []evidenceCheckpointRequirement, seed *evidenceStructuralContinuationSeed) *evidenceStructuralAccumulator {
		accumulator := newEvidenceStructuralAccumulator(requirements, observer)
		if seed != nil {
			value := cloneProjectionValue(seed.context)
			accumulator.structuralSeed = &evidenceStructuralContinuationSeed{value}
		}
		return accumulator
	}
	journal := p.journals[id]
	if journal == nil {
		if p.finalReserved != nil && p.finalReserved.JournalIdentityDigest == id {
			seed, err := newEvidenceStructuralContinuationSeed(p.finalReserved.Continuation)
			if err != nil {
				return nil, false
			}
			return build(nil, seed), true
		}
		return nil, false
	}
	requirements := append([]evidenceCheckpointRequirement(nil), journal.requirements...)
	var seed *evidenceStructuralContinuationSeed
	if journal.continuation != nil {
		value := cloneProjectionValue(journal.continuation.context)
		seed = &evidenceStructuralContinuationSeed{value}
	}
	return build(requirements, seed), true
}

func (p *lineageStructuralPlan) acceptJournal(id Digest, replay *evidenceStructuralReplay) error {
	if replay == nil {
		return invalidEvidence("lineage-chain", "registered journal structure")
	}
	journal := p.journals[id]
	if journal == nil {
		if p.finalReserved == nil || p.finalReserved.JournalIdentityDigest != id {
			return invalidEvidence("lineage-chain", "orphan registered journal")
		}
		expected := EvidenceFrame{FormatVersion: EvidenceFrameFormat, Sequence: 0, RecordKind: EvidenceRecordHeader, Record: EvidenceRecord{Header: &p.finalReserved.PlannedSegment0Header}, RecordDigest: p.finalReserved.ExpectedSegment0HeaderDigest}
		if replay.records != 1 || replay.segments != 1 || !canonicalEqual(replay.firstFrame, expected) {
			return invalidEvidence("lineage-chain", "reserved header registration")
		}
	} else if !canonicalEqual(replay.firstFrame, journal.expected) {
		return invalidEvidence("lineage-chain", "registered journal structure")
	}
	if p.results[id] != nil {
		return invalidEvidence("lineage-chain", "registered journal missing")
	}
	p.results[id] = replay
	return nil
}

func (p *lineageStructuralPlan) finish(actual map[Digest]EvidenceFrame, journalIDs map[Digest]bool) (*lineageStructuralReplay, error) {
	expected := make(map[Digest]bool, len(p.journals)+1)
	for id, journal := range p.journals {
		replay := p.results[id]
		if replay == nil {
			return nil, invalidEvidence("lineage-chain", "registered journal missing")
		}
		if journal.supersededOutcome == "activated_no_migration_progress" && replay.records != 1 {
			return nil, invalidEvidence("lineage-chain", "header boundary")
		}
		if journal.supersededOutcome != "" && journal.supersededOutcome != "activated_no_migration_progress" && replay.records != journal.checkpointNext {
			return nil, invalidEvidence("lineage-chain", "checkpoint boundary")
		}
		if journal.active {
			if journal.checkpointNext == 0 {
				if replay.records > 2 {
					return nil, invalidEvidence("lineage-chain", "active journal without checkpoint")
				}
			} else if replay.records < journal.checkpointNext || replay.records > journal.checkpointNext+1 {
				return nil, invalidEvidence("lineage-chain", "active journal checkpoint lag")
			}
		}
		expected[id] = true
	}
	if p.finalReserved != nil {
		id := p.finalReserved.JournalIdentityDigest
		_, journalPresent := p.results[id]
		_, actualPresent := actual[id]
		if journalPresent != actualPresent {
			return nil, invalidEvidence("lineage-chain", "reserved header registration split")
		}
		if journalPresent {
			expected[id] = true
		}
	}
	if len(expected) != len(journalIDs) || len(expected) != len(actual) {
		return nil, invalidEvidence("lineage-chain", "registered journal cardinality")
	}
	for id := range expected {
		if !journalIDs[id] {
			return nil, invalidEvidence("lineage-chain", "registered journal missing")
		}
		actualFrame, ok := actual[id]
		if !ok || actualFrame.Validate() != nil || !canonicalEqual(actualFrame, p.results[id].firstFrame) {
			return nil, invalidEvidence("lineage-chain", "actual header")
		}
	}
	supersessions := make([]lineageStructuralSupersession, len(p.supersessions))
	for index := range p.supersessions {
		supersessions[index] = lineageStructuralSupersession{cloneProjectionValue(p.supersessions[index].frame), cloneLineageFrame(p.supersessions[index].checkpoint)}
	}
	return &lineageStructuralReplay{cloneProjectionValue(p.header), supersessions}, nil
}

func validateLineageChainStructure(frames []LineageIndexFrame, actualSegment0 map[Digest]EvidenceFrame, journals map[Digest][][]EvidenceFrame) (*lineageStructuralReplay, error) {
	plan, err := scanLineageChainStructure(frames)
	if err != nil {
		return nil, err
	}
	journalIDs := make(map[Digest]bool, len(journals))
	for id, segments := range journals {
		accumulator, ok := plan.newJournalAccumulator(id, nil)
		if !ok {
			return nil, invalidEvidence("lineage-chain", "orphan registered journal")
		}
		if len(segments) == 0 || len(segments) > int(maxEvidenceReservedSegments) {
			return nil, invalidEvidence("lineage-chain", "registered journal structure")
		}
		for _, segment := range segments {
			if err := accumulator.beginSegment(); err != nil {
				return nil, invalidEvidence("lineage-chain", "registered journal structure")
			}
			for _, frame := range segment {
				canonical, err := canonicalContractKey(frame)
				if err != nil || accumulator.consumeFrame(frame, uint64(len(canonical))+8) != nil {
					return nil, invalidEvidence("lineage-chain", "registered journal structure")
				}
			}
			if err := accumulator.endSegment(); err != nil {
				return nil, invalidEvidence("lineage-chain", "registered journal structure")
			}
		}
		replay, err := accumulator.finish()
		if err != nil {
			return nil, invalidEvidence("lineage-chain", "registered journal structure")
		}
		if err := plan.acceptJournal(id, replay); err != nil {
			return nil, err
		}
		journalIDs[id] = true
	}
	return plan.finish(actualSegment0, journalIDs)
}

func validateStructuralSupersessionContinuation(superseded GenerationSuperseded, oldReserved GenerationReserved, checkpointFrame *LineageIndexFrame) error {
	if superseded.Outcome == "activated_no_migration_progress" {
		if superseded.PlannedGenerationReserved == nil || !canonicalEqual(superseded.PlannedGenerationReserved.Continuation, oldReserved.Continuation) {
			return invalidEvidence("lineage-chain", "inherited continuation")
		}
		return nil
	}
	if superseded.PlannedGenerationReserved == nil {
		return nil
	}
	if checkpointFrame == nil || checkpointFrame.Record.Checkpoint == nil {
		return invalidEvidence("lineage-chain", "planned continuation checkpoint")
	}
	continuation := superseded.PlannedGenerationReserved.Continuation
	checkpoint := checkpointFrame.Record.Checkpoint
	if continuation == nil || continuation.SourceJournalIdentityDigest != superseded.OldJournalIdentityDigest || continuation.SourceCheckpointRecordDigest != checkpointFrame.RecordDigest || checkpoint.LastTerminalDigest == nil || continuation.SourceTerminalDigest != *checkpoint.LastTerminalDigest {
		return invalidEvidence("lineage-chain", "planned continuation source")
	}
	switch superseded.Outcome {
	case "exact_committed_continue_successor":
		if continuation.StartAction != "begin_first_attempt_next_entry" || continuation.AttemptIndex != 1 || continuation.PreviousAttemptTerminalDigest != nil {
			return invalidEvidence("lineage-chain", "next-entry continuation")
		}
	case "precommit_aborted_retryable", "exact_pending", "resolved_pending":
		if continuation.StartAction != "begin_next_attempt" || checkpoint.MigrationID == nil || checkpoint.AttemptIndex == nil || continuation.MigrationID != *checkpoint.MigrationID || continuation.AttemptIndex != *checkpoint.AttemptIndex+1 || continuation.PreviousAttemptTerminalDigest == nil || *continuation.PreviousAttemptTerminalDigest != continuation.SourceTerminalDigest {
			return invalidEvidence("lineage-chain", "next-attempt continuation")
		}
	}
	return nil
}

func cloneLineageFrame(frame *LineageIndexFrame) *LineageIndexFrame {
	if frame == nil {
		return nil
	}
	owned := cloneProjectionValue(*frame)
	return &owned
}
