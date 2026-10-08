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

declare -a migrations=(
  000001_expand_migration_kernel.sql
  000002_expand_tenancy.sql
  000003_expand_membership_rbac.sql
  000004_expand_membership_rbac_mutations.sql
  000005_close_membership_binding_authority.sql
  000006_close_subject_issuer_validation.sql
  000007_expand_durable_coordination_kernel.sql
  000008_add_durable_coordination_service.sql
  000009_redact_coordination_conflicts.sql
  000010_expand_compatibility_recovery_kernel.sql
  000011_add_compatibility_recovery_writer.sql
)

if ! docker version >/dev/null 2>&1; then
  echo "A running Docker daemon is required" >&2
  exit 1
fi

if command -v sha256sum >/dev/null 2>&1; then
  sha256_file() {
    sha256sum "$1" | awk '{print $1}'
  }
  sha256_stdin() {
    sha256sum | awk '{print $1}'
  }
elif command -v shasum >/dev/null 2>&1; then
  sha256_file() {
    shasum -a 256 "$1" | awk '{print $1}'
  }
  sha256_stdin() {
    shasum -a 256 | awk '{print $1}'
  }
else
  echo "sha256sum or shasum is required" >&2
  exit 1
fi

sha256_text() {
  printf '%s' "$1" | sha256_stdin
}

ledger_checksum="sha256:$(sha256_file "$module_dir/migrations/manifest.json")"
schema_bundle_digest="sha256:$(sha256_file "$module_dir/migrations/schema-bundle.json")"
source_commit=$(git -C "$repo_root" rev-parse HEAD)
source_tree=$(git -C "$repo_root" rev-parse HEAD^{tree})
source_dirty=false
if [[ -n $(git -C "$repo_root" status --porcelain=v1 --untracked-files=all) ]]; then
  source_dirty=true
fi

run_id="local-logical-restore-$$-$RANDOM"
ownership_label="com.hxp0618.cloud-agents.test-run"
active_container=""
test_password=""
tmp_dir=$(mktemp -d)

