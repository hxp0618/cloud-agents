package evidencefs

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"testing"
)

func TestGenerationRotatedSegmentCompositeDurableOwnsPayloadsAndOrder(t *testing.T) {
	f, _, lease, target, journal := generationLeaseForSnapshot(t, [][]byte{[]byte("segment-zero")})
	snapshot, err := lease.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	previousIdentity, _ := snapshot.IdentityDigest()
	oldIndex, _ := snapshot.IndexBytes()
	oldSegment, _ := snapshot.SegmentBytes(0)
	header, headerCheckpoint := []byte("rotation-header"), []byte("rotation-checkpoint")
	caller, callerCheckpoint := []byte("caller-frame"), []byte("caller-checkpoint")
	wantHeader, wantHeaderCheckpoint := append([]byte(nil), header...), append([]byte(nil), headerCheckpoint...)
	wantCaller, wantCallerCheckpoint := append([]byte(nil), caller...), append([]byte(nil), callerCheckpoint...)
	baselineDataSyncs, baselineSyncs := f.fdatasyncs, f.fsyncs
	baselineDataNames, baselineSyncNames := len(f.fdatasyncNames), len(f.fsyncNames)
	f.partialWrite = 1
	firstWrite := f.writes + 1
	f.onWrite = func(_ *fakeBackend, _ *fakeNode, call int) {
		if call == firstWrite {
			clear(header)
			clear(headerCheckpoint)
			clear(caller)
			clear(callerCheckpoint)
		}
	}

	result, err := lease.AppendRotatedSegmentComposite(context.Background(), snapshot, header, headerCheckpoint, caller, callerCheckpoint)
	if err != nil {
		t.Fatal(err)
	}
	next := result.Snapshot()
	nextIdentity, identityErr := next.IdentityDigest()
	index, indexErr := next.IndexBytes()
	first, firstErr := next.SegmentBytes(0)
	second, secondErr := next.SegmentBytes(1)
	wantIndex := append(append(append([]byte(nil), oldIndex...), wantHeaderCheckpoint...), wantCallerCheckpoint...)
	wantSecond := append(append([]byte(nil), wantHeader...), wantCaller...)
	wantDataNames := []string{admissionSegmentName(1), admissionSegmentName(1), "index.caj", admissionSegmentName(1), "index.caj"}
	journalName := fmt.Sprintf("%x", journal)
	if result.Outcome() != AdmissionTransitionDurable || !result.ValidFor(lease) || identityErr != nil || indexErr != nil || firstErr != nil || secondErr != nil ||
		result.PreviousSnapshotIdentity() != previousIdentity || result.NextSnapshotIdentity() != nextIdentity || result.SegmentOrdinal() != 1 || result.IndexPreviousSize() != uint64(len(oldIndex)) ||
		result.RotationHeaderFramedDigest() != sha256.Sum256(wantHeader) || result.RotationCheckpointFramedDigest() != sha256.Sum256(wantHeaderCheckpoint) || result.CallerFramedDigest() != sha256.Sum256(wantCaller) || result.CallerCheckpointFramedDigest() != sha256.Sum256(wantCallerCheckpoint) ||
		!bytes.Equal(index, wantIndex) || !bytes.Equal(first, oldSegment) || !bytes.Equal(second, wantSecond) || f.fdatasyncs-baselineDataSyncs != len(wantDataNames) || !equalStrings(f.fdatasyncNames[baselineDataNames:], wantDataNames) || f.fsyncs-baselineSyncs != 1 || len(f.fsyncNames) != baselineSyncNames+1 || f.fsyncNames[baselineSyncNames] != journalName ||
		result.previousIndex.stat.size != 0 || result.previousSegments != nil || result.rotationHeaderFramed != nil || result.rotationCheckpointFramed != nil || result.callerFramed != nil || result.callerCheckpointFramed != nil {
		t.Fatalf("outcome=%q valid=%v identities=%x/%x ordinal=%d syncs=%v fsync=%v bytes=%q/%q/%q errs=%v/%v/%v/%v", result.Outcome(), result.ValidFor(lease), result.PreviousSnapshotIdentity(), result.NextSnapshotIdentity(), result.SegmentOrdinal(), f.fdatasyncNames[baselineDataNames:], f.fsyncNames[baselineSyncNames:], first, second, index, identityErr, indexErr, firstErr, secondErr)
	}
	if _, err := snapshot.IndexFact(); !errors.Is(err, ErrLeaseInvalid) {
		t.Fatalf("old snapshot survived rotation: %v", err)
	}
	if err := lease.Close(); err != nil || len(f.handles) != 0 {
		t.Fatalf("close=%v handles=%d", err, len(f.handles))
	}
	_, _ = target, journal
}

