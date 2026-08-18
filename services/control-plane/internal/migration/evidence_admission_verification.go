package migration

import "fmt"

// admissionHistoricalVerificationFacts is an ordinary, non-authoritative
// summary of immutable historical runtime and projection inputs. It contains
// neither executable SQL bytes nor a StatementPlan exactness sentinel.
type admissionHistoricalVerificationFacts struct {
	maxAttempts                                                        uint32
	lineageQuotaProfile                                                string
	manifestDigest, runnerProjectionDecisionDigest, schemaBundleDigest Digest
	authorityProfileDigest, authorityBindingDigest                     Digest
	orderedMigrations                                                  []string
	statementSubjects                                                  map[string][][32]byte
	finalCatalogDigest                                                 map[string][32]byte
	catalogContractDigest                                              map[string][32]byte
	attemptPredecessorCatalog                                          map[string][32]byte
	ledgerRows                                                         []CommitIntentLedgerRow
}

func buildHistoricalVerificationFacts(bundle *RuntimeBundle, bindings RunnerProjectionBindings) (*admissionHistoricalVerificationFacts, error) {
	validationTime, err := bindings.historicalValidationTime()
	if err != nil {
		return nil, err
	}
	if err := bindings.validateHistorical(); err != nil {
		return nil, err
	}
	plans, err := buildExactStatementPlans(bundle, bindings, validationTime)
	if err != nil {
		return nil, err
	}
	manifest, _, err := bundle.ownedInputs.copyVerified()
	if err != nil {
		return nil, err
	}
	if manifest.ExecutionPolicy.MaxAttempts == 0 || manifest.ExecutionPolicy.MaxAttempts > uint64(^uint32(0)) {
		return nil, fail(CodeInvalidManifest, "admission-historical-verification", "historical retry bound is invalid", nil)
	}
	quotaProfile, err := manifest.ExecutionPolicy.SelectedLineageQuotaProfile(manifest.FormatVersion)
	if err != nil {
		return nil, err
	}
	facts := &admissionHistoricalVerificationFacts{
		maxAttempts:         uint32(manifest.ExecutionPolicy.MaxAttempts),
		lineageQuotaProfile: quotaProfile,
		manifestDigest:      manifest.ManifestDigest, runnerProjectionDecisionDigest: bindings.runnerProjectionDecisionDigest,
		schemaBundleDigest: manifest.SchemaBundleDigest, authorityProfileDigest: bindings.authorityProfileDigest, authorityBindingDigest: bindings.authorityBindingDigest,
		orderedMigrations:         make([]string, 0, len(manifest.SchemaBundle.Migrations)),
		statementSubjects:         make(map[string][][32]byte, len(manifest.SchemaBundle.Migrations)),
		finalCatalogDigest:        make(map[string][32]byte, len(manifest.SchemaBundle.Migrations)),
		catalogContractDigest:     make(map[string][32]byte, len(manifest.SchemaBundle.Migrations)),
		attemptPredecessorCatalog: make(map[string][32]byte, len(manifest.SchemaBundle.Migrations)),
		ledgerRows:                make([]CommitIntentLedgerRow, 0, len(manifest.SchemaBundle.Migrations)),
	}
	for _, entry := range manifest.SchemaBundle.Migrations {
		binding, ok := exactCatalogBindingForHead(bindings.executableCatalogs, entry.ID)
		if !ok {
			return nil, fail(CodeUntrusted, "admission-historical-verification", "historical catalog binding is incomplete", nil)
		}
		final := CatalogStateProjection{Present: &SchemaPresentProjection{State: "schema_present", Scope: cloneProjectionValue(binding.verifiedCatalog.scope), Body: cloneProjectionValue(binding.verifiedCatalog.expected.Body)}}
		digest, err := final.ComputeDigest()
		if err != nil {
			return nil, err
		}
		facts.orderedMigrations = append(facts.orderedMigrations, entry.ID)
		facts.finalCatalogDigest[entry.ID] = digestRaw(digest)
		facts.catalogContractDigest[entry.ID] = digestRaw(entry.CatalogContract.SHA256)
		row := commitIntentLedgerRow(entry, manifest.SchemaBundleDigest)
		if err := row.Validate(); err != nil {
			return nil, fail(CodeInvalidManifest, "admission-historical-verification", "historical ledger row cannot be reconstructed", err)
		}
		facts.ledgerRows = append(facts.ledgerRows, row)
	}
	for _, plan := range plans {
		subject, err := admissionStatementPlanSubjectFromPlan(plan)
		if err != nil {
			return nil, err
		}
		facts.statementSubjects[plan.MigrationID] = append(facts.statementSubjects[plan.MigrationID], subject)
		if plan.StatementIndex == 0 {
			facts.attemptPredecessorCatalog[plan.MigrationID] = digestRaw(plan.ExpectedTransition.CatalogBefore.Digest)
		}
	}
	for _, migration := range facts.orderedMigrations {
		if len(facts.statementSubjects[migration]) == 0 || facts.attemptPredecessorCatalog[migration] == ([32]byte{}) {
			return nil, fail(CodeInvalidManifest, "admission-historical-verification", "historical statement closure is empty", nil)
		}
	}
	return facts, nil
}

