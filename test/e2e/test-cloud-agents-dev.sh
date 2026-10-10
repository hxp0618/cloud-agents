#!/usr/bin/env bash

set -euo pipefail
umask 077

repository_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)
for command in docker node; do
  if ! command -v "$command" >/dev/null 2>&1; then
    echo "cloud-agents dev smoke requires $command" >&2
    exit 2
  fi
done
mkdir -p "$repository_root/.tmp"
log_file=$(mktemp "$repository_root/.tmp/cloud-agents-dev-smoke.XXXXXX.log")
dev_pid=
container_name=
state_directory=
fixture_pid=
fixture_directory=
foundation_project_id=
foundation_workspace_id=

cleanup() {
  status=$?
  trap - EXIT HUP INT TERM
  if [[ -n $dev_pid ]] && kill -0 "$dev_pid" 2>/dev/null; then
    kill -TERM "$dev_pid" 2>/dev/null || true
    wait "$dev_pid" 2>/dev/null || true
  fi
  if [[ -n $container_name ]]; then
    docker rm -f "$container_name" >/dev/null 2>&1 || true
  fi
  if [[ -n $fixture_pid ]] && kill -0 "$fixture_pid" 2>/dev/null; then
    kill -TERM "$fixture_pid" 2>/dev/null || true
    wait "$fixture_pid" 2>/dev/null || true
  fi
  if [[ -n $foundation_project_id && -n $foundation_workspace_id ]]; then
    for volume in $(docker volume ls -q \
      --filter label=cloud-agents.dev/managed=true \
      --filter label=cloud-agents.dev/resource=foundation-workspace \
      --filter label=cloud-agents.dev/tenant=tenant-local \
      --filter label=cloud-agents.dev/project="$foundation_project_id" \
      --filter label=cloud-agents.dev/workspace="$foundation_workspace_id"); do
      docker volume rm "$volume" >/dev/null 2>&1 || true
    done
  fi
  case "$fixture_directory" in
    "$repository_root"/.tmp/cloud-agents-dev-foundation.*)
      if [[ -d $fixture_directory && ! -L $fixture_directory ]]; then
        find "$fixture_directory" -depth -delete
      fi
      ;;
  esac
  case "$state_directory" in
    "$repository_root"/.tmp/cloud-agents-dev.*)
      if [[ -d $state_directory && ! -L $state_directory ]]; then
        find "$state_directory" -depth -delete
      fi
      ;;
  esac
  find "$repository_root/.tmp" -maxdepth 1 -type f -name "$(basename -- "$log_file")" -delete
  exit "$status"
}
trap cleanup EXIT HUP INT TERM

read -r control_plane_port worker_port identity_port admin_port user_port < <(node <<'NODE'
const net = require("node:net");
const servers = Array.from({length:5}, () => net.createServer());
Promise.all(
  servers.map(
    (server) =>
      new Promise((resolve, reject) => {
        server.once("error", reject);
        server.listen(0, "127.0.0.1", resolve);
      }),
  ),
)
  .then(() => {
    console.log(servers.map((server) => server.address().port).join(" "));
    return Promise.all(servers.map((server) => new Promise((resolve) => server.close(resolve))));
  })
  .catch((error) => {
    console.error(error);
    process.exitCode = 1;
  });
NODE
)

fixture_directory=$(mktemp -d "$repository_root/.tmp/cloud-agents-dev-foundation.XXXXXX")
node "$repository_root/test/e2e/test-cloud-agents-dev-foundation.mjs" "$fixture_directory" >"$fixture_directory/helper.log" 2>&1 &
fixture_pid=$!
for attempt in {1..300}; do
  if [[ -f $fixture_directory/ready && -f $fixture_directory/fixture.json ]]; then
    break
  fi
  if ! kill -0 "$fixture_pid" 2>/dev/null; then
    cat "$fixture_directory/helper.log" >&2
    echo "localdev Foundation fixture exited before becoming ready" >&2
    exit 1
  fi
  if [[ $attempt == 300 ]]; then
    cat "$fixture_directory/helper.log" >&2
    echo "localdev Foundation fixture did not become ready" >&2
    exit 1
  fi
  sleep 1
