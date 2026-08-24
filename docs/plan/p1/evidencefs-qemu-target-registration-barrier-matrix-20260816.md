# P1 evidencefs QEMU target-registration barrier power-loss matrix — 2026-08-16

- Status：**IMPLEMENTATION EVIDENCE — PASS；Gate OPEN**
- Fixed source：`139d53a16e4b990fcf27167caabd383daaf52ecd`
- Source tree：`e04dd2a12d339d5ed7e03c31959a5f46119ec52c`
- Branch：`codex/cloud-agents-platform-p1`
- Evidence snapshot：`2026-08-16T06:55:44Z`
- Executor：Codex P1 implementation executor
- Independent Gate reviewer：**not assigned**

## 1. Scope

This record extends the isolated QEMU harness fixed by
[`evidencefs-qemu-generation-activation-barrier-matrix-20260816.md`](evidencefs-qemu-generation-activation-barrier-matrix-20260816.md)
with target-lineage registration and fresh prefix recovery. Earlier records remain historical and
unchanged.

For every barrier and each of ext4 and xfs, the harness creates a fresh raw evidence disk and a new
guest. The guest starts from the closed root grammar containing only `objects/sha256` and
`lineages.lock`. A package-private test mount authority acquires the sealed revision-zero
`AdmissionInventory` for one fixed target. Production `Open` is checked in every fresh classifier and
continues to reject with `ErrTrustedMountAuthority`.

Two independent barrier families run:

1. `CreateTargetLineage` starts from `target_absent` and covers creation and durability of
   `lineages/`, the target directory, `writer.lock`, the exact nonblocking flock, and `index.caj`;
2. `RecoverTargetLineage` starts from a separately constructed and fully durable
   `registration_prefix_index` whose index is the exact 23-byte prefix of the 47-byte candidate
   header. It covers parent durability replay, the retained lock, truncate-to-zero, rewrite and final
   durability.

The host kills the entire QEMU process at one exact syscall boundary. A second new QEMU process mounts
the same disk, obtains a fresh sealed inventory, classifies the physical prefix and consumes a new
mutation token. `target_absent` is recreated; every prefix state is recovered. Success requires exact
47-byte index contents, `registered_empty`, zero generation journals and a final
`AdmissionInventory.Revalidate`.

This is filesystem implementation evidence. No token, fd, transition result or in-memory authority
survives the crash, but the package-private test constructor is not a production trusted-mount
provisioner.

## 2. Exact create barriers

The create backend is dormant until the absent-target inventory and one-shot mutation token exist.
Once armed, it records the exact root, lineages, target, lock and index descriptors returned by the
real Linux backend. Every marker is emitted immediately before a real syscall or only after that
syscall returns successfully.

The 25 create barriers are:

1. before/after `mkdirat(<root>, "lineages")`;
2. before/after `fsync(<root>)`;
3. before/after `mkdirat(<lineages>, <target>)`;
4. before/after `fsync(<lineages>)`;
5. before/after exclusive `writer.lock` creation;
6. before/after `fdatasync(writer.lock)`;
7. before/after `fsync(<target>)` for the lock entry;
8. before/after the exact nonblocking `flock(writer.lock)`;
9. before/after exclusive `index.caj` creation;
10. before index write, after a successful real half write, and after the complete write;
11. before/after `fdatasync(index.caj)`; and
12. before/after the final `fsync(<target>)`.

The wrapper records descriptor roles from exact parent/name relationships. The short-write marker is
emitted only after the real syscall writes the requested half payload. The production loop would retry
a natural short write, but QEMU is killed while the marker is blocked.

## 3. Exact recovery barriers

The recovery baseline is created before arming and made durable in this order:

`lineages mkdir → root fsync → target mkdir → lineages fsync → lock create/fdatasync → target fsync →`
`23-byte index create/write/fdatasync → target fsync`.

`AcquireAdmission` then locks the exact prefix `writer.lock`. The recovery backend observes that held
descriptor during acquisition and refuses to arm unless it exists. The 21 recovery barriers are:

