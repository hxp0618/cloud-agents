package migration

import (
	"context"
	"crypto/sha256"
	"errors"
	"sync"
	"sync/atomic"
	"time"
)

const (
	runnerLedgerCommitObservationWriterPermitDigestDomain   = "cloud-agents/runner-ledger-commit-observation-writer/permit/v1"
	runnerLedgerAmbiguousResolutionWriterPermitDigestDomain = "cloud-agents/runner-ledger-ambiguous-resolution-writer/permit/v1"
	runnerLedgerReconciliationClosedReceiptDigestDomain     = "cloud-agents/runner-ledger-reconciliation/closed-receipt/v1"
)

// These two binder ports deliberately remain distinct. Neither permit can be
// converted into, or consumed by, the other record kernel.
type runnerLedgerCommitObservationRecordBinder interface {
	EvidenceSession
	runnerLedgerRecoveryAdmissionClaimBinder
	bindRunnerLedgerRecoveryCommitObservationRecord(context.Context, *runnerLedgerCommitObservationWriterPermit) (EvidenceJournal, JournalCursor, *OwnedEvidenceRecord, error)
	runnerLedgerCommitObservationRecordBinderSealed()
}

type runnerLedgerAmbiguousResolutionRecordBinder interface {
	EvidenceSession
	runnerLedgerRecoveryAdmissionClaimBinder
	bindRunnerLedgerRecoveryAmbiguousResolutionRecord(context.Context, *runnerLedgerAmbiguousResolutionWriterPermit) (EvidenceJournal, JournalCursor, *OwnedEvidenceRecord, error)
	runnerLedgerAmbiguousResolutionRecordBinderSealed()
}

type runnerLedgerReconciliationRecordBinder interface {
	EvidenceSession
	runnerLedgerRecoveryAdmissionClaimBinder
}

type runnerLedgerReconciliationAdmissionSeed struct {
	session                  DatabaseSession
	binder                   runnerLedgerReconciliationRecordBinder
	use                      *runnerLedgerRecoveryAdmissionUseRecord
	key                      int64
	candidateBinding         *verifiedEvidenceRunBinding
	generation               generationIdentity
	evidenceBoundary         [32]byte
	recoveryDigest           [32]byte
	recoveryTail             Digest
	consumerFactSubject      Digest
	ledgerDigest             Digest
	ledgerHead               string
	ledgerLength             uint32
	connectedAuthority       Digest
	migrationAuthority       Digest
	projectionSubject        Digest
	catalogContractDigest    *Digest
	catalogDigest            Digest
	runtimeInputs            [32]byte
	bindings                 RunnerProjectionBindings
	projection               *runnerLedgerCatalogPreflight
	database                 runnerPreparedDatabaseIdentity
	selection                runnerLedgerRecoveryAdmissionSelection
	admissionPermitCanonical [32]byte
}

// runnerLedgerReconciliationClosedReceipt is minted only after the exact
// retained read-only session has been revalidated, unlocked, reset, and
// closed. It carries no database handle and cannot itself append evidence.
type runnerLedgerReconciliationClosedReceipt struct {
	self                     *runnerLedgerReconciliationClosedReceipt
	action                   runnerLedgerRecoveryAction
	classification           *runnerLedgerReconciliationFacts
	observedLedger           runnerLedgerPrefix
	projectionSubject        Digest
	consumerFactSubject      Digest
	evidenceBoundary         [32]byte
	admissionPermitCanonical [32]byte
	database                 runnerPreparedDatabaseIdentity
	closed                   bool
	token                    *runnerLedgerReconciliationReceiptToken
	canonical                [32]byte
}

type runnerLedgerReconciliationReceiptToken struct{}

type runnerLedgerCommitObservationWriterPermit struct {
	self                     *runnerLedgerCommitObservationWriterPermit
	binder                   runnerLedgerCommitObservationRecordBinder
	use                      *runnerLedgerRecoveryAdmissionUseRecord
	candidateBinding         *verifiedEvidenceRunBinding
	generation               generationIdentity
	recoveryDigest           [32]byte
	recoveryTail             Digest
	cursor                   JournalCursor
	selection                runnerLedgerRecoveryAdmissionSelection
	receipt                  *runnerLedgerReconciliationClosedReceipt
	terminal                 AttemptTerminalState
	consumerFactSubject      Digest
	evidenceBoundary         [32]byte
	admissionPermitCanonical [32]byte
	consumed                 *atomic.Bool
	canonical                [32]byte
}

type runnerLedgerAmbiguousResolutionWriterPermit struct {
	self                     *runnerLedgerAmbiguousResolutionWriterPermit
	binder                   runnerLedgerAmbiguousResolutionRecordBinder
	use                      *runnerLedgerRecoveryAdmissionUseRecord
	candidateBinding         *verifiedEvidenceRunBinding
	generation               generationIdentity
	recoveryDigest           [32]byte
	recoveryTail             Digest
	cursor                   JournalCursor
	selection                runnerLedgerRecoveryAdmissionSelection
	receipt                  *runnerLedgerReconciliationClosedReceipt
	resolution               AmbiguousResolutionState
	consumerFactSubject      Digest
	evidenceBoundary         [32]byte
	admissionPermitCanonical [32]byte
	consumed                 *atomic.Bool
	canonical                [32]byte
}

type runnerLedgerCommitObservationWriterPermitRecord struct {
	permit           *runnerLedgerCommitObservationWriterPermit
	binder           runnerLedgerCommitObservationRecordBinder
	candidateBinding *verifiedEvidenceRunBinding
	cursorValid      *atomic.Bool
	consumed         *atomic.Bool
	canonical        [32]byte
}

type runnerLedgerAmbiguousResolutionWriterPermitRecord struct {
	permit           *runnerLedgerAmbiguousResolutionWriterPermit
	binder           runnerLedgerAmbiguousResolutionRecordBinder
	candidateBinding *verifiedEvidenceRunBinding
	cursorValid      *atomic.Bool
	consumed         *atomic.Bool
	canonical        [32]byte
}

type runnerLedgerCommitObservationWriterClaim struct {
	binder           runnerLedgerCommitObservationRecordBinder
	candidateBinding *verifiedEvidenceRunBinding
	generation       generationIdentity
	recoveryDigest   [32]byte
	recoveryTail     Digest
	cursor           JournalCursor
	selection        runnerLedgerRecoveryAdmissionSelection
	receipt          *runnerLedgerReconciliationClosedReceipt
	terminal         AttemptTerminalState
	canonical        [32]byte
}

type runnerLedgerAmbiguousResolutionWriterClaim struct {
	binder           runnerLedgerAmbiguousResolutionRecordBinder
	candidateBinding *verifiedEvidenceRunBinding
	generation       generationIdentity
	recoveryDigest   [32]byte
	recoveryTail     Digest
	cursor           JournalCursor
	selection        runnerLedgerRecoveryAdmissionSelection
	receipt          *runnerLedgerReconciliationClosedReceipt
	resolution       AmbiguousResolutionState
	canonical        [32]byte
}

var (
	runnerLedgerCommitObservationWriterPermitRegistry   sync.Map
	runnerLedgerAmbiguousResolutionWriterPermitRegistry sync.Map
)

func claimRunnerLedgerCommitObservationAdmissionPermit(permit *runnerLedgerCommitObservationAdmissionPermit) (runnerLedgerReconciliationAdmissionSeed, error) {
	if permit == nil {
		return runnerLedgerReconciliationAdmissionSeed{}, fail(CodeTransactionBoundary, "runner-ledger-recovery-commit-observation-claim", "commit-observation admission permit is unavailable", nil)
	}
	return claimRunnerLedgerReconciliationAdmissionPermit(permit, generatedRunnerLedgerRecoveryProfiles[2].action)
}

func claimRunnerLedgerAmbiguousResolutionAdmissionPermit(permit *runnerLedgerAmbiguousResolutionAdmissionPermit) (runnerLedgerReconciliationAdmissionSeed, error) {
	if permit == nil {
		return runnerLedgerReconciliationAdmissionSeed{}, fail(CodeTransactionBoundary, "runner-ledger-recovery-ambiguous-resolution-claim", "ambiguous-resolution admission permit is unavailable", nil)
	}
	return claimRunnerLedgerReconciliationAdmissionPermit(permit, generatedRunnerLedgerRecoveryProfiles[3].action)
}

type runnerLedgerReconciliationAdmissionCleanup struct {
	session DatabaseSession
	key     int64
}

type runnerLedgerReconciliationAdmissionCleanupBinding struct {
	self      *runnerLedgerReconciliationAdmissionCleanupBinding
	owner     runnerLedgerRecoveryCloseOnlyPermit
	core      *runnerLedgerRecoveryAdmissionPermitCore
	session   DatabaseSession
	key       int64
	action    runnerLedgerRecoveryAction
	canonical [32]byte
}

type runnerLedgerReconciliationAdmissionCleanupRecord struct {
	self      *runnerLedgerReconciliationAdmissionCleanupRecord
	binding   *runnerLedgerReconciliationAdmissionCleanupBinding
	owner     runnerLedgerRecoveryCloseOnlyPermit
	core      *runnerLedgerRecoveryAdmissionPermitCore
	session   DatabaseSession
	key       int64
	action    runnerLedgerRecoveryAction
	canonical [32]byte
}

var (
	runnerLedgerReconciliationAdmissionCleanupRegistry sync.Map
	runnerLedgerReconciliationAdmissionClaimMu         sync.Mutex
)

func runnerLedgerRecoveryPermitRawCore(owner runnerLedgerRecoveryCloseOnlyPermit) *runnerLedgerRecoveryAdmissionPermitCore {
	switch permit := owner.(type) {
	case *runnerLedgerCommitObservationAdmissionPermit:
		if permit != nil {
			return permit.core
		}
	case *runnerLedgerAmbiguousResolutionAdmissionPermit:
		if permit != nil {
			return permit.core
		}
	case *runnerLedgerRetryHandoffAdmissionPermit:
		if permit != nil {
			return permit.core
		}
	case *runnerLedgerRecoveryExecutionAdmissionPermit:
		if permit != nil {
			return permit.core
		}
	}
	return nil
}

func sameRunnerLedgerReconciliationAdmissionCleanup(left, right runnerLedgerReconciliationAdmissionCleanup) bool {
	return left.session != nil && right.session != nil && left.key == right.key &&
		sameRunnerOwnedPointer(left.session, right.session)
}

func registerRunnerLedgerReconciliationAdmissionCleanup(owner runnerLedgerRecoveryCloseOnlyPermit, core *runnerLedgerRecoveryAdmissionPermitCore) bool {
	if owner == nil || core == nil || core.binding == nil || core.session == nil || core.canonical == ([32]byte{}) ||
		(core.action != generatedRunnerLedgerRecoveryProfiles[2].action && core.action != generatedRunnerLedgerRecoveryProfiles[3].action &&
			core.action != generatedRunnerLedgerRecoveryProfiles[4].action && core.action != generatedRunnerLedgerRecoveryProfiles[5].action) {
		return false
	}
	binding := &runnerLedgerReconciliationAdmissionCleanupBinding{
		owner: owner, core: core, session: core.session, key: core.key, action: core.action, canonical: core.canonical,
	}
	binding.self = binding
	record := &runnerLedgerReconciliationAdmissionCleanupRecord{
		binding: binding, owner: owner, core: core, session: core.session, key: core.key,
		action: core.action, canonical: core.canonical,
	}
	record.self = record
	runnerLedgerReconciliationAdmissionCleanupRegistry.Store(owner, record)
	return validRunnerLedgerReconciliationAdmissionCleanupRecord(owner, core, record, core.action, true)
}

func deleteRunnerLedgerReconciliationAdmissionCleanup(owner runnerLedgerRecoveryCloseOnlyPermit) {
	runnerLedgerReconciliationAdmissionCleanupRegistry.Delete(owner)
}

func validRunnerLedgerReconciliationAdmissionCleanupRegistry(owner runnerLedgerRecoveryCloseOnlyPermit, core *runnerLedgerRecoveryAdmissionPermitCore) bool {
	if core == nil || (core.action != generatedRunnerLedgerRecoveryProfiles[2].action && core.action != generatedRunnerLedgerRecoveryProfiles[3].action &&
		core.action != generatedRunnerLedgerRecoveryProfiles[4].action && core.action != generatedRunnerLedgerRecoveryProfiles[5].action) {
		return true
	}
	value, loaded := runnerLedgerReconciliationAdmissionCleanupRegistry.Load(owner)
	record, ok := value.(*runnerLedgerReconciliationAdmissionCleanupRecord)
	return loaded && ok && validRunnerLedgerReconciliationAdmissionCleanupRecord(owner, core, record, core.action, true)
}

