package migration

import (
	"context"
	"time"
)

func claimRunnerLedgerRecoveryExecutionAdmissionPermit(permit *runnerLedgerRecoveryExecutionAdmissionPermit) (runnerLedgerReconciliationAdmissionSeed, error) {
	if permit == nil || permit.self != permit {
		return runnerLedgerReconciliationAdmissionSeed{}, fail(CodeTransactionBoundary, "runner-ledger-recovery-success-claim", "recovery execution-admission permit is unavailable", nil)
	}
	return claimRunnerLedgerReconciliationAdmissionPermit(permit, generatedRunnerLedgerRecoveryProfiles[5].action)
}

func (runner *Runner) executeRunnerLedgerRecoverySuccess(ctx context.Context, admission *runnerLedgerRecoveryExecutionAdmissionPermit, bundle *RuntimeBundle, plans []StatementPlan) (runnerLedgerEntrySuccessOutcome, error) {
	ready, err := runner.prepareRunnerLedgerRecoverySuccess(ctx, admission, bundle, plans)
	if err != nil {
		return runnerLedgerEntrySuccessOutcome{}, err
	}
	return runner.executeRunnerLedgerSuccessState(ctx, ready)
}

func (runner *Runner) prepareRunnerLedgerRecoverySuccess(ctx context.Context, admission *runnerLedgerRecoveryExecutionAdmissionPermit, bundle *RuntimeBundle, plans []StatementPlan) (*runnerLedgerEntrySuccessState, error) {
	seed, err := claimRunnerLedgerRecoveryExecutionAdmissionPermit(admission)
	if err != nil {
		return nil, err
	}
	failClosed := func(primary error) (*runnerLedgerEntrySuccessState, error) {
		return nil, closeRunnerDatabasePreflight(seed.session, seed.key, true, primary)
	}
	if runner == nil || ctx == nil || !validGeneratedRunnerLedgerRecoveryProfiles() {
		return failClosed(fail(CodeTransactionBoundary, "runner-ledger-recovery-success-claim", "recovery success writer inputs are unavailable", nil))
	}
	if err := contextAdmissionError(ctx); err != nil {
		return failClosed(err)
	}
	evidence, ok := seed.binder.(runnerLedgerRecoverySuccessEvidenceBinder)
	if !ok || evidence == nil || !runnerOwnedPointer(evidence) ||
		!validRunnerLedgerRecoveryAdmissionUse(evidence, seed.use, seed.consumerFactSubject,
			generatedRunnerLedgerRecoveryProfiles[5].action, seed.evidenceBoundary, true) {
		return failClosed(fail(CodeEvidenceRecoveryRequired, "runner-ledger-recovery-success-claim", "recovery success evidence binder is unavailable", nil))
	}
	verifiedBundle, err := verifiedRunnerLedgerCatalogBundle(bundle)
	if err != nil {
		return failClosed(err)
	}
	data, err := runnerLedgerRecoverySuccessInitialData(seed, evidence, verifiedBundle, plans)
	if err != nil {
		return failClosed(err)
	}
	state, err := sealRunnerLedgerEntrySuccessState(data, "unclassified", "consume_recovery_execution_permit")
	if err != nil {
		return failClosed(err)
	}
	return state, nil
}

