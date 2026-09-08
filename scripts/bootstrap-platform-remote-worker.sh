#!/bin/sh

set -eu

required() {
  name=$1
  value=$2
  if [ -z "$value" ]; then
    echo "set $name" >&2
    exit 2
  fi
  case "$value" in
    *'
'*) echo "$name must be one line" >&2; exit 2 ;;
  esac
}

required CLOUD_AGENTS_PLATFORM_RELEASE_DIR "${CLOUD_AGENTS_PLATFORM_RELEASE_DIR:-}"
required CLOUD_AGENTS_REMOTE_WORKER_INSTALL_DIR "${CLOUD_AGENTS_REMOTE_WORKER_INSTALL_DIR:-}"
required CLOUD_AGENTS_REMOTE_WORKER_CONTROL_PLANE_URL "${CLOUD_AGENTS_REMOTE_WORKER_CONTROL_PLANE_URL:-}"
required CLOUD_AGENTS_REMOTE_WORKER_SERVER_CA_FILE "${CLOUD_AGENTS_REMOTE_WORKER_SERVER_CA_FILE:-}"
required CLOUD_AGENTS_REMOTE_WORKER_BOOTSTRAP_TOKEN_FILE "${CLOUD_AGENTS_REMOTE_WORKER_BOOTSTRAP_TOKEN_FILE:-}"
required CLOUD_AGENTS_REMOTE_WORKER_TENANT "${CLOUD_AGENTS_REMOTE_WORKER_TENANT:-}"
required CLOUD_AGENTS_REMOTE_WORKER_PROJECT "${CLOUD_AGENTS_REMOTE_WORKER_PROJECT:-}"
required CLOUD_AGENTS_REMOTE_WORKER_ENROLLMENT "${CLOUD_AGENTS_REMOTE_WORKER_ENROLLMENT:-}"
required CLOUD_AGENTS_REMOTE_WORKER_INCARNATION "${CLOUD_AGENTS_REMOTE_WORKER_INCARNATION:-}"
required CLOUD_AGENTS_REMOTE_WORKER_CAPABILITIES "${CLOUD_AGENTS_REMOTE_WORKER_CAPABILITIES:-}"
required CLOUD_AGENTS_REMOTE_WORKER_CAPACITY_CPU_MILLIS "${CLOUD_AGENTS_REMOTE_WORKER_CAPACITY_CPU_MILLIS:-}"
required CLOUD_AGENTS_REMOTE_WORKER_CAPACITY_MEMORY_BYTES "${CLOUD_AGENTS_REMOTE_WORKER_CAPACITY_MEMORY_BYTES:-}"
required CLOUD_AGENTS_REMOTE_WORKER_CAPACITY_DISK_BYTES "${CLOUD_AGENTS_REMOTE_WORKER_CAPACITY_DISK_BYTES:-}"

