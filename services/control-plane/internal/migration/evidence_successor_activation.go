package migration

import (
	"bytes"
	"context"
	"crypto/sha256"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/evidencefs"
)

type SuccessorGenerationHeaderTransitionResult struct {
	outcome            evidencefs.AdmissionTransitionOutcome
	next               *SuccessorHeaderDurablePermit
	candidateDigest    [32]byte
	candidateSequence  uint64
	candidateRevision  uint64
	previousRevision   uint64
	journal            Digest
	headerRecordDigest Digest
	headerBytesDigest  [32]byte
	headerSize         uint64
}

func (r SuccessorGenerationHeaderTransitionResult) Outcome() evidencefs.AdmissionTransitionOutcome {
	return r.outcome
}
func (r SuccessorGenerationHeaderTransitionResult) Next() *SuccessorHeaderDurablePermit {
	return r.next
}
func (r SuccessorGenerationHeaderTransitionResult) CandidateKind() string {
	return "generation_header"
}
func (r SuccessorGenerationHeaderTransitionResult) CandidateDigest() [32]byte {
	return r.candidateDigest
}
func (r SuccessorGenerationHeaderTransitionResult) CandidateSequence() uint64 {
	return r.candidateSequence
}
func (r SuccessorGenerationHeaderTransitionResult) CandidateRevision() uint64 {
	return r.candidateRevision
}
func (r SuccessorGenerationHeaderTransitionResult) PreviousRevision() uint64 {
	return r.previousRevision
}
func (r SuccessorGenerationHeaderTransitionResult) Journal() Digest { return r.journal }
func (r SuccessorGenerationHeaderTransitionResult) HeaderRecordDigest() Digest {
	return r.headerRecordDigest
}
func (r SuccessorGenerationHeaderTransitionResult) HeaderBytesDigest() [32]byte {
	return r.headerBytesDigest
}
func (r SuccessorGenerationHeaderTransitionResult) HeaderSize() uint64 { return r.headerSize }

type SuccessorHeaderDurablePermit struct {
	self  *SuccessorHeaderDurablePermit
	state *successorAdmissionState
}

type SuccessorGenerationActivationTransitionResult struct {
	outcome                evidencefs.AdmissionTransitionOutcome
	next                   *SuccessorGenerationReadyPermit
	candidateDigest        [32]byte
	candidateSequence      uint64
	candidateRevision      uint64
	previousRevision       uint64
	activationRecordDigest Digest
	reservedRecordDigest   Digest
	headerRecordDigest     Digest
}

func (r SuccessorGenerationActivationTransitionResult) Outcome() evidencefs.AdmissionTransitionOutcome {
	return r.outcome
}
func (r SuccessorGenerationActivationTransitionResult) Next() *SuccessorGenerationReadyPermit {
	return r.next
}
func (r SuccessorGenerationActivationTransitionResult) CandidateKind() string {
	return "generation_activated"
}
func (r SuccessorGenerationActivationTransitionResult) CandidateDigest() [32]byte {
	return r.candidateDigest
}
func (r SuccessorGenerationActivationTransitionResult) CandidateSequence() uint64 {
	return r.candidateSequence
}
func (r SuccessorGenerationActivationTransitionResult) CandidateRevision() uint64 {
	return r.candidateRevision
}
func (r SuccessorGenerationActivationTransitionResult) PreviousRevision() uint64 {
	return r.previousRevision
}
func (r SuccessorGenerationActivationTransitionResult) ActivationRecordDigest() Digest {
	return r.activationRecordDigest
}
func (r SuccessorGenerationActivationTransitionResult) ReservedRecordDigest() Digest {
	return r.reservedRecordDigest
}
func (r SuccessorGenerationActivationTransitionResult) HeaderRecordDigest() Digest {
	return r.headerRecordDigest
}

type SuccessorGenerationReadyPermit struct {
	self  *SuccessorGenerationReadyPermit
	state *successorAdmissionState
}

