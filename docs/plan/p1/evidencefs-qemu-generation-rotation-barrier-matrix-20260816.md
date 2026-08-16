# P1 evidencefs QEMU generation-rotation barrier power-loss matrix — 2026-08-16

- Status：**IMPLEMENTATION EVIDENCE — PASS；Gate OPEN**
- Fixed source：`0e242ee346a0f37d3f75a4ed66407d317d2d0b66`
- Source tree：`04981acf154c0983069783ab029fd2d5c9bfe559`
- Branch：`codex/cloud-agents-platform-p1`
- Evidence snapshot：`2026-08-16T00:45:34Z`
- Executor：Codex P1 implementation executor
- Independent Gate reviewer：**not assigned**

## 1. Scope

This record extends the isolated QEMU harness fixed by
[`evidencefs-qemu-generation-append-barrier-matrix-20260816.md`](evidencefs-qemu-generation-append-barrier-matrix-20260816.md)
with the retained `GenerationLease.AppendRotatedSegmentComposite` durability order. Earlier records
remain historical and unchanged.

For every barrier and each of ext4 and xfs, the harness creates a fresh raw evidence disk and starts
a new guest. Using the real Linux backend, that guest durably creates the closed root grammar,
registers the target lineage, creates and activates segment 0, hands off to a retained generation
lease and completes one existing-segment journal/checkpoint composite. Only then does the tagged
wrapper arm, so baseline construction cannot consume or mislabel a candidate boundary.

The candidate transition creates segment 1, makes its empty inode and directory entry durable,
appends a fixed 48-byte rotation header, appends a fixed 52-byte header checkpoint to the existing
167-byte index, appends a fixed 48-byte caller record and finally appends a fixed 59-byte caller
checkpoint. The host kills the entire QEMU process at one of these 26 boundaries:

1. `before-segment-create`;
2. `after-segment-create`;
3. `before-empty-fdatasync`;
4. `after-empty-fdatasync`;
5. `before-segment-directory-fsync`;
6. `after-segment-directory-fsync`;
7. `before-header-write`;
8. `after-short-header-write`;
9. `after-header-write`;
10. `before-header-fdatasync`;
11. `after-header-fdatasync`;
12. `before-header-checkpoint-write`;
13. `after-short-header-checkpoint-write`;
14. `after-header-checkpoint-write`;
15. `before-header-checkpoint-fdatasync`;
16. `after-header-checkpoint-fdatasync`;
17. `before-caller-write`;
18. `after-short-caller-write`;
19. `after-caller-write`;
20. `before-caller-fdatasync`;
21. `after-caller-fdatasync`;
22. `before-caller-checkpoint-write`;
23. `after-short-caller-checkpoint-write`;
24. `after-caller-checkpoint-write`;
25. `before-caller-checkpoint-fdatasync`; and
26. `after-caller-checkpoint-fdatasync`.

A second new QEMU process mounts the same disk and classifies the recovered index and segment set
through sealed `AdmissionInventory` views. No in-memory unknown result or reconciliation capability
survives the crash; this classifier is filesystem implementation evidence, not recovery authority.

## 2. Real syscall and ordering boundary

The test backend binds `index.caj`, new segment 1 and its journal directory to the exact file
descriptors returned after arming. It delegates create, every `pwrite`, `fdatasync` and directory
`fsync` to `linuxBackend`. Segment and index writes each maintain independent cumulative byte counts
and completed-operation ordinals, so a natural partial write cannot make a retry or later caller
operation masquerade as another boundary.

Each `before-*` marker is emitted immediately before the real syscall. Each short marker is emitted
only after a successful half-payload `pwrite`; each full-write marker requires cumulative completion;
and each `after-*-fdatasync` or `after-*-fsync` marker is emitted only after the syscall returns nil.

## 3. Closed recovery contract

The fresh classifier requires the exact 167-byte post-existing-append index, the exact 114-byte
segment-0 baseline, exactly one journal and either one or two canonical segment ordinals. An optional
segment 1 and both index suffixes must be absent, exact strict prefixes or exact complete bytes. It
uses the same ten closed states as `GenerationRotationResult.Reconcile`:

- `segment_absent`;
- `segment_empty`;
- `header_torn`;
- `header_complete`;
- `header_checkpoint_torn`;
- `header_composite_complete`;
- `caller_torn`;
- `caller_complete`;
- `caller_checkpoint_torn`; and
- `composite_complete`.

