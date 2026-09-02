#!/bin/sh

set -eu

: "${CLOUD_AGENTS_ENDPOINT:?set the public Control Plane HTTPS endpoint}"
: "${CLOUD_AGENTS_TOKEN_FILE:?set the Control Plane bearer token file}"
: "${CLOUD_AGENTS_TENANT:?set the tenant id}"
: "${CLOUD_AGENTS_PROJECT:?set the project id}"
: "${CLOUD_AGENTS_E2E_LEASE_ID:?set the ready Environment Lease id}"
: "${CLOUD_AGENTS_E2E_RUN_ID:?set the parent acceptance run id}"
: "${CLOUD_AGENTS_E2E_OUTPUT_DIR:?set the existing non-secret E2E result directory}"

cloud_agentsctl=${CLOUD_AGENTSCTL-cloud-agentsctl}
ca_file=${CLOUD_AGENTS_CA_FILE-}
approval_session="$CLOUD_AGENTS_E2E_RUN_ID-approval"
input_session="$CLOUD_AGENTS_E2E_RUN_ID-user-input"
active_sessions=
execute_pid=

if [ ! -f "$CLOUD_AGENTS_TOKEN_FILE" ] || [ ! -d "$CLOUD_AGENTS_E2E_OUTPUT_DIR" ]; then
  echo "token file and E2E output directory must exist" >&2
  exit 1
fi
command -v "$cloud_agentsctl" >/dev/null
command -v node >/dev/null

run_ctl() {
  if [ -n "$ca_file" ]; then
    "$cloud_agentsctl" --endpoint "$CLOUD_AGENTS_ENDPOINT" --ca-file "$ca_file" --token-file "$CLOUD_AGENTS_TOKEN_FILE" --tenant "$CLOUD_AGENTS_TENANT" "$@"
  else
    "$cloud_agentsctl" --endpoint "$CLOUD_AGENTS_ENDPOINT" --token-file "$CLOUD_AGENTS_TOKEN_FILE" --tenant "$CLOUD_AGENTS_TENANT" "$@"
  fi
}

cleanup() {
  if [ -n "$execute_pid" ]; then
    kill "$execute_pid" >/dev/null 2>&1 || true
    wait "$execute_pid" >/dev/null 2>&1 || true
  fi
  for session_id in $active_sessions; do
    run_ctl --project "$CLOUD_AGENTS_PROJECT" --session "$session_id" \
      --request-id "$session_id-close" --idempotency-key "$session_id-close" session close >/dev/null 2>&1 || true
  done
}
trap cleanup EXIT HUP INT TERM

create_turn() {
  provider=$1
  session_id=$2
  turn_id=$3
  prompt=$4
  run_ctl --project "$CLOUD_AGENTS_PROJECT" --lease "$CLOUD_AGENTS_E2E_LEASE_ID" --session "$session_id" \
    --request-id "$session_id-create" --idempotency-key "$session_id-create" session create --provider "$provider" >/dev/null
  active_sessions="$active_sessions $session_id"
  run_ctl --project "$CLOUD_AGENTS_PROJECT" --session "$session_id" --turn "$turn_id" \
    --request-id "$turn_id-create" --idempotency-key "$turn_id-create" turn create --input "$prompt" >/dev/null
}

start_execution() {
  session_id=$1
  turn_id=$2
  execution_id=$3
  runtime_mode=$4
  interaction_mode=$5
  prompt=$6
  final_file=$7
  run_ctl --timeout 5m --project "$CLOUD_AGENTS_PROJECT" --session "$session_id" --turn "$turn_id" --execution "$execution_id" \
    --request-id "$execution_id-run" --idempotency-key "$execution_id-run" execution execute \
    --runtime-mode "$runtime_mode" --interaction-mode "$interaction_mode" --input "$prompt" >"$final_file" 2>"$final_file.stderr" &
  execute_pid=$!
}

