#!/bin/sh

set -eu

: "${CLOUD_AGENTS_ENDPOINT:?set the public Control Plane HTTPS endpoint}"
: "${CLOUD_AGENTS_ADMIN_TOKEN_FILE:?set the Admin API bearer token file}"
: "${CLOUD_AGENTS_USER_TOKEN_FILE:?set the User API bearer token file}"
: "${CLOUD_AGENTS_TENANT:?set the tenant id}"
: "${CLOUD_AGENTS_PROJECT:?set the project id}"
: "${CLOUD_AGENTS_TARGET_ID:?set a stable Kubernetes deployment target id}"
: "${CLOUD_AGENTS_TARGET_ENDPOINT:?set the target Kubernetes API HTTPS endpoint}"
: "${CLOUD_AGENTS_TARGET_CREDENTIAL_REF:?set the mounted Kubernetes credential reference}"
: "${CLOUD_AGENTS_WORKER_IMAGE_REPOSITORY:?set the Worker image repository without tag or digest}"
: "${CLOUD_AGENTS_RELEASE_DIGEST:?set the Worker image release digest}"
: "${CLOUD_AGENTS_WORKER_ARCHITECTURE:?set linux/amd64 or linux/arm64}"
: "${CLOUD_AGENTS_PLATFORM_VERSION:?set the verified platform version}"
: "${CLOUD_AGENTS_RUNTIME_VERSION:?set the verified runtime version}"
: "${CLOUD_AGENTS_CODEX_VERSION:?set the verified Codex version}"
: "${CLOUD_AGENTS_CLAUDE_CODE_VERSION:?set the verified Claude Code version}"
: "${CLOUD_AGENTS_PROVIDER_SECRET_REF:?set the target Provider credential Secret name}"
: "${CLOUD_AGENTS_KUBECONFIG:?set the operator kubeconfig used only by this acceptance script}"
: "${CLOUD_AGENTS_KUBERNETES_NAMESPACE:?set the target Worker namespace}"
: "${CLOUD_AGENTS_E2E_OUTPUT_DIR:?set a new directory for non-secret E2E results}"

cloud_agentsctl=${CLOUD_AGENTSCTL-cloud-agentsctl}
kubectl=${KUBECTL-kubectl}
ca_file=${CLOUD_AGENTS_CA_FILE-}
target_name=${CLOUD_AGENTS_TARGET_NAME-$CLOUD_AGENTS_TARGET_ID}
script_directory=$(CDPATH= cd "$(dirname "$0")" && pwd)

if [ ! -f "$CLOUD_AGENTS_ADMIN_TOKEN_FILE" ] || [ ! -f "$CLOUD_AGENTS_USER_TOKEN_FILE" ] || [ ! -f "$CLOUD_AGENTS_KUBECONFIG" ] || [ -e "$CLOUD_AGENTS_E2E_OUTPUT_DIR" ] || [ "${#CLOUD_AGENTS_TARGET_ID}" -gt 90 ]; then
  echo "Admin/User token files and kubeconfig must exist, target id must be at most 90 characters, and CLOUD_AGENTS_E2E_OUTPUT_DIR must be new" >&2
  exit 1
fi
command -v "$cloud_agentsctl" >/dev/null
command -v "$kubectl" >/dev/null
command -v curl >/dev/null
command -v node >/dev/null
mkdir -m 0700 "$CLOUD_AGENTS_E2E_OUTPUT_DIR"

admin_curl_config="$CLOUD_AGENTS_E2E_OUTPUT_DIR/.admin-curl.conf"
user_curl_config="$CLOUD_AGENTS_E2E_OUTPUT_DIR/.user-curl.conf"
printf 'header = "Authorization: Bearer %s"\n' "$(sed -n '1p' "$CLOUD_AGENTS_ADMIN_TOKEN_FILE")" >"$admin_curl_config"
printf 'header = "Authorization: Bearer %s"\n' "$(sed -n '1p' "$CLOUD_AGENTS_USER_TOKEN_FILE")" >"$user_curl_config"
chmod 0600 "$admin_curl_config" "$user_curl_config"

