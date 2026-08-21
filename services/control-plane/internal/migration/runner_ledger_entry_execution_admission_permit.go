package migration

import (
	"context"
	"crypto/sha256"
	"strconv"
	"sync"
)

const runnerLedgerEntryExecutionPermitDigestDomain = "cloud-agents/runner-ledger-entry-execution-admission/permit/v1"

// runnerLedgerEntryExecutionPermit retains one fresh dedicated database
// session and its signed advisory lock. In Slice B its only consumer is
// closeRunnerLedgerEntryExecutionPermit. No transaction, SQL, ledger, or
// evidence mutation operation is defined on this type.
type runnerLedgerEntryExecutionPermit struct {
	self                     *runnerLedgerEntryExecutionPermit
	binding                  *runnerLedgerEntryExecutionPermitBinding
	session                  DatabaseSession
	evidenceBinder           runnerLedgerEntryExecutionAdmissionClaimBinder
	use                      *runnerLedgerEntryExecutionAdmissionUseRecord
	key                      int64
	candidateBinding         *verifiedEvidenceRunBinding
	generation               generationIdentity
	evidenceBoundary         [32]byte
	consumerFactSubject      Digest
	action                   runnerLedgerEntryExecutionAdmissionAction
	ledgerDigest             Digest
	ledgerHead               string
	ledgerLength             uint32
	connectedAuthorityDigest Digest
	migrationAuthorityDigest Digest
	projectionSubject        Digest
	catalogContractDigest    *Digest
	catalogDigest            Digest
	database                 runnerPreparedDatabaseIdentity
	selection                runnerLedgerEntryAdmissionSelection
	canonical                [32]byte
	closed                   bool
}

type runnerLedgerEntryExecutionPermitBinding struct {
	permit           *runnerLedgerEntryExecutionPermit
	session          DatabaseSession
	evidenceBinder   runnerLedgerEntryExecutionAdmissionClaimBinder
	use              *runnerLedgerEntryExecutionAdmissionUseRecord
	key              int64
	candidateBinding *verifiedEvidenceRunBinding
	canonical        [32]byte
}

type runnerLedgerEntryExecutionPermitRegistryRecord struct {
	permit           *runnerLedgerEntryExecutionPermit
	binding          *runnerLedgerEntryExecutionPermitBinding
	session          DatabaseSession
	evidenceBinder   runnerLedgerEntryExecutionAdmissionClaimBinder
	use              *runnerLedgerEntryExecutionAdmissionUseRecord
	key              int64
	candidateBinding *verifiedEvidenceRunBinding
	canonical        [32]byte
}

var runnerLedgerEntryExecutionPermitRegistry sync.Map

