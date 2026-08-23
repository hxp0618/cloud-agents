package postgres

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authz"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// This opt-in test proves only the database fact-materialization side of the
// sealed RBAC boundary. A production principal cannot be minted by tests in
// this package, so end-to-end binding belongs to authn's external conformance
// harness rather than a store-local constructor.
func TestTenantRBACFactMaterializationPostgresConformance(t *testing.T) {
	if testing.Short() {
		t.Skip("PostgreSQL RBAC fact conformance is disabled in short mode")
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
		t.Fatal(err)
	}
	config.MinConns, config.MaxConns = 1, 1
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	var canSelectRoles, canInsertRoles, canSelectPermissions, canInsertPermissions bool
	var canSelectMemberships, canInsertMemberships, canSelectBindings, canInsertBindings bool
	err = pool.QueryRow(ctx, `SELECT
pg_catalog.has_table_privilege(current_user, 'cloud_agents.builtin_roles', 'SELECT'),
pg_catalog.has_table_privilege(current_user, 'cloud_agents.builtin_roles', 'INSERT'),
pg_catalog.has_table_privilege(current_user, 'cloud_agents.builtin_role_permissions', 'SELECT'),
pg_catalog.has_table_privilege(current_user, 'cloud_agents.builtin_role_permissions', 'INSERT'),
pg_catalog.has_table_privilege(current_user, 'cloud_agents.memberships', 'SELECT'),
pg_catalog.has_table_privilege(current_user, 'cloud_agents.memberships', 'INSERT'),
pg_catalog.has_table_privilege(current_user, 'cloud_agents.role_bindings', 'SELECT'),
pg_catalog.has_table_privilege(current_user, 'cloud_agents.role_bindings', 'INSERT')`).Scan(
		&canSelectRoles, &canInsertRoles, &canSelectPermissions, &canInsertPermissions,
		&canSelectMemberships, &canInsertMemberships, &canSelectBindings, &canInsertBindings,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !canSelectRoles || canInsertRoles || !canSelectPermissions || canInsertPermissions ||
		!canSelectMemberships || canInsertMemberships || !canSelectBindings || canInsertBindings {
		t.Fatal("runtime RBAC table privilege boundary drift")
	}
	for _, table := range []string{"memberships", "role_bindings"} {
		var ignored int64
		err = pool.QueryRow(ctx, `SELECT CASE $1
WHEN 'memberships' THEN (SELECT pg_catalog.count(*) FROM cloud_agents.memberships)
WHEN 'role_bindings' THEN (SELECT pg_catalog.count(*) FROM cloud_agents.role_bindings)
END`, table).Scan(&ignored)
		var postgresError *pgconn.PgError
		if !errors.As(err, &postgresError) || postgresError.Code != "22023" {
			t.Fatalf("unbound %s read error = %v, want SQLSTATE 22023", table, err)
		}
	}
	runner, err := NewTenantTransactionRunner(pool)
	if err != nil {
		t.Fatal(err)
	}
	subject := authz.SubjectRef{Kind: "user", Issuer: "https://identity.example.test/", Subject: "user-alpha"}
	err = runner.WithTenantRead(ctx, "tenant-001", func(callbackContext context.Context, capability TenantReadCapability) error {
		handle, ok := capability.(*tenantReadHandle)
		if !ok {
			t.Fatal("unexpected tenant capability implementation")
		}
		scope, resolved, readErr := handle.resolveAuthorizationScope(callbackContext, authz.ScopeRef{Level: authz.ScopeProject, ID: "project-alpha"})
		if readErr != nil || !resolved || scope.ProjectID != "project-alpha" {
			return readErr
		}
		if _, readErr = handle.readBuiltinRoleCatalog(callbackContext); readErr != nil {
			return readErr
		}
		candidates, readErr := handle.readAuthorizationCandidates(callbackContext, subject)
		if readErr == nil && len(candidates) == 0 {
			t.Fatal("seeded RBAC candidate is absent")
		}
		return readErr
	})
	if err != nil {
		t.Fatal(err)
	}
}
