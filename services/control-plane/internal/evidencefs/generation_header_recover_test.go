package evidencefs

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"testing"
)

func addGenerationRegistrationPrefix(f *fakeBackend, lineage *fakeNode, journal [32]byte, state GenerationRegistrationState, segment []byte) *fakeNode {
	name := fmt.Sprintf("%x", journal)
	directory := f.directory(name)
	if state == GenerationRegistrationPrefixLock || segment != nil {
		directory.children["writer.lock"] = f.regular("writer.lock", nil)
	}
	if segment != nil {
		directory.children[admissionSegmentName(0)] = f.regular(admissionSegmentName(0), segment)
	}
	lineage.children[name] = directory
	return directory
}

func TestAdmissionRecoverGenerationHeaderPhysicalStates(t *testing.T) {
	header := []byte("canonical-generation-segment-zero-header")
	tests := []struct {
		name       string
		state      GenerationRegistrationState
		segment    []byte
		wantFacts  int
		wantWrites bool
		wantTrunc  bool
	}{
		{name: "directory", state: GenerationRegistrationPrefixDirectory, wantFacts: 1, wantWrites: true},
		{name: "lock", state: GenerationRegistrationPrefixLock, wantFacts: 1, wantWrites: true},
		{name: "segment-empty", segment: []byte{}, wantWrites: true, wantTrunc: true},
		{name: "segment-torn", segment: header[:11], wantWrites: true, wantTrunc: true},
		{name: "segment-complete", segment: header},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			f := newFakeBackend()
			f.partialWrite = 5
			target := digestForTest(9)
			journal := digestForTest(77)
			lineageNode := addAdmissionLineage(f, target, 0, 0)
			addGenerationRegistrationPrefix(f, lineageNode, journal, test.state, test.segment)
			store := testStore(t, f)
			lease, inventory, err := store.AcquireAdmission(context.Background(), target)
			if err != nil {
				t.Fatal(err)
			}
			lineage, err := inventory.Lineage(target)
			if err != nil {
				t.Fatal(err)
			}
			facts, err := lineage.GenerationRegistrations()
			if err != nil || len(facts) != test.wantFacts {
				t.Fatalf("facts=%v err=%v", facts, err)
			}
			journals, err := lineage.Journals()
			if err != nil || len(journals) != 1-test.wantFacts {
				t.Fatalf("journals=%v err=%v", journals, err)
			}
			if len(facts) == 1 {
				state, stateErr := facts[0].State()
				gotLineage, lineageErr := facts[0].Lineage()
				gotJournal, journalErr := facts[0].Journal()
				full, fullErr := facts[0].FullSetDigest()
				inventoryFull, inventoryFullErr := inventory.FullSetDigest()
				if stateErr != nil || lineageErr != nil || journalErr != nil || fullErr != nil || inventoryFullErr != nil || state != test.state || gotLineage != target || gotJournal != journal || full != inventoryFull {
					t.Fatalf("state=%q lineage=%x journal=%x full=%x inventory=%x errors=%v/%v/%v/%v/%v", state, gotLineage, gotJournal, full, inventoryFull, stateErr, lineageErr, journalErr, fullErr, inventoryFullErr)
				}
			}
			writesBefore, truncatesBefore := f.writes, f.truncates
			token, err := inventory.MutationToken()
			if err != nil {
				t.Fatal(err)
			}
			result, err := token.RecoverGenerationHeader(context.Background(), inventory, journal, header)
			if err != nil || result.Outcome() != AdmissionTransitionDurable || !result.ValidFor(result.Inventory()) {
				t.Fatalf("result=%+v err=%v", result, err)
			}
			next := result.Inventory()
			nextLineage, err := next.Lineage(target)
			if err != nil {
				t.Fatal(err)
			}
			nextFacts, err := nextLineage.GenerationRegistrations()
			if err != nil || len(nextFacts) != 0 {
				t.Fatalf("next facts=%v err=%v", nextFacts, err)
			}
			nextJournal := findAdmissionJournal(nextLineage, journal)
			if nextJournal == nil {
				t.Fatal("recovered journal missing")
			}
			segments, err := nextJournal.Segments()
			if err != nil || len(segments) != 1 {
				t.Fatalf("segments=%v err=%v", segments, err)
			}
			got, err := segments[0].ReadAll(context.Background())
			if err != nil || !bytes.Equal(got, header) {
				t.Fatalf("segment=%q err=%v", got, err)
			}
			if err := next.Revalidate(context.Background()); err != nil {
				t.Fatal(err)
			}
			if (f.writes > writesBefore) != test.wantWrites || (f.truncates > truncatesBefore) != test.wantTrunc || len(lease.journalLocks) != 1 {
				t.Fatalf("writes=%d/%d truncates=%d/%d locks=%v", writesBefore, f.writes, truncatesBefore, f.truncates, lease.journalLocks)
			}
			if err := lease.Close(); err != nil || len(f.handles) != 0 {
				t.Fatalf("close=%v handles=%d", err, len(f.handles))
			}
		})
	}
}

