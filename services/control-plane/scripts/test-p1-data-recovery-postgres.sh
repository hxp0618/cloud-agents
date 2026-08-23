#!/usr/bin/env bash

set -euo pipefail

script_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
repo_root=$(cd -- "$script_dir/../../.." && pwd -P)
module_dir="$repo_root/services/control-plane"
source "$script_dir/p1-data-recovery-cleanup.sh"
manifest_path="$module_dir/migrations/manifest.json"
postgres_image="postgres@sha256:742f40ea20b9ff2ff31db5458d127452988a2164df9e17441e191f3b72252193"
expected_server_version="170010"
ownership_label="com.hxp0618.cloud-agents.test-run"
run_id="p1-data-recovery-$$-$RANDOM"
test_password="cag-local-only-recovery-$$"
source_container=""
restore_container=""
artifact_dir=$(mktemp -d "${TMPDIR:-/tmp}/cag-p1-data-recovery.XXXXXX")

if [[ $(go version | awk '{print $3}') != "go1.26.6" ]]; then
  echo "Go 1.26.6 is required" >&2
  exit 1
fi
if ! docker version >/dev/null 2>&1; then
  echo "A running Docker daemon is required" >&2
  exit 1
fi
if ! docker image inspect "$postgres_image" >/dev/null 2>&1; then
  echo "Missing exact local image: $postgres_image" >&2
  echo "Pull it explicitly before rerunning; this matrix never pulls implicitly." >&2
  exit 1
fi

sha256_file() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  else
    shasum -a 256 "$1" | awk '{print $1}'
  fi
}

remove_owned_container() {
  local container=$1
  local observed_owner
  if [[ -z $container ]] || ! docker container inspect "$container" >/dev/null 2>&1; then
    return 0
  fi
  observed_owner=$(docker container inspect --format "{{index .Config.Labels \"$ownership_label\"}}" "$container")
  if [[ $observed_owner != "$run_id" ]]; then
    echo "Refusing to remove container not owned by this run: $container" >&2
    return 1
  fi
  docker rm -f "$container" >/dev/null
}

