# P1 evidencefs QEMU generation-header barrier power-loss matrix — 2026-08-16

- Status：**IMPLEMENTATION EVIDENCE — PASS；Gate OPEN**
- Fixed source：`f650faeaef01bfc9cd94dce387a7794f08c9f598`
- Source tree：`c9a2aa87cd3d84fe174e7e397e6e3345ad0ff65e`
- Branch：`codex/cloud-agents-platform-p1`
- Evidence snapshot：`2026-08-16T08:02:19Z`
- Executor：Codex P1 implementation executor
- Independent Gate reviewer：**not assigned**

## 1. Scope

This record extends the isolated QEMU harness fixed by
[`evidencefs-qemu-target-registration-barrier-matrix-20260816.md`](evidencefs-qemu-target-registration-barrier-matrix-20260816.md)
with generation-journal directory/lock prefix inventory and deterministic segment-0 recovery. Earlier
records remain historical and unchanged.

The implementation adds two evidencefs-owned, revision-bound physical facts:

- `generation_prefix_directory` for an otherwise empty canonical journal directory; and
- `generation_prefix_lock` for the same directory with an exact zero-length `writer.lock`.

Both facts are sealed into the complete `AdmissionInventory` full-set digest, immutable slot graph and
root journal limit. A directory containing `writer.lock` plus segment-0 remains an ordinary
`AdmissionJournalView`; evidencefs reads its opaque bytes but does not decode C3. Malformed child sets,
non-zero lock files, segment gaps and count overflow mint no prefix authority.

`RecoverGenerationHeader` accepts only the current target lineage and one exact journal identity from
that inventory. It handles directory-only, lock-only, zero-byte segment, strict byte-prefix segment and
already-complete segment states. Existing non-prefix bytes and multi-segment journals are rejected
before mutation. Torn/empty segment recovery durably truncates to zero before rewriting. The transition
never removes, renames or replaces the prefix, retains the exact generation lock for the following
activation/handoff path, advances the inventory revision and requires a final exact-byte inventory plus
`Revalidate`.

Every mutation/durability attempt closes the result algebra: any subsequent failure is `unknown` and
revokes the full admission epoch. Context or closed-input failures before the first attempt preserve the
pre-mutation result. Migration-side brand-new reserve/header validators now also require zero generation
prefix facts; they cannot ignore a prefix and mint an incompatible permit.

For this matrix, every barrier starts with a fresh raw ext4 or xfs evidence disk. The host kills the
entire QEMU process at one exact syscall boundary. A second new QEMU process mounts the same disk,
obtains a fresh sealed inventory, classifies the physical state and consumes a new mutation token.
Production `Open` is checked in every classifier and continues to reject with
`ErrTrustedMountAuthority`.

This is filesystem implementation evidence. The package-private test mount authority is not a
production trusted-mount provisioner, and the fresh classifier does not substitute for the still-open
migration-owned verified-plan/reopen binder.

## 2. Exact create barriers

The create baseline is a fully durable registered target lineage with no generation journal. The
backend is armed only after the target lineage lock, revision-bound inventory and one-shot mutation
token exist. The 21 create barriers are:

1. before/after exclusive generation-directory creation;
2. before/after replaying `fsync(<target-lineage>)` for that directory entry;
3. before/after exclusive `writer.lock` creation;
4. before/after `fdatasync(writer.lock)`;
5. before/after `fsync(<journal>)` for the lock entry;
6. before/after the exact nonblocking `flock(writer.lock)`;
7. before/after exclusive `segment-00000000.caj` creation;
8. before segment write, after a successful real half `pwrite`, and after the complete write;
9. before/after `fdatasync(segment-00000000.caj)`; and
10. before/after the final `fsync(<journal>)`.

Markers are bound to descriptors discovered from exact parent/name relationships and are emitted
immediately before the real syscall or only after it returns successfully. The half-write marker blocks
after the real syscall has written exactly half the candidate.

## 3. Exact recovery barriers

The recovery baseline is independently constructed and fully durable:

`registered target → journal mkdir/parent fsync → lock create/fdatasync/directory fsync →`
`28-byte segment prefix create/write/fdatasync/directory fsync`.

`AcquireAdmission` then locks the target lineage and inventories the one-segment journal. The 23
recovery barriers are:

1. before/after replaying `fsync(<target-lineage>)`;
2. before/after opening the existing journal lock read-write;
3. before/after `fdatasync(writer.lock)`;
4. before/after replaying `fsync(<journal>)` for the lock;
5. before/after the exact nonblocking `flock(writer.lock)`;
6. before/after opening segment-0 read-write;
7. before/after `ftruncate(segment-0, 0)`;
8. before/after the truncate `fdatasync`;
9. before rewrite, after a successful real half `pwrite`, and after the complete rewrite;
10. before/after the final segment `fdatasync`; and
11. before/after the final journal-directory `fsync`.

