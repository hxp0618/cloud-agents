# P1 runner ledger entry writer contract audit - 2026-08-22

- Status: **CONTRACT DIRECTION APPROVED; ORDERED LOCAL SLICES AUTHORIZED**
- Audited source: `d4cad5d7dab43e1bbc261d4902d8e526364d4db3`
- Repository tree: `977a612b533a8ce83539c630ce7b93366e9eea7c`
- Control-plane subtree: `6d9dd4b294474628a1387c8cd14bd9d0540b2d9f`
- Accepted decision: [`ADR-0022`](../adr/0022-p1-runner-ledger-entry-success-writer-contract.md)
- Scope: read-only audit and versioned contract proposal only

This record does not add a generated profile, database session, permit consumer, transaction, SQL execution,
ledger/evidence mutation, `Runner.Run` branch, HTTP/P2/provider surface, production database write, deployment,
publication, release, or Gate closure.

## 1. Fixed inputs

| Input                                                        | SHA-256                                                            |
| ------------------------------------------------------------ | ------------------------------------------------------------------ |
| Current signed schema bundle                                 | `a01a22e09c7301aeafc87eb1f09b67cb844e5ac5bc5b3c6dd1e66827e348b90f` |
| Generated entry-admission v1 registry                        | `2dc0210f1aad1dd6cff1183324837ab7e88cc5491e9046ae07302b25a1f9e372` |
| Generated entry-admission Go profile                         | `c95850d99a5cbc9d82480d2c63befd6a39ffaeeb2f2c2f1374ce21091ff806c6` |
| Close-only `runnerLedgerEntryAdmissionPermit` implementation | `255088e37e40d897d76ba589dbf2afd9dbb7dcf3e9d17e6b9d752735f4306714` |

The generated v1 registry itself binds profile digest
`sha256:bee8d42d328984f929fafa1e9dd2d4b18c94814e9375a1e1a1199ad8cb0ab551`, state-machine digest
`sha256:94baeab96aaa0da096b6de2c48fc646723d9ad003b68fd1135edf4adcc141189`, and policy digest
`sha256:9869fae48b30e4077f346b1d7c4309056a2129fb5974e73762d897e2925dc26a`.

## 2. Verified current boundary

### 2.1 Entry-admission v1 is intentionally non-consumable

The immutable generated registry admits five pairs only to `prepare_entry_admission`, then permits only
`close_without_mutation`. Its implementation boundary fixes `permitConsumer=none`, forbids migration/RW
transactions, `BeginMigration`, ledger mutation, and evidence mutation, and leaves both entry and recovery writers
`not_implemented`.

The production permit matches that contract: it retains the fresh dedicated session and advisory lock to prove
exact cleanup, is registry-backed and one-shot, and has no writer transition. Adding a consumer to this type would
change v1 semantics without changing its generated identity. That direction is rejected.

### 2.2 The existing writer is a closed historical special case

The existing `runnerPreparedCurrentSession` chain requires:

- empty durable ledger;
- `brand_new` or `brand_new_inherited`;
- `begin_first_attempt`;
- entry index zero and attempt index one;
- one migration and one statement;
- segment zero with fixed early cursor positions; and
- bundle completion after the first committed terminal.

Its evidence binders are correspondingly named and validated as brand-new first-statement/final-statement records.
They cannot safely support a wider bundle by removing count, index, cursor, or completion checks. The reviewed next
contract must introduce distinct types and binders rather than weakening the historical authority chain.

### 2.3 Multi-statement support is mandatory

The signed current bundle contains eleven migrations. The exact number of statement descriptors in each migration's
signed cumulative catalog source is:

| Migration | Statements | Migration | Statements |
| --------- | ---------: | --------- | ---------: |
| `000001`  |         20 | `000007`  |         89 |
| `000002`  |         71 | `000008`  |         34 |
| `000003`  |         46 | `000009`  |         30 |
| `000004`  |         20 | `000010`  |         52 |
| `000005`  |          1 | `000011`  |        161 |
| `000006`  |          1 |           |            |

Therefore a one-statement-only entry writer cannot execute the current signed source of truth. The existing wire
contract already supports repeated statement intent/intermediate pairs with exact previous-intermediate links; the
missing work is a new production authority chain, dynamic cursor handling, and exact per-entry transaction state.

### 2.4 Cross-entry reuse is unsafe

The current commit lifecycle closes the dedicated database session after the commit result is observed and before a
durable terminal is returned. This is required for ambiguous-outcome handling. A committed entry cannot retain or
reuse that session as authority for the next entry.

The safe boundary is one entry per execution permit and one dedicated session. After a durable committed terminal,
any next entry must re-enter the locked read-only preflight and fresh execution-admission path. Ordinary committed
result data must not choose or authorize the next writer.

## 3. Threat conclusions

### T1 — no v1 permit reinterpretation

`runner-ledger-entry-admission/v1` must remain byte-identical and close-only. The next execution authority needs a
new generated identity and a separately sealed permit minted by a fresh same-session revalidation kernel.

