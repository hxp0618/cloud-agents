# P1 evidencefs QEMU generation-activation barrier power-loss matrix — 2026-08-16

- Status：**IMPLEMENTATION EVIDENCE — PASS；Gate OPEN**
- Fixed source：`be7cae84e3d25cb9dc07ca7cf45a118b10307954`
- Source tree：`ea396e6bc8af79e9302b60b10a15199a9fc89b9f`
- Branch：`codex/cloud-agents-platform-p1`
- Evidence snapshot：`2026-08-16T04:23:11Z`
- Executor：Codex P1 implementation executor
- Independent Gate reviewer：**not assigned**

## 1. Scope

This record extends the isolated QEMU harness fixed by
[`evidencefs-qemu-generation-rotation-barrier-matrix-20260816.md`](evidencefs-qemu-generation-rotation-barrier-matrix-20260816.md)
with the target-index activation append. Earlier records remain historical and unchanged.

For every barrier and each of ext4 and xfs, the harness creates a fresh raw evidence disk and starts
a new guest. Using the real Linux backend, that guest durably creates the closed root grammar,
registers one target lineage and creates its segment-0 generation header. It deliberately stops with
the exact 47-byte lineage-index header and exact 57-byte segment-0 header: no activation index frame
has been appended. Only then does the tagged wrapper arm, so target registration and generation
creation cannot consume or mislabel an activation boundary.

The candidate transition calls the sealed mutation token's `AppendTargetIndex` with a fixed 60-byte
activation frame. The host kills the entire QEMU process at one of five exact boundaries:

1. `before-activation-write`;
2. `after-short-activation-write`;
3. `after-activation-write`;
4. `before-activation-fdatasync`; and
5. `after-activation-fdatasync`.

A second new QEMU process mounts the same disk and classifies the recovered index and segment through
sealed `AdmissionInventory` views. No in-memory transition result, token or reconciliation capability
survives the crash; this is filesystem implementation evidence, not production recovery authority.

## 2. Real syscall and ordering boundary

The test backend remains dormant through target and generation-header durability. Once armed, it
captures the exact `index.caj` descriptor returned by `openFileAtReadWrite` and delegates every
candidate `pwrite` and `fdatasync` to `linuxBackend`. A second index open is rejected, and cumulative
write accounting prevents a natural partial-write retry from masquerading as a later boundary.

The `before-activation-write` marker is emitted immediately before the real first `pwrite`.
`after-short-activation-write` is emitted only after a real half-payload write returns the expected
30 bytes. `after-activation-write` requires cumulative completion of all 60 bytes.
`before-activation-fdatasync` precedes the real data sync, and `after-activation-fdatasync` is emitted
only after that syscall returns nil.

The fixed harness also closes a guest-root construction ambiguity discovered during exact replay. It
now cleanly unmounts the task-owned temporary root image, flushes and detaches its loop descriptor,
runs `e2fsck -pf` against the detached image, preserves diagnostic output on failure and syncs the
checked image before QEMU starts. This check never targets an evidence disk or host filesystem.

## 3. Closed recovery contract

Every fresh classifier first proves production `Open` still rejects with
`ErrTrustedMountAuthority`. It then uses package-private test mount authority to require:

- the exact target and exactly one canonical lineage;
- the exact 47-byte index baseline plus only an absent, strict-prefix or complete activation suffix;
- exactly one journal with the expected digest;
- exactly one ordinal-0 segment containing the exact 57-byte generation header; and
- a successful final `AdmissionInventory.Revalidate`.

The candidate index is classified through the production suffix reconciler into exactly three closed
states:

- `unchanged`;
- `activation_torn`; or
- `activation_complete`.

The barrier-specific allowed sets are:

| Barrier                        | Allowed fresh-mount state                         |
| ------------------------------ | ------------------------------------------------- |
| `before-activation-write`      | unchanged                                         |
| `after-short-activation-write` | unchanged or activation torn                      |
| `after-activation-write`       | unchanged, activation torn or activation complete |
| `before-activation-fdatasync`  | unchanged, activation torn or activation complete |
| `after-activation-fdatasync`   | activation complete                               |

Wrong target/journal identity, additional lineages or segments, a modified segment, non-prefix
activation bytes, extra index bytes, inventory drift or any read failure rejects the guest.

