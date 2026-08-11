package migration

import (
	"reflect"
	"testing"
	"time"
)

type runnerBindingFixture struct {
	now              time.Time
	expiresAt        time.Time
	decision         VerifiedTrustDecision
	authorityProfile verifiedAuthorityProfileSubject
	authorityBinding AuthorityBinding
	authority        VerifiedAuthorityContract
	recoveryPolicy   verifiedRecoveryPolicySubject
	initialScope     VerifiedSchemaBundleScope
	catalogs         []verifiedExecutableCatalogSubject
}

func TestRunnerProjectionBindingsCanonicalDecisionsAndLineageExclusions(t *testing.T) {
	fixture := newRunnerBindingFixture(t, []string{"000001"})
	bound, err := bindVerifiedRunnerProjectionDecision(fixture.decision, fixture.authorityProfile, fixture.authorityBinding, fixture.authority, fixture.recoveryPolicy, fixture.initialScope, fixture.catalogs, fixture.now)
	if err != nil {
		t.Fatal(err)
	}
	bindings, err := bound.runnerProjectionBindings()
	if err != nil {
		t.Fatal(err)
	}
	if bindings.releaseTrustDecisionDigest == bindings.runnerProjectionDecisionDigest || bindings.runnerProjectionDecisionDigest == bindings.executionLineageDigest {
		t.Fatalf("decision domains collided: release=%s combined=%s lineage=%s", bindings.releaseTrustDecisionDigest, bindings.runnerProjectionDecisionDigest, bindings.executionLineageDigest)
	}

	rotated := newRunnerBindingFixture(t, []string{"000001"})
	rotated.decision.expiresAt = fixture.expiresAt.Add(30 * time.Minute)
	rotated.decision.securityEpoch = fixture.decision.securityEpoch + 1
	rotated.authorityBinding.ExpiresAt = fixture.expiresAt.Add(45 * time.Minute).Format(time.RFC3339)
	rotated.authorityBinding.SecurityEpoch++
	authorityDigest := mustCanonicalDigest(t, rotated.authorityBinding)
	rotated.authority, err = bindVerifiedAuthorityContract(authorityDigest, rotated.authorityBinding.ExpectedProjections, fixture.expiresAt.Add(45*time.Minute), uint64(rotated.authorityBinding.SecurityEpoch))
	if err != nil {
		t.Fatal(err)
	}
	rotated.initialScope, err = bindVerifiedSchemaBundleScope(
		rotated.decision.expectedSchemaBundleDigest, fixture.initialScope.Scope(), fixture.initialScope.BoundPrecondition(),
		fixture.initialScope.DefaultACLOwners(), fixture.initialScope.ObjectCreatorClosure(), rotated.decision.expiresAt, rotated.decision.securityEpoch,
	)
	if err != nil {
		t.Fatal(err)
	}
	for index := range rotated.catalogs {
		raw := mustJSON(t, rotated.catalogs[index].contract)
		rotated.catalogs[index], err = bindVerifiedExecutableCatalogSubject(raw, DigestBytes(raw), fixture.expiresAt.Add(time.Hour), 2, fixture.now)
		if err != nil {
			t.Fatal(err)
		}
	}
	rotatedBound, err := bindVerifiedRunnerProjectionDecision(rotated.decision, rotated.authorityProfile, rotated.authorityBinding, rotated.authority, rotated.recoveryPolicy, rotated.initialScope, rotated.catalogs, rotated.now)
	if err != nil {
		t.Fatal(err)
	}
	rotatedBindings, err := rotatedBound.runnerProjectionBindings()
	if err != nil {
		t.Fatal(err)
	}
	if bindings.executionLineageDigest != rotatedBindings.executionLineageDigest {
		t.Fatalf("expiry/epoch/binding rotation changed stable lineage: %s != %s", bindings.executionLineageDigest, rotatedBindings.executionLineageDigest)
	}
	if bindings.runnerProjectionDecisionDigest == rotatedBindings.runnerProjectionDecisionDigest {
		t.Fatal("authority/catalog rotation did not change the combined projection decision")
	}
}

