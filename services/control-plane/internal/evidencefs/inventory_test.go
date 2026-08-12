package evidencefs

import (
	"context"
	"errors"
	"testing"
)

func TestAdmissionInventoryTargetBindingAndLifecycle(t *testing.T) {
	f := newFakeBackend()
	present := digestForTest(1)
	other := digestForTest(2)
	addAdmissionLineage(f, present, 0, 0)
	addAdmissionLineage(f, other, 0, 0)
	store := testStore(t, f)
	lease, inventory, err := store.AcquireAdmission(context.Background(), present)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := inventory.Target(); err != nil || got != present || got == other {
		t.Fatalf("target=%x err=%v", got, err)
	}
	copyInventory := *inventory
	if _, err := copyInventory.Target(); !errors.Is(err, ErrLeaseInvalid) {
		t.Fatalf("copy target err=%v", err)
	}
	if err := copyInventory.Revalidate(context.Background()); !errors.Is(err, ErrLeaseInvalid) {
		t.Fatalf("copy revalidate err=%v", err)
	}
	var zero AdmissionInventory
	if _, err := zero.Target(); !errors.Is(err, ErrLeaseInvalid) {
		t.Fatalf("zero target err=%v", err)
	}
	if err := zero.Revalidate(context.Background()); !errors.Is(err, ErrLeaseInvalid) {
		t.Fatalf("zero revalidate err=%v", err)
	}
	if err := lease.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := inventory.Target(); !errors.Is(err, ErrLeaseInvalid) {
		t.Fatalf("closed target err=%v", err)
	}
	if err := inventory.Revalidate(context.Background()); !errors.Is(err, ErrLeaseInvalid) {
		t.Fatalf("closed revalidate err=%v", err)
	}

	f = newFakeBackend()
	store = testStore(t, f)
	absent := digestForTest(9)
	lease, inventory, err = store.AcquireAdmission(context.Background(), absent)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := inventory.Target(); err != nil || got != absent {
		t.Fatalf("absent target=%x err=%v", got, err)
	}
	if err := inventory.Revalidate(context.Background()); err != nil {
		t.Fatalf("absent revalidate: %v", err)
	}
	_ = lease.Close()
}

func TestAdmissionInventoryRevalidateStableAndDetectsDrift(t *testing.T) {
	t.Run("stable-does-not-advance-authority", func(t *testing.T) {
		f := newFakeBackend()
		objectDigest := f.addFinal([]byte("object"))
		addAdmissionLineage(f, digestForTest(1), 1, 1)
		store := testStore(t, f)
		lease, inventory, err := store.AcquireAdmission(context.Background(), digestForTest(1))
		if err != nil {
			t.Fatal(err)
		}
		baselineHandles := len(f.handles)
		full, _ := inventory.FullSetDigest()
		revision, _ := inventory.Revision()
		target, _ := inventory.Target()
		lineages, _ := inventory.LineageIDs()
		objects, _ := inventory.Objects()
		if len(objects) != 1 {
			t.Fatalf("objects=%d", len(objects))
		}
		for attempt := 0; attempt < 3; attempt++ {
			if err := inventory.Revalidate(context.Background()); err != nil {
				t.Fatal(err)
			}
			if len(f.handles) != baselineHandles {
				t.Fatalf("attempt=%d handles=%d baseline=%d", attempt, len(f.handles), baselineHandles)
			}
		}
		fullAfter, _ := inventory.FullSetDigest()
		revisionAfter, _ := inventory.Revision()
		targetAfter, _ := inventory.Target()
		lineagesAfter, _ := inventory.LineageIDs()
		objectsAfter, _ := inventory.Objects()
		if fullAfter != full || revision != 0 || revisionAfter != revision || targetAfter != target || len(lineagesAfter) != len(lineages) || objectsAfter[0] != objects[0] {
			t.Fatal("revalidation changed inventory authority")
		}
		if digest, _ := objectsAfter[0].Digest(); digest != objectDigest {
			t.Fatalf("object digest=%x", digest)
		}
		_ = lease.Close()
	})

	tests := map[string]func(*fakeBackend, *fakeNode){
		"index-content": func(_ *fakeBackend, lineage *fakeNode) { lineage.children["index.caj"].data[0] ^= 1 },
		"segment-content": func(_ *fakeBackend, lineage *fakeNode) {
			for name, journal := range lineage.children {
				if finalNamePattern.MatchString(name) {
					journal.children[admissionSegmentName(0)].data[0] ^= 1
				}
			}
		},
		"object-content": func(f *fakeBackend, _ *fakeNode) {
			for _, object := range f.shaDir().children {
				object.data[0] ^= 1
			}
		},
		"entry-add": func(f *fakeBackend, _ *fakeNode) { addAdmissionLineage(f, digestForTest(2), 0, 0) },
		"entry-remove": func(f *fakeBackend, _ *fakeNode) {
			delete(f.root.children["lineages"].children, fmtDigest(digestForTest(1)))
		},
		"directory-metadata": func(f *fakeBackend, _ *fakeNode) { f.root.children["lineages"].stat.nlink++ },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			f := newFakeBackend()
			f.addFinal([]byte("object"))
			lineage := addAdmissionLineage(f, digestForTest(1), 1, 1)
			store := testStore(t, f)
			lease, inventory, err := store.AcquireAdmission(context.Background(), digestForTest(1))
			if err != nil {
				t.Fatal(err)
			}
			baseline := len(f.handles)
			mutate(f, lineage)
			if err := inventory.Revalidate(context.Background()); err == nil {
				t.Fatal("drift passed revalidation")
			}
			if len(f.handles) != baseline {
				t.Fatalf("handles=%d baseline=%d", len(f.handles), baseline)
			}
			assertRevokedInventory(t, lease, inventory)
			if err := lease.Close(); err != nil || len(f.handles) != 0 {
				t.Fatalf("cleanup err=%v handles=%d", err, len(f.handles))
			}
		})
	}
}

