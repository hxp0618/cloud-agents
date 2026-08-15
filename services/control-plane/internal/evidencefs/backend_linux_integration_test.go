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
	linuxIntegrationVerifyEnv   = "CLOUD_AGENTS_EVIDENCEFS_VERIFY_EXISTING"

	linuxIntegrationObjectReady     = "EVIDENCEFS_INTEGRATION_OBJECT_PUBLISHED_AND_ROOT_LOCKED"
	linuxIntegrationGenerationReady = "EVIDENCEFS_INTEGRATION_GENERATION_DURABLE_AND_LOCKED"
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
	root := newLinuxIntegrationRoot(t, rootPath)
	admission, inventory, err := root.AcquireAdmission(context.Background(), linuxIntegrationTarget)
	if err != nil {
		t.Fatal(err)
	}
	var generation *GenerationLease
	defer func() {
		if generation != nil {
			_ = generation.Close()
		} else if admission != nil {
			_ = admission.Close()
		}
	}()

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
	generation = nil
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

func linuxIntegrationExpectedSegmentZero() []byte {
	return []byte(linuxIntegrationGenerationHeader + linuxIntegrationExistingJournalFrame)
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
