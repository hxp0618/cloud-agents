# P1 evidencefs Linux ext4/xfs isolated QEMU power-loss matrix — 2026-08-16

- Status：**IMPLEMENTATION EVIDENCE — PASS；Gate OPEN**
- Fixed source：`3d21e908e0410d23ba44ee5499e615f6f837c4c7`
- Source tree：`a3e2db1d52326ebc9671468decd5b3c9ba87b43e`
- Branch：`codex/cloud-agents-platform-p1`
- Evidence snapshot：`2026-08-15T20:19:59Z`
- Executor：Codex P1 implementation executor
- Independent Gate reviewer：**not assigned**

## 1. Scope

This record fixes an isolated virtual-machine power-cycle extension to the exact test-only source
from [`evidencefs-linux-generation-matrix-20260816.md`](evidencefs-linux-generation-matrix-20260816.md).
The existing object/generation record remains historical and unchanged.

The host wrapper cross-compiles the tagged Linux/arm64 evidencefs test, then launches a one-shot
privileged container from an exact local image. Inside that container it builds a read-only Alpine
guest root, starts `qemu-system-aarch64` with TCG, and attaches a dedicated raw evidence disk with
`cache=none,aio=threads`. The guest root and evidence disk are separate virtio block devices.

For each of ext4 and xfs, the fixed run executes four fresh guest boots:

1. format the evidence disk, prepare the closed root grammar, publish and bind the fixed object,
   wait for the durable-ready marker, then send `SIGKILL` to the entire QEMU process without guest
   unmount, sync, poweroff or `Lease.Close`;
2. start a new QEMU process on the same raw disk, mount it, verify the exact object, cleanly unmount
   and power off;
3. start another guest, execute target registration, generation header/index durability, handoff,
   existing-segment append and rotated-segment append, wait for the durable-ready marker, then kill
   the entire QEMU process without guest cleanup or `GenerationLease.Close`; and
4. start a final new guest on the same raw disk, mount it, require production `Open` to remain
   rejecting, verify the exact object/target/journal/index/two-segment bytes, cleanly unmount and
   power off.

The QEMU process is the host of the isolated guest. Killing it terminates the guest kernel and its
mount namespace abruptly; this is materially stronger than killing only the writer process or
performing a clean container unmount. It remains a virtual guest-host power-cycle test, not a claim
about physical storage-controller caches or bare-metal host power removal.

## 2. Fixed environment and artifacts

| Input                        | Exact value                                                                      |
| ---------------------------- | -------------------------------------------------------------------------------- |
| Fixed evidencefs test source | `180929ad9a5723fbeef3ea36ca21678caf791501`                                       |
| Integration test binary      | SHA-256 `ca65ec93ef7b598f1c3f6b3a1b67b157d1d11c27a65379c5037b87caf23f6b0e`       |
| Base image ref               | `alpine@sha256:14358309a308569c32bdc37e2e0e9694be33a9d99e68afb0f5ff33cc1f695dce` |
| Local image ID               | `sha256:2c15e55df5d63efb31b629a557df305130612a16feb029c93447e54dda2c4189`        |
| QEMU                         | `qemu-system-aarch64-10.0.0-r1`; TCG `virt`, Cortex-A72, 2 vCPU, 768 MiB         |
| Guest kernel                 | `linux-virt-6.12.103-r0` / `6.12.103-0-virt`                                     |
| ext4 tooling                 | `e2fsprogs-1.47.2-r2`                                                            |
| xfs tooling                  | `xfsprogs-6.14.0-r0`                                                             |
| Container package manifest   | SHA-256 `d4db731ed3d00840ece5c114808335af1ac1be3bf6be74f4dd5df590f76e54f0`       |
| Guest package manifest       | SHA-256 `1a2aefaecd9def95205dd19290faa6b0b2a89a1c394fb73aa05820f97f643b38`       |
| Guest init script            | SHA-256 `5a156c3c567ac6162b0bfc26cf21172354e165b81c8d315ee4a9f703a7a042df`       |
| Container runner script      | SHA-256 `eb25653efa0006437fb3c8d84b8b5043ba11582ea2f4a75681b2317e853e0199`       |
| Host wrapper script          | SHA-256 `b4c320af7cbd1ff03fdf46978f8c4e187d8ead6dab3031ccad3b218d31ca23a2`       |
| ShellCheck                   | `shellcheck-0.10.0-r2`, run in the same exact base image                         |
| ext4/xfs evidence disk sizes | 192 MiB / 512 MiB raw                                                            |

