# G-CONTRACT successor/supply rebind Slice B independent review

Date: 2026-08-25

## Verdict

`APPROVE`

| Severity | Findings |
| -------- | -------: |
| P0       |        0 |
| P1       |        0 |
| P2       |        0 |

The independent reviewer examined the exact fixed candidate from a fresh
detached worktree and removed the worktree, temporary dependency link, caches
and reports after review. The reviewer did not modify the candidate, primary
worktree, generated successor artifacts, a remote host, a database, a
deployment, a release or a Gate record.

This verdict approves only ADR-0029 / D-052 Slice B's complete pre-replay
authority. It does not approve or claim native replay, an assembled
generator-supply-v2 profile, a successor generation lock, detached review
binding, production effects or Gate closure. `ALL_GATES_OPEN` remains in
force.

## Fixed candidate identity

- branch: `codex/cloud-agents-platform-p0`;
- parent: `13dd2a26d8d04478d495c82f5eef1b9230dfde3b`;
- candidate: `a2f4ec986ce8ff5d6e707254ce475673eda9d3ff`;
- candidate tree: `16188339a65a20ab4d6f00b24f1a7f2143cfb1c1`;
- parent-to-candidate binary diff SHA-256:
  `7a89be15d6c14aed4c6b79b9667cab08e4b02d6ae579fa3e76d3a519be64a99d`;
- candidate path count: `50`;
- local candidate and `origin/codex/cloud-agents-platform-p0` both pointed
  exactly to the candidate during review.

The exact 50-path candidate is:

1. `contracts/generated/platform/v1alpha1/contract-closure-profile-v3.json`;
2. `contracts/generated/proto/manifest.json`;
3. `contracts/platform/v1alpha1/fixtures/golden/contract-closure-profile-source-v3.json`;
4. `contracts/platform/v1alpha1/fixtures/manifest.json`;
5. `contracts/platform/v1alpha1/schemas/contract-closure-profile-source-v3.schema.json`;
6. `docs/plan/cloud-agents-platform/06-status-tracker.md`;
7. `docs/plan/p1/README.md`;
8. `docs/plan/p1/g-contract-successor-supply-rebind-slice-b-implementation-20260825.md`;
9. `scripts/check-generator-supply-evidence.ts`;
10. `scripts/check-platform-contract-standards.test.ts`;
11. `scripts/check-platform-contract-standards.ts`;
12. `scripts/generate-platform-contract-closure-profile.ts`;
13. `scripts/generate-platform-contract-lock.ts`;
14. `scripts/generate-platform-contract-review-binding.ts`;
15. `scripts/generate-platform-generator-supply-profile.ts`;
16. `scripts/lib/platform-contract-closure-profile-v3.test.ts`;
17. `scripts/lib/platform-contract-closure-profile-v3.ts`;
18. `scripts/lib/platform-contract-lock.test.ts`;
19. `scripts/lib/platform-contract-lock.ts`;
20. `scripts/lib/platform-contract-review-binding.test.ts`;
21. `scripts/lib/platform-contract-review-binding.ts`;
22. `scripts/lib/platform-contract-standards-profile.test.ts`;
23. `scripts/lib/platform-contract-standards-profile.ts`;
24. `scripts/lib/platform-contracts.test.ts`;
25. `scripts/lib/platform-contracts.ts`;
26. `scripts/lib/platform-generator-supply-profile-v2.test.ts`;
27. `scripts/lib/platform-generator-supply-profile-v2.ts`;
28. `scripts/lib/platform-generator-supply-replay-v2.ts`;
29. `scripts/lib/platform-successor-dag.test.ts`;
30. `scripts/lib/platform-successor-dag.ts`;
31. `scripts/replay-platform-generators-isolated.sh`;
32. `scripts/replay-platform-generators.test.ts`;
33. `scripts/replay-platform-generators.ts`;
34. `sdk/go/gen/common/v1alpha1/identity_generated.go`;
35. `sdk/go/gen/common/v1alpha1/json_generated.go`;
36. `sdk/go/gen/openapi/v1alpha1/client_generated.go`;
37. `sdk/go/gen/platform/v1alpha1/json_generated.go`;
38. `sdk/go/generated-manifest.json`;
39. `sdk/go/json-generated-manifest.json`;
40. `sdk/go/proto-generated-manifest.json`;
41. `sdk/typescript/generated-manifest.json`;
42. `sdk/typescript/json-generated-manifest.json`;
43. `sdk/typescript/proto-generated-manifest.json`;
44. `sdk/typescript/src/index.ts`;
45. `sdk/typescript/src/platform.ts`;
46. `tools/contract-review-binding/v1/source.json`;
47. `tools/contract-standards/check_contract_standards.py`;
48. `tools/contract-standards/profile-v2.json`;
49. `tools/contract-standards/test_contract_standards.py`;
50. `tools/generator-supply/v2/source.json`.

