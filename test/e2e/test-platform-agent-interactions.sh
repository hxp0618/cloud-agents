#!/bin/sh

set -eu

: "${CLOUD_AGENTS_ENDPOINT:?set the public Control Plane HTTPS endpoint}"
: "${CLOUD_AGENTS_TOKEN_FILE:?set the Control Plane bearer token file}"
: "${CLOUD_AGENTS_TENANT:?set the tenant id}"
: "${CLOUD_AGENTS_PROJECT:?set the project id}"
: "${CLOUD_AGENTS_E2E_RUN_ID:?set the parent acceptance run id}"
: "${CLOUD_AGENTS_E2E_OUTPUT_DIR:?set the existing non-secret E2E result directory}"

lease_id=${CLOUD_AGENTS_E2E_LEASE_ID-}
workspace_id=${CLOUD_AGENTS_E2E_WORKSPACE_ID-}
sandbox_id=${CLOUD_AGENTS_E2E_SANDBOX_ID-}
sandbox_generation=${CLOUD_AGENTS_E2E_SANDBOX_GENERATION-}
environment_profile_id=${CLOUD_AGENTS_E2E_ENVIRONMENT_PROFILE_ID-}
environment_profile_version=${CLOUD_AGENTS_E2E_ENVIRONMENT_PROFILE_VERSION-1}
if [ -n "$lease_id" ]; then
  if [ -n "$workspace_id" ] || [ -n "$sandbox_id" ] || [ -n "$sandbox_generation" ] || [ -n "$environment_profile_id" ]; then
    echo "set either CLOUD_AGENTS_E2E_LEASE_ID or direct Sandbox binding" >&2
    exit 1
  fi
elif [ -z "$workspace_id" ] || [ -z "$sandbox_id" ] || [ -z "$sandbox_generation" ] || [ -z "$environment_profile_id" ]; then
  echo "set CLOUD_AGENTS_E2E_LEASE_ID or complete direct Sandbox binding" >&2
  exit 1
fi

cloud_agentsctl=${CLOUD_AGENTSCTL-cloud-agentsctl}
ca_file=${CLOUD_AGENTS_CA_FILE-}
admin_token_file=${CLOUD_AGENTS_E2E_ADMIN_TOKEN_FILE-}
admin_curl_config=${CLOUD_AGENTS_E2E_ADMIN_CURL_CONFIG-}
approval_session="$CLOUD_AGENTS_E2E_RUN_ID-approval"
input_session="$CLOUD_AGENTS_E2E_RUN_ID-user-input"
active_sessions=
execute_pid=
takeover_generation=
takeover_checkpoint_digest=
takeover_needs_reconcile=0
execution_retry_suffix=
recovery_provider=${CLOUD_AGENTS_E2E_RECOVERY_PROVIDER-codex}
recovery_fault=${CLOUD_AGENTS_E2E_RECOVERY_FAULT-both}
recovery_environment=${CLOUD_AGENTS_E2E_ENVIRONMENT-docker}
recovery_mcp_refs_json=${CLOUD_AGENTS_E2E_MCP_SERVER_REFS_JSON-}
recovery_skill_refs_json=${CLOUD_AGENTS_E2E_SKILL_BUNDLE_REFS_JSON-}
recovery_checkpoint_mode=${CLOUD_AGENTS_E2E_RECOVERY_CHECKPOINT_MODE-interaction}
recovery_artifact_path=${CLOUD_AGENTS_E2E_RECOVERY_ARTIFACT_PATH-}
recovery_expected_content=${CLOUD_AGENTS_E2E_RECOVERY_EXPECTED_CONTENT-}
recovery_bash_command=${CLOUD_AGENTS_E2E_RECOVERY_BASH_COMMAND-}
recovery_mcp_tool_name=${CLOUD_AGENTS_E2E_RECOVERY_MCP_TOOL_NAME-}
recovery_skill_name=${CLOUD_AGENTS_E2E_RECOVERY_SKILL_NAME-managed-capability-acceptance}
execution_timeout=${CLOUD_AGENTS_E2E_EXECUTION_TIMEOUT-5m}
recovery_capability_bound=0
recovery_grant_id=
case "$recovery_provider" in
  codex | claudeAgent | pi | deepseek-harness) ;;
  *) echo "CLOUD_AGENTS_E2E_RECOVERY_PROVIDER is invalid" >&2; exit 1 ;;
esac
case "$recovery_fault" in
  worker | agent | both) ;;
  *) echo "CLOUD_AGENTS_E2E_RECOVERY_FAULT is invalid" >&2; exit 1 ;;
esac
case "$recovery_environment" in
  docker | remote-worker | kubernetes) ;;
  *) echo "CLOUD_AGENTS_E2E_ENVIRONMENT is invalid" >&2; exit 1 ;;
esac
case "$recovery_checkpoint_mode" in
  interaction | side-effect) ;;
  *) echo "CLOUD_AGENTS_E2E_RECOVERY_CHECKPOINT_MODE is invalid" >&2; exit 1 ;;
esac
if [ "$recovery_checkpoint_mode" = side-effect ]; then
  [ "$recovery_provider" = pi ] || [ "$recovery_provider" = deepseek-harness ] || {
    echo "side-effect checkpoint recovery is reserved for Pi and deepseek-harness" >&2
    exit 1
  }
  [ -n "$recovery_artifact_path" ] && [ -n "$recovery_expected_content" ] && [ -n "$recovery_bash_command" ] && [ -n "$recovery_mcp_tool_name" ] || {
    echo "side-effect checkpoint recovery requires artifact, command and managed MCP inputs" >&2
    exit 1
  }
  recovery_artifact_name=${recovery_artifact_path#".cloud-agents-stage3-acceptance/"}
  case "$recovery_artifact_path:$recovery_artifact_name" in
    .cloud-agents-stage3-acceptance/*:*.txt)
      case "$recovery_artifact_name" in
        '' | */* | *[!A-Za-z0-9_.-]*) echo "side-effect recovery artifact path is outside the acceptance workspace" >&2; exit 1 ;;
      esac
      ;;
    *) echo "side-effect recovery artifact path is outside the acceptance workspace" >&2; exit 1 ;;
  esac
  case "$recovery_mcp_tool_name" in
    mcp__[A-Za-z0-9_-]*__acceptance_marker | mcp__[A-Za-z0-9_-]*__acceptance_marker__[a-f0-9][a-f0-9][a-f0-9][a-f0-9][a-f0-9][a-f0-9][a-f0-9][a-f0-9]) ;;
    *) echo "side-effect recovery MCP tool name is invalid" >&2; exit 1 ;;
  esac
fi
if [ -n "$recovery_mcp_refs_json" ] || [ -n "$recovery_skill_refs_json" ]; then
  [ -n "$recovery_mcp_refs_json" ] && [ -n "$recovery_skill_refs_json" ] || {
    echo "capability-bound recovery requires both MCP Server and Skill Bundle refs" >&2
    exit 1
  }
  recovery_capability_bound=1
fi

if [ ! -f "$CLOUD_AGENTS_TOKEN_FILE" ] || [ ! -d "$CLOUD_AGENTS_E2E_OUTPUT_DIR" ]; then
  echo "token file and E2E output directory must exist" >&2
  exit 1
fi
command -v "$cloud_agentsctl" >/dev/null
command -v node >/dev/null
if [ "$recovery_capability_bound" -eq 1 ]; then
  CLOUD_AGENTS_E2E_MCP_SERVER_REFS_JSON="$recovery_mcp_refs_json" \
  CLOUD_AGENTS_E2E_SKILL_BUNDLE_REFS_JSON="$recovery_skill_refs_json" node <<'NODE'
for (const [name, kind, id] of [
  ["CLOUD_AGENTS_E2E_MCP_SERVER_REFS_JSON", "serverId", "mcp"],
  ["CLOUD_AGENTS_E2E_SKILL_BUNDLE_REFS_JSON", "bundleId", "skill"],
]) {
  const value = JSON.parse(process.env[name]);
  if (!Array.isArray(value) || value.length !== 1 || typeof value[0]?.[kind] !== "string") {
    throw new Error(`${id} recovery refs must contain exactly one resource`);
  }
}
NODE
fi
if [ -n "${CLOUD_AGENTS_E2E_CONTROL_PLANE_CONTAINER-}" ]; then
  command -v docker >/dev/null
  command -v cmp >/dev/null
  command -v curl >/dev/null
  printf '%s\n' "$CLOUD_AGENTS_E2E_CONTROL_PLANE_CONTAINER" | grep -Eq '^[0-9a-f]{64}$' || {
    echo "Control Plane container must be an exact Docker container id" >&2
    exit 1
  }
fi
auth_config_file=${CLOUD_AGENTS_E2E_AUTH_CONFIG-}
auth_test_private_key_file=${CLOUD_AGENTS_E2E_AUTH_TEST_PRIVATE_KEY-}
if [ -n "${CLOUD_AGENTS_E2E_CONTROL_PLANE_CONTAINER-}" ]; then
  [ -f "$auth_config_file" ] || { echo "auth config file is required for interaction authorization checks" >&2; exit 1; }
  [ -f "$auth_test_private_key_file" ] || { echo "auth test private key is required for interaction authorization checks" >&2; exit 1; }
fi
if [ -n "${CLOUD_AGENTS_E2E_WORKER_CONTAINER-}" ]; then
  command -v docker >/dev/null
  printf '%s\n' "$CLOUD_AGENTS_E2E_WORKER_CONTAINER" | grep -Eq '^[0-9a-f]{64}$' || {
    echo "Worker container must be an exact Docker container id" >&2
    exit 1
  }
fi
if [ -n "${CLOUD_AGENTS_E2E_AGENT_RUNTIME_ID-}" ]; then
  if [ "$recovery_environment" = docker ]; then command -v docker >/dev/null; fi
  printf '%s\n' "$CLOUD_AGENTS_E2E_AGENT_RUNTIME_ID" | grep -Eq '^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$' || {
    echo "Agent Runtime id is invalid" >&2
    exit 1
  }
fi
if [ -n "${CLOUD_AGENTS_E2E_AGENT_TARGET_ID-}" ]; then
  printf '%s\n' "$CLOUD_AGENTS_E2E_AGENT_TARGET_ID" | grep -Eq '^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$' || {
    echo "Agent Runtime target id is invalid" >&2
    exit 1
  }
fi
if [ -n "${CLOUD_AGENTS_E2E_POSTGRES_CONTAINER-}" ]; then
  printf '%s\n' "$CLOUD_AGENTS_E2E_POSTGRES_CONTAINER" | grep -Eq '^[0-9a-f]{64}$' || {
    echo "Postgres container must be an exact Docker container id" >&2
    exit 1
  }
fi

run_ctl() {
  if [ -n "$ca_file" ]; then
    "$cloud_agentsctl" --endpoint "$CLOUD_AGENTS_ENDPOINT" --ca-file "$ca_file" --token-file "$CLOUD_AGENTS_TOKEN_FILE" --tenant "$CLOUD_AGENTS_TENANT" "$@"
  else
    "$cloud_agentsctl" --endpoint "$CLOUD_AGENTS_ENDPOINT" --token-file "$CLOUD_AGENTS_TOKEN_FILE" --tenant "$CLOUD_AGENTS_TENANT" "$@"
  fi
}

bounded_request_id() {
  value=$1
  if [ "${#value}" -le 128 ]; then
    printf '%s' "$value"
    return 0
  fi
  CLOUD_AGENTS_REQUEST_ID="$value" node -e 'const crypto=require("node:crypto");process.stdout.write(`req-${crypto.createHash("sha256").update(process.env.CLOUD_AGENTS_REQUEST_ID).digest("hex").slice(0,48)}`)'
}

run_admin_ctl() {
  [ -n "$admin_token_file" ] || return 1
  if [ -n "$ca_file" ]; then
    "$cloud_agentsctl" --endpoint "$CLOUD_AGENTS_ENDPOINT" --ca-file "$ca_file" --token-file "$admin_token_file" --tenant "$CLOUD_AGENTS_TENANT" "$@"
  else
    "$cloud_agentsctl" --endpoint "$CLOUD_AGENTS_ENDPOINT" --token-file "$admin_token_file" --tenant "$CLOUD_AGENTS_TENANT" "$@"
  fi
}

cleanup_complete=0
cleanup() {
  if [ "${cleanup_complete:-0}" -eq 1 ]; then
    return 0
  fi
  cleanup_complete=1
  if [ -n "$execute_pid" ]; then
    kill "$execute_pid" >/dev/null 2>&1 || true
    wait "$execute_pid" >/dev/null 2>&1 || true
  fi
  for session_id in $active_sessions; do
    run_ctl --project "$CLOUD_AGENTS_PROJECT" --session "$session_id" \
      --request-id "$(bounded_request_id "$session_id-close")" --idempotency-key "$(bounded_request_id "$session_id-close")" session close >/dev/null 2>&1 || true
  done
}
finish_interactions() {
  interaction_status=$1
  interaction_failed_phase=$interaction_phase
  trap - EXIT HUP INT TERM
  interaction_phase=cleanup
  cleanup
  if [ "$interaction_status" -ne 0 ]; then
    printf 'AGENT_INTERACTIONS_FAILURE phase=%s exit_status=%s\n' \
      "$interaction_failed_phase" "$interaction_status" >&2
  fi
  exit "$interaction_status"
}
interaction_phase=approval
trap 'finish_interactions $?' EXIT
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM

