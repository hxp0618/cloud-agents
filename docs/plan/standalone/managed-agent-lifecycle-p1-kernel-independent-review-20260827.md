# Standalone Platform v0.1 — Managed Agent lifecycle P1 kernel independent review

- Review ref: `managed-agent-lifecycle-p1-kernel-independent-review-20260827`
- Status: `APPROVE`
- Verdict: `P0=0`, `P1=0`, `P2=0`
- P0 parent: `16947a7db51d63ecce7f32153c88322d6e894854`
- Initial implementation candidate: `5b148be4f4d6335d6b1e50a12783b8a1e054f027`
- Fixed candidate: `007f2034321821ca361fe8d6b19c209ddf3494b6`
- Review branch: `codex/review-standalone-managed-agent-lifecycle-20260827`
- Review mode: independent, read-only candidate review. This commit adds only
  this review record; the fixed candidate implementation was not edited here.
- Gate effect: none; this review does not close or advance any Gate.

## Fixed identities

The candidate and its parent resolved to these exact Git identities before the
review record was written:

| Identity | Value |
| --- | --- |
| fixed candidate commit | `007f2034321821ca361fe8d6b19c209ddf3494b6` |
| fixed candidate tree | `5073c251895f26445b36a4303008e4590e0c35bf` |
| P0 parent tree | `94a89b4ee73d67f94ffc26df64ac5c7f650a0215` |
| binary diff SHA-256 (`git diff --binary parent..candidate`) | `bd5e4a586828919cec0e4df0fc6022259124fd25e9d92f4815c753a15f19d37c` |
| lifecycle state-machine profile digest | `sha256:e8e090658e6a0890fcd940cb6d713c7265e5eb38b0edab729c44de6556b9c66b` |

The fixed candidate changes exactly six paths:

1. `services/control-plane/internal/managedagent/lifecycle.go`
2. `services/control-plane/internal/managedagent/lifecycle_test.go`
3. `services/control-plane/internal/managedagent/profile.go`
4. `services/control-plane/internal/managedagent/surface_test.go`
5. `services/control-plane/internal/managedagent/README.md`
6. `docs/plan/standalone/managed-agent-lifecycle-p1-kernel-implementation-20260827.md`

No `contracts/`, `services/control-plane/migrations/`, generated SDK,
provider, Worker, deployment, release, or Gate path is in the diff. The
previously approved D-053-MIG-000014.r1/r2 objects remain byte-identical to
the P0 parent, including these representative Git blobs:

| D-053 object | Git blob in parent and candidate |
| --- | --- |
| `docs/plan/p1/durable-project-create-migration-runner-binding-r2-20260827.md` | `e67d4017313399de97dfda4e7fc2ec19137d4116` |
| `docs/plan/p1/durable-project-create-migration-runner-binding-independent-review-20260827.md` | `49f72dc285eb20934dbeba8077b69a4fc54e1291` |
| `services/control-plane/migrations/successor/000014/runner-binding/authority-source.json` | `d08443e2cc00e93b5ab2a9f6f59a8b63a5ca04e8` |
| `services/control-plane/migrations/successor/000014/runner-binding/profile.json` | `41721ae62b7b6d896546007fb706f5bf115c803b` |

## Scope and authority check

The fixed candidate is a transport-neutral, in-memory Control Plane kernel
under `cloud-agents/managed-agent-lifecycle/v1alpha1`. It binds Session → Turn
→ Execution with a checked-in transition table and verifies the profile digest
at construction and mutation boundaries. The reviewed edges are:

- Session `active → closed` on `close` after every turn is terminal;
- Turn `queued → running` on `start_execution`;
- Turn `running → completed|failed|interrupted|cancelled` on the corresponding
  execution, interrupt, or cancel event; and
- Execution `queued → running → succeeded|failed|cancelled`, with distinct
  `interrupt` and `cancel` event identities.

The store enforces tenant/project-qualified parent keys, one foreground
non-terminal turn, one execution attempt, exact generation and parent binding,
terminal immutability, bounded ASCII identifiers plus bounded UTF-8 tokens, and
UTF-8/size checks,
context checks, and detached snapshots. Turn input is represented only by a
SHA-256 digest. Idempotency keys are scoped by tenant/project and operation;
the digest is derived internally from a typed projection, so callers cannot
provide a request digest or select a state. Same-key equal requests replay the
detached result and payload drift returns `ErrIdempotencyConflict`.

