#!/usr/bin/env bash

set -euo pipefail

script_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
repo_root=$(cd -- "$script_dir/../../.." && pwd -P)
module_dir="$repo_root/services/control-plane"

if [[ $(go version | awk '{print $3}') != "go1.26.6" ]]; then
  echo "Go 1.26.6 is required" >&2
  exit 1
fi
if ! docker version >/dev/null 2>&1; then
  echo "A running Docker daemon is required" >&2
  exit 1
fi

declare -a matrix=(
  "15|150018|postgres@sha256:6eb0add3b77c081df18aa518ce43df58fdcc40f2e6d868a6fd08038dc7acd425"
  "16|160014|postgres@sha256:95206741a5b214807675e14165369d05b93a9cf692223b616d07cca227e74b0b"
  "17|170010|postgres@sha256:742f40ea20b9ff2ff31db5458d127452988a2164df9e17441e191f3b72252193"
)

run_id="tenant-helper-matrix-$$-$RANDOM"
ownership_label="com.hxp0618.cloud-agents.test-run"
active_container=""
cleanup() {
  if [[ -n "$active_container" ]]; then
    if docker container inspect "$active_container" >/dev/null 2>&1; then
      observed_owner=$(docker container inspect \
        --format "{{index .Config.Labels \"$ownership_label\"}}" \
        "$active_container")
      if [[ $observed_owner != "$run_id" ]]; then
        echo "Refusing to remove container not owned by this run: $active_container" >&2
        return 1
      fi
      docker rm -f "$active_container" >/dev/null
    fi
    active_container=""
  fi
}
on_signal() {
  local exit_code=$1
  trap - EXIT INT TERM
  cleanup || true
  exit "$exit_code"
}
trap cleanup EXIT
trap 'on_signal 130' INT
trap 'on_signal 143' TERM

for matrix_entry in "${matrix[@]}"; do
  IFS='|' read -r postgres_major expected_version image <<<"$matrix_entry"
  if ! docker image inspect "$image" >/dev/null 2>&1; then
    echo "Missing exact local image: $image" >&2
    echo "Pull it explicitly before rerunning; this matrix never pulls implicitly." >&2
    exit 1
  fi

  active_container="cag-p1-tenant-pg${postgres_major}-$$-$RANDOM"
  test_password="cag-local-only-${postgres_major}-$$"
  if docker container inspect "$active_container" >/dev/null 2>&1; then
    echo "Refusing to reuse existing container name: $active_container" >&2
    exit 1
  fi
  docker run -d \
    --pull=never \
    --name "$active_container" \
    --label "$ownership_label=$run_id" \
    -p 127.0.0.1::5432 \
    -e POSTGRES_PASSWORD="$test_password" \
    -e POSTGRES_INITDB_ARGS='--encoding=UTF8 --locale=C' \
    -v "$module_dir/migrations:/workspace/services/control-plane/migrations:ro" \
    "$image" >/dev/null

  ready_count=0
  for attempt in $(seq 1 90); do
    if docker exec "$active_container" \
      pg_isready -h 127.0.0.1 -U postgres -d postgres >/dev/null 2>&1; then
      ready_count=$((ready_count + 1))
      if [[ $ready_count -ge 2 ]]; then
        break
      fi
    else
      ready_count=0
    fi
    if [[ $attempt -eq 90 ]]; then
      docker logs "$active_container" >&2
      exit 1
    fi
    sleep 1
  done

  docker exec -e PGPASSWORD="$test_password" "$active_container" \
    psql -X -v ON_ERROR_STOP=1 -h 127.0.0.1 -U postgres -d postgres \
    -f /workspace/services/control-plane/migrations/bootstrap/roles.sql >/dev/null

  if [[ $postgres_major -eq 15 ]]; then
    membership_grants=$(cat <<'SQL'
GRANT cloud_agents_migration_owner TO cag_migration;
GRANT cloud_agents_runtime TO cag_runtime;
GRANT cloud_agents_bootstrap_admin TO cag_bootstrap;
SQL
)
  else
    membership_grants=$(cat <<'SQL'
GRANT cloud_agents_migration_owner TO cag_migration WITH INHERIT FALSE, SET TRUE;
GRANT cloud_agents_runtime TO cag_runtime WITH INHERIT TRUE, SET TRUE;
GRANT cloud_agents_bootstrap_admin TO cag_bootstrap WITH INHERIT TRUE, SET TRUE;
SQL
)
  fi

  docker exec -i -e PGPASSWORD="$test_password" "$active_container" \
    psql -X -v ON_ERROR_STOP=1 -h 127.0.0.1 -U postgres -d postgres <<SQL >/dev/null
