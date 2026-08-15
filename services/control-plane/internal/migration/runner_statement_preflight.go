package migration

import (
	"context"
	"crypto/sha256"
	"strconv"
	"sync"
)

// runnerPreparedCurrentStatement proves the exact statement-zero authority and
// catalog-before projection on the same runner-owned migration transaction.
// It deliberately exposes no evidence append, SQL execution, ledger, or commit
// method; this slice can only validate and rollback it.
type runnerPreparedCurrentStatement struct {
	self                 *runnerPreparedCurrentStatement
	binding              *runnerPreparedCurrentStatementBinding
	session              DatabaseSession
	transaction          MigrationTransaction
	evidence             EvidenceSession
	key                  int64
	candidateBinding     *verifiedEvidenceRunBinding
	generation           generationIdentity
	recoveryDigest       [32]byte
	transactionCanonical [32]byte
	dispatch             runnerPreparedDispatch
	database             runnerPreparedDatabaseIdentity
	statementIndex       uint32
	statementPlanDigest  [32]byte
	snapshotDigest       [32]byte
	authorityDigest      Digest
	catalogDigest        Digest
	canonical            [32]byte
	closed               bool
}

type runnerPreparedCurrentStatementBinding struct {
	prepared         *runnerPreparedCurrentStatement
	session          DatabaseSession
	transaction      MigrationTransaction
	evidence         EvidenceSession
	key              int64
	candidateBinding *verifiedEvidenceRunBinding
	canonical        [32]byte
}

type runnerPreparedCurrentStatementRegistryRecord struct {
	prepared         *runnerPreparedCurrentStatement
	binding          *runnerPreparedCurrentStatementBinding
	session          DatabaseSession
	transaction      MigrationTransaction
	evidence         EvidenceSession
	key              int64
	candidateBinding *verifiedEvidenceRunBinding
	canonical        [32]byte
}

type runnerPreparedCurrentStatementSeed struct {
	session                DatabaseSession
	transaction            MigrationTransaction
	evidence               EvidenceSession
	key                    int64
	candidateBinding       *verifiedEvidenceRunBinding
	generation             generationIdentity
	recoveryDigest         [32]byte
	transactionCanonical   [32]byte
	database               runnerPreparedDatabaseIdentity
	dispatch               runnerPreparedDispatch
	policy                 ExecutionPolicy
	bindings               RunnerProjectionBindings
	plan                   StatementPlan
	transactionAuthority   Digest
	transactionPredecessor Digest
}

var runnerPreparedCurrentStatementRegistry sync.Map

func (runner *Runner) prepareCurrentStatement(ctx context.Context, prepared *runnerPreparedCurrentTransaction, bundle *RuntimeBundle, plans []StatementPlan) (*runnerPreparedCurrentStatement, error) {
	seed, err := consumeRunnerPreparedCurrentTransaction(prepared, bundle, plans)
	if err != nil {
		return nil, closeRunnerPreparedCurrentTransaction(prepared, err)
	}
	failClosed := func(primary error) (*runnerPreparedCurrentStatement, error) {
		return nil, closeRunnerCurrentTransactionResources(seed.session, seed.transaction, seed.key, primary)
	}
	statementIndex := uint32(0)
	facts, projectionErr := runner.projectRunnerTransactionPreflight(
		ctx, seed.transaction, seed.policy, seed.database, seed.dispatch.migrationID, &statementIndex,
		seed.bindings, seed.plan, &seed.transactionAuthority, seed.transactionPredecessor, "runner-statement",
	)
	if projectionErr != nil {
		return failClosed(projectionErr)
	}
	boundary, boundaryErr := seed.transaction.Boundary(ctx, seed.key)
	status, statusOK := migrationProjectionTxStatus(seed.transaction)
	if boundaryErr != nil || !statusOK || status != 'T' || boundary.TxStatus != 'T' || boundary.CurrentUser != MigrationOwnerRole || !boundary.LockHeld {
		return failClosed(mapRunnerDatabasePreflightError(boundaryErr, "runner-statement-boundary", "statement preflight escaped the exact role, status, or advisory lock boundary"))
	}
	if !runnerPreparedEvidenceMatches(seed.evidence, seed.candidateBinding, seed.generation, seed.recoveryDigest) {
		return failClosed(fail(CodeEvidenceJournalFailed, "runner-statement-evidence", "current evidence authority changed during statement preflight", nil))
	}
	preparedStatement, bindErr := bindRunnerPreparedCurrentStatement(seed, facts)
	if bindErr != nil {
		return failClosed(bindErr)
	}
	return preparedStatement, nil
}

