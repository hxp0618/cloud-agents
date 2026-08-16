//go:build linux && evidencefsintegration

package evidencefs

import (
	"bytes"
	"context"
	"errors"
	"testing"
)

const linuxIntegrationRepairCheckpointFrame = "cloud-agents-evidencefs-generation-repair-checkpoint-frame-v1"

type linuxIntegrationGenerationResyncCrashBackend struct {
	linuxBackend
	barrier   string
	armed     bool
	indexFD   int
	segmentFD int
}

var _ backend = (*linuxIntegrationGenerationResyncCrashBackend)(nil)

func (b *linuxIntegrationGenerationResyncCrashBackend) arm(barrier string) {
	b.barrier = barrier
	b.indexFD = -1
	b.segmentFD = -1
	b.armed = true
}

func (b *linuxIntegrationGenerationResyncCrashBackend) openFileAtReadWrite(parent int, name string) (int, error) {
	fd, err := b.linuxBackend.openFileAtReadWrite(parent, name)
	if !b.armed || err != nil {
		return fd, err
	}
	switch name {
	case "index.caj":
		b.indexFD = fd
	case admissionSegmentName(0):
		b.segmentFD = fd
	}
	return fd, nil
}

func (b *linuxIntegrationGenerationResyncCrashBackend) fdatasync(fd int) error {
	if !b.armed {
		return b.linuxBackend.fdatasync(fd)
	}
	before, after := "", ""
	switch fd {
	case b.segmentFD:
		before, after = "before-resync-segment-fdatasync", "after-resync-segment-fdatasync"
	case b.indexFD:
		before, after = "before-resync-index-fdatasync", "after-resync-index-fdatasync"
	default:
		return b.linuxBackend.fdatasync(fd)
	}
	blockLinuxIntegrationCrashBarrier(b.barrier, before)
	err := b.linuxBackend.fdatasync(fd)
	if err == nil {
		blockLinuxIntegrationCrashBarrier(b.barrier, after)
	}
	return err
}

type linuxIntegrationGenerationTruncateCrashBackend struct {
	linuxBackend
	barrier   string
	armed     bool
	indexFD   int
	segmentFD int
}

var _ backend = (*linuxIntegrationGenerationTruncateCrashBackend)(nil)

func (b *linuxIntegrationGenerationTruncateCrashBackend) arm(barrier string) {
	b.barrier = barrier
	b.indexFD = -1
	b.segmentFD = -1
	b.armed = true
}

func (b *linuxIntegrationGenerationTruncateCrashBackend) openFileAtReadWrite(parent int, name string) (int, error) {
	fd, err := b.linuxBackend.openFileAtReadWrite(parent, name)
	if !b.armed || err != nil {
		return fd, err
	}
	switch name {
	case "index.caj":
		b.indexFD = fd
	case admissionSegmentName(0):
		b.segmentFD = fd
	}
	return fd, nil
}

func (b *linuxIntegrationGenerationTruncateCrashBackend) truncate(fd int, size int64) error {
	if !b.armed {
		return b.linuxBackend.truncate(fd, size)
	}
	before, after := "", ""
	switch fd {
	case b.segmentFD:
		before, after = "before-truncate-segment", "after-truncate-segment"
	case b.indexFD:
		before, after = "before-truncate-index", "after-truncate-index"
	default:
		return b.linuxBackend.truncate(fd, size)
	}
	blockLinuxIntegrationCrashBarrier(b.barrier, before)
	err := b.linuxBackend.truncate(fd, size)
	if err == nil {
		blockLinuxIntegrationCrashBarrier(b.barrier, after)
	}
	return err
}

func (b *linuxIntegrationGenerationTruncateCrashBackend) fdatasync(fd int) error {
	if !b.armed {
		return b.linuxBackend.fdatasync(fd)
	}
	before, after := "", ""
	switch fd {
	case b.segmentFD:
		before, after = "before-truncate-segment-fdatasync", "after-truncate-segment-fdatasync"
	case b.indexFD:
		before, after = "before-truncate-index-fdatasync", "after-truncate-index-fdatasync"
	default:
		return b.linuxBackend.fdatasync(fd)
	}
	blockLinuxIntegrationCrashBarrier(b.barrier, before)
	err := b.linuxBackend.fdatasync(fd)
	if err == nil {
		blockLinuxIntegrationCrashBarrier(b.barrier, after)
	}
	return err
}

