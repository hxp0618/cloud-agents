package evidencefs

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"testing"
)

func addTargetRegistrationPrefix(f *fakeBackend, target [32]byte, state TargetRegistrationState, index []byte) *fakeNode {
	lineages := f.root.children["lineages"]
	if lineages == nil {
		lineages = f.directory("lineages")
		f.root.children["lineages"] = lineages
	}
	name := fmt.Sprintf("%x", target)
	directory := f.directory(name)
	if state == TargetRegistrationPrefixLock || state == TargetRegistrationPrefixIndex {
		directory.children["writer.lock"] = f.regular("writer.lock", nil)
	}
	if state == TargetRegistrationPrefixIndex {
		directory.children["index.caj"] = f.regular("index.caj", index)
	}
	lineages.children[name] = directory
	return directory
}

func TestRecoverTargetLineageDurablePrefixMatrix(t *testing.T) {
	header := []byte("opaque-verified-lineage-index-header")
	tests := []struct {
		name                 string
		state                TargetRegistrationState
		prefix               []byte
		truncates            int
		writes               bool
		directorySizeChanges bool
		dataSyncs            []string
		directorys           []string
	}{
		{name: "directory", state: TargetRegistrationPrefixDirectory, writes: true, dataSyncs: []string{"writer.lock", "index.caj"}, directorys: []string{"lineages", fmt.Sprintf("%x", digestForTest(9)), fmt.Sprintf("%x", digestForTest(9))}},
		{name: "lock", state: TargetRegistrationPrefixLock, writes: true, directorySizeChanges: true, dataSyncs: []string{"writer.lock", "index.caj"}, directorys: []string{"lineages", fmt.Sprintf("%x", digestForTest(9)), fmt.Sprintf("%x", digestForTest(9))}},
		{name: "index-empty", state: TargetRegistrationPrefixIndex, truncates: 1, writes: true, dataSyncs: []string{"writer.lock", "index.caj", "index.caj"}, directorys: []string{"lineages", fmt.Sprintf("%x", digestForTest(9)), fmt.Sprintf("%x", digestForTest(9))}},
		{name: "index-torn", state: TargetRegistrationPrefixIndex, prefix: header[:11], truncates: 1, writes: true, dataSyncs: []string{"writer.lock", "index.caj", "index.caj"}, directorys: []string{"lineages", fmt.Sprintf("%x", digestForTest(9)), fmt.Sprintf("%x", digestForTest(9))}},
		{name: "index-complete", state: TargetRegistrationPrefixIndex, prefix: header, dataSyncs: []string{"writer.lock", "index.caj"}, directorys: []string{"lineages", fmt.Sprintf("%x", digestForTest(9)), fmt.Sprintf("%x", digestForTest(9))}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			f := newFakeBackend()
			target := digestForTest(9)
			directory := addTargetRegistrationPrefix(f, target, test.state, test.prefix)
			store := testStore(t, f)
			lease, inventory, err := store.AcquireAdmission(context.Background(), target)
			if err != nil {
				t.Fatal(err)
			}
			fact, err := inventory.TargetRegistration()
			if err != nil || fact == nil {
				t.Fatalf("fact=%v err=%v", fact, err)
			}
			if test.directorySizeChanges {
				f.onFsync = func(_ *fakeBackend, node *fakeNode, _ int) {
					if node == directory {
						node.stat.size++
					}
				}
			}
			state, err := fact.State()
			if err != nil || state != test.state {
				t.Fatalf("state=%q err=%v", state, err)
			}
			oldFullSet, err := inventory.FullSetDigest()
			if err != nil {
				t.Fatal(err)
			}
			token, err := inventory.MutationToken()
			if err != nil {
				t.Fatal(err)
			}
			result, err := token.RecoverTargetLineage(context.Background(), inventory, header)
			if err != nil || result.Outcome() != AdmissionTransitionDurable || result.CandidateKind() != "target_lineage_recovery" || result.CandidateDigest() != sha256.Sum256(header) || result.PreviousRevision() != 0 || result.CandidateRevision() != 1 {
				t.Fatalf("result=%+v err=%v", result, err)
			}
			next := result.Inventory()
			newFullSet, err := next.FullSetDigest()
			if err != nil || newFullSet == oldFullSet {
				t.Fatalf("full set old=%x new=%x err=%v", oldFullSet, newFullSet, err)
			}
			registered, err := next.Lineage(target)
			if err != nil {
				t.Fatal(err)
			}
			index, err := registered.Index()
			if err != nil {
				t.Fatal(err)
			}
			stored, err := index.ReadAll(context.Background())
			if err != nil || !bytes.Equal(stored, header) {
				t.Fatalf("stored=%q err=%v", stored, err)
			}
			nextFact, err := next.TargetRegistration()
			if err != nil || nextFact == nil {
				t.Fatalf("next fact=%v err=%v", nextFact, err)
			}
			nextState, err := nextFact.State()
			if err != nil || nextState != TargetRegistrationRegisteredEmpty {
				t.Fatalf("next state=%q err=%v", nextState, err)
			}
			if f.truncates != test.truncates || (f.writes > 0) != test.writes || !equalStrings(f.fdatasyncNames, test.dataSyncs) || !equalStrings(f.fsyncNames, test.directorys) || f.unlinks != 0 || f.renames != 0 {
				t.Fatalf("truncate=%d writes=%d fdatasync=%v fsync=%v unlink=%d rename=%d", f.truncates, f.writes, f.fdatasyncNames, f.fsyncNames, f.unlinks, f.renames)
			}
			if len(lease.locks) != 1 || lease.locks[0].name != fmt.Sprintf("%x", target) || !directory.children["writer.lock"].locked {
				t.Fatalf("locks=%v nodeLocked=%v", lease.locks, directory.children["writer.lock"].locked)
			}
			if err := next.Revalidate(context.Background()); err != nil {
				t.Fatal(err)
			}
			if err := lease.Close(); err != nil || len(f.handles) != 0 {
				t.Fatalf("close=%v handles=%d", err, len(f.handles))
			}
		})
	}
}

