package migration

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"sort"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/evidencefs"
)

const admissionReplayDigestDomain = "cloud-agents-platform-evidence-admission-replay/v2\x00"

// admissionReplayTranscript is an owned description of a revision-zero
// evidencefs inventory. It deliberately has no seal, self pointer, registry, or
// authority consumer. In particular, it cannot authorize recovery or execution.
type admissionReplayTranscript struct {
	revision             uint64
	fullSetDigest        [32]byte
	target               [32]byte
	targetAbsent         bool
	lineages             []admissionReplayLineage
	objects              []admissionReplayObject
	recoveryNeeds        []admissionRecoveryNeed
	references           []admissionReplayReference
	journalBytes         uint64
	journalRecords       uint64
	journalReservedBytes uint64
	indexBytes           uint64
	indexRecords         uint64
	indexReservedBytes   uint64
	canonical            [32]byte
}

type admissionReplayLineage struct {
	id                                             [32]byte
	index                                          admissionReplayFile
	indexRecords                                   uint64
	indexHeaderFramedBytes                         uint64
	journals                                       []admissionReplayJournal
	state                                          admissionReplayLineageState
	generations                                    []admissionReplayGeneration
	header                                         admissionReplayLineageHeader
	indexHeaderRecordDigest, indexTailRecordDigest Digest
}
type admissionReplayLineageHeader struct {
	executionLineageDigest                                        Digest
	deploymentID, databaseName, repositoryIdentity, limitsProfile string
}

type admissionReplayGeneration struct {
	journalID, reservedRecordDigest, runnerProjectionDecisionDigest, schemaBundleDigest, quotaReservationDigest Digest
	reservedRecords, reservedBytes                                                                              uint64
	reservedSegments                                                                                            uint32
	expectedSegment0HeaderDigest                                                                                Digest
	activationRecordDigest, latestCheckpointRecordDigest, supersessionRecordDigest                              *Digest
	latestCheckpointTailDigest                                                                                  *Digest
	latestCheckpointNext                                                                                        uint64
	previousCheckpointRecordDigest                                                                              *Digest
	latestRecoveryState, supersessionOutcome                                                                    string
	indexDebits                                                                                                 []admissionReplayIndexDebit
	header                                                                                                      *admissionReplayHeaderFacts
	continuation                                                                                                *admissionReplayContinuation
	summary                                                                                                     *evidenceJournalSummary
	latestCheckpointSummary                                                                                     *evidenceJournalSummary
	plannedSuccessor                                                                                            *admissionReplayGeneration
	currentTail                                                                                                 *admissionReplayRecoveryTail
	verificationTerminals                                                                                       []admissionReplayTerminalEvent
	verificationFinals                                                                                          []admissionReplayTerminalFinal
	verificationCommits                                                                                         []admissionReplayTerminalCommit
	verificationRetries                                                                                         []admissionReplayTerminalRetry
	verificationResolutions                                                                                     []admissionReplayTerminalResolution
	verificationOpen                                                                                            *admissionReplayOpenAttempt
	verificationCatalogContract                                                                                 [32]byte
	runtimeInspection                                                                                           *admissionReplayRuntimeInspection
	remainingIndexRecords, remainingIndexBytes                                                                  uint64
	indexHeaderDebited                                                                                          bool
	supersessionAuthorityDigest                                                                                 Digest
	oldCheckpointRecordDigest, oldActivationRecordDigest, oldInitialJournalTailDigest                           *Digest
}

type admissionReplayRecoveryRecord[T any] struct {
	sequence             uint64
	previousRecordDigest *Digest
	recordDigest         Digest
	bodyDigest           Digest
	body                 T
}
type admissionReplayRecoveryTail struct {
	migrationID  string
	attemptIndex uint32
	intent       *admissionReplayRecoveryRecord[StatementIntent]
	intermediate *admissionReplayRecoveryRecord[StatementIntermediateEvidence]
	commit       *admissionReplayRecoveryRecord[CommitIntent]
	terminal     *admissionReplayRecoveryRecord[AttemptTerminalState]
	resolution   *admissionReplayRecoveryRecord[AmbiguousResolutionState]
}
type admissionReplayTerminalEvent struct {
	migrationID, attemptIndex, statementCount, lastStatementIndex uint32
	outcome, resolutionOutcome                                    uint8
	flags                                                         uint16
	terminalDigest, statementChain                                [32]byte
}

const (
	admissionTerminalHasFinal uint16 = 1 << iota
	admissionTerminalHasCommit
	admissionTerminalHasRetry
	admissionTerminalHasResolution
	admissionTerminalHasStatements
)

type admissionReplayTerminalFinal struct {
	ordinal                                  uint32
	lastIntermediateRecord, preledgerCatalog [32]byte
}
type admissionReplayTerminalCommit struct {
	ordinal                   uint32
	expectedLedgerLength      uint32
	commitRecord, commitBody  [32]byte
	previousAttemptTerminal   [32]byte
	attemptPredecessorCatalog [32]byte
	lastIntermediateState     [32]byte
}
type admissionReplayTerminalRetry struct {
	ordinal                                                                   uint32
	proofKind, commitRejectedReason                                           uint8
	attemptPredecessorCatalog, observedCatalog, ledgerPrefix, authorityResult [32]byte
}
type admissionReplayTerminalResolution struct {
	ordinal          uint32
	resolutionDigest [32]byte
}
type admissionReplayOpenAttempt struct {
	migrationID, attemptIndex, statementCount, lastStatementIndex uint32
	statementChain                                                [32]byte
	commitPresent                                                 bool
	commitRecord, commitBody, previousAttemptTerminal             [32]byte
	attemptPredecessorCatalog, lastIntermediateState              [32]byte
	expectedLedgerLength                                          uint32
}
type admissionReplayJournalCollector struct {
	terminals       []admissionReplayTerminalEvent
	finals          []admissionReplayTerminalFinal
	commits         []admissionReplayTerminalCommit
	retries         []admissionReplayTerminalRetry
	resolutions     []admissionReplayTerminalResolution
	active          *admissionReplayAttemptKey
	frontier        admissionReplayAttemptFrontier
	closed          *admissionReplayClosedAttempt
	initial         *admissionReplayContinuation
	tailKey         admissionReplayAttemptKey
	catalogContract [32]byte
}
type admissionReplayClosedAttempt struct {
	key               admissionReplayAttemptKey
	terminalDigest    Digest
	outcome           string
	resolutionOutcome string
	sequence          uint64
	recordDigest      Digest
	ordinal           uint32
}
type admissionReplayAttemptKey struct {
	migrationID  string
	attemptIndex uint32
}
type admissionReplayAttemptFrontier struct {
	statementCount, lastStatementIndex            uint32
	statementChain                                [32]byte
	lastIntermediateRecord, lastIntermediateState [32]byte
	preledgerCatalog                              [32]byte
	commitRecord, commitBody, commitPrevious      [32]byte
	commitPredecessor                             [32]byte
	expectedLedgerLength                          uint32
}

func admissionCanonicalSubject(domain string, value any) ([32]byte, error) {
	canonical, err := canonicalContractKey(value)
	if err != nil {
		return [32]byte{}, err
	}
	return sha256.Sum256([]byte("admission-verification/" + domain + "\x00" + canonical)), nil
}

func (c *admissionReplayJournalCollector) observe(frame EvidenceFrame) error {
	migration, attempt := structuralRecordAttempt(frame.Record)
	key := admissionReplayAttemptKey{migration, attempt}
	c.tailKey = key
	catalog := admissionRecordCatalogContract(frame.Record)
	if catalog == ([32]byte{}) {
		return admissionCorrupt("admission-verification-events", "catalog contract binding is invalid", nil)
	}
	if c.catalogContract == ([32]byte{}) {
		c.catalogContract = catalog
	} else if c.catalogContract != catalog {
		return admissionCorrupt("admission-verification-events", "catalog contract binding drifted within journal", nil)
	}
	switch frame.RecordKind {
	case EvidenceRecordStatementIntent:
		v := frame.Record.StatementIntent
		if c.active == nil {
			if err := c.beginAttempt(key, v.PreviousAttemptTerminalDigest); err != nil {
				return err
			}
		} else if *c.active != key {
			return admissionCorrupt("admission-verification-events", "attempts are not serial", nil)
		}
		subject, err := admissionStatementPlanSubject(*v)
		if err != nil {
			return err
		}
		c.frontier.statementChain = admissionStatementChainStep(c.frontier.statementChain, v.MigrationID, v.AttemptIndex, v.StatementIndex, subject)
		c.frontier.statementCount++
		c.frontier.lastStatementIndex = v.StatementIndex
	case EvidenceRecordIntermediate:
		v := frame.Record.Intermediate
		if c.active == nil || *c.active != key {
			return admissionCorrupt("admission-verification-events", "intermediate has no active attempt", nil)
		}
		c.frontier.lastIntermediateRecord = digestRaw(frame.RecordDigest)
		c.frontier.lastIntermediateState = digestRaw(v.State.IntermediateStateDigest)
		if v.PreledgerCatalogResult != nil {
			c.frontier.preledgerCatalog = digestRaw(v.PreledgerCatalogResult.Digest)
		}
	case EvidenceRecordCommitIntent:
		v := frame.Record.CommitIntent
		if c.active == nil || *c.active != key {
			return admissionCorrupt("admission-verification-events", "commit has no active attempt", nil)
		}
		c.frontier.commitRecord = digestRaw(frame.RecordDigest)
		var err error
		c.frontier.commitBody, err = admissionCommitSubject(*v)
		if err != nil {
			return err
		}
		if v.PreviousAttemptTerminalDigest != nil {
			c.frontier.commitPrevious = digestRaw(*v.PreviousAttemptTerminalDigest)
		}
		c.frontier.commitPredecessor = digestRaw(v.AttemptPredecessorCatalogDigest)
		c.frontier.expectedLedgerLength = v.ExpectedLedgerLength
	case EvidenceRecordAttemptTerminal:
		v := frame.Record.AttemptTerminal
		if c.active == nil {
			if err := c.beginAttempt(key, v.PreviousAttemptTerminalDigest); err != nil {
				return err
			}
		}
		if *c.active != key {
			return admissionCorrupt("admission-verification-events", "terminal has no active attempt", nil)
		}
		frontier := c.frontier
		migrationID, err := admissionMigrationNumber(v.MigrationID)
		if err != nil {
			return err
		}
		outcome, err := admissionTerminalOutcomeCode(v.Outcome)
		if err != nil {
			return err
		}
		e := admissionReplayTerminalEvent{migrationID: migrationID, attemptIndex: v.AttemptIndex, statementCount: frontier.statementCount, lastStatementIndex: frontier.lastStatementIndex, outcome: outcome, terminalDigest: digestRaw(v.TerminalDigest), statementChain: frontier.statementChain}
		if frontier.statementCount != 0 {
			e.flags |= admissionTerminalHasStatements
		}
		ordinal := uint32(len(c.terminals))
		requiresFinal := v.Outcome == "committed" || len(v.Outcome) >= 10 && v.Outcome[:10] == "ambiguous_"
		if requiresFinal {
			e.flags |= admissionTerminalHasFinal
			c.finals = append(c.finals, admissionReplayTerminalFinal{ordinal: ordinal, lastIntermediateRecord: frontier.lastIntermediateRecord, preledgerCatalog: frontier.preledgerCatalog})
		}
		if frontier.commitRecord != ([32]byte{}) {
			e.flags |= admissionTerminalHasCommit
			c.commits = append(c.commits, admissionReplayTerminalCommit{ordinal: ordinal, expectedLedgerLength: frontier.expectedLedgerLength, commitRecord: frontier.commitRecord, commitBody: frontier.commitBody, previousAttemptTerminal: frontier.commitPrevious, attemptPredecessorCatalog: frontier.commitPredecessor, lastIntermediateState: frontier.lastIntermediateState})
		}
		if v.RetryProof != nil {
			proofKind, reason, subjectErr := admissionRetryProofCodes(*v.RetryProof)
			if subjectErr != nil {
				return subjectErr
			}
			e.flags |= admissionTerminalHasRetry
			c.retries = append(c.retries, admissionReplayTerminalRetry{ordinal: ordinal, proofKind: proofKind, commitRejectedReason: reason, attemptPredecessorCatalog: digestRaw(v.RetryProof.AttemptPredecessorCatalogDigest), observedCatalog: digestRaw(v.RetryProof.ObservedCatalogDigest), ledgerPrefix: digestRaw(v.RetryProof.LedgerPrefixDigest), authorityResult: digestRaw(v.RetryProof.AuthorityResultDigest)})
		}
		c.terminals = append(c.terminals, e)
		closedKey := key
		c.closed = &admissionReplayClosedAttempt{key: closedKey, terminalDigest: v.TerminalDigest, outcome: v.Outcome, sequence: frame.Sequence, recordDigest: frame.RecordDigest, ordinal: ordinal}
		c.active = nil
		c.frontier = admissionReplayAttemptFrontier{}
	case EvidenceRecordAmbiguousResolution:
		v := frame.Record.AmbiguousResolution
		if c.closed == nil || c.closed.terminalDigest != v.UnresolvedTerminalDigest || frame.Sequence != c.closed.sequence+1 || frame.PreviousRecordDigest == nil || *frame.PreviousRecordDigest != c.closed.recordDigest || int(c.closed.ordinal) >= len(c.terminals) {
			return admissionCorrupt("admission-verification-events", "resolution does not close current attempt", nil)
		}
		outcome, err := admissionResolutionOutcomeCode(v.Outcome)
		if err != nil {
			return err
		}
		e := &c.terminals[c.closed.ordinal]
		e.flags |= admissionTerminalHasResolution
		e.resolutionOutcome = outcome
		c.resolutions = append(c.resolutions, admissionReplayTerminalResolution{ordinal: c.closed.ordinal, resolutionDigest: digestRaw(v.ResolutionDigest)})
		c.closed.resolutionOutcome = v.Outcome
	}
	return nil
}

func admissionRecordCatalogContract(record EvidenceRecord) [32]byte {
	var digest Digest
	switch {
	case record.StatementIntent != nil:
		digest = record.StatementIntent.CatalogContractDigest
	case record.Intermediate != nil:
		digest = record.Intermediate.State.CatalogContractDigest
	case record.CommitIntent != nil:
		digest = record.CommitIntent.CatalogContractDigest
	case record.AttemptTerminal != nil:
		digest = record.AttemptTerminal.CatalogContractDigest
	case record.AmbiguousResolution != nil:
		digest = record.AmbiguousResolution.CatalogContractDigest
	}
	return digestRaw(digest)
}

func (c *admissionReplayJournalCollector) beginAttempt(key admissionReplayAttemptKey, previous *Digest) error {
	if c.closed == nil {
		if c.initial != nil {
			initial := c.initial
			valid := key.migrationID == initial.migrationID
			switch initial.startAction {
			case "begin_first_attempt_next_entry":
				valid = valid && key.attemptIndex == 1 && previous == nil
			case "begin_next_attempt":
				valid = valid && key.attemptIndex == initial.attemptIndex && previous != nil && initial.previousAttemptTerminalDigest != nil && *previous == *initial.previousAttemptTerminalDigest && *previous == initial.sourceTerminalDigest
			default:
				valid = false
			}
			if !valid {
				return admissionCorrupt("admission-verification-events", "successor attempt identity is invalid", nil)
			}
			c.initial = nil
		} else if key.attemptIndex != 1 || previous != nil {
			return admissionCorrupt("admission-verification-events", "first attempt identity is invalid", nil)
		}
	} else if key.migrationID == c.closed.key.migrationID {
		if key.attemptIndex != c.closed.key.attemptIndex+1 || previous == nil || *previous != c.closed.terminalDigest || !stringIn(c.closed.outcome, "aborted_retryable", "ambiguous_reconciled_pending") && c.closed.resolutionOutcome != "resolved_pending" {
			return admissionCorrupt("admission-verification-events", "next attempt predecessor is invalid", nil)
		}
	} else if key.attemptIndex != 1 || previous != nil || !stringIn(c.closed.outcome, "committed", "ambiguous_reconciled_committed") && c.closed.resolutionOutcome != "resolved_committed" {
		return admissionCorrupt("admission-verification-events", "next migration transition is invalid", nil)
	}
	active := key
	c.active = &active
	c.frontier = admissionReplayAttemptFrontier{}
	return nil
}