func validRunnerLedgerReconciliationAdmissionCleanupRecord(
	owner runnerLedgerRecoveryCloseOnlyPermit,
	core *runnerLedgerRecoveryAdmissionPermitCore,
	record *runnerLedgerReconciliationAdmissionCleanupRecord,
	action runnerLedgerRecoveryAction,
	requireExactHandles bool,
) bool {
	if owner == nil || core == nil || record == nil || record.self != record || record.binding == nil ||
		record.binding.self != record.binding || record.owner != owner || record.binding.owner != owner ||
		record.core != core || record.binding.core != core || record.session == nil || record.binding.session == nil ||
		record.action != action || record.binding.action != action || record.canonical == ([32]byte{}) ||
		record.canonical != record.binding.canonical {
		return false
	}
	if !requireExactHandles {
		return true
	}
	return record.key == core.key && record.binding.key == core.key &&
		sameRunnerOwnedPointer(record.session, core.session) && sameRunnerOwnedPointer(record.binding.session, core.session) &&
		record.canonical == core.canonical
}

func claimRunnerLedgerReconciliationAdmissionRecords(owner runnerLedgerRecoveryCloseOnlyPermit) (
	runnerLedgerRecoveryAdmissionPermitRegistryRecord,
	bool,
	*runnerLedgerReconciliationAdmissionCleanupRecord,
	bool,
) {
	runnerLedgerReconciliationAdmissionClaimMu.Lock()
	defer runnerLedgerReconciliationAdmissionClaimMu.Unlock()
	primaryValue, primaryLoaded := runnerLedgerRecoveryAdmissionPermitRegistry.LoadAndDelete(owner)
	primary, primaryOK := primaryValue.(runnerLedgerRecoveryAdmissionPermitRegistryRecord)
	cleanupValue, cleanupLoaded := runnerLedgerReconciliationAdmissionCleanupRegistry.LoadAndDelete(owner)
	cleanup, cleanupOK := cleanupValue.(*runnerLedgerReconciliationAdmissionCleanupRecord)
	return primary, primaryLoaded && primaryOK, cleanup, cleanupLoaded && cleanupOK
}

func invalidateRunnerLedgerReconciliationAdmissionOwner(owner runnerLedgerRecoveryCloseOnlyPermit, core *runnerLedgerRecoveryAdmissionPermitCore) {
	if core != nil {
		core.closed = true
		core.session = nil
		core.evidenceBinder = nil
		core.use = nil
		core.projection = nil
	}
	switch permit := owner.(type) {
	case *runnerLedgerCommitObservationAdmissionPermit:
		if permit != nil {
			permit.core = nil
		}
	case *runnerLedgerAmbiguousResolutionAdmissionPermit:
		if permit != nil {
			permit.core = nil
		}
	case *runnerLedgerRetryHandoffAdmissionPermit:
		if permit != nil {
			permit.core = nil
		}
	case *runnerLedgerRecoveryExecutionAdmissionPermit:
		if permit != nil {
			permit.core = nil
		}
	}
}

// runnerLedgerReconciliationAdmissionCleanupAuthority chooses cleanup facts
// independently from successor validity. At least one independently registered
// record is mandatory, so a caller-constructed owner/core/binding graph has no
// cleanup authority. Once registry provenance is present, three matching
// copies select the original session and reject a singly drifted foreign handle.
func runnerLedgerReconciliationAdmissionCleanupAuthority(
	owner runnerLedgerRecoveryCloseOnlyPermit,
	rawCore *runnerLedgerRecoveryAdmissionPermitCore,
	record runnerLedgerRecoveryAdmissionPermitRegistryRecord,
	recordOK bool,
	cleanupRecord *runnerLedgerReconciliationAdmissionCleanupRecord,
	cleanupRecordOK bool,
	action runnerLedgerRecoveryAction,
) (runnerLedgerReconciliationAdmissionCleanup, bool) {
	if !recordOK && !cleanupRecordOK {
		return runnerLedgerReconciliationAdmissionCleanup{}, false
	}
	if rawCore == nil {
		if cleanupRecordOK && cleanupRecord != nil {
			rawCore = cleanupRecord.core
		} else {
			rawCore = record.core
		}
	}
	if rawCore == nil || cleanupRecordOK &&
		!validRunnerLedgerReconciliationAdmissionCleanupRecord(owner, rawCore, cleanupRecord, action, false) {
		return runnerLedgerReconciliationAdmissionCleanup{}, false
	}
	candidates := make([]runnerLedgerReconciliationAdmissionCleanup, 0, 7)
	if cleanupRecordOK {
		candidates = append(candidates,
			runnerLedgerReconciliationAdmissionCleanup{session: cleanupRecord.session, key: cleanupRecord.key},
			runnerLedgerReconciliationAdmissionCleanup{session: cleanupRecord.binding.session, key: cleanupRecord.binding.key},
		)
	}
	candidates = append(candidates, runnerLedgerReconciliationAdmissionCleanup{session: rawCore.session, key: rawCore.key})
	binding := rawCore.binding
	bindingLinked := binding != nil && binding.core == rawCore && binding.owner == owner
	if bindingLinked {
		candidates = append(candidates, runnerLedgerReconciliationAdmissionCleanup{session: binding.session, key: binding.key})
	}
	if recordOK && record.owner == owner && record.core == rawCore {
		candidates = append(candidates, runnerLedgerReconciliationAdmissionCleanup{session: record.session, key: record.key})
		if record.binding != nil && record.binding.core == rawCore && record.binding.owner == owner {
			candidates = append(candidates, runnerLedgerReconciliationAdmissionCleanup{session: record.binding.session, key: record.binding.key})
		}
	}
	for _, candidate := range candidates {
		votes := 0
		for _, other := range candidates {
			if sameRunnerLedgerReconciliationAdmissionCleanup(candidate, other) {
				votes++
			}
		}
		if votes >= 3 {
			return candidate, true
		}
	}
	return runnerLedgerReconciliationAdmissionCleanup{}, false
}

func claimRunnerLedgerReconciliationAdmissionPermit(owner runnerLedgerRecoveryCloseOnlyPermit, action runnerLedgerRecoveryAction) (runnerLedgerReconciliationAdmissionSeed, error) {
	var seed runnerLedgerReconciliationAdmissionSeed
	core := runnerLedgerRecoveryPermitRawCore(owner)
	record, recordOK, cleanupRecord, cleanupRecordOK := claimRunnerLedgerReconciliationAdmissionRecords(owner)
	if !recordOK && !cleanupRecordOK {
		return seed, fail(CodeEvidenceRecoveryRequired, "runner-ledger-reconciliation-claim", "reconciliation cleanup authority is unavailable or changed", nil)
	}
	cleanup, cleanupOK := runnerLedgerReconciliationAdmissionCleanupAuthority(owner, core, record, recordOK, cleanupRecord, cleanupRecordOK, action)
	var binder runnerLedgerReconciliationRecordBinder
	binderOK := false
	if recordOK {
		switch action {
		case generatedRunnerLedgerRecoveryProfiles[2].action:
			var exact runnerLedgerCommitObservationRecordBinder
			exact, binderOK = record.evidenceBinder.(runnerLedgerCommitObservationRecordBinder)
			binder = exact
		case generatedRunnerLedgerRecoveryProfiles[3].action:
			var exact runnerLedgerAmbiguousResolutionRecordBinder
			exact, binderOK = record.evidenceBinder.(runnerLedgerAmbiguousResolutionRecordBinder)
			binder = exact
		case generatedRunnerLedgerRecoveryProfiles[4].action:
			var exact runnerLedgerRetryHandoffBinder
			exact, binderOK = record.evidenceBinder.(runnerLedgerRetryHandoffBinder)
			binder = exact
		case generatedRunnerLedgerRecoveryProfiles[5].action:
			var exact runnerLedgerRecoverySuccessEvidenceBinder
			exact, binderOK = record.evidenceBinder.(runnerLedgerRecoverySuccessEvidenceBinder)
			binder = exact
		}
	}
	valid := recordOK && cleanupRecordOK && record.session != nil && binderOK && binder != nil &&
		core != nil && runnerLedgerRecoveryPermitCore(owner) == core && record.owner == owner && core.owner == owner &&
		core.action == action && owner.recoveryAdmissionAction() == action &&
		sameRunnerOwnedPointer(record.evidenceBinder, binder) &&
		validRunnerLedgerReconciliationAdmissionCleanupRecord(owner, core, cleanupRecord, action, true) &&
		validRunnerLedgerRecoveryAdmissionPermitWithRecord(owner, core, record)
	if valid {
		seed = runnerLedgerReconciliationAdmissionSeed{
			session: record.session, binder: binder, use: core.use, key: core.key,
			candidateBinding: core.candidateBinding, generation: core.generation,
			evidenceBoundary: core.evidenceBoundary, recoveryDigest: core.recoveryDigest, recoveryTail: core.recoveryTail,
			consumerFactSubject: core.consumerFactSubject, ledgerDigest: core.ledgerDigest, ledgerHead: core.ledgerHead,
			ledgerLength: core.ledgerLength, connectedAuthority: core.connectedAuthorityDigest,
			migrationAuthority: core.migrationAuthorityDigest, projectionSubject: core.projectionSubject,
			catalogContractDigest: cloneDigestPointer(core.catalogContractDigest), catalogDigest: core.catalogDigest,
			runtimeInputs: core.runtimeInputs, bindings: core.bindings.ownedCopy(), projection: cloneRunnerLedgerCatalogPreflight(core.projection),
			database: core.database, selection: core.selection, admissionPermitCanonical: core.canonical,
		}
	}
	invalidateRunnerLedgerReconciliationAdmissionOwner(owner, core)
	if valid {
		return seed, nil
	}
	primary := fail(CodeEvidenceRecoveryRequired, "runner-ledger-reconciliation-claim", "reconciliation admission authority changed before transfer", nil)
	if cleanupOK {
		return runnerLedgerReconciliationAdmissionSeed{}, closeRunnerDatabasePreflight(cleanup.session, cleanup.key, true, primary)
	}
	return runnerLedgerReconciliationAdmissionSeed{}, primary
}

func closeRunnerLedgerReconciliationAdmissionPermit(owner runnerLedgerRecoveryCloseOnlyPermit, expected runnerLedgerRecoveryAction, primary error) error {
	core := runnerLedgerRecoveryPermitRawCore(owner)
	record, recordOK, cleanupRecord, cleanupRecordOK := claimRunnerLedgerReconciliationAdmissionRecords(owner)
	if !recordOK && !cleanupRecordOK {
		return fail(CodeTransactionBoundary, "runner-ledger-recovery-admission-close", "reconciliation cleanup authority is unavailable or already closed", nil)
	}
	cleanup, cleanupOK := runnerLedgerReconciliationAdmissionCleanupAuthority(
		owner, core, record, recordOK, cleanupRecord, cleanupRecordOK, expected,
	)
	valid := recordOK && cleanupRecordOK && core != nil && runnerLedgerRecoveryPermitCore(owner) == core &&
		core.action == expected && owner.recoveryAdmissionAction() == expected &&
		validRunnerLedgerReconciliationAdmissionCleanupRecord(owner, core, cleanupRecord, expected, true) &&
		validRunnerLedgerRecoveryAdmissionPermitWithRecord(owner, core, record)
	invalidateRunnerLedgerReconciliationAdmissionOwner(owner, core)
	if !valid {
		primary = fail(CodeTransactionBoundary, "runner-ledger-recovery-admission-close", "reconciliation admission permit changed before close", nil)
	}
	if cleanupOK {
		return closeRunnerDatabasePreflight(cleanup.session, cleanup.key, true, primary)
	}
	return primary
}