func (p *SuccessorReservedDurablePermit) CreateGenerationHeader(ctx context.Context, candidate OwnedCurrentCandidate) (SuccessorGenerationHeaderTransitionResult, error) {
	pre := SuccessorGenerationHeaderTransitionResult{outcome: evidencefs.AdmissionTransitionPreMutationFailure, candidateSequence: 8}
	if p == nil || p.self != p || p.state == nil || !validSuccessorAdmissionState(p, p.state, successorAdmissionReservedDurable, candidate) || !successorPlannedGenerationExact(p.state, candidate) {
		return pre, fail(CodeEvidenceRecoveryRequired, "successor-generation-header", "successor durable reservation authority is unavailable", nil)
	}
	if err := contextAdmissionError(ctx); err != nil {
		return pre, err
	}
	reserved := cloneProjectionValue(*p.state.plan.reservedFrame.Record.Reserved)
	generation := generationIdentity{
		owner: candidate.owner, executionLineageDigest: reserved.ExecutionLineageDigest,
		journalIdentityDigest: reserved.JournalIdentityDigest, runnerProjectionDecisionDigest: reserved.RunnerProjectionDecisionDigest,
		schemaBundleDigest: reserved.SchemaBundleDigest,
	}
	activationHeader, bindErr := bindActivationHeader(generation, reserved, p.state.runtimeReceipt, p.state.recoveryReceipt)
	headerFrame, headerBytes, encodeErr := encodeAdmissionActivationHeader(activationHeader)
	if bindErr != nil || encodeErr != nil || !canonicalEqual(headerFrame, p.state.plan.headerFrame) || headerFrame.RecordDigest != reserved.ExpectedSegment0HeaderDigest {
		return pre, fail(CodeEvidenceRecoveryRequired, "successor-generation-header", "successor activation header does not match reservation receipts", nil)
	}
	headerBytes = append([]byte(nil), headerBytes...)
	pre.previousRevision = p.state.revision
	pre.candidateRevision = p.state.revision + 1
	pre.journal = reserved.JournalIdentityDigest
	pre.headerRecordDigest = headerFrame.RecordDigest
	pre.headerBytesDigest = sha256.Sum256(headerBytes)
	pre.headerSize = uint64(len(headerBytes))
	if pre.headerBytesDigest == ([32]byte{}) || pre.headerSize == 0 || pre.journal.Validate() != nil {
		return pre, fail(CodeEvidenceRecoveryRequired, "successor-generation-header", "successor header identity is invalid", nil)
	}
	if _, err := readSuccessorCurrentIndex(ctx, p.state, p.state.indexRecords, p.state.indexTail, p.state.indexDigest, p.state.indexSize); err != nil {
		return pre, err
	}
	journalCount, err := validateSuccessorJournalInventory(ctx, p.state.inventory, p.state.target, pre.journal, nil, 0, false, "successor-generation-header-prefix")
	if err != nil {
		return pre, err
	}
	if !validSuccessorAdmissionState(p, p.state, successorAdmissionReservedDurable, candidate) || !successorPlannedGenerationExact(p.state, candidate) || !canonicalEqual(reserved, *p.state.plan.reservedFrame.Record.Reserved) {
		return pre, fail(CodeEvidenceRecoveryRequired, "successor-generation-header", "successor durable reservation changed before header creation", nil)
	}
	if !p.state.consumed.CompareAndSwap(false, true) {
		return pre, fail(CodeEvidenceRecoveryRequired, "successor-generation-header", "successor durable reservation authority was already consumed", nil)
	}
	fsResult, transitionErr := p.state.mutation.CreateGenerationHeader(ctx, p.state.inventory, digestRaw(pre.journal), headerBytes)
	result := SuccessorGenerationHeaderTransitionResult{
		outcome: fsResult.Outcome(), candidateDigest: fsResult.CandidateDigest(), candidateSequence: 8,
		candidateRevision: fsResult.CandidateRevision(), previousRevision: fsResult.PreviousRevision(), journal: pre.journal,
		headerRecordDigest: pre.headerRecordDigest, headerBytesDigest: pre.headerBytesDigest, headerSize: pre.headerSize,
	}
	if fsResult.Outcome() != evidencefs.AdmissionTransitionDurable {
		if fsResult.Outcome() == evidencefs.AdmissionTransitionPreMutationFailure {
			p.state.consumed.CompareAndSwap(true, false)
		}
		return result, mapAdmissionMutationError(transitionErr, "successor-generation-header")
	}
	postFailure := func(suffix string) (SuccessorGenerationHeaderTransitionResult, error) {
		_ = fsResult.Invalidate()
		return successorGenerationHeaderUnknown(result), admissionPostMutationFailure("successor-generation-header" + suffix)
	}
	if transitionErr != nil || fsResult.CandidateKind() != "generation_header" || fsResult.CandidateSequence() != 0 || fsResult.CandidateDigest() == ([32]byte{}) || fsResult.PreviousRevision() != pre.previousRevision || fsResult.CandidateRevision() != pre.candidateRevision || fsResult.Journal() != digestRaw(pre.journal) || fsResult.HeaderDigest() != pre.headerBytesDigest || fsResult.HeaderSize() != pre.headerSize || fsResult.Inventory() == nil || !fsResult.ValidFor(fsResult.Inventory()) {
		return postFailure("")
	}
	nextInventory := fsResult.Inventory()
	revision, revisionErr := nextInventory.Revision()
	target, targetErr := nextInventory.Target()
	fullSet, fullSetErr := nextInventory.FullSetDigest()
	if revisionErr != nil || revision != pre.candidateRevision {
		return postFailure("-revision")
	}
	if targetErr != nil || target != p.state.target {
		return postFailure("-target")
	}
	if fullSetErr != nil || fullSet == ([32]byte{}) || fullSet == p.state.fullSet {
		return postFailure("-full-set")
	}
	index, indexErr := readSuccessorInventoryIndex(ctx, nextInventory, target, p.state.indexRecords, p.state.indexTail, p.state.indexDigest, p.state.indexSize, "successor-generation-header-index")
	if indexErr != nil || index.digest != p.state.indexDigest {
		return postFailure("-index")
	}
	nextJournalCount, journalErr := validateSuccessorJournalInventory(ctx, nextInventory, target, pre.journal, headerBytes, journalCount+1, true, "successor-generation-header-inventory")
	if journalErr != nil || nextJournalCount != journalCount+1 {
		return postFailure("-inventory")
	}
	if !successorReceiptsExact(p.state, candidate) {
		return postFailure("-receipts")
	}
	nextToken, tokenErr := nextInventory.MutationToken()
	if tokenErr != nil || !nextToken.ValidFor(nextInventory) {
		return postFailure("-token")
	}
	step := successorAdmissionStep{inventory: nextInventory, mutation: nextToken, target: target, fullSet: fullSet, revision: revision}
	nextState := nextSuccessorAdmissionState(p.state, successorAdmissionHeaderDurable, step)
	nextState.fsJournal = fsResult
	nextState.journal = pre.journal
	nextState.headerDigest = pre.headerRecordDigest
	nextState.journalCount = nextJournalCount
	nextState.activationHeader = cloneSuccessorActivationHeader(activationHeader)
	nextState.headerFrame = cloneProjectionValue(headerFrame)
	nextState.headerBytes = headerBytes
	nextState.headerBytesHash = pre.headerBytesDigest
	nextState.fsJournalCandidate = fsResult.CandidateDigest()
	next := &SuccessorHeaderDurablePermit{state: nextState}
	next.self = next
	if !sealSuccessorAdmissionState(next, nextState) || !validSuccessorAdmissionState(next, nextState, successorAdmissionHeaderDurable, candidate) {
		if nextState.binding != nil {
			successorAdmissionStateRegistry.Delete(nextState.binding)
		}
		return postFailure("-seal")
	}
	result.next = next
	return result, nil
}

