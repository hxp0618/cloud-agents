# Standalone Platform v0.1 — Managed Agent lifecycle event/cursor P1 kernel

- Slice: `managed-agent-event-journal-p1-kernel`
- Authority: `cloud-agents/managed-agent-events/v1alpha1`
- Parent P0 commit: `8679b7ee554298a63c243b49087cad164e9c1c5f`
- Fixed implementation commit: `192b2e5f5204560c122daedad480d7adac68279d`
- Fixed implementation tree: `27e7d2eae6d63183d151118c7b08e4e296dd25d0`
- Binary diff SHA-256 against the P0 parent:
  `sha256:71eb587437b0ce7a4177484dca18377aae0f584d6c877414f1e6932bdd0a8dd5`
- Candidate branch: `codex/standalone-managed-agent-event-journal-20260827`

## Decision boundary

This slice adds one local, transport-neutral event projection to the already
approved Managed Agent lifecycle kernel. It is deliberately ephemeral and
read-only from a consumer's perspective: successful in-memory mutations append
typed events and callers may page through them with a scope-bound cursor.

It does not alter any D-053-MIG-000014.r1/r2 source/profile/schema/manifest/
SQL/catalog/archive/review bytes, D-053-EC-2 authority, generated public
contracts or SDKs. It does not open a canonical/production Runner, PostgreSQL
writer, HTTP/P2/provider path, Worker dispatch, Workspace/Artifact/Credential
actuator, deployment, publication, release, or Gate transition.

## Frozen event authority

`LifecycleEventProfile` freezes:

- profile ID `cloud-agents/managed-agent-events/v1alpha1`;
- algorithm `global-sequence-scope-filter-v1`;
- the exact event field projection
  `event_id|sequence|scope|operation|resource|session_id|turn_id|execution_id|generation|occurred_at|mutation_digest|input_digest|result_digest|error_code|changes(resource,from,to,version)`;
- the nine successful lifecycle operations in their deterministic order:
  `session.create`, `session.close`, `turn.create`, `execution.create`,
  `execution.start`, `execution.complete`, `execution.fail`,
  `turn.interrupt`, and `turn.cancel`;
- maximum page size `64`; and
- profile digest
  `sha256:e38816e4df5b8aff6338537283f7eb7f9757aef9333b9a4e464dcec365a913b4`.

The event sequence is assigned inside the store under its existing mutex. A
replayed idempotency key returns the original mutation record and does not
append a duplicate. Coupled turn/execution transitions produce a deterministic
two-edge change list; raw input is represented only by its existing SHA-256
digest, and no secret or provider response is retained.

## Cursor and isolation rules

The zero `EventCursor` is the only beginning cursor. Every subsequent cursor
must carry the exact scope, sequence, event ID, profile ID, and profile digest;
the reader verifies that the sequence/event pair exists and belongs to the
requested scope before returning data. A page filters the global monotonic
sequence by the requested tenant/project scope, returns detached events and
nested change lists, and reports whether another event for that scope exists.
Malformed, foreign-scope, foreign-event, profile-drifted, future, partial, or
oversized cursors fail closed before a read result is returned.

## Verification target

The candidate is expected to pass:

- `GOWORK=off GOFLAGS=-mod=readonly go test ./internal/managedagent -count=1`;
- the same package's race run, `-shuffle=on`, and `go vet`;
- Control Plane compile-only closure with `go test ./... -run '^$'`;
- focused server and module-policy tests; and
- negative checks for idempotent replay, invalid transitions, cancellation,
  tenant isolation, cursor identity/profile drift, cursor sequence gaps,
  page-size bounds, detached results, and raw-input non-retention.

These checks prove only the local event/cursor kernel. They do not prove a
durable event log, watch HTTP endpoint, PostgreSQL persistence, retry/recovery
writer, provider execution, production readiness, or any Gate closure.
