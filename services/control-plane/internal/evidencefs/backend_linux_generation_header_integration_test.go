//go:build linux && evidencefsintegration

package evidencefs

import (
	"bytes"
	"context"
	"errors"
	"os"
	"testing"
)

// linuxIntegrationGenerationHeaderCrashBackend is armed only after the target
// lineage has been registered and locked. It marks the exact directory, lock,
// and segment-0 durability barriers used by CreateGenerationHeader or
// RecoverGenerationHeader.
type linuxIntegrationGenerationHeaderCrashBackend struct {
	linuxBackend
	barrier          string
	scenario         string
	armed            bool
	rootFD           int
	lineagesFD       int
	lineageFD        int
	journalFD        int
	lockFD           int
	segmentFD        int
	journalSyncCalls int
	segmentSyncCalls int
	segmentExpected  int
	segmentWritten   int
}

var _ backend = (*linuxIntegrationGenerationHeaderCrashBackend)(nil)

func (b *linuxIntegrationGenerationHeaderCrashBackend) arm(scenario, barrier string) {
	b.scenario, b.barrier, b.armed = scenario, barrier, true
	b.rootFD, b.lineagesFD, b.lineageFD, b.journalFD, b.lockFD, b.segmentFD = -1, -1, -1, -1, -1, -1
	b.journalSyncCalls, b.segmentSyncCalls, b.segmentExpected, b.segmentWritten = 0, 0, 0, 0
}

func (b *linuxIntegrationGenerationHeaderCrashBackend) prefix() string {
	if b.scenario == "recovery" {
		return "generation-header-recovery"
	}
	return "generation-header"
}

func (b *linuxIntegrationGenerationHeaderCrashBackend) point(suffix string) string {
	if suffix == "" {
		return ""
	}
	return b.prefix() + "-" + suffix
}

func (b *linuxIntegrationGenerationHeaderCrashBackend) openRoot(path string) (int, error) {
	fd, err := b.linuxBackend.openRoot(path)
	if b.armed && err == nil {
		b.rootFD = fd
	}
	return fd, err
}

func (b *linuxIntegrationGenerationHeaderCrashBackend) openDirAt(parent int, name string) (int, error) {
	fd, err := b.linuxBackend.openDirAt(parent, name)
	if !b.armed || err != nil {
		return fd, err
	}
	switch {
	case parent == b.rootFD && name == "lineages":
		b.lineagesFD = fd
	case parent == b.lineagesFD && name == targetName(linuxIntegrationTarget):
		b.lineageFD = fd
	case parent == b.lineageFD && name == targetName(linuxIntegrationJournal):
		b.journalFD = fd
	}
	return fd, nil
}

func (b *linuxIntegrationGenerationHeaderCrashBackend) mkdirAt(parent int, name string) error {
	if !b.armed || b.scenario != "create" || parent != b.lineageFD || name != targetName(linuxIntegrationJournal) {
		return b.linuxBackend.mkdirAt(parent, name)
	}
	blockLinuxIntegrationCrashBarrier(b.barrier, b.point("before-directory-create"))
	err := b.linuxBackend.mkdirAt(parent, name)
	if err == nil {
		blockLinuxIntegrationCrashBarrier(b.barrier, b.point("after-directory-create"))
	}
	return err
}

func (b *linuxIntegrationGenerationHeaderCrashBackend) openFileAt(parent int, name string, create bool) (int, error) {
	if !b.armed || b.scenario != "create" || !create || parent != b.journalFD || name != "writer.lock" && name != admissionSegmentName(0) {
		return b.linuxBackend.openFileAt(parent, name, create)
	}
	kind := "lock"
	if name == admissionSegmentName(0) {
		kind = "segment"
	}
	blockLinuxIntegrationCrashBarrier(b.barrier, b.point("before-"+kind+"-create"))
	fd, err := b.linuxBackend.openFileAt(parent, name, create)
	if err == nil {
		if kind == "lock" {
			b.lockFD = fd
		} else {
			b.segmentFD = fd
		}
		blockLinuxIntegrationCrashBarrier(b.barrier, b.point("after-"+kind+"-create"))
	}
	return fd, err
}

