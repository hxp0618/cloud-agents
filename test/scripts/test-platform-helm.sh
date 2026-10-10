#!/bin/sh

set -eu

if [ "$#" -lt 1 ] || [ "$#" -gt 2 ] || [ ! -d "$1" ] || { [ "$#" -eq 2 ] && [ ! -d "$2" ]; }; then
  echo "usage: CLOUD_AGENTS_HELM_CONTEXT=<context> test-platform-helm.sh PLATFORM_RELEASE_DIRECTORY [N_MINUS_1_RELEASE_DIRECTORY]" >&2
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
previous_candidate_directory=
[ "$#" -eq 1 ] || previous_candidate_directory=$(CDPATH= cd -- "$2" && pwd)
release_version() {
  node -e 'const fs=require("node:fs");const value=JSON.parse(fs.readFileSync(process.argv[1],"utf8"));if(value.kind!=="cloud-agents-platform-release"||typeof value.version!=="string")process.exit(1);process.stdout.write(value.version)' "$1/platform-release-manifest.json"
}
candidate_version=$(release_version "$candidate_directory")
previous_version=
[ -z "$previous_candidate_directory" ] || previous_version=$(release_version "$previous_candidate_directory")
if [ -n "$previous_version" ] && [ "$previous_version" = "$candidate_version" ]; then
  echo "N and N-1 platform release versions must differ" >&2
  exit 2
fi
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
candidate_deployment_archive=$1
set -- "$candidate_directory"/cloud-agents-migrations-*.tar
if [ "$#" -ne 1 ] || [ ! -f "$1" ]; then
  echo "platform release must contain exactly one migration package" >&2
  exit 1
fi
candidate_migration_archive=$1
candidate_schema_head=$(tar -tf "$candidate_migration_archive" | sed -n 's#^services/control-plane/migrations/product/\([0-9][0-9]*\)/manifest.json$#\1#p')
case "$candidate_schema_head" in
  ??????) ;;
  *) echo "platform release migration package has no unique six-digit schema head" >&2; exit 1 ;;
esac
case "$candidate_schema_head" in
  *[!0-9]*) echo "platform release migration package has an invalid schema head" >&2; exit 1 ;;
esac
candidate_migration_count=$(printf '%s\n' "$candidate_schema_head" | sed 's/^0*//')
[ -n "$candidate_migration_count" ] && [ "$candidate_migration_count" -gt 0 ] || {
  echo "platform release migration package has an invalid schema head" >&2
  exit 1
}
previous_cli=
previous_deployment_archive=
if [ -n "$previous_candidate_directory" ]; then
  previous_cli=$previous_candidate_directory/cloud-agentsctl-$cli_target
  test -x "$previous_cli" || {
    echo "N-1 platform release is missing executable cloud-agentsctl-$cli_target" >&2
    exit 1
  }
  set -- "$previous_candidate_directory"/cloud-agents-deployment-*.tar
  if [ "$#" -ne 1 ] || [ ! -f "$1" ]; then
    echo "N-1 platform release must contain exactly one deployment package" >&2
    exit 1
  fi
  previous_deployment_archive=$1
fi

smoke_directory=$(mktemp -d "${TMPDIR:-/tmp}/cloud-agents-helm-smoke.XXXXXX")
namespace=cloud-agents-helm-smoke-$$
release_name=cloud-agents
image_tag=smoke-$$-n
previous_image_tag=
[ -z "$previous_candidate_directory" ] || previous_image_tag=smoke-$$-n-1
control_plane_image=cloud-agents/helm-smoke-control-plane:$image_tag
identity_image=cloud-agents/helm-smoke-identity:$image_tag
worker_image=cloud-agents/helm-smoke-worker:$image_tag
migrate_image=cloud-agents/helm-smoke-migrate:$image_tag
gateway_image=cloud-agents/helm-smoke-access-gateway:$image_tag
admin_web_image=cloud-agents/helm-smoke-admin-web:$image_tag
user_web_image=cloud-agents/helm-smoke-user-web:$image_tag
previous_control_plane_image=
previous_worker_image=
previous_migrate_image=
previous_gateway_image=
previous_admin_web_image=
previous_user_web_image=
if [ -n "$previous_image_tag" ]; then
  previous_control_plane_image=cloud-agents/helm-smoke-control-plane:$previous_image_tag
  previous_worker_image=cloud-agents/helm-smoke-worker:$previous_image_tag
  previous_migrate_image=cloud-agents/helm-smoke-migrate:$previous_image_tag
  previous_gateway_image=cloud-agents/helm-smoke-access-gateway:$previous_image_tag
  previous_admin_web_image=cloud-agents/helm-smoke-admin-web:$previous_image_tag
  previous_user_web_image=cloud-agents/helm-smoke-user-web:$previous_image_tag
fi
customer_node_image=debian@sha256:88200866dfff7ea7f5cbcb6ec7c8a701889efe6fe859fe64d6990e4b07ea4171
customer_node_container=cloud-agents-helm-smoke-node-$$
admin_port=$((20000 + ($$ % 1000) * 10 + 1))
control_plane_port=$((admin_port + 1))
gateway_port=$((admin_port + 2))
gateway_ssh_port=$((admin_port + 3))
worker_port=$((admin_port + 4))
user_port=$((admin_port + 5))
admin_forward_pid=
user_forward_pid=
control_plane_forward_pid=
gateway_forward_pid=
worker_forward_pid=
automation_refresh_pid=
verified=false
migration_summary=
upgrade_summary=not-requested
identity_summary=not-tested
service_identity_summary=not-tested
no_agent_summary=not-tested

cleanup() {
  status=$?
  trap - 0 HUP INT TERM
  set +e
  for pid in "$automation_refresh_pid" "$user_forward_pid" "$admin_forward_pid" "$control_plane_forward_pid" "$gateway_forward_pid" "$worker_forward_pid"; do
    if [ -n "$pid" ]; then
      kill "$pid" >/dev/null 2>&1
      wait "$pid" 2>/dev/null
    fi
  done
  node_owner=$(docker inspect --format '{{ index .Config.Labels "cloud-agents.dev/test-run" }}' "$customer_node_container" 2>/dev/null)
  if [ "$node_owner" = "$namespace" ]; then
    docker rm -f "$customer_node_container" >/dev/null 2>&1 || status=1
  elif [ -n "$node_owner" ]; then
    echo "refusing to remove customer node without the exact test ownership label" >&2
    status=1
  fi
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
  for image in "$control_plane_image" "$identity_image" "$worker_image" "$migrate_image" "$gateway_image" "$admin_web_image" "$user_web_image" \
    "$previous_control_plane_image" "$previous_worker_image" "$previous_migrate_image" \
    "$previous_gateway_image" "$previous_admin_web_image" "$previous_user_web_image"; do
    [ -n "$image" ] || continue
    docker image inspect "$image" >/dev/null 2>&1 || continue
    image_remove_attempt=0
    until docker image rm "$image" >/dev/null 2>&1; do
      image_remove_attempt=$((image_remove_attempt + 1))
      if [ "$image_remove_attempt" -ge 30 ]; then
        echo "test-owned image remained after cleanup: $image" >&2
        status=1
        break
      fi
      sleep 1
    done
  done
  if [ -n "$(docker ps -aq --filter "label=cloud-agents.dev/test-run=$namespace")" ]; then
    echo "test-owned customer node remains after cleanup" >&2
    status=1
  fi
  case "$smoke_directory" in
    "${TMPDIR:-/tmp}"/cloud-agents-helm-smoke.*) rm -rf -- "$smoke_directory" ;;
    *) echo "refusing to remove unexpected Helm smoke directory" >&2; status=1 ;;
  esac
  if [ "$status" -eq 0 ] && [ "$verified" = true ]; then
    echo "Helm User/Admin Web/RemoteWorker smoke passed (context=$context, schema=$migration_summary, user-web=same-origin, user-admin=403, customer-node=outbound, identity=$identity_summary, service-identity=$service_identity_summary, gateway=TLS+SSH, upgrade=$upgrade_summary, no-agent=$no_agent_summary, restart=passed, cleanup=zero)"
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

current_deployment=$smoke_directory/current
mkdir "$current_deployment"
tar -xf "$candidate_deployment_archive" -C "$current_deployment"
chart=$current_deployment/deploy/helm/cloud-agents
build_release_images() (
  release_directory=$1
  deployment_directory=$2
  tag=$3
  for component in control-plane identity worker migrate access-gateway; do
    if [ "$component" = identity ] && [ ! -f "$deployment_directory/deploy/docker/identity.Dockerfile" ]; then
      continue
    fi
    docker build --quiet --platform "$image_platform" \
      --tag "cloud-agents/helm-smoke-$component:$tag" \
      --file "$deployment_directory/deploy/docker/$component.Dockerfile" "$release_directory" >/dev/null
  done
  docker build --quiet --platform "$image_platform" \
    --tag "cloud-agents/helm-smoke-admin-web:$tag" \
    --file "$deployment_directory/deploy/docker/admin-web.Dockerfile" "$deployment_directory/deploy" >/dev/null
  if [ -f "$deployment_directory/deploy/docker/user-web.Dockerfile" ]; then
    docker build --quiet --platform "$image_platform" \
      --tag "cloud-agents/helm-smoke-user-web:$tag" \
      --file "$deployment_directory/deploy/docker/user-web.Dockerfile" "$deployment_directory/deploy" >/dev/null
  fi
)
load_kind_images() {
  tag=$1
  case "$context" in
    kind-*)
      command -v kind >/dev/null 2>&1 || {
        echo "platform Helm smoke requires kind for context $context" >&2
        exit 2
      }
      cluster=${context#kind-}
      set -- \
        "cloud-agents/helm-smoke-control-plane:$tag" \
        "cloud-agents/helm-smoke-worker:$tag" \
        "cloud-agents/helm-smoke-migrate:$tag" \
        "cloud-agents/helm-smoke-access-gateway:$tag" \
        "cloud-agents/helm-smoke-admin-web:$tag" postgres:17.6-bookworm
      if docker image inspect "cloud-agents/helm-smoke-identity:$tag" >/dev/null 2>&1; then
        set -- "$@" "cloud-agents/helm-smoke-identity:$tag"
      fi
      if docker image inspect "cloud-agents/helm-smoke-user-web:$tag" >/dev/null 2>&1; then
        set -- "$@" "cloud-agents/helm-smoke-user-web:$tag"
      fi
      kind load docker-image --name "$cluster" "$@" >/dev/null
      ;;
  esac
}
build_release_images "$candidate_directory" "$current_deployment" "$image_tag"
load_kind_images "$image_tag"
previous_chart=
if [ -n "$previous_candidate_directory" ]; then
  previous_deployment=$smoke_directory/previous
  mkdir "$previous_deployment"
  tar -xf "$previous_deployment_archive" -C "$previous_deployment"
  previous_chart=$previous_deployment/deploy/helm/cloud-agents
  build_release_images "$previous_candidate_directory" "$previous_deployment" "$previous_image_tag"
  load_kind_images "$previous_image_tag"
