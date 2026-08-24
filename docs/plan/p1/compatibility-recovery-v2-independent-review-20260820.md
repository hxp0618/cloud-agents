# P1-A2.4 compatibility/recovery v2 independent review - 2026-08-20

- Status: **APPROVE bounded implementation/review slice — P0=0 / P1=0 / P2=0**
- Reviewed HEAD and remote ref: `b639b07fe873bf42570902fc65694f0f3f296398`
- Implementation chain: `820b734` → `031c36b` → `b639b07`
- Branch: `codex/cloud-agents-platform-p1`
- Review mode: fixed-source read-only pass after the three implementation slices were committed and pushed
- Scope: versioned generated registry/profile repair, append-only PostgreSQL writer kernel, typed service/claim,
  and local PostgreSQL 15/16/17 normal/race matrix
- Does not authorize: HTTP/P2/provider or other external effects, production database mutation, deployment,
  release/publication, or any immutable/aggregate Gate closure

## 1. Verdict

The fixed implementation is internally consistent across the generated contract registry, PostgreSQL operation
surface, typed service and local matrix. No P0, P1 or P2 finding remains in the reviewed A2.4 boundary.

| Boundary                          | Verdict | Evidence                                                                                                                                                            |
| --------------------------------- | ------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| historical v1 and `000010`        | PASS    | the v1 source/schema/registry and `000010` are byte-identical to the pre-A2.4 repair ref                                                                            |
| generated v2 registry/profile     | PASS    | six profiles and 26 unique operations are generated and bound to exact registry, state-machine, policy and schema digests                                           |
| append-only `000011` writer       | PASS    | every registered SQL function exists; private helpers have no non-owner grant; tables remain denied to runtime/bootstrap/PUBLIC                                     |
| typed service/claim               | PASS    | 26 generated selections map one-to-one to 26 typed methods; caller profiles and SQL names are absent; claim copies and reuse fail closed                            |
| transaction outcomes              | PASS    | mutations execute once in SERIALIZABLE transactions; result drift rolls back before commit; commit ambiguity becomes unknown/reconcile-required without write retry |
| authority/identity                | PASS    | bootstrap/runtime/migration-owner paths are separated; the exact ASCII identifier grammar is revalidated without normalization or mapping                           |
| PostgreSQL matrix                 | PASS    | fresh PostgreSQL 15/16/17 normal/race legs pass with isolated roles, direct-DML denial and cross-authority faults                                                   |
| HTTP/P2/provider/external effects | PASS    | the implementation exposes no route, handler, provider, worker, Session, Turn, Execution or delivery port                                                           |
| immutable and aggregate Gates     | OPEN    | this review is not a Gate signature and closes no Gate                                                                                                              |

## 2. Generated authority and append-only storage

The v2 Go package is generated from the checked-in registry. An `Operation` is opaque outside its package; every
public service method selects one exact generated operation internally. The generator rejects registry identity,
schema binding, selection rule, mode, unknown-outcome and implementation-boundary drift. The service additionally
checks the exact registry/state-machine/policy/schema tuple before minting a one-shot private claim.

Mechanical review found exactly 26 registry SQL operation names, all present in `000011`; exactly 26 generated
operation getters; and exactly 26 public typed service methods. No operation name or profile is accepted from a
caller or selected from a stored row.

The forward migration leaves the historical v1 registry and `000010` same-bits. `000011` owns its v2 tables,
transition facts, pure binding helpers and generated operation wrappers. Raw tables are denied to runtime,
bootstrap and PUBLIC. Bootstrap receives only workload-principal wrappers, runtime receives only live-instance,
retirement, preflight and pure binding wrappers, while backfill/restore remains on the explicit migration-owner
path. Private lock, transition and domain helpers receive no non-owner grant.

## 3. Typed claim and closed outcomes

Every mutation is input-validated before database acquisition and executes once through a SERIALIZABLE tenant
transaction. Reconcile and preflight run read-only. The claim copies its argument tuple, validates self identity and
can be consumed only once. The workload-principal bootstrap path uses a restricted transaction-local tenant binder;
the SECURITY DEFINER wrapper still performs its own `require_tenant_id` and role check. Existing shared tenant read
and mutation entry points continue using the ordinary binder.

Typed result validation occurs inside the transaction callback. An invalid result shape therefore rolls back before
commit. A non-PostgreSQL commit response loss discards the physical connection and returns only
`DatabaseUnknown` plus `ReconcileRequired=true`; no mutation retry occurs. PostgreSQL/input/authority/conflict
failures map to closed redacted errors without raw server text, credentials or SQL.

## 4. Fixed evidence

Reviewed SHA-256 bindings:

- generated v2 registry: `837e9a7f3b9e00dc5971cafb2bb37695b4fd5553db7e3003214f327be05e850e`;
- `000011_add_compatibility_recovery_writer.sql`: `67811ee604d01732d5ed19e4c0c108013f2acd95a0f9eefc716fd6b1072a2b61`;
- generated Go registry: `976ad31264546818a00c369fda7ee6552fe9cae1614d106d52f8e9b2e006f6b0`;
- typed service: `fa71f017095a8046af48215c79fb168713dcc75b9754b0b5446f0b2acd014a35`;
- service unit tests: `373e277c17154d8396a24ea76a347bc7e6fd21ff10a2a2baeccdecb0747e0cc3`;
- service integration test: `27680002001c38dfb90d5746ea03c9141df8b38bb4e59bd2df484f4f67b2f20d`;
- PostgreSQL service matrix: `b4eaf315d1ff1370162aaaf72159c7f90b0748f76d3ca2d35d9c9a90edee9fef`;
- generation lock: `0ddf6a0d8652ac42e1b133ef332614c483c4783f24ec8269f145289763102e13`.

Fresh fixed-source review checks passed under Node `24.13.1`, Bun `1.3.14` and Go `1.26.6`:

- byte-exact generated registry/Go registry/contract lock and migration bundle checks;
- focused compatibility/store normal and race tests;
- non-migration control-plane package normal and race tests;
- control-plane vet, build, module tidy-diff and module verify;
- Linux amd64/arm64 CGO-free compile/build for the changed Go packages;
- PostgreSQL 15/16/17 normal/race service matrix after the fixed commit;
- lint, owned-file formatting, shell syntax, scope scans and `git diff --check`.

The contract checker still reports its documented missing official/compatibility suites and marks the result
`notGateClosure=true`. The migration bundle remains `UNPUBLISHED_BOOTSTRAP_MUTABLE` with runtime catalog
introspection, signing and publication not implemented. The aggregate Go-module checker entered the known long
`internal/migration` suite and was stopped rather than reported as success; this run is **NOT PASS**. The existing
untracked `services/control-plane/migration.test` was not inspected, changed, staged or committed. The historical
repository secret scan also did not finish within the bounded local review window; a scoped staged-source pattern
scan was clean, but no fresh full-history secret-scan PASS is claimed.

## 5. Boundary after approval

This record approves only the owner-requested A2.4 generated registry/profile → append-only writer kernel → typed
service/claim/matrix implementation and its bounded review. It does not approve HTTP/P2/provider integration,
production provisioning or database writes, deployment, release/publication, PITR/HA, or any Gate closure. All
immutable and aggregate Gates remain OPEN and require their own fixed evidence and signatures.
