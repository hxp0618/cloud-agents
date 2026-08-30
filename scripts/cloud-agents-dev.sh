#!/usr/bin/env bash

set -euo pipefail

repository_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
postgres_image=${CLOUD_AGENTS_DEV_POSTGRES_IMAGE:-postgres:17.6-bookworm}
control_plane_listen=${CLOUD_AGENTS_DEV_CONTROL_PLANE_LISTEN:-127.0.0.1:8080}
worker_listen=${CLOUD_AGENTS_DEV_WORKER_LISTEN:-127.0.0.1:8091}
workspace_directory=${CLOUD_AGENTS_DEV_WORKSPACE_DIRECTORY:-$repository_root}
credential_directory=${CLOUD_AGENTS_DEV_PROVIDER_CREDENTIALS_DIR:-}
runtime_max_sessions=${CLOUD_AGENTS_DEV_RUNTIME_MAX_SESSIONS:-4}
run_id="${UID:-0}-$$"
container_name="cloud-agents-dev-$run_id"
state_parent="$repository_root/.tmp"
mkdir -p "$state_parent"
state_directory=$(mktemp -d "$state_parent/cloud-agents-dev.XXXXXX")
chmod 700 "$state_directory"
worker_pid=
control_plane_pid=

cleanup() {
  status=$?
  trap - EXIT INT TERM HUP
  if [[ -n $control_plane_pid ]]; then
    kill "$control_plane_pid" 2>/dev/null || true
    wait "$control_plane_pid" 2>/dev/null || true
  fi
  if [[ -n $worker_pid ]]; then
    kill "$worker_pid" 2>/dev/null || true
    wait "$worker_pid" 2>/dev/null || true
  fi
  docker rm -f "$container_name" >/dev/null 2>&1 || true
  rm -rf -- "$state_directory"
  exit "$status"
}
trap cleanup EXIT INT TERM HUP

for command in bun curl docker go node od; do
  if ! command -v "$command" >/dev/null 2>&1; then
    echo "cloud-agents dev requires $command on PATH" >&2
    exit 1
  fi
done
if [[ $(bun --version) != 1.3.14 ]]; then
  echo "cloud-agents dev requires Bun 1.3.14" >&2
  exit 1
fi
node_version=$(node --version)
if [[ ! $node_version =~ ^v24\.([0-9]+)\.([0-9]+)$ ]] || ((BASH_REMATCH[1] < 18)) || ((BASH_REMATCH[1] == 18 && BASH_REMATCH[2] < 1)); then
  echo "cloud-agents dev requires Node.js >=24.18.1 <25" >&2
  exit 1
fi
export GOTOOLCHAIN=go1.26.6
export GOFLAGS=-mod=readonly
if [[ $(go version) != "go version go1.26.6 "* ]]; then
  echo "cloud-agents dev requires Go 1.26.6" >&2
  exit 1
