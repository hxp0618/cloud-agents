#!/bin/sh

set -eu

: "${CLOUD_AGENTS_ENDPOINT:?set the public Control Plane HTTPS endpoint}"
: "${CLOUD_AGENTS_TOKEN_FILE:?set the Control Plane bearer token file}"
: "${CLOUD_AGENTS_TENANT:?set the tenant id}"
: "${CLOUD_AGENTS_PROJECT:?set the project id}"
: "${CLOUD_AGENTS_TARGET_ID:?set a stable SSH deployment target id}"
: "${CLOUD_AGENTS_TARGET_ENDPOINT:?set the target endpoint as ssh://host[:port]}"
: "${CLOUD_AGENTS_TARGET_CREDENTIAL_REF:?set the mounted SSH credential reference}"
: "${CLOUD_AGENTS_RELEASE_DIGEST:?set the Worker image release digest}"
: "${CLOUD_AGENTS_PROVIDER_VOLUME_REF:?set the target Provider credential volume name}"
: "${CLOUD_AGENTS_SSH_HOST:?set the operator SSH host matching the target endpoint}"
: "${CLOUD_AGENTS_SSH_USER:?set the operator SSH user}"
: "${CLOUD_AGENTS_SSH_IDENTITY_FILE:?set the isolated operator SSH private key file}"
: "${CLOUD_AGENTS_SSH_KNOWN_HOSTS_FILE:?set the operator pinned known_hosts file}"
: "${CLOUD_AGENTS_E2E_OUTPUT_DIR:?set a new directory for non-secret E2E results}"

cloud_agentsctl=${CLOUD_AGENTSCTL-cloud-agentsctl}
ssh_client=${SSH-ssh}
ca_file=${CLOUD_AGENTS_CA_FILE-}
ssh_port=${CLOUD_AGENTS_SSH_PORT-22}
target_name=${CLOUD_AGENTS_TARGET_NAME-$CLOUD_AGENTS_TARGET_ID}
script_directory=$(CDPATH= cd "$(dirname "$0")" && pwd)

if [ ! -f "$CLOUD_AGENTS_TOKEN_FILE" ] || [ ! -f "$CLOUD_AGENTS_SSH_IDENTITY_FILE" ] || [ ! -f "$CLOUD_AGENTS_SSH_KNOWN_HOSTS_FILE" ] || [ -e "$CLOUD_AGENTS_E2E_OUTPUT_DIR" ]; then
  echo "token, SSH identity, and known_hosts files must exist, and CLOUD_AGENTS_E2E_OUTPUT_DIR must be new" >&2
  exit 1
fi
command -v "$cloud_agentsctl" >/dev/null
command -v "$ssh_client" >/dev/null
command -v node >/dev/null
CLOUD_AGENTS_E2E_TARGET_ENDPOINT="$CLOUD_AGENTS_TARGET_ENDPOINT" CLOUD_AGENTS_E2E_SSH_HOST="$CLOUD_AGENTS_SSH_HOST" CLOUD_AGENTS_E2E_SSH_PORT="$ssh_port" node <<'NODE'
const endpoint = new URL(process.env.CLOUD_AGENTS_E2E_TARGET_ENDPOINT);
const host = endpoint.hostname.replace(/^\[|\]$/g, "");
const port = endpoint.port || "22";
if (endpoint.protocol !== "ssh:" || endpoint.username || endpoint.password || endpoint.pathname || endpoint.search || endpoint.hash || host.toLowerCase() !== process.env.CLOUD_AGENTS_E2E_SSH_HOST.toLowerCase() || port !== process.env.CLOUD_AGENTS_E2E_SSH_PORT || !/^[A-Za-z0-9._:-]+$/.test(host) || !/^[1-9][0-9]{0,4}$/.test(port) || Number(port) > 65535) {
  throw new Error("operator SSH host/port does not exactly match the target endpoint");
}
NODE
case "$CLOUD_AGENTS_SSH_USER" in
  *[!A-Za-z0-9._-]*|'') echo "CLOUD_AGENTS_SSH_USER is invalid" >&2; exit 1 ;;
esac
for identifier in "$CLOUD_AGENTS_TENANT" "$CLOUD_AGENTS_PROJECT" "$CLOUD_AGENTS_TARGET_ID"; do
  case "$identifier" in
    *[!A-Za-z0-9._-]*|'') echo "tenant, project, and target ids must be shell-safe identifiers" >&2; exit 1 ;;
  esac
done
mkdir -m 0700 "$CLOUD_AGENTS_E2E_OUTPUT_DIR"

run_id="ssh-e2e-$(date -u +%Y%m%d%H%M%S)-$$"
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

run_ssh() {
  "$ssh_client" -o BatchMode=yes -o IdentitiesOnly=yes -o StrictHostKeyChecking=yes \
    -o "UserKnownHostsFile=$CLOUD_AGENTS_SSH_KNOWN_HOSTS_FILE" -o ConnectTimeout=10 \
    -i "$CLOUD_AGENTS_SSH_IDENTITY_FILE" -p "$ssh_port" -l "$CLOUD_AGENTS_SSH_USER" "$CLOUD_AGENTS_SSH_HOST" "$@"
}

