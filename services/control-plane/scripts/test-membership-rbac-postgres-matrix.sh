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

run_id="membership-rbac-matrix-$$-$RANDOM"
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

  active_container="cag-p1-rbac-pg${postgres_major}-$$-$RANDOM"
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
  for migration in \
    000001_expand_migration_kernel.sql \
    000002_expand_tenancy.sql \
    000003_expand_membership_rbac.sql \
    000004_expand_membership_rbac_mutations.sql; do
    docker exec -e PGPASSWORD="$test_password" "$active_container" \
      psql -X -v ON_ERROR_STOP=1 --single-transaction \
      -h 127.0.0.1 -U cag_migration -d cagtest \
      -c 'SET ROLE cloud_agents_migration_owner' \
      -f "/workspace/services/control-plane/migrations/$migration" >/dev/null
  done

  for tenant in 001 002 mutation-normal mutation-race; do
    bootstrap_result=$(docker exec -e PGPASSWORD="$test_password" "$active_container" \
      psql -X -v ON_ERROR_STOP=1 -At -h 127.0.0.1 -U cag_bootstrap -d cagtest \
      -c "SELECT * FROM cloud_agents.bootstrap_platform_tenant('tenant-$tenant','tenant-$tenant','Tenant $tenant','audit-$tenant','bootstrap');")
    if [[ $bootstrap_result != "tenant-$tenant|1" ]]; then
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
SELECT
    tenant_id, tenant_id, resource_version, resource_kind, resource_uid,
    'created', 'cag_migration', occurred_at
FROM (VALUES
    ('tenant-mutation-normal', 2::bigint, 'membership'::text, 'membership-admin', '2026-08-17T09:00:00Z'::timestamptz),
    ('tenant-mutation-normal', 3::bigint, 'role_binding'::text, 'role-binding-admin', '2026-08-17T09:00:01Z'::timestamptz),
    ('tenant-mutation-race', 2::bigint, 'membership'::text, 'membership-admin', '2026-08-17T09:00:00Z'::timestamptz),
    ('tenant-mutation-race', 3::bigint, 'role_binding'::text, 'role-binding-admin', '2026-08-17T09:00:01Z'::timestamptz)
) AS seed(tenant_id, resource_version, resource_kind, resource_uid, occurred_at);
INSERT INTO cloud_agents.memberships (
    tenant_id, tenant_ref_id, membership_uid, membership_name,
    subject_kind, subject_issuer, subject_value, subject_digest,
    scope_level, scope_tenant_uid, scope_organization_uid, scope_project_uid,
    state, expires_at, resource_version, created_at, updated_at
)
SELECT
    tenant_id, tenant_id, 'membership-admin', 'membership-admin',
    'user', 'https://identity.example.test/', 'user-admin',
    'sha256:0403af0ad3826bcac1d20a2a58361e97d94508730794a9b0b1bf94bd7ab7b2bc',
    'tenant', tenant_id, NULL, NULL, 'active', NULL, 2,
    '2026-08-17T09:00:00Z', '2026-08-17T09:00:00Z'
FROM (VALUES ('tenant-mutation-normal'), ('tenant-mutation-race')) AS seed(tenant_id);
INSERT INTO cloud_agents.role_bindings (
    tenant_id, tenant_ref_id, role_binding_uid, role_binding_name,
    subject_kind, subject_issuer, subject_value, subject_digest,
    role_name, role_version, scope_level,
    scope_tenant_uid, scope_organization_uid, scope_project_uid,
    state, expires_at, resource_version, created_at, updated_at
)
SELECT
    tenant_id, tenant_id, 'role-binding-admin', 'role-binding-admin',
    'user', 'https://identity.example.test/', 'user-admin',
    'sha256:0403af0ad3826bcac1d20a2a58361e97d94508730794a9b0b1bf94bd7ab7b2bc',
    'tenant.admin', 1, 'tenant', tenant_id, NULL, NULL,
    'active', NULL, 3, '2026-08-17T09:00:01Z', '2026-08-17T09:00:01Z'
