package migration

const runnerLedgerConsumerFactDigestDomain = "cloud-agents/runner-ledger-consumer/fact/v1"

type runnerLedgerConsumerAction string

const (
	runnerLedgerConsumerReturnSuccessNoop      runnerLedgerConsumerAction = "return_success_noop"
	runnerLedgerConsumerEntryNotImplemented    runnerLedgerConsumerAction = "entry_not_implemented"
	runnerLedgerConsumerRecoveryNotImplemented runnerLedgerConsumerAction = "recovery_not_implemented"
)

type runnerLedgerConsumerTransition struct {
	from  string
	event string
	to    string
}

type runnerLedgerConsumerProfile struct {
	profileID                        string
	profileDigest                    string
	stateMachineID                   string
	canonicalizationProfile          string
	canonicalizationAlgorithm        string
	digestAlgorithm                  string
	comparison                       string
	preflightProfileBinding          string
	preflightDispatchBinding         string
	currentEvidenceBinding           string
	orderedMigrationPrefixBinding    string
	runtimeResultBinding             string
	storedContradictionPrecedence    string
	contextOperationalPrecedence     string
	recoveryRequiredPrecedence       string
	unsupportedTransitionPrecedence  string
	oneShotConsumptionPrecedence     string
	unknownOutcomePrecedence         string
	runnerConsumerBoundary           string
	existingBrandNewWriterBoundary   string
	entryWriterBoundary              string
	recoveryWriterBoundary           string
	databaseSessionBoundary          string
	databaseTransactionBoundary      string
	ledgerMutationBoundary           string
	evidenceMutationBoundary         string
	httpSurfaceBoundary              string
	p2SurfaceBoundary                string
	providerSideEffectsBoundary      string
	productionDatabaseWritesBoundary string
	deploymentBoundary               string
	publicationBoundary              string
	gateStatusBoundary               string
}

// runnerLedgerConsumerFact is ordinary, immutable-by-convention data. It
// binds one already-consumed preflight dispatch to the generated consumer
// profile and current manifest. It carries no claim, session, transaction,
// lease, receipt, verifier artifact, writer token, or mutation port.
type runnerLedgerConsumerFact struct {
	profileID                        string
	profileDigest                    string
	registryDigest                   string
	stateMachineDigest               string
	policyDigest                     string
	boundPreflightRegistryID         string
	boundPreflightRegistryDigest     string
	boundPreflightStateMachineDigest string
	boundPreflightPolicyDigest       string
	boundPreflightProfileID          string
	boundPreflightProfileDigest      string
	action                           runnerLedgerConsumerAction
	dispatch                         runnerLedgerPreflightDispatch
	manifestDigest                   Digest
	canonical                        string
	subjectDigest                    Digest
}

type runnerLedgerConsumerFactWire struct {
	ProfileID                        string                            `json:"profile_id"`
	ProfileDigest                    string                            `json:"profile_digest"`
	RegistryDigest                   string                            `json:"registry_digest"`
	StateMachineDigest               string                            `json:"state_machine_digest"`
	PolicyDigest                     string                            `json:"policy_digest"`
	BoundPreflightRegistryID         string                            `json:"bound_preflight_registry_id"`
	BoundPreflightRegistryDigest     string                            `json:"bound_preflight_registry_digest"`
	BoundPreflightStateMachineDigest string                            `json:"bound_preflight_state_machine_digest"`
	BoundPreflightPolicyDigest       string                            `json:"bound_preflight_policy_digest"`
	BoundPreflightProfileID          string                            `json:"bound_preflight_profile_id"`
	BoundPreflightProfileDigest      string                            `json:"bound_preflight_profile_digest"`
	Action                           runnerLedgerConsumerAction        `json:"action"`
	Dispatch                         runnerLedgerPreflightDispatchWire `json:"dispatch"`
	DispatchSubjectDigest            Digest                            `json:"dispatch_subject_digest"`
	ManifestDigest                   Digest                            `json:"manifest_digest"`
}

