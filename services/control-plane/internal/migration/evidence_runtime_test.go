package migration

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/evidencefs"
)

var reviewedEvidenceSinkAuthorityUses = map[string]int{
	"AppendGenerationActivated":                      3,
	"GenerationHandoffReady":                         2,
	"GenerationReadyPermit":                          1,
	"GenerationRecoveryReady":                        2,
	"GenerationReplayReady":                          2,
	"HistoricalSuccessorGenerationHandoffReady":      2,
	"HistoricalSuccessorGenerationReadyPermit":       1,
	"HistoricalSuccessorGenerationRecoveryReady":     2,
	"HistoricalSuccessorGenerationReplayReady":       2,
	"HistoricalSuccessorHeaderDurablePermit":         1,
	"HistoricalSuccessorReservedDurablePermit":       1,
	"HistoricalSupersessionAdjacentReserveReady":     1,
	"RegisteredGenerationHandoffPermit":              3,
	"RegisteredGenerationRecoveryReady":              1,
	"bindHistoricalSupersessionAdjacentReserveReady": 1,
	"bindRegisteredGenerationHandoff":                1,
	"historicalSuccessorGenerationHandoffRegistry":   1,
	"historicalSuccessorGenerationRecoveryRegistry":  1,
	"historicalSuccessorGenerationReplayRegistry":    1,
}

func reviewedEvidenceSinkAuthorityUse(fileName, identifier string) bool {
	_, allowed := reviewedEvidenceSinkAuthorityUses[identifier]
	return fileName == "evidence_sink.go" && allowed
}

func TestEvidenceSinkAuthorityCompositionRootUsesAreExact(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "evidence_sink.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]int{}
	ast.Inspect(file, func(node ast.Node) bool {
		identifier, ok := node.(*ast.Ident)
		if ok {
			if _, tracked := reviewedEvidenceSinkAuthorityUses[identifier.Name]; tracked {
				got[identifier.Name]++
			}
		}
		return true
	})
	if len(got) != len(reviewedEvidenceSinkAuthorityUses) {
		t.Fatalf("evidence sink authority uses=%v want=%v", got, reviewedEvidenceSinkAuthorityUses)
	}
	for identifier, want := range reviewedEvidenceSinkAuthorityUses {
		if got[identifier] != want {
			t.Fatalf("evidence sink authority %s uses=%d want=%d", identifier, got[identifier], want)
		}
	}
}

func TestDurableContentReceiptBindersRejectLiteralPublicationAuthority(t *testing.T) {
	owner := &evidenceOwnerToken{nonce: [16]byte{41}}
	runtimeBytes := []byte("runtime-durable-object")
	runtimeArtifact := VerifiedRuntimeArtifact{owner: owner, bytes: runtimeBytes, digest: DigestBytes(runtimeBytes), sizeBytes: uint64(len(runtimeBytes))}
	for name, object := range map[string]verifiedDurableContentObject{
		"zero":    {},
		"literal": {publication: &evidencefs.Publication{}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := bindRuntimeContentReceipt(owner, runtimeArtifact, object); !IsCode(err, CodeEvidenceRecoveryRequired) {
				t.Fatalf("runtime binder did not reject unavailable publication authority: %v", err)
			}
		})
	}
	for name, receipt := range map[string]VerifiedContentReceipt{
		"zero": {},
		"self-consistent-literal": {
			owner: owner, kind: durableRuntimeContentObject, digest: runtimeArtifact.digest,
			sizeBytes: runtimeArtifact.sizeBytes, publication: &evidencefs.Publication{}, binding: &verifiedContentReceiptBinding{},
		},
	} {
		t.Run("runtime-validator-"+name, func(t *testing.T) {
			if validRuntimeReceipt(receipt, owner, runtimeArtifact.digest, runtimeArtifact.sizeBytes) {
				t.Fatal("runtime receipt literal bypassed unavailable publication authority")
			}
		})
	}

	verifierOwner := &recoveryVerifierOwner{verifier: &recoveryVerifierFake{}, token: owner}
	recoveryBytes := []byte("recovery-durable-object")
	recoveryArtifact := VerifiedDecisionRecoveryArtifact{owner: verifierOwner, bytes: recoveryBytes, digest: DigestBytes(recoveryBytes), sizeBytes: uint64(len(recoveryBytes)), decision: DigestBytes([]byte("decision"))}
	for name, object := range map[string]verifiedDurableContentObject{
		"zero":    {},
		"literal": {publication: &evidencefs.Publication{}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := bindDecisionRecoveryReceipt(owner, recoveryArtifact, object); !IsCode(err, CodeEvidenceRecoveryRequired) {
				t.Fatalf("recovery binder did not reject unavailable publication authority: %v", err)
			}
		})
	}
	if validDecisionRecoveryReceipt(VerifiedDecisionRecoveryReceipt{
		owner: owner, kind: durableDecisionRecoveryContentObject, digest: recoveryArtifact.digest,
		sizeBytes: recoveryArtifact.sizeBytes, publication: &evidencefs.Publication{}, binding: &verifiedDecisionRecoveryReceiptBinding{},
	}, owner, recoveryArtifact.digest, recoveryArtifact.sizeBytes) {
		t.Fatal("recovery receipt literal bypassed unavailable publication authority")
	}
}

