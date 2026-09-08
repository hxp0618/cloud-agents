#!/bin/sh

set -eu

if [ "$#" -ne 1 ] || [ ! -d "$1" ]; then
  echo "usage: CLOUD_AGENTS_HELM_CONTEXT=<context> test-platform-helm.sh PLATFORM_RELEASE_DIRECTORY" >&2
  exit 2
fi
for command in curl docker helm kubectl node openssl ssh-keygen ssh-keyscan tar; do
  command -v "$command" >/dev/null 2>&1 || {
    echo "platform Helm smoke requires $command" >&2
    exit 2
  }
done

context=${CLOUD_AGENTS_HELM_CONTEXT:?set the explicitly approved Kubernetes context}
if [ "$(kubectl config current-context)" != "$context" ]; then
  echo "current Kubernetes context does not match CLOUD_AGENTS_HELM_CONTEXT" >&2
  exit 2
fi
kubectl --context "$context" cluster-info >/dev/null

candidate_directory=$(CDPATH= cd -- "$1" && pwd)
case "$(uname -s)/$(uname -m)" in
  Darwin/arm64) cli_target=darwin-arm64; image_platform=linux/arm64 ;;
  Darwin/x86_64) cli_target=darwin-amd64; image_platform=linux/amd64 ;;
  Linux/aarch64 | Linux/arm64) cli_target=linux-arm64; image_platform=linux/arm64 ;;
  Linux/x86_64 | Linux/amd64) cli_target=linux-amd64; image_platform=linux/amd64 ;;
  *) echo "platform Helm smoke requires Darwin or Linux on amd64 or arm64" >&2; exit 2 ;;
esac
cli=$candidate_directory/cloud-agentsctl-$cli_target
test -x "$cli" || {
  echo "platform release is missing executable cloud-agentsctl-$cli_target" >&2
  exit 1
}
set -- "$candidate_directory"/cloud-agents-deployment-*.tar
if [ "$#" -ne 1 ] || [ ! -f "$1" ]; then
  echo "platform release must contain exactly one deployment package" >&2
  exit 1
fi

smoke_directory=$(mktemp -d "${TMPDIR:-/tmp}/cloud-agents-helm-smoke.XXXXXX")
namespace=cloud-agents-helm-smoke-$$
release_name=cloud-agents
image_tag=smoke-$$
control_plane_image=cloud-agents/helm-smoke-control-plane:$image_tag
worker_image=cloud-agents/helm-smoke-worker:$image_tag
migrate_image=cloud-agents/helm-smoke-migrate:$image_tag
gateway_image=cloud-agents/helm-smoke-access-gateway:$image_tag
admin_web_image=cloud-agents/helm-smoke-admin-web:$image_tag
admin_port=$((20000 + ($$ % 1000) * 10 + 1))
control_plane_port=$((admin_port + 1))
gateway_port=$((admin_port + 2))
gateway_ssh_port=$((admin_port + 3))
admin_forward_pid=
control_plane_forward_pid=
gateway_forward_pid=
verified=false
migration_summary=

cleanup() {
  status=$?
  trap - 0 HUP INT TERM
  set +e
  for pid in "$admin_forward_pid" "$control_plane_forward_pid" "$gateway_forward_pid"; do
    if [ -n "$pid" ]; then
      kill "$pid" >/dev/null 2>&1
      wait "$pid" 2>/dev/null
    fi
  done
  if [ "$status" -ne 0 ]; then
    kubectl --context "$context" -n "$namespace" get pods,services,jobs,pvc -o wide >&2
    kubectl --context "$context" -n "$namespace" logs -l app.kubernetes.io/instance="$release_name" --all-containers --tail=100 >&2
  fi
  owner=$(kubectl --context "$context" get namespace "$namespace" -o jsonpath='{.metadata.labels.cloud-agents\.dev/test-run}' 2>/dev/null)
  if [ "$owner" = "$namespace" ]; then
    kubectl --context "$context" delete namespace "$namespace" --wait=true --timeout=180s >/dev/null || status=1
  elif [ -n "$owner" ]; then
    echo "refusing to delete namespace without the exact test ownership label" >&2
    status=1
  fi
  if kubectl --context "$context" get pv -o json | node -e '
    const fs = require("node:fs");
    const namespace = process.argv[1];
    const value = JSON.parse(fs.readFileSync(0, "utf8"));
    process.exit(value.items.some((item) => item.spec.claimRef?.namespace === namespace) ? 1 : 0);
  ' "$namespace"; then :; else
    echo "test-owned persistent volume remains after namespace cleanup" >&2
    status=1
  fi
  for image in "$control_plane_image" "$worker_image" "$migrate_image" "$gateway_image" "$admin_web_image"; do
    docker image rm "$image" >/dev/null 2>&1 || status=1
  done
  case "$smoke_directory" in
    "${TMPDIR:-/tmp}"/cloud-agents-helm-smoke.*) rm -rf -- "$smoke_directory" ;;
    *) echo "refusing to remove unexpected Helm smoke directory" >&2; status=1 ;;
  esac
  if [ "$status" -eq 0 ] && [ "$verified" = true ]; then
    echo "Helm Admin Web smoke passed (context=$context, schema=$migration_summary, user-admin=403, gateway=TLS+SSH, restart=passed, cleanup=zero)"
  fi
  exit "$status"
}
trap cleanup 0 HUP INT TERM