func (runner *Runner) revalidateAndCloseRunnerLedgerReconciliationAdmission(ctx context.Context, seed runnerLedgerReconciliationAdmissionSeed, bundle *RuntimeBundle) (*runnerLedgerReconciliationClosedReceipt, error) {
	failClosed := func(primary error) (*runnerLedgerReconciliationClosedReceipt, error) {
		return nil, closeRunnerDatabasePreflight(seed.session, seed.key, true, primary)
	}
	if seed.session == nil || seed.binder == nil || seed.candidateBinding == nil || seed.projection == nil ||
		seed.projection.reconciliation == nil || seed.runtimeInputs == ([32]byte{}) || seed.admissionPermitCanonical == ([32]byte{}) ||
		!runnerLedgerReconciliationSelection(seed.selection) || !validRunnerLedgerCatalogPreflight(seed.projection) ||
		seed.bindings.validateAt(time.Now()) != nil || seed.bindings.expectedCanonical == "" ||
		!validRunnerLedgerRecoveryAdmissionUse(seed.binder, seed.use, seed.consumerFactSubject, seed.selection.action, seed.evidenceBoundary, true) {
		return failClosed(fail(CodeEvidenceRecoveryRequired, "runner-ledger-reconciliation-revalidate", "reconciliation admission facts are unavailable or changed", nil))
	}
	verifiedBundle, err := verifiedRunnerLedgerCatalogBundle(bundle)
	if err != nil || verifiedBundle.ownedInputs.canonical != seed.runtimeInputs {
		if err == nil {
			err = fail(CodeUntrusted, "runner-ledger-reconciliation-runtime", "verified runtime changed after recovery admission", nil)
		}
		return failClosed(err)
	}
	verifiedPlans, err := buildExactStatementPlans(verifiedBundle, seed.bindings, time.Now())
	if err != nil {
		return failClosed(err)
	}
	planDigest, planCount, err := runnerEntryPlanClosureDigest(verifiedPlans, seed.selection.migrationID)
	if err != nil || planDigest != seed.selection.planDigest || planCount != seed.selection.planCount {
		return failClosed(fail(CodeUntrusted, "runner-ledger-reconciliation-plans", "verified statement-plan closure changed after recovery admission", err))
	}
	current := seed.binder.CurrentCandidate()
	active := seed.binder.ActiveGeneration()
	recovery := seed.binder.RecoverySnapshot()
	if !validOwnedCurrentCandidate(current) || current.binding != seed.candidateBinding || active.kind != activeGenerationCurrent ||
		!sameGenerationIdentity(active.identity, seed.generation) ||
		!runnerLedgerReconciliationRecoveryBoundary(recovery, seed.generation, seed.recoveryDigest, seed.recoveryTail, seed.selection, seed.projection.reconciliation) {
		return failClosed(fail(CodeEvidenceRecoveryRequired, "runner-ledger-reconciliation-revalidate", "evidence boundary changed before final database revalidation", nil))
	}
	hint, err := runnerLedgerReconciliationHintFromSnapshot(recovery)
	if err != nil || hint == nil {
		return failClosed(fail(CodeEvidenceRecoveryRequired, "runner-ledger-reconciliation-revalidate", "reconciliation hint changed before final database revalidation", err))
	}
	projection := seed.projection
	observation := &runnerLockedLedgerCatalogObservation{
		session: seed.session, key: seed.key, bindings: seed.bindings.ownedCopy(), bundle: verifiedBundle,
		plans: verifiedPlans, ledger: cloneRunnerLedgerPrefix(projection.ledger),
		connected: cloneProjectionValue(projection.connectedAuthority), migrationRole: cloneProjectionValue(projection.migrationRoleAuthority),
		initial: cloneCatalogStateProjectionResultPointer(projection.initialPredecessor), cumulative: cloneCatalogProjectionResultPointer(projection.cumulativeCatalog),
		catalogContractDigest: cloneDigestPointer(projection.catalogContractDigest), projectionSubject: projection.projectionSubjectDigest,
		reconciliationHint: cloneRunnerLedgerReconciliationHint(hint), reconciliation: cloneRunnerLedgerReconciliationFacts(projection.reconciliation),
	}
	observation.self = observation
	primary := observation.revalidateRecoveryAdmission(ctx, runner)
	if err := observation.close(primary); err != nil {
		return nil, err
	}
	return bindRunnerLedgerReconciliationClosedReceipt(seed)
}

func bindRunnerLedgerReconciliationClosedReceipt(seed runnerLedgerReconciliationAdmissionSeed) (*runnerLedgerReconciliationClosedReceipt, error) {
	if seed.projection == nil || !validRunnerLedgerCatalogPreflight(seed.projection) ||
		!runnerLedgerReconciliationSelection(seed.selection) || seed.projection.reconciliation == nil {
		return nil, fail(CodeEvidenceRecoveryRequired, "runner-ledger-reconciliation-receipt", "closed reconciliation inputs are unavailable", nil)
	}
	receipt := &runnerLedgerReconciliationClosedReceipt{
		action: seed.selection.action, classification: cloneRunnerLedgerReconciliationFacts(seed.projection.reconciliation),
		observedLedger: cloneRunnerLedgerPrefix(seed.projection.ledger), projectionSubject: seed.projectionSubject,
		consumerFactSubject: seed.consumerFactSubject, evidenceBoundary: seed.evidenceBoundary,
		admissionPermitCanonical: seed.admissionPermitCanonical, database: seed.database,
		closed: true, token: &runnerLedgerReconciliationReceiptToken{},
	}
	receipt.self = receipt
	receipt.canonical = runnerLedgerReconciliationClosedReceiptDigest(receipt)
	if !validRunnerLedgerReconciliationClosedReceipt(receipt, seed.selection) {
		return nil, fail(CodeEvidenceRecoveryRequired, "runner-ledger-reconciliation-receipt", "closed reconciliation receipt could not be sealed", nil)
	}
	return receipt, nil
}

func validRunnerLedgerReconciliationClosedReceipt(receipt *runnerLedgerReconciliationClosedReceipt, selection runnerLedgerRecoveryAdmissionSelection) bool {
	return receipt != nil && receipt.self == receipt && receipt.closed && receipt.token != nil &&
		receipt.action == selection.action && runnerLedgerReconciliationSelection(selection) &&
		validRunnerLedgerReconciliationFacts(receipt.classification) &&
		receipt.classification.state == selection.recoveryState && receipt.classification.action == selection.recoveryAction &&
		receipt.classification.migrationID == selection.migrationID && receipt.classification.attemptIndex == selection.attemptIndex &&
		validRunnerLedgerPrefixShape(receipt.observedLedger) && receipt.projectionSubject.Validate() == nil &&
		receipt.consumerFactSubject.Validate() == nil && receipt.evidenceBoundary != ([32]byte{}) &&
		receipt.admissionPermitCanonical != ([32]byte{}) && receipt.database.postgresMajor >= 15 && receipt.database.postgresMajor <= 17 &&
		receipt.database.serverVersionNum != 0 && receipt.database.databaseName != "" && receipt.database.sessionUser != "" &&
		receipt.database.currentUser == MigrationOwnerRole && receipt.canonical != ([32]byte{}) &&
		receipt.canonical == runnerLedgerReconciliationClosedReceiptDigest(receipt)
}

func runnerLedgerReconciliationClosedReceiptDigest(receipt *runnerLedgerReconciliationClosedReceipt) [32]byte {
	if receipt == nil || receipt.self != receipt || !receipt.closed || receipt.token == nil ||
		!validRunnerLedgerReconciliationFacts(receipt.classification) || !validRunnerLedgerPrefixShape(receipt.observedLedger) ||
		receipt.projectionSubject.Validate() != nil || receipt.consumerFactSubject.Validate() != nil ||
		receipt.evidenceBoundary == ([32]byte{}) || receipt.admissionPermitCanonical == ([32]byte{}) ||
		receipt.database.postgresMajor < 15 || receipt.database.postgresMajor > 17 || receipt.database.serverVersionNum == 0 ||
		receipt.database.databaseName == "" || receipt.database.sessionUser == "" || receipt.database.currentUser != MigrationOwnerRole {
		return [32]byte{}
	}
	wire := struct {
		Action              runnerLedgerRecoveryAction          `json:"action"`
		Classification      runnerLedgerReconciliationFactsWire `json:"classification"`
		ObservedRows        []CommitIntentLedgerRow             `json:"observed_rows"`
		ObservedDigest      Digest                              `json:"observed_digest"`
		ObservedHead        string                              `json:"observed_head"`
		ProjectionSubject   Digest                              `json:"projection_subject"`
		ConsumerFactSubject Digest                              `json:"consumer_fact_subject"`
		PostgresMajor       uint16                              `json:"postgres_major"`
		ServerVersionNum    uint32                              `json:"server_version_num"`
		DatabaseName        string                              `json:"database_name"`
		DatabaseSessionUser string                              `json:"database_session_user"`
		DatabaseCurrentUser string                              `json:"database_current_user"`
	}{
		Action: receipt.action, Classification: receipt.classification.wire(),
		ObservedRows: cloneProjectionValue(receipt.observedLedger.rows), ObservedDigest: receipt.observedLedger.digest,
		ObservedHead: receipt.observedLedger.head, ProjectionSubject: receipt.projectionSubject,
		ConsumerFactSubject: receipt.consumerFactSubject,
		PostgresMajor:       receipt.database.postgresMajor, ServerVersionNum: receipt.database.serverVersionNum,
		DatabaseName: receipt.database.databaseName, DatabaseSessionUser: receipt.database.sessionUser,
		DatabaseCurrentUser: receipt.database.currentUser,
	}
	canonical, err := canonicalContractKey(wire)
	if err != nil || canonical == "" {
		return [32]byte{}
	}
	h := sha256.New()
	h.Write([]byte(runnerLedgerReconciliationClosedReceiptDigestDomain + "\x00"))
	h.Write(receipt.evidenceBoundary[:])
	h.Write(receipt.admissionPermitCanonical[:])
	h.Write([]byte(canonical))
	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result
}

func runnerLedgerReconciliationSelection(selection runnerLedgerRecoveryAdmissionSelection) bool {
	if !runnerLedgerRecoverySelectionAllowed(selection) || selection.planCount == 0 || selection.planDigest == ([32]byte{}) ||
		selection.attemptIndex == 0 || selection.maxAttempts == 0 || selection.attemptIndex > selection.maxAttempts ||
		selection.entryDigest.Validate() != nil || !migrationIDPattern.MatchString(selection.migrationID) ||
		selection.recoveryAction != RecoveryReconcileCommit {
		return false
	}
	return (selection.action == generatedRunnerLedgerRecoveryProfiles[2].action && selection.profileIndex == 2 && selection.recoveryState == RecoveryDanglingCommitIntent) ||
		(selection.action == generatedRunnerLedgerRecoveryProfiles[3].action && selection.profileIndex == 3 && selection.recoveryState == RecoveryAmbiguousUnresolved)
}

func runnerLedgerReconciliationRecoveryBoundary(snapshot *RecoverySnapshot, generation generationIdentity, recoveryDigest [32]byte, recoveryTail Digest, selection runnerLedgerRecoveryAdmissionSelection, facts *runnerLedgerReconciliationFacts) bool {
	if snapshot == nil || !runnerLedgerReconciliationSelection(selection) || !validRunnerLedgerReconciliationFacts(facts) ||
		!validRecoverySnapshotForJournal(snapshot, generation, snapshot.cursor) ||
		generationJournalRecoveryDigest(snapshot) != recoveryDigest || snapshot.tailDigest != recoveryTail ||
		snapshot.state != selection.recoveryState || snapshot.nextPermittedAction != selection.recoveryAction ||
		snapshot.migrationID == nil || *snapshot.migrationID != selection.migrationID ||
		snapshot.attemptIndex == nil || *snapshot.attemptIndex != selection.attemptIndex {
		return false
	}
	hint, err := runnerLedgerReconciliationHintFromSnapshot(snapshot)
	if err != nil || hint == nil {
		return false
	}
	return facts.state == hint.state && facts.action == hint.action && facts.migrationID == hint.migrationID &&
		facts.attemptIndex == hint.attemptIndex && facts.targetIndex == hint.targetIndex &&
		facts.commitRecordDigest == hint.commitRecordDigest && facts.commitBodyDigest == hint.commitBodyDigest &&
		runnerCanonicalEqual(facts.expectedLedgerRow, hint.commit.LedgerRow) &&
		facts.pendingCatalogDigest == hint.pendingCatalogDigest && facts.committedCatalogDigest == hint.committedCatalogDigest &&
		equalDigestPointer(facts.unresolvedTerminalDigest, hint.unresolvedTerminalDigest) &&
		equalDigestPointer(facts.unresolvedTerminalRecordDigest, hint.unresolvedTerminalRecordDigest)
}

func mintRunnerLedgerCommitObservationWriterPermit(seed runnerLedgerReconciliationAdmissionSeed, receipt *runnerLedgerReconciliationClosedReceipt) (*runnerLedgerCommitObservationWriterPermit, error) {
	binder, ok := seed.binder.(runnerLedgerCommitObservationRecordBinder)
	recovery := seed.binder.RecoverySnapshot()
	if !ok || !validRunnerLedgerReconciliationClosedReceipt(receipt, seed.selection) ||
		seed.selection.action != generatedRunnerLedgerRecoveryProfiles[2].action ||
		!runnerLedgerReconciliationRecoveryBoundary(recovery, seed.generation, seed.recoveryDigest, seed.recoveryTail, seed.selection, receipt.classification) {
		return nil, fail(CodeEvidenceRecoveryRequired, "runner-ledger-recovery-commit-observation-permit", "commit-observation evidence boundary changed after lifecycle close", nil)
	}
	terminal, err := buildRunnerLedgerReconciliationTerminal(recovery, receipt.classification, seed.selection, seed.database.postgresMajor)
	if err != nil {
		return nil, err
	}
	permit := &runnerLedgerCommitObservationWriterPermit{
		binder: binder, use: seed.use, candidateBinding: seed.candidateBinding, generation: seed.generation,
		recoveryDigest: seed.recoveryDigest, recoveryTail: seed.recoveryTail, cursor: recovery.cursor.clone(),
		selection: seed.selection, receipt: receipt, terminal: terminal,
		consumerFactSubject: seed.consumerFactSubject, evidenceBoundary: seed.evidenceBoundary,
		admissionPermitCanonical: seed.admissionPermitCanonical, consumed: &atomic.Bool{},
	}
	permit.self = permit
	permit.canonical = runnerLedgerCommitObservationWriterPermitDigest(permit)
	if permit.canonical == ([32]byte{}) {
		return nil, fail(CodeEvidenceRecoveryRequired, "runner-ledger-recovery-commit-observation-permit", "commit-observation writer permit could not be identified", nil)
	}
	runnerLedgerCommitObservationWriterPermitRegistry.Store(permit, runnerLedgerCommitObservationWriterPermitRecord{
		permit: permit, binder: binder, candidateBinding: seed.candidateBinding,
		cursorValid: permit.cursor.valid, consumed: permit.consumed, canonical: permit.canonical,
	})
	if !validRunnerLedgerCommitObservationWriterPermit(permit) {
		runnerLedgerCommitObservationWriterPermitRegistry.Delete(permit)
		return nil, fail(CodeEvidenceRecoveryRequired, "runner-ledger-recovery-commit-observation-permit", "commit-observation writer permit could not be sealed", nil)
	}
	return permit, nil
}

