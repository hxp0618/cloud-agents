#!/usr/bin/env bash

set -euo pipefail
umask 077

repository_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
postgres_image=${CLOUD_AGENTS_DEV_POSTGRES_IMAGE:-postgres:17.6-bookworm}
control_plane_listen=${CLOUD_AGENTS_DEV_CONTROL_PLANE_LISTEN:-127.0.0.1:8080}
worker_listen=${CLOUD_AGENTS_DEV_WORKER_LISTEN:-127.0.0.1:8091}
identity_port=${CLOUD_AGENTS_DEV_IDENTITY_PORT:-8443}
admin_port=${CLOUD_AGENTS_DEV_ADMIN_PORT:-4174}
user_port=${CLOUD_AGENTS_DEV_USER_PORT:-4173}
admin_origin="https://localhost:$admin_port"
user_origin="https://127.0.0.1:$user_port"
identity_origin="https://127.0.0.1:$identity_port"
credential_directory=${CLOUD_AGENTS_DEV_PROVIDER_CREDENTIALS_DIR:-}
docker_credential_directory=${CLOUD_AGENTS_DEV_DOCKER_CREDENTIALS_DIR:-}
runtime_max_sessions=${CLOUD_AGENTS_DEV_RUNTIME_MAX_SESSIONS:-4}
export CLOUD_AGENT_PROVIDER_HOST_EXPERIMENTAL_PROVIDERS="${CLOUD_AGENT_PROVIDER_HOST_EXPERIMENTAL_PROVIDERS-codex,claudeAgent,pi,deepseek-harness}"
run_id="${UID:-0}-$$"
container_name="cloud-agents-dev-$run_id"
state_parent="$repository_root/.tmp"
mkdir -p "$state_parent"
state_directory=$(mktemp -d "$state_parent/cloud-agents-dev.XXXXXX")
chmod 700 "$state_directory"
worker_pid=
control_plane_pid=
identity_pid=
admin_pid=
user_pid=

cleanup() {
  status=$?
  trap - EXIT INT TERM HUP
  for pid in "$admin_pid" "$user_pid" "$control_plane_pid" "$identity_pid" "$worker_pid"; do
    if [[ -n $pid ]]; then
      kill "$pid" 2>/dev/null || true
      wait "$pid" 2>/dev/null || true
    fi
  done
  docker rm -f "$container_name" >/dev/null 2>&1 || true
  rm -rf -- "$state_directory"
  exit "$status"
}
trap cleanup EXIT INT TERM HUP

workspace_directory=${CLOUD_AGENTS_DEV_WORKSPACE_DIRECTORY:-$state_directory/workspace}
if [[ -z ${CLOUD_AGENTS_DEV_WORKSPACE_DIRECTORY:-} ]]; then
  mkdir -p "$workspace_directory"
fi

