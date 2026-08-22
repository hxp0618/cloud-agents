package migration

import (
	"context"
	"crypto/sha256"
	"strconv"
	"sync"
	"time"
)

const runnerLedgerRecoveryAdmissionPermitDigestDomain = "cloud-agents/runner-ledger-recovery-admission/permit/v1"

type runnerLedgerRecoveryAdmissionSelection struct {
	action         runnerLedgerRecoveryAction
	recoveryState  RecoveryState
	recoveryAction RecoveryAction
	profileIndex   uint8
	entryIndex     uint32
	migrationID    string
	entryDigest    Digest
	attemptIndex   uint32
	maxAttempts    uint32
	planCount      uint32
	planDigest     [32]byte
}

// runnerLedgerRecoveryCloseOnlyPermit is deliberately narrower than every
// recovery writer port. Slice B can only release the exact retained read-only
// session and advisory lock; it cannot open a transaction, append evidence,
// execute SQL, or produce an ordinary recovery result.
type runnerLedgerRecoveryCloseOnlyPermit interface {
	closeWithoutMutation(error) error
	recoveryAdmissionAction() runnerLedgerRecoveryAction
	runnerLedgerRecoveryCloseOnlyPermitSealed()
}

type runnerLedgerAbortTerminalAdmissionPermit struct {
	self *runnerLedgerAbortTerminalAdmissionPermit
	core *runnerLedgerRecoveryAdmissionPermitCore
}

type runnerLedgerCommitObservationAdmissionPermit struct {
	self *runnerLedgerCommitObservationAdmissionPermit
	core *runnerLedgerRecoveryAdmissionPermitCore
}

type runnerLedgerAmbiguousResolutionAdmissionPermit struct {
	self *runnerLedgerAmbiguousResolutionAdmissionPermit
	core *runnerLedgerRecoveryAdmissionPermitCore
}

type runnerLedgerRetryHandoffAdmissionPermit struct {
	self *runnerLedgerRetryHandoffAdmissionPermit
	core *runnerLedgerRecoveryAdmissionPermitCore
}

type runnerLedgerRecoveryExecutionAdmissionPermit struct {
	self *runnerLedgerRecoveryExecutionAdmissionPermit
	core *runnerLedgerRecoveryAdmissionPermitCore
}

type runnerLedgerReturnFailureAdmissionPermit struct {
	self *runnerLedgerReturnFailureAdmissionPermit
	core *runnerLedgerRecoveryAdmissionPermitCore
}

type runnerLedgerRecoveryAdmissionPermitCore struct {
	self                     *runnerLedgerRecoveryAdmissionPermitCore
	binding                  *runnerLedgerRecoveryAdmissionPermitBinding
	owner                    runnerLedgerRecoveryCloseOnlyPermit
	session                  DatabaseSession
	evidenceBinder           runnerLedgerRecoveryAdmissionClaimBinder
	use                      *runnerLedgerRecoveryAdmissionUseRecord
	key                      int64
	candidateBinding         *verifiedEvidenceRunBinding
	generation               generationIdentity
	evidenceBoundary         [32]byte
	recoveryDigest           [32]byte
	recoveryTail             Digest
	consumerFactSubject      Digest
	action                   runnerLedgerRecoveryAction
	ledgerDigest             Digest
	ledgerHead               string
	ledgerLength             uint32
	connectedAuthorityDigest Digest
	migrationAuthorityDigest Digest
	projectionSubject        Digest
	catalogContractDigest    *Digest
	catalogDigest            Digest
	runtimeInputs            [32]byte
	bindings                 RunnerProjectionBindings
	projection               *runnerLedgerCatalogPreflight
	database                 runnerPreparedDatabaseIdentity
	selection                runnerLedgerRecoveryAdmissionSelection
	canonical                [32]byte
	closed                   bool
}

type runnerLedgerRecoveryAdmissionPermitBinding struct {
	core             *runnerLedgerRecoveryAdmissionPermitCore
	owner            runnerLedgerRecoveryCloseOnlyPermit
	session          DatabaseSession
	evidenceBinder   runnerLedgerRecoveryAdmissionClaimBinder
	use              *runnerLedgerRecoveryAdmissionUseRecord
	key              int64
	candidateBinding *verifiedEvidenceRunBinding
	canonical        [32]byte
}

type runnerLedgerRecoveryAdmissionPermitRegistryRecord struct {
	core             *runnerLedgerRecoveryAdmissionPermitCore
	binding          *runnerLedgerRecoveryAdmissionPermitBinding
	owner            runnerLedgerRecoveryCloseOnlyPermit
	session          DatabaseSession
	evidenceBinder   runnerLedgerRecoveryAdmissionClaimBinder
	use              *runnerLedgerRecoveryAdmissionUseRecord
	key              int64
	candidateBinding *verifiedEvidenceRunBinding
	canonical        [32]byte
}

var runnerLedgerRecoveryAdmissionPermitRegistry sync.Map

