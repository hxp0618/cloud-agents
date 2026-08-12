package migration

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"math"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestCanonicalFrameEncodersMatchEveryGoldenVector(t *testing.T) {
	t.Parallel()
	root := filepath.Join(migrationRoot(t), "fixtures", "projection", "golden")
	for _, fixture := range []struct {
		path    string
		lineage bool
	}{
		{"evidence-record-chain-v1.json", false},
		{"evidence-ambiguous-chain-v1.json", false},
		{"evidence-retry-chains-v1.json", false},
		{"lineage-index-chain-v1.json", true},
	} {
		t.Run(fixture.path, func(t *testing.T) {
			for index, raw := range collectFrameMembers(t, filepath.Join(root, fixture.path), fixture.lineage) {
				if fixture.lineage {
					var frame LineageIndexFrame
					decodeFixtureValue(t, raw, &frame)
					got, err := EncodeCanonicalLineageFrame(frame)
					if err != nil {
						t.Fatalf("frame %d: %v", index, err)
					}
					assertFramedSameBits(t, got, raw)
					continue
				}
				var frame EvidenceFrame
				decodeFixtureValue(t, raw, &frame)
				got, err := EncodeCanonicalEvidenceFrame(frame)
				if err != nil {
					t.Fatalf("frame %d: %v", index, err)
				}
				assertFramedSameBits(t, got, raw)
			}
		})
	}
}

func collectFrameMembers(t *testing.T, path string, lineage bool) []JSONValue {
	t.Helper()
	root := fixtureObject(t, path)
	frames := make([]JSONValue, 0)
	var visit func(JSONValue)
	visit = func(value JSONValue) {
		switch value := value.(type) {
		case []JSONValue:
			for _, member := range value {
				visit(member)
			}
		case map[string]JSONValue:
			format, _ := value["format_version"].(string)
			if format == EvidenceFrameFormat && !lineage || format == LineageFrameFormat && lineage {
				frames = append(frames, value)
				return
			}
			for _, member := range value {
				visit(member)
			}
		}
	}
	visit(root)
	if len(frames) == 0 {
		t.Fatal("fixture contains no frame vectors")
	}
	return frames
}

func assertFramedSameBits(t *testing.T, framed []byte, raw JSONValue) {
	t.Helper()
	canonical, err := CanonicalJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	want := lengthPrefix(canonical)
	if !bytes.Equal(framed, want) {
		t.Fatal("canonical frame encoder differs from checked-in vector")
	}
	if binary.BigEndian.Uint64(framed[:8]) != uint64(len(canonical)) {
		t.Fatal("length prefix is not uint64 big endian")
	}
}

func TestEvidenceFrameReplayRejectsEveryIncompleteBoundary(t *testing.T) {
	t.Parallel()
	raw := fixtureArrayMembers(t, migrationFixturePath(t, "golden/evidence-record-chain-v1.json"), "frames")[0]
	framed := lengthPrefix(raw)
	profile := evidenceReplayTestProfile()
	for boundary := 0; boundary < len(framed); boundary++ {
		t.Run(strconv.Itoa(boundary), func(t *testing.T) {
			reader := newEvidenceReplayForBytes(t, framed[:boundary], profile, true)
			got := reader.Next()
			want := FrameReplayCorrupt
			if boundary == 0 {
				want = FrameReplayCleanEOF
			}
			if got.State != want || got.BytesRead != uint64(boundary) {
				t.Fatalf("boundary %d: state=%d bytes=%d", boundary, got.State, got.BytesRead)
			}
		})
	}
	reader := newEvidenceReplayForBytes(t, framed, profile, true)
	if got := reader.Next(); got.State != FrameReplayValid || got.Frame == nil || got.BytesRead != uint64(len(framed)) {
		t.Fatalf("complete frame = %+v", got)
	}
	if got := reader.Next(); got.State != FrameReplayCleanEOF || got.StartOffset != uint64(len(framed)) {
		t.Fatalf("clean EOF = %+v", got)
	}
}

