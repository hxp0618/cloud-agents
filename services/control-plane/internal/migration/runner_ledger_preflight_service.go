package migration

import (
	"context"
	"crypto/sha256"
	"sync"
	"sync/atomic"
)

const (
	runnerLedgerPreflightEntryDigestDomain    = "cloud-agents/runner-ledger-preflight/entry/v1"
	runnerLedgerPreflightDispatchDigestDomain = "cloud-agents/runner-ledger-preflight/dispatch/v1"
	runnerLedgerPreflightClaimDigestDomain    = "cloud-agents/runner-ledger-preflight/claim/v1"
)

type runnerLedgerPreflightDispatchKind string

const (
	runnerLedgerPreflightDispatchEntry         runnerLedgerPreflightDispatchKind = "entry"
	runnerLedgerPreflightDispatchRecovery      runnerLedgerPreflightDispatchKind = "recovery"
	runnerLedgerPreflightDispatchReturnSuccess runnerLedgerPreflightDispatchKind = "return_success"
)

// runnerLedgerPreflightDispatch is an ordinary closed result. It contains
// exact identities and disposition facts, but no session, transaction,
// evidence lease, receipt, verifier artifact, writer token, or mutation port.
type runnerLedgerPreflightDispatch struct {
	kind                           runnerLedgerPreflightDispatchKind
	fact                           runnerLedgerPreflightFact
	journalIdentityDigest          Digest
	runnerProjectionDecisionDigest Digest
	recoverySnapshotDigest         Digest
	recoveryMigrationID            *string
	recoveryAttemptIndex           *uint32
	recoveryTailDigest             Digest
	subjectDigest                  Digest
}

type runnerLedgerPreflightDispatchWire struct {
	Kind                           runnerLedgerPreflightDispatchKind `json:"kind"`
	Fact                           runnerLedgerPreflightFactWire     `json:"fact"`
	FactSubjectDigest              Digest                            `json:"fact_subject_digest"`
	JournalIdentityDigest          Digest                            `json:"journal_identity_digest"`
	RunnerProjectionDecisionDigest Digest                            `json:"runner_projection_decision_digest"`
	RecoverySnapshotDigest         Digest                            `json:"recovery_snapshot_digest"`
	RecoveryMigrationID            *string                           `json:"recovery_migration_id"`
	RecoveryAttemptIndex           *uint32                           `json:"recovery_attempt_index"`
	RecoveryTailDigest             Digest                            `json:"recovery_tail_digest"`
}

// runnerLedgerPreflightClaimBinder is deliberately sealed inside migration.
// The production implementation lives on generationEvidenceSession, where it
// can revalidate the current candidate, generation, journal schema, and
// recovery snapshot under the existing session -> journal lock order.
type runnerLedgerPreflightClaimBinder interface {
	bindRunnerLedgerPreflightClaim(context.Context, runnerLedgerPreflightClaimRequest) (*runnerLedgerPreflightClaim, error)
	consumeRunnerLedgerPreflightClaim(context.Context, *runnerLedgerPreflightClaim, OwnedCurrentCandidate) (runnerLedgerPreflightDispatch, error)
	runnerLedgerPreflightClaimBinderSealed()
}

type runnerLedgerPreflightClaimRequest struct {
	projection *runnerLedgerCatalogPreflight
	candidate  OwnedCurrentCandidate
}

type runnerLedgerPreflightEvidenceFacts struct {
	binder           runnerLedgerPreflightClaimBinder
	candidateBinding *verifiedEvidenceRunBinding
	generation       generationIdentity
	schema           verifiedRecoverySchemaWitness
	recovery         *RecoverySnapshot
	schemaDigest     [32]byte
	recoveryDigest   [32]byte
	sessionDigest    [32]byte
	journalDigest    [32]byte
}

// runnerLedgerPreflightClaim is the one-shot authority between the read-only
// service and the closed dispatch. Ordinary copies and literals fail the self,
// binding, registry, and one-live-claim checks.
type runnerLedgerPreflightClaim struct {
	self                    *runnerLedgerPreflightClaim
	binding                 *runnerLedgerPreflightClaimBinding
	candidateBinding        *verifiedEvidenceRunBinding
	generation              generationIdentity
	projectionSubjectDigest Digest
	schemaDigest            [32]byte
	recoveryDigest          [32]byte
	sessionDigest           [32]byte
	journalDigest           [32]byte
	dispatch                runnerLedgerPreflightDispatch
	consumed                *atomic.Bool
	canonical               [32]byte
}