func assertRevokedInventory(t *testing.T, lease *AdmissionLease, inventory *AdmissionInventory) {
	t.Helper()
	if lease.Active() {
		t.Fatal("failed revalidation retained active lease")
	}
	if _, err := inventory.Target(); !errors.Is(err, ErrLeaseInvalid) {
		t.Fatalf("target after revoke err=%v", err)
	}
	if _, err := inventory.LineageIDs(); !errors.Is(err, ErrLeaseInvalid) {
		t.Fatalf("views after revoke err=%v", err)
	}
}

func fmtDigest(value [32]byte) string {
	const digits = "0123456789abcdef"
	result := make([]byte, 64)
	for index, b := range value {
		result[index*2] = digits[b>>4]
		result[index*2+1] = digits[b&15]
	}
	return string(result)
}

func TestAdmissionInventoryRevalidateFaultsAndCancellation(t *testing.T) {
	t.Run("scan-close-revokes-and-poisons", func(t *testing.T) {
		f := newFakeBackend()
		addAdmissionLineage(f, digestForTest(1), 0, 0)
		store := testStore(t, f)
		lease, inventory, err := store.AcquireAdmission(context.Background(), digestForTest(1))
		if err != nil {
			t.Fatal(err)
		}
		f.failCloseNames["sha256"] = true
		if err := inventory.Revalidate(context.Background()); !errors.Is(err, ErrFilesystem) || store.usable() {
			t.Fatalf("err=%v usable=%v", err, store.usable())
		}
		assertRevokedInventory(t, lease, inventory)
		if _, err := store.AcquireRoot(context.Background()); !errors.Is(err, ErrFilesystem) {
			t.Fatalf("poisoned root acquired: %v", err)
		}
		if nextLease, nextInventory, err := store.AcquireAdmission(context.Background(), digestForTest(1)); nextLease != nil || nextInventory != nil || !errors.Is(err, ErrFilesystem) {
			t.Fatalf("lease=%v inventory=%v err=%v", nextLease, nextInventory, err)
		}
		_ = lease.Close()
	})

	t.Run("read-fault", func(t *testing.T) {
		f := newFakeBackend()
		addAdmissionLineage(f, digestForTest(1), 0, 0)
		store := testStore(t, f)
		lease, inventory, err := store.AcquireAdmission(context.Background(), digestForTest(1))
		if err != nil {
			t.Fatal(err)
		}
		f.failOpenNames["index.caj"] = errors.New("read open")
		if err := inventory.Revalidate(context.Background()); !errors.Is(err, ErrFilesystem) {
			t.Fatalf("err=%v", err)
		}
		assertRevokedInventory(t, lease, inventory)
		if err := lease.Close(); err != nil || len(f.handles) != 0 {
			t.Fatalf("cleanup err=%v handles=%d", err, len(f.handles))
		}
	})

	t.Run("close-fault", func(t *testing.T) {
		f := newFakeBackend()
		addAdmissionLineage(f, digestForTest(1), 0, 0)
		store := testStore(t, f)
		lease, inventory, err := store.AcquireAdmission(context.Background(), digestForTest(1))
		if err != nil {
			t.Fatal(err)
		}
		f.failCloseNames["index.caj"] = true
		if err := inventory.Revalidate(context.Background()); !errors.Is(err, ErrFilesystem) || store.usable() {
			t.Fatalf("err=%v usable=%v", err, store.usable())
		}
		assertRevokedInventory(t, lease, inventory)
		_ = lease.Close()
	})

	t.Run("pre-cancel", func(t *testing.T) {
		f := newFakeBackend()
		store := testStore(t, f)
		lease, inventory, err := store.AcquireAdmission(context.Background(), digestForTest(1))
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if err := inventory.Revalidate(ctx); !errors.Is(err, context.Canceled) {
			t.Fatalf("err=%v", err)
		}
		if !lease.Active() {
			t.Fatal("clean pre-cancel revoked lease")
		}
		_ = lease.Close()
	})

	for _, phase := range []string{"mid", "post"} {
		t.Run(phase+"-cancel", func(t *testing.T) {
			f := newFakeBackend()
			lineage := addAdmissionLineage(f, digestForTest(1), 0, 0)
			store := testStore(t, f)
			lease, inventory, err := store.AcquireAdmission(context.Background(), digestForTest(1))
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			reads := 0
			shaReads := 0
			if phase == "mid" {
				f.onPread = func(_ *fakeBackend, node *fakeNode, _ int) {
					if node == lineage.children["index.caj"] {
						reads++
						cancel()
					}
				}
			} else {
				f.onReadDir = func(_ *fakeBackend, node *fakeNode, _ int) {
					if node.name == "sha256" {
						shaReads++
						// The first two sha256 reads belong to the before
						// discovery+Scan boundary; the third begins after.
						if shaReads == 3 {
							cancel()
						}
					}
				}
			}
			if err := inventory.Revalidate(ctx); !errors.Is(err, context.Canceled) {
				t.Fatalf("phase=%s reads=%d shaReads=%d err=%v", phase, reads, shaReads, err)
			}
			if !lease.Active() {
				t.Fatalf("clean %s cancel revoked lease", phase)
			}
			_ = lease.Close()
		})
	}
}

