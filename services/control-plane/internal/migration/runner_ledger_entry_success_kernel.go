package migration

import (
	"context"
	"crypto/sha256"
	"errors"
	"sync"
	"sync/atomic"
)

const runnerLedgerEntrySuccessStateDigestDomain = "cloud-agents/runner-ledger-entry-success-writer/state/v1"

const (
	runnerLedgerEntrySuccessExecutionReady           = "execution_ready"
	runnerLedgerEntrySuccessTransactionReady         = "transaction_ready"
	runnerLedgerEntrySuccessStatementReady           = "statement_ready"
	runnerLedgerEntrySuccessIntentDurable            = "intent_durable"
	runnerLedgerEntrySuccessStatementExecuted        = "statement_executed"
	runnerLedgerEntrySuccessIntermediateDurable      = "intermediate_durable"
	runnerLedgerEntrySuccessFinalIntermediateDurable = "final_intermediate_durable"
	runnerLedgerEntrySuccessLedgerReadbackReady      = "ledger_readback_ready"
	runnerLedgerEntrySuccessCommitIntentDurable      = "commit_intent_durable"
	runnerLedgerEntrySuccessCommitKnownCommitted     = "commit_known_committed"
	runnerLedgerEntrySuccessTerminalDurable          = "terminal_durable"
	runnerLedgerEntrySuccessEntryCommittedComplete   = "entry_committed_complete"
	runnerLedgerEntrySuccessEntryCommittedNextEntry  = "entry_committed_next_entry"
)

type runnerLedgerEntrySuccessOutcome struct {
	state        string
	migrationID  string
	ledgerHead   string
	ledgerLength uint32
}

func (outcome runnerLedgerEntrySuccessOutcome) valid() bool {
	return stringIn(outcome.state, runnerLedgerEntrySuccessEntryCommittedComplete, runnerLedgerEntrySuccessEntryCommittedNextEntry) &&
		migrationIDPattern.MatchString(outcome.migrationID) && outcome.ledgerHead == outcome.migrationID && outcome.ledgerLength > 0
}

type runnerLedgerEntrySuccessData struct {
	phase                    string
	session                  DatabaseSession
	transaction              MigrationTransaction
	evidence                 runnerLedgerEntrySuccessEvidenceBinder
	journal                  EvidenceJournal
	use                      *runnerLedgerEntryExecutionAdmissionUseRecord
	key                      int64
	candidateBinding         *verifiedEvidenceRunBinding
	generation               generationIdentity
	consumerFactSubject      Digest
	evidenceBoundary         [32]byte
	recoveryDigest           [32]byte
	recoveryTail             Digest
	database                 runnerPreparedDatabaseIdentity
	selection                runnerLedgerEntryAdmissionSelection
	ledgerBeforeDigest       Digest
	ledgerBeforeHead         string
	ledgerBeforeLength       uint32
	connectedAuthority       Digest
	migrationAuthority       Digest
	projectionSubject        Digest
	observedCatalogContract  *Digest
	observedCatalogDigest    Digest
	bundle                   *RuntimeBundle
	bindings                 RunnerProjectionBindings
	entry                    MigrationEntry
	plans                    []StatementPlan
	maxAttempts              uint32
	cursor                   JournalCursor
	statementIndex           uint32
	transactionAuthority     Digest
	currentCatalogDigest     Digest
	firstCatalogBeforeDigest Digest
	previousIntermediate     *Digest
	authorityBefore          ProjectionResultEvidence
	catalogBefore            ProjectionResultEvidence
	intent                   StatementIntent
	intentRecordDigest       Digest
	authorityAfterProjection AuthorityProjection
	catalogAfterProjection   CatalogStateProjection
	authorityAfter           ProjectionResultEvidence
	catalogAfter             ProjectionResultEvidence
	intermediate             StatementIntermediateEvidence
	intermediateRecordDigest Digest
	ledgerRows               []CommitIntentLedgerRow
	ledgerPrefixDigest       Digest
	ledgerHead               string
	ledgerLength             uint32
	commit                   CommitIntent
	commitRecordDigest       Digest
	commitFacts              runnerCommitProtocolFacts
	terminal                 AttemptTerminalState
	terminalRecordDigest     Digest
	mutationAttempted        bool
}

type runnerLedgerEntrySuccessState struct {
	self      *runnerLedgerEntrySuccessState
	binding   *runnerLedgerEntrySuccessStateBinding
	data      runnerLedgerEntrySuccessData
	canonical [32]byte
	closed    bool
}

type runnerLedgerEntrySuccessStateBinding struct {
	state            *runnerLedgerEntrySuccessState
	session          DatabaseSession
	transaction      MigrationTransaction
	evidence         runnerLedgerEntrySuccessEvidenceBinder
	journal          EvidenceJournal
	candidateBinding *verifiedEvidenceRunBinding
	cursorValid      *atomic.Bool
	canonical        [32]byte
}

type runnerLedgerEntrySuccessStateRegistryRecord struct {
	state     *runnerLedgerEntrySuccessState
	binding   *runnerLedgerEntrySuccessStateBinding
	data      runnerLedgerEntrySuccessData
	canonical [32]byte
	claimed   *atomic.Bool
}

var runnerLedgerEntrySuccessStateRegistry sync.Map
var runnerLedgerEntrySuccessStateCleanupRegistry sync.Map

func (runner *Runner) executeRunnerLedgerEntrySuccess(ctx context.Context, permit *runnerLedgerEntryExecutionPermit, bundle *RuntimeBundle, plans []StatementPlan) (runnerLedgerEntrySuccessOutcome, error) {
	ready, err := runner.prepareRunnerLedgerEntrySuccess(ctx, permit, bundle, plans)
	if err != nil {
		return runnerLedgerEntrySuccessOutcome{}, err
	}
	transaction, err := runner.beginRunnerLedgerEntrySuccessTransaction(ctx, ready)
	if err != nil {
		return runnerLedgerEntrySuccessOutcome{}, err
	}
	state, err := runner.prepareRunnerLedgerEntrySuccessStatement(ctx, transaction)
	if err != nil {
		return runnerLedgerEntrySuccessOutcome{}, err
	}
	for {
		state, err = runner.appendRunnerLedgerEntrySuccessIntent(ctx, state)
		if err != nil {
			return runnerLedgerEntrySuccessOutcome{}, err
		}
		state, err = runner.executeRunnerLedgerEntrySuccessStatement(ctx, state)
		if err != nil {
			return runnerLedgerEntrySuccessOutcome{}, err
		}
		state, err = runner.appendRunnerLedgerEntrySuccessIntermediate(ctx, state)
		if err != nil {
			return runnerLedgerEntrySuccessOutcome{}, err
		}
		if state.data.phase == runnerLedgerEntrySuccessFinalIntermediateDurable {
			break
		}
		state, err = runner.advanceRunnerLedgerEntrySuccessStatement(ctx, state)
		if err != nil {
			return runnerLedgerEntrySuccessOutcome{}, err
		}
	}
	state, err = runner.insertRunnerLedgerEntrySuccessLedger(ctx, state)
	if err != nil {
		return runnerLedgerEntrySuccessOutcome{}, err
	}
	state, err = runner.appendRunnerLedgerEntrySuccessCommitIntent(ctx, state)
	if err != nil {
		return runnerLedgerEntrySuccessOutcome{}, err
	}
	state, err = runner.commitRunnerLedgerEntrySuccess(ctx, state)
	if err != nil {
		return runnerLedgerEntrySuccessOutcome{}, err
	}
	state, err = runner.appendRunnerLedgerEntrySuccessTerminal(ctx, state)
	if err != nil {
		return runnerLedgerEntrySuccessOutcome{}, err
	}
	return finishRunnerLedgerEntrySuccess(state)
}

func (runner *Runner) prepareRunnerLedgerEntrySuccess(ctx context.Context, permit *runnerLedgerEntryExecutionPermit, bundle *RuntimeBundle, plans []StatementPlan) (*runnerLedgerEntrySuccessState, error) {
	if permit == nil || permit.self != permit {
		return nil, fail(CodeTransactionBoundary, "runner-ledger-entry-success-claim", "execution permit is unavailable", nil)
	}
	registered, loaded := runnerLedgerEntryExecutionPermitRegistry.LoadAndDelete(permit)
	record, recordOK := registered.(runnerLedgerEntryExecutionPermitRegistryRecord)
	if !loaded || !recordOK || record.permit != permit {
		return nil, fail(CodeTransactionBoundary, "runner-ledger-entry-success-claim", "execution permit is unavailable or already consumed", nil)
	}
	valid := validRunnerLedgerEntryExecutionPermitWithRecord(permit, record)
	permit.closed = true
	permit.session = nil
	permit.evidenceBinder = nil
	permit.use = nil
	if !valid {
		return nil, closeRunnerDatabasePreflight(record.session, record.key, true,
			fail(CodeTransactionBoundary, "runner-ledger-entry-success-claim", "execution permit changed before consumption", nil))
	}
	failClosed := func(primary error) (*runnerLedgerEntrySuccessState, error) {
		return nil, closeRunnerDatabasePreflight(record.session, record.key, true, primary)
	}
	if runner == nil || ctx == nil || !validRunnerLedgerEntryExecutionAdmissionProfiles() {
		return failClosed(fail(CodeTransactionBoundary, "runner-ledger-entry-success-claim", "success writer inputs are unavailable", nil))
	}
	if err := contextAdmissionError(ctx); err != nil {
		return failClosed(err)
	}
	evidence, ok := record.evidenceBinder.(runnerLedgerEntrySuccessEvidenceBinder)
	if !ok || evidence == nil || !runnerOwnedPointer(evidence) ||
		!validRunnerLedgerEntryExecutionAdmissionUse(record.evidenceBinder, record.use, permit.consumerFactSubject, permit.evidenceBoundary, true) {
		return failClosed(fail(CodeEvidenceRecoveryRequired, "runner-ledger-entry-success-claim", "success evidence binder is unavailable", nil))
	}
	verifiedBundle, err := verifiedRunnerLedgerCatalogBundle(bundle)
	if err != nil {
		return failClosed(err)
	}
	data, err := runnerLedgerEntrySuccessInitialData(permit, record, evidence, verifiedBundle, plans)
	if err != nil {
		return failClosed(err)
	}
	state, err := sealRunnerLedgerEntrySuccessState(data, "unclassified", "consume_execution_permit")
	if err != nil {
		return failClosed(err)
	}
	return state, nil
}