func (runner *Runner) prepareRunnerLedgerEntryExecutionAdmission(ctx context.Context, dsn string, bundle *RuntimeBundle, plans []StatementPlan, evidence EvidenceSession, candidate OwnedCurrentCandidate, fact runnerLedgerConsumerFact) (*runnerLedgerEntryExecutionPermit, error) {
	if runner == nil || ctx == nil || bundle == nil || bundle.Manifest == nil || !fact.valid() ||
		!validRunnerLedgerEntryExecutionAdmissionProfiles() {
		return nil, fail(CodeProjectionNotImplemented, "runner-ledger-entry-execution-admission", "execution admission inputs or generated profiles are unavailable", nil)
	}
	if err := contextAdmissionError(ctx); err != nil {
		return nil, err
	}
	binder, ok := evidence.(runnerLedgerEntryExecutionAdmissionClaimBinder)
	if !ok || !runnerOwnedPointer(binder) || !validOwnedCurrentCandidate(candidate) {
		return nil, fail(CodeEvidenceRecoveryRequired, "runner-ledger-entry-execution-admission", "same-verifier execution evidence binder is unavailable", nil)
	}
	claim, err := binder.bindRunnerLedgerEntryExecutionAdmissionClaim(ctx, runnerLedgerEntryExecutionAdmissionClaimRequest{
		fact: fact.clone(), candidate: candidate,
	})
	if err != nil {
		return nil, err
	}
	defer revokeRunnerLedgerEntryExecutionAdmissionClaim(claim)

	observation, err := runner.openRunnerLockedLedgerCatalogObservation(ctx, dsn, bundle, plans, evidence, candidate)
	if err != nil {
		return nil, err
	}
	failClosed := func(primary error) (*runnerLedgerEntryExecutionPermit, error) {
		return nil, observation.close(primary)
	}
	projection, err := observation.bind()
	if err != nil {
		return failClosed(err)
	}
	selection, err := validateRunnerLedgerEntryAdmissionObservation(fact, projection, observation)
	if err != nil {
		return failClosed(err)
	}
	if err := observation.revalidateExecutionAdmission(ctx, runner); err != nil {
		return failClosed(err)
	}
	boundary, err := binder.consumeRunnerLedgerEntryExecutionAdmissionClaim(ctx, claim, candidate)
	if err != nil {
		return failClosed(err)
	}
	if err := contextAdmissionError(ctx); err != nil {
		return failClosed(err)
	}
	action, err := validateRunnerLedgerEntryExecutionAdmissionSelection(fact, selection)
	if err != nil {
		return failClosed(err)
	}
	permit, err := bindRunnerLedgerEntryExecutionPermit(observation, binder, candidate, fact, boundary, selection, action)
	if err != nil {
		return failClosed(err)
	}
	return permit, nil
}

func (observation *runnerLockedLedgerCatalogObservation) revalidateExecutionAdmission(ctx context.Context, runner *Runner) error {
	if !observation.active() || runner == nil {
		return fail(CodeTransactionBoundary, "runner-ledger-entry-execution-admission-revalidate", "locked execution observation is unavailable", nil)
	}
	if err := observation.validateExecutionAdmissionBoundary(ctx, "before final authority projection"); err != nil {
		return err
	}
	refreshedAuthority, err := runner.projectRunnerAuthorityPhase(
		ctx, observation.session, observation.bindings.verifiedAuthority, AuthorityPhaseMigrationRole,
	)
	if err != nil {
		return err
	}
	if !sameRunnerDedicatedSessionIdentity(observation.connected.Metadata.Snapshot, refreshedAuthority.Metadata.Snapshot) {
		return fail(CodeProjectionMetadataMismatch, "runner-ledger-entry-execution-admission-authority", "final authority projection does not describe the retained database session", nil)
	}
	if !runnerCanonicalEqual(refreshedAuthority, observation.migrationRole) {
		return fail(CodeAuthorityDrift, "runner-ledger-entry-execution-admission-authority", "final authority projection changed before execution admission", nil)
	}
	if err := observation.revalidate(ctx, runner); err != nil {
		return err
	}
	return observation.validateExecutionAdmissionBoundary(ctx, "after final ledger and catalog projection")
}

func (observation *runnerLockedLedgerCatalogObservation) validateExecutionAdmissionBoundary(ctx context.Context, stage string) error {
	boundary, err := observation.session.Boundary(ctx, observation.key)
	if err != nil {
		return mapRunnerDatabasePreflightError(
			err, "runner-ledger-entry-execution-admission-boundary", "retained database session boundary could not be reread",
		)
	}
	if boundary.TxStatus != 'I' {
		return fail(CodeTransactionBoundary, "runner-ledger-entry-execution-admission-boundary", "retained database session is not idle "+stage, nil)
	}
	if boundary.CurrentUser != MigrationOwnerRole {
		return fail(CodeAuthorityDrift, "runner-ledger-entry-execution-admission-boundary", "retained database role changed "+stage, nil)
	}
	if !boundary.LockHeld {
		return fail(CodeLockLost, "runner-ledger-entry-execution-admission-boundary", "signed advisory lock is not held "+stage, nil)
	}
	return nil
}

