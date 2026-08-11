package migration

import (
	"context"
	"testing"
	"time"
)

func TestPGAuthorityTypedScansAndPG16Options(t *testing.T) {
	major := uint16(16)
	metadata := pgTestMetadata(major)
	roles := []RoleProjection{
		{Name: "grantor", Inherit: true, ConnectionLimitInt32Decimal: "-1", Config: []string{}},
		{Name: "group", Inherit: true, ConnectionLimitInt32Decimal: "-1", Config: []string{}},
		{Name: "workload", Login: true, Inherit: true, ConnectionLimitInt32Decimal: "5", Config: []string{}},
	}
	snapshot := &pgTestSnapshot{metadata: metadata, queries: map[projectionQueryID][]pgTestQuery{
		projectionQueryCapability: {{rows: [][]any{pgCapabilityRow(major)}}},
		projectionQueryAuthorityRoles: {{rows: [][]any{
			pgRoleRow(roles[0]), pgRoleRow(roles[1]), pgRoleRow(roles[2]),
		}}},
		projectionQueryAuthorityMemberships: {{rows: [][]any{{"group", "workload", "grantor", false, pgString("true"), pgString("false")}}}},
		projectionQueryDatabaseAuthority: {{rows: [][]any{
			pgDatabaseRow("database", nil, nil, nil, nil, nil, nil, nil),
			pgDatabaseRow("acl", pgString("grantor"), pgString("workload"), pgString("CONNECT"), pgBool(false), nil, nil, nil),
			pgDatabaseRow("effective", nil, nil, nil, nil, pgString("grantor"), pgBool(false), pgBool(false)),
			pgDatabaseRow("effective", nil, nil, nil, nil, pgString("group"), pgBool(false), pgBool(false)),
			pgDatabaseRow("effective", nil, nil, nil, nil, pgString("workload"), pgBool(true), pgBool(false)),
		}}},
		projectionQueryRoleSettings: {{rows: [][]any{}}},
	}}
	projector, err := NewPGProjector(context.Background(), snapshot)
	if err != nil {
		t.Fatal(err)
	}
	expected := AuthorityProjection{Roles: roles}
	components, err := projector.readAuthorityComponents(context.Background(), snapshot, expected)
	if err != nil {
		t.Fatal(err)
	}
	if len(components.Roles) != 3 || len(components.DirectMemberships) != 1 {
		t.Fatalf("unexpected authority closure: %+v", components)
	}
	edge := components.DirectMemberships[0]
	if !edge.InheritOption || edge.SetOption {
		t.Fatalf("PG16 grant options were not preserved: %+v", edge)
	}
	if components.DatabaseACL.CatalogValue != "explicit" || len(components.DatabaseACL.Entries) != 1 || !components.EffectiveCreate["workload"] {
		t.Fatalf("database ACL/effective projection differs: %+v", components)
	}
}

func TestPG15MembershipOptionsAreSyntheticLegacyTrue(t *testing.T) {
	normalizer := pg15Normalizer{}
	inherit, set, err := normalizer.membershipOptions(nil, nil)
	if err != nil || !inherit || !set {
		t.Fatalf("PG15 legacy options differ: inherit=%t set=%t err=%v", inherit, set, err)
	}
	if _, _, err := normalizer.membershipOptions(pgString("true"), nil); !IsCode(err, CodeProjectionCapabilityMismatch) {
		t.Fatalf("PG15 accepted post-15 option: %v", err)
	}
}

