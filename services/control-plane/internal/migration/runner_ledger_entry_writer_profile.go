package migration

type runnerLedgerEntryExecutionAdmissionAction string

const runnerLedgerEntryExecutionAdmissionPrepare runnerLedgerEntryExecutionAdmissionAction = "prepare_entry_execution"

type runnerLedgerEntrySuccessWriterAction string

const runnerLedgerEntrySuccessWriterExecute runnerLedgerEntrySuccessWriterAction = "execute_one_entry_known_success"

type runnerLedgerEntryWriterTransition struct {
	from  string
	event string
	to    string
}

type runnerLedgerEntryExecutionAdmissionProfile struct {
	profileID                 string
	profileDigest             string
	stateMachineID            string
	canonicalizationProfile   string
	canonicalizationAlgorithm string
	digestAlgorithm           string
	comparison                string
	identityBindings          [10]string
	errorPrecedence           [7]string
	implementationBoundary    [19]string
}

type runnerLedgerEntrySuccessWriterProfile struct {
	profileID                 string
	profileDigest             string
	stateMachineID            string
	canonicalizationProfile   string
	canonicalizationAlgorithm string
	digestAlgorithm           string
	comparison                string
	identityBindings          [11]string
	errorPrecedence           [7]string
	implementationBoundary    [21]string
}

func (profile runnerLedgerEntryExecutionAdmissionProfile) valid() bool {
	if profile.profileID != "runner-ledger-entry-execution-admission/v1" ||
		profile.profileDigest != runnerLedgerEntryExecutionAdmissionProfileDigest ||
		profile.stateMachineID != profile.profileID ||
		profile.canonicalizationProfile != "cloud-agents-runner-ledger-entry-execution-admission/v1-rfc8785-sha256" ||
		profile.canonicalizationAlgorithm != "RFC8785" || profile.digestAlgorithm != "SHA-256" ||
		profile.comparison != "exact_string_no_rewrite" ||
		profile.identityBindings != [...]string{
			"exact_immutable_runner_ledger_entry_admission_v1_generated_identity",
			"exact_entry_action_same_verifier_one_shot_claim",
			"exact_candidate_generation_journal_recovery_projection_and_cursor_before_and_after_database_read",
			"exact_ordered_next_entry_index_id_ledger_row_and_digest",
			"exact_nonempty_ordered_entry_plan_count_and_domain_separated_digest",
			"exact_connected_role_settings_and_migration_role_same_session_identity",
			"exact_length_head_rows_and_digest_before_and_after_catalog_projection",
			"exact_initial_or_cumulative_verified_projection_subject_and_result",
			"exact_signed_key_held_until_success_writer_or_exact_close",
			"registry_backed_noncopyable_one_shot_same_session_authority",
		} ||
		profile.errorPrecedence != [...]string{
			"MIGRATION_EVIDENCE_JOURNAL_CORRUPT_BEFORE_NOT_IMPLEMENTED",
			"STABLE_CONTEXT_OR_JOURNAL_FAILED_BEFORE_NOT_IMPLEMENTED",
			"MIGRATION_EVIDENCE_RECOVERY_REQUIRED_BEFORE_NOT_IMPLEMENTED",
			"MIGRATION_PROJECTION_NOT_IMPLEMENTED",
			"CLEANUP_FAILURE_INVALIDATES_EXECUTION_PERMIT_AND_DOMINATES_NOT_IMPLEMENTED",
			"SECOND_TRANSITION_FAILS_CLOSED",
			"FAIL_CLOSED_NO_RETRY_FROM_ORDINARY_FACT_OR_CLOSE_ONLY_PERMIT",
		} ||
		profile.implementationBoundary != [...]string{
			"entry_execution_contract_only",
			"immutable_close_only_no_consumer",
			"separate_existing_authority_chain",
			"exact_success_writer_v1_after_fixed_review",
			"separate_generated_success_writer_only",
			"not_implemented",
			"fresh_dedicated_locked_until_success_writer_or_exact_close",
			"not_opened_by_admission",
			"forbidden_in_admission",
			"forbidden_in_admission",
			"forbidden_in_admission",
			"forbidden_in_admission",
			"not_implemented",
			"not_implemented",
			"forbidden",
			"not_authorized",
			"not_authorized",
			"not_authorized",
			"all_gates_open",
		} {
		return false
	}
	for _, value := range []string{
		profile.profileDigest,
		runnerLedgerEntryExecutionAdmissionRegistryDigest,
		runnerLedgerEntryExecutionAdmissionStateMachineDigest,
		runnerLedgerEntryExecutionAdmissionPolicyDigest,
		runnerLedgerEntryExecutionAdmissionBoundEntryAdmissionRegistryDigest,
		runnerLedgerEntryExecutionAdmissionBoundEntryAdmissionStateMachineDigest,
		runnerLedgerEntryExecutionAdmissionBoundEntryAdmissionPolicyDigest,
		runnerLedgerEntryExecutionAdmissionBoundEntryAdmissionProfileDigest,
	} {
		if Digest(value).Validate() != nil {
			return false
		}
	}
	return runnerLedgerEntryExecutionAdmissionBoundEntryAdmissionRegistryID == "cloud-agents/platform/runner-ledger-entry-admission" &&
		runnerLedgerEntryExecutionAdmissionBoundEntryAdmissionRegistryDigest == runnerLedgerEntryAdmissionRegistryDigest &&
		runnerLedgerEntryExecutionAdmissionBoundEntryAdmissionStateMachineDigest == runnerLedgerEntryAdmissionStateMachineDigest &&
		runnerLedgerEntryExecutionAdmissionBoundEntryAdmissionPolicyDigest == runnerLedgerEntryAdmissionPolicyDigest &&
		runnerLedgerEntryExecutionAdmissionBoundEntryAdmissionProfileID == generatedRunnerLedgerEntryAdmissionProfile.profileID &&
		runnerLedgerEntryExecutionAdmissionBoundEntryAdmissionProfileDigest == generatedRunnerLedgerEntryAdmissionProfile.profileDigest &&
		generatedRunnerLedgerEntryExecutionAdmissionPairCount == 4 &&
		validGeneratedRunnerLedgerEntryExecutionAdmissionTransitions()
}