func runnerLedgerEntrySuccessInitialData(permit *runnerLedgerEntryExecutionPermit, record runnerLedgerEntryExecutionPermitRegistryRecord, evidence runnerLedgerEntrySuccessEvidenceBinder, bundle *RuntimeBundle, plans []StatementPlan) (runnerLedgerEntrySuccessData, error) {
	var data runnerLedgerEntrySuccessData
	if permit == nil || bundle == nil || bundle.Manifest == nil || int(permit.selection.entryIndex) >= len(bundle.Manifest.SchemaBundle.Migrations) ||
		permit.selection.migrationID == "" || permit.selection.planCount == 0 || permit.selection.planDigest == ([32]byte{}) {
		return data, fail(CodeUntrusted, "runner-ledger-entry-success-input", "selected runtime entry is unavailable", nil)
	}
	entry := bundle.Manifest.SchemaBundle.Migrations[permit.selection.entryIndex]
	wantRow := commitIntentLedgerRow(entry, bundle.Manifest.SchemaBundleDigest)
	canonical, err := canonicalContractKey(wantRow)
	if err != nil || wantRow.Validate() != nil || permit.selection.entryDigest != DigestBytes([]byte(runnerLedgerPreflightEntryDigestDomain+"\x00"+canonical)) ||
		entry.ID != permit.selection.migrationID || bundle.Manifest.SchemaBundleDigest != permit.generation.schemaBundleDigest {
		return data, fail(CodeUntrusted, "runner-ledger-entry-success-input", "selected signed entry differs from the execution permit", nil)
	}
	planDigest, planCount, err := runnerEntryPlanClosureDigest(plans, entry.ID)
	if err != nil || planDigest != permit.selection.planDigest || planCount != permit.selection.planCount {
		return data, fail(CodeUntrusted, "runner-ledger-entry-success-input", "statement plan closure differs from the execution permit", nil)
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
		return data, fail(CodeUntrusted, "runner-ledger-entry-success-input", "selected statement plan count changed", nil)
	}
	candidate := evidence.CurrentCandidate()
	active := evidence.ActiveGeneration()
	snapshot := evidence.RecoverySnapshot()
	journal := evidence.Journal()
	bindings, err := runnerCurrentProjectionBindings(evidence, candidate)
	if err != nil || !validOwnedCurrentCandidate(candidate) || candidate.binding != permit.candidateBinding ||
		active.kind != activeGenerationCurrent || !sameGenerationIdentity(active.identity, permit.generation) ||
		journal == nil || !sameRunnerOwnedPointer(journal, active.journal) || snapshot == nil ||
		!validRecoverySnapshotForJournal(snapshot, permit.generation, snapshot.cursor) ||
		generationJournalRecoveryDigest(snapshot) != permit.recoveryDigest || snapshot.tailDigest != permit.recoveryTail ||
		!runnerLedgerEntrySuccessInitialRecoveryMatches(snapshot, permit.selection.entryIndex) {
		return data, fail(CodeEvidenceRecoveryRequired, "runner-ledger-entry-success-input", "current evidence boundary differs from the execution permit", err)
	}
	catalog, ok := exactCatalogBindingForHead(bindings.executableCatalogs, entry.ID)
	if !ok || catalog.verifiedCatalog.validate() != nil || catalog.catalogContractDigest != catalog.verifiedCatalog.SubjectDigest() {
		return data, fail(CodeUntrusted, "runner-ledger-entry-success-input", "selected catalog contract is unavailable", nil)
	}
	source, err := exactMigrationSource(catalog.catalogContract.SourceDescriptors, entry.ID)
	if err != nil || len(source.Statements) != len(selected) {
		return data, fail(CodeUntrusted, "runner-ledger-entry-success-input", "catalog statement closure differs from the selected plans", err)
	}
	if bundle.Manifest.ExecutionPolicy.Validate() != nil || bundle.Manifest.ExecutionPolicy.MaxAttempts == 0 || bundle.Manifest.ExecutionPolicy.MaxAttempts > uint64(^uint32(0)) {
		return data, fail(CodeUntrusted, "runner-ledger-entry-success-input", "execution policy is unavailable", nil)
	}
	if permit.ledgerLength == 0 {
		if permit.catalogContractDigest != nil || entry.PredecessorID != nil {
			return data, fail(CodeUntrusted, "runner-ledger-entry-success-input", "first entry predecessor binding is contradictory", nil)
		}
	} else if permit.catalogContractDigest == nil || entry.PredecessorID == nil || *entry.PredecessorID != permit.ledgerHead ||
		entry.PredecessorCatalogContract.Artifact == nil || entry.PredecessorCatalogContract.Artifact.SHA256 != *permit.catalogContractDigest {
		return data, fail(CodeUntrusted, "runner-ledger-entry-success-input", "next entry predecessor catalog differs from the locked ledger head", nil)
	}
	data = runnerLedgerEntrySuccessData{
		phase: runnerLedgerEntrySuccessExecutionReady, session: record.session, evidence: evidence, journal: journal,
		use: record.use, key: record.key, candidateBinding: record.candidateBinding, generation: permit.generation,
		consumerFactSubject: permit.consumerFactSubject, evidenceBoundary: permit.evidenceBoundary,
		recoveryDigest: permit.recoveryDigest, recoveryTail: permit.recoveryTail, database: permit.database,
		selection: permit.selection, ledgerBeforeDigest: permit.ledgerDigest, ledgerBeforeHead: permit.ledgerHead,
		ledgerBeforeLength: permit.ledgerLength, connectedAuthority: permit.connectedAuthorityDigest,
		migrationAuthority: permit.migrationAuthorityDigest, projectionSubject: permit.projectionSubject,
		observedCatalogContract: cloneDigestPointer(permit.catalogContractDigest), observedCatalogDigest: permit.catalogDigest,
		bundle: bundle, bindings: bindings.ownedCopy(), entry: cloneProjectionValue(entry), plans: selected,
		maxAttempts: uint32(bundle.Manifest.ExecutionPolicy.MaxAttempts), cursor: snapshot.cursor.clone(),
		currentCatalogDigest: permit.catalogDigest,
	}
	return data, nil
}

func runnerLedgerEntrySuccessInitialRecoveryMatches(snapshot *RecoverySnapshot, entryIndex uint32) bool {
	if snapshot == nil {
		return false
	}
	if entryIndex == 0 {
		return (snapshot.state == RecoveryBrandNew || snapshot.state == RecoveryBrandNewInherited) &&
			snapshot.nextPermittedAction == RecoveryBeginFirstAttempt && snapshot.migrationID == nil && snapshot.attemptIndex == nil
	}
	return (snapshot.state == RecoveryBrandNewInherited || snapshot.state == RecoveryTerminal) &&
		snapshot.nextPermittedAction == RecoveryBeginFirstAttemptNextEntry
}

func sealRunnerLedgerEntrySuccessState(data runnerLedgerEntrySuccessData, from, event string) (*runnerLedgerEntrySuccessState, error) {
	if !runnerLedgerEntrySuccessTransitionAllowed(from, event, data.phase) {
		return nil, runnerLedgerEntrySuccessSealFailure(data, "success writer transition is outside the generated registry")
	}
	owned, err := cloneRunnerLedgerEntrySuccessData(data)
	if err != nil {
		return nil, runnerLedgerEntrySuccessSealFailure(data, "success writer state could not be owned")
	}
	state := &runnerLedgerEntrySuccessState{data: owned}
	state.self = state
	state.binding = &runnerLedgerEntrySuccessStateBinding{
		state: state, session: owned.session, transaction: owned.transaction, evidence: owned.evidence,
		journal: owned.journal, candidateBinding: owned.candidateBinding, cursorValid: owned.cursor.valid,
	}
	state.canonical = runnerLedgerEntrySuccessStateDigest(state)
	state.binding.canonical = state.canonical
	if state.canonical == ([32]byte{}) {
		return nil, runnerLedgerEntrySuccessSealFailure(data, "success writer state could not be identified")
	}
	recordData, err := cloneRunnerLedgerEntrySuccessData(owned)
	if err != nil {
		return nil, runnerLedgerEntrySuccessSealFailure(data, "success writer registry state could not be owned")
	}
	cleanupData, err := cloneRunnerLedgerEntrySuccessData(owned)
	if err != nil {
		return nil, runnerLedgerEntrySuccessSealFailure(data, "success writer cleanup state could not be owned")
	}
	claimed := &atomic.Bool{}
	record := &runnerLedgerEntrySuccessStateRegistryRecord{
		state: state, binding: cloneRunnerLedgerEntrySuccessStateBinding(state.binding), data: recordData, canonical: state.canonical, claimed: claimed,
	}
	cleanupRecord := &runnerLedgerEntrySuccessStateRegistryRecord{
		state: state, binding: cloneRunnerLedgerEntrySuccessStateBinding(state.binding), data: cleanupData, canonical: state.canonical, claimed: claimed,
	}
	runnerLedgerEntrySuccessStateRegistry.Store(state, record)
	runnerLedgerEntrySuccessStateCleanupRegistry.Store(state, cleanupRecord)
	if !validRunnerLedgerEntrySuccessState(state) {
		runnerLedgerEntrySuccessStateRegistry.Delete(state)
		runnerLedgerEntrySuccessStateCleanupRegistry.Delete(state)
		return nil, runnerLedgerEntrySuccessSealFailure(data, "success writer state could not be sealed")
	}
	return state, nil
}

func runnerLedgerEntrySuccessSealFailure(data runnerLedgerEntrySuccessData, message string) error {
	code := CodeTransactionBoundary
	if data.mutationAttempted {
		code = CodeEvidenceRecoveryRequired
		revokeRunnerLedgerEntrySuccessCursor(data)
	}
	return fail(code, "runner-ledger-entry-success-seal", message, nil)
}

func revokeRunnerLedgerEntrySuccessCursor(data runnerLedgerEntrySuccessData) {
	if data.cursor.valid != nil {
		data.cursor.valid.Store(false)
	}
}

func runnerLedgerEntrySuccessPostCommitFailure(data runnerLedgerEntrySuccessData, op, message string, cause error) error {
	revokeRunnerLedgerEntrySuccessCursor(data)
	return fail(CodeEvidenceRecoveryRequired, op, message, cause)
}

func runnerLedgerEntrySuccessTransitionAllowed(from, event, to string) bool {
	if !validRunnerLedgerEntryExecutionAdmissionProfiles() {
		return false
	}
	for _, transition := range generatedRunnerLedgerEntrySuccessWriterTransitions {
		if transition.from == from && transition.event == event && transition.to == to {
			return true
		}
	}
	return false
}

func validRunnerLedgerEntrySuccessState(state *runnerLedgerEntrySuccessState) bool {
	if state == nil || state.self != state || state.closed || state.binding == nil || state.binding.state != state ||
		state.canonical == ([32]byte{}) || state.binding.canonical != state.canonical ||
		state.canonical != runnerLedgerEntrySuccessStateDigest(state) || !validRunnerLedgerEntrySuccessData(state.data) ||
		!sameRunnerLedgerEntrySuccessAuthority(state.data, state.binding) {
		return false
	}
	registered, ok := runnerLedgerEntrySuccessStateRegistry.Load(state)
	record, recordOK := registered.(*runnerLedgerEntrySuccessStateRegistryRecord)
	cleanup, cleanupOK := runnerLedgerEntrySuccessStateCleanupRegistry.Load(state)
	cleanupRecord, cleanupRecordOK := cleanup.(*runnerLedgerEntrySuccessStateRegistryRecord)
	return ok && recordOK && cleanupOK && cleanupRecordOK &&
		sameRunnerLedgerEntrySuccessRegistryRecords(record, cleanupRecord, state) && !record.claimed.Load() &&
		sameRunnerLedgerEntrySuccessStateBindings(record.binding, state.binding) && record.canonical == state.canonical
}

func sameRunnerLedgerEntrySuccessRegistryRecords(left, right *runnerLedgerEntrySuccessStateRegistryRecord, state *runnerLedgerEntrySuccessState) bool {
	return validRunnerLedgerEntrySuccessRegistryRecord(left, state) && validRunnerLedgerEntrySuccessRegistryRecord(right, state) &&
		left != right && left.claimed == right.claimed && left.binding != right.binding &&
		sameRunnerLedgerEntrySuccessStateBindings(left.binding, right.binding) && left.canonical == right.canonical &&
		runnerLedgerEntrySuccessDataDigest(left.data) == runnerLedgerEntrySuccessDataDigest(right.data)
}

func validRunnerLedgerEntrySuccessRegistryRecord(record *runnerLedgerEntrySuccessStateRegistryRecord, state *runnerLedgerEntrySuccessState) bool {
	return record != nil && state != nil && record.state == state && record.binding != nil && record.binding != state.binding && record.claimed != nil &&
		record.canonical != ([32]byte{}) && runnerLedgerEntrySuccessDataDigest(record.data) == record.canonical &&
		record.binding.state == state && record.binding.canonical == record.canonical && sameRunnerLedgerEntrySuccessAuthority(record.data, record.binding)
}

func cloneRunnerLedgerEntrySuccessStateBinding(binding *runnerLedgerEntrySuccessStateBinding) *runnerLedgerEntrySuccessStateBinding {
	if binding == nil {
		return nil
	}
	owned := *binding
	return &owned
}

func sameRunnerLedgerEntrySuccessStateBindings(left, right *runnerLedgerEntrySuccessStateBinding) bool {
	return left != nil && right != nil && left.state == right.state && left.canonical == right.canonical &&
		left.candidateBinding == right.candidateBinding && left.cursorValid == right.cursorValid &&
		sameOptionalRunnerOwnedPointer(left.session, right.session) && sameOptionalRunnerOwnedPointer(left.transaction, right.transaction) &&
		sameRunnerOwnedPointer(left.evidence, right.evidence) && sameRunnerOwnedPointer(left.journal, right.journal)
}

func sameRunnerLedgerEntrySuccessAuthority(data runnerLedgerEntrySuccessData, binding *runnerLedgerEntrySuccessStateBinding) bool {
	return binding != nil && binding.candidateBinding == data.candidateBinding && binding.cursorValid == data.cursor.valid &&
		sameOptionalRunnerOwnedPointer(binding.session, data.session) && sameOptionalRunnerOwnedPointer(binding.transaction, data.transaction) &&
		sameRunnerOwnedPointer(binding.evidence, data.evidence) && sameRunnerOwnedPointer(binding.journal, data.journal)
}

func sameOptionalRunnerOwnedPointer(left, right any) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return sameRunnerOwnedPointer(left, right)
}

