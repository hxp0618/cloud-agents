package migration

import (
	"context"
	"crypto/sha256"
	"errors"
	"strconv"
	"sync"
	"sync/atomic"
)

// runnerReadbackCurrentLedger proves that the exact signed row was inserted and
// read back inside the still-open migration transaction. It exposes no evidence
// append, commit-intent, or transaction commit capability.
type runnerReadbackCurrentLedger struct {
	self                     *runnerReadbackCurrentLedger
	binding                  *runnerReadbackCurrentLedgerBinding
	session                  DatabaseSession
	transaction              MigrationTransaction
	evidence                 EvidenceSession
	journal                  EvidenceJournal
	key                      int64
	candidateBinding         *verifiedEvidenceRunBinding
	generation               generationIdentity
	intermediateCanonical    [32]byte
	recoveryDigest           [32]byte
	dispatch                 runnerPreparedDispatch
	database                 runnerPreparedDatabaseIdentity
	maxAttempts              uint32
	policy                   ExecutionPolicy
	plan                     StatementPlan
	intent                   StatementIntent
	intermediate             StatementIntermediateEvidence
	cursor                   JournalCursor
	intentRecordDigest       Digest
	intermediateRecordDigest Digest
	checkpointDigest         Digest
	ledgerRow                CommitIntentLedgerRow
	ledgerPrefixDigest       Digest
	ledgerHead               string
	ledgerLength             uint32
	boundary                 BoundaryState
	canonical                [32]byte
	closed                   bool
}

type runnerReadbackCurrentLedgerBinding struct {
	prepared         *runnerReadbackCurrentLedger
	session          DatabaseSession
	transaction      MigrationTransaction
	evidence         EvidenceSession
	journal          EvidenceJournal
	key              int64
	candidateBinding *verifiedEvidenceRunBinding
	cursorValid      *atomic.Bool
	canonical        [32]byte
}

type runnerReadbackCurrentLedgerRegistryRecord struct {
	prepared         *runnerReadbackCurrentLedger
	binding          *runnerReadbackCurrentLedgerBinding
	session          DatabaseSession
	transaction      MigrationTransaction
	evidence         EvidenceSession
	journal          EvidenceJournal
	key              int64
	candidateBinding *verifiedEvidenceRunBinding
	cursorValid      *atomic.Bool
	canonical        [32]byte
}

type runnerCurrentLedgerSeed struct {
	session                  DatabaseSession
	transaction              MigrationTransaction
	evidence                 EvidenceSession
	journal                  EvidenceJournal
	key                      int64
	candidateBinding         *verifiedEvidenceRunBinding
	generation               generationIdentity
	intermediateCanonical    [32]byte
	recoveryDigest           [32]byte
	dispatch                 runnerPreparedDispatch
	database                 runnerPreparedDatabaseIdentity
	maxAttempts              uint32
	policy                   ExecutionPolicy
	plan                     StatementPlan
	intent                   StatementIntent
	intermediate             StatementIntermediateEvidence
	cursor                   JournalCursor
	intentRecordDigest       Digest
	intermediateRecordDigest Digest
	checkpointDigest         Digest
}

type runnerCurrentLedgerReadbackFacts struct {
	row          CommitIntentLedgerRow
	prefixDigest Digest
	head         string
	length       uint32
}

var runnerReadbackCurrentLedgerRegistry sync.Map