func consumeRunnerPreparedCurrentTransaction(prepared *runnerPreparedCurrentTransaction, bundle *RuntimeBundle, plans []StatementPlan) (runnerPreparedCurrentStatementSeed, error) {
	if !validRunnerPreparedCurrentTransaction(prepared) {
		return runnerPreparedCurrentStatementSeed{}, fail(CodeTransactionBoundary, "runner-statement-claim", "transaction-wide preflight authority is unavailable or changed", nil)
	}
	if bundle == nil || bundle.Manifest.ExecutionPolicy.Validate() != nil || bundle.Manifest.SchemaBundleDigest != prepared.generation.schemaBundleDigest || len(bundle.Manifest.SchemaBundle.Migrations) == 0 || bundle.Manifest.SchemaBundle.Migrations[0].ID != prepared.dispatch.migrationID {
		return runnerPreparedCurrentStatementSeed{}, fail(CodeUntrusted, "runner-statement-claim", "runtime bundle differs from the transaction-wide dispatch", nil)
	}
	planDigest, planCount, err := runnerEntryPlanClosureDigest(plans, prepared.dispatch.migrationID)
	if err != nil || planDigest != prepared.dispatch.planDigest || planCount != prepared.dispatch.planCount {
		return runnerPreparedCurrentStatementSeed{}, fail(CodeUntrusted, "runner-statement-claim", "statement plan closure differs from the transaction-wide dispatch", nil)
	}
	plan, err := firstRunnerStatementPlan(bundle, plans)
	if err != nil || plan.StatementIndex != 0 || runnerStatementPlanDigest(plan) == ([32]byte{}) || plan.ExpectedTransition.CatalogBefore.Digest != prepared.catalogDigest {
		return runnerPreparedCurrentStatementSeed{}, fail(CodeUntrusted, "runner-statement-claim", "first statement plan differs from the transaction-wide predecessor", nil)
	}
	current := prepared.evidence.CurrentCandidate()
	if !validOwnedCurrentCandidate(current) || current.binding != prepared.candidateBinding {
		return runnerPreparedCurrentStatementSeed{}, fail(CodeEvidenceJournalFailed, "runner-statement-claim", "current evidence candidate differs from the transaction-wide dispatch", nil)
	}
	bindings, err := current.verifiedRun.currentDecision.decision.runnerProjectionBindings()
	if err != nil || bindings.schemaBundleDigest != bundle.Manifest.SchemaBundleDigest || bindings.runnerProjectionDecisionDigest != prepared.generation.runnerProjectionDecisionDigest {
		return runnerPreparedCurrentStatementSeed{}, fail(CodeUntrusted, "runner-statement-claim", "verified projection bindings differ from the transaction-wide dispatch", nil)
	}
	registered, ok := runnerPreparedCurrentTransactionRegistry.LoadAndDelete(prepared)
	record, recordOK := registered.(runnerPreparedCurrentTransactionRegistryRecord)
	if !ok || !recordOK || record.prepared != prepared || record.binding != prepared.binding || record.key != prepared.key || record.candidateBinding != prepared.candidateBinding || record.canonical != prepared.canonical || !sameRunnerOwnedPointer(record.session, prepared.session) || !sameRunnerOwnedPointer(record.transaction, prepared.transaction) || !sameRunnerOwnedPointer(record.evidence, prepared.evidence) {
		return runnerPreparedCurrentStatementSeed{}, fail(CodeTransactionBoundary, "runner-statement-claim", "transaction-wide preflight authority could not be consumed exactly once", nil)
	}
	seed := runnerPreparedCurrentStatementSeed{
		session: record.session, transaction: record.transaction, evidence: record.evidence,
		key: record.key, candidateBinding: record.candidateBinding,
		generation: prepared.generation, recoveryDigest: prepared.recoveryDigest, transactionCanonical: record.canonical,
		database: prepared.database, dispatch: prepared.dispatch, policy: bundle.Manifest.ExecutionPolicy,
		bindings: bindings, plan: plan, transactionAuthority: prepared.authorityDigest, transactionPredecessor: prepared.catalogDigest,
	}
	prepared.closed = true
	prepared.session = nil
	prepared.transaction = nil
	prepared.evidence = nil
	prepared.binding = nil
	return seed, nil
}

