package migration

import (
	"context"
	"time"
)

const runnerLedgerCatalogPreflightDigestDomain = "cloud-agents-platform-runner-ledger-catalog-preflight/v1"

type runnerLedgerCatalogState string

const (
	runnerLedgerCatalogEmpty    runnerLedgerCatalogState = "empty"
	runnerLedgerCatalogPartial  runnerLedgerCatalogState = "partial"
	runnerLedgerCatalogComplete runnerLedgerCatalogState = "complete"
)

// runnerLedgerCatalogPreflight is a sealed, read-only observation. It owns no
// database session, transaction, evidence handle, receipt, or writer token.
// Slice C consumes it only through the package-private same-verifier claim
// binder; Runner.Run and every writer path remain disconnected.
type runnerLedgerCatalogPreflight struct {
	profileID                      string
	profileDigest                  string
	registryDigest                 string
	stateMachineDigest             string
	policyDigest                   string
	state                          runnerLedgerCatalogState
	schemaBundleDigest             Digest
	executionLineageDigest         Digest
	runnerProjectionDecisionDigest Digest
	authoritySubjectDigest         Digest
	projectionSubjectDigest        Digest
	catalogContractDigest          *Digest
	migrationCount                 uint32
	ledger                         runnerLedgerPrefix
	connectedAuthority             ProjectionResult[AuthorityProjection]
	migrationRoleAuthority         ProjectionResult[AuthorityProjection]
	initialPredecessor             *ProjectionResult[CatalogStateProjection]
	cumulativeCatalog              *ProjectionResult[CatalogProjection]
	subjectDigest                  Digest
}

type runnerLedgerCatalogPreflightWire struct {
	ProfileID                      string                                    `json:"profile_id"`
	ProfileDigest                  string                                    `json:"profile_digest"`
	RegistryDigest                 string                                    `json:"registry_digest"`
	StateMachineDigest             string                                    `json:"state_machine_digest"`
	PolicyDigest                   string                                    `json:"policy_digest"`
	State                          runnerLedgerCatalogState                  `json:"state"`
	SchemaBundleDigest             Digest                                    `json:"schema_bundle_digest"`
	ExecutionLineageDigest         Digest                                    `json:"execution_lineage_digest"`
	RunnerProjectionDecisionDigest Digest                                    `json:"runner_projection_decision_digest"`
	AuthoritySubjectDigest         Digest                                    `json:"authority_subject_digest"`
	ProjectionSubjectDigest        Digest                                    `json:"projection_subject_digest"`
	CatalogContractDigest          *Digest                                   `json:"catalog_contract_digest"`
	MigrationCount                 uint32                                    `json:"migration_count"`
	LedgerRows                     []CommitIntentLedgerRow                   `json:"ledger_rows"`
	LedgerDigest                   Digest                                    `json:"ledger_digest"`
	LedgerHead                     string                                    `json:"ledger_head"`
	ConnectedAuthority             ProjectionResult[AuthorityProjection]     `json:"connected_authority"`
	MigrationRoleAuthority         ProjectionResult[AuthorityProjection]     `json:"migration_role_authority"`
	InitialPredecessor             *ProjectionResult[CatalogStateProjection] `json:"initial_predecessor"`
	CumulativeCatalog              *ProjectionResult[CatalogProjection]      `json:"cumulative_catalog"`
}