func runnerLedgerRecoverySuccessInitialData(seed runnerLedgerReconciliationAdmissionSeed, evidence runnerLedgerRecoverySuccessEvidenceBinder, bundle *RuntimeBundle, plans []StatementPlan) (runnerLedgerEntrySuccessData, error) {
	var data runnerLedgerEntrySuccessData
	selection := seed.selection
	if evidence == nil || bundle == nil || bundle.Manifest == nil || int(selection.entryIndex) >= len(bundle.Manifest.SchemaBundle.Migrations) ||
		selection.action != generatedRunnerLedgerRecoveryProfiles[5].action || selection.profileIndex != 5 ||
		selection.migrationID == "" || selection.planCount == 0 || selection.planDigest == ([32]byte{}) ||
		selection.attemptIndex == 0 || selection.maxAttempts == 0 || selection.attemptIndex > selection.maxAttempts ||
		seed.session == nil || seed.use == nil || seed.candidateBinding == nil || seed.admissionPermitCanonical == ([32]byte{}) ||
		seed.runtimeInputs == ([32]byte{}) || seed.bindings.validateAt(time.Now()) != nil {
		return data, fail(CodeUntrusted, "runner-ledger-recovery-success-input", "selected recovery runtime entry is unavailable", nil)
	}
	entry := bundle.Manifest.SchemaBundle.Migrations[selection.entryIndex]
	wantRow := commitIntentLedgerRow(entry, bundle.Manifest.SchemaBundleDigest)
	canonical, err := canonicalContractKey(wantRow)
	if err != nil || wantRow.Validate() != nil || selection.entryDigest != DigestBytes([]byte(runnerLedgerPreflightEntryDigestDomain+"\x00"+canonical)) ||
		entry.ID != selection.migrationID || bundle.Manifest.SchemaBundleDigest != seed.generation.schemaBundleDigest ||
		bundle.ownedInputs.canonical != seed.runtimeInputs {
		return data, fail(CodeUntrusted, "runner-ledger-recovery-success-input", "selected signed entry differs from the recovery execution permit", nil)
	}
	planDigest, planCount, err := runnerEntryPlanClosureDigest(plans, entry.ID)
	if err != nil || planDigest != selection.planDigest || planCount != selection.planCount {
		return data, fail(CodeUntrusted, "runner-ledger-recovery-success-input", "statement plan closure differs from the recovery execution permit", nil)
	}
	selected := make([]StatementPlan, 0, planCount)
	for _, plan := range plans {
		if plan.MigrationID != entry.ID {
			continue
		}
		owned, cloneErr := cloneRunnerStatementIntentPlan(plan)
		if cloneErr != nil {
			return data, cloneErr
		}
		selected = append(selected, owned)
	}
	if uint32(len(selected)) != planCount {
		return data, fail(CodeUntrusted, "runner-ledger-recovery-success-input", "selected recovery statement plan count changed", nil)
	}
	candidate := evidence.CurrentCandidate()
	active := evidence.ActiveGeneration()
	snapshot := evidence.RecoverySnapshot()
	journal := evidence.Journal()
	bindings, err := runnerCurrentProjectionBindings(evidence, candidate)
	previousTerminal, recoveryOK := runnerLedgerRecoverySuccessInitialRecovery(snapshot, selection)
	if err != nil || !recoveryOK || !validOwnedCurrentCandidate(candidate) || candidate.binding != seed.candidateBinding ||
		active.kind != activeGenerationCurrent || active.recoveryExecutionBindings != nil ||
		!sameGenerationIdentity(active.identity, seed.generation) || journal == nil ||
		!sameRunnerOwnedPointer(journal, active.journal) || !seed.bindings.exactlyMatches(bindings) ||
		!validRecoverySnapshotForJournal(snapshot, seed.generation, snapshot.cursor) ||
		generationJournalRecoveryDigest(snapshot) != seed.recoveryDigest || snapshot.tailDigest != seed.recoveryTail {
		return data, fail(CodeEvidenceRecoveryRequired, "runner-ledger-recovery-success-input", "current inherited evidence boundary differs from the recovery execution permit", err)
	}
	catalog, ok := exactCatalogBindingForHead(bindings.executableCatalogs, entry.ID)
	if !ok || catalog.verifiedCatalog.validate() != nil || catalog.catalogContractDigest != catalog.verifiedCatalog.SubjectDigest() {
		return data, fail(CodeUntrusted, "runner-ledger-recovery-success-input", "selected catalog contract is unavailable", nil)
	}
	source, err := exactMigrationSource(catalog.catalogContract.SourceDescriptors, entry.ID)
	if err != nil || len(source.Statements) != len(selected) {
		return data, fail(CodeUntrusted, "runner-ledger-recovery-success-input", "catalog statement closure differs from the selected recovery plans", err)
	}
	policy := bundle.Manifest.ExecutionPolicy
	if policy.Validate() != nil || policy.MaxAttempts == 0 || policy.MaxAttempts > uint64(^uint32(0)) ||
		uint32(policy.MaxAttempts) != selection.maxAttempts || selection.attemptIndex > uint32(policy.MaxAttempts) ||
		len(bundle.Manifest.SchemaBundle.Migrations) > int(^uint32(0)) {
		return data, fail(CodeUntrusted, "runner-ledger-recovery-success-input", "recovery execution policy is unavailable", nil)
	}
	if seed.ledgerLength != selection.entryIndex {
		return data, fail(CodeInvalidLedger, "runner-ledger-recovery-success-input", "recovery ledger prefix does not select the inherited entry", nil)
	}
	if seed.ledgerLength == 0 {
		if seed.catalogContractDigest != nil || entry.PredecessorID != nil {
			return data, fail(CodeUntrusted, "runner-ledger-recovery-success-input", "first recovery entry predecessor binding is contradictory", nil)
		}
	} else if seed.catalogContractDigest == nil || entry.PredecessorID == nil || *entry.PredecessorID != seed.ledgerHead ||
		entry.PredecessorCatalogContract.Artifact == nil || entry.PredecessorCatalogContract.Artifact.SHA256 != *seed.catalogContractDigest {
		return data, fail(CodeUntrusted, "runner-ledger-recovery-success-input", "recovery entry predecessor catalog differs from the locked ledger head", nil)
	}
	data = runnerLedgerEntrySuccessData{
		writerKind: runnerLedgerSuccessWriterRecoveryV1,
		phase:      runnerLedgerEntrySuccessExecutionReady, session: seed.session, evidence: evidence, journal: journal,
		recoveryUse: seed.use, key: seed.key, candidateBinding: seed.candidateBinding, generation: seed.generation,
		consumerFactSubject: seed.consumerFactSubject, evidenceBoundary: seed.evidenceBoundary,
		recoveryDigest: seed.recoveryDigest, recoveryTail: seed.recoveryTail, database: seed.database,
		selection: runnerLedgerEntryAdmissionSelection{
			entryIndex: selection.entryIndex, migrationID: selection.migrationID,
			entryDigest: selection.entryDigest, planCount: selection.planCount, planDigest: selection.planDigest,
		},
		attemptIndex: selection.attemptIndex, previousAttemptTerminal: cloneDigestPointer(previousTerminal),
		initialRecoveryState: selection.recoveryState, initialRecoveryAction: selection.recoveryAction,
		admissionPermitCanonical: seed.admissionPermitCanonical,
		ledgerBeforeDigest:       seed.ledgerDigest, ledgerBeforeHead: seed.ledgerHead, ledgerBeforeLength: seed.ledgerLength,
		connectedAuthority: seed.connectedAuthority, migrationAuthority: seed.migrationAuthority,
		projectionSubject: seed.projectionSubject, observedCatalogContract: cloneDigestPointer(seed.catalogContractDigest),
		observedCatalogDigest: seed.catalogDigest, bundle: runnerLedgerEntrySuccessRuntimeHandle(bundle),
		runtimePolicy: cloneProjectionValue(policy), runtimeEntryCount: uint32(len(bundle.Manifest.SchemaBundle.Migrations)),
		runtimeInputs: bundle.ownedInputs.canonical, bindings: bindings.ownedCopy(), entry: cloneProjectionValue(entry),
		plans: selected, maxAttempts: selection.maxAttempts, cursor: snapshot.cursor.clone(), currentCatalogDigest: seed.catalogDigest,
	}
	return data, nil
}

