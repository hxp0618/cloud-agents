#!/bin/sh

set -eu

: "${CLOUD_AGENTS_ENDPOINT:?set the public Control Plane HTTPS endpoint}"
: "${CLOUD_AGENTS_TOKEN_FILE:?set the Control Plane bearer token file}"
: "${CLOUD_AGENTS_TENANT:?set the tenant id}"
: "${CLOUD_AGENTS_PROJECT:?set the project id}"
: "${CLOUD_AGENTS_TARGET_ID:?set a stable Kubernetes deployment target id}"
: "${CLOUD_AGENTS_TARGET_ENDPOINT:?set the target Kubernetes API HTTPS endpoint}"
: "${CLOUD_AGENTS_TARGET_CREDENTIAL_REF:?set the mounted Kubernetes credential reference}"
: "${CLOUD_AGENTS_RELEASE_DIGEST:?set the Worker image release digest}"
: "${CLOUD_AGENTS_PROVIDER_SECRET_REF:?set the target Provider credential Secret name}"
: "${CLOUD_AGENTS_KUBECONFIG:?set the operator kubeconfig used only by this acceptance script}"
: "${CLOUD_AGENTS_KUBERNETES_NAMESPACE:?set the target Worker namespace}"
: "${CLOUD_AGENTS_E2E_OUTPUT_DIR:?set a new directory for non-secret E2E results}"

cloud_agentsctl=${CLOUD_AGENTSCTL-cloud-agentsctl}
kubectl=${KUBECTL-kubectl}
ca_file=${CLOUD_AGENTS_CA_FILE-}
target_name=${CLOUD_AGENTS_TARGET_NAME-$CLOUD_AGENTS_TARGET_ID}

if [ ! -f "$CLOUD_AGENTS_TOKEN_FILE" ] || [ ! -f "$CLOUD_AGENTS_KUBECONFIG" ] || [ -e "$CLOUD_AGENTS_E2E_OUTPUT_DIR" ] || [ "${#CLOUD_AGENTS_TARGET_ID}" -gt 90 ]; then
  echo "token file and kubeconfig must exist, target id must be at most 90 characters, and CLOUD_AGENTS_E2E_OUTPUT_DIR must be new" >&2
  exit 1
fi
command -v "$cloud_agentsctl" >/dev/null
command -v "$kubectl" >/dev/null
command -v node >/dev/null
mkdir -m 0700 "$CLOUD_AGENTS_E2E_OUTPUT_DIR"

run_id="kubernetes-e2e-$(date -u +%Y%m%d%H%M%S)-$$"
lease_id="$run_id-lease"
codex_session="$run_id-codex"
claude_session="$run_id-claude"
lease_created=

run_ctl() {
  if [ -n "$ca_file" ]; then
    "$cloud_agentsctl" --endpoint "$CLOUD_AGENTS_ENDPOINT" --ca-file "$ca_file" --token-file "$CLOUD_AGENTS_TOKEN_FILE" --tenant "$CLOUD_AGENTS_TENANT" "$@"
  else
    "$cloud_agentsctl" --endpoint "$CLOUD_AGENTS_ENDPOINT" --token-file "$CLOUD_AGENTS_TOKEN_FILE" --tenant "$CLOUD_AGENTS_TENANT" "$@"
  fi
}

run_kubectl() {
  "$kubectl" --kubeconfig "$CLOUD_AGENTS_KUBECONFIG" --namespace "$CLOUD_AGENTS_KUBERNETES_NAMESPACE" "$@"
}

terminate_on_failure() {
  if [ -n "$lease_created" ]; then
    run_ctl --timeout 3m --project "$CLOUD_AGENTS_PROJECT" --lease "$lease_id" \
      --request-id "$run_id-lease-terminate" --idempotency-key "$run_id-lease-terminate" \
      environment-lease terminate --generation 1 >/dev/null 2>&1 || true
  fi
}
trap terminate_on_failure EXIT HUP INT TERM

