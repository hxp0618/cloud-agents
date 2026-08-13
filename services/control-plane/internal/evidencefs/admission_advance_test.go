package evidencefs

import (
	"context"
	"errors"
	"testing"
)

func TestAdmissionInventoryAdvanceConsumesTokenAndKeepsFullSet(t *testing.T) {
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
	fullSet, err := inventory.FullSetDigest()
	if err != nil {
		t.Fatal(err)
	}
	result, err := token.Advance(context.Background(), inventory)
	if err != nil || result.Outcome() != AdmissionTransitionDurable || result.CandidateKind() != "inventory_advance" || result.CandidateSequence() != 0 || result.CandidateRevision() != 1 || result.PreviousRevision() != 0 || result.Inventory() == nil {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	nextFullSet, err := result.Inventory().FullSetDigest()
	if err != nil || nextFullSet != fullSet {
		t.Fatalf("full old=%x next=%x err=%v", fullSet, nextFullSet, err)
	}
	if _, err := inventory.Revision(); !errors.Is(err, ErrLeaseInvalid) || token.ValidFor(inventory) {
		t.Fatalf("old authority survived: err=%v token=%v", err, token.ValidFor(inventory))
	}
	if f.writes != 0 || f.renames != 0 || f.fsyncs != 0 || f.fdatasyncs != 0 {
		t.Fatalf("mutation calls writes=%d renames=%d fsync=%d fdatasync=%d", f.writes, f.renames, f.fsyncs, f.fdatasyncs)
	}
	if err := lease.Close(); err != nil || len(f.handles) != 0 {
		t.Fatalf("close err=%v handles=%d", err, len(f.handles))
	}
}

func TestAdmissionInventoryAdvancePreCancelPreservesAuthority(t *testing.T) {
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
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := token.Advance(ctx, inventory)
	if !errors.Is(err, context.Canceled) || result.Outcome() != AdmissionTransitionPreMutationFailure || !token.ValidFor(inventory) || !lease.Active() {
		t.Fatalf("result=%+v err=%v token=%v active=%v", result, err, token.ValidFor(inventory), lease.Active())
	}
	if err := lease.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestAdmissionInventoryAdvanceDurableResultCanInvalidate(t *testing.T) {
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
	result, err := token.Advance(context.Background(), inventory)
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