func validateRunnerLedgerEntryExecutionAdmissionSelection(fact runnerLedgerConsumerFact, selection runnerLedgerEntryAdmissionSelection) (runnerLedgerEntryExecutionAdmissionAction, error) {
	if !fact.valid() || fact.dispatch.fact.recovery == nil || fact.dispatch.fact.nextEntry == nil ||
		!validRunnerLedgerEntryExecutionAdmissionProfiles() ||
		selection.entryDigest.Validate() != nil || !migrationIDPattern.MatchString(selection.migrationID) ||
		selection.planCount == 0 || selection.planDigest == ([32]byte{}) {
		return "", fail(CodeEvidenceRecoveryRequired, "runner-ledger-entry-execution-admission-selection", "execution selection is unavailable or changed", nil)
	}
	action, ok := generatedRunnerLedgerEntryExecutionAdmissionAction(
		fact.dispatch.fact.disposition,
		fact.dispatch.fact.recovery.State,
		fact.dispatch.fact.recovery.Action,
	)
	if !ok || action != runnerLedgerEntryExecutionAdmissionPrepare {
		return "", fail(CodeProjectionNotImplemented, "runner-ledger-entry-execution-admission-selection", "entry transition is outside the generated execution-admission profile", nil)
	}
	if selection.entryIndex != fact.dispatch.fact.orderedMigrationPrefixLength ||
		selection.migrationID != fact.dispatch.fact.nextEntry.MigrationID ||
		selection.entryDigest != fact.dispatch.fact.nextEntry.EntryDigest {
		return "", fail(CodeEvidenceJournalCorrupt, "runner-ledger-entry-execution-admission-selection", "selected entry differs from the consumed generated fact", nil)
	}
	return action, nil
}

