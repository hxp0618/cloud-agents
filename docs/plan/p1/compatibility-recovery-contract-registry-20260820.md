# P1-A2.4 compatibility and recovery contract registry - 2026-08-20

- Status: **LOCAL GENERATED-CONTRACT SLICE IMPLEMENTED; REVIEW PENDING**
- Decision: [ADR-0015](../adr/0015-p1-compatibility-recovery-contract.md)
- Parent authority: [ADR-0008 §8](../adr/0008-p1-postgres-data-kernel.md)
- Authorization: owner approval for the versioned lineage/quota profile direction and the ordered
  `contract/state-machine registry -> append-only PostgreSQL kernel -> service/claim/matrix/independent review`
  progression
- Does not claim: independent review, SQL migration `000010`, a Go/runtime consumer, HTTP/P2 work, external side
  effects, production mutation, deployment, release, or any Gate closure

## Implemented boundary

The A2.4 entry slice freezes five generated profiles and five same-id closed state machines:

```text
editable source + strict schemas + semantic invariants + ADR boundary
  -> deterministic registry generator
  -> checked-in compatibility/recovery registry
  -> generation-lock input/output digest
```

The profiles cover bounded backfill, live instance registration, migration preflight, local restore evidence, and
retirement receipt. The source fixes the inclusive migration range (`000001` through `000009`), the instance and
writer fields, heartbeat/TTL and drain evidence, six retirement facts, fail-closed unavailable/contradictory
evidence, and the explicit new-database bootstrap exception. PITR, HA, and failover remain P4 work.

The generated artifact is contract evidence only. There is deliberately no `000010`, no PostgreSQL table or function,
no Go binder, no HTTP route, no provider/worker/session/turn/execution wiring, and no external side effect. A future
consumer must bind the exact generated registry identity and digest; caller-provided profiles are forbidden.

## Local verification

The current local checks pass for this bounded slice:

- `bun scripts/check-platform-contracts.ts` (49 fixture cases; the checker still reports its existing missing-suite
  list as open);
- generated registry `--check` and focused registry/semantic tests;
- generated source/profile/state-machine/policy mutations reject before generation;
- generation-lock source/output wiring records the A2.4 registry as `notGateClosure: true` and explicitly records
  `NOT_IMPLEMENTED` consumers, forbidden external side effects, contract-only local restore, and P4 PITR/HA;
- `bun test scripts` (137 passed, 0 failed, 1,594 assertions), `bun run lint`, `bun run typecheck`, `bun run build`,
  `bun run secret:scan`, and scoped formatting/diff checks;
- full migration closure, independent review, PostgreSQL matrix, runtime consumer, and Gate checks remain pending or
  not applicable to this entry slice.

The checks used the repository-pinned versions Node `24.13.1`, Bun `1.3.14`, and Go `1.26.6` (Go was not changed by
this slice). The generated registry SHA-256 is
`f8a0ff0ebc91bab93b1bacf5ec6241f44c8639ae8a11dc0712b485f88156e812`; the generation-lock SHA-256 is
`3b4b2e96cf2815aa0ce3c906b2f78283b4ade3a8bdc4c1134d0e13a8a2e4dcf0`; and the ADR SHA-256 is
`8dc5adf7c32518a150663b514eeaabf2b6a65a66173394135dda88b38d0d3305`. These are local source-evidence hashes,
not deployment or production capability evidence. This document must not be read as evidence of a production
registry or restore capability.
