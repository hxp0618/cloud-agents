#!/bin/sh

set -eu

export PATH=/sbin:/usr/sbin:/bin:/usr/bin:/usr/local/bin

mount -t proc proc /proc 2>/dev/null || true
mount -t sysfs sysfs /sys 2>/dev/null || true
mount -t devtmpfs devtmpfs /dev 2>/dev/null || true
mount -t tmpfs tmpfs /run

filesystem=""
mode=""
barrier=""
IFS= read -r kernel_command_line </proc/cmdline
for argument in $kernel_command_line; do
  case "$argument" in
    evidencefs_fs=*) filesystem=${argument#evidencefs_fs=} ;;
    evidencefs_mode=*) mode=${argument#evidencefs_mode=} ;;
    evidencefs_barrier=*) barrier=${argument#evidencefs_barrier=} ;;
  esac
done

case "$filesystem" in
  ext4 | xfs) ;;
  *)
    echo "invalid evidencefs guest filesystem" >&2
    poweroff -f
    exit 1
    ;;
esac
case "$mode" in
  create-object | verify-object | create-generation | verify-all | crash-object | classify-object) ;;
  *)
    echo "invalid evidencefs guest mode" >&2
    poweroff -f
    exit 1
    ;;
esac

modprobe virtio_blk 2>/dev/null || true
modprobe "$filesystem" 2>/dev/null || true
mkdir -p /mnt/evidence

if [ "$mode" = create-object ] || [ "$mode" = crash-object ]; then
  if [ "$filesystem" = ext4 ]; then
    mkfs.ext4 -q -F /dev/vdb
  else
    mkfs.xfs -q -f /dev/vdb
  fi
fi

mount -t "$filesystem" /dev/vdb /mnt/evidence

if [ "$mode" = create-object ] || [ "$mode" = crash-object ]; then
  if [ -d /mnt/evidence/lost+found ]; then
    rmdir /mnt/evidence/lost+found
  fi
  chmod 0700 /mnt/evidence
  mkdir -m 0700 /mnt/evidence/objects
  mkdir -m 0700 /mnt/evidence/objects/sha256
  touch /mnt/evidence/lineages.lock
  chmod 0600 /mnt/evidence/lineages.lock
  sync
fi

run_test() {
  env \
    CLOUD_AGENTS_REQUIRE_EVIDENCEFS_LINUX_INTEGRATION=1 \
    CLOUD_AGENTS_EVIDENCEFS_INTEGRATION_FS="$filesystem" \
    CLOUD_AGENTS_EVIDENCEFS_INTEGRATION_ROOT=/mnt/evidence \
    "$@" \
    /usr/local/bin/evidencefs.test \
    -test.run '^TestLinuxIntegrationDurabilityRestartAndCrossProcessLocks$' \
    -test.count=1 \
    -test.v
}

run_holder() {
  hold_fifo=/run/evidencefs-holder
  rm -f "$hold_fifo"
  mkfifo "$hold_fifo"
  exec 3<>"$hold_fifo"
  if run_test "$@" <"$hold_fifo"; then
    result=0
  else
    result=$?
  fi
  exec 3>&-
  rm -f "$hold_fifo"
  return "$result"
}

case "$mode" in
  create-object)
    run_holder CLOUD_AGENTS_EVIDENCEFS_INTEGRATION_HELPER=publish-hold || true
    echo "object holder exited before guest power loss" >&2
    ;;
  verify-object)
    run_test CLOUD_AGENTS_EVIDENCEFS_INTEGRATION_HELPER=verify-object
    umount /mnt/evidence
    echo "EVIDENCEFS_QEMU_VERIFY_OBJECT_PASS filesystem=$filesystem"
    ;;
  create-generation)
    run_holder CLOUD_AGENTS_EVIDENCEFS_INTEGRATION_HELPER=generation-hold || true
    echo "generation holder exited before guest power loss" >&2
    ;;
  verify-all)
    run_test CLOUD_AGENTS_EVIDENCEFS_VERIFY_EXISTING=1
    umount /mnt/evidence
    echo "EVIDENCEFS_QEMU_VERIFY_ALL_PASS filesystem=$filesystem"
    ;;
  crash-object)
    if [ -z "$barrier" ]; then
      echo "object crash barrier is required" >&2
      poweroff -f
      exit 1
    fi
    run_holder \
      CLOUD_AGENTS_EVIDENCEFS_INTEGRATION_HELPER=publish-crash \
      CLOUD_AGENTS_EVIDENCEFS_INTEGRATION_BARRIER="$barrier" || true
    echo "object crash helper exited before guest power loss" >&2
    ;;
  classify-object)
    if [ -z "$barrier" ]; then
      echo "object classification barrier is required" >&2
      poweroff -f
      exit 1
    fi
    run_test \
      CLOUD_AGENTS_EVIDENCEFS_INTEGRATION_HELPER=classify-object-crash \
      CLOUD_AGENTS_EVIDENCEFS_INTEGRATION_BARRIER="$barrier"
    umount /mnt/evidence
    echo "EVIDENCEFS_QEMU_CLASSIFY_OBJECT_PASS filesystem=$filesystem barrier=$barrier"
    ;;
esac

poweroff -f
