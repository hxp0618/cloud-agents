# ADR-0013: P1 durable coordination contract and state registry

- Status: Accepted
- Date: 2026-08-19
- Decision owner: hxp0618
- Implementation executor: Codex
- Scope: P1-A2.3 Durable Coordination
- Extends: [ADR-0008](0008-p1-postgres-data-kernel.md)
- Approval basis: the owner explicitly approved the ordered
  `contract/state-machine registry -> append-only PostgreSQL kernel -> service/claim/matrix/independent review`
  direction on 2026-08-19
- Does not authorize: a Managed Agent HTTP implementation, P2 Session/Turn/Execution, Worker or Provider side
  effects, production database mutation, deployment, release, or any Gate closure

## Context

ADR-0008 fixed the top-level durable coordination boundaries, but its caller-provided `operation_name` wording is
not sufficient authority for an idempotency or operation row. The current public contract has one idempotent HTTP
mutation, `managedAgentCreateProject`, and already fixes its strict request projection, RFC 8785 bytes, SHA-256
digest, path tenant authority, and SubjectRef digest. It does not yet fix a generated operation profile, closed
durable state machines, replay envelope, outbox claim lifecycle, or leader fencing contract.

A2.3 must establish those inputs before migration `000007` exists. This ADR narrows the already approved P1
implementation and replaces raw operation-name admission with a generated, digest-bound profile registry.

## Decision

### 1. Ordered implementation slices

A2.3 is implemented and reviewed in three separate commits/slices:

1. contract and state-machine registry;
2. append-only PostgreSQL data kernel;
3. typed service, claim/reconciliation logic, PostgreSQL 15/16/17 matrix, source-bound evidence, and independent
   security review.

Slice 1 cannot create `000007`, database tables, a Go store/service, or an HTTP handler. Slice 2 starts only from
the exact generated registry and generation lock produced by slice 1. Slice 3 cannot expose the existing
Managed Agent route or call a real external actuator; delivery is exercised only through an injected port and
local test doubles.

### 2. Editable and generated authority

The editable authority chain is:

```text
JSON Schema 2020-12
  + OpenAPI operation/route/header authority
  + source registry/state machines/policies
  + one source profile document per idempotent operation
        |
        v checked-in deterministic generator
contracts/generated/platform/v1alpha1/durable-coordination-registry.json
        |
        v exact profileId + profileDigest lookup
future PostgreSQL and Go consumers
```

Callers may supply an idempotency key and request data, but never an operation name, profile body, profile digest,
state, transition, retry policy, or finalizer policy as authority. A future service selects a generated profile by
the already resolved OpenAPI operation. Missing, duplicate, stale, unknown, or digest-mismatched profiles fail
before an idempotency row or side effect.

The generated file contains no timestamp, host path, environment-derived value, or caller input. It records:

- a domain-separated digest of the complete source registry;
- separate state-machine and policy digests;
- exact operation, attempt, receipt, finalizer, cleanup, idempotency, outbox, leader, and audit identity/claim rules
  inside the generated policy object;
- each source profile plus a domain-separated profile digest that also binds the state-machine and policy
  digests;
- a domain-separated digest of the generated registry body.

`contracts/generation.lock.json` binds the generator sources, exact input manifest, generated output path and
output SHA-256. Editing generated output without changing an input fails `--check`.

### 3. Current operation profile

The only current source profile is `managedAgentCreateProject/v1alpha1`. It binds:

- OpenAPI operation ID `managedAgentCreateProject`;
- `POST /v1/tenants/{tenantId}/projects` and required `Idempotency-Key`;
- the existing strict create-project idempotency projection schema and canonicalization profile;
- SubjectRef exact-string SHA-256 identity;
- path tenant authority, organization scope authority, and `projects.create` permission;
- a 24-hour database-time idempotency replay TTL;
- a redacted resource-reference terminal envelope;
- `resource_change` outbox class;
- no PlatformOperation row, no required finalizer, and no external side effect.

This profile does not implement or expose the route. It is a contract input for later storage and service slices.
Adding a second idempotent operation requires a new source profile, exact OpenAPI/projection binding, fixtures,
regeneration, review, and a new generation-lock digest.

### 4. Idempotency authority and lifecycle

ADR-0008's logical `operation_name` is replaced by the generated profile identity. The durable uniqueness authority
is:

```text
(tenant_id, subject_digest, profile_id, profile_digest, idempotency_key)
```

The request digest is the profile-defined canonical projection digest. The idempotency state machine is:

```text
pending --record_success--> succeeded
pending --record_failure--> failed
```

`succeeded` and `failed` are immutable terminal states. Same key and same request digest returns only an
in-progress reference while pending, or the typed redacted terminal envelope after completion. Same key and a
different request digest returns a stable conflict before mutation or enqueue. Rows may be removed only after
they are terminal, expired by database time, and unreferenced by an active operation or finalizer.

Stored terminal envelopes allow only resource kind/ID/version or a stable error code. Raw request/response JSON,
authorization headers, tokens, credentials, pairing material, stack traces, and provider payloads are forbidden.

### 5. PlatformOperation, Attempt, Receipt, cleanup, and Finalizer

The generated catalog contains closed deterministic transition tables. Every `(from, event)` pair has exactly one
destination; all states are reachable; terminal states have no outgoing edge.