func bindRunnerPreparedCurrentStatement(seed runnerPreparedCurrentStatementSeed, facts runnerTransactionProjectionFacts) (*runnerPreparedCurrentStatement, error) {
	planDigest := runnerStatementPlanDigest(seed.plan)
	if seed.transaction == nil || !runnerOwnedPointer(seed.transaction) || planDigest == ([32]byte{}) || facts.snapshotDigest == ([32]byte{}) || facts.authorityDigest != seed.transactionAuthority || facts.catalogDigest != seed.transactionPredecessor || !runnerPreparedEvidenceMatches(seed.evidence, seed.candidateBinding, seed.generation, seed.recoveryDigest) {
		return nil, fail(CodeTransactionBoundary, "runner-statement-seal", "statement preflight inputs are unavailable or changed", nil)
	}
	prepared := &runnerPreparedCurrentStatement{
		session: seed.session, transaction: seed.transaction, evidence: seed.evidence, key: seed.key,
		candidateBinding: seed.candidateBinding, generation: seed.generation, recoveryDigest: seed.recoveryDigest,
		transactionCanonical: seed.transactionCanonical, dispatch: seed.dispatch, database: seed.database,
		statementIndex: seed.plan.StatementIndex, statementPlanDigest: planDigest,
		snapshotDigest: facts.snapshotDigest, authorityDigest: facts.authorityDigest, catalogDigest: facts.catalogDigest,
	}
	prepared.self = prepared
	binding := &runnerPreparedCurrentStatementBinding{
		prepared: prepared, session: seed.session, transaction: seed.transaction, evidence: seed.evidence,
		key: seed.key, candidateBinding: seed.candidateBinding,
	}
	prepared.binding = binding
	prepared.canonical = runnerPreparedCurrentStatementDigest(prepared)
	binding.canonical = prepared.canonical
	if prepared.canonical == ([32]byte{}) {
		return nil, fail(CodeTransactionBoundary, "runner-statement-seal", "statement preflight could not be identified", nil)
	}
	runnerPreparedCurrentStatementRegistry.Store(prepared, runnerPreparedCurrentStatementRegistryRecord{
		prepared: prepared, binding: binding, session: seed.session, transaction: seed.transaction,
		evidence: seed.evidence, key: seed.key, candidateBinding: seed.candidateBinding, canonical: prepared.canonical,
	})
	if !validRunnerPreparedCurrentStatement(prepared) {
		runnerPreparedCurrentStatementRegistry.Delete(prepared)
		return nil, fail(CodeTransactionBoundary, "runner-statement-seal", "statement preflight could not be sealed", nil)
	}
	return prepared, nil
}

func validRunnerPreparedCurrentStatement(prepared *runnerPreparedCurrentStatement) bool {
	if prepared == nil || prepared.self != prepared || prepared.closed || prepared.binding == nil || prepared.binding.prepared != prepared || prepared.key != prepared.binding.key || prepared.candidateBinding == nil || prepared.binding.candidateBinding != prepared.candidateBinding || !sameRunnerOwnedPointer(prepared.session, prepared.binding.session) || !sameRunnerOwnedPointer(prepared.transaction, prepared.binding.transaction) || !sameRunnerOwnedPointer(prepared.evidence, prepared.binding.evidence) || prepared.canonical == ([32]byte{}) || prepared.canonical != prepared.binding.canonical || prepared.canonical != runnerPreparedCurrentStatementDigest(prepared) {
		return false
	}
	registered, ok := runnerPreparedCurrentStatementRegistry.Load(prepared)
	record, recordOK := registered.(runnerPreparedCurrentStatementRegistryRecord)
	if !ok || !recordOK || record.prepared != prepared || record.binding != prepared.binding || record.key != prepared.key || record.candidateBinding != prepared.candidateBinding || record.canonical != prepared.canonical || !sameRunnerOwnedPointer(record.session, prepared.session) || !sameRunnerOwnedPointer(record.transaction, prepared.transaction) || !sameRunnerOwnedPointer(record.evidence, prepared.evidence) {
		return false
	}
	status, statusOK := migrationProjectionTxStatus(prepared.transaction)
	return statusOK && status == 'T' && runnerPreparedEvidenceMatches(prepared.evidence, prepared.candidateBinding, prepared.generation, prepared.recoveryDigest)
}

