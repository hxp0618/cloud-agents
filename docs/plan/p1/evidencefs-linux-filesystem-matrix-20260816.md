# P1 evidencefs Linux ext4/xfs clean-restart matrix — 2026-08-16

- Status：**IMPLEMENTATION EVIDENCE — PASS；Gate OPEN**
- Fixed source：`e230c74581cba2ab815406fa045fda9ba1b6f77c`
- Source tree：`71096073e79bb908d3327d50e0a67a8b0292d509`
- Branch：`codex/cloud-agents-platform-p1`
- Evidence snapshot：`2026-08-15T19:17:44Z`
- Executor：Codex P1 implementation executor
- Independent Gate reviewer：**not assigned**

## 1. Scope

This record fixes the first opt-in run of the real `evidencefs` Linux backend on both filesystem
types allowed by ADR-0010. The harness creates a fresh loop-backed filesystem in an owned,
one-shot privileged container, prepares the closed evidence root grammar, and runs a statically
cross-compiled package test against actual Linux syscalls.

For both ext4 and xfs, the test proves:

1. the mounted root reports the exact filesystem magic and a nonzero
   `name_to_handle_at(2)` mount identity;
2. production `evidencefs.Open` still rejects with `ErrTrustedMountAuthority`;
3. a package-private **test-only** authority can use `linuxBackend` to acquire the real
   `lineages.lock`, scan the empty object store, publish one object through temp write,
   `fdatasync`, no-replace rename and directory `fsync`, and bind the publication;
4. a second process cannot acquire the same root-wide flock while the publisher is alive;
5. after the publisher process is forcibly killed without calling `Lease.Close`, a fresh
   process reacquires the lock and verifies the exact object digest and size;
6. after a clean `sync`, unmount and remount of the same loop image, another fresh process
   verifies the same object again; and
7. loop devices use autoclear semantics after mount, and the host-side post-run check observes
   no task-owned container or attached loop device.

The fixed object SHA-256 is
`733b503ad96e7f5f8beb7db76873efe54788d02ea7dbb9761e784483611fa707`.

## 2. Fixed environment and artifacts

| Input                   | Exact value                                                                                                               |
| ----------------------- | ------------------------------------------------------------------------------------------------------------------------- |
| Go toolchain            | `go1.26.5 darwin/arm64`; cross-compiled with `GOOS=linux GOARCH=arm64 CGO_ENABLED=0`                                      |
| Docker server           | `29.4.0` / OrbStack                                                                                                       |
| Linux kernel            | `7.0.14-orbstack-00380-ga7e0a2dc9535` / `linux/arm64`                                                                     |
| Base image ref          | `alpine@sha256:14358309a308569c32bdc37e2e0e9694be33a9d99e68afb0f5ff33cc1f695dce`                                          |
| Local image ID          | `sha256:2c15e55df5d63efb31b629a557df305130612a16feb029c93447e54dda2c4189`                                                 |
| ext4 tooling            | `e2fsprogs-1.47.2-r2`; installed-file manifest SHA-256 `04762094b55c2520d2bd6e1791ce3240d881b710e4949887a8d28dd21371cd50` |
| xfs tooling             | `xfsprogs-6.14.0-r0`; installed-file manifest SHA-256 `23d14b59d7f2f13e30e54f5456a4971ec0ea2ebaee4ca96d332b26d93ff3eab9`  |
| Integration test binary | SHA-256 `2312cecbad57115111c10090313a432475680c3e3619fe4dc3bda4e75510240b`                                                |
| Integration test source | SHA-256 `f236866f04bab8bb38a9c1a5dec69f11c7c1ae0e31a4f97ab19448fa39872c29`                                                |
| Matrix script           | SHA-256 `33585f3712ac1be7bf429104027235518f38ad70faf0f49bd2176aa2a569ccea`                                                |

The ext4 mount reported magic `0xef53`; xfs reported `0x58465342`. Both mounted a dedicated
`/dev/loop0` image at `/mnt/evidence` in a fresh container namespace. Mount IDs are runtime
namespace facts and are intentionally not treated as reusable authority.