func (runner *Runner) prepareRunnerLedgerRecoveryAdmission(ctx context.Context, dsn string, bundle *RuntimeBundle, plans []StatementPlan, evidence EvidenceSession, candidate OwnedCurrentCandidate, fact runnerLedgerConsumerFact) (runnerLedgerRecoveryCloseOnlyPermit, error) {
	if runner == nil || ctx == nil || bundle == nil || bundle.Manifest == nil || !fact.valid() ||
		!validGeneratedRunnerLedgerRecoveryProfiles() {
		return nil, fail(CodeProjectionNotImplemented, "runner-ledger-recovery-admission", "recovery admission inputs or generated profiles are unavailable", nil)
	}
	if err := contextAdmissionError(ctx); err != nil {
		return nil, err
	}
	action, ok := generatedRunnerLedgerRecoveryAdmissionAction(
		fact.dispatch.fact.disposition, fact.dispatch.fact.recovery.State, fact.dispatch.fact.recovery.Action,
	)
	if !ok {
		return nil, fail(CodeProjectionNotImplemented, "runner-ledger-recovery-admission-selection", "consumer transition is outside the generated recovery admission profile", nil)
	}
	binder, ok := evidence.(runnerLedgerRecoveryAdmissionClaimBinder)
	if !ok || !runnerOwnedPointer(binder) || !validOwnedCurrentCandidate(candidate) {
		return nil, fail(CodeEvidenceRecoveryRequired, "runner-ledger-recovery-admission", "same-verifier recovery evidence binder is unavailable", nil)
	}
	claim, err := binder.bindRunnerLedgerRecoveryAdmissionClaim(ctx, runnerLedgerRecoveryAdmissionClaimRequest{
		fact: fact.clone(), candidate: candidate,
	})
	if err != nil {
		return nil, err
	}
	defer revokeRunnerLedgerRecoveryAdmissionClaim(claim)

	var observation *runnerLockedLedgerCatalogObservation
	if action == generatedRunnerLedgerRecoveryProfiles[2].action || action == generatedRunnerLedgerRecoveryProfiles[3].action {
		hint, hintErr := runnerLedgerReconciliationHintFromSnapshot(evidence.RecoverySnapshot())
		if hintErr != nil {
			return nil, hintErr
		}
		if hint == nil {
			return nil, fail(CodeEvidenceRecoveryRequired, "runner-ledger-recovery-admission", "reconciliation recovery boundary is unavailable", nil)
		}
		observation, err = runner.openRunnerLockedLedgerCatalogObservationWithReconciliation(ctx, dsn, bundle, plans, evidence, candidate, hint)
	} else {
		observation, err = runner.openRunnerLockedLedgerCatalogObservation(ctx, dsn, bundle, plans, evidence, candidate)
	}
	if err != nil {
		return nil, err
	}
	failClosed := func(primary error) (runnerLedgerRecoveryCloseOnlyPermit, error) {
		return nil, observation.close(primary)
	}
	projection, err := observation.bind()
	if err != nil {
		return failClosed(err)
	}
	selection, err := validateRunnerLedgerRecoveryAdmissionObservation(fact, projection, observation, action)
	if err != nil {
		return failClosed(err)
	}
	if err := observation.revalidateRecoveryAdmission(ctx, runner); err != nil {
		return failClosed(err)
	}
	boundary, err := binder.consumeRunnerLedgerRecoveryAdmissionClaim(ctx, claim, candidate)
	if err != nil {
		return failClosed(err)
	}
	if err := contextAdmissionError(ctx); err != nil {
		return failClosed(err)
	}
	permit, err := bindRunnerLedgerRecoveryAdmissionPermit(observation, projection, binder, candidate, fact, boundary, selection)
	if err != nil {
		return failClosed(err)
	}
	return permit, nil
}

func (runner *Runner) admitRunnerLedgerRecoveryAction(ctx context.Context, dsn string, bundle *RuntimeBundle, plans []StatementPlan, evidence EvidenceSession, candidate OwnedCurrentCandidate, fact runnerLedgerConsumerFact) error {
	permit, err := runner.prepareRunnerLedgerRecoveryAdmission(ctx, dsn, bundle, plans, evidence, candidate, fact)
	if err != nil {
		return err
	}
	if abort, ok := permit.(*runnerLedgerAbortTerminalAdmissionPermit); ok {
		return runner.appendRunnerLedgerRecoveryAbortTerminal(ctx, abort, bundle, plans)
	}
	if observation, ok := permit.(*runnerLedgerCommitObservationAdmissionPermit); ok {
		return runner.appendRunnerLedgerRecoveryCommitObservation(ctx, observation, bundle, plans)
	}
	if resolution, ok := permit.(*runnerLedgerAmbiguousResolutionAdmissionPermit); ok {
		return runner.appendRunnerLedgerRecoveryAmbiguousResolution(ctx, resolution, bundle, plans)
	}
	return permit.closeWithoutMutation(nil)
}

