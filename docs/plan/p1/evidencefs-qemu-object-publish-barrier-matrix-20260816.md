# P1 evidencefs QEMU object-publish barrier power-loss matrix — 2026-08-16

- Status：**IMPLEMENTATION EVIDENCE — PASS；Gate OPEN**
- Fixed source：`b6cfa888c983e8d3b9ab1a744f8716bd75ce1457`
- Source tree：`5eaac44df8f13be40935332e460b6d14ff310412`
- Branch：`codex/cloud-agents-platform-p1`
- Evidence snapshot：`2026-08-15T21:17:26Z`
- Executor：Codex P1 implementation executor
- Independent Gate reviewer：**not assigned**

## 1. Scope

This record adds object-publication barrier coverage to the isolated QEMU harness fixed by
[`evidencefs-qemu-powerloss-matrix-20260816.md`](evidencefs-qemu-powerloss-matrix-20260816.md).
The earlier durable-return record remains historical and unchanged.

For each barrier and each of ext4 and xfs, the harness creates a fresh raw evidence disk, builds the
closed root grammar, syncs that baseline, starts a new guest, enters the real evidencefs
`Lease.Publish` path and pauses immediately before or after one selected Linux syscall boundary.
The host then sends `SIGKILL` to the entire QEMU process. A second new QEMU process mounts the same
raw disk and classifies the recovered object store through a sealed `Scan`.

The matrix covers these eleven object-publication boundaries:

1. `before-temp-write`;
2. `after-short-temp-write`, after a real successful 22-byte write from the fixed 44-byte payload;
3. `after-temp-write`, only after the real syscall reports all 44 bytes written;
4. `before-temp-fdatasync`;
5. `after-temp-fdatasync`;
6. `before-rename`;
7. `after-rename`;
8. `before-final-fdatasync`;
9. `after-final-fdatasync`;
10. `before-directory-fsync`; and
11. `after-directory-fsync`.

The tagged test backend embeds and delegates to the production `linuxBackend`; it does not replace
Linux write, sync, rename, open, stat, hash or directory operations with an in-memory fake. It only
pauses at the listed boundaries. Each `before-*` marker is emitted immediately before the real
call; each `after-*` marker is emitted only after the syscall returns the expected successful byte
count or nil error.

## 2. Recovery contract

The fresh guest requires production `Open` to continue returning `ErrTrustedMountAuthority`, then
uses the package-private tagged test authority to acquire the root lease and perform a sealed scan.
It accepts only the following closed recovery states:

| Barrier group                                          | Allowed state after fresh mount                    |
| ------------------------------------------------------ | -------------------------------------------------- |
| before/short/after temp write; before temp `fdatasync` | no final; temp absent or at most 44 bytes          |
| after temp `fdatasync`; before rename                  | no final; temp absent or exactly 44 bytes          |
| after rename through before directory `fsync`          | absent, exact 44-byte temp, or exact 44-byte final |
| after directory `fsync`                                | exactly one 44-byte final; no temp                 |

Any extra final, extra temp, wrong final size/content, overlong temp, final+temp coexistence, root
grammar contradiction or scan failure rejects the guest. Temporary files never count as final
content authority.

## 3. Fixed environment and artifacts

| Input                          | Exact value                                                                      |
| ------------------------------ | -------------------------------------------------------------------------------- |
| Integration test source        | SHA-256 `6107a7330712e83148f6f645b9357bbd7c9331ad1d6ab3a9bc5c129e16a73d17`       |
| Guest init script              | SHA-256 `796f54af5e1cafc79b1762ef83f2ccbced06d99c578a751b73c7032b72fb8c09`       |
| Container runner script        | SHA-256 `4d89baf8a4853f514d5ef2ef8962a57385521373851a414c08e97ae074e52cf7`       |
| Host wrapper script            | SHA-256 `b4c320af7cbd1ff03fdf46978f8c4e187d8ead6dab3031ccad3b218d31ca23a2`       |
| Linux/arm64 integration binary | SHA-256 `bddca4c223427be70d56a84f348bde205279756a610d0773bccc6b28b558da2b`       |
| Linux/amd64 compile artifact   | SHA-256 `c5313dcda6beaa198660c66638ceafdfe255702fca07ada13e9136093cbaf0b3`       |
| Base image ref                 | `alpine@sha256:14358309a308569c32bdc37e2e0e9694be33a9d99e68afb0f5ff33cc1f695dce` |
| Local image ID                 | `sha256:2c15e55df5d63efb31b629a557df305130612a16feb029c93447e54dda2c4189`        |
| QEMU                           | `qemu-system-aarch64-10.0.0-r1`; TCG `virt`, Cortex-A72, 2 vCPU, 768 MiB         |
| Guest kernel                   | `linux-virt-6.12.103-r0` / `6.12.103-0-virt`                                     |
| ext4 / xfs tooling             | `e2fsprogs-1.47.2-r2` / `xfsprogs-6.14.0-r0`                                     |
| Container package manifest     | SHA-256 `d4db731ed3d00840ece5c114808335af1ac1be3bf6be74f4dd5df590f76e54f0`       |
| Guest package manifest         | SHA-256 `1a2aefaecd9def95205dd19290faa6b0b2a89a1c394fb73aa05820f97f643b38`       |
| ShellCheck                     | `shellcheck-0.10.0-r2`, run in the same exact base image                         |
| ext4 / xfs evidence disk       | fresh 192 MiB / 512 MiB raw image per barrier                                    |
| QEMU data-disk cache mode      | `cache=none,aio=threads`                                                         |

