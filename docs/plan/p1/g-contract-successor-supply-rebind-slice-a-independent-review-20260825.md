# G-CONTRACT successor/supply rebind Slice A independent review

Date: 2026-08-25

## Verdict

`APPROVE`

| Severity | Findings |
| -------- | -------: |
| P0       |        0 |
| P1       |        0 |
| P2       |        0 |

The independent reviewer examined the exact fixed candidate in a fresh
detached clone and removed that clone after review. The reviewer did not modify
the candidate, the primary worktree, any generated successor artifact, a
remote host, a database, a deployment, a release, or a Gate record.

This verdict approves only ADR-0029 / D-052 Slice A's frozen predecessor,
successor-contract, exact-DAG, and dormant detached-consumer implementation.
It does not generate closure-v3 or generator-supply-v2 authority, does not
create replay or review artifacts, does not authorize an external effect, and
does not close any Gate. `ALL_GATES_OPEN` remains in force.

## Fixed candidate identity

- branch: `codex/cloud-agents-platform-p0`;
- baseline: `980563719664d92af80dd2930db020380011930a`;
- candidate: `d7f7a180c7621907cdaf2fa2b35b7777209695a1`;
- candidate tree: `15c6af6a055d9fe69f7ba9dbb73a5685174f202b`;
- unique parent: `c9f9e6a6b009557f8258a79a2f704a13330724b2`;
- baseline-to-candidate binary diff SHA-256:
  `781e1115353aabcb3aab58d833c5a266150211bbef96dc0a9fd4fd420f9bd47c`;
- candidate path count: `23`;
- local HEAD and `origin/codex/cloud-agents-platform-p0` both pointed exactly
  to the candidate during review.

The exact 23-path candidate is:

1. `contracts/platform/v1alpha1/schemas/contract-closure-profile-source-v3.schema.json`;
2. `contracts/platform/v1alpha1/schemas/contract-closure-profile-v3.schema.json`;
3. `docs/plan/cloud-agents-platform/06-status-tracker.md`;
4. `docs/plan/p1/README.md`;
5. `docs/plan/p1/g-contract-successor-supply-rebind-slice-a-implementation-20260824.md`;
6. `scripts/generate-platform-contract-review-binding.ts`;
7. `scripts/lib/platform-contract-closure-profile-v3.test.ts`;
8. `scripts/lib/platform-contract-closure-profile-v3.ts`;
9. `scripts/lib/platform-contract-review-binding.test.ts`;
10. `scripts/lib/platform-contract-review-binding.ts`;
11. `scripts/lib/platform-generator-supply-profile-v2.test.ts`;
12. `scripts/lib/platform-generator-supply-profile-v2.ts`;
13. `scripts/lib/platform-generator-supply-replay-v2.test.ts`;
14. `scripts/lib/platform-generator-supply-replay-v2.ts`;
15. `scripts/lib/platform-successor-dag.test.ts`;
16. `scripts/lib/platform-successor-dag.ts`;
17. `scripts/lib/platform-successor-predecessor.test.ts`;
18. `scripts/lib/platform-successor-predecessor.ts`;
19. `tools/contract-review-binding/v1/review-binding-registry-v1.schema.json`;
20. `tools/contract-review-binding/v1/review-binding-source-v1.schema.json`;
21. `tools/contract-review-binding/v1/review-tuple-v1.schema.json`;
22. `tools/generator-supply/v2/generator-supply-profile-source-v2.schema.json`;
23. `tools/generator-supply/v2/generator-supply-profile-v2.schema.json`.

This review record was absent from the candidate and therefore cannot make the
candidate self-reviewing.

## Preserved rejected candidates

No rejected candidate was amended, rebased, squashed, force-pushed, or removed.

