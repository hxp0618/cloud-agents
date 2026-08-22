# P1 aggregate Gate gap audit — 2026-08-22

- Status: **READ-ONLY ENTRY AUDIT COMPLETE; P1 REMAINS IN PROGRESS; NO GATE CLOSED**
- Audited source: `1416fb34c03a3ada25dd58728046aa753b6e03ed`
- Source tree: `47d2c467c1b1057eab60ea2109f70cf77bdc7554`
- Control-plane subtree: `44174a871ddf2da859f901050634f9f1995f0aa6`
- Branch: `codex/cloud-agents-p1-gate-gap-audit-20260822`
- Scope: P1 Exit Gate evidence inventory and exact next-entry blockers only
- Independent reviewer: **pending fixed-candidate read-only review**
- Gate effect: **none**

This audit answers one narrow question: after the reusable migration shard runner received an
independent `APPROVE`, what evidence is still required before P1 can honestly change from
`IN PROGRESS` to `VERIFIED`? It does not reinterpret implementation records as Gate signatures and
does not run a broad migration suite, PostgreSQL matrix, physical power action, generator, deploy,
or production database operation.

## 1. Fixed authorities and reviewed inputs

The P1 Exit Gate set is exactly `G-CONTRACT`, `G-DATA`, `G-AUTHORITY-P1`, and
`G-SECURITY-P1`. Progressive Gate rules require a fixed immutable phase record with source, dirty
state, toolchains, artifact digests, commands, results, DRI, independent reviewer, and invalidation
rules; implementation or matrix records cannot be promoted implicitly.

| Audited authority/evidence                                                | SHA-256                                                            |
| ------------------------------------------------------------------------- | ------------------------------------------------------------------ |
| `cloud-agents-platform/05-gates-and-acceptance.md`                        | `4a3d0b3c184e9673944411adbc5c8ea933883c855d5aada67862dad8e4dcc994` |
| `cloud-agents-platform/06-status-tracker.md`                              | `aef319cebba5e946ffa077ec46e60a9306286c57fe594d256f79fd5da588b175` |
| `adr/0023-p1-runner-ledger-recovery-writer-contract.md`                   | `b1c0735eb9f00bcfaeb7a14bb6c7bfd4fec1bbfe6dba1c6faa5854caa548fe13` |
| `runner-ledger-recovery-contract-audit-independent-review-r2-20260822.md` | `e8192b5ae4525ed11717935b2e821e9160f0f24161d3868f8869a4d2cfeb924c` |
| `evidencefs-physical-powerloss-entry-audit-20260822.md`                   | `9e4867ba436388751c2f07b9ab30f065435c192699dc37e2cfe9c5c02b2595b7` |
| `migration-shard-runner-closure-repair-independent-review-20260822.md`    | `9284c72f6e3fe3637bcddbf8f4a0d27e53045697363fc80fe9af9c740a6bb9cd` |
| `sdk-identity-closure-independent-review-20260821.md`                     | `cd39fac1f39039479c0b1e182959d26ee40d76718f39b27c22ca49da159c1fe5` |
| `compatibility-recovery-v2-independent-review-20260820.md`                | `c7330ac38f8b72b0002f7bf08ad2b91ae46e40456d46531a32ed8ae11df84185` |

The implementation history also contains independently reviewed generated JSON/Proto/Go/TypeScript
SDK slices, PG15/16/17 catalog and compatibility/recovery matrices, tenant/RLS/RBAC kernels,
evidencefs ext4/XFS and QEMU barriers, first-attempt known-success runner authority, and the approved
reusable shard runner. Those are necessary evidence, but no current record claims that they satisfy
every row of any P1 Exit Gate.

## 2. Criterion-by-criterion disposition

