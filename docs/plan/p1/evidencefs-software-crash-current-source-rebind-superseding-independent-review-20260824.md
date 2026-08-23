# P1 evidencefs software-crash current-source rebind superseding independent review

Date: 2026-08-24

## Verdict

`APPROVE`

| Severity | Findings |
| -------- | -------: |
| P0       |        0 |
| P1       |        0 |
| P2       |        0 |

The fixed repair candidate changes only the status line rejected by the preceding independent review. It now says
unambiguously that filesystem `Done` and every Gate remain open. The unchanged lineage, same-bits evidencefs subtree,
authority documents, retained artifact hashes, sanitized recovery facts, and ADR-0024 physical-power non-claim were
reproduced without a new finding.

This approval is only an approval of the fixed rebind candidate. It is not filesystem `Done`, a Gate-closing record,
or authorization for deployment, publication, release, production/shared-device mutation, or artifact cleanup.

This review did not run a crash, `poweroff`, `reboot`, mount, loop setup, filesystem mutation, database operation,
remote deployment, or artifact cleanup. All filesystem and aggregate Gates remain OPEN.

## Fixed candidate

- candidate branch: `codex/cloud-agents-p1-software-crash-current-rebind-20260824`
- candidate commit: `682fac82dc74d95fff62663cb3151882bd4492a0`
- candidate tree: `4ca6eff4647e360fac96cd6189c37393b3dd352f`
- parent candidate: `b61fa31b6ba91dce56a5f713e1cdbef28bb0ce70`
- canonical candidate diff SHA-256: `c88d9bc8d4213d0cb739f1149b52471f49c2be4098e9c48731ba27ba60fbfe6a`
- candidate document raw SHA-256: `541933f5063892233744d391f48f916ca172b70d14bcab5a37aa3b27aa6e3793`
- changed scope: exactly one line in the existing rebind evidence document, one insertion and one deletion

The candidate branch was present at its exact remote commit. The review worktree remained otherwise clean.

## Superseded finding and repair

The preceding fixed-candidate review is bound as follows:

- review commit: `475feebc9c2e61eaa9be6f364fd7dfce9fca5bb4`
- review tree: `9c3eaac579eb9d1099f9a85c49779136fc56b30d`
- reviewed candidate: `b61fa31b6ba91dce56a5f713e1cdbef28bb0ce70`
- review document raw SHA-256: `790ed7c69fd8868f142680121c7db9766afec58fbce9aa57a5fd7eb61ade943a`
- verdict: `REQUEST_CHANGES`, P0/P1/P2 = `0/1/0`

Its sole P1 rejected the ambiguous status text `FILESYSTEM DONE AND GATES OPEN`. The repair replaces only that text
with:

```text
FILESYSTEM DONE: OPEN; ALL GATES: OPEN
```

This resolves the finding: the primary status field now agrees with the document's remaining-boundary section,
ADR-0024, the original host-crash record, and the canonical tracker. No lower-page disclaimer is needed to reverse an
apparent closure claim, and no other candidate byte changed.

## Reproduced lineage and evidence

The original tested source and current source both resolve
`services/control-plane/internal/evidencefs` to tree
`5e4b0b76a8f1b8c6408b4505a8befa8e5e3369ed`. The original source remains an ancestor of the current source, and an
exact path-limited Git diff is empty.

All seven authority/current evidence document hashes and all eight whole-QEMU power-cycle or fixed-barrier document
hashes reproduce the values in the candidate. The one-line repair changes none of those records.

The executor-local retained set under the fixed temporary evidence directory was re-read without printing raw
contents. The binary and six logs reproduced these exact SHA-256 values:

| Artifact                       | SHA-256                                                            |
| ------------------------------ | ------------------------------------------------------------------ |
| Linux/amd64 test binary        | `62f81d2ed69c71953b0e7fcc545e9bcca5af8873322d604a188662629b496579` |
| barrier log                    | `4bc2f9e51fac59362bac4b34b52c0901c43769e390c8b64214f605e24100c60a` |
| classifier log                 | `344b647af32f2b405dafa3217dc6c8a7225ce0bc0b4d50fac64fb702c12cf5fe` |
| read-only filesystem-check log | `f6190124984f1d5149072c33aef82ddbdf250fb34db1afc6361b36d516904964` |
| kernel-recovery log            | `9f5068bdf8f1bc53ad7fc2ea7d3e77cc48587946f3ffca06296613f8e339b24f` |
| observer timeline              | `bd98d1371cc35284b43994ec86947ede5cf023df13c678086f8ea135e070102c` |
| observer error log             | `e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855` |

The immediately preceding fixed review independently reproduced the same retained DUT binary/log hashes and final
2 GiB image hash `c63a5dd3d48dbe215122e8bd9321e849c40a6834ac7659dd7c5237a9004aaaca`, and confirmed that the image was not
loop-mapped. The repair changes only candidate prose, so this superseding review binds that read-only verification by
the preceding review commit and document digest above; it does not claim a new destructive execution.

Whitelisted parsing again returned only non-identifying booleans and counts. It confirmed:

- the exact `after-directory-fsync` barrier marker;
- exact `final_count=1`, `final_bytes=44`, zero temporary objects, and `PASS`;
- all five read-only filesystem-check passes;
- kernel recovery completion followed by mount and unmount;
- an observer `UP -> DOWN -> UP` sequence; and
- an empty observer error log.

No raw machine-specific identifier-bearing line was copied into this review.

## Historical identifier-manifest boundary

The original identifier-bearing manifest and two-boot list bytes were not freshly reproduced. This remains consistent
with the approved ADR-0024 boundary because the candidate describes them only as historical digest bindings, does not
infer a new serialization, and expressly denies fresh reproduction or disclosure.

This approval does not allow a later consumer to present those raw contents as freshly verified. Any future claim
that depends on exact raw identifier fields requires separately authorized access and a new review.

## Static and safety checks

- candidate commit, tree, parent, one-line scope, canonical diff digest, document digest, and remote branch: PASS;
- original/current source ancestry, same-bits evidencefs subtree, and empty path-limited diff: PASS;
- authority documents, QEMU records, and executor-local retained artifacts SHA-256: PASS;
- sanitized recovery fact parsing: PASS;
- candidate document `oxfmt 0.62.0 --check`: PASS;
- candidate-range `git diff --check`: PASS;
- redacted Gitleaks `8.30.1` scan: one commit, 117 bytes, zero findings;
- candidate document literal scan: zero IPv4, MAC, or device-path literals;
- destructive, power, mount, cleanup, production, HTTP/P2/provider, deployment, publication, release, and Gate actions:
  NOT RUN.

## Disposition

The fixed candidate is approved for consumption by a later, separately fixed current-source Gate record. This review
does not itself consume the candidate into such a record and does not close filesystem `Done` or any Gate.