release_directory=$(CDPATH= cd -- "$CLOUD_AGENTS_PLATFORM_RELEASE_DIR" && pwd)
install_directory=$CLOUD_AGENTS_REMOTE_WORKER_INSTALL_DIR
case "$install_directory" in
  /*) ;;
  *) echo "CLOUD_AGENTS_REMOTE_WORKER_INSTALL_DIR must be absolute" >&2; exit 2 ;;
esac
if [ -e "$install_directory" ]; then
  echo "RemoteWorker install directory already exists" >&2
  exit 2
fi
parent_directory=${install_directory%/*}
[ -n "$parent_directory" ] || parent_directory=/
[ -d "$parent_directory" ] || {
  echo "RemoteWorker install parent does not exist" >&2
  exit 2
}

case "$(uname -s)/$(uname -m)" in
  Linux/x86_64 | Linux/amd64) target=linux-amd64 ;;
  Linux/aarch64 | Linux/arm64) target=linux-arm64 ;;
  *) echo "RemoteWorker bootstrap supports Linux amd64 or arm64" >&2; exit 2 ;;
esac
cli=$release_directory/cloud-agentsctl-$target
worker=$release_directory/cloud-agents-remote-worker-$target
for file in "$cli" "$worker" "$CLOUD_AGENTS_REMOTE_WORKER_SERVER_CA_FILE" "$CLOUD_AGENTS_REMOTE_WORKER_BOOTSTRAP_TOKEN_FILE"; do
  [ -f "$file" ] || {
    echo "RemoteWorker bootstrap input is missing" >&2
    exit 2
  }
done
[ -x "$cli" ] && [ -x "$worker" ] || {
  echo "RemoteWorker release binaries must be executable" >&2
  exit 2
}

enrollment_version=${CLOUD_AGENTS_REMOTE_WORKER_ENROLLMENT_RESOURCE_VERSION:-1}
case "$enrollment_version" in
  '' | *[!0-9]*) echo "RemoteWorker enrollment resource version must be a positive integer" >&2; exit 2 ;;
esac
[ "$enrollment_version" -ge 1 ] || {
  echo "RemoteWorker enrollment resource version must be a positive integer" >&2
  exit 2
}
certificate_version=$((enrollment_version + 1))
bootstrap_idempotency_key=$CLOUD_AGENTS_REMOTE_WORKER_ENROLLMENT
while [ "${#bootstrap_idempotency_key}" -lt 16 ]; do
  bootstrap_idempotency_key="${bootstrap_idempotency_key}0"
done
docker_endpoint=${CLOUD_AGENTS_REMOTE_WORKER_DOCKER_ENDPOINT:-}
credential_directory=${CLOUD_AGENTS_REMOTE_WORKER_CREDENTIAL_DIRECTORY:-}
credential_ref=${CLOUD_AGENTS_REMOTE_WORKER_CREDENTIAL_REF:-}
case ",$CLOUD_AGENTS_REMOTE_WORKER_CAPABILITIES," in
  *,docker,*)
    [ -n "$docker_endpoint" ] && [ -n "$credential_directory" ] && [ -n "$credential_ref" ] || {
      echo "Docker-capable RemoteWorker requires endpoint and credential inputs" >&2
      exit 2
    }
    ;;
esac

umask 077
staging_directory=$(mktemp -d "$parent_directory/.cloud-agents-remote-worker.XXXXXX")
secret_claimed=false
cleanup() {
  status=$?
  trap - 0 HUP INT TERM
  if [ "$status" -ne 0 ]; then
    if [ "$secret_claimed" = true ]; then
      echo "certificate issuance failed; enrollment material retained in $staging_directory for retry" >&2
    else
      case "$staging_directory" in
        "$parent_directory"/.cloud-agents-remote-worker.*) rm -rf -- "$staging_directory" ;;
      esac
    fi
  fi
  exit "$status"
}
trap cleanup 0
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM

cp "$worker" "$staging_directory/cloud-agents-remote-worker"
cp "$CLOUD_AGENTS_REMOTE_WORKER_SERVER_CA_FILE" "$staging_directory/control-plane-ca.pem"
chmod 0500 "$staging_directory/cloud-agents-remote-worker"
chmod 0400 "$staging_directory/control-plane-ca.pem"

claim_response=$("$cli" \
  --endpoint "$CLOUD_AGENTS_REMOTE_WORKER_CONTROL_PLANE_URL" \
  --ca-file "$staging_directory/control-plane-ca.pem" \
  --token-file "$CLOUD_AGENTS_REMOTE_WORKER_BOOTSTRAP_TOKEN_FILE" \
  --tenant "$CLOUD_AGENTS_REMOTE_WORKER_TENANT" \
  --project "$CLOUD_AGENTS_REMOTE_WORKER_PROJECT" \
  --enrollment "$CLOUD_AGENTS_REMOTE_WORKER_ENROLLMENT" \
  --request-id "$CLOUD_AGENTS_REMOTE_WORKER_ENROLLMENT" \
  --idempotency-key "$bootstrap_idempotency_key" \
  remote-worker-enrollment claim-secret \
  --expected-resource-version "$enrollment_version")
printf '%s\n' "$claim_response" >"$staging_directory/claim-response.json"
secret_claimed=true
enrollment_secret=$(sed -n 's/.*"enrollmentSecret":"\(carw1_[A-Za-z0-9_-]*\)".*/\1/p' "$staging_directory/claim-response.json")
case "$enrollment_secret" in
  carw1_*) ;;
  *) echo "RemoteWorker enrollment response did not contain a valid secret" >&2; exit 1 ;;
