package postgres

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authz"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestTenantRBACMutationPostgresConformance(t *testing.T) {
	if testing.Short() {
		t.Skip("PostgreSQL mutation conformance is disabled in short mode")
	}
	databaseURL := os.Getenv("CLOUD_AGENTS_TEST_DATABASE_URL")
	if databaseURL == "" {
		if os.Getenv("CLOUD_AGENTS_REQUIRE_POSTGRES_TEST") == "1" {
			t.Fatal("CLOUD_AGENTS_TEST_DATABASE_URL is required by the PostgreSQL mutation gate")
		}
		t.Skip("CLOUD_AGENTS_TEST_DATABASE_URL is not configured")
	}
	tenantID := os.Getenv("CLOUD_AGENTS_MUTATION_TENANT_ID")
	if !validMutationIdentifier(tenantID) {
		t.Fatal("CLOUD_AGENTS_MUTATION_TENANT_ID must identify an isolated seeded mutation tenant")
	}
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatalf("parse PostgreSQL test configuration: %v", err)
	}
	config.MinConns = 1
	config.MaxConns = 4
	config.MaxConnIdleTime = 30 * time.Second
	config.MaxConnLifetime = time.Minute
	config.HealthCheckPeriod = 5 * time.Second

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatalf("create PostgreSQL mutation pool: %v", err)
	}
	t.Cleanup(pool.Close)
	verificationDatabaseURL := os.Getenv("CLOUD_AGENTS_TEST_MIGRATION_DATABASE_URL")
	if verificationDatabaseURL == "" {
		t.Fatal("CLOUD_AGENTS_TEST_MIGRATION_DATABASE_URL is required by the mutation audit gate")
	}
	verificationConfig, err := pgxpool.ParseConfig(verificationDatabaseURL)
	if err != nil {
		t.Fatalf("parse PostgreSQL mutation verification configuration: %v", err)
	}
	verificationConfig.MinConns = 0
	verificationConfig.MaxConns = 1
	verificationPool, err := pgxpool.NewWithConfig(ctx, verificationConfig)
	if err != nil {
		t.Fatalf("create PostgreSQL mutation verification pool: %v", err)
	}
	t.Cleanup(verificationPool.Close)
	assertRBACMutationPrivileges(t, ctx, pool)

	runner := newTenantTransactionRunner(newPGXPool(pool), defaultCleanupTimeout)
	now := time.Now().UTC()
	runner.clock = func() time.Time { return now }
	service, err := newRBACMutationService(runner)
	if err != nil {
		t.Fatalf("create mutation service: %v", err)
	}
	actor := authz.SubjectRef{
		Kind: "user", Issuer: "https://identity.example.test/", Subject: "user-admin",
	}
	target := authz.SubjectRef{
		Kind: "user", Issuer: "https://identity.example.test/", Subject: "user-target",
	}
	scope := authz.ScopeRef{Level: authz.ScopeTenant, ID: tenantID}

	if _, err := service.BindRole(ctx, tenantID, actor, BindRoleInput{
		ExpectedTenantRevision: 3, RoleBindingUID: "platform-role-denied", RoleBindingName: "platform-role-denied",
		Subject: target, RoleName: "platform.admin", RoleVersion: 1, Scope: scope,
		AuditFactUID: "audit-platform-role-denied", ReasonCode: "conformance",
	}); !errors.Is(err, ErrMutationInvalidInput) {
		t.Fatalf("platform role error = %v, want ErrMutationInvalidInput", err)
	}
	equalExpiry := now
	if _, err := service.CreateMembership(ctx, tenantID, actor, CreateMembershipInput{
		ExpectedTenantRevision: 3, MembershipUID: "equal-expiry-denied", MembershipName: "equal-expiry-denied",
		Subject: target, Scope: scope, ExpiresAt: &equalExpiry,
		AuditFactUID: "audit-equal-expiry-denied", ReasonCode: "conformance",
	}); !errors.Is(err, ErrMutationInvalidInput) {
		t.Fatalf("equal expiry error = %v, want ErrMutationInvalidInput", err)
	}
	orphanBinding, err := service.BindRole(ctx, tenantID, actor, BindRoleInput{
		ExpectedTenantRevision: 3, RoleBindingUID: "orphan-binding-denied", RoleBindingName: "orphan-binding-denied",
		Subject: target, RoleName: "tenant.admin", RoleVersion: 1, Scope: scope,
		AuditFactUID: "audit-orphan-binding-denied", ReasonCode: "conformance",
	})
	if !errors.Is(err, ErrMutationConflict) || orphanBinding != (MutationResult{}) {
		t.Fatalf("orphan binding result/error = %#v/%v, want zero/ErrMutationConflict", orphanBinding, err)
	}

	membership, err := service.CreateMembership(ctx, tenantID, actor, CreateMembershipInput{
		ExpectedTenantRevision: 3, MembershipUID: "membership-target", MembershipName: "membership-target",
		Subject: target, Scope: scope, AuditFactUID: "audit-membership-create", ReasonCode: "conformance",
	})
	assertMutationIntegrationResult(t, membership, err, tenantID, "membership-target", 4, authz.MembershipActive)
	binding, err := service.BindRole(ctx, tenantID, actor, BindRoleInput{
		ExpectedTenantRevision: 4, RoleBindingUID: "role-binding-target", RoleBindingName: "role-binding-target",
		Subject: target, RoleName: "tenant.admin", RoleVersion: 1, Scope: scope,
		AuditFactUID: "audit-role-binding-bind", ReasonCode: "conformance",
	})
	assertMutationIntegrationResult(t, binding, err, tenantID, "role-binding-target", 5, authz.BindingActive)

	decision := authorizePostgres(t, ctx, runner, tenantID, authz.Request{
		Subject: target, Permission: permissionMembershipCreate, Resource: scope,
	})
	if !decision.Allowed || decision.Evidence == nil || decision.Evidence.RoleBindingUID != "role-binding-target" {
		t.Fatalf("new subject authorization = %#v", decision)
	}
	suspended, err := service.SuspendMembership(ctx, tenantID, actor, MembershipTransitionInput{
		ExpectedTenantRevision: 5, MembershipUID: "membership-target", ExpectedResourceVersion: 4,
		AuditFactUID: "audit-membership-suspend", ReasonCode: "conformance",
	})
	assertMutationIntegrationResult(t, suspended, err, tenantID, "membership-target", 6, authz.MembershipSuspended)
	assertPostgresDeny(t, authorizePostgres(t, ctx, runner, tenantID, authz.Request{
		Subject: target, Permission: permissionMembershipCreate, Resource: scope,
	}), authz.DenyNoEligibleBinding)
	revokedMembership, err := service.RevokeMembership(ctx, tenantID, actor, MembershipTransitionInput{
		ExpectedTenantRevision: 6, MembershipUID: "membership-target", ExpectedResourceVersion: 6,
		AuditFactUID: "audit-membership-revoke", ReasonCode: "conformance",
	})
	assertMutationIntegrationResult(t, revokedMembership, err, tenantID, "membership-target", 7, authz.MembershipRevoked)
	resurrectingMembership, err := service.CreateMembership(ctx, tenantID, actor, CreateMembershipInput{
		ExpectedTenantRevision: 7, MembershipUID: "membership-target-recreated", MembershipName: "membership-target-recreated",
		Subject: target, Scope: scope, AuditFactUID: "audit-membership-resurrection-denied", ReasonCode: "conformance",
	})
	if !errors.Is(err, ErrMutationConflict) || resurrectingMembership != (MutationResult{}) {
		t.Fatalf("resurrecting membership result/error = %#v/%v, want zero/ErrMutationConflict", resurrectingMembership, err)
	}
	revokedBinding, err := service.RevokeRoleBinding(ctx, tenantID, actor, RevokeRoleBindingInput{
		ExpectedTenantRevision: 7, RoleBindingUID: "role-binding-target", ExpectedResourceVersion: 5,
		AuditFactUID: "audit-role-binding-revoke", ReasonCode: "conformance",
	})
	assertMutationIntegrationResult(t, revokedBinding, err, tenantID, "role-binding-target", 8, authz.BindingRevoked)

	concurrentResults := make(chan MutationResult, 2)
	concurrentErrors := make(chan error, 2)
	var wait sync.WaitGroup
	for index := 0; index < 2; index++ {
		index := index
		wait.Add(1)
		go func() {
			defer wait.Done()
			uid := fmt.Sprintf("membership-race-%d", index)
			result, mutationErr := service.CreateMembership(ctx, tenantID, actor, CreateMembershipInput{
				ExpectedTenantRevision: 8, MembershipUID: uid, MembershipName: uid,
				Subject: authz.SubjectRef{Kind: "user", Issuer: "https://identity.example.test/", Subject: fmt.Sprintf("race-%d", index)},
				Scope:   scope, AuditFactUID: fmt.Sprintf("audit-race-%d", index), ReasonCode: "conformance",
			})
			if mutationErr != nil {
				concurrentErrors <- mutationErr
				return
			}
			concurrentResults <- result
		}()
	}
	wait.Wait()
	close(concurrentResults)
	close(concurrentErrors)
	results := make([]MutationResult, 0, 2)
	for result := range concurrentResults {
		results = append(results, result)
	}
	errorsSeen := make([]error, 0, 2)
	for mutationErr := range concurrentErrors {
		errorsSeen = append(errorsSeen, mutationErr)
	}
	if len(results) != 1 || results[0].ResourceVersion != 9 || len(errorsSeen) != 1 || !errors.Is(errorsSeen[0], ErrMutationConflict) {
		t.Fatalf("concurrent results/errors = %#v/%v", results, errorsSeen)
	}

	futureExpiry := now.Add(time.Hour)
	gapProof, err := service.CreateMembership(ctx, tenantID, actor, CreateMembershipInput{
		ExpectedTenantRevision: 9, MembershipUID: "membership-after-race", MembershipName: "membership-after-race",
		Subject: authz.SubjectRef{Kind: "serviceAccount", Issuer: "spiffe://identity.example.test/", Subject: "after-race"},
		Scope:   scope, ExpiresAt: &futureExpiry, AuditFactUID: "audit-after-race", ReasonCode: "conformance",
	})
	assertMutationIntegrationResult(t, gapProof, err, tenantID, "membership-after-race", 10, authz.MembershipActive)

	assertRBACMutationDurableFacts(t, ctx, verificationPool, tenantID, target)
	if _, err := service.CreateMembership(ctx, "tenant-002", actor, CreateMembershipInput{
		ExpectedTenantRevision: 1, MembershipUID: "cross-tenant-denied", MembershipName: "cross-tenant-denied",
		Subject: target, Scope: authz.ScopeRef{Level: authz.ScopeTenant, ID: "tenant-002"},
		AuditFactUID: "audit-cross-tenant", ReasonCode: "conformance",
	}); !errors.Is(err, ErrMutationDenied) {
		t.Fatalf("cross-tenant actor error = %v, want ErrMutationDenied", err)
	}
}

