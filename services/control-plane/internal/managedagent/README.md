# Managed Agent application lifecycle

Current code includes the durable Runtime execution path in [`durable_runtime_execution.go`](durable_runtime_execution.go), in addition to the original in-memory kernel below. Managed Agent owns application Session/Turn/Execution, not the independent long-lived Workspace/Volume lifetime introduced by the [foundation-first design](../../../../docs/plan/cloud-agents-platform/02-target-architecture.md). Keep existing behavior compatible; new user CloudAgents work follows BASE-READY.

While an execution is active, its in-memory message view is the complete public
transcript, including the last persisted checkpoint; readers use the persisted
snapshot only when no active view exists. The execution claim keeps renewing
after the Runtime returns a terminal message until the fenced durable
completion or failure transition returns. Each renewal attempt is bounded to
less than one renewal interval so a stalled store call fails the claim before
the lease can expire silently. Runtime health checks stop at the terminal
message so settlement delay cannot turn a completed Runtime into a spurious
Worker-health failure.

Each accepted non-terminal Runtime frame remains a durable checkpoint boundary.
Store-side transcript validation, size enforcement, digesting, and persistence
reuse one canonical encoding at that boundary; optimizations must not batch
frames or widen the recovery loss window. Checkpoint writes inherit the Runtime
lifetime and have a finite persistence deadline. Terminal completion, failure,
and cancellation use an independent finite persistence deadline so Runtime
expiry cannot stop claim renewal mid-settlement and a stalled store cannot hold
the execution forever.

Long-running execution POST requests use a bounded admission pool separate from
ordinary API requests. Execution saturation therefore rejects only new
execution starts while reads, cancellation, and other control requests retain
their own bounded capacity.

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