done
FIXTURE_FILE="$fixture_directory/fixture.json" node -e '
const fs=require("node:fs"); const value=JSON.parse(fs.readFileSync(process.env.FIXTURE_FILE,"utf8"));
console.log("opensandbox_server_binding="+JSON.stringify(value.serverBinding));'
fixture_values=$(FIXTURE_FILE="$fixture_directory/fixture.json" node -e '
const fs=require("node:fs"); const value=JSON.parse(fs.readFileSync(process.env.FIXTURE_FILE,"utf8"));
process.stdout.write([value.dockerCredentials,value.providerCredentials,value.dockerEndpoint,value.credentialRef,value.imageURI,value.releaseDigest].join("\n"));')
fixture_parts=()
while IFS= read -r fixture_value; do
  fixture_parts+=("$fixture_value")
done <<<"$fixture_values"
docker_credentials_directory=${fixture_parts[0]}
provider_credentials_directory=${fixture_parts[1]}
docker_endpoint=${fixture_parts[2]}
docker_credential_ref=${fixture_parts[3]}
foundation_image_uri=${fixture_parts[4]}
foundation_release_digest=${fixture_parts[5]}

CLOUD_AGENTS_DEV_CONTROL_PLANE_LISTEN="127.0.0.1:$control_plane_port" \
CLOUD_AGENTS_DEV_WORKER_LISTEN="127.0.0.1:$worker_port" \
CLOUD_AGENTS_DEV_IDENTITY_PORT="$identity_port" CLOUD_AGENTS_DEV_ADMIN_PORT="$admin_port" CLOUD_AGENTS_DEV_USER_PORT="$user_port" \
CLOUD_AGENTS_DEV_PROVIDER_CREDENTIALS_DIR="$provider_credentials_directory" \
CLOUD_AGENTS_DEV_DOCKER_CREDENTIALS_DIR="$docker_credentials_directory" \
  bash "$repository_root/scripts/cloud-agents-dev.sh" >"$log_file" 2>&1 &
dev_pid=$!
container_name="cloud-agents-dev-${UID:-0}-$dev_pid"

for attempt in {1..180}; do
  if grep -Fq "State directory: " "$log_file"; then
    break
  fi
  if ! kill -0 "$dev_pid" 2>/dev/null; then
    wait "$dev_pid" 2>/dev/null || true
    cat "$log_file" >&2
    echo "cloud-agents dev exited before becoming ready" >&2
    exit 1
  fi
  if [[ $attempt == 180 ]]; then
    cat "$log_file" >&2
    echo "cloud-agents dev did not become ready" >&2
    exit 1
  fi
  sleep 1
done

state_directory=$(sed -n 's/^State directory: //p' "$log_file" | tail -n 1)
case "$state_directory" in
  "$repository_root"/.tmp/cloud-agents-dev.*) ;;
  *) echo "cloud-agents dev did not report an owned state directory" >&2; exit 1 ;;
esac
cli="$state_directory/bin/cloud-agentsctl"
admin_origin="https://localhost:$admin_port"
user_origin="https://127.0.0.1:$user_port"
export NODE_EXTRA_CA_CERTS="$state_directory/ca.crt"
automation_fixture="$repository_root/test/e2e/identity-automation-fixture.mjs"
node "$automation_fixture" create "$admin_origin" "$state_directory/admin-account.json" tenant-local \
  localdev-admin admin tenant.admin tenant tenant-local "$state_directory/admin.credential"
"$cli" profile configure-service-account --web-endpoint "$admin_origin" --control-plane-endpoint "https://127.0.0.1:$control_plane_port" \
  --application admin --ca-file "$state_directory/ca.crt" --profile "$state_directory/admin.profile" \
  --credential-file "$state_directory/admin.credential" --tenant tenant-local >/dev/null
