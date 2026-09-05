# Managed Agent application lifecycle

Current code includes the durable Runtime execution path in [`durable_runtime_execution.go`](durable_runtime_execution.go), in addition to the original in-memory kernel below. Managed Agent owns application Session/Turn/Execution, not the independent long-lived Workspace/Volume lifetime introduced by the [foundation-first design](../../../../docs/plan/cloud-agents-platform/02-target-architecture.md). Keep existing behavior compatible; new user CloudAgents work follows BASE-READY.

## Historical P1 lifecycle kernel

The no-database/no-HTTP statements in this section describe that bounded kernel slice, not the package or platform as a whole.

This package is the first bounded Control Plane seam for the public Managed
Agent authority. It implements a versioned, transport-neutral Session → Turn →
Execution state machine in memory:

- Session creation starts in `active` and can close only after all turns are
  terminal.
- A session admits one foreground turn at a time. A turn starts as `queued`.
- One execution attempt is attached to a turn, then moves
  `queued → running → succeeded|failed|cancelled`.
- Execution transitions atomically move the parent turn to
  `completed|failed|interrupted|cancelled`; interrupt and cancel remain
  distinct terminal reasons.
- Every mutation derives its own SHA-256 request digest from typed input and
  provides same-key replay or a deterministic idempotency conflict.
- Tenant/project, parent identity, generation, digest, identifier, UTF-8, and
  context checks fail closed before state mutation.

The state is intentionally ephemeral and has no PostgreSQL, HTTP listener,
Worker/Supervisor, Provider, Workspace, Artifact, Credential, deployment, or
release dependency. There is no retry/recovery writer, event stream, durable
receipt, or public route in this slice; those require separate authorities and
reviews. The package must not be read as P2 Managed Agent completion or as a
Gate closure.

The profile ID is `cloud-agents/managed-agent-lifecycle/v1alpha1`. Its checked-in
state-machine digest is verified at construction and each mutation boundary.

## Local event projection

Successful lifecycle mutations also append a detached, in-memory
`LifecycleEvent` projection under the versioned profile
`cloud-agents/managed-agent-events/v1alpha1`. A single monotonic sequence is
filtered by the tenant/project scope; returned cursors bind the exact event ID,
scope, profile ID, and profile digest. Idempotent mutation replay returns the
original result without appending a second event, and event records contain
only typed resource IDs, state edges, timestamps, and digests—not raw input or
secrets.

This is a local read seam for ordering and cursor negative tests. It is not a
durable event log, HTTP watch endpoint, PostgreSQL writer, worker dispatch,
provider call, deployment, release, or Gate evidence.