FROM (VALUES ('tenant-mutation-normal'), ('tenant-mutation-race')) AS seed(tenant_id);
UPDATE cloud_agents.tenant_resource_versions
SET current_revision = 3, updated_at = '2026-08-17T09:00:01Z'
WHERE tenant_id IN ('tenant-mutation-normal', 'tenant-mutation-race')
    AND tenant_uid = tenant_id;
COMMIT;
SQL

  docker exec -i -e PGPASSWORD="$test_password" "$active_container" \
    psql -X -v ON_ERROR_STOP=1 -h 127.0.0.1 -U cag_migration -d cagtest <<'SQL' >/dev/null
SET ROLE cloud_agents_migration_owner;
BEGIN;
SET CONSTRAINTS ALL DEFERRED;
INSERT INTO cloud_agents.resource_changes (
    tenant_id, tenant_uid, resource_version, resource_kind, resource_uid,
    change_kind, actor_database_principal, occurred_at
) VALUES
    ('tenant-001', 'tenant-001', 2, 'organization', 'organization-alpha', 'created', 'cag_migration', '2026-08-17T10:00:00Z'),
    ('tenant-001', 'tenant-001', 3, 'project', 'project-alpha', 'created', 'cag_migration', '2026-08-17T10:00:01Z'),
    ('tenant-001', 'tenant-001', 4, 'membership', 'membership-alpha', 'created', 'cag_migration', '2026-08-17T10:00:02Z'),
    ('tenant-001', 'tenant-001', 5, 'role_binding', 'role-binding-alpha', 'created', 'cag_migration', '2026-08-17T10:00:03Z'),
    ('tenant-001', 'tenant-001', 6, 'membership', 'membership-broad', 'created', 'cag_migration', '2026-08-17T10:00:04Z');
INSERT INTO cloud_agents.organizations (
    tenant_id, tenant_ref_id, organization_uid, organization_name, display_name,
    state, resource_version, created_at, updated_at
) VALUES (
    'tenant-001', 'tenant-001', 'organization-alpha', 'organization-alpha', 'Organization Alpha',
    'active', 2, '2026-08-17T10:00:00Z', '2026-08-17T10:00:00Z'
);
INSERT INTO cloud_agents.projects (
    tenant_id, tenant_ref_id, project_uid, project_name, organization_uid, display_name,
    state, resource_version, created_at, updated_at
) VALUES (
    'tenant-001', 'tenant-001', 'project-alpha', 'project-alpha', 'organization-alpha', 'Project Alpha',
    'active', 3, '2026-08-17T10:00:01Z', '2026-08-17T10:00:01Z'
);
INSERT INTO cloud_agents.memberships (
    tenant_id, tenant_ref_id, membership_uid, membership_name,
    subject_kind, subject_issuer, subject_value, subject_digest,
    scope_level, scope_tenant_uid, scope_organization_uid, scope_project_uid,
    state, expires_at, resource_version, created_at, updated_at
) VALUES
    (
        'tenant-001', 'tenant-001', 'membership-alpha', 'membership-alpha',
        'user', 'https://identity.example.test/', 'user-alpha',
        'sha256:53bd6637a4285997d6b72abdaf8547a89293be1dc8e852630460a5ca091b47d7',
        'organization', NULL, 'organization-alpha', NULL,
        'active', NULL, 4, '2026-08-17T10:00:02Z', '2026-08-17T10:00:02Z'
    ),
    (
        'tenant-001', 'tenant-001', 'membership-broad', 'membership-broad',
        'user', 'https://identity.example.test/', 'user-alpha',
        'sha256:53bd6637a4285997d6b72abdaf8547a89293be1dc8e852630460a5ca091b47d7',
        'tenant', 'tenant-001', NULL, NULL,
        'suspended', NULL, 6, '2026-08-17T10:00:04Z', '2026-08-17T10:00:04Z'
    );