run_id="kubernetes-e2e-$(date -u +%Y%m%d%H%M%S)-$$"
lease_id="$run_id-lease"
codex_session="$run_id-codex"
claude_session="$run_id-claude"
lease_created=

run_ctl() {
  if [ -n "$ca_file" ]; then
    "$cloud_agentsctl" --endpoint "$CLOUD_AGENTS_ENDPOINT" --ca-file "$ca_file" --token-file "$CLOUD_AGENTS_USER_TOKEN_FILE" --tenant "$CLOUD_AGENTS_TENANT" "$@"
  else
    "$cloud_agentsctl" --endpoint "$CLOUD_AGENTS_ENDPOINT" --token-file "$CLOUD_AGENTS_USER_TOKEN_FILE" --tenant "$CLOUD_AGENTS_TENANT" "$@"
  fi
}

run_admin_ctl() {
  if [ -n "$ca_file" ]; then
    "$cloud_agentsctl" --endpoint "$CLOUD_AGENTS_ENDPOINT" --ca-file "$ca_file" --token-file "$CLOUD_AGENTS_ADMIN_TOKEN_FILE" --tenant "$CLOUD_AGENTS_TENANT" "$@"
  else
    "$cloud_agentsctl" --endpoint "$CLOUD_AGENTS_ENDPOINT" --token-file "$CLOUD_AGENTS_ADMIN_TOKEN_FILE" --tenant "$CLOUD_AGENTS_TENANT" "$@"
  fi
}

run_api() {
  config=$1
  method=$2
  path=$3
  request_id=$4
  shift 4
  if [ -n "$ca_file" ]; then
    curl --silent --show-error --fail-with-body --cacert "$ca_file" --config "$config" --request "$method" \
      --header "X-Request-ID: $request_id" "$@" "$CLOUD_AGENTS_ENDPOINT$path"
  else
    curl --silent --show-error --fail-with-body --config "$config" --request "$method" \
      --header "X-Request-ID: $request_id" "$@" "$CLOUD_AGENTS_ENDPOINT$path"
  fi
}

run_kubectl() {
  "$kubectl" --kubeconfig "$CLOUD_AGENTS_KUBECONFIG" --namespace "$CLOUD_AGENTS_KUBERNETES_NAMESPACE" "$@"
}

terminate_on_failure() {
  if [ -n "$lease_created" ]; then
    run_api "$user_curl_config" POST "/v1/tenants/$CLOUD_AGENTS_TENANT/projects/$CLOUD_AGENTS_PROJECT/environments/$lease_id:terminate" \
      "$run_id-environment-terminate" --header "Idempotency-Key: $run_id-environment-terminate" \
      --header 'Content-Type: application/json' --data '{"expectedGeneration":1}' >/dev/null 2>&1 || true
  fi
  rm -f "$admin_curl_config" "$user_curl_config"
}
trap terminate_on_failure EXIT HUP INT TERM

target_file="$CLOUD_AGENTS_E2E_OUTPUT_DIR/target.json"
run_admin_ctl --project "$CLOUD_AGENTS_PROJECT" --target "$CLOUD_AGENTS_TARGET_ID" \
  --request-id "kubernetes-target-register-$CLOUD_AGENTS_TARGET_ID" \
  --idempotency-key "kubernetes-target-register-$CLOUD_AGENTS_TARGET_ID" \
  target register --target-name "$target_name" --kind kubernetes \
  --target-endpoint "$CLOUD_AGENTS_TARGET_ENDPOINT" --credential-ref "$CLOUD_AGENTS_TARGET_CREDENTIAL_REF" >"$target_file"
run_admin_ctl --project "$CLOUD_AGENTS_PROJECT" --target "$CLOUD_AGENTS_TARGET_ID" \
  --request-id "$run_id-target-probe" --idempotency-key "$run_id-target-probe" \
  target probe --expected-generation 1 >"$CLOUD_AGENTS_E2E_OUTPUT_DIR/target-probe.json"
