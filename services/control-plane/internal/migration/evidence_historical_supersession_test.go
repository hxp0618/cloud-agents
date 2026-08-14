package migration

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/evidencefs"
)

func TestHistoricalSupersessionFramesRebuildStoredAdjacentReservation(t *testing.T) {
	chain, generation := historicalSupersessionFrameFixture(t)
	lineage := digestRaw(chain[1].Record.Reserved.ExecutionLineageDigest)
	superseded, reserved, header, supersededBytes, reservedBytes, err := buildHistoricalSupersessionFrames(lineage, uint64(len(chain)), chain[len(chain)-1].RecordDigest, generation)
	if err != nil {
		t.Fatal(err)
	}
	wantSupersededBytes, err := EncodeCanonicalLineageFrame(chain[len(chain)-1])
	if err != nil {
		t.Fatal(err)
	}
	if !canonicalEqual(superseded, chain[len(chain)-1]) || string(supersededBytes) != string(wantSupersededBytes) || reserved.Sequence != uint64(len(chain)) || reserved.PreviousRecordDigest == nil || *reserved.PreviousRecordDigest != superseded.RecordDigest || !canonicalEqual(reserved.Record.Reserved, superseded.Record.Superseded.PlannedGenerationReserved) || reserved.Record.Reserved.ExpectedSegment0HeaderDigest != header.RecordDigest || len(reservedBytes) == 0 {
		t.Fatalf("historical successor frames are not exact: superseded=%+v reserved=%+v header=%+v", superseded, reserved, header)
	}

	for name, mutate := range map[string]func(*uint64, *Digest, *admissionReplayGeneration){
		"records": func(records *uint64, _ *Digest, _ *admissionReplayGeneration) { (*records)++ },
		"tail":    func(_ *uint64, tail *Digest, _ *admissionReplayGeneration) { *tail = testDigest("other-tail") },
		"authority": func(_ *uint64, _ *Digest, value *admissionReplayGeneration) {
			value.supersessionAuthorityDigest = testDigest("other-authority")
		},
		"outcome": func(_ *uint64, _ *Digest, value *admissionReplayGeneration) {
			value.supersessionOutcome = "exact_pending"
		},
		"planned": func(_ *uint64, _ *Digest, value *admissionReplayGeneration) { value.plannedSuccessor = nil },
		"predecessor": func(_ *uint64, _ *Digest, value *admissionReplayGeneration) {
			value.oldCheckpointRecordDigest = digestPointer(testDigest("other-checkpoint"))
		},
	} {
		t.Run(name, func(t *testing.T) {
			records := uint64(len(chain))
			tail := chain[len(chain)-1].RecordDigest
			candidate := cloneAdmissionGeneration(generation)
			mutate(&records, &tail, &candidate)
			if _, _, _, _, _, err := buildHistoricalSupersessionFrames(lineage, records, tail, candidate); !IsCode(err, CodeEvidenceJournalCorrupt) {
				t.Fatalf("mutated stored supersession was accepted: %v", err)
			}
		})
	}
}