func mintRunnerLedgerAmbiguousResolutionWriterPermit(seed runnerLedgerReconciliationAdmissionSeed, receipt *runnerLedgerReconciliationClosedReceipt) (*runnerLedgerAmbiguousResolutionWriterPermit, error) {
	binder, ok := seed.binder.(runnerLedgerAmbiguousResolutionRecordBinder)
	recovery := seed.binder.RecoverySnapshot()
	if !ok || !validRunnerLedgerReconciliationClosedReceipt(receipt, seed.selection) ||
		seed.selection.action != generatedRunnerLedgerRecoveryProfiles[3].action ||
		!runnerLedgerReconciliationRecoveryBoundary(recovery, seed.generation, seed.recoveryDigest, seed.recoveryTail, seed.selection, receipt.classification) {
		return nil, fail(CodeEvidenceRecoveryRequired, "runner-ledger-recovery-ambiguous-resolution-permit", "ambiguous-resolution evidence boundary changed after lifecycle close", nil)
	}
	resolution, err := buildRunnerLedgerAmbiguousResolution(recovery, receipt.classification, seed.selection)
	if err != nil {
		return nil, err
	}
	permit := &runnerLedgerAmbiguousResolutionWriterPermit{
		binder: binder, use: seed.use, candidateBinding: seed.candidateBinding, generation: seed.generation,
		recoveryDigest: seed.recoveryDigest, recoveryTail: seed.recoveryTail, cursor: recovery.cursor.clone(),
		selection: seed.selection, receipt: receipt, resolution: resolution,
		consumerFactSubject: seed.consumerFactSubject, evidenceBoundary: seed.evidenceBoundary,
		admissionPermitCanonical: seed.admissionPermitCanonical, consumed: &atomic.Bool{},
	}
	permit.self = permit
	permit.canonical = runnerLedgerAmbiguousResolutionWriterPermitDigest(permit)
	if permit.canonical == ([32]byte{}) {
		return nil, fail(CodeEvidenceRecoveryRequired, "runner-ledger-recovery-ambiguous-resolution-permit", "ambiguous-resolution writer permit could not be identified", nil)
	}
	runnerLedgerAmbiguousResolutionWriterPermitRegistry.Store(permit, runnerLedgerAmbiguousResolutionWriterPermitRecord{
		permit: permit, binder: binder, candidateBinding: seed.candidateBinding,
		cursorValid: permit.cursor.valid, consumed: permit.consumed, canonical: permit.canonical,
	})
	if !validRunnerLedgerAmbiguousResolutionWriterPermit(permit) {
		runnerLedgerAmbiguousResolutionWriterPermitRegistry.Delete(permit)
		return nil, fail(CodeEvidenceRecoveryRequired, "runner-ledger-recovery-ambiguous-resolution-permit", "ambiguous-resolution writer permit could not be sealed", nil)
	}
	return permit, nil
}

func validRunnerLedgerCommitObservationWriterPermit(permit *runnerLedgerCommitObservationWriterPermit) bool {
	if !validRunnerLedgerCommitObservationWriterPermitWithoutRegistry(permit) {
		return false
	}
	registered, loaded := runnerLedgerCommitObservationWriterPermitRegistry.Load(permit)
	record, ok := registered.(runnerLedgerCommitObservationWriterPermitRecord)
	return loaded && ok && record.permit == permit && sameRunnerOwnedPointer(record.binder, permit.binder) &&
		record.candidateBinding == permit.candidateBinding && record.cursorValid == permit.cursor.valid &&
		record.consumed == permit.consumed && record.canonical == permit.canonical
}

func validRunnerLedgerCommitObservationWriterPermitWithoutRegistry(permit *runnerLedgerCommitObservationWriterPermit) bool {
	return permit != nil && permit.self == permit && permit.binder != nil && runnerOwnedPointer(permit.binder) && permit.use != nil &&
		permit.candidateBinding != nil && permit.generation.owner == permit.candidateBinding.owner &&
		permit.recoveryDigest != ([32]byte{}) && permit.recoveryTail.Validate() == nil && permit.cursor.Valid() &&
		sameGenerationIdentity(permit.cursor.generation, permit.generation) && runnerLedgerReconciliationSelection(permit.selection) &&
		permit.selection.action == generatedRunnerLedgerRecoveryProfiles[2].action &&
		validRunnerLedgerReconciliationClosedReceipt(permit.receipt, permit.selection) &&
		permit.terminal.Validate(permit.selection.maxAttempts) == nil && permit.consumed != nil && !permit.consumed.Load() &&
		validRunnerLedgerRecoveryAdmissionUse(permit.binder, permit.use, permit.consumerFactSubject, permit.selection.action, permit.evidenceBoundary, true) &&
		permit.admissionPermitCanonical != ([32]byte{}) && permit.canonical != ([32]byte{}) &&
		permit.canonical == runnerLedgerCommitObservationWriterPermitDigest(permit)
}

func validRunnerLedgerAmbiguousResolutionWriterPermit(permit *runnerLedgerAmbiguousResolutionWriterPermit) bool {
	if !validRunnerLedgerAmbiguousResolutionWriterPermitWithoutRegistry(permit) {
		return false
	}
	registered, loaded := runnerLedgerAmbiguousResolutionWriterPermitRegistry.Load(permit)
	record, ok := registered.(runnerLedgerAmbiguousResolutionWriterPermitRecord)
	return loaded && ok && record.permit == permit && sameRunnerOwnedPointer(record.binder, permit.binder) &&
		record.candidateBinding == permit.candidateBinding && record.cursorValid == permit.cursor.valid &&
		record.consumed == permit.consumed && record.canonical == permit.canonical
}

func validRunnerLedgerAmbiguousResolutionWriterPermitWithoutRegistry(permit *runnerLedgerAmbiguousResolutionWriterPermit) bool {
	return permit != nil && permit.self == permit && permit.binder != nil && runnerOwnedPointer(permit.binder) && permit.use != nil &&
		permit.candidateBinding != nil && permit.generation.owner == permit.candidateBinding.owner &&
		permit.recoveryDigest != ([32]byte{}) && permit.recoveryTail.Validate() == nil && permit.cursor.Valid() &&
		sameGenerationIdentity(permit.cursor.generation, permit.generation) && runnerLedgerReconciliationSelection(permit.selection) &&
		permit.selection.action == generatedRunnerLedgerRecoveryProfiles[3].action &&
		validRunnerLedgerReconciliationClosedReceipt(permit.receipt, permit.selection) &&
		permit.resolution.Validate() == nil && permit.consumed != nil && !permit.consumed.Load() &&
		validRunnerLedgerRecoveryAdmissionUse(permit.binder, permit.use, permit.consumerFactSubject, permit.selection.action, permit.evidenceBoundary, true) &&
		permit.admissionPermitCanonical != ([32]byte{}) && permit.canonical != ([32]byte{}) &&
		permit.canonical == runnerLedgerAmbiguousResolutionWriterPermitDigest(permit)
}

func runnerLedgerCommitObservationWriterPermitDigest(permit *runnerLedgerCommitObservationWriterPermit) [32]byte {
	if !runnerLedgerCommitObservationWriterPermitDigestInputs(permit) {
		return [32]byte{}
	}
	return runnerLedgerReconciliationWriterPermitDigest(
		runnerLedgerCommitObservationWriterPermitDigestDomain, permit.candidateBinding, permit.generation,
		permit.recoveryDigest, permit.recoveryTail, permit.cursor, permit.selection, permit.receipt,
		EvidenceRecord{AttemptTerminal: cloneAttemptTerminalPointer(&permit.terminal)}, permit.evidenceBoundary,
		permit.admissionPermitCanonical,
	)
}

func runnerLedgerCommitObservationWriterPermitDigestInputs(permit *runnerLedgerCommitObservationWriterPermit) bool {
	return permit != nil && permit.self == permit && permit.binder != nil && permit.candidateBinding != nil &&
		permit.generation.owner == permit.candidateBinding.owner && permit.recoveryDigest != ([32]byte{}) &&
		permit.recoveryTail.Validate() == nil && permit.cursor.Valid() && sameGenerationIdentity(permit.cursor.generation, permit.generation) &&
		permit.selection.action == generatedRunnerLedgerRecoveryProfiles[2].action && runnerLedgerReconciliationSelection(permit.selection) &&
		validRunnerLedgerReconciliationClosedReceipt(permit.receipt, permit.selection) &&
		permit.terminal.Validate(permit.selection.maxAttempts) == nil && permit.consumerFactSubject.Validate() == nil &&
		permit.evidenceBoundary != ([32]byte{}) && permit.admissionPermitCanonical != ([32]byte{}) &&
		permit.consumed != nil && !permit.consumed.Load()
}

func runnerLedgerAmbiguousResolutionWriterPermitDigest(permit *runnerLedgerAmbiguousResolutionWriterPermit) [32]byte {
	if !runnerLedgerAmbiguousResolutionWriterPermitDigestInputs(permit) {
		return [32]byte{}
	}
	return runnerLedgerReconciliationWriterPermitDigest(
		runnerLedgerAmbiguousResolutionWriterPermitDigestDomain, permit.candidateBinding, permit.generation,
		permit.recoveryDigest, permit.recoveryTail, permit.cursor, permit.selection, permit.receipt,
		EvidenceRecord{AmbiguousResolution: cloneAmbiguousResolutionPointer(&permit.resolution)}, permit.evidenceBoundary,
		permit.admissionPermitCanonical,
	)
}

func runnerLedgerAmbiguousResolutionWriterPermitDigestInputs(permit *runnerLedgerAmbiguousResolutionWriterPermit) bool {
	return permit != nil && permit.self == permit && permit.binder != nil && permit.candidateBinding != nil &&
		permit.generation.owner == permit.candidateBinding.owner && permit.recoveryDigest != ([32]byte{}) &&
		permit.recoveryTail.Validate() == nil && permit.cursor.Valid() && sameGenerationIdentity(permit.cursor.generation, permit.generation) &&
		permit.selection.action == generatedRunnerLedgerRecoveryProfiles[3].action && runnerLedgerReconciliationSelection(permit.selection) &&
		validRunnerLedgerReconciliationClosedReceipt(permit.receipt, permit.selection) && permit.resolution.Validate() == nil &&
		permit.consumerFactSubject.Validate() == nil && permit.evidenceBoundary != ([32]byte{}) &&
		permit.admissionPermitCanonical != ([32]byte{}) && permit.consumed != nil && !permit.consumed.Load()
}

