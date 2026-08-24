#!/usr/bin/env bash

set -euo pipefail

script_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
repo_root=$(cd -- "$script_dir/../../.." && pwd -P)
module_dir="$repo_root/services/control-plane"

if [[ $(go version | awk '{print $3}') != "go1.26.6" ]]; then
  echo "Go 1.26.6 is required" >&2
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
if [[ $harness_image != *@sha256:* ]]; then
  echo "CLOUD_AGENTS_EVIDENCEFS_HARNESS_IMAGE must be an exact local image digest" >&2
  exit 1
fi
if ! docker image inspect "$harness_image" >/dev/null 2>&1; then
  echo "Missing exact local image: $harness_image" >&2
  echo "Pull it explicitly before rerunning; this matrix never pulls implicitly." >&2
  exit 1
fi

image_id=$(docker image inspect --format '{{.Id}}' "$harness_image")
image_os=$(docker image inspect --format '{{.Os}}' "$harness_image")
image_arch=$(docker image inspect --format '{{.Architecture}}' "$harness_image")
case "$image_arch" in
  amd64 | x86_64) goarch=amd64 ;;
  arm64 | aarch64) goarch=arm64 ;;
  *)
    echo "Unsupported harness architecture: $image_arch" >&2
    exit 1
    ;;
esac
if [[ $image_id != sha256:* || $image_os != "linux" ]]; then
  echo "Harness image is not an exact Linux image" >&2
  exit 1
fi

temporary_dir=$(mktemp -d)
run_id="migration-evidence-sink-linux-matrix-$$-$RANDOM"
ownership_label="com.hxp0618.cloud-agents.test-run"
active_container=""

