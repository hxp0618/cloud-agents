# Standalone Platform v0.1 — Managed Agent lifecycle P1 kernel

- Slice: `managed-agent-lifecycle-p1-kernel`
- Authority: `cloud-agents/managed-agent-lifecycle/v1alpha1`
- Parent candidate: `16947a7db51d63ecce7f32153c88322d6e894854`
- Scope: one transport-neutral Control Plane lifecycle kernel; no public route,
  PostgreSQL writer, Worker claim, Provider call, Workspace/Artifact/Credential
  actuator, deployment, release, or Gate transition.

## Source and boundary

The implementation is constrained by the existing public-platform authority in
`docs/plan/cloud-agents-platform/01-product-scope-and-authority.md` and
`docs/plan/cloud-agents-platform/02-target-architecture.md`, plus the existing
Runtime v2.3 correlation vocabulary in
`packages/cloud-agent-protocol/src/protocol.ts` and
`packages/cloud-agent-protocol/src/runtimeEvent.ts`. The current
`contracts/managed-agent/v1alpha1/openapi.json` contains only the
Tenant/Organization/Project/RBAC foundation; it has no Session/Turn/Execution
paths. This slice therefore does not invent or publish an HTTP schema.

## Implemented code

`services/control-plane/internal/managedagent` provides:

1. A checked-in profile and state-machine digest, with construction/mutation
   drift checks.
2. Tenant/project-scoped in-memory Session, Turn, and Execution snapshots.
3. Strict transitions:
   - Session `active → closed` only when every turn is terminal;
   - Turn `queued → running → completed|failed|interrupted|cancelled`;
   - Execution `queued → running → succeeded|failed|cancelled`;
   - one execution attempt and one foreground non-terminal turn per session.
4. Atomic parent/child updates for start, complete, fail, interrupt, and cancel.
5. Typed identifier/UTF-8/control-character/size/generation/digest checks,
   context cancellation checks, exact parent/tenant binding, and detached
   snapshots. Raw turn input is hashed and never retained.
6. Per-operation idempotency replay. The request digest is derived inside the
   kernel from a canonical typed projection; same-key payload drift returns a
   stable conflict.

The state is intentionally ephemeral. Retry/recovery, event cursor/sequence,
durable receipts, authn/authz binding, PostgreSQL append-only persistence,
Worker/Supervisor dispatch, and Provider execution remain separate slices.

## Verification target

The candidate must pass package normal/race tests, `go vet`, module-policy
checks, and focused negative tests for invalid transitions, tenant isolation,
idempotency conflict/replay, cancellation, generation mismatch, malformed
input, and forbidden actuator imports. These results prove only this local
kernel boundary; they do not close `G-MANAGED-AGENT`, `G-DATA`, `G-AUTHORITY`,
`G-SECURITY`, or any release/production Gate.