create_lease() {
  run_ctl --timeout 3m --project "$CLOUD_AGENTS_PROJECT" --target "$CLOUD_AGENTS_TARGET_ID" --lease "$lease_id" \
    --request-id "$run_id-lease-create" --idempotency-key "$run_id-lease-create" \
    environment-lease create --name "$lease_id" --release-digest "$CLOUD_AGENTS_RELEASE_DIGEST" \
    --expected-target-generation 1 --provider-credential-ref "$CLOUD_AGENTS_PROVIDER_VOLUME_REF" \
    --cpu-limit-millis 1000 --memory-limit-bytes 536870912 --ttl-seconds 3600
}

terminate_lease() {
  run_ctl --timeout 3m --project "$CLOUD_AGENTS_PROJECT" --lease "$lease_id" \
    --request-id "$run_id-lease-terminate" --idempotency-key "$run_id-lease-terminate" \
    environment-lease terminate --generation 1
}

list_remote_workers() {
  run_ssh docker ps -a --format '{{.Names}}' \
    --filter "label=cloud-agents.dev/managed=true" \
    --filter "label=cloud-agents.dev/tenant=$CLOUD_AGENTS_TENANT" \
    --filter "label=cloud-agents.dev/project=$CLOUD_AGENTS_PROJECT" \
    --filter "label=cloud-agents.dev/target=$CLOUD_AGENTS_TARGET_ID" \
    --filter "label=cloud-agents.dev/lease=$lease_id"
}

terminate_on_failure() {
  if [ -n "$lease_created" ]; then
    terminate_lease >/dev/null 2>&1 || true
  fi
}
trap terminate_on_failure EXIT HUP INT TERM

run_ctl --project "$CLOUD_AGENTS_PROJECT" --target "$CLOUD_AGENTS_TARGET_ID" \
  --request-id "ssh-target-register-$CLOUD_AGENTS_TARGET_ID" \
  --idempotency-key "ssh-target-register-$CLOUD_AGENTS_TARGET_ID" \
  target register --target-name "$target_name" --kind ssh \
  --target-endpoint "$CLOUD_AGENTS_TARGET_ENDPOINT" --credential-ref "$CLOUD_AGENTS_TARGET_CREDENTIAL_REF" >"$CLOUD_AGENTS_E2E_OUTPUT_DIR/target.json"
run_ctl --project "$CLOUD_AGENTS_PROJECT" --target "$CLOUD_AGENTS_TARGET_ID" \
  --request-id "$run_id-target-probe" --idempotency-key "$run_id-target-probe" \
  target probe --expected-generation 1 >"$CLOUD_AGENTS_E2E_OUTPUT_DIR/target-probe.json"
case "$(cat "$CLOUD_AGENTS_E2E_OUTPUT_DIR/target-probe.json")" in
  *'"targetKind":"ssh"'*'"observedPhase":"ready"'*) ;;
  *) echo "SSH target probe did not become ready" >&2; exit 1 ;;
esac
run_ctl --project "$CLOUD_AGENTS_PROJECT" --target "$CLOUD_AGENTS_TARGET_ID" \
  --request-id "$run_id-target-status" target get >"$CLOUD_AGENTS_E2E_OUTPUT_DIR/target-status.json"
case "$(cat "$CLOUD_AGENTS_E2E_OUTPUT_DIR/target-status.json")" in
  *'"targetKind":"ssh"'*'"observedPhase":"ready"'*) ;;
  *) echo "SSH target ready status was not persisted" >&2; exit 1 ;;
esac

lease_created=1
create_lease >"$CLOUD_AGENTS_E2E_OUTPUT_DIR/lease.json"
create_lease >"$CLOUD_AGENTS_E2E_OUTPUT_DIR/lease-replay.json"
cmp "$CLOUD_AGENTS_E2E_OUTPUT_DIR/lease.json" "$CLOUD_AGENTS_E2E_OUTPUT_DIR/lease-replay.json"
case "$(cat "$CLOUD_AGENTS_E2E_OUTPUT_DIR/lease.json")" in
  *'"observedPhase":"ready"'*'"cleanupPhase":"none"'*) ;;
  *) echo "SSH target Worker did not become ready" >&2; exit 1 ;;
esac
run_ctl --project "$CLOUD_AGENTS_PROJECT" --lease "$lease_id" \
  --request-id "$run_id-lease-status" environment-lease get >"$CLOUD_AGENTS_E2E_OUTPUT_DIR/lease-status.json"
case "$(cat "$CLOUD_AGENTS_E2E_OUTPUT_DIR/lease-status.json")" in
  *'"observedPhase":"ready"'*'"cleanupPhase":"none"'*'"targetId":"'"$CLOUD_AGENTS_TARGET_ID"'"'*) ;;
  *) echo "SSH target Lease ready status was not persisted" >&2; exit 1 ;;
esac