cleanup() {
  if [[ -n $active_container ]] && docker container inspect "$active_container" >/dev/null 2>&1; then
    observed_owner=$(docker container inspect --format "{{index .Config.Labels \"$ownership_label\"}}" "$active_container")
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

test_binary="$temporary_dir/migration-evidence-sink.test"
GOWORK=off GOTOOLCHAIN=local GOFLAGS=-mod=readonly \
  GOOS=linux GOARCH="$goarch" CGO_ENABLED=0 \
  go -C "$module_dir" test -c -tags=evidencefsintegration \
  -o "$test_binary" ./internal/migration
test_binary_sha256=$(sha256_file "$test_binary")

provision_binary="$temporary_dir/cloud-agents-evidencefs-provision"
GOWORK=off GOTOOLCHAIN=local GOFLAGS=-mod=readonly \
  GOOS=linux GOARCH="$goarch" CGO_ENABLED=0 \
  go -C "$module_dir" build \
  -o "$provision_binary" ./cmd/cloud-agents-evidencefs-provision
provision_binary_sha256=$(sha256_file "$provision_binary")

for filesystem in ext4 xfs; do
  active_container="cag-p1-migration-evidence-${filesystem}-$$-$RANDOM"
  docker run --rm --pull=never --privileged \
    --name "$active_container" \
    --label "$ownership_label=$run_id" \
    --mount "type=bind,src=$test_binary,dst=/usr/local/bin/migration-evidence-sink.test,readonly" \
    --mount "type=bind,src=$provision_binary,dst=/usr/local/bin/cloud-agents-evidencefs-provision,readonly" \
    --mount "type=bind,src=$module_dir,dst=/workspace,readonly" \
    -e "EVIDENCEFS_MATRIX_FS=$filesystem" \
    "$harness_image" sh -ceu '
      apk add --no-cache e2fsprogs=1.47.2-r2 xfsprogs=6.14.0-r0 >/dev/null
      mkdir -p /mnt/evidence
      if [ "$EVIDENCEFS_MATRIX_FS" = ext4 ]; then image_size=192M; else image_size=512M; fi
      truncate -s "$image_size" /tmp/evidencefs.img
      loopdev=$(losetup -f)
      losetup "$loopdev" /tmp/evidencefs.img
      associated=1
      mounted=0
      cleanup_container() {
        if [ "$mounted" = 1 ]; then umount /mnt/evidence 2>/dev/null || true; fi
        if [ "$associated" = 1 ]; then losetup -d "$loopdev" 2>/dev/null || true; fi
      }
      trap cleanup_container EXIT INT TERM
      if [ "$EVIDENCEFS_MATRIX_FS" = ext4 ]; then mkfs.ext4 -q -F "$loopdev"; else mkfs.xfs -q -f "$loopdev"; fi
      mount -t "$EVIDENCEFS_MATRIX_FS" "$loopdev" /mnt/evidence
      mounted=1
      losetup -d "$loopdev"
      associated=0
      if [ -d /mnt/evidence/lost+found ]; then rmdir /mnt/evidence/lost+found; fi
      runner_name=$(awk -F: '\''$3 == 1001 { print $1; exit }'\'' /etc/passwd)
      if [ -z "$runner_name" ]; then
        adduser -D -H -u 1001 evidence-runner
        runner_name=evidence-runner
      fi
      chown 1001:1001 /mnt/evidence
      chmod 0700 /mnt/evidence
      install -d -m 0700 -o 1001 -g 1001 /mnt/evidence/objects /mnt/evidence/objects/sha256
      install -m 0600 -o 1001 -g 1001 /dev/null /mnt/evidence/lineages.lock
      sync
      /usr/local/bin/cloud-agents-evidencefs-provision provision \
        --root /mnt/evidence \
        --runner-uid 1001 \
        --confirm-direct-local-mount
      authority_name=$(printf %s /mnt/evidence | sha256sum | cut -d " " -f 1).authority
      authority_path=/run/cloud-agents/evidencefs-mounts/$authority_name
      test -f "$authority_path"
      test "$(stat -c %u:%a:%h:%s "$authority_path")" = "0:444:1:224"
      su "$runner_name" -s /bin/sh -c "
        cd /workspace/internal/migration && \\
        CLOUD_AGENTS_REQUIRE_MIGRATION_EVIDENCE_SINK=1 \\
        CLOUD_AGENTS_EVIDENCEFS_INTEGRATION_ROOT=/mnt/evidence \\
        /usr/local/bin/migration-evidence-sink.test \\
          -test.run ^TestLinuxProductionEvidenceSinkBrandNewAndRegisteredReopen$ \\
          -test.count=1 -test.v
      "
      sync
      /usr/local/bin/cloud-agents-evidencefs-provision revoke --root /mnt/evidence
      test ! -e "$authority_path"
      su "$runner_name" -s /bin/sh -c "
        cd /workspace/internal/migration && \\
        CLOUD_AGENTS_REQUIRE_MIGRATION_EVIDENCE_SINK_REVOKED=1 \\
        CLOUD_AGENTS_EVIDENCEFS_INTEGRATION_ROOT=/mnt/evidence \\
        /usr/local/bin/migration-evidence-sink.test \\
          -test.run ^TestLinuxProductionEvidenceSinkOpenFailsAfterRevocation$ \\
          -test.count=1 -test.v
      "
      echo "MIGRATION_EVIDENCE_SINK_LINUX_ENV filesystem=$EVIDENCEFS_MATRIX_FS kernel=$(uname -r) runner_uid=1001 result=PASS"
      umount /mnt/evidence
      mounted=0
      trap - EXIT INT TERM
    '
  active_container=""
  echo "MIGRATION_EVIDENCE_SINK_LINUX_MATRIX filesystem=$filesystem image_ref=$harness_image image_id=$image_id arch=$image_os/$image_arch test_binary_sha256=$test_binary_sha256 provision_binary_sha256=$provision_binary_sha256 result=PASS"
done

residual=$(docker ps -aq --filter "label=$ownership_label=$run_id")
if [[ -n $residual ]]; then
  echo "Migration evidence sink matrix left owned containers behind: $residual" >&2
  exit 1
fi

echo "Migration EvidenceSink Linux ext4/xfs matrix: PASS"