create_turn() {
  provider=$1
  session_id=$2
  turn_id=$3
  prompt=$4
  if [ -n "$lease_id" ]; then
    run_ctl --project "$CLOUD_AGENTS_PROJECT" --lease "$lease_id" --session "$session_id" \
    --request-id "$(bounded_request_id "$session_id-create")" --idempotency-key "$(bounded_request_id "$session_id-create")" session create --provider "$provider" >/dev/null
  else
    run_ctl --project "$CLOUD_AGENTS_PROJECT" --session "$session_id" \
      --request-id "$(bounded_request_id "$session_id-create")" --idempotency-key "$(bounded_request_id "$session_id-create")" \
      session create --provider "$provider" --workspace "$workspace_id" --sandbox "$sandbox_id" \
      --sandbox-generation "$sandbox_generation" --environment-profile "$environment_profile_id" \
      --environment-profile-version "$environment_profile_version" >/dev/null
  fi
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
  capability_bound=${8-0}
  set --
  if [ "$capability_bound" -eq 1 ]; then
    set -- --mcp-server-refs-json "$recovery_mcp_refs_json" --skill-bundle-refs-json "$recovery_skill_refs_json"
  fi
  execution_retry_request_id=$(bounded_request_id "$execution_id-run$execution_retry_suffix")
  execution_retry_idempotency_key=$(bounded_request_id "$execution_id-run")
  run_ctl --timeout "$execution_timeout" --project "$CLOUD_AGENTS_PROJECT" --session "$session_id" --turn "$turn_id" --execution "$execution_id" \
    --request-id "$execution_retry_request_id" --idempotency-key "$execution_retry_idempotency_key" execution execute \
    --runtime-mode "$runtime_mode" --interaction-mode "$interaction_mode" "$@" --input "$prompt" >"$final_file" 2>"$final_file.stderr" &
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
      --request-id "$(bounded_request_id "$execution_id-interaction-$attempt")" execution get >"$current_file" 2>/dev/null; then
      CLOUD_AGENTS_E2E_EXECUTION_FILE="$current_file" CLOUD_AGENTS_E2E_INTERACTION_TYPE="$interaction_type" node <<'NODE' >"$interaction_file"
const { readFileSync } = require("node:fs");
const value = JSON.parse(readFileSync(process.env.CLOUD_AGENTS_E2E_EXECUTION_FILE, "utf8"));
const matches = (value.messages ?? []).filter((message) => message.messageType === "InteractionRequest" && message.payload?.interactionType === process.env.CLOUD_AGENTS_E2E_INTERACTION_TYPE);
const uniqueMatches = [...new Map(matches.map((message) => [message.payload?.requestId, message])).values()];
if (uniqueMatches.length > 1) throw new Error("multiple active interactions of the requested type");
if (uniqueMatches.length === 0) {
  if (value.spec?.state !== "queued" && value.spec?.state !== "running") throw new Error(`execution became ${value.spec?.state} before interaction`);
} else {
  const requestId = uniqueMatches[0].payload?.requestId;
  const questionId = uniqueMatches[0].payload?.questions?.[0]?.id ?? "";
  if (!Number.isSafeInteger(value.spec?.generation) || value.spec.generation < 1 || typeof requestId !== "string" || requestId.length === 0 || (process.env.CLOUD_AGENTS_E2E_INTERACTION_TYPE === "user-input" && (typeof questionId !== "string" || questionId.length === 0))) throw new Error("interaction payload is invalid");
  if (value.spec?.checkpoint?.sequence >= 1 && value.spec.checkpoint.pendingInteractionCount >= 1) {
    process.stdout.write(JSON.stringify({ generation: value.spec.generation, requestId, questionId }));
  }
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

expect_stale_approval_rejected() {
  session_id=$1
  turn_id=$2
  execution_id=$3
  interaction_file=$4
  generation=$(interaction_field "$interaction_file" generation)
  interaction_request=$(interaction_field "$interaction_file" requestId)
  set +e
  run_ctl --project "$CLOUD_AGENTS_PROJECT" --session "$session_id" --turn "$turn_id" --execution "$execution_id" \
    --request-id "$execution_id-stale-approval" execution resolve-approval \
    --generation "$((generation + 1))" --interaction-request "$interaction_request" --decision decline \
    >"$CLOUD_AGENTS_E2E_OUTPUT_DIR/$execution_id-stale-approval.json" \
    2>"$CLOUD_AGENTS_E2E_OUTPUT_DIR/$execution_id-stale-approval.stderr"
  status=$?
  set -e
  if [ "$status" -eq 0 ]; then
    echo "stale Approval generation was accepted" >&2
    return 1
  fi
  printf 'stale_interaction_negative=passed type=approval\n'
}

expect_stale_user_input_rejected() {
  session_id=$1
  turn_id=$2
  execution_id=$3
  interaction_file=$4
  generation=$(interaction_field "$interaction_file" generation)
  interaction_request=$(interaction_field "$interaction_file" requestId)
  question_id=$(interaction_field "$interaction_file" questionId)
  answers_json=$(CLOUD_AGENTS_E2E_QUESTION_ID="$question_id" node -e 'process.stdout.write(JSON.stringify({[process.env.CLOUD_AGENTS_E2E_QUESTION_ID]:["Staging"]}))')
  set +e
  run_ctl --project "$CLOUD_AGENTS_PROJECT" --session "$session_id" --turn "$turn_id" --execution "$execution_id" \
    --request-id "$execution_id-stale-user-input" execution resolve-user-input \
    --generation "$((generation + 1))" --interaction-request "$interaction_request" --answers-json "$answers_json" \
    >"$CLOUD_AGENTS_E2E_OUTPUT_DIR/$execution_id-stale-user-input.json" \
    2>"$CLOUD_AGENTS_E2E_OUTPUT_DIR/$execution_id-stale-user-input.stderr"
  status=$?
  set -e
  if [ "$status" -eq 0 ]; then
    echo "stale User Input generation was accepted" >&2
    return 1
  fi
  printf 'stale_interaction_negative=passed type=user-input\n'
}
expect_cross_tenant_interaction_rejected() {
  session_id=$1
  turn_id=$2
  execution_id=$3
  interaction_type=$4
  interaction_file=$5
  [ -n "$ca_file" ] || { echo "cross-tenant interaction check requires CLOUD_AGENTS_CA_FILE" >&2; return 1; }
  command -v curl >/dev/null || { echo "cross-tenant interaction check requires curl" >&2; return 1; }
  generation=$(interaction_field "$interaction_file" generation)
  interaction_request=$(interaction_field "$interaction_file" requestId)
  if [ "$interaction_type" = approval ]; then
    body=$(printf '{"generation":%s,"requestId":"%s","decision":"decline"}' "$generation" "$interaction_request")
    action=resolveApproval
  else
    question_id=$(interaction_field "$interaction_file" questionId)
    answers_json=$(node -e 'process.stdout.write(JSON.stringify({[process.argv[1]]:["Staging"]}))' "$question_id")
    body=$(printf '{"generation":%s,"requestId":"%s","answers":%s}' "$generation" "$interaction_request" "$answers_json")
    action=resolveUserInput
  fi
  token=$(cat "$CLOUD_AGENTS_TOKEN_FILE")
  set +e
  http_status=$(curl --silent --show-error --cacert "$ca_file" \
    --header "Authorization: Bearer $token" --header "X-Request-ID: $execution_id-cross-tenant-$interaction_type" \
    --header "Content-Type: application/json" --request POST --data "$body" --output "$CLOUD_AGENTS_E2E_OUTPUT_DIR/$execution_id-cross-tenant-$interaction_type.json" \
    --write-out '%{http_code}' \
    "$CLOUD_AGENTS_ENDPOINT/v1/tenants/tenant-compose-smoke-other/projects/$CLOUD_AGENTS_PROJECT/sessions/$session_id/turns/$turn_id/executions/$execution_id:$action")
  curl_status=$?
  set -e
  if [ "$curl_status" -ne 0 ] || [ "$http_status" -ne 401 ]; then
    echo "cross-tenant $interaction_type interaction was accepted: curl=$curl_status http=$http_status" >&2
    return 1
  fi
  printf 'cross_tenant_interaction_negative=passed type=%s\n' "$interaction_type"
}
expect_interaction_authorization_rejected() {
  session_id=$1
  turn_id=$2
  execution_id=$3
  interaction_type=$4
  interaction_file=$5
  [ -n "${CLOUD_AGENTS_E2E_CONTROL_PLANE_CONTAINER-}" ] || return 0
  generation=$(interaction_field "$interaction_file" generation)
  interaction_request=$(interaction_field "$interaction_file" requestId)
  if [ "$interaction_type" = approval ]; then
    body=$(printf '{"generation":%s,"requestId":"%s","decision":"decline"}' "$generation" "$interaction_request")
    action=resolveApproval
  else
    question_id=$(interaction_field "$interaction_file" questionId)
    answers_json=$(node -e 'process.stdout.write(JSON.stringify({[process.argv[1]]:["Staging"]}))' "$question_id")
    body=$(printf '{"generation":%s,"requestId":"%s","answers":%s}' "$generation" "$interaction_request" "$answers_json")
    action=resolveUserInput
  fi
  token=$(cat "$CLOUD_AGENTS_TOKEN_FILE")
  expired_token_file="$CLOUD_AGENTS_E2E_OUTPUT_DIR/$execution_id-expired-token"
  AUTH_TEST_PRIVATE_KEY="$auth_test_private_key_file" AUTH_CONFIG_FILE="$auth_config_file" \
    CLOUD_AGENTS_EXPIRED_TOKEN_FILE="$expired_token_file" node <<'NODE'
const {createSign}=require("node:crypto");
const {readFileSync,writeFileSync,chmodSync}=require("node:fs");
const auth=JSON.parse(readFileSync(process.env.AUTH_CONFIG_FILE,"utf8"));
const now=Math.floor(Date.now()/1000);
const claims={iss:auth.issuer,aud:auth.audience,sub:"expired-interaction-token",exp:now-3600,iat:now-3601,client_id:"interaction-test-client",jti:"expired-interaction-token",scope:"projects.act",["https://schemas.cloud-agents.dev/claims/security-epoch"]:auth.securityEpoch,["https://schemas.cloud-agents.dev/claims/subject-kind"]:"user",["https://schemas.cloud-agents.dev/claims/tenant-id"]:"tenant-compose-smoke",["https://schemas.cloud-agents.dev/claims/token-profile"]:"cloud-agents-access-token/v1"};
const encode=(value)=>Buffer.from(JSON.stringify(value)).toString("base64url");
const signingInput=`${encode({alg:"RS256",kid:auth.keys[0].jwk.kid,typ:"at+jwt"})}.${encode(claims)}`;
const signature=createSign("RSA-SHA256").update(signingInput).end().sign(readFileSync(process.env.AUTH_TEST_PRIVATE_KEY)).toString("base64url");
writeFileSync(process.env.CLOUD_AGENTS_EXPIRED_TOKEN_FILE,`${signingInput}.${signature}\n`,{mode:0o600});
chmodSync(process.env.CLOUD_AGENTS_EXPIRED_TOKEN_FILE,0o600);
NODE
  set +e
  expired_status=$(curl --silent --show-error --cacert "$ca_file" \
    --header "Authorization: Bearer $(cat "$expired_token_file")" --header "X-Request-ID: $execution_id-expired-auth-$interaction_type" \
    --header "Content-Type: application/json" --request POST --data "$body" --output "$CLOUD_AGENTS_E2E_OUTPUT_DIR/$execution_id-expired-auth-$interaction_type.json" \
    --write-out '%{http_code}' \
    "$CLOUD_AGENTS_ENDPOINT/v1/tenants/$CLOUD_AGENTS_TENANT/projects/$CLOUD_AGENTS_PROJECT/sessions/$session_id/turns/$turn_id/executions/$execution_id:$action")
  expired_curl_status=$?
  set -e
  if [ "$expired_curl_status" -ne 0 ] || [ "$expired_status" -ne 401 ]; then
    echo "expired interaction authorization was accepted: curl=$expired_curl_status http=$expired_status" >&2
    return 1
  fi
  auth_backup="$CLOUD_AGENTS_E2E_OUTPUT_DIR/$execution_id-auth-original.json"
  cp "$auth_config_file" "$auth_backup"
  chmod 0600 "$auth_backup"
  AUTH_CONFIG_FILE="$auth_config_file" node <<'NODE'
const {readFileSync,writeFileSync,chmodSync}=require("node:fs");
const path=process.env.AUTH_CONFIG_FILE;
const auth=JSON.parse(readFileSync(path,"utf8"));
auth.generation+=1;
auth.securityEpoch+=1;
chmodSync(path,0o600);
writeFileSync(path,`${JSON.stringify(auth)}\n`);
chmodSync(path,0o444);
NODE
  docker kill --signal HUP "$CLOUD_AGENTS_E2E_CONTROL_PLANE_CONTAINER" >/dev/null
  revoked_status=0
  revoked_http=0
  attempt=0
  while [ "$attempt" -lt 20 ]; do
    set +e
    revoked_http=$(curl --silent --show-error --cacert "$ca_file" \
      --header "Authorization: Bearer $token" --header "X-Request-ID: $execution_id-revoked-auth-$interaction_type" \
      --header "Content-Type: application/json" --request POST --data "$body" --output "$CLOUD_AGENTS_E2E_OUTPUT_DIR/$execution_id-revoked-auth-$interaction_type.json" \
      --write-out '%{http_code}' \
      "$CLOUD_AGENTS_ENDPOINT/v1/tenants/$CLOUD_AGENTS_TENANT/projects/$CLOUD_AGENTS_PROJECT/sessions/$session_id/turns/$turn_id/executions/$execution_id:$action")
    revoked_status=$?
    set -e
    [ "$revoked_status" -eq 0 ] && [ "$revoked_http" -eq 401 ] && break
    attempt=$((attempt + 1))
    sleep 0.1
  done
  AUTH_CONFIG_FILE="$auth_config_file" AUTH_BACKUP_FILE="$auth_backup" node <<'NODE'
const {readFileSync,writeFileSync,chmodSync}=require("node:fs");
const path=process.env.AUTH_CONFIG_FILE;
const auth=JSON.parse(readFileSync(process.env.AUTH_BACKUP_FILE,"utf8"));
auth.generation+=2;
chmodSync(path,0o600);
writeFileSync(path,`${JSON.stringify(auth)}\n`);
chmodSync(path,0o444);
NODE
  docker kill --signal HUP "$CLOUD_AGENTS_E2E_CONTROL_PLANE_CONTAINER" >/dev/null
  attempt=0
  restored=1
  while [ "$attempt" -lt 20 ]; do
    if run_ctl --project "$CLOUD_AGENTS_PROJECT" --session "$session_id" --turn "$turn_id" --execution "$execution_id" \
      --request-id "$execution_id-auth-restored-$interaction_type" execution get >/dev/null 2>&1; then
      restored=0
      break
    fi
    attempt=$((attempt + 1))
    sleep 0.1
  done
  if [ "$revoked_status" -ne 0 ] || [ "$revoked_http" -ne 401 ] || [ "$restored" -ne 0 ]; then
    echo "revoked interaction authorization check failed: curl=$revoked_status http=$revoked_http restored=$restored" >&2
    return 1
  fi
  printf 'interaction_authorization_expired=passed type=%s\n' "$interaction_type"
  printf 'interaction_authorization_revoked=passed type=%s\n' "$interaction_type"
}

expect_checkpoint_protocol_rejected() {
  session_id=$1
  turn_id=$2
  execution_id=$3
  [ -n "${CLOUD_AGENTS_E2E_POSTGRES_CONTAINER-}" ] || return 0
  postgres_query() {
    sql=$4
    printf '%s\n' "$sql" | docker exec -i "$CLOUD_AGENTS_E2E_POSTGRES_CONTAINER" \
      psql -qAt -U cloud_agents_install_admin -d cloud_agents \
      -v ON_ERROR_STOP=1 -v tenant="$1" -v project="$CLOUD_AGENTS_PROJECT" \
      -v session="$2" -v turn="$3" -v execution="$execution_id"
  }
  postgres_query_value() {
    value=$5
    sql=$4
    printf '%s\n' "$sql" | docker exec -i "$CLOUD_AGENTS_E2E_POSTGRES_CONTAINER" \
      psql -qAt -U cloud_agents_install_admin -d cloud_agents \
      -v ON_ERROR_STOP=1 -v tenant="$1" -v project="$CLOUD_AGENTS_PROJECT" \
      -v session="$2" -v turn="$3" -v execution="$execution_id" -v value="$value"
  }
  original_protocol=$(postgres_query "$CLOUD_AGENTS_TENANT" "$session_id" "$turn_id" \
    "SELECT checkpoint_protocol FROM cloud_agents.managed_agent_executions WHERE tenant_id = :'tenant' AND project_uid = :'project' AND session_uid = :'session' AND turn_uid = :'turn' AND execution_uid = :'execution';")
  if [ "$original_protocol" != runtime-message-checkpoint-v1 ]; then
    echo "expected checkpoint protocol v1, got '$original_protocol'" >&2
    return 1
  fi
  postgres_query "$CLOUD_AGENTS_TENANT" "$session_id" "$turn_id" \
    "UPDATE cloud_agents.managed_agent_executions SET checkpoint_protocol = 'runtime-message-checkpoint-v0' WHERE tenant_id = :'tenant' AND project_uid = :'project' AND session_uid = :'session' AND turn_uid = :'turn' AND execution_uid = :'execution';" >/dev/null
  set +e
  run_ctl --project "$CLOUD_AGENTS_PROJECT" --session "$session_id" --turn "$turn_id" --execution "$execution_id" \
    --request-id "$execution_id-checkpoint-protocol" execution get \
    >"$CLOUD_AGENTS_E2E_OUTPUT_DIR/$execution_id-checkpoint-protocol.json" \
    2>"$CLOUD_AGENTS_E2E_OUTPUT_DIR/$execution_id-checkpoint-protocol.stderr"
  status=$?
  set -e
  postgres_query "$CLOUD_AGENTS_TENANT" "$session_id" "$turn_id" \
    "UPDATE cloud_agents.managed_agent_executions SET checkpoint_protocol = 'runtime-message-checkpoint-v1' WHERE tenant_id = :'tenant' AND project_uid = :'project' AND session_uid = :'session' AND turn_uid = :'turn' AND execution_uid = :'execution';" >/dev/null
  if [ "$status" -eq 0 ]; then
    echo "incompatible checkpoint protocol was accepted" >&2
    return 1
  fi
  printf 'checkpoint_protocol_negative=passed\n'
  original_messages=$(postgres_query "$CLOUD_AGENTS_TENANT" "$session_id" "$turn_id" \
    "SELECT runtime_messages FROM cloud_agents.managed_agent_executions WHERE tenant_id = :'tenant' AND project_uid = :'project' AND session_uid = :'session' AND turn_uid = :'turn' AND execution_uid = :'execution';")
  test -n "$original_messages"
  postgres_query "$CLOUD_AGENTS_TENANT" "$session_id" "$turn_id" \
    "UPDATE cloud_agents.managed_agent_executions SET runtime_messages = NULL WHERE tenant_id = :'tenant' AND project_uid = :'project' AND session_uid = :'session' AND turn_uid = :'turn' AND execution_uid = :'execution';" >/dev/null
  set +e
  run_ctl --project "$CLOUD_AGENTS_PROJECT" --session "$session_id" --turn "$turn_id" --execution "$execution_id" \
    --request-id "$execution_id-checkpoint-messages" execution get \
    >"$CLOUD_AGENTS_E2E_OUTPUT_DIR/$execution_id-checkpoint-messages.json" \
    2>"$CLOUD_AGENTS_E2E_OUTPUT_DIR/$execution_id-checkpoint-messages.stderr"
  status=$?
  set -e
  postgres_query_value "$CLOUD_AGENTS_TENANT" "$session_id" "$turn_id" \
    "UPDATE cloud_agents.managed_agent_executions SET runtime_messages = :'value' WHERE tenant_id = :'tenant' AND project_uid = :'project' AND session_uid = :'session' AND turn_uid = :'turn' AND execution_uid = :'execution';" \
    "$original_messages" >/dev/null
  if [ "$status" -eq 0 ]; then
    echo "missing checkpoint transcript was accepted" >&2
    return 1
  fi
  original_digest=$(postgres_query "$CLOUD_AGENTS_TENANT" "$session_id" "$turn_id" \
    "SELECT checkpoint_digest FROM cloud_agents.managed_agent_executions WHERE tenant_id = :'tenant' AND project_uid = :'project' AND session_uid = :'session' AND turn_uid = :'turn' AND execution_uid = :'execution';")
  test -n "$original_digest"
  postgres_query "$CLOUD_AGENTS_TENANT" "$session_id" "$turn_id" \
    "UPDATE cloud_agents.managed_agent_executions SET checkpoint_digest = 'sha256:$(printf '%064d' 0)' WHERE tenant_id = :'tenant' AND project_uid = :'project' AND session_uid = :'session' AND turn_uid = :'turn' AND execution_uid = :'execution';" >/dev/null
  set +e
  run_ctl --project "$CLOUD_AGENTS_PROJECT" --session "$session_id" --turn "$turn_id" --execution "$execution_id" \
    --request-id "$execution_id-checkpoint-digest" execution get \
    >"$CLOUD_AGENTS_E2E_OUTPUT_DIR/$execution_id-checkpoint-digest.json" \
    2>"$CLOUD_AGENTS_E2E_OUTPUT_DIR/$execution_id-checkpoint-digest.stderr"
  status=$?
  set -e
  postgres_query_value "$CLOUD_AGENTS_TENANT" "$session_id" "$turn_id" \
    "UPDATE cloud_agents.managed_agent_executions SET checkpoint_digest = :'value' WHERE tenant_id = :'tenant' AND project_uid = :'project' AND session_uid = :'session' AND turn_uid = :'turn' AND execution_uid = :'execution';" \
    "$original_digest" >/dev/null
  if [ "$status" -eq 0 ]; then
    echo "corrupt checkpoint digest was accepted" >&2
    return 1
  fi
  printf 'checkpoint_transcript_negative=passed\n'
}

wait_for_success() {
  final_file=$1
  session_id=$2
  turn_id=$3
  execution_id=$4
  if ! wait "$execute_pid"; then
    echo "interactive Provider execution failed" >&2
    if [ -s "$final_file.stderr" ]; then
      cat "$final_file.stderr" >&2
    fi
    diagnostic_file="$final_file.failed"
    if run_ctl --project "$CLOUD_AGENTS_PROJECT" --session "$session_id" --turn "$turn_id" --execution "$execution_id" \
      --request-id "$execution_id-failed" execution get >"$diagnostic_file" 2>/dev/null; then
      CLOUD_AGENTS_E2E_EXECUTION_FILE="$diagnostic_file" node <<'NODE' >&2
const { readFileSync } = require("node:fs");
const value = JSON.parse(readFileSync(process.env.CLOUD_AGENTS_E2E_EXECUTION_FILE, "utf8"));
const errors = (value.messages ?? []).filter((message) => message.messageType === "Error");
const runtimeErrorCodes = errors.flatMap((message) => typeof message.error?.code === "string" ? [message.error.code] : []);
const classify = (message) => {
  const normalized = String(message ?? "").toLowerCase();
  if (normalized.includes("auth") || normalized.includes("401") || normalized.includes("403")) return "auth";
  if (normalized.includes("credential") || normalized.includes("secret")) return "credential";
  if (normalized.includes("timeout") || normalized.includes("timed out")) return "timeout";
  if (normalized.includes("rate") || normalized.includes("429") || normalized.includes("quota")) return "rate";
  if (normalized.includes("mcp")) return "mcp";
  if (normalized.includes("skill")) return "skill";
  if (normalized.includes("resume") || normalized.includes("thread")) return "resume";
  if (normalized.includes("network") || normalized.includes("fetch") || normalized.includes("connect")) return "network";
  return "other";
};
const errorDetails = errors.map((message) => {
  const detail = typeof message.error?.message === "string" ? message.error.message : "";
  return { category: classify(detail), messageLength: detail.length };
});
process.stderr.write(`${JSON.stringify({ state: value.spec?.state, errorCode: value.spec?.errorCode, recoveryState: value.spec?.recoveryState, attemptNumber: value.spec?.attemptNumber, runtimeErrorCodes, errorDetails })}\n`);
NODE
    fi
    return 1
  fi
  execute_pid=
  CLOUD_AGENTS_E2E_EXECUTION_FILE="$final_file" node <<'NODE'
const { readFileSync } = require("node:fs");
const value = JSON.parse(readFileSync(process.env.CLOUD_AGENTS_E2E_EXECUTION_FILE, "utf8"));
if (value.spec?.state !== "succeeded" || !value.messages?.some((message) => message.messageType === "Result")) throw new Error("interactive Provider execution did not succeed");
NODE
}

wait_for_claim_expiry() {
  session_id=$1
  turn_id=$2
  execution_id=$3
  state_file=$4
  attempt=0
  while [ "$attempt" -lt 60 ]; do
    attempt=$((attempt + 1))
    if run_ctl --project "$CLOUD_AGENTS_PROJECT" --session "$session_id" --turn "$turn_id" --execution "$execution_id" \
      --request-id "$(bounded_request_id "$execution_id-claim-expiry-$attempt")" execution get >"$state_file" 2>/dev/null &&
      CLOUD_AGENTS_E2E_EXECUTION_FILE="$state_file" node <<'NODE'
const { readFileSync } = require("node:fs");
const value = JSON.parse(readFileSync(process.env.CLOUD_AGENTS_E2E_EXECUTION_FILE, "utf8"));
process.exit(value.spec?.state === "running" && Date.parse(value.spec?.claimExpiresAt ?? "") + 1000 < Date.now() ? 0 : 1);
NODE
    then
      return 0
    fi
    sleep 1
  done
  echo "timed out waiting for interactive Provider execution claim expiry" >&2
  return 1
}

prepare_interaction_takeover() {
  session_id=$1
  turn_id=$2
  execution_id=$3
  runtime_mode=$4
  interaction_mode=$5
  prompt=$6
  output_prefix=$7
  capability_bound=${8-0}
  set --
  if [ "$capability_bound" -eq 1 ]; then
    set -- --mcp-server-refs-json "$recovery_mcp_refs_json" --skill-bundle-refs-json "$recovery_skill_refs_json"
  fi
  wait_for_claim_expiry "$session_id" "$turn_id" "$execution_id" "$output_prefix.claim-expired"
  set +e
  run_ctl --timeout 60s --project "$CLOUD_AGENTS_PROJECT" --session "$session_id" --turn "$turn_id" --execution "$execution_id" \
    --request-id "$execution_id-run" --idempotency-key "$execution_id-run" execution execute \
    --runtime-mode "$runtime_mode" --interaction-mode "$interaction_mode" "$@" --input "$prompt" \
    >"$output_prefix.blocked" 2>"$output_prefix.blocked.stderr"
  blocked_status=$?
  set -e
  if [ "$blocked_status" -eq 0 ]; then
    echo "interactive recovery replay bypassed side-effect reconciliation" >&2
    return 1
  fi
  run_ctl --project "$CLOUD_AGENTS_PROJECT" --session "$session_id" --turn "$turn_id" --execution "$execution_id" \
    --request-id "$execution_id-recovery-state" execution get >"$output_prefix.awaiting-reconciliation"
  takeover_values=$(CLOUD_AGENTS_E2E_EXECUTION_FILE="$output_prefix.awaiting-reconciliation" node <<'NODE'
const { readFileSync } = require("node:fs");
const value = JSON.parse(readFileSync(process.env.CLOUD_AGENTS_E2E_EXECUTION_FILE, "utf8"));
const checkpoint = value.spec?.checkpoint;
if (value.spec?.state !== "running" || !Number.isSafeInteger(value.spec?.generation) || typeof checkpoint?.digest !== "string") throw new Error("interactive recovery state is invalid");
if (value.spec.recoveryState === "awaiting_reconciliation" && value.spec.recoveryReason === "side_effect_outcome_unknown" && checkpoint.pendingSideEffect === true) {
  process.stdout.write(`1|${value.spec.generation}|${checkpoint.digest}`);
} else if (checkpoint.pendingSideEffect === false && checkpoint.pendingInteractionCount > 0) {
  process.stdout.write(`0|${value.spec.generation}|${checkpoint.digest}`);
} else {
  throw new Error("interactive recovery did not stop after fencing the obsolete callback");
}
NODE
  )
  takeover_needs_reconcile=${takeover_values%%|*}
  takeover_values=${takeover_values#*|}
  takeover_generation=${takeover_values%%|*}
  takeover_checkpoint_digest=${takeover_values#*|}
}

reconcile_interaction_takeover() {
  session_id=$1
  turn_id=$2
  execution_id=$3
  run_ctl --project "$CLOUD_AGENTS_PROJECT" --session "$session_id" --turn "$turn_id" --execution "$execution_id" \
    --request-id "$execution_id-reconcile" --idempotency-key "$execution_id-reconcile" execution reconcile \
    --generation "$takeover_generation" --checkpoint-digest "$takeover_checkpoint_digest" --outcome not-applied >/dev/null
  execution_retry_suffix=-reconciled
}

assert_interaction_takeover() {
  final_file=$1
  expected_mode=${2-process-restart}
  CLOUD_AGENTS_E2E_EXECUTION_FILE="$final_file" CLOUD_AGENTS_E2E_RECOVERY_MODE="$expected_mode" node <<'NODE'
const { readFileSync } = require("node:fs");
const value = JSON.parse(readFileSync(process.env.CLOUD_AGENTS_E2E_EXECUTION_FILE, "utf8"));
const expectedModes = new Set(process.env.CLOUD_AGENTS_E2E_RECOVERY_MODE.split("|"));
if (value.spec?.attemptNumber !== 2 || value.spec?.recoveryState !== "recovered" || !expectedModes.has(value.spec?.recoveryMode)) {
  process.stderr.write(`${JSON.stringify({state: value.spec?.state, errorCode: value.spec?.errorCode, attemptNumber: value.spec?.attemptNumber, recoveryState: value.spec?.recoveryState, recoveryMode: value.spec?.recoveryMode, recoverySourceTargetId: value.spec?.recoverySourceTargetId, recoveryTargetId: value.spec?.recoveryTargetId})}\\n`);
  throw new Error(`interactive execution did not complete through ${process.env.CLOUD_AGENTS_E2E_RECOVERY_MODE}`);
}
NODE
}

restart_control_plane_while_waiting() {
  session_id=$1
  [ -n "${CLOUD_AGENTS_E2E_CONTROL_PLANE_CONTAINER-}" ] || return 0
  previous_started_at=$(docker inspect --format '{{.State.StartedAt}}' "$CLOUD_AGENTS_E2E_CONTROL_PLANE_CONTAINER")
  docker kill --signal KILL "$CLOUD_AGENTS_E2E_CONTROL_PLANE_CONTAINER" >/dev/null
  if wait "$execute_pid"; then
    echo "interactive execution completed while its persisted interaction was unresolved" >&2
    return 1
  fi
  execute_pid=
  attempt=0
  while [ "$attempt" -lt 90 ]; do
    attempt=$((attempt + 1))
    running=$(docker inspect --format '{{.State.Running}}' "$CLOUD_AGENTS_E2E_CONTROL_PLANE_CONTAINER" 2>/dev/null || printf 'false')
    started_at=$(docker inspect --format '{{.State.StartedAt}}' "$CLOUD_AGENTS_E2E_CONTROL_PLANE_CONTAINER" 2>/dev/null || true)
    if [ "$running" != true ]; then
      docker start "$CLOUD_AGENTS_E2E_CONTROL_PLANE_CONTAINER" >/dev/null 2>&1 || true
      sleep 1
      continue
    fi
    if [ "$started_at" = "$previous_started_at" ]; then
      sleep 1
      continue
    fi
    published_endpoint=$(docker port "$CLOUD_AGENTS_E2E_CONTROL_PLANE_CONTAINER" 8080/tcp 2>/dev/null || true)
    case "$published_endpoint" in
      127.0.0.1:*) CLOUD_AGENTS_ENDPOINT="https://$published_endpoint" ;;
      0.0.0.0:*) CLOUD_AGENTS_ENDPOINT="https://127.0.0.1:${published_endpoint##*:}" ;;
      \[::\]:*) CLOUD_AGENTS_ENDPOINT="https://127.0.0.1:${published_endpoint##*:}" ;;
    esac
    if { [ -n "$ca_file" ] && curl --silent --show-error --fail --noproxy '*' --cacert "$ca_file" "$CLOUD_AGENTS_ENDPOINT/readyz" >/dev/null 2>&1; } ||
      { [ -z "$ca_file" ] && curl --silent --show-error --fail --noproxy '*' "$CLOUD_AGENTS_ENDPOINT/readyz" >/dev/null 2>&1; }; then
      if run_ctl --project "$CLOUD_AGENTS_PROJECT" --session "$session_id" \
      --request-id "$(bounded_request_id "$session_id-after-control-plane-restart-$attempt")" session get >/dev/null 2>&1; then
        if [ "${CLOUD_AGENTS_E2E_REMOTE_WORKER-0}" = 1 ]; then
          target_ready=0
          target_attempt=0
          while [ "$target_attempt" -lt 90 ]; do
            target_attempt=$((target_attempt + 1))
            if target_output=$(run_admin_ctl --project "$CLOUD_AGENTS_PROJECT" --target "${CLOUD_AGENTS_E2E_AGENT_TARGET_ID-}" \
              --request-id "$session_id-target-ready-$target_attempt" target get 2>/dev/null) &&
              case "$target_output" in
                *'"targetKind":"remote-worker"'*'"observedPhase":"ready"'*) true ;;
                *) false ;;
              esac; then
              target_ready=1
              break
            fi
            sleep 1
          done
          [ "$target_ready" -eq 1 ] || { echo "RemoteWorker target did not become ready after Control Plane restart" >&2; return 1; }
          pty_queue_attempt=0
          while [ "$pty_queue_attempt" -lt 90 ]; do
            pty_queue_attempt=$((pty_queue_attempt + 1))
            pty_queue_count=$(printf '%s\n' \
              "SELECT count(*) FROM cloud_agents.remote_worker_sandbox_pty_commands WHERE tenant_id = :'tenant' AND project_uid = :'project' AND target_uid = :'target' AND state IN ('pending','delivered') AND deadline_at > clock_timestamp();" |
              docker exec -i "$CLOUD_AGENTS_E2E_POSTGRES_CONTAINER" psql -qAt -U cloud_agents_install_admin -d cloud_agents \
                -v ON_ERROR_STOP=1 -v tenant="$CLOUD_AGENTS_TENANT" -v project="$CLOUD_AGENTS_PROJECT" -v target="$CLOUD_AGENTS_E2E_AGENT_TARGET_ID" 2>/dev/null || printf 'unavailable')
            if [ "$pty_queue_count" = 0 ]; then
              # Require one quiet interval so the worker has acknowledged any
              # expired delivery before the recovery attempt creates a session.
              sleep 1
              pty_queue_count=$(printf '%s\n' \
                "SELECT count(*) FROM cloud_agents.remote_worker_sandbox_pty_commands WHERE tenant_id = :'tenant' AND project_uid = :'project' AND target_uid = :'target' AND state IN ('pending','delivered') AND deadline_at > clock_timestamp();" |
                docker exec -i "$CLOUD_AGENTS_E2E_POSTGRES_CONTAINER" psql -qAt -U cloud_agents_install_admin -d cloud_agents \
                  -v ON_ERROR_STOP=1 -v tenant="$CLOUD_AGENTS_TENANT" -v project="$CLOUD_AGENTS_PROJECT" -v target="$CLOUD_AGENTS_E2E_AGENT_TARGET_ID" 2>/dev/null || printf 'unavailable')
              if [ "$pty_queue_count" = 0 ]; then
                printf 'interaction_remote_worker_pty_queue_ready=passed target=%s\n' "$CLOUD_AGENTS_E2E_AGENT_TARGET_ID"
                break
              fi
            fi
            sleep 1
          done
          [ "$pty_queue_count" = 0 ] || { echo "RemoteWorker PTY queue did not become ready after Control Plane restart" >&2; return 1; }
        else
          sleep 5
        fi
        printf 'interaction_wait_control_plane_restart=passed session=%s\n' "$session_id"
        return 0
      fi
    fi
    sleep 1
  done
  echo "Control Plane did not recover during persisted interaction wait" >&2
  return 1
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
      --request-id "$(bounded_request_id "$execution_id-running-$attempt")" execution get >"$state_file" 2>/dev/null; then
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