func TestAdmissionInventoryRevalidateDetectsPreexistingAndCurrentReadMutation(t *testing.T) {
	t.Run("preexisting-early-index", func(t *testing.T) {
		f := newFakeBackend()
		lineage := addAdmissionLineage(f, digestForTest(1), 0, 0)
		store := testStore(t, f)
		lease, inventory, err := store.AcquireAdmission(context.Background(), digestForTest(1))
		if err != nil {
			t.Fatal(err)
		}
		lineage.children["index.caj"].data[0] ^= 1
		if err := inventory.Revalidate(context.Background()); err == nil {
			t.Fatal("preexisting same-size index mutation passed")
		}
		assertRevokedInventory(t, lease, inventory)
		_ = lease.Close()
	})

	t.Run("current-fd", func(t *testing.T) {
		f := newFakeBackend()
		lineage := addAdmissionLineage(f, digestForTest(1), 0, 0)
		index := lineage.children["index.caj"]
		store := testStore(t, f)
		lease, inventory, err := store.AcquireAdmission(context.Background(), digestForTest(1))
		if err != nil {
			t.Fatal(err)
		}
		f.onPread = func(_ *fakeBackend, node *fakeNode, _ int) {
			if node == index {
				index.data[0] ^= 1
			}
		}
		if err := inventory.Revalidate(context.Background()); err == nil {
			t.Fatal("current-fd same-size mutation passed")
		}
		assertRevokedInventory(t, lease, inventory)
		_ = lease.Close()
	})
}

