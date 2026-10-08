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
    -f /workspace/services/control-plane/migrations/000002_expand_tenancy.sql \
    -f /workspace/services/control-plane/migrations/000003_expand_membership_rbac.sql \
    -f /workspace/services/control-plane/migrations/000004_expand_membership_rbac_mutations.sql \
    -f /workspace/services/control-plane/migrations/000005_close_membership_binding_authority.sql \
    -f /workspace/services/control-plane/migrations/000006_close_subject_issuer_validation.sql \
    -f /workspace/services/control-plane/migrations/000007_expand_durable_coordination_kernel.sql \
    -f /workspace/services/control-plane/migrations/000008_add_durable_coordination_service.sql \
    -f /workspace/services/control-plane/migrations/000009_redact_coordination_conflicts.sql \
    -f /workspace/services/control-plane/migrations/000010_expand_compatibility_recovery_kernel.sql \
    -f /workspace/services/control-plane/migrations/000011_add_compatibility_recovery_writer.sql \
    -f /workspace/services/control-plane/migrations/000012_fix_compatibility_recovery_preflight.sql \
    -f /workspace/services/control-plane/migrations/000013_add_durable_project_create_writer.sql \
    -f /workspace/services/control-plane/migrations/000014_harden_durable_project_create_identifiers.sql \
    -f /workspace/services/control-plane/migrations/000015_add_managed_agent_sessions.sql \
    -f /workspace/services/control-plane/migrations/000016_add_managed_agent_turns.sql \
    -f /workspace/services/control-plane/migrations/000017_add_managed_agent_executions.sql \
    -f /workspace/services/control-plane/migrations/000018_add_managed_agent_events.sql \
    -f /workspace/services/control-plane/migrations/000019_persist_managed_agent_execution_cancellation.sql \
    -f /workspace/services/control-plane/migrations/000020_persist_managed_agent_execution_interruption.sql \
    -f /workspace/services/control-plane/migrations/000021_add_managed_host_environment_leases.sql \
    -f /workspace/services/control-plane/migrations/000022_repair_managed_agent_lifecycle_transitions.sql \
    -f /workspace/services/control-plane/migrations/000023_scope_managed_agent_event_ids.sql \
    -f /workspace/services/control-plane/migrations/000024_persist_managed_agent_provider_resume_cursor.sql \
    -f /workspace/services/control-plane/migrations/000025_bootstrap_tenant_administrator.sql >/dev/null

  bootstrap_sql="SELECT * FROM cloud_agents.bootstrap_tenant_administrator_v1('tenant-001','tenant-001','Tenant 001','organization-001','organization-001','Organization 001','user','https://identity.example.test/','user-001','membership-admin-001','membership-admin-001','role-binding-admin-001','role-binding-admin-001','audit-tenant-001','audit-membership-001','audit-role-binding-001','bootstrap');"
  bootstrap_result=$(docker exec -e PGPASSWORD="$test_password" "$active_container" \
    psql -X -v ON_ERROR_STOP=1 -At -h 127.0.0.1 -U cag_bootstrap -d cagtest \
    -c "$bootstrap_sql")
  if [[ $bootstrap_result != "tenant-001|organization-001|membership-admin-001|role-binding-admin-001|4" ]]; then
    echo "Unexpected tenant administrator bootstrap result: $bootstrap_result" >&2
    exit 1
  fi
  replay_result=$(docker exec -e PGPASSWORD="$test_password" "$active_container" \
    psql -X -v ON_ERROR_STOP=1 -At -h 127.0.0.1 -U cag_bootstrap -d cagtest \
    -c "$bootstrap_sql")
  if [[ $replay_result != "$bootstrap_result" ]]; then
    echo "Tenant administrator bootstrap replay drifted: $replay_result" >&2
    exit 1
  fi
  if docker exec -e PGPASSWORD="$test_password" "$active_container" \
    psql -X -v ON_ERROR_STOP=1 -At -h 127.0.0.1 -U cag_runtime -d cagtest \
    -c "$bootstrap_sql" >/dev/null 2>&1; then
    echo "Runtime login unexpectedly executed tenant administrator bootstrap" >&2
    exit 1
  fi
  if docker exec -e PGPASSWORD="$test_password" "$active_container" \
    psql -X -v ON_ERROR_STOP=1 -At -h 127.0.0.1 -U cag_bootstrap -d cagtest \
    -c "${bootstrap_sql/Organization 001/Conflicting Organization}" >/dev/null 2>&1; then
    echo "Conflicting tenant administrator bootstrap unexpectedly succeeded" >&2
    exit 1
  fi

  bootstrap_result=$(docker exec -e PGPASSWORD="$test_password" "$active_container" \
    psql -X -v ON_ERROR_STOP=1 -At -h 127.0.0.1 -U cag_bootstrap -d cagtest \
    -c "SELECT * FROM cloud_agents.bootstrap_platform_tenant('tenant-002','tenant-002','Tenant 002','audit-tenant-002','bootstrap');")
  if [[ $bootstrap_result != "tenant-002|1" ]]; then
    echo "Unexpected tenant bootstrap result: $bootstrap_result" >&2
    exit 1
  fi

  bootstrap_facts=$(docker exec -e PGPASSWORD="$test_password" "$active_container" \
    psql -X -v ON_ERROR_STOP=1 -At -h 127.0.0.1 -U cag_runtime -d cagtest \
    -c "SELECT pg_catalog.set_config('cloud_agents.tenant_id','tenant-001',false); SELECT revision.current_revision, (SELECT count(*) FROM cloud_agents.organizations), (SELECT count(*) FROM cloud_agents.memberships), (SELECT count(*) FROM cloud_agents.role_bindings), (SELECT count(*) FROM cloud_agents.builtin_role_permissions AS permission WHERE permission.role_name = 'tenant.admin' AND permission.permission = 'projects.create') FROM cloud_agents.tenant_resource_versions AS revision WHERE revision.tenant_id = 'tenant-001';" | tail -1)
  if [[ $bootstrap_facts != "4|1|1|1|1" ]]; then
    echo "Unexpected tenant administrator bootstrap facts: $bootstrap_facts" >&2
    exit 1
  fi

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
  GOTOOLCHAIN=local GOFLAGS=-mod=readonly \
    go -C "$module_dir" test \
      -run '^TestTenantTransactionRunnerPostgresConformance$' \
      -count=1 -v ./internal/store/postgres
  CLOUD_AGENTS_TEST_DATABASE_URL="$database_url" \
  CLOUD_AGENTS_REQUIRE_POSTGRES_TEST=1 \
  GOTOOLCHAIN=local GOFLAGS=-mod=readonly \
    go -C "$module_dir" test -race \
      -run '^TestTenantTransactionRunnerPostgresConformance$' \
      -count=1 -v ./internal/store/postgres

  echo "PostgreSQL $postgres_major tenant helper matrix: PASS ($preflight)"
  cleanup
done

echo "Tenant helper PostgreSQL 15/16/17 local matrix: PASS"
