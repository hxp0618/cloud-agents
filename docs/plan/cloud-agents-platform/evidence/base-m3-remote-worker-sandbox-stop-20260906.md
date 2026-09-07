# BASE-M3 RemoteWorker Sandbox Stop dispatch

## Source and scope

- Date: 2026-09-06, Asia/Shanghai.
- Branch: `codex/cloud-agents-platform-p0`.
- Pre-commit source HEAD: `9dfd0910bbab2141399194307ecd7c126fb46905`; the worktree was dirty. Unrelated `.gitignore`, `CLAUDE.md`, `AGENTS.md`, `go.work.sum`, `docs/img.png` and existing plan-document changes were excluded.
- Scope: accept the existing generated Admin Sandbox Stop action for a running RemoteWorker Sandbox, deliver one exact outbound command, delete compute on the customer node, retain the Workspace volume, settle the durable Operation and replay its receipt.
- Product migration: `product-000069`; schema bundle digest `sha256:fa3cd65a63d2ed05fedeef2f69b81e929c2c35b582c4f9c7678639095457ae92`; manifest digest `sha256:7842051b6579dc70d48240eabb2d63a3f57f4b498cae4b3652cec34d7ef9117b`.

## Implemented authority and boundary

- PostgreSQL keeps the existing Sandbox lifecycle, outbox, Operation, generation, target and Audit authority. The new forward migration permits `sandbox.stop` only for the exact RemoteWorker target currently making the authenticated heartbeat.
- The command is bound to the retained physical volume and prior runtime ID, Operation, generation and spec digest. It contains no endpoint, `credentialRef`, Provider credential or Secret bytes; the customer node resolves Docker and OpenSandbox access from its local process configuration.
- The independent RemoteWorker persists the command and receipt and reuses the shared Foundation executor. A successful Stop verifies volume ownership, deletes the exact OpenSandbox runtime, returns no live runtime identity and requires `cleanupComplete=true`.
- Settlement additionally matches action, deterministic command ID, attempt, target, incarnation, Operation, Sandbox and generation. Exact settled Stop receipt replay is acknowledged without repeating deletion. Claim and settlement Audit subjects are the current mTLS certificate fingerprint.
- No new Admin page was needed: the existing generated-SDK Stop confirmation, generation/resourceVersion fence, Operation status and retained-Workspace wording already apply to RemoteWorker-backed Sandboxes.

## Reproducible checks

### Product migration and persisted authority

```sh
GOTOOLCHAIN=go1.26.6 bun scripts/test-foundation-product-migration.mjs
```

This passed against disposable PostgreSQL `17.6 (Debian 17.6-2.pgdg12+1)` arm64: exact product-000068 to product-000069 upgrade, fresh 69-migration install, no-op replay and a 69-row immutable ledger. The harness removed only its owned container and anonymous volume.

### Real outbound Stop

```sh
GOTOOLCHAIN=go1.26.6 bun scripts/test-foundation-controller-docker.mjs /tmp/cloud-agents-base-m3-stop/evidence
```

The successful run used OrbStack Docker `29.4.0`, disposable PostgreSQL 17.6, a real short-lived mTLS RemoteWorker process and a real OpenSandbox Docker runtime. It proved:

- runtime `452973a9-d7a8-4116-b5d3-03de8b66d290` was created on the customer node and then deleted by outbound Stop;
- physical Workspace volume `ca-ws-887ca62cff2f8a88152a4cc647d35ad95434d0563208bb7a3d43110f` remained bound while Admin observed `stopped` and `writerReleased=true`;
- exact Stop receipt replay succeeded twice;
- create and Stop produced four claim/settlement Audit facts, all bound to the current mTLS certificate fingerprint;
- final cleanup left zero test-owned runtime containers and Workspace volumes.

An earlier diagnostic run exposed that stopped Sandbox runtime state is durably represented as an empty string rather than SQL `NULL`; the exact-replay predicate was corrected and the complete harness was rerun successfully. Compact facts are in [base-m3-remote-worker-sandbox-stop-20260906.json](base-m3-remote-worker-sandbox-stop-20260906.json).

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
bunx oxlint scripts/test-foundation-controller-docker.mjs sdk/typescript/src/platform.ts --deny-warnings
bun run secret:scan
git diff --check
```

Contract validation reported 186 schemas and 126 OpenAPI operations; all generators were current. The TypeScript SDK ran 50 tests, Admin Web ran 33 tests and built with its existing over-500-kB chunk warning, and the focused script suites ran 41 tests. A mistakenly invoked standalone ESLint 10 command found no repository ESLint config and therefore executed no lint; the repository's actual oxlint command above passed for changed JavaScript/TypeScript files.

## Evidence boundary and next item

This proves RemoteWorker Sandbox Stop, retained Workspace authority, exact receipt replay, mTLS Audit identity and scoped cleanup on one real local Docker customer node. It does not prove RemoteWorker Rebuild, reverse Exec/Files/PTY/Preview/SSH access, claim renewal beyond 60 seconds, active NAT/network interruption reconciliation, Kubernetes customer nodes, deployment, publication or browser visual QA.

The next BASE-M3 slice is RemoteWorker Sandbox Rebuild using this lifecycle channel; the reverse access channel and reconnect reconciliation follow. BASE-M3 and the overall Goal remain incomplete.
