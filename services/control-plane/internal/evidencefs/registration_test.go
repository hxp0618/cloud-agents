package evidencefs

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"testing"
)

func TestCreateTargetLineageDurableAdvancesInventory(t *testing.T) {
	f := newFakeBackend()
	f.partialWrite = 3
	store := testStore(t, f)
	target := digestForTest(9)
	lease, inventory, err := store.AcquireAdmission(context.Background(), target)
	if err != nil {
		t.Fatal(err)
	}
	token, err := inventory.MutationToken()
	if err != nil {
		t.Fatal(err)
	}
	header := []byte("opaque-verified-lineage-index-header")
	result, err := token.CreateTargetLineage(context.Background(), inventory, header)
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome() != AdmissionTransitionDurable || result.PreviousRevision() != 0 || result.CandidateRevision() != 1 || result.CandidateKind() != "target_lineage" || result.CandidateDigest() != sha256.Sum256(header) {
		t.Fatalf("result=%+v", result)
	}
	next := result.Inventory()
	if next == nil {
		t.Fatal("durable transition returned no inventory")
	}
	if revision, err := next.Revision(); err != nil || revision != 1 {
		t.Fatalf("revision=%d err=%v", revision, err)
	}
	if _, err := inventory.Revision(); !errors.Is(err, ErrLeaseInvalid) || token.ValidFor(inventory) {
		t.Fatalf("old authority survived: inventoryErr=%v tokenValid=%v", err, token.ValidFor(inventory))
	}
	if absent, err := next.TargetAbsent(); err != nil || absent != nil {
		t.Fatalf("absent=%v err=%v", absent, err)
	}
	lineage, err := next.Lineage(target)
	if err != nil {
		t.Fatal(err)
	}
	index, err := lineage.Index()
	if err != nil {
		t.Fatal(err)
	}
	stored, err := index.ReadAll(context.Background())
	if err != nil || !bytes.Equal(stored, header) {
		t.Fatalf("stored=%q err=%v", stored, err)
	}
	if err := next.Revalidate(context.Background()); err != nil {
		t.Fatal(err)
	}
	nextToken, err := next.MutationToken()
	if err != nil || !nextToken.ValidFor(next) {
		t.Fatalf("next token=%v err=%v", nextToken, err)
	}
	name := fmt.Sprintf("%x", target)
	if len(f.mkdirs) != 2 || f.mkdirs[0] != "lineages" || f.mkdirs[1] != name {
		t.Fatalf("mkdirs=%v", f.mkdirs)
	}
	if f.fsyncs != 4 || f.fdatasyncs != 2 || f.writes <= 1 {
		t.Fatalf("fsync=%d fdatasync=%d writes=%d", f.fsyncs, f.fdatasyncs, f.writes)
	}
	if got, want := len(lease.locks), 1; got != want || lease.locks[0].name != name {
		t.Fatalf("locks=%v", lease.locks)
	}
	if got, want := len(f.handles), 3; got != want {
		t.Fatalf("handles=%d want=%d", got, want)
	}
	if err := lease.Close(); err != nil || len(f.handles) != 0 {
		t.Fatalf("close err=%v handles=%d", err, len(f.handles))
	}
}

