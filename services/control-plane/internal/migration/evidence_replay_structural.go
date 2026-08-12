package migration

// evidenceStructuralReplay is a package-private description of facts proved by
// strict wire validation and the journal FSM. It is deliberately not sealed and
// carries no authority: copying or constructing one cannot mint a recovery
// snapshot, admission plan, content receipt, or external execution witness.
type evidenceStructuralReplay struct {
	segments  [][]EvidenceFrame
	frames    []EvidenceFrame
	headers   []JournalHeader
	intents   []StatementIntent
	terminals []evidenceStructuralTerminal
}

type evidenceStructuralTerminal struct {
	frame  EvidenceFrame
	header JournalHeader
	state  evidenceAttemptState
}

// validateEvidenceChainStructure is the legacy logical wrapper for callers that
// hold a flat frame chain. It groups at validated header boundaries, but that
// inferred grouping is not physical inventory authority. Admission replay must
// call validateEvidenceChainStructureSegments with one slice per inventoried
// segment file.
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
	for index := range frames {
		if frames[index].RecordKind == EvidenceRecordHeader {
			segments = append(segments, nil)
		}
		if len(segments) == 0 {
			return nil, invalidEvidence("chain", "record before header")
		}
		segments[len(segments)-1] = append(segments[len(segments)-1], cloneProjectionValue(frames[index]))
	}
	return segments, nil
}