func (runner *Runner) insertAndReadbackCurrentLedger(ctx context.Context, durable *runnerDurableFinalIntermediate) (*runnerReadbackCurrentLedger, error) {
	seed, err := consumeRunnerDurableFinalIntermediate(durable)
	if err != nil {
		return nil, closeRunnerDurableFinalIntermediate(durable, err)
	}
	failClosed := func(primary error) (*runnerReadbackCurrentLedger, error) {
		return nil, closeRunnerCurrentTransactionResources(seed.session, seed.transaction, seed.key, primary)
	}
	if ctx == nil || runner == nil {
		return failClosed(fail(CodeTransactionBoundary, "runner-ledger-readback", "ledger readback context or runner is unavailable", nil))
	}
	if contextErr := ctx.Err(); contextErr != nil {
		return failClosed(mapRunnerCurrentLedgerError(contextErr, "runner-ledger-readback", "ledger mutation was interrupted"))
	}
	bundle, entry, expectedRow, inputErr := runnerCurrentLedgerInputs(seed)
	if inputErr != nil {
		return failClosed(inputErr)
	}
	adapter, ok := seed.transaction.(runnerTransactionLedger)
	if !ok {
		return failClosed(fail(CodeInvalidLedger, "runner-ledger-write", "migration transaction lacks the sealed ledger adapter", nil))
	}
	rows, ledgerErr := adapter.insertAndReadRunnerLedgerRow(ctx, entry, seed.generation.schemaBundleDigest)
	if ledgerErr != nil {
		return failClosed(mapRunnerCurrentLedgerError(ledgerErr, "runner-ledger-write", "exact ledger insert and readback failed"))
	}
	facts, readbackErr := validateRunnerCurrentLedgerReadback(rows, bundle, expectedRow)
	if readbackErr != nil {
		return failClosed(readbackErr)
	}
	boundary, boundaryErr := seed.transaction.Boundary(ctx, seed.key)
	status, statusOK := migrationProjectionTxStatus(seed.transaction)
	if boundaryErr != nil || !statusOK || status != 'T' || boundary.TxStatus != 'T' || boundary.CurrentUser != MigrationOwnerRole || !boundary.LockHeld {
		return failClosed(mapRunnerDatabasePreflightError(boundaryErr, "runner-ledger-boundary", "ledger readback escaped the exact role, status, or advisory lock boundary"))
	}
	newRecoveryDigest, evidenceErr := runnerDurableFinalIntermediateEvidenceDigest(
		seed.evidence, seed.journal, seed.candidateBinding, seed.generation, seed.maxAttempts,
		seed.cursor, seed.intentRecordDigest, seed.intermediateRecordDigest, &seed.intent, &seed.intermediate, seed.evidence.RecoverySnapshot(),
	)
	if evidenceErr != nil || newRecoveryDigest != seed.recoveryDigest {
		seed.cursor.valid.Store(false)
		return failClosed(fail(CodeEvidenceJournalFailed, "runner-ledger-evidence", "durable final intermediate changed during ledger readback", nil))
	}
	prepared, sealErr := bindRunnerReadbackCurrentLedger(seed, facts, boundary)
	if sealErr != nil {
		seed.cursor.valid.Store(false)
		return failClosed(sealErr)
	}
	return prepared, nil
}

func consumeRunnerDurableFinalIntermediate(durable *runnerDurableFinalIntermediate) (runnerCurrentLedgerSeed, error) {
	if !validRunnerDurableFinalIntermediate(durable) {
		return runnerCurrentLedgerSeed{}, fail(CodeTransactionBoundary, "runner-ledger-claim", "durable final intermediate authority is unavailable or changed", nil)
	}
	if _, ok := durable.transaction.(runnerTransactionLedger); !ok {
		return runnerCurrentLedgerSeed{}, fail(CodeInvalidLedger, "runner-ledger-claim", "migration transaction lacks the sealed ledger adapter", nil)
	}
	plan, err := cloneRunnerStatementIntentPlan(durable.plan)
	if err != nil {
		return runnerCurrentLedgerSeed{}, fail(CodeUntrusted, "runner-ledger-claim", "exact final statement plan is unavailable", nil)
	}
	registered, ok := runnerDurableFinalIntermediateRegistry.LoadAndDelete(durable)
	record, recordOK := registered.(runnerDurableFinalIntermediateRegistryRecord)
	if !ok || !recordOK || record.prepared != durable || record.binding != durable.binding || record.key != durable.key || record.candidateBinding != durable.candidateBinding || record.cursorValid != durable.cursor.valid || record.canonical != durable.canonical || !sameRunnerOwnedPointer(record.session, durable.session) || !sameRunnerOwnedPointer(record.transaction, durable.transaction) || !sameRunnerOwnedPointer(record.evidence, durable.evidence) || !sameRunnerOwnedPointer(record.journal, durable.journal) {
		return runnerCurrentLedgerSeed{}, fail(CodeTransactionBoundary, "runner-ledger-claim", "durable final intermediate could not be consumed exactly once", nil)
	}
	seed := runnerCurrentLedgerSeed{
		session: record.session, transaction: record.transaction, evidence: record.evidence, journal: record.journal,
		key: record.key, candidateBinding: record.candidateBinding, generation: durable.generation,
		intermediateCanonical: record.canonical, recoveryDigest: durable.recoveryDigest,
		dispatch: durable.dispatch, database: durable.database, maxAttempts: durable.maxAttempts,
		policy: cloneProjectionValue(durable.policy), plan: plan, intent: cloneProjectionValue(durable.intent),
		intermediate: cloneProjectionValue(durable.intermediate), cursor: durable.cursor.clone(),
		intentRecordDigest: durable.intentRecordDigest, intermediateRecordDigest: durable.intermediateRecordDigest,
		checkpointDigest: durable.checkpointDigest,
	}
	durable.closed = true
	durable.session = nil
	durable.transaction = nil
	durable.evidence = nil
	durable.journal = nil
	durable.binding = nil
	durable.policy = ExecutionPolicy{}
	durable.plan = StatementPlan{}
	durable.intent = StatementIntent{}
	durable.intermediate = StatementIntermediateEvidence{}
	return seed, nil
}