The barrier-specific allowed sets are:

| Barrier group                                            | Allowed fresh-mount state                                            |
| -------------------------------------------------------- | -------------------------------------------------------------------- |
| before create                                            | segment absent                                                       |
| after create through before directory `fsync`            | segment absent or empty                                              |
| after directory `fsync`; before header write             | segment empty                                                        |
| after short header write                                 | segment empty or header torn                                         |
| after header write; before header `fdatasync`            | segment empty, header torn or header complete                        |
| after header `fdatasync`; before header-checkpoint write | header complete                                                      |
| after short header-checkpoint write                      | header complete or header-checkpoint torn                            |
| after header-checkpoint write; before its `fdatasync`    | header complete, header-checkpoint torn or header composite complete |
| after header-checkpoint `fdatasync`; before caller write | header composite complete                                            |
| after short caller write                                 | header composite complete or caller torn                             |
| after caller write; before caller `fdatasync`            | header composite complete, caller torn or caller complete            |
| after caller `fdatasync`; before caller-checkpoint write | caller complete                                                      |
| after short caller-checkpoint write                      | caller complete or caller-checkpoint torn                            |
| after caller-checkpoint write; before its `fdatasync`    | caller complete, caller-checkpoint torn or composite complete        |
| after caller-checkpoint `fdatasync`                      | composite complete                                                   |

Any candidate index ahead of its segment, wrong baseline, non-prefix bytes, extra bytes, unexpected
segment ordinal, wrong lineage/journal identity, inventory drift or read failure rejects the guest.

## 4. Fixed environment and artifacts

| Input                          | Exact value                                                                           |
| ------------------------------ | ------------------------------------------------------------------------------------- |
| Integration test source        | SHA-256 `cda5021c1a287a07cca6a6eb93343c0fb1cc5ccaf93069954d82ddca609e3c1d`            |
| Guest init script              | SHA-256 `fb42227aa63cfe527930a46a24a0a0ddd5b5c67bdab47f2c52d9ac2ec749e81d`            |
| Container runner script        | SHA-256 `14f605302846a4f419a47f2b45a5d6c1198ae0deb0452d684427a00dcc62e510`            |
| Host wrapper script            | SHA-256 `b4c320af7cbd1ff03fdf46978f8c4e187d8ead6dab3031ccad3b218d31ca23a2`            |
| Linux/arm64 integration binary | SHA-256 `1135628e8fa538ec7670ae4d70ab21d4a78a0ab51a9d2f439f161badbadbec36`            |
| Linux/amd64 compile artifact   | SHA-256 `e92cbc47accf6ef9437ab69442edc7e5f5e2c999464660ba64e6d518b642bd57`            |
| Exact replay log               | 208 lines; SHA-256 `94c0e37ce408d2f108baecffbf60e283457f79f919d1629e7d5f711ba23f5ee6` |
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
external input; this record does not close `G-SUPPLY-CHAIN`.

## 5. Exact-commit result

After implementation commit and push, local `HEAD`, tracking ref and remote feature branch all
resolved to `0e242ee346a0f37d3f75a4ed66407d317d2d0b66`. The exact replay command was:

```sh
CLOUD_AGENTS_EVIDENCEFS_HARNESS_IMAGE='alpine@sha256:14358309a308569c32bdc37e2e0e9694be33a9d99e68afb0f5ff33cc1f695dce' \
  ./services/control-plane/scripts/test-evidencefs-linux-powerloss-matrix.sh
```

Both filesystems produced the same observed sequence:

| Barrier                               | ext4                      | xfs                       | Index / segment 0 / segment 1 |
| ------------------------------------- | ------------------------- | ------------------------- | ----------------------------- |
| `before-segment-create`               | segment absent            | segment absent            | 167 / 114 / absent            |
| `after-segment-create`                | segment absent            | segment absent            | 167 / 114 / absent            |
| `before-empty-fdatasync`              | segment absent            | segment absent            | 167 / 114 / absent            |
| `after-empty-fdatasync`               | segment empty             | segment empty             | 167 / 114 / 0 B               |
| `before-segment-directory-fsync`      | segment empty             | segment empty             | 167 / 114 / 0 B               |
| `after-segment-directory-fsync`       | segment empty             | segment empty             | 167 / 114 / 0 B               |
| `before-header-write`                 | segment empty             | segment empty             | 167 / 114 / 0 B               |
| `after-short-header-write`            | segment empty             | segment empty             | 167 / 114 / 0 B               |
| `after-header-write`                  | segment empty             | segment empty             | 167 / 114 / 0 B               |
| `before-header-fdatasync`             | segment empty             | segment empty             | 167 / 114 / 0 B               |
| `after-header-fdatasync`              | header complete           | header complete           | 167 / 114 / 48 B              |
| `before-header-checkpoint-write`      | header complete           | header complete           | 167 / 114 / 48 B              |
| `after-short-header-checkpoint-write` | header complete           | header complete           | 167 / 114 / 48 B              |
| `after-header-checkpoint-write`       | header complete           | header complete           | 167 / 114 / 48 B              |
| `before-header-checkpoint-fdatasync`  | header complete           | header complete           | 167 / 114 / 48 B              |
| `after-header-checkpoint-fdatasync`   | header composite complete | header composite complete | 219 / 114 / 48 B              |
| `before-caller-write`                 | header composite complete | header composite complete | 219 / 114 / 48 B              |
| `after-short-caller-write`            | header composite complete | header composite complete | 219 / 114 / 48 B              |
| `after-caller-write`                  | header composite complete | header composite complete | 219 / 114 / 48 B              |
| `before-caller-fdatasync`             | header composite complete | header composite complete | 219 / 114 / 48 B              |
| `after-caller-fdatasync`              | caller complete           | caller complete           | 219 / 114 / 96 B              |
| `before-caller-checkpoint-write`      | caller complete           | caller complete           | 219 / 114 / 96 B              |
| `after-short-caller-checkpoint-write` | caller complete           | caller complete           | 219 / 114 / 96 B              |
| `after-caller-checkpoint-write`       | caller complete           | caller complete           | 219 / 114 / 96 B              |
| `before-caller-checkpoint-fdatasync`  | caller complete           | caller complete           | 219 / 114 / 96 B              |
| `after-caller-checkpoint-fdatasync`   | composite complete        | composite complete        | 278 / 114 / 96 B              |

The short-write markers prove that the half-payload syscalls returned successfully. Their bytes did
not survive either fresh mount in this run, so the four torn states were accepted by the classifier
but not observed. This record reports observed states separately from the larger allowed contract.

The exact log contains 26 crash and 26 reopen lines per filesystem, plus exactly one
`EVIDENCEFS_QEMU_GENERATION_ROTATION_BARRIER_MATRIX ... barriers=26 result=PASS` line per filesystem.
The host-visible loop-device set was byte-equal before and after the run, and no task-owned container
remained.

## 6. Other gates

The fixed implementation passed:

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
default package was compiled with `-run '^$'`, and the affected evidencefs normal/race suites ran.

## 7. Authority and destructive-action boundary

All new Go behavior lives in a `_test.go` file guarded by `linux && evidencefsintegration` and adds
no production constructor, backend injection or exported authority. Production `Open` is invoked
after every crash and must still return `ErrTrustedMountAuthority` before the package-private test
authority can inspect the mount.

The only forced termination target is the child `qemu-system-aarch64` process inside the task-owned
one-shot container. The harness does not reboot the host, invoke SysRq, reset a host block device,
unmount a host filesystem or target an existing VM.

No migration DTO, C3 decoder, receipt, verifier, runner, database, `EvidenceSink`, controller or
deployment path participates. Index and journal payloads remain opaque evidencefs test bytes.

## 8. Explicitly open

This record does **not** prove or authorize:

- target registration, generation-directory/header creation, activation index append, standalone
  checkpoint heal, resync, truncate or discard at every syscall barrier;
- a non-forgeable trusted-mount provisioner, positive production constructor or required-syscall
  probe;
- a physical storage-controller reset, volatile hardware write-cache behavior, physical host power
  removal, cloud block device or bare-metal kernel/storage stack;
- x86_64 runtime execution, another QEMU/kernel version or another filesystem;
- a production migration/runner/DB path, typed C3 frames, receipts, public `EvidenceSink` or
  `Connect`;
- filesystem-slice Done, independent reviewer closure, a P1 Gate, Platform RC, deployment, Beta,
  GA or release.

This record closes only the package-private isolated-QEMU retained segment-rotation composite
barrier evidence item. Production `Open` remains fail closed, and P1-A2.1b remains out of scope
until the remaining filesystem authority and durability work is separately fixed, implemented and
reviewed.