func (p *SuccessorHeaderDurablePermit) AppendGenerationActivated(ctx context.Context, candidate OwnedCurrentCandidate) (SuccessorGenerationActivationTransitionResult, error) {
	pre := SuccessorGenerationActivationTransitionResult{outcome: evidencefs.AdmissionTransitionPreMutationFailure, candidateSequence: 9}
	if p == nil || p.self != p || p.state == nil || !validSuccessorAdmissionState(p, p.state, successorAdmissionHeaderDurable, candidate) {
		return pre, fail(CodeEvidenceRecoveryRequired, "successor-generation-activate", "successor header-durable authority is unavailable", nil)
	}
	if err := contextAdmissionError(ctx); err != nil {
		return pre, err
	}
	activatedFrame, activatedBytes, buildErr := buildSuccessorActivatedFrame(p.state.plan.reservedFrame, p.state.headerFrame)
	if buildErr != nil {
		return pre, fail(CodeEvidenceRecoveryRequired, "successor-generation-activate", "successor activation frame cannot be constructed", nil)
	}
	activatedBytes = append([]byte(nil), activatedBytes...)
	pre.previousRevision = p.state.revision
	pre.candidateRevision = p.state.revision + 1
	pre.candidateDigest = sha256.Sum256(activatedBytes)
	pre.activationRecordDigest = activatedFrame.RecordDigest
	pre.reservedRecordDigest = p.state.reservedDigest
	pre.headerRecordDigest = p.state.headerDigest
	if pre.candidateDigest == ([32]byte{}) || pre.activationRecordDigest.Validate() != nil {
		return pre, fail(CodeEvidenceRecoveryRequired, "successor-generation-activate", "successor activation identity is invalid", nil)
	}
	prefix, err := readSuccessorCurrentIndex(ctx, p.state, p.state.indexRecords, p.state.indexTail, p.state.indexDigest, p.state.indexSize)
	if err != nil {
		return pre, err
	}
	journalCount, err := validateSuccessorJournalInventory(ctx, p.state.inventory, p.state.target, p.state.journal, p.state.headerBytes, p.state.journalCount, true, "successor-generation-activate-prefix")
	if err != nil || journalCount != p.state.journalCount {
		return pre, err
	}
	if activatedFrame.Sequence != uint64(len(prefix.frames)) || activatedFrame.PreviousRecordDigest == nil || *activatedFrame.PreviousRecordDigest != prefix.tail || !validSuccessorAdmissionState(p, p.state, successorAdmissionHeaderDurable, candidate) || !bytes.Equal(activatedBytes, mustEncodeLineageFrame(activatedFrame)) {
		return pre, fail(CodeEvidenceRecoveryRequired, "successor-generation-activate", "successor header-durable authority changed before activation", nil)
	}
	if !p.state.consumed.CompareAndSwap(false, true) {
		return pre, fail(CodeEvidenceRecoveryRequired, "successor-generation-activate", "successor header-durable authority was already consumed", nil)
	}
	fsResult, transitionErr := p.state.mutation.AppendTargetIndex(ctx, p.state.inventory, activatedBytes)
	result := SuccessorGenerationActivationTransitionResult{
		outcome: fsResult.Outcome(), candidateDigest: fsResult.CandidateDigest(), candidateSequence: 9,
		candidateRevision: fsResult.CandidateRevision(), previousRevision: fsResult.PreviousRevision(),
		activationRecordDigest: pre.activationRecordDigest, reservedRecordDigest: pre.reservedRecordDigest, headerRecordDigest: pre.headerRecordDigest,
	}
	if fsResult.Outcome() != evidencefs.AdmissionTransitionDurable {
		if fsResult.Outcome() == evidencefs.AdmissionTransitionPreMutationFailure {
			p.state.consumed.CompareAndSwap(true, false)
		}
		return result, mapAdmissionMutationError(transitionErr, "successor-generation-activate")
	}
	postFailure := func(suffix string) (SuccessorGenerationActivationTransitionResult, error) {
		_ = fsResult.Invalidate()
		return successorGenerationActivationUnknown(result), admissionPostMutationFailure("successor-generation-activate" + suffix)
	}
	if transitionErr != nil || fsResult.CandidateKind() != "target_index_append" || fsResult.CandidateSequence() != 0 || fsResult.CandidateDigest() != pre.candidateDigest || fsResult.PreviousRevision() != pre.previousRevision || fsResult.CandidateRevision() != pre.candidateRevision || fsResult.Inventory() == nil {
		return postFailure("")
	}
	nextInventory := fsResult.Inventory()
	revision, revisionErr := nextInventory.Revision()
	target, targetErr := nextInventory.Target()
	fullSet, fullSetErr := nextInventory.FullSetDigest()
	if revisionErr != nil || revision != pre.candidateRevision {
		return postFailure("-revision")
	}
	if targetErr != nil || target != p.state.target {
		return postFailure("-target")
	}
	if fullSetErr != nil || fullSet == ([32]byte{}) || fullSet == p.state.fullSet {
		return postFailure("-full-set")
	}
	verified, verifyErr := validateSuccessorIndexAppend(ctx, nextInventory, target, prefix.raw, activatedBytes, p.state.indexRecords+1, pre.activationRecordDigest)
	if verifyErr != nil {
		return postFailure("-index")
	}
	nextJournalCount, journalErr := validateSuccessorJournalInventory(ctx, nextInventory, target, p.state.journal, p.state.headerBytes, p.state.journalCount, true, "successor-generation-activate-inventory")
	if journalErr != nil || nextJournalCount != p.state.journalCount {
		return postFailure("-inventory")
	}
	if !successorReceiptsExact(p.state, candidate) {
		return postFailure("-receipts")
	}
	nextToken, tokenErr := nextInventory.MutationToken()
	if tokenErr != nil || !nextToken.ValidFor(nextInventory) {
		return postFailure("-token")
	}
	step := successorAdmissionStep{inventory: nextInventory, mutation: nextToken, target: target, fullSet: fullSet, revision: revision}
	nextState := nextSuccessorAdmissionState(p.state, successorAdmissionGenerationReady, step)
	nextState.fsIndex = fsResult
	nextState.indexPrefixDigest = prefix.digest
	nextState.indexDigest = verified.digest
	nextState.framedDigest = pre.candidateDigest
	nextState.indexPrefixSize = prefix.size
	nextState.indexSize = verified.size
	nextState.indexRecords = uint64(len(verified.frames))
	nextState.indexTail = pre.activationRecordDigest
	nextState.activatedFrame = cloneProjectionValue(activatedFrame)
	nextState.activatedBytes = activatedBytes
	nextState.activationBytesHash = pre.candidateDigest
	nextState.activationDigest = pre.activationRecordDigest
	next := &SuccessorGenerationReadyPermit{state: nextState}
	next.self = next
	if !sealSuccessorAdmissionState(next, nextState) || !validSuccessorAdmissionState(next, nextState, successorAdmissionGenerationReady, candidate) {
		if nextState.binding != nil {
			successorAdmissionStateRegistry.Delete(nextState.binding)
		}
		return postFailure("-seal")
	}
	result.next = next
	return result, nil
}