1. before/after replaying `fsync(<lineages>)`;
2. before/after `fdatasync` on the exact retained `writer.lock`;
3. before/after replaying `fsync(<target>)` for the lock;
4. before/after re-flocking the exact retained lock fd;
5. before/after opening the existing `index.caj` read-write;
6. before/after `ftruncate(index.caj, 0)`;
7. before/after the truncate `fdatasync`;
8. before rewrite, after a successful real half `pwrite`, and after the complete rewrite;
9. before/after the final index `fdatasync`; and
10. before/after the final target-directory `fsync`.

The baseline already has durable directory entries, so recovery measures byte durability rather than
new-entry persistence. It never removes, renames or replaces the prefix.

## 4. Closed fresh-mount classification

The classifier accepts only these physical states:

- `target_absent`;
- `registration_prefix_directory`;
- `registration_prefix_lock` with exact zero-length `writer.lock`;
- `registration_prefix_index_empty` with a zero-length index;
- `registration_prefix_index_torn` whose bytes are a strict prefix of the fixed header; or
- `registration_prefix_index_complete` whose bytes exactly equal the fixed header.

Any other child set, non-prefix bytes, additional lineage/journal, wrong target, wrong file identity,
read failure, stale inventory or final revalidation failure rejects the guest. A complete prefix is
still observed as `registration_prefix_index` after a fresh acquisition: only the recovery transition
can promote it to `registered_empty` under the new epoch.

The barrier-specific allowed sets are wider than the single observed run where filesystem semantics
permit either the prior or attempted state. The successful replay observed the following exact counts
on both filesystems:

| Scenario | Fresh-mount state                    | ext4 | xfs |
| -------- | ------------------------------------ | ---: | --: |
| create   | `target_absent`                      |    7 |   7 |
| create   | `registration_prefix_directory`      |    4 |   4 |
| create   | `registration_prefix_lock`           |   11 |  11 |
| create   | `registration_prefix_index_complete` |    3 |   3 |
| recovery | `registration_prefix_index_torn`     |   13 |  13 |
| recovery | `registration_prefix_index_empty`    |    5 |   5 |
| recovery | `registration_prefix_index_complete` |    3 |   3 |

No create-side empty/torn index survived this replay: before index `fdatasync`, both filesystems
reopened at `registration_prefix_lock`; after it returned, both reopened with the exact 47-byte index.
For recovery, both filesystems retained the old 23-byte prefix until truncate `fdatasync`, then
reopened at zero bytes until final index `fdatasync`, and finally reopened at 47 bytes.

Every one of the 92 fresh classifiers (46 per filesystem) subsequently reached exact
`registered_empty` and passed `Revalidate`.

## 5. Fixed environment and artifacts

| Input                          | Exact value                                                                           |
| ------------------------------ | ------------------------------------------------------------------------------------- |
| Integration test source        | SHA-256 `0fd6c0f62c4f077748a12699b87ec955b7b3c45a0bb8f61a7db555807a0b6533`            |
| Guest init script              | SHA-256 `0a77c39e03b349291f125201cfb863099e21f0463ca2fc98143be8e03eb8d20e`            |
| Container runner script        | SHA-256 `2e8c92e25a2f10169f14495788bf1d2d3dd6c59021f5d1422efb5d35f2ecc313`            |
| Host wrapper script            | SHA-256 `1c10a892da5e27e1c81b5ec35747e0861d6da075ebce7bc592068ff28d6cdb94`            |
| Linux/arm64 integration binary | SHA-256 `f0af995c5205bb93cc6b64bf82ec0f80fcab860e65e1975f7ed5d222f4097ddb`            |
| Linux/amd64 compile artifact   | SHA-256 `7400c1bcf86e15d61e943c700bb5a2a912f057841af88337a070a1ca7d9c4add`            |
| Exact replay stdout            | 419 lines；SHA-256 `e454766310bf3afa211a9fb93be51d8d8e39afa81b139761131f8160b212bb18` |
| Exact replay stderr            | 8 lines；SHA-256 `d4d216fc4009d135a5929d4cb1d2e4201d8f2e6082a27959b35986cb02a10dd1`   |
| Base image ref                 | `alpine@sha256:14358309a308569c32bdc37e2e0e9694be33a9d99e68afb0f5ff33cc1f695dce`      |
| Local image ID                 | `sha256:2c15e55df5d63efb31b629a557df305130612a16feb029c93447e54dda2c4189`             |
| APK repository                 | `https://mirrors.tuna.tsinghua.edu.cn/alpine`                                         |
| APK repositories manifest      | SHA-256 `987dd9199983e359711f6c72200f99835e1a16d68c705baf429e8b5bbc20c9b4`            |
| Container package manifest     | SHA-256 `d4db731ed3d00840ece5c114808335af1ac1be3bf6be74f4dd5df590f76e54f0`            |
| Guest package manifest         | SHA-256 `1a2aefaecd9def95205dd19290faa6b0b2a89a1c394fb73aa05820f97f643b38`            |
| QEMU / guest kernel            | `qemu-system-aarch64-10.0.0-r1` / `linux-virt-6.12.103-r0`                            |
| ext4 / xfs tooling             | `e2fsprogs-1.47.2-r2` / `xfsprogs-6.14.0-r0`                                          |
| QEMU execution                 | TCG `virt`, Cortex-A72, 2 vCPU, 768 MiB；`cache=none,aio=threads`                     |
| ext4 / xfs evidence disk       | fresh 192 MiB / 512 MiB raw image per barrier                                         |