func validateRunnerLedgerRecoveryAdmissionObservation(fact runnerLedgerConsumerFact, projection *runnerLedgerCatalogPreflight, observation *runnerLockedLedgerCatalogObservation, action runnerLedgerRecoveryAction) (runnerLedgerRecoveryAdmissionSelection, error) {
	var selection runnerLedgerRecoveryAdmissionSelection
	if !fact.valid() || fact.dispatch.fact.recovery == nil || !validRunnerLedgerCatalogPreflight(projection) ||
		observation == nil || !observation.active() || observation.bundle == nil || !validGeneratedRunnerLedgerRecoveryProfiles() {
		return selection, fail(CodeEvidenceRecoveryRequired, "runner-ledger-recovery-admission-bind", "recovery observation is unavailable or changed", nil)
	}
	profile, profileIndex, ok := runnerLedgerRecoveryActionProfile(action)
	if !ok || !generatedRunnerLedgerRecoveryProfileAllows(
		profile.profileID, fact.dispatch.fact.disposition, fact.dispatch.fact.recovery.State, fact.dispatch.fact.recovery.Action,
	) || fact.manifestDigest != observation.bundle.Manifest.ManifestDigest ||
		fact.dispatch.fact.schemaBundleDigest != projection.schemaBundleDigest ||
		fact.dispatch.fact.executionLineageDigest != projection.executionLineageDigest ||
		fact.dispatch.runnerProjectionDecisionDigest != projection.runnerProjectionDecisionDigest {
		return selection, fail(CodeEvidenceJournalCorrupt, "runner-ledger-recovery-admission-bind", "fresh database observation differs from the consumed recovery fact", nil)
	}
	if projection.reconciliation == nil {
		if fact.dispatch.fact.orderedMigrationPrefixLength != uint32(len(projection.ledger.rows)) ||
			fact.dispatch.fact.orderedMigrationPrefixDigest != projection.ledger.digest ||
			!sameOptionalString(fact.dispatch.fact.orderedMigrationPrefixHead, cloneStringPointerIfNonEmpty(projection.ledger.head)) ||
			fact.dispatch.fact.lastAppliedCatalogContractDigest != projection.projectionSubjectDigest {
			return selection, fail(CodeEvidenceJournalCorrupt, "runner-ledger-recovery-admission-bind", "fresh database observation differs from the consumed recovery fact", nil)
		}
	} else if !validRunnerLedgerReconciliationFacts(projection.reconciliation) ||
		fact.dispatch.fact.orderedMigrationPrefixLength != projection.reconciliation.targetIndex ||
		fact.dispatch.fact.lastAppliedCatalogContractDigest != projection.reconciliation.predecessorProjectionSubject ||
		fact.dispatch.fact.recovery.State != projection.reconciliation.state ||
		fact.dispatch.fact.recovery.Action != projection.reconciliation.action ||
		fact.dispatch.recoveryMigrationID == nil || *fact.dispatch.recoveryMigrationID != projection.reconciliation.migrationID ||
		fact.dispatch.recoveryAttemptIndex == nil || *fact.dispatch.recoveryAttemptIndex != projection.reconciliation.attemptIndex {
		return selection, fail(CodeEvidenceJournalCorrupt, "runner-ledger-recovery-admission-bind", "fresh reconciliation observation differs from the consumed recovery fact", nil)
	}
	switch fact.dispatch.fact.disposition {
	case runnerLedgerPreflightEmptyBrandNew:
		if projection.state != runnerLedgerCatalogEmpty || len(projection.ledger.rows) != 0 || fact.dispatch.kind != runnerLedgerPreflightDispatchEntry {
			return selection, fail(CodeEvidenceJournalCorrupt, "runner-ledger-recovery-admission-bind", "empty recovery fact observed a non-empty database prefix", nil)
		}
	case runnerLedgerPreflightPartialRetryOrRecovery:
		if fact.dispatch.kind != runnerLedgerPreflightDispatchRecovery || projection.reconciliation == nil &&
			(projection.state != runnerLedgerCatalogPartial || len(projection.ledger.rows) == 0) {
			return selection, fail(CodeEvidenceJournalCorrupt, "runner-ledger-recovery-admission-bind", "partial recovery fact observed a non-partial database prefix", nil)
		}
	default:
		return selection, fail(CodeProjectionNotImplemented, "runner-ledger-recovery-admission-selection", "consumer transition is not a recovery admission", nil)
	}
	index := int(fact.dispatch.fact.orderedMigrationPrefixLength)
	entries := observation.bundle.Manifest.SchemaBundle.Migrations
	if index < 0 || index >= len(entries) || uint64(index) > uint64(^uint32(0)) {
		return selection, fail(CodeEvidenceJournalCorrupt, "runner-ledger-recovery-admission-bind", "recovery migration index is outside the signed schema bundle", nil)
	}
	entry := entries[index]
	row := commitIntentLedgerRow(entry, observation.bundle.Manifest.SchemaBundleDigest)
	canonical, err := canonicalContractKey(row)
	if err != nil || row.Validate() != nil || canonical == "" {
		return selection, fail(CodeUntrusted, "runner-ledger-recovery-admission-bind", "selected signed recovery entry cannot be reproduced", nil)
	}
	entryDigest := DigestBytes([]byte(runnerLedgerPreflightEntryDigestDomain + "\x00" + canonical))
	if fact.dispatch.fact.nextEntry != nil &&
		(fact.dispatch.fact.nextEntry.MigrationID != entry.ID || fact.dispatch.fact.nextEntry.EntryDigest != entryDigest) {
		return selection, fail(CodeEvidenceJournalCorrupt, "runner-ledger-recovery-admission-bind", "selected recovery entry differs from the exact signed entry", nil)
	}
	if fact.dispatch.recoveryMigrationID != nil {
		if *fact.dispatch.recoveryMigrationID != entry.ID {
			return selection, fail(CodeEvidenceJournalCorrupt, "runner-ledger-recovery-admission-bind", "recovery migration differs from the selected signed entry", nil)
		}
	} else if fact.dispatch.fact.recovery.State != RecoveryBrandNewInherited || fact.dispatch.fact.recovery.Action != RecoveryBeginFirstAttempt {
		return selection, fail(CodeEvidenceJournalCorrupt, "runner-ledger-recovery-admission-bind", "recovery migration identity is unexpectedly absent", nil)
	}
	attemptIndex := uint32(1)
	if fact.dispatch.recoveryAttemptIndex != nil {
		attemptIndex = *fact.dispatch.recoveryAttemptIndex
	} else if fact.dispatch.fact.recovery.State != RecoveryBrandNewInherited || fact.dispatch.fact.recovery.Action != RecoveryBeginFirstAttempt {
		return selection, fail(CodeEvidenceJournalCorrupt, "runner-ledger-recovery-admission-bind", "recovery attempt identity is unexpectedly absent", nil)
	}
	policy := observation.bundle.Manifest.ExecutionPolicy
	if policy.Validate() != nil || policy.MaxAttempts == 0 || policy.MaxAttempts > uint64(^uint32(0)) ||
		attemptIndex == 0 || uint64(attemptIndex) > policy.MaxAttempts {
		return selection, fail(CodeEvidenceJournalCorrupt, "runner-ledger-recovery-admission-bind", "recovery attempt exceeds the signed execution policy", nil)
	}
	planDigest, planCount, err := runnerEntryPlanClosureDigest(observation.plans, entry.ID)
	if err != nil {
		return selection, err
	}
	selection = runnerLedgerRecoveryAdmissionSelection{
		action: action, recoveryState: fact.dispatch.fact.recovery.State, recoveryAction: fact.dispatch.fact.recovery.Action,
		profileIndex: profileIndex, entryIndex: uint32(index), migrationID: entry.ID,
		entryDigest: entryDigest, attemptIndex: attemptIndex, maxAttempts: uint32(policy.MaxAttempts),
		planCount: planCount, planDigest: planDigest,
	}
	return selection, nil
}

func (observation *runnerLockedLedgerCatalogObservation) revalidateRecoveryAdmission(ctx context.Context, runner *Runner) error {
	if !observation.active() || runner == nil {
		return fail(CodeTransactionBoundary, "runner-ledger-recovery-admission-revalidate", "locked recovery observation is unavailable", nil)
	}
	if err := observation.validateRecoveryAdmissionBoundary(ctx, "before final authority projection"); err != nil {
		return err
	}
	refreshedAuthority, err := runner.projectRunnerAuthorityPhase(
		ctx, observation.session, observation.bindings.verifiedAuthority, AuthorityPhaseMigrationRole,
	)
	if err != nil {
		return err
	}
	if !sameRunnerDedicatedSessionIdentity(observation.connected.Metadata.Snapshot, refreshedAuthority.Metadata.Snapshot) {
		return fail(CodeProjectionMetadataMismatch, "runner-ledger-recovery-admission-authority", "final authority projection does not describe the retained database session", nil)
	}
	if !runnerCanonicalEqual(refreshedAuthority, observation.migrationRole) {
		return fail(CodeAuthorityDrift, "runner-ledger-recovery-admission-authority", "final authority projection changed before recovery admission", nil)
	}
	if err := observation.revalidate(ctx, runner); err != nil {
		return err
	}
	return observation.validateRecoveryAdmissionBoundary(ctx, "after final ledger and catalog projection")
}

