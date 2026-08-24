# G-CONTRACT closure-profile v3 assembled pre-consumer independent review

Date: 2026-08-25

## Verdict

`APPROVE`

| Severity | Findings |
| -------- | -------: |
| P0       |        0 |
| P1       |        0 |
| P2       |        0 |

Normalized verdict: `APPROVE_P0_0_P1_0_P2_0`.

This independent fixed-object review approves only ADR-0029 / D-052 Slice F's
assembled canonical closure-profile v3 pre-consumer semantics. It does not
mutate the candidate, consume a review tuple, generate the detached binding
registry, close `G-CONTRACT` or any aggregate Gate, or authorize an external
side effect. Every Gate remains `OPEN` or `IN PROGRESS`.

## Fixed candidate identity

- branch: `codex/cloud-agents-platform-p0`;
- candidate: `1ba7eda5ad6241ad8a065408d787e73cd7013ce0`;
- parent: `5def3ad5deb157264429dc5178f57ec916c66dc7`;
- candidate tree: `5a73a8edd4aee56a38aeb37c37b8009e481dfeae`;
- exact parent-to-candidate full-index binary diff SHA-256:
  `861c5b1a2e434bbdf84618e9f5826a726614e117fd9d7344b2ea0655001b7f61`;
- changed path count: 11; and
- local HEAD and `origin/codex/cloud-agents-platform-p0` both resolved exactly
  to the candidate before review.

The exact 11 changed paths are the successor generation lock, the assembled
generator-supply-v2 profile/evidence manifest, and its nine replay evidence
files. No closure-v3 source, schema, generated registry, runtime source,
detached-consumer source, successor DAG, lock builder, predecessor verifier,
test, plan index, tracker, or Gate record changes in this candidate.

This review path was absent from the candidate and the working tree before the
verdict. It therefore cannot make the reviewed candidate self-reviewing.

## Canonical closure-v3 authority

The candidate retains the reviewed pre-replay authority byte-for-byte:

| Authority                                                                             | SHA-256                                                            |  Bytes |
| ------------------------------------------------------------------------------------- | ------------------------------------------------------------------ | -----: |
| `contracts/platform/v1alpha1/fixtures/golden/contract-closure-profile-source-v3.json` | `face6b9f01732255d4f3ae3aebb040d0af19efae416bad074a2f84510e385862` | 13,451 |
| `contracts/generated/platform/v1alpha1/contract-closure-profile-v3.json`              | `e8384fb25f3828dfafeecf0040110df3a51cd64ce5877e966ecec12769099bf4` | 14,215 |

The source/output, their implementation and tests, the successor DAG, lock
builder, and immutable-predecessor verifier have the same Git blobs in the
candidate and its parent. Current checks reproduce the exact canonical bytes.

The registry preserves the exact ordered seven-criterion inventory. Six
criteria are `SATISFIED_CANDIDATE`; canonical `missing` is derived, not
manually edited, and remains exactly:

```text
remaining-generator-supply-chain-review
```

The assembled supply-v2 profile is
`REPLAY_VERIFIED_REVIEW_PENDING`. Its existence does not rewrite canonical
closure-v3. The supply criterion has no fabricated review, and only the later
detached non-bootstrap consumer may derive an effective candidate view from
complete fixed review tuples.

## Runtime criterion and authority boundary

`runtime-server-path-and-tenant-authority-enforcement` remains the exact
bounded `SATISFIED_CANDIDATE` reviewed on fixed runtime candidate
`b3eda9e7cc97225c1e2256ee27e0c07c8dbd462e`, tree
`2165fd70efd097e7e1decb109cee31e9f6af8ee5`, parent
`9fe7338d3c424731e0b9946f5252e3f61d5326a9`, and binary diff SHA-256
`d4e6e96595d9d1554356e30878ce4d57143efb579d5a369ebf97c085f3f67562`.
Its independent review remains fixed at verdict
`APPROVE_P0_0_P1_0_P2_0` and raw SHA-256
`d75212ba6880f91b33fa52f20011e79af962cdb99cc29a27313685211f204ad2`.

The current candidate's production runtime server, focused server test,
external authn test, PostgreSQL matrix script, `go.mod`, and `go.sum` Git blobs
are identical to the fixed runtime candidate. The reviewed transport-neutral
path still has one authority flow:

```text
route tenant + request metadata + closed body
  -> generated ValidateCreateProjectServerRequest
  -> exact typed mapper
  -> concrete ClaimIdempotency(validated tenant, same VerifiedPrincipal pointer, claim)
```

