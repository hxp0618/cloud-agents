# P1 evidencefs software-crash current-source same-bits rebind — 2026-08-24

- Status: **CURRENT-SOURCE SAME-BITS CANDIDATE; INDEPENDENT REVIEW PENDING; FILESYSTEM DONE: OPEN; ALL GATES: OPEN**
- Current source commit/tree: `98cd0a30d151cdb3c667911a540a3e4006972bbf` /
  `fa40473b29c7c4782f5d2811ef6b144c8760d29d`
- Current evidencefs subtree: `5e4b0b76a8f1b8c6408b4505a8befa8e5e3369ed`
- Original execution record commit: `b3e6aec975a83833821bf765bba5ec915399cc4c`
- Original tested source/tree: `2023f73b14aa57f1ded0c06006de20e6e2294141` /
  `f460b00faabf5400f4d065628da345bb46f7c962`
- Scope: same-bits rebinding and retained-artifact verification only
- Independent reviewer: **PENDING**
- Gate effect: **none**

This candidate rebinds the existing owner-authorized bare-metal Magic SysRq `b` execution to the current source and
to the accepted ADR-0024 software-crash durability boundary. It does not run another crash, `poweroff`, `reboot`,
filesystem mutation, database operation, or remote deployment. A clean `poweroff` remains lifecycle smoke only and
is not promoted to abrupt-crash evidence.

## 1. Fixed authority and same-bits lineage

The current source retains the exact evidencefs implementation tree tested by the original run. The following command
returns no diff, and both commits resolve the subtree to
`5e4b0b76a8f1b8c6408b4505a8befa8e5e3369ed`:

```bash
git diff --quiet \
  2023f73b14aa57f1ded0c06006de20e6e2294141..98cd0a30d151cdb3c667911a540a3e4006972bbf \
  -- services/control-plane/internal/evidencefs
```

The current source fixes these unchanged authority/evidence records:

| Input                                                                              | SHA-256                                                            |
| ---------------------------------------------------------------------------------- | ------------------------------------------------------------------ |
| `docs/plan/adr/0024-p1-software-crash-durability-acceptance.md`                    | `597f0d9881aabe44e9d67876ae81d83808a18c3725f5c7ac66279bcda53e0bd0` |
| `docs/plan/p1/software-crash-durability-acceptance-independent-review-20260823.md` | `0b7fb81292e507b9bf15204d44670027c41bf7e83efe07612758217a5c5a712e` |
| `docs/plan/p1/evidencefs-host-crash-simulation-20260823.md`                        | `c4df9602872ac62c303d681782f1f6a35c544e53df3daa2a7fdb557212d283eb` |
| `docs/plan/p1/evidencefs-linux-filesystem-matrix-20260816.md`                      | `3d1b0478f9fe72a47d2783633d4f77c43ba93f57dfae5bb9e4a029e43678d457` |
| `docs/plan/p1/evidencefs-linux-generation-matrix-20260816.md`                      | `fd3e29c3d92590cfb6396cfa66ae5c4203b4fb552a7ab7c40a9eb92bdbed14aa` |
| `docs/plan/p1/evidencefs-required-syscall-probe-20260816.md`                       | `5748f6134ffe5828aa5d6ef76e06a5bcac7fcf5046a3b40dee56c303b5a5688e` |
| `docs/plan/p1/evidencefs-trusted-mount-authority-20260817.md`                      | `fddaea432928ffaee5ca75d3fff8c2a693a7cc6bc8c083eba5f5eb40d642b6e4` |

The eight unchanged whole-QEMU power-cycle and fixed-barrier records are also bound:

| QEMU record                                                        | SHA-256                                                            |
| ------------------------------------------------------------------ | ------------------------------------------------------------------ |
| `evidencefs-qemu-powerloss-matrix-20260816.md`                     | `13b5810f24551b036227eb129eb16b6282227d481d06d3ef386803dbf9067eec` |
| `evidencefs-qemu-object-publish-barrier-matrix-20260816.md`        | `886b4aa0fc3eea67aa05d430948363b1ca95a51219ed1387e2cf02ab436dd6f2` |
| `evidencefs-qemu-target-registration-barrier-matrix-20260816.md`   | `b35078fd72b4a6ce4e1c1691fa6ee8968fe73b140f1ecba29f93b0862e8131fa` |
| `evidencefs-qemu-generation-header-barrier-matrix-20260816.md`     | `ee40b6877ae62321fba0eea10f1a41faa8ca5491616523dbfa37542082038846` |
| `evidencefs-qemu-generation-append-barrier-matrix-20260816.md`     | `1fc27faa9306b772008e478da63ca30741a991d8169eb156ed62fed28338f825` |
| `evidencefs-qemu-generation-activation-barrier-matrix-20260816.md` | `f09592ddbe5056b03cf752b7540b92afd373a139f27c068864b1650db36cfa89` |
| `evidencefs-qemu-generation-repair-barrier-matrix-20260816.md`     | `d261a43cf77ea7e0479f7cc1f6744c7777420141733a24ca583393d6340c6e6d` |
| `evidencefs-qemu-generation-rotation-barrier-matrix-20260816.md`   | `1bda466808baa1a7d64137d38f5eb8fe4bd21f21098e5ead8b50d87a3a0e6b52` |