type linuxIntegrationGenerationCheckpointCrashBackend struct {
	linuxBackend
	barrier  string
	armed    bool
	indexFD  int
	expected int
	written  int
}

var _ backend = (*linuxIntegrationGenerationCheckpointCrashBackend)(nil)

func (b *linuxIntegrationGenerationCheckpointCrashBackend) arm(barrier string) {
	b.barrier = barrier
	b.indexFD = -1
	b.expected = 0
	b.written = 0
	b.armed = true
}

func (b *linuxIntegrationGenerationCheckpointCrashBackend) openFileAtReadWrite(parent int, name string) (int, error) {
	fd, err := b.linuxBackend.openFileAtReadWrite(parent, name)
	if b.armed && err == nil && name == "index.caj" {
		b.indexFD = fd
	}
	return fd, err
}

func (b *linuxIntegrationGenerationCheckpointCrashBackend) pwrite(fd int, source []byte, offset int64) (int, error) {
	if !b.armed || fd != b.indexFD {
		return b.linuxBackend.pwrite(fd, source, offset)
	}
	if b.written == 0 {
		b.expected = len(source)
		blockLinuxIntegrationCrashBarrier(b.barrier, "before-checkpoint-write")
	}
	if b.written == 0 && b.barrier == "after-short-checkpoint-write" {
		limit := len(source) / 2
		if limit == 0 {
			limit = 1
		}
		count, err := b.linuxBackend.pwrite(fd, source[:limit], offset)
		if count > 0 {
			b.written += count
		}
		if err == nil && count == limit {
			blockLinuxIntegrationCrashBarrier(b.barrier, "after-short-checkpoint-write")
		}
		return count, err
	}
	count, err := b.linuxBackend.pwrite(fd, source, offset)
	if count > 0 {
		b.written += count
	}
	if err == nil && b.written == b.expected {
		blockLinuxIntegrationCrashBarrier(b.barrier, "after-checkpoint-write")
	}
	return count, err
}

func (b *linuxIntegrationGenerationCheckpointCrashBackend) fdatasync(fd int) error {
	if !b.armed || fd != b.indexFD {
		return b.linuxBackend.fdatasync(fd)
	}
	blockLinuxIntegrationCrashBarrier(b.barrier, "before-checkpoint-fdatasync")
	err := b.linuxBackend.fdatasync(fd)
	if err == nil {
		blockLinuxIntegrationCrashBarrier(b.barrier, "after-checkpoint-fdatasync")
	}
	return err
}

type linuxIntegrationGenerationDiscardCrashBackend struct {
	linuxBackend
	phase            string
	barrier          string
	segmentFD        int
	journalDirectory int
	failedHeader     bool
}

var _ backend = (*linuxIntegrationGenerationDiscardCrashBackend)(nil)

func (b *linuxIntegrationGenerationDiscardCrashBackend) prepareUnknown() {
	b.phase = "prepare"
	b.segmentFD = -1
	b.journalDirectory = -1
	b.failedHeader = false
}

func (b *linuxIntegrationGenerationDiscardCrashBackend) arm(barrier string) {
	b.phase = "discard"
	b.barrier = barrier
	b.journalDirectory = -1
}

func (b *linuxIntegrationGenerationDiscardCrashBackend) openFileAt(parent int, name string, create bool) (int, error) {
	fd, err := b.linuxBackend.openFileAt(parent, name, create)
	if b.phase == "prepare" && create && err == nil && name == admissionSegmentName(1) {
		b.segmentFD = fd
	}
	return fd, err
}

func (b *linuxIntegrationGenerationDiscardCrashBackend) pwrite(fd int, source []byte, offset int64) (int, error) {
	if b.phase == "prepare" && fd == b.segmentFD && !b.failedHeader {
		b.failedHeader = true
		return 0, errors.New("injected durable empty segment response loss")
	}
	return b.linuxBackend.pwrite(fd, source, offset)
}

func (b *linuxIntegrationGenerationDiscardCrashBackend) unlinkAt(parent int, name string) error {
	if b.phase != "discard" || name != admissionSegmentName(1) {
		return b.linuxBackend.unlinkAt(parent, name)
	}
	b.journalDirectory = parent
	blockLinuxIntegrationCrashBarrier(b.barrier, "before-discard-unlink")
	err := b.linuxBackend.unlinkAt(parent, name)
	if err == nil {
		blockLinuxIntegrationCrashBarrier(b.barrier, "after-discard-unlink")
	}
	return err
}