admin_common=(--profile "$state_directory/admin.profile")
common=()
control_plane_curl() {
  local application=$1
  shift
  local origin=$admin_origin
  if [[ $application == user ]]; then origin=$user_origin; fi
  local prefix
  prefix=$(mktemp -d "$state_directory/request.XXXXXXXX")/token
  node "$automation_fixture" exchange "$origin" "$state_directory/$application.credential" tenant-local "$prefix" "$project_id"
  local status=0
  curl --cacert "$state_directory/ca.crt" --config "$prefix.curl.conf" "$@" || status=$?
  rm -f "$prefix.token" "$prefix.curl.conf"
  rmdir "${prefix%/token}"
  return "$status"
}
control_plane_api() {
  local method=$1 path=$2 request_id=$3 idempotency_key=$4 body=$5 output=$6
  control_plane_curl admin --silent --show-error --fail-with-body --request "$method" \
    --header "X-Request-ID: $request_id" --header "Idempotency-Key: $idempotency_key" --header "Content-Type: application/json" \
    --data "$body" "https://127.0.0.1:$control_plane_port$path" >"$output" ||
    { status=$?; cat "$output" >&2; return "$status"; }
}

project_json=$("$cli" "${admin_common[@]}" --request-id localdev-smoke-project \
  --idempotency-key localdev-smoke-project-key project create --name localdev-smoke-project \
  --display-name "Localdev Smoke Project" --organization-id organization-local)
project_id=$(printf '%s' "$project_json" | node -e 'const fs=require("node:fs");process.stdout.write(JSON.parse(fs.readFileSync(0,"utf8")).metadata.uid)')
"$cli" profile use --profile "$state_directory/admin.profile" --tenant tenant-local --project "$project_id" >/dev/null
node "$automation_fixture" create "$admin_origin" "$state_directory/admin-account.json" tenant-local \
  localdev-user user project.operator project "$project_id" "$state_directory/user.credential"
"$cli" profile configure-service-account --web-endpoint "$user_origin" --control-plane-endpoint "https://127.0.0.1:$control_plane_port" \
  --application user --ca-file "$state_directory/ca.crt" --profile "$state_directory/user.profile" \
  --credential-file "$state_directory/user.credential" --tenant tenant-local --project "$project_id" >/dev/null
common=(--profile "$state_directory/user.profile")
foundation_project_id="$project_id"
case "$project_id" in project-*) ;; *) echo "localdev project id is invalid" >&2; exit 1 ;; esac
foundation_workspace_id="localdev-foundation-workspace-${project_id#project-}"
foundation_sandbox_id="localdev-foundation-sandbox-${project_id#project-}"

storage_policy_body='{"expectedResourceVersion":"0","policyName":"storage-localdev","userSummary":"20 GiB managed workspace","workspaceType":"managed-volume","workspaceCapacityBytes":21474836480,"retentionSeconds":0,"cleanupOnLeaseTermination":true,"allowWorkspaceReuse":true}'
control_plane_api PUT "/v1/admin/tenants/tenant-local/projects/$project_id/storage-policies/storage-localdev" \
  localdev-storage-policy localdev-storage-policy "$storage_policy_body" "$state_directory/storage-policy.json"
network_policy_body='{"expectedResourceVersion":"0","policyName":"network-localdev","userSummary":"Codex Provider API access only","defaultEgress":"restricted","allowedEgress":["api.openai.com"],"ingressEnabled":false,"previewEnabled":false}'
control_plane_api PUT "/v1/admin/tenants/tenant-local/projects/$project_id/network-policies/network-localdev" \
  localdev-network-policy localdev-network-policy "$network_policy_body" "$state_directory/network-policy.json"

target_output=$("$cli" "${admin_common[@]}" --target local-docker-target \
  --request-id localdev-target-register --idempotency-key localdev-target-register target register \
  --target-name local-docker-target --kind docker --target-endpoint "$docker_endpoint" --credential-ref "$docker_credential_ref")
case "$target_output" in *'"generation":1'*'"targetKind":"docker"'*) ;; *) echo "localdev Docker target was not registered: $target_output" >&2; exit 1 ;; esac
probe_output=$("$cli" "${admin_common[@]}" --target local-docker-target \
  --request-id localdev-target-probe --idempotency-key localdev-target-probe target probe --expected-generation 1)
case "$probe_output" in *'"generation":1'*'"observedPhase":"ready"'*) ;; *) echo "localdev Docker target did not become ready: $probe_output" >&2; exit 1 ;; esac