// projectRunnerLedgerCatalogPreflight is the Slice B kernel. Its sole
// production caller is the package-private Slice C claim service, and it never
// opens a migration transaction.
func (runner *Runner) projectRunnerLedgerCatalogPreflight(ctx context.Context, dsn string, bundle *RuntimeBundle, plans []StatementPlan, evidence EvidenceSession, candidate OwnedCurrentCandidate) (*runnerLedgerCatalogPreflight, error) {
	if runner == nil || ctx == nil {
		return nil, fail(CodeTransactionBoundary, "runner-ledger-catalog-preflight", "runner or projection context is unavailable", nil)
	}
	if err := ctx.Err(); err != nil {
		return nil, mapRunnerDatabasePreflightError(err, "runner-ledger-catalog-preflight", "ledger catalog preflight was interrupted")
	}
	bindings, err := runnerCurrentProjectionBindings(evidence, candidate)
	if err != nil {
		return nil, err
	}
	if err := bindings.validateAt(time.Now()); err != nil {
		return nil, err
	}
	verifiedBundle, err := verifiedRunnerLedgerCatalogBundle(bundle)
	if err != nil {
		return nil, err
	}
	bundle = verifiedBundle
	verifiedPlans, err := buildExactStatementPlans(bundle, bindings, time.Now())
	if err != nil {
		return nil, err
	}
	plans = verifiedPlans
	if runner.Connector == nil {
		return nil, fail(CodeProjectionNotImplemented, "runner-ledger-catalog-connector", "no dedicated database connector is configured", nil)
	}
	if dsn == "" || len(bundle.Manifest.SchemaBundle.Migrations) == 0 ||
		uint64(len(bundle.Manifest.SchemaBundle.Migrations)) > uint64(^uint32(0)) ||
		bindings.schemaBundleDigest != bundle.Manifest.SchemaBundleDigest {
		return nil, fail(CodeUntrusted, "runner-ledger-catalog-preflight", "projection inputs differ from the active verified runtime", nil)
	}
	if !generatedRunnerLedgerPreflightProfile.valid() {
		return nil, fail(CodeUntrusted, "runner-ledger-catalog-preflight", "generated runner ledger preflight profile is unavailable or changed", nil)
	}
	key, err := bundle.Manifest.SchemaBundle.AdvisoryLock.Key()
	if err != nil {
		return nil, err
	}
	session, connectErr := runner.Connector.Connect(ctx, dsn)
	if connectErr != nil || session == nil {
		primary := connectErr
		if primary == nil {
			primary = fail(CodeTransactionBoundary, "runner-ledger-catalog-connect", "database connector returned no dedicated session", nil)
		} else {
			primary = mapRunnerDatabasePreflightError(primary, "runner-ledger-catalog-connect", "dedicated database connection failed")
		}
		return nil, closeRunnerDatabasePreflight(session, 0, false, primary)
	}
	locked := false
	failClosed := func(primary error) (*runnerLedgerCatalogPreflight, error) {
		return nil, closeRunnerDatabasePreflight(session, key, locked, primary)
	}

	connected, err := runner.projectRunnerAuthorityPhase(ctx, session, bindings.verifiedAuthority, AuthorityPhaseConnectedSession)
	if err != nil {
		return failClosed(err)
	}
	major := connected.Metadata.Snapshot.PostgresMajor
	policy := bundle.Manifest.ExecutionPolicy
	if uint64(major) < policy.PostgresMajorMin || uint64(major) > policy.PostgresMajorMax {
		return failClosed(fail(CodeUnsupported, "runner-ledger-catalog-server-version", "PostgreSQL major is outside the signed execution policy", nil))
	}
	if err := session.SetRoleAndSettings(ctx, policy); err != nil {
		return failClosed(mapRunnerDatabasePreflightError(err, "runner-ledger-catalog-settings", "dedicated session role or settings could not be configured"))
	}
	if err := session.AcquireAdvisoryLock(ctx, key); err != nil {
		return failClosed(mapRunnerDatabasePreflightError(err, "runner-ledger-catalog-lock", "signed advisory lock could not be acquired"))
	}
	locked = true
	migrationRole, err := runner.projectRunnerAuthorityPhase(ctx, session, bindings.verifiedAuthority, AuthorityPhaseMigrationRole)
	if err != nil {
		return failClosed(err)
	}
	if !sameRunnerDedicatedSessionIdentity(connected.Metadata.Snapshot, migrationRole.Metadata.Snapshot) {
		return failClosed(fail(CodeProjectionMetadataMismatch, "runner-ledger-catalog-authority", "authority phases do not describe the same dedicated database session", nil))
	}

	before, err := readRunnerLedgerPrefix(ctx, session, bundle)
	if err != nil {
		return failClosed(err)
	}
	var initial *ProjectionResult[CatalogStateProjection]
	var cumulative *ProjectionResult[CatalogProjection]
	var catalogContractDigest *Digest
	var projectionSubjectDigest Digest
	if len(before.rows) == 0 {
		firstPlan, planErr := firstRunnerStatementPlan(bundle, plans)
		if planErr != nil {
			return failClosed(planErr)
		}
		projected, projectionErr := runner.projectRunnerInitialPrecondition(ctx, session, bindings.initialSchemaScope, firstPlan)
		if projectionErr != nil {
			return failClosed(projectionErr)
		}
		initial = &projected
		projectionSubjectDigest = bindings.initialSchemaScope.SubjectDigest()
		if !sameRunnerDatabaseIdentity(migrationRole.Metadata.Snapshot, projected.Metadata.Snapshot) {
			return failClosed(fail(CodeProjectionMetadataMismatch, "runner-ledger-catalog-predecessor", "initial predecessor projection does not describe the locked database session", nil))
		}
	} else {
		catalogBinding, ok := exactCatalogBindingForHead(bindings.executableCatalogs, before.head)
		if !ok || catalogBinding.catalogContractDigest.Validate() != nil || catalogBinding.verifiedCatalog.validate() != nil {
			return failClosed(fail(CodeUntrusted, "runner-ledger-catalog-selection", "ledger head has no exact signed cumulative catalog", nil))
		}
		projected, projectionErr := runner.projectRunnerCumulativeCatalog(ctx, session, catalogBinding.verifiedCatalog)
		if projectionErr != nil {
			return failClosed(projectionErr)
		}
		cumulative = &projected
		ownedDigest := catalogBinding.catalogContractDigest
		catalogContractDigest = &ownedDigest
		projectionSubjectDigest = catalogBinding.verifiedCatalog.SubjectDigest()
		if !sameRunnerDatabaseIdentity(migrationRole.Metadata.Snapshot, projected.Metadata.Snapshot) {
			return failClosed(fail(CodeProjectionMetadataMismatch, "runner-ledger-catalog-cumulative", "cumulative catalog projection does not describe the locked database session", nil))
		}
	}

	after, err := readRunnerLedgerPrefix(ctx, session, bundle)
	if err != nil {
		return failClosed(err)
	}
	if !sameRunnerLedgerPrefix(before, after) {
		return failClosed(fail(CodeInvalidLedger, "runner-ledger-catalog-preflight", "ledger prefix changed across the catalog projection", nil))
	}
	if err := closeRunnerDatabasePreflight(session, key, true, nil); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, mapRunnerDatabasePreflightError(err, "runner-ledger-catalog-preflight", "ledger catalog preflight was interrupted before sealing")
	}
	return bindRunnerLedgerCatalogPreflight(
		bindings, bundle, plans, before, connected, migrationRole,
		initial, cumulative, catalogContractDigest, projectionSubjectDigest,
	)
}