func runnerCurrentLedgerInputs(seed runnerCurrentLedgerSeed) (*RuntimeBundle, MigrationEntry, CommitIntentLedgerRow, error) {
	current := seed.evidence.CurrentCandidate()
	active := seed.evidence.ActiveGeneration()
	if !validOwnedCurrentCandidate(current) || current.binding != seed.candidateBinding || active.kind != activeGenerationCurrent || !sameGenerationIdentity(active.identity, seed.generation) || !sameRunnerOwnedPointer(active.journal, seed.journal) {
		return nil, MigrationEntry{}, CommitIntentLedgerRow{}, fail(CodeEvidenceJournalFailed, "runner-ledger-input", "current evidence candidate differs from the durable intermediate", nil)
	}
	bundle, err := LoadRuntimeBundle(current.runtimeArtifact.bytes, current.verifiedRun.currentDecision.decision)
	if err != nil || bundle == nil || bundle.Manifest.SchemaBundleDigest != seed.generation.schemaBundleDigest || uint64(seed.dispatch.entryIndex) >= uint64(len(bundle.Manifest.SchemaBundle.Migrations)) {
		return nil, MigrationEntry{}, CommitIntentLedgerRow{}, fail(CodeUntrusted, "runner-ledger-input", "verified runtime cannot reproduce the current ledger entry", nil)
	}
	entry := cloneProjectionValue(bundle.Manifest.SchemaBundle.Migrations[seed.dispatch.entryIndex])
	if entry.ID != seed.dispatch.migrationID || entry.ID != seed.plan.MigrationID || entry.SQLArtifact.SHA256 != seed.plan.SQLArtifactSHA256 || entry.SQLArtifact.SizeBytes != seed.plan.SQLArtifactSizeBytes || seed.plan.StatementIndex+1 != seed.dispatch.planCount {
		return nil, MigrationEntry{}, CommitIntentLedgerRow{}, fail(CodeUntrusted, "runner-ledger-input", "signed ledger entry differs from the durable final statement", nil)
	}
	row := commitIntentLedgerRow(entry, seed.generation.schemaBundleDigest)
	if err := row.Validate(); err != nil {
		return nil, MigrationEntry{}, CommitIntentLedgerRow{}, fail(CodeInvalidLedger, "runner-ledger-input", "signed ledger row cannot be reconstructed", nil)
	}
	return bundle, entry, row, nil
}

func validateRunnerCurrentLedgerReadback(rows []LedgerRow, bundle *RuntimeBundle, expected CommitIntentLedgerRow) (runnerCurrentLedgerReadbackFacts, error) {
	if rows == nil || bundle == nil || bundle.Lineage == nil || expected.Validate() != nil {
		return runnerCurrentLedgerReadbackFacts{}, fail(CodeInvalidLedger, "runner-ledger-readback", "ledger readback inputs are unavailable", nil)
	}
	owned := cloneProjectionValue(rows)
	snapshot, err := ValidateLedger(owned, bundle.Lineage)
	if err != nil || snapshot == nil || len(owned) != 1 || snapshot.Head != expected.MigrationID {
		return runnerCurrentLedgerReadbackFacts{}, fail(CodeInvalidLedger, "runner-ledger-readback", "ledger is not the exact one-row signed prefix", nil)
	}
	observed, err := commitIntentLedgerRowFromObserved(owned[0])
	if err != nil || !runnerCanonicalEqual(observed, expected) {
		return runnerCurrentLedgerReadbackFacts{}, fail(CodeInvalidLedger, "runner-ledger-readback", "inserted ledger identity differs from the signed row", nil)
	}
	prefixDigest, err := LedgerPrefixDigest([]CommitIntentLedgerRow{observed})
	if err != nil || prefixDigest.Validate() != nil {
		return runnerCurrentLedgerReadbackFacts{}, fail(CodeInvalidLedger, "runner-ledger-readback", "exact ledger prefix identity is unavailable", nil)
	}
	return runnerCurrentLedgerReadbackFacts{row: observed, prefixDigest: prefixDigest, head: snapshot.Head, length: 1}, nil
}