func TestLineageReplayIsTypedAndBounded(t *testing.T) {
	t.Parallel()
	raw := fixtureArrayMembers(t, migrationFixturePath(t, "golden/lineage-index-chain-v1.json"), "frames")[0]
	framed := lengthPrefix(raw)
	profile := lineageReplayTestProfile()
	profile.ContainerMaximumBytes = uint64(len(framed))
	profile.ContainerMaximumRecords = 1
	profile.GlobalMaximumBytes = uint64(len(framed))
	profile.GlobalMaximumRecords = 1
	reader := newLineageReplayForBytes(t, framed, profile)
	got := reader.Next()
	if got.State != FrameReplayValid || got.Frame == nil || got.Frame.RecordKind != LineageRecordHeader {
		t.Fatalf("lineage result = %+v", got)
	}
}

func TestReplayRejectsLimitBeforeAllocationAndChecksOverflow(t *testing.T) {
	t.Parallel()
	for _, declared := range []uint64{maxEvidenceFrameBytes - 8 + 1, math.MaxUint64} {
		prefix := make([]byte, 8)
		binary.BigEndian.PutUint64(prefix, declared)
		probe := &countingReader{data: prefix}
		reader, err := newEvidenceFrameReplayReader(newEvidenceVerifiedReplaySourceForTest(probe, evidenceSegmentContainer, "journal/segment-0", 0, 0, uint64(len(prefix))), evidenceReplayTestProfile())
		if err != nil {
			t.Fatal(err)
		}
		got := reader.Next()
		if got.State != FrameReplayLimitExceeded || !IsCode(got.Err, CodeEvidenceJournalLimitExceeded) || probe.read != 8 {
			t.Fatalf("declared=%d result=%+v read=%d", declared, got, probe.read)
		}
	}

	raw := fixtureArrayMembers(t, migrationFixturePath(t, "golden/evidence-record-chain-v1.json"), "frames")[0]
	framed := lengthPrefix(raw)
	for name, mutate := range map[string]func(*FrameReplayProfile){
		"container-bytes":   func(p *FrameReplayProfile) { p.ContainerBytes = p.ContainerMaximumBytes - uint64(len(framed)) + 1 },
		"container-records": func(p *FrameReplayProfile) { p.ContainerRecords = p.ContainerMaximumRecords },
		"global-bytes":      func(p *FrameReplayProfile) { p.GlobalBytes = p.GlobalMaximumBytes - uint64(len(framed)) + 1 },
		"global-records":    func(p *FrameReplayProfile) { p.GlobalRecords = p.GlobalMaximumRecords },
	} {
		t.Run(name, func(t *testing.T) {
			profile := evidenceReplayTestProfile()
			mutate(&profile)
			reader := newEvidenceReplayForBytes(t, framed, profile, true)
			got := reader.Next()
			if got.State != FrameReplayLimitExceeded || got.BytesRead != 8 {
				t.Fatalf("result=%+v", got)
			}
		})
	}
	if _, overflow := checkedFrameAdd(math.MaxUint64, 1); !overflow {
		t.Fatal("checked add missed overflow")
	}
}

func TestRawReplayFrameMaximumIsInclusive(t *testing.T) {
	t.Parallel()
	raw := fixtureArrayMembers(t, migrationFixturePath(t, "golden/evidence-record-chain-v1.json"), "frames")[0]
	framed := lengthPrefix(raw)
	profile := evidenceReplayTestProfile()
	exact, err := newFramedReplayReader(newEvidenceVerifiedReplaySourceForTest(bytes.NewReader(framed), evidenceSegmentContainer, "test", 0, 0, uint64(len(framed))), profile, uint64(len(framed)), evidenceSegmentContainer)
	if err != nil {
		t.Fatal(err)
	}
	if got := exact.next(); got.state != rawFrameValid {
		t.Fatalf("exact maximum = %+v", got)
	}
	over, err := newFramedReplayReader(newEvidenceVerifiedReplaySourceForTest(bytes.NewReader(framed), evidenceSegmentContainer, "test", 0, 0, uint64(len(framed))), profile, uint64(len(framed)-1), evidenceSegmentContainer)
	if err != nil {
		t.Fatal(err)
	}
	if got := over.next(); got.state != rawFrameLimitExceeded || got.bytesRead != 8 {
		t.Fatalf("maximum + 1 = %+v", got)
	}
}