func (b *linuxIntegrationGenerationHeaderCrashBackend) openFileAtReadWrite(parent int, name string) (int, error) {
	if !b.armed || b.scenario != "recovery" || parent != b.journalFD || name != "writer.lock" && name != admissionSegmentName(0) {
		return b.linuxBackend.openFileAtReadWrite(parent, name)
	}
	kind := "lock"
	if name == admissionSegmentName(0) {
		kind = "segment"
	}
	blockLinuxIntegrationCrashBarrier(b.barrier, b.point("before-"+kind+"-open"))
	fd, err := b.linuxBackend.openFileAtReadWrite(parent, name)
	if err == nil {
		if kind == "lock" {
			b.lockFD = fd
		} else {
			b.segmentFD = fd
		}
		blockLinuxIntegrationCrashBarrier(b.barrier, b.point("after-"+kind+"-open"))
	}
	return fd, err
}

func (b *linuxIntegrationGenerationHeaderCrashBackend) truncate(fd int, size int64) error {
	if !b.armed || b.scenario != "recovery" || fd != b.segmentFD || size != 0 {
		return b.linuxBackend.truncate(fd, size)
	}
	blockLinuxIntegrationCrashBarrier(b.barrier, b.point("before-segment-truncate"))
	err := b.linuxBackend.truncate(fd, size)
	if err == nil {
		blockLinuxIntegrationCrashBarrier(b.barrier, b.point("after-segment-truncate"))
	}
	return err
}

func (b *linuxIntegrationGenerationHeaderCrashBackend) pwrite(fd int, source []byte, offset int64) (int, error) {
	if !b.armed || fd != b.segmentFD {
		return b.linuxBackend.pwrite(fd, source, offset)
	}
	if b.segmentWritten == 0 {
		b.segmentExpected = len(source)
		blockLinuxIntegrationCrashBarrier(b.barrier, b.point("before-segment-write"))
	}
	if b.segmentWritten == 0 && b.barrier == b.point("after-short-segment-write") {
		limit := len(source) / 2
		if limit == 0 {
			limit = 1
		}
		count, err := b.linuxBackend.pwrite(fd, source[:limit], offset)
		if count > 0 {
			b.segmentWritten += count
		}
		if err == nil && count == limit {
			blockLinuxIntegrationCrashBarrier(b.barrier, b.point("after-short-segment-write"))
		}
		return count, err
	}
	count, err := b.linuxBackend.pwrite(fd, source, offset)
	if count > 0 {
		b.segmentWritten += count
	}
	if err == nil && b.segmentWritten == b.segmentExpected {
		blockLinuxIntegrationCrashBarrier(b.barrier, b.point("after-segment-write"))
	}
	return count, err
}

func (b *linuxIntegrationGenerationHeaderCrashBackend) fdatasync(fd int) error {
	if !b.armed {
		return b.linuxBackend.fdatasync(fd)
	}
	before, after := "", ""
	switch {
	case fd == b.lockFD:
		before, after = b.point("before-lock-fdatasync"), b.point("after-lock-fdatasync")
	case fd == b.segmentFD && b.scenario == "recovery" && b.segmentSyncCalls == 0:
		before, after = b.point("before-truncate-fdatasync"), b.point("after-truncate-fdatasync")
	case fd == b.segmentFD:
		before, after = b.point("before-segment-fdatasync"), b.point("after-segment-fdatasync")
	}
	blockLinuxIntegrationCrashBarrier(b.barrier, before)
	err := b.linuxBackend.fdatasync(fd)
	if err == nil {
		if fd == b.segmentFD {
			b.segmentSyncCalls++
		}
		blockLinuxIntegrationCrashBarrier(b.barrier, after)
	}
	return err
}

