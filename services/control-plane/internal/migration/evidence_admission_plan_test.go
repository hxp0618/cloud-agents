package migration

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/evidencefs"
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

func TestAdmissionPermitRejectsLiteralAndCrossBoundInputs(t *testing.T) {
	candidate := quotaCandidateForBundle(t, quotaAdmissionBundleForTest(t), []byte("recovery"))
	inventory := &evidencefs.AdmissionInventory{}
	if permit, err := bindAdmissionPermit(context.Background(), inventory, &evidencefs.AdmissionMutationToken{}, &VerifiedAdmissionHistory{}, &VerifiedAdmissionPlan{}, candidate); permit != nil || !IsCode(err, CodeEvidenceRecoveryRequired) {
		t.Fatalf("literal inputs minted permit: permit=%+v err=%v", permit, err)
	}
	if validAdmissionPermit(&AdmissionPermit{}, inventory, candidate) {
		t.Fatal("literal permit passed validation")
	}
	if result, err := (&AdmissionPermit{}).CreateTargetLineage(context.Background(), candidate); result.Next() != nil || result.Outcome() != evidencefs.AdmissionTransitionPreMutationFailure || !IsCode(err, CodeEvidenceRecoveryRequired) {
		t.Fatalf("literal permit entered transition: result=%+v err=%v", result, err)
	}
	if validRegisteredAdmissionPermit(&RegisteredAdmissionPermit{}, inventory, candidate) {
		t.Fatal("literal registered permit passed validation")
	}
}

func TestAdmissionMutationErrorMappingAndClosedResult(t *testing.T) {
	if err := mapAdmissionMutationError(errors.Join(evidencefs.ErrUnknown, context.Canceled), "test"); !IsCode(err, CodeEvidenceJournalFailed) {
		t.Fatalf("unknown mutation leaked context authority: %v", err)
	}
	if err := mapAdmissionMutationError(context.Canceled, "test"); !IsCode(err, CodeContextCanceled) {
		t.Fatalf("pre-mutation context mapping changed: %v", err)
	}
	if err := mapAdmissionMutationError(evidencefs.ErrLimit, "test"); !IsCode(err, CodeEvidenceJournalLimitExceeded) {
		t.Fatalf("candidate limit became stored corruption: %v", err)
	}
	if err := mapAdmissionMutationError(evidencefs.ErrLeaseInvalid, "test"); !IsCode(err, CodeEvidenceJournalFailed) {
		t.Fatalf("lease failure mapping changed: %v", err)
	}
	if err := admissionPostMutationFailure("test"); !IsCode(err, CodeEvidenceJournalFailed) {
		t.Fatalf("post-mutation failure became retryable: %v", err)
	}
	result := AdmissionPermitTransitionResult{outcome: evidencefs.AdmissionTransitionUnknown, candidateKind: "target_lineage", candidateDigest: [32]byte{1}, candidateSequence: 2, candidateRevision: 4, previousRevision: 3}
	if result.Next() != nil || result.CandidateKind() != "target_lineage" || result.CandidateDigest() != ([32]byte{1}) || result.CandidateSequence() != 2 || result.CandidateRevision() != 4 || result.PreviousRevision() != 3 {
		t.Fatalf("closed diagnosis changed: %+v", result)
	}
}

func TestRegisteredAdmissionPermitDigestRejectsCopyAndMutation(t *testing.T) {
	candidate := quotaCandidateForBundle(t, quotaAdmissionBundleForTest(t), []byte("recovery"))
	prior := &AdmissionPermit{binding: &admissionPermitBinding{canonical: [32]byte{1}}}
	plan := &VerifiedAdmissionPlan{binding: &verifiedAdmissionPlanBinding{canonical: [32]byte{2}}}
	permit := &RegisteredAdmissionPermit{prior: prior, plan: plan, candidateBinding: candidate.binding, target: [32]byte{3}, fullSet: [32]byte{4}, indexDigest: [32]byte{5}, revision: 1}
	permit.self = permit
	want := registeredAdmissionPermitDigest(permit)
	if want == ([32]byte{}) {
		t.Fatal("registered permit digest is empty")
	}
	copyPermit := *permit
	if registeredAdmissionPermitDigest(&copyPermit) != ([32]byte{}) {
		t.Fatal("registered permit copy retained self binding")
	}
	for name, mutate := range map[string]func(*RegisteredAdmissionPermit){
		"target":   func(v *RegisteredAdmissionPermit) { v.target[0]++ },
		"full set": func(v *RegisteredAdmissionPermit) { v.fullSet[0]++ },
		"index":    func(v *RegisteredAdmissionPermit) { v.indexDigest[0]++ },
		"revision": func(v *RegisteredAdmissionPermit) { v.revision++ },
		"reused":   func(v *RegisteredAdmissionPermit) { v.reused = !v.reused },
	} {
		t.Run(name, func(t *testing.T) {
			value := *permit
			value.self = &value
			mutate(&value)
			if registeredAdmissionPermitDigest(&value) == want {
				t.Fatal("mutation did not change registered permit digest")
			}
		})
	}
}