func TestVerifiedEvidenceRunTotalBinderOwnsInputsAndRejectsEverySwap(t *testing.T) {
	bundle := quotaAdmissionBundleForTest(t)
	baseline := quotaCandidateForBundle(t, bundle, []byte("ignored-shape"))
	decision := baseline.verifiedRun.currentDecision.decision
	bindings, err := decision.runnerProjectionBindings()
	if err != nil {
		t.Fatal(err)
	}
	current := baseline.verifiedRun.currentDecision
	outer := append([]byte(nil), baseline.runtimeArtifact.bytes...)
	recovery := baseline.decisionRecoveryArtifact
	recovery.bytes = append([]byte(nil), recovery.bytes...)

	run, runtime, candidate, err := bindVerifiedEvidenceRun(decision, bindings, current, outer, recovery)
	if err != nil || !validOwnedCurrentCandidate(candidate) || run.binding == nil || run.binding != runtime.binding || run.binding != candidate.binding {
		t.Fatalf("total binder baseline: run=%+v runtime=%+v candidate=%+v err=%v", run, runtime, candidate, err)
	}
	// The result owns all mutable inputs. Mutating caller aliases cannot change
	// the authority that was already minted.
	outer[0] ^= 0x01
	recovery.bytes[0] ^= 0x01
	bindings.executableCatalogs[0].catalogContract.SourceDescriptors[0].Statements[0].Start++
	if !validOwnedCurrentCandidate(candidate) {
		t.Fatal("caller alias mutation changed an owned evidence candidate")
	}
	run.decisionRecoveryArtifact.bytes[0] ^= 0x01
	runtime.bytes[0] ^= 0x01
	if !validOwnedCurrentCandidate(candidate) {
		t.Fatal("separately returned run/runtime aliases changed the owned candidate")
	}

	if _, _, _, err := bindVerifiedEvidenceRun(VerifiedTrustDecision{}, RunnerProjectionBindings{}, OwnedVerifiedDecision{}, nil, VerifiedDecisionRecoveryArtifact{}); !IsCode(err, CodeEvidenceRecoveryRequired) {
		t.Fatalf("zero literal entered total binder: %v", err)
	}
	wrongBindings := bindings.ownedCopy()
	wrongBindings.runnerProjectionDecisionDigest = testDigest("swapped-projection")
	if _, _, _, err := bindVerifiedEvidenceRun(decision, wrongBindings, current, baseline.runtimeArtifact.bytes, baseline.decisionRecoveryArtifact); !IsCode(err, CodeEvidenceRecoveryRequired) {
		t.Fatalf("projection swap entered total binder: %v", err)
	}
	wrongCurrent := current
	wrongCurrent.digest = testDigest("swapped-current")
	if _, _, _, err := bindVerifiedEvidenceRun(decision, baseline.verifiedRun.currentDecision.decision.projectionBindings.ownedCopy(), wrongCurrent, baseline.runtimeArtifact.bytes, baseline.decisionRecoveryArtifact); !IsCode(err, CodeEvidenceRecoveryRequired) {
		t.Fatalf("current decision swap entered total binder: %v", err)
	}
	wrongRecovery := baseline.decisionRecoveryArtifact
	wrongRecovery.owner = &recoveryVerifierOwner{verifier: &recoveryVerifierFake{}, token: baseline.owner}
	if _, _, _, err := bindVerifiedEvidenceRun(decision, *decision.projectionBindings, current, baseline.runtimeArtifact.bytes, wrongRecovery); !IsCode(err, CodeEvidenceRecoveryRequired) {
		t.Fatalf("verifier owner swap entered total binder: %v", err)
	}
	wrongRecovery = baseline.decisionRecoveryArtifact
	wrongRecovery.digest = testDigest("swapped-recovery")
	if _, _, _, err := bindVerifiedEvidenceRun(decision, *decision.projectionBindings, current, baseline.runtimeArtifact.bytes, wrongRecovery); !IsCode(err, CodeEvidenceRecoveryRequired) {
		t.Fatalf("recovery digest swap entered total binder: %v", err)
	}
	noncanonicalRecovery := baseline.decisionRecoveryArtifact
	noncanonicalRecovery.bytes = append([]byte(" "), noncanonicalRecovery.bytes...)
	noncanonicalRecovery.sizeBytes = uint64(len(noncanonicalRecovery.bytes))
	noncanonicalRecovery.digest = DigestBytes(noncanonicalRecovery.bytes)
	if _, _, _, err := bindVerifiedEvidenceRun(decision, *decision.projectionBindings, current, baseline.runtimeArtifact.bytes, noncanonicalRecovery); !IsCode(err, CodeEvidenceRecoveryRequired) {
		t.Fatalf("noncanonical recovery entered total binder: %v", err)
	}
	profileRecovery := baseline.decisionRecoveryArtifact
	profileInputs, err := decodeDecisionRecoveryVerificationInputs(profileRecovery.bytes)
	if err != nil {
		t.Fatal(err)
	}
	profileInputs.ProfileDigest = testDigest("other-recovery-profile")
	profileCanonical, err := canonicalContractKey(profileInputs)
	if err != nil {
		t.Fatal(err)
	}
	profileRecovery.bytes = []byte(profileCanonical)
	profileRecovery.sizeBytes = uint64(len(profileRecovery.bytes))
	profileRecovery.digest = DigestBytes(profileRecovery.bytes)
	if _, _, _, err := bindVerifiedEvidenceRun(decision, *decision.projectionBindings, current, baseline.runtimeArtifact.bytes, profileRecovery); !IsCode(err, CodeEvidenceRecoveryRequired) {
		t.Fatalf("recovery profile swap entered total binder: %v", err)
	}
	wrongOuter := append([]byte(nil), baseline.runtimeArtifact.bytes...)
	wrongOuter[0] ^= 0x01
	if _, _, _, err := bindVerifiedEvidenceRun(decision, *decision.projectionBindings, current, wrongOuter, baseline.decisionRecoveryArtifact); !IsCode(err, CodeEvidenceRecoveryRequired) {
		t.Fatalf("outer bytes swap entered total binder: %v", err)
	}

	mutations := map[string]func(*OwnedCurrentCandidate){
		"candidate owner":   func(value *OwnedCurrentCandidate) { value.owner = &evidenceOwnerToken{} },
		"candidate binding": func(value *OwnedCurrentCandidate) { value.binding = &verifiedEvidenceRunBinding{} },
		"run owner":         func(value *OwnedCurrentCandidate) { value.verifiedRun.owner = &evidenceOwnerToken{} },
		"run binding":       func(value *OwnedCurrentCandidate) { value.verifiedRun.binding = &verifiedEvidenceRunBinding{} },
		"current owner":     func(value *OwnedCurrentCandidate) { value.verifiedRun.currentDecision.owner = &recoveryVerifierOwner{} },
		"current digest":    func(value *OwnedCurrentCandidate) { value.verifiedRun.currentDecision.digest = testDigest("current") },
		"release": func(value *OwnedCurrentCandidate) {
			value.verifiedRun.releaseTrustDecisionDigest = testDigest("release")
		},
		"projection": func(value *OwnedCurrentCandidate) {
			value.verifiedRun.runnerProjectionDecisionDigest = testDigest("projection")
		},
		"lineage":      func(value *OwnedCurrentCandidate) { value.verifiedRun.executionLineageDigest = testDigest("lineage") },
		"outer digest": func(value *OwnedCurrentCandidate) { value.verifiedRun.outerArtifactDigest = testDigest("outer") },
		"outer size":   func(value *OwnedCurrentCandidate) { value.verifiedRun.outerArtifactSizeBytes++ },
		"recovery digest": func(value *OwnedCurrentCandidate) {
			value.verifiedRun.decisionRecoveryArtifactSHA256 = testDigest("recovery")
		},
		"recovery size": func(value *OwnedCurrentCandidate) { value.verifiedRun.decisionRecoveryArtifactSizeBytes++ },
		"manifest":      func(value *OwnedCurrentCandidate) { value.verifiedRun.manifestDigest = testDigest("mutated-manifest") },
		"runner release": func(value *OwnedCurrentCandidate) {
			value.verifiedRun.runnerReleaseDigest = testDigest("mutated-runner")
		},
		"schema":               func(value *OwnedCurrentCandidate) { value.verifiedRun.schemaBundleDigest = testDigest("schema") },
		"authority profile":    func(value *OwnedCurrentCandidate) { value.verifiedRun.authorityProfileDigest = testDigest("profile") },
		"authority binding":    func(value *OwnedCurrentCandidate) { value.verifiedRun.authorityBindingDigest = testDigest("authority") },
		"runtime bytes":        func(value *OwnedCurrentCandidate) { value.runtimeArtifact.bytes[0] ^= 0x01 },
		"runtime digest":       func(value *OwnedCurrentCandidate) { value.runtimeArtifact.digest = testDigest("runtime") },
		"runtime binding":      func(value *OwnedCurrentCandidate) { value.runtimeArtifact.binding = &verifiedEvidenceRunBinding{} },
		"recovery bytes":       func(value *OwnedCurrentCandidate) { value.decisionRecoveryArtifact.bytes[0] ^= 0x01 },
		"recovery decision":    func(value *OwnedCurrentCandidate) { value.decisionRecoveryArtifact.decision = testDigest("decision") },
		"bound recovery bytes": func(value *OwnedCurrentCandidate) { value.verifiedRun.decisionRecoveryArtifact.bytes[0] ^= 0x01 },
		"projection closure": func(value *OwnedCurrentCandidate) {
			value.verifiedRun.currentDecision.decision.projectionBindings.authorityBindingDigest = testDigest("nested")
		},
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			value := quotaCandidateForBundle(t, quotaAdmissionBundleForTest(t), []byte("candidate"))
			mutate(&value)
			if validOwnedCurrentCandidate(value) {
				t.Fatal("mutated candidate retained total authority")
			}
			if _, _, err := quotaCandidateArtifacts(quotaAdmissionBundleForTest(t).quotaFacts, value); !IsCode(err, CodeEvidenceRecoveryRequired) {
				t.Fatalf("mutated candidate entered quota: %v", err)
			}
		})
	}
}

