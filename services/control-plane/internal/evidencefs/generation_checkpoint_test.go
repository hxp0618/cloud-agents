package evidencefs

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"testing"
)

func TestGenerationCheckpointAppendDurableOwnsBytesAndSnapshot(t *testing.T) {
	f, _, lease, target, journal := generationLeaseForSnapshot(t, [][]byte{[]byte("header"), []byte("rotation")})
	snapshot, err := lease.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	previousIdentity, _ := snapshot.IdentityDigest()
	oldIndex, _ := snapshot.IndexBytes()
	first, _ := snapshot.SegmentBytes(0)
	last, _ := snapshot.SegmentBytes(1)
	checkpoint := []byte("healing-checkpoint")
	wantCheckpoint := append([]byte(nil), checkpoint...)
	firstWrite := f.writes + 1
	f.partialWrite = 1
	f.onWrite = func(_ *fakeBackend, _ *fakeNode, call int) {
		if call == firstWrite {
			clear(checkpoint)
		}
	}
	baselineSyncs, baselineSyncNames := f.fdatasyncs, len(f.fdatasyncNames)
	result, err := lease.AppendGenerationCheckpoint(context.Background(), snapshot, checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	next := result.Snapshot()
	nextIdentity, identityErr := next.IdentityDigest()
	index, indexErr := next.IndexBytes()
	gotFirst, firstErr := next.SegmentBytes(0)
	gotLast, lastErr := next.SegmentBytes(1)
	if result.Outcome() != AdmissionTransitionDurable || !result.ValidFor(lease) || identityErr != nil || indexErr != nil || firstErr != nil || lastErr != nil ||
		result.CheckpointFramedDigest() != sha256.Sum256(wantCheckpoint) || result.PreviousSnapshotIdentity() != previousIdentity || result.NextSnapshotIdentity() != nextIdentity ||
		result.IndexPreviousSize() != uint64(len(oldIndex)) || !bytes.Equal(index, append(append([]byte(nil), oldIndex...), wantCheckpoint...)) ||
		!bytes.Equal(first, gotFirst) || !bytes.Equal(last, gotLast) || f.fdatasyncs-baselineSyncs != 1 || len(f.fdatasyncNames) != baselineSyncNames+1 || f.fdatasyncNames[len(f.fdatasyncNames)-1] != "index.caj" {
		t.Fatalf("outcome=%q valid=%v identities=%x/%x size=%d syncs=%d names=%v index=%q errs=%v/%v/%v/%v", result.Outcome(), result.ValidFor(lease), result.PreviousSnapshotIdentity(), result.NextSnapshotIdentity(), result.IndexPreviousSize(), f.fdatasyncs-baselineSyncs, f.fdatasyncNames, index, identityErr, indexErr, firstErr, lastErr)
	}
	if _, err := snapshot.IndexFact(); !errors.Is(err, ErrLeaseInvalid) {
		t.Fatalf("old snapshot survived checkpoint append: %v", err)
	}
	indexNode, lastNode := generationAppendNodes(f, target, journal, 1)
	if !bytes.Equal(indexNode.data, index) || !bytes.Equal(lastNode.data, last) {
		t.Fatal("durable checkpoint result differs from filesystem")
	}
	if err := lease.Close(); err != nil || len(f.handles) != 0 {
		t.Fatalf("close=%v handles=%d", err, len(f.handles))
	}
}

func TestGenerationCheckpointPreflightPreservesAuthority(t *testing.T) {
	tests := []struct {
		name    string
		ctx     func() context.Context
		payload func() []byte
		want    error
	}{
		{name: "canceled", ctx: canceledContextForGenerationAppend, payload: func() []byte { return []byte("checkpoint") }, want: context.Canceled},
		{name: "empty", ctx: context.Background, payload: func() []byte { return nil }, want: ErrInvalidInput},
		{name: "limit", ctx: context.Background, payload: func() []byte { return make([]byte, maximumAdmissionIndexBytes) }, want: ErrLimit},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			f, _, lease, _, _ := generationLeaseForSnapshot(t, [][]byte{[]byte("header")})
			snapshot, err := lease.Snapshot(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			writes, syncs := f.writes, f.fdatasyncs
			result, err := lease.AppendGenerationCheckpoint(test.ctx(), snapshot, test.payload())
			if !errors.Is(err, test.want) || result.Outcome() != AdmissionTransitionPreMutationFailure || result.Snapshot() != nil || result.ValidFor(lease) || !lease.OwnsSnapshot(snapshot) || !lease.Active() || f.writes != writes || f.fdatasyncs != syncs {
				t.Fatalf("err=%v outcome=%q valid=%v owns=%v active=%v writes=%d/%d syncs=%d/%d", err, result.Outcome(), result.ValidFor(lease), lease.OwnsSnapshot(snapshot), lease.Active(), f.writes, writes, f.fdatasyncs, syncs)
			}
			if err := lease.Close(); err != nil || len(f.handles) != 0 {
				t.Fatalf("close=%v handles=%d", err, len(f.handles))
			}
		})
	}
}

