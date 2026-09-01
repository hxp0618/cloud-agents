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
target_worker_credentials_volume="${project}-target-worker-credentials"
target_provider_credentials_volume="${project}-target-provider-credentials"
retry_provider_credentials_volume="${project}-retry-provider-credentials"
compose() {
  docker compose --env-file "$environment_file" -f "$compose_file" -f "$compose_override_file" "$@"
}
cleanup() {
  status=$?
  trap - 0 HUP INT TERM
  for target_lease in lease-compose-target lease-compose-target-retry; do
    for container in $(docker ps -aq \
      --filter label=cloud-agents.dev/tenant=tenant-compose-smoke \
      --filter label=cloud-agents.dev/lease="$target_lease"); do
      if [ "$status" -ne 0 ]; then
        docker logs "$container" >&2 || true
      fi
      docker rm -f "$container" >/dev/null 2>&1 || true
    done
  done
  if [ -f "$environment_file" ]; then
    if [ "$status" -ne 0 ]; then
      compose logs --no-color --tail=200 control-plane worker migrate postgres >&2 || true
    fi
    compose down --volumes --remove-orphans >/dev/null 2>&1 || true
  fi
  docker rm -f "$registry_container" >/dev/null 2>&1 || true
  docker volume rm "$target_worker_credentials_volume" "$target_provider_credentials_volume" \
    "$retry_provider_credentials_volume" >/dev/null 2>&1 || true
  if [ -n "$worker_repository" ]; then
    docker image rm "$worker_repository:smoke" >/dev/null 2>&1 || true
  fi
  if [ -n "$worker_repository" ] && [ -n "$worker_release_digest" ]; then
    docker image rm "$worker_repository@$worker_release_digest" >/dev/null 2>&1 || true
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
  "$smoke_directory/target-worker-credentials" "$smoke_directory/target-provider-credentials"
chmod 0755 "$smoke_directory" "$smoke_directory/control-plane-tls" \
  "$smoke_directory/worker-tls" "$smoke_directory/provider-credentials" \
  "$smoke_directory/docker-target-credentials" "$smoke_directory/docker-target-credentials/docker-compose-target" \
  "$smoke_directory/kubernetes-target-credentials" \
  "$smoke_directory/target-worker-credentials" "$smoke_directory/target-provider-credentials"
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
const claims = {
  iss: issuer, sub: "user-compose-smoke", aud: audience, exp: now + 1800, iat: now - 10,
  jti: "compose-smoke-token", client_id: "compose-smoke-client",
  scope: ["organizations.list", "projects.act", "projects.create", "projects.get"].sort().join(" "),
  "https://schemas.cloud-agents.dev/claims/security-epoch": 1,
  "https://schemas.cloud-agents.dev/claims/subject-kind": "user",
  "https://schemas.cloud-agents.dev/claims/tenant-id": "tenant-compose-smoke",
  "https://schemas.cloud-agents.dev/claims/token-profile": "cloud-agents-access-token/v1",
};
const encode = (value) => Buffer.from(JSON.stringify(value)).toString("base64url");
const signingInput = `${encode({ alg: "RS256", kid, typ: "at+jwt" })}.${encode(claims)}`;
const signature = createSign("RSA-SHA256").update(signingInput).end().sign(privateKey).toString("base64url");
const admissionToken = randomBytes(24).toString("hex");
const kubernetesToken = randomBytes(24).toString("hex");
writeFileSync(`${state}/token`, `${signingInput}.${signature}\n`);
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

docker volume create "$target_worker_credentials_volume" >/dev/null
docker volume create "$target_provider_credentials_volume" >/dev/null
docker run --rm --user 0 --entrypoint /bin/sh \
  -v "$target_worker_credentials_volume:/target" \
  -v "$smoke_directory/target-worker-credentials:/source:ro" \
  postgres:17.6-bookworm -ec \
  'cp /source/server.crt /source/server.key /source/client-ca.crt /source/admission-token /target/ && chown 1000:1000 /target/* && chmod 0400 /target/*'
docker run --rm --user 0 --entrypoint /bin/sh \
  -v "$target_provider_credentials_volume:/target" \
  -v "$smoke_directory/target-provider-credentials:/source:ro" \
  postgres:17.6-bookworm -ec \
  'cp /source/tenant-compose-smoke.unavailable-provider.json /target/ && chown 1000:1000 /target/* && chmod 0400 /target/*'
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

lease_output=$(cloud_agentsctl --timeout 60s --project "$project_id" --target docker-compose-target \
  --lease lease-compose-target --request-id compose-smoke-lease-create \
  --idempotency-key compose-smoke-lease-create environment-lease create \
  --name lease-compose-target --release-digest "$worker_release_digest" \
  --expected-target-generation 1 --provider-credential-ref "$target_provider_credentials_volume" \
  --cpu-limit-millis 1000 --memory-limit-bytes 536870912 --ttl-seconds 3600)
case "$lease_output" in
  *'"generation":1'*'"observedPhase":"ready"'*'"cleanupPhase":"none"'*'"workerEndpoint":"https://host.docker.internal:'*'"workerSpiffeId":"spiffe://cloud-agents.compose/worker-target"'*) ;;
  *) echo "Compose Docker target Worker did not become ready: $lease_output" >&2; exit 1 ;;