func admissionMigrationNumber(value string) (uint32, error) {
	if !migrationIDPattern.MatchString(value) {
		return 0, admissionCorrupt("admission-verification-events", "migration identity is invalid", nil)
	}
	var result uint32
	for index := range value {
		result = result*10 + uint32(value[index]-'0')
	}
	return result, nil
}

func admissionTerminalOutcomeCode(value string) (uint8, error) {
	for index, candidate := range []string{"committed", "aborted_retryable", "aborted_terminal", "ambiguous_reconciled_committed", "ambiguous_reconciled_pending", "ambiguous_divergent", "ambiguous_unresolved"} {
		if value == candidate {
			return uint8(index + 1), nil
		}
	}
	return 0, admissionCorrupt("admission-verification-events", "terminal outcome is invalid", nil)
}

func admissionResolutionOutcomeCode(value string) (uint8, error) {
	for index, candidate := range []string{"resolved_committed", "resolved_pending", "resolved_divergent"} {
		if value == candidate {
			return uint8(index + 1), nil
		}
	}
	return 0, admissionCorrupt("admission-verification-events", "resolution outcome is invalid", nil)
}

func admissionRetryProofCodes(value RetryProofEvidence) (uint8, uint8, error) {
	var kind uint8
	for index, candidate := range []string{"projection_transient_exact_predecessor", "precommit_rollback_exact_predecessor", "precommit_connection_terminated_exact_predecessor", "commit_rejected_exact_predecessor"} {
		if value.ProofKind == candidate {
			kind = uint8(index + 1)
			break
		}
	}
	if kind == 0 {
		return 0, 0, admissionCorrupt("admission-verification-events", "retry proof kind is invalid", nil)
	}
	if value.CommitRejectedReason == nil {
		return kind, 0, nil
	}
	for index, candidate := range []string{"serialization_failure", "deadlock_detected", "other_confirmed_postgres_error"} {
		if *value.CommitRejectedReason == candidate {
			return kind, uint8(index + 1), nil
		}
	}
	return 0, 0, admissionCorrupt("admission-verification-events", "retry rejection reason is invalid", nil)
}

func admissionStatementPlanSubject(v StatementIntent) ([32]byte, error) {
	classification, err := canonicalContractKey(v.Classification)
	if err != nil {
		return [32]byte{}, err
	}
	h := sha256.New()
	h.Write([]byte("admission-statement-plan-subject/v1\x00"))
	for _, d := range []Digest{v.SQLArtifactSHA256, v.StatementSHA256, v.ExpectedTransitionDigest} {
		raw := digestRaw(d)
		h.Write(raw[:])
	}
	writeAdmissionUint(h, v.SQLArtifactSizeBytes)
	writeAdmissionUint(h, v.StartOffset)
	writeAdmissionUint(h, v.EndOffset)
	writeAdmissionString(h, classification)
	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result, nil
}

func admissionStatementChainStep(previous [32]byte, migration string, attempt, statement uint32, subject [32]byte) [32]byte {
	h := sha256.New()
	h.Write([]byte("admission-statement-chain/v1\x00"))
	h.Write(previous[:])
	writeAdmissionString(h, migration)
	writeAdmissionUint(h, uint64(attempt))
	writeAdmissionUint(h, uint64(statement))
	h.Write(subject[:])
	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result
}

func admissionCommitSubject(v CommitIntent) ([32]byte, error) {
	return admissionCanonicalSubject("commit-intent", v)
}

func (t *admissionReplayRecoveryTail) observe(frame EvidenceFrame) error {
	branch, err := frame.Record.branch()
	if err != nil {
		return err
	}
	canonical, err := canonicalContractKey(branch)
	if err != nil {
		return err
	}
	bodyDigest := DigestBytes([]byte(canonical))
	migration, attempt := structuralRecordAttempt(frame.Record)
	if t.attemptIndex != 0 && (t.migrationID != migration || t.attemptIndex != attempt) {
		*t = admissionReplayRecoveryTail{}
	}
	t.migrationID, t.attemptIndex = migration, attempt
	switch frame.RecordKind {
	case EvidenceRecordStatementIntent:
		body := cloneProjectionValue(*frame.Record.StatementIntent)
		t.intent = &admissionReplayRecoveryRecord[StatementIntent]{frame.Sequence, cloneDigestPointer(frame.PreviousRecordDigest), frame.RecordDigest, bodyDigest, body}
		t.intermediate = nil
		t.commit = nil
		t.terminal = nil
		t.resolution = nil
	case EvidenceRecordIntermediate:
		body := cloneProjectionValue(*frame.Record.Intermediate)
		t.intermediate = &admissionReplayRecoveryRecord[StatementIntermediateEvidence]{frame.Sequence, cloneDigestPointer(frame.PreviousRecordDigest), frame.RecordDigest, bodyDigest, body}
		t.commit = nil
		t.terminal = nil
		t.resolution = nil
	case EvidenceRecordCommitIntent:
		body := cloneProjectionValue(*frame.Record.CommitIntent)
		t.commit = &admissionReplayRecoveryRecord[CommitIntent]{frame.Sequence, cloneDigestPointer(frame.PreviousRecordDigest), frame.RecordDigest, bodyDigest, body}
		t.terminal = nil
		t.resolution = nil
	case EvidenceRecordAttemptTerminal:
		body := cloneProjectionValue(*frame.Record.AttemptTerminal)
		t.terminal = &admissionReplayRecoveryRecord[AttemptTerminalState]{frame.Sequence, cloneDigestPointer(frame.PreviousRecordDigest), frame.RecordDigest, bodyDigest, body}
		t.resolution = nil
	case EvidenceRecordAmbiguousResolution:
		body := cloneProjectionValue(*frame.Record.AmbiguousResolution)
		t.resolution = &admissionReplayRecoveryRecord[AmbiguousResolutionState]{frame.Sequence, cloneDigestPointer(frame.PreviousRecordDigest), frame.RecordDigest, bodyDigest, body}
	}
	return nil
}

func validateAdmissionRecoveryRecord[T any](record *admissionReplayRecoveryRecord[T], kind EvidenceRecordKind, wrap func(*T) EvidenceRecord) error {
	if record == nil {
		return nil
	}
	canonical, err := canonicalContractKey(record.body)
	if err != nil || DigestBytes([]byte(canonical)) != record.bodyDigest {
		return admissionCorrupt("admission-recovery-tail", "typed body digest mismatch", err)
	}
	body := cloneProjectionValue(record.body)
	frame := EvidenceFrame{FormatVersion: EvidenceFrameFormat, Sequence: record.sequence, PreviousRecordDigest: cloneDigestPointer(record.previousRecordDigest), RecordKind: kind, Record: wrap(&body), RecordDigest: record.recordDigest}
	want, err := frame.ComputeDigest()
	if err != nil || want != record.recordDigest || frame.Validate() != nil {
		return admissionCorrupt("admission-recovery-tail", "typed frame binding mismatch", err)
	}
	return nil
}
func validateAdmissionRecoveryTail(t *admissionReplayRecoveryTail) error {
	if t == nil {
		return nil
	}
	checks := []error{
		validateAdmissionRecoveryRecord(t.intent, EvidenceRecordStatementIntent, func(v *StatementIntent) EvidenceRecord { return EvidenceRecord{StatementIntent: v} }),
		validateAdmissionRecoveryRecord(t.intermediate, EvidenceRecordIntermediate, func(v *StatementIntermediateEvidence) EvidenceRecord { return EvidenceRecord{Intermediate: v} }),
		validateAdmissionRecoveryRecord(t.commit, EvidenceRecordCommitIntent, func(v *CommitIntent) EvidenceRecord { return EvidenceRecord{CommitIntent: v} }),
		validateAdmissionRecoveryRecord(t.terminal, EvidenceRecordAttemptTerminal, func(v *AttemptTerminalState) EvidenceRecord { return EvidenceRecord{AttemptTerminal: v} }),
		validateAdmissionRecoveryRecord(t.resolution, EvidenceRecordAmbiguousResolution, func(v *AmbiguousResolutionState) EvidenceRecord { return EvidenceRecord{AmbiguousResolution: v} }),
	}
	for _, err := range checks {
		if err != nil {
			return err
		}
	}
	return nil
}

type admissionReplayIndexDebit struct {
	kind         LineageRecordKind
	recordDigest Digest
	framedBytes  uint64
}
type admissionReplayHeaderFacts struct {
	journalIdentityDigest, releaseTrustDecisionDigest, runnerProjectionDecisionDigest, executionLineageDigest                       Digest
	outerArtifactDigest                                                                                                             Digest
	outerArtifactSize                                                                                                               uint64
	recoveryArtifactDigest                                                                                                          Digest
	recoveryArtifactSize                                                                                                            uint64
	manifestDigest, runnerReleaseDigest, schemaBundleDigest, authorityProfileDigest, authorityBindingDigest, quotaReservationDigest Digest
	reservedRecords, reservedBytes                                                                                                  uint64
	reservedSegments                                                                                                                uint32
}
type admissionReplayContinuation struct {
	startAction, migrationID                                                        string
	attemptIndex                                                                    uint32
	previousAttemptTerminalDigest                                                   *Digest
	sourceJournalIdentityDigest, sourceCheckpointRecordDigest, sourceTerminalDigest Digest
}

func compactAdmissionGenerations(frames []LineageIndexFrame) ([]admissionReplayGeneration, error) {
	var result []admissionReplayGeneration
	for index := range frames {
		frame := frames[index]
		canonical, err := canonicalContractKey(frame)
		if err != nil {
			return nil, err
		}
		debit := admissionReplayIndexDebit{frame.RecordKind, frame.RecordDigest, uint64(len(canonical)) + 8}
		switch frame.RecordKind {
		case LineageRecordGenerationReserved:
			r := frame.Record.Reserved
			generation := compactAdmissionReserved(*r, frame.RecordDigest)
			generation.indexDebits = []admissionReplayIndexDebit{debit}
			result = append(result, generation)
		case LineageRecordGenerationActivated:
			if len(result) != 0 {
				d := frame.RecordDigest
				result[len(result)-1].activationRecordDigest = &d
				result[len(result)-1].indexDebits = append(result[len(result)-1].indexDebits, debit)
			}
		case LineageRecordGenerationCheckpoint:
			if len(result) != 0 {
				d := frame.RecordDigest
				c := frame.Record.Checkpoint
				result[len(result)-1].latestCheckpointRecordDigest = &d
				tail := c.JournalTailDigest
				result[len(result)-1].latestCheckpointTailDigest = &tail
				result[len(result)-1].latestCheckpointNext = c.JournalNextSequence
				result[len(result)-1].latestRecoveryState = c.RecoveryState
				summary := summaryFromCheckpoint(*c)
				result[len(result)-1].latestCheckpointSummary = &summary
				result[len(result)-1].previousCheckpointRecordDigest = cloneDigestPointer(c.PreviousCheckpointRecordDigest)
				result[len(result)-1].indexDebits = append(result[len(result)-1].indexDebits, debit)
			}
		case LineageRecordGenerationSuperseded:
			if len(result) != 0 {
				d := frame.RecordDigest
				result[len(result)-1].supersessionRecordDigest = &d
				result[len(result)-1].supersessionOutcome = frame.Record.Superseded.Outcome
				s := frame.Record.Superseded
				result[len(result)-1].supersessionAuthorityDigest = s.LineageSupersessionAuthorityDigest
				result[len(result)-1].oldCheckpointRecordDigest = cloneDigestPointer(s.OldCheckpointRecordDigest)
				result[len(result)-1].oldActivationRecordDigest = cloneDigestPointer(s.OldActivationRecordDigest)
				result[len(result)-1].oldInitialJournalTailDigest = cloneDigestPointer(s.OldInitialJournalTailDigest)
				result[len(result)-1].indexDebits = append(result[len(result)-1].indexDebits, debit)
				if planned := frame.Record.Superseded.PlannedGenerationReserved; planned != nil {
					successor := compactAdmissionReserved(*planned, "")
					result[len(result)-1].plannedSuccessor = &successor
				}
			}
		}
	}
	return result, nil
}

func compactAdmissionReserved(r GenerationReserved, recordDigest Digest) admissionReplayGeneration {
	generation := admissionReplayGeneration{journalID: r.JournalIdentityDigest, reservedRecordDigest: recordDigest, runnerProjectionDecisionDigest: r.RunnerProjectionDecisionDigest, schemaBundleDigest: r.SchemaBundleDigest, quotaReservationDigest: r.QuotaReservationDigest, reservedRecords: r.ReservedRecords, reservedBytes: r.ReservedBytes, reservedSegments: r.ReservedSegments, expectedSegment0HeaderDigest: r.ExpectedSegment0HeaderDigest}
	header := compactAdmissionHeaderFacts(r.PlannedSegment0Header)
	generation.header = &header
	if r.Continuation != nil {
		c := r.Continuation
		generation.continuation = &admissionReplayContinuation{c.StartAction, c.MigrationID, c.AttemptIndex, cloneDigestPointer(c.PreviousAttemptTerminalDigest), c.SourceJournalIdentityDigest, c.SourceCheckpointRecordDigest, c.SourceTerminalDigest}
	}
	return generation
}

func expandAdmissionHeaderFacts(h admissionReplayHeaderFacts) (JournalHeader, error) {
	result := JournalHeader{
		FormatVersion: EvidenceJournalFormat, JournalIdentityDigest: h.journalIdentityDigest, ReleaseTrustDecisionDigest: h.releaseTrustDecisionDigest,
		RunnerProjectionDecisionDigest: h.runnerProjectionDecisionDigest, ExecutionLineageDigest: h.executionLineageDigest,
		OuterArtifactDigest: h.outerArtifactDigest, OuterArtifactSizeBytes: h.outerArtifactSize, DecisionRecoveryArtifactSHA256: h.recoveryArtifactDigest,
		DecisionRecoveryArtifactSizeBytes: h.recoveryArtifactSize, ManifestDigest: h.manifestDigest, RunnerReleaseDigest: h.runnerReleaseDigest,
		SchemaBundleDigest: h.schemaBundleDigest, AuthorityProfileDigest: h.authorityProfileDigest, AuthorityBindingDigest: h.authorityBindingDigest,
		LimitsProfile: EvidenceLimitsProfile, QuotaReservationDigest: h.quotaReservationDigest, ReservedRecords: h.reservedRecords,
		ReservedBytes: h.reservedBytes, ReservedSegments: h.reservedSegments,
	}
	if err := result.Validate(); err != nil {
		return JournalHeader{}, admissionCorrupt("admission-generation", "compact journal header is invalid", err)
	}
	return result, nil
}