func bindRunnerLedgerConsumerFact(profile runnerLedgerConsumerProfile, dispatch runnerLedgerPreflightDispatch, manifestDigest Digest) (runnerLedgerConsumerFact, error) {
	if !profile.valid() {
		return runnerLedgerConsumerFact{}, fail(CodeUntrusted, "runner-ledger-consumer-contract", "generated runner ledger consumer profile is unavailable or changed", nil)
	}
	if !dispatch.valid() || manifestDigest.Validate() != nil {
		return runnerLedgerConsumerFact{}, fail(CodeInvalidManifest, "runner-ledger-consumer-contract", "runner ledger consumer inputs are unavailable or changed", nil)
	}
	action, ok := generatedRunnerLedgerConsumerAction(dispatch.fact.disposition, dispatch.fact.recovery.State, dispatch.fact.recovery.Action)
	if !ok {
		return runnerLedgerConsumerFact{}, fail(CodeInvalidManifest, "runner-ledger-consumer-contract", "preflight dispatch is outside the generated consumer matrix", nil)
	}
	fact := runnerLedgerConsumerFact{
		profileID: profile.profileID, profileDigest: profile.profileDigest,
		registryDigest: runnerLedgerConsumerRegistryDigest, stateMachineDigest: runnerLedgerConsumerStateMachineDigest,
		policyDigest:                     runnerLedgerConsumerPolicyDigest,
		boundPreflightRegistryID:         runnerLedgerConsumerBoundPreflightRegistryID,
		boundPreflightRegistryDigest:     runnerLedgerConsumerBoundPreflightRegistryDigest,
		boundPreflightStateMachineDigest: runnerLedgerConsumerBoundPreflightStateMachineDigest,
		boundPreflightPolicyDigest:       runnerLedgerConsumerBoundPreflightPolicyDigest,
		boundPreflightProfileID:          runnerLedgerConsumerBoundPreflightProfileID,
		boundPreflightProfileDigest:      runnerLedgerConsumerBoundPreflightProfileDigest,
		action:                           action, dispatch: dispatch.clone(), manifestDigest: manifestDigest,
	}
	canonical, err := canonicalContractKey(fact.wire())
	if err != nil || canonical == "" {
		return runnerLedgerConsumerFact{}, fail(CodeInvalidManifest, "runner-ledger-consumer-contract", "runner ledger consumer fact is not canonical", nil)
	}
	fact.canonical = canonical
	fact.subjectDigest = DigestBytes([]byte(runnerLedgerConsumerFactDigestDomain + "\x00" + canonical))
	if !fact.valid() {
		return runnerLedgerConsumerFact{}, fail(CodeInvalidManifest, "runner-ledger-consumer-contract", "runner ledger consumer fact violates its generated profile", nil)
	}
	return fact, nil
}

func (profile runnerLedgerConsumerProfile) valid() bool {
	return profile == generatedRunnerLedgerConsumerProfile &&
		Digest(profile.profileDigest).Validate() == nil &&
		Digest(runnerLedgerConsumerRegistryDigest).Validate() == nil &&
		Digest(runnerLedgerConsumerStateMachineDigest).Validate() == nil &&
		Digest(runnerLedgerConsumerPolicyDigest).Validate() == nil &&
		runnerLedgerConsumerBoundPreflightRegistryID == "cloud-agents/platform/runner-ledger-preflight" &&
		runnerLedgerConsumerBoundPreflightRegistryDigest == runnerLedgerPreflightRegistryDigest &&
		runnerLedgerConsumerBoundPreflightStateMachineDigest == runnerLedgerPreflightStateMachineDigest &&
		runnerLedgerConsumerBoundPreflightPolicyDigest == runnerLedgerPreflightPolicyDigest &&
		runnerLedgerConsumerBoundPreflightProfileID == generatedRunnerLedgerPreflightProfile.profileID &&
		runnerLedgerConsumerBoundPreflightProfileDigest == generatedRunnerLedgerPreflightProfile.profileDigest &&
		validRunnerLedgerConsumerTransitions() && validRunnerLedgerConsumerPairs()
}