func (runner *Runner) projectRunnerCumulativeCatalog(ctx context.Context, session DatabaseSession, contract VerifiedCatalogContract) (ProjectionResult[CatalogProjection], error) {
	snapshot, err := BeginRunnerSessionProjectionSnapshot(ctx, session, AuthorityPhaseMigrationRole)
	if err != nil {
		return ProjectionResult[CatalogProjection]{}, mapRunnerDatabasePreflightError(err, "runner-ledger-catalog-snapshot", "cumulative catalog snapshot could not be opened")
	}
	if snapshot == nil {
		return ProjectionResult[CatalogProjection]{}, fail(CodeProjectionSnapshotInvalid, "runner-ledger-catalog-snapshot", "cumulative catalog snapshot is unavailable", nil)
	}
	metadata := snapshot.Metadata()
	factory := runner.projectionFactory
	if factory == nil {
		factory = pgRunnerAuthorityProjectorFactory{}
	}
	projector, factoryErr := factory.newRunnerAuthorityProjector(ctx, snapshot)
	var result ProjectionResult[CatalogProjection]
	var projectionErr error
	if factoryErr == nil && projector != nil {
		result, projectionErr = projector.ProjectCatalog(ctx, snapshot, contract, contract.Scope())
	} else if factoryErr != nil {
		projectionErr = mapRunnerDatabasePreflightError(factoryErr, "runner-ledger-catalog-projector", "cumulative catalog projector could not be constructed")
	} else {
		projectionErr = fail(CodeProjectionSnapshotInvalid, "runner-ledger-catalog-projector", "cumulative catalog projector is unavailable", nil)
	}
	closeCtx, cancel := cleanupContext()
	closeErr := snapshot.RollbackAndReturnToRunner(closeCtx)
	cancel()
	if closeErr != nil {
		return ProjectionResult[CatalogProjection]{}, mapRunnerDatabasePreflightError(closeErr, "runner-ledger-catalog-snapshot-close", "cumulative catalog snapshot could not return the dedicated session")
	}
	if projectionErr != nil {
		return ProjectionResult[CatalogProjection]{}, mapRunnerDatabasePreflightError(projectionErr, "runner-ledger-catalog-projection", "cumulative catalog projection failed")
	}
	if err := validateRunnerLedgerCatalogResult(result, metadata, contract); err != nil {
		return ProjectionResult[CatalogProjection]{}, err
	}
	return result, nil
}