func TestAdmissionGenerationRegistrationFactRejectsCopyAndClosedLease(t *testing.T) {
	f := newFakeBackend()
	target, journal := digestForTest(9), digestForTest(77)
	lineageNode := addAdmissionLineage(f, target, 0, 0)
	addGenerationRegistrationPrefix(f, lineageNode, journal, GenerationRegistrationPrefixLock, nil)
	store := testStore(t, f)
	lease, inventory, err := store.AcquireAdmission(context.Background(), target)
	if err != nil {
		t.Fatal(err)
	}
	lineage, err := inventory.Lineage(target)
	if err != nil {
		t.Fatal(err)
	}
	facts, err := lineage.GenerationRegistrations()
	if err != nil || len(facts) != 1 {
		t.Fatalf("facts=%v err=%v", facts, err)
	}
	copyFact := *facts[0]
	if _, err := copyFact.State(); !errors.Is(err, ErrLeaseInvalid) {
		t.Fatalf("copy state=%v", err)
	}
	var literal GenerationRegistrationFact
	if _, err := literal.Journal(); !errors.Is(err, ErrLeaseInvalid) {
		t.Fatalf("literal journal=%v", err)
	}
	facts[0] = nil
	again, err := lineage.GenerationRegistrations()
	if err != nil || len(again) != 1 || again[0] == nil {
		t.Fatalf("alias mutation leaked: %v %v", again, err)
	}
	if err := lease.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := again[0].State(); !errors.Is(err, ErrLeaseInvalid) {
		t.Fatalf("closed state=%v", err)
	}
}

func TestAdmissionGenerationRegistrationRevalidateRejectsDriftAndGraphTamper(t *testing.T) {
	for _, name := range []string{"physical-directory", "slot-expectation", "fact-field"} {
		t.Run(name, func(t *testing.T) {
			f := newFakeBackend()
			target, journal := digestForTest(9), digestForTest(77)
			lineageNode := addAdmissionLineage(f, target, 0, 0)
			directory := addGenerationRegistrationPrefix(f, lineageNode, journal, GenerationRegistrationPrefixLock, nil)
			store := testStore(t, f)
			lease, inventory, err := store.AcquireAdmission(context.Background(), target)
			if err != nil {
				t.Fatal(err)
			}
			lineage, err := inventory.Lineage(target)
			if err != nil {
				t.Fatal(err)
			}
			facts, err := lineage.GenerationRegistrations()
			if err != nil || len(facts) != 1 {
				t.Fatalf("facts=%v err=%v", facts, err)
			}
			switch name {
			case "physical-directory":
				directory.stat.mode = 0o755
			case "slot-expectation":
				expected := inventory.slot.registrations[facts[0]]
				expected.directory.mode = 0o755
				inventory.slot.registrations[facts[0]] = expected
			case "fact-field":
				facts[0].journal = digestForTest(78)
			}
			if err := inventory.Revalidate(context.Background()); err == nil || lease.Active() {
				t.Fatalf("revalidate=%v active=%v", err, lease.Active())
			}
			_ = lease.Close()
			if len(f.handles) != 0 {
				t.Fatalf("handles=%d", len(f.handles))
			}
		})
	}
}

