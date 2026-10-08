#!/usr/bin/env bash

set -euo pipefail

script_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
repo_root=$(cd -- "$script_dir/../../.." && pwd -P)
module_dir="$repo_root/services/control-plane"

declare -a matrix=(
  "15|150018|postgres@sha256:6eb0add3b77c081df18aa518ce43df58fdcc40f2e6d868a6fd08038dc7acd425"
  "16|160014|postgres@sha256:95206741a5b214807675e14165369d05b93a9cf692223b616d07cca227e74b0b"
  "17|170010|postgres@sha256:742f40ea20b9ff2ff31db5458d127452988a2164df9e17441e191f3b72252193"
)

schema_bundle_digest="sha256:54bd987183d6e2d8a7e3ba58a5fa5ee0666015a101193f363f671be294bb2907"
run_id="compatibility-recovery-preflight-retirement-$$-$RANDOM"
ownership_label="com.hxp0618.cloud-agents.test-run"
active_container=""
test_password=""

if ! docker version >/dev/null 2>&1; then
  echo "A running Docker daemon is required" >&2
  exit 1
fi

digest() {
  printf 'sha256:%064x' "$1"
}

cleanup_container() {
  local observed_owner=""
  if [[ -z "$active_container" ]]; then
    return 0
  fi
  if ! docker container inspect "$active_container" >/dev/null 2>&1; then
    active_container=""
    return 0
  fi
  observed_owner=$(docker container inspect \
    --format "{{index .Config.Labels \"$ownership_label\"}}" \
    "$active_container")
  if [[ $observed_owner != "$run_id" ]]; then
    echo "Refusing to remove container not owned by this run: $active_container" >&2
    return 1
  fi
  docker rm -f "$active_container" >/dev/null
  if docker container inspect "$active_container" >/dev/null 2>&1; then
    echo "Owned test container remains after removal: $active_container" >&2
    return 1
  fi
  active_container=""
}

on_exit() {
  local exit_code=$?
  trap - EXIT INT TERM
  cleanup_container || true
  exit "$exit_code"
}

on_signal() {
  local exit_code=$1
  trap - EXIT INT TERM
  cleanup_container || true
  exit "$exit_code"
}

query_as() {
  local user=$1
  local sql=$2
  docker exec \
    -e PGPASSWORD="$test_password" \
    -e PGOPTIONS='-c cloud_agents.tenant_id=tenant-a' \
    "$active_container" \
    psql -X -At -F '|' -v ON_ERROR_STOP=1 -h 127.0.0.1 \
    -U "$user" -d cagtest -c "$sql"
}

assert_equal() {
  local actual=$1
  local expected=$2
  local description=$3
  if [[ $actual != "$expected" ]]; then
    echo "$description: expected '$expected', got '$actual'" >&2
    exit 1
  fi
}

preflight_result() {
  local postgres_major=$1
  query_as cag_runtime \
    "SELECT state, COALESCE(stable_error_code, ''), decision FROM cloud_agents.compatibility_recovery_migration_preflight_evaluate_v2('tenant-a',$postgres_major,'$(digest 1)','$schema_bundle_digest','000012',12,2,'$(digest 3)',false,NULL);"
}

trap on_exit EXIT
trap 'on_signal 130' INT
trap 'on_signal 143' TERM

