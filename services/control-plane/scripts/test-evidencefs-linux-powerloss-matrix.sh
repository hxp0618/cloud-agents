#!/usr/bin/env bash

set -euo pipefail

script_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
repo_root=$(cd -- "$script_dir/../../.." && pwd -P)
module_dir="$repo_root/services/control-plane"

if [[ $(go version | awk '{print $3}') != "go1.26.5" ]]; then
  echo "Go 1.26.5 is required" >&2
  exit 1
fi
if ! docker version >/dev/null 2>&1; then
  echo "A running Docker daemon is required" >&2
  exit 1
fi

sha256_file() {
  local file=$1
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$file" | awk '{print $1}'
    return
  fi
  if command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$file" | awk '{print $1}'
    return
  fi
  echo "sha256sum or shasum is required" >&2
  return 1
}

harness_image=${CLOUD_AGENTS_EVIDENCEFS_HARNESS_IMAGE:-}
apk_repository=${CLOUD_AGENTS_EVIDENCEFS_APK_REPOSITORY:-}
matrix_scope=${CLOUD_AGENTS_EVIDENCEFS_MATRIX_SCOPE:-full}
if [[ $harness_image != *@sha256:* ]]; then
  echo "CLOUD_AGENTS_EVIDENCEFS_HARNESS_IMAGE must be an exact local image digest" >&2
  exit 1
fi

case $apk_repository in
  "" | https://dl-cdn.alpinelinux.org/alpine | https://mirrors.tuna.tsinghua.edu.cn/alpine) ;;
  *)
    echo "CLOUD_AGENTS_EVIDENCEFS_APK_REPOSITORY is not an allowed exact repository" >&2
    exit 1
    ;;
esac
case $matrix_scope in
  full | generation-header | generation-repair) ;;
  *)
    echo "CLOUD_AGENTS_EVIDENCEFS_MATRIX_SCOPE is not allowed" >&2
    exit 1
    ;;
esac
if ! docker image inspect "$harness_image" >/dev/null 2>&1; then
  echo "Missing exact local image: $harness_image" >&2
  echo "Pull it explicitly before rerunning; this matrix never pulls implicitly." >&2
  exit 1
fi

image_id=$(docker image inspect --format '{{.Id}}' "$harness_image")
image_os=$(docker image inspect --format '{{.Os}}' "$harness_image")
image_arch=$(docker image inspect --format '{{.Architecture}}' "$harness_image")
if [[ $image_id != sha256:* || $image_os != linux || $image_arch != arm64 ]]; then
  echo "Power-loss harness requires an exact Linux/arm64 image" >&2
  exit 1
fi

loop_before=$(docker run --rm --pull=never --privileged "$harness_image" sh -ceu 'losetup -a | LC_ALL=C sort')

temporary_dir=$(mktemp -d)
run_id="evidencefs-qemu-powerloss-$$-$RANDOM"
ownership_label="com.hxp0618.cloud-agents.powerloss-test-run"
active_container=""

cleanup() {
  if [[ -n $active_container ]] && docker container inspect "$active_container" >/dev/null 2>&1; then
    observed_owner=$(docker container inspect \
      --format "{{index .Config.Labels \"$ownership_label\"}}" \
      "$active_container")
    if [[ $observed_owner != "$run_id" ]]; then
      echo "Refusing to remove container not owned by this run: $active_container" >&2
      return 1
    fi
    docker rm -f -v "$active_container" >/dev/null
  fi
  active_container=""
  if [[ -d $temporary_dir ]]; then
    find "$temporary_dir" -depth -delete
  fi
}

on_signal() {
  local exit_code=$1
  trap - EXIT INT TERM
  cleanup || true
  exit "$exit_code"
}

trap cleanup EXIT
trap 'on_signal 130' INT
trap 'on_signal 143' TERM

binary="$temporary_dir/evidencefs-linux-integration.test"
GOWORK=off GOTOOLCHAIN=local GOFLAGS=-mod=readonly \
  GOOS=linux GOARCH=arm64 CGO_ENABLED=0 \
  go -C "$module_dir" test -c -tags=evidencefsintegration \
  -o "$binary" ./internal/evidencefs
binary_sha256=$(sha256_file "$binary")

active_container="cag-p1-evidencefs-qemu-$$-$RANDOM"
repository_args=()
if [[ -n $apk_repository ]]; then
  repository_args+=(--env "EVIDENCEFS_POWERLOSS_APK_REPOSITORY=$apk_repository")
fi
repository_args+=(--env "EVIDENCEFS_POWERLOSS_MATRIX_SCOPE=$matrix_scope")
docker run --rm --pull=never --privileged \
  --name "$active_container" \
  --label "$ownership_label=$run_id" \
  --mount "type=bind,src=$temporary_dir,dst=/work" \
  --mount "type=bind,src=$binary,dst=/inputs/evidencefs.test,readonly" \
  --mount "type=bind,src=$script_dir/evidencefs-powerloss-guest-init.sh,dst=/inputs/evidencefs-powerloss-guest-init.sh,readonly" \
  --mount "type=bind,src=$script_dir/evidencefs-powerloss-container.sh,dst=/usr/local/bin/evidencefs-powerloss-container,readonly" \
  "${repository_args[@]}" \
  "$harness_image" /usr/local/bin/evidencefs-powerloss-container
active_container=""

residual=$(docker ps -aq --filter "label=$ownership_label=$run_id")
if [[ -n $residual ]]; then
  echo "Evidencefs QEMU matrix left owned containers behind: $residual" >&2
  exit 1
fi
loop_after=$(docker run --rm --pull=never --privileged "$harness_image" sh -ceu 'losetup -a | LC_ALL=C sort')
if [[ $loop_after != "$loop_before" ]]; then
  echo "Evidencefs QEMU matrix changed the host loop-device set" >&2
  exit 1
fi

echo "EVIDENCEFS_QEMU_HOST image_ref=$harness_image image_id=$image_id arch=$image_os/$image_arch test_binary_sha256=$binary_sha256 result=PASS"
case $matrix_scope in
  full) echo "Evidencefs Linux ext4/xfs isolated QEMU power-loss matrix: PASS" ;;
  generation-header) echo "Evidencefs Linux ext4/xfs isolated QEMU generation-header power-loss matrix: PASS" ;;
  generation-repair) echo "Evidencefs Linux ext4/xfs isolated QEMU generation-repair power-loss matrix: PASS" ;;
esac
