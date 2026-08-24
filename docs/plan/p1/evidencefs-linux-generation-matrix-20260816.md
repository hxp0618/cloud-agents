# P1 evidencefs Linux ext4/xfs generation clean-restart matrix — 2026-08-16

- Status：**IMPLEMENTATION EVIDENCE — PASS；Gate OPEN**
- Fixed source：`180929ad9a5723fbeef3ea36ca21678caf791501`
- Source tree：`15484b20c70d3ffee3fba0655466cae8af524622`
- Branch：`codex/cloud-agents-platform-p1`
- Evidence snapshot：`2026-08-15T19:43:25Z`
- Executor：Codex P1 implementation executor
- Independent Gate reviewer：**not assigned**

## 1. Scope

This record extends the earlier object-only
[`evidencefs-linux-filesystem-matrix-20260816.md`](evidencefs-linux-filesystem-matrix-20260816.md)
on the exact test-only Linux authority boundary. The earlier fixed-source record remains historical;
this record fixes the expanded source and results separately.

For both ext4 and xfs, one fresh loop-backed filesystem run now proves:

1. production `evidencefs.Open` still rejects with `ErrTrustedMountAuthority` before probe or
   mutation;
2. the real Linux backend publishes and binds the fixed object, a second process is blocked on
   the root-wide flock, and `SIGKILL` releases that lock without calling `Lease.Close`;
3. a new process acquires full-root admission for an absent fixed target and performs the exact
   opaque filesystem transition chain:

   ```text
   AcquireAdmission
     → CreateTargetLineage
     → CreateGenerationHeader
     → AppendTargetIndex
     → HandoffGeneration
     → GenerationSnapshot
     → AppendExistingSegmentComposite
     → AppendRotatedSegmentComposite
   ```

4. after handoff, a second process can acquire and release the root lock while its full-root
   `AcquireAdmission` remains blocked by the retained target-lineage lock;
5. after the generation holder is killed without calling `GenerationLease.Close`, the parent and
   a fresh verifier process reacquire full-root admission and verify the exact lineage, journal,
   index and two ordered segment byte strings;
6. after `sync`, clean unmount and remount of the same loop image, another fresh process verifies
   the object and all generation bytes again; and
7. the host-side cleanup check observes neither a task-owned container nor an attached loop
   device.

`evidencefs` treats every index/journal payload in this matrix as opaque. The test exercises the
filesystem durability and lock transitions only; it does not claim that these fixed strings are
C3 migration frames or that migration/verifier/DB authority was constructed.

## 2. Fixed identities and final bytes

| Fact                       | Exact value                                                                           |
| -------------------------- | ------------------------------------------------------------------------------------- |
| Object SHA-256             | `733b503ad96e7f5f8beb7db76873efe54788d02ea7dbb9761e784483611fa707`                    |
| Target SHA-256             | `1c1a470c6c36c66667e5ad51cd726fdcedc81fb020710fa6be85862b7009a042`                    |
| Journal SHA-256            | `23b81a709396fe74d903b2c0c8004e3a50a825687a60addfb546259523822e4e`                    |
| Final `index.caj`          | 278 bytes; SHA-256 `80407baf89f4842c8913472146e1e869d8f4dbb88be0e8b1a7f58d1496ed0e68` |
| Final segment 0            | 114 bytes; SHA-256 `191fde56225d1dd59ab829742d29b2003c4bccef654386267e02ca915e1e4b9d` |
| Final segment 1            | 96 bytes; SHA-256 `9b5d199da51975345561fb4e2e6190feb4946930abddf7b002f979e48e2549c4`  |
| Integration test source    | SHA-256 `bab9b25c469b9312784298dac609df1bd8148f1db7a118a11369c83703dcb5eb`            |
| Matrix script              | SHA-256 `c2052a3d2adecf2e34fcb28d0fa1124536954a00565852f0ff03ee2c2263c2d7`            |
| Cross-compiled test binary | SHA-256 `ca65ec93ef7b598f1c3f6b3a1b67b157d1d11c27a65379c5037b87caf23f6b0e`            |

The final index is the exact concatenation of the lineage header, activation append, existing
segment checkpoint, rotation-header checkpoint and rotation-caller checkpoint. Segment 0 is the
exact generation header followed by the existing-segment caller record. Segment 1 is the exact
rotation header followed by its caller record. The verifier compares full owned bytes, ordered
segment ordinals and cardinality; it does not accept digest-only or prefix-only equality.

## 3. Fixed environment

| Input          | Exact value                                                                                                               |
| -------------- | ------------------------------------------------------------------------------------------------------------------------- |
| Go toolchain   | `go1.26.5 darwin/arm64`; test binary cross-compiled with `GOOS=linux GOARCH=arm64 CGO_ENABLED=0`                          |
| Docker server  | `29.4.0` / OrbStack                                                                                                       |
| Linux kernel   | `7.0.14-orbstack-00380-ga7e0a2dc9535` / `linux/arm64`                                                                     |
| Base image ref | `alpine@sha256:14358309a308569c32bdc37e2e0e9694be33a9d99e68afb0f5ff33cc1f695dce`                                          |
| Local image ID | `sha256:2c15e55df5d63efb31b629a557df305130612a16feb029c93447e54dda2c4189`                                                 |
| ext4 tooling   | `e2fsprogs-1.47.2-r2`; installed-file manifest SHA-256 `04762094b55c2520d2bd6e1791ce3240d881b710e4949887a8d28dd21371cd50` |
| xfs tooling    | `xfsprogs-6.14.0-r0`; installed-file manifest SHA-256 `23d14b59d7f2f13e30e54f5456a4971ec0ea2ebaee4ca96d332b26d93ff3eab9`  |