fi

openssl req -x509 -newkey rsa:2048 -nodes -sha256 -days 1 \
  -subj /CN=cloud-agents-helm-smoke-ca \
  -keyout "$smoke_directory/ca.key" -out "$smoke_directory/ca.crt" \
  -addext basicConstraints=critical,CA:TRUE -addext keyUsage=critical,keyCertSign,cRLSign >/dev/null 2>&1
issue_certificate() {
  name=$1
  subject=$2
  alternative_names=$3
  extended_usage=$4
  certificate_authority=${5:-ca}
  openssl req -newkey rsa:2048 -nodes -sha256 -subj "/CN=$subject" \
    -keyout "$smoke_directory/$name.key" -out "$smoke_directory/$name.csr" \
    -addext "subjectAltName=$alternative_names" -addext "extendedKeyUsage=$extended_usage" \
    -addext keyUsage=digitalSignature >/dev/null 2>&1
  openssl x509 -req -sha256 -days 1 -in "$smoke_directory/$name.csr" \
    -CA "$smoke_directory/$certificate_authority.crt" \
    -CAkey "$smoke_directory/$certificate_authority.key" -CAcreateserial \
    -copy_extensions copy -out "$smoke_directory/$name.crt" >/dev/null 2>&1
}
service_prefix=$release_name-cloud-agents
issue_certificate control-plane "$service_prefix-control-plane" \
  "DNS:$service_prefix-control-plane,DNS:$service_prefix-control-plane.$namespace.svc,DNS:control-plane.helm-smoke.localhost,DNS:host.docker.internal,IP:127.0.0.1" serverAuth
issue_certificate identity "$service_prefix-identity" \
  "DNS:$service_prefix-identity,DNS:$service_prefix-identity.$namespace.svc,DNS:identity.helm-smoke.localhost" serverAuth
issue_certificate admin-web admin.helm-smoke.localhost "DNS:admin.helm-smoke.localhost" serverAuth
issue_certificate user-web user.helm-smoke.localhost "DNS:user.helm-smoke.localhost" serverAuth
issue_certificate worker "$service_prefix-worker" \
  "DNS:$service_prefix-worker,DNS:$service_prefix-worker.$namespace.svc,URI:spiffe://cloud-agents.helm/worker" serverAuth
issue_certificate worker-client control-plane \
  "URI:spiffe://cloud-agents.helm/control-plane" clientAuth
issue_certificate access-gateway "$service_prefix-access-gateway" \
  "DNS:$service_prefix-access-gateway,DNS:$service_prefix-access-gateway.$namespace.svc,IP:127.0.0.1" serverAuth
openssl req -x509 -newkey rsa:2048 -nodes -sha256 -days 1 \
  -subj /CN=cloud-agents-helm-smoke-next-ca \
  -keyout "$smoke_directory/service-next-ca.key" -out "$smoke_directory/service-next-ca.crt" \
  -addext basicConstraints=critical,CA:TRUE -addext keyUsage=critical,keyCertSign,cRLSign >/dev/null 2>&1
openssl x509 -in "$smoke_directory/service-next-ca.crt" \
  -out "$smoke_directory/service-ca-overlap.crt"
openssl x509 -in "$smoke_directory/ca.crt" >>"$smoke_directory/service-ca-overlap.crt"
issue_certificate control-plane-next "$service_prefix-control-plane" \
  "DNS:$service_prefix-control-plane,DNS:$service_prefix-control-plane.$namespace.svc,DNS:host.docker.internal,IP:127.0.0.1" serverAuth service-next-ca
issue_certificate worker-next "$service_prefix-worker" \
  "DNS:$service_prefix-worker,DNS:$service_prefix-worker.$namespace.svc,URI:spiffe://cloud-agents.helm/worker" serverAuth service-next-ca
issue_certificate worker-client-next control-plane \
  "URI:spiffe://cloud-agents.helm/control-plane" clientAuth service-next-ca
issue_certificate access-gateway-next "$service_prefix-access-gateway" \
  "DNS:$service_prefix-access-gateway,DNS:$service_prefix-access-gateway.$namespace.svc,IP:127.0.0.1" serverAuth service-next-ca
service_ca=$smoke_directory/ca.crt
cp "$service_ca" "$smoke_directory/active-control-plane-ca.crt"
chmod 0600 "$smoke_directory/active-control-plane-ca.crt"
admin_web_spki=$(openssl x509 -in "$smoke_directory/admin-web.crt" -pubkey -noout | \
  openssl pkey -pubin -outform DER | openssl dgst -sha256 -binary | openssl base64 -A)
user_web_spki=$(openssl x509 -in "$smoke_directory/user-web.crt" -pubkey -noout | \
  openssl pkey -pubin -outform DER | openssl dgst -sha256 -binary | openssl base64 -A)
browser_tls_spki="$admin_web_spki,$user_web_spki"
worker_client_certificate=$smoke_directory/worker-client.crt
worker_client_key=$smoke_directory/worker-client.key
ssh-keygen -q -t ed25519 -N "" -f "$smoke_directory/gateway-ssh-host-key"
openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:P-256 -nodes -sha256 -days 1 \
  -subj /CN=cloud-agents-helm-remote-worker-ca \
  -keyout "$smoke_directory/remote-worker-ca.key" -out "$smoke_directory/remote-worker-ca.crt" \
  -addext basicConstraints=critical,CA:TRUE -addext keyUsage=critical,keyCertSign,cRLSign >/dev/null 2>&1
openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:P-256 -nodes -sha256 -days 1 \
  -subj /CN=cloud-agents-helm-remote-worker-next-ca \
  -keyout "$smoke_directory/remote-worker-next-ca.key" -out "$smoke_directory/remote-worker-next-ca.crt" \
  -addext basicConstraints=critical,CA:TRUE -addext keyUsage=critical,keyCertSign,cRLSign >/dev/null 2>&1
openssl x509 -in "$smoke_directory/remote-worker-next-ca.crt" \
  -out "$smoke_directory/remote-worker-ca-overlap.crt"
openssl x509 -in "$smoke_directory/remote-worker-ca.crt" \
  >>"$smoke_directory/remote-worker-ca-overlap.crt"

CLOUD_AGENTS_HELM_SMOKE_STATE=$smoke_directory \
CLOUD_AGENTS_HELM_ADMIN_PORT=$admin_port \
CLOUD_AGENTS_HELM_USER_PORT=$user_port node <<'NODE'
const { createSign, generateKeyPairSync, randomBytes } = require("node:crypto");
const { writeFileSync } = require("node:fs");
const state = process.env.CLOUD_AGENTS_HELM_SMOKE_STATE;
const issuer = "https://identity.helm-smoke.localhost";
const audience = "https://api.helm-smoke.test";
const adminAudience = `https://admin.helm-smoke.localhost:${process.env.CLOUD_AGENTS_HELM_ADMIN_PORT ?? ""}`;
const userAudience = `https://user.helm-smoke.localhost:${process.env.CLOUD_AGENTS_HELM_USER_PORT ?? ""}`;
if (!/^https:\/\/admin\.helm-smoke\.localhost:\d+$/u.test(adminAudience) ||
    !/^https:\/\/user\.helm-smoke\.localhost:\d+$/u.test(userAudience)) throw new Error("Helm Web origins are invalid");
