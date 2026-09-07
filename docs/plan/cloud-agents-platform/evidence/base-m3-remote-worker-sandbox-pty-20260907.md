# BASE-M3 RemoteWorker Sandbox PTY

## Source and scope

- Date: 2026-09-07, Asia/Shanghai.
- Branch: `codex/cloud-agents-platform-p0`.
- Pre-commit source HEAD: `ca36698b7bfcdb593eec9f585424165235b883a9`; the worktree was dirty. Unrelated `.gitignore`, `CLAUDE.md`, `AGENTS.md`, `go.work.sum`, `docs/img.png` and existing plan-document changes were excluded.
- Scope: keep the generated User PTY API and Access Gateway route unchanged while carrying a real RemoteWorker PTY through PostgreSQL authority and the existing outbound mTLS heartbeat.
- Product migration: `product-000073`; schema bundle digest `sha256:c4526daf950758886c286cabb097bc9223411ac0a573c67dcc4d3ca13ff20304`; manifest digest `sha256:421748bea039fd899ebd210bcac463d4ffdeee8b1713a4086d70e8269afa4343`.

## Implemented authority and boundary

- The browser still connects only to the Access Gateway with the existing generation-bound Grant and relative WebSocket path. It receives no Target endpoint, runtime identity or credential reference and never connects directly to the customer node.
- RemoteWorker `create`, `get`, `delete` and bounded `exchange` commands are owner-only PostgreSQL rows. Request and claim independently revalidate the active Grant, current Sandbox generation/runtime receipt, exact RemoteWorker Target, active node/certificate/heartbeat and `docker,pty` capabilities.
- Lifecycle, Exec and Files retain priority over PTY. Delivery and settlement bind command, Grant, Sandbox generation, Target, incarnation, certificate and action; exact receipt replay is idempotent and changed or mismatched receipts return `409`.
- The customer-node process persists one PTY command/receipt in its existing `0600` state file and connects only to its node-local OpenSandbox endpoint. One exchange carries at most one 64 KiB input frame and at most 1,052,672 output bytes/256 frames.
- Access Gateway terminates the user WebSocket, rechecks Grant authority on active connections and forwards only bounded frames through heartbeat commands. Absolute `since` and `takeover` values preserve the existing cursor/reconnect contract across Gateway restart.
- The runtime database role cannot read PTY command/frame rows. Existing Admin Grant diagnostics expose only PTY session count; no new Admin content API, DTO or page was added.

## Reproducible checks

### Product migration and persisted authority

```sh
bun scripts/test-foundation-product-migration.mjs
```

This passed against disposable PostgreSQL `17.6 (Debian 17.6-2.pgdg12+1)` arm64: exact `000072` to `000073` upgrade, fresh 73-migration install and no-op replay. The harness removed only its owned database container and anonymous volume.

### Real outbound PTY access

```sh
bun scripts/test-foundation-controller-docker.mjs \
  docs/plan/cloud-agents-platform/evidence/base-m3-remote-worker-sandbox-pty-20260907 \
  --remote-worker-only
```

Run `foundation-controller-e1d5a8aa-e451-4522-b8f1-ccf69f95856c` passed using OrbStack Docker `29.4.0`, disposable PostgreSQL 17.6, real short-lived mTLS RemoteWorker processes and a real OpenSandbox Docker runtime. It proved:

- the unchanged PTY API created a real `/workspace` shell and transferred two markers through Access Gateway, PostgreSQL, outbound heartbeat and the node-local OpenSandbox WebSocket;
- seven PTY commands settled successfully; Gateway restart preserved the session and reconnect from cursor zero returned replay framing before accepting new input;
- wrong-token and cross-tenant creation returned `403`; a schema-valid receipt with the wrong Grant and a changed exact-replay receipt returned `409`; access after PTY deletion returned the existing oracle-resistant `403`;
- all command delivery remained bound to the current incarnation and certificate, every receipt settled, and the runtime role could not read the frame table;
- Admin diagnostics exposed only one PTY session count and did not contain session ID, terminal markers or frame payloads;
- final Sandbox Stop and scoped cleanup left zero test-owned OpenSandbox runtime containers and zero Foundation Workspace volumes.

Machine-readable facts and the redacted OpenSandbox log are in [base-m3-remote-worker-sandbox-pty-20260907](base-m3-remote-worker-sandbox-pty-20260907/).

### Code, generated SDK and static checks

The following checks passed on the current worktree:

```sh
bun scripts/generate-platform-json-sdks.ts --check
bun run platform:migrations:check
bun run platform:sdk:consumers
bun run typecheck
bun run --cwd sdk/typescript test -- --run platform.test.ts
go test -race ./services/control-plane/cmd/cloud-agents-remote-worker \
  ./services/control-plane/internal/accessgateway \
  ./services/control-plane/internal/remoteworker \
  ./services/control-plane/internal/store/postgres \
  ./services/control-plane/internal/server \
  ./sdk/go/gen/platform/v1alpha1
go vet ./services/control-plane/cmd/cloud-agents-remote-worker \
  ./services/control-plane/internal/accessgateway \
  ./services/control-plane/internal/remoteworker \
  ./services/control-plane/internal/store/postgres \
  ./services/control-plane/internal/server \
  ./sdk/go/gen/platform/v1alpha1
node --check scripts/test-foundation-controller-docker.mjs
bun run secret:scan
git diff --check
```

The focused TypeScript generated-SDK suite passed 36 tests. The migration bundle and product package generators were current.

The repository-wide version-gated wrappers were not claimed as passing: this host has Bun 1.4.2 and uv 0.6.0 instead of pinned Bun 1.3.14 and uv 0.12.5, while the Go wrappers require 1.26.6 and the host has 1.27.1. The full generated OpenAPI package retains the pre-existing unrelated stale `allowedEgress` and `INVALID_IDENTIFIER` fixtures.

## Evidence boundary and next item

This proves bounded reverse PTY access and Gateway cursor reconnect on one real local Docker customer node with disposable PostgreSQL. It does not prove long-duration command-claim renewal, induced NAT interruption recovery, RemoteWorker Preview/SSH, Kubernetes customer nodes, deployment or publication.

The next BASE-M3 slice is RemoteWorker Preview, followed by SSH, long-connection claim renewal and reconnect reconciliation. BASE-M3 and the overall Goal remain incomplete.