func validRunnerLedgerEntrySuccessData(data runnerLedgerEntrySuccessData) bool {
	if !validRunnerLedgerEntryExecutionAdmissionProfiles() || data.evidence == nil || data.journal == nil ||
		!runnerOwnedPointer(data.evidence) || !runnerOwnedPointer(data.journal) || data.use == nil ||
		data.candidateBinding == nil || data.generation.owner == nil || data.generation.owner != data.candidateBinding.owner ||
		data.recoveryDigest == ([32]byte{}) || data.recoveryTail.Validate() != nil || data.ledgerBeforeDigest.Validate() != nil ||
		data.connectedAuthority.Validate() != nil || data.migrationAuthority.Validate() != nil || data.projectionSubject.Validate() != nil ||
		data.observedCatalogDigest.Validate() != nil || data.selection.entryDigest.Validate() != nil || data.selection.planDigest == ([32]byte{}) ||
		data.selection.planCount == 0 || uint32(len(data.plans)) != data.selection.planCount || data.maxAttempts == 0 ||
		data.bundle == nil || data.bundle.Manifest == nil || data.bundle.Manifest.SchemaBundleDigest != data.generation.schemaBundleDigest ||
		data.entry.ID != data.selection.migrationID || data.statementIndex >= uint32(len(data.plans)) ||
		!data.cursor.Valid() || !sameGenerationIdentity(data.cursor.generation, data.generation) ||
		data.consumerFactSubject.Validate() != nil || data.evidenceBoundary == ([32]byte{}) ||
		!validRunnerLedgerEntryExecutionAdmissionUse(data.evidence, data.use, data.consumerFactSubject, data.evidenceBoundary, true) ||
		!runnerLedgerEntrySuccessEvidenceMatches(data) {
		return false
	}
	for index := range data.plans {
		if data.plans[index].validateExact() != nil || data.plans[index].MigrationID != data.entry.ID || data.plans[index].StatementIndex != uint32(index) {
			return false
		}
	}
	if data.observedCatalogContract != nil && data.observedCatalogContract.Validate() != nil {
		return false
	}
	transactionPhase := stringIn(data.phase,
		runnerLedgerEntrySuccessTransactionReady, runnerLedgerEntrySuccessStatementReady,
		runnerLedgerEntrySuccessIntentDurable, runnerLedgerEntrySuccessStatementExecuted,
		runnerLedgerEntrySuccessIntermediateDurable, runnerLedgerEntrySuccessFinalIntermediateDurable,
		runnerLedgerEntrySuccessLedgerReadbackReady, runnerLedgerEntrySuccessCommitIntentDurable)
	switch {
	case data.phase == runnerLedgerEntrySuccessExecutionReady:
		return data.session != nil && data.transaction == nil && runnerOwnedPointer(data.session)
	case transactionPhase:
		status, ok := migrationProjectionTxStatus(data.transaction)
		return data.session != nil && data.transaction != nil && runnerOwnedPointer(data.session) && runnerOwnedPointer(data.transaction) && ok && status == 'T'
	case data.phase == runnerLedgerEntrySuccessCommitKnownCommitted:
		return data.session == nil && data.transaction == nil && validRunnerCommitProtocolFacts(data.commitFacts) && data.commitFacts.outcome == runnerCommitProtocolCommitted
	case data.phase == runnerLedgerEntrySuccessTerminalDurable:
		return data.session == nil && data.transaction == nil && validRunnerCommitProtocolFacts(data.commitFacts) &&
			data.commitFacts.outcome == runnerCommitProtocolCommitted && data.terminal.Validate(data.maxAttempts) == nil &&
			data.terminalRecordDigest.Validate() == nil
	default:
		return false
	}
}

func runnerLedgerEntrySuccessEvidenceMatches(data runnerLedgerEntrySuccessData) bool {
	current := data.evidence.CurrentCandidate()
	active := data.evidence.ActiveGeneration()
	journal := data.evidence.Journal()
	snapshot := data.evidence.RecoverySnapshot()
	return validOwnedCurrentCandidate(current) && current.binding == data.candidateBinding &&
		active.kind == activeGenerationCurrent && sameGenerationIdentity(active.identity, data.generation) &&
		journal != nil && sameRunnerOwnedPointer(journal, data.journal) && snapshot != nil &&
		validRecoverySnapshotForJournal(snapshot, data.generation, data.cursor) && sameCursorIdentity(snapshot.cursor, data.cursor) &&
		generationJournalRecoveryDigest(snapshot) == data.recoveryDigest && snapshot.tailDigest == data.recoveryTail
}

func consumeRunnerLedgerEntrySuccessState(state *runnerLedgerEntrySuccessState, expectedPhase string) (runnerLedgerEntrySuccessData, error) {
	if state == nil {
		return runnerLedgerEntrySuccessData{}, fail(CodeTransactionBoundary, "runner-ledger-entry-success-transition", "success writer authority is unavailable", nil)
	}
	record, registriesValid := claimRunnerLedgerEntrySuccessStateRecord(state)
	if record == nil || record.binding == nil {
		return runnerLedgerEntrySuccessData{}, fail(CodeTransactionBoundary, "runner-ledger-entry-success-transition", "success writer authority is unavailable or already consumed", nil)
	}
	owned, cloneErr := cloneRunnerLedgerEntrySuccessData(record.data)
	valid := cloneErr == nil && registriesValid && state.data.phase == expectedPhase && validRunnerLedgerEntrySuccessStateWithoutRegistry(state, record)
	state.closed = true
	state.data.session = nil
	state.data.transaction = nil
	state.data.evidence = nil
	state.data.journal = nil
	state.data.use = nil
	state.binding = nil
	if !valid {
		if cloneErr != nil {
			return owned, cloneErr
		}
		return owned, fail(CodeTransactionBoundary, "runner-ledger-entry-success-transition", "success writer authority changed before transition", nil)
	}
	return owned, nil
}

func claimRunnerLedgerEntrySuccessStateRecord(state *runnerLedgerEntrySuccessState) (*runnerLedgerEntrySuccessStateRegistryRecord, bool) {
	cleanup, cleanupLoaded := runnerLedgerEntrySuccessStateCleanupRegistry.Load(state)
	cleanupRecord, cleanupOK := cleanup.(*runnerLedgerEntrySuccessStateRegistryRecord)
	if cleanupLoaded && cleanupOK && validRunnerLedgerEntrySuccessRegistryRecord(cleanupRecord, state) {
		if !cleanupRecord.claimed.CompareAndSwap(false, true) {
			return nil, false
		}
		registered, loaded := runnerLedgerEntrySuccessStateRegistry.LoadAndDelete(state)
		record, recordOK := registered.(*runnerLedgerEntrySuccessStateRegistryRecord)
		runnerLedgerEntrySuccessStateCleanupRegistry.Delete(state)
		return cleanupRecord, loaded && recordOK && sameRunnerLedgerEntrySuccessRegistryRecords(record, cleanupRecord, state)
	}
	registered, loaded := runnerLedgerEntrySuccessStateRegistry.LoadAndDelete(state)
	record, recordOK := registered.(*runnerLedgerEntrySuccessStateRegistryRecord)
	runnerLedgerEntrySuccessStateCleanupRegistry.Delete(state)
	if !loaded || !recordOK || !validRunnerLedgerEntrySuccessRegistryRecord(record, state) || !record.claimed.CompareAndSwap(false, true) {
		return nil, false
	}
	return record, false
}

func validRunnerLedgerEntrySuccessStateWithoutRegistry(state *runnerLedgerEntrySuccessState, record *runnerLedgerEntrySuccessStateRegistryRecord) bool {
	return state != nil && state.self == state && !state.closed && state.binding != nil && sameRunnerLedgerEntrySuccessStateBindings(record.binding, state.binding) &&
		record.canonical == state.canonical && state.binding.canonical == state.canonical &&
		state.canonical == runnerLedgerEntrySuccessStateDigest(state) && validRunnerLedgerEntrySuccessData(state.data) &&
		runnerLedgerEntrySuccessDataDigest(record.data) == record.canonical && sameRunnerLedgerEntrySuccessAuthority(record.data, record.binding) &&
		sameRunnerLedgerEntrySuccessAuthority(state.data, record.binding)
}

func closeRunnerLedgerEntrySuccessData(data runnerLedgerEntrySuccessData, primary error) error {
	if data.session == nil {
		return primary
	}
	if data.transaction != nil {
		return closeRunnerCurrentTransactionResources(data.session, data.transaction, data.key, primary)
	}
	return closeRunnerDatabasePreflight(data.session, data.key, true, primary)
}

func closeRunnerLedgerEntrySuccessState(state *runnerLedgerEntrySuccessState, primary error) error {
	data, err := consumeRunnerLedgerEntrySuccessState(state, statePhaseOrEmpty(state))
	if err != nil {
		if len(data.phase) != 0 {
			return closeRunnerLedgerEntrySuccessData(data, err)
		}
		return err
	}
	return closeRunnerLedgerEntrySuccessData(data, primary)
}

func statePhaseOrEmpty(state *runnerLedgerEntrySuccessState) string {
	if state == nil {
		return ""
	}
	return state.data.phase
}

func cloneRunnerLedgerEntrySuccessData(data runnerLedgerEntrySuccessData) (runnerLedgerEntrySuccessData, error) {
	owned := data
	owned.observedCatalogContract = cloneDigestPointer(data.observedCatalogContract)
	owned.bundle = data.bundle
	owned.bindings = data.bindings.ownedCopy()
	owned.entry = cloneProjectionValue(data.entry)
	owned.plans = make([]StatementPlan, len(data.plans))
	for index := range data.plans {
		plan, err := cloneRunnerStatementIntentPlan(data.plans[index])
		if err != nil {
			return runnerLedgerEntrySuccessData{}, err
		}
		owned.plans[index] = plan
	}
	owned.cursor = data.cursor.clone()
	owned.previousIntermediate = cloneDigestPointer(data.previousIntermediate)
	owned.authorityBefore = cloneProjectionValue(data.authorityBefore)
	owned.catalogBefore = cloneProjectionValue(data.catalogBefore)
	owned.intent = cloneProjectionValue(data.intent)
	owned.authorityAfterProjection = cloneProjectionValue(data.authorityAfterProjection)
	owned.catalogAfterProjection = cloneProjectionValue(data.catalogAfterProjection)
	owned.authorityAfter = cloneProjectionValue(data.authorityAfter)
	owned.catalogAfter = cloneProjectionValue(data.catalogAfter)
	owned.intermediate = cloneProjectionValue(data.intermediate)
	owned.ledgerRows = cloneProjectionValue(data.ledgerRows)
	owned.commit = cloneProjectionValue(data.commit)
	owned.terminal = cloneProjectionValue(data.terminal)
	return owned, nil
}

func runnerLedgerEntrySuccessStateDigest(state *runnerLedgerEntrySuccessState) [32]byte {
	if state == nil || state.self != state || state.closed {
		return [32]byte{}
	}
	return runnerLedgerEntrySuccessDataDigest(state.data)
}