for command in bun curl docker go node od openssl; do
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
export GOTOOLCHAIN=local
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
if [[ -n $docker_credential_directory && (! -d $docker_credential_directory || $docker_credential_directory != /*) ]]; then
  echo "CLOUD_AGENTS_DEV_DOCKER_CREDENTIALS_DIR must be an absolute directory" >&2
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
  -f /workspace/services/control-plane/migrations/bootstrap/roles.sql \
  -f /workspace/services/control-plane/migrations/bootstrap/roles_identity_service.sql >/dev/null
docker exec -e PGPASSWORD="$password" "$container_name" \
  psql -X -v ON_ERROR_STOP=1 --single-transaction -h 127.0.0.1 -U postgres -d cloud_agents_dev \
  -v cloud_agents_database=cloud_agents_dev \
  -v cloud_agents_migration_password="$password" \
  -v cloud_agents_runtime_password="$password" \
  -v cloud_agents_tenant_bootstrap_password="$password" \
  -v cloud_agents_identity_service_password="$password" \
  -v cloud_agents_identity_bootstrap_password="$password" \
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
go -C "$repository_root/services/control-plane" build -o "$state_directory/bin/cloud-agents-control-plane" ./cmd/cloud-agents-control-plane
go -C "$repository_root/services/worker" build -o "$state_directory/bin/cloud-agents-worker" ./cmd/cloud-agents-worker
go -C "$repository_root/services/control-plane" build -o "$state_directory/bin/cloud-agents-identity" ./cmd/cloud-agents-identity
for package in sdk/typescript apps/admin-web apps/user-web packages/cloud-agent-distribution; do
  bun run --cwd "$repository_root/$package" build
done
runtime_command="$repository_root/packages/cloud-agent-distribution/dist/stdio.mjs"
chmod 755 "$runtime_command"

"$state_directory/bin/cloud-agents-product-migrate" \
  --database-url "$migration_database_url" \
  --repository-root "$repository_root" >"$state_directory/migration.json"
# Generate only task-owned installation material; the same production binaries consume it.
CLOUD_AGENTS_DEV_STATE="$state_directory" CLOUD_AGENTS_DEV_DB_PASSWORD="$password" \
CLOUD_AGENTS_DEV_DB_PORT="$postgres_port" CLOUD_AGENTS_DEV_CP="$control_plane_listen" \
CLOUD_AGENTS_DEV_ADMIN="$admin_origin" CLOUD_AGENTS_DEV_USER="$user_origin" \
CLOUD_AGENTS_DEV_IDENTITY="$identity_origin" node --input-type=module <<'NODE'
import { randomBytes } from "node:crypto";
import { writeFileSync, chmodSync } from "node:fs";
import { execFileSync } from "node:child_process";
const env = process.env, dir = env.CLOUD_AGENTS_DEV_STATE;
const file = (name, value) => { const path = `${dir}/${name}`; writeFileSync(path, value, {mode:0o600,flag:"wx"}); return path; };
const run = (...args) => execFileSync("openssl", args, {stdio:"ignore"});
run("req","-x509","-newkey","rsa:2048","-nodes","-days","2","-subj","/CN=Cloud Agents local development CA","-addext","basicConstraints=critical,CA:TRUE","-keyout",`${dir}/ca.key`,"-out",`${dir}/ca.crt`);
for (const name of ["server", "worker", "control-plane-client"]) {
  run("req","-new","-newkey","rsa:2048","-nodes","-subj",`/CN=${name}`,"-keyout",`${dir}/${name}.key`,"-out",`${dir}/${name}.csr`);
  const ext = file(`${name}.ext`, `basicConstraints=CA:FALSE
keyUsage=digitalSignature,keyEncipherment
extendedKeyUsage=serverAuth,clientAuth
subjectAltName=IP:127.0.0.1,DNS:localhost,URI:spiffe://cloud-agents.local/${name}
`);
  run("x509","-req","-in",`${dir}/${name}.csr`,"-CA",`${dir}/ca.crt`,"-CAkey",`${dir}/ca.key`,"-set_serial",`0x${randomBytes(16).toString("hex")}`,"-days","2","-extfile",ext,"-out",`${dir}/${name}.crt`);
  chmodSync(`${dir}/${name}.key`,0o600);
}
run("genpkey","-algorithm","RSA","-pkeyopt","rsa_keygen_bits:2048","-out",`${dir}/signing.key`);
chmodSync(`${dir}/signing.key`,0o600);
const proof = name => file(name, randomBytes(32).toString("base64url"));
const database = user => `postgres://cloud_agents_${user}:${env.CLOUD_AGENTS_DEV_DB_PASSWORD}@127.0.0.1:${env.CLOUD_AGENTS_DEV_DB_PORT}/cloud_agents_dev?sslmode=disable`;
const common = {issuer:env.CLOUD_AGENTS_DEV_IDENTITY,adminAudience:env.CLOUD_AGENTS_DEV_ADMIN,userAudience:env.CLOUD_AGENTS_DEV_USER};
const signing = {signingKeyId:"identity-local",signingPrivateKeyFile:`${dir}/signing.key`};
const serviceProofs = {adminWebCredentialFile:proof("admin-web.proof"),userWebCredentialFile:proof("user-web.proof"),controlPlaneCredentialFile:proof("control-plane.proof"),controlPlaneAuthorizationCredentialFile:proof("identity-control-plane.proof")};
const password = randomBytes(24).toString("base64url");
file("initial-password",password);
file("admin-account.json",JSON.stringify({email:"admin@example.com",password}));
file("identity-initialize.json",JSON.stringify({...common,...signing,bootstrapDatabaseUrlFile:file("identity-bootstrap-database-url",database("identity_bootstrap")),setupProofFile:proof("setup.proof"),userId:"local",email:"admin@example.com",displayName:"Local administrator",passwordHashFile:`${dir}/password.hash`,keyNotBefore:new Date(Date.now()-60000).toISOString().replace(/\.\d{3}Z$/, "Z"),keyNotAfter:new Date(Date.now()+86400000).toISOString().replace(/\.\d{3}Z$/, "Z")}));
file("identity-run.json",JSON.stringify({...common,...signing,...serviceProofs,listen:`127.0.0.1:${new URL(env.CLOUD_AGENTS_DEV_IDENTITY).port}`,databaseUrlFile:file("identity-database-url",database("identity_service_login")),tlsCertificateFile:`${dir}/server.crt`,tlsPrivateKeyFile:`${dir}/server.key`,csrfKeyFile:file("csrf.key",randomBytes(32)),providerFlowKeyFile:file("provider-flow.key",randomBytes(32)),providerSecretFiles:{},providerRootCaFiles:{},controlPlaneUrl:`https://${env.CLOUD_AGENTS_DEV_CP}`,controlPlaneRootCaFile:`${dir}/ca.crt`}));
file("control-plane-identity.json",JSON.stringify({...common,identityBaseUrl:common.issuer,caFile:`${dir}/ca.crt`,controlPlaneCredentialFile:serviceProofs.controlPlaneCredentialFile,identityCredentialFile:serviceProofs.controlPlaneAuthorizationCredentialFile}));
file("access-grant.key",randomBytes(32));
proof("worker-admission.proof");
NODE
"$state_directory/bin/cloud-agents-identity" hash-password --password-file "$state_directory/initial-password" --output-file "$state_directory/password.hash"
"$state_directory/bin/cloud-agents-identity" initialize --config "$state_directory/identity-initialize.json" >"$state_directory/identity-initialize.log"
docker exec -e PGPASSWORD="$password" "$container_name" \
  psql -X -v ON_ERROR_STOP=1 -h 127.0.0.1 -U cloud_agents_tenant_bootstrap -d cloud_agents_dev \
  -v cloud_agents_tenant_uid=tenant-local \
  -v cloud_agents_tenant_name=tenant-local \
  -v cloud_agents_tenant_display_name='Local Tenant' \
  -v cloud_agents_organization_uid=organization-local \
  -v cloud_agents_organization_name=organization-local \
  -v cloud_agents_organization_display_name='Local Organization' \
  -v cloud_agents_admin_subject_kind=user \
  -v cloud_agents_admin_subject_issuer="$identity_origin" \
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

if [[ -z $credential_directory ]]; then
  credential_directory="$state_directory/provider-credentials"
  mkdir "$credential_directory"
fi
if [[ -z $docker_credential_directory ]]; then
  docker_credential_directory="$state_directory/docker-credentials"
  mkdir "$docker_credential_directory"
fi
"$state_directory/bin/cloud-agents-worker" \
  --listen "$worker_listen" --tls-cert "$state_directory/worker.crt" --tls-key "$state_directory/worker.key" \
  --client-ca "$state_directory/ca.crt" --worker-spiffe-id spiffe://cloud-agents.local/worker \
  --admission-lease-id local-runtime --admission-generation 1 --admission-token-file "$state_directory/worker-admission.proof" \
  --runtime-command "$runtime_command" --runtime-directory "$workspace_directory" \
  --runtime-max-sessions "$runtime_max_sessions" --provider-credential-directory "$credential_directory" >"$state_directory/worker.log" 2>&1 &
worker_pid=$!
"$state_directory/bin/cloud-agents-identity" run --config "$state_directory/identity-run.json" >"$state_directory/identity.log" 2>&1 &
identity_pid=$!

for attempt in {1..120}; do
  if curl -fsS --cacert "$state_directory/ca.crt" "$identity_origin/.well-known/jwks.json" >"$state_directory/jwks.json" 2>/dev/null; then break; fi
  if ! kill -0 "$identity_pid" 2>/dev/null || [[ $attempt == 120 ]]; then cat "$state_directory/identity.log" >&2; exit 1; fi
  sleep 1
done

CLOUD_AGENTS_PLATFORM_PROVIDER_CREDENTIALS_DIRECTORY="$credential_directory" \
CLOUD_AGENTS_PLATFORM_DOCKER_CREDENTIALS_DIRECTORY="$docker_credential_directory" \
CLOUD_AGENTS_PLATFORM_ADMISSION_TOKEN="$(cat "$state_directory/worker-admission.proof")" \
"$state_directory/bin/cloud-agents-control-plane" \
  --listen "$control_plane_listen" --database-url "$runtime_database_url" \
  --auth-config "$state_directory/control-plane-identity.json" \
  --tls-cert "$state_directory/server.crt" --tls-key "$state_directory/server.key" \
  --worker-endpoint "https://$worker_listen" --worker-spiffe-id spiffe://cloud-agents.local/worker \
  --worker-client-cert "$state_directory/control-plane-client.crt" --worker-client-key "$state_directory/control-plane-client.key" --worker-ca "$state_directory/ca.crt" \
  --admission-lease-id local-runtime --admission-generation 1 \
  --access-grant-key-file "$state_directory/access-grant.key" \
  --workspace-directory "$workspace_directory" >"$state_directory/control-plane.log" 2>&1 &
control_plane_pid=$!

for attempt in {1..120}; do
  if curl -fsS --cacert "$state_directory/ca.crt" "https://$control_plane_listen/readyz" >"$state_directory/control-plane-readiness.json" 2>/dev/null; then break; fi
  if ! kill -0 "$control_plane_pid" 2>/dev/null || [[ $attempt == 120 ]]; then cat "$state_directory/control-plane.log" >&2; exit 1; fi
  sleep 1
done

for application in admin user; do
  if [[ $application == admin ]]; then origin=$admin_origin; peer_origin=$user_origin; port=$admin_port;
  else origin=$user_origin; peer_origin=$admin_origin; port=$user_port; fi
  CLOUD_AGENTS_WEB_SCOPE="$application" CLOUD_AGENTS_WEB_PORT="$port" \
  CLOUD_AGENTS_WEB_ORIGIN="$origin" CLOUD_AGENTS_WEB_PEER_ORIGIN="$peer_origin" \
  CLOUD_AGENTS_WEB_ROOT="$repository_root/apps/$application-web/dist" \
  CLOUD_AGENTS_WEB_TLS_CERT_FILE="$state_directory/server.crt" CLOUD_AGENTS_WEB_TLS_KEY_FILE="$state_directory/server.key" \
  CLOUD_AGENTS_WEB_IDENTITY_URL="$identity_origin" CLOUD_AGENTS_WEB_IDENTITY_CA_FILE="$state_directory/ca.crt" \
  CLOUD_AGENTS_WEB_IDENTITY_CREDENTIAL_FILE="$state_directory/$application-web.proof" \
  CLOUD_AGENTS_WEB_CONTROL_PLANE_URL="https://$control_plane_listen" CLOUD_AGENTS_WEB_CONTROL_PLANE_CA_FILE="$state_directory/ca.crt" \
  node "$repository_root/deploy/web/server.mjs" >"$state_directory/$application-web.log" 2>&1 &
  pid=$!
  if [[ $application == admin ]]; then admin_pid=$pid; else user_pid=$pid; fi
  for attempt in {1..60}; do
    if curl -fsS --cacert "$state_directory/ca.crt" "$origin/" >/dev/null 2>&1; then break; fi
    if ! kill -0 "$pid" 2>/dev/null || [[ $attempt == 60 ]]; then cat "$state_directory/$application-web.log" >&2; exit 1; fi
    sleep 1
  done
done

echo "Cloud Agents local development stack is ready."
echo "State directory: $state_directory"
echo "Control Plane: https://$control_plane_listen"
echo "Admin Web: $admin_origin"
echo "User Web: $user_origin"
echo "Local CA: $state_directory/ca.crt (trust this task-owned CA in your browser)"
echo "Initial administrator: admin@example.com"
echo "Initial password file: $state_directory/initial-password"
printf 'CLI: %q login --web-endpoint %q --control-plane-endpoint %q --application admin --ca-file %q --profile %q\n' \
  "$state_directory/bin/cloud-agentsctl" "$admin_origin" "https://$control_plane_listen" "$state_directory/ca.crt" "$state_directory/admin.profile"
echo "Press Ctrl-C to stop and remove this temporary stack."

while kill -0 "$worker_pid" "$identity_pid" "$control_plane_pid" "$admin_pid" "$user_pid" 2>/dev/null; do sleep 1; done
cat "$state_directory/worker.log" "$state_directory/identity.log" "$state_directory/control-plane.log" "$state_directory/admin-web.log" "$state_directory/user-web.log" >&2
exit 1
