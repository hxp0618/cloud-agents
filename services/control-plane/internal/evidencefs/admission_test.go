package evidencefs

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

func digestForTest(value int) [32]byte {
	var digest [32]byte
	digest[30] = byte(value >> 8)
	digest[31] = byte(value)
	return digest
}

func addAdmissionLineage(f *fakeBackend, id [32]byte, journals int, segments int) *fakeNode {
	lineages := f.root.children["lineages"]
	if lineages == nil {
		lineages = f.directory("lineages")
		f.root.children["lineages"] = lineages
	}
	name := fmt.Sprintf("%x", id)
	lineage := f.directory(name)
	lineage.children["writer.lock"] = f.regular("writer.lock", nil)
	lineage.children["index.caj"] = f.regular("index.caj", []byte("index-"+name))
	for journalIndex := 0; journalIndex < journals; journalIndex++ {
		journalName := fmt.Sprintf("%064x", journalIndex+1000)
		journal := f.directory(journalName)
		journal.children["writer.lock"] = f.regular("writer.lock", nil)
		for segment := 0; segment < segments; segment++ {
			segmentName := admissionSegmentName(segment)
			journal.children[segmentName] = f.regular(segmentName, []byte(fmt.Sprintf("segment-%d-%d", journalIndex, segment)))
		}
		lineage.children[journalName] = journal
	}
	lineages.children[name] = lineage
	return lineage
}

