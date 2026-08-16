# P1 evidencefs required-syscall probe — 2026-08-16

- Status：**IMPLEMENTATION + REAL RUNTIME EVIDENCE — PASS；Gate OPEN**
- Fixed source：`a3c7651fc01c7ad69e1c0f6a6bc87d3d2854df80`
- Source tree：`9e81a63a4ebb197af30bce8dbe4a8914f4966feb`
- Branch：`codex/cloud-agents-platform-p1`
- Evidence snapshot：`2026-08-16T10:38:11Z`
- Executor：Codex P1 implementation executor
- Independent Gate reviewer：**not assigned**

## 1. Scope and authority boundary

This slice implements the evidencefs online required-syscall probe frozen by ADR-0010. It does not
implement or simulate the missing trusted-mount provisioner.

`newRootWithRequiredProbe` remains package-private and accepts only the existing private
`mountAuthority`. An evidencefs-package-wide static test rejects any production consumer outside the defining
probe/root files, and public `evidencefs.Open` continues to return `ErrTrustedMountAuthority` before
performing a probe or mutation. No exported test constructor, raw path/FD adoption API, interface
authority, environment bypass or migration-side seal was added.

The future provisioner must first mint a non-forgeable mount capability and only then enter this
probe. Passing a kernel mountinfo consistency check alone remains insufficient.

## 2. Closed online probe

The probe acquires the existing `<root>/lineages.lock` root-wide lease before opening
`objects/sha256`. It then verifies:

1. a second independently opened file description cannot acquire the already-held exclusive flock;
2. three distinct 128-bit CSPRNG values produce only exact `.tmp-<32lowerhex>` names that were absent
   under the root lock;
3. an `O_NOFOLLOW|O_CREAT|O_EXCL` regular temp accepts the fixed payload, has exact owner/mode/device/
   inode/link-count/size metadata and completes `fdatasync`;
4. `fsync(objects/sha256)` closes the create directory-entry barrier;
5. `renameat2(RENAME_NOREPLACE)` moves the source to an absent temp name, removes the source name,
   preserves inode/content identity and completes a directory `fsync`;
6. a second synced temp cannot replace the existing destination and both files remain byte/identity
   exact after the expected conflict; and
7. every owned temp is unlinked independently and the containing directory is synced before success.

The independent open-file-description lock check exercises the same Linux `flock` exclusion used
across processes. The earlier fixed real ext4/xfs matrix at `180929a` separately proves an actual
second process cannot take this root lock while the first process holds it.

All probe file names remain inside the existing object temp grammar. A crash/cleanup-unknown leftover
is therefore a normal conservatively counted temp rather than an ignored probe namespace or a final
object.

## 3. Failure and cleanup semantics

A context cancellation or deadline before the first temp mutation returns the original context error
and leaves the root retryable. After any create, cancellation becomes a filesystem failure and the
root is poisoned.

Fault tests cover lock error/non-exclusion, CSPRNG failure, write, `fdatasync`, directory `fsync`,
rename failure, rename response loss, source-not-removed behavior, conflict error, illegal replacement,
file/contender/directory close failure and cleanup unlink failure. Rename uncertainty drains both
possible names. Cleanup attempts every owned unlink plus final directory `fsync` without short-circuit.
A failed cleanup leaves only a valid, conservatively countable temp name and returns no usable root.

Random-name collision retries never delete or modify the pre-existing temp. All descriptor-close
failures override clean success and poison the root.

## 4. Real ext4/XFS runtime matrix

The fixed Linux/amd64 tagged test binary was copied to an authorized isolated Debian 12 test host
running kernel `6.1.0-15-amd64`. Each filesystem used a fresh sparse image and a private loop mount
with `nosuid,nodev,noexec`; the closed evidence root was created only inside that temporary image.

| Filesystem | Image   | Result | Observation                                                             |
| ---------- | ------- | ------ | ----------------------------------------------------------------------- |
| ext4       | 256 MiB | PASS   | online lock/data-sync/directory-sync/no-replace/cleanup probe completed |
| XFS        | 1 GiB   | PASS   | the same fixed binary and probe completed against the XFS loop mount    |

The first ext4 fixture attempt correctly failed before the probe because the freshly mounted
filesystem root had default mode `0755`. After the fixture reset mode `0700`, the second attempt again
failed before the probe because ext4's automatic `lost+found` violated the closed root grammar. The
final fresh fixture removed that empty, task-owned directory during image initialization and then
passed. These were fail-closed fixture corrections, not code changes or probe failures.

The host did not have xfsprogs installed. The run downloaded Debian `xfsprogs 6.1.0-1`, `libinih1` and
`liburcu8` from the already-configured Aliyun mirror, extracted them only under the task-owned `/tmp`
directory and invoked `mkfs.xfs` with a temporary `LD_LIBRARY_PATH`. It did not install or remove a
system package. The XFS kernel module was loaded only for the mount and unloaded afterward.

Post-run checks proved there was no task mount, image directory, test binary, installed xfsprogs
package or newly loaded XFS module left on the host. This matrix tests online syscall behavior; it is
not a controller reset, process restart, power-loss or trusted-mount authority test.

## 5. Exact source and artifacts

| Input                                 | Exact value                                                                |
| ------------------------------------- | -------------------------------------------------------------------------- |
| evidencefs root source                | SHA-256 `e202cdd4608d83abdf60858f23efb42a0458c295a128f1bd614e60a80e3170e2` |
| required-syscall probe source         | SHA-256 `d009fb7cd4ac3b052707316297ccaa2319673668c7e2379e79ebfca6baed289e` |
| required-syscall probe tests          | SHA-256 `9b93728058f0079ef30514872a57b2e994f7fef961ff13d194b0df37f819207d` |
| shared fake backend tests             | SHA-256 `e6ae99e0e9700a5e17332fd5925a0d4413c3b4c813b793284bc05002b79c9133` |
| tagged Linux integration tests        | SHA-256 `2ab39e77e5142962cbb38f8c16c8d93307d4f8812215dad66aebe978e632da1f` |
| Linux/amd64 tagged integration binary | SHA-256 `597451d6ab2088f12d01c29cedf4260d324a105686cfbf4059a1c14578c2a365` |
| Linux/arm64 tagged integration binary | SHA-256 `8407f01dc7640ef9963967bdf615ecf317e90215c89889c73159bd27b4129904` |

## 6. Gates

The fixed source passed:

```sh
go test -count=1 ./internal/evidencefs
go test -race -count=1 ./internal/evidencefs
go vet ./...
go build ./...
go test -run '^$' ./...
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go vet -tags=evidencefsintegration ./internal/evidencefs
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go test -c -tags=evidencefsintegration ./internal/evidencefs
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go test -c -tags=evidencefsintegration ./internal/evidencefs
git diff --check
bun scripts/secret-scan.ts
```

The exact Linux/amd64 binary then passed `TestLinuxRequiredSyscallProbe` once on each fresh ext4 and
XFS image. Local source direction checks also confirmed evidencefs does not import migration.

## 7. Remaining boundary

This record does **not** provide:

- a non-forgeable trusted-mount provisioner or successful production `Open`;
- automatic creation of a missing `lineages.lock` under provisioner authority;
- a positive cross-package production activation/handoff/session integration;
- process-restart or power-loss behavior during the online probe;
- remaining repair per-barrier matrices or physical controller/host power-loss evidence;
- runner/DB `Connect`, deployment, independent immutable Gate review, Platform RC, Beta or GA.

`G-DATA`、`G-AUTHORITY-P1`、`G-SECURITY-P1` and the aggregate Gates remain `IN PROGRESS`.