load_remote_worker_pty_session() {
  remote_worker_state_directory=$(docker inspect --format '{{range .Mounts}}{{if eq .Destination "/node-output"}}{{.Source}}{{end}}{{end}}' "$CLOUD_AGENTS_E2E_WORKER_CONTAINER")
  case "$remote_worker_state_directory" in
    */remote-worker-node | */remote-worker-node-restore) ;;
    *) echo "RemoteWorker state file mount is not test-owned" >&2; return 1 ;;
  esac
  remote_worker_state_file="$remote_worker_state_directory/install/state.json"
  remote_worker_pty_session=$(CLOUD_AGENTS_STATE_FILE="$remote_worker_state_file" node -e '
    const { readFileSync } = require("node:fs");
    const value = JSON.parse(readFileSync(process.env.CLOUD_AGENTS_STATE_FILE, "utf8"));
    const sessionId = value.sandboxPtyCommand?.sessionId ?? value.sandboxPtyCommandReceipt?.sessionId;
    if (!/^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$/.test(sessionId ?? "")) process.exit(1);
    process.stdout.write(sessionId);') || true
  if [ -z "$remote_worker_pty_session" ] && [ -n "${CLOUD_AGENTS_E2E_POSTGRES_CONTAINER-}" ]; then
    remote_worker_pty_session=$(printf '%s\n' \
      "SELECT session_uid FROM cloud_agents.sandbox_pty_sessions WHERE tenant_id = :'tenant' AND project_uid = :'project' AND sandbox_uid = :'sandbox' AND deleted_at IS NULL ORDER BY created_at DESC LIMIT 1;" |
      docker exec -i "$CLOUD_AGENTS_E2E_POSTGRES_CONTAINER" psql -qAt -U cloud_agents_install_admin -d cloud_agents \
        -v ON_ERROR_STOP=1 -v tenant="$CLOUD_AGENTS_TENANT" -v project="$CLOUD_AGENTS_PROJECT" -v sandbox="$sandbox_id") || true
  fi
}

