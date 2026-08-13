package migration

// admissionHistoricalVerificationFacts is an ordinary, non-authoritative
// summary of immutable historical runtime and projection inputs. It contains
// neither executable SQL bytes nor a StatementPlan exactness sentinel.
type admissionHistoricalVerificationFacts struct {
	maxAttempts        uint32
	orderedMigrations  []string
	statementSubjects  map[string][][32]byte
	finalCatalogDigest map[string][32]byte
	ledgerRows         []CommitIntentLedgerRow
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
	facts := &admissionHistoricalVerificationFacts{
		maxAttempts:        uint32(manifest.ExecutionPolicy.MaxAttempts),
		orderedMigrations:  make([]string, 0, len(manifest.SchemaBundle.Migrations)),
		statementSubjects:  make(map[string][][32]byte, len(manifest.SchemaBundle.Migrations)),
		finalCatalogDigest: make(map[string][32]byte, len(manifest.SchemaBundle.Migrations)),
		ledgerRows:         make([]CommitIntentLedgerRow, 0, len(manifest.SchemaBundle.Migrations)),
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
		facts.ledgerRows = append(facts.ledgerRows, commitIntentLedgerRow(entry, manifest.SchemaBundleDigest))
	}
	for _, plan := range plans {
		subject, err := admissionStatementPlanSubjectFromPlan(plan)
		if err != nil {
			return nil, err
		}
		facts.statementSubjects[plan.MigrationID] = append(facts.statementSubjects[plan.MigrationID], subject)
	}
	for _, migration := range facts.orderedMigrations {
		if len(facts.statementSubjects[migration]) == 0 {
			return nil, fail(CodeInvalidManifest, "admission-historical-verification", "historical statement closure is empty", nil)
		}
	}
	return facts, nil
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
