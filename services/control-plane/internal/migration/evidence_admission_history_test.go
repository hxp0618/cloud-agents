package migration

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/evidencefs"
)

func TestAdmissionHistoryAuthorityFailsClosedWithoutOpaqueInventory(t *testing.T) {
	candidate := quotaCandidateForBundle(t, quotaAdmissionBundleForTest(t), []byte("recovery"))
	for name, inventory := range map[string]*evidencefs.AdmissionInventory{"nil": nil, "literal": {}} {
		t.Run(name, func(t *testing.T) {
			if history, err := bindVerifiedAdmissionHistory(context.Background(), inventory, candidate); history != nil || err == nil {
				t.Fatalf("unsealed inventory minted history: history=%+v err=%v", history, err)
			}
		})
	}
	if history, err := bindVerifiedAdmissionHistory(context.Background(), &evidencefs.AdmissionInventory{}, OwnedCurrentCandidate{}); history != nil || !IsCode(err, CodeEvidenceRecoveryRequired) {
		t.Fatalf("literal candidate minted history: history=%+v err=%v", history, err)
	}
	if validVerifiedAdmissionHistory(&VerifiedAdmissionHistory{}, candidate) {
		t.Fatal("literal history passed registry validation")
	}
	history := &VerifiedAdmissionHistory{owner: candidate.verifiedRun.currentDecision.owner, candidateBinding: candidate.binding, inventory: &evidencefs.AdmissionInventory{}, rootFacts: rootFactsForTest(t, nil)}
	binding := &verifiedAdmissionHistoryBinding{owner: history.owner, candidateBinding: candidate.binding, inventory: history.inventory, history: history}
	history.binding, binding.canonical = binding, admissionHistoryDigest(history)
	verifiedAdmissionHistoryRegistry.Store(binding, binding.canonical)
	copyHistory := *history
	if validVerifiedAdmissionHistory(&copyHistory, candidate) {
		t.Fatal("copied history reused the original registry binding")
	}
}

func TestMaterializedHistoricalSupersessionFailsClosedWithoutRegisteredEvidence(t *testing.T) {
	_, _, lineage, source, _, _ := registeredGenerationReplayFixture(t, 5)
	planned := plannedSuccessorReplayFixture(source)
	superseded := testDigest("history-intermediate-superseded")
	source.supersessionRecordDigest = &superseded
	source.plannedSuccessor = &planned
	actual := cloneAdmissionGeneration(planned)
	actual.reservedRecordDigest = testDigest("history-intermediate-reserved")
	actual.activationRecordDigest = digestPointer(testDigest("history-intermediate-activated"))
	lineage.generations = []admissionReplayGeneration{cloneAdmissionGeneration(source), cloneAdmissionGeneration(actual)}

	oldDecision := testDigest("history-intermediate-old")
	intermediateDecision := testDigest("history-intermediate-successor")
	currentDecision := testDigest("history-current-successor")
	sourceEvidence := &admissionVerifiedGenerationEvidence{decision: OwnedVerifiedDecision{digest: oldDecision}}
	plannedEvidence := &admissionVerifiedGenerationEvidence{decision: OwnedVerifiedDecision{digest: intermediateDecision}}
	current := OwnedVerifiedDecision{digest: currentDecision}
	err := verifyMaterializedHistoricalSupersession(context.Background(), lineage, &lineage.generations[0], &lineage.generations[1], sourceEvidence, plannedEvidence, current)
	if !IsCode(err, CodeEvidenceRecoveryRequired) {
		t.Fatalf("unregistered historical successor was misclassified: %v", err)
	}
}

