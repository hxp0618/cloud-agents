# BASE-M3 RemoteWorker Sandbox create dispatch

## Source and scope

- Date: 2026-09-06, Asia/Shanghai.
- Branch: `codex/cloud-agents-platform-p0`.
- Pre-commit source HEAD: `0bbbbabca96d2493c358e1d01c5097cebe87057b`; the worktree was dirty. Unrelated `.gitignore`, `CLAUDE.md`, `AGENTS.md`, `go.work.sum`, `docs/img.png` and existing plan-document changes were excluded from this slice.
- Scope: admit a published no-Agent RuntimeProfile against an online `remote-worker` target, claim its existing durable Sandbox Operation through outbound mTLS heartbeat, execute the create on the customer node with node-local Docker/OpenSandbox credentials, and settle/replay the physical receipt.
- Product migration: `product-000068`; schema bundle digest `sha256:1486734853a18c930240fe2f02deb483c44866061f1b85c4a6457804402afa6a`; manifest digest `sha256:5a3bbb04ec71c2a8ac4a40fada3435698bfcbb9a0dbc0d9adea93328fd9d0a77`.

## Implemented authority and boundary

- PostgreSQL remains the placement, Operation, outbox, generation and Audit authority. RuntimeProfile publish and User Sandbox admission accept a RemoteWorker target only while its enrolled certificate, heartbeat, desired/observed scheduling state and Docker capability are current by database time. Offline or drained nodes stop new admission and claims.
- The heartbeat response carries one exact `sandbox.create` attempt for the authenticated target/incarnation. It contains the fixed image digest, resource limits and Network Policy facts, but no Control Plane endpoint, Docker endpoint, `credentialRef`, provider credential or Secret bytes. The node resolves its Docker/OpenSandbox endpoint and credential reference only from local process configuration.
- The independent RemoteWorker persists the command before execution and the receipt before acknowledgement. Restart therefore resumes the same durable intent. The physical effect reuses the existing Foundation Docker/OpenSandbox executor rather than adding a second lifecycle implementation.
- Settlement validates the deterministic command ID, attempt, target, incarnation, Operation, Sandbox and generation in PostgreSQL. Exact settled receipt replay is acknowledged without repeating the physical effect. Claim and settlement Audit subjects are the current authenticated mTLS certificate fingerprint.
- Admin Web can select ready Docker or RemoteWorker placement targets for no-Agent RuntimeProfiles through generated SDK data. The User API still returns only published profile summaries and accepts profile ID/version, not target, endpoint or credential fields.

## Reproducible checks

### Product migration and persisted API authority

```sh
GOTOOLCHAIN=go1.26.6 bun scripts/test-foundation-product-migration.mjs
```

This passed against an ownership-labelled, network-disabled PostgreSQL `17.6 (Debian 17.6-2.pgdg12+1)` arm64 container:

- exact product-000067 to product-000068 upgrade, fresh 68-migration install and no-op replay;
- 68-row immutable ledger with both upgrade-path bundle digests;
- RuntimeProfile publish/User admission against current RemoteWorker availability;
- all existing Admin/User authorization, RemoteWorker identity, heartbeat, Target projection and Drain/Resume checks.

The harness verified its ownership label and removed only its disposable PostgreSQL container and anonymous volume.

### Real outbound customer node and OpenSandbox create

```sh
GOTOOLCHAIN=go1.26.6 node scripts/test-foundation-controller-docker.mjs /tmp/cloud-agents-base-m3/evidence
```

The final run used OrbStack Docker `29.4.0`, disposable PostgreSQL `17.6`, a real short-lived mTLS RemoteWorker process and real OpenSandbox Docker runtime. It proved:

- a published User-visible RuntimeProfile targeting the online RemoteWorker;
- durable User Sandbox admission, outbound heartbeat claim and command persistence;
- customer-node creation of runtime `1e6f8a74-a7ef-4ae0-907a-7fdc976d1320` with retained Workspace volume `ca-ws-887ca62cff2f8a88152a4cc647d35ad95434d0563208bb7a3d43110f`;
- Admin observed state `running`, exact receipt replay twice, and two node Audit facts bound to the current mTLS fingerprint;
- zero test-owned runtime containers and Workspace volumes after precise cleanup.

The compact machine-readable result is [base-m3-remote-worker-sandbox-create-20260906.json](base-m3-remote-worker-sandbox-create-20260906.json). Pre-existing local registry and diagnostic containers/volume were preserved.

### Contracts, SDK, code and Admin Web

The following passed with Node `24.18.1`, Bun `1.3.14`, Go `1.26.6`, Python `3.14.7` and uv `0.12.5`:

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
bun run secret:scan
git diff --check
```

Contract validation reported 186 schemas and 126 OpenAPI operations; all generators were current. The TypeScript SDK ran 49 tests, Admin Web ran 33 tests and built with its existing over-500-kB chunk warning, and the two focused script suites ran 41 tests.

The repository-wide `go -C services/control-plane test ./...` was also attempted but is not claimed passing: the unchanged `internal/migration` package already expects an obsolete 11-entry quota fact while current HEAD carries the same `sha256:c7e08e81...` 13-entry bundle, also trips its existing sealed-session spread check, and timed out after 10 minutes. All packages outside that pre-existing migration evidence debt passed. Repository-wide lint likewise reaches an unchanged `no-console` error in `apps/admin-web/visual-baseline/daytona-v0.190.0/capture-worker-health.mjs`; lint restricted to this slice passed.

## Evidence boundary and next item

This proves initial Workspace/Sandbox create dispatch on one real local Docker customer node, persisted command/receipt recovery, exact receipt replay, Audit identity and scoped cleanup. It does not yet prove RemoteWorker Exec/Files routing, Stop/Rebuild, lease renewal for effects longer than 60 seconds, process/network interruption during an active effect, NAT outage reconciliation, Kubernetes customer nodes, deployment, publication, Provider runtime or browser visual QA.

The next BASE-M3 slice is RemoteWorker Sandbox Exec/Files using the existing generation-bound access authority, followed by Stop/Rebuild and reconnect reconciliation. BASE-M3 and the overall Goal remain incomplete.