The exact base image is never pulled implicitly. Alpine package versions and installed-file
manifests are fixed, but repository/cache availability remains an external input and this evidence
does not close `G-SUPPLY-CHAIN`.

## 4. Exact-commit result

After commit and push, local `HEAD`, tracking ref and the remote feature branch all resolved to
`b6cfa888c983e8d3b9ab1a744f8716bd75ce1457`. The exact replay command was:

```sh
CLOUD_AGENTS_EVIDENCEFS_HARNESS_IMAGE='alpine@sha256:14358309a308569c32bdc37e2e0e9694be33a9d99e68afb0f5ff33cc1f695dce' \
  ./services/control-plane/scripts/test-evidencefs-linux-powerloss-matrix.sh
```

Observed fresh-mount states were:

| Barrier                  | ext4 recovery | xfs recovery |
| ------------------------ | ------------- | ------------ |
| `before-temp-write`      | absent        | absent       |
| `after-short-temp-write` | absent        | absent       |
| `after-temp-write`       | absent        | absent       |
| `before-temp-fdatasync`  | absent        | absent       |
| `after-temp-fdatasync`   | temp, 44 B    | temp, 44 B   |
| `before-rename`          | temp, 44 B    | temp, 44 B   |
| `after-rename`           | temp, 44 B    | temp, 44 B   |
| `before-final-fdatasync` | temp, 44 B    | temp, 44 B   |
| `after-final-fdatasync`  | temp, 44 B    | final, 44 B  |
| `before-directory-fsync` | temp, 44 B    | final, 44 B  |
| `after-directory-fsync`  | final, 44 B   | final, 44 B  |

The filesystem-specific pre-directory-sync results differ but remain inside the fixed allowed
set. Both filesystems require and produced the unique exact final after directory `fsync` returned.
The runner reported `barriers=11 result=PASS` independently for ext4 and xfs, then reported the
host-level isolated matrix and exact binary digest as `PASS`.

The wrapper also proved the host-visible loop-device set was byte-equal before and after the run
and that no container carrying the unique run ownership label remained.

## 5. Other gates

The fixed candidate passed:

```sh
sh -n services/control-plane/scripts/evidencefs-powerloss-guest-init.sh
sh -n services/control-plane/scripts/evidencefs-powerloss-container.sh
bash -n services/control-plane/scripts/test-evidencefs-linux-powerloss-matrix.sh
shellcheck services/control-plane/scripts/evidencefs-powerloss-guest-init.sh \
  services/control-plane/scripts/evidencefs-powerloss-container.sh \
  services/control-plane/scripts/test-evidencefs-linux-powerloss-matrix.sh
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go test -c -tags=evidencefsintegration ./internal/evidencefs
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go test -c -tags=evidencefsintegration ./internal/evidencefs
go test -race ./internal/evidencefs
go vet ./...
go build ./...
go test -count=1 -timeout=25m ./...
git diff --check
```

The final full-module command passed; `internal/migration` required `965.821s`. A preceding plain
`go test ./...` run reached Go's default `10m0s` timeout while executing unchanged migration tests,
with no assertion failure. That default-timeout result is retained as a real gate observation and
is not represented as a pass. Raising or optimizing the repository-wide migration test schedule is
outside this object-barrier slice.

## 6. Destructive-action boundary

The only forced termination target is the child `qemu-system-aarch64` process created inside the
task-owned one-shot container. The harness never reboots the host, invokes SysRq, resets a host
block device, unmounts a host filesystem or targets an existing VM. Each guest root/evidence image
lives under the exact wrapper-owned temporary directory and is removed by its checked cleanup.

## 7. Authority boundary

All new Go code is in a `_test.go` file guarded by `linux && evidencefsintegration`. It adds no
production constructor, backend injection, exported authority or migration dependency. Production
`evidencefs.Open` is called after every crash and must reject before the package-private test
authority can scan the recovered root.

No migration DTO, C3 frame, receipt, verifier, runner, database, `EvidenceSink`, controller or
deployment path participates. The fixed payload is opaque evidencefs test content.

## 8. Explicitly open

This record does **not** prove or authorize:

- a non-forgeable trusted-mount provisioner, positive production constructor or required-syscall
  probe;
- generation registration/index/journal create, append, rotation, checkpoint, repair or handoff at
  every individual syscall barrier;
- a physical storage-controller reset, volatile hardware write-cache behavior, physical host power
  removal, cloud block device or bare-metal kernel/storage stack;
- x86_64 runtime execution, another QEMU/kernel version or another filesystem;
- a production migration/runner/DB path, typed C3 frames, receipts, public `EvidenceSink` or
  `Connect`;
- filesystem-slice Done, independent reviewer closure, a P1 Gate, Platform RC, deployment, Beta,
  GA or release.

This record closes only the package-private isolated-QEMU object-publish barrier evidence item.
Production `Open` remains fail closed, and P1-A2.1b remains out of scope until the remaining
filesystem authority and durability work is separately fixed, implemented and reviewed.
