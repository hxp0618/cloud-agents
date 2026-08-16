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

run_id="migration-projection-matrix-$$-$RANDOM"
ownership_label="com.hxp0618.cloud-agents.test-run"
active_container=""

cleanup() {
  if [[ -z "$active_container" ]]; then
    return 0
  fi
  if docker container inspect "$active_container" >/dev/null 2>&1; then
    observed_owner=$(docker container inspect \
      --format "{{index .Config.Labels \"$ownership_label\"}}" \
      "$active_container")
    if [[ $observed_owner != "$run_id" ]]; then
      echo "Refusing to remove container not owned by this run: $active_container" >&2
      return 1
    fi
    docker rm -f -v "$active_container" >/dev/null
  fi
  active_container=""
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
  local_image_id=$(docker image inspect --format '{{.Id}}' "$image")
  image_os=$(docker image inspect --format '{{.Os}}' "$image")
  image_arch=$(docker image inspect --format '{{.Architecture}}' "$image")
  if [[ $local_image_id != sha256:* || $image_os != "linux" || -z "$image_arch" ]]; then
    echo "Invalid local image evidence for $image" >&2
    exit 1
  fi

  major_same_bits=""
  for instance in A B; do
    active_container="cag-p1-projection-pg${postgres_major}-${instance}-$$-$RANDOM"
    test_password="cag-local-only-${postgres_major}-${instance}-$$-$RANDOM"
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

    if [[ $postgres_major -eq 15 ]]; then
      membership_grant='GRANT cloud_agents_migration_owner TO cag_migration;'
    else
      membership_grant='GRANT cloud_agents_migration_owner TO cag_migration WITH ADMIN FALSE, INHERIT FALSE, SET TRUE;'
    fi
    docker exec -i -e PGPASSWORD="$test_password" "$active_container" \
      psql -X -v ON_ERROR_STOP=1 -h 127.0.0.1 -U postgres -d postgres <<SQL >/dev/null
CREATE ROLE cag_db_owner NOLOGIN NOSUPERUSER NOINHERIT NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS;
CREATE ROLE cloud_agents_bootstrap_admin NOLOGIN NOSUPERUSER NOINHERIT NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS;
CREATE ROLE cloud_agents_migration_owner NOLOGIN NOSUPERUSER NOINHERIT NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS;
CREATE ROLE cloud_agents_runtime NOLOGIN NOSUPERUSER NOINHERIT NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS;
CREATE ROLE cag_migration LOGIN NOSUPERUSER NOINHERIT NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS PASSWORD '$test_password';
$membership_grant
CREATE DATABASE cag_projection OWNER cag_db_owner TEMPLATE template0 ENCODING 'UTF8' LC_COLLATE 'C' LC_CTYPE 'C';
REVOKE TEMPORARY ON DATABASE cag_projection FROM PUBLIC;
GRANT CREATE ON DATABASE cag_projection TO cloud_agents_migration_owner;
SQL

    docker exec -i -e PGPASSWORD="$test_password" "$active_container" \
      psql -X -v ON_ERROR_STOP=1 -h 127.0.0.1 -U postgres -d cag_projection <<SQL >/dev/null
ALTER DEFAULT PRIVILEGES FOR ROLE cloud_agents_migration_owner GRANT SELECT ON TABLES TO cag_migration;
SQL
    if [[ $postgres_major -eq 17 ]]; then
      docker exec -i -e PGPASSWORD="$test_password" "$active_container" \
        psql -X -v ON_ERROR_STOP=1 -h 127.0.0.1 -U postgres -d cag_projection <<SQL >/dev/null
ALTER DEFAULT PRIVILEGES FOR ROLE cloud_agents_migration_owner REVOKE MAINTAIN ON TABLES FROM cloud_agents_migration_owner;
SQL
    fi

    preflight=$(docker exec -e PGPASSWORD="$test_password" "$active_container" \
      psql -X -v ON_ERROR_STOP=1 -At -F '|' -h 127.0.0.1 -U postgres -d cag_projection \
      -c "SELECT current_setting('server_version_num'), pg_catalog.version(), current_setting('server_encoding'), d.datcollate, d.datctype FROM pg_catalog.pg_database d WHERE d.datname=pg_catalog.current_database();")
    IFS='|' read -r observed_version exact_version encoding datcollate datctype <<<"$preflight"
    if [[ $observed_version != "$expected_version" || $encoding != "UTF8" || $datcollate != "C" || $datctype != "C" ]]; then
      echo "Unexpected PostgreSQL $postgres_major/$instance preflight: $preflight" >&2
      exit 1
    fi
    host_port=$(docker port "$active_container" 5432/tcp | sed -E 's/.*:([0-9]+)$/\1/')
    admin_url="postgres://postgres:$test_password@127.0.0.1:$host_port/cag_projection?sslmode=disable"
    migration_url="postgres://cag_migration:$test_password@127.0.0.1:$host_port/cag_projection?sslmode=disable"

    set +e
    normal_output=$(CLOUD_AGENTS_PROJECTION_ADMIN_URL="$admin_url" \
      CLOUD_AGENTS_PROJECTION_MIGRATION_URL="$migration_url" \
      CLOUD_AGENTS_REQUIRE_POSTGRES_PROJECTION_TEST=1 \
      CLOUD_AGENTS_EXPECTED_POSTGRES_MAJOR="$postgres_major" \
      CLOUD_AGENTS_EXPECTED_POSTGRES_VERSION_NUM="$expected_version" \
      CLOUD_AGENTS_PROJECTION_INSTANCE="$instance" \
      CLOUD_AGENTS_PROJECTION_IMAGE_ID="$local_image_id" \
      CLOUD_AGENTS_PROJECTION_CONTAINER_ARCH="$image_os/$image_arch" \
      CLOUD_AGENTS_PROJECTION_PROFILE='UTF8/C/C' \
      GOWORK=off GOTOOLCHAIN=local GOFLAGS=-mod=readonly \
      go -C "$module_dir" test -timeout=10m -run '^TestPGProjectionPostgresMatrix$' -count=1 -v ./internal/migration)
    normal_status=$?
    set -e
    printf '%s\n' "$normal_output"
    if [[ $normal_status -ne 0 ]]; then
      echo "Normal projection matrix failed for PostgreSQL $postgres_major/$instance with exit $normal_status" >&2
      exit "$normal_status"
    fi
    normal_same_bits=$(printf '%s\n' "$normal_output" | sed -n 's/.*POSTGRES_PROJECTION_SAME_BITS .* digest=\(sha256:[0-9a-f]*\) .*/\1/p' | tail -1)
    if [[ -z "$normal_same_bits" ]]; then
      echo "Normal projection matrix did not emit same-bits evidence" >&2
      exit 1
    fi

    set +e
    race_output=$(CLOUD_AGENTS_PROJECTION_ADMIN_URL="$admin_url" \
      CLOUD_AGENTS_PROJECTION_MIGRATION_URL="$migration_url" \
      CLOUD_AGENTS_REQUIRE_POSTGRES_PROJECTION_TEST=1 \
      CLOUD_AGENTS_EXPECTED_POSTGRES_MAJOR="$postgres_major" \
      CLOUD_AGENTS_EXPECTED_POSTGRES_VERSION_NUM="$expected_version" \
      CLOUD_AGENTS_PROJECTION_INSTANCE="$instance" \
      CLOUD_AGENTS_PROJECTION_IMAGE_ID="$local_image_id" \
      CLOUD_AGENTS_PROJECTION_CONTAINER_ARCH="$image_os/$image_arch" \
      CLOUD_AGENTS_PROJECTION_PROFILE='UTF8/C/C' \
      GOWORK=off GOTOOLCHAIN=local GOFLAGS=-mod=readonly \
      go -C "$module_dir" test -race -timeout=10m -run '^TestPGProjectionPostgresMatrix$' -count=1 -v ./internal/migration)
    race_status=$?
    set -e
    printf '%s\n' "$race_output"
    if [[ $race_status -ne 0 ]]; then
      echo "Race projection matrix failed for PostgreSQL $postgres_major/$instance with exit $race_status" >&2
      exit "$race_status"
    fi
    race_same_bits=$(printf '%s\n' "$race_output" | sed -n 's/.*POSTGRES_PROJECTION_SAME_BITS .* digest=\(sha256:[0-9a-f]*\) .*/\1/p' | tail -1)
    if [[ $race_same_bits != "$normal_same_bits" ]]; then
      echo "Normal/race same-bits mismatch for PostgreSQL $postgres_major/$instance: $normal_same_bits vs $race_same_bits" >&2
      exit 1
    fi
    if [[ -z "$major_same_bits" ]]; then
      major_same_bits="$normal_same_bits"
    elif [[ $major_same_bits != "$normal_same_bits" ]]; then
      echo "Fresh database A/B same-bits mismatch for PostgreSQL $postgres_major: $major_same_bits vs $normal_same_bits" >&2
      exit 1
    fi

    echo "POSTGRES_PROJECTION_EVIDENCE major=$postgres_major instance=$instance version_num=$observed_version image_ref=$image local_image_id=$local_image_id arch=$image_os/$image_arch profile=UTF8/C/C same_bits=$normal_same_bits exact_version=$exact_version"
    cleanup
  done
done

residual=$(docker ps -aq --filter "label=$ownership_label=$run_id")
if [[ -n "$residual" ]]; then
  echo "Projection matrix left owned containers behind: $residual" >&2
  exit 1
fi

echo "Migration projection PostgreSQL 15/16/17 fresh A/B local matrix: PASS"