func bindRunnerLedgerEntryExecutionPermit(observation *runnerLockedLedgerCatalogObservation, binder runnerLedgerEntryExecutionAdmissionClaimBinder, candidate OwnedCurrentCandidate, fact runnerLedgerConsumerFact, boundary runnerLedgerEntryExecutionAdmissionEvidenceBoundary, selection runnerLedgerEntryAdmissionSelection, action runnerLedgerEntryExecutionAdmissionAction) (*runnerLedgerEntryExecutionPermit, error) {
	if observation == nil || !observation.active() || binder == nil || !runnerOwnedPointer(binder) ||
		!validOwnedCurrentCandidate(candidate) || !fact.valid() ||
		!validRunnerLedgerEntryExecutionAdmissionProfiles() ||
		boundary.candidateBinding != candidate.binding || boundary.canonical == ([32]byte{}) ||
		boundary.canonical != runnerLedgerEntryExecutionAdmissionEvidenceBoundaryDigest(boundary) ||
		boundary.claimDigest == ([32]byte{}) || boundary.factSubject != fact.subjectDigest ||
		boundary.generation.owner != candidate.owner ||
		boundary.generation.executionLineageDigest != observation.bindings.executionLineageDigest ||
		boundary.generation.schemaBundleDigest != observation.bindings.schemaBundleDigest ||
		boundary.generation.runnerProjectionDecisionDigest != observation.bindings.runnerProjectionDecisionDigest ||
		action != runnerLedgerEntryExecutionAdmissionPrepare || selection.planCount == 0 ||
		selection.planDigest == ([32]byte{}) || selection.entryDigest.Validate() != nil ||
		!migrationIDPattern.MatchString(selection.migrationID) {
		return nil, fail(CodeEvidenceRecoveryRequired, "runner-ledger-entry-execution-admission-permit", "execution admission authority is unavailable or changed", nil)
	}
	wantAction, ok := generatedRunnerLedgerEntryExecutionAdmissionAction(
		fact.dispatch.fact.disposition, fact.dispatch.fact.recovery.State, fact.dispatch.fact.recovery.Action,
	)
	if !ok || wantAction != action {
		return nil, fail(CodeProjectionNotImplemented, "runner-ledger-entry-execution-admission-permit", "entry transition is outside the generated execution profile", nil)
	}
	useValue, useOK := runnerLedgerEntryExecutionAdmissionUseByEvidenceBinder.Load(binder)
	use, useRecordOK := useValue.(*runnerLedgerEntryExecutionAdmissionUseRecord)
	if !useOK || !useRecordOK ||
		!validRunnerLedgerEntryExecutionAdmissionUse(binder, use, fact.subjectDigest, boundary.canonical, true) {
		return nil, fail(CodeEvidenceRecoveryRequired, "runner-ledger-entry-execution-admission-permit", "consumed evidence boundary is unavailable or changed", nil)
	}
	expectedKey, err := observation.bundle.Manifest.SchemaBundle.AdvisoryLock.Key()
	if err != nil || expectedKey != observation.key {
		return nil, fail(CodeUntrusted, "runner-ledger-entry-execution-admission-permit", "signed advisory lock differs from the retained session", nil)
	}
	metadata := observation.migrationRole.Metadata.Snapshot
	catalogDigest := Digest("")
	if observation.initial != nil {
		catalogDigest = observation.initial.Digest
	} else if observation.cumulative != nil {
		catalogDigest = observation.cumulative.Digest
	}
	if catalogDigest.Validate() != nil ||
		!sameRunnerDedicatedSessionIdentity(observation.connected.Metadata.Snapshot, metadata) {
		return nil, fail(CodeProjectionMetadataMismatch, "runner-ledger-entry-execution-admission-permit", "retained database identity or catalog is unavailable", nil)
	}
	permit := &runnerLedgerEntryExecutionPermit{
		session:                  observation.session,
		evidenceBinder:           binder,
		use:                      use,
		key:                      observation.key,
		candidateBinding:         candidate.binding,
		generation:               boundary.generation,
		evidenceBoundary:         boundary.canonical,
		consumerFactSubject:      fact.subjectDigest,
		action:                   action,
		ledgerDigest:             observation.ledger.digest,
		ledgerHead:               observation.ledger.head,
		ledgerLength:             uint32(len(observation.ledger.rows)),
		connectedAuthorityDigest: observation.connected.Digest,
		migrationAuthorityDigest: observation.migrationRole.Digest,
		projectionSubject:        observation.projectionSubject,
		catalogContractDigest:    cloneDigestPointer(observation.catalogContractDigest),
		catalogDigest:            catalogDigest,
		database: runnerPreparedDatabaseIdentity{
			postgresMajor: metadata.PostgresMajor, serverVersionNum: metadata.ServerVersionNum,
			databaseName: metadata.DatabaseName, sessionUser: metadata.SessionUser, currentUser: metadata.CurrentUser,
		},
		selection: selection,
	}
	permit.self = permit
	permit.binding = &runnerLedgerEntryExecutionPermitBinding{
		permit: permit, session: observation.session, evidenceBinder: binder, use: use,
		key: observation.key, candidateBinding: candidate.binding,
	}
	permit.canonical = runnerLedgerEntryExecutionPermitDigest(permit)
	permit.binding.canonical = permit.canonical
	if permit.canonical == ([32]byte{}) {
		return nil, fail(CodeTransactionBoundary, "runner-ledger-entry-execution-admission-permit", "execution permit could not be identified", nil)
	}
	runnerLedgerEntryExecutionPermitRegistry.Store(permit, runnerLedgerEntryExecutionPermitRegistryRecord{
		permit: permit, binding: permit.binding, session: observation.session, evidenceBinder: binder,
		use: use, key: observation.key, candidateBinding: candidate.binding, canonical: permit.canonical,
	})
	if !validRunnerLedgerEntryExecutionPermit(permit) {
		runnerLedgerEntryExecutionPermitRegistry.Delete(permit)
		return nil, fail(CodeTransactionBoundary, "runner-ledger-entry-execution-admission-permit", "execution permit could not be sealed", nil)
	}
	observation.transferred = true
	observation.session = nil
	return permit, nil
}