func (observation *runnerLockedLedgerCatalogObservation) validateRecoveryAdmissionBoundary(ctx context.Context, stage string) error {
	boundary, err := observation.session.Boundary(ctx, observation.key)
	if err != nil {
		return mapRunnerDatabasePreflightError(err, "runner-ledger-recovery-admission-boundary", "retained database session boundary could not be reread")
	}
	if boundary.TxStatus != 'I' {
		return fail(CodeTransactionBoundary, "runner-ledger-recovery-admission-boundary", "retained database session is not idle "+stage, nil)
	}
	if boundary.CurrentUser != MigrationOwnerRole {
		return fail(CodeAuthorityDrift, "runner-ledger-recovery-admission-boundary", "retained database role changed "+stage, nil)
	}
	if !boundary.LockHeld {
		return fail(CodeLockLost, "runner-ledger-recovery-admission-boundary", "signed advisory lock is not held "+stage, nil)
	}
	return nil
}

func runnerLedgerRecoveryActionProfile(action runnerLedgerRecoveryAction) (runnerLedgerRecoveryProfile, uint8, bool) {
	for _, index := range [...]uint8{1, 2, 3, 4, 5, 7} {
		profile := generatedRunnerLedgerRecoveryProfiles[index]
		if profile.action == action && profile.valid() {
			return profile, index, true
		}
	}
	return runnerLedgerRecoveryProfile{}, 0, false
}

func bindRunnerLedgerRecoveryAdmissionPermit(observation *runnerLockedLedgerCatalogObservation, projection *runnerLedgerCatalogPreflight, binder runnerLedgerRecoveryAdmissionClaimBinder, candidate OwnedCurrentCandidate, fact runnerLedgerConsumerFact, boundary runnerLedgerRecoveryAdmissionEvidenceBoundary, selection runnerLedgerRecoveryAdmissionSelection) (runnerLedgerRecoveryCloseOnlyPermit, error) {
	profile, profileIndex, ok := runnerLedgerRecoveryActionProfile(selection.action)
	if observation == nil || !observation.active() || binder == nil || !runnerOwnedPointer(binder) ||
		!validOwnedCurrentCandidate(candidate) || !fact.valid() || !validRunnerLedgerCatalogPreflight(projection) ||
		observation.bundle == nil || observation.bundle.ownedInputs.canonical == ([32]byte{}) ||
		observation.bindings.validateAt(time.Now()) != nil || !ok || profileIndex != selection.profileIndex ||
		!generatedRunnerLedgerRecoveryProfileAllows(profile.profileID, fact.dispatch.fact.disposition, fact.dispatch.fact.recovery.State, fact.dispatch.fact.recovery.Action) ||
		boundary.candidateBinding != candidate.binding || boundary.canonical == ([32]byte{}) ||
		boundary.canonical != runnerLedgerRecoveryAdmissionEvidenceBoundaryDigest(boundary) ||
		boundary.claimDigest == ([32]byte{}) || boundary.factSubject != fact.subjectDigest || boundary.action != selection.action ||
		boundary.generation.owner != candidate.owner ||
		boundary.generation.executionLineageDigest != observation.bindings.executionLineageDigest ||
		boundary.generation.schemaBundleDigest != observation.bindings.schemaBundleDigest ||
		boundary.generation.runnerProjectionDecisionDigest != observation.bindings.runnerProjectionDecisionDigest ||
		selection.planCount == 0 || selection.planDigest == ([32]byte{}) || selection.entryDigest.Validate() != nil ||
		!migrationIDPattern.MatchString(selection.migrationID) || selection.attemptIndex == 0 ||
		selection.maxAttempts == 0 || selection.attemptIndex > selection.maxAttempts {
		return nil, fail(CodeEvidenceRecoveryRequired, "runner-ledger-recovery-admission-permit", "recovery admission authority is unavailable or changed", nil)
	}
	useValue, useOK := runnerLedgerRecoveryAdmissionUseByEvidenceBind.Load(binder)
	use, useRecordOK := useValue.(*runnerLedgerRecoveryAdmissionUseRecord)
	if !useOK || !useRecordOK ||
		!validRunnerLedgerRecoveryAdmissionUse(binder, use, fact.subjectDigest, selection.action, boundary.canonical, true) {
		return nil, fail(CodeEvidenceRecoveryRequired, "runner-ledger-recovery-admission-permit", "consumed recovery evidence boundary is unavailable or changed", nil)
	}
	expectedKey, err := observation.bundle.Manifest.SchemaBundle.AdvisoryLock.Key()
	if err != nil || expectedKey != observation.key {
		return nil, fail(CodeUntrusted, "runner-ledger-recovery-admission-permit", "signed advisory lock differs from the retained recovery session", nil)
	}
	metadata := observation.migrationRole.Metadata.Snapshot
	catalogDigest := Digest("")
	if observation.initial != nil {
		catalogDigest = observation.initial.Digest
	} else if observation.cumulative != nil {
		catalogDigest = observation.cumulative.Digest
	} else if observation.reconciliation != nil {
		catalogDigest = observation.reconciliation.subjectDigest
	}
	if catalogDigest.Validate() != nil || !sameRunnerDedicatedSessionIdentity(observation.connected.Metadata.Snapshot, metadata) {
		return nil, fail(CodeProjectionMetadataMismatch, "runner-ledger-recovery-admission-permit", "retained database identity or catalog is unavailable", nil)
	}
	core := &runnerLedgerRecoveryAdmissionPermitCore{
		session: observation.session, evidenceBinder: binder, use: use, key: observation.key,
		candidateBinding: candidate.binding, generation: boundary.generation, evidenceBoundary: boundary.canonical,
		recoveryDigest: boundary.recoveryDigest, recoveryTail: boundary.recoveryTail,
		consumerFactSubject: fact.subjectDigest, action: selection.action,
		ledgerDigest: observation.ledger.digest, ledgerHead: observation.ledger.head,
		ledgerLength: uint32(len(observation.ledger.rows)), connectedAuthorityDigest: observation.connected.Digest,
		migrationAuthorityDigest: observation.migrationRole.Digest, projectionSubject: observation.projectionSubject,
		catalogContractDigest: cloneDigestPointer(observation.catalogContractDigest), catalogDigest: catalogDigest,
		runtimeInputs: observation.bundle.ownedInputs.canonical, bindings: observation.bindings.ownedCopy(),
		projection: cloneRunnerLedgerCatalogPreflight(projection),
		database: runnerPreparedDatabaseIdentity{
			postgresMajor: metadata.PostgresMajor, serverVersionNum: metadata.ServerVersionNum,
			databaseName: metadata.DatabaseName, sessionUser: metadata.SessionUser, currentUser: metadata.CurrentUser,
		},
		selection: selection,
	}
	core.self = core
	owner := newRunnerLedgerRecoveryActionPermit(core)
	if owner == nil {
		return nil, fail(CodeProjectionNotImplemented, "runner-ledger-recovery-admission-permit", "recovery action has no close-only permit type", nil)
	}
	core.owner = owner
	core.binding = &runnerLedgerRecoveryAdmissionPermitBinding{
		core: core, owner: owner, session: observation.session, evidenceBinder: binder, use: use,
		key: observation.key, candidateBinding: candidate.binding,
	}
	core.canonical = runnerLedgerRecoveryAdmissionPermitDigest(core)
	core.binding.canonical = core.canonical
	if core.canonical == ([32]byte{}) {
		return nil, fail(CodeTransactionBoundary, "runner-ledger-recovery-admission-permit", "recovery admission permit could not be identified", nil)
	}
	runnerLedgerRecoveryAdmissionPermitRegistry.Store(owner, runnerLedgerRecoveryAdmissionPermitRegistryRecord{
		core: core, binding: core.binding, owner: owner, session: observation.session, evidenceBinder: binder,
		use: use, key: observation.key, candidateBinding: candidate.binding, canonical: core.canonical,
	})
	if (core.action == generatedRunnerLedgerRecoveryProfiles[2].action || core.action == generatedRunnerLedgerRecoveryProfiles[3].action) &&
		!registerRunnerLedgerReconciliationAdmissionCleanup(owner, core) {
		runnerLedgerRecoveryAdmissionPermitRegistry.Delete(owner)
		deleteRunnerLedgerReconciliationAdmissionCleanup(owner)
		return nil, fail(CodeTransactionBoundary, "runner-ledger-recovery-admission-permit", "reconciliation cleanup authority could not be sealed", nil)
	}
	if !validRunnerLedgerRecoveryAdmissionPermit(owner) {
		runnerLedgerRecoveryAdmissionPermitRegistry.Delete(owner)
		deleteRunnerLedgerReconciliationAdmissionCleanup(owner)
		return nil, fail(CodeTransactionBoundary, "runner-ledger-recovery-admission-permit", "recovery admission permit could not be sealed", nil)
	}
	observation.transferred = true
	observation.session = nil
	return owner, nil
}