func validateRunnerLedgerCatalogResult(result ProjectionResult[CatalogProjection], snapshot SnapshotMetadata, contract VerifiedCatalogContract) error {
	if snapshot.validate() != nil || snapshot.AuthorityPhase != AuthorityPhaseMigrationRole || contract.validate() != nil {
		return fail(CodeProjectionMetadataMismatch, "runner-ledger-catalog-result", "cumulative catalog inputs are invalid", nil)
	}
	scope := contract.Scope()
	if result.Metadata.validate() != nil || !runnerCanonicalEqual(result.Metadata.Snapshot, snapshot) ||
		result.Metadata.VerifiedSubjectDigest != contract.SubjectDigest() || result.Metadata.QueryCount == 0 ||
		result.Metadata.RowCount == 0 || result.Metadata.TotalBytes == 0 || result.Metadata.Scope == nil ||
		!equalProjectionScopes(*result.Metadata.Scope, scope) {
		return fail(CodeProjectionMetadataMismatch, "runner-ledger-catalog-result", "cumulative catalog metadata is incomplete or mismatched", nil)
	}
	expected := contract.ExpectedProjection()
	digest, err := digestProjectionWrapper(CatalogProjectionDigestDomain, result.Projection)
	if err != nil || result.Projection.Validate() != nil || digest != result.Digest || !runnerCanonicalEqual(result.Projection, expected) {
		return fail(CodeCatalogDrift, "runner-ledger-catalog-result", "cumulative catalog differs from the verified final projection", nil)
	}
	return nil
}

func bindRunnerLedgerCatalogPreflight(bindings RunnerProjectionBindings, bundle *RuntimeBundle, plans []StatementPlan, ledger runnerLedgerPrefix, connected, migrationRole ProjectionResult[AuthorityProjection], initial *ProjectionResult[CatalogStateProjection], cumulative *ProjectionResult[CatalogProjection], catalogContractDigest *Digest, projectionSubjectDigest Digest) (*runnerLedgerCatalogPreflight, error) {
	verifiedBundle, err := verifiedRunnerLedgerCatalogBundle(bundle)
	if err != nil {
		return nil, err
	}
	bundle = verifiedBundle
	verifiedPlans, err := buildExactStatementPlans(bundle, bindings, time.Now())
	if err != nil {
		return nil, err
	}
	plans = verifiedPlans
	if !generatedRunnerLedgerPreflightProfile.valid() || bindings.validateAt(time.Now()) != nil ||
		bindings.schemaBundleDigest != bundle.Manifest.SchemaBundleDigest || uint64(len(bundle.Manifest.SchemaBundle.Migrations)) > uint64(^uint32(0)) ||
		len(bundle.Manifest.SchemaBundle.Migrations) == 0 || projectionSubjectDigest.Validate() != nil {
		return nil, fail(CodeUntrusted, "runner-ledger-catalog-bind", "verified runtime or projection bindings are unavailable", nil)
	}
	if err := validateRunnerLedgerPrefixForBundle(ledger, bundle); err != nil {
		return nil, err
	}
	if err := validateRunnerAuthorityProjectionResult(connected, connected.Metadata.Snapshot, bindings.verifiedAuthority, AuthorityPhaseConnectedSession); err != nil {
		return nil, err
	}
	if err := validateRunnerAuthorityProjectionResult(migrationRole, migrationRole.Metadata.Snapshot, bindings.verifiedAuthority, AuthorityPhaseMigrationRole); err != nil {
		return nil, err
	}
	if !sameRunnerDedicatedSessionIdentity(connected.Metadata.Snapshot, migrationRole.Metadata.Snapshot) {
		return nil, fail(CodeProjectionMetadataMismatch, "runner-ledger-catalog-bind", "authority projections describe different database sessions", nil)
	}

	state := runnerLedgerCatalogEmpty
	if len(ledger.rows) > 0 && len(ledger.rows) < len(bundle.Manifest.SchemaBundle.Migrations) {
		state = runnerLedgerCatalogPartial
	} else if len(ledger.rows) == len(bundle.Manifest.SchemaBundle.Migrations) {
		state = runnerLedgerCatalogComplete
	}
	if state == runnerLedgerCatalogEmpty {
		if initial == nil || cumulative != nil || catalogContractDigest != nil || projectionSubjectDigest != bindings.initialSchemaScope.SubjectDigest() {
			return nil, fail(CodeProjectionMetadataMismatch, "runner-ledger-catalog-bind", "empty prefix lacks the exact initial predecessor projection", nil)
		}
		firstPlan, err := firstRunnerStatementPlan(bundle, plans)
		if err != nil {
			return nil, err
		}
		condition := bindings.initialSchemaScope.BoundPrecondition()
		if err := validateRunnerInitialPreconditionResult(*initial, initial.Metadata.Snapshot, bindings.initialSchemaScope, condition, firstPlan.ExpectedTransition.CatalogBefore); err != nil {
			return nil, err
		}
		if !sameRunnerDatabaseIdentity(migrationRole.Metadata.Snapshot, initial.Metadata.Snapshot) {
			return nil, fail(CodeProjectionMetadataMismatch, "runner-ledger-catalog-bind", "initial predecessor changed before sealing", nil)
		}
	} else {
		catalog, ok := exactCatalogBindingForHead(bindings.executableCatalogs, ledger.head)
		if initial != nil || cumulative == nil || catalogContractDigest == nil || !ok ||
			*catalogContractDigest != catalog.catalogContractDigest || projectionSubjectDigest != catalog.verifiedCatalog.SubjectDigest() {
			return nil, fail(CodeUntrusted, "runner-ledger-catalog-bind", "non-empty prefix lacks the exact cumulative catalog binding", nil)
		}
		if err := validateRunnerLedgerCatalogResult(*cumulative, cumulative.Metadata.Snapshot, catalog.verifiedCatalog); err != nil {
			return nil, err
		}
		if !sameRunnerDatabaseIdentity(migrationRole.Metadata.Snapshot, cumulative.Metadata.Snapshot) {
			return nil, fail(CodeProjectionMetadataMismatch, "runner-ledger-catalog-bind", "cumulative catalog changed before sealing", nil)
		}
	}

	prepared := &runnerLedgerCatalogPreflight{
		profileID: generatedRunnerLedgerPreflightProfile.profileID, profileDigest: generatedRunnerLedgerPreflightProfile.profileDigest,
		registryDigest: runnerLedgerPreflightRegistryDigest, stateMachineDigest: runnerLedgerPreflightStateMachineDigest,
		policyDigest: runnerLedgerPreflightPolicyDigest, state: state,
		schemaBundleDigest: bindings.schemaBundleDigest, executionLineageDigest: bindings.executionLineageDigest,
		runnerProjectionDecisionDigest: bindings.runnerProjectionDecisionDigest,
		authoritySubjectDigest:         bindings.verifiedAuthority.SubjectDigest(), projectionSubjectDigest: projectionSubjectDigest,
		catalogContractDigest: cloneDigestPointer(catalogContractDigest), migrationCount: uint32(len(bundle.Manifest.SchemaBundle.Migrations)),
		ledger: cloneRunnerLedgerPrefix(ledger), connectedAuthority: cloneProjectionValue(connected),
		migrationRoleAuthority: cloneProjectionValue(migrationRole),
		initialPredecessor:     cloneCatalogStateProjectionResultPointer(initial), cumulativeCatalog: cloneCatalogProjectionResultPointer(cumulative),
	}
	prepared.subjectDigest = runnerLedgerCatalogPreflightSubjectDigest(prepared)
	if prepared.subjectDigest.Validate() != nil {
		return nil, fail(CodeTransactionBoundary, "runner-ledger-catalog-bind", "read-only projection result could not be identified", nil)
	}
	if !validRunnerLedgerCatalogPreflight(prepared) {
		return nil, fail(CodeTransactionBoundary, "runner-ledger-catalog-bind", "read-only projection result could not be sealed", nil)
	}
	return prepared, nil
}

