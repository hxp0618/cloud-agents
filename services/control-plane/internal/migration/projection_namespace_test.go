package migration

import (
	"context"
	"testing"
	"time"
)

func TestPGNamespaceProjectsExplicitEffectiveAndGlobalSchemaDefaultACL(t *testing.T) {
	major := uint16(17)
	migrationID := "000001"
	scope := ProjectionScope{ScopeKind: "predecessor", MigrationID: &migrationID, DeclaredObjects: []ObjectIdentityProjection{}}
	snapshot := &pgTestSnapshot{metadata: pgTestMetadata(major), queries: map[projectionQueryID][]pgTestQuery{
		projectionQueryCapability: {{rows: [][]any{pgCapabilityRow(major)}}},
		projectionQueryNamespace: {{rows: [][]any{
			pgNamespaceRow("schema", pgString(projectionTargetSchema), pgString(MigrationOwnerRole), true, nil, nil, nil, nil, nil, nil, nil),
			pgNamespaceRow("security_label", pgString(projectionTargetSchema), pgString(MigrationOwnerRole), false, nil, nil, nil, nil, pgString("selinux"), pgString("system_u"), nil),
		}}},
		projectionQueryNamespaceCreators: {{rows: [][]any{{MigrationOwnerRole}}}},
		projectionQueryDefaultACLs: {{rows: [][]any{
			pgDefaultACLRow("41", MigrationOwnerRole, nil, "r", pgString(MigrationOwnerRole), pgString(RuntimeRole), pgString("SELECT"), pgBool(false), true),
			pgDefaultACLRow("42", MigrationOwnerRole, pgString(projectionTargetSchema), "r", pgString(MigrationOwnerRole), pgString(RuntimeRole), pgString("INSERT"), pgBool(false), true),
		}}},
	}}
	projector, err := NewPGProjector(context.Background(), snapshot)
	if err != nil {
		t.Fatal(err)
	}
	projection, err := projector.readNamespace(context.Background(), snapshot, scope, []string{MigrationOwnerRole}, []string{MigrationOwnerRole})
	if err != nil {
		t.Fatal(err)
	}
	if projection.Absent || projection.Body.Schema.Owner != MigrationOwnerRole {
		t.Fatalf("unexpected namespace state: %+v", projection)
	}
	if projection.Body.Schema.ExplicitACL.CatalogValue != "null" || len(projection.Body.Schema.EffectiveACL) != 1 || projection.Body.Schema.EffectiveACL[0].Origin != "owner_implicit" {
		t.Fatalf("explicit/effective ACL distinction lost: %+v", projection.Body.Schema)
	}
	if len(projection.Body.DefaultACL) != 2 || projection.Body.DefaultACL[0].Schema != nil || projection.Body.DefaultACL[1].Schema == nil || projection.Body.DefaultACL[0].ObjectKind != "table" {
		t.Fatalf("global/schema default ACL rows were folded or misordered: %+v", projection.Body.DefaultACL)
	}
}

func TestPGNamespaceAbsentAndUnknownObject(t *testing.T) {
	major := uint16(15)
	migrationID := "000001"
	scope := ProjectionScope{ScopeKind: "predecessor", MigrationID: &migrationID, DeclaredObjects: []ObjectIdentityProjection{}}
	projector := &PGProjector{major: major, capabilities: pgProjectionCapabilities{Major: major}, normalizer: pg15Normalizer{}}
	absentSnapshot := &pgTestSnapshot{metadata: pgTestMetadata(major), queries: map[projectionQueryID][]pgTestQuery{
		projectionQueryNamespace: {{rows: [][]any{pgNamespaceRow("absent", pgString(projectionTargetSchema), nil, true, nil, nil, nil, nil, nil, nil, nil)}}},
	}}
	absent, err := projector.readNamespace(context.Background(), absentSnapshot, scope, []string{MigrationOwnerRole}, []string{MigrationOwnerRole})
	if err != nil || !absent.Absent {
		t.Fatalf("schema absence projection failed: %+v err=%v", absent, err)
	}
	for _, kind := range []string{"relation", "function", "type", "extension", "collation", "operator", "opclass", "opfamily", "conversion", "ts_config", "ts_dict", "ts_parser", "ts_template", "statistic_ext"} {
		unknownSnapshot := &pgTestSnapshot{metadata: pgTestMetadata(major), queries: map[projectionQueryID][]pgTestQuery{
			projectionQueryNamespace: {{rows: [][]any{pgNamespaceRow(kind, pgString(projectionTargetSchema), pgString(MigrationOwnerRole), false, nil, nil, nil, nil, pgString("detected"), nil, nil)}}},
		}}
		if _, err := projector.readNamespace(context.Background(), unknownSnapshot, scope, []string{MigrationOwnerRole}, []string{MigrationOwnerRole}); !IsCode(err, CodeProjectionUnknownObject) {
			t.Fatalf("A2.1b object kind %s was not rejected: %v", kind, err)
		}
	}
}