func TestAdmissionHistoryDigestBindsEveryOrdinaryInput(t *testing.T) {
	candidate := quotaCandidateForBundle(t, quotaAdmissionBundleForTest(t), []byte("recovery"))
	facts := admissionHistoricalFactsFixture(t)
	bindRecoveryFactsToCandidate(facts, candidate)
	registered := registeredGenerationDigestFixture(t, candidate)
	history := &VerifiedAdmissionHistory{
		owner: candidate.verifiedRun.currentDecision.owner, candidateBinding: candidate.binding,
		revision: 0, target: [32]byte{1}, fullSet: [32]byte{2}, transcriptCanonical: [32]byte{3},
		currentFacts: facts,
		rootFacts:    rootFactsForTest(t, nil), reservation: evidenceQuotaReservation{ReservedRecords: 1, ReservedJournalBytes: 2, ReservedSegments: 1, ReservedIndexRecords: 3, ReservedIndexBytes: 4},
		quotaAdmission:   rootQuotaAdmission{finalObjectCount: 1, finalObjectBytes: 2, journalCount: 3, journalReservedBytes: 4, indexCount: 5, indexReservedBytes: 6, targetIndexRecords: 7, targetIndexReservedBytes: 8},
		targetGeneration: registered,
	}
	want := admissionHistoryDigest(history)
	mutations := []func(*VerifiedAdmissionHistory){
		func(v *VerifiedAdmissionHistory) { v.revision++ }, func(v *VerifiedAdmissionHistory) { v.target[0]++ }, func(v *VerifiedAdmissionHistory) { v.fullSet[0]++ },
		func(v *VerifiedAdmissionHistory) { v.transcriptCanonical[0]++ }, func(v *VerifiedAdmissionHistory) { v.rootFacts.indexReservedBytes++ },
		func(v *VerifiedAdmissionHistory) { v.targetState = admissionLineageEmpty }, func(v *VerifiedAdmissionHistory) { v.targetHeader.deploymentID = "drift" },
		func(v *VerifiedAdmissionHistory) { v.targetIndexRecords++ }, func(v *VerifiedAdmissionHistory) { v.targetIndexTail = testDigest("history-tail") },
		func(v *VerifiedAdmissionHistory) {
			v.currentFacts = cloneAdmissionHistoricalVerificationFacts(v.currentFacts)
			v.currentFacts.statementSubjects[v.currentFacts.orderedMigrations[0]][0][0]++
		},
		func(v *VerifiedAdmissionHistory) { v.reservation.ReservedCheckpointRecords++ }, func(v *VerifiedAdmissionHistory) { v.reservation.ReservedIndexBytes++ }, func(v *VerifiedAdmissionHistory) { v.reservation.ReservedBytes++ }, func(v *VerifiedAdmissionHistory) { v.quotaAdmission.targetIndexReservedBytes++ },
		func(v *VerifiedAdmissionHistory) {
			value := *v.targetGeneration
			value.canonical[0]++
			v.targetGeneration = &value
		},
		func(v *VerifiedAdmissionHistory) { v.targetGeneration = nil },
	}
	for index, mutate := range mutations {
		value := *history
		mutate(&value)
		if admissionHistoryDigest(&value) == want {
			t.Fatalf("history digest omitted mutation %d", index)
		}
	}
}