case "$(cat "$CLOUD_AGENTS_E2E_OUTPUT_DIR/target-probe.json")" in
  *'"targetKind":"kubernetes"'*'"observedPhase":"ready"'*) ;;
  *) echo "Kubernetes target probe did not become ready" >&2; exit 1 ;;
esac
run_admin_ctl --project "$CLOUD_AGENTS_PROJECT" --target "$CLOUD_AGENTS_TARGET_ID" \
  --request-id "$run_id-target-status" target get >"$CLOUD_AGENTS_E2E_OUTPUT_DIR/target-status.json"
case "$(cat "$CLOUD_AGENTS_E2E_OUTPUT_DIR/target-status.json")" in
  *'"targetKind":"kubernetes"'*'"observedPhase":"ready"'*) ;;
  *) echo "Kubernetes target ready status was not persisted" >&2; exit 1 ;;
esac

release_id="$run_id-release"
storage_policy_id="$run_id-storage"
network_policy_id="$run_id-network"
profile_id="$run_id-profile"
release_body_file="$CLOUD_AGENTS_E2E_OUTPUT_DIR/release-request.json"
CLOUD_AGENTS_E2E_RELEASE_ID="$release_id" CLOUD_AGENTS_E2E_WORKER_IMAGE_REPOSITORY="$CLOUD_AGENTS_WORKER_IMAGE_REPOSITORY" \
  CLOUD_AGENTS_E2E_RELEASE_DIGEST="$CLOUD_AGENTS_RELEASE_DIGEST" CLOUD_AGENTS_E2E_WORKER_ARCHITECTURE="$CLOUD_AGENTS_WORKER_ARCHITECTURE" \
  CLOUD_AGENTS_E2E_PLATFORM_VERSION="$CLOUD_AGENTS_PLATFORM_VERSION" CLOUD_AGENTS_E2E_RUNTIME_VERSION="$CLOUD_AGENTS_RUNTIME_VERSION" \
  CLOUD_AGENTS_E2E_CODEX_VERSION="$CLOUD_AGENTS_CODEX_VERSION" CLOUD_AGENTS_E2E_CLAUDE_CODE_VERSION="$CLOUD_AGENTS_CLAUDE_CODE_VERSION" \
  node >"$release_body_file" <<'NODE'
const value = {
  releaseId: process.env.CLOUD_AGENTS_E2E_RELEASE_ID,
  releaseName: process.env.CLOUD_AGENTS_E2E_RELEASE_ID,
  imageRepository: process.env.CLOUD_AGENTS_E2E_WORKER_IMAGE_REPOSITORY,
  releaseDigest: process.env.CLOUD_AGENTS_E2E_RELEASE_DIGEST,
  platformVersion: process.env.CLOUD_AGENTS_E2E_PLATFORM_VERSION,
  runtimeVersion: process.env.CLOUD_AGENTS_E2E_RUNTIME_VERSION,
  codexVersion: process.env.CLOUD_AGENTS_E2E_CODEX_VERSION,
  claudeCodeVersion: process.env.CLOUD_AGENTS_E2E_CLAUDE_CODE_VERSION,
  architectures: [process.env.CLOUD_AGENTS_E2E_WORKER_ARCHITECTURE],
  verificationEvidenceDigest: process.env.CLOUD_AGENTS_E2E_RELEASE_DIGEST,
};
process.stdout.write(JSON.stringify(value));
NODE
run_api "$admin_curl_config" POST "/v1/admin/tenants/$CLOUD_AGENTS_TENANT/projects/$CLOUD_AGENTS_PROJECT/worker-releases" \
  "$run_id-release-register" --header "Idempotency-Key: $run_id-release-register" --header 'Content-Type: application/json' \
  --data-binary "@$release_body_file" >"$CLOUD_AGENTS_E2E_OUTPUT_DIR/release.json"