INSERT INTO cloud_agents.role_bindings (
    tenant_id, tenant_ref_id, role_binding_uid, role_binding_name,
    subject_kind, subject_issuer, subject_value, subject_digest,
    role_name, role_version,
    scope_level, scope_tenant_uid, scope_organization_uid, scope_project_uid,
    state, expires_at, resource_version, created_at, updated_at
) VALUES (
    'tenant-001', 'tenant-001', 'role-binding-alpha', 'role-binding-alpha',
    'user', 'https://identity.example.test/', 'user-alpha',
    'sha256:53bd6637a4285997d6b72abdaf8547a89293be1dc8e852630460a5ca091b47d7',
    'project.viewer', 1,
    'project', NULL, NULL, 'project-alpha',
    'active', NULL, 5, '2026-08-17T10:00:03Z', '2026-08-17T10:00:03Z'
);
UPDATE cloud_agents.tenant_resource_versions
SET current_revision = 6, updated_at = '2026-08-17T10:00:04Z'
WHERE tenant_id = 'tenant-001' AND tenant_uid = 'tenant-001';
COMMIT;
SQL

  preflight=$(docker exec -e PGPASSWORD="$test_password" "$active_container" \
    psql -X -v ON_ERROR_STOP=1 -At -h 127.0.0.1 -U cag_runtime -d cagtest \
    -c "SELECT current_setting('server_version_num'), current_setting('server_encoding'), d.datcollate, d.datctype, current_user, pg_has_role(current_user,'cloud_agents_runtime','USAGE'), (SELECT count(*) FROM cloud_agents.builtin_roles), (SELECT count(*) FROM cloud_agents.builtin_role_permissions) FROM pg_catalog.pg_database AS d WHERE d.datname = pg_catalog.current_database();")
  if [[ $preflight != "$expected_version|UTF8|C|C|cag_runtime|t|7|141" ]]; then
    echo "Unexpected PostgreSQL $postgres_major RBAC preflight: $preflight" >&2
    exit 1
  fi

  host_port=$(docker port "$active_container" 5432/tcp | sed -E 's/.*:([0-9]+)$/\1/')
  database_url="postgres://cag_runtime:$test_password@127.0.0.1:$host_port/cagtest?sslmode=disable"
  migration_database_url="postgres://cag_migration:$test_password@127.0.0.1:$host_port/cagtest?sslmode=disable"
  CLOUD_AGENTS_TEST_DATABASE_URL="$database_url" \
  CLOUD_AGENTS_REQUIRE_POSTGRES_TEST=1 \
  GOWORK=off GOTOOLCHAIN=local GOFLAGS=-mod=readonly \
    go -C "$module_dir" test \
      -run '^TestTenantAuthorizationPostgresConformance$' \
      -count=1 -v ./internal/store/postgres
  CLOUD_AGENTS_TEST_DATABASE_URL="$database_url" \
  CLOUD_AGENTS_TEST_MIGRATION_DATABASE_URL="$migration_database_url" \
  CLOUD_AGENTS_MUTATION_TENANT_ID='tenant-mutation-normal' \
  CLOUD_AGENTS_REQUIRE_POSTGRES_TEST=1 \
  GOWORK=off GOTOOLCHAIN=local GOFLAGS=-mod=readonly \
    go -C "$module_dir" test \
      -run '^TestTenantRBACMutationPostgresConformance$' \
      -count=1 -v ./internal/store/postgres
  CLOUD_AGENTS_TEST_DATABASE_URL="$database_url" \
  CLOUD_AGENTS_REQUIRE_POSTGRES_TEST=1 \
  GOWORK=off GOTOOLCHAIN=local GOFLAGS=-mod=readonly \
    go -C "$module_dir" test -race \
      -run '^TestTenantAuthorizationPostgresConformance$' \
      -count=1 -v ./internal/store/postgres
  CLOUD_AGENTS_TEST_DATABASE_URL="$database_url" \
  CLOUD_AGENTS_TEST_MIGRATION_DATABASE_URL="$migration_database_url" \
  CLOUD_AGENTS_MUTATION_TENANT_ID='tenant-mutation-race' \
  CLOUD_AGENTS_REQUIRE_POSTGRES_TEST=1 \
  GOWORK=off GOTOOLCHAIN=local GOFLAGS=-mod=readonly \
    go -C "$module_dir" test -race \
      -run '^TestTenantRBACMutationPostgresConformance$' \
      -count=1 -v ./internal/store/postgres

  cross_tenant_fault=$(docker exec -i -e PGPASSWORD="$test_password" "$active_container" \
    psql -X -v ON_ERROR_STOP=0 -At -h 127.0.0.1 -U cag_runtime -d cagtest 2>&1 <<'SQL'
BEGIN;
SELECT pg_catalog.set_config('cloud_agents.tenant_id', 'tenant-002', true);
SELECT * FROM cloud_agents.create_membership(
    'tenant-mutation-normal', 10, 'direct-cross-tenant', 'direct-cross-tenant',
    'user', 'https://identity.example.test/', 'direct-cross-tenant',
    'tenant', 'tenant-mutation-normal', NULL,
    'audit-direct-cross-tenant', 'conformance'
);
ROLLBACK;
SQL
)
  if [[ $cross_tenant_fault != *"ERROR:  membership create input is invalid"* ]]; then
    echo "Direct cross-tenant mutation did not fail closed:" >&2
    echo "$cross_tenant_fault" >&2
    exit 1
  fi

  if [[ $postgres_major -eq 15 ]]; then
    rogue_membership_grant='GRANT cloud_agents_runtime TO cag_runtime_rogue;'
  else
    rogue_membership_grant='GRANT cloud_agents_runtime TO cag_runtime_rogue WITH INHERIT TRUE, SET TRUE;'
  fi
  docker exec -i -e PGPASSWORD="$test_password" "$active_container" \
    psql -X -v ON_ERROR_STOP=1 -h 127.0.0.1 -U postgres -d cagtest <<SQL >/dev/null
