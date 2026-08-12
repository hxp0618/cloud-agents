package migration

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/evidencefs"
)

func immediateEvidenceBackoff(ctx context.Context, _ int) error { return evidenceContextError(ctx) }

func testEvidenceLockFile(f *fakeEvidenceFSOps, fd int, inode uint64, kind evidenceLockKind) evidenceLockFile {
	return evidenceLockFile{ops: f, fd: fd, device: 7, inode: inode, kind: kind}
}

// fakeEvidenceRootLease deliberately cannot expose an evidencefs lease. It can
// test orchestration only and cannot authorize Scan, Publish, or quota state.
type fakeEvidenceRootLease struct {
	active     bool
	closeErr   error
	closeCalls int
}

func (l *fakeEvidenceRootLease) Active() bool { return l != nil && l.active }
func (l *fakeEvidenceRootLease) Close() error {
	l.closeCalls++
	l.active = false
	return l.closeErr
}
func (*fakeEvidenceRootLease) publicationLease() *evidencefs.RootLease { return nil }

type fakeEvidenceRootStore struct {
	leases       []*fakeEvidenceRootLease
	errs         []error
	acquireCalls int
	before       func(int)
}

func (s *fakeEvidenceRootStore) AcquireRoot(context.Context) (evidenceRootLeaseHandle, error) {
	index := s.acquireCalls
	s.acquireCalls++
	if s.before != nil {
		s.before(index)
	}
	if index < len(s.errs) && s.errs[index] != nil {
		return nil, s.errs[index]
	}
	if index >= len(s.leases) {
		return nil, errors.New("unexpected root acquisition")
	}
	return s.leases[index], nil
}

func activeTestRootLease() *fakeEvidenceRootLease { return &fakeEvidenceRootLease{active: true} }

func testLineageHandle(root evidenceRootLeaseHandle, file evidenceLockFile) *evidenceLineageLock {
	handle := &evidenceLineageLock{root: root, lineage: file, lineageHeld: true}
	handle.self = handle
	return handle
}

func TestAcquireRootThenTryLineageReleasesRootBeforeReacquire(t *testing.T) {
	fs := newFakeEvidenceFS()
	fs.busy[11] = 1
	first, second := activeTestRootLease(), activeTestRootLease()
	store := &fakeEvidenceRootStore{leases: []*fakeEvidenceRootLease{first, second}}
	store.before = func(index int) {
		if index == 1 && first.closeCalls != 1 {
			t.Fatalf("root was not closed before reacquire: %+v", first)
		}
	}
	old := evidenceLockBackoff
	evidenceLockBackoff = immediateEvidenceBackoff
	t.Cleanup(func() { evidenceLockBackoff = old })

	h, err := acquireRootThenTryLineage(context.Background(), store, testEvidenceLockFile(fs, 11, 11, evidenceLineageLockKind))
	if err != nil {
		t.Fatal(err)
	}
	if store.acquireCalls != 2 || len(fs.lockAttempts) != 2 || first.closeCalls != 1 || second.closeCalls != 0 {
		t.Fatalf("order mismatch acquire=%d locks=%v first=%+v second=%+v", store.acquireCalls, fs.lockAttempts, first, second)
	}
	if h.root.publicationLease() != nil {
		t.Fatal("test facade minted publication authority")
	}
	if err := h.ReleaseRoot(); err != nil {
		t.Fatal(err)
	}
	if second.closeCalls != 1 || !fs.locks[11] {
		t.Fatalf("ReleaseRoot did not preserve lineage: root=%+v locks=%v", second, fs.locks)
	}
	if err := h.Close(); err != nil {
		t.Fatal(err)
	}
	if err := h.Close(); !IsCode(err, CodeEvidenceJournalFailed) {
		t.Fatalf("double close: %v", err)
	}
}