func runnerLedgerEntrySuccessDataDigest(data runnerLedgerEntrySuccessData) [32]byte {
	if data.candidateBinding == nil || data.candidateBinding.canonical == ([32]byte{}) || data.recoveryDigest == ([32]byte{}) ||
		data.selection.planDigest == ([32]byte{}) || data.bundle == nil || data.bundle.Manifest == nil || data.cursor.valid == nil {
		return [32]byte{}
	}
	h := sha256.New()
	h.Write([]byte(runnerLedgerEntrySuccessStateDigestDomain + "\x00"))
	h.Write(data.candidateBinding.canonical[:])
	h.Write(data.evidenceBoundary[:])
	h.Write(data.recoveryDigest[:])
	h.Write(data.selection.planDigest[:])
	for _, identity := range runnerLedgerEntrySuccessWriterIdentityStrings() {
		writeAdmissionString(h, identity)
	}
	for _, value := range []string{
		data.phase, data.generation.executionLineageDigest.String(), data.generation.journalIdentityDigest.String(),
		data.generation.runnerProjectionDecisionDigest.String(), data.generation.schemaBundleDigest.String(),
		data.consumerFactSubject.String(), data.recoveryTail.String(), data.selection.migrationID, data.selection.entryDigest.String(),
		data.ledgerBeforeDigest.String(), data.ledgerBeforeHead, data.connectedAuthority.String(),
		data.migrationAuthority.String(), data.projectionSubject.String(), data.observedCatalogDigest.String(),
		data.currentCatalogDigest.String(), data.firstCatalogBeforeDigest.String(), data.transactionAuthority.String(),
		data.database.databaseName, data.database.sessionUser, data.database.currentUser,
		data.bundle.Manifest.ManifestDigest.String(), data.bundle.Manifest.SchemaBundleDigest.String(),
		data.intentRecordDigest.String(), data.intermediateRecordDigest.String(), data.ledgerPrefixDigest.String(),
		data.ledgerHead, data.commitRecordDigest.String(), data.terminalRecordDigest.String(),
		string(data.commitFacts.outcome), data.commitFacts.rejectionReason,
	} {
		writeAdmissionString(h, value)
	}
	writeAdmissionUint(h, uint64(data.selection.entryIndex))
	writeAdmissionUint(h, uint64(data.selection.planCount))
	writeAdmissionUint(h, uint64(data.ledgerBeforeLength))
	writeAdmissionUint(h, uint64(data.maxAttempts))
	writeAdmissionUint(h, uint64(data.statementIndex))
	writeAdmissionUint(h, uint64(data.ledgerLength))
	writeAdmissionUint(h, boolUint64(data.mutationAttempted))
	writeAdmissionUint(h, boolUint64(data.commitFacts.commitCalled))
	writeAdmissionUint(h, boolUint64(data.commitFacts.readyForQuery))
	writeAdmissionUint(h, boolUint64(data.commitFacts.connectionClosed))
	writeGenerationJournalCursor(h, data.cursor)
	writeOptionalAdmissionDigest(h, data.observedCatalogContract)
	writeOptionalAdmissionDigest(h, data.previousIntermediate)
	if data.catalogAfterProjection.Absent == nil && data.catalogAfterProjection.Present == nil {
		writeAdmissionString(h, "catalog-after-projection-absent")
	} else {
		canonical, err := canonicalContractKey(data.catalogAfterProjection)
		if err != nil {
			return [32]byte{}
		}
		writeAdmissionString(h, "catalog-after-projection-present")
		writeAdmissionString(h, canonical)
	}
	for _, plan := range data.plans {
		writeAdmissionString(h, plan.exactCanonical)
	}
	for _, value := range []any{
		data.entry, data.authorityBefore, data.catalogBefore, data.intent, data.authorityAfterProjection,
		data.authorityAfter, data.catalogAfter, data.intermediate,
		data.ledgerRows, data.commit, data.terminal,
	} {
		canonical, err := canonicalContractKey(value)
		if err != nil {
			return [32]byte{}
		}
		writeAdmissionString(h, canonical)
	}
	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result
}

func writeOptionalAdmissionDigest(h interface{ Write([]byte) (int, error) }, value *Digest) {
	if value == nil {
		writeAdmissionString(h, "absent")
		return
	}
	writeAdmissionString(h, "present")
	writeAdmissionString(h, value.String())
}

func (runner *Runner) beginRunnerLedgerEntrySuccessTransaction(ctx context.Context, ready *runnerLedgerEntrySuccessState) (*runnerLedgerEntrySuccessState, error) {
	data, err := consumeRunnerLedgerEntrySuccessState(ready, runnerLedgerEntrySuccessExecutionReady)
	if err != nil {
		return nil, closeRunnerLedgerEntrySuccessData(data, err)
	}
	failClosed := func(primary error) (*runnerLedgerEntrySuccessState, error) {
		return nil, closeRunnerLedgerEntrySuccessData(data, primary)
	}
	if runner == nil || ctx == nil {
		return failClosed(fail(CodeTransactionBoundary, "runner-ledger-entry-success-transaction", "success writer transaction context is unavailable", nil))
	}
	if err := contextAdmissionError(ctx); err != nil {
		return failClosed(err)
	}
	boundary, boundaryErr := data.session.Boundary(ctx, data.key)
	if boundaryErr != nil || boundary.TxStatus != 'I' || boundary.CurrentUser != MigrationOwnerRole || !boundary.LockHeld {
		return failClosed(mapRunnerDatabasePreflightError(boundaryErr, "runner-ledger-entry-success-transaction", "retained execution session left its exact idle boundary"))
	}
	transaction, beginErr := data.session.BeginMigration(ctx)
	if beginErr != nil || transaction == nil || !runnerOwnedPointer(transaction) {
		if beginErr == nil {
			beginErr = fail(CodeTransactionBoundary, "runner-ledger-entry-success-transaction", "migration transaction ownership is unavailable", nil)
		} else {
			beginErr = mapRunnerDatabasePreflightError(beginErr, "runner-ledger-entry-success-transaction", "serializable migration transaction could not be opened")
		}
		data.transaction = transaction
		return failClosed(beginErr)
	}
	data.transaction = transaction
	boundary, boundaryErr = transaction.Boundary(ctx, data.key)
	status, statusOK := migrationProjectionTxStatus(transaction)
	if boundaryErr != nil || !statusOK || status != 'T' || boundary.TxStatus != 'T' || boundary.CurrentUser != MigrationOwnerRole || !boundary.LockHeld {
		return failClosed(mapRunnerDatabasePreflightError(boundaryErr, "runner-ledger-entry-success-transaction", "migration transaction left its exact role, lock, or status boundary"))
	}
	if !runnerLedgerEntrySuccessEvidenceMatches(data) {
		return failClosed(fail(CodeEvidenceJournalFailed, "runner-ledger-entry-success-transaction", "evidence boundary changed while opening the transaction", nil))
	}
	data.phase = runnerLedgerEntrySuccessTransactionReady
	next, sealErr := sealRunnerLedgerEntrySuccessState(data, runnerLedgerEntrySuccessExecutionReady, "begin_transaction")
	if sealErr != nil {
		return failClosed(sealErr)
	}
	return next, nil
}

func (runner *Runner) prepareRunnerLedgerEntrySuccessStatement(ctx context.Context, transaction *runnerLedgerEntrySuccessState) (*runnerLedgerEntrySuccessState, error) {
	data, err := consumeRunnerLedgerEntrySuccessState(transaction, runnerLedgerEntrySuccessTransactionReady)
	if err != nil {
		return nil, closeRunnerLedgerEntrySuccessData(data, err)
	}
	prepared, err := runner.prepareRunnerLedgerEntrySuccessStatementData(ctx, data)
	if err != nil {
		return nil, closeRunnerLedgerEntrySuccessData(data, err)
	}
	prepared.phase = runnerLedgerEntrySuccessStatementReady
	next, err := sealRunnerLedgerEntrySuccessState(prepared, runnerLedgerEntrySuccessTransactionReady, "prepare_statement")
	if err != nil {
		return nil, closeRunnerLedgerEntrySuccessData(prepared, err)
	}
	return next, nil
}

func (runner *Runner) advanceRunnerLedgerEntrySuccessStatement(ctx context.Context, intermediate *runnerLedgerEntrySuccessState) (*runnerLedgerEntrySuccessState, error) {
	data, err := consumeRunnerLedgerEntrySuccessState(intermediate, runnerLedgerEntrySuccessIntermediateDurable)
	if err != nil {
		return nil, closeRunnerLedgerEntrySuccessData(data, err)
	}
	if data.statementIndex+1 >= uint32(len(data.plans)) || data.intermediate.State.IntermediateStateDigest.Validate() != nil {
		return nil, closeRunnerLedgerEntrySuccessData(data,
			fail(CodeTransactionBoundary, "runner-ledger-entry-success-advance", "non-final statement successor is unavailable", nil))
	}
	previous := data.intermediate.State.IntermediateStateDigest
	data.previousIntermediate = &previous
	data.statementIndex++
	data.authorityBefore = ProjectionResultEvidence{}
	data.catalogBefore = ProjectionResultEvidence{}
	data.intent = StatementIntent{}
	data.intentRecordDigest = ""
	data.authorityAfterProjection = AuthorityProjection{}
	data.catalogAfterProjection = CatalogStateProjection{}
	data.authorityAfter = ProjectionResultEvidence{}
	data.catalogAfter = ProjectionResultEvidence{}
	data.intermediate = StatementIntermediateEvidence{}
	data.intermediateRecordDigest = ""
	prepared, err := runner.prepareRunnerLedgerEntrySuccessStatementData(ctx, data)
	if err != nil {
		return nil, closeRunnerLedgerEntrySuccessData(data, err)
	}
	prepared.phase = runnerLedgerEntrySuccessStatementReady
	next, err := sealRunnerLedgerEntrySuccessState(prepared, runnerLedgerEntrySuccessIntermediateDurable, "advance_statement")
	if err != nil {
		return nil, closeRunnerLedgerEntrySuccessData(prepared, err)
	}
	return next, nil
}

func (runner *Runner) prepareRunnerLedgerEntrySuccessStatementData(ctx context.Context, data runnerLedgerEntrySuccessData) (runnerLedgerEntrySuccessData, error) {
	if runner == nil || ctx == nil || data.transaction == nil || data.statementIndex >= uint32(len(data.plans)) {
		return data, fail(CodeTransactionBoundary, "runner-ledger-entry-success-statement", "statement preparation inputs are unavailable", nil)
	}
	if err := contextAdmissionError(ctx); err != nil {
		return data, err
	}
	plan := data.plans[data.statementIndex]
	if plan.validateExact() != nil || plan.StatementIndex != data.statementIndex || plan.MigrationID != data.entry.ID {
		return data, fail(CodeUntrusted, "runner-ledger-entry-success-statement", "exact statement plan changed", nil)
	}
	before, err := runner.projectRunnerLedgerEntrySuccessBefore(ctx, data, plan)
	if err != nil {
		return data, err
	}
	if data.statementIndex == 0 {
		if data.ledgerBeforeLength == 0 && (before.catalogResult.Digest != data.observedCatalogDigest || plan.ExpectedTransition.CatalogBefore.Digest != data.observedCatalogDigest) {
			return data, fail(CodeCatalogDrift, "runner-ledger-entry-success-statement", "entry predecessor differs from the locked catalog observation", nil)
		}
		data.firstCatalogBeforeDigest = before.catalogResult.Digest
		data.transactionAuthority = before.authorityResult.Digest
	} else {
		if data.previousIntermediate == nil || before.catalogResult.Digest != data.currentCatalogDigest ||
			plan.ExpectedTransition.CatalogBefore.Digest != data.currentCatalogDigest ||
			before.authorityResult.Digest != data.transactionAuthority {
			return data, fail(CodeCatalogDrift, "runner-ledger-entry-success-statement", "statement predecessor does not continue the durable entry chain", nil)
		}
	}
	catalog, ok := exactCatalogBindingForHead(data.bindings.executableCatalogs, data.entry.ID)
	if !ok || catalog.verifiedCatalog.validate() != nil {
		return data, fail(CodeUntrusted, "runner-ledger-entry-success-statement", "selected catalog authority is unavailable", nil)
	}
	intent := StatementIntent{
		SchemaBundleDigest: data.generation.schemaBundleDigest, CatalogContractDigest: catalog.catalogContractDigest,
		AuthorityProfileDigest: data.bindings.authorityProfileDigest, AuthorityBindingDigest: data.bindings.authorityBindingDigest,
		MigrationID: plan.MigrationID, AttemptIndex: 1, StatementIndex: plan.StatementIndex,
		SQLPath: plan.SQLArtifactPath, SQLArtifactSHA256: plan.SQLArtifactSHA256, SQLArtifactSizeBytes: plan.SQLArtifactSizeBytes,
		StartOffset: plan.StartOffset, EndOffset: plan.EndOffset, StatementSHA256: plan.StatementSHA256,
		Classification: cloneProjectionValue(plan.Classification), PreviousAttemptTerminalDigest: nil,
		PreviousIntermediateStateDigest: cloneDigestPointer(data.previousIntermediate), ExpectedTransitionDigest: plan.ExpectedTransitionDigest,
		AuthorityBeforeDigest: before.authorityResult.Digest, CatalogBeforeDigest: before.catalogResult.Digest,
		AuthorityBeforeResult: cloneProjectionValue(before.authorityResult), CatalogBeforeResult: cloneProjectionValue(before.catalogResult),
	}
	if intent.Validate() != nil || !planMatchesIntent(exactStatementWitnessFromPlan(plan, 1), intent) ||
		!runnerStatementIntentProjectionEvidenceMatches(plan, intent.AuthorityBeforeResult, intent.CatalogBeforeResult) {
		return data, fail(CodeUntrusted, "runner-ledger-entry-success-statement", "statement intent cannot be reproduced from verified inputs", nil)
	}
	data.authorityBefore = cloneProjectionValue(before.authorityResult)
	data.catalogBefore = cloneProjectionValue(before.catalogResult)
	data.intent = intent
	return data, nil
}

