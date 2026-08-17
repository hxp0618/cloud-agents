# P1-A2.1b-impl-3 verified PostgreSQL catalog matrix — 2026-08-17

- Status：**IMPLEMENTATION + PG15/16/17 A/B × IDLE/BORROWED × NORMAL/RACE MATRIX — PASS；SUPPLY/INDEPENDENT REVIEW OPEN**
- Fixed implementation commit：`bbb0bf21922525468568afea675a6c1dc5522409`
- Fixed implementation tree：`b1072245d2e0b6e5775123ce3e2484c2d821f462`
- Fixed base commit：`aabea19`
- Branch：`codex/cloud-agents-platform-p1`
- Evidence date：2026-08-17 Asia/Shanghai
- Executor：Codex P1 implementation executor
- Independent Gate reviewer：**not assigned**

## 1. Closed implementation boundary

`bbb0bf2` closes the implementation and conformance-matrix portion of ADR-0010 A2.1b-impl-3:

1. `PGProjector.ProjectCatalog` now consumes the already-completed relation, dependency and expression
   projector and returns a typed `CatalogProjection`, digest and bounded metadata only when the complete
   actual body is canonical-equal to one opaque `VerifiedCatalogContract`;
2. an executable catalog artifact alone remains insufficient. The combined trust-decision binder must
   bind the catalog subject to the same signed `default_acl_owners` and `object_creator_closure` as the
   verified schema bundle before any catalog query can run;
3. the verified wrapper binds subject digest, final scope, complete expected projection, owner closures,
   expiry and security epoch. Literal, expired, copied-with-mutation, scope-swapped, owner-swapped and
   uncombined wrappers fail closed;
4. the full A2.1b `projection_model` is an exact field allowlist. Sparse or reordered relation, column,
   constraint, index, policy, trigger, function, expression or denied-object profiles are rejected during
   strict catalog decode;
5. the checked-in immutable representative subject covers a schema, relation, generated/default/check
   expressions, expression index and predicate, RLS policy, SQL function argument default, trigger
   function and trigger `WHEN`, exact dependencies and default ACL state;
6. the runner decision rebinds each executable catalog to the verified initial schema owner closure and
   rechecks that equality both in the frozen decision and when exact statement plans are built;
7. the live matrix exercises the exported API through both owned repeatable-read/read-only snapshots and
   borrowed serializable/read-write migration snapshots. Both modes return the same complete catalog
   digest and retain their distinct snapshot ownership metadata.

This commit changes no production migration SQL, checked-in `schema-000001.json`/`schema-000002.json`,
`go.mod`, `go.sum`, production CLI, Gate, deployment or release state.

## 2. Fixed source and subject identities

The code commit changes 12 files with `514` insertions and `27` deletions.

| File                                                                                                 | SHA-256                                                            |
| ---------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------ |
| `services/control-plane/internal/migration/projection_pg_adapter.go`                                 | `9c3ba8cb6016d718a4c392678b3bb4f96e8ac67e78f1143d0aa4a619974f6390` |
| `services/control-plane/internal/migration/projection_api.go`                                        | `1fde6aadeaf2b1b610d1404bed037b229ff55b0699beeabd57a32495002937a1` |
| `services/control-plane/internal/migration/projection_validation.go`                                 | `a9af8b9eea9dc438cf54ef5d2f2817c34c4bbb9c768338eb791c8e19e6e98f21` |
| `services/control-plane/internal/migration/runner_projection_bindings.go`                            | `8ed827886d3b306c889508405e1cb8a2a0092d14128a6be1bd374ee70525a8e7` |
| `services/control-plane/internal/migration/projection_postgres_integration_test.go`                  | `44f62760de830e0f3999e87f565acb9de400aab8df8c3300ed11eb3f903e9f87` |
| `services/control-plane/internal/migration/testdata/postgres_projection/catalog-representative.json` | `d3d72f58d251c1342b1e8d6075ac1420774b392ebff9d0cc33fe77394a22d90a` |
| `services/control-plane/scripts/test-migration-projection-postgres-matrix.sh`                        | `771ecbadec535043d2591ad2ad8d5d5ac52a7ad986e737875c3633b085a5ea70` |

The representative artifact digest and the `catalog_subject` emitted by every matrix leg are therefore
the same value: `sha256:d3d72f58d251c1342b1e8d6075ac1420774b392ebff9d0cc33fe77394a22d90a`.

## 3. PostgreSQL acceptance matrix

The matrix used fresh temporary A/B databases for each exact locally pinned image. Every instance ran
the matrix once normally and once with `-race`; every run exercised an idle snapshot and a borrowed
migration snapshot. Container names and labels were unique to the run, implicit image pulls were
forbidden, and all owned containers were removed.

