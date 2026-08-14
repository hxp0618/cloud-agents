package migration

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"testing"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/evidencefs"
)

func TestRegisteredReceiptBindersRejectLiteralPublicationAuthority(t *testing.T) {
	candidate := quotaCandidateForBundle(t, quotaAdmissionBundleForTest(t), []byte("registered-receipts"))
	bindings, err := candidate.verifiedRun.currentDecision.decision.runnerProjectionBindings()
	if err != nil {
		t.Fatal(err)
	}
	header := JournalHeader{
		FormatVersion: EvidenceJournalFormat, JournalIdentityDigest: testDigest("registered-journal"),
		RunnerProjectionDecisionDigest: candidate.verifiedRun.runnerProjectionDecisionDigest,
		ExecutionLineageDigest:         candidate.verifiedRun.executionLineageDigest,
		OuterArtifactDigest:            candidate.verifiedRun.outerArtifactDigest, OuterArtifactSizeBytes: candidate.verifiedRun.outerArtifactSizeBytes,
		DecisionRecoveryArtifactSHA256:    candidate.verifiedRun.decisionRecoveryArtifactSHA256,
		DecisionRecoveryArtifactSizeBytes: candidate.verifiedRun.decisionRecoveryArtifactSizeBytes,
		SchemaBundleDigest:                candidate.verifiedRun.schemaBundleDigest,
	}
	generation := GenerationDescriptor{
		identity: generationIdentity{candidate.owner, header.ExecutionLineageDigest, header.JournalIdentityDigest, header.RunnerProjectionDecisionDigest, header.SchemaBundleDigest},
		header:   header, recoveryArtifactDigest: header.DecisionRecoveryArtifactSHA256, recoveryArtifactSize: header.DecisionRecoveryArtifactSizeBytes,
	}
	publication := &evidencefs.RegisteredPublication{}
	if receipt, err := bindRegisteredRuntimeReceipt(candidate.verifiedRun.currentDecision, bindings, generation, publication); receipt != (VerifiedContentReceipt{}) || !IsCode(err, CodeEvidenceRecoveryRequired) {
		t.Fatalf("runtime receipt=%+v err=%v", receipt, err)
	}
	if receipt, err := bindRegisteredDecisionRecoveryReceipt(candidate.verifiedRun.currentDecision, bindings, generation, candidate.decisionRecoveryArtifact, publication); receipt != (VerifiedDecisionRecoveryReceipt{}) || !IsCode(err, CodeEvidenceRecoveryRequired) {
		t.Fatalf("recovery receipt=%+v err=%v", receipt, err)
	}
	if validRegisteredRuntimeReceipt(VerifiedContentReceipt{}, candidate.owner, header.OuterArtifactDigest, header.OuterArtifactSizeBytes) || validRegisteredDecisionRecoveryReceipt(VerifiedDecisionRecoveryReceipt{}, candidate.owner, header.DecisionRecoveryArtifactSHA256, header.DecisionRecoveryArtifactSizeBytes) || registeredReceiptsSameStore(VerifiedContentReceipt{}, VerifiedDecisionRecoveryReceipt{}) {
		t.Fatal("literal registered receipt retained authority")
	}
}

