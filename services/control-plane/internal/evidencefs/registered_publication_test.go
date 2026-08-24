package evidencefs

import (
	"context"
	"errors"
	"testing"
)

func registeredPublicationFixture(t *testing.T, data []byte) (*fakeBackend, *Store, *AdmissionLease, *AdmissionInventory, *AdmissionObjectView, [32]byte) {
	t.Helper()
	f := newFakeBackend()
	digest := f.addFinal(data)
	store := testStore(t, f)
	lease, inventory, err := store.AcquireAdmission(context.Background(), digestForTest(9))
	if err != nil {
		t.Fatal(err)
	}
	objects, err := inventory.Objects()
	if err != nil || len(objects) != 1 {
		t.Fatalf("objects=%d err=%v", len(objects), err)
	}
	return f, store, lease, inventory, objects[0], digest
}

func TestRegisteredPublicationSurvivesAdmissionClose(t *testing.T) {
	f, _, lease, _, view, digest := registeredPublicationFixture(t, []byte("registered-object"))
	first, err := view.RegisterPublication(context.Background())
	if err != nil || !first.Matches(digest, uint64(len("registered-object"))) || first.Identity() == nil {
		t.Fatalf("publication=%+v err=%v", first, err)
	}
	second, err := view.RegisterPublication(context.Background())
	if err != nil || !first.SameStore(second) || !first.SameObject(second) {
		t.Fatalf("second=%+v err=%v sameStore=%v sameObject=%v", second, err, first.SameStore(second), first.SameObject(second))
	}
	if err := lease.Close(); err != nil || len(f.handles) != 0 {
		t.Fatalf("close=%v handles=%d", err, len(f.handles))
	}
	if !first.Matches(digest, uint64(len("registered-object"))) || !first.SameObject(second) {
		t.Fatal("normal admission close revoked registered publication")
	}
	copyValue := *first
	if copyValue.Matches(digest, uint64(len("registered-object"))) || copyValue.Identity() != nil {
		t.Fatal("copied registered publication retained authority")
	}
	if (&RegisteredPublication{}).Matches(digest, uint64(len("registered-object"))) || (&RegisteredPublication{}).Identity() != nil {
		t.Fatal("literal registered publication retained authority")
	}
}

func TestRegisteredPublicationRejectsTemporaryZeroAndCanceled(t *testing.T) {
	t.Run("temporary", func(t *testing.T) {
		f := newFakeBackend()
		f.addTemp(1, []byte("temporary"))
		store := testStore(t, f)
		lease, inventory, err := store.AcquireAdmission(context.Background(), digestForTest(9))
		if err != nil {
			t.Fatal(err)
		}
		objects, err := inventory.TemporaryObjects()
		if err != nil || len(objects) != 1 {
			t.Fatalf("objects=%d err=%v", len(objects), err)
		}
		if publication, err := objects[0].RegisterPublication(context.Background()); publication != nil || !errors.Is(err, ErrInvalidInput) || !lease.Active() {
			t.Fatalf("publication=%v err=%v active=%v", publication, err, lease.Active())
		}
		_ = lease.Close()
	})
	t.Run("zero", func(t *testing.T) {
		_, _, lease, _, view, _ := registeredPublicationFixture(t, nil)
		if publication, err := view.RegisterPublication(context.Background()); publication != nil || !errors.Is(err, ErrInvalidInput) || !lease.Active() {
			t.Fatalf("publication=%v err=%v active=%v", publication, err, lease.Active())
		}
		_ = lease.Close()
	})
	t.Run("canceled", func(t *testing.T) {
		_, _, lease, _, view, _ := registeredPublicationFixture(t, []byte("cancel"))
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if publication, err := view.RegisterPublication(ctx); publication != nil || !errors.Is(err, context.Canceled) || !lease.Active() {
			t.Fatalf("publication=%v err=%v active=%v", publication, err, lease.Active())
		}
		_ = lease.Close()
	})
}

func TestRegisteredPublicationRejectsContentMutationAndForeignStore(t *testing.T) {
	f, _, lease, _, view, digest := registeredPublicationFixture(t, []byte("registered-object"))
	f.shaDir().children[view.file.name].data[0] ^= 1
	if publication, err := view.RegisterPublication(context.Background()); publication != nil || !errors.Is(err, ErrCorrupt) || lease.Active() {
		t.Fatalf("publication=%v err=%v active=%v", publication, err, lease.Active())
	}
	if err := lease.Close(); err != nil {
		t.Fatal(err)
	}

	_, _, firstLease, _, firstView, firstDigest := registeredPublicationFixture(t, []byte("same-content"))
	_, _, secondLease, _, secondView, secondDigest := registeredPublicationFixture(t, []byte("same-content"))
	first, firstErr := firstView.RegisterPublication(context.Background())
	second, secondErr := secondView.RegisterPublication(context.Background())
	if firstErr != nil || secondErr != nil || firstDigest != secondDigest || first.SameStore(second) || first.SameObject(second) || first.Matches(digest, uint64(len("registered-object"))) {
		t.Fatalf("firstErr=%v secondErr=%v sameStore=%v sameObject=%v", firstErr, secondErr, first.SameStore(second), first.SameObject(second))
	}
	_ = firstLease.Close()
	_ = secondLease.Close()
}

func TestRegisteredPublicationDigestRejectsFieldMutation(t *testing.T) {
	_, _, lease, _, view, _ := registeredPublicationFixture(t, []byte("registered-digest"))
	publication, err := view.RegisterPublication(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	faults := map[string]func(*RegisteredPublication){
		"digest":        func(value *RegisteredPublication) { value.digest[0]++ },
		"size":          func(value *RegisteredPublication) { value.size++ },
		"full set":      func(value *RegisteredPublication) { value.fullSet[0]++ },
		"revision":      func(value *RegisteredPublication) { value.revision++ },
		"view identity": func(value *RegisteredPublication) { value.viewIdentity[0]++ },
		"object":        func(value *RegisteredPublication) { value.identity.object.inode++ },
	}
	for name, mutate := range faults {
		t.Run(name, func(t *testing.T) {
			value := *publication
			value.self = &value
			identity := *publication.identity
			identity.self = &identity
			value.identity = &identity
			mutate(&value)
			if value.valid() {
				t.Fatal("mutated publication retained authority")
			}
		})
	}
	_ = lease.Close()
}