func testStore(t *testing.T, f *fakeBackend) *Store {
	t.Helper()
	store, err := newRootWithAuthority(context.Background(), "/evidence", f.uid, f, mountAuthority{seal: &struct{}{}})
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func TestAcquireAdmissionEmptyAndTargetAbsentCreatesNothing(t *testing.T) {
	f := newFakeBackend()
	store := testStore(t, f)
	target := digestForTest(9)
	lease, inventory, err := store.AcquireAdmission(context.Background(), target)
	if err != nil {
		t.Fatal(err)
	}
	ids, err := inventory.LineageIDs()
	if err != nil || len(ids) != 0 {
		t.Fatalf("ids=%v err=%v", ids, err)
	}
	fact, err := inventory.TargetAbsent()
	if err != nil || fact == nil {
		t.Fatalf("fact=%v err=%v", fact, err)
	}
	got, err := fact.Target()
	if err != nil || got != target {
		t.Fatalf("target=%x err=%v", got, err)
	}
	if _, exists := f.root.children["lineages"]; exists {
		t.Fatal("absent target created forbidden lineages directory")
	}
	if f.writes != 0 || f.renames != 0 || f.fsyncs != 0 || f.fdatasyncs != 0 {
		t.Fatalf("mutation calls writes=%d renames=%d fsync=%d fdatasync=%d", f.writes, f.renames, f.fsyncs, f.fdatasyncs)
	}
	if err := lease.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestAdmissionMutationTokenIsExactOneShotRevisionAuthority(t *testing.T) {
	f := newFakeBackend()
	store := testStore(t, f)
	lease, inventory, err := store.AcquireAdmission(context.Background(), digestForTest(9))
	if err != nil {
		t.Fatal(err)
	}
	token, err := inventory.MutationToken()
	if err != nil || !token.ValidFor(inventory) {
		t.Fatalf("token=%+v err=%v", token, err)
	}
	if second, err := inventory.MutationToken(); second != nil || !errors.Is(err, ErrLeaseInvalid) {
		t.Fatalf("second token minted: token=%+v err=%v", second, err)
	}
	copyToken := *token
	if copyToken.ValidFor(inventory) {
		t.Fatal("copied token retained authority")
	}
	for name, mutate := range map[string]func(*AdmissionMutationToken){
		"target":   func(v *AdmissionMutationToken) { v.target[0]++ },
		"full set": func(v *AdmissionMutationToken) { v.fullSet[0]++ },
		"revision": func(v *AdmissionMutationToken) { v.revision++ },
		"consumed": func(v *AdmissionMutationToken) { v.consumed = true },
	} {
		t.Run(name, func(t *testing.T) {
			value := *token
			value.self = &value
			mutate(&value)
			if value.ValidFor(inventory) {
				t.Fatal("mutated token retained authority")
			}
		})
	}
	if (&AdmissionMutationToken{}).ValidFor(inventory) {
		t.Fatal("literal token retained authority")
	}
	copyInventory := *inventory
	if token.ValidFor(&copyInventory) {
		t.Fatal("token accepted copied inventory")
	}
	otherBackend := newFakeBackend()
	otherStore := testStore(t, otherBackend)
	otherLease, otherInventory, err := otherStore.AcquireAdmission(context.Background(), digestForTest(10))
	if err != nil {
		t.Fatal(err)
	}
	defer otherLease.Close()
	if token.ValidFor(otherInventory) {
		t.Fatal("token crossed admission epoch")
	}
	if err := lease.Close(); err != nil {
		t.Fatal(err)
	}
	if token.ValidFor(inventory) {
		t.Fatal("token survived lease close")
	}
}

func TestAdmissionMutationTokenRevokedByTerminalDrift(t *testing.T) {
	f := newFakeBackend()
	lineage := addAdmissionLineage(f, digestForTest(1), 0, 0)
	store := testStore(t, f)
	lease, inventory, err := store.AcquireAdmission(context.Background(), digestForTest(1))
	if err != nil {
		t.Fatal(err)
	}
	token, err := inventory.MutationToken()
	if err != nil {
		t.Fatal(err)
	}
	lineage.children["index.caj"].data[0] ^= 1
	if err := inventory.Revalidate(context.Background()); err == nil {
		t.Fatal("terminal drift passed revalidation")
	}
	if token.ValidFor(inventory) {
		t.Fatal("token survived terminal drift")
	}
	if err := lease.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestAcquireAdmissionCanonicalLocksAndPresentTarget(t *testing.T) {
	f := newFakeBackend()
	for _, value := range []int{3, 1, 2} {
		addAdmissionLineage(f, digestForTest(value), 1, 2)
	}
	store := testStore(t, f)
	lease, inventory, err := store.AcquireAdmission(context.Background(), digestForTest(2))
	if err != nil {
		t.Fatal(err)
	}
	ids, err := inventory.LineageIDs()
	if err != nil || len(ids) != 3 || ids[0] != digestForTest(1) || ids[1] != digestForTest(2) || ids[2] != digestForTest(3) {
		t.Fatalf("ids=%x err=%v", ids, err)
	}
	fact, err := inventory.TargetAbsent()
	if err != nil || fact != nil {
		t.Fatalf("present fact=%v err=%v", fact, err)
	}
	for _, id := range ids {
		lineage, err := inventory.Lineage(id)
		if err != nil {
			t.Fatal(err)
		}
		index, err := lineage.Index()
		if err != nil {
			t.Fatal(err)
		}
		if bytes, err := index.ReadAll(context.Background()); err != nil || len(bytes) == 0 {
			t.Fatalf("index bytes=%q err=%v", bytes, err)
		}
		journals, err := lineage.Journals()
		if err != nil || len(journals) != 1 {
			t.Fatalf("journals=%v err=%v", journals, err)
		}
		segments, err := journals[0].Segments()
		if err != nil || len(segments) != 2 {
			t.Fatalf("segments=%v err=%v", segments, err)
		}
	}
	if err := lease.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestAcquireAdmissionOneAndSixtyFourLineages(t *testing.T) {
	for _, count := range []int{1, 64} {
		t.Run(fmt.Sprintf("count-%d", count), func(t *testing.T) {
			f := newFakeBackend()
			for value := 0; value < count; value++ {
				addAdmissionLineage(f, digestForTest(value), 0, 0)
			}
			store := testStore(t, f)
			lease, inventory, err := store.AcquireAdmission(context.Background(), digestForTest(1000))
			if err != nil {
				t.Fatal(err)
			}
			ids, err := inventory.LineageIDs()
			if err != nil || len(ids) != count {
				t.Fatalf("ids=%d err=%v", len(ids), err)
			}
			_ = lease.Close()
		})
	}
}

func TestAcquireAdmissionHoldsOnlyRootAndLineageFDsThenClosesAll(t *testing.T) {
	f := newFakeBackend()
	for value := 1; value <= 3; value++ {
		addAdmissionLineage(f, digestForTest(value), 1, 2)
	}
	store := testStore(t, f)
	lease, _, err := store.AcquireAdmission(context.Background(), digestForTest(9))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(f.handles), 2+3; got != want {
		t.Fatalf("live fds=%d want=%d", got, want)
	}
	if err := lease.Close(); err != nil {
		t.Fatal(err)
	}
	if len(f.handles) != 0 {
		t.Fatalf("fd leak=%v", f.handles)
	}
}

func TestAcquireAdmissionBusyRetriesReleaseAll(t *testing.T) {
	for _, busyValue := range []int{1, 2, 3} {
		t.Run(fmt.Sprintf("busy-%d", busyValue), func(t *testing.T) {
			f := newFakeBackend()
			for value := 1; value <= 3; value++ {
				addAdmissionLineage(f, digestForTest(value), 0, 0)
			}
			lineageName := fmt.Sprintf("%x", digestForTest(busyValue))
			busy := f.root.children["lineages"].children[lineageName].children["writer.lock"]
			f.busyInodeAttempts[busy.stat.inode] = 1
			store := testStore(t, f)
			lease, _, err := store.AcquireAdmission(context.Background(), digestForTest(9))
			if err != nil || lease == nil {
				t.Fatalf("lease=%v err=%v", lease, err)
			}
			if len(f.tryLockAttempts) < 5 {
				t.Fatalf("attempts=%v", f.tryLockAttempts)
			}
			if err := lease.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestAcquireAdmissionCancelExhaustAndTryErrorCleanup(t *testing.T) {
	t.Run("cancel", func(t *testing.T) {
		f := newFakeBackend()
		lineage := addAdmissionLineage(f, digestForTest(1), 0, 0)
		lineage.children["writer.lock"].locked = true
		store := testStore(t, f)
		ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
		defer cancel()
		if lease, inventory, err := store.AcquireAdmission(ctx, digestForTest(9)); lease != nil || inventory != nil || !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("lease=%v inventory=%v err=%v", lease, inventory, err)
		}
		if f.root.children["lineages.lock"].locked {
			t.Fatal("root lock leaked")
		}
	})

	t.Run("exhaust", func(t *testing.T) {
		f := newFakeBackend()
		lineage := addAdmissionLineage(f, digestForTest(1), 0, 0)
		f.busyInodeAttempts[lineage.children["writer.lock"].stat.inode] = maximumAdmissionAttempts
		store := testStore(t, f)
		if lease, inventory, err := store.AcquireAdmission(context.Background(), digestForTest(9)); lease != nil || inventory != nil || !errors.Is(err, ErrFilesystem) {
			t.Fatalf("lease=%v inventory=%v err=%v", lease, inventory, err)
		}
		if f.root.children["lineages.lock"].locked {
			t.Fatal("root lock leaked")
		}
	})

	t.Run("try-error-unlocks-ambiguous-fd", func(t *testing.T) {
		f := newFakeBackend()
		lineage := addAdmissionLineage(f, digestForTest(1), 0, 0)
		f.failTryLockInodes[lineage.children["writer.lock"].stat.inode] = true
		store := testStore(t, f)
		lease, inventory, err := store.AcquireAdmission(context.Background(), digestForTest(9))
		if lease != nil || inventory != nil || !errors.Is(err, ErrFilesystem) || f.unlockAttempts == 0 || len(f.tryLockAttempts) != 2 {
			t.Fatalf("lease=%v inventory=%v err=%v unlockAttempts=%d tries=%v", lease, inventory, err, f.unlockAttempts, f.tryLockAttempts)
		}
	})

	for _, failing := range []int{2, 3} {
		t.Run(fmt.Sprintf("try-error-%d-releases-predecessors-reverse", failing), func(t *testing.T) {
			f := newFakeBackend()
			locks := make([]*fakeNode, 0, 3)
			for value := 1; value <= 3; value++ {
				lineage := addAdmissionLineage(f, digestForTest(value), 0, 0)
				locks = append(locks, lineage.children["writer.lock"])
			}
			f.failTryLockInodes[locks[failing-1].stat.inode] = true
			store := testStore(t, f)
			lease, inventory, err := store.AcquireAdmission(context.Background(), digestForTest(9))
			if lease != nil || inventory != nil || !errors.Is(err, ErrFilesystem) {
				t.Fatalf("lease=%v inventory=%v err=%v", lease, inventory, err)
			}
			want := make([]uint64, 0, failing)
			for index := failing - 1; index >= 0; index-- {
				want = append(want, locks[index].stat.inode)
			}
			if len(f.unlockInodes) < len(want) {
				t.Fatalf("unlocks=%v want=%v", f.unlockInodes, want)
			}
			for index := range want {
				if f.unlockInodes[index] != want[index] {
					t.Fatalf("unlocks=%v want=%v", f.unlockInodes, want)
				}
			}
		})
	}

	t.Run("cleanup-failure-does-not-retry", func(t *testing.T) {
		f := newFakeBackend()
		lineage := addAdmissionLineage(f, digestForTest(1), 0, 0)
		f.busyInodeAttempts[lineage.children["writer.lock"].stat.inode] = 1
		f.failCloseNames["writer.lock"] = true
		store := testStore(t, f)
		if lease, inventory, err := store.AcquireAdmission(context.Background(), digestForTest(9)); lease != nil || inventory != nil || !errors.Is(err, ErrFilesystem) {
			t.Fatalf("lease=%v inventory=%v err=%v", lease, inventory, err)
		}
		if store.usable() {
			t.Fatal("cleanup failure did not poison store")
		}
	})
}

func TestAcquireAdmissionOpenErrorsFailImmediatelyWithoutRetry(t *testing.T) {
	for _, name := range []string{"lineages", fmt.Sprintf("%x", digestForTest(1)), "writer.lock"} {
		t.Run(name, func(t *testing.T) {
			f := newFakeBackend()
			addAdmissionLineage(f, digestForTest(1), 0, 0)
			store := testStore(t, f)
			// Allow initial discovery; arm the failure on the first lineage lock walk.
			armed := false
			f.onReadDir = func(f *fakeBackend, node *fakeNode, _ int) {
				if !armed && node.name == "root" {
					armed = true
					f.failOpenNames[name] = fakeNotExist
				}
			}
			lease, inventory, err := store.AcquireAdmission(context.Background(), digestForTest(9))
			if lease != nil || inventory != nil || !errors.Is(err, ErrFilesystem) {
				t.Fatalf("lease=%v inventory=%v err=%v", lease, inventory, err)
			}
			if len(f.tryLockAttempts) > 1 {
				t.Fatalf("retried hard open error tries=%v", f.tryLockAttempts)
			}
		})
	}
}

func TestAcquireAdmissionRejectsHalfRegistrationAndClosedGrammar(t *testing.T) {
	tests := map[string]func(*fakeBackend){
		"missing-index": func(f *fakeBackend) { delete(addAdmissionLineage(f, digestForTest(1), 0, 0).children, "index.caj") },
		"unknown": func(f *fakeBackend) {
			addAdmissionLineage(f, digestForTest(1), 0, 0).children["unknown"] = f.regular("unknown", nil)
		},
		"segment-gap": func(f *fakeBackend) {
			lineage := addAdmissionLineage(f, digestForTest(1), 1, 2)
			for name, journal := range lineage.children {
				if finalNamePattern.MatchString(name) {
					delete(journal.children, admissionSegmentName(0))
				}
			}
		},
		"symlink-kind": func(f *fakeBackend) {
			addAdmissionLineage(f, digestForTest(1), 0, 0).children["index.caj"].stat.kind = kindUnknown
		},
		"hardlink": func(f *fakeBackend) {
			addAdmissionLineage(f, digestForTest(1), 0, 0).children["index.caj"].stat.nlink = 2
		},
		"wrong-mode": func(f *fakeBackend) {
			addAdmissionLineage(f, digestForTest(1), 0, 0).children["index.caj"].stat.mode = 0o644
		},
		"wrong-owner": func(f *fakeBackend) { addAdmissionLineage(f, digestForTest(1), 0, 0).children["index.caj"].stat.uid++ },
		"xdev": func(f *fakeBackend) {
			addAdmissionLineage(f, digestForTest(1), 0, 0).children["index.caj"].stat.device++
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			f := newFakeBackend()
			mutate(f)
			store := testStore(t, f)
			lease, inventory, err := store.AcquireAdmission(context.Background(), digestForTest(1))
			if lease != nil || inventory != nil || !errors.Is(err, ErrFilesystem) {
				t.Fatalf("lease=%v inventory=%v err=%v", lease, inventory, err)
			}
		})
	}
}

func TestAcquireAdmissionExactLineageLimit(t *testing.T) {
	f := newFakeBackend()
	for value := 0; value < maximumAdmissionLineages; value++ {
		addAdmissionLineage(f, digestForTest(value), 0, 0)
	}
	store := testStore(t, f)
	lease, inventory, err := store.AcquireAdmission(context.Background(), digestForTest(1000))
	if err != nil {
		t.Fatal(err)
	}
	ids, _ := inventory.LineageIDs()
	if len(ids) != maximumAdmissionLineages {
		t.Fatalf("ids=%d", len(ids))
	}
	_ = lease.Close()

	f = newFakeBackend()
	for value := 0; value < maximumAdmissionLineages+1; value++ {
		addAdmissionLineage(f, digestForTest(value), 0, 0)
	}
	store = testStore(t, f)
	if lease, inventory, err := store.AcquireAdmission(context.Background(), digestForTest(1000)); lease != nil || inventory != nil || !errors.Is(err, ErrLimit) {
		t.Fatalf("lease=%v inventory=%v err=%v", lease, inventory, err)
	}
}

func TestAdmissionCloseReverseOrderAndPoisonsOnCleanupFailure(t *testing.T) {
	f := newFakeBackend()
	for value := 1; value <= 3; value++ {
		addAdmissionLineage(f, digestForTest(value), 0, 0)
	}
	store := testStore(t, f)
	lease, _, err := store.AcquireAdmission(context.Background(), digestForTest(9))
	if err != nil {
		t.Fatal(err)
	}
	expected := make([]uint64, len(lease.locks))
	for index := range lease.locks {
		expected[len(lease.locks)-1-index] = lease.locks[index].stat.inode
	}
	f.closeAttempts = nil
	f.unlockInodes = nil
	f.failUnlock = true
	f.failCloseNames["writer.lock"] = true
	if err := lease.Close(); !errors.Is(err, ErrFilesystem) {
		t.Fatalf("close err=%v", err)
	}
	if f.unlockAttempts < 4 || store.usable() {
		t.Fatalf("unlock attempts=%d usable=%v closes=%v", f.unlockAttempts, store.usable(), f.closeAttempts)
	}
	if len(f.unlockInodes) < len(expected) {
		t.Fatalf("unlock order=%v expected prefix=%v", f.unlockInodes, expected)
	}
	for index := range expected {
		if f.unlockInodes[index] != expected[index] {
			t.Fatalf("unlock order=%v expected prefix=%v", f.unlockInodes, expected)
		}
	}
}