func runnerLedgerReconciliationWriterPermitDigest(domain string, candidate *verifiedEvidenceRunBinding, generation generationIdentity, recoveryDigest [32]byte, recoveryTail Digest, cursor JournalCursor, selection runnerLedgerRecoveryAdmissionSelection, receipt *runnerLedgerReconciliationClosedReceipt, record EvidenceRecord, evidenceBoundary, admissionCanonical [32]byte) [32]byte {
	if domain == "" || candidate == nil || candidate.canonical == ([32]byte{}) || generation.owner != candidate.owner ||
		recoveryDigest == ([32]byte{}) || recoveryTail.Validate() != nil || !cursor.Valid() || !sameGenerationIdentity(cursor.generation, generation) ||
		!runnerLedgerReconciliationSelection(selection) || !validRunnerLedgerReconciliationClosedReceipt(receipt, selection) ||
		validateEvidenceRecord(record) != nil || evidenceBoundary == ([32]byte{}) || admissionCanonical == ([32]byte{}) {
		return [32]byte{}
	}
	recordCanonical, err := canonicalContractKey(record)
	if err != nil || recordCanonical == "" {
		return [32]byte{}
	}
	h := sha256.New()
	h.Write([]byte(domain + "\x00"))
	h.Write(candidate.canonical[:])
	h.Write(recoveryDigest[:])
	h.Write(evidenceBoundary[:])
	h.Write(admissionCanonical[:])
	h.Write(receipt.canonical[:])
	h.Write(selection.planDigest[:])
	for _, value := range runnerLedgerReconciliationWriterIdentityStrings(selection.profileIndex) {
		writeAdmissionString(h, value)
	}
	for _, value := range []string{
		generation.executionLineageDigest.String(), generation.journalIdentityDigest.String(),
		generation.runnerProjectionDecisionDigest.String(), generation.schemaBundleDigest.String(),
		recoveryTail.String(), string(selection.action), string(selection.recoveryState), string(selection.recoveryAction),
		selection.migrationID, selection.entryDigest.String(), receipt.classification.subjectDigest.String(), recordCanonical,
	} {
		writeAdmissionString(h, value)
	}
	writeAdmissionUint(h, uint64(selection.profileIndex))
	writeAdmissionUint(h, uint64(selection.entryIndex))
	writeAdmissionUint(h, uint64(selection.attemptIndex))
	writeAdmissionUint(h, uint64(selection.maxAttempts))
	writeAdmissionUint(h, uint64(selection.planCount))
	writeGenerationJournalCursor(h, cursor)
	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result
}

func runnerLedgerReconciliationWriterIdentityStrings(profileIndex uint8) []string {
	if profileIndex != 2 && profileIndex != 3 {
		return nil
	}
	common := generatedRunnerLedgerRecoveryProfiles[0]
	writer := generatedRunnerLedgerRecoveryProfiles[profileIndex]
	return []string{
		common.registryID, common.registryDigest, common.profileID, common.profileDigest, common.stateMachineDigest, common.policyDigest,
		writer.registryID, writer.registryDigest, writer.profileID, writer.profileDigest, writer.stateMachineDigest, writer.policyDigest,
		writer.predecessor.registryID, writer.predecessor.registryDigest, writer.predecessor.profileID,
		writer.predecessor.profileDigest, writer.predecessor.stateMachineDigest, writer.predecessor.policyDigest,
	}
}

func consumeRunnerLedgerCommitObservationWriterPermit(permit *runnerLedgerCommitObservationWriterPermit, binder runnerLedgerCommitObservationRecordBinder) (runnerLedgerCommitObservationWriterClaim, error) {
	var claim runnerLedgerCommitObservationWriterClaim
	if permit == nil || permit.self != permit {
		return claim, fail(CodeEvidenceRecoveryRequired, "runner-ledger-recovery-commit-observation-permit", "commit-observation writer permit is unavailable", nil)
	}
	registered, loaded := runnerLedgerCommitObservationWriterPermitRegistry.LoadAndDelete(permit)
	record, ok := registered.(runnerLedgerCommitObservationWriterPermitRecord)
	valid := loaded && ok && record.permit == permit && sameRunnerOwnedPointer(record.binder, binder) &&
		record.candidateBinding == permit.candidateBinding && record.cursorValid == permit.cursor.valid &&
		record.consumed == permit.consumed && record.canonical == permit.canonical &&
		validRunnerLedgerCommitObservationWriterPermitWithoutRegistry(permit)
	if valid {
		valid = permit.consumed.CompareAndSwap(false, true)
	}
	if valid {
		claim = runnerLedgerCommitObservationWriterClaim{
			binder: binder, candidateBinding: permit.candidateBinding, generation: permit.generation,
			recoveryDigest: permit.recoveryDigest, recoveryTail: permit.recoveryTail, cursor: permit.cursor.clone(),
			selection: permit.selection, receipt: permit.receipt, terminal: cloneProjectionValue(permit.terminal), canonical: permit.canonical,
		}
	}
	permit.binder = nil
	permit.use = nil
	permit.receipt = nil
	if !valid {
		return runnerLedgerCommitObservationWriterClaim{}, fail(CodeEvidenceRecoveryRequired, "runner-ledger-recovery-commit-observation-permit", "commit-observation writer permit changed or was already consumed", nil)
	}
	return claim, nil
}

func consumeRunnerLedgerAmbiguousResolutionWriterPermit(permit *runnerLedgerAmbiguousResolutionWriterPermit, binder runnerLedgerAmbiguousResolutionRecordBinder) (runnerLedgerAmbiguousResolutionWriterClaim, error) {
	var claim runnerLedgerAmbiguousResolutionWriterClaim
	if permit == nil || permit.self != permit {
		return claim, fail(CodeEvidenceRecoveryRequired, "runner-ledger-recovery-ambiguous-resolution-permit", "ambiguous-resolution writer permit is unavailable", nil)
	}
	registered, loaded := runnerLedgerAmbiguousResolutionWriterPermitRegistry.LoadAndDelete(permit)
	record, ok := registered.(runnerLedgerAmbiguousResolutionWriterPermitRecord)
	valid := loaded && ok && record.permit == permit && sameRunnerOwnedPointer(record.binder, binder) &&
		record.candidateBinding == permit.candidateBinding && record.cursorValid == permit.cursor.valid &&
		record.consumed == permit.consumed && record.canonical == permit.canonical &&
		validRunnerLedgerAmbiguousResolutionWriterPermitWithoutRegistry(permit)
	if valid {
		valid = permit.consumed.CompareAndSwap(false, true)
	}
	if valid {
		claim = runnerLedgerAmbiguousResolutionWriterClaim{
			binder: binder, candidateBinding: permit.candidateBinding, generation: permit.generation,
			recoveryDigest: permit.recoveryDigest, recoveryTail: permit.recoveryTail, cursor: permit.cursor.clone(),
			selection: permit.selection, receipt: permit.receipt, resolution: cloneProjectionValue(permit.resolution), canonical: permit.canonical,
		}
	}
	permit.binder = nil
	permit.use = nil
	permit.receipt = nil
	if !valid {
		return runnerLedgerAmbiguousResolutionWriterClaim{}, fail(CodeEvidenceRecoveryRequired, "runner-ledger-recovery-ambiguous-resolution-permit", "ambiguous-resolution writer permit changed or was already consumed", nil)
	}
	return claim, nil
}

func cloneAmbiguousResolutionPointer(value *AmbiguousResolutionState) *AmbiguousResolutionState {
	if value == nil {
		return nil
	}
	owned := cloneProjectionValue(*value)
	return &owned
}

func buildRunnerLedgerReconciliationTerminal(snapshot *RecoverySnapshot, facts *runnerLedgerReconciliationFacts, selection runnerLedgerRecoveryAdmissionSelection, postgresMajor uint16) (AttemptTerminalState, error) {
	var terminal AttemptTerminalState
	if snapshot == nil || snapshot.state != RecoveryDanglingCommitIntent || snapshot.nextPermittedAction != RecoveryReconcileCommit ||
		snapshot.lastStatementIntent == nil || snapshot.lastIntermediateEvidence == nil || snapshot.commitIntent == nil ||
		snapshot.lastIntermediateStateDigest == nil || snapshot.lastCommitIntentRecordDigest == nil ||
		snapshot.lastTerminal != nil || snapshot.lastResolution != nil || !runnerLedgerReconciliationSelection(selection) ||
		selection.action != generatedRunnerLedgerRecoveryProfiles[2].action || !validRunnerLedgerReconciliationFacts(facts) ||
		facts.state != RecoveryDanglingCommitIntent || postgresMajor < 15 || postgresMajor > 17 {
		return terminal, fail(CodeEvidenceRecoveryRequired, "runner-ledger-recovery-commit-observation-record", "commit-observation recovery inputs are unavailable", nil)
	}
	intent := snapshot.lastStatementIntent.value
	intermediate := snapshot.lastIntermediateEvidence.value
	commit := snapshot.commitIntent.value
	if intent.Validate() != nil || intermediate.Validate() != nil || commit.Validate() != nil ||
		commit.MigrationID != selection.migrationID || commit.AttemptIndex != selection.attemptIndex ||
		commit.MigrationID != facts.migrationID || commit.AttemptIndex != facts.attemptIndex ||
		commit.LastIntermediateStateDigest != intermediate.State.IntermediateStateDigest ||
		commit.LastIntermediateStateDigest != *snapshot.lastIntermediateStateDigest ||
		commit.AttemptPredecessorCatalogDigest != facts.pendingCatalogDigest ||
		intermediate.PreledgerCatalogResult == nil || intermediate.PreledgerCatalogResult.Digest != facts.committedCatalogDigest ||
		*snapshot.lastCommitIntentRecordDigest != facts.commitRecordDigest || !runnerCanonicalEqual(commit.LedgerRow, facts.expectedLedgerRow) {
		return terminal, fail(CodeEvidenceJournalCorrupt, "runner-ledger-recovery-commit-observation-record", "durable commit intent differs from the closed reconciliation receipt", nil)
	}
	outcome := ""
	reconcile := string(facts.outcome)
	switch facts.outcome {
	case runnerLedgerReconciliationExactCommitted:
		outcome = "ambiguous_reconciled_committed"
	case runnerLedgerReconciliationExactPending:
		outcome = "ambiguous_reconciled_pending"
	case runnerLedgerReconciliationDivergent:
		outcome = "ambiguous_divergent"
	default:
		return terminal, fail(CodeEvidenceRecoveryRequired, "runner-ledger-recovery-commit-observation-record", "reconciliation outcome is unsupported", nil)
	}
	stableCode := string(CodeAmbiguousCommit)
	failure := StableFailureEvidence{
		Code: CodeAmbiguousCommit, Phase: "reconcile", Path: "transaction", Major: &postgresMajor, Retryable: false,
	}
	terminal = AttemptTerminalState{
		SchemaBundleDigest: commit.SchemaBundleDigest, CatalogContractDigest: commit.CatalogContractDigest,
		AuthorityProfileDigest: commit.AuthorityProfileDigest, AuthorityBindingDigest: commit.AuthorityBindingDigest,
		MigrationID: commit.MigrationID, AttemptIndex: commit.AttemptIndex,
		PreviousAttemptTerminalDigest: cloneDigestPointer(commit.PreviousAttemptTerminalDigest),
		LastIntermediateStateDigest:   digestPointer(commit.LastIntermediateStateDigest),
		Outcome:                       outcome, StableErrorCode: &stableCode, FailureEvidence: &failure,
		ReconcileResult: reconcile,
	}
	var err error
	terminal.TerminalDigest, err = terminal.ComputeDigest()
	if err != nil || terminal.Validate(selection.maxAttempts) != nil {
		return AttemptTerminalState{}, fail(CodeEvidenceRecoveryRequired, "runner-ledger-recovery-commit-observation-record", "commit-observation terminal could not be reproduced", err)
	}
	return terminal, nil
}

func buildRunnerLedgerAmbiguousResolution(snapshot *RecoverySnapshot, facts *runnerLedgerReconciliationFacts, selection runnerLedgerRecoveryAdmissionSelection) (AmbiguousResolutionState, error) {
	var resolution AmbiguousResolutionState
	if snapshot == nil || snapshot.state != RecoveryAmbiguousUnresolved || snapshot.nextPermittedAction != RecoveryReconcileCommit ||
		snapshot.commitIntent == nil || snapshot.lastCommitIntentRecordDigest == nil || snapshot.lastTerminal == nil ||
		snapshot.lastTerminalDigest == nil || snapshot.lastResolution != nil || snapshot.lastResolutionDigest != nil ||
		!runnerLedgerReconciliationSelection(selection) || selection.action != generatedRunnerLedgerRecoveryProfiles[3].action ||
		!validRunnerLedgerReconciliationFacts(facts) || facts.state != RecoveryAmbiguousUnresolved ||
		facts.unresolvedTerminalDigest == nil || facts.unresolvedTerminalRecordDigest == nil {
		return resolution, fail(CodeEvidenceRecoveryRequired, "runner-ledger-recovery-ambiguous-resolution-record", "ambiguous-resolution recovery inputs are unavailable", nil)
	}
	commit := snapshot.commitIntent.value
	terminal := snapshot.lastTerminal.value
	if commit.Validate() != nil || terminal.Validate() != nil || terminal.Outcome != "ambiguous_unresolved" ||
		terminal.StableErrorCode == nil || terminal.MigrationID != selection.migrationID || terminal.AttemptIndex != selection.attemptIndex ||
		terminal.TerminalDigest != *snapshot.lastTerminalDigest || terminal.TerminalDigest != *facts.unresolvedTerminalDigest ||
		snapshot.lastTerminal.recordDigest != *facts.unresolvedTerminalRecordDigest ||
		commit.MigrationID != terminal.MigrationID || commit.AttemptIndex != terminal.AttemptIndex ||
		*snapshot.lastCommitIntentRecordDigest != facts.commitRecordDigest || !runnerCanonicalEqual(commit.LedgerRow, facts.expectedLedgerRow) {
		return resolution, fail(CodeEvidenceJournalCorrupt, "runner-ledger-recovery-ambiguous-resolution-record", "unresolved terminal differs from the closed reconciliation receipt", nil)
	}
	outcome := ""
	switch facts.outcome {
	case runnerLedgerReconciliationExactCommitted:
		outcome = "resolved_committed"
	case runnerLedgerReconciliationExactPending:
		outcome = "resolved_pending"
	case runnerLedgerReconciliationDivergent:
		outcome = "resolved_divergent"
	default:
		return resolution, fail(CodeEvidenceRecoveryRequired, "runner-ledger-recovery-ambiguous-resolution-record", "reconciliation outcome is unsupported", nil)
	}
	resolution = AmbiguousResolutionState{
		SchemaBundleDigest: terminal.SchemaBundleDigest, CatalogContractDigest: terminal.CatalogContractDigest,
		AuthorityProfileDigest: terminal.AuthorityProfileDigest, AuthorityBindingDigest: terminal.AuthorityBindingDigest,
		MigrationID: terminal.MigrationID, AttemptIndex: terminal.AttemptIndex,
		UnresolvedTerminalDigest: terminal.TerminalDigest, Outcome: outcome,
		ReconcileResult: string(facts.outcome), StableErrorCode: ErrorCode(*terminal.StableErrorCode),
	}
	var err error
	resolution.ResolutionDigest, err = resolution.ComputeDigest()
	if err != nil || resolution.Validate() != nil {
		return AmbiguousResolutionState{}, fail(CodeEvidenceRecoveryRequired, "runner-ledger-recovery-ambiguous-resolution-record", "ambiguous resolution could not be reproduced", err)
	}
	return resolution, nil
}