func expandAdmissionGenerationReserved(lineage [32]byte, g admissionReplayGeneration) (GenerationReserved, error) {
	if g.header == nil || digestString(lineage) != g.header.executionLineageDigest || g.header.journalIdentityDigest != g.journalID || g.header.runnerProjectionDecisionDigest != g.runnerProjectionDecisionDigest || g.header.schemaBundleDigest != g.schemaBundleDigest || g.header.quotaReservationDigest != g.quotaReservationDigest || g.header.reservedRecords != g.reservedRecords || g.header.reservedBytes != g.reservedBytes || g.header.reservedSegments != g.reservedSegments {
		return GenerationReserved{}, admissionCorrupt("admission-generation", "compact generation and header differ", nil)
	}
	header, err := expandAdmissionHeaderFacts(*g.header)
	if err != nil {
		return GenerationReserved{}, err
	}
	result := GenerationReserved{
		ExecutionLineageDigest: digestString(lineage), JournalIdentityDigest: g.journalID, RunnerProjectionDecisionDigest: g.runnerProjectionDecisionDigest,
		SchemaBundleDigest: g.schemaBundleDigest, QuotaReservationDigest: g.quotaReservationDigest, ReservedRecords: g.reservedRecords,
		ReservedBytes: g.reservedBytes, ReservedSegments: g.reservedSegments, PlannedSegment0Header: header,
		ExpectedSegment0HeaderDigest: g.expectedSegment0HeaderDigest,
	}
	if g.continuation != nil {
		c := g.continuation
		result.Continuation = &LineageContinuationContext{c.startAction, c.migrationID, c.attemptIndex, cloneDigestPointer(c.previousAttemptTerminalDigest), c.sourceJournalIdentityDigest, c.sourceCheckpointRecordDigest, c.sourceTerminalDigest}
	}
	if err := result.Validate(); err != nil {
		return GenerationReserved{}, admissionCorrupt("admission-generation", "compact generation reservation is invalid", err)
	}
	return result, nil
}

type admissionReplayJournal struct {
	id       [32]byte
	segments []admissionReplaySegment
	records  uint64
	tail     Digest
}

type admissionReplaySegment struct {
	file    admissionReplayFile
	records uint64
}

type admissionReplayFile struct {
	ordinal  uint32
	size     uint64
	digest   [32]byte
	identity [32]byte
}

type admissionReplayObject struct {
	temporary bool
	digest    Digest
	size      uint64
	identity  [32]byte
}

type admissionRecoveryNeed struct {
	kind      durableContentObjectKind
	digest    Digest
	sizeBytes uint64
}

type admissionReplayLineageState string

const (
	admissionLineageEmpty                  admissionReplayLineageState = "empty"
	admissionLineageReservedUnregistered   admissionReplayLineageState = "reserved_unregistered"
	admissionLineageReservedHeader         admissionReplayLineageState = "reserved_header_unactivated"
	admissionLineageActiveInitial          admissionReplayLineageState = "active_initial"
	admissionLineageActiveCheckpointed     admissionReplayLineageState = "active_checkpointed"
	admissionLineageActiveUnknownExtension admissionReplayLineageState = "active_unknown_extension"
	admissionLineageSuperseded             admissionReplayLineageState = "superseded"
)

type admissionObjectReference struct {
	lineageID    [32]byte
	journalID    [32]byte
	headerDigest Digest
	kind         durableContentObjectKind
	digest       Digest
	sizeBytes    uint64
	decision     Digest
	manifest     Digest
	schema       Digest
	records      uint64
	bytes        uint64
	segments     uint32
}

type admissionObjectReferenceKey struct {
	lineageID, journalID [32]byte
	headerDigest         Digest
	kind                 durableContentObjectKind
	digest               Digest
	sizeBytes            uint64
}

func (ref admissionObjectReference) identityKey() admissionObjectReferenceKey {
	return admissionObjectReferenceKey{ref.lineageID, ref.journalID, ref.headerDigest, ref.kind, ref.digest, ref.sizeBytes}
}

type admissionReplayReference struct {
	lineageID                         [32]byte
	journalID                         [32]byte
	headerDigest                      Digest
	kind                              durableContentObjectKind
	digest                            Digest
	sizeBytes                         uint64
	present                           bool
	objectIdentity                    [32]byte
	inspection                        [32]byte
	runtime                           *admissionReplayRuntimeInspection
	recoveryDecision, recoveryProfile Digest
}

type admissionReplayRuntimeInspection struct {
	manifestDigest, schemaBundleDigest Digest
	maxAttempts                        uint64
	statementCounts                    []uint64
	reservation                        evidenceQuotaReservation
}

type admissionCorruptAccumulator struct {
	key   string
	first error
}

func (a *admissionCorruptAccumulator) add(err error) {
	a.addAt("99999999", err)
}
func (a *admissionCorruptAccumulator) addAt(key string, err error) {
	if err != nil && (a.first == nil || key < a.key) {
		a.key, a.first = key, admissionCorrupt("admission-pass1", "stored evidence inventory is corrupt", err)
	}
}

// replayAdmissionInventory consumes every view before the final Revalidate.
// Its result is structural input only and cannot mint a VerifiedAdmissionPlan.
func replayAdmissionInventory(ctx context.Context, inventory *evidencefs.AdmissionInventory, target [32]byte) (*admissionReplayTranscript, error) {
	if inventory == nil {
		return nil, admissionFailed("admission-replay", "inventory is unavailable", nil)
	}
	if err := contextAdmissionError(ctx); err != nil {
		return nil, err
	}
	revision, err := inventory.Revision()
	if err != nil {
		return nil, mapEvidenceAdmissionError(err, "admission-revision")
	}
	corrupt := &admissionCorruptAccumulator{}
	if revision != 0 {
		corrupt.add(admissionCorrupt("admission-revision", "inventory revision is not zero", nil))
	}
	boundTarget, err := inventory.Target()
	if err != nil {
		return nil, mapEvidenceAdmissionError(err, "admission-target")
	}
	if boundTarget != target {
		corrupt.add(admissionCorrupt("admission-target", "acquisition target mismatch", nil))
	}
	fullSet, err := inventory.FullSetDigest()
	if err != nil {
		return nil, mapEvidenceAdmissionError(err, "admission-full-set")
	}
	if fullSet == ([32]byte{}) {
		corrupt.add(admissionCorrupt("admission-full-set", "full-set digest is empty", nil))
	}
	ids, err := inventory.LineageIDs()
	if err != nil {
		return nil, mapEvidenceAdmissionError(err, "admission-lineages")
	}
	if !strictRawDigestOrder(ids) {
		corrupt.add(admissionCorrupt("admission-lineages", "lineage ids are not strictly ordered", nil))
	}
	present := rawDigestContains(ids, target)
	absentFact, err := inventory.TargetAbsent()
	if err != nil {
		return nil, mapEvidenceAdmissionError(err, "admission-target-absence")
	}
	if err := validateAdmissionTargetXOR(target, fullSet, present, absentFact); err != nil {
		if IsCode(err, CodeEvidenceJournalCorrupt) {
			corrupt.add(err)
		} else {
			return nil, err
		}
	}

	transcript := &admissionReplayTranscript{revision: revision, fullSetDigest: fullSet, target: target, targetAbsent: !present}
	globalJournals := make(map[[32]byte]bool)
	var references []admissionObjectReference
	for lineageOrdinal, id := range ids {
		if err := contextAdmissionError(ctx); err != nil {
			return nil, err
		}
		view, err := inventory.Lineage(id)
		if err != nil {
			return nil, mapEvidenceAdmissionError(err, "admission-lineage")
		}
		lineage, refs, lineageCorrupt, err := replayAdmissionLineage(ctx, view, id, globalJournals)
		if err != nil {
			return nil, err
		}
		corrupt.addAt(fmt.Sprintf("10%06d", lineageOrdinal), lineageCorrupt)
		transcript.lineages = append(transcript.lineages, lineage)
		transcript.indexBytes, err = admissionSaturatingAdd(transcript.indexBytes, lineage.index.size)
		corrupt.addAt("07000000", err)
		transcript.indexRecords, err = admissionSaturatingAdd(transcript.indexRecords, lineage.indexRecords)
		corrupt.addAt("07000001", err)
		if transcript.indexBytes > rootIndexMaximumBytes || uint64(len(transcript.lineages)) > rootIndexMaximumCount {
			corrupt.add(admissionCorrupt("admission-index-usage", "stored root index usage exceeds maximum", nil))
		}
		if transcript.indexRecords > rootIndexMaximumCount*lineageIndexMaximumRecords {
			corrupt.add(admissionCorrupt("admission-index-usage", "stored root index records exceed maximum", nil))
		}
		for _, journal := range lineage.journals {
			transcript.journalRecords, err = admissionSaturatingAdd(transcript.journalRecords, journal.records)
			corrupt.addAt("08000000", err)
			for _, segment := range journal.segments {
				transcript.journalBytes, err = admissionSaturatingAdd(transcript.journalBytes, segment.file.size)
				corrupt.addAt("08000001", err)
			}
		}
		if transcript.journalBytes > rootJournalMaximumBytes || uint64(len(globalJournals)) > rootJournalMaximumCount {
			corrupt.add(admissionCorrupt("admission-journal-usage", "stored root journal usage exceeds maximum", nil))
		}
		if transcript.journalRecords > rootJournalMaximumCount*maxEvidenceReservedRecords {
			corrupt.add(admissionCorrupt("admission-journal-usage", "stored root journal records exceed maximum", nil))
		}
		references = append(references, refs...)
	}

	finalViews, err := inventory.Objects()
	if err != nil {
		return nil, mapEvidenceAdmissionError(err, "admission-objects")
	}
	tempViews, err := inventory.TemporaryObjects()
	if err != nil {
		return nil, mapEvidenceAdmissionError(err, "admission-temporary-objects")
	}
	objects, replayReferences, needs, err := replayAdmissionObjects(ctx, finalViews, tempViews, references)
	if err != nil {
		if IsCode(err, CodeEvidenceJournalCorrupt) {
			corrupt.add(err)
		} else {
			return nil, err
		}
	}
	transcript.objects, transcript.references, transcript.recoveryNeeds = objects, replayReferences, needs
	if err := attachAdmissionInspections(transcript); err != nil {
		if IsCode(err, CodeEvidenceJournalCorrupt) {
			corrupt.add(err)
		} else {
			return nil, err
		}
	}
	if transcript.journalReservedBytes > rootJournalMaximumBytes || transcript.indexReservedBytes > rootIndexMaximumBytes {
		corrupt.add(admissionCorrupt("admission-inspection", "verified historical reservation exceeds root maximum", nil))
	}
	if err := inventory.Revalidate(ctx); err != nil {
		return nil, mapEvidenceAdmissionError(err, "admission-revalidate")
	}
	if corrupt.first != nil {
		return nil, corrupt.first
	}
	transcript.canonical = admissionReplayCanonicalDigest(transcript)
	// Every stored slice/body was copied at its accessor/collector boundary;
	// returning this owned value avoids a full-root second copy at peak usage.
	return transcript, nil
}