func TestRunnerProjectionBindingsRejectCatalogOrderDuplicateAndMutation(t *testing.T) {
	fixture := newRunnerBindingFixture(t, []string{"000001", "000002"})
	reordered := []verifiedExecutableCatalogSubject{fixture.catalogs[1], fixture.catalogs[0]}
	if _, err := bindVerifiedRunnerProjectionDecision(fixture.decision, fixture.authorityProfile, fixture.authorityBinding, fixture.authority, fixture.recoveryPolicy, fixture.initialScope, reordered, fixture.now); !IsCode(err, CodeUntrusted) {
		t.Fatalf("reordered catalog subjects were accepted: %v", err)
	}
	duplicated := []verifiedExecutableCatalogSubject{fixture.catalogs[0], fixture.catalogs[0]}
	if _, err := bindVerifiedRunnerProjectionDecision(fixture.decision, fixture.authorityProfile, fixture.authorityBinding, fixture.authority, fixture.recoveryPolicy, fixture.initialScope, duplicated, fixture.now); !IsCode(err, CodeUntrusted) {
		t.Fatalf("duplicate catalog subjects were accepted: %v", err)
	}

	bound, err := bindVerifiedRunnerProjectionDecision(fixture.decision, fixture.authorityProfile, fixture.authorityBinding, fixture.authority, fixture.recoveryPolicy, fixture.initialScope, fixture.catalogs, fixture.now)
	if err != nil {
		t.Fatal(err)
	}
	// Caller aliases are not retained after the bind.
	fixture.catalogs[0].contract.SchemaHead = "999999"
	if _, err := bound.runnerProjectionBindings(); err != nil {
		t.Fatalf("caller catalog mutation reached bound decision: %v", err)
	}

	for name, mutate := range map[string]func(*RunnerProjectionBindings){
		"release digest": func(value *RunnerProjectionBindings) {
			value.releaseTrustDecisionDigest = testDigest("changed-release")
		},
		"release expiry":  func(value *RunnerProjectionBindings) { value.releaseExpiresAt = fixture.now.Add(-time.Second) },
		"authority epoch": func(value *RunnerProjectionBindings) { value.authoritySecurityEpoch++ },
		"authority profile body": func(value *RunnerProjectionBindings) {
			value.verifiedAuthorityProfile.contract.Database.Encoding = "LATIN1"
		},
		"catalog digest": func(value *RunnerProjectionBindings) {
			value.executableCatalogs[0].catalogContractDigest = testDigest("changed-catalog")
		},
		"catalog epoch": func(value *RunnerProjectionBindings) { value.executableCatalogs[0].securityEpoch++ },
		"catalog expiry": func(value *RunnerProjectionBindings) {
			value.executableCatalogs[0].expiresAt = fixture.now.Add(-time.Second)
		},
		"catalog head": func(value *RunnerProjectionBindings) { value.executableCatalogs[0].schemaHead = "999999" },
		"catalog source": func(value *RunnerProjectionBindings) {
			value.executableCatalogs[0].catalogContract.SourceDescriptors[0].Statements[0].Start++
		},
		"wrapper alias": func(value *RunnerProjectionBindings) {
			value.verifiedAuthority.expected.ConnectedSession.EffectiveCreate[MigrationOwnerRole] = false
		},
		"schema scope alias": func(value *RunnerProjectionBindings) {
			value.initialSchemaScope.defaultACLOwners[0] = RuntimeRole
		},
	} {
		mutated, err := bound.runnerProjectionBindings()
		if err != nil {
			t.Fatal(err)
		}
		mutate(&mutated)
		if err := mutated.validateAt(fixture.now); err == nil {
			t.Fatalf("%s mutation was accepted: %v", name, err)
		}
	}

	replaced, err := bound.runnerProjectionBindings()
	if err != nil {
		t.Fatal(err)
	}
	otherAuthority, err := bindVerifiedAuthorityContract(
		testDigest("another-valid-authority-binding"), fixture.authorityBinding.ExpectedProjections,
		fixture.expiresAt, uint64(fixture.authorityBinding.SecurityEpoch),
	)
	if err != nil {
		t.Fatal(err)
	}
	replaced.verifiedAuthority = otherAuthority
	if err := replaced.validateAt(fixture.now); !IsCode(err, CodeUntrusted) {
		t.Fatalf("another individually valid authority wrapper was accepted: %v", err)
	}

	replaced, err = bound.runnerProjectionBindings()
	if err != nil {
		t.Fatal(err)
	}
	otherScope, err := bindVerifiedSchemaBundleScope(
		testDigest("another-valid-schema-bundle"), fixture.initialScope.Scope(), fixture.initialScope.BoundPrecondition(),
		fixture.initialScope.DefaultACLOwners(), fixture.initialScope.ObjectCreatorClosure(), fixture.expiresAt, fixture.decision.securityEpoch,
	)
	if err != nil {
		t.Fatal(err)
	}
	replaced.initialSchemaScope = otherScope
	if err := replaced.validateAt(fixture.now); !IsCode(err, CodeUntrusted) {
		t.Fatalf("another individually valid initial schema scope was accepted: %v", err)
	}

	replaced, err = bound.runnerProjectionBindings()
	if err != nil {
		t.Fatal(err)
	}
	alternateExpected := cloneProjectionValue(fixture.authorityBinding.ExpectedProjections)
	alternateExpected.ConnectedSession.DatabaseName = "cloud_agents_alternate"
	alternateExpected.MigrationRole.DatabaseName = "cloud_agents_alternate"
	alternateExpected.MigrationTransaction.DatabaseName = "cloud_agents_alternate"
	sameSubjectAuthority, err := bindVerifiedAuthorityContract(
		mustCanonicalDigest(t, fixture.authorityBinding), alternateExpected,
		fixture.expiresAt, uint64(fixture.authorityBinding.SecurityEpoch),
	)
	if err != nil {
		t.Fatal(err)
	}
	replaced.verifiedAuthority = sameSubjectAuthority
	if err := replaced.validateAt(fixture.now); !IsCode(err, CodeUntrusted) {
		t.Fatalf("same-subject authority wrapper with another valid expected body was accepted: %v", err)
	}

	replaced, err = bound.runnerProjectionBindings()
	if err != nil {
		t.Fatal(err)
	}
	sameSubjectScope, err := bindVerifiedSchemaBundleScope(
		fixture.decision.expectedSchemaBundleDigest, fixture.initialScope.Scope(), fixture.initialScope.BoundPrecondition(),
		fixture.initialScope.DefaultACLOwners(), []string{MigrationOwnerRole, RuntimeRole}, fixture.expiresAt, fixture.decision.securityEpoch,
	)
	if err != nil {
		t.Fatal(err)
	}
	replaced.initialSchemaScope = sameSubjectScope
	if err := replaced.validateAt(fixture.now); !IsCode(err, CodeUntrusted) {
		t.Fatalf("same-subject initial scope with another valid closure was accepted: %v", err)
	}

	fixture.authorityBinding.ExpectedProjections.ConnectedSession.DatabaseName = "caller_alias_mutation"
	if _, err := bound.runnerProjectionBindings(); err != nil {
		t.Fatalf("caller authority binding alias mutation reached the total binding: %v", err)
	}

	reverified := bound
	owned, err := bound.runnerProjectionBindings()
	if err != nil {
		t.Fatal(err)
	}
	owned.authoritySecurityEpoch++
	reverified.projectionBindings = &owned
	if bound.exactlyMatches(reverified) {
		t.Fatal("decision reverify accepted an exact bindings mismatch")
	}
}

