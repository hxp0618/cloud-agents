# P1-A2.3 durable coordination pre-entry blocker - 2026-08-18

- Status: **RESOLVED FOR ORDERED ENTRY - DIRECTION APPROVED 2026-08-19**
- Fixed clean ref: `d05229578a2d9758619d0798b4375cbae3993b4e`
- Branch: `codex/cloud-agents-platform-p1`
- Remote: local HEAD and `origin/codex/cloud-agents-platform-p1` exact
- Affected slice: P1-A2.3 Durable Coordination
- Original audit boundary: before the 2026-08-19 approval this record did not authorize ADR-0013, contract schemas,
  migration `000007`, Go domain/store/service code, public routes, database mutation, Gate closure, deployment,
  release, or entry into P2. Section 6 records the later, narrower authorization.

## 1. Current entry facts

A2.2-impl-3 remediation has completed its fixed-source implementation and independent review. That closes no
aggregate Gate and does not authorize A2.3. ADR-0011 and ADR-0012 both expressly exclude A2.3 coordination.

ADR-0008 requires the SubjectRef identity and every idempotent request projection to be frozen before A2.3
writes SQL. The current contract tree already contains:

| Contract fact                                      | Exact current evidence                                                    |
| -------------------------------------------------- | ------------------------------------------------------------------------- |
| `SubjectRef` schema                                | `766e571265096b6f1a092eb587048bcfc955ddae308db22c9afd08fed5dc931c`        |
| canonical `SubjectRef` fixture                     | `d901f7e34cabe1c9a5052e7dafb8ad4d8d28e6025e5cda54d75519fc0c2cadba`        |
| create-project idempotency projection schema       | `47d1dfa4fe30d327ce31b09eb2d399da58bcc1119667b6df713b5d0c03929d3d`        |
| create-project canonical/digest fixture            | `5a77c5c126f8fd2e76fd7b988a2d4b4110f073fdb0f8ffc3a74d3ce86dbc036b`        |
| excluded-transport-headers same-intent fixture     | `2d0df45c43d21abd06bbdcff641dbb661c793a9963c3c5f7f90c4ceb0b061737`        |
| current contract generation lock                   | `a868f8ac39d21a7c4b968e0864e2baa86977a44aa1d0a8dbe42ebf40131c80fe`        |
| contract source manifest recorded by semantic pass | `sha256:f6240231ac0acce6d7d390a54eb0773d6047e5e6e2ecc98f851cc7caa10426e5` |

The Managed Agent OpenAPI currently contains exactly one mutation carrying `Idempotency-Key`:
`POST /v1/tenants/{tenantId}/projects`, operation ID `managedAgentCreateProject`. The contract checker explicitly
rejects zero, multiple, renamed, or differently routed idempotent mutations in this bootstrap profile. Its
projection excludes all transport headers and binds the path tenant plus strict request body through RFC 8785
and SHA-256.

No A2.3 SQL table, migration, durable-coordination domain model, PostgreSQL store, leader, outbox, operation,
attempt, receipt, or finalizer implementation exists on the fixed ref. The migration head remains append-only
`000006_close_subject_issuer_validation.sql`.

## 2. Current-turn verification

The in-repository semantic checker passed on the fixed ref with:

- 84 JSON files, 32 schemas, two OpenAPI files, three proto files;
- two fixture manifests and 42 fixture cases;
- nine unique operation IDs;
- `AJV_2020_AND_IN_REPO_SEMANTICS_PASS`.

The first ambient `platform:contracts:check` invocation stopped at the generation-lock runtime guard. That shell
exposed Node `26.7.0`, Bun `1.3.14`, and the local Go `1.26.5` binary to the checker, while the repository requires
Node `24.13.1`, Bun `1.3.14`, and Go `1.26.6`. This was a fail-closed toolchain mismatch, not a passing lock check
and not a contract finding.

On 2026-08-19, a read-only follow-up on clean ref `b6edb5f88b05f6c41bb0bc3437d116c4a5550e65`
provided the pinned runtimes only through a temporary `PATH`. The official Node
`node-v24.13.1-darwin-arm64.tar.gz` archive had SHA-256
`8c039d59f2fec6195e4281ad5b0d02b9a940897b4df7b849c6fb48be6787bba6` and passed its downloaded official
`SHASUMS256.txt`; the existing Go toolchain module exposed `go1.26.6` under `GOTOOLCHAIN=local`, and Bun exposed
`1.3.14`. Under that exact tuple, `bun run platform:contracts:check` passed both the semantic checker with the
same counts and manifest above and the byte-exact generation-lock check with `platform-contract-lock: current`.
No repository file changed during the check. This closes only the local pinned-toolchain validation limitation;
it does not close any missing contract suite, Gate, or A2.3 authorization requirement.