type runnerLedgerPreflightClaimBinding struct {
	claim            *runnerLedgerPreflightClaim
	binder           runnerLedgerPreflightClaimBinder
	candidateBinding *verifiedEvidenceRunBinding
	consumed         *atomic.Bool
	canonical        [32]byte
}

type runnerLedgerPreflightClaimRegistryRecord struct {
	claim            *runnerLedgerPreflightClaim
	binding          *runnerLedgerPreflightClaimBinding
	binder           runnerLedgerPreflightClaimBinder
	candidateBinding *verifiedEvidenceRunBinding
	consumed         *atomic.Bool
	canonical        [32]byte
}

var (
	runnerLedgerPreflightClaimRegistry         sync.Map
	runnerLedgerPreflightClaimByEvidenceBinder sync.Map
)

// prepareRunnerLedgerPreflightClaim is the sole reviewed production caller of
// the read-only kernel. Runner.Run reaches it only through the closed generated
// consumer service; it does not enter the migration writer or mutate evidence.
func (runner *Runner) prepareRunnerLedgerPreflightClaim(ctx context.Context, dsn string, bundle *RuntimeBundle, plans []StatementPlan, evidence EvidenceSession, candidate OwnedCurrentCandidate) (*runnerLedgerPreflightClaim, error) {
	projection, err := runner.projectRunnerLedgerCatalogPreflight(ctx, dsn, bundle, plans, evidence, candidate)
	if err != nil {
		return nil, err
	}
	binder, ok := evidence.(runnerLedgerPreflightClaimBinder)
	if !ok || !runnerOwnedPointer(binder) {
		return nil, fail(CodeEvidenceRecoveryRequired, "runner-ledger-preflight-service", "same-verifier evidence binder is unavailable", nil)
	}
	claim, err := binder.bindRunnerLedgerPreflightClaim(ctx, runnerLedgerPreflightClaimRequest{projection: projection, candidate: candidate})
	if err != nil {
		return nil, err
	}
	if !validRunnerLedgerPreflightClaim(claim, binder, candidate.binding) {
		revokeRunnerLedgerPreflightClaim(claim)
		return nil, fail(CodeEvidenceRecoveryRequired, "runner-ledger-preflight-service", "typed preflight claim could not be sealed", nil)
	}
	return claim, nil
}

// claimRunnerLedgerPreflightDispatch consumes one exact claim only after the
// evidence implementation revalidates the live boundary. The returned value is
// ordinary data and cannot authorize a writer by itself.
func (runner *Runner) claimRunnerLedgerPreflightDispatch(ctx context.Context, evidence EvidenceSession, candidate OwnedCurrentCandidate, claim *runnerLedgerPreflightClaim) (runnerLedgerPreflightDispatch, error) {
	if runner == nil {
		return runnerLedgerPreflightDispatch{}, fail(CodeEvidenceRecoveryRequired, "runner-ledger-preflight-claim", "runner service is unavailable", nil)
	}
	if err := contextAdmissionError(ctx); err != nil {
		return runnerLedgerPreflightDispatch{}, err
	}
	binder, ok := evidence.(runnerLedgerPreflightClaimBinder)
	if !ok || !runnerOwnedPointer(binder) || !validOwnedCurrentCandidate(candidate) {
		return runnerLedgerPreflightDispatch{}, fail(CodeEvidenceRecoveryRequired, "runner-ledger-preflight-claim", "claim consumer authority is unavailable", nil)
	}
	return binder.consumeRunnerLedgerPreflightClaim(ctx, claim, candidate)
}

