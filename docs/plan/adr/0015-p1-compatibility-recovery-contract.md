# ADR-0015: P1 compatibility and recovery contract registry

- Status: Accepted
- Scope: A2.4 generated contract/state-machine registry only
- Depends on: ADR-0008 §8, the accepted P1 PostgreSQL data-kernel plan

## Decision

The A2.4 entry slice freezes a generated, digest-bound registry for live-instance
compatibility, retirement evidence, local restore evidence, migration preflight,
and bounded backfill state. The editable source is validated by the checked-in
JSON Schema and semantic generator; future consumers must bind the exact generated
`registryId` and `registryDigest`.

The registry records the versioned migration range, instance identity and writer
fields, heartbeat/TTL and drain state, the six facts required to exclude an expired
registration, restore/preflight evidence, and closed state-machine transitions.
Unknown, unavailable, stale, disjoint, or contradictory evidence is rejected.
An empty registry is an explicit bootstrap exception only for a new database; an
existing ledger cannot use it.

## Boundary

This slice adds no SQL migration (there is no `000010`), Go consumer, HTTP route,
provider/worker/session/turn/execution path, or external side effect. P1 remains
limited to local logical backup/restore and preflight contract evidence; PITR,
HA, and failover remain P4 work. The generated registry and its tests are
non-Gate evidence and do not close any aggregate Gate.

## Acceptance evidence

- Source and generated documents validate under the in-repo AJV 2020-12 checker.
- Profile/state-machine IDs, ordering, transitions, retirement facts, and boundary
  constants are semantically checked before generation.
- Generation lock records all source/tool/schema/ADR inputs and the generated
  output digest.
- Negative mutations reject missing TTL proof, nondeterministic transitions, and
  any attempt to enable an HTTP or external-side-effect consumer.