const kid = "helm-smoke-key";
const now = Math.floor(Date.now() / 1000);
const { privateKey, publicKey } = generateKeyPairSync("rsa", { modulusLength: 2048 });
const exported = publicKey.export({ format: "jwk" });
const jwk = { alg: "RS256", e: exported.e, key_ops: ["verify"], kid, kty: "RSA", n: exported.n, use: "sig" };
writeFileSync(`${state}/auth.json`, `${JSON.stringify({
  issuer, audience, adminAudience, generation: 1, securityEpoch: 1, notBefore: now - 60, expiresAt: now + 3600,
  keys: [{ jwk, enabled: true, notBefore: now - 60, notAfter: now + 3600 }],
})}\n`);
const base = {
  iss: issuer, sub: "user-initial-admin", exp: now + 1800, iat: now - 10,
  client_id: "helm-smoke-client", jti: "",
  "https://schemas.cloud-agents.dev/claims/security-epoch": 1,
  "https://schemas.cloud-agents.dev/claims/subject-kind": "user",
  "https://schemas.cloud-agents.dev/claims/tenant-id": "tenant-helm-smoke",
  "https://schemas.cloud-agents.dev/claims/token-profile": "cloud-agents-access-token/v1",
};
const encode = (value) => Buffer.from(JSON.stringify(value)).toString("base64url");
const issue = (jti, tokenAudience, scopes) => {
  const claims = { ...base, aud: tokenAudience, jti, scope: [...scopes].sort().join(" ") };
  const input = `${encode({ alg: "RS256", kid, typ: "at+jwt" })}.${encode(claims)}`;
  return `${input}.${createSign("RSA-SHA256").update(input).end().sign(privateKey).toString("base64url")}`;
};
const adminScopes = [
  "audit.list", "environments.create", "environments.delete", "environments.get", "environment-profiles.list",
  "leases.act", "leases.get", "leases.list", "organizations.list", "profiles.act",
  "mcp-servers.create", "mcp-servers.get", "mcp-servers.list", "mcp-servers.delete", "skill-bundles.create", "skill-bundles.get", "skill-bundles.list", "skill-bundles.delete",
  "operations.list", "profiles.create", "profiles.get", "profiles.list", "projects.act", "projects.create",
  "network-policies.get", "network-policies.list", "network-policies.update", "projects.get", "quotas.get", "quotas.update",
  "releases.create", "releases.list", "sandboxes.act", "sandboxes.get", "sandboxes.list",
  "remote-worker-enrollments.act", "remote-worker-enrollments.create", "remote-worker-enrollments.get", "remote-worker-enrollments.list",
  "storage-policies.get", "storage-policies.list", "storage-policies.update", "targets.act", "targets.create",
  "targets.get", "targets.list", "workers.list", "snapshots.act", "snapshots.create", "snapshots.delete",
  "snapshots.get", "snapshots.list",
];
const userScopes = [
  "environment-quotas.get", "environments.create", "environments.delete", "environments.get", "environment-profiles.list",
  "organizations.list", "projects.act", "projects.create", "projects.get", "projects.list", "sandboxes.update", "tenants.get",
];
writeFileSync(`${state}/admin-token`, `${issue("helm-smoke-admin", adminAudience, adminScopes)}\n`);
writeFileSync(`${state}/admin-denied-token`, `${issue("helm-smoke-admin-denied", adminAudience, userScopes)}\n`);
writeFileSync(`${state}/user-token`, `${issue("helm-smoke-user", audience, userScopes)}\n`);
writeFileSync(`${state}/bootstrap-token`, `${issue("helm-smoke-bootstrap", audience, [
  "projects.act", "remote-worker-bootstrap.act",
])}\n`);
writeFileSync(`${state}/access-grant.key`, randomBytes(32));
writeFileSync(`${state}/admission-token`, randomBytes(24).toString("hex"));
writeFileSync(`${state}/database-password`, randomBytes(24).toString("hex"));
writeFileSync(`${state}/tenant-helm-smoke.unavailable-provider.json`, '{"payload":{}}\n');
const identityPassword = randomBytes(24).toString("base64url");
for (const [name, email] of [
  ["admin-account.json", "admin@example.com"],
  ["admin-denied-account.json", "helm-member@identity.test"],
  ["user-account.json", "helm-member@identity.test"],
]) {
  writeFileSync(`${state}/${name}`, `${JSON.stringify({ email, password: identityPassword })}\n`, { mode: 0o600 });
}
writeFileSync(`${state}/initial-password`, identityPassword, { mode: 0o400 });
writeFileSync(
  `${state}/identity-signing.key`,
  generateKeyPairSync("rsa", { modulusLength: 2048 }).privateKey.export({ format: "pem", type: "pkcs8" }),
  { mode: 0o600 },
);
writeFileSync(`${state}/identity-csrf.key`, randomBytes(32), { mode: 0o600 });
writeFileSync(`${state}/identity-provider-flow.key`, randomBytes(32), { mode: 0o600 });
for (const name of ["identity-setup.proof", "admin-web.proof", "user-web.proof", "control-plane.proof", "identity-control-plane.proof"]) {
  writeFileSync(`${state}/${name}`, randomBytes(32).toString("base64url"), { mode: 0o600 });
}
NODE
identity_key_not_before=$(node -e 'process.stdout.write(new Date(Date.now() - 60_000).toISOString().replace(/\.000Z$/u, "Z"))')
identity_key_not_after=$(node -e 'process.stdout.write(new Date(Date.now() + 365 * 24 * 60 * 60_000).toISOString().replace(/\.000Z$/u, "Z"))')
docker run --rm --platform "$image_platform" --user 0:0 \
  --volume "$smoke_directory:/state" "$identity_image" hash-password \
  --password-file /state/initial-password --output-file /state/initial-password.hash
chmod 0600 "$smoke_directory"/admin-token "$smoke_directory"/admin-denied-token "$smoke_directory"/user-token "$smoke_directory"/bootstrap-token \
  "$smoke_directory"/access-grant.key "$smoke_directory"/admission-token \
  "$smoke_directory"/database-password "$smoke_directory"/gateway-ssh-host-key \
  "$smoke_directory"/admin-account.json "$smoke_directory"/admin-denied-account.json "$smoke_directory"/user-account.json \
  "$smoke_directory"/identity-signing.key "$smoke_directory"/identity-csrf.key "$smoke_directory"/identity-provider-flow.key \
  "$smoke_directory"/identity-setup.proof "$smoke_directory"/admin-web.proof "$smoke_directory"/user-web.proof \
  "$smoke_directory"/control-plane.proof "$smoke_directory"/identity-control-plane.proof \
  "$smoke_directory"/initial-password.hash
cp "$smoke_directory/user-token" "$smoke_directory/legacy-user-token"
chmod 0600 "$smoke_directory/legacy-user-token"

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
  "$current_deployment/deploy/bootstrap/roles.sql" "$postgres_pod:/tmp/roles.sql"
kubectl --context "$context" -n "$namespace" cp \
  "$current_deployment/deploy/bootstrap/roles_identity_service.sql" "$postgres_pod:/tmp/roles-identity-service.sql"
kubectl --context "$context" -n "$namespace" cp \
  "$current_deployment/deploy/compose/provision.sql" "$postgres_pod:/tmp/provision.sql"
kubectl --context "$context" -n "$namespace" cp \
  "$current_deployment/deploy/bootstrap/database.sql" "$postgres_pod:/tmp/database.sql"
kubectl --context "$context" -n "$namespace" exec "$postgres_pod" -- \
  psql -U cloud_agents_install_admin -d cloud_agents --single-transaction -v ON_ERROR_STOP=1 \
  -v cloud_agents_database=cloud_agents -v cloud_agents_database_owner=cloud_agents_database_owner \
  -v cloud_agents_migration_password="$database_password" -v cloud_agents_runtime_password="$database_password" \
  -v cloud_agents_tenant_bootstrap_password="$database_password" \
  -v cloud_agents_identity_bootstrap_password="$database_password" \
  -v cloud_agents_identity_service_password="$database_password" \
  -f /tmp/roles.sql -f /tmp/roles-identity-service.sql -f /tmp/provision.sql -f /tmp/database.sql >/dev/null
kubectl --context "$context" -n "$namespace" exec "$postgres_pod" -- env \
  PGPASSWORD="$database_password" psql -h postgres -U cloud_agents_migration -d cloud_agents \
  -X -A -t -v ON_ERROR_STOP=1 -c 'SELECT current_user' | grep -Fx cloud_agents_migration >/dev/null

kubectl --context "$context" -n "$namespace" create secret generic cloud-agents-database \
  --from-literal=runtime-url="postgresql://cloud_agents_runtime_login:$database_password@postgres:5432/cloud_agents?sslmode=disable" \
  --from-literal=migration-url="postgresql://cloud_agents_migration:$database_password@postgres:5432/cloud_agents?sslmode=disable" \
  --from-literal=tenant-bootstrap-url="postgresql://cloud_agents_tenant_bootstrap:$database_password@postgres:5432/cloud_agents?sslmode=disable" \
  --from-literal=identity-service-url="postgresql://cloud_agents_identity_service_login:$database_password@postgres:5432/cloud_agents?sslmode=disable" \
  --from-literal=identity-bootstrap-url="postgresql://cloud_agents_identity_bootstrap:$database_password@postgres:5432/cloud_agents?sslmode=disable" >/dev/null
kubectl --context "$context" -n "$namespace" create secret generic cloud-agents-auth \
  --from-file=auth.json="$smoke_directory/auth.json" >/dev/null
kubectl --context "$context" -n "$namespace" create secret generic cloud-agents-identity-tls \
  --from-file=tls.crt="$smoke_directory/identity.crt" --from-file=tls.key="$smoke_directory/identity.key" \
  --from-file=ca.crt="$smoke_directory/ca.crt" >/dev/null
kubectl --context "$context" -n "$namespace" create secret generic cloud-agents-identity-signing \
  --from-file=signing.key="$smoke_directory/identity-signing.key" >/dev/null
kubectl --context "$context" -n "$namespace" create secret generic cloud-agents-identity-csrf \
  --from-file=csrf.key="$smoke_directory/identity-csrf.key" >/dev/null
kubectl --context "$context" -n "$namespace" create secret generic cloud-agents-identity-provider-flow \
  --from-file=provider-flow.key="$smoke_directory/identity-provider-flow.key" >/dev/null
kubectl --context "$context" -n "$namespace" create secret generic cloud-agents-identity-service-proofs \
  --from-file=admin-web.proof="$smoke_directory/admin-web.proof" \
  --from-file=user-web.proof="$smoke_directory/user-web.proof" \
  --from-file=control-plane.proof="$smoke_directory/control-plane.proof" \
  --from-file=identity-control-plane.proof="$smoke_directory/identity-control-plane.proof" >/dev/null
kubectl --context "$context" -n "$namespace" create secret generic cloud-agents-identity-bootstrap \
  --from-file=setup.proof="$smoke_directory/identity-setup.proof" \
  --from-file=password.hash="$smoke_directory/initial-password.hash" >/dev/null
kubectl --context "$context" -n "$namespace" create secret generic cloud-agents-admin-web-tls \
  --from-file=tls.crt="$smoke_directory/admin-web.crt" --from-file=tls.key="$smoke_directory/admin-web.key" >/dev/null
kubectl --context "$context" -n "$namespace" create secret generic cloud-agents-user-web-tls \
  --from-file=tls.crt="$smoke_directory/user-web.crt" --from-file=tls.key="$smoke_directory/user-web.key" >/dev/null
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
kubectl --context "$context" -n "$namespace" create secret generic cloud-agents-remote-worker-ca \
  --from-file=ca.crt="$smoke_directory/remote-worker-ca.crt" \
  --from-file=ca.key="$smoke_directory/remote-worker-ca.key" >/dev/null
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
  --from-literal=CLOUD_AGENTS_ADMIN_SUBJECT_ISSUER=https://identity.helm-smoke.localhost \
  --from-literal=CLOUD_AGENTS_ADMIN_SUBJECT_VALUE=user-initial-admin \
  --from-literal=CLOUD_AGENTS_ADMIN_MEMBERSHIP_UID=membership-helm-admin \
  --from-literal=CLOUD_AGENTS_ADMIN_MEMBERSHIP_NAME=membership-helm-admin \
  --from-literal=CLOUD_AGENTS_ADMIN_ROLE_BINDING_UID=role-binding-helm-admin \
  --from-literal=CLOUD_AGENTS_ADMIN_ROLE_BINDING_NAME=role-binding-helm-admin \
  --from-literal=CLOUD_AGENTS_TENANT_AUDIT_FACT_UID=audit-helm-tenant \
  --from-literal=CLOUD_AGENTS_MEMBERSHIP_AUDIT_FACT_UID=audit-helm-membership \
  --from-literal=CLOUD_AGENTS_ROLE_BINDING_AUDIT_FACT_UID=audit-helm-role-binding \
  --from-literal=CLOUD_AGENTS_BOOTSTRAP_REASON_CODE=helm-smoke >/dev/null

