package migration

import (
	"context"
	"crypto/sha256"
	"strconv"
	"sync"
)

const runnerLedgerEntryAdmissionPermitDigestDomain = "cloud-agents/runner-ledger-entry-admission/permit/v1"

type runnerLedgerEntryAdmissionSelection struct {
	entryIndex  uint32
	migrationID string
	entryDigest Digest
	planCount   uint32
	planDigest  [32]byte
}

// runnerLedgerEntryAdmissionPermit retains exactly one read-only database
// session and its signed advisory lock. Its only transition in this slice is
// closeRunnerLedgerEntryAdmissionPermit; no transaction or writer method is
// defined on this type.
type runnerLedgerEntryAdmissionPermit struct {
	self                     *runnerLedgerEntryAdmissionPermit
	binding                  *runnerLedgerEntryAdmissionPermitBinding
	session                  DatabaseSession
	evidenceBinder           runnerLedgerEntryAdmissionClaimBinder
	use                      *runnerLedgerEntryAdmissionUseRecord
	key                      int64
	candidateBinding         *verifiedEvidenceRunBinding
	generation               generationIdentity
	evidenceBoundary         [32]byte
	consumerFactSubject      Digest
	ledgerDigest             Digest
	ledgerHead               string
	ledgerLength             uint32
	connectedAuthorityDigest Digest
	migrationAuthorityDigest Digest
	projectionSubject        Digest
	catalogDigest            Digest
	database                 runnerPreparedDatabaseIdentity
	selection                runnerLedgerEntryAdmissionSelection
	canonical                [32]byte
	closed                   bool
}

type runnerLedgerEntryAdmissionPermitBinding struct {
	permit           *runnerLedgerEntryAdmissionPermit
	session          DatabaseSession
	evidenceBinder   runnerLedgerEntryAdmissionClaimBinder
	use              *runnerLedgerEntryAdmissionUseRecord
	key              int64
	candidateBinding *verifiedEvidenceRunBinding
	canonical        [32]byte
}

type runnerLedgerEntryAdmissionPermitRegistryRecord struct {
	permit           *runnerLedgerEntryAdmissionPermit
	binding          *runnerLedgerEntryAdmissionPermitBinding
	session          DatabaseSession
	evidenceBinder   runnerLedgerEntryAdmissionClaimBinder
	use              *runnerLedgerEntryAdmissionUseRecord
	key              int64
	candidateBinding *verifiedEvidenceRunBinding
	canonical        [32]byte
}

var runnerLedgerEntryAdmissionPermitRegistry sync.Map

