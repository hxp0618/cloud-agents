# Standalone Platform v0.1 — Managed Agent lifecycle event/cursor P1 kernel independent review

- Review ref: `managed-agent-event-journal-p1-kernel-independent-review-20260827`
- Reviewer: independent read-only review process
- Candidate branch: `codex/standalone-managed-agent-event-journal-20260827`
- Fixed candidate commit: `bdbeb9ea29cb11e08c39e704ac11c77a67f881f4`
- Fixed candidate tree: `9e99631ba159dbf4be86b172b3ed40885b1ec85f`
- Parent P0 commit: `8679b7ee554298a63c243b49087cad164e9c1c5f`
- Candidate binary diff SHA-256 against P0:
  `sha256:378137c7c50102daa5e00d633efda4204d02e4b8901b0a02e6134665df4e5fac`
- Review worktree: clean before adding this record

## Scope reviewed

The fixed candidate adds one local event/cursor projection to the approved
transport-neutral Managed Agent lifecycle kernel:

- `services/control-plane/internal/managedagent/events.go` defines the frozen
  event profile, typed state-change/event records, scope-bound cursor, paging
  reader, detached-copy behavior, and deterministic operation projection;
- `services/control-plane/internal/managedagent/events_test.go` covers normal,
  negative, isolation, replay, cancellation, and non-retention cases;
- `lifecycle.go`, `surface_test.go`, and the package README wire the projection
  into successful mutations and document its boundary; and
- the standalone implementation record binds the candidate identity and
  non-claims.

The reviewed diff contains no change to D-053-MIG-000014.r1/r2 source,
profile, schema, manifest, SQL, catalog, archive, or review bytes; D-053-EC-2;
public generated contracts/SDKs; or any deployment/configuration authority.

## Authority and isolation findings

1. `cloud-agents/managed-agent-events/v1alpha1` freezes the exact field
   projection, nine-operation order, cursor algorithm, page bound, and digest.
   The digest was independently recomputed from the checked-in ID, algorithm,
   fields, and operation sequence and matched
   `sha256:e38816e4df5b8aff6338537283f7eb7f9757aef9333b9a4e464dcec365a913b4`.
2. The sequence is assigned only while the existing store mutex is held after
   a successful mutation. Idempotency replay returns the stored mutation and
   does not append another event. Failed transitions and cancelled contexts do
   not advance the sequence.
3. Every non-zero cursor is checked for exact scope, sequence, event ID,
   profile ID, and profile digest, and the referenced event must belong to the
   requested scope. Foreign, partial, future, profile-drifted, and unknown
   sequence cursors fail closed. Results and nested state-change slices are
   detached before returning.
4. Events contain only resource IDs, state edges, timestamps, and digests. Raw
   turn input, provider credentials, and response bodies are not stored.
5. The package import and dependency closure contains no PostgreSQL, HTTP,
   Worker, Provider, process, or network actuator. The implementation remains
   in-memory and does not create a durable log, public watch route, or writer.

## Verification evidence

| Evidence class | Command / check | Result |
| --- | --- | --- |
| Unit | `GOWORK=off GOFLAGS=-mod=readonly go test ./internal/managedagent -count=1 -timeout=5m` | PASS |
| Race | `GOWORK=off GOFLAGS=-mod=readonly go test -race ./internal/managedagent -count=1 -timeout=5m` | PASS |
| Shuffle | `GOWORK=off GOFLAGS=-mod=readonly go test ./internal/managedagent -count=3 -shuffle=on -timeout=5m` | PASS |
| Static | `GOWORK=off GOFLAGS=-mod=readonly go vet ./internal/managedagent` | PASS |
| Focused event matrix | `go test ./internal/managedagent -run '^TestLifecycleEvents' -count=1 -v -cover` | PASS; 58.8% package coverage for the selected run |
| Consumer compile | `GOWORK=off GOFLAGS=-mod=readonly go test ./internal/server ./internal/modpolicy -count=1 -timeout=5m` | PASS |
| Module compile closure | `GOWORK=off GOFLAGS=-mod=readonly go test ./... -run '^$' -count=1 -timeout=10m` | PASS |
| Dependency boundary | `go list -deps ./internal/managedagent` plus forbidden-actuator scan | PASS |
| File/lineage boundary | stable regular-file check, `git diff --check`, exact r1/r2 path comparison | PASS |

No broad migration suite was rerun: this candidate does not modify its
inputs, and repeating that unrelated long-running command would provide no
additional evidence for this local event slice.

## Verdict

**APPROVE — P0=0, P1=0, P2=0.**

This verdict approves only the fixed local event/cursor kernel at the exact
commit/tree above. It does not authorize a PostgreSQL event writer, durable
cursor/retention or recovery implementation, HTTP watch endpoint, Worker or
Provider execution, production database access, deployment, publication,
release, or any Gate transition/closure. It does not supersede or alter
D-053-MIG-000014.r2 or D-053-EC-2.

Any change to the event fields, operation vocabulary, cursor algorithm, or
candidate bytes requires a new fixed candidate and a fresh independent review.
