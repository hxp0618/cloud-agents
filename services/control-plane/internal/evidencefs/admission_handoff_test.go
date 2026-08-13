package evidencefs

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

func admissionWithGenerationForHandoff(t *testing.T, lineageCount int) (*fakeBackend, *Store, *AdmissionLease, *AdmissionInventory, *AdmissionMutationToken, [32]byte, [32]byte) {
	t.Helper()
	f := newFakeBackend()
	target := digestForTest(9)
	for value := 1; value <= lineageCount; value++ {
		id := digestForTest(value)
		if value == lineageCount {
			id = target
		}
		addAdmissionLineage(f, id, 0, 0)
	}
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
	created, err := token.CreateGenerationHeader(context.Background(), inventory, journal, []byte("canonical-generation-header"))
	if err != nil || !created.ValidFor(created.Inventory()) {
		t.Fatalf("created=%+v err=%v", created, err)
	}
	inventory = created.Inventory()
	token, err = inventory.MutationToken()
	if err != nil {
		t.Fatal(err)
	}
	return f, store, lease, inventory, token, target, journal
}

func TestAdmissionHandoffRetainsOnlyTargetAndGenerationLocks(t *testing.T) {
	f, store, admission, inventory, token, target, journal := admissionWithGenerationForHandoff(t, 3)
	if got, want := len(f.handles), 2+3+1; got != want {
		t.Fatalf("pre-handoff handles=%d want=%d", got, want)
	}
	generation, err := token.HandoffGeneration(context.Background(), inventory, journal)
	if err != nil || generation == nil || !generation.Active() {
		t.Fatalf("generation=%v err=%v active=%v", generation, err, generation != nil && generation.Active())
	}
	if admission.Active() || token.ValidFor(inventory) {
		t.Fatalf("old authority survived: admission=%v token=%v", admission.Active(), token.ValidFor(inventory))
	}
	if _, err := inventory.Revision(); !errors.Is(err, ErrLeaseInvalid) {
		t.Fatalf("old inventory revision err=%v", err)
	}
	gotTarget, targetErr := generation.Target()
	gotJournal, journalErr := generation.Journal()
	if targetErr != nil || journalErr != nil || gotTarget != target || gotJournal != journal {
		t.Fatalf("target=%x/%v journal=%x/%v", gotTarget, targetErr, gotJournal, journalErr)
	}
	if f.root.children["lineages.lock"].locked {
		t.Fatal("root-wide lock remained held after handoff")
	}
	lineages := f.root.children["lineages"]
	for _, value := range []int{1, 2} {
		name := fmt.Sprintf("%x", digestForTest(value))
		if lineages.children[name].children["writer.lock"].locked {
			t.Fatalf("non-target lineage %s remained locked", name)
		}
	}
	targetName, journalName := fmt.Sprintf("%x", target), fmt.Sprintf("%x", journal)
	if !lineages.children[targetName].children["writer.lock"].locked || !lineages.children[targetName].children[journalName].children["writer.lock"].locked {
		t.Fatal("target lineage or generation lock was released")
	}
	if got, want := len(f.handles), 2; got != want {
		t.Fatalf("post-handoff handles=%d want=%d", got, want)
	}
	rootLease, err := store.AcquireRoot(context.Background())
	if err != nil {
		t.Fatalf("root lock was not released: %v", err)
	}
	if err := rootLease.Close(); err != nil {
		t.Fatal(err)
	}
	if err := admission.Close(); !errors.Is(err, ErrLeaseInvalid) {
		t.Fatalf("old admission close=%v", err)
	}
	if err := generation.Close(); err != nil || len(f.handles) != 0 {
		t.Fatalf("generation close=%v handles=%d", err, len(f.handles))
	}
	if lineages.children[targetName].children["writer.lock"].locked || lineages.children[targetName].children[journalName].children["writer.lock"].locked {
		t.Fatal("generation close retained a lock")
	}
}