image_repository() { printf '%s' "${1%:*}"; }
helm_apply() (
  action=$1
  target_chart=$2
  target_tag=$3
  set -- --kube-context "$context" "$action" "$release_name" "$target_chart" --namespace "$namespace" \
    --wait --timeout 5m --set-string runtime.workspace.size=1Gi --set worker.enabled=true \
    --set tenantBootstrap.enabled=true \
    --set-string controlPlane.workerSPIFFEID=spiffe://cloud-agents.helm/worker \
    --set-string images.controlPlane.repository="$(image_repository "$control_plane_image")" \
    --set-string images.controlPlane.tag="$target_tag" --set-string images.controlPlane.pullPolicy=Never \
    --set-string images.worker.repository="$(image_repository "$worker_image")" \
    --set-string images.worker.tag="$target_tag" --set-string images.worker.pullPolicy=Never \
    --set-string images.migrate.repository="$(image_repository "$migrate_image")" \
    --set-string images.migrate.tag="$target_tag" --set-string images.migrate.pullPolicy=Never \
    --set-string images.accessGateway.repository="$(image_repository "$gateway_image")" \
    --set-string images.accessGateway.tag="$target_tag" --set-string images.accessGateway.pullPolicy=Never \
    --set-string images.adminWeb.repository="$(image_repository "$admin_web_image")" \
    --set-string images.adminWeb.tag="$target_tag" --set-string images.adminWeb.pullPolicy=Never
  if [ -f "$target_chart/templates/identity.yaml" ]; then
    set -- "$@" \
      --set-string images.identity.repository="$(image_repository "$identity_image")" \
      --set-string images.identity.tag="$target_tag" --set-string images.identity.pullPolicy=Never \
      --set-string identity.issuer=https://identity.helm-smoke.localhost \
      --set-string identity.adminAudience="https://admin.helm-smoke.localhost:$admin_port" \
      --set-string identity.userAudience="https://user.helm-smoke.localhost:$user_port" \
      --set identityBootstrap.enabled=true \
      --set-string identityBootstrap.keyNotBefore="$identity_key_not_before" \
      --set-string identityBootstrap.keyNotAfter="$identity_key_not_after"
  fi
  if [ -f "$target_chart/templates/user-web.yaml" ]; then
    set -- "$@" \
      --set-string images.userWeb.repository="$(image_repository "$user_web_image")" \
      --set-string images.userWeb.tag="$target_tag" --set-string images.userWeb.pullPolicy=Never
  fi
  helm "$@" --set-string remoteWorker.certificateAuthoritySecretName=cloud-agents-remote-worker-ca \
    --set-string remoteWorker.trustDomain=cloud-agents.helm >/dev/null
)
wait_deployments() {
  for component in identity control-plane worker access-gateway admin-web user-web; do
    if [ "$component" = identity ] && ! kubectl --context "$context" -n "$namespace" get \
      deployment/$release_name-cloud-agents-identity >/dev/null 2>&1; then
      continue
    fi
    if [ "$component" = user-web ] && ! kubectl --context "$context" -n "$namespace" get \
      deployment/$release_name-cloud-agents-user-web >/dev/null 2>&1; then
      continue
    fi
    kubectl --context "$context" -n "$namespace" rollout status \
      deployment/$release_name-cloud-agents-$component --timeout=180s >/dev/null
  done
}
assert_release_images() {
  kubectl --context "$context" -n "$namespace" get deployment \
    -l app.kubernetes.io/instance="$release_name" -o json | node -e '
const fs = require("node:fs");
const value = JSON.parse(fs.readFileSync(0, "utf8"));
const expected = process.argv[1];
for (const deployment of value.items) {
  const component = deployment.metadata.labels["app.kubernetes.io/component"];
  if (deployment.spec.template.spec.containers[0].image !== `cloud-agents/helm-smoke-${component}:${expected}`) process.exit(1);
  if ((deployment.spec.template.spec.initContainers ?? []).some((container) => container.image !== `cloud-agents/helm-smoke-migrate:${expected}`)) process.exit(1);
}
' "$1"
}
wait_for_test_job() {
  job_name=$1
  if ! kubectl --context "$context" -n "$namespace" wait \
    --for=condition=complete "job/$job_name" --timeout=180s >/dev/null; then
    kubectl --context "$context" -n "$namespace" logs "job/$job_name" --all-containers >&2 || true
    echo "Helm smoke job failed: $job_name" >&2
    return 1
  fi
}
initialize_identity_for_upgrade() {
  migration_job=$release_name-identity-upgrade-migrate
  initialize_job=$release_name-identity-upgrade-initialize
  initialize_config=$release_name-identity-upgrade-config
  cat <<EOF | kubectl --context "$context" -n "$namespace" apply -f - >/dev/null
apiVersion: batch/v1
kind: Job
metadata:
  name: $migration_job
  labels:
    cloud-agents.dev/test-run: $namespace
spec:
  backoffLimit: 0
  template:
    metadata:
      labels:
        cloud-agents.dev/test-run: $namespace
    spec:
      automountServiceAccountToken: false
      restartPolicy: Never
      securityContext:
        runAsNonRoot: true
        seccompProfile: { type: RuntimeDefault }
      containers:
        - name: migrate
          image: $migrate_image
          imagePullPolicy: Never
          env:
            - name: CLOUD_AGENTS_PLATFORM_DATABASE_URL
              valueFrom:
                secretKeyRef: { name: cloud-agents-database, key: migration-url }
          securityContext:
            allowPrivilegeEscalation: false
            readOnlyRootFilesystem: true
            capabilities: { drop: [ALL] }
EOF
  wait_for_test_job "$migration_job"
  kubectl --context "$context" -n "$namespace" delete "job/$migration_job" --wait=true >/dev/null

  CLOUD_AGENTS_HELM_IDENTITY_INITIALIZE_CONFIG="$smoke_directory/identity-upgrade-initialize.json" \
  CLOUD_AGENTS_HELM_ADMIN_PORT=$admin_port CLOUD_AGENTS_HELM_USER_PORT=$user_port \
  CLOUD_AGENTS_HELM_KEY_NOT_BEFORE="$identity_key_not_before" \
  CLOUD_AGENTS_HELM_KEY_NOT_AFTER="$identity_key_not_after" node <<'NODE'
const { writeFileSync } = require("node:fs");
const configuration = {
  bootstrapDatabaseUrlFile: "/run/cloud-agents/identity/database/identity-bootstrap-url",
  issuer: "https://identity.helm-smoke.localhost",
  adminAudience: `https://admin.helm-smoke.localhost:${process.env.CLOUD_AGENTS_HELM_ADMIN_PORT}`,
  userAudience: `https://user.helm-smoke.localhost:${process.env.CLOUD_AGENTS_HELM_USER_PORT}`,
  signingKeyId: "identity-signing-1",
  signingPrivateKeyFile: "/run/cloud-agents/identity/signing/signing.key",
  setupProofFile: "/run/cloud-agents/identity/bootstrap/setup.proof",
  userId: "initial-admin",
  email: "admin@example.com",
  displayName: "Initial administrator",
  passwordHashFile: "/run/cloud-agents/identity/bootstrap/password.hash",
  keyNotBefore: process.env.CLOUD_AGENTS_HELM_KEY_NOT_BEFORE,
  keyNotAfter: process.env.CLOUD_AGENTS_HELM_KEY_NOT_AFTER,
};
writeFileSync(process.env.CLOUD_AGENTS_HELM_IDENTITY_INITIALIZE_CONFIG, `${JSON.stringify(configuration)}\n`, { mode: 0o600 });
NODE
  kubectl --context "$context" -n "$namespace" create configmap "$initialize_config" \
    --from-file=initialize.json="$smoke_directory/identity-upgrade-initialize.json" >/dev/null
  cat <<EOF | kubectl --context "$context" -n "$namespace" apply -f - >/dev/null
apiVersion: batch/v1
kind: Job
metadata:
  name: $initialize_job
  labels:
    cloud-agents.dev/test-run: $namespace
spec:
  backoffLimit: 0
  template:
    metadata:
      labels:
        cloud-agents.dev/test-run: $namespace
    spec:
      automountServiceAccountToken: false
      restartPolicy: Never
      securityContext:
        runAsNonRoot: true
        runAsUser: 65532
        runAsGroup: 65532
        fsGroup: 65532
        seccompProfile: { type: RuntimeDefault }
      containers:
        - name: identity-initialize
          image: $identity_image
          imagePullPolicy: Never
          args: ["initialize", "--config", "/run/cloud-agents/identity/config/initialize.json"]
          securityContext:
            allowPrivilegeEscalation: false
            readOnlyRootFilesystem: true
            capabilities: { drop: [ALL] }
          volumeMounts:
            - { name: config, mountPath: /run/cloud-agents/identity/config, readOnly: true }
            - { name: database, mountPath: /run/cloud-agents/identity/database, readOnly: true }
            - { name: signing, mountPath: /run/cloud-agents/identity/signing, readOnly: true }
            - { name: bootstrap, mountPath: /run/cloud-agents/identity/bootstrap, readOnly: true }
      volumes:
        - name: config
          configMap: { name: $initialize_config, defaultMode: 0440 }
        - name: database
          secret: { secretName: cloud-agents-database, defaultMode: 0440 }
        - name: signing
          secret: { secretName: cloud-agents-identity-signing, defaultMode: 0440 }
        - name: bootstrap
          secret: { secretName: cloud-agents-identity-bootstrap, defaultMode: 0440 }
EOF
  wait_for_test_job "$initialize_job"
  kubectl --context "$context" -n "$namespace" delete "job/$initialize_job" --wait=true >/dev/null
  kubectl --context "$context" -n "$namespace" delete "configmap/$initialize_config" >/dev/null
}
install_chart=$chart
install_tag=$image_tag
if [ -n "$previous_chart" ]; then
  install_chart=$previous_chart
  install_tag=$previous_image_tag
  cli=$previous_cli