func TestAdmissionInventoryRevalidateRejectsAuthorityGraphTamper(t *testing.T) {
	tests := map[string]func(*AdmissionInventory){
		"inventory-target":   func(i *AdmissionInventory) { i.target = digestForTest(99) },
		"inventory-full-set": func(i *AdmissionInventory) { i.fullSet[0] ^= 1 },
		"lineage-id":         func(i *AdmissionInventory) { i.lineages[0].id = digestForTest(99) },
		"lineage-name":       func(i *AdmissionInventory) { i.lineages[0].name = "forged" },
		"lineage-order":      func(i *AdmissionInventory) { i.lineages[0], i.lineages[1] = i.lineages[1], i.lineages[0] },
		"journal-id":         func(i *AdmissionInventory) { i.lineages[0].journals[0].id = digestForTest(99) },
		"journal-name":       func(i *AdmissionInventory) { i.lineages[0].journals[0].name = "forged" },
		"file-digest":        func(i *AdmissionInventory) { i.lineages[0].index.digest[0] ^= 1 },
		"file-identity":      func(i *AdmissionInventory) { i.lineages[0].index.identity[0] ^= 1 },
		"file-stat":          func(i *AdmissionInventory) { i.lineages[0].index.stat.size++ },
		"file-parent":        func(i *AdmissionInventory) { i.lineages[0].index.parents[0].stat.nlink++ },
		"object-digest":      func(i *AdmissionInventory) { i.objects[0].digest[0] ^= 1 },
		"object-temporary":   func(i *AdmissionInventory) { i.objects[0].temporary = !i.objects[0].temporary },
		"slot-baseline":      func(i *AdmissionInventory) { i.slot.discovery.lineages[0].index.size++ },
		"slot-expectation": func(i *AdmissionInventory) {
			expected := i.slot.files[i.lineages[0].index]
			expected.digest[0] ^= 1
			i.slot.files[i.lineages[0].index] = expected
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			f := newFakeBackend()
			f.addFinal([]byte("object"))
			addAdmissionLineage(f, digestForTest(1), 1, 1)
			addAdmissionLineage(f, digestForTest(2), 1, 1)
			store := testStore(t, f)
			lease, inventory, err := store.AcquireAdmission(context.Background(), digestForTest(1))
			if err != nil {
				t.Fatal(err)
			}
			mutate(inventory)
			if err := inventory.Revalidate(context.Background()); !errors.Is(err, ErrLeaseInvalid) {
				t.Fatalf("err=%v", err)
			}
			assertRevokedInventory(t, lease, inventory)
			if err := lease.Close(); err != nil || len(f.handles) != 0 {
				t.Fatalf("cleanup err=%v handles=%d", err, len(f.handles))
			}
		})
	}
}

func TestAdmissionInventoryObjectsAndFullSetChange(t *testing.T) {
	f := newFakeBackend()
	content := []byte("registered-object")
	digest := f.addFinal(content)
	f.addTemp(1, []byte("temporary"))
	addAdmissionLineage(f, digestForTest(1), 1, 1)
	store := testStore(t, f)
	lease, inventory, err := store.AcquireAdmission(context.Background(), digestForTest(9))
	if err != nil {
		t.Fatal(err)
	}
	objects, err := inventory.Objects()
	if err != nil || len(objects) != 1 {
		t.Fatalf("objects=%v err=%v", objects, err)
	}
	gotDigest, err := objects[0].Digest()
	if err != nil || gotDigest != digest {
		t.Fatalf("digest=%x err=%v", gotDigest, err)
	}
	bytes, err := objects[0].ReadAll(context.Background())
	if err != nil || string(bytes) != string(content) {
		t.Fatalf("bytes=%q err=%v", bytes, err)
	}
	temps, err := inventory.TemporaryObjects()
	if err != nil || len(temps) != 1 {
		t.Fatalf("temps=%v err=%v", temps, err)
	}
	if temporary, err := temps[0].Temporary(); err != nil || !temporary {
		t.Fatalf("temporary=%v err=%v", temporary, err)
	}
	full, err := inventory.FullSetDigest()
	if err != nil || full == ([32]byte{}) {
		t.Fatalf("full=%x err=%v", full, err)
	}
	_ = lease.Close()
}