func (runner *Runner) appendRunnerLedgerRecoveryCommitObservation(ctx context.Context, admission *runnerLedgerCommitObservationAdmissionPermit, bundle *RuntimeBundle, _ []StatementPlan) error {
	if runner == nil || ctx == nil {
		return fail(CodeEvidenceRecoveryRequired, "runner-ledger-recovery-commit-observation", "commit-observation writer context is unavailable", nil)
	}
	if err := contextAdmissionError(ctx); err != nil {
		return err
	}
	seed, err := claimRunnerLedgerCommitObservationAdmissionPermit(admission)
	if err != nil {
		return err
	}
	receipt, err := runner.revalidateAndCloseRunnerLedgerReconciliationAdmission(ctx, seed, bundle)
	if err != nil {
		return err
	}
	permit, err := mintRunnerLedgerCommitObservationWriterPermit(seed, receipt)
	if err != nil {
		return err
	}
	prior := seed.binder.RecoverySnapshot()
	if !runnerLedgerReconciliationRecoveryBoundary(prior, seed.generation, seed.recoveryDigest, seed.recoveryTail, seed.selection, receipt.classification) {
		revokeRunnerLedgerReconciliationCursor(permit.cursor)
		return fail(CodeEvidenceRecoveryRequired, "runner-ledger-recovery-commit-observation-bind", "commit-observation recovery boundary changed before evidence binding", nil)
	}
	expected := cloneProjectionValue(permit.terminal)
	journal, cursor, owned, err := permit.binder.bindRunnerLedgerRecoveryCommitObservationRecord(ctx, permit)
	if err != nil {
		return mapRunnerLedgerRecoveryReconciliationError(err, "runner-ledger-recovery-commit-observation-bind", "commit-observation record could not be bound")
	}
	if !validRunnerLedgerCommitObservationBoundRecord(seed.binder.Journal(), journal, cursor, owned, permit, expected) {
		revokeRunnerLedgerReconciliationCursor(permit.cursor)
		revokeRunnerLedgerReconciliationCursor(cursor)
		return fail(CodeEvidenceRecoveryRequired, "runner-ledger-recovery-commit-observation-bind", "commit-observation binder returned foreign evidence authority", nil)
	}
	oldCursor := cursor.clone()
	result, appendErr := journal.AppendDurable(ctx, cursor, owned)
	if appendErr != nil {
		return runnerLedgerRecoveryReconciliationAppendFailure(oldCursor, result, appendErr, "commit-observation")
	}
	nextCursor, recordDigest, err := validateRunnerLedgerRecoveryReconciliationAppendResult(
		oldCursor, seed.generation, EvidenceRecord{AttemptTerminal: cloneAttemptTerminalPointer(&expected)}, EvidenceRecordAttemptTerminal, result,
	)
	if err != nil {
		return err
	}
	snapshot := seed.binder.RecoverySnapshot()
	if !sameRunnerOwnedPointer(seed.binder.Journal(), journal) ||
		!runnerLedgerRecoveryCommitObservationSnapshotMatches(snapshot, prior, seed.generation, nextCursor, recordDigest, expected, seed.selection, receipt.classification) {
		revokeRunnerLedgerReconciliationCursor(nextCursor)
		return fail(CodeEvidenceRecoveryRequired, "runner-ledger-recovery-commit-observation-result", "durable commit-observation terminal differs from the exact recovery boundary", nil)
	}
	return nil
}

func (runner *Runner) appendRunnerLedgerRecoveryAmbiguousResolution(ctx context.Context, admission *runnerLedgerAmbiguousResolutionAdmissionPermit, bundle *RuntimeBundle, _ []StatementPlan) error {
	if runner == nil || ctx == nil {
		return fail(CodeEvidenceRecoveryRequired, "runner-ledger-recovery-ambiguous-resolution", "ambiguous-resolution writer context is unavailable", nil)
	}
	if err := contextAdmissionError(ctx); err != nil {
		return err
	}
	seed, err := claimRunnerLedgerAmbiguousResolutionAdmissionPermit(admission)
	if err != nil {
		return err
	}
	receipt, err := runner.revalidateAndCloseRunnerLedgerReconciliationAdmission(ctx, seed, bundle)
	if err != nil {
		return err
	}
	permit, err := mintRunnerLedgerAmbiguousResolutionWriterPermit(seed, receipt)
	if err != nil {
		return err
	}
	prior := seed.binder.RecoverySnapshot()
	if !runnerLedgerReconciliationRecoveryBoundary(prior, seed.generation, seed.recoveryDigest, seed.recoveryTail, seed.selection, receipt.classification) {
		revokeRunnerLedgerReconciliationCursor(permit.cursor)
		return fail(CodeEvidenceRecoveryRequired, "runner-ledger-recovery-ambiguous-resolution-bind", "ambiguous-resolution recovery boundary changed before evidence binding", nil)
	}
	expected := cloneProjectionValue(permit.resolution)
	journal, cursor, owned, err := permit.binder.bindRunnerLedgerRecoveryAmbiguousResolutionRecord(ctx, permit)
	if err != nil {
		return mapRunnerLedgerRecoveryReconciliationError(err, "runner-ledger-recovery-ambiguous-resolution-bind", "ambiguous-resolution record could not be bound")
	}
	if !validRunnerLedgerAmbiguousResolutionBoundRecord(seed.binder.Journal(), journal, cursor, owned, permit, expected) {
		revokeRunnerLedgerReconciliationCursor(permit.cursor)
		revokeRunnerLedgerReconciliationCursor(cursor)
		return fail(CodeEvidenceRecoveryRequired, "runner-ledger-recovery-ambiguous-resolution-bind", "ambiguous-resolution binder returned foreign evidence authority", nil)
	}
	oldCursor := cursor.clone()
	result, appendErr := journal.AppendDurable(ctx, cursor, owned)
	if appendErr != nil {
		return runnerLedgerRecoveryReconciliationAppendFailure(oldCursor, result, appendErr, "ambiguous-resolution")
	}
	nextCursor, recordDigest, err := validateRunnerLedgerRecoveryReconciliationAppendResult(
		oldCursor, seed.generation, EvidenceRecord{AmbiguousResolution: cloneAmbiguousResolutionPointer(&expected)}, EvidenceRecordAmbiguousResolution, result,
	)
	if err != nil {
		return err
	}
	snapshot := seed.binder.RecoverySnapshot()
	if !sameRunnerOwnedPointer(seed.binder.Journal(), journal) ||
		!runnerLedgerRecoveryAmbiguousResolutionSnapshotMatches(snapshot, prior, seed.generation, nextCursor, recordDigest, expected, seed.selection, receipt.classification) {
		revokeRunnerLedgerReconciliationCursor(nextCursor)
		return fail(CodeEvidenceRecoveryRequired, "runner-ledger-recovery-ambiguous-resolution-result", "durable ambiguous resolution differs from the exact recovery boundary", nil)
	}
	return nil
}

func validRunnerLedgerCommitObservationBoundRecord(currentJournal, journal EvidenceJournal, cursor JournalCursor, owned *OwnedEvidenceRecord, permit *runnerLedgerCommitObservationWriterPermit, terminal AttemptTerminalState) bool {
	return currentJournal != nil && journal != nil && sameRunnerOwnedPointer(currentJournal, journal) && owned != nil &&
		owned.consumed != nil && !owned.consumed.Load() && permit != nil && sameCursorIdentity(cursor, permit.cursor) &&
		sameGenerationIdentity(owned.generation, permit.generation) && sameCursorIdentity(owned.cursor, cursor) &&
		owned.witness != nil && owned.witness.kind() == EvidenceRecordAttemptTerminal &&
		sameGenerationIdentity(owned.witness.generationIdentity(), permit.generation) && sameCursorIdentity(owned.witness.cursorIdentity(), cursor) &&
		owned.wire.AttemptTerminal != nil && owned.wire.StatementIntent == nil && owned.wire.Intermediate == nil &&
		owned.wire.CommitIntent == nil && owned.wire.AmbiguousResolution == nil && owned.wire.Header == nil &&
		owned.wire.AttemptTerminal.Validate(permit.selection.maxAttempts) == nil && runnerCanonicalEqual(*owned.wire.AttemptTerminal, terminal)
}

func validRunnerLedgerAmbiguousResolutionBoundRecord(currentJournal, journal EvidenceJournal, cursor JournalCursor, owned *OwnedEvidenceRecord, permit *runnerLedgerAmbiguousResolutionWriterPermit, resolution AmbiguousResolutionState) bool {
	return currentJournal != nil && journal != nil && sameRunnerOwnedPointer(currentJournal, journal) && owned != nil &&
		owned.consumed != nil && !owned.consumed.Load() && permit != nil && sameCursorIdentity(cursor, permit.cursor) &&
		sameGenerationIdentity(owned.generation, permit.generation) && sameCursorIdentity(owned.cursor, cursor) &&
		owned.witness != nil && owned.witness.kind() == EvidenceRecordAmbiguousResolution &&
		sameGenerationIdentity(owned.witness.generationIdentity(), permit.generation) && sameCursorIdentity(owned.witness.cursorIdentity(), cursor) &&
		owned.wire.AmbiguousResolution != nil && owned.wire.StatementIntent == nil && owned.wire.Intermediate == nil &&
		owned.wire.CommitIntent == nil && owned.wire.AttemptTerminal == nil && owned.wire.Header == nil &&
		owned.wire.AmbiguousResolution.Validate() == nil && runnerCanonicalEqual(*owned.wire.AmbiguousResolution, resolution)
}

