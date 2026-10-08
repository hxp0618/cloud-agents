#!/usr/bin/env bash

set -euo pipefail

script_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
repo_root=$(cd -- "$script_dir/../../.." && pwd -P)
module_dir="$repo_root/services/control-plane"

if ! docker version >/dev/null 2>&1; then
  echo "A running Docker daemon is required" >&2
  exit 1
fi

declare -a matrix=(
  "15|150018|postgres@sha256:6eb0add3b77c081df18aa518ce43df58fdcc40f2e6d868a6fd08038dc7acd425"
  "16|160014|postgres@sha256:95206741a5b214807675e14165369d05b93a9cf692223b616d07cca227e74b0b"
  "17|170010|postgres@sha256:742f40ea20b9ff2ff31db5458d127452988a2164df9e17441e191f3b72252193"
)

registry_digest="sha256:11c0f599e8320668a6f601241206c795933b26e3b9c456a58353a0d13c7ecd30"
state_machine_digest="sha256:5c4fa5c0cfac253b45a41c2e49ee7e863b9efbe124e5d743e041f5e01f5c6f15"
policy_digest="sha256:95023973eb007a958a3c5aea3ac61b6caa7cd8955b9a24fcef3ad269230c64e8"
profile_id="managedAgentCreateProject/v1alpha1"
profile_digest="sha256:059b4cca58f9621e9b70b723fb3b681f62948d6d4965af60105165afce680d5a"

run_id="durable-coordination-kernel-$$-$RANDOM"
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

  active_container="cag-p1-coordination-pg${postgres_major}-$$-$RANDOM"
  test_password="cag-local-only-${postgres_major}-$$"
  docker run -d \
    --pull=never \
    --name "$active_container" \
    --label "$ownership_label=$run_id" \
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
    000007_expand_durable_coordination_kernel.sql; do
    docker exec -e PGPASSWORD="$test_password" "$active_container" \
      psql -X -v ON_ERROR_STOP=1 --single-transaction \
      -h 127.0.0.1 -U cag_migration -d cagtest \
      -c 'SET ROLE cloud_agents_migration_owner' \
      -f "/workspace/services/control-plane/migrations/$migration" >/dev/null
  done

  for tenant in tenant-a tenant-b; do
    result=$(docker exec -e PGPASSWORD="$test_password" "$active_container" \
      psql -X -At -v ON_ERROR_STOP=1 -h 127.0.0.1 -U cag_bootstrap -d cagtest \
      -c "SELECT * FROM cloud_agents.bootstrap_platform_tenant('$tenant','$tenant','Tenant ${tenant#tenant-}','audit-$tenant','bootstrap');")
    if [[ $result != "$tenant|1" ]]; then
      echo "Unexpected tenant bootstrap result: $result" >&2
      exit 1
    fi
  done

  docker exec -i -e PGPASSWORD="$test_password" "$active_container" \
    psql -X -v ON_ERROR_STOP=1 -h 127.0.0.1 -U cag_migration -d cagtest <<SQL >/dev/null
SET ROLE cloud_agents_migration_owner;
BEGIN;
SET CONSTRAINTS ALL DEFERRED;
INSERT INTO cloud_agents.resource_changes (
    tenant_id, tenant_uid, resource_version, resource_kind, resource_uid,
    change_kind, actor_database_principal, occurred_at
) VALUES
    ('tenant-a', 'tenant-a', 2, 'project', 'project-a', 'created', 'cag_migration', '2026-08-19T00:00:00Z'),
    ('tenant-b', 'tenant-b', 2, 'project', 'project-b', 'created', 'cag_migration', '2026-08-19T00:00:00Z');
INSERT INTO cloud_agents.idempotency_records (
    tenant_id, tenant_ref_id, subject_digest, registry_digest, profile_id, profile_digest,
    idempotency_key, request_digest, state, resource_kind, resource_id, resource_version,
    created_at, updated_at, expires_at, terminal_at
) VALUES (
    'tenant-a', 'tenant-a', 'sha256:${registry_digest#sha256:}', '$registry_digest',
    '$profile_id', '$profile_digest', 'idem-tenant-a-0001',
    'sha256:${state_machine_digest#sha256:}', 'succeeded', 'project', 'project-a', 2,
    '2026-08-19T00:00:00Z', '2026-08-19T00:00:00Z', '2026-08-20T00:00:00Z',
    '2026-08-19T00:00:00Z'
);
INSERT INTO cloud_agents.outbox_events (
    tenant_id, tenant_ref_id, event_id, registry_digest, profile_id, profile_digest,
    event_class, aggregate_kind, aggregate_id, aggregate_sequence, resource_version,
    generation, operation_id, operation_generation, payload_digest, state, delivery_attempts
) VALUES (
    'tenant-a', 'tenant-a', 'event-tenant-a-0001', '$registry_digest', '$profile_id',
    '$profile_digest', 'resource_change', 'project', 'project-a', 2, 2,
    0, NULL, NULL, 'sha256:${policy_digest#sha256:}', 'pending', 0
);
INSERT INTO cloud_agents.coordination_audit_facts (
    tenant_id, tenant_ref_id, audit_fact_id, registry_digest, profile_id, profile_digest,
    subject_digest, resource_kind, resource_id, resource_version, transition, outcome
) VALUES (
    'tenant-a', 'tenant-a', 'audit-coordination-a-0001', '$registry_digest', '$profile_id',
    '$profile_digest', 'sha256:${registry_digest#sha256:}', 'project', 'project-a', 2,
    'resource_created', 'succeeded'
);
INSERT INTO cloud_agents.leader_leases (
    leader_name, holder_id, holder_incarnation, fencing_token,
    lease_started_at, lease_expires_at, updated_at
) VALUES
    ('coordination-reconciler', 'holder-a', 'incarnation-a', 1,
     '2026-08-19T00:00:00Z', '2026-08-19T00:00:01Z', '2026-08-19T00:00:00Z'),
    ('finalizer-reconciler', 'holder-b', 'incarnation-b', 1,
     '2026-08-19T00:00:00Z', '2026-08-19T00:01:00Z', '2026-08-19T00:00:00Z');