fence_remote_worker_pty() {
  [ "$recovery_environment" = remote-worker ] || return 0
  load_remote_worker_pty_session
  printf '%s\n' "$remote_worker_pty_session" | grep -Eq '^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$' || {
    echo "RemoteWorker durable state has no active PTY session" >&2
    return 1
  }
  pty_delete_result=$(printf '%s\n' \
    "UPDATE cloud_agents.sandbox_pty_sessions SET deleted_at = COALESCE(deleted_at, transaction_timestamp()) WHERE tenant_id = :'tenant' AND project_uid = :'project' AND sandbox_uid = :'sandbox' AND session_uid = :'session' AND deleted_at IS NULL RETURNING session_uid;" |
    docker exec -i "$CLOUD_AGENTS_E2E_POSTGRES_CONTAINER" psql -qAt -U cloud_agents_install_admin -d cloud_agents \
      -v ON_ERROR_STOP=1 -v tenant="$CLOUD_AGENTS_TENANT" -v project="$CLOUD_AGENTS_PROJECT" -v sandbox="$sandbox_id" -v session="$remote_worker_pty_session") || {
    echo "RemoteWorker PTY session deletion failed" >&2
    return 1
  }
  [ "$pty_delete_result" = "$remote_worker_pty_session" ] || {
    echo "RemoteWorker PTY session was not active in the test database" >&2
    return 1
  }
  # Do not let a restarted test Worker replay its durable PTY command into the old claim.
  rm -f -- "$remote_worker_state_file"
  # Keep the old PTY callback down for the 30s runtime claim lease before takeover.
  sleep 35
}