`PlatformOperation/v1` states are `pending`, `running`, `reconciling`, `succeeded`, `failed`, and `canceled`:

- a pending operation can start one new attempt or be canceled before an attempt;
- a running attempt can succeed, fail terminally, fail retryably into a new pending attempt, or become unknown and
  enter reconciliation;
- reconciliation can prove success, prove terminal failure, or authorize a new pending attempt;
- `succeeded`, `failed`, and `canceled` are terminal.

`OperationAttempt/v1` states are `ready`, `claimed`, `unknown`, `succeeded`, and `failed`. Claim expiry or ambiguous
commit moves a claimed attempt to `unknown`, never directly back to `ready`; reconciliation must prove success or
failure. Retry creates a new attempt identity rather than resetting an old attempt.

`TerminalReceipt/v1` is `absent -> persisted`. The persisted receipt identity is
`tenant_id + operation_id + operation_generation + attempt_number + receipt_id`; outcome is one of
`succeeded|failed|canceled`. A receipt is append-only and immutable. An unknown attempt has no terminal receipt.

Cleanup uses the closed phases already fixed by the platform architecture:
`none|pending|revoking|draining|reaping|complete|blocked`. `complete` is possible only after every required
finalizer is `succeeded`. `blocked` fences admission and may return to `pending` only through an audited manual
recovery event with an advanced recovery generation.

Finalizer states are `pending|claimed|retry_wait|unknown|succeeded|dead_letter`. Claim ambiguity enters `unknown`;
reconciliation decides success, retry, or terminal dead letter. A dead-letter required finalizer forces cleanup to
`blocked`; it never satisfies cleanup completion.

### 6. Outbox contract

Outbox delivery is at-least-once and requires consumer deduplication by event ID. Claim authority always binds the
complete tuple:

```text
claim_holder_id + claim_incarnation + claim_token + claim_expires_at
```

ACK, retry, claim expiry, and DLQ updates match message ID and the full tuple. Database time owns claim expiry.
The closed states are `pending|claimed|retry_wait|delivered|dead_letter`; `delivered` and `dead_letter` are terminal.
The fixed delivery budget is eight attempts with retry backoff seconds `1,2,4,8,16,32,64,128`.

The mutually exclusive event classes remain:

- `resource_change`: real tenant resource version, `generation = 0`, null operation ID, and aggregate sequence equal
  to resource version;
- `operation_effect`: non-null operation ID, positive generation, and positive aggregate sequence.

Both classes have unique `(tenant_id, event_id)`. Operation effects additionally have unique
`(tenant_id, aggregate_kind, aggregate_id, generation, aggregate_sequence)`. The current create-project profile
selects only `resource_change` and does not deliver it to an external system in A2.3.

### 7. Leader fencing

Leader identity is `leader_name + holder_id + holder_incarnation`. A lease also carries a positive monotonic
fencing token and database-time expiry. Acquisition/takeover locks the leader row; takeover requires observed
expiry and increments the token. Lease duration must be between one and sixty seconds.

Every leader-owned authority write locks the same leader row and matches holder identity, incarnation, positive
token, and unexpired database time in the same short transaction. A process-local clock, holder ID alone, or a
reused incarnation cannot authorize a write.

### 8. Audit and secret boundary

Durable audit is a minimal fact, not a request/response archive. It may contain tenant, subject digest, profile ID
and digest, operation/attempt/resource references, transition, stable outcome/error, resource version, generation,
fencing token, and database timestamp. It must not contain raw authorization, cookies, access/refresh tokens,
pairing URLs/tokens/hashes, credentials, private keys, broker grants, raw request/result bodies, or provider data.

The same prohibition applies to idempotency, outbox, receipt, logs, traces, backups, and test snapshots. Schema
strictness and negative fixtures reject additional secret-bearing fields; later Go/SQL consumers must independently
enforce the generated allowlist.

### 9. Later slice requirements

Slice 2 must use append-only migration `000007`, tenant-owned composite keys/FKs, FORCE RLS, database-time
constraints, migration/runtime role separation, immutable applied migration rules, and exact generated profile
digests. It cannot add a profile or state absent from the registry.

Slice 3 must expose typed package-internal APIs, use closed result types for committed/rejected/unknown database
outcomes, test full claim tuple and fencing, and run PostgreSQL 15/16/17 normal/race/fault matrices. Outbox delivery
uses only an injected port. Independent review is required before this A2.3 implementation boundary is called
closed.

None of these slices closes `G-CONTRACT`, `G-DATA`, `G-AUTHORITY-P1`, `G-SECURITY-P1`, or an aggregate Gate.

## Rejected alternatives

### Store caller-provided operation names

Rejected. A string can select unreviewed retention, replay, outbox, or finalizer semantics and is not contract
authority.

### Hand-maintain the runtime registry

Rejected. A Go map or SQL seed disconnected from OpenAPI/projection inputs creates a second authority and cannot be
reproduced from the generation lock.

### Retry unknown attempts by resetting them to ready

Rejected. A response-lost external effect could be duplicated. Unknown requires reconciliation and a new attempt
identity only after a proved retry decision.

### Store complete request or response bodies for replay

Rejected. It expands the secret and privacy surface and permits contract drift. Replay uses the canonical digest
and a typed redacted envelope only.

### Implement the existing HTTP route in A2.3

Rejected. This approval freezes foundation authority only. Public mutation execution and P2 side effects remain
outside the slice.