func TestOwnedCurrentCandidateRevocationIsExactAndOneShot(t *testing.T) {
	bundle := quotaAdmissionBundleForTest(t)
	candidate := quotaCandidateForBundle(t, bundle, []byte("ignored-shape"))
	copyCandidate := candidate
	if !validOwnedCurrentCandidate(candidate) || !revokeOwnedCurrentCandidate(copyCandidate) || validOwnedCurrentCandidate(candidate) || revokeOwnedCurrentCandidate(candidate) {
		t.Fatal("owned current candidate revocation was not exact, shared, and one-shot")
	}
}

func TestEvidenceOpenCandidateReconstructionOwnsAndRejectsEveryBindingSwap(t *testing.T) {
	bundle := quotaAdmissionBundleForTest(t)
	baseline := quotaCandidateForBundle(t, bundle, []byte("ignored-shape"))
	run := baseline.verifiedRun
	run.decisionRecoveryArtifact.bytes = append([]byte(nil), baseline.verifiedRun.decisionRecoveryArtifact.bytes...)
	runtime := baseline.runtimeArtifact
	runtime.bytes = append([]byte(nil), baseline.runtimeArtifact.bytes...)
	reconstructed, err := ownedCurrentCandidateFromEvidenceRun(run, runtime)
	if err != nil || !validOwnedCurrentCandidate(reconstructed) || reconstructed.binding != baseline.binding {
		t.Fatalf("open-side candidate reconstruction failed: candidate=%+v err=%v", reconstructed, err)
	}
	run.decisionRecoveryArtifact.bytes[0] ^= 1
	runtime.bytes[0] ^= 1
	if !validOwnedCurrentCandidate(reconstructed) {
		t.Fatal("open-side reconstruction retained caller byte aliases")
	}
	for name, mutate := range map[string]func(*VerifiedEvidenceRun, *VerifiedRuntimeArtifact){
		"run binding": func(run *VerifiedEvidenceRun, _ *VerifiedRuntimeArtifact) {
			run.binding = &verifiedEvidenceRunBinding{}
		},
		"runtime binding": func(_ *VerifiedEvidenceRun, runtime *VerifiedRuntimeArtifact) {
			runtime.binding = &verifiedEvidenceRunBinding{}
		},
		"runtime bytes":  func(_ *VerifiedEvidenceRun, runtime *VerifiedRuntimeArtifact) { runtime.bytes[0] ^= 1 },
		"recovery bytes": func(run *VerifiedEvidenceRun, _ *VerifiedRuntimeArtifact) { run.decisionRecoveryArtifact.bytes[0] ^= 1 },
	} {
		t.Run(name, func(t *testing.T) {
			faultRun := baseline.verifiedRun
			faultRun.decisionRecoveryArtifact.bytes = append([]byte(nil), baseline.verifiedRun.decisionRecoveryArtifact.bytes...)
			faultRuntime := baseline.runtimeArtifact
			faultRuntime.bytes = append([]byte(nil), baseline.runtimeArtifact.bytes...)
			mutate(&faultRun, &faultRuntime)
			if candidate, err := ownedCurrentCandidateFromEvidenceRun(faultRun, faultRuntime); candidate.binding != nil || candidate.owner != nil || !IsCode(err, CodeEvidenceRecoveryRequired) {
				t.Fatalf("binding swap reconstructed a candidate: candidate=%+v err=%v", candidate, err)
			}
		})
	}
	if !revokeOwnedCurrentCandidate(baseline) {
		t.Fatal("open-side reconstruction fixture did not revoke")
	}
}