func TestGenerationRotatedSegmentCompositePreflightPreservesAuthority(t *testing.T) {
	tests := []struct {
		name   string
		ctx    func() context.Context
		header func() []byte
		hcp    func() []byte
		caller func() []byte
		ccp    func() []byte
		want   error
	}{
		{name: "canceled", ctx: canceledContextForGenerationAppend, header: bytesValue("h"), hcp: bytesValue("hc"), caller: bytesValue("c"), ccp: bytesValue("cc"), want: context.Canceled},
		{name: "empty-header", ctx: context.Background, header: bytesValue(""), hcp: bytesValue("hc"), caller: bytesValue("c"), ccp: bytesValue("cc"), want: ErrInvalidInput},
		{name: "empty-header-checkpoint", ctx: context.Background, header: bytesValue("h"), hcp: bytesValue(""), caller: bytesValue("c"), ccp: bytesValue("cc"), want: ErrInvalidInput},
		{name: "empty-caller", ctx: context.Background, header: bytesValue("h"), hcp: bytesValue("hc"), caller: bytesValue(""), ccp: bytesValue("cc"), want: ErrInvalidInput},
		{name: "empty-caller-checkpoint", ctx: context.Background, header: bytesValue("h"), hcp: bytesValue("hc"), caller: bytesValue("c"), ccp: bytesValue(""), want: ErrInvalidInput},
		{name: "segment-limit", ctx: context.Background, header: sizedBytes(maximumAdmissionSegmentBytes), hcp: bytesValue("hc"), caller: bytesValue("c"), ccp: bytesValue("cc"), want: ErrLimit},
		{name: "index-limit", ctx: context.Background, header: bytesValue("h"), hcp: sizedBytes(maximumAdmissionIndexBytes), caller: bytesValue("c"), ccp: bytesValue("cc"), want: ErrLimit},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			f, _, lease, target, journal := generationLeaseForSnapshot(t, [][]byte{[]byte("header")})
			snapshot, err := lease.Snapshot(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			index, segment := generationAppendNodes(f, target, journal, 0)
			oldIndex, oldSegment := append([]byte(nil), index.data...), append([]byte(nil), segment.data...)
			writes, dataSyncs, syncs := f.writes, f.fdatasyncs, f.fsyncs
			result, err := lease.AppendRotatedSegmentComposite(test.ctx(), snapshot, test.header(), test.hcp(), test.caller(), test.ccp())
			generation := generationNode(f, target, journal)
			if !errors.Is(err, test.want) || result.Outcome() != AdmissionTransitionPreMutationFailure || result.Snapshot() != nil || result.ValidFor(lease) || !lease.OwnsSnapshot(snapshot) || !lease.Active() || f.writes != writes || f.fdatasyncs != dataSyncs || f.fsyncs != syncs || generation.children[admissionSegmentName(1)] != nil || !bytes.Equal(index.data, oldIndex) || !bytes.Equal(segment.data, oldSegment) {
				t.Fatalf("err=%v outcome=%q owns=%v active=%v writes=%d/%d dataSyncs=%d/%d syncs=%d/%d", err, result.Outcome(), lease.OwnsSnapshot(snapshot), lease.Active(), f.writes, writes, f.fdatasyncs, dataSyncs, f.fsyncs, syncs)
			}
			if err := lease.Close(); err != nil || len(f.handles) != 0 {
				t.Fatalf("close=%v handles=%d", err, len(f.handles))
			}
		})
	}
}

