package migration

import (
	"crypto/sha256"
	"reflect"
	"strconv"
	"sync"
)

type runnerPreparedDatabaseIdentity struct {
	postgresMajor    uint16
	serverVersionNum uint32
	databaseName     string
	sessionUser      string
	currentUser      string
}

type runnerPreparedDispatch struct {
	recoveryState RecoveryState
	action        RecoveryAction
	migrationID   string
	attemptIndex  uint32
	entryIndex    uint32
	planCount     uint32
	planDigest    [32]byte
}

// runnerPreparedCurrentSession is the first production execution authority
// that retains the locked database session. This slice deliberately gives it
// no transaction-opening method: it can only be validated and closed.
type runnerPreparedCurrentSession struct {
	self             *runnerPreparedCurrentSession
	binding          *runnerPreparedCurrentSessionBinding
	session          DatabaseSession
	evidence         EvidenceSession
	key              int64
	candidateBinding *verifiedEvidenceRunBinding
	generation       generationIdentity
	recoveryDigest   [32]byte
	ledgerDigest     Digest
	ledgerHead       string
	authorityDigest  Digest
	catalogDigest    Digest
	database         runnerPreparedDatabaseIdentity
	dispatch         runnerPreparedDispatch
	canonical        [32]byte
	closed           bool
}

type runnerPreparedCurrentSessionBinding struct {
	prepared         *runnerPreparedCurrentSession
	session          DatabaseSession
	evidence         EvidenceSession
	key              int64
	candidateBinding *verifiedEvidenceRunBinding
	canonical        [32]byte
}

type runnerPreparedCurrentSessionRegistryRecord struct {
	prepared         *runnerPreparedCurrentSession
	binding          *runnerPreparedCurrentSessionBinding
	session          DatabaseSession
	evidence         EvidenceSession
	key              int64
	candidateBinding *verifiedEvidenceRunBinding
	canonical        [32]byte
}

var runnerPreparedCurrentSessionRegistry sync.Map

