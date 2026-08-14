package evidencefs

import (
	"bytes"
	"context"
	"errors"
	"testing"
)

func TestGenerationResyncDurablePreservesExactBytesAndIdentity(t *testing.T) {
	f, _, lease, _, _ := generationLeaseForSnapshot(t, [][]byte{[]byte("header"), []byte("rotation-and-candidate")})
	snapshot, err := lease.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	previousIdentity, _ := snapshot.IdentityDigest()
	index, _ := snapshot.IndexBytes()
	first, _ := snapshot.SegmentBytes(0)
	last, _ := snapshot.SegmentBytes(1)
	baselineSyncs, baselineNames := f.fdatasyncs, len(f.fdatasyncNames)
	result, err := lease.ResyncGenerationSnapshot(context.Background(), snapshot)
	if err != nil {
		t.Fatal(err)
	}
	next := result.Snapshot()
	nextIdentity, identityErr := next.IdentityDigest()
	nextIndex, indexErr := next.IndexBytes()
	nextFirst, firstErr := next.SegmentBytes(0)
	nextLast, lastErr := next.SegmentBytes(1)
	if result.Outcome() != AdmissionTransitionDurable || !result.ValidFor(lease) || identityErr != nil || indexErr != nil || firstErr != nil || lastErr != nil || result.PreviousSnapshotIdentity() != previousIdentity || result.NextSnapshotIdentity() != previousIdentity || nextIdentity != previousIdentity || result.SegmentOrdinal() != 1 || !bytes.Equal(index, nextIndex) || !bytes.Equal(first, nextFirst) || !bytes.Equal(last, nextLast) || f.fdatasyncs-baselineSyncs != 2 || len(f.fdatasyncNames) != baselineNames+2 || f.fdatasyncNames[baselineNames] != admissionSegmentName(1) || f.fdatasyncNames[baselineNames+1] != "index.caj" {
		t.Fatalf("outcome=%q valid=%v identities=%x/%x/%x ordinal=%d syncs=%d names=%v errs=%v/%v/%v/%v", result.Outcome(), result.ValidFor(lease), result.PreviousSnapshotIdentity(), result.NextSnapshotIdentity(), nextIdentity, result.SegmentOrdinal(), f.fdatasyncs-baselineSyncs, f.fdatasyncNames, identityErr, indexErr, firstErr, lastErr)
	}
	if _, err := snapshot.IndexFact(); !errors.Is(err, ErrLeaseInvalid) {
		t.Fatalf("old snapshot survived resync: %v", err)
	}
	if err := lease.Close(); err != nil || len(f.handles) != 0 {
		t.Fatalf("close=%v handles=%d", err, len(f.handles))
	}
}

func TestGenerationResyncPreflightPreservesAuthority(t *testing.T) {
	f, _, lease, _, _ := generationLeaseForSnapshot(t, [][]byte{[]byte("header")})
	snapshot, err := lease.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	baseline := f.fdatasyncs
	result, err := lease.ResyncGenerationSnapshot(ctx, snapshot)
	if !errors.Is(err, context.Canceled) || result.Outcome() != AdmissionTransitionPreMutationFailure || result.Snapshot() != nil || result.ValidFor(lease) || !lease.OwnsSnapshot(snapshot) || !lease.Active() || f.fdatasyncs != baseline {
		t.Fatalf("err=%v outcome=%q valid=%v owns=%v active=%v syncs=%d/%d", err, result.Outcome(), result.ValidFor(lease), lease.OwnsSnapshot(snapshot), lease.Active(), f.fdatasyncs, baseline)
	}
	if err := lease.Close(); err != nil || len(f.handles) != 0 {
		t.Fatalf("close=%v handles=%d", err, len(f.handles))
	}
}