func TestEvidenceOpenCandidateReconstructionHasExactProductionCallEdges(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	calls := map[string]int{}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || filepath.Ext(name) != ".go" || len(name) >= 8 && name[len(name)-8:] == "_test.go" {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			identifier, ok := call.Fun.(*ast.Ident)
			if !ok || identifier.Name != "ownedCurrentCandidateFromEvidenceRun" {
				return true
			}
			calls[name]++
			if name != "evidence_runtime.go" && name != "evidence_sink.go" {
				t.Fatalf("evidence Open candidate reconstruction spread into %s", name)
			}
			return true
		})
	}
	if calls["evidence_runtime.go"] != 1 || calls["evidence_sink.go"] != 1 || len(calls) != 2 {
		t.Fatalf("evidence candidate reconstruction call edges=%v want one in each reviewed composition root", calls)
	}
}

func TestOwnedEvidenceRecordIsKindGenerationCursorBoundAndSingleUse(t *testing.T) {
	document := fixtureObject(t, migrationFixturePath(t, "golden/evidence-record-chain-v1.json"))
	frames := decodeEvidenceFrames(t, document["frames"])
	context := fixtureObjectValue(t, document["validation_context"], "validation context")
	witness := buildEvidenceWitness(t, frames, context)
	owner := &evidenceOwnerToken{nonce: [16]byte{8}}
	generation := recoveryFixtureGeneration(owner, *frames[0].Record.Header)
	cursor := runtimeCursorAt(generation, frames[0].RecordDigest, 1)
	planWitness := witness.plans[evidenceStatementKey("000001", 1, 0)]
	plan := StatementPlan{MigrationID: planWitness.migrationID, StatementIndex: planWitness.statementIndex, SQLArtifactSHA256: planWitness.sqlArtifactSHA256, SQLArtifactSizeBytes: planWitness.sqlArtifactSizeBytes, StartOffset: planWitness.startOffset, EndOffset: planWitness.endOffset, StatementSHA256: planWitness.statementSHA256, Classification: frames[1].Record.StatementIntent.Classification, ExpectedTransitionDigest: planWitness.expectedTransitionDigest, exact: true, exactCanonical: "owned"}
	plan.sqlBytes = make([]byte, plan.EndOffset-plan.StartOffset)
	// validateExact includes the signed transition and bytes; use the real plan
	// shape from the wire oracle for the runtime binder's mismatch path first.
	badWitness := ownedStatementIntentWitness{ownedAppendContext{generation, cursor, frames[:1], witness}, plan}
	if _, err := bindOwnedEvidenceRecord(frames[1].Record, badWitness); err == nil {
		t.Fatal("accepted incomplete statement plan")
	}

	// Non-plan branches demonstrate the sealed ownership mechanics independent
	// of statement classifier construction.
	terminalFrame := frames[len(frames)-1]
	terminalCursor := runtimeCursorAt(generation, frames[len(frames)-2].RecordDigest, terminalFrame.Sequence)
	terminalWitness := ownedAttemptTerminalWitness{ownedAppendContext{generation, terminalCursor, frames[:len(frames)-1], witness}, terminalFrame.Record.AttemptTerminal.TerminalDigest, nil, 3}
	owned, err := bindOwnedEvidenceRecord(terminalFrame.Record, terminalWitness)
	if err != nil {
		t.Fatal(err)
	}
	other := generation
	other.owner = &evidenceOwnerToken{nonce: [16]byte{9}}
	if _, err := owned.consume(other, terminalCursor); err == nil {
		t.Fatal("accepted generation swap")
	}
	wrongCursor := terminalCursor.clone()
	wrongCursor.nextSequence++
	if _, err := owned.consume(generation, wrongCursor); err == nil {
		t.Fatal("accepted cursor swap")
	}
	if _, err := owned.consume(generation, terminalCursor); err != nil {
		t.Fatal(err)
	}
	if _, err := owned.consume(generation, terminalCursor); err == nil {
		t.Fatal("reused consumed record")
	}

	if _, err := bindOwnedEvidenceRecord(EvidenceRecord{Header: frames[0].Record.Header}, terminalWitness); err == nil {
		t.Fatal("header entered caller witness union")
	}
	var disk EvidenceFrame
	raw := mustJSON(t, terminalFrame)
	if _, err := DecodeStrict(raw, &disk); err != nil {
		t.Fatal(err)
	}
	if _, err := bindOwnedEvidenceRecord(disk.Record, nil); err == nil {
		t.Fatal("disk DTO recovered append authority")
	}
}