func (b *linuxIntegrationGenerationDiscardCrashBackend) fsync(fd int) error {
	if b.phase != "discard" || fd != b.journalDirectory {
		return b.linuxBackend.fsync(fd)
	}
	blockLinuxIntegrationCrashBarrier(b.barrier, "before-discard-directory-fsync")
	err := b.linuxBackend.fsync(fd)
	if err == nil {
		blockLinuxIntegrationCrashBarrier(b.barrier, "after-discard-directory-fsync")
	}
	return err
}

func resyncLinuxIntegrationGenerationAtCrashBarrier(t *testing.T, rootPath, barrier string) {
	t.Helper()
	if !validLinuxIntegrationGenerationResyncCrashBarrier(barrier) {
		t.Fatal("unknown generation resync crash barrier")
	}
	ops := &linuxIntegrationGenerationResyncCrashBackend{}
	generation, snapshot := createLinuxIntegrationBaseGeneration(t, rootPath, ops)
	ops.arm(barrier)
	result, err := generation.ResyncGenerationSnapshot(context.Background(), snapshot)
	t.Fatalf("generation resync crossed crash barrier: outcome=%q snapshot=%v err=%v", result.Outcome(), result.Snapshot(), err)
}

func truncateLinuxIntegrationGenerationAtCrashBarrier(t *testing.T, rootPath, barrier string) {
	t.Helper()
	if !validLinuxIntegrationGenerationTruncateCrashBarrier(barrier) {
		t.Fatal("unknown generation truncate crash barrier")
	}
	ops := &linuxIntegrationGenerationTruncateCrashBackend{}
	generation, snapshot := createLinuxIntegrationBaseGeneration(t, rootPath, ops)
	appended, err := generation.AppendExistingSegmentComposite(context.Background(), snapshot, []byte(linuxIntegrationExistingJournalFrame), []byte(linuxIntegrationExistingCheckpointFrame))
	if err != nil || appended.Outcome() != AdmissionTransitionDurable || !appended.ValidFor(generation) {
		t.Fatalf("truncate baseline append outcome=%q valid=%v err=%v", appended.Outcome(), appended.ValidFor(generation), err)
	}
	ops.arm(barrier)
	result, err := generation.TruncateGenerationTails(context.Background(), appended.Snapshot(), uint64(len(linuxIntegrationBaseSegmentZero())), uint64(len(linuxIntegrationBaseIndex())))
	t.Fatalf("generation truncate crossed crash barrier: outcome=%q snapshot=%v err=%v", result.Outcome(), result.Snapshot(), err)
}

func checkpointLinuxIntegrationGenerationAtCrashBarrier(t *testing.T, rootPath, barrier string) {
	t.Helper()
	if !validLinuxIntegrationGenerationCheckpointCrashBarrier(barrier) {
		t.Fatal("unknown generation checkpoint crash barrier")
	}
	ops := &linuxIntegrationGenerationCheckpointCrashBackend{}
	generation, snapshot := createLinuxIntegrationBaseGeneration(t, rootPath, ops)
	ops.arm(barrier)
	result, err := generation.AppendGenerationCheckpoint(context.Background(), snapshot, []byte(linuxIntegrationRepairCheckpointFrame))
	t.Fatalf("generation checkpoint crossed crash barrier: outcome=%q snapshot=%v err=%v", result.Outcome(), result.Snapshot(), err)
}

func discardLinuxIntegrationGenerationAtCrashBarrier(t *testing.T, rootPath, barrier string) {
	t.Helper()
	if !validLinuxIntegrationGenerationDiscardCrashBarrier(barrier) {
		t.Fatal("unknown generation discard crash barrier")
	}
	ops := &linuxIntegrationGenerationDiscardCrashBackend{}
	generation, snapshot := createLinuxIntegrationBaseGeneration(t, rootPath, ops)
	ops.prepareUnknown()
	rotation, err := generation.AppendRotatedSegmentComposite(context.Background(), snapshot, []byte(linuxIntegrationRotationHeaderFrame), []byte(linuxIntegrationRotationCheckpointFrame), []byte(linuxIntegrationRotationCallerFrame), []byte(linuxIntegrationRotationCallerCheckpoint))
	if !errors.Is(err, ErrUnknown) || rotation.Outcome() != AdmissionTransitionUnknown {
		t.Fatalf("discard preparation outcome=%q err=%v", rotation.Outcome(), err)
	}
	state, err := rotation.Reconcile(context.Background(), generation)
	if err != nil || state != GenerationRotationReconcileSegmentEmpty {
		t.Fatalf("discard preparation reconcile=%q err=%v", state, err)
	}
	ops.arm(barrier)
	result, err := rotation.DiscardIncompleteSegment(context.Background(), generation)
	t.Fatalf("generation discard crossed crash barrier: outcome=%q snapshot=%v err=%v", result.Outcome(), result.Snapshot(), err)
}