restart_worker_during_execution() {
  if [ "$recovery_environment" = remote-worker ]; then
    previous_started_at=$(docker inspect --format '{{.State.StartedAt}}' "$CLOUD_AGENTS_E2E_WORKER_CONTAINER")
    docker kill --signal KILL "$CLOUD_AGENTS_E2E_WORKER_CONTAINER" >/dev/null
    if [ "$recovery_provider" = deepseek-harness ]; then
      printf 'remote_worker_pty_fence=not-applicable provider=%s adapter=str_replace_editor\n' "$recovery_provider"
    else
      fence_remote_worker_pty
    fi
    docker start "$CLOUD_AGENTS_E2E_WORKER_CONTAINER" >/dev/null
    attempt=0
    while [ "$attempt" -lt 90 ]; do
      attempt=$((attempt + 1))
      running=$(docker inspect --format '{{.State.Running}}' "$CLOUD_AGENTS_E2E_WORKER_CONTAINER" 2>/dev/null || true)
      started_at=$(docker inspect --format '{{.State.StartedAt}}' "$CLOUD_AGENTS_E2E_WORKER_CONTAINER" 2>/dev/null || true)
      if [ "$running" = true ] && [ "$started_at" != "$previous_started_at" ] &&
        target_output=$(run_admin_ctl --project "$CLOUD_AGENTS_PROJECT" --target "$CLOUD_AGENTS_E2E_AGENT_TARGET_ID" \
          --request-id "$CLOUD_AGENTS_E2E_RUN_ID-worker-restart-target-$attempt" target get 2>/dev/null) &&
        case "$target_output" in *'"targetKind":"remote-worker"'*'"observedPhase":"ready"'*) true ;; *) false ;; esac; then
        return 0
      fi
      sleep 1
    done
    echo "RemoteWorker did not become ready after the injected process exit" >&2
    return 1
  fi
  [ "$recovery_environment" = docker ] || { echo "Worker fault target is unavailable for $recovery_environment" >&2; return 1; }
  previous_started_at=$(docker inspect --format '{{.State.StartedAt}}' "$CLOUD_AGENTS_E2E_WORKER_CONTAINER")
  docker kill --signal KILL "$CLOUD_AGENTS_E2E_WORKER_CONTAINER" >/dev/null
  attempt=0
  while [ "$attempt" -lt 90 ]; do
    attempt=$((attempt + 1))
    running=$(docker inspect --format '{{.State.Running}}' "$CLOUD_AGENTS_E2E_WORKER_CONTAINER" 2>/dev/null || printf 'false')
    started_at=$(docker inspect --format '{{.State.StartedAt}}' "$CLOUD_AGENTS_E2E_WORKER_CONTAINER" 2>/dev/null || true)
    if [ "$running" = true ] && [ "$started_at" != "$previous_started_at" ]; then
      worker_port=$(docker port "$CLOUD_AGENTS_E2E_WORKER_CONTAINER" 8091/tcp 2>/dev/null | sed -n '1s/.*://p')
      if [ -n "$worker_port" ] && CLOUD_AGENTS_E2E_WORKER_PORT="$worker_port" node <<'NODE'
const net = require("node:net");
const socket = net.createConnection({host: "127.0.0.1", port: Number(process.env.CLOUD_AGENTS_E2E_WORKER_PORT)});
const fail = () => { socket.destroy(); process.exit(1); };
socket.setTimeout(1000, fail);
socket.once("connect", () => { socket.destroy(); process.exit(0); });
socket.once("error", fail);
NODE
      then
        if [ -n "$admin_token_file" ] && [ -n "$admin_curl_config" ]; then
          lease_file=$(mktemp)
          health_file=$(mktemp)
          health_status_file=$(mktemp)
          if run_admin_ctl --project "$CLOUD_AGENTS_PROJECT" --lease "$lease_id" \
            --request-id "$CLOUD_AGENTS_E2E_RUN_ID-worker-restart-lease" \
            environment-lease get >"$lease_file" 2>/dev/null; then
            lease_generation=$(CLOUD_AGENTS_E2E_LEASE_FILE="$lease_file" node <<'NODE'
const { readFileSync } = require("node:fs");
const value = JSON.parse(readFileSync(process.env.CLOUD_AGENTS_E2E_LEASE_FILE, "utf8"));
if (!Number.isSafeInteger(value.spec?.generation) || value.spec.generation < 1) process.exit(1);
process.stdout.write(String(value.spec.generation));
NODE
            )
            if [ -n "$lease_generation" ] && curl --silent --show-error --cacert "$ca_file" \
              --config "$admin_curl_config" \
              --header "X-Request-ID: $CLOUD_AGENTS_E2E_RUN_ID-worker-restart-health-$attempt" \
              --output "$health_file" \
              --write-out '%{http_code}' >"$health_status_file" \
              "$CLOUD_AGENTS_ENDPOINT/v1/admin/tenants/$CLOUD_AGENTS_TENANT/projects/$CLOUD_AGENTS_PROJECT/workers/$lease_id/health?expectedGeneration=$lease_generation"; then
              if CLOUD_AGENTS_E2E_HEALTH_FILE="$health_file" CLOUD_AGENTS_E2E_HEALTH_STATUS_FILE="$health_status_file" node <<'NODE'
const { readFileSync } = require("node:fs");
const value = JSON.parse(readFileSync(process.env.CLOUD_AGENTS_E2E_HEALTH_FILE, "utf8"));
const status = readFileSync(process.env.CLOUD_AGENTS_E2E_HEALTH_STATUS_FILE, "utf8");
if (status !== "200") {
  console.error(`worker restart health status=${status} code=${value.error?.code ?? "unknown"}`);
  process.exit(1);
}
if (value.state !== "serving") {
  console.error(`worker restart health state=${value.state ?? "unknown"}`);
  process.exit(1);
}
process.exit(value.state === "serving" ? 0 : 1);
NODE
              then
                rm -f "$lease_file" "$health_file" "$health_status_file"
                return 0
              fi
            fi
          fi
          rm -f "$lease_file" "$health_file" "$health_status_file"
        else
          return 0
        fi
      fi
    fi
    if [ "$running" != true ]; then
      docker start "$CLOUD_AGENTS_E2E_WORKER_CONTAINER" >/dev/null 2>&1 || true
    fi
    sleep 1
  done
  docker inspect --format '{{json .State}}' "$CLOUD_AGENTS_E2E_WORKER_CONTAINER" >&2 || true
  docker logs --tail 40 "$CLOUD_AGENTS_E2E_WORKER_CONTAINER" >&2 || true
  if [ -n "${health_file-}" ] && [ -s "$health_file" ]; then
    cat "$health_file" >&2 || true
  fi
  echo "Worker did not restart and accept TCP connections after the injected process exit" >&2
  return 1
}

restart_mcp_fixture_after_worker() {
  fixture_container=${CLOUD_AGENTS_E2E_MCP_FIXTURE_CONTAINER-}
  [ -n "$fixture_container" ] || return 0
  case "$fixture_container" in
    *[!A-Za-z0-9_.-]*) echo "invalid MCP fixture container name" >&2; return 1 ;;
  esac
  docker restart "$fixture_container" >/dev/null
  attempt=0
  while [ "$attempt" -lt 30 ]; do
    if docker exec "$fixture_container" node -e \
      'fetch("http://127.0.0.1:48765").then(() => process.exit(0)).catch(() => process.exit(1))' \
      >/dev/null 2>&1; then
      return 0
    fi
    attempt=$((attempt + 1))
    sleep 0.1
  done
  echo "MCP fixture did not recover after Worker restart" >&2
  return 1
}

recovery_prompt() {
  marker=$1
  recovery_session_id=${2-}
  if [ "$recovery_checkpoint_mode" = side-effect ]; then
    if [ "$recovery_provider" = deepseek-harness ]; then
      recovery_absolute_path="/workspace/.cloud-agents/managed-agent/tenants/$CLOUD_AGENTS_TENANT/projects/$CLOUD_AGENTS_PROJECT/sessions/$recovery_session_id/workspace/$recovery_artifact_path"
      printf '%s' "Use the $recovery_skill_name Skill exactly once for this managed capability recovery request and follow it exactly. Then call str_replace_editor exactly once with command create, path $recovery_absolute_path, and file_text $recovery_expected_content. Do not use shell commands. After the file is created, reply with '$marker'. Do not reply before the file is created."
    else
      printf '%s' "Call the managed MCP tool named $recovery_mcp_tool_name exactly once, then use the $recovery_skill_name Skill for this managed capability recovery request. Follow it exactly: use the Bash tool exactly once to run this exact command: $recovery_bash_command. Do not use another tool. After the command succeeds, reply with '$marker'. Do not reply before the command succeeds."
    fi
    return 0
  fi
  if [ "$recovery_capability_bound" -eq 1 ]; then
    case "$recovery_provider" in
      codex) recovery_skill_name=managed-capability-acceptance ;;
      claudeAgent) recovery_skill_name=managed-capability-acceptance:managed-capability-acceptance ;;
      *) echo "capability-bound recovery does not support Provider $recovery_provider" >&2; return 1 ;;
    esac
    printf '%s' "Use the $recovery_skill_name Skill for this managed capability interactive recovery request and follow it exactly. You must call its managed MCP tool, then call AskUserQuestion exactly once with one non-secret environment question that offers Staging, and wait for the answer. After the answer, your final response must contain exactly '$marker' and no other text."
  else
    printf '%s' "Before replying, call request_user_input exactly once with one non-secret question asking which environment to use and offer Staging as an option. After the answer, your final response must contain exactly '$marker' and no other text."
  fi
}

wait_for_recovery_side_effect_checkpoint() {
  session_id=$1
  turn_id=$2
  execution_id=$3
  checkpoint_file=$4
  checkpoint_diagnostic_file="$checkpoint_file.diagnostic"
  checkpoint_request_prefix=$(CLOUD_AGENTS_E2E_REQUEST_ID="$execution_id" node <<'NODE'
const { createHash } = require("node:crypto");
const value = process.env.CLOUD_AGENTS_E2E_REQUEST_ID;
process.stdout.write(value.length <= 80 ? value : `execution-${createHash("sha256").update(value).digest("hex").slice(0, 32)}`);
NODE
)
  rm -f "$checkpoint_diagnostic_file"
  attempt=0
  while [ "$attempt" -lt 180 ]; do
    attempt=$((attempt + 1))
    if run_ctl --project "$CLOUD_AGENTS_PROJECT" --session "$session_id" --turn "$turn_id" --execution "$execution_id" \
      --request-id "$checkpoint_request_prefix-recovery-checkpoint-$attempt" execution get >"$checkpoint_file" 2>/dev/null; then
      if CLOUD_AGENTS_E2E_EXECUTION_FILE="$checkpoint_file" node <<'NODE' >"$checkpoint_diagnostic_file"
const { readFileSync } = require("node:fs");
const value = JSON.parse(readFileSync(process.env.CLOUD_AGENTS_E2E_EXECUTION_FILE, "utf8"));
const messages = value.messages ?? [];
const completedCapability = (resourceId, sourceItemType) => typeof resourceId === "string" && messages.some((message) => {
  const payload = message.payload?.payload;
  return message.messageType === "Event" && message.payload?.eventType === "item.completed" &&
    payload?.status === "completed" && payload?.data?.capabilityResourceId === resourceId &&
    (!sourceItemType || String(payload.data?.sourceItemType ?? "").toLowerCase() === sourceItemType);
});
const observedCapability = (resourceId, sourceItemType) => typeof resourceId === "string" && messages.some((message) => {
  const eventType = message.payload?.eventType;
  const payload = message.payload?.payload;
  return message.messageType === "Event" && ["item.started", "item.updated", "item.completed"].includes(eventType) &&
    payload?.data?.capabilityResourceId === resourceId &&
    String(payload.data?.sourceItemType ?? "").toLowerCase() === sourceItemType;
});
const activeItems = new Map();
for (const message of messages) {
  const eventType = message.payload?.eventType;
  const payload = message.payload?.payload;
  const itemId = payload?.data?.providerItemId;
  if (message.messageType !== "Event" || typeof itemId !== "string") continue;
  if (eventType === "item.started" || eventType === "item.updated") activeItems.set(itemId, payload);
  if (eventType === "item.completed") activeItems.delete(itemId);
}
const expectedMcp = value.spec?.mcpServerRefs?.[0]?.serverId;
const expectedSkill = value.spec?.skillBundleRefs?.[0]?.bundleId;
const pendingBash = [...activeItems.values()].some((payload) =>
  payload?.itemType === "command_execution" &&
  String(payload?.data?.sourceItemType ?? "").toLowerCase() === "bash");
const gates = {
  stateRunning: value.spec?.state === "running",
  checkpointPresent: value.spec?.checkpoint?.sequence >= 1,
  pendingSideEffect: value.spec?.checkpoint?.pendingSideEffect === true,
  completedMcp: completedCapability(expectedMcp),
  observedSkill: observedCapability(expectedSkill, "skill"),
  // Side-effect recovery may hold a non-Bash managed tool (DeepSeek uses
  // str_replace_editor); interaction recovery still needs its pending Bash.
  activeBash: value.spec?.checkpoint?.pendingSideEffect === true || pendingBash,
};
const outline = messages.flatMap((message) => {
  if (message.messageType !== "Event") return [];
  const payload = message.payload?.payload;
  return [{
    eventType: message.payload?.eventType,
    itemType: payload?.itemType,
    status: payload?.status,
    sourceItemType: payload?.data?.sourceItemType,
    providerItemId: payload?.data?.providerItemId,
    capabilityResourceId: payload?.data?.capabilityResourceId,
  }];
}).slice(-64);
process.stdout.write(`${JSON.stringify({gates, outline})}\n`);
const ready = Object.values(gates).every(Boolean);
if (ready) process.exit(0);
if (["succeeded", "failed", "cancelled"].includes(value.spec?.state)) process.exit(2);
process.exit(1);
NODE
      then
        rm -f "$checkpoint_diagnostic_file"
        return 0
      else
        checkpoint_status=$?
        if [ "$checkpoint_status" -eq 2 ]; then
          cat "$checkpoint_diagnostic_file" >&2
          rm -f "$checkpoint_diagnostic_file"
          return 1
        fi
      fi
    fi
    sleep 1
  done
  if [ -s "$checkpoint_diagnostic_file" ]; then
    cat "$checkpoint_diagnostic_file" >&2
  fi
  rm -f "$checkpoint_diagnostic_file"
  echo "timed out waiting for capability-bound recovery side-effect checkpoint" >&2
  return 1
}

