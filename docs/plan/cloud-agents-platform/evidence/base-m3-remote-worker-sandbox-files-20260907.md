# BASE-M3 RemoteWorker Sandbox Files

## Source and scope

- Date: 2026-09-07, Asia/Shanghai.
- Branch: `codex/cloud-agents-platform-p0`.
- Pre-commit source HEAD: `ea207889b94f3fff0a1b9df2f48e7b27cafe8f47`; the worktree was dirty. Unrelated `.gitignore`, `CLAUDE.md`, `AGENTS.md`, `go.work.sum`, `docs/img.png` and existing plan-document changes were excluded.
- Scope: keep the generated User Sandbox Files API unchanged while routing real RemoteWorker list/read/write/delete work through the existing short-lived Grant, PostgreSQL authority and authenticated outbound heartbeat.
- Product migration: `product-000072`; schema bundle digest `sha256:c7e08e81b463d04dd267438ac636811200586d5d84d8cb2e8d18799bd2c5faca`; product package digest `sha256:7a2e72f76e1c4e34085428e35e8bc8e33db3f016a36ce5404ff122743adc0e95`; manifest digest `sha256:56af03a65461e2009cf73c16ac2b1d74d856f68e3efc8b363ab84c537660c4d1`.

## Implemented authority and boundary

- The browser still calls only the Access Gateway. It receives no Target endpoint, runtime identity or credential reference and never connects directly to Docker or the RemoteWorker.
- The existing generation-bound Grant and activity authority resolves either the local Docker runtime or the exact active RemoteWorker target. RemoteWorker work is queued in owner-only PostgreSQL rows and delivered only to the current mTLS incarnation and certificate.
- Lifecycle commands retain priority over Exec, and Exec retains priority over Files. Files commands have a 65-second deadline, 16 MiB write limit, 1 MiB version-bound read pages and 1000-entry list limit.
- The customer-node process reuses its node-local OpenSandbox client for `/workspace`-relative list/read/write/delete. Paths are revalidated on the node; root is accepted only for list and symlink traversal fails closed.
- Settlement binds the receipt to the command action, path, read offset/limit/version or written entry metadata. Exact receipt replay is idempotent; changed or authority-mismatched receipts return `409`.
- The runtime database role cannot read queued paths or content. Existing Admin Grant diagnostics remain metadata-only: counts and stable errors, never file paths, names or bytes. No new Admin page or parallel DTO was needed.

## Reproducible checks

### Product migration and persisted authority

```sh
bun scripts/test-foundation-product-migration.mjs
```

This passed against disposable PostgreSQL `17.6 (Debian 17.6-2.pgdg12+1)` arm64: exact `000071` to `000072` upgrade, fresh 72-migration install and no-op replay. The harness removed only its owned database container and anonymous volume.

### Real outbound Files access

```sh
bun scripts/test-foundation-controller-docker.mjs \
  docs/plan/cloud-agents-platform/evidence/base-m3-remote-worker-sandbox-files-20260907 \
  --remote-worker-only
```

Run `foundation-controller-6041522b-4a69-4ea4-9652-486e265b9d9c` passed using OrbStack Docker `29.4.0`, disposable PostgreSQL 17.6, a real short-lived mTLS RemoteWorker process and a real OpenSandbox Docker runtime. It proved:

- a real 1,900,000-byte write crossed the former 2 MiB heartbeat response ceiling, then list and two version-bound read pages returned the exact bytes across an Access Gateway restart;
- wrong-token and cross-tenant requests returned `403`, symlink traversal returned `409`, and read after delete returned `404`;
- a schema-valid receipt for the wrong command path returned `409` before settlement; the correct receipt settled, exact replay succeeded, and a changed valid file version under the same identity returned `409`;
- receipt delivery remained bound to the current incarnation and certificate; the runtime role could not read the command/path/content table;
- ordinary user Admin Grant access returned `403`, while Admin diagnostics exposed only 7 attempts, 2 failures and a stable error without file names, paths or bytes;
- final Stop and scoped cleanup left zero test-owned OpenSandbox runtime containers and zero Foundation Workspace volumes.

Machine-readable facts and the worker log are in [base-m3-remote-worker-sandbox-files-20260907](base-m3-remote-worker-sandbox-files-20260907/).

### Code, generated SDK and static checks

These checks passed on the current worktree:

```sh
bun run platform:migrations:check
bun run typecheck
bunx vitest run sdk/typescript/src/platform.test.ts
bun run platform:sdk:consumers
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

The focused TypeScript generated-SDK suite passed 35 tests. The migration bundle and product package generators were current.

The repository-wide version-gated wrappers were not claimed as passing: this host has Bun 1.4.2 and uv 0.6.0 instead of pinned Bun 1.3.14 and uv 0.12.5, while the Go wrappers require 1.26.6 and the host has 1.27.1. `platform:contracts:check` stopped after its current AJV suite audit, `platform:sdk:check` stopped after the current identity/JSON generators at the proto Go version gate, and `platform:go:check` stopped at the same Go gate. The full generated OpenAPI package also retains two pre-existing unrelated stale fixture failures (`allowedEgress` and `INVALID_IDENTIFIER`); the affected HTTP heartbeat limit tests passed.

## Evidence boundary and next item

This proves reverse Files access on one real local Docker customer node with disposable PostgreSQL. It does not prove RemoteWorker PTY, Preview/SSH or other long-lived connections, active network interruption recovery, claim renewal beyond the bounded command deadline, Kubernetes customer nodes, deployment or publication.

The next BASE-M3 slice is RemoteWorker PTY over the outbound connection; Preview/SSH, long-connection renewal and reconnect reconciliation follow. BASE-M3 and the overall Goal remain incomplete.