The semantic checker still reports the existing P1 contract/Gate gaps for official JSON Schema/OpenAPI suites,
proto descriptor/breaking checks, generated SDK replay, N/N-1 readers, response/watch unknown-field
preservation, runtime server path/tenant enforcement, and remaining generator supply-chain review. A2.3 must not
claim those as complete.

## 3. Decisions still missing

ADR-0008 freezes top-level authority keys, but it does not define enough closed wire/state semantics to implement
A2.3 safely. Before any contract, SQL, or Go implementation, a separately approved decision must close:

1. the versioned operation-profile registry. A raw caller-provided `operation_name` cannot become authority;
   only a contract-registered operation ID/profile/digest may enter idempotency storage;
2. the exact `PlatformOperation`, Attempt, immutable terminal Receipt, and required Finalizer identities, state
   enums, legal transitions, generation rules, timestamps, terminal outcomes, and stable errors;
3. idempotency lifecycle, expiry, same-key/same-digest replay envelope, same-key/different-digest conflict, result
   redaction, deletion/retention, and no-side-effect ordering;
4. the two mutually exclusive outbox event classes already required by ADR-0008, plus exact payload references,
   claim/reclaim/ACK/retry/DLQ states, database-time expiry, bounded retry policy, and full claim-tuple fencing;
5. leader identity, incarnation, expiry, monotonic fencing-token takeover, and the exact writes that require a
   same-transaction token check;
6. minimal audit facts and the prohibition on pairing tokens, credentials, raw auth material, or secret-bearing
   request/result bodies in idempotency, outbox, receipt, audit, log, trace, or backup;
7. slice boundaries, PostgreSQL 15/16/17 isolation/race/fault matrices, source-bound supply refresh, and an
   independent security reviewer.

## 4. Recommended decision package

The narrow forward direction for explicit approval is:

1. split A2.3 into three reviewable slices: contract/state-machine registry; append-only PostgreSQL data kernel;
   typed service/claim/matrix/independent review;
2. accept only generated contract-registry operation profiles. The existing
   `managedAgentCreateProject/v1alpha1` profile is the only current entry, but A2.3 does not expose or execute its
   Managed Agent HTTP route;
3. implement no external side effect in A2.3. Outbox delivery uses a port; A2.3 owns durable enqueue, claim,
   fencing, retry/DLQ state and receipt/finalizer authority only;
4. keep all A2.3 tables tenant-owned with composite tenant foreign keys, forced RLS, database-time expiry, and
   the existing runtime/migration role separation;
5. add migration `000007` only after the contract registry/state machines and pinned generation lock are exact,
   reviewed inputs. Applied migrations remain immutable and any repair is a later forward migration;
6. keep public HTTP mutation, Worker actuation, Session/Turn/Execution, Managed Host Lease, production database
   writes, and every immutable/aggregate Gate outside this authorization.

Approval of this direction would authorize only an ADR and the ordered A2.3 implementation slices above. It
would not authorize deployment, production mutation, A2.4, P2, release, or Gate closure.

## 5. Resume boundary

Before resuming, verify the named worktree and refs:

```sh
cd /Users/huang/devel/project/huang/business/cloud-agents-platform-p1
git status --short --branch
git rev-parse HEAD
git rev-parse '@{u}'
git ls-remote --heads origin codex/cloud-agents-platform-p1
```

After explicit approval, the first writable artifact must be the decision ADR and contract/state-machine slice.
Do not create `000007`, A2.3 tables, or service code before that freeze.

## 6. Approval resolution - 2026-08-19

The owner approved section 4 exactly in this order:

1. contract/state-machine registry;
2. append-only PostgreSQL kernel;
3. service/claim/matrix/independent review.

The approval accepts only generated contract-registry profiles. It does not authorize a public HTTP surface, P2
Session/Turn/Execution or any external side effect, and it closes no Gate. ADR-0013 is the accepted decision record;
slice 1 must be committed and reviewed before migration `000007` or any Go/SQL consumer begins.