func validRunnerLedgerCatalogPreflight(prepared *runnerLedgerCatalogPreflight) bool {
	return prepared != nil && prepared.subjectDigest.Validate() == nil &&
		prepared.subjectDigest == runnerLedgerCatalogPreflightSubjectDigest(prepared) &&
		validRunnerLedgerCatalogPreflightShape(prepared)
}

func validRunnerLedgerCatalogPreflightShape(prepared *runnerLedgerCatalogPreflight) bool {
	if prepared.profileID != generatedRunnerLedgerPreflightProfile.profileID || prepared.profileDigest != generatedRunnerLedgerPreflightProfile.profileDigest ||
		prepared.registryDigest != runnerLedgerPreflightRegistryDigest || prepared.stateMachineDigest != runnerLedgerPreflightStateMachineDigest ||
		prepared.policyDigest != runnerLedgerPreflightPolicyDigest || !generatedRunnerLedgerPreflightProfile.valid() || prepared.migrationCount == 0 ||
		prepared.schemaBundleDigest.Validate() != nil || prepared.executionLineageDigest.Validate() != nil ||
		prepared.runnerProjectionDecisionDigest.Validate() != nil || prepared.authoritySubjectDigest.Validate() != nil ||
		prepared.projectionSubjectDigest.Validate() != nil || uint64(len(prepared.ledger.rows)) > uint64(prepared.migrationCount) ||
		!validRunnerLedgerPrefixShape(prepared.ledger) ||
		!validRunnerLedgerAuthorityResult(prepared.connectedAuthority, prepared.authoritySubjectDigest, AuthorityPhaseConnectedSession) ||
		!validRunnerLedgerAuthorityResult(prepared.migrationRoleAuthority, prepared.authoritySubjectDigest, AuthorityPhaseMigrationRole) ||
		!sameRunnerDedicatedSessionIdentity(prepared.connectedAuthority.Metadata.Snapshot, prepared.migrationRoleAuthority.Metadata.Snapshot) {
		return false
	}
	switch prepared.state {
	case runnerLedgerCatalogEmpty:
		return len(prepared.ledger.rows) == 0 && prepared.ledger.head == "" && prepared.catalogContractDigest == nil &&
			prepared.initialPredecessor != nil && prepared.cumulativeCatalog == nil &&
			validRunnerLedgerInitialResult(*prepared.initialPredecessor, prepared.projectionSubjectDigest) &&
			sameRunnerDatabaseIdentity(prepared.migrationRoleAuthority.Metadata.Snapshot, prepared.initialPredecessor.Metadata.Snapshot)
	case runnerLedgerCatalogPartial:
		return len(prepared.ledger.rows) > 0 && uint64(len(prepared.ledger.rows)) < uint64(prepared.migrationCount) &&
			prepared.catalogContractDigest != nil && prepared.catalogContractDigest.Validate() == nil && prepared.initialPredecessor == nil &&
			*prepared.catalogContractDigest == prepared.projectionSubjectDigest &&
			prepared.cumulativeCatalog != nil && prepared.cumulativeCatalog.Projection.SchemaHead == prepared.ledger.head &&
			validRunnerLedgerCumulativeResult(*prepared.cumulativeCatalog, prepared.projectionSubjectDigest) &&
			sameRunnerDatabaseIdentity(prepared.migrationRoleAuthority.Metadata.Snapshot, prepared.cumulativeCatalog.Metadata.Snapshot)
	case runnerLedgerCatalogComplete:
		return uint64(len(prepared.ledger.rows)) == uint64(prepared.migrationCount) && prepared.catalogContractDigest != nil &&
			prepared.catalogContractDigest.Validate() == nil && prepared.initialPredecessor == nil && prepared.cumulativeCatalog != nil &&
			*prepared.catalogContractDigest == prepared.projectionSubjectDigest &&
			prepared.cumulativeCatalog.Projection.SchemaHead == prepared.ledger.head &&
			validRunnerLedgerCumulativeResult(*prepared.cumulativeCatalog, prepared.projectionSubjectDigest) &&
			sameRunnerDatabaseIdentity(prepared.migrationRoleAuthority.Metadata.Snapshot, prepared.cumulativeCatalog.Metadata.Snapshot)
	default:
		return false
	}
}