func TestAdmissionViewsRejectCopyZeroCrossLeaseAndClosed(t *testing.T) {
	f := newFakeBackend()
	addAdmissionLineage(f, digestForTest(1), 1, 1)
	store := testStore(t, f)
	lease, inventory, err := store.AcquireAdmission(context.Background(), digestForTest(9))
	if err != nil {
		t.Fatal(err)
	}
	lineage, _ := inventory.Lineage(digestForTest(1))
	index, _ := lineage.Index()
	fact, _ := inventory.TargetAbsent()

	copyInventory := *inventory
	if _, err := copyInventory.LineageIDs(); !errors.Is(err, ErrLeaseInvalid) {
		t.Fatalf("copied inventory err=%v", err)
	}
	copyLineage := *lineage
	if _, err := copyLineage.ID(); !errors.Is(err, ErrLeaseInvalid) {
		t.Fatalf("copied lineage err=%v", err)
	}
	copyIndex := *index
	if _, err := copyIndex.ReadAll(context.Background()); !errors.Is(err, ErrLeaseInvalid) {
		t.Fatalf("copied index err=%v", err)
	}
	copyFact := *fact
	if _, err := copyFact.Target(); !errors.Is(err, ErrLeaseInvalid) {
		t.Fatalf("copied fact err=%v", err)
	}
	var zero AdmissionInventory
	if _, err := zero.FullSetDigest(); !errors.Is(err, ErrLeaseInvalid) {
		t.Fatalf("zero err=%v", err)
	}

	f2 := newFakeBackend()
	addAdmissionLineage(f2, digestForTest(1), 0, 0)
	store2 := testStore(t, f2)
	lease2, inventory2, err := store2.AcquireAdmission(context.Background(), digestForTest(9))
	if err != nil {
		t.Fatal(err)
	}
	foreign := *inventory
	foreign.lease = lease2
	foreign.store = store2
	foreign.epoch = lease2.epoch
	foreign.slot = inventory2.slot
	if _, err := foreign.LineageIDs(); !errors.Is(err, ErrLeaseInvalid) {
		t.Fatalf("foreign err=%v", err)
	}
	_ = lease2.Close()
	_ = lease.Close()
	if _, err := index.ReadAll(context.Background()); !errors.Is(err, ErrLeaseInvalid) {
		t.Fatalf("closed index err=%v", err)
	}
}

func TestAdmissionReadDetectsSameSizeMutationAndDirectorySwap(t *testing.T) {
	t.Run("same-size", func(t *testing.T) {
		f := newFakeBackend()
		lineageNode := addAdmissionLineage(f, digestForTest(1), 0, 0)
		store := testStore(t, f)
		lease, inventory, err := store.AcquireAdmission(context.Background(), digestForTest(1))
		if err != nil {
			t.Fatal(err)
		}
		index, _ := func() (*AdmissionFileView, error) {
			lineage, _ := inventory.Lineage(digestForTest(1))
			return lineage.Index()
		}()
		node := lineageNode.children["index.caj"]
		node.data[0] ^= 1
		if _, err := index.ReadAll(context.Background()); !errors.Is(err, ErrCorrupt) {
			t.Fatalf("same-size err=%v", err)
		}
		_ = lease.Close()
	})

	t.Run("directory-swap", func(t *testing.T) {
		f := newFakeBackend()
		addAdmissionLineage(f, digestForTest(1), 0, 0)
		store := testStore(t, f)
		lease, inventory, err := store.AcquireAdmission(context.Background(), digestForTest(1))
		if err != nil {
			t.Fatal(err)
		}
		lineage, _ := inventory.Lineage(digestForTest(1))
		index, _ := lineage.Index()
		old := f.root.children["lineages"]
		replacement := f.directory("lineages")
		replacement.children = old.children
		f.root.children["lineages"] = replacement
		if _, err := index.ReadAll(context.Background()); !errors.Is(err, ErrCorrupt) {
			t.Fatalf("directory swap err=%v", err)
		}
		_ = lease.Close()
	})
}