| Candidate                                  | Tree                                       | Parent                                     | Paths | Binary diff SHA-256                                                | Verdict                          |
| ------------------------------------------ | ------------------------------------------ | ------------------------------------------ | ----: | ------------------------------------------------------------------ | -------------------------------- |
| `150446570076512c1280298b61ff1779a98f34a3` | `aec476352dfe07953559881cbd95d3d2183d2365` | `980563719664d92af80dd2930db020380011930a` |    21 | `e7ba7c418d1e2077f1703a51b4ceba815ab694dc3566f071665c2f9c82315d3b` | `REQUEST_CHANGES P0=0/P1=2/P2=0` |
| `ea4b4a40ca1df66145c88b92df4da2410042385d` | `aa4b88567474a31c59024a374c2f0aeecf6d0e48` | `150446570076512c1280298b61ff1779a98f34a3` |    23 | `8a191200368812f67b33ba73ecd6045c047ea6e7a779a9593c25a2465abcfe83` | `REQUEST_CHANGES P0=0/P1=1/P2=0` |
| `c9f9e6a6b009557f8258a79a2f704a13330724b2` | `b48eda0986bdd58aee48a140362a7b5e5fab1085` | `ea4b4a40ca1df66145c88b92df4da2410042385d` |    23 | `da39f54d2cfdb81dc2971c69cddc845e3f46865ed4f276e94cac32e47c74c481` | `REQUEST_CHANGES P0=0/P1=2/P2=0` |

The first candidate lacked the generator-supply-v1 closed review pair and
allowed arbitrary digest-bound receipt promotion. The second closed those
relationships but allowed detached authority semantics and file SHA to come
from different ancestor-directory snapshots. The third closed the detached
mixed read but left closure-v2 fence/derived-read and complete-v1-fence/derived-
snapshot ABA windows. The approved candidate is an additive child of all three
and preserves their exact evidence identities.

## Reviewed closure

The independent review confirmed:

- contract-closure v1/v2 and generator-supply v1 are immutable predecessor
  authority, including all 39 evidence-manifest members and the unique
  candidate/review direct-child closed pair;
- closure-v3's seven criteria and derived `missing` set remain strict,
  canonical, non-self-referential, and candidate-only;
- every closure-v2 registry-derived read validates the exact fixed output path,
  size, and SHA-256 from the same stable-read bytes before parsing;
- generator-supply-v2 stable-reads and fixes the v1 evidence manifest itself,
  takes source authority from the immutable outer map, and validates path,
  size, and SHA-256 for all seven derived inputs before parsing;
- the supply semantic currentness set contains four replay authorities, the
  fixed manifest plus seven v1 derived inputs, and eight exact receipts: 20
  input identities in total, in addition to the complete-v1 and v2-outer
  current wrappers;
- the exact-eight receipt graph closes run, isolation, projection, summary,
  same-bits, cross-platform, rootfs, and safety relationships;
- closure and supply registry semantics and output-file SHA come from the same
  read, and the detached binder does not reopen either authority;
- the candidate Git blob's embedded profile and registry digests must equal the
  detached review tuple;
- the exact ordered 16-path late-bound DAG, atomic no-overwrite detached
  binding state machine, and non-bootstrap separation remain fail-closed.

Both new parent-directory ABA restore tests passed. A directory B containing
different bytes cannot cross either derived boundary; a B byte set equal to A
has no semantic difference.

## Fixed-candidate verification

The reviewer accepted only the bounded checks owned by Slice A:

| Check                                          |                              Result |
| ---------------------------------------------- | ----------------------------------: |
| immutable predecessor verifier                 |                          15/15 PASS |
| exact successor DAG                            |                            6/6 PASS |
| replay receipt semantics v2                    |                          11/11 PASS |
| generator-supply v2                            |                          12/12 PASS |
| contract-closure v3                            |                            9/9 PASS |
| detached review binding                        |                          16/16 PASS |
| total focused Vitest                           |                          69/69 PASS |
| 23-path `oxfmt 0.62.0 --check`                 |                                PASS |
| seven JSON schemas with `jq empty`             |                                PASS |
| exact baseline-to-candidate `git diff --check` |                                PASS |
| Gitleaks 8.30.1 exact four-commit range        | 0 findings, about 376.48 KB scanned |

The reviewer also confirmed all 18 expected Slice A
source/output/replay/review/tuple artifacts remained absent, the primary
worktree was clean, and the temporary root `node_modules` was absent. No broad
Bun suite, broad Go migration suite, native replay, production database,
HTTP/P2/provider, deployment, publication, release, main merge, or Gate command
was run.

## Slice boundary

This fixed-object approval satisfies ADR-0029's prerequisite for leaving Slice
A. Under the already accepted ordered Slices A-H and continuing execution
authority, Slice B may now begin as a new additive pre-replay implementation.
That progression remains limited to versioned generated contract authority and
focused local evidence. It does not broaden the external-effect boundary, and
every Gate remains open.