func TestExactStatementPlanBindsArtifactClassificationAndTransition(t *testing.T) {
	fixture := newRunnerBindingFixture(t, []string{"000001"})
	bundle, catalog := exactPlanBundle(t, fixture.decision.expectedSchemaBundleDigest, fixture.initialScope.BoundPrecondition(), fixture.authorityProfile)
	catalogRaw := mustJSON(t, catalog)
	catalogSubject, err := bindVerifiedExecutableCatalogSubject(catalogRaw, DigestBytes(catalogRaw), fixture.expiresAt.Add(time.Hour), 1, fixture.now)
	if err != nil {
		t.Fatal(err)
	}
	fixture.catalogs = []verifiedExecutableCatalogSubject{catalogSubject}
	bound, err := bindVerifiedRunnerProjectionDecision(fixture.decision, fixture.authorityProfile, fixture.authorityBinding, fixture.authority, fixture.recoveryPolicy, fixture.initialScope, fixture.catalogs, fixture.now)
	if err != nil {
		t.Fatal(err)
	}
	bindings, err := bound.runnerProjectionBindings()
	if err != nil {
		t.Fatal(err)
	}
	plans, err := buildExactStatementPlans(bundle, bindings, fixture.now)
	if err != nil {
		t.Fatal(err)
	}
	if len(plans) != 1 || plans[0].MigrationID != "000001" || plans[0].StatementIndex != 0 || !plans[0].exact {
		t.Fatalf("unexpected exact plan: %+v", plans)
	}
	wantSQL := append([]byte(nil), bundle.Files[bundle.Manifest.SchemaBundle.Migrations[0].SQLArtifact.Path]...)
	gotSQL, err := plans[0].exactSQLBytes()
	if err != nil || !reflect.DeepEqual(gotSQL, wantSQL) {
		t.Fatalf("exact SQL bytes differ: got=%q want=%q err=%v", gotSQL, wantSQL, err)
	}
	// The plan owns its bytes even if the RuntimeBundle.Files map is mutated.
	bundle.Files[bundle.Manifest.SchemaBundle.Migrations[0].SQLArtifact.Path][0] ^= 0x01
	if err := plans[0].validateExact(); err != nil {
		t.Fatalf("runtime alias mutation reached frozen plan: %v", err)
	}
	if _, err := buildExactStatementPlans(bundle, bindings, fixture.now); !IsCode(err, CodeInvalidArtifact) {
		t.Fatalf("mutated runtime SQL artifact was accepted: %v", err)
	}

	for name, mutate := range map[string]func(*StatementPlan){
		"offset":          func(plan *StatementPlan) { plan.EndOffset++ },
		"artifact digest": func(plan *StatementPlan) { plan.SQLArtifactSHA256 = testDigest("changed-sql-artifact") },
		"classification": func(plan *StatementPlan) {
			plan.Classification.TargetIdentity = "table:unquoted:cloud_agents/unquoted:other"
		},
		"transition":  func(plan *StatementPlan) { plan.ExpectedTransition.AuthorityRelation = "changed" },
		"owned bytes": func(plan *StatementPlan) { plan.sqlBytes[0] ^= 0x01 },
	} {
		mutated := plans[0]
		mutated.sqlBytes = append([]byte(nil), plans[0].sqlBytes...)
		mutate(&mutated)
		if err := mutated.validateExact(); err == nil {
			t.Fatalf("%s drift was accepted", name)
		}
	}

	for name, mutate := range map[string]func(*SQLStatementDescriptor){
		"descriptor offset": func(descriptor *SQLStatementDescriptor) { descriptor.Start++ },
		"unknown target": func(descriptor *SQLStatementDescriptor) {
			descriptor.Classification.TargetIdentity = "table:unquoted:cloud_agents/unquoted:unknown"
		},
	} {
		t.Run(name, func(t *testing.T) {
			localFixture := newRunnerBindingFixture(t, []string{"000001"})
			localBundle, localCatalog := exactPlanBundle(t, localFixture.decision.expectedSchemaBundleDigest, localFixture.initialScope.BoundPrecondition(), localFixture.authorityProfile)
			mutate(&localCatalog.SourceDescriptors[0].Statements[0])
			localSubject := installExactCatalog(t, localBundle, localCatalog, localFixture.expiresAt.Add(time.Hour), localFixture.now)
			localBound, err := bindVerifiedRunnerProjectionDecision(localFixture.decision, localFixture.authorityProfile, localFixture.authorityBinding, localFixture.authority, localFixture.recoveryPolicy, localFixture.initialScope, []verifiedExecutableCatalogSubject{localSubject}, localFixture.now)
			if err != nil {
				t.Fatal(err)
			}
			localBindings, err := localBound.runnerProjectionBindings()
			if err != nil {
				t.Fatal(err)
			}
			if _, err := buildExactStatementPlans(localBundle, localBindings, localFixture.now); !IsCode(err, CodeInvalidSQL) {
				t.Fatalf("signed structural drift was accepted: %v", err)
			}
		})
	}
}