fi
helm_apply install "$install_chart" "$install_tag"
wait_deployments
assert_release_images "$install_tag"
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
const user = deployments["user-web"]?.spec.template.spec;
if (user && (user.automountServiceAccountToken !== false || user.securityContext.runAsUser !== 1000 || user.containers[0]?.securityContext?.readOnlyRootFilesystem !== true || JSON.stringify(user).includes("provider-credentials") || JSON.stringify(user).includes("target-credentials") || JSON.stringify(user).includes("/var/run/docker.sock"))) process.exit(1);
const gateway = deployments["access-gateway"].spec.template.spec;
if (gateway.securityContext.runAsUser !== 65532 || gateway.initContainers[0].args[0] !== "--mode=0400") process.exit(1);
'

stop_forwards() {
  for pid in "$user_forward_pid" "$admin_forward_pid" "$control_plane_forward_pid" "$gateway_forward_pid" "$worker_forward_pid"; do
    if [ -n "$pid" ]; then
      kill "$pid" >/dev/null 2>&1 || true
      wait "$pid" 2>/dev/null || true
    fi
  done
  admin_forward_pid=
  user_forward_pid=
  control_plane_forward_pid=
  gateway_forward_pid=
  worker_forward_pid=
}
forwards_alive() {
  for pid in "$user_forward_pid" "$admin_forward_pid" "$control_plane_forward_pid" "$gateway_forward_pid"; do
    [ -z "$pid" ] || kill -0 "$pid" 2>/dev/null || return 1
  done
}
print_forward_logs() {
  for log in user-forward admin-forward control-plane-forward gateway-forward; do
    [ ! -f "$smoke_directory/$log.log" ] || cat "$smoke_directory/$log.log" >&2
  done
}
web_health_ready() {
  host=$1
  port=$2
  if kubectl --context "$context" -n "$namespace" get \
    deployment/$release_name-cloud-agents-identity >/dev/null 2>&1; then
    curl --silent --show-error --fail --noproxy '*' --cacert "$smoke_directory/ca.crt" \
      --resolve "$host:$port:127.0.0.1" "https://$host:$port/healthz" >/dev/null 2>&1
  else
    curl --silent --show-error --fail "http://127.0.0.1:$port/healthz" >/dev/null 2>&1
  fi
}
start_forwards() {
  forward_restart=${1:-0}
  if kubectl --context "$context" -n "$namespace" get \
    service/$release_name-cloud-agents-user-web >/dev/null 2>&1; then
    kubectl --context "$context" -n "$namespace" port-forward \
      service/$release_name-cloud-agents-user-web "$user_port:4173" >"$smoke_directory/user-forward.log" 2>&1 &
    user_forward_pid=$!
  else
    user_forward_pid=
  fi
  kubectl --context "$context" -n "$namespace" port-forward \
    service/$release_name-cloud-agents-admin-web "$admin_port:4174" >"$smoke_directory/admin-forward.log" 2>&1 &
  admin_forward_pid=$!
  kubectl --context "$context" -n "$namespace" port-forward --address=0.0.0.0 \
    service/$release_name-cloud-agents-control-plane "$control_plane_port:8080" >"$smoke_directory/control-plane-forward.log" 2>&1 &
  control_plane_forward_pid=$!
  kubectl --context "$context" -n "$namespace" port-forward \
    service/$release_name-cloud-agents-access-gateway "$gateway_port:8090" "$gateway_ssh_port:2222" >"$smoke_directory/gateway-forward.log" 2>&1 &
  gateway_forward_pid=$!
  attempt=0
  until { [ -z "$user_forward_pid" ] || web_health_ready user.helm-smoke.localhost "$user_port"; } \
    && web_health_ready admin.helm-smoke.localhost "$admin_port" \
    && curl --silent --show-error --fail --cacert "$service_ca" "https://127.0.0.1:$control_plane_port/readyz" >/dev/null 2>&1 \
    && curl --silent --show-error --fail --cacert "$service_ca" "https://127.0.0.1:$gateway_port/healthz" >/dev/null 2>&1; do
    if ! forwards_alive; then
      stop_forwards
      forward_restart=$((forward_restart + 1))
      if [ "$forward_restart" -ge 3 ]; then
        echo "Helm service port-forwards exited repeatedly" >&2
        print_forward_logs
        exit 1
      fi
      start_forwards "$forward_restart"
      return
    fi
    attempt=$((attempt + 1))
    if [ "$attempt" -ge 60 ]; then
      echo "Helm service port-forwards did not become ready" >&2
      print_forward_logs
      exit 1
    fi
    sleep 1
  done
}
start_forwards
admin_web_origin="https://admin.helm-smoke.localhost:$admin_port"
user_web_origin="https://user.helm-smoke.localhost:$user_port"
control_plane_origin="https://127.0.0.1:$control_plane_port"
automation_fixture=$current_deployment/test/e2e/identity-automation-fixture.mjs
candidate_cli=$candidate_directory/cloud-agentsctl-$cli_target
admin_cli_profile=$smoke_directory/admin.profile
user_cli_profile=$smoke_directory/user.profile
provision_admin_automation() {
  NODE_EXTRA_CA_CERTS="$smoke_directory/ca.crt" node "$automation_fixture" create \
    "$admin_web_origin" "$smoke_directory/admin-account.json" tenant-helm-smoke \
    helm-admin-automation admin tenant.admin tenant tenant-helm-smoke \
    "$smoke_directory/automation-admin.credential"
  NODE_EXTRA_CA_CERTS="$smoke_directory/ca.crt" node "$automation_fixture" exchange \
    "$admin_web_origin" "$smoke_directory/automation-admin.credential" tenant-helm-smoke \
    "$smoke_directory/automation-admin-tenant"
  cp "$smoke_directory/automation-admin-tenant.token" "$smoke_directory/admin-token"
  cp "$smoke_directory/automation-admin-tenant.curl.conf" "$smoke_directory/admin-curl.conf"
  chmod 0600 "$smoke_directory/admin-token" "$smoke_directory/admin-curl.conf"
  "$candidate_cli" profile configure-service-account --web-endpoint "$admin_web_origin" \
    --control-plane-endpoint "$control_plane_origin" --application admin \
    --credential-file "$smoke_directory/automation-admin.credential" --tenant tenant-helm-smoke \
    --ca-file "$smoke_directory/active-control-plane-ca.crt" --profile "$admin_cli_profile" >/dev/null
}
provision_project_automation() {
  NODE_EXTRA_CA_CERTS="$smoke_directory/ca.crt" node "$automation_fixture" exchange \
    "$admin_web_origin" "$smoke_directory/automation-admin.credential" tenant-helm-smoke \
    "$smoke_directory/automation-admin-project" "$project_id"
  cp "$smoke_directory/automation-admin-project.token" "$smoke_directory/admin-token"
  cp "$smoke_directory/automation-admin-project.token" "$smoke_directory/bootstrap-token"
  cp "$smoke_directory/automation-admin-project.curl.conf" "$smoke_directory/admin-curl.conf"
  NODE_EXTRA_CA_CERTS="$smoke_directory/ca.crt" node "$automation_fixture" create \
    "$admin_web_origin" "$smoke_directory/admin-account.json" tenant-helm-smoke \
    helm-user-automation user project.operator project "$project_id" \
    "$smoke_directory/automation-user.credential"
  NODE_EXTRA_CA_CERTS="$smoke_directory/ca.crt" node "$automation_fixture" exchange \
    "$user_web_origin" "$smoke_directory/automation-user.credential" tenant-helm-smoke \
    "$smoke_directory/automation-user-project" "$project_id"
  cp "$smoke_directory/automation-user-project.token" "$smoke_directory/user-token"
  cp "$smoke_directory/automation-user-project.curl.conf" "$smoke_directory/user-curl.conf"
  NODE_EXTRA_CA_CERTS="$smoke_directory/ca.crt" node "$automation_fixture" create \
    "$admin_web_origin" "$smoke_directory/admin-account.json" tenant-helm-smoke \
    helm-denied-automation admin project.viewer project "$project_id" \
    "$smoke_directory/automation-denied.credential"
  NODE_EXTRA_CA_CERTS="$smoke_directory/ca.crt" node "$automation_fixture" exchange \
    "$admin_web_origin" "$smoke_directory/automation-denied.credential" tenant-helm-smoke \
    "$smoke_directory/automation-denied-project" "$project_id"
  cp "$smoke_directory/automation-denied-project.token" "$smoke_directory/admin-denied-token"
  cp "$smoke_directory/automation-denied-project.curl.conf" "$smoke_directory/admin-denied-curl.conf"
  chmod 0600 "$smoke_directory/admin-token" "$smoke_directory/bootstrap-token" \
    "$smoke_directory/admin-curl.conf" "$smoke_directory/user-token" "$smoke_directory/user-curl.conf" \
    "$smoke_directory/admin-denied-token" "$smoke_directory/admin-denied-curl.conf"
  "$candidate_cli" profile configure-service-account --web-endpoint "$admin_web_origin" \
    --control-plane-endpoint "$control_plane_origin" --application admin \
    --credential-file "$smoke_directory/automation-admin.credential" --tenant tenant-helm-smoke \
    --project "$project_id" --ca-file "$smoke_directory/active-control-plane-ca.crt" --profile "$admin_cli_profile" >/dev/null
  "$candidate_cli" profile configure-service-account --web-endpoint "$user_web_origin" \
    --control-plane-endpoint "$control_plane_origin" --application user \
    --credential-file "$smoke_directory/automation-user.credential" --tenant tenant-helm-smoke \
    --project "$project_id" --ca-file "$smoke_directory/active-control-plane-ca.crt" --profile "$user_cli_profile" >/dev/null
}
replace_private_file() {
  source_file=$1
  destination_file=$2
  temporary_file="$destination_file.next"
  cp "$source_file" "$temporary_file" || return 1
  chmod 0600 "$temporary_file" || return 1
  mv -f "$temporary_file" "$destination_file"
}
refresh_automation_tokens() {
  if ! NODE_EXTRA_CA_CERTS="$smoke_directory/ca.crt" node "$automation_fixture" exchange \
    "$admin_web_origin" "$smoke_directory/automation-admin.credential" tenant-helm-smoke \
    "$smoke_directory/automation-admin-refresh" "$project_id"; then
    return 1
  fi
  if ! NODE_EXTRA_CA_CERTS="$smoke_directory/ca.crt" node "$automation_fixture" exchange \
    "$user_web_origin" "$smoke_directory/automation-user.credential" tenant-helm-smoke \
    "$smoke_directory/automation-user-refresh" "$project_id"; then
    return 1
  fi
  if ! NODE_EXTRA_CA_CERTS="$smoke_directory/ca.crt" node "$automation_fixture" exchange \
    "$admin_web_origin" "$smoke_directory/automation-denied.credential" tenant-helm-smoke \
    "$smoke_directory/automation-denied-refresh" "$project_id"; then
    return 1
  fi
  replace_private_file "$smoke_directory/automation-admin-refresh.token" "$smoke_directory/admin-token" || return 1
  replace_private_file "$smoke_directory/automation-admin-refresh.token" "$smoke_directory/bootstrap-token" || return 1
  replace_private_file "$smoke_directory/automation-admin-refresh.curl.conf" "$smoke_directory/admin-curl.conf" || return 1
  replace_private_file "$smoke_directory/automation-user-refresh.token" "$smoke_directory/user-token" || return 1
  replace_private_file "$smoke_directory/automation-user-refresh.curl.conf" "$smoke_directory/user-curl.conf" || return 1
  replace_private_file "$smoke_directory/automation-denied-refresh.token" "$smoke_directory/admin-denied-token" || return 1
  replace_private_file "$smoke_directory/automation-denied-refresh.curl.conf" "$smoke_directory/admin-denied-curl.conf" || return 1
}
automation_token_refresh_loop() {
  automation_refresh_sleep_pid=
  trap 'if [ -n "$automation_refresh_sleep_pid" ]; then kill "$automation_refresh_sleep_pid" >/dev/null 2>&1 || true; wait "$automation_refresh_sleep_pid" 2>/dev/null || true; fi; exit 0' HUP INT TERM
  automation_refresh_wait() {
    sleep "$1" &
    automation_refresh_sleep_pid=$!
    wait "$automation_refresh_sleep_pid"
    automation_refresh_sleep_pid=
  }
  while automation_refresh_wait 240; do
    until refresh_automation_tokens; do
      echo "automation token refresh failed; retrying" >&2
      automation_refresh_wait 15 || return 0
    done
  done
}
reload_control_plane_remote_worker_ca() {
  secret_name=$1
  certificate=$2
  stop_forwards
  kubectl --context "$context" -n "$namespace" create secret generic "$secret_name" \
    --from-file=ca.crt="$certificate" --from-file=ca.key="$smoke_directory/remote-worker-next-ca.key" \
    >/dev/null
  helm --kube-context "$context" upgrade "$release_name" "$chart" --namespace "$namespace" \
    --wait --timeout 5m --reuse-values \
    --set-string remoteWorker.certificateAuthoritySecretName="$secret_name" >/dev/null
  kubectl --context "$context" -n "$namespace" get \
    deployment/$release_name-cloud-agents-control-plane -o json | node -e '
const fs = require("node:fs");
const value = JSON.parse(fs.readFileSync(0, "utf8"));
const volume = value.spec?.template?.spec?.volumes?.find((item) => item.name === "remote-worker-ca");
if (volume?.secret?.secretName !== process.argv[1]) process.exit(1);
' "$secret_name"
  start_forwards
}