func (runner *Runner) projectRunnerLedgerEntrySuccessBefore(ctx context.Context, data runnerLedgerEntrySuccessData, plan StatementPlan) (runnerTransactionProjectionFacts, error) {
	profile, ok := data.transaction.(runnerTransactionProjectionProfile)
	if !ok {
		return runnerTransactionProjectionFacts{}, fail(CodeTransactionBoundary, "runner-ledger-entry-success-before-profile", "transaction cannot enter the closed projection profile", nil)
	}
	if err := profile.enterRunnerProjectionProfile(ctx); err != nil {
		return runnerTransactionProjectionFacts{}, mapRunnerDatabasePreflightError(err, "runner-ledger-entry-success-before-profile", "statement-before projection profile could not be configured")
	}
	statementIndex := plan.StatementIndex
	snapshot, err := BorrowMigrationProjectionSnapshot(ctx, data.transaction, plan.MigrationID, &statementIndex)
	if err != nil {
		return runnerTransactionProjectionFacts{}, mapRunnerDatabasePreflightError(err, "runner-ledger-entry-success-before-snapshot", "statement-before projection snapshot could not be borrowed")
	}
	if snapshot == nil {
		return runnerTransactionProjectionFacts{}, fail(CodeProjectionSnapshotInvalid, "runner-ledger-entry-success-before-snapshot", "statement-before projection snapshot is unavailable", nil)
	}
	metadata := snapshot.Metadata()
	if !sameRunnerPreparedDatabaseIdentity(data.database, metadata) || metadata.MigrationID == nil || *metadata.MigrationID != plan.MigrationID ||
		!sameRunnerStatementIndex(metadata.StatementIndex, &statementIndex) {
		return runnerTransactionProjectionFacts{}, fail(CodeProjectionMetadataMismatch, "runner-ledger-entry-success-before-snapshot", "statement-before snapshot differs from the retained database identity", nil)
	}
	factory := runner.projectionFactory
	if factory == nil {
		factory = pgRunnerAuthorityProjectorFactory{}
	}
	projector, err := factory.newRunnerAuthorityProjector(ctx, snapshot)
	if err != nil || projector == nil {
		return runnerTransactionProjectionFacts{}, mapRunnerDatabasePreflightError(err, "runner-ledger-entry-success-before-projector", "statement-before projector is unavailable")
	}
	authority, err := projector.ProjectAuthority(ctx, snapshot, data.bindings.verifiedAuthority, AuthorityPhaseMigrationTransaction)
	if err != nil {
		return runnerTransactionProjectionFacts{}, mapRunnerDatabasePreflightError(err, "runner-ledger-entry-success-before-authority", "statement-before authority projection failed")
	}
	if err := validateRunnerAuthorityProjectionResult(authority, metadata, data.bindings.verifiedAuthority, AuthorityPhaseMigrationTransaction); err != nil {
		return runnerTransactionProjectionFacts{}, err
	}
	catalogBinding, ok := exactCatalogBindingForHead(data.bindings.executableCatalogs, plan.MigrationID)
	if !ok || catalogBinding.verifiedCatalog.validate() != nil {
		return runnerTransactionProjectionFacts{}, fail(CodeUntrusted, "runner-ledger-entry-success-before-catalog", "statement catalog authority is unavailable", nil)
	}
	catalog, err := projector.ProjectTransitionState(ctx, snapshot, catalogBinding.verifiedCatalog, cloneProjectionValue(plan.ExpectedTransition.CatalogBefore.Scope))
	if err != nil {
		return runnerTransactionProjectionFacts{}, mapRunnerDatabasePreflightError(err, "runner-ledger-entry-success-before-catalog", "statement-before catalog projection failed")
	}
	if err := validateRunnerStatementAfterCatalogResult(catalog, metadata, catalogBinding.verifiedCatalog, plan.ExpectedTransition.CatalogBefore); err != nil {
		return runnerTransactionProjectionFacts{}, err
	}
	if !runnerCanonicalEqual(authority.Metadata.Snapshot, catalog.Metadata.Snapshot) {
		return runnerTransactionProjectionFacts{}, fail(CodeProjectionMetadataMismatch, "runner-ledger-entry-success-before", "statement-before authority and catalog snapshots differ", nil)
	}
	if err := profile.restoreRunnerExecutionProfile(ctx, data.bundle.Manifest.ExecutionPolicy); err != nil {
		return runnerTransactionProjectionFacts{}, mapRunnerDatabasePreflightError(err, "runner-ledger-entry-success-before-profile", "verified execution profile could not be restored")
	}
	facts := runnerTransactionProjectionFacts{
		snapshotDigest: runnerTransactionSnapshotDigest(metadata), authorityDigest: authority.Digest, catalogDigest: catalog.Digest,
		authorityResult: ProjectionResultEvidence{Digest: authority.Digest, Metadata: cloneProjectionValue(authority.Metadata)},
		catalogResult:   ProjectionResultEvidence{Digest: catalog.Digest, Metadata: cloneProjectionValue(catalog.Metadata)},
	}
	if facts.snapshotDigest == ([32]byte{}) || facts.authorityResult.Validate() != nil || facts.catalogResult.Validate() != nil {
		return runnerTransactionProjectionFacts{}, fail(CodeProjectionMetadataMismatch, "runner-ledger-entry-success-before", "statement-before projection could not be sealed", nil)
	}
	return facts, nil
}

func (runner *Runner) appendRunnerLedgerEntrySuccessIntent(ctx context.Context, prepared *runnerLedgerEntrySuccessState) (*runnerLedgerEntrySuccessState, error) {
	data, err := consumeRunnerLedgerEntrySuccessState(prepared, runnerLedgerEntrySuccessStatementReady)
	if err != nil {
		return nil, closeRunnerLedgerEntrySuccessData(data, err)
	}
	record := EvidenceRecord{StatementIntent: cloneStatementIntentPointer(&data.intent)}
	data, recordDigest, err := appendRunnerLedgerEntrySuccessRecord(ctx, data, record, data.plans[data.statementIndex])
	if err != nil {
		return nil, closeRunnerLedgerEntrySuccessData(data, err)
	}
	data.intentRecordDigest = recordDigest
	if !runnerLedgerEntrySuccessIntentRecoveryMatches(data) {
		return nil, closeRunnerLedgerEntrySuccessData(data,
			fail(CodeEvidenceJournalFailed, "runner-ledger-entry-success-intent", "durable statement intent recovery boundary is contradictory", nil))
	}
	data.phase = runnerLedgerEntrySuccessIntentDurable
	next, err := sealRunnerLedgerEntrySuccessState(data, runnerLedgerEntrySuccessStatementReady, "append_intent_durable")
	if err != nil {
		return nil, closeRunnerLedgerEntrySuccessData(data, err)
	}
	return next, nil
}

func appendRunnerLedgerEntrySuccessRecord(ctx context.Context, data runnerLedgerEntrySuccessData, record EvidenceRecord, plan StatementPlan) (runnerLedgerEntrySuccessData, Digest, error) {
	if ctx == nil || data.evidence == nil || data.journal == nil || !data.cursor.Valid() ||
		validateEvidenceRecord(record) != nil || plan.validateExact() != nil || !runnerLedgerEntrySuccessEvidenceMatches(data) {
		return data, "", fail(CodeEvidenceRecoveryRequired, "runner-ledger-entry-success-append", "evidence append inputs are unavailable", nil)
	}
	request, err := mintRunnerLedgerEntrySuccessEvidenceRequest(
		data.evidence, data.candidateBinding, data.generation, data.recoveryDigest,
		data.cursor, record, plan, data.maxAttempts,
	)
	if err != nil {
		return data, "", err
	}
	journal, cursor, owned, err := data.evidence.bindRunnerLedgerEntrySuccessRecord(ctx, request)
	if err != nil {
		return data, "", err
	}
	if journal == nil || owned == nil || !sameRunnerOwnedPointer(journal, data.journal) || !sameCursorIdentity(cursor, data.cursor) {
		return data, "", fail(CodeEvidenceRecoveryRequired, "runner-ledger-entry-success-append", "evidence binder returned foreign authority", nil)
	}
	oldCursor := data.cursor.clone()
	data.mutationAttempted = true
	result, appendErr := journal.AppendDurable(ctx, cursor, owned)
	if appendErr != nil {
		return data, "", runnerLedgerEntrySuccessAppendFailure(oldCursor, result, appendErr)
	}
	nextCursor, recordDigest, err := validateRunnerLedgerEntrySuccessAppendResult(oldCursor, data.generation, record, result)
	if err != nil {
		return data, "", err
	}
	data.cursor = nextCursor
	snapshot := data.evidence.RecoverySnapshot()
	if snapshot == nil || !validRecoverySnapshotForJournal(snapshot, data.generation, nextCursor) ||
		!sameCursorIdentity(snapshot.cursor, nextCursor) || snapshot.tailDigest != recordDigest {
		nextCursor.valid.Store(false)
		return data, "", fail(CodeEvidenceJournalFailed, "runner-ledger-entry-success-append", "durable evidence snapshot differs from the returned cursor", nil)
	}
	digest := generationJournalRecoveryDigest(snapshot)
	if digest == ([32]byte{}) {
		nextCursor.valid.Store(false)
		return data, "", fail(CodeEvidenceJournalFailed, "runner-ledger-entry-success-append", "durable recovery snapshot could not be identified", nil)
	}
	data.recoveryDigest = digest
	data.recoveryTail = recordDigest
	return data, recordDigest, nil
}

func runnerLedgerEntrySuccessAppendFailure(cursor JournalCursor, result AppendResult, appendErr error) error {
	if !cursor.Valid() || result.outcome == appendOutcomeUnknown || result.durableCursor != nil || result.candidateRecordDigest.Validate() == nil {
		return fail(CodeEvidenceRecoveryRequired, "runner-ledger-entry-success-append", "evidence mutation outcome requires strict reopen", nil)
	}
	if errors.Is(appendErr, context.Canceled) {
		return fail(CodeContextCanceled, "runner-ledger-entry-success-append", "evidence append was canceled before mutation", nil)
	}
	if errors.Is(appendErr, context.DeadlineExceeded) {
		return fail(CodeDeadlineExceeded, "runner-ledger-entry-success-append", "evidence append deadline expired before mutation", nil)
	}
	var stable *Error
	if errors.As(appendErr, &stable) {
		return fail(stable.Code, "runner-ledger-entry-success-append", "evidence append failed before mutation", nil)
	}
	return fail(CodeEvidenceJournalFailed, "runner-ledger-entry-success-append", "evidence append failed before mutation", nil)
}

func validateRunnerLedgerEntrySuccessAppendResult(cursor JournalCursor, generation generationIdentity, record EvidenceRecord, result AppendResult) (JournalCursor, Digest, error) {
	failResult := func(message string) (JournalCursor, Digest, error) {
		if cursor.valid != nil {
			cursor.valid.Store(false)
		}
		if result.durableCursor != nil && result.durableCursor.valid != nil {
			result.durableCursor.valid.Store(false)
		}
		return JournalCursor{}, "", fail(CodeEvidenceRecoveryRequired, "runner-ledger-entry-success-append-result", message, nil)
	}
	if result.outcome != appendOutcomeDurable || result.durableCursor == nil || !result.durableCursor.Valid() || cursor.Valid() ||
		!sameGenerationIdentity(result.durableCursor.generation, generation) || result.candidateRecordDigest.Validate() != nil ||
		result.candidateCheckpointRecordDigest.Validate() != nil || result.candidatePreviousRecordDigest == nil {
		return failResult("durable append result is unavailable or contradictory")
	}
	rotated := result.rotationHeaderRecordDigest != nil || result.rotationHeaderCheckpointRecordDigest != nil
	if (result.rotationHeaderRecordDigest == nil) != (result.rotationHeaderCheckpointRecordDigest == nil) {
		return failResult("rotation diagnosis is one-sided")
	}
	wantSequence := cursor.nextSequence
	wantSegment := cursor.segmentIndex
	wantNextSequence := cursor.nextSequence + 1
	wantIndexNext := cursor.lineageIndexNextSequence + 1
	wantPrevious := cloneDigestPointer(cursor.previousRecordDigest)
	if rotated {
		if result.rotationHeaderRecordDigest.Validate() != nil || result.rotationHeaderCheckpointRecordDigest.Validate() != nil {
			return failResult("rotation identity is invalid")
		}
		wantSequence++
		wantSegment++
		wantNextSequence++
		wantIndexNext++
		wantPrevious = cloneDigestPointer(result.rotationHeaderRecordDigest)
	}
	if result.candidateSequence != wantSequence || !equalDigestPointer(result.candidatePreviousRecordDigest, wantPrevious) ||
		result.durableCursor.segmentIndex != wantSegment || result.durableCursor.nextSequence != wantNextSequence ||
		result.durableCursor.lineageIndexNextSequence != wantIndexNext ||
		result.durableCursor.previousRecordDigest == nil || *result.durableCursor.previousRecordDigest != result.candidateRecordDigest ||
		result.durableCursor.latestCheckpointRecordDigest == nil || *result.durableCursor.latestCheckpointRecordDigest != result.candidateCheckpointRecordDigest ||
		result.durableCursor.lineageIndexPreviousRecordDigest != result.candidateCheckpointRecordDigest ||
		result.durableCursor.valid == cursor.valid {
		return failResult("durable cursor does not describe the exact composite append")
	}
	frame := EvidenceFrame{
		FormatVersion: EvidenceFrameFormat, Sequence: result.candidateSequence,
		PreviousRecordDigest: cloneDigestPointer(result.candidatePreviousRecordDigest),
		RecordKind:           admissionEvidenceRecordKind(record), Record: cloneEvidenceRecord(record),
	}
	computed, err := frame.ComputeDigest()
	frame.RecordDigest = computed
	if err != nil || frame.Validate() != nil || computed != result.candidateRecordDigest {
		return failResult("candidate record digest differs from the exact durable frame")
	}
	return result.durableCursor.clone(), result.candidateRecordDigest, nil
}