func bindRunnerLedgerPreflightClaimFromEvidence(ctx context.Context, request runnerLedgerPreflightClaimRequest, facts runnerLedgerPreflightEvidenceFacts) (*runnerLedgerPreflightClaim, error) {
	if err := contextAdmissionError(ctx); err != nil {
		return nil, err
	}
	if !validOwnedCurrentCandidate(request.candidate) || !validRunnerLedgerPreflightEvidenceFacts(facts, request.candidate.binding) {
		return nil, fail(CodeEvidenceRecoveryRequired, "runner-ledger-preflight-bind", "same-verifier evidence facts are unavailable", nil)
	}
	dispatch, err := buildRunnerLedgerPreflightDispatch(request.projection, facts)
	if err != nil {
		return nil, err
	}
	claim := &runnerLedgerPreflightClaim{
		candidateBinding: request.candidate.binding, generation: facts.generation,
		projectionSubjectDigest: request.projection.subjectDigest,
		schemaDigest:            facts.schemaDigest, recoveryDigest: facts.recoveryDigest,
		sessionDigest: facts.sessionDigest, journalDigest: facts.journalDigest,
		dispatch: dispatch.clone(), consumed: &atomic.Bool{},
	}
	claim.self = claim
	claim.binding = &runnerLedgerPreflightClaimBinding{
		claim: claim, binder: facts.binder, candidateBinding: request.candidate.binding, consumed: claim.consumed,
	}
	claim.canonical = runnerLedgerPreflightClaimDigest(claim)
	claim.binding.canonical = claim.canonical
	if claim.canonical == ([32]byte{}) {
		return nil, fail(CodeEvidenceRecoveryRequired, "runner-ledger-preflight-bind", "typed preflight claim could not be identified", nil)
	}
	runnerLedgerPreflightClaimRegistry.Store(claim, runnerLedgerPreflightClaimRegistryRecord{
		claim: claim, binding: claim.binding, binder: facts.binder, candidateBinding: request.candidate.binding,
		consumed: claim.consumed, canonical: claim.canonical,
	})
	if previous, loaded := runnerLedgerPreflightClaimByEvidenceBinder.LoadOrStore(facts.binder, claim); loaded {
		runnerLedgerPreflightClaimRegistry.Delete(claim)
		_ = previous
		return nil, fail(CodeEvidenceRecoveryRequired, "runner-ledger-preflight-bind", "an unconsumed preflight claim already exists", nil)
	}
	if !validRunnerLedgerPreflightClaim(claim, facts.binder, request.candidate.binding) {
		revokeRunnerLedgerPreflightClaim(claim)
		return nil, fail(CodeEvidenceRecoveryRequired, "runner-ledger-preflight-bind", "typed preflight claim could not be sealed", nil)
	}
	if err := contextAdmissionError(ctx); err != nil {
		revokeRunnerLedgerPreflightClaim(claim)
		return nil, err
	}
	return claim, nil
}

func consumeRunnerLedgerPreflightClaimFromEvidence(ctx context.Context, claim *runnerLedgerPreflightClaim, candidate OwnedCurrentCandidate, facts runnerLedgerPreflightEvidenceFacts) (runnerLedgerPreflightDispatch, error) {
	if err := contextAdmissionError(ctx); err != nil {
		return runnerLedgerPreflightDispatch{}, err
	}
	if !validOwnedCurrentCandidate(candidate) || !validRunnerLedgerPreflightEvidenceFacts(facts, candidate.binding) {
		return runnerLedgerPreflightDispatch{}, fail(CodeEvidenceRecoveryRequired, "runner-ledger-preflight-claim", "typed preflight claim is unavailable or changed", nil)
	}
	if !validRunnerLedgerPreflightClaim(claim, facts.binder, candidate.binding) {
		// A copy or literal cannot revoke the original one-shot claim. An exact
		// registered claim whose owned fields drifted is terminally unusable and
		// is revoked so it cannot pin the evidence binder indefinitely.
		if claim != nil && claim.self == claim {
			revokeRunnerLedgerPreflightClaim(claim)
		}
		return runnerLedgerPreflightDispatch{}, fail(CodeEvidenceRecoveryRequired, "runner-ledger-preflight-claim", "typed preflight claim is unavailable or changed", nil)
	}
	if !sameGenerationIdentity(claim.generation, facts.generation) || claim.schemaDigest != facts.schemaDigest ||
		claim.recoveryDigest != facts.recoveryDigest || claim.sessionDigest != facts.sessionDigest ||
		claim.journalDigest != facts.journalDigest {
		revokeRunnerLedgerPreflightClaim(claim)
		return runnerLedgerPreflightDispatch{}, fail(CodeEvidenceRecoveryRequired, "runner-ledger-preflight-claim", "evidence changed after claim minting", nil)
	}
	if !claim.consumed.CompareAndSwap(false, true) {
		revokeRunnerLedgerPreflightClaim(claim)
		return runnerLedgerPreflightDispatch{}, fail(CodeEvidenceRecoveryRequired, "runner-ledger-preflight-claim", "typed preflight claim was already consumed", nil)
	}
	runnerLedgerPreflightClaimRegistry.Delete(claim)
	runnerLedgerPreflightClaimByEvidenceBinder.CompareAndDelete(facts.binder, claim)
	dispatch := claim.dispatch.clone()
	if !dispatch.valid() {
		return runnerLedgerPreflightDispatch{}, fail(CodeEvidenceRecoveryRequired, "runner-ledger-preflight-claim", "closed dispatch changed before consumption", nil)
	}
	return dispatch, nil
}