func validateEvidenceChainStructureSegments(segments [][]EvidenceFrame) (*evidenceStructuralReplay, error) {
	if len(segments) == 0 || len(segments) > int(maxEvidenceReservedSegments) {
		return nil, invalidEvidence("chain", "physical segment count")
	}
	ownedSegments := cloneProjectionValue(segments)
	var owned []EvidenceFrame
	var physicalBytes uint64
	for segmentIndex := range ownedSegments {
		segment := ownedSegments[segmentIndex]
		if len(segment) == 0 || segment[0].RecordKind != EvidenceRecordHeader {
			return nil, invalidEvidence("chain", "physical segment header")
		}
		var segmentBytes uint64
		for frameIndex := range segment {
			if frameIndex != 0 && segment[frameIndex].RecordKind == EvidenceRecordHeader {
				return nil, invalidEvidence("chain", "middle physical segment header")
			}
			canonical, err := canonicalContractKey(segment[frameIndex])
			if err != nil {
				return nil, err
			}
			frameBytes := uint64(len(canonical)) + 8
			if segmentBytes > ^uint64(0)-frameBytes || physicalBytes > ^uint64(0)-frameBytes {
				return nil, invalidEvidence("chain", "physical byte overflow")
			}
			segmentBytes += frameBytes
			physicalBytes += frameBytes
		}
		if validateEvidenceSegmentUsage(uint64(len(segment)), segmentBytes) != nil {
			return nil, invalidEvidence("chain", "physical segment limit")
		}
		owned = append(owned, segment...)
	}
	replay := &evidenceStructuralReplay{segments: ownedSegments, frames: owned}
	var previous *Digest
	var header *JournalHeader
	var firstHeader *JournalHeader
	var previousSegmentFinal *Digest
	attempts := map[string]*evidenceAttemptState{}
	seenIntents := map[string]bool{}
	lastTerminal := map[string]*AttemptTerminalState{}
	lastResolution := map[string]*AmbiguousResolutionState{}
	for index := range owned {
		frame := &owned[index]
		if err := frame.Validate(); err != nil {
			return nil, err
		}
		if frame.Sequence != uint64(index) || !equalDigestPointer(frame.PreviousRecordDigest, previous) {
			return nil, invalidEvidence("chain", "sequence or previous")
		}
		if frame.RecordKind == EvidenceRecordHeader {
			h := frame.Record.Header
			if index == 0 {
				if h.SegmentIndex != 0 {
					return nil, invalidEvidence("chain", "initial segment")
				}
				firstHeader = h
			} else if header == nil || firstHeader == nil || h.SegmentIndex != header.SegmentIndex+1 || h.PreviousSegmentRecordDigest == nil || previousSegmentFinal == nil || *h.PreviousSegmentRecordDigest != *previousSegmentFinal || !sameJournalGeneration(*firstHeader, *h) {
				return nil, invalidEvidence("chain", "segment header")
			}
			header = h
			replay.headers = append(replay.headers, cloneProjectionValue(*h))
		} else {
			if header == nil {
				return nil, invalidEvidence("chain", "record before header")
			}
			if err := recordMatchesHeader(frame.Record, *header); err != nil {
				return nil, err
			}
			key, migration, attempt, err := evidenceAttemptIdentity(frame.Record)
			if err != nil {
				return nil, err
			}
			state := attempts[key]
			if state == nil {
				state = &evidenceAttemptState{}
				attempts[key] = state
			}
			switch frame.RecordKind {
			case EvidenceRecordStatementIntent:
				intent := frame.Record.StatementIntent
				if state.terminal != nil || state.commit != nil {
					return nil, invalidEvidence("chain", "intent after closed boundary")
				}
				statementKey := evidenceStatementKey(migration, attempt, intent.StatementIndex)
				if seenIntents[statementKey] {
					return nil, invalidEvidence("chain", "duplicate statement intent")
				}
				seenIntents[statementKey] = true
				if state.lastIntent == nil {
					if intent.StatementIndex != 0 {
						return nil, invalidEvidence("chain", "first statement index")
					}
				} else if intent.StatementIndex != state.lastIntent.Record.StatementIntent.StatementIndex+1 {
					return nil, invalidEvidence("chain", "statement index gap")
				}
				if intent.StatementIndex == 0 {
					predecessor := lastTerminal[migration]
					if predecessor == nil {
						if attempt != 1 || intent.PreviousAttemptTerminalDigest != nil {
							return nil, invalidEvidence("chain", "first attempt")
						}
					} else if attempt != predecessor.AttemptIndex+1 || intent.PreviousAttemptTerminalDigest == nil || *intent.PreviousAttemptTerminalDigest != predecessor.TerminalDigest || !predecessorAllowsNextAttempt(*predecessor, lastResolution[migration]) {
						return nil, invalidEvidence("chain", "attempt predecessor")
					}
				} else if state.lastIntermediate == nil || intent.PreviousIntermediateStateDigest == nil || *intent.PreviousIntermediateStateDigest != state.lastIntermediate.Record.Intermediate.State.IntermediateStateDigest {
					return nil, invalidEvidence("chain", "statement predecessor")
				}
				state.lastIntent = frame
				replay.intents = append(replay.intents, cloneProjectionValue(*intent))
			case EvidenceRecordIntermediate:
				intermediate := frame.Record.Intermediate
				if state.lastIntent == nil || state.commit != nil || state.terminal != nil || state.lastIntermediate != nil && state.lastIntermediate.Record.Intermediate.State.StatementIndex == state.lastIntent.Record.StatementIntent.StatementIndex {
					return nil, invalidEvidence("chain", "intermediate position")
				}
				intent := state.lastIntent.Record.StatementIntent
				if intermediate.State.StatementIndex != intent.StatementIndex || intermediate.State.StatementSHA256 != intent.StatementSHA256 || !projectionEvidenceEqual(intermediate.AuthorityBeforeResult, intent.AuthorityBeforeResult) || !projectionEvidenceEqual(intermediate.CatalogBeforeResult, intent.CatalogBeforeResult) {
					return nil, invalidEvidence("chain", "intermediate intent")
				}
				state.lastIntermediate = frame
			case EvidenceRecordCommitIntent:
				commit := frame.Record.CommitIntent
				if state.commit != nil || state.terminal != nil || state.lastIntermediate == nil {
					return nil, invalidEvidence("chain", "commit position")
				}
				if commit.LastIntermediateStateDigest != state.lastIntermediate.Record.Intermediate.State.IntermediateStateDigest {
					return nil, invalidEvidence("chain", "commit intermediate")
				}
				state.commit = frame
			case EvidenceRecordAttemptTerminal:
				terminal := frame.Record.AttemptTerminal
				if state.terminal != nil || state.resolution != nil {
					return nil, invalidEvidence("chain", "second terminal")
				}
				if err := validateStructuralTerminal(*terminal, *frame, state); err != nil {
					return nil, err
				}
				state.terminal = frame
				lastTerminal[migration] = terminal
				replay.terminals = append(replay.terminals, evidenceStructuralTerminal{cloneProjectionValue(*frame), cloneProjectionValue(*header), cloneEvidenceAttemptState(state)})
			case EvidenceRecordAmbiguousResolution:
				resolution := frame.Record.AmbiguousResolution
				if state.terminal == nil || state.resolution != nil || index == 0 || owned[index-1].RecordKind != EvidenceRecordAttemptTerminal {
					return nil, invalidEvidence("chain", "resolution adjacency")
				}
				terminal := state.terminal.Record.AttemptTerminal
				if terminal.Outcome != "ambiguous_unresolved" || resolution.UnresolvedTerminalDigest != terminal.TerminalDigest || terminal.StableErrorCode == nil || string(resolution.StableErrorCode) != *terminal.StableErrorCode {
					return nil, invalidEvidence("chain", "resolution terminal")
				}
				state.resolution = frame
				lastResolution[migration] = resolution
			}
		}
		digest := frame.RecordDigest
		previous = &digest
		previousSegmentFinal = &digest
	}
	if firstHeader == nil || uint64(len(owned)) > firstHeader.ReservedRecords || uint32(len(ownedSegments)) > firstHeader.ReservedSegments || physicalBytes > firstHeader.ReservedBytes {
		return nil, invalidEvidence("chain", "observed journal exceeds reservation")
	}
	return replay, nil
}