func TestAdmissionHandoffPreflightFailuresPreserveAuthority(t *testing.T) {
	f, _, admission, inventory, token, _, journal := admissionWithGenerationForHandoff(t, 1)
	baselineUnlocks := f.unlockAttempts
	if generation, err := token.HandoffGeneration(context.Background(), inventory, digestForTest(88)); generation != nil || !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("wrong-journal generation=%v err=%v", generation, err)
	}
	if !admission.Active() || !token.ValidFor(inventory) || f.unlockAttempts != baselineUnlocks {
		t.Fatalf("preflight consumed authority active=%v token=%v unlock=%d close=%d", admission.Active(), token.ValidFor(inventory), f.unlockAttempts, len(f.closeAttempts))
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if generation, err := token.HandoffGeneration(ctx, inventory, journal); generation != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled generation=%v err=%v", generation, err)
	}
	if !admission.Active() || !token.ValidFor(inventory) {
		t.Fatal("pre-cancel consumed authority")
	}
	if err := admission.Close(); err != nil || len(f.handles) != 0 {
		t.Fatalf("cleanup=%v handles=%d", err, len(f.handles))
	}
}

func TestAdmissionHandoffCleanupFailureReturnsNoLeaseAndPoisonsStore(t *testing.T) {
	f, store, admission, inventory, token, target, journal := admissionWithGenerationForHandoff(t, 3)
	f.closeAttempts = nil
	f.unlockInodes = nil
	f.onUnlock = func(value *fakeBackend, node *fakeNode, _ int) {
		if node.name == "lineages.lock" {
			value.failUnlock = true
			value.failCloseNames["lineages.lock"] = true
		}
	}
	generation, err := token.HandoffGeneration(context.Background(), inventory, journal)
	if generation != nil || !errors.Is(err, ErrFilesystem) || store.usable() || admission.Active() || token.ValidFor(inventory) {
		t.Fatalf("generation=%v err=%v usable=%v admission=%v token=%v", generation, err, store.usable(), admission.Active(), token.ValidFor(inventory))
	}
	if len(f.handles) != 0 || f.unlockAttempts < 2+3 || len(f.closeAttempts) < 2+3+1 {
		t.Fatalf("handles=%d unlock=%d closes=%v", len(f.handles), f.unlockAttempts, f.closeAttempts)
	}
	targetName, journalName := fmt.Sprintf("%x", target), fmt.Sprintf("%x", journal)
	if f.root.children["lineages.lock"].locked || f.root.children["lineages"].children[targetName].children["writer.lock"].locked || f.root.children["lineages"].children[targetName].children[journalName].children["writer.lock"].locked {
		t.Fatal("failed handoff leaked a held lock")
	}
	if err := admission.Close(); !errors.Is(err, ErrLeaseInvalid) {
		t.Fatalf("old admission close=%v", err)
	}
}

func TestGenerationLeaseRejectsCopyMutationAndDoubleClose(t *testing.T) {
	f, _, _, inventory, token, target, journal := admissionWithGenerationForHandoff(t, 1)
	generation, err := token.HandoffGeneration(context.Background(), inventory, journal)
	if err != nil {
		t.Fatal(err)
	}
	copyLease := *generation
	if copyLease.Active() {
		t.Fatal("copied generation lease retained authority")
	}
	for name, mutate := range map[string]func(*GenerationLease){
		"target":     func(value *GenerationLease) { value.target[0]++ },
		"journal":    func(value *GenerationLease) { value.journal[0]++ },
		"lineage":    func(value *GenerationLease) { value.lineage.stat.inode++ },
		"generation": func(value *GenerationLease) { value.generation.stat.inode++ },
	} {
		t.Run(name, func(t *testing.T) {
			value := *generation
			value.self = &value
			mutate(&value)
			if value.Active() {
				t.Fatal("mutated generation lease retained authority")
			}
		})
	}
	if (&GenerationLease{}).Active() {
		t.Fatal("literal generation lease retained authority")
	}
	if err := generation.Close(); err != nil {
		t.Fatal(err)
	}
	if generation.Active() {
		t.Fatal("closed generation lease remained active")
	}
	if _, err := generation.Target(); !errors.Is(err, ErrLeaseInvalid) {
		t.Fatalf("closed target err=%v", err)
	}
	if err := generation.Close(); !errors.Is(err, ErrLeaseInvalid) {
		t.Fatalf("double close=%v", err)
	}
	if len(f.handles) != 0 {
		t.Fatalf("handles=%d", len(f.handles))
	}
	_ = target
}

