# BASE-M3 RemoteWorker Sandbox Preview

## Source and scope

- Date: 2026-09-07, Asia/Shanghai.
- Branch: `codex/cloud-agents-platform-p0`.
- Pre-commit source HEAD: `add0e2fd7f30c3730d18858ec4536320de82496c`; the worktree was dirty. Unrelated `.gitignore`, `CLAUDE.md`, `AGENTS.md`, `go.work.sum`, `docs/img.png` and existing plan-document changes were excluded.
- Scope: keep the generated User Preview registration/revoke/proxy API unchanged while carrying bounded HTTP requests to a real RemoteWorker Sandbox through PostgreSQL authority and the existing outbound mTLS heartbeat.
- Product migration: `product-000074`; schema bundle digest `sha256:e6c4dcec05174098e23a16638c5c9c5ef856b8b4940d6d520a790c4da063e5c3`; manifest digest `sha256:11dc5681047b3cc35798dd60a86b715f8a1af6bdf0db50b08734aa1a525af5e4`.

## Implemented authority and boundary

- The browser still uses only the Access Gateway, generation-bound Grant and relative Preview path. It receives no Target endpoint, runtime identity or credential reference and never connects directly to the customer node.
- RemoteWorker Preview uses an owner-only PostgreSQL command/content row. Request and claim independently revalidate the active Grant, registered port, Network Policy, current Sandbox generation/runtime receipt, exact RemoteWorker Target and active node/certificate/heartbeat.
- Lifecycle, Exec, Files and PTY retain priority over Preview. Delivery and settlement bind command, Grant, Sandbox generation, Target, incarnation, certificate and port; exact receipt replay is idempotent and changed or mismatched receipts return `409`.
- The customer-node process persists one Preview command/receipt in its existing `0600` state file and proxies only through its node-local OpenSandbox server proxy. Request bodies are limited to 1 MiB, responses to 4 MiB and command lifetime to 65 seconds.
- Credentials, cookies, forwarding and hop-by-hop headers are stripped in the Gateway and again on the node. Response `Set-Cookie` is removed, and duplicate proxy response fields are reduced to the contract's canonical unique list.
- The runtime database role cannot read Preview request/response rows. Existing Admin Grant diagnostics expose only registered port numbers; no Admin content API, DTO or page was added.

## Reproducible checks

### Product migration and persisted authority

```sh
bun scripts/test-foundation-product-migration.mjs
```

This passed against disposable PostgreSQL `17.6 (Debian 17.6-2.pgdg12+1)` arm64: exact `000073` to `000074` upgrade, fresh 74-migration install and no-op replay. The harness removed only its owned database container and anonymous volume.

### Real outbound Preview access

```sh
bun scripts/test-foundation-controller-docker.mjs \
  docs/plan/cloud-agents-platform/evidence/base-m3-remote-worker-sandbox-preview-20260907 \
  --remote-worker-only
```

Run `foundation-controller-c00844f3-eaeb-4b5e-9611-a8de4c4ed8b1` passed using OrbStack Docker `29.4.0`, disposable PostgreSQL 17.6, real short-lived mTLS RemoteWorker processes and a real OpenSandbox Docker runtime. It proved:

- a real Node HTTP server on port 3000 received the original POST method, `/hello?value=alpha`, request body and public header through Access Gateway, PostgreSQL, outbound heartbeat and node-local OpenSandbox;
- authorization, proxy authorization, cookie, forwarding and OpenSandbox credentials did not reach the Sandbox, and `Set-Cookie` did not reach the browser;
- two Preview commands settled successfully; Gateway restart preserved access, exact receipt replay succeeded and a changed receipt returned `409`;
- wrong token and cross-tenant access returned `403`; unregistered, internal and revoked ports returned `404`; an authority-mismatched receipt returned `409`;
- every command remained bound to the current incarnation and certificate, the runtime role could not read Preview content rows, and Admin output contained only port metadata without request/response content;
- final Sandbox Stop and scoped cleanup left zero test-owned OpenSandbox runtime containers and zero Foundation Workspace volumes.

Machine-readable facts and the redacted OpenSandbox log are in [base-m3-remote-worker-sandbox-preview-20260907](base-m3-remote-worker-sandbox-preview-20260907/).

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
  ./services/control-plane/internal/opensandbox \
  ./services/control-plane/internal/remoteworker \
  ./services/control-plane/internal/store/postgres \
  ./services/control-plane/internal/server \
  ./sdk/go/gen/platform/v1alpha1
go vet ./services/control-plane/cmd/cloud-agents-remote-worker \
  ./services/control-plane/internal/accessgateway \
  ./services/control-plane/internal/opensandbox \
  ./services/control-plane/internal/remoteworker \
  ./services/control-plane/internal/store/postgres \
  ./services/control-plane/internal/server \
  ./sdk/go/gen/platform/v1alpha1
node --check scripts/test-foundation-controller-docker.mjs
bun run secret:scan
git diff --check
```

The focused TypeScript generated-SDK suite passed 37 tests. The migration bundle and product package generators were current, and fresh TypeScript/Go SDK consumer builds passed.

The repository-wide version-gated wrappers were not claimed as passing: this host has Bun 1.4.2 and uv 0.6.0 instead of pinned Bun 1.3.14 and uv 0.12.5, while the Go wrappers require 1.26.6 and the host has 1.27.1. The full generated OpenAPI package retains the pre-existing unrelated stale `allowedEgress` and `INVALID_IDENTIFIER` fixtures.

## Evidence boundary and next item

This proves bounded request/response Preview access on one real local Docker customer node with disposable PostgreSQL. It does not prove streaming Preview, long-duration command-claim renewal, induced NAT interruption recovery, RemoteWorker SSH, Kubernetes customer nodes, deployment or publication.

The next BASE-M3 slice is RemoteWorker SSH, followed by long-connection claim renewal and reconnect reconciliation. BASE-M3 and the overall Goal remain incomplete.