func buildRunnerLedgerPreflightDispatch(projection *runnerLedgerCatalogPreflight, facts runnerLedgerPreflightEvidenceFacts) (runnerLedgerPreflightDispatch, error) {
	if !validRunnerLedgerCatalogPreflight(projection) || !validRunnerLedgerPreflightEvidenceFacts(facts, facts.candidateBinding) ||
		projection.schemaBundleDigest != facts.generation.schemaBundleDigest ||
		projection.executionLineageDigest != facts.generation.executionLineageDigest ||
		projection.runnerProjectionDecisionDigest != facts.generation.runnerProjectionDecisionDigest ||
		uint64(projection.migrationCount) != uint64(len(facts.schema.signedExpectedLedgerRows)) ||
		len(facts.schema.orderedMigrations) != len(facts.schema.signedExpectedLedgerRows) {
		return runnerLedgerPreflightDispatch{}, fail(CodeEvidenceRecoveryRequired, "runner-ledger-preflight-bind", "projection and evidence describe different verified runtime identities", nil)
	}
	if len(projection.ledger.rows) != len(facts.schema.durableObservedLedgerPrefix) ||
		projection.ledger.digest != facts.schema.durableObservedLedgerDigest {
		return runnerLedgerPreflightDispatch{}, fail(CodeEvidenceJournalCorrupt, "runner-ledger-preflight-bind", "database and evidence ledger prefixes differ", nil)
	}
	for index := range projection.ledger.rows {
		if !canonicalEqual(projection.ledger.rows[index], facts.schema.durableObservedLedgerPrefix[index]) {
			return runnerLedgerPreflightDispatch{}, fail(CodeEvidenceJournalCorrupt, "runner-ledger-preflight-bind", "database and evidence ledger rows differ", nil)
		}
	}
	for index, migration := range facts.schema.orderedMigrations {
		if facts.schema.signedExpectedLedgerRows[index].MigrationID != migration {
			return runnerLedgerPreflightDispatch{}, fail(CodeEvidenceJournalCorrupt, "runner-ledger-preflight-bind", "same-verifier migration order is inconsistent", nil)
		}
	}
	recovery := facts.recovery
	if recovery == nil || recovery.tailDigest.Validate() != nil || facts.recoveryDigest == ([32]byte{}) {
		return runnerLedgerPreflightDispatch{}, fail(CodeEvidenceRecoveryRequired, "runner-ledger-preflight-bind", "recovery snapshot is unavailable", nil)
	}
	recoveryPair := &runnerLedgerPreflightRecoveryDisposition{State: recovery.state, Action: recovery.nextPermittedAction}
	var disposition runnerLedgerPreflightDisposition
	var nextEntry *runnerLedgerPreflightNextEntry
	var kind runnerLedgerPreflightDispatchKind
	switch projection.state {
	case runnerLedgerCatalogEmpty:
		disposition, kind = runnerLedgerPreflightEmptyBrandNew, runnerLedgerPreflightDispatchEntry
		entry, err := runnerLedgerPreflightNextEntryFromSchema(facts.schema, 0)
		if err != nil {
			return runnerLedgerPreflightDispatch{}, err
		}
		nextEntry = &entry
	case runnerLedgerCatalogPartial:
		if generatedRunnerLedgerPreflightRecoveryPairAllowed(runnerLedgerPreflightPartialNextEntry, recovery.state, recovery.nextPermittedAction) {
			disposition, kind = runnerLedgerPreflightPartialNextEntry, runnerLedgerPreflightDispatchEntry
			entry, err := runnerLedgerPreflightNextEntryFromSchema(facts.schema, len(projection.ledger.rows))
			if err != nil {
				return runnerLedgerPreflightDispatch{}, err
			}
			nextEntry = &entry
		} else {
			disposition, kind = runnerLedgerPreflightPartialRetryOrRecovery, runnerLedgerPreflightDispatchRecovery
		}
	case runnerLedgerCatalogComplete:
		disposition, kind = runnerLedgerPreflightCompleteReturnSuccess, runnerLedgerPreflightDispatchReturnSuccess
	default:
		return runnerLedgerPreflightDispatch{}, fail(CodeEvidenceRecoveryRequired, "runner-ledger-preflight-bind", "ledger disposition is unclassified", nil)
	}
	if !generatedRunnerLedgerPreflightRecoveryPairAllowed(disposition, recovery.state, recovery.nextPermittedAction) ||
		!runnerLedgerPreflightRecoveryIdentityMatches(disposition, projection, facts.schema, recovery) {
		return runnerLedgerPreflightDispatch{}, fail(CodeEvidenceRecoveryRequired, "runner-ledger-preflight-bind", "ledger and recovery dispositions do not form an approved transition", nil)
	}
	if disposition == runnerLedgerPreflightCompleteReturnSuccess {
		if err := validateRunnerLedgerPreflightFinalCatalog(projection, facts.schema.finalCatalogDigest); err != nil {
			return runnerLedgerPreflightDispatch{}, err
		}
	}
	fact, err := bindRunnerLedgerPreflightFact(generatedRunnerLedgerPreflightProfile, disposition, runnerLedgerPreflightFactInput{
		SchemaBundleDigest: projection.schemaBundleDigest, ExecutionLineageDigest: projection.executionLineageDigest,
		OrderedMigrationPrefixDigest: projection.ledger.digest, OrderedMigrationPrefixLength: uint32(len(projection.ledger.rows)),
		OrderedMigrationPrefixHead:       cloneStringPointerIfNonEmpty(projection.ledger.head),
		LastAppliedCatalogContractDigest: projection.projectionSubjectDigest,
		NextEntry:                        nextEntry, Recovery: recoveryPair,
	})
	if err != nil {
		return runnerLedgerPreflightDispatch{}, err
	}
	dispatch := runnerLedgerPreflightDispatch{
		kind: kind, fact: fact.clone(), journalIdentityDigest: facts.generation.journalIdentityDigest,
		runnerProjectionDecisionDigest: facts.generation.runnerProjectionDecisionDigest,
		recoverySnapshotDigest:         digestString(facts.recoveryDigest),
		recoveryMigrationID:            cloneStringPointer(recovery.migrationID), recoveryAttemptIndex: cloneUint32Pointer(recovery.attemptIndex),
		recoveryTailDigest: recovery.tailDigest,
	}
	dispatch.subjectDigest = runnerLedgerPreflightDispatchSubjectDigest(dispatch)
	if !dispatch.valid() {
		return runnerLedgerPreflightDispatch{}, fail(CodeEvidenceRecoveryRequired, "runner-ledger-preflight-bind", "closed dispatch could not be sealed", nil)
	}
	return dispatch, nil
}