// verifyAdmissionGeneration compares the complete compact pass-one result with
// facts reconstructed from the same-verifier historical decision. It returns
// no authority. Lifecycle-only retry and ambiguous receipts cannot be rebuilt
// from disk and therefore remain recovery-required after every stored fact has
// been checked.
func verifyAdmissionGeneration(generation *admissionReplayGeneration, facts *admissionHistoricalVerificationFacts) error {
	if generation == nil || facts == nil || generation.header == nil || generation.runtimeInspection == nil || facts.maxAttempts == 0 || len(facts.orderedMigrations) == 0 {
		return admissionCorrupt("admission-pass2", "historical generation facts are incomplete", nil)
	}
	header := generation.header
	inspection := generation.runtimeInspection
	if header.manifestDigest != facts.manifestDigest || header.limitsProfile != facts.lineageQuotaProfile || generation.runnerProjectionDecisionDigest != facts.runnerProjectionDecisionDigest || generation.schemaBundleDigest != facts.schemaBundleDigest || header.runnerProjectionDecisionDigest != facts.runnerProjectionDecisionDigest || header.schemaBundleDigest != facts.schemaBundleDigest || header.authorityProfileDigest != facts.authorityProfileDigest || header.authorityBindingDigest != facts.authorityBindingDigest || inspection.manifestDigest != facts.manifestDigest || inspection.lineageQuotaProfile != facts.lineageQuotaProfile || inspection.schemaBundleDigest != facts.schemaBundleDigest || inspection.maxAttempts != uint64(facts.maxAttempts) || len(inspection.statementCounts) != len(facts.orderedMigrations) {
		return admissionCorrupt("admission-pass2", "historical generation differs from recovered authority", nil)
	}
	for index, migration := range facts.orderedMigrations {
		if len(facts.statementSubjects[migration]) == 0 || inspection.statementCounts[index] != uint64(len(facts.statementSubjects[migration])) || index >= len(facts.ledgerRows) || facts.ledgerRows[index].MigrationID != migration {
			return admissionCorrupt("admission-pass2", "historical statement or ledger closure differs", nil)
		}
	}
	if len(facts.ledgerRows) != len(facts.orderedMigrations) {
		return admissionCorrupt("admission-pass2", "historical ledger closure differs", nil)
	}
	ordinals := make(map[string]int, len(facts.orderedMigrations))
	for index, migration := range facts.orderedMigrations {
		ordinals[migration] = index
	}
	var previousMigration string
	var previousAttempt uint32
	recoveryRequired := false
	for ordinal := range generation.verificationTerminals {
		event := generation.verificationTerminals[ordinal]
		migration := fmt.Sprintf("%06d", event.migrationID)
		index, ok := ordinals[migration]
		if !ok || event.attemptIndex == 0 || event.attemptIndex > facts.maxAttempts || facts.catalogContractDigest[migration] != generation.verificationCatalogContract {
			return admissionCorrupt("admission-pass2", "terminal identity differs from historical closure", nil)
		}
		if previousMigration != "" {
			previousIndex := ordinals[previousMigration]
			if migration == previousMigration {
				if event.attemptIndex != previousAttempt+1 {
					return admissionCorrupt("admission-pass2", "terminal attempt order differs from historical closure", nil)
				}
			} else if index != previousIndex+1 || event.attemptIndex != 1 {
				return admissionCorrupt("admission-pass2", "terminal migration order differs from historical closure", nil)
			}
		}
		if err := verifyAdmissionAttemptStatements(event.migrationID, event.attemptIndex, event.statementCount, event.lastStatementIndex, event.statementChain, facts.statementSubjects[migration]); err != nil {
			return err
		}
		final, commit, retry := sparseAdmissionTerminal(generation, uint32(ordinal))
		if event.flags&admissionTerminalHasFinal != 0 {
			if final == nil || event.statementCount != uint32(len(facts.statementSubjects[migration])) || event.lastStatementIndex+1 != event.statementCount || final.preledgerCatalog != facts.finalCatalogDigest[migration] {
				return admissionCorrupt("admission-pass2", "terminal final boundary differs from historical closure", nil)
			}
		} else if final != nil {
			return admissionCorrupt("admission-pass2", "terminal has an unexpected final boundary", nil)
		}
		if event.flags&admissionTerminalHasCommit != 0 {
			if commit == nil {
				return admissionCorrupt("admission-pass2", "terminal commit differs from historical closure", nil)
			}
			if err := verifyAdmissionCommit(*commit, migration, event.attemptIndex, index, facts); err != nil {
				return admissionCorrupt("admission-pass2", "terminal commit differs from historical closure", err)
			}
		} else if commit != nil {
			return admissionCorrupt("admission-pass2", "terminal has an unexpected commit boundary", nil)
		}
		if event.outcome == 2 && event.attemptIndex >= facts.maxAttempts {
			return admissionCorrupt("admission-pass2", "retry terminal exceeds historical retry bound", nil)
		}
		if event.flags&admissionTerminalHasRetry != 0 {
			if retry == nil {
				return admissionCorrupt("admission-pass2", "retry terminal lacks compact proof", nil)
			}
			recoveryRequired = true
		} else if retry != nil {
			return admissionCorrupt("admission-pass2", "terminal has an unexpected retry proof", nil)
		}
		if event.outcome >= 4 || event.flags&admissionTerminalHasResolution != 0 {
			recoveryRequired = true
		}
		previousMigration, previousAttempt = migration, event.attemptIndex
	}
	if open := generation.verificationOpen; open != nil {
		migration := fmt.Sprintf("%06d", open.migrationID)
		index, ok := ordinals[migration]
		if !ok || open.attemptIndex == 0 || open.attemptIndex > facts.maxAttempts || facts.catalogContractDigest[migration] != generation.verificationCatalogContract {
			return admissionCorrupt("admission-pass2", "open attempt differs from historical closure", nil)
		}
		if previousMigration != "" && (migration == previousMigration && open.attemptIndex != previousAttempt+1 || migration != previousMigration && (index != ordinals[previousMigration]+1 || open.attemptIndex != 1)) {
			return admissionCorrupt("admission-pass2", "open attempt order differs from historical closure", nil)
		}
		if err := verifyAdmissionAttemptStatements(open.migrationID, open.attemptIndex, open.statementCount, open.lastStatementIndex, open.statementChain, facts.statementSubjects[migration]); err != nil {
			return err
		}
		if open.commitPresent {
			commit := admissionReplayTerminalCommit{expectedLedgerLength: open.expectedLedgerLength, commitRecord: open.commitRecord, commitBody: open.commitBody, previousAttemptTerminal: open.previousAttemptTerminal, attemptPredecessorCatalog: open.attemptPredecessorCatalog, lastIntermediateState: open.lastIntermediateState}
			if err := verifyAdmissionCommit(commit, migration, open.attemptIndex, index, facts); err != nil {
				return admissionCorrupt("admission-pass2", "open commit differs from historical closure", err)
			}
		}
	}
	if recoveryRequired {
		return fail(CodeEvidenceRecoveryRequired, "admission-pass2", "historical lifecycle receipt is unavailable", nil)
	}
	return nil
}