func classifyLinuxIntegrationGenerationRepairCrashState(t *testing.T, rootPath, scenario, barrier string) {
	t.Helper()
	if !validLinuxIntegrationGenerationRepairCrashBarrier(scenario, barrier) {
		t.Fatal("unknown generation repair crash barrier")
	}
	if root, err := Open(context.Background(), rootPath); root != nil || !errors.Is(err, ErrTrustedMountAuthority) {
		t.Fatalf("production Open bypassed trusted mount authority: root=%v err=%v", root, err)
	}
	index, segments := readLinuxIntegrationGenerationRepairFiles(t, rootPath)
	state, ok := classifyLinuxIntegrationGenerationRepairBytes(scenario, index, segments)
	if !ok {
		t.Fatalf("invalid generation repair bytes: scenario=%s index=%q segments=%q", scenario, index, segments)
	}
	if !validLinuxIntegrationGenerationRepairCrashState(scenario, barrier, state) {
		t.Fatalf("scenario=%q barrier=%q rejected generation repair state=%q", scenario, barrier, state)
	}
	t.Logf("EVIDENCEFS_INTEGRATION_GENERATION_REPAIR_CRASH_RECOVERY scenario=%s barrier=%s state=%s index_bytes=%d segments=%d", scenario, barrier, state, len(index), len(segments))
}

