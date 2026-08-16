//go:build linux && evidencefsintegration

package evidencefs

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

const (
	linuxIntegrationRequiredEnv = "CLOUD_AGENTS_REQUIRE_EVIDENCEFS_LINUX_INTEGRATION"
	linuxIntegrationRootEnv     = "CLOUD_AGENTS_EVIDENCEFS_INTEGRATION_ROOT"
	linuxIntegrationFSEnv       = "CLOUD_AGENTS_EVIDENCEFS_INTEGRATION_FS"
	linuxIntegrationHelperEnv   = "CLOUD_AGENTS_EVIDENCEFS_INTEGRATION_HELPER"
	linuxIntegrationBarrierEnv  = "CLOUD_AGENTS_EVIDENCEFS_INTEGRATION_BARRIER"
	linuxIntegrationVerifyEnv   = "CLOUD_AGENTS_EVIDENCEFS_VERIFY_EXISTING"

	linuxIntegrationObjectReady     = "EVIDENCEFS_INTEGRATION_OBJECT_PUBLISHED_AND_ROOT_LOCKED"
	linuxIntegrationGenerationReady = "EVIDENCEFS_INTEGRATION_GENERATION_DURABLE_AND_LOCKED"
	linuxIntegrationBarrierReady    = "EVIDENCEFS_INTEGRATION_CRASH_BARRIER"
	linuxIntegrationPayload         = "cloud-agents-evidencefs-linux-integration-v1"
	linuxIntegrationTargetSubject   = "cloud-agents-evidencefs-linux-target-v1"
	linuxIntegrationJournalSubject  = "cloud-agents-evidencefs-linux-journal-v1"

	linuxIntegrationIndexHeader              = "cloud-agents-evidencefs-lineage-index-header-v1"
	linuxIntegrationGenerationHeader         = "cloud-agents-evidencefs-generation-segment-zero-header-v1"
	linuxIntegrationActivationIndexFrame     = "cloud-agents-evidencefs-generation-activation-index-frame-v1"
	linuxIntegrationExistingJournalFrame     = "cloud-agents-evidencefs-existing-segment-journal-frame-v1"
	linuxIntegrationExistingCheckpointFrame  = "cloud-agents-evidencefs-existing-segment-checkpoint-frame-v1"
	linuxIntegrationRotationHeaderFrame      = "cloud-agents-evidencefs-rotation-header-frame-v1"
	linuxIntegrationRotationCheckpointFrame  = "cloud-agents-evidencefs-rotation-checkpoint-frame-v1"
	linuxIntegrationRotationCallerFrame      = "cloud-agents-evidencefs-rotation-caller-frame-v1"
	linuxIntegrationRotationCallerCheckpoint = "cloud-agents-evidencefs-rotation-caller-checkpoint-frame-v1"
)

var (
	linuxIntegrationDigest  = sha256.Sum256([]byte(linuxIntegrationPayload))
	linuxIntegrationTarget  = sha256.Sum256([]byte(linuxIntegrationTargetSubject))
	linuxIntegrationJournal = sha256.Sum256([]byte(linuxIntegrationJournalSubject))
)

// TestLinuxIntegrationDurabilityRestartAndCrossProcessLocks is deliberately
// hidden behind an opt-in build tag and environment gate. It exercises the
// real Linux backend with a package-private test authority; it does not make
// Open usable and cannot mint production trusted-mount authority.
func TestLinuxIntegrationDurabilityRestartAndCrossProcessLocks(t *testing.T) {
	if os.Getenv(linuxIntegrationRequiredEnv) != "1" {
		t.Skip("real Linux evidencefs integration was not explicitly required")
	}
	rootPath := os.Getenv(linuxIntegrationRootEnv)
	filesystem := os.Getenv(linuxIntegrationFSEnv)
	if rootPath == "" || !filepath.IsAbs(rootPath) || filepath.Clean(rootPath) != rootPath {
		t.Fatal("integration root must be an absolute canonical path")
	}
	mountID := requireLinuxIntegrationMount(t, rootPath, filesystem)

	switch os.Getenv(linuxIntegrationHelperEnv) {
	case "publish-hold":
		publishAndHoldLinuxIntegrationObject(t, rootPath)
		return
	case "generation-hold":
		createAndHoldLinuxIntegrationGeneration(t, rootPath)
		return
	case "verify-object":
		verifyLinuxIntegrationObject(t, rootPath)
		return
	case "verify-generation":
		verifyLinuxIntegrationGeneration(t, rootPath)
		return
	case "publish-crash":
		publishLinuxIntegrationObjectAtCrashBarrier(t, rootPath, os.Getenv(linuxIntegrationBarrierEnv))
		return
	case "classify-object-crash":
		classifyLinuxIntegrationObjectCrashState(t, rootPath, os.Getenv(linuxIntegrationBarrierEnv))
		return
	case "generation-append-crash":
		appendLinuxIntegrationGenerationAtCrashBarrier(t, rootPath, os.Getenv(linuxIntegrationBarrierEnv))
		return
	case "classify-generation-append-crash":
		classifyLinuxIntegrationGenerationAppendCrashState(t, rootPath, os.Getenv(linuxIntegrationBarrierEnv))
		return
	case "generation-rotation-crash":
		rotateLinuxIntegrationGenerationAtCrashBarrier(t, rootPath, os.Getenv(linuxIntegrationBarrierEnv))
		return
	case "classify-generation-rotation-crash":
		classifyLinuxIntegrationGenerationRotationCrashState(t, rootPath, os.Getenv(linuxIntegrationBarrierEnv))
		return
	case "generation-activation-crash":
		activateLinuxIntegrationGenerationAtCrashBarrier(t, rootPath, os.Getenv(linuxIntegrationBarrierEnv))
		return
	case "classify-generation-activation-crash":
		classifyLinuxIntegrationGenerationActivationCrashState(t, rootPath, os.Getenv(linuxIntegrationBarrierEnv))
		return
	case "":
	default:
		t.Fatal("unknown integration helper mode")
	}

	if root, err := Open(context.Background(), rootPath); root != nil || !errors.Is(err, ErrTrustedMountAuthority) {
		t.Fatalf("production Open bypassed trusted mount authority: root=%v err=%v", root, err)
	}
	if os.Getenv(linuxIntegrationVerifyEnv) == "1" {
		verifyLinuxIntegrationObject(t, rootPath)
		verifyLinuxIntegrationGeneration(t, rootPath)
		t.Logf("EVIDENCEFS_LINUX_REOPEN filesystem=%s mount_id=%d object=%s target=%s journal=%s segments=2", filesystem, mountID, hex.EncodeToString(linuxIntegrationDigest[:]), hex.EncodeToString(linuxIntegrationTarget[:]), hex.EncodeToString(linuxIntegrationJournal[:]))
		return
	}

	publisher := startLinuxIntegrationHolder(t, "publish-hold", linuxIntegrationObjectReady)
	contender := newLinuxIntegrationRoot(t, rootPath)
	blockedContext, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	blockedLease, blockedErr := contender.AcquireRoot(blockedContext)
	cancel()
	if blockedLease != nil || !errors.Is(blockedErr, context.DeadlineExceeded) {
		if blockedLease != nil {
			_ = blockedLease.Close()
		}
		t.Fatalf("cross-process root lock did not block: lease=%v err=%v", blockedLease, blockedErr)
	}
	publisher.kill(t, "publisher")
	verifyLinuxIntegrationObject(t, rootPath)
	verifyLinuxIntegrationInFreshProcess(t, "verify-object", "durable object")

	generator := startLinuxIntegrationHolder(t, "generation-hold", linuxIntegrationGenerationReady)
	rootContender := newLinuxIntegrationRoot(t, rootPath)
	rootContext, rootCancel := context.WithTimeout(context.Background(), time.Second)
	rootLease, err := rootContender.AcquireRoot(rootContext)
	rootCancel()
	if err != nil || rootLease == nil {
		t.Fatalf("handoff retained the root lock: lease=%v err=%v", rootLease, err)
	}
	if err := rootLease.Close(); err != nil {
		t.Fatal(err)
	}
	lineageContext, lineageCancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	blockedAdmission, blockedInventory, blockedErr := rootContender.AcquireAdmission(lineageContext, linuxIntegrationTarget)
	lineageCancel()
	if blockedAdmission != nil || blockedInventory != nil || !errors.Is(blockedErr, context.DeadlineExceeded) {
		if blockedAdmission != nil {
			_ = blockedAdmission.Close()
		}
		t.Fatalf("handoff did not retain the target lineage lock: lease=%v inventory=%v err=%v", blockedAdmission, blockedInventory, blockedErr)
	}
	generator.kill(t, "generation holder")

	verifyLinuxIntegrationGeneration(t, rootPath)
	verifyLinuxIntegrationInFreshProcess(t, "verify-generation", "durable generation")
	t.Logf("EVIDENCEFS_LINUX_INTEGRATION filesystem=%s mount_id=%d object=%s target=%s journal=%s segments=2", filesystem, mountID, hex.EncodeToString(linuxIntegrationDigest[:]), hex.EncodeToString(linuxIntegrationTarget[:]), hex.EncodeToString(linuxIntegrationJournal[:]))
}