func TestReplayClassifiesCompleteInvalidFramesAsCorrupt(t *testing.T) {
	t.Parallel()
	raw := fixtureArrayMembers(t, migrationFixturePath(t, "golden/evidence-record-chain-v1.json"), "frames")[0]
	tests := map[string][]byte{
		"whitespace":  append(append([]byte(nil), raw...), ' '),
		"key-order":   bytes.Replace(raw, []byte(`{"format_version":`), []byte(`{"sequence":0,"format_version":`), 1),
		"duplicate":   mustRead(t, migrationFixturePath(t, "negative/evidence-frame-duplicate.raw")),
		"self-digest": mutateEvidenceDigest(raw),
		"trailing":    append(append([]byte(nil), raw...), []byte(`{}`)...),
	}
	for name, payload := range tests {
		t.Run(name, func(t *testing.T) {
			reader := newEvidenceReplayForBytes(t, lengthPrefix(payload), evidenceReplayTestProfile(), true)
			got := reader.Next()
			if got.State != FrameReplayCorrupt || !IsCode(got.Err, CodeEvidenceJournalCorrupt) {
				t.Fatalf("result=%+v", got)
			}
		})
	}
}

func TestReplayClassifiesMiddleCorruptionAndIncompleteTailAsCorrupt(t *testing.T) {
	t.Parallel()
	raw := fixtureArrayMembers(t, migrationFixturePath(t, "golden/evidence-record-chain-v1.json"), "frames")
	first := lengthPrefix(raw[0])
	second := lengthPrefix(raw[1])
	corruptFirst := append([]byte(nil), first...)
	corruptFirst[len(corruptFirst)-1] ^= 1
	stream := append(corruptFirst, second...)
	reader := newEvidenceReplayForBytes(t, stream, evidenceReplayTestProfile(), true)
	if got := reader.Next(); got.State != FrameReplayCorrupt || got.BytesRead != uint64(len(first)) {
		t.Fatalf("middle corruption = %+v", got)
	}

	stream = append(append([]byte(nil), first...), second[:len(second)-1]...)
	reader = newEvidenceReplayForBytes(t, stream, evidenceReplayTestProfile(), true)
	if got := reader.Next(); got.State != FrameReplayValid {
		t.Fatalf("first frame = %+v", got)
	}
	if got := reader.Next(); got.State != FrameReplayCorrupt || got.StartOffset != uint64(len(first)) {
		t.Fatalf("incomplete tail = %+v", got)
	}
}

