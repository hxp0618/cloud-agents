package evidencefs

import (
	"context"
	"errors"
	"testing"
)

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
