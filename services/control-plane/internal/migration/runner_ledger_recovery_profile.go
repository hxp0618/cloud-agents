package migration

type runnerLedgerRecoveryAction string

type runnerLedgerRecoveryPair struct {
	disposition    runnerLedgerPreflightDisposition
	state          RecoveryState
	action         RecoveryAction
	consumerAction string
	profileAction  runnerLedgerRecoveryAction
}

type runnerLedgerRecoveryTransition struct {
	from  string
	event string
	to    string
}

type runnerLedgerRecoveryRegistryBinding struct {
	registryID         string
	registryDigest     string
	stateMachineDigest string
	policyDigest       string
	profileID          string
	profileDigest      string
}

type runnerLedgerRecoveryProfile struct {
	family                    string
	action                    runnerLedgerRecoveryAction
	registryID                string
	registryDigest            string
	profileID                 string
	profileDigest             string
	stateMachineID            string
	stateMachineDigest        string
	policyDigest              string
	canonicalizationProfile   string
	canonicalizationAlgorithm string
	digestAlgorithm           string
	comparison                string
	predecessor               runnerLedgerRecoveryRegistryBinding
	permitFromProfileID       string
	identityBindings          [10]string
	errorPrecedence           [7]string
	implementationBoundary    [18]string
	pairCount                 uint8
	pairs                     [12]runnerLedgerRecoveryPair
	transitionCount           uint8
	transitions               [25]runnerLedgerRecoveryTransition
	historicalBindingCount    uint8
	historicalBindings        [5]runnerLedgerRecoveryRegistryBinding
}

func (binding runnerLedgerRecoveryRegistryBinding) valid() bool {
	if binding.registryID == "" || binding.profileID == "" {
		return false
	}
	for _, value := range [...]string{
		binding.registryDigest,
		binding.stateMachineDigest,
		binding.policyDigest,
		binding.profileDigest,
	} {
		if Digest(value).Validate() != nil {
			return false
		}
	}
	return true
}

func (profile runnerLedgerRecoveryProfile) registryBinding() runnerLedgerRecoveryRegistryBinding {
	return runnerLedgerRecoveryRegistryBinding{
		registryID:         profile.registryID,
		registryDigest:     profile.registryDigest,
		stateMachineDigest: profile.stateMachineDigest,
		policyDigest:       profile.policyDigest,
		profileID:          profile.profileID,
		profileDigest:      profile.profileDigest,
	}
}

func (profile runnerLedgerRecoveryProfile) valid() bool {
	if profile.family == "" || profile.action == "" || profile.registryID == "" ||
		profile.profileID == "" || profile.stateMachineID != profile.profileID ||
		profile.canonicalizationProfile == "" ||
		profile.canonicalizationAlgorithm != "RFC8785" || profile.digestAlgorithm != "SHA-256" ||
		profile.comparison != "exact_string_no_rewrite" || !profile.predecessor.valid() ||
		profile.pairCount > uint8(len(profile.pairs)) ||
		profile.transitionCount == 0 || profile.transitionCount > uint8(len(profile.transitions)) ||
		profile.historicalBindingCount > uint8(len(profile.historicalBindings)) {
		return false
	}
	for _, value := range [...]string{
		profile.registryDigest,
		profile.profileDigest,
		profile.stateMachineDigest,
		profile.policyDigest,
	} {
		if Digest(value).Validate() != nil {
			return false
		}
	}
	for _, value := range profile.identityBindings {
		if value == "" {
			return false
		}
	}
	if profile.errorPrecedence != [...]string{
		"MIGRATION_EVIDENCE_JOURNAL_CORRUPT_BEFORE_ACTION",
		"STABLE_CONTEXT_OR_OPERATIONAL_FAILURE_BEFORE_ACTION",
		"MIGRATION_EVIDENCE_RECOVERY_REQUIRED_BEFORE_UNSUPPORTED",
		"MIGRATION_PROJECTION_NOT_IMPLEMENTED",
		"UNKNOWN_MUTATION_REVOKES_OLD_CURSOR_AND_REQUIRES_RECOVERY",
		"CLEANUP_UNCERTAINTY_DOMINATES_ORDINARY_RESULT",
		"OLD_PERMIT_AND_SECOND_TRANSITION_FAIL_CLOSED",
	} || profile.implementationBoundary[0] != "generated_contract_and_ordinary_profile_only" ||
		profile.implementationBoundary[1] != "none_in_slice_a" ||
		profile.implementationBoundary[2] != "not_implemented_in_slice_a" ||
		profile.implementationBoundary[3] != "not_opened_in_slice_a" ||
		profile.implementationBoundary[4] != "forbidden_in_slice_a" ||
		profile.implementationBoundary[5] != "forbidden_in_slice_a" ||
		profile.implementationBoundary[6] != "forbidden_in_slice_a" ||
		profile.implementationBoundary[7] != "forbidden_in_slice_a" ||
		profile.implementationBoundary[8] != "forbidden_in_slice_a" ||
		profile.implementationBoundary[10] != "not_implemented_in_slice_a" ||
		profile.implementationBoundary[11] != "not_implemented" ||
		profile.implementationBoundary[12] != "not_implemented" ||
		profile.implementationBoundary[13] != "forbidden" ||
		profile.implementationBoundary[14] != "not_authorized" ||
		profile.implementationBoundary[15] != "not_authorized" ||
		profile.implementationBoundary[16] != "not_authorized" ||
		profile.implementationBoundary[17] != "all_gates_open" {
		return false
	}
	for i := uint8(0); i < profile.pairCount; i++ {
		pair := profile.pairs[i]
		if pair.disposition == "" || pair.state == "" || pair.action == "" ||
			(pair.consumerAction != "entry_not_implemented" && pair.consumerAction != "recovery_not_implemented") ||
			pair.profileAction != profile.action && profile.family != "recovery_admission" {
			return false
		}
	}
	for i := profile.pairCount; i < uint8(len(profile.pairs)); i++ {
		if profile.pairs[i] != (runnerLedgerRecoveryPair{}) {
			return false
		}
	}
	for i := uint8(0); i < profile.transitionCount; i++ {
		transition := profile.transitions[i]
		if transition.from == "" || transition.event == "" || transition.to == "" {
			return false
		}
	}
	for i := profile.transitionCount; i < uint8(len(profile.transitions)); i++ {
		if profile.transitions[i] != (runnerLedgerRecoveryTransition{}) {
			return false
		}
	}
	for i := uint8(0); i < profile.historicalBindingCount; i++ {
		if !profile.historicalBindings[i].valid() {
			return false
		}
	}
	for i := profile.historicalBindingCount; i < uint8(len(profile.historicalBindings)); i++ {
		if profile.historicalBindings[i] != (runnerLedgerRecoveryRegistryBinding{}) {
			return false
		}
	}
	return true
}