esac
replayed_lease_output=$(cloud_agentsctl --timeout 60s --project "$project_id" --target docker-compose-target \
  --lease lease-compose-target --request-id compose-smoke-lease-create \
  --idempotency-key compose-smoke-lease-create environment-lease create \
  --name lease-compose-target --release-digest "$worker_release_digest" \
  --expected-target-generation 1 --provider-credential-ref "$target_provider_credentials_volume" \
  --cpu-limit-millis 1000 --memory-limit-bytes 536870912 --ttl-seconds 3600)
case "$replayed_lease_output" in
  *'"generation":1'*'"observedPhase":"ready"'*'"cleanupPhase":"none"'*'"targetId":"docker-compose-target"'*'"workerSpiffeId":"spiffe://cloud-agents.compose/worker-target"'*) ;;
  *) echo "Compose Docker target deployment replay changed identity or state: $replayed_lease_output" >&2; exit 1 ;;
esac
target_container_count=$(docker ps -q \
  --filter label=cloud-agents.dev/tenant=tenant-compose-smoke \
  --filter label=cloud-agents.dev/lease=lease-compose-target | wc -l | tr -d ' ')
if [ "$target_container_count" -ne 1 ]; then
  echo "Compose Docker target created $target_container_count Workers, expected 1" >&2
  exit 1
fi

cloud_agentsctl --project "$project_id" --lease lease-compose-target --session session-compose-smoke \
  --request-id compose-smoke-session-create --idempotency-key compose-smoke-session-create \
  session create --provider unavailable-provider >/dev/null
cloud_agentsctl --project "$project_id" --session session-compose-smoke --turn turn-compose-smoke \
  --request-id compose-smoke-turn-create --idempotency-key compose-smoke-turn-create \
  turn create --input "verify packaged Compose Runtime" >/dev/null
set +e
execute_output=$(cloud_agentsctl --project "$project_id" --session session-compose-smoke \
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
execution_output=$(cloud_agentsctl --project "$project_id" --session session-compose-smoke \
  --turn turn-compose-smoke --execution execution-compose-smoke \
  --request-id compose-smoke-execution-get execution get)
case "$execution_output" in
  *'"state":"failed","errorCode":"provider_not_installed"'*'"messageType":"Error"'*) ;;
  *) echo "Compose Runtime terminal failure was not persisted: $execution_output" >&2; exit 1 ;;