func bindRunnerPreparedCurrentSession(session DatabaseSession, evidence EvidenceSession, key int64, candidate OwnedCurrentCandidate, openedSnapshot *RecoverySnapshot, ledger runnerLedgerPrefix, authority ProjectionResult[AuthorityProjection], precondition ProjectionResult[CatalogStateProjection], bundle *RuntimeBundle, plans []StatementPlan) (*runnerPreparedCurrentSession, error) {
	if session == nil || evidence == nil || !runnerOwnedPointer(session) || !runnerOwnedPointer(evidence) || !validOwnedCurrentCandidate(candidate) || openedSnapshot == nil || bundle == nil || len(bundle.Manifest.SchemaBundle.Migrations) == 0 {
		return nil, fail(CodeEvidenceJournalFailed, "runner-prepared-session", "prepared session inputs are unavailable", nil)
	}
	bindings, err := candidate.verifiedRun.currentDecision.decision.runnerProjectionBindings()
	if err != nil || bindings.schemaBundleDigest != bundle.Manifest.SchemaBundleDigest || bindings.runnerProjectionDecisionDigest != candidate.verifiedRun.runnerProjectionDecisionDigest {
		return nil, fail(CodeUntrusted, "runner-prepared-session", "current projection bindings differ from the prepared runtime", nil)
	}
	expectedKey, err := bundle.Manifest.SchemaBundle.AdvisoryLock.Key()
	if err != nil || expectedKey != key {
		return nil, fail(CodeUntrusted, "runner-prepared-session", "prepared advisory lock differs from the signed runtime", nil)
	}
	current := evidence.CurrentCandidate()
	active := evidence.ActiveGeneration()
	currentSnapshot := evidence.RecoverySnapshot()
	openedDigest := generationJournalRecoveryDigest(openedSnapshot)
	if !validOwnedCurrentCandidate(current) || current.binding != candidate.binding || active.kind != activeGenerationCurrent || active.identity.owner != candidate.owner || active.identity.executionLineageDigest != candidate.verifiedRun.executionLineageDigest || active.identity.schemaBundleDigest != candidate.verifiedRun.schemaBundleDigest || active.identity.runnerProjectionDecisionDigest != candidate.verifiedRun.currentDecision.digest || active.ownedDecision.owner != candidate.verifiedRun.currentDecision.owner || active.ownedDecision.digest != active.identity.runnerProjectionDecisionDigest || !active.ownedDecision.decision.exactlyMatches(candidate.verifiedRun.currentDecision.decision) || active.recoveryExecutionBindings != nil || openedDigest == ([32]byte{}) || currentSnapshot == nil || generationJournalRecoveryDigest(currentSnapshot) != openedDigest || !sameCursorIdentity(currentSnapshot.cursor, openedSnapshot.cursor) || !validRecoverySnapshotForJournal(currentSnapshot, active.identity, currentSnapshot.cursor) {
		return nil, fail(CodeEvidenceJournalFailed, "runner-prepared-session", "current evidence authority changed before dispatch", nil)
	}
	if !runnerBrandNewRecoverySnapshot(currentSnapshot) {
		return nil, fail(CodeProjectionNotImplemented, "runner-recovery-dispatch", "current recovery action is not implemented", nil)
	}
	emptyLedgerDigest, err := LedgerPrefixDigest([]CommitIntentLedgerRow{})
	if err != nil || ledger.rows == nil || len(ledger.rows) != 0 || ledger.head != "" || ledger.digest != emptyLedgerDigest {
		return nil, fail(CodeInvalidLedger, "runner-prepared-session", "brand-new dispatch requires the exact empty ledger prefix", nil)
	}
	entry := bundle.Manifest.SchemaBundle.Migrations[0]
	planDigest, planCount, err := runnerEntryPlanClosureDigest(plans, entry.ID)
	if err != nil {
		return nil, err
	}
	firstPlan, err := firstRunnerStatementPlan(bundle, plans)
	if err != nil {
		return nil, err
	}
	if err := validateRunnerAuthorityProjectionResult(authority, authority.Metadata.Snapshot, bindings.verifiedAuthority, AuthorityPhaseMigrationRole); err != nil {
		return nil, err
	}
	condition := bindings.initialSchemaScope.BoundPrecondition()
	if err := validateRunnerInitialPreconditionResult(precondition, precondition.Metadata.Snapshot, bindings.initialSchemaScope, condition, firstPlan.ExpectedTransition.CatalogBefore); err != nil {
		return nil, err
	}
	if precondition.Digest != firstPlan.ExpectedTransition.CatalogBefore.Digest || !sameRunnerDatabaseIdentity(authority.Metadata.Snapshot, precondition.Metadata.Snapshot) {
		return nil, fail(CodeProjectionMetadataMismatch, "runner-prepared-session", "database authority or predecessor projection changed before dispatch", nil)
	}
	metadata := authority.Metadata.Snapshot
	prepared := &runnerPreparedCurrentSession{
		session: session, evidence: evidence, key: key, candidateBinding: candidate.binding,
		generation: active.identity, recoveryDigest: openedDigest, ledgerDigest: ledger.digest, ledgerHead: ledger.head,
		authorityDigest: authority.Digest, catalogDigest: precondition.Digest,
		database: runnerPreparedDatabaseIdentity{
			postgresMajor: metadata.PostgresMajor, serverVersionNum: metadata.ServerVersionNum,
			databaseName: metadata.DatabaseName, sessionUser: metadata.SessionUser, currentUser: metadata.CurrentUser,
		},
		dispatch: runnerPreparedDispatch{
			recoveryState: currentSnapshot.state, action: currentSnapshot.nextPermittedAction,
			migrationID: entry.ID, attemptIndex: 1, entryIndex: 0, planCount: planCount, planDigest: planDigest,
		},
	}
	prepared.self = prepared
	binding := &runnerPreparedCurrentSessionBinding{
		prepared: prepared, session: session, evidence: evidence, key: key, candidateBinding: candidate.binding,
	}
	prepared.binding = binding
	prepared.canonical = runnerPreparedCurrentSessionDigest(prepared)
	binding.canonical = prepared.canonical
	if prepared.canonical == ([32]byte{}) {
		return nil, fail(CodeTransactionBoundary, "runner-prepared-session", "prepared session authority could not be identified", nil)
	}
	runnerPreparedCurrentSessionRegistry.Store(prepared, runnerPreparedCurrentSessionRegistryRecord{
		prepared: prepared, binding: binding, session: session, evidence: evidence, key: key,
		candidateBinding: candidate.binding, canonical: prepared.canonical,
	})
	if !validRunnerPreparedCurrentSession(prepared) {
		runnerPreparedCurrentSessionRegistry.Delete(prepared)
		return nil, fail(CodeTransactionBoundary, "runner-prepared-session", "prepared session authority could not be sealed", nil)
	}
	return prepared, nil
}

func runnerBrandNewRecoverySnapshot(snapshot *RecoverySnapshot) bool {
	if snapshot == nil || snapshot.cursor.segmentIndex != 0 || snapshot.cursor.nextSequence != 1 || snapshot.cursor.latestCheckpointRecordDigest != nil || snapshot.nextPermittedAction != RecoveryBeginFirstAttempt || snapshot.state != RecoveryBrandNew && snapshot.state != RecoveryBrandNewInherited {
		return false
	}
	return snapshot.migrationID == nil && snapshot.attemptIndex == nil && snapshot.lineageContinuation == nil && snapshot.lastStatementIntent == nil && snapshot.lastIntermediateEvidence == nil && snapshot.commitIntent == nil && snapshot.lastTerminal == nil && snapshot.lastResolution == nil && snapshot.lastTerminalDigest == nil && snapshot.lastResolutionDigest == nil && snapshot.lastStatementIntentRecordDigest == nil && snapshot.lastIntermediateEvidenceRecordDigest == nil && snapshot.lastCommitIntentRecordDigest == nil && snapshot.previousAttemptTerminalDigest == nil && snapshot.lastIntermediateStateDigest == nil
}

