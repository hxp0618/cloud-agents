package migration

import (
	"context"
	"sync/atomic"
	"testing"
)

func TestBrandNewAdmissionFramesBindCurrentCandidateAndExactQuota(t *testing.T) {
	candidate := quotaCandidateForBundle(t, quotaAdmissionBundleForTest(t), []byte("recovery"))
	bindings, err := candidate.verifiedRun.currentDecision.decision.runnerProjectionBindings()
	if err != nil {
		t.Fatal(err)
	}
	history := &VerifiedAdmissionHistory{
		candidateBinding: candidate.binding, target: digestRaw(bindings.executionLineageDigest), rootFacts: rootFactsForTest(t, nil),
		reservation: evidenceQuotaReservation{ReservedRecords: 2, ReservedJournalBytes: 3, ReservedSegments: 1, ReservedIndexRecords: 4, ReservedIndexBytes: lineageRecordFrameLimits[LineageRecordHeader] + lineageRecordFrameLimits[LineageRecordGenerationReserved], ReservedBytes: 3 + lineageRecordFrameLimits[LineageRecordHeader] + lineageRecordFrameLimits[LineageRecordGenerationReserved]},
	}
	lineage, reserved, lineageBytes, reservedBytes, err := buildBrandNewAdmissionFrames(history, candidate)
	if err != nil {
		t.Fatal(err)
	}
	if lineage.Sequence != 0 || lineage.Record.Header == nil || reserved.Sequence != 1 || reserved.PreviousRecordDigest == nil || *reserved.PreviousRecordDigest != lineage.RecordDigest || reserved.Record.Reserved == nil || reserved.Record.Reserved.PlannedSegment0Header.OuterArtifactDigest != candidate.runtimeArtifact.digest || reserved.Record.Reserved.PlannedSegment0Header.DecisionRecoveryArtifactSHA256 != candidate.decisionRecoveryArtifact.digest || len(lineageBytes) == 0 || len(reservedBytes) == 0 {
		t.Fatalf("brand-new frames are incomplete: lineage=%+v reserved=%+v", lineage, reserved)
	}
	if got, err := EncodeCanonicalLineageFrame(lineage); err != nil || string(got) != string(lineageBytes) {
		t.Fatalf("lineage bytes drifted: %v", err)
	}
	if got, err := EncodeCanonicalLineageFrame(reserved); err != nil || string(got) != string(reservedBytes) {
		t.Fatalf("reserved bytes drifted: %v", err)
	}

	present := *history
	present.rootFacts.targetIndexPresent = true
	present.rootFacts.targetIndexRecords, present.rootFacts.targetIndexBytes = 1, uint64(len(lineageBytes))
	present.targetState, present.targetIndexRecords, present.targetIndexTail = admissionLineageEmpty, 1, lineage.RecordDigest
	present.targetHeader = admissionReplayLineageHeader{bindings.executionLineageDigest, bindings.deploymentID, bindings.expectedDatabaseName, bindings.releaseSubject.RepositoryIdentity, LineageLimitsProfile}
	_, presentReserved, gotLineage, _, err := buildBrandNewAdmissionFrames(&present, candidate)
	if err != nil || string(gotLineage) != string(lineageBytes) || presentReserved.Sequence != 1 || presentReserved.PreviousRecordDigest == nil || *presentReserved.PreviousRecordDigest != lineage.RecordDigest {
		t.Fatalf("registered-empty target did not reuse exact header: %v", err)
	}
	present.targetState = admissionLineageActiveInitial
	if _, _, _, _, err := buildBrandNewAdmissionFrames(&present, candidate); !IsCode(err, CodeEvidenceRecoveryRequired) {
		t.Fatalf("active target entered brand-new path: %v", err)
	}
}

func TestAdmissionPlanRequiresRegisteredLiveHistory(t *testing.T) {
	candidate := quotaCandidateForBundle(t, quotaAdmissionBundleForTest(t), []byte("recovery"))
	if plan, err := bindVerifiedAdmissionPlan(context.Background(), &VerifiedAdmissionHistory{}, candidate); plan != nil || !IsCode(err, CodeEvidenceRecoveryRequired) {
		t.Fatalf("literal history minted plan: plan=%+v err=%v", plan, err)
	}
	if validVerifiedAdmissionPlan(&VerifiedAdmissionPlan{}, &VerifiedAdmissionHistory{}, candidate) {
		t.Fatal("literal plan passed registry validation")
	}
}

func TestAdmissionPlanDigestBindsExactFramesAndRejectsCopy(t *testing.T) {
	candidate := quotaCandidateForBundle(t, quotaAdmissionBundleForTest(t), []byte("recovery"))
	history := &VerifiedAdmissionHistory{binding: &verifiedAdmissionHistoryBinding{canonical: [32]byte{1}}}
	plan := &VerifiedAdmissionPlan{history: history, candidateBinding: candidate.binding, lineageHeaderBytes: []byte("header"), reservedFrameBytes: []byte("reserved"), consumed: &atomic.Bool{}}
	binding := &verifiedAdmissionPlanBinding{plan: plan, history: history, candidate: candidate.binding}
	plan.binding, binding.canonical = binding, admissionPlanDigest(plan)
	want := binding.canonical
	copyPlan := *plan
	copyPlan.lineageHeaderBytes = append([]byte(nil), plan.lineageHeaderBytes...)
	copyPlan.lineageHeaderBytes[0] ^= 1
	if admissionPlanDigest(&copyPlan) == want {
		t.Fatal("lineage header byte mutation did not change plan digest")
	}
	copyPlan = *plan
	copyPlan.reservedFrameBytes = append([]byte(nil), plan.reservedFrameBytes...)
	copyPlan.reservedFrameBytes[0] ^= 1
	if admissionPlanDigest(&copyPlan) == want {
		t.Fatal("reservation byte mutation did not change plan digest")
	}
	copyPlan = *plan
	copyPlan.reservedFrame.Sequence++
	if admissionPlanFramesExact(&copyPlan) {
		t.Fatal("reservation struct mutation still matched frozen bytes")
	}
	verifiedAdmissionPlanRegistry.Store(binding, binding.canonical)
	if validVerifiedAdmissionPlan(&copyPlan, history, candidate) {
		t.Fatal("copied plan reused original registry binding")
	}
}
