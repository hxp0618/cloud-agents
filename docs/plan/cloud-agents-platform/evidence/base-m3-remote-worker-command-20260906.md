# BASE-M3 RemoteWorker scheduling command and Admin Drain/Resume

## Source and scope

- Date: 2026-09-06, Asia/Shanghai.
- Branch: `codex/cloud-agents-platform-p0`.
- Pre-commit source HEAD: `0ecfa13923ae2e56f1e042990d6c77e3f25ac6cb`; the worktree was dirty. Unrelated `.gitignore`, `CLAUDE.md`, `AGENTS.md`, `go.work.sum`, `docs/img.png` and existing plan-document changes were excluded from this slice.
- Scope: add server-owned RemoteWorker Drain/Resume desired generation, outbound heartbeat command delivery, durable node receipt state, command deadline, exact idempotency, Operation/Audit closure and the corresponding Admin confirmation flow.
- Product migration: `product-000066`; schema bundle digest `sha256:1dafedc5a58b35ddf9b20e4e7b1334818ae9d6254013ca5f2a9a626ec4a1dbf3`; manifest digest `sha256:9c370cd54be65d3cbd4dd86b338b0a454abed738079d8f26951f073fd0ac80a5`.

## Implemented authority and security boundary

- Control Plane exposes generated Admin API operations for server-authoritative scheduling preview, generation/resource-version-fenced transition and per-enrollment Operation listing. A transition requires the exact enrollment ID, preview digest and idempotency key; ordinary user tokens receive `403`, with a durable denied-write event.
- PostgreSQL atomically advances the desired generation, persists the current command and appends requested Operation/Audit events. Heartbeat receipt settlement succeeds or fails that same Operation and appends the terminal Audit result. Exact request and receipt replay return the prior result; conflicting generation, request digest or receipt fail closed.
- The mTLS heartbeat response carries at most the current unexpired command. The RemoteWorker applies only `active` or `drained`, stores incarnation/observed generation/state/last command/receipt in a `0600` state file using temp-file sync, rename and directory sync, and reports the receipt on the next heartbeat. Restart and duplicate delivery do not advance the generation twice.
- A command expires after 30 seconds. An expired command produces a failed receipt with stable code `remote-worker-command-expired`; it does not change the local observed generation.
- Admin Web uses the generated Admin API for preview, exact enrollment confirmation, submit and durable Operation/Audit status. It does not receive node certificate, enrollment Secret, endpoint, credential, Workspace, Artifact or user-content bytes.
- The shared durable-coordination operation discovery now recognizes the existing RemoteWorker one-time Secret, CSR exchange and certificate exact-replay annotations as typed owners, while unknown annotations still fail closed.

## Reproducible checks

### Real PostgreSQL, HTTPS/mTLS and outbound processes

```sh
node scripts/test-foundation-product-migration.mjs
```

This passed against one ownership-labelled, network-disabled PostgreSQL `17.6 (Debian 17.6-2.pgdg12+1)` arm64 container:

- exact product-000065 to product-000066 upgrade, fresh 66-migration install and no-op replay;
- a 66-row immutable migration ledger with the two upgrade-path bundle digests;
- real generated Admin/User clients and Control Plane HTTP authorization, including ordinary-user scheduling `403` and one durable denied-write event;
- a separately built RemoteWorker process receiving Drain over mTLS, persisting state, restarting with the same state file and returning the command receipt;
- server-owned generation/resource-version fencing, exact transition replay and receipt replay;
- Resume delivery, conflicting receipt rejection, a database-time-expired Drain receipt and exact failed-receipt replay, failed Operation and terminal Audit closure;
- Admin projections without Secret, certificate, endpoint, credential or user-content bytes.

The harness verified its ownership label and removed only its disposable PostgreSQL container and anonymous volume.

### Generated contracts, code and web

The following passed on the dirty source tree:

```sh
GOTOOLCHAIN=go1.26.6 GOFLAGS=-mod=readonly go -C services/control-plane test \
  ./internal/remoteworker ./internal/server ./internal/store/postgres
GOTOOLCHAIN=go1.26.6 GOFLAGS=-mod=readonly go -C services/worker test ./...
GOTOOLCHAIN=go1.26.6 GOFLAGS=-mod=readonly go -C sdk/go test \
  ./gen/openapi/v1alpha1 -run 'TestRemoteWorkerHeartbeatUsesOnlyMTLSTransportAuthority|TestGeneratedOpenAPIClient.*RemoteWorker' -count=1
GOTOOLCHAIN=go1.26.6 GOFLAGS=-mod=readonly go -C services/control-plane vet \
  ./internal/remoteworker ./internal/server ./internal/store/postgres
GOTOOLCHAIN=go1.26.6 GOFLAGS=-mod=readonly go -C services/worker vet \
  ./cmd/cloud-agents-remote-worker
GOTOOLCHAIN=go1.26.6 GOFLAGS=-mod=readonly go -C sdk/go vet \
  ./gen/openapi/v1alpha1 ./gen/platform/v1alpha1
bun run platform:migrations:check
bun run --cwd=sdk/typescript typecheck
bun run --cwd=sdk/typescript test
bun run --cwd=sdk/typescript build
bun run --cwd=apps/admin-web typecheck
bun run --cwd=apps/admin-web test
bun run --cwd=apps/admin-web build
bun run secret:scan
git diff --check
```

The TypeScript SDK ran 49 passing tests. Admin Web ran 33 passing tests and built successfully; Vite retained its existing warning that the single minified JavaScript chunk exceeds 500 kB.

The full contract suite also passed with the repository tuple Node `24.18.1`, Bun `1.3.14`, Go `1.26.6`, Python `3.14.7` and uv `0.12.5`, including OpenAPI/JSON Schema standards, all generated coordination registries and exact Proto/JSON SDK checks. Its AJV official-suite audit remains explicitly `EXECUTED_NONCONFORMANT non-Gate`, as defined by the existing checker.

The Control Plane full `go test ./...` is not claimed: the unrelated historical `internal/migration` package exceeded its current evidence quota fixtures and timed out under the ambient Go `1.27.1`; the affected RemoteWorker/server/store packages passed under the pinned Go version. The full generated Go OpenAPI package also retains two unrelated fixture failures for NetworkPolicy and RuntimeProfile; the changed RemoteWorker tests passed. This slice did not run the Admin browser visual matrix.

## Evidence boundary and next item

This proves the generation-fenced Drain/Resume command path from Admin intent through PostgreSQL, HTTPS/mTLS delivery, durable RemoteWorker restart state, receipt settlement and Operation/Audit projection.

Drain/Resume here changes only node scheduling state. It does not yet prove that an outbound-only customer node can create, Exec/Files, stop or rebuild a Workspace/Sandbox, that offline nodes stop new placement, or that reconnect performs workload reconciliation without replaying stale commands. No external customer node, NAT outage, Kubernetes deployment, image publication or release was used. The next BASE-M3 slice is the customer-node Workspace/Sandbox command and reconnect reconciliation path.