func buildSuccessorActivatedFrame(reservedFrame LineageIndexFrame, headerFrame EvidenceFrame) (LineageIndexFrame, []byte, error) {
	if reservedFrame.Validate() != nil || reservedFrame.RecordKind != LineageRecordGenerationReserved || reservedFrame.Record.Reserved == nil || reservedFrame.Sequence == maxJSONInteger || headerFrame.Validate() != nil || headerFrame.Sequence != 0 || headerFrame.PreviousRecordDigest != nil || headerFrame.RecordKind != EvidenceRecordHeader || headerFrame.Record.Header == nil {
		return LineageIndexFrame{}, nil, invalidEvidence("successor-generation-activate", "reserved or header frame is invalid")
	}
	reserved := reservedFrame.Record.Reserved
	if !canonicalEqual(*headerFrame.Record.Header, reserved.PlannedSegment0Header) || headerFrame.RecordDigest != reserved.ExpectedSegment0HeaderDigest {
		return LineageIndexFrame{}, nil, invalidEvidence("successor-generation-activate", "reserved and header frames differ")
	}
	activated := GenerationActivated{
		ExecutionLineageDigest: reserved.ExecutionLineageDigest, JournalIdentityDigest: reserved.JournalIdentityDigest,
		RunnerProjectionDecisionDigest: reserved.RunnerProjectionDecisionDigest, SchemaBundleDigest: reserved.SchemaBundleDigest,
		QuotaReservationDigest: reserved.QuotaReservationDigest, GenerationReservedRecordDigest: reservedFrame.RecordDigest,
		Segment0HeaderDigest: headerFrame.RecordDigest, InitialJournalTailDigest: headerFrame.RecordDigest,
	}
	previous := reservedFrame.RecordDigest
	frame := LineageIndexFrame{
		FormatVersion: LineageFrameFormat, Sequence: reservedFrame.Sequence + 1, PreviousRecordDigest: &previous,
		RecordKind: LineageRecordGenerationActivated, Record: LineageIndexRecord{Activated: &activated},
	}
	var err error
	frame.RecordDigest, err = frame.ComputeDigest()
	if err != nil || frame.Validate() != nil || activated.Validate() != nil {
		return LineageIndexFrame{}, nil, invalidEvidence("successor-generation-activate", "activation frame is invalid")
	}
	raw, err := EncodeCanonicalLineageFrame(frame)
	if err != nil {
		return LineageIndexFrame{}, nil, err
	}
	return cloneProjectionValue(frame), append([]byte(nil), raw...), nil
}

