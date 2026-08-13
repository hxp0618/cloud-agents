package evidencefs

import (
	"context"
	"crypto/sha256"
	"errors"
	"testing"
)

func admissionPublishedFixture(t *testing.T) (*fakeBackend, *AdmissionLease, *AdmissionInventory, *AdmissionMutationToken, *Publication, [32]byte, []byte) {
	t.Helper()
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
	published, err := token.PublishObject(context.Background(), inventory, digest, content)
	if err != nil || !published.ValidFor(published.Inventory()) {
		t.Fatalf("published=%+v err=%v", published, err)
	}
	nextToken, err := published.Inventory().MutationToken()
	if err != nil {
		t.Fatal(err)
	}
	return f, lease, published.Inventory(), nextToken, published.Publication(), digest, content
}

func TestAdmissionBindPublishedObjectAdvancesAndBinds(t *testing.T) {
	f, lease, inventory, token, publication, digest, content := admissionPublishedFixture(t)
	oldFullSet, err := inventory.FullSetDigest()
	if err != nil {
		t.Fatal(err)
	}
	result, err := token.BindPublishedObject(context.Background(), inventory, publication, digest, uint64(len(content)))
	if err != nil || result.Outcome() != AdmissionTransitionDurable || result.CandidateKind() != "content_binding" || result.CandidateDigest() != digest || result.CandidateSequence() != 0 || result.CandidateRevision() != 2 || result.PreviousRevision() != 1 || result.Size() != uint64(len(content)) {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	next := result.Inventory()
	if next == nil || result.Publication() != publication || !result.ValidFor(next) || !publication.Matches(digest, uint64(len(content))) {
		t.Fatalf("next=%v publication=%v valid=%v matches=%v", next, publication, result.ValidFor(next), publication.Matches(digest, uint64(len(content))))
	}
	newFullSet, err := next.FullSetDigest()
	if err != nil || newFullSet != oldFullSet {
		t.Fatalf("full old=%x new=%x err=%v", oldFullSet, newFullSet, err)
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
	if !publication.Matches(digest, uint64(len(content))) {
		t.Fatal("bound publication did not survive lease close")
	}
}

func TestAdmissionBindPublishedObjectMismatchPreservesTransientAuthority(t *testing.T) {
	for name, invoke := range map[string]func(*AdmissionMutationToken, *AdmissionInventory, *Publication, [32]byte, uint64) (AdmissionBindingTransitionResult, error){
		"digest": func(token *AdmissionMutationToken, inventory *AdmissionInventory, publication *Publication, digest [32]byte, size uint64) (AdmissionBindingTransitionResult, error) {
			digest[0]++
			return token.BindPublishedObject(context.Background(), inventory, publication, digest, size)
		},
		"copy": func(token *AdmissionMutationToken, inventory *AdmissionInventory, publication *Publication, digest [32]byte, size uint64) (AdmissionBindingTransitionResult, error) {
			copyPublication := *publication
			return token.BindPublishedObject(context.Background(), inventory, &copyPublication, digest, size)
		},
	} {
		t.Run(name, func(t *testing.T) {
			f, lease, inventory, token, publication, digest, content := admissionPublishedFixture(t)
			result, err := invoke(token, inventory, publication, digest, uint64(len(content)))
			if !errors.Is(err, ErrInvalidInput) || result.Outcome() != AdmissionTransitionPreMutationFailure || result.Inventory() != nil || result.Publication() != nil || !token.ValidFor(inventory) {
				t.Fatalf("result=%+v err=%v token=%v", result, err, token.ValidFor(inventory))
			}
			if publication.Matches(digest, uint64(len(content))) {
				t.Fatal("mismatch bound transient publication")
			}
			if err := lease.rootLease.BindPublication(publication, digest, uint64(len(content))); err != nil {
				t.Fatalf("original transient publication was consumed: %v", err)
			}
			if err := lease.Close(); err != nil || len(f.handles) != 0 {
				t.Fatalf("close err=%v handles=%d", err, len(f.handles))
			}
		})
	}
}

func TestAdmissionBindPublishedObjectTerminalFailureRevokesEpoch(t *testing.T) {
	f, lease, inventory, token, publication, digest, content := admissionPublishedFixture(t)
	f.failCloseAt["sha256"] = f.closeNameCounts["sha256"] + 1
	result, err := token.BindPublishedObject(context.Background(), inventory, publication, digest, uint64(len(content)))
	if err == nil || result.Outcome() != AdmissionTransitionPreMutationFailure || lease.Active() || token.ValidFor(inventory) || publication.Matches(digest, uint64(len(content))) {
		t.Fatalf("result=%+v err=%v active=%v token=%v matches=%v", result, err, lease.Active(), token.ValidFor(inventory), publication.Matches(digest, uint64(len(content))))
	}
	if err := lease.Close(); err == nil {
		// Store is poisoned by the close ambiguity, but genuine lease cleanup is
		// still attempted. Depending on which descriptor fault fired, Close may
		// itself be clean or report the poisoned root close.
	}
	if len(f.handles) != 0 {
		t.Fatalf("fd leak=%v", f.handles)
	}
}

func TestAdmissionBindingDurableResultCanInvalidatePairOnly(t *testing.T) {
	f, lease, inventory, token, publication, digest, content := admissionPublishedFixture(t)
	result, err := token.BindPublishedObject(context.Background(), inventory, publication, digest, uint64(len(content)))
	if err != nil {
		t.Fatal(err)
	}
	if err := result.Invalidate(); err != nil || lease.Active() || !publication.Matches(digest, uint64(len(content))) {
		t.Fatalf("invalidate err=%v active=%v publication=%v", err, lease.Active(), publication.Matches(digest, uint64(len(content))))
	}
	if err := result.Invalidate(); !errors.Is(err, ErrLeaseInvalid) {
		t.Fatalf("second invalidate=%v", err)
	}
	if err := lease.Close(); err != nil || len(f.handles) != 0 {
		t.Fatalf("close err=%v handles=%d", err, len(f.handles))
	}
}