run_api "$admin_curl_config" PUT "/v1/admin/tenants/$CLOUD_AGENTS_TENANT/projects/$CLOUD_AGENTS_PROJECT/storage-policies/$storage_policy_id" \
  "$run_id-storage-policy" --header "Idempotency-Key: $run_id-storage-policy" --header 'Content-Type: application/json' \
  --data "{\"expectedResourceVersion\":\"0\",\"policyName\":\"$storage_policy_id\",\"userSummary\":\"20 GiB managed workspace\",\"workspaceType\":\"managed-volume\",\"workspaceCapacityBytes\":21474836480,\"retentionSeconds\":0,\"cleanupOnLeaseTermination\":true,\"allowWorkspaceReuse\":true}" \
  >"$CLOUD_AGENTS_E2E_OUTPUT_DIR/storage-policy.json"
run_api "$admin_curl_config" PUT "/v1/admin/tenants/$CLOUD_AGENTS_TENANT/projects/$CLOUD_AGENTS_PROJECT/network-policies/$network_policy_id" \
  "$run_id-network-policy" --header "Idempotency-Key: $run_id-network-policy" --header 'Content-Type: application/json' \
  --data "{\"expectedResourceVersion\":\"0\",\"policyName\":\"$network_policy_id\",\"userSummary\":\"Public internet access\",\"defaultEgress\":\"public\",\"allowedEgress\":[],\"ingressEnabled\":false,\"previewEnabled\":false}" \
  >"$CLOUD_AGENTS_E2E_OUTPUT_DIR/network-policy.json"
profile_body_file="$CLOUD_AGENTS_E2E_OUTPUT_DIR/profile-request.json"
CLOUD_AGENTS_E2E_PROFILE_ID="$profile_id" CLOUD_AGENTS_E2E_STORAGE_POLICY_ID="$storage_policy_id" \
  CLOUD_AGENTS_E2E_NETWORK_POLICY_ID="$network_policy_id" CLOUD_AGENTS_E2E_RELEASE_DIGEST="$CLOUD_AGENTS_RELEASE_DIGEST" \
  CLOUD_AGENTS_E2E_TARGET_ID="$CLOUD_AGENTS_TARGET_ID" CLOUD_AGENTS_E2E_PROVIDER_CREDENTIAL_REF="$CLOUD_AGENTS_PROVIDER_SECRET_REF" \
  node >"$profile_body_file" <<'NODE'
const value = {
  profileId: process.env.CLOUD_AGENTS_E2E_PROFILE_ID,
  profileName: process.env.CLOUD_AGENTS_E2E_PROFILE_ID,
  version: 1,
  description: "Kubernetes target real E2E",
  providerKinds: ["codex", "claudeAgent"],
  cpuLimitMillis: 1000,
  memoryLimitBytes: 536870912,
  storagePolicyRef: process.env.CLOUD_AGENTS_E2E_STORAGE_POLICY_ID,
  networkPolicyRef: process.env.CLOUD_AGENTS_E2E_NETWORK_POLICY_ID,
  releaseDigest: process.env.CLOUD_AGENTS_E2E_RELEASE_DIGEST,
  targetRefs: [process.env.CLOUD_AGENTS_E2E_TARGET_ID],
  providerCredentialRef: process.env.CLOUD_AGENTS_E2E_PROVIDER_CREDENTIAL_REF,
};
process.stdout.write(JSON.stringify(value));
NODE
run_api "$admin_curl_config" POST "/v1/admin/tenants/$CLOUD_AGENTS_TENANT/projects/$CLOUD_AGENTS_PROJECT/environment-profiles" \
  "$run_id-profile-create" --header "Idempotency-Key: $run_id-profile-create" --header 'Content-Type: application/json' \
  --data-binary "@$profile_body_file" >"$CLOUD_AGENTS_E2E_OUTPUT_DIR/profile.json"
run_api "$admin_curl_config" POST "/v1/admin/tenants/$CLOUD_AGENTS_TENANT/projects/$CLOUD_AGENTS_PROJECT/environment-profiles/$profile_id/versions/1:publish" \
  "$run_id-profile-publish" --header "Idempotency-Key: $run_id-profile-publish" --header 'Content-Type: application/json' \
  --data '{"expectedResourceVersion":"1"}' >"$CLOUD_AGENTS_E2E_OUTPUT_DIR/profile-published.json"

