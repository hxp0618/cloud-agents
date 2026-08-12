package migration

import (
	"context"
	"errors"
	"testing"
)

func immediateEvidenceBackoff(ctx context.Context, _ int) error { return evidenceContextError(ctx) }

func testEvidenceLockFile(f *fakeEvidenceFSOps, fd int, inode uint64, kind evidenceLockKind) evidenceLockFile {
	return evidenceLockFile{ops: f, fd: fd, device: 7, inode: inode, kind: kind}
}

func TestAcquireRootThenTryLineageReleasesRootWhenLineageBusy(t *testing.T) {
	f := newFakeEvidenceFS()
	f.busy[11] = 1
	old := evidenceLockBackoff
	evidenceLockBackoff = immediateEvidenceBackoff
	t.Cleanup(func() { evidenceLockBackoff = old })
	h, err := acquireRootThenTryLineage(context.Background(), testEvidenceLockFile(f, 10, 10, evidenceRootLockKind), testEvidenceLockFile(f, 11, 11, evidenceLineageLockKind))
	if err != nil {
		t.Fatal(err)
	}
	if len(f.lockAttempts) < 4 || f.lockAttempts[0] != 10 || f.lockAttempts[1] != 11 || f.lockAttempts[2] != 10 || f.lockAttempts[3] != 11 {
		t.Fatalf("lock order: %v", f.lockAttempts)
	}
	if len(f.unlocks) == 0 || f.unlocks[0] != 10 {
		t.Fatalf("root not immediately released: %v", f.unlocks)
	}
	if err := h.ReleaseRoot(); err != nil {
		t.Fatal(err)
	}
	if f.locks[10] || !f.locks[11] {
		t.Fatalf("unexpected held locks: %v", f.locks)
	}
	if err := h.Close(); err != nil {
		t.Fatal(err)
	}
	if err := h.Close(); !IsCode(err, CodeEvidenceJournalFailed) {
		t.Fatalf("double close: %v", err)
	}
}

func TestAcquireRootThenTryLineageCancellationAndFailures(t *testing.T) {
	old := evidenceLockBackoff
	evidenceLockBackoff = immediateEvidenceBackoff
	t.Cleanup(func() { evidenceLockBackoff = old })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	shared := newFakeEvidenceFS()
	if _, err := acquireRootThenTryLineage(ctx, testEvidenceLockFile(shared, 1, 1, evidenceRootLockKind), testEvidenceLockFile(shared, 2, 2, evidenceLineageLockKind)); !IsCode(err, CodeContextCanceled) {
		t.Fatalf("cancel: %v", err)
	}
	f := newFakeEvidenceFS()
	f.lockErr = errors.New("secret lock")
	if _, err := acquireRootThenTryLineage(context.Background(), testEvidenceLockFile(f, 1, 1, evidenceRootLockKind), testEvidenceLockFile(f, 2, 2, evidenceLineageLockKind)); !IsCode(err, CodeEvidenceJournalFailed) {
		t.Fatalf("lock: %v", err)
	}
	f = newFakeEvidenceFS()
	f.busy[2] = 1
	f.unlockErr = errors.New("secret unlock")
	if _, err := acquireRootThenTryLineage(context.Background(), testEvidenceLockFile(f, 1, 1, evidenceRootLockKind), testEvidenceLockFile(f, 2, 2, evidenceLineageLockKind)); !IsCode(err, CodeEvidenceJournalFailed) {
		t.Fatalf("unlock: %v", err)
	}
}

func TestAcquireRootThenTryLineageRootBusyIsBounded(t *testing.T) {
	f := newFakeEvidenceFS()
	f.busy[1] = evidenceLockRetryLimit
	calls := 0
	old := evidenceLockBackoff
	evidenceLockBackoff = func(context.Context, int) error {
		calls++
		return nil
	}
	t.Cleanup(func() { evidenceLockBackoff = old })
	if _, err := acquireRootThenTryLineage(context.Background(), testEvidenceLockFile(f, 1, 1, evidenceRootLockKind), testEvidenceLockFile(f, 2, 2, evidenceLineageLockKind)); !IsCode(err, CodeEvidenceJournalFailed) {
		t.Fatalf("root busy retry was not bounded: %v", err)
	}
	if calls != evidenceLockRetryLimit-1 || !f.closed[1] || !f.closed[2] {
		t.Fatalf("retry/ownership mismatch calls=%d closed=%v", calls, f.closed)
	}
}

func TestGenerationLockRequiresLineageAndLivesUntilOneShotClose(t *testing.T) {
	f := newFakeEvidenceFS()
	if _, err := acquireGenerationLock(context.Background(), nil, testEvidenceLockFile(f, 12, 12, evidenceGenerationLockKind)); !IsCode(err, CodeEvidenceJournalFailed) {
		t.Fatalf("nil lineage: %v", err)
	}
	lineage := &evidenceLineageLock{lineage: testEvidenceLockFile(f, 11, 11, evidenceLineageLockKind), lineageHeld: true}
	h, err := acquireGenerationLock(context.Background(), lineage, testEvidenceLockFile(f, 12, 12, evidenceGenerationLockKind))
	if err != nil {
		t.Fatal(err)
	}
	if !f.locks[12] {
		t.Fatal("generation lock not held")
	}
	if err := h.Close(); err != nil {
		t.Fatal(err)
	}
	if f.locks[12] || !f.closed[12] {
		t.Fatalf("generation handle not released: locks=%v closed=%v", f.locks, f.closed)
	}
	if err := h.Close(); !IsCode(err, CodeEvidenceJournalFailed) {
		t.Fatalf("double close: %v", err)
	}
}

