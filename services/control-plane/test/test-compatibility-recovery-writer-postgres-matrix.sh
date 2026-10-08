#!/usr/bin/env bash

set -euo pipefail

script_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
repo_root=$(cd -- "$script_dir/../../.." && pwd -P)
module_dir="$repo_root/services/control-plane"
registry="$repo_root/contracts/generated/platform/v1alpha1/compatibility-recovery-registry-v2.json"

if ! docker version >/dev/null 2>&1; then
  echo "A running Docker daemon is required" >&2
  exit 1
fi
if ! command -v jq >/dev/null 2>&1; then
  echo "jq is required to bind the matrix to the generated registry" >&2
  exit 1
fi

declare -a matrix=(
  "15|150018|postgres@sha256:6eb0add3b77c081df18aa518ce43df58fdcc40f2e6d868a6fd08038dc7acd425"
  "16|160014|postgres@sha256:95206741a5b214807675e14165369d05b93a9cf692223b616d07cca227e74b0b"
  "17|170010|postgres@sha256:742f40ea20b9ff2ff31db5458d127452988a2164df9e17441e191f3b72252193"
)

registry_digest="sha256:d5ca128f28d637349dd6f8515f9ca6bb182fb0778a3160e24d731712589f2973"
state_machine_digest="sha256:41ed340b8a1106341f8b797210492af0f9c022d8d43803977ff8079d52251863"
policy_digest="sha256:20f5b6e30e7d7254baabc97894aba2af2d2bcf40f4175f504d195b4e3a832708"
expected_operations=$(jq -r \
  '.profiles[].spec.operations[].sqlFunction | split(".")[1]' \
  "$registry" | LC_ALL=C sort)

digest() {
  local character=$1
  local value=""
  local index
  for index in $(seq 1 64); do
    value="${value}${character}"
  done
  printf 'sha256:%s' "$value"
}

d1=$(digest 1); d2=$(digest 2); d3=$(digest 3); d4=$(digest 4)
d5=$(digest 5); d6=$(digest 6); d7=$(digest 7); d8=$(digest 8)
d9=$(digest 9); da=$(digest a); db=$(digest b); dc=$(digest c)
dd=$(digest d); de=$(digest e); df=$(digest f)

run_id="compatibility-recovery-writer-$$-$RANDOM"
ownership_label="com.hxp0618.cloud-agents.test-run"
active_container=""
test_password=""
tmp_dir=$(mktemp -d)