apply_service_identity_phase() {
  phase=$1
  control_plane_prefix=$2
  worker_prefix=$3
  worker_client_prefix=$4
  gateway_prefix=$5
  ca_bundle=$6
  control_plane_secret=cloud-agents-control-plane-tls-$phase
  worker_secret=cloud-agents-worker-tls-$phase
  worker_client_secret=cloud-agents-worker-client-tls-$phase
  gateway_secret=cloud-agents-access-gateway-tls-$phase
  kubectl --context "$context" -n "$namespace" create secret generic "$control_plane_secret" \
    --from-file=tls.crt="$smoke_directory/$control_plane_prefix.crt" \
    --from-file=tls.key="$smoke_directory/$control_plane_prefix.key" \
    --from-file=ca.crt="$ca_bundle" >/dev/null
  kubectl --context "$context" -n "$namespace" create secret generic "$worker_secret" \
    --from-file=tls.crt="$smoke_directory/$worker_prefix.crt" \
    --from-file=tls.key="$smoke_directory/$worker_prefix.key" \
    --from-file=ca.crt="$ca_bundle" >/dev/null
  kubectl --context "$context" -n "$namespace" create secret generic "$worker_client_secret" \
    --from-file=tls.crt="$smoke_directory/$worker_client_prefix.crt" \
    --from-file=tls.key="$smoke_directory/$worker_client_prefix.key" >/dev/null
  kubectl --context "$context" -n "$namespace" create secret generic "$gateway_secret" \
    --from-file=tls.crt="$smoke_directory/$gateway_prefix.crt" \
    --from-file=tls.key="$smoke_directory/$gateway_prefix.key" >/dev/null
  stop_forwards
  helm --kube-context "$context" upgrade "$release_name" "$chart" --namespace "$namespace" \
    --wait --timeout 5m --reuse-values \
    --set-string tls.controlPlaneSecretName="$control_plane_secret" \
    --set-string tls.workerSecretName="$worker_secret" \
    --set-string tls.workerClientSecretName="$worker_client_secret" \
    --set-string tls.accessGatewaySecretName="$gateway_secret" \
    --set-string adminWeb.controlPlaneCASecretName="$control_plane_secret" \
    --set-string userWeb.controlPlaneCASecretName="$control_plane_secret" >/dev/null
  kubectl --context "$context" -n "$namespace" get deployment \
    -l app.kubernetes.io/instance="$release_name" -o json | node -e '
const fs = require("node:fs");
const value = JSON.parse(fs.readFileSync(0, "utf8"));
const deployments = Object.fromEntries(value.items.map((item) => [item.metadata.labels["app.kubernetes.io/component"], item]));
const secret = (component, volume) => deployments[component]?.spec?.template?.spec?.volumes?.find((item) => item.name === volume)?.secret?.secretName;
if (secret("control-plane", "control-plane-tls") !== process.argv[1] || secret("control-plane", "worker-ca") !== process.argv[2] || secret("control-plane", "worker-client") !== process.argv[3] || secret("worker", "tls") !== process.argv[2] || secret("access-gateway", "tls") !== process.argv[4] || secret("admin-web", "control-plane-ca") !== process.argv[1] || secret("user-web", "control-plane-ca") !== process.argv[1]) process.exit(1);
' "$control_plane_secret" "$worker_secret" "$worker_client_secret" "$gateway_secret"
  service_ca=$ca_bundle
  cp "$ca_bundle" "$smoke_directory/active-control-plane-ca.crt.next"
  chmod 0600 "$smoke_directory/active-control-plane-ca.crt.next"
  mv -f "$smoke_directory/active-control-plane-ca.crt.next" "$smoke_directory/active-control-plane-ca.crt"
  worker_client_certificate=$smoke_directory/$worker_client_prefix.crt
  worker_client_key=$smoke_directory/$worker_client_prefix.key
  start_forwards
  verify_project "helm-smoke-service-identity-$phase"
}

install_customer_node_ca() {
  source=$1
  source_name=${source##*/}
  docker run --rm --platform "$image_platform" \
    --label "cloud-agents.dev/test-run=$namespace" \
    --volume "$smoke_directory:/smoke:ro" \
    --volume "$smoke_directory/customer-node:/node-output" \
    "$customer_node_image" sh -eu -c '
      temporary=/node-output/install/control-plane-ca.pem.next
      cp "/smoke/$1" "$temporary"
      chmod 0400 "$temporary"
      mv -f "$temporary" /node-output/install/control-plane-ca.pem
    ' sh "$source_name"
}

run_customer_node_once() {
  customer_node_attempt=0
  until docker run --rm --platform "$image_platform" \
    --label "cloud-agents.dev/test-run=$namespace" \
    --add-host host.docker.internal:host-gateway \
    --volume "$smoke_directory/customer-node:/node-output" \
    "$customer_node_image" /node-output/install/run.sh --once >/dev/null; do
    customer_node_attempt=$((customer_node_attempt + 1))
    [ "$customer_node_attempt" -lt 3 ] || return 1
    sleep 1
  done
}

if [ -n "$previous_candidate_directory" ]; then
  project_output=$("$previous_cli" --endpoint "$control_plane_origin" \
    --ca-file "$service_ca" --token-file "$smoke_directory/legacy-user-token" \
    --tenant tenant-helm-smoke --request-id helm-smoke-project-create \
    --idempotency-key helm-smoke-project-create project create --name helm-smoke-project \
    --display-name 'Helm Smoke Project' --organization-id organization-helm-smoke)
else
  provision_admin_automation
  project_output=$("$candidate_cli" --profile "$admin_cli_profile" \
    --request-id helm-smoke-project-create --idempotency-key helm-smoke-project-create \
    project create --name helm-smoke-project --display-name 'Helm Smoke Project' \
    --organization-id organization-helm-smoke)
fi
project_id=$(printf '%s' "$project_output" | node -e 'const fs=require("node:fs");const value=JSON.parse(fs.readFileSync(0,"utf8"));process.stdout.write(value.metadata.uid)')
case "$project_id" in project-*) ;; *) echo "Helm project id is invalid" >&2; exit 1 ;; esac
verify_project() {
  "$candidate_cli" --profile "$admin_cli_profile" --request-id "$1" project get | \
    node -e 'const fs=require("node:fs");const value=JSON.parse(fs.readFileSync(0,"utf8"));if(value.metadata?.uid!==process.argv[1])process.exit(1)' "$project_id"
}
verify_legacy_project() {
  "$previous_cli" --endpoint "$control_plane_origin" --ca-file "$service_ca" \
    --token-file "$smoke_directory/legacy-user-token" --tenant tenant-helm-smoke --project "$project_id" \
    --request-id "$1" project get | node -e '
const fs = require("node:fs");
const value = JSON.parse(fs.readFileSync(0, "utf8"));
if (value.metadata?.uid !== process.argv[1]) process.exit(1);
' "$project_id"
}
if [ -n "$previous_candidate_directory" ]; then
  verify_legacy_project helm-smoke-project-before-upgrade
  stop_forwards
  initialize_identity_for_upgrade
  helm_apply upgrade "$chart" "$image_tag"
  wait_deployments
  assert_release_images "$image_tag"
  start_forwards
  provision_admin_automation
  provision_project_automation
  verify_project helm-smoke-project-after-upgrade

  stop_forwards
  helm --kube-context "$context" rollback "$release_name" 1 --namespace "$namespace" \
    --wait --timeout 5m >/dev/null
  wait_deployments
  assert_release_images "$previous_image_tag"
  start_forwards
  verify_legacy_project helm-smoke-project-after-rollback

  stop_forwards
  helm_apply upgrade "$chart" "$image_tag"
  wait_deployments
  assert_release_images "$image_tag"
  start_forwards
  verify_project helm-smoke-project-after-reupgrade
  cli=$candidate_cli
  upgrade_summary="$previous_version-$candidate_version-passed"