The harness image is digest-pinned and is never pulled implicitly. `apk add` remains dependent on
the configured Alpine repository/cache rather than a project-pinned package archive. Installed-file
manifests are recorded, but neither package archive is checked in or published as an immutable
project artifact; this remains implementation-environment evidence, not a `G-SUPPLY-CHAIN`
closure.

## 4. Replay command and result

The matrix was rerun after commit and push with local `HEAD`, local branch and remote branch all
fixed to `180929ad9a5723fbeef3ea36ca21678caf791501`:

```sh
CLOUD_AGENTS_EVIDENCEFS_HARNESS_IMAGE='alpine@sha256:14358309a308569c32bdc37e2e0e9694be33a9d99e68afb0f5ff33cc1f695dce' \
  ./services/control-plane/scripts/test-evidencefs-linux-filesystem-matrix.sh
```

Result:

```text
EVIDENCEFS_LINUX_INTEGRATION filesystem=ext4 ... segments=2
EVIDENCEFS_LINUX_REOPEN filesystem=ext4 ... segments=2
EVIDENCEFS_LINUX_MATRIX filesystem=ext4 ... test_binary_sha256=ca65ec93... result=PASS
EVIDENCEFS_LINUX_INTEGRATION filesystem=xfs ... segments=2
EVIDENCEFS_LINUX_REOPEN filesystem=xfs ... segments=2
EVIDENCEFS_LINUX_MATRIX filesystem=xfs ... test_binary_sha256=ca65ec93... result=PASS
Evidencefs Linux ext4/xfs clean-restart matrix: PASS
```

The fixed source also passed:

```sh
GOWORK=off GOTOOLCHAIN=local GOFLAGS=-mod=readonly \
  go test -count=1 ./internal/evidencefs

GOWORK=off GOTOOLCHAIN=local GOFLAGS=-mod=readonly \
  go test -count=1 -race ./internal/evidencefs

GOWORK=off GOTOOLCHAIN=local GOFLAGS=-mod=readonly \
  go test -count=1 ./...

GOWORK=off GOTOOLCHAIN=local GOFLAGS=-mod=readonly \
  go vet ./...

GOWORK=off GOTOOLCHAIN=local GOFLAGS=-mod=readonly \
  go build ./...

GOWORK=off GOTOOLCHAIN=local GOFLAGS=-mod=readonly \
  GOOS=linux GOARCH=arm64 CGO_ENABLED=0 \
  go vet -tags=evidencefsintegration ./internal/evidencefs

GOWORK=off GOTOOLCHAIN=local GOFLAGS=-mod=readonly \
  GOOS=linux GOARCH=arm64 CGO_ENABLED=0 \
  go test -c -tags=evidencefsintegration \
  -o /tmp/evidencefs-integration-arm64.test ./internal/evidencefs

GOWORK=off GOTOOLCHAIN=local GOFLAGS=-mod=readonly \
  GOOS=linux GOARCH=amd64 CGO_ENABLED=0 \
  go test -c -tags=evidencefsintegration \
  -o /tmp/evidencefs-integration-amd64.test ./internal/evidencefs
```

`bash -n` and `git diff --check` also passed. The post-run check returned empty results for both
the ownership-label container query and `losetup -a` inside a fresh privileged harness
container.

## 5. Authority boundary

The positive constructor remains confined to
`backend_linux_integration_test.go`, which is both `_test.go` and guarded by
`linux && evidencefsintegration`. It calls the package-private `newRootWithAuthority` and
package-private `mountAuthority` directly. No exported constructor, build-tagged production
file, callback interface, `unsafe`, `go:linkname`, migration-side seal or environment bypass was
added.

The helper process intentionally holds only the exact generation and target-lineage locks after
handoff. The parent proves that root acquisition succeeds while full-root admission times out;
this distinguishes handoff lock ownership from the earlier root-wide publisher lock check. Both
holders are terminated with `SIGKILL`, so the positive reopen depends on kernel process-lock
release plus durable bytes, not test cleanup methods.

## 6. Explicitly open

This record does **not** prove or authorize:

- a non-forgeable external trusted-mount provisioner or positive production constructor;
- a production migration/runner/DB path, typed C3 frames, verifier receipts, public
  `EvidenceSink`, or `Connect`;
- storage-controller reset, VM reset, abrupt host power loss, torn persistence, volatile write
  cache behavior, or a crash before a durability transition returns success;
- an unclean filesystem mount/recovery; the remount is preceded by `sync` and clean `umount`;
- x86_64 runtime behavior, another kernel/storage implementation, bare-metal ext4/xfs, or cloud
  block storage;
- the remaining response-lost/controller/host power-loss matrix required by ADR-0010;
- an independent reviewer closure, P1 filesystem-slice Done, any P1 Gate, Platform RC,
  deployment, Beta, GA or release.

Production `Open` therefore remains fail closed. This test-only clean-restart result must not be
used to enter P1-A2.1b or to relabel any Gate as `VERIFIED`.