func TestIncompleteFramesNeverGainHealingAuthority(t *testing.T) {
	t.Parallel()
	raw := fixtureArrayMembers(t, migrationFixturePath(t, "golden/evidence-record-chain-v1.json"), "frames")[0]
	framed := lengthPrefix(raw)
	torn := framed[:len(framed)-1]
	for _, test := range []struct {
		name     string
		input    []byte
		observed uint64
		final    bool
		want     FrameReplayState
	}{
		{"final-exact", torn, uint64(len(torn)), true, FrameReplayCorrupt},
		{"nonfinal-same-bits", torn, uint64(len(torn)), false, FrameReplayCorrupt},
		{"size-short", torn, uint64(len(torn) - 1), true, FrameReplayCorrupt},
		{"size-long", torn, uint64(len(torn) + 1), true, FrameReplayCorrupt},
		{"trailing-beyond-inventory", append(append([]byte(nil), torn...), 'x'), uint64(len(torn)), true, FrameReplayCorrupt},
	} {
		t.Run(test.name, func(t *testing.T) {
			finalOrdinal := uint32(1)
			if test.final {
				finalOrdinal = 0
			}
			reader, err := newEvidenceFrameReplayReader(newEvidenceVerifiedReplaySourceForTest(bytes.NewReader(test.input), evidenceSegmentContainer, "journal/segment-0", 0, finalOrdinal, test.observed), evidenceReplayTestProfile())
			if err != nil {
				t.Fatal(err)
			}
			got := reader.Next()
			if got.State != test.want {
				t.Fatalf("result=%+v", got)
			}
			again := reader.Next()
			if again.State != got.State || again.StartOffset != got.StartOffset || again.BytesRead != got.BytesRead || again.Err != got.Err {
				t.Fatalf("terminal result was not stable: first=%+v again=%+v", got, again)
			}
		})
	}

	limited := io.LimitReader(bytes.NewReader(framed), int64(len(torn)))
	reader, err := newEvidenceFrameReplayReader(newEvidenceVerifiedReplaySourceForTest(limited, evidenceSegmentContainer, "journal/segment-0", 0, 0, uint64(len(framed))), evidenceReplayTestProfile())
	if err != nil {
		t.Fatal(err)
	}
	if got := reader.Next(); got.State != FrameReplayCorrupt {
		t.Fatalf("limited subreader created false final authority: %+v", got)
	}

	subSource := newEvidenceVerifiedReplaySourceForTest(bytes.NewReader(framed[1:]), evidenceSegmentContainer, "journal/segment-0", 0, 0, uint64(len(framed)))
	subSource.readBytes = 1
	if _, err = newEvidenceFrameReplayReader(subSource, evidenceReplayTestProfile()); !IsCode(err, CodeEvidenceJournalFailed) {
		t.Fatalf("offset subreader boundary was accepted: %v", err)
	}
}

func TestNonfinalCompleteContainerEndsWithCleanEOF(t *testing.T) {
	t.Parallel()
	raw := fixtureArrayMembers(t, migrationFixturePath(t, "golden/evidence-record-chain-v1.json"), "frames")[0]
	framed := lengthPrefix(raw)
	reader := newEvidenceReplayForBytes(t, framed, evidenceReplayTestProfile(), false)
	if got := reader.Next(); got.State != FrameReplayValid {
		t.Fatalf("complete nonfinal frame = %+v", got)
	}
	got := reader.Next()
	if got.State != FrameReplayCleanEOF || got.StartOffset != uint64(len(framed)) || got.BytesRead != 0 || got.Err != nil {
		t.Fatalf("complete nonfinal EOF = %+v", got)
	}
	if again := reader.Next(); again.State != got.State || again.StartOffset != got.StartOffset || again.BytesRead != got.BytesRead || again.Err != got.Err {
		t.Fatalf("nonfinal EOF terminal cache drifted: first=%+v again=%+v", got, again)
	}
}

func TestVerifiedReplaySourceCannotBeSwappedOrReused(t *testing.T) {
	t.Parallel()
	raw := fixtureArrayMembers(t, migrationFixturePath(t, "golden/evidence-record-chain-v1.json"), "frames")[0]
	framed := lengthPrefix(raw)
	source := newEvidenceVerifiedReplaySourceForTest(bytes.NewReader(framed), evidenceSegmentContainer, "journal/segment-0", 0, 0, uint64(len(framed)))
	if _, err := newLineageFrameReplayReader(source, lineageReplayTestProfile()); !IsCode(err, CodeEvidenceJournalFailed) {
		t.Fatalf("source kind swap accepted: %v", err)
	}
	source = newEvidenceVerifiedReplaySourceForTest(bytes.NewReader(framed), evidenceSegmentContainer, "journal/segment-0", 0, 0, uint64(len(framed)))
	if _, err := newEvidenceFrameReplayReader(source, evidenceReplayTestProfile()); err != nil {
		t.Fatal(err)
	}
	if _, err := newEvidenceFrameReplayReader(source, evidenceReplayTestProfile()); !IsCode(err, CodeEvidenceJournalFailed) {
		t.Fatalf("source boundary reuse accepted: %v", err)
	}
}