## 4. Fixed environment and artifacts

| Input                          | Exact value                                                                           |
| ------------------------------ | ------------------------------------------------------------------------------------- |
| Integration test source        | SHA-256 `df6eea47815fa7a750b75d840e5de6e617fbb47745f96acd33355ad2019f4a80`            |
| Guest init script              | SHA-256 `12022455c1f3054663df0ea28769acce12fa8a68a300b7fead4ecc96530e934b`            |
| Container runner script        | SHA-256 `8c7382d0723bfd17c2015707c3014a56f7703579ec00f374e4f1a6726bfc3244`            |
| Host wrapper script            | SHA-256 `b4c320af7cbd1ff03fdf46978f8c4e187d8ead6dab3031ccad3b218d31ca23a2`            |
| Linux/arm64 integration binary | SHA-256 `d4f9db6cc341872e4dee3670be3934e92ba4e334b6d7d231629f5f3719d4a202`            |
| Linux/amd64 compile artifact   | SHA-256 `cb10615111a2612476f7ed09affe990151b83078599d037f4d0c86767b6206a0`            |
| Exact replay log               | 230 lines; SHA-256 `2e89fffc2d546a64cff2b32162e3efe82ed2df1fc480d705271c9dae66bd799b` |
| Base image ref                 | `alpine@sha256:14358309a308569c32bdc37e2e0e9694be33a9d99e68afb0f5ff33cc1f695dce`      |
| Local image ID                 | `sha256:2c15e55df5d63efb31b629a557df305130612a16feb029c93447e54dda2c4189`             |
| QEMU                           | `qemu-system-aarch64-10.0.0-r1`; TCG `virt`, Cortex-A72, 2 vCPU, 768 MiB              |
| Guest kernel                   | `linux-virt-6.12.103-r0` / `6.12.103-0-virt`                                          |
| ext4 / xfs tooling             | `e2fsprogs-1.47.2-r2` / `xfsprogs-6.14.0-r0`                                          |
| Container package manifest     | SHA-256 `d4db731ed3d00840ece5c114808335af1ac1be3bf6be74f4dd5df590f76e54f0`            |
| Guest package manifest         | SHA-256 `1a2aefaecd9def95205dd19290faa6b0b2a89a1c394fb73aa05820f97f643b38`            |
| QEMU data-disk cache mode      | `cache=none,aio=threads`                                                              |
| ext4 / xfs evidence disk       | fresh 192 MiB / 512 MiB raw image per barrier                                         |
| ShellCheck                     | `shellcheck-0.10.0-r2`, run in the exact base image                                   |

The exact base image is never pulled implicitly. Package archive/cache availability remains an
external input; this record does not close `G-SUPPLY-CHAIN`. Guest package installation emitted two
repository-cache fallback warnings on stderr, but the fixed package manifests above were recomputed
inside the successful run and remained exact. The hashed 230-line replay log is the stdout evidence
stream.

## 5. Exact-commit result

After both implementation commits and push, local `HEAD`, tracking ref and remote feature branch all
resolved to `be7cae84e3d25cb9dc07ca7cf45a118b10307954`. The exact replay command was:

```sh
CLOUD_AGENTS_EVIDENCEFS_HARNESS_IMAGE='alpine@sha256:14358309a308569c32bdc37e2e0e9694be33a9d99e68afb0f5ff33cc1f695dce' \
  ./services/control-plane/scripts/test-evidencefs-linux-powerloss-matrix.sh
```

Both filesystems produced the same activation sequence:

| Barrier                        | ext4                | xfs                 | Index / segment 0 |
| ------------------------------ | ------------------- | ------------------- | ----------------- |
| `before-activation-write`      | unchanged           | unchanged           | 47 / 57 B         |
| `after-short-activation-write` | unchanged           | unchanged           | 47 / 57 B         |
| `after-activation-write`       | unchanged           | unchanged           | 47 / 57 B         |
| `before-activation-fdatasync`  | unchanged           | unchanged           | 47 / 57 B         |
| `after-activation-fdatasync`   | activation complete | activation complete | 107 / 57 B        |

The short-write marker proves a successful real half-payload syscall. Those bytes did not survive
either fresh mount in this run, so `activation_torn` was accepted by the classifier but not observed.
This record reports the observed states separately from the larger allowed contract.