func TestAdmissionReadReturnsOwnedBytesAndDetectsInodeSwap(t *testing.T) {
	f := newFakeBackend()
	lineageNode := addAdmissionLineage(f, digestForTest(1), 0, 0)
	store := testStore(t, f)
	lease, inventory, err := store.AcquireAdmission(context.Background(), digestForTest(1))
	if err != nil {
		t.Fatal(err)
	}
	lineage, _ := inventory.Lineage(digestForTest(1))
	index, _ := lineage.Index()
	first, err := index.ReadAll(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	first[0] ^= 1
	second, err := index.ReadAll(context.Background())
	if err != nil || first[0] == second[0] {
		t.Fatalf("returned bytes alias storage first=%q second=%q err=%v", first, second, err)
	}
	original := lineageNode.children["index.caj"]
	lineageNode.children["index.caj"] = f.regular("index.caj", original.data)
	if _, err := index.ReadAll(context.Background()); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("inode swap err=%v", err)
	}
	_ = lease.Close()
}

func TestAdmissionAccessorMismatchCloseFailureAndRepeatedFDStability(t *testing.T) {
	f := newFakeBackend()
	addAdmissionLineage(f, digestForTest(1), 0, 0)
	store := testStore(t, f)
	lease, inventory, err := store.AcquireAdmission(context.Background(), digestForTest(1))
	if err != nil {
		t.Fatal(err)
	}
	lineage, _ := inventory.Lineage(digestForTest(1))
	index, _ := lineage.Index()
	baseline := len(f.handles)
	for attempt := 0; attempt < 20; attempt++ {
		if _, err := index.ReadAll(context.Background()); err != nil {
			t.Fatal(err)
		}
		if len(f.handles) != baseline {
			t.Fatalf("attempt=%d handles=%d baseline=%d", attempt, len(f.handles), baseline)
		}
	}
	// Same-inode metadata mismatch occurs after open. Its descriptor close also
	// fails; the close failure must poison and dominate returned authority.
	lineages := f.root.children["lineages"]
	lineages.stat.mode = 0o600
	f.failCloseNames["lineages"] = true
	if bytes, err := index.ReadAll(context.Background()); bytes != nil || !errors.Is(err, ErrFilesystem) || store.usable() {
		t.Fatalf("bytes=%v err=%v usable=%v handles=%d", bytes, err, store.usable(), len(f.handles))
	}
	_ = lease.Close()
}

func TestAdmissionLimitsRejectBeforePayloadRead(t *testing.T) {
	tests := map[string]func(*fakeBackend){
		"index-plus-one": func(f *fakeBackend) {
			lineage := addAdmissionLineage(f, digestForTest(1), 0, 0)
			lineage.children["index.caj"].stat.size = maximumAdmissionIndexBytes + 1
		},
		"segment-plus-one": func(f *fakeBackend) {
			lineage := addAdmissionLineage(f, digestForTest(1), 1, 1)
			for name, journal := range lineage.children {
				if finalNamePattern.MatchString(name) {
					journal.children[admissionSegmentName(0)].stat.size = maximumAdmissionSegmentBytes + 1
				}
			}
		},
		"journal-count-plus-one": func(f *fakeBackend) {
			for value := 0; value < maximumAdmissionJournals+1; value++ {
				addAdmissionLineage(f, digestForTest(value), 1, 1)
			}
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			f := newFakeBackend()
			mutate(f)
			store := testStore(t, f)
			before := f.preads
			lease, inventory, err := store.AcquireAdmission(context.Background(), digestForTest(99))
			if lease != nil || inventory != nil || !errors.Is(err, ErrLimit) || f.preads != before {
				t.Fatalf("lease=%v inventory=%v err=%v preads=%d", lease, inventory, err, f.preads)
			}
		})
	}
}