wait_for_interaction() {
  session_id=$1
  turn_id=$2
  execution_id=$3
  interaction_type=$4
  interaction_file=$5
  current_file="$interaction_file.current"
  attempt=0
  while [ "$attempt" -lt 90 ]; do
    attempt=$((attempt + 1))
    if run_ctl --project "$CLOUD_AGENTS_PROJECT" --session "$session_id" --turn "$turn_id" --execution "$execution_id" \
      --request-id "$execution_id-interaction-$attempt" execution get >"$current_file" 2>/dev/null; then
      CLOUD_AGENTS_E2E_EXECUTION_FILE="$current_file" CLOUD_AGENTS_E2E_INTERACTION_TYPE="$interaction_type" node <<'NODE' >"$interaction_file"
const { readFileSync } = require("node:fs");
const value = JSON.parse(readFileSync(process.env.CLOUD_AGENTS_E2E_EXECUTION_FILE, "utf8"));
const matches = (value.messages ?? []).filter((message) => message.messageType === "InteractionRequest" && message.payload?.interactionType === process.env.CLOUD_AGENTS_E2E_INTERACTION_TYPE);
if (matches.length > 1) throw new Error("multiple active interactions of the requested type");
if (matches.length === 0) {
  if (value.spec?.state !== "queued" && value.spec?.state !== "running") throw new Error(`execution became ${value.spec?.state} before interaction`);
} else {
  const requestId = matches[0].payload?.requestId;
  const questionId = matches[0].payload?.questions?.[0]?.id ?? "";
  if (!Number.isSafeInteger(value.spec?.generation) || value.spec.generation < 1 || typeof requestId !== "string" || requestId.length === 0 || (process.env.CLOUD_AGENTS_E2E_INTERACTION_TYPE === "user-input" && (typeof questionId !== "string" || questionId.length === 0))) throw new Error("interaction payload is invalid");
  process.stdout.write(JSON.stringify({ generation: value.spec.generation, requestId, questionId }));
}
NODE
      if [ -s "$interaction_file" ]; then
        rm -f "$current_file"
        return 0
      fi
    fi
    sleep 1
  done
  echo "timed out waiting for $interaction_type interaction" >&2
  return 1
}

interaction_field() {
  file=$1
  field=$2
  CLOUD_AGENTS_E2E_INTERACTION_FILE="$file" CLOUD_AGENTS_E2E_INTERACTION_FIELD="$field" node <<'NODE'
const { readFileSync } = require("node:fs");
const value = JSON.parse(readFileSync(process.env.CLOUD_AGENTS_E2E_INTERACTION_FILE, "utf8"));
const result = value[process.env.CLOUD_AGENTS_E2E_INTERACTION_FIELD];
if (typeof result !== "string" && !Number.isSafeInteger(result)) throw new Error("interaction field is invalid");
process.stdout.write(String(result));
NODE
}

wait_for_success() {
  final_file=$1
  if ! wait "$execute_pid"; then
    echo "interactive Provider execution failed" >&2
    return 1
  fi
  execute_pid=
  CLOUD_AGENTS_E2E_EXECUTION_FILE="$final_file" node <<'NODE'
const { readFileSync } = require("node:fs");
const value = JSON.parse(readFileSync(process.env.CLOUD_AGENTS_E2E_EXECUTION_FILE, "utf8"));
if (value.spec?.state !== "succeeded" || !value.messages?.some((message) => message.messageType === "Result")) throw new Error("interactive Provider execution did not succeed");
NODE
}