| Gate             | Evidence already present                                                                                                                                                                               | Remaining proof before `VERIFIED`                                                                                                                                                                                                                                                                                                                                                                                                                                         | Current disposition |
| ---------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------- |
| `G-CONTRACT`     | Generated JSON identity/SDK/server seams and Proto SDKs have bounded independent reviews; later operation-specific registries/profiles have fixed per-slice reviews                                    | One current-source immutable phase record must bind the complete current JSON Schema/OpenAPI, Proto descriptor, generated Go/TS SDK/server/mapping fixtures, generation lock, N/N-1/negative/unknown-field behavior, and fresh external consumers. D-047 would add generated identities and invalidate an earlier aggregate contract record.                                                                                                                              | `IN PROGRESS`       |
| `G-DATA`         | Fixed PG15/16/17 matrices, handwritten pgx/SQL kernels, tenant/RLS and compatibility/recovery slices, evidencefs durable barriers, first-attempt success writer and strict runner evidence are present | The proposed recovery/reconcile/failure runner outcomes are still `NOT_IMPLEMENTED`; no immutable record yet proves the complete current migration state machine. A P1 closure must also bind current-source local logical backup/restore, migration replay, outbox/idempotency/leader recovery data digests, N/N-1/live-instance retirement, and every ADR-0010 durability prerequisite. Physical controller/host power-loss evidence and filesystem Done remain absent. | `IN PROGRESS`       |
| `G-AUTHORITY-P1` | Generated operation/profile selection, database roles, tenant context, global-table allowlists, same-verifier claims and ordinary first-attempt writer separation have reviewed slices                 | D-047 must first be explicitly approved, then prove one non-union admission/writer authority for every recovery outcome without ordinary-result conversion or mutation retry. Physical evidencefs authority and one immutable current-source phase record with invalidation rules are still missing.                                                                                                                                                                      | `IN PROGRESS`       |
| `G-SECURITY-P1`  | RLS/RBAC/identity negative fixtures, fail-closed evidence replay, trusted-mount claims, barrier matrices, dependency reviews and scoped secret checks exist                                            | The approved scope still lacks D-047 recovery fault/ambiguity/redaction/cross-profile closure, physical controller/host power-loss evidence, and one current-source phase record that binds all P1 security criteria, current dependency/secret evidence, waivers and reviewer verdict.                                                                                                                                                                                   | `IN PROGRESS`       |

Absence of a named immutable phase record is itself a missing exit artifact. A green implementation
test, a historical matrix, or a search that finds no defect cannot replace that record.

## 3. Two non-substitutable blockers

### 3.1 Owner decision: D-047 / ADR-0023

ADR-0023 is still `Proposed; explicit owner approval required`. Its independently approved audit
proves that the proposed twelve-pair split is internally accurate; it does not authorize generated
profiles, permits, PostgreSQL/evidence writers, or caller wiring. The standing automatic-execution
instruction cannot silently mint this new mutation authority because the decision document itself
requires an explicit owner decision.

The exact requested decision is whether to approve ADR-0023 Sections **Proposed decision**, **Proposed
closed pair mapping**, and **Proposed ordered slices A-G**, retaining all explicit non-claims. Until
that decision exists, the executor must not:

- generate a recovery profile or registry;
- mint a recovery admission or writer permit;
- implement abort/reconcile/handoff/recovery-execution/failure mutation paths; or
- change any current `MIGRATION_PROJECTION_NOT_IMPLEMENTED` result.

If approved, the ordered implementation is generated contracts → read-only recovery admission →
abort terminal writer → commit reconciliation writers → retry handoff → recovery execution → typed
failure/caller matrix, with a fixed independent verdict between authority-expanding slices.

### 3.2 Environment: physical controller/host power loss

The fixed physical entry audit found no dedicated expendable bare-metal DUT/storage target and no
independently tested out-of-band hard-off/hard-on recovery path. Existing QEMU, process-kill, real
ext4/XFS, trusted-mount and production-opened evidence cannot be promoted to physical controller
cache-loss evidence. No available host may be powered off, reformatted, unmounted or repurposed to
manufacture a result.

This blocker can close only when a future entry record proves every precondition in the physical
audit, followed by the fixed destructive matrix and an independent filesystem Done review.

## 4. Safe ordered continuation

1. Obtain the explicit owner decision for ADR-0023. A rejection also resolves the decision, but P1
   plans must then be revised rather than silently omitting required recovery outcomes.
2. If approved, implement and independently review slices A-G in order. Use focused and risk-bound
   tests during development; do not repeatedly rerun the same full `internal/migration` command on
   an unchanged commit.
3. Acquire a dedicated physical DUT, disposable device and independent controller before any
   physical power-loss mutation. Continue other local work while that external resource is absent.
4. After the final approved contract/data implementation settles, assemble fresh current-source
   candidate records for `G-CONTRACT`, `G-DATA`, `G-AUTHORITY-P1`, and `G-SECURITY-P1`. Each record
   must prove every exit row and receive an independent P0/P1/P2 verdict.
5. Only when all four records are `VERIFIED` may P1 change state or P2 start. Later aggregate
   `G-AUTHORITY`, `G-SECURITY`, `G-SUPPLY-CHAIN`, `G-BASELINE`, and Platform RC remain separate.

## 5. No-repeat and non-claims

The existing full-normal and 700-shard artifacts remain fixed historical evidence. This audit does
not request or treat another unchanged-source full migration or broad race run as progress. Future
verification must be criterion-driven: rerun an exhaustive scope only when changed inputs or an
unproven exit criterion make that scope necessary, and never treat timeout as PASS.

This audit does **not** authorize or prove:

- D-047/ADR-0023 approval or implementation;
- physical power action, production database write, HTTP/P2/provider effect, deployment, publication,
  release or main merge;
- any P1 Exit Gate, aggregate Gate, Platform RC, Beta or GA closure; or
- that current implementation evidence remains valid after a future contract/core/store change.