func admissionEvidenceRecordKind(record EvidenceRecord) EvidenceRecordKind {
	switch {
	case record.StatementIntent != nil:
		return EvidenceRecordStatementIntent
	case record.Intermediate != nil:
		return EvidenceRecordIntermediate
	case record.CommitIntent != nil:
		return EvidenceRecordCommitIntent
	case record.AttemptTerminal != nil:
		return EvidenceRecordAttemptTerminal
	default:
		return ""
	}
}

func runnerLedgerEntrySuccessIntentRecoveryMatches(data runnerLedgerEntrySuccessData) bool {
	snapshot := data.evidence.RecoverySnapshot()
	if snapshot == nil || snapshot.state != RecoveryDanglingStatementIntent || snapshot.migrationID == nil ||
		*snapshot.migrationID != data.intent.MigrationID || snapshot.attemptIndex == nil || *snapshot.attemptIndex != 1 ||
		snapshot.lastStatementIntent == nil || snapshot.lastStatementIntentRecordDigest == nil ||
		*snapshot.lastStatementIntentRecordDigest != data.intentRecordDigest || snapshot.tailDigest != data.intentRecordDigest ||
		snapshot.lastStatementIntent.recordDigest != data.intentRecordDigest ||
		!runnerCanonicalEqual(snapshot.lastStatementIntent.value, data.intent) || snapshot.lastIntermediateEvidence != nil ||
		snapshot.commitIntent != nil || snapshot.lastTerminal != nil {
		return false
	}
	return generationJournalRecoveryDigest(snapshot) == data.recoveryDigest && sameCursorIdentity(snapshot.cursor, data.cursor)
}

func (runner *Runner) executeRunnerLedgerEntrySuccessStatement(ctx context.Context, durable *runnerLedgerEntrySuccessState) (*runnerLedgerEntrySuccessState, error) {
	data, err := consumeRunnerLedgerEntrySuccessState(durable, runnerLedgerEntrySuccessIntentDurable)
	if err != nil {
		return nil, closeRunnerLedgerEntrySuccessData(data, err)
	}
	failClosed := func(primary error) (*runnerLedgerEntrySuccessState, error) {
		return nil, closeRunnerLedgerEntrySuccessData(data, primary)
	}
	if runner == nil || ctx == nil || data.statementIndex >= uint32(len(data.plans)) {
		return failClosed(fail(CodeEvidenceRecoveryRequired, "runner-ledger-entry-success-execute", "durable intent cannot enter statement execution", nil))
	}
	plan := data.plans[data.statementIndex]
	sql, err := plan.exactSQLBytes()
	if err != nil || DigestBytes(sql) != data.intent.StatementSHA256 || data.intent.StatementSHA256 != plan.StatementSHA256 ||
		!planMatchesIntent(exactStatementWitnessFromPlan(plan, 1), data.intent) {
		return failClosed(fail(CodeEvidenceRecoveryRequired, "runner-ledger-entry-success-execute", "owned SQL bytes differ from the durable intent", nil))
	}
	data.mutationAttempted = true
	if err := data.transaction.ExecuteStatement(ctx, append([]byte(nil), sql...)); err != nil {
		return failClosed(fail(CodeEvidenceRecoveryRequired, "runner-ledger-entry-success-execute", "statement execution failed after durable intent", nil))
	}
	status, statusOK := migrationProjectionTxStatus(data.transaction)
	boundary, boundaryErr := data.transaction.Boundary(ctx, data.key)
	if boundaryErr != nil || !statusOK || status != 'T' || boundary.TxStatus != 'T' || boundary.CurrentUser != MigrationOwnerRole || !boundary.LockHeld {
		return failClosed(fail(CodeEvidenceRecoveryRequired, "runner-ledger-entry-success-execute", "statement execution lost its exact transaction boundary", nil))
	}
	if !runnerLedgerEntrySuccessEvidenceMatches(data) {
		return failClosed(fail(CodeEvidenceRecoveryRequired, "runner-ledger-entry-success-execute", "durable intent changed during statement execution", nil))
	}
	data.phase = runnerLedgerEntrySuccessStatementExecuted
	next, err := sealRunnerLedgerEntrySuccessState(data, runnerLedgerEntrySuccessIntentDurable, "execute_exact_statement")
	if err != nil {
		return failClosed(err)
	}
	return next, nil
}

func (runner *Runner) appendRunnerLedgerEntrySuccessIntermediate(ctx context.Context, executed *runnerLedgerEntrySuccessState) (*runnerLedgerEntrySuccessState, error) {
	data, err := consumeRunnerLedgerEntrySuccessState(executed, runnerLedgerEntrySuccessStatementExecuted)
	if err != nil {
		return nil, closeRunnerLedgerEntrySuccessData(data, err)
	}
	failClosed := func(primary error) (*runnerLedgerEntrySuccessState, error) {
		return nil, closeRunnerLedgerEntrySuccessData(data, primary)
	}
	if runner == nil || ctx == nil || data.statementIndex >= uint32(len(data.plans)) {
		return failClosed(fail(CodeEvidenceRecoveryRequired, "runner-ledger-entry-success-intermediate", "statement-after inputs are unavailable", nil))
	}
	plan := data.plans[data.statementIndex]
	catalog, ok := exactCatalogBindingForHead(data.bindings.executableCatalogs, plan.MigrationID)
	if !ok || catalog.verifiedCatalog.validate() != nil {
		return failClosed(fail(CodeUntrusted, "runner-ledger-entry-success-intermediate", "selected catalog authority is unavailable", nil))
	}
	afterSeed := runnerProjectedCurrentStatementAfterSeed{
		transaction: data.transaction, key: data.key, generation: data.generation, database: data.database,
		policy: cloneProjectionValue(data.bundle.Manifest.ExecutionPolicy), plan: plan, intent: cloneProjectionValue(data.intent),
		verifiedAuthority:        cloneVerifiedAuthorityContract(data.bindings.verifiedAuthority),
		verifiedCatalog:          cloneVerifiedCatalogContract(catalog.verifiedCatalog),
		runnerProjectionDecision: data.generation.runnerProjectionDecisionDigest,
	}
	after, err := runner.projectRunnerStatementAfter(ctx, afterSeed)
	if err != nil {
		return failClosed(err)
	}
	boundary, boundaryErr := data.transaction.Boundary(ctx, data.key)
	status, statusOK := migrationProjectionTxStatus(data.transaction)
	if boundaryErr != nil || !statusOK || status != 'T' || boundary.TxStatus != 'T' || boundary.CurrentUser != MigrationOwnerRole || !boundary.LockHeld {
		return failClosed(fail(CodeEvidenceRecoveryRequired, "runner-ledger-entry-success-intermediate", "statement-after projection lost its exact transaction boundary", nil))
	}
	state, err := buildRunnerStatementAfterState(afterSeed, after, boundary)
	if err != nil {
		return failClosed(err)
	}
	intermediate := StatementIntermediateEvidence{
		State: cloneProjectionValue(state), AuthorityBeforeResult: cloneProjectionValue(data.intent.AuthorityBeforeResult),
		CatalogBeforeResult:  cloneProjectionValue(data.intent.CatalogBeforeResult),
		AuthorityAfterResult: ProjectionResultEvidence{Digest: after.authority.Digest, Metadata: cloneProjectionValue(after.authority.Metadata)},
		CatalogAfterResult:   ProjectionResultEvidence{Digest: after.catalog.Digest, Metadata: cloneProjectionValue(after.catalog.Metadata)},
	}
	final := data.statementIndex+1 == uint32(len(data.plans))
	if final {
		preledgerSeed := runnerProjectedCurrentPreledgerSeed{
			transaction: data.transaction, key: data.key, generation: data.generation, database: data.database,
			policy: cloneProjectionValue(data.bundle.Manifest.ExecutionPolicy), plan: plan, intent: cloneProjectionValue(data.intent),
			state:                  cloneProjectionValue(state),
			authorityAfter:         ProjectionResultEvidence{Digest: after.authority.Digest, Metadata: cloneProjectionValue(after.authority.Metadata)},
			catalogAfter:           ProjectionResultEvidence{Digest: after.catalog.Digest, Metadata: cloneProjectionValue(after.catalog.Metadata)},
			catalogAfterProjection: cloneProjectionValue(after.catalog.Projection),
			verifiedAuthority:      cloneVerifiedAuthorityContract(data.bindings.verifiedAuthority),
			verifiedCatalog:        cloneVerifiedCatalogContract(catalog.verifiedCatalog),
		}
		preledger, projectionErr := runner.projectRunnerCurrentPreledger(ctx, preledgerSeed)
		if projectionErr != nil {
			return failClosed(projectionErr)
		}
		boundary, boundaryErr = data.transaction.Boundary(ctx, data.key)
		status, statusOK = migrationProjectionTxStatus(data.transaction)
		if boundaryErr != nil || !statusOK || status != 'T' || boundary.TxStatus != 'T' || boundary.CurrentUser != MigrationOwnerRole || !boundary.LockHeld {
			return failClosed(fail(CodeEvidenceRecoveryRequired, "runner-ledger-entry-success-preledger", "preledger projection lost its exact transaction boundary", nil))
		}
		preledgerState, stateErr := buildRunnerPreledgerState(preledgerSeed, preledger, boundary)
		if stateErr != nil || !runnerCanonicalEqual(preledgerState, state) {
			return failClosed(fail(CodeIntermediateStateMismatch, "runner-ledger-entry-success-preledger", "preledger state differs from immediate statement-after", stateErr))
		}
		preledgerAuthority := ProjectionResultEvidence{Digest: preledger.authority.Digest, Metadata: cloneProjectionValue(preledger.authority.Metadata)}
		preledgerCatalog := ProjectionResultEvidence{Digest: preledger.catalog.Digest, Metadata: cloneProjectionValue(preledger.catalog.Metadata)}
		intermediate.PreledgerAuthorityResult = &preledgerAuthority
		intermediate.PreledgerCatalogResult = &preledgerCatalog
	}
	if !runnerLedgerEntrySuccessIntermediateMatches(plan, data.intent, intermediate, final) {
		return failClosed(fail(CodeIntermediateStateMismatch, "runner-ledger-entry-success-intermediate", "intermediate evidence differs from the exact statement transition", nil))
	}
	data.authorityAfterProjection = cloneProjectionValue(after.authority.Projection)
	data.catalogAfterProjection = cloneProjectionValue(after.catalog.Projection)
	data.authorityAfter = cloneProjectionValue(intermediate.AuthorityAfterResult)
	data.catalogAfter = cloneProjectionValue(intermediate.CatalogAfterResult)
	data.intermediate = cloneProjectionValue(intermediate)
	record := EvidenceRecord{Intermediate: cloneStatementIntermediatePointer(&intermediate)}
	data, recordDigest, err := appendRunnerLedgerEntrySuccessRecord(ctx, data, record, plan)
	if err != nil {
		return nil, closeRunnerLedgerEntrySuccessData(data, err)
	}
	data.intermediateRecordDigest = recordDigest
	data.currentCatalogDigest = intermediate.CatalogAfterResult.Digest
	if !runnerLedgerEntrySuccessIntermediateRecoveryMatches(data) {
		return nil, closeRunnerLedgerEntrySuccessData(data,
			fail(CodeEvidenceJournalFailed, "runner-ledger-entry-success-intermediate", "durable intermediate recovery boundary is contradictory", nil))
	}
	if final {
		data.phase = runnerLedgerEntrySuccessFinalIntermediateDurable
		next, sealErr := sealRunnerLedgerEntrySuccessState(data, runnerLedgerEntrySuccessStatementExecuted, "append_intermediate_final")
		if sealErr != nil {
			return failClosed(sealErr)
		}
		return next, nil
	}
	data.phase = runnerLedgerEntrySuccessIntermediateDurable
	next, sealErr := sealRunnerLedgerEntrySuccessState(data, runnerLedgerEntrySuccessStatementExecuted, "append_intermediate_nonfinal")
	if sealErr != nil {
		return failClosed(sealErr)
	}
	return next, nil
}

