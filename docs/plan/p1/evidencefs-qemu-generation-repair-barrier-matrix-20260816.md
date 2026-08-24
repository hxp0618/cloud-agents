# P1 evidencefs QEMU generation-repair barrier matrix — 2026-08-16

- Status：**IMPLEMENTATION + ISOLATED QEMU EVIDENCE — PASS；Gate OPEN**
- Fixed source：`7d78e3d9956e9e046433f2584e8b19d5719ac7ae`
- Source tree：`096993887cf866033fcbe84b5b3d0ae2b5edf684`
- Branch：`codex/cloud-agents-platform-p1`
- Evidence snapshot：`2026-08-16T11:08:30Z`
- Executor：Codex P1 implementation executor
- Independent Gate reviewer：**not assigned**

## 1. Scope and authority boundary

This slice adds an isolated QEMU crash/reopen matrix for four already-implemented retained-generation
repair operations:

1. `ResyncGenerationSnapshot`;
2. `TruncateGenerationTails`;
3. `AppendGenerationCheckpoint`; and
4. `GenerationRotationResult.DiscardIncompleteSegment`.

All new Go behavior is in a `_test.go` file guarded by `linux && evidencefsintegration`. The helper
continues to enter evidencefs only through the existing package-private test authority. Production
`Open` is checked during every fresh classifier boot and still returns `ErrTrustedMountAuthority`.
No public constructor, raw path/FD adoption seam, migration-side seal or trusted provisioner was added.

`GenerationRotationResult.Reconcile` is read-only and therefore has no mutation barrier of its own.
The prior 26-barrier rotation matrix already classifies all ten response-lost rotation states. This
slice additionally invokes `Reconcile` to prove the durable empty-segment precursor before exercising
discard, then classifies the post-crash empty/absent state from a fresh mount.

## 2. Closed barrier families

| Scenario   | Barriers per filesystem | Mutation order and fresh-mount states                                      |
| ---------- | ----------------------- | -------------------------------------------------------------------------- |
| resync     | 4                       | final segment `fdatasync` → lineage index `fdatasync`; bytes stay complete |
| truncate   | 8                       | segment truncate/sync → index truncate/sync                                |
| checkpoint | 5                       | index write/short-write → index `fdatasync`                                |
| discard    | 4                       | exact new-final-segment unlink → journal-directory `fsync`                 |
| **Total**  | **21**                  | **42 barriers across ext4 + XFS**                                          |

The truncate classifier accepts only the ordered closed set
`unchanged → segment_truncated → both_truncated`. It rejects an index-only truncation, extension,
wrong prefix, extra segment or any other cross-file combination. The checkpoint classifier accepts
only unchanged, a strict prefix of the exact candidate checkpoint, or the complete candidate. The
discard classifier accepts only the exact durable empty response-lost final segment or its absence;
any non-empty/unexpected segment is rejected.

The crash backend that prepares discard deliberately returns an error before the first header write,
after the new empty segment and its directory entry have both completed their normal durability
barriers. This produces a real in-memory `Unknown` rotation capability; discard is not authorized by
the observed bytes alone.

## 3. Isolated ext4/XFS execution

The exact local Linux/arm64 integration binary was injected into the existing Alpine 3.22/QEMU
arm64 harness. Every barrier used a fresh raw evidence disk, a fresh guest filesystem, an exact
syscall-bound pause, whole-QEMU `SIGKILL`, and a second QEMU process that mounted the crashed disk and
classified all files through sealed inventory views followed by `Revalidate`.

| Filesystem | Scenarios                          | Crash/reopen barriers | Result |
| ---------- | ---------------------------------- | --------------------- | ------ |
| ext4       | resync/truncate/checkpoint/discard | 21                    | PASS   |
| XFS        | resync/truncate/checkpoint/discard | 21                    | PASS   |

That is 42 exact barriers and 84 crash/classifier guest boots. The host wrapper proved the loop-device
set was byte-equal before and after the run and that no task-owned container remained.

Observed persistence converged as follows on both filesystems:

- every resync boundary reopened with the exact complete bytes;
- truncate remained unchanged before the first file sync, became `segment_truncated` after segment
  `fdatasync`, and became `both_truncated` after index `fdatasync`;
- the checkpoint candidate was absent before index `fdatasync` and complete afterward; and
- discard retained the empty segment before directory `fsync` and reopened absent afterward.