func TestPGMembershipCanonicalShortestWitnessAndEdgeCount(t *testing.T) {
	projector := &PGProjector{major: 16, normalizer: pg16Normalizer{}}
	roles := map[string]RoleProjection{
		"a": {Name: "a", Inherit: true}, "b": {Name: "b", Inherit: true},
		"g": {Name: "g", Inherit: true}, "z": {Name: "z", Inherit: true},
	}
	edges := []DirectMembershipProjection{
		{Role: "a", Member: "z", Grantor: "g", InheritOption: true, SetOption: true},
		{Role: "b", Member: "z", Grantor: "g", InheritOption: true, SetOption: true},
		{Role: "g", Member: "a", Grantor: "g", InheritOption: true, SetOption: true},
		{Role: "g", Member: "b", Grantor: "g", InheritOption: true, SetOption: false},
	}
	requested := []ReachabilityProjection{{Role: "g", Member: "z"}}
	projection, err := projector.projectReachability(requested, roles, edges)
	if err != nil {
		t.Fatal(err)
	}
	for _, privilege := range projection[0].Privileges {
		if privilege.EdgeCount != 4 {
			t.Fatalf("%s edge_count=%d", privilege.PrivilegeKind, privilege.EdgeCount)
		}
	}
	memberWitness := *projection[0].Privileges[0].CanonicalWitness
	if len(memberWitness) != 3 || memberWitness[0] != "z" || memberWitness[1] != "a" || memberWitness[2] != "g" {
		t.Fatalf("non-canonical shortest witness: %v", memberWitness)
	}
	setWitness := *projection[0].Privileges[2].CanonicalWitness
	if setWitness[1] != "a" {
		t.Fatalf("SET witness ignored edge option: %v", setWitness)
	}
}

func TestPGMembershipCycleAndDuplicateLogicalEdgeReject(t *testing.T) {
	projector := &PGProjector{major: 17, normalizer: pg17Normalizer{}}
	roles := map[string]RoleProjection{"a": {Name: "a", Inherit: true}, "b": {Name: "b", Inherit: true}}
	requested := []ReachabilityProjection{{Role: "b", Member: "a"}}
	cycle := []DirectMembershipProjection{
		{Role: "b", Member: "a", Grantor: "a", InheritOption: true, SetOption: true},
		{Role: "a", Member: "b", Grantor: "a", InheritOption: true, SetOption: true},
	}
	if _, err := projector.projectReachability(requested, roles, cycle); !IsCode(err, CodeProjectionNonCanonicalWitness) {
		t.Fatalf("cycle was not rejected: %v", err)
	}
	duplicate := []DirectMembershipProjection{
		{Role: "b", Member: "a", Grantor: "a", InheritOption: true, SetOption: true},
		{Role: "b", Member: "a", Grantor: "b", InheritOption: true, SetOption: true},
	}
	if _, err := projector.projectReachability(requested, roles, duplicate); !IsCode(err, CodeProjectionNonCanonicalWitness) {
		t.Fatalf("duplicate logical edge was not rejected: %v", err)
	}
}

func TestPGMembershipUsageIsMajorSpecificAndIndependent(t *testing.T) {
	roles := map[string]RoleProjection{
		"member": {Name: "member", Inherit: false},
		"role":   {Name: "role", Inherit: true},
	}
	requested := []ReachabilityProjection{{Role: "role", Member: "member"}}
	edges := []DirectMembershipProjection{{Role: "role", Member: "member", Grantor: "role", InheritOption: true, SetOption: false}}
	pg15, err := (&PGProjector{major: 15, normalizer: pg15Normalizer{}}).projectReachability(requested, roles, edges)
	if err != nil {
		t.Fatal(err)
	}
	pg16, err := (&PGProjector{major: 16, normalizer: pg16Normalizer{}}).projectReachability(requested, roles, edges)
	if err != nil {
		t.Fatal(err)
	}
	if pg15[0].Privileges[1].Reachable || !pg16[0].Privileges[1].Reachable {
		t.Fatalf("USAGE did not preserve PG15 rolinherit vs PG16 edge semantics: pg15=%+v pg16=%+v", pg15, pg16)
	}
	if !pg16[0].Privileges[0].Reachable || pg16[0].Privileges[2].Reachable {
		t.Fatalf("MEMBER/SET were inferred from USAGE: %+v", pg16[0].Privileges)
	}
}