func TestAppendUnknownInvalidatesCursorAndNeverMintsDurableAuthority(t *testing.T) {
	document := fixtureObject(t, migrationFixturePath(t, "golden/evidence-record-chain-v1.json"))
	frames := decodeEvidenceFrames(t, document["frames"])
	owner := &evidenceOwnerToken{nonce: [16]byte{10}}
	generation := recoveryFixtureGeneration(owner, *frames[0].Record.Header)
	terminal := frames[len(frames)-1]
	cursor := runtimeCursorAt(generation, frames[len(frames)-2].RecordDigest, terminal.Sequence)
	witness := buildEvidenceWitness(t, frames, fixtureObjectValue(t, document["validation_context"], "validation context"))
	owned, err := bindOwnedEvidenceRecord(terminal.Record, ownedAttemptTerminalWitness{ownedAppendContext{generation, cursor, frames[:len(frames)-1], witness}, terminal.Record.AttemptTerminal.TerminalDigest, nil, 3})
	if err != nil {
		t.Fatal(err)
	}
	result, err := finishAppend(cursor, owned, generation, appendOutcomeUnknown, nil, terminal.RecordDigest, DigestBytes([]byte("checkpoint")))
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome() != "unknown" || result.DurableCursor() != nil || cursor.Valid() {
		t.Fatal("unknown append preserved cursor authority")
	}
}