// linuxIntegrationCrashBackend delegates to the real syscall backend and only
// pauses at fixed object-publication durability boundaries.
type linuxIntegrationCrashBackend struct {
	linuxBackend
	barrier        string
	writeCalls     int
	dataSyncCalls  int
	renameCalls    int
	directoryCalls int
}

var _ backend = (*linuxIntegrationCrashBackend)(nil)

func (b *linuxIntegrationCrashBackend) hit(point string) {
	if b == nil {
		return
	}
	blockLinuxIntegrationCrashBarrier(b.barrier, point)
}

func blockLinuxIntegrationCrashBarrier(barrier, point string) {
	if barrier != point {
		return
	}
	if _, err := fmt.Fprintf(os.Stdout, "%s barrier=%s\n", linuxIntegrationBarrierReady, point); err != nil {
		panic(err)
	}
	for {
		time.Sleep(time.Hour)
	}
}

// linuxIntegrationGenerationAppendCrashBackend stays dormant while the
// baseline generation is made durable, then delegates the candidate append to
// real Linux pwrite/fdatasync calls around one fixed crash boundary.
type linuxIntegrationGenerationAppendCrashBackend struct {
	linuxBackend
	barrier         string
	armed           bool
	journalFD       int
	indexFD         int
	journalExpected int
	journalWritten  int
	indexExpected   int
	indexWritten    int
}

var _ backend = (*linuxIntegrationGenerationAppendCrashBackend)(nil)

func (b *linuxIntegrationGenerationAppendCrashBackend) arm(barrier string) {
	b.barrier = barrier
	b.journalFD = -1
	b.indexFD = -1
	b.journalExpected = 0
	b.journalWritten = 0
	b.indexExpected = 0
	b.indexWritten = 0
	b.armed = true
}

func (b *linuxIntegrationGenerationAppendCrashBackend) openFileAtReadWrite(parent int, name string) (int, error) {
	fd, err := b.linuxBackend.openFileAtReadWrite(parent, name)
	if !b.armed || err != nil {
		return fd, err
	}
	switch name {
	case "index.caj":
		if b.indexFD >= 0 {
			panic("generation append opened index twice")
		}
		b.indexFD = fd
	case admissionSegmentName(0):
		if b.journalFD >= 0 {
			panic("generation append opened journal segment twice")
		}
		b.journalFD = fd
	}
	return fd, nil
}

func (b *linuxIntegrationGenerationAppendCrashBackend) pwrite(fd int, source []byte, offset int64) (int, error) {
	if !b.armed {
		return b.linuxBackend.pwrite(fd, source, offset)
	}
	var expected, written *int
	before, short, after := "", "", ""
	switch fd {
	case b.journalFD:
		expected, written = &b.journalExpected, &b.journalWritten
		before, short, after = "before-journal-write", "after-short-journal-write", "after-journal-write"
	case b.indexFD:
		expected, written = &b.indexExpected, &b.indexWritten
		before, short, after = "before-index-write", "after-short-index-write", "after-index-write"
	default:
		return b.linuxBackend.pwrite(fd, source, offset)
	}
	if *written == 0 {
		*expected = len(source)
		blockLinuxIntegrationCrashBarrier(b.barrier, before)
	}
	if *written == 0 && b.barrier == short {
		limit := len(source) / 2
		if limit == 0 {
			limit = 1
		}
		count, err := b.linuxBackend.pwrite(fd, source[:limit], offset)
		if count > 0 {
			*written += count
		}
		if err == nil && count == limit {
			blockLinuxIntegrationCrashBarrier(b.barrier, short)
		}
		return count, err
	}
	count, err := b.linuxBackend.pwrite(fd, source, offset)
	if count > 0 {
		*written += count
	}
	if err == nil && *written == *expected {
		blockLinuxIntegrationCrashBarrier(b.barrier, after)
	}
	return count, err
}

func (b *linuxIntegrationGenerationAppendCrashBackend) fdatasync(fd int) error {
	if !b.armed {
		return b.linuxBackend.fdatasync(fd)
	}
	before, after := "", ""
	switch fd {
	case b.journalFD:
		before, after = "before-journal-fdatasync", "after-journal-fdatasync"
	case b.indexFD:
		before, after = "before-index-fdatasync", "after-index-fdatasync"
	}
	blockLinuxIntegrationCrashBarrier(b.barrier, before)
	err := b.linuxBackend.fdatasync(fd)
	if err == nil {
		blockLinuxIntegrationCrashBarrier(b.barrier, after)
	}
	return err
}

// linuxIntegrationGenerationActivationCrashBackend stays dormant while the
// target and generation header are made durable. Once armed, it binds the
// candidate activation append to the exact existing index descriptor.
type linuxIntegrationGenerationActivationCrashBackend struct {
	linuxBackend
	barrier  string
	armed    bool
	indexFD  int
	expected int
	written  int
}

var _ backend = (*linuxIntegrationGenerationActivationCrashBackend)(nil)

func (b *linuxIntegrationGenerationActivationCrashBackend) arm(barrier string) {
	b.barrier = barrier
	b.indexFD = -1
	b.expected = 0
	b.written = 0
	b.armed = true
}

func (b *linuxIntegrationGenerationActivationCrashBackend) openFileAtReadWrite(parent int, name string) (int, error) {
	fd, err := b.linuxBackend.openFileAtReadWrite(parent, name)
	if !b.armed || err != nil || name != "index.caj" {
		return fd, err
	}
	if b.indexFD >= 0 {
		panic("generation activation opened index twice")
	}
	b.indexFD = fd
	return fd, nil
}

func (b *linuxIntegrationGenerationActivationCrashBackend) pwrite(fd int, source []byte, offset int64) (int, error) {
	if !b.armed || fd != b.indexFD {
		return b.linuxBackend.pwrite(fd, source, offset)
	}
	if b.written == 0 {
		b.expected = len(source)
		blockLinuxIntegrationCrashBarrier(b.barrier, "before-activation-write")
	}
	if b.written == 0 && b.barrier == "after-short-activation-write" {
		limit := len(source) / 2
		if limit == 0 {
			limit = 1
		}
		count, err := b.linuxBackend.pwrite(fd, source[:limit], offset)
		if count > 0 {
			b.written += count
		}
		if err == nil && count == limit {
			blockLinuxIntegrationCrashBarrier(b.barrier, "after-short-activation-write")
		}
		return count, err
	}
	count, err := b.linuxBackend.pwrite(fd, source, offset)
	if count > 0 {
		b.written += count
	}
	if err == nil && b.written == b.expected {
		blockLinuxIntegrationCrashBarrier(b.barrier, "after-activation-write")
	}
	return count, err
}

func (b *linuxIntegrationGenerationActivationCrashBackend) fdatasync(fd int) error {
	if !b.armed || fd != b.indexFD {
		return b.linuxBackend.fdatasync(fd)
	}
	blockLinuxIntegrationCrashBarrier(b.barrier, "before-activation-fdatasync")
	err := b.linuxBackend.fdatasync(fd)
	if err == nil {
		blockLinuxIntegrationCrashBarrier(b.barrier, "after-activation-fdatasync")
	}
	return err
}

