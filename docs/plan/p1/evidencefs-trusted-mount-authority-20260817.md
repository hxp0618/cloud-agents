# P1 evidencefs trusted-mount authority — 2026-08-17

- Status：**IMPLEMENTATION + REAL EXT4/XFS PRODUCTION-OPEN EVIDENCE — PASS；Gates OPEN**
- Fixed implementation commit：`381b04ab64aabc686100b1ec74330ed9ec6b939e`
- Fixed source tree：`f2b20225692e9ac259a28cc2bec34ff23e567dab`
- Branch：`codex/cloud-agents-platform-p1`
- Source path manifest SHA-256：`695a4b135088de7358a541cb2011ad892ec9c5e33e3226651506cef4434eb63d`
- Source hash manifest SHA-256：`bb6181666ec81aa5c4b7cdc64a0e9e9c74bde834516efdae3642956e24605b09`
- Evidence date：2026-08-17 Asia/Shanghai
- Executor：Codex P1 implementation executor
- Independent Gate reviewer：**not assigned**

## 1. Closed scope

This slice replaces the unconditional production `evidencefs.Open` rejection on supported Linux hosts
with a root-provisioned, non-forgeable, current-boot/current-mount capability. It closes only the
trusted-mount constructor and production-open boundary:

1. a root-only CLI provisions or revokes one capability at the fixed protected path
   `/run/cloud-agents/evidencefs-mounts/<sha256(canonical-root)>.authority`;
2. the fixed 224-byte capability body binds a random nonce, canonical root digest, non-root runner UID,
   boot ID, mount namespace device/inode, mount ID, filesystem type, root device/inode/owner/mode,
   device major/minor, direct source digest and mount/superblock option digest;
3. only a dedicated direct-local ext4 or XFS mount whose root is `/` inside that filesystem is admitted;
   bind/rbind mounts, non-device sources, path aliases and unsupported filesystems are rejected;
4. `mountauthority.Load` verifies the root-owned path, directory ancestry, file identity and exact
   current process/mount facts, then mints the only opaque anti-copy `Claim`;
5. production Linux `evidencefs.Open` accepts only a non-root process with identical real/effective/
   saved/filesystem UIDs, zero effective/permitted/inheritable capabilities, an exact loaded claim and
   a successful required-syscall probe;
6. every fresh evidencefs reopen revalidates the same boot, namespace, mount, filesystem, source,
   options and root identity, and poisons the root on drift or close uncertainty;
7. Darwin and other unsupported platforms remain fail closed with `ErrTrustedMountAuthority`.

The capability is not portable across a reboot, mount namespace, remount, root replacement, runner UID
or copied file. `Observation` values are explicitly non-authority. No public authority constructor,
environment bypass, foreign-FD adoption API, migration-side seal or reverse package dependency was added.

## 2. Provision and revoke barriers

Provision requires UID 0 plus the explicit `--confirm-direct-local-mount` flag and never initializes or
mutates the evidence root itself. The authority write is:

`create-exclusive → write exact bytes → fsync(file) → chmod(0444) → fsync(file) → close → rename-noreplace → fsync(directory) → fresh file/directory/root/mount revalidation`.

The mutation-fault suite injects write, both file-sync, chmod, close, rename response-loss and directory
sync failures. It verifies that no uncertain result mints a claim. Revoke derives the basename from the
canonical root, performs exact unlink plus directory `fsync`, and rechecks absence; unlink response loss
is also covered. Cleanup attempts are independent rather than short-circuited.

The static firewall fixes the production call graph:

- `Load` is consumed only by `internal/evidencefs/open_linux.go`;
- `ObserveFD` is consumed only by the Linux evidencefs backend;
- `Provision` and `Revoke` are consumed only by the root-only CLI;
- `newClaim` remains private and is reached only from a successful `Load`.

## 3. Fixed source files

The implementation commit changes exactly 20 files (`2366` insertions, `13` deletions):