Because the recovery baseline already has durable directory entries, this family isolates byte
durability and lock reacquisition. The old prefix remains recoverable until truncate durability, zero
bytes remain recoverable until final segment durability, and the final directory sync closes the
barrier chain without changing the exact bytes.

## 4. Closed fresh-mount classification

The classifier admits only:

- `generation_absent`;
- `generation_prefix_directory`;
- `generation_prefix_lock` with exact zero-length lock bytes;
- `generation_segment_empty` with one zero-byte segment;
- `generation_segment_torn` whose bytes are a strict prefix of the fixed 57-byte header; or
- `generation_segment_complete` whose bytes exactly equal that header.

Any other child set, additional segment, non-prefix bytes, wrong lineage/journal identity, stale view,
read failure or revalidation failure rejects the guest. `generation_absent` is completed through
`CreateGenerationHeader`; every other admitted state uses `RecoverGenerationHeader`. All 88 fresh
classifiers reached one exact 57-byte segment, zero remaining prefix facts, a valid durable result and a
final `AdmissionInventory.Revalidate`.

The successful replay observed identical aggregate counts on ext4 and xfs:

| Scenario | Fresh-mount state             | ext4 | xfs |
| -------- | ----------------------------- | ---: | --: |
| create   | `generation_absent`           |    3 |   3 |
| create   | `generation_prefix_directory` |    4 |   4 |
| create   | `generation_prefix_lock`      |   11 |  11 |
| create   | `generation_segment_complete` |    3 |   3 |
| recovery | `generation_segment_torn`     |   15 |  15 |
| recovery | `generation_segment_empty`    |    5 |   5 |
| recovery | `generation_segment_complete` |    3 |   3 |

No create-side empty/torn segment survived this replay: before segment `fdatasync`, both filesystems
reopened at the last durable lock-only state; after it returned, both reopened with the exact 57-byte
segment. Recovery retained the old 28-byte prefix until truncate `fdatasync`, then reopened at zero
bytes until final segment `fdatasync`, and finally reopened at 57 bytes.

## 5. Fixed environment and artifacts

| Input                                 | Exact value                                                                           |
| ------------------------------------- | ------------------------------------------------------------------------------------- |
| Admission discovery source            | SHA-256 `ac14ac364fa443f45c38ff0917cf33b0790cacb3e956d68785fa208707bf2f06`            |
| Admission inventory source            | SHA-256 `aa62e126b8fc5602dab780ecf9ac00d72242d50d0a43ec06dd511aa6381f3c07`            |
| Generation-prefix fact source         | SHA-256 `7a4692ef33da0ff82aaaee6b566fdc83ff0e3d69a18bbc9170169afc3d8e17c5`            |
| Generation recovery source            | SHA-256 `dd7fa57665903c91d8d192b6755f2b0b08341ec1448d807bec0a4078270da9b4`            |
| Generation recovery unit tests        | SHA-256 `1992b4e39fd876140e3f9eab36cc9a50d1bad600efcc24672698def0de71f5cf`            |
| Generation-header integration backend | SHA-256 `628174d471ac593d62eacc3e65c0a3cad2bfbdc60b107f3ef79a957bdf4cab14`            |
| Integration test dispatcher           | SHA-256 `23b38c39c34bd25a34b9aaf0fb60d78a03f6bfe837dca0d13f63d2be5e5bc01d`            |
| Guest init script                     | SHA-256 `72237d59c3efc38fc344516911cffb94546593f43dae8efe4ea710b3df8508ca`            |
| Container runner script               | SHA-256 `92f0a4c7ebf18a17d42da59ce8749f1e0ce3452b92b17d0519c476c144dcad8a`            |
| Host wrapper script                   | SHA-256 `09068ebd919f9bd531b9f546a51b3e1d80b479db3a2bb4aaa755f2df5dc76423`            |
| Linux/arm64 integration binary        | SHA-256 `c2db0fd57656794052875a38d4e2b200cf8983c749e35d2ebc90b7cd9af6f73a`            |
| Linux/amd64 compile artifact          | SHA-256 `d0d59a3af7a0d5453c3df6b124225e93d94d13cd248ee615b4cad8e7750d97e6`            |
| Exact replay stdout                   | 187 lines；SHA-256 `ca8d35f623da13706db899bffe8f999e9b95cce1688bceadafb945423ecc9e8d` |
| Exact replay stderr                   | 8 lines；SHA-256 `d4d216fc4009d135a5929d4cb1d2e4201d8f2e6082a27959b35986cb02a10dd1`   |
| Base image ref                        | `alpine@sha256:14358309a308569c32bdc37e2e0e9694be33a9d99e68afb0f5ff33cc1f695dce`      |
| Local image ID                        | `sha256:2c15e55df5d63efb31b629a557df305130612a16feb029c93447e54dda2c4189`             |
| APK repository                        | `https://mirrors.tuna.tsinghua.edu.cn/alpine`                                         |
| APK repositories manifest             | SHA-256 `987dd9199983e359711f6c72200f99835e1a16d68c705baf429e8b5bbc20c9b4`            |
| Container package manifest            | SHA-256 `d4db731ed3d00840ece5c114808335af1ac1be3bf6be74f4dd5df590f76e54f0`            |
| Guest package manifest                | SHA-256 `1a2aefaecd9def95205dd19290faa6b0b2a89a1c394fb73aa05820f97f643b38`            |
| QEMU / guest kernel                   | `qemu-system-aarch64-10.0.0-r1` / `linux-virt-6.12.103-r0`                            |
| QEMU execution                        | TCG `virt`, Cortex-A72, 2 vCPU, 768 MiB；`cache=none,aio=threads`                     |
| ext4 / xfs evidence disk              | fresh 192 MiB / 512 MiB raw image per barrier                                         |