// verifiedRunnerLedgerCatalogBundle reconstructs the runtime view from the
// privately owned LoadRuntimeBundle inputs. The public RuntimeBundle fields
// are projections for callers and cannot drive the read-only database kernel.
func verifiedRunnerLedgerCatalogBundle(bundle *RuntimeBundle) (*RuntimeBundle, error) {
	if bundle == nil {
		return nil, fail(CodeUntrusted, "runner-ledger-catalog-runtime", "verified runtime bundle is unavailable", nil)
	}
	manifest, files, err := bundle.ownedInputs.copyVerified()
	if err != nil {
		return nil, err
	}
	manifestRaw, ok := files[RuntimeManifestPath]
	if !ok {
		return nil, fail(CodeUntrusted, "runner-ledger-catalog-runtime", "owned runtime manifest is unavailable", nil)
	}
	decodedManifest, _, err := DecodeManifest(manifestRaw)
	if err != nil || !runnerCanonicalEqual(*manifest, *decodedManifest) {
		return nil, fail(CodeUntrusted, "runner-ledger-catalog-runtime", "owned runtime manifest cannot be reproduced", err)
	}
	if err := validateRuntimeRecords(decodedManifest, files); err != nil {
		return nil, fail(CodeUntrusted, "runner-ledger-catalog-runtime", "owned runtime records cannot be reproduced", err)
	}
	schemaRaw, ok := files[RuntimeSchemaBundlePath]
	if !ok {
		return nil, fail(CodeUntrusted, "runner-ledger-catalog-runtime", "owned schema bundle is unavailable", nil)
	}
	current, err := DecodeSchemaBundleDocument(schemaRaw)
	if err != nil || current.SchemaBundleDigest != decodedManifest.SchemaBundleDigest ||
		!runnerCanonicalEqual(current.SchemaBundle, decodedManifest.SchemaBundle) {
		return nil, fail(CodeUntrusted, "runner-ledger-catalog-runtime", "owned schema bundle differs from the runtime manifest", err)
	}
	lineage, err := validateSchemaLineage(current, files)
	if err != nil {
		return nil, fail(CodeUntrusted, "runner-ledger-catalog-runtime", "owned runtime lineage cannot be reproduced", err)
	}
	if err := validateRuntimeClosure(decodedManifest, lineage); err != nil {
		return nil, fail(CodeUntrusted, "runner-ledger-catalog-runtime", "owned runtime closure cannot be reproduced", err)
	}
	ownedInputs, err := bindVerifiedRuntimeBundleInputs(decodedManifest, files, bundle.ownedInputs.outerArtifactDigest, bundle.ownedInputs.outerArtifactSize)
	if err != nil {
		return nil, err
	}
	return &RuntimeBundle{Manifest: decodedManifest, Lineage: lineage, Files: files, ownedInputs: ownedInputs}, nil
}