func (fact runnerLedgerConsumerFact) valid() bool {
	if !generatedRunnerLedgerConsumerProfile.valid() || !fact.dispatch.valid() || fact.manifestDigest.Validate() != nil ||
		fact.profileID != generatedRunnerLedgerConsumerProfile.profileID || fact.profileDigest != generatedRunnerLedgerConsumerProfile.profileDigest ||
		fact.registryDigest != runnerLedgerConsumerRegistryDigest || fact.stateMachineDigest != runnerLedgerConsumerStateMachineDigest ||
		fact.policyDigest != runnerLedgerConsumerPolicyDigest || fact.boundPreflightRegistryID != runnerLedgerConsumerBoundPreflightRegistryID ||
		fact.boundPreflightRegistryDigest != runnerLedgerConsumerBoundPreflightRegistryDigest ||
		fact.boundPreflightStateMachineDigest != runnerLedgerConsumerBoundPreflightStateMachineDigest ||
		fact.boundPreflightPolicyDigest != runnerLedgerConsumerBoundPreflightPolicyDigest ||
		fact.boundPreflightProfileID != runnerLedgerConsumerBoundPreflightProfileID ||
		fact.boundPreflightProfileDigest != runnerLedgerConsumerBoundPreflightProfileDigest || fact.subjectDigest.Validate() != nil {
		return false
	}
	expectedAction, ok := generatedRunnerLedgerConsumerAction(fact.dispatch.fact.disposition, fact.dispatch.fact.recovery.State, fact.dispatch.fact.recovery.Action)
	if !ok || fact.action != expectedAction || !validRunnerLedgerConsumerActionShape(fact.action, fact.dispatch) {
		return false
	}
	canonical, err := canonicalContractKey(fact.wire())
	return err == nil && canonical == fact.canonical &&
		fact.subjectDigest == DigestBytes([]byte(runnerLedgerConsumerFactDigestDomain+"\x00"+canonical))
}

func (fact runnerLedgerConsumerFact) clone() runnerLedgerConsumerFact {
	copy := fact
	copy.dispatch = fact.dispatch.clone()
	return copy
}

func (fact runnerLedgerConsumerFact) canonicalBytes() []byte {
	if !fact.valid() {
		return nil
	}
	return []byte(fact.canonical)
}

func (fact runnerLedgerConsumerFact) wire() runnerLedgerConsumerFactWire {
	return runnerLedgerConsumerFactWire{
		ProfileID: fact.profileID, ProfileDigest: fact.profileDigest,
		RegistryDigest: fact.registryDigest, StateMachineDigest: fact.stateMachineDigest, PolicyDigest: fact.policyDigest,
		BoundPreflightRegistryID: fact.boundPreflightRegistryID, BoundPreflightRegistryDigest: fact.boundPreflightRegistryDigest,
		BoundPreflightStateMachineDigest: fact.boundPreflightStateMachineDigest, BoundPreflightPolicyDigest: fact.boundPreflightPolicyDigest,
		BoundPreflightProfileID: fact.boundPreflightProfileID, BoundPreflightProfileDigest: fact.boundPreflightProfileDigest,
		Action: fact.action, Dispatch: fact.dispatch.wire(), DispatchSubjectDigest: fact.dispatch.subjectDigest,
		ManifestDigest: fact.manifestDigest,
	}
}

func validRunnerLedgerConsumerActionShape(action runnerLedgerConsumerAction, dispatch runnerLedgerPreflightDispatch) bool {
	switch action {
	case runnerLedgerConsumerReturnSuccessNoop:
		return dispatch.kind == runnerLedgerPreflightDispatchReturnSuccess && dispatch.fact.disposition == runnerLedgerPreflightCompleteReturnSuccess
	case runnerLedgerConsumerEntryNotImplemented:
		return dispatch.kind == runnerLedgerPreflightDispatchEntry &&
			(dispatch.fact.disposition == runnerLedgerPreflightEmptyBrandNew || dispatch.fact.disposition == runnerLedgerPreflightPartialNextEntry)
	case runnerLedgerConsumerRecoveryNotImplemented:
		return dispatch.kind == runnerLedgerPreflightDispatchRecovery && dispatch.fact.disposition == runnerLedgerPreflightPartialRetryOrRecovery
	default:
		return false
	}
}

func validRunnerLedgerConsumerPairs() bool {
	return generatedRunnerLedgerConsumerPairCount == 17 && generatedRunnerLedgerPreflightRecoveryPairCount == 17
}

func validRunnerLedgerConsumerTransitions() bool {
	if len(generatedRunnerLedgerConsumerTransitions) != 4 {
		return false
	}
	seen := map[string]bool{}
	for _, transition := range generatedRunnerLedgerConsumerTransitions {
		if transition.from != "unclassified" || transition.event == "" || transition.to == "" || seen[transition.to] {
			return false
		}
		seen[transition.to] = true
	}
	return seen["complete_return_success_noop"] && seen["entry_not_implemented"] &&
		seen["recovery_not_implemented"] && seen["unknown_not_implemented"]
}