func TestRegisteredGenerationDigestBindsEveryOwnedFact(t *testing.T) {
	candidate := quotaCandidateForBundle(t, quotaAdmissionBundleForTest(t), []byte("recovery"))
	registered := registeredGenerationDigestFixture(t, candidate)
	want := verifiedAdmissionRegisteredGenerationDigest(registered)
	if want == ([32]byte{}) {
		t.Fatal("registered generation digest is empty")
	}
	mutations := map[string]func(*verifiedAdmissionRegisteredGeneration){
		"header": func(v *verifiedAdmissionRegisteredGeneration) { v.descriptor.header.ReservedRecords++ },
		"replay tail": func(v *verifiedAdmissionRegisteredGeneration) {
			v.descriptor.replayTailDigest = testDigest("other-tail")
		},
		"descriptor recovery": func(v *verifiedAdmissionRegisteredGeneration) {
			v.descriptor.recoveryArtifactDigest = testDigest("other-descriptor-recovery")
		},
		"descriptor recovery size": func(v *verifiedAdmissionRegisteredGeneration) { v.descriptor.recoveryArtifactSize++ },
		"decision":                 func(v *verifiedAdmissionRegisteredGeneration) { v.decision.digest = testDigest("other-decision") },
		"bindings":                 func(v *verifiedAdmissionRegisteredGeneration) { v.bindings.expectedCanonical += "-drift" },
		"bundle":                   func(v *verifiedAdmissionRegisteredGeneration) { v.bundle.ownedInputs.canonical[0]++ },
		"artifact digest": func(v *verifiedAdmissionRegisteredGeneration) {
			v.recoveryArtifact.digest = testDigest("other-artifact")
		},
		"artifact size":   func(v *verifiedAdmissionRegisteredGeneration) { v.recoveryArtifact.sizeBytes++ },
		"runtime digest":  func(v *verifiedAdmissionRegisteredGeneration) { v.runtimeReceipt.digest = testDigest("other-runtime") },
		"runtime size":    func(v *verifiedAdmissionRegisteredGeneration) { v.runtimeReceipt.sizeBytes++ },
		"recovery digest": func(v *verifiedAdmissionRegisteredGeneration) { v.recoveryReceipt.digest = testDigest("other-receipt") },
		"recovery size":   func(v *verifiedAdmissionRegisteredGeneration) { v.recoveryReceipt.sizeBytes++ },
		"policy": func(v *verifiedAdmissionRegisteredGeneration) {
			v.policy = &VerifiedHistoricalRecoveryPolicy{digest: testDigest("policy")}
		},
		"replay": func(v *verifiedAdmissionRegisteredGeneration) { v.replay.canonical[0]++ },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			value := cloneRegisteredGenerationDigestFixture(registered)
			mutate(value)
			if verifiedAdmissionRegisteredGenerationDigest(value) == want {
				t.Fatal("registered generation digest omitted mutation")
			}
		})
	}
}

func registeredGenerationDigestFixture(t *testing.T, candidate OwnedCurrentCandidate) *verifiedAdmissionRegisteredGeneration {
	t.Helper()
	bundle := quotaAdmissionBundleForTest(t)
	bindings, err := candidate.verifiedRun.currentDecision.decision.runnerProjectionBindings()
	if err != nil {
		t.Fatal(err)
	}
	history := &VerifiedAdmissionHistory{
		candidateBinding: candidate.binding, target: digestRaw(bindings.executionLineageDigest), rootFacts: rootFactsForTest(t, nil),
		reservation: evidenceQuotaReservation{
			ReservedRecords: 2, ReservedJournalBytes: 3, ReservedSegments: 1, ReservedIndexRecords: 4,
			ReservedIndexBytes: lineageRecordFrameLimits[LineageRecordHeader] + lineageRecordFrameLimits[LineageRecordGenerationReserved],
			ReservedBytes:      3 + lineageRecordFrameLimits[LineageRecordHeader] + lineageRecordFrameLimits[LineageRecordGenerationReserved],
		},
	}
	_, reservedFrame, _, _, err := buildBrandNewAdmissionFrames(history, candidate)
	if err != nil || reservedFrame.Record.Reserved == nil {
		t.Fatalf("registered generation fixture: %v", err)
	}
	reserved := reservedFrame.Record.Reserved
	header := cloneProjectionValue(reserved.PlannedSegment0Header)
	descriptor := GenerationDescriptor{
		identity: generationIdentity{candidate.owner, header.ExecutionLineageDigest, header.JournalIdentityDigest, header.RunnerProjectionDecisionDigest, header.SchemaBundleDigest},
		header:   header, replayTailDigest: reserved.ExpectedSegment0HeaderDigest,
		recoveryArtifactDigest: header.DecisionRecoveryArtifactSHA256, recoveryArtifactSize: header.DecisionRecoveryArtifactSizeBytes,
	}
	runtimeBinding := &verifiedContentReceiptBinding{owner: candidate.owner, kind: durableRuntimeContentObject, digest: header.OuterArtifactDigest, sizeBytes: header.OuterArtifactSizeBytes}
	recoveryBinding := &verifiedDecisionRecoveryReceiptBinding{owner: candidate.owner, kind: durableDecisionRecoveryContentObject, digest: header.DecisionRecoveryArtifactSHA256, sizeBytes: header.DecisionRecoveryArtifactSizeBytes}
	registered := &verifiedAdmissionRegisteredGeneration{
		descriptor: descriptor, decision: candidate.verifiedRun.currentDecision, bindings: bindings.ownedCopy(), bundle: bundle,
		recoveryArtifact: ownedDecisionRecoveryArtifactCopy(candidate.decisionRecoveryArtifact),
		runtimeReceipt:   VerifiedContentReceipt{owner: candidate.owner, kind: durableRuntimeContentObject, digest: runtimeBinding.digest, sizeBytes: runtimeBinding.sizeBytes, binding: runtimeBinding},
		recoveryReceipt:  VerifiedDecisionRecoveryReceipt{owner: candidate.owner, kind: durableDecisionRecoveryContentObject, digest: recoveryBinding.digest, sizeBytes: recoveryBinding.sizeBytes, binding: recoveryBinding},
		replay:           &verifiedAdmissionGenerationReplay{canonical: [32]byte{9}},
		handoffConsumed:  &atomic.Bool{},
	}
	registered.canonical = verifiedAdmissionRegisteredGenerationDigest(registered)
	if registered.canonical == ([32]byte{}) {
		t.Fatal("registered generation fixture did not seal")
	}
	return registered
}

