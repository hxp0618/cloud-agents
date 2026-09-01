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
trap 'rm -rf -- "$smoke_directory"' 0 HUP INT TERM
tar -xf "$1" -C "$smoke_directory"

for image in control-plane worker migrate; do
  docker buildx build \
    --platform "$platform" \
    --file "$smoke_directory/deploy/docker/$image.Dockerfile" \
    --output type=cacheonly \
    "$candidate_directory"
done