func TestAcquireRootThenTryLineageRootErrorsAreCentrallyMapped(t *testing.T) {
	tests := []struct {
		name string
		err  error
		code ErrorCode
	}{
		{"canceled", context.Canceled, CodeContextCanceled},
		{"deadline", context.DeadlineExceeded, CodeDeadlineExceeded},
		{"corrupt", evidencefs.ErrCorrupt, CodeEvidenceJournalCorrupt},
		{"limit", evidencefs.ErrLimit, CodeEvidenceJournalLimitExceeded},
		{"filesystem", evidencefs.ErrFilesystem, CodeEvidenceJournalFailed},
		{"unknown", errors.New("/secret/root: raw failure"), CodeEvidenceJournalFailed},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fs := newFakeEvidenceFS()
			store := &fakeEvidenceRootStore{errs: []error{tc.err}}
			_, err := acquireRootThenTryLineage(context.Background(), store, testEvidenceLockFile(fs, 2, 2, evidenceLineageLockKind))
			if !IsCode(err, tc.code) || strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "/") {
				t.Fatalf("mapping leaked or misclassified: %v", err)
			}
			var stable *Error
			if !errors.As(err, &stable) || stable.Err != nil || !fs.closed[2] {
				t.Fatalf("mapping retained cause or lost ownership: err=%+v closed=%v", stable, fs.closed)
			}
		})
	}
	if err := mapEvidenceRootError(nil, "evidence-root-lock"); !IsCode(err, CodeEvidenceJournalFailed) || strings.Contains(err.Error(), "/") {
		t.Fatalf("nil root error was not mapped to a stable generic failure: %v", err)
	}
}

func TestAcquireRootThenTryLineageInactiveRootCleanupDominates(t *testing.T) {
	fs := newFakeEvidenceFS()
	lease := &fakeEvidenceRootLease{closeErr: errors.New("secret cleanup")}
	store := &fakeEvidenceRootStore{leases: []*fakeEvidenceRootLease{lease}}
	_, err := acquireRootThenTryLineage(context.Background(), store, testEvidenceLockFile(fs, 2, 2, evidenceLineageLockKind))
	if !IsCode(err, CodeEvidenceJournalFailed) || !strings.Contains(err.Error(), "cleanup") || strings.Contains(err.Error(), "secret") || lease.closeCalls != 1 || !fs.closed[2] {
		t.Fatalf("inactive cleanup did not dominate safely: err=%v lease=%+v closed=%v", err, lease, fs.closed)
	}
}

func TestAcquireRootThenTryLineageCancellationAndLineageFailures(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	fs := newFakeEvidenceFS()
	if _, err := acquireRootThenTryLineage(ctx, &fakeEvidenceRootStore{}, testEvidenceLockFile(fs, 2, 2, evidenceLineageLockKind)); !IsCode(err, CodeContextCanceled) || !fs.closed[2] {
		t.Fatalf("cancel cleanup err=%v closed=%v", err, fs.closed)
	}

	fs = newFakeEvidenceFS()
	fs.lockErr = errors.New("secret lineage failure")
	lease := activeTestRootLease()
	if _, err := acquireRootThenTryLineage(context.Background(), &fakeEvidenceRootStore{leases: []*fakeEvidenceRootLease{lease}}, testEvidenceLockFile(fs, 2, 2, evidenceLineageLockKind)); !IsCode(err, CodeEvidenceJournalFailed) || lease.closeCalls != 1 || !fs.closed[2] {
		t.Fatalf("lineage failure cleanup err=%v root=%+v closed=%v", err, lease, fs.closed)
	}
}

func TestAcquireRootThenTryLineageBusyIsBounded(t *testing.T) {
	fs := newFakeEvidenceFS()
	fs.busy[2] = evidenceLockRetryLimit
	store := &fakeEvidenceRootStore{}
	for range evidenceLockRetryLimit {
		store.leases = append(store.leases, activeTestRootLease())
	}
	calls := 0
	old := evidenceLockBackoff
	evidenceLockBackoff = func(context.Context, int) error { calls++; return nil }
	t.Cleanup(func() { evidenceLockBackoff = old })
	if _, err := acquireRootThenTryLineage(context.Background(), store, testEvidenceLockFile(fs, 2, 2, evidenceLineageLockKind)); !IsCode(err, CodeEvidenceJournalFailed) {
		t.Fatalf("busy retry was not bounded: %v", err)
	}
	if calls != evidenceLockRetryLimit-1 || store.acquireCalls != evidenceLockRetryLimit || !fs.closed[2] {
		t.Fatalf("retry mismatch backoff=%d acquire=%d closed=%v", calls, store.acquireCalls, fs.closed)
	}
	for i, lease := range store.leases {
		if lease.closeCalls != 1 {
			t.Fatalf("lease %d not closed exactly once: %+v", i, lease)
		}
	}
}