// linuxIntegrationGenerationRotationCrashBackend stays dormant through the
// durable base generation and existing-segment append. Once armed, it binds
// each crash boundary to the exact index, new segment, or journal-directory
// descriptor used by the candidate rotation composite.
type linuxIntegrationGenerationRotationCrashBackend struct {
	linuxBackend
	barrier          string
	armed            bool
	indexFD          int
	segmentFD        int
	journalDirectory int
	indexWrites      linuxIntegrationRotationWriteState
	segmentWrites    linuxIntegrationRotationWriteState
}

type linuxIntegrationRotationWriteState struct {
	completed int
	expected  int
	written   int
}

var _ backend = (*linuxIntegrationGenerationRotationCrashBackend)(nil)

func (b *linuxIntegrationGenerationRotationCrashBackend) arm(barrier string) {
	b.barrier = barrier
	b.indexFD = -1
	b.segmentFD = -1
	b.journalDirectory = -1
	b.indexWrites = linuxIntegrationRotationWriteState{}
	b.segmentWrites = linuxIntegrationRotationWriteState{}
	b.armed = true
}

func (b *linuxIntegrationGenerationRotationCrashBackend) openFileAtReadWrite(parent int, name string) (int, error) {
	fd, err := b.linuxBackend.openFileAtReadWrite(parent, name)
	if !b.armed || err != nil || name != "index.caj" {
		return fd, err
	}
	if b.indexFD >= 0 {
		panic("generation rotation opened index twice")
	}
	b.indexFD = fd
	return fd, nil
}

func (b *linuxIntegrationGenerationRotationCrashBackend) openFileAt(parent int, name string, create bool) (int, error) {
	if !b.armed || !create || name != admissionSegmentName(1) {
		return b.linuxBackend.openFileAt(parent, name, create)
	}
	if b.segmentFD >= 0 || b.journalDirectory >= 0 {
		panic("generation rotation created segment twice")
	}
	blockLinuxIntegrationCrashBarrier(b.barrier, "before-segment-create")
	fd, err := b.linuxBackend.openFileAt(parent, name, create)
	if err != nil {
		return fd, err
	}
	b.segmentFD = fd
	b.journalDirectory = parent
	blockLinuxIntegrationCrashBarrier(b.barrier, "after-segment-create")
	return fd, nil
}

func (b *linuxIntegrationGenerationRotationCrashBackend) pwrite(fd int, source []byte, offset int64) (int, error) {
	if !b.armed {
		return b.linuxBackend.pwrite(fd, source, offset)
	}
	var state *linuxIntegrationRotationWriteState
	var before, short, after string
	switch fd {
	case b.segmentFD:
		state = &b.segmentWrites
		switch state.completed {
		case 0:
			before, short, after = "before-header-write", "after-short-header-write", "after-header-write"
		case 1:
			before, short, after = "before-caller-write", "after-short-caller-write", "after-caller-write"
		default:
			panic("generation rotation wrote segment more than twice")
		}
	case b.indexFD:
		state = &b.indexWrites
		switch state.completed {
		case 0:
			before, short, after = "before-header-checkpoint-write", "after-short-header-checkpoint-write", "after-header-checkpoint-write"
		case 1:
			before, short, after = "before-caller-checkpoint-write", "after-short-caller-checkpoint-write", "after-caller-checkpoint-write"
		default:
			panic("generation rotation wrote index more than twice")
		}
	default:
		return b.linuxBackend.pwrite(fd, source, offset)
	}
	if state.written == 0 {
		state.expected = len(source)
		blockLinuxIntegrationCrashBarrier(b.barrier, before)
	}
	if state.written == 0 && b.barrier == short {
		limit := len(source) / 2
		if limit == 0 {
			limit = 1
		}
		count, err := b.linuxBackend.pwrite(fd, source[:limit], offset)
		if count > 0 {
			state.written += count
		}
		if err == nil && count == limit {
			blockLinuxIntegrationCrashBarrier(b.barrier, short)
		}
		return count, err
	}
	count, err := b.linuxBackend.pwrite(fd, source, offset)
	if count > 0 {
		state.written += count
	}
	if err == nil && state.written == state.expected {
		blockLinuxIntegrationCrashBarrier(b.barrier, after)
		state.completed++
		state.expected = 0
		state.written = 0
	}
	return count, err
}

func (b *linuxIntegrationGenerationRotationCrashBackend) fdatasync(fd int) error {
	if !b.armed {
		return b.linuxBackend.fdatasync(fd)
	}
	before, after := "", ""
	switch fd {
	case b.segmentFD:
		switch b.segmentWrites.completed {
		case 0:
			before, after = "before-empty-fdatasync", "after-empty-fdatasync"
		case 1:
			before, after = "before-header-fdatasync", "after-header-fdatasync"
		case 2:
			before, after = "before-caller-fdatasync", "after-caller-fdatasync"
		default:
			panic("generation rotation synced unexpected segment state")
		}
	case b.indexFD:
		switch b.indexWrites.completed {
		case 1:
			before, after = "before-header-checkpoint-fdatasync", "after-header-checkpoint-fdatasync"
		case 2:
			before, after = "before-caller-checkpoint-fdatasync", "after-caller-checkpoint-fdatasync"
		default:
			panic("generation rotation synced unexpected index state")
		}
	}
	blockLinuxIntegrationCrashBarrier(b.barrier, before)
	err := b.linuxBackend.fdatasync(fd)
	if err == nil {
		blockLinuxIntegrationCrashBarrier(b.barrier, after)
	}
	return err
}

func (b *linuxIntegrationGenerationRotationCrashBackend) fsync(fd int) error {
	if !b.armed || fd != b.journalDirectory {
		return b.linuxBackend.fsync(fd)
	}
	blockLinuxIntegrationCrashBarrier(b.barrier, "before-segment-directory-fsync")
	err := b.linuxBackend.fsync(fd)
	if err == nil {
		blockLinuxIntegrationCrashBarrier(b.barrier, "after-segment-directory-fsync")
	}
	return err
}

func (b *linuxIntegrationCrashBackend) write(fd int, source []byte) (int, error) {
	b.writeCalls++
	if b.writeCalls == 1 {
		b.hit("before-temp-write")
		if b.barrier == "after-short-temp-write" {
			limit := len(source) / 2
			if limit == 0 {
				limit = 1
			}
			count, err := b.linuxBackend.write(fd, source[:limit])
			if err == nil && count == limit {
				b.hit("after-short-temp-write")
			}
			return count, err
		}
	}
	count, err := b.linuxBackend.write(fd, source)
	if b.writeCalls == 1 && err == nil && count == len(source) {
		b.hit("after-temp-write")
	}
	return count, err
}

func (b *linuxIntegrationCrashBackend) fdatasync(fd int) error {
	b.dataSyncCalls++
	before, after := "", ""
	switch b.dataSyncCalls {
	case 1:
		before, after = "before-temp-fdatasync", "after-temp-fdatasync"
	case 2:
		before, after = "before-final-fdatasync", "after-final-fdatasync"
	}
	b.hit(before)
	err := b.linuxBackend.fdatasync(fd)
	if err == nil {
		b.hit(after)
	}
	return err
}

func (b *linuxIntegrationCrashBackend) renameNoReplace(parent int, oldName, newName string) error {
	b.renameCalls++
	if b.renameCalls == 1 {
		b.hit("before-rename")
	}
	err := b.linuxBackend.renameNoReplace(parent, oldName, newName)
	if b.renameCalls == 1 && err == nil {
		b.hit("after-rename")
	}
	return err
}

func (b *linuxIntegrationCrashBackend) fsync(fd int) error {
	b.directoryCalls++
	if b.directoryCalls == 1 {
		b.hit("before-directory-fsync")
	}
	err := b.linuxBackend.fsync(fd)
	if b.directoryCalls == 1 && err == nil {
		b.hit("after-directory-fsync")
	}
	return err
}

func publishLinuxIntegrationObjectAtCrashBarrier(t *testing.T, rootPath, barrier string) {
	t.Helper()
	if !validLinuxIntegrationObjectCrashBarrier(barrier) {
		t.Fatal("unknown object crash barrier")
	}
	ops := &linuxIntegrationCrashBackend{barrier: barrier}
	root, err := newRootWithAuthority(context.Background(), rootPath, uint32(os.Getuid()), ops, mountAuthority{seal: &struct{}{}})
	if err != nil {
		t.Fatal(err)
	}
	lease, err := root.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	scan, err := lease.Scan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	publication, err := lease.Publish(context.Background(), scan, linuxIntegrationDigest, []byte(linuxIntegrationPayload))
	t.Fatalf("publish crossed crash barrier: publication=%v err=%v", publication, err)
}