The exact base image is never pulled implicitly. Alpine package names and versions are exact, and
installed-file manifests are fixed in the output, but their APK archives remain repository/cache
inputs rather than checked-in immutable project artifacts. This does not close
`G-SUPPLY-CHAIN`.

## 3. Replay command and result

The matrix was rerun after commit and push with local `HEAD`, tracking ref and remote branch fixed
to `3d21e908e0410d23ba44ee5499e615f6f837c4c7`:

```sh
CLOUD_AGENTS_EVIDENCEFS_HARNESS_IMAGE='alpine@sha256:14358309a308569c32bdc37e2e0e9694be33a9d99e68afb0f5ff33cc1f695dce' \
  ./services/control-plane/scripts/test-evidencefs-linux-powerloss-matrix.sh
```

Result:

```text
EVIDENCEFS_QEMU_ENV ... package_manifest_sha256=d4db731e... guest_package_manifest_sha256=1a2aefae...
EVIDENCEFS_QEMU_POWER_LOSS filesystem=ext4 mode=create-object ... result=KILLED
EVIDENCEFS_QEMU_RESTART filesystem=ext4 mode=verify-object ... result=PASS
EVIDENCEFS_QEMU_POWER_LOSS filesystem=ext4 mode=create-generation ... result=KILLED
EVIDENCEFS_QEMU_RESTART filesystem=ext4 mode=verify-all ... result=PASS
EVIDENCEFS_QEMU_MATRIX filesystem=ext4 result=PASS
EVIDENCEFS_QEMU_POWER_LOSS filesystem=xfs mode=create-object ... result=KILLED
EVIDENCEFS_QEMU_RESTART filesystem=xfs mode=verify-object ... result=PASS
EVIDENCEFS_QEMU_POWER_LOSS filesystem=xfs mode=create-generation ... result=KILLED
EVIDENCEFS_QEMU_RESTART filesystem=xfs mode=verify-all ... result=PASS
EVIDENCEFS_QEMU_MATRIX filesystem=xfs result=PASS
Evidencefs Linux ext4/xfs isolated QEMU power-loss matrix: PASS
```

The exact candidate also passed:

```sh
sh -n services/control-plane/scripts/evidencefs-powerloss-guest-init.sh
sh -n services/control-plane/scripts/evidencefs-powerloss-container.sh
bash -n services/control-plane/scripts/test-evidencefs-linux-powerloss-matrix.sh
git diff --check
```

All three scripts passed ShellCheck at style severity. The wrapper snapshots the host-visible loop
device set before and after the run and requires byte-equal output. It also requires no container
with the unique run ownership label after completion. The exact run left neither an owned
container nor a new loop attachment.

## 4. Destructive-action boundary

The only forced termination target is the child `qemu-system-aarch64` process created inside the
task-owned one-shot container. The harness never invokes host reboot, SysRq, physical power
control, block-device reset, host filesystem unmount or a command against an existing VM. Guest
root and evidence images live under an exact `mktemp -d` scope and are deleted by the host wrapper.

The wrapper refuses to remove a container unless its unique ownership label matches the current
run. Root-image loop setup is cleaned in the inner runner, and the host wrapper separately proves
the pre/post loop set is unchanged. The guest root disk is read-only while QEMU runs; all evidence
mutations are confined to the dedicated raw data image.

## 5. Authority boundary

This harness reuses the package-private `_test.go` authority guarded by
`linux && evidencefsintegration`. It adds no Go production file or constructor. Production
`evidencefs.Open` is invoked on the final recovered mount and must still return
`ErrTrustedMountAuthority` before probe or mutation.

No migration DTO, verifier receipt, runner, database, signature, public `EvidenceSink`, cloud
controller or deployment path is used. The fixed index/journal values remain opaque evidencefs
test bytes.

## 6. Explicitly open

This record does **not** prove or authorize:

- a non-forgeable external trusted-mount provisioner or positive production constructor;
- an actual storage-controller reset, physical host power removal, volatile hardware write-cache
  behavior, bare-metal kernel/storage stack or cloud block device;
- power loss before/during each individual create/write/fdatasync/fsync barrier, short writes,
  response-lost classification, torn writes or the complete ADR-0010 barrier matrix;
- x86_64 runtime behavior, a second QEMU/kernel version or another filesystem;
- a production migration/runner/DB path, typed C3 frames, receipts, `EvidenceSink` or `Connect`;
- filesystem-slice Done, independent reviewer closure, any P1 Gate, Platform RC, deployment,
  Beta, GA or release.

This record closes only the isolated guest-host power-cycle-after-durable-return evidence item.
Production `Open` remains fail closed, and P1-A2.1b remains out of scope until the trusted
provisioner and full per-barrier matrix are separately fixed, implemented and reviewed.
