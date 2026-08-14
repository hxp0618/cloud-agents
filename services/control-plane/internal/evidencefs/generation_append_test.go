package evidencefs

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"testing"
)

func TestGenerationAppendExistingSegmentCompositeDurablePartialWrites(t *testing.T) {
	f, _, lease, target, journal := generationLeaseForSnapshot(t, [][]byte{[]byte("header"), []byte("rotation")})
	snapshot, err := lease.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	previousIdentity, err := snapshot.IdentityDigest()
	if err != nil {
		t.Fatal(err)
	}
	oldIndex, err := snapshot.IndexBytes()
	if err != nil {
		t.Fatal(err)
	}
	oldSegment, err := snapshot.SegmentBytes(1)
	if err != nil {
		t.Fatal(err)
	}
	journalFramed, checkpointFramed := []byte("journal-record"), []byte("checkpoint-record")
	wantJournalFramed, wantCheckpointFramed := append([]byte(nil), journalFramed...), append([]byte(nil), checkpointFramed...)
	baselineSyncs := f.fdatasyncs
	f.partialWrite = 1
	firstWrite := f.writes + 1
	f.onWrite = func(_ *fakeBackend, _ *fakeNode, call int) {
		if call == firstWrite {
			clear(journalFramed)
			clear(checkpointFramed)
		}
	}

	result, err := lease.AppendExistingSegmentComposite(context.Background(), snapshot, journalFramed, checkpointFramed)
	if err != nil {
		t.Fatal(err)
	}
	next := result.Snapshot()
	nextIdentity, identityErr := next.IdentityDigest()
	index, indexErr := next.IndexBytes()
	segment, segmentErr := next.SegmentBytes(1)
	first, firstErr := next.SegmentBytes(0)
	if result.Outcome() != AdmissionTransitionDurable || !result.ValidFor(lease) || identityErr != nil || indexErr != nil || segmentErr != nil || firstErr != nil ||
		result.PreviousSnapshotIdentity() != previousIdentity || result.NextSnapshotIdentity() != nextIdentity || result.SegmentOrdinal() != 1 ||
		result.JournalPreviousSize() != uint64(len(oldSegment)) || result.IndexPreviousSize() != uint64(len(oldIndex)) ||
		result.JournalFramedDigest() != sha256.Sum256(wantJournalFramed) || result.CheckpointFramedDigest() != sha256.Sum256(wantCheckpointFramed) ||
		!bytes.Equal(index, append(append([]byte(nil), oldIndex...), wantCheckpointFramed...)) ||
		!bytes.Equal(segment, append(append([]byte(nil), oldSegment...), wantJournalFramed...)) || !bytes.Equal(first, []byte("header")) ||
		f.fdatasyncs-baselineSyncs != 2 || len(f.fdatasyncNames) < 2 || f.fdatasyncNames[len(f.fdatasyncNames)-2] != admissionSegmentName(1) || f.fdatasyncNames[len(f.fdatasyncNames)-1] != "index.caj" {
		t.Fatalf("outcome=%q valid=%v identities=%x/%x ordinal=%d sizes=%d/%d syncs=%d bytes=%q/%q/%q errs=%v/%v/%v/%v", result.Outcome(), result.ValidFor(lease), result.PreviousSnapshotIdentity(), result.NextSnapshotIdentity(), result.SegmentOrdinal(), result.JournalPreviousSize(), result.IndexPreviousSize(), f.fdatasyncs-baselineSyncs, first, segment, index, identityErr, indexErr, segmentErr, firstErr)
	}
	if _, err := snapshot.IndexFact(); !errors.Is(err, ErrLeaseInvalid) {
		t.Fatalf("previous snapshot survived durable append: %v", err)
	}
	copySnapshot := *next
	copyResult := result
	copyResult.snapshot = &copySnapshot
	if copyResult.ValidFor(lease) {
		t.Fatal("copied snapshot retained durable result authority")
	}
	replacement, err := lease.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.ValidFor(lease) || !lease.OwnsSnapshot(replacement) {
		t.Fatal("durable result survived snapshot replacement")
	}
	indexNode, segmentNode := generationAppendNodes(f, target, journal, 1)
	if !bytes.Equal(indexNode.data, index) || !bytes.Equal(segmentNode.data, segment) {
		t.Fatal("durable snapshot did not match final filesystem bytes")
	}
	if err := lease.Close(); err != nil || len(f.handles) != 0 {
		t.Fatalf("close=%v handles=%d", err, len(f.handles))
	}
}