func runnerLedgerCatalogPreflightSubjectDigest(prepared *runnerLedgerCatalogPreflight) Digest {
	if prepared == nil {
		return ""
	}
	canonical, err := canonicalContractKey(prepared.wire())
	if err != nil || canonical == "" {
		return ""
	}
	return DigestBytes([]byte(runnerLedgerCatalogPreflightDigestDomain + "\x00" + canonical))
}

func (prepared *runnerLedgerCatalogPreflight) wire() runnerLedgerCatalogPreflightWire {
	return runnerLedgerCatalogPreflightWire{
		ProfileID: prepared.profileID, ProfileDigest: prepared.profileDigest, RegistryDigest: prepared.registryDigest,
		StateMachineDigest: prepared.stateMachineDigest, PolicyDigest: prepared.policyDigest, State: prepared.state,
		SchemaBundleDigest: prepared.schemaBundleDigest, ExecutionLineageDigest: prepared.executionLineageDigest,
		RunnerProjectionDecisionDigest: prepared.runnerProjectionDecisionDigest, AuthoritySubjectDigest: prepared.authoritySubjectDigest,
		ProjectionSubjectDigest: prepared.projectionSubjectDigest, CatalogContractDigest: cloneDigestPointer(prepared.catalogContractDigest),
		MigrationCount: prepared.migrationCount, LedgerRows: cloneProjectionValue(prepared.ledger.rows), LedgerDigest: prepared.ledger.digest,
		LedgerHead: prepared.ledger.head, ConnectedAuthority: cloneProjectionValue(prepared.connectedAuthority),
		MigrationRoleAuthority: cloneProjectionValue(prepared.migrationRoleAuthority),
		InitialPredecessor:     cloneCatalogStateProjectionResultPointer(prepared.initialPredecessor),
		CumulativeCatalog:      cloneCatalogProjectionResultPointer(prepared.cumulativeCatalog),
	}
}

func validateRunnerLedgerPrefixForBundle(prefix runnerLedgerPrefix, bundle *RuntimeBundle) error {
	if bundle == nil || bundle.Lineage == nil || prefix.rows == nil {
		return fail(CodeInvalidLedger, "runner-ledger-catalog-prefix", "ledger prefix is unavailable", nil)
	}
	observed := make([]LedgerRow, len(prefix.rows))
	for index, row := range prefix.rows {
		if row.Validate() != nil || row.SQLSizeBytes > uint64(^uint64(0)>>1) {
			return fail(CodeInvalidLedger, "runner-ledger-catalog-prefix", "ledger prefix contains an invalid row", nil)
		}
		observed[index] = LedgerRow{
			MigrationID: row.MigrationID, MigrationName: row.MigrationName, PredecessorID: cloneStringPointer(row.PredecessorID),
			Phase: row.Phase, SchemaFrom: row.SchemaFrom, SchemaTo: row.SchemaTo,
			CompatibleBinaryMin: row.CompatibleBinaryMin, CompatibleBinaryMax: row.CompatibleBinaryMax,
			SQLPath: row.SQLPath, SQLSizeBytes: int64(row.SQLSizeBytes), SQLSHA256: row.SQLSHA256, BundleDigest: row.BundleDigest,
			TransactionMode: row.TransactionMode, Reentrancy: row.Reentrancy, RollbackBoundary: row.RollbackBoundary,
			RequiresLiveInstancePreflight: row.RequiresLiveInstancePreflight, RequiresPITRPreflight: row.RequiresPITRPreflight,
		}
	}
	snapshot, err := ValidateLedger(observed, bundle.Lineage)
	if err != nil || snapshot == nil || snapshot.Head != prefix.head {
		return fail(CodeInvalidLedger, "runner-ledger-catalog-prefix", "ledger prefix differs from the signed lineage", nil)
	}
	want, err := LedgerPrefixDigest(prefix.rows)
	if err != nil || want != prefix.digest {
		return fail(CodeInvalidLedger, "runner-ledger-catalog-prefix", "ledger prefix digest differs from its exact rows", nil)
	}
	return nil
}