func TestGenerationCheckpointMutationFailuresAreUnknownAndReplayable(t *testing.T) {
	tests := []struct {
		name     string
		fault    func(*fakeBackend, *context.Context)
		appended bool
	}{
		{name: "write", fault: func(f *fakeBackend, _ *context.Context) { f.failWriteAt = f.writes + 1 }},
		{name: "sync", fault: func(f *fakeBackend, _ *context.Context) { f.failFdatasyncAt = f.fdatasyncs + 1 }, appended: true},
		{name: "context-after-write", fault: func(f *fakeBackend, ctx *context.Context) {
			value, cancel := context.WithCancel(context.Background())
			*ctx = value
			write := f.writes + 1
			f.onWrite = func(_ *fakeBackend, _ *fakeNode, call int) {
				if call == write {
					cancel()
				}
			}
		}, appended: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			f, _, lease, _, _ := generationLeaseForSnapshot(t, [][]byte{[]byte("header")})
			snapshot, err := lease.Snapshot(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			oldIndex, _ := snapshot.IndexBytes()
			checkpoint := []byte("checkpoint")
			ctx := context.Background()
			test.fault(f, &ctx)
			result, err := lease.AppendGenerationCheckpoint(ctx, snapshot, checkpoint)
			if !errors.Is(err, ErrUnknown) || result.Outcome() != AdmissionTransitionUnknown || result.Snapshot() != nil || result.ValidFor(lease) || !lease.Active() || lease.OwnsSnapshot(snapshot) {
				t.Fatalf("err=%v outcome=%q snapshot=%v valid=%v active=%v owns=%v", err, result.Outcome(), result.Snapshot(), result.ValidFor(lease), lease.Active(), lease.OwnsSnapshot(snapshot))
			}
			fresh, err := lease.Snapshot(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			index, _ := fresh.IndexBytes()
			want := oldIndex
			if test.appended {
				want = append(append([]byte(nil), oldIndex...), checkpoint...)
			}
			if !bytes.Equal(index, want) {
				t.Fatalf("index=%q want=%q", index, want)
			}
			if err := lease.Close(); err != nil || len(f.handles) != 0 {
				t.Fatalf("close=%v handles=%d", err, len(f.handles))
			}
		})
	}
}

func TestGenerationCheckpointRejectsAnySnapshotDriftBeforeWrite(t *testing.T) {
	for name, mutate := range map[string]func(*fakeBackend, [32]byte, [32]byte){
		"index": func(f *fakeBackend, target, journal [32]byte) {
			index, _ := generationAppendNodes(f, target, journal, 0)
			index.data[0] ^= 1
		},
		"journal": func(f *fakeBackend, target, journal [32]byte) {
			_, segment := generationAppendNodes(f, target, journal, 0)
			segment.data[0] ^= 1
		},
		"set": func(f *fakeBackend, target, journal [32]byte) {
			dir := f.root.children["lineages"].children[fmt.Sprintf("%x", target)].children[fmt.Sprintf("%x", journal)]
			dir.children[admissionSegmentName(1)] = f.regular(admissionSegmentName(1), []byte("extra"))
		},
	} {
		t.Run(name, func(t *testing.T) {
			f, store, lease, target, journal := generationLeaseForSnapshot(t, [][]byte{[]byte("header")})
			snapshot, err := lease.Snapshot(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			mutate(f, target, journal)
			writes := f.writes
			result, err := lease.AppendGenerationCheckpoint(context.Background(), snapshot, []byte("checkpoint"))
			if err == nil || errors.Is(err, ErrUnknown) || result.Outcome() != AdmissionTransitionPreMutationFailure || result.Snapshot() != nil || lease.Active() || lease.OwnsSnapshot(snapshot) || f.writes != writes || !store.usable() {
				t.Fatalf("err=%v outcome=%q active=%v owns=%v writes=%d/%d usable=%v", err, result.Outcome(), lease.Active(), lease.OwnsSnapshot(snapshot), f.writes, writes, store.usable())
			}
			if err := lease.Close(); err != nil || len(f.handles) != 0 {
				t.Fatalf("close=%v handles=%d", err, len(f.handles))
			}
		})
	}
}

func TestGenerationCheckpointCleanupFailurePoisonsStore(t *testing.T) {
	f, store, lease, _, _ := generationLeaseForSnapshot(t, [][]byte{[]byte("header")})
	snapshot, err := lease.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	f.failCloseAt["index.caj"] = f.closeNameCounts["index.caj"] + 2
	result, err := lease.AppendGenerationCheckpoint(context.Background(), snapshot, []byte("checkpoint"))
	if !errors.Is(err, ErrUnknown) || !errors.Is(err, ErrFilesystem) || result.Outcome() != AdmissionTransitionUnknown || result.Snapshot() != nil || lease.Active() || store.usable() || len(f.handles) != 2 {
		t.Fatalf("err=%v outcome=%q snapshot=%v active=%v usable=%v handles=%d", err, result.Outcome(), result.Snapshot(), lease.Active(), store.usable(), len(f.handles))
	}
	delete(f.failCloseAt, "index.caj")
	if err := lease.Close(); err != nil || len(f.handles) != 0 {
		t.Fatalf("close=%v handles=%d", err, len(f.handles))
	}
}