func TestTypedReplayReaderOwnsSourceUntilExplicitClose(t *testing.T) {
	t.Parallel()
	raw := fixtureArrayMembers(t, migrationFixturePath(t, "golden/evidence-record-chain-v1.json"), "frames")[0]
	framed := lengthPrefix(raw)
	source := newEvidenceVerifiedReplaySourceForTest(bytes.NewReader(framed), evidenceSegmentContainer, "journal/segment-0", 0, 0, uint64(len(framed)))
	reader, err := newEvidenceFrameReplayReader(source, evidenceReplayTestProfile())
	if err != nil {
		t.Fatal(err)
	}
	if source.closed {
		t.Fatal("constructor closed the successfully transferred source")
	}
	if got := reader.Next(); got.State != FrameReplayValid {
		t.Fatalf("result=%+v", got)
	}
	if source.closed {
		t.Fatal("terminal/valid replay auto-closed the source")
	}
	if err := reader.Close(); err != nil || !source.closed {
		t.Fatalf("close err=%v source.closed=%v", err, source.closed)
	}
	if err := reader.Close(); !IsCode(err, CodeEvidenceJournalFailed) {
		t.Fatalf("double close=%v", err)
	}
	if got := reader.Next(); got.State != FrameReplayJournalFailed || !IsCode(got.Err, CodeEvidenceJournalFailed) {
		t.Fatalf("next after close=%+v", got)
	}
	var nilReader *EvidenceFrameReplayReader
	if err := nilReader.Close(); !IsCode(err, CodeEvidenceJournalFailed) {
		t.Fatalf("nil close=%v", err)
	}
}

func TestTypedReplayReaderConstructorFailureClosesSource(t *testing.T) {
	t.Parallel()
	badProfile := evidenceReplayTestProfile()
	badProfile.ContainerMaximumBytes = 0
	source := newEvidenceVerifiedReplaySourceForTest(bytes.NewReader(nil), evidenceSegmentContainer, "journal/segment-0", 0, 0, 0)
	if _, err := newEvidenceFrameReplayReader(source, badProfile); !IsCode(err, CodeEvidenceJournalLimitExceeded) || !source.closed {
		t.Fatalf("constructor err=%v source.closed=%v", err, source.closed)
	}

	source = newEvidenceVerifiedReplaySourceForTest(bytes.NewReader(nil), lineageIndexContainer, "lineage/index", 0, 0, 0)
	if _, err := newEvidenceFrameReplayReader(source, evidenceReplayTestProfile()); !IsCode(err, CodeEvidenceJournalFailed) || !source.closed {
		t.Fatalf("kind mismatch err=%v source.closed=%v", err, source.closed)
	}
}