func TestPGNamespaceCreatorClosureRequiresExactEffectiveCreateSet(t *testing.T) {
	projector := &PGProjector{major: 17, normalizer: pg17Normalizer{}}
	for name, rows := range map[string][][]any{
		"missing": {},
		"extra":   {{MigrationOwnerRole}, {"unexpected_creator"}},
	} {
		t.Run(name, func(t *testing.T) {
			snapshot := &pgTestSnapshot{queries: map[projectionQueryID][]pgTestQuery{
				projectionQueryNamespaceCreators: {{rows: rows}},
			}}
			if err := projector.reconcileObjectCreators(context.Background(), snapshot, []string{MigrationOwnerRole}); !IsCode(err, CodeAuthorityDrift) {
				t.Fatalf("creator closure drift was accepted: %v", err)
			}
		})
	}
}

func TestPGDefaultACLFiltersUnrelatedGlobalAndRejectsAuthorityDrift(t *testing.T) {
	projector := &PGProjector{major: 16, normalizer: pg16Normalizer{}}
	unrelated := &pgTestSnapshot{queries: map[projectionQueryID][]pgTestQuery{
		projectionQueryDefaultACLs: {{rows: [][]any{pgDefaultACLRow("5", "unrelated", nil, "r", pgString("unrelated"), pgString("PUBLIC"), pgString("SELECT"), pgBool(false), false)}}},
	}}
	rows, err := projector.readDefaultACL(context.Background(), unrelated, []string{MigrationOwnerRole}, []string{MigrationOwnerRole})
	if err != nil || len(rows) != 0 {
		t.Fatalf("unrelated global default ACL was not filtered: rows=%+v err=%v", rows, err)
	}
	drift := &pgTestSnapshot{queries: map[projectionQueryID][]pgTestQuery{
		projectionQueryDefaultACLs: {{rows: [][]any{pgDefaultACLRow("5", "unrelated", nil, "r", pgString("unrelated"), pgString("PUBLIC"), pgString("SELECT"), pgBool(false), true)}}},
	}}
	if _, err := projector.readDefaultACL(context.Background(), drift, []string{MigrationOwnerRole}, []string{MigrationOwnerRole}); !IsCode(err, CodeAuthorityDrift) {
		t.Fatalf("effective CREATE drift was not prioritized: %v", err)
	}
}

func TestPGDefaultACLInvalidScopeAndACLBounds(t *testing.T) {
	projector := &PGProjector{major: 17, normalizer: pg17Normalizer{}}
	invalid := &pgTestSnapshot{queries: map[projectionQueryID][]pgTestQuery{
		projectionQueryDefaultACLs: {{rows: [][]any{pgDefaultACLRow("6", MigrationOwnerRole, pgString(projectionTargetSchema), "n", nil, nil, nil, nil, true)}}},
	}}
	if _, err := projector.readDefaultACL(context.Background(), invalid, []string{MigrationOwnerRole}, []string{MigrationOwnerRole}); !IsCode(err, CodeProjectionInvalidScope) {
		t.Fatalf("schema-scoped schema default ACL was accepted: %v", err)
	}
	acl := newACLAccumulator(17, false, "catalog_explicit")
	if err := acl.add("acl", "owner", "runtime", "USAGE", false, schemaPrivileges); err != nil {
		t.Fatal(err)
	}
	if err := acl.add("acl", "owner", "runtime", "USAGE", false, schemaPrivileges); !IsCode(err, CodeProjectionUnknownObject) {
		t.Fatalf("duplicate ACL row was accepted: %v", err)
	}
	nullACL := newACLAccumulator(17, true, "catalog_explicit")
	if err := nullACL.add("acl", "owner", "runtime", "USAGE", false, schemaPrivileges); !IsCode(err, CodeProjectionCatalogQueryFailed) {
		t.Fatalf("null ACL expanded row was accepted: %v", err)
	}
}

