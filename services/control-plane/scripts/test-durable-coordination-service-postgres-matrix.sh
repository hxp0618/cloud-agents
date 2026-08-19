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

run_id="durable-coordination-service-$$-$RANDOM"
ownership_label="com.hxp0618.cloud-agents.test-run"
active_container=""
test_password=""

cleanup() {
  if [[ -n "$active_container" ]] && docker container inspect "$active_container" >/dev/null 2>&1; then
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

  active_container="cag-p1-coordination-service-pg${postgres_major}-$$-$RANDOM"
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
    000008_add_durable_coordination_service.sql; do
    docker exec -e PGPASSWORD="$test_password" "$active_container" \
      psql -X -v ON_ERROR_STOP=1 --single-transaction \
      -h 127.0.0.1 -U cag_migration -d cagtest \
      -c 'SET ROLE cloud_agents_migration_owner' \
      -f "/workspace/services/control-plane/migrations/$migration" >/dev/null
  done

  for run_mode in normal race; do
    tenant_id="tenant-coordination-$run_mode"
    bootstrap_result=$(docker exec -e PGPASSWORD="$test_password" "$active_container" \
      psql -X -v ON_ERROR_STOP=1 -At -h 127.0.0.1 -U cag_bootstrap -d cagtest \
      -c "SELECT * FROM cloud_agents.bootstrap_platform_tenant('$tenant_id','$tenant_id','Coordination $run_mode','audit-bootstrap-$run_mode','bootstrap');")
    if [[ $bootstrap_result != "$tenant_id|1" ]]; then
      echo "Unexpected tenant bootstrap result: $bootstrap_result" >&2
      exit 1
    fi
  done

  docker exec -i -e PGPASSWORD="$test_password" "$active_container" \
    psql -X -v ON_ERROR_STOP=1 -h 127.0.0.1 -U cag_migration -d cagtest <<'SQL' >/dev/null
SET ROLE cloud_agents_migration_owner;
BEGIN;
SET CONSTRAINTS ALL DEFERRED;
INSERT INTO cloud_agents.resource_changes (
    tenant_id, tenant_uid, resource_version, resource_kind, resource_uid,
    change_kind, actor_database_principal, occurred_at
)
SELECT tenant_id, tenant_id, resource_version, resource_kind, resource_uid,
    'created', 'cag_migration', occurred_at
FROM (VALUES
    ('tenant-coordination-normal', 2::bigint, 'organization'::text, 'organization-normal', '2026-08-19T12:00:00Z'::timestamptz),
    ('tenant-coordination-normal', 3::bigint, 'project'::text, 'project-normal', '2026-08-19T12:00:01Z'::timestamptz),
    ('tenant-coordination-normal', 4::bigint, 'membership'::text, 'membership-admin-normal', '2026-08-19T12:00:02Z'::timestamptz),
    ('tenant-coordination-normal', 5::bigint, 'role_binding'::text, 'role-binding-admin-normal', '2026-08-19T12:00:03Z'::timestamptz),
    ('tenant-coordination-race', 2::bigint, 'organization'::text, 'organization-race', '2026-08-19T12:00:00Z'::timestamptz),
    ('tenant-coordination-race', 3::bigint, 'project'::text, 'project-race', '2026-08-19T12:00:01Z'::timestamptz),
    ('tenant-coordination-race', 4::bigint, 'membership'::text, 'membership-admin-race', '2026-08-19T12:00:02Z'::timestamptz),
    ('tenant-coordination-race', 5::bigint, 'role_binding'::text, 'role-binding-admin-race', '2026-08-19T12:00:03Z'::timestamptz)
) AS seed(tenant_id, resource_version, resource_kind, resource_uid, occurred_at);

INSERT INTO cloud_agents.organizations (
    tenant_id, tenant_ref_id, organization_uid, organization_name, display_name,
    state, resource_version, created_at, updated_at
)
SELECT tenant_id, tenant_id, 'organization-' || run_mode, 'organization-' || run_mode,
    'Organization ' || run_mode, 'active', 2,
    '2026-08-19T12:00:00Z', '2026-08-19T12:00:00Z'
FROM (VALUES
    ('tenant-coordination-normal', 'normal'),
    ('tenant-coordination-race', 'race')
) AS seed(tenant_id, run_mode);

INSERT INTO cloud_agents.projects (
    tenant_id, tenant_ref_id, project_uid, project_name, organization_uid, display_name,
    state, resource_version, created_at, updated_at
)
SELECT tenant_id, tenant_id, 'project-' || run_mode, 'project-' || run_mode,
    'organization-' || run_mode, 'Project ' || run_mode, 'active', 3,
    '2026-08-19T12:00:01Z', '2026-08-19T12:00:01Z'
FROM (VALUES
    ('tenant-coordination-normal', 'normal'),
    ('tenant-coordination-race', 'race')
) AS seed(tenant_id, run_mode);

INSERT INTO cloud_agents.memberships (
    tenant_id, tenant_ref_id, membership_uid, membership_name,
    subject_kind, subject_issuer, subject_value, subject_digest,
    scope_level, scope_tenant_uid, scope_organization_uid, scope_project_uid,
    state, expires_at, resource_version, created_at, updated_at
)
SELECT tenant_id, tenant_id, 'membership-admin-' || run_mode, 'membership-admin-' || run_mode,
    'user', 'https://identity.example.test/', 'user-admin',
    'sha256:0403af0ad3826bcac1d20a2a58361e97d94508730794a9b0b1bf94bd7ab7b2bc',
    'tenant', tenant_id, NULL, NULL, 'active', NULL, 4,
    '2026-08-19T12:00:02Z', '2026-08-19T12:00:02Z'
FROM (VALUES
    ('tenant-coordination-normal', 'normal'),
    ('tenant-coordination-race', 'race')
) AS seed(tenant_id, run_mode);

INSERT INTO cloud_agents.role_bindings (
    tenant_id, tenant_ref_id, role_binding_uid, role_binding_name,
    subject_kind, subject_issuer, subject_value, subject_digest,
    role_name, role_version, scope_level,
    scope_tenant_uid, scope_organization_uid, scope_project_uid,
    state, expires_at, resource_version, created_at, updated_at
)
SELECT tenant_id, tenant_id, 'role-binding-admin-' || run_mode, 'role-binding-admin-' || run_mode,
    'user', 'https://identity.example.test/', 'user-admin',
    'sha256:0403af0ad3826bcac1d20a2a58361e97d94508730794a9b0b1bf94bd7ab7b2bc',
    'tenant.admin', 1, 'tenant', tenant_id, NULL, NULL,
    'active', NULL, 5, '2026-08-19T12:00:03Z', '2026-08-19T12:00:03Z'
FROM (VALUES
    ('tenant-coordination-normal', 'normal'),
    ('tenant-coordination-race', 'race')
) AS seed(tenant_id, run_mode);

UPDATE cloud_agents.tenant_resource_versions
SET current_revision = 5, updated_at = '2026-08-19T12:00:03Z'
WHERE tenant_id IN ('tenant-coordination-normal', 'tenant-coordination-race')
    AND tenant_uid = tenant_id;
COMMIT;
SQL

  host_port=$(docker port "$active_container" 5432/tcp | awk -F: 'NR == 1 { print $NF }')
  if [[ ! $host_port =~ ^[0-9]+$ ]]; then
    echo "Unable to resolve PostgreSQL host port: $host_port" >&2
    exit 1
  fi
  database_url="postgres://cag_runtime:$test_password@127.0.0.1:$host_port/cagtest?sslmode=disable"
  migration_database_url="postgres://cag_migration:$test_password@127.0.0.1:$host_port/cagtest?sslmode=disable"

  CLOUD_AGENTS_TEST_DATABASE_URL="$database_url" \
  CLOUD_AGENTS_TEST_MIGRATION_DATABASE_URL="$migration_database_url" \
  CLOUD_AGENTS_COORDINATION_TENANT_ID='tenant-coordination-normal' \
  CLOUD_AGENTS_COORDINATION_RUN_ID='normal' \
  CLOUD_AGENTS_REQUIRE_POSTGRES_TEST=1 \
  GOWORK=off GOTOOLCHAIN=local GOFLAGS=-mod=readonly \
    go -C "$module_dir" test \
      -run '^TestDurableCoordinationPostgresConformance$' \
      -count=1 -v ./internal/store/postgres

  docker exec -e PGPASSWORD="$test_password" "$active_container" \
    psql -X -v ON_ERROR_STOP=1 -h 127.0.0.1 -U cag_migration -d cagtest \
    -c 'SET ROLE cloud_agents_migration_owner; DELETE FROM cloud_agents.leader_leases WHERE leader_name = '\''outbox-dispatcher'\'';' \
    >/dev/null

  CLOUD_AGENTS_TEST_DATABASE_URL="$database_url" \
  CLOUD_AGENTS_TEST_MIGRATION_DATABASE_URL="$migration_database_url" \
  CLOUD_AGENTS_COORDINATION_TENANT_ID='tenant-coordination-race' \
  CLOUD_AGENTS_COORDINATION_RUN_ID='race' \
  CLOUD_AGENTS_REQUIRE_POSTGRES_TEST=1 \
  GOWORK=off GOTOOLCHAIN=local GOFLAGS=-mod=readonly \
    go -C "$module_dir" test -race \
      -run '^TestDurableCoordinationPostgresConformance$' \
      -count=1 -v ./internal/store/postgres

  expect_sql_failure cag_runtime \
    "INSERT INTO cloud_agents.outbox_events (tenant_id) VALUES ('tenant-coordination-normal')"
  expect_sql_failure cag_runtime \
    "SELECT cloud_agents.transition_outbox_claim('tenant-coordination-normal','event-normal-main','holder-normal','incarnation-normal','claim-normal-main',clock_timestamp(),'delivery_succeeded',NULL,'sha256:1111111111111111111111111111111111111111111111111111111111111111','audit-direct-transition')"
  expect_sql_failure cag_bootstrap \
    "SELECT cloud_agents.claim_managed_agent_create_project_idempotency('tenant-coordination-normal','sha256:1111111111111111111111111111111111111111111111111111111111111111','idempotency-direct-bootstrap','sha256:1111111111111111111111111111111111111111111111111111111111111111','audit-direct-bootstrap')"

  callable_boundary=$(docker exec -e PGPASSWORD="$test_password" "$active_container" \
    psql -X -At -v ON_ERROR_STOP=1 -h 127.0.0.1 -U postgres -d cagtest -c "SELECT
      (SELECT count(*) FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace
       WHERE n.nspname='cloud_agents' AND p.proname IN
         ('claim_managed_agent_create_project_idempotency','complete_managed_agent_create_project_success',
          'complete_managed_agent_create_project_failure','acquire_coordination_leader','renew_coordination_leader',
          'claim_outbox_event','acknowledge_outbox_event','retry_outbox_event','dead_letter_outbox_event',
          'reap_expired_outbox_claim')
         AND has_function_privilege('cloud_agents_runtime',p.oid,'EXECUTE')),
      (SELECT count(*) FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace
       WHERE n.nspname='cloud_agents' AND p.proname IN ('append_coordination_audit','transition_outbox_claim')
         AND has_function_privilege('cloud_agents_runtime',p.oid,'EXECUTE')),
      (SELECT count(*) FROM cloud_agents.platform_operations),
      (SELECT count(*) FROM cloud_agents.operation_finalizers);")
  if [[ $callable_boundary != "10|0|0|0" ]]; then
    echo "Durable coordination callable boundary drift: $callable_boundary" >&2
    exit 1
  fi

  echo "durable-coordination-service: PostgreSQL $postgres_major ($actual_version) normal/race/fault PASS"
  cleanup
done

echo "durable-coordination-service: PG15/16/17 normal/race/fault matrix PASS"