func runnerEntryPlanClosureDigest(plans []StatementPlan, migrationID string) ([32]byte, uint32, error) {
	if !migrationIDPattern.MatchString(migrationID) || len(plans) == 0 {
		return [32]byte{}, 0, fail(CodeInvalidManifest, "runner-prepared-session", "entry statement plan closure is unavailable", nil)
	}
	h := sha256.New()
	h.Write([]byte("cloud-agents-platform-runner-entry-plan-closure/v1\x00"))
	writeAdmissionString(h, migrationID)
	var count uint32
	var groupCount uint32
	var previousMigrationID string
	selectedStarted := false
	selectedComplete := false
	for _, plan := range plans {
		if err := plan.validateExact(); err != nil {
			return [32]byte{}, 0, err
		}
		if plan.MigrationID != previousMigrationID {
			if previousMigrationID != "" && plan.MigrationID <= previousMigrationID {
				return [32]byte{}, 0, fail(CodeUntrusted, "runner-prepared-session", "entry statement plan groups are reordered or duplicated", nil)
			}
			if plan.StatementIndex != 0 {
				return [32]byte{}, 0, fail(CodeUntrusted, "runner-prepared-session", "entry statement plan group does not begin at index zero", nil)
			}
			previousMigrationID = plan.MigrationID
			groupCount = 0
		}
		if plan.StatementIndex != groupCount {
			return [32]byte{}, 0, fail(CodeUntrusted, "runner-prepared-session", "entry statement plans are missing, reordered, or duplicated", nil)
		}
		if groupCount == ^uint32(0) {
			return [32]byte{}, 0, fail(CodeInvalidManifest, "runner-prepared-session", "entry statement plan group exceeds uint32", nil)
		}
		groupCount++
		if plan.MigrationID != migrationID {
			if selectedStarted {
				selectedComplete = true
			}
			continue
		}
		if selectedComplete || plan.StatementIndex != count {
			return [32]byte{}, 0, fail(CodeUntrusted, "runner-prepared-session", "entry statement plans are missing, reordered, or duplicated", nil)
		}
		selectedStarted = true
		writeAdmissionString(h, plan.exactCanonical)
		if count == ^uint32(0) {
			return [32]byte{}, 0, fail(CodeInvalidManifest, "runner-prepared-session", "entry statement plan count exceeds uint32", nil)
		}
		count++
	}
	if count == 0 {
		return [32]byte{}, 0, fail(CodeInvalidManifest, "runner-prepared-session", "entry has no exact statement plans", nil)
	}
	writeAdmissionUint(h, uint64(count))
	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result, count, nil
}

func validRunnerPreparedCurrentSession(prepared *runnerPreparedCurrentSession) bool {
	if prepared == nil || prepared.self != prepared || prepared.closed || prepared.binding == nil || prepared.binding.prepared != prepared || prepared.key != prepared.binding.key || prepared.candidateBinding == nil || prepared.binding.candidateBinding != prepared.candidateBinding || !sameRunnerOwnedPointer(prepared.session, prepared.binding.session) || !sameRunnerOwnedPointer(prepared.evidence, prepared.binding.evidence) || prepared.canonical == ([32]byte{}) || prepared.canonical != prepared.binding.canonical || prepared.canonical != runnerPreparedCurrentSessionDigest(prepared) {
		return false
	}
	registered, ok := runnerPreparedCurrentSessionRegistry.Load(prepared)
	record, recordOK := registered.(runnerPreparedCurrentSessionRegistryRecord)
	if !ok || !recordOK || record.prepared != prepared || record.binding != prepared.binding || record.key != prepared.key || record.candidateBinding != prepared.candidateBinding || record.canonical != prepared.canonical || !sameRunnerOwnedPointer(record.session, prepared.session) || !sameRunnerOwnedPointer(record.evidence, prepared.evidence) {
		return false
	}
	return runnerPreparedEvidenceMatches(prepared.evidence, prepared.candidateBinding, prepared.generation, prepared.recoveryDigest)
}