func TestCheckedInMutableCatalogSubjectsFailClosed(t *testing.T) {
	_, manifest := buildCheckedInRuntimeTar(t)
	for _, entry := range manifest.SchemaBundle.Migrations {
		raw := mustRead(t, modulePathForRuntimeArtifact(t, entry.CatalogContract.Path))
		if _, err := bindVerifiedExecutableCatalogSubject(raw, entry.CatalogContract.SHA256, time.Now().Add(time.Hour), 1, time.Now()); !IsCode(err, CodeProjectionNotImplemented) {
			t.Fatalf("checked-in mutable catalog %s did not fail closed: %v", entry.ID, err)
		}
	}
}

func TestVerifiedAuthorityProfileTotalBindingFaults(t *testing.T) {
	fixture := newRunnerBindingFixture(t, []string{"000001"})
	profileRaw := mustJSON(t, fixture.authorityProfile.contract)
	if _, err := bindVerifiedAuthorityProfileSubject(profileRaw, testDigest("wrong-profile")); !IsCode(err, CodeUntrusted) {
		t.Fatalf("authority profile digest fault was accepted: %v", err)
	}
	wrongBinding := fixture.authorityBinding
	wrongBinding.AuthorityProfileDigest = testDigest("other-authority-profile")
	if _, err := bindVerifiedRunnerProjectionDecision(fixture.decision, fixture.authorityProfile, wrongBinding, fixture.authority, fixture.recoveryPolicy, fixture.initialScope, fixture.catalogs, fixture.now); !IsCode(err, CodeUntrusted) {
		t.Fatalf("authority binding/profile digest mismatch was accepted: %v", err)
	}
	mutated := fixture.authorityProfile.ownedCopy()
	mutated.raw[0] ^= 0x01
	if err := mutated.validate(); err == nil {
		t.Fatal("authority profile owned bytes mutation was accepted")
	}
	mutated = fixture.authorityProfile.ownedCopy()
	mutated.contract.Database.Encoding = "LATIN1"
	if err := mutated.validate(); err == nil {
		t.Fatal("authority profile body mutation was accepted")
	}
	mutated = fixture.authorityProfile.ownedCopy()
	mutated.contract.PublicationStatus = "UNPUBLISHED_BOOTSTRAP_MUTABLE"
	mutated.contract.RuntimeIntrospectionStatus = "NOT_IMPLEMENTED"
	if err := mutated.validate(); !IsCode(err, CodeProjectionNotImplemented) {
		t.Fatalf("authority profile status mutation was accepted: %v", err)
	}

	_, manifest := buildCheckedInRuntimeTar(t)
	currentRecord := manifest.ExecutionPolicy.AuthorityContract
	currentRaw := mustRead(t, modulePathForRuntimeArtifact(t, currentRecord.Path))
	if _, err := bindVerifiedAuthorityProfileSubject(currentRaw, currentRecord.SHA256); !IsCode(err, CodeProjectionNotImplemented) {
		t.Fatalf("checked-in mutable authority profile was accepted: %v", err)
	}
}