## 2. Retained artifact verification

The executor re-read the retained six-file raw log set without copying host, device, boot, network, or DMI
identifiers into the repository. Each artifact still matches the immutable execution record:

| Retained artifact                   | SHA-256                                                            | Reproduced fact                                                   |
| ----------------------------------- | ------------------------------------------------------------------ | ----------------------------------------------------------------- |
| Linux/amd64 integration test binary | `62f81d2ed69c71953b0e7fcc545e9bcca5af8873322d604a188662629b496579` | exact originally executed binary                                  |
| barrier helper log                  | `4bc2f9e51fac59362bac4b34b52c0901c43769e390c8b64214f605e24100c60a` | reached `after-directory-fsync` and remained running before crash |
| fresh classifier log                | `344b647af32f2b405dafa3217dc6c8a7225ce0bc0b4d50fac64fb702c12cf5fe` | `final_count=1`, `final_bytes=44`, no temporary object, test PASS |
| post-unmount `e2fsck -fn` log       | `f6190124984f1d5149072c33aef82ddbdf250fb34db1afc6361b36d516904964` | all five passes completed; status 0 in the fixed record           |
| kernel recovery log                 | `9f5068bdf8f1bc53ad7fc2ea7d3e77cc48587946f3ffca06296613f8e339b24f` | ext4 recovery completed, mounted, then unmounted                  |
| independent observer timeline       | `bd98d1371cc35284b43994ec86947ede5cf023df13c678086f8ea135e070102c` | exact `UP -> DOWN -> UP` transition at the fixed UTC times        |
| independent observer error log      | `e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855` | empty                                                             |
| final disposable ext4 image         | `c63a5dd3d48dbe215122e8bd9321e849c40a6834ac7659dd7c5237a9004aaaca` | retained 2 GiB image matches the fixed record                     |

The original record's redacted execution-manifest digest
`c3c8e82f0ab00d7da43c2b9a95f71eb73729240b1022ad9e7f56c1da4387c0d6` remains an immutable historical binding.
Its identifier-bearing bytes are intentionally not added to Git. This candidate independently reproduces the
non-sensitive artifact hashes and execution facts above; it does not claim to reproduce or disclose the redacted
host-identity manifest bytes. The historical canonical six-log bundle digest remains
`c0dc894005258408eae22a2e253a08181e1d546f29f90c55614360e90326e168`; this rebind reproduces each of its six
constituent hashes rather than inferring a new archive serialization. The identifier-bearing two-boot list remains
bound historically as `3583091d3c9d41b58b4184f7f90119f69c718ff8bd52dc7fe9f5560893003489` and is likewise not disclosed or claimed
as freshly reproduced here.

## 3. Acceptance effect and remaining boundary

Under the independently approved ADR-0024 boundary, the retained execution is a current-source candidate for the
required externally observed, no-sync bare-metal abrupt software-crash item. Together with the fixed ext4/XFS
process-restart, trusted-mount and whole-QEMU barrier records, physical power removal, a dedicated test disk, BMC
hard-off, storage-controller reset and volatile hardware-cache loss are not mandatory P1/Platform-RC blockers.
Those physical scenarios remain optional future hardening and are not claimed here.

This rebind still does **not** by itself prove or authorize:

- a clean `poweroff`/`reboot` as crash-consistency evidence;
- a new crash execution, physical power interruption, production/shared-device mutation, or cleanup of retained
  artifacts;
- live PostgreSQL, migration lifecycle, N/N-1 rolling processes, PITR preflight, logical backup/restore, or whole-schema
  tenant isolation;
- reviewer-signed filesystem `Done`, `G-DATA`, `G-AUTHORITY-P1`, `G-SECURITY-P1`, any aggregate Gate, Platform RC,
  deployment, publication, release, or main-branch merge.

An independent fixed-candidate P0/P1/P2 review must reproduce the Git identities, same-bits subtree, all listed
document and retained-artifact hashes, sanitized recovery facts, ADR-0024 semantics, and non-claims before this
candidate can be consumed by a later current-source Gate record.