func validGeneratedRunnerLedgerRecoveryProfiles() bool {
	return validRunnerLedgerRecoveryProfiles(generatedRunnerLedgerRecoveryProfiles)
}

func validRunnerLedgerRecoveryProfiles(profiles [8]runnerLedgerRecoveryProfile) bool {
	wantFamilies := [...]string{
		"recovery_admission",
		"abort_terminal_writer",
		"commit_observation_writer",
		"ambiguous_resolution_writer",
		"retry_handoff",
		"recovery_execution_admission",
		"recovery_success_writer",
		"return_failure",
	}
	wantCounts := [...]uint8{12, 4, 1, 1, 1, 3, 0, 2}
	registryIDs := make(map[string]struct{}, len(profiles))
	profileIDs := make(map[string]struct{}, len(profiles))
	digests := make(map[string]struct{}, len(profiles)*4)
	for i := range profiles {
		profile := profiles[i]
		if !profile.valid() || profile.family != wantFamilies[i] || profile.pairCount != wantCounts[i] {
			return false
		}
		if _, ok := registryIDs[profile.registryID]; ok {
			return false
		}
		if _, ok := profileIDs[profile.profileID]; ok {
			return false
		}
		if _, ok := digests[profile.registryDigest]; ok {
			return false
		}
		for _, digest := range [...]string{
			profile.registryDigest,
			profile.profileDigest,
			profile.stateMachineDigest,
			profile.policyDigest,
		} {
			if _, ok := digests[digest]; ok {
				return false
			}
			digests[digest] = struct{}{}
		}
		registryIDs[profile.registryID] = struct{}{}
		profileIDs[profile.profileID] = struct{}{}
	}
	common := profiles[0]
	execution := profiles[5]
	writer := profiles[6]
	if common.profileID != "runner-ledger-recovery-admission/v1" ||
		common.predecessor.profileID != "runner-ledger-consumer/v1" || common.permitFromProfileID != "" ||
		common.historicalBindingCount != 5 || common.predecessor != common.historicalBindings[1] ||
		execution.profileID != "runner-ledger-recovery-execution-admission/v1" ||
		writer.profileID != "runner-ledger-recovery-success-writer/v1" || writer.pairCount != 0 ||
		writer.predecessor != execution.registryBinding() || writer.permitFromProfileID != execution.profileID {
		return false
	}
	wantHistoricalProfiles := [...]string{
		"runner-ledger-preflight/v1",
		"runner-ledger-consumer/v1",
		"runner-ledger-entry-admission/v1",
		"runner-ledger-entry-execution-admission/v1",
		"runner-ledger-entry-success-writer/v1",
	}
	for i := range wantHistoricalProfiles {
		if common.historicalBindings[i].profileID != wantHistoricalProfiles[i] {
			return false
		}
	}
	commonBinding := common.registryBinding()
	for _, index := range [...]int{1, 2, 3, 4, 5, 7} {
		profile := profiles[index]
		if profile.predecessor != commonBinding || profile.permitFromProfileID != common.profileID {
			return false
		}
		for i := uint8(0); i < profile.pairCount; i++ {
			pair := profile.pairs[i]
			if pair.profileAction != profile.action || !recoveryPairExists(common, pair) {
				return false
			}
		}
	}
	for _, pair := range common.pairs[:common.pairCount] {
		action, ok := generatedRunnerLedgerRecoveryAdmissionAction(pair.disposition, pair.state, pair.action)
		if !ok || action != pair.profileAction {
			return false
		}
	}
	writerAction, ok := generatedRunnerLedgerRecoverySuccessWriterAction(execution.action)
	return ok && writerAction == writer.action
}

func recoveryPairExists(profile runnerLedgerRecoveryProfile, want runnerLedgerRecoveryPair) bool {
	for i := uint8(0); i < profile.pairCount; i++ {
		if profile.pairs[i] == want {
			return true
		}
	}
	return false
}