func validateRunnerLedgerRecoveryReconciliationAppendResult(cursor JournalCursor, generation generationIdentity, record EvidenceRecord, kind EvidenceRecordKind, result AppendResult) (JournalCursor, Digest, error) {
	failResult := func(message string) (JournalCursor, Digest, error) {
		revokeRunnerLedgerReconciliationCursor(cursor)
		if result.durableCursor != nil {
			revokeRunnerLedgerReconciliationCursor(*result.durableCursor)
		}
		return JournalCursor{}, "", fail(CodeEvidenceRecoveryRequired, "runner-ledger-reconciliation-append-result", message, nil)
	}
	if !stringIn(string(kind), string(EvidenceRecordAttemptTerminal), string(EvidenceRecordAmbiguousResolution)) ||
		result.outcome != appendOutcomeDurable || result.durableCursor == nil || !result.durableCursor.Valid() || cursor.Valid() ||
		!sameGenerationIdentity(result.durableCursor.generation, generation) || result.candidateRecordDigest.Validate() != nil ||
		result.candidateCheckpointRecordDigest.Validate() != nil || result.candidatePreviousRecordDigest == nil {
		return failResult("durable reconciliation append result is unavailable or contradictory")
	}
	rotated := result.rotationHeaderRecordDigest != nil || result.rotationHeaderCheckpointRecordDigest != nil
	if (result.rotationHeaderRecordDigest == nil) != (result.rotationHeaderCheckpointRecordDigest == nil) {
		return failResult("reconciliation rotation diagnosis is one-sided")
	}
	wantSequence := cursor.nextSequence
	wantSegment := cursor.segmentIndex
	wantNextSequence := cursor.nextSequence + 1
	wantIndexNext := cursor.lineageIndexNextSequence + 1
	wantPrevious := cloneDigestPointer(cursor.previousRecordDigest)
	if rotated {
		if result.rotationHeaderRecordDigest.Validate() != nil || result.rotationHeaderCheckpointRecordDigest.Validate() != nil {
			return failResult("reconciliation rotation identity is invalid")
		}
		wantSequence++
		wantSegment++
		wantNextSequence++
		wantIndexNext++
		wantPrevious = cloneDigestPointer(result.rotationHeaderRecordDigest)
	}
	if result.candidateSequence != wantSequence || !equalDigestPointer(result.candidatePreviousRecordDigest, wantPrevious) ||
		result.durableCursor.segmentIndex != wantSegment || result.durableCursor.nextSequence != wantNextSequence ||
		result.durableCursor.lineageIndexNextSequence != wantIndexNext || result.durableCursor.previousRecordDigest == nil ||
		*result.durableCursor.previousRecordDigest != result.candidateRecordDigest || result.durableCursor.latestCheckpointRecordDigest == nil ||
		*result.durableCursor.latestCheckpointRecordDigest != result.candidateCheckpointRecordDigest ||
		result.durableCursor.lineageIndexPreviousRecordDigest != result.candidateCheckpointRecordDigest || result.durableCursor.valid == cursor.valid {
		return failResult("durable reconciliation cursor does not describe the exact composite append")
	}
	frame := EvidenceFrame{
		FormatVersion: EvidenceFrameFormat, Sequence: result.candidateSequence,
		PreviousRecordDigest: cloneDigestPointer(result.candidatePreviousRecordDigest), RecordKind: kind, Record: cloneEvidenceRecord(record),
	}
	computed, err := frame.ComputeDigest()
	frame.RecordDigest = computed
	if err != nil || frame.Validate() != nil || computed != result.candidateRecordDigest {
		return failResult("reconciliation record digest differs from the exact durable frame")
	}
	return result.durableCursor.clone(), result.candidateRecordDigest, nil
}

func runnerLedgerRecoveryReconciliationAppendFailure(cursor JournalCursor, result AppendResult, appendErr error, action string) error {
	if !cursor.Valid() || result.outcome != "" || result.durableCursor != nil || result.candidateSequence != 0 ||
		result.candidatePreviousRecordDigest != nil || result.candidateRecordDigest != "" || result.candidateCheckpointRecordDigest != "" ||
		result.rotationHeaderRecordDigest != nil || result.rotationHeaderCheckpointRecordDigest != nil {
		revokeRunnerLedgerReconciliationCursor(cursor)
		if result.durableCursor != nil {
			revokeRunnerLedgerReconciliationCursor(*result.durableCursor)
		}
		return fail(CodeEvidenceRecoveryRequired, "runner-ledger-reconciliation-append", action+" mutation outcome requires strict reopen", nil)
	}
	if errors.Is(appendErr, context.Canceled) {
		return fail(CodeContextCanceled, "runner-ledger-reconciliation-append", action+" append was canceled before mutation", nil)
	}
	if errors.Is(appendErr, context.DeadlineExceeded) {
		return fail(CodeDeadlineExceeded, "runner-ledger-reconciliation-append", action+" append deadline expired before mutation", nil)
	}
	var stable *Error
	if errors.As(appendErr, &stable) {
		return fail(stable.Code, "runner-ledger-reconciliation-append", action+" append failed before mutation", nil)
	}
	return fail(CodeEvidenceJournalFailed, "runner-ledger-reconciliation-append", action+" append failed before mutation", nil)
}

func mapRunnerLedgerRecoveryReconciliationError(err error, op, message string) error {
	var stable *Error
	if errors.As(err, &stable) {
		return fail(stable.Code, op, message, nil)
	}
	if errors.Is(err, context.Canceled) {
		return fail(CodeContextCanceled, op, message, nil)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return fail(CodeDeadlineExceeded, op, message, nil)
	}
	return fail(CodeEvidenceJournalFailed, op, message, nil)
}

func revokeRunnerLedgerReconciliationCursor(cursor JournalCursor) {
	if cursor.valid != nil {
		cursor.valid.Store(false)
	}
}

func runnerLedgerRecoveryCommitObservationSnapshotMatches(snapshot, prior *RecoverySnapshot, generation generationIdentity, cursor JournalCursor, recordDigest Digest, terminal AttemptTerminalState, selection runnerLedgerRecoveryAdmissionSelection, facts *runnerLedgerReconciliationFacts) bool {
	if !runnerLedgerReconciliationPriorPrefixMatches(snapshot, prior, generation, cursor, recordDigest, selection, facts) ||
		prior.state != RecoveryDanglingCommitIntent || prior.lastTerminal != nil || prior.lastResolution != nil ||
		snapshot.lastTerminal == nil || snapshot.lastTerminalDigest == nil || *snapshot.lastTerminalDigest != terminal.TerminalDigest ||
		snapshot.lastTerminal.recordDigest != recordDigest || snapshot.lastTerminal.tailDigest != recordDigest ||
		snapshot.lastTerminal.owner != generation.owner || !sameGenerationIdentity(snapshot.lastTerminal.generation, generation) ||
		!sameCursorIdentity(snapshot.lastTerminal.cursor, cursor) || !runnerCanonicalEqual(snapshot.lastTerminal.value, terminal) ||
		snapshot.lastResolution != nil || snapshot.lastResolutionDigest != nil || snapshot.lineageContinuation != nil ||
		!equalDigestPointer(snapshot.previousAttemptTerminalDigest, terminal.PreviousAttemptTerminalDigest) {
		return false
	}
	switch facts.outcome {
	case runnerLedgerReconciliationExactCommitted:
		return terminal.Outcome == "ambiguous_reconciled_committed" &&
			(snapshot.state == RecoveryCompleted && snapshot.nextPermittedAction == RecoveryReturnSuccess ||
				snapshot.state == RecoveryTerminal && snapshot.nextPermittedAction == RecoveryBeginFirstAttemptNextEntry)
	case runnerLedgerReconciliationExactPending:
		want := RecoveryReturnFailure
		if selection.attemptIndex < selection.maxAttempts {
			want = RecoveryBeginNextAttempt
		}
		return terminal.Outcome == "ambiguous_reconciled_pending" && snapshot.state == RecoveryTerminal && snapshot.nextPermittedAction == want
	case runnerLedgerReconciliationDivergent:
		return terminal.Outcome == "ambiguous_divergent" && snapshot.state == RecoveryDivergent && snapshot.nextPermittedAction == RecoveryReturnFailure
	default:
		return false
	}
}

func runnerLedgerRecoveryAmbiguousResolutionSnapshotMatches(snapshot, prior *RecoverySnapshot, generation generationIdentity, cursor JournalCursor, recordDigest Digest, resolution AmbiguousResolutionState, selection runnerLedgerRecoveryAdmissionSelection, facts *runnerLedgerReconciliationFacts) bool {
	if !runnerLedgerReconciliationPriorPrefixMatches(snapshot, prior, generation, cursor, recordDigest, selection, facts) ||
		prior.state != RecoveryAmbiguousUnresolved || prior.lastTerminal == nil || prior.lastTerminalDigest == nil || prior.lastResolution != nil ||
		snapshot.lastTerminal == nil || snapshot.lastTerminalDigest == nil || *snapshot.lastTerminalDigest != *prior.lastTerminalDigest ||
		snapshot.lastTerminal.recordDigest != prior.lastTerminal.recordDigest || snapshot.lastTerminal.tailDigest != recordDigest ||
		snapshot.lastTerminal.owner != generation.owner || !sameGenerationIdentity(snapshot.lastTerminal.generation, generation) ||
		!sameCursorIdentity(snapshot.lastTerminal.cursor, cursor) || !runnerCanonicalEqual(snapshot.lastTerminal.value, prior.lastTerminal.value) ||
		snapshot.lastResolution == nil || snapshot.lastResolutionDigest == nil || *snapshot.lastResolutionDigest != resolution.ResolutionDigest ||
		snapshot.lastResolution.recordDigest != recordDigest || snapshot.lastResolution.tailDigest != recordDigest ||
		snapshot.lastResolution.owner != generation.owner || !sameGenerationIdentity(snapshot.lastResolution.generation, generation) ||
		!sameCursorIdentity(snapshot.lastResolution.cursor, cursor) || !runnerCanonicalEqual(snapshot.lastResolution.value, resolution) ||
		resolution.UnresolvedTerminalDigest != *prior.lastTerminalDigest || snapshot.lineageContinuation != nil ||
		!equalDigestPointer(snapshot.previousAttemptTerminalDigest, prior.previousAttemptTerminalDigest) {
		return false
	}
	switch facts.outcome {
	case runnerLedgerReconciliationExactCommitted:
		return resolution.Outcome == "resolved_committed" &&
			(snapshot.state == RecoveryCompleted && snapshot.nextPermittedAction == RecoveryReturnSuccess ||
				snapshot.state == RecoveryTerminal && snapshot.nextPermittedAction == RecoveryBeginFirstAttemptNextEntry)
	case runnerLedgerReconciliationExactPending:
		want := RecoveryReturnFailure
		if selection.attemptIndex < selection.maxAttempts {
			want = RecoveryBeginNextAttempt
		}
		return resolution.Outcome == "resolved_pending" && snapshot.state == RecoveryTerminal && snapshot.nextPermittedAction == want
	case runnerLedgerReconciliationDivergent:
		return resolution.Outcome == "resolved_divergent" && snapshot.state == RecoveryDivergent && snapshot.nextPermittedAction == RecoveryReturnFailure
	default:
		return false
	}
}

func runnerLedgerReconciliationPriorPrefixMatches(snapshot, prior *RecoverySnapshot, generation generationIdentity, cursor JournalCursor, recordDigest Digest, selection runnerLedgerRecoveryAdmissionSelection, facts *runnerLedgerReconciliationFacts) bool {
	if snapshot == nil || prior == nil || !validRunnerLedgerReconciliationFacts(facts) ||
		!validRecoverySnapshotForJournal(snapshot, generation, cursor) || !sameCursorIdentity(snapshot.cursor, cursor) ||
		snapshot.tailDigest != recordDigest || snapshot.migrationID == nil || *snapshot.migrationID != selection.migrationID ||
		snapshot.attemptIndex == nil || *snapshot.attemptIndex != selection.attemptIndex ||
		prior.lastStatementIntent == nil || prior.lastStatementIntentRecordDigest == nil ||
		prior.lastIntermediateEvidence == nil || prior.lastIntermediateEvidenceRecordDigest == nil || prior.lastIntermediateStateDigest == nil ||
		prior.commitIntent == nil || prior.lastCommitIntentRecordDigest == nil ||
		snapshot.lastStatementIntent == nil || snapshot.lastStatementIntentRecordDigest == nil ||
		snapshot.lastIntermediateEvidence == nil || snapshot.lastIntermediateEvidenceRecordDigest == nil || snapshot.lastIntermediateStateDigest == nil ||
		snapshot.commitIntent == nil || snapshot.lastCommitIntentRecordDigest == nil ||
		*snapshot.lastStatementIntentRecordDigest != *prior.lastStatementIntentRecordDigest ||
		*snapshot.lastIntermediateEvidenceRecordDigest != *prior.lastIntermediateEvidenceRecordDigest ||
		*snapshot.lastIntermediateStateDigest != *prior.lastIntermediateStateDigest ||
		*snapshot.lastCommitIntentRecordDigest != *prior.lastCommitIntentRecordDigest ||
		*snapshot.lastCommitIntentRecordDigest != facts.commitRecordDigest ||
		!runnerLedgerRecoveredPrefixValueMatches(snapshot.lastStatementIntent.owner, snapshot.lastStatementIntent.generation, snapshot.lastStatementIntent.cursor, snapshot.lastStatementIntent.tailDigest, snapshot.lastStatementIntent.recordDigest, generation, cursor, recordDigest, *prior.lastStatementIntentRecordDigest) ||
		!runnerLedgerRecoveredPrefixValueMatches(snapshot.lastIntermediateEvidence.owner, snapshot.lastIntermediateEvidence.generation, snapshot.lastIntermediateEvidence.cursor, snapshot.lastIntermediateEvidence.tailDigest, snapshot.lastIntermediateEvidence.recordDigest, generation, cursor, recordDigest, *prior.lastIntermediateEvidenceRecordDigest) ||
		!runnerLedgerRecoveredPrefixValueMatches(snapshot.commitIntent.owner, snapshot.commitIntent.generation, snapshot.commitIntent.cursor, snapshot.commitIntent.tailDigest, snapshot.commitIntent.recordDigest, generation, cursor, recordDigest, *prior.lastCommitIntentRecordDigest) ||
		!runnerCanonicalEqual(snapshot.lastStatementIntent.value, prior.lastStatementIntent.value) ||
		!runnerCanonicalEqual(snapshot.lastIntermediateEvidence.value, prior.lastIntermediateEvidence.value) ||
		!runnerCanonicalEqual(snapshot.commitIntent.value, prior.commitIntent.value) {
		return false
	}
	return generationJournalRecoveryDigest(snapshot) != ([32]byte{})
}