func newRunnerLedgerRecoveryActionPermit(core *runnerLedgerRecoveryAdmissionPermitCore) runnerLedgerRecoveryCloseOnlyPermit {
	if core == nil {
		return nil
	}
	switch core.action {
	case generatedRunnerLedgerRecoveryProfiles[1].action:
		permit := &runnerLedgerAbortTerminalAdmissionPermit{core: core}
		permit.self = permit
		return permit
	case generatedRunnerLedgerRecoveryProfiles[2].action:
		permit := &runnerLedgerCommitObservationAdmissionPermit{core: core}
		permit.self = permit
		return permit
	case generatedRunnerLedgerRecoveryProfiles[3].action:
		permit := &runnerLedgerAmbiguousResolutionAdmissionPermit{core: core}
		permit.self = permit
		return permit
	case generatedRunnerLedgerRecoveryProfiles[4].action:
		permit := &runnerLedgerRetryHandoffAdmissionPermit{core: core}
		permit.self = permit
		return permit
	case generatedRunnerLedgerRecoveryProfiles[5].action:
		permit := &runnerLedgerRecoveryExecutionAdmissionPermit{core: core}
		permit.self = permit
		return permit
	case generatedRunnerLedgerRecoveryProfiles[7].action:
		permit := &runnerLedgerReturnFailureAdmissionPermit{core: core}
		permit.self = permit
		return permit
	default:
		return nil
	}
}

func validRunnerLedgerRecoveryAdmissionPermit(owner runnerLedgerRecoveryCloseOnlyPermit) bool {
	core := runnerLedgerRecoveryPermitCore(owner)
	if core == nil || core.self != core || core.owner != owner || core.closed || core.binding == nil ||
		core.binding.core != core || core.binding.owner != owner || core.session == nil ||
		!sameRunnerOwnedPointer(core.session, core.binding.session) || core.evidenceBinder == nil ||
		!sameRunnerOwnedPointer(core.evidenceBinder, core.binding.evidenceBinder) || core.key != core.binding.key ||
		core.use == nil || core.binding.use != core.use || core.candidateBinding == nil ||
		core.binding.candidateBinding != core.candidateBinding || core.canonical == ([32]byte{}) ||
		core.binding.canonical != core.canonical || core.canonical != runnerLedgerRecoveryAdmissionPermitDigest(core) {
		return false
	}
	registered, ok := runnerLedgerRecoveryAdmissionPermitRegistry.Load(owner)
	record, recordOK := registered.(runnerLedgerRecoveryAdmissionPermitRegistryRecord)
	return ok && recordOK && record.core == core && record.binding == core.binding && record.owner == owner &&
		record.key == core.key && record.candidateBinding == core.candidateBinding && record.use == core.use &&
		record.canonical == core.canonical && sameRunnerOwnedPointer(record.session, core.session) &&
		sameRunnerOwnedPointer(record.evidenceBinder, core.evidenceBinder) &&
		validRunnerLedgerReconciliationAdmissionCleanupRegistry(owner, core) &&
		validRunnerLedgerRecoveryAdmissionUse(core.evidenceBinder, core.use, core.consumerFactSubject, core.action, core.evidenceBoundary, true)
}

