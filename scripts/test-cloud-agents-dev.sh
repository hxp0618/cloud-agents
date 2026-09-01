#!/usr/bin/env bash

set -euo pipefail

repository_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
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

read -r control_plane_port worker_port < <(node <<'NODE'
const net = require("node:net");
const servers = [net.createServer(), net.createServer()];
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

CLOUD_AGENTS_DEV_CONTROL_PLANE_LISTEN="127.0.0.1:$control_plane_port" \
CLOUD_AGENTS_DEV_WORKER_LISTEN="127.0.0.1:$worker_port" \
  bash "$repository_root/scripts/cloud-agents-dev.sh" >"$log_file" 2>&1 &
dev_pid=$!
container_name="cloud-agents-dev-${UID:-0}-$dev_pid"

for attempt in {1..180}; do
  if grep -Fq "Token file: " "$log_file"; then
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

token_file=$(sed -n 's/^Token file: //p' "$log_file" | tail -n 1)
case "$token_file" in
  "$repository_root"/.tmp/cloud-agents-dev.*/control-plane.token) ;;
  *) echo "cloud-agents dev did not report an owned token file" >&2; exit 1 ;;
esac
state_directory=${token_file%/control-plane.token}
cli="$state_directory/bin/cloud-agentsctl"
common=(--endpoint "http://127.0.0.1:$control_plane_port" --token-file "$token_file" --tenant tenant-local)

project_json=$("$cli" "${common[@]}" --request-id localdev-smoke-project \
  --idempotency-key localdev-smoke-project-key project create --name localdev-smoke-project \
  --display-name "Localdev Smoke Project" --organization-id organization-local)
project_id=$(printf '%s' "$project_json" | node -e 'const fs=require("node:fs");process.stdout.write(JSON.parse(fs.readFileSync(0,"utf8")).metadata.uid)')
case "$project_id" in project-*) ;; *) echo "localdev project id is invalid" >&2; exit 1 ;; esac

"$cli" "${common[@]}" --project "$project_id" --session localdev-smoke-session \
  --request-id localdev-smoke-session --idempotency-key localdev-smoke-session-key \
  session create --provider unavailable-provider >/dev/null
set +e
execute_output=$("$cli" "${common[@]}" --project "$project_id" --session localdev-smoke-session \
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
"$cli" "${common[@]}" --project "$project_id" --session localdev-smoke-session \
  --turn localdev-smoke-turn --execution localdev-smoke-execution \
  --request-id localdev-smoke-execution-get execution get | node -e '
const fs = require("node:fs");
const execution = JSON.parse(fs.readFileSync(0, "utf8"));
if (execution.spec?.state !== "failed" || execution.spec?.errorCode !== "provider_not_installed") process.exit(1);
'

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
