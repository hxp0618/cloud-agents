package evidencefs

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"testing"
)

func generationLeaseForSnapshot(t *testing.T, segments [][]byte) (*fakeBackend, *Store, *GenerationLease, [32]byte, [32]byte) {
	t.Helper()
	f := newFakeBackend()
	target, journal := digestForTest(9), digestForTest(77)
	if len(segments) == 0 {
		t.Fatal("snapshot fixture requires segment zero")
	}
	addAdmissionLineage(f, target, 0, 0)
	store := testStore(t, f)
	admission, inventory, err := store.AcquireAdmission(context.Background(), target)
	if err != nil {
		t.Fatal(err)
	}
	token, err := inventory.MutationToken()
	if err != nil {
		t.Fatal(err)
	}
	created, err := token.CreateGenerationHeader(context.Background(), inventory, journal, segments[0])
	if err != nil {
		t.Fatal(err)
	}
	inventory = created.Inventory()
	token, err = inventory.MutationToken()
	if err != nil {
		t.Fatal(err)
	}
	lease, err := token.HandoffGeneration(context.Background(), inventory, journal)
	if err != nil || lease == nil {
		t.Fatalf("lease=%v err=%v", lease, err)
	}
	if admission.Active() {
		t.Fatal("admission remained active")
	}
	generation := f.root.children["lineages"].children[fmt.Sprintf("%x", target)].children[fmt.Sprintf("%x", journal)]
	for ordinal := 1; ordinal < len(segments); ordinal++ {
		generation.children[admissionSegmentName(ordinal)] = f.regular(admissionSegmentName(ordinal), segments[ordinal])
	}
	return f, store, lease, target, journal
}

func TestGenerationSnapshotReadContextAndFileCloseFailure(t *testing.T) {
	t.Run("context", func(t *testing.T) {
		f, _, lease, _, _ := generationLeaseForSnapshot(t, [][]byte{[]byte("header")})
		snapshot, err := lease.Snapshot(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if raw, err := snapshot.ReadSegment(ctx, 0); raw != nil || !errors.Is(err, context.Canceled) || !lease.Active() {
			t.Fatalf("raw=%v err=%v active=%v", raw, err, lease.Active())
		}
		if err := lease.Close(); err != nil || len(f.handles) != 0 {
			t.Fatalf("cleanup=%v handles=%d", err, len(f.handles))
		}
	})
	t.Run("close", func(t *testing.T) {
		f, store, lease, _, _ := generationLeaseForSnapshot(t, [][]byte{[]byte("header")})
		snapshot, err := lease.Snapshot(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		f.failCloseNames[admissionSegmentName(0)] = true
		if raw, err := snapshot.ReadSegment(context.Background(), 0); raw != nil || !errors.Is(err, ErrFilesystem) || lease.Active() || store.usable() {
			t.Fatalf("raw=%v err=%v active=%v usable=%v", raw, err, lease.Active(), store.usable())
		}
		delete(f.failCloseNames, admissionSegmentName(0))
		if err := lease.Close(); err != nil || len(f.handles) != 0 {
			t.Fatalf("cleanup=%v handles=%d", err, len(f.handles))
		}
	})
}

func TestGenerationSnapshotReadsOwnedIndexAndOrderedSegments(t *testing.T) {
	f, _, lease, _, _ := generationLeaseForSnapshot(t, [][]byte{[]byte("header"), []byte("rotation-and-record")})
	snapshot, err := lease.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.index.bytes) != 0 || len(snapshot.segments[0].bytes) != 0 {
		t.Fatal("snapshot retained raw file bytes")
	}
	index, indexErr := snapshot.IndexBytes()
	indexFact, indexFactErr := snapshot.IndexFact()
	count, countErr := snapshot.SegmentCount()
	first, firstErr := snapshot.SegmentBytes(0)
	second, secondErr := snapshot.SegmentBytes(1)
	firstFact, factErr := snapshot.SegmentFact(0)
	identity, identityErr := snapshot.IdentityDigest()
	if indexErr != nil || indexFactErr != nil || countErr != nil || firstErr != nil || secondErr != nil || factErr != nil || identityErr != nil || len(index) == 0 || count != 2 || !bytes.Equal(first, []byte("header")) || !bytes.Equal(second, []byte("rotation-and-record")) || indexFact.Size != uint64(len(index)) || firstFact.Ordinal != 0 || firstFact.Size != uint64(len(first)) || firstFact.ContentDigest == ([32]byte{}) || firstFact.IdentityDigest == ([32]byte{}) || identity == ([32]byte{}) {
		t.Fatalf("index=%q/%+v count=%d first=%q second=%q fact=%+v identity=%x errs=%v/%v/%v/%v/%v/%v/%v", index, indexFact, count, first, second, firstFact, identity, indexErr, indexFactErr, countErr, firstErr, secondErr, factErr, identityErr)
	}
	first[0] ^= 1
	again, err := snapshot.SegmentBytes(0)
	if err != nil || bytes.Equal(first, again) {
		t.Fatal("caller mutation changed owned segment bytes")
	}
	if _, err := snapshot.SegmentBytes(2); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("out-of-range segment=%v", err)
	}
	if err := snapshot.Revalidate(context.Background()); err != nil || !lease.Active() {
		t.Fatalf("revalidate=%v active=%v", err, lease.Active())
	}
	if err := lease.Close(); err != nil || len(f.handles) != 0 {
		t.Fatalf("close=%v handles=%d", err, len(f.handles))
	}
	if _, err := snapshot.IndexBytes(); !errors.Is(err, ErrLeaseInvalid) {
		t.Fatalf("snapshot survived close: %v", err)
	}
}