The package has no public route and no imports or calls for PostgreSQL,
`net/http`, Worker/Supervisor dispatch, Provider execution, Workspace,
Artifact, Credential, deployment, or release. The surface test and the source
review confirm that this boundary is not an accidental production actuator.

## First finding and same-slice repair

The first read-only pass over `5b148be` found one P1 state-machine binding
error. `InterruptTurn` checked the Execution `cancel` edge while
`CancelTurn` checked the Execution `interrupt` edge. Both paths happened to
produce the same terminal state, so ordinary outcome tests did not expose the
identity inversion; a profile/table change could therefore make the wrong
operation admissible.

Commit `007f203` repairs the two selectors to their exact operation events and
adds `TestLifecycleInterruptAndCancelKeepDistinctExecutionEdges`. The fixed
candidate was then reread line by line and retested. No remaining P0, P1, or
P2 finding was identified inside the declared in-memory kernel boundary.

The absence of a generated public Managed Agent schema/SDK, durable
PostgreSQL state, authn/authz binding, event cursor, retry/recovery writer, and
cross-language RFC 8785 profile is intentional scope for this local seam, is
already stated in the implementation record/README, and is not promoted to a
finding. Those capabilities require their own approved authority and review.

## Independent verification

Commands were run from the clean review worktree using Go `1.26.6`
darwin/arm64. The repository's available Bun was `1.4.0`; it was used only for
the existing module-policy test. No live or production database was contacted.

| Evidence class | Command / observation | Result |
| --- | --- | --- |
| lifecycle normal | `GOWORK=off GOFLAGS=-mod=readonly go test ./internal/managedagent -count=1 -timeout=5m` | PASS |
| lifecycle race | same package with `-race` | PASS |
| lifecycle repeated | normal `-count=20 -shuffle=on`; race `-count=5 -shuffle=on` | PASS |
| lifecycle coverage | `go test ... -cover` | PASS, 79.4% |
| static | `go vet ./internal/managedagent` | PASS |
| module closure | `go mod tidy -diff` | PASS, empty diff |
| focused Control Plane packages | `managedagent coordination server authn authz store/postgres` normal suite | PASS |
| cross-platform compile | Linux amd64 and arm64, `CGO_ENABLED=0`, test compile only | PASS |
| module policy | `bunx vitest run scripts/lib/platform-go-modules.test.ts --reporter=dot` | PASS, 13/13 |
| source boundary | AST import test, dependency listing, forbidden actuator search | PASS; no production actuator import |
| formatting | `gofmt -d` on changed Go and candidate-range `git diff --check` | PASS |
| referenced authority paths | implementation record/README source paths exist | PASS |

The one broad Control Plane invocation was intentionally not converted into a
green claim. `go test ./... -count=1 -timeout=30m` passed the new package and
the other unaffected packages, but the pre-existing `internal/migration`
package reported quota/evidence failures (including
`MIGRATION_EVIDENCE_JOURNAL_LIMIT_EXCEEDED` and recovery-evidence mismatch)
and reached its 30-minute timeout. The candidate changes no migration path;
this result is recorded as `NOT PASS / unrelated current-source baseline`, not
as candidate evidence and not as a reason to repeat the long migration run.

The fixed-candidate HEAD-only Gitleaks `8.30.1` scan found no leaks. A
candidate-range scan also reported two `generic-api-key` matches in the
initial commit's deterministic `idem-turn-busy-1` / `idem-turn-busy-2` test
placeholders. They are non-secret local literals with no credential source or
runtime use; the findings are classified as false-positive fixture matches,
not hidden or silently discarded.

## Non-claims and verdict

This review did not modify the fixed implementation, use SSH or remote hosts,
open a listener, use a live PostgreSQL instance, write production data, invoke
a Provider, dispatch a Worker, deploy, publish, release, or close a Gate. It
does not authorize public/canonical Runner behavior, production database
writes, HTTP/P2/provider effects, durable retry/recovery, event publication,
or the full Managed Agent E2E Gate.

The verdict is **APPROVE — P0=0, P1=0, P2=0** for fixed candidate
`007f2034321821ca361fe8d6b19c209ddf3494b6` only. The approval covers the
versioned local lifecycle kernel, its fail-closed checks, and the repaired
interrupt/cancel event binding. It does not create r3/r4 scope and does not
alter D-053-MIG-000014.r1/r2 or any Gate state.
