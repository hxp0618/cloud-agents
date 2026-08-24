# ADR-0017: P1 compatibility and recovery versioned registry/profile repair

- Status: Accepted by owner approval on 2026-08-20
- Scope: A2.4 versioned generated registry/profile Slice A only
- Depends on: ADR-0015, ADR-0016, and the approved A2.4 service-entry blocker

## Decision

A2.4 proceeds as three ordered slices:

1. repair the generated, versioned compatibility/recovery registry/profile;
2. add a later append-only PostgreSQL writer kernel after `000010`; and
3. add typed service/claim consumers, the normal/race/fault matrix, and an
   independent review record.

The first slice creates `cloud-agents-compatibility-recovery-source/v2` and
`cloud-agents-compatibility-recovery-registry/v2`. Its six generated profiles
are `backfill/v2`, `live-instance/v2`, `migration-preflight/v2`,
`restore-evidence/v2`, `retirement-receipt/v2`, and `workload-principal/v2`.
Each profile binds exact operation, SQL-function, service-method, capability,
state-machine, evidence, and unknown/reconcile identities. A caller, stored
row, guessed schema, or operation string cannot select or elevate a profile.

The v2 registry binds schema head `000010`, its checked-in catalog and
migration digests, and the historical v1 registry digest. The v1 source,
generated output, ADR-0015, ADR-0016, `000010`, and all predecessor artifacts
remain immutable same-bits inputs. V2 is a new generated artifact; it does not
rewrite the v1 registry or migration history.

## Slice boundaries

Slice A is contract evidence only. It generates and validates source/output,
schemas, fixture-manifest entries, and generation-lock evidence. It does not
implement a SQL writer, Go consumer, HTTP route, P2 surface, provider or
worker/session/turn/execution path, or any external side effect.

The future writer kernel is a new forward migration after `000010`; it is not
part of this ADR's Slice A output. Production database writes, deployment,
release, and publication are not authorized. All immutable and aggregate Gates
remain open.

The generated implementation boundary is therefore explicit:

- runtime and typed service consumer: `NOT_IMPLEMENTED`;
- SQL writer after `000010`: `NOT_IMPLEMENTED`;
- HTTP and P2: `NOT_IMPLEMENTED`;
- provider/external side effects: `FORBIDDEN`;
- production database writes: `NOT_AUTHORIZED`;
- Gate status: `ALL_GATES_OPEN`.

## Acceptance evidence

- v1 source/output digests are replayed and unchanged;
- v2 source/output and all schema/catalog bindings are deterministic;
- the generated-only selector and exact profile/operation identities are
  validated by schema and semantic fixtures;
- the lock records v1 and v2 as distinct non-Gate pipelines; and
- later slices may start only from this generated evidence and must retain the
  same no-external-side-effects and no-production-write boundary until their
  own review is complete.