func TestAdmissionTerminalPassRejectsMutationAndEntryChange(t *testing.T) {
	t.Run("same-inode-same-size-content", func(t *testing.T) {
		f := newFakeBackend()
		lineage := addAdmissionLineage(f, digestForTest(1), 0, 0)
		index := lineage.children["index.caj"]
		matches := 0
		f.onPread = func(_ *fakeBackend, node *fakeNode, _ int) {
			if node == index {
				matches++
				if matches == 2 {
					index.data[0] ^= 1
				}
			}
		}
		store := testStore(t, f)
		if lease, inventory, err := store.AcquireAdmission(context.Background(), digestForTest(1)); lease != nil || inventory != nil || err == nil {
			t.Fatalf("lease=%v inventory=%v err=%v", lease, inventory, err)
		}
	})

	t.Run("terminal-entry-add", func(t *testing.T) {
		f := newFakeBackend()
		addAdmissionLineage(f, digestForTest(1), 0, 0)
		matches := 0
		f.onPread = func(f *fakeBackend, node *fakeNode, _ int) {
			if node.name == "index.caj" {
				matches++
			}
			if matches == 2 {
				matches++
				addAdmissionLineage(f, digestForTest(2), 0, 0)
			}
		}
		store := testStore(t, f)
		if lease, inventory, err := store.AcquireAdmission(context.Background(), digestForTest(1)); lease != nil || inventory != nil || err == nil {
			t.Fatalf("lease=%v inventory=%v err=%v", lease, inventory, err)
		}
	})
}

func TestAdmissionTerminalRejectsSameInodeDirectoryMetadataDrift(t *testing.T) {
	for _, mutate := range []func(*fakeNode){
		func(node *fakeNode) { node.stat.mode = 0o600 },
		func(node *fakeNode) { node.stat.uid++ },
		func(node *fakeNode) { node.stat.nlink++ },
	} {
		f := newFakeBackend()
		addAdmissionLineage(f, digestForTest(1), 0, 0)
		lineages := f.root.children["lineages"]
		matches := 0
		f.onPread = func(_ *fakeBackend, node *fakeNode, _ int) {
			if node.name == "index.caj" {
				matches++
			}
			if matches == 2 {
				matches++
				mutate(lineages)
			}
		}
		store := testStore(t, f)
		if lease, inventory, err := store.AcquireAdmission(context.Background(), digestForTest(1)); lease != nil || inventory != nil || err == nil {
			t.Fatalf("lease=%v inventory=%v err=%v", lease, inventory, err)
		}
	}
}

func TestAdmissionTerminalCloseFailurePoisonsAndMintsNoAuthority(t *testing.T) {
	for _, closeName := range []string{"root", "lineages", "index.caj"} {
		t.Run(closeName, func(t *testing.T) {
			f := newFakeBackend()
			addAdmissionLineage(f, digestForTest(1), 0, 0)
			matches := 0
			f.onPread = func(f *fakeBackend, node *fakeNode, _ int) {
				if node.name == "index.caj" {
					matches++
				}
				if matches == 2 {
					matches++
					f.failCloseNames[closeName] = true
				}
			}
			store := testStore(t, f)
			lease, inventory, err := store.AcquireAdmission(context.Background(), digestForTest(1))
			if lease != nil || inventory != nil || !errors.Is(err, ErrFilesystem) || store.usable() {
				t.Fatalf("lease=%v inventory=%v err=%v usable=%v", lease, inventory, err, store.usable())
			}
		})
	}
}

