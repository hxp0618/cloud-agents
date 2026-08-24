package migration

type runnerLedgerEntryAdmissionAction string

const runnerLedgerEntryAdmissionPrepare runnerLedgerEntryAdmissionAction = "prepare_entry_admission"

type runnerLedgerEntryAdmissionTransition struct {
	from  string
	event string
	to    string
}

type runnerLedgerEntryAdmissionProfile struct {
	profileID                        string
	profileDigest                    string
	stateMachineID                   string
	canonicalizationProfile          string
	canonicalizationAlgorithm        string
	digestAlgorithm                  string
	comparison                       string
	consumerProfileBinding           string
	consumedConsumerFactBinding      string
	currentEvidenceBinding           string
	selectedEntryBinding             string
	planClosureBinding               string
	databaseSessionBinding           string
	ledgerPrefixBinding              string
	catalogProjectionBinding         string
	advisoryLockBinding              string
	storedContradictionPrecedence    string
	contextOperationalPrecedence     string
	recoveryRequiredPrecedence       string
	unsupportedTransitionPrecedence  string
	closeUnlockUnknownPrecedence     string
	oneShotConsumptionPrecedence     string
	unknownOutcomePrecedence         string
	runnerConsumerBoundary           string
	existingBrandNewWriterBoundary   string
	entryWriterBoundary              string
	recoveryWriterBoundary           string
	databaseSessionBoundary          string
	databaseTransactionBoundary      string
	beginMigrationBoundary           string
	ledgerMutationBoundary           string
	evidenceMutationBoundary         string
	permitConsumerBoundary           string
	httpSurfaceBoundary              string
	p2SurfaceBoundary                string
	providerSideEffectsBoundary      string
	productionDatabaseWritesBoundary string
	deploymentBoundary               string
	publicationBoundary              string
	gateStatusBoundary               string
}

func (profile runnerLedgerEntryAdmissionProfile) valid() bool {
	if profile.profileID != "runner-ledger-entry-admission/v1" || profile.profileDigest != runnerLedgerEntryAdmissionProfileDigest ||
		profile.stateMachineID != profile.profileID ||
		profile.canonicalizationProfile != "cloud-agents-runner-ledger-entry-admission/v1-rfc8785-sha256" ||
		profile.canonicalizationAlgorithm != "RFC8785" || profile.digestAlgorithm != "SHA-256" ||
		profile.comparison != "exact_string_no_rewrite" ||
		profile.consumerProfileBinding != "exact_runner_ledger_consumer_v1_generated_identity" ||
		profile.consumedConsumerFactBinding != "exact_entry_action_same_verifier_consumed_subject" ||
		profile.currentEvidenceBinding != "exact_candidate_generation_journal_recovery_and_projection_before_and_after_database_read" ||
		profile.selectedEntryBinding != "exact_ordered_next_entry_index_id_and_digest" ||
		profile.planClosureBinding != "exact_entry_plan_count_and_domain_separated_digest" ||
		profile.databaseSessionBinding != "exact_connected_and_migration_role_same_session_identity" ||
		profile.ledgerPrefixBinding != "exact_length_head_rows_and_digest_before_and_after_catalog_projection" ||
		profile.catalogProjectionBinding != "exact_initial_or_cumulative_verified_projection_subject_and_result" ||
		profile.advisoryLockBinding != "exact_signed_key_held_until_permit_close" ||
		profile.storedContradictionPrecedence != "MIGRATION_EVIDENCE_JOURNAL_CORRUPT_BEFORE_NOT_IMPLEMENTED" ||
		profile.contextOperationalPrecedence != "STABLE_CONTEXT_OR_JOURNAL_FAILED_BEFORE_NOT_IMPLEMENTED" ||
		profile.recoveryRequiredPrecedence != "MIGRATION_EVIDENCE_RECOVERY_REQUIRED_BEFORE_NOT_IMPLEMENTED" ||
		profile.unsupportedTransitionPrecedence != "MIGRATION_PROJECTION_NOT_IMPLEMENTED" ||
		profile.closeUnlockUnknownPrecedence != "CLEANUP_FAILURE_INVALIDATES_PERMIT_AND_DOMINATES_NOT_IMPLEMENTED" ||
		profile.oneShotConsumptionPrecedence != "SECOND_TRANSITION_FAILS_CLOSED" ||
		profile.unknownOutcomePrecedence != "FAIL_CLOSED_NO_RETRY_FROM_ORDINARY_FACT" ||
		profile.runnerConsumerBoundary != "entry_read_only_admission_only" ||
		profile.existingBrandNewWriterBoundary != "separate_existing_authority_chain" ||
		profile.entryWriterBoundary != "not_implemented" || profile.recoveryWriterBoundary != "not_implemented" ||
		profile.databaseSessionBoundary != "fresh_dedicated_locked_read_only_until_exact_close" ||
		profile.databaseTransactionBoundary != "migration_and_read_write_forbidden" ||
		profile.beginMigrationBoundary != "forbidden" || profile.ledgerMutationBoundary != "forbidden" ||
		profile.evidenceMutationBoundary != "forbidden" || profile.permitConsumerBoundary != "none" ||
		profile.httpSurfaceBoundary != "not_implemented" || profile.p2SurfaceBoundary != "not_implemented" ||
		profile.providerSideEffectsBoundary != "forbidden" ||
		profile.productionDatabaseWritesBoundary != "not_authorized" ||
		profile.deploymentBoundary != "not_authorized" || profile.publicationBoundary != "not_authorized" ||
		profile.gateStatusBoundary != "all_gates_open" {
		return false
	}
	for _, value := range []string{
		profile.profileDigest,
		runnerLedgerEntryAdmissionRegistryDigest,
		runnerLedgerEntryAdmissionStateMachineDigest,
		runnerLedgerEntryAdmissionPolicyDigest,
		runnerLedgerEntryAdmissionBoundConsumerRegistryDigest,
		runnerLedgerEntryAdmissionBoundConsumerStateMachineDigest,
		runnerLedgerEntryAdmissionBoundConsumerPolicyDigest,
		runnerLedgerEntryAdmissionBoundConsumerProfileDigest,
	} {
		if Digest(value).Validate() != nil {
			return false
		}
	}
	return runnerLedgerEntryAdmissionBoundConsumerRegistryID == "cloud-agents/platform/runner-ledger-consumer" &&
		runnerLedgerEntryAdmissionBoundConsumerRegistryDigest == runnerLedgerConsumerRegistryDigest &&
		runnerLedgerEntryAdmissionBoundConsumerStateMachineDigest == runnerLedgerConsumerStateMachineDigest &&
		runnerLedgerEntryAdmissionBoundConsumerPolicyDigest == runnerLedgerConsumerPolicyDigest &&
		runnerLedgerEntryAdmissionBoundConsumerProfileID == generatedRunnerLedgerConsumerProfile.profileID &&
		runnerLedgerEntryAdmissionBoundConsumerProfileDigest == generatedRunnerLedgerConsumerProfile.profileDigest &&
		generatedRunnerLedgerEntryAdmissionPairCount == 5 && validGeneratedRunnerLedgerEntryAdmissionTransitions()
}