func runnerLedgerPreflightRecoveryIdentityMatches(disposition runnerLedgerPreflightDisposition, projection *runnerLedgerCatalogPreflight, schema verifiedRecoverySchemaWitness, recovery *RecoverySnapshot) bool {
	if projection == nil || recovery == nil || len(schema.signedExpectedLedgerRows) == 0 {
		return false
	}
	if recovery.attemptIndex != nil && *recovery.attemptIndex == 0 {
		return false
	}
	switch disposition {
	case runnerLedgerPreflightEmptyBrandNew:
		first := schema.signedExpectedLedgerRows[0].MigrationID
		switch recovery.state {
		case RecoveryBrandNew:
			return recovery.migrationID == nil && recovery.attemptIndex == nil
		case RecoveryBrandNewInherited:
			switch recovery.nextPermittedAction {
			case RecoveryBeginFirstAttempt:
				return runnerLedgerPreflightRecoveryIdentityAbsent(recovery)
			case RecoveryBeginNextAttempt:
				return runnerLedgerPreflightRecoveryIdentityIs(recovery, first, 2) &&
					recovery.previousAttemptTerminalDigest != nil && recovery.previousAttemptTerminalDigest.Validate() == nil
			default:
				return false
			}
		default:
			return false
		}
	case runnerLedgerPreflightPartialNextEntry:
		next := schema.signedExpectedLedgerRows[len(projection.ledger.rows)].MigrationID
		if recovery.state == RecoveryBrandNewInherited {
			return runnerLedgerPreflightRecoveryIdentityIs(recovery, next, 1) && *recovery.attemptIndex == 1
		}
		return recovery.state == RecoveryTerminal && runnerLedgerPreflightRecoveryIdentityIs(recovery, projection.ledger.head, 1)
	case runnerLedgerPreflightPartialRetryOrRecovery:
		nextIndex := len(projection.ledger.rows)
		if nextIndex >= len(schema.signedExpectedLedgerRows) {
			return false
		}
		if recovery.state == RecoveryBrandNewInherited && recovery.nextPermittedAction == RecoveryBeginFirstAttempt {
			return runnerLedgerPreflightRecoveryIdentityAbsent(recovery)
		}
		minimumAttempt := uint32(1)
		if recovery.state == RecoveryBrandNewInherited && recovery.nextPermittedAction == RecoveryBeginNextAttempt {
			minimumAttempt = 2
			if recovery.previousAttemptTerminalDigest == nil || recovery.previousAttemptTerminalDigest.Validate() != nil {
				return false
			}
		}
		return runnerLedgerPreflightRecoveryIdentityIs(recovery, schema.signedExpectedLedgerRows[nextIndex].MigrationID, minimumAttempt)
	case runnerLedgerPreflightCompleteReturnSuccess:
		return recovery.state == RecoveryCompleted && recovery.nextPermittedAction == RecoveryReturnSuccess &&
			runnerLedgerPreflightRecoveryIdentityIs(recovery, projection.ledger.head, 1)
	default:
		return false
	}
}