func runnerPreparedEvidenceMatches(evidence EvidenceSession, candidateBinding *verifiedEvidenceRunBinding, generation generationIdentity, recoveryDigest [32]byte) bool {
	if evidence == nil || candidateBinding == nil || recoveryDigest == ([32]byte{}) {
		return false
	}
	current := evidence.CurrentCandidate()
	active := evidence.ActiveGeneration()
	snapshot := evidence.RecoverySnapshot()
	return validOwnedCurrentCandidate(current) && current.binding == candidateBinding && active.kind == activeGenerationCurrent && sameGenerationIdentity(active.identity, generation) && active.ownedDecision.owner == current.verifiedRun.currentDecision.owner && active.ownedDecision.digest == generation.runnerProjectionDecisionDigest && active.ownedDecision.decision.exactlyMatches(current.verifiedRun.currentDecision.decision) && active.recoveryExecutionBindings == nil && snapshot != nil && validRecoverySnapshotForJournal(snapshot, generation, snapshot.cursor) && generationJournalRecoveryDigest(snapshot) == recoveryDigest && runnerBrandNewRecoverySnapshot(snapshot)
}

func runnerPreparedCurrentSessionDigest(prepared *runnerPreparedCurrentSession) [32]byte {
	if prepared == nil || prepared.self != prepared || prepared.closed || prepared.session == nil || prepared.evidence == nil || prepared.candidateBinding == nil || prepared.candidateBinding.owner == nil || prepared.generation.owner != prepared.candidateBinding.owner || prepared.candidateBinding.canonical == ([32]byte{}) || prepared.recoveryDigest == ([32]byte{}) || prepared.ledgerDigest.Validate() != nil || prepared.ledgerHead != "" || prepared.authorityDigest.Validate() != nil || prepared.catalogDigest.Validate() != nil || prepared.database.postgresMajor == 0 || prepared.database.serverVersionNum == 0 || prepared.database.databaseName == "" || prepared.database.sessionUser == "" || prepared.database.currentUser != MigrationOwnerRole || prepared.dispatch.recoveryState != RecoveryBrandNew && prepared.dispatch.recoveryState != RecoveryBrandNewInherited || prepared.dispatch.action != RecoveryBeginFirstAttempt || !migrationIDPattern.MatchString(prepared.dispatch.migrationID) || prepared.dispatch.attemptIndex != 1 || prepared.dispatch.entryIndex != 0 || prepared.dispatch.planCount == 0 || prepared.dispatch.planDigest == ([32]byte{}) {
		return [32]byte{}
	}
	h := sha256.New()
	h.Write([]byte("cloud-agents-platform-runner-prepared-current-session/v1\x00"))
	h.Write(prepared.candidateBinding.canonical[:])
	for _, value := range []Digest{prepared.generation.executionLineageDigest, prepared.generation.journalIdentityDigest, prepared.generation.runnerProjectionDecisionDigest, prepared.generation.schemaBundleDigest, prepared.ledgerDigest, prepared.authorityDigest, prepared.catalogDigest} {
		if value.Validate() != nil {
			return [32]byte{}
		}
		writeAdmissionString(h, value.String())
	}
	h.Write(prepared.recoveryDigest[:])
	h.Write(prepared.dispatch.planDigest[:])
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
	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result
}

func closeRunnerPreparedCurrentSession(prepared *runnerPreparedCurrentSession, primary error) error {
	if prepared == nil {
		return primary
	}
	if prepared.self != prepared {
		return fail(CodeTransactionBoundary, "runner-prepared-session-close", "prepared session copy cannot close database authority", nil)
	}
	registered, ok := runnerPreparedCurrentSessionRegistry.Load(prepared)
	record, recordOK := registered.(runnerPreparedCurrentSessionRegistryRecord)
	if !ok || !recordOK || record.prepared != prepared || record.binding == nil {
		return fail(CodeTransactionBoundary, "runner-prepared-session-close", "prepared session authority is unavailable", nil)
	}
	valid := validRunnerPreparedCurrentSession(prepared)
	runnerPreparedCurrentSessionRegistry.Delete(prepared)
	prepared.closed = true
	prepared.session = nil
	prepared.evidence = nil
	if !valid {
		primary = fail(CodeTransactionBoundary, "runner-prepared-session-close", "prepared session authority changed before close", nil)
	}
	return closeRunnerDatabasePreflight(record.session, record.key, true, primary)
}

func runnerOwnedPointer(value any) bool {
	reflected := reflect.ValueOf(value)
	return reflected.IsValid() && reflected.Kind() == reflect.Pointer && !reflected.IsNil()
}

func sameRunnerOwnedPointer(left, right any) bool {
	leftValue, rightValue := reflect.ValueOf(left), reflect.ValueOf(right)
	return leftValue.IsValid() && rightValue.IsValid() && leftValue.Type() == rightValue.Type() && leftValue.Kind() == reflect.Pointer && rightValue.Kind() == reflect.Pointer && !leftValue.IsNil() && !rightValue.IsNil() && leftValue.Pointer() == rightValue.Pointer()
}