func TestInventoryProductionChainReplayReaderClosesOwnedFD(t *testing.T) {
	t.Parallel()
	raw := fixtureArrayMembers(t, migrationFixturePath(t, "golden/evidence-record-chain-v1.json"), "frames")[0]
	framed := lengthPrefix(raw)
	f := newFakeEvidenceFS()
	f.directoryNames[2] = []string{"segment-00000000.caj"}
	f.files[2] = map[string]bool{"segment-00000000.caj": true}
	f.stats[2] = fakeDirectoryStat(7)
	f.nextFD = 100
	st := fakeRegularStat(7)
	st.size = uint64(len(framed))
	f.stats[100] = st
	root := &evidenceFSRoot{ops: f, fd: 1, uid: 501, device: 7}
	inventory, err := root.inventoryEvidenceSegments(2, "journal")
	if err != nil {
		t.Fatal(err)
	}
	entry := inventory.entries[0]
	openedFD := f.nextFD
	f.stats[openedFD] = evidenceFileStat{device: entry.device, inode: entry.inode, size: entry.observedSize, mode: 0o600, uid: 501, nlink: 1, kind: evidenceFileRegular}
	f.readers[openedFD] = strings.NewReader(string(framed))
	reader, err := inventory.OpenEvidenceReplayReader(0, evidenceReplayTestProfile())
	if err != nil {
		t.Fatal(err)
	}
	if got := reader.Next(); got.State != FrameReplayValid {
		t.Fatalf("result=%+v", got)
	}
	if f.closed[openedFD] {
		t.Fatal("production chain auto-closed the source before explicit close")
	}
	if err := reader.Close(); err != nil || !f.closed[openedFD] {
		t.Fatalf("close err=%v fd closed=%v", err, f.closed[openedFD])
	}
}

func TestTypedReplayReaderPreservesConstructorErrorWhenCloseFails(t *testing.T) {
	t.Parallel()
	primary := frameIOLimit("test-profile", "invalid")
	source := newCloseFaultReplaySource(t, errors.New("close failed"))
	err := closeReplaySourceAfterConstructorFailure(source, primary)
	if !IsCode(err, CodeEvidenceJournalLimitExceeded) || !source.closed || !strings.Contains(err.Error(), "validation and close failed") {
		t.Fatalf("combined error=%v source.closed=%v", err, source.closed)
	}
}

func TestTypedReplayReaderCloseFailureIsOneShot(t *testing.T) {
	t.Parallel()
	source := newCloseFaultReplaySource(t, errors.New("close failed"))
	reader, err := newEvidenceFrameReplayReader(source, evidenceReplayTestProfile())
	if err != nil {
		t.Fatal(err)
	}
	if err := reader.Close(); !IsCode(err, CodeEvidenceJournalFailed) || !source.closed {
		t.Fatalf("close error=%v source.closed=%v", err, source.closed)
	}
	if err := reader.Close(); !IsCode(err, CodeEvidenceJournalFailed) {
		t.Fatalf("double close=%v", err)
	}
	if got := reader.Next(); got.State != FrameReplayJournalFailed || !IsCode(got.Err, CodeEvidenceJournalFailed) {
		t.Fatalf("next after failed close=%+v", got)
	}
}

func newCloseFaultReplaySource(t *testing.T, closeErr error) *evidenceVerifiedReplaySource {
	t.Helper()
	source := newEvidenceVerifiedReplaySourceForTest(bytes.NewReader(nil), evidenceSegmentContainer, "journal/segment-0", 0, 0, 0)
	source.reader = nil
	source.ops = &closeErrorEvidenceOps{err: closeErr}
	source.fd = 9
	return source
}

func mutateEvidenceDigest(raw []byte) []byte {
	mutated := append([]byte(nil), raw...)
	needle := []byte(`"record_digest":"sha256:`)
	index := bytes.Index(mutated, needle)
	if index < 0 {
		return mutated
	}
	index += len(needle)
	if mutated[index] == '0' {
		mutated[index] = '1'
	} else {
		mutated[index] = '0'
	}
	return mutated
}