func runnerLedgerPreflightRecoveryIdentityAbsent(recovery *RecoverySnapshot) bool {
	return recovery != nil && recovery.migrationID == nil && recovery.attemptIndex == nil
}

func runnerLedgerPreflightRecoveryIdentityIs(recovery *RecoverySnapshot, migration string, minimumAttempt uint32) bool {
	return recovery != nil && migrationIDPattern.MatchString(migration) && recovery.migrationID != nil &&
		*recovery.migrationID == migration && recovery.attemptIndex != nil && *recovery.attemptIndex >= minimumAttempt
}

func runnerLedgerPreflightNextEntryFromSchema(schema verifiedRecoverySchemaWitness, index int) (runnerLedgerPreflightNextEntry, error) {
	if index < 0 || index >= len(schema.signedExpectedLedgerRows) || index >= len(schema.orderedMigrations) {
		return runnerLedgerPreflightNextEntry{}, fail(CodeEvidenceJournalCorrupt, "runner-ledger-preflight-entry", "signed next entry is unavailable", nil)
	}
	row := schema.signedExpectedLedgerRows[index]
	if row.Validate() != nil || row.MigrationID != schema.orderedMigrations[index] {
		return runnerLedgerPreflightNextEntry{}, fail(CodeEvidenceJournalCorrupt, "runner-ledger-preflight-entry", "signed next entry is invalid", nil)
	}
	canonical, err := canonicalContractKey(row)
	if err != nil || canonical == "" {
		return runnerLedgerPreflightNextEntry{}, fail(CodeEvidenceJournalCorrupt, "runner-ledger-preflight-entry", "signed next entry is not canonical", nil)
	}
	entry := runnerLedgerPreflightNextEntry{
		MigrationID: row.MigrationID,
		EntryDigest: DigestBytes([]byte(runnerLedgerPreflightEntryDigestDomain + "\x00" + canonical)),
	}
	if !validRunnerLedgerPreflightNextEntry(&entry) {
		return runnerLedgerPreflightNextEntry{}, fail(CodeEvidenceJournalCorrupt, "runner-ledger-preflight-entry", "signed next entry could not be identified", nil)
	}
	return entry, nil
}

func validateRunnerLedgerPreflightFinalCatalog(projection *runnerLedgerCatalogPreflight, expected Digest) error {
	if projection == nil || projection.state != runnerLedgerCatalogComplete || projection.cumulativeCatalog == nil ||
		projection.cumulativeCatalog.Metadata.Scope == nil || expected.Validate() != nil {
		return fail(CodeEvidenceRecoveryRequired, "runner-ledger-preflight-catalog", "complete catalog evidence is unavailable", nil)
	}
	state := CatalogStateProjection{Present: &SchemaPresentProjection{
		State: "schema_present", Scope: cloneProjectionValue(*projection.cumulativeCatalog.Metadata.Scope),
		Body: cloneProjectionValue(projection.cumulativeCatalog.Projection.Body),
	}}
	digest, err := state.ComputeDigest()
	if err != nil || digest != expected {
		return fail(CodeEvidenceJournalCorrupt, "runner-ledger-preflight-catalog", "database catalog and evidence final catalog differ", nil)
	}
	return nil
}

func validRunnerLedgerPreflightEvidenceFacts(facts runnerLedgerPreflightEvidenceFacts, candidateBinding *verifiedEvidenceRunBinding) bool {
	return facts.binder != nil && runnerOwnedPointer(facts.binder) && candidateBinding != nil && facts.candidateBinding == candidateBinding &&
		facts.generation.owner != nil && facts.generation.owner == candidateBinding.owner &&
		facts.schemaDigest != ([32]byte{}) && facts.schemaDigest == generationJournalSchemaDigest(facts.schema, facts.generation) &&
		facts.recovery != nil && validRecoverySnapshotForJournal(facts.recovery, facts.generation, facts.recovery.cursor) &&
		facts.recoveryDigest != ([32]byte{}) && facts.recoveryDigest == generationJournalRecoveryDigest(facts.recovery) &&
		facts.sessionDigest != ([32]byte{}) && facts.journalDigest != ([32]byte{})
}

