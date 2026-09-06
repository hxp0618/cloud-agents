# BASE-M3 RemoteWorker enrollment authority and Admin management

## Source and scope

- Date: 2026-09-06, Asia/Shanghai.
- Branch: `codex/cloud-agents-platform-p0`.
- Pre-commit source HEAD: `d68b489d6017c9761ae790f9a94e033816b50ebc`; the worktree was dirty. Unrelated `.gitignore`, `go.work.sum`, `AGENTS.md`, `CLAUDE.md`, `docs/img.png` and plan-document changes were excluded from this slice.
- Scope: persist a short-lived RemoteWorker enrollment intent, expose redacted Admin create/list/detail/revoke/audit APIs and Admin Web, and let a separately scoped CLI bootstrap token claim one non-replayable secret. This is the first BASE-M3 slice, not phase completion.
- Product migration: `product-000062`, digest `sha256:7f564957725c2ac879cde68ef528d4e03cc6057dd34874325ee2fbb51b039aa0`.

## Implemented authority and security boundary

- PostgreSQL owns enrollment state, expiry, resource version, idempotency and append-only activity. The table stores only a SHA-256 digest of the 256-bit random `carw1_` bootstrap secret; plaintext exists only in the successful claim request and response process memory.
- Admin routes require `remote-worker-enrollments.{list,get,create,act}` and return metadata/audit only. An ordinary user token receives 403 on Admin create. The Admin token receives 403 from the bootstrap claim route.
- The bootstrap route requires the separate `remote-worker-bootstrap.act` scope, returns `Cache-Control: no-store` and `Pragma: no-cache`, and refuses every replay with 409 rather than returning the secret twice. Its token cannot list Admin enrollment resources.
- The generated Go and TypeScript SDKs enforce path/body ownership and the secret response cache policy. `cloud-agentsctl remote-worker-enrollment claim-secret` is the only UI supplied for secret retrieval; Admin Web neither includes that SDK method nor receives, displays or stores the secret.
- Admin Web uses the generated SDK for create/list/detail/revoke/audit, exact enrollment-ID confirmation, resource-version fencing and fresh idempotency keys. The page labels the CLI-only secret boundary in both `zh-CN` and `en-US`.
- The local development launcher writes the user, Admin and bootstrap tokens to three distinct mode-0600 files and refreshes them independently.

## Reproducible checks

### Real PostgreSQL migration, HTTP and generated-client closure

```sh
node scripts/test-foundation-product-migration.mjs
```

This passed against an owned, network-disabled PostgreSQL `17.6 (Debian 17.6-2.pgdg12+1)` arm64 container:

- exact `product-000061` to `product-000062` upgrade, fresh 62-migration install and no-op replay;
- 62-row immutable ledger, two upgrade-path bundle digests and the two RemoteWorker enrollment tables;
- generated Admin/bootstrap Go clients over in-process Control Plane HTTP;
- create and revoke idempotent replay, resource versions `1 -> 2 -> 3`, and three durable create/claim/revoke audit events;
- ordinary-user Admin 403, Admin bootstrap-claim 403 and bootstrap Admin-read denial;
- one successful no-store claim, replay 409, redacted Admin projection, and database equality to the secret digest with no plaintext in enrollment/activity rows.

The script verified its unique ownership label, then removed only its disposable PostgreSQL container and anonymous volume. The existing ready Deployment Target remained a SQL fixture; no Controller, Sandbox or customer node was started.

### Focused code, generation and web checks

The following passed on the dirty source tree:

```sh
go test ./services/control-plane/internal/remoteworker \
  ./services/control-plane/internal/store/postgres \
  ./services/control-plane/internal/server \
  ./services/control-plane/internal/authn \
  ./services/control-plane/cmd/cloud-agents-control-plane \
  ./services/control-plane/cmd/cloud-agentsctl \
  ./services/control-plane/internal/localmigration
go vet ./services/control-plane/internal/remoteworker \
  ./services/control-plane/internal/store/postgres \
  ./services/control-plane/internal/server \
  ./services/control-plane/internal/authn \
  ./services/control-plane/cmd/cloud-agents-control-plane \
  ./services/control-plane/cmd/cloud-agentsctl \
  ./services/control-plane/internal/localmigration
go test -tags=localdev ./services/control-plane/cmd/cloud-agents-control-plane ./services/control-plane/internal/authn
go vet -tags=localdev ./services/control-plane/cmd/cloud-agents-control-plane ./services/control-plane/internal/authn
bun scripts/generate-platform-json-sdks.ts --check
bun scripts/check-platform-migration-bundle.ts
bun scripts/generate-platform-migration-bundle.ts --check
bun scripts/generate-foundation-migration-package.ts --check
bash -n scripts/cloud-agents-dev.sh
```

The focused CLI test also exercised the exact bootstrap path, headers, fenced request body and generated response decoder. TypeScript SDK typecheck, 46 tests and build passed. Admin Web typecheck, 32 tests and production build passed; Vite retained its existing warning that the single minified JavaScript chunk exceeds 500 kB.

The official AJV suite audit remained `EXECUTED_NONCONFORMANT`, as before. The umbrella contract-standards command was not claimed because the host has Bun 1.4.1 and uv 0.6.0 while the repository pins Bun 1.3.14 and uv 0.12.5; Python 3.14.7 matched. Direct JSON SDK and migration generation checks above passed.

## Evidence boundary and next item

This proves enrollment intent authority, secret isolation/non-replay, generated clients, CLI retrieval, Admin metadata management and durable audit on disposable PostgreSQL. Admin Web was typechecked, tested and built, but this slice did not run the full browser visual matrix.

It does not issue or verify a CSR, create a short-lived mTLS node identity, rotate/revoke a certificate, establish an outbound customer-node connection, report heartbeat/capacity, execute a fenced command, reconnect/reconcile, or provide node Drain/Resume. Those are the next BASE-M3 slices; Kubernetes, deployment/publication and BASE-READY also remain unproved.