func runnerLedgerRecoveredPrefixValueMatches(owner *evidenceOwnerToken, recoveredGeneration generationIdentity, recoveredCursor JournalCursor, tail, record Digest, generation generationIdentity, cursor JournalCursor, wantTail, wantRecord Digest) bool {
	return owner == generation.owner && sameGenerationIdentity(recoveredGeneration, generation) && sameCursorIdentity(recoveredCursor, cursor) &&
		tail == wantTail && record == wantRecord
}

func (s *generationEvidenceSession) bindRunnerLedgerRecoveryCommitObservationRecord(ctx context.Context, permit *runnerLedgerCommitObservationWriterPermit) (EvidenceJournal, JournalCursor, *OwnedEvidenceRecord, error) {
	claimed, err := consumeRunnerLedgerCommitObservationWriterPermit(permit, s)
	if err != nil {
		return nil, JournalCursor{}, nil, err
	}
	if err := contextAdmissionError(ctx); err != nil {
		return nil, JournalCursor{}, nil, err
	}
	if s == nil || s.self != s {
		return nil, JournalCursor{}, nil, fail(CodeEvidenceRecoveryRequired, "runner-ledger-recovery-commit-observation-evidence", "evidence session is unavailable", nil)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.validLocked() || s.candidate.binding != claimed.candidateBinding || s.active.kind != activeGenerationCurrent ||
		s.active.recoveryExecutionBindings != nil || !sameGenerationIdentity(s.active.identity, claimed.generation) {
		return nil, JournalCursor{}, nil, fail(CodeEvidenceRecoveryRequired, "runner-ledger-recovery-commit-observation-evidence", "current same-verifier evidence session changed", nil)
	}
	journal := s.journal
	journal.mu.Lock()
	defer journal.mu.Unlock()
	if !journal.validLocked() || journal.state == nil || journal.state.unknown != nil ||
		!sameCursorIdentity(journal.state.cursor, claimed.cursor) || generationJournalRecoveryDigest(journal.state.recovery) != claimed.recoveryDigest ||
		!runnerLedgerReconciliationRecoveryBoundary(journal.state.recovery, claimed.generation, claimed.recoveryDigest, claimed.recoveryTail, claimed.selection, claimed.receipt.classification) ||
		journal.schema.maxAttempts[claimed.selection.migrationID] != claimed.selection.maxAttempts ||
		int(claimed.selection.entryIndex) >= len(journal.schema.orderedMigrations) ||
		journal.schema.orderedMigrations[claimed.selection.entryIndex] != claimed.selection.migrationID ||
		int(claimed.receipt.classification.targetIndex) >= len(journal.schema.signedExpectedLedgerRows) ||
		!runnerCanonicalEqual(journal.schema.signedExpectedLedgerRows[claimed.receipt.classification.targetIndex], claimed.receipt.classification.expectedLedgerRow) ||
		uint32(len(journal.schema.durableObservedLedgerPrefix)) != claimed.receipt.classification.targetIndex {
		return nil, JournalCursor{}, nil, fail(CodeEvidenceRecoveryRequired, "runner-ledger-recovery-commit-observation-evidence", "current journal boundary changed", nil)
	}
	header, ok := generationJournalHeader(journal)
	if !ok || claimed.terminal.SchemaBundleDigest != header.SchemaBundleDigest ||
		claimed.terminal.AuthorityProfileDigest != header.AuthorityProfileDigest ||
		claimed.terminal.AuthorityBindingDigest != header.AuthorityBindingDigest ||
		claimed.terminal.CatalogContractDigest != journal.state.recovery.commitIntent.value.CatalogContractDigest {
		return nil, JournalCursor{}, nil, fail(CodeEvidenceRecoveryRequired, "runner-ledger-recovery-commit-observation-evidence", "commit-observation terminal header authority changed", nil)
	}
	prefix, err := readRunnerLedgerEntrySuccessPrefixLocked(ctx, journal)
	if err != nil {
		return nil, JournalCursor{}, nil, err
	}
	if len(prefix) == 0 || claimed.cursor.previousRecordDigest == nil || prefix[len(prefix)-1].Record.CommitIntent == nil ||
		prefix[len(prefix)-1].RecordDigest != *claimed.cursor.previousRecordDigest ||
		prefix[len(prefix)-1].RecordDigest != claimed.receipt.classification.commitRecordDigest ||
		prefix[len(prefix)-1].Sequence+1 != claimed.cursor.nextSequence {
		return nil, JournalCursor{}, nil, admissionCorrupt("runner-ledger-recovery-commit-observation-evidence", "stored evidence prefix differs from the current commit cursor", nil)
	}
	chain := cloneRunnerEvidenceChainWitness(journal.schema.chainWitness)
	if chain.ambiguousBoundaries == nil {
		chain.ambiguousBoundaries = map[Digest]ownedAmbiguousBoundaryWitness{}
	}
	if _, exists := chain.ambiguousBoundaries[claimed.terminal.TerminalDigest]; exists ||
		journal.state.recovery.lastIntermediateEvidenceRecordDigest == nil || journal.state.recovery.lastCommitIntentRecordDigest == nil {
		return nil, JournalCursor{}, nil, admissionCorrupt("runner-ledger-recovery-commit-observation-evidence", "commit-observation boundary already exists or is incomplete", nil)
	}
	chain.ambiguousBoundaries[claimed.terminal.TerminalDigest] = ownedAmbiguousBoundaryWitness{
		migrationID: claimed.selection.migrationID, attemptIndex: claimed.selection.attemptIndex, commitCalled: true,
		finalIntermediateRecordDigest: *journal.state.recovery.lastIntermediateEvidenceRecordDigest,
		commitIntentRecordDigest:      *journal.state.recovery.lastCommitIntentRecordDigest,
	}
	witness := ownedAttemptTerminalWitness{
		ownedAppendContext: ownedAppendContext{
			generation: claimed.generation, cursor: claimed.cursor.clone(), prefix: cloneProjectionValue(prefix), chain: chain,
		},
		terminalDigest: claimed.terminal.TerminalDigest, maxAttempts: claimed.selection.maxAttempts,
	}
	owned, err := bindOwnedEvidenceRecord(EvidenceRecord{AttemptTerminal: cloneAttemptTerminalPointer(&claimed.terminal)}, witness)
	if err != nil {
		return nil, JournalCursor{}, nil, admissionCorrupt("runner-ledger-recovery-commit-observation-evidence", "commit-observation terminal record is invalid", err)
	}
	return journal, journal.state.cursor.clone(), owned, nil
}

func (s *generationEvidenceSession) bindRunnerLedgerRecoveryAmbiguousResolutionRecord(ctx context.Context, permit *runnerLedgerAmbiguousResolutionWriterPermit) (EvidenceJournal, JournalCursor, *OwnedEvidenceRecord, error) {
	claimed, err := consumeRunnerLedgerAmbiguousResolutionWriterPermit(permit, s)
	if err != nil {
		return nil, JournalCursor{}, nil, err
	}
	if err := contextAdmissionError(ctx); err != nil {
		return nil, JournalCursor{}, nil, err
	}
	if s == nil || s.self != s {
		return nil, JournalCursor{}, nil, fail(CodeEvidenceRecoveryRequired, "runner-ledger-recovery-ambiguous-resolution-evidence", "evidence session is unavailable", nil)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.validLocked() || s.candidate.binding != claimed.candidateBinding || s.active.kind != activeGenerationCurrent ||
		s.active.recoveryExecutionBindings != nil || !sameGenerationIdentity(s.active.identity, claimed.generation) {
		return nil, JournalCursor{}, nil, fail(CodeEvidenceRecoveryRequired, "runner-ledger-recovery-ambiguous-resolution-evidence", "current same-verifier evidence session changed", nil)
	}
	journal := s.journal
	journal.mu.Lock()
	defer journal.mu.Unlock()
	if !journal.validLocked() || journal.state == nil || journal.state.unknown != nil ||
		!sameCursorIdentity(journal.state.cursor, claimed.cursor) || generationJournalRecoveryDigest(journal.state.recovery) != claimed.recoveryDigest ||
		!runnerLedgerReconciliationRecoveryBoundary(journal.state.recovery, claimed.generation, claimed.recoveryDigest, claimed.recoveryTail, claimed.selection, claimed.receipt.classification) ||
		journal.schema.maxAttempts[claimed.selection.migrationID] != claimed.selection.maxAttempts ||
		int(claimed.selection.entryIndex) >= len(journal.schema.orderedMigrations) ||
		journal.schema.orderedMigrations[claimed.selection.entryIndex] != claimed.selection.migrationID ||
		int(claimed.receipt.classification.targetIndex) >= len(journal.schema.signedExpectedLedgerRows) ||
		!runnerCanonicalEqual(journal.schema.signedExpectedLedgerRows[claimed.receipt.classification.targetIndex], claimed.receipt.classification.expectedLedgerRow) ||
		uint32(len(journal.schema.durableObservedLedgerPrefix)) != claimed.receipt.classification.targetIndex {
		return nil, JournalCursor{}, nil, fail(CodeEvidenceRecoveryRequired, "runner-ledger-recovery-ambiguous-resolution-evidence", "current journal boundary changed", nil)
	}
	header, ok := generationJournalHeader(journal)
	if !ok || claimed.resolution.SchemaBundleDigest != header.SchemaBundleDigest ||
		claimed.resolution.AuthorityProfileDigest != header.AuthorityProfileDigest ||
		claimed.resolution.AuthorityBindingDigest != header.AuthorityBindingDigest {
		return nil, JournalCursor{}, nil, fail(CodeEvidenceRecoveryRequired, "runner-ledger-recovery-ambiguous-resolution-evidence", "ambiguous-resolution header authority changed", nil)
	}
	prefix, err := readRunnerLedgerEntrySuccessPrefixLocked(ctx, journal)
	if err != nil {
		return nil, JournalCursor{}, nil, err
	}
	if len(prefix) == 0 || claimed.cursor.previousRecordDigest == nil || prefix[len(prefix)-1].Record.AttemptTerminal == nil ||
		prefix[len(prefix)-1].RecordDigest != *claimed.cursor.previousRecordDigest ||
		prefix[len(prefix)-1].Record.AttemptTerminal.TerminalDigest != claimed.resolution.UnresolvedTerminalDigest ||
		prefix[len(prefix)-1].Sequence+1 != claimed.cursor.nextSequence {
		return nil, JournalCursor{}, nil, admissionCorrupt("runner-ledger-recovery-ambiguous-resolution-evidence", "stored evidence prefix is not the exact unresolved terminal", nil)
	}
	boundary, exists := journal.schema.chainWitness.ambiguousBoundaries[claimed.resolution.UnresolvedTerminalDigest]
	if !exists || !boundary.commitCalled || boundary.migrationID != claimed.selection.migrationID ||
		boundary.attemptIndex != claimed.selection.attemptIndex || journal.state.recovery.lastIntermediateEvidenceRecordDigest == nil ||
		journal.state.recovery.lastCommitIntentRecordDigest == nil ||
		boundary.finalIntermediateRecordDigest != *journal.state.recovery.lastIntermediateEvidenceRecordDigest ||
		boundary.commitIntentRecordDigest != *journal.state.recovery.lastCommitIntentRecordDigest {
		return nil, JournalCursor{}, nil, admissionCorrupt("runner-ledger-recovery-ambiguous-resolution-evidence", "unresolved terminal lacks its exact ambiguous boundary", nil)
	}
	witness := ownedAmbiguousResolutionWitness{
		ownedAppendContext: ownedAppendContext{
			generation: claimed.generation, cursor: claimed.cursor.clone(), prefix: cloneProjectionValue(prefix),
			chain: cloneRunnerEvidenceChainWitness(journal.schema.chainWitness),
		},
		unresolvedTerminalDigest: claimed.resolution.UnresolvedTerminalDigest,
		priorTerminal:            cloneProjectionValue(prefix[len(prefix)-1]),
	}
	owned, err := bindOwnedEvidenceRecord(EvidenceRecord{AmbiguousResolution: cloneAmbiguousResolutionPointer(&claimed.resolution)}, witness)
	if err != nil {
		return nil, JournalCursor{}, nil, admissionCorrupt("runner-ledger-recovery-ambiguous-resolution-evidence", "ambiguous-resolution record is invalid", err)
	}
	return journal, journal.state.cursor.clone(), owned, nil
}

func (*generationEvidenceSession) runnerLedgerCommitObservationRecordBinderSealed() {}

func (*generationEvidenceSession) runnerLedgerAmbiguousResolutionRecordBinderSealed() {}
