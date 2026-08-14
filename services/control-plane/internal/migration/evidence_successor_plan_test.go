package migration

import (
	"bytes"
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"sync/atomic"
	"testing"
)

func TestSuccessorAdmissionFramesAreAdjacentAndByteExact(t *testing.T) {
	history, candidate, subject, authorityDigest, execution := successorAdmissionPlanFixture(t)
	superseded, reserved, header, supersededRaw, reservedRaw, err := buildSuccessorAdmissionFrames(history, candidate, subject, authorityDigest, execution)
	if err != nil {
		t.Fatal(err)
	}
	if superseded.Record.Superseded == nil || reserved.Record.Reserved == nil || header.Record.Header == nil || superseded.Sequence != history.targetIndexRecords || reserved.Sequence != superseded.Sequence+1 || reserved.PreviousRecordDigest == nil || *reserved.PreviousRecordDigest != superseded.RecordDigest {
		t.Fatal("successor plan lost exact adjacent frame identities")
	}
	if !canonicalEqual(superseded.Record.Superseded.PlannedGenerationReserved, reserved.Record.Reserved) || !canonicalEqual(subject.Continuation, reserved.Record.Reserved.Continuation) || header.RecordDigest != reserved.Record.Reserved.ExpectedSegment0HeaderDigest {
		t.Fatal("nested reservation or planned header differs from adjacent durable body")
	}
	wantSuperseded, err := EncodeCanonicalLineageFrame(superseded)
	if err != nil || !bytes.Equal(wantSuperseded, supersededRaw) {
		t.Fatal("superseded canonical bytes differ")
	}
	wantReserved, err := EncodeCanonicalLineageFrame(reserved)
	if err != nil || !bytes.Equal(wantReserved, reservedRaw) {
		t.Fatal("reserved canonical bytes differ")
	}
}

func TestSuccessorAdmissionFramesRejectBoundaryAuthorityAndQuotaSwaps(t *testing.T) {
	for name, mutate := range map[string]func(*VerifiedAdmissionHistory, *lineageSupersessionAuthoritySubject, *Digest, *VerifiedRecoveryExecutionBindings){
		"target state": func(history *VerifiedAdmissionHistory, _ *lineageSupersessionAuthoritySubject, _ *Digest, _ *VerifiedRecoveryExecutionBindings) {
			history.targetState = admissionLineageActiveUnknownExtension
		},
		"checkpoint": func(_ *VerifiedAdmissionHistory, subject *lineageSupersessionAuthoritySubject, _ *Digest, _ *VerifiedRecoveryExecutionBindings) {
			subject.OldCheckpointRecordDigest = digestPointer(testDigest("other-checkpoint"))
		},
		"successor decision": func(_ *VerifiedAdmissionHistory, subject *lineageSupersessionAuthoritySubject, digest *Digest, _ *VerifiedRecoveryExecutionBindings) {
			subject.SuccessorRunnerProjectionDecisionDigest = testDigest("other-successor")
			*digest, _ = subject.ComputeDigest()
		},
		"authority digest": func(_ *VerifiedAdmissionHistory, _ *lineageSupersessionAuthoritySubject, digest *Digest, _ *VerifiedRecoveryExecutionBindings) {
			*digest = testDigest("other-authority")
		},
		"execution digest": func(_ *VerifiedAdmissionHistory, _ *lineageSupersessionAuthoritySubject, _ *Digest, execution *VerifiedRecoveryExecutionBindings) {
			execution.digest = testDigest("other-execution")
		},
		"old supersede records": func(history *VerifiedAdmissionHistory, _ *lineageSupersessionAuthoritySubject, _ *Digest, _ *VerifiedRecoveryExecutionBindings) {
			history.targetGeneration.replay.indexDebitRecords = history.targetGeneration.replay.reservation.ReservedIndexRecords
		},
		"old supersede bytes": func(history *VerifiedAdmissionHistory, _ *lineageSupersessionAuthoritySubject, _ *Digest, _ *VerifiedRecoveryExecutionBindings) {
			history.targetGeneration.replay.indexDebitBytes = history.targetGeneration.replay.reservation.ReservedIndexBytes
		},
		"new reserve bytes": func(history *VerifiedAdmissionHistory, _ *lineageSupersessionAuthoritySubject, _ *Digest, _ *VerifiedRecoveryExecutionBindings) {
			history.reservation.ReservedIndexBytes = 1
		},
	} {
		t.Run(name, func(t *testing.T) {
			history, candidate, subject, authorityDigest, execution := successorAdmissionPlanFixture(t)
			mutate(history, &subject, &authorityDigest, &execution)
			if _, _, _, _, _, err := buildSuccessorAdmissionFrames(history, candidate, subject, authorityDigest, execution); err == nil {
				t.Fatal("successor mutation was accepted")
			}
		})
	}
}