func TestRecoverTargetLineageRejectsDifferentPrefixBeforeMutation(t *testing.T) {
	f := newFakeBackend()
	target := digestForTest(9)
	directory := addTargetRegistrationPrefix(f, target, TargetRegistrationPrefixIndex, []byte("wrong"))
	store := testStore(t, f)
	lease, inventory, err := store.AcquireAdmission(context.Background(), target)
	if err != nil {
		t.Fatal(err)
	}
	token, err := inventory.MutationToken()
	if err != nil {
		t.Fatal(err)
	}
	before := append([]byte(nil), directory.children["index.caj"].data...)
	result, err := token.RecoverTargetLineage(context.Background(), inventory, []byte("candidate"))
	if !errors.Is(err, ErrCorrupt) || errors.Is(err, ErrUnknown) || result.Outcome() != AdmissionTransitionPreMutationFailure || result.Inventory() != nil || lease.Active() || token.ValidFor(inventory) || f.truncates != 0 || f.writes != 0 || f.fdatasyncs != 0 || f.fsyncs != 0 || !bytes.Equal(directory.children["index.caj"].data, before) {
		t.Fatalf("result=%+v err=%v active=%v token=%v truncates=%d writes=%d sync=%d/%d bytes=%q", result, err, lease.Active(), token.ValidFor(inventory), f.truncates, f.writes, f.fdatasyncs, f.fsyncs, directory.children["index.caj"].data)
	}
	if err := lease.Close(); err != nil || len(f.handles) != 0 {
		t.Fatalf("close=%v handles=%d", err, len(f.handles))
	}
}

