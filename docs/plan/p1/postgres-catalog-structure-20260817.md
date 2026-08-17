# P1-A2.1b-impl-1 PostgreSQL catalog structure — 2026-08-17

- Status：**IMPLEMENTATION + LOCAL PG15/16/17 STRUCTURAL MATRIX — PASS；PUBLIC PROJECTOR CLOSED；Gates OPEN**
- Fixed implementation commit：`ed37295ad8cdb9373c17247c6e8c9e1ce4926e8e`
- Fixed base commit：`6f4389d5a885a5fd30f6853647620fafc59ccd66`
- Fixed source tree：`71b6c033b86bda40a646673ec70bf0be73f7ef51`
- Branch：`codex/cloud-agents-platform-p1`
- Evidence date：2026-08-17 Asia/Shanghai
- Executor：Codex P1 implementation executor
- Independent Gate reviewer：**not assigned**

## 1. Closed scope

This slice implements the structural half of ADR-0010 A2.1b without enabling the production catalog
projector:

1. eleven sealed fixed-query IDs read relations, columns, constraints, indexes and terms, policies,
   triggers, functions and arguments, internal objects and raw dependency edges;
2. PostgreSQL OIDs exist only as ephemeral addresses inside one owned snapshot. The retained projection
   uses logical object identities and normalized scalar values;
3. relations, functions and their child objects are reconciled against the exact declared-object set.
   Unknown, unsupported, undeclared and dependency-outside-closure objects enter a deterministic denied
   set and fail closed;
4. known TOAST objects, implicit constraint indexes and PostgreSQL-generated FK/unique triggers are
   normalized into closed internal identities without retaining generated names or instance-local OIDs;
5. expression-bearing fields retain only ordinary, package-private expression slots. No expression AST
   is fabricated, and `completeBody` rejects every unresolved slot with
   `MIGRATION_PROJECTION_NOT_IMPLEMENTED`;
6. full relation/function/column/constraint/index/policy/trigger structural validators now reject sparse,
   duplicate, unsorted, cross-object and expression-bearing bodies;
7. PostgreSQL 17's owner-only, non-grantable `MAINTAIN` baseline is normalized because owner identity is
   already projected. Any wider or grantable `MAINTAIN` edge is rejected as an unknown object.

The exported `PGProjector.ProjectCatalog` entry point remains fail closed. This commit does not connect
the structural result to runner, CLI, signed expected subjects, database mutation or a durable authority.

## 2. Query and identity boundary

All catalog reads use the existing bounded `ProjectionSnapshot` query path and fixed SQL. The queries
are PG15/16/17 parse-safe, schema-scoped and deterministically ordered. OIDs are never accepted from a
caller and never enter a final digest. Cross-row joins are closed before the builder can emit a logical
identity.

The namespace reader has two explicit modes:

- the existing A2.1a path still rejects relation/function/catalog objects;
- the A2.1b structural path retains typed namespace sightings so the later catalog queries must account
  for every object.

An AST regression test prevents any production file outside `projection_catalog.go` from calling
`readCatalogStructure` or `completeBody` before A2.1b-impl-2 closes expression normalization and the
production binding.

## 3. Fixed implementation scope

The implementation commit changes exactly 9 files: `3686` insertions and `29` deletions. It changes no
`go.mod`, `go.sum`, migration SQL or checked-in catalog JSON.

| File                                                                            | SHA-256                                                            |
| ------------------------------------------------------------------------------- | ------------------------------------------------------------------ |
| `services/control-plane/internal/migration/projection_api.go`                   | `5e86570d0b93e1b9d07c3b313933c8b62fc399557bf7fc62082990a108fe514c` |
| `services/control-plane/internal/migration/projection_namespace.go`             | `0f180f5a88aed9829c8dad806828da64fa38b71c0c9e8a1959b7b7ae70808dc8` |
| `services/control-plane/internal/migration/projection_pg_adapter.go`            | `6e86dad53445eddddd3feaeebedaabff6b520b4d4ef3305a0556c662d755a9b0` |
| `services/control-plane/internal/migration/projection_pg_adapter_test.go`       | `3dfd0e981a4ea1b249a0c236944f10b1d466ef3aa5e20de93f9b3105c088b517` |
| `services/control-plane/internal/migration/projection_validation.go`            | `7add11e025fc63fd7f9f7042a0f9247ebc17b5b289a0996abfddb26bdd726c6e` |
| `services/control-plane/internal/migration/projection_catalog.go`               | `768c1d3b8cc8fa2f318dd1219853e6f2f01827ff74bdaa743f83f4d51e297f12` |
| `services/control-plane/internal/migration/projection_catalog_queries.go`       | `fb43e6686d0f0177e4c4c7399e000ce37010a1755b5554f466372ee9d172e6d0` |
| `services/control-plane/internal/migration/projection_catalog_test.go`          | `c44a48653912852874695fed8c4aa0c04ca02cf838f312bf619985767a88b260` |
| `services/control-plane/internal/migration/projection_catalog_postgres_test.go` | `f24753a99bc218847f366742ce37bc31207f36a7e06d9a3d2bf28968b3ea0562` |