func assertRBACMutationPrivileges(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	var callable int64
	var helperCallable int64
	var directDML int64
	var ownerBound int64
	var publicCallable int64
	var bootstrapCallable int64
	err := pool.QueryRow(ctx, `SELECT
    (
        SELECT pg_catalog.count(*)
        FROM pg_catalog.pg_proc AS routine
        JOIN pg_catalog.pg_namespace AS namespace ON namespace.oid = routine.pronamespace
        WHERE namespace.nspname = 'cloud_agents'
            AND routine.proname IN (
                'create_membership', 'suspend_membership', 'revoke_membership',
                'bind_role', 'revoke_role_binding'
            )
            AND pg_catalog.has_function_privilege(current_user, routine.oid, 'EXECUTE')
    ),
    (
        SELECT pg_catalog.count(*)
        FROM pg_catalog.pg_proc AS routine
        JOIN pg_catalog.pg_namespace AS namespace ON namespace.oid = routine.pronamespace
        WHERE namespace.nspname = 'cloud_agents'
            AND routine.proname IN (
                'subject_ref_digest', 'require_runtime_mutation_principal',
                'allocate_tenant_revision', 'transition_membership'
            )
            AND pg_catalog.has_function_privilege(current_user, routine.oid, 'EXECUTE')
    ),
    (
        SELECT pg_catalog.count(*)
        FROM (VALUES ('memberships'::text), ('role_bindings'::text)) AS target(table_name)
        WHERE pg_catalog.has_table_privilege(
            current_user,
            pg_catalog.format('cloud_agents.%I', target.table_name),
            'INSERT,UPDATE,DELETE'
        )
    ),
    (
        SELECT pg_catalog.count(*)
        FROM pg_catalog.pg_proc AS routine
        JOIN pg_catalog.pg_namespace AS namespace ON namespace.oid = routine.pronamespace
        JOIN pg_catalog.pg_roles AS owner_role ON owner_role.oid = routine.proowner
        WHERE namespace.nspname = 'cloud_agents'
            AND routine.proname IN (
                'subject_ref_digest', 'require_runtime_mutation_principal',
                'allocate_tenant_revision', 'create_membership', 'transition_membership',
                'suspend_membership', 'revoke_membership', 'bind_role', 'revoke_role_binding'
            )
            AND owner_role.rolname = 'cloud_agents_migration_owner'
    ),
    (
        SELECT pg_catalog.count(*)
        FROM pg_catalog.pg_proc AS routine
        JOIN pg_catalog.pg_namespace AS namespace ON namespace.oid = routine.pronamespace
        WHERE namespace.nspname = 'cloud_agents'
            AND routine.proname IN (
                'subject_ref_digest', 'require_runtime_mutation_principal',
                'allocate_tenant_revision', 'create_membership', 'transition_membership',
                'suspend_membership', 'revoke_membership', 'bind_role', 'revoke_role_binding'
            )
            AND EXISTS (
                SELECT 1
                FROM pg_catalog.aclexplode(
                    COALESCE(routine.proacl, pg_catalog.acldefault('f', routine.proowner))
                ) AS privilege
                WHERE privilege.grantee = 0 AND privilege.privilege_type = 'EXECUTE'
            )
    ),
    (
        SELECT pg_catalog.count(*)
        FROM pg_catalog.pg_proc AS routine
        JOIN pg_catalog.pg_namespace AS namespace ON namespace.oid = routine.pronamespace
        WHERE namespace.nspname = 'cloud_agents'
            AND routine.proname IN (
                'subject_ref_digest', 'require_runtime_mutation_principal',
                'allocate_tenant_revision', 'create_membership', 'transition_membership',
                'suspend_membership', 'revoke_membership', 'bind_role', 'revoke_role_binding'
            )
            AND pg_catalog.has_function_privilege(
                'cloud_agents_bootstrap_admin', routine.oid, 'EXECUTE'
            )
    )`).Scan(
		&callable,
		&helperCallable,
		&directDML,
		&ownerBound,
		&publicCallable,
		&bootstrapCallable,
	)
	if err != nil {
		t.Fatalf("read mutation privileges: %v", err)
	}
	if callable != 5 || helperCallable != 0 || directDML != 0 || ownerBound != 9 || publicCallable != 0 || bootstrapCallable != 0 {
		t.Fatalf(
			"callable/helper/direct-DML/owner/public/bootstrap counts = %d/%d/%d/%d/%d/%d",
			callable, helperCallable, directDML, ownerBound, publicCallable, bootstrapCallable,
		)
	}
}

