package migration

const runnerLedgerPreflightFactDigestDomain = "cloud-agents/runner-ledger-preflight/fact/v1"

type runnerLedgerPreflightDisposition string

const (
	runnerLedgerPreflightCompleteReturnSuccess  runnerLedgerPreflightDisposition = "complete_return_success"
	runnerLedgerPreflightEmptyBrandNew          runnerLedgerPreflightDisposition = "empty_brand_new"
	runnerLedgerPreflightPartialNextEntry       runnerLedgerPreflightDisposition = "partial_next_entry"
	runnerLedgerPreflightPartialRetryOrRecovery runnerLedgerPreflightDisposition = "partial_retry_or_recovery"
	runnerLedgerPreflightUnknownOrFailed        runnerLedgerPreflightDisposition = "unknown_or_failed"
)

type runnerLedgerPreflightTransition struct {
	from  string
	event string
	to    runnerLedgerPreflightDisposition
}

type runnerLedgerPreflightProfile struct {
	profileID                         string
	profileDigest                     string
	stateMachineID                    string
	canonicalizationProfile           string
	canonicalizationAlgorithm         string
	digestAlgorithm                   string
	comparison                        string
	schemaBundleBinding               string
	executionLineageBinding           string
	orderedMigrationPrefixBinding     string
	lastAppliedCatalogBinding         string
	nextEntryBinding                  string
	evidenceRecoveryBinding           string
	storedContradictionPrecedence     string
	contextOrOperationalPrecedence    string
	recoveryRequiredPrecedence        string
	classifiedWithoutBinderPrecedence string
	unknownOutcomePrecedence          string
	runnerConsumerBoundary            string
	databaseSessionBoundary           string
	databaseTransactionBoundary       string
	ledgerMutationBoundary            string
	evidenceMutationBoundary          string
	httpSurfaceBoundary               string
	p2SurfaceBoundary                 string
	providerSideEffectsBoundary       string
	productionDatabaseWritesBoundary  string
	deploymentBoundary                string
	publicationBoundary               string
	gateStatusBoundary                string
}

type runnerLedgerPreflightNextEntry struct {
	MigrationID string `json:"migration_id"`
	EntryDigest Digest `json:"entry_digest"`
}

type runnerLedgerPreflightRecoveryDisposition struct {
	State  RecoveryState  `json:"state"`
	Action RecoveryAction `json:"action"`
}

type runnerLedgerPreflightFactInput struct {
	SchemaBundleDigest               Digest
	ExecutionLineageDigest           Digest
	OrderedMigrationPrefixDigest     Digest
	OrderedMigrationPrefixLength     uint32
	OrderedMigrationPrefixHead       *string
	LastAppliedCatalogContractDigest Digest
	NextEntry                        *runnerLedgerPreflightNextEntry
	Recovery                         *runnerLedgerPreflightRecoveryDisposition
}

// runnerLedgerPreflightFact is an ordinary immutable-by-convention value. It
// intentionally carries no session, transaction, lease, receipt, writer token,
// or runtime authority. Its sole production consumer is the package-private
// Slice C same-verifier binder; it is not accepted by Runner.Run or a writer.
type runnerLedgerPreflightFact struct {
	profileID                        string
	profileDigest                    string
	registryDigest                   string
	stateMachineDigest               string
	policyDigest                     string
	disposition                      runnerLedgerPreflightDisposition
	schemaBundleDigest               Digest
	executionLineageDigest           Digest
	orderedMigrationPrefixDigest     Digest
	orderedMigrationPrefixLength     uint32
	orderedMigrationPrefixHead       *string
	lastAppliedCatalogContractDigest Digest
	nextEntry                        *runnerLedgerPreflightNextEntry
	recovery                         *runnerLedgerPreflightRecoveryDisposition
	canonical                        string
	subjectDigest                    Digest
}

type runnerLedgerPreflightFactWire struct {
	ProfileID                        string                                    `json:"profile_id"`
	ProfileDigest                    string                                    `json:"profile_digest"`
	RegistryDigest                   string                                    `json:"registry_digest"`
	StateMachineDigest               string                                    `json:"state_machine_digest"`
	PolicyDigest                     string                                    `json:"policy_digest"`
	Disposition                      runnerLedgerPreflightDisposition          `json:"disposition"`
	SchemaBundleDigest               Digest                                    `json:"schema_bundle_digest"`
	ExecutionLineageDigest           Digest                                    `json:"execution_lineage_digest"`
	OrderedMigrationPrefixDigest     Digest                                    `json:"ordered_migration_prefix_digest"`
	OrderedMigrationPrefixLength     uint32                                    `json:"ordered_migration_prefix_length"`
	OrderedMigrationPrefixHead       *string                                   `json:"ordered_migration_prefix_head"`
	LastAppliedCatalogContractDigest Digest                                    `json:"last_applied_catalog_contract_digest"`
	NextEntry                        *runnerLedgerPreflightNextEntry           `json:"next_entry"`
	Recovery                         *runnerLedgerPreflightRecoveryDisposition `json:"recovery"`
}