CREATE ROLE cag_runtime_rogue LOGIN NOSUPERUSER INHERIT NOCREATEDB CREATEROLE NOREPLICATION NOBYPASSRLS PASSWORD '$test_password';
$rogue_membership_grant
SQL
  rogue_member_fault=$(docker exec -i -e PGPASSWORD="$test_password" "$active_container" \
    psql -X -v ON_ERROR_STOP=0 -At -h 127.0.0.1 -U cag_runtime -d cagtest 2>&1 <<'SQL'
BEGIN;
SELECT pg_catalog.set_config('cloud_agents.tenant_id', 'tenant-mutation-normal', true);
SELECT * FROM cloud_agents.create_membership(
    'tenant-mutation-normal', 10, 'unsafe-member-denied', 'unsafe-member-denied',
    'user', 'https://identity.example.test/', 'unsafe-member-denied',
    'tenant', 'tenant-mutation-normal', NULL,
    'audit-unsafe-member-denied', 'conformance'
);
ROLLBACK;
SQL
)
  docker exec -i -e PGPASSWORD="$test_password" "$active_container" \
    psql -X -v ON_ERROR_STOP=1 -h 127.0.0.1 -U postgres -d cagtest <<'SQL' >/dev/null
REVOKE cloud_agents_runtime FROM cag_runtime_rogue;
DROP ROLE cag_runtime_rogue;
SQL
  if [[ $rogue_member_fault != *"ERROR:  runtime mutation group has unsafe member cag_runtime_rogue"* ]]; then
    echo "Unsafe runtime-group member did not fence mutation authority:" >&2
    echo "$rogue_member_fault" >&2
    exit 1
  fi

  echo "PostgreSQL $postgres_major membership RBAC matrix: PASS ($preflight)"
  cleanup
done

echo "Membership RBAC PostgreSQL 15/16/17 local matrix: PASS"