func cloneStatementIntermediatePointer(value *StatementIntermediateEvidence) *StatementIntermediateEvidence {
	if value == nil {
		return nil
	}
	owned := cloneProjectionValue(*value)
	return &owned
}

func runnerLedgerEntrySuccessIntermediateMatches(plan StatementPlan, intent StatementIntent, intermediate StatementIntermediateEvidence, final bool) bool {
	if plan.validateExact() != nil || intent.Validate() != nil || intermediate.Validate() != nil ||
		!planMatchesIntent(exactStatementWitnessFromPlan(plan, 1), intent) ||
		(intermediate.PreledgerAuthorityResult != nil) != final || (intermediate.PreledgerCatalogResult != nil) != final ||
		!projectionEvidenceEqual(intermediate.AuthorityBeforeResult, intent.AuthorityBeforeResult) ||
		!projectionEvidenceEqual(intermediate.CatalogBeforeResult, intent.CatalogBeforeResult) {
		return false
	}
	state := intermediate.State
	if state.MigrationID != plan.MigrationID || state.AttemptIndex != 1 || state.StatementIndex != plan.StatementIndex ||
		state.StatementSHA256 != plan.StatementSHA256 || state.SchemaBundleDigest != intent.SchemaBundleDigest ||
		state.CatalogContractDigest != intent.CatalogContractDigest || state.AuthorityProfileDigest != intent.AuthorityProfileDigest ||
		state.AuthorityBindingDigest != intent.AuthorityBindingDigest || state.ControlPlaneStates.ExpectedTransitionDigest != plan.ExpectedTransitionDigest ||
		!equalDigestPointer(state.PreviousIntermediateStateDigest, intent.PreviousIntermediateStateDigest) ||
		state.AuthorityBeforeDigest != intent.AuthorityBeforeDigest || state.CatalogBeforeDigest != intent.CatalogBeforeDigest ||
		state.AuthorityAfterDigest != intermediate.AuthorityAfterResult.Digest || state.CatalogAfterDigest != intermediate.CatalogAfterResult.Digest ||
		!runnerIntermediateSnapshotPair(intermediate.AuthorityAfterResult.Metadata, intermediate.CatalogAfterResult.Metadata, plan.MigrationID, &plan.StatementIndex) {
		return false
	}
	if final {
		return runnerFinalIntermediateShapeMatches(plan, intent, intermediate)
	}
	return intermediate.PreledgerAuthorityResult == nil && intermediate.PreledgerCatalogResult == nil
}

func runnerLedgerEntrySuccessIntermediateRecoveryMatches(data runnerLedgerEntrySuccessData) bool {
	snapshot := data.evidence.RecoverySnapshot()
	if snapshot == nil || snapshot.state != RecoveryDanglingIntermediate || snapshot.migrationID == nil ||
		*snapshot.migrationID != data.intermediate.State.MigrationID || snapshot.attemptIndex == nil || *snapshot.attemptIndex != 1 ||
		snapshot.lastStatementIntent == nil || snapshot.lastIntermediateEvidence == nil ||
		snapshot.lastStatementIntentRecordDigest == nil || *snapshot.lastStatementIntentRecordDigest != data.intentRecordDigest ||
		snapshot.lastIntermediateEvidenceRecordDigest == nil || *snapshot.lastIntermediateEvidenceRecordDigest != data.intermediateRecordDigest ||
		snapshot.lastIntermediateStateDigest == nil || *snapshot.lastIntermediateStateDigest != data.intermediate.State.IntermediateStateDigest ||
		snapshot.tailDigest != data.intermediateRecordDigest ||
		!runnerCanonicalEqual(snapshot.lastStatementIntent.value, data.intent) ||
		!runnerCanonicalEqual(snapshot.lastIntermediateEvidence.value, data.intermediate) || snapshot.commitIntent != nil || snapshot.lastTerminal != nil {
		return false
	}
	return generationJournalRecoveryDigest(snapshot) == data.recoveryDigest && sameCursorIdentity(snapshot.cursor, data.cursor)
}

func (runner *Runner) insertRunnerLedgerEntrySuccessLedger(ctx context.Context, durable *runnerLedgerEntrySuccessState) (*runnerLedgerEntrySuccessState, error) {
	data, err := consumeRunnerLedgerEntrySuccessState(durable, runnerLedgerEntrySuccessFinalIntermediateDurable)
	if err != nil {
		return nil, closeRunnerLedgerEntrySuccessData(data, err)
	}
	failClosed := func(primary error) (*runnerLedgerEntrySuccessState, error) {
		return nil, closeRunnerLedgerEntrySuccessData(data, primary)
	}
	if runner == nil || ctx == nil {
		return failClosed(fail(CodeEvidenceRecoveryRequired, "runner-ledger-entry-success-ledger", "ledger mutation inputs are unavailable", nil))
	}
	bundle, err := verifiedRunnerLedgerCatalogBundle(data.bundle)
	if err != nil {
		return failClosed(err)
	}
	adapter, ok := data.transaction.(runnerTransactionLedger)
	if !ok || adapter == nil {
		return failClosed(fail(CodeInvalidLedger, "runner-ledger-entry-success-ledger", "transaction lacks the sealed ledger adapter", nil))
	}
	expectedRow := commitIntentLedgerRow(data.entry, data.generation.schemaBundleDigest)
	if expectedRow.Validate() != nil {
		return failClosed(fail(CodeInvalidLedger, "runner-ledger-entry-success-ledger", "selected signed ledger row is invalid", nil))
	}
	data.mutationAttempted = true
	rows, ledgerErr := adapter.insertAndReadRunnerLedgerRow(ctx, data.entry, data.generation.schemaBundleDigest)
	if ledgerErr != nil {
		return failClosed(fail(CodeEvidenceRecoveryRequired, "runner-ledger-entry-success-ledger", "ledger insert or readback failed after mutation attempt", nil))
	}
	facts, err := validateRunnerLedgerEntrySuccessReadback(rows, bundle, data, expectedRow)
	if err != nil {
		return failClosed(err)
	}
	boundary, boundaryErr := data.transaction.Boundary(ctx, data.key)
	status, statusOK := migrationProjectionTxStatus(data.transaction)
	if boundaryErr != nil || !statusOK || status != 'T' || boundary.TxStatus != 'T' || boundary.CurrentUser != MigrationOwnerRole || !boundary.LockHeld {
		return failClosed(fail(CodeEvidenceRecoveryRequired, "runner-ledger-entry-success-ledger", "ledger readback lost its exact transaction boundary", nil))
	}
	if !runnerLedgerEntrySuccessEvidenceMatches(data) {
		return failClosed(fail(CodeEvidenceRecoveryRequired, "runner-ledger-entry-success-ledger", "durable final intermediate changed during ledger mutation", nil))
	}
	data.ledgerRows = facts.rows
	data.ledgerPrefixDigest = facts.digest
	data.ledgerHead = facts.head
	data.ledgerLength = facts.length
	data.phase = runnerLedgerEntrySuccessLedgerReadbackReady
	next, err := sealRunnerLedgerEntrySuccessState(data, runnerLedgerEntrySuccessFinalIntermediateDurable, "insert_and_readback_ledger")
	if err != nil {
		return failClosed(err)
	}
	return next, nil
}

type runnerLedgerEntrySuccessReadbackFacts struct {
	rows   []CommitIntentLedgerRow
	digest Digest
	head   string
	length uint32
}

func validateRunnerLedgerEntrySuccessReadback(rows []LedgerRow, bundle *RuntimeBundle, data runnerLedgerEntrySuccessData, expectedRow CommitIntentLedgerRow) (runnerLedgerEntrySuccessReadbackFacts, error) {
	var facts runnerLedgerEntrySuccessReadbackFacts
	if bundle == nil || uint64(len(rows)) > uint64(^uint32(0)) || uint32(len(rows)) != data.ledgerBeforeLength+1 ||
		int(data.ledgerBeforeLength) >= len(rows) {
		return facts, fail(CodeInvalidLedger, "runner-ledger-entry-success-ledger", "ledger readback length differs from the exact predecessor plus selected row", nil)
	}
	ownedRows := cloneProjectionValue(rows)
	snapshot, err := ValidateLedger(ownedRows, bundle.Lineage)
	if err != nil || snapshot.Head != data.entry.ID {
		return facts, fail(CodeInvalidLedger, "runner-ledger-entry-success-ledger", "ledger readback is not the exact signed prefix", nil)
	}
	converted := make([]CommitIntentLedgerRow, len(ownedRows))
	for index := range ownedRows {
		converted[index], err = commitIntentLedgerRowFromObserved(ownedRows[index])
		if err != nil {
			return facts, err
		}
	}
	if !runnerCanonicalEqual(converted[len(converted)-1], expectedRow) {
		return facts, fail(CodeInvalidLedger, "runner-ledger-entry-success-ledger", "ledger readback selected row differs from the signed entry", nil)
	}
	predecessorDigest, err := LedgerPrefixDigest(converted[:data.ledgerBeforeLength])
	if err != nil || predecessorDigest != data.ledgerBeforeDigest {
		return facts, fail(CodeInvalidLedger, "runner-ledger-entry-success-ledger", "ledger readback predecessor digest changed", nil)
	}
	predecessorHead := ""
	if data.ledgerBeforeLength > 0 {
		predecessorHead = converted[data.ledgerBeforeLength-1].MigrationID
	}
	if predecessorHead != data.ledgerBeforeHead {
		return facts, fail(CodeInvalidLedger, "runner-ledger-entry-success-ledger", "ledger readback predecessor head changed", nil)
	}
	digest, err := LedgerPrefixDigest(converted)
	if err != nil {
		return facts, err
	}
	return runnerLedgerEntrySuccessReadbackFacts{rows: converted, digest: digest, head: snapshot.Head, length: uint32(len(converted))}, nil
}

func (runner *Runner) appendRunnerLedgerEntrySuccessCommitIntent(ctx context.Context, readback *runnerLedgerEntrySuccessState) (*runnerLedgerEntrySuccessState, error) {
	data, err := consumeRunnerLedgerEntrySuccessState(readback, runnerLedgerEntrySuccessLedgerReadbackReady)
	if err != nil {
		return nil, closeRunnerLedgerEntrySuccessData(data, err)
	}
	failClosed := func(primary error) (*runnerLedgerEntrySuccessState, error) {
		return nil, closeRunnerLedgerEntrySuccessData(data, primary)
	}
	if runner == nil || ctx == nil || len(data.plans) == 0 || data.intermediate.Validate() != nil || data.intermediate.PreledgerCatalogResult == nil {
		return failClosed(fail(CodeEvidenceRecoveryRequired, "runner-ledger-entry-success-commit-intent", "commit-intent inputs are unavailable", nil))
	}
	expectedRow := commitIntentLedgerRow(data.entry, data.generation.schemaBundleDigest)
	commit := CommitIntent{
		SchemaBundleDigest:     data.generation.schemaBundleDigest,
		CatalogContractDigest:  data.intent.CatalogContractDigest,
		AuthorityProfileDigest: data.bindings.authorityProfileDigest,
		AuthorityBindingDigest: data.bindings.authorityBindingDigest,
		MigrationID:            data.entry.ID, AttemptIndex: 1, PreviousAttemptTerminalDigest: nil,
		AttemptPredecessorCatalogDigest: data.firstCatalogBeforeDigest,
		LastIntermediateStateDigest:     data.intermediate.State.IntermediateStateDigest,
		ExpectedLedgerLength:            data.ledgerLength, ExpectedLedgerHead: data.ledgerHead,
		LedgerRow: cloneProjectionValue(expectedRow),
	}
	if commit.Validate() != nil || commit.ExpectedLedgerLength != data.ledgerBeforeLength+1 ||
		commit.ExpectedLedgerHead != data.entry.ID || commit.AttemptPredecessorCatalogDigest != data.plans[0].ExpectedTransition.CatalogBefore.Digest ||
		!runnerCanonicalEqual(commit.LedgerRow, data.ledgerRows[len(data.ledgerRows)-1]) {
		return failClosed(fail(CodeInvalidLedger, "runner-ledger-entry-success-commit-intent", "commit intent differs from the exact ledger readback", nil))
	}
	data.commit = cloneProjectionValue(commit)
	record := EvidenceRecord{CommitIntent: cloneCommitIntentPointer(&commit)}
	data, recordDigest, err := appendRunnerLedgerEntrySuccessRecord(ctx, data, record, data.plans[len(data.plans)-1])
	if err != nil {
		return nil, closeRunnerLedgerEntrySuccessData(data, err)
	}
	data.commitRecordDigest = recordDigest
	if !runnerLedgerEntrySuccessCommitRecoveryMatches(data) {
		return nil, closeRunnerLedgerEntrySuccessData(data,
			fail(CodeEvidenceJournalFailed, "runner-ledger-entry-success-commit-intent", "durable commit-intent recovery boundary is contradictory", nil))
	}
	data.phase = runnerLedgerEntrySuccessCommitIntentDurable
	next, err := sealRunnerLedgerEntrySuccessState(data, runnerLedgerEntrySuccessLedgerReadbackReady, "append_commit_intent")
	if err != nil {
		return failClosed(err)
	}
	return next, nil
}

