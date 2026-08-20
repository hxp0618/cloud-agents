# A2.4 compatibility/recovery v2 service and claim slice - 2026-08-20

- Status: **IMPLEMENTED - INDEPENDENT REVIEW PENDING**
- Branch: `codex/cloud-agents-platform-p1`
- Scope: generated v2 operation bindings, typed PostgreSQL service/claim consumer, and local PostgreSQL 15/16/17 normal/race matrix
- Boundary: no HTTP/P2/provider/worker/session/turn/execution surface; no production database write, deployment, release, publication, or Gate closure

## Generated binding

The service consumes only the generated Go registry at
`services/control-plane/internal/compatibility/registry_generated.go`. The source is the checked-in v2 generated
registry (`6` profiles, `26` operations), bound to schema head `000010`, its catalog digest, migration digest,
state-machine digest, policy digest, and registry digest. Every public method selects its operation internally;
there is no caller-provided profile, SQL function, row-selected profile, normalization, alias, or Unicode mapping.
The contract lock records the generated Go output and marks the consumer as non-Gate evidence with all forbidden
surfaces still open.

## Typed service and claims

`CompatibilityRecoveryService` provides operation-specific methods for workload principals, live instances,
backfills, restore evidence, retirement receipts, and read-only migration preflight. Each call creates a private
one-shot claim carrying the exact generated operation and SQL argument tuple. Copy, zero, and consumed claims
fail closed. Mutation claims execute once through the serializable tenant runner; a commit response loss returns
`DatabaseUnknown` with `ReconcileRequired=true` and never retries the write. Reconcile/preflight claims use a
read-only tenant transaction. The bootstrap workload-principal path uses a restricted transaction-local tenant
binding and still relies on the SECURITY DEFINER function's authority check; no historical role grant was changed.

Results expose only typed state/version/epoch/time, closed result codes, and stable error identifiers. PostgreSQL
errors are mapped to stable input/rejected/authority/database classes without raw SQL, credentials, or error text.

## Matrix evidence

`services/control-plane/scripts/test-compatibility-recovery-service-postgres-matrix.sh` creates fresh local,
owner-labelled containers from the exact pinned PostgreSQL 15/16/17 images. It applies migrations `000001` through
`000011`, provisions isolated bootstrap/runtime/migration roles, checks direct table denial and generated helper
access, then runs isolated normal and race service conformance for workload, live, restore, preflight, backfill,
retirement, stale epoch, reconcile, and cross-authority cases. The script never pulls images implicitly and refuses
to remove containers it does not own.

## Verification boundary

Focused compatibility and PostgreSQL service tests pass in normal and race mode. The local PostgreSQL 15/16/17
normal/race matrix passes. Full migration/runner remains subject to the existing ten-minute timeout record and is
not claimed as passing. This record is not an independent review and does not close any immutable or aggregate Gate.