func TestGenerationRotatedSegmentCompositeMutationFailuresAreUnknown(t *testing.T) {
	tests := []struct {
		name       string
		fault      func(*fakeBackend)
		segment    []byte
		checkpoint []byte
		reconcile  GenerationRotationReconcileState
	}{
		{name: "create", fault: func(f *fakeBackend) { f.failOpenNames[admissionSegmentName(1)] = errors.New("create") }, reconcile: GenerationRotationReconcileSegmentAbsent},
		{name: "empty-sync", fault: func(f *fakeBackend) { f.failFdatasyncAt = f.fdatasyncs + 1 }, reconcile: GenerationRotationReconcileSegmentEmpty},
		{name: "directory-sync", fault: func(f *fakeBackend) { f.failFsyncAt = f.fsyncs + 1 }, reconcile: GenerationRotationReconcileSegmentEmpty},
		{name: "header-write", fault: func(f *fakeBackend) { f.failWriteAt = f.writes + 1 }, reconcile: GenerationRotationReconcileSegmentEmpty},
		{name: "header-sync", fault: func(f *fakeBackend) { f.failFdatasyncAt = f.fdatasyncs + 2 }, segment: []byte("header"), reconcile: GenerationRotationReconcileHeaderComplete},
		{name: "header-checkpoint-write", fault: func(f *fakeBackend) { f.failWriteAt = f.writes + 2 }, segment: []byte("header"), reconcile: GenerationRotationReconcileHeaderComplete},
		{name: "header-checkpoint-sync", fault: func(f *fakeBackend) { f.failFdatasyncAt = f.fdatasyncs + 3 }, segment: []byte("header"), checkpoint: []byte("header-checkpoint"), reconcile: GenerationRotationReconcileHeaderCompositeComplete},
		{name: "caller-write", fault: func(f *fakeBackend) { f.failWriteAt = f.writes + 3 }, segment: []byte("header"), checkpoint: []byte("header-checkpoint"), reconcile: GenerationRotationReconcileHeaderCompositeComplete},
		{name: "caller-sync", fault: func(f *fakeBackend) { f.failFdatasyncAt = f.fdatasyncs + 4 }, segment: []byte("headercaller"), checkpoint: []byte("header-checkpoint"), reconcile: GenerationRotationReconcileCallerComplete},
		{name: "caller-checkpoint-write", fault: func(f *fakeBackend) { f.failWriteAt = f.writes + 4 }, segment: []byte("headercaller"), checkpoint: []byte("header-checkpoint"), reconcile: GenerationRotationReconcileCallerComplete},
		{name: "caller-checkpoint-sync", fault: func(f *fakeBackend) { f.failFdatasyncAt = f.fdatasyncs + 5 }, segment: []byte("headercaller"), checkpoint: []byte("header-checkpointcaller-checkpoint"), reconcile: GenerationRotationReconcileCompositeComplete},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			f, _, lease, target, journal := generationLeaseForSnapshot(t, [][]byte{[]byte("segment-zero")})
			snapshot, err := lease.Snapshot(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			oldIndex, _ := snapshot.IndexBytes()
			previousIdentity, _ := snapshot.IdentityDigest()
			baselineHandles := len(f.handles)
			test.fault(f)
			result, err := lease.AppendRotatedSegmentComposite(context.Background(), snapshot, []byte("header"), []byte("header-checkpoint"), []byte("caller"), []byte("caller-checkpoint"))
			if !errors.Is(err, ErrUnknown) || result.Outcome() != AdmissionTransitionUnknown || result.Snapshot() != nil || result.ValidFor(lease) || result.PreviousSnapshotIdentity() != previousIdentity || result.SegmentOrdinal() != 1 || result.IndexPreviousSize() != uint64(len(oldIndex)) || result.rotationHeaderFramed == nil || result.rotationCheckpointFramed == nil || result.callerFramed == nil || result.callerCheckpointFramed == nil || len(f.handles) != baselineHandles || lease.OwnsSnapshot(snapshot) || !lease.Active() {
				t.Fatalf("err=%v outcome=%q valid=%v active=%v owns=%v handles=%d/%d", err, result.Outcome(), result.ValidFor(lease), lease.Active(), lease.OwnsSnapshot(snapshot), len(f.handles), baselineHandles)
			}
			generation := generationNode(f, target, journal)
			newSegment := generation.children[admissionSegmentName(1)]
			if test.segment == nil {
				if newSegment != nil && len(newSegment.data) != 0 {
					t.Fatalf("segment=%q want absent/empty", newSegment.data)
				}
			} else if newSegment == nil || !bytes.Equal(newSegment.data, test.segment) {
				t.Fatalf("segment=%v want=%q", newSegment, test.segment)
			}
			index := generationAppendIndexNode(f, target)
			wantIndex := append([]byte(nil), oldIndex...)
			wantIndex = append(wantIndex, test.checkpoint...)
			if !bytes.Equal(index.data, wantIndex) {
				t.Fatalf("index=%q want=%q", index.data, wantIndex)
			}
			delete(f.failOpenNames, admissionSegmentName(1))
			state, reconcileErr := result.Reconcile(context.Background(), lease)
			if reconcileErr != nil || state != test.reconcile || len(f.handles) != baselineHandles {
				t.Fatalf("reconcile=%q want=%q err=%v handles=%d/%d", state, test.reconcile, reconcileErr, len(f.handles), baselineHandles)
			}
			if err := lease.Close(); err != nil || len(f.handles) != 0 {
				t.Fatalf("close=%v handles=%d", err, len(f.handles))
			}
		})
	}
}