func bindRunnerLedgerPreflightFact(profile runnerLedgerPreflightProfile, disposition runnerLedgerPreflightDisposition, input runnerLedgerPreflightFactInput) (runnerLedgerPreflightFact, error) {
	if !profile.valid() {
		return runnerLedgerPreflightFact{}, fail(CodeUntrusted, "runner-ledger-preflight-contract", "generated runner ledger preflight profile is unavailable or changed", nil)
	}
	fact := runnerLedgerPreflightFact{
		profileID: profile.profileID, profileDigest: profile.profileDigest,
		registryDigest:                   runnerLedgerPreflightRegistryDigest,
		stateMachineDigest:               runnerLedgerPreflightStateMachineDigest,
		policyDigest:                     runnerLedgerPreflightPolicyDigest,
		disposition:                      disposition,
		schemaBundleDigest:               input.SchemaBundleDigest,
		executionLineageDigest:           input.ExecutionLineageDigest,
		orderedMigrationPrefixDigest:     input.OrderedMigrationPrefixDigest,
		orderedMigrationPrefixLength:     input.OrderedMigrationPrefixLength,
		orderedMigrationPrefixHead:       cloneStringPointer(input.OrderedMigrationPrefixHead),
		lastAppliedCatalogContractDigest: input.LastAppliedCatalogContractDigest,
		nextEntry:                        cloneRunnerLedgerPreflightNextEntry(input.NextEntry),
		recovery:                         cloneRunnerLedgerPreflightRecovery(input.Recovery),
	}
	canonical, err := canonicalContractKey(fact.wire())
	if err != nil {
		return runnerLedgerPreflightFact{}, fail(CodeInvalidManifest, "runner-ledger-preflight-contract", "runner ledger preflight fact is not canonical", nil)
	}
	fact.canonical = canonical
	fact.subjectDigest = DigestBytes([]byte(runnerLedgerPreflightFactDigestDomain + "\x00" + canonical))
	if !fact.valid() {
		return runnerLedgerPreflightFact{}, fail(CodeInvalidManifest, "runner-ledger-preflight-contract", "runner ledger preflight fact violates its generated profile", nil)
	}
	return fact, nil
}

func (profile runnerLedgerPreflightProfile) valid() bool {
	return profile == generatedRunnerLedgerPreflightProfile &&
		Digest(profile.profileDigest).Validate() == nil &&
		Digest(runnerLedgerPreflightRegistryDigest).Validate() == nil &&
		Digest(runnerLedgerPreflightStateMachineDigest).Validate() == nil &&
		Digest(runnerLedgerPreflightPolicyDigest).Validate() == nil &&
		validRunnerLedgerPreflightTransitions() &&
		validRunnerLedgerPreflightRecoveryPairs()
}

func (fact runnerLedgerPreflightFact) valid() bool {
	if !generatedRunnerLedgerPreflightProfile.valid() ||
		fact.profileID != generatedRunnerLedgerPreflightProfile.profileID ||
		fact.profileDigest != generatedRunnerLedgerPreflightProfile.profileDigest ||
		fact.registryDigest != runnerLedgerPreflightRegistryDigest ||
		fact.stateMachineDigest != runnerLedgerPreflightStateMachineDigest ||
		fact.policyDigest != runnerLedgerPreflightPolicyDigest ||
		fact.schemaBundleDigest.Validate() != nil ||
		fact.executionLineageDigest.Validate() != nil ||
		fact.orderedMigrationPrefixDigest.Validate() != nil ||
		fact.lastAppliedCatalogContractDigest.Validate() != nil ||
		fact.subjectDigest.Validate() != nil ||
		!validRunnerLedgerPreflightPrefix(fact.orderedMigrationPrefixLength, fact.orderedMigrationPrefixHead) ||
		!validRunnerLedgerPreflightDisposition(fact) {
		return false
	}
	canonical, err := canonicalContractKey(fact.wire())
	return err == nil && canonical == fact.canonical &&
		fact.subjectDigest == DigestBytes([]byte(runnerLedgerPreflightFactDigestDomain+"\x00"+canonical))
}

func (fact runnerLedgerPreflightFact) clone() runnerLedgerPreflightFact {
	copy := fact
	copy.orderedMigrationPrefixHead = cloneStringPointer(fact.orderedMigrationPrefixHead)
	copy.nextEntry = cloneRunnerLedgerPreflightNextEntry(fact.nextEntry)
	copy.recovery = cloneRunnerLedgerPreflightRecovery(fact.recovery)
	return copy
}

func (fact runnerLedgerPreflightFact) canonicalBytes() []byte {
	if !fact.valid() {
		return nil
	}
	return []byte(fact.canonical)
}