| File                                                                                      | SHA-256                                                            |
| ----------------------------------------------------------------------------------------- | ------------------------------------------------------------------ |
| `services/control-plane/cmd/cloud-agents-evidencefs-provision/main.go`                    | `1ad224dd1ac50f3e0e71b04fb8c5c5b80d31b6881593fb006134eba14c37d3b5` |
| `services/control-plane/cmd/cloud-agents-evidencefs-provision/main_test.go`               | `9a85d9695463fe7c03970df9b74b15813d5b54ea9d392f2477adf5609cebed49` |
| `services/control-plane/internal/evidencefs/backend_linux.go`                             | `d33a974087ac7b50a82b18e73e7af3ceb06f6266a112779dd59ccd29c9ee5eac` |
| `services/control-plane/internal/evidencefs/backend_linux_production_integration_test.go` | `ece090904f9bb7442c3e3d28e02c8786ed073549a90782edaaa07800f9139c9f` |
| `services/control-plane/internal/evidencefs/open_linux.go`                                | `a2bfd5884f0dc15df24ac077ae616e35d37795dd8279a870384b1d998e0d62ff` |
| `services/control-plane/internal/evidencefs/open_unsupported.go`                          | `25e929ca3fa8ceb97b303b05fd3e689cab1fe971096ee3683d3982ded746453d` |
| `services/control-plane/internal/evidencefs/probe_test.go`                                | `a06f2aa0137d53b7065c8e07fb09ce6cbf7710307b8a60e17ec4a253ce4ce072` |
| `services/control-plane/internal/evidencefs/root.go`                                      | `cf4053de44c789e21396d0bc0f7e96669c945640b99c22e065226298aeb0d1f8` |
| `services/control-plane/internal/evidencefs/store_test.go`                                | `7c77482db8fbd31ce46eab67774cc312100169ae48a71c6573e691f76e942cea` |
| `services/control-plane/internal/mountauthority/authority.go`                             | `2e7fe2047d2b2b647e0dcda401dea683bef833eabc59345b2858443219a88b20` |
| `services/control-plane/internal/mountauthority/authority_linux.go`                       | `13ee10f40668b35fb6f96b4c9bb1884f074b29961751b0387668e8dcfe65800e` |
| `services/control-plane/internal/mountauthority/authority_linux_integration_test.go`      | `2d782a078cab8e38ec9cc39144706553c7002fa27463c8b83ab2d63273d34bac` |
| `services/control-plane/internal/mountauthority/authority_mutation_linux_test.go`         | `0eab62dd7e0bf6b9d7ef74f2edaafcde9da7d69d6b57218acafdd0cca76bdbf8` |
| `services/control-plane/internal/mountauthority/authority_test.go`                        | `f1b4ce554184cc95e28c4045c81552acd0209f3a67826bef4e1d2bdef8c3b389` |
| `services/control-plane/internal/mountauthority/authority_unsupported.go`                 | `79dedd1c0f49a38d4ec569f1eed4662f9d54a2344179655a92ef5cf92f4c6c35` |
| `services/control-plane/internal/mountauthority/firewall_test.go`                         | `84c39c10bed13bdc4b780535e3df5b29bd03d35a952b0f438f3292df79065bdd` |
| `services/control-plane/internal/mountauthority/observe_linux.go`                         | `f23d280087726e9a5f268f40b75b93f67fd634d82461d6d3615b00e2678e24da` |
| `services/control-plane/internal/mountauthority/observe_linux_test.go`                    | `ca7f98ac4952bd20f22c3a4d228c43253a41360f43c7f31ba18748a2a82f4d57` |
| `services/control-plane/internal/mountauthority/observe_unsupported.go`                   | `06605a64f7cb2ff0b8c6fff4c5587389cef6750cd6124d8aebd01a26f42513b2` |
| `services/control-plane/scripts/test-evidencefs-linux-filesystem-matrix.sh`               | `5d3cb73255ee705e2e03b3e9a108df42d38aa0088b5b4c9090343709940cb15d` |

## 4. Deterministic local and cross-platform gates

The final source was verified with the declared toolchain: Node `24.13.1`, Bun `1.3.14`, and Go
`1.26.6` from
`/Users/huang/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.6.darwin-arm64`.

| Gate                                                                                       | Result                                         |
| ------------------------------------------------------------------------------------------ | ---------------------------------------------- |
| `platform:go:check` across all 3 modules, readonly/tidy-diff and 30-minute tests           | PASS                                           |
| controlled migration suite, `-parallel=1 -timeout=30m`                                     | PASS (`1019.432s`)                             |
| selected Darwin tests and all affected normal/race suites                                  | PASS                                           |
| `go vet`, `go build`, `go mod verify`, module tidy-diff                                    | PASS                                           |
| Linux amd64/arm64 mountauthority/evidencefs/integration test compile and provisioner build | PASS                                           |
| Linux amd64 pure mountauthority/evidencefs binaries executed in Alpine                     | PASS                                           |
| contracts checker                                                                          | PASS — `BOOTSTRAP_VALIDATED`, not Gate closure |
| migration checker                                                                          | PASS — `BOOTSTRAP_VALIDATED`, not Gate closure |
| lint, typecheck, package tests and build                                                   | PASS                                           |
| script tests                                                                               | PASS (`119/119`)                               |
| secret scan, `bash -n`, `git diff --check`                                                 | PASS                                           |

The exact Linux output artifacts were:

| Artifact                         | amd64 SHA-256                                                      | arm64 SHA-256                                                      |
| -------------------------------- | ------------------------------------------------------------------ | ------------------------------------------------------------------ |
| mountauthority test              | `17f881dabd43943a7b5e69fcc14599042bcf29e693125c63b983c84b4cb45979` | `4742966f94436c7693b352cf0af430131cc6ec23252428fef15f0eb169611af9` |
| evidencefs test                  | `c04553d17a4941d76044b89a148c8579af1ab4a73b4736acb9030971b90f3a65` | `ac3404dc760a241b96fac7ace20f67e68ae6b7b9f82e306180ff048d638912dc` |
| production-open integration test | `547fe66a1c6abddd13022b2eff96ba3935b6745a4b4f8e569b6ddfea615cd44e` | `6bea801cdeffa131efa89adbe7a2358bc059452838902b152c86afd94b6fa17e` |
| provisioner                      | `373923dcfe8aefa87fa691b4c71231b11130bc99c98611c725d8e4996789096e` | `860505f00bd7ff4586b4bf18571aae5b505a18c0a755dacfcb7b5977ec10be5e` |

The initial generation-lock check intentionally rejected the ambient Node `26.7.0` / Go `1.26.5`
runtime. Repeating it with the declared paths returned `platform-contract-lock: current`; no generated
contract file changed.

One uncontrolled full migration run was started concurrently with other suites and hit its default
10-minute timeout. The isolated failing-name candidate passed in `4.536s`, then the controlled whole
suite above passed. This record does not relabel the timeout as a pass.

Full `bun run fmt:check` still reports only two unchanged pre-existing documentation files:

- `docs/plan/p1/dependency-reviews/pgx-v5.10.0-x-text-v0.39.0-implemented-closure.md`;
- `docs/plan/p1/dependency-reviews/x-sys-v0.44.0.md`.

No dirty Go or shell file is formatting-applicable to that baseline; all Go sources are `gofmt` clean.

## 5. Real ext4/XFS production-open evidence

The fixed Linux/amd64 integration and provisioner binaries above were copied to
`root@103.217.189.80` and replayed on Linux `6.1.0-15-amd64` (`x86_64`) inside an independent mount
namespace with a private tmpfs `/run`. The run used unique scope
`/tmp/cag-evidencefs-trusted-open-final-20260817-3bb622d`, sparse loop images and a non-root UID 1001
runner started with empty bounding, inheritable and ambient capability sets.

For both filesystems the sequence was:

1. create a fresh sparse image and direct loop device;
2. make and mount ext4 or XFS, establish the closed evidence root grammar, then transfer root ownership
   to UID 1001;
3. provision the exact root-owned 0444/224-byte capability as UID 0;
4. execute `TestLinuxProductionOpenWithProvisionedAuthority` as UID 1001;
5. acquire and close a real root lease through production `evidencefs.Open`;
6. revoke the capability as UID 0;
7. execute `TestProductionOpenFailsClosed` as UID 1001 and require rejection;
8. unmount, detach loop devices and remove the unique temporary scope.

| Filesystem | Observed mount                                                                        | Positive result                    | Revocation result   |
| ---------- | ------------------------------------------------------------------------------------- | ---------------------------------- | ------------------- |
| ext4       | mount ID `1004`, source `/dev/loop0`, direct ext4 root                                | production `Open` PASS as UID 1001 | new `Open` rejected |
| XFS        | mount ID `1004`, source `/dev/loop0`, direct XFS root with observed XFS super-options | production `Open` PASS as UID 1001 | new `Open` rejected |

The final cleanup check found no matching loop device or temporary authority. The XFS utility was
unpacked, not installed, from fixed Debian packages obtained through the already configured Aliyun
mirror. Those temporary packages are not committed outputs or part of this source identity. An initial
XFS setup attempt used the wrong `/usr/sbin/mkfs.xfs` path and failed before any mount or authority
creation; cleanup passed and the fixed `/sbin/mkfs.xfs` replay is the result recorded above.

OrbStack Linux 7.0 returns `ENOSYS` for the required `openat2` operation, so the opt-in positive
integration test correctly skips there; ordinary Linux unit/fault tests still execute. Internal hosts
`192.168.31.107`, `192.168.31.234` and `94.237.78.97` were unreachable during the bounded replay;
`188.239.23.134` was reachable but did not provide `mkfs.xfs`. No changes were made to those hosts.

## 6. Explicit non-claims and next boundary

This record is local implementation and runtime evidence, not an independently signed immutable Gate
record. It does **not** prove:

- physical controller/host power-loss durability or fs-cache persistence;
- migration-owned `AdmissionInventory`/receipt/activation/handoff cross-package construction through a
  production-opened store;
- public `EvidenceSink`, runner/DB `Connect`, database authority, N-1/PITR or provider integration;
- packaging, deployment, Platform RC, Beta, GA or release approval;
- final artifact/supply-chain closure.

The next authorized implementation boundary is the migration adapter and production cross-package
authority chain, followed separately by runner/DB work. `G-DATA`, `G-AUTHORITY-P1`,
`G-SECURITY-P1`, `G-SUPPLY-CHAIN` and all aggregate Gates remain `IN PROGRESS`.