func TestPGACLUsesFixedPrivilegeRankAndUnknownPrincipalsFailClosed(t *testing.T) {
	acl := newACLAccumulator(17, false, "catalog_explicit")
	for _, privilege := range []string{"TEMPORARY", "CONNECT", "CREATE"} {
		if err := acl.add("acl", "owner", "runtime", privilege, privilege == "CREATE", databasePrivileges); err != nil {
			t.Fatal(err)
		}
	}
	entry := acl.projection().Entries[0]
	if got := entry.Privileges; len(got) != 3 || got[0] != "CONNECT" || got[1] != "CREATE" || got[2] != "TEMPORARY" {
		t.Fatalf("privileges were not sorted by the ADR rank: %v", got)
	}
	if got := entry.Grantable; len(got) != 1 || got[0] != "CREATE" {
		t.Fatalf("grantable privileges were not rank-normalized: %v", got)
	}

	projector := &PGProjector{major: 17, normalizer: pg17Normalizer{}}
	database := &pgTestSnapshot{queries: map[projectionQueryID][]pgTestQuery{
		projectionQueryDatabaseAuthority: {{rows: [][]any{
			pgDatabaseRow("database", nil, nil, nil, nil, nil, nil, nil),
			pgDatabaseRow("acl", nil, pgString("runtime"), pgString("CONNECT"), pgBool(false), nil, nil, nil),
		}}},
	}}
	if _, err := projector.readDatabaseAuthority(context.Background(), database, []string{"runtime"}); !IsCode(err, CodeProjectionUnknownObject) {
		t.Fatalf("unknown database ACL grantor did not map to UNKNOWN_OBJECT: %v", err)
	}
	defaultACL := &pgTestSnapshot{queries: map[projectionQueryID][]pgTestQuery{
		projectionQueryDefaultACLs: {{rows: [][]any{pgDefaultACLRow("9", MigrationOwnerRole, nil, "r", pgString(MigrationOwnerRole), nil, pgString("SELECT"), pgBool(false), true)}}},
	}}
	if _, err := projector.readDefaultACL(context.Background(), defaultACL, []string{MigrationOwnerRole}, []string{MigrationOwnerRole}); !IsCode(err, CodeProjectionUnknownObject) {
		t.Fatalf("unknown default ACL grantee did not map to UNKNOWN_OBJECT: %v", err)
	}
	migrationID := "000001"
	scope := ProjectionScope{ScopeKind: "predecessor", MigrationID: &migrationID, DeclaredObjects: []ObjectIdentityProjection{}}
	namespace := &pgTestSnapshot{queries: map[projectionQueryID][]pgTestQuery{
		projectionQueryNamespace: {{rows: [][]any{
			pgNamespaceRow("schema", pgString(projectionTargetSchema), pgString(MigrationOwnerRole), false, nil, nil, nil, nil, nil, nil, nil),
			pgNamespaceRow("schema_acl", pgString(projectionTargetSchema), pgString(MigrationOwnerRole), false, nil, pgString("runtime"), pgString("USAGE"), pgBool(false), nil, nil, nil),
		}}},
	}}
	if _, err := projector.readNamespace(context.Background(), namespace, scope, []string{MigrationOwnerRole}, []string{MigrationOwnerRole}); !IsCode(err, CodeProjectionUnknownObject) {
		t.Fatalf("unknown schema ACL grantor did not map to UNKNOWN_OBJECT: %v", err)
	}
}