runtime_profile_body=$(printf '{"profileId":"localdev-agent-runtime","profileName":"localdev-agent-runtime","version":1,"description":"Local Foundation Agent Runtime","workloadTrust":"trusted-single-tenant","isolationRuntime":"runc","targetId":"local-docker-target","networkPolicyRef":"network-localdev","imageUri":"%s","releaseDigest":"%s","cpuMillis":1000,"memoryBytes":536870912}' "$foundation_image_uri" "$foundation_release_digest")
control_plane_api POST "/v1/admin/tenants/tenant-local/projects/$project_id/runtime-profiles" \
  localdev-runtime-profile localdev-runtime-profile "$runtime_profile_body" "$state_directory/runtime-profile.json"
control_plane_api POST "/v1/admin/tenants/tenant-local/projects/$project_id/runtime-profiles/localdev-agent-runtime/versions/1:publish" \
  localdev-runtime-profile-publish localdev-runtime-profile-publish '{"expectedResourceVersion":"1"}' "$state_directory/runtime-profile-published.json"
release_body=$(printf '{"releaseId":"localdev-agent-release","releaseName":"localdev-agent-release","imageRepository":"%s","releaseDigest":"%s","platformVersion":"platform-v1","runtimeVersion":"runtime-v1","codexVersion":"codex-v1","claudeCodeVersion":"claude-v1","architectures":["linux/arm64"],"verificationEvidenceDigest":"%s"}' "${foundation_image_uri%@*}" "$foundation_release_digest" "$foundation_release_digest")
control_plane_api POST "/v1/admin/tenants/tenant-local/projects/$project_id/worker-releases" \
  localdev-release-create localdev-release-create "$release_body" "$state_directory/release.json"
environment_profile_body=$(printf '{"profileId":"localdev-agent-environment","profileName":"localdev-agent-environment","version":1,"description":"Local Foundation Agent Environment","providerKinds":["codex"],"cpuLimitMillis":1000,"memoryLimitBytes":536870912,"storagePolicyRef":"storage-localdev","networkPolicyRef":"network-localdev","releaseDigest":"%s","targetRefs":["local-docker-target"],"providerCredentialRef":"provider-localdev"}' "$foundation_release_digest")
control_plane_api POST "/v1/admin/tenants/tenant-local/projects/$project_id/environment-profiles" \
  localdev-environment-profile localdev-environment-profile "$environment_profile_body" "$state_directory/environment-profile.json"
environment_profile_path="/v1/admin/tenants/tenant-local/projects/$project_id/environment-profiles/localdev-agent-environment"
control_plane_curl admin --silent --show-error --fail --request GET \
  --header "X-Request-ID: localdev-environment-profile-get" \
  "https://127.0.0.1:$control_plane_port/v1/admin/tenants/tenant-local/projects/$project_id/environment-profiles?pageSize=100" >"$state_directory/environment-profile-current.json"
environment_profile_resource_version=$(ENVIRONMENT_PROFILE_FILE="$state_directory/environment-profile-current.json" node -e 'const fs=require("node:fs");const v=JSON.parse(fs.readFileSync(process.env.ENVIRONMENT_PROFILE_FILE,"utf8"));const p=(v.environmentProfiles??[]).find((item)=>item.metadata?.uid==="localdev-agent-environment"||item.metadata?.name==="localdev-agent-environment");process.stdout.write(String(p?.metadata?.resourceVersion??""));')
if [[ -z $environment_profile_resource_version ]]; then
  echo "localdev environment profile did not return a resource version" >&2
  exit 1
fi
control_plane_api POST "$environment_profile_path/versions/1:publish" \
  localdev-environment-profile-publish localdev-environment-profile-publish "{\"expectedResourceVersion\":\"$environment_profile_resource_version\"}" "$state_directory/environment-profile-published.json"

foundation_sandbox_body=$(printf '{"workspaceId":"%s","workspaceName":"%s","sandboxId":"%s","runtimeProfileId":"localdev-agent-runtime","runtimeProfileVersion":1,"ttlSeconds":600}' "$foundation_workspace_id" "$foundation_workspace_id" "$foundation_sandbox_id")
control_plane_curl user --silent --show-error --fail --request POST \
  --header "X-Request-ID: $foundation_sandbox_id" \
  --header "Idempotency-Key: $foundation_sandbox_id" --header "Content-Type: application/json" \
  --data "$foundation_sandbox_body" "https://127.0.0.1:$control_plane_port/v1/tenants/tenant-local/projects/$project_id/sandbox-sessions" >"$state_directory/sandbox-create.json"