func verifyAdmissionAttemptStatements(migrationNumber, attempt, count, last uint32, got [32]byte, subjects [][32]byte) error {
	if count == 0 {
		if last != 0 || got != ([32]byte{}) {
			return admissionCorrupt("admission-pass2", "empty statement prefix is inconsistent", nil)
		}
		return nil
	}
	if count > uint32(len(subjects)) || last+1 != count {
		return admissionCorrupt("admission-pass2", "statement prefix differs from historical closure", nil)
	}
	migration := fmt.Sprintf("%06d", migrationNumber)
	var chain [32]byte
	for index := uint32(0); index < count; index++ {
		chain = admissionStatementChainStep(chain, migration, attempt, index, subjects[index])
	}
	if chain != got {
		return admissionCorrupt("admission-pass2", "statement chain differs from historical closure", nil)
	}
	return nil
}

func sparseAdmissionTerminal(generation *admissionReplayGeneration, ordinal uint32) (*admissionReplayTerminalFinal, *admissionReplayTerminalCommit, *admissionReplayTerminalRetry) {
	var final *admissionReplayTerminalFinal
	var commit *admissionReplayTerminalCommit
	var retry *admissionReplayTerminalRetry
	for index := range generation.verificationFinals {
		if generation.verificationFinals[index].ordinal == ordinal {
			final = &generation.verificationFinals[index]
		}
	}
	for index := range generation.verificationCommits {
		if generation.verificationCommits[index].ordinal == ordinal {
			commit = &generation.verificationCommits[index]
		}
	}
	for index := range generation.verificationRetries {
		if generation.verificationRetries[index].ordinal == ordinal {
			retry = &generation.verificationRetries[index]
		}
	}
	return final, commit, retry
}