COMMIT;
SQL

  helper_result=$(docker exec -e PGPASSWORD="$test_password" "$active_container" \
    psql -X -At -v ON_ERROR_STOP=1 -h 127.0.0.1 -U cag_runtime -d cagtest \
    -c "SELECT cloud_agents.coordination_registry_digest(), cloud_agents.coordination_state_machine_digest(), cloud_agents.coordination_policy_digest(), cloud_agents.coordination_profile_is_registered('$profile_id','$profile_digest'), cloud_agents.coordination_profile_creates_operation('$profile_id','$profile_digest'), cloud_agents.coordination_profile_outbox_class('$profile_id','$profile_digest'), cloud_agents.coordination_profile_replay_ttl_seconds('$profile_id','$profile_digest');")
  expected_helpers="$registry_digest|$state_machine_digest|$policy_digest|t|f|resource_change|86400"
  if [[ $helper_result != "$expected_helpers" ]]; then
    echo "Generated-profile helper drift: $helper_result" >&2
    exit 1
  fi

  catalog_result=$(docker exec -i -e PGPASSWORD="$test_password" "$active_container" \
    psql -X -At -v ON_ERROR_STOP=1 -h 127.0.0.1 -U postgres -d cagtest <<'SQL'
SELECT
  (SELECT count(*) FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
   WHERE n.nspname = 'cloud_agents'
     AND c.relname IN ('platform_operations','operation_attempts','terminal_receipts','operation_finalizers','idempotency_records','outbox_events','coordination_audit_facts','leader_leases')
     AND pg_catalog.pg_get_userbyid(c.relowner) = 'cloud_agents_migration_owner'),
  (SELECT count(*) FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
   WHERE n.nspname = 'cloud_agents'
     AND c.relname IN ('platform_operations','operation_attempts','terminal_receipts','operation_finalizers','idempotency_records','outbox_events','coordination_audit_facts')
     AND c.relrowsecurity AND c.relforcerowsecurity),
  (SELECT count(*) FROM (VALUES
     ('platform_operations'),('operation_attempts'),('terminal_receipts'),('operation_finalizers'),
     ('idempotency_records'),('outbox_events'),('coordination_audit_facts'),('leader_leases')
   ) AS tables(name)
   WHERE has_table_privilege('cloud_agents_runtime', 'cloud_agents.' || name, 'SELECT')
     AND NOT has_table_privilege('cloud_agents_runtime', 'cloud_agents.' || name, 'INSERT,UPDATE,DELETE,TRUNCATE')
     AND NOT has_table_privilege('cloud_agents_bootstrap_admin', 'cloud_agents.' || name, 'SELECT,INSERT,UPDATE,DELETE,TRUNCATE')),
  (SELECT count(*) FROM (VALUES
     ('cloud_agents.coordination_registry_digest()'),
     ('cloud_agents.coordination_state_machine_digest()'),
     ('cloud_agents.coordination_policy_digest()'),
     ('cloud_agents.coordination_profile_is_registered(text,text)'),
     ('cloud_agents.coordination_profile_creates_operation(text,text)'),
     ('cloud_agents.coordination_profile_outbox_class(text,text)'),
     ('cloud_agents.coordination_profile_replay_ttl_seconds(text,text)')
   ) AS functions(signature)
   WHERE has_function_privilege('cloud_agents_runtime', signature, 'EXECUTE')
     AND NOT has_function_privilege('cloud_agents_bootstrap_admin', signature, 'EXECUTE')),
  (SELECT count(*) FROM information_schema.columns
   WHERE table_schema = 'cloud_agents'
     AND table_name IN ('terminal_receipts','idempotency_records','outbox_events','coordination_audit_facts')
     AND column_name IN ('authorization','cookie','credential','pairing_token','pairing_url','private_key','provider_payload','raw_request','raw_response','refresh_token','secret','token_hash'));
SQL
)
  if [[ $catalog_result != "8|7|8|7|0" ]]; then
    echo "Coordination catalog/ACL boundary drift: $catalog_result" >&2
    exit 1
  fi

  tenant_a=$(docker exec -e PGPASSWORD="$test_password" "$active_container" \
    psql -X -Atq -v ON_ERROR_STOP=1 -h 127.0.0.1 -U cag_runtime -d cagtest \
    -c "BEGIN; SET LOCAL cloud_agents.tenant_id = 'tenant-a'; SELECT (SELECT count(*) FROM cloud_agents.idempotency_records) || '|' || (SELECT count(*) FROM cloud_agents.outbox_events) || '|' || (SELECT count(*) FROM cloud_agents.coordination_audit_facts); COMMIT;")
  tenant_b=$(docker exec -e PGPASSWORD="$test_password" "$active_container" \
    psql -X -Atq -v ON_ERROR_STOP=1 -h 127.0.0.1 -U cag_runtime -d cagtest \
    -c "BEGIN; SET LOCAL cloud_agents.tenant_id = 'tenant-b'; SELECT (SELECT count(*) FROM cloud_agents.idempotency_records) || '|' || (SELECT count(*) FROM cloud_agents.outbox_events) || '|' || (SELECT count(*) FROM cloud_agents.coordination_audit_facts); COMMIT;")
  if [[ $tenant_a != "1|1|1" || $tenant_b != "0|0|0" ]]; then
    echo "Tenant RLS drift: tenant-a=$tenant_a tenant-b=$tenant_b" >&2
    exit 1
  fi

  expect_sql_failure cag_runtime \
    "INSERT INTO cloud_agents.coordination_audit_facts (tenant_id) VALUES ('tenant-a')"
  expect_sql_failure cag_migration \
    "SET ROLE cloud_agents_migration_owner; INSERT INTO cloud_agents.platform_operations (tenant_id,tenant_ref_id,operation_id,operation_generation,registry_digest,state_machine_digest,policy_digest,profile_id,profile_digest,subject_digest,request_digest,state,cleanup_phase,recovery_generation,current_attempt_number) VALUES ('tenant-a','tenant-a','operation-a',1,'$registry_digest','$state_machine_digest','$policy_digest','$profile_id','$profile_digest','sha256:${registry_digest#sha256:}','sha256:${policy_digest#sha256:}','pending','none',0,0)"
  expect_sql_failure cag_migration \
    "SET ROLE cloud_agents_migration_owner; INSERT INTO cloud_agents.idempotency_records (tenant_id,tenant_ref_id,subject_digest,registry_digest,profile_id,profile_digest,idempotency_key,request_digest,state,created_at,updated_at,expires_at) VALUES ('tenant-b','tenant-b','sha256:${registry_digest#sha256:}','$registry_digest','$profile_id','sha256:${registry_digest#sha256:}','idem-tenant-b-0001','sha256:${policy_digest#sha256:}','pending','2026-08-19T00:00:00Z','2026-08-19T00:00:00Z','2026-08-20T00:00:00Z')"
  expect_sql_failure cag_migration \
    "SET ROLE cloud_agents_migration_owner; INSERT INTO cloud_agents.idempotency_records (tenant_id,tenant_ref_id,subject_digest,registry_digest,profile_id,profile_digest,idempotency_key,request_digest,state,created_at,updated_at,expires_at) VALUES ('tenant-b','tenant-b','sha256:${registry_digest#sha256:}','$registry_digest','$profile_id','$profile_digest','idem-tenant-b-0002','sha256:${policy_digest#sha256:}','pending','2026-08-19T00:00:00Z','2026-08-19T00:00:00Z','2026-08-20T00:00:01Z')"
  expect_sql_failure cag_migration \
    "SET ROLE cloud_agents_migration_owner; INSERT INTO cloud_agents.leader_leases (leader_name,holder_id,holder_incarnation,fencing_token,lease_started_at,lease_expires_at,updated_at) VALUES ('outbox-dispatcher','holder-c','incarnation-c',1,'2026-08-19T00:00:00Z','2026-08-19T00:00:00.999999Z','2026-08-19T00:00:00Z')"
  expect_sql_failure cag_migration \
    "SET ROLE cloud_agents_migration_owner; INSERT INTO cloud_agents.leader_leases (leader_name,holder_id,holder_incarnation,fencing_token,lease_started_at,lease_expires_at,updated_at) VALUES ('outbox-dispatcher','holder-c','incarnation-c',1,'2026-08-19T00:00:00Z','2026-08-19T00:01:00.000001Z','2026-08-19T00:00:00Z')"

  echo "durable-coordination-kernel: PostgreSQL $postgres_major ($actual_version) PASS"
  cleanup
done

echo "durable-coordination-kernel: PG15/16/17 schema-only matrix PASS"
