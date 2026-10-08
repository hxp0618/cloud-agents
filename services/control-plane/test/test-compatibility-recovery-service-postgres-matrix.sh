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

run_id="compatibility-recovery-service-$$-$RANDOM"
ownership_label="com.hxp0618.cloud-agents.test-run"
active_container=""
test_password=""

cleanup() {
  if [[ -n "$active_container" ]] && docker container inspect "$active_container" >/dev/null 2>&1; then
    observed_owner=$(docker container inspect --format "{{index .Config.Labels \"$ownership_label\"}}" "$active_container")
    if [[ $observed_owner != "$run_id" ]]; then
      echo "Refusing to remove container not owned by this run: $active_container" >&2
      return 1
    fi
    docker rm -f "$active_container" >/dev/null
  fi
  active_container=""
}

on_signal() {
  local exit_code=$1
  trap - EXIT INT TERM
  cleanup || true
  exit "$exit_code"
}

expect_sql_failure() {
  local user=$1
  local sql=$2
  if docker exec -e PGPASSWORD="$test_password" "$active_container" \
    psql -X -v ON_ERROR_STOP=1 -h 127.0.0.1 -U "$user" -d cagtest -c "$sql" \
    >/dev/null 2>&1; then
    echo "Expected SQL failure for $user: $sql" >&2
    exit 1
  fi
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

  active_container="cag-p1-compatibility-service-pg${postgres_major}-$$-$RANDOM"
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
    if docker exec "$active_container" pg_isready -h 127.0.0.1 -U postgres -d postgres >/dev/null 2>&1; then
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

  actual_version=$(docker exec -e PGPASSWORD="$test_password" "$active_container" \
    psql -X -At -h 127.0.0.1 -U postgres -d postgres -c 'SHOW server_version_num')
  if [[ $actual_version != "$expected_version" ]]; then
    echo "Unexpected PostgreSQL version: expected $expected_version, got $actual_version" >&2
    exit 1
  fi

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

  for migration in \
    000001_expand_migration_kernel.sql \
    000002_expand_tenancy.sql \
    000003_expand_membership_rbac.sql \
    000004_expand_membership_rbac_mutations.sql \
    000005_close_membership_binding_authority.sql \
    000006_close_subject_issuer_validation.sql \
    000007_expand_durable_coordination_kernel.sql \
    000008_add_durable_coordination_service.sql \
    000009_redact_coordination_conflicts.sql \
    000010_expand_compatibility_recovery_kernel.sql \
    000011_add_compatibility_recovery_writer.sql; do
    docker exec -e PGPASSWORD="$test_password" "$active_container" \
      psql -X -v ON_ERROR_STOP=1 --single-transaction \
      -h 127.0.0.1 -U cag_migration -d cagtest \
      -c 'SET ROLE cloud_agents_migration_owner' \
      -f "/workspace/services/control-plane/migrations/$migration" >/dev/null
  done

  for run_mode in normal race; do
    tenant_id="tenant-compatibility-${postgres_major}-${run_mode}"
    bootstrap_result=$(docker exec -e PGPASSWORD="$test_password" "$active_container" \
      psql -X -v ON_ERROR_STOP=1 -At -h 127.0.0.1 -U cag_bootstrap -d cagtest \
      -c "SELECT * FROM cloud_agents.bootstrap_platform_tenant('$tenant_id','$tenant_id','Compatibility $run_mode','audit-bootstrap-$run_mode','bootstrap');")
    if [[ $bootstrap_result != "$tenant_id|1" ]]; then
      echo "Unexpected tenant bootstrap result: $bootstrap_result" >&2
      exit 1
    fi
  done

  expect_sql_failure cag_runtime \
    "SET cloud_agents.tenant_id='tenant-compatibility-${postgres_major}-normal'; INSERT INTO cloud_agents.compatibility_recovery_live_instances_v2(tenant_id) VALUES ('tenant-compatibility-${postgres_major}-normal');"
  expect_sql_failure cag_bootstrap \
    "SET cloud_agents.tenant_id='tenant-compatibility-${postgres_major}-normal'; SELECT * FROM cloud_agents.compatibility_recovery_live_instance_reconcile_v2('tenant-compatibility-${postgres_major}-normal','control-plane','missing',1,1,'sha256:$(printf '1%.0s' {1..64})');"

  bootstrap_probe=$(docker exec -e PGPASSWORD="$test_password" "$active_container" \
    psql -X -v ON_ERROR_STOP=1 -At -h 127.0.0.1 -U cag_bootstrap -d cagtest \
    -c "SET cloud_agents.tenant_id='tenant-compatibility-${postgres_major}-normal'; SELECT result_code FROM cloud_agents.compatibility_recovery_workload_principal_reconcile_v2('tenant-compatibility-${postgres_major}-normal','probe','postgres','sha256:$(printf '2%.0s' {1..64})');")
  if [[ $bootstrap_probe != $'SET\nnot_observed' ]]; then
    echo "Unexpected bootstrap compatibility probe: $bootstrap_probe" >&2
    exit 1
  fi

  host_port=$(docker inspect --format='{{(index (index .NetworkSettings.Ports "5432/tcp") 0).HostPort}}' "$active_container")
  bootstrap_url="postgres://cag_bootstrap:${test_password}@127.0.0.1:${host_port}/cagtest?sslmode=disable"
  runtime_url="postgres://cag_runtime:${test_password}@127.0.0.1:${host_port}/cagtest?sslmode=disable"
  migration_url="postgres://cag_migration:${test_password}@127.0.0.1:${host_port}/cagtest?sslmode=disable"

  for run_mode in normal race; do
    tenant_id="tenant-compatibility-${postgres_major}-${run_mode}"
    test_command=(go test ./internal/store/postgres -run '^TestCompatibilityRecoveryPostgresConformance$' -count=1)
    if [[ $run_mode == race ]]; then
      test_command=(go test -race ./internal/store/postgres -run '^TestCompatibilityRecoveryPostgresConformance$' -count=1)
    fi
    (
      cd "$module_dir"
      env \
        GOWORK=off \
        GOTOOLCHAIN=local \
        GOFLAGS=-mod=readonly \
        CLOUD_AGENTS_REQUIRE_COMPATIBILITY_POSTGRES_TEST=1 \
        CLOUD_AGENTS_COMPATIBILITY_BOOTSTRAP_DATABASE_URL="$bootstrap_url" \
        CLOUD_AGENTS_COMPATIBILITY_RUNTIME_DATABASE_URL="$runtime_url" \
        CLOUD_AGENTS_COMPATIBILITY_MIGRATION_DATABASE_URL="$migration_url" \
        CLOUD_AGENTS_COMPATIBILITY_RUN_ID="$run_mode" \
        CLOUD_AGENTS_COMPATIBILITY_TENANT_ID="$tenant_id" \
        CLOUD_AGENTS_COMPATIBILITY_POSTGRES_MAJOR="$postgres_major" \
        "${test_command[@]}"
    )
  done

  cleanup
done

echo "compatibility-recovery-service-postgres-matrix: PASS"