func TestGenerationRotationReconcileClassifiesEveryPartialSuffix(t *testing.T) {
	for _, test := range []struct {
		name      string
		header    []byte
		headerCP  []byte
		caller    []byte
		callerCP  []byte
		failWrite int
		want      GenerationRotationReconcileState
	}{
		{name: "header-torn", header: []byte("header"), headerCP: []byte("hcp"), caller: []byte("c"), callerCP: []byte("ccp"), failWrite: 2, want: GenerationRotationReconcileHeaderTorn},
		{name: "header-checkpoint-torn", header: []byte("hdr"), headerCP: []byte("header-checkpoint"), caller: []byte("c"), callerCP: []byte("ccp"), failWrite: 3, want: GenerationRotationReconcileHeaderCheckpointTorn},
		{name: "caller-torn", header: []byte("hdr"), headerCP: []byte("hcp"), caller: []byte("caller"), callerCP: []byte("ccp"), failWrite: 4, want: GenerationRotationReconcileCallerTorn},
		{name: "caller-checkpoint-torn", header: []byte("hdr"), headerCP: []byte("hcp"), caller: []byte("cal"), callerCP: []byte("caller-checkpoint"), failWrite: 5, want: GenerationRotationReconcileCallerCheckpointTorn},
	} {
		t.Run(test.name, func(t *testing.T) {
			f, _, lease, _, _ := generationLeaseForSnapshot(t, [][]byte{[]byte("segment-zero")})
			snapshot, err := lease.Snapshot(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			f.partialWrite = 3
			f.failWriteAt = f.writes + test.failWrite
			result, err := lease.AppendRotatedSegmentComposite(context.Background(), snapshot, test.header, test.headerCP, test.caller, test.callerCP)
			if !errors.Is(err, ErrUnknown) || result.Outcome() != AdmissionTransitionUnknown || !lease.Active() {
				t.Fatalf("err=%v outcome=%q active=%v", err, result.Outcome(), lease.Active())
			}
			state, err := result.Reconcile(context.Background(), lease)
			if err != nil || state != test.want {
				t.Fatalf("state=%q want=%q err=%v", state, test.want, err)
			}
			if err := lease.Close(); err != nil || len(f.handles) != 0 {
				t.Fatalf("close=%v handles=%d", err, len(f.handles))
			}
		})
	}
}

func TestGenerationRotationDiscardRestoresOnlyEmptyOrTornCreatedSegment(t *testing.T) {
	for _, test := range []struct {
		name  string
		fault func(*fakeBackend)
		want  GenerationRotationReconcileState
	}{
		{name: "empty", fault: func(f *fakeBackend) { f.failFdatasyncAt = f.fdatasyncs + 1 }, want: GenerationRotationReconcileSegmentEmpty},
		{name: "header-torn", fault: func(f *fakeBackend) {
			f.partialWrite = 3
			f.failWriteAt = f.writes + 2
		}, want: GenerationRotationReconcileHeaderTorn},
	} {
		t.Run(test.name, func(t *testing.T) {
			f, _, lease, target, journal := generationLeaseForSnapshot(t, [][]byte{[]byte("segment-zero")})
			snapshot, err := lease.Snapshot(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			oldIdentity, _ := snapshot.IdentityDigest()
			oldIndex, _ := snapshot.IndexBytes()
			oldSegment, _ := snapshot.SegmentBytes(0)
			test.fault(f)
			rotation, err := lease.AppendRotatedSegmentComposite(context.Background(), snapshot, []byte("header"), []byte("header-checkpoint"), []byte("caller"), []byte("caller-checkpoint"))
			if !errors.Is(err, ErrUnknown) {
				t.Fatal(err)
			}
			if state, reconcileErr := rotation.Reconcile(context.Background(), lease); reconcileErr != nil || state != test.want {
				t.Fatalf("state=%q want=%q err=%v", state, test.want, reconcileErr)
			}
			unlinks, syncs := f.unlinks, f.fsyncs
			discarded, err := rotation.DiscardIncompleteSegment(context.Background(), lease)
			if err != nil || discarded.Outcome() != AdmissionTransitionDurable || !discarded.ValidFor(lease) || discarded.PreviousSnapshotIdentity() != oldIdentity || discarded.NextSnapshotIdentity() != oldIdentity || discarded.SegmentOrdinal() != 1 || generationNode(f, target, journal).children[admissionSegmentName(1)] != nil || f.unlinks != unlinks+1 || f.fsyncs != syncs+1 {
				t.Fatalf("err=%v outcome=%q valid=%v identities=%x/%x unlink=%d/%d fsync=%d/%d", err, discarded.Outcome(), discarded.ValidFor(lease), discarded.PreviousSnapshotIdentity(), discarded.NextSnapshotIdentity(), f.unlinks, unlinks, f.fsyncs, syncs)
			}
			next := discarded.Snapshot()
			index, indexErr := next.IndexBytes()
			segment, segmentErr := next.SegmentBytes(0)
			if indexErr != nil || segmentErr != nil || !bytes.Equal(index, oldIndex) || !bytes.Equal(segment, oldSegment) {
				t.Fatalf("restored bytes=%q/%q errors=%v/%v", index, segment, indexErr, segmentErr)
			}
			if state, reconcileErr := rotation.Reconcile(context.Background(), lease); reconcileErr != nil || state != GenerationRotationReconcileSegmentAbsent {
				t.Fatalf("post-discard state=%q err=%v", state, reconcileErr)
			}
			if err := lease.Close(); err != nil || len(f.handles) != 0 {
				t.Fatalf("close=%v handles=%d", err, len(f.handles))
			}
		})
	}
}

func TestGenerationRotationDiscardUnknownRemainsClassifiable(t *testing.T) {
	for _, test := range []struct {
		name       string
		fault      func(*fakeBackend)
		want       GenerationRotationReconcileState
		segmentOut bool
	}{
		{name: "unlink", fault: func(f *fakeBackend) { f.failUnlinkAt = f.unlinks + 1 }, want: GenerationRotationReconcileSegmentEmpty},
		{name: "directory-sync", fault: func(f *fakeBackend) { f.failFsyncAt = f.fsyncs + 1 }, want: GenerationRotationReconcileSegmentAbsent, segmentOut: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			f, _, lease, target, journal := generationLeaseForSnapshot(t, [][]byte{[]byte("segment-zero")})
			snapshot, err := lease.Snapshot(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			f.failFdatasyncAt = f.fdatasyncs + 1
			rotation, err := lease.AppendRotatedSegmentComposite(context.Background(), snapshot, []byte("header"), []byte("header-checkpoint"), []byte("caller"), []byte("caller-checkpoint"))
			if !errors.Is(err, ErrUnknown) {
				t.Fatal(err)
			}
			test.fault(f)
			discarded, err := rotation.DiscardIncompleteSegment(context.Background(), lease)
			if !errors.Is(err, ErrUnknown) || discarded.Outcome() != AdmissionTransitionUnknown || discarded.Snapshot() != nil || !lease.Active() {
				t.Fatalf("err=%v outcome=%q active=%v", err, discarded.Outcome(), lease.Active())
			}
			present := generationNode(f, target, journal).children[admissionSegmentName(1)] != nil
			if present == test.segmentOut {
				t.Fatalf("segment present=%v want=%v", present, !test.segmentOut)
			}
			state, reconcileErr := rotation.Reconcile(context.Background(), lease)
			if reconcileErr != nil || state != test.want {
				t.Fatalf("state=%q want=%q err=%v", state, test.want, reconcileErr)
			}
			if err := lease.Close(); err != nil || len(f.handles) != 0 {
				t.Fatalf("close=%v handles=%d", err, len(f.handles))
			}
		})
	}
}

func TestGenerationRotationDiscardRejectsCompletedHeader(t *testing.T) {
	f, _, lease, _, _ := generationLeaseForSnapshot(t, [][]byte{[]byte("segment-zero")})
	snapshot, err := lease.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	f.failFdatasyncAt = f.fdatasyncs + 2
	rotation, err := lease.AppendRotatedSegmentComposite(context.Background(), snapshot, []byte("header"), []byte("header-checkpoint"), []byte("caller"), []byte("caller-checkpoint"))
	if !errors.Is(err, ErrUnknown) {
		t.Fatal(err)
	}
	unlinks := f.unlinks
	discarded, err := rotation.DiscardIncompleteSegment(context.Background(), lease)
	if !errors.Is(err, ErrInvalidInput) || discarded.Outcome() != AdmissionTransitionPreMutationFailure || f.unlinks != unlinks || !lease.Active() {
		t.Fatalf("err=%v outcome=%q unlinks=%d/%d active=%v", err, discarded.Outcome(), f.unlinks, unlinks, lease.Active())
	}
	if err := lease.Close(); err != nil || len(f.handles) != 0 {
		t.Fatalf("close=%v handles=%d", err, len(f.handles))
	}
}

func TestGenerationRotationReconcileRejectsDifferentOrUnorderedState(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*fakeBackend, [32]byte, [32]byte)
	}{
		{name: "different-new-suffix", mutate: func(f *fakeBackend, target, journal [32]byte) {
			segment := generationNode(f, target, journal).children[admissionSegmentName(1)]
			segment.data[len(segment.data)-1] ^= 1
		}},
		{name: "old-segment-drift", mutate: func(f *fakeBackend, target, journal [32]byte) {
			segment := generationNode(f, target, journal).children[admissionSegmentName(0)]
			segment.data[0] ^= 1
		}},
		{name: "checkpoint-ahead", mutate: func(f *fakeBackend, target, _ [32]byte) {
			index := generationAppendIndexNode(f, target)
			index.data = append(index.data, []byte("header-checkpointcaller-checkpoint")...)
			index.stat.size = uint64(len(index.data))
		}},
		{name: "extra-segment", mutate: func(f *fakeBackend, target, journal [32]byte) {
			generationNode(f, target, journal).children[admissionSegmentName(2)] = f.regular(admissionSegmentName(2), []byte("extra"))
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			f, _, lease, target, journal := generationLeaseForSnapshot(t, [][]byte{[]byte("segment-zero")})
			snapshot, err := lease.Snapshot(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			f.failFdatasyncAt = f.fdatasyncs + 2
			result, err := lease.AppendRotatedSegmentComposite(context.Background(), snapshot, []byte("header"), []byte("header-checkpoint"), []byte("caller"), []byte("caller-checkpoint"))
			if !errors.Is(err, ErrUnknown) {
				t.Fatal(err)
			}
			test.mutate(f, target, journal)
			if state, err := result.Reconcile(context.Background(), lease); state != "" || !errors.Is(err, ErrCorrupt) || lease.Active() {
				t.Fatalf("state=%q err=%v active=%v", state, err, lease.Active())
			}
			if err := lease.Close(); err != nil || len(f.handles) != 0 {
				t.Fatalf("close=%v handles=%d", err, len(f.handles))
			}
		})
	}
}

func TestGenerationRotatedSegmentCompositePreMutationDriftRevokesLease(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*fakeBackend, [32]byte, [32]byte)
	}{
		{name: "same-size-segment", mutate: func(f *fakeBackend, target, journal [32]byte) {
			_, segment := generationAppendNodes(f, target, journal, 0)
			segment.data[0] ^= 1
		}},
		{name: "generation-lock", mutate: func(f *fakeBackend, target, journal [32]byte) {
			generation := generationNode(f, target, journal)
			generation.children["writer.lock"] = f.regular("writer.lock", nil)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			f, store, lease, target, journal := generationLeaseForSnapshot(t, [][]byte{[]byte("segment-zero")})
			snapshot, err := lease.Snapshot(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			test.mutate(f, target, journal)
			writes := f.writes
			result, err := lease.AppendRotatedSegmentComposite(context.Background(), snapshot, []byte("header"), []byte("header-checkpoint"), []byte("caller"), []byte("caller-checkpoint"))
			if err == nil || errors.Is(err, ErrUnknown) || result.Outcome() != AdmissionTransitionPreMutationFailure || result.Snapshot() != nil || lease.Active() || lease.OwnsSnapshot(snapshot) || f.writes != writes || generationNode(f, target, journal).children[admissionSegmentName(1)] != nil || !store.usable() {
				t.Fatalf("err=%v outcome=%q active=%v owns=%v writes=%d/%d usable=%v", err, result.Outcome(), lease.Active(), lease.OwnsSnapshot(snapshot), f.writes, writes, store.usable())
			}
			if err := lease.Close(); err != nil || len(f.handles) != 0 {
				t.Fatalf("close=%v handles=%d", err, len(f.handles))
			}
		})
	}
}