func TestExactPlanRejectsAuthorityProfileDescriptorAndRuntimeFaults(t *testing.T) {
	fixture := newRunnerBindingFixture(t, []string{"000001"})
	bundle, catalog := exactPlanBundle(t, fixture.decision.expectedSchemaBundleDigest, fixture.initialScope.BoundPrecondition(), fixture.authorityProfile)
	catalogRaw := mustJSON(t, catalog)
	catalogSubject, err := bindVerifiedExecutableCatalogSubject(catalogRaw, DigestBytes(catalogRaw), fixture.expiresAt.Add(time.Hour), 1, fixture.now)
	if err != nil {
		t.Fatal(err)
	}
	bound, err := bindVerifiedRunnerProjectionDecision(fixture.decision, fixture.authorityProfile, fixture.authorityBinding, fixture.authority, fixture.recoveryPolicy, fixture.initialScope, []verifiedExecutableCatalogSubject{catalogSubject}, fixture.now)
	if err != nil {
		t.Fatal(err)
	}
	bindings, err := bound.runnerProjectionBindings()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := buildExactStatementPlans(bundle, bindings, fixture.now); err != nil {
		t.Fatalf("exact authority profile baseline failed: %v", err)
	}

	for name, mutate := range map[string]func(*RuntimeBundle){
		"manifest digest": func(value *RuntimeBundle) {
			value.Manifest.ExecutionPolicy.AuthorityContract.SHA256 = testDigest("wrong-authority-descriptor")
		},
		"runtime descriptor": func(value *RuntimeBundle) {
			for index := range value.Manifest.RuntimeArtifacts {
				if value.Manifest.RuntimeArtifacts[index].Path == value.Manifest.ExecutionPolicy.AuthorityContract.Path {
					value.Manifest.RuntimeArtifacts[index].SizeBytes++
				}
			}
		},
		"runtime bytes": func(value *RuntimeBundle) {
			path := value.Manifest.ExecutionPolicy.AuthorityContract.Path
			value.Files[path][0] ^= 0x01
		},
	} {
		t.Run(name, func(t *testing.T) {
			owned := cloneRuntimeBundleForAuthorityFault(bundle)
			mutate(owned)
			if _, err := buildExactStatementPlans(owned, bindings, fixture.now); err == nil {
				t.Fatal("authority profile fault was accepted")
			}
		})
	}
}