if kubectl --context "$context" get namespace "$namespace" >/dev/null 2>&1; then
  echo "test namespace already exists" >&2
  exit 1
fi
kubectl --context "$context" create namespace "$namespace" >/dev/null
kubectl --context "$context" label namespace "$namespace" cloud-agents.dev/test-run="$namespace" >/dev/null

tar -xf "$1" -C "$smoke_directory"
chart=$smoke_directory/deploy/helm/cloud-agents
for component in control-plane worker migrate access-gateway; do
  case "$component" in
    control-plane) image=$control_plane_image ;;
    worker) image=$worker_image ;;
    migrate) image=$migrate_image ;;
    access-gateway) image=$gateway_image ;;
  esac
  docker build --quiet --platform "$image_platform" --tag "$image" \
    --file "$smoke_directory/deploy/docker/$component.Dockerfile" "$candidate_directory" >/dev/null
done
docker build --quiet --platform "$image_platform" --tag "$admin_web_image" \
  --file "$smoke_directory/deploy/docker/admin-web.Dockerfile" "$smoke_directory/deploy" >/dev/null

openssl req -x509 -newkey rsa:2048 -nodes -sha256 -days 1 \
  -subj /CN=cloud-agents-helm-smoke-ca \
  -keyout "$smoke_directory/ca.key" -out "$smoke_directory/ca.crt" \
  -addext basicConstraints=critical,CA:TRUE -addext keyUsage=critical,keyCertSign,cRLSign >/dev/null 2>&1
issue_certificate() {
  name=$1
  subject=$2
  alternative_names=$3
  extended_usage=$4
  openssl req -newkey rsa:2048 -nodes -sha256 -subj "/CN=$subject" \
    -keyout "$smoke_directory/$name.key" -out "$smoke_directory/$name.csr" \
    -addext "subjectAltName=$alternative_names" -addext "extendedKeyUsage=$extended_usage" \
    -addext keyUsage=digitalSignature >/dev/null 2>&1
  openssl x509 -req -sha256 -days 1 -in "$smoke_directory/$name.csr" \
    -CA "$smoke_directory/ca.crt" -CAkey "$smoke_directory/ca.key" -CAcreateserial \
    -copy_extensions copy -out "$smoke_directory/$name.crt" >/dev/null 2>&1
}
service_prefix=$release_name-cloud-agents
issue_certificate control-plane "$service_prefix-control-plane" \
  "DNS:$service_prefix-control-plane,DNS:$service_prefix-control-plane.$namespace.svc,IP:127.0.0.1" serverAuth
issue_certificate worker "$service_prefix-worker" \
  "DNS:$service_prefix-worker,DNS:$service_prefix-worker.$namespace.svc,URI:spiffe://cloud-agents.helm/worker" serverAuth
issue_certificate worker-client control-plane \
  "URI:spiffe://cloud-agents.helm/control-plane" clientAuth
issue_certificate access-gateway "$service_prefix-access-gateway" \
  "DNS:$service_prefix-access-gateway,DNS:$service_prefix-access-gateway.$namespace.svc,IP:127.0.0.1" serverAuth
ssh-keygen -q -t ed25519 -N "" -f "$smoke_directory/gateway-ssh-host-key"