func classifyLinuxIntegrationObjectCrashState(t *testing.T, rootPath, barrier string) {
	t.Helper()
	if !validLinuxIntegrationObjectCrashBarrier(barrier) {
		t.Fatal("unknown object crash barrier")
	}
	if root, err := Open(context.Background(), rootPath); root != nil || !errors.Is(err, ErrTrustedMountAuthority) {
		t.Fatalf("production Open bypassed trusted mount authority: root=%v err=%v", root, err)
	}
	root := newLinuxIntegrationRoot(t, rootPath)
	lease, err := root.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := lease.Close(); err != nil {
			t.Fatal(err)
		}
	}()
	scan, err := lease.Scan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	finalCount, finalBytes := scan.FinalUsage()
	tempCount, tempBytes := scan.TemporaryUsage()
	hasFinal := scan.HasObject(linuxIntegrationDigest, uint64(len(linuxIntegrationPayload)))
	state := ""
	switch {
	case hasFinal && finalCount == 1 && finalBytes == uint64(len(linuxIntegrationPayload)) && tempCount == 0 && tempBytes == 0:
		state = "final"
	case !hasFinal && finalCount == 0 && finalBytes == 0 && tempCount == 0 && tempBytes == 0:
		state = "absent"
	case !hasFinal && finalCount == 0 && finalBytes == 0 && tempCount == 1 && tempBytes <= uint64(len(linuxIntegrationPayload)):
		state = "temp"
	default:
		t.Fatalf("invalid object crash state: final=%v count=%d/%d temp=%d/%d", hasFinal, finalCount, finalBytes, tempCount, tempBytes)
	}
	if !validLinuxIntegrationObjectCrashState(barrier, state, tempBytes) {
		t.Fatalf("barrier %q rejected recovery state=%q temp_bytes=%d", barrier, state, tempBytes)
	}
	t.Logf("EVIDENCEFS_INTEGRATION_OBJECT_CRASH_RECOVERY barrier=%s state=%s final_count=%d final_bytes=%d temp_count=%d temp_bytes=%d", barrier, state, finalCount, finalBytes, tempCount, tempBytes)
}

func validLinuxIntegrationObjectCrashBarrier(barrier string) bool {
	switch barrier {
	case "before-temp-write", "after-short-temp-write", "after-temp-write",
		"before-temp-fdatasync", "after-temp-fdatasync", "before-rename",
		"after-rename", "before-final-fdatasync", "after-final-fdatasync",
		"before-directory-fsync", "after-directory-fsync":
		return true
	default:
		return false
	}
}

func validLinuxIntegrationObjectCrashState(barrier, state string, tempBytes uint64) bool {
	switch barrier {
	case "before-temp-write", "after-short-temp-write", "after-temp-write", "before-temp-fdatasync":
		return state == "absent" || (state == "temp" && tempBytes <= uint64(len(linuxIntegrationPayload)))
	case "after-temp-fdatasync", "before-rename":
		return state == "absent" || (state == "temp" && tempBytes == uint64(len(linuxIntegrationPayload)))
	case "after-rename", "before-final-fdatasync", "after-final-fdatasync", "before-directory-fsync":
		return state == "absent" || state == "final" || (state == "temp" && tempBytes == uint64(len(linuxIntegrationPayload)))
	case "after-directory-fsync":
		return state == "final"
	default:
		return false
	}
}

func appendLinuxIntegrationGenerationAtCrashBarrier(t *testing.T, rootPath, barrier string) {
	t.Helper()
	if !validLinuxIntegrationGenerationAppendCrashBarrier(barrier) {
		t.Fatal("unknown generation append crash barrier")
	}
	ops := &linuxIntegrationGenerationAppendCrashBackend{}
	generation, snapshot := createLinuxIntegrationBaseGeneration(t, rootPath, ops)
	ops.arm(barrier)
	result, err := generation.AppendExistingSegmentComposite(context.Background(), snapshot, []byte(linuxIntegrationExistingJournalFrame), []byte(linuxIntegrationExistingCheckpointFrame))
	t.Fatalf("generation append crossed crash barrier: outcome=%q snapshot=%v err=%v", result.Outcome(), result.Snapshot(), err)
}