func TestHistoricalSupersessionAdjacentDigestBindsEveryOrdinaryField(t *testing.T) {
	chain, generation := historicalSupersessionFrameFixture(t)
	lineage := digestRaw(chain[1].Record.Reserved.ExecutionLineageDigest)
	superseded, reserved, header, supersededBytes, reservedBytes, err := buildHistoricalSupersessionFrames(lineage, uint64(len(chain)), chain[len(chain)-1].RecordDigest, generation)
	if err != nil {
		t.Fatal(err)
	}
	source := &verifiedAdmissionRegisteredGeneration{replay: &verifiedAdmissionGenerationReplay{supersessionDebited: true}, canonical: [32]byte{1}}
	planned := &verifiedAdmissionRegisteredGeneration{canonical: [32]byte{2}}
	authority := &VerifiedLineageSupersessionAuthority{digest: generation.supersessionAuthorityDigest}
	receipt := &verifiedHistoricalSupersessionReceipt{
		authorityDigest: authority.digest,
		runtimeReceipt:  VerifiedContentReceipt{digest: generation.plannedSuccessor.header.outerArtifactDigest, sizeBytes: generation.plannedSuccessor.header.outerArtifactSize},
		recoveryReceipt: VerifiedDecisionRecoveryReceipt{digest: generation.plannedSuccessor.header.recoveryArtifactDigest, sizeBytes: generation.plannedSuccessor.header.recoveryArtifactSize},
	}
	ready := &HistoricalSupersessionAdjacentReserveReady{
		candidateBinding: &verifiedEvidenceRunBinding{canonical: [32]byte{3}}, mutation: &evidencefs.AdmissionMutationToken{},
		target: lineage, fullSet: [32]byte{4}, transcriptCanonical: [32]byte{5}, indexRecords: uint64(len(chain)), indexTail: superseded.RecordDigest,
		indexDigest: [32]byte{6}, indexSize: 512, source: source, planned: planned,
		plannedRuntime: VerifiedRuntimeArtifact{digest: generation.plannedSuccessor.header.outerArtifactDigest, sizeBytes: generation.plannedSuccessor.header.outerArtifactSize},
		authority:      authority, receipt: receipt, supersededFrame: superseded, reservedFrame: reserved, headerFrame: header,
		supersededFrameBytes: supersededBytes, reservedFrameBytes: reservedBytes, consumed: &atomic.Bool{},
	}
	ready.self = ready
	baseline := historicalSupersessionAdjacentDigest(ready)
	if baseline == ([32]byte{}) {
		t.Fatal("historical adjacent fixture did not produce a canonical digest")
	}
	for name, mutate := range map[string]func(*HistoricalSupersessionAdjacentReserveReady){
		"target":     func(v *HistoricalSupersessionAdjacentReserveReady) { v.target[0] ^= 1 },
		"full set":   func(v *HistoricalSupersessionAdjacentReserveReady) { v.fullSet[0] ^= 1 },
		"transcript": func(v *HistoricalSupersessionAdjacentReserveReady) { v.transcriptCanonical[0] ^= 1 },
		"index":      func(v *HistoricalSupersessionAdjacentReserveReady) { v.indexDigest[0] ^= 1 },
		"source":     func(v *HistoricalSupersessionAdjacentReserveReady) { v.source.canonical[0] ^= 1 },
		"planned":    func(v *HistoricalSupersessionAdjacentReserveReady) { v.planned.canonical[0] ^= 1 },
		"root":       func(v *HistoricalSupersessionAdjacentReserveReady) { v.rootFacts.indexCount++ },
		"quota":      func(v *HistoricalSupersessionAdjacentReserveReady) { v.quotaAdmission.indexCount++ },
		"runtime": func(v *HistoricalSupersessionAdjacentReserveReady) {
			v.plannedRuntime.digest = testDigest("other-runtime")
		},
		"authority": func(v *HistoricalSupersessionAdjacentReserveReady) {
			v.authority.digest = testDigest("other-authority")
		},
		"receipt": func(v *HistoricalSupersessionAdjacentReserveReady) {
			v.receipt.runtimeReceipt.digest = testDigest("other-receipt")
		},
		"superseded bytes": func(v *HistoricalSupersessionAdjacentReserveReady) { v.supersededFrameBytes[0] ^= 1 },
		"reserved bytes":   func(v *HistoricalSupersessionAdjacentReserveReady) { v.reservedFrameBytes[0] ^= 1 },
	} {
		t.Run(name, func(t *testing.T) {
			value := *ready
			value.self = &value
			value.source = &verifiedAdmissionRegisteredGeneration{replay: &verifiedAdmissionGenerationReplay{supersessionDebited: true}, canonical: ready.source.canonical}
			value.planned = &verifiedAdmissionRegisteredGeneration{canonical: ready.planned.canonical}
			value.authority = &VerifiedLineageSupersessionAuthority{digest: ready.authority.digest}
			value.receipt = &verifiedHistoricalSupersessionReceipt{authorityDigest: ready.receipt.authorityDigest, runtimeReceipt: ready.receipt.runtimeReceipt, recoveryReceipt: ready.receipt.recoveryReceipt}
			value.supersededFrame = cloneProjectionValue(ready.supersededFrame)
			value.reservedFrame = cloneProjectionValue(ready.reservedFrame)
			value.headerFrame = cloneProjectionValue(ready.headerFrame)
			value.supersededFrameBytes = append([]byte(nil), ready.supersededFrameBytes...)
			value.reservedFrameBytes = append([]byte(nil), ready.reservedFrameBytes...)
			mutate(&value)
			if historicalSupersessionAdjacentDigest(&value) == baseline {
				t.Fatal("historical adjacent mutation retained canonical digest")
			}
		})
	}

	reservedPermit := &HistoricalSuccessorReservedDurablePermit{
		candidateBinding: ready.candidateBinding, mutation: ready.mutation,
		target: ready.target, fullSet: ready.fullSet, revision: ready.revision + 1,
		indexRecords: ready.indexRecords + 1, indexTail: reserved.RecordDigest, indexDigest: [32]byte{7}, indexSize: ready.indexSize + uint64(len(reservedBytes)),
		source: source, planned: planned, plannedRuntime: ready.plannedRuntime, authority: authority, receipt: receipt,
		reservedFrame: reserved, headerFrame: header, reservedFrameBytes: append([]byte(nil), reservedBytes...),
		readyCanonical: baseline, consumed: &atomic.Bool{},
	}
	reservedPermit.self = reservedPermit
	reservedBaseline := historicalSuccessorReservedDigest(reservedPermit)
	if reservedBaseline == ([32]byte{}) {
		t.Fatal("historical reserved fixture did not produce a canonical digest")
	}
	for name, mutate := range map[string]func(*HistoricalSuccessorReservedDurablePermit){
		"ready": func(v *HistoricalSuccessorReservedDurablePermit) { v.readyCanonical[0] ^= 1 },
		"index": func(v *HistoricalSuccessorReservedDurablePermit) { v.indexDigest[0] ^= 1 },
		"runtime": func(v *HistoricalSuccessorReservedDurablePermit) {
			v.plannedRuntime.digest = testDigest("reserved-other-runtime")
		},
		"authority": func(v *HistoricalSuccessorReservedDurablePermit) {
			v.authority.digest = testDigest("reserved-other-authority")
		},
		"reserved": func(v *HistoricalSuccessorReservedDurablePermit) { v.reservedFrameBytes[0] ^= 1 },
		"header": func(v *HistoricalSuccessorReservedDurablePermit) {
			v.headerFrame.RecordDigest = testDigest("reserved-other-header")
		},
	} {
		t.Run("reserved-"+name, func(t *testing.T) {
			value := *reservedPermit
			value.self = &value
			value.source = &verifiedAdmissionRegisteredGeneration{replay: &verifiedAdmissionGenerationReplay{supersessionDebited: true}, canonical: reservedPermit.source.canonical}
			value.planned = &verifiedAdmissionRegisteredGeneration{canonical: reservedPermit.planned.canonical}
			value.authority = &VerifiedLineageSupersessionAuthority{digest: reservedPermit.authority.digest}
			value.receipt = &verifiedHistoricalSupersessionReceipt{authorityDigest: reservedPermit.receipt.authorityDigest, runtimeReceipt: reservedPermit.receipt.runtimeReceipt, recoveryReceipt: reservedPermit.receipt.recoveryReceipt}
			value.reservedFrame = cloneProjectionValue(reservedPermit.reservedFrame)
			value.headerFrame = cloneProjectionValue(reservedPermit.headerFrame)
			value.reservedFrameBytes = append([]byte(nil), reservedPermit.reservedFrameBytes...)
			mutate(&value)
			if historicalSuccessorReservedDigest(&value) == reservedBaseline {
				t.Fatal("historical reserved mutation retained canonical digest")
			}
		})
	}
}