lease_file="$CLOUD_AGENTS_E2E_OUTPUT_DIR/environment.json"
run_api "$user_curl_config" POST "/v1/tenants/$CLOUD_AGENTS_TENANT/projects/$CLOUD_AGENTS_PROJECT/environments" \
  "$run_id-environment-create" --header "Idempotency-Key: $run_id-environment-create" --header 'Content-Type: application/json' \
  --data "{\"profileId\":\"$profile_id\",\"profileVersion\":1}" >"$lease_file"

lease_id=$(CLOUD_AGENTS_E2E_ENVIRONMENT_FILE="$lease_file" node <<'NODE'
const { readFileSync } = require("node:fs");
const value = JSON.parse(readFileSync(process.env.CLOUD_AGENTS_E2E_ENVIRONMENT_FILE, "utf8"));
if (typeof value.environmentId !== "string" || value.environmentId === "") throw new Error("User API omitted the Environment id");
process.stdout.write(value.environmentId);
NODE
)
lease_created=1

case "$(cat "$lease_file")" in
  *'"observedPhase":"ready"'*) ;;
  *) echo "Kubernetes target Worker did not become ready" >&2; exit 1 ;;
esac
run_admin_ctl --project "$CLOUD_AGENTS_PROJECT" --lease "$lease_id" \
  --request-id "$run_id-lease-status" environment-lease get >"$CLOUD_AGENTS_E2E_OUTPUT_DIR/lease-status.json"
case "$(cat "$CLOUD_AGENTS_E2E_OUTPUT_DIR/lease-status.json")" in
  *'"observedPhase":"ready"'*'"cleanupPhase":"none"'*'"targetId":"'"$CLOUD_AGENTS_TARGET_ID"'"'*) ;;
  *) echo "Kubernetes target Lease ready status was not persisted" >&2; exit 1 ;;
esac

run_kubectl get deployments -l cloud-agents.dev/managed=true -o json >"$CLOUD_AGENTS_E2E_OUTPUT_DIR/deployments.json"
deployment_name=$(CLOUD_AGENTS_E2E_RESOURCE_FILE="$CLOUD_AGENTS_E2E_OUTPUT_DIR/deployments.json" CLOUD_AGENTS_E2E_TENANT="$CLOUD_AGENTS_TENANT" CLOUD_AGENTS_E2E_PROJECT="$CLOUD_AGENTS_PROJECT" CLOUD_AGENTS_E2E_TARGET="$CLOUD_AGENTS_TARGET_ID" CLOUD_AGENTS_E2E_LEASE="$lease_id" node <<'NODE'
const { readFileSync } = require("node:fs");
const value = JSON.parse(readFileSync(process.env.CLOUD_AGENTS_E2E_RESOURCE_FILE, "utf8"));
const names = value.items.filter(({ metadata }) => {
  const annotations = metadata?.annotations ?? {};
  return annotations["cloud-agents.dev/tenant"] === process.env.CLOUD_AGENTS_E2E_TENANT &&
    annotations["cloud-agents.dev/project"] === process.env.CLOUD_AGENTS_E2E_PROJECT &&
    annotations["cloud-agents.dev/target"] === process.env.CLOUD_AGENTS_E2E_TARGET &&
    annotations["cloud-agents.dev/lease"] === process.env.CLOUD_AGENTS_E2E_LEASE;
}).map(({ metadata }) => metadata.name);
if (names.length !== 1) throw new Error(`expected one managed Worker Deployment, got ${names.length}`);
process.stdout.write(names[0]);
NODE
)

create_session() {
  provider=$1
  session_id=$2
  run_ctl --project "$CLOUD_AGENTS_PROJECT" --lease "$lease_id" --session "$session_id" \
    --request-id "$session_id-create" --idempotency-key "$session_id-create" session create --provider "$provider" >/dev/null
}