The server cannot derive tenant authority from the body, principal,
configuration, or provider output. It neither accepts raw subject/issuer
authority nor mints, copies, or reinterprets a verified principal. Subject,
issuer, target-tenant, trust-lineage, membership/RBAC, versioned lineage/quota,
canonical digest, reservation, and append-only transaction rules remain owned
by their existing verified-principal and durable-coordination authorities;
closure-v3 neither weakens nor reclassifies them.

The canonical runtime boundary remains:

- transport: `TRANSPORT_NEUTRAL_CLAIM_ONLY`;
- HTTP, OIDC, JWKS, project writer, provider, and external effects:
  `NOT_IMPLEMENTED`;
- production database writes, deployment, and publication: `NOT_AUTHORIZED`;
- Gate state: `ALL_GATES_OPEN`.

## Pre-consumer and successor-lock state

The detached review-binding source is current while its tuple and output are
both absent. Normal check therefore returns exactly `PRE_REVIEW_ABSENT`; no
effective `missing=[]` view exists. Partial, unknown-field, self-review,
masquerading canonical-profile, stale-output, and dependency-drift states fail
closed in the focused state-machine suite.

The assembled generation lock is
`cloud-agents-platform-contract-generation-lock/v2` with status
`SUCCESSOR_ASSEMBLED_PRE_REVIEW`, `notGateClosure=true`,
`gateStatus=ALL_GATES_OPEN`, and `reviewBinding.state=PRE_REVIEW_ABSENT`. It
binds the unchanged canonical closure-v3 output, the assembled supply-v2
profile, the exact 49-output manifest, and the ordered 16 late-bound paths.

## Focused verification

All executable checks used fixed Bun 1.3.14 and the fixed supplied dependency
tree. A transient root `node_modules` symlink was used only because Bun does
not resolve ESM Ajv from `NODE_PATH`; it was removed by the command trap. No
package installation occurred.

| Check                                                                  |                                                                                   Result |
| ---------------------------------------------------------------------- | ---------------------------------------------------------------------------------------: |
| closure-v3, detached binding, successor DAG, predecessor focused tests |                                                                               54/54 PASS |
| successor-lock-only focused tests                                      |                                                                                 6/6 PASS |
| exact seven-file pre-replay suite against assembled lock               | 91/105 PASS; 14 historical v1 tracked-lock shape assertions not applicable after Slice E |
| closure-v3 source/output current checks                                |                                                                                     PASS |
| binding source/state checks                                            |                                                          `CURRENT` / `PRE_REVIEW_ABSENT` |
| supply-v2 assembly/evidence checks                                     |                                                              `ASSEMBLED_PROFILE_CURRENT` |
| successor generation-lock current check                                |                                                                                     PASS |
| `oxfmt 0.62.0 --check` relevant TypeScript                             |                                                                         10/10 files PASS |
| `oxlint 1.77.0` closure/binding/DAG/predecessor scope                  |                                                                 8/8 files, zero warnings |
| exact candidate `git diff --check`                                     |                                                                                     PASS |

The 14 non-passing exact-seven cases directly parse the tracked lock as the
historical v1 `dialects`/`pipelines`/`tools` shape. ADR-0029 Slice E explicitly
replaces that exact excluded path with the successor v2 lock while freezing
all non-exact16 bytes, so those historical static assertions cannot be changed
inside this candidate. They are recorded, not hidden or called passing. The
six successor-lock state-machine tests and the no-output successor current
check cover the applicable assembled v2 authority.

A narrow TypeScript 5.7.3 probe produced no diagnostic in the closure-v3
source/test, detached-binding production source, successor DAG source/test, or
predecessor test. It reproduced eight inherited diagnostics in unchanged
bytes: two readonly-negative-test assignments in the binding test, one exact-
optional mutation-hook diagnostic in the predecessor source, and five
`unknown` request diagnostics in `platform-json-semantics.ts`. The lock source
also retains three pre-existing lint warnings. Every affected blob is
identical to the approved pre-replay parent, and the exact11 assembled
candidate changes no TypeScript byte; these are not new candidate findings.

The first `NODE_PATH`-only Bun attempt stopped at the Ajv import boundary and
is excluded as a harness-resolution attempt. No broad Bun suite, Go or
migration test, SSH/native replay, writer, database, HTTP/P2/provider,
deployment, publication, release, main merge, or Gate operation was run.

## Review boundary

This verdict permits only the already ordered continuation after Slice F's
separate fixed review records exist. It does not itself create or approve the
future review tuple or detached binding output. Slice G must consume the exact
fixed review bytes through the predeclared non-bootstrap state machine and
must receive its own final Slice H fixed-object review. Canonical closure-v3
remains immutable in its reviewed pre-consumer state, and every Gate remains
open.
