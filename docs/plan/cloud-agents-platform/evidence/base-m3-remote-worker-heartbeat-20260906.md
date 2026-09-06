# BASE-M3 RemoteWorker outbound heartbeat and Admin node health

## Source and scope

- Date: 2026-09-06, Asia/Shanghai.
- Branch: `codex/cloud-agents-platform-p0`.
- Pre-commit source HEAD: `4dd670b6a79edd8f7fc228f0cfbe69968601666f`; the worktree was dirty. Unrelated `.gitignore`, `CLAUDE.md`, `AGENTS.md`, `go.work.sum`, `docs/img.png` and existing plan-document changes were excluded from this slice.
- Scope: add the first real outbound RemoteWorker process, mTLS heartbeat/reconnect behavior, persisted version/capability/capacity and database-time node health, plus the corresponding Admin list/detail view. This is not generation-fenced command delivery or BASE-M3 completion.
- Product migration: `product-000065`; schema bundle digest `sha256:5f21a9925b71f7548de55426a96a3a359ea28f001185a30d7f02a77456113d7a`; manifest digest `sha256:629f997ddc5869374aa864ed660e3b5673320365ac1ef381845e5497ea720dc1`.

## Implemented authority and security boundary

- The heartbeat route is declared as OpenAPI `mutualTLS`. It accepts no User/Admin bearer or enrollment Secret. The verified leaf SPIFFE identity must match the exact tenant, project, enrollment and incarnation path before PostgreSQL checks the current certificate fingerprint, active state and expiry.
- PostgreSQL remains the lifecycle and liveness authority. The first accepted heartbeat establishes node generation/resource version `1`; later reports may advance only to a server-owned generation and cannot regress. Heartbeats persist canonical capabilities, capacity, Worker/OS/architecture/kernel versions, first connection, last heartbeat and a fixed 30-second expiry.
- Health is calculated with database time on every Admin list/detail read: under 10 seconds is `online`, 10–30 seconds is `degraded`, and expired heartbeat or certificate is `offline`. A certificate rotation to a different incarnation clears stale node status before the new incarnation connects.
- `cloud-agents-remote-worker` is an outbound-only Linux/macOS process. It opens no listener, requires TLS 1.3, an explicit server CA and the short-lived client keypair, sends every five seconds, and retries transport/API failures with a resettable 1–30 second backoff. Logs contain errors but not certificate, private-key or enrollment-Secret bytes.
- Admin Web uses the generated Admin API and displays server-returned node health, observed/desired generation, resource version, platform, capabilities, capacity and heartbeat timestamps in `en-US` and `zh-CN`. It does not infer health in the browser or receive endpoint, credential, certificate-chain, private-key, user-content or Workspace-content bytes.

## Reproducible checks

### Real PostgreSQL, HTTPS/mTLS and outbound process

```sh
bun scripts/test-foundation-product-migration.mjs
```

This passed against one ownership-labelled, network-disabled PostgreSQL `17.6 (Debian 17.6-2.pgdg12+1)` arm64 container:

- exact product-000064 to product-000065 upgrade, fresh 65-migration install and no-op replay;
- 65-row immutable migration ledger with the two expected upgrade-path bundle digests;
- a real TLS server with the RemoteWorker CA, generated bootstrap/Admin/node clients and PostgreSQL RLS/functions;
- a separately built Linux `cloud-agents-remote-worker` process presenting the issued client certificate and completing one real outbound heartbeat;
- repeated heartbeat through a new client connection, displaced/expired/revoked certificate 401 paths, wrong incarnation 401, and observed generation conflict 409;
- Admin detail reporting the persisted platform/capability/capacity values and database-time `online`, `degraded`, `offline`, then recovered `online` states;
- raw Admin responses without enrollment Secret/digest, certificate chain, private key or user content.

The harness verified its ownership label and removed only its disposable PostgreSQL container and anonymous volume. It did not start a customer command channel, external Controller, Sandbox, customer Workspace or deployment target.

The process reconnect loop also has a deterministic check proving one- and two-second failure backoff, reset to the five-second heartbeat interval after recovery. The real harness starts the process with `--once`; it does not claim a timed real-network outage/recovery run.

### Focused generation, code, web and security checks

The following passed on the dirty source tree:

```sh
go test ./services/control-plane/internal/remoteworker \
  ./services/control-plane/internal/store/postgres \
  ./services/control-plane/internal/server \
  ./services/control-plane/cmd/cloud-agents-control-plane \
  ./services/worker/cmd/cloud-agents-remote-worker
go test -tags=localdev ./services/control-plane/cmd/cloud-agents-control-plane
go vet ./services/control-plane/internal/remoteworker \
  ./services/control-plane/internal/store/postgres \
  ./services/control-plane/internal/server \
  ./services/control-plane/cmd/cloud-agents-control-plane \
  ./services/worker/cmd/cloud-agents-remote-worker
go vet -tags=localdev ./services/control-plane/cmd/cloud-agents-control-plane
go test ./sdk/go/gen/platform/v1alpha1
go test ./sdk/go/gen/openapi/v1alpha1 -run 'RemoteWorker(Heartbeat|MTLS|Bootstrap)'
bun run platform:migrations:check
bun scripts/generate-platform-json-sdks.ts --check
bun scripts/generate-foundation-migration-package.ts --check
bunx vitest run scripts/lib/platform-migration-sql.test.ts
bun run --cwd=sdk/typescript typecheck
bun run --cwd=sdk/typescript test
bun run --cwd=sdk/typescript build
bun run --cwd=apps/admin-web typecheck
bun run --cwd=apps/admin-web test
bun run --cwd=apps/admin-web build
bun run secret:scan
git diff --check
```

The focused migration-classifier suite ran 37 passing tests; the TypeScript SDK ran 49 passing tests; Admin Web ran 32 passing tests and built successfully. Vite retained its existing warning that the single minified JavaScript chunk exceeds 500 kB.

The umbrella checks were executed but are not claimed as passing. `platform:contracts:check` recorded the existing `EXECUTED_NONCONFORMANT` AJV audit, then stopped because the host has Bun `1.4.1` and uv `0.6.0` instead of pinned Bun `1.3.14` and uv `0.12.5`; Python `3.14.7` matched. `platform:sdk:check` passed identity and JSON generation, then stopped at Proto generation because host Go `1.27.1` differs from pinned `1.26.6`; `platform:go:check` stopped at the same Go mismatch. Direct affected checks above passed. The full generated Go OpenAPI client test package still has two pre-existing fixture failures: its NetworkPolicy fixture omits required `allowedEgress`, and its RuntimeProfile fixture omits required `networkPolicyRef`; the RemoteWorker-focused tests pass. This slice did not run the full Admin visual matrix.

## Evidence boundary and next item

This proves a real outbound process can authenticate and report through the generated contract into PostgreSQL, reconnect logic is executable, liveness is server-authoritative, Admin renders only the persisted safe node projection, and certificate/path/generation negative paths are enforced.

It does not prove outbound command delivery, command deadline/idempotency, node acknowledgement of a changed desired generation, Drain/Resume, customer-node Sandbox execution, a timed real-network outage recovery, Kubernetes deployment, publication or BASE-M3 completion. The next slice is generation-fenced command delivery with Admin-confirmed Drain/Resume and Audit.