CLOUD_AGENTS_HELM_SMOKE_STATE=$smoke_directory node <<'NODE'
const { createSign, generateKeyPairSync, randomBytes } = require("node:crypto");
const { writeFileSync } = require("node:fs");
const state = process.env.CLOUD_AGENTS_HELM_SMOKE_STATE;
const issuer = "https://issuer.helm-smoke.test";
const audience = "https://api.helm-smoke.test";
const kid = "helm-smoke-key";
const now = Math.floor(Date.now() / 1000);
const { privateKey, publicKey } = generateKeyPairSync("rsa", { modulusLength: 2048 });
const exported = publicKey.export({ format: "jwk" });
const jwk = { alg: "RS256", e: exported.e, key_ops: ["verify"], kid, kty: "RSA", n: exported.n, use: "sig" };
writeFileSync(`${state}/auth.json`, `${JSON.stringify({
  issuer, audience, generation: 1, securityEpoch: 1, notBefore: now - 60, expiresAt: now + 3600,
  keys: [{ jwk, enabled: true, notBefore: now - 60, notAfter: now + 3600 }],
})}\n`);
const base = {
  iss: issuer, sub: "user-helm-smoke", aud: audience, exp: now + 1800, iat: now - 10,
  client_id: "helm-smoke-client", jti: "",
  "https://schemas.cloud-agents.dev/claims/security-epoch": 1,
  "https://schemas.cloud-agents.dev/claims/subject-kind": "user",
  "https://schemas.cloud-agents.dev/claims/tenant-id": "tenant-helm-smoke",
  "https://schemas.cloud-agents.dev/claims/token-profile": "cloud-agents-access-token/v1",
};
const encode = (value) => Buffer.from(JSON.stringify(value)).toString("base64url");
const issue = (jti, scopes) => {
  const claims = { ...base, jti, scope: [...scopes].sort().join(" ") };
  const input = `${encode({ alg: "RS256", kid, typ: "at+jwt" })}.${encode(claims)}`;
  return `${input}.${createSign("RSA-SHA256").update(input).end().sign(privateKey).toString("base64url")}`;
};
writeFileSync(`${state}/admin-token`, `${issue("helm-smoke-admin", [
  "audit.list", "environments.create", "environments.get", "environment-profiles.list",
  "leases.act", "leases.get", "leases.list", "organizations.list", "profiles.act",
  "operations.list", "profiles.create", "profiles.get", "profiles.list", "projects.act", "projects.create",
  "network-policies.get", "network-policies.list", "network-policies.update", "projects.get", "quotas.get", "quotas.update",
  "releases.create", "releases.list", "sandboxes.act", "sandboxes.get", "sandboxes.list",
  "storage-policies.get", "storage-policies.list", "storage-policies.update", "targets.act", "targets.create",
  "targets.get", "targets.list", "workers.list", "snapshots.act", "snapshots.create", "snapshots.delete",
  "snapshots.get", "snapshots.list",
])}\n`);
writeFileSync(`${state}/user-token`, `${issue("helm-smoke-user", [
  "environment-quotas.get", "environments.create", "environments.get", "environment-profiles.list",
  "projects.act", "projects.get", "sandboxes.update",
])}\n`);
writeFileSync(`${state}/access-grant.key`, randomBytes(32));
writeFileSync(`${state}/admission-token`, randomBytes(24).toString("hex"));
writeFileSync(`${state}/database-password`, randomBytes(24).toString("hex"));
writeFileSync(`${state}/tenant-helm-smoke.unavailable-provider.json`, '{"payload":{}}\n');
NODE
chmod 0600 "$smoke_directory"/admin-token "$smoke_directory"/user-token \
  "$smoke_directory"/access-grant.key "$smoke_directory"/admission-token \
  "$smoke_directory"/database-password "$smoke_directory"/gateway-ssh-host-key

database_password=$(sed -n '1p' "$smoke_directory/database-password")
admission_token=$(sed -n '1p' "$smoke_directory/admission-token")
kubectl --context "$context" -n "$namespace" run postgres --image=postgres:17.6-bookworm \
  --env=POSTGRES_DB=cloud_agents --env=POSTGRES_USER=cloud_agents_install_admin \
  --env="POSTGRES_PASSWORD=$database_password" >/dev/null