func TestRecoverTargetLineageRejectsSameSizePostInventoryPrefixSubstitution(t *testing.T) {
	f := newFakeBackend()
	target := digestForTest(9)
	directory := addTargetRegistrationPrefix(f, target, TargetRegistrationPrefixIndex, []byte("abcX"))
	store := testStore(t, f)
	lease, inventory, err := store.AcquireAdmission(context.Background(), target)
	if err != nil {
		t.Fatal(err)
	}
	token, err := inventory.MutationToken()
	if err != nil {
		t.Fatal(err)
	}
	copy(directory.children["index.caj"].data, []byte("abcd"))
	result, err := token.RecoverTargetLineage(context.Background(), inventory, []byte("abcdef"))
	if !errors.Is(err, ErrCorrupt) || errors.Is(err, ErrUnknown) || result.Outcome() != AdmissionTransitionPreMutationFailure || lease.Active() || token.ValidFor(inventory) || f.truncates != 0 || f.writes != 0 || f.fdatasyncs != 0 || f.fsyncs != 0 {
		t.Fatalf("result=%+v err=%v active=%v token=%v mutations=%d/%d/%d/%d", result, err, lease.Active(), token.ValidFor(inventory), f.truncates, f.writes, f.fdatasyncs, f.fsyncs)
	}
	if err := lease.Close(); err != nil || len(f.handles) != 0 {
		t.Fatalf("close=%v handles=%d", err, len(f.handles))
	}
}

