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

registry_digest="sha256:9df9dcf4c9e62cd95b43be362bf5a332bf9637ca881f16fbd25486ad0792f72d"
state_machine_digest="sha256:5fb7f076c40aed31d5309a4de6aa2a66b93f3d560a535ecf992dc1f817d8f408"
policy_digest="sha256:804ee0280ab5c98a48989abf511659d2a6f801fa5201617c3e436f848dfdc11d"
backfill_profile="sha256:779791352f9ba77f1f75c3cd6e5b4a846ee00687217eb3489ec8877513809047"
live_profile="sha256:aeb12441bc83a110047a1a69a413d2672cf5ba8c82747d52a842ab91c4840790"
preflight_profile="sha256:0ef86c85d7878202ac16f06c6b32a7bd84d642433a7098a25fc09b5f7f8599ba"
restore_profile="sha256:d095186e6f70205f9c842acc8e7232ff658c4aecbed06436ad91532e6cf4042e"
retirement_profile="sha256:cf2e57dcf51bfea35e7ca82875acb04225e5a050fcf3d394cb6f1bc457d2a3ac"

run_id="compatibility-recovery-kernel-$$-$RANDOM"
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

  active_container="cag-p1-compat-recovery-pg${postgres_major}-$$-$RANDOM"
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
    if docker exec "$active_container" pg_isready -h 127.0.0.1 -U postgres -d postgres >/dev/null 2>&1; then
      ready_count=$((ready_count + 1))
      [[ $ready_count -ge 2 ]] && break
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
  [[ $actual_version == "$expected_version" ]] || {
    echo "Unexpected PostgreSQL version: expected $expected_version, got $actual_version" >&2
    exit 1
  }

  docker exec -e PGPASSWORD="$test_password" "$active_container" \
    psql -X -v ON_ERROR_STOP=1 -h 127.0.0.1 -U postgres -d postgres \
    -f /workspace/services/control-plane/migrations/bootstrap/roles.sql >/dev/null

  if [[ $postgres_major -eq 15 ]]; then
    membership_grants=$'GRANT cloud_agents_migration_owner TO cag_migration;\nGRANT cloud_agents_runtime TO cag_runtime;\nGRANT cloud_agents_bootstrap_admin TO cag_bootstrap;'
  else
    membership_grants=$'GRANT cloud_agents_migration_owner TO cag_migration WITH INHERIT FALSE, SET TRUE;\nGRANT cloud_agents_runtime TO cag_runtime WITH INHERIT TRUE, SET TRUE;\nGRANT cloud_agents_bootstrap_admin TO cag_bootstrap WITH INHERIT TRUE, SET TRUE;'
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
    000007_expand_durable_coordination_kernel.sql \
    000008_add_durable_coordination_service.sql \
    000009_redact_coordination_conflicts.sql \
    000010_expand_compatibility_recovery_kernel.sql; do
    docker exec -e PGPASSWORD="$test_password" "$active_container" \
      psql -X -v ON_ERROR_STOP=1 --single-transaction \
      -h 127.0.0.1 -U cag_migration -d cagtest \
      -c 'SET ROLE cloud_agents_migration_owner' \
      -f "/workspace/services/control-plane/migrations/$migration" >/dev/null
  done

  helper_result=$(docker exec -e PGPASSWORD="$test_password" "$active_container" \
    psql -X -At -v ON_ERROR_STOP=1 -h 127.0.0.1 -U cag_runtime -d cagtest -c \
    "SELECT cloud_agents.compatibility_recovery_registry_digest(), cloud_agents.compatibility_recovery_state_machine_digest(), cloud_agents.compatibility_recovery_policy_digest(), cloud_agents.compatibility_recovery_profile_digest('backfill/v1'), cloud_agents.compatibility_recovery_profile_digest('live-instance/v1'), cloud_agents.compatibility_recovery_profile_digest('migration-preflight/v1'), cloud_agents.compatibility_recovery_profile_digest('restore-evidence/v1'), cloud_agents.compatibility_recovery_profile_digest('retirement-receipt/v1'), cloud_agents.compatibility_recovery_profile_is_registered('backfill/v1','$backfill_profile'), cloud_agents.compatibility_recovery_profile_is_registered('unknown/v1','sha256:0000000000000000000000000000000000000000000000000000000000000000');")
  expected_helpers="$registry_digest|$state_machine_digest|$policy_digest|$backfill_profile|$live_profile|$preflight_profile|$restore_profile|$retirement_profile|t|f"
  [[ $helper_result == "$expected_helpers" ]] || {
    echo "Compatibility registry helper drift: $helper_result" >&2
    exit 1
  }

  catalog_result=$(docker exec -i -e PGPASSWORD="$test_password" "$active_container" \
    psql -X -At -v ON_ERROR_STOP=1 -h 127.0.0.1 -U postgres -d cagtest <<'SQL'
SELECT
  (SELECT count(*) FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
   WHERE n.nspname = 'cloud_agents'
     AND c.relname IN ('workload_database_principals','migration_backfills','schema_restore_evidence','live_instances','instance_retirement_receipts')
     AND pg_catalog.pg_get_userbyid(c.relowner) = 'cloud_agents_migration_owner'),
  (SELECT count(*) FROM (VALUES
     ('workload_database_principals'),('migration_backfills'),('schema_restore_evidence'),('live_instances'),('instance_retirement_receipts')
   ) AS tables(name)
   WHERE NOT has_table_privilege('cloud_agents_runtime', 'cloud_agents.' || name, 'SELECT,INSERT,UPDATE,DELETE,TRUNCATE')
     AND NOT has_table_privilege('cloud_agents_bootstrap_admin', 'cloud_agents.' || name, 'SELECT,INSERT,UPDATE,DELETE,TRUNCATE')
     AND NOT EXISTS (
       SELECT 1
       FROM pg_catalog.pg_class c
       JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
         CROSS JOIN LATERAL pg_catalog.aclexplode(
         COALESCE(c.relacl, pg_catalog.acldefault('r', c.relowner))) privilege
       WHERE n.nspname = 'cloud_agents' AND c.relname = tables.name
         AND privilege.grantee = 0
         AND privilege.privilege_type IN ('SELECT','INSERT','UPDATE','DELETE','TRUNCATE'))),
  (SELECT count(*) FROM (VALUES
     ('cloud_agents.compatibility_recovery_registry_digest()'),
     ('cloud_agents.compatibility_recovery_state_machine_digest()'),
     ('cloud_agents.compatibility_recovery_policy_digest()'),
     ('cloud_agents.compatibility_recovery_profile_digest(text)'),
     ('cloud_agents.compatibility_recovery_profile_is_registered(text,text)')
   ) AS functions(signature)
   WHERE has_function_privilege('cloud_agents_runtime', signature, 'EXECUTE')
     AND NOT has_function_privilege('cloud_agents_bootstrap_admin', signature, 'EXECUTE')
     AND NOT EXISTS (
       SELECT 1
       FROM pg_catalog.pg_proc p
       CROSS JOIN LATERAL pg_catalog.aclexplode(
         COALESCE(p.proacl, pg_catalog.acldefault('f', p.proowner))) privilege
       WHERE p.oid = functions.signature::pg_catalog.regprocedure
         AND privilege.grantee = 0 AND privilege.privilege_type = 'EXECUTE')),
  (SELECT count(*) FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace
   WHERE n.nspname = 'cloud_agents'
     AND p.proname LIKE 'compatibility_recovery_%'
     AND p.provolatile = 'i' AND p.proparallel = 's' AND NOT p.prosecdef);
SQL
)
  [[ $catalog_result == "5|5|5|5" ]] || {
    echo "Compatibility catalog/ACL boundary drift: $catalog_result" >&2
    exit 1
  }

  docker exec -i -e PGPASSWORD="$test_password" "$active_container" \
    psql -X -v ON_ERROR_STOP=1 -h 127.0.0.1 -U cag_migration -d cagtest <<SQL >/dev/null