func newRunnerBindingFixture(t *testing.T, heads []string) runnerBindingFixture {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Second)
	expiresAt := now.Add(time.Hour)
	decision := VerifiedTrustDecision{
		verified: true, expectedSchemaBundleDigest: testDigest("schema-bundle"), expectedBootstrapBundleDigest: testDigest("bootstrap-bundle"),
		expectedManifestDigest: testDigest("manifest"), expectedOuterArtifactDigest: testDigest("outer"), expectedRunnerReleaseDigest: testDigest("runner"),
		repositoryIdentity: "hxp0618/cloud-agents", releaseIdentity: "v0.0.0-test", expiresAt: expiresAt, securityEpoch: 1,
	}
	authorityProfileRaw := mustJSON(t, executableAuthorityProfile())
	authorityProfile, err := bindVerifiedAuthorityProfileSubject(authorityProfileRaw, DigestBytes(authorityProfileRaw))
	if err != nil {
		t.Fatal(err)
	}
	expected := AuthorityExpectedProjections{
		ConnectedSession:     minimalAuthorityProjection(AuthorityPhaseConnectedSession),
		MigrationRole:        minimalAuthorityProjection(AuthorityPhaseMigrationRole),
		MigrationTransaction: minimalAuthorityProjection(AuthorityPhaseMigrationTransaction),
	}
	authorityBinding := AuthorityBinding{
		FormatVersion: AuthorityBindingFormat, AuthorityProfileDigest: authorityProfile.artifactDigest, DeploymentID: "deployment_1",
		IssuedAt: now.Add(-time.Minute).Format(time.RFC3339), ExpiresAt: expiresAt.Format(time.RFC3339), SecurityEpoch: 1, ExpectedProjections: expected,
	}
	authority, err := bindVerifiedAuthorityContract(mustCanonicalDigest(t, authorityBinding), expected, expiresAt, 1)
	if err != nil {
		t.Fatal(err)
	}
	recoveryPolicy, err := bindVerifiedRecoveryPolicySubject(recoveryPolicySignedSubject{
		Domain: recoveryPolicySubjectDomain, IssuerKeyIdentityDigest: testDigest("recovery-policy-issuer"),
		ExpiresAt: expiresAt.Format(time.RFC3339), SecurityEpoch: 1, MinimumOldSecurityEpoch: 1,
		OldRevocationPolicyDigest: testDigest("old-revocation-policy"), OldDecisionAuthorizations: []oldDecisionAuthorization{},
	}, 1, now)
	if err != nil {
		t.Fatal(err)
	}
	migrationID := "000001"
	scope := ProjectionScope{ScopeKind: "predecessor", MigrationID: &migrationID, DeclaredObjects: []ObjectIdentityProjection{}}
	condition := CatalogPrecondition{AcceptedStates: []CatalogStateProjection{
		{Absent: &SchemaAbsentProjection{State: "schema_absent", Scope: scope, Schema: "cloud_agents"}},
		{Present: &SchemaPresentProjection{State: "schema_present", Scope: scope, Body: minimalCatalogBody()}},
	}}
	initialScope, err := bindVerifiedSchemaBundleScope(decision.expectedSchemaBundleDigest, scope, condition, []string{MigrationOwnerRole}, []string{MigrationOwnerRole}, expiresAt, 1)
	if err != nil {
		t.Fatal(err)
	}
	catalogs := make([]verifiedExecutableCatalogSubject, len(heads))
	for index, head := range heads {
		contract := executableCatalogForHead(t, head)
		raw := mustJSON(t, contract)
		catalogs[index], err = bindVerifiedExecutableCatalogSubject(raw, DigestBytes(raw), expiresAt.Add(time.Hour), 1, now)
		if err != nil {
			t.Fatal(err)
		}
	}
	return runnerBindingFixture{now: now, expiresAt: expiresAt, decision: decision, authorityProfile: authorityProfile, authorityBinding: authorityBinding, authority: authority, recoveryPolicy: recoveryPolicy, initialScope: initialScope, catalogs: catalogs}
}