Mechanical recounting of the final log produced, per filesystem:

- object publish: 11 crash and 11 reopen lines;
- existing-segment append: 10 crash and 10 reopen lines;
- retained rotation: 26 crash and 26 reopen lines; and
- generation activation: 5 crash and 5 reopen lines.

Each family also emitted exactly one matching `..._BARRIER_MATRIX ... result=PASS` line per
filesystem. The host wrapper proved the loop-device set was byte-equal before and after the run, and
no task-owned container remained.

An earlier replay fixed to `1ba208b622baff067f88c0e1d55866772caa0e01` aborted before
`EVIDENCEFS_QEMU_ENV`: the temporary guest-root loop-device fsck returned operational status 8. It
ran no QEMU barrier assertion, produced an empty stdout log and is not used as passing evidence.
`be7cae8` fixed only that task-owned root-image validation order before the successful exact replay.

## 6. Other gates

The final fixed implementation passed:

```sh
sh -n services/control-plane/scripts/evidencefs-powerloss-guest-init.sh
sh -n services/control-plane/scripts/evidencefs-powerloss-container.sh
bash -n services/control-plane/scripts/test-evidencefs-linux-powerloss-matrix.sh
shellcheck services/control-plane/scripts/evidencefs-powerloss-guest-init.sh \
  services/control-plane/scripts/evidencefs-powerloss-container.sh \
  services/control-plane/scripts/test-evidencefs-linux-powerloss-matrix.sh
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go test -c -tags=evidencefsintegration ./internal/evidencefs
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go test -c -tags=evidencefsintegration ./internal/evidencefs
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go vet -tags=evidencefsintegration ./internal/evidencefs
go test -count=1 ./internal/evidencefs
go test -race -count=1 ./internal/evidencefs
go test -run '^$' ./...
go vet ./...
go build ./...
git diff --check
```

The exact parent implementation had already passed `go test -count=1 -timeout=25m ./...`; the
unchanged migration package required `965.821s`. This implementation changes only Linux-tagged test
code and shell harnesses, so that 16-minute default-tag execution suite was not repeated. Every
default package was compiled with `-run '^$'`, and the affected evidencefs normal/race suites ran on
the final fixed commit.

## 7. Authority and destructive-action boundary

All new Go behavior lives in a `_test.go` file guarded by `linux && evidencefsintegration` and adds
no production constructor, backend injection or exported authority. Production `Open` is invoked
after every crash and must still return `ErrTrustedMountAuthority` before package-private test
authority can inspect the mount.

The only forced termination target is the child `qemu-system-aarch64` process inside the task-owned
one-shot container. The harness does not reboot the host, invoke SysRq, reset a host block device,
unmount a host filesystem or target an existing VM.

No migration DTO, C3 decoder, receipt, verifier, runner, database, `EvidenceSink`, controller or
deployment path participates. Index and journal payloads remain opaque evidencefs test bytes.

## 8. Explicitly open

This record does **not** prove or authorize:

- target registration or generation-directory/header creation at every syscall barrier; the baseline
  only requires their durable success before arming activation;
- standalone checkpoint heal, resync, truncate or discard at every syscall barrier;
- a non-forgeable trusted-mount provisioner, positive production constructor or required-syscall
  probe;
- a physical storage-controller reset, volatile hardware write-cache behavior, physical host power
  removal, cloud block device or bare-metal kernel/storage stack;
- x86_64 runtime execution, another QEMU/kernel version or another filesystem;
- a production migration/runner/DB path, typed C3 frames, receipts, public `EvidenceSink` or
  `Connect`;
- filesystem-slice Done, independent reviewer closure, a P1 Gate, Platform RC, deployment, Beta,
  GA or release.

Current production `AcquireAdmission` intentionally rejects partial target-registration grammar, and
this test-only activation slice adds no recovery constructor or `TargetRegistrationState` authority.
Registration/header barrier recovery must be designed and reviewed separately before that matrix can
be claimed.

This record closes only the package-private isolated-QEMU generation-activation index-append barrier
evidence item. Production `Open` remains fail closed, and P1-A2.1b remains out of scope until the
remaining filesystem authority and durability work is separately fixed, implemented and reviewed.