func validRunnerLedgerEntryExecutionPermit(permit *runnerLedgerEntryExecutionPermit) bool {
	if permit == nil || permit.self != permit || permit.closed || permit.binding == nil || permit.binding.permit != permit ||
		permit.session == nil || !sameRunnerOwnedPointer(permit.session, permit.binding.session) ||
		permit.evidenceBinder == nil || !sameRunnerOwnedPointer(permit.evidenceBinder, permit.binding.evidenceBinder) ||
		permit.key != permit.binding.key || permit.use == nil || permit.binding.use != permit.use ||
		permit.candidateBinding == nil || permit.binding.candidateBinding != permit.candidateBinding ||
		permit.canonical == ([32]byte{}) || permit.binding.canonical != permit.canonical ||
		permit.canonical != runnerLedgerEntryExecutionPermitDigest(permit) {
		return false
	}
	registered, ok := runnerLedgerEntryExecutionPermitRegistry.Load(permit)
	record, recordOK := registered.(runnerLedgerEntryExecutionPermitRegistryRecord)
	return ok && recordOK &&
		validRunnerLedgerEntryExecutionAdmissionUse(permit.evidenceBinder, permit.use, permit.consumerFactSubject, permit.evidenceBoundary, true) &&
		record.permit == permit && record.binding == permit.binding && record.key == permit.key &&
		record.candidateBinding == permit.candidateBinding && record.use == permit.use && record.canonical == permit.canonical &&
		sameRunnerOwnedPointer(record.session, permit.session) && sameRunnerOwnedPointer(record.evidenceBinder, permit.evidenceBinder)
}

func runnerLedgerEntryExecutionPermitDigest(permit *runnerLedgerEntryExecutionPermit) [32]byte {
	if permit == nil || permit.self != permit || permit.closed || permit.session == nil || permit.evidenceBinder == nil || permit.use == nil ||
		permit.candidateBinding == nil || permit.generation.owner == nil || permit.generation.owner != permit.candidateBinding.owner ||
		permit.evidenceBoundary == ([32]byte{}) || permit.consumerFactSubject.Validate() != nil ||
		permit.action != runnerLedgerEntryExecutionAdmissionPrepare || permit.ledgerDigest.Validate() != nil ||
		permit.connectedAuthorityDigest.Validate() != nil || permit.migrationAuthorityDigest.Validate() != nil ||
		permit.projectionSubject.Validate() != nil || permit.catalogDigest.Validate() != nil ||
		permit.database.postgresMajor == 0 || permit.database.serverVersionNum == 0 ||
		permit.database.databaseName == "" || permit.database.sessionUser == "" ||
		permit.database.currentUser != MigrationOwnerRole || permit.selection.entryDigest.Validate() != nil ||
		!migrationIDPattern.MatchString(permit.selection.migrationID) || permit.selection.planCount == 0 ||
		permit.selection.planDigest == ([32]byte{}) || !validRunnerLedgerEntryExecutionAdmissionProfiles() {
		return [32]byte{}
	}
	if permit.catalogContractDigest != nil && permit.catalogContractDigest.Validate() != nil {
		return [32]byte{}
	}
	for _, value := range []Digest{
		permit.generation.executionLineageDigest, permit.generation.journalIdentityDigest,
		permit.generation.runnerProjectionDecisionDigest, permit.generation.schemaBundleDigest,
	} {
		if value.Validate() != nil {
			return [32]byte{}
		}
	}
	h := sha256.New()
	h.Write([]byte(runnerLedgerEntryExecutionPermitDigestDomain + "\x00"))
	h.Write(permit.candidateBinding.canonical[:])
	h.Write(permit.evidenceBoundary[:])
	h.Write(permit.selection.planDigest[:])
	for _, value := range runnerLedgerEntryExecutionAdmissionProfileIdentityStrings() {
		writeAdmissionString(h, value)
	}
	for _, value := range []string{
		string(permit.action),
		permit.generation.executionLineageDigest.String(), permit.generation.journalIdentityDigest.String(),
		permit.generation.runnerProjectionDecisionDigest.String(), permit.generation.schemaBundleDigest.String(),
		permit.consumerFactSubject.String(), permit.ledgerDigest.String(), permit.ledgerHead,
		permit.connectedAuthorityDigest.String(), permit.migrationAuthorityDigest.String(), permit.projectionSubject.String(),
		permit.catalogDigest.String(), strconv.FormatInt(permit.key, 10), permit.database.databaseName,
		permit.database.sessionUser, permit.database.currentUser, permit.selection.migrationID, permit.selection.entryDigest.String(),
	} {
		writeAdmissionString(h, value)
	}
	if permit.catalogContractDigest == nil {
		writeAdmissionString(h, "catalog-contract:absent")
	} else {
		writeAdmissionString(h, "catalog-contract:present")
		writeAdmissionString(h, permit.catalogContractDigest.String())
	}
	writeAdmissionUint(h, uint64(permit.ledgerLength))
	writeAdmissionUint(h, uint64(permit.database.postgresMajor))
	writeAdmissionUint(h, uint64(permit.database.serverVersionNum))
	writeAdmissionUint(h, uint64(permit.selection.entryIndex))
	writeAdmissionUint(h, uint64(permit.selection.planCount))
	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result
}

