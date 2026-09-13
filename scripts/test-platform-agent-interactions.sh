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
approval_session="$CLOUD_AGENTS_E2E_RUN_ID-approval"
input_session="$CLOUD_AGENTS_E2E_RUN_ID-user-input"
active_sessions=
execute_pid=
takeover_generation=
takeover_checkpoint_digest=
takeover_needs_reconcile=0

if [ ! -f "$CLOUD_AGENTS_TOKEN_FILE" ] || [ ! -d "$CLOUD_AGENTS_E2E_OUTPUT_DIR" ]; then
  echo "token file and E2E output directory must exist" >&2
  exit 1
fi
command -v "$cloud_agentsctl" >/dev/null
command -v node >/dev/null
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
  command -v docker >/dev/null
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

run_admin_ctl() {
  [ -n "$admin_token_file" ] || return 1
  if [ -n "$ca_file" ]; then
    "$cloud_agentsctl" --endpoint "$CLOUD_AGENTS_ENDPOINT" --ca-file "$ca_file" --token-file "$admin_token_file" --tenant "$CLOUD_AGENTS_TENANT" "$@"
  else
    "$cloud_agentsctl" --endpoint "$CLOUD_AGENTS_ENDPOINT" --token-file "$admin_token_file" --tenant "$CLOUD_AGENTS_TENANT" "$@"
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
  if [ -n "$lease_id" ]; then
    run_ctl --project "$CLOUD_AGENTS_PROJECT" --lease "$lease_id" --session "$session_id" \
      --request-id "$session_id-create" --idempotency-key "$session_id-create" session create --provider "$provider" >/dev/null
  else
    run_ctl --project "$CLOUD_AGENTS_PROJECT" --session "$session_id" \
      --request-id "$session_id-create" --idempotency-key "$session_id-create" \
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
const runtimeErrorCodes = (value.messages ?? []).flatMap((message) => message.messageType === "Error" && typeof message.error?.code === "string" ? [message.error.code] : []);
process.stderr.write(`${JSON.stringify({ state: value.spec?.state, errorCode: value.spec?.errorCode, recoveryState: value.spec?.recoveryState, attemptNumber: value.spec?.attemptNumber, runtimeErrorCodes })}\n`);
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
      --request-id "$execution_id-claim-expiry-$attempt" execution get >"$state_file" 2>/dev/null &&
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
  wait_for_claim_expiry "$session_id" "$turn_id" "$execution_id" "$output_prefix.claim-expired"
  set +e
  run_ctl --timeout 60s --project "$CLOUD_AGENTS_PROJECT" --session "$session_id" --turn "$turn_id" --execution "$execution_id" \
    --request-id "$execution_id-run" --idempotency-key "$execution_id-run" execution execute \
    --runtime-mode "$runtime_mode" --interaction-mode "$interaction_mode" --input "$prompt" \
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
}