func TestAdmissionSlotRejectsSelfResetAndFieldTamper(t *testing.T) {
	f := newFakeBackend()
	f.addFinal([]byte("object"))
	addAdmissionLineage(f, digestForTest(1), 1, 1)
	store := testStore(t, f)
	lease, inventory, err := store.AcquireAdmission(context.Background(), digestForTest(9))
	if err != nil {
		t.Fatal(err)
	}
	lineage, _ := inventory.Lineage(digestForTest(1))
	journalViews, _ := lineage.Journals()
	index, _ := lineage.Index()
	lineageCopy := *lineage
	lineageCopy.self = &lineageCopy
	lineageCopy.name = "forged"
	if _, err := lineageCopy.ID(); !errors.Is(err, ErrLeaseInvalid) {
		t.Fatalf("lineage tamper=%v", err)
	}
	journalCopy := *journalViews[0]
	journalCopy.self = &journalCopy
	journalCopy.lineage = "forged"
	if _, err := journalCopy.ID(); !errors.Is(err, ErrLeaseInvalid) {
		t.Fatalf("journal tamper=%v", err)
	}
	fileCopy := *index
	fileCopy.self = &fileCopy
	fileCopy.ordinal++
	if _, err := fileCopy.Size(); !errors.Is(err, ErrLeaseInvalid) {
		t.Fatalf("file tamper=%v", err)
	}
	objects, _ := inventory.Objects()
	objectCopy := *objects[0]
	objectCopy.self = &objectCopy
	objectCopy.temporary = true
	if _, err := objectCopy.Size(); !errors.Is(err, ErrLeaseInvalid) {
		t.Fatalf("object tamper=%v", err)
	}
	lineage.index = journalViews[0].segments[0]
	if _, err := lineage.Index(); !errors.Is(err, ErrLeaseInvalid) {
		t.Fatalf("parent relation tamper=%v", err)
	}
	_ = lease.Close()
}

func TestAdmissionIndexAndSegmentExactLimitsPassWithoutAllocatingDeclaredBytes(t *testing.T) {
	f := newFakeBackend()
	lineage := addAdmissionLineage(f, digestForTest(1), 1, 1)
	index := lineage.children["index.caj"]
	index.stat.size = maximumAdmissionIndexBytes
	index.data = make([]byte, maximumAdmissionIndexBytes)
	for name, journal := range lineage.children {
		if finalNamePattern.MatchString(name) {
			segment := journal.children[admissionSegmentName(0)]
			segment.stat.size = maximumAdmissionSegmentBytes
			segment.data = make([]byte, maximumAdmissionSegmentBytes)
		}
	}
	store := testStore(t, f)
	lease, inventory, err := store.AcquireAdmission(context.Background(), digestForTest(1))
	if err != nil || lease == nil || inventory == nil {
		t.Fatalf("lease=%v inventory=%v err=%v", lease, inventory, err)
	}
	_ = lease.Close()
}

func TestAdmissionJournalAggregateLimitExactAndPlusOne(t *testing.T) {
	makeRoot := func(plusOne bool) (*fakeBackend, *fakeNode) {
		f := newFakeBackend()
		lineage := addAdmissionLineage(f, digestForTest(1), 16, 16)
		for name, journal := range lineage.children {
			if !finalNamePattern.MatchString(name) {
				continue
			}
			for segmentName, segment := range journal.children {
				if segmentName == "writer.lock" {
					continue
				}
				segment.stat.size = maximumAdmissionSegmentBytes
				segment.data = nil
				segment.virtualZero = true
			}
		}
		if plusOne {
			for name, journal := range lineage.children {
				if finalNamePattern.MatchString(name) {
					journal.children[admissionSegmentName(0)].stat.size++
					return f, lineage
				}
			}
		}
		return f, lineage
	}
	f, _ := makeRoot(false)
	store := testStore(t, f)
	lease, inventory, err := store.AcquireAdmission(context.Background(), digestForTest(1))
	if err != nil || lease == nil || inventory == nil {
		t.Fatalf("exact lease=%v inventory=%v err=%v", lease, inventory, err)
	}
	_ = lease.Close()

	f, _ = makeRoot(true)
	store = testStore(t, f)
	if lease, inventory, err := store.AcquireAdmission(context.Background(), digestForTest(1)); lease != nil || inventory != nil || !errors.Is(err, ErrLimit) {
		t.Fatalf("plus-one lease=%v inventory=%v err=%v", lease, inventory, err)
	}
}