func TestGenerationResyncFailuresAreUnknownAndFreshReplayable(t *testing.T) {
	tests := []struct {
		name  string
		fault func(*fakeBackend, *context.Context)
	}{
		{name: "segment-sync", fault: func(f *fakeBackend, _ *context.Context) { f.failFdatasyncAt = f.fdatasyncs + 1 }},
		{name: "index-sync", fault: func(f *fakeBackend, _ *context.Context) { f.failFdatasyncAt = f.fdatasyncs + 2 }},
		{name: "context-between-syncs", fault: func(f *fakeBackend, ctx *context.Context) {
			value, cancel := context.WithCancel(context.Background())
			*ctx = value
			first := f.fdatasyncs + 1
			f.onFdatasync = func(_ *fakeBackend, _ *fakeNode, call int) {
				if call == first {
					cancel()
				}
			}
		}},
		{name: "context-after-syncs", fault: func(f *fakeBackend, ctx *context.Context) {
			value, cancel := context.WithCancel(context.Background())
			*ctx = value
			second := f.fdatasyncs + 2
			f.onFdatasync = func(_ *fakeBackend, _ *fakeNode, call int) {
				if call == second {
					cancel()
				}
			}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			f, _, lease, _, _ := generationLeaseForSnapshot(t, [][]byte{[]byte("header")})
			snapshot, err := lease.Snapshot(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			identity, _ := snapshot.IdentityDigest()
			index, _ := snapshot.IndexBytes()
			segment, _ := snapshot.SegmentBytes(0)
			ctx := context.Background()
			test.fault(f, &ctx)
			result, err := lease.ResyncGenerationSnapshot(ctx, snapshot)
			if !errors.Is(err, ErrUnknown) || result.Outcome() != AdmissionTransitionUnknown || result.Snapshot() != nil || result.ValidFor(lease) || !lease.Active() || lease.OwnsSnapshot(snapshot) {
				t.Fatalf("err=%v outcome=%q snapshot=%v valid=%v active=%v owns=%v", err, result.Outcome(), result.Snapshot(), result.ValidFor(lease), lease.Active(), lease.OwnsSnapshot(snapshot))
			}
			fresh, err := lease.Snapshot(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			freshIdentity, _ := fresh.IdentityDigest()
			freshIndex, _ := fresh.IndexBytes()
			freshSegment, _ := fresh.SegmentBytes(0)
			if freshIdentity != identity || !bytes.Equal(index, freshIndex) || !bytes.Equal(segment, freshSegment) {
				t.Fatal("resync uncertainty changed filesystem bytes or identity")
			}
			if err := lease.Close(); err != nil || len(f.handles) != 0 {
				t.Fatalf("close=%v handles=%d", err, len(f.handles))
			}
		})
	}
}

func TestGenerationResyncRejectsDriftBeforeFirstSync(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*fakeBackend, [32]byte, [32]byte)
	}{
		{name: "content", mutate: func(f *fakeBackend, target, journal [32]byte) {
			_, segment := generationAppendNodes(f, target, journal, 0)
			segment.data[0] ^= 1
		}},
		{name: "replacement-after-full-read", mutate: func(f *fakeBackend, _, _ [32]byte) {
			name := admissionSegmentName(0)
			f.replaceOnOpen = name
			f.replaceOnOpenAt = f.openNameCounts[name] + 2
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			f, store, lease, target, journal := generationLeaseForSnapshot(t, [][]byte{[]byte("header")})
			snapshot, err := lease.Snapshot(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			test.mutate(f, target, journal)
			baseline := f.fdatasyncs
			result, err := lease.ResyncGenerationSnapshot(context.Background(), snapshot)
			if err == nil || errors.Is(err, ErrUnknown) || result.Outcome() != AdmissionTransitionPreMutationFailure || result.Snapshot() != nil || lease.Active() || lease.OwnsSnapshot(snapshot) || f.fdatasyncs != baseline || !store.usable() {
				t.Fatalf("err=%v outcome=%q active=%v owns=%v syncs=%d/%d usable=%v", err, result.Outcome(), lease.Active(), lease.OwnsSnapshot(snapshot), f.fdatasyncs, baseline, store.usable())
			}
			if err := lease.Close(); err != nil || len(f.handles) != 0 {
				t.Fatalf("close=%v handles=%d", err, len(f.handles))
			}
		})
	}
}

func TestGenerationResyncTerminalDriftAndCleanupFailureAreUnknown(t *testing.T) {
	t.Run("terminal-drift", func(t *testing.T) {
		f, _, lease, target, journal := generationLeaseForSnapshot(t, [][]byte{[]byte("header"), []byte("last")})
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
		result, err := lease.ResyncGenerationSnapshot(context.Background(), snapshot)
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
		f, store, lease, _, _ := generationLeaseForSnapshot(t, [][]byte{[]byte("header")})
		snapshot, err := lease.Snapshot(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		f.failCloseAt["index.caj"] = f.closeNameCounts["index.caj"] + 2
		result, err := lease.ResyncGenerationSnapshot(context.Background(), snapshot)
		if !errors.Is(err, ErrUnknown) || !errors.Is(err, ErrFilesystem) || result.Outcome() != AdmissionTransitionUnknown || result.Snapshot() != nil || lease.Active() || store.usable() || len(f.handles) != 2 {
			t.Fatalf("err=%v outcome=%q snapshot=%v active=%v usable=%v handles=%d", err, result.Outcome(), result.Snapshot(), lease.Active(), store.usable(), len(f.handles))
		}
		delete(f.failCloseAt, "index.caj")
		if err := lease.Close(); err != nil || len(f.handles) != 0 {
			t.Fatalf("close=%v handles=%d", err, len(f.handles))
		}
	})
}