wait_for_running() {
  session_id=$1
  turn_id=$2
  execution_id=$3
  state_file=$4
  generation_file="$state_file.generation"
  attempt=0
  while [ "$attempt" -lt 90 ]; do
    attempt=$((attempt + 1))
    if run_ctl --project "$CLOUD_AGENTS_PROJECT" --session "$session_id" --turn "$turn_id" --execution "$execution_id" \
      --request-id "$execution_id-running-$attempt" execution get >"$state_file" 2>/dev/null; then
      CLOUD_AGENTS_E2E_EXECUTION_FILE="$state_file" node <<'NODE' >"$generation_file"
const { readFileSync } = require("node:fs");
const value = JSON.parse(readFileSync(process.env.CLOUD_AGENTS_E2E_EXECUTION_FILE, "utf8"));
if (value.spec?.state === "running") {
  if (!Number.isSafeInteger(value.spec?.generation) || value.spec.generation < 1) throw new Error("running execution generation is invalid");
  process.stdout.write(String(value.spec.generation));
} else if (value.spec?.state !== "queued") {
  throw new Error(`execution became ${value.spec?.state} before control action`);
}
NODE
      if [ -s "$generation_file" ]; then
        cat "$generation_file"
        return 0
      fi
    fi
    sleep 1
  done
  echo "timed out waiting for running execution" >&2
  return 1
}

run_controlled_stop() {
  action=$1
  session_id="$CLOUD_AGENTS_E2E_RUN_ID-$action"
  turn_id="$session_id-turn"
  execution_id="$session_id-execution"
  prompt="Use the shell tool now to run exactly: sleep 300. Do not finish or call another tool until that command completes."
  create_turn codex "$session_id" "$turn_id" "$prompt"
  background_file="$CLOUD_AGENTS_E2E_OUTPUT_DIR/$execution_id-background.json"
  state_file="$CLOUD_AGENTS_E2E_OUTPUT_DIR/$execution_id-running.json"
  action_file="$CLOUD_AGENTS_E2E_OUTPUT_DIR/$execution_id-$action.json"
  start_execution "$session_id" "$turn_id" "$execution_id" full-access default "$prompt" "$background_file"
  generation=$(wait_for_running "$session_id" "$turn_id" "$execution_id" "$state_file")
  sleep 2
  run_ctl --project "$CLOUD_AGENTS_PROJECT" --session "$session_id" --turn "$turn_id" --execution "$execution_id" \
    --request-id "$execution_id-$action" --idempotency-key "$execution_id-$action" \
    execution "$action" --generation "$generation" >"$action_file"
  if wait "$execute_pid"; then :; fi
  execute_pid=
  CLOUD_AGENTS_E2E_EXECUTION_FILE="$action_file" CLOUD_AGENTS_E2E_CONTROL_ACTION="$action" node <<'NODE'
const { readFileSync } = require("node:fs");
const value = JSON.parse(readFileSync(process.env.CLOUD_AGENTS_E2E_EXECUTION_FILE, "utf8"));
const expectedError = process.env.CLOUD_AGENTS_E2E_CONTROL_ACTION === "interrupt" ? "interrupted" : "cancelled";
if (value.spec?.state !== "cancelled" || value.spec?.errorCode !== expectedError) throw new Error(`${process.env.CLOUD_AGENTS_E2E_CONTROL_ACTION} terminal state changed`);
NODE
}

approval_turn="$approval_session-turn"
approval_execution="$approval_session-execution"
approval_path=".cloud-agents-acceptance/$CLOUD_AGENTS_E2E_RUN_ID-approved.txt"
approval_prompt="Use the Write tool to create exactly one file at $approval_path containing the single line 'approved interaction E2E' followed by a newline. Do not modify any other file. Wait for approval when requested, then reply done."
create_turn claudeAgent "$approval_session" "$approval_turn" "$approval_prompt"
approval_final="$CLOUD_AGENTS_E2E_OUTPUT_DIR/$approval_execution.json"
approval_interaction="$CLOUD_AGENTS_E2E_OUTPUT_DIR/$approval_execution-interaction.json"
start_execution "$approval_session" "$approval_turn" "$approval_execution" approval-required default "$approval_prompt" "$approval_final"
wait_for_interaction "$approval_session" "$approval_turn" "$approval_execution" approval "$approval_interaction"
run_ctl --project "$CLOUD_AGENTS_PROJECT" --session "$approval_session" --turn "$approval_turn" --execution "$approval_execution" \
  --request-id "$approval_execution-resolve" execution resolve-approval \
  --generation "$(interaction_field "$approval_interaction" generation)" \
  --interaction-request "$(interaction_field "$approval_interaction" requestId)" --decision accept >/dev/null