func TestReplayClassifiesReaderFaults(t *testing.T) {
	t.Parallel()
	raw := fixtureArrayMembers(t, migrationFixturePath(t, "golden/evidence-record-chain-v1.json"), "frames")[0]
	framed := lengthPrefix(raw)
	failure := errors.New("read failed")
	for _, test := range []struct {
		name  string
		input io.Reader
		state FrameReplayState
	}{
		{"partial", &chunkReader{data: framed, maximum: 3}, FrameReplayValid},
		{"zero-prefix", &zeroReader{}, FrameReplayJournalFailed},
		{"error-after-prefix-bytes", &scriptedReader{steps: []readStep{{data: framed[:3], err: failure}}}, FrameReplayJournalFailed},
		{"error-after-payload-bytes", &scriptedReader{steps: []readStep{{data: framed[:8]}, {data: framed[8:19], err: failure}}}, FrameReplayJournalFailed},
		{"eof-with-complete-prefix", &scriptedReader{steps: []readStep{{data: framed[:8], err: io.EOF}, {data: framed[8:]}}}, FrameReplayValid},
		{"eof-with-complete-payload", &scriptedReader{steps: []readStep{{data: framed[:8]}, {data: framed[8:], err: io.EOF}}}, FrameReplayValid},
	} {
		t.Run(test.name, func(t *testing.T) {
			reader, err := newEvidenceFrameReplayReader(newEvidenceVerifiedReplaySourceForTest(test.input, evidenceSegmentContainer, "journal/segment-0", 0, 0, uint64(len(framed))), evidenceReplayTestProfile())
			if err != nil {
				t.Fatal(err)
			}
			got := reader.Next()
			if got.State != test.state {
				t.Fatalf("state=%d err=%v", got.State, got.Err)
			}
			if got.State == FrameReplayJournalFailed && !IsCode(got.Err, CodeEvidenceJournalFailed) {
				t.Fatalf("error=%v", got.Err)
			}
		})
	}
}

func TestWriteCanonicalFrameHandlesShortZeroAndError(t *testing.T) {
	t.Parallel()
	raw := fixtureArrayMembers(t, migrationFixturePath(t, "golden/evidence-record-chain-v1.json"), "frames")[0]
	var frame EvidenceFrame
	decodeFixtureValue(t, fixtureObjectValueFromRaw(t, raw), &frame)
	want := lengthPrefix(raw)

	partial := &shortWriter{maximum: 7}
	n, err := WriteCanonicalEvidenceFrame(partial, frame)
	if err != nil || n != len(want) || !bytes.Equal(partial.data, want) {
		t.Fatalf("partial writer n=%d err=%v", n, err)
	}
	for _, writer := range []io.Writer{&zeroWriter{}, &errorWriter{maximum: 11, err: errors.New("write failed")}} {
		n, err = WriteCanonicalEvidenceFrame(writer, frame)
		if err == nil || !IsCode(err, CodeEvidenceJournalFailed) || n < 0 || n >= len(want) {
			t.Fatalf("writer=%T n=%d err=%v", writer, n, err)
		}
	}
}

func fixtureObjectValueFromRaw(t *testing.T, raw []byte) map[string]JSONValue {
	t.Helper()
	value, err := ParseStrictJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	return fixtureObjectValue(t, value, "raw fixture")
}

func evidenceReplayTestProfile() FrameReplayProfile {
	return FrameReplayProfile{ContainerMaximumBytes: 16 << 20, ContainerMaximumRecords: 4096, GlobalMaximumBytes: 4 << 30, GlobalMaximumRecords: maxJSONInteger}
}

func lineageReplayTestProfile() FrameReplayProfile {
	return FrameReplayProfile{ContainerMaximumBytes: 16 << 20, ContainerMaximumRecords: 16384, GlobalMaximumBytes: 4 << 30, GlobalMaximumRecords: maxJSONInteger}
}

func newEvidenceReplayForBytes(t *testing.T, data []byte, profile FrameReplayProfile, final bool) *EvidenceFrameReplayReader {
	t.Helper()
	finalOrdinal := uint32(1)
	if final {
		finalOrdinal = 0
	}
	reader, err := newEvidenceFrameReplayReader(newEvidenceVerifiedReplaySourceForTest(bytes.NewReader(data), evidenceSegmentContainer, "journal/segment-0", 0, finalOrdinal, uint64(len(data))), profile)
	if err != nil {
		t.Fatal(err)
	}
	return reader
}