func TestPGReachabilityCrossCheckRejectsIndependentSemanticDrift(t *testing.T) {
	projector := &PGProjector{major: 16, normalizer: pg16Normalizer{}}
	witness := []string{"member", "role"}
	one := uint32(1)
	projected := []ReachabilityProjection{{Role: "role", Member: "member", Privileges: []ReachabilityPrivilegeProjection{
		{PrivilegeKind: "member", Reachable: true, MinDepth: &one, CanonicalWitness: &witness},
		{PrivilegeKind: "usage", Reachable: false},
		{PrivilegeKind: "set", Reachable: false},
	}}}
	snapshot := &pgTestSnapshot{queries: map[projectionQueryID][]pgTestQuery{
		projectionQueryAuthorityReachability: {{rows: [][]any{{"member", "role", true, true, false}}}},
	}}
	if err := projector.crossCheckReachability(context.Background(), snapshot, projected); !IsCode(err, CodeAuthorityDrift) {
		t.Fatalf("independent pg_has_role USAGE drift was accepted: %v", err)
	}
}

func TestPGReachabilityCrossCheckRequiresCompleteOneToOneRows(t *testing.T) {
	projector := &PGProjector{major: 16, normalizer: pg16Normalizer{}}
	projected := []ReachabilityProjection{{Role: "role", Member: "member", Privileges: []ReachabilityPrivilegeProjection{
		{PrivilegeKind: "member", Reachable: false},
		{PrivilegeKind: "usage", Reachable: false},
		{PrivilegeKind: "set", Reachable: false},
	}}}
	tests := []struct {
		name string
		rows [][]any
		code ErrorCode
	}{
		{name: "missing", rows: [][]any{}, code: CodeAuthorityDrift},
		{name: "duplicate", rows: [][]any{{"member", "role", false, false, false}, {"member", "role", false, false, false}}, code: CodeProjectionUnknownObject},
		{name: "unbound", rows: [][]any{{"other", "role", false, false, false}}, code: CodeProjectionUnknownObject},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			snapshot := &pgTestSnapshot{queries: map[projectionQueryID][]pgTestQuery{
				projectionQueryAuthorityReachability: {{rows: test.rows}},
			}}
			if err := projector.crossCheckReachability(context.Background(), snapshot, projected); !IsCode(err, test.code) {
				t.Fatalf("incomplete one-to-one pg_has_role rows were accepted: %v", err)
			}
		})
	}
}