func (profile runnerLedgerEntrySuccessWriterProfile) valid() bool {
	if profile.profileID != "runner-ledger-entry-success-writer/v1" ||
		profile.profileDigest != runnerLedgerEntrySuccessWriterProfileDigest ||
		profile.stateMachineID != profile.profileID ||
		profile.canonicalizationProfile != "cloud-agents-runner-ledger-entry-success-writer/v1-rfc8785-sha256" ||
		profile.canonicalizationAlgorithm != "RFC8785" || profile.digestAlgorithm != "SHA-256" ||
		profile.comparison != "exact_string_no_rewrite" ||
		profile.identityBindings != [...]string{
			"exact_runner_ledger_entry_execution_admission_v1_generated_identity",
			"exact_registry_backed_noncopyable_one_shot_same_session_authority",
			"exact_generation_journal_cursor_and_each_returned_successor",
			"exact_one_ordered_entry_index_id_ledger_row_and_digest",
			"exact_nonempty_ordered_entry_plan_count_ranges_digests_and_classifications",
			"exact_intent_execute_intermediate_for_every_signed_statement",
			"same_fresh_locked_session_from_consumed_execution_permit",
			"one_serializable_read_write_transaction_for_exact_entry",
			"exact_predecessor_then_signed_row_insert_and_readback",
			"exact_before_after_and_final_preledger_same_transaction",
			"commit_once_known_committed_before_terminal_append",
		} ||
		profile.errorPrecedence != [...]string{
			"MIGRATION_EVIDENCE_JOURNAL_CORRUPT_BEFORE_WRITER_ACTION",
			"STABLE_CONTEXT_OR_OPERATIONAL_FAILURE_INVALIDATES_CURRENT_AUTHORITY",
			"MIGRATION_EVIDENCE_RECOVERY_REQUIRED_NO_SQL_OR_COMMIT_REEXECUTION",
			"ZERO_MUTATION_CLOSED_FAILURE",
			"MUTATION_ATTEMPT_UNKNOWN_REOPEN_NO_RETRY",
			"CLEANUP_FAILURE_PRESERVES_IRREVERSIBLE_OUTCOME_AND_REQUIRES_REOPEN",
			"OLD_SUCCESSOR_AND_SECOND_TRANSITION_FAIL_CLOSED",
		} ||
		profile.implementationBoundary != [...]string{
			"none_in_slice_a",
			"separate_existing_authority_chain",
			"one_entry_multi_statement_known_success_only_after_fixed_review",
			"separate_typed_caller_after_kernel_review",
			"not_implemented",
			"not_implemented",
			"not_implemented",
			"not_implemented",
			"exact_consumed_execution_permit_session",
			"one_serializable_read_write_transaction_per_entry",
			"exact_signed_statement_extended_protocol_once",
			"exact_signed_row_same_transaction_only",
			"exact_success_record_chain_only",
			"fresh_preflight_and_execution_admission_for_each_next_entry",
			"not_implemented",
			"not_implemented",
			"forbidden",
			"not_authorized",
			"not_authorized",
			"not_authorized",
			"all_gates_open",
		} {
		return false
	}
	for _, value := range []string{
		profile.profileDigest,
		runnerLedgerEntrySuccessWriterRegistryDigest,
		runnerLedgerEntrySuccessWriterStateMachineDigest,
		runnerLedgerEntrySuccessWriterPolicyDigest,
		runnerLedgerEntrySuccessWriterBoundExecutionAdmissionRegistryDigest,
		runnerLedgerEntrySuccessWriterBoundExecutionAdmissionStateMachineDigest,
		runnerLedgerEntrySuccessWriterBoundExecutionAdmissionPolicyDigest,
		runnerLedgerEntrySuccessWriterBoundExecutionAdmissionProfileDigest,
	} {
		if Digest(value).Validate() != nil {
			return false
		}
	}
	return runnerLedgerEntrySuccessWriterBoundExecutionAdmissionRegistryID == "cloud-agents/platform/runner-ledger-entry-execution-admission" &&
		runnerLedgerEntrySuccessWriterBoundExecutionAdmissionRegistryDigest == runnerLedgerEntryExecutionAdmissionRegistryDigest &&
		runnerLedgerEntrySuccessWriterBoundExecutionAdmissionStateMachineDigest == runnerLedgerEntryExecutionAdmissionStateMachineDigest &&
		runnerLedgerEntrySuccessWriterBoundExecutionAdmissionPolicyDigest == runnerLedgerEntryExecutionAdmissionPolicyDigest &&
		runnerLedgerEntrySuccessWriterBoundExecutionAdmissionProfileID == generatedRunnerLedgerEntryExecutionAdmissionProfile.profileID &&
		runnerLedgerEntrySuccessWriterBoundExecutionAdmissionProfileDigest == generatedRunnerLedgerEntryExecutionAdmissionProfile.profileDigest &&
		validGeneratedRunnerLedgerEntrySuccessWriterTransitions()
}