func (b *linuxIntegrationGenerationHeaderCrashBackend) fsync(fd int) error {
	if !b.armed {
		return b.linuxBackend.fsync(fd)
	}
	before, after := "", ""
	switch {
	case fd == b.lineageFD:
		before, after = b.point("before-parent-fsync"), b.point("after-parent-fsync")
	case fd == b.journalFD && b.journalSyncCalls == 0:
		before, after = b.point("before-lock-directory-fsync"), b.point("after-lock-directory-fsync")
	case fd == b.journalFD:
		before, after = b.point("before-segment-directory-fsync"), b.point("after-segment-directory-fsync")
	}
	blockLinuxIntegrationCrashBarrier(b.barrier, before)
	err := b.linuxBackend.fsync(fd)
	if err == nil {
		if fd == b.journalFD {
			b.journalSyncCalls++
		}
		blockLinuxIntegrationCrashBarrier(b.barrier, after)
	}
	return err
}

func (b *linuxIntegrationGenerationHeaderCrashBackend) tryLock(fd int) (bool, error) {
	if !b.armed || fd != b.lockFD {
		return b.linuxBackend.tryLock(fd)
	}
	blockLinuxIntegrationCrashBarrier(b.barrier, b.point("before-lock-flock"))
	locked, err := b.linuxBackend.tryLock(fd)
	if err == nil && locked {
		blockLinuxIntegrationCrashBarrier(b.barrier, b.point("after-lock-flock"))
	}
	return locked, err
}

func createLinuxIntegrationGenerationHeaderAtCrashBarrier(t *testing.T, rootPath, barrier string) {
	t.Helper()
	if !validLinuxIntegrationGenerationHeaderCrashBarrier(barrier) {
		t.Fatal("unknown generation header crash barrier")
	}
	prepareLinuxIntegrationGenerationHeaderBaseline(t, rootPath, nil)
	ops := &linuxIntegrationGenerationHeaderCrashBackend{}
	root, err := newRootWithAuthority(context.Background(), rootPath, uint32(os.Getuid()), ops, mountAuthority{seal: &struct{}{}})
	if err != nil {
		t.Fatal(err)
	}
	lease, inventory := acquireLinuxIntegrationRegisteredTarget(t, root)
	defer func() { _ = lease.Close() }()
	token, err := inventory.MutationToken()
	if err != nil {
		t.Fatal(err)
	}
	ops.arm("create", barrier)
	result, err := token.CreateGenerationHeader(context.Background(), inventory, linuxIntegrationJournal, []byte(linuxIntegrationGenerationHeader))
	t.Fatalf("generation header creation crossed crash barrier: outcome=%q inventory=%v err=%v", result.Outcome(), result.Inventory(), err)
}

func recoverLinuxIntegrationGenerationHeaderAtCrashBarrier(t *testing.T, rootPath, barrier string) {
	t.Helper()
	if !validLinuxIntegrationGenerationHeaderRecoveryCrashBarrier(barrier) {
		t.Fatal("unknown generation header recovery crash barrier")
	}
	header := []byte(linuxIntegrationGenerationHeader)
	prepareLinuxIntegrationGenerationHeaderBaseline(t, rootPath, header[:len(header)/2])
	ops := &linuxIntegrationGenerationHeaderCrashBackend{}
	root, err := newRootWithAuthority(context.Background(), rootPath, uint32(os.Getuid()), ops, mountAuthority{seal: &struct{}{}})
	if err != nil {
		t.Fatal(err)
	}
	lease, inventory, err := root.AcquireAdmission(context.Background(), linuxIntegrationTarget)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = lease.Close() }()
	token, err := inventory.MutationToken()
	if err != nil {
		t.Fatal(err)
	}
	ops.arm("recovery", barrier)
	result, err := token.RecoverGenerationHeader(context.Background(), inventory, linuxIntegrationJournal, header)
	t.Fatalf("generation header recovery crossed crash barrier: outcome=%q inventory=%v err=%v", result.Outcome(), result.Inventory(), err)
}

