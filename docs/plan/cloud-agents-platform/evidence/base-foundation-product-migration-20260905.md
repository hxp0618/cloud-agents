# Foundation product migration 000053 — 2026-09-05

Source base `2f4e7964`, branch `codex/cloud-agents-platform-p0`. This slice adds
the generated product-000053 migration package, runner binding, current product
migrator/default deployment artifact references and reproducible installer test.
Unrelated `.gitignore`, `go.work.sum` and `docs/img.png` remain untouched.

The immutable product-000052 package remains available as the predecessor and
upgrade input. The new generator reads it and migration 000053, classifies every
SQL statement through the existing closed DDL profile, appends exact catalog
identities, computes domain-separated schema/manifest digests, and emits one
new package plus its Go runner binding. Existing historical packages are not
rewritten. The DDL allowlist adds only the five named coordination replacements
and the exact hash-bound single-writer partial index; arbitrary replacement
functions and arbitrary unique indexes remain rejected.

The normal contract currentness chain checks the foundation profile/SQL, and
the migration chain checks product-000053. Release packaging and migrate image
inputs now select `cloud-agents-migrations-000053.tar` and `product-000053`.
No image was built, published or deployed.

## Real installation and upgrade checks

```sh
node scripts/test-foundation-product-migration.mjs
bun run platform:migrations:check
go test -race ./services/control-plane/internal/localmigration ./services/control-plane/cmd/cloud-agents-product-migrate
go vet ./services/control-plane/internal/localmigration ./services/control-plane/cmd/cloud-agents-product-migrate
bunx vitest run scripts/lib/platform-migration-sql.test.ts scripts/lib/platform-release.test.ts
sh scripts/test-platform-go-products.sh
```

On OrbStack/aarch64, the native harness started one network-disabled
`postgres:17.6-bookworm` container with two fresh databases and a read-only
repository mount. It used an isolated NOINHERIT, non-superuser migration login:

- the real product runner installed product-000052, upgraded exactly one entry
  to product-000053, and then returned deterministic no-op;
- the current `cloud-agents-product-migrate` Linux binary independently installed
  all 53 migrations from product-000053 and returned no-op on replay;
- both ledgers contain exactly 000001–000053. Upgrade retains two immutable
  bundle digests; fresh install has the single current bundle digest;
- all three foundation relations exist after the current installer completes.

[Machine-readable results](base-foundation-product-migration-20260905.json) fix
the source, backend, versions and generated digests. The test removed only its
label-verified disposable container, anonymous database volume and generated
temporary binaries. No existing database, Workspace volume or runtime resource
was modified.

Generation currentness, migration package validation, Go race/vet, 53 scoped
TypeScript tests and the repository Go-products matrix passed. The umbrella
contract command stopped before unrelated downstream generators because local
`uv 0.12.9` differs from the repository pin `0.12.5`; the pin was not changed.
The two new generators were run directly and are current. This tool-version
boundary is not presented as a contract-suite pass.

## Remaining boundary

This proves installability and 000052→000053 upgrade of the persistent intent
schema only. It does not prove a public contract/SDK, RuntimeProfile resolution,
HTTP authorization, Controller claim renewal/effects/recovery, OpenSandbox
adoption/readiness, physical writer fencing or Admin lifecycle. No production
migration, deployment, image publication or formal Gate closure occurred.
BASE-M0/M1 and BASE-READY remain open; the next work is recorded only in 06.
