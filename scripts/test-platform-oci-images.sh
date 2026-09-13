#!/bin/sh

set -eu

if [ "$#" -ne 1 ] || [ ! -d "$1" ]; then
  echo "usage: test-platform-oci-images.sh PLATFORM_RELEASE_DIRECTORY" >&2
  exit 2
fi

candidate_directory=$(CDPATH= cd -- "$1" && pwd)
platform=${CLOUD_AGENTS_PLATFORM:-linux/amd64}
case "$platform" in
  linux/amd64 | linux/arm64) ;;
  *)
    echo "CLOUD_AGENTS_PLATFORM must be linux/amd64 or linux/arm64" >&2
    exit 2
    ;;
esac

set -- "$candidate_directory"/cloud-agents-deployment-*.tar
if [ "$#" -ne 1 ] || [ ! -f "$1" ]; then
  echo "platform release must contain exactly one deployment package" >&2
  exit 1
fi

smoke_directory=$(mktemp -d "$candidate_directory/.oci-smoke.XXXXXX")
image_prefix="cloud-agents-oci-smoke-$$"
cleanup() {
  rm -rf -- "$smoke_directory"
  for image in control-plane worker migrate; do
    docker image rm "$image_prefix:$image" >/dev/null 2>&1 || true
  done
}
trap cleanup 0 HUP INT TERM
tar -xf "$1" -C "$smoke_directory"
set -- "$candidate_directory"/cloud-agents-migrations-*.tar
if [ "$#" -ne 1 ] || [ ! -f "$1" ]; then
  echo "platform release must contain exactly one migration package" >&2
  exit 1
fi
migration_head=${1##*-}
migration_head=${migration_head%.tar}

for image in control-plane worker migrate; do
  docker buildx build \
    --platform "$platform" \
    --file "$smoke_directory/deploy/docker/$image.Dockerfile" \
    --tag "$image_prefix:$image" \
    --load \
    "$candidate_directory"
done

expect_startup_failure() {
  image=$1
  expected_exit=$2
  expected_message=$3
  set +e
  output=$(docker run --rm "$image_prefix:$image" 2>&1)
  exit_code=$?
  set -e
  if [ "$exit_code" -ne "$expected_exit" ]; then
    echo "$image image exited $exit_code, expected $expected_exit: $output" >&2
    exit 1
  fi
  case "$output" in
    *"$expected_message"*) ;;
    *)
      echo "$image image did not reach its application entry point: $output" >&2
      exit 1
      ;;
  esac
}

expect_startup_failure control-plane 2 "database, authentication, TLS, and access Grant configuration are required"
expect_startup_failure worker 2 "startup or shutdown failed"
expect_startup_failure migrate 1 "database URL, repository root, and product-$migration_head selector are required"

test "$(docker run --rm --entrypoint /usr/local/bin/codex "$image_prefix:worker" --version)" = "codex-cli 0.150.1"
test "$(docker run --rm --entrypoint /usr/local/bin/claude "$image_prefix:worker" --version)" = "2.1.207 (Claude Code)"
test "$(docker run --rm --entrypoint /usr/local/bin/dsh "$image_prefix:worker" --version)" = "0.1.2-rc.1"
docker run --rm --entrypoint /usr/bin/test "$image_prefix:migrate" \
	-r "/opt/cloud-agents/migrations/services/control-plane/migrations/product/$migration_head/manifest.json"

runtime_output=$(
  printf '%s\n' \
    '{"requestId":"oci-smoke-describe-codex","protocolVersion":{"major":2,"minor":3},"executionId":"oci-smoke","generation":1,"commandType":"Describe","commandId":"oci-smoke-describe-codex","occurredAt":"2026-09-01T00:00:00.000Z","payload":{"provider":"codex"}}' \
    '{"requestId":"oci-smoke-describe-claude","protocolVersion":{"major":2,"minor":3},"executionId":"oci-smoke","generation":1,"commandType":"Describe","commandId":"oci-smoke-describe-claude","occurredAt":"2026-09-01T00:00:00.000Z","payload":{"provider":"claudeAgent"}}' \
    '{"requestId":"oci-smoke-describe-pi","protocolVersion":{"major":2,"minor":3},"executionId":"oci-smoke","generation":1,"commandType":"Describe","commandId":"oci-smoke-describe-pi","occurredAt":"2026-09-01T00:00:00.000Z","payload":{"provider":"pi"}}' \
    '{"requestId":"oci-smoke-describe-dsh","protocolVersion":{"major":2,"minor":3},"executionId":"oci-smoke","generation":1,"commandType":"Describe","commandId":"oci-smoke-describe-dsh","occurredAt":"2026-09-01T00:00:00.000Z","payload":{"provider":"deepseek-harness"}}' |
    docker run --rm -i \
      --env CLOUD_AGENT_PROVIDER_HOST_EXPERIMENTAL_PROVIDERS=codex,claudeAgent,pi,deepseek-harness \
      --entrypoint /usr/local/bin/cloud-agent-runtime \
      "$image_prefix:worker" \
      --protocol-v2
)
case "$runtime_output" in
  *'"providerKind":"codex"'*'"name":"codex","version":"0.150.1","available":true,"compatible":true'*) ;;
  *) echo "Worker image Codex Runtime descriptor is unavailable or incompatible" >&2; exit 1 ;;
esac
case "$runtime_output" in
  *'"providerKind":"claudeAgent"'*'"name":"@anthropic-ai/claude-agent-sdk","version":"0.3.207","available":true,"compatible":true'*) ;;
  *) echo "Worker image Claude Runtime descriptor is unavailable or incompatible" >&2; exit 1 ;;
esac
case "$runtime_output" in
  *'"providerKind":"pi"'*'"name":"@earendil-works/pi-coding-agent","version":"0.85.1","available":true,"compatible":true'*) ;;
  *) echo "Worker image Pi Runtime descriptor is unavailable or incompatible" >&2; exit 1 ;;
esac
case "$runtime_output" in
  *'"providerKind":"deepseek-harness"'*'"name":"@deepseek-ai/dsh-sdk-client","version":"0.1.2-rc.1","available":true,"compatible":true'*) ;;
  *) echo "Worker image DeepSeek Harness Runtime descriptor is unavailable or incompatible" >&2; exit 1 ;;
esac