func TestPGNamespaceSubdigestsUseClosedIndependentDomains(t *testing.T) {
	effective := []ACLProjection{{
		Grantor: MigrationOwnerRole, Grantee: MigrationOwnerRole,
		Privileges: []string{"CREATE", "USAGE"}, Grantable: []string{"CREATE", "USAGE"}, Origin: "owner_implicit",
	}}
	schema := SchemaProjection{
		Name: projectionTargetSchema, Owner: MigrationOwnerRole,
		ExplicitACL:  ACLSetProjection{CatalogValue: "null", Entries: []ACLProjection{}},
		EffectiveACL: effective,
	}
	explicitDigest, err := computeSchemaExplicitACLDigest(schema)
	if err != nil {
		t.Fatal(err)
	}
	effectiveDigest, err := computeSchemaEffectiveACLDigest(schema)
	if err != nil {
		t.Fatal(err)
	}
	mutated := schema
	mutated.ExplicitACL = ACLSetProjection{CatalogValue: "explicit", Entries: []ACLProjection{{
		Grantor: MigrationOwnerRole, Grantee: RuntimeRole,
		Privileges: []string{"USAGE"}, Grantable: []string{}, Origin: "catalog_explicit",
	}}}
	mutatedExplicit, err := computeSchemaExplicitACLDigest(mutated)
	if err != nil {
		t.Fatal(err)
	}
	mutatedEffective, err := computeSchemaEffectiveACLDigest(mutated)
	if err != nil {
		t.Fatal(err)
	}
	if explicitDigest == mutatedExplicit || effectiveDigest != mutatedEffective {
		t.Fatalf("subdigest exclusion boundary failed: explicit %s/%s effective %s/%s", explicitDigest, mutatedExplicit, effectiveDigest, mutatedEffective)
	}
	rows := []DefaultACLProjection{{
		Owner: MigrationOwnerRole, Schema: nil, ObjectKind: "table",
		ACL: ACLSetProjection{CatalogValue: "explicit", Entries: []ACLProjection{{
			Grantor: MigrationOwnerRole, Grantee: RuntimeRole,
			Privileges: []string{"SELECT"}, Grantable: []string{}, Origin: "default_acl_catalog",
		}}},
	}}
	defaultDigest, err := computeDefaultACLDigest(rows)
	if err != nil || defaultDigest == explicitDigest || defaultDigest == effectiveDigest {
		t.Fatalf("default ACL digest domain collided or failed: %s err=%v", defaultDigest, err)
	}
}

func TestPGProjectPreconditionReturnsTypedAbsentAndBoundedMetadata(t *testing.T) {
	major := uint16(15)
	metadata := pgTestMetadata(major)
	migrationID := "000001"
	scope := ProjectionScope{ScopeKind: "predecessor", MigrationID: &migrationID, DeclaredObjects: []ObjectIdentityProjection{}}
	body := pgEmptyNamespaceBody()
	absent := CatalogStateProjection{Absent: &SchemaAbsentProjection{State: "schema_absent", Scope: scope, Schema: projectionTargetSchema}}
	present := CatalogStateProjection{Present: &SchemaPresentProjection{State: "schema_present", Scope: scope, Body: body}}
	snapshot := &pgTestSnapshot{metadata: metadata, queries: map[projectionQueryID][]pgTestQuery{
		projectionQueryNamespace: {{rows: [][]any{pgNamespaceRow("absent", pgString(projectionTargetSchema), nil, true, nil, nil, nil, nil, nil, nil, nil)}}},
	}}
	projector := &PGProjector{major: major, capabilities: pgProjectionCapabilities{Major: major, ServerVersionNum: metadata.ServerVersionNum}, normalizer: pg15Normalizer{}}
	condition := CatalogPrecondition{AcceptedStates: []CatalogStateProjection{absent, present}}
	verified, err := bindVerifiedSchemaBundleScope(projectionTestDigest, scope, condition, []string{MigrationOwnerRole}, []string{MigrationOwnerRole}, time.Now().Add(time.Hour), 1)
	if err != nil {
		t.Fatal(err)
	}
	result, err := projector.ProjectPrecondition(context.Background(), snapshot, verified, condition)
	if err != nil {
		t.Fatal(err)
	}
	if result.Projection.Absent == nil || result.Metadata.Scope == nil || result.Metadata.QueryCount != 1 || result.Metadata.ProjectionKind != ProjectionKindCatalogState {
		t.Fatalf("precondition result ABI differs: %+v", result)
	}
	wantDigest, err := absent.ComputeDigest()
	if err != nil || result.Digest != wantDigest {
		t.Fatalf("precondition digest differs: got=%s want=%s err=%v", result.Digest, wantDigest, err)
	}
}