func bindRunnerReadbackCurrentLedger(seed runnerCurrentLedgerSeed, facts runnerCurrentLedgerReadbackFacts, boundary BoundaryState) (*runnerReadbackCurrentLedger, error) {
	if facts.row.Validate() != nil || facts.prefixDigest.Validate() != nil || facts.head != seed.dispatch.migrationID || facts.length != seed.dispatch.entryIndex+1 || !seed.cursor.Valid() {
		return nil, fail(CodeInvalidLedger, "runner-ledger-seal", "ledger readback facts are unavailable or contradictory", nil)
	}
	plan, err := cloneRunnerStatementIntentPlan(seed.plan)
	if err != nil {
		return nil, fail(CodeUntrusted, "runner-ledger-seal", "exact final statement plan is unavailable", nil)
	}
	prepared := &runnerReadbackCurrentLedger{
		session: seed.session, transaction: seed.transaction, evidence: seed.evidence, journal: seed.journal,
		key: seed.key, candidateBinding: seed.candidateBinding, generation: seed.generation,
		intermediateCanonical: seed.intermediateCanonical, recoveryDigest: seed.recoveryDigest,
		dispatch: seed.dispatch, database: seed.database, maxAttempts: seed.maxAttempts,
		policy: cloneProjectionValue(seed.policy), plan: plan, intent: cloneProjectionValue(seed.intent),
		intermediate: cloneProjectionValue(seed.intermediate), cursor: seed.cursor.clone(),
		intentRecordDigest: seed.intentRecordDigest, intermediateRecordDigest: seed.intermediateRecordDigest,
		checkpointDigest: seed.checkpointDigest, ledgerRow: cloneProjectionValue(facts.row),
		ledgerPrefixDigest: facts.prefixDigest, ledgerHead: facts.head, ledgerLength: facts.length,
		boundary: boundary,
	}
	prepared.self = prepared
	binding := &runnerReadbackCurrentLedgerBinding{
		prepared: prepared, session: seed.session, transaction: seed.transaction, evidence: seed.evidence,
		journal: seed.journal, key: seed.key, candidateBinding: seed.candidateBinding, cursorValid: seed.cursor.valid,
	}
	prepared.binding = binding
	prepared.canonical = runnerReadbackCurrentLedgerDigest(prepared)
	binding.canonical = prepared.canonical
	if prepared.canonical == ([32]byte{}) {
		return nil, fail(CodeInvalidLedger, "runner-ledger-seal", "ledger readback authority could not be identified", nil)
	}
	runnerReadbackCurrentLedgerRegistry.Store(prepared, runnerReadbackCurrentLedgerRegistryRecord{
		prepared: prepared, binding: binding, session: seed.session, transaction: seed.transaction,
		evidence: seed.evidence, journal: seed.journal, key: seed.key, candidateBinding: seed.candidateBinding,
		cursorValid: seed.cursor.valid, canonical: prepared.canonical,
	})
	if !validRunnerReadbackCurrentLedger(prepared) {
		runnerReadbackCurrentLedgerRegistry.Delete(prepared)
		return nil, fail(CodeInvalidLedger, "runner-ledger-seal", "ledger readback authority could not be sealed", nil)
	}
	return prepared, nil
}