kubectl --context "$context" -n "$namespace" expose pod postgres --port=5432 --target-port=5432 >/dev/null
kubectl --context "$context" -n "$namespace" wait pod/postgres --for=condition=Ready --timeout=120s >/dev/null
postgres_pod=postgres
attempt=0
ready_checks=0
while [ "$ready_checks" -lt 3 ]; do
  attempt=$((attempt + 1))
  if [ "$attempt" -ge 60 ]; then
    echo "Helm smoke PostgreSQL did not become ready" >&2
    exit 1
  fi
  if kubectl --context "$context" -n "$namespace" exec "$postgres_pod" -- \
    pg_isready -U cloud_agents_install_admin -d cloud_agents >/dev/null 2>&1; then
    ready_checks=$((ready_checks + 1))
  else
    ready_checks=0
  fi
  sleep 1
done
kubectl --context "$context" -n "$namespace" cp \
  "$smoke_directory/deploy/bootstrap/roles.sql" "$postgres_pod:/tmp/roles.sql"
kubectl --context "$context" -n "$namespace" cp \
  "$smoke_directory/deploy/compose/provision.sql" "$postgres_pod:/tmp/provision.sql"
kubectl --context "$context" -n "$namespace" cp \
  "$smoke_directory/deploy/bootstrap/database.sql" "$postgres_pod:/tmp/database.sql"
kubectl --context "$context" -n "$namespace" exec "$postgres_pod" -- \
  psql -U cloud_agents_install_admin -d cloud_agents --single-transaction -v ON_ERROR_STOP=1 \
  -v cloud_agents_database=cloud_agents -v cloud_agents_database_owner=cloud_agents_database_owner \
  -v cloud_agents_migration_password="$database_password" -v cloud_agents_runtime_password="$database_password" \
  -v cloud_agents_tenant_bootstrap_password="$database_password" \
  -f /tmp/roles.sql -f /tmp/provision.sql -f /tmp/database.sql >/dev/null
kubectl --context "$context" -n "$namespace" exec "$postgres_pod" -- env \
  PGPASSWORD="$database_password" psql -h postgres -U cloud_agents_migration -d cloud_agents \
  -X -A -t -v ON_ERROR_STOP=1 -c 'SELECT current_user' | grep -Fx cloud_agents_migration >/dev/null

kubectl --context "$context" -n "$namespace" create secret generic cloud-agents-database \
  --from-literal=runtime-url="postgresql://cloud_agents_runtime_login:$database_password@postgres:5432/cloud_agents?sslmode=disable" \
  --from-literal=migration-url="postgresql://cloud_agents_migration:$database_password@postgres:5432/cloud_agents?sslmode=disable" \
  --from-literal=tenant-bootstrap-url="postgresql://cloud_agents_tenant_bootstrap:$database_password@postgres:5432/cloud_agents?sslmode=disable" >/dev/null
kubectl --context "$context" -n "$namespace" create secret generic cloud-agents-auth \
  --from-file=auth.json="$smoke_directory/auth.json" >/dev/null
kubectl --context "$context" -n "$namespace" create secret generic cloud-agents-access-grant \
  --from-file=access-grant.key="$smoke_directory/access-grant.key" >/dev/null
kubectl --context "$context" -n "$namespace" create secret generic cloud-agents-control-plane-tls \
  --from-file=tls.crt="$smoke_directory/control-plane.crt" \
  --from-file=tls.key="$smoke_directory/control-plane.key" --from-file=ca.crt="$smoke_directory/ca.crt" >/dev/null
kubectl --context "$context" -n "$namespace" create secret generic cloud-agents-worker-tls \
  --from-file=tls.crt="$smoke_directory/worker.crt" --from-file=tls.key="$smoke_directory/worker.key" \
  --from-file=ca.crt="$smoke_directory/ca.crt" >/dev/null
kubectl --context "$context" -n "$namespace" create secret generic cloud-agents-worker-client-tls \
  --from-file=tls.crt="$smoke_directory/worker-client.crt" \
  --from-file=tls.key="$smoke_directory/worker-client.key" >/dev/null
kubectl --context "$context" -n "$namespace" create secret generic cloud-agents-access-gateway-tls \
  --from-file=tls.crt="$smoke_directory/access-gateway.crt" \
  --from-file=tls.key="$smoke_directory/access-gateway.key" >/dev/null
