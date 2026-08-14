package migration

import (
	"bytes"
	"context"
	"crypto/sha256"
	"sync/atomic"
	"testing"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/evidencefs"
)

func TestSuccessorGenerationHandoffAndReplayRejectLiterals(t *testing.T) {
	candidate := quotaCandidateForBundle(t, quotaAdmissionBundleForTest(t), []byte("successor-handoff-literal"))
	result, err := (&SuccessorGenerationReadyPermit{}).Handoff(context.Background(), candidate)
	if result.Next() != nil || result.Outcome() != evidencefs.AdmissionTransitionPreMutationFailure || result.CandidateKind() != "generation_handoff" || result.CandidateSequence() != 10 || !IsCode(err, CodeEvidenceRecoveryRequired) {
		t.Fatalf("literal successor generation entered handoff: result=%+v err=%v", result, err)
	}
	if validSuccessorGenerationHandoffReady(&SuccessorGenerationHandoffReady{}, candidate) {
		t.Fatal("literal successor handoff passed validation")
	}
	replay, err := (&SuccessorGenerationHandoffReady{}).Replay(context.Background(), candidate)
	if replay.Next() != nil || !IsCode(err, CodeEvidenceRecoveryRequired) {
		t.Fatalf("literal successor handoff entered replay: result=%+v err=%v", replay, err)
	}
	if validSuccessorGenerationReplayReady(&SuccessorGenerationReplayReady{}, candidate) {
		t.Fatal("literal successor replay passed validation")
	}
	if err := (&SuccessorGenerationHandoffReady{}).Close(); !IsCode(err, CodeEvidenceJournalFailed) {
		t.Fatalf("literal successor handoff close=%v", err)
	}
	if err := (&SuccessorGenerationReplayReady{}).Close(); !IsCode(err, CodeEvidenceJournalFailed) {
		t.Fatalf("literal successor replay close=%v", err)
	}
}