func TestSuccessorAdmissionFramesAcceptExactHeaderOnlyBoundary(t *testing.T) {
	history, candidate, subject, authorityDigest, execution := successorAdmissionPlanFixture(t)
	activation := testDigest("successor-old-activation")
	history.targetState = admissionLineageActiveInitial
	history.targetIndexTail = activation
	history.targetGeneration.replay.cursor.latestCheckpointRecordDigest = nil
	history.targetGeneration.replay.cursor.lineageIndexPreviousRecordDigest = activation
	history.targetGeneration.replay.recovery.state = RecoveryBrandNew
	history.targetGeneration.replay.recovery.nextPermittedAction = RecoveryBeginFirstAttempt
	history.targetGeneration.replay.recovery.lastTerminalDigest = nil
	policy := execution.policy
	policy.AllowedOutcomes = []string{"activated_no_migration_progress"}
	policy.OutcomeConstraints = []historicalOutcomeConstraint{{Outcome: "activated_no_migration_progress", Continuation: historicalOutcomeContinuation{Kind: "exact_carry_old_generation"}}}
	policyDigest, err := policy.ComputeDigest()
	if err != nil {
		t.Fatal(err)
	}
	execution.policy = policy
	execution.subject.HistoricalRecoveryPolicyDigest = policyDigest
	execution.subject.OldRecoveryState = string(RecoveryBrandNew)
	execution.digest, err = execution.subject.ComputeDigest()
	if err != nil {
		t.Fatal(err)
	}
	subject.HistoricalRecoveryPolicyDigest = policyDigest
	subject.RecoveryExecutionBindingsDigest = execution.digest
	subject.ObservedOutcome = "activated_no_migration_progress"
	subject.OldCheckpointRecordDigest = nil
	subject.OldActivationRecordDigest = &activation
	subject.OldInitialJournalTailDigest = &history.targetGeneration.replay.recovery.tailDigest
	subject.OldTerminalDigest = nil
	subject.OldResolutionDigest = nil
	subject.Continuation = nil
	authorityDigest, err = subject.ComputeDigest()
	if err != nil {
		t.Fatal(err)
	}
	superseded, reserved, _, _, _, err := buildSuccessorAdmissionFrames(history, candidate, subject, authorityDigest, execution)
	if err != nil || superseded.Record.Superseded == nil || reserved.Record.Reserved == nil || superseded.Record.Superseded.Outcome != "activated_no_migration_progress" || reserved.Record.Reserved.Continuation != nil {
		t.Fatalf("header-only successor boundary rejected: superseded=%+v reserved=%+v err=%v", superseded.Record.Superseded, reserved.Record.Reserved, err)
	}
}

func TestVerifiedSuccessorAdmissionPlanDigestBindsEveryClosedInput(t *testing.T) {
	history, candidate, subject, authorityDigest, execution := successorAdmissionPlanFixture(t)
	superseded, reserved, header, supersededRaw, reservedRaw, err := buildSuccessorAdmissionFrames(history, candidate, subject, authorityDigest, execution)
	if err != nil {
		t.Fatal(err)
	}
	authority := &VerifiedLineageSupersessionAuthority{digest: authorityDigest}
	plan := &VerifiedSuccessorAdmissionPlan{
		history: history, registered: history.targetGeneration, candidateBinding: candidate.binding, authority: authority,
		authoritySubject: subject, authorityDigest: authorityDigest, executionDigest: execution.digest,
		supersededFrame: superseded, reservedFrame: reserved, headerFrame: header,
		supersededFrameBytes: supersededRaw, reservedFrameBytes: reservedRaw, consumed: &atomic.Bool{},
	}
	plan.self = plan
	baseline := verifiedSuccessorAdmissionPlanDigest(plan)
	if baseline == ([32]byte{}) || !successorAdmissionPlanFramesExact(plan) {
		t.Fatal("successor plan fixture is invalid")
	}
	for name, mutate := range map[string]func(*VerifiedSuccessorAdmissionPlan){
		"authority":  func(value *VerifiedSuccessorAdmissionPlan) { value.authorityDigest = testDigest("changed-authority") },
		"execution":  func(value *VerifiedSuccessorAdmissionPlan) { value.executionDigest = testDigest("changed-execution") },
		"subject":    func(value *VerifiedSuccessorAdmissionPlan) { value.authoritySubject.ObservedOutcome = "exact_pending" },
		"superseded": func(value *VerifiedSuccessorAdmissionPlan) { value.supersededFrameBytes[0] ^= 1 },
		"reserved":   func(value *VerifiedSuccessorAdmissionPlan) { value.reservedFrameBytes[0] ^= 1 },
		"header": func(value *VerifiedSuccessorAdmissionPlan) {
			value.headerFrame.RecordDigest = testDigest("changed-header")
		},
	} {
		t.Run(name, func(t *testing.T) {
			value := *plan
			value.self = &value
			value.authoritySubject = cloneProjectionValue(plan.authoritySubject)
			value.supersededFrame = cloneProjectionValue(plan.supersededFrame)
			value.reservedFrame = cloneProjectionValue(plan.reservedFrame)
			value.headerFrame = cloneProjectionValue(plan.headerFrame)
			value.supersededFrameBytes = append([]byte(nil), plan.supersededFrameBytes...)
			value.reservedFrameBytes = append([]byte(nil), plan.reservedFrameBytes...)
			mutate(&value)
			if verifiedSuccessorAdmissionPlanDigest(&value) == baseline {
				t.Fatal("successor plan mutation did not change canonical digest")
			}
		})
	}
}

