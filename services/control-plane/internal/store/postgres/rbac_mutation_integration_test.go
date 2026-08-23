package postgres

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// The store package has no production or test constructor for a verified
// principal. This opt-in integration therefore verifies the closed database
// function authority without executing a mutation; cross-package principal to
// operation conformance is owned by authn's external test harness.
func TestTenantRBACMutationAuthorityPostgresConformance(t *testing.T) {
	if testing.Short() {
		t.Skip("PostgreSQL mutation authority conformance is disabled in short mode")
	}
	databaseURL := os.Getenv("CLOUD_AGENTS_TEST_DATABASE_URL")
	if databaseURL == "" {
		if os.Getenv("CLOUD_AGENTS_REQUIRE_POSTGRES_TEST") == "1" {
			t.Fatal("CLOUD_AGENTS_TEST_DATABASE_URL is required by the PostgreSQL mutation gate")
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
	var callable int
	err = pool.QueryRow(ctx, `SELECT pg_catalog.count(*)
FROM pg_catalog.pg_proc AS routine
JOIN pg_catalog.pg_namespace AS namespace ON namespace.oid = routine.pronamespace
WHERE namespace.nspname = 'cloud_agents'
  AND routine.proname IN ('create_membership','suspend_membership','revoke_membership','bind_role','revoke_role_binding')
  AND pg_catalog.has_function_privilege(current_user, routine.oid, 'EXECUTE')`).Scan(&callable)
	if err != nil {
		t.Fatal(err)
	}
	if callable != 5 {
		t.Fatalf("callable typed RBAC mutation functions = %d, want 5", callable)
	}
	var directDML int
	err = pool.QueryRow(ctx, `SELECT pg_catalog.count(*)
FROM (VALUES
  ('memberships', 'INSERT'), ('memberships', 'UPDATE'), ('memberships', 'DELETE'),
  ('role_bindings', 'INSERT'), ('role_bindings', 'UPDATE'), ('role_bindings', 'DELETE')
) AS checked(table_name, privilege)
WHERE pg_catalog.has_table_privilege(
  current_user,
  'cloud_agents.' || checked.table_name,
  checked.privilege
)`).Scan(&directDML)
	if err != nil {
		t.Fatal(err)
	}
	if directDML != 0 {
		t.Fatalf("direct RBAC table mutation privileges = %d, want 0", directDML)
	}
}