func cloneSuccessorActivationHeader(value ownedActivationHeader) ownedActivationHeader {
	return ownedActivationHeader{
		header: cloneProjectionValue(value.header), generation: value.generation, reserved: cloneProjectionValue(value.reserved),
	}
}

func validateSuccessorJournalInventory(ctx context.Context, inventory *evidencefs.AdmissionInventory, target [32]byte, journalID Digest, headerBytes []byte, expectedCount uint64, present bool, op string) (uint64, error) {
	if inventory == nil || target == ([32]byte{}) || journalID.Validate() != nil || present && len(headerBytes) == 0 {
		return 0, fail(CodeEvidenceRecoveryRequired, op, "successor journal expectation is invalid", nil)
	}
	lineage, lineageErr := inventory.Lineage(target)
	absent, absentErr := inventory.TargetAbsent()
	if lineageErr != nil || absentErr != nil {
		for _, accessorErr := range []error{lineageErr, absentErr} {
			if accessorErr != nil {
				return 0, mapEvidenceAdmissionError(accessorErr, op)
			}
		}
	}
	if absent != nil {
		return 0, admissionCorrupt(op, "successor target became absent", nil)
	}
	journals, err := lineage.Journals()
	if err != nil {
		return 0, mapEvidenceAdmissionError(err, op)
	}
	if expectedCount != 0 && uint64(len(journals)) != expectedCount {
		return 0, admissionCorrupt(op, "successor journal cardinality differs", nil)
	}
	matches := 0
	for _, journal := range journals {
		id, idErr := journal.ID()
		if idErr != nil {
			return 0, mapEvidenceAdmissionError(idErr, op)
		}
		if id != digestRaw(journalID) {
			continue
		}
		matches++
		if !present {
			continue
		}
		segments, segmentErr := journal.Segments()
		if segmentErr != nil {
			return 0, mapEvidenceAdmissionError(segmentErr, op)
		}
		if len(segments) != 1 {
			return 0, admissionCorrupt(op, "successor header segment cardinality differs", nil)
		}
		ordinal, ordinalErr := segments[0].Ordinal()
		size, sizeErr := segments[0].Size()
		digest, digestErr := segments[0].Digest()
		raw, readErr := segments[0].ReadAll(ctx)
		for _, accessorErr := range []error{ordinalErr, sizeErr, digestErr, readErr} {
			if accessorErr != nil {
				return 0, mapEvidenceAdmissionError(accessorErr, op)
			}
		}
		if ordinal != 0 || size != uint64(len(headerBytes)) || digest != sha256.Sum256(headerBytes) || !bytes.Equal(raw, headerBytes) {
			return 0, admissionCorrupt(op, "successor segment-0 differs from planned header", nil)
		}
	}
	if (present && matches != 1) || (!present && matches != 0) {
		return 0, admissionCorrupt(op, "successor journal presence differs", nil)
	}
	if err := inventory.Revalidate(ctx); err != nil {
		return 0, mapEvidenceAdmissionError(err, op+"-terminal-revalidate")
	}
	return uint64(len(journals)), nil
}