kubectl --context "$context" -n "$namespace" create secret generic cloud-agents-access-gateway-ssh \
  --from-file=ssh-host-key="$smoke_directory/gateway-ssh-host-key" >/dev/null
kubectl --context "$context" -n "$namespace" create secret generic cloud-agents-admission \
  --from-literal=lease-id=helm-smoke-lease --from-literal=generation=1 \
  --from-literal=token="$admission_token" >/dev/null
kubectl --context "$context" -n "$namespace" create secret generic cloud-agents-runtime-env \
  --from-literal=CLOUD_AGENT_PROVIDER_HOST_EXPERIMENTAL_PROVIDERS=unavailable-provider >/dev/null
kubectl --context "$context" -n "$namespace" create secret generic cloud-agents-provider-credentials \
  --from-file=tenant-helm-smoke.unavailable-provider.json="$smoke_directory/tenant-helm-smoke.unavailable-provider.json" >/dev/null
kubectl --context "$context" -n "$namespace" create secret generic cloud-agents-kubernetes-targets \
  --from-literal=placeholder=helm-smoke >/dev/null
kubectl --context "$context" -n "$namespace" create secret generic cloud-agents-tenant-bootstrap \
  --from-literal=CLOUD_AGENTS_TENANT_UID=tenant-helm-smoke \
  --from-literal=CLOUD_AGENTS_TENANT_NAME=tenant-helm-smoke \
  --from-literal=CLOUD_AGENTS_TENANT_DISPLAY_NAME='Helm Smoke Tenant' \
  --from-literal=CLOUD_AGENTS_ORGANIZATION_UID=organization-helm-smoke \
  --from-literal=CLOUD_AGENTS_ORGANIZATION_NAME=organization-helm-smoke \
  --from-literal=CLOUD_AGENTS_ORGANIZATION_DISPLAY_NAME='Helm Smoke Organization' \
  --from-literal=CLOUD_AGENTS_ADMIN_SUBJECT_KIND=user \
  --from-literal=CLOUD_AGENTS_ADMIN_SUBJECT_ISSUER=https://issuer.helm-smoke.test \
  --from-literal=CLOUD_AGENTS_ADMIN_SUBJECT_VALUE=user-helm-smoke \
  --from-literal=CLOUD_AGENTS_ADMIN_MEMBERSHIP_UID=membership-helm-admin \
  --from-literal=CLOUD_AGENTS_ADMIN_MEMBERSHIP_NAME=membership-helm-admin \
  --from-literal=CLOUD_AGENTS_ADMIN_ROLE_BINDING_UID=role-binding-helm-admin \
  --from-literal=CLOUD_AGENTS_ADMIN_ROLE_BINDING_NAME=role-binding-helm-admin \
  --from-literal=CLOUD_AGENTS_TENANT_AUDIT_FACT_UID=audit-helm-tenant \
  --from-literal=CLOUD_AGENTS_MEMBERSHIP_AUDIT_FACT_UID=audit-helm-membership \
  --from-literal=CLOUD_AGENTS_ROLE_BINDING_AUDIT_FACT_UID=audit-helm-role-binding \
  --from-literal=CLOUD_AGENTS_BOOTSTRAP_REASON_CODE=helm-smoke >/dev/null

image_repository() { printf '%s' "${1%:*}"; }
helm --kube-context "$context" install "$release_name" "$chart" --namespace "$namespace" \
  --wait --timeout 5m --set-string runtime.workspace.size=1Gi \
  --set-string controlPlane.workerSPIFFEID=spiffe://cloud-agents.helm/worker \
  --set-string images.controlPlane.repository="$(image_repository "$control_plane_image")" \
  --set-string images.controlPlane.tag="$image_tag" --set-string images.controlPlane.pullPolicy=Never \
  --set-string images.worker.repository="$(image_repository "$worker_image")" \
  --set-string images.worker.tag="$image_tag" --set-string images.worker.pullPolicy=Never \
  --set-string images.migrate.repository="$(image_repository "$migrate_image")" \
  --set-string images.migrate.tag="$image_tag" --set-string images.migrate.pullPolicy=Never \
  --set-string images.accessGateway.repository="$(image_repository "$gateway_image")" \
  --set-string images.accessGateway.tag="$image_tag" --set-string images.accessGateway.pullPolicy=Never \
  --set-string images.adminWeb.repository="$(image_repository "$admin_web_image")" \
  --set-string images.adminWeb.tag="$image_tag" --set-string images.adminWeb.pullPolicy=Never >/dev/null