func TestGenerationSnapshotDetectsContentAndSetDrift(t *testing.T) {
	for name, mutate := range map[string]func(*fakeBackend, [32]byte, [32]byte){
		"index": func(f *fakeBackend, target, _ [32]byte) {
			file := f.root.children["lineages"].children[fmt.Sprintf("%x", target)].children["index.caj"]
			file.data[0] ^= 1
		},
		"segment": func(f *fakeBackend, target, journal [32]byte) {
			file := f.root.children["lineages"].children[fmt.Sprintf("%x", target)].children[fmt.Sprintf("%x", journal)].children[admissionSegmentName(0)]
			file.data[0] ^= 1
		},
		"extra-segment": func(f *fakeBackend, target, journal [32]byte) {
			dir := f.root.children["lineages"].children[fmt.Sprintf("%x", target)].children[fmt.Sprintf("%x", journal)]
			dir.children[admissionSegmentName(1)] = f.regular(admissionSegmentName(1), []byte("extra"))
		},
	} {
		t.Run(name, func(t *testing.T) {
			f, _, lease, target, journal := generationLeaseForSnapshot(t, [][]byte{[]byte("header")})
			snapshot, err := lease.Snapshot(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			mutate(f, target, journal)
			if err := snapshot.Revalidate(context.Background()); err == nil || lease.Active() {
				t.Fatalf("drift=%v active=%v", err, lease.Active())
			}
			if err := lease.Close(); err != nil || len(f.handles) != 0 {
				t.Fatalf("cleanup=%v handles=%d", err, len(f.handles))
			}
		})
	}
}

func TestGenerationSnapshotContextFailurePreservesLease(t *testing.T) {
	f, _, lease, _, _ := generationLeaseForSnapshot(t, [][]byte{[]byte("header")})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if snapshot, err := lease.Snapshot(ctx); snapshot != nil || !errors.Is(err, context.Canceled) || !lease.Active() {
		t.Fatalf("snapshot=%v err=%v active=%v", snapshot, err, lease.Active())
	}
	if err := lease.Close(); err != nil || len(f.handles) != 0 {
		t.Fatalf("cleanup=%v handles=%d", err, len(f.handles))
	}
}

func TestGenerationSnapshotCloseFailureRevokesLeaseAndPoisonsStore(t *testing.T) {
	f, store, lease, _, _ := generationLeaseForSnapshot(t, [][]byte{[]byte("header")})
	f.failCloseNames["index.caj"] = true
	if snapshot, err := lease.Snapshot(context.Background()); snapshot != nil || !errors.Is(err, ErrFilesystem) || lease.Active() || store.usable() {
		t.Fatalf("snapshot=%v err=%v active=%v usable=%v", snapshot, err, lease.Active(), store.usable())
	}
	delete(f.failCloseNames, "index.caj")
	if err := lease.Close(); err != nil || len(f.handles) != 0 {
		t.Fatalf("cleanup=%v handles=%d", err, len(f.handles))
	}
}

func TestGenerationSnapshotRejectsCopyAndPairedMutation(t *testing.T) {
	f, _, lease, _, _ := generationLeaseForSnapshot(t, [][]byte{[]byte("header")})
	snapshot, err := lease.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	copySnapshot := *snapshot
	if _, err := copySnapshot.IndexBytes(); !errors.Is(err, ErrLeaseInvalid) {
		t.Fatal("copied snapshot retained authority")
	}
	snapshot.index.digest[0]++
	snapshot.canonical = generationSnapshotDigest(snapshot)
	snapshot.binding.canonical = snapshot.canonical
	if _, err := snapshot.IndexBytes(); !errors.Is(err, ErrLeaseInvalid) {
		t.Fatal("paired mutation crossed immutable registry")
	}
	if err := lease.Close(); err != nil || len(f.handles) != 0 {
		t.Fatalf("cleanup=%v handles=%d", err, len(f.handles))
	}
}

func TestGenerationSnapshotReplacementInvalidatesPriorSnapshot(t *testing.T) {
	f, _, lease, _, _ := generationLeaseForSnapshot(t, [][]byte{[]byte("header")})
	first, err := lease.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	second, err := lease.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := first.IndexFact(); !errors.Is(err, ErrLeaseInvalid) {
		t.Fatalf("replaced snapshot remained live: %v", err)
	}
	if _, err := second.IndexFact(); err != nil {
		t.Fatalf("replacement snapshot invalid: %v", err)
	}
	if err := lease.Close(); err != nil || len(f.handles) != 0 {
		t.Fatalf("cleanup=%v handles=%d", err, len(f.handles))
	}
	if _, err := second.IndexFact(); !errors.Is(err, ErrLeaseInvalid) {
		t.Fatalf("snapshot survived lease close: %v", err)
	}
}

func TestGenerationSnapshotRepeatedReadsDoNotLeakDescriptors(t *testing.T) {
	f, _, lease, _, _ := generationLeaseForSnapshot(t, [][]byte{[]byte("header")})
	snapshot, err := lease.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	baseline := len(f.handles)
	for attempt := 0; attempt < 20; attempt++ {
		if _, err := snapshot.ReadIndex(context.Background()); err != nil {
			t.Fatal(err)
		}
		if _, err := snapshot.ReadSegment(context.Background(), 0); err != nil {
			t.Fatal(err)
		}
		if len(f.handles) != baseline {
			t.Fatalf("attempt=%d handles=%d baseline=%d", attempt, len(f.handles), baseline)
		}
	}
	if err := lease.Close(); err != nil || len(f.handles) != 0 {
		t.Fatalf("cleanup=%v handles=%d", err, len(f.handles))
	}
}

func TestGenerationSnapshotRejectsInvalidSegmentGrammar(t *testing.T) {
	for name, mutate := range map[string]func(*fakeBackend, [32]byte, [32]byte){
		"gap": func(f *fakeBackend, target, journal [32]byte) {
			dir := f.root.children["lineages"].children[fmt.Sprintf("%x", target)].children[fmt.Sprintf("%x", journal)]
			dir.children[admissionSegmentName(2)] = f.regular(admissionSegmentName(2), []byte("gap"))
		},
		"empty": func(f *fakeBackend, target, journal [32]byte) {
			segment := f.root.children["lineages"].children[fmt.Sprintf("%x", target)].children[fmt.Sprintf("%x", journal)].children[admissionSegmentName(0)]
			segment.data = nil
			segment.stat.size = 0
		},
		"missing-lock": func(f *fakeBackend, target, journal [32]byte) {
			delete(f.root.children["lineages"].children[fmt.Sprintf("%x", target)].children[fmt.Sprintf("%x", journal)].children, "writer.lock")
		},
	} {
		t.Run(name, func(t *testing.T) {
			f, _, lease, target, journal := generationLeaseForSnapshot(t, [][]byte{[]byte("header")})
			mutate(f, target, journal)
			if snapshot, err := lease.Snapshot(context.Background()); snapshot != nil || err == nil || lease.Active() {
				t.Fatalf("snapshot=%v err=%v active=%v", snapshot, err, lease.Active())
			}
			if err := lease.Close(); err != nil || len(f.handles) != 0 {
				t.Fatalf("cleanup=%v handles=%d", err, len(f.handles))
			}
		})
	}
}

func TestGenerationSnapshotRevalidateCloseFailureInvalidatesAuthority(t *testing.T) {
	f, store, lease, _, _ := generationLeaseForSnapshot(t, [][]byte{[]byte("header")})
	snapshot, err := lease.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	f.failCloseNames["index.caj"] = true
	if err := snapshot.Revalidate(context.Background()); !errors.Is(err, ErrFilesystem) || lease.Active() || store.usable() {
		t.Fatalf("err=%v active=%v usable=%v", err, lease.Active(), store.usable())
	}
	delete(f.failCloseNames, "index.caj")
	if err := lease.Close(); err != nil || len(f.handles) != 0 {
		t.Fatalf("cleanup=%v handles=%d", err, len(f.handles))
	}
}
