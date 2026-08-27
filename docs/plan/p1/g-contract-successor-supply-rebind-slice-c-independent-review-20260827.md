# ADR-0029 / D-052 Slice C independent read-only review — 2026-08-27

## Verdict

**APPROVE — P0=0, P1=0, P2=0.**

This is an independent, read-only review of fixed candidate
`a63533967e844c992d51eb5e85ab968f6ab5a998`. No candidate bytes, generated
outputs, lock, database, network, deployment, publication, release, or Gate
was changed. Projection archive and metadata were external temporary outputs,
not replay receipts or Gate-closing evidence.

## Candidate and lineage binding

| item                          | value                                                              |
| ----------------------------- | ------------------------------------------------------------------ |
| candidate commit              | `a63533967e844c992d51eb5e85ab968f6ab5a998`                         |
| direct parent                 | `532fd21a7415764a82d5ff350a7b2fa6a4dc3de4`                         |
| candidate tree                | `e0a034da5719c298c499cb572e4b40d1695d1400`                         |
| parent tree                   | `b215f8cc462bbf1dbcd055080bd4ff4bd19ea3b9`                         |
| candidate binary diff SHA-256 | `178b77ac463fa14e33e0594a78e074a3bc233aa53acbd9a77c999d23aa112d1b` |
| candidate name/status SHA-256 | `2f496437eab7fd9e83db7ef4b1972124a46b5d1f6cdf6f96a29ab8f1fc61e7b1` |

The candidate is a direct child of P0. Its only changed paths are one Slice C
record and two plan/status index updates. Slice A/B, r1/r2, v1 predecessor,
generated source/profile/schema/manifest, SQL/catalog/archive, and review
bytes are byte-identical to the parent. The review checkout was clean before
adding this record.

## Projection reconstruction

The versioned wrapper was run twice from fresh canonical external leaves:

```text
scripts/replay-platform-generators-isolated.sh build-projection \
  /private/tmp/review-d052-slice-c.pntEsa \
  /private/tmp/d052-projection-review.jTCtMt/proj1
scripts/replay-platform-generators-isolated.sh build-projection \
  /private/tmp/review-d052-slice-c.pntEsa \
  /private/tmp/d052-projection-review.jTCtMt/proj2
```

Both `projection.json` and `core-generator-input-projection.tar` were
byte-identical:

| projection fact         | value                                                                                                                                           |
| ----------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------- |
| projection tree         | `a1ddde6d955116342a39336dc45268dfd46e10e1`                                                                                                      |
| archive SHA-256         | `3a9e552659060c331aef7c4637bbb1808c462d541d879bed28cc424e884d7904`                                                                              |
| archive size            | `50012160` bytes                                                                                                                                |
| metadata SHA-256        | `592cc17f8b1ca0c24089b262bffcf950577055caa33c5d634ac6a2849e11be1d`                                                                              |
| entries / regular files | `1770 / 1564`                                                                                                                                   |
| member manifest         | `utf8-bytewise-sorted-path-type-mode-size-sha256-linktarget-nul-v1`, SHA-256 `cad4f9c605d32a57e12ba1465139ceb7fa18adcf2a74651db4a4b9848785dbba` |
| regular-file manifest   | `utf8-bytewise-sorted-path-mode-size-sha256-nul-v1`, SHA-256 `74e4911836d27b181db9fb693d045d5f7ff25bef69bd05f90beb855ca6426831`                 |

Independent `inspect-generator-replay-archive.py core-projection` output
matched the wrapper inspection and reconstructed the exact projection tree.
The temporary index accepted only Git modes `100644` and `100755`; archive
members were regular files without symlinks or special modes.

The exclusion authority has exactly 16 entries, in ADR-0029 order, with
JSON-array SHA-256
`a0d4a4980559b906c8f59ce1a8590886c5cd39f4e21e52c0312914257037d994`:

```text
contracts/generation.lock.json
tools/generator-supply/v2/evidence-manifest.json
tools/generator-supply/v2/profile.json
tools/generator-supply/v2/evidence/replay.json
tools/generator-supply/v2/evidence/replay/darwin-a.json
tools/generator-supply/v2/evidence/replay/darwin-b.json
tools/generator-supply/v2/evidence/replay/darwin-isolation.json
tools/generator-supply/v2/evidence/replay/linux-a.json
tools/generator-supply/v2/evidence/replay/linux-b.json
tools/generator-supply/v2/evidence/replay/linux-isolation.json
tools/generator-supply/v2/evidence/replay/projection.json
docs/plan/p1/g-contract-closure-profile-v3-independent-review-20260824.md
docs/plan/p1/g-contract-generator-supply-profile-v2-independent-review-20260824.md
tools/contract-review-binding/v1/review-tuple.json
tools/contract-review-binding/v1/registry.json
docs/plan/p1/g-contract-detached-review-binding-independent-review-20260824.md
```

## Focused checks

Passed:

- `bunx vitest run scripts/replay-platform-generators.test.ts --reporter=dot` — 13/13.
- `bunx vitest run scripts/lib/platform-successor-dag.test.ts scripts/lib/platform-generator-supply-replay-v2.test.ts scripts/lib/platform-generator-supply-profile-v2.test.ts scripts/lib/platform-contract-closure-profile.test.ts scripts/lib/generator-replay-path-authority.test.ts --reporter=dot` — 66/66.
- `git diff --check 532fd21a7415764a82d5ff350a7b2fa6a4dc3de4 a63533967e844c992d51eb5e85ab968f6ab5a998` — clean.
- Changed-document format/mode inspection and candidate-range secret-shaped scan — no finding (values not emitted).

`platform-contract-lock.test.ts` reports 16 failures on both this candidate
and the parent P0 checkout, before any Slice C change: one historical
standards-cardinality expectation and 15 reads of legacy lock `dialects`,
`pipelines`, or `tools` fields absent from the current lock shape. This is an
inherited stale-lock mismatch, not a candidate regression or Slice C finding.

## Scope boundary

Approval covers only deterministic Slice C projection write/check and
archive/tree reconstruction. Slice D native replay, Slice E assembly/lock,
detached binding, production Runner/database writes, and all external effects
remain pending or unauthorized. No Gate is closed or reclassified.