func TestGenerationAppendPreflightPreservesCurrentAuthority(t *testing.T) {
	tests := []struct {
		name       string
		context    func() context.Context
		journal    func() []byte
		checkpoint []byte
		want       error
	}{
		{name: "canceled", context: canceledContextForGenerationAppend, journal: func() []byte { return []byte("journal") }, checkpoint: []byte("checkpoint"), want: context.Canceled},
		{name: "empty-journal", context: context.Background, journal: func() []byte { return nil }, checkpoint: []byte("checkpoint"), want: ErrInvalidInput},
		{name: "empty-checkpoint", context: context.Background, journal: func() []byte { return []byte("journal") }, checkpoint: nil, want: ErrInvalidInput},
		{name: "segment-limit", context: context.Background, journal: func() []byte { return make([]byte, maximumAdmissionSegmentBytes) }, checkpoint: []byte("checkpoint"), want: ErrLimit},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			f, _, lease, target, journal := generationLeaseForSnapshot(t, [][]byte{[]byte("header")})
			snapshot, err := lease.Snapshot(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			indexNode, segmentNode := generationAppendNodes(f, target, journal, 0)
			oldIndex, oldSegment := append([]byte(nil), indexNode.data...), append([]byte(nil), segmentNode.data...)
			writes, syncs := f.writes, f.fdatasyncs
			result, err := lease.AppendExistingSegmentComposite(test.context(), snapshot, test.journal(), test.checkpoint)
			if !errors.Is(err, test.want) || result.Outcome() != AdmissionTransitionPreMutationFailure || result.Snapshot() != nil || result.ValidFor(lease) || !lease.OwnsSnapshot(snapshot) || !lease.Active() || f.writes != writes || f.fdatasyncs != syncs || !bytes.Equal(indexNode.data, oldIndex) || !bytes.Equal(segmentNode.data, oldSegment) {
				t.Fatalf("err=%v outcome=%q valid=%v owns=%v active=%v writes=%d/%d syncs=%d/%d", err, result.Outcome(), result.ValidFor(lease), lease.OwnsSnapshot(snapshot), lease.Active(), f.writes, writes, f.fdatasyncs, syncs)
			}
			if err := lease.Close(); err != nil || len(f.handles) != 0 {
				t.Fatalf("close=%v handles=%d", err, len(f.handles))
			}
		})
	}
}

func TestGenerationAppendPreMutationDriftRevokesLease(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*fakeBackend, [32]byte, [32]byte)
	}{
		{name: "same-size-prefix", mutate: func(f *fakeBackend, target, journal [32]byte) {
			_, segment := generationAppendNodes(f, target, journal, 0)
			segment.data[0] ^= 1
		}},
		{name: "segment-replacement", mutate: func(f *fakeBackend, _, _ [32]byte) {
			name := admissionSegmentName(0)
			f.replaceOnOpen = name
			f.replaceOnOpenAt = f.openNameCounts[name] + 1
		}},
		{name: "lineage-lock-replacement", mutate: func(f *fakeBackend, target, _ [32]byte) {
			lineage := f.root.children["lineages"].children[fmt.Sprintf("%x", target)]
			lineage.children["writer.lock"] = f.regular("writer.lock", nil)
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
			writes := f.writes
			result, err := lease.AppendExistingSegmentComposite(context.Background(), snapshot, []byte("journal"), []byte("checkpoint"))
			if err == nil || errors.Is(err, ErrUnknown) || result.Outcome() != AdmissionTransitionPreMutationFailure || result.Snapshot() != nil || lease.Active() || lease.OwnsSnapshot(snapshot) || f.writes != writes || !store.usable() {
				t.Fatalf("err=%v outcome=%q active=%v owns=%v writes=%d/%d usable=%v", err, result.Outcome(), lease.Active(), lease.OwnsSnapshot(snapshot), f.writes, writes, store.usable())
			}
			if err := lease.Close(); err != nil || len(f.handles) != 0 {
				t.Fatalf("close=%v handles=%d", err, len(f.handles))
			}
		})
	}
}