run_real_turn() {
  provider=$1
  session_id=$2
  phase=$3
  turn_id="$session_id-$phase-turn"
  execution_id="$session_id-$phase-execution"
  artifact_path=".cloud-agents-acceptance/$run_id-$provider-$phase.txt"
  expected_content="cloud-agents Kubernetes target $provider $phase real E2E"
  case "$provider" in
    codex) file_tool="Use apply_patch to create" ;;
    claudeAgent) file_tool="Use the Write tool to create" ;;
    *) echo "unsupported Provider $provider" >&2; exit 1 ;;
  esac
  prompt="$file_tool exactly one file at $artifact_path. Its complete contents must be the single ASCII line '$expected_content' followed by a newline. Do not modify any other file. Then reply done."
  run_ctl --project "$CLOUD_AGENTS_PROJECT" --session "$session_id" --turn "$turn_id" \
    --request-id "$turn_id-create" --idempotency-key "$turn_id-create" turn create --input "$prompt" >/dev/null
  execution_file="$CLOUD_AGENTS_E2E_OUTPUT_DIR/$execution_id.json"
  run_ctl --timeout 10m --project "$CLOUD_AGENTS_PROJECT" --session "$session_id" --turn "$turn_id" --execution "$execution_id" \
    --request-id "$execution_id-run" --idempotency-key "$execution_id-run" execution execute \
    --runtime-mode full-access --interaction-mode default --input "$prompt" >"$execution_file"
  artifact_index=$(CLOUD_AGENTS_E2E_EXECUTION_FILE="$execution_file" CLOUD_AGENTS_E2E_ARTIFACT_PATH="$artifact_path" node <<'NODE'
const { readFileSync } = require("node:fs");
const value = JSON.parse(readFileSync(process.env.CLOUD_AGENTS_E2E_EXECUTION_FILE, "utf8"));
if (value.spec?.state !== "succeeded") throw new Error("real Provider execution did not succeed");
const indexes = value.messages.flatMap((message, index) => {
  const artifact = message.payload?.artifact;
  return message.messageType === "ArtifactCandidate" && artifact?.sourceRoot === "workspace" && artifact?.path === process.env.CLOUD_AGENTS_E2E_ARTIFACT_PATH && typeof artifact?.kind === "string" && artifact.kind.replaceAll("_", "-") === "generated-file" ? [index] : [];
});
if (indexes.length !== 1) throw new Error("expected one workspace ArtifactCandidate");
process.stdout.write(String(indexes[0]));
NODE
  )
  artifact_file="$CLOUD_AGENTS_E2E_OUTPUT_DIR/$execution_id-artifact.txt"
  run_ctl --project "$CLOUD_AGENTS_PROJECT" --session "$session_id" --turn "$turn_id" --execution "$execution_id" \
    --request-id "$execution_id-artifact" execution download-artifact --message-index "$artifact_index" >"$artifact_file"
  CLOUD_AGENTS_E2E_ARTIFACT_FILE="$artifact_file" CLOUD_AGENTS_E2E_EXPECTED_CONTENT="$expected_content" node <<'NODE'
const { readFileSync } = require("node:fs");
const expected = Buffer.from(`${process.env.CLOUD_AGENTS_E2E_EXPECTED_CONTENT}\n`);
if (!readFileSync(process.env.CLOUD_AGENTS_E2E_ARTIFACT_FILE).equals(expected)) throw new Error("downloaded Artifact content changed");
NODE
  run_ctl --timeout 1m --project "$CLOUD_AGENTS_PROJECT" --session "$session_id" --execution "$execution_id" \
    --request-id "$execution_id-events" events watch --limit 64 --until-terminal >"$CLOUD_AGENTS_E2E_OUTPUT_DIR/$execution_id-events.jsonl"
}

create_session codex "$codex_session"
run_real_turn codex "$codex_session" before-restart
run_kubectl rollout restart "deployment/$deployment_name"
run_kubectl rollout status "deployment/$deployment_name" --timeout=3m
sleep 2
run_real_turn codex "$codex_session" after-restart
create_session claudeAgent "$claude_session"
run_real_turn claudeAgent "$claude_session" after-restart
CLOUD_AGENTS_E2E_LEASE_ID="$lease_id" CLOUD_AGENTS_E2E_RUN_ID="$run_id" \
  sh "$script_directory/test-platform-agent-interactions.sh"