func validateStructuralTerminal(terminal AttemptTerminalState, frame EvidenceFrame, state *evidenceAttemptState) error {
	if terminal.RetryProof != nil {
		switch terminal.RetryProof.ProofKind {
		case "commit_rejected_exact_predecessor":
			if state.commit == nil || frame.Sequence != state.commit.Sequence+1 || frame.PreviousRecordDigest == nil || *frame.PreviousRecordDigest != state.commit.RecordDigest {
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
	if state.lastIntermediate == nil || terminal.LastIntermediateStateDigest == nil || *terminal.LastIntermediateStateDigest != state.lastIntermediate.Record.Intermediate.State.IntermediateStateDigest {
		return invalidEvidence("chain", "terminal intermediate boundary")
	}
	if state.commit == nil || frame.Sequence != state.commit.Sequence+1 || frame.PreviousRecordDigest == nil || *frame.PreviousRecordDigest != state.commit.RecordDigest || state.commit.Sequence != state.lastIntermediate.Sequence+1 || state.commit.PreviousRecordDigest == nil || *state.commit.PreviousRecordDigest != state.lastIntermediate.RecordDigest {
		return invalidEvidence("chain", "terminal commit boundary")
	}
	return nil
}

func cloneEvidenceAttemptState(state *evidenceAttemptState) evidenceAttemptState {
	clone := func(frame *EvidenceFrame) *EvidenceFrame {
		if frame == nil {
			return nil
		}
		owned := cloneProjectionValue(*frame)
		return &owned
	}
	return evidenceAttemptState{clone(state.lastIntent), clone(state.lastIntermediate), clone(state.commit), clone(state.terminal), clone(state.resolution)}
}

type lineageStructuralReplay struct {
	frames        []LineageIndexFrame
	header        LineageIndexHeader
	supersessions []lineageStructuralSupersession
}

type lineageStructuralSupersession struct {
	frame      LineageIndexFrame
	checkpoint *LineageIndexFrame
}

// actualSegment0 and journals describe the registered set referenced by this
// one lineage. This validator requires every activated generation it observes;
// the future ALL-history inventory adapter remains responsible for rejecting
// extra/orphan registered journal views before it calls this package-private
// seam.
func validateLineageChainStructure(frames []LineageIndexFrame, actualSegment0 map[Digest]EvidenceFrame, journals map[Digest][][]EvidenceFrame) (*lineageStructuralReplay, error) {
	if len(frames) == 0 || len(frames) > 16384 {
		return nil, invalidEvidence("lineage-chain", "empty")
	}
	owned := cloneProjectionValue(frames)
	replay := &lineageStructuralReplay{frames: owned}
	journalReplays := make(map[Digest]*evidenceStructuralReplay, len(journals))
	activatedJournals := make(map[Digest]bool)
	var previous *Digest
	var indexBytes uint64
	var header *LineageIndexHeader
	var reservedFrame *LineageIndexFrame
	var activatedFrame *LineageIndexFrame
	var checkpointFrame *LineageIndexFrame
	var checkpointNextSequence uint64
	var supersededFrame *LineageIndexFrame
	for index := range owned {
		frame := &owned[index]
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
			header = frame.Record.Header
			replay.header = cloneProjectionValue(*header)
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
				reservedFrame, activatedFrame, checkpointFrame, supersededFrame, checkpointNextSequence = frame, nil, nil, nil, 0
			case LineageRecordGenerationActivated:
				activated := frame.Record.Activated
				if reservedFrame == nil || activatedFrame != nil {
					return nil, invalidEvidence("lineage-chain", "activation position")
				}
				reserved := reservedFrame.Record.Reserved
				if activated.GenerationReservedRecordDigest != reservedFrame.RecordDigest || activated.ExecutionLineageDigest != reserved.ExecutionLineageDigest || activated.JournalIdentityDigest != reserved.JournalIdentityDigest || activated.RunnerProjectionDecisionDigest != reserved.RunnerProjectionDecisionDigest || activated.SchemaBundleDigest != reserved.SchemaBundleDigest || activated.QuotaReservationDigest != reserved.QuotaReservationDigest || activated.Segment0HeaderDigest != reserved.ExpectedSegment0HeaderDigest {
					return nil, invalidEvidence("lineage-chain", "activation binding")
				}
				actual, ok := actualSegment0[activated.JournalIdentityDigest]
				expected := EvidenceFrame{FormatVersion: EvidenceFrameFormat, Sequence: 0, PreviousRecordDigest: nil, RecordKind: EvidenceRecordHeader, Record: EvidenceRecord{Header: &reserved.PlannedSegment0Header}, RecordDigest: reserved.ExpectedSegment0HeaderDigest}
				if !ok || actual.Validate() != nil || actual.Sequence != 0 || actual.PreviousRecordDigest != nil || actual.RecordKind != EvidenceRecordHeader || actual.RecordDigest != activated.Segment0HeaderDigest || !canonicalEqual(actual, expected) {
					return nil, invalidEvidence("lineage-chain", "actual header")
				}
				journal, ok := journals[activated.JournalIdentityDigest]
				if !ok || activatedJournals[activated.JournalIdentityDigest] {
					return nil, invalidEvidence("lineage-chain", "registered journal missing")
				}
				journalReplay, err := validateEvidenceChainStructureSegments(journal)
				if err != nil || len(journalReplay.frames) == 0 || !canonicalEqual(journalReplay.frames[0], actual) {
					return nil, invalidEvidence("lineage-chain", "registered journal structure")
				}
				journalReplays[activated.JournalIdentityDigest] = journalReplay
				activatedJournals[activated.JournalIdentityDigest] = true
				activatedFrame = frame
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
				journal := journalReplays[checkpoint.JournalIdentityDigest]
				if journal == nil || checkpoint.JournalNextSequence == 0 || checkpoint.JournalNextSequence > uint64(len(journal.frames)) || checkpoint.JournalNextSequence <= checkpointNextSequence || journal.frames[checkpoint.JournalNextSequence-1].RecordDigest != checkpoint.JournalTailDigest {
					return nil, invalidEvidence("lineage-chain", "checkpoint tail")
				}
				prefix, err := validateEvidenceChainStructure(journal.frames[:checkpoint.JournalNextSequence])
				if err != nil {
					return nil, invalidEvidence("lineage-chain", "checkpoint prefix")
				}
				summary, err := summarizeStructuralEvidenceJournal(prefix)
				if err != nil || !checkpointSummaryEqual(*checkpoint, summary) {
					return nil, invalidEvidence("lineage-chain", "checkpoint summary")
				}
				checkpointFrame = frame
				checkpointNextSequence = checkpoint.JournalNextSequence
			case LineageRecordGenerationSuperseded:
				superseded := frame.Record.Superseded
				if reservedFrame == nil || activatedFrame == nil || superseded.ExecutionLineageDigest != header.ExecutionLineageDigest || superseded.OldJournalIdentityDigest != activatedFrame.Record.Activated.JournalIdentityDigest || superseded.OldRunnerProjectionDecisionDigest != activatedFrame.Record.Activated.RunnerProjectionDecisionDigest || superseded.OldSchemaBundleDigest != activatedFrame.Record.Activated.SchemaBundleDigest {
					return nil, invalidEvidence("lineage-chain", "superseded generation identity")
				}
				if superseded.Outcome == "activated_no_migration_progress" {
					journal := journalReplays[superseded.OldJournalIdentityDigest]
					if checkpointFrame != nil || journal == nil || len(journal.frames) != 1 || superseded.OldActivationRecordDigest == nil || *superseded.OldActivationRecordDigest != activatedFrame.RecordDigest || superseded.OldInitialJournalTailDigest == nil || *superseded.OldInitialJournalTailDigest != activatedFrame.Record.Activated.InitialJournalTailDigest {
						return nil, invalidEvidence("lineage-chain", "header boundary")
					}
				} else if checkpointFrame == nil || superseded.OldCheckpointRecordDigest == nil || *superseded.OldCheckpointRecordDigest != checkpointFrame.RecordDigest || checkpointFrame.Record.Checkpoint.JournalNextSequence != uint64(len(journalReplays[superseded.OldJournalIdentityDigest].frames)) {
					return nil, invalidEvidence("lineage-chain", "checkpoint boundary")
				}
				if err := validateStructuralSupersessionContinuation(*superseded, *reservedFrame.Record.Reserved, checkpointFrame); err != nil {
					return nil, err
				}
				replay.supersessions = append(replay.supersessions, lineageStructuralSupersession{cloneProjectionValue(*frame), cloneLineageFrame(checkpointFrame)})
				supersededFrame = frame
			}
		}
		digest := frame.RecordDigest
		previous = &digest
	}
	registeredJournals := make(map[Digest]bool, len(activatedJournals)+1)
	for digest := range activatedJournals {
		registeredJournals[digest] = true
	}
	if reservedFrame != nil && activatedFrame == nil {
		reserved := reservedFrame.Record.Reserved
		journal, journalPresent := journals[reserved.JournalIdentityDigest]
		actual, actualPresent := actualSegment0[reserved.JournalIdentityDigest]
		if journalPresent != actualPresent {
			return nil, invalidEvidence("lineage-chain", "reserved header registration split")
		}
		if journalPresent {
			expected := EvidenceFrame{FormatVersion: EvidenceFrameFormat, Sequence: 0, PreviousRecordDigest: nil, RecordKind: EvidenceRecordHeader, Record: EvidenceRecord{Header: &reserved.PlannedSegment0Header}, RecordDigest: reserved.ExpectedSegment0HeaderDigest}
			if len(journal) != 1 || len(journal[0]) != 1 || actual.Validate() != nil || !canonicalEqual(journal[0][0], actual) || !canonicalEqual(actual, expected) {
				return nil, invalidEvidence("lineage-chain", "reserved header registration")
			}
			registeredJournals[reserved.JournalIdentityDigest] = true
		}
	}
	if activatedFrame != nil && supersededFrame == nil {
		journal := journalReplays[activatedFrame.Record.Activated.JournalIdentityDigest]
		if journal == nil {
			return nil, invalidEvidence("lineage-chain", "active journal missing")
		}
		// Historical checkpoint intervals remain sparse for the C3 wire format.
		// Reopening the one active generation is narrower: durable evidence may
		// contain at most one linear candidate beyond its latest checkpoint.
		if checkpointFrame == nil {
			if len(journal.frames) > 2 {
				return nil, invalidEvidence("lineage-chain", "active journal without checkpoint")
			}
		} else if uint64(len(journal.frames)) < checkpointNextSequence || uint64(len(journal.frames)) > checkpointNextSequence+1 {
			return nil, invalidEvidence("lineage-chain", "active journal checkpoint lag")
		}
	}
	if len(registeredJournals) != len(journals) || len(registeredJournals) != len(actualSegment0) {
		return nil, invalidEvidence("lineage-chain", "registered journal cardinality")
	}
	for digest := range journals {
		if !registeredJournals[digest] {
			return nil, invalidEvidence("lineage-chain", "orphan registered journal")
		}
	}
	for digest := range actualSegment0 {
		if !registeredJournals[digest] {
			return nil, invalidEvidence("lineage-chain", "orphan segment zero")
		}
	}
	return replay, nil
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