func TestGenerationAppendMutationFailuresAreUnknownAndReplayable(t *testing.T) {
	tests := []struct {
		name               string
		fault              func(*fakeBackend, *context.Context)
		journalAppended    bool
		checkpointAppended bool
	}{
		{name: "journal-write", fault: func(f *fakeBackend, _ *context.Context) { f.failWriteAt = f.writes + 1 }},
		{name: "journal-sync", fault: func(f *fakeBackend, _ *context.Context) { f.failFdatasyncAt = f.fdatasyncs + 1 }, journalAppended: true},
		{name: "checkpoint-write", fault: func(f *fakeBackend, _ *context.Context) { f.failWriteAt = f.writes + 2 }, journalAppended: true},
		{name: "checkpoint-sync", fault: func(f *fakeBackend, _ *context.Context) { f.failFdatasyncAt = f.fdatasyncs + 2 }, journalAppended: true, checkpointAppended: true},
		{name: "context-after-journal", fault: func(f *fakeBackend, ctx *context.Context) {
			value, cancel := context.WithCancel(context.Background())
			*ctx = value
			write := f.writes + 1
			f.onWrite = func(_ *fakeBackend, _ *fakeNode, call int) {
				if call == write {
					cancel()
				}
			}
		}, journalAppended: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			f, _, lease, target, journal := generationLeaseForSnapshot(t, [][]byte{[]byte("header")})
			snapshot, err := lease.Snapshot(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			oldIndex, _ := snapshot.IndexBytes()
			oldSegment, _ := snapshot.SegmentBytes(0)
			journalFramed, checkpointFramed := []byte("journal"), []byte("checkpoint")
			ctx := context.Background()
			test.fault(f, &ctx)
			result, err := lease.AppendExistingSegmentComposite(ctx, snapshot, journalFramed, checkpointFramed)
			if !errors.Is(err, ErrUnknown) || result.Outcome() != AdmissionTransitionUnknown || result.Snapshot() != nil || result.ValidFor(lease) || !lease.Active() {
				t.Fatalf("err=%v outcome=%q snapshot=%v valid=%v active=%v", err, result.Outcome(), result.Snapshot(), result.ValidFor(lease), lease.Active())
			}
			if _, err := snapshot.IndexFact(); !errors.Is(err, ErrLeaseInvalid) {
				t.Fatalf("old snapshot survived unknown: %v", err)
			}
			fresh, err := lease.Snapshot(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			index, _ := fresh.IndexBytes()
			segment, _ := fresh.SegmentBytes(0)
			wantIndex, wantSegment := oldIndex, oldSegment
			if test.journalAppended {
				wantSegment = append(append([]byte(nil), oldSegment...), journalFramed...)
			}
			if test.checkpointAppended {
				wantIndex = append(append([]byte(nil), oldIndex...), checkpointFramed...)
			}
			indexNode, segmentNode := generationAppendNodes(f, target, journal, 0)
			if !bytes.Equal(index, wantIndex) || !bytes.Equal(segment, wantSegment) || !bytes.Equal(indexNode.data, wantIndex) || !bytes.Equal(segmentNode.data, wantSegment) {
				t.Fatalf("index=%q want=%q segment=%q want=%q", index, wantIndex, segment, wantSegment)
			}
			if err := lease.Close(); err != nil || len(f.handles) != 0 {
				t.Fatalf("close=%v handles=%d", err, len(f.handles))
			}
		})
	}
}

func TestGenerationAppendTerminalDriftIsUnknown(t *testing.T) {
	f, _, lease, target, journal := generationLeaseForSnapshot(t, [][]byte{[]byte("header"), []byte("rotation")})
	snapshot, err := lease.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	write := f.writes + 2
	f.onWrite = func(f *fakeBackend, _ *fakeNode, call int) {
		if call == write {
			_, first := generationAppendNodes(f, target, journal, 0)
			first.data[0] ^= 1
		}
	}
	result, err := lease.AppendExistingSegmentComposite(context.Background(), snapshot, []byte("journal"), []byte("checkpoint"))
	if !errors.Is(err, ErrUnknown) || result.Outcome() != AdmissionTransitionUnknown || result.Snapshot() != nil || !lease.Active() || lease.OwnsSnapshot(snapshot) {
		t.Fatalf("err=%v outcome=%q snapshot=%v active=%v owns=%v", err, result.Outcome(), result.Snapshot(), lease.Active(), lease.OwnsSnapshot(snapshot))
	}
	if fresh, err := lease.Snapshot(context.Background()); err != nil || fresh == nil {
		t.Fatalf("fresh=%v err=%v", fresh, err)
	}
	if err := lease.Close(); err != nil || len(f.handles) != 0 {
		t.Fatalf("close=%v handles=%d", err, len(f.handles))
	}
}

func TestGenerationAppendCleanupFailurePoisonsStoreAndStillClosesAll(t *testing.T) {
	f, store, lease, _, _ := generationLeaseForSnapshot(t, [][]byte{[]byte("header")})
	snapshot, err := lease.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	f.failCloseNames["index.caj"] = true
	result, err := lease.AppendExistingSegmentComposite(context.Background(), snapshot, []byte("journal"), []byte("checkpoint"))
	if !errors.Is(err, ErrUnknown) || !errors.Is(err, ErrFilesystem) || result.Outcome() != AdmissionTransitionUnknown || result.Snapshot() != nil || lease.Active() || store.usable() || len(f.handles) != 2 {
		t.Fatalf("err=%v outcome=%q snapshot=%v active=%v usable=%v handles=%d", err, result.Outcome(), result.Snapshot(), lease.Active(), store.usable(), len(f.handles))
	}
	for _, name := range []string{"index.caj", admissionSegmentName(0), fmt.Sprintf("%x", lease.journal), fmt.Sprintf("%x", lease.target), "lineages", "root"} {
		if !containsGenerationAppendString(f.closeAttempts, name) {
			t.Fatalf("cleanup did not attempt %q: %v", name, f.closeAttempts)
		}
	}
	delete(f.failCloseNames, "index.caj")
	if err := lease.Close(); err != nil || len(f.handles) != 0 {
		t.Fatalf("close=%v handles=%d", err, len(f.handles))
	}
}

func TestGenerationAppendRejectsForeignCopiedAndStaleAuthority(t *testing.T) {
	f, _, lease, _, _ := generationLeaseForSnapshot(t, [][]byte{[]byte("header")})
	otherBackend, _, otherLease, _, _ := generationLeaseForSnapshot(t, [][]byte{[]byte("other")})
	current, err := lease.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	foreign, err := otherLease.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	writes := f.writes
	for name, candidate := range map[string]*GenerationSnapshot{
		"foreign": foreign,
		"copy":    func() *GenerationSnapshot { value := *current; return &value }(),
	} {
		t.Run(name, func(t *testing.T) {
			result, err := lease.AppendExistingSegmentComposite(context.Background(), candidate, []byte("journal"), []byte("checkpoint"))
			if !errors.Is(err, ErrLeaseInvalid) || result.Outcome() != AdmissionTransitionPreMutationFailure || result.ValidFor(lease) || !lease.OwnsSnapshot(current) || f.writes != writes {
				t.Fatalf("err=%v outcome=%q valid=%v owns=%v writes=%d/%d", err, result.Outcome(), result.ValidFor(lease), lease.OwnsSnapshot(current), f.writes, writes)
			}
		})
	}
	replacement, err := lease.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	result, err := lease.AppendExistingSegmentComposite(context.Background(), current, []byte("journal"), []byte("checkpoint"))
	if !errors.Is(err, ErrLeaseInvalid) || result.Outcome() != AdmissionTransitionPreMutationFailure || !lease.OwnsSnapshot(replacement) || f.writes != writes {
		t.Fatalf("stale err=%v outcome=%q owns=%v writes=%d/%d", err, result.Outcome(), lease.OwnsSnapshot(replacement), f.writes, writes)
	}
	copyLease := *lease
	result, err = copyLease.AppendExistingSegmentComposite(context.Background(), replacement, []byte("journal"), []byte("checkpoint"))
	if !errors.Is(err, ErrLeaseInvalid) || result.Outcome() != AdmissionTransitionPreMutationFailure || !lease.OwnsSnapshot(replacement) || f.writes != writes {
		t.Fatalf("copy lease err=%v outcome=%q owns=%v writes=%d/%d", err, result.Outcome(), lease.OwnsSnapshot(replacement), f.writes, writes)
	}
	result, err = lease.AppendExistingSegmentComposite(nil, replacement, []byte("journal"), []byte("checkpoint"))
	if !errors.Is(err, ErrInvalidInput) || result.Outcome() != AdmissionTransitionPreMutationFailure || !lease.OwnsSnapshot(replacement) || f.writes != writes {
		t.Fatalf("nil context err=%v outcome=%q owns=%v writes=%d/%d", err, result.Outcome(), lease.OwnsSnapshot(replacement), f.writes, writes)
	}
	if err := lease.Close(); err != nil || len(f.handles) != 0 {
		t.Fatalf("close=%v handles=%d", err, len(f.handles))
	}
	if err := otherLease.Close(); err != nil || len(otherBackend.handles) != 0 {
		t.Fatalf("other close=%v handles=%d", err, len(otherBackend.handles))
	}
}

func generationAppendNodes(f *fakeBackend, target, journal [32]byte, ordinal int) (*fakeNode, *fakeNode) {
	lineage := f.root.children["lineages"].children[fmt.Sprintf("%x", target)]
	return lineage.children["index.caj"], lineage.children[fmt.Sprintf("%x", journal)].children[admissionSegmentName(ordinal)]
}

func canceledContextForGenerationAppend() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}

func containsGenerationAppendString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