sandbox_generation=
sandbox_runtime_id=
sandbox_resource_version=
for attempt in {1..180}; do
  sandbox_file="$state_directory/sandbox.json"
  control_plane_curl admin --silent --show-error --fail --request GET \
    --header "X-Request-ID: localdev-foundation-sandbox-get" \
    "https://127.0.0.1:$control_plane_port/v1/admin/tenants/tenant-local/projects/$project_id/sandbox-sessions/$foundation_sandbox_id" >"$sandbox_file"
  sandbox_values=$(SANDBOX_FILE="$sandbox_file" node -e 'const fs=require("node:fs");const v=JSON.parse(fs.readFileSync(process.env.SANDBOX_FILE,"utf8"));process.stdout.write([v.spec?.observedState??"",v.spec?.generation??"",v.spec?.runtimeId??"",v.spec?.stableErrorCode??"",v.metadata?.resourceVersion??""].join("|"));')
  IFS='|' read -r sandbox_state sandbox_generation sandbox_runtime_id sandbox_error sandbox_resource_version <<<"$sandbox_values"
  if [[ $sandbox_state == running ]]; then
    printf '%s\n' "$sandbox_runtime_id" >"$fixture_directory/runtime-id"
    break
  fi
  if [[ $sandbox_state == failed ]]; then
    echo "localdev Foundation Sandbox failed: $sandbox_error" >&2
    exit 1
  fi
  if [[ $attempt == 180 ]]; then
    echo "localdev Foundation Sandbox did not become ready: state=$sandbox_state error=$sandbox_error" >&2
    exit 1
  fi
  sleep 1
done
if [[ ! $sandbox_generation =~ ^[1-9][0-9]*$ || -z $sandbox_runtime_id ]]; then
  echo "localdev Foundation Sandbox returned an invalid generation/runtime" >&2
  exit 1
fi

"$cli" "${common[@]}" --session localdev-smoke-session \
  --request-id localdev-smoke-session --idempotency-key localdev-smoke-session-key \
  session create --provider codex --workspace "$foundation_workspace_id" --sandbox "$foundation_sandbox_id" \
  --sandbox-generation "$sandbox_generation" --environment-profile localdev-agent-environment --environment-profile-version 1 >/dev/null
set +e
execute_output=$("$cli" "${common[@]}" --session localdev-smoke-session \
  --turn localdev-smoke-turn --execution localdev-smoke-execution \
  --request-id localdev-smoke-execution --idempotency-key localdev-smoke-execution-key \
  execution execute --runtime-mode approval-required --interaction-mode default \
  --input "verify localdev Runtime" 2>&1)
execute_status=$?
set -e
if [[ $execute_status != 2 || $execute_output != "cloud-agentsctl: managedAgentExecute: RUNTIME_FAILED" ]]; then
  echo "localdev Runtime failure boundary changed: exit=$execute_status output=$execute_output" >&2
  exit 1
fi
execution_file="$state_directory/execution.json"
"$cli" "${common[@]}" --session localdev-smoke-session \
  --turn localdev-smoke-turn --execution localdev-smoke-execution \
  --request-id localdev-smoke-execution-get execution get >"$execution_file"
if ! EXECUTION_FILE="$execution_file" node -e '
const fs = require("node:fs");
const execution = JSON.parse(fs.readFileSync(process.env.EXECUTION_FILE, "utf8"));
if (execution.spec?.state !== "failed" || execution.spec?.errorCode !== "credential_invalid") process.exit(1);
'; then
  cat "$execution_file" >&2
  exit 1
fi

kill -TERM "$dev_pid"
set +e
wait "$dev_pid"
dev_status=$?
set -e
dev_pid=
if [[ $dev_status != 0 && $dev_status != 130 && $dev_status != 143 ]]; then
  cat "$log_file" >&2
  echo "cloud-agents dev shutdown failed: exit=$dev_status" >&2
  exit 1
fi
if [[ -e $state_directory ]] || docker inspect "$container_name" >/dev/null 2>&1; then
  echo "cloud-agents dev left owned resources after shutdown" >&2
  exit 1
fi

echo "cloud-agents dev smoke passed"