func validStoredSuccessorHeaderFacts(state *successorAdmissionState) bool {
	if state == nil || state.plan == nil || state.plan.reservedFrame.Record.Reserved == nil || len(state.headerBytes) == 0 || state.journal.Validate() != nil || state.headerDigest.Validate() != nil || state.journalCount == 0 || state.headerBytesHash != sha256.Sum256(state.headerBytes) || state.fsJournalCandidate == ([32]byte{}) || state.fsJournalCandidate != state.fsJournal.CandidateDigest() {
		return false
	}
	reserved := state.plan.reservedFrame.Record.Reserved
	generation := generationIdentity{state.runtimeReceipt.owner, reserved.ExecutionLineageDigest, reserved.JournalIdentityDigest, reserved.RunnerProjectionDecisionDigest, reserved.SchemaBundleDigest}
	wantHeader, bindErr := bindActivationHeader(generation, *reserved, state.runtimeReceipt, state.recoveryReceipt)
	wantFrame, wantBytes, encodeErr := encodeAdmissionActivationHeader(wantHeader)
	if bindErr != nil || encodeErr != nil || !sameGenerationIdentity(state.activationHeader.generation, generation) || !canonicalEqual(state.activationHeader.header, wantHeader.header) || !canonicalEqual(state.activationHeader.reserved, wantHeader.reserved) || !canonicalEqual(state.headerFrame, wantFrame) || !canonicalEqual(state.headerFrame, state.plan.headerFrame) || !bytes.Equal(state.headerBytes, wantBytes) || state.headerDigest != wantFrame.RecordDigest || state.journal != reserved.JournalIdentityDigest {
		return false
	}
	expected := state
	if state.stage == successorAdmissionGenerationReady {
		expected = state.prior
	}
	return expected != nil && state.fsJournal.Outcome() == evidencefs.AdmissionTransitionDurable && state.fsJournal.Inventory() == expected.inventory && state.fsJournal.CandidateKind() == "generation_header" && state.fsJournal.CandidateSequence() == 0 && state.fsJournal.PreviousRevision()+1 == expected.revision && state.fsJournal.CandidateRevision() == expected.revision && state.fsJournal.Journal() == digestRaw(state.journal) && state.fsJournal.HeaderDigest() == state.headerBytesHash && state.fsJournal.HeaderSize() == uint64(len(state.headerBytes))
}