func TestRootReleaseAndCompositeCloseFailClosed(t *testing.T) {
	fs := newFakeEvidenceFS()
	lease := activeTestRootLease()
	lease.closeErr = errors.New("secret close")
	h := testLineageHandle(lease, testEvidenceLockFile(fs, 2, 2, evidenceLineageLockKind))
	if err := h.ReleaseRoot(); !IsCode(err, CodeEvidenceJournalFailed) || !h.done || !fs.closed[2] {
		t.Fatalf("release failure did not poison composite: err=%v handle=%+v", err, h)
	}

	fs = newFakeEvidenceFS()
	lease = activeTestRootLease()
	h = testLineageHandle(lease, testEvidenceLockFile(fs, 2, 2, evidenceLineageLockKind))
	fs.closeErr = errors.New("lineage close")
	if err := h.Close(); !IsCode(err, CodeEvidenceJournalFailed) || lease.closeCalls != 1 || !h.done {
		t.Fatalf("composite close did not attempt both: err=%v root=%+v", err, lease)
	}
}

func TestReleaseRootInactiveGenuineHandleCleansEveryOwnedResource(t *testing.T) {
	fs := newFakeEvidenceFS()
	root := &fakeEvidenceRootLease{active: false}
	lineage := testLineageHandle(root, testEvidenceLockFile(fs, 2, 2, evidenceLineageLockKind))
	fs.locks[2] = true
	if err := lineage.ReleaseRoot(); !IsCode(err, CodeEvidenceJournalFailed) {
		t.Fatalf("inactive root was not rejected: %v", err)
	}
	if !lineage.done || !lineage.rootReleased || lineage.lineageHeld || root.closeCalls != 1 || fs.locks[2] || !fs.closed[2] {
		t.Fatalf("inactive root leaked owned resources: handle=%+v root=%+v locks=%v closed=%v", lineage, root, fs.locks, fs.closed)
	}

	fs = newFakeEvidenceFS()
	root = &fakeEvidenceRootLease{active: false, closeErr: errors.New("secret root close")}
	lineage = testLineageHandle(root, testEvidenceLockFile(fs, 2, 2, evidenceLineageLockKind))
	fs.locks[2] = true
	fs.unlockErr = errors.New("secret lineage unlock")
	fs.closeErr = errors.New("secret lineage close")
	if err := lineage.ReleaseRoot(); !IsCode(err, CodeEvidenceJournalFailed) || !strings.Contains(err.Error(), "cleanup") || strings.Contains(err.Error(), "secret") {
		t.Fatalf("inactive cleanup faults leaked or misclassified: %v", err)
	}
	if root.closeCalls != 1 || !fs.closed[2] || !lineage.done || !lineage.rootReleased || lineage.lineageHeld {
		t.Fatalf("inactive fault path skipped cleanup: handle=%+v root=%+v closed=%v", lineage, root, fs.closed)
	}
}

func TestCopiedLineageAndGenerationHandlesCannotReleaseOriginal(t *testing.T) {
	fs := newFakeEvidenceFS()
	root := activeTestRootLease()
	lineage := testLineageHandle(root, testEvidenceLockFile(fs, 2, 2, evidenceLineageLockKind))
	fs.locks[2] = true
	copyLineage := *lineage
	if err := copyLineage.ReleaseRoot(); !IsCode(err, CodeEvidenceJournalFailed) {
		t.Fatalf("copied lineage released root: %v", err)
	}
	if err := copyLineage.Close(); !IsCode(err, CodeEvidenceJournalFailed) {
		t.Fatalf("copied lineage closed: %v", err)
	}
	if root.closeCalls != 0 || !fs.locks[2] || fs.closed[2] || !root.Active() {
		t.Fatalf("copied lineage touched original authority: root=%+v locks=%v closed=%v", root, fs.locks, fs.closed)
	}

	generation, err := acquireGenerationLock(context.Background(), lineage, testEvidenceLockFile(fs, 3, 3, evidenceGenerationLockKind))
	if err != nil {
		t.Fatal(err)
	}
	copyGeneration := *generation
	if err := copyGeneration.Close(); !IsCode(err, CodeEvidenceJournalFailed) {
		t.Fatalf("copied generation closed: %v", err)
	}
	if !fs.locks[3] || fs.closed[3] {
		t.Fatalf("copied generation touched original fd: locks=%v closed=%v", fs.locks, fs.closed)
	}
	if err := generation.Close(); err != nil {
		t.Fatalf("original generation no longer closes: %v", err)
	}
	if err := lineage.Close(); err != nil {
		t.Fatalf("original lineage no longer closes: %v", err)
	}
}

