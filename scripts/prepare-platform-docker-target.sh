#!/bin/sh

set -eu

: "${CLOUD_AGENTS_WORKER_IMAGE:?set the exact Worker image repository@sha256:digest}"
: "${CLOUD_AGENTS_WORKER_CREDENTIAL_REF:?set the target Worker credential volume name}"
: "${CLOUD_AGENTS_WORKER_CREDENTIAL_DIR:?set the source Worker credential directory}"
: "${CLOUD_AGENTS_PROVIDER_CREDENTIAL_REF:?set the target Provider credential volume name}"
: "${CLOUD_AGENTS_PROVIDER_CREDENTIAL_DIR:?set the source tenant Provider credential directory}"
: "${CLOUD_AGENTS_TENANT:?set the tenant id used by the Provider credential files}"

docker_cli=${DOCKER-docker}

command -v "$docker_cli" >/dev/null
for directory in "$CLOUD_AGENTS_WORKER_CREDENTIAL_DIR" "$CLOUD_AGENTS_PROVIDER_CREDENTIAL_DIR"; do
  if [ ! -d "$directory" ] || [ -L "$directory" ] || [ "$directory" = / ]; then
    echo "credential source directories must exist" >&2
    exit 1
  fi
  case "$directory" in
    /*) ;;
    *) echo "credential source directories must be absolute" >&2; exit 1 ;;
  esac
done
worker_credential_dir=$(CDPATH= cd -- "$CLOUD_AGENTS_WORKER_CREDENTIAL_DIR" && pwd -P)
provider_credential_dir=$(CDPATH= cd -- "$CLOUD_AGENTS_PROVIDER_CREDENTIAL_DIR" && pwd -P)
if [ "$worker_credential_dir" = "$provider_credential_dir" ] || [ "$CLOUD_AGENTS_WORKER_CREDENTIAL_REF" = "$CLOUD_AGENTS_PROVIDER_CREDENTIAL_REF" ]; then
  echo "Worker and Provider credential sources and volumes must be distinct" >&2
  exit 1
fi
for name in "$CLOUD_AGENTS_WORKER_CREDENTIAL_REF" "$CLOUD_AGENTS_PROVIDER_CREDENTIAL_REF" "$CLOUD_AGENTS_TENANT"; do
  case "$name" in
    *[!A-Za-z0-9._-]*|'') echo "tenant and volume names contain unsupported characters" >&2; exit 1 ;;
  esac
  if [ "${#name}" -gt 128 ]; then
    echo "tenant and volume names are too long" >&2
    exit 1
  fi
done
case "$CLOUD_AGENTS_WORKER_IMAGE" in
  *@sha256:*)
    image_repository=${CLOUD_AGENTS_WORKER_IMAGE%@sha256:*}
    image_digest=${CLOUD_AGENTS_WORKER_IMAGE##*@sha256:}
    ;;
  *) echo "Worker image must use an immutable sha256 digest" >&2; exit 1 ;;
esac
case "$image_repository" in
  *'@'*|'') echo "Worker image must use exactly one immutable sha256 digest" >&2; exit 1 ;;
esac
case "$image_digest" in
  *[!0-9a-f]*|'') echo "Worker image digest is invalid" >&2; exit 1 ;;
esac
if [ "${#image_digest}" -ne 64 ]; then
  echo "Worker image digest is invalid" >&2
  exit 1
fi
for file in server.crt server.key client-ca.crt admission-token; do
  if [ ! -f "$worker_credential_dir/$file" ] || [ -L "$worker_credential_dir/$file" ]; then
    echo "Worker credential directory is incomplete" >&2
    exit 1
  fi
done
set -- "$provider_credential_dir/$CLOUD_AGENTS_TENANT".*.json
if [ ! -f "$1" ]; then
  echo "Provider credential directory has no tenant envelope" >&2
  exit 1
fi
for file in "$@"; do
  if [ ! -f "$file" ] || [ -L "$file" ]; then
    echo "Provider credential directory contains an invalid tenant envelope" >&2
    exit 1
  fi
done

if ! "$docker_cli" image inspect "$CLOUD_AGENTS_WORKER_IMAGE" >/dev/null 2>&1; then
  "$docker_cli" pull "$CLOUD_AGENTS_WORKER_IMAGE" >/dev/null
fi
for volume in "$CLOUD_AGENTS_WORKER_CREDENTIAL_REF" "$CLOUD_AGENTS_PROVIDER_CREDENTIAL_REF"; do
  "$docker_cli" volume create "$volume" >/dev/null
  if ! "$docker_cli" run --rm --pull never --user 0 --entrypoint /bin/sh \
    -v "$volume:/target" "$CLOUD_AGENTS_WORKER_IMAGE" -ec \
    'test -z "$(find /target -mindepth 1 -maxdepth 1 -print -quit)"'; then
    echo "credential volume $volume is not empty; refusing to overwrite it" >&2
    exit 1
  fi
done

"$docker_cli" run --rm --pull never --user 0 --entrypoint /bin/sh \
  -v "$CLOUD_AGENTS_WORKER_CREDENTIAL_REF:/target" \
  -v "$worker_credential_dir:/source:ro" \
  "$CLOUD_AGENTS_WORKER_IMAGE" -ec \
  'cp /source/server.crt /source/server.key /source/client-ca.crt /source/admission-token /target/ && chown 1000:1000 /target/* && chmod 0400 /target/*'
"$docker_cli" run --rm --pull never --user 0 --entrypoint /bin/sh \
  -e "TENANT_ID=$CLOUD_AGENTS_TENANT" \
  -v "$CLOUD_AGENTS_PROVIDER_CREDENTIAL_REF:/target" \
  -v "$provider_credential_dir:/source:ro" \
  "$CLOUD_AGENTS_WORKER_IMAGE" -ec \
  'cp /source/"$TENANT_ID".*.json /target/ && chown 1000:1000 /target/* && chmod 0400 /target/*'

printf '%s\n' "Docker target credential volumes prepared"
