package migration

import (
	"context"
	"crypto/sha256"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/evidencefs"
)

func TestHistoricalSuccessorHeaderAndActivationRejectLiterals(t *testing.T) {
	candidate := quotaCandidateForBundle(t, quotaAdmissionBundleForTest(t), []byte("historical-successor-activation-literal"))
	header, err := (&HistoricalSuccessorReservedDurablePermit{}).CreateGenerationHeader(context.Background(), candidate)
	if header.Next() != nil || header.Outcome() != evidencefs.AdmissionTransitionPreMutationFailure || header.CandidateKind() != "generation_header" || header.CandidateSequence() != 2 || !IsCode(err, CodeEvidenceRecoveryRequired) {
		t.Fatalf("literal historical reserved permit escaped: result=%+v err=%v", header, err)
	}
	activated, err := (&HistoricalSuccessorHeaderDurablePermit{}).AppendGenerationActivated(context.Background(), candidate)
	if activated.Next() != nil || activated.Outcome() != evidencefs.AdmissionTransitionPreMutationFailure || activated.CandidateKind() != "generation_activated" || activated.CandidateSequence() != 3 || !IsCode(err, CodeEvidenceRecoveryRequired) {
		t.Fatalf("literal historical header permit escaped: result=%+v err=%v", activated, err)
	}
	if validHistoricalSuccessorHeaderPermit(&HistoricalSuccessorHeaderDurablePermit{}, candidate) || validHistoricalSuccessorGenerationReadyPermit(&HistoricalSuccessorGenerationReadyPermit{}, candidate) {
		t.Fatal("literal historical activation authority was accepted")
	}
	if _, ok := any(&HistoricalSuccessorGenerationReadyPermit{}).(interface{ ActiveGeneration() ActiveGeneration }); ok {
		t.Fatal("historical generation-ready permit exposed runtime authority")
	}
}

func TestHistoricalSuccessorActivationDigestsBindEveryStageFact(t *testing.T) {
	chain, generation := historicalSupersessionFrameFixture(t)
	lineage := digestRaw(chain[1].Record.Reserved.ExecutionLineageDigest)
	_, reserved, header, _, reservedBytes, err := buildHistoricalSupersessionFrames(lineage, uint64(len(chain)), chain[len(chain)-1].RecordDigest, generation)
	if err != nil {
		t.Fatal(err)
	}
	headerBytes, err := EncodeCanonicalEvidenceFrame(header)
	if err != nil {
		t.Fatal(err)
	}
	activated, activatedBytes, err := buildSuccessorActivatedFrame(reserved, header)
	if err != nil {
		t.Fatal(err)
	}
	source := &verifiedAdmissionRegisteredGeneration{replay: &verifiedAdmissionGenerationReplay{supersessionDebited: true}, canonical: [32]byte{1}}
	planned := &verifiedAdmissionRegisteredGeneration{canonical: [32]byte{2}}
	authority := &VerifiedLineageSupersessionAuthority{digest: generation.supersessionAuthorityDigest}
	receipt := &verifiedHistoricalSupersessionReceipt{authorityDigest: authority.digest}
	binding := &verifiedEvidenceRunBinding{canonical: [32]byte{3}}
	mutation := &evidencefs.AdmissionMutationToken{}
	headerPermit := &HistoricalSuccessorHeaderDurablePermit{
		candidateBinding: binding, mutation: mutation, target: lineage, fullSet: [32]byte{4}, revision: 2,
		indexRecords: uint64(len(chain)) + 1, indexTail: reserved.RecordDigest, indexDigest: [32]byte{5}, indexSize: 1024,
		source: source, planned: planned, plannedRuntime: VerifiedRuntimeArtifact{digest: generation.plannedSuccessor.header.outerArtifactDigest, sizeBytes: generation.plannedSuccessor.header.outerArtifactSize},
		authority: authority, receipt: receipt, reservedFrame: reserved, headerFrame: header, reservedFrameBytes: reservedBytes,
		headerBytes: headerBytes, headerBytesHash: sha256Bytes(headerBytes), journal: reserved.Record.Reserved.JournalIdentityDigest, journalCount: 1,
		priorCanonical: [32]byte{6}, consumed: &atomic.Bool{},
	}
	headerPermit.self = headerPermit
	headerBaseline := historicalSuccessorHeaderDigest(headerPermit)
	if headerBaseline == ([32]byte{}) {
		t.Fatal("historical header fixture did not seal")
	}
	for name, mutate := range map[string]func(*HistoricalSuccessorHeaderDurablePermit){
		"prior":   func(v *HistoricalSuccessorHeaderDurablePermit) { v.priorCanonical[0] ^= 1 },
		"index":   func(v *HistoricalSuccessorHeaderDurablePermit) { v.indexDigest[0] ^= 1 },
		"source":  func(v *HistoricalSuccessorHeaderDurablePermit) { v.source.canonical[0] ^= 1 },
		"planned": func(v *HistoricalSuccessorHeaderDurablePermit) { v.planned.canonical[0] ^= 1 },
		"runtime": func(v *HistoricalSuccessorHeaderDurablePermit) {
			v.plannedRuntime.digest = testDigest("other-header-runtime")
		},
		"authority": func(v *HistoricalSuccessorHeaderDurablePermit) {
			v.authority.digest = testDigest("other-header-authority")
		},
		"header":  func(v *HistoricalSuccessorHeaderDurablePermit) { v.headerBytes[0] ^= 1 },
		"journal": func(v *HistoricalSuccessorHeaderDurablePermit) { v.journal = testDigest("other-header-journal") },
	} {
		t.Run("header-"+name, func(t *testing.T) {
			value := cloneHistoricalHeaderDigestFixture(headerPermit)
			mutate(value)
			if historicalSuccessorHeaderDigest(value) == headerBaseline {
				t.Fatal("historical header mutation retained canonical digest")
			}
		})
	}

	ready := &HistoricalSuccessorGenerationReadyPermit{
		candidateBinding: binding, mutation: mutation, target: lineage, fullSet: [32]byte{7}, revision: 3,
		indexRecords: uint64(len(chain)) + 2, indexTail: activated.RecordDigest, indexDigest: [32]byte{8}, indexSize: 2048,
		source: source, planned: planned, plannedRuntime: headerPermit.plannedRuntime, authority: authority, receipt: receipt,
		reservedFrame: reserved, headerFrame: header, activatedFrame: activated, reservedFrameBytes: reservedBytes, activatedBytes: activatedBytes,
		headerBytes: headerBytes, headerBytesHash: sha256Bytes(headerBytes), journal: headerPermit.journal, journalCount: 1,
		priorCanonical: headerBaseline, consumed: &atomic.Bool{},
	}
	ready.self = ready
	readyBaseline := historicalSuccessorGenerationReadyDigest(ready)
	if readyBaseline == ([32]byte{}) {
		t.Fatal("historical generation-ready fixture did not seal")
	}
	for name, mutate := range map[string]func(*HistoricalSuccessorGenerationReadyPermit){
		"prior": func(v *HistoricalSuccessorGenerationReadyPermit) { v.priorCanonical[0] ^= 1 },
		"index": func(v *HistoricalSuccessorGenerationReadyPermit) { v.indexDigest[0] ^= 1 },
		"runtime": func(v *HistoricalSuccessorGenerationReadyPermit) {
			v.plannedRuntime.digest = testDigest("other-ready-runtime")
		},
		"authority": func(v *HistoricalSuccessorGenerationReadyPermit) {
			v.authority.digest = testDigest("other-ready-authority")
		},
		"activated": func(v *HistoricalSuccessorGenerationReadyPermit) { v.activatedBytes[0] ^= 1 },
		"header":    func(v *HistoricalSuccessorGenerationReadyPermit) { v.headerBytes[0] ^= 1 },
	} {
		t.Run("ready-"+name, func(t *testing.T) {
			value := cloneHistoricalReadyDigestFixture(ready)
			mutate(value)
			if historicalSuccessorGenerationReadyDigest(value) == readyBaseline {
				t.Fatal("historical generation-ready mutation retained canonical digest")
			}
		})
	}
}

