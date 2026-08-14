package migration

import (
	"context"
	"testing"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/evidencefs"
)

func TestSuccessorActivationConcreteStagesRejectLiterals(t *testing.T) {
	candidate := quotaCandidateForBundle(t, quotaAdmissionBundleForTest(t), []byte("successor-activation-literal"))
	header, err := (&SuccessorReservedDurablePermit{}).CreateGenerationHeader(context.Background(), candidate)
	if header.Next() != nil || header.Outcome() != evidencefs.AdmissionTransitionPreMutationFailure || header.CandidateKind() != "generation_header" || header.CandidateSequence() != 8 || !IsCode(err, CodeEvidenceRecoveryRequired) {
		t.Fatalf("literal reserved authority escaped: result=%+v err=%v", header, err)
	}
	activated, err := (&SuccessorHeaderDurablePermit{}).AppendGenerationActivated(context.Background(), candidate)
	if activated.Next() != nil || activated.Outcome() != evidencefs.AdmissionTransitionPreMutationFailure || activated.CandidateKind() != "generation_activated" || activated.CandidateSequence() != 9 || !IsCode(err, CodeEvidenceRecoveryRequired) {
		t.Fatalf("literal header authority escaped: result=%+v err=%v", activated, err)
	}
}

func TestSuccessorActivatedFrameUsesAdjacentReservedSequence(t *testing.T) {
	history, candidate, subject, authorityDigest, execution := successorAdmissionPlanFixture(t)
	_, reserved, header, _, _, err := buildSuccessorAdmissionFrames(history, candidate, subject, authorityDigest, execution)
	if err != nil {
		t.Fatal(err)
	}
	if reserved.Sequence == 1 {
		t.Fatal("successor fixture unexpectedly used brand-new reservation position")
	}
	activated, raw, err := buildSuccessorActivatedFrame(reserved, header)
	if err != nil {
		t.Fatal(err)
	}
	if activated.Record.Activated == nil || activated.Sequence != reserved.Sequence+1 || activated.PreviousRecordDigest == nil || *activated.PreviousRecordDigest != reserved.RecordDigest || activated.Record.Activated.GenerationReservedRecordDigest != reserved.RecordDigest || activated.Record.Activated.Segment0HeaderDigest != header.RecordDigest || activated.Record.Activated.InitialJournalTailDigest != header.RecordDigest {
		t.Fatal("successor activation did not retain adjacent reservation/header identities")
	}
	want, err := EncodeCanonicalLineageFrame(activated)
	if err != nil || string(raw) != string(want) {
		t.Fatal("successor activation bytes are not canonical")
	}
	if _, _, err := buildAdmissionActivatedFrame(reserved, header); err == nil {
		t.Fatal("brand-new activation builder accepted successor reservation position")
	}
}

func TestSuccessorActivatedFrameRejectsReservationAndHeaderSwaps(t *testing.T) {
	history, candidate, subject, authorityDigest, execution := successorAdmissionPlanFixture(t)
	_, reserved, header, _, _, err := buildSuccessorAdmissionFrames(history, candidate, subject, authorityDigest, execution)
	if err != nil {
		t.Fatal(err)
	}
	wrongHeader := cloneProjectionValue(header)
	wrongHeader.RecordDigest = testDigest("wrong-successor-header")
	if _, _, err := buildSuccessorActivatedFrame(reserved, wrongHeader); err == nil {
		t.Fatal("mismatched successor header was accepted")
	}
	wrongReserved := cloneProjectionValue(reserved)
	wrongReserved.RecordDigest = testDigest("wrong-successor-reserved")
	if _, _, err := buildSuccessorActivatedFrame(wrongReserved, header); err == nil {
		t.Fatal("mismatched successor reservation was accepted")
	}
	maxSequence := cloneProjectionValue(reserved)
	maxSequence.Sequence = maxJSONInteger
	maxSequence.RecordDigest, _ = maxSequence.ComputeDigest()
	if _, _, err := buildSuccessorActivatedFrame(maxSequence, header); err == nil {
		t.Fatal("maximum successor reservation sequence overflow was accepted")
	}
}

func TestSuccessorActivationHeaderCopyPreservesOpaqueOwner(t *testing.T) {
	history, candidate, subject, authorityDigest, execution := successorAdmissionPlanFixture(t)
	_, reservedFrame, _, _, _, err := buildSuccessorAdmissionFrames(history, candidate, subject, authorityDigest, execution)
	if err != nil {
		t.Fatal(err)
	}
	reserved := cloneProjectionValue(*reservedFrame.Record.Reserved)
	value := ownedActivationHeader{
		header: reserved.PlannedSegment0Header,
		generation: generationIdentity{
			owner: candidate.owner, executionLineageDigest: reserved.ExecutionLineageDigest,
			journalIdentityDigest: reserved.JournalIdentityDigest, runnerProjectionDecisionDigest: reserved.RunnerProjectionDecisionDigest,
			schemaBundleDigest: reserved.SchemaBundleDigest,
		},
		reserved: reserved,
	}
	cloned := cloneSuccessorActivationHeader(value)
	if cloned.generation.owner != candidate.owner || !sameGenerationIdentity(cloned.generation, value.generation) || !canonicalEqual(cloned.header, value.header) || !canonicalEqual(cloned.reserved, value.reserved) {
		t.Fatal("successor activation header copy lost opaque owner or canonical bodies")
	}
}