func TestGenerationRotatedSegmentCompositeCleanupFailurePoisonsStore(t *testing.T) {
	f, store, lease, _, _ := generationLeaseForSnapshot(t, [][]byte{[]byte("segment-zero")})
	snapshot, err := lease.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	baselineHandles := len(f.handles)
	f.failCloseNames[admissionSegmentName(1)] = true
	result, err := lease.AppendRotatedSegmentComposite(context.Background(), snapshot, []byte("header"), []byte("header-checkpoint"), []byte("caller"), []byte("caller-checkpoint"))
	if !errors.Is(err, ErrUnknown) || !errors.Is(err, ErrFilesystem) || result.Outcome() != AdmissionTransitionUnknown || result.Snapshot() != nil || lease.Active() || store.usable() || len(f.handles) != baselineHandles {
		t.Fatalf("err=%v outcome=%q active=%v usable=%v handles=%d/%d", err, result.Outcome(), lease.Active(), store.usable(), len(f.handles), baselineHandles)
	}
	delete(f.failCloseNames, admissionSegmentName(1))
	if err := lease.Close(); err != nil || len(f.handles) != 0 {
		t.Fatalf("close=%v handles=%d", err, len(f.handles))
	}
}

func generationNode(f *fakeBackend, target, journal [32]byte) *fakeNode {
	return f.root.children["lineages"].children[fmt.Sprintf("%x", target)].children[fmt.Sprintf("%x", journal)]
}

func generationAppendIndexNode(f *fakeBackend, target [32]byte) *fakeNode {
	return f.root.children["lineages"].children[fmt.Sprintf("%x", target)].children["index.caj"]
}

func bytesValue(value string) func() []byte { return func() []byte { return []byte(value) } }
func sizedBytes(size uint64) func() []byte  { return func() []byte { return make([]byte, int(size)) } }

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
