package postgres

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestTenantTransactionRunnerPostgresConformance(t *testing.T) {
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

	initialConnection, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire initial PostgreSQL connection: %v", err)
	}
	var initialPID int32
	var initialTenantSetting *string
	if err := initialConnection.QueryRow(ctx, `SELECT pg_catalog.pg_backend_pid()`).Scan(&initialPID); err != nil {
		initialConnection.Release()
		t.Fatalf("read initial backend PID: %v", err)
	}
	if err := initialConnection.QueryRow(
		ctx,
		`SELECT pg_catalog.current_setting('cloud_agents.tenant_id', true)`,
	).Scan(&initialTenantSetting); err != nil {
		initialConnection.Release()
		t.Fatalf("read initial tenant setting: %v", err)
	}
	initialConnection.Release()
	if initialTenantSetting != nil {
		t.Fatalf("fresh physical connection tenant setting = %q, want NULL", *initialTenantSetting)
	}

	runner, err := NewTenantTransactionRunner(pool)
	if err != nil {
		t.Fatalf("create tenant transaction runner: %v", err)
	}
	for _, tenantID := range []string{"tenant-001", "tenant-002"} {
		tenantID := tenantID
		if err := runner.WithTenantRead(ctx, tenantID, func(
			callbackContext context.Context,
			capability TenantReadCapability,
		) error {
			tenant, readErr := capability.GetPlatformTenant(callbackContext)
			if readErr != nil {
				return readErr
			}
			if tenant.TenantID != tenantID || tenant.TenantUID != tenantID {
				t.Fatalf("tenant projection = %#v, want %s", tenant, tenantID)
			}
			return nil
		}); err != nil {
			t.Fatalf("WithTenantRead(%q): %v", tenantID, err)
		}
	}

	reusedConnection, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire reused PostgreSQL connection: %v", err)
	}
	defer reusedConnection.Release()
	var reusedPID int32
	var clearedTenantSetting *string
	if err := reusedConnection.QueryRow(ctx, `SELECT pg_catalog.pg_backend_pid()`).Scan(&reusedPID); err != nil {
		t.Fatalf("read reused backend PID: %v", err)
	}
	if reusedPID != initialPID {
		t.Fatalf("physical connection changed: initial PID %d, reused PID %d", initialPID, reusedPID)
	}
	if err := reusedConnection.QueryRow(
		ctx,
		`SELECT pg_catalog.current_setting('cloud_agents.tenant_id', true)`,
	).Scan(&clearedTenantSetting); err != nil {
		t.Fatalf("read cleared tenant setting: %v", err)
	}
	if clearedTenantSetting == nil || *clearedTenantSetting != "" {
		t.Fatalf("reused physical connection tenant setting = %#v, want empty string", clearedTenantSetting)
	}

	var unexpectedTenant string
	err = reusedConnection.QueryRow(ctx, `SELECT cloud_agents.require_tenant_id()`).Scan(&unexpectedTenant)
	var postgresError *pgconn.PgError
	if !errors.As(err, &postgresError) || postgresError.Code != "22023" {
		t.Fatalf("cleared require_tenant_id error = %v, want SQLSTATE 22023", err)
	}
	if status := reusedConnection.Conn().PgConn().TxStatus(); status != idleTransactionStatus {
		t.Fatalf("connection status after cleared-state error = %q, want idle", status)
	}
}
