package migration

import (
	"context"
	"testing"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/evidencefs"
)

func TestSuccessorIndexConcreteStagesRejectLiterals(t *testing.T) {
	candidate := quotaCandidateForBundle(t, quotaAdmissionBundleForTest(t), []byte("successor-index-literal"))
	superseded, err := (&SuccessorReceiptBoundReady{}).AppendGenerationSuperseded(context.Background(), candidate)
	if superseded.Next() != nil || superseded.Outcome() != evidencefs.AdmissionTransitionPreMutationFailure || superseded.CandidateKind() != "generation_superseded" || superseded.CandidateSequence() != 6 || !IsCode(err, CodeEvidenceRecoveryRequired) {
		t.Fatalf("literal receipt-bound state escaped: result=%+v err=%v", superseded, err)
	}
	reserved, err := (&SuccessorAdjacentReserveReady{}).AppendGenerationReserved(context.Background(), candidate)
	if reserved.Next() != nil || reserved.Outcome() != evidencefs.AdmissionTransitionPreMutationFailure || reserved.CandidateKind() != "generation_reserved" || reserved.CandidateSequence() != 7 || !IsCode(err, CodeEvidenceRecoveryRequired) {
		t.Fatalf("literal adjacent state escaped: result=%+v err=%v", reserved, err)
	}
}

func TestSuccessorPendingReservationPrefixIsStructurallyClosed(t *testing.T) {
	fixture := fixtureObject(t, migrationFixturePath(t, "golden/lineage-index-chain-v1.json"))
	base := decodeLineageFrames(t, fixture["frames"])
	if len(base) < 5 || base[1].Record.Reserved == nil || base[3].Record.Checkpoint == nil || base[4].Record.Superseded == nil || base[3].Record.Checkpoint.LastTerminalDigest == nil {
		t.Fatal("golden fixture lacks successor construction facts")
	}
	chain := cloneProjectionValue(base[:5])
	planned := cloneProjectionValue(*chain[1].Record.Reserved)
	plannedHeader := cloneProjectionValue(planned.PlannedSegment0Header)
	plannedHeader.OuterArtifactDigest = DigestBytes([]byte("successor-pending-runtime"))
	journalID, err := JournalIdentityDigest(plannedHeader)
	if err != nil {
		t.Fatal(err)
	}
	planned.JournalIdentityDigest = journalID
	plannedHeader.JournalIdentityDigest = journalID
	planned.Continuation = &LineageContinuationContext{
		StartAction: "begin_first_attempt_next_entry", MigrationID: "000002", AttemptIndex: 1,
		SourceJournalIdentityDigest:  chain[4].Record.Superseded.OldJournalIdentityDigest,
		SourceCheckpointRecordDigest: chain[3].RecordDigest, SourceTerminalDigest: *chain[3].Record.Checkpoint.LastTerminalDigest,
	}
	planned.QuotaReservationDigest, err = QuotaReservationDigest(planned)
	if err != nil {
		t.Fatal(err)
	}
	plannedHeader.QuotaReservationDigest = planned.QuotaReservationDigest
	planned.PlannedSegment0Header = plannedHeader
	header := EvidenceFrame{FormatVersion: EvidenceFrameFormat, Sequence: 0, RecordKind: EvidenceRecordHeader, Record: EvidenceRecord{Header: &planned.PlannedSegment0Header}}
	header.RecordDigest, err = header.ComputeDigest()
	if err != nil {
		t.Fatal(err)
	}
	planned.ExpectedSegment0HeaderDigest = header.RecordDigest
	chain[4].Record.Superseded.Outcome = "exact_committed_continue_successor"
	chain[4].Record.Superseded.PlannedGenerationReserved = &planned
	redigestStructuralLineageFrames(t, chain)
	if _, err := scanLineageChainStructure(chain); err != nil {
		t.Fatalf("durable superseded-pending-reservation prefix rejected: %v", err)
	}
	chain = append(chain, LineageIndexFrame{FormatVersion: LineageFrameFormat, RecordKind: LineageRecordGenerationReserved, Record: LineageIndexRecord{Reserved: &planned}})
	redigestStructuralLineageFrames(t, chain)
	if _, err := scanLineageChainStructure(chain); err != nil {
		t.Fatalf("adjacent durable reservation prefix rejected: %v", err)
	}
	reserved := chain[len(chain)-1]
	superseded := chain[len(chain)-2]
	if reserved.PreviousRecordDigest == nil || *reserved.PreviousRecordDigest != superseded.RecordDigest || superseded.Record.Superseded == nil || superseded.Record.Superseded.PlannedGenerationReserved == nil || !canonicalEqual(*superseded.Record.Superseded.PlannedGenerationReserved, *reserved.Record.Reserved) {
		t.Fatal("golden successor pair is not byte-adjacent and body-exact")
	}
}

func TestConsumedSuccessorIndexAuthorityRejectsLiterals(t *testing.T) {
	candidate := quotaCandidateForBundle(t, quotaAdmissionBundleForTest(t), []byte("successor-index-consumed-literal"))
	if validConsumedSuccessorAdmissionState(&SuccessorReceiptBoundReady{}, &successorAdmissionState{}, successorAdmissionReceiptBound, candidate) {
		t.Fatal("literal consumed successor authority was accepted")
	}
	if validSuccessorInventoryIndex(&successorAdmissionState{}) {
		t.Fatal("literal successor index authority was accepted")
	}
}