func attachAdmissionInspections(transcript *admissionReplayTranscript) error {
	if transcript == nil {
		return admissionCorrupt("admission-inspection", "transcript is unavailable", nil)
	}
	type inspectionTarget struct {
		generation             *admissionReplayGeneration
		mayOwnIndexHeader      bool
		indexHeaderFramedBytes uint64
	}
	byGeneration := make(map[[64]byte][]inspectionTarget)
	transcript.journalReservedBytes, transcript.indexReservedBytes = 0, 0
	for lineageIndex := range transcript.lineages {
		lineage := &transcript.lineages[lineageIndex]
		register := func(generation *admissionReplayGeneration, mayOwnIndexHeader bool) error {
			if generation == nil {
				return nil
			}
			generation.runtimeInspection = nil
			generation.remainingIndexRecords, generation.remainingIndexBytes = 0, 0
			generation.indexHeaderDebited = false
			var key [64]byte
			copy(key[:32], lineage.id[:])
			journal := digestRaw(generation.journalID)
			copy(key[32:], journal[:])
			byGeneration[key] = append(byGeneration[key], inspectionTarget{generation, mayOwnIndexHeader, lineage.indexHeaderFramedBytes})
			return nil
		}
		for generationIndex := range lineage.generations {
			generation := &lineage.generations[generationIndex]
			if err := register(generation, generationIndex == 0); err != nil {
				return err
			}
			if err := register(generation.plannedSuccessor, false); err != nil {
				return err
			}
		}
	}
	for index := range transcript.references {
		ref := &transcript.references[index]
		if !ref.present || ref.kind != durableRuntimeContentObject {
			continue
		}
		var key [64]byte
		copy(key[:32], ref.lineageID[:])
		copy(key[32:], ref.journalID[:])
		targets := byGeneration[key]
		inspection := ref.runtime
		if len(targets) == 0 || inspection == nil {
			return admissionCorrupt("admission-inspection", "runtime bundle differs from stored generation reservation", nil)
		}
		withIndexHeader, err := calculateEvidenceQuotaReservationFromArithmeticFacts(quotaBundleArithmeticFacts{inspection.maxAttempts, inspection.statementCounts}, true)
		if err != nil {
			return admissionCorrupt("admission-inspection", "runtime bundle index-header reservation is invalid", err)
		}
		reservationMatches := func(generation *admissionReplayGeneration, reservation evidenceQuotaReservation) bool {
			return reservation.ReservedRecords == generation.reservedRecords && reservation.ReservedBytes == generation.reservedBytes && reservation.ReservedSegments == generation.reservedSegments
		}
		for _, target := range targets {
			generation := target.generation
			if generation.header == nil || inspection.manifestDigest != generation.header.manifestDigest || inspection.schemaBundleDigest != generation.schemaBundleDigest {
				return admissionCorrupt("admission-inspection", "runtime bundle differs from stored generation reservation", nil)
			}
			reservation := inspection.reservation
			ownsIndexHeader := false
			switch {
			case reservationMatches(generation, reservation):
			case target.mayOwnIndexHeader && reservationMatches(generation, withIndexHeader):
				reservation, ownsIndexHeader = withIndexHeader, true
			default:
				return admissionCorrupt("admission-inspection", "runtime bundle differs from stored generation reservation", nil)
			}
			var consumedRecords, consumedBytes uint64
			if ownsIndexHeader {
				if target.indexHeaderFramedBytes == 0 || target.indexHeaderFramedBytes > lineageRecordFrameLimits[LineageRecordHeader] {
					return admissionCorrupt("admission-inspection", "lineage header debit is unavailable or exceeds its reservation", nil)
				}
				consumedRecords, consumedBytes = 1, target.indexHeaderFramedBytes
			}
			for _, debit := range generation.indexDebits {
				var err error
				consumedRecords, err = admissionCheckedAdd(consumedRecords, 1)
				if err != nil {
					return err
				}
				consumedBytes, err = admissionCheckedAdd(consumedBytes, debit.framedBytes)
				if err != nil {
					return err
				}
			}
			if consumedRecords > reservation.ReservedIndexRecords || consumedBytes > reservation.ReservedIndexBytes {
				return admissionCorrupt("admission-inspection", "durable index debit exceeds generation reservation", nil)
			}
			generation.remainingIndexRecords = reservation.ReservedIndexRecords - consumedRecords
			generation.remainingIndexBytes = reservation.ReservedIndexBytes - consumedBytes
			generation.indexHeaderDebited = ownsIndexHeader
			owned := *inspection
			owned.statementCounts = append([]uint64(nil), inspection.statementCounts...)
			owned.reservation = reservation
			generation.runtimeInspection = &owned
		}
		var addErr error
		lineage := lineageByID(transcript.lineages, ref.lineageID)
		if lineage == nil {
			return admissionCorrupt("admission-inspection", "generation lineage is absent", nil)
		}
		for _, journal := range lineage.journals {
			if journal.id == ref.journalID {
				var physical uint64
				for _, segment := range journal.segments {
					physical, addErr = admissionSaturatingAdd(physical, segment.file.size)
					if addErr != nil {
						return addErr
					}
				}
				if physical > inspection.reservation.ReservedJournalBytes {
					return admissionCorrupt("admission-inspection", "physical journal exceeds verified reservation", nil)
				}
			}
		}
	}
	for lineageIndex := range transcript.lineages {
		for generationIndex := range transcript.lineages[lineageIndex].generations {
			generation := &transcript.lineages[lineageIndex].generations[generationIndex]
			if generation.runtimeInspection == nil {
				continue
			}
			var err error
			transcript.journalReservedBytes, err = admissionSaturatingAdd(transcript.journalReservedBytes, generation.runtimeInspection.reservation.ReservedJournalBytes)
			if err != nil {
				return err
			}
			transcript.indexReservedBytes, err = admissionSaturatingAdd(transcript.indexReservedBytes, generation.remainingIndexBytes)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// rootQuotaUsageFactsFromAdmissionTranscript is a pure projection from the
// fully replayed ordinary transcript. It mints no authority and deliberately
// recomputes every aggregate from compact physical facts instead of accepting
// the transcript's cached counters as caller-supplied quota input.
func rootQuotaUsageFactsFromAdmissionTranscript(transcript *admissionReplayTranscript) (rootQuotaUsageFacts, error) {
	if transcript == nil || transcript.revision != 0 || transcript.canonical == ([32]byte{}) || transcript.canonical != admissionReplayCanonicalDigest(transcript) {
		return rootQuotaUsageFacts{}, admissionCorrupt("admission-quota", "admission transcript is not exact", nil)
	}
	facts := rootQuotaUsageFacts{targetIndexPresent: !transcript.targetAbsent}
	var err error
	for _, object := range transcript.objects {
		if object.size == 0 || object.size > rootTempMaximumEachBytes || requireDigest("admission-quota.object", object.digest) != nil {
			return rootQuotaUsageFacts{}, admissionCorrupt("admission-quota", "stored object quota fact is invalid", nil)
		}
		if object.temporary {
			facts.tempCount, err = admissionCheckedAdd(facts.tempCount, 1)
			if err != nil {
				return rootQuotaUsageFacts{}, err
			}
			facts.tempBytes, err = admissionCheckedAdd(facts.tempBytes, object.size)
			if err != nil {
				return rootQuotaUsageFacts{}, err
			}
			if object.size > facts.largestTempBytes {
				facts.largestTempBytes = object.size
			}
			continue
		}
		facts.finalObjects = append(facts.finalObjects, rootQuotaObjectFact{digest: object.digest, size: object.size})
		facts.finalObjectBytes, err = admissionCheckedAdd(facts.finalObjectBytes, object.size)
		if err != nil {
			return rootQuotaUsageFacts{}, err
		}
	}
	facts.finalObjects = canonicalRootObjects(facts.finalObjects)
	for lineageIndex := range transcript.lineages {
		lineage := &transcript.lineages[lineageIndex]
		facts.indexCount, err = admissionCheckedAdd(facts.indexCount, 1)
		if err != nil {
			return rootQuotaUsageFacts{}, err
		}
		facts.indexActualBytes, err = admissionCheckedAdd(facts.indexActualBytes, lineage.index.size)
		if err != nil {
			return rootQuotaUsageFacts{}, err
		}
		facts.journalCount, err = admissionCheckedAdd(facts.journalCount, uint64(len(lineage.journals)))
		if err != nil {
			return rootQuotaUsageFacts{}, err
		}
		var lineageReservedBytes, lineageReservedRecords uint64
		for generationIndex := range lineage.generations {
			generation := &lineage.generations[generationIndex]
			if generation.runtimeInspection == nil {
				return rootQuotaUsageFacts{}, fail(CodeEvidenceRecoveryRequired, "admission-quota", "historical runtime quota inspection is unavailable", nil)
			}
			facts.journalReservedBytes, err = admissionCheckedAdd(facts.journalReservedBytes, generation.runtimeInspection.reservation.ReservedJournalBytes)
			if err != nil {
				return rootQuotaUsageFacts{}, err
			}
			lineageReservedBytes, err = admissionCheckedAdd(lineageReservedBytes, generation.remainingIndexBytes)
			if err != nil {
				return rootQuotaUsageFacts{}, err
			}
			lineageReservedRecords, err = admissionCheckedAdd(lineageReservedRecords, generation.remainingIndexRecords)
			if err != nil {
				return rootQuotaUsageFacts{}, err
			}
		}
		facts.indexReservedBytes, err = admissionCheckedAdd(facts.indexReservedBytes, lineageReservedBytes)
		if err != nil {
			return rootQuotaUsageFacts{}, err
		}
		if lineage.id == transcript.target {
			facts.targetIndexRecords, facts.targetIndexBytes = lineage.indexRecords, lineage.index.size
			facts.targetIndexReservedRecords, facts.targetIndexReservedBytes = lineageReservedRecords, lineageReservedBytes
		}
	}
	if facts.journalReservedBytes != transcript.journalReservedBytes || facts.indexActualBytes != transcript.indexBytes || facts.indexReservedBytes != transcript.indexReservedBytes || facts.targetIndexPresent && (facts.targetIndexRecords == 0 || facts.targetIndexBytes == 0) || facts.targetIndexPresent == transcript.targetAbsent || !facts.valid() {
		return rootQuotaUsageFacts{}, admissionCorrupt("admission-quota", "recomputed root quota facts differ from admission transcript", nil)
	}
	return facts, nil
}

func lineageByID(lineages []admissionReplayLineage, id [32]byte) *admissionReplayLineage {
	for index := range lineages {
		if lineages[index].id == id {
			return &lineages[index]
		}
	}
	return nil
}

func validateAdmissionTargetXOR(target, fullSet [32]byte, present bool, fact *evidencefs.TargetAbsentFact) error {
	if present {
		if fact != nil {
			return admissionCorrupt("admission-target", "present target also has absence fact", nil)
		}
		return nil
	}
	if fact == nil {
		return admissionCorrupt("admission-target", "absent target has no absence fact", nil)
	}
	gotTarget, err := fact.Target()
	if err != nil {
		return mapEvidenceAdmissionError(err, "admission-target-fact")
	}
	gotFullSet, err := fact.FullSetDigest()
	if err != nil {
		return mapEvidenceAdmissionError(err, "admission-target-fact")
	}
	if gotTarget != target || gotFullSet != fullSet {
		return admissionCorrupt("admission-target", "absence fact binding mismatch", nil)
	}
	return nil
}

func replayAdmissionLineage(ctx context.Context, view *evidencefs.AdmissionLineageView, expected [32]byte, global map[[32]byte]bool) (admissionReplayLineage, []admissionObjectReference, error, error) {
	findings := &admissionCorruptAccumulator{}
	actual, err := view.ID()
	if err != nil {
		return admissionReplayLineage{}, nil, nil, mapEvidenceAdmissionError(err, "admission-lineage-id")
	}
	if actual != expected {
		findings.addAt("01000000", admissionCorrupt("admission-lineage-id", "lineage id mismatch", nil))
	}
	indexView, err := view.Index()
	if err != nil {
		return admissionReplayLineage{}, nil, nil, mapEvidenceAdmissionError(err, "admission-index")
	}
	indexFile, indexBytes, indexFinding, indexFatal := readAdmissionFile(ctx, indexView, 0)
	if indexFatal != nil {
		return admissionReplayLineage{}, nil, nil, indexFatal
	}
	findings.addAt("02000000", indexFinding)
	indexFrames, indexDecodeErr := decodeAdmissionLineageFrames(indexBytes)
	if indexDecodeErr != nil {
		findings.addAt("02000000", indexDecodeErr)
	}
	indexBound := indexFinding == nil && indexDecodeErr == nil
	if indexBound && (len(indexFrames) == 0 || indexFrames[0].Record.Header == nil || digestRaw(indexFrames[0].Record.Header.ExecutionLineageDigest) != expected) {
		findings.addAt("02000001", admissionCorrupt("admission-index", "physical lineage and index identity differ", nil))
		indexBound = false
	}
	var plan *lineageStructuralPlan
	if indexBound {
		plan, err = scanLineageChainStructure(indexFrames)
		if err != nil {
			findings.addAt("02000002", admissionCorrupt("admission-lineage-structure", "lineage structure is invalid", err))
			indexBound = false
		}
	}

	journalViews, err := view.Journals()
	if err != nil {
		return admissionReplayLineage{}, nil, nil, mapEvidenceAdmissionError(err, "admission-journals")
	}
	lineage := admissionReplayLineage{id: expected, index: indexFile, indexRecords: uint64(len(indexFrames))}
	var references []admissionObjectReference
	if indexBound {
		indexHeaderBytes, encodeErr := EncodeCanonicalLineageFrame(indexFrames[0])
		if encodeErr != nil || len(indexHeaderBytes) == 0 {
			findings.addAt("02000003", admissionCorrupt("admission-index", "lineage header framing cannot be reconstructed", encodeErr))
			indexBound = false
		} else {
			lineage.indexHeaderFramedBytes = uint64(len(indexHeaderBytes))
		}
	}
	if indexBound {
		lineage.generations, err = compactAdmissionGenerations(indexFrames)
		if err != nil {
			findings.addAt("02000003", admissionCorrupt("admission-index", "generation compaction failed", err))
			indexBound = false
		}
		if indexBound {
			h := indexFrames[0].Record.Header
			lineage.header = admissionReplayLineageHeader{h.ExecutionLineageDigest, h.DeploymentID, h.ExpectedDatabaseIdentity.DatabaseName, h.RepositoryIdentity, h.LimitsProfile}
			lineage.indexHeaderRecordDigest = indexFrames[0].RecordDigest
			lineage.indexTailRecordDigest = indexFrames[len(indexFrames)-1].RecordDigest
			for _, generation := range lineage.generations {
				references = append(references, admissionGenerationReferences(expected, generation)...)
				if generation.plannedSuccessor != nil {
					references = append(references, admissionGenerationReferences(expected, *generation.plannedSuccessor)...)
				}
			}
		}
	}
	segmentZero := make(map[Digest]EvidenceFrame, len(journalViews))
	journalIDs := make(map[Digest]bool, len(journalViews))
	var previous [32]byte
	for ordinal, journalView := range journalViews {
		key := func(phase int) string { return fmt.Sprintf("%02d%06d", phase, ordinal) }
		journalID, err := journalView.ID()
		journalIDValid := true
		if err != nil {
			return admissionReplayLineage{}, nil, nil, mapEvidenceAdmissionError(err, "admission-journal-id")
		}
		if journalIDValid && ordinal > 0 && bytes.Compare(previous[:], journalID[:]) >= 0 {
			findings.addAt(key(3), admissionCorrupt("admission-journals", "journal ids are not strictly ordered", nil))
		}
		if journalIDValid && global[journalID] {
			findings.addAt(key(3), admissionCorrupt("admission-journals", "journal id is registered more than once", nil))
		}
		if journalIDValid {
			global[journalID], previous = true, journalID
		}
		segments, err := journalView.Segments()
		if err != nil {
			return admissionReplayLineage{}, nil, nil, mapEvidenceAdmissionError(err, "admission-segments")
		}
		journal := admissionReplayJournal{id: journalID}
		journalDigest := digestString(journalID)
		accumulator, expectedJournal := openEvidenceJournalStructuralStream(plan, journalDigest, nil)
		if !journalIDValid {
			expectedJournal = false
		}
		if plan != nil && !expectedJournal {
			findings.addAt(key(3), admissionCorrupt("admission-journals", "orphan registered journal", nil))
		}
		collector := &admissionReplayJournalCollector{}
		generationIndex := -1
		for index := range lineage.generations {
			if lineage.generations[index].journalID == journalDigest {
				generationIndex = index
				collector.initial = cloneAdmissionContinuation(lineage.generations[index].continuation)
				break
			}
		}
		drainOnly := !journalIDValid
		for segmentOrdinal, segmentView := range segments {
			file, raw, fileFinding, fileFatal := readAdmissionFile(ctx, segmentView, uint32(segmentOrdinal))
			if fileFatal != nil {
				return admissionReplayLineage{}, nil, nil, fileFatal
			}
			if fileFinding != nil {
				findings.addAt(key(4), fileFinding)
				drainOnly = true
			}
			if drainOnly {
				appendAdmissionDrainedSegment(file, raw, key(4), &journal, findings)
				continue
			}
			if err := accumulator.beginSegment(); err != nil {
				findings.addAt(key(4), admissionCorrupt("admission-journal-structure", "journal segment cannot begin", err))
				appendAdmissionDrainedSegment(file, raw, key(4), &journal, findings)
				drainOnly = true
				continue
			}
			records, tail, first, err := streamAdmissionEvidenceFrames(raw, accumulator, collector)
			if err != nil {
				findings.addAt(key(4), err)
				drainOnly = true
				appendAdmissionDrainedSegment(file, raw, key(4), &journal, findings)
				continue
			}
			if err := accumulator.endSegment(); err != nil {
				findings.addAt(key(4), admissionCorrupt("admission-journal-structure", "journal segment cannot end", err))
				appendAdmissionDrainedSegment(file, raw, key(4), &journal, findings)
				drainOnly = true
				continue
			}
			if segmentOrdinal == 0 {
				segmentZero[journalDigest] = first
			}
			journal.segments = append(journal.segments, admissionReplaySegment{file: file, records: records})
			journal.records, err = admissionSaturatingAdd(journal.records, records)
			findings.addAt(key(4), err)
			journal.tail = tail
		}
		var replay *evidenceStructuralReplay
		var structuralErr error
		if !drainOnly {
			replay, structuralErr = accumulator.finish()
		}
		journalBound := !drainOnly && structuralErr == nil && expectedJournal && indexBound
		if structuralErr != nil {
			findings.addAt(key(4), admissionCorrupt("admission-journal-structure", "journal structure is invalid", structuralErr))
		}
		if journalBound {
			if err := collector.validate(); err != nil {
				findings.addAt(key(5), err)
				journalBound = false
			}
		}
		var recoveryTail *admissionReplayRecoveryTail
		if journalBound {
			var tailFinding, tailFatal error
			recoveryTail, tailFinding, tailFatal = rebuildAdmissionRecoveryTail(ctx, segments, journal.segments, journal.tail, collector.tailKey)
			if tailFatal != nil {
				return admissionReplayLineage{}, nil, nil, tailFatal
			}
			if tailFinding != nil {
				findings.addAt(key(5), tailFinding)
				journalBound = false
			}
		}
		if journalBound {
			if generationIndex >= 0 {
				summary := cloneEvidenceJournalSummary(replay.summary)
				lineage.generations[generationIndex].summary = &summary
				lineage.generations[generationIndex].currentTail = cloneAdmissionRecoveryTail(recoveryTail)
				lineage.generations[generationIndex].verificationTerminals = append([]admissionReplayTerminalEvent(nil), collector.terminals...)
				lineage.generations[generationIndex].verificationFinals = append([]admissionReplayTerminalFinal(nil), collector.finals...)
				lineage.generations[generationIndex].verificationCommits = append([]admissionReplayTerminalCommit(nil), collector.commits...)
				lineage.generations[generationIndex].verificationRetries = append([]admissionReplayTerminalRetry(nil), collector.retries...)
				lineage.generations[generationIndex].verificationResolutions = append([]admissionReplayTerminalResolution(nil), collector.resolutions...)
				lineage.generations[generationIndex].verificationOpen = collector.openAttempt()
				lineage.generations[generationIndex].verificationCatalogContract = collector.catalogContract
				header := compactAdmissionHeaderFacts(*replay.firstFrame.Record.Header)
				lineage.generations[generationIndex].header = &header
			}
			if err := plan.acceptJournal(journalDigest, replay); err != nil {
				findings.addAt(key(5), admissionCorrupt("admission-lineage-structure", "journal does not satisfy lineage plan", err))
				journalBound = false
			}
		}
		journalIDs[journalDigest] = true
		lineage.journals = append(lineage.journals, journal)
	}
	lineageBound := indexBound && plan != nil
	if lineageBound {
		if _, err = plan.finish(segmentZero, journalIDs); err != nil {
			findings.addAt("06000000", admissionCorrupt("admission-lineage-structure", "lineage structure is invalid", err))
			lineageBound = false
		}
	}
	if !lineageBound || findings.first != nil {
		lineage.state = ""
		lineage.generations = nil
		return lineage, nil, findings.first, nil
	}
	lineage.state, err = classifyAdmissionLineageStateCompact(indexFrames, lineage.journals)
	if err != nil {
		return admissionReplayLineage{}, nil, err, nil
	}
	return lineage, references, nil, nil
}

func (c *admissionReplayJournalCollector) validate() error {
	if c.catalogContract == ([32]byte{}) && (len(c.terminals) != 0 || c.active != nil) {
		return admissionCorrupt("admission-verification-events", "catalog contract binding is absent", nil)
	}
	for _, event := range c.terminals {
		statements := event.flags&admissionTerminalHasStatements != 0
		known := admissionTerminalHasFinal | admissionTerminalHasCommit | admissionTerminalHasRetry | admissionTerminalHasResolution | admissionTerminalHasStatements
		if event.flags & ^known != 0 || event.migrationID > 999999 || event.attemptIndex == 0 || statements != (event.statementCount != 0) || statements && (event.lastStatementIndex+1 != event.statementCount || event.statementChain == ([32]byte{})) || !statements && (event.lastStatementIndex != 0 || event.statementChain != ([32]byte{})) || event.outcome == 0 || event.outcome > 7 || event.terminalDigest == ([32]byte{}) {
			return admissionCorrupt("admission-verification-events", "terminal folded payload is incomplete", nil)
		}
		if event.flags&admissionTerminalHasResolution == 0 && event.resolutionOutcome != 0 || event.flags&admissionTerminalHasResolution != 0 && (event.resolutionOutcome == 0 || event.resolutionOutcome > 3) {
			return admissionCorrupt("admission-verification-events", "terminal resolution payload is inconsistent", nil)
		}
		final, commit, retry, resolution := event.flags&admissionTerminalHasFinal != 0, event.flags&admissionTerminalHasCommit != 0, event.flags&admissionTerminalHasRetry != 0, event.flags&admissionTerminalHasResolution != 0
		if final && (!statements || !commit) || resolution && event.outcome != 7 || event.outcome == 1 && (!final || !commit || retry || resolution) || event.outcome >= 4 && event.outcome <= 7 && (!final || !commit || retry) || event.outcome == 2 && !retry || event.outcome == 3 && final {
			return admissionCorrupt("admission-verification-events", "terminal outcome and sparse facts disagree", nil)
		}
	}
	checks := []error{
		validateAdmissionSparse(len(c.terminals), admissionTerminalHasFinal, c.terminals, len(c.finals), func(index int) uint32 { return c.finals[index].ordinal }, func(index int) bool {
			return c.finals[index].lastIntermediateRecord != ([32]byte{}) && c.finals[index].preledgerCatalog != ([32]byte{})
		}),
		validateAdmissionSparse(len(c.terminals), admissionTerminalHasCommit, c.terminals, len(c.commits), func(index int) uint32 { return c.commits[index].ordinal }, func(index int) bool {
			return c.commits[index].expectedLedgerLength != 0 && c.commits[index].commitRecord != ([32]byte{}) && c.commits[index].commitBody != ([32]byte{}) && c.commits[index].attemptPredecessorCatalog != ([32]byte{}) && c.commits[index].lastIntermediateState != ([32]byte{})
		}),
		validateAdmissionSparse(len(c.terminals), admissionTerminalHasRetry, c.terminals, len(c.retries), func(index int) uint32 { return c.retries[index].ordinal }, func(index int) bool {
			v := c.retries[index]
			return v.proofKind > 0 && v.proofKind <= 4 && (v.proofKind == 4) == (v.commitRejectedReason != 0) && v.commitRejectedReason <= 3 && v.attemptPredecessorCatalog != ([32]byte{}) && v.observedCatalog != ([32]byte{}) && v.ledgerPrefix != ([32]byte{}) && v.authorityResult != ([32]byte{})
		}),
		validateAdmissionSparse(len(c.terminals), admissionTerminalHasResolution, c.terminals, len(c.resolutions), func(index int) uint32 { return c.resolutions[index].ordinal }, func(index int) bool { return c.resolutions[index].resolutionDigest != ([32]byte{}) }),
	}
	for _, err := range checks {
		if err != nil {
			return err
		}
	}
	for _, retry := range c.retries {
		commit := c.terminals[retry.ordinal].flags&admissionTerminalHasCommit != 0
		if (retry.proofKind == 4) != commit {
			return admissionCorrupt("admission-verification-events", "retry proof and commit boundary disagree", nil)
		}
	}
	if c.active != nil && *c.active != c.tailKey {
		return admissionCorrupt("admission-verification-events", "open attempt is not the physical tail", nil)
	}
	if open := c.openAttempt(); open != nil {
		if open.migrationID > 999999 || open.attemptIndex == 0 || open.statementCount == 0 || open.lastStatementIndex+1 != open.statementCount || open.statementChain == ([32]byte{}) || open.commitPresent != (open.commitRecord != ([32]byte{})) || open.commitPresent && (open.commitBody == ([32]byte{}) || open.attemptPredecessorCatalog == ([32]byte{}) || open.lastIntermediateState == ([32]byte{}) || open.expectedLedgerLength == 0) || !open.commitPresent && (open.commitBody != ([32]byte{}) || open.previousAttemptTerminal != ([32]byte{}) || open.attemptPredecessorCatalog != ([32]byte{}) || open.lastIntermediateState != ([32]byte{}) || open.expectedLedgerLength != 0) {
			return admissionCorrupt("admission-verification-events", "open attempt payload is inconsistent", nil)
		}
	}
	return nil
}

func validateAdmissionSparse(terminalCount int, bit uint16, terminals []admissionReplayTerminalEvent, length int, ordinalAt func(int) uint32, validAt func(int) bool) error {
	cursor := 0
	for ordinal := 0; ordinal < terminalCount; ordinal++ {
		present := terminals[ordinal].flags&bit != 0
		if present {
			if cursor >= length || ordinalAt(cursor) != uint32(ordinal) || !validAt(cursor) {
				return admissionCorrupt("admission-verification-events", "terminal sparse payload is inconsistent", nil)
			}
			cursor++
		} else if cursor < length && ordinalAt(cursor) == uint32(ordinal) {
			return admissionCorrupt("admission-verification-events", "terminal sparse payload is inconsistent", nil)
		}
	}
	if cursor != length {
		return admissionCorrupt("admission-verification-events", "terminal sparse payload ordinal is invalid", nil)
	}
	return nil
}

func (c *admissionReplayJournalCollector) openAttempt() *admissionReplayOpenAttempt {
	if c.active == nil {
		return nil
	}
	migrationID, err := admissionMigrationNumber(c.active.migrationID)
	if err != nil {
		return nil
	}
	value := &admissionReplayOpenAttempt{migrationID: migrationID, attemptIndex: c.active.attemptIndex, statementCount: c.frontier.statementCount, lastStatementIndex: c.frontier.lastStatementIndex, statementChain: c.frontier.statementChain}
	if c.frontier.commitRecord != ([32]byte{}) {
		value.commitPresent = true
		value.commitRecord, value.commitBody = c.frontier.commitRecord, c.frontier.commitBody
		value.previousAttemptTerminal = c.frontier.commitPrevious
		value.attemptPredecessorCatalog = c.frontier.commitPredecessor
		value.lastIntermediateState = c.frontier.lastIntermediateState
		value.expectedLedgerLength = c.frontier.expectedLedgerLength
	}
	return value
}

func cloneAdmissionContinuation(value *admissionReplayContinuation) *admissionReplayContinuation {
	if value == nil {
		return nil
	}
	owned := *value
	owned.previousAttemptTerminalDigest = cloneDigestPointer(value.previousAttemptTerminalDigest)
	return &owned
}

func compactAdmissionHeaderFacts(h JournalHeader) admissionReplayHeaderFacts {
	return admissionReplayHeaderFacts{h.JournalIdentityDigest, h.ReleaseTrustDecisionDigest, h.RunnerProjectionDecisionDigest, h.ExecutionLineageDigest, h.OuterArtifactDigest, h.OuterArtifactSizeBytes, h.DecisionRecoveryArtifactSHA256, h.DecisionRecoveryArtifactSizeBytes, h.ManifestDigest, h.RunnerReleaseDigest, h.SchemaBundleDigest, h.AuthorityProfileDigest, h.AuthorityBindingDigest, h.QuotaReservationDigest, h.ReservedRecords, h.ReservedBytes, h.ReservedSegments}
}

func admissionGenerationReferences(lineage [32]byte, generation admissionReplayGeneration) []admissionObjectReference {
	if generation.header == nil {
		return nil
	}
	journal := digestRaw(generation.journalID)
	h := generation.header
	common := admissionObjectReference{lineageID: lineage, journalID: journal, headerDigest: generation.expectedSegment0HeaderDigest, decision: generation.runnerProjectionDecisionDigest, manifest: h.manifestDigest, schema: generation.schemaBundleDigest, records: generation.reservedRecords, bytes: generation.reservedBytes, segments: generation.reservedSegments}
	runtime := common
	runtime.kind, runtime.digest, runtime.sizeBytes = durableRuntimeContentObject, h.outerArtifactDigest, h.outerArtifactSize
	recovery := common
	recovery.kind, recovery.digest, recovery.sizeBytes = durableDecisionRecoveryContentObject, h.recoveryArtifactDigest, h.recoveryArtifactSize
	return []admissionObjectReference{runtime, recovery}
}

func readAdmissionFile(ctx context.Context, view *evidencefs.AdmissionFileView, expectedOrdinal uint32) (admissionReplayFile, []byte, error, error) {
	if view == nil {
		return admissionReplayFile{}, nil, admissionCorrupt("admission-file", "file view is unavailable", nil), nil
	}
	var finding error
	capture := func(err error, operation string) error {
		if err == nil {
			return nil
		}
		mapped := mapEvidenceAdmissionError(err, operation)
		if IsCode(mapped, CodeEvidenceJournalCorrupt) {
			if finding == nil {
				finding = mapped
			}
			return nil
		}
		return mapped
	}
	size, err := view.Size()
	if fatal := capture(err, "admission-file-size"); fatal != nil {
		return admissionReplayFile{}, nil, finding, fatal
	}
	digest, err := view.Digest()
	if fatal := capture(err, "admission-file-digest"); fatal != nil {
		return admissionReplayFile{}, nil, finding, fatal
	}
	identity, err := view.IdentityDigest()
	if fatal := capture(err, "admission-file-identity"); fatal != nil {
		return admissionReplayFile{}, nil, finding, fatal
	}
	ordinal, err := view.Ordinal()
	if fatal := capture(err, "admission-file-ordinal"); fatal != nil {
		return admissionReplayFile{}, nil, finding, fatal
	}
	fact := admissionReplayFile{ordinal: ordinal, size: size, digest: digest, identity: identity}
	if ordinal != expectedOrdinal || digest == ([32]byte{}) || identity == ([32]byte{}) {
		if finding == nil {
			finding = admissionCorrupt("admission-file", "file identity or ordinal is invalid", nil)
		}
	}
	raw, err := view.ReadAll(ctx)
	if err != nil {
		if fatal := capture(err, "admission-file-read"); fatal != nil {
			return fact, nil, finding, fatal
		}
		return fact, nil, finding, nil
	}
	if uint64(len(raw)) != size || sha256.Sum256(raw) != digest {
		if finding == nil {
			finding = admissionCorrupt("admission-file", "file bytes differ from inventory", nil)
		}
	}
	return fact, raw, finding, nil
}

func streamAdmissionEvidenceFrames(raw []byte, accumulator interface {
	consumeFrame(EvidenceFrame, uint64) error
}, collectors ...*admissionReplayJournalCollector) (uint64, Digest, EvidenceFrame, error) {
	var records uint64
	var tail Digest
	var first EvidenceFrame
	err := decodeAdmissionFramedBytes(raw, 16<<20, 4096, maxEvidenceFrameBytes, func(framed []byte) error {
		frame, err := DecodeCanonicalEvidenceFrame(framed)
		if err != nil {
			return err
		}
		if records == 0 {
			first = cloneProjectionValue(*frame)
		}
		if err := accumulator.consumeFrame(*frame, uint64(len(framed))); err != nil {
			return err
		}
		if len(collectors) != 0 && collectors[0] != nil && frame.RecordKind != EvidenceRecordHeader {
			if err := collectors[0].observe(*frame); err != nil {
				return err
			}
		}
		records++
		tail = frame.RecordDigest
		return nil
	})
	if err != nil {
		return 0, "", EvidenceFrame{}, admissionCorrupt("admission-evidence-decode", "stored evidence segment is invalid", err)
	}
	return records, tail, first, nil
}

func drainAdmissionEvidenceFrames(raw []byte) (uint64, Digest, error) {
	var records uint64
	var tail Digest
	err := decodeAdmissionFramedBytes(raw, 16<<20, 4096, maxEvidenceFrameBytes, func(framed []byte) error {
		frame, err := DecodeCanonicalEvidenceFrame(framed)
		if err != nil {
			return err
		}
		records++
		tail = frame.RecordDigest
		return nil
	})
	if err != nil {
		return records, tail, admissionCorrupt("admission-evidence-drain", "stored evidence segment is invalid", err)
	}
	return records, tail, nil
}

// rebuildAdmissionRecoveryTail is the bounded second read of an already-valid
// journal. Historical frames remain compact; only the final physical attempt's
// at-most-five recovery bodies survive this scan. Revalidate closes the
// inventory snapshot after this read.
func rebuildAdmissionRecoveryTail(ctx context.Context, views []*evidencefs.AdmissionFileView, observed []admissionReplaySegment, expectedTail Digest, key admissionReplayAttemptKey) (*admissionReplayRecoveryTail, error, error) {
	var tail admissionReplayRecoveryTail
	var finding error
	for ordinal, view := range views {
		file, raw, fileFinding, fatal := readAdmissionFile(ctx, view, uint32(ordinal))
		if fatal != nil {
			return nil, finding, fatal
		}
		if fileFinding != nil && finding == nil {
			finding = fileFinding
		}
		if ordinal >= len(observed) || file != observed[ordinal].file {
			if finding == nil {
				finding = admissionCorrupt("admission-tail-reread", "journal file changed during admission", nil)
			}
		}
		if raw == nil {
			continue
		}
		var records uint64
		var physicalTail Digest
		err := decodeAdmissionFramedBytes(raw, 16<<20, 4096, maxEvidenceFrameBytes, func(framed []byte) error {
			frame, err := DecodeCanonicalEvidenceFrame(framed)
			if err != nil {
				return err
			}
			records++
			physicalTail = frame.RecordDigest
			if frame.RecordKind == EvidenceRecordHeader {
				return nil
			}
			migration, attempt := structuralRecordAttempt(frame.Record)
			if migration == key.migrationID && attempt == key.attemptIndex {
				return tail.observe(*frame)
			}
			return nil
		})
		if err != nil && finding == nil {
			finding = admissionCorrupt("admission-tail-reread", "stored evidence changed during tail reconstruction", err)
		}
		if ordinal < len(observed) && records != observed[ordinal].records && finding == nil {
			finding = admissionCorrupt("admission-tail-reread", "journal framing changed during admission", nil)
		}
		if ordinal == len(views)-1 && physicalTail != expectedTail && finding == nil {
			finding = admissionCorrupt("admission-tail-reread", "journal tail changed during admission", nil)
		}
	}
	if len(views) != len(observed) && finding == nil {
		finding = admissionCorrupt("admission-tail-reread", "journal segment set changed during admission", nil)
	}
	if key.migrationID == "" {
		return nil, finding, nil
	}
	if err := validateAdmissionRecoveryTail(&tail); err != nil && finding == nil {
		finding = err
	}
	return &tail, finding, nil
}

func appendAdmissionDrainedSegment(file admissionReplayFile, raw []byte, key string, journal *admissionReplayJournal, findings *admissionCorruptAccumulator) {
	records, tail, drainErr := drainAdmissionEvidenceFrames(raw)
	findings.addAt(key, drainErr)
	journal.segments = append(journal.segments, admissionReplaySegment{file: file, records: records})
	var addErr error
	journal.records, addErr = admissionSaturatingAdd(journal.records, records)
	findings.addAt(key, addErr)
	if tail != "" {
		journal.tail = tail
	}
}

func decodeAdmissionLineageFrames(raw []byte) ([]LineageIndexFrame, error) {
	var frames []LineageIndexFrame
	err := decodeAdmissionFramedBytes(raw, lineageIndexMaximumBytes, lineageIndexMaximumRecords, maxLineageFrameBytes, func(framed []byte) error {
		frame, err := DecodeCanonicalLineageFrame(framed)
		if err != nil {
			return err
		}
		frames = append(frames, cloneProjectionValue(*frame))
		return nil
	})
	if err != nil {
		return nil, admissionCorrupt("admission-index-decode", "stored lineage index is invalid", err)
	}
	if usageErr := validateLineageIndexUsage(uint64(len(frames)), uint64(len(raw))); usageErr != nil {
		return nil, admissionCorrupt("admission-index-decode", "stored lineage index exceeds usage limits", usageErr)
	}
	return frames, nil
}

func decodeAdmissionFramedBytes(raw []byte, containerMaximum, recordMaximum, frameMaximum uint64, consume func([]byte) error) error {
	if len(raw) == 0 || uint64(len(raw)) > containerMaximum || recordMaximum == 0 || frameMaximum < 8 || consume == nil {
		return admissionCorrupt("admission-frame", "empty framed container", nil)
	}
	var records uint64
	for offset := uint64(0); offset < uint64(len(raw)); {
		if records == recordMaximum {
			return admissionCorrupt("admission-frame", "container record maximum exceeded", nil)
		}
		records++
		remaining := uint64(len(raw)) - offset
		if remaining < 8 {
			return admissionCorrupt("admission-frame", "torn frame prefix", nil)
		}
		payload := binary.BigEndian.Uint64(raw[offset : offset+8])
		framed, overflow := checkedFrameAdd(payload, 8)
		if overflow || payload > maxJSONInteger || framed > frameMaximum || framed > remaining || framed > uint64(math.MaxInt) {
			return admissionCorrupt("admission-frame", "invalid frame length", nil)
		}
		end := offset + framed
		if err := consume(raw[offset:end]); err != nil {
			return admissionCorrupt("admission-frame", "non-canonical frame", err)
		}
		offset = end
	}
	return nil
}

func replayAdmissionObjects(ctx context.Context, finals, temps []*evidencefs.AdmissionObjectView, references []admissionObjectReference) ([]admissionReplayObject, []admissionReplayReference, []admissionRecoveryNeed, error) {
	findings := &admissionCorruptAccumulator{}
	if uint64(len(finals)) > rootFinalObjectMaximumCount {
		findings.addAt("00000000", admissionCorrupt("admission-object-usage", "stored final object count exceeds maximum", nil))
	}
	if uint64(len(temps)) > rootTempMaximumCount {
		findings.addAt("00000001", admissionCorrupt("admission-object-usage", "stored temporary object count exceeds maximum", nil))
	}
	refSizeByDigest := make(map[Digest]uint64)
	uniqueReferences := make(map[admissionObjectReferenceKey]admissionObjectReference)
	referenceKinds := make(map[Digest]map[durableContentObjectKind]bool)
	for _, ref := range references {
		if ref.digest.Validate() != nil || ref.sizeBytes == 0 || ref.kind != durableRuntimeContentObject && ref.kind != durableDecisionRecoveryContentObject || ref.decision.Validate() != nil || ref.manifest.Validate() != nil || ref.schema.Validate() != nil || ref.headerDigest.Validate() != nil || ref.records == 0 || ref.bytes == 0 || ref.segments == 0 {
			findings.addAt("01000000", admissionCorrupt("admission-object-reference", "journal object reference is invalid", nil))
			continue
		}
		if ref.kind == durableRuntimeContentObject && ref.sizeBytes > maxRuntimeTarSize {
			findings.addAt("01000001", admissionCorrupt("admission-object-reference", "runtime object reference exceeds maximum", nil))
			continue
		}
		if ref.kind == durableDecisionRecoveryContentObject && ref.sizeBytes > maxDecisionRecoveryArtifactBytes {
			findings.addAt("01000001", admissionCorrupt("admission-object-reference", "recovery object reference exceeds maximum", nil))
			continue
		}
		if prior, ok := refSizeByDigest[ref.digest]; ok && prior != ref.sizeBytes {
			findings.addAt("01000002", admissionCorrupt("admission-object-reference", "object digest has conflicting declarations", nil))
			continue
		}
		refSizeByDigest[ref.digest] = ref.sizeBytes
		key := ref.identityKey()
		if prior, exists := uniqueReferences[key]; exists && prior != ref {
			findings.addAt("01000003", admissionCorrupt("admission-object-reference", "duplicate object reference facts disagree", nil))
			continue
		}
		uniqueReferences[key] = ref
		if referenceKinds[ref.digest] == nil {
			referenceKinds[ref.digest] = make(map[durableContentObjectKind]bool)
		}
		referenceKinds[ref.digest][ref.kind] = true
	}
	objects := make([]admissionReplayObject, 0, len(finals)+len(temps))
	finalIdentityByDigest := make(map[Digest][32]byte)
	runtimeInspectionByDigest := make(map[Digest]admissionReplayRuntimeInspection)
	type recoveryInspection struct {
		digest            [32]byte
		decision, profile Digest
	}
	recoveryInspectionByDigest := make(map[Digest]recoveryInspection)
	tempDigests := make(map[Digest]bool)
	identities := make(map[[32]byte]bool)
	var finalBytes, tempBytes uint64
	consume := func(view *evidencefs.AdmissionObjectView, temporary bool, ordinal int) error {
		key := func(phase int) string { return fmt.Sprintf("%02d%06d", phase, ordinal) }
		if err := contextAdmissionError(ctx); err != nil {
			return err
		}
		valid := true
		actualTemporary, err := view.Temporary()
		if err != nil {
			return mapEvidenceAdmissionError(err, "admission-object-kind")
		}
		if actualTemporary != temporary {
			findings.addAt(key(2), admissionCorrupt("admission-object-kind", "object collection kind mismatch", nil))
			valid = false
		}
		rawDigest, err := view.Digest()
		if err != nil {
			return mapEvidenceAdmissionError(err, "admission-object-digest")
		}
		digest := digestString(rawDigest)
		size, err := view.Size()
		if err != nil {
			return mapEvidenceAdmissionError(err, "admission-object-size")
		}
		if size == 0 || size > rootTempMaximumEachBytes {
			findings.addAt(key(3), admissionCorrupt("admission-object-size", "stored object size exceeds maximum", nil))
			valid = false
		}
		identity, err := view.IdentityDigest()
		if err != nil {
			return mapEvidenceAdmissionError(err, "admission-object-identity")
		}
		if identity == ([32]byte{}) || identities[identity] {
			findings.addAt(key(3), admissionCorrupt("admission-object", "object identity is empty or duplicated", nil))
			valid = false
		}
		identities[identity] = true
		content, err := view.ReadAll(ctx)
		if err != nil {
			mapped := mapEvidenceAdmissionError(err, "admission-object-read")
			if IsCode(mapped, CodeEvidenceJournalCorrupt) {
				findings.addAt(key(4), mapped)
				valid = false
			} else {
				return mapped
			}
		}
		if uint64(len(content)) != size || DigestBytes(content) != digest {
			findings.addAt(key(4), admissionCorrupt("admission-object", "object bytes differ from inventory", nil))
			valid = false
		}
		if !valid {
			return nil
		}
		object := admissionReplayObject{temporary: temporary, digest: digest, size: size, identity: identity}
		if !temporary {
			var addErr error
			finalBytes, addErr = admissionSaturatingAdd(finalBytes, size)
			findings.addAt(key(5), addErr)
			if finalBytes > rootFinalObjectMaximumBytes {
				findings.addAt(key(5), admissionCorrupt("admission-object-usage", "stored final object bytes exceed maximum", nil))
			}
			if _, duplicate := finalIdentityByDigest[digest]; duplicate {
				findings.addAt(key(5), admissionCorrupt("admission-object", "final object digest is duplicated", nil))
				return nil
			}
			finalIdentityByDigest[digest] = identity
			if referenceKinds[digest][durableRuntimeContentObject] {
				inspection, inspectErr := inspectAdmissionRuntimeObject(content)
				if inspectErr != nil {
					findings.addAt(key(6), inspectErr)
				} else {
					runtimeInspectionByDigest[digest] = inspection
				}
			}
			if referenceKinds[digest][durableDecisionRecoveryContentObject] {
				inspection, decision, profile, inspectErr := inspectAdmissionRecoveryObject(content, digest, size)
				if inspectErr != nil {
					findings.addAt(key(6), inspectErr)
				} else {
					recoveryInspectionByDigest[digest] = recoveryInspection{inspection, decision, profile}
				}
			}
			if declaredSize, referenced := refSizeByDigest[digest]; referenced {
				if declaredSize != size {
					findings.addAt(key(5), admissionCorrupt("admission-object", "referenced object size mismatch", nil))
					return nil
				}
			}
		} else {
			var addErr error
			tempBytes, addErr = admissionSaturatingAdd(tempBytes, size)
			findings.addAt(key(5), addErr)
			if tempBytes > rootTempMaximumTotalBytes {
				findings.addAt(key(5), admissionCorrupt("admission-object-usage", "stored temporary object bytes exceed maximum", nil))
			}
			tempDigests[digest] = true
		}
		objects = append(objects, object)
		return nil
	}
	for ordinal, view := range finals {
		if err := consume(view, false, ordinal); err != nil {
			return nil, nil, nil, err
		}
	}
	for ordinal, view := range temps {
		if err := consume(view, true, len(finals)+ordinal); err != nil {
			return nil, nil, nil, err
		}
	}
	sort.Slice(objects, func(i, j int) bool { return admissionObjectLess(objects[i], objects[j]) })
	var needs []admissionRecoveryNeed
	replayReferences := make([]admissionReplayReference, 0, len(uniqueReferences))
	for _, ref := range uniqueReferences {
		identity, ok := finalIdentityByDigest[ref.digest]
		fact := admissionReplayReference{lineageID: ref.lineageID, journalID: ref.journalID, headerDigest: ref.headerDigest, kind: ref.kind, digest: ref.digest, sizeBytes: ref.sizeBytes, present: ok}
		if ok {
			fact.objectIdentity = identity
			if ref.kind == durableRuntimeContentObject {
				inspection := runtimeInspectionByDigest[ref.digest]
				fact.inspection = inspection.digest()
				fact.runtime = &inspection
			} else {
				inspection := recoveryInspectionByDigest[ref.digest]
				if inspection.decision != ref.decision {
					findings.addAt("06000001", admissionCorrupt("admission-recovery-object", "recovery decision differs from generation", nil))
				} else {
					fact.inspection = inspection.digest
					fact.recoveryDecision, fact.recoveryProfile = inspection.decision, inspection.profile
				}
			}
		}
		if !ok {
			fact.runtime = nil
		}
		replayReferences = append(replayReferences, fact)
		if !ok {
			// A temporary object never satisfies a durable reference. Tracking the
			// digest still ensures the full temporary catalog was consumed.
			_ = tempDigests[ref.digest]
			needs = append(needs, admissionRecoveryNeed{ref.kind, ref.digest, ref.sizeBytes})
		}
	}
	sort.Slice(replayReferences, func(i, j int) bool { return admissionReferenceLess(replayReferences[i], replayReferences[j]) })
	sort.Slice(needs, func(i, j int) bool {
		if needs[i].kind != needs[j].kind {
			return needs[i].kind < needs[j].kind
		}
		if needs[i].digest != needs[j].digest {
			return needs[i].digest < needs[j].digest
		}
		return needs[i].sizeBytes < needs[j].sizeBytes
	})
	if findings.first != nil {
		return nil, nil, nil, findings.first
	}
	if len(replayReferences) != len(uniqueReferences) {
		return nil, nil, nil, admissionCorrupt("admission-object-reference", "object reference catalog is incomplete", nil)
	}
	return objects, replayReferences, needs, nil
}

func inspectAdmissionRuntimeObject(raw []byte) (admissionReplayRuntimeInspection, error) {
	decoded, err := decodeRuntimeBundle(raw)
	if err != nil {
		return admissionReplayRuntimeInspection{}, admissionCorrupt("admission-runtime-object", "registered runtime object is invalid", err)
	}
	maxAttempts, statementCounts, err := inspectQuotaBundleFacts(decoded.manifest, decoded.files)
	if err != nil {
		return admissionReplayRuntimeInspection{}, admissionCorrupt("admission-runtime-object", "registered runtime quota facts are invalid", err)
	}
	reservation, err := calculateEvidenceQuotaReservationFromArithmeticFacts(quotaBundleArithmeticFacts{maxAttempts, statementCounts}, false)
	if err != nil {
		return admissionReplayRuntimeInspection{}, admissionCorrupt("admission-runtime-object", "registered runtime reservation is invalid", err)
	}
	return admissionReplayRuntimeInspection{decoded.manifest.ManifestDigest, decoded.manifest.SchemaBundleDigest, maxAttempts, append([]uint64(nil), statementCounts...), reservation}, nil
}

func inspectAdmissionRecoveryObject(raw []byte, digest Digest, size uint64) ([32]byte, Digest, Digest, error) {
	inputs, err := decodeDecisionRecoveryVerificationInputs(raw)
	canonical, canonicalErr := canonicalContractKey(inputs)
	if err != nil || canonicalErr != nil || canonical != string(raw) || uint64(len(raw)) != size || DigestBytes(raw) != digest {
		return [32]byte{}, "", "", admissionCorrupt("admission-recovery-object", "registered recovery object is invalid", err)
	}
	h := sha256.New()
	h.Write([]byte("cloud-agents-platform-admission-recovery-object-inspection/v1\x00"))
	h.Write([]byte(digest))
	writeAdmissionUint(h, size)
	h.Write([]byte(inputs.OldRunnerProjectionDecisionDigest))
	h.Write([]byte(inputs.ProfileDigest))
	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result, inputs.OldRunnerProjectionDecisionDigest, inputs.ProfileDigest, nil
}

func (i admissionReplayRuntimeInspection) digest() [32]byte {
	h := sha256.New()
	h.Write([]byte("cloud-agents-platform-admission-runtime-object-inspection/v1\x00"))
	for _, digest := range []Digest{i.manifestDigest, i.schemaBundleDigest} {
		h.Write([]byte(digest))
	}
	writeAdmissionUint(h, i.maxAttempts)
	writeAdmissionUint(h, uint64(len(i.statementCounts)))
	for _, count := range i.statementCounts {
		writeAdmissionUint(h, count)
	}
	writeAdmissionReservation(h, i.reservation)
	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result
}

func writeAdmissionReservation(h interface{ Write([]byte) (int, error) }, value evidenceQuotaReservation) {
	for _, field := range []uint64{value.ReservedRecords, value.ReservedJournalBytes, uint64(value.ReservedSegments), value.ReservedCheckpointRecords, value.ReservedIndexRecords, value.ReservedIndexBytes, value.ReservedBytes} {
		writeAdmissionUint(h, field)
	}
}

func classifyAdmissionLineageStateCompact(index []LineageIndexFrame, journals []admissionReplayJournal) (admissionReplayLineageState, error) {
	var reserved *GenerationReserved
	var activated *GenerationActivated
	var checkpoint *GenerationCheckpoint
	var superseded *GenerationSuperseded
	for i := range index {
		switch index[i].RecordKind {
		case LineageRecordGenerationReserved:
			reserved, activated, checkpoint, superseded = index[i].Record.Reserved, nil, nil, nil
		case LineageRecordGenerationActivated:
			activated = index[i].Record.Activated
		case LineageRecordGenerationCheckpoint:
			checkpoint = index[i].Record.Checkpoint
		case LineageRecordGenerationSuperseded:
			superseded = index[i].Record.Superseded
		}
	}
	if reserved == nil {
		return admissionLineageEmpty, nil
	}
	wanted := digestRaw(reserved.JournalIdentityDigest)
	var journal admissionReplayJournal
	registered := false
	for _, candidate := range journals {
		if candidate.id == wanted {
			journal, registered = candidate, true
			break
		}
	}
	if activated == nil {
		if registered {
			return admissionLineageReservedHeader, nil
		}
		return admissionLineageReservedUnregistered, nil
	}
	if superseded != nil {
		return admissionLineageSuperseded, nil
	}
	if !registered {
		return "", admissionCorrupt("admission-lineage-state", "active journal is absent", nil)
	}
	count := journal.records
	if checkpoint == nil {
		if count == 1 {
			return admissionLineageActiveInitial, nil
		}
		if count == 2 {
			return admissionLineageActiveUnknownExtension, nil
		}
		return "", admissionCorrupt("admission-lineage-state", "active journal checkpoint lag", nil)
	}
	if checkpoint.JournalNextSequence == count {
		return admissionLineageActiveCheckpointed, nil
	}
	if checkpoint.JournalNextSequence+1 == count {
		return admissionLineageActiveUnknownExtension, nil
	}
	return "", admissionCorrupt("admission-lineage-state", "active journal checkpoint lag", nil)
}

func admissionReplayCanonicalDigest(t *admissionReplayTranscript) [32]byte {
	h := sha256.New()
	h.Write([]byte(admissionReplayDigestDomain))
	writeAdmissionUint(h, t.revision)
	h.Write(t.fullSetDigest[:])
	h.Write(t.target[:])
	if t.targetAbsent {
		h.Write([]byte{1})
	} else {
		h.Write([]byte{0})
	}
	writeAdmissionUint(h, uint64(len(t.lineages)))
	for _, lineage := range t.lineages {
		h.Write(lineage.id[:])
		writeAdmissionFileDigest(h, lineage.index)
		writeAdmissionUint(h, lineage.indexRecords)
		writeAdmissionUint(h, lineage.indexHeaderFramedBytes)
		for _, d := range []Digest{lineage.header.executionLineageDigest, lineage.indexHeaderRecordDigest, lineage.indexTailRecordDigest} {
			h.Write([]byte(d))
			h.Write([]byte{0})
		}
		for _, s := range []string{lineage.header.deploymentID, lineage.header.databaseName, lineage.header.repositoryIdentity, lineage.header.limitsProfile} {
			writeAdmissionString(h, s)
		}
		h.Write([]byte(lineage.state))
		h.Write([]byte{0})
		writeAdmissionUint(h, uint64(len(lineage.journals)))
		for _, journal := range lineage.journals {
			h.Write(journal.id[:])
			h.Write([]byte(journal.tail))
			h.Write([]byte{0})
			writeAdmissionUint(h, journal.records)
			writeAdmissionUint(h, uint64(len(journal.segments)))
			for _, segment := range journal.segments {
				writeAdmissionFileDigest(h, segment.file)
				writeAdmissionUint(h, segment.records)
			}
		}
		writeAdmissionUint(h, uint64(len(lineage.generations)))
		for _, generation := range lineage.generations {
			h.Write([]byte(generation.journalID))
			h.Write([]byte{0})
			for _, d := range []Digest{generation.reservedRecordDigest, generation.runnerProjectionDecisionDigest, generation.schemaBundleDigest, generation.quotaReservationDigest, generation.expectedSegment0HeaderDigest} {
				h.Write([]byte(d))
				h.Write([]byte{0})
			}
			writeAdmissionUint(h, generation.reservedRecords)
			writeAdmissionUint(h, generation.reservedBytes)
			writeAdmissionUint(h, uint64(generation.reservedSegments))
			writeAdmissionOptionalDigest(h, generation.activationRecordDigest)
			writeAdmissionOptionalDigest(h, generation.latestCheckpointRecordDigest)
			writeAdmissionOptionalDigest(h, generation.latestCheckpointTailDigest)
			writeAdmissionOptionalDigest(h, generation.previousCheckpointRecordDigest)
			writeAdmissionUint(h, generation.latestCheckpointNext)
			h.Write([]byte(generation.latestRecoveryState))
			h.Write([]byte{0})
			writeAdmissionOptionalDigest(h, generation.supersessionRecordDigest)
			h.Write([]byte(generation.supersessionAuthorityDigest))
			h.Write([]byte{0})
			writeAdmissionOptionalDigest(h, generation.oldCheckpointRecordDigest)
			writeAdmissionOptionalDigest(h, generation.oldActivationRecordDigest)
			writeAdmissionOptionalDigest(h, generation.oldInitialJournalTailDigest)
			h.Write([]byte(generation.supersessionOutcome))
			h.Write([]byte{0})
			writeAdmissionUint(h, uint64(len(generation.indexDebits)))
			for _, debit := range generation.indexDebits {
				h.Write([]byte(debit.kind))
				h.Write([]byte{0})
				h.Write([]byte(debit.recordDigest))
				h.Write([]byte{0})
				writeAdmissionUint(h, debit.framedBytes)
			}
			if generation.header == nil {
				h.Write([]byte{0})
			} else {
				h.Write([]byte{1})
				writeAdmissionHeaderFacts(h, *generation.header)
			}
			if generation.continuation == nil {
				h.Write([]byte{0})
			} else {
				c := generation.continuation
				h.Write([]byte{1})
				h.Write([]byte(c.startAction))
				h.Write([]byte{0})
				h.Write([]byte(c.migrationID))
				h.Write([]byte{0})
				writeAdmissionUint(h, uint64(c.attemptIndex))
				writeAdmissionOptionalDigest(h, c.previousAttemptTerminalDigest)
				for _, d := range []Digest{c.sourceJournalIdentityDigest, c.sourceCheckpointRecordDigest, c.sourceTerminalDigest} {
					h.Write([]byte(d))
					h.Write([]byte{0})
				}
			}
			if generation.summary == nil {
				h.Write([]byte{0})
			} else {
				h.Write([]byte{1})
				writeAdmissionSummary(h, *generation.summary)
			}
			if generation.latestCheckpointSummary == nil {
				h.Write([]byte{0})
			} else {
				h.Write([]byte{1})
				writeAdmissionSummary(h, *generation.latestCheckpointSummary)
			}
			if generation.plannedSuccessor == nil {
				h.Write([]byte{0})
			} else {
				h.Write([]byte{1})
				writeAdmissionPlannedGeneration(h, *generation.plannedSuccessor)
			}
			if generation.currentTail == nil {
				h.Write([]byte{0})
			} else {
				h.Write([]byte{1})
				writeAdmissionRecoveryTail(h, *generation.currentTail)
			}
			writeAdmissionUint(h, uint64(len(generation.verificationTerminals)))
			for _, event := range generation.verificationTerminals {
				writeAdmissionTerminalEvent(h, event)
			}
			writeAdmissionUint(h, uint64(len(generation.verificationFinals)))
			for _, value := range generation.verificationFinals {
				writeAdmissionTerminalFinal(h, value)
			}
			writeAdmissionUint(h, uint64(len(generation.verificationCommits)))
			for _, value := range generation.verificationCommits {
				writeAdmissionTerminalCommit(h, value)
			}
			writeAdmissionUint(h, uint64(len(generation.verificationRetries)))
			for _, value := range generation.verificationRetries {
				writeAdmissionTerminalRetry(h, value)
			}
			writeAdmissionUint(h, uint64(len(generation.verificationResolutions)))
			for _, value := range generation.verificationResolutions {
				writeAdmissionTerminalResolution(h, value)
			}
			writeAdmissionOpenAttempt(h, generation.verificationOpen)
			h.Write(generation.verificationCatalogContract[:])
			if generation.runtimeInspection == nil {
				h.Write([]byte{0})
			} else {
				h.Write([]byte{1})
				inspectionDigest := generation.runtimeInspection.digest()
				h.Write(inspectionDigest[:])
			}
			writeAdmissionUint(h, generation.remainingIndexRecords)
			writeAdmissionUint(h, generation.remainingIndexBytes)
			if generation.indexHeaderDebited {
				h.Write([]byte{1})
			} else {
				h.Write([]byte{0})
			}
		}
	}
	writeAdmissionUint(h, uint64(len(t.objects)))
	for _, object := range t.objects {
		if object.temporary {
			h.Write([]byte{1})
		} else {
			h.Write([]byte{0})
		}
		h.Write([]byte(object.digest))
		h.Write([]byte{0})
		writeAdmissionUint(h, object.size)
		h.Write(object.identity[:])
	}
	writeAdmissionUint(h, uint64(len(t.recoveryNeeds)))
	for _, need := range t.recoveryNeeds {
		h.Write([]byte(need.kind))
		h.Write([]byte{0})
		h.Write([]byte(need.digest))
		h.Write([]byte{0})
		writeAdmissionUint(h, need.sizeBytes)
	}
	writeAdmissionUint(h, uint64(len(t.references)))
	for _, ref := range t.references {
		h.Write(ref.lineageID[:])
		h.Write(ref.journalID[:])
		h.Write([]byte(ref.headerDigest))
		h.Write([]byte{0})
		h.Write([]byte(ref.kind))
		h.Write([]byte{0})
		h.Write([]byte(ref.digest))
		h.Write([]byte{0})
		writeAdmissionUint(h, ref.sizeBytes)
		if ref.present {
			h.Write([]byte{1})
		} else {
			h.Write([]byte{0})
		}
		h.Write(ref.objectIdentity[:])
		h.Write(ref.inspection[:])
		h.Write([]byte(ref.recoveryDecision))
		h.Write([]byte{0})
		h.Write([]byte(ref.recoveryProfile))
		h.Write([]byte{0})
		if ref.runtime == nil {
			h.Write([]byte{0})
		} else {
			h.Write([]byte{1})
			inspectionDigest := ref.runtime.digest()
			h.Write(inspectionDigest[:])
		}
	}
	writeAdmissionUint(h, t.journalBytes)
	writeAdmissionUint(h, t.journalRecords)
	writeAdmissionUint(h, t.journalReservedBytes)
	writeAdmissionUint(h, t.indexBytes)
	writeAdmissionUint(h, t.indexRecords)
	writeAdmissionUint(h, t.indexReservedBytes)
	var digest [32]byte
	copy(digest[:], h.Sum(nil))
	return digest
}

func writeAdmissionFileDigest(h interface{ Write([]byte) (int, error) }, file admissionReplayFile) {
	writeAdmissionUint(h, uint64(file.ordinal))
	writeAdmissionUint(h, file.size)
	_, _ = h.Write(file.digest[:])
	_, _ = h.Write(file.identity[:])
}

func writeAdmissionUint(h interface{ Write([]byte) (int, error) }, value uint64) {
	var encoded [8]byte
	binary.BigEndian.PutUint64(encoded[:], value)
	_, _ = h.Write(encoded[:])
}

func writeAdmissionOptionalDigest(h interface{ Write([]byte) (int, error) }, value *Digest) {
	if value == nil {
		_, _ = h.Write([]byte{0})
		return
	}
	_, _ = h.Write([]byte{1})
	_, _ = h.Write([]byte(*value))
	_, _ = h.Write([]byte{0})
}
func writeAdmissionHeaderFacts(h interface{ Write([]byte) (int, error) }, value admissionReplayHeaderFacts) {
	for _, d := range []Digest{value.journalIdentityDigest, value.releaseTrustDecisionDigest, value.runnerProjectionDecisionDigest, value.executionLineageDigest, value.outerArtifactDigest, value.recoveryArtifactDigest, value.manifestDigest, value.runnerReleaseDigest, value.schemaBundleDigest, value.authorityProfileDigest, value.authorityBindingDigest, value.quotaReservationDigest} {
		_, _ = h.Write([]byte(d))
		_, _ = h.Write([]byte{0})
	}
	writeAdmissionUint(h, value.outerArtifactSize)
	writeAdmissionUint(h, value.recoveryArtifactSize)
	writeAdmissionUint(h, value.reservedRecords)
	writeAdmissionUint(h, value.reservedBytes)
	writeAdmissionUint(h, uint64(value.reservedSegments))
}

func writeAdmissionSummary(h interface{ Write([]byte) (int, error) }, s evidenceJournalSummary) {
	h.Write([]byte(s.recoveryState))
	h.Write([]byte{0})
	if s.migrationID == nil {
		h.Write([]byte{0})
	} else {
		h.Write([]byte{1})
		h.Write([]byte(*s.migrationID))
		h.Write([]byte{0})
	}
	if s.attemptIndex == nil {
		h.Write([]byte{0})
	} else {
		h.Write([]byte{1})
		writeAdmissionUint(h, uint64(*s.attemptIndex))
	}
	for _, d := range []*Digest{s.lastStatementIntentRecordDigest, s.lastIntermediateEvidenceRecordDigest, s.lastCommitIntentRecordDigest, s.lastTerminalDigest, s.lastResolutionDigest, s.previousAttemptTerminalDigest, s.lastIntermediateStateDigest} {
		writeAdmissionOptionalDigest(h, d)
	}
}
func writeAdmissionString(h interface{ Write([]byte) (int, error) }, value string) {
	writeAdmissionUint(h, uint64(len(value)))
	_, _ = h.Write([]byte(value))
}
func writeAdmissionIDs(h interface{ Write([]byte) (int, error) }, ids ...uint32) {
	for _, id := range ids {
		writeAdmissionUint(h, uint64(id))
	}
}
func writeAdmissionTerminalEvent(h interface{ Write([]byte) (int, error) }, v admissionReplayTerminalEvent) {
	writeAdmissionIDs(h, v.migrationID, v.attemptIndex, v.statementCount, v.lastStatementIndex)
	h.Write([]byte{v.outcome, v.resolutionOutcome})
	writeAdmissionUint(h, uint64(v.flags))
	h.Write(v.terminalDigest[:])
	h.Write(v.statementChain[:])
}
func writeAdmissionTerminalFinal(h interface{ Write([]byte) (int, error) }, v admissionReplayTerminalFinal) {
	writeAdmissionUint(h, uint64(v.ordinal))
	h.Write(v.lastIntermediateRecord[:])
	h.Write(v.preledgerCatalog[:])
}
func writeAdmissionTerminalCommit(h interface{ Write([]byte) (int, error) }, v admissionReplayTerminalCommit) {
	writeAdmissionUint(h, uint64(v.ordinal))
	writeAdmissionUint(h, uint64(v.expectedLedgerLength))
	h.Write(v.commitRecord[:])
	h.Write(v.commitBody[:])
	h.Write(v.previousAttemptTerminal[:])
	h.Write(v.attemptPredecessorCatalog[:])
	h.Write(v.lastIntermediateState[:])
}
func writeAdmissionTerminalRetry(h interface{ Write([]byte) (int, error) }, v admissionReplayTerminalRetry) {
	writeAdmissionUint(h, uint64(v.ordinal))
	h.Write([]byte{v.proofKind, v.commitRejectedReason})
	h.Write(v.attemptPredecessorCatalog[:])
	h.Write(v.observedCatalog[:])
	h.Write(v.ledgerPrefix[:])
	h.Write(v.authorityResult[:])
}
func writeAdmissionTerminalResolution(h interface{ Write([]byte) (int, error) }, v admissionReplayTerminalResolution) {
	writeAdmissionUint(h, uint64(v.ordinal))
	h.Write(v.resolutionDigest[:])
}
func writeAdmissionOpenAttempt(h interface{ Write([]byte) (int, error) }, v *admissionReplayOpenAttempt) {
	if v == nil {
		h.Write([]byte{0})
		return
	}
	h.Write([]byte{1})
	writeAdmissionIDs(h, v.migrationID, v.attemptIndex, v.statementCount, v.lastStatementIndex)
	h.Write(v.statementChain[:])
	if v.commitPresent {
		h.Write([]byte{1})
		h.Write(v.commitRecord[:])
		h.Write(v.commitBody[:])
		h.Write(v.previousAttemptTerminal[:])
		h.Write(v.attemptPredecessorCatalog[:])
		h.Write(v.lastIntermediateState[:])
		writeAdmissionUint(h, uint64(v.expectedLedgerLength))
	} else {
		h.Write([]byte{0})
	}
}
func writeAdmissionRecoveryTail(h interface{ Write([]byte) (int, error) }, t admissionReplayRecoveryTail) {
	h.Write([]byte(t.migrationID))
	h.Write([]byte{0})
	writeAdmissionUint(h, uint64(t.attemptIndex))
	writeTailRecord := func(present bool, sequence uint64, previous *Digest, digest, bodyDigest Digest) {
		if !present {
			h.Write([]byte{0})
			return
		}
		h.Write([]byte{1})
		writeAdmissionUint(h, sequence)
		writeAdmissionOptionalDigest(h, previous)
		h.Write([]byte(digest))
		h.Write([]byte{0})
		h.Write([]byte(bodyDigest))
		h.Write([]byte{0})
	}
	if t.intent == nil {
		writeTailRecord(false, 0, nil, "", "")
	} else {
		writeTailRecord(true, t.intent.sequence, t.intent.previousRecordDigest, t.intent.recordDigest, t.intent.bodyDigest)
	}
	if t.intermediate == nil {
		writeTailRecord(false, 0, nil, "", "")
	} else {
		writeTailRecord(true, t.intermediate.sequence, t.intermediate.previousRecordDigest, t.intermediate.recordDigest, t.intermediate.bodyDigest)
	}
	if t.commit == nil {
		writeTailRecord(false, 0, nil, "", "")
	} else {
		writeTailRecord(true, t.commit.sequence, t.commit.previousRecordDigest, t.commit.recordDigest, t.commit.bodyDigest)
	}
	if t.terminal == nil {
		writeTailRecord(false, 0, nil, "", "")
	} else {
		writeTailRecord(true, t.terminal.sequence, t.terminal.previousRecordDigest, t.terminal.recordDigest, t.terminal.bodyDigest)
	}
	if t.resolution == nil {
		writeTailRecord(false, 0, nil, "", "")
	} else {
		writeTailRecord(true, t.resolution.sequence, t.resolution.previousRecordDigest, t.resolution.recordDigest, t.resolution.bodyDigest)
	}
}

func cloneAdmissionRecoveryTail(t *admissionReplayRecoveryTail) *admissionReplayRecoveryTail {
	if t == nil {
		return nil
	}
	owned := *t
	if t.intent != nil {
		v := *t.intent
		v.previousRecordDigest = cloneDigestPointer(t.intent.previousRecordDigest)
		v.body = cloneProjectionValue(t.intent.body)
		owned.intent = &v
	}
	if t.intermediate != nil {
		v := *t.intermediate
		v.previousRecordDigest = cloneDigestPointer(t.intermediate.previousRecordDigest)
		v.body = cloneProjectionValue(t.intermediate.body)
		owned.intermediate = &v
	}
	if t.commit != nil {
		v := *t.commit
		v.previousRecordDigest = cloneDigestPointer(t.commit.previousRecordDigest)
		v.body = cloneProjectionValue(t.commit.body)
		owned.commit = &v
	}
	if t.terminal != nil {
		v := *t.terminal
		v.previousRecordDigest = cloneDigestPointer(t.terminal.previousRecordDigest)
		v.body = cloneProjectionValue(t.terminal.body)
		owned.terminal = &v
	}
	if t.resolution != nil {
		v := *t.resolution
		v.previousRecordDigest = cloneDigestPointer(t.resolution.previousRecordDigest)
		v.body = cloneProjectionValue(t.resolution.body)
		owned.resolution = &v
	}
	return &owned
}

func cloneAdmissionGeneration(g admissionReplayGeneration) admissionReplayGeneration {
	owned := g
	owned.activationRecordDigest = cloneDigestPointer(g.activationRecordDigest)
	owned.latestCheckpointRecordDigest = cloneDigestPointer(g.latestCheckpointRecordDigest)
	owned.latestCheckpointTailDigest = cloneDigestPointer(g.latestCheckpointTailDigest)
	owned.previousCheckpointRecordDigest = cloneDigestPointer(g.previousCheckpointRecordDigest)
	owned.supersessionRecordDigest = cloneDigestPointer(g.supersessionRecordDigest)
	owned.oldCheckpointRecordDigest = cloneDigestPointer(g.oldCheckpointRecordDigest)
	owned.oldActivationRecordDigest = cloneDigestPointer(g.oldActivationRecordDigest)
	owned.oldInitialJournalTailDigest = cloneDigestPointer(g.oldInitialJournalTailDigest)
	owned.indexDebits = append([]admissionReplayIndexDebit(nil), g.indexDebits...)
	if g.header != nil {
		v := *g.header
		owned.header = &v
	}
	if g.continuation != nil {
		v := *g.continuation
		v.previousAttemptTerminalDigest = cloneDigestPointer(g.continuation.previousAttemptTerminalDigest)
		owned.continuation = &v
	}
	if g.summary != nil {
		v := cloneEvidenceJournalSummary(*g.summary)
		owned.summary = &v
	}
	if g.latestCheckpointSummary != nil {
		v := cloneEvidenceJournalSummary(*g.latestCheckpointSummary)
		owned.latestCheckpointSummary = &v
	}
	if g.plannedSuccessor != nil {
		v := cloneAdmissionGeneration(*g.plannedSuccessor)
		owned.plannedSuccessor = &v
	}
	owned.currentTail = cloneAdmissionRecoveryTail(g.currentTail)
	owned.verificationTerminals = append([]admissionReplayTerminalEvent(nil), g.verificationTerminals...)
	owned.verificationFinals = append([]admissionReplayTerminalFinal(nil), g.verificationFinals...)
	owned.verificationCommits = append([]admissionReplayTerminalCommit(nil), g.verificationCommits...)
	owned.verificationRetries = append([]admissionReplayTerminalRetry(nil), g.verificationRetries...)
	owned.verificationResolutions = append([]admissionReplayTerminalResolution(nil), g.verificationResolutions...)
	if g.verificationOpen != nil {
		v := *g.verificationOpen
		owned.verificationOpen = &v
	}
	if g.runtimeInspection != nil {
		v := *g.runtimeInspection
		v.statementCounts = append([]uint64(nil), g.runtimeInspection.statementCounts...)
		owned.runtimeInspection = &v
	}
	return owned
}
func writeAdmissionPlannedGeneration(h interface{ Write([]byte) (int, error) }, g admissionReplayGeneration) {
	for _, d := range []Digest{g.journalID, g.runnerProjectionDecisionDigest, g.schemaBundleDigest, g.quotaReservationDigest, g.expectedSegment0HeaderDigest} {
		h.Write([]byte(d))
		h.Write([]byte{0})
	}
	writeAdmissionUint(h, g.reservedRecords)
	writeAdmissionUint(h, g.reservedBytes)
	writeAdmissionUint(h, uint64(g.reservedSegments))
	if g.header == nil {
		h.Write([]byte{0})
	} else {
		h.Write([]byte{1})
		writeAdmissionHeaderFacts(h, *g.header)
	}
	if g.continuation == nil {
		h.Write([]byte{0})
	} else {
		c := g.continuation
		h.Write([]byte{1})
		h.Write([]byte(c.startAction))
		h.Write([]byte{0})
		h.Write([]byte(c.migrationID))
		h.Write([]byte{0})
		writeAdmissionUint(h, uint64(c.attemptIndex))
		writeAdmissionOptionalDigest(h, c.previousAttemptTerminalDigest)
		for _, d := range []Digest{c.sourceJournalIdentityDigest, c.sourceCheckpointRecordDigest, c.sourceTerminalDigest} {
			h.Write([]byte(d))
			h.Write([]byte{0})
		}
	}
}

func cloneAdmissionReplayTranscript(value *admissionReplayTranscript) *admissionReplayTranscript {
	if value == nil {
		return nil
	}
	owned := *value
	owned.lineages = make([]admissionReplayLineage, len(value.lineages))
	for i := range value.lineages {
		owned.lineages[i] = value.lineages[i]
		owned.lineages[i].generations = make([]admissionReplayGeneration, len(value.lineages[i].generations))
		for j := range value.lineages[i].generations {
			owned.lineages[i].generations[j] = cloneAdmissionGeneration(value.lineages[i].generations[j])
		}
		owned.lineages[i].journals = make([]admissionReplayJournal, len(value.lineages[i].journals))
		for j := range value.lineages[i].journals {
			owned.lineages[i].journals[j] = value.lineages[i].journals[j]
			owned.lineages[i].journals[j].segments = append([]admissionReplaySegment(nil), value.lineages[i].journals[j].segments...)
		}
	}
	owned.objects = append([]admissionReplayObject(nil), value.objects...)
	owned.recoveryNeeds = append([]admissionRecoveryNeed(nil), value.recoveryNeeds...)
	owned.references = make([]admissionReplayReference, len(value.references))
	for index := range value.references {
		owned.references[index] = value.references[index]
		if value.references[index].runtime != nil {
			inspection := *value.references[index].runtime
			inspection.statementCounts = append([]uint64(nil), value.references[index].runtime.statementCounts...)
			owned.references[index].runtime = &inspection
		}
	}
	return &owned
}

func admissionObjectLess(a, b admissionReplayObject) bool {
	if a.temporary != b.temporary {
		return !a.temporary
	}
	if a.digest != b.digest {
		return a.digest < b.digest
	}
	return bytes.Compare(a.identity[:], b.identity[:]) < 0
}

func admissionReferenceLess(a, b admissionReplayReference) bool {
	if comparison := bytes.Compare(a.lineageID[:], b.lineageID[:]); comparison != 0 {
		return comparison < 0
	}
	if comparison := bytes.Compare(a.journalID[:], b.journalID[:]); comparison != 0 {
		return comparison < 0
	}
	if a.headerDigest != b.headerDigest {
		return a.headerDigest < b.headerDigest
	}
	if a.kind != b.kind {
		return a.kind < b.kind
	}
	if a.digest != b.digest {
		return a.digest < b.digest
	}
	return a.sizeBytes < b.sizeBytes
}

func strictRawDigestOrder(values [][32]byte) bool {
	for i := 1; i < len(values); i++ {
		if bytes.Compare(values[i-1][:], values[i][:]) >= 0 {
			return false
		}
	}
	return true
}

func rawDigestIndex(values [][32]byte, target [32]byte) int {
	index := sort.Search(len(values), func(i int) bool { return bytes.Compare(values[i][:], target[:]) >= 0 })
	if index < len(values) && values[index] == target {
		return index
	}
	return -1
}

func rawDigestContains(values [][32]byte, target [32]byte) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func digestString(raw [32]byte) Digest { return Digest("sha256:" + hex.EncodeToString(raw[:])) }

func digestRaw(value Digest) [32]byte {
	var result [32]byte
	raw, err := hex.DecodeString(value.Hex())
	if err == nil && len(raw) == len(result) {
		copy(result[:], raw)
	}
	return result
}

func contextAdmissionError(ctx context.Context) error {
	if ctx == nil {
		return admissionFailed("admission-context", "context is unavailable", nil)
	}
	if err := ctx.Err(); err != nil {
		return mapEvidenceAdmissionError(err, "admission-context")
	}
	return nil
}

func admissionCheckedAdd(left, right uint64) (uint64, error) {
	if left > math.MaxUint64-right {
		return 0, admissionCorrupt("admission-usage", "stored usage overflows", nil)
	}
	return left + right, nil
}

func admissionSaturatingAdd(left, right uint64) (uint64, error) {
	if left > math.MaxUint64-right {
		return math.MaxUint64, admissionCorrupt("admission-usage", "stored usage overflows", nil)
	}
	return left + right, nil
}

func mapEvidenceAdmissionError(err error, op string) error {
	switch {
	case errors.Is(err, context.Canceled):
		return fail(CodeContextCanceled, op, "admission replay was canceled", nil)
	case errors.Is(err, context.DeadlineExceeded):
		return fail(CodeDeadlineExceeded, op, "admission replay deadline exceeded", nil)
	case errors.Is(err, evidencefs.ErrCorrupt), errors.Is(err, evidencefs.ErrLimit), errors.Is(err, evidencefs.ErrInvalidInput):
		return admissionCorrupt(op, "stored evidence inventory is invalid", nil)
	default:
		return admissionFailed(op, "evidence inventory operation failed", nil)
	}
}

func admissionCorrupt(op, message string, cause error) error {
	return fail(CodeEvidenceJournalCorrupt, op, message, cause)
}
func admissionFailed(op, message string, cause error) error {
	return fail(CodeEvidenceJournalFailed, op, message, cause)
}
