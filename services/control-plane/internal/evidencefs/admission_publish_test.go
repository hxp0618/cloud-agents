package evidencefs

import (
	"context"
	"crypto/sha256"
	"errors"
	"testing"
)

func TestAdmissionPublishObjectDurableAdvancesInventory(t *testing.T) {
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
	content := []byte("candidate-runtime-object")
	digest := sha256.Sum256(content)
	oldFullSet, err := inventory.FullSetDigest()
	if err != nil {
		t.Fatal(err)
	}
	result, err := token.PublishObject(context.Background(), inventory, digest, content)
	if err != nil || result.Outcome() != AdmissionTransitionDurable || result.CandidateKind() != "content_object" || result.CandidateDigest() != digest || result.CandidateSequence() != 0 || result.CandidateRevision() != 1 || result.PreviousRevision() != 0 || result.Size() != uint64(len(content)) || result.Reused() {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	next, publication := result.Inventory(), result.Publication()
	if next == nil || publication == nil || !result.ValidFor(next) || publication.Matches(digest, uint64(len(content))) {
		t.Fatalf("next=%v publication=%v transientMatches=%v", next, publication, publication != nil && publication.Matches(digest, uint64(len(content))))
	}
	if revision, err := next.Revision(); err != nil || revision != 1 {
		t.Fatalf("revision=%d err=%v", revision, err)
	}
	newFullSet, err := next.FullSetDigest()
	if err != nil || newFullSet == oldFullSet {
		t.Fatalf("full set old=%x new=%x err=%v", oldFullSet, newFullSet, err)
	}
	objects, err := next.Objects()
	if err != nil || len(objects) != 1 {
		t.Fatalf("objects=%v err=%v", objects, err)
	}
	if got, err := objects[0].Digest(); err != nil || got != digest {
		t.Fatalf("digest=%x err=%v", got, err)
	}
	if _, err := inventory.Revision(); !errors.Is(err, ErrLeaseInvalid) || token.ValidFor(inventory) {
		t.Fatalf("old authority survived: err=%v token=%v", err, token.ValidFor(inventory))
	}
	copyResult := result
	copyResult.candidateDigest[0]++
	if copyResult.ValidFor(next) || (AdmissionPublicationTransitionResult{}).ValidFor(next) {
		t.Fatal("mutated or literal publication result retained authority")
	}
	if err := lease.rootLease.BindPublication(publication, digest, uint64(len(content))); err != nil || !publication.Matches(digest, uint64(len(content))) {
		t.Fatalf("bind err=%v matches=%v", err, publication.Matches(digest, uint64(len(content))))
	}
	if err := next.Revalidate(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := lease.Close(); err != nil || len(f.handles) != 0 {
		t.Fatalf("close err=%v handles=%d", err, len(f.handles))
	}
}

func TestAdmissionPublishObjectDurablyReusesExistingFinal(t *testing.T) {
	f := newFakeBackend()
	target := digestForTest(9)
	addAdmissionLineage(f, target, 0, 0)
	content := []byte("candidate-runtime-object")
	digest := f.addFinal(content)
	store := testStore(t, f)
	lease, inventory, err := store.AcquireAdmission(context.Background(), target)
	if err != nil {
		t.Fatal(err)
	}
	oldFullSet, err := inventory.FullSetDigest()
	if err != nil {
		t.Fatal(err)
	}
	token, err := inventory.MutationToken()
	if err != nil {
		t.Fatal(err)
	}
	result, err := token.PublishObject(context.Background(), inventory, digest, content)
	if err != nil || result.Outcome() != AdmissionTransitionDurable || !result.Reused() || result.Publication() == nil {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	newFullSet, err := result.Inventory().FullSetDigest()
	if err != nil || newFullSet != oldFullSet || f.writes != 0 || f.renames != 0 || f.fsyncs != 1 {
		t.Fatalf("full old=%x new=%x err=%v writes=%d renames=%d fsync=%d", oldFullSet, newFullSet, err, f.writes, f.renames, f.fsyncs)
	}
	if err := lease.rootLease.BindPublication(result.Publication(), digest, uint64(len(content))); err != nil {
		t.Fatal(err)
	}
	if err := lease.Close(); err != nil || len(f.handles) != 0 {
		t.Fatalf("close err=%v handles=%d", err, len(f.handles))
	}
}

func TestAdmissionPublishObjectPreMutationFailurePreservesAuthority(t *testing.T) {
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
	result, err := token.PublishObject(context.Background(), inventory, [32]byte{1}, []byte("content"))
	if !errors.Is(err, ErrInvalidInput) || result.Outcome() != AdmissionTransitionPreMutationFailure || result.Inventory() != nil || result.Publication() != nil || !token.ValidFor(inventory) {
		t.Fatalf("result=%+v err=%v token=%v", result, err, token.ValidFor(inventory))
	}
	if f.writes != 0 || f.renames != 0 || f.fsyncs != 0 || f.fdatasyncs != 0 {
		t.Fatalf("mutation calls writes=%d renames=%d fsync=%d fdatasync=%d", f.writes, f.renames, f.fsyncs, f.fdatasyncs)
	}
	if err := lease.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestAdmissionPublishObjectPreScanCancellationPreservesAuthority(t *testing.T) {
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
	content := []byte("candidate-runtime-object")
	digest := sha256.Sum256(content)
	result, err := token.PublishObject(ctx, inventory, digest, content)
	if !errors.Is(err, context.Canceled) || result.Outcome() != AdmissionTransitionPreMutationFailure || !token.ValidFor(inventory) || !lease.Active() {
		t.Fatalf("result=%+v err=%v token=%v active=%v", result, err, token.ValidFor(inventory), lease.Active())
	}
	if f.writes != 0 || f.renames != 0 || f.fsyncs != 0 || f.fdatasyncs != 0 {
		t.Fatalf("mutation calls writes=%d renames=%d fsync=%d fdatasync=%d", f.writes, f.renames, f.fsyncs, f.fdatasyncs)
	}
	if err := lease.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestAdmissionPublishObjectUnknownRevokesEpoch(t *testing.T) {
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
	f.failFdatasyncAt = 1
	content := []byte("candidate-runtime-object")
	digest := sha256.Sum256(content)
	result, err := token.PublishObject(context.Background(), inventory, digest, content)
	if !errors.Is(err, ErrUnknown) || result.Outcome() != AdmissionTransitionUnknown || result.Inventory() != nil || result.Publication() != nil || lease.Active() || token.ValidFor(inventory) {
		t.Fatalf("result=%+v err=%v active=%v token=%v", result, err, lease.Active(), token.ValidFor(inventory))
	}
	if err := lease.Close(); err != nil || len(f.handles) != 0 {
		t.Fatalf("close err=%v handles=%d", err, len(f.handles))
	}
}

func TestAdmissionPublicationDurableResultCanInvalidate(t *testing.T) {
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
	content := []byte("candidate-runtime-object")
	digest := sha256.Sum256(content)
	result, err := token.PublishObject(context.Background(), inventory, digest, content)
	if err != nil {
		t.Fatal(err)
	}
	if err := result.Invalidate(); err != nil || lease.Active() || result.Publication().Matches(digest, uint64(len(content))) {
		t.Fatalf("invalidate err=%v active=%v bound=%v", err, lease.Active(), result.Publication().Matches(digest, uint64(len(content))))
	}
	if err := result.Invalidate(); !errors.Is(err, ErrLeaseInvalid) {
		t.Fatalf("second invalidate=%v", err)
	}
	if err := lease.Close(); err != nil || len(f.handles) != 0 {
		t.Fatalf("close err=%v handles=%d", err, len(f.handles))
	}
}