CREATE ROLE cag_db_owner NOLOGIN NOSUPERUSER NOINHERIT NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS;
CREATE ROLE cag_migration LOGIN NOSUPERUSER NOINHERIT NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS PASSWORD '$test_password';
CREATE ROLE cag_runtime LOGIN NOSUPERUSER INHERIT NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS PASSWORD '$test_password';
CREATE ROLE cag_bootstrap LOGIN NOSUPERUSER INHERIT NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS PASSWORD '$test_password';
$membership_grants
CREATE DATABASE cagtest OWNER cag_db_owner TEMPLATE template0 ENCODING 'UTF8' LC_COLLATE 'C' LC_CTYPE 'C';
SQL

  docker exec -e PGPASSWORD="$test_password" "$active_container" \
    psql -X -v ON_ERROR_STOP=1 -h 127.0.0.1 -U postgres -d postgres \
    -f /workspace/services/control-plane/migrations/bootstrap/roles.sql >/dev/null
  docker exec -e PGPASSWORD="$test_password" "$active_container" \
    psql -X -v ON_ERROR_STOP=1 -h 127.0.0.1 -U postgres -d cagtest \
    --set=cloud_agents_database=cagtest \
    --set=cloud_agents_database_owner=cag_db_owner \
    -f /workspace/services/control-plane/migrations/bootstrap/database.sql >/dev/null
  docker exec -e PGPASSWORD="$test_password" "$active_container" \
    psql -X -v ON_ERROR_STOP=1 -h 127.0.0.1 -U cag_migration -d cagtest \
    -c 'SET ROLE cloud_agents_migration_owner' \
    -f /workspace/services/control-plane/migrations/000001_expand_migration_kernel.sql \
    -f /workspace/services/control-plane/migrations/000002_expand_tenancy.sql >/dev/null

  for tenant in 001 002; do
    bootstrap_result=$(docker exec -e PGPASSWORD="$test_password" "$active_container" \
      psql -X -v ON_ERROR_STOP=1 -At -h 127.0.0.1 -U cag_bootstrap -d cagtest \
      -c "SELECT * FROM cloud_agents.bootstrap_platform_tenant('tenant-$tenant','tenant-$tenant','Tenant $tenant','audit-$tenant','bootstrap');")
    if [[ $bootstrap_result != "tenant-$tenant|1" ]]; then
      echo "Unexpected tenant bootstrap result: $bootstrap_result" >&2
      exit 1
    fi
  done

  preflight=$(docker exec -e PGPASSWORD="$test_password" "$active_container" \
    psql -X -v ON_ERROR_STOP=1 -At -h 127.0.0.1 -U cag_runtime -d cagtest \
    -c "SELECT current_setting('server_version_num'), current_setting('server_encoding'), d.datcollate, d.datctype, current_user, pg_has_role(current_user,'cloud_agents_runtime','USAGE') FROM pg_catalog.pg_database AS d WHERE d.datname = pg_catalog.current_database();")
  if [[ $preflight != "$expected_version|UTF8|C|C|cag_runtime|t" ]]; then
    echo "Unexpected PostgreSQL $postgres_major preflight: $preflight" >&2
    exit 1
  fi

  host_port=$(docker port "$active_container" 5432/tcp | sed -E 's/.*:([0-9]+)$/\1/')
  database_url="postgres://cag_runtime:$test_password@127.0.0.1:$host_port/cagtest?sslmode=disable"
  CLOUD_AGENTS_TEST_DATABASE_URL="$database_url" \
  CLOUD_AGENTS_REQUIRE_POSTGRES_TEST=1 \
  GOWORK=off GOTOOLCHAIN=local GOFLAGS=-mod=readonly \
    go -C "$module_dir" test \
      -run '^TestTenantTransactionRunnerPostgresConformance$' \
      -count=1 -v ./internal/store/postgres
  CLOUD_AGENTS_TEST_DATABASE_URL="$database_url" \
  CLOUD_AGENTS_REQUIRE_POSTGRES_TEST=1 \
  GOWORK=off GOTOOLCHAIN=local GOFLAGS=-mod=readonly \
    go -C "$module_dir" test -race \
      -run '^TestTenantTransactionRunnerPostgresConformance$' \
      -count=1 -v ./internal/store/postgres

  echo "PostgreSQL $postgres_major tenant helper matrix: PASS ($preflight)"
  cleanup
done

echo "Tenant helper PostgreSQL 15/16/17 local matrix: PASS"