target_file="$CLOUD_AGENTS_E2E_OUTPUT_DIR/target.json"
run_ctl --project "$CLOUD_AGENTS_PROJECT" --target "$CLOUD_AGENTS_TARGET_ID" \
  --request-id "kubernetes-target-register-$CLOUD_AGENTS_TARGET_ID" \
  --idempotency-key "kubernetes-target-register-$CLOUD_AGENTS_TARGET_ID" \
  target register --target-name "$target_name" --kind kubernetes \
  --target-endpoint "$CLOUD_AGENTS_TARGET_ENDPOINT" --credential-ref "$CLOUD_AGENTS_TARGET_CREDENTIAL_REF" >"$target_file"
run_ctl --project "$CLOUD_AGENTS_PROJECT" --target "$CLOUD_AGENTS_TARGET_ID" \
  --request-id "$run_id-target-probe" --idempotency-key "$run_id-target-probe" \
  target probe --expected-generation 1 >"$CLOUD_AGENTS_E2E_OUTPUT_DIR/target-probe.json"
case "$(cat "$CLOUD_AGENTS_E2E_OUTPUT_DIR/target-probe.json")" in
  *'"targetKind":"kubernetes"'*'"observedPhase":"ready"'*) ;;
  *) echo "Kubernetes target probe did not become ready" >&2; exit 1 ;;
esac

lease_file="$CLOUD_AGENTS_E2E_OUTPUT_DIR/lease.json"
lease_created=1
run_ctl --timeout 3m --project "$CLOUD_AGENTS_PROJECT" --target "$CLOUD_AGENTS_TARGET_ID" --lease "$lease_id" \
  --request-id "$run_id-lease-create" --idempotency-key "$run_id-lease-create" \
  environment-lease create --name "$lease_id" --release-digest "$CLOUD_AGENTS_RELEASE_DIGEST" \
  --expected-target-generation 1 --provider-credential-ref "$CLOUD_AGENTS_PROVIDER_SECRET_REF" \
  --cpu-limit-millis 1000 --memory-limit-bytes 536870912 --ttl-seconds 3600 >"$lease_file"

case "$(cat "$lease_file")" in
  *'"observedPhase":"ready"'*'"cleanupPhase":"none"'*) ;;
  *) echo "Kubernetes target Worker did not become ready" >&2; exit 1 ;;
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

for session_id in "$codex_session" "$claude_session"; do
  run_ctl --project "$CLOUD_AGENTS_PROJECT" --session "$session_id" --request-id "$session_id-close" \
    --idempotency-key "$session_id-close" session close >/dev/null
done

terminate_file="$CLOUD_AGENTS_E2E_OUTPUT_DIR/lease-terminate.json"
run_ctl --timeout 3m --project "$CLOUD_AGENTS_PROJECT" --lease "$lease_id" \
  --request-id "$run_id-lease-terminate" --idempotency-key "$run_id-lease-terminate" \
  environment-lease terminate --generation 1 >"$terminate_file"
run_ctl --timeout 3m --project "$CLOUD_AGENTS_PROJECT" --lease "$lease_id" \
  --request-id "$run_id-lease-terminate" --idempotency-key "$run_id-lease-terminate" \
  environment-lease terminate --generation 1 >"$CLOUD_AGENTS_E2E_OUTPUT_DIR/lease-terminate-replay.json"
case "$(cat "$terminate_file")" in
  *'"desiredPhase":"terminated"'*'"observedPhase":"terminated"'*'"cleanupPhase":"complete"'*) ;;
  *) echo "Kubernetes target Lease did not terminate cleanly" >&2; exit 1 ;;
esac
cmp "$terminate_file" "$CLOUD_AGENTS_E2E_OUTPUT_DIR/lease-terminate-replay.json"
lease_created=

run_ctl --project "$CLOUD_AGENTS_PROJECT" --target "$CLOUD_AGENTS_TARGET_ID" \
  --request-id "$run_id-target-cleanup" --idempotency-key "$run_id-target-cleanup" \
  target cleanup --expected-generation 1 >"$CLOUD_AGENTS_E2E_OUTPUT_DIR/target-cleanup.json"
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