for matrix_entry in "${matrix[@]}"; do
  IFS='|' read -r postgres_major expected_version image <<<"$matrix_entry"
  if ! docker image inspect "$image" >/dev/null 2>&1; then
    echo "Missing exact local image: $image" >&2
    exit 1
  fi

  active_container="cag-p1-preflight-retirement-pg${postgres_major}-$$-$RANDOM"
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
  assert_equal "$actual_version" "$expected_version" "PostgreSQL version"

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
    000010_expand_compatibility_recovery_kernel.sql \
    000011_add_compatibility_recovery_writer.sql \
    000012_fix_compatibility_recovery_preflight.sql; do
    docker exec -e PGPASSWORD="$test_password" "$active_container" \
      psql -X -v ON_ERROR_STOP=1 --single-transaction \
      -h 127.0.0.1 -U cag_migration -d cagtest \
      -c 'SET ROLE cloud_agents_migration_owner' \
      -f "/workspace/services/control-plane/migrations/$migration" >/dev/null
  done

  restore=$(query_as cag_migration \
    "SET ROLE cloud_agents_migration_owner; SELECT result_code,state,version FROM cloud_agents.compatibility_recovery_restore_evidence_record_v2('tenant-a','drill-retirement',$postgres_major,'$(digest 1)','$schema_bundle_digest','000012','$(digest 2)','$(digest 3)',transaction_timestamp(),'$(digest 4)','$(digest 5)'); SELECT result_code,state,version FROM cloud_agents.compatibility_recovery_restore_evidence_complete_v2('tenant-a','drill-retirement',1,'$(digest 3)','$(digest 6)','$(digest 7)');")
  assert_equal "$restore" $'SET\napplied|recorded|1\napplied|complete|2' \
    "restore evidence lifecycle"

  current=$(query_as cag_runtime \
    "SELECT result_code,state,version,writer_epoch FROM cloud_agents.compatibility_recovery_live_instance_register_v2('tenant-a','control-plane','writer-current',1,12,1,'v2.0.0','000010','000012',300,'$(digest 10)','$(digest 11)'); SELECT result_code,state,version,writer_epoch FROM cloud_agents.compatibility_recovery_live_instance_activate_v2('tenant-a','control-plane','writer-current',1,12,1,2,'$(digest 12)','$(digest 13)');")
  assert_equal "$current" $'applied|registered|1|1\napplied|active|2|2' \
    "current writer lifecycle"

  predecessor=$(query_as cag_runtime \
    "SELECT result_code,state,version,writer_epoch FROM cloud_agents.compatibility_recovery_live_instance_register_v2('tenant-a','control-plane','writer-predecessor',1,11,1,'v1.0.0','000010','000011',300,'$(digest 14)','$(digest 15)'); SELECT result_code,state,version,writer_epoch FROM cloud_agents.compatibility_recovery_live_instance_activate_v2('tenant-a','control-plane','writer-predecessor',1,11,1,2,'$(digest 16)','$(digest 17)');")
  assert_equal "$predecessor" $'applied|registered|1|1\napplied|active|2|2' \
    "unexpired predecessor lifecycle"
  assert_equal "$(preflight_result "$postgres_major")" \
    'rejected|preflight_unexpired_instance_incompatible|rejected' \
    "active incompatible predecessor blocks preflight"

  fenced=$(query_as cag_runtime \
    "SELECT result_code,state,version,writer_epoch FROM cloud_agents.compatibility_recovery_live_instance_fence_v2('tenant-a','control-plane','writer-predecessor',1,11,2,3,'$(digest 20)','$(digest 21)');")
  assert_equal "$fenced" 'applied|fenced|3|3' "predecessor fence"
  assert_equal "$(preflight_result "$postgres_major")" \
    'rejected|preflight_unexpired_instance_incompatible|rejected' \
    "fenced predecessor without receipt still blocks"

  incomplete=$(query_as cag_runtime \
    "SELECT result_code,state,version,writer_epoch FROM cloud_agents.compatibility_recovery_retirement_receipt_collect_v2('tenant-a','control-plane','writer-predecessor',1,11,3,0,true,false,false,false,false,true,'$(digest 22)','$(digest 23)');")
  assert_equal "$incomplete" 'applied|collecting|1|3' "incomplete receipt collect"
  assert_equal "$(preflight_result "$postgres_major")" \
    'rejected|preflight_unexpired_instance_incompatible|rejected' \
    "incomplete predecessor receipt still blocks"

  complete=$(query_as cag_runtime \
    "SELECT result_code,state,version,writer_epoch FROM cloud_agents.compatibility_recovery_retirement_receipt_collect_v2('tenant-a','control-plane','writer-predecessor',1,11,3,1,false,true,true,true,true,false,'$(digest 24)','$(digest 25)'); SELECT result_code,state,version,writer_epoch FROM cloud_agents.compatibility_recovery_retirement_receipt_complete_v2('tenant-a','control-plane','writer-predecessor',1,11,3,2,'$(digest 26)','$(digest 27)','$(digest 28)');")
  assert_equal "$complete" $'applied|collecting|2|3\napplied|complete|3|3' \
    "complete predecessor receipt"
  assert_equal "$(preflight_result "$postgres_major")" 'approved||approved' \
    "complete exact predecessor receipt permits preflight"

  expired=$(query_as cag_runtime \
    "SELECT result_code,state,version,writer_epoch FROM cloud_agents.compatibility_recovery_live_instance_register_v2('tenant-a','control-plane','writer-expired',1,10,1,'v1.0.0','000010','000011',1,'$(digest 30)','$(digest 31)'); SELECT result_code,state,version,writer_epoch FROM cloud_agents.compatibility_recovery_live_instance_activate_v2('tenant-a','control-plane','writer-expired',1,10,1,2,'$(digest 32)','$(digest 33)'); SELECT result_code,state,version,writer_epoch FROM cloud_agents.compatibility_recovery_live_instance_fence_v2('tenant-a','control-plane','writer-expired',1,10,2,3,'$(digest 34)','$(digest 35)');")
  assert_equal "$expired" $'applied|registered|1|1\napplied|active|2|2\napplied|fenced|3|3' \
    "expired predecessor lifecycle"
  query_as cag_runtime 'SELECT pg_catalog.pg_sleep(2);' >/dev/null
  assert_equal "$(preflight_result "$postgres_major")" \
    'rejected|preflight_expired_instance_unretired|rejected' \
    "expired fenced predecessor without receipt blocks"

  expired_complete=$(query_as cag_runtime \
    "SELECT result_code,state,version,writer_epoch FROM cloud_agents.compatibility_recovery_retirement_receipt_collect_v2('tenant-a','control-plane','writer-expired',1,10,3,0,true,true,true,true,true,true,'$(digest 36)','$(digest 37)'); SELECT result_code,state,version,writer_epoch FROM cloud_agents.compatibility_recovery_retirement_receipt_complete_v2('tenant-a','control-plane','writer-expired',1,10,3,1,'$(digest 38)','$(digest 39)','$(digest 40)');")
  assert_equal "$expired_complete" $'applied|collecting|1|3\napplied|complete|2|3' \
    "complete expired predecessor receipt"
  assert_equal "$(preflight_result "$postgres_major")" 'approved||approved' \
    "complete exact expired receipt permits preflight"

  cleanup_container
  echo "compatibility-recovery-preflight-retirement: PostgreSQL $postgres_major ($actual_version) PASS"
done

trap - EXIT INT TERM
cleanup_container
echo "compatibility-recovery-preflight-retirement: PG15/16/17 focused matrix PASS"