func TestTargetRegistrationDiscoveryRequiresExactOldSetPlusTarget(t *testing.T) {
	f := newFakeBackend()
	old := digestForTest(2)
	oldLineage := addAdmissionLineage(f, old, 0, 0)
	store := testStore(t, f)
	lease, inventory, err := store.AcquireAdmission(context.Background(), digestForTest(1))
	if err != nil {
		t.Fatal(err)
	}
	previous := cloneAdmissionDiscovery(inventory.slot.discovery)
	lineages := f.root.children["lineages"]
	targetName := fmt.Sprintf("%x", digestForTest(1))
	target := f.directory(targetName)
	target.children["writer.lock"] = f.regular("writer.lock", nil)
	target.children["index.caj"] = f.regular("index.caj", []byte("header"))
	lineages.children[targetName] = target
	lineages.stat.nlink++
	next, err := store.discoverAdmissionRoot(context.Background())
	if err != nil || !targetRegistrationDiscoveryMatches(previous, next, targetName) {
		t.Fatalf("exact next failed: err=%v", err)
	}
	oldLineage.children["index.caj"].data[0] ^= 1
	oldLineage.children["index.caj"].stat.size = uint64(len(oldLineage.children["index.caj"].data))
	// A same-sized byte mutation is checked by inventory hashing. This helper
	// closes the independent identity/set boundary, so replace the old inode to
	// prove it rejects a changed registered lineage.
	oldLineage.children["index.caj"] = f.regular("index.caj", oldLineage.children["index.caj"].data)
	changed, err := store.discoverAdmissionRoot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if targetRegistrationDiscoveryMatches(previous, changed, targetName) {
		t.Fatal("changed old lineage passed exact-set comparison")
	}
	if err := lease.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestCreateTargetLineageLineageLimitFailsBeforeMutation(t *testing.T) {
	f := newFakeBackend()
	for value := 0; value < maximumAdmissionLineages; value++ {
		addAdmissionLineage(f, digestForTest(value), 0, 0)
	}
	store := testStore(t, f)
	lease, inventory, err := store.AcquireAdmission(context.Background(), digestForTest(1000))
	if err != nil {
		t.Fatal(err)
	}
	token, err := inventory.MutationToken()
	if err != nil {
		t.Fatal(err)
	}
	result, err := token.CreateTargetLineage(context.Background(), inventory, []byte("header"))
	if !errors.Is(err, ErrLimit) || result.Outcome() != AdmissionTransitionPreMutationFailure || result.CandidateRevision() != 1 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if !token.ValidFor(inventory) || len(f.mkdirs) != 0 || f.writes != 0 || f.fsyncs != 0 || f.fdatasyncs != 0 {
		t.Fatalf("authority/mutation changed: valid=%v mkdirs=%v writes=%d fsync=%d fdatasync=%d", token.ValidFor(inventory), f.mkdirs, f.writes, f.fsyncs, f.fdatasyncs)
	}
	if err := lease.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestCreateTargetLineageInsertsCanonicalLock(t *testing.T) {
	f := newFakeBackend()
	existing := digestForTest(2)
	addAdmissionLineage(f, existing, 0, 0)
	store := testStore(t, f)
	target := digestForTest(1)
	lease, inventory, err := store.AcquireAdmission(context.Background(), target)
	if err != nil {
		t.Fatal(err)
	}
	token, err := inventory.MutationToken()
	if err != nil {
		t.Fatal(err)
	}
	result, err := token.CreateTargetLineage(context.Background(), inventory, []byte("header"))
	if err != nil || result.Outcome() != AdmissionTransitionDurable {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if len(f.mkdirs) != 1 || f.mkdirs[0] != fmt.Sprintf("%x", target) || f.fsyncs != 3 {
		t.Fatalf("mkdirs=%v fsyncs=%d", f.mkdirs, f.fsyncs)
	}
	if len(lease.locks) != 2 || lease.locks[0].name != fmt.Sprintf("%x", target) || lease.locks[1].name != fmt.Sprintf("%x", existing) {
		t.Fatalf("locks=%v", lease.locks)
	}
	ids, err := result.Inventory().LineageIDs()
	if err != nil || len(ids) != 2 || ids[0] != target || ids[1] != existing {
		t.Fatalf("ids=%x err=%v", ids, err)
	}
	if err := lease.Close(); err != nil || len(f.handles) != 0 {
		t.Fatalf("close err=%v handles=%d", err, len(f.handles))
	}
}

func TestCreateTargetLineagePreMutationFailuresPreserveAuthority(t *testing.T) {
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
	copyToken := *token
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for name, invoke := range map[string]func() (AdmissionTransitionResult, error){
		"empty": func() (AdmissionTransitionResult, error) {
			return token.CreateTargetLineage(context.Background(), inventory, nil)
		},
		"copy": func() (AdmissionTransitionResult, error) {
			return copyToken.CreateTargetLineage(context.Background(), inventory, []byte("header"))
		},
		"canceled": func() (AdmissionTransitionResult, error) {
			return token.CreateTargetLineage(ctx, inventory, []byte("header"))
		},
	} {
		t.Run(name, func(t *testing.T) {
			result, err := invoke()
			if err == nil || result.Outcome() != AdmissionTransitionPreMutationFailure || result.Inventory() != nil {
				t.Fatalf("result=%+v err=%v", result, err)
			}
		})
	}
	if !token.ValidFor(inventory) || len(f.mkdirs) != 0 || f.writes != 0 || f.fsyncs != 0 || f.fdatasyncs != 0 {
		t.Fatalf("authority/mutation changed: valid=%v mkdir=%v writes=%d fsync=%d fdatasync=%d", token.ValidFor(inventory), f.mkdirs, f.writes, f.fsyncs, f.fdatasyncs)
	}
	if err := lease.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestCreateTargetLineagePostMutationFailureIsUnknownAndClosable(t *testing.T) {
	for name, arm := range map[string]func(*fakeBackend){
		"parent-sync": func(f *fakeBackend) { f.failFsync = true },
		"index-sync":  func(f *fakeBackend) { f.failFdatasyncAt = 2 },
		"index-close": func(f *fakeBackend) { f.failCloseAt["index.caj"] = 1 },
	} {
		t.Run(name, func(t *testing.T) {
			f := newFakeBackend()
			store := testStore(t, f)
			target := digestForTest(9)
			lease, inventory, err := store.AcquireAdmission(context.Background(), target)
			if err != nil {
				t.Fatal(err)
			}
			token, err := inventory.MutationToken()
			if err != nil {
				t.Fatal(err)
			}
			arm(f)
			header := []byte("header")
			result, err := token.CreateTargetLineage(context.Background(), inventory, header)
			if !errors.Is(err, ErrUnknown) || result.Outcome() != AdmissionTransitionUnknown || result.Inventory() != nil || result.CandidateDigest() != sha256.Sum256(header) {
				t.Fatalf("result=%+v err=%v", result, err)
			}
			if lease.Active() || token.ValidFor(inventory) {
				t.Fatalf("unknown authority survived: lease=%v token=%v", lease.Active(), token.ValidFor(inventory))
			}
			if _, exists := f.root.children["lineages"]; !exists {
				t.Fatal("post-mutation failure removed durable prefix")
			}
			if err := lease.Close(); err != nil && name != "index-close" {
				t.Fatalf("close err=%v", err)
			}
			if len(f.handles) != 0 {
				t.Fatalf("fd leak=%v", f.handles)
			}
		})
	}
}

func TestCreateTargetLineageCancellationAfterWriteIsUnknown(t *testing.T) {
	f := newFakeBackend()
	f.partialWrite = 1
	ctx, cancel := context.WithCancel(context.Background())
	f.onWrite = func(_ *fakeBackend, node *fakeNode, call int) {
		if node.name == "index.caj" && call == 1 {
			cancel()
		}
	}
	store := testStore(t, f)
	target := digestForTest(9)
	lease, inventory, err := store.AcquireAdmission(context.Background(), target)
	if err != nil {
		t.Fatal(err)
	}
	token, err := inventory.MutationToken()
	if err != nil {
		t.Fatal(err)
	}
	result, err := token.CreateTargetLineage(ctx, inventory, []byte("header"))
	if !errors.Is(err, ErrUnknown) || !errors.Is(err, context.Canceled) || result.Outcome() != AdmissionTransitionUnknown || lease.Active() || token.ValidFor(inventory) {
		t.Fatalf("result=%+v err=%v lease=%v token=%v", result, err, lease.Active(), token.ValidFor(inventory))
	}
	name := fmt.Sprintf("%x", target)
	index := f.root.children["lineages"].children[name].children["index.caj"]
	if len(index.data) != 1 {
		t.Fatalf("partial index bytes=%q", index.data)
	}
	if err := lease.Close(); err != nil || len(f.handles) != 0 {
		t.Fatalf("close err=%v handles=%d", err, len(f.handles))
	}
}

func TestAdmissionTransitionDurableResultCanOnlyInvalidate(t *testing.T) {
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
	result, err := token.CreateTargetLineage(context.Background(), inventory, []byte("header"))
	if err != nil || result.Outcome() != AdmissionTransitionDurable || result.Inventory() == nil {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if err := result.Invalidate(); err != nil {
		t.Fatal(err)
	}
	if lease.Active() {
		t.Fatal("invalidated durable result retained lease authority")
	}
	if err := result.Invalidate(); !errors.Is(err, ErrLeaseInvalid) {
		t.Fatalf("durable result invalidated twice: %v", err)
	}
	if err := (AdmissionTransitionResult{}).Invalidate(); !errors.Is(err, ErrLeaseInvalid) {
		t.Fatalf("literal result invalidated authority: %v", err)
	}
	if err := lease.Close(); err != nil || len(f.handles) != 0 {
		t.Fatalf("close err=%v handles=%d", err, len(f.handles))
	}
}