func runnerLedgerRecoverySuccessInitialRecovery(snapshot *RecoverySnapshot, selection runnerLedgerRecoveryAdmissionSelection) (*Digest, bool) {
	if snapshot == nil || snapshot.state != RecoveryBrandNewInherited || snapshot.nextPermittedAction != selection.recoveryAction ||
		selection.recoveryState != RecoveryBrandNewInherited || snapshot.cursor.segmentIndex != 0 || snapshot.cursor.nextSequence != 1 ||
		snapshot.cursor.latestCheckpointRecordDigest != nil || snapshot.lastStatementIntent != nil ||
		snapshot.lastIntermediateEvidence != nil || snapshot.commitIntent != nil || snapshot.lastTerminal != nil ||
		snapshot.lastResolution != nil || snapshot.lastTerminalDigest != nil || snapshot.lastResolutionDigest != nil {
		return nil, false
	}
	switch selection.recoveryAction {
	case RecoveryBeginFirstAttempt:
		return nil, selection.attemptIndex == 1 && snapshot.migrationID == nil && snapshot.attemptIndex == nil &&
			snapshot.previousAttemptTerminalDigest == nil && snapshot.lineageContinuation == nil
	case RecoveryBeginNextAttempt:
		if selection.attemptIndex < 2 || snapshot.migrationID == nil || *snapshot.migrationID != selection.migrationID ||
			snapshot.attemptIndex == nil || *snapshot.attemptIndex != selection.attemptIndex ||
			snapshot.previousAttemptTerminalDigest == nil || snapshot.lineageContinuation == nil {
			return nil, false
		}
		continuation := snapshot.lineageContinuation.value
		if continuation.Validate() != nil || continuation.StartAction != "begin_next_attempt" ||
			continuation.MigrationID != selection.migrationID || continuation.AttemptIndex != selection.attemptIndex ||
			!equalDigestPointer(continuation.PreviousAttemptTerminalDigest, snapshot.previousAttemptTerminalDigest) ||
			continuation.SourceTerminalDigest != *snapshot.previousAttemptTerminalDigest {
			return nil, false
		}
		return cloneDigestPointer(snapshot.previousAttemptTerminalDigest), true
	default:
		return nil, false
	}
}
