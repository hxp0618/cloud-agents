package evidencefs

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"testing"
)

func TestAdmissionCreateGenerationHeaderDurableAndRetainsLock(t *testing.T) {
	f := newFakeBackend()
	f.partialWrite = 3
	target := digestForTest(9)
	addAdmissionLineage(f, target, 0, 0)
	store := testStore(t, f)
	lease, inventory, err := store.AcquireAdmission(context.Background(), target)
	if err != nil {
		t.Fatal(err)
	}
	token, err := inventory.MutationToken()
	if err != nil {
		t.Fatal(err)
	}
	journal := digestForTest(77)
	header := []byte("canonical-segment-zero-header")
	oldFullSet, err := inventory.FullSetDigest()
	if err != nil {
		t.Fatal(err)
	}
	result, err := token.CreateGenerationHeader(context.Background(), inventory, journal, header)
	if err != nil || result.Outcome() != AdmissionTransitionDurable || result.CandidateKind() != "generation_header" || result.HeaderDigest() != sha256.Sum256(header) || result.HeaderSize() != uint64(len(header)) || result.Journal() != journal || result.CandidateRevision() != 1 || result.PreviousRevision() != 0 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	next := result.Inventory()
	if next == nil || !result.ValidFor(next) {
		t.Fatalf("next=%v valid=%v", next, result.ValidFor(next))
	}
	lineage, err := next.Lineage(target)
	if err != nil {
		t.Fatal(err)
	}
	view := findAdmissionJournal(lineage, journal)
	if view == nil {
		t.Fatal("journal view is absent")
	}
	segments, err := view.Segments()
	if err != nil || len(segments) != 1 {
		t.Fatalf("segments=%v err=%v", segments, err)
	}
	got, err := segments[0].ReadAll(context.Background())
	if err != nil || !bytes.Equal(got, header) {
		t.Fatalf("header=%q err=%v", got, err)
	}
	newFullSet, err := next.FullSetDigest()
	if err != nil || newFullSet == oldFullSet || len(lease.journalLocks) != 1 || lease.journalLocks[0].name != fmt.Sprintf("%x", journal) {
		t.Fatalf("full old=%x new=%x err=%v locks=%v", oldFullSet, newFullSet, err, lease.journalLocks)
	}
	if f.fsyncs != 3 || f.fdatasyncs != 2 || f.writes <= 1 {
		t.Fatalf("fsync=%d fdatasync=%d writes=%d", f.fsyncs, f.fdatasyncs, f.writes)
	}
	if err := lease.Close(); err != nil || len(f.handles) != 0 {
		t.Fatalf("close err=%v handles=%d", err, len(f.handles))
	}
}

