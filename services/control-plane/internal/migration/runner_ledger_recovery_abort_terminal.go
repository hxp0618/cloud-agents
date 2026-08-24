package migration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sync"
	"sync/atomic"
	"time"
)

const runnerLedgerAbortTerminalWriterPermitDigestDomain = "cloud-agents/runner-ledger-abort-terminal-writer/permit/v1"

// runnerLedgerAbortTerminalRecordBinder is the only Slice C bridge to the
// evidence mutation kernel. Recovery admission remains read-only: it must
// first close its retained database lifecycle and mint the one-shot permit
// consumed by this narrower interface.
type runnerLedgerAbortTerminalRecordBinder interface {
	EvidenceSession
	runnerLedgerRecoveryAdmissionClaimBinder
	bindRunnerLedgerRecoveryAbortTerminalRecord(context.Context, *runnerLedgerAbortTerminalWriterPermit) (EvidenceJournal, JournalCursor, *OwnedEvidenceRecord, error)
	runnerLedgerAbortTerminalRecordBinderSealed()
}

type runnerLedgerAbortTerminalAdmissionSeed struct {
	session                  DatabaseSession
	binder                   runnerLedgerAbortTerminalRecordBinder
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

type runnerLedgerAbortTerminalWriterPermit struct {
	self                     *runnerLedgerAbortTerminalWriterPermit
	binder                   runnerLedgerAbortTerminalRecordBinder
	use                      *runnerLedgerRecoveryAdmissionUseRecord
	candidateBinding         *verifiedEvidenceRunBinding
	generation               generationIdentity
	recoveryDigest           [32]byte
	recoveryTail             Digest
	cursor                   JournalCursor
	selection                runnerLedgerRecoveryAdmissionSelection
	receipt                  verifiedPrecommitTerminatedRetry
	terminal                 AttemptTerminalState
	consumerFactSubject      Digest
	evidenceBoundary         [32]byte
	admissionPermitCanonical [32]byte
	projectionSubject        Digest
	database                 runnerPreparedDatabaseIdentity
	consumed                 *atomic.Bool
	canonical                [32]byte
}

type runnerLedgerAbortTerminalWriterPermitRecord struct {
	permit           *runnerLedgerAbortTerminalWriterPermit
	binder           runnerLedgerAbortTerminalRecordBinder
	candidateBinding *verifiedEvidenceRunBinding
	cursorValid      *atomic.Bool
	consumed         *atomic.Bool
	canonical        [32]byte
}

type runnerLedgerAbortTerminalWriterClaim struct {
	binder                   runnerLedgerAbortTerminalRecordBinder
	candidateBinding         *verifiedEvidenceRunBinding
	generation               generationIdentity
	recoveryDigest           [32]byte
	recoveryTail             Digest
	cursor                   JournalCursor
	selection                runnerLedgerRecoveryAdmissionSelection
	receipt                  verifiedPrecommitTerminatedRetry
	terminal                 AttemptTerminalState
	consumerFactSubject      Digest
	evidenceBoundary         [32]byte
	admissionPermitCanonical [32]byte
	projectionSubject        Digest
	database                 runnerPreparedDatabaseIdentity
	canonical                [32]byte
}

var runnerLedgerAbortTerminalWriterPermitRegistry sync.Map

func (runner *Runner) appendRunnerLedgerRecoveryAbortTerminal(ctx context.Context, admission *runnerLedgerAbortTerminalAdmissionPermit, bundle *RuntimeBundle, _ []StatementPlan) error {
	if runner == nil || ctx == nil {
		return fail(CodeEvidenceRecoveryRequired, "runner-ledger-recovery-abort-terminal", "abort-terminal writer context is unavailable", nil)
	}
	if err := contextAdmissionError(ctx); err != nil {
		return err
	}
	seed, err := claimRunnerLedgerAbortTerminalAdmissionPermit(admission)
	if err != nil {
		return err
	}
	receipt, err := runner.revalidateAndCloseRunnerLedgerAbortTerminalAdmission(ctx, seed, bundle)
	if err != nil {
		return err
	}
	permit, err := mintRunnerLedgerAbortTerminalWriterPermit(seed, receipt)
	if err != nil {
		return err
	}
	priorSnapshot := seed.binder.RecoverySnapshot()
	if !runnerLedgerAbortTerminalRecoveryBoundary(priorSnapshot, seed.generation, seed.recoveryDigest, seed.recoveryTail, seed.selection) {
		if permit.cursor.valid != nil {
			permit.cursor.valid.Store(false)
		}
		return fail(CodeEvidenceRecoveryRequired, "runner-ledger-recovery-abort-terminal-bind", "abort-terminal recovery boundary changed before evidence binding", nil)
	}
	expectedTerminal := cloneProjectionValue(permit.terminal)
	journal, cursor, owned, err := seed.binder.bindRunnerLedgerRecoveryAbortTerminalRecord(ctx, permit)
	if err != nil {
		return mapRunnerLedgerRecoveryAbortTerminalError(err, "runner-ledger-recovery-abort-terminal-bind", "abort-terminal record could not be bound")
	}
	if !validRunnerLedgerRecoveryAbortTerminalBoundRecord(seed.binder.Journal(), journal, cursor, owned, permit, expectedTerminal) {
		if permit.cursor.valid != nil {
			permit.cursor.valid.Store(false)
		}
		if cursor.valid != nil {
			cursor.valid.Store(false)
		}
		return fail(CodeEvidenceRecoveryRequired, "runner-ledger-recovery-abort-terminal-bind", "abort-terminal binder returned foreign evidence authority", nil)
	}
	oldCursor := cursor.clone()
	result, appendErr := journal.AppendDurable(ctx, cursor, owned)
	if appendErr != nil {
		return runnerLedgerRecoveryAbortTerminalAppendFailure(oldCursor, result, appendErr)
	}
	nextCursor, recordDigest, err := validateRunnerLedgerRecoveryAbortTerminalAppendResult(oldCursor, seed.generation, EvidenceRecord{AttemptTerminal: cloneAttemptTerminalPointer(&expectedTerminal)}, result)
	if err != nil {
		return err
	}
	snapshot := seed.binder.RecoverySnapshot()
	if !sameRunnerOwnedPointer(seed.binder.Journal(), journal) ||
		!runnerLedgerRecoveryAbortTerminalSnapshotMatches(snapshot, priorSnapshot, seed.generation, nextCursor, recordDigest, expectedTerminal, seed.selection) {
		if nextCursor.valid != nil {
			nextCursor.valid.Store(false)
		}
		return fail(CodeEvidenceRecoveryRequired, "runner-ledger-recovery-abort-terminal-result", "durable abort terminal differs from the exact recovery boundary", nil)
	}
	return nil
}

func validRunnerLedgerRecoveryAbortTerminalBoundRecord(currentJournal, journal EvidenceJournal, cursor JournalCursor, owned *OwnedEvidenceRecord, permit *runnerLedgerAbortTerminalWriterPermit, terminal AttemptTerminalState) bool {
	if currentJournal == nil || journal == nil || !sameRunnerOwnedPointer(currentJournal, journal) || owned == nil ||
		owned.consumed == nil || owned.consumed.Load() ||
		permit == nil || !sameCursorIdentity(cursor, permit.cursor) || !sameGenerationIdentity(owned.generation, permit.generation) ||
		!sameCursorIdentity(owned.cursor, cursor) || owned.witness == nil || owned.witness.kind() != EvidenceRecordAttemptTerminal ||
		!sameGenerationIdentity(owned.witness.generationIdentity(), permit.generation) || !sameCursorIdentity(owned.witness.cursorIdentity(), cursor) ||
		owned.wire.AttemptTerminal == nil || owned.wire.StatementIntent != nil || owned.wire.Intermediate != nil ||
		owned.wire.CommitIntent != nil || owned.wire.AmbiguousResolution != nil || owned.wire.Header != nil ||
		owned.wire.AttemptTerminal.Validate(permit.selection.maxAttempts) != nil {
		return false
	}
	return runnerCanonicalEqual(*owned.wire.AttemptTerminal, terminal)
}

func claimRunnerLedgerAbortTerminalAdmissionPermit(permit *runnerLedgerAbortTerminalAdmissionPermit) (runnerLedgerAbortTerminalAdmissionSeed, error) {
	var seed runnerLedgerAbortTerminalAdmissionSeed
	if permit == nil || permit.self != permit {
		return seed, fail(CodeTransactionBoundary, "runner-ledger-recovery-abort-terminal-claim", "abort-terminal admission permit is unavailable", nil)
	}
	registered, loaded := runnerLedgerRecoveryAdmissionPermitRegistry.LoadAndDelete(permit)
	record, recordOK := registered.(runnerLedgerRecoveryAdmissionPermitRegistryRecord)
	if !loaded || !recordOK || record.owner != permit || record.session == nil {
		return seed, fail(CodeTransactionBoundary, "runner-ledger-recovery-abort-terminal-claim", "abort-terminal admission permit changed or was already consumed", nil)
	}
	core := record.core
	binder, binderOK := record.evidenceBinder.(runnerLedgerAbortTerminalRecordBinder)
	valid := binderOK && core != nil && permit.core == core && core.action == generatedRunnerLedgerRecoveryProfiles[1].action &&
		permit.recoveryAdmissionAction() == core.action && validRunnerLedgerRecoveryAdmissionPermitWithRecord(permit, core, record)
	if valid {
		seed = runnerLedgerAbortTerminalAdmissionSeed{
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
	if core != nil {
		core.closed = true
		core.session = nil
		core.evidenceBinder = nil
		core.use = nil
		core.projection = nil
	}
	permit.core = nil
	if valid {
		return seed, nil
	}
	primary := fail(CodeEvidenceRecoveryRequired, "runner-ledger-recovery-abort-terminal-claim", "abort-terminal admission authority changed before transfer", nil)
	return runnerLedgerAbortTerminalAdmissionSeed{}, closeRunnerDatabasePreflight(record.session, record.key, true, primary)
}

func (runner *Runner) revalidateAndCloseRunnerLedgerAbortTerminalAdmission(ctx context.Context, seed runnerLedgerAbortTerminalAdmissionSeed, bundle *RuntimeBundle) (verifiedPrecommitTerminatedRetry, error) {
	var empty verifiedPrecommitTerminatedRetry
	failClosed := func(primary error) (verifiedPrecommitTerminatedRetry, error) {
		return empty, closeRunnerDatabasePreflight(seed.session, seed.key, true, primary)
	}
	if seed.session == nil || seed.binder == nil || seed.candidateBinding == nil || seed.projection == nil ||
		seed.runtimeInputs == ([32]byte{}) || seed.admissionPermitCanonical == ([32]byte{}) ||
		!runnerLedgerAbortTerminalSelection(seed.selection) || !validRunnerLedgerCatalogPreflight(seed.projection) ||
		seed.bindings.validateAt(time.Now()) != nil || seed.bindings.expectedCanonical == "" ||
		!validRunnerLedgerRecoveryAdmissionUse(seed.binder, seed.use, seed.consumerFactSubject, generatedRunnerLedgerRecoveryProfiles[1].action, seed.evidenceBoundary, true) {
		return failClosed(fail(CodeEvidenceRecoveryRequired, "runner-ledger-recovery-abort-terminal-revalidate", "abort-terminal admission facts are unavailable or changed", nil))
	}
	verifiedBundle, err := verifiedRunnerLedgerCatalogBundle(bundle)
	if err != nil || verifiedBundle.ownedInputs.canonical != seed.runtimeInputs {
		if err == nil {
			err = fail(CodeUntrusted, "runner-ledger-recovery-abort-terminal-runtime", "verified runtime changed after recovery admission", nil)
		}
		return failClosed(err)
	}
	verifiedPlans, err := buildExactStatementPlans(verifiedBundle, seed.bindings, time.Now())
	if err != nil {
		return failClosed(err)
	}
	planDigest, planCount, err := runnerEntryPlanClosureDigest(verifiedPlans, seed.selection.migrationID)
	if err != nil || planDigest != seed.selection.planDigest || planCount != seed.selection.planCount {
		return failClosed(fail(CodeUntrusted, "runner-ledger-recovery-abort-terminal-plans", "verified statement-plan closure changed after recovery admission", err))
	}
	current := seed.binder.CurrentCandidate()
	active := seed.binder.ActiveGeneration()
	recovery := seed.binder.RecoverySnapshot()
	if !validOwnedCurrentCandidate(current) || current.binding != seed.candidateBinding || active.kind != activeGenerationCurrent ||
		!sameGenerationIdentity(active.identity, seed.generation) || !runnerLedgerAbortTerminalRecoveryBoundary(recovery, seed.generation, seed.recoveryDigest, seed.recoveryTail, seed.selection) {
		return failClosed(fail(CodeEvidenceRecoveryRequired, "runner-ledger-recovery-abort-terminal-revalidate", "evidence boundary changed before final database revalidation", nil))
	}
	projection := seed.projection
	observation := &runnerLockedLedgerCatalogObservation{
		session: seed.session, key: seed.key, bindings: seed.bindings.ownedCopy(), bundle: verifiedBundle,
		plans: verifiedPlans, ledger: cloneRunnerLedgerPrefix(projection.ledger),
		connected: cloneProjectionValue(projection.connectedAuthority), migrationRole: cloneProjectionValue(projection.migrationRoleAuthority),
		initial:               cloneCatalogStateProjectionResultPointer(projection.initialPredecessor),
		cumulative:            cloneCatalogProjectionResultPointer(projection.cumulativeCatalog),
		catalogContractDigest: cloneDigestPointer(projection.catalogContractDigest), projectionSubject: projection.projectionSubjectDigest,
	}
	observation.self = observation
	primary := observation.revalidateRecoveryAdmission(ctx, runner)
	if err := observation.close(primary); err != nil {
		return empty, err
	}
	return bindRunnerLedgerAbortTerminalLifecycleReceipt(seed)
}

func bindRunnerLedgerAbortTerminalLifecycleReceipt(seed runnerLedgerAbortTerminalAdmissionSeed) (verifiedPrecommitTerminatedRetry, error) {
	var empty verifiedPrecommitTerminatedRetry
	if seed.projection == nil || seed.catalogDigest.Validate() != nil || seed.migrationAuthority.Validate() != nil ||
		seed.ledgerDigest.Validate() != nil || !runnerLedgerAbortTerminalSelection(seed.selection) {
		return empty, fail(CodeEvidenceRecoveryRequired, "runner-ledger-recovery-abort-terminal-receipt", "closed lifecycle receipt inputs are unavailable", nil)
	}
	identity := ownedRetryIdentity{
		migrationID: seed.selection.migrationID, attemptIndex: seed.selection.attemptIndex,
		executionLineageDigest: seed.generation.executionLineageDigest, journalIdentityDigest: seed.generation.journalIdentityDigest,
	}
	nonceDigest := sha256.Sum256(append([]byte("cloud-agents/runner-ledger-abort-terminal-writer/lifecycle/v1\x00"), seed.admissionPermitCanonical[:]...))
	token := &retryLifecycleOrderToken{}
	copy(token.verifierNonce[:], nonceDigest[:len(token.verifierNonce)])
	oldID := "recovery-admission:" + hex.EncodeToString(seed.admissionPermitCanonical[:])
	newDigest := sha256.Sum256(append([]byte("cloud-agents/runner-ledger-abort-terminal-writer/closed/v1\x00"), seed.admissionPermitCanonical[:]...))
	newID := "abort-terminal-closed:" + hex.EncodeToString(newDigest[:])
	predecessor := ownedRecoveryPredecessorReceipt{
		identity: identity, newLifecycleID: newID, order: ownedLifecycleOrderAuthority{token: token, ordinal: 2},
		ledgerRows: cloneProjectionValue(seed.projection.ledger.rows), ledgerPrefixDigest: seed.ledgerDigest,
		attemptPredecessorCatalog: seed.catalogDigest, observedCatalogDigest: seed.catalogDigest,
		authorityResultDigest: seed.migrationAuthority,
	}
	receipt, err := bindPrecommitTerminatedRetryReceipt(
		ownedPrecommitTerminatedReceipt{identity: identity, oldLifecycleID: oldID, order: ownedLifecycleOrderAuthority{token: token, ordinal: 1}, oldHandleClosed: true},
		predecessor,
	)
	if err != nil {
		return empty, fail(CodeEvidenceRecoveryRequired, "runner-ledger-recovery-abort-terminal-receipt", "closed lifecycle receipt could not be sealed", err)
	}
	verified, ok := receipt.(verifiedPrecommitTerminatedRetry)
	if !ok || !validRunnerLedgerAbortTerminalReceipt(verified, seed.generation, seed.selection) {
		return empty, fail(CodeEvidenceRecoveryRequired, "runner-ledger-recovery-abort-terminal-receipt", "closed lifecycle receipt has the wrong concrete owner", nil)
	}
	return verified, nil
}

func mintRunnerLedgerAbortTerminalWriterPermit(seed runnerLedgerAbortTerminalAdmissionSeed, receipt verifiedPrecommitTerminatedRetry) (*runnerLedgerAbortTerminalWriterPermit, error) {
	current := seed.binder.CurrentCandidate()
	active := seed.binder.ActiveGeneration()
	recovery := seed.binder.RecoverySnapshot()
	if !validOwnedCurrentCandidate(current) || current.binding != seed.candidateBinding || active.kind != activeGenerationCurrent ||
		!sameGenerationIdentity(active.identity, seed.generation) || !runnerLedgerAbortTerminalRecoveryBoundary(recovery, seed.generation, seed.recoveryDigest, seed.recoveryTail, seed.selection) ||
		!validRunnerLedgerAbortTerminalReceipt(receipt, seed.generation, seed.selection) {
		return nil, fail(CodeEvidenceRecoveryRequired, "runner-ledger-recovery-abort-terminal-permit", "abort-terminal evidence boundary changed after lifecycle close", nil)
	}
	terminal, err := buildRunnerLedgerAbortTerminal(recovery, receipt, seed.selection, seed.database.postgresMajor)
	if err != nil {
		return nil, err
	}
	permit := &runnerLedgerAbortTerminalWriterPermit{
		binder: seed.binder, use: seed.use, candidateBinding: seed.candidateBinding, generation: seed.generation,
		recoveryDigest: seed.recoveryDigest, recoveryTail: seed.recoveryTail, cursor: recovery.cursor.clone(),
		selection: seed.selection, receipt: cloneRunnerLedgerAbortTerminalReceipt(receipt), terminal: terminal,
		consumerFactSubject: seed.consumerFactSubject, evidenceBoundary: seed.evidenceBoundary,
		admissionPermitCanonical: seed.admissionPermitCanonical,
		projectionSubject:        seed.projectionSubject, database: seed.database, consumed: &atomic.Bool{},
	}
	permit.self = permit
	permit.canonical = runnerLedgerAbortTerminalWriterPermitDigest(permit)
	if permit.canonical == ([32]byte{}) {
		return nil, fail(CodeEvidenceRecoveryRequired, "runner-ledger-recovery-abort-terminal-permit", "abort-terminal writer permit could not be identified", nil)
	}
	runnerLedgerAbortTerminalWriterPermitRegistry.Store(permit, runnerLedgerAbortTerminalWriterPermitRecord{
		permit: permit, binder: seed.binder, candidateBinding: seed.candidateBinding,
		cursorValid: permit.cursor.valid, consumed: permit.consumed, canonical: permit.canonical,
	})
	if !validRunnerLedgerAbortTerminalWriterPermit(permit) {
		runnerLedgerAbortTerminalWriterPermitRegistry.Delete(permit)
		return nil, fail(CodeEvidenceRecoveryRequired, "runner-ledger-recovery-abort-terminal-permit", "abort-terminal writer permit could not be sealed", nil)
	}
	return permit, nil
}

func validRunnerLedgerAbortTerminalWriterPermit(permit *runnerLedgerAbortTerminalWriterPermit) bool {
	if !validRunnerLedgerAbortTerminalWriterPermitWithoutRegistry(permit) {
		return false
	}
	registered, loaded := runnerLedgerAbortTerminalWriterPermitRegistry.Load(permit)
	record, ok := registered.(runnerLedgerAbortTerminalWriterPermitRecord)
	return loaded && ok && record.permit == permit && sameRunnerOwnedPointer(record.binder, permit.binder) &&
		record.candidateBinding == permit.candidateBinding && record.cursorValid == permit.cursor.valid &&
		record.consumed == permit.consumed && record.canonical == permit.canonical
}

func validRunnerLedgerAbortTerminalWriterPermitWithoutRegistry(permit *runnerLedgerAbortTerminalWriterPermit) bool {
	return permit != nil && permit.self == permit && permit.binder != nil && runnerOwnedPointer(permit.binder) &&
		permit.use != nil && permit.candidateBinding != nil && permit.generation.owner == permit.candidateBinding.owner &&
		permit.recoveryDigest != ([32]byte{}) && permit.recoveryTail.Validate() == nil && permit.cursor.Valid() &&
		sameGenerationIdentity(permit.cursor.generation, permit.generation) && runnerLedgerAbortTerminalSelection(permit.selection) &&
		validRunnerLedgerAbortTerminalReceipt(permit.receipt, permit.generation, permit.selection) &&
		permit.terminal.Validate(permit.selection.maxAttempts) == nil && permit.consumerFactSubject.Validate() == nil &&
		permit.admissionPermitCanonical != ([32]byte{}) && permit.projectionSubject.Validate() == nil &&
		permit.database.postgresMajor >= 15 && permit.database.postgresMajor <= 17 && permit.database.serverVersionNum != 0 &&
		permit.database.databaseName != "" && permit.database.sessionUser != "" && permit.database.currentUser == MigrationOwnerRole &&
		permit.consumed != nil && !permit.consumed.Load() &&
		validRunnerLedgerRecoveryAdmissionUse(permit.binder, permit.use, permit.consumerFactSubject, generatedRunnerLedgerRecoveryProfiles[1].action, permit.evidenceBoundary, true) &&
		permit.canonical != ([32]byte{}) && permit.canonical == runnerLedgerAbortTerminalWriterPermitDigest(permit)
}

func consumeRunnerLedgerAbortTerminalWriterPermit(permit *runnerLedgerAbortTerminalWriterPermit, binder runnerLedgerAbortTerminalRecordBinder) (runnerLedgerAbortTerminalWriterClaim, error) {
	var claimed runnerLedgerAbortTerminalWriterClaim
	if permit == nil || permit.self != permit {
		return claimed, fail(CodeEvidenceRecoveryRequired, "runner-ledger-recovery-abort-terminal-permit", "abort-terminal writer permit is unavailable", nil)
	}
	registered, loaded := runnerLedgerAbortTerminalWriterPermitRegistry.LoadAndDelete(permit)
	record, ok := registered.(runnerLedgerAbortTerminalWriterPermitRecord)
	valid := loaded && ok && record.permit == permit && sameRunnerOwnedPointer(record.binder, binder) &&
		record.candidateBinding == permit.candidateBinding && record.cursorValid == permit.cursor.valid &&
		record.consumed == permit.consumed && record.canonical == permit.canonical &&
		validRunnerLedgerAbortTerminalWriterPermitForClaim(permit)
	if valid {
		valid = permit.consumed.CompareAndSwap(false, true)
	}
	if valid {
		claimed = runnerLedgerAbortTerminalWriterClaim{
			binder: binder, candidateBinding: permit.candidateBinding, generation: permit.generation,
			recoveryDigest: permit.recoveryDigest, recoveryTail: permit.recoveryTail, cursor: permit.cursor.clone(),
			selection: permit.selection, receipt: cloneRunnerLedgerAbortTerminalReceipt(permit.receipt),
			terminal: cloneProjectionValue(permit.terminal), consumerFactSubject: permit.consumerFactSubject,
			evidenceBoundary:         permit.evidenceBoundary,
			admissionPermitCanonical: permit.admissionPermitCanonical, projectionSubject: permit.projectionSubject,
			database: permit.database, canonical: permit.canonical,
		}
	}
	permit.binder = nil
	permit.use = nil
	if !valid {
		return runnerLedgerAbortTerminalWriterClaim{}, fail(CodeEvidenceRecoveryRequired, "runner-ledger-recovery-abort-terminal-permit", "abort-terminal writer permit changed or was already consumed", nil)
	}
	return claimed, nil
}

func validRunnerLedgerAbortTerminalWriterPermitForClaim(permit *runnerLedgerAbortTerminalWriterPermit) bool {
	if permit == nil || permit.self != permit || permit.binder == nil || permit.use == nil || permit.consumed == nil || permit.consumed.Load() ||
		permit.canonical == ([32]byte{}) || permit.canonical != runnerLedgerAbortTerminalWriterPermitDigest(permit) {
		return false
	}
	return validRunnerLedgerRecoveryAdmissionUse(
		permit.binder, permit.use, permit.consumerFactSubject,
		generatedRunnerLedgerRecoveryProfiles[1].action, permit.evidenceBoundary, true,
	)
}

func runnerLedgerAbortTerminalWriterPermitDigest(permit *runnerLedgerAbortTerminalWriterPermit) [32]byte {
	if permit == nil || permit.self != permit || permit.binder == nil || permit.candidateBinding == nil || permit.consumed == nil ||
		permit.consumed.Load() || permit.generation.owner != permit.candidateBinding.owner || permit.recoveryDigest == ([32]byte{}) ||
		permit.recoveryTail.Validate() != nil || !permit.cursor.Valid() || !sameGenerationIdentity(permit.cursor.generation, permit.generation) ||
		!runnerLedgerAbortTerminalSelection(permit.selection) || !validRunnerLedgerAbortTerminalReceipt(permit.receipt, permit.generation, permit.selection) ||
		permit.terminal.Validate(permit.selection.maxAttempts) != nil || permit.consumerFactSubject.Validate() != nil ||
		permit.evidenceBoundary == ([32]byte{}) || permit.admissionPermitCanonical == ([32]byte{}) || permit.projectionSubject.Validate() != nil ||
		permit.database.postgresMajor < 15 || permit.database.postgresMajor > 17 || permit.database.serverVersionNum == 0 ||
		permit.database.databaseName == "" || permit.database.sessionUser == "" || permit.database.currentUser != MigrationOwnerRole ||
		!validGeneratedRunnerLedgerRecoveryProfiles() {
		return [32]byte{}
	}
	h := sha256.New()
	h.Write([]byte(runnerLedgerAbortTerminalWriterPermitDigestDomain + "\x00"))
	h.Write(permit.candidateBinding.canonical[:])
	h.Write(permit.recoveryDigest[:])
	h.Write(permit.evidenceBoundary[:])
	h.Write(permit.admissionPermitCanonical[:])
	h.Write(permit.selection.planDigest[:])
	for _, value := range runnerLedgerAbortTerminalWriterIdentityStrings() {
		writeAdmissionString(h, value)
	}
	for _, value := range []string{
		permit.generation.executionLineageDigest.String(), permit.generation.journalIdentityDigest.String(),
		permit.generation.runnerProjectionDecisionDigest.String(), permit.generation.schemaBundleDigest.String(),
		permit.recoveryTail.String(), permit.consumerFactSubject.String(), permit.projectionSubject.String(),
		string(permit.selection.recoveryState), string(permit.selection.recoveryAction), permit.selection.migrationID,
		permit.selection.entryDigest.String(), permit.terminal.TerminalDigest.String(),
		permit.receipt.old.oldLifecycleID, permit.receipt.predecessor.newLifecycleID,
		permit.receipt.predecessor.ledgerPrefixDigest.String(), permit.receipt.predecessor.attemptPredecessorCatalog.String(),
		permit.receipt.predecessor.observedCatalogDigest.String(), permit.receipt.predecessor.authorityResultDigest.String(),
		permit.database.databaseName, permit.database.sessionUser, permit.database.currentUser,
	} {
		writeAdmissionString(h, value)
	}
	h.Write(permit.receipt.old.order.token.verifierNonce[:])
	writeAdmissionUint(h, permit.receipt.old.order.ordinal)
	writeAdmissionUint(h, permit.receipt.predecessor.order.ordinal)
	writeAdmissionUint(h, uint64(permit.selection.profileIndex))
	writeAdmissionUint(h, uint64(permit.selection.entryIndex))
	writeAdmissionUint(h, uint64(permit.selection.attemptIndex))
	writeAdmissionUint(h, uint64(permit.selection.maxAttempts))
	writeAdmissionUint(h, uint64(permit.selection.planCount))
	writeAdmissionUint(h, uint64(permit.database.postgresMajor))
	writeAdmissionUint(h, uint64(permit.database.serverVersionNum))
	writeGenerationJournalCursor(h, permit.cursor)
	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result
}

func runnerLedgerAbortTerminalWriterIdentityStrings() []string {
	common := generatedRunnerLedgerRecoveryProfiles[0]
	writer := generatedRunnerLedgerRecoveryProfiles[1]
	return []string{
		common.registryID, common.registryDigest, common.profileID, common.profileDigest, common.stateMachineDigest, common.policyDigest,
		writer.registryID, writer.registryDigest, writer.profileID, writer.profileDigest, writer.stateMachineDigest, writer.policyDigest,
		writer.predecessor.registryID, writer.predecessor.registryDigest, writer.predecessor.profileID,
		writer.predecessor.profileDigest, writer.predecessor.stateMachineDigest, writer.predecessor.policyDigest,
	}
}

func runnerLedgerAbortTerminalSelection(selection runnerLedgerRecoveryAdmissionSelection) bool {
	if selection.action != generatedRunnerLedgerRecoveryProfiles[1].action || selection.profileIndex != 1 ||
		selection.entryDigest.Validate() != nil || !migrationIDPattern.MatchString(selection.migrationID) ||
		selection.attemptIndex == 0 || selection.maxAttempts == 0 || selection.attemptIndex > selection.maxAttempts ||
		selection.planCount == 0 || selection.planDigest == ([32]byte{}) {
		return false
	}
	if selection.recoveryState != RecoveryDanglingStatementIntent && selection.recoveryState != RecoveryDanglingIntermediate {
		return false
	}
	return selection.recoveryAction == RecoveryAppendAbortedRetryable || selection.recoveryAction == RecoveryAppendAbortedTerminal
}

func validRunnerLedgerAbortTerminalReceipt(receipt verifiedPrecommitTerminatedRetry, generation generationIdentity, selection runnerLedgerRecoveryAdmissionSelection) bool {
	identity := receipt.old.identity
	return receipt.old.oldHandleClosed && receipt.old.order.token != nil && receipt.predecessor.order.token == receipt.old.order.token &&
		receipt.old.order.ordinal == 1 && receipt.predecessor.order.ordinal == 2 &&
		identity == receipt.predecessor.identity && identity.migrationID == selection.migrationID &&
		identity.attemptIndex == selection.attemptIndex && identity.executionLineageDigest == generation.executionLineageDigest &&
		identity.journalIdentityDigest == generation.journalIdentityDigest &&
		validateOwnedRetryInputs(identity, receipt.old.oldLifecycleID, receipt.old.order, receipt.predecessor) == nil
}

func cloneRunnerLedgerAbortTerminalReceipt(receipt verifiedPrecommitTerminatedRetry) verifiedPrecommitTerminatedRetry {
	owned, _ := cloneAdmissionRetryReceipt(receipt).(verifiedPrecommitTerminatedRetry)
	return owned
}

func buildRunnerLedgerAbortTerminal(snapshot *RecoverySnapshot, receipt verifiedPrecommitTerminatedRetry, selection runnerLedgerRecoveryAdmissionSelection, postgresMajor uint16) (AttemptTerminalState, error) {
	var terminal AttemptTerminalState
	if snapshot == nil || snapshot.lastStatementIntent == nil || snapshot.migrationID == nil || snapshot.attemptIndex == nil ||
		*snapshot.migrationID != selection.migrationID || *snapshot.attemptIndex != selection.attemptIndex ||
		!runnerLedgerAbortTerminalSelection(selection) || postgresMajor < 15 || postgresMajor > 17 ||
		!validRunnerLedgerAbortTerminalReceipt(receipt, snapshot.generation, selection) {
		return terminal, fail(CodeEvidenceRecoveryRequired, "runner-ledger-recovery-abort-terminal-record", "abort-terminal recovery inputs are unavailable", nil)
	}
	intent := snapshot.lastStatementIntent.value
	if intent.Validate() != nil || intent.MigrationID != selection.migrationID || intent.AttemptIndex != selection.attemptIndex ||
		!equalDigestPointer(intent.PreviousAttemptTerminalDigest, snapshot.previousAttemptTerminalDigest) {
		return terminal, fail(CodeEvidenceRecoveryRequired, "runner-ledger-recovery-abort-terminal-record", "durable statement intent changed before abort", nil)
	}
	var lastIntermediate *Digest
	switch selection.recoveryState {
	case RecoveryDanglingStatementIntent:
		if snapshot.lastIntermediateEvidence != nil || snapshot.lastIntermediateStateDigest != nil {
			return terminal, fail(CodeEvidenceRecoveryRequired, "runner-ledger-recovery-abort-terminal-record", "intent-only recovery contains an intermediate", nil)
		}
	case RecoveryDanglingIntermediate:
		if snapshot.lastIntermediateEvidence == nil || snapshot.lastIntermediateStateDigest == nil ||
			snapshot.lastIntermediateEvidence.value.Validate() != nil ||
			snapshot.lastIntermediateEvidence.value.State.IntermediateStateDigest != *snapshot.lastIntermediateStateDigest {
			return terminal, fail(CodeEvidenceRecoveryRequired, "runner-ledger-recovery-abort-terminal-record", "durable intermediate changed before abort", nil)
		}
		lastIntermediate = cloneDigestPointer(snapshot.lastIntermediateStateDigest)
	default:
		return terminal, fail(CodeEvidenceRecoveryRequired, "runner-ledger-recovery-abort-terminal-record", "unsupported abort recovery state", nil)
	}
	if snapshot.commitIntent != nil || snapshot.lastTerminal != nil || snapshot.lastResolution != nil {
		return terminal, fail(CodeEvidenceRecoveryRequired, "runner-ledger-recovery-abort-terminal-record", "abort recovery already contains a later durable record", nil)
	}
	retryable := selection.recoveryAction == RecoveryAppendAbortedRetryable
	outcome := "aborted_terminal"
	if retryable {
		outcome = "aborted_retryable"
	}
	code := string(CodeTransactionBoundary)
	failure := StableFailureEvidence{
		Code: CodeTransactionBoundary, Phase: "reconcile", Path: "transaction", Major: &postgresMajor, Retryable: retryable,
	}
	proof := RetryProofEvidence{
		ProofKind:                       "precommit_connection_terminated_exact_predecessor",
		AttemptPredecessorCatalogDigest: receipt.predecessor.attemptPredecessorCatalog,
		ObservedCatalogDigest:           receipt.predecessor.observedCatalogDigest,
		LedgerPrefixDigest:              receipt.predecessor.ledgerPrefixDigest,
		AuthorityResultDigest:           receipt.predecessor.authorityResultDigest,
	}
	terminal = AttemptTerminalState{
		SchemaBundleDigest: intent.SchemaBundleDigest, CatalogContractDigest: intent.CatalogContractDigest,
		AuthorityProfileDigest: intent.AuthorityProfileDigest, AuthorityBindingDigest: intent.AuthorityBindingDigest,
		MigrationID: intent.MigrationID, AttemptIndex: intent.AttemptIndex,
		PreviousAttemptTerminalDigest: cloneDigestPointer(intent.PreviousAttemptTerminalDigest),
		LastIntermediateStateDigest:   lastIntermediate, Outcome: outcome, StableErrorCode: &code,
		FailureEvidence: &failure, RetryProof: &proof, ReconcileResult: "not_run",
	}
	digest, err := terminal.ComputeDigest()
	terminal.TerminalDigest = digest
	if err != nil || terminal.Validate(selection.maxAttempts) != nil {
		return AttemptTerminalState{}, fail(CodeEvidenceRecoveryRequired, "runner-ledger-recovery-abort-terminal-record", "abort terminal could not be reproduced", err)
	}
	return terminal, nil
}

func runnerLedgerAbortTerminalRecoveryBoundary(snapshot *RecoverySnapshot, generation generationIdentity, recoveryDigest [32]byte, recoveryTail Digest, selection runnerLedgerRecoveryAdmissionSelection) bool {
	if snapshot == nil || !runnerLedgerAbortTerminalSelection(selection) ||
		!validRecoverySnapshotForJournal(snapshot, generation, snapshot.cursor) ||
		generationJournalRecoveryDigest(snapshot) != recoveryDigest || snapshot.tailDigest != recoveryTail ||
		snapshot.state != selection.recoveryState || snapshot.nextPermittedAction != selection.recoveryAction ||
		snapshot.migrationID == nil || *snapshot.migrationID != selection.migrationID ||
		snapshot.attemptIndex == nil || *snapshot.attemptIndex != selection.attemptIndex || snapshot.lastStatementIntent == nil ||
		snapshot.commitIntent != nil || snapshot.lastTerminal != nil || snapshot.lastResolution != nil {
		return false
	}
	if selection.recoveryState == RecoveryDanglingStatementIntent {
		return snapshot.lastIntermediateEvidence == nil && snapshot.lastIntermediateStateDigest == nil
	}
	return snapshot.lastIntermediateEvidence != nil && snapshot.lastIntermediateStateDigest != nil
}

func (s *generationEvidenceSession) bindRunnerLedgerRecoveryAbortTerminalRecord(ctx context.Context, permit *runnerLedgerAbortTerminalWriterPermit) (EvidenceJournal, JournalCursor, *OwnedEvidenceRecord, error) {
	claimed, err := consumeRunnerLedgerAbortTerminalWriterPermit(permit, s)
	if err != nil {
		return nil, JournalCursor{}, nil, err
	}
	if err := contextAdmissionError(ctx); err != nil {
		return nil, JournalCursor{}, nil, err
	}
	if s == nil || s.self != s {
		return nil, JournalCursor{}, nil, fail(CodeEvidenceRecoveryRequired, "runner-ledger-recovery-abort-terminal-evidence", "evidence session is unavailable", nil)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.validLocked() || s.candidate.binding != claimed.candidateBinding || s.active.kind != activeGenerationCurrent ||
		s.active.recoveryExecutionBindings != nil || !sameGenerationIdentity(s.active.identity, claimed.generation) {
		return nil, JournalCursor{}, nil, fail(CodeEvidenceRecoveryRequired, "runner-ledger-recovery-abort-terminal-evidence", "current same-verifier evidence session changed", nil)
	}
	journal := s.journal
	journal.mu.Lock()
	defer journal.mu.Unlock()
	if !journal.validLocked() || journal.state == nil || journal.state.unknown != nil ||
		!sameCursorIdentity(journal.state.cursor, claimed.cursor) ||
		generationJournalRecoveryDigest(journal.state.recovery) != claimed.recoveryDigest ||
		!runnerLedgerAbortTerminalRecoveryBoundary(journal.state.recovery, claimed.generation, claimed.recoveryDigest, claimed.recoveryTail, claimed.selection) ||
		journal.schema.maxAttempts[claimed.selection.migrationID] != claimed.selection.maxAttempts ||
		int(claimed.selection.entryIndex) >= len(journal.schema.orderedMigrations) ||
		journal.schema.orderedMigrations[claimed.selection.entryIndex] != claimed.selection.migrationID ||
		!runnerCanonicalEqual(journal.schema.durableObservedLedgerPrefix, claimed.receipt.predecessor.ledgerRows) ||
		journal.schema.durableObservedLedgerDigest != claimed.receipt.predecessor.ledgerPrefixDigest {
		return nil, JournalCursor{}, nil, fail(CodeEvidenceRecoveryRequired, "runner-ledger-recovery-abort-terminal-evidence", "current journal boundary changed", nil)
	}
	header, ok := generationJournalHeader(journal)
	if !ok || claimed.terminal.SchemaBundleDigest != header.SchemaBundleDigest ||
		claimed.terminal.AuthorityProfileDigest != header.AuthorityProfileDigest ||
		claimed.terminal.AuthorityBindingDigest != header.AuthorityBindingDigest ||
		claimed.receipt.validateRetryProof(*claimed.terminal.RetryProof, claimed.terminal, nil, header) != nil {
		return nil, JournalCursor{}, nil, fail(CodeEvidenceRecoveryRequired, "runner-ledger-recovery-abort-terminal-evidence", "abort terminal receipt or header authority changed", nil)
	}
	prefix, err := readRunnerLedgerEntrySuccessPrefixLocked(ctx, journal)
	if err != nil {
		return nil, JournalCursor{}, nil, err
	}
	if len(prefix) == 0 || claimed.cursor.previousRecordDigest == nil ||
		prefix[len(prefix)-1].RecordDigest != *claimed.cursor.previousRecordDigest ||
		prefix[len(prefix)-1].Sequence+1 != claimed.cursor.nextSequence {
		return nil, JournalCursor{}, nil, admissionCorrupt("runner-ledger-recovery-abort-terminal-evidence", "stored evidence prefix differs from the current cursor", nil)
	}
	chain := cloneRunnerEvidenceChainWitness(journal.schema.chainWitness)
	if chain.retryReceipts == nil {
		chain.retryReceipts = map[Digest]verifiedRetryReceipt{}
	}
	if _, exists := chain.retryReceipts[claimed.terminal.TerminalDigest]; exists {
		return nil, JournalCursor{}, nil, admissionCorrupt("runner-ledger-recovery-abort-terminal-evidence", "abort terminal retry receipt already exists", nil)
	}
	chain.retryReceipts[claimed.terminal.TerminalDigest] = cloneRunnerLedgerAbortTerminalReceipt(claimed.receipt)
	witness := ownedAttemptTerminalWitness{
		ownedAppendContext: ownedAppendContext{generation: claimed.generation, cursor: claimed.cursor.clone(), prefix: cloneProjectionValue(prefix), chain: chain},
		terminalDigest:     claimed.terminal.TerminalDigest, retry: cloneRunnerLedgerAbortTerminalReceipt(claimed.receipt),
		maxAttempts: claimed.selection.maxAttempts,
	}
	owned, err := bindOwnedEvidenceRecord(EvidenceRecord{AttemptTerminal: cloneAttemptTerminalPointer(&claimed.terminal)}, witness)
	if err != nil {
		return nil, JournalCursor{}, nil, admissionCorrupt("runner-ledger-recovery-abort-terminal-evidence", "abort terminal record is invalid", err)
	}
	return journal, journal.state.cursor.clone(), owned, nil
}

func (*generationEvidenceSession) runnerLedgerAbortTerminalRecordBinderSealed() {}

func runnerLedgerRecoveryAbortTerminalAppendFailure(cursor JournalCursor, result AppendResult, appendErr error) error {
	if !cursor.Valid() || result.outcome != "" || result.durableCursor != nil || result.candidateSequence != 0 ||
		result.candidatePreviousRecordDigest != nil || result.candidateRecordDigest != "" || result.candidateCheckpointRecordDigest != "" ||
		result.rotationHeaderRecordDigest != nil || result.rotationHeaderCheckpointRecordDigest != nil {
		return fail(CodeEvidenceRecoveryRequired, "runner-ledger-recovery-abort-terminal-append", "abort terminal mutation outcome requires strict reopen", nil)
	}
	if errors.Is(appendErr, context.Canceled) {
		return fail(CodeContextCanceled, "runner-ledger-recovery-abort-terminal-append", "abort terminal append was canceled before mutation", nil)
	}
	if errors.Is(appendErr, context.DeadlineExceeded) {
		return fail(CodeDeadlineExceeded, "runner-ledger-recovery-abort-terminal-append", "abort terminal append deadline expired before mutation", nil)
	}
	var stable *Error
	if errors.As(appendErr, &stable) {
		return fail(stable.Code, "runner-ledger-recovery-abort-terminal-append", "abort terminal append failed before mutation", nil)
	}
	return fail(CodeEvidenceJournalFailed, "runner-ledger-recovery-abort-terminal-append", "abort terminal append failed before mutation", nil)
}

func mapRunnerLedgerRecoveryAbortTerminalError(err error, op, message string) error {
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

func validateRunnerLedgerRecoveryAbortTerminalAppendResult(cursor JournalCursor, generation generationIdentity, record EvidenceRecord, result AppendResult) (JournalCursor, Digest, error) {
	failResult := func(message string) (JournalCursor, Digest, error) {
		if cursor.valid != nil {
			cursor.valid.Store(false)
		}
		if result.durableCursor != nil && result.durableCursor.valid != nil {
			result.durableCursor.valid.Store(false)
		}
		return JournalCursor{}, "", fail(CodeEvidenceRecoveryRequired, "runner-ledger-recovery-abort-terminal-append-result", message, nil)
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
		RecordKind:           EvidenceRecordAttemptTerminal, Record: cloneEvidenceRecord(record),
	}
	computed, err := frame.ComputeDigest()
	frame.RecordDigest = computed
	if err != nil || frame.Validate() != nil || computed != result.candidateRecordDigest {
		return failResult("candidate record digest differs from the exact durable frame")
	}
	return result.durableCursor.clone(), result.candidateRecordDigest, nil
}

func runnerLedgerRecoveryAbortTerminalSnapshotMatches(snapshot, prior *RecoverySnapshot, generation generationIdentity, cursor JournalCursor, recordDigest Digest, terminal AttemptTerminalState, selection runnerLedgerRecoveryAdmissionSelection) bool {
	recoveredIntent := (*OwnedRecovered[StatementIntent])(nil)
	recoveredTerminal := (*OwnedRecovered[AttemptTerminalState])(nil)
	if snapshot != nil {
		recoveredIntent = snapshot.lastStatementIntent
		recoveredTerminal = snapshot.lastTerminal
	}
	if snapshot == nil || prior == nil || prior.lastStatementIntent == nil || prior.lastStatementIntentRecordDigest == nil ||
		prior.state != selection.recoveryState || prior.tailDigest.Validate() != nil ||
		snapshot.state != RecoveryTerminal || snapshot.migrationID == nil || *snapshot.migrationID != selection.migrationID ||
		snapshot.attemptIndex == nil || *snapshot.attemptIndex != selection.attemptIndex || recoveredIntent == nil ||
		snapshot.lastStatementIntentRecordDigest == nil ||
		recoveredTerminal == nil || snapshot.lastTerminalDigest == nil || *snapshot.lastTerminalDigest != terminal.TerminalDigest ||
		recoveredTerminal.recordDigest != recordDigest || snapshot.tailDigest != recordDigest ||
		!runnerCanonicalEqual(recoveredTerminal.value, terminal) || snapshot.commitIntent != nil || snapshot.lastCommitIntentRecordDigest != nil ||
		snapshot.lastResolution != nil || snapshot.lastResolutionDigest != nil || snapshot.lineageContinuation != nil ||
		!equalDigestPointer(snapshot.previousAttemptTerminalDigest, terminal.PreviousAttemptTerminalDigest) ||
		recoveredIntent.owner != generation.owner || recoveredTerminal.owner != generation.owner ||
		!sameGenerationIdentity(recoveredIntent.generation, generation) || !sameGenerationIdentity(recoveredTerminal.generation, generation) ||
		!sameCursorIdentity(recoveredIntent.cursor, cursor) || !sameCursorIdentity(recoveredTerminal.cursor, cursor) ||
		recoveredIntent.tailDigest != recordDigest || recoveredTerminal.tailDigest != recordDigest ||
		recoveredIntent.recordDigest != *snapshot.lastStatementIntentRecordDigest ||
		*snapshot.lastStatementIntentRecordDigest != *prior.lastStatementIntentRecordDigest ||
		!runnerCanonicalEqual(recoveredIntent.value, prior.lastStatementIntent.value) || recoveredIntent.value.Validate() != nil ||
		!validRecoverySnapshotForJournal(snapshot, generation, cursor) || !sameCursorIdentity(snapshot.cursor, cursor) {
		return false
	}
	if selection.recoveryState == RecoveryDanglingStatementIntent {
		if *snapshot.lastStatementIntentRecordDigest != prior.tailDigest || prior.lastIntermediateEvidence != nil ||
			prior.lastIntermediateEvidenceRecordDigest != nil || snapshot.lastIntermediateEvidence != nil ||
			snapshot.lastIntermediateEvidenceRecordDigest != nil || terminal.LastIntermediateStateDigest != nil {
			return false
		}
	} else if prior.lastIntermediateEvidence == nil || prior.lastIntermediateEvidenceRecordDigest == nil ||
		snapshot.lastIntermediateEvidence == nil || terminal.LastIntermediateStateDigest == nil ||
		snapshot.lastIntermediateEvidenceRecordDigest == nil || *snapshot.lastIntermediateEvidenceRecordDigest != prior.tailDigest ||
		*snapshot.lastIntermediateEvidenceRecordDigest != *prior.lastIntermediateEvidenceRecordDigest ||
		snapshot.lastIntermediateStateDigest == nil || *snapshot.lastIntermediateStateDigest != *terminal.LastIntermediateStateDigest ||
		!runnerCanonicalEqual(snapshot.lastIntermediateEvidence.value, prior.lastIntermediateEvidence.value) ||
		snapshot.lastIntermediateEvidence.value.Validate() != nil ||
		snapshot.lastIntermediateEvidence.owner != generation.owner ||
		!sameGenerationIdentity(snapshot.lastIntermediateEvidence.generation, generation) ||
		!sameCursorIdentity(snapshot.lastIntermediateEvidence.cursor, cursor) || snapshot.lastIntermediateEvidence.tailDigest != recordDigest ||
		snapshot.lastIntermediateEvidence.recordDigest != *snapshot.lastIntermediateEvidenceRecordDigest {
		return false
	}
	wantAction := RecoveryReturnFailure
	if selection.recoveryAction == RecoveryAppendAbortedRetryable {
		wantAction = RecoveryBeginNextAttempt
	}
	return snapshot.nextPermittedAction == wantAction && generationJournalRecoveryDigest(snapshot) != ([32]byte{})
}

func runnerLedgerRecoverySelectionAllowed(selection runnerLedgerRecoveryAdmissionSelection) bool {
	if selection.action == "" || selection.recoveryState == "" || selection.recoveryAction == "" {
		return false
	}
	profile, profileIndex, ok := runnerLedgerRecoveryActionProfile(selection.action)
	if !ok || profileIndex != selection.profileIndex {
		return false
	}
	for index := uint8(0); index < profile.pairCount; index++ {
		pair := profile.pairs[index]
		if pair.profileAction == selection.action && pair.state == selection.recoveryState && pair.action == selection.recoveryAction {
			return true
		}
	}
	return false
}