func validStoredSuccessorActivationFacts(state *successorAdmissionState) bool {
	if state == nil || len(state.activatedBytes) == 0 || state.activationDigest.Validate() != nil || state.activationBytesHash != sha256.Sum256(state.activatedBytes) || state.activationDigest != state.activatedFrame.RecordDigest {
		return false
	}
	wantFrame, wantBytes, err := buildSuccessorActivatedFrame(state.plan.reservedFrame, state.headerFrame)
	return err == nil && canonicalEqual(state.activatedFrame, wantFrame) && bytes.Equal(state.activatedBytes, wantBytes)
}

func validSuccessorInventoryHeader(state *successorAdmissionState) bool {
	if state == nil || state.inventory == nil || state.journal.Validate() != nil || state.headerBytesHash == ([32]byte{}) || state.journalCount == 0 || !validSuccessorInventoryIndex(state) {
		return false
	}
	lineage, err := state.inventory.Lineage(state.target)
	if err != nil {
		return false
	}
	journals, err := lineage.Journals()
	if err != nil || uint64(len(journals)) != state.journalCount {
		return false
	}
	found := false
	for _, journal := range journals {
		id, idErr := journal.ID()
		if idErr != nil {
			return false
		}
		if id != digestRaw(state.journal) {
			continue
		}
		if found {
			return false
		}
		found = true
		segments, segmentErr := journal.Segments()
		if segmentErr != nil || len(segments) != 1 {
			return false
		}
		ordinal, ordinalErr := segments[0].Ordinal()
		size, sizeErr := segments[0].Size()
		digest, digestErr := segments[0].Digest()
		if ordinalErr != nil || sizeErr != nil || digestErr != nil || ordinal != 0 || size != uint64(len(state.headerBytes)) || digest != state.headerBytesHash {
			return false
		}
	}
	return found
}

func successorGenerationHeaderUnknown(value SuccessorGenerationHeaderTransitionResult) SuccessorGenerationHeaderTransitionResult {
	value.outcome = evidencefs.AdmissionTransitionUnknown
	value.next = nil
	return value
}

func successorGenerationActivationUnknown(value SuccessorGenerationActivationTransitionResult) SuccessorGenerationActivationTransitionResult {
	value.outcome = evidencefs.AdmissionTransitionUnknown
	value.next = nil
	return value
}