func TestHistoricalSupersessionAdjacentAuthorityRejectsLiteralsBeforeReplay(t *testing.T) {
	candidate := quotaCandidateForBundle(t, quotaAdmissionBundleForTest(t), []byte("historical-supersession-literal"))
	if validHistoricalSupersessionAdjacentReady(&HistoricalSupersessionAdjacentReserveReady{}, candidate) {
		t.Fatal("literal historical adjacent authority was accepted")
	}
	transition, err := (&HistoricalSupersessionAdjacentReserveReady{}).AppendGenerationReserved(context.Background(), candidate)
	if transition.Next() != nil || transition.Outcome() != evidencefs.AdmissionTransitionPreMutationFailure || transition.CandidateKind() != "generation_reserved" || transition.CandidateSequence() != 1 || !IsCode(err, CodeEvidenceRecoveryRequired) {
		t.Fatalf("literal historical adjacent transition escaped: result=%+v err=%v", transition, err)
	}
	if validHistoricalSuccessorReservedPermit(&HistoricalSuccessorReservedDurablePermit{}, candidate) || historicalSuccessorReservedDigest(&HistoricalSuccessorReservedDurablePermit{}) != ([32]byte{}) {
		t.Fatal("literal historical reserved permit was accepted")
	}
	if ready, err := bindHistoricalSupersessionAdjacentReserveReady(context.Background(), &evidencefs.AdmissionInventory{}, OwnedCurrentCandidate{}); ready != nil || !IsCode(err, CodeEvidenceRecoveryRequired) {
		t.Fatalf("literal inventory/candidate entered historical replay: ready=%+v err=%v", ready, err)
	}
}

