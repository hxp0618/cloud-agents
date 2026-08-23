# P1 evidencefs bare-metal host-crash simulation — 2026-08-23

- Status: **BOUNDED SOFTWARE-CRASH SCENARIO PASS; PHYSICAL POWER-LOSS AND GATES OPEN**
- Fixed source: `2023f73b14aa57f1ded0c06006de20e6e2294141`
- Source tree: `f460b00faabf5400f4d065628da345bb46f7c962`
- Control-plane subtree: `c1d678f708ec231b446a11e46572a11fccefc97c`
- Scope: ext4 object publication at `after-directory-fsync` under one bare-metal host crash
- Independent reviewer: **not assigned**
- Gate effect: **none**

This record captures one owner-authorized destructive software-crash simulation. It does not claim
physical controller or SSD cache power loss. The storage device remained powered, the evidence
filesystem was a loop-backed ext4 image on an ext4 host filesystem, and only one object-publication
barrier was exercised. A normal `poweroff` was deliberately not used because it would sync and
unmount cleanly; the executor used Magic SysRq `b`, which immediately rebooted the kernel without
sync or unmount.

## 1. Fixed inputs and redacted environment binding

The Linux/amd64 integration test binary was built with Go `1.26.6`, `CGO_ENABLED=0`, and the existing
`evidencefsintegration` build tag. Its SHA-256 is
`62f81d2ed69c71953b0e7fcc545e9bcca5af8873322d604a188662629b496579`.
`-test.list` returned the exact
`TestLinuxIntegrationDurabilityRestartAndCrossProcessLocks` entry before the run.

The first uploaded binary omitted the integration build tag and returned `testing: warning: no tests
to run`; it did not mutate the evidence root and was replaced before the barrier helper started. Its
SHA-256 was `9cb21c0f6fa60778c4b958a6da8f46c38ca9ea0f980efb8547d6b306b8d9a13c` and it is not an execution
input.

The raw execution manifest binds the authorized DUT and observer addresses, host and machine
identities, DMI and root-device identities, pre/post boot identities, target filesystem UUID,
transition timestamps, source identity, test binary, image digest, and raw artifact bundle. Those
identifiers are intentionally omitted from the repository. The canonical manifest SHA-256 is
`c3c8e82f0ab00d7da43c2b9a95f71eb73729240b1022ad9e7f56c1da4387c0d6`.

Sanitized execution facts:

| Fact             | Value                                                                                         |
| ---------------- | --------------------------------------------------------------------------------------------- |
| DUT              | owner-authorized bare-metal x86_64 host, Debian 12, Linux `6.1.0-15-amd64`                    |
| Observer         | separate owner-authorized Linux host with an independent network path to the DUT              |
| Target           | disposable 2 GiB loop image on the DUT's `/home` ext4 filesystem                              |
| Inner filesystem | fresh ext4, mounted `rw,noatime`                                                              |
| Barrier          | `after-directory-fsync` in `publish-crash`                                                    |
| Crash primitive  | root write of `b` to `/proc/sysrq-trigger`; no sync, unmount, service stop, or clean shutdown |
| Fresh classifier | existing `classify-object-crash` helper, same fixed binary and barrier                        |

## 2. Execution and recovery result

Before the crash, the helper printed the exact marker
`EVIDENCEFS_INTEGRATION_CRASH_BARRIER barrier=after-directory-fsync` and remained active. The
observer recorded these UTC state transitions:

| UTC                    | State                       |
| ---------------------- | --------------------------- |
| `2026-08-23T05:23:50Z` | `UP` baseline               |
| `2026-08-23T05:26:43Z` | `DOWN` after SysRq          |
| `2026-08-23T05:28:23Z` | `UP` after automatic reboot |

The DUT's boot identity changed. No second trigger or retry was issued. Before the fresh mount, the
same ext4 filesystem UUID was present and its superblock advertised `needs_recovery`. The fresh mount
therefore performed the normal ext4 journal replay before classification.

The fixed classifier returned:

```text
EVIDENCEFS_INTEGRATION_OBJECT_CRASH_RECOVERY barrier=after-directory-fsync state=final final_count=1 final_bytes=44 temp_count=0 temp_bytes=0
PASS
```

After clean unmount, read-only `e2fsck -fn` returned status `0` and completed all five passes. The
target remained unmounted and no loop mapping remained. The restarted DUT had no failed systemd
unit, its root filesystem was writable, and its pre-existing K3s service returned to `active`.

## 3. Artifact bindings

Raw logs remain outside the repository. The executor-local six-file log bundle SHA-256 is
`c0dc894005258408eae22a2e253a08181e1d546f29f90c55614360e90326e168`.

| Artifact                      | SHA-256                                                            |
| ----------------------------- | ------------------------------------------------------------------ |
| barrier helper log            | `4bc2f9e51fac59362bac4b34b52c0901c43769e390c8b64214f605e24100c60a` |
| fresh classifier log          | `344b647af32f2b405dafa3217dc6c8a7225ce0bc0b4d50fac64fb702c12cf5fe` |
| post-unmount `e2fsck -fn` log | `f6190124984f1d5149072c33aef82ddbdf250fb34db1afc6361b36d516904964` |
| kernel ext4 recovery log      | `9f5068bdf8f1bc53ad7fc2ea7d3e77cc48587946f3ffca06296613f8e339b24f` |
| observer UP/DOWN timeline     | `bd98d1371cc35284b43994ec86947ede5cf023df13c678086f8ea135e070102c` |
| observer error log, empty     | `e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855` |
| final 2 GiB evidence image    | `c63a5dd3d48dbe215122e8bd9321e849c40a6834ac7659dd7c5237a9004aaaca` |
| redacted two-boot list        | `3583091d3c9d41b58b4184f7f90119f69c718ff8bd52dc7fe9f5560893003489` |

The observer process was stopped after the final timeline hash. The loop mapping and mount were
removed. The image and DUT-side logs are retained pending independent review; that retained test
directory is residue and must be explicitly removed after review.

## 4. Verdict and non-claims

The named ext4 `after-directory-fsync` software host-crash scenario is **PASS**. This adds bare-metal
kernel-crash/reboot evidence beyond process kill and whole-QEMU kill, but it does not satisfy the
physical entry conditions in the
[physical controller/host power-loss audit](evidencefs-physical-powerloss-entry-audit-20260822.md).

The following remain **NOT RUN / OPEN**:

- physical power removal, storage-controller reset, SSD volatile-cache loss, external hard-off, or
  independent hard-on recovery;
- a dedicated direct-attached disposable device, XFS, or the complete ADR-0010 barrier matrix;
- production database mutation, live PostgreSQL, deployment, publication, or release;
- independent P0/P1/P2 review of this execution record;
- reviewer-signed filesystem Done, `G-DATA`, `G-AUTHORITY-P1`, `G-SECURITY-P1`, or any aggregate
  Gate closure.

All affected Gates therefore remain `IN PROGRESS`/`OPEN`.