for component in control-plane worker access-gateway admin-web; do
  kubectl --context "$context" -n "$namespace" rollout status \
    deployment/$release_name-cloud-agents-$component --timeout=180s >/dev/null
done
kubectl --context "$context" -n "$namespace" get deployment \
  -l app.kubernetes.io/instance="$release_name" -o json | node -e '
const fs = require("node:fs");
const value = JSON.parse(fs.readFileSync(0, "utf8"));
const deployments = Object.fromEntries(value.items.map((item) => [item.metadata.labels["app.kubernetes.io/component"], item]));
for (const component of ["control-plane", "worker", "access-gateway", "admin-web"]) {
  const pod = deployments[component]?.spec.template.spec;
  if (!pod || pod.automountServiceAccountToken !== false) process.exit(1);
  const container = pod.containers[0];
  if (container.securityContext?.readOnlyRootFilesystem !== true || container.securityContext?.allowPrivilegeEscalation !== false) process.exit(1);
  if (!container.securityContext?.capabilities?.drop?.includes("ALL")) process.exit(1);
  if (JSON.stringify(pod).includes("/var/run/docker.sock")) process.exit(1);
}
const admin = deployments["admin-web"].spec.template.spec;
if (admin.securityContext.runAsUser !== 1000 || JSON.stringify(admin).includes("provider-credentials") || JSON.stringify(admin).includes("target-credentials")) process.exit(1);
const gateway = deployments["access-gateway"].spec.template.spec;
if (gateway.securityContext.runAsUser !== 65532 || gateway.initContainers[0].args[0] !== "--mode=0400") process.exit(1);
'

kubectl --context "$context" -n "$namespace" port-forward \
  service/$release_name-cloud-agents-admin-web "$admin_port:4174" >"$smoke_directory/admin-forward.log" 2>&1 &
admin_forward_pid=$!
kubectl --context "$context" -n "$namespace" port-forward \
  service/$release_name-cloud-agents-control-plane "$control_plane_port:8080" >"$smoke_directory/control-plane-forward.log" 2>&1 &
control_plane_forward_pid=$!
kubectl --context "$context" -n "$namespace" port-forward \
  service/$release_name-cloud-agents-access-gateway "$gateway_port:8090" "$gateway_ssh_port:2222" >"$smoke_directory/gateway-forward.log" 2>&1 &
gateway_forward_pid=$!
attempt=0
until curl --silent --show-error --fail "http://127.0.0.1:$admin_port/healthz" >/dev/null 2>&1 \
  && curl --silent --show-error --fail --cacert "$smoke_directory/ca.crt" "https://127.0.0.1:$control_plane_port/readyz" >/dev/null 2>&1 \
  && curl --silent --show-error --fail --cacert "$smoke_directory/ca.crt" "https://127.0.0.1:$gateway_port/healthz" >/dev/null 2>&1; do
  attempt=$((attempt + 1))
  if [ "$attempt" -ge 60 ]; then
    echo "Helm service port-forwards did not become ready" >&2
    exit 1
  fi
  sleep 1
done

project_output=$("$cli" --endpoint "https://127.0.0.1:$control_plane_port" \
  --ca-file "$smoke_directory/ca.crt" --token-file "$smoke_directory/admin-token" \
  --tenant tenant-helm-smoke --request-id helm-smoke-project-create \
  --idempotency-key helm-smoke-project-create project create --name helm-smoke-project \
  --display-name 'Helm Smoke Project' --organization-id organization-helm-smoke)
project_id=$(printf '%s' "$project_output" | node -e 'const fs=require("node:fs");const value=JSON.parse(fs.readFileSync(0,"utf8"));process.stdout.write(value.metadata.uid)')
case "$project_id" in project-*) ;; *) echo "Helm project id is invalid" >&2; exit 1 ;; esac
curl --silent --show-error --fail-with-body --request PUT \
  --header "Authorization: Bearer $(sed -n '1p' "$smoke_directory/admin-token")" \
  --header 'Content-Type: application/json' --header 'X-Request-ID: helm-smoke-quota-create' \
  --header 'Idempotency-Key: helm-smoke-quota-create' \
  --data '{"expectedResourceVersion":"0","maxConcurrentLeases":1,"maxCpuMillis":1000,"maxMemoryBytes":536870912,"maxLeaseTtlSeconds":3600}' \
  "http://127.0.0.1:$admin_port/v1/admin/tenants/tenant-helm-smoke/projects/$project_id/lease-quota" >/dev/null