else
  provision_project_automation
  verify_project helm-smoke-project-fresh-install
fi
refresh_automation_tokens
automation_token_refresh_loop &
automation_refresh_pid=$!
curl --silent --show-error --fail-with-body --cacert "$service_ca" \
  --config "$smoke_directory/admin-curl.conf" --request PUT \
  --header 'Content-Type: application/json' --header 'X-Request-ID: helm-smoke-quota-create' \
  --header 'Idempotency-Key: helm-smoke-quota-create' \
  --data '{"expectedResourceVersion":"0","maxConcurrentLeases":1,"maxCpuMillis":1000,"maxMemoryBytes":536870912,"maxLeaseTtlSeconds":3600}' \
  "$control_plane_origin/v1/admin/tenants/tenant-helm-smoke/projects/$project_id/lease-quota" >/dev/null
user_status=$(curl --silent --output /dev/null --write-out '%{http_code}' --cacert "$service_ca" \
  --config "$smoke_directory/admin-denied-curl.conf" \
  --header 'X-Request-ID: helm-smoke-user-admin-denied' \
  "$control_plane_origin/v1/admin/tenants/tenant-helm-smoke/projects/$project_id/deployment-targets?pageSize=1")
test "$user_status" = 403 || {
  echo "underprivileged Admin automation token crossed the Helm Admin API" >&2
  exit 1
}

remote_worker_enrollment=remote-worker-helm-smoke
curl --silent --show-error --fail-with-body --cacert "$service_ca" \
  --config "$smoke_directory/admin-curl.conf" --request POST \
  --header 'Content-Type: application/json' --header 'X-Request-ID: helm-smoke-remote-worker-create' \
  --header 'Idempotency-Key: helm-smoke-remote-worker-create' \
  --data "{\"enrollmentId\":\"$remote_worker_enrollment\",\"workerId\":\"worker-helm-smoke\",\"workerName\":\"worker-helm-smoke\",\"ttlSeconds\":900}" \
  "$control_plane_origin/v1/admin/tenants/tenant-helm-smoke/projects/$project_id/remote-worker-enrollments" \
  >"$smoke_directory/remote-worker-created.json"
mkdir "$smoke_directory/customer-node"
docker run --rm --platform "$image_platform" \
  --label "cloud-agents.dev/test-run=$namespace" \
  --add-host host.docker.internal:host-gateway \
  --volume "$candidate_directory:/release:ro" \
  --volume "$smoke_directory:/smoke:ro" \
  --volume "$smoke_directory/customer-node:/node-output" \
  --env CLOUD_AGENTS_PLATFORM_RELEASE_DIR=/release \
  --env CLOUD_AGENTS_REMOTE_WORKER_INSTALL_DIR=/node-output/install \
  --env "CLOUD_AGENTS_REMOTE_WORKER_CONTROL_PLANE_URL=https://host.docker.internal:$control_plane_port" \
  --env CLOUD_AGENTS_REMOTE_WORKER_SERVER_CA_FILE=/smoke/ca.crt \
  --env CLOUD_AGENTS_REMOTE_WORKER_BOOTSTRAP_TOKEN_FILE=/smoke/bootstrap-token \
  --env CLOUD_AGENTS_REMOTE_WORKER_TENANT=tenant-helm-smoke \
  --env "CLOUD_AGENTS_REMOTE_WORKER_PROJECT=$project_id" \
  --env "CLOUD_AGENTS_REMOTE_WORKER_ENROLLMENT=$remote_worker_enrollment" \
  --env CLOUD_AGENTS_REMOTE_WORKER_INCARNATION=incarnation-helm-smoke \
  --env CLOUD_AGENTS_REMOTE_WORKER_CAPABILITIES=exec \
  --env CLOUD_AGENTS_REMOTE_WORKER_CAPACITY_CPU_MILLIS=1000 \
  --env CLOUD_AGENTS_REMOTE_WORKER_CAPACITY_MEMORY_BYTES=536870912 \
  --env CLOUD_AGENTS_REMOTE_WORKER_CAPACITY_DISK_BYTES=1073741824 \
  "$customer_node_image" sh /smoke/current/scripts/bootstrap-platform-remote-worker.sh
test ! -e "$smoke_directory/customer-node/install/enrollment-secret"
test ! -e "$smoke_directory/customer-node/install/claim-response.json"
docker run --rm --platform "$image_platform" \
  --volume "$smoke_directory/customer-node:/node-output:ro" "$customer_node_image" sh -c \
  'test "$(stat -c %a /node-output/install)" = 700 && test "$(stat -c %a /node-output/install/identity.pem)" = 600 && test "$(stat -c %a /node-output/install/identity.resource-version)" = 600 && test "$(cat /node-output/install/identity.resource-version)" = 3 && test "$(stat -c %a /node-output/install/control-plane-ca.pem)" = 400 && test "$(stat -c %a /node-output/install/run.sh)" = 500'
docker run --detach --platform "$image_platform" --name "$customer_node_container" \
  --label "cloud-agents.dev/test-run=$namespace" \
  --add-host host.docker.internal:host-gateway \
  --volume "$smoke_directory/customer-node:/node-output" \
  "$customer_node_image" /node-output/install/run.sh >/dev/null
docker inspect "$customer_node_container" | node -e '
const fs = require("node:fs");
const [container] = JSON.parse(fs.readFileSync(0, "utf8"));
if (Object.keys(container.Config.ExposedPorts ?? {}).length !== 0) process.exit(1);
if (Object.keys(container.HostConfig.PortBindings ?? {}).length !== 0) process.exit(1);
'
attempt=0
until curl --silent --show-error --fail-with-body --cacert "$service_ca" \
  --config "$smoke_directory/admin-curl.conf" \
  --header 'X-Request-ID: helm-smoke-remote-worker-get' \
  "$control_plane_origin/v1/admin/tenants/tenant-helm-smoke/projects/$project_id/remote-worker-enrollments/$remote_worker_enrollment" \
  >"$smoke_directory/remote-worker-online.json" \
  && node -e '
const fs = require("node:fs");
const value = JSON.parse(fs.readFileSync(process.argv[1], "utf8"));
const version = process.argv[2];
if (value.spec?.state !== "enrolled" || value.spec.node?.healthState !== "online" || value.spec.node?.workerVersion !== version || value.spec.node?.os !== "linux") process.exit(1);
if (JSON.stringify(value).includes("carw1_")) process.exit(1);
' "$smoke_directory/remote-worker-online.json" "$candidate_version"; do
  attempt=$((attempt + 1))
  if [ "$attempt" -ge 30 ]; then
    echo "fresh outbound customer node did not become online" >&2
    exit 1
  fi
  sleep 1
done