for session_id in "$codex_session" "$claude_session"; do
  run_ctl --project "$CLOUD_AGENTS_PROJECT" --session "$session_id" --request-id "$session_id-close" \
    --idempotency-key "$session_id-close" session close >/dev/null
done

terminate_file="$CLOUD_AGENTS_E2E_OUTPUT_DIR/lease-terminate.json"
run_api "$user_curl_config" POST "/v1/tenants/$CLOUD_AGENTS_TENANT/projects/$CLOUD_AGENTS_PROJECT/environments/$lease_id:terminate" \
  "$run_id-environment-terminate" --header "Idempotency-Key: $run_id-environment-terminate" \
  --header 'Content-Type: application/json' --data '{"expectedGeneration":1}' >"$terminate_file"
run_api "$user_curl_config" POST "/v1/tenants/$CLOUD_AGENTS_TENANT/projects/$CLOUD_AGENTS_PROJECT/environments/$lease_id:terminate" \
  "$run_id-environment-terminate" --header "Idempotency-Key: $run_id-environment-terminate" \
  --header 'Content-Type: application/json' --data '{"expectedGeneration":1}' >"$CLOUD_AGENTS_E2E_OUTPUT_DIR/lease-terminate-replay.json"
case "$(cat "$terminate_file")" in
  *'"observedPhase":"terminated"'*) ;;
  *) echo "Kubernetes target Lease did not terminate cleanly" >&2; exit 1 ;;
esac
cmp "$terminate_file" "$CLOUD_AGENTS_E2E_OUTPUT_DIR/lease-terminate-replay.json"
run_admin_ctl --project "$CLOUD_AGENTS_PROJECT" --lease "$lease_id" \
  --request-id "$run_id-lease-terminated-status" environment-lease get >"$CLOUD_AGENTS_E2E_OUTPUT_DIR/lease-terminated-status.json"
case "$(cat "$CLOUD_AGENTS_E2E_OUTPUT_DIR/lease-terminated-status.json")" in
  *'"desiredPhase":"terminated"'*'"observedPhase":"terminated"'*'"cleanupPhase":"complete"'*) ;;
  *) echo "Kubernetes target Admin Lease projection did not record cleanup" >&2; exit 1 ;;
esac
lease_created=

run_admin_ctl --project "$CLOUD_AGENTS_PROJECT" --target "$CLOUD_AGENTS_TARGET_ID" \
  --request-id "$run_id-target-cleanup" --idempotency-key "$run_id-target-cleanup" \
  target cleanup --expected-generation 1 --confirm-target-id "$CLOUD_AGENTS_TARGET_ID" >"$CLOUD_AGENTS_E2E_OUTPUT_DIR/target-cleanup.json"
for resource in deployments services persistentvolumeclaims; do
  run_kubectl get "$resource" -l cloud-agents.dev/managed=true -o json >"$CLOUD_AGENTS_E2E_OUTPUT_DIR/$resource-final.json"
done
CLOUD_AGENTS_E2E_OUTPUT_DIR="$CLOUD_AGENTS_E2E_OUTPUT_DIR" CLOUD_AGENTS_E2E_LEASE="$lease_id" node <<'NODE'
const { readFileSync } = require("node:fs");
for (const resource of ["deployments", "services", "persistentvolumeclaims"]) {
  const value = JSON.parse(readFileSync(`${process.env.CLOUD_AGENTS_E2E_OUTPUT_DIR}/${resource}-final.json`, "utf8"));
  if (value.items.some(({ metadata }) => metadata?.annotations?.["cloud-agents.dev/lease"] === process.env.CLOUD_AGENTS_E2E_LEASE)) throw new Error(`${resource} retained the terminated Lease`);
}
NODE

printf '%s\n' "Kubernetes target real E2E passed; results: $CLOUD_AGENTS_E2E_OUTPUT_DIR"
