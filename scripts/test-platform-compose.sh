#!/bin/sh

set -eu

if [ "$#" -lt 1 ] || [ "$#" -gt 2 ] || [ ! -d "$1" ]; then
  echo "usage: test-platform-compose.sh PLATFORM_RELEASE_DIRECTORY [REAL_PROVIDER_CREDENTIALS_DIRECTORY]" >&2
  exit 2
fi
real_provider_credentials_directory=
if [ "$#" -eq 2 ]; then
  case "$2" in
    /*) ;;
    *) echo "real Provider credentials directory must be absolute" >&2; exit 2 ;;
  esac
  if [ ! -d "$2" ] || [ -L "$2" ]; then
    echo "real Provider credentials directory must be a non-symlink directory" >&2
    exit 2
  fi
  real_provider_credentials_directory=$(CDPATH= cd -- "$2" && pwd -P)
  for provider in codex claudeAgent; do
    credential_file="$real_provider_credentials_directory/tenant-compose-smoke.$provider.json"
    if [ ! -f "$credential_file" ] || [ -L "$credential_file" ]; then
      echo "real Provider credentials directory is missing a non-symlink tenant-compose-smoke.$provider.json" >&2
      exit 2
    fi
  done
fi
for command in curl docker node openssl; do
  if ! command -v "$command" >/dev/null 2>&1; then
    echo "platform Compose smoke requires $command" >&2
    exit 2
  fi
done
docker_host=$(docker context inspect --format '{{.Endpoints.docker.Host}}')
case "$docker_host" in
  unix:///*) docker_socket=${docker_host#unix://} ;;
  *) echo "platform Compose Docker target smoke requires a Unix Docker context" >&2; exit 2 ;;
esac

candidate_directory=$(CDPATH= cd -- "$1" && pwd)
case "$(uname -s)/$(uname -m)" in
  Darwin/arm64) cli_target=darwin-arm64; image_platform=linux/arm64 ;;
  Darwin/x86_64) cli_target=darwin-amd64; image_platform=linux/amd64 ;;
  Linux/aarch64 | Linux/arm64) cli_target=linux-arm64; image_platform=linux/arm64 ;;
  Linux/x86_64 | Linux/amd64) cli_target=linux-amd64; image_platform=linux/amd64 ;;
  *) echo "platform Compose smoke requires Darwin or Linux on amd64 or arm64" >&2; exit 2 ;;
esac
cli="$candidate_directory/cloud-agentsctl-$cli_target"
if [ ! -x "$cli" ]; then
  echo "platform release is missing executable cloud-agentsctl-$cli_target" >&2
  exit 1
fi
set -- "$candidate_directory"/cloud-agents-deployment-*.tar
if [ "$#" -ne 1 ] || [ ! -f "$1" ]; then
  echo "platform release must contain exactly one deployment package" >&2
  exit 1
fi

smoke_directory=$(mktemp -d "$candidate_directory/.compose-smoke.XXXXXX")
project="cloud-agents-compose-smoke-$$"
environment_file="$smoke_directory/compose.env"
compose_file="$smoke_directory/deployment/deploy/compose/docker-compose.yml"
compose_override_file="$smoke_directory/compose-target-override.yml"
docker_proxy_pid=
kubernetes_api_pid=
registry_container="${project}-registry"
worker_repository=
worker_release_digest=
worker_upgrade_release_digest=
target_worker_credentials_volume="${project}-target-worker-credentials"
target_provider_credentials_volume="${project}-target-provider-credentials"
retry_provider_credentials_volume="${project}-retry-provider-credentials"
project_id=
profile_environment_id=
compose() {
  docker compose --env-file "$environment_file" -f "$compose_file" -f "$compose_override_file" "$@"
}
cleanup() {
  status=$?
  trap - 0 HUP INT TERM
  if [ -n "$project_id" ]; then
    for container in $(docker ps -aq \
      --filter label=cloud-agents.dev/tenant=tenant-compose-smoke \
      --filter label=cloud-agents.dev/project="$project_id"); do
      if [ "$status" -ne 0 ]; then
        docker logs "$container" >&2 || true
      fi
      docker rm -f "$container" >/dev/null 2>&1 || true
    done
  fi
  if [ -f "$environment_file" ]; then
    if [ "$status" -ne 0 ]; then
      compose logs --no-color --tail=200 control-plane worker migrate postgres >&2 || true
    fi
    compose down --volumes --remove-orphans >/dev/null 2>&1 || true
  fi
  docker rm -f "$registry_container" >/dev/null 2>&1 || true
  docker rm -f "${project}-worker-upgrade-seed" >/dev/null 2>&1 || true
  docker volume rm "$target_worker_credentials_volume" "$target_provider_credentials_volume" \
    "$retry_provider_credentials_volume" >/dev/null 2>&1 || true
  if [ -n "$worker_repository" ]; then
    docker image rm "$worker_repository:smoke" "$worker_repository:upgrade" >/dev/null 2>&1 || true
  fi
  if [ -n "$worker_repository" ] && [ -n "$worker_release_digest" ]; then
    docker image rm "$worker_repository@$worker_release_digest" >/dev/null 2>&1 || true
  fi
  if [ -n "$worker_repository" ] && [ -n "$worker_upgrade_release_digest" ]; then
    docker image rm "$worker_repository@$worker_upgrade_release_digest" >/dev/null 2>&1 || true
  fi
  if [ -n "$docker_proxy_pid" ]; then
    kill "$docker_proxy_pid" >/dev/null 2>&1 || true
    wait "$docker_proxy_pid" 2>/dev/null || true
  fi
  if [ -n "$kubernetes_api_pid" ]; then
    kill "$kubernetes_api_pid" >/dev/null 2>&1 || true
    wait "$kubernetes_api_pid" 2>/dev/null || true
  fi
  docker image rm "${project}-control-plane" "${project}-worker" "${project}-migrate" >/dev/null 2>&1 || true
  rm -rf -- "$smoke_directory"
  exit "$status"
}
trap cleanup 0 HUP INT TERM

mkdir -p "$smoke_directory/deployment" "$smoke_directory/control-plane-tls" \
  "$smoke_directory/worker-tls" "$smoke_directory/provider-credentials" "$smoke_directory/workspace" \
  "$smoke_directory/docker-target-credentials/docker-compose-target" \
  "$smoke_directory/kubernetes-target-credentials" \
  "$smoke_directory/prepared-kubernetes-target-credentials" \
  "$smoke_directory/ssh-target-credentials" \
  "$smoke_directory/fake-kubectl-state" \
  "$smoke_directory/target-worker-credentials" "$smoke_directory/target-provider-credentials"
chmod 0755 "$smoke_directory" "$smoke_directory/control-plane-tls" \
  "$smoke_directory/worker-tls" "$smoke_directory/provider-credentials" \
  "$smoke_directory/docker-target-credentials" "$smoke_directory/docker-target-credentials/docker-compose-target" \
  "$smoke_directory/kubernetes-target-credentials" \
  "$smoke_directory/prepared-kubernetes-target-credentials" \
  "$smoke_directory/ssh-target-credentials" \
  "$smoke_directory/target-worker-credentials" "$smoke_directory/target-provider-credentials"
chmod 0700 "$smoke_directory/fake-kubectl-state"
chmod 0777 "$smoke_directory/workspace"
tar -xf "$1" -C "$smoke_directory/deployment"

openssl req -x509 -newkey rsa:2048 -nodes -sha256 -days 1 \
  -subj /CN=cloud-agents-compose-smoke-ca \
  -keyout "$smoke_directory/ca.key" -out "$smoke_directory/ca.crt" \
  -addext basicConstraints=critical,CA:TRUE -addext keyUsage=critical,keyCertSign,cRLSign >/dev/null 2>&1
openssl req -newkey rsa:2048 -nodes -sha256 -subj /CN=worker \
  -keyout "$smoke_directory/worker-tls/server.key" -out "$smoke_directory/worker.csr" \
  -addext subjectAltName=DNS:worker,URI:spiffe://cloud-agents.compose/worker \
  -addext extendedKeyUsage=serverAuth -addext keyUsage=digitalSignature >/dev/null 2>&1
openssl x509 -req -sha256 -days 1 -in "$smoke_directory/worker.csr" \
  -CA "$smoke_directory/ca.crt" -CAkey "$smoke_directory/ca.key" -CAcreateserial \
  -copy_extensions copy -out "$smoke_directory/worker-tls/server.crt" >/dev/null 2>&1
openssl req -newkey rsa:2048 -nodes -sha256 -subj /CN=host.docker.internal \
  -keyout "$smoke_directory/target-worker-credentials/server.key" \
  -out "$smoke_directory/target-worker.csr" \
  -addext subjectAltName=DNS:host.docker.internal,URI:spiffe://cloud-agents.compose/worker-target \
  -addext extendedKeyUsage=serverAuth -addext keyUsage=digitalSignature >/dev/null 2>&1
openssl x509 -req -sha256 -days 1 -in "$smoke_directory/target-worker.csr" \
  -CA "$smoke_directory/ca.crt" -CAkey "$smoke_directory/ca.key" -CAcreateserial \
  -copy_extensions copy -out "$smoke_directory/target-worker-credentials/server.crt" >/dev/null 2>&1
openssl req -newkey rsa:2048 -nodes -sha256 -subj /CN=control-plane-worker-client \
  -keyout "$smoke_directory/control-plane-tls/worker-client.key" \
  -out "$smoke_directory/control-plane-client.csr" \
  -addext subjectAltName=URI:spiffe://cloud-agents.compose/control-plane \
  -addext extendedKeyUsage=clientAuth -addext keyUsage=digitalSignature >/dev/null 2>&1
openssl x509 -req -sha256 -days 1 -in "$smoke_directory/control-plane-client.csr" \
  -CA "$smoke_directory/ca.crt" -CAkey "$smoke_directory/ca.key" -CAcreateserial \
  -copy_extensions copy -out "$smoke_directory/control-plane-tls/worker-client.crt" >/dev/null 2>&1
openssl req -newkey rsa:2048 -nodes -sha256 -subj /CN=control-plane \
  -keyout "$smoke_directory/control-plane-tls/server.key" \
  -out "$smoke_directory/control-plane-server.csr" \
  -addext subjectAltName=DNS:control-plane,IP:127.0.0.1 \
  -addext extendedKeyUsage=serverAuth -addext keyUsage=digitalSignature >/dev/null 2>&1
openssl x509 -req -sha256 -days 1 -in "$smoke_directory/control-plane-server.csr" \
  -CA "$smoke_directory/ca.crt" -CAkey "$smoke_directory/ca.key" -CAcreateserial \
  -copy_extensions copy -out "$smoke_directory/control-plane-tls/server.crt" >/dev/null 2>&1
cp "$smoke_directory/ca.crt" "$smoke_directory/control-plane-tls/worker-ca.crt"
cp "$smoke_directory/ca.crt" "$smoke_directory/worker-tls/client-ca.crt"
cp "$smoke_directory/ca.crt" "$smoke_directory/target-worker-credentials/client-ca.crt"

openssl req -x509 -newkey rsa:2048 -nodes -sha256 -days 1 \
  -subj /CN=cloud-agents-compose-docker-target-ca \
  -keyout "$smoke_directory/docker-target-ca.key" -out "$smoke_directory/docker-target-ca.crt" \
  -addext basicConstraints=critical,CA:TRUE -addext keyUsage=critical,keyCertSign,cRLSign >/dev/null 2>&1
openssl req -newkey rsa:2048 -nodes -sha256 -subj /CN=host.docker.internal \
  -keyout "$smoke_directory/docker-target-server.key" -out "$smoke_directory/docker-target-server.csr" \
  -addext subjectAltName=DNS:host.docker.internal,IP:127.0.0.1 \
  -addext extendedKeyUsage=serverAuth -addext keyUsage=digitalSignature >/dev/null 2>&1
openssl x509 -req -sha256 -days 1 -in "$smoke_directory/docker-target-server.csr" \
  -CA "$smoke_directory/docker-target-ca.crt" -CAkey "$smoke_directory/docker-target-ca.key" -CAcreateserial \
  -copy_extensions copy -out "$smoke_directory/docker-target-server.crt" >/dev/null 2>&1
openssl req -newkey rsa:2048 -nodes -sha256 -subj /CN=control-plane-docker-target-client \
  -keyout "$smoke_directory/docker-target-credentials/docker-compose-target/key.pem" \
  -out "$smoke_directory/docker-target-client.csr" \
  -addext extendedKeyUsage=clientAuth -addext keyUsage=digitalSignature >/dev/null 2>&1
openssl x509 -req -sha256 -days 1 -in "$smoke_directory/docker-target-client.csr" \
  -CA "$smoke_directory/docker-target-ca.crt" -CAkey "$smoke_directory/docker-target-ca.key" -CAcreateserial \
  -copy_extensions copy -out "$smoke_directory/docker-target-credentials/docker-compose-target/cert.pem" >/dev/null 2>&1
cp "$smoke_directory/docker-target-ca.crt" \
  "$smoke_directory/docker-target-credentials/docker-compose-target/ca.pem"
cp "$smoke_directory/docker-target-ca.crt" \
  "$smoke_directory/kubernetes-target-credentials/kubernetes-compose-target.ca.crt"
chmod 0444 "$smoke_directory"/control-plane-tls/* "$smoke_directory"/worker-tls/*

CLOUD_AGENTS_COMPOSE_SMOKE_STATE="$smoke_directory" \
CLOUD_AGENTS_COMPOSE_SMOKE_RELEASE="$candidate_directory" \
CLOUD_AGENTS_COMPOSE_SMOKE_PROJECT="$project" \
CLOUD_AGENTS_COMPOSE_SMOKE_PLATFORM="$image_platform" \
  node <<'NODE'
const { createSign, generateKeyPairSync, randomBytes } = require("node:crypto");
const { chmodSync, writeFileSync } = require("node:fs");

const state = process.env.CLOUD_AGENTS_COMPOSE_SMOKE_STATE;
const release = process.env.CLOUD_AGENTS_COMPOSE_SMOKE_RELEASE;
const project = process.env.CLOUD_AGENTS_COMPOSE_SMOKE_PROJECT;
const platform = process.env.CLOUD_AGENTS_COMPOSE_SMOKE_PLATFORM;
if (![state, release, project, platform].every((value) => value && !value.includes("\n"))) {
  throw new Error("invalid Compose smoke environment");
}
const deploy = `${state}/deployment/deploy`;
const issuer = "https://issuer.compose.test";
const audience = "https://api.compose.test";
const kid = "compose-smoke-key";
const now = Math.floor(Date.now() / 1000);
const { privateKey, publicKey } = generateKeyPairSync("rsa", { modulusLength: 2048 });
const exported = publicKey.export({ format: "jwk" });
const jwk = { alg: "RS256", e: exported.e, key_ops: ["verify"], kid, kty: "RSA", n: exported.n, use: "sig" };
const auth = {
  issuer, audience, generation: 1, securityEpoch: 1, notBefore: now - 60, expiresAt: now + 3600,
  keys: [{ jwk, enabled: true, notBefore: now - 60, notAfter: now + 3600 }],
};
writeFileSync(`${state}/auth.json`, `${JSON.stringify(auth)}\n`);
const baseClaims = {
  iss: issuer, sub: "user-compose-smoke", aud: audience, exp: now + 1800, iat: now - 10,
  client_id: "compose-smoke-client",
  "https://schemas.cloud-agents.dev/claims/security-epoch": 1,
  "https://schemas.cloud-agents.dev/claims/subject-kind": "user",
  "https://schemas.cloud-agents.dev/claims/tenant-id": "tenant-compose-smoke",
  "https://schemas.cloud-agents.dev/claims/token-profile": "cloud-agents-access-token/v1",
};
const encode = (value) => Buffer.from(JSON.stringify(value)).toString("base64url");
const issueToken = (tokenId, scopes) => {
  const claims = { ...baseClaims, jti: tokenId, scope: [...scopes].sort().join(" ") };
  const signingInput = `${encode({ alg: "RS256", kid, typ: "at+jwt" })}.${encode(claims)}`;
  const signature = createSign("RSA-SHA256").update(signingInput).end().sign(privateKey).toString("base64url");
  return `${signingInput}.${signature}`;
};
const adminToken = issueToken("compose-smoke-admin-token", [
  "audit.list", "environments.create", "environments.get", "environment-profiles.list",
  "leases.act", "leases.get", "leases.list", "organizations.list", "profiles.act",
  "operations.list", "profiles.create", "profiles.get", "profiles.list", "projects.act", "projects.create",
  "projects.get", "quotas.get", "quotas.update", "releases.create", "releases.list", "targets.act", "targets.create", "targets.get", "targets.list", "workers.list",
]);
const userToken = issueToken("compose-smoke-user-token", [
  "environment-quotas.get", "environments.create", "environments.get", "environment-profiles.list", "projects.act", "projects.get",
]);
const admissionToken = randomBytes(24).toString("hex");
const kubernetesToken = randomBytes(24).toString("hex");
writeFileSync(`${state}/token`, `${adminToken}\n`);
writeFileSync(`${state}/user-token`, `${userToken}\n`);
writeFileSync(`${state}/admin-curl.conf`, `header = "Authorization: Bearer ${adminToken}"\n`);
writeFileSync(`${state}/user-curl.conf`, `header = "Authorization: Bearer ${userToken}"\n`);
writeFileSync(`${state}/runtime.env`, "CLOUD_AGENT_PROVIDER_HOST_EXPERIMENTAL_PROVIDERS=codex,claudeAgent\nCLOUD_AGENT_PROVIDER_OUTER_SANDBOX_PROFILE=single-tenant-trusted-v1\n");
writeFileSync(`${state}/provider-credentials/tenant-compose-smoke.unavailable-provider.json`, '{"payload":{}}\n');
writeFileSync(`${state}/target-provider-credentials/tenant-compose-smoke.unavailable-provider.json`, '{"payload":{}}\n');
writeFileSync(`${state}/target-worker-credentials/admission-token`, admissionToken);
writeFileSync(`${state}/kubernetes-target-credentials/kubernetes-compose-target.token`, `${kubernetesToken}\n`);
writeFileSync(`${state}/compose-target-override.yml`, 'services:\n  control-plane:\n    extra_hosts:\n      - "host.docker.internal:host-gateway"\n');
writeFileSync(`${state}/docker-proxy.mjs`, [
  'import { chmodSync, readFileSync, writeFileSync } from "node:fs";',
  'import net from "node:net";',
  'import tls from "node:tls";',
  'const [socketPath, caPath, certPath, keyPath, portPath] = process.argv.slice(2);',
  'const server = tls.createServer({ ca: readFileSync(caPath), cert: readFileSync(certPath), key: readFileSync(keyPath), minVersion: "TLSv1.2", requestCert: true, rejectUnauthorized: true }, (client) => {',
  '  const upstream = net.createConnection(socketPath);',
  '  const close = () => { client.destroy(); upstream.destroy(); };',
  '  client.on("error", close); upstream.on("error", close);',
  '  client.pipe(upstream); upstream.pipe(client);',
  '});',
  'server.on("tlsClientError", () => {});',
  'server.listen(0, "0.0.0.0", () => { writeFileSync(portPath, String(server.address().port)); chmodSync(portPath, 0o600); });',
  'process.on("SIGTERM", () => process.exit(0));',
].join("\n") + "\n");
writeFileSync(`${state}/kubernetes-api.mjs`, [
  'import { chmodSync, readFileSync, writeFileSync } from "node:fs";',
  'import https from "node:https";',
  'const [certPath, keyPath, tokenPath, portPath] = process.argv.slice(2);',
  'const token = readFileSync(tokenPath, "utf8").trimEnd();',
  'const server = https.createServer({ cert: readFileSync(certPath), key: readFileSync(keyPath), minVersion: "TLSv1.2" }, (request, response) => {',
  '  if (request.method !== "GET" || request.headers.authorization !== `Bearer ${token}`) { response.writeHead(401).end(); return; }',
  '  response.setHeader("content-type", "application/json");',
  '  const path = new URL(request.url, "https://kubernetes.invalid").pathname;',
  '  if (path === "/version") { response.end(JSON.stringify({ major: "1", minor: "34+", gitVersion: "v1.34.2", platform: "linux/arm64" })); return; }',
  '  if (["/apis/apps/v1/namespaces/cloud-agents-target/deployments", "/api/v1/namespaces/cloud-agents-target/services", "/api/v1/namespaces/cloud-agents-target/persistentvolumeclaims"].includes(path)) { response.end(JSON.stringify({ metadata: {}, items: [] })); return; }',
  '  response.writeHead(404).end();',
  '});',
  'server.listen(0, "0.0.0.0", () => { writeFileSync(portPath, String(server.address().port)); chmodSync(portPath, 0o600); });',
  'process.on("SIGTERM", () => process.exit(0));',
].join("\n") + "\n");
const password = randomBytes(24).toString("hex");
const values = [
  `COMPOSE_PROJECT_NAME=${project}`,
  `CLOUD_AGENTS_RELEASE_DIR=${release}`,
  `CLOUD_AGENTS_DEPLOY_DIR=${deploy}`,
  `CLOUD_AGENTS_PLATFORM=${platform}`,
  "CLOUD_AGENTS_CONTROL_PLANE_BIND=127.0.0.1:",
  "CLOUD_AGENTS_WORKER_BIND=127.0.0.1:",
  "CLOUD_AGENTS_POSTGRES_DB=cloud_agents",
  `CLOUD_AGENTS_POSTGRES_INSTALL_PASSWORD=${password}`,
  `CLOUD_AGENTS_MIGRATION_PASSWORD=${password}`,
  `CLOUD_AGENTS_RUNTIME_PASSWORD=${password}`,
  `CLOUD_AGENTS_TENANT_BOOTSTRAP_PASSWORD=${password}`,
  `CLOUD_AGENTS_BOOTSTRAP_DATABASE_URL=postgresql://cloud_agents_install_admin:${password}@postgres:5432/cloud_agents`,
  `CLOUD_AGENTS_MIGRATION_DATABASE_URL=postgresql://cloud_agents_migration:${password}@postgres:5432/cloud_agents`,
  `CLOUD_AGENTS_RUNTIME_DATABASE_URL=postgresql://cloud_agents_runtime_login:${password}@postgres:5432/cloud_agents`,
  `CLOUD_AGENTS_TENANT_BOOTSTRAP_DATABASE_URL=postgresql://cloud_agents_tenant_bootstrap:${password}@postgres:5432/cloud_agents`,
  "CLOUD_AGENTS_TENANT_UID=tenant-compose-smoke",
  "CLOUD_AGENTS_TENANT_NAME=tenant-compose-smoke",
  "CLOUD_AGENTS_TENANT_DISPLAY_NAME=Compose Smoke Tenant",
  "CLOUD_AGENTS_ORGANIZATION_UID=organization-compose-smoke",
  "CLOUD_AGENTS_ORGANIZATION_NAME=organization-compose-smoke",
  "CLOUD_AGENTS_ORGANIZATION_DISPLAY_NAME=Compose Smoke Organization",
  "CLOUD_AGENTS_ADMIN_SUBJECT_KIND=user",
  `CLOUD_AGENTS_ADMIN_SUBJECT_ISSUER=${issuer}`,
  "CLOUD_AGENTS_ADMIN_SUBJECT_VALUE=user-compose-smoke",
  "CLOUD_AGENTS_ADMIN_MEMBERSHIP_UID=membership-compose-admin",
  "CLOUD_AGENTS_ADMIN_MEMBERSHIP_NAME=membership-compose-admin",
  "CLOUD_AGENTS_ADMIN_ROLE_BINDING_UID=role-binding-compose-admin",
  "CLOUD_AGENTS_ADMIN_ROLE_BINDING_NAME=role-binding-compose-admin",
  "CLOUD_AGENTS_TENANT_AUDIT_FACT_UID=audit-compose-tenant",
  "CLOUD_AGENTS_MEMBERSHIP_AUDIT_FACT_UID=audit-compose-membership",
  "CLOUD_AGENTS_ROLE_BINDING_AUDIT_FACT_UID=audit-compose-role-binding",
  "CLOUD_AGENTS_BOOTSTRAP_REASON_CODE=compose-smoke",
  `CLOUD_AGENTS_AUTH_CONFIG=${state}/auth.json`,
  `CLOUD_AGENTS_RUNTIME_ENV_FILE=${state}/runtime.env`,
  `CLOUD_AGENTS_PROVIDER_CREDENTIALS_DIR=${state}/provider-credentials`,
  `CLOUD_AGENTS_DOCKER_CREDENTIALS_DIR=${state}/docker-target-credentials`,
  `CLOUD_AGENTS_KUBERNETES_CREDENTIALS_DIR=${state}/kubernetes-target-credentials`,
  `CLOUD_AGENTS_SSH_CREDENTIALS_DIR=${state}/ssh-target-credentials`,
  `CLOUD_AGENTS_CONTROL_PLANE_TLS_DIR=${state}/control-plane-tls`,
  `CLOUD_AGENTS_WORKER_TLS_DIR=${state}/worker-tls`,
  "CLOUD_AGENTS_WORKER_ENDPOINT=https://worker:8091",
  "CLOUD_AGENTS_WORKER_SPIFFE_ID=spiffe://cloud-agents.compose/worker",
  `CLOUD_AGENTS_WORKER_CLIENT_CERT=${state}/control-plane-tls/worker-client.crt`,
  `CLOUD_AGENTS_WORKER_CLIENT_KEY=${state}/control-plane-tls/worker-client.key`,
  `CLOUD_AGENTS_WORKER_CA=${state}/control-plane-tls/worker-ca.crt`,
  `CLOUD_AGENTS_WORKSPACE_DIR=${state}/workspace`,
  "CLOUD_AGENTS_WORKER_WORKSPACE_DIRECTORY=/workspace",
  "CLOUD_AGENTS_RUNTIME_MAX_SESSIONS=2",
  "CLOUD_AGENTS_ADMISSION_LEASE_ID=compose-smoke-lease",
  "CLOUD_AGENTS_ADMISSION_GENERATION=1",
  `CLOUD_AGENTS_ADMISSION_TOKEN=${admissionToken}`,
];
writeFileSync(`${state}/compose.env`, `${values.join("\n")}\n`);
chmodSync(`${state}/auth.json`, 0o444);
chmodSync(`${state}/runtime.env`, 0o444);
chmodSync(`${state}/provider-credentials/tenant-compose-smoke.unavailable-provider.json`, 0o444);
chmodSync(`${state}/target-provider-credentials/tenant-compose-smoke.unavailable-provider.json`, 0o444);
chmodSync(`${state}/target-worker-credentials/admission-token`, 0o400);
chmodSync(`${state}/kubernetes-target-credentials/kubernetes-compose-target.token`, 0o400);
chmodSync(`${state}/docker-proxy.mjs`, 0o400);
chmodSync(`${state}/kubernetes-api.mjs`, 0o400);
chmodSync(`${state}/token`, 0o600);
chmodSync(`${state}/user-token`, 0o600);
chmodSync(`${state}/admin-curl.conf`, 0o600);
chmodSync(`${state}/user-curl.conf`, 0o600);
chmodSync(`${state}/compose.env`, 0o600);
NODE

chmod 0444 "$smoke_directory"/target-worker-credentials/server.* \
  "$smoke_directory"/target-worker-credentials/client-ca.crt \
  "$smoke_directory"/docker-target-credentials/docker-compose-target/*.pem \
  "$smoke_directory"/kubernetes-target-credentials/kubernetes-compose-target.ca.crt

docker_proxy_port_file="$smoke_directory/docker-proxy.port"
node "$smoke_directory/docker-proxy.mjs" "$docker_socket" \
  "$smoke_directory/docker-target-ca.crt" "$smoke_directory/docker-target-server.crt" \
  "$smoke_directory/docker-target-server.key" "$docker_proxy_port_file" &
docker_proxy_pid=$!
attempt=0
while [ ! -s "$docker_proxy_port_file" ]; do
  attempt=$((attempt + 1))
  if [ "$attempt" -ge 50 ] || ! kill -0 "$docker_proxy_pid" 2>/dev/null; then
    echo "Docker target mTLS proxy did not start" >&2
    exit 1
  fi
  sleep 0.1
done
docker_proxy_port=$(cat "$docker_proxy_port_file")
case "$docker_proxy_port" in
  '' | *[!0-9]*) echo "Docker target mTLS proxy returned an invalid port" >&2; exit 1 ;;
esac

kubernetes_api_port_file="$smoke_directory/kubernetes-api.port"
node "$smoke_directory/kubernetes-api.mjs" \
  "$smoke_directory/docker-target-server.crt" "$smoke_directory/docker-target-server.key" \
  "$smoke_directory/kubernetes-target-credentials/kubernetes-compose-target.token" \
  "$kubernetes_api_port_file" &
kubernetes_api_pid=$!
attempt=0
while [ ! -s "$kubernetes_api_port_file" ]; do
  attempt=$((attempt + 1))
  if [ "$attempt" -ge 50 ] || ! kill -0 "$kubernetes_api_pid" 2>/dev/null; then
    echo "Kubernetes target API smoke server did not start" >&2
    exit 1
  fi
  sleep 0.1
done
kubernetes_api_port=$(cat "$kubernetes_api_port_file")
case "$kubernetes_api_port" in
  '' | *[!0-9]*) echo "Kubernetes target API smoke server returned an invalid port" >&2; exit 1 ;;
esac
test "$(curl --silent --show-error --fail \
  --cacert "$smoke_directory/docker-target-ca.crt" \
  --cert "$smoke_directory/docker-target-credentials/docker-compose-target/cert.pem" \
  --key "$smoke_directory/docker-target-credentials/docker-compose-target/key.pem" \
  "https://127.0.0.1:$docker_proxy_port/_ping")" = OK

compose config --quiet
compose --profile bootstrap run --rm bootstrap >/dev/null
compose --profile tenant-bootstrap run --rm tenant-bootstrap >/dev/null
compose up -d --build >/dev/null

endpoint=
wait_ready() {
  endpoint=$(compose port control-plane 8080)
  if [ -z "$endpoint" ]; then
    echo "Compose Control Plane port is unavailable" >&2
    exit 1
  fi
  attempt=0
  until curl --silent --show-error --fail --cacert "$smoke_directory/ca.crt" "https://$endpoint/readyz" >/dev/null 2>&1; do
    attempt=$((attempt + 1))
    if [ "$attempt" -ge 60 ]; then
      compose logs --no-color --tail=200 worker control-plane migrate postgres >&2
      exit 1
    fi
    sleep 1
  done
}
wait_ready
cloud_agentsctl() {
  "$cli" --endpoint "https://$endpoint" --ca-file "$smoke_directory/ca.crt" \
    --token-file "$smoke_directory/token" --tenant tenant-compose-smoke "$@"
}
cloud_agentsctl_user() {
  "$cli" --endpoint "https://$endpoint" --ca-file "$smoke_directory/ca.crt" \
    --token-file "$smoke_directory/user-token" --tenant tenant-compose-smoke "$@"
}
control_plane_api() {
  auth_config=$1
  method=$2
  path=$3
  request_id=$4
  shift 4
  curl --silent --show-error --fail-with-body --cacert "$smoke_directory/ca.crt" \
    --config "$auth_config" --request "$method" --header "X-Request-ID: $request_id" \
    --header "Content-Type: application/json" "$@" "https://$endpoint$path"
}

docker run -d --name "$registry_container" -p 127.0.0.1::5000 registry:2 >/dev/null
registry_endpoint=$(docker port "$registry_container" 5000/tcp)
registry_port=${registry_endpoint##*:}
case "$registry_port" in
  '' | *[!0-9]*) echo "local Worker registry returned an invalid port" >&2; exit 1 ;;
esac
attempt=0
until curl --silent --show-error --fail "http://127.0.0.1:$registry_port/v2/" >/dev/null 2>&1; do
  attempt=$((attempt + 1))
  if [ "$attempt" -ge 30 ]; then
    docker logs "$registry_container" >&2
    exit 1
  fi
  sleep 1
done
worker_repository="localhost:$registry_port/cloud-agents/worker"
worker_image=$(compose images -q worker)
if [ -z "$worker_image" ]; then
  echo "Compose Worker image is unavailable" >&2
  exit 1
fi
docker tag "$worker_image" "$worker_repository:smoke"
docker push "$worker_repository:smoke" >/dev/null
worker_reference=$(docker image inspect "$worker_repository:smoke" --format '{{json .RepoDigests}}' |
  node -e 'const fs=require("node:fs");const values=JSON.parse(fs.readFileSync(0,"utf8"));if(values.length!==1)process.exit(1);process.stdout.write(values[0])')
worker_release_digest=${worker_reference##*@}
case "$worker_release_digest" in
  sha256:????????????????????????????????????????????????????????????????) ;;
  *) echo "pushed Worker image returned an invalid digest" >&2; exit 1 ;;
esac
docker create --name "${project}-worker-upgrade-seed" "$worker_image" >/dev/null
docker commit --change 'LABEL cloud-agents.dev.compose-release=upgrade' \
  "${project}-worker-upgrade-seed" "$worker_repository:upgrade" >/dev/null
docker rm "${project}-worker-upgrade-seed" >/dev/null
docker push "$worker_repository:upgrade" >/dev/null
worker_upgrade_reference=$(docker image inspect "$worker_repository:upgrade" --format '{{json .RepoDigests}}' |
  node -e 'const fs=require("node:fs");const values=JSON.parse(fs.readFileSync(0,"utf8"));if(values.length!==1)process.exit(1);process.stdout.write(values[0])')
worker_upgrade_release_digest=${worker_upgrade_reference##*@}
case "$worker_upgrade_release_digest" in
  sha256:????????????????????????????????????????????????????????????????) ;;
  *) echo "pushed upgrade Worker image returned an invalid digest" >&2; exit 1 ;;
esac
if [ "$worker_upgrade_release_digest" = "$worker_release_digest" ]; then
  echo "Compose upgrade Worker image did not produce a distinct digest" >&2
  exit 1
fi

CLOUD_AGENTS_COMPOSE_SMOKE_STATE="$smoke_directory" \
CLOUD_AGENTS_COMPOSE_SMOKE_WORKER_REPOSITORY="$worker_repository" \
CLOUD_AGENTS_COMPOSE_SMOKE_WORKER_CREDENTIAL_REF="$target_worker_credentials_volume" \
  node <<'NODE'
const { chmodSync, writeFileSync } = require("node:fs");
const state = process.env.CLOUD_AGENTS_COMPOSE_SMOKE_STATE;
const workerImageRepository = process.env.CLOUD_AGENTS_COMPOSE_SMOKE_WORKER_REPOSITORY;
const workerCredentialRef = process.env.CLOUD_AGENTS_COMPOSE_SMOKE_WORKER_CREDENTIAL_REF;
if (![state, workerImageRepository, workerCredentialRef].every((value) => value && !value.includes("\n"))) {
  throw new Error("invalid Docker target deployment inputs");
}
const descriptor = {
  workerImageRepository,
  workerCredentialRef,
  workerSpiffeId: "spiffe://cloud-agents.compose/worker-target",
  workerServerName: "host.docker.internal",
};
const path = `${state}/docker-target-credentials/docker-compose-target/deployment.json`;
writeFileSync(path, `${JSON.stringify(descriptor)}\n`);
chmodSync(path, 0o444);
const kubernetesDescriptorPath = `${state}/kubernetes-target-credentials/kubernetes-compose-target.deployment.json`;
writeFileSync(kubernetesDescriptorPath, `${JSON.stringify({ namespace: "cloud-agents-target", workerImageRepository, workerCredentialSecretRef: "cloud-agents-worker-target", workerSpiffeId: "spiffe://cloud-agents.compose/worker-target", workerServerName: "worker-target.example" })}\n`);
chmodSync(kubernetesDescriptorPath, 0o444);
NODE

cp "$smoke_directory/ca.crt" "$smoke_directory/fake-kubectl-state/ca.crt"
: >"$smoke_directory/fake-kubeconfig"
chmod 0600 "$smoke_directory/fake-kubeconfig"
cat >"$smoke_directory/fake-kubectl" <<'SH'
#!/bin/sh
set -eu
state=${CLOUD_AGENTS_FAKE_KUBECTL_STATE:?}
printf '%s\n' "$*" >>"$state/calls"
case "$*" in
  *' config view '*'certificate-authority-data'*) openssl base64 -A -in "$state/ca.crt" ;;
  *' config view '*'cluster.server'*) printf '%s' 'https://kubernetes-target.example:6443' ;;
  *' apply '*) cat >"$state/rbac.yaml" ;;
  *' auth can-i '*) printf '%s\n' yes ;;
  *' get secret '*) ;;
  *' create token '*) printf '%s\n' fake-service-account-token ;;
  *' create secret generic '*) ;;
  *) echo "unexpected fake kubectl command: $*" >&2; exit 1 ;;
esac
SH
chmod 0755 "$smoke_directory/fake-kubectl"

prepare_kubernetes_target() {
  CLOUD_AGENTS_FAKE_KUBECTL_STATE="$smoke_directory/fake-kubectl-state" \
  CLOUD_AGENTS_KUBECONFIG="$smoke_directory/fake-kubeconfig" \
  CLOUD_AGENTS_KUBERNETES_CONTEXT=target-context \
  CLOUD_AGENTS_KUBERNETES_NAMESPACE=cloud-agents-target \
  CLOUD_AGENTS_KUBERNETES_SERVICE_ACCOUNT=control-plane \
  CLOUD_AGENTS_KUBERNETES_TOKEN_DURATION=24h \
  CLOUD_AGENTS_TARGET_CREDENTIAL_REF=kubernetes-prepared-target \
  CLOUD_AGENTS_KUBERNETES_CREDENTIALS_DIR="$smoke_directory/prepared-kubernetes-target-credentials" \
  CLOUD_AGENTS_WORKER_IMAGE_REPOSITORY="$worker_repository" \
  CLOUD_AGENTS_WORKER_CREDENTIAL_SECRET_REF=cloud-agents-worker-target \
  CLOUD_AGENTS_WORKER_CREDENTIAL_DIR="$smoke_directory/target-worker-credentials" \
  CLOUD_AGENTS_PROVIDER_CREDENTIAL_SECRET_REF=cloud-agents-provider-target \
  CLOUD_AGENTS_PROVIDER_CREDENTIAL_DIR="$smoke_directory/target-provider-credentials" \
  CLOUD_AGENTS_TENANT=tenant-compose-smoke \
  CLOUD_AGENTS_WORKER_SPIFFE_ID=spiffe://cloud-agents.compose/worker-target \
  CLOUD_AGENTS_WORKER_SERVER_NAME=worker-target.example \
  KUBECTL="$smoke_directory/fake-kubectl" \
    sh "$smoke_directory/deployment/scripts/prepare-platform-kubernetes-target.sh"
}
kubernetes_prepare_output=$(prepare_kubernetes_target)
case "$kubernetes_prepare_output" in
  *'endpoint=https://kubernetes-target.example:6443 credentialRef=kubernetes-prepared-target'*) ;;
  *) echo "Kubernetes target preparation returned an invalid result" >&2; exit 1 ;;
esac
if prepare_kubernetes_target >/dev/null 2>&1; then
  echo "Kubernetes target preparation overwrote existing credentials" >&2
  exit 1
fi
CLOUD_AGENTS_COMPOSE_SMOKE_STATE="$smoke_directory" node <<'NODE'
const { readFileSync, statSync } = require("node:fs");
const state = process.env.CLOUD_AGENTS_COMPOSE_SMOKE_STATE;
const directory = `${state}/prepared-kubernetes-target-credentials`;
const files = ["kubernetes-prepared-target.ca.crt", "kubernetes-prepared-target.token", "kubernetes-prepared-target.deployment.json"];
for (const file of files) {
  if ((statSync(`${directory}/${file}`).mode & 0o777) !== 0o400) throw new Error(`${file} mode is not 0400`);
}
if (!readFileSync(`${directory}/kubernetes-prepared-target.ca.crt`).equals(readFileSync(`${state}/ca.crt`))) throw new Error("prepared Kubernetes CA changed");
if (readFileSync(`${directory}/kubernetes-prepared-target.token`, "utf8") !== "fake-service-account-token\n") throw new Error("prepared Kubernetes token changed");
const descriptor = JSON.parse(readFileSync(`${directory}/kubernetes-prepared-target.deployment.json`, "utf8"));
if (descriptor.namespace !== "cloud-agents-target" || descriptor.workerCredentialSecretRef !== "cloud-agents-worker-target") throw new Error("prepared Kubernetes descriptor changed");
const rbac = readFileSync(`${state}/fake-kubectl-state/rbac.yaml`, "utf8");
if (!rbac.includes('resourceNames: ["cloud-agents-worker-target", "cloud-agents-provider-target"]') || !rbac.includes('verbs: ["get", "list", "create", "patch", "delete"]')) throw new Error("prepared Kubernetes RBAC changed");
if (readFileSync(`${state}/fake-kubectl-state/calls`, "utf8").includes("fake-service-account-token")) throw new Error("Kubernetes token entered command log");
NODE

prepare_target_credentials() {
  CLOUD_AGENTS_WORKER_IMAGE="$worker_reference" \
  CLOUD_AGENTS_WORKER_CREDENTIAL_REF="$target_worker_credentials_volume" \
  CLOUD_AGENTS_WORKER_CREDENTIAL_DIR="$smoke_directory/target-worker-credentials" \
  CLOUD_AGENTS_PROVIDER_CREDENTIAL_REF="$target_provider_credentials_volume" \
  CLOUD_AGENTS_PROVIDER_CREDENTIAL_DIR="$smoke_directory/target-provider-credentials" \
  CLOUD_AGENTS_TENANT=tenant-compose-smoke \
    sh "$smoke_directory/deployment/scripts/prepare-platform-docker-target.sh"
}
prepare_target_credentials >/dev/null
if prepare_target_credentials >/dev/null 2>&1; then
  echo "Docker target preparation overwrote non-empty credential volumes" >&2
  exit 1
fi
if [ -n "$real_provider_credentials_directory" ]; then
  docker run --rm --user 0 --entrypoint /bin/sh \
    -v "$target_provider_credentials_volume:/target" \
    -v "$real_provider_credentials_directory:/source:ro" \
    postgres:17.6-bookworm -ec \
    'cp /source/tenant-compose-smoke.codex.json /source/tenant-compose-smoke.claudeAgent.json /target/ && chown 1000:1000 /target/* && chmod 0400 /target/*'
fi

organization_output=$(cloud_agentsctl --request-id compose-smoke-organizations organization list)
case "$organization_output" in
  *'"uid":"organization-compose-smoke"'*) ;;
  *) echo "Compose bootstrap organization is unavailable" >&2; exit 1 ;;
esac
project_output=$(cloud_agentsctl --request-id compose-smoke-project-create \
  --idempotency-key compose-smoke-project-create project create --name compose-smoke-project \
  --display-name "Compose Smoke Project" --organization-id organization-compose-smoke)
project_id=$(printf '%s' "$project_output" | node -e 'const fs=require("node:fs");const value=JSON.parse(fs.readFileSync(0,"utf8"));process.stdout.write(value.metadata.uid)')
case "$project_id" in project-*) ;; *) echo "Compose project id is invalid" >&2; exit 1 ;; esac

kubernetes_target_output=$(cloud_agentsctl --project "$project_id" --target kubernetes-compose-target \
  --request-id compose-smoke-kubernetes-target-register --idempotency-key compose-smoke-kubernetes-target-register \
  target register --target-name kubernetes-compose-target --kind kubernetes \
  --target-endpoint "https://host.docker.internal:$kubernetes_api_port" \
  --credential-ref kubernetes-compose-target)
case "$kubernetes_target_output" in
  *'"generation":1'*'"targetKind":"kubernetes"'*'"observedPhase":"unprobed"'*) ;;
  *) echo "Compose Kubernetes target was not registered" >&2; exit 1 ;;
esac
kubernetes_probe_output=$(cloud_agentsctl --project "$project_id" --target kubernetes-compose-target \
  --request-id compose-smoke-kubernetes-target-probe --idempotency-key compose-smoke-kubernetes-target-probe \
  target probe --expected-generation 1)
case "$kubernetes_probe_output" in
  *'"generation":1'*'"observedPhase":"ready"'*'"apiVersion":"1.34"'*'"engineVersion":"v1.34.2"'*'"os":"linux"'*'"architecture":"arm64"'*) ;;
  *) echo "Compose Kubernetes target probe did not become ready" >&2; exit 1 ;;
esac
kubernetes_target_get_output=$(cloud_agentsctl --project "$project_id" --target kubernetes-compose-target \
  --request-id compose-smoke-kubernetes-target-get target get)
case "$kubernetes_target_get_output" in
  *'"generation":1'*'"targetKind":"kubernetes"'*'"observedPhase":"ready"'*) ;;
  *) echo "Compose Kubernetes target ready state was not persisted" >&2; exit 1 ;;
esac
kubernetes_cleanup_output=$(cloud_agentsctl --project "$project_id" --target kubernetes-compose-target \
  --request-id compose-smoke-kubernetes-target-cleanup --idempotency-key compose-smoke-kubernetes-target-cleanup \
  target cleanup --expected-generation 1)
case "$kubernetes_cleanup_output" in
  *'"generation":1'*'"targetKind":"kubernetes"'*'"observedPhase":"ready"'*) ;;
  *) echo "Compose Kubernetes target cleanup failed" >&2; exit 1 ;;
esac

target_output=$(cloud_agentsctl --project "$project_id" --target docker-compose-target \
  --request-id compose-smoke-target-register --idempotency-key compose-smoke-target-register \
  target register --target-name docker-compose-target --kind docker \
  --target-endpoint "https://host.docker.internal:$docker_proxy_port" \
  --credential-ref docker-compose-target)
case "$target_output" in
  *'"generation":1'*'"targetKind":"docker"'*'"observedPhase":"unprobed"'*) ;;
  *) echo "Compose Docker target was not registered" >&2; exit 1 ;;
esac
probe_output=$(cloud_agentsctl --project "$project_id" --target docker-compose-target \
  --request-id compose-smoke-target-probe --idempotency-key compose-smoke-target-probe \
  target probe --expected-generation 1)
case "$probe_output" in
  *'"generation":1'*'"observedPhase":"ready"'*'"apiVersion":'*'"engineVersion":'*) ;;
  *) echo "Compose Docker target probe did not become ready" >&2; exit 1 ;;
esac
target_get_output=$(cloud_agentsctl --project "$project_id" --target docker-compose-target \
  --request-id compose-smoke-target-get target get)
case "$target_get_output" in
  *'"generation":1'*'"observedPhase":"ready"'*) ;;
  *) echo "Compose Docker target ready state was not persisted" >&2; exit 1 ;;
esac

failed_lease_output=$(cloud_agentsctl --timeout 60s --project "$project_id" --target docker-compose-target \
  --lease lease-compose-target-retry --request-id compose-smoke-retry-lease-create \
  --idempotency-key compose-smoke-retry-lease-create environment-lease create \
  --name lease-compose-target-retry --release-digest "$worker_release_digest" \
  --expected-target-generation 1 --provider-credential-ref "$retry_provider_credentials_volume" \
  --cpu-limit-millis 1000 --memory-limit-bytes 536870912 --ttl-seconds 3600)
case "$failed_lease_output" in
  *'"generation":1'*'"observedPhase":"failed"'*'"cleanupPhase":"none"'*'"stableErrorCode":"docker-deployment-config-unavailable"'*) ;;
  *) echo "Compose missing credential volume did not persist a recoverable failed deployment: $failed_lease_output" >&2; exit 1 ;;
esac
retry_container_count=$(docker ps -aq \
  --filter label=cloud-agents.dev/tenant=tenant-compose-smoke \
  --filter label=cloud-agents.dev/lease=lease-compose-target-retry | wc -l | tr -d ' ')
if [ "$retry_container_count" -ne 0 ]; then
  echo "Compose failed deployment left a target Worker" >&2
  exit 1
fi
docker volume create "$retry_provider_credentials_volume" >/dev/null
docker run --rm --user 0 --entrypoint /bin/sh \
  -v "$retry_provider_credentials_volume:/target" \
  -v "$smoke_directory/target-provider-credentials:/source:ro" \
  postgres:17.6-bookworm -ec \
  'cp /source/tenant-compose-smoke.unavailable-provider.json /target/ && chown 1000:1000 /target/* && chmod 0400 /target/*'
recovered_lease_output=$(cloud_agentsctl --timeout 60s --project "$project_id" --target docker-compose-target \
  --lease lease-compose-target-retry --request-id compose-smoke-retry-lease-create \
  --idempotency-key compose-smoke-retry-lease-create environment-lease create \
  --name lease-compose-target-retry --release-digest "$worker_release_digest" \
  --expected-target-generation 1 --provider-credential-ref "$retry_provider_credentials_volume" \
  --cpu-limit-millis 1000 --memory-limit-bytes 536870912 --ttl-seconds 3600)
case "$recovered_lease_output" in
  *'"generation":1'*'"observedPhase":"ready"'*'"cleanupPhase":"none"'*'"targetId":"docker-compose-target"'*'"stableErrorCode"'*)
    echo "Compose failed deployment recovery retained an error: $recovered_lease_output" >&2; exit 1 ;;
  *'"generation":1'*'"observedPhase":"ready"'*'"cleanupPhase":"none"'*'"targetId":"docker-compose-target"'*) ;;
  *) echo "Compose failed deployment did not recover: $recovered_lease_output" >&2; exit 1 ;;
esac
retry_container_count=$(docker ps -q \
  --filter label=cloud-agents.dev/tenant=tenant-compose-smoke \
  --filter label=cloud-agents.dev/lease=lease-compose-target-retry | wc -l | tr -d ' ')
if [ "$retry_container_count" -ne 1 ]; then
  echo "Compose failed deployment recovery created $retry_container_count Workers, expected 1" >&2
  exit 1
fi
retry_terminate_output=$(cloud_agentsctl --timeout 60s --project "$project_id" --lease lease-compose-target-retry \
  --request-id compose-smoke-retry-lease-terminate --idempotency-key compose-smoke-retry-lease-terminate \
  environment-lease terminate --generation 1)
case "$retry_terminate_output" in
  *'"generation":2'*'"observedPhase":"terminated"'*'"cleanupPhase":"complete"'*) ;;
  *) echo "Compose recovered deployment did not terminate cleanly: $retry_terminate_output" >&2; exit 1 ;;
esac
retry_container_count=$(docker ps -aq \
  --filter label=cloud-agents.dev/tenant=tenant-compose-smoke \
  --filter label=cloud-agents.dev/lease=lease-compose-target-retry | wc -l | tr -d ' ')
if [ "$retry_container_count" -ne 0 ]; then
  echo "Compose recovered deployment left a target Worker after termination" >&2
  exit 1
fi

release_id=compose-worker-release
release_create_file="$smoke_directory/release-create.json"
release_create_body=$(printf '{"releaseId":"%s","releaseName":"%s","imageRepository":"%s","releaseDigest":"%s","platformVersion":"platform-v1","runtimeVersion":"runtime-v1","codexVersion":"codex-v1","claudeCodeVersion":"claude-v1","architectures":["%s"],"verificationEvidenceDigest":"%s"}' \
  "$release_id" "$release_id" "$worker_repository" "$worker_release_digest" "$image_platform" "$worker_release_digest")
control_plane_api "$smoke_directory/admin-curl.conf" POST \
  "/v1/admin/tenants/tenant-compose-smoke/projects/$project_id/worker-releases" \
  compose-smoke-release-create --header "Idempotency-Key: compose-smoke-release-create" \
  --data "$release_create_body" >"$release_create_file"
CLOUD_AGENTS_COMPOSE_RELEASE_FILE="$release_create_file" \
CLOUD_AGENTS_COMPOSE_RELEASE_ID="$release_id" \
CLOUD_AGENTS_COMPOSE_RELEASE_DIGEST="$worker_release_digest" node <<'NODE'
const { readFileSync } = require("node:fs");
const value = JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_RELEASE_FILE, "utf8"));
if (value.kind !== "WorkerRelease" || value.metadata?.uid !== process.env.CLOUD_AGENTS_COMPOSE_RELEASE_ID ||
    value.spec?.releaseDigest !== process.env.CLOUD_AGENTS_COMPOSE_RELEASE_DIGEST ||
    value.spec?.status !== "approved" || value.spec?.verificationState !== "attested" ||
    value.metadata?.resourceVersion !== "1") {
  throw new Error("Admin API did not persist the approved Worker release");
}
for (const forbidden of ["credentialRef", "providerCredentialRef", "endpoint", "secret", "prompt", "artifact"]) {
  if (JSON.stringify(value).toLowerCase().includes(forbidden.toLowerCase())) {
    throw new Error(`Worker release response exposed ${forbidden}`);
  }
}
NODE
release_replay_file="$smoke_directory/release-replay.json"
control_plane_api "$smoke_directory/admin-curl.conf" POST \
  "/v1/admin/tenants/tenant-compose-smoke/projects/$project_id/worker-releases" \
  compose-smoke-release-create --header "Idempotency-Key: compose-smoke-release-create" \
  --data "$release_create_body" >"$release_replay_file"
if ! cmp -s "$release_create_file" "$release_replay_file"; then
  echo "Compose Worker release idempotent replay drifted" >&2
  exit 1
fi
release_list_file="$smoke_directory/release-list.json"
control_plane_api "$smoke_directory/admin-curl.conf" GET \
  "/v1/admin/tenants/tenant-compose-smoke/projects/$project_id/worker-releases?pageSize=200" \
  compose-smoke-release-list >"$release_list_file"
CLOUD_AGENTS_COMPOSE_RELEASE_FILE="$release_list_file" CLOUD_AGENTS_COMPOSE_RELEASE_ID="$release_id" node <<'NODE'
const { readFileSync } = require("node:fs");
const value = JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_RELEASE_FILE, "utf8"));
if (value.kind !== "WorkerReleasePage" || value.workerReleases?.length !== 1 ||
    value.workerReleases[0]?.metadata?.uid !== process.env.CLOUD_AGENTS_COMPOSE_RELEASE_ID) {
  throw new Error("Admin API Worker release catalog drifted");
}
NODE
user_admin_release_file="$smoke_directory/user-admin-release-denied.json"
user_admin_release_status=$(curl --silent --show-error --cacert "$smoke_directory/ca.crt" \
  --config "$smoke_directory/user-curl.conf" --request GET \
  --header "X-Request-ID: compose-smoke-user-admin-release-denied" \
  --output "$user_admin_release_file" --write-out '%{http_code}' \
  "https://$endpoint/v1/admin/tenants/tenant-compose-smoke/projects/$project_id/worker-releases?pageSize=200")
if [ "$user_admin_release_status" -ne 403 ] || \
  ! grep -q '"code":"AUTHORIZATION_DENIED"' "$user_admin_release_file"; then
  echo "Compose ordinary User token was not denied by the Worker Release Admin API" >&2
  exit 1
fi
upgrade_release_id=compose-worker-release-upgrade
upgrade_release_create_file="$smoke_directory/upgrade-release-create.json"
upgrade_release_create_body=$(printf '{"releaseId":"%s","releaseName":"%s","imageRepository":"%s","releaseDigest":"%s","platformVersion":"platform-v1","runtimeVersion":"runtime-v1","codexVersion":"codex-v1","claudeCodeVersion":"claude-v1","architectures":["%s"],"verificationEvidenceDigest":"%s"}' \
  "$upgrade_release_id" "$upgrade_release_id" "$worker_repository" "$worker_upgrade_release_digest" "$image_platform" "$worker_upgrade_release_digest")
control_plane_api "$smoke_directory/admin-curl.conf" POST \
  "/v1/admin/tenants/tenant-compose-smoke/projects/$project_id/worker-releases" \
  compose-smoke-upgrade-release-create --header "Idempotency-Key: compose-smoke-upgrade-release-create" \
  --data "$upgrade_release_create_body" >"$upgrade_release_create_file"
CLOUD_AGENTS_COMPOSE_RELEASE_FILE="$upgrade_release_create_file" \
  CLOUD_AGENTS_COMPOSE_RELEASE_ID="$upgrade_release_id" \
  CLOUD_AGENTS_COMPOSE_RELEASE_DIGEST="$worker_upgrade_release_digest" node <<'NODE'
const { readFileSync } = require("node:fs");
const value = JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_RELEASE_FILE, "utf8"));
if (value.metadata?.uid !== process.env.CLOUD_AGENTS_COMPOSE_RELEASE_ID ||
    value.spec?.releaseDigest !== process.env.CLOUD_AGENTS_COMPOSE_RELEASE_DIGEST ||
    value.spec?.status !== "approved" || value.spec?.verificationState !== "attested") {
  throw new Error("Admin API did not persist the approved upgrade Worker release");
}
NODE
unapproved_profile_file="$smoke_directory/unapproved-profile.json"
unapproved_profile_body='{"profileId":"unapproved-profile","profileName":"unapproved-profile","version":1,"description":"Unapproved release must be rejected","providerKinds":["codex"],"cpuLimitMillis":1000,"memoryLimitBytes":536870912,"storagePolicyRef":"storage-compose","networkPolicyRef":"network-compose","releaseDigest":"sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","targetRefs":["docker-compose-target"],"providerCredentialRef":"provider-unapproved"}'
unapproved_profile_status=$(curl --silent --show-error --cacert "$smoke_directory/ca.crt" \
  --config "$smoke_directory/admin-curl.conf" --request POST \
  --header "X-Request-ID: compose-smoke-unapproved-profile" \
  --header "Idempotency-Key: compose-smoke-unapproved-profile" \
  --header "Content-Type: application/json" --data "$unapproved_profile_body" \
  --output "$unapproved_profile_file" --write-out '%{http_code}' \
  "https://$endpoint/v1/admin/tenants/tenant-compose-smoke/projects/$project_id/environment-profiles")
if [ "$unapproved_profile_status" -ne 409 ] || \
  ! grep -q '"code":"PROFILE_VERSION_CONFLICT"' "$unapproved_profile_file"; then
  echo "Compose Profile accepted an unapproved Worker release" >&2
  exit 1
fi

profile_id=compose-docker-profile
profile_create_file="$smoke_directory/profile-create.json"
profile_create_body=$(printf '{"profileId":"%s","profileName":"%s","version":1,"description":"Packaged Docker worker profile","providerKinds":["codex","claudeAgent"],"cpuLimitMillis":1000,"memoryLimitBytes":536870912,"storagePolicyRef":"storage-compose","networkPolicyRef":"network-compose","releaseDigest":"%s","targetRefs":["%s"],"providerCredentialRef":"%s"}' \
  "$profile_id" "$profile_id" "$worker_release_digest" docker-compose-target "$target_provider_credentials_volume")
control_plane_api "$smoke_directory/admin-curl.conf" POST \
  "/v1/admin/tenants/tenant-compose-smoke/projects/$project_id/environment-profiles" \
  compose-smoke-profile-create --header "Idempotency-Key: compose-smoke-profile-create" \
  --data "$profile_create_body" >"$profile_create_file"
profile_resource_version=$(
  CLOUD_AGENTS_COMPOSE_PROFILE_FILE="$profile_create_file" CLOUD_AGENTS_COMPOSE_PROFILE_ID="$profile_id" node <<'NODE'
const { readFileSync } = require("node:fs");
const value = JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_PROFILE_FILE, "utf8"));
if (value.kind !== "EnvironmentProfile" || value.spec?.profileId !== process.env.CLOUD_AGENTS_COMPOSE_PROFILE_ID ||
    value.spec?.version !== 1 || value.spec?.status !== "draft" || value.metadata?.resourceVersion !== "1") {
  throw new Error("Admin API did not persist the expected draft Profile");
}
process.stdout.write(value.metadata.resourceVersion);
NODE
)
profile_publish_file="$smoke_directory/profile-publish.json"
control_plane_api "$smoke_directory/admin-curl.conf" POST \
  "/v1/admin/tenants/tenant-compose-smoke/projects/$project_id/environment-profiles/$profile_id/versions/1:publish" \
  compose-smoke-profile-publish --header "Idempotency-Key: compose-smoke-profile-publish" \
  --data "{\"expectedResourceVersion\":\"$profile_resource_version\"}" >"$profile_publish_file"
CLOUD_AGENTS_COMPOSE_PROFILE_FILE="$profile_publish_file" CLOUD_AGENTS_COMPOSE_PROFILE_ID="$profile_id" node <<'NODE'
const { readFileSync } = require("node:fs");
const value = JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_PROFILE_FILE, "utf8"));
if (value.kind !== "EnvironmentProfile" || value.spec?.profileId !== process.env.CLOUD_AGENTS_COMPOSE_PROFILE_ID ||
    value.spec?.version !== 1 || value.spec?.status !== "published" || value.metadata?.resourceVersion !== "2" ||
    typeof value.spec?.publishedAt !== "string") {
  throw new Error("Admin API did not publish the Profile");
}
NODE

user_admin_profile_file="$smoke_directory/user-admin-profile-denied.json"
user_admin_profile_status=$(curl --silent --show-error --cacert "$smoke_directory/ca.crt" \
  --config "$smoke_directory/user-curl.conf" --request GET \
  --header "X-Request-ID: compose-smoke-user-admin-profile-denied" \
  --output "$user_admin_profile_file" --write-out '%{http_code}' \
  "https://$endpoint/v1/admin/tenants/tenant-compose-smoke/projects/$project_id/environment-profiles?pageSize=200")
if [ "$user_admin_profile_status" -ne 403 ] || \
  ! grep -q '"code":"AUTHORIZATION_DENIED"' "$user_admin_profile_file"; then
  echo "Compose ordinary User token was not denied by the Profile Admin API" >&2
  exit 1
fi

published_profiles_file="$smoke_directory/published-profiles.json"
control_plane_api "$smoke_directory/user-curl.conf" GET \
  "/v1/tenants/tenant-compose-smoke/projects/$project_id/environment-profiles?pageSize=200" \
  compose-smoke-published-profiles >"$published_profiles_file"
CLOUD_AGENTS_COMPOSE_PROFILE_FILE="$published_profiles_file" CLOUD_AGENTS_COMPOSE_PROFILE_ID="$profile_id" node <<'NODE'
const { readFileSync } = require("node:fs");
const value = JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_PROFILE_FILE, "utf8"));
const profile = value.environmentProfiles?.find((item) => item.profileId === process.env.CLOUD_AGENTS_COMPOSE_PROFILE_ID);
const expectedKeys = ["apiVersion", "availability", "cpuLimitMillis", "description", "kind", "memoryLimitBytes",
  "name", "profileId", "projectRef", "providerKinds", "status", "version"].sort();
if (!profile || profile.kind !== "EnvironmentProfileSummary" || profile.version !== 1 ||
    profile.status !== "published" || profile.availability !== "available") {
  throw new Error("User API did not return the published Profile summary");
}
const actualKeys = Object.keys(profile).sort();
if (actualKeys.join("\n") !== expectedKeys.join("\n")) {
  throw new Error(`User Profile summary field boundary changed: ${actualKeys.join(",")}`);
}
NODE

quota_path="/v1/admin/tenants/tenant-compose-smoke/projects/$project_id/lease-quota"
quota_create_file="$smoke_directory/quota-create.json"
quota_create_body='{"expectedResourceVersion":"0","maxConcurrentLeases":1,"maxCpuMillis":1000,"maxMemoryBytes":536870912,"maxLeaseTtlSeconds":3600}'
control_plane_api "$smoke_directory/admin-curl.conf" PUT "$quota_path" \
  compose-smoke-quota-create --header "Idempotency-Key: compose-smoke-quota-create" \
  --data "$quota_create_body" >"$quota_create_file"
CLOUD_AGENTS_COMPOSE_QUOTA_FILE="$quota_create_file" CLOUD_AGENTS_COMPOSE_PROJECT_ID="$project_id" node <<'NODE'
const { readFileSync } = require("node:fs");
const value = JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_QUOTA_FILE, "utf8"));
if (value.kind !== "ProjectLeaseQuota" || value.metadata?.resourceVersion !== "1" ||
    value.spec?.projectRef?.id !== process.env.CLOUD_AGENTS_COMPOSE_PROJECT_ID ||
    value.spec?.maxConcurrentLeases !== 1 || value.spec?.maxCpuMillis !== 1000 ||
    value.spec?.maxMemoryBytes !== 536870912 || value.spec?.maxLeaseTtlSeconds !== 3600 ||
    value.status?.activeLeases !== 0 || value.status?.usedCpuMillis !== 0 ||
    value.status?.usedMemoryBytes !== 0) {
  throw new Error("Admin API did not persist the initial project Lease quota");
}
NODE
quota_replay_file="$smoke_directory/quota-replay.json"
control_plane_api "$smoke_directory/admin-curl.conf" PUT "$quota_path" \
  compose-smoke-quota-create --header "Idempotency-Key: compose-smoke-quota-create" \
  --data "$quota_create_body" >"$quota_replay_file"
if ! cmp -s "$quota_create_file" "$quota_replay_file"; then
  echo "Compose project Lease quota idempotent replay drifted" >&2
  exit 1
fi
user_admin_quota_file="$smoke_directory/user-admin-quota-denied.json"
user_admin_quota_status=$(curl --silent --show-error --cacert "$smoke_directory/ca.crt" \
  --config "$smoke_directory/user-curl.conf" --request GET \
  --header "X-Request-ID: compose-smoke-user-admin-quota-denied" \
  --output "$user_admin_quota_file" --write-out '%{http_code}' \
  "https://$endpoint$quota_path")
if [ "$user_admin_quota_status" -ne 403 ] || \
  ! grep -q '"code":"AUTHORIZATION_DENIED"' "$user_admin_quota_file"; then
  echo "Compose ordinary User token was not denied by the project Lease quota Admin API" >&2
  exit 1
fi
user_quota_file="$smoke_directory/user-quota.json"
control_plane_api "$smoke_directory/user-curl.conf" GET \
  "/v1/tenants/tenant-compose-smoke/projects/$project_id/lease-quota" \
  compose-smoke-user-quota >"$user_quota_file"
CLOUD_AGENTS_COMPOSE_QUOTA_FILE="$user_quota_file" CLOUD_AGENTS_COMPOSE_PROJECT_ID="$project_id" node <<'NODE'
const { readFileSync } = require("node:fs");
const value = JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_QUOTA_FILE, "utf8"));
const expectedKeys = ["activeLeases", "apiVersion", "kind", "maxConcurrentLeases", "maxCpuMillis",
  "maxLeaseTtlSeconds", "maxMemoryBytes", "projectRef", "usedCpuMillis", "usedMemoryBytes"].sort();
const actualKeys = Object.keys(value).sort();
if (value.kind !== "ProjectLeaseQuotaSummary" ||
    value.projectRef?.id !== process.env.CLOUD_AGENTS_COMPOSE_PROJECT_ID ||
    value.maxConcurrentLeases !== 1 || value.activeLeases !== 0 ||
    actualKeys.join("\n") !== expectedKeys.join("\n")) {
  throw new Error(`User project Lease quota boundary changed: ${actualKeys.join(",")}`);
}
for (const forbidden of ["endpoint", "credentialref", "providercredentialref", "releasedigest", "secret"]) {
  if (JSON.stringify(value).toLowerCase().includes(forbidden)) {
    throw new Error(`User project Lease quota exposed ${forbidden}`);
  }
}
NODE

user_environment_body=$(printf '{"profileId":"%s","profileVersion":1}' "$profile_id")
user_environment_file="$smoke_directory/user-environment.json"
control_plane_api "$smoke_directory/user-curl.conf" POST \
  "/v1/tenants/tenant-compose-smoke/projects/$project_id/environments" \
  compose-smoke-user-environment-create --header "Idempotency-Key: compose-smoke-user-environment-create" \
  --data "$user_environment_body" >"$user_environment_file"
profile_environment_id=$(CLOUD_AGENTS_COMPOSE_ENVIRONMENT_FILE="$user_environment_file" node -e \
  'const {readFileSync}=require("node:fs");process.stdout.write(JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_ENVIRONMENT_FILE,"utf8")).environmentId)')
CLOUD_AGENTS_COMPOSE_ENVIRONMENT_FILE="$user_environment_file" CLOUD_AGENTS_COMPOSE_PROFILE_ID="$profile_id" node <<'NODE'
const { readFileSync } = require("node:fs");
const value = JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_ENVIRONMENT_FILE, "utf8"));
const expectedKeys = ["apiVersion", "environmentId", "expiresAt", "kind", "observedPhase", "profileId", "profileVersion", "projectRef"].sort();
const actualKeys = Object.keys(value).sort();
if (value.kind !== "UserEnvironment" || value.profileId !== process.env.CLOUD_AGENTS_COMPOSE_PROFILE_ID ||
    value.profileVersion !== 1 || value.observedPhase !== "ready" || actualKeys.join("\n") !== expectedKeys.join("\n")) {
  throw new Error(`Profile did not create a safe ready User Environment: ${actualKeys.join(",")}`);
}
NODE
replayed_user_environment_file="$smoke_directory/user-environment-replayed.json"
control_plane_api "$smoke_directory/user-curl.conf" POST \
  "/v1/tenants/tenant-compose-smoke/projects/$project_id/environments" \
  compose-smoke-user-environment-create --header "Idempotency-Key: compose-smoke-user-environment-create" \
  --data "$user_environment_body" >"$replayed_user_environment_file"
if ! cmp -s "$user_environment_file" "$replayed_user_environment_file"; then
  echo "Compose Profile environment creation was not idempotent" >&2
  exit 1
fi

lease_output=$(cloud_agentsctl --project "$project_id" --lease "$profile_environment_id" \
  --request-id compose-smoke-profile-lease environment-lease get)
case "$lease_output" in
  *'"generation":1'*'"observedPhase":"ready"'*'"cleanupPhase":"none"'*'"releaseDigest":"'"$worker_release_digest"'"'*'"targetId":"docker-compose-target"'*'"providerCredentialRef":"'"$target_provider_credentials_volume"'"'*'"workerEndpoint":"https://host.docker.internal:'*'"workerSpiffeId":"spiffe://cloud-agents.compose/worker-target"'*) ;;
  *) echo "Compose Profile did not resolve to the expected ready Docker Worker: $lease_output" >&2; exit 1 ;;
esac
target_container_count=$(docker ps -q \
  --filter label=cloud-agents.dev/tenant=tenant-compose-smoke \
  --filter label=cloud-agents.dev/lease="$profile_environment_id" | wc -l | tr -d ' ')
if [ "$target_container_count" -ne 1 ]; then
  echo "Compose Profile created $target_container_count Docker Workers, expected 1" >&2
  exit 1
fi

active_user_quota_file="$smoke_directory/active-user-quota.json"
control_plane_api "$smoke_directory/user-curl.conf" GET \
  "/v1/tenants/tenant-compose-smoke/projects/$project_id/lease-quota" \
  compose-smoke-active-user-quota >"$active_user_quota_file"
CLOUD_AGENTS_COMPOSE_QUOTA_FILE="$active_user_quota_file" node <<'NODE'
const { readFileSync } = require("node:fs");
const value = JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_QUOTA_FILE, "utf8"));
if (value.activeLeases !== 1 || value.usedCpuMillis !== 1000 ||
    value.usedMemoryBytes !== 536870912 || value.maxConcurrentLeases !== 1) {
  throw new Error("User quota summary did not report the active Profile environment");
}
NODE
quota_denied_environment_file="$smoke_directory/quota-denied-environment.json"
quota_denied_environment_status=$(curl --silent --show-error --cacert "$smoke_directory/ca.crt" \
  --config "$smoke_directory/user-curl.conf" --request POST \
  --header "X-Request-ID: compose-smoke-quota-denied-environment" \
  --header "Idempotency-Key: compose-smoke-quota-denied-environment" \
  --header "Content-Type: application/json" --data "$user_environment_body" \
  --output "$quota_denied_environment_file" --write-out '%{http_code}' \
  "https://$endpoint/v1/tenants/tenant-compose-smoke/projects/$project_id/environments")
if [ "$quota_denied_environment_status" -ne 409 ] || \
  ! grep -q '"code":"PROJECT_LEASE_COUNT_QUOTA_EXCEEDED"' "$quota_denied_environment_file"; then
  echo "Compose project Lease count quota did not reject a second environment" >&2
  exit 1
fi
target_container_count=$(docker ps -q \
  --filter label=cloud-agents.dev/tenant=tenant-compose-smoke \
  --filter label=cloud-agents.dev/project="$project_id" | wc -l | tr -d ' ')
if [ "$target_container_count" -ne 1 ]; then
  echo "Compose quota rejection changed the active Docker Worker count to $target_container_count" >&2
  exit 1
fi
quota_update_file="$smoke_directory/quota-update.json"
quota_update_body='{"expectedResourceVersion":"1","maxConcurrentLeases":2,"maxCpuMillis":2000,"maxMemoryBytes":1073741824,"maxLeaseTtlSeconds":3600}'
control_plane_api "$smoke_directory/admin-curl.conf" PUT "$quota_path" \
  compose-smoke-quota-update --header "Idempotency-Key: compose-smoke-quota-update" \
  --data "$quota_update_body" >"$quota_update_file"
CLOUD_AGENTS_COMPOSE_QUOTA_FILE="$quota_update_file" node <<'NODE'
const { readFileSync } = require("node:fs");
const value = JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_QUOTA_FILE, "utf8"));
if (value.metadata?.resourceVersion !== "2" || value.spec?.maxConcurrentLeases !== 2 ||
    value.spec?.maxCpuMillis !== 2000 || value.spec?.maxMemoryBytes !== 1073741824 ||
    value.status?.activeLeases !== 1 || value.status?.usedCpuMillis !== 1000 ||
    value.status?.usedMemoryBytes !== 536870912) {
  throw new Error("Admin API did not update project Lease quota with the resource-version fence");
}
NODE
quota_audit_file="$smoke_directory/quota-audit.json"
control_plane_api "$smoke_directory/admin-curl.conf" GET \
  "$quota_path/audit-events?pageSize=200" compose-smoke-quota-audit >"$quota_audit_file"
CLOUD_AGENTS_COMPOSE_QUOTA_FILE="$quota_audit_file" node <<'NODE'
const { readFileSync } = require("node:fs");
const value = JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_QUOTA_FILE, "utf8"));
if (value.kind !== "AdminAuditEventPage" || value.events?.length !== 2 ||
    !value.events.every((event) => event.action === "quota.set" &&
      event.resourceKind === "ProjectLeaseQuota" && event.result === "succeeded")) {
  throw new Error("Project Lease quota did not close two Admin Audit events");
}
NODE

admin_workers_file="$smoke_directory/admin-workers.json"
control_plane_api "$smoke_directory/admin-curl.conf" GET \
  "/v1/admin/tenants/tenant-compose-smoke/projects/$project_id/workers?pageSize=200" \
  compose-smoke-admin-workers >"$admin_workers_file"
CLOUD_AGENTS_COMPOSE_WORKERS_FILE="$admin_workers_file" \
  CLOUD_AGENTS_COMPOSE_ENVIRONMENT_ID="$profile_environment_id" \
  CLOUD_AGENTS_COMPOSE_WORKER_RELEASE="$worker_release_digest" node <<'NODE'
const { readFileSync } = require("node:fs");
const value = JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_WORKERS_FILE, "utf8"));
const worker = value.workers?.find((item) => item.metadata?.uid === process.env.CLOUD_AGENTS_COMPOSE_ENVIRONMENT_ID);
const forbidden = new Set(["endpoint", "workerendpoint", "credentialref", "providercredentialref", "prompt", "workspace", "artifact"]);
const visit = (item) => {
  if (Array.isArray(item)) return item.forEach(visit);
  if (!item || typeof item !== "object") return;
  for (const [key, nested] of Object.entries(item)) {
    if (forbidden.has(key.toLowerCase())) throw new Error(`Worker Admin API exposed forbidden field ${key}`);
    visit(nested);
  }
};
visit(value);
if (value.kind !== "WorkerPage" || value.workers?.length !== 1 || !worker ||
    worker.spec?.leaseId !== process.env.CLOUD_AGENTS_COMPOSE_ENVIRONMENT_ID ||
    worker.spec?.targetId !== "docker-compose-target" || worker.spec?.targetKind !== "docker" ||
    worker.spec?.releaseDigest !== process.env.CLOUD_AGENTS_COMPOSE_WORKER_RELEASE ||
    worker.spec?.state !== "ready" || worker.spec?.cleanupPhase !== "none" ||
    worker.spec?.cpuLimitMillis !== 1000 || worker.spec?.memoryLimitBytes !== 536870912 ||
    typeof worker.spec?.lastHealthAt !== "string" || worker.spec.lastHealthAt !== worker.metadata?.updatedAt ||
    typeof worker.spec?.readyAt !== "string" || typeof worker.spec?.workerSpiffeId !== "string" ||
    typeof worker.spec?.workerServerName !== "string") {
  throw new Error("Admin API did not project the ready Docker Worker");
}
NODE

user_admin_workers_file="$smoke_directory/user-admin-workers-denied.json"
user_admin_workers_status=$(curl --silent --show-error --cacert "$smoke_directory/ca.crt" \
  --config "$smoke_directory/user-curl.conf" --request GET \
  --header "X-Request-ID: compose-smoke-user-admin-workers-denied" \
  --output "$user_admin_workers_file" --write-out '%{http_code}' \
  "https://$endpoint/v1/admin/tenants/tenant-compose-smoke/projects/$project_id/workers?pageSize=200")
if [ "$user_admin_workers_status" -ne 403 ] || \
  ! grep -q '"code":"AUTHORIZATION_DENIED"' "$user_admin_workers_file"; then
  echo "Compose ordinary User token was not denied by the Worker Admin API" >&2
  exit 1
fi

scheduling_path="/v1/admin/tenants/tenant-compose-smoke/projects/$project_id/deployment-targets/docker-compose-target"
drain_preview_file="$smoke_directory/target-drain-preview.json"
control_plane_api "$smoke_directory/admin-curl.conf" GET \
  "$scheduling_path:scheduling-preview" compose-smoke-target-drain-preview >"$drain_preview_file"
drain_request_body=$(CLOUD_AGENTS_COMPOSE_SCHEDULING_FILE="$drain_preview_file" \
  CLOUD_AGENTS_COMPOSE_ENVIRONMENT_ID="$profile_environment_id" node <<'NODE'
const { readFileSync } = require("node:fs");
const value = JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_SCHEDULING_FILE, "utf8"));
const lease = value.spec?.activeLeases?.[0];
if (value.kind !== "DeploymentTargetSchedulingPreview" || value.metadata?.uid !== "docker-compose-target" ||
    value.spec?.currentState !== "active" || value.spec?.desiredState !== "drained" ||
    value.spec?.expectedGeneration !== 1 || value.spec?.expectedResourceVersion !== value.metadata?.resourceVersion ||
    !/^sha256:[0-9a-f]{64}$/.test(value.spec?.impactDigest) || value.spec?.activeLeases?.length !== 1 ||
    lease?.leaseId !== process.env.CLOUD_AGENTS_COMPOSE_ENVIRONMENT_ID || lease?.observedPhase !== "ready") {
  throw new Error("Admin scheduling preview did not bind the active Docker Lease impact");
}
process.stdout.write(JSON.stringify({
  expectedGeneration: value.spec.expectedGeneration,
  expectedResourceVersion: value.spec.expectedResourceVersion,
  desiredState: value.spec.desiredState,
  impactDigest: value.spec.impactDigest,
}));
NODE
)
user_admin_scheduling_file="$smoke_directory/user-admin-scheduling-denied.json"
user_admin_scheduling_status=$(curl --silent --show-error --cacert "$smoke_directory/ca.crt" \
  --config "$smoke_directory/user-curl.conf" --request GET \
  --header "X-Request-ID: compose-smoke-user-admin-scheduling-denied" \
  --output "$user_admin_scheduling_file" --write-out '%{http_code}' \
  "https://$endpoint$scheduling_path:scheduling-preview")
if [ "$user_admin_scheduling_status" -ne 403 ] || \
  ! grep -q '"code":"AUTHORIZATION_DENIED"' "$user_admin_scheduling_file"; then
  echo "Compose ordinary User token was not denied by the Target scheduling Admin API" >&2
  exit 1
fi
drain_operation_file="$smoke_directory/target-drain-operation.json"
control_plane_api "$smoke_directory/admin-curl.conf" POST "$scheduling_path:scheduling" \
  compose-smoke-target-drain --header "Idempotency-Key: compose-smoke-target-drain" \
  --data "$drain_request_body" >"$drain_operation_file"
CLOUD_AGENTS_COMPOSE_OPERATION_FILE="$drain_operation_file" node <<'NODE'
const { readFileSync } = require("node:fs");
const value = JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_OPERATION_FILE, "utf8"));
if (value.kind !== "MaintenanceOperation" || value.action !== "target.drain" ||
    value.resourceId !== "docker-compose-target" || value.resourceGeneration !== 1 ||
    value.state !== "succeeded" || value.currentStep !== "complete") {
  throw new Error("Admin API did not persist a succeeded Target drain operation");
}
NODE
replayed_drain_operation_file="$smoke_directory/target-drain-operation-replayed.json"
control_plane_api "$smoke_directory/admin-curl.conf" POST "$scheduling_path:scheduling" \
  compose-smoke-target-drain --header "Idempotency-Key: compose-smoke-target-drain" \
  --data "$drain_request_body" >"$replayed_drain_operation_file"
if ! cmp -s "$drain_operation_file" "$replayed_drain_operation_file"; then
  echo "Compose Target drain was not idempotent" >&2
  exit 1
fi
drained_target_output=$(cloud_agentsctl --project "$project_id" --target docker-compose-target \
  --request-id compose-smoke-drained-target-get target get)
case "$drained_target_output" in
  *'"schedulingState":"drained"'*'"observedPhase":"ready"'*) ;;
  *) echo "Compose Target drain did not preserve ready runtime state: $drained_target_output" >&2; exit 1 ;;
esac
drained_profiles_file="$smoke_directory/drained-published-profiles.json"
control_plane_api "$smoke_directory/user-curl.conf" GET \
  "/v1/tenants/tenant-compose-smoke/projects/$project_id/environment-profiles?pageSize=200" \
  compose-smoke-drained-published-profiles >"$drained_profiles_file"
CLOUD_AGENTS_COMPOSE_PROFILE_FILE="$drained_profiles_file" CLOUD_AGENTS_COMPOSE_PROFILE_ID="$profile_id" node <<'NODE'
const { readFileSync } = require("node:fs");
const value = JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_PROFILE_FILE, "utf8"));
const profile = value.environmentProfiles?.find((item) => item.profileId === process.env.CLOUD_AGENTS_COMPOSE_PROFILE_ID);
if (profile !== undefined) throw new Error("Drained Target remained visible to User Profile selection");
NODE
drained_environment_file="$smoke_directory/drained-environment-denied.json"
drained_environment_status=$(curl --silent --show-error --cacert "$smoke_directory/ca.crt" \
  --config "$smoke_directory/user-curl.conf" --request POST \
  --header "X-Request-ID: compose-smoke-drained-environment" \
  --header "Idempotency-Key: compose-smoke-drained-environment" \
  --header "Content-Type: application/json" --data "$user_environment_body" \
  --output "$drained_environment_file" --write-out '%{http_code}' \
  "https://$endpoint/v1/tenants/tenant-compose-smoke/projects/$project_id/environments")
if [ "$drained_environment_status" -ne 409 ] || ! grep -q '"code":"LEASE_CONFLICT"' "$drained_environment_file"; then
  echo "Compose drained Target accepted a new User Environment" >&2
  exit 1
fi
drained_replay_file="$smoke_directory/drained-existing-environment-replay.json"
control_plane_api "$smoke_directory/user-curl.conf" POST \
  "/v1/tenants/tenant-compose-smoke/projects/$project_id/environments" \
  compose-smoke-user-environment-create --header "Idempotency-Key: compose-smoke-user-environment-create" \
  --data "$user_environment_body" >"$drained_replay_file"
if ! cmp -s "$user_environment_file" "$drained_replay_file"; then
  echo "Compose Target drain broke an existing idempotent User Environment replay" >&2
  exit 1
fi
target_container_count=$(docker ps -q \
  --filter label=cloud-agents.dev/tenant=tenant-compose-smoke \
  --filter label=cloud-agents.dev/lease="$profile_environment_id" | wc -l | tr -d ' ')
if [ "$target_container_count" -ne 1 ]; then
  echo "Compose Target drain changed the existing Docker Worker count" >&2
  exit 1
fi

lease_release_path="/v1/admin/tenants/tenant-compose-smoke/projects/$project_id/environment-leases/$profile_environment_id"
upgrade_preview_file="$smoke_directory/lease-upgrade-preview.json"
control_plane_api "$smoke_directory/admin-curl.conf" GET \
  "$lease_release_path:upgrade-preview?releaseDigest=sha256%3A${worker_upgrade_release_digest#sha256:}" \
  compose-smoke-lease-upgrade-preview >"$upgrade_preview_file"
upgrade_request_body=$(CLOUD_AGENTS_COMPOSE_PREVIEW_FILE="$upgrade_preview_file" \
  CLOUD_AGENTS_COMPOSE_ENVIRONMENT_ID="$profile_environment_id" \
  CLOUD_AGENTS_COMPOSE_CURRENT_RELEASE="$worker_release_digest" \
  CLOUD_AGENTS_COMPOSE_TARGET_RELEASE="$worker_upgrade_release_digest" node <<'NODE'
const { readFileSync } = require("node:fs");
const value = JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_PREVIEW_FILE, "utf8"));
const spec = value.spec;
if (value.kind !== "EnvironmentLeaseUpgradePreview" || value.metadata?.uid !== process.env.CLOUD_AGENTS_COMPOSE_ENVIRONMENT_ID ||
    spec?.action !== "upgrade" || spec.currentReleaseDigest !== process.env.CLOUD_AGENTS_COMPOSE_CURRENT_RELEASE ||
    spec.targetReleaseDigest !== process.env.CLOUD_AGENTS_COMPOSE_TARGET_RELEASE ||
    spec.rollbackReleaseDigest !== process.env.CLOUD_AGENTS_COMPOSE_CURRENT_RELEASE || spec.rollbackGeneration !== 1 ||
    spec.expectedGeneration !== 1 || spec.expectedResourceVersion !== value.metadata.resourceVersion ||
    spec.affectedTargets !== 1 || spec.affectedWorkers !== 1 || spec.affectedLeases !== 1 ||
    !/^sha256:[0-9a-f]{64}$/.test(spec.impactDigest)) {
  throw new Error("Admin upgrade preview did not bind the selected Worker and Lease");
}
process.stdout.write(JSON.stringify({
  releaseDigest: spec.targetReleaseDigest,
  expectedGeneration: spec.expectedGeneration,
  expectedResourceVersion: spec.expectedResourceVersion,
  impactDigest: spec.impactDigest,
}));
NODE
)
user_admin_upgrade_file="$smoke_directory/user-admin-upgrade-denied.json"
user_admin_upgrade_status=$(curl --silent --show-error --cacert "$smoke_directory/ca.crt" \
  --config "$smoke_directory/user-curl.conf" --request GET \
  --header "X-Request-ID: compose-smoke-user-admin-upgrade-denied" \
  --output "$user_admin_upgrade_file" --write-out '%{http_code}' \
  "https://$endpoint$lease_release_path:upgrade-preview?releaseDigest=sha256%3A${worker_upgrade_release_digest#sha256:}")
if [ "$user_admin_upgrade_status" -ne 403 ] || \
  ! grep -q '"code":"AUTHORIZATION_DENIED"' "$user_admin_upgrade_file"; then
  echo "Compose ordinary User token was not denied by the Lease upgrade Admin API" >&2
  exit 1
fi
upgrade_operation_file="$smoke_directory/lease-upgrade-operation.json"
control_plane_api "$smoke_directory/admin-curl.conf" POST "$lease_release_path:upgrade" \
  compose-smoke-lease-upgrade --header "Idempotency-Key: compose-smoke-lease-upgrade" \
  --data "$upgrade_request_body" >"$upgrade_operation_file"
CLOUD_AGENTS_COMPOSE_OPERATION_FILE="$upgrade_operation_file" node <<'NODE'
const { readFileSync } = require("node:fs");
const value = JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_OPERATION_FILE, "utf8"));
if (value.kind !== "MaintenanceOperation" || value.action !== "target.upgrade" ||
    value.resourceId !== "docker-compose-target" || value.resourceGeneration !== 1 ||
    value.state !== "succeeded" || value.currentStep !== "complete") {
  throw new Error("Admin API did not close the Worker upgrade operation");
}
NODE
upgrade_replay_file="$smoke_directory/lease-upgrade-operation-replayed.json"
control_plane_api "$smoke_directory/admin-curl.conf" POST "$lease_release_path:upgrade" \
  compose-smoke-lease-upgrade --header "Idempotency-Key: compose-smoke-lease-upgrade" \
  --data "$upgrade_request_body" >"$upgrade_replay_file"
if ! cmp -s "$upgrade_operation_file" "$upgrade_replay_file"; then
  echo "Compose Worker upgrade was not idempotent" >&2
  exit 1
fi
upgraded_lease_output=$(cloud_agentsctl --project "$project_id" --lease "$profile_environment_id" \
  --request-id compose-smoke-upgraded-lease environment-lease get)
case "$upgraded_lease_output" in
  *'"generation":2'*'"observedPhase":"ready"'*'"cleanupPhase":"none"'*'"releaseDigest":"'"$worker_upgrade_release_digest"'"'*) ;;
  *) echo "Compose Worker upgrade did not persist the target release: $upgraded_lease_output" >&2; exit 1 ;;
esac
target_container_count=$(docker ps -q \
  --filter label=cloud-agents.dev/tenant=tenant-compose-smoke \
  --filter label=cloud-agents.dev/lease="$profile_environment_id" | wc -l | tr -d ' ')
if [ "$target_container_count" -ne 1 ]; then
  echo "Compose Worker upgrade left $target_container_count active Workers" >&2
  exit 1
fi
target_container_id=$(docker ps -q \
  --filter label=cloud-agents.dev/tenant=tenant-compose-smoke \
  --filter label=cloud-agents.dev/lease="$profile_environment_id")
if [ "$(docker inspect "$target_container_id" --format '{{.Config.Image}}')" != "$worker_repository@$worker_upgrade_release_digest" ]; then
  echo "Compose Worker upgrade did not run the selected digest" >&2
  exit 1
fi

rollback_preview_file="$smoke_directory/lease-rollback-preview.json"
control_plane_api "$smoke_directory/admin-curl.conf" GET \
  "$lease_release_path:rollback-preview" compose-smoke-lease-rollback-preview >"$rollback_preview_file"
rollback_request_body=$(CLOUD_AGENTS_COMPOSE_PREVIEW_FILE="$rollback_preview_file" \
  CLOUD_AGENTS_COMPOSE_CURRENT_RELEASE="$worker_upgrade_release_digest" \
  CLOUD_AGENTS_COMPOSE_TARGET_RELEASE="$worker_release_digest" node <<'NODE'
const { readFileSync } = require("node:fs");
const value = JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_PREVIEW_FILE, "utf8"));
const spec = value.spec;
if (spec?.action !== "rollback" || spec.currentReleaseDigest !== process.env.CLOUD_AGENTS_COMPOSE_CURRENT_RELEASE ||
    spec.targetReleaseDigest !== process.env.CLOUD_AGENTS_COMPOSE_TARGET_RELEASE ||
    spec.rollbackReleaseDigest !== process.env.CLOUD_AGENTS_COMPOSE_TARGET_RELEASE || spec.rollbackGeneration !== 1 ||
    spec.expectedGeneration !== 2 || spec.expectedResourceVersion !== value.metadata?.resourceVersion ||
    spec.affectedTargets !== 1 || spec.affectedWorkers !== 1 || spec.affectedLeases !== 1 ||
    !/^sha256:[0-9a-f]{64}$/.test(spec.impactDigest)) {
  throw new Error("Admin rollback preview did not bind the persisted prior release");
}
process.stdout.write(JSON.stringify({
  releaseDigest: spec.targetReleaseDigest,
  expectedGeneration: spec.expectedGeneration,
  expectedResourceVersion: spec.expectedResourceVersion,
  impactDigest: spec.impactDigest,
}));
NODE
)
rollback_operation_file="$smoke_directory/lease-rollback-operation.json"
control_plane_api "$smoke_directory/admin-curl.conf" POST "$lease_release_path:rollback" \
  compose-smoke-lease-rollback --header "Idempotency-Key: compose-smoke-lease-rollback" \
  --data "$rollback_request_body" >"$rollback_operation_file"
CLOUD_AGENTS_COMPOSE_OPERATION_FILE="$rollback_operation_file" node <<'NODE'
const { readFileSync } = require("node:fs");
const value = JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_OPERATION_FILE, "utf8"));
if (value.action !== "target.rollback" || value.resourceId !== "docker-compose-target" ||
    value.resourceGeneration !== 1 || value.state !== "succeeded" || value.currentStep !== "complete") {
  throw new Error("Admin API did not close the Worker rollback operation");
}
NODE
rollback_replay_file="$smoke_directory/lease-rollback-operation-replayed.json"
control_plane_api "$smoke_directory/admin-curl.conf" POST "$lease_release_path:rollback" \
  compose-smoke-lease-rollback --header "Idempotency-Key: compose-smoke-lease-rollback" \
  --data "$rollback_request_body" >"$rollback_replay_file"
if ! cmp -s "$rollback_operation_file" "$rollback_replay_file"; then
  echo "Compose Worker rollback was not idempotent" >&2
  exit 1
fi
rolled_back_lease_output=$(cloud_agentsctl --project "$project_id" --lease "$profile_environment_id" \
  --request-id compose-smoke-rolled-back-lease environment-lease get)
case "$rolled_back_lease_output" in
  *'"generation":3'*'"observedPhase":"ready"'*'"cleanupPhase":"none"'*'"releaseDigest":"'"$worker_release_digest"'"'*) ;;
  *) echo "Compose Worker rollback did not restore the prior release: $rolled_back_lease_output" >&2; exit 1 ;;
esac
target_container_count=$(docker ps -q \
  --filter label=cloud-agents.dev/tenant=tenant-compose-smoke \
  --filter label=cloud-agents.dev/lease="$profile_environment_id" | wc -l | tr -d ' ')
if [ "$target_container_count" -ne 1 ]; then
  echo "Compose Worker rollback left $target_container_count active Workers" >&2
  exit 1
fi
target_container_id=$(docker ps -q \
  --filter label=cloud-agents.dev/tenant=tenant-compose-smoke \
  --filter label=cloud-agents.dev/lease="$profile_environment_id")
if [ "$(docker inspect "$target_container_id" --format '{{.Config.Image}}')" != "$worker_repository@$worker_release_digest" ]; then
  echo "Compose Worker rollback did not run the prior digest" >&2
  exit 1
fi

resume_preview_file="$smoke_directory/target-resume-preview.json"
control_plane_api "$smoke_directory/admin-curl.conf" GET \
  "$scheduling_path:scheduling-preview" compose-smoke-target-resume-preview >"$resume_preview_file"
resume_request_body=$(CLOUD_AGENTS_COMPOSE_SCHEDULING_FILE="$resume_preview_file" \
  CLOUD_AGENTS_COMPOSE_ENVIRONMENT_ID="$profile_environment_id" node <<'NODE'
const { readFileSync } = require("node:fs");
const value = JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_SCHEDULING_FILE, "utf8"));
if (value.spec?.currentState !== "drained" || value.spec?.desiredState !== "active" ||
    value.spec?.activeLeases?.length !== 1 ||
    value.spec.activeLeases[0]?.leaseId !== process.env.CLOUD_AGENTS_COMPOSE_ENVIRONMENT_ID) {
  throw new Error("Admin scheduling preview did not bind the Target resume impact");
}
process.stdout.write(JSON.stringify({
  expectedGeneration: value.spec.expectedGeneration,
  expectedResourceVersion: value.spec.expectedResourceVersion,
  desiredState: value.spec.desiredState,
  impactDigest: value.spec.impactDigest,
}));
NODE
)
resume_operation_file="$smoke_directory/target-resume-operation.json"
control_plane_api "$smoke_directory/admin-curl.conf" POST "$scheduling_path:scheduling" \
  compose-smoke-target-resume --header "Idempotency-Key: compose-smoke-target-resume" \
  --data "$resume_request_body" >"$resume_operation_file"
CLOUD_AGENTS_COMPOSE_OPERATION_FILE="$resume_operation_file" node <<'NODE'
const { readFileSync } = require("node:fs");
const value = JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_OPERATION_FILE, "utf8"));
if (value.action !== "target.resume" || value.resourceId !== "docker-compose-target" ||
    value.state !== "succeeded" || value.currentStep !== "complete") {
  throw new Error("Admin API did not persist a succeeded Target resume operation");
}
NODE
resumed_target_output=$(cloud_agentsctl --project "$project_id" --target docker-compose-target \
  --request-id compose-smoke-resumed-target-get target get)
case "$resumed_target_output" in
  *'"schedulingState":"active"'*'"observedPhase":"ready"'*) ;;
  *) echo "Compose Target resume did not restore scheduling: $resumed_target_output" >&2; exit 1 ;;
esac
resumed_environment_file="$smoke_directory/resumed-user-environment.json"
control_plane_api "$smoke_directory/user-curl.conf" POST \
  "/v1/tenants/tenant-compose-smoke/projects/$project_id/environments" \
  compose-smoke-resumed-environment --header "Idempotency-Key: compose-smoke-resumed-environment" \
  --data "$user_environment_body" >"$resumed_environment_file"
resumed_environment_id=$(CLOUD_AGENTS_COMPOSE_ENVIRONMENT_FILE="$resumed_environment_file" node -e \
  'const {readFileSync}=require("node:fs");const value=JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_ENVIRONMENT_FILE,"utf8"));if(value.observedPhase!=="ready")process.exit(1);process.stdout.write(value.environmentId)')
resumed_terminate_output=$(cloud_agentsctl --timeout 60s --project "$project_id" --lease "$resumed_environment_id" \
  --request-id compose-smoke-resumed-environment-terminate \
  --idempotency-key compose-smoke-resumed-environment-terminate environment-lease terminate --generation 1)
case "$resumed_terminate_output" in
  *'"generation":2'*'"observedPhase":"terminated"'*'"cleanupPhase":"complete"'*) ;;
  *) echo "Compose resumed Target environment did not terminate cleanly: $resumed_terminate_output" >&2; exit 1 ;;
esac

scheduling_operations_file="$smoke_directory/target-scheduling-operations.json"
scheduling_audit_file="$smoke_directory/target-scheduling-audit.json"
control_plane_api "$smoke_directory/admin-curl.conf" GET \
  "$scheduling_path/operations?pageSize=200" compose-smoke-target-scheduling-operations >"$scheduling_operations_file"
control_plane_api "$smoke_directory/admin-curl.conf" GET \
  "$scheduling_path/audit-events?pageSize=200" compose-smoke-target-scheduling-audit >"$scheduling_audit_file"
CLOUD_AGENTS_COMPOSE_OPERATIONS_FILE="$scheduling_operations_file" \
  CLOUD_AGENTS_COMPOSE_AUDIT_FILE="$scheduling_audit_file" node <<'NODE'
const { readFileSync } = require("node:fs");
const operations = JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_OPERATIONS_FILE, "utf8"));
const audit = JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_AUDIT_FILE, "utf8"));
for (const action of ["target.drain", "target.upgrade", "target.rollback", "target.resume"]) {
  if (!operations.operations?.some((item) => item.action === action && item.state === "succeeded") ||
      !audit.events?.some((item) => item.action === action && item.result === "succeeded")) {
    throw new Error(`Target ${action} did not close Operation and Audit authority`);
  }
}
NODE

codex_session_output=$(cloud_agentsctl_user --project "$project_id" --lease "$profile_environment_id" \
  --session session-compose-smoke \
  --request-id compose-smoke-session-create --idempotency-key compose-smoke-session-create \
  session create --provider codex)
case "$codex_session_output" in
  *'"providerKind":"codex"'*'"environmentLeaseId":"'"$profile_environment_id"'"'*'"environmentProfileId":"'"$profile_id"'"'*'"environmentProfileVersion":1'*) ;;
  *) echo "Compose User token did not create the Profile-bound Codex Session" >&2; exit 1 ;;
esac
codex_turn_output=$(cloud_agentsctl_user --project "$project_id" --session session-compose-smoke \
  --turn turn-compose-smoke \
  --request-id compose-smoke-turn-create --idempotency-key compose-smoke-turn-create \
  turn create --input "verify packaged Compose Runtime")
case "$codex_turn_output" in
  *'"state":"queued"'*) ;;
  *) echo "Compose User token did not persist the Codex Turn" >&2; exit 1 ;;
esac
claude_session_output=$(cloud_agentsctl_user --project "$project_id" --lease "$profile_environment_id" \
  --session session-compose-smoke-claude --request-id compose-smoke-claude-session-create \
  --idempotency-key compose-smoke-claude-session-create session create --provider claudeAgent)
case "$claude_session_output" in
  *'"providerKind":"claudeAgent"'*'"environmentLeaseId":"'"$profile_environment_id"'"'*'"environmentProfileId":"'"$profile_id"'"'*'"environmentProfileVersion":1'*) ;;
  *) echo "Compose User token did not create the Profile-bound Claude Code Session" >&2; exit 1 ;;
esac
claude_turn_output=$(cloud_agentsctl_user --project "$project_id" --session session-compose-smoke-claude \
  --turn turn-compose-smoke-claude --request-id compose-smoke-claude-turn-create \
  --idempotency-key compose-smoke-claude-turn-create turn create --input "verify packaged Compose Runtime")
case "$claude_turn_output" in
  *'"state":"queued"'*) ;;
  *) echo "Compose User token did not persist the Claude Code Turn" >&2; exit 1 ;;
esac
set +e
execute_output=$(cloud_agentsctl_user --project "$project_id" --session session-compose-smoke \
  --turn turn-compose-smoke --execution execution-compose-smoke \
  --request-id compose-smoke-execution --idempotency-key compose-smoke-execution \
  execution execute --runtime-mode approval-required --interaction-mode default \
  --input "verify packaged Compose Runtime" 2>&1)
execute_status=$?
set -e
if [ "$execute_status" -ne 2 ] || [ "$execute_output" != "cloud-agentsctl: managedAgentExecute: RUNTIME_FAILED" ]; then
  echo "Compose Runtime failure boundary changed: exit=$execute_status output=$execute_output" >&2
  exit 1
fi
execution_output=$(cloud_agentsctl_user --project "$project_id" --session session-compose-smoke \
  --turn turn-compose-smoke --execution execution-compose-smoke \
  --request-id compose-smoke-execution-get execution get)
case "$execution_output" in
  *'"state":"failed","errorCode":"runtime_open_failed"'*) ;;
  *) echo "Compose Runtime terminal failure was not persisted: $execution_output" >&2; exit 1 ;;
esac
events_output=$(cloud_agentsctl_user --project "$project_id" --session session-compose-smoke \
  --execution execution-compose-smoke --request-id compose-smoke-events \
  events watch --limit 1 --until-terminal)
case "$events_output" in
  *'"kind":"Event"'*'"operation":"execution.fail"'*'"executionId":"execution-compose-smoke"'*) ;;
  *) echo "Compose event watch did not reach the durable execution terminal event" >&2; exit 1 ;;
esac

compose restart control-plane >/dev/null
wait_ready
restarted_lease_output=$(cloud_agentsctl --project "$project_id" --lease "$profile_environment_id" \
  --request-id compose-smoke-restarted-lease environment-lease get)
case "$restarted_lease_output" in
  *'"generation":3'*'"observedPhase":"ready"'*'"cleanupPhase":"none"'*'"targetId":"docker-compose-target"'*) ;;
  *) echo "Compose Control Plane restart lost the Profile environment Lease: $restarted_lease_output" >&2; exit 1 ;;
esac
restarted_user_environment_file="$smoke_directory/restarted-user-environment.json"
control_plane_api "$smoke_directory/user-curl.conf" GET \
  "/v1/tenants/tenant-compose-smoke/projects/$project_id/environments/$profile_environment_id" \
  compose-smoke-restarted-user-environment >"$restarted_user_environment_file"
CLOUD_AGENTS_COMPOSE_ENVIRONMENT_FILE="$restarted_user_environment_file" \
  CLOUD_AGENTS_COMPOSE_ENVIRONMENT_ID="$profile_environment_id" \
  CLOUD_AGENTS_COMPOSE_PROFILE_ID="$profile_id" node <<'NODE'
const { readFileSync } = require("node:fs");
const value = JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_ENVIRONMENT_FILE, "utf8"));
const expectedKeys = ["apiVersion", "environmentId", "expiresAt", "kind", "observedPhase", "profileId", "profileVersion", "projectRef"].sort();
if (value.environmentId !== process.env.CLOUD_AGENTS_COMPOSE_ENVIRONMENT_ID ||
    value.profileId !== process.env.CLOUD_AGENTS_COMPOSE_PROFILE_ID || value.profileVersion !== 1 ||
    value.observedPhase !== "ready" || Object.keys(value).sort().join("\n") !== expectedKeys.join("\n")) {
  throw new Error("Control Plane restart changed the safe User Environment projection");
}
NODE
restarted_execution_output=$(cloud_agentsctl_user --project "$project_id" --session session-compose-smoke \
  --turn turn-compose-smoke --execution execution-compose-smoke \
  --request-id compose-smoke-restarted-execution execution get)
case "$restarted_execution_output" in
  *'"state":"failed","errorCode":"runtime_open_failed"'*) ;;
  *) echo "Compose Control Plane restart lost the durable execution" >&2; exit 1 ;;
esac

run_real_provider_turn() {
  provider_kind=$1
  provider_slug=$2
  session_id="session-compose-real-$provider_slug"
  turn_id="turn-compose-real-$provider_slug"
  execution_id="execution-compose-real-$provider_slug"
  artifact_path=".cloud-agents-stage3-acceptance/docker-target-real-$provider_slug.txt"
  expected_content="cloud-agents Docker target $provider_kind real E2E"
  case "$provider_kind" in
    codex) file_tool="Use apply_patch to create" ;;
    claudeAgent) file_tool="Use the Write tool to create" ;;
  esac
  prompt="$file_tool exactly one file at $artifact_path. Its complete contents must be the single ASCII line '$expected_content' followed by a newline. Do not modify any other file. Then reply done."

  cloud_agentsctl_user --project "$project_id" --lease "$profile_environment_id" --session "$session_id" \
    --request-id "compose-real-$provider_slug-session" --idempotency-key "compose-real-$provider_slug-session" \
    session create --provider "$provider_kind" >/dev/null
  cloud_agentsctl_user --project "$project_id" --session "$session_id" --turn "$turn_id" \
    --request-id "compose-real-$provider_slug-turn" --idempotency-key "compose-real-$provider_slug-turn" \
    turn create --input "$prompt" >/dev/null
  execution_file="$smoke_directory/$execution_id.json"
  cloud_agentsctl_user --timeout 10m --project "$project_id" --session "$session_id" --turn "$turn_id" \
    --execution "$execution_id" --request-id "compose-real-$provider_slug-execution" \
    --idempotency-key "compose-real-$provider_slug-execution" execution execute \
    --runtime-mode full-access --interaction-mode default --input "$prompt" >"$execution_file"

  artifact_index=$(
    CLOUD_AGENTS_COMPOSE_EXECUTION_FILE="$execution_file" \
    CLOUD_AGENTS_COMPOSE_ARTIFACT_PATH="$artifact_path" node <<'NODE'
const { readFileSync } = require("node:fs");
const value = JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_EXECUTION_FILE, "utf8"));
if (value.spec?.state !== "succeeded" || !value.messages?.some((message) => message.messageType === "Result")) {
  throw new Error("real Provider execution did not succeed with a Result");
}
const indexes = value.messages.flatMap((message, index) => {
  const artifact = message.payload?.artifact;
  return message.messageType === "ArtifactCandidate" &&
    artifact?.sourceRoot === "workspace" && artifact?.path === process.env.CLOUD_AGENTS_COMPOSE_ARTIFACT_PATH &&
    typeof artifact?.kind === "string" && artifact.kind.replaceAll("_", "-") === "generated-file" ? [index] : [];
});
if (indexes.length !== 1) throw new Error("real Provider execution did not emit the expected generated-file ArtifactCandidate");
process.stdout.write(String(indexes[0]));
NODE
  )
  artifact_file="$smoke_directory/$provider_slug-artifact.txt"
  cloud_agentsctl_user --project "$project_id" --session "$session_id" --turn "$turn_id" \
    --execution "$execution_id" --request-id "compose-real-$provider_slug-artifact" \
    execution download-artifact --message-index "$artifact_index" >"$artifact_file"
  CLOUD_AGENTS_COMPOSE_ARTIFACT_FILE="$artifact_file" \
  CLOUD_AGENTS_COMPOSE_EXPECTED_CONTENT="$expected_content" node <<'NODE'
const { readFileSync } = require("node:fs");
const actual = readFileSync(process.env.CLOUD_AGENTS_COMPOSE_ARTIFACT_FILE);
const expected = Buffer.from(`${process.env.CLOUD_AGENTS_COMPOSE_EXPECTED_CONTENT}\n`);
if (!actual.equals(expected)) throw new Error("real Provider generated-file Artifact content changed");
NODE
  real_events_output=$(cloud_agentsctl_user --timeout 60s --project "$project_id" --session "$session_id" \
    --execution "$execution_id" --request-id "compose-real-$provider_slug-events" \
    events watch --limit 64 --until-terminal)
  case "$real_events_output" in
    *'"operation":"execution.complete"'*'"executionId":"'"$execution_id"'"'*) ;;
    *) echo "Compose real $provider_kind event watch did not reach execution.complete" >&2; exit 1 ;;
  esac
}

if [ -n "$real_provider_credentials_directory" ]; then
  run_real_provider_turn codex codex
  run_real_provider_turn claudeAgent claude
fi

terminate_output=$(cloud_agentsctl --timeout 60s --project "$project_id" --lease "$profile_environment_id" \
  --request-id compose-smoke-lease-terminate --idempotency-key compose-smoke-lease-terminate \
  environment-lease terminate --generation 3)
case "$terminate_output" in
  *'"generation":4'*'"desiredPhase":"terminated"'*'"observedPhase":"terminated"'*'"cleanupPhase":"complete"'*) ;;
  *) echo "Compose Docker target Lease did not terminate cleanly: $terminate_output" >&2; exit 1 ;;
esac
replayed_terminate_output=$(cloud_agentsctl --timeout 60s --project "$project_id" --lease "$profile_environment_id" \
  --request-id compose-smoke-lease-terminate --idempotency-key compose-smoke-lease-terminate \
  environment-lease terminate --generation 3)
if [ "$replayed_terminate_output" != "$terminate_output" ]; then
  echo "Compose Docker target termination was not idempotent" >&2
  exit 1
fi
docker_cleanup_output=$(cloud_agentsctl --project "$project_id" --target docker-compose-target \
  --request-id compose-smoke-docker-target-cleanup --idempotency-key compose-smoke-docker-target-cleanup \
  target cleanup --expected-generation 1)
case "$docker_cleanup_output" in
  *'"generation":1'*'"targetKind":"docker"'*'"observedPhase":"ready"'*) ;;
  *) echo "Compose Docker target cleanup failed" >&2; exit 1 ;;
esac
target_container_count=$(docker ps -aq \
  --filter label=cloud-agents.dev/tenant=tenant-compose-smoke \
  --filter label=cloud-agents.dev/lease="$profile_environment_id" | wc -l | tr -d ' ')
if [ "$target_container_count" -ne 0 ]; then
  echo "Compose Docker target Worker was not cleaned up" >&2
  exit 1
fi
admin_workers_after_cleanup_file="$smoke_directory/admin-workers-after-cleanup.json"
control_plane_api "$smoke_directory/admin-curl.conf" GET \
  "/v1/admin/tenants/tenant-compose-smoke/projects/$project_id/workers?pageSize=200" \
  compose-smoke-admin-workers-after-cleanup >"$admin_workers_after_cleanup_file"
CLOUD_AGENTS_COMPOSE_WORKERS_FILE="$admin_workers_after_cleanup_file" node <<'NODE'
const { readFileSync } = require("node:fs");
const value = JSON.parse(readFileSync(process.env.CLOUD_AGENTS_COMPOSE_WORKERS_FILE, "utf8"));
if (value.kind !== "WorkerPage" || !Array.isArray(value.workers) || value.workers.length !== 0) {
  throw new Error("Admin API retained a Worker after Lease termination and cleanup");
}
NODE

backup="$smoke_directory/cloud-agents.dump"
compose --profile backup run --rm -T backup >"$backup"
test -s "$backup"
docker run --rm -i postgres:17.6-bookworm pg_restore --list <"$backup" >/dev/null
compose down --volumes --remove-orphans >/dev/null
compose --profile bootstrap run --rm bootstrap >/dev/null
compose --profile restore run --rm -T restore <"$backup" >/dev/null
set +e
restore_output=$(compose --profile restore run --rm -T restore <"$backup" 2>&1)
restore_status=$?
set -e
case "$restore_output" in
  *"restore target already contains the cloud_agents schema"*) ;;
  *) echo "Compose repeated restore did not fail closed: exit=$restore_status output=$restore_output" >&2; exit 1 ;;
esac
if [ "$restore_status" -eq 0 ]; then
  echo "Compose repeated restore unexpectedly succeeded" >&2
  exit 1
fi

compose up -d >/dev/null
wait_ready
restored_output=$(cloud_agentsctl_user --project "$project_id" --session session-compose-smoke \
  --turn turn-compose-smoke --execution execution-compose-smoke \
  --request-id compose-smoke-restored-execution execution get)
case "$restored_output" in
  *'"state":"failed","errorCode":"runtime_open_failed"'*) ;;
  *) echo "Compose restore omitted the durable execution" >&2; exit 1 ;;
esac

echo "platform Compose smoke passed ($image_platform, $cli_target, profile=$profile_id:v1, environment=$profile_environment_id, docker-workers=0)"
