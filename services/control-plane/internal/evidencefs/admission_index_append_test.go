package evidencefs

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"testing"
)

func TestAdmissionAppendTargetIndexDurableAdvancesExactEOF(t *testing.T) {
	f := newFakeBackend()
	f.partialWrite = 2
	target := digestForTest(9)
	lineage := addAdmissionLineage(f, target, 0, 0)
	prefix := []byte("lineage-header")
	lineage.children["index.caj"].data = append([]byte(nil), prefix...)
	lineage.children["index.caj"].stat.size = uint64(len(prefix))
	store := testStore(t, f)
	lease, inventory := acquireRegisteredAdmissionForTest(t, f, store, target)
	previousRevision, err := inventory.Revision()
	if err != nil {
		t.Fatal(err)
	}
	token, err := inventory.MutationToken()
	if err != nil {
		t.Fatal(err)
	}
	frame := []byte("generation-reserved-frame")
	oldFullSet, err := inventory.FullSetDigest()
	if err != nil {
		t.Fatal(err)
	}
	result, err := token.AppendTargetIndex(context.Background(), inventory, frame)
	if err != nil || result.Outcome() != AdmissionTransitionDurable || result.CandidateKind() != "target_index_append" || result.CandidateDigest() != sha256.Sum256(frame) || result.CandidateRevision() != previousRevision+1 || result.PreviousRevision() != previousRevision {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	next := result.Inventory()
	if next == nil {
		t.Fatal("durable append returned no inventory")
	}
	if fact, err := next.TargetRegistration(); err != nil || fact != nil {
		t.Fatalf("post-append registration fact=%v err=%v", fact, err)
	}
	nextLineage, err := next.Lineage(target)
	if err != nil {
		t.Fatal(err)
	}
	index, err := nextLineage.Index()
	if err != nil {
		t.Fatal(err)
	}
	got, err := index.ReadAll(context.Background())
	want := append(append([]byte(nil), prefix...), frame...)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("index=%q want=%q err=%v", got, want, err)
	}
	newFullSet, err := next.FullSetDigest()
	if err != nil || newFullSet == oldFullSet || f.writes <= 1 || f.fdatasyncs != 1 || f.fsyncs != 0 {
		t.Fatalf("full old=%x new=%x err=%v writes=%d fdatasync=%d fsync=%d", oldFullSet, newFullSet, err, f.writes, f.fdatasyncs, f.fsyncs)
	}
	if _, err := inventory.Revision(); !errors.Is(err, ErrLeaseInvalid) || token.ValidFor(inventory) {
		t.Fatalf("old authority survived: err=%v token=%v", err, token.ValidFor(inventory))
	}
	if err := next.Revalidate(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := lease.Close(); err != nil || len(f.handles) != 0 {
		t.Fatalf("close err=%v handles=%d", err, len(f.handles))
	}
}

func TestAdmissionAppendTargetIndexPreMutationFailuresPreserveAuthority(t *testing.T) {
	f := newFakeBackend()
	store := testStore(t, f)
	lease, inventory, err := store.AcquireAdmission(context.Background(), digestForTest(9))
	if err != nil {
		t.Fatal(err)
	}
	token, err := inventory.MutationToken()
	if err != nil {
		t.Fatal(err)
	}
	result, err := token.AppendTargetIndex(context.Background(), inventory, []byte("frame"))
	if !errors.Is(err, ErrInvalidInput) || result.Outcome() != AdmissionTransitionPreMutationFailure || !token.ValidFor(inventory) {
		t.Fatalf("absent result=%+v err=%v token=%v", result, err, token.ValidFor(inventory))
	}
	if err := lease.Close(); err != nil {
		t.Fatal(err)
	}

	f = newFakeBackend()
	target := digestForTest(9)
	lineage := addAdmissionLineage(f, target, 0, 0)
	lineage.children["index.caj"].stat.size = maximumAdmissionIndexBytes
	lineage.children["index.caj"].data = nil
	lineage.children["index.caj"].virtualZero = true
	store = testStore(t, f)
	lease, inventory = acquireRegisteredAdmissionForTest(t, f, store, target)
	token, err = inventory.MutationToken()
	if err != nil {
		t.Fatal(err)
	}
	result, err = token.AppendTargetIndex(context.Background(), inventory, []byte("x"))
	if !errors.Is(err, ErrLimit) || result.Outcome() != AdmissionTransitionPreMutationFailure || !token.ValidFor(inventory) || f.writes != 0 {
		t.Fatalf("limit result=%+v err=%v token=%v writes=%d", result, err, token.ValidFor(inventory), f.writes)
	}
	if err := lease.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestAdmissionAppendTargetIndexPostWriteFailureIsUnknown(t *testing.T) {
	for name, arm := range map[string]func(*fakeBackend, context.CancelFunc){
		"sync": func(f *fakeBackend, _ context.CancelFunc) { f.failFdatasyncAt = 1 },
		"cancel": func(f *fakeBackend, cancel context.CancelFunc) {
			f.partialWrite = 1
			f.onWrite = func(_ *fakeBackend, node *fakeNode, call int) {
				if node.name == "index.caj" && call == 1 {
					cancel()
				}
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFakeBackend()
			target := digestForTest(9)
			lineage := addAdmissionLineage(f, target, 0, 0)
			lineage.children["index.caj"].data = []byte("header")
			lineage.children["index.caj"].stat.size = uint64(len("header"))
			store := testStore(t, f)
			lease, inventory := acquireRegisteredAdmissionForTest(t, f, store, target)
			token, err := inventory.MutationToken()
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			arm(f, cancel)
			result, err := token.AppendTargetIndex(ctx, inventory, []byte("frame"))
			if !errors.Is(err, ErrUnknown) || result.Outcome() != AdmissionTransitionUnknown || result.Inventory() != nil || lease.Active() || token.ValidFor(inventory) {
				t.Fatalf("result=%+v err=%v active=%v token=%v", result, err, lease.Active(), token.ValidFor(inventory))
			}
			if len(lineage.children["index.caj"].data) <= len("header") {
				t.Fatal("post-write failure left no durable candidate bytes")
			}
			if err := lease.Close(); err != nil || len(f.handles) != 0 {
				t.Fatalf("close err=%v handles=%d", err, len(f.handles))
			}
		})
	}
}

func TestAdmissionAppendTargetIndexDurableResultCanInvalidate(t *testing.T) {
	f := newFakeBackend()
	target := digestForTest(9)
	addAdmissionLineage(f, target, 0, 0)
	store := testStore(t, f)
	lease, inventory := acquireRegisteredAdmissionForTest(t, f, store, target)
	token, err := inventory.MutationToken()
	if err != nil {
		t.Fatal(err)
	}
	result, err := token.AppendTargetIndex(context.Background(), inventory, []byte("frame"))
	if err != nil {
		t.Fatal(err)
	}
	if err := result.Invalidate(); err != nil || lease.Active() {
		t.Fatalf("invalidate err=%v active=%v", err, lease.Active())
	}
	if err := result.Invalidate(); !errors.Is(err, ErrLeaseInvalid) {
		t.Fatalf("second invalidate=%v", err)
	}
	if err := lease.Close(); err != nil || len(f.handles) != 0 {
		t.Fatalf("close err=%v handles=%d", err, len(f.handles))
	}
}
