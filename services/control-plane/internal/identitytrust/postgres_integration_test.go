package identitytrust

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPostgresCheckpointRuntimeConformance(t *testing.T) {
	if testing.Short() {
		t.Skip("PostgreSQL identity trust conformance is disabled in short mode")
	}
	databaseURL := os.Getenv("CLOUD_AGENTS_TEST_DATABASE_URL")
	if databaseURL == "" {
		if os.Getenv("CLOUD_AGENTS_REQUIRE_POSTGRES_TEST") == "1" {
			t.Fatal("CLOUD_AGENTS_TEST_DATABASE_URL is required by the PostgreSQL identity trust gate")
		}
		t.Skip("CLOUD_AGENTS_TEST_DATABASE_URL is not configured")
	}
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatalf("parse PostgreSQL test configuration: %v", err)
	}
	config.MinConns, config.MaxConns = 1, 1
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatalf("create PostgreSQL test pool: %v", err)
	}
	t.Cleanup(pool.Close)
	transaction, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatalf("begin checkpoint transaction: %v", err)
	}
	defer func() { _ = transaction.Rollback(context.Background()) }()

	var callable int
	if err := transaction.QueryRow(ctx, `SELECT pg_catalog.count(*)
FROM pg_catalog.pg_proc AS routine
JOIN pg_catalog.pg_namespace AS namespace ON namespace.oid = routine.pronamespace
WHERE namespace.nspname = 'cloud_agents_identity'
  AND routine.proname IN ('load_trust_checkpoint', 'compare_trust_checkpoint')
  AND pg_catalog.has_function_privilege(current_user, routine.oid, 'EXECUTE')`).Scan(&callable); err != nil {
		t.Fatalf("read checkpoint function privileges: %v", err)
	}
	if callable != 2 {
		t.Fatalf("callable checkpoint functions = %d, want 2", callable)
	}
	var directTablePrivileges int
	if err := transaction.QueryRow(ctx, `SELECT pg_catalog.count(*)
FROM pg_catalog.unnest(ARRAY['SELECT','INSERT','UPDATE','DELETE','TRUNCATE','REFERENCES','TRIGGER']::text[]) AS privilege
WHERE pg_catalog.has_table_privilege(current_user, 'cloud_agents_identity.trust_checkpoints', privilege)`).Scan(&directTablePrivileges); err != nil {
		t.Fatalf("read checkpoint table privileges: %v", err)
	}
	if directTablePrivileges != 0 {
		t.Fatalf("direct checkpoint table privileges = %d, want 0", directTablePrivileges)
	}

	state := checkpointStateForDatabase(t)
	state.Issuer = fmt.Sprintf("https://identity-trust-%d.example.test", time.Now().UnixNano())
	state.AuthorityDigest, err = authorityDigest(state)
	if err != nil {
		t.Fatal(err)
	}
	checkpoint := &PostgresCheckpoint{database: transaction}
	if _, exists, err := checkpoint.Load(ctx, state.Issuer); err != nil || exists {
		t.Fatalf("initial checkpoint exists=%t err=%v", exists, err)
	}
	if err := checkpoint.CompareAndSwap(ctx, state.Issuer, "", state); err != nil {
		t.Fatalf("insert checkpoint: %v", err)
	}
	if err := checkpoint.CompareAndSwap(ctx, state.Issuer, "", state); !errors.Is(err, ErrCheckpoint) {
		t.Fatalf("duplicate absent checkpoint error = %v", err)
	}
	loaded, exists, err := checkpoint.Load(ctx, state.Issuer)
	if err != nil || !exists || loaded.AuthorityDigest != state.AuthorityDigest {
		t.Fatalf("loaded checkpoint exists=%t state=%+v err=%v", exists, loaded, err)
	}
	next := cloneState(state)
	next.Revision++
	next.AuthorityDigest, err = authorityDigest(next)
	if err != nil {
		t.Fatal(err)
	}
	if err := checkpoint.CompareAndSwap(ctx, state.Issuer, "sha256:"+strings.Repeat("0", 64), next); !errors.Is(err, ErrCheckpoint) {
		t.Fatalf("invalid expected digest error = %v", err)
	}
	if err := checkpoint.CompareAndSwap(ctx, state.Issuer, state.AuthorityDigest, next); err != nil {
		t.Fatalf("advance checkpoint: %v", err)
	}
	loaded, exists, err = checkpoint.Load(ctx, state.Issuer)
	if err != nil || !exists || loaded.Revision != next.Revision || loaded.AuthorityDigest != next.AuthorityDigest {
		t.Fatalf("advanced checkpoint exists=%t state=%+v err=%v", exists, loaded, err)
	}

	var ignored []byte
	err = transaction.QueryRow(ctx, `SELECT state_bytes FROM cloud_agents_identity.load_trust_checkpoint($1)`, " https://invalid.example").Scan(&ignored)
	var postgresError *pgconn.PgError
	if !errors.As(err, &postgresError) || postgresError.Code != "22023" {
		t.Fatalf("invalid issuer error = %v, want SQLSTATE 22023", err)
	}
}