func (runner *Runner) prepareRunnerLedgerEntryAdmission(ctx context.Context, dsn string, bundle *RuntimeBundle, plans []StatementPlan, evidence EvidenceSession, candidate OwnedCurrentCandidate, fact runnerLedgerConsumerFact) (*runnerLedgerEntryAdmissionPermit, error) {
	if runner == nil || ctx == nil || bundle == nil || bundle.Manifest == nil || !fact.valid() ||
		!generatedRunnerLedgerEntryAdmissionProfile.valid() {
		return nil, fail(CodeProjectionNotImplemented, "runner-ledger-entry-admission", "entry admission inputs are unavailable", nil)
	}
	if err := contextAdmissionError(ctx); err != nil {
		return nil, err
	}
	binder, ok := evidence.(runnerLedgerEntryAdmissionClaimBinder)
	if !ok || !runnerOwnedPointer(binder) || !validOwnedCurrentCandidate(candidate) {
		return nil, fail(CodeEvidenceRecoveryRequired, "runner-ledger-entry-admission", "same-verifier entry evidence binder is unavailable", nil)
	}
	claim, err := binder.bindRunnerLedgerEntryAdmissionClaim(ctx, runnerLedgerEntryAdmissionClaimRequest{fact: fact.clone(), candidate: candidate})
	if err != nil {
		return nil, err
	}
	defer revokeRunnerLedgerEntryAdmissionClaim(claim)

	observation, err := runner.openRunnerLockedLedgerCatalogObservation(ctx, dsn, bundle, plans, evidence, candidate)
	if err != nil {
		return nil, err
	}
	failClosed := func(primary error) (*runnerLedgerEntryAdmissionPermit, error) {
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
	if err := observation.revalidate(ctx, runner); err != nil {
		return failClosed(err)
	}
	boundary, err := binder.consumeRunnerLedgerEntryAdmissionClaim(ctx, claim, candidate)
	if err != nil {
		return failClosed(err)
	}
	if err := contextAdmissionError(ctx); err != nil {
		return failClosed(err)
	}
	permit, err := bindRunnerLedgerEntryAdmissionPermit(observation, binder, candidate, fact, boundary, selection)
	if err != nil {
		return failClosed(err)
	}
	return permit, nil
}

func validateRunnerLedgerEntryAdmissionObservation(fact runnerLedgerConsumerFact, projection *runnerLedgerCatalogPreflight, observation *runnerLockedLedgerCatalogObservation) (runnerLedgerEntryAdmissionSelection, error) {
	var selection runnerLedgerEntryAdmissionSelection
	if !fact.valid() || fact.action != runnerLedgerConsumerEntryNotImplemented || !validRunnerLedgerCatalogPreflight(projection) ||
		observation == nil || !observation.active() || observation.bundle == nil || fact.dispatch.fact.nextEntry == nil {
		return selection, fail(CodeEvidenceRecoveryRequired, "runner-ledger-entry-admission-bind", "entry observation is unavailable or changed", nil)
	}
	action, ok := generatedRunnerLedgerEntryAdmissionAction(
		fact.dispatch.fact.disposition, fact.dispatch.fact.recovery.State, fact.dispatch.fact.recovery.Action,
	)
	if !ok || action != runnerLedgerEntryAdmissionPrepare || fact.manifestDigest != observation.bundle.Manifest.ManifestDigest ||
		fact.dispatch.fact.schemaBundleDigest != projection.schemaBundleDigest ||
		fact.dispatch.fact.executionLineageDigest != projection.executionLineageDigest ||
		fact.dispatch.runnerProjectionDecisionDigest != projection.runnerProjectionDecisionDigest ||
		fact.dispatch.fact.orderedMigrationPrefixLength != uint32(len(projection.ledger.rows)) ||
		fact.dispatch.fact.orderedMigrationPrefixDigest != projection.ledger.digest ||
		!sameOptionalString(fact.dispatch.fact.orderedMigrationPrefixHead, cloneStringPointerIfNonEmpty(projection.ledger.head)) ||
		fact.dispatch.fact.lastAppliedCatalogContractDigest != projection.projectionSubjectDigest {
		return selection, fail(CodeEvidenceJournalCorrupt, "runner-ledger-entry-admission-bind", "fresh database observation differs from the consumed entry fact", nil)
	}
	switch fact.dispatch.fact.disposition {
	case runnerLedgerPreflightEmptyBrandNew:
		if projection.state != runnerLedgerCatalogEmpty {
			return selection, fail(CodeEvidenceJournalCorrupt, "runner-ledger-entry-admission-bind", "empty entry fact observed a non-empty database prefix", nil)
		}
	case runnerLedgerPreflightPartialNextEntry:
		if projection.state != runnerLedgerCatalogPartial {
			return selection, fail(CodeEvidenceJournalCorrupt, "runner-ledger-entry-admission-bind", "partial entry fact observed a non-partial database prefix", nil)
		}
	default:
		return selection, fail(CodeProjectionNotImplemented, "runner-ledger-entry-admission-bind", "consumer transition is not an entry admission", nil)
	}
	index := int(fact.dispatch.fact.orderedMigrationPrefixLength)
	entries := observation.bundle.Manifest.SchemaBundle.Migrations
	if index < 0 || index >= len(entries) || uint64(index) > uint64(^uint32(0)) {
		return selection, fail(CodeEvidenceJournalCorrupt, "runner-ledger-entry-admission-bind", "selected entry index is outside the signed schema bundle", nil)
	}
	entry := entries[index]
	row := commitIntentLedgerRow(entry, observation.bundle.Manifest.SchemaBundleDigest)
	canonical, err := canonicalContractKey(row)
	if err != nil || row.Validate() != nil || canonical == "" {
		return selection, fail(CodeUntrusted, "runner-ledger-entry-admission-bind", "selected signed entry cannot be reproduced", nil)
	}
	entryDigest := DigestBytes([]byte(runnerLedgerPreflightEntryDigestDomain + "\x00" + canonical))
	if fact.dispatch.fact.nextEntry.MigrationID != entry.ID || fact.dispatch.fact.nextEntry.EntryDigest != entryDigest {
		return selection, fail(CodeEvidenceJournalCorrupt, "runner-ledger-entry-admission-bind", "selected entry differs from the exact signed next entry", nil)
	}
	planDigest, planCount, err := runnerEntryPlanClosureDigest(observation.plans, entry.ID)
	if err != nil {
		return selection, err
	}
	selection = runnerLedgerEntryAdmissionSelection{
		entryIndex: uint32(index), migrationID: entry.ID, entryDigest: entryDigest,
		planCount: planCount, planDigest: planDigest,
	}
	return selection, nil
}

func bindRunnerLedgerEntryAdmissionPermit(observation *runnerLockedLedgerCatalogObservation, binder runnerLedgerEntryAdmissionClaimBinder, candidate OwnedCurrentCandidate, fact runnerLedgerConsumerFact, boundary runnerLedgerEntryAdmissionEvidenceBoundary, selection runnerLedgerEntryAdmissionSelection) (*runnerLedgerEntryAdmissionPermit, error) {
	if observation == nil || !observation.active() || binder == nil || !runnerOwnedPointer(binder) ||
		!validOwnedCurrentCandidate(candidate) || !fact.valid() || boundary.candidateBinding != candidate.binding ||
		boundary.canonical == ([32]byte{}) || boundary.canonical != runnerLedgerEntryAdmissionEvidenceBoundaryDigest(boundary) ||
		boundary.factSubject != fact.subjectDigest || boundary.generation.owner != candidate.owner ||
		boundary.generation.executionLineageDigest != observation.bindings.executionLineageDigest ||
		boundary.generation.schemaBundleDigest != observation.bindings.schemaBundleDigest ||
		boundary.generation.runnerProjectionDecisionDigest != observation.bindings.runnerProjectionDecisionDigest ||
		selection.planCount == 0 || selection.planDigest == ([32]byte{}) || selection.entryDigest.Validate() != nil ||
		!migrationIDPattern.MatchString(selection.migrationID) {
		return nil, fail(CodeEvidenceRecoveryRequired, "runner-ledger-entry-admission-permit", "entry admission authority is unavailable or changed", nil)
	}
	useValue, useOK := runnerLedgerEntryAdmissionUseByEvidenceBinder.Load(binder)
	use, useRecordOK := useValue.(*runnerLedgerEntryAdmissionUseRecord)
	if !useOK || !useRecordOK || !validRunnerLedgerEntryAdmissionUse(binder, use, fact.subjectDigest, boundary.canonical, true) {
		return nil, fail(CodeEvidenceRecoveryRequired, "runner-ledger-entry-admission-permit", "consumed evidence boundary is unavailable or changed", nil)
	}
	expectedKey, err := observation.bundle.Manifest.SchemaBundle.AdvisoryLock.Key()
	if err != nil || expectedKey != observation.key {
		return nil, fail(CodeUntrusted, "runner-ledger-entry-admission-permit", "signed advisory lock differs from the retained session", nil)
	}
	metadata := observation.migrationRole.Metadata.Snapshot
	catalogDigest := Digest("")
	if observation.initial != nil {
		catalogDigest = observation.initial.Digest
	} else if observation.cumulative != nil {
		catalogDigest = observation.cumulative.Digest
	}
	if catalogDigest.Validate() != nil || !sameRunnerDedicatedSessionIdentity(observation.connected.Metadata.Snapshot, metadata) {
		return nil, fail(CodeProjectionMetadataMismatch, "runner-ledger-entry-admission-permit", "retained database identity or catalog is unavailable", nil)
	}
	permit := &runnerLedgerEntryAdmissionPermit{
		session: observation.session, evidenceBinder: binder, use: use, key: observation.key, candidateBinding: candidate.binding,
		generation: boundary.generation, evidenceBoundary: boundary.canonical, consumerFactSubject: fact.subjectDigest,
		ledgerDigest: observation.ledger.digest, ledgerHead: observation.ledger.head, ledgerLength: uint32(len(observation.ledger.rows)),
		connectedAuthorityDigest: observation.connected.Digest, migrationAuthorityDigest: observation.migrationRole.Digest,
		projectionSubject: observation.projectionSubject, catalogDigest: catalogDigest,
		database: runnerPreparedDatabaseIdentity{
			postgresMajor: metadata.PostgresMajor, serverVersionNum: metadata.ServerVersionNum,
			databaseName: metadata.DatabaseName, sessionUser: metadata.SessionUser, currentUser: metadata.CurrentUser,
		},
		selection: selection,
	}
	permit.self = permit
	permit.binding = &runnerLedgerEntryAdmissionPermitBinding{
		permit: permit, session: observation.session, evidenceBinder: binder, use: use,
		key: observation.key, candidateBinding: candidate.binding,
	}
	permit.canonical = runnerLedgerEntryAdmissionPermitDigest(permit)
	permit.binding.canonical = permit.canonical
	if permit.canonical == ([32]byte{}) {
		return nil, fail(CodeTransactionBoundary, "runner-ledger-entry-admission-permit", "entry admission permit could not be identified", nil)
	}
	runnerLedgerEntryAdmissionPermitRegistry.Store(permit, runnerLedgerEntryAdmissionPermitRegistryRecord{
		permit: permit, binding: permit.binding, session: observation.session, evidenceBinder: binder, use: use,
		key: observation.key, candidateBinding: candidate.binding, canonical: permit.canonical,
	})
	if !validRunnerLedgerEntryAdmissionPermit(permit) {
		runnerLedgerEntryAdmissionPermitRegistry.Delete(permit)
		return nil, fail(CodeTransactionBoundary, "runner-ledger-entry-admission-permit", "entry admission permit could not be sealed", nil)
	}
	observation.transferred = true
	observation.session = nil
	return permit, nil
}

func validRunnerLedgerEntryAdmissionPermit(permit *runnerLedgerEntryAdmissionPermit) bool {
	if permit == nil || permit.self != permit || permit.closed || permit.binding == nil || permit.binding.permit != permit ||
		permit.session == nil || !sameRunnerOwnedPointer(permit.session, permit.binding.session) || permit.evidenceBinder == nil ||
		!sameRunnerOwnedPointer(permit.evidenceBinder, permit.binding.evidenceBinder) || permit.key != permit.binding.key ||
		permit.use == nil || permit.binding.use != permit.use ||
		permit.candidateBinding == nil || permit.binding.candidateBinding != permit.candidateBinding || permit.canonical == ([32]byte{}) ||
		permit.binding.canonical != permit.canonical || permit.canonical != runnerLedgerEntryAdmissionPermitDigest(permit) {
		return false
	}
	registered, ok := runnerLedgerEntryAdmissionPermitRegistry.Load(permit)
	record, recordOK := registered.(runnerLedgerEntryAdmissionPermitRegistryRecord)
	return ok && recordOK && validRunnerLedgerEntryAdmissionUse(permit.evidenceBinder, permit.use, permit.consumerFactSubject, permit.evidenceBoundary, true) &&
		record.permit == permit && record.binding == permit.binding && record.key == permit.key &&
		record.candidateBinding == permit.candidateBinding && record.use == permit.use && record.canonical == permit.canonical &&
		sameRunnerOwnedPointer(record.session, permit.session) && sameRunnerOwnedPointer(record.evidenceBinder, permit.evidenceBinder)
}

func runnerLedgerEntryAdmissionPermitDigest(permit *runnerLedgerEntryAdmissionPermit) [32]byte {
	if permit == nil || permit.self != permit || permit.closed || permit.session == nil || permit.evidenceBinder == nil || permit.use == nil ||
		permit.candidateBinding == nil || permit.generation.owner == nil || permit.generation.owner != permit.candidateBinding.owner ||
		permit.evidenceBoundary == ([32]byte{}) || permit.consumerFactSubject.Validate() != nil || permit.ledgerDigest.Validate() != nil ||
		permit.connectedAuthorityDigest.Validate() != nil || permit.migrationAuthorityDigest.Validate() != nil ||
		permit.projectionSubject.Validate() != nil || permit.catalogDigest.Validate() != nil || permit.database.postgresMajor == 0 ||
		permit.database.serverVersionNum == 0 || permit.database.databaseName == "" || permit.database.sessionUser == "" ||
		permit.database.currentUser != MigrationOwnerRole || permit.selection.entryDigest.Validate() != nil ||
		!migrationIDPattern.MatchString(permit.selection.migrationID) || permit.selection.planCount == 0 || permit.selection.planDigest == ([32]byte{}) {
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
	h.Write([]byte(runnerLedgerEntryAdmissionPermitDigestDomain + "\x00"))
	h.Write(permit.candidateBinding.canonical[:])
	h.Write(permit.evidenceBoundary[:])
	h.Write(permit.selection.planDigest[:])
	for _, value := range []string{
		generatedRunnerLedgerEntryAdmissionProfile.profileID, generatedRunnerLedgerEntryAdmissionProfile.profileDigest,
		runnerLedgerEntryAdmissionRegistryDigest, runnerLedgerEntryAdmissionStateMachineDigest, runnerLedgerEntryAdmissionPolicyDigest,
		permit.generation.executionLineageDigest.String(), permit.generation.journalIdentityDigest.String(),
		permit.generation.runnerProjectionDecisionDigest.String(), permit.generation.schemaBundleDigest.String(),
		permit.consumerFactSubject.String(), permit.ledgerDigest.String(), permit.ledgerHead,
		permit.connectedAuthorityDigest.String(), permit.migrationAuthorityDigest.String(), permit.projectionSubject.String(),
		permit.catalogDigest.String(), strconv.FormatInt(permit.key, 10), permit.database.databaseName,
		permit.database.sessionUser, permit.database.currentUser, permit.selection.migrationID, permit.selection.entryDigest.String(),
	} {
		writeAdmissionString(h, value)
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

func closeRunnerLedgerEntryAdmissionPermit(permit *runnerLedgerEntryAdmissionPermit, primary error) error {
	if permit == nil {
		return primary
	}
	if permit.self != permit {
		return fail(CodeTransactionBoundary, "runner-ledger-entry-admission-close", "entry admission permit copy cannot close database authority", nil)
	}
	registered, ok := runnerLedgerEntryAdmissionPermitRegistry.LoadAndDelete(permit)
	record, recordOK := registered.(runnerLedgerEntryAdmissionPermitRegistryRecord)
	if !ok || !recordOK || record.permit != permit || record.binding == nil || record.session == nil {
		return fail(CodeTransactionBoundary, "runner-ledger-entry-admission-close", "entry admission permit is unavailable or already closed", nil)
	}
	valid := validRunnerLedgerEntryAdmissionPermitWithRecord(permit, record)
	permit.closed = true
	permit.session = nil
	permit.evidenceBinder = nil
	permit.use = nil
	if !valid {
		primary = fail(CodeTransactionBoundary, "runner-ledger-entry-admission-close", "entry admission permit changed before close", nil)
	}
	return closeRunnerDatabasePreflight(record.session, record.key, true, primary)
}

func validRunnerLedgerEntryAdmissionPermitWithRecord(permit *runnerLedgerEntryAdmissionPermit, record runnerLedgerEntryAdmissionPermitRegistryRecord) bool {
	if permit == nil || record.permit != permit || record.binding != permit.binding || record.key != permit.key ||
		record.candidateBinding != permit.candidateBinding || record.use != permit.use || record.canonical != permit.canonical ||
		!sameRunnerOwnedPointer(record.session, permit.session) || !sameRunnerOwnedPointer(record.evidenceBinder, permit.evidenceBinder) {
		return false
	}
	if !validRunnerLedgerEntryAdmissionUse(permit.evidenceBinder, permit.use, permit.consumerFactSubject, permit.evidenceBoundary, true) {
		return false
	}
	// The permit record was removed before this helper, so validate all immutable shape
	// and canonical facts directly rather than querying the live registry again.
	return permit.self == permit && !permit.closed && permit.binding != nil && permit.binding.permit == permit &&
		permit.binding.key == permit.key && permit.binding.use == permit.use && permit.binding.candidateBinding == permit.candidateBinding &&
		permit.binding.canonical == permit.canonical && permit.canonical == runnerLedgerEntryAdmissionPermitDigest(permit)
}