recovery_artifact_digest() {
  container=$1
  artifact_absolute=$2
  case "$artifact_absolute" in
    /workspace/.cloud-agents/managed-agent/tenants/*/projects/*/sessions/*/workspace/.cloud-agents-stage3-acceptance/*.txt) ;;
    *) echo "invalid recovery artifact probe path" >&2; return 1 ;;
  esac
  if [ "$recovery_environment" = docker ]; then
    case "$container" in
      '' | *[!A-Za-z0-9_.-]*) echo "invalid recovery runtime container" >&2; return 1 ;;
    esac
    docker exec "$container" sh -c 'if test -f "$1"; then sha256sum -- "$1" | cut -d" " -f1; else printf "absent\n"; fi' sh "$artifact_absolute"
    return
  fi
  if [ "$recovery_environment" = remote-worker ]; then
    if [ -z "$recovery_grant_id" ]; then
      grant_request_id=$(bounded_request_id "$CLOUD_AGENTS_E2E_RUN_ID-artifact-grant")
      grant_output=$(run_ctl --project "$CLOUD_AGENTS_PROJECT" --sandbox "$sandbox_id" \
        --request-id "$grant_request_id" --idempotency-key "$grant_request_id" sandbox grant \
        --expected-generation "$sandbox_generation" --ttl-seconds 300 2>/dev/null) || return 1
      recovery_grant_id=$(CLOUD_AGENTS_E2E_GRANT_JSON="$grant_output" node -e '
        const value = JSON.parse(process.env.CLOUD_AGENTS_E2E_GRANT_JSON);
        if (!/^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$/.test(value.grantId ?? "")) process.exit(1);
        process.stdout.write(value.grantId);') || return 1
    fi
    artifact_relative=${artifact_absolute#*/workspace/}
    artifact_directory=${artifact_relative%/*}
    artifact_name=${artifact_relative##*/}
    attempt=0
    while [ "$attempt" -lt 5 ]; do
      attempt=$((attempt + 1))
      read_request_id=$(bounded_request_id "$CLOUD_AGENTS_E2E_RUN_ID-artifact-file-read-$attempt")
      read_error_file="$CLOUD_AGENTS_E2E_OUTPUT_DIR/$CLOUD_AGENTS_E2E_RUN_ID-artifact-file-read-$attempt.stderr"
      if page=$(run_ctl --project "$CLOUD_AGENTS_PROJECT" --grant "$recovery_grant_id" \
        --request-id "$read_request_id" files read --path "$artifact_relative" --limit 1048576 2>"$read_error_file") &&
        CLOUD_AGENTS_E2E_FILE_PAGE="$page" node -e '
          const { createHash } = require("node:crypto");
          const value = JSON.parse(process.env.CLOUD_AGENTS_E2E_FILE_PAGE);
          const content = Buffer.from(value.contentBase64Url ?? "", "base64url");
          if (value.eof !== true || value.totalBytes !== content.length) process.exit(1);
          process.stdout.write(createHash("sha256").update(content).digest("hex"));'
      then
        rm -f "$read_error_file"
        return 0
      fi
      if grep -Eq '(^|[^A-Z])NOT_FOUND([^A-Z]|$)' "$read_error_file" 2>/dev/null; then
        rm -f "$read_error_file"
        printf 'absent'
        return 0
      fi
      rm -f "$read_error_file"
      list_request_id=$(bounded_request_id "$CLOUD_AGENTS_E2E_RUN_ID-artifact-file-list-$attempt")
      list_error_file="$CLOUD_AGENTS_E2E_OUTPUT_DIR/$CLOUD_AGENTS_E2E_RUN_ID-artifact-file-list-$attempt.stderr"
      if listing=$(run_ctl --project "$CLOUD_AGENTS_PROJECT" --grant "$recovery_grant_id" \
        --request-id "$list_request_id" files list --path "$artifact_directory" 2>"$list_error_file") &&
        CLOUD_AGENTS_E2E_FILE_LIST="$listing" CLOUD_AGENTS_E2E_ARTIFACT_PATH="$artifact_relative" \
        CLOUD_AGENTS_E2E_ARTIFACT_NAME="$artifact_name" node -e '
          const value = JSON.parse(process.env.CLOUD_AGENTS_E2E_FILE_LIST);
          const path = process.env.CLOUD_AGENTS_E2E_ARTIFACT_PATH;
          const name = process.env.CLOUD_AGENTS_E2E_ARTIFACT_NAME;
          const present = (value.entries ?? []).some((entry) => entry.path === path || entry.path === name);
          if (present) process.exit(1);
          process.stdout.write("absent");'
      then
        rm -f "$list_error_file"
        return 0
      fi
      if grep -Eq '(^|[^A-Z])NOT_FOUND([^A-Z]|$)' "$list_error_file" 2>/dev/null; then
        rm -f "$list_error_file"
        printf 'absent'
        return 0
      fi
      rm -f "$list_error_file"
      sleep 1
    done
    return 1
  fi
  attempt=0
  while [ "$attempt" -lt 5 ]; do
    attempt=$((attempt + 1))
    if probe=$(run_ctl --project "$CLOUD_AGENTS_PROJECT" --sandbox "$sandbox_id" \
      --request-id "$(bounded_request_id "$CLOUD_AGENTS_E2E_RUN_ID-artifact-probe-$attempt")" sandbox exec \
      --expected-generation "$sandbox_generation" \
      --command "if test -f '$artifact_absolute'; then sha256sum -- '$artifact_absolute' | cut -d' ' -f1; else printf 'absent\n'; fi") &&
      CLOUD_AGENTS_E2E_PROBE="$probe" node -e '
        const value = JSON.parse(process.env.CLOUD_AGENTS_E2E_PROBE);
        const stdout = String(value.stdout ?? "").replace(/[\\r\\n]+$/, "");
        if (value.exitCode !== 0 || !/^(?:[a-f0-9]{64}|absent)$/.test(stdout) || value.stderr !== "") process.exit(1);
        process.stdout.write(stdout);'
    then
      return 0
    fi
    sleep 1
  done
  return 1
}

reconcile_side_effect_takeover() {
  session_id=$1
  turn_id=$2
  execution_id=$3
  runtime_container=$4
  artifact_absolute=$5
  expected_digest=$6
  if [ "$takeover_needs_reconcile" -ne 1 ]; then
    return 0
  fi
  observed_digest=$(recovery_artifact_digest "$runtime_container" "$artifact_absolute") || {
    echo "recovery side effect probe is unavailable" >&2
    return 1
  }
  case "$observed_digest" in
    "$expected_digest") outcome=confirmed ;;
    absent) outcome=not-applied ;;
    *) echo "recovery side effect has an unexpected digest" >&2; return 1 ;;
  esac
  run_ctl --project "$CLOUD_AGENTS_PROJECT" --session "$session_id" --turn "$turn_id" --execution "$execution_id" \
    --request-id "$execution_id-reconcile" --idempotency-key "$execution_id-reconcile" execution reconcile \
    --generation "$takeover_generation" --checkpoint-digest "$takeover_checkpoint_digest" --outcome "$outcome" >/dev/null
  execution_retry_suffix=-reconciled
  printf 'side_effect_reconciliation=passed outcome=%s\n' "$outcome"
}

assert_side_effect_recovery_result() {
  final_file=$1
  expected_mode=$2
  marker=$3
  CLOUD_AGENTS_E2E_EXECUTION_FILE="$final_file" CLOUD_AGENTS_E2E_RECOVERY_MODE="$expected_mode" CLOUD_AGENTS_E2E_MARKER="$marker" node <<'NODE'
const { readFileSync } = require("node:fs");
const value = JSON.parse(readFileSync(process.env.CLOUD_AGENTS_E2E_EXECUTION_FILE, "utf8"));
const expectedModes = new Set(process.env.CLOUD_AGENTS_E2E_RECOVERY_MODE.split("|"));
if (value.spec?.attemptNumber !== 2 || value.spec?.recoveryState !== "recovered" || !expectedModes.has(value.spec?.recoveryMode) || value.spec?.state !== "succeeded") {
  throw new Error("side-effect process recovery did not complete safely");
}
if (!JSON.stringify(value.messages ?? []).includes(process.env.CLOUD_AGENTS_E2E_MARKER)) throw new Error("recovered process result changed");
NODE
}