SET ROLE cloud_agents_migration_owner;
BEGIN;
INSERT INTO cloud_agents.workload_database_principals (
    database_principal, registry_digest, state_machine_digest, policy_digest,
    service_kind, instance_id, incarnation, rollout_generation, writer_epoch,
    can_register_instance, can_heartbeat_instance, can_retire_instance, state,
    expires_at, created_at, updated_at
) VALUES (
    'cag-runtime-a', '$registry_digest', '$state_machine_digest', '$policy_digest',
    'control-plane', 'instance-a', 1, 1, 1, true, true, true, 'active',
    '2026-08-21T00:00:00Z', '2026-08-20T00:00:00Z', '2026-08-20T00:00:00Z'
);
INSERT INTO cloud_agents.migration_backfills (
    migration_id, registry_digest, state_machine_digest, policy_digest, profile_id,
    profile_digest, state, phase, cursor, digest, count, batch_generation,
    committed_at, stable_error_code, created_at, updated_at
) VALUES (
    '000010', '$registry_digest', '$state_machine_digest', '$policy_digest',
    'backfill/v1', '$backfill_profile', 'succeeded', 'reconcile', 'cursor-000010',
    '$registry_digest', 1, 1, '2026-08-20T00:00:00Z', NULL,
    '2026-08-20T00:00:00Z', '2026-08-20T00:00:00Z'
);
INSERT INTO cloud_agents.schema_restore_evidence (
    drill_id, registry_digest, state_machine_digest, policy_digest, profile_id,
    profile_digest, state, scope, postgres_major, ledger_checksum,
    target_schema_bundle_digest, target_phase, restore_point_digest, evidence_digest,
    drill_at, created_at, updated_at
) VALUES (
    'restore-000010', '$registry_digest', '$state_machine_digest', '$policy_digest',
    'restore-evidence/v1', '$restore_profile', 'drill_verified',
    'local_logical_backup_restore_and_preflight', $postgres_major,
    '$registry_digest', '$state_machine_digest', '000010', '$policy_digest',
    '$registry_digest', '2026-08-19T00:00:00Z', '2026-08-20T00:00:00Z',
    '2026-08-20T00:00:00Z'
);
INSERT INTO cloud_agents.live_instances (
    service_kind, instance_id, incarnation, registry_digest, state_machine_digest,
    policy_digest, profile_id, profile_digest, rollout_generation, writer_epoch,
    binary_version, supported_schema_min, supported_schema_max, drain_state,
    heartbeat_at, heartbeat_ttl_seconds, created_at, updated_at
) VALUES (
    'control-plane', 'instance-a', 1, '$registry_digest', '$state_machine_digest',
    '$policy_digest', 'live-instance/v1', '$live_profile', 1, 1, 'v1.0.0',
    '000009', '000010', 'active', '2026-08-20T00:00:00Z', 60,
    '2026-08-20T00:00:00Z', '2026-08-20T00:00:00Z'
);
INSERT INTO cloud_agents.instance_retirement_receipts (
    service_kind, instance_id, incarnation, rollout_generation, registry_digest,
    state_machine_digest, policy_digest, profile_id, profile_digest, state,
    credential_revoked, endpoint_revoked, process_terminated, leader_released,
    claim_released, generation_fenced, writer_epoch, receipt_digest,
    rejection_reason, created_at, updated_at
) VALUES (
    'control-plane', 'instance-a', 1, 1, '$registry_digest', '$state_machine_digest',
    '$policy_digest', 'retirement-receipt/v1', '$retirement_profile', 'complete',
    true, true, true, true, true, true, 1, '$registry_digest', NULL,
    '2026-08-20T00:00:00Z', '2026-08-20T00:00:00Z'
);
COMMIT;
SQL

  row_counts=$(docker exec -e PGPASSWORD="$test_password" "$active_container" \
    psql -X -At -v ON_ERROR_STOP=1 -h 127.0.0.1 -U postgres -d cagtest -c \
    "SELECT (SELECT count(*) FROM cloud_agents.workload_database_principals) || '|' || (SELECT count(*) FROM cloud_agents.migration_backfills) || '|' || (SELECT count(*) FROM cloud_agents.schema_restore_evidence) || '|' || (SELECT count(*) FROM cloud_agents.live_instances) || '|' || (SELECT count(*) FROM cloud_agents.instance_retirement_receipts);")
  [[ $row_counts == "1|1|1|1|1" ]] || { echo "Unexpected deterministic row counts: $row_counts" >&2; exit 1; }

  expect_sql_failure cag_runtime \
    "INSERT INTO cloud_agents.migration_backfills (migration_id,registry_digest,state_machine_digest,policy_digest,profile_id,profile_digest,state,phase,cursor,digest) VALUES ('000011','$registry_digest','$state_machine_digest','$policy_digest','backfill/v1','$backfill_profile','pending','reconcile','cursor','${registry_digest}');"
  expect_sql_failure cag_migration \
    "SET ROLE cloud_agents_migration_owner; INSERT INTO cloud_agents.migration_backfills (migration_id,registry_digest,state_machine_digest,policy_digest,profile_id,profile_digest,state,phase,cursor,digest) VALUES ('000012','$registry_digest','$state_machine_digest','$policy_digest','backfill/v1','sha256:0000000000000000000000000000000000000000000000000000000000000000','pending','reconcile','cursor','${registry_digest}');"
  expect_sql_failure cag_migration \
    "SET ROLE cloud_agents_migration_owner; INSERT INTO cloud_agents.schema_restore_evidence (drill_id,registry_digest,state_machine_digest,policy_digest,profile_id,profile_digest,state,scope,postgres_major,ledger_checksum,target_schema_bundle_digest,target_phase,restore_point_digest,evidence_digest,drill_at,created_at,updated_at) VALUES ('restore-future','$registry_digest','$state_machine_digest','$policy_digest','restore-evidence/v1','$restore_profile','recorded','local_logical_backup_restore_and_preflight',$postgres_major,'$registry_digest','$state_machine_digest','000010','$policy_digest','sha256:1111111111111111111111111111111111111111111111111111111111111111','2026-08-21T00:00:00Z','2026-08-20T00:00:00Z','2026-08-20T00:00:00Z');"
  expect_sql_failure cag_migration \
    "SET ROLE cloud_agents_migration_owner; INSERT INTO cloud_agents.schema_restore_evidence (drill_id,registry_digest,state_machine_digest,policy_digest,profile_id,profile_digest,state,scope,postgres_major,ledger_checksum,target_schema_bundle_digest,target_phase,restore_point_digest,evidence_digest,drill_at,created_at,updated_at) VALUES ('restore-pg14','$registry_digest','$state_machine_digest','$policy_digest','restore-evidence/v1','$restore_profile','recorded','local_logical_backup_restore_and_preflight',14,'$registry_digest','$state_machine_digest','000010','$policy_digest','sha256:2222222222222222222222222222222222222222222222222222222222222222','2026-08-19T00:00:00Z','2026-08-20T00:00:00Z','2026-08-20T00:00:00Z');"
  expect_sql_failure cag_migration \
    "SET ROLE cloud_agents_migration_owner; INSERT INTO cloud_agents.live_instances (service_kind,instance_id,incarnation,registry_digest,state_machine_digest,policy_digest,profile_id,profile_digest,rollout_generation,writer_epoch,binary_version,supported_schema_min,supported_schema_max,drain_state,heartbeat_at,heartbeat_ttl_seconds,created_at,updated_at) VALUES ('control-plane','instance-b',1,'$registry_digest','$state_machine_digest','$policy_digest','live-instance/v1','$live_profile',1,1,'v1.0.0','000010','000009','active','2026-08-20T00:00:00Z',60,'2026-08-20T00:00:00Z','2026-08-20T00:00:00Z');"
  expect_sql_failure cag_migration \
    "SET ROLE cloud_agents_migration_owner; INSERT INTO cloud_agents.live_instances (service_kind,instance_id,incarnation,registry_digest,state_machine_digest,policy_digest,profile_id,profile_digest,rollout_generation,writer_epoch,binary_version,supported_schema_min,supported_schema_max,drain_state,heartbeat_at,heartbeat_ttl_seconds,created_at,updated_at) VALUES ('control-plane','instance-c',1,'$registry_digest','$state_machine_digest','$policy_digest','live-instance/v1','$live_profile',1,1,'v1.0.0','000009','000010','not-a-state','2026-08-20T00:00:00Z',60,'2026-08-20T00:00:00Z','2026-08-20T00:00:00Z');"
  expect_sql_failure cag_migration \
    "SET ROLE cloud_agents_migration_owner; INSERT INTO cloud_agents.schema_restore_evidence (drill_id,registry_digest,state_machine_digest,policy_digest,profile_id,profile_digest,state,scope,postgres_major,ledger_checksum,target_schema_bundle_digest,target_phase,restore_point_digest,evidence_digest,drill_at,created_at,updated_at) VALUES ('restore-duplicate','$registry_digest','$state_machine_digest','$policy_digest','restore-evidence/v1','$restore_profile','recorded','local_logical_backup_restore_and_preflight',$postgres_major,'$registry_digest','$state_machine_digest','000010','$policy_digest','$registry_digest','2026-08-19T00:00:00Z','2026-08-20T00:00:00Z','2026-08-20T00:00:00Z');"
  expect_sql_failure cag_migration \
    "SET ROLE cloud_agents_migration_owner; INSERT INTO cloud_agents.instance_retirement_receipts (service_kind,instance_id,incarnation,rollout_generation,registry_digest,state_machine_digest,policy_digest,profile_id,profile_digest,state,credential_revoked,endpoint_revoked,process_terminated,leader_released,claim_released,generation_fenced,writer_epoch,receipt_digest,created_at,updated_at) VALUES ('control-plane','instance-a',1,2,'$registry_digest','$state_machine_digest','$policy_digest','retirement-receipt/v1','$retirement_profile','complete',true,true,true,true,false,true,1,'$registry_digest','2026-08-20T00:00:00Z','2026-08-20T00:00:00Z');"
  expect_sql_failure cag_migration \
    "SET ROLE cloud_agents_migration_owner; INSERT INTO cloud_agents.instance_retirement_receipts (service_kind,instance_id,incarnation,rollout_generation,registry_digest,state_machine_digest,policy_digest,profile_id,profile_digest,state,credential_revoked,endpoint_revoked,process_terminated,leader_released,claim_released,generation_fenced,writer_epoch,receipt_digest,created_at,updated_at) VALUES ('control-plane','instance-missing',1,1,'$registry_digest','$state_machine_digest','$policy_digest','retirement-receipt/v1','$retirement_profile','collecting',false,false,false,false,false,false,1,NULL,'2026-08-20T00:00:00Z','2026-08-20T00:00:00Z');"

  echo "compatibility-recovery-kernel: PostgreSQL $postgres_major ($actual_version) PASS"
  cleanup
done

echo "compatibility-recovery-kernel: PG15/16/17 schema-only matrix PASS"