The exact image is never pulled implicitly. The restricted `generation-header` scope is an exact
allowlisted harness mode used to avoid rerunning previously passed families; unknown scopes reject
before a container is created. The eight stderr lines are the unchanged QEMU group note and two APK
cache-fallback warnings; no Go test, QEMU, filesystem or classifier error appears there.

## 6. Exact replay and mechanical result

The exact replay command was:

```sh
CLOUD_AGENTS_EVIDENCEFS_HARNESS_IMAGE='alpine@sha256:14358309a308569c32bdc37e2e0e9694be33a9d99e68afb0f5ff33cc1f695dce' \
  CLOUD_AGENTS_EVIDENCEFS_APK_REPOSITORY='https://mirrors.tuna.tsinghua.edu.cn/alpine' \
  CLOUD_AGENTS_EVIDENCEFS_MATRIX_SCOPE='generation-header' \
  ./services/control-plane/scripts/test-evidencefs-linux-powerloss-matrix.sh
```

Mechanical recounting of the fixed 187-line stdout produced, for each filesystem:

- generation-header create: 21 crash and 21 reopen lines, plus one family PASS;
- generation-header recovery: 23 crash and 23 reopen lines, plus one family PASS; and
- one filesystem-scope PASS.

The log contains exactly two create-family PASS lines, two recovery-family PASS lines, two scope PASS
lines, one container final PASS, one host exact-image/binary PASS and one host final PASS. The wrapper
proved the host loop-device set was byte-equal before and after the run, and no task-owned container
remained.

## 7. Other gates

The fixed implementation passed:

```sh
go test -count=1 ./internal/evidencefs
go test -race -count=1 ./internal/evidencefs
go test -race -count=1 ./internal/migration \
  -run 'TestGenerationHeader|TestGenerationReservation|TestAdmissionReservedUnregistered'
go test -run '^$' ./...
go vet ./...
go build ./...
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go test -c -tags=evidencefsintegration ./internal/evidencefs
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go test -c -tags=evidencefsintegration ./internal/evidencefs
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go vet -tags=evidencefsintegration ./internal/evidencefs
sh -n scripts/evidencefs-powerloss-guest-init.sh
sh -n scripts/evidencefs-powerloss-container.sh
bash -n scripts/test-evidencefs-linux-powerloss-matrix.sh
shellcheck scripts/evidencefs-powerloss-guest-init.sh \
  scripts/evidencefs-powerloss-container.sh \
  scripts/test-evidencefs-linux-powerloss-matrix.sh
git diff --check
```

ShellCheck ran as exact `shellcheck-0.10.0-r2`. The invalid-scope fault rejected before image/container
work. The unchanged long-running migration suite and previously passed QEMU families were not
unnecessarily rerun; every default package was compiled, the affected migration tests ran under race,
and the complete evidencefs suite ran normal and race with cache disabled.

## 8. Remaining boundary

This record closes the package-private virtual generation-header create and torn-prefix recovery barrier
matrix only. It does **not** provide:

- a trusted production mount provisioner or production `Open` success path;
- a migration-owned same-verifier reopen binder that cross-binds the planned C3 header to these fresh
  physical prefix facts before mutation;
- every remaining generation append/rotation repair/resync barrier;
- physical controller, host-power or real storage-cache loss evidence;
- independent immutable Gate review;
- public sink, runner/DB `Connect`, deployment, Platform RC, Beta or GA.

`G-DATA`、`G-AUTHORITY-P1`、`G-SECURITY-P1` and the aggregate Gates therefore remain
`IN PROGRESS`.
