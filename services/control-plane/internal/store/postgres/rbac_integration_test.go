package postgres

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authz"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestTenantAuthorizationPostgresConformance(t *testing.T) {
	if testing.Short() {
		t.Skip("PostgreSQL conformance is disabled in short mode")
	}
	databaseURL := os.Getenv("CLOUD_AGENTS_TEST_DATABASE_URL")
	if databaseURL == "" {
		if os.Getenv("CLOUD_AGENTS_REQUIRE_POSTGRES_TEST") == "1" {
			t.Fatal("CLOUD_AGENTS_TEST_DATABASE_URL is required by the PostgreSQL conformance gate")
		}
		t.Skip("CLOUD_AGENTS_TEST_DATABASE_URL is not configured")
	}
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatalf("parse PostgreSQL test configuration: %v", err)
	}
	config.MinConns = 1
	config.MaxConns = 1
	config.MaxConnIdleTime = 30 * time.Second
	config.MaxConnLifetime = time.Minute
	config.HealthCheckPeriod = 5 * time.Second

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatalf("create PostgreSQL test pool: %v", err)
	}
	t.Cleanup(pool.Close)

	var canSelectRoles bool
	var canInsertRoles bool
	var canSelectPermissions bool
	var canInsertPermissions bool
	var canSelectMemberships bool
	var canInsertMemberships bool
	var canSelectBindings bool
	var canInsertBindings bool
	if err := pool.QueryRow(ctx, `SELECT
		pg_catalog.has_table_privilege(current_user, 'cloud_agents.builtin_roles', 'SELECT'),
		pg_catalog.has_table_privilege(current_user, 'cloud_agents.builtin_roles', 'INSERT'),
		pg_catalog.has_table_privilege(current_user, 'cloud_agents.builtin_role_permissions', 'SELECT'),
		pg_catalog.has_table_privilege(current_user, 'cloud_agents.builtin_role_permissions', 'INSERT'),
        pg_catalog.has_table_privilege(current_user, 'cloud_agents.memberships', 'SELECT'),
		pg_catalog.has_table_privilege(current_user, 'cloud_agents.memberships', 'INSERT'),
		pg_catalog.has_table_privilege(current_user, 'cloud_agents.role_bindings', 'SELECT'),
		pg_catalog.has_table_privilege(current_user, 'cloud_agents.role_bindings', 'INSERT')`).Scan(
		&canSelectRoles,
		&canInsertRoles,
		&canSelectPermissions,
		&canInsertPermissions,
		&canSelectMemberships,
		&canInsertMemberships,
		&canSelectBindings,
		&canInsertBindings,
	); err != nil {
		t.Fatalf("read runtime RBAC privileges: %v", err)
	}
	if !canSelectRoles || canInsertRoles || !canSelectPermissions || canInsertPermissions || !canSelectMemberships || canInsertMemberships || !canSelectBindings || canInsertBindings {
		t.Fatalf(
			"runtime roles/permissions/memberships/bindings SELECT/INSERT = %t/%t %t/%t %t/%t %t/%t, want true/false for each",
			canSelectRoles, canInsertRoles, canSelectPermissions, canInsertPermissions,
			canSelectMemberships, canInsertMemberships, canSelectBindings, canInsertBindings,
		)
	}
	for _, table := range []string{"memberships", "role_bindings"} {
		var unboundCount int64
		err = pool.QueryRow(ctx, `SELECT CASE $1
			WHEN 'memberships' THEN (SELECT pg_catalog.count(*) FROM cloud_agents.memberships)
			WHEN 'role_bindings' THEN (SELECT pg_catalog.count(*) FROM cloud_agents.role_bindings)
		END`, table).Scan(&unboundCount)
		var postgresError *pgconn.PgError
		if !errors.As(err, &postgresError) || postgresError.Code != "22023" {
			t.Fatalf("unbound %s read error = %v, want SQLSTATE 22023", table, err)
		}
	}
	assertPostgresTenantRBACCounts(t, ctx, pool, "tenant-001", 2, 1)
	assertPostgresTenantRBACCounts(t, ctx, pool, "tenant-002", 0, 0)

	runner, err := NewTenantTransactionRunner(pool)
	if err != nil {
		t.Fatalf("create tenant transaction runner: %v", err)
	}
	runner.clock = func() time.Time {
		return time.Date(2026, time.August, 17, 12, 0, 0, 0, time.UTC)
	}
	subject := authz.SubjectRef{
		Kind: "user", Issuer: "https://identity.example.test/", Subject: "user-alpha",
	}
	request := authz.Request{
		Subject: subject, Permission: "projects.get",
		Resource: authz.ScopeRef{Level: authz.ScopeProject, ID: "project-alpha"},
	}
	decision := authorizePostgres(t, ctx, runner, "tenant-001", request)
	if !decision.Allowed || decision.Evidence == nil || decision.Evidence.MembershipUID != "membership-alpha" || decision.Evidence.RoleBindingUID != "role-binding-alpha" || decision.Evidence.RoleName != "project.viewer" {
		t.Fatalf("allow decision = %#v", decision)
	}

	permissionDenied := request
	permissionDenied.Permission = "projects.update"
	assertPostgresDeny(t, authorizePostgres(t, ctx, runner, "tenant-001", permissionDenied), authz.DenyNoEligibleBinding)
	unknownPermission := request
	unknownPermission.Permission = "projects.future"
	assertPostgresDeny(t, authorizePostgres(t, ctx, runner, "tenant-001", unknownPermission), authz.DenyNoEligibleBinding)
	crossTenant := authorizePostgres(t, ctx, runner, "tenant-002", request)
	assertPostgresDeny(t, crossTenant, authz.DenyUnknownScope)
	caseChanged := request
	caseChanged.Subject.Issuer = "https://Identity.Example.Test/"
	assertPostgresDeny(t, authorizePostgres(t, ctx, runner, "tenant-001", caseChanged), authz.DenyNoEligibleBinding)
	platform := request
	platform.Resource = authz.ScopeRef{Level: authz.ScopePlatform}
	assertPostgresDeny(t, authorizePostgres(t, ctx, runner, "tenant-001", platform), authz.DenyPlatformRuntime)
}