func TestAdmissionHandoffReleasesOtherGenerationBeforeOtherLineages(t *testing.T) {
	f := newFakeBackend()
	target := digestForTest(9)
	addAdmissionLineage(f, digestForTest(1), 0, 0)
	addAdmissionLineage(f, target, 0, 0)
	store := testStore(t, f)
	admission, inventory, err := store.AcquireAdmission(context.Background(), target)
	if err != nil {
		t.Fatal(err)
	}
	for _, journal := range [][32]byte{digestForTest(70), digestForTest(80)} {
		token, tokenErr := inventory.MutationToken()
		if tokenErr != nil {
			t.Fatal(tokenErr)
		}
		created, createErr := token.CreateGenerationHeader(context.Background(), inventory, journal, []byte("header"))
		if createErr != nil {
			t.Fatal(createErr)
		}
		inventory = created.Inventory()
	}
	token, err := inventory.MutationToken()
	if err != nil {
		t.Fatal(err)
	}
	otherGenerationInode := admission.journalLocks[0].stat.inode
	retainedGenerationInode := admission.journalLocks[1].stat.inode
	otherLineageInode := admission.locks[0].stat.inode
	retainedLineageInode := admission.locks[1].stat.inode
	rootInode := admission.rootLease.lock.inode
	f.unlockInodes = nil
	generation, err := token.HandoffGeneration(context.Background(), inventory, digestForTest(80))
	if err != nil {
		t.Fatal(err)
	}
	want := []uint64{otherGenerationInode, otherLineageInode, rootInode}
	if len(f.unlockInodes) != len(want) {
		t.Fatalf("handoff unlocks=%v want=%v", f.unlockInodes, want)
	}
	for index := range want {
		if f.unlockInodes[index] != want[index] {
			t.Fatalf("handoff unlocks=%v want=%v", f.unlockInodes, want)
		}
	}
	if !generation.Active() || !f.root.children["lineages"].children[fmt.Sprintf("%x", target)].children[fmt.Sprintf("%x", digestForTest(80))].children["writer.lock"].locked {
		t.Fatal("selected generation was not retained")
	}
	f.unlockInodes = nil
	if err := generation.Close(); err != nil {
		t.Fatal(err)
	}
	if len(f.unlockInodes) != 2 || f.unlockInodes[0] != retainedGenerationInode || f.unlockInodes[1] != retainedLineageInode {
		t.Fatalf("generation close unlocks=%v", f.unlockInodes)
	}
}

func TestGenerationLeaseCloseAttemptsBothLocksAndPoisonsStore(t *testing.T) {
	f, store, _, inventory, token, _, journal := admissionWithGenerationForHandoff(t, 1)
	generation, err := token.HandoffGeneration(context.Background(), inventory, journal)
	if err != nil {
		t.Fatal(err)
	}
	f.closeAttempts = nil
	f.unlockAttempts = 0
	f.failUnlock = true
	f.failCloseNames["writer.lock"] = true
	if err := generation.Close(); !errors.Is(err, ErrFilesystem) || store.usable() || generation.Active() {
		t.Fatalf("close=%v usable=%v active=%v", err, store.usable(), generation.Active())
	}
	if f.unlockAttempts != 2 || len(f.closeAttempts) != 2 || len(f.handles) != 0 {
		t.Fatalf("unlock=%d close=%v handles=%d", f.unlockAttempts, f.closeAttempts, len(f.handles))
	}
	if err := generation.Close(); !errors.Is(err, ErrLeaseInvalid) {
		t.Fatalf("double close=%v", err)
	}
}

func TestGenerationLeaseBindingMutationFailsClosed(t *testing.T) {
	f, _, _, inventory, token, _, journal := admissionWithGenerationForHandoff(t, 1)
	generation, err := token.HandoffGeneration(context.Background(), inventory, journal)
	if err != nil {
		t.Fatal(err)
	}
	generation.binding.generation.inode++
	if generation.Active() {
		t.Fatal("mutated binding retained authority")
	}
	if _, err := generation.Journal(); !errors.Is(err, ErrLeaseInvalid) {
		t.Fatalf("mutated journal err=%v", err)
	}
	if err := generation.Close(); err != nil || len(f.handles) != 0 {
		t.Fatalf("cleanup=%v handles=%d", err, len(f.handles))
	}
}

func TestGenerationLeasePairedFieldMutationMissesRegistry(t *testing.T) {
	f, _, _, inventory, token, _, journal := admissionWithGenerationForHandoff(t, 1)
	generation, err := token.HandoffGeneration(context.Background(), inventory, journal)
	if err != nil {
		t.Fatal(err)
	}
	generation.generation.stat.inode++
	generation.binding.generation.inode++
	if generation.Active() {
		t.Fatal("paired field mutation retained registered authority")
	}
	if err := generation.Close(); err != nil || len(f.handles) != 0 {
		t.Fatalf("cleanup=%v handles=%d", err, len(f.handles))
	}
}