The host accepts only the official Alpine base URL, the exact TUNA URL above, or an omitted override.
It passes the selected value explicitly to the container; the container independently rejects every
other value and records the repositories file before installing packages. APK signatures and exact
package versions remain enforced, and the package manifests are byte-equal to the earlier official-
transport replay. The mirror is a transport input, not a new package trust root.

The exact image is never pulled implicitly. The eight stderr lines are the unchanged QEMU group note
and two APK cache-fallback warnings; no Go test, QEMU, filesystem or classifier error appears there.

## 6. Exact replay and mechanical result

The exact replay command was:

```sh
CLOUD_AGENTS_EVIDENCEFS_HARNESS_IMAGE='alpine@sha256:14358309a308569c32bdc37e2e0e9694be33a9d99e68afb0f5ff33cc1f695dce' \
  CLOUD_AGENTS_EVIDENCEFS_APK_REPOSITORY='https://mirrors.tuna.tsinghua.edu.cn/alpine' \
  ./services/control-plane/scripts/test-evidencefs-linux-powerloss-matrix.sh
```

Mechanical recounting of the fixed 419-line stdout produced, for each filesystem:

- base durable-return matrix: 1 PASS;
- object publish: 11 crash and 11 reopen lines, plus 1 family PASS;
- target registration create: 25 crash and 25 reopen lines, plus 1 family PASS;
- target registration recovery: 21 crash and 21 reopen lines, plus 1 family PASS;
- existing-segment append: 10 crash and 10 reopen lines, plus 1 family PASS;
- retained rotation: 26 crash and 26 reopen lines, plus 1 family PASS; and
- generation activation: 5 crash and 5 reopen lines, plus 1 family PASS.

The log contains exactly one fixed APK line, one QEMU environment line, one container final PASS, one
host exact-image/binary PASS and one host final PASS. The wrapper proved the host loop-device set was
byte-equal before and after the run, and no task-owned container remained.

An earlier source-fixed attempt at `ffff5c5` used the default official repository transport. It was
stopped after 20 minutes while still inside pre-QEMU `apk add`; stdout and stderr were both empty, no
raw evidence disk or QEMU assertion ran, and task-owned container/temp state was removed. It is an
operational transport failure and is not used as passing evidence. `139d53a` added only the restricted,
explicit transport selector before the successful replay.

## 7. Other gates

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

The host-side invalid-repository fault also rejected before creating a temporary run or container.
ShellCheck ran as exact `shellcheck-0.10.0-r2` inside the fixed Linux/arm64 base image. The unchanged
default migration implementation suite was not unnecessarily rerun; every default package was
compiled with `-run '^$'`, and the affected evidencefs suite ran normal and race with cache disabled.

## 8. Remaining boundary

This record closes the test-only virtual barrier matrix for target registration create and torn-prefix
recovery only. It does **not** provide:

- a trusted production mount provisioner or production `Open` success path;
- generation-directory/lock/segment-0 header create/recovery barriers;
- all remaining repair/resync barriers;
- physical controller, host-power or real storage-cache loss evidence;
- independent immutable Gate review;
- public sink, runner/DB `Connect`, deployment, Platform RC, Beta or GA.

`G-DATA`、`G-AUTHORITY-P1`、`G-SECURITY-P1` and the aggregate Gates therefore remain
`IN PROGRESS`.