func runnerLedgerRecoveryPermitCore(owner runnerLedgerRecoveryCloseOnlyPermit) *runnerLedgerRecoveryAdmissionPermitCore {
	switch permit := owner.(type) {
	case *runnerLedgerAbortTerminalAdmissionPermit:
		if permit != nil && permit.self == permit && permit.recoveryAdmissionAction() == generatedRunnerLedgerRecoveryProfiles[1].action {
			return permit.core
		}
	case *runnerLedgerCommitObservationAdmissionPermit:
		if permit != nil && permit.self == permit && permit.recoveryAdmissionAction() == generatedRunnerLedgerRecoveryProfiles[2].action {
			return permit.core
		}
	case *runnerLedgerAmbiguousResolutionAdmissionPermit:
		if permit != nil && permit.self == permit && permit.recoveryAdmissionAction() == generatedRunnerLedgerRecoveryProfiles[3].action {
			return permit.core
		}
	case *runnerLedgerRetryHandoffAdmissionPermit:
		if permit != nil && permit.self == permit && permit.recoveryAdmissionAction() == generatedRunnerLedgerRecoveryProfiles[4].action {
			return permit.core
		}
	case *runnerLedgerRecoveryExecutionAdmissionPermit:
		if permit != nil && permit.self == permit && permit.recoveryAdmissionAction() == generatedRunnerLedgerRecoveryProfiles[5].action {
			return permit.core
		}
	case *runnerLedgerReturnFailureAdmissionPermit:
		if permit != nil && permit.self == permit && permit.recoveryAdmissionAction() == generatedRunnerLedgerRecoveryProfiles[7].action {
			return permit.core
		}
	}
	return nil
}

func runnerLedgerRecoveryAdmissionPermitDigest(core *runnerLedgerRecoveryAdmissionPermitCore) [32]byte {
	if core == nil || core.self != core || core.owner == nil || core.closed || core.session == nil ||
		core.evidenceBinder == nil || core.use == nil || core.candidateBinding == nil || core.generation.owner == nil ||
		core.generation.owner != core.candidateBinding.owner || core.evidenceBoundary == ([32]byte{}) ||
		core.recoveryDigest == ([32]byte{}) || core.recoveryTail.Validate() != nil ||
		core.consumerFactSubject.Validate() != nil || core.ledgerDigest.Validate() != nil ||
		core.connectedAuthorityDigest.Validate() != nil || core.migrationAuthorityDigest.Validate() != nil ||
		core.projectionSubject.Validate() != nil || core.catalogDigest.Validate() != nil ||
		core.database.postgresMajor == 0 || core.database.serverVersionNum == 0 || core.database.databaseName == "" ||
		core.database.sessionUser == "" || core.database.currentUser != MigrationOwnerRole || core.runtimeInputs == ([32]byte{}) ||
		core.bindings.validateAt(time.Now()) != nil || core.bindings.expectedCanonical == "" ||
		!runnerLedgerRecoveryAdmissionProjectionMatchesCore(core) || !runnerLedgerRecoverySelectionAllowed(core.selection) ||
		core.selection.action != core.action || core.selection.recoveryState == "" || core.selection.recoveryAction == "" ||
		core.selection.entryDigest.Validate() != nil ||
		!migrationIDPattern.MatchString(core.selection.migrationID) || core.selection.planCount == 0 ||
		core.selection.planDigest == ([32]byte{}) || core.selection.attemptIndex == 0 ||
		core.selection.maxAttempts == 0 || core.selection.attemptIndex > core.selection.maxAttempts ||
		!validGeneratedRunnerLedgerRecoveryProfiles() {
		return [32]byte{}
	}
	profile, profileIndex, ok := runnerLedgerRecoveryActionProfile(core.action)
	if !ok || profileIndex != core.selection.profileIndex || core.owner.recoveryAdmissionAction() != core.action {
		return [32]byte{}
	}
	if core.catalogContractDigest != nil && core.catalogContractDigest.Validate() != nil {
		return [32]byte{}
	}
	for _, value := range []Digest{
		core.generation.executionLineageDigest, core.generation.journalIdentityDigest,
		core.generation.runnerProjectionDecisionDigest, core.generation.schemaBundleDigest,
	} {
		if value.Validate() != nil {
			return [32]byte{}
		}
	}
	h := sha256.New()
	h.Write([]byte(runnerLedgerRecoveryAdmissionPermitDigestDomain + "\x00"))
	h.Write(core.candidateBinding.canonical[:])
	h.Write(core.evidenceBoundary[:])
	h.Write(core.recoveryDigest[:])
	h.Write(core.selection.planDigest[:])
	h.Write(core.runtimeInputs[:])
	writeRunnerLedgerRecoveryIdentity(h, core.action)
	for _, value := range []string{
		profile.registryID, profile.registryDigest, profile.profileID, profile.profileDigest,
		profile.stateMachineDigest, profile.policyDigest, string(core.action),
		core.generation.executionLineageDigest.String(), core.generation.journalIdentityDigest.String(),
		core.generation.runnerProjectionDecisionDigest.String(), core.generation.schemaBundleDigest.String(),
		core.consumerFactSubject.String(), core.recoveryTail.String(), core.ledgerDigest.String(), core.ledgerHead,
		core.connectedAuthorityDigest.String(), core.migrationAuthorityDigest.String(), core.projectionSubject.String(),
		core.catalogDigest.String(), strconv.FormatInt(core.key, 10), core.database.databaseName,
		core.database.sessionUser, core.database.currentUser, core.selection.migrationID, core.selection.entryDigest.String(),
		core.bindings.expectedCanonical, core.projection.subjectDigest.String(),
	} {
		writeAdmissionString(h, value)
	}
	if core.catalogContractDigest == nil {
		writeAdmissionString(h, "catalog-contract:absent")
	} else {
		writeAdmissionString(h, "catalog-contract:present")
		writeAdmissionString(h, core.catalogContractDigest.String())
	}
	writeAdmissionUint(h, uint64(profileIndex))
	writeAdmissionUint(h, uint64(core.ledgerLength))
	writeAdmissionUint(h, uint64(core.database.postgresMajor))
	writeAdmissionUint(h, uint64(core.database.serverVersionNum))
	writeAdmissionUint(h, uint64(core.selection.entryIndex))
	writeAdmissionUint(h, uint64(core.selection.attemptIndex))
	writeAdmissionUint(h, uint64(core.selection.maxAttempts))
	writeAdmissionUint(h, uint64(core.selection.planCount))
	writeAdmissionString(h, string(core.selection.recoveryState))
	writeAdmissionString(h, string(core.selection.recoveryAction))
	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result
}