func verifyAdmissionCommit(compact admissionReplayTerminalCommit, migration string, attempt uint32, migrationIndex int, facts *admissionHistoricalVerificationFacts) error {
	if migrationIndex < 0 || migrationIndex >= len(facts.ledgerRows) || compact.expectedLedgerLength != uint32(migrationIndex+1) || compact.commitBody == ([32]byte{}) || compact.attemptPredecessorCatalog == ([32]byte{}) || compact.attemptPredecessorCatalog != facts.attemptPredecessorCatalog[migration] || compact.lastIntermediateState == ([32]byte{}) {
		return admissionCorrupt("admission-pass2", "commit compact facts are incomplete", nil)
	}
	var previous *Digest
	if attempt > 1 {
		if compact.previousAttemptTerminal == ([32]byte{}) {
			return admissionCorrupt("admission-pass2", "commit predecessor is absent", nil)
		}
		value := digestString(compact.previousAttemptTerminal)
		previous = &value
	} else if compact.previousAttemptTerminal != ([32]byte{}) {
		return admissionCorrupt("admission-pass2", "first commit has a predecessor", nil)
	}
	value := CommitIntent{
		SchemaBundleDigest: facts.schemaBundleDigest, CatalogContractDigest: digestString(facts.catalogContractDigest[migration]),
		AuthorityProfileDigest: facts.authorityProfileDigest, AuthorityBindingDigest: facts.authorityBindingDigest,
		MigrationID: migration, AttemptIndex: attempt, PreviousAttemptTerminalDigest: previous,
		AttemptPredecessorCatalogDigest: digestString(compact.attemptPredecessorCatalog), LastIntermediateStateDigest: digestString(compact.lastIntermediateState),
		ExpectedLedgerLength: compact.expectedLedgerLength, ExpectedLedgerHead: migration, LedgerRow: cloneProjectionValue(facts.ledgerRows[migrationIndex]),
	}
	if err := value.Validate(); err != nil {
		return err
	}
	digest, err := admissionCommitSubject(value)
	if err != nil || digest != compact.commitBody {
		return admissionCorrupt("admission-pass2", "commit subject differs from historical closure", err)
	}
	return nil
}

func admissionStatementPlanSubjectFromPlan(plan StatementPlan) ([32]byte, error) {
	if err := plan.validateExact(); err != nil {
		return [32]byte{}, err
	}
	return admissionStatementPlanSubject(StatementIntent{
		SQLArtifactSHA256: plan.SQLArtifactSHA256, SQLArtifactSizeBytes: plan.SQLArtifactSizeBytes,
		StartOffset: plan.StartOffset, EndOffset: plan.EndOffset, StatementSHA256: plan.StatementSHA256,
		Classification: cloneProjectionValue(plan.Classification), ExpectedTransitionDigest: plan.ExpectedTransitionDigest,
	})
}

func commitIntentLedgerRow(entry MigrationEntry, bundle Digest) CommitIntentLedgerRow {
	return CommitIntentLedgerRow{
		MigrationID: entry.ID, MigrationName: entry.Name, PredecessorID: cloneProjectionValue(entry.PredecessorID),
		Phase: entry.Phase, SchemaFrom: entry.SchemaFrom, SchemaTo: entry.SchemaTo,
		CompatibleBinaryMin: entry.CompatibleControlPlaneMin, CompatibleBinaryMax: entry.CompatibleControlPlaneMax,
		SQLPath: entry.SQLArtifact.Path, SQLSizeBytes: entry.SQLArtifact.SizeBytes, SQLSHA256: entry.SQLArtifact.SHA256,
		BundleDigest: bundle, TransactionMode: entry.TransactionMode, Reentrancy: entry.Reentrancy,
		RollbackBoundary: entry.RollbackBoundary, RequiresLiveInstancePreflight: entry.RequiresLiveInstancePreflight,
		RequiresPITRPreflight: entry.RequiresPITRPreflight,
	}
}