func acquireLinuxIntegrationRegisteredTarget(t *testing.T, root *Store) (*AdmissionLease, *AdmissionInventory) {
	t.Helper()
	lease, inventory, err := root.AcquireAdmission(context.Background(), linuxIntegrationTarget)
	if err != nil {
		t.Fatal(err)
	}
	fact, err := inventory.TargetRegistration()
	if err != nil || fact == nil {
		t.Fatalf("target registration fact=%v err=%v", fact, err)
	}
	state, err := fact.State()
	if err != nil || state != TargetRegistrationPrefixIndex {
		t.Fatalf("target registration state=%q err=%v", state, err)
	}
	token, err := inventory.MutationToken()
	if err != nil {
		t.Fatal(err)
	}
	result, err := token.RecoverTargetLineage(context.Background(), inventory, []byte(linuxIntegrationIndexHeader))
	if err != nil || result.Outcome() != AdmissionTransitionDurable || result.Inventory() == nil {
		t.Fatalf("target registration recovery outcome=%q inventory=%v err=%v", result.Outcome(), result.Inventory(), err)
	}
	return lease, result.Inventory()
}

func prepareLinuxIntegrationGenerationHeaderBaseline(t *testing.T, rootPath string, segmentPrefix []byte) {
	t.Helper()
	ops := linuxBackend{}
	closeFD := func(fd int) {
		if err := ops.close(fd); err != nil {
			t.Fatal(err)
		}
	}
	writeAll := func(fd int, value []byte) {
		for offset := 0; offset < len(value); {
			written, err := ops.pwrite(fd, value[offset:], int64(offset))
			if err != nil || written <= 0 || written > len(value)-offset {
				t.Fatalf("baseline write=%d remaining=%d err=%v", written, len(value)-offset, err)
			}
			offset += written
		}
	}
	rootFD, err := ops.openRoot(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := ops.mkdirAt(rootFD, "lineages"); err != nil {
		t.Fatal(err)
	}
	if err := ops.fsync(rootFD); err != nil {
		t.Fatal(err)
	}
	lineagesFD, err := ops.openDirAt(rootFD, "lineages")
	if err != nil {
		t.Fatal(err)
	}
	if err := ops.mkdirAt(lineagesFD, targetName(linuxIntegrationTarget)); err != nil {
		t.Fatal(err)
	}
	if err := ops.fsync(lineagesFD); err != nil {
		t.Fatal(err)
	}
	lineageFD, err := ops.openDirAt(lineagesFD, targetName(linuxIntegrationTarget))
	if err != nil {
		t.Fatal(err)
	}
	lockFD, err := ops.openFileAt(lineageFD, "writer.lock", true)
	if err != nil {
		t.Fatal(err)
	}
	if err := ops.fdatasync(lockFD); err != nil {
		t.Fatal(err)
	}
	if err := ops.fsync(lineageFD); err != nil {
		t.Fatal(err)
	}
	closeFD(lockFD)
	indexFD, err := ops.openFileAt(lineageFD, "index.caj", true)
	if err != nil {
		t.Fatal(err)
	}
	writeAll(indexFD, []byte(linuxIntegrationIndexHeader))
	if err := ops.fdatasync(indexFD); err != nil {
		t.Fatal(err)
	}
	if err := ops.fsync(lineageFD); err != nil {
		t.Fatal(err)
	}
	closeFD(indexFD)
	if segmentPrefix != nil {
		if err := ops.mkdirAt(lineageFD, targetName(linuxIntegrationJournal)); err != nil {
			t.Fatal(err)
		}
		if err := ops.fsync(lineageFD); err != nil {
			t.Fatal(err)
		}
		journalFD, err := ops.openDirAt(lineageFD, targetName(linuxIntegrationJournal))
		if err != nil {
			t.Fatal(err)
		}
		journalLockFD, err := ops.openFileAt(journalFD, "writer.lock", true)
		if err != nil {
			t.Fatal(err)
		}
		if err := ops.fdatasync(journalLockFD); err != nil {
			t.Fatal(err)
		}
		if err := ops.fsync(journalFD); err != nil {
			t.Fatal(err)
		}
		closeFD(journalLockFD)
		segmentFD, err := ops.openFileAt(journalFD, admissionSegmentName(0), true)
		if err != nil {
			t.Fatal(err)
		}
		writeAll(segmentFD, segmentPrefix)
		if err := ops.fdatasync(segmentFD); err != nil {
			t.Fatal(err)
		}
		if err := ops.fsync(journalFD); err != nil {
			t.Fatal(err)
		}
		closeFD(segmentFD)
		closeFD(journalFD)
	}
	closeFD(lineageFD)
	closeFD(lineagesFD)
	closeFD(rootFD)
}

func classifyLinuxIntegrationGenerationHeaderCrashState(t *testing.T, rootPath, barrier, scenario string) {
	t.Helper()
	if scenario == "create" {
		if !validLinuxIntegrationGenerationHeaderCrashBarrier(barrier) {
			t.Fatal("unknown generation header crash barrier")
		}
	} else if scenario == "recovery" {
		if !validLinuxIntegrationGenerationHeaderRecoveryCrashBarrier(barrier) {
			t.Fatal("unknown generation header recovery crash barrier")
		}
	} else {
		t.Fatal("unknown generation header scenario")
	}
	if root, err := Open(context.Background(), rootPath); root != nil || !errors.Is(err, ErrTrustedMountAuthority) {
		t.Fatalf("production Open bypassed trusted mount authority: root=%v err=%v", root, err)
	}
	root := newLinuxIntegrationRoot(t, rootPath)
	lease, inventory, err := root.AcquireAdmission(context.Background(), linuxIntegrationTarget)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := lease.Close(); err != nil {
			t.Fatal(err)
		}
	}()
	lineage, lineageErr := inventory.Lineage(linuxIntegrationTarget)
	if lineageErr != nil {
		fact, factErr := inventory.TargetRegistration()
		if factErr != nil || fact == nil {
			t.Fatalf("target prefix fact=%v err=%v lineage=%v", fact, factErr, lineageErr)
		}
		state, stateErr := fact.State()
		if stateErr != nil || state != TargetRegistrationPrefixIndex {
			t.Fatalf("target prefix state=%q err=%v", state, stateErr)
		}
		token, tokenErr := inventory.MutationToken()
		if tokenErr != nil {
			t.Fatal(tokenErr)
		}
		result, recoverErr := token.RecoverTargetLineage(context.Background(), inventory, []byte(linuxIntegrationIndexHeader))
		if recoverErr != nil || result.Outcome() != AdmissionTransitionDurable || result.Inventory() == nil {
			t.Fatalf("target promotion outcome=%q inventory=%v err=%v", result.Outcome(), result.Inventory(), recoverErr)
		}
		inventory = result.Inventory()
		lineage, lineageErr = inventory.Lineage(linuxIntegrationTarget)
	}
	if lineageErr != nil {
		t.Fatal(lineageErr)
	}
	facts, err := lineage.GenerationRegistrations()
	if err != nil {
		t.Fatal(err)
	}
	journals, err := lineage.Journals()
	if err != nil {
		t.Fatal(err)
	}
	state := "generation_absent"
	segmentBytes := uint64(0)
	if len(facts)+len(journals) > 1 {
		t.Fatalf("generation prefix cardinality facts=%d journals=%d", len(facts), len(journals))
	}
	if len(facts) == 1 {
		gotJournal, journalErr := facts[0].Journal()
		prefixState, stateErr := facts[0].State()
		if journalErr != nil || stateErr != nil || gotJournal != linuxIntegrationJournal {
			t.Fatalf("generation fact journal=%x state=%q errors=%v/%v", gotJournal, prefixState, journalErr, stateErr)
		}
		state = string(prefixState)
	}
	if len(journals) == 1 {
		gotJournal, journalErr := journals[0].ID()
		segments, segmentsErr := journals[0].Segments()
		if journalErr != nil || segmentsErr != nil || gotJournal != linuxIntegrationJournal || len(segments) != 1 {
			t.Fatalf("generation journal=%x segments=%d errors=%v/%v", gotJournal, len(segments), journalErr, segmentsErr)
		}
		raw, readErr := segments[0].ReadAll(context.Background())
		if readErr != nil {
			t.Fatal(readErr)
		}
		segmentBytes = uint64(len(raw))
		header := []byte(linuxIntegrationGenerationHeader)
		switch {
		case len(raw) == 0:
			state = "generation_segment_empty"
		case len(raw) < len(header) && bytes.Equal(raw, header[:len(raw)]):
			state = "generation_segment_torn"
		case bytes.Equal(raw, header):
			state = "generation_segment_complete"
		default:
			t.Fatalf("invalid generation prefix bytes=%d", len(raw))
		}
	}
	if scenario == "create" {
		if !validLinuxIntegrationGenerationHeaderCrashState(barrier, state) {
			t.Fatalf("create barrier %q rejected state=%q bytes=%d", barrier, state, segmentBytes)
		}
	} else if !validLinuxIntegrationGenerationHeaderRecoveryCrashState(barrier, state) {
		t.Fatalf("recovery barrier %q rejected state=%q bytes=%d", barrier, state, segmentBytes)
	}
	token, err := inventory.MutationToken()
	if err != nil {
		t.Fatal(err)
	}
	var result AdmissionJournalTransitionResult
	if state == "generation_absent" {
		result, err = token.CreateGenerationHeader(context.Background(), inventory, linuxIntegrationJournal, []byte(linuxIntegrationGenerationHeader))
	} else {
		result, err = token.RecoverGenerationHeader(context.Background(), inventory, linuxIntegrationJournal, []byte(linuxIntegrationGenerationHeader))
	}
	if err != nil || result.Outcome() != AdmissionTransitionDurable || result.Inventory() == nil || !result.ValidFor(result.Inventory()) {
		t.Fatalf("fresh generation recovery state=%q outcome=%q inventory=%v err=%v", state, result.Outcome(), result.Inventory(), err)
	}
	next := result.Inventory()
	nextLineage, err := next.Lineage(linuxIntegrationTarget)
	if err != nil {
		t.Fatal(err)
	}
	nextFacts, err := nextLineage.GenerationRegistrations()
	if err != nil || len(nextFacts) != 0 {
		t.Fatalf("next generation facts=%d err=%v", len(nextFacts), err)
	}
	nextJournal := findAdmissionJournal(nextLineage, linuxIntegrationJournal)
	if nextJournal == nil {
		t.Fatal("recovered generation journal is absent")
	}
	segments, err := nextJournal.Segments()
	if err != nil || len(segments) != 1 {
		t.Fatalf("recovered segments=%d err=%v", len(segments), err)
	}
	raw, err := segments[0].ReadAll(context.Background())
	if err != nil || !bytes.Equal(raw, []byte(linuxIntegrationGenerationHeader)) {
		t.Fatalf("recovered header=%q err=%v", raw, err)
	}
	if err := next.Revalidate(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Logf("EVIDENCEFS_INTEGRATION_GENERATION_HEADER_CRASH_RECOVERY scenario=%s barrier=%s state=%s segment_bytes=%d transition=%s", scenario, barrier, state, segmentBytes, result.CandidateKind())
}

func validLinuxIntegrationGenerationHeaderCrashBarrier(barrier string) bool {
	switch barrier {
	case "generation-header-before-directory-create", "generation-header-after-directory-create",
		"generation-header-before-parent-fsync", "generation-header-after-parent-fsync",
		"generation-header-before-lock-create", "generation-header-after-lock-create",
		"generation-header-before-lock-fdatasync", "generation-header-after-lock-fdatasync",
		"generation-header-before-lock-directory-fsync", "generation-header-after-lock-directory-fsync",
		"generation-header-before-lock-flock", "generation-header-after-lock-flock",
		"generation-header-before-segment-create", "generation-header-after-segment-create",
		"generation-header-before-segment-write", "generation-header-after-short-segment-write", "generation-header-after-segment-write",
		"generation-header-before-segment-fdatasync", "generation-header-after-segment-fdatasync",
		"generation-header-before-segment-directory-fsync", "generation-header-after-segment-directory-fsync":
		return true
	default:
		return false
	}
}

func validLinuxIntegrationGenerationHeaderRecoveryCrashBarrier(barrier string) bool {
	switch barrier {
	case "generation-header-recovery-before-parent-fsync", "generation-header-recovery-after-parent-fsync",
		"generation-header-recovery-before-lock-open", "generation-header-recovery-after-lock-open",
		"generation-header-recovery-before-lock-fdatasync", "generation-header-recovery-after-lock-fdatasync",
		"generation-header-recovery-before-lock-directory-fsync", "generation-header-recovery-after-lock-directory-fsync",
		"generation-header-recovery-before-lock-flock", "generation-header-recovery-after-lock-flock",
		"generation-header-recovery-before-segment-open", "generation-header-recovery-after-segment-open",
		"generation-header-recovery-before-segment-truncate", "generation-header-recovery-after-segment-truncate",
		"generation-header-recovery-before-truncate-fdatasync", "generation-header-recovery-after-truncate-fdatasync",
		"generation-header-recovery-before-segment-write", "generation-header-recovery-after-short-segment-write", "generation-header-recovery-after-segment-write",
		"generation-header-recovery-before-segment-fdatasync", "generation-header-recovery-after-segment-fdatasync",
		"generation-header-recovery-before-segment-directory-fsync", "generation-header-recovery-after-segment-directory-fsync":
		return true
	default:
		return false
	}
}

func validLinuxIntegrationGenerationHeaderCrashState(barrier, state string) bool {
	directory, lock := string(GenerationRegistrationPrefixDirectory), string(GenerationRegistrationPrefixLock)
	switch barrier {
	case "generation-header-before-directory-create":
		return state == "generation_absent"
	case "generation-header-after-directory-create", "generation-header-before-parent-fsync":
		return state == "generation_absent" || state == directory
	case "generation-header-after-parent-fsync", "generation-header-before-lock-create":
		return state == directory
	case "generation-header-after-lock-create", "generation-header-before-lock-fdatasync", "generation-header-after-lock-fdatasync", "generation-header-before-lock-directory-fsync":
		return state == directory || state == lock
	case "generation-header-after-lock-directory-fsync", "generation-header-before-lock-flock", "generation-header-after-lock-flock", "generation-header-before-segment-create":
		return state == lock
	case "generation-header-after-segment-create", "generation-header-before-segment-write":
		return state == lock || state == "generation_segment_empty"
	case "generation-header-after-short-segment-write":
		return state == lock || state == "generation_segment_empty" || state == "generation_segment_torn"
	case "generation-header-after-segment-write", "generation-header-before-segment-fdatasync":
		return state == lock || state == "generation_segment_empty" || state == "generation_segment_torn" || state == "generation_segment_complete"
	case "generation-header-after-segment-fdatasync", "generation-header-before-segment-directory-fsync":
		return state == lock || state == "generation_segment_complete"
	case "generation-header-after-segment-directory-fsync":
		return state == "generation_segment_complete"
	default:
		return false
	}
}

func validLinuxIntegrationGenerationHeaderRecoveryCrashState(barrier, state string) bool {
	switch barrier {
	case "generation-header-recovery-before-parent-fsync", "generation-header-recovery-after-parent-fsync",
		"generation-header-recovery-before-lock-open", "generation-header-recovery-after-lock-open",
		"generation-header-recovery-before-lock-fdatasync", "generation-header-recovery-after-lock-fdatasync",
		"generation-header-recovery-before-lock-directory-fsync", "generation-header-recovery-after-lock-directory-fsync",
		"generation-header-recovery-before-lock-flock", "generation-header-recovery-after-lock-flock",
		"generation-header-recovery-before-segment-open", "generation-header-recovery-after-segment-open", "generation-header-recovery-before-segment-truncate":
		return state == "generation_segment_torn"
	case "generation-header-recovery-after-segment-truncate", "generation-header-recovery-before-truncate-fdatasync":
		return state == "generation_segment_torn" || state == "generation_segment_empty"
	case "generation-header-recovery-after-truncate-fdatasync", "generation-header-recovery-before-segment-write":
		return state == "generation_segment_empty"
	case "generation-header-recovery-after-short-segment-write":
		return state == "generation_segment_empty" || state == "generation_segment_torn"
	case "generation-header-recovery-after-segment-write", "generation-header-recovery-before-segment-fdatasync":
		return state == "generation_segment_empty" || state == "generation_segment_torn" || state == "generation_segment_complete"
	case "generation-header-recovery-after-segment-fdatasync", "generation-header-recovery-before-segment-directory-fsync", "generation-header-recovery-after-segment-directory-fsync":
		return state == "generation_segment_complete"
	default:
		return false
	}
}