func TestRuntimePublishedPermitRejectsLiteralCopyAndMutation(t *testing.T) {
	candidate := quotaCandidateForBundle(t, quotaAdmissionBundleForTest(t), []byte("recovery"))
	inventory := &evidencefs.AdmissionInventory{}
	if result, err := (&RegisteredAdmissionPermit{}).PublishRuntime(context.Background(), candidate); result.Next() != nil || result.Outcome() != evidencefs.AdmissionTransitionPreMutationFailure || result.CandidateKind() != "runtime_object" || !IsCode(err, CodeEvidenceRecoveryRequired) {
		t.Fatalf("literal permit entered runtime publish: result=%+v err=%v", result, err)
	}
	if validRuntimePublishedPermit(&RuntimePublishedPermit{}, inventory, candidate) {
		t.Fatal("literal runtime-published permit passed validation")
	}
	prior := &RegisteredAdmissionPermit{binding: &registeredAdmissionPermitBinding{canonical: [32]byte{1}}}
	plan := &VerifiedAdmissionPlan{binding: &verifiedAdmissionPlanBinding{canonical: [32]byte{2}}}
	permit := &RuntimePublishedPermit{
		prior: prior, plan: plan, candidateBinding: candidate.binding, target: [32]byte{3}, fullSet: [32]byte{4},
		revision: 2, digest: candidate.runtimeArtifact.digest, size: candidate.runtimeArtifact.sizeBytes,
	}
	permit.self = permit
	want := runtimePublishedPermitDigest(permit)
	if want == ([32]byte{}) {
		t.Fatal("runtime-published permit digest is empty")
	}
	copyPermit := *permit
	if runtimePublishedPermitDigest(&copyPermit) != ([32]byte{}) {
		t.Fatal("runtime-published permit copy retained self binding")
	}
	for name, mutate := range map[string]func(*RuntimePublishedPermit){
		"target":   func(v *RuntimePublishedPermit) { v.target[0]++ },
		"full set": func(v *RuntimePublishedPermit) { v.fullSet[0]++ },
		"revision": func(v *RuntimePublishedPermit) { v.revision++ },
		"digest":   func(v *RuntimePublishedPermit) { v.digest = testDigest("other") },
		"size":     func(v *RuntimePublishedPermit) { v.size++ },
		"reused":   func(v *RuntimePublishedPermit) { v.reused = !v.reused },
	} {
		t.Run(name, func(t *testing.T) {
			value := *permit
			value.self = &value
			mutate(&value)
			if runtimePublishedPermitDigest(&value) == want {
				t.Fatal("mutation did not change runtime-published permit digest")
			}
		})
	}
	result := RuntimePublicationTransitionResult{outcome: evidencefs.AdmissionTransitionUnknown, candidateDigest: [32]byte{5}, candidateSequence: 1, candidateRevision: 3, previousRevision: 2}
	if result.Next() != nil || result.CandidateSequence() != 1 || result.CandidateDigest() != ([32]byte{5}) || result.CandidateRevision() != 3 || result.PreviousRevision() != 2 {
		t.Fatalf("runtime closed diagnosis changed: %+v", result)
	}
}

func TestAdmissionPermitDigestRejectsCopyAndReuse(t *testing.T) {
	candidate := quotaCandidateForBundle(t, quotaAdmissionBundleForTest(t), []byte("recovery"))
	history := &VerifiedAdmissionHistory{binding: &verifiedAdmissionHistoryBinding{canonical: [32]byte{1}}}
	plan := &VerifiedAdmissionPlan{history: history, candidateBinding: candidate.binding, binding: &verifiedAdmissionPlanBinding{canonical: [32]byte{2}}, consumed: &atomic.Bool{}}
	permit := &AdmissionPermit{history: history, plan: plan, candidateBinding: candidate.binding, consumed: &atomic.Bool{}}
	permit.self = permit
	binding := &admissionPermitBinding{permit: permit, history: history, plan: plan}
	permit.binding, binding.canonical = binding, admissionPermitDigest(permit)
	want := binding.canonical
	copyPermit := *permit
	if admissionPermitDigest(&copyPermit) != ([32]byte{}) || admissionPermitDigest(permit) != want {
		t.Fatal("permit self binding is not exact")
	}
	plan.binding.canonical[0]++
	if admissionPermitDigest(permit) == want {
		t.Fatal("plan binding mutation did not change permit digest")
	}
	if !plan.consumed.CompareAndSwap(false, true) || plan.consumed.CompareAndSwap(false, true) {
		t.Fatal("plan one-shot consumption is not exact")
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