esac
printf '%s\n' "$enrollment_secret" >"$staging_directory/enrollment-secret"
rm -f -- "$staging_directory/claim-response.json"
unset claim_response enrollment_secret

"$cli" \
  --endpoint "$CLOUD_AGENTS_REMOTE_WORKER_CONTROL_PLANE_URL" \
  --ca-file "$staging_directory/control-plane-ca.pem" \
  --enrollment-secret-file "$staging_directory/enrollment-secret" \
  --tenant "$CLOUD_AGENTS_REMOTE_WORKER_TENANT" \
  --project "$CLOUD_AGENTS_REMOTE_WORKER_PROJECT" \
  --enrollment "$CLOUD_AGENTS_REMOTE_WORKER_ENROLLMENT" \
  --request-id "$CLOUD_AGENTS_REMOTE_WORKER_ENROLLMENT" \
  --idempotency-key "$bootstrap_idempotency_key" \
  remote-worker-enrollment issue-certificate \
  --expected-resource-version "$certificate_version" \
  --incarnation "$CLOUD_AGENTS_REMOTE_WORKER_INCARNATION" \
  --identity-file "$staging_directory/identity.pem" >/dev/null
rm -f -- "$staging_directory/enrollment-secret"
secret_claimed=false

shell_quote() {
  printf "'%s'" "$(printf '%s' "$1" | sed "s/'/'\"'\"'/g")"
}
{
  printf '#!/bin/sh\nset -eu\nexec '
  shell_quote "$install_directory/cloud-agents-remote-worker"
  for argument in \
    "--control-plane-url=$CLOUD_AGENTS_REMOTE_WORKER_CONTROL_PLANE_URL" \
    "--tenant=$CLOUD_AGENTS_REMOTE_WORKER_TENANT" \
    "--project=$CLOUD_AGENTS_REMOTE_WORKER_PROJECT" \
    "--enrollment=$CLOUD_AGENTS_REMOTE_WORKER_ENROLLMENT" \
    "--incarnation=$CLOUD_AGENTS_REMOTE_WORKER_INCARNATION" \
    "--certificate=$install_directory/identity.pem" \
    "--private-key=$install_directory/identity.pem" \
    "--server-ca=$install_directory/control-plane-ca.pem" \
    "--state-file=$install_directory/state.json" \
    "--docker-endpoint=$docker_endpoint" \
    "--credential-directory=$credential_directory" \
    "--credential-ref=$credential_ref" \
    "--kernel-version=$(uname -r)" \
    "--capabilities=$CLOUD_AGENTS_REMOTE_WORKER_CAPABILITIES" \
    "--capacity-cpu-millis=$CLOUD_AGENTS_REMOTE_WORKER_CAPACITY_CPU_MILLIS" \
    "--capacity-memory-bytes=$CLOUD_AGENTS_REMOTE_WORKER_CAPACITY_MEMORY_BYTES" \
    "--capacity-disk-bytes=$CLOUD_AGENTS_REMOTE_WORKER_CAPACITY_DISK_BYTES"
  do
    printf ' \\\n  '
    shell_quote "$argument"
  done
  printf ' "$@"\n'
} >"$staging_directory/run.sh"
chmod 0500 "$staging_directory/run.sh"
mv "$staging_directory" "$install_directory"
trap - 0 HUP INT TERM
printf 'RemoteWorker installed at %s; run %s/run.sh under a local service supervisor.\n' "$install_directory" "$install_directory"
