# BASE-M2 Sandbox Network Policy execution and Admin management

## Source and scope

- Date: 2026-09-06, Asia/Shanghai.
- Branch: `codex/cloud-agents-platform-p0`.
- Pre-commit source HEAD: `44a8739147692aa8c2e7108f2798d74eff95badd`; the worktree was dirty and unrelated changes were excluded from this slice.
- Scope: bind an executable Network Policy to the no-Agent RuntimeProfile, enforce it in the fixed OpenSandbox candidate, expose only policy metadata and enforcement state through Admin API/Web, and keep User API projections free of infrastructure authority.
- Product migration: `product-000061`, digest `sha256:97023479797ada9119ef207592d10f25f6cd8d7a877e5d5656c821f909c52b45`.

## Implemented authority

- `NetworkPolicy.spec.allowedEgress` is the canonical direct allow list. New restricted profiles require at least one normalized DNS name, wildcard DNS name, IP, or CIDR; deny policies require an empty list. Public egress and unresolved ingress/reference policies fail closed for new no-Agent admission.
- RuntimeProfile create/publish and Sandbox create/rebuild resolve the immutable Network Policy binding in PostgreSQL. Legacy pre-authority records remain inspectable, while new work cannot silently inherit public networking.
- The Controller sends the resolved policy to OpenSandbox, then authenticates to the fixed egress sidecar and settles only after the sidecar returns the exact policy with `dns+nft` enforcement. Preview registration rechecks `previewEnabled` in the database authority.
- Admin Web uses the generated SDK to create/update direct targets and displays RuntimeProfile binding plus Sandbox pending/enforced/failed state. Admin responses contain policy IDs, normalized targets and status only; they do not expose endpoint, credential, Workspace, Artifact, prompt, code, or Provider content. User RuntimeProfile projection remains redacted.

## Reproducible checks

### Contract, generation, and focused checks

The following commands passed on the dirty source tree:

```sh
bun scripts/generate-foundation-migration-package.ts --check
bun run platform:migrations:check
bun scripts/generate-platform-json-sdks.ts --check
GOTOOLCHAIN=local GOFLAGS=-mod=readonly go -C services/control-plane test ./internal/networkpolicy ./internal/coordination ./internal/opensandbox ./internal/foundationcontroller ./internal/store/postgres ./internal/server ./internal/localmigration
GOTOOLCHAIN=local GOFLAGS=-mod=readonly go -C services/control-plane vet ./internal/networkpolicy ./internal/coordination ./internal/opensandbox ./internal/foundationcontroller ./internal/store/postgres ./internal/server ./internal/localmigration
GOTOOLCHAIN=local GOFLAGS=-mod=readonly go -C sdk/go test ./gen/platform/v1alpha1
bun test sdk/typescript/src/platform.test.ts
bun --cwd sdk/typescript build
bun --cwd apps/admin-web test
bun --cwd apps/admin-web typecheck
bun --cwd apps/admin-web build
node --check scripts/test-foundation-controller-docker.mjs
node --check scripts/test-foundation-product-migration.mjs
```

Results: generated artifacts were current; focused Go tests and vet passed; generated TypeScript tests passed 29/29; Admin tests passed 32/32; Admin typecheck and production build passed. Vite retained its existing warning that the single minified JavaScript chunk is above 500 kB.

The repository-wide standards command was not claimed: this host has Bun 1.4.1 and uv 0.6.0 while the repository pins Bun 1.3.14 and uv 0.12.5; the full SDK umbrella additionally expects Go 1.26.6 while the host has Go 1.27.1. The affected JSON SDK and migration generators were checked directly.

### Real PostgreSQL migration and HTTP authority

```sh
node scripts/test-foundation-product-migration.mjs
```

Against disposable PostgreSQL 17.6 arm64 this passed fresh install through 000061, exact 000060 to 000061 upgrade, no-op replay, 61 immutable ledger rows with two bundle digests, generated Admin/User HTTP authorization, RuntimeProfile lifecycle, durable Sandbox acceptance, and ordinary-user Admin 403. Restricted direct policy create and update passed before binding; update after RuntimeProfile reference returned conflict. The owned PostgreSQL container and anonymous volume were removed.

### Real OrbStack Docker isolation

```sh
node scripts/test-foundation-controller-docker.mjs \
  docs/plan/cloud-agents-platform/evidence/base-m2-sandbox-network-policy-20260906-runtime-isolation-accepted
```

Machine-readable result: [evidence.json](base-m2-sandbox-network-policy-20260906-runtime-isolation-accepted/evidence.json). Candidate log: [opensandbox.log](base-m2-sandbox-network-policy-20260906-runtime-isolation-accepted/opensandbox.log).

- Run: `foundation-controller-55e1817c-1650-4cc9-ad71-567d19b3735b` on OrbStack Docker 29.4.0 and disposable PostgreSQL 17.6.
- Fixed candidate source: `207d94c7dc7735c143856fe5c6538b743e478786`; server, execd, egress and Sandbox images are digest-pinned in `evidence.json`.
- A restricted Sandbox reached its exact allowed sink. A second Sandbox sink, the Docker host/Control Plane path, and `169.254.169.254` were unreachable.
- The authenticated `/policy` sidecar response matched the RuntimeProfile policy and reported `dns+nft`; Controller restart adopted the same runtime without duplication.
- With `previewEnabled=false`, a new Preview port registration returned 403. Existing wrong-token, cross-tenant, revocation, expiry, Files, PTY and SSH negative paths also passed.
- Stop, rebuild, TTL stop, failure compensation and final cleanup passed. Final inventories contained no `opensandbox.io/id` container, no test-labeled container, and no Foundation Workspace volume.

## Evidence boundary

This proves the current no-Agent Docker execution path and PostgreSQL/Admin authority on one local OrbStack host. Admin Web was source-run through its connection/error surface and build-tested, but this slice did not run the complete browser visual matrix. It does not prove Kubernetes, outbound customer-node networking, deployment, image publication, or BASE-READY; those remain in BASE-M3 through BASE-M5.