wait_for_success "$approval_final"
approval_artifact_index=$(CLOUD_AGENTS_E2E_EXECUTION_FILE="$approval_final" CLOUD_AGENTS_E2E_ARTIFACT_PATH="$approval_path" node <<'NODE'
const { readFileSync } = require("node:fs");
const value = JSON.parse(readFileSync(process.env.CLOUD_AGENTS_E2E_EXECUTION_FILE, "utf8"));
const indexes = (value.messages ?? []).flatMap((message, index) => {
  const artifact = message.payload?.artifact;
  return message.messageType === "ArtifactCandidate" && artifact?.sourceRoot === "workspace" && artifact?.path === process.env.CLOUD_AGENTS_E2E_ARTIFACT_PATH && typeof artifact?.kind === "string" && artifact.kind.replaceAll("_", "-") === "generated-file" ? [index] : [];
});
if (indexes.length !== 1) throw new Error("approval interaction did not produce one workspace ArtifactCandidate");
process.stdout.write(String(indexes[0]));
NODE
)
approval_artifact_file="$CLOUD_AGENTS_E2E_OUTPUT_DIR/$approval_execution-artifact.txt"
run_ctl --project "$CLOUD_AGENTS_PROJECT" --session "$approval_session" --turn "$approval_turn" --execution "$approval_execution" \
  --request-id "$approval_execution-artifact" execution download-artifact --message-index "$approval_artifact_index" >"$approval_artifact_file"
CLOUD_AGENTS_E2E_ARTIFACT_FILE="$approval_artifact_file" node <<'NODE'
const { readFileSync } = require("node:fs");
if (!readFileSync(process.env.CLOUD_AGENTS_E2E_ARTIFACT_FILE).equals(Buffer.from("approved interaction E2E\n"))) throw new Error("approval interaction Artifact content changed");
NODE

input_turn="$input_session-turn"
input_execution="$input_session-execution"
input_prompt="Before replying, call request_user_input exactly once with one non-secret question asking which environment to use and offer Staging as an option. After the answer, reply with the selected environment and do not call any other tool."
create_turn codex "$input_session" "$input_turn" "$input_prompt"
input_final="$CLOUD_AGENTS_E2E_OUTPUT_DIR/$input_execution.json"
input_interaction="$CLOUD_AGENTS_E2E_OUTPUT_DIR/$input_execution-interaction.json"
start_execution "$input_session" "$input_turn" "$input_execution" approval-required plan "$input_prompt" "$input_final"
wait_for_interaction "$input_session" "$input_turn" "$input_execution" user-input "$input_interaction"
question_id=$(interaction_field "$input_interaction" questionId)
answers_json=$(CLOUD_AGENTS_E2E_QUESTION_ID="$question_id" node -e 'process.stdout.write(JSON.stringify({[process.env.CLOUD_AGENTS_E2E_QUESTION_ID]:["Staging"]}))')
run_ctl --project "$CLOUD_AGENTS_PROJECT" --session "$input_session" --turn "$input_turn" --execution "$input_execution" \
  --request-id "$input_execution-resolve" execution resolve-user-input \
  --generation "$(interaction_field "$input_interaction" generation)" \
  --interaction-request "$(interaction_field "$input_interaction" requestId)" --answers-json "$answers_json" >/dev/null
wait_for_success "$input_final"

run_controlled_stop cancel
run_controlled_stop interrupt

cleanup
active_sessions=
trap - EXIT HUP INT TERM
printf '%s\n' "Agent approval and user-input real E2E passed"