func validGeneratedRunnerLedgerEntryExecutionAdmissionTransitions() bool {
	want := [...]runnerLedgerEntryWriterTransition{
		{from: "unclassified", event: "select_first_attempt_entry", to: "session_revalidating"},
		{from: "unclassified", event: "select_unknown", to: "unknown_rejected"},
		{from: "session_revalidating", event: "revalidate_exact_boundary", to: "execution_admission_ready"},
		{from: "session_revalidating", event: "revalidate_failed", to: "execution_admission_closed"},
		{from: "execution_admission_ready", event: "close_without_mutation", to: "execution_admission_closed"},
	}
	if generatedRunnerLedgerEntryExecutionAdmissionTransitions != want {
		return false
	}
	type pair struct {
		disposition runnerLedgerPreflightDisposition
		state       RecoveryState
		action      RecoveryAction
	}
	for _, item := range [...]pair{
		{runnerLedgerPreflightEmptyBrandNew, RecoveryBrandNew, RecoveryBeginFirstAttempt},
		{runnerLedgerPreflightEmptyBrandNew, RecoveryBrandNewInherited, RecoveryBeginFirstAttempt},
		{runnerLedgerPreflightPartialNextEntry, RecoveryBrandNewInherited, RecoveryBeginFirstAttemptNextEntry},
		{runnerLedgerPreflightPartialNextEntry, RecoveryTerminal, RecoveryBeginFirstAttemptNextEntry},
	} {
		if action, ok := generatedRunnerLedgerEntryExecutionAdmissionAction(item.disposition, item.state, item.action); !ok || action != runnerLedgerEntryExecutionAdmissionPrepare {
			return false
		}
	}
	if action, ok := generatedRunnerLedgerEntryExecutionAdmissionAction(runnerLedgerPreflightEmptyBrandNew, RecoveryBrandNewInherited, RecoveryBeginNextAttempt); ok || action != "" {
		return false
	}
	return true
}