func newLineageReplayForBytes(t *testing.T, data []byte, profile FrameReplayProfile) *LineageFrameReplayReader {
	t.Helper()
	reader, err := newLineageFrameReplayReader(newEvidenceVerifiedReplaySourceForTest(bytes.NewReader(data), lineageIndexContainer, "lineage/index", 0, 0, uint64(len(data))), profile)
	if err != nil {
		t.Fatal(err)
	}
	return reader
}

type countingReader struct {
	data []byte
	read int
}

func (reader *countingReader) Read(destination []byte) (int, error) {
	n := copy(destination, reader.data[reader.read:])
	reader.read += n
	if reader.read == len(reader.data) {
		return n, io.EOF
	}
	return n, nil
}

type chunkReader struct {
	data    []byte
	offset  int
	maximum int
}

func (reader *chunkReader) Read(destination []byte) (int, error) {
	if reader.offset == len(reader.data) {
		return 0, io.EOF
	}
	maximum := reader.maximum
	if maximum > len(destination) {
		maximum = len(destination)
	}
	if maximum > len(reader.data)-reader.offset {
		maximum = len(reader.data) - reader.offset
	}
	n := copy(destination, reader.data[reader.offset:reader.offset+maximum])
	reader.offset += n
	return n, nil
}

type zeroReader struct{}

func (*zeroReader) Read([]byte) (int, error) { return 0, nil }

type readStep struct {
	data []byte
	err  error
}

type scriptedReader struct{ steps []readStep }

func (reader *scriptedReader) Read(destination []byte) (int, error) {
	if len(reader.steps) == 0 {
		return 0, io.EOF
	}
	step := reader.steps[0]
	reader.steps = reader.steps[1:]
	return copy(destination, step.data), step.err
}

type shortWriter struct {
	maximum int
	data    []byte
}

func (writer *shortWriter) Write(data []byte) (int, error) {
	n := writer.maximum
	if n > len(data) {
		n = len(data)
	}
	writer.data = append(writer.data, data[:n]...)
	return n, nil
}

type zeroWriter struct{}

func (*zeroWriter) Write([]byte) (int, error) { return 0, nil }

type errorWriter struct {
	maximum int
	err     error
}

type closeErrorEvidenceOps struct {
	evidenceFSOps
	err error
}

func (ops *closeErrorEvidenceOps) close(int) error { return ops.err }

func (writer *errorWriter) Write(data []byte) (int, error) {
	n := writer.maximum
	if n > len(data) {
		n = len(data)
	}
	return n, writer.err
}

func FuzzEvidenceFrameStreamingReader(f *testing.F) {
	f.Add([]byte{})
	f.Add(make([]byte, 8))
	f.Add([]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff})
	f.Fuzz(func(t *testing.T, data []byte) {
		reader, err := newEvidenceFrameReplayReader(newEvidenceVerifiedReplaySourceForTest(bytes.NewReader(data), evidenceSegmentContainer, "fuzz/evidence", 0, 0, uint64(len(data))), evidenceReplayTestProfile())
		if err != nil {
			t.Fatal(err)
		}
		for records := uint64(0); records < 4097; records++ {
			result := reader.Next()
			if result.State != FrameReplayValid {
				return
			}
		}
		t.Fatal("reader exceeded bounded record count")
	})
}

func FuzzLineageFrameStreamingReader(f *testing.F) {
	f.Add([]byte{})
	f.Add(make([]byte, 8))
	f.Fuzz(func(t *testing.T, data []byte) {
		profile := lineageReplayTestProfile()
		profile.ContainerMaximumRecords = 64
		reader, err := newLineageFrameReplayReader(newEvidenceVerifiedReplaySourceForTest(bytes.NewReader(data), lineageIndexContainer, "fuzz/lineage", 0, 0, uint64(len(data))), profile)
		if err != nil {
			t.Fatal(err)
		}
		for records := uint64(0); records < 65; records++ {
			result := reader.Next()
			if result.State != FrameReplayValid {
				return
			}
		}
		t.Fatal("reader exceeded bounded record count")
	})
}
