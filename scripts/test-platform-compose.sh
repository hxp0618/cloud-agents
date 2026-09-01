#!/bin/sh

set -eu

if [ "$#" -ne 1 ] || [ ! -d "$1" ]; then
  echo "usage: test-platform-compose.sh PLATFORM_RELEASE_DIRECTORY" >&2
  exit 2
fi
for command in curl docker node openssl; do
  if ! command -v "$command" >/dev/null 2>&1; then
    echo "platform Compose smoke requires $command" >&2
    exit 2
  fi
done

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
compose() {
  docker compose --env-file "$environment_file" -f "$compose_file" "$@"
}
cleanup() {
  status=$?
  trap - 0 HUP INT TERM
  if [ -f "$environment_file" ]; then
    compose down --volumes --remove-orphans >/dev/null 2>&1 || true
  fi
  docker image rm "${project}-control-plane" "${project}-worker" "${project}-migrate" >/dev/null 2>&1 || true
  rm -rf -- "$smoke_directory"
  exit "$status"
}
trap cleanup 0 HUP INT TERM

mkdir -p "$smoke_directory/deployment" "$smoke_directory/control-plane-tls" \
  "$smoke_directory/worker-tls" "$smoke_directory/provider-credentials" "$smoke_directory/workspace"
chmod 0755 "$smoke_directory" "$smoke_directory/control-plane-tls" \
  "$smoke_directory/worker-tls" "$smoke_directory/provider-credentials"
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
writeFileSync(`${state}/token`, `${signingInput}.${signature}\n`);
writeFileSync(`${state}/runtime.env`, "CLOUD_AGENT_PROVIDER_HOST_EXPERIMENTAL_PROVIDERS=codex,claudeAgent\nCLOUD_AGENT_PROVIDER_OUTER_SANDBOX_PROFILE=single-tenant-trusted-v1\n");
writeFileSync(`${state}/provider-credentials/unavailable-provider.json`, '{"payload":{}}\n');
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
  `CLOUD_AGENTS_ADMISSION_TOKEN=${randomBytes(24).toString("hex")}`,
];
writeFileSync(`${state}/compose.env`, `${values.join("\n")}\n`);
chmodSync(`${state}/auth.json`, 0o444);
chmodSync(`${state}/runtime.env`, 0o444);
chmodSync(`${state}/provider-credentials/unavailable-provider.json`, 0o444);
chmodSync(`${state}/token`, 0o600);
chmodSync(`${state}/compose.env`, 0o600);
NODE

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

cloud_agentsctl --project "$project_id" --session session-compose-smoke \
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
  *) echo "Compose Runtime terminal failure was not persisted" >&2; exit 1 ;;
esac
events_output=$(cloud_agentsctl --project "$project_id" --session session-compose-smoke \
  --execution execution-compose-smoke --request-id compose-smoke-events \
  events watch --limit 1 --until-terminal)
case "$events_output" in
  *'"kind":"Event"'*'"operation":"execution.fail"'*'"executionId":"execution-compose-smoke"'*) ;;
  *) echo "Compose event watch did not reach the durable execution terminal event" >&2; exit 1 ;;
esac

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