This review record was absent from the candidate and therefore cannot make the
candidate self-reviewing.

## Reviewed closure

The independent review confirmed:

- the approved Slice A candidate/review lineage is a direct ancestor of the
  Slice B candidate, and closure v1/v2 plus generator-supply v1 remain exact
  immutable predecessors;
- closure-v3 source/output are current canonical bytes, retain seven criteria,
  classify six as `SATISFIED_CANDIDATE`, and derive only
  `remaining-generator-supply-chain-review` as canonical `missing`;
- standards-v2 binds the immutable v1 profile, the exact source-contract
  manifest and the 60-schema / 2-manifest / 79-case current corpus;
- generator-supply-v2 is exactly `DECLARED_PRE_REPLAY`, fixed-v1 evidence is
  current, and none of the eight replay receipts or two assembly outputs is
  present;
- binding source is `CURRENT`, normal binding state is
  `PRE_REVIEW_ABSENT`, and no review tuple, registry or effective
  `missing=[]` view exists;
- the core generator output authority contains exactly 49 unique ordered
  paths, and the projection exclusions contain exactly the 16 ordered paths
  fixed by Slice A;
- the legacy `contracts/generation.lock.json` remains byte-identical to its
  parent at 237,214 bytes and SHA-256
  `29cd59f1f69e35a6c0fd312524883b6a90be6fe09616dd21864ed9ce52c96101`;
  the other 15 late-bound paths are absent;
- every generated/declarative surface retains `notGateClosure=true`,
  `ALL_GATES_OPEN`, and the closed production database, HTTP/P2/provider,
  deployment and publication boundaries.

## Preserved lock-review repair

The first working-byte lock review remains preserved as
`REQUEST_CHANGES, P0=0/P1=3/P2=0`. The fixed candidate closes all three
findings:

1. supply current validation returns the candidate manifest and 49-output
   count from the complete replay-receipt semantic snapshot;
2. the live ordered 49-file manifest uses replay's exact
   `path\0bare-sha256\0git-mode\0` algorithm, must equal the receipt-verified
   candidate manifest, and retains file plus parent identities for collective
   terminal rechecks before and after derivation;
3. both legacy and successor modes share one contained generation-lock writer
   using `O_EXCL|O_NOFOLLOW`, file and directory `fsync`, same-directory atomic
   rename, parent-identity recheck, and final/ancestor symlink sentinels.

Partial and ready-to-write binding states remain fail-closed. No successor lock
mode was invoked during the review.

## Fixed-candidate verification

| Check                                           |                                                    Result |
| ----------------------------------------------- | --------------------------------------------------------: |
| 15 named focused Vitest files                   |                           15/15 files, 152/152 tests PASS |
| exact pinned core generator chain               |                                      19/19 `--check` PASS |
| closure-v3 source/output checks                 |                                                      PASS |
| supply-v2 / fixed-v1 evidence checks            |                              `DECLARED_PRE_REPLAY` / PASS |
| detached binding source/state                   |                           `CURRENT` / `PRE_REVIEW_ABSENT` |
| standards official suite                        | 46 files / 383 cases / 1,299 assertions / 79 remotes PASS |
| standards current corpus and Python tests       |                                    60/2/79 and 13/13 PASS |
| `oxfmt 0.62.0 --check`                          |                                          43/43 files PASS |
| JSON / Go / Python / Bash checks                |                                 14 / 4 / 2 / 1 files PASS |
| exact parent-to-candidate `git diff --check`    |                                             50 paths PASS |
| Gitleaks 8.30.1 exact parent-to-candidate range |                       1 commit, 169,813 bytes, 0 findings |

The formal runs used pinned Bun 1.3.14, Go 1.26.6 and Python 3.14.7. A
preliminary environment attempt that relied on `NODE_PATH`, and a preliminary
list that mistakenly included an out-of-scope legacy supply-profile suite,
were excluded from candidate evidence. The exact 152-test list and pinned
19-command chain then passed once each.

The reviewer also confirmed the candidate worktree/index remained clean,
root `node_modules` was absent outside the temporary review link, and no
`.idea`, `migration.test`, late-bound artifact or review output entered the
candidate. No broad Bun suite, broad `go test ./internal/migration`, native
replay, SSH, production database, HTTP/P2/provider, deployment, publication,
release, main merge or Gate operation was run.

## Slice boundary

This fixed-object approval satisfies ADR-0029's prerequisite for leaving Slice
B. Under the accepted ordered Slices A-H, Slice C may now construct and verify
the immutable pre-replay projection from a clean reviewed lineage. It must not
install a replay receipt, run Darwin/Linux native replay, assemble supply-v2,
write the successor lock, create detached binding output, or change any Gate.