func (dispatch runnerLedgerPreflightDispatch) valid() bool {
	if !dispatch.fact.valid() || dispatch.journalIdentityDigest.Validate() != nil || dispatch.runnerProjectionDecisionDigest.Validate() != nil ||
		dispatch.recoverySnapshotDigest.Validate() != nil || dispatch.recoveryTailDigest.Validate() != nil ||
		dispatch.subjectDigest.Validate() != nil || dispatch.subjectDigest != runnerLedgerPreflightDispatchSubjectDigest(dispatch) ||
		dispatch.fact.recovery == nil || dispatch.fact.recovery.State == "" || dispatch.fact.recovery.Action == "" ||
		!runnerLedgerPreflightDispatchRecoveryIdentityShape(dispatch) {
		return false
	}
	switch dispatch.kind {
	case runnerLedgerPreflightDispatchEntry:
		return (dispatch.fact.disposition == runnerLedgerPreflightEmptyBrandNew || dispatch.fact.disposition == runnerLedgerPreflightPartialNextEntry) && dispatch.fact.nextEntry != nil
	case runnerLedgerPreflightDispatchRecovery:
		return dispatch.fact.disposition == runnerLedgerPreflightPartialRetryOrRecovery && dispatch.fact.nextEntry == nil
	case runnerLedgerPreflightDispatchReturnSuccess:
		return dispatch.fact.disposition == runnerLedgerPreflightCompleteReturnSuccess && dispatch.fact.nextEntry == nil &&
			dispatch.recoveryMigrationID != nil && dispatch.recoveryAttemptIndex != nil
	default:
		return false
	}
}

func runnerLedgerPreflightDispatchRecoveryIdentityShape(dispatch runnerLedgerPreflightDispatch) bool {
	recovery := dispatch.fact.recovery
	if recovery == nil {
		return false
	}
	absent := dispatch.recoveryMigrationID == nil && dispatch.recoveryAttemptIndex == nil
	present := dispatch.recoveryMigrationID != nil && migrationIDPattern.MatchString(*dispatch.recoveryMigrationID) &&
		dispatch.recoveryAttemptIndex != nil && *dispatch.recoveryAttemptIndex > 0
	if !absent && !present {
		return false
	}
	if recovery.State == RecoveryBrandNewInherited && recovery.Action == RecoveryBeginFirstAttempt {
		return absent && (dispatch.fact.disposition == runnerLedgerPreflightEmptyBrandNew ||
			dispatch.fact.disposition == runnerLedgerPreflightPartialRetryOrRecovery)
	}
	if recovery.State == RecoveryBrandNew && recovery.Action == RecoveryBeginFirstAttempt {
		return absent && dispatch.fact.disposition == runnerLedgerPreflightEmptyBrandNew
	}
	if recovery.State == RecoveryBrandNewInherited && recovery.Action == RecoveryBeginNextAttempt {
		return present && *dispatch.recoveryAttemptIndex >= 2
	}
	return present
}

func (dispatch runnerLedgerPreflightDispatch) clone() runnerLedgerPreflightDispatch {
	copy := dispatch
	copy.fact = dispatch.fact.clone()
	copy.recoveryMigrationID = cloneStringPointer(dispatch.recoveryMigrationID)
	copy.recoveryAttemptIndex = cloneUint32Pointer(dispatch.recoveryAttemptIndex)
	return copy
}

func (dispatch runnerLedgerPreflightDispatch) wire() runnerLedgerPreflightDispatchWire {
	return runnerLedgerPreflightDispatchWire{
		Kind: dispatch.kind, Fact: dispatch.fact.wire(), FactSubjectDigest: dispatch.fact.subjectDigest,
		JournalIdentityDigest: dispatch.journalIdentityDigest, RunnerProjectionDecisionDigest: dispatch.runnerProjectionDecisionDigest,
		RecoverySnapshotDigest: dispatch.recoverySnapshotDigest, RecoveryMigrationID: cloneStringPointer(dispatch.recoveryMigrationID),
		RecoveryAttemptIndex: cloneUint32Pointer(dispatch.recoveryAttemptIndex), RecoveryTailDigest: dispatch.recoveryTailDigest,
	}
}