func TestAcquireAdmissionGenerationRegistrationLimits(t *testing.T) {
	t.Run("exact", func(t *testing.T) {
		f := newFakeBackend()
		target := digestForTest(9)
		lineage := addAdmissionLineage(f, target, 0, 0)
		for index := 0; index < maximumAdmissionJournals; index++ {
			addGenerationRegistrationPrefix(f, lineage, digestForTest(100+index), GenerationRegistrationPrefixDirectory, nil)
		}
		store := testStore(t, f)
		lease, inventory, err := store.AcquireAdmission(context.Background(), digestForTest(8))
		if err != nil {
			t.Fatal(err)
		}
		view, err := inventory.Lineage(target)
		if err != nil {
			t.Fatal(err)
		}
		facts, err := view.GenerationRegistrations()
		if err != nil || len(facts) != maximumAdmissionJournals {
			t.Fatalf("facts=%d err=%v", len(facts), err)
		}
		if err := lease.Close(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("plus-one", func(t *testing.T) {
		f := newFakeBackend()
		target := digestForTest(9)
		lineage := addAdmissionLineage(f, target, 0, 0)
		for index := 0; index <= maximumAdmissionJournals; index++ {
			addGenerationRegistrationPrefix(f, lineage, digestForTest(100+index), GenerationRegistrationPrefixDirectory, nil)
		}
		store := testStore(t, f)
		lease, inventory, err := store.AcquireAdmission(context.Background(), digestForTest(8))
		if lease != nil || inventory != nil || !errors.Is(err, ErrLimit) {
			t.Fatalf("lease=%v inventory=%v err=%v", lease, inventory, err)
		}
	})
}

func TestAcquireAdmissionGenerationRegistrationGrammar(t *testing.T) {
	tests := map[string]func(*fakeBackend, *fakeNode, [32]byte){
		"segment-without-lock": func(f *fakeBackend, lineage *fakeNode, journal [32]byte) {
			directory := addGenerationRegistrationPrefix(f, lineage, journal, GenerationRegistrationPrefixDirectory, nil)
			directory.children[admissionSegmentName(0)] = f.regular(admissionSegmentName(0), []byte("header"))
		},
		"nonzero-lock": func(f *fakeBackend, lineage *fakeNode, journal [32]byte) {
			directory := addGenerationRegistrationPrefix(f, lineage, journal, GenerationRegistrationPrefixLock, nil)
			directory.children["writer.lock"].data = []byte{1}
			directory.children["writer.lock"].stat.size = 1
		},
		"segment-gap": func(f *fakeBackend, lineage *fakeNode, journal [32]byte) {
			directory := addGenerationRegistrationPrefix(f, lineage, journal, GenerationRegistrationPrefixLock, nil)
			directory.children[admissionSegmentName(1)] = f.regular(admissionSegmentName(1), []byte("header"))
		},
		"unexpected-entry": func(f *fakeBackend, lineage *fakeNode, journal [32]byte) {
			directory := addGenerationRegistrationPrefix(f, lineage, journal, GenerationRegistrationPrefixLock, nil)
			directory.children["other"] = f.regular("other", nil)
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			f := newFakeBackend()
			target, journal := digestForTest(9), digestForTest(77)
			lineage := addAdmissionLineage(f, target, 0, 0)
			mutate(f, lineage, journal)
			store := testStore(t, f)
			lease, inventory, err := store.AcquireAdmission(context.Background(), target)
			if lease != nil || inventory != nil || !errors.Is(err, ErrCorrupt) {
				t.Fatalf("lease=%v inventory=%v err=%v", lease, inventory, err)
			}
		})
	}
}

func TestAdmissionRecoverGenerationHeaderRejectsNonPrefixBeforeMutation(t *testing.T) {
	f := newFakeBackend()
	target, journal := digestForTest(9), digestForTest(77)
	lineageNode := addAdmissionLineage(f, target, 0, 0)
	addGenerationRegistrationPrefix(f, lineageNode, journal, "", []byte("wrong"))
	store := testStore(t, f)
	lease, inventory, err := store.AcquireAdmission(context.Background(), target)
	if err != nil {
		t.Fatal(err)
	}
	token, err := inventory.MutationToken()
	if err != nil {
		t.Fatal(err)
	}
	fsyncs, writes, truncates := f.fsyncs, f.writes, f.truncates
	result, err := token.RecoverGenerationHeader(context.Background(), inventory, journal, []byte("canonical-header"))
	if !errors.Is(err, ErrCorrupt) || result.Outcome() != AdmissionTransitionPreMutationFailure || lease.Active() || token.ValidFor(inventory) || f.fsyncs != fsyncs || f.writes != writes || f.truncates != truncates {
		t.Fatalf("result=%+v err=%v active=%v token=%v ops=%d/%d %d/%d %d/%d", result, err, lease.Active(), token.ValidFor(inventory), fsyncs, f.fsyncs, writes, f.writes, truncates, f.truncates)
	}
	if err := lease.Close(); err != nil || len(f.handles) != 0 {
		t.Fatalf("close=%v handles=%d", err, len(f.handles))
	}
}

func TestAdmissionRecoverGenerationHeaderPreMutationFailuresPreserveAuthority(t *testing.T) {
	t.Run("canceled", func(t *testing.T) {
		f := newFakeBackend()
		target, journal := digestForTest(9), digestForTest(77)
		lineageNode := addAdmissionLineage(f, target, 0, 0)
		addGenerationRegistrationPrefix(f, lineageNode, journal, GenerationRegistrationPrefixDirectory, nil)
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
		cancel()
		fsyncs := f.fsyncs
		result, err := token.RecoverGenerationHeader(ctx, inventory, journal, []byte("header"))
		if !errors.Is(err, context.Canceled) || result.Outcome() != AdmissionTransitionPreMutationFailure || !lease.Active() || !token.ValidFor(inventory) || f.fsyncs != fsyncs {
			t.Fatalf("result=%+v err=%v active=%v token=%v fsync=%d/%d", result, err, lease.Active(), token.ValidFor(inventory), fsyncs, f.fsyncs)
		}
		if err := lease.Close(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("multiple-segments", func(t *testing.T) {
		f := newFakeBackend()
		target, journal := digestForTest(9), digestForTest(77)
		lineageNode := addAdmissionLineage(f, target, 0, 0)
		directory := addGenerationRegistrationPrefix(f, lineageNode, journal, "", []byte("header"))
		directory.children[admissionSegmentName(1)] = f.regular(admissionSegmentName(1), []byte("next"))
		store := testStore(t, f)
		lease, inventory, err := store.AcquireAdmission(context.Background(), target)
		if err != nil {
			t.Fatal(err)
		}
		token, err := inventory.MutationToken()
		if err != nil {
			t.Fatal(err)
		}
		result, err := token.RecoverGenerationHeader(context.Background(), inventory, journal, []byte("header"))
		if !errors.Is(err, ErrInvalidInput) || result.Outcome() != AdmissionTransitionPreMutationFailure || !lease.Active() || !token.ValidFor(inventory) {
			t.Fatalf("result=%+v err=%v active=%v token=%v", result, err, lease.Active(), token.ValidFor(inventory))
		}
		if err := lease.Close(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("copied-token", func(t *testing.T) {
		f := newFakeBackend()
		target, journal := digestForTest(9), digestForTest(77)
		lineageNode := addAdmissionLineage(f, target, 0, 0)
		addGenerationRegistrationPrefix(f, lineageNode, journal, GenerationRegistrationPrefixLock, nil)
		store := testStore(t, f)
		lease, inventory, err := store.AcquireAdmission(context.Background(), target)
		if err != nil {
			t.Fatal(err)
		}
		token, err := inventory.MutationToken()
		if err != nil {
			t.Fatal(err)
		}
		copied := *token
		result, err := copied.RecoverGenerationHeader(context.Background(), inventory, journal, []byte("header"))
		if !errors.Is(err, ErrInvalidInput) || result.Outcome() != AdmissionTransitionPreMutationFailure || !token.ValidFor(inventory) {
			t.Fatalf("result=%+v err=%v original=%v", result, err, token.ValidFor(inventory))
		}
		if err := lease.Close(); err != nil {
			t.Fatal(err)
		}
	})
}

func TestAdmissionRecoverGenerationHeaderPostBarrierFailureUnknown(t *testing.T) {
	header := []byte("canonical-header")
	tests := []struct {
		name    string
		state   GenerationRegistrationState
		segment []byte
		arm     func(*fakeBackend, *fakeNode)
	}{
		{name: "parent-sync", state: GenerationRegistrationPrefixDirectory, arm: func(f *fakeBackend, _ *fakeNode) { f.failFsyncAt = f.fsyncs + 1 }},
		{name: "lock-sync", state: GenerationRegistrationPrefixDirectory, arm: func(f *fakeBackend, _ *fakeNode) { f.failFdatasyncAt = f.fdatasyncs + 1 }},
		{name: "lock-directory-sync", state: GenerationRegistrationPrefixDirectory, arm: func(f *fakeBackend, _ *fakeNode) { f.failFsyncAt = f.fsyncs + 2 }},
		{name: "lock-busy", state: GenerationRegistrationPrefixLock, arm: func(f *fakeBackend, directory *fakeNode) {
			inode := directory.children["writer.lock"].stat.inode
			f.busyInodeAttempts[inode] = 1
		}},
		{name: "lock-error", state: GenerationRegistrationPrefixLock, arm: func(f *fakeBackend, directory *fakeNode) {
			f.failTryLockInodes[directory.children["writer.lock"].stat.inode] = true
		}},
		{name: "segment-create", state: GenerationRegistrationPrefixLock, arm: func(f *fakeBackend, _ *fakeNode) { f.failOpenNames[admissionSegmentName(0)] = errors.New("open") }},
		{name: "truncate", segment: header[:5], arm: func(f *fakeBackend, _ *fakeNode) { f.failTruncateAt = f.truncates + 1 }},
		{name: "truncate-response-lost", segment: header[:5], arm: func(f *fakeBackend, _ *fakeNode) { f.failTruncateAfterAt = f.truncates + 1 }},
		{name: "write", state: GenerationRegistrationPrefixLock, arm: func(f *fakeBackend, _ *fakeNode) { f.failWriteAt = f.writes + 1 }},
		{name: "segment-sync", state: GenerationRegistrationPrefixLock, arm: func(f *fakeBackend, _ *fakeNode) { f.failFdatasyncAt = f.fdatasyncs + 2 }},
		{name: "segment-close", state: GenerationRegistrationPrefixLock, arm: func(f *fakeBackend, _ *fakeNode) { f.failCloseNames[admissionSegmentName(0)] = true }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			f := newFakeBackend()
			target, journal := digestForTest(9), digestForTest(77)
			lineageNode := addAdmissionLineage(f, target, 0, 0)
			directory := addGenerationRegistrationPrefix(f, lineageNode, journal, test.state, test.segment)
			store := testStore(t, f)
			lease, inventory, err := store.AcquireAdmission(context.Background(), target)
			if err != nil {
				t.Fatal(err)
			}
			token, err := inventory.MutationToken()
			if err != nil {
				t.Fatal(err)
			}
			test.arm(f, directory)
			result, err := token.RecoverGenerationHeader(context.Background(), inventory, journal, header)
			if !errors.Is(err, ErrUnknown) || result.Outcome() != AdmissionTransitionUnknown || result.Inventory() != nil || lease.Active() || token.ValidFor(inventory) {
				t.Fatalf("result=%+v err=%v active=%v token=%v", result, err, lease.Active(), token.ValidFor(inventory))
			}
			if lineageNode.children[fmt.Sprintf("%x", journal)] == nil {
				t.Fatal("recovery removed prefix")
			}
			unlockBefore := f.unlockAttempts
			_ = lease.Close()
			if len(f.handles) != 0 {
				t.Fatalf("handles=%d", len(f.handles))
			}
			if test.name == "segment-close" && store.usable() {
				t.Fatal("close uncertainty did not poison store")
			}
			if (test.name == "write" || test.name == "segment-sync" || test.name == "segment-close") && f.unlockAttempts <= unlockBefore {
				t.Fatalf("retained journal lock was not released: before=%d after=%d", unlockBefore, f.unlockAttempts)
			}
		})
	}
}