func (fact runnerLedgerPreflightFact) wire() runnerLedgerPreflightFactWire {
	return runnerLedgerPreflightFactWire{
		ProfileID: fact.profileID, ProfileDigest: fact.profileDigest,
		RegistryDigest: fact.registryDigest, StateMachineDigest: fact.stateMachineDigest,
		PolicyDigest: fact.policyDigest, Disposition: fact.disposition,
		SchemaBundleDigest:               fact.schemaBundleDigest,
		ExecutionLineageDigest:           fact.executionLineageDigest,
		OrderedMigrationPrefixDigest:     fact.orderedMigrationPrefixDigest,
		OrderedMigrationPrefixLength:     fact.orderedMigrationPrefixLength,
		OrderedMigrationPrefixHead:       cloneStringPointer(fact.orderedMigrationPrefixHead),
		LastAppliedCatalogContractDigest: fact.lastAppliedCatalogContractDigest,
		NextEntry:                        cloneRunnerLedgerPreflightNextEntry(fact.nextEntry),
		Recovery:                         cloneRunnerLedgerPreflightRecovery(fact.recovery),
	}
}

func validRunnerLedgerPreflightDisposition(fact runnerLedgerPreflightFact) bool {
	switch fact.disposition {
	case runnerLedgerPreflightEmptyBrandNew:
		return fact.orderedMigrationPrefixLength == 0 && fact.nextEntry != nil &&
			validRunnerLedgerPreflightNextEntry(fact.nextEntry) &&
			validRunnerLedgerPreflightRecoveryPair(fact.disposition, fact.recovery)
	case runnerLedgerPreflightPartialNextEntry:
		return fact.orderedMigrationPrefixLength > 0 && validRunnerLedgerPreflightNextEntry(fact.nextEntry) &&
			validRunnerLedgerPreflightRecoveryPair(fact.disposition, fact.recovery)
	case runnerLedgerPreflightPartialRetryOrRecovery:
		return fact.orderedMigrationPrefixLength > 0 && fact.nextEntry == nil &&
			validRunnerLedgerPreflightRecoveryPair(fact.disposition, fact.recovery)
	case runnerLedgerPreflightCompleteReturnSuccess:
		return fact.orderedMigrationPrefixLength > 0 && fact.nextEntry == nil &&
			validRunnerLedgerPreflightRecoveryPair(fact.disposition, fact.recovery)
	case runnerLedgerPreflightUnknownOrFailed:
		return fact.nextEntry == nil && fact.recovery == nil
	default:
		return false
	}
}

func validRunnerLedgerPreflightPrefix(length uint32, head *string) bool {
	if length == 0 {
		return head == nil
	}
	return head != nil && migrationIDPattern.MatchString(*head)
}

func validRunnerLedgerPreflightNextEntry(entry *runnerLedgerPreflightNextEntry) bool {
	return entry != nil && migrationIDPattern.MatchString(entry.MigrationID) && entry.EntryDigest.Validate() == nil
}

func validRunnerLedgerPreflightRecoveryPair(disposition runnerLedgerPreflightDisposition, value *runnerLedgerPreflightRecoveryDisposition) bool {
	return value != nil && generatedRunnerLedgerPreflightRecoveryPairAllowed(disposition, value.State, value.Action)
}

func validRunnerLedgerPreflightRecoveryPairs() bool {
	return generatedRunnerLedgerPreflightRecoveryPairCount == 17
}

func validRunnerLedgerPreflightTransitions() bool {
	if len(generatedRunnerLedgerPreflightTransitions) != 5 {
		return false
	}
	seen := map[runnerLedgerPreflightDisposition]bool{}
	for _, transition := range generatedRunnerLedgerPreflightTransitions {
		if transition.from != "unclassified" || transition.event == "" || seen[transition.to] {
			return false
		}
		seen[transition.to] = true
	}
	return seen[runnerLedgerPreflightCompleteReturnSuccess] && seen[runnerLedgerPreflightEmptyBrandNew] &&
		seen[runnerLedgerPreflightPartialNextEntry] && seen[runnerLedgerPreflightPartialRetryOrRecovery] &&
		seen[runnerLedgerPreflightUnknownOrFailed]
}

func runnerLedgerPreflightDispositionForEvent(event string) (runnerLedgerPreflightDisposition, bool) {
	for _, transition := range generatedRunnerLedgerPreflightTransitions {
		if transition.from == "unclassified" && transition.event == event {
			return transition.to, true
		}
	}
	return "", false
}

func cloneRunnerLedgerPreflightNextEntry(value *runnerLedgerPreflightNextEntry) *runnerLedgerPreflightNextEntry {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func cloneRunnerLedgerPreflightRecovery(value *runnerLedgerPreflightRecoveryDisposition) *runnerLedgerPreflightRecoveryDisposition {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}