### T2 — no mixed success/recovery writer

The five entry-admission pairs contain one retry action:
`empty_brand_new / brand_new_inherited / begin_next_attempt`. That pair depends on a previous attempt terminal and
recovery receipts. It must not enter the first success-writer version.

The first writer profile can accept only the four first-attempt pairs. Abort, retry, dangling evidence, commit
reconciliation, terminal failure, and return failure need separate generated contracts and independent reviews.

### T3 — one statement is not one transaction state

For a 161-statement entry, every statement needs its own durable intent, exact SQL execution, immediate after
projection, durable intermediate, and next-statement successor. Only the final intermediate may carry the preledger
pair. The transaction, ledger prefix, entry, attempt, bundle, catalog/authority bindings, and evidence cursor must
remain identical across every successor.

### T4 — evidence position cannot be hard-coded

The retained evidence session supports existing-segment append, rotation, checkpoint, reopen, and unknown outcome.
A multi-statement writer cannot assume segment zero or absolute sequences one through four. Every append must consume
the prior sealed cursor and accept only the returned next cursor. Rotation and checkpoint are part of the durable
transition, not optional cleanup.

### T5 — database commit and evidence terminal are separate irreversible barriers

A known committed database transaction followed by a failed/unknown terminal append must never execute SQL or
commit again. A durable commit intent plus exact ledger/catalog replay is the recovery boundary. The success profile
may append a committed terminal only for a known committed outcome; all other outcomes leave recovery work
`NOT_IMPLEMENTED`.

### T6 — no bundle-level success from an ordinary entry result

A single committed entry may mean either complete bundle or exact next entry. The first writer should return a
closed one-entry outcome only. A later typed orchestrator may loop through fresh preflight/admission calls and may
produce the public bundle result only after exact completion. This avoids using an ordinary entry result as a writer
permit.

## 4. Accepted contract boundary

[`ADR-0022`](../adr/0022-p1-runner-ledger-entry-success-writer-contract.md) freezes two distinct generated profiles:

1. `runner-ledger-entry-execution-admission/v1`, which repeats fresh locked revalidation and mints a new one-shot
   execution permit without touching the ADR-0021 permit; and
2. `runner-ledger-entry-success-writer/v1`, which consumes one such permit and drives one exact first attempt through
   a known committed terminal.

The first profile initially has only a close-without-mutation implementation slice. The second profile is not wired
to `Runner.Run` until its multi-statement kernel, durability/fault matrix, and independent review are fixed.

## 5. Required implementation order

1. **Generated registries:** source schemas, fixtures, generated JSON/Go profiles, manifests, generation lock, and
   historical v1 same-bits.
2. **Execution admission:** fresh session/role/settings/lock plus exact ledger/catalog/evidence reread; new permit;
   close-only implementation and fault matrix.
3. **Success kernel:** one exact entry, all signed statements, dynamic evidence cursor, exact ledger readback,
   commit-once, committed terminal; no public caller.
4. **Independent kernel review:** fixed candidate, normal/race, barrier faults, ephemeral database matrix, forbidden
   surface scan, P0/P1/P2 verdict.
5. **Typed caller:** only after kernel approval, connect the four first-attempt pairs and re-enter fresh preflight for
   each next entry; repeat matrix and independent review.
6. **Recovery profiles:** separately decide and implement retry/abort/reconcile/failure paths.

Each slice is an independent commit/review boundary. None authorizes a production database, deployment, release,
publication, main merge, or Gate closure.

## 6. Recorded owner authority

The standing goal explicitly authorizes automatic continuation through the approved Platform plan without another
owner prompt, while retaining the no-production/no-release boundaries. D-046 therefore records the following exact
scope:

> Approve ADR-0022's two new versioned generated profiles and the ordered generated-registry → close-only
> execution-admission → one-entry multi-statement success-kernel → independent review → typed caller slices. Keep
> all retry/abort/reconcile/failure writers `NOT_IMPLEMENTED`; keep preflight/consumer/entry-admission v1 immutable;
> do not authorize production database writes, HTTP/P2/provider effects, deployment, publication, release, main
> merge, or any Gate closure.

This authority permits the ordered local slices only. It does not permit a later slice to skip generated identity,
fixed-candidate review, or the separately required recovery profiles.

## 7. Local validation

The contract-only candidate was checked with the repository-pinned Node `24.13.1`, Bun `1.3.14`, and Go `1.26.6`
toolchain tuple:

- frozen dependency install: PASS;
- target Markdown format write/check: PASS;
- repository lint: PASS;
- repository workspace typecheck: PASS;
- platform contract, registry, SDK, and generation-lock check: PASS/current (`109` JSON files, `46` schemas,
  `58` fixture cases);
- local Markdown links for the five changed/new documents: PASS; and
- `git diff --check`: PASS.

No Go test, migration suite, live PostgreSQL matrix, mutation test, or Gate check is claimed by this document-only
audit. Existing generated files and production code were not changed.
