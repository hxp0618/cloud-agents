# BASE-M3 RemoteWorker DeploymentTarget projection

## Source and scope

- Date: 2026-09-06, Asia/Shanghai.
- Branch: `codex/cloud-agents-platform-p0`.
- Pre-commit source HEAD: `01aba9f23d933a7ae5df6a5e2a045b581883cb58`; the worktree was dirty. Unrelated `.gitignore`, `CLAUDE.md`, `AGENTS.md`, `go.work.sum`, `docs/img.png` and existing plan-document changes were excluded from this slice.
- Scope: project each RemoteWorker enrollment into a server-owned DeploymentTarget, derive its Admin readiness from authenticated heartbeat authority, expose the immutable target ID through generated SDKs, and keep the generic Target lifecycle from mutating RemoteWorker-owned state.
- Product migration: `product-000067`; schema bundle digest `sha256:495334403eafd64b3695e1162ecf9f25490fd8b9ff6342e47d70e60a6802a24b`; manifest digest `sha256:6e036d7073774fc3fcb3b8e79cb056b3b50336925b8fccfa672313c54b58d314`.

## Implemented authority and boundary

- PostgreSQL assigns each enrollment one deterministic `rwt-<sha256>` target ID and owns the matching `remote-worker` DeploymentTarget row. Existing enrollments are backfilled; create, certificate state, incarnation, desired generation and heartbeat facts update the same projection through one database trigger.
- The Admin projection derives heartbeat expiry and certificate expiry from the database clock. A fresh enrollment is `unprobed`, an authenticated heartbeat is `ready`, an expired heartbeat is `unavailable/remote-worker-offline`, reconnect restores `ready`, certificate rotation clears stale node facts to `unprobed`, and revocation is `unavailable/remote-worker-unavailable`.
- Admin cannot register `remote-worker` through the generic Target create contract. Generic Probe, Cleanup and Target Drain/Resume return `409`; those lifecycle controls stay on the RemoteWorker enrollment API. The Admin Web disables those generic buttons and links the projected target to its RemoteWorker placement ID.
- The target endpoint is the non-network identifier `remote-worker://<enrollment-id>` and `credentialRef` is the enrollment ID, not credential bytes. The browser still calls only the Control Plane Admin API. Ordinary user tokens receive `403` on the Admin target route.
- The migration statement profile was extended only for the exact 000067 constraint expansion, backfill, trigger and security-invoker Admin view hashes; arbitrary statements remain rejected.

## Reproducible checks

### Real PostgreSQL, HTTPS/mTLS and outbound process

```sh
GOTOOLCHAIN=go1.26.6 bun scripts/test-foundation-product-migration.mjs
```

This passed against one ownership-labelled, network-disabled PostgreSQL `17.6 (Debian 17.6-2.pgdg12+1)` arm64 container:

- exact product-000066 to product-000067 upgrade, fresh 67-migration install and no-op replay;
- a 67-row immutable migration ledger with both upgrade-path bundle digests;
- generated Admin/User clients and Control Plane HTTP authorization, including ordinary-user Target `403`;
- deterministic enrollment-to-target linkage and `unprobed`, heartbeat `ready`, database-clock `offline`, reconnect `ready`, rotation `unprobed` and revocation `unavailable` projections;
- generic Target Probe, Cleanup preview and scheduling preview all rejected with `409` before any generic Target operation was accepted;
- the existing real mTLS certificate, outbound RemoteWorker heartbeat, Drain/Resume receipt, restart, Audit and redaction chain remained passing.

The harness verified its ownership label and removed only its disposable PostgreSQL container and anonymous volume.

### Contracts, generated code and Admin Web

The following passed on the dirty source tree with Node `24.18.1`, Bun `1.3.14`, Go `1.26.6`, Python `3.14.7` and uv `0.12.5`:

```sh
bun run platform:contracts:check
bun run platform:migrations:check
bun run platform:sdk:check
go -C services/control-plane test \
  ./internal/deploymenttarget ./internal/remoteworker ./internal/store/postgres ./internal/server
go -C services/control-plane vet \
  ./internal/deploymenttarget ./internal/remoteworker ./internal/store/postgres ./internal/server
bun run --filter '@cloud-agents/cloud-agent-platform-sdk' typecheck
bun run --filter '@cloud-agents/cloud-agent-platform-sdk' test
bun run --filter '@cloud-agents/cloud-agent-platform-sdk' build
bun run --filter '@cloud-agents/admin-web' typecheck
bun run --filter '@cloud-agents/admin-web' test
bun run --filter '@cloud-agents/admin-web' build
bunx vitest run scripts/lib/platform-migration-sql.test.ts
bun run secret:scan
git diff --check
```

The TypeScript SDK ran 49 passing tests. Admin Web ran 33 passing tests and built successfully; Vite retained its existing warning that the single minified JavaScript chunk exceeds 500 kB. The contract standards check reported 184 schemas, 126 OpenAPI operations and all gates open while retaining its explicit non-Gate AJV audit label.

## Evidence boundary and next item

This proves a real server-owned placement target and Admin status authority for an outbound RemoteWorker. It does not prove that RuntimeProfile/Sandbox placement can yet dispatch a workload to that node, or that Exec/Files/Stop/Rebuild run there. No external customer node, NAT outage, Kubernetes deployment, image publication, Provider runtime or browser visual matrix was used.

The next BASE-M3 slice remains the outbound customer-node Workspace/Sandbox workload command and reconnect reconciliation path. Offline target admission and stale command handling must be proven there before BASE-M3 can be marked complete.
