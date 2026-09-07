# BASE-M3 RemoteWorker Sandbox Exec

## Source and scope

- Date: 2026-09-07, Asia/Shanghai.
- Branch: `codex/cloud-agents-platform-p0`.
- Pre-commit source HEAD: `774b790ef349fa97fa459c24e63485cdb27783e5`; the worktree was dirty. Unrelated `.gitignore`, `CLAUDE.md`, `AGENTS.md`, `go.work.sum`, `docs/img.png` and existing plan-document changes were excluded.
- Scope: preserve the existing generated User Sandbox Exec contract while routing a RemoteWorker Sandbox command over the authenticated outbound heartbeat, executing it through node-local OpenSandbox authority and returning one bounded result.
- Product migration: `product-000071`; schema bundle digest `sha256:3b1bb4eb61ab662986440395d8c2858f38d9948a0efd1a9612014ccd199bd8b7`; manifest digest `sha256:961df6c9d3588b1d123f0cbda6e42204ad1b6ad91e726ce50d6c339ba0eb3b03`.

## Implemented authority and boundary

- The public User API remains generation-bound and does not accept a Target, endpoint, credential reference or runtime identity. Admin scope remains forbidden from invoking Exec.
- PostgreSQL authorizes the exact tenant/project/Sandbox generation and stores the command, delivery assignment and bounded receipt under forced owner-only RLS. The runtime database role has no table `SELECT` privilege.
- The current active mTLS incarnation claims one command through the existing heartbeat. Delivery is bound to that incarnation and certificate fingerprint; lifecycle commands retain priority.
- The RemoteWorker persists the command and receipt in its existing `0600` state file, then reuses node-local Docker/OpenSandbox configuration to execute the command in `/workspace`.
- Exact request and receipt replay are idempotent; a changed request or changed receipt under the same identity is rejected with `409`. A non-zero shell exit is a successful transport result.
- No new Admin UI was needed: Exec remains a User operation, while Admin receives neither command nor output content.

## Reproducible checks

### Product migration and persisted authority

```sh
GOTOOLCHAIN=go1.26.6 bun scripts/test-foundation-product-migration.mjs
```

This passed against disposable PostgreSQL `17.6 (Debian 17.6-2.pgdg12+1)` arm64: exact product-000070 to product-000071 upgrade, fresh 71-migration install, no-op replay and a 71-row immutable ledger. The harness removed only its owned database container and volume.

### Real outbound Exec

```sh
remote_exec_output=$(mktemp -d /tmp/cloud-agents-base-m3-exec.XXXXXX)
PATH=/Users/huang/devel/soft/nvm/versions/node/v24.18.1/bin:$PATH \
  GOTOOLCHAIN=go1.26.6 \
  bunx bun@1.3.14 scripts/test-foundation-controller-docker.mjs \
  "$remote_exec_output/evidence" --remote-worker-only
```

The exact current script passed at `/tmp/cloud-agents-remote-exec-current.qqWIl6/evidence-8/evidence.json` using OrbStack Docker `29.4.0`, disposable PostgreSQL 17.6, a real short-lived mTLS RemoteWorker process and a real OpenSandbox Docker runtime. It proved:

- command `rwexec-RMUJHXLLD5ZBQIVQTWQTBDZ22D` ran in the rebuilt Sandbox and returned exit `7`, stderr `remote-exec-stderr` and the exact retained Workspace SHA-256 `64369e3856843eaad19d590ae5eba5153c8683a49002eb4b634ae2ba599e13d0`;
- stale generation returned `409` and Admin invocation returned `403`;
- exact User request and worker receipt replay succeeded while changed replay returned `409`;
- the settled row remained bound to the current incarnation and mTLS certificate, and the runtime role could not read the command/output table;
- final Stop and scoped cleanup left zero OpenSandbox runtime containers and zero Foundation Workspace volumes.

Compact facts are in [base-m3-remote-worker-sandbox-exec-20260907.json](base-m3-remote-worker-sandbox-exec-20260907.json).

### Contracts, SDK, code and Admin Web

These checks passed with Node 24.18.1, Bun 1.3.14, Go 1.26.6, Python 3.14.7 and uv 0.12.5:

```sh
bun run platform:contracts:check
bun run platform:migrations:check
go test ./sdk/go/gen/platform/v1alpha1 \
  ./services/control-plane/cmd/cloud-agents-remote-worker \
  ./services/control-plane/internal/foundationcontroller \
  ./services/control-plane/internal/remoteworker \
  ./services/control-plane/internal/store/postgres \
  ./services/control-plane/internal/server
go vet ./services/control-plane/cmd/cloud-agents-remote-worker \
  ./services/control-plane/internal/foundationcontroller \
  ./services/control-plane/internal/remoteworker \
  ./services/control-plane/internal/store/postgres \
  ./services/control-plane/internal/server
bunx vitest run scripts/lib/platform-migration-sql.test.ts scripts/lib/platform-json-sdk.test.ts
bun run --filter '@cloud-agents/cloud-agent-platform-sdk' typecheck
bun run --filter '@cloud-agents/cloud-agent-platform-sdk' test
bun run --filter '@cloud-agents/cloud-agent-platform-sdk' build
bun run --filter '@cloud-agents/admin-web' typecheck
bun run --filter '@cloud-agents/admin-web' test
bun run --filter '@cloud-agents/admin-web' build
bunx oxlint scripts/test-foundation-controller-docker.mjs sdk/typescript/src/platform.ts sdk/typescript/src/platform.test.ts --deny-warnings
bun run secret:scan
git diff --check
```

Contract validation reported 188 schemas and 126 OpenAPI operations; all generators were current. The TypeScript SDK ran 51 tests, Admin Web ran 33 tests and built with its existing over-500-kB chunk warning, and the focused script suites ran 41 tests.

## Evidence boundary and next item

Two broader harness attempts without `--remote-worker-only` passed this RemoteWorker section and cleanup, then the pre-existing later `TestLiveFoundationControllerRestart` path failed to advance a rebuilt local Docker Sandbox beyond `reconciling/unknown` (one immediate observation and one 120-second wait, while OpenSandbox continued returning HTTP 200). The broad controller harness is therefore not claimed as passing in this slice.

This proves bounded reverse Exec on one real local Docker customer node. It does not prove reverse Files, PTY or long-lived connections, claim renewal beyond the command deadline, active NAT/network interruption reconciliation, Kubernetes customer nodes, deployment, publication or browser visual QA.

The next BASE-M3 slice is reverse Files access; connection recovery and long-operation claim renewal follow. BASE-M3 and the overall Goal remain incomplete.