func closeRunnerLedgerEntryExecutionPermit(permit *runnerLedgerEntryExecutionPermit, primary error) error {
	if permit == nil {
		return primary
	}
	if permit.self != permit {
		return fail(CodeTransactionBoundary, "runner-ledger-entry-execution-admission-close", "execution permit copy cannot close database authority", nil)
	}
	registered, ok := runnerLedgerEntryExecutionPermitRegistry.LoadAndDelete(permit)
	record, recordOK := registered.(runnerLedgerEntryExecutionPermitRegistryRecord)
	if !ok || !recordOK || record.permit != permit || record.binding == nil || record.session == nil {
		return fail(CodeTransactionBoundary, "runner-ledger-entry-execution-admission-close", "execution permit is unavailable or already closed", nil)
	}
	valid := validRunnerLedgerEntryExecutionPermitWithRecord(permit, record)
	permit.closed = true
	permit.session = nil
	permit.evidenceBinder = nil
	permit.use = nil
	if !valid {
		primary = fail(CodeTransactionBoundary, "runner-ledger-entry-execution-admission-close", "execution permit changed before close", nil)
	}
	return closeRunnerDatabasePreflight(record.session, record.key, true, primary)
}

func validRunnerLedgerEntryExecutionPermitWithRecord(permit *runnerLedgerEntryExecutionPermit, record runnerLedgerEntryExecutionPermitRegistryRecord) bool {
	if permit == nil || record.binding == nil || record.permit != permit || record.binding != permit.binding || record.key != permit.key ||
		record.candidateBinding != permit.candidateBinding || record.use != permit.use || record.canonical != permit.canonical ||
		!sameRunnerOwnedPointer(record.session, permit.session) ||
		!sameRunnerOwnedPointer(record.evidenceBinder, permit.evidenceBinder) ||
		!sameRunnerOwnedPointer(record.binding.session, record.session) ||
		!sameRunnerOwnedPointer(record.binding.evidenceBinder, record.evidenceBinder) ||
		record.binding.use != record.use || record.binding.candidateBinding != record.candidateBinding {
		return false
	}
	if !validRunnerLedgerEntryExecutionAdmissionUse(permit.evidenceBinder, permit.use, permit.consumerFactSubject, permit.evidenceBoundary, true) {
		return false
	}
	// The registry record was removed before this helper. Validate every sealed
	// field directly, then cleanup the exact retained session regardless of the
	// validation result.
	return permit.self == permit && !permit.closed && permit.binding != nil && permit.binding.permit == permit &&
		permit.binding.key == permit.key && permit.binding.use == permit.use &&
		permit.binding.candidateBinding == permit.candidateBinding &&
		sameRunnerOwnedPointer(permit.binding.session, permit.session) &&
		sameRunnerOwnedPointer(permit.binding.evidenceBinder, permit.evidenceBinder) &&
		permit.binding.canonical == permit.canonical &&
		permit.canonical == runnerLedgerEntryExecutionPermitDigest(permit)
}
