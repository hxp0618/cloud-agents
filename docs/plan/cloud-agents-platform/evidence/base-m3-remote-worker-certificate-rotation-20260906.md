# BASE-M3 RemoteWorker certificate rotation and revocation

## Source and scope

- Date: 2026-09-06, Asia/Shanghai.
- Branch: `codex/cloud-agents-platform-p0`.
- Pre-commit source HEAD: `0219a21061772c82b216269b1c4dfc034861a02e`; the worktree was dirty. Unrelated `.gitignore`, `CLAUDE.md`, `AGENTS.md`, `go.work.sum`, `docs/img.png` and existing plan-document changes were excluded from this slice.
- Scope: authenticate an enrolled node with its short-lived mTLS identity, rotate that identity with response-loss replay protection, revoke the active identity from Admin, expose certificate lifecycle metadata, and wire production TLS client-certificate verification. This is not an outbound command channel or BASE-M3 completion.
- Product migration: `product-000064`; schema bundle digest `sha256:e492f9c206265ab7e40de71e67fbd27dd9ce0556af3ffab0ba25ae1fa7a4d541`; manifest digest `sha256:4ff4cea829ebcd7fa841975efbaad6f5db5194a4a64e4dc54557025fc773be8c`.

## Implemented authority and security boundary

- The node rotation route uses OpenAPI `mutualTLS` and accepts no User/Admin bearer or enrollment Secret. The Go node SDK requires HTTPS, leaves `Authorization` absent, disables redirects, and relies on a caller-owned TLS transport for the short-lived client certificate.
- Production HTTPS requests a client certificate when the RemoteWorker CA is configured and verifies any presented chain with that CA. Existing bearer routes remain usable without a client certificate. The server then checks the verified leaf's single client-auth SPIFFE identity and exact tenant/project/enrollment path before database authorization.
- PostgreSQL remains the lifecycle authority. Rotation requires an active, unexpired current fingerprint and expected resource version. A concurrent or response-loss replay returns the persisted public chain without signing again only when the same idempotency key, request digest and current/previous fingerprint match. Only one previous fingerprint is retained.
- Rotation increments resourceVersion and appends Audit. The Admin revoke action keeps the enrolled node record, marks the active certificate `revoked`, records actor/idempotency/request facts, increments resourceVersion and appends Audit. Any subsequent node rotation is rejected with 401.
- Admin list/detail exposes only lifecycle state, fingerprint, SPIFFE ID, expiry and revocation time. It does not return certificate bytes, CSR, enrollment Secret/digest, private key or user content. Admin Web reuses the existing confirmed danger action and shows active/revoked certificate metadata in `en-US` and `zh-CN`.
- The migration SQL classifier no longer needs per-version hash entries for each simple backfill or row trigger. It accepts only a bounded `cloud_agents` table backfill with a `WHERE` clause and a no-argument `BEFORE ... FOR EACH ROW` trigger; wrong schema, unbounded updates, subqueries, `AFTER`/`DELETE`, trigger arguments and statement-level triggers remain rejected.

## Reproducible checks

### Real PostgreSQL migration and verified mTLS lifecycle

```sh
bun scripts/test-foundation-product-migration.mjs
```

This passed against an owned, network-disabled PostgreSQL `17.6 (Debian 17.6-2.pgdg12+1)` arm64 container:

- exact product-000063 to product-000064 upgrade, fresh 64-migration install and no-op replay;
- 64-row immutable ledger with the two expected upgrade-path bundle digests;
- enrollment Secret claim and initial CSR issuance through generated clients;
- an actual TLS server configured with the RemoteWorker CA and a generated Go mTLS client presenting the issued leaf and chain;
- rotation to a new key/incarnation and matching returned certificate, plus exact replay from the previous certificate returning the persisted chain;
- reuse of the rotation idempotency key with a changed request returning 409;
- the previous certificate with a different idempotency key returning 401;
- ordinary User token calling the Admin revoke route returning 403;
- Admin active-certificate revoke and exact Admin replay, followed by the revoked current certificate returning 401 on rotation;
- five durable enrollment Audit events and raw Admin responses without Secret, CSR or certificate-chain bytes.

The script verified its ownership label, then removed only its disposable PostgreSQL container and anonymous volume. No outbound customer-node process, command channel, Controller or Docker Sandbox was started.

### Focused code, generation, SDK, web and chart checks

The following passed on the dirty source tree:

```sh
go test ./services/control-plane/internal/remoteworker \
  ./services/control-plane/internal/store/postgres \
  ./services/control-plane/internal/server \
  ./services/control-plane/cmd/cloud-agents-control-plane
go vet ./services/control-plane/internal/remoteworker \
  ./services/control-plane/internal/store/postgres \
  ./services/control-plane/internal/server \
  ./services/control-plane/cmd/cloud-agents-control-plane
go test -tags=localdev ./services/control-plane/cmd/cloud-agents-control-plane
go vet -tags=localdev ./services/control-plane/cmd/cloud-agents-control-plane
go test ./sdk/go/gen/openapi/v1alpha1 -run 'TestRemoteWorker(MTLS|Bootstrap)'
go test ./sdk/go/gen/platform/v1alpha1
bun run platform:migrations:check
bun scripts/generate-platform-json-sdks.ts --check
bunx vitest run scripts/lib/platform-migration-sql.test.ts
bun run --cwd=sdk/typescript typecheck
bun run --cwd=sdk/typescript test
bun run --cwd=sdk/typescript build
bun run --cwd=apps/admin-web typecheck
bun run --cwd=apps/admin-web test
bun run --cwd=apps/admin-web build
sh scripts/test-cloud-agents-helm.sh
bun run secret:scan
git diff --check
```

TypeScript SDK ran 48 passing tests; Admin Web ran 32 passing tests and built successfully. Vite retained its existing warning that the single minified JavaScript chunk exceeds 500 kB. Migration generation was deterministic, and the bounded migration-classifier suite ran 37 passing tests. Helm lint and Secret scanning passed.

The umbrella checks were also executed but are not claimed as passing. `platform:contracts:check` recorded the existing `EXECUTED_NONCONFORMANT` AJV audit, then stopped because the host has Bun `1.4.1` and uv `0.6.0` instead of pinned Bun `1.3.14` and uv `0.12.5`; Python `3.14.7` matched. `platform:sdk:check` passed identity and JSON generation, then stopped at Proto generation because host Go `1.27.1` differs from pinned `1.26.6`; `platform:go:check` stopped at the same mismatch. Direct affected checks above passed. This slice did not run the full Admin visual matrix.

## Evidence boundary and next item

This proves generated contract/SDK, persisted authority, verified TLS client identity, exact response-loss replay, active-certificate revocation, production TLS wiring, Admin action/metadata and negative permission paths on disposable PostgreSQL and local builds.

It does not prove an outbound customer-node process, heartbeat/capacity/version reporting, disconnect/reconnect, command idempotency/deadline, generation fencing, node health, Drain/Resume, real customer-node Sandbox execution or BASE-M3 completion. Those remain the next BASE-M3 slice; no deployment, publication, Kubernetes action, customer resource operation or formal Gate closure is claimed.