func TestGenerationLockRequiresLineageAndPreservesSemantics(t *testing.T) {
	fs := newFakeEvidenceFS()
	if _, err := acquireGenerationLock(context.Background(), nil, testEvidenceLockFile(fs, 12, 12, evidenceGenerationLockKind)); !IsCode(err, CodeEvidenceJournalFailed) {
		t.Fatalf("nil lineage: %v", err)
	}
	lineage := testLineageHandle(nil, testEvidenceLockFile(fs, 11, 11, evidenceLineageLockKind))
	h, err := acquireGenerationLock(context.Background(), lineage, testEvidenceLockFile(fs, 12, 12, evidenceGenerationLockKind))
	if err != nil {
		t.Fatal(err)
	}
	if !fs.locks[12] {
		t.Fatal("generation lock not held")
	}
	if err := h.Close(); err != nil {
		t.Fatal(err)
	}
	if fs.locks[12] || !fs.closed[12] {
		t.Fatalf("generation handle not released: locks=%v closed=%v", fs.locks, fs.closed)
	}
	if err := h.Close(); !IsCode(err, CodeEvidenceJournalFailed) {
		t.Fatalf("double close: %v", err)
	}

	lineageCopy := *lineage
	beforeUnlocks := len(fs.unlocks)
	if _, err := acquireGenerationLock(context.Background(), &lineageCopy, testEvidenceLockFile(fs, 13, 13, evidenceGenerationLockKind)); !IsCode(err, CodeEvidenceJournalFailed) || !fs.closed[13] {
		t.Fatalf("copied lineage admitted generation: err=%v closed=%v", err, fs.closed)
	}
	if len(fs.unlocks) != beforeUnlocks || fs.closed[11] {
		t.Fatalf("copied lineage touched original fd: unlocks=%v closed=%v", fs.unlocks, fs.closed)
	}
}

func TestLocksRejectWrongKindAndInvalidMetadata(t *testing.T) {
	fs := newFakeEvidenceFS()
	lineage := testLineageHandle(nil, testEvidenceLockFile(fs, 2, 2, evidenceLineageLockKind))
	_, err := acquireGenerationLock(context.Background(), lineage, testEvidenceLockFile(fs, 3, 3, evidenceLineageLockKind))
	if !IsCode(err, CodeEvidenceJournalFailed) || !fs.closed[3] {
		t.Fatalf("wrong generation kind: err=%v closed=%v", err, fs.closed)
	}

	fs = newFakeEvidenceFS()
	root := &evidenceFSRoot{ops: fs, fd: 1, uid: 501, device: 7}
	fs.stats[9] = fakeRegularStat(7)
	st := fs.stats[9]
	st.mode = 0o666
	fs.stats[9] = st
	if _, err := verifiedEvidenceLockFile(root, 9, evidenceLineageLockKind); !IsCode(err, CodeEvidenceJournalFailed) || !fs.closed[9] {
		t.Fatalf("invalid metadata ownership: err=%v closed=%v", err, fs.closed)
	}
}

func TestEvidenceFSDependencyDirectionIsOneWay(t *testing.T) {
	parseImports := func(dir string) []string {
		t.Helper()
		packages, err := parser.ParseDir(token.NewFileSet(), dir, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		var imports []string
		for _, pkg := range packages {
			for _, file := range pkg.Files {
				ast.Inspect(file, func(node ast.Node) bool {
					if spec, ok := node.(*ast.ImportSpec); ok {
						path, _ := strconv.Unquote(spec.Path.Value)
						imports = append(imports, path)
					}
					return true
				})
			}
		}
		return imports
	}
	migrationImports := parseImports(".")
	evidenceFSImports := parseImports(filepath.Join("..", "evidencefs"))
	if !containsString(migrationImports, "github.com/hxp0618/cloud-agents/services/control-plane/internal/evidencefs") {
		t.Fatal("migration no longer imports the evidencefs root authority")
	}
	for _, path := range evidenceFSImports {
		if strings.Contains(path, "/internal/migration") {
			t.Fatalf("evidencefs imports migration: %s", path)
		}
	}
}

func TestRootLineageAdapterHasNoProductionCallerBeforeCrossBinding(t *testing.T) {
	packages, err := parser.ParseDir(token.NewFileSet(), ".", func(info fs.FileInfo) bool {
		return !strings.HasSuffix(info.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	for _, pkg := range packages {
		for _, file := range pkg.Files {
			ast.Inspect(file, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				if ident, ok := call.Fun.(*ast.Ident); ok && ident.Name == "acquireRootThenTryLineage" {
					calls++
				}
				return true
			})
		}
	}
	if calls != 0 {
		t.Fatalf("root-lineage adapter gained %d production callers before same-root cross-binding", calls)
	}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