esac
events_output=$(cloud_agentsctl --project "$project_id" --session session-compose-smoke \
  --execution execution-compose-smoke --request-id compose-smoke-events \
  events watch --limit 1 --until-terminal)
case "$events_output" in
  *'"kind":"Event"'*'"operation":"execution.fail"'*'"executionId":"execution-compose-smoke"'*) ;;
  *) echo "Compose event watch did not reach the durable execution terminal event" >&2; exit 1 ;;
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

  cloud_agentsctl --project "$project_id" --lease lease-compose-target --session "$session_id" \
    --request-id "compose-real-$provider_slug-session" --idempotency-key "compose-real-$provider_slug-session" \
    session create --provider "$provider_kind" >/dev/null
  cloud_agentsctl --project "$project_id" --session "$session_id" --turn "$turn_id" \
    --request-id "compose-real-$provider_slug-turn" --idempotency-key "compose-real-$provider_slug-turn" \
    turn create --input "$prompt" >/dev/null
  execution_file="$smoke_directory/$execution_id.json"
  cloud_agentsctl --timeout 10m --project "$project_id" --session "$session_id" --turn "$turn_id" \
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
  cloud_agentsctl --project "$project_id" --session "$session_id" --turn "$turn_id" \
    --execution "$execution_id" --request-id "compose-real-$provider_slug-artifact" \
    execution download-artifact --message-index "$artifact_index" >"$artifact_file"
  CLOUD_AGENTS_COMPOSE_ARTIFACT_FILE="$artifact_file" \
  CLOUD_AGENTS_COMPOSE_EXPECTED_CONTENT="$expected_content" node <<'NODE'
const { readFileSync } = require("node:fs");
const actual = readFileSync(process.env.CLOUD_AGENTS_COMPOSE_ARTIFACT_FILE);
const expected = Buffer.from(`${process.env.CLOUD_AGENTS_COMPOSE_EXPECTED_CONTENT}\n`);
if (!actual.equals(expected)) throw new Error("real Provider generated-file Artifact content changed");
NODE
  real_events_output=$(cloud_agentsctl --timeout 60s --project "$project_id" --session "$session_id" \
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

terminate_output=$(cloud_agentsctl --timeout 60s --project "$project_id" --lease lease-compose-target \
  --request-id compose-smoke-lease-terminate --idempotency-key compose-smoke-lease-terminate \
  environment-lease terminate --generation 1)
case "$terminate_output" in
  *'"generation":2'*'"desiredPhase":"terminated"'*'"observedPhase":"terminated"'*'"cleanupPhase":"complete"'*) ;;
  *) echo "Compose Docker target Lease did not terminate cleanly: $terminate_output" >&2; exit 1 ;;
esac
replayed_terminate_output=$(cloud_agentsctl --timeout 60s --project "$project_id" --lease lease-compose-target \
  --request-id compose-smoke-lease-terminate --idempotency-key compose-smoke-lease-terminate \
  environment-lease terminate --generation 1)
if [ "$replayed_terminate_output" != "$terminate_output" ]; then
  echo "Compose Docker target termination was not idempotent" >&2
  exit 1
fi
target_container_count=$(docker ps -aq \
  --filter label=cloud-agents.dev/tenant=tenant-compose-smoke \
  --filter label=cloud-agents.dev/lease=lease-compose-target | wc -l | tr -d ' ')
if [ "$target_container_count" -ne 0 ]; then
  echo "Compose Docker target Worker was not cleaned up" >&2
  exit 1
fi

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
restored_output=$(cloud_agentsctl --project "$project_id" --session session-compose-smoke \
  --turn turn-compose-smoke --execution execution-compose-smoke \
  --request-id compose-smoke-restored-execution execution get)
case "$restored_output" in
  *'"state":"failed","errorCode":"provider_not_installed"'*) ;;
  *) echo "Compose restore omitted the durable execution" >&2; exit 1 ;;
esac

echo "platform Compose smoke passed ($image_platform, $cli_target)"