func validGeneratedRunnerLedgerEntryAdmissionTransitions() bool {
	want := [...]runnerLedgerEntryAdmissionTransition{
		{from: "unclassified", event: "select_entry", to: "session_revalidating"},
		{from: "unclassified", event: "select_unknown", to: "unknown_rejected"},
		{from: "session_revalidating", event: "revalidate_exact_boundary", to: "admission_ready"},
		{from: "session_revalidating", event: "revalidate_failed", to: "admission_closed"},
		{from: "admission_ready", event: "close_without_mutation", to: "admission_closed"},
	}
	if generatedRunnerLedgerEntryAdmissionTransitions != want {
		return false
	}
	type pair struct {
		disposition runnerLedgerPreflightDisposition
		state       RecoveryState
		action      RecoveryAction
	}
	allowed := [...]pair{
		{runnerLedgerPreflightEmptyBrandNew, RecoveryBrandNew, RecoveryBeginFirstAttempt},
		{runnerLedgerPreflightEmptyBrandNew, RecoveryBrandNewInherited, RecoveryBeginFirstAttempt},
		{runnerLedgerPreflightEmptyBrandNew, RecoveryBrandNewInherited, RecoveryBeginNextAttempt},
		{runnerLedgerPreflightPartialNextEntry, RecoveryBrandNewInherited, RecoveryBeginFirstAttemptNextEntry},
		{runnerLedgerPreflightPartialNextEntry, RecoveryTerminal, RecoveryBeginFirstAttemptNextEntry},
	}
	for _, item := range allowed {
		action, ok := generatedRunnerLedgerEntryAdmissionAction(item.disposition, item.state, item.action)
		if !ok || action != runnerLedgerEntryAdmissionPrepare {
			return false
		}
	}
	for _, item := range [...]pair{
		{runnerLedgerPreflightCompleteReturnSuccess, RecoveryCompleted, RecoveryReturnSuccess},
		{runnerLedgerPreflightPartialRetryOrRecovery, RecoveryTerminal, RecoveryBeginNextAttempt},
		{runnerLedgerPreflightPartialRetryOrRecovery, RecoveryDivergent, RecoveryReturnFailure},
	} {
		if action, ok := generatedRunnerLedgerEntryAdmissionAction(item.disposition, item.state, item.action); ok || action != "" {
			return false
		}
	}
	return true
}