func TestHistoricalSuccessorActivationAuthorityDoesNotSpread(t *testing.T) {
	allowed := map[string]map[string]bool{
		"evidence_historical_supersession_activation.go": {
			"HistoricalSuccessorHeaderDurablePermit":   true,
			"HistoricalSuccessorGenerationReadyPermit": true,
		},
		"evidence_historical_supersession_handoff.go": {
			"HistoricalSuccessorGenerationReadyPermit": true,
		},
		"evidence_generation_journal.go": {
			"HistoricalSuccessorGenerationReadyPermit": true,
		},
	}
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			identifier, ok := node.(*ast.Ident)
			if ok && (identifier.Name == "HistoricalSuccessorHeaderDurablePermit" || identifier.Name == "HistoricalSuccessorGenerationReadyPermit") && !allowed[name][identifier.Name] {
				t.Fatalf("historical successor activation authority spread into %s", name)
			}
			return true
		})
	}
}

func cloneHistoricalHeaderDigestFixture(value *HistoricalSuccessorHeaderDurablePermit) *HistoricalSuccessorHeaderDurablePermit {
	result := *value
	result.self = &result
	result.source = &verifiedAdmissionRegisteredGeneration{replay: &verifiedAdmissionGenerationReplay{supersessionDebited: true}, canonical: value.source.canonical}
	result.planned = &verifiedAdmissionRegisteredGeneration{canonical: value.planned.canonical}
	result.authority = &VerifiedLineageSupersessionAuthority{digest: value.authority.digest}
	result.receipt = &verifiedHistoricalSupersessionReceipt{authorityDigest: value.receipt.authorityDigest}
	result.reservedFrame = cloneProjectionValue(value.reservedFrame)
	result.headerFrame = cloneProjectionValue(value.headerFrame)
	result.reservedFrameBytes = append([]byte(nil), value.reservedFrameBytes...)
	result.headerBytes = append([]byte(nil), value.headerBytes...)
	return &result
}

func cloneHistoricalReadyDigestFixture(value *HistoricalSuccessorGenerationReadyPermit) *HistoricalSuccessorGenerationReadyPermit {
	result := *value
	result.self = &result
	result.source = &verifiedAdmissionRegisteredGeneration{replay: &verifiedAdmissionGenerationReplay{supersessionDebited: true}, canonical: value.source.canonical}
	result.planned = &verifiedAdmissionRegisteredGeneration{canonical: value.planned.canonical}
	result.authority = &VerifiedLineageSupersessionAuthority{digest: value.authority.digest}
	result.receipt = &verifiedHistoricalSupersessionReceipt{authorityDigest: value.receipt.authorityDigest}
	result.reservedFrame = cloneProjectionValue(value.reservedFrame)
	result.headerFrame = cloneProjectionValue(value.headerFrame)
	result.activatedFrame = cloneProjectionValue(value.activatedFrame)
	result.reservedFrameBytes = append([]byte(nil), value.reservedFrameBytes...)
	result.headerBytes = append([]byte(nil), value.headerBytes...)
	result.activatedBytes = append([]byte(nil), value.activatedBytes...)
	return &result
}

func sha256Bytes(raw []byte) [32]byte { return sha256.Sum256(raw) }
