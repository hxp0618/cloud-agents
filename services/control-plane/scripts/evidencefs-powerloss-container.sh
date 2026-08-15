#!/bin/sh

set -eu

work_dir=${EVIDENCEFS_POWERLOSS_WORK_DIR:-/work}
test_binary=${EVIDENCEFS_POWERLOSS_TEST_BINARY:-/inputs/evidencefs.test}
guest_init=${EVIDENCEFS_POWERLOSS_GUEST_INIT:-/inputs/evidencefs-powerloss-guest-init.sh}

if [ ! -d "$work_dir" ] || [ ! -x "$test_binary" ] || [ ! -f "$guest_init" ]; then
  echo "power-loss container inputs are incomplete" >&2
  exit 1
fi

apk add --no-cache \
  alpine-base=3.22.5-r0 \
  e2fsprogs=1.47.2-r2 \
  linux-virt=6.12.103-r0 \
  qemu-img=10.0.0-r1 \
  qemu-system-aarch64=10.0.0-r1 \
  xfsprogs=6.14.0-r0 >/dev/null

root_image="$work_dir/guest-root.img"
root_mount="$work_dir/guest-root"
kernel=/boot/vmlinuz-virt
initramfs=/boot/initramfs-virt
root_loop=""
root_mounted=0
qemu_pid=""

cleanup() {
  if [ -n "$qemu_pid" ] && kill -0 "$qemu_pid" 2>/dev/null; then
    kill -KILL "$qemu_pid" 2>/dev/null || true
    wait "$qemu_pid" 2>/dev/null || true
  fi
  qemu_pid=""
  if [ "$root_mounted" = 1 ]; then
    umount "$root_mount" 2>/dev/null || true
  fi
  root_mounted=0
  if [ -n "$root_loop" ]; then
    losetup -d "$root_loop" 2>/dev/null || true
  fi
  root_loop=""
}

trap cleanup EXIT INT TERM

truncate -s 1G "$root_image"
root_loop=$(losetup -f)
losetup "$root_loop" "$root_image"
mkfs.ext4 -q -F -L evidence-rootfs "$root_loop"
mkdir -p "$root_mount"
mount -t ext4 "$root_loop" "$root_mount"
root_mounted=1

apk --root "$root_mount" --arch aarch64 --initdb --no-cache \
  --repositories-file /etc/apk/repositories \
  --keys-dir /etc/apk/keys \
  add \
  alpine-base=3.22.5-r0 \
  e2fsprogs=1.47.2-r2 \
  xfsprogs=6.14.0-r0 >/dev/null

guest_package_manifest=$(apk --root "$root_mount" manifest alpine-base e2fsprogs xfsprogs | LC_ALL=C sort | sha256sum | cut -d ' ' -f 1)

mkdir -p "$root_mount/lib" "$root_mount/mnt/evidence" "$root_mount/sbin" "$root_mount/usr/local/bin"
cp -a /lib/modules "$root_mount/lib/"
cp "$guest_init" "$root_mount/sbin/evidencefs-powerloss-init"
cp "$test_binary" "$root_mount/usr/local/bin/evidencefs.test"
chmod 0755 "$root_mount/sbin/evidencefs-powerloss-init" "$root_mount/usr/local/bin/evidencefs.test"
sync
umount "$root_mount"
root_mounted=0
losetup -d "$root_loop"
root_loop=""

package_manifest=$(apk manifest linux-virt qemu-img qemu-system-aarch64 e2fsprogs xfsprogs | LC_ALL=C sort | sha256sum | cut -d ' ' -f 1)
echo "EVIDENCEFS_QEMU_ENV kernel_package=linux-virt-6.12.103-r0 qemu=qemu-system-aarch64-10.0.0-r1 package_manifest_sha256=$package_manifest guest_package_manifest_sha256=$guest_package_manifest"