func TestSuccessorPlanBinderRejectsLiteralAuthorityWithoutConsumption(t *testing.T) {
	candidate := quotaCandidateForBundle(t, quotaAdmissionBundleForTest(t), []byte("successor-literal"))
	authority := &VerifiedLineageSupersessionAuthority{}
	if plan, err := bindVerifiedSuccessorAdmissionPlan(context.Background(), &VerifiedAdmissionHistory{}, candidate, authority); plan != nil || !IsCode(err, CodeEvidenceRecoveryRequired) || authority.consumed.Load() {
		t.Fatalf("literal successor authority entered plan binder: plan=%+v err=%v consumed=%v", plan, err, authority.consumed.Load())
	}
}

func TestSuccessorPlanAuthorityDoesNotSpreadBeforeTransitionSlice(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") || name == "evidence_successor_plan.go" {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			identifier, ok := node.(*ast.Ident)
			if ok && (identifier.Name == "VerifiedSuccessorAdmissionPlan" || identifier.Name == "bindVerifiedSuccessorAdmissionPlan") {
				t.Fatalf("successor plan authority spread into %s", name)
			}
			return true
		})
	}
}

func successorAdmissionPlanFixture(t *testing.T) (*VerifiedAdmissionHistory, OwnedCurrentCandidate, lineageSupersessionAuthoritySubject, Digest, VerifiedRecoveryExecutionBindings) {
	t.Helper()
	candidate := quotaCandidateForBundle(t, quotaAdmissionBundleForTest(t), []byte("successor-plan"))
	current := candidate.verifiedRun.currentDecision
	currentBindings, err := current.decision.runnerProjectionBindings()
	if err != nil {
		t.Fatal(err)
	}
	old := generationIdentity{
		owner: candidate.owner, executionLineageDigest: candidate.verifiedRun.executionLineageDigest, journalIdentityDigest: testDigest("successor-old-journal"),
		runnerProjectionDecisionDigest: testDigest("successor-old-decision"), schemaBundleDigest: testDigest("successor-old-schema"),
	}
	journalTail := testDigest("successor-old-journal-tail")
	checkpoint := testDigest("successor-old-checkpoint")
	terminal := testDigest("successor-old-terminal")
	valid := &atomic.Bool{}
	valid.Store(true)
	cursor := JournalCursor{
		owner: candidate.owner, generation: old, segmentIndex: 0, nextSequence: 4, previousRecordDigest: &journalTail,
		lineageIndexNextSequence: 4, lineageIndexPreviousRecordDigest: checkpoint, latestCheckpointRecordDigest: &checkpoint, valid: valid,
	}
	recovery := &RecoverySnapshot{
		owner: candidate.owner, generation: old, cursor: cursor.clone(), tailDigest: journalTail, state: RecoveryTerminal,
		lastTerminalDigest: &terminal, nextPermittedAction: RecoveryBeginFirstAttemptNextEntry,
	}
	continuation := &LineageContinuationContext{
		StartAction: "begin_first_attempt_next_entry", MigrationID: "000002", AttemptIndex: 1,
		SourceJournalIdentityDigest: old.journalIdentityDigest, SourceCheckpointRecordDigest: checkpoint, SourceTerminalDigest: terminal,
	}
	policy := historicalRecoveryPolicySubject{
		RecoveryPolicySubjectDigest: currentBindings.recoveryPolicySubjectDigest, ExecutionLineageDigest: old.executionLineageDigest,
		OldJournalIdentityDigest: old.journalIdentityDigest, OldRunnerProjectionDecisionDigest: old.runnerProjectionDecisionDigest,
		OldSchemaBundleDigest: old.schemaBundleDigest, OldDecisionRecoveryArtifactSHA256: testDigest("successor-old-recovery"), OldDecisionRecoveryArtifactSizeBytes: 32,
		SuccessorRunnerProjectionDecisionDigest: candidate.verifiedRun.runnerProjectionDecisionDigest, SuccessorSchemaBundleDigest: candidate.verifiedRun.schemaBundleDigest,
		AllowedOutcomes: []string{"exact_committed_continue_successor"}, OutcomeConstraints: []historicalOutcomeConstraint{{
			Outcome: "exact_committed_continue_successor", Continuation: historicalOutcomeContinuation{Kind: "exact_identity", Identity: &lineageContinuationIdentity{StartAction: continuation.StartAction, MigrationID: continuation.MigrationID, AttemptIndex: continuation.AttemptIndex, PreviousAttempt: "null"}},
		}},
	}
	policyDigest, err := policy.ComputeDigest()
	if err != nil {
		t.Fatal(err)
	}
	executionSubject := recoveryExecutionBindingsSubject{
		HistoricalRecoveryPolicyDigest: policyDigest, ExecutionLineageDigest: old.executionLineageDigest,
		CurrentRunnerProjectionDecisionDigest: candidate.verifiedRun.runnerProjectionDecisionDigest, OldRunnerProjectionDecisionDigest: old.runnerProjectionDecisionDigest,
		OldJournalIdentityDigest: old.journalIdentityDigest, OldSchemaBundleDigest: old.schemaBundleDigest,
		OldDecisionRecoveryArtifactSHA256: policy.OldDecisionRecoveryArtifactSHA256, OldDecisionRecoveryArtifactSizeBytes: policy.OldDecisionRecoveryArtifactSizeBytes,
		OldJournalReplayTailDigest: journalTail, OldRecoveryState: string(recovery.state), ActionsProfile: oldAttemptRecoveryActionsProfile,
	}
	executionDigest, err := executionSubject.ComputeDigest()
	if err != nil {
		t.Fatal(err)
	}
	execution := VerifiedRecoveryExecutionBindings{
		owner: current.owner, session: candidate.owner, generation: old, tailDigest: journalTail, snapshot: recovery,
		policy: policy, subject: executionSubject, digest: executionDigest,
	}
	authoritySubject := lineageSupersessionAuthoritySubject{
		HistoricalRecoveryPolicyDigest: policyDigest, RecoveryExecutionBindingsDigest: executionDigest,
		ExecutionLineageDigest: old.executionLineageDigest, OldJournalIdentityDigest: old.journalIdentityDigest,
		OldRunnerProjectionDecisionDigest: old.runnerProjectionDecisionDigest, OldSchemaBundleDigest: old.schemaBundleDigest,
		OldCheckpointRecordDigest: &checkpoint, OldTerminalDigest: &terminal, ObservedOutcome: "exact_committed_continue_successor",
		SuccessorRunnerProjectionDecisionDigest: candidate.verifiedRun.runnerProjectionDecisionDigest, SuccessorSchemaBundleDigest: candidate.verifiedRun.schemaBundleDigest,
		Continuation: continuation,
	}
	authorityDigest, err := authoritySubject.ComputeDigest()
	if err != nil {
		t.Fatal(err)
	}
	oldReservation := evidenceQuotaReservation{
		ReservedRecords: 16, ReservedJournalBytes: 1 << 18, ReservedSegments: 4, ReservedCheckpointRecords: 15,
		ReservedIndexRecords: 19, ReservedIndexBytes: 1 << 18, ReservedBytes: 1 << 19,
	}
	newReservation := evidenceQuotaReservation{
		ReservedRecords: 16, ReservedJournalBytes: 1 << 18, ReservedSegments: 4, ReservedCheckpointRecords: 15,
		ReservedIndexRecords: 18, ReservedIndexBytes: 1 << 18, ReservedBytes: 1 << 19,
	}
	replay := &verifiedAdmissionGenerationReplay{
		cursor: cursor, recovery: recovery, reservation: oldReservation, indexDebitRecords: 4, indexDebitBytes: 1024,
	}
	registered := &verifiedAdmissionRegisteredGeneration{descriptor: GenerationDescriptor{identity: old, replayTailDigest: journalTail}, replay: replay, canonical: [32]byte{2}}
	history := &VerifiedAdmissionHistory{
		candidateBinding: candidate.binding, target: digestRaw(candidate.verifiedRun.executionLineageDigest), targetState: admissionLineageActiveCheckpointed,
		targetIndexRecords: 4, targetIndexTail: checkpoint, reservation: newReservation, targetGeneration: registered, binding: &verifiedAdmissionHistoryBinding{canonical: [32]byte{1}},
	}
	return history, candidate, authoritySubject, authorityDigest, execution
}