func executableAuthorityProfile() AuthorityContract {
	return AuthorityContract{
		FormatVersion: "cloud-agents-platform-authority-contract/v1", ContractKind: "database_role_authority",
		PublicationStatus: "PUBLISHED_IMMUTABLE", RuntimeIntrospectionStatus: "IMPLEMENTED",
		Database:                 AuthorityDatabaseContract{Encoding: "UTF8", LocaleProvider: "libc", Datcollate: "C", Datctype: "C"},
		GroupRoles:               []string{MigrationOwnerRole, RuntimeRole, BootstrapAdminRole},
		RequiredProjectionFields: authorityProjectionFieldClosure(), RequiredBindingFields: authorityBindingFieldClosure(),
	}
}

func executableCatalogForHead(t *testing.T, head string) CatalogContract {
	t.Helper()
	contract := minimalCatalogContract(t, "PUBLISHED_IMMUTABLE", "IMPLEMENTED")
	oldMigrationID := contract.SourceDescriptors[0].MigrationID
	contract.SchemaHead = head
	contract.ExpectedProjection.SchemaHead = head
	contract.SourceDescriptors[0].MigrationID = head
	for index := range contract.SourceDescriptors[0].Statements {
		transition := &contract.SourceDescriptors[0].Statements[index].ExpectedTransition
		if transition.CatalogBefore.Scope.MigrationID != nil && *transition.CatalogBefore.Scope.MigrationID == oldMigrationID {
			transition.CatalogBefore.Scope.MigrationID = stringPointer(head)
		}
		if transition.CatalogAfter.Scope.MigrationID != nil && *transition.CatalogAfter.Scope.MigrationID == oldMigrationID {
			transition.CatalogAfter.Scope.MigrationID = stringPointer(head)
		}
		if transition.CatalogAfter.Scope.SchemaHead != nil {
			transition.CatalogAfter.Scope.SchemaHead = stringPointer(head)
			final := CatalogStateProjection{Present: &SchemaPresentProjection{State: "schema_present", Scope: transition.CatalogAfter.Scope, Body: contract.ExpectedProjection.Body}}
			digest, err := final.ComputeDigest()
			if err != nil {
				t.Fatal(err)
			}
			transition.CatalogAfter.Digest = digest
		}
	}
	return contract
}