assert_interaction_takeover() {
  final_file=$1
  expected_mode=${2-process-restart}
  CLOUD_AGENTS_E2E_EXECUTION_FILE="$final_file" CLOUD_AGENTS_E2E_RECOVERY_MODE="$expected_mode" node <<'NODE'
const { readFileSync } = require("node:fs");
const value = JSON.parse(readFileSync(process.env.CLOUD_AGENTS_E2E_EXECUTION_FILE, "utf8"));
if (value.spec?.attemptNumber !== 2 || value.spec?.recoveryState !== "recovered" || value.spec?.recoveryMode !== process.env.CLOUD_AGENTS_E2E_RECOVERY_MODE) {
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
      --request-id "$session_id-after-control-plane-restart-$attempt" session get >/dev/null 2>&1; then
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

restart_worker_during_execution() {
  previous_started_at=$(docker inspect --format '{{.State.StartedAt}}' "$CLOUD_AGENTS_E2E_WORKER_CONTAINER")
  docker kill --signal KILL "$CLOUD_AGENTS_E2E_WORKER_CONTAINER" >/dev/null
  attempt=0
  while [ "$attempt" -lt 90 ]; do
    attempt=$((attempt + 1))
    running=$(docker inspect --format '{{.State.Running}}' "$CLOUD_AGENTS_E2E_WORKER_CONTAINER" 2>/dev/null || printf 'false')
    started_at=$(docker inspect --format '{{.State.StartedAt}}' "$CLOUD_AGENTS_E2E_WORKER_CONTAINER" 2>/dev/null || true)
    if [ "$running" = true ] && [ "$started_at" != "$previous_started_at" ]; then
      return 0
    fi
    if [ "$running" != true ]; then
      docker start "$CLOUD_AGENTS_E2E_WORKER_CONTAINER" >/dev/null 2>&1 || true
    fi
    sleep 1
  done
  echo "Worker did not restart after the injected process exit" >&2
  return 1
}

run_worker_exit_recovery() {
  [ -n "${CLOUD_AGENTS_E2E_WORKER_CONTAINER-}" ] || return 0
  session_id="$CLOUD_AGENTS_E2E_RUN_ID-worker-exit"
  turn_id="$session_id-turn"
  execution_id="$session_id-execution"
  marker="worker-exit-recovered-$CLOUD_AGENTS_E2E_RUN_ID"
  prompt="Before replying, call request_user_input exactly once with one non-secret question asking which environment to use and offer Staging as an option. After the answer, reply with '$marker'."
  create_turn codex "$session_id" "$turn_id" "$prompt"
  final_file="$CLOUD_AGENTS_E2E_OUTPUT_DIR/$execution_id.json"
  interaction_file="$CLOUD_AGENTS_E2E_OUTPUT_DIR/$execution_id-interaction.json"
  start_execution "$session_id" "$turn_id" "$execution_id" approval-required plan "$prompt" "$final_file"
  wait_for_interaction "$session_id" "$turn_id" "$execution_id" user-input "$interaction_file"
  restart_worker_during_execution
  if [ -n "$execute_pid" ] && kill -0 "$execute_pid" 2>/dev/null; then
    kill "$execute_pid" >/dev/null 2>&1 || true
  fi
  if [ -n "$execute_pid" ]; then
    set +e
    wait "$execute_pid"
    set -e
    execute_pid=
  fi
  run_ctl --project "$CLOUD_AGENTS_PROJECT" --session "$session_id" --turn "$turn_id" --execution "$execution_id" \
    --request-id "$execution_id-after-worker-exit" execution get >"$final_file.after-worker-exit"
  CLOUD_AGENTS_E2E_EXECUTION_FILE="$final_file.after-worker-exit" node <<'NODE'
const { readFileSync } = require("node:fs");
const value = JSON.parse(readFileSync(process.env.CLOUD_AGENTS_E2E_EXECUTION_FILE, "utf8"));
if (value.spec?.state !== "running" || value.spec?.checkpoint?.sequence < 1 || value.spec?.checkpoint?.pendingInteractionCount < 1) throw new Error("Worker exit settled or lost the checkpointed interaction");
NODE
  run_ctl --project "$CLOUD_AGENTS_PROJECT" --session "$session_id" --turn "$turn_id" --execution "$execution_id" \
    --request-id "$execution_id-resolve" execution resolve-user-input \
    --generation "$(interaction_field "$interaction_file" generation)" \
    --interaction-request "$(interaction_field "$interaction_file" requestId)" \
    --answers-json "$(question_id=$(interaction_field "$interaction_file" questionId); node -e 'process.stdout.write(JSON.stringify({[process.argv[1]]:["Staging"]}))' "$question_id")" >/dev/null
  resolved_file="$final_file.after-worker-exit-resolved"
  execution_completed=0
  attempt=0
  while [ "$attempt" -lt 90 ]; do
    attempt=$((attempt + 1))
    if run_ctl --project "$CLOUD_AGENTS_PROJECT" --session "$session_id" --turn "$turn_id" --execution "$execution_id" \
      --request-id "$execution_id-after-worker-exit-resolved-$attempt" execution get >"$resolved_file" 2>/dev/null; then
      resolved_state=$(CLOUD_AGENTS_E2E_EXECUTION_FILE="$resolved_file" node <<'NODE'
const { readFileSync } = require("node:fs");
const value = JSON.parse(readFileSync(process.env.CLOUD_AGENTS_E2E_EXECUTION_FILE, "utf8"));
process.stdout.write(`${value.spec?.state ?? ""}|${Date.parse(value.spec?.claimExpiresAt ?? "") + 1000 < Date.now() ? 1 : 0}`);
NODE
      )
      resolved_execution_state=${resolved_state%%|*}
      resolved_claim_expired=${resolved_state#*|}
      if [ "$resolved_execution_state" = succeeded ]; then
        cp "$resolved_file" "$final_file"
        execution_completed=1
        break
      fi
      case "$resolved_execution_state" in
        failed | cancelled) echo "Worker exit execution became $resolved_execution_state after interaction resolution" >&2; return 1 ;;
        running) [ "$resolved_claim_expired" -eq 1 ] && break ;;
      esac
    fi
    sleep 1
  done
  if [ "$execution_completed" -ne 1 ]; then
    start_execution "$session_id" "$turn_id" "$execution_id" approval-required plan "$prompt" "$final_file"
    wait_for_success "$final_file" "$session_id" "$turn_id" "$execution_id"
  fi
  CLOUD_AGENTS_E2E_EXECUTION_FILE="$final_file" CLOUD_AGENTS_E2E_MARKER="$marker" node <<'NODE'
const { readFileSync } = require("node:fs");
const value = JSON.parse(readFileSync(process.env.CLOUD_AGENTS_E2E_EXECUTION_FILE, "utf8"));
const attemptNumber = value.spec?.attemptNumber;
if (![1, 2].includes(attemptNumber)) throw new Error("Worker exit lost the durable Agent execution claim");
if (attemptNumber === 2 && value.spec?.recoveryState !== "recovered") throw new Error("Worker exit takeover did not reconcile the durable execution");
if (!JSON.stringify(value.messages ?? []).includes(process.env.CLOUD_AGENTS_E2E_MARKER)) throw new Error("recovered Worker execution result changed");
NODE
  printf 'worker_exit_survival=passed session=%s\n' "$session_id"
}

run_agent_exit_recovery() {
  [ -n "${CLOUD_AGENTS_E2E_AGENT_RUNTIME_ID-}" ] || return 0
  [ -n "$lease_id" ] || return 0
  [ -n "${CLOUD_AGENTS_E2E_AGENT_TARGET_ID-}" ] || { echo "Agent Runtime target id is required" >&2; return 1; }
  session_id="$CLOUD_AGENTS_E2E_RUN_ID-agent-exit"
  turn_id="$session_id-turn"
  execution_id="$session_id-execution"
  marker="agent-exit-recovered-$CLOUD_AGENTS_E2E_RUN_ID"
  prompt="Before replying, call request_user_input exactly once with one non-secret question asking which environment to use and offer Staging as an option. After the answer, reply with '$marker'."
  create_turn codex "$session_id" "$turn_id" "$prompt"
  final_file="$CLOUD_AGENTS_E2E_OUTPUT_DIR/$execution_id.json"
  interaction_file="$CLOUD_AGENTS_E2E_OUTPUT_DIR/$execution_id-interaction.json"
  start_execution "$session_id" "$turn_id" "$execution_id" approval-required plan "$prompt" "$final_file"
  wait_for_interaction "$session_id" "$turn_id" "$execution_id" user-input "$interaction_file"
  agent_runtime_container=$(docker ps -q \
    --filter "label=cloud-agents.dev/tenant=$CLOUD_AGENTS_TENANT" \
    --filter "label=cloud-agents.dev/project=$CLOUD_AGENTS_PROJECT" \
    --filter "label=cloud-agents.dev/lease=$lease_id" \
    --filter "label=cloud-agents.dev/target=$CLOUD_AGENTS_E2E_AGENT_TARGET_ID")
  case "$agent_runtime_container" in
    '' | *' '*) echo "Agent Runtime container inventory is not exact" >&2; return 1 ;;
  esac
  docker exec -e CLOUD_AGENT_PROCESS=cloud-agent-runtime "$agent_runtime_container" sh -c '
    found=
    for command_line in /proc/[0-9]*/cmdline; do
      [ -r "$command_line" ] || continue
      runtime_arg=$(tr "\000" "\n" <"$command_line" 2>/dev/null | sed -n '2p' || true)
      if [ "$runtime_arg" = "/usr/local/bin/$CLOUD_AGENT_PROCESS" ]; then
          pid=${command_line#/proc/}
          pid=${pid%/cmdline}
          kill -KILL "$pid"
          found=1
      fi
    done
    [ -n "$found" ]
  '
  if wait "$execute_pid"; then
    echo "Agent execution completed after its Runtime process was killed" >&2
    return 1
  fi
  execute_pid=
  run_ctl --project "$CLOUD_AGENTS_PROJECT" --session "$session_id" --turn "$turn_id" --execution "$execution_id" \
    --request-id "$execution_id-after-agent-exit" execution get >"$final_file.after-agent-exit"
  CLOUD_AGENTS_E2E_EXECUTION_FILE="$final_file.after-agent-exit" node <<'NODE'
const { readFileSync } = require("node:fs");
const value = JSON.parse(readFileSync(process.env.CLOUD_AGENTS_E2E_EXECUTION_FILE, "utf8"));
if (value.spec?.state !== "running" || value.spec?.attemptNumber !== 1 || value.spec?.checkpoint?.sequence < 1 || value.spec?.checkpoint?.pendingInteractionCount < 1) {
  process.stderr.write(`${JSON.stringify({state: value.spec?.state, errorCode: value.spec?.errorCode, attemptNumber: value.spec?.attemptNumber, recoveryState: value.spec?.recoveryState, checkpoint: value.spec?.checkpoint})}\n`);
  throw new Error("Agent Runtime exit settled or lost the checkpointed interaction");
}
NODE
  prepare_interaction_takeover "$session_id" "$turn_id" "$execution_id" approval-required plan "$prompt" "$final_file"
  run_ctl --project "$CLOUD_AGENTS_PROJECT" --session "$session_id" --turn "$turn_id" --execution "$execution_id" \
    --request-id "$execution_id-resolve" execution resolve-user-input \
    --generation "$(interaction_field "$interaction_file" generation)" \
    --interaction-request "$(interaction_field "$interaction_file" requestId)" \
    --answers-json "$(question_id=$(interaction_field "$interaction_file" questionId); node -e 'process.stdout.write(JSON.stringify({[process.argv[1]]:["Staging"]}))' "$question_id")" >/dev/null
  if [ "$takeover_needs_reconcile" -eq 1 ]; then
    reconcile_interaction_takeover "$session_id" "$turn_id" "$execution_id"
  fi
  start_execution "$session_id" "$turn_id" "$execution_id" approval-required plan "$prompt" "$final_file"
  wait_for_success "$final_file" "$session_id" "$turn_id" "$execution_id"
  assert_interaction_takeover "$final_file" same-node-reconnect
  CLOUD_AGENTS_E2E_EXECUTION_FILE="$final_file" CLOUD_AGENTS_E2E_MARKER="$marker" node <<'NODE'
const { readFileSync } = require("node:fs");
const value = JSON.parse(readFileSync(process.env.CLOUD_AGENTS_E2E_EXECUTION_FILE, "utf8"));
if (!JSON.stringify(value.messages ?? []).includes(process.env.CLOUD_AGENTS_E2E_MARKER)) throw new Error("recovered Agent execution result changed");
NODE
  printf 'agent_process_exit_recovery=passed session=%s\n' "$session_id"
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

run_long_task
run_worker_exit_recovery
run_agent_exit_recovery
run_controlled_stop cancel
run_controlled_stop interrupt

cleanup
active_sessions=
trap - EXIT HUP INT TERM
printf '%s\n' "Agent approval and user-input real E2E passed"
