# BASE-M3 RemoteWorker Sandbox Rebuild dispatch

## Source and scope

- Date: 2026-09-07, Asia/Shanghai.
- Branch: `codex/cloud-agents-platform-p0`.
- Pre-commit source HEAD: `b592799af9cc2c1b2fe184541e6aaf46281c5b2f`; the worktree was dirty. Unrelated `.gitignore`, `CLAUDE.md`, `AGENTS.md`, `go.work.sum`, `docs/img.png` and existing plan-document changes were excluded.
- Scope: accept the existing generated Admin Sandbox Rebuild action for a stopped RemoteWorker Sandbox, deliver one exact outbound command, create new compute on the customer node with the retained Workspace volume, settle and replay the receipt, then stop the rebuilt compute without residue.
- Product migration: `product-000070`; schema bundle digest `sha256:5de78240f28a5ae86d8bbd9fbc2cc435ce2299239006145a5a3452383d823bf9`; manifest digest `sha256:d3f21d45dac04c615cfcae323d6cc850fea39b8b2ae49081af11d23e73dc6a9a`.

## Implemented authority and boundary

- PostgreSQL remains the lifecycle, outbox, Operation, generation, Target and Audit authority. Rebuild admission now accepts the server-owned RemoteWorker Target only while its current mTLS heartbeat, Docker capability, scheduling state and fixed Network Policy are available.
- The outbound `sandbox.rebuild` command binds the retained physical volume but carries no stale runtime identity. It contains no endpoint, `credentialRef`, Provider credential or Secret bytes; the customer node resolves Docker and OpenSandbox access from local process configuration.
- The RemoteWorker reuses the same Foundation executor as local Docker create. That executor verifies the retained volume owner, rejects a different physical volume and creates a new generation-bound OpenSandbox runtime.
- Settlement matches action, deterministic command ID, attempt, Target, incarnation, Operation, Sandbox and generation. Exact successful Rebuild receipt replay is acknowledged without creating another runtime.
- No new Admin page was needed: the existing generated-SDK Rebuild confirmation, generation/resourceVersion fence, Operation status and retained-Workspace wording already apply.

## Reproducible checks

### Product migration and persisted authority

```sh
GOTOOLCHAIN=go1.26.6 bun scripts/test-foundation-product-migration.mjs
```

This passed against disposable PostgreSQL `17.6 (Debian 17.6-2.pgdg12+1)` arm64: exact product-000069 to product-000070 upgrade, fresh 70-migration install, no-op replay and a 70-row immutable ledger. The harness removed only its owned container and anonymous volume.

### Real outbound Rebuild

```sh
GOTOOLCHAIN=go1.26.6 bun scripts/test-foundation-controller-docker.mjs /tmp/cloud-agents-base-m3-rebuild/evidence
```

The successful run used OrbStack Docker `29.4.0`, disposable PostgreSQL 17.6, a real short-lived mTLS RemoteWorker process and a real OpenSandbox Docker runtime. It proved:

- runtime `58f87ab5-7d85-4391-8e93-fac504ac0fa8` wrote a Workspace proof before outbound Stop;
- Admin Rebuild created runtime `6ebd362b-92cf-4276-9ef5-2369b0ba5d70` on the customer node with the same physical volume `ca-ws-887ca62cff2f8a88152a4cc647d35ad95434d0563208bb7a3d43110f`;
- the rebuilt runtime read the same Workspace file with SHA-256 `64369e3856843eaad19d590ae5eba5153c8683a49002eb4b634ae2ba599e13d0`;
- exact create, Stop, Rebuild and cleanup-Stop receipt replay succeeded; eight claim/settlement Audit facts used the current mTLS certificate fingerprint;
- final Admin Stop removed rebuilt compute and scoped harness cleanup left zero test-owned runtime containers and Workspace volumes.

Compact facts are in [base-m3-remote-worker-sandbox-rebuild-20260907.json](base-m3-remote-worker-sandbox-rebuild-20260907.json).

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

Contract validation reported 186 schemas and 126 OpenAPI operations; all generators were current. The TypeScript SDK ran 50 tests, Admin Web ran 33 tests and built with its existing over-500-kB chunk warning, and the focused script suites ran 41 tests.

## Evidence boundary and next item

This proves RemoteWorker Sandbox Rebuild, exact retained-Workspace bytes, new runtime generation, receipt replay, mTLS Audit identity and scoped cleanup on one real local Docker customer node. It does not prove reverse Exec/Files/PTY/Preview/SSH access, claim renewal beyond 60 seconds, active NAT/network interruption reconciliation, Kubernetes customer nodes, deployment, publication or browser visual QA.

The next BASE-M3 slice is the reverse Exec/Files access channel; long-operation claim renewal and reconnect reconciliation follow. BASE-M3 and the overall Goal remain incomplete.