func exactPlanBundle(t *testing.T, schemaBundleDigest Digest, predecessor CatalogPrecondition, authorityProfile verifiedAuthorityProfileSubject) (*RuntimeBundle, CatalogContract) {
	t.Helper()
	sqlRaw := []byte("CREATE TABLE cloud_agents.t (id text);")
	statements, err := SplitPostgreSQLStatements(sqlRaw)
	if err != nil || len(statements) != 1 {
		t.Fatalf("split exact SQL: statements=%d err=%v", len(statements), err)
	}
	contract := executableCatalogForHead(t, "000001")
	descriptor := &contract.SourceDescriptors[0].Statements[0]
	descriptor.Index = 0
	descriptor.Start = uint64(statements[0].Start)
	descriptor.End = uint64(statements[0].End)
	descriptor.SHA256 = statements[0].SHA256
	descriptor.Classification = SQLClassificationDescriptor{
		Profile: "postgresql-ddl-v1", Command: "CREATE", ObjectKind: "TABLE", TargetIdentity: "table:unquoted:cloud_agents/unquoted:t",
	}
	contract.SourceDescriptors[0].SQLSHA256 = DigestBytes(sqlRaw)
	catalogRaw := mustJSON(t, contract)
	sqlRecord := ArtifactRecord{Path: "services/control-plane/migrations/000001_test.sql", Mode: "100644", SizeBytes: uint64(len(sqlRaw)), SHA256: DigestBytes(sqlRaw)}
	catalogRecord := ArtifactRecord{Path: "services/control-plane/migrations/catalog/schema-000001.json", Mode: "100644", SizeBytes: uint64(len(catalogRaw)), SHA256: DigestBytes(catalogRaw)}
	authorityRaw := append([]byte(nil), authorityProfile.raw...)
	authorityRecord := ArtifactRecord{Path: "services/control-plane/migrations/catalog/authority-v1.json", Mode: "100644", SizeBytes: uint64(len(authorityRaw)), SHA256: DigestBytes(authorityRaw)}
	entry := MigrationEntry{ID: "000001", SQLArtifact: sqlRecord, CatalogContract: catalogRecord, PredecessorCatalogContract: cloneProjectionValue(predecessor)}
	manifest := &Manifest{
		SchemaBundleDigest: schemaBundleDigest,
		ExecutionPolicy:    ExecutionPolicy{AuthorityContract: authorityRecord},
		SchemaBundle: SchemaBundle{
			SchemaHead: "000001", ProjectionScopeAuthority: ProjectionScopeAuthority{
				DefaultACLOwners: []string{MigrationOwnerRole}, ObjectCreatorClosure: []string{MigrationOwnerRole},
			},
			Migrations: []MigrationEntry{entry},
		},
		RuntimeArtifacts: []ArtifactRecord{sqlRecord, authorityRecord, catalogRecord},
	}
	files := map[string][]byte{
		sqlRecord.Path: append([]byte(nil), sqlRaw...), authorityRecord.Path: append([]byte(nil), authorityRaw...), catalogRecord.Path: append([]byte(nil), catalogRaw...),
	}
	return &RuntimeBundle{Manifest: manifest, Files: files}, contract
}

func installExactCatalog(t *testing.T, bundle *RuntimeBundle, contract CatalogContract, expiresAt, now time.Time) verifiedExecutableCatalogSubject {
	t.Helper()
	raw := mustJSON(t, contract)
	record := &bundle.Manifest.SchemaBundle.Migrations[0].CatalogContract
	record.SizeBytes = uint64(len(raw))
	record.SHA256 = DigestBytes(raw)
	for index := range bundle.Manifest.RuntimeArtifacts {
		if bundle.Manifest.RuntimeArtifacts[index].Path == record.Path {
			bundle.Manifest.RuntimeArtifacts[index] = *record
		}
	}
	bundle.Files[record.Path] = append([]byte(nil), raw...)
	subject, err := bindVerifiedExecutableCatalogSubject(raw, record.SHA256, expiresAt, 1, now)
	if err != nil {
		t.Fatal(err)
	}
	return subject
}

func cloneRuntimeBundleForAuthorityFault(bundle *RuntimeBundle) *RuntimeBundle {
	manifest := cloneProjectionValue(*bundle.Manifest)
	owned := &RuntimeBundle{Manifest: &manifest, Files: make(map[string][]byte, len(bundle.Files))}
	for path, raw := range bundle.Files {
		owned.Files[path] = append([]byte(nil), raw...)
	}
	return owned
}

func mustCanonicalDigest(t *testing.T, value any) Digest {
	t.Helper()
	digest, err := digestRunnerProjectionCanonical(value)
	if err != nil {
		t.Fatal(err)
	}
	return digest
}

func testDigest(label string) Digest { return DigestBytes([]byte(label)) }

func stringPointer(value string) *string { return &value }

func modulePathForRuntimeArtifact(t *testing.T, runtimePath string) string {
	t.Helper()
	const prefix = "services/control-plane/"
	if len(runtimePath) <= len(prefix) || runtimePath[:len(prefix)] != prefix {
		t.Fatalf("unexpected runtime path %q", runtimePath)
	}
	return moduleRoot(t) + "/" + runtimePath[len(prefix):]
}
