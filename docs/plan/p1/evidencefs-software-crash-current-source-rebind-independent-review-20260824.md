# P1 evidencefs software-crash current-source rebind independent review

Date: 2026-08-24

## Verdict

`REQUEST_CHANGES`

| Severity | Findings |
| -------- | -------: |
| P0       |        0 |
| P1       |        1 |
| P2       |        0 |

The fixed candidate accurately reproduces its lineage, same-bits evidencefs subtree, retained artifact hashes,
sanitized recovery facts, and ADR-0024 physical-power non-claim. It cannot be approved because its high-level status
line declares filesystem `Done`, while the same document and every governing record say reviewer-signed filesystem
`Done` remains open.

This review did not run a crash, `poweroff`, `reboot`, mount, loop setup, filesystem mutation, database operation,
remote deployment, or artifact cleanup. It does not close any Gate.

## Fixed candidate

- candidate branch: `codex/cloud-agents-p1-software-crash-current-rebind-20260824`
- candidate commit: `b61fa31b6ba91dce56a5f713e1cdbef28bb0ce70`
- candidate tree: `00226e4bbd7775a8d1e9bbc9485851221e7a6bd0`
- parent/current source: `98cd0a30d151cdb3c667911a540a3e4006972bbf`
- canonical candidate diff SHA-256: `3bd169a0c7bbe516d4ea9277f5b522d4deb154ce5088e8f448b9df49b7dd0759`
- candidate document raw SHA-256: `5e08fdb8232cfbba23d02c7ae8b7ddf7727a31d9a3c8dad070316f756a1f69df`
- changed scope: exactly one new evidence document

The candidate branch was present at its exact remote commit. The review worktree remained otherwise clean.

## Finding

### P1: Status line falsely declares filesystem Done

The candidate's status line says:

```text
FILESYSTEM DONE AND GATES OPEN
```

That is an affirmative filesystem-`Done` claim. It contradicts:

- the candidate's own remaining-boundary section, which says the rebind does not prove reviewer-signed filesystem
  `Done`;
- ADR-0024, which says the decision and the single software-crash scenario do not close a filesystem slice;
- the original host-crash record, whose status leaves reviewer-signed filesystem `Done` open; and
- the canonical tracker, where current filesystem `Done` remains open.

Because this is the candidate's primary status field, a later evidence consumer can reasonably treat it as a closure
claim despite the lower-page disclaimer. The status must instead state unambiguously that filesystem `Done` is not
claimed or remains open. No other candidate byte needs repair based on this review.

## Reproduced lineage and evidence

The original tested source and current source both resolve
`services/control-plane/internal/evidencefs` to tree
`5e4b0b76a8f1b8c6408b4505a8befa8e5e3369ed`. The original source is an ancestor of the current source, and an exact
path-limited Git diff is empty.

All seven authority/current evidence document hashes and all eight whole-QEMU power-cycle or fixed-barrier document
hashes reproduce the values in the candidate. No authority or QEMU document changed in this rebind.

The executor-local retained set under the fixed temporary evidence directory was read without printing raw contents.
The binary and six logs reproduced these exact SHA-256 values:

| Artifact                       | SHA-256                                                            |
| ------------------------------ | ------------------------------------------------------------------ |
| Linux/amd64 test binary        | `62f81d2ed69c71953b0e7fcc545e9bcca5af8873322d604a188662629b496579` |
| barrier log                    | `4bc2f9e51fac59362bac4b34b52c0901c43769e390c8b64214f605e24100c60a` |
| classifier log                 | `344b647af32f2b405dafa3217dc6c8a7225ce0bc0b4d50fac64fb702c12cf5fe` |
| read-only filesystem-check log | `f6190124984f1d5149072c33aef82ddbdf250fb34db1afc6361b36d516904964` |
| kernel-recovery log            | `9f5068bdf8f1bc53ad7fc2ea7d3e77cc48587946f3ffca06296613f8e339b24f` |
| observer timeline              | `bd98d1371cc35284b43994ec86947ede5cf023df13c678086f8ea135e070102c` |
| observer error log             | `e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855` |

Read-only hashing on the retained DUT reproduced the same binary, barrier, classifier, and filesystem-check hashes.
The retained 2 GiB image reproduced
`c63a5dd3d48dbe215122e8bd9321e849c40a6834ac7659dd7c5237a9004aaaca` and was not loop-mapped at review time.

Whitelisted parsing returned only non-identifying booleans and counts. It confirmed:

- the exact `after-directory-fsync` barrier marker;
- exact `final_count=1`, `final_bytes=44`, zero temporary objects, and `PASS`;
- all five read-only filesystem-check passes;
- kernel recovery completion followed by mount and unmount;
- an observer `UP -> DOWN -> UP` sequence; and
- an empty observer error log.

No raw machine-specific identifier-bearing line was copied into this review.

## Historical identifier-manifest boundary

The original identifier-bearing manifest and two-boot list bytes were not retained in the reviewed local or DUT
artifact sets and therefore were not freshly reproduced. This is **not a separate candidate finding** under the exact
ADR-0024 boundary:

1. the candidate describes both as historical digest bindings and expressly denies fresh reproduction or disclosure;
2. it does not infer a new archive or manifest serialization from the retained logs;
3. ADR-0024 requires a bounded immutable historical execution record plus a current-source phase record and
   independent review for each consuming Gate; it does not require sensitive identifier bytes to be committed or
   replayed during every same-bits rebind; and
4. the independently retained timeline, binary, execution logs, recovery log, and image support the sanitized facts
   actually claimed by this candidate.

This boundary does prohibit later consumers from presenting the historical identifier contents as freshly verified.
Any future claim that depends on the exact raw identifier fields would require separately authorized access and a new
review; this review supplies no such claim.

## Static and safety checks

- candidate/current/original commit, tree, parent, ancestry, and one-file diff identities: PASS;
- candidate document, authority documents, QEMU records, local artifacts, and retained DUT artifacts SHA-256: PASS;
- candidate document `oxfmt 0.62.0 --check`: PASS;
- candidate-range `git diff --check`: PASS;
- redacted Gitleaks `8.30.1` scan: one commit, approximately 8.79 KB, zero findings;
- candidate document literal scan: zero IPv4, MAC, UUID, or device-path literals;
- destructive, power, mount, cleanup, production, HTTP/P2/provider, deployment, publication, and Gate actions: NOT
  RUN.

## Required disposition

Repair only the status line so it explicitly says filesystem `Done` remains open/not claimed, fix a new immutable
candidate commit and raw document hash, and repeat the bounded review. Until then this candidate is not approved and
all filesystem and aggregate Gates remain OPEN.