The harness base image is digest-pinned and never pulled implicitly. The exact `apk` versions
are requested and their installed-file manifests are recorded, but the APK files were fetched
online during this local run and are not checked-in or published as immutable artifacts. This
is therefore environment evidence, not a `G-SUPPLY-CHAIN` closure.

## 3. Replay commands and results

From `services/control-plane` at the fixed source:

```sh
CLOUD_AGENTS_EVIDENCEFS_HARNESS_IMAGE='alpine@sha256:14358309a308569c32bdc37e2e0e9694be33a9d99e68afb0f5ff33cc1f695dce' \
  ./scripts/test-evidencefs-linux-filesystem-matrix.sh
```

Result:

```text
EVIDENCEFS_LINUX_MATRIX filesystem=ext4 ... test_binary_sha256=2312ce... result=PASS
EVIDENCEFS_LINUX_MATRIX filesystem=xfs  ... test_binary_sha256=2312ce... result=PASS
Evidencefs Linux ext4/xfs clean-restart matrix: PASS
```

The same fixed source also passed:

```sh
GOWORK=off GOTOOLCHAIN=local GOFLAGS=-mod=readonly \
  go test -count=1 ./internal/evidencefs

GOWORK=off GOTOOLCHAIN=local GOFLAGS=-mod=readonly \
  go test -race -count=1 ./internal/evidencefs

GOWORK=off GOTOOLCHAIN=local GOFLAGS=-mod=readonly \
  GOOS=linux GOARCH=arm64 CGO_ENABLED=0 \
  go vet -tags=evidencefsintegration ./internal/evidencefs

GOWORK=off GOTOOLCHAIN=local GOFLAGS=-mod=readonly \
  GOOS=linux GOARCH=arm64 CGO_ENABLED=0 \
  go test -c -tags=evidencefsintegration ./internal/evidencefs

GOWORK=off GOTOOLCHAIN=local GOFLAGS=-mod=readonly \
  GOOS=linux GOARCH=amd64 CGO_ENABLED=0 \
  go test -c -tags=evidencefsintegration ./internal/evidencefs
```

The normal and race package tests passed, both tagged Linux architectures compiled, and the
post-matrix cleanup check returned an empty `losetup -a` plus no containers with the task
ownership label.

## 4. Authority and cleanup boundary

The only positive root constructor used here is in
`backend_linux_integration_test.go`, which is both `_test.go` and guarded by
`linux && evidencefsintegration`. It calls the package-private
`newRootWithAuthority` with a package-private `mountAuthority`; no exported constructor,
build-tagged production file, interface callback, `unsafe`, `go:linkname`, environment switch
or migration-side seal was added.

The checked-in production `Open` path is unchanged and still rejects before probe or mutation.
The matrix additionally invokes `Open` on each real mount and requires that rejection before it
uses the test-only constructor.

Containers carry a unique ownership label. Normal and signal cleanup refuse to remove a
container with a different label. Loop devices are detached immediately after each successful
mount so the kernel marks them autoclear; the same backing image is explicitly rebound for the
remount check. The temporary cross-compiled test binary is deleted through an exact
`mktemp -d` scope.

## 5. Explicitly open

This record does **not** prove or authorize:

- a non-forgeable external trusted-mount provisioner or production constructor;
- a positive migration-to-production-`evidencefs`/runner/DB path;
- storage-controller reset, VM reset, abrupt host power loss, write-cache behavior, torn
  persistence or crash consistency before a successful publish return;
- an unclean filesystem mount/recovery; the remount is preceded by `sync` and `umount`;
- x86_64 runtime behavior, a second kernel/storage implementation, bare-metal ext4/xfs or cloud
  block storage;
- the complete journal/index/admission/power-loss barrier matrix required by ADR-0010;
- an independent reviewer closure, any P1 Gate, Platform RC, deployment, Beta, GA or release.

The next filesystem-slice work remains: obtain an approved external mount authority, run the
production constructor without a test seam, and execute the fixed ext4/xfs controller/host
power-loss matrix. Until those inputs exist, `evidencefs.Open` must remain fail closed and
P1/A2.1b may not be entered by treating this clean-restart record as filesystem-slice Done.