wait_for_marker() {
  log_file=$1
  marker=$2
  attempts=0
  while ! grep -F "$marker" "$log_file" >/dev/null 2>&1; do
    if ! kill -0 "$qemu_pid" 2>/dev/null; then
      wait "$qemu_pid" 2>/dev/null || true
      qemu_pid=""
      echo "QEMU exited before marker: $marker" >&2
      sed -n '1,240p' "$log_file" >&2
      return 1
    fi
    attempts=$((attempts + 1))
    if [ "$attempts" -ge 1800 ]; then
      echo "QEMU marker timeout: $marker" >&2
      sed -n '1,240p' "$log_file" >&2
      return 1
    fi
    sleep 0.1
  done
}

start_guest() {
  filesystem=$1
  mode=$2
  data_image=$3
  log_file=$4
  : >"$log_file"
  qemu-system-aarch64 \
    -machine virt,accel=tcg,gic-version=3 \
    -cpu cortex-a72 \
    -smp 2 \
    -m 768 \
    -nodefaults \
    -no-user-config \
    -no-reboot \
    -display none \
    -monitor none \
    -serial stdio \
    -kernel "$kernel" \
    -initrd "$initramfs" \
    -append "console=ttyAMA0 root=/dev/vda rootfstype=ext4 ro init=/sbin/evidencefs-powerloss-init evidencefs_fs=$filesystem evidencefs_mode=$mode" \
    -object rng-random,filename=/dev/urandom,id=rng0 \
    -device virtio-rng-pci,rng=rng0,addr=0x3 \
    -drive "if=none,file=$root_image,format=raw,readonly=on,id=root" \
    -device virtio-blk-pci,drive=root,addr=0x1 \
    -drive "if=none,file=$data_image,format=raw,cache=none,aio=threads,id=data" \
    -device virtio-blk-pci,drive=data,addr=0x2 \
    >"$log_file" 2>&1 &
  qemu_pid=$!
}

kill_guest_after_marker() {
  filesystem=$1
  mode=$2
  data_image=$3
  marker=$4
  log_file="$work_dir/$filesystem-$mode.log"
  start_guest "$filesystem" "$mode" "$data_image" "$log_file"
  wait_for_marker "$log_file" "$marker"
  kill -KILL "$qemu_pid"
  if wait "$qemu_pid" 2>/dev/null; then
    echo "QEMU power-loss target exited cleanly" >&2
    return 1
  fi
  qemu_pid=""
  echo "EVIDENCEFS_QEMU_POWER_LOSS filesystem=$filesystem mode=$mode marker=$marker result=KILLED"
}

verify_guest() {
  filesystem=$1
  mode=$2
  data_image=$3
  marker=$4
  log_file="$work_dir/$filesystem-$mode.log"
  start_guest "$filesystem" "$mode" "$data_image" "$log_file"
  wait_for_marker "$log_file" "$marker"
  if ! wait "$qemu_pid"; then
    qemu_pid=""
    echo "QEMU verifier failed after marker: $marker" >&2
    sed -n '1,240p' "$log_file" >&2
    return 1
  fi
  qemu_pid=""
  echo "EVIDENCEFS_QEMU_RESTART filesystem=$filesystem mode=$mode marker=$marker result=PASS"
}

for filesystem in ext4 xfs; do
  data_image="$work_dir/$filesystem-evidence.img"
  if [ "$filesystem" = ext4 ]; then
    truncate -s 192M "$data_image"
  else
    truncate -s 512M "$data_image"
  fi
  kill_guest_after_marker "$filesystem" create-object "$data_image" EVIDENCEFS_INTEGRATION_OBJECT_PUBLISHED_AND_ROOT_LOCKED
  verify_guest "$filesystem" verify-object "$data_image" "EVIDENCEFS_QEMU_VERIFY_OBJECT_PASS filesystem=$filesystem"
  kill_guest_after_marker "$filesystem" create-generation "$data_image" EVIDENCEFS_INTEGRATION_GENERATION_DURABLE_AND_LOCKED
  verify_guest "$filesystem" verify-all "$data_image" "EVIDENCEFS_QEMU_VERIFY_ALL_PASS filesystem=$filesystem"
  echo "EVIDENCEFS_QEMU_MATRIX filesystem=$filesystem result=PASS"
done

trap - EXIT INT TERM
cleanup
echo "Evidencefs Linux ext4/xfs QEMU power-loss matrix: PASS"