func TestRecoverTargetLineageBarrierFailuresAreUnknownAndNeverDeletePrefix(t *testing.T) {
	header := []byte("opaque-verified-lineage-index-header")
	tests := []struct {
		name   string
		state  TargetRegistrationState
		prefix []byte
		arm    func(*fakeBackend, *fakeNode, context.CancelFunc)
	}{
		{name: "parent-sync", state: TargetRegistrationPrefixDirectory, arm: func(f *fakeBackend, _ *fakeNode, _ context.CancelFunc) { f.failFsyncAt = 1 }},
		{name: "lock-create", state: TargetRegistrationPrefixDirectory, arm: func(f *fakeBackend, _ *fakeNode, _ context.CancelFunc) { f.failOpenNames["writer.lock"] = fakeNotExist }},
		{name: "lock-fdatasync", state: TargetRegistrationPrefixDirectory, arm: func(f *fakeBackend, _ *fakeNode, _ context.CancelFunc) { f.failFdatasyncAt = 1 }},
		{name: "lock-directory-sync", state: TargetRegistrationPrefixDirectory, arm: func(f *fakeBackend, _ *fakeNode, _ context.CancelFunc) { f.failFsyncAt = 2 }},
		{name: "lock-busy", state: TargetRegistrationPrefixDirectory, arm: func(f *fakeBackend, _ *fakeNode, _ context.CancelFunc) {
			f.onTryLock = func(value *fakeBackend, node *fakeNode, _ int) { value.busyInodeAttempts[node.stat.inode] = 1 }
		}},
		{name: "lock-try-error", state: TargetRegistrationPrefixDirectory, arm: func(f *fakeBackend, _ *fakeNode, _ context.CancelFunc) {
			f.onTryLock = func(value *fakeBackend, node *fakeNode, _ int) { value.failTryLockInodes[node.stat.inode] = true }
		}},
		{name: "index-create", state: TargetRegistrationPrefixLock, arm: func(f *fakeBackend, _ *fakeNode, _ context.CancelFunc) { f.failOpenNames["index.caj"] = fakeNotExist }},
		{name: "existing-lock-reacquire", state: TargetRegistrationPrefixLock, arm: func(f *fakeBackend, directory *fakeNode, _ context.CancelFunc) {
			f.failTryLockInodes[directory.children["writer.lock"].stat.inode] = true
		}},
		{name: "index-write", state: TargetRegistrationPrefixLock, arm: func(f *fakeBackend, _ *fakeNode, _ context.CancelFunc) { f.failWriteAt = 1 }},
		{name: "truncate", state: TargetRegistrationPrefixIndex, prefix: header[:9], arm: func(f *fakeBackend, _ *fakeNode, _ context.CancelFunc) { f.failTruncateAt = 1 }},
		{name: "truncate-response-lost", state: TargetRegistrationPrefixIndex, prefix: header[:9], arm: func(f *fakeBackend, _ *fakeNode, _ context.CancelFunc) { f.failTruncateAfterAt = 1 }},
		{name: "truncate-sync", state: TargetRegistrationPrefixIndex, prefix: header[:9], arm: func(f *fakeBackend, _ *fakeNode, _ context.CancelFunc) { f.failFdatasyncAt = 2 }},
		{name: "complete-index-sync", state: TargetRegistrationPrefixIndex, prefix: header, arm: func(f *fakeBackend, _ *fakeNode, _ context.CancelFunc) { f.failFdatasyncAt = 2 }},
		{name: "final-directory-sync", state: TargetRegistrationPrefixIndex, prefix: header, arm: func(f *fakeBackend, _ *fakeNode, _ context.CancelFunc) { f.failFsyncAt = 3 }},
		{name: "cancel-after-write", state: TargetRegistrationPrefixLock, arm: func(f *fakeBackend, _ *fakeNode, cancel context.CancelFunc) {
			f.partialWrite = 1
			f.onWrite = func(_ *fakeBackend, node *fakeNode, call int) {
				if node.name == "index.caj" && call == 1 {
					cancel()
				}
			}
		}},
		{name: "terminal-extra-entry", state: TargetRegistrationPrefixIndex, prefix: header, arm: func(f *fakeBackend, directory *fakeNode, _ context.CancelFunc) {
			f.onFsync = func(value *fakeBackend, node *fakeNode, call int) {
				if node == directory && call == 3 {
					directory.children["unexpected"] = value.regular("unexpected", nil)
				}
			}
		}},
		{name: "index-close", state: TargetRegistrationPrefixIndex, prefix: header, arm: func(f *fakeBackend, _ *fakeNode, _ context.CancelFunc) {
			f.failCloseAt["index.caj"] = f.closeNameCounts["index.caj"] + 3
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			f := newFakeBackend()
			target := digestForTest(9)
			directory := addTargetRegistrationPrefix(f, target, test.state, test.prefix)
			store := testStore(t, f)
			lease, inventory, err := store.AcquireAdmission(context.Background(), target)
			if err != nil {
				t.Fatal(err)
			}
			token, err := inventory.MutationToken()
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			test.arm(f, directory, cancel)
			result, err := token.RecoverTargetLineage(ctx, inventory, header)
			if !errors.Is(err, ErrUnknown) || result.Outcome() != AdmissionTransitionUnknown || result.Inventory() != nil || result.CandidateKind() != "target_lineage_recovery" || result.CandidateDigest() != sha256.Sum256(header) || lease.Active() || token.ValidFor(inventory) {
				t.Fatalf("result=%+v err=%v active=%v token=%v", result, err, lease.Active(), token.ValidFor(inventory))
			}
			if test.name == "cancel-after-write" && !errors.Is(err, context.Canceled) {
				t.Fatalf("unknown cancellation lost cause: %v", err)
			}
			name := fmt.Sprintf("%x", target)
			if f.root.children["lineages"] == nil || f.root.children["lineages"].children[name] == nil || f.unlinks != 0 || f.renames != 0 {
				t.Fatalf("prefix removed: lineages=%v unlink=%d rename=%d", f.root.children["lineages"], f.unlinks, f.renames)
			}
			if test.name == "lock-try-error" && f.unlockAttempts == 0 {
				t.Fatal("ambiguous try-lock error did not attempt unlock")
			}
			if test.name == "index-close" && store.usable() {
				t.Fatal("descriptor cleanup uncertainty did not poison store")
			}
			_ = lease.Close()
			if len(f.handles) != 0 {
				t.Fatalf("descriptor leak=%v", f.handles)
			}
		})
	}
}

