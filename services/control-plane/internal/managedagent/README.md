# Managed Agent lifecycle P1 kernel

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