The classifier intentionally allows the broader protocol-safe pre-sync alternatives because a
different controller/cache may persist an unsynced truncate, write or unlink earlier. Only the
post-`fdatasync`/post-directory-`fsync` state is required to have converged.

## 4. Exact source and artifacts

| Input                                      | Exact value                                                                      |
| ------------------------------------------ | -------------------------------------------------------------------------------- |
| shared tagged Linux integration test       | SHA-256 `6f054ee67c6310d91798d11a4d97f50b6b24fbb79d0c7d68d23d8586d0595be7`       |
| generation-repair tagged integration test  | SHA-256 `b84340b11da96c16f135bf8d479f72035d821b1fa3eeb033c12c06d2a5e34d71`       |
| QEMU container harness                     | SHA-256 `803e43380170a0aa19932c1974df28cceceec1b127233c1a0a72209738c84d9d`       |
| QEMU guest init                            | SHA-256 `4e08e7b2efd00be4b1df6c91f303aa5c2e58928dd3e8d831be00392ba3d1fba2`       |
| host power-loss wrapper                    | SHA-256 `4e475709eae670351ac191d2f2429555032807ebe30970ea3023ce14d603ea14`       |
| Linux/amd64 tagged integration binary      | SHA-256 `61ad73b57f451fd0bb1c7f5db2e799238d119ad5d43aa82ebdbe24ffbbd5969d`       |
| Linux/arm64 tagged integration/QEMU binary | SHA-256 `78302159b85fb31efdb24335897785e7b95cfa553d241a07ce03fc5ef4d89a7c`       |
| exact harness image ref                    | `alpine@sha256:14358309a308569c32bdc37e2e0e9694be33a9d99e68afb0f5ff33cc1f695dce` |
| exact harness image ID                     | `sha256:2c15e55df5d63efb31b629a557df305130612a16feb029c93447e54dda2c4189`        |

The fixed harness package manifest was
`d4db731ed3d00840ece5c114808335af1ac1be3bf6be74f4dd5df590f76e54f0`; the fixed guest package
manifest was `1a2aefaecd9def95205dd19290faa6b0b2a89a1c394fb73aa05820f97f643b38`.

## 5. Gates

The fixed source passed:

```sh
go test -count=1 ./internal/evidencefs
go test -race -count=1 ./internal/evidencefs
go test -run '^$' ./...
go vet ./...
go build ./...
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go vet -tags=evidencefsintegration ./internal/evidencefs
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go test -c -tags=evidencefsintegration ./internal/evidencefs
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go test -c -tags=evidencefsintegration ./internal/evidencefs
sh -n scripts/evidencefs-powerloss-guest-init.sh
sh -n scripts/evidencefs-powerloss-container.sh
bash -n scripts/test-evidencefs-linux-powerloss-matrix.sh
shellcheck scripts/evidencefs-powerloss-guest-init.sh \
  scripts/evidencefs-powerloss-container.sh \
  scripts/test-evidencefs-linux-powerloss-matrix.sh
git diff --check
```

ShellCheck ran as exact `shellcheck-0.10.0-r2`. The new narrow scope also passed its explicit invalid-scope
rejection before any image/container work. The QEMU command was:

```sh
CLOUD_AGENTS_EVIDENCEFS_HARNESS_IMAGE='alpine@sha256:14358309a308569c32bdc37e2e0e9694be33a9d99e68afb0f5ff33cc1f695dce' \
CLOUD_AGENTS_EVIDENCEFS_APK_REPOSITORY='https://mirrors.tuna.tsinghua.edu.cn/alpine' \
CLOUD_AGENTS_EVIDENCEFS_MATRIX_SCOPE='generation-repair' \
scripts/test-evidencefs-linux-powerloss-matrix.sh
```

The historical full QEMU matrix and unrelated long-running migration suite were not rerun; the new
narrow scope exercised every changed barrier and both filesystems.

## 6. Remaining boundary

This record does **not** provide:

- a non-forgeable trusted-mount provisioner or successful production `Open`;
- positive cross-package production reopen/activation/handoff/session integration;
- physical controller, host-power, storage-cache or device power-cut evidence;
- automatic filesystem repair after an unclassifiable corrupt state;
- runner/DB `Connect`, deployment, independent immutable Gate review, Platform RC, Beta or GA.

`G-DATA`、`G-AUTHORITY-P1`、`G-SECURITY-P1` and the aggregate Gates remain `IN PROGRESS`.