func validRunnerLedgerPrefixShape(prefix runnerLedgerPrefix) bool {
	if prefix.rows == nil || prefix.digest.Validate() != nil {
		return false
	}
	for _, row := range prefix.rows {
		if row.Validate() != nil {
			return false
		}
	}
	want, err := LedgerPrefixDigest(prefix.rows)
	if err != nil || want != prefix.digest {
		return false
	}
	if len(prefix.rows) == 0 {
		return prefix.head == ""
	}
	return prefix.head == prefix.rows[len(prefix.rows)-1].MigrationID
}

func validRunnerLedgerAuthorityResult(result ProjectionResult[AuthorityProjection], subject Digest, phase AuthorityPhase) bool {
	snapshot := result.Metadata.Snapshot
	if snapshot.validate() != nil || snapshot.AuthorityPhase != phase || result.Metadata.validate() != nil ||
		result.Metadata.VerifiedSubjectDigest != subject || result.Metadata.QueryCount != runnerAuthorityProjectionQueryCount ||
		result.Metadata.RowCount == 0 || result.Metadata.TotalBytes == 0 || result.Projection.Validate() != nil ||
		result.Projection.Phase != phase || result.Projection.DatabaseName != snapshot.DatabaseName ||
		result.Projection.SessionUser != snapshot.SessionUser || result.Projection.CurrentUser != snapshot.CurrentUser {
		return false
	}
	digest, err := digestProjectionWrapper(AuthorityProjectionDigestDomain, result.Projection)
	return err == nil && digest == result.Digest
}

func validRunnerLedgerInitialResult(result ProjectionResult[CatalogStateProjection], subject Digest) bool {
	if result.Metadata.validate() != nil || result.Metadata.Snapshot.AuthorityPhase != AuthorityPhaseMigrationRole ||
		result.Metadata.VerifiedSubjectDigest != subject || result.Metadata.QueryCount != 1 || result.Metadata.RowCount == 0 ||
		result.Metadata.TotalBytes == 0 || result.Metadata.Scope == nil || result.Projection.Validate() != nil {
		return false
	}
	var scope ProjectionScope
	if result.Projection.Absent != nil {
		scope = result.Projection.Absent.Scope
	} else if result.Projection.Present != nil {
		scope = result.Projection.Present.Scope
	} else {
		return false
	}
	digest, err := result.Projection.ComputeDigest()
	return err == nil && digest == result.Digest && equalProjectionScopes(*result.Metadata.Scope, scope)
}

func validRunnerLedgerCumulativeResult(result ProjectionResult[CatalogProjection], subject Digest) bool {
	if result.Metadata.validate() != nil || result.Metadata.Snapshot.AuthorityPhase != AuthorityPhaseMigrationRole ||
		result.Metadata.VerifiedSubjectDigest != subject || result.Metadata.QueryCount == 0 || result.Metadata.RowCount == 0 ||
		result.Metadata.TotalBytes == 0 || result.Metadata.Scope == nil || result.Projection.Validate() != nil ||
		result.Metadata.Scope.SchemaHead == nil || *result.Metadata.Scope.SchemaHead != result.Projection.SchemaHead ||
		!equalObjectIdentityClosures(result.Metadata.Scope.DeclaredObjects, result.Projection.Body.DeclaredObjects) {
		return false
	}
	digest, err := digestProjectionWrapper(CatalogProjectionDigestDomain, result.Projection)
	return err == nil && digest == result.Digest
}

func sameRunnerDedicatedSessionIdentity(connected, migrationRole SnapshotMetadata) bool {
	return connected.validate() == nil && migrationRole.validate() == nil &&
		connected.AuthorityPhase == AuthorityPhaseConnectedSession && migrationRole.AuthorityPhase == AuthorityPhaseMigrationRole &&
		connected.PostgresMajor == migrationRole.PostgresMajor && connected.ServerVersionNum == migrationRole.ServerVersionNum &&
		connected.DatabaseName == migrationRole.DatabaseName && connected.SessionUser == migrationRole.SessionUser
}

func cloneRunnerLedgerPrefix(value runnerLedgerPrefix) runnerLedgerPrefix {
	return runnerLedgerPrefix{rows: cloneProjectionValue(value.rows), digest: value.digest, head: value.head}
}

func cloneCatalogStateProjectionResultPointer(value *ProjectionResult[CatalogStateProjection]) *ProjectionResult[CatalogStateProjection] {
	if value == nil {
		return nil
	}
	copy := cloneProjectionValue(*value)
	return &copy
}

func cloneCatalogProjectionResultPointer(value *ProjectionResult[CatalogProjection]) *ProjectionResult[CatalogProjection] {
	if value == nil {
		return nil
	}
	copy := cloneProjectionValue(*value)
	return &copy
}
