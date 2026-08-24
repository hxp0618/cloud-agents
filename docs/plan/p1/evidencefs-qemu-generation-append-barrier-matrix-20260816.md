# P1 evidencefs QEMU generation-append barrier power-loss matrix — 2026-08-16

- Status：**IMPLEMENTATION EVIDENCE — PASS；Gate OPEN**
- Fixed source：`daa6b9ff18b5f625e9b7520503fd16fa6732adb5`
- Source tree：`b87fddbdbb08d8c4e7ab1d229e0c5bcae77130d3`
- Branch：`codex/cloud-agents-platform-p1`
- Evidence snapshot：`2026-08-15T21:52:55Z`
- Executor：Codex P1 implementation executor
- Independent Gate reviewer：**not assigned**

## 1. Scope

This record extends the isolated QEMU harness fixed by
[`evidencefs-qemu-object-publish-barrier-matrix-20260816.md`](evidencefs-qemu-object-publish-barrier-matrix-20260816.md)
with the existing-segment `GenerationLease.AppendExistingSegmentComposite` durability order.
Earlier records remain historical and unchanged.

For every barrier and each of ext4 and xfs, the harness creates a fresh raw evidence disk and starts
a new guest. Using the real Linux backend, that guest durably creates the closed root grammar,
registers the target lineage, creates segment 0, appends the activation index bytes, hands off to a
retained generation lease and seals the baseline snapshot. Only then does the tagged wrapper arm.

The candidate transition appends one fixed 57-byte journal suffix to existing segment 0, calls
`fdatasync` on that segment, appends one fixed 60-byte checkpoint suffix to the existing lineage
index and calls `fdatasync` on the index. The host kills the entire QEMU process at one of these ten
boundaries:

1. `before-journal-write`;
2. `after-short-journal-write`, after a real successful half-payload `pwrite`;
3. `after-journal-write`, only after the complete journal payload is reported written;
4. `before-journal-fdatasync`;
5. `after-journal-fdatasync`;
6. `before-index-write`;
7. `after-short-index-write`, after a real successful half-payload `pwrite`;
8. `after-index-write`, only after the complete checkpoint payload is reported written;
9. `before-index-fdatasync`; and
10. `after-index-fdatasync`.

A second new QEMU process mounts the same disk and classifies the recovered index/segment bytes
through sealed `AdmissionInventory` views. No in-memory unknown result or reconciliation capability
survives the crash; this classifier is filesystem implementation evidence, not recovery authority.

## 2. Real syscall and ordering boundary

The test backend is dormant during baseline construction. Once armed, it records the exact
read-write file descriptors returned for `index.caj` and segment 0, then delegates every `pwrite`
and `fdatasync` to `linuxBackend`. Markers are bound to those descriptors rather than a global call
count, and complete-write markers require cumulative completion. A natural short write therefore
cannot make a later journal retry masquerade as an index write.

Each `before-*` marker is emitted immediately before the real call. Each full/short-write marker is
emitted only after the kernel reports the required byte count, and each `after-*-fdatasync` marker
is emitted only after the syscall returns nil.

## 3. Closed recovery contract

The classifier requires the exact 107-byte baseline index prefix and exact 57-byte baseline segment
prefix. Candidate suffixes must be absent, an exact strict prefix, or exact complete bytes. It uses
the same five closed states as `GenerationAppendResult.Reconcile`:

- `unchanged`;
- `journal_torn`;
- `journal_complete`;
- `checkpoint_torn`; and
- `composite_complete`.

The barrier-specific allowed sets are:

| Barrier group                                   | Allowed fresh-mount state                                |
| ----------------------------------------------- | -------------------------------------------------------- |
| before journal write                            | `unchanged`                                              |
| after short journal write                       | `unchanged` or `journal_torn`                            |
| after journal write; before journal `fdatasync` | unchanged, journal torn, or journal complete             |
| after journal `fdatasync`; before index write   | `journal_complete`                                       |
| after short index write                         | `journal_complete` or `checkpoint_torn`                  |
| after index write; before index `fdatasync`     | journal complete, checkpoint torn, or composite complete |
| after index `fdatasync`                         | `composite_complete`                                     |

An index candidate ahead of its journal candidate, wrong baseline, non-prefix bytes, extra bytes,
wrong segment set, wrong journal identity, inventory drift or read failure rejects the guest.

## 4. Fixed environment and artifacts

| Input                          | Exact value                                                                      |
| ------------------------------ | -------------------------------------------------------------------------------- |
| Integration test source        | SHA-256 `0adfc2d6bb26f17b0125bde5a21a279ab796fce8d4936b5ebe12ecadde062410`       |
| Guest init script              | SHA-256 `cac43927f125257342706e938f154d02b16f5fdd0e84abaa0cd77a23e9f0b983`       |
| Container runner script        | SHA-256 `e150fd2cc0672f004b0d61af60d8140952b175a7998c8f9be7f9455159e62926`       |
| Host wrapper script            | SHA-256 `b4c320af7cbd1ff03fdf46978f8c4e187d8ead6dab3031ccad3b218d31ca23a2`       |
| Linux/arm64 integration binary | SHA-256 `89f4185ae8b45ac3ed9994abad84f7140fc8fca23e57e9e521648473bb03d9b5`       |
| Linux/amd64 compile artifact   | SHA-256 `649a2c8665d15f1c0d215b0a29647ec8182a683529d5a94bd3dbe93bad83a0b5`       |
| Base image ref                 | `alpine@sha256:14358309a308569c32bdc37e2e0e9694be33a9d99e68afb0f5ff33cc1f695dce` |
| Local image ID                 | `sha256:2c15e55df5d63efb31b629a557df305130612a16feb029c93447e54dda2c4189`        |
| QEMU                           | `qemu-system-aarch64-10.0.0-r1`; TCG `virt`, Cortex-A72, 2 vCPU, 768 MiB         |
| Guest kernel                   | `linux-virt-6.12.103-r0` / `6.12.103-0-virt`                                     |
| ext4 / xfs tooling             | `e2fsprogs-1.47.2-r2` / `xfsprogs-6.14.0-r0`                                     |
| Container package manifest     | SHA-256 `d4db731ed3d00840ece5c114808335af1ac1be3bf6be74f4dd5df590f76e54f0`       |
| Guest package manifest         | SHA-256 `1a2aefaecd9def95205dd19290faa6b0b2a89a1c394fb73aa05820f97f643b38`       |
| QEMU data-disk cache mode      | `cache=none,aio=threads`                                                         |
| ext4 / xfs evidence disk       | fresh 192 MiB / 512 MiB raw image per barrier                                    |
| ShellCheck                     | `shellcheck-0.10.0-r2`, run in the exact base image                              |