func cloneRegisteredGenerationDigestFixture(value *verifiedAdmissionRegisteredGeneration) *verifiedAdmissionRegisteredGeneration {
	result := *value
	result.descriptor.header = cloneProjectionValue(value.descriptor.header)
	result.bindings = value.bindings.ownedCopy()
	result.recoveryArtifact = ownedDecisionRecoveryArtifactCopy(value.recoveryArtifact)
	bundle := *value.bundle
	result.bundle = &bundle
	if value.replay != nil {
		replay := *value.replay
		result.replay = &replay
	}
	return &result
}

func TestAdmissionHistoryRecoveryFactsAreDeepOwnedAndClosed(t *testing.T) {
	candidate := quotaCandidateForBundle(t, quotaAdmissionBundleForTest(t), []byte("recovery"))
	facts := admissionHistoricalFactsFixture(t)
	bindRecoveryFactsToCandidate(facts, candidate)
	owned := cloneAdmissionHistoricalVerificationFacts(facts)
	if !validAdmissionRecoveryFacts(owned) || admissionRecoveryFactsDigest(owned) == ([32]byte{}) {
		t.Fatal("valid verifier facts did not produce a closed recovery digest")
	}
	want := admissionRecoveryFactsDigest(owned)
	facts.orderedMigrations[0] = "999999"
	for key := range facts.statementSubjects {
		facts.statementSubjects[key][0][0]++
	}
	facts.ledgerRows[0].MigrationName = "drift"
	if admissionRecoveryFactsDigest(owned) != want {
		t.Fatal("source alias mutation changed owned recovery facts")
	}
	for name, mutate := range map[string]func(*admissionHistoricalVerificationFacts){
		"missing subjects": func(v *admissionHistoricalVerificationFacts) { delete(v.statementSubjects, v.orderedMigrations[0]) },
		"extra catalog":    func(v *admissionHistoricalVerificationFacts) { v.catalogContractDigest["999999"] = [32]byte{1} },
		"row mismatch":     func(v *admissionHistoricalVerificationFacts) { v.ledgerRows[0].MigrationID = "999999" },
		"zero subject": func(v *admissionHistoricalVerificationFacts) {
			v.statementSubjects[v.orderedMigrations[0]][0] = [32]byte{}
		},
	} {
		t.Run(name, func(t *testing.T) {
			value := cloneAdmissionHistoricalVerificationFacts(owned)
			mutate(value)
			if validAdmissionRecoveryFacts(value) || admissionRecoveryFactsDigest(value) != ([32]byte{}) {
				t.Fatal("incomplete recovery facts remained valid")
			}
		})
	}
}
