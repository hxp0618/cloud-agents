package migration

import (
	"bytes"
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

func TestHistoricalSuccessorHandoffAndReplayRejectLiterals(t *testing.T) {
	candidate := quotaCandidateForBundle(t, quotaAdmissionBundleForTest(t), []byte("historical-successor-handoff-literal"))
	result, err := (&HistoricalSuccessorGenerationReadyPermit{}).Handoff(context.Background(), candidate)
	if result.Next() != nil || result.Outcome() != evidencefs.AdmissionTransitionPreMutationFailure || result.CandidateKind() != "generation_handoff" || result.CandidateSequence() != 4 || !IsCode(err, CodeEvidenceRecoveryRequired) {
		t.Fatalf("literal historical generation entered handoff: result=%+v err=%v", result, err)
	}
	if validHistoricalSuccessorGenerationHandoffReady(&HistoricalSuccessorGenerationHandoffReady{}, candidate) {
		t.Fatal("literal historical handoff passed validation")
	}
	replay, err := (&HistoricalSuccessorGenerationHandoffReady{}).Replay(context.Background(), candidate)
	if replay.Next() != nil || !IsCode(err, CodeEvidenceRecoveryRequired) {
		t.Fatalf("literal historical handoff entered replay: result=%+v err=%v", replay, err)
	}
	if validHistoricalSuccessorGenerationReplayReady(&HistoricalSuccessorGenerationReplayReady{}, candidate) {
		t.Fatal("literal historical replay passed validation")
	}
	if err := (&HistoricalSuccessorGenerationHandoffReady{}).Close(); !IsCode(err, CodeEvidenceJournalFailed) {
		t.Fatalf("literal historical handoff close=%v", err)
	}
	if err := (&HistoricalSuccessorGenerationReplayReady{}).Close(); !IsCode(err, CodeEvidenceJournalFailed) {
		t.Fatalf("literal historical replay close=%v", err)
	}
}

func TestHistoricalSuccessorHandoffAndReplayAreNotRuntimeAuthority(t *testing.T) {
	for name, value := range map[string]any{
		"handoff": &HistoricalSuccessorGenerationHandoffReady{},
		"replay":  &HistoricalSuccessorGenerationReplayReady{},
	} {
		t.Run(name, func(t *testing.T) {
			if _, ok := value.(EvidenceJournal); ok {
				t.Fatal("pre-recovery value implemented EvidenceJournal")
			}
			if _, ok := value.(interface{ Cursor() JournalCursor }); ok {
				t.Fatal("pre-recovery value exposed JournalCursor")
			}
			if _, ok := value.(interface{ ActiveGeneration() ActiveGeneration }); ok {
				t.Fatal("pre-recovery value exposed ActiveGeneration")
			}
		})
	}
}

func TestHistoricalSuccessorHandoffDigestsRejectCopyAndMutation(t *testing.T) {
	candidate := quotaCandidateForBundle(t, quotaAdmissionBundleForTest(t), []byte("historical-successor-handoff-digest"))
	prior, _, frames, _, fact := historicalSuccessorGenerationReplayFixture(t)
	if historicalSuccessorHandoffCandidateDigest(prior) == ([32]byte{}) {
		t.Fatal("historical successor handoff candidate digest is empty")
	}
	handoff := &HistoricalSuccessorGenerationHandoffReady{
		prior: prior, candidateBinding: candidate.binding, lease: &evidencefs.GenerationLease{},
		target: prior.target, journal: prior.journal, revision: prior.revision, consumed: &atomic.Bool{},
	}
	handoff.self = handoff
	handoff.binding = &historicalSuccessorGenerationHandoffBinding{ready: handoff, prior: prior, candidateBinding: candidate.binding, lease: handoff.lease}
	handoff.binding.canonical = historicalSuccessorGenerationHandoffDigest(handoff)
	if handoff.binding.canonical == ([32]byte{}) {
		t.Fatal("historical successor handoff digest is empty")
	}
	copyHandoff := *handoff
	if historicalSuccessorGenerationHandoffDigest(&copyHandoff) != ([32]byte{}) {
		t.Fatal("copied historical successor handoff retained digest authority")
	}
	for name, mutate := range map[string]func(*HistoricalSuccessorGenerationHandoffReady){
		"target": func(value *HistoricalSuccessorGenerationHandoffReady) { value.target[0]++ },
		"journal": func(value *HistoricalSuccessorGenerationHandoffReady) {
			value.journal = testDigest("other-historical-handoff-journal")
		},
		"revision": func(value *HistoricalSuccessorGenerationHandoffReady) { value.revision++ },
		"candidate": func(value *HistoricalSuccessorGenerationHandoffReady) {
			binding := *value.candidateBinding
			binding.canonical[0]++
			value.candidateBinding = &binding
		},
		"prior": func(value *HistoricalSuccessorGenerationHandoffReady) {
			priorCopy := *value.prior
			binding := *priorCopy.binding
			binding.canonical[0]++
			priorCopy.binding = &binding
			value.prior = &priorCopy
		},
	} {
		t.Run("handoff-"+name, func(t *testing.T) {
			value := *handoff
			value.self = &value
			mutate(&value)
			if historicalSuccessorGenerationHandoffDigest(&value) == handoff.binding.canonical {
				t.Fatal("historical successor handoff mutation retained canonical digest")
			}
		})
	}

	replay := &HistoricalSuccessorGenerationReplayReady{
		prior: handoff, candidateBinding: candidate.binding, lease: handoff.lease, snapshot: &evidencefs.GenerationSnapshot{},
		target: prior.target, journal: prior.journal, revision: prior.revision, snapshotIdentity: [32]byte{9},
		indexFact: fact, segmentFact: evidencefs.GenerationFileFact{Ordinal: 0, Size: 23, ContentDigest: [32]byte{7}, IdentityDigest: [32]byte{8}},
		indexRecords: uint64(len(frames)), segmentCount: 1, journalRecords: 1,
		journalTail: prior.headerFrame.RecordDigest, consumed: &atomic.Bool{},
	}
	replay.self = replay
	wantReplay := historicalSuccessorGenerationReplayDigest(replay)
	if wantReplay == ([32]byte{}) {
		t.Fatal("historical successor replay digest is empty")
	}
	copyReplay := *replay
	if historicalSuccessorGenerationReplayDigest(&copyReplay) != ([32]byte{}) {
		t.Fatal("copied historical successor replay retained digest authority")
	}
	for name, mutate := range map[string]func(*HistoricalSuccessorGenerationReplayReady){
		"snapshot": func(value *HistoricalSuccessorGenerationReplayReady) { value.snapshotIdentity[0]++ },
		"index":    func(value *HistoricalSuccessorGenerationReplayReady) { value.indexFact.IdentityDigest[0]++ },
		"segment":  func(value *HistoricalSuccessorGenerationReplayReady) { value.segmentFact.ContentDigest[0]++ },
		"records":  func(value *HistoricalSuccessorGenerationReplayReady) { value.indexRecords++ },
		"tail": func(value *HistoricalSuccessorGenerationReplayReady) {
			value.journalTail = testDigest("other-historical-replay-tail")
		},
	} {
		t.Run("replay-"+name, func(t *testing.T) {
			value := *replay
			value.self = &value
			mutate(&value)
			if historicalSuccessorGenerationReplayDigest(&value) == wantReplay {
				t.Fatal("historical successor replay mutation retained canonical digest")
			}
		})
	}
}

func TestHistoricalSuccessorReplayStrictlyBindsAdjacentIndexAndHeaderOnlyJournal(t *testing.T) {
	prior, raw, frames, headerRaw, fact := historicalSuccessorGenerationReplayFixture(t)
	if err := validateHistoricalSuccessorReplayIndex(prior, raw, frames, fact); err != nil {
		t.Fatal(err)
	}
	plan, err := scanLineageChainStructure(frames)
	if err != nil {
		t.Fatal(err)
	}
	stream, registered := openEvidenceJournalStructuralStream(plan, prior.journal, nil)
	if !registered || stream == nil {
		t.Fatal("historical successor journal was not registered by strict index replay")
	}
	if err := stream.beginSegment(); err != nil {
		t.Fatal(err)
	}
	records, tail, first, err := streamGenerationReplaySegment(headerRaw, stream)
	if err == nil {
		err = stream.endSegment()
	}
	var replay *evidenceStructuralReplay
	if err == nil {
		replay, err = stream.finish()
	}
	if err != nil || replay == nil || records != 1 || tail != prior.headerFrame.RecordDigest || !canonicalEqual(first, prior.headerFrame) {
		t.Fatalf("historical successor header replay failed: records=%d tail=%s replay=%+v err=%v", records, tail, replay, err)
	}
	journalPlan := plan.journals[prior.journal]
	if journalPlan == nil || !journalPlan.active || !journalPlan.activated || journalPlan.checkpointNext != 0 || journalPlan.supersededOutcome != "" {
		t.Fatal("historical successor journal did not remain current and header-only")
	}
	if err := plan.acceptJournal(prior.journal, replay); err != nil {
		t.Fatal(err)
	}
	encodedTail := make([]byte, 0)
	for _, frame := range frames[len(frames)-3:] {
		encoded, encodeErr := EncodeCanonicalLineageFrame(frame)
		if encodeErr != nil {
			t.Fatal(encodeErr)
		}
		encodedTail = append(encodedTail, encoded...)
	}
	if !bytes.HasSuffix(raw, encodedTail) {
		t.Fatal("historical successor replay fixture lost exact adjacent byte suffix")
	}
}

func TestHistoricalSuccessorReplayRejectsEveryAdjacentBoundaryMutation(t *testing.T) {
	prior, raw, frames, _, fact := historicalSuccessorGenerationReplayFixture(t)

	wrongFact := fact
	wrongFact.ContentDigest[0] ^= 1
	if err := validateHistoricalSuccessorReplayIndex(prior, raw, frames, wrongFact); !IsCode(err, CodeEvidenceJournalCorrupt) {
		t.Fatalf("wrong historical successor file fact accepted: %v", err)
	}

	wrongPrior := *prior
	wrongPrior.reservedFrame = cloneProjectionValue(prior.reservedFrame)
	wrongPrevious := testDigest("wrong-historical-supersession")
	wrongPrior.reservedFrame.PreviousRecordDigest = &wrongPrevious
	if err := validateHistoricalSuccessorReplayIndex(&wrongPrior, raw, frames, fact); !IsCode(err, CodeEvidenceJournalCorrupt) {
		t.Fatalf("wrong supersession predecessor accepted: %v", err)
	}

	wrongPrior = *prior
	wrongPrior.authority = &VerifiedLineageSupersessionAuthority{digest: testDigest("wrong-historical-authority")}
	if err := validateHistoricalSuccessorReplayIndex(&wrongPrior, raw, frames, fact); !IsCode(err, CodeEvidenceJournalCorrupt) {
		t.Fatalf("wrong historical authority accepted: %v", err)
	}

	wrongFrames := cloneProjectionValue(frames)
	wrongFrames[len(wrongFrames)-2].Record.Reserved.RunnerProjectionDecisionDigest = testDigest("wrong-historical-planned-decision")
	if err := validateHistoricalSuccessorReplayIndex(prior, raw, wrongFrames, fact); !IsCode(err, CodeEvidenceJournalCorrupt) {
		t.Fatalf("wrong nested historical reservation accepted: %v", err)
	}

	wrongRaw := append([]byte(nil), raw...)
	wrongRaw[len(wrongRaw)-1] ^= 1
	wrongRawFact := fact
	wrongRawFact.ContentDigest = sha256.Sum256(wrongRaw)
	if err := validateHistoricalSuccessorReplayIndex(prior, wrongRaw, frames, wrongRawFact); !IsCode(err, CodeEvidenceJournalCorrupt) {
		t.Fatalf("wrong historical adjacent bytes accepted: %v", err)
	}
}

func TestHistoricalSuccessorHandoffAuthorityDoesNotSpread(t *testing.T) {
	const owner = "evidence_historical_supersession_handoff.go"
	allowed := map[string]map[string]bool{
		"evidence_generation_journal.go": {
			"HistoricalSuccessorGenerationReplayReady":    true,
			"historicalSuccessorGenerationHandoffDigest":  true,
			"historicalSuccessorGenerationReplayDigest":   true,
			"historicalSuccessorGenerationReplayReceipts": true,
		},
	}
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || name == owner || name == "evidence_historical_supersession_recovery.go" || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			identifier, ok := node.(*ast.Ident)
			if ok && (strings.HasPrefix(identifier.Name, "HistoricalSuccessorGenerationHandoff") ||
				strings.HasPrefix(identifier.Name, "HistoricalSuccessorGenerationReplay") ||
				strings.HasPrefix(identifier.Name, "historicalSuccessorGenerationHandoff") ||
				strings.HasPrefix(identifier.Name, "historicalSuccessorGenerationReplay") ||
				strings.HasPrefix(identifier.Name, "validHistoricalSuccessorGenerationHandoff") ||
				strings.HasPrefix(identifier.Name, "validConsumedHistoricalSuccessorGenerationHandoff") ||
				strings.HasPrefix(identifier.Name, "validHistoricalSuccessorGenerationReplay") ||
				strings.HasPrefix(identifier.Name, "validConsumedHistoricalSuccessorGenerationReplay")) && !allowed[name][identifier.Name] {
				t.Fatalf("historical successor handoff authority spread into %s through %s", name, identifier.Name)
			}
			return true
		})
	}
	file, err := parser.ParseFile(token.NewFileSet(), owner, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		if selector, ok := call.Fun.(*ast.SelectorExpr); ok && (selector.Sel.Name == "Connect" || selector.Sel.Name == "Open" || selector.Sel.Name == "BindJournal") {
			t.Fatalf("historical successor handoff called forbidden runtime entrypoint %s", selector.Sel.Name)
		}
		return true
	})
}