func TestHistoricalSupersessionPolicyErrorMappingIsClosed(t *testing.T) {
	for name, test := range map[string]struct {
		input error
		code  ErrorCode
	}{
		"canceled": {context.Canceled, CodeContextCanceled},
		"deadline": {context.DeadlineExceeded, CodeDeadlineExceeded},
		"corrupt":  {admissionCorrupt("test", "stored mismatch", nil), CodeEvidenceJournalCorrupt},
		"recovery": {fail(CodeUntrusted, "test", "verifier rejected", nil), CodeEvidenceRecoveryRequired},
	} {
		t.Run(name, func(t *testing.T) {
			if err := mapHistoricalSupersessionPolicyError(test.input); !IsCode(err, test.code) {
				t.Fatalf("policy error mapping=%v want=%s", err, test.code)
			}
		})
	}
}

func TestHistoricalSupersessionRecoveryAuthorityDoesNotSpread(t *testing.T) {
	allowed := map[string]bool{"evidence_historical_supersession.go": true, "evidence_historical_supersession_activation.go": true, "evidence_admission_history.go": true, "evidence_trust_recovery.go": true}
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || allowed[name] || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			identifier, ok := node.(*ast.Ident)
			if ok && (identifier.Name == "HistoricalSupersessionAdjacentReserveReady" || identifier.Name == "HistoricalSuccessorReservedDurablePermit" || identifier.Name == "bindHistoricalSupersessionAdjacentReserveReady" || identifier.Name == "recoverHistoricalSupersessionPolicy" || identifier.Name == "bindRecoveredHistoricalSupersessionPolicy" || identifier.Name == "bindHistoricalSupersessionRecoveryExecution" || identifier.Name == "recoveryPolicyAuthorizesDecision") {
				t.Fatalf("historical supersession recovery authority spread into %s", name)
			}
			return true
		})
	}
}

func historicalSupersessionFrameFixture(t *testing.T) ([]LineageIndexFrame, admissionReplayGeneration) {
	t.Helper()
	fixture := fixtureObject(t, migrationFixturePath(t, "golden/lineage-index-chain-v1.json"))
	base := decodeLineageFrames(t, fixture["frames"])
	if len(base) < 5 || base[1].Record.Reserved == nil || base[3].Record.Checkpoint == nil || base[4].Record.Superseded == nil || base[3].Record.Checkpoint.LastTerminalDigest == nil {
		t.Fatal("golden fixture lacks successor construction facts")
	}
	chain := cloneProjectionValue(base[:5])
	planned := cloneProjectionValue(*chain[1].Record.Reserved)
	plannedHeader := cloneProjectionValue(planned.PlannedSegment0Header)
	plannedHeader.OuterArtifactDigest = DigestBytes([]byte("historical-successor-runtime"))
	plannedHeader.OuterArtifactSizeBytes = uint64(len("historical-successor-runtime"))
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
	generations, err := compactAdmissionGenerations(chain)
	if err != nil || len(generations) != 1 || generations[0].plannedSuccessor == nil {
		t.Fatalf("compact historical successor fixture failed: generations=%+v err=%v", generations, err)
	}
	return chain, generations[0]
}