func assertPostgresTenantRBACCounts(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	tenantID string,
	wantMemberships int64,
	wantBindings int64,
) {
	t.Helper()
	transaction, err := pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatalf("begin %s RBAC count transaction: %v", tenantID, err)
	}
	defer func() { _ = transaction.Rollback(context.Background()) }()
	var configured string
	if err := transaction.QueryRow(ctx, `SELECT pg_catalog.set_config('cloud_agents.tenant_id', $1, true)`, tenantID).Scan(&configured); err != nil {
		t.Fatalf("bind %s RBAC count transaction: %v", tenantID, err)
	}
	if configured != tenantID {
		t.Fatalf("configured RBAC tenant = %q, want %q", configured, tenantID)
	}
	var memberships int64
	var bindings int64
	if err := transaction.QueryRow(ctx, `SELECT
		(SELECT pg_catalog.count(*) FROM cloud_agents.memberships),
		(SELECT pg_catalog.count(*) FROM cloud_agents.role_bindings)`).Scan(&memberships, &bindings); err != nil {
		t.Fatalf("read %s RBAC counts: %v", tenantID, err)
	}
	if memberships != wantMemberships || bindings != wantBindings {
		t.Fatalf("%s membership/binding counts = %d/%d, want %d/%d", tenantID, memberships, bindings, wantMemberships, wantBindings)
	}
	if err := transaction.Rollback(ctx); err != nil {
		t.Fatalf("rollback %s RBAC count transaction: %v", tenantID, err)
	}
}

func authorizePostgres(
	t *testing.T,
	ctx context.Context,
	runner *TenantTransactionRunner,
	tenantID string,
	request authz.Request,
) authz.Decision {
	t.Helper()
	var decision authz.Decision
	err := runner.WithTenantRead(ctx, tenantID, func(
		callbackContext context.Context,
		capability TenantReadCapability,
	) error {
		var authorizeErr error
		decision, authorizeErr = capability.Authorize(callbackContext, request)
		return authorizeErr
	})
	if err != nil {
		t.Fatalf("authorize tenant %q: %v", tenantID, err)
	}
	return decision
}

func assertPostgresDeny(t *testing.T, decision authz.Decision, reason authz.DenyReason) {
	t.Helper()
	if decision.Allowed || decision.Evidence != nil || decision.Reason != reason {
		t.Fatalf("decision = %#v, want deny %s", decision, reason)
	}
}