func TestAcquireAdmissionPrefixLockParticipatesInCanonicalRetry(t *testing.T) {
	f := newFakeBackend()
	target := digestForTest(2)
	addAdmissionLineage(f, digestForTest(1), 0, 0)
	directory := addTargetRegistrationPrefix(f, target, TargetRegistrationPrefixLock, nil)
	addAdmissionLineage(f, digestForTest(3), 0, 0)
	f.busyInodeAttempts[directory.children["writer.lock"].stat.inode] = 1
	store := testStore(t, f)
	lease, inventory, err := store.AcquireAdmission(context.Background(), target)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"lineages.lock", "writer.lock", "writer.lock", "lineages.lock", "writer.lock", "writer.lock", "writer.lock"}
	if !equalStrings(f.tryLockAttempts, want) || len(lease.locks) != 3 || lease.locks[0].name != fmt.Sprintf("%x", digestForTest(1)) || lease.locks[1].name != fmt.Sprintf("%x", target) || lease.locks[2].name != fmt.Sprintf("%x", digestForTest(3)) {
		t.Fatalf("attempts=%v locks=%v", f.tryLockAttempts, lease.locks)
	}
	fact, err := inventory.TargetRegistration()
	if err != nil || fact == nil {
		t.Fatalf("fact=%v err=%v", fact, err)
	}
	state, err := fact.State()
	if err != nil || state != TargetRegistrationPrefixLock {
		t.Fatalf("state=%q err=%v", state, err)
	}
	if err := lease.Close(); err != nil || len(f.handles) != 0 {
		t.Fatalf("close=%v handles=%d", err, len(f.handles))
	}
}