func cloneRunnerLedgerCatalogPreflight(prepared *runnerLedgerCatalogPreflight) *runnerLedgerCatalogPreflight {
	if prepared == nil {
		return nil
	}
	owned := *prepared
	owned.catalogContractDigest = cloneDigestPointer(prepared.catalogContractDigest)
	owned.ledger = cloneRunnerLedgerPrefix(prepared.ledger)
	owned.connectedAuthority = cloneProjectionValue(prepared.connectedAuthority)
	owned.migrationRoleAuthority = cloneProjectionValue(prepared.migrationRoleAuthority)
	owned.initialPredecessor = cloneCatalogStateProjectionResultPointer(prepared.initialPredecessor)
	owned.cumulativeCatalog = cloneCatalogProjectionResultPointer(prepared.cumulativeCatalog)
	owned.reconciliation = cloneRunnerLedgerReconciliationFacts(prepared.reconciliation)
	return &owned
}

func runnerLedgerRecoveryAdmissionProjectionMatchesCore(core *runnerLedgerRecoveryAdmissionPermitCore) bool {
	if core == nil || core.projection == nil || !validRunnerLedgerCatalogPreflight(core.projection) ||
		core.projection.schemaBundleDigest != core.generation.schemaBundleDigest ||
		core.projection.executionLineageDigest != core.generation.executionLineageDigest ||
		core.projection.runnerProjectionDecisionDigest != core.generation.runnerProjectionDecisionDigest ||
		core.projection.authoritySubjectDigest != core.bindings.verifiedAuthority.SubjectDigest() ||
		core.projection.projectionSubjectDigest != core.projectionSubject ||
		core.projection.ledger.digest != core.ledgerDigest || core.projection.ledger.head != core.ledgerHead ||
		uint32(len(core.projection.ledger.rows)) != core.ledgerLength ||
		core.projection.connectedAuthority.Digest != core.connectedAuthorityDigest ||
		core.projection.migrationRoleAuthority.Digest != core.migrationAuthorityDigest ||
		!runnerLedgerRecoveryAdmissionDatabaseMatches(core.database, core.projection.migrationRoleAuthority.Metadata.Snapshot) ||
		!sameRunnerDedicatedSessionIdentity(core.projection.connectedAuthority.Metadata.Snapshot, core.projection.migrationRoleAuthority.Metadata.Snapshot) ||
		(core.projection.catalogContractDigest == nil) != (core.catalogContractDigest == nil) {
		return false
	}
	if core.catalogContractDigest != nil && *core.projection.catalogContractDigest != *core.catalogContractDigest {
		return false
	}
	catalogDigest := Digest("")
	if core.projection.initialPredecessor != nil {
		catalogDigest = core.projection.initialPredecessor.Digest
	} else if core.projection.cumulativeCatalog != nil {
		catalogDigest = core.projection.cumulativeCatalog.Digest
	} else if core.projection.reconciliation != nil {
		catalogDigest = core.projection.reconciliation.subjectDigest
	}
	return catalogDigest == core.catalogDigest && core.bindings.schemaBundleDigest == core.generation.schemaBundleDigest &&
		core.bindings.executionLineageDigest == core.generation.executionLineageDigest &&
		core.bindings.runnerProjectionDecisionDigest == core.generation.runnerProjectionDecisionDigest
}

func runnerLedgerRecoveryAdmissionDatabaseMatches(database runnerPreparedDatabaseIdentity, metadata SnapshotMetadata) bool {
	return metadata.validate() == nil && metadata.AuthorityPhase == AuthorityPhaseMigrationRole &&
		metadata.PostgresMajor == database.postgresMajor && metadata.ServerVersionNum == database.serverVersionNum &&
		metadata.DatabaseName == database.databaseName && metadata.SessionUser == database.sessionUser &&
		metadata.CurrentUser == database.currentUser && database.currentUser == MigrationOwnerRole
}

func closeRunnerLedgerRecoveryAdmissionPermit(owner runnerLedgerRecoveryCloseOnlyPermit, expected runnerLedgerRecoveryAction, primary error) error {
	if owner == nil {
		return primary
	}
	if expected == generatedRunnerLedgerRecoveryProfiles[2].action || expected == generatedRunnerLedgerRecoveryProfiles[3].action {
		return closeRunnerLedgerReconciliationAdmissionPermit(owner, expected, primary)
	}
	registered, ok := runnerLedgerRecoveryAdmissionPermitRegistry.LoadAndDelete(owner)
	record, recordOK := registered.(runnerLedgerRecoveryAdmissionPermitRegistryRecord)
	if !ok || !recordOK || record.owner != owner || record.session == nil {
		return fail(CodeTransactionBoundary, "runner-ledger-recovery-admission-close", "recovery admission permit is unavailable or already closed", nil)
	}
	core := record.core
	valid := core != nil && runnerLedgerRecoveryPermitCore(owner) == core && core.action == expected &&
		owner.recoveryAdmissionAction() == expected && validRunnerLedgerRecoveryAdmissionPermitWithRecord(owner, core, record)
	if core != nil {
		core.closed = true
		core.session = nil
		core.evidenceBinder = nil
		core.use = nil
	}
	if !valid {
		primary = fail(CodeTransactionBoundary, "runner-ledger-recovery-admission-close", "recovery admission permit changed before close", nil)
	}
	return closeRunnerDatabasePreflight(record.session, record.key, true, primary)
}