func TestPGProjectAuthorityUsesVerifiedExpectedAndReturnsDigest(t *testing.T) {
	major := uint16(16)
	metadata := pgTestMetadata(major)
	roles := []RoleProjection{
		{Name: BootstrapAdminRole, ConnectionLimitInt32Decimal: "-1", Config: []string{}},
		{Name: MigrationOwnerRole, ConnectionLimitInt32Decimal: "-1", Config: []string{}},
		{Name: RuntimeRole, ConnectionLimitInt32Decimal: "-1", Config: []string{}},
		{Name: "grantor", ConnectionLimitInt32Decimal: "-1", Config: []string{}},
		{Name: "supergrantor", Superuser: true, ConnectionLimitInt32Decimal: "-1", Config: []string{}},
		{Name: "workload", Login: true, ConnectionLimitInt32Decimal: "5", Config: []string{}},
	}
	one := uint32(1)
	witness := []string{"workload", MigrationOwnerRole}
	direct := []DirectMembershipProjection{{Role: MigrationOwnerRole, Member: "workload", Grantor: "supergrantor", InheritOption: false, SetOption: true}}
	reachability := []ReachabilityProjection{{Role: MigrationOwnerRole, Member: "workload", Privileges: []ReachabilityPrivilegeProjection{
		{PrivilegeKind: "member", Reachable: true, MinDepth: &one, CanonicalWitness: &witness, EdgeCount: 1},
		{PrivilegeKind: "usage", Reachable: false, EdgeCount: 1},
		{PrivilegeKind: "set", Reachable: true, MinDepth: &one, CanonicalWitness: &witness, EdgeCount: 1},
	}}}
	effectiveCreate := map[string]bool{}
	effectiveTemporary := map[string]bool{}
	for _, role := range roles {
		effectiveCreate[role.Name] = role.Name == "grantor"
		effectiveTemporary[role.Name] = role.Name == "grantor"
	}
	expected := AuthorityProjection{
		Phase: AuthorityPhaseConnectedSession, SessionUser: metadata.SessionUser, CurrentUser: metadata.CurrentUser,
		DatabaseName: metadata.DatabaseName, DatabaseOwner: "grantor", DatabaseEncoding: "UTF8",
		LocaleProvider: "libc", Datcollate: "C", Datctype: "C",
		DatabaseACL: ACLSetProjection{CatalogValue: "null", Entries: []ACLProjection{}},
		Roles:       roles, DirectMemberships: direct, MembershipReachability: reachability,
		DatabaseRoleSettings: []DatabaseRoleSettingProjection{},
		EffectiveCreate:      effectiveCreate,
		EffectiveTemporary:   effectiveTemporary,
	}
	migrationRole := cloneProjectionValue(expected)
	migrationRole.Phase = AuthorityPhaseMigrationRole
	migrationRole.CurrentUser = MigrationOwnerRole
	migrationTransaction := cloneProjectionValue(expected)
	migrationTransaction.Phase = AuthorityPhaseMigrationTransaction
	migrationTransaction.CurrentUser = MigrationOwnerRole
	authorityInput := AuthorityExpectedProjections{
		ConnectedSession: expected, MigrationRole: migrationRole, MigrationTransaction: migrationTransaction,
	}
	contract, err := bindVerifiedAuthorityContract(projectionTestDigest, authorityInput, time.Now().Add(time.Hour), 1)
	if err != nil {
		t.Fatal(err)
	}
	authorityInput.ConnectedSession.EffectiveCreate["workload"] = true
	boundExpected, err := contract.ExpectedProjection(AuthorityPhaseConnectedSession)
	if err != nil || boundExpected.EffectiveCreate["workload"] {
		t.Fatalf("authority constructor retained caller aliases: projection=%+v err=%v", boundExpected, err)
	}
	authorityInput.ConnectedSession.EffectiveCreate["workload"] = false
	tampered, err := bindVerifiedAuthorityContract(projectionTestDigest, authorityInput, time.Now().Add(time.Hour), 1)
	if err != nil {
		t.Fatal(err)
	}
	tampered.expected.ConnectedSession.EffectiveCreate["workload"] = true
	if _, err := tampered.ExpectedProjection(AuthorityPhaseConnectedSession); !IsCode(err, CodeUntrusted) {
		t.Fatalf("authority wrapper self-mutation was accepted: %v", err)
	}
	for name, decision := range map[string]struct {
		expiresAt time.Time
		epoch     uint64
	}{
		"missing": {expiresAt: time.Time{}, epoch: 1},
		"expired": {expiresAt: time.Now().Add(-time.Second), epoch: 1},
		"epoch0":  {expiresAt: time.Now().Add(time.Hour), epoch: 0},
	} {
		if _, err := bindVerifiedAuthorityContract(projectionTestDigest, authorityInput, decision.expiresAt, decision.epoch); !IsCode(err, CodeUntrusted) {
			t.Fatalf("authority %s trust decision was accepted: %v", name, err)
		}
	}
	databaseRow := pgDatabaseRow("database", nil, nil, nil, nil, nil, nil, nil)
	databaseRow[10] = true
	snapshot := &pgTestSnapshot{metadata: metadata, queries: map[projectionQueryID][]pgTestQuery{
		projectionQueryAuthorityRoles: {{rows: [][]any{
			pgRoleRow(roles[0]), pgRoleRow(roles[1]), pgRoleRow(roles[2]), pgRoleRow(roles[3]), pgRoleRow(roles[4]), pgRoleRow(roles[5]),
		}}},
		projectionQueryAuthorityMemberships: {{rows: [][]any{{MigrationOwnerRole, "workload", "supergrantor", false, pgString("false"), pgString("true")}}}},
		projectionQueryDatabaseAuthority: {{rows: [][]any{
			databaseRow,
			pgDatabaseRow("effective", nil, nil, nil, nil, pgString(BootstrapAdminRole), pgBool(false), pgBool(false)),
			pgDatabaseRow("effective", nil, nil, nil, nil, pgString(MigrationOwnerRole), pgBool(false), pgBool(false)),
			pgDatabaseRow("effective", nil, nil, nil, nil, pgString(RuntimeRole), pgBool(false), pgBool(false)),
			pgDatabaseRow("effective", nil, nil, nil, nil, pgString("grantor"), pgBool(true), pgBool(true)),
			pgDatabaseRow("effective", nil, nil, nil, nil, pgString("supergrantor"), pgBool(false), pgBool(false)),
			pgDatabaseRow("effective", nil, nil, nil, nil, pgString("workload"), pgBool(false), pgBool(false)),
		}}},
		projectionQueryRoleSettings:          {{rows: [][]any{}}},
		projectionQueryAuthorityReachability: {{rows: [][]any{{"workload", MigrationOwnerRole, true, false, true}}}},
	}}
	projector := &PGProjector{major: major, capabilities: pgProjectionCapabilities{Major: major, ServerVersionNum: metadata.ServerVersionNum}, normalizer: pg16Normalizer{}}
	result, err := projector.ProjectAuthority(context.Background(), snapshot, contract, AuthorityPhaseConnectedSession)
	if err != nil {
		t.Fatal(err)
	}
	if result.Projection.Phase != AuthorityPhaseConnectedSession || result.Metadata.Scope != nil || result.Metadata.QueryCount != 5 || result.Digest == "" {
		t.Fatalf("authority result ABI differs: %+v", result)
	}
	snapshot.queries = map[projectionQueryID][]pgTestQuery{
		projectionQueryAuthorityRoles: {{rows: [][]any{
			pgRoleRow(roles[0]), pgRoleRow(roles[1]), pgRoleRow(roles[2]), pgRoleRow(roles[3]), pgRoleRow(roles[4]), pgRoleRow(roles[5]),
		}}},
		projectionQueryAuthorityMemberships: {{rows: [][]any{{MigrationOwnerRole, "workload", "supergrantor", false, pgString("false"), pgString("true")}}}},
		projectionQueryDatabaseAuthority: {{rows: [][]any{
			databaseRow,
			pgDatabaseRow("effective", nil, nil, nil, nil, pgString(BootstrapAdminRole), pgBool(false), pgBool(false)),
			pgDatabaseRow("effective", nil, nil, nil, nil, pgString(MigrationOwnerRole), pgBool(false), pgBool(false)),
			pgDatabaseRow("effective", nil, nil, nil, nil, pgString(RuntimeRole), pgBool(false), pgBool(false)),
			pgDatabaseRow("effective", nil, nil, nil, nil, pgString("grantor"), pgBool(true), pgBool(true)),
			pgDatabaseRow("effective", nil, nil, nil, nil, pgString("supergrantor"), pgBool(false), pgBool(false)),
			pgDatabaseRow("effective", nil, nil, nil, nil, pgString("workload"), pgBool(true), pgBool(false)),
		}}},
		projectionQueryRoleSettings:          {{rows: [][]any{}}},
		projectionQueryAuthorityReachability: {{rows: [][]any{{"workload", MigrationOwnerRole, true, false, true}}}},
	}
	if _, err := projector.ProjectAuthority(context.Background(), snapshot, contract, AuthorityPhaseConnectedSession); !IsCode(err, CodeAuthorityDrift) {
		t.Fatalf("authority drift was not rejected: %v", err)
	}
}

func pgRoleRow(role RoleProjection) []any {
	return []any{role.Name, role.Login, role.Inherit, role.Superuser, role.CreateRole, role.CreateDB, role.Replication, role.BypassRLS, role.ConnectionLimitInt32Decimal, role.ValidUntil, role.Config}
}

func pgDatabaseRow(kind string, grantor, grantee, privilege *string, grantable *bool, principal *string, effectiveCreate, effectiveTemporary *bool) []any {
	encoding, provider, collate, ctype := (*string)(nil), (*string)(nil), (*string)(nil), (*string)(nil)
	aclNull := false
	if kind == "database" {
		encoding, provider, collate, ctype = pgString("UTF8"), pgString("c"), pgString("C"), pgString("C")
	}
	return []any{kind, "cloud_agents_test", "grantor", encoding, provider, collate, ctype,
		(*string)(nil), (*string)(nil), (*string)(nil), aclNull,
		grantor, grantee, privilege, grantable, principal, effectiveCreate, effectiveTemporary}
}
