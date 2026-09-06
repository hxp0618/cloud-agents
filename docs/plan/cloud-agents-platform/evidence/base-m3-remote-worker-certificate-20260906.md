# BASE-M3 RemoteWorker CSR and short-lived node identity

## Source and scope

- Date: 2026-09-06, Asia/Shanghai.
- Branch: `codex/cloud-agents-platform-p0`.
- Pre-commit source HEAD: `ba369077bab3da13a8ed02f532b69fc25cc5490c`; the worktree was dirty. Unrelated `.gitignore`, `CLAUDE.md`, `AGENTS.md`, `go.work.sum`, `docs/img.png` and existing plan-document changes were excluded from this slice.
- Scope: authenticate the one-time enrollment secret, validate a node-generated CSR, issue and persist a 15-minute mTLS client identity, expose only certificate metadata to Admin, and wire the production signer configuration. This is a BASE-M3 identity slice, not outbound node connectivity or phase completion.
- Product migration: `product-000063`; schema bundle digest `sha256:4160c3a7ad6bc177fb52ed5bdb3ce7390a84c482d53683be545a59a3af3ee501`; manifest digest `sha256:1dbd0b8674c256180c52719100de44fcc68d93a4e7e35d9071bd4d4e3aafd367`.

## Implemented authority and security boundary

- The certificate route uses the distinct `RemoteWorkerEnrollment` authorization scheme. User and Admin bearer tokens are not node identities. PostgreSQL authenticates the stored enrollment-secret digest before signing and verifies it again while atomically persisting the result.
- The signer verifies the CSR signature and accepts only ECDSA P-256/P-384 or RSA 2048–4096 public keys. It ignores requested subject/SAN values and writes exactly one server-owned SPIFFE URI for tenant, project, enrollment and incarnation, with client-auth usage only.
- The leaf is valid for 15 minutes with one minute of backward clock tolerance. Signing fails closed when the CA is not yet valid, expired, or cannot cover the full leaf lifetime; runtime authority failure is returned as 503 rather than an invalid-CSR response.
- PostgreSQL owns enrollment transition, incarnation, certificate fingerprint/serial/public chain, exact idempotent replay and append-only audit. The private key and CSR bytes are not persisted. A replay with the same key and request returns the originally stored public chain; changed CSR/incarnation conflicts.
- `cloud-agentsctl` generates the P-256 key locally and reserves a new absolute `0600` identity file with `O_EXCL` before calling the server. It verifies the returned chain matches that key, never overwrites an existing path, and writes no private key to stdout.
- Admin list/detail and Audit expose state, incarnation, SPIFFE ID, fingerprint and expiry only. They omit enrollment Secret/digest, CSR, certificate bytes and private key. The Admin Web uses only those generated SDK fields and labels this boundary in `zh-CN` and `en-US`.
- Production Control Plane accepts a paired CA certificate/key and trust domain. The Helm chart mounts only the named Secret keys `ca.crt`/`ca.key`; values validation rejects partial configuration. With no configured signer the bootstrap endpoint remains present but returns 503.

## Reproducible checks

### Real PostgreSQL migration, HTTP and generated-client closure

```sh
bun scripts/test-foundation-product-migration.mjs
```

This passed against an owned, network-disabled PostgreSQL `17.6 (Debian 17.6-2.pgdg12+1)` arm64 container:

- exact `product-000062` to `product-000063` upgrade, fresh 63-migration install and no-op replay;
- 63-row immutable ledger and two upgrade-path bundle digests;
- real generated Admin/bootstrap Go clients over in-process Control Plane HTTP;
- Admin create and separately scoped one-time Secret claim before certificate issuance;
- signed certificate/private-key match, exact persisted public-chain replay, and wrong enrollment Secret returning 401;
- enrollment resource version `2 -> 3`, durable issue-certificate Audit, and Admin metadata matching the issued identity;
- raw Admin response excluding Secret/digest, CSR and certificate-chain fields, while PostgreSQL contained only the Secret digest and public certificate chain.

The script verified its unique ownership label, then removed only its disposable PostgreSQL container and anonymous volume. It did not start a customer node, RemoteWorker channel, Controller or Docker Sandbox.

### Focused code, generation, SDK, web and chart checks

The following passed on the dirty source tree:

```sh
go test ./services/control-plane/internal/authn \
  ./services/control-plane/internal/remoteworker \
  ./services/control-plane/internal/store/postgres \
  ./services/control-plane/internal/server \
  ./services/control-plane/cmd/cloud-agents-control-plane \
  ./services/control-plane/cmd/cloud-agentsctl
go test -tags=localdev ./services/control-plane/cmd/cloud-agents-control-plane \
  ./services/control-plane/cmd/cloud-agentsctl
go vet ./services/control-plane/internal/authn \
  ./services/control-plane/internal/remoteworker \
  ./services/control-plane/internal/store/postgres \
  ./services/control-plane/internal/server \
  ./services/control-plane/cmd/cloud-agents-control-plane \
  ./services/control-plane/cmd/cloud-agentsctl
go vet -tags=localdev ./services/control-plane/cmd/cloud-agents-control-plane \
  ./services/control-plane/cmd/cloud-agentsctl
go test ./sdk/go/gen/openapi/v1alpha1 -run '^TestRemoteWorkerBootstrapHTTPClientUsesEnrollmentScheme$'
go test ./sdk/go/gen/platform/v1alpha1
bun run platform:migrations:check
bun scripts/generate-platform-json-sdks.ts --check
sh scripts/test-cloud-agents-helm.sh
bun run secret:scan
bun run --cwd=sdk/typescript build
bun run --cwd=sdk/typescript typecheck
bun run --cwd=sdk/typescript test
bun run --cwd=apps/admin-web typecheck
bun run --cwd=apps/admin-web test
bun run --cwd=apps/admin-web build
git diff --check
```

TypeScript SDK ran 47 passing tests; Admin Web ran 32 passing tests and built successfully. Vite retained its existing warning that the single minified JavaScript chunk exceeds 500 kB. Helm lint and positive/negative RemoteWorker signer value assertions passed. The repository secret scan found no unallowlisted secret-shaped tracked content.

The umbrella checks were executed but are not claimed as passing: `platform:contracts:check` stopped after its current `EXECUTED_NONCONFORMANT` AJV audit because the host has Bun `1.4.1` and uv `0.6.0` instead of pinned Bun `1.3.14` and uv `0.12.5`; Python `3.14.7` matched. `platform:sdk:check` passed identity and JSON generation, then stopped at Proto generation because host Go `1.27.1` differs from pinned `1.26.6`; `platform:go:check` stopped at the same Go mismatch. Direct affected generation and Go checks above passed. The current oxfmt also wants to reformat the already compact pre-slice `RemoteWorkerEnrollmentPanel.tsx`; this slice kept the scoped semantic diff and passed `git diff --check` instead of claiming the repository formatter check.

## Evidence boundary and next item

This proves persisted enrollment-authenticated CSR exchange, short-lived client identity issuance, exact response replay, production signer wiring, generated clients/CLI, and redacted Admin metadata on disposable PostgreSQL and local builds.

It does not prove certificate rotation or active-certificate revocation, a TLS-authenticated outbound customer-node channel, heartbeat/capacity/version reporting, disconnect/reconnect, generation-fenced commands, node health, or Drain/Resume. Those remain the next BASE-M3 slices; no Kubernetes deployment, publication, real customer-node action or BASE-READY closure is claimed.