func runnerLedgerPreflightDispatchSubjectDigest(dispatch runnerLedgerPreflightDispatch) Digest {
	canonical, err := canonicalContractKey(dispatch.wire())
	if err != nil || canonical == "" {
		return ""
	}
	return DigestBytes([]byte(runnerLedgerPreflightDispatchDigestDomain + "\x00" + canonical))
}

func runnerLedgerPreflightClaimDigest(claim *runnerLedgerPreflightClaim) [32]byte {
	if claim == nil || claim.self != claim || claim.binding == nil || claim.candidateBinding == nil || claim.consumed == nil ||
		!claim.dispatch.valid() || claim.projectionSubjectDigest.Validate() != nil || claim.schemaDigest == ([32]byte{}) ||
		claim.recoveryDigest == ([32]byte{}) || claim.sessionDigest == ([32]byte{}) || claim.journalDigest == ([32]byte{}) ||
		claim.generation.owner == nil || claim.generation.owner != claim.candidateBinding.owner {
		return [32]byte{}
	}
	h := sha256.New()
	h.Write([]byte(runnerLedgerPreflightClaimDigestDomain + "\x00"))
	h.Write(claim.candidateBinding.canonical[:])
	for _, value := range []Digest{
		claim.generation.executionLineageDigest, claim.generation.journalIdentityDigest,
		claim.generation.runnerProjectionDecisionDigest, claim.generation.schemaBundleDigest,
		claim.projectionSubjectDigest, claim.dispatch.subjectDigest,
	} {
		if value.Validate() != nil {
			return [32]byte{}
		}
		writeAdmissionString(h, value.String())
	}
	h.Write(claim.schemaDigest[:])
	h.Write(claim.recoveryDigest[:])
	h.Write(claim.sessionDigest[:])
	h.Write(claim.journalDigest[:])
	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result
}

func validRunnerLedgerPreflightClaim(claim *runnerLedgerPreflightClaim, binder runnerLedgerPreflightClaimBinder, candidateBinding *verifiedEvidenceRunBinding) bool {
	if claim == nil || claim.self != claim || claim.binding == nil || claim.binding.claim != claim || claim.binding.binder == nil ||
		!sameRunnerOwnedPointer(claim.binding.binder, binder) || claim.binding.candidateBinding != candidateBinding ||
		claim.binding.consumed == nil || claim.binding.consumed != claim.consumed || claim.consumed.Load() ||
		claim.candidateBinding != candidateBinding || claim.canonical == ([32]byte{}) || claim.binding.canonical != claim.canonical ||
		claim.canonical != runnerLedgerPreflightClaimDigest(claim) {
		return false
	}
	registered, ok := runnerLedgerPreflightClaimRegistry.Load(claim)
	record, recordOK := registered.(runnerLedgerPreflightClaimRegistryRecord)
	current, currentOK := runnerLedgerPreflightClaimByEvidenceBinder.Load(binder)
	return ok && recordOK && currentOK && current == claim && record.claim == claim && record.binding == claim.binding &&
		sameRunnerOwnedPointer(record.binder, binder) && record.candidateBinding == candidateBinding && record.consumed == claim.consumed &&
		record.canonical == claim.canonical
}

func revokeRunnerLedgerPreflightClaim(claim *runnerLedgerPreflightClaim) {
	if claim == nil {
		return
	}
	registered, ok := runnerLedgerPreflightClaimRegistry.LoadAndDelete(claim)
	record, recordOK := registered.(runnerLedgerPreflightClaimRegistryRecord)
	if !ok || !recordOK {
		if claim.consumed != nil {
			claim.consumed.Store(true)
		}
		if claim.binding != nil && claim.binding.binder != nil && runnerOwnedPointer(claim.binding.binder) {
			runnerLedgerPreflightClaimByEvidenceBinder.CompareAndDelete(claim.binding.binder, claim)
		}
		return
	}
	if record.consumed != nil {
		record.consumed.Store(true)
	}
	runnerLedgerPreflightClaimByEvidenceBinder.CompareAndDelete(record.binder, claim)
}

func revokeRunnerLedgerPreflightClaims(binder runnerLedgerPreflightClaimBinder) {
	if binder == nil || !runnerOwnedPointer(binder) {
		return
	}
	value, ok := runnerLedgerPreflightClaimByEvidenceBinder.LoadAndDelete(binder)
	claim, claimOK := value.(*runnerLedgerPreflightClaim)
	if ok && claimOK {
		runnerLedgerPreflightClaimRegistry.Delete(claim)
		if claim.consumed != nil {
			claim.consumed.Store(true)
		}
	}
}

func cloneStringPointerIfNonEmpty(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