fi
if [[ ! -d $workspace_directory || $workspace_directory != /* ]]; then
  echo "CLOUD_AGENTS_DEV_WORKSPACE_DIRECTORY must be an absolute directory" >&2
  exit 1
fi
if [[ -n $credential_directory && (! -d $credential_directory || $credential_directory != /*) ]]; then
  echo "CLOUD_AGENTS_DEV_PROVIDER_CREDENTIALS_DIR must be an absolute directory" >&2
  exit 1
fi
if [[ ! $runtime_max_sessions =~ ^[1-9][0-9]*$ ]] || ((runtime_max_sessions > 1024)); then
  echo "CLOUD_AGENTS_DEV_RUNTIME_MAX_SESSIONS must be between 1 and 1024" >&2
  exit 1
fi
if ! docker info >/dev/null 2>&1; then
  echo "cloud-agents dev requires a running Docker engine" >&2
  exit 1
fi

password=$(od -An -N32 -tx1 /dev/urandom | tr -d ' \n')
docker run -d --rm \
  --name "$container_name" \
  --label "cloud-agents.dev-run=$run_id" \
  -p 127.0.0.1::5432 \
  -e POSTGRES_DB=cloud_agents_dev \
  -e POSTGRES_PASSWORD="$password" \
  -e POSTGRES_INITDB_ARGS='--encoding=UTF8 --locale=C' \
  -v "$repository_root:/workspace:ro" \
  "$postgres_image" >/dev/null

for attempt in {1..90}; do
  if docker exec "$container_name" pg_isready -h 127.0.0.1 -U postgres -d cloud_agents_dev >/dev/null 2>&1; then
    break
  fi
  if [[ $attempt == 90 ]]; then
    docker logs "$container_name" >&2
    exit 1
  fi
  sleep 1
done

docker exec -e PGPASSWORD="$password" "$container_name" \
  psql -X -v ON_ERROR_STOP=1 -h 127.0.0.1 -U postgres -d cloud_agents_dev \
  -f /workspace/services/control-plane/migrations/bootstrap/roles.sql >/dev/null
docker exec -e PGPASSWORD="$password" "$container_name" \
  psql -X -v ON_ERROR_STOP=1 --single-transaction -h 127.0.0.1 -U postgres -d cloud_agents_dev \
  -v cloud_agents_database=cloud_agents_dev \
  -v cloud_agents_migration_password="$password" \
  -v cloud_agents_runtime_password="$password" \
  -v cloud_agents_tenant_bootstrap_password="$password" \
  -f /workspace/deploy/compose/provision.sql >/dev/null
docker exec -e PGPASSWORD="$password" "$container_name" \
  psql -X -v ON_ERROR_STOP=1 -h 127.0.0.1 -U postgres -d cloud_agents_dev \
  -v cloud_agents_database=cloud_agents_dev \
  -v cloud_agents_database_owner=cloud_agents_database_owner \
  -f /workspace/services/control-plane/migrations/bootstrap/database.sql >/dev/null

postgres_port=$(docker port "$container_name" 5432/tcp | awk -F: 'NR == 1 { print $NF }')
if [[ ! $postgres_port =~ ^[0-9]+$ ]]; then
  echo "cloud-agents dev could not resolve the PostgreSQL port" >&2
  exit 1
fi
migration_database_url="postgres://cloud_agents_migration:$password@127.0.0.1:$postgres_port/cloud_agents_dev?sslmode=disable"
runtime_database_url="postgres://cloud_agents_runtime_login:$password@127.0.0.1:$postgres_port/cloud_agents_dev?sslmode=disable"

mkdir -p "$state_directory/bin"
go -C "$repository_root/services/control-plane" build -o "$state_directory/bin/cloud-agents-product-migrate" ./cmd/cloud-agents-product-migrate
go -C "$repository_root/services/control-plane" build -o "$state_directory/bin/cloud-agentsctl" ./cmd/cloud-agentsctl
go -C "$repository_root/services/control-plane" build -tags=localdev -o "$state_directory/bin/cloud-agents-control-plane" ./cmd/cloud-agents-control-plane
go -C "$repository_root/services/worker" build -tags=localdev -o "$state_directory/bin/cloud-agents-worker" ./cmd/cloud-agents-worker
bun run --cwd "$repository_root/packages/cloud-agent-distribution" build
runtime_command="$repository_root/packages/cloud-agent-distribution/dist/stdio.mjs"
chmod 755 "$runtime_command"

"$state_directory/bin/cloud-agents-product-migrate" \
  --database-url "$migration_database_url" \
  --repository-root "$repository_root" >"$state_directory/migration.json"
docker exec -e PGPASSWORD="$password" "$container_name" \
  psql -X -v ON_ERROR_STOP=1 -h 127.0.0.1 -U cloud_agents_tenant_bootstrap -d cloud_agents_dev \
  -v cloud_agents_tenant_uid=tenant-local \
  -v cloud_agents_tenant_name=tenant-local \
  -v cloud_agents_tenant_display_name='Local Tenant' \
  -v cloud_agents_organization_uid=organization-local \
  -v cloud_agents_organization_name=organization-local \
  -v cloud_agents_organization_display_name='Local Organization' \
  -v cloud_agents_admin_subject_kind=user \
  -v cloud_agents_admin_subject_issuer=https://local.invalid/cloud-agents/authn \
  -v cloud_agents_admin_subject_value=user-local \
  -v cloud_agents_admin_membership_uid=membership-local-admin \
  -v cloud_agents_admin_membership_name=membership-local-admin \
  -v cloud_agents_admin_role_binding_uid=role-binding-local-admin \
  -v cloud_agents_admin_role_binding_name=role-binding-local-admin \
  -v cloud_agents_tenant_audit_fact_uid=audit-local-tenant \
  -v cloud_agents_membership_audit_fact_uid=audit-local-membership \
  -v cloud_agents_role_binding_audit_fact_uid=audit-local-role-binding \
  -v cloud_agents_bootstrap_reason_code=local-bootstrap \
  -f /workspace/deploy/helm/cloud-agents/files/tenant-bootstrap.sql >"$state_directory/bootstrap.log"

worker_arguments=(
  --listen "$worker_listen"
  --token-file "$state_directory/worker.token"
  --runtime-command "$runtime_command"
  --runtime-directory "$workspace_directory"
  --runtime-max-sessions "$runtime_max_sessions"
)
if [[ -n $credential_directory ]]; then
  worker_arguments+=(--provider-credential-directory "$credential_directory")
fi
"$state_directory/bin/cloud-agents-worker" "${worker_arguments[@]}" >"$state_directory/worker.log" 2>&1 &
worker_pid=$!

for attempt in {1..120}; do
  if [[ -f $state_directory/worker.token ]] && \
    curl -fsS -H "Authorization: Bearer $(tr -d '\n' <"$state_directory/worker.token")" \
      "http://$worker_listen/healthz" >"$state_directory/worker-health.json" 2>/dev/null; then
    break
  fi
  if ! kill -0 "$worker_pid" 2>/dev/null; then
    cat "$state_directory/worker.log" >&2
    exit 1
  fi
  if [[ $attempt == 120 ]]; then
    cat "$state_directory/worker.log" >&2
    exit 1
  fi
  sleep 1
done

"$state_directory/bin/cloud-agents-control-plane" \
  --listen "$control_plane_listen" \
  --database-url "$runtime_database_url" \
  --local-token-file "$state_directory/control-plane.token" \
  --local-tenant-id tenant-local \
  --local-subject user-local \
  --worker-endpoint "http://$worker_listen" \
  --worker-token-file "$state_directory/worker.token" \
  --workspace-directory "$workspace_directory" >"$state_directory/control-plane.log" 2>&1 &
control_plane_pid=$!

for attempt in {1..120}; do
  if [[ -f $state_directory/control-plane.token ]] && \
    curl -fsS "http://$control_plane_listen/readyz" >"$state_directory/control-plane-readiness.json" 2>/dev/null; then
    break
  fi
  if ! kill -0 "$control_plane_pid" 2>/dev/null; then
    cat "$state_directory/control-plane.log" >&2
    exit 1
  fi
  if [[ $attempt == 120 ]]; then
    cat "$state_directory/control-plane.log" >&2
    exit 1
  fi
  sleep 1
done

echo "Cloud Agents local development stack is ready."
echo "Control Plane: http://$control_plane_listen"
echo "Worker: http://$worker_listen"
echo "Tenant: tenant-local"
echo "Organization: organization-local"
echo "Token file: $state_directory/control-plane.token"
printf 'CLI: %q --endpoint %q --token-file %q --tenant tenant-local --request-id REQUEST_ID\n' \
  "$state_directory/bin/cloud-agentsctl" "http://$control_plane_listen" "$state_directory/control-plane.token"
echo "Press Ctrl-C to stop."

while kill -0 "$worker_pid" 2>/dev/null && kill -0 "$control_plane_pid" 2>/dev/null; do
  sleep 1
done
cat "$state_directory/worker.log" "$state_directory/control-plane.log" >&2
exit 1