cleanup_container() {
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

cleanup() {
  cleanup_container
  rm -rf -- "$tmp_dir"
}

on_signal() {
  local exit_code=$1
  trap - EXIT INT TERM
  cleanup || true
  exit "$exit_code"
}

query_as() {
  local user=$1
  local sql=$2
  docker exec -e PGPASSWORD="$test_password" "$active_container" \
    psql -X -At -F '|' -v ON_ERROR_STOP=1 -h 127.0.0.1 \
    -U "$user" -d cagtest -c "$sql"
}

expect_sql_failure() {
  local user=$1
  local sql=$2
  if query_as "$user" "$sql" >/dev/null 2>&1; then
    echo "Expected SQL failure for $user: $sql" >&2
    exit 1
  fi
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

trap cleanup EXIT
trap 'on_signal 130' INT
trap 'on_signal 143' TERM

for matrix_entry in "${matrix[@]}"; do
  IFS='|' read -r postgres_major expected_version image <<<"$matrix_entry"
  if ! docker image inspect "$image" >/dev/null 2>&1; then
    echo "Missing exact local image: $image" >&2
    exit 1
  fi

  active_container="cag-p1-compat-writer-pg${postgres_major}-$$-$RANDOM"
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
    000011_add_compatibility_recovery_writer.sql; do
    docker exec -e PGPASSWORD="$test_password" "$active_container" \
      psql -X -v ON_ERROR_STOP=1 --single-transaction \
      -h 127.0.0.1 -U cag_migration -d cagtest \
      -c 'SET ROLE cloud_agents_migration_owner' \
      -f "/workspace/services/control-plane/migrations/$migration" >/dev/null
  done

  helper_result=$(query_as cag_runtime \
    "SELECT cloud_agents.compatibility_recovery_registry_digest_v2(), cloud_agents.compatibility_recovery_state_machine_digest_v2(), cloud_agents.compatibility_recovery_policy_digest_v2(), cloud_agents.compatibility_recovery_schema_head_v2(), cloud_agents.compatibility_recovery_profile_is_registered_v2('migration-preflight/v2','sha256:e02302ea60eca9855d362d8bcab7efc0466adab6d3a486d828adccdbc5411d7a');")
  assert_equal "$helper_result" \
    "$registry_digest|$state_machine_digest|$policy_digest|000010|t" \
    "v2 registry helper binding"

  observed_operations=$(query_as postgres \
    "SELECT p.proname FROM pg_catalog.pg_proc AS p JOIN pg_catalog.pg_namespace AS n ON n.oid=p.pronamespace WHERE n.nspname='cloud_agents' AND p.proname IN ($(jq -r '[.profiles[].spec.operations[].sqlFunction | split(".")[1] | "\u0027" + . + "\u0027"] | join(",")' "$registry")) ORDER BY p.proname;")
  assert_equal "$observed_operations" "$expected_operations" \
    "generated operation SQL catalog"

  catalog_result=$(query_as postgres \
    "SELECT (SELECT count(*) FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='cloud_agents' AND c.relname IN ('compatibility_recovery_workload_principals_v2','compatibility_recovery_backfills_v2','compatibility_recovery_restore_evidence_v2','compatibility_recovery_live_instances_v2','compatibility_recovery_retirement_receipts_v2','compatibility_recovery_transition_facts_v2') AND pg_catalog.pg_get_userbyid(c.relowner)='cloud_agents_migration_owner' AND c.relrowsecurity AND c.relforcerowsecurity), (SELECT count(*) FROM pg_catalog.pg_policies WHERE schemaname='cloud_agents' AND tablename LIKE 'compatibility_recovery_%_v2' AND roles='{cloud_agents_migration_owner}' AND qual LIKE '%require_tenant_id%'), (SELECT count(*) FROM pg_catalog.pg_proc p JOIN pg_catalog.pg_namespace n ON n.oid=p.pronamespace WHERE n.nspname='cloud_agents' AND p.proname ~ '^compatibility_recovery_[a-z0-9_]+_v2$' AND NOT EXISTS (SELECT 1 FROM pg_catalog.aclexplode(COALESCE(p.proacl,pg_catalog.acldefault('f',p.proowner))) a WHERE a.grantee=0 AND a.privilege_type='EXECUTE'));")
  assert_equal "$catalog_result" "6|6|44" "v2 RLS and function ACL catalog"

  expect_sql_failure cag_runtime \
    "SET cloud_agents.tenant_id='tenant-a'; SELECT count(*) FROM cloud_agents.compatibility_recovery_live_instances_v2;"
  expect_sql_failure cag_bootstrap \
    "SET cloud_agents.tenant_id='tenant-a'; SELECT count(*) FROM cloud_agents.compatibility_recovery_workload_principals_v2;"
  expect_sql_failure cag_runtime \
    "SET cloud_agents.tenant_id='tenant-a'; SELECT * FROM cloud_agents.compatibility_recovery_workload_principal_register_v2('tenant-a','forbidden','postgres','forbidden',1,'$d1','$d2');"

  principal=$(query_as cag_bootstrap \
    "SET cloud_agents.tenant_id='tenant-a'; SELECT result_code,state,version,writer_epoch FROM cloud_agents.compatibility_recovery_workload_principal_register_v2('tenant-a','workload-a','postgres','principal-a',1,'$d1','$d2');")
  assert_equal "$principal" $'SET\napplied|active|1|1' "principal register"
  principal=$(query_as cag_bootstrap \
    "SET cloud_agents.tenant_id='tenant-a'; SELECT result_code,state,version,writer_epoch FROM cloud_agents.compatibility_recovery_workload_principal_rotate_v2('tenant-a','workload-a','postgres','principal-a','principal-b',1,2,'$d3','$d4'); SELECT result_code,state,version,writer_epoch FROM cloud_agents.compatibility_recovery_workload_principal_revoke_v2('tenant-a','workload-a','postgres','principal-b',2,'$d5','$d6');")
  assert_equal "$principal" $'SET\napplied|active|2|2\napplied|revoked|3|2' \
    "principal rotate and revoke"

  live=$(query_as cag_runtime \
    "SET cloud_agents.tenant_id='tenant-a'; SELECT result_code,state,version,writer_epoch FROM cloud_agents.compatibility_recovery_live_instance_register_v2('tenant-a','control-plane','instance-a',1,1,1,'v1.0.0','000010','000011',300,'$d7','$d8'); SELECT result_code,state,version,writer_epoch FROM cloud_agents.compatibility_recovery_live_instance_activate_v2('tenant-a','control-plane','instance-a',1,1,1,2,'$d9','$da');")
  assert_equal "$live" $'SET\napplied|registered|1|1\napplied|active|2|2' \
    "live register and activate"

  restore=$(query_as cag_migration \
    "SET ROLE cloud_agents_migration_owner; SET cloud_agents.tenant_id='tenant-a'; SELECT result_code,state,version FROM cloud_agents.compatibility_recovery_restore_evidence_record_v2('tenant-a','drill-a',$postgres_major,'$db','$dc','000011','$dd','$de',transaction_timestamp(),'$df','$(digest 0)'); SELECT result_code,state,version FROM cloud_agents.compatibility_recovery_restore_evidence_complete_v2('tenant-a','drill-a',1,'$de','sha256:0101010101010101010101010101010101010101010101010101010101010101','sha256:0202020202020202020202020202020202020202020202020202020202020202');")
  assert_equal "$restore" $'SET\nSET\napplied|recorded|1\napplied|complete|2' \
    "restore evidence lifecycle"

  preflight=$(query_as cag_runtime \
    "SET cloud_agents.tenant_id='tenant-a'; SELECT result_code,state,decision,COALESCE(stable_error_code,''),writer_epoch FROM cloud_agents.compatibility_recovery_migration_preflight_evaluate_v2('tenant-a',$postgres_major,'$db','$dc','000011',1,2,'$de',false,NULL); SELECT state,stable_error_code FROM cloud_agents.compatibility_recovery_migration_preflight_evaluate_v2('tenant-a',$postgres_major,'$db','$dc','000011',1,2,'$de',true,'sha256:0303030303030303030303030303030303030303030303030303030303030303');")
  assert_equal "$preflight" $'SET\nobserved|approved|approved||2\nrejected|preflight_irreversible_approval_unverified' \
    "migration preflight closed decisions"

  backfill=$(query_as cag_migration \
    "SET ROLE cloud_agents_migration_owner; SET cloud_agents.tenant_id='tenant-a'; SELECT result_code,state,version,writer_epoch FROM cloud_agents.compatibility_recovery_backfill_start_v2('tenant-a','000011','expand','cursor-0','$d1',1,'sha256:0404040404040404040404040404040404040404040404040404040404040404','sha256:0505050505050505050505050505050505050505050505050505050505050505'); SELECT result_code,state,version,writer_epoch FROM cloud_agents.compatibility_recovery_backfill_acquire_lease_v2('tenant-a','000011','migration-a',1,2,60,'sha256:0606060606060606060606060606060606060606060606060606060606060606','sha256:0707070707070707070707070707070707070707070707070707070707070707'); SELECT result_code,state,version,writer_epoch FROM cloud_agents.compatibility_recovery_backfill_advance_v2('tenant-a','000011','migration-a',2,'backfill','cursor-1','$d2',1,'sha256:0808080808080808080808080808080808080808080808080808080808080808','sha256:0909090909090909090909090909090909090909090909090909090909090909'); SELECT result_code,state,version,writer_epoch FROM cloud_agents.compatibility_recovery_backfill_complete_v2('tenant-a','000011','migration-a',2,'contract','cursor-2','$d3',1,'sha256:1010101010101010101010101010101010101010101010101010101010101010','sha256:1110111011101110111011101110111011101110111011101110111011101110');")
  assert_equal "$backfill" $'SET\nSET\napplied|pending|1|1\napplied|leased|2|2\napplied|running|3|2\napplied|succeeded|4|2' \
    "backfill lifecycle"

  unfenced=$(query_as cag_runtime \
    "SET cloud_agents.tenant_id='tenant-a'; SELECT result_code,stable_error_code FROM cloud_agents.compatibility_recovery_retirement_receipt_collect_v2('tenant-a','control-plane','instance-a',1,1,2,0,true,true,true,true,true,true,'sha256:1212121212121212121212121212121212121212121212121212121212121212','sha256:1313131313131313131313131313131313131313131313131313131313131313');")
  assert_equal "$unfenced" $'SET\nrejected|retirement_generation_fence_contradiction' \
    "unfenced retirement contradiction"

  retirement=$(query_as cag_runtime \
    "SET cloud_agents.tenant_id='tenant-a'; SELECT result_code,state,version,writer_epoch FROM cloud_agents.compatibility_recovery_live_instance_fence_v2('tenant-a','control-plane','instance-a',1,1,2,3,'sha256:1414141414141414141414141414141414141414141414141414141414141414','sha256:1515151515151515151515151515151515151515151515151515151515151515'); SELECT result_code,state,version,writer_epoch FROM cloud_agents.compatibility_recovery_retirement_receipt_collect_v2('tenant-a','control-plane','instance-a',1,1,3,0,true,true,true,true,true,true,'sha256:1616161616161616161616161616161616161616161616161616161616161616','sha256:1717171717171717171717171717171717171717171717171717171717171717'); SELECT result_code,state,version,writer_epoch FROM cloud_agents.compatibility_recovery_retirement_receipt_complete_v2('tenant-a','control-plane','instance-a',1,1,3,1,'sha256:1818181818181818181818181818181818181818181818181818181818181818','sha256:1919191919191919191919191919191919191919191919191919191919191919','sha256:2020202020202020202020202020202020202020202020202020202020202020');")
  assert_equal "$retirement" $'SET\napplied|fenced|3|3\napplied|collecting|1|3\napplied|complete|2|3' \
    "fenced retirement lifecycle"

  observed=$(query_as cag_runtime \
    "SET cloud_agents.tenant_id='tenant-a'; SELECT result_code,state,version,writer_epoch FROM cloud_agents.compatibility_recovery_retirement_receipt_complete_v2('tenant-a','control-plane','instance-a',1,1,3,1,'sha256:1818181818181818181818181818181818181818181818181818181818181818','sha256:1919191919191919191919191919191919191919191919191919191919191919','sha256:2020202020202020202020202020202020202020202020202020202020202020'); SELECT result_code,state,version,transition_observed FROM cloud_agents.compatibility_recovery_retirement_receipt_reconcile_v2('tenant-a','control-plane','instance-a',1,1,'sha256:1919191919191919191919191919191919191919191919191919191919191919');")
  assert_equal "$observed" $'SET\nobserved|complete|2|3\nobserved|complete|2|t' \
    "retirement observed and reconcile"

  concurrent_digest='sha256:2121212121212121212121212121212121212121212121212121212121212121'
  query_as cag_bootstrap \
    "SET cloud_agents.tenant_id='tenant-a'; SELECT result_code FROM cloud_agents.compatibility_recovery_workload_principal_register_v2('tenant-a','concurrent-a','postgres','concurrent-a',1,'$concurrent_digest','sha256:2221222122212221222122212221222122212221222122212221222122212221');" \
    >"$tmp_dir/concurrent-a" &
  first_pid=$!
  query_as cag_bootstrap \
    "SET cloud_agents.tenant_id='tenant-a'; SELECT result_code FROM cloud_agents.compatibility_recovery_workload_principal_register_v2('tenant-a','concurrent-b','postgres','concurrent-b',1,'$concurrent_digest','sha256:2323232323232323232323232323232323232323232323232323232323232323');" \
    >"$tmp_dir/concurrent-b" &
  second_pid=$!
  wait "$first_pid"
  wait "$second_pid"
  concurrent_results=$(LC_ALL=C sort "$tmp_dir/concurrent-a" "$tmp_dir/concurrent-b")
  assert_equal "$concurrent_results" $'SET\nSET\napplied\nconflict' \
    "concurrent transition digest serialization"

  expect_sql_failure cag_runtime \
    "SET cloud_agents.tenant_id='tenant-b'; SELECT * FROM cloud_agents.compatibility_recovery_live_instance_reconcile_v2('tenant-a','control-plane','instance-a',1,1,'$d1');"
  expect_sql_failure cag_runtime \
    "SET cloud_agents.tenant_id='tenant-a'; SELECT * FROM cloud_agents.compatibility_recovery_backfill_reconcile_v2('tenant-a','000011','$d1');"

  echo "compatibility-recovery-writer: PostgreSQL $postgres_major ($actual_version) PASS"
  cleanup_container
done

trap - EXIT INT TERM
cleanup
echo "compatibility-recovery-writer: PG15/16/17 writer matrix PASS"
