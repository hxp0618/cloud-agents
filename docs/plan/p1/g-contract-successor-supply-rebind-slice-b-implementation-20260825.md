# G-CONTRACT successor/supply rebind Slice B implementation — 2026-08-25

## Boundary

This record implements only ADR-0029 / D-052 Slice B on the reviewed Slice A
lineage. Slice A candidate
`d7f7a180c7621907cdaf2fa2b35b7777209695a1` was independently approved by
review-record commit `13dd2a26d8d04478d495c82f5eef1b9230dfde3b` with
`APPROVE, P0=0/P1=0/P2=0` before these bytes were created.

Slice B materializes the complete pre-replay authority: canonical closure-v3
source/output, contract-standards v2, generator-supply-v2 source authority,
the dormant detached-consumer source, the expanded core generated output set,
the versioned replay authority, and a successor generation-lock derivation
that remains dormant until assembly.

This slice does not create any native replay receipt, supply-v2 evidence
manifest/profile, independent review record, detached review tuple/output, or
final review. The tracked legacy `contracts/generation.lock.json` is an exact
projection exclusion and remains byte-for-byte unchanged; the other 15
late-bound paths remain absent. No native replay, production database write,
HTTP/P2/provider effect, deployment, publication, release, main merge, or Gate
transition is part of Slice B.

## Canonical closure v3

The generated closure-v3 pair is now complete and deterministic:

| Authority                                                                             | SHA-256                                                            |  Bytes |
| ------------------------------------------------------------------------------------- | ------------------------------------------------------------------ | -----: |
| `contracts/platform/v1alpha1/fixtures/golden/contract-closure-profile-source-v3.json` | `face6b9f01732255d4f3ae3aebb040d0af19efae416bad074a2f84510e385862` | 13,451 |
| `contracts/generated/platform/v1alpha1/contract-closure-profile-v3.json`              | `e8384fb25f3828dfafeecf0040110df3a51cd64ce5877e966ecec12769099bf4` | 14,215 |

The v3 registry preserves closure v1/v2 as immutable predecessors, carries all
seven criteria with their reviewed/runtime boundaries, and derives exactly one
canonical `missing` item:
`remaining-generator-supply-chain-review`. It does not promote that pending
item or reinterpret bounded implementation evidence as a Gate signature.

Source generation is an explicit pre-replay action. Normal replay `--write`
only writes the generated v3 output and cannot rewrite source authority. Both
source and output current checks require exact dedicated canonical serializer
bytes, not merely JSON semantic equality. Their writers require repository-
contained regular topology and use no-follow publication so a final symlink
cannot redirect output outside the repository.

The canonical contract tree now contains 60 JSON schemas, two fixture
manifests and 79 fixture cases. Its manifest digest is
`sha256:97ccd739db755b1fbfaf9166f87c4cd985980d6ec78a1b172bbd65638006413c`.
Bootstrap consumes the complete current v3 registry but still reports
`notGateClosure=true`.

## Contract standards v2

`tools/contract-standards/profile-v2.json` is a versioned generated profile
with SHA-256
`9457d4bdc12f16b366d9c56a25a107103f5b2b64650de20f509f3ef96d0d4d01`
and size 3,539 bytes. It binds the immutable v1 profile, the current 60/2/79
contract corpus and the exact source-contract manifest above.

The TypeScript and Python validators explicitly select v2 while retaining v1
validation. Fixed Bun 1.3.14, Python 3.14.7 and uv 0.12.5 validation covers the
official JSON Schema corpus at 46 files / 383 cases / 1,299 assertions / 79
remotes, the current 60/2/79 fixtures, and two OpenAPI 3.1 documents with nine
operations. This is independent non-Gate standards evidence; it does not
change any Gate state.

## Generator supply and exact replay closure

`tools/generator-supply/v2/source.json` is the pre-replay authority with
SHA-256
`31d33cfde3f24df0e2b18b70e445232399c75f3a283df8fc1fc6bd945db99305`
and size 8,280 bytes. Its only current state is `DECLARED_PRE_REPLAY`; presence
of a subset of late artifacts fails closed and no assembly writer is exposed
in Slice B.

The replay closure now contains exactly 49 unique UTF-8-bytewise-sorted core
output paths and the exact ordered 16 projection exclusions from Slice A. The
19 core generators were run in their mandatory order and then checked without
output under the pinned supply toolchain. The checked-in 49-file candidate
manifest, using replay's
`utf8-bytewise-sorted-path-nul-sha256-nul-git-mode-v1` algorithm, is
`sha256:d0136124f1f760ae60c34e3b0e47161cb528fa3222f3330a440338f6a47da50e`.

The versioned replay authorities are fixed as:

| Authority                                        | SHA-256                                                            |  Bytes |
| ------------------------------------------------ | ------------------------------------------------------------------ | -----: |
| `scripts/replay-platform-generators-isolated.sh` | `b4c0f23c45c2a3a1a391daadcc44554793fda948168f35f3ffaf4d32cedd9070` | 83,800 |
| `scripts/replay-platform-generators.ts`          | `96bc41cd702a35b0c4febfd62c48e0e261fc0656f6f91583522eb47e96cf07a1` | 41,526 |

The wrapper and TypeScript authority agree on all 49 outputs and all 16
exclusions. Closure-v3 source/output and every core SDK/manifest fanout are
inside the projection; only declared late evidence is excluded. Slice B does
not invoke the Darwin/Linux A/B replay modes.

## Dormant detached consumer

`tools/contract-review-binding/v1/source.json` has SHA-256
`99b688b39c2819a02e6d974675c72317285a7545b073bd1cb071a081fcff2f45`
and size 1,900 bytes. Its source writer/check is separate from normal binding
write/check. With tuple and registry absent, both normal operations remain a
strict `PRE_REVIEW_ABSENT` no-op. No Slice B command can fabricate a review
tuple, effective `missing=[]` view, or self-reviewing registry.

## Versioned successor lock derivation

The lock module adds a pure
`cloud-agents-platform-contract-generation-lock/v2` builder and a production
post-assembly builder/checker. It binds standards-v2, closure-v3, assembled
supply-v2, the exact 49 local output records, replay's candidate manifest, the
ordered 16 exclusions, and either the exact `PRE_REVIEW_ABSENT` binding state
or a complete current tuple/registry pair. Partial and ready-to-write binding
states fail closed. Every document retains `notGateClosure=true` and
`ALL_GATES_OPEN`.

The first independent working-byte review rejected the initial lock derivation
with `REQUEST_CHANGES, P0=0/P1=3/P2=0`: it checked only 49-path topology rather
than equality with replay's candidate manifest, discarded the collective
49-file snapshot identities before the terminal fence, and used a writer that
could follow a final symlink. The additive repair binds the validated
supply-v2 receipt manifest to the live 49-file manifest, terminally rechecks
the complete output snapshot, and confines both legacy and successor lock
writers to the one contained generation-lock path with no-follow semantics.
Adversarial mutation and external-sentinel tests cover those three rejected
sequences.

The successor modes are deliberately dormant because assembly is absent. They
were neither written nor checked in Slice B. The historical lock SHA-256
remains
`29cd59f1f69e35a6c0fd312524883b6a90be6fe09616dd21864ed9ce52c96101`
at 237,214 bytes. All lock writers are scoped to that single path and no
identity/core generator side write remains.

## Focused verification

Verification is bounded to Slice B owners and the exact core generator chain:

| Check                                         |                                    Result |
| --------------------------------------------- | ----------------------------------------: |
| 15 named Vitest files                         |                              152/152 PASS |
| exact 19 core generator `--check` chain       |                                      PASS |
| closure-v3 source/output current checks       |                                      PASS |
| supply-v2 source and fixed-v1 evidence checks |              `DECLARED_PRE_REPLAY` / PASS |
| detached binding source/current check         |           `CURRENT` / `PRE_REVIEW_ABSENT` |
| contract bootstrap                            | 60 schemas / 2 manifests / 79 cases, PASS |
| fixed-toolchain standards                     |          46/383/1299/79 and 60/2/79, PASS |
| Python standards unittest                     |                                13/13 PASS |
| 43-file `oxfmt 0.62.0 --check`                |                                      PASS |
| 14 changed JSON files with `jq empty`         |                                      PASS |
| changed Go/Python/Bash format and syntax      |                                      PASS |
| exact 50-path staged `git diff --check`       |                                      PASS |
| Gitleaks 8.30.1 staged scan, about 170 KB     |                                0 findings |

The current implementation contains exactly 50 changed paths. These bounded
checks are repeated against the fixed Slice B candidate by its independent
review. No broad Bun suite, broad `go test ./internal/migration`, native replay,
remote execution or external-effect test is claimed here.

## Review and next-slice status

The closure/bootstrap and replay/supply/binding module reviews returned
`APPROVE, P0=0/P1=0/P2=0` after their scoped serializer/timeout repairs. The
successor-lock first review remains preserved as the three-P1 rejection above.
Its additive repair received a fresh working-byte
`APPROVE, P0=0/P1=0/P2=0` after 40/40 focused lock/supply tests. This is not a
substitute for the required separate fixed-object review of all 50 paths.

Slice C may begin only after this exact Slice B byte set is committed, pushed
and independently returns `APPROVE, P0=0/P1=0/P2=0`. Every Gate remains OPEN
or `IN PROGRESS`; no production database, HTTP/P2/provider, deployment,
publication, release, main merge or Gate authority is granted by this record.