func validRunnerReadbackCurrentLedger(prepared *runnerReadbackCurrentLedger) bool {
	if prepared == nil || prepared.self != prepared || prepared.closed || prepared.binding == nil || prepared.binding.prepared != prepared || prepared.binding.key != prepared.key || prepared.binding.candidateBinding != prepared.candidateBinding || prepared.binding.cursorValid != prepared.cursor.valid || !sameRunnerOwnedPointer(prepared.binding.session, prepared.session) || !sameRunnerOwnedPointer(prepared.binding.transaction, prepared.transaction) || !sameRunnerOwnedPointer(prepared.binding.evidence, prepared.evidence) || !sameRunnerOwnedPointer(prepared.binding.journal, prepared.journal) || prepared.canonical == ([32]byte{}) || prepared.canonical != prepared.binding.canonical || prepared.canonical != runnerReadbackCurrentLedgerDigest(prepared) {
		return false
	}
	registered, ok := runnerReadbackCurrentLedgerRegistry.Load(prepared)
	record, recordOK := registered.(runnerReadbackCurrentLedgerRegistryRecord)
	if !ok || !recordOK || record.prepared != prepared || record.binding != prepared.binding || record.key != prepared.key || record.candidateBinding != prepared.candidateBinding || record.cursorValid != prepared.cursor.valid || record.canonical != prepared.canonical || !sameRunnerOwnedPointer(record.session, prepared.session) || !sameRunnerOwnedPointer(record.transaction, prepared.transaction) || !sameRunnerOwnedPointer(record.evidence, prepared.evidence) || !sameRunnerOwnedPointer(record.journal, prepared.journal) {
		return false
	}
	status, statusOK := migrationProjectionTxStatus(prepared.transaction)
	if !statusOK || status != 'T' {
		return false
	}
	digest, err := runnerDurableFinalIntermediateEvidenceDigest(
		prepared.evidence, prepared.journal, prepared.candidateBinding, prepared.generation, prepared.maxAttempts,
		prepared.cursor, prepared.intentRecordDigest, prepared.intermediateRecordDigest, &prepared.intent, &prepared.intermediate, prepared.evidence.RecoverySnapshot(),
	)
	return err == nil && digest == prepared.recoveryDigest
}

func runnerReadbackCurrentLedgerDigest(prepared *runnerReadbackCurrentLedger) [32]byte {
	if prepared == nil || prepared.self != prepared || prepared.closed || prepared.session == nil || prepared.transaction == nil || prepared.evidence == nil || prepared.journal == nil || prepared.candidateBinding == nil || prepared.candidateBinding.owner == nil || prepared.generation.owner != prepared.candidateBinding.owner || prepared.candidateBinding.canonical == ([32]byte{}) || prepared.intermediateCanonical == ([32]byte{}) || prepared.recoveryDigest == ([32]byte{}) || prepared.policy.Validate() != nil || prepared.policy.MaxAttempts != uint64(prepared.maxAttempts) || prepared.plan.validateExact() != nil || prepared.plan.StatementIndex+1 != prepared.dispatch.planCount || prepared.intent.Validate() != nil || prepared.intermediate.Validate() != nil || !runnerFinalIntermediateShapeMatches(prepared.plan, prepared.intent, prepared.intermediate) || prepared.intentRecordDigest.Validate() != nil || prepared.intermediateRecordDigest.Validate() != nil || prepared.checkpointDigest.Validate() != nil || !prepared.cursor.Valid() || !sameGenerationIdentity(prepared.cursor.generation, prepared.generation) || prepared.cursor.segmentIndex != 0 || prepared.cursor.nextSequence != 3 || prepared.cursor.previousRecordDigest == nil || *prepared.cursor.previousRecordDigest != prepared.intermediateRecordDigest || prepared.cursor.latestCheckpointRecordDigest == nil || *prepared.cursor.latestCheckpointRecordDigest != prepared.checkpointDigest || prepared.ledgerRow.Validate() != nil || prepared.ledgerPrefixDigest.Validate() != nil || prepared.ledgerHead != prepared.dispatch.migrationID || prepared.ledgerLength != prepared.dispatch.entryIndex+1 || prepared.ledgerLength != 1 || prepared.database.postgresMajor == 0 || prepared.database.serverVersionNum == 0 || prepared.database.databaseName == "" || prepared.database.sessionUser == "" || prepared.database.currentUser != MigrationOwnerRole || prepared.dispatch.recoveryState != RecoveryBrandNew && prepared.dispatch.recoveryState != RecoveryBrandNewInherited || prepared.dispatch.action != RecoveryBeginFirstAttempt || prepared.dispatch.migrationID != prepared.intent.MigrationID || prepared.dispatch.attemptIndex != prepared.intent.AttemptIndex || prepared.dispatch.entryIndex != 0 || prepared.dispatch.planCount == 0 || prepared.dispatch.planDigest == ([32]byte{}) || prepared.boundary.TxStatus != 'T' || prepared.boundary.CurrentUser != MigrationOwnerRole || !prepared.boundary.LockHeld {
		return [32]byte{}
	}
	wantPrefix, err := LedgerPrefixDigest([]CommitIntentLedgerRow{prepared.ledgerRow})
	if err != nil || wantPrefix != prepared.ledgerPrefixDigest || prepared.ledgerRow.MigrationID != prepared.dispatch.migrationID || prepared.ledgerRow.BundleDigest != prepared.generation.schemaBundleDigest || prepared.ledgerRow.SQLSHA256 != prepared.plan.SQLArtifactSHA256 || prepared.ledgerRow.SQLSizeBytes != prepared.plan.SQLArtifactSizeBytes {
		return [32]byte{}
	}
	values := []any{prepared.policy, prepared.plan.exactSentinel(), prepared.intent, prepared.intermediate, prepared.ledgerRow, prepared.boundary}
	h := sha256.New()
	h.Write([]byte("cloud-agents-platform-runner-readback-current-ledger/v1\x00"))
	h.Write(prepared.candidateBinding.canonical[:])
	h.Write(prepared.intermediateCanonical[:])
	h.Write(prepared.recoveryDigest[:])
	for _, value := range values {
		canonical, err := canonicalContractKey(value)
		if err != nil || canonical == "" {
			return [32]byte{}
		}
		writeAdmissionString(h, canonical)
	}
	for _, value := range []Digest{
		prepared.generation.executionLineageDigest, prepared.generation.journalIdentityDigest,
		prepared.generation.runnerProjectionDecisionDigest, prepared.generation.schemaBundleDigest,
		prepared.intentRecordDigest, prepared.intermediateRecordDigest, prepared.checkpointDigest,
		prepared.ledgerPrefixDigest,
	} {
		if value.Validate() != nil {
			return [32]byte{}
		}
		writeAdmissionString(h, value.String())
	}
	writeGenerationJournalCursor(h, prepared.cursor)
	writeAdmissionString(h, strconv.FormatInt(prepared.key, 10))
	writeAdmissionUint(h, uint64(prepared.database.postgresMajor))
	writeAdmissionUint(h, uint64(prepared.database.serverVersionNum))
	writeAdmissionString(h, prepared.database.databaseName)
	writeAdmissionString(h, prepared.database.sessionUser)
	writeAdmissionString(h, prepared.database.currentUser)
	writeAdmissionString(h, string(prepared.dispatch.recoveryState))
	writeAdmissionString(h, string(prepared.dispatch.action))
	writeAdmissionString(h, prepared.dispatch.migrationID)
	writeAdmissionUint(h, uint64(prepared.dispatch.attemptIndex))
	writeAdmissionUint(h, uint64(prepared.dispatch.entryIndex))
	writeAdmissionUint(h, uint64(prepared.dispatch.planCount))
	h.Write(prepared.dispatch.planDigest[:])
	writeAdmissionUint(h, uint64(prepared.maxAttempts))
	writeAdmissionString(h, prepared.ledgerHead)
	writeAdmissionUint(h, uint64(prepared.ledgerLength))
	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result
}

