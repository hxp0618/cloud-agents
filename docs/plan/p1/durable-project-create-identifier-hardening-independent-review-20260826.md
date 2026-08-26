# Durable Project-create identifier hardening — independent review

Date: 2026-08-26 Asia/Shanghai

## Verdict

`APPROVE - P0=0 / P1=0 / P2=0`

This is one fresh, read-only review of the fixed bounded successor candidate
`8e6045c735b5892a129fbe5befb6b34d9ec6c759`. The candidate was not modified.
This review approves only the isolated identifier hardening; it does not
promote the canonical migration runner, close a Gate, or authorize an external
effect.

## Fixed candidate and topology

- parent: `46af0133554571d47b605872bc38c3844201875f`;
- candidate tree: `563022d13653d1993318225a712b816193afd825`;
- candidate parent tree: `6a2a9598bbbc839016785129f6452d2acf25f15c`;
- parent-to-candidate binary diff SHA-256:
  `8fe468be735ea9823e6912dd19c57b949a25964c85182e0671d0891557541bdf`;
- the review path is absent from the candidate tree.

The candidate is a single-parent direct child and changes exactly these four
paths:

1. `docs/plan/p1/durable-project-create-identifier-hardening-successor-entry-20260826.md`;
2. `scripts/lib/platform-durable-project-create-identifiers.test.ts`;
3. `scripts/lib/platform-migration-sql.ts`;
4. `services/control-plane/migrations/000014_harden_durable_project_create_identifiers.sql`.

No canonical migration manifest/schema-bundle, v2 lineage output, D-053
projection/profile/replay/lock, HTTP, provider, P2, deployment, or release
path is in the candidate diff.

## Immutable predecessor and SQL semantics

`000013_add_durable_project_create_writer.sql` is byte-identical to the
reviewed predecessor. Its SHA-256 is
`d8c3687e300767f7e27f673c6a9fc3de098fbec1b8911dc018c47d32de33dffa`, and the
candidate test fixes that value. The new migration is one
`CREATE OR REPLACE FUNCTION` statement with the same function signature and
return table. PostgreSQL owner/ACL and existing rows are preserved by the
append-only replacement.

The new-write identifier block has two independent domains:

- `cloud-agents/durable-project-create/operation-id/v1`;
- `cloud-agents/durable-project-create/event-id/v1`.

Each domain and input is length-prefixed, converted as UTF-8, hashed with
`pg_catalog.sha256(bytea)`, and lower-case hex encoded without truncation. The
operation and event suffixes are separate 64-hex values, yielding identifier
lengths 79 and 85 respectively. The focused test also fixes deterministic
sample digests (`c9896e…` and `2c86a1…`) and asserts they differ.

The replay/conflict branch returns before identifier derivation and before the
new durable writes. Therefore an idempotency row created under `000013` keeps
its stored MD5-derived operation/event identifiers; only a newly admitted
create uses the SHA-256 domains.

## Independently reproduced checks

```text
bunx vitest run scripts/lib/platform-durable-project-create-identifiers.test.ts scripts/lib/platform-migration-sql.test.ts
  2 files / 10 tests passed

GOWORK=off GOFLAGS=-mod=readonly \
  go test ./internal/store/postgres -run 'TestDurableProjectCreate' -count=1
  PASS

bun scripts/check-platform-migration-bundle.ts
  BOOTSTRAP_VALIDATED; notGateClosure=true

bun scripts/generate-platform-migration-bundle.ts --check
  platform-migration-bundle: current

bunx oxfmt --check scripts/lib/platform-durable-project-create-identifiers.test.ts
  PASS

bunx oxlint scripts/lib/platform-durable-project-create-identifiers.test.ts --deny-warnings
  PASS

git diff --check 46af0133554571d47b605872bc38c3844201875f 8e6045c735b5892a129fbe5befb6b34d9ec6c759
  PASS
```

No PostgreSQL instance, SSH target, production database, HTTP/P2/provider
call, deployment, publication, release, or Gate operation was used. The
canonical migration manifest remains at schema head `000013`; binding this
successor into generated runner/manifest authority is intentionally a separate
future current-source entry and is not claimed here.

## Review boundary

`APPROVE` closes only this local identifier-hardening candidate and resolves
the reviewed MD5 finding at the SQL successor boundary. It does not claim
that the successor is installed or production-runnable, does not rewrite
`000013`, does not invalidate or regenerate D-053 evidence, and leaves every
aggregate Gate `OPEN`/`IN PROGRESS`.