user_status=$(curl --silent --output /dev/null --write-out '%{http_code}' \
  --header "Authorization: Bearer $(sed -n '1p' "$smoke_directory/user-token")" \
  --header 'X-Request-ID: helm-smoke-user-admin-denied' \
  "http://127.0.0.1:$admin_port/v1/admin/tenants/tenant-helm-smoke/projects/$project_id/deployment-targets?pageSize=1")
test "$user_status" = 403 || {
  echo "ordinary User token crossed the Helm Admin API proxy" >&2
  exit 1
}

kill "$admin_forward_pid" "$gateway_forward_pid" >/dev/null 2>&1 || true
wait "$admin_forward_pid" "$gateway_forward_pid" 2>/dev/null || true
admin_forward_pid=
gateway_forward_pid=
kubectl --context "$context" -n "$namespace" rollout restart \
  deployment/$release_name-cloud-agents-access-gateway \
  deployment/$release_name-cloud-agents-admin-web >/dev/null
kubectl --context "$context" -n "$namespace" rollout status \
  deployment/$release_name-cloud-agents-access-gateway --timeout=180s >/dev/null
kubectl --context "$context" -n "$namespace" rollout status \
  deployment/$release_name-cloud-agents-admin-web --timeout=180s >/dev/null
kubectl --context "$context" -n "$namespace" port-forward \
  service/$release_name-cloud-agents-admin-web "$admin_port:4174" >"$smoke_directory/admin-forward.log" 2>&1 &
admin_forward_pid=$!
kubectl --context "$context" -n "$namespace" port-forward \
  service/$release_name-cloud-agents-access-gateway "$gateway_port:8090" "$gateway_ssh_port:2222" >"$smoke_directory/gateway-forward.log" 2>&1 &
gateway_forward_pid=$!
attempt=0
until curl --silent --show-error --fail "http://127.0.0.1:$admin_port/healthz" >/dev/null 2>&1 \
  && curl --silent --show-error --fail --cacert "$smoke_directory/ca.crt" "https://127.0.0.1:$gateway_port/healthz" >/dev/null 2>&1; do
  attempt=$((attempt + 1))
  if [ "$attempt" -ge 30 ]; then
    echo "Helm service port-forwards did not recover after restart" >&2
    exit 1
  fi
  sleep 1
done
node "$smoke_directory/scripts/test-platform-compose-admin-web.mjs" \
  "http://127.0.0.1:$admin_port" "$smoke_directory/admin-token" "$smoke_directory/user-token" \
  tenant-helm-smoke "$project_id"
ssh-keyscan -p "$gateway_ssh_port" 127.0.0.1 2>/dev/null >"$smoke_directory/scanned-host-key.pub"
expected_fingerprint=$(ssh-keygen -lf "$smoke_directory/gateway-ssh-host-key.pub" | awk '{print $2}')
actual_fingerprint=$(ssh-keygen -lf "$smoke_directory/scanned-host-key.pub" | awk 'NR == 1 {print $2}')
test "$actual_fingerprint" = "$expected_fingerprint" || {
  echo "Helm Access Gateway served an unexpected SSH host key" >&2
  exit 1
}
helm --kube-context "$context" -n "$namespace" status "$release_name" -o json | node -e '
const fs = require("node:fs");
const value = JSON.parse(fs.readFileSync(0, "utf8"));
if (value.info.status !== "deployed") process.exit(1);
'
migration_summary=$(kubectl --context "$context" -n "$namespace" exec "$postgres_pod" -- \
  psql -U cloud_agents_install_admin -d cloud_agents -X -A -t -v ON_ERROR_STOP=1 \
  -c "SELECT count(*) || ':' || min(migration_id) || '-' || max(migration_id) FROM cloud_agents.schema_migrations")
test "$migration_summary" = '84:000001-000084' || {
  echo "Helm migration ledger is not at product schema head 000084" >&2
  exit 1
}
verified=true