func closeRunnerReadbackCurrentLedger(prepared *runnerReadbackCurrentLedger, primary error) error {
	if prepared == nil {
		return primary
	}
	if prepared.self != prepared {
		return fail(CodeTransactionBoundary, "runner-ledger-close", "ledger readback copy cannot close database authority", nil)
	}
	registered, ok := runnerReadbackCurrentLedgerRegistry.Load(prepared)
	record, recordOK := registered.(runnerReadbackCurrentLedgerRegistryRecord)
	if !ok || !recordOK || record.prepared != prepared || record.binding == nil {
		return fail(CodeTransactionBoundary, "runner-ledger-close", "ledger readback authority is unavailable", nil)
	}
	valid := validRunnerReadbackCurrentLedger(prepared)
	runnerReadbackCurrentLedgerRegistry.Delete(prepared)
	prepared.closed = true
	prepared.session = nil
	prepared.transaction = nil
	prepared.evidence = nil
	prepared.journal = nil
	prepared.binding = nil
	prepared.policy = ExecutionPolicy{}
	prepared.plan = StatementPlan{}
	prepared.intent = StatementIntent{}
	prepared.intermediate = StatementIntermediateEvidence{}
	prepared.ledgerRow = CommitIntentLedgerRow{}
	if !valid {
		primary = fail(CodeTransactionBoundary, "runner-ledger-close", "ledger readback authority changed before close", nil)
	}
	return closeRunnerCurrentTransactionResources(record.session, record.transaction, record.key, primary)
}

func mapRunnerCurrentLedgerError(err error, op, message string) error {
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
	return fail(CodeInvalidLedger, op, message, nil)
}