## 4. Deterministic local gates

The fixed source used Go `1.26.6 darwin/arm64` and Docker client/server `29.4.0`.

| Gate                                                                                 | Result          |
| ------------------------------------------------------------------------------------ | --------------- |
| focused catalog tests                                                                | PASS (`0.772s`) |
| focused catalog race tests                                                           | PASS (`2.102s`) |
| `GOWORK=off GOTOOLCHAIN=local GOFLAGS=-mod=readonly go test ./... -run '^$'`         | PASS            |
| `go vet ./internal/migration` after the final test-safety patch                      | PASS            |
| production-identical candidate `go vet ./...` and `go build ./...`                   | PASS            |
| production-identical candidate Linux amd64/arm64 test compile and build              | PASS            |
| `gofmt -l`, `git diff --check`, fixed-query registry and pre-expression AST firewall | PASS            |

An attempted standard `go test ./... -count=1` run is deliberately **not** recorded as PASS. It
accidentally used the host-default Go `1.26.5`, then reached the existing 10-minute package timeout while
executing an unrelated runner fixture path, with no catalog assertion failure. The same focused runner
test under the pinned Go `1.26.6` was also slower on a clean base-tree replay than on this candidate.
That bounded observation does not turn the full suite into either a catalog regression or a green Gate;
it remains explicitly non-green for this record.

## 5. Local PostgreSQL matrix

Each matrix leg used a fresh temporary container and a dedicated database named exactly
`cag_catalog_parse`. Live tests required the explicit `CLOUD_AGENTS_REQUIRE_CATALOG_PARSE_TEST=1` gate,
refused any other database name, refused a pre-existing `cloud_agents` schema or Cloud Agents roles,
and cleaned only objects they created. All three containers were removed after the run.

| PostgreSQL image                                                                                          | Local image ID                                                            | Result                                                                                       |
| --------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------- |
| `postgres:15.18` / `postgres@sha256:6eb0add3b77c081df18aa518ce43df58fdcc40f2e6d868a6fd08038dc7acd425`     | `sha256:1659a1a994f204ed0397ff17a73d72d27128bfa7d420bd7288ad5e8eb28fa588` | fixed queries PASS; representative PASS; heads 000001/000002 PASS                            |
| `postgres:16` / `postgres@sha256:95206741a5b214807675e14165369d05b93a9cf692223b616d07cca227e74b0b`        | `sha256:02ad0fee02aedf51870d43f109a106a7a8f36930ce9b467ee755254470f227d8` | fixed queries PASS; representative PASS; heads 000001/000002 PASS                            |
| `postgres:17-alpine` / `postgres@sha256:742f40ea20b9ff2ff31db5458d127452988a2164df9e17441e191f3b72252193` | `sha256:8ee7c900f4de054e8f0a3b42b8f5da38b8fba57c3ccd7c1a02be28cb0b06494f` | fixed queries PASS; representative PASS; heads 000001/000002 PASS; wider `MAINTAIN` rejected |

The structural outputs were same-bits across all three majors:

| Fixture                                   | SHA-256                                                                   |
| ----------------------------------------- | ------------------------------------------------------------------------- |
| representative relation/function/children | `sha256:ed03ef759f3e25f1d40c89c6ed38f2fe8c4be187e8a67c1a9a6b6a7514bf9c52` |
| checked-in head `000001`                  | `sha256:85155ccfea43e3aab32af694cc94d1f33622a7f7a11726b305a1376a93981d18` |
| checked-in head `000002`                  | `sha256:48694e1b20956979e47b96d7a23699ac63e84f98be157ed0b6290159376e2bdb` |

This is an arm64 local matrix. It is not the A2.1b-impl-3 dual-instance/two-snapshot-mode matrix and
does not establish cloud or x86_64 equivalence.

## 6. Explicit non-claims and next boundary

This record is implementation evidence, not an independently signed immutable Gate record. It does
**not** prove or enable:

- `cloud-agents-sql-expression/v1`, `pg_node_tree` normalization or expression equality;
- a successful exported `ProjectCatalog`, runner/CLI integration, DB `Connect`, SQL execution, ledger
  insertion or commit;
- signed expected subject publication, deployment trust-root wiring, N-1/PITR or recovery closure;
- production database writes, deployment, Platform RC, Beta, GA, release, image or module publication;
- A2.1b-impl-3 matrix/review, final supply-chain refresh or any aggregate Gate closure.

The next authorized boundary is A2.1b-impl-2 expression normalization. Until it and the later matrix /
review slice close, catalog publication remains `UNPUBLISHED_BOOTSTRAP_MUTABLE`, runtime introspection
and the public projector remain `NOT_IMPLEMENTED`, and every P1 aggregate Gate remains `IN PROGRESS`.