workers_file="$CLOUD_AGENTS_E2E_OUTPUT_DIR/remote-workers.txt"
list_remote_workers >"$workers_file"
worker_name=$(CLOUD_AGENTS_E2E_WORKERS_FILE="$workers_file" node <<'NODE'
const { readFileSync } = require("node:fs");
const names = readFileSync(process.env.CLOUD_AGENTS_E2E_WORKERS_FILE, "utf8").split(/\r?\n/).filter(Boolean);
if (names.length !== 1 || !/^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$/.test(names[0])) throw new Error(`expected one managed SSH Worker, got ${names.length}`);
process.stdout.write(names[0]);
NODE
)
if [ "$(run_ssh docker inspect --format '{{.State.Running}}' -- "$worker_name")" != true ] || [ "$(run_ssh docker inspect --format '{{.HostConfig.RestartPolicy.Name}}' -- "$worker_name")" != unless-stopped ]; then
  echo "SSH target Worker state or restart policy is invalid" >&2
  exit 1
fi

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
  expected_content="cloud-agents SSH target $provider $phase real E2E"
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
if (value.spec?.state !== "succeeded" || !value.messages?.some((message) => message.messageType === "Result")) throw new Error("real Provider execution did not succeed");
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
  events_file="$CLOUD_AGENTS_E2E_OUTPUT_DIR/$execution_id-events.jsonl"
  run_ctl --timeout 1m --project "$CLOUD_AGENTS_PROJECT" --session "$session_id" --execution "$execution_id" \
    --request-id "$execution_id-events" events watch --limit 64 --until-terminal >"$events_file"
  if ! grep -q '"operation":"execution.complete"' "$events_file"; then
    echo "real $provider event stream did not reach execution.complete" >&2
    exit 1
  fi
}

create_session codex "$codex_session"
run_real_turn codex "$codex_session" before-restart
sleep 10
restart_state=$(run_ssh docker inspect --format '{{.RestartCount}} {{.State.Pid}}' -- "$worker_name")
set -- $restart_state
if [ "$#" -ne 2 ]; then
  echo "SSH target Worker returned an invalid restart state" >&2
  exit 1
fi
restart_count_before=$1
worker_pid_before=$2
case "$restart_count_before$worker_pid_before" in
  *[!0-9]*|'') echo "SSH target Worker returned an invalid restart state" >&2; exit 1 ;;
esac
run_ssh "docker exec $worker_name /bin/sh -c 'kill -QUIT 1'" >"$CLOUD_AGENTS_E2E_OUTPUT_DIR/worker-crash.txt" 2>&1 || true
attempt=0
worker_restarted=
while [ -z "$worker_restarted" ]; do
  attempt=$((attempt + 1))
  if [ "$attempt" -ge 30 ]; then
    echo "SSH target Worker did not recover from its process crash" >&2
    exit 1
  fi
  if restart_state=$(run_ssh docker inspect --format '{{.State.Running}} {{.RestartCount}} {{.State.Pid}}' -- "$worker_name" 2>/dev/null); then
    set -- $restart_state
    if [ "$#" -eq 3 ] && [ "$1" = true ]; then
      case "$2$3" in
        *[!0-9]*|'') ;;
        *)
          if [ "$2" -gt "$restart_count_before" ] && [ "$3" -ne "$worker_pid_before" ]; then
            worker_restarted=1
          fi
          ;;
      esac
    fi
  fi
  sleep 2
done
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

terminate_lease >"$CLOUD_AGENTS_E2E_OUTPUT_DIR/lease-terminate.json"
terminate_lease >"$CLOUD_AGENTS_E2E_OUTPUT_DIR/lease-terminate-replay.json"
cmp "$CLOUD_AGENTS_E2E_OUTPUT_DIR/lease-terminate.json" "$CLOUD_AGENTS_E2E_OUTPUT_DIR/lease-terminate-replay.json"
case "$(cat "$CLOUD_AGENTS_E2E_OUTPUT_DIR/lease-terminate.json")" in
  *'"desiredPhase":"terminated"'*'"observedPhase":"terminated"'*'"cleanupPhase":"complete"'*) ;;
  *) echo "SSH target Lease did not terminate cleanly" >&2; exit 1 ;;
esac
lease_created=

run_ctl --project "$CLOUD_AGENTS_PROJECT" --target "$CLOUD_AGENTS_TARGET_ID" \
  --request-id "$run_id-target-cleanup" --idempotency-key "$run_id-target-cleanup" \
  target cleanup --expected-generation 1 >"$CLOUD_AGENTS_E2E_OUTPUT_DIR/target-cleanup.json"
list_remote_workers >"$CLOUD_AGENTS_E2E_OUTPUT_DIR/remote-workers-final.txt"
if [ -s "$CLOUD_AGENTS_E2E_OUTPUT_DIR/remote-workers-final.txt" ]; then
  echo "SSH target retained the terminated Worker" >&2
  exit 1
fi

printf '%s\n' "SSH target real E2E passed; results: $CLOUD_AGENTS_E2E_OUTPUT_DIR"