The exact base image is never pulled implicitly. Package archive/cache availability remains an
external input; this record does not close `G-SUPPLY-CHAIN`.

## 5. Exact-commit result

After commit and push, local `HEAD`, tracking ref and the remote feature branch all resolved to
`daa6b9ff18b5f625e9b7520503fd16fa6732adb5`. The exact replay command was:

```sh
CLOUD_AGENTS_EVIDENCEFS_HARNESS_IMAGE='alpine@sha256:14358309a308569c32bdc37e2e0e9694be33a9d99e68afb0f5ff33cc1f695dce' \
  ./services/control-plane/scripts/test-evidencefs-linux-powerloss-matrix.sh
```

Both filesystems produced the same observed state sequence:

| Barrier                     | ext4               | xfs                | Recovered index / segment |
| --------------------------- | ------------------ | ------------------ | ------------------------- |
| `before-journal-write`      | unchanged          | unchanged          | 107 B / 57 B              |
| `after-short-journal-write` | unchanged          | unchanged          | 107 B / 57 B              |
| `after-journal-write`       | unchanged          | unchanged          | 107 B / 57 B              |
| `before-journal-fdatasync`  | unchanged          | unchanged          | 107 B / 57 B              |
| `after-journal-fdatasync`   | journal complete   | journal complete   | 107 B / 114 B             |
| `before-index-write`        | journal complete   | journal complete   | 107 B / 114 B             |
| `after-short-index-write`   | journal complete   | journal complete   | 107 B / 114 B             |
| `after-index-write`         | journal complete   | journal complete   | 107 B / 114 B             |
| `before-index-fdatasync`    | journal complete   | journal complete   | 107 B / 114 B             |
| `after-index-fdatasync`     | composite complete | composite complete | 167 B / 114 B             |

The short-write markers prove the half-payload syscalls returned successfully; their bytes were
not present after either fresh mount in this run. The runner reported
`EVIDENCEFS_QEMU_GENERATION_APPEND_BARRIER_MATRIX ... barriers=10 result=PASS` separately for ext4
and xfs, then reported the host-level matrix and exact integration binary digest as `PASS`.

The host-visible loop-device set was byte-equal before and after the run, and no task-owned
container remained.

## 6. Other gates

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
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go vet -tags=evidencefsintegration ./internal/evidencefs
go test -count=1 ./internal/evidencefs
go test -race -count=1 ./internal/evidencefs
go test -run '^$' ./...
go vet ./...
go build ./...
git diff --check
```

The exact parent implementation had already passed `go test -count=1 -timeout=25m ./...`; the
unchanged migration package required `965.821s`. This commit changes only Linux-tagged test code and
shell harnesses, so the 16-minute default-tag execution suite was not repeated. The exact current
commit did compile every default package with `-run '^$'` and executed the affected evidencefs
normal/race suites.

## 7. Authority and destructive-action boundary

All new Go behavior lives in a `_test.go` file guarded by `linux && evidencefsintegration` and adds
no production constructor, backend injection or exported authority. Production `Open` is invoked
after every crash and must still return `ErrTrustedMountAuthority` before the package-private test
authority can inspect the mount.

The only forced termination target is the child `qemu-system-aarch64` process inside the
task-owned one-shot container. The harness does not reboot the host, invoke SysRq, reset a host
block device, unmount a host filesystem or target an existing VM.

No migration DTO, C3 decoder, receipt, verifier, runner, database, `EvidenceSink`, controller or
deployment path participates. Index and journal payloads remain opaque evidencefs test bytes.

## 8. Explicitly open

This record does **not** prove or authorize:

- target registration, generation-directory/header creation, activation index append, handoff,
  segment rotation, standalone checkpoint heal, resync, truncate or discard at every syscall
  barrier;
- a non-forgeable trusted-mount provisioner, positive production constructor or required-syscall
  probe;
- a physical storage-controller reset, volatile hardware write-cache behavior, physical host power
  removal, cloud block device or bare-metal kernel/storage stack;
- x86_64 runtime execution, another QEMU/kernel version or another filesystem;
- a production migration/runner/DB path, typed C3 frames, receipts, public `EvidenceSink` or
  `Connect`;
- filesystem-slice Done, independent reviewer closure, a P1 Gate, Platform RC, deployment, Beta,
  GA or release.

This record closes only the package-private isolated-QEMU existing-segment composite-append
barrier evidence item. Production `Open` remains fail closed, and P1-A2.1b remains out of scope
until the remaining filesystem authority and durability work is separately fixed, implemented and
reviewed.