func TestRecoverTargetLineageOwnsCandidateAndInvalidatesOldFact(t *testing.T) {
	f := newFakeBackend()
	target := digestForTest(9)
	addTargetRegistrationPrefix(f, target, TargetRegistrationPrefixLock, nil)
	store := testStore(t, f)
	lease, inventory, err := store.AcquireAdmission(context.Background(), target)
	if err != nil {
		t.Fatal(err)
	}
	fact, err := inventory.TargetRegistration()
	if err != nil || fact == nil {
		t.Fatalf("fact=%v err=%v", fact, err)
	}
	copyFact := *fact
	if _, err := copyFact.State(); !errors.Is(err, ErrLeaseInvalid) {
		t.Fatalf("copied fact retained authority: %v", err)
	}
	token, err := inventory.MutationToken()
	if err != nil {
		t.Fatal(err)
	}
	header := []byte("opaque-verified-lineage-index-header")
	want := append([]byte(nil), header...)
	f.partialWrite = 1
	f.onWrite = func(_ *fakeBackend, node *fakeNode, call int) {
		if node.name == "index.caj" && call == 1 {
			clear(header)
		}
	}
	result, err := token.RecoverTargetLineage(context.Background(), inventory, header)
	if err != nil || result.Outcome() != AdmissionTransitionDurable || result.CandidateDigest() != sha256.Sum256(want) {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if _, err := fact.State(); !errors.Is(err, ErrLeaseInvalid) {
		t.Fatalf("old fact survived revision advance: %v", err)
	}
	next := result.Inventory()
	lineage, err := next.Lineage(target)
	if err != nil {
		t.Fatal(err)
	}
	index, err := lineage.Index()
	if err != nil {
		t.Fatal(err)
	}
	stored, err := index.ReadAll(context.Background())
	if err != nil || !bytes.Equal(stored, want) {
		t.Fatalf("stored=%q want=%q err=%v", stored, want, err)
	}
	nextFact, err := next.TargetRegistration()
	if err != nil || nextFact == nil {
		t.Fatalf("next fact=%v err=%v", nextFact, err)
	}
	if prefixIndex, err := nextFact.Index(); err != nil || prefixIndex != nil {
		t.Fatalf("registered fact exposed prefix index=%v err=%v", prefixIndex, err)
	}
	if err := lease.Close(); err != nil || len(f.handles) != 0 {
		t.Fatalf("close=%v handles=%d", err, len(f.handles))
	}
}

func TestUnregisteredTargetCanOnlyUseRegistrationTransition(t *testing.T) {
	for _, state := range []TargetRegistrationState{TargetRegistrationAbsent, TargetRegistrationPrefixDirectory, TargetRegistrationPrefixLock, TargetRegistrationPrefixIndex} {
		t.Run(string(state), func(t *testing.T) {
			f := newFakeBackend()
			target := digestForTest(9)
			if state != TargetRegistrationAbsent {
				addTargetRegistrationPrefix(f, target, state, []byte("prefix"))
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
			advanced, advanceErr := token.Advance(context.Background(), inventory)
			content := []byte("candidate-runtime-object")
			published, publishErr := token.PublishObject(context.Background(), inventory, sha256.Sum256(content), content)
			if !errors.Is(advanceErr, ErrInvalidInput) || advanced.Outcome() != AdmissionTransitionPreMutationFailure || !errors.Is(publishErr, ErrInvalidInput) || published.Outcome() != AdmissionTransitionPreMutationFailure || !token.ValidFor(inventory) || f.writes != 0 || f.renames != 0 || f.fsyncs != 0 || f.fdatasyncs != 0 {
				t.Fatalf("advance=%+v/%v publish=%+v/%v token=%v mutations=%d/%d/%d/%d", advanced, advanceErr, published, publishErr, token.ValidFor(inventory), f.writes, f.renames, f.fsyncs, f.fdatasyncs)
			}
			if err := lease.Close(); err != nil || len(f.handles) != 0 {
				t.Fatalf("close=%v handles=%d", err, len(f.handles))
			}
		})
	}
}

func TestRecoverTargetLineagePreMutationFailuresPreserveFreshAuthority(t *testing.T) {
	f := newFakeBackend()
	target := digestForTest(9)
	addTargetRegistrationPrefix(f, target, TargetRegistrationPrefixLock, nil)
	store := testStore(t, f)
	lease, inventory, err := store.AcquireAdmission(context.Background(), target)
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
			return token.RecoverTargetLineage(context.Background(), inventory, nil)
		},
		"copy": func() (AdmissionTransitionResult, error) {
			return copyToken.RecoverTargetLineage(context.Background(), inventory, []byte("header"))
		},
		"canceled": func() (AdmissionTransitionResult, error) {
			return token.RecoverTargetLineage(ctx, inventory, []byte("header"))
		},
	} {
		t.Run(name, func(t *testing.T) {
			result, err := invoke()
			if err == nil || errors.Is(err, ErrUnknown) || result.Outcome() != AdmissionTransitionPreMutationFailure || result.Inventory() != nil {
				t.Fatalf("result=%+v err=%v", result, err)
			}
		})
	}
	if !token.ValidFor(inventory) || f.writes != 0 || f.truncates != 0 || f.fdatasyncs != 0 || f.fsyncs != 0 {
		t.Fatalf("authority/mutation changed: token=%v writes=%d truncate=%d sync=%d/%d", token.ValidFor(inventory), f.writes, f.truncates, f.fdatasyncs, f.fsyncs)
	}
	if err := lease.Close(); err != nil || len(f.handles) != 0 {
		t.Fatalf("close=%v handles=%d", err, len(f.handles))
	}
}

func TestTargetRecoveryDurableResultCanInvalidate(t *testing.T) {
	f := newFakeBackend()
	target := digestForTest(9)
	addTargetRegistrationPrefix(f, target, TargetRegistrationPrefixIndex, []byte("header"))
	store := testStore(t, f)
	lease, inventory, err := store.AcquireAdmission(context.Background(), target)
	if err != nil {
		t.Fatal(err)
	}
	token, err := inventory.MutationToken()
	if err != nil {
		t.Fatal(err)
	}
	result, err := token.RecoverTargetLineage(context.Background(), inventory, []byte("header"))
	if err != nil || result.Outcome() != AdmissionTransitionDurable {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if err := result.Invalidate(); err != nil || lease.Active() {
		t.Fatalf("invalidate=%v active=%v", err, lease.Active())
	}
	if err := result.Invalidate(); !errors.Is(err, ErrLeaseInvalid) {
		t.Fatalf("second invalidate=%v", err)
	}
	if err := lease.Close(); err != nil || len(f.handles) != 0 {
		t.Fatalf("close=%v handles=%d", err, len(f.handles))
	}
}
