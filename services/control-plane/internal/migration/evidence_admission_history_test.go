package migration

import (
	"context"
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

func TestAdmissionHistoryDigestBindsEveryOrdinaryInput(t *testing.T) {
	candidate := quotaCandidateForBundle(t, quotaAdmissionBundleForTest(t), []byte("recovery"))
	facts := admissionHistoricalFactsFixture(t)
	bindRecoveryFactsToCandidate(facts, candidate)
	history := &VerifiedAdmissionHistory{
		owner: candidate.verifiedRun.currentDecision.owner, candidateBinding: candidate.binding,
		revision: 0, target: [32]byte{1}, fullSet: [32]byte{2}, transcriptCanonical: [32]byte{3},
		currentFacts: facts,
		rootFacts:    rootFactsForTest(t, nil), reservation: evidenceQuotaReservation{ReservedRecords: 1, ReservedJournalBytes: 2, ReservedSegments: 1, ReservedIndexRecords: 3, ReservedIndexBytes: 4},
		quotaAdmission: rootQuotaAdmission{finalObjectCount: 1, finalObjectBytes: 2, journalCount: 3, journalReservedBytes: 4, indexCount: 5, indexReservedBytes: 6, targetIndexRecords: 7, targetIndexReservedBytes: 8},
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
	}
	for index, mutate := range mutations {
		value := *history
		mutate(&value)
		if admissionHistoryDigest(&value) == want {
			t.Fatalf("history digest omitted mutation %d", index)
		}
	}
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