func cloneCommitIntentPointer(value *CommitIntent) *CommitIntent {
	if value == nil {
		return nil
	}
	owned := cloneProjectionValue(*value)
	return &owned
}

func runnerLedgerEntrySuccessCommitRecoveryMatches(data runnerLedgerEntrySuccessData) bool {
	snapshot := data.evidence.RecoverySnapshot()
	if snapshot == nil || snapshot.state != RecoveryDanglingCommitIntent || snapshot.nextPermittedAction != RecoveryReconcileCommit ||
		snapshot.migrationID == nil || *snapshot.migrationID != data.commit.MigrationID || snapshot.attemptIndex == nil || *snapshot.attemptIndex != 1 ||
		snapshot.commitIntent == nil || snapshot.lastCommitIntentRecordDigest == nil || *snapshot.lastCommitIntentRecordDigest != data.commitRecordDigest ||
		snapshot.tailDigest != data.commitRecordDigest || !runnerCanonicalEqual(snapshot.commitIntent.value, data.commit) ||
		snapshot.lastStatementIntent == nil || !runnerCanonicalEqual(snapshot.lastStatementIntent.value, data.intent) ||
		snapshot.lastIntermediateEvidence == nil || !runnerCanonicalEqual(snapshot.lastIntermediateEvidence.value, data.intermediate) ||
		snapshot.lastTerminal != nil {
		return false
	}
	return generationJournalRecoveryDigest(snapshot) == data.recoveryDigest && sameCursorIdentity(snapshot.cursor, data.cursor)
}

func (runner *Runner) commitRunnerLedgerEntrySuccess(ctx context.Context, durable *runnerLedgerEntrySuccessState) (*runnerLedgerEntrySuccessState, error) {
	data, err := consumeRunnerLedgerEntrySuccessState(durable, runnerLedgerEntrySuccessCommitIntentDurable)
	if err != nil {
		return nil, closeRunnerLedgerEntrySuccessData(data, err)
	}
	if runner == nil || ctx == nil {
		return nil, closeRunnerLedgerEntrySuccessData(data,
			fail(CodeEvidenceRecoveryRequired, "runner-ledger-entry-success-commit", "commit context is unavailable after durable commit intent", nil))
	}
	protocol, ok := data.transaction.(runnerCommitProtocol)
	if !ok || protocol == nil {
		return nil, closeRunnerLedgerEntrySuccessData(data,
			fail(CodeEvidenceRecoveryRequired, "runner-ledger-entry-success-commit", "transaction lacks the sealed commit protocol", nil))
	}
	observation, commitCalled, invokeErr := invokeRunnerCommitProtocol(ctx, data.transaction)
	if !commitCalled {
		if invokeErr == nil {
			invokeErr = fail(CodeTransactionBoundary, "runner-ledger-entry-success-commit", "commit protocol did not invoke commit", nil)
		}
		return nil, closeRunnerLedgerEntrySuccessData(data,
			fail(CodeEvidenceRecoveryRequired, "runner-ledger-entry-success-commit", "commit intent requires strict reopen before another mutation", nil))
	}
	data.mutationAttempted = true
	alreadyClosed := protocol.runnerCommitProtocolConnectionClosed()
	closeProven := closeRunnerPostCommitSession(data.session, alreadyClosed)
	data.session = nil
	data.transaction = nil
	if invokeErr != nil || observation == nil {
		return nil, runnerLedgerEntrySuccessPostCommitFailure(data, "runner-ledger-entry-success-commit", "commit outcome or session close is not safely reusable", nil)
	}
	facts, consumeErr := consumeRunnerCommitProtocolObservation(observation, protocol)
	if consumeErr != nil || !validRunnerCommitProtocolFacts(facts) || facts.outcome != runnerCommitProtocolCommitted || !closeProven {
		return nil, runnerLedgerEntrySuccessPostCommitFailure(data, "runner-ledger-entry-success-commit", "only a known committed outcome with closed old session may advance", nil)
	}
	if !runnerLedgerEntrySuccessEvidenceMatches(data) {
		return nil, runnerLedgerEntrySuccessPostCommitFailure(data, "runner-ledger-entry-success-commit", "durable commit intent changed across database commit", nil)
	}
	data.commitFacts = facts
	data.phase = runnerLedgerEntrySuccessCommitKnownCommitted
	next, err := sealRunnerLedgerEntrySuccessState(data, runnerLedgerEntrySuccessCommitIntentDurable, "commit_known")
	if err != nil {
		return nil, err
	}
	return next, nil
}

func (runner *Runner) appendRunnerLedgerEntrySuccessTerminal(ctx context.Context, committed *runnerLedgerEntrySuccessState) (*runnerLedgerEntrySuccessState, error) {
	data, err := consumeRunnerLedgerEntrySuccessState(committed, runnerLedgerEntrySuccessCommitKnownCommitted)
	if err != nil {
		return nil, runnerLedgerEntrySuccessPostCommitFailure(data, "runner-ledger-entry-success-terminal", "known commit state changed before terminal append", err)
	}
	if runner == nil || ctx == nil || data.commit.Validate() != nil || data.intermediate.Validate() != nil {
		return nil, runnerLedgerEntrySuccessPostCommitFailure(data, "runner-ledger-entry-success-terminal", "known commit terminal inputs are unavailable", nil)
	}
	lastIntermediate := data.intermediate.State.IntermediateStateDigest
	terminal := AttemptTerminalState{
		SchemaBundleDigest: data.generation.schemaBundleDigest, CatalogContractDigest: data.commit.CatalogContractDigest,
		AuthorityProfileDigest: data.commit.AuthorityProfileDigest, AuthorityBindingDigest: data.commit.AuthorityBindingDigest,
		MigrationID: data.commit.MigrationID, AttemptIndex: 1, PreviousAttemptTerminalDigest: nil,
		LastIntermediateStateDigest: &lastIntermediate, Outcome: "committed", ReconcileResult: "not_run",
	}
	terminal.TerminalDigest, err = terminal.ComputeDigest()
	if err != nil || terminal.Validate(data.maxAttempts) != nil {
		return nil, runnerLedgerEntrySuccessPostCommitFailure(data, "runner-ledger-entry-success-terminal", "committed terminal cannot be reproduced", nil)
	}
	data.terminal = cloneProjectionValue(terminal)
	record := EvidenceRecord{AttemptTerminal: cloneAttemptTerminalPointer(&terminal)}
	data, recordDigest, err := appendRunnerLedgerEntrySuccessRecord(ctx, data, record, data.plans[len(data.plans)-1])
	if err != nil {
		return nil, runnerLedgerEntrySuccessPostCommitFailure(data, "runner-ledger-entry-success-terminal", "known commit terminal append requires strict reopen", nil)
	}
	data.terminalRecordDigest = recordDigest
	if !runnerLedgerEntrySuccessTerminalRecoveryMatches(data) {
		return nil, runnerLedgerEntrySuccessPostCommitFailure(data, "runner-ledger-entry-success-terminal", "durable terminal recovery boundary is contradictory", nil)
	}
	data.phase = runnerLedgerEntrySuccessTerminalDurable
	next, err := sealRunnerLedgerEntrySuccessState(data, runnerLedgerEntrySuccessCommitKnownCommitted, "append_terminal_durable")
	if err != nil {
		return nil, runnerLedgerEntrySuccessPostCommitFailure(data, "runner-ledger-entry-success-terminal", "durable terminal state could not be sealed", err)
	}
	return next, nil
}

func cloneAttemptTerminalPointer(value *AttemptTerminalState) *AttemptTerminalState {
	if value == nil {
		return nil
	}
	owned := cloneProjectionValue(*value)
	return &owned
}

func runnerLedgerEntrySuccessTerminalRecoveryMatches(data runnerLedgerEntrySuccessData) bool {
	snapshot := data.evidence.RecoverySnapshot()
	if snapshot == nil || snapshot.migrationID == nil || *snapshot.migrationID != data.terminal.MigrationID ||
		snapshot.attemptIndex == nil || *snapshot.attemptIndex != 1 || snapshot.lastTerminal == nil ||
		snapshot.lastTerminalDigest == nil || *snapshot.lastTerminalDigest != data.terminal.TerminalDigest ||
		snapshot.lastTerminal.recordDigest != data.terminalRecordDigest || snapshot.tailDigest != data.terminalRecordDigest ||
		!runnerCanonicalEqual(snapshot.lastTerminal.value, data.terminal) || snapshot.commitIntent == nil ||
		!runnerCanonicalEqual(snapshot.commitIntent.value, data.commit) {
		return false
	}
	complete := data.selection.entryIndex+1 == uint32(len(data.bundle.Manifest.SchemaBundle.Migrations))
	if complete {
		if snapshot.state != RecoveryCompleted || snapshot.nextPermittedAction != RecoveryReturnSuccess {
			return false
		}
	} else if snapshot.state != RecoveryTerminal || snapshot.nextPermittedAction != RecoveryBeginFirstAttemptNextEntry {
		return false
	}
	return generationJournalRecoveryDigest(snapshot) == data.recoveryDigest && sameCursorIdentity(snapshot.cursor, data.cursor)
}

func finishRunnerLedgerEntrySuccess(terminal *runnerLedgerEntrySuccessState) (runnerLedgerEntrySuccessOutcome, error) {
	data, err := consumeRunnerLedgerEntrySuccessState(terminal, runnerLedgerEntrySuccessTerminalDurable)
	if err != nil {
		return runnerLedgerEntrySuccessOutcome{}, runnerLedgerEntrySuccessPostCommitFailure(data, "runner-ledger-entry-success-result", "durable terminal state changed before classification", err)
	}
	if !runnerLedgerEntrySuccessTerminalRecoveryMatches(data) {
		return runnerLedgerEntrySuccessOutcome{}, runnerLedgerEntrySuccessPostCommitFailure(data, "runner-ledger-entry-success-result", "durable terminal changed before result classification", nil)
	}
	state := runnerLedgerEntrySuccessEntryCommittedNextEntry
	event := "classify_next_entry"
	if data.selection.entryIndex+1 == uint32(len(data.bundle.Manifest.SchemaBundle.Migrations)) {
		state = runnerLedgerEntrySuccessEntryCommittedComplete
		event = "classify_bundle_complete"
	}
	if !runnerLedgerEntrySuccessTransitionAllowed(runnerLedgerEntrySuccessTerminalDurable, event, state) {
		return runnerLedgerEntrySuccessOutcome{}, runnerLedgerEntrySuccessPostCommitFailure(data, "runner-ledger-entry-success-result", "terminal classification is outside the generated registry", nil)
	}
	outcome := runnerLedgerEntrySuccessOutcome{state: state, migrationID: data.entry.ID, ledgerHead: data.ledgerHead, ledgerLength: data.ledgerLength}
	if !outcome.valid() {
		return runnerLedgerEntrySuccessOutcome{}, runnerLedgerEntrySuccessPostCommitFailure(data, "runner-ledger-entry-success-result", "committed entry result is contradictory", nil)
	}
	return outcome, nil
}