func historicalSuccessorGenerationReplayFixture(t *testing.T) (*HistoricalSuccessorGenerationReadyPermit, []byte, []LineageIndexFrame, []byte, evidencefs.GenerationFileFact) {
	t.Helper()
	live, raw, frames, headerRaw, fact := successorGenerationReplayFixture(t)
	if len(frames) < 3 {
		t.Fatal("successor fixture lacks adjacent supersession chain")
	}
	superseded := frames[len(frames)-3]
	reserved := cloneProjectionValue(frames[len(frames)-2])
	activated := cloneProjectionValue(frames[len(frames)-1])
	reservedRaw, err := EncodeCanonicalLineageFrame(reserved)
	if err != nil {
		t.Fatal(err)
	}
	activatedRaw, err := EncodeCanonicalLineageFrame(activated)
	if err != nil {
		t.Fatal(err)
	}
	authority := &VerifiedLineageSupersessionAuthority{digest: superseded.Record.Superseded.LineageSupersessionAuthorityDigest}
	prior := &HistoricalSuccessorGenerationReadyPermit{
		candidateBinding: live.candidateBinding, revision: live.revision, target: live.target,
		indexRecords: uint64(len(frames)), indexTail: activated.RecordDigest, indexDigest: sha256.Sum256(raw), indexSize: uint64(len(raw)),
		source:  &verifiedAdmissionRegisteredGeneration{replay: &verifiedAdmissionGenerationReplay{supersessionDebited: true}, canonical: [32]byte{1}},
		planned: &verifiedAdmissionRegisteredGeneration{canonical: [32]byte{2}}, authority: authority,
		receipt:       &verifiedHistoricalSupersessionReceipt{authorityDigest: authority.digest},
		reservedFrame: reserved, headerFrame: cloneProjectionValue(live.state.plan.headerFrame), activatedFrame: activated,
		reservedFrameBytes: reservedRaw, activatedBytes: activatedRaw, headerBytes: append([]byte(nil), headerRaw...),
		headerBytesHash: sha256.Sum256(headerRaw), journal: live.journal, journalCount: 1, consumed: &atomic.Bool{},
	}
	prior.self = prior
	prior.binding = &historicalSuccessorGenerationReadyBinding{permit: prior, candidateBinding: prior.candidateBinding, source: prior.source, planned: prior.planned, authority: prior.authority, receipt: prior.receipt, canonical: [32]byte{3}}
	return prior, raw, frames, headerRaw, fact
}