func readLinuxIntegrationGenerationRepairFiles(t *testing.T, rootPath string) ([]byte, [][]byte) {
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
	lineage, lineageErr := inventory.Lineage(linuxIntegrationTarget)
	if targetErr != nil || lineageErr != nil || target != linuxIntegrationTarget {
		t.Fatalf("target=%x errors=%v/%v", target, targetErr, lineageErr)
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
	views, err := journals[0].Segments()
	if err != nil || len(views) < 1 || len(views) > 2 {
		t.Fatalf("segments=%d err=%v", len(views), err)
	}
	segments := make([][]byte, len(views))
	for ordinal, view := range views {
		gotOrdinal, ordinalErr := view.Ordinal()
		raw, readErr := view.ReadAll(context.Background())
		if ordinalErr != nil || readErr != nil || gotOrdinal != uint32(ordinal) {
			t.Fatalf("segment=%d ordinal=%d errors=%v/%v", ordinal, gotOrdinal, ordinalErr, readErr)
		}
		segments[ordinal] = raw
	}
	if err := inventory.Revalidate(context.Background()); err != nil {
		t.Fatal(err)
	}
	return index, segments
}

func classifyLinuxIntegrationGenerationRepairBytes(scenario string, index []byte, segments [][]byte) (string, bool) {
	switch scenario {
	case "resync":
		if bytes.Equal(index, linuxIntegrationBaseIndex()) && len(segments) == 1 && bytes.Equal(segments[0], linuxIntegrationBaseSegmentZero()) {
			return "complete", true
		}
	case "truncate":
		if len(segments) != 1 {
			return "", false
		}
		indexLong := bytes.Equal(index, linuxIntegrationRotationBaseIndex())
		indexShort := bytes.Equal(index, linuxIntegrationBaseIndex())
		segmentLong := bytes.Equal(segments[0], linuxIntegrationExpectedSegmentZero())
		segmentShort := bytes.Equal(segments[0], linuxIntegrationBaseSegmentZero())
		switch {
		case indexLong && segmentLong:
			return "unchanged", true
		case indexLong && segmentShort:
			return "segment_truncated", true
		case indexShort && segmentShort:
			return "both_truncated", true
		}
	case "checkpoint":
		if len(segments) != 1 || !bytes.Equal(segments[0], linuxIntegrationBaseSegmentZero()) || !bytes.HasPrefix(index, linuxIntegrationBaseIndex()) {
			return "", false
		}
		suffix := index[len(linuxIntegrationBaseIndex()):]
		checkpoint := []byte(linuxIntegrationRepairCheckpointFrame)
		switch {
		case len(suffix) == 0:
			return "unchanged", true
		case len(suffix) < len(checkpoint) && bytes.Equal(suffix, checkpoint[:len(suffix)]):
			return "checkpoint_torn", true
		case bytes.Equal(suffix, checkpoint):
			return "checkpoint_complete", true
		}
	case "discard":
		if !bytes.Equal(index, linuxIntegrationBaseIndex()) || len(segments) < 1 || len(segments) > 2 || !bytes.Equal(segments[0], linuxIntegrationBaseSegmentZero()) {
			return "", false
		}
		if len(segments) == 1 {
			return "segment_absent", true
		}
		if len(segments[1]) == 0 {
			return "segment_empty", true
		}
	}
	return "", false
}

func validLinuxIntegrationGenerationRepairCrashBarrier(scenario, barrier string) bool {
	switch scenario {
	case "resync":
		return validLinuxIntegrationGenerationResyncCrashBarrier(barrier)
	case "truncate":
		return validLinuxIntegrationGenerationTruncateCrashBarrier(barrier)
	case "checkpoint":
		return validLinuxIntegrationGenerationCheckpointCrashBarrier(barrier)
	case "discard":
		return validLinuxIntegrationGenerationDiscardCrashBarrier(barrier)
	default:
		return false
	}
}

func validLinuxIntegrationGenerationResyncCrashBarrier(barrier string) bool {
	switch barrier {
	case "before-resync-segment-fdatasync", "after-resync-segment-fdatasync",
		"before-resync-index-fdatasync", "after-resync-index-fdatasync":
		return true
	default:
		return false
	}
}

func validLinuxIntegrationGenerationTruncateCrashBarrier(barrier string) bool {
	switch barrier {
	case "before-truncate-segment", "after-truncate-segment",
		"before-truncate-segment-fdatasync", "after-truncate-segment-fdatasync",
		"before-truncate-index", "after-truncate-index",
		"before-truncate-index-fdatasync", "after-truncate-index-fdatasync":
		return true
	default:
		return false
	}
}

func validLinuxIntegrationGenerationCheckpointCrashBarrier(barrier string) bool {
	switch barrier {
	case "before-checkpoint-write", "after-short-checkpoint-write", "after-checkpoint-write",
		"before-checkpoint-fdatasync", "after-checkpoint-fdatasync":
		return true
	default:
		return false
	}
}

func validLinuxIntegrationGenerationDiscardCrashBarrier(barrier string) bool {
	switch barrier {
	case "before-discard-unlink", "after-discard-unlink",
		"before-discard-directory-fsync", "after-discard-directory-fsync":
		return true
	default:
		return false
	}
}

func validLinuxIntegrationGenerationRepairCrashState(scenario, barrier, state string) bool {
	switch scenario {
	case "resync":
		return state == "complete"
	case "truncate":
		switch barrier {
		case "before-truncate-segment":
			return state == "unchanged"
		case "after-truncate-segment", "before-truncate-segment-fdatasync":
			return state == "unchanged" || state == "segment_truncated"
		case "after-truncate-segment-fdatasync", "before-truncate-index":
			return state == "segment_truncated"
		case "after-truncate-index", "before-truncate-index-fdatasync":
			return state == "segment_truncated" || state == "both_truncated"
		case "after-truncate-index-fdatasync":
			return state == "both_truncated"
		}
	case "checkpoint":
		switch barrier {
		case "before-checkpoint-write":
			return state == "unchanged"
		case "after-short-checkpoint-write":
			return state == "unchanged" || state == "checkpoint_torn"
		case "after-checkpoint-write", "before-checkpoint-fdatasync":
			return state == "unchanged" || state == "checkpoint_torn" || state == "checkpoint_complete"
		case "after-checkpoint-fdatasync":
			return state == "checkpoint_complete"
		}
	case "discard":
		switch barrier {
		case "before-discard-unlink":
			return state == "segment_empty"
		case "after-discard-unlink", "before-discard-directory-fsync":
			return state == "segment_empty" || state == "segment_absent"
		case "after-discard-directory-fsync":
			return state == "segment_absent"
		}
	}
	return false
}