initial_certificate_sha256=$(node -e '
const fs = require("node:fs");
const value = JSON.parse(fs.readFileSync(process.argv[1], "utf8"));
if (value.metadata?.resourceVersion !== "3" || typeof value.spec?.certificateSha256 !== "string") process.exit(1);
process.stdout.write(value.spec.certificateSha256);
' "$smoke_directory/remote-worker-online.json")
docker rm --force "$customer_node_container" >/dev/null
cp "$smoke_directory/customer-node/install/identity.pem" "$smoke_directory/customer-node/old-identity.pem"
reload_control_plane_remote_worker_ca cloud-agents-remote-worker-ca-overlap \
  "$smoke_directory/remote-worker-ca-overlap.crt"
run_customer_node_once
docker run --rm --platform "$image_platform" \
  --label "cloud-agents.dev/test-run=$namespace" \
  --add-host host.docker.internal:host-gateway \
  --volume "$smoke_directory/customer-node:/node-output" \
  "$customer_node_image" /node-output/install/run.sh --rotate-certificate-once --once >/dev/null
test ! -e "$smoke_directory/customer-node/install/identity.resource-version.pending"
test "$(sed -n '1p' "$smoke_directory/customer-node/install/identity.resource-version")" = 4
if docker run --rm --platform "$image_platform" \
  --label "cloud-agents.dev/test-run=$namespace" \
  --add-host host.docker.internal:host-gateway \
  --volume "$smoke_directory/customer-node:/node-output" \
  "$customer_node_image" /node-output/install/run.sh \
    --certificate=/node-output/old-identity.pem --private-key=/node-output/old-identity.pem \
    --certificate-resource-version-file= --state-file=/node-output/old-state.json --once \
    >"$smoke_directory/old-identity.log" 2>&1; then
  echo "rotated RemoteWorker identity remained valid for heartbeat" >&2
  exit 1
fi
grep -q 'AUTHENTICATION_FAILED' "$smoke_directory/old-identity.log"
curl --silent --show-error --fail-with-body --cacert "$service_ca" \
  --config "$smoke_directory/admin-curl.conf" \
  --header 'X-Request-ID: helm-smoke-remote-worker-rotated' \
  "$control_plane_origin/v1/admin/tenants/tenant-helm-smoke/projects/$project_id/remote-worker-enrollments/$remote_worker_enrollment" \
  >"$smoke_directory/remote-worker-rotated.json"
node -e '
const fs = require("node:fs");
const value = JSON.parse(fs.readFileSync(process.argv[1], "utf8"));
if (value.metadata?.resourceVersion !== "4" || value.spec?.certificateState !== "active" || value.spec?.certificateSha256 === process.argv[2] || value.spec?.node?.healthState !== "online") process.exit(1);
' "$smoke_directory/remote-worker-rotated.json" "$initial_certificate_sha256"
openssl x509 -in "$smoke_directory/customer-node/install/identity.pem" \
  -out "$smoke_directory/customer-node/rotated-leaf.crt"
openssl verify -CAfile "$smoke_directory/remote-worker-next-ca.crt" \
  "$smoke_directory/customer-node/rotated-leaf.crt" >/dev/null
reload_control_plane_remote_worker_ca cloud-agents-remote-worker-ca-next \
  "$smoke_directory/remote-worker-next-ca.crt"
kubectl --context "$context" -n "$namespace" get secret cloud-agents-remote-worker-ca-next \
  -o jsonpath='{.data.ca\.crt}' | openssl base64 -d -A | \
  cmp - "$smoke_directory/remote-worker-next-ca.crt"
run_customer_node_once
if docker run --rm --platform "$image_platform" \
  --label "cloud-agents.dev/test-run=$namespace" \
  --add-host host.docker.internal:host-gateway \
  --volume "$smoke_directory/customer-node:/node-output" \
  "$customer_node_image" /node-output/install/run.sh \
    --certificate=/node-output/old-identity.pem --private-key=/node-output/old-identity.pem \
    --certificate-resource-version-file= --state-file=/node-output/old-root-state.json --once \
    >"$smoke_directory/old-root.log" 2>&1; then
  echo "old RemoteWorker CA root remained trusted after overlap removal" >&2
  exit 1
fi
grep -q 'AUTHENTICATION_FAILED' "$smoke_directory/old-root.log"
cp "$smoke_directory/ca.crt" "$smoke_directory/customer-node/old-control-plane-ca.pem"
install_customer_node_ca "$smoke_directory/service-ca-overlap.crt"
apply_service_identity_phase overlap control-plane worker worker-client access-gateway \
  "$smoke_directory/service-ca-overlap.crt"
run_customer_node_once
apply_service_identity_phase next control-plane-next worker-next worker-client-next \
  access-gateway-next "$smoke_directory/service-ca-overlap.crt"
run_customer_node_once
install_customer_node_ca "$smoke_directory/service-next-ca.crt"
apply_service_identity_phase final control-plane-next worker-next worker-client-next \
  access-gateway-next "$smoke_directory/service-next-ca.crt"
for secret in cloud-agents-control-plane-tls-final cloud-agents-worker-tls-final; do
  kubectl --context "$context" -n "$namespace" get secret "$secret" -o jsonpath='{.data.ca\.crt}' | \
    openssl base64 -d -A | cmp - "$smoke_directory/service-next-ca.crt"
done
run_customer_node_once
kubectl --context "$context" -n "$namespace" port-forward \
  service/$release_name-cloud-agents-worker "$worker_port:8091" >"$smoke_directory/worker-forward.log" 2>&1 &
worker_forward_pid=$!
attempt=0
worker_probe_log=$smoke_directory/worker-probe.log
until curl --silent --show-error --noproxy '*' \
  --cert "$worker_client_certificate" --key "$worker_client_key" \
  --cacert "$service_ca" --resolve "$service_prefix-worker:$worker_port:127.0.0.1" \
  "https://$service_prefix-worker:$worker_port/not-found" >"$worker_probe_log" 2>&1; do
  attempt=$((attempt + 1))
  if [ "$attempt" -ge 30 ]; then
    echo "Worker mTLS port-forward did not become ready" >&2
    cat "$smoke_directory/worker-forward.log" "$worker_probe_log" >&2
    exit 1
  fi
  sleep 1
done
if docker run --rm --platform "$image_platform" \
  --label "cloud-agents.dev/test-run=$namespace" \
  --add-host host.docker.internal:host-gateway \
  --volume "$smoke_directory/customer-node:/node-output" \
  "$customer_node_image" /node-output/install/run.sh \
    --server-ca=/node-output/old-control-plane-ca.pem --once \
    >"$smoke_directory/old-control-plane-ca.log" 2>&1; then
  echo "RemoteWorker trusted the old Control Plane root after removal" >&2
  exit 1
fi
if curl --silent --show-error --fail --cacert "$smoke_directory/ca.crt" \
  "https://127.0.0.1:$control_plane_port/readyz" \
  >"$smoke_directory/old-control-plane-root.log" 2>&1; then
  echo "Control Plane served a certificate trusted by the old root" >&2
  exit 1
fi
if curl --silent --show-error --fail --cacert "$smoke_directory/ca.crt" \
  "https://127.0.0.1:$gateway_port/healthz" \
  >"$smoke_directory/old-gateway-root.log" 2>&1; then
  echo "Access Gateway served a certificate trusted by the old root" >&2
  exit 1
fi
if curl --silent --show-error --noproxy '*' \
  --cert "$smoke_directory/worker-client.crt" \
  --key "$smoke_directory/worker-client.key" --cacert "$service_ca" \
  --resolve "$service_prefix-worker:$worker_port:127.0.0.1" \
  "https://$service_prefix-worker:$worker_port/not-found" \
  >"$smoke_directory/old-worker-client.log" 2>&1; then
  echo "Worker trusted the old Control Plane client certificate after root removal" >&2
  exit 1
fi
docker run --detach --platform "$image_platform" --name "$customer_node_container" \
  --label "cloud-agents.dev/test-run=$namespace" \
  --add-host host.docker.internal:host-gateway \
  --volume "$smoke_directory/customer-node:/node-output" \
  "$customer_node_image" /node-output/install/run.sh >/dev/null
identity_summary=leaf+ca-rotated+old-root-rejected
service_identity_summary=ca-rotated+old-rejected

kill "$user_forward_pid" "$admin_forward_pid" "$gateway_forward_pid" >/dev/null 2>&1 || true
wait "$user_forward_pid" "$admin_forward_pid" "$gateway_forward_pid" 2>/dev/null || true
user_forward_pid=
admin_forward_pid=
gateway_forward_pid=
kubectl --context "$context" -n "$namespace" rollout restart \
  deployment/$release_name-cloud-agents-access-gateway \
  deployment/$release_name-cloud-agents-user-web \
  deployment/$release_name-cloud-agents-admin-web >/dev/null
kubectl --context "$context" -n "$namespace" rollout status \
  deployment/$release_name-cloud-agents-access-gateway --timeout=180s >/dev/null
kubectl --context "$context" -n "$namespace" rollout status \
  deployment/$release_name-cloud-agents-user-web --timeout=180s >/dev/null
kubectl --context "$context" -n "$namespace" rollout status \
  deployment/$release_name-cloud-agents-admin-web --timeout=180s >/dev/null
kubectl --context "$context" -n "$namespace" port-forward \
  service/$release_name-cloud-agents-user-web "$user_port:4173" >"$smoke_directory/user-forward.log" 2>&1 &
user_forward_pid=$!
kubectl --context "$context" -n "$namespace" port-forward \
  service/$release_name-cloud-agents-admin-web "$admin_port:4174" >"$smoke_directory/admin-forward.log" 2>&1 &
admin_forward_pid=$!
kubectl --context "$context" -n "$namespace" port-forward \
  service/$release_name-cloud-agents-access-gateway "$gateway_port:8090" "$gateway_ssh_port:2222" >"$smoke_directory/gateway-forward.log" 2>&1 &
gateway_forward_pid=$!
attempt=0
until web_health_ready user.helm-smoke.localhost "$user_port" \
  && web_health_ready admin.helm-smoke.localhost "$admin_port" \
  && curl --silent --show-error --fail --cacert "$service_ca" "https://127.0.0.1:$gateway_port/healthz" >/dev/null 2>&1; do
  attempt=$((attempt + 1))
  if [ "$attempt" -ge 30 ]; then
    echo "Helm service port-forwards did not recover after restart" >&2
    exit 1
  fi
  sleep 1
done
CLOUD_AGENTS_BROWSER_PROVISION_MEMBER=1 \
CLOUD_AGENTS_BROWSER_TLS_SPKI="$browser_tls_spki" \
NODE_EXTRA_CA_CERTS="$smoke_directory/ca.crt" \
  node "$current_deployment/test/e2e/test-platform-compose-admin-web.mjs" \
	"https://admin.helm-smoke.localhost:$admin_port" "$smoke_directory/admin-account.json" "$smoke_directory/admin-denied-account.json" \
	"$smoke_directory/user-account.json" \
	tenant-helm-smoke "$project_id" "https://user.helm-smoke.localhost:$user_port"
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
expected_migration_summary="$candidate_migration_count:000001-$candidate_schema_head"
test "$migration_summary" = "$expected_migration_summary" || {
  echo "Helm migration ledger is not at packaged product schema head $candidate_schema_head" >&2
  exit 1
}
stop_forwards
helm --kube-context "$context" upgrade "$release_name" "$chart" --namespace "$namespace" \
  --wait --timeout 5m --reuse-values --set worker.enabled=false >/dev/null
for component in control-plane access-gateway admin-web user-web; do
  kubectl --context "$context" -n "$namespace" rollout status \
    deployment/$release_name-cloud-agents-$component --timeout=180s >/dev/null
done
for resource in deployment service; do
  if kubectl --context "$context" -n "$namespace" get \
    "$resource/$release_name-cloud-agents-worker" >/dev/null 2>&1; then
    echo "no-Agent Helm release retained the Managed Agent Worker $resource" >&2
    exit 1
  fi
done
if kubectl --context "$context" -n "$namespace" get \
  "pvc/$release_name-cloud-agents-workspace" >/dev/null 2>&1; then
  echo "no-Agent Helm release retained the Managed Agent workspace" >&2
  exit 1
fi
kubectl --context "$context" -n "$namespace" get \
  "deployment/$release_name-cloud-agents-control-plane" -o json | node -e '
const fs = require("node:fs");
const deployment = JSON.parse(fs.readFileSync(0, "utf8"));
const text = JSON.stringify(deployment.spec.template.spec);
if (/CLOUD_AGENTS_PLATFORM_(WORKER|ADMISSION)|provider-credentials|\/workspace/.test(text)) process.exit(1);
'
kubectl --context "$context" -n "$namespace" delete secret \
  cloud-agents-worker-tls cloud-agents-worker-client-tls cloud-agents-admission \
  cloud-agents-runtime-env cloud-agents-provider-credentials >/dev/null
kubectl --context "$context" -n "$namespace" rollout restart \
  deployment/$release_name-cloud-agents-control-plane >/dev/null
kubectl --context "$context" -n "$namespace" rollout status \
  deployment/$release_name-cloud-agents-control-plane --timeout=180s >/dev/null
start_forwards
verify_project helm-smoke-project-no-agent
no_agent_summary=worker+provider-secrets-absent
verified=true