func assertMutationIntegrationResult(
	t *testing.T,
	result MutationResult,
	err error,
	tenantID string,
	uid string,
	version int64,
	state string,
) {
	t.Helper()
	if err != nil {
		t.Fatalf("mutation %s error = %v", uid, err)
	}
	if result != (MutationResult{TenantID: tenantID, ResourceUID: uid, ResourceVersion: version, State: state}) {
		t.Fatalf("mutation %s result = %#v", uid, result)
	}
}

func assertRBACMutationDurableFacts(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	tenantID string,
	target authz.SubjectRef,
) {
	t.Helper()
	transaction, err := pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatalf("begin durable fact read: %v", err)
	}
	defer func() { _ = transaction.Rollback(context.Background()) }()
	if _, err := transaction.Exec(ctx, "SET ROLE cloud_agents_migration_owner"); err != nil {
		t.Fatalf("enter mutation verification role: %v", err)
	}
	var configured string
	if err := transaction.QueryRow(ctx, bindTenantSQL, tenantID).Scan(&configured); err != nil || configured != tenantID {
		t.Fatalf("bind durable fact tenant = %q/%v", configured, err)
	}
	var revision int64
	if err := transaction.QueryRow(ctx, `SELECT current_revision
FROM cloud_agents.tenant_resource_versions
WHERE tenant_id = cloud_agents.require_tenant_id() AND tenant_uid = tenant_id`).Scan(&revision); err != nil {
		t.Fatalf("read final tenant revision: %v", err)
	}
	if revision != 10 {
		t.Fatalf("final tenant revision = %d, want 10", revision)
	}
	var actions []string
	if err := transaction.QueryRow(ctx, `SELECT pg_catalog.array_agg(action ORDER BY resource_version)
FROM cloud_agents.audit_facts
WHERE tenant_id = cloud_agents.require_tenant_id()
    AND resource_version BETWEEN 4 AND 8`).Scan(&actions); err != nil {
		t.Fatalf("read mutation audit actions: %v", err)
	}
	wantActions := []string{
		"membership.create", "role_binding.bind", "membership.suspend",
		"membership.revoke", "role_binding.revoke",
	}
	if len(actions) != len(wantActions) {
		t.Fatalf("audit actions = %#v", actions)
	}
	for index := range actions {
		if actions[index] != wantActions[index] {
			t.Fatalf("audit actions = %#v, want %#v", actions, wantActions)
		}
	}
	var actorPrincipals []string
	if err := transaction.QueryRow(ctx, `SELECT pg_catalog.array_agg(DISTINCT actor_database_principal ORDER BY actor_database_principal)
FROM cloud_agents.audit_facts
WHERE tenant_id = cloud_agents.require_tenant_id() AND resource_version BETWEEN 4 AND 10`).Scan(&actorPrincipals); err != nil {
		t.Fatalf("read mutation audit principals: %v", err)
	}
	if len(actorPrincipals) != 1 || actorPrincipals[0] != "cag_runtime" {
		t.Fatalf("audit principals = %#v", actorPrincipals)
	}
	wantDigest, err := target.Digest()
	if err != nil {
		t.Fatal(err)
	}
	var storedDigest string
	if err := transaction.QueryRow(ctx, `SELECT subject_digest
FROM cloud_agents.memberships
WHERE tenant_id = cloud_agents.require_tenant_id() AND membership_uid = 'membership-target'`).Scan(&storedDigest); err != nil {
		t.Fatalf("read stored subject digest: %v", err)
	}
	if storedDigest != wantDigest {
		t.Fatalf("stored subject digest = %s, want %s", storedDigest, wantDigest)
	}
	var versions []int64
	if err := transaction.QueryRow(ctx, `SELECT pg_catalog.array_agg(resource_version ORDER BY resource_version)
FROM cloud_agents.resource_changes
WHERE tenant_id = cloud_agents.require_tenant_id() AND resource_version BETWEEN 4 AND 10`).Scan(&versions); err != nil {
		t.Fatalf("read mutation resource versions: %v", err)
	}
	sort.Slice(versions, func(i, j int) bool { return versions[i] < versions[j] })
	wantVersions := []int64{4, 5, 6, 7, 8, 9, 10}
	if len(versions) != len(wantVersions) {
		t.Fatalf("resource versions = %#v", versions)
	}
	for index := range versions {
		if versions[index] != wantVersions[index] {
			t.Fatalf("resource versions = %#v, want %#v", versions, wantVersions)
		}
	}
	if err := transaction.Rollback(ctx); err != nil {
		t.Fatalf("rollback durable fact read: %v", err)
	}
}