func TestRotationAppendResultBindsHeaderDiagnosisAndTwoStepCursor(t *testing.T) {
	document := fixtureObject(t, migrationFixturePath(t, "golden/evidence-record-chain-v1.json"))
	frames := decodeEvidenceFrames(t, document["frames"])
	owner := &evidenceOwnerToken{nonce: [16]byte{34}}
	generation := recoveryFixtureGeneration(owner, *frames[0].Record.Header)
	previous := frames[len(frames)-2].RecordDigest
	header, headerCheckpoint := testDigest("rotation-header"), testDigest("rotation-header-checkpoint")
	candidate, candidateCheckpoint := testDigest("rotation-candidate"), testDigest("rotation-candidate-checkpoint")

	unknownCursor := runtimeCursorAt(generation, previous, frames[len(frames)-1].Sequence)
	unknown, err := finishConsumedRotationAppend(unknownCursor, generation, appendOutcomeUnknown, nil, candidate, candidateCheckpoint, header, headerCheckpoint)
	if err != nil || unknown.Outcome() != "unknown" || unknown.DurableCursor() != nil || unknownCursor.Valid() || unknown.candidateSequence != frames[len(frames)-1].Sequence+1 || unknown.candidatePreviousRecordDigest == nil || *unknown.candidatePreviousRecordDigest != header || unknown.rotationHeaderRecordDigest == nil || *unknown.rotationHeaderRecordDigest != header || unknown.rotationHeaderCheckpointRecordDigest == nil || *unknown.rotationHeaderCheckpointRecordDigest != headerCheckpoint || unknown.candidateRecordDigest != candidate || unknown.candidateCheckpointRecordDigest != candidateCheckpoint {
		t.Fatalf("unknown rotation=%+v err=%v cursor=%v", unknown, err, unknownCursor.Valid())
	}

	durableCursor := runtimeCursorAt(generation, candidate, frames[len(frames)-1].Sequence+2)
	durableCursor.segmentIndex = 1
	durableCursor.lineageIndexNextSequence = 3
	input := runtimeCursorAt(generation, previous, frames[len(frames)-1].Sequence)
	durable, err := finishConsumedRotationAppend(input, generation, appendOutcomeDurable, &durableCursor, candidate, candidateCheckpoint, header, headerCheckpoint)
	if err != nil || durable.Outcome() != "durable" || durable.DurableCursor() == nil || durable.DurableCursor().segmentIndex != 1 || input.Valid() {
		t.Fatalf("durable rotation=%+v err=%v cursor=%v", durable, err, input.Valid())
	}

	badInput := runtimeCursorAt(generation, previous, frames[len(frames)-1].Sequence)
	badDurable := durableCursor.clone()
	badDurable.lineageIndexNextSequence--
	if _, err := finishConsumedRotationAppend(badInput, generation, appendOutcomeDurable, &badDurable, candidate, candidateCheckpoint, header, headerCheckpoint); err == nil || !badInput.Valid() {
		t.Fatalf("pre-consume rotation contradiction changed cursor: %v", err)
	}
}