cleanup() {
  local status=0
  remove_owned_container "$source_container" || status=1
  remove_owned_container "$restore_container" || status=1
  source_container=""
  restore_container=""
  if [[ -d $artifact_dir ]]; then
    find "$artifact_dir" -type f -exec chmod u+rw {} + 2>/dev/null || true
    rm -rf "$artifact_dir" || status=1
    if [[ -e $artifact_dir ]]; then
      status=1
    fi
  fi
  return "$status"
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

GOWORK=off GOTOOLCHAIN=local GOFLAGS=-mod=readonly \
  go -C "$module_dir" run ./scripts/data-recovery-validator \
    --manifest "$manifest_path" \
    --repo-root "$repo_root" \
    --mode ledger-tsv >"$artifact_dir/ledger.tsv"
GOWORK=off GOTOOLCHAIN=local GOFLAGS=-mod=readonly \
  go -C "$module_dir" run ./scripts/data-recovery-validator \
    --manifest "$manifest_path" \
    --repo-root "$repo_root" \
    --mode apply-sql \
    --container-repo-root /workspace/repo \
    --container-ledger-path /workspace/run/ledger.tsv >"$artifact_dir/apply.sql"

start_postgres() {
  local container=$1
  local actual_version
  docker run -d \
    --pull=never \
    --name "$container" \
    --label "$ownership_label=$run_id" \
    -p 127.0.0.1::5432 \
    -e POSTGRES_PASSWORD="$test_password" \
    -e POSTGRES_INITDB_ARGS='--encoding=UTF8 --locale=C' \
    -v "$repo_root:/workspace/repo:ro" \
    -v "$artifact_dir:/workspace/run:rw" \
    "$postgres_image" >/dev/null

  local ready_count=0
  local attempt
  for attempt in $(seq 1 90); do
    if docker exec "$container" pg_isready -h 127.0.0.1 -U postgres -d postgres >/dev/null 2>&1; then
      ready_count=$((ready_count + 1))
      if [[ $ready_count -ge 2 ]]; then
        break
      fi
    else
      ready_count=0
    fi
    if [[ $attempt -eq 90 ]]; then
      docker logs "$container" >&2
      return 1
    fi
    sleep 1
  done
  actual_version=$(docker exec -e PGPASSWORD="$test_password" "$container" \
    psql -X -At -h 127.0.0.1 -U postgres -d postgres -c 'SHOW server_version_num')
  if [[ $actual_version != "$expected_server_version" ]]; then
    echo "Unexpected PostgreSQL version: expected $expected_server_version, got $actual_version" >&2
    return 1
  fi
}

setup_cluster_database() {
  local container=$1
  local database=$2
  docker exec -e PGPASSWORD="$test_password" "$container" \
    psql -X -v ON_ERROR_STOP=1 -h 127.0.0.1 -U postgres -d postgres \
    -f /workspace/repo/services/control-plane/migrations/bootstrap/roles.sql >/dev/null
  docker exec -i -e PGPASSWORD="$test_password" "$container" \
    psql -X -v ON_ERROR_STOP=1 -h 127.0.0.1 -U postgres -d postgres <<SQL >/dev/null
CREATE ROLE cag_db_owner NOLOGIN NOSUPERUSER NOINHERIT NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS;
CREATE ROLE cag_migration LOGIN NOSUPERUSER NOINHERIT NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS PASSWORD '$test_password';
CREATE ROLE cag_runtime LOGIN NOSUPERUSER INHERIT NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS PASSWORD '$test_password';
CREATE ROLE cag_bootstrap LOGIN NOSUPERUSER INHERIT NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS PASSWORD '$test_password';
GRANT cloud_agents_migration_owner TO cag_migration WITH INHERIT FALSE, SET TRUE;
GRANT cloud_agents_runtime TO cag_runtime WITH INHERIT TRUE, SET TRUE;
GRANT cloud_agents_bootstrap_admin TO cag_bootstrap WITH INHERIT TRUE, SET TRUE;
CREATE DATABASE $database OWNER cag_db_owner TEMPLATE template0 ENCODING 'UTF8' LC_COLLATE 'C' LC_CTYPE 'C';
SQL
  docker exec -e PGPASSWORD="$test_password" "$container" \
    psql -X -v ON_ERROR_STOP=1 -h 127.0.0.1 -U postgres -d postgres \
    -f /workspace/repo/services/control-plane/migrations/bootstrap/roles.sql >/dev/null
  docker exec -e PGPASSWORD="$test_password" "$container" \
    psql -X -v ON_ERROR_STOP=1 -h 127.0.0.1 -U postgres -d "$database" \
    --set=cloud_agents_database="$database" \
    --set=cloud_agents_database_owner=cag_db_owner \
    -f /workspace/repo/services/control-plane/migrations/bootstrap/database.sql >/dev/null
}

host_port() {
  local port
  port=$(docker port "$1" 5432/tcp | awk -F: 'NR == 1 { print $NF }')
  if [[ ! $port =~ ^[0-9]+$ ]]; then
    echo "Unable to resolve PostgreSQL host port for $1: $port" >&2
    return 1
  fi
  printf '%s\n' "$port"
}

export_ledger() {
  local container=$1
  local database=$2
  local output=$3
  docker exec -e PGPASSWORD="$test_password" "$container" \
    psql -X -qAt -v ON_ERROR_STOP=1 -h 127.0.0.1 -U postgres -d "$database" \
    -c "COPY (SELECT migration_id,migration_name,predecessor_id,phase,schema_from,schema_to,compatible_binary_min,compatible_binary_max,sql_path,sql_size_bytes,sql_sha256,bundle_digest,transaction_mode,reentrancy,rollback_boundary,requires_live_instance_preflight,requires_pitr_preflight FROM cloud_agents.schema_migrations ORDER BY migration_id) TO STDOUT WITH (FORMAT text)" \
    >"$output"
}

verify_ledger() {
  local container=$1
  local database=$2
  local output=$3
  export_ledger "$container" "$database" "$output"
  if ! cmp -s "$artifact_dir/ledger.tsv" "$output"; then
    echo "Restored migration ledger does not match the current manifest" >&2
    diff -u "$artifact_dir/ledger.tsv" "$output" >&2 || true
    return 1
  fi
}

snapshot_database() {
  local container=$1
  local database=$2
  local output=$3
  local table
  local sequence
  {
    printf 'database\tcloud-agents-logical-recovery\n'
    docker exec -e PGPASSWORD="$test_password" "$container" \
      psql -X -qAt -v ON_ERROR_STOP=1 -h 127.0.0.1 -U postgres -d "$database" \
      -c "SELECT 'server_version' || E'\\t' || pg_catalog.current_setting('server_version_num')"
    docker exec -e PGPASSWORD="$test_password" "$container" \
      psql -X -qAt -v ON_ERROR_STOP=1 -h 127.0.0.1 -U postgres -d "$database" \
      -c "SELECT c.relname FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='cloud_agents' AND c.relkind IN ('r','p') ORDER BY c.relname" |
      while IFS= read -r table; do
        if [[ ! $table =~ ^[a-z0-9_]+$ ]]; then
          echo "Unsafe table name in canonical snapshot: $table" >&2
          return 1
        fi
        printf 'table\t%s\n' "$table"
        docker exec -e PGPASSWORD="$test_password" "$container" \
          psql -X -qAt -v ON_ERROR_STOP=1 -h 127.0.0.1 -U postgres -d "$database" \
          -c "COPY (SELECT pg_catalog.to_jsonb(row_value)::text FROM cloud_agents.\"$table\" AS row_value ORDER BY pg_catalog.to_jsonb(row_value)::text) TO STDOUT WITH (FORMAT text)"
      done
    docker exec -e PGPASSWORD="$test_password" "$container" \
      psql -X -qAt -v ON_ERROR_STOP=1 -h 127.0.0.1 -U postgres -d "$database" \
      -c "SELECT c.relname FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='cloud_agents' AND c.relkind='S' ORDER BY c.relname" |
      while IFS= read -r sequence; do
        if [[ ! $sequence =~ ^[a-z0-9_]+$ ]]; then
          echo "Unsafe sequence name in canonical snapshot: $sequence" >&2
          return 1
        fi
        printf 'sequence\t%s\n' "$sequence"
        docker exec -e PGPASSWORD="$test_password" "$container" \
          psql -X -qAt -v ON_ERROR_STOP=1 -h 127.0.0.1 -U postgres -d "$database" \
          -c "SELECT last_value::text || E'\\t' || is_called::text FROM cloud_agents.\"$sequence\""
      done
  } >"$output"
}

seed_recovery_tenant() {
  local container=$1
  local database=$2
  local bootstrap_result
  bootstrap_result=$(docker exec -e PGPASSWORD="$test_password" "$container" \
    psql -X -At -v ON_ERROR_STOP=1 -h 127.0.0.1 -U cag_bootstrap -d "$database" \
    -c "SELECT * FROM cloud_agents.bootstrap_platform_tenant('tenant-data-recovery','tenant-data-recovery','Data Recovery Tenant','audit-data-recovery-bootstrap','bootstrap');")
  if [[ $bootstrap_result != "tenant-data-recovery|1" ]]; then
    echo "Unexpected recovery tenant bootstrap result: $bootstrap_result" >&2
    return 1
  fi
  docker exec -i -e PGPASSWORD="$test_password" "$container" \
    psql -X -v ON_ERROR_STOP=1 -h 127.0.0.1 -U cag_migration -d "$database" <<'SQL' >/dev/null
SET ROLE cloud_agents_migration_owner;
BEGIN;
INSERT INTO cloud_agents.resource_changes (
    tenant_id, tenant_uid, resource_version, resource_kind, resource_uid,
    change_kind, actor_database_principal, occurred_at
) VALUES
    ('tenant-data-recovery','tenant-data-recovery',2,'organization','organization-recovery','created','cag_migration','2026-08-23T00:00:00Z'),
    ('tenant-data-recovery','tenant-data-recovery',3,'project','project-recovery','created','cag_migration','2026-08-23T00:00:01Z'),
    ('tenant-data-recovery','tenant-data-recovery',4,'membership','membership-admin-recovery','created','cag_migration','2026-08-23T00:00:02Z'),
    ('tenant-data-recovery','tenant-data-recovery',5,'role_binding','role-binding-admin-recovery','created','cag_migration','2026-08-23T00:00:03Z');
INSERT INTO cloud_agents.organizations (
    tenant_id, tenant_ref_id, organization_uid, organization_name, display_name,
    state, resource_version, created_at, updated_at
) VALUES (
    'tenant-data-recovery','tenant-data-recovery','organization-recovery','organization-recovery',
    'Recovery Organization','active',2,'2026-08-23T00:00:00Z','2026-08-23T00:00:00Z'
);
INSERT INTO cloud_agents.projects (
    tenant_id, tenant_ref_id, project_uid, project_name, organization_uid, display_name,
    state, resource_version, created_at, updated_at
) VALUES (
    'tenant-data-recovery','tenant-data-recovery','project-recovery','project-recovery',
    'organization-recovery','Recovery Project','active',3,'2026-08-23T00:00:01Z','2026-08-23T00:00:01Z'
);
INSERT INTO cloud_agents.memberships (
    tenant_id, tenant_ref_id, membership_uid, membership_name,
    subject_kind, subject_issuer, subject_value, subject_digest,
    scope_level, scope_tenant_uid, scope_organization_uid, scope_project_uid,
    state, expires_at, resource_version, created_at, updated_at
) VALUES (
    'tenant-data-recovery','tenant-data-recovery','membership-admin-recovery','membership-admin-recovery',
    'user','https://identity.example.test/','user-admin',
    'sha256:0403af0ad3826bcac1d20a2a58361e97d94508730794a9b0b1bf94bd7ab7b2bc',
    'tenant','tenant-data-recovery',NULL,NULL,'active',NULL,4,
    '2026-08-23T00:00:02Z','2026-08-23T00:00:02Z'
);
INSERT INTO cloud_agents.role_bindings (
    tenant_id, tenant_ref_id, role_binding_uid, role_binding_name,
    subject_kind, subject_issuer, subject_value, subject_digest,
    role_name, role_version, scope_level,
    scope_tenant_uid, scope_organization_uid, scope_project_uid,
    state, expires_at, resource_version, created_at, updated_at
) VALUES (
    'tenant-data-recovery','tenant-data-recovery','role-binding-admin-recovery','role-binding-admin-recovery',
    'user','https://identity.example.test/','user-admin',
    'sha256:0403af0ad3826bcac1d20a2a58361e97d94508730794a9b0b1bf94bd7ab7b2bc',
    'tenant.admin',1,'tenant','tenant-data-recovery',NULL,NULL,'active',NULL,5,
    '2026-08-23T00:00:03Z','2026-08-23T00:00:03Z'
);
UPDATE cloud_agents.tenant_resource_versions
SET current_revision=5, updated_at='2026-08-23T00:00:03Z'
WHERE tenant_id='tenant-data-recovery' AND tenant_uid='tenant-data-recovery';
COMMIT;
SQL
}

run_recovery_test() {
  local container=$1
  local database=$2
  local phase=$3
  local port
  port=$(host_port "$container")
  CLOUD_AGENTS_TEST_DATABASE_URL="postgres://cag_runtime:$test_password@127.0.0.1:$port/$database?sslmode=disable" \
  CLOUD_AGENTS_TEST_MIGRATION_DATABASE_URL="postgres://cag_migration:$test_password@127.0.0.1:$port/$database?sslmode=disable" \
  CLOUD_AGENTS_DATA_RECOVERY_PHASE="$phase" \
  CLOUD_AGENTS_DATA_RECOVERY_TENANT_ID='tenant-data-recovery' \
  CLOUD_AGENTS_REQUIRE_DATA_RECOVERY_TEST=1 \
  GOWORK=off GOTOOLCHAIN=local GOFLAGS=-mod=readonly \
    go -C "$module_dir" test ./internal/store/postgres \
      -run '^TestDurableCoordinationPostgresRecovery$' -count=1 -timeout=2m
}

source_container="cag-p1-data-source-$$-$RANDOM"
restore_container="cag-p1-data-restore-$$-$RANDOM"
start_postgres "$source_container"
setup_cluster_database "$source_container" cagtest
docker exec -e PGPASSWORD="$test_password" "$source_container" \
  psql -X -v ON_ERROR_STOP=1 -h 127.0.0.1 -U cag_migration -d cagtest \
  -f /workspace/run/apply.sql >/dev/null
verify_ledger "$source_container" cagtest "$artifact_dir/source-ledger.tsv"
seed_recovery_tenant "$source_container" cagtest
run_recovery_test "$source_container" cagtest prepare
snapshot_database "$source_container" cagtest "$artifact_dir/source.snapshot"
source_digest=$(sha256_file "$artifact_dir/source.snapshot")

docker exec -e PGPASSWORD="$test_password" "$source_container" \
  pg_dump --format=custom --compress=0 --no-owner \
    -h 127.0.0.1 -U postgres -d cagtest -f /workspace/run/data-recovery.dump
backup_digest=$(sha256_file "$artifact_dir/data-recovery.dump")
backup_size=$(wc -c <"$artifact_dir/data-recovery.dump" | tr -d ' ')
if [[ $backup_size -le 0 ]]; then
  echo "Logical backup artifact is empty" >&2
  exit 1
fi

start_postgres "$restore_container"
setup_cluster_database "$restore_container" cagrestore
docker exec -e PGPASSWORD="$test_password" "$restore_container" \
  pg_restore --exit-on-error --no-owner --role=cloud_agents_migration_owner \
    -h 127.0.0.1 -U cag_migration -d cagrestore /workspace/run/data-recovery.dump >/dev/null
verify_ledger "$restore_container" cagrestore "$artifact_dir/restored-ledger.tsv"
snapshot_database "$restore_container" cagrestore "$artifact_dir/restored.snapshot"
restored_digest=$(sha256_file "$artifact_dir/restored.snapshot")
if [[ $restored_digest != "$source_digest" ]]; then
  echo "Restored data digest mismatch: source=$source_digest restored=$restored_digest" >&2
  diff -u "$artifact_dir/source.snapshot" "$artifact_dir/restored.snapshot" >&2 || true
  exit 1
fi

run_recovery_test "$restore_container" cagrestore recover
snapshot_database "$restore_container" cagrestore "$artifact_dir/recovered.snapshot"
recovered_digest=$(sha256_file "$artifact_dir/recovered.snapshot")
image_id=$(docker image inspect --format '{{.Id}}' "$postgres_image")
printf -v pass_line \
  'p1-data-recovery: postgres=17 server_version=%s image=%s backup_sha256=%s backup_size=%s source_data_sha256=%s restored_data_sha256=%s recovered_data_sha256=%s PASS' \
  "$expected_server_version" "$image_id" "$backup_digest" "$backup_size" "$source_digest" "$restored_digest" "$recovered_digest"
p1_data_recovery_finish "$pass_line"