func TestLockHandleReleaseAndCloseFailuresFailClosed(t *testing.T) {
	f := newFakeEvidenceFS()
	h := &evidenceLineageLock{root: testEvidenceLockFile(f, 1, 1, evidenceRootLockKind), lineage: testEvidenceLockFile(f, 2, 2, evidenceLineageLockKind), rootHeld: true, lineageHeld: true}
	f.unlockErr = errors.New("secret")
	if err := h.ReleaseRoot(); !IsCode(err, CodeEvidenceJournalFailed) {
		t.Fatalf("release: %v", err)
	}
	f = newFakeEvidenceFS()
	h = &evidenceLineageLock{root: testEvidenceLockFile(f, 1, 1, evidenceRootLockKind), lineage: testEvidenceLockFile(f, 2, 2, evidenceLineageLockKind), rootHeld: true, lineageHeld: true}
	f.closeErr = errors.New("secret")
	if err := h.Close(); !IsCode(err, CodeEvidenceJournalFailed) {
		t.Fatalf("close: %v", err)
	}
}

func TestLockAcquisitionFailuresCloseOwnedDescriptors(t *testing.T) {
	old := evidenceLockBackoff
	evidenceLockBackoff = immediateEvidenceBackoff
	t.Cleanup(func() { evidenceLockBackoff = old })

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	f := newFakeEvidenceFS()
	_, err := acquireRootThenTryLineage(ctx, testEvidenceLockFile(f, 1, 1, evidenceRootLockKind), testEvidenceLockFile(f, 2, 2, evidenceLineageLockKind))
	if !IsCode(err, CodeContextCanceled) || !f.closed[1] || !f.closed[2] {
		t.Fatalf("cancel cleanup err=%v closed=%v", err, f.closed)
	}

	f = newFakeEvidenceFS()
	f.lockErr = errors.New("try")
	_, err = acquireRootThenTryLineage(context.Background(), testEvidenceLockFile(f, 1, 1, evidenceRootLockKind), testEvidenceLockFile(f, 2, 2, evidenceLineageLockKind))
	if !IsCode(err, CodeEvidenceJournalFailed) || !f.closed[1] || !f.closed[2] {
		t.Fatalf("try cleanup err=%v closed=%v", err, f.closed)
	}

	f = newFakeEvidenceFS()
	lineage := &evidenceLineageLock{lineage: testEvidenceLockFile(f, 2, 2, evidenceLineageLockKind), lineageHeld: true}
	_, err = acquireGenerationLock(ctx, lineage, testEvidenceLockFile(f, 3, 3, evidenceGenerationLockKind))
	if !IsCode(err, CodeContextCanceled) || !f.closed[3] {
		t.Fatalf("generation cancel err=%v closed=%v", err, f.closed)
	}
}

func TestLocksRejectAliasedIdentityAndWrongKind(t *testing.T) {
	f := newFakeEvidenceFS()
	_, err := acquireRootThenTryLineage(context.Background(), testEvidenceLockFile(f, 1, 9, evidenceRootLockKind), testEvidenceLockFile(f, 2, 9, evidenceLineageLockKind))
	if !IsCode(err, CodeEvidenceJournalFailed) || !f.closed[1] || !f.closed[2] {
		t.Fatalf("alias: err=%v closed=%v", err, f.closed)
	}

	f = newFakeEvidenceFS()
	lineage := &evidenceLineageLock{lineage: testEvidenceLockFile(f, 2, 2, evidenceLineageLockKind), lineageHeld: true}
	_, err = acquireGenerationLock(context.Background(), lineage, testEvidenceLockFile(f, 3, 3, evidenceRootLockKind))
	if !IsCode(err, CodeEvidenceJournalFailed) || !f.closed[3] {
		t.Fatalf("kind: err=%v closed=%v", err, f.closed)
	}
}

func TestVerifiedLockFileOwnsDescriptorOnInvalidMetadata(t *testing.T) {
	f := newFakeEvidenceFS()
	root := &evidenceFSRoot{ops: f, fd: 1, uid: 501, device: 7}
	f.stats[9] = fakeRegularStat(7)
	st := f.stats[9]
	st.mode = 0o666
	f.stats[9] = st
	if _, err := verifiedEvidenceLockFile(root, 9, evidenceRootLockKind); !IsCode(err, CodeEvidenceJournalFailed) || !f.closed[9] {
		t.Fatalf("invalid metadata did not transfer/close fd: err=%v closed=%v", err, f.closed)
	}

	f = newFakeEvidenceFS()
	root = &evidenceFSRoot{ops: f, fd: 1, uid: 501, device: 7}
	f.stats[10] = fakeRegularStat(7)
	f.statErr = errors.New("metadata")
	f.closeErr = errors.New("close")
	if _, err := verifiedEvidenceLockFile(root, 10, evidenceRootLockKind); !IsCode(err, CodeEvidenceJournalFailed) || !f.closed[10] {
		t.Fatalf("combined metadata/close fault lost ownership: err=%v closed=%v", err, f.closed)
	}
}