func TestOwnedRecordRejectsMultipleBranchesAndAppendResultShapeFaults(t *testing.T) {
	document := fixtureObject(t, migrationFixturePath(t, "golden/evidence-record-chain-v1.json"))
	frames := decodeEvidenceFrames(t, document["frames"])
	context := fixtureObjectValue(t, document["validation_context"], "validation context")
	chain := buildEvidenceWitness(t, frames, context)
	owner := &evidenceOwnerToken{nonce: [16]byte{11}}
	generation := recoveryFixtureGeneration(owner, *frames[0].Record.Header)
	terminal := frames[len(frames)-1]
	cursor := runtimeCursorAt(generation, frames[len(frames)-2].RecordDigest, terminal.Sequence)
	witness := func(c JournalCursor) ownedAttemptTerminalWitness {
		return ownedAttemptTerminalWitness{ownedAppendContext{generation, c, frames[:len(frames)-1], chain}, terminal.Record.AttemptTerminal.TerminalDigest, nil, 3}
	}
	multiple := cloneEvidenceRecord(terminal.Record)
	intent := cloneProjectionValue(*frames[1].Record.StatementIntent)
	multiple.StatementIntent = &intent
	if _, err := bindOwnedEvidenceRecord(multiple, witness(cursor)); err == nil {
		t.Fatal("accepted multiple union branches")
	}
	newOwned := func(c JournalCursor) *OwnedEvidenceRecord {
		owned, err := bindOwnedEvidenceRecord(terminal.Record, witness(c))
		if err != nil {
			t.Fatal(err)
		}
		return owned
	}
	if _, err := finishAppend(cursor, newOwned(cursor), generation, appendOutcome("bogus"), nil, terminal.RecordDigest, DigestBytes([]byte("checkpoint"))); err == nil || !cursor.Valid() {
		t.Fatal("pre-consume unknown kind changed cursor or was accepted")
	}
	wrongDurable := runtimeCursorAt(generation, DigestBytes([]byte("wrong")), cursor.nextSequence+1)
	if _, err := finishAppend(cursor, newOwned(cursor), generation, appendOutcomeDurable, &wrongDurable, terminal.RecordDigest, DigestBytes([]byte("checkpoint"))); err == nil || !cursor.Valid() {
		t.Fatal("pre-consume durable contradiction changed cursor or was accepted")
	}
	durable := runtimeCursorAt(generation, terminal.RecordDigest, cursor.nextSequence+1)
	result, err := finishAppend(cursor, newOwned(cursor), generation, appendOutcomeDurable, &durable, terminal.RecordDigest, DigestBytes([]byte("checkpoint")))
	if err != nil || result.DurableCursor() == nil || cursor.Valid() {
		t.Fatalf("durable append authority: %v", err)
	}
}