wait_for_recovery_interaction() {
  session_id=$1
  turn_id=$2
  execution_id=$3
  interaction_file=$4
  if [ "$recovery_capability_bound" -ne 1 ]; then
    wait_for_interaction "$session_id" "$turn_id" "$execution_id" user-input "$interaction_file"
    return
  fi
  current_file="$interaction_file.current"
  handled_approvals=
  attempt=0
  while [ "$attempt" -lt 180 ]; do
    attempt=$((attempt + 1))
    if run_ctl --project "$CLOUD_AGENTS_PROJECT" --session "$session_id" --turn "$turn_id" --execution "$execution_id" \
      --request-id "$(bounded_request_id "$execution_id-recovery-interaction-$attempt")" execution get >"$current_file" 2>/dev/null; then
      pending=$(CLOUD_AGENTS_E2E_EXECUTION_FILE="$current_file" \
        CLOUD_AGENTS_E2E_HANDLED_APPROVALS="$handled_approvals" node <<'NODE'
const { readFileSync } = require("node:fs");
const value = JSON.parse(readFileSync(process.env.CLOUD_AGENTS_E2E_EXECUTION_FILE, "utf8"));
const handled = new Set((process.env.CLOUD_AGENTS_E2E_HANDLED_APPROVALS ?? "").split(" ").filter(Boolean));
for (const message of value.messages ?? []) {
  if (message.messageType !== "InteractionRequest") continue;
  const requestId = message.payload?.requestId;
  if (typeof requestId !== "string" || !Number.isSafeInteger(value.spec?.generation)) continue;
  if (message.payload?.interactionType === "user-input" && value.spec?.checkpoint?.pendingInteractionCount >= 1) {
    const questionId = message.payload?.questions?.[0]?.id;
    if (typeof questionId === "string" && questionId) {
      process.stdout.write(`user|${JSON.stringify({generation: value.spec.generation, requestId, questionId})}`);
      process.exit(0);
    }
  }
  if (message.payload?.interactionType === "approval" && !handled.has(requestId)) {
    process.stdout.write(`approval|${value.spec.generation}|${requestId}`);
    process.exit(0);
  }
}
if (!["queued", "running"].includes(value.spec?.state)) {
  console.error(JSON.stringify({
    state: value.spec?.state,
    errorCode: value.spec?.errorCode,
    attemptNumber: value.spec?.attemptNumber,
    recoveryState: value.spec?.recoveryState,
    recoveryMode: value.spec?.recoveryMode,
    checkpoint: value.spec?.checkpoint ? {
      sequence: value.spec.checkpoint.sequence,
      pendingInteractionCount: value.spec.checkpoint.pendingInteractionCount,
      pendingSideEffect: value.spec.checkpoint.pendingSideEffect,
    } : undefined,
    messages: (value.messages ?? []).map((message) => ({
      messageType: message.messageType,
      eventType: message.payload?.eventType,
      errorCode: message.error?.code ?? message.payload?.errorCode,
    })),
  }));
  process.exit(1);
}
NODE
      )
      case "$pending" in
        user\|*) printf '%s' "${pending#user|}" >"$interaction_file"; rm -f "$current_file"; return 0 ;;
        approval\|*)
          approval_rest=${pending#approval|}
          approval_generation=${approval_rest%%|*}
          approval_request=${approval_rest#*|}
          run_ctl --project "$CLOUD_AGENTS_PROJECT" --session "$session_id" --turn "$turn_id" --execution "$execution_id" \
            --request-id "$(bounded_request_id "$execution_id-recovery-approve-$attempt")" --idempotency-key "$(bounded_request_id "$execution_id-recovery-approve-$attempt")" \
            execution resolve-approval --generation "$approval_generation" \
            --interaction-request "$approval_request" --decision accept >/dev/null
          handled_approvals="$handled_approvals $approval_request"
          ;;
      esac
    fi
    sleep 1
  done
  echo "timed out waiting for capability-bound recovery user input" >&2
  return 1
}

assert_recovery_capabilities() {
  final_file=$1
  [ "$recovery_capability_bound" -eq 1 ] || return 0
  CLOUD_AGENTS_E2E_EXECUTION_FILE="$final_file" \
  CLOUD_AGENTS_E2E_MCP_SERVER_REFS_JSON="$recovery_mcp_refs_json" \
  CLOUD_AGENTS_E2E_SKILL_BUNDLE_REFS_JSON="$recovery_skill_refs_json" node <<'NODE'
const { readFileSync } = require("node:fs");
const value = JSON.parse(readFileSync(process.env.CLOUD_AGENTS_E2E_EXECUTION_FILE, "utf8"));
const expectedMcp = JSON.parse(process.env.CLOUD_AGENTS_E2E_MCP_SERVER_REFS_JSON)[0];
const expectedSkill = JSON.parse(process.env.CLOUD_AGENTS_E2E_SKILL_BUNDLE_REFS_JSON)[0];
const mcpRefs = value.spec?.mcpServerRefs ?? [];
const skillRefs = value.spec?.skillBundleRefs ?? [];
const calledMcp = (value.messages ?? []).some((message) =>
  message.messageType === "Event" && message.payload?.eventType === "item.completed" &&
  message.payload?.payload?.status === "completed" &&
  message.payload?.payload?.data?.capabilityResourceId === expectedMcp.serverId);
const usedSkill = (value.messages ?? []).some((message) =>
  message.messageType === "Event" && message.payload?.eventType === "item.completed" &&
  message.payload?.payload?.status === "completed" &&
  ["skill", "Skill"].includes(message.payload?.payload?.data?.sourceItemType) &&
  message.payload?.payload?.data?.capabilityResourceId === expectedSkill.bundleId);
if (!calledMcp || !usedSkill || mcpRefs.length !== 1 || mcpRefs[0]?.serverId !== expectedMcp.serverId ||
    skillRefs.length !== 1 || skillRefs[0]?.bundleId !== expectedSkill.bundleId) {
  throw new Error("recovery did not retain and use the bound MCP Server and Skill Bundle");
}
NODE
}

complete_side_effect_process_recovery() {
  session_id=$1
  turn_id=$2
  execution_id=$3
  prompt=$4
  final_file=$5
  marker=$6
  runtime_container=$7
  artifact_absolute="/workspace/.cloud-agents/managed-agent/tenants/$CLOUD_AGENTS_TENANT/projects/$CLOUD_AGENTS_PROJECT/sessions/$session_id/workspace/$recovery_artifact_path"
  expected_digest=$(CLOUD_AGENTS_E2E_EXPECTED_CONTENT="$recovery_expected_content" CLOUD_AGENTS_E2E_EXPECTED_CONTENT_EOL="$([ "$recovery_provider" = deepseek-harness ] || printf 1)" node -e '
    const content = process.env.CLOUD_AGENTS_E2E_EXPECTED_CONTENT;
    const value = process.env.CLOUD_AGENTS_E2E_EXPECTED_CONTENT_EOL === "1" ? `${content}\n` : content;
    process.stdout.write(require("node:crypto").createHash("sha256").update(value).digest("hex"));
  ')
  prepare_interaction_takeover "$session_id" "$turn_id" "$execution_id" full-access default "$prompt" "$final_file" "$recovery_capability_bound"
  reconcile_side_effect_takeover "$session_id" "$turn_id" "$execution_id" "$runtime_container" "$artifact_absolute" "$expected_digest"
  start_execution "$session_id" "$turn_id" "$execution_id" full-access default "$prompt" "$final_file" "$recovery_capability_bound"
  wait_for_success "$final_file" "$session_id" "$turn_id" "$execution_id"
  assert_side_effect_recovery_result "$final_file" 'process-restart|same-node-reconnect' "$marker"
  assert_recovery_capabilities "$final_file"
  printf 'process_side_effect_recovery=passed session=%s provider=%s capability_bound=%s\n' "$session_id" "$recovery_provider" "$recovery_capability_bound"
}

run_worker_exit_recovery() {
  case "$recovery_fault" in worker | both) ;; *) return 0 ;; esac
  [ -n "${CLOUD_AGENTS_E2E_WORKER_CONTAINER-}" ] || { echo "Worker fault target is required" >&2; return 1; }
  session_id="$CLOUD_AGENTS_E2E_RUN_ID-worker-exit"
  turn_id="$session_id-turn"
  execution_id="$session_id-execution"
  marker="worker-exit-recovered-$CLOUD_AGENTS_E2E_RUN_ID"
  prompt=$(recovery_prompt "$marker" "$session_id")
  create_turn "$recovery_provider" "$session_id" "$turn_id" "$prompt"
  final_file="$CLOUD_AGENTS_E2E_OUTPUT_DIR/$execution_id.json"
  interaction_file="$CLOUD_AGENTS_E2E_OUTPUT_DIR/$execution_id-interaction.json"
  if [ "$recovery_checkpoint_mode" = side-effect ]; then
    start_execution "$session_id" "$turn_id" "$execution_id" full-access default "$prompt" "$final_file" "$recovery_capability_bound"
    wait_for_recovery_side_effect_checkpoint "$session_id" "$turn_id" "$execution_id" "$final_file.checkpoint"
  else
    start_execution "$session_id" "$turn_id" "$execution_id" approval-required plan "$prompt" "$final_file" "$recovery_capability_bound"
    wait_for_recovery_interaction "$session_id" "$turn_id" "$execution_id" "$interaction_file"
  fi
  restart_worker_during_execution
  if [ "$recovery_environment" = docker ]; then
    restart_mcp_fixture_after_worker
  fi
  if [ -n "$execute_pid" ] && kill -0 "$execute_pid" 2>/dev/null; then
    kill "$execute_pid" >/dev/null 2>&1 || true
  fi
  if [ -n "$execute_pid" ]; then
    set +e
    wait "$execute_pid"
    set -e
    execute_pid=
  fi
  checkpoint_ready=0
  attempt=0
  while [ "$attempt" -lt 30 ]; do
    attempt=$((attempt + 1))
    run_ctl --project "$CLOUD_AGENTS_PROJECT" --session "$session_id" --turn "$turn_id" --execution "$execution_id" \
      --request-id "$(bounded_request_id "$execution_id-after-worker-exit-$attempt")" execution get >"$final_file.after-worker-exit"
    if CLOUD_AGENTS_E2E_EXECUTION_FILE="$final_file.after-worker-exit" CLOUD_AGENTS_E2E_CHECKPOINT_MODE="$recovery_checkpoint_mode" node <<'NODE'
const { readFileSync } = require("node:fs");
const value = JSON.parse(readFileSync(process.env.CLOUD_AGENTS_E2E_EXECUTION_FILE, "utf8"));
const ready = process.env.CLOUD_AGENTS_E2E_CHECKPOINT_MODE === "side-effect"
  ? value.spec?.checkpoint?.pendingSideEffect === true
  : value.spec?.checkpoint?.pendingInteractionCount >= 1;
process.exit(value.spec?.state === "running" && value.spec?.checkpoint?.sequence >= 1 && ready ? 0 : 1);
NODE
    then
      checkpoint_ready=1
      break
    fi
    sleep 1
  done
  if [ "$checkpoint_ready" -ne 1 ]; then
    CLOUD_AGENTS_E2E_EXECUTION_FILE="$final_file.after-worker-exit" node <<'NODE'
const { readFileSync } = require("node:fs");
const value = JSON.parse(readFileSync(process.env.CLOUD_AGENTS_E2E_EXECUTION_FILE, "utf8"));
process.stderr.write(`${JSON.stringify({state: value.spec?.state, errorCode: value.spec?.errorCode, attemptNumber: value.spec?.attemptNumber, recoveryState: value.spec?.recoveryState, checkpoint: value.spec?.checkpoint})}\n`);
throw new Error("Worker exit settled or lost the checkpointed interaction");
NODE
  fi
  if [ "$recovery_checkpoint_mode" = side-effect ]; then
    complete_side_effect_process_recovery "$session_id" "$turn_id" "$execution_id" "$prompt" "$final_file" "$marker" "$CLOUD_AGENTS_E2E_WORKER_CONTAINER"
    return 0
  fi
  prepare_interaction_takeover "$session_id" "$turn_id" "$execution_id" approval-required plan "$prompt" "$final_file" "$recovery_capability_bound"
  run_ctl --project "$CLOUD_AGENTS_PROJECT" --session "$session_id" --turn "$turn_id" --execution "$execution_id" \
    --request-id "$execution_id-resolve" execution resolve-user-input \
    --generation "$(interaction_field "$interaction_file" generation)" \
    --interaction-request "$(interaction_field "$interaction_file" requestId)" \
    --answers-json "$(question_id=$(interaction_field "$interaction_file" questionId); node -e 'process.stdout.write(JSON.stringify({[process.argv[1]]:["Staging"]}))' "$question_id")" >/dev/null
  if [ "$takeover_needs_reconcile" -eq 1 ]; then
    reconcile_interaction_takeover "$session_id" "$turn_id" "$execution_id"
  fi
  start_execution "$session_id" "$turn_id" "$execution_id" approval-required plan "$prompt" "$final_file" "$recovery_capability_bound"
  wait_for_success "$final_file" "$session_id" "$turn_id" "$execution_id"
  assert_interaction_takeover "$final_file" 'process-restart|same-node-reconnect'
  CLOUD_AGENTS_E2E_EXECUTION_FILE="$final_file" CLOUD_AGENTS_E2E_MARKER="$marker" node <<'NODE'
const { readFileSync } = require("node:fs");
const value = JSON.parse(readFileSync(process.env.CLOUD_AGENTS_E2E_EXECUTION_FILE, "utf8"));
if (!JSON.stringify(value.messages ?? []).includes(process.env.CLOUD_AGENTS_E2E_MARKER)) throw new Error("recovered Worker execution result changed");
NODE
  assert_recovery_capabilities "$final_file"
  printf 'worker_exit_survival=passed session=%s provider=%s capability_bound=%s\n' "$session_id" "$recovery_provider" "$recovery_capability_bound"
}