func TestPGProjectPreconditionRejectsCallerOverlayAndExpiredDecision(t *testing.T) {
	major := uint16(15)
	metadata := pgTestMetadata(major)
	migrationID := "000001"
	scope := ProjectionScope{ScopeKind: "predecessor", MigrationID: &migrationID, DeclaredObjects: []ObjectIdentityProjection{}}
	absent := CatalogStateProjection{Absent: &SchemaAbsentProjection{State: "schema_absent", Scope: scope, Schema: projectionTargetSchema}}
	present := CatalogStateProjection{Present: &SchemaPresentProjection{State: "schema_present", Scope: scope, Body: pgEmptyNamespaceBody()}}
	condition := CatalogPrecondition{AcceptedStates: []CatalogStateProjection{absent, present}}
	projector := &PGProjector{major: major, capabilities: pgProjectionCapabilities{Major: major, ServerVersionNum: metadata.ServerVersionNum}, normalizer: pg15Normalizer{}}
	verified, err := bindVerifiedSchemaBundleScope(projectionTestDigest, scope, condition, []string{MigrationOwnerRole}, []string{MigrationOwnerRole}, time.Now().Add(time.Hour), 1)
	if err != nil {
		t.Fatal(err)
	}
	overlay := cloneProjectionValue(condition)
	overlay.AcceptedStates[0], overlay.AcceptedStates[1] = overlay.AcceptedStates[1], overlay.AcceptedStates[0]
	snapshot := &pgTestSnapshot{metadata: metadata, queries: map[projectionQueryID][]pgTestQuery{}}
	if _, err := projector.ProjectPrecondition(context.Background(), snapshot, verified, overlay); !IsCode(err, CodeInvalidManifest) {
		t.Fatalf("caller precondition overlay was accepted: %v", err)
	}
	expired, err := bindVerifiedSchemaBundleScope(projectionTestDigest, scope, condition, []string{MigrationOwnerRole}, []string{MigrationOwnerRole}, time.Now().Add(-time.Second), 1)
	if !IsCode(err, CodeUntrusted) {
		t.Fatalf("expired wrapper constructor did not fail closed: %v", err)
	}
	if _, err := projector.ProjectPrecondition(context.Background(), snapshot, expired, condition); !IsCode(err, CodeUntrusted) {
		t.Fatalf("expired verified precondition was accepted: %v", err)
	}
}

func TestPGProjectPreconditionUsesBoundCloneAfterValidation(t *testing.T) {
	major := uint16(15)
	metadata := pgTestMetadata(major)
	migrationID := "000001"
	scope := ProjectionScope{ScopeKind: "predecessor", MigrationID: &migrationID, DeclaredObjects: []ObjectIdentityProjection{}}
	absent := CatalogStateProjection{Absent: &SchemaAbsentProjection{State: "schema_absent", Scope: scope, Schema: projectionTargetSchema}}
	present := CatalogStateProjection{Present: &SchemaPresentProjection{State: "schema_present", Scope: scope, Body: pgEmptyNamespaceBody()}}
	condition := CatalogPrecondition{AcceptedStates: []CatalogStateProjection{absent, present}}
	verified, err := bindVerifiedSchemaBundleScope(projectionTestDigest, scope, condition, []string{MigrationOwnerRole}, []string{MigrationOwnerRole}, time.Now().Add(time.Hour), 1)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := &pgTestSnapshot{metadata: metadata, queries: map[projectionQueryID][]pgTestQuery{
		projectionQueryNamespace: {{rows: [][]any{pgNamespaceRow("absent", pgString(projectionTargetSchema), nil, true, nil, nil, nil, nil, nil, nil, nil)}}},
	}}
	snapshot.queryHook = func(id projectionQueryID) {
		if id == projectionQueryNamespace {
			condition.AcceptedStates[0] = present
		}
	}
	projector := &PGProjector{major: major, capabilities: pgProjectionCapabilities{Major: major, ServerVersionNum: metadata.ServerVersionNum}, normalizer: pg15Normalizer{}}
	result, err := projector.ProjectPrecondition(context.Background(), snapshot, verified, condition)
	if err != nil {
		t.Fatalf("post-validation caller mutation affected bound precondition: %v", err)
	}
	if result.Projection.Absent == nil {
		t.Fatalf("projector did not retain the verified bound predecessor: %+v", result.Projection)
	}
}