func TestEvidenceRuntimeProductionConstructorSealsCanonicalLocatorAndHasNoForbiddenImports(t *testing.T) {
	for name, rootPath := range map[string]string{
		"empty": "", "relative": "evidence", "unclean": "/var/lib/../evidence", "filesystem-root": "/",
	} {
		t.Run(name, func(t *testing.T) {
			if sink, err := NewEvidenceSink(rootPath); sink != nil || !IsCode(err, CodeEvidenceJournalFailed) {
				t.Fatalf("invalid sink locator accepted: %T %v", sink, err)
			}
		})
	}
	sink, err := NewEvidenceSink("/evidence")
	if err != nil {
		t.Fatal(err)
	}
	concrete, ok := sink.(*evidenceFSSink)
	if !ok || !concrete.valid() {
		t.Fatalf("production sink was not sealed: %T", sink)
	}
	copySink := *concrete
	if copySink.valid() || (&evidenceFSSink{rootPath: "/evidence"}).valid() || (&evidenceFSSink{}).valid() {
		t.Fatal("copied or literal sink acquired production authority")
	}
	if session, snapshot, openErr := copySink.Open(context.Background(), VerifiedEvidenceRun{}, VerifiedRuntimeArtifact{}); session != nil || snapshot != nil || !IsCode(openErr, CodeEvidenceJournalFailed) {
		t.Fatalf("copied sink returned a non-closed result: session=%T snapshot=%+v err=%v", session, snapshot, openErr)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if session, snapshot, openErr := sink.Open(canceled, VerifiedEvidenceRun{}, VerifiedRuntimeArtifact{}); session != nil || snapshot != nil || !IsCode(openErr, CodeContextCanceled) {
		t.Fatalf("canceled Open returned a non-closed result: session=%T snapshot=%+v err=%v", session, snapshot, openErr)
	}
	if session, snapshot, openErr := sink.Open(context.Background(), VerifiedEvidenceRun{}, VerifiedRuntimeArtifact{}); session != nil || snapshot != nil || !IsCode(openErr, CodeEvidenceRecoveryRequired) {
		t.Fatalf("unbound candidate reached the filesystem: session=%T snapshot=%+v err=%v", session, snapshot, openErr)
	}
	root := filepath.Dir(mustSourceFile(t))
	for _, name := range []string{"evidence_runtime.go", "evidence_sink.go", "evidence_recovery.go", "evidence_trust_recovery.go"} {
		file, err := parser.ParseFile(token.NewFileSet(), filepath.Join(root, name), nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, spec := range file.Imports {
			path := spec.Path.Value
			if path == `"os"` || path == `"io/fs"` || path == `"github.com/jackc/pgx/v5"` {
				t.Fatalf("%s imports forbidden %s", name, path)
			}
		}
		ast.Inspect(file, func(ast.Node) bool { return true })
	}
}

func TestEvidenceSinkSessionBindTransfersCleanupOnlyOnClosedSuccess(t *testing.T) {
	releaseCalls := 0
	cleanup := &evidenceSinkOpenCleanup{release: func() error { releaseCalls++; return nil }}
	session := &runnerEvidenceSessionFake{journal: &runnerEvidenceJournalFake{}}
	bindErr := admissionCorrupt("test-bind", "bind failed", nil)
	if got, err := finishEvidenceSessionBind(session, bindErr, cleanup, "test-session"); got != nil || err != bindErr || session.closeCalls != 1 || cleanup.release == nil {
		t.Fatalf("defensive bind result=%T err=%v closes=%d release=%v", got, err, session.closeCalls, cleanup.release != nil)
	}
	if err := cleanup.release(); err != nil || releaseCalls != 1 {
		t.Fatalf("retained cleanup err=%v calls=%d", err, releaseCalls)
	}
	cleanup = &evidenceSinkOpenCleanup{release: func() error { releaseCalls++; return nil }}
	session = &runnerEvidenceSessionFake{journal: &runnerEvidenceJournalFake{}, closeErr: admissionCorrupt("test-close", "close failed", nil)}
	if got, err := finishEvidenceSessionBind(session, bindErr, cleanup, "test-session"); got != nil || !IsCode(err, CodeEvidenceJournalFailed) || session.closeCalls != 1 || cleanup.release == nil {
		t.Fatalf("cleanup dominance result=%T err=%v closes=%d release=%v", got, err, session.closeCalls, cleanup.release != nil)
	}

	cleanup = &evidenceSinkOpenCleanup{release: func() error { releaseCalls++; return nil }}
	if got, err := finishEvidenceSessionBind(nil, nil, cleanup, "test-session"); got != nil || !IsCode(err, CodeEvidenceJournalFailed) || cleanup.release == nil {
		t.Fatalf("nil bind result=%T err=%v release=%v", got, err, cleanup.release != nil)
	}

	cleanup = &evidenceSinkOpenCleanup{release: func() error { releaseCalls++; return nil }}
	session = &runnerEvidenceSessionFake{journal: &runnerEvidenceJournalFake{}}
	if got, err := finishEvidenceSessionBind(session, nil, cleanup, "test-session"); got != session || err != nil || cleanup.release != nil || session.closed {
		t.Fatalf("successful bind result=%T err=%v release=%v closed=%v", got, err, cleanup.release != nil, session.closed)
	}
}

func runtimeCursorAt(g generationIdentity, previous Digest, sequence uint64) JournalCursor {
	valid := &atomic.Bool{}
	valid.Store(true)
	return JournalCursor{owner: g.owner, generation: g, nextSequence: sequence, previousRecordDigest: digestPointer(previous), lineageIndexNextSequence: 1, lineageIndexPreviousRecordDigest: DigestBytes([]byte("lineage")), valid: valid}
}
func mustSourceFile(t *testing.T) string {
	t.Helper()
	path, err := filepath.Abs("evidence_runtime_test.go")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	return path
}
