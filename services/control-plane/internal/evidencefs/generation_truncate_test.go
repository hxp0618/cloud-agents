package evidencefs

import (
	"bytes"
	"context"
	"errors"
	"testing"
)

func TestGenerationTailRepairDurableExactPrefixes(t *testing.T) {
	for _, test := range []struct {
		name           string
		segment, index bool
		wantOperations []string
	}{
		{name: "segment", segment: true, wantOperations: []string{admissionSegmentName(1)}},
		{name: "index", index: true, wantOperations: []string{"index.caj"}},
		{name: "both", segment: true, index: true, wantOperations: []string{admissionSegmentName(1), "index.caj"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			f, _, lease, target, journal := generationLeaseForSnapshot(t, [][]byte{[]byte("header"), []byte("last")})
			indexNode, segmentNode := generationAppendNodes(f, target, journal, 1)
			indexPrefix, segmentPrefix := append([]byte(nil), indexNode.data...), append([]byte(nil), segmentNode.data...)
			if test.segment {
				appendGenerationTailForTest(segmentNode, []byte("segment-torn"))
			}
			if test.index {
				appendGenerationTailForTest(indexNode, []byte("index-torn"))
			}
			snapshot, err := lease.Snapshot(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			previousIdentity, _ := snapshot.IdentityDigest()
			segmentSize, indexSize := segmentNode.stat.size, indexNode.stat.size
			if test.segment {
				segmentSize = uint64(len(segmentPrefix))
			}
			if test.index {
				indexSize = uint64(len(indexPrefix))
			}
			baselineTruncates, baselineSyncs := f.truncates, f.fdatasyncs
			baselineTruncateNames, baselineSyncNames := len(f.truncateNames), len(f.fdatasyncNames)
			result, err := lease.TruncateGenerationTails(context.Background(), snapshot, segmentSize, indexSize)
			if err != nil {
				t.Fatal(err)
			}
			next := result.Snapshot()
			nextIdentity, identityErr := next.IdentityDigest()
			index, indexErr := next.IndexBytes()
			segment, segmentErr := next.SegmentBytes(1)
			if result.Outcome() != AdmissionTransitionDurable || !result.ValidFor(lease) || identityErr != nil || indexErr != nil || segmentErr != nil || result.PreviousSnapshotIdentity() != previousIdentity || result.NextSnapshotIdentity() != nextIdentity || nextIdentity == previousIdentity || result.SegmentOrdinal() != 1 || result.SegmentPreviousSize() != snapshot.segments[1].stat.size || result.SegmentNextSize() != segmentSize || result.IndexPreviousSize() != snapshot.index.stat.size || result.IndexNextSize() != indexSize || !bytes.Equal(index, indexPrefix) || !bytes.Equal(segment, segmentPrefix) || f.truncates-baselineTruncates != len(test.wantOperations) || f.fdatasyncs-baselineSyncs != len(test.wantOperations) || !equalGenerationRepairNames(f.truncateNames[baselineTruncateNames:], test.wantOperations) || !equalGenerationRepairNames(f.fdatasyncNames[baselineSyncNames:], test.wantOperations) {
				t.Fatalf("outcome=%q valid=%v identities=%x/%x ordinal=%d sizes=%d/%d %d/%d operations=%v/%v want=%v bytes=%q/%q errs=%v/%v/%v", result.Outcome(), result.ValidFor(lease), result.PreviousSnapshotIdentity(), result.NextSnapshotIdentity(), result.SegmentOrdinal(), result.SegmentPreviousSize(), result.SegmentNextSize(), result.IndexPreviousSize(), result.IndexNextSize(), f.truncateNames[baselineTruncateNames:], f.fdatasyncNames[baselineSyncNames:], test.wantOperations, segment, index, identityErr, segmentErr, indexErr)
			}
			if _, err := snapshot.IndexFact(); !errors.Is(err, ErrLeaseInvalid) {
				t.Fatalf("old snapshot survived repair: %v", err)
			}
			if err := lease.Close(); err != nil || len(f.handles) != 0 {
				t.Fatalf("close=%v handles=%d", err, len(f.handles))
			}
		})
	}
}

func TestGenerationTailRepairPreflightRejectsZeroExtendAndNoop(t *testing.T) {
	tests := []struct {
		name                     string
		segmentDelta, indexDelta int64
		nilContext               bool
	}{
		{name: "no-op"},
		{name: "segment-zero", segmentDelta: -1 << 62},
		{name: "segment-extend", segmentDelta: 1},
		{name: "index-zero", indexDelta: -1 << 62},
		{name: "index-extend", indexDelta: 1},
		{name: "nil-context", segmentDelta: -1, nilContext: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			f, _, lease, _, _ := generationLeaseForSnapshot(t, [][]byte{[]byte("header")})
			snapshot, err := lease.Snapshot(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			segmentSize, indexSize := snapshot.segments[0].stat.size, snapshot.index.stat.size
			segmentSize = generationRepairSizeForTest(segmentSize, test.segmentDelta)
			indexSize = generationRepairSizeForTest(indexSize, test.indexDelta)
			ctx := context.Background()
			want := ErrInvalidInput
			if test.nilContext {
				ctx = nil
			}
			result, err := lease.TruncateGenerationTails(ctx, snapshot, segmentSize, indexSize)
			if !errors.Is(err, want) || result.Outcome() != AdmissionTransitionPreMutationFailure || result.Snapshot() != nil || result.ValidFor(lease) || !lease.OwnsSnapshot(snapshot) || !lease.Active() || f.truncates != 0 {
				t.Fatalf("err=%v outcome=%q valid=%v owns=%v active=%v truncates=%d", err, result.Outcome(), result.ValidFor(lease), lease.OwnsSnapshot(snapshot), lease.Active(), f.truncates)
			}
			if err := lease.Close(); err != nil || len(f.handles) != 0 {
				t.Fatalf("close=%v handles=%d", err, len(f.handles))
			}
		})
	}
}

func TestGenerationTailRepairFailuresAreUnknownAndReplayable(t *testing.T) {
	tests := []struct {
		name           string
		fault          func(*fakeBackend, *context.Context)
		segmentTrimmed bool
		indexTrimmed   bool
	}{
		{name: "segment-truncate", fault: func(f *fakeBackend, _ *context.Context) { f.failTruncateAt = f.truncates + 1 }},
		{name: "segment-response", fault: func(f *fakeBackend, _ *context.Context) { f.failTruncateAfterAt = f.truncates + 1 }, segmentTrimmed: true},
		{name: "segment-sync", fault: func(f *fakeBackend, _ *context.Context) { f.failFdatasyncAt = f.fdatasyncs + 1 }, segmentTrimmed: true},
		{name: "index-truncate", fault: func(f *fakeBackend, _ *context.Context) { f.failTruncateAt = f.truncates + 2 }, segmentTrimmed: true},
		{name: "index-response", fault: func(f *fakeBackend, _ *context.Context) { f.failTruncateAfterAt = f.truncates + 2 }, segmentTrimmed: true, indexTrimmed: true},
		{name: "index-sync", fault: func(f *fakeBackend, _ *context.Context) { f.failFdatasyncAt = f.fdatasyncs + 2 }, segmentTrimmed: true, indexTrimmed: true},
		{name: "context-between", fault: func(f *fakeBackend, ctx *context.Context) {
			value, cancel := context.WithCancel(context.Background())
			*ctx = value
			first := f.fdatasyncs + 1
			f.onFdatasync = func(_ *fakeBackend, _ *fakeNode, call int) {
				if call == first {
					cancel()
				}
			}
		}, segmentTrimmed: true},
		{name: "context-after", fault: func(f *fakeBackend, ctx *context.Context) {
			value, cancel := context.WithCancel(context.Background())
			*ctx = value
			second := f.fdatasyncs + 2
			f.onFdatasync = func(_ *fakeBackend, _ *fakeNode, call int) {
				if call == second {
					cancel()
				}
			}
		}, segmentTrimmed: true, indexTrimmed: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			f, _, lease, target, journal := generationLeaseForSnapshot(t, [][]byte{[]byte("header")})
			indexNode, segmentNode := generationAppendNodes(f, target, journal, 0)
			indexPrefix, segmentPrefix := append([]byte(nil), indexNode.data...), append([]byte(nil), segmentNode.data...)
			appendGenerationTailForTest(indexNode, []byte("index-torn"))
			appendGenerationTailForTest(segmentNode, []byte("segment-torn"))
			snapshot, err := lease.Snapshot(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			indexTorn, segmentTorn := append([]byte(nil), indexNode.data...), append([]byte(nil), segmentNode.data...)
			ctx := context.Background()
			test.fault(f, &ctx)
			result, err := lease.TruncateGenerationTails(ctx, snapshot, uint64(len(segmentPrefix)), uint64(len(indexPrefix)))
			if !errors.Is(err, ErrUnknown) || result.Outcome() != AdmissionTransitionUnknown || result.Snapshot() != nil || result.ValidFor(lease) || !lease.Active() || lease.OwnsSnapshot(snapshot) {
				t.Fatalf("err=%v outcome=%q snapshot=%v valid=%v active=%v owns=%v", err, result.Outcome(), result.Snapshot(), result.ValidFor(lease), lease.Active(), lease.OwnsSnapshot(snapshot))
			}
			fresh, err := lease.Snapshot(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			index, _ := fresh.IndexBytes()
			segment, _ := fresh.SegmentBytes(0)
			wantIndex, wantSegment := indexTorn, segmentTorn
			if test.segmentTrimmed {
				wantSegment = segmentPrefix
			}
			if test.indexTrimmed {
				wantIndex = indexPrefix
			}
			if !bytes.Equal(index, wantIndex) || !bytes.Equal(segment, wantSegment) {
				t.Fatalf("index=%q want=%q segment=%q want=%q", index, wantIndex, segment, wantSegment)
			}
			if err := lease.Close(); err != nil || len(f.handles) != 0 {
				t.Fatalf("close=%v handles=%d", err, len(f.handles))
			}
		})
	}
}

func TestGenerationTailRepairRejectsDriftBeforeTruncate(t *testing.T) {
	f, store, lease, target, journal := generationLeaseForSnapshot(t, [][]byte{[]byte("header")})
	indexNode, segmentNode := generationAppendNodes(f, target, journal, 0)
	appendGenerationTailForTest(segmentNode, []byte("torn"))
	snapshot, err := lease.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	indexNode.data[0] ^= 1
	result, err := lease.TruncateGenerationTails(context.Background(), snapshot, uint64(len("header")), indexNode.stat.size)
	if err == nil || errors.Is(err, ErrUnknown) || result.Outcome() != AdmissionTransitionPreMutationFailure || result.Snapshot() != nil || lease.Active() || lease.OwnsSnapshot(snapshot) || f.truncates != 0 || !store.usable() {
		t.Fatalf("err=%v outcome=%q active=%v owns=%v truncates=%d usable=%v", err, result.Outcome(), lease.Active(), lease.OwnsSnapshot(snapshot), f.truncates, store.usable())
	}
	if err := lease.Close(); err != nil || len(f.handles) != 0 {
		t.Fatalf("close=%v handles=%d", err, len(f.handles))
	}
}

func TestGenerationTailRepairTerminalDriftAndCleanupFailureAreUnknown(t *testing.T) {
	t.Run("terminal-drift", func(t *testing.T) {
		f, _, lease, target, journal := generationLeaseForSnapshot(t, [][]byte{[]byte("header"), []byte("last")})
		indexNode, segmentNode := generationAppendNodes(f, target, journal, 1)
		indexSize, segmentSize := indexNode.stat.size, segmentNode.stat.size
		appendGenerationTailForTest(indexNode, []byte("index-torn"))
		appendGenerationTailForTest(segmentNode, []byte("segment-torn"))
		snapshot, err := lease.Snapshot(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		second := f.fdatasyncs + 2
		f.onFdatasync = func(f *fakeBackend, _ *fakeNode, call int) {
			if call == second {
				_, first := generationAppendNodes(f, target, journal, 0)
				first.data[0] ^= 1
			}
		}
		result, err := lease.TruncateGenerationTails(context.Background(), snapshot, segmentSize, indexSize)
		if !errors.Is(err, ErrUnknown) || result.Outcome() != AdmissionTransitionUnknown || result.Snapshot() != nil || !lease.Active() || lease.OwnsSnapshot(snapshot) {
			t.Fatalf("err=%v outcome=%q snapshot=%v active=%v owns=%v", err, result.Outcome(), result.Snapshot(), lease.Active(), lease.OwnsSnapshot(snapshot))
		}
		if fresh, err := lease.Snapshot(context.Background()); err != nil || fresh == nil {
			t.Fatalf("fresh=%v err=%v", fresh, err)
		}
		if err := lease.Close(); err != nil || len(f.handles) != 0 {
			t.Fatalf("close=%v handles=%d", err, len(f.handles))
		}
	})
	t.Run("cleanup", func(t *testing.T) {
		f, store, lease, target, journal := generationLeaseForSnapshot(t, [][]byte{[]byte("header")})
		indexNode, segmentNode := generationAppendNodes(f, target, journal, 0)
		indexSize, segmentSize := indexNode.stat.size, segmentNode.stat.size
		appendGenerationTailForTest(indexNode, []byte("index-torn"))
		appendGenerationTailForTest(segmentNode, []byte("segment-torn"))
		snapshot, err := lease.Snapshot(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		f.failCloseAt["index.caj"] = f.closeNameCounts["index.caj"] + 2
		result, err := lease.TruncateGenerationTails(context.Background(), snapshot, segmentSize, indexSize)
		if !errors.Is(err, ErrUnknown) || !errors.Is(err, ErrFilesystem) || result.Outcome() != AdmissionTransitionUnknown || result.Snapshot() != nil || lease.Active() || store.usable() || len(f.handles) != 2 {
			t.Fatalf("err=%v outcome=%q snapshot=%v active=%v usable=%v handles=%d", err, result.Outcome(), result.Snapshot(), lease.Active(), store.usable(), len(f.handles))
		}
		delete(f.failCloseAt, "index.caj")
		if err := lease.Close(); err != nil || len(f.handles) != 0 {
			t.Fatalf("close=%v handles=%d", err, len(f.handles))
		}
	})
}

func appendGenerationTailForTest(node *fakeNode, suffix []byte) {
	node.data = append(node.data, suffix...)
	node.stat.size = uint64(len(node.data))
}

func equalGenerationRepairNames(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func generationRepairSizeForTest(current uint64, delta int64) uint64 {
	if delta < -int64(current) {
		return 0
	}
	return uint64(int64(current) + delta)
}