cleanup_container() {
  if [[ -n $active_container ]] &&
    docker container inspect "$active_container" >/dev/null 2>&1; then
    local observed_owner
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
  local database=$2
  local sql=$3
  docker exec -e PGPASSWORD="$test_password" "$active_container" \
    psql -X -Atq -F '|' -v ON_ERROR_STOP=1 -h 127.0.0.1 \
    -U "$user" -d "$database" -c "$sql"
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

bootstrap_database() {
  local database=$1
  docker exec -e PGPASSWORD="$test_password" "$active_container" \
    psql -X -v ON_ERROR_STOP=1 -h 127.0.0.1 -U postgres -d postgres \
    -c "CREATE DATABASE $database OWNER cag_db_owner TEMPLATE template0 ENCODING 'UTF8' LC_COLLATE 'C' LC_CTYPE 'C'" \
    >/dev/null
  docker exec -e PGPASSWORD="$test_password" "$active_container" \
    psql -X -v ON_ERROR_STOP=1 -h 127.0.0.1 -U postgres -d "$database" \
    --set=cloud_agents_database="$database" \
    --set=cloud_agents_database_owner=cag_db_owner \
    -f /workspace/services/control-plane/migrations/bootstrap/database.sql \
    >/dev/null
}

drop_database() {
  local database=$1
  docker exec -e PGPASSWORD="$test_password" "$active_container" \
    psql -X -v ON_ERROR_STOP=1 -h 127.0.0.1 -U postgres -d postgres \
    -c "DROP DATABASE IF EXISTS $database WITH (FORCE)" >/dev/null
}

apply_migrations() {
  local database=$1
  local migration
  for migration in "${migrations[@]}"; do
    docker exec -e PGPASSWORD="$test_password" "$active_container" \
      psql -X -v ON_ERROR_STOP=1 --single-transaction \
      -h 127.0.0.1 -U cag_migration -d "$database" \
      -c 'SET ROLE cloud_agents_migration_owner' \
      -f "/workspace/services/control-plane/migrations/$migration" \
      >/dev/null
  done
}

snapshot_data() {
  local database=$1
  local output=$2
  docker exec -i -e PGPASSWORD="$test_password" "$active_container" \
    psql -X -Atq -v ON_ERROR_STOP=1 -h 127.0.0.1 \
    -U postgres -d "$database" >"$output" <<'SQL'
SET TIME ZONE 'UTC';
SELECT pg_catalog.format(
    'SELECT %L || E''\t'' || pg_catalog.count(*)::text || E''\t'' || '
        || 'COALESCE(pg_catalog.jsonb_agg(pg_catalog.to_jsonb(data_row) '
        || 'ORDER BY pg_catalog.to_jsonb(data_row)::text)::text, ''[]'') '
        || 'FROM %I.%I AS data_row;',
    namespace.nspname || '.' || relation.relname,
    namespace.nspname,
    relation.relname
)
FROM pg_catalog.pg_class AS relation
JOIN pg_catalog.pg_namespace AS namespace
    ON namespace.oid = relation.relnamespace
WHERE namespace.nspname = 'cloud_agents'
    AND relation.relkind IN ('r', 'p')
ORDER BY namespace.nspname, relation.relname
\gexec
SELECT pg_catalog.format(
    'SELECT %L || E''\t'' || last_value::text || E''\t'' || is_called::text '
        || 'FROM %I.%I;',
    'sequence:' || namespace.nspname || '.' || relation.relname,
    namespace.nspname,
    relation.relname
)
FROM pg_catalog.pg_class AS relation
JOIN pg_catalog.pg_namespace AS namespace
    ON namespace.oid = relation.relnamespace
WHERE namespace.nspname = 'cloud_agents'
    AND relation.relkind = 'S'
ORDER BY namespace.nspname, relation.relname
\gexec
SQL
}

snapshot_schema() {
  local database=$1
  local output=$2
  docker exec -e PGPASSWORD="$test_password" "$active_container" \
    pg_dump -h 127.0.0.1 -U postgres -d "$database" \
    --schema-only --no-owner --quote-all-identifiers >"$output"
}

seed_source() {
  local database=$1
  local postgres_major=$2
  local coordination_registry='sha256:11c0f599e8320668a6f601241206c795933b26e3b9c456a58353a0d13c7ecd30'
  local coordination_profile='sha256:059b4cca58f9621e9b70b723fb3b681f62948d6d4965af60105165afce680d5a'
  local coordination_state='sha256:5c4fa5c0cfac253b45a41c2e49ee7e863b9efbe124e5d743e041f5e01f5c6f15'
  local coordination_policy='sha256:95023973eb007a958a3c5aea3ac61b6caa7cd8955b9a24fcef3ad269230c64e8'
  local result

  result=$(query_as cag_bootstrap "$database" \
    "SELECT * FROM cloud_agents.bootstrap_platform_tenant('tenant-restore','tenant-restore','Restore fixture','audit-restore-bootstrap','bootstrap')")
  assert_equal "$result" 'tenant-restore|1' "tenant bootstrap"

  docker exec -i -e PGPASSWORD="$test_password" "$active_container" \
    psql -X -v ON_ERROR_STOP=1 -h 127.0.0.1 \
    -U cag_migration -d "$database" >/dev/null <<SQL
SET ROLE cloud_agents_migration_owner;
BEGIN;
SET CONSTRAINTS ALL DEFERRED;
INSERT INTO cloud_agents.resource_changes (
    tenant_id, tenant_uid, resource_version, resource_kind, resource_uid,
    change_kind, actor_database_principal, occurred_at
) VALUES
    ('tenant-restore', 'tenant-restore', 2, 'organization', 'organization-restore',
     'created', 'cag_migration', '2026-08-22T00:00:01Z'),
    ('tenant-restore', 'tenant-restore', 3, 'project', 'project-restore',
     'created', 'cag_migration', '2026-08-22T00:00:02Z');
INSERT INTO cloud_agents.organizations (
    tenant_id, tenant_ref_id, organization_uid, organization_name, display_name,
    state, resource_version, created_at, updated_at
) VALUES (
    'tenant-restore', 'tenant-restore', 'organization-restore',
    'organization-restore', 'Restore Organization', 'active', 2,
    '2026-08-22T00:00:01Z', '2026-08-22T00:00:01Z'
);
INSERT INTO cloud_agents.projects (
    tenant_id, tenant_ref_id, project_uid, project_name, organization_uid,
    display_name, state, resource_version, created_at, updated_at
) VALUES (
    'tenant-restore', 'tenant-restore', 'project-restore', 'project-restore',
    'organization-restore', 'Restore Project', 'active', 3,
    '2026-08-22T00:00:02Z', '2026-08-22T00:00:02Z'
);
UPDATE cloud_agents.tenant_resource_versions
SET current_revision = 3, updated_at = '2026-08-22T00:00:02Z'
WHERE tenant_id = 'tenant-restore' AND tenant_uid = 'tenant-restore';
INSERT INTO cloud_agents.idempotency_records (
    tenant_id, tenant_ref_id, subject_digest, registry_digest, profile_id,
    profile_digest, idempotency_key, request_digest, state, resource_kind,
    resource_id, resource_version, created_at, updated_at, expires_at, terminal_at
) VALUES (
    'tenant-restore', 'tenant-restore', '$coordination_registry',
    '$coordination_registry', 'managedAgentCreateProject/v1alpha1',
    '$coordination_profile', 'restore-idempotency', '$coordination_state',
    'succeeded', 'project', 'project-restore', 3,
    '2026-08-22T00:00:03Z', '2026-08-22T00:00:03Z',
    '2026-08-23T00:00:03Z', '2026-08-22T00:00:03Z'
);
INSERT INTO cloud_agents.outbox_events (
    tenant_id, tenant_ref_id, event_id, registry_digest, profile_id,
    profile_digest, event_class, aggregate_kind, aggregate_id,
    aggregate_sequence, resource_version, generation, operation_id,
    operation_generation, payload_digest, state, delivery_attempts,
    available_at, created_at, updated_at
) VALUES (
    'tenant-restore', 'tenant-restore', 'restore-event',
    '$coordination_registry', 'managedAgentCreateProject/v1alpha1',
    '$coordination_profile', 'resource_change', 'project', 'project-restore',
    3, 3, 0, NULL, NULL, '$coordination_policy', 'pending', 0,
    '2026-08-22T00:00:04Z', '2026-08-22T00:00:04Z', '2026-08-22T00:00:04Z'
);
INSERT INTO cloud_agents.coordination_audit_facts (
    tenant_id, tenant_ref_id, audit_fact_id, registry_digest, profile_id,
    profile_digest, subject_digest, resource_kind, resource_id,
    resource_version, transition, outcome, occurred_at
) VALUES (
    'tenant-restore', 'tenant-restore', 'restore-coordination-audit',
    '$coordination_registry', 'managedAgentCreateProject/v1alpha1',
    '$coordination_profile', '$coordination_registry', 'project',
    'project-restore', 3, 'resource_created', 'succeeded',
    '2026-08-22T00:00:04Z'
);
INSERT INTO cloud_agents.leader_leases (
    leader_name, holder_id, holder_incarnation, fencing_token,
    lease_started_at, lease_expires_at, updated_at
) VALUES (
    'restore-fixture-leader', 'restore-holder', 'restore-incarnation', 7,
    '2026-08-22T00:00:00Z', '2026-08-22T00:01:00Z',
    '2026-08-22T00:00:00Z'
);
COMMIT;
SQL

  result=$(query_as cag_migration "$database" \
    "SET ROLE cloud_agents_migration_owner; SET cloud_agents.tenant_id='tenant-restore'; SELECT result_code,state,version FROM cloud_agents.compatibility_recovery_workload_principal_register_v2('tenant-restore','restore-workload','postgres','restore-principal',1,'sha256:1111111111111111111111111111111111111111111111111111111111111111','sha256:1212121212121212121212121212121212121212121212121212121212121212'); SELECT result_code,state,version FROM cloud_agents.compatibility_recovery_backfill_start_v2('tenant-restore','000011','expand','restore-cursor','sha256:1313131313131313131313131313131313131313131313131313131313131313',1,'sha256:1414141414141414141414141414141414141414141414141414141414141414','sha256:1515151515151515151515151515151515151515151515151515151515151515');")
  assert_equal "$result" $'applied|active|1\napplied|pending|1' \
    "compatibility seed"

  result=$(query_as cag_runtime "$database" \
    "SET cloud_agents.tenant_id='tenant-restore'; SELECT result_code,state,version FROM cloud_agents.compatibility_recovery_live_instance_register_v2('tenant-restore','control-plane','restore-instance',1,1,1,'v1.0.0','000001','000011',3600,'sha256:1616161616161616161616161616161616161616161616161616161616161616','sha256:1717171717171717171717171717171717171717171717171717171717171717'); SELECT result_code,state,version FROM cloud_agents.compatibility_recovery_live_instance_activate_v2('tenant-restore','control-plane','restore-instance',1,1,1,2,'sha256:1818181818181818181818181818181818181818181818181818181818181818','sha256:1919191919191919191919191919191919191919191919191919191919191919');")
  assert_equal "$result" $'applied|registered|1\napplied|active|2' \
    "live instance seed"

  result=$(query_as cag_runtime "$database" \
    "SET cloud_agents.tenant_id='tenant-restore'; SELECT result_code,state,COALESCE(stable_error_code,'') FROM cloud_agents.compatibility_recovery_migration_preflight_evaluate_v2('tenant-restore',$postgres_major,'$ledger_checksum','$schema_bundle_digest','000011',1,2,'sha256:2020202020202020202020202020202020202020202020202020202020202020',false,NULL)")
  assert_equal "$result" 'observed|rejected|preflight_restore_evidence_mismatch' \
    "preflight without actual restore evidence"
}

record_restore_evidence() {
  local database=$1
  local postgres_major=$2
  local dump_digest=$3
  local evidence_digest=$4
  local record_transition=$5
  local record_request=$6
  local complete_transition=$7
  local complete_request=$8
  local result

  result=$(query_as cag_migration "$database" \
    "SET ROLE cloud_agents_migration_owner; SET cloud_agents.tenant_id='tenant-restore'; SELECT result_code,state,version FROM cloud_agents.compatibility_recovery_restore_evidence_record_v2('tenant-restore','local-logical-restore-$postgres_major',$postgres_major,'$ledger_checksum','$schema_bundle_digest','000011','$dump_digest','$evidence_digest','2026-08-22T00:00:00Z','$record_transition','$record_request'); SELECT result_code,state,version FROM cloud_agents.compatibility_recovery_restore_evidence_complete_v2('tenant-restore','local-logical-restore-$postgres_major',1,'$evidence_digest','$complete_transition','$complete_request');")
  assert_equal "$result" $'applied|recorded|1\napplied|complete|2' \
    "actual restore evidence lifecycle"

  result=$(query_as cag_runtime "$database" \
    "SET cloud_agents.tenant_id='tenant-restore'; SELECT result_code,state,decision,COALESCE(stable_error_code,''),writer_epoch FROM cloud_agents.compatibility_recovery_migration_preflight_evaluate_v2('tenant-restore',$postgres_major,'$ledger_checksum','$schema_bundle_digest','000011',1,2,'$evidence_digest',false,NULL)")
  assert_equal "$result" 'observed|approved|approved||2' \
    "preflight after actual restore evidence"
}

trap cleanup EXIT
trap 'on_signal 130' INT
trap 'on_signal 143' TERM

printf 'local-logical-restore-source\tcommit=%s\ttree=%s\tdirty=%s\tledger=%s\tschema_bundle=%s\n' \
  "$source_commit" "$source_tree" "$source_dirty" \
  "$ledger_checksum" "$schema_bundle_digest"

for matrix_entry in "${matrix[@]}"; do
  IFS='|' read -r postgres_major expected_version image <<<"$matrix_entry"
  if ! docker image inspect "$image" >/dev/null 2>&1; then
    echo "Missing exact local image: $image" >&2
    exit 1
  fi

  active_container="cag-p1-local-restore-pg${postgres_major}-$$-$RANDOM"
  test_password="cag-local-only-${postgres_major}-$$"
  docker run -d --pull=never \
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

  actual_version=$(query_as postgres postgres 'SHOW server_version_num')
  assert_equal "$actual_version" "$expected_version" "PostgreSQL version"
  platform=$(docker image inspect --format '{{.Os}}/{{.Architecture}}' "$image")

  docker exec -e PGPASSWORD="$test_password" "$active_container" \
    psql -X -v ON_ERROR_STOP=1 -h 127.0.0.1 -U postgres -d postgres \
    -f /workspace/services/control-plane/migrations/bootstrap/roles.sql \
    >/dev/null

  if [[ $postgres_major -eq 15 ]]; then
    membership_grants=$'GRANT cloud_agents_migration_owner TO cag_migration;\nGRANT cloud_agents_runtime TO cag_runtime;\nGRANT cloud_agents_bootstrap_admin TO cag_bootstrap;'
  else
    membership_grants=$'GRANT cloud_agents_migration_owner TO cag_migration WITH INHERIT FALSE, SET TRUE;\nGRANT cloud_agents_runtime TO cag_runtime WITH INHERIT TRUE, SET TRUE;\nGRANT cloud_agents_bootstrap_admin TO cag_bootstrap WITH INHERIT TRUE, SET TRUE;'
  fi

  docker exec -i -e PGPASSWORD="$test_password" "$active_container" \
    psql -X -v ON_ERROR_STOP=1 -h 127.0.0.1 -U postgres -d postgres \
    >/dev/null <<SQL
CREATE ROLE cag_db_owner NOLOGIN NOSUPERUSER NOINHERIT NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS;
CREATE ROLE cag_migration LOGIN NOSUPERUSER NOINHERIT NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS PASSWORD '$test_password';
CREATE ROLE cag_runtime LOGIN NOSUPERUSER INHERIT NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS PASSWORD '$test_password';
CREATE ROLE cag_bootstrap LOGIN NOSUPERUSER INHERIT NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS PASSWORD '$test_password';
$membership_grants
SQL

  bootstrap_database cagsource
  apply_migrations cagsource
  seed_source cagsource "$postgres_major"

  source_data="$tmp_dir/pg${postgres_major}-source-data.tsv"
  source_schema="$tmp_dir/pg${postgres_major}-source-schema.sql"
  restored_data="$tmp_dir/pg${postgres_major}-restored-data.tsv"
  restored_schema="$tmp_dir/pg${postgres_major}-restored-schema.sql"
  dump_file="$tmp_dir/pg${postgres_major}.dump"
  dump_toc="$tmp_dir/pg${postgres_major}-toc.txt"
  corrupt_dump="$tmp_dir/pg${postgres_major}-corrupt.dump"

  snapshot_data cagsource "$source_data"
  snapshot_schema cagsource "$source_schema"
  source_data_digest="sha256:$(sha256_file "$source_data")"
  source_schema_digest="sha256:$(sha256_file "$source_schema")"
  table_count=$(wc -l <"$source_data" | tr -d ' ')
  row_count=$(awk -F '\t' '$1 !~ /^sequence:/ { total += $2 } END { print total + 0 }' "$source_data")

  docker exec -e PGPASSWORD="$test_password" "$active_container" \
    pg_dump -h 127.0.0.1 -U postgres -d cagsource \
    --format=custom --compress=0 --no-owner >"$dump_file"
  dump_digest="sha256:$(sha256_file "$dump_file")"
  docker exec -i "$active_container" pg_restore --list \
    <"$dump_file" >"$dump_toc"
  toc_digest="sha256:$(sha256_file "$dump_toc")"

  dump_size=$(wc -c <"$dump_file" | tr -d ' ')
  if [[ $dump_size -le 2048 ]]; then
    echo "Logical dump is unexpectedly small: $dump_size bytes" >&2
    exit 1
  fi
  head -c 2048 "$dump_file" >"$corrupt_dump"
  bootstrap_database cagcorrupt
  if docker exec -i -e PGPASSWORD="$test_password" "$active_container" \
    pg_restore --exit-on-error --no-owner \
    --role=cloud_agents_migration_owner \
    -h 127.0.0.1 -U cag_migration -d cagcorrupt \
    <"$corrupt_dump" >"$tmp_dir/pg${postgres_major}-corrupt-restore.log" 2>&1; then
    echo "Truncated logical dump unexpectedly restored" >&2
    exit 1
  fi
  drop_database cagcorrupt

  bootstrap_database cagrestore
  docker exec -i -e PGPASSWORD="$test_password" "$active_container" \
    pg_restore --exit-on-error --no-owner \
    --role=cloud_agents_migration_owner \
    -h 127.0.0.1 -U cag_migration -d cagrestore \
    <"$dump_file" >"$tmp_dir/pg${postgres_major}-restore.log" 2>&1

  snapshot_data cagrestore "$restored_data"
  snapshot_schema cagrestore "$restored_schema"
  restored_data_digest="sha256:$(sha256_file "$restored_data")"
  restored_schema_digest="sha256:$(sha256_file "$restored_schema")"
  assert_equal "$restored_data_digest" "$source_data_digest" \
    "restored data digest"
  assert_equal "$restored_schema_digest" "$source_schema_digest" \
    "restored schema digest"
  if ! cmp -s "$source_data" "$restored_data"; then
    echo "Restored canonical data bytes differ from source" >&2
    exit 1
  fi
  if ! cmp -s "$source_schema" "$restored_schema"; then
    echo "Restored schema bytes differ from source" >&2
    exit 1
  fi

  source_after="$tmp_dir/pg${postgres_major}-source-after-data.tsv"
  snapshot_data cagsource "$source_after"
  assert_equal "sha256:$(sha256_file "$source_after")" "$source_data_digest" \
    "source data changed during backup/restore"

  evidence_digest="sha256:$(sha256_text "cloud-agents-local-logical-restore-evidence/v1
postgres_major=$postgres_major
image=$image
version=$actual_version
dump=$dump_digest
toc=$toc_digest
source_data=$source_data_digest
restored_data=$restored_data_digest
source_schema=$source_schema_digest
restored_schema=$restored_schema_digest
ledger=$ledger_checksum
schema_bundle=$schema_bundle_digest")"
  record_transition="sha256:$(sha256_text "restore-record/$postgres_major/$evidence_digest")"
  record_request="sha256:$(sha256_text "restore-record-request/$postgres_major/$evidence_digest")"
  complete_transition="sha256:$(sha256_text "restore-complete/$postgres_major/$evidence_digest")"
  complete_request="sha256:$(sha256_text "restore-complete-request/$postgres_major/$evidence_digest")"
  record_restore_evidence cagrestore "$postgres_major" "$dump_digest" \
    "$evidence_digest" "$record_transition" "$record_request" \
    "$complete_transition" "$complete_request"

  tenant_rows=$(query_as cag_runtime cagrestore \
    "SET cloud_agents.tenant_id='tenant-restore'; SELECT (SELECT count(*) FROM cloud_agents.platform_tenants),(SELECT count(*) FROM cloud_agents.projects),(SELECT count(*) FROM cloud_agents.idempotency_records),(SELECT count(*) FROM cloud_agents.outbox_events)")
  assert_equal "$tenant_rows" '1|1|1|1' "restored tenant-visible rows"
  other_tenant_rows=$(query_as cag_runtime cagrestore \
    "SET cloud_agents.tenant_id='tenant-other'; SELECT (SELECT count(*) FROM cloud_agents.platform_tenants),(SELECT count(*) FROM cloud_agents.projects),(SELECT count(*) FROM cloud_agents.idempotency_records),(SELECT count(*) FROM cloud_agents.outbox_events)")
  assert_equal "$other_tenant_rows" '0|0|0|0' "restored cross-tenant isolation"

  printf 'local-logical-restore\tpg=%s\tversion=%s\timage=%s\tplatform=%s\tdump=%s\ttoc=%s\tschema=%s\tdata=%s\tevidence=%s\ttables=%s\trows=%s\ttruncated=REJECTED\tpreflight=APPROVED\n' \
    "$postgres_major" "$actual_version" "$image" "$platform" \
    "$dump_digest" "$toc_digest" "$source_schema_digest" \
    "$source_data_digest" "$evidence_digest" "$table_count" "$row_count"

  cleanup_container
done

trap - EXIT INT TERM
cleanup
echo "local-logical-restore: PG15/16/17 dump/restore/digest/preflight matrix PASS"