func validRunnerLedgerRecoveryAdmissionPermitWithRecord(owner runnerLedgerRecoveryCloseOnlyPermit, core *runnerLedgerRecoveryAdmissionPermitCore, record runnerLedgerRecoveryAdmissionPermitRegistryRecord) bool {
	if core == nil || record.core != core || record.binding != core.binding || record.owner != owner ||
		record.key != core.key || record.candidateBinding != core.candidateBinding || record.use != core.use ||
		record.canonical != core.canonical || !sameRunnerOwnedPointer(record.session, core.session) ||
		!sameRunnerOwnedPointer(record.evidenceBinder, core.evidenceBinder) ||
		!sameRunnerOwnedPointer(record.binding.session, record.session) ||
		!sameRunnerOwnedPointer(record.binding.evidenceBinder, record.evidenceBinder) ||
		record.binding.core != core || record.binding.owner != owner || record.binding.use != record.use ||
		record.binding.candidateBinding != record.candidateBinding {
		return false
	}
	if !validRunnerLedgerRecoveryAdmissionUse(core.evidenceBinder, core.use, core.consumerFactSubject, core.action, core.evidenceBoundary, true) {
		return false
	}
	return core.self == core && core.owner == owner && !core.closed && core.binding != nil &&
		core.binding.core == core && core.binding.owner == owner && core.binding.key == core.key &&
		core.binding.use == core.use && core.binding.candidateBinding == core.candidateBinding &&
		sameRunnerOwnedPointer(core.binding.session, core.session) &&
		sameRunnerOwnedPointer(core.binding.evidenceBinder, core.evidenceBinder) &&
		core.binding.canonical == core.canonical && core.canonical == runnerLedgerRecoveryAdmissionPermitDigest(core)
}

func (permit *runnerLedgerAbortTerminalAdmissionPermit) closeWithoutMutation(primary error) error {
	if permit == nil {
		return fail(CodeTransactionBoundary, "runner-ledger-recovery-admission-close", "abort-terminal permit copy cannot close database authority", nil)
	}
	return closeRunnerLedgerRecoveryAdmissionPermit(permit, generatedRunnerLedgerRecoveryProfiles[1].action, primary)
}

func (*runnerLedgerAbortTerminalAdmissionPermit) recoveryAdmissionAction() runnerLedgerRecoveryAction {
	return generatedRunnerLedgerRecoveryProfiles[1].action
}
func (*runnerLedgerAbortTerminalAdmissionPermit) runnerLedgerRecoveryCloseOnlyPermitSealed() {}

func (permit *runnerLedgerCommitObservationAdmissionPermit) closeWithoutMutation(primary error) error {
	if permit == nil {
		return fail(CodeTransactionBoundary, "runner-ledger-recovery-admission-close", "commit-observation permit copy cannot close database authority", nil)
	}
	return closeRunnerLedgerRecoveryAdmissionPermit(permit, generatedRunnerLedgerRecoveryProfiles[2].action, primary)
}

func (*runnerLedgerCommitObservationAdmissionPermit) recoveryAdmissionAction() runnerLedgerRecoveryAction {
	return generatedRunnerLedgerRecoveryProfiles[2].action
}
func (*runnerLedgerCommitObservationAdmissionPermit) runnerLedgerRecoveryCloseOnlyPermitSealed() {}

func (permit *runnerLedgerAmbiguousResolutionAdmissionPermit) closeWithoutMutation(primary error) error {
	if permit == nil {
		return fail(CodeTransactionBoundary, "runner-ledger-recovery-admission-close", "ambiguous-resolution permit copy cannot close database authority", nil)
	}
	return closeRunnerLedgerRecoveryAdmissionPermit(permit, generatedRunnerLedgerRecoveryProfiles[3].action, primary)
}

func (*runnerLedgerAmbiguousResolutionAdmissionPermit) recoveryAdmissionAction() runnerLedgerRecoveryAction {
	return generatedRunnerLedgerRecoveryProfiles[3].action
}
func (*runnerLedgerAmbiguousResolutionAdmissionPermit) runnerLedgerRecoveryCloseOnlyPermitSealed() {}

func (permit *runnerLedgerRetryHandoffAdmissionPermit) closeWithoutMutation(primary error) error {
	if permit == nil {
		return fail(CodeTransactionBoundary, "runner-ledger-recovery-admission-close", "retry-handoff permit copy cannot close database authority", nil)
	}
	return closeRunnerLedgerRecoveryAdmissionPermit(permit, generatedRunnerLedgerRecoveryProfiles[4].action, primary)
}

func (*runnerLedgerRetryHandoffAdmissionPermit) recoveryAdmissionAction() runnerLedgerRecoveryAction {
	return generatedRunnerLedgerRecoveryProfiles[4].action
}
func (*runnerLedgerRetryHandoffAdmissionPermit) runnerLedgerRecoveryCloseOnlyPermitSealed() {}

func (permit *runnerLedgerRecoveryExecutionAdmissionPermit) closeWithoutMutation(primary error) error {
	if permit == nil {
		return fail(CodeTransactionBoundary, "runner-ledger-recovery-admission-close", "recovery-execution permit copy cannot close database authority", nil)
	}
	return closeRunnerLedgerRecoveryAdmissionPermit(permit, generatedRunnerLedgerRecoveryProfiles[5].action, primary)
}

func (*runnerLedgerRecoveryExecutionAdmissionPermit) recoveryAdmissionAction() runnerLedgerRecoveryAction {
	return generatedRunnerLedgerRecoveryProfiles[5].action
}
func (*runnerLedgerRecoveryExecutionAdmissionPermit) runnerLedgerRecoveryCloseOnlyPermitSealed() {}

func (permit *runnerLedgerReturnFailureAdmissionPermit) closeWithoutMutation(primary error) error {
	if permit == nil {
		return fail(CodeTransactionBoundary, "runner-ledger-recovery-admission-close", "return-failure permit copy cannot close database authority", nil)
	}
	return closeRunnerLedgerRecoveryAdmissionPermit(permit, generatedRunnerLedgerRecoveryProfiles[7].action, primary)
}

func (*runnerLedgerReturnFailureAdmissionPermit) recoveryAdmissionAction() runnerLedgerRecoveryAction {
	return generatedRunnerLedgerRecoveryProfiles[7].action
}
func (*runnerLedgerReturnFailureAdmissionPermit) runnerLedgerRecoveryCloseOnlyPermitSealed() {}