func TestSuccessorGenerationHandoffAndReplayAreNotRuntimeAuthority(t *testing.T) {
	for name, value := range map[string]any{
		"handoff": &SuccessorGenerationHandoffReady{},
		"replay":  &SuccessorGenerationReplayReady{},
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

func TestSuccessorGenerationHandoffDigestsRejectCopyAndMutation(t *testing.T) {
	candidate := quotaCandidateForBundle(t, quotaAdmissionBundleForTest(t), []byte("successor-handoff-digest"))
	state := &successorAdmissionState{
		target: [32]byte{1}, journal: testDigest("successor-handoff-journal"), revision: 10,
		binding: &successorAdmissionStateBinding{canonical: [32]byte{2}},
	}
	prior := &SuccessorGenerationReadyPermit{state: state}
	prior.self = prior
	if successorGenerationHandoffCandidateDigest(prior) == ([32]byte{}) {
		t.Fatal("successor handoff candidate digest is empty")
	}
	ready := &SuccessorGenerationHandoffReady{
		prior: prior, state: state, candidateBinding: candidate.binding, lease: &evidencefs.GenerationLease{},
		target: state.target, journal: state.journal, revision: state.revision, consumed: &atomic.Bool{},
	}
	ready.self = ready
	want := successorGenerationHandoffDigest(ready)
	if want == ([32]byte{}) {
		t.Fatal("successor handoff digest is empty")
	}
	copyReady := *ready
	if successorGenerationHandoffDigest(&copyReady) != ([32]byte{}) {
		t.Fatal("copied successor handoff retained digest authority")
	}
	for name, mutate := range map[string]func(*SuccessorGenerationHandoffReady){
		"target":   func(value *SuccessorGenerationHandoffReady) { value.target[0]++ },
		"journal":  func(value *SuccessorGenerationHandoffReady) { value.journal = testDigest("other-journal") },
		"revision": func(value *SuccessorGenerationHandoffReady) { value.revision++ },
	} {
		t.Run(name, func(t *testing.T) {
			value := *ready
			value.self = &value
			mutate(&value)
			if successorGenerationHandoffDigest(&value) == want {
				t.Fatal("successor handoff mutation did not change digest")
			}
		})
	}
}

func TestSuccessorReplayStrictlyBindsAdjacentIndexAndHeaderOnlyJournal(t *testing.T) {
	ready, raw, frames, headerRaw, fact := successorGenerationReplayFixture(t)
	if err := ready.validateSuccessorReplayIndex(raw, frames, fact); err != nil {
		t.Fatal(err)
	}
	plan, err := scanLineageChainStructure(frames)
	if err != nil {
		t.Fatal(err)
	}
	stream, registered := openEvidenceJournalStructuralStream(plan, ready.journal, nil)
	if !registered || stream == nil {
		t.Fatal("successor journal was not registered by strict index replay")
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
	if err != nil || replay == nil || records != 1 || tail != ready.state.headerDigest || !canonicalEqual(first, ready.state.plan.headerFrame) {
		t.Fatalf("successor header replay failed: records=%d tail=%s replay=%+v err=%v", records, tail, replay, err)
	}
	journalPlan := plan.journals[ready.journal]
	if journalPlan == nil || !journalPlan.active || !journalPlan.activated || journalPlan.checkpointNext != 0 || journalPlan.supersededOutcome != "" {
		t.Fatal("successor journal did not remain the current activated generation")
	}
	if err := plan.acceptJournal(ready.journal, replay); err != nil {
		t.Fatal(err)
	}
}

func TestSuccessorReplayRejectsEveryAdjacentBoundaryMutation(t *testing.T) {
	ready, raw, frames, _, fact := successorGenerationReplayFixture(t)

	wrongFact := fact
	wrongFact.ContentDigest[0] ^= 1
	if err := ready.validateSuccessorReplayIndex(raw, frames, wrongFact); !IsCode(err, CodeEvidenceJournalCorrupt) {
		t.Fatalf("wrong file fact accepted: %v", err)
	}

	wrongFrames := cloneProjectionValue(frames)
	wrongFrames[len(wrongFrames)-2].RecordDigest = testDigest("wrong-successor-reservation")
	if err := ready.validateSuccessorReplayIndex(raw, wrongFrames, fact); !IsCode(err, CodeEvidenceJournalCorrupt) {
		t.Fatalf("wrong reservation accepted: %v", err)
	}

	wrongRaw := append([]byte(nil), raw...)
	wrongRaw[len(wrongRaw)-1] ^= 1
	wrongRawFact := fact
	wrongRawFact.ContentDigest = sha256.Sum256(wrongRaw)
	if err := ready.validateSuccessorReplayIndex(wrongRaw, frames, wrongRawFact); !IsCode(err, CodeEvidenceJournalCorrupt) {
		t.Fatalf("wrong adjacent bytes accepted: %v", err)
	}

	wrongState := *ready.state
	wrongState.indexPrefixDigest[0] ^= 1
	wrongReady := *ready
	wrongReady.self = &wrongReady
	wrongReady.state = &wrongState
	if err := wrongReady.validateSuccessorReplayIndex(raw, frames, fact); !IsCode(err, CodeEvidenceJournalCorrupt) {
		t.Fatalf("wrong durable prefix accepted: %v", err)
	}
}

func TestSuccessorGenerationReplayDigestBindsSnapshotFacts(t *testing.T) {
	ready, _, frames, _, fact := successorGenerationReplayFixture(t)
	replay := &SuccessorGenerationReplayReady{
		prior: ready, state: ready.state, candidateBinding: ready.candidateBinding,
		lease: ready.lease, snapshot: &evidencefs.GenerationSnapshot{}, target: ready.target, journal: ready.journal,
		revision: ready.revision, snapshotIdentity: [32]byte{9}, indexFact: fact,
		segmentFact:  evidencefs.GenerationFileFact{Ordinal: 0, Size: 23, ContentDigest: [32]byte{7}, IdentityDigest: [32]byte{8}},
		indexRecords: uint64(len(frames)), segmentCount: 1, journalRecords: 1,
		journalTail: ready.state.headerDigest, consumed: &atomic.Bool{},
	}
	replay.self = replay
	want := successorGenerationReplayDigest(replay)
	if want == ([32]byte{}) {
		t.Fatal("successor replay digest is empty")
	}
	copyReplay := *replay
	if successorGenerationReplayDigest(&copyReplay) != ([32]byte{}) {
		t.Fatal("copied successor replay retained digest authority")
	}
	for name, mutate := range map[string]func(*SuccessorGenerationReplayReady){
		"snapshot": func(value *SuccessorGenerationReplayReady) { value.snapshotIdentity[0]++ },
		"index":    func(value *SuccessorGenerationReplayReady) { value.indexFact.IdentityDigest[0]++ },
		"segment":  func(value *SuccessorGenerationReplayReady) { value.segmentFact.ContentDigest[0]++ },
		"records":  func(value *SuccessorGenerationReplayReady) { value.indexRecords++ },
		"tail":     func(value *SuccessorGenerationReplayReady) { value.journalTail = testDigest("other-tail") },
	} {
		t.Run(name, func(t *testing.T) {
			value := *replay
			value.self = &value
			mutate(&value)
			if successorGenerationReplayDigest(&value) == want {
				t.Fatal("successor replay mutation did not change digest")
			}
		})
	}
}

func successorGenerationReplayFixture(t *testing.T) (*SuccessorGenerationHandoffReady, []byte, []LineageIndexFrame, []byte, evidencefs.GenerationFileFact) {
	t.Helper()
	fixture := fixtureObject(t, migrationFixturePath(t, "golden/lineage-index-chain-v1.json"))
	base := decodeLineageFrames(t, fixture["frames"])
	if len(base) < 4 || base[0].Record.Header == nil || base[1].Record.Reserved == nil || base[2].Record.Activated == nil || base[3].Record.Checkpoint == nil || base[3].Record.Checkpoint.LastTerminalDigest == nil {
		t.Fatal("golden lineage fixture lacks successor replay facts")
	}
	frames := cloneProjectionValue(base[:4])
	oldReserved := frames[1].Record.Reserved
	oldActivated := frames[2].Record.Activated
	checkpoint := frames[3].Record.Checkpoint
	planned := cloneProjectionValue(*oldReserved)
	plannedHeader := cloneProjectionValue(planned.PlannedSegment0Header)
	plannedHeader.OuterArtifactDigest = testDigest("successor-replay-runtime")
	plannedHeader.OuterArtifactSizeBytes = 128
	plannedHeader.DecisionRecoveryArtifactSHA256 = testDigest("successor-replay-recovery")
	plannedHeader.DecisionRecoveryArtifactSizeBytes = 64
	planned.RunnerProjectionDecisionDigest = testDigest("successor-replay-decision")
	planned.SchemaBundleDigest = testDigest("successor-replay-schema")
	plannedHeader.RunnerProjectionDecisionDigest = planned.RunnerProjectionDecisionDigest
	plannedHeader.SchemaBundleDigest = planned.SchemaBundleDigest
	journal, err := JournalIdentityDigest(plannedHeader)
	if err != nil {
		t.Fatal(err)
	}
	planned.JournalIdentityDigest = journal
	plannedHeader.JournalIdentityDigest = journal
	planned.Continuation = &LineageContinuationContext{
		StartAction: "begin_first_attempt_next_entry", MigrationID: "000002", AttemptIndex: 1,
		SourceJournalIdentityDigest: oldActivated.JournalIdentityDigest, SourceCheckpointRecordDigest: frames[3].RecordDigest,
		SourceTerminalDigest: *checkpoint.LastTerminalDigest,
	}
	planned.QuotaReservationDigest, err = QuotaReservationDigest(planned)
	if err != nil {
		t.Fatal(err)
	}
	plannedHeader.QuotaReservationDigest = planned.QuotaReservationDigest
	planned.PlannedSegment0Header = plannedHeader
	header := EvidenceFrame{
		FormatVersion: EvidenceFrameFormat, Sequence: 0, RecordKind: EvidenceRecordHeader,
		Record: EvidenceRecord{Header: &planned.PlannedSegment0Header},
	}
	header.RecordDigest, err = header.ComputeDigest()
	if err != nil {
		t.Fatal(err)
	}
	planned.ExpectedSegment0HeaderDigest = header.RecordDigest
	supersededBody := GenerationSuperseded{
		ExecutionLineageDigest: oldActivated.ExecutionLineageDigest, OldJournalIdentityDigest: oldActivated.JournalIdentityDigest,
		OldRunnerProjectionDecisionDigest: oldActivated.RunnerProjectionDecisionDigest, OldSchemaBundleDigest: oldActivated.SchemaBundleDigest,
		OldCheckpointRecordDigest: digestPointer(frames[3].RecordDigest), LineageSupersessionAuthorityDigest: testDigest("successor-replay-authority"),
		Outcome: "exact_committed_continue_successor", PlannedGenerationReserved: &planned,
	}
	previous := frames[len(frames)-1].RecordDigest
	superseded := LineageIndexFrame{
		FormatVersion: LineageFrameFormat, Sequence: uint64(len(frames)), PreviousRecordDigest: &previous,
		RecordKind: LineageRecordGenerationSuperseded, Record: LineageIndexRecord{Superseded: &supersededBody},
	}
	superseded.RecordDigest, err = superseded.ComputeDigest()
	if err != nil || superseded.Validate() != nil {
		t.Fatalf("successor superseded fixture is invalid: %v", err)
	}
	reservedPrevious := superseded.RecordDigest
	reserved := LineageIndexFrame{
		FormatVersion: LineageFrameFormat, Sequence: superseded.Sequence + 1, PreviousRecordDigest: &reservedPrevious,
		RecordKind: LineageRecordGenerationReserved, Record: LineageIndexRecord{Reserved: &planned},
	}
	reserved.RecordDigest, err = reserved.ComputeDigest()
	if err != nil || reserved.Validate() != nil {
		t.Fatalf("successor reservation fixture is invalid: %v", err)
	}
	activated, activatedRaw, err := buildSuccessorActivatedFrame(reserved, header)
	if err != nil {
		t.Fatal(err)
	}
	frames = append(frames, superseded, reserved, activated)
	if _, err := scanLineageChainStructure(frames); err != nil {
		t.Fatalf("successor replay index fixture is structurally invalid: %v", err)
	}
	supersededRaw, err := EncodeCanonicalLineageFrame(superseded)
	if err != nil {
		t.Fatal(err)
	}
	reservedRaw, err := EncodeCanonicalLineageFrame(reserved)
	if err != nil {
		t.Fatal(err)
	}
	var raw []byte
	for _, frame := range frames {
		framed, encodeErr := EncodeCanonicalLineageFrame(frame)
		if encodeErr != nil {
			t.Fatal(encodeErr)
		}
		raw = append(raw, framed...)
	}
	headerRaw, err := EncodeCanonicalEvidenceFrame(header)
	if err != nil {
		t.Fatal(err)
	}
	history := &VerifiedAdmissionHistory{targetIndexRecords: 4}
	plan := &VerifiedSuccessorAdmissionPlan{
		history: history, supersededFrame: superseded, reservedFrame: reserved, headerFrame: header,
		supersededFrameBytes: supersededRaw, reservedFrameBytes: reservedRaw,
	}
	prefixSize := uint64(len(raw) - len(activatedRaw))
	state := &successorAdmissionState{
		plan: plan, history: history, target: digestRaw(oldActivated.ExecutionLineageDigest), journal: journal,
		revision: 10, indexPrefixDigest: sha256.Sum256(raw[:prefixSize]), indexDigest: sha256.Sum256(raw),
		indexPrefixSize: prefixSize, indexSize: uint64(len(raw)), indexRecords: uint64(len(frames)),
		indexTail: activated.RecordDigest, supersededDigest: superseded.RecordDigest, reservedDigest: reserved.RecordDigest,
		headerDigest: header.RecordDigest, headerBytes: append([]byte(nil), headerRaw...), activatedFrame: activated,
		activatedBytes: append([]byte(nil), activatedRaw...), activationDigest: activated.RecordDigest,
		binding: &successorAdmissionStateBinding{canonical: [32]byte{3}},
	}
	candidateBinding := &verifiedEvidenceRunBinding{canonical: [32]byte{4}}
	prior := &SuccessorGenerationReadyPermit{state: state}
	prior.self = prior
	ready := &SuccessorGenerationHandoffReady{
		prior: prior, state: state, candidateBinding: candidateBinding, lease: &evidencefs.GenerationLease{},
		target: state.target, journal: state.journal, revision: state.revision, consumed: &atomic.Bool{},
	}
	ready.self = ready
	ready.binding = &successorGenerationHandoffBinding{
		ready: ready, prior: prior, state: state, candidateBinding: candidateBinding, lease: ready.lease, canonical: [32]byte{5},
	}
	fact := evidencefs.GenerationFileFact{Ordinal: 0, Size: uint64(len(raw)), ContentDigest: sha256.Sum256(raw), IdentityDigest: [32]byte{6}}
	return ready, raw, frames, headerRaw, fact
}

func TestSuccessorReplayFixtureUsesExactByteSuffix(t *testing.T) {
	ready, raw, _, _, _ := successorGenerationReplayFixture(t)
	want := append(append(append([]byte(nil), ready.state.plan.supersededFrameBytes...), ready.state.plan.reservedFrameBytes...), ready.state.activatedBytes...)
	if !bytes.HasSuffix(raw, want) {
		t.Fatal("successor replay fixture lost exact adjacent byte suffix")
	}
}