func validGeneratedRunnerLedgerEntrySuccessWriterTransitions() bool {
	want := [...]runnerLedgerEntryWriterTransition{
		{from: "unclassified", event: "consume_execution_permit", to: "execution_ready"},
		{from: "unclassified", event: "select_unknown", to: "unknown_rejected"},
		{from: "execution_ready", event: "begin_transaction", to: "transaction_ready"},
		{from: "execution_ready", event: "fail_before_mutation", to: "closed_failure"},
		{from: "transaction_ready", event: "prepare_statement", to: "statement_ready"},
		{from: "transaction_ready", event: "fail_before_mutation", to: "closed_failure"},
		{from: "statement_ready", event: "append_intent_durable", to: "intent_durable"},
		{from: "statement_ready", event: "fail_before_mutation", to: "closed_failure"},
		{from: "intent_durable", event: "execute_exact_statement", to: "statement_executed"},
		{from: "intent_durable", event: "fail_after_intent", to: "recovery_required_closed"},
		{from: "statement_executed", event: "append_intermediate_nonfinal", to: "intermediate_durable"},
		{from: "statement_executed", event: "append_intermediate_final", to: "final_intermediate_durable"},
		{from: "statement_executed", event: "mutation_outcome_unknown", to: "recovery_required_closed"},
		{from: "intermediate_durable", event: "advance_statement", to: "statement_ready"},
		{from: "intermediate_durable", event: "fail_after_intermediate", to: "recovery_required_closed"},
		{from: "final_intermediate_durable", event: "insert_and_readback_ledger", to: "ledger_readback_ready"},
		{from: "final_intermediate_durable", event: "fail_after_intermediate", to: "recovery_required_closed"},
		{from: "ledger_readback_ready", event: "append_commit_intent", to: "commit_intent_durable"},
		{from: "ledger_readback_ready", event: "mutation_outcome_unknown", to: "recovery_required_closed"},
		{from: "commit_intent_durable", event: "commit_known", to: "commit_known_committed"},
		{from: "commit_intent_durable", event: "commit_rejected_or_unknown", to: "recovery_required_closed"},
		{from: "commit_known_committed", event: "append_terminal_durable", to: "terminal_durable"},
		{from: "commit_known_committed", event: "terminal_append_failed_or_unknown", to: "recovery_required_closed"},
		{from: "terminal_durable", event: "classify_bundle_complete", to: "entry_committed_complete"},
		{from: "terminal_durable", event: "classify_next_entry", to: "entry_committed_next_entry"},
	}
	if generatedRunnerLedgerEntrySuccessWriterTransitions != want {
		return false
	}
	action, ok := generatedRunnerLedgerEntrySuccessWriterAction(runnerLedgerEntryExecutionAdmissionPrepare)
	if !ok || action != runnerLedgerEntrySuccessWriterExecute {
		return false
	}
	action, ok = generatedRunnerLedgerEntrySuccessWriterAction("")
	return !ok && action == ""
}