run_agent_exit_recovery() {
  case "$recovery_fault" in agent | both) ;; *) return 0 ;; esac
  [ -n "${CLOUD_AGENTS_E2E_AGENT_RUNTIME_ID-}" ] || { echo "Agent Runtime fault target is required" >&2; return 1; }
  [ -z "$lease_id" ] || { echo "Agent Runtime recovery requires direct Sandbox binding" >&2; return 1; }
  [ -n "${CLOUD_AGENTS_E2E_AGENT_TARGET_ID-}" ] || { echo "Agent Runtime target id is required" >&2; return 1; }
  session_id="$CLOUD_AGENTS_E2E_RUN_ID-agent-exit"
  turn_id="$session_id-turn"
  execution_id="$session_id-execution"
  # Keep the final response marker short; session/run ids already identify the recovery.
  marker=agent-exit-recovered
  prompt=$(recovery_prompt "$marker" "$session_id")
  create_turn "$recovery_provider" "$session_id" "$turn_id" "$prompt"
  final_file="$CLOUD_AGENTS_E2E_OUTPUT_DIR/$execution_id.json"
  interaction_file="$CLOUD_AGENTS_E2E_OUTPUT_DIR/$execution_id-interaction.json"
  if [ "$recovery_checkpoint_mode" = side-effect ]; then
    start_execution "$session_id" "$turn_id" "$execution_id" full-access default "$prompt" "$final_file" "$recovery_capability_bound"
    wait_for_recovery_side_effect_checkpoint "$session_id" "$turn_id" "$execution_id" "$final_file.checkpoint"
  else
    start_execution "$session_id" "$turn_id" "$execution_id" approval-required plan "$prompt" "$final_file" "$recovery_capability_bound"
    wait_for_recovery_interaction "$session_id" "$turn_id" "$execution_id" "$interaction_file"
  fi
  runtime_pid_digest=$(node -e 'process.stdout.write(require("node:crypto").createHash("sha256").update(process.argv[1]).digest("hex"))' "$execution_id")
  runtime_kill_command="
    pid=\$(cat '/tmp/cloud-agents-runtime/$runtime_pid_digest.pid') || exit 1
    case \"\$pid\" in ''|*[!0-9]*) exit 1;; esac
    if [ ! -r \"/proc/\$pid/cmdline\" ]; then
      kill -0 \"\$pid\" 2>/dev/null && exit 1
      exit 0
    fi
    runtime_cmdline=\$(tr '\000' ' ' <\"/proc/\$pid/cmdline\")
    case \" \$runtime_cmdline \" in
      *\" /usr/local/bin/cloud-agent-runtime \"*) ;;
      *) exit 1 ;;
    esac
    kill_descendants() {
      child_file=\"/proc/\$1/task/\$1/children\"
      [ -r \"\$child_file\" ] || return 0
      for child in \$(cat \"\$child_file\"); do
        kill_descendants \"\$child\"
        kill -KILL \"\$child\" 2>/dev/null || true
      done
    }
    kill_descendants \"\$pid\"
    runtime_stat=\$(cat \"/proc/\$pid/stat\") || exit 1
    runtime_stat=\${runtime_stat#*) }
    runtime_stat=\${runtime_stat#* }
    runtime_stat=\${runtime_stat#* }
    runtime_pgid=\${runtime_stat%% *}
    case \"\$runtime_pgid\" in
      ''|*[!0-9]*) exit 1 ;;
      0|1) exit 1 ;;
    esac
    kill -KILL -- \"-\$runtime_pgid\" 2>/dev/null || true
    kill -KILL \"\$pid\"
  "
  agent_runtime_container=
  if [ "$recovery_environment" = docker ]; then
    agent_runtime_container=$(docker ps -q --filter "label=opensandbox.io/id=$CLOUD_AGENTS_E2E_AGENT_RUNTIME_ID")
    if ! printf '%s\n' "$agent_runtime_container" | grep -Eq '^[0-9a-f]{12}$' ||
      [ "$(printf '%s\n' "$agent_runtime_container" | wc -l)" -ne 1 ]; then
      echo "Agent Runtime container inventory is not exact" >&2
      return 1
    fi
    docker exec "$agent_runtime_container" sh -c "$runtime_kill_command"
  else
    kill_result=$(run_ctl --project "$CLOUD_AGENTS_PROJECT" --sandbox "$sandbox_id" \
      --request-id "$execution_id-kill-runtime" sandbox exec \
      --expected-generation "$sandbox_generation" --command "$runtime_kill_command") || return 1
    CLOUD_AGENTS_E2E_KILL_RESULT="$kill_result" node -e '
      const value = JSON.parse(process.env.CLOUD_AGENTS_E2E_KILL_RESULT);
      if (value.exitCode !== 0) process.exit(1);'
  fi
  if wait "$execute_pid"; then
    echo "Agent execution completed after its Runtime process was killed" >&2
    return 1
  fi
  execute_pid=
  if [ "$recovery_provider" = deepseek-harness ]; then
    printf 'remote_worker_pty_fence=not-applicable provider=%s adapter=str_replace_editor\n' "$recovery_provider"
  else
    fence_remote_worker_pty
  fi
  run_ctl --project "$CLOUD_AGENTS_PROJECT" --session "$session_id" --turn "$turn_id" --execution "$execution_id" \
    --request-id "$(bounded_request_id "$execution_id-after-agent-exit")" execution get >"$final_file.after-agent-exit"
  CLOUD_AGENTS_E2E_EXECUTION_FILE="$final_file.after-agent-exit" CLOUD_AGENTS_E2E_CHECKPOINT_MODE="$recovery_checkpoint_mode" node <<'NODE'
const { readFileSync } = require("node:fs");
const value = JSON.parse(readFileSync(process.env.CLOUD_AGENTS_E2E_EXECUTION_FILE, "utf8"));
const pending = process.env.CLOUD_AGENTS_E2E_CHECKPOINT_MODE === "side-effect"
  ? value.spec?.checkpoint?.pendingSideEffect === true
  : value.spec?.checkpoint?.pendingInteractionCount >= 1;
if (value.spec?.state !== "running" || value.spec?.attemptNumber !== 1 || value.spec?.checkpoint?.sequence < 1 || !pending) {
  process.stderr.write(`${JSON.stringify({state: value.spec?.state, errorCode: value.spec?.errorCode, attemptNumber: value.spec?.attemptNumber, recoveryState: value.spec?.recoveryState, checkpoint: value.spec?.checkpoint})}\n`);
  throw new Error("Agent Runtime exit settled or lost the checkpointed interaction");
}
NODE
  if [ "$recovery_checkpoint_mode" = side-effect ]; then
    complete_side_effect_process_recovery "$session_id" "$turn_id" "$execution_id" "$prompt" "$final_file" "$marker" "$agent_runtime_container"
    return 0
  fi
  prepare_interaction_takeover "$session_id" "$turn_id" "$execution_id" approval-required plan "$prompt" "$final_file" "$recovery_capability_bound"
  run_ctl --project "$CLOUD_AGENTS_PROJECT" --session "$session_id" --turn "$turn_id" --execution "$execution_id" \
    --request-id "$execution_id-resolve" execution resolve-user-input \
    --generation "$(interaction_field "$interaction_file" generation)" \
    --interaction-request "$(interaction_field "$interaction_file" requestId)" \
    --answers-json "$(question_id=$(interaction_field "$interaction_file" questionId); node -e 'process.stdout.write(JSON.stringify({[process.argv[1]]:["Staging"]}))' "$question_id")" >/dev/null
  if [ "$takeover_needs_reconcile" -eq 1 ]; then
    reconcile_interaction_takeover "$session_id" "$turn_id" "$execution_id"
  fi
  start_execution "$session_id" "$turn_id" "$execution_id" approval-required plan "$prompt" "$final_file" "$recovery_capability_bound"
  wait_for_success "$final_file" "$session_id" "$turn_id" "$execution_id"
  assert_interaction_takeover "$final_file" 'process-restart|same-node-reconnect'
  CLOUD_AGENTS_E2E_EXECUTION_FILE="$final_file" CLOUD_AGENTS_E2E_MARKER="$marker" node <<'NODE'
const { readFileSync } = require("node:fs");
const value = JSON.parse(readFileSync(process.env.CLOUD_AGENTS_E2E_EXECUTION_FILE, "utf8"));
if (!JSON.stringify(value.messages ?? []).includes(process.env.CLOUD_AGENTS_E2E_MARKER)) throw new Error("recovered Agent execution result changed");
NODE
  assert_recovery_capabilities "$final_file"
  printf 'agent_process_exit_recovery=passed session=%s provider=%s capability_bound=%s\n' "$session_id" "$recovery_provider" "$recovery_capability_bound"
}

run_long_task() {
  session_id="$CLOUD_AGENTS_E2E_RUN_ID-long-task"
  turn_id="$session_id-turn"
  execution_id="$session_id-execution"
  prompt="Use the Bash tool exactly twice: first run sleep 35 && printf 'long-task-part-one\\n', then run sleep 35 && printf 'long-task-part-two\\n'. Do not finish or call another tool until both commands complete, then reply with both exact stdout lines."
  create_turn claudeAgent "$session_id" "$turn_id" "$prompt"
  final_file="$CLOUD_AGENTS_E2E_OUTPUT_DIR/$execution_id.json"
  start_execution "$session_id" "$turn_id" "$execution_id" full-access default "$prompt" "$final_file"
  started_epoch=$(date +%s)
  wait_for_success "$final_file" "$session_id" "$turn_id" "$execution_id"
  elapsed_seconds=$(( $(date +%s) - started_epoch ))
  if [ "$elapsed_seconds" -lt 60 ]; then
    echo "real long task completed below the required duration: ${elapsed_seconds}s" >&2
    return 1
  fi
  printf 'long_task_real_e2e=passed duration_seconds=%s\n' "$elapsed_seconds"
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
    --request-id "$(bounded_request_id "$execution_id-$action")" --idempotency-key "$(bounded_request_id "$execution_id-$action")" \
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

if [ "${CLOUD_AGENTS_E2E_RECOVERY_ONLY:-0}" = 1 ]; then
  case "$recovery_fault" in
    worker) interaction_phase=worker-recovery; [ -n "${CLOUD_AGENTS_E2E_WORKER_CONTAINER-}" ] && { [ -n "$lease_id" ] || [ "$recovery_environment" = remote-worker ]; } || { echo "worker recovery requires an exact Worker fault target and binding" >&2; exit 1; } ;;
    agent) interaction_phase=agent-recovery; [ -n "${CLOUD_AGENTS_E2E_AGENT_RUNTIME_ID-}" ] && [ -n "${CLOUD_AGENTS_E2E_AGENT_TARGET_ID-}" ] && [ -z "$lease_id" ] || { echo "agent recovery requires Agent Runtime fault target and direct Sandbox binding" >&2; exit 1; } ;;
    both) interaction_phase=worker-recovery; echo "recovery-only requires one fault target per invocation" >&2; exit 1 ;;
  esac
  run_worker_exit_recovery
  run_agent_exit_recovery
  interaction_phase=cleanup
  cleanup
  active_sessions=
  trap - EXIT HUP INT TERM
  printf '%s\n' "Agent Worker and Runtime recovery real E2E passed"
  exit 0
fi

interaction_phase=approval
approval_turn="$approval_session-turn"
approval_execution="$approval_session-execution"
approval_path=".cloud-agents-acceptance/$CLOUD_AGENTS_E2E_RUN_ID-approved.txt"
approval_prompt="Use the Write tool to create exactly one file at $approval_path containing the single line 'approved interaction E2E' followed by a newline. Do not modify any other file. Wait for approval when requested, then reply done."
create_turn claudeAgent "$approval_session" "$approval_turn" "$approval_prompt"
approval_final="$CLOUD_AGENTS_E2E_OUTPUT_DIR/$approval_execution.json"
approval_interaction="$CLOUD_AGENTS_E2E_OUTPUT_DIR/$approval_execution-interaction.json"
start_execution "$approval_session" "$approval_turn" "$approval_execution" approval-required default "$approval_prompt" "$approval_final"
  wait_for_interaction "$approval_session" "$approval_turn" "$approval_execution" approval "$approval_interaction"
  expect_stale_approval_rejected "$approval_session" "$approval_turn" "$approval_execution" "$approval_interaction"
  expect_cross_tenant_interaction_rejected "$approval_session" "$approval_turn" "$approval_execution" approval "$approval_interaction"
  expect_interaction_authorization_rejected "$approval_session" "$approval_turn" "$approval_execution" approval "$approval_interaction"
  expect_checkpoint_protocol_rejected "$approval_session" "$approval_turn" "$approval_execution"
if [ -n "${CLOUD_AGENTS_E2E_CONTROL_PLANE_CONTAINER-}" ]; then
  approval_interaction_after_restart="$approval_interaction.after-restart"
  restart_control_plane_while_waiting "$approval_session"
  wait_for_interaction "$approval_session" "$approval_turn" "$approval_execution" approval "$approval_interaction_after_restart"
  cmp "$approval_interaction" "$approval_interaction_after_restart"
  prepare_interaction_takeover "$approval_session" "$approval_turn" "$approval_execution" approval-required default "$approval_prompt" "$approval_final"
fi
run_ctl --project "$CLOUD_AGENTS_PROJECT" --session "$approval_session" --turn "$approval_turn" --execution "$approval_execution" \
  --request-id "$approval_execution-resolve" execution resolve-approval \
  --generation "$(interaction_field "$approval_interaction" generation)" \
  --interaction-request "$(interaction_field "$approval_interaction" requestId)" --decision accept >/dev/null
if [ -n "${CLOUD_AGENTS_E2E_CONTROL_PLANE_CONTAINER-}" ]; then
  if [ "$takeover_needs_reconcile" -eq 1 ]; then
    reconcile_interaction_takeover "$approval_session" "$approval_turn" "$approval_execution"
  fi
  start_execution "$approval_session" "$approval_turn" "$approval_execution" approval-required default "$approval_prompt" "$approval_final"
fi
wait_for_success "$approval_final" "$approval_session" "$approval_turn" "$approval_execution"
if [ -n "${CLOUD_AGENTS_E2E_CONTROL_PLANE_CONTAINER-}" ]; then
  assert_interaction_takeover "$approval_final"
fi
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

interaction_phase=user-input
input_turn="$input_session-turn"
input_execution="$input_session-execution"
input_prompt="Before replying, call request_user_input exactly once with one non-secret question asking which environment to use and offer Staging as an option. After the answer, reply with the selected environment and do not call any other tool."
create_turn codex "$input_session" "$input_turn" "$input_prompt"
input_final="$CLOUD_AGENTS_E2E_OUTPUT_DIR/$input_execution.json"
input_interaction="$CLOUD_AGENTS_E2E_OUTPUT_DIR/$input_execution-interaction.json"
start_execution "$input_session" "$input_turn" "$input_execution" approval-required plan "$input_prompt" "$input_final"
  wait_for_interaction "$input_session" "$input_turn" "$input_execution" user-input "$input_interaction"
  expect_stale_user_input_rejected "$input_session" "$input_turn" "$input_execution" "$input_interaction"
  expect_cross_tenant_interaction_rejected "$input_session" "$input_turn" "$input_execution" user-input "$input_interaction"
  expect_interaction_authorization_rejected "$input_session" "$input_turn" "$input_execution" user-input "$input_interaction"
if [ -n "${CLOUD_AGENTS_E2E_CONTROL_PLANE_CONTAINER-}" ]; then
  input_interaction_after_restart="$input_interaction.after-restart"
  restart_control_plane_while_waiting "$input_session"
  wait_for_interaction "$input_session" "$input_turn" "$input_execution" user-input "$input_interaction_after_restart"
  cmp "$input_interaction" "$input_interaction_after_restart"
  prepare_interaction_takeover "$input_session" "$input_turn" "$input_execution" approval-required plan "$input_prompt" "$input_final"
fi
question_id=$(interaction_field "$input_interaction" questionId)
answers_json=$(CLOUD_AGENTS_E2E_QUESTION_ID="$question_id" node -e 'process.stdout.write(JSON.stringify({[process.env.CLOUD_AGENTS_E2E_QUESTION_ID]:["Staging"]}))')
run_ctl --project "$CLOUD_AGENTS_PROJECT" --session "$input_session" --turn "$input_turn" --execution "$input_execution" \
  --request-id "$input_execution-resolve" execution resolve-user-input \
  --generation "$(interaction_field "$input_interaction" generation)" \
  --interaction-request "$(interaction_field "$input_interaction" requestId)" --answers-json "$answers_json" >/dev/null
if [ -n "${CLOUD_AGENTS_E2E_CONTROL_PLANE_CONTAINER-}" ]; then
  if [ "$takeover_needs_reconcile" -eq 1 ]; then
    reconcile_interaction_takeover "$input_session" "$input_turn" "$input_execution"
  fi
  start_execution "$input_session" "$input_turn" "$input_execution" approval-required plan "$input_prompt" "$input_final"
fi
wait_for_success "$input_final" "$input_session" "$input_turn" "$input_execution"
if [ -n "${CLOUD_AGENTS_E2E_CONTROL_PLANE_CONTAINER-}" ]; then
  assert_interaction_takeover "$input_final"
fi

interaction_phase=long-task
run_long_task
interaction_phase=worker-recovery
run_worker_exit_recovery
interaction_phase=agent-recovery
run_agent_exit_recovery
interaction_phase=cancel
run_controlled_stop cancel
interaction_phase=interrupt
run_controlled_stop interrupt

interaction_phase=cleanup
cleanup
active_sessions=
trap - EXIT HUP INT TERM
printf '%s\n' "Agent approval and user-input real E2E passed"