func TestAdmissionCreateGenerationHeaderPreMutationFailuresPreserveAuthority(t *testing.T) {
	f := newFakeBackend()
	target := digestForTest(9)
	addAdmissionLineage(f, target, maximumAdmissionJournalsPerLineage, 1)
	store := testStore(t, f)
	lease, inventory, err := store.AcquireAdmission(context.Background(), target)
	if err != nil {
		t.Fatal(err)
	}
	token, err := inventory.MutationToken()
	if err != nil {
		t.Fatal(err)
	}
	result, err := token.CreateGenerationHeader(context.Background(), inventory, digestForTest(77), []byte("header"))
	if !errors.Is(err, ErrLimit) || result.Outcome() != AdmissionTransitionPreMutationFailure || !token.ValidFor(inventory) || len(f.mkdirs) != 0 || f.writes != 0 {
		t.Fatalf("result=%+v err=%v token=%v mkdirs=%v writes=%d", result, err, token.ValidFor(inventory), f.mkdirs, f.writes)
	}
	if err := lease.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestAdmissionCreateGenerationHeaderPostMutationFailureIsUnknown(t *testing.T) {
	for name, arm := range map[string]func(*fakeBackend){
		"parent-sync":  func(f *fakeBackend) { f.failFsync = true },
		"lock-sync":    func(f *fakeBackend) { f.failFdatasyncAt = 1 },
		"segment-sync": func(f *fakeBackend) { f.failFdatasyncAt = 2 },
		"lock-busy": func(f *fakeBackend) {
			f.onTryLock = func(value *fakeBackend, node *fakeNode, _ int) {
				if node.name == "writer.lock" {
					value.busyInodeAttempts[node.stat.inode] = 1
				}
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFakeBackend()
			target := digestForTest(9)
			addAdmissionLineage(f, target, 0, 0)
			store := testStore(t, f)
			lease, inventory, err := store.AcquireAdmission(context.Background(), target)
			if err != nil {
				t.Fatal(err)
			}
			token, err := inventory.MutationToken()
			if err != nil {
				t.Fatal(err)
			}
			arm(f)
			journal := digestForTest(77)
			result, err := token.CreateGenerationHeader(context.Background(), inventory, journal, []byte("header"))
			if !errors.Is(err, ErrUnknown) || result.Outcome() != AdmissionTransitionUnknown || result.Inventory() != nil || lease.Active() || token.ValidFor(inventory) {
				t.Fatalf("result=%+v err=%v active=%v token=%v", result, err, lease.Active(), token.ValidFor(inventory))
			}
			name := fmt.Sprintf("%x", journal)
			if f.root.children["lineages"].children[fmt.Sprintf("%x", target)].children[name] == nil {
				t.Fatal("post-mutation failure removed journal prefix")
			}
			if err := lease.Close(); err != nil || len(f.handles) != 0 {
				t.Fatalf("close err=%v handles=%d", err, len(f.handles))
			}
		})
	}
}

func TestAdmissionCreateGenerationHeaderDurableResultCanInvalidate(t *testing.T) {
	f := newFakeBackend()
	target := digestForTest(9)
	addAdmissionLineage(f, target, 0, 0)
	store := testStore(t, f)
	lease, inventory, err := store.AcquireAdmission(context.Background(), target)
	if err != nil {
		t.Fatal(err)
	}
	token, err := inventory.MutationToken()
	if err != nil {
		t.Fatal(err)
	}
	result, err := token.CreateGenerationHeader(context.Background(), inventory, digestForTest(77), []byte("header"))
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

func TestAdmissionCreateGenerationHeaderRetainedLockIsSlotBound(t *testing.T) {
	f := newFakeBackend()
	target := digestForTest(9)
	addAdmissionLineage(f, target, 0, 0)
	store := testStore(t, f)
	lease, inventory, err := store.AcquireAdmission(context.Background(), target)
	if err != nil {
		t.Fatal(err)
	}
	token, err := inventory.MutationToken()
	if err != nil {
		t.Fatal(err)
	}
	result, err := token.CreateGenerationHeader(context.Background(), inventory, digestForTest(77), []byte("header"))
	if err != nil || !result.ValidFor(result.Inventory()) {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	lease.journalLocks[0].stat.inode++
	if result.ValidFor(result.Inventory()) || result.Inventory().Revalidate(context.Background()) == nil || lease.Active() {
		t.Fatalf("tampered result remained valid: active=%v", lease.Active())
	}
	if err := lease.Close(); err != nil || len(f.handles) != 0 {
		t.Fatalf("close err=%v handles=%d", err, len(f.handles))
	}
}

func TestAdmissionCreateGenerationHeaderCloseAttemptsJournalAndLineageCleanup(t *testing.T) {
	f := newFakeBackend()
	target := digestForTest(9)
	addAdmissionLineage(f, target, 0, 0)
	store := testStore(t, f)
	lease, inventory, err := store.AcquireAdmission(context.Background(), target)
	if err != nil {
		t.Fatal(err)
	}
	token, err := inventory.MutationToken()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := token.CreateGenerationHeader(context.Background(), inventory, digestForTest(77), []byte("header")); err != nil {
		t.Fatal(err)
	}
	f.closeAttempts = nil
	f.unlockAttempts = 0
	f.failUnlock = true
	f.failCloseNames["writer.lock"] = true
	if err := lease.Close(); !errors.Is(err, ErrFilesystem) || store.usable() {
		t.Fatalf("close err=%v usable=%v", err, store.usable())
	}
	if f.unlockAttempts < 3 || len(f.closeAttempts) < 4 || len(f.handles) != 0 {
		t.Fatalf("unlock=%d close=%v handles=%d", f.unlockAttempts, f.closeAttempts, len(f.handles))
	}
}

func TestAdmissionCreateGenerationHeaderPreservesAcquisitionOrderForCleanup(t *testing.T) {
	f := newFakeBackend()
	target := digestForTest(9)
	addAdmissionLineage(f, target, 0, 0)
	store := testStore(t, f)
	lease, inventory, err := store.AcquireAdmission(context.Background(), target)
	if err != nil {
		t.Fatal(err)
	}
	for _, journal := range [][32]byte{digestForTest(90), digestForTest(80)} {
		token, tokenErr := inventory.MutationToken()
		if tokenErr != nil {
			t.Fatal(tokenErr)
		}
		result, createErr := token.CreateGenerationHeader(context.Background(), inventory, journal, []byte("header"))
		if createErr != nil || !result.ValidFor(result.Inventory()) {
			t.Fatalf("journal=%x result=%+v err=%v", journal, result, createErr)
		}
		inventory = result.Inventory()
	}
	if len(lease.journalLocks) != 2 || lease.journalLocks[0].name != fmt.Sprintf("%x", digestForTest(90)) || lease.journalLocks[1].name != fmt.Sprintf("%x", digestForTest(80)) {
		t.Fatalf("acquisition order=%v", lease.journalLocks)
	}
	wantFirst, wantSecond := lease.journalLocks[1].stat.inode, lease.journalLocks[0].stat.inode
	f.unlockInodes = nil
	if err := lease.Close(); err != nil || len(f.handles) != 0 {
		t.Fatalf("close err=%v handles=%d", err, len(f.handles))
	}
	if len(f.unlockInodes) < 2 || f.unlockInodes[0] != wantFirst || f.unlockInodes[1] != wantSecond {
		t.Fatalf("unlock order=%v want prefix=[%d %d]", f.unlockInodes, wantFirst, wantSecond)
	}
}