| PostgreSQL | Exact image                                                                        | Local image ID                                                            | A/B normal/race | idle/borrowed catalog |
| ---------- | ---------------------------------------------------------------------------------- | ------------------------------------------------------------------------- | --------------- | --------------------- |
| 15.18      | `postgres@sha256:6eb0add3b77c081df18aa518ce43df58fdcc40f2e6d868a6fd08038dc7acd425` | `sha256:1659a1a994f204ed0397ff17a73d72d27128bfa7d420bd7288ad5e8eb28fa588` | PASS            | exact same bits       |
| 16.14      | `postgres@sha256:95206741a5b214807675e14165369d05b93a9cf692223b616d07cca227e74b0b` | `sha256:02ad0fee02aedf51870d43f109a106a7a8f36930ce9b467ee755254470f227d8` | PASS            | exact same bits       |
| 17.10      | `postgres@sha256:742f40ea20b9ff2ff31db5458d127452988a2164df9e17441e191f3b72252193` | `sha256:8ee7c900f4de054e8f0a3b42b8f5da38b8fba57c3ccd7c1a02be28cb0b06494f` | PASS            | exact same bits       |

The complete catalog projection digest was identical across all 24 catalog observations:

`sha256:7b71b5b0a95233f0de5acea079292d2c10bcf1f1f946b16ebee37a87aa8d7abd`

The aggregate authority+precondition+catalog summaries were:

- PostgreSQL 15: `sha256:ec868a597d1eb9042300e76543bff74f768468e129e89264397a74dff3178803`;
- PostgreSQL 16 and 17: `sha256:572bcb30b93119020bfcb965b18a05ed4f2f0e630521edb631a431c310fb7d05`.

The PG15 aggregate differs only because its signed authority capability projection is intentionally
version-specific; the complete catalog digest remains identical across 15/16/17.

## 4. Fault and local gates

The live fault matrix rejects:

- expression/default mutation with `MIGRATION_CATALOG_DRIFT`;
- function source mutation with `MIGRATION_CATALOG_DRIFT`;
- extra effective schema creator with `MIGRATION_AUTHORITY_DRIFT`;
- undeclared relation with `MIGRATION_PROJECTION_UNKNOWN_OBJECT`;
- database owner/ACL, role attributes, membership grant options, schema owner/ACL and default ACL drift;
- canceled snapshot reuse and terminated-backend pool hijack.

The ordinary tests additionally reject nil context/snapshot, scope swap, uncombined catalog subject,
projection-model mutation, caller alias mutation and an individually valid catalog owner-closure swap.

| Gate                                                                   | Result                                       |
| ---------------------------------------------------------------------- | -------------------------------------------- |
| focused catalog/contract/binding tests                                 | PASS (`0.739s`)                              |
| focused race tests                                                     | PASS (`3.657s`)                              |
| compile all Go packages                                                | PASS (`internal/migration` compile `1.294s`) |
| `go vet ./...` and `go build ./...`                                    | PASS                                         |
| Linux amd64 and arm64 all-package test compile through `/usr/bin/true` | PASS                                         |
| PG15/16/17 A/B normal/race matrix                                      | PASS                                         |
| `gofmt`, `bash -n`, `git diff --check`                                 | PASS                                         |

A default `go test ./internal/migration -count=1` run is deliberately **not** recorded as PASS. It
reached the package's 10-minute timeout while the pre-existing CPU-heavy
`TestRunnerPreledgerProjectionFaultsRollbackWithoutAppendingEvidenceOrLedger/snapshot` path was still
executing canonical validation. The stack showed active work rather than a deadlock and no catalog
assertion failed. Focused, race, compile, vet/build and the live matrix above all passed; this record does
not convert the incomplete aggregate run into a green Gate.

## 5. Explicit non-claims and remaining closure

This is implementation and local conformance evidence, not an independently signed Gate record.

- The production checked-in `schema-000001.json` and `schema-000002.json` remain
  `UNPUBLISHED_BOOTSTRAP_MUTABLE` / `NOT_IMPLEMENTED`; the production CLI still rejects them before a
  database connection.
- No production signature verifier, deployment trust root, database mutation, ledger write, commit,
  deployment, Platform RC, Beta, GA or release is enabled by this slice.
- No x86_64 live database matrix, cloud PostgreSQL, N-1/PITR, controller/host power-loss or immutable
  reviewer signature is claimed.
- Supply-chain lock/SBOM/provenance refresh and independent reviewer closure remain the next impl-3
  boundaries. Until both close, every aggregate P1 Gate remains `IN PROGRESS`.