func classifyLinuxIntegrationGenerationAppendCrashState(t *testing.T, rootPath, barrier string) {
	t.Helper()
	if !validLinuxIntegrationGenerationAppendCrashBarrier(barrier) {
		t.Fatal("unknown generation append crash barrier")
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
	target, targetErr := inventory.Target()
	lineageIDs, lineageIDsErr := inventory.LineageIDs()
	lineage, lineageErr := inventory.Lineage(linuxIntegrationTarget)
	if targetErr != nil || lineageIDsErr != nil || lineageErr != nil || target != linuxIntegrationTarget || len(lineageIDs) != 1 || lineageIDs[0] != linuxIntegrationTarget {
		t.Fatalf("target=%x ids=%x errors=%v/%v/%v", target, lineageIDs, targetErr, lineageIDsErr, lineageErr)
	}
	indexView, err := lineage.Index()
	if err != nil {
		t.Fatal(err)
	}
	index, err := indexView.ReadAll(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	journals, err := lineage.Journals()
	if err != nil || len(journals) != 1 {
		t.Fatalf("journals=%d err=%v", len(journals), err)
	}
	journal, err := journals[0].ID()
	if err != nil || journal != linuxIntegrationJournal {
		t.Fatalf("journal=%x err=%v", journal, err)
	}
	segments, err := journals[0].Segments()
	if err != nil || len(segments) != 1 {
		t.Fatalf("segments=%d err=%v", len(segments), err)
	}
	ordinal, ordinalErr := segments[0].Ordinal()
	segment, segmentErr := segments[0].ReadAll(context.Background())
	if ordinalErr != nil || segmentErr != nil || ordinal != 0 {
		t.Fatalf("segment ordinal=%d errors=%v/%v", ordinal, ordinalErr, segmentErr)
	}
	state, ok := classifyLinuxIntegrationGenerationAppendBytes(index, segment)
	if !ok {
		t.Fatalf("invalid generation append crash bytes: index=%q segment=%q", index, segment)
	}
	if !validLinuxIntegrationGenerationAppendCrashState(barrier, state) {
		t.Fatalf("barrier %q rejected generation append recovery state=%q", barrier, state)
	}
	if err := inventory.Revalidate(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Logf("EVIDENCEFS_INTEGRATION_GENERATION_APPEND_CRASH_RECOVERY barrier=%s state=%s index_bytes=%d segment_bytes=%d", barrier, state, len(index), len(segment))
}

func validLinuxIntegrationGenerationAppendCrashBarrier(barrier string) bool {
	switch barrier {
	case "before-journal-write", "after-short-journal-write", "after-journal-write",
		"before-journal-fdatasync", "after-journal-fdatasync", "before-index-write",
		"after-short-index-write", "after-index-write", "before-index-fdatasync",
		"after-index-fdatasync":
		return true
	default:
		return false
	}
}

func classifyLinuxIntegrationGenerationAppendBytes(index, segment []byte) (GenerationAppendReconcileState, bool) {
	indexBase := linuxIntegrationBaseIndex()
	segmentBase := linuxIntegrationBaseSegmentZero()
	if !bytes.HasPrefix(index, indexBase) || !bytes.HasPrefix(segment, segmentBase) {
		return "", false
	}
	journalState, journalOK := generationReconcileSuffixState(segment, uint64(len(segmentBase)), []byte(linuxIntegrationExistingJournalFrame))
	checkpointState, checkpointOK := generationReconcileSuffixState(index, uint64(len(indexBase)), []byte(linuxIntegrationExistingCheckpointFrame))
	if !journalOK || !checkpointOK {
		return "", false
	}
	switch {
	case journalState == generationSuffixAbsent && checkpointState == generationSuffixAbsent:
		return GenerationAppendReconcileUnchanged, true
	case journalState == generationSuffixPartial && checkpointState == generationSuffixAbsent:
		return GenerationAppendReconcileJournalTorn, true
	case journalState == generationSuffixComplete && checkpointState == generationSuffixAbsent:
		return GenerationAppendReconcileJournalComplete, true
	case journalState == generationSuffixComplete && checkpointState == generationSuffixPartial:
		return GenerationAppendReconcileCheckpointTorn, true
	case journalState == generationSuffixComplete && checkpointState == generationSuffixComplete:
		return GenerationAppendReconcileCompositeComplete, true
	default:
		return "", false
	}
}

func validLinuxIntegrationGenerationAppendCrashState(barrier string, state GenerationAppendReconcileState) bool {
	switch barrier {
	case "before-journal-write":
		return state == GenerationAppendReconcileUnchanged
	case "after-short-journal-write":
		return state == GenerationAppendReconcileUnchanged || state == GenerationAppendReconcileJournalTorn
	case "after-journal-write", "before-journal-fdatasync":
		return state == GenerationAppendReconcileUnchanged || state == GenerationAppendReconcileJournalTorn || state == GenerationAppendReconcileJournalComplete
	case "after-journal-fdatasync", "before-index-write":
		return state == GenerationAppendReconcileJournalComplete
	case "after-short-index-write":
		return state == GenerationAppendReconcileJournalComplete || state == GenerationAppendReconcileCheckpointTorn
	case "after-index-write", "before-index-fdatasync":
		return state == GenerationAppendReconcileJournalComplete || state == GenerationAppendReconcileCheckpointTorn || state == GenerationAppendReconcileCompositeComplete
	case "after-index-fdatasync":
		return state == GenerationAppendReconcileCompositeComplete
	default:
		return false
	}
}

type linuxIntegrationGenerationActivationState string

const (
	linuxIntegrationGenerationActivationUnchanged linuxIntegrationGenerationActivationState = "unchanged"
	linuxIntegrationGenerationActivationTorn      linuxIntegrationGenerationActivationState = "activation_torn"
	linuxIntegrationGenerationActivationComplete  linuxIntegrationGenerationActivationState = "activation_complete"
)

func activateLinuxIntegrationGenerationAtCrashBarrier(t *testing.T, rootPath, barrier string) {
	t.Helper()
	if !validLinuxIntegrationGenerationActivationCrashBarrier(barrier) {
		t.Fatal("unknown generation activation crash barrier")
	}
	ops := &linuxIntegrationGenerationActivationCrashBackend{}
	_, inventory := createLinuxIntegrationPreActivation(t, rootPath, ops)
	ops.arm(barrier)
	token, err := inventory.MutationToken()
	if err != nil {
		t.Fatal(err)
	}
	result, err := token.AppendTargetIndex(context.Background(), inventory, []byte(linuxIntegrationActivationIndexFrame))
	t.Fatalf("generation activation crossed crash barrier: outcome=%q inventory=%v err=%v", result.Outcome(), result.Inventory(), err)
}

func classifyLinuxIntegrationGenerationActivationCrashState(t *testing.T, rootPath, barrier string) {
	t.Helper()
	if !validLinuxIntegrationGenerationActivationCrashBarrier(barrier) {
		t.Fatal("unknown generation activation crash barrier")
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
	target, targetErr := inventory.Target()
	lineageIDs, lineageIDsErr := inventory.LineageIDs()
	lineage, lineageErr := inventory.Lineage(linuxIntegrationTarget)
	if targetErr != nil || lineageIDsErr != nil || lineageErr != nil || target != linuxIntegrationTarget || len(lineageIDs) != 1 || lineageIDs[0] != linuxIntegrationTarget {
		t.Fatalf("target=%x ids=%x errors=%v/%v/%v", target, lineageIDs, targetErr, lineageIDsErr, lineageErr)
	}
	indexView, err := lineage.Index()
	if err != nil {
		t.Fatal(err)
	}
	index, err := indexView.ReadAll(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	journals, err := lineage.Journals()
	if err != nil || len(journals) != 1 {
		t.Fatalf("journals=%d err=%v", len(journals), err)
	}
	journal, err := journals[0].ID()
	if err != nil || journal != linuxIntegrationJournal {
		t.Fatalf("journal=%x err=%v", journal, err)
	}
	segments, err := journals[0].Segments()
	if err != nil || len(segments) != 1 {
		t.Fatalf("segments=%d err=%v", len(segments), err)
	}
	ordinal, ordinalErr := segments[0].Ordinal()
	segment, segmentErr := segments[0].ReadAll(context.Background())
	if ordinalErr != nil || segmentErr != nil || ordinal != 0 || !bytes.Equal(segment, linuxIntegrationBaseSegmentZero()) {
		t.Fatalf("segment ordinal=%d bytes=%q errors=%v/%v", ordinal, segment, ordinalErr, segmentErr)
	}
	state, ok := classifyLinuxIntegrationGenerationActivationBytes(index)
	if !ok {
		t.Fatalf("invalid generation activation crash index: %q", index)
	}
	if !validLinuxIntegrationGenerationActivationCrashState(barrier, state) {
		t.Fatalf("barrier %q rejected generation activation recovery state=%q", barrier, state)
	}
	if err := inventory.Revalidate(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Logf("EVIDENCEFS_INTEGRATION_GENERATION_ACTIVATION_CRASH_RECOVERY barrier=%s state=%s index_bytes=%d segment_bytes=%d", barrier, state, len(index), len(segment))
}

func validLinuxIntegrationGenerationActivationCrashBarrier(barrier string) bool {
	switch barrier {
	case "before-activation-write", "after-short-activation-write", "after-activation-write",
		"before-activation-fdatasync", "after-activation-fdatasync":
		return true
	default:
		return false
	}
}

func classifyLinuxIntegrationGenerationActivationBytes(index []byte) (linuxIntegrationGenerationActivationState, bool) {
	base := []byte(linuxIntegrationIndexHeader)
	if !bytes.HasPrefix(index, base) {
		return "", false
	}
	state, ok := generationReconcileSuffixState(index, uint64(len(base)), []byte(linuxIntegrationActivationIndexFrame))
	if !ok {
		return "", false
	}
	switch state {
	case generationSuffixAbsent:
		return linuxIntegrationGenerationActivationUnchanged, true
	case generationSuffixPartial:
		return linuxIntegrationGenerationActivationTorn, true
	case generationSuffixComplete:
		return linuxIntegrationGenerationActivationComplete, true
	default:
		return "", false
	}
}

func validLinuxIntegrationGenerationActivationCrashState(barrier string, state linuxIntegrationGenerationActivationState) bool {
	switch barrier {
	case "before-activation-write":
		return state == linuxIntegrationGenerationActivationUnchanged
	case "after-short-activation-write":
		return state == linuxIntegrationGenerationActivationUnchanged || state == linuxIntegrationGenerationActivationTorn
	case "after-activation-write", "before-activation-fdatasync":
		return state == linuxIntegrationGenerationActivationUnchanged || state == linuxIntegrationGenerationActivationTorn || state == linuxIntegrationGenerationActivationComplete
	case "after-activation-fdatasync":
		return state == linuxIntegrationGenerationActivationComplete
	default:
		return false
	}
}

func rotateLinuxIntegrationGenerationAtCrashBarrier(t *testing.T, rootPath, barrier string) {
	t.Helper()
	if !validLinuxIntegrationGenerationRotationCrashBarrier(barrier) {
		t.Fatal("unknown generation rotation crash barrier")
	}
	ops := &linuxIntegrationGenerationRotationCrashBackend{}
	generation, snapshot := createLinuxIntegrationBaseGeneration(t, rootPath, ops)
	appended, err := generation.AppendExistingSegmentComposite(context.Background(), snapshot, []byte(linuxIntegrationExistingJournalFrame), []byte(linuxIntegrationExistingCheckpointFrame))
	if err != nil || appended.Outcome() != AdmissionTransitionDurable || !appended.ValidFor(generation) {
		t.Fatalf("rotation baseline append outcome=%q valid=%v err=%v", appended.Outcome(), appended.ValidFor(generation), err)
	}
	if err := appended.Snapshot().Revalidate(context.Background()); err != nil {
		t.Fatal(err)
	}
	ops.arm(barrier)
	result, err := generation.AppendRotatedSegmentComposite(context.Background(), appended.Snapshot(), []byte(linuxIntegrationRotationHeaderFrame), []byte(linuxIntegrationRotationCheckpointFrame), []byte(linuxIntegrationRotationCallerFrame), []byte(linuxIntegrationRotationCallerCheckpoint))
	t.Fatalf("generation rotation crossed crash barrier: outcome=%q snapshot=%v err=%v", result.Outcome(), result.Snapshot(), err)
}

func classifyLinuxIntegrationGenerationRotationCrashState(t *testing.T, rootPath, barrier string) {
	t.Helper()
	if !validLinuxIntegrationGenerationRotationCrashBarrier(barrier) {
		t.Fatal("unknown generation rotation crash barrier")
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
	target, targetErr := inventory.Target()
	lineageIDs, lineageIDsErr := inventory.LineageIDs()
	lineage, lineageErr := inventory.Lineage(linuxIntegrationTarget)
	if targetErr != nil || lineageIDsErr != nil || lineageErr != nil || target != linuxIntegrationTarget || len(lineageIDs) != 1 || lineageIDs[0] != linuxIntegrationTarget {
		t.Fatalf("target=%x ids=%x errors=%v/%v/%v", target, lineageIDs, targetErr, lineageIDsErr, lineageErr)
	}
	indexView, err := lineage.Index()
	if err != nil {
		t.Fatal(err)
	}
	index, err := indexView.ReadAll(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	journals, err := lineage.Journals()
	if err != nil || len(journals) != 1 {
		t.Fatalf("journals=%d err=%v", len(journals), err)
	}
	journal, err := journals[0].ID()
	if err != nil || journal != linuxIntegrationJournal {
		t.Fatalf("journal=%x err=%v", journal, err)
	}
	segments, err := journals[0].Segments()
	if err != nil || len(segments) < 1 || len(segments) > 2 {
		t.Fatalf("segments=%d err=%v", len(segments), err)
	}
	segmentZeroOrdinal, segmentZeroOrdinalErr := segments[0].Ordinal()
	segmentZero, segmentZeroErr := segments[0].ReadAll(context.Background())
	if segmentZeroOrdinalErr != nil || segmentZeroErr != nil || segmentZeroOrdinal != 0 {
		t.Fatalf("segment zero ordinal=%d errors=%v/%v", segmentZeroOrdinal, segmentZeroOrdinalErr, segmentZeroErr)
	}
	segmentOnePresent := len(segments) == 2
	var segmentOne []byte
	if segmentOnePresent {
		segmentOneOrdinal, ordinalErr := segments[1].Ordinal()
		segmentOne, err = segments[1].ReadAll(context.Background())
		if ordinalErr != nil || err != nil || segmentOneOrdinal != 1 {
			t.Fatalf("segment one ordinal=%d errors=%v/%v", segmentOneOrdinal, ordinalErr, err)
		}
	}
	state, ok := classifyLinuxIntegrationGenerationRotationBytes(index, segmentZero, segmentOnePresent, segmentOne)
	if !ok {
		t.Fatalf("invalid generation rotation crash bytes: index=%q segment0=%q segment1_present=%v segment1=%q", index, segmentZero, segmentOnePresent, segmentOne)
	}
	if !validLinuxIntegrationGenerationRotationCrashState(barrier, state) {
		t.Fatalf("barrier %q rejected generation rotation recovery state=%q", barrier, state)
	}
	if err := inventory.Revalidate(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Logf("EVIDENCEFS_INTEGRATION_GENERATION_ROTATION_CRASH_RECOVERY barrier=%s state=%s index_bytes=%d segment0_bytes=%d segment1_present=%v segment1_bytes=%d", barrier, state, len(index), len(segmentZero), segmentOnePresent, len(segmentOne))
}

func validLinuxIntegrationGenerationRotationCrashBarrier(barrier string) bool {
	switch barrier {
	case "before-segment-create", "after-segment-create",
		"before-empty-fdatasync", "after-empty-fdatasync",
		"before-segment-directory-fsync", "after-segment-directory-fsync",
		"before-header-write", "after-short-header-write", "after-header-write",
		"before-header-fdatasync", "after-header-fdatasync",
		"before-header-checkpoint-write", "after-short-header-checkpoint-write", "after-header-checkpoint-write",
		"before-header-checkpoint-fdatasync", "after-header-checkpoint-fdatasync",
		"before-caller-write", "after-short-caller-write", "after-caller-write",
		"before-caller-fdatasync", "after-caller-fdatasync",
		"before-caller-checkpoint-write", "after-short-caller-checkpoint-write", "after-caller-checkpoint-write",
		"before-caller-checkpoint-fdatasync", "after-caller-checkpoint-fdatasync":
		return true
	default:
		return false
	}
}

func classifyLinuxIntegrationGenerationRotationBytes(index, segmentZero []byte, segmentOnePresent bool, segmentOne []byte) (GenerationRotationReconcileState, bool) {
	indexBase := linuxIntegrationRotationBaseIndex()
	if !bytes.HasPrefix(index, indexBase) || !bytes.Equal(segmentZero, linuxIntegrationExpectedSegmentZero()) {
		return "", false
	}
	indexState, indexOK := classifyGenerationRotationSuffix(index, uint64(len(indexBase)), []byte(linuxIntegrationRotationCheckpointFrame), []byte(linuxIntegrationRotationCallerCheckpoint))
	if !indexOK {
		return "", false
	}
	if !segmentOnePresent {
		if indexState == generationRotationSuffixAbsent {
			return GenerationRotationReconcileSegmentAbsent, true
		}
		return "", false
	}
	segmentState, segmentOK := classifyGenerationRotationSuffix(segmentOne, 0, []byte(linuxIntegrationRotationHeaderFrame), []byte(linuxIntegrationRotationCallerFrame))
	if !segmentOK {
		return "", false
	}
	switch {
	case segmentState == generationRotationSuffixAbsent && indexState == generationRotationSuffixAbsent:
		return GenerationRotationReconcileSegmentEmpty, true
	case segmentState == generationRotationSuffixFirstPartial && indexState == generationRotationSuffixAbsent:
		return GenerationRotationReconcileHeaderTorn, true
	case segmentState == generationRotationSuffixFirstComplete && indexState == generationRotationSuffixAbsent:
		return GenerationRotationReconcileHeaderComplete, true
	case segmentState == generationRotationSuffixFirstComplete && indexState == generationRotationSuffixFirstPartial:
		return GenerationRotationReconcileHeaderCheckpointTorn, true
	case segmentState == generationRotationSuffixFirstComplete && indexState == generationRotationSuffixFirstComplete:
		return GenerationRotationReconcileHeaderCompositeComplete, true
	case segmentState == generationRotationSuffixSecondPartial && indexState == generationRotationSuffixFirstComplete:
		return GenerationRotationReconcileCallerTorn, true
	case segmentState == generationRotationSuffixSecondComplete && indexState == generationRotationSuffixFirstComplete:
		return GenerationRotationReconcileCallerComplete, true
	case segmentState == generationRotationSuffixSecondComplete && indexState == generationRotationSuffixSecondPartial:
		return GenerationRotationReconcileCallerCheckpointTorn, true
	case segmentState == generationRotationSuffixSecondComplete && indexState == generationRotationSuffixSecondComplete:
		return GenerationRotationReconcileCompositeComplete, true
	default:
		return "", false
	}
}

func validLinuxIntegrationGenerationRotationCrashState(barrier string, state GenerationRotationReconcileState) bool {
	switch barrier {
	case "before-segment-create":
		return state == GenerationRotationReconcileSegmentAbsent
	case "after-segment-create", "before-empty-fdatasync", "after-empty-fdatasync", "before-segment-directory-fsync":
		return state == GenerationRotationReconcileSegmentAbsent || state == GenerationRotationReconcileSegmentEmpty
	case "after-segment-directory-fsync", "before-header-write":
		return state == GenerationRotationReconcileSegmentEmpty
	case "after-short-header-write":
		return state == GenerationRotationReconcileSegmentEmpty || state == GenerationRotationReconcileHeaderTorn
	case "after-header-write", "before-header-fdatasync":
		return state == GenerationRotationReconcileSegmentEmpty || state == GenerationRotationReconcileHeaderTorn || state == GenerationRotationReconcileHeaderComplete
	case "after-header-fdatasync", "before-header-checkpoint-write":
		return state == GenerationRotationReconcileHeaderComplete
	case "after-short-header-checkpoint-write":
		return state == GenerationRotationReconcileHeaderComplete || state == GenerationRotationReconcileHeaderCheckpointTorn
	case "after-header-checkpoint-write", "before-header-checkpoint-fdatasync":
		return state == GenerationRotationReconcileHeaderComplete || state == GenerationRotationReconcileHeaderCheckpointTorn || state == GenerationRotationReconcileHeaderCompositeComplete
	case "after-header-checkpoint-fdatasync", "before-caller-write":
		return state == GenerationRotationReconcileHeaderCompositeComplete
	case "after-short-caller-write":
		return state == GenerationRotationReconcileHeaderCompositeComplete || state == GenerationRotationReconcileCallerTorn
	case "after-caller-write", "before-caller-fdatasync":
		return state == GenerationRotationReconcileHeaderCompositeComplete || state == GenerationRotationReconcileCallerTorn || state == GenerationRotationReconcileCallerComplete
	case "after-caller-fdatasync", "before-caller-checkpoint-write":
		return state == GenerationRotationReconcileCallerComplete
	case "after-short-caller-checkpoint-write":
		return state == GenerationRotationReconcileCallerComplete || state == GenerationRotationReconcileCallerCheckpointTorn
	case "after-caller-checkpoint-write", "before-caller-checkpoint-fdatasync":
		return state == GenerationRotationReconcileCallerComplete || state == GenerationRotationReconcileCallerCheckpointTorn || state == GenerationRotationReconcileCompositeComplete
	case "after-caller-checkpoint-fdatasync":
		return state == GenerationRotationReconcileCompositeComplete
	default:
		return false
	}
}

type linuxIntegrationHolder struct {
	command *exec.Cmd
	stdin   io.WriteCloser
	stderr  bytes.Buffer
	waited  bool
}

func startLinuxIntegrationHolder(t *testing.T, mode, ready string) *linuxIntegrationHolder {
	t.Helper()
	holder := &linuxIntegrationHolder{command: integrationHelperCommand(mode)}
	var err error
	holder.stdin, err = holder.command.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := holder.command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	holder.command.Stderr = &holder.stderr
	if err := holder.command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(holder.cleanup)
	scanner := bufio.NewScanner(stdout)
	if !scanner.Scan() || scanner.Text() != ready {
		_ = holder.command.Process.Kill()
		_ = holder.command.Wait()
		holder.waited = true
		t.Fatalf("helper %q did not reach its durable lock state: line=%q scan_err=%v stderr=%q", mode, scanner.Text(), scanner.Err(), holder.stderr.String())
	}
	return holder
}

func (h *linuxIntegrationHolder) kill(t *testing.T, label string) {
	t.Helper()
	if err := h.command.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = h.stdin.Close()
	if err := h.command.Wait(); err == nil {
		t.Fatalf("%s unexpectedly exited cleanly instead of being killed", label)
	}
	h.waited = true
}

func (h *linuxIntegrationHolder) cleanup() {
	_ = h.stdin.Close()
	if h.command.Process != nil && !h.waited {
		_ = h.command.Process.Kill()
		_ = h.command.Wait()
		h.waited = true
	}
}

func integrationHelperCommand(mode string) *exec.Cmd {
	command := exec.Command(os.Args[0], "-test.run=^TestLinuxIntegrationDurabilityRestartAndCrossProcessLocks$", "-test.count=1")
	command.Env = append(os.Environ(), linuxIntegrationHelperEnv+"="+mode)
	return command
}

func verifyLinuxIntegrationInFreshProcess(t *testing.T, mode, subject string) {
	t.Helper()
	command := integrationHelperCommand(mode)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("fresh verifier process rejected %s: err=%v output=%q", subject, err, output)
	}
}

func publishAndHoldLinuxIntegrationObject(t *testing.T, rootPath string) {
	root := newLinuxIntegrationRoot(t, rootPath)
	lease, err := root.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	scan, err := lease.Scan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	publication, err := lease.Publish(context.Background(), scan, linuxIntegrationDigest, []byte(linuxIntegrationPayload))
	if err != nil {
		t.Fatal(err)
	}
	if err := lease.BindPublication(publication, linuxIntegrationDigest, uint64(len(linuxIntegrationPayload))); err != nil {
		t.Fatal(err)
	}
	if _, err := fmt.Fprintln(os.Stdout, linuxIntegrationObjectReady); err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(io.Discard, os.Stdin); err != nil {
		t.Fatal(err)
	}
	if err := lease.Close(); err != nil {
		t.Fatal(err)
	}
}

func createAndHoldLinuxIntegrationGeneration(t *testing.T, rootPath string) {
	generation, snapshot := createLinuxIntegrationBaseGeneration(t, rootPath, linuxBackend{})
	appended, err := generation.AppendExistingSegmentComposite(context.Background(), snapshot, []byte(linuxIntegrationExistingJournalFrame), []byte(linuxIntegrationExistingCheckpointFrame))
	if err != nil || appended.Outcome() != AdmissionTransitionDurable || !appended.ValidFor(generation) {
		t.Fatalf("existing append outcome=%q valid=%v err=%v", appended.Outcome(), appended.ValidFor(generation), err)
	}
	rotated, err := generation.AppendRotatedSegmentComposite(context.Background(), appended.Snapshot(), []byte(linuxIntegrationRotationHeaderFrame), []byte(linuxIntegrationRotationCheckpointFrame), []byte(linuxIntegrationRotationCallerFrame), []byte(linuxIntegrationRotationCallerCheckpoint))
	if err != nil || rotated.Outcome() != AdmissionTransitionDurable || !rotated.ValidFor(generation) {
		t.Fatalf("rotation outcome=%q valid=%v err=%v", rotated.Outcome(), rotated.ValidFor(generation), err)
	}
	verifyLinuxIntegrationSnapshot(t, rotated.Snapshot())
	if err := rotated.Snapshot().Revalidate(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := fmt.Fprintln(os.Stdout, linuxIntegrationGenerationReady); err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(io.Discard, os.Stdin); err != nil {
		t.Fatal(err)
	}
	if err := generation.Close(); err != nil {
		t.Fatal(err)
	}
}

func createLinuxIntegrationPreActivation(t *testing.T, rootPath string, ops backend) (*AdmissionLease, *AdmissionInventory) {
	t.Helper()
	root, err := newRootWithAuthority(context.Background(), rootPath, uint32(os.Getuid()), ops, mountAuthority{seal: &struct{}{}})
	if err != nil {
		t.Fatal(err)
	}
	admission, inventory, err := root.AcquireAdmission(context.Background(), linuxIntegrationTarget)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if admission != nil {
			_ = admission.Close()
		}
	})
	token, err := inventory.MutationToken()
	if err != nil {
		t.Fatal(err)
	}
	registered, err := token.CreateTargetLineage(context.Background(), inventory, []byte(linuxIntegrationIndexHeader))
	if err != nil || registered.Outcome() != AdmissionTransitionDurable || registered.Inventory() == nil {
		t.Fatalf("pre-activation target registration outcome=%q inventory=%v err=%v", registered.Outcome(), registered.Inventory(), err)
	}
	inventory = registered.Inventory()
	token, err = inventory.MutationToken()
	if err != nil {
		t.Fatal(err)
	}
	created, err := token.CreateGenerationHeader(context.Background(), inventory, linuxIntegrationJournal, []byte(linuxIntegrationGenerationHeader))
	if err != nil || created.Outcome() != AdmissionTransitionDurable || created.Inventory() == nil || !created.ValidFor(created.Inventory()) {
		t.Fatalf("pre-activation generation create outcome=%q inventory=%v valid=%v err=%v", created.Outcome(), created.Inventory(), created.ValidFor(created.Inventory()), err)
	}
	return admission, created.Inventory()
}

func createLinuxIntegrationBaseGeneration(t *testing.T, rootPath string, ops backend) (*GenerationLease, *GenerationSnapshot) {
	t.Helper()
	root, err := newRootWithAuthority(context.Background(), rootPath, uint32(os.Getuid()), ops, mountAuthority{seal: &struct{}{}})
	if err != nil {
		t.Fatal(err)
	}
	admission, inventory, err := root.AcquireAdmission(context.Background(), linuxIntegrationTarget)
	if err != nil {
		t.Fatal(err)
	}
	var generation *GenerationLease
	t.Cleanup(func() {
		if generation != nil {
			_ = generation.Close()
		} else if admission != nil {
			_ = admission.Close()
		}
	})

	token, err := inventory.MutationToken()
	if err != nil {
		t.Fatal(err)
	}
	registered, err := token.CreateTargetLineage(context.Background(), inventory, []byte(linuxIntegrationIndexHeader))
	if err != nil || registered.Outcome() != AdmissionTransitionDurable || registered.Inventory() == nil {
		t.Fatalf("target registration outcome=%q inventory=%v err=%v", registered.Outcome(), registered.Inventory(), err)
	}
	inventory = registered.Inventory()

	token, err = inventory.MutationToken()
	if err != nil {
		t.Fatal(err)
	}
	created, err := token.CreateGenerationHeader(context.Background(), inventory, linuxIntegrationJournal, []byte(linuxIntegrationGenerationHeader))
	if err != nil || created.Outcome() != AdmissionTransitionDurable || created.Inventory() == nil || !created.ValidFor(created.Inventory()) {
		t.Fatalf("generation create outcome=%q inventory=%v valid=%v err=%v", created.Outcome(), created.Inventory(), created.ValidFor(created.Inventory()), err)
	}
	inventory = created.Inventory()

	token, err = inventory.MutationToken()
	if err != nil {
		t.Fatal(err)
	}
	activated, err := token.AppendTargetIndex(context.Background(), inventory, []byte(linuxIntegrationActivationIndexFrame))
	if err != nil || activated.Outcome() != AdmissionTransitionDurable || activated.Inventory() == nil {
		t.Fatalf("activation append outcome=%q inventory=%v err=%v", activated.Outcome(), activated.Inventory(), err)
	}
	inventory = activated.Inventory()

	token, err = inventory.MutationToken()
	if err != nil {
		t.Fatal(err)
	}
	generation, err = token.HandoffGeneration(context.Background(), inventory, linuxIntegrationJournal)
	if err != nil || generation == nil || !generation.Active() || admission.Active() {
		t.Fatalf("handoff generation=%v active=%v admission_active=%v err=%v", generation, generation != nil && generation.Active(), admission.Active(), err)
	}
	snapshot, err := generation.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return generation, snapshot
}

func verifyLinuxIntegrationSnapshot(t *testing.T, snapshot *GenerationSnapshot) {
	t.Helper()
	index, indexErr := snapshot.ReadIndex(context.Background())
	count, countErr := snapshot.SegmentCount()
	segmentZero, segmentZeroErr := snapshot.ReadSegment(context.Background(), 0)
	segmentOne, segmentOneErr := snapshot.ReadSegment(context.Background(), 1)
	if indexErr != nil || countErr != nil || segmentZeroErr != nil || segmentOneErr != nil || count != 2 ||
		!bytes.Equal(index, linuxIntegrationExpectedIndex()) || !bytes.Equal(segmentZero, linuxIntegrationExpectedSegmentZero()) || !bytes.Equal(segmentOne, linuxIntegrationExpectedSegmentOne()) {
		t.Fatalf("snapshot index=%q segments=%d/%q/%q errors=%v/%v/%v/%v", index, count, segmentZero, segmentOne, indexErr, countErr, segmentZeroErr, segmentOneErr)
	}
}

func verifyLinuxIntegrationObject(t *testing.T, rootPath string) {
	t.Helper()
	root := newLinuxIntegrationRoot(t, rootPath)
	lease, err := root.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := lease.Close(); err != nil {
			t.Fatal(err)
		}
	}()
	scan, err := lease.Scan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !scan.HasObject(linuxIntegrationDigest, uint64(len(linuxIntegrationPayload))) {
		t.Fatal("durable integration object was absent or mismatched")
	}
}

func verifyLinuxIntegrationGeneration(t *testing.T, rootPath string) {
	t.Helper()
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
	target, targetErr := inventory.Target()
	lineageIDs, lineageIDsErr := inventory.LineageIDs()
	lineage, lineageErr := inventory.Lineage(linuxIntegrationTarget)
	if targetErr != nil || lineageIDsErr != nil || lineageErr != nil || target != linuxIntegrationTarget || len(lineageIDs) != 1 || lineageIDs[0] != linuxIntegrationTarget {
		t.Fatalf("target=%x ids=%x errors=%v/%v/%v", target, lineageIDs, targetErr, lineageIDsErr, lineageErr)
	}
	indexView, err := lineage.Index()
	if err != nil {
		t.Fatal(err)
	}
	index, err := indexView.ReadAll(context.Background())
	if err != nil || !bytes.Equal(index, linuxIntegrationExpectedIndex()) {
		t.Fatalf("index=%q err=%v", index, err)
	}
	journals, err := lineage.Journals()
	if err != nil || len(journals) != 1 {
		t.Fatalf("journals=%d err=%v", len(journals), err)
	}
	journal, err := journals[0].ID()
	if err != nil || journal != linuxIntegrationJournal {
		t.Fatalf("journal=%x err=%v", journal, err)
	}
	segments, err := journals[0].Segments()
	if err != nil || len(segments) != 2 {
		t.Fatalf("segments=%d err=%v", len(segments), err)
	}
	wants := [][]byte{linuxIntegrationExpectedSegmentZero(), linuxIntegrationExpectedSegmentOne()}
	for ordinal, segment := range segments {
		gotOrdinal, ordinalErr := segment.Ordinal()
		raw, readErr := segment.ReadAll(context.Background())
		if ordinalErr != nil || readErr != nil || gotOrdinal != uint32(ordinal) || !bytes.Equal(raw, wants[ordinal]) {
			t.Fatalf("segment=%d ordinal=%d bytes=%q errors=%v/%v", ordinal, gotOrdinal, raw, ordinalErr, readErr)
		}
	}
	if err := inventory.Revalidate(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func linuxIntegrationExpectedIndex() []byte {
	return []byte(linuxIntegrationIndexHeader + linuxIntegrationActivationIndexFrame + linuxIntegrationExistingCheckpointFrame + linuxIntegrationRotationCheckpointFrame + linuxIntegrationRotationCallerCheckpoint)
}

func linuxIntegrationBaseIndex() []byte {
	return []byte(linuxIntegrationIndexHeader + linuxIntegrationActivationIndexFrame)
}

func linuxIntegrationRotationBaseIndex() []byte {
	return []byte(linuxIntegrationIndexHeader + linuxIntegrationActivationIndexFrame + linuxIntegrationExistingCheckpointFrame)
}

func linuxIntegrationExpectedSegmentZero() []byte {
	return []byte(linuxIntegrationGenerationHeader + linuxIntegrationExistingJournalFrame)
}

func linuxIntegrationBaseSegmentZero() []byte {
	return []byte(linuxIntegrationGenerationHeader)
}

func linuxIntegrationExpectedSegmentOne() []byte {
	return []byte(linuxIntegrationRotationHeaderFrame + linuxIntegrationRotationCallerFrame)
}

func newLinuxIntegrationRoot(t *testing.T, rootPath string) *Root {
	t.Helper()
	root, err := newRootWithAuthority(context.Background(), rootPath, uint32(os.Getuid()), linuxBackend{}, mountAuthority{seal: &struct{}{}})
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func requireLinuxIntegrationMount(t *testing.T, rootPath, filesystem string) int {
	t.Helper()
	expected := int64(0)
	switch filesystem {
	case "ext4":
		expected = unix.EXT4_SUPER_MAGIC
	case "xfs":
		expected = unix.XFS_SUPER_MAGIC
	default:
		t.Fatal("integration filesystem must be ext4 or xfs")
	}
	var statfs unix.Statfs_t
	if err := unix.Statfs(rootPath, &statfs); err != nil || int64(statfs.Type) != expected {
		t.Fatalf("integration filesystem mismatch: type=%#x expected=%#x err=%v", statfs.Type, expected, err)
	}
	_, mountID, err := unix.NameToHandleAt(unix.AT_FDCWD, rootPath, 0)
	if err != nil || mountID <= 0 {
		t.Fatalf("integration mount identity is unavailable: mount_id=%d err=%v", mountID, err)
	}
	return mountID
}