func TestFreshAndRegisteredReceiptKindsCannotSwap(t *testing.T) {
	owner := &evidenceOwnerToken{nonce: [16]byte{43}}
	digest := testDigest("registered-kind")
	registered := &evidencefs.RegisteredPublication{}
	runtimeBinding := &verifiedContentReceiptBinding{owner: owner, kind: durableRuntimeContentObject, digest: digest, sizeBytes: 1, registeredPublication: registered}
	runtime := VerifiedContentReceipt{owner: owner, kind: durableRuntimeContentObject, digest: digest, sizeBytes: 1, registeredPublication: registered, binding: runtimeBinding}
	if validRuntimeReceipt(runtime, owner, digest, 1) {
		t.Fatal("registered runtime receipt entered fresh validator")
	}
	recoveryBinding := &verifiedDecisionRecoveryReceiptBinding{owner: owner, kind: durableDecisionRecoveryContentObject, digest: digest, sizeBytes: 1, registeredPublication: registered}
	recovery := VerifiedDecisionRecoveryReceipt{owner: owner, kind: durableDecisionRecoveryContentObject, digest: digest, sizeBytes: 1, registeredPublication: registered, binding: recoveryBinding}
	if validDecisionRecoveryReceipt(recovery, owner, digest, 1) {
		t.Fatal("registered recovery receipt entered fresh validator")
	}
	freshPublication := &evidencefs.Publication{}
	freshRuntimeBinding := &verifiedContentReceiptBinding{owner: owner, kind: durableRuntimeContentObject, digest: digest, sizeBytes: 1, publication: freshPublication}
	freshRuntime := VerifiedContentReceipt{owner: owner, kind: durableRuntimeContentObject, digest: digest, sizeBytes: 1, publication: freshPublication, binding: freshRuntimeBinding}
	if validRegisteredRuntimeReceipt(freshRuntime, owner, digest, 1) {
		t.Fatal("fresh runtime receipt entered registered validator")
	}
	freshRecoveryBinding := &verifiedDecisionRecoveryReceiptBinding{owner: owner, kind: durableDecisionRecoveryContentObject, digest: digest, sizeBytes: 1, publication: freshPublication}
	freshRecovery := VerifiedDecisionRecoveryReceipt{owner: owner, kind: durableDecisionRecoveryContentObject, digest: digest, sizeBytes: 1, publication: freshPublication, binding: freshRecoveryBinding}
	if validRegisteredDecisionRecoveryReceipt(freshRecovery, owner, digest, 1) {
		t.Fatal("fresh recovery receipt entered registered validator")
	}
}

func TestRegisteredGenerationRevocationDeletesBothReceiptAuthorities(t *testing.T) {
	runtimeBinding := &verifiedContentReceiptBinding{}
	recoveryBinding := &verifiedDecisionRecoveryReceiptBinding{}
	verifiedContentReceiptRegistry.Store(runtimeBinding, runtimeBinding)
	verifiedDecisionRecoveryReceiptRegistry.Store(recoveryBinding, recoveryBinding)
	value := &verifiedAdmissionRegisteredGeneration{
		runtimeReceipt:  VerifiedContentReceipt{binding: runtimeBinding},
		recoveryReceipt: VerifiedDecisionRecoveryReceipt{binding: recoveryBinding},
	}
	revokeVerifiedAdmissionRegisteredGeneration(value)
	if _, ok := verifiedContentReceiptRegistry.Load(runtimeBinding); ok {
		t.Fatal("runtime receipt registry entry survived generation revocation")
	}
	if _, ok := verifiedDecisionRecoveryReceiptRegistry.Load(recoveryBinding); ok {
		t.Fatal("recovery receipt registry entry survived generation revocation")
	}
	revokeVerifiedAdmissionRegisteredGeneration(nil)
}

func TestRegisteredReceiptConsumersAreRecoveryOnly(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	allowed := map[string]bool{"evidence_registered_receipt.go": true, "evidence_admission_history.go": true, "evidence_historical_supersession.go": true, "evidence_historical_supersession_activation.go": true, "evidence_historical_supersession_recovery.go": true, "evidence_registered_generation_handoff.go": true, "evidence_generation_journal.go": true, "evidence_session.go": true, "evidence_trust_recovery.go": true}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || allowed[name] || len(name) < 3 || name[len(name)-3:] != ".go" || len(name) >= 8 && name[len(name)-8:] == "_test.go" {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			identifier, ok := node.(*ast.Ident)
			if ok && (identifier.Name == "bindRegisteredRuntimeReceipt" || identifier.Name == "bindRegisteredDecisionRecoveryReceipt" || identifier.Name == "validRegisteredRuntimeReceipt" || identifier.Name == "validRegisteredDecisionRecoveryReceipt") {
				t.Fatalf("registered receipt authority spread into %s", name)
			}
			return true
		})
	}
}