func TestPGCatalogAndTransitionRemainNotImplemented(t *testing.T) {
	major := uint16(16)
	metadata := pgTestMetadata(major)
	head := "000001"
	scope := ProjectionScope{ScopeKind: "final", SchemaHead: &head, DeclaredObjects: []ObjectIdentityProjection{}}
	expected := CatalogProjection{SchemaHead: head, Body: pgEmptyNamespaceBody()}
	contract, err := bindVerifiedCatalogContract(projectionTestDigest, scope, expected, time.Now().Add(time.Hour), 1)
	if err != nil {
		t.Fatal(err)
	}
	expected.Body.Schema.Comment = pgString("caller mutation")
	boundExpected := contract.ExpectedProjection()
	if boundExpected.Body.Schema.Comment != nil {
		t.Fatalf("catalog constructor retained caller aliases: %+v", boundExpected.Body.Schema.Comment)
	}
	expected.Body.Schema.Comment = nil
	tampered, err := bindVerifiedCatalogContract(projectionTestDigest, scope, expected, time.Now().Add(time.Hour), 1)
	if err != nil {
		t.Fatal(err)
	}
	tampered.expected.Body.Schema.Comment = pgString("wrapper mutation")
	if err := tampered.validate(); !IsCode(err, CodeUntrusted) {
		t.Fatalf("catalog wrapper self-mutation was accepted: %v", err)
	}
	for name, decision := range map[string]struct {
		expiresAt time.Time
		epoch     uint64
	}{
		"missing": {expiresAt: time.Time{}, epoch: 1},
		"expired": {expiresAt: time.Now().Add(-time.Second), epoch: 1},
		"epoch0":  {expiresAt: time.Now().Add(time.Hour), epoch: 0},
	} {
		if _, err := bindVerifiedCatalogContract(projectionTestDigest, scope, expected, decision.expiresAt, decision.epoch); !IsCode(err, CodeUntrusted) {
			t.Fatalf("catalog %s trust decision was accepted: %v", name, err)
		}
	}
	snapshot := &pgTestSnapshot{metadata: metadata, queries: map[projectionQueryID][]pgTestQuery{}}
	projector := &PGProjector{major: major, capabilities: pgProjectionCapabilities{Major: major, ServerVersionNum: metadata.ServerVersionNum}, normalizer: pg16Normalizer{}}
	if _, err := projector.ProjectCatalog(context.Background(), snapshot, contract, scope); !IsCode(err, CodeProjectionNotImplemented) {
		t.Fatalf("namespace-only catalog projection escaped A2.1b boundary: %v", err)
	}
	if _, err := projector.ProjectTransitionState(context.Background(), snapshot, contract, scope); !IsCode(err, CodeProjectionNotImplemented) {
		t.Fatalf("transition projection escaped A2.1b boundary: %v", err)
	}
}

func pgEmptyNamespaceBody() CatalogProjectionBody {
	return CatalogProjectionBody{
		Schema: SchemaProjection{
			Name: projectionTargetSchema, Owner: MigrationOwnerRole,
			ExplicitACL: ACLSetProjection{CatalogValue: "null", Entries: []ACLProjection{}},
			EffectiveACL: []ACLProjection{{
				Grantor: MigrationOwnerRole, Grantee: MigrationOwnerRole,
				Privileges: []string{"CREATE", "USAGE"}, Grantable: []string{"CREATE", "USAGE"}, Origin: "owner_implicit",
			}},
			SecurityLabels: []SecurityLabel{},
		},
		DefaultACL: []DefaultACLProjection{}, Relations: []RelationProjection{}, Functions: []FunctionProjection{},
		Dependencies: []DependencyProjection{}, DeclaredObjects: []ObjectIdentityProjection{}, DeniedObjects: []DeniedObjectProjection{},
	}
}

func pgNamespaceRow(kind string, namespace, owner *string, aclNull bool, grantor, grantee, privilege *string, grantable *bool, value1, value2, value3 *string) []any {
	return []any{kind, namespace, owner, aclNull, grantor, grantee, privilege, grantable, value1, value2, value3}
}

func pgDefaultACLRow(identity, owner string, schema *string, rawKind string, grantor, grantee, privilege *string, grantable *bool, effectiveCreate bool) []any {
	return []any{identity, owner, schema, rawKind, grantor, grantee, privilege, grantable, effectiveCreate}
}