func runnerPreparedCurrentStatementDigest(prepared *runnerPreparedCurrentStatement) [32]byte {
	if prepared == nil || prepared.self != prepared || prepared.closed || prepared.session == nil || prepared.transaction == nil || prepared.evidence == nil || prepared.candidateBinding == nil || prepared.candidateBinding.owner == nil || prepared.generation.owner != prepared.candidateBinding.owner || prepared.candidateBinding.canonical == ([32]byte{}) || prepared.recoveryDigest == ([32]byte{}) || prepared.transactionCanonical == ([32]byte{}) || prepared.statementIndex != 0 || prepared.statementPlanDigest == ([32]byte{}) || prepared.snapshotDigest == ([32]byte{}) || prepared.authorityDigest.Validate() != nil || prepared.catalogDigest.Validate() != nil || prepared.database.postgresMajor == 0 || prepared.database.serverVersionNum == 0 || prepared.database.databaseName == "" || prepared.database.sessionUser == "" || prepared.database.currentUser != MigrationOwnerRole || prepared.dispatch.recoveryState != RecoveryBrandNew && prepared.dispatch.recoveryState != RecoveryBrandNewInherited || prepared.dispatch.action != RecoveryBeginFirstAttempt || !migrationIDPattern.MatchString(prepared.dispatch.migrationID) || prepared.dispatch.attemptIndex != 1 || prepared.dispatch.entryIndex != 0 || prepared.dispatch.planCount == 0 || prepared.dispatch.planDigest == ([32]byte{}) {
		return [32]byte{}
	}
	h := sha256.New()
	h.Write([]byte("cloud-agents-platform-runner-prepared-current-statement/v1\x00"))
	h.Write(prepared.candidateBinding.canonical[:])
	h.Write(prepared.transactionCanonical[:])
	h.Write(prepared.recoveryDigest[:])
	h.Write(prepared.statementPlanDigest[:])
	h.Write(prepared.snapshotDigest[:])
	h.Write(prepared.dispatch.planDigest[:])
	for _, value := range []Digest{prepared.generation.executionLineageDigest, prepared.generation.journalIdentityDigest, prepared.generation.runnerProjectionDecisionDigest, prepared.generation.schemaBundleDigest, prepared.authorityDigest, prepared.catalogDigest} {
		if value.Validate() != nil {
			return [32]byte{}
		}
		writeAdmissionString(h, value.String())
	}
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
	writeAdmissionUint(h, uint64(prepared.statementIndex))
	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result
}

func runnerStatementPlanDigest(plan StatementPlan) [32]byte {
	if plan.validateExact() != nil || plan.StatementIndex != 0 || plan.exactCanonical == "" {
		return [32]byte{}
	}
	h := sha256.New()
	h.Write([]byte("cloud-agents-platform-runner-statement-plan/v1\x00"))
	writeAdmissionString(h, plan.exactCanonical)
	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result
}

func closeRunnerPreparedCurrentStatement(prepared *runnerPreparedCurrentStatement, primary error) error {
	if prepared == nil {
		return primary
	}
	if prepared.self != prepared {
		return fail(CodeTransactionBoundary, "runner-statement-close", "statement preflight copy cannot close database authority", nil)
	}
	registered, ok := runnerPreparedCurrentStatementRegistry.Load(prepared)
	record, recordOK := registered.(runnerPreparedCurrentStatementRegistryRecord)
	if !ok || !recordOK || record.prepared != prepared || record.binding == nil {
		return fail(CodeTransactionBoundary, "runner-statement-close", "statement preflight authority is unavailable", nil)
	}
	valid := validRunnerPreparedCurrentStatement(prepared)
	runnerPreparedCurrentStatementRegistry.Delete(prepared)
	prepared.closed = true
	prepared.session = nil
	prepared.transaction = nil
	prepared.evidence = nil
	prepared.binding = nil
	if !valid {
		primary = fail(CodeTransactionBoundary, "runner-statement-close", "statement preflight authority changed before close", nil)
	}
	return closeRunnerCurrentTransactionResources(record.session, record.transaction, record.key, primary)
}
