# P1-A2.1b-impl-2 PostgreSQL catalog expressions — 2026-08-17

- Status：**IMPLEMENTATION + LOCAL PG15/16/17 EXPRESSION MATRIX — PASS；PUBLIC PROJECTOR CLOSED；Gates OPEN**
- Fixed implementation commit：`3b3f8f6c9628ebae4fe0774e46de8e10b5067d54`
- Fixed base commit：`675023eb9c38f12b292898d2a0c16d4fc8f3ebb0`
- Fixed source tree：`451f795739d8f65f4361610bd3fcee15fa45919d`
- Branch：`codex/cloud-agents-platform-p1`
- Evidence date：2026-08-17 Asia/Shanghai
- Executor：Codex P1 implementation executor
- Independent Gate reviewer：**not assigned**

## 1. Closed scope

This slice completes the package-private expression half of ADR-0010 A2.1b without enabling the
production catalog projector:

1. one additional sealed fixed query reads every column default/generated expression, constraint
   expression, index predicate/expression term, policy `USING`/`WITH CHECK`, trigger `WHEN` and function
   argument default from the same owned snapshot;
2. raw `pg_node_tree` is used only as a closed node-kind/cardinality witness. A bounded lexer and Pratt
   parser consume PostgreSQL's non-pretty deparse into the version-neutral
   `cloud-agents-sql-expression/v1` AST;
3. the retained AST has a closed node set: column, constant, function, operator, boolean, null test,
   array, scalar-array operator, SQL value and cast. Constants retain typed logical values, not raw Datum,
   OID, raw node text or deparse text;
4. every AST is rebound to its owning relation or function. Column name/type, projected or allowlisted
   function signature/return type, operator operand/result type and array/cast semantics must match
   exactly;
5. every function/operator reference requires the exact owner-to-reference `normal` dependency edge.
   Missing, swapped or conflicting dependency facts fail body validation;
6. expression source kind/field/ordinal/cardinality is closed, all structural expression slots must be
   consumed exactly once, and aggregate normalized nodes are bounded by the fixed inclusive 4,096-node
   catalog limit;
7. the trigger branch derives only the parenthesized `WHEN` expression from fixed
   `pg_get_triggerdef(..., false)` output, because `pg_get_expr(tgqual, tgrelid)` cannot safely deparse
   `NEW`/`OLD` range-table variables;
8. an AST firewall permits the structural and expression completion seams only in their two reviewed
   package-private callers.

The exported `PGProjector.ProjectCatalog` remains fail closed with
`MIGRATION_PROJECTION_NOT_IMPLEMENTED`. No runner, CLI, signed expected subject, DB mutation or durable
authority can consume the ordinary completed body in this slice.

## 2. Closed grammar and semantic boundary

The parser rejects subqueries, collations, `AT TIME ZONE`, statement terminators, trailing tokens,
unknown node tags, invalid UTF-8, delimiter/cardinality drift and expression forms outside the fixed
profile. Supported built-ins and operators are exact signatures; projected calls are limited to
ordinary `function` objects from the same completed body. Procedure, aggregate or window identities
cannot become expression-call authority.

Validation is deliberately repeated at the signed-body boundary. A shape-correct literal AST cannot
change a column type, function return type, operator result, owner or dependency edge and retain a valid
catalog projection. The completed result is still an ordinary clone, not a verified contract, permit or
runtime authority.

## 3. Fixed implementation scope

The implementation commit changes exactly 9 files: `2466` insertions and `34` deletions. It changes no
`go.mod`, `go.sum`, migration SQL, checked-in catalog JSON or public production entry point.

| File                                                                            | SHA-256                                                            |
| ------------------------------------------------------------------------------- | ------------------------------------------------------------------ |
| `services/control-plane/internal/migration/projection_api.go`                   | `5e1555713a840e19838f6b7ec4e3f305f34ee26161c554c0302a0f15e26f4808` |
| `services/control-plane/internal/migration/projection_catalog_postgres_test.go` | `9c2c0fbea47f351422ad983d5f91e8833f103532c51e8713d42aa23da0c9e363` |
| `services/control-plane/internal/migration/projection_catalog_test.go`          | `7827ec6a58f3312bdd9c7b55491597d03f30c14bb867d8c9726eb403181cde4d` |
| `services/control-plane/internal/migration/projection_expression.go`            | `09f9cd70e7ccd780c2b22917f86de4e9715382ba6d0b44bccf991b7f1e1e7b63` |
| `services/control-plane/internal/migration/projection_expression_queries.go`    | `46b36c97f2c933f7d1de2b4c1b3fec805bc9a003dc2fd58e8d851fda7cb204d0` |
| `services/control-plane/internal/migration/projection_expression_test.go`       | `8cf2bd15db4bb7c7f5a6e73b177f0631256a7e909f6e033dfcf76639678719e0` |
| `services/control-plane/internal/migration/projection_pg_adapter.go`            | `f3cb08ede14bc183505f5e53131621d2bb70e154bf65618641aabe1ffea7da72` |
| `services/control-plane/internal/migration/projection_pg_adapter_test.go`       | `8cf8ae67aa894f86f0b1bbaec7125154bfe662f1287073090d8be3af96d77930` |
| `services/control-plane/internal/migration/projection_validation.go`            | `60bb66d9de0f616a7cf8ea2a61a26d7cdda5263714978d24c8b0bbc1737a60a7` |

## 4. Deterministic local gates

The fixed source used the pinned Go `1.26.6 darwin/arm64` toolchain.

| Gate                                                                                                     | Result          |
| -------------------------------------------------------------------------------------------------------- | --------------- |
| focused projection/catalog/expression tests                                                              | PASS (`0.519s`) |
| focused catalog/expression race tests                                                                    | PASS (`2.043s`) |
| `go vet ./...` and `go build ./...`                                                                      | PASS            |
| Linux amd64 and arm64 all-package test compile via `/usr/bin/true`                                       | PASS            |
| `gofmt`, `git diff --check`, fixed-query registry, source-coverage faults and internal-seam AST firewall | PASS            |

A standard all-package test is deliberately **not** recorded as PASS. The repository's long migration
runner suite exceeded a bounded 90-second diagnostic run while continuing to make normal test progress;
the subtest active at timeout, `TestRunnerCommitIntentFaultsRollbackWithoutTransactionCommit/append-error`,
passed by itself in about four seconds. Earlier default-timeout runs of the same pre-existing suite also
did not produce a usable full green result. There was no expression assertion failure, but this evidence
record does not convert an incomplete aggregate run into a Gate.

## 5. Local PostgreSQL matrix

Each leg used a temporary container and the dedicated database name `cag_catalog_parse`. The explicit
test gate refused another database name, an existing `cloud_agents` schema or existing Cloud Agents
roles. All three containers were removed after the run.

| PostgreSQL image                                                                                          | Local image ID                                                            | Result                                                                   |
| --------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------- | ------------------------------------------------------------------------ |
| `postgres:15.18` / `postgres@sha256:6eb0add3b77c081df18aa518ce43df58fdcc40f2e6d868a6fd08038dc7acd425`     | `sha256:1659a1a994f204ed0397ff17a73d72d27128bfa7d420bd7288ad5e8eb28fa588` | fixed query + representative expressions + heads 000001/000002 PASS      |
| `postgres:16` / `postgres@sha256:95206741a5b214807675e14165369d05b93a9cf692223b616d07cca227e74b0b`        | `sha256:02ad0fee02aedf51870d43f109a106a7a8f36930ce9b467ee755254470f227d8` | fixed query + representative expressions + heads 000001/000002 PASS      |
| `postgres:17-alpine` / `postgres@sha256:742f40ea20b9ff2ff31db5458d127452988a2164df9e17441e191f3b72252193` | `sha256:8ee7c900f4de054e8f0a3b42b8f5da38b8fba57c3ccd7c1a02be28cb0b06494f` | same PASS set; wider non-owner/non-baseline `MAINTAIN` remained rejected |

The outputs were same-bits across PG15, PG16 and PG17:

| Fixture                                       | SHA-256                                                                   |
| --------------------------------------------- | ------------------------------------------------------------------------- |
| representative completed expression body      | `sha256:7bac87487db06292603a1513c137aecada1b15bb2a40dd7ff8297e760c236f5b` |
| representative expanded structural fixture    | `sha256:439a79a35e719634b3e3398a2a4d9ee8f84536f6f4f8ed08210f26e61811ee48` |
| checked-in head `000001` structure            | `sha256:85155ccfea43e3aab32af694cc94d1f33622a7f7a11726b305a1376a93981d18` |
| checked-in head `000001` completed expression | `sha256:a6877e819f9a070975e1c6f2e4ff601f821bd56f7086e14bf0b2e1d8eee9650f` |
| checked-in head `000002` structure            | `sha256:48694e1b20956979e47b96d7a23699ac63e84f98be157ed0b6290159376e2bdb` |
| checked-in head `000002` completed expression | `sha256:6f6b1a0aa58d1525fb1115f7a75c928ab35a643ad1d469ac5fa05929417a790e` |

The representative fixture now includes a generated expression, check, policy predicate, function
argument default, expression index, partial predicate and trigger `WHEN`. This remains a local arm64
single-instance/single-snapshot-mode matrix, not the impl-3 acceptance matrix.

## 6. Explicit non-claims and next boundary

This implementation evidence is not an independently signed immutable Gate record. It does **not**
prove or enable:

- successful exported `ProjectCatalog` or signed expected projection binding;
- runner/CLI configuration, DB `Connect`, SQL execution, ledger insertion or commit;
- three-major × dual-instance × two-snapshot-mode conformance or the complete fault matrix;
- production database writes, deployment, Platform RC, Beta, GA, release, image/module publication;
- N-1/PITR, physical controller power loss, final supply-chain refresh or aggregate Gate closure.

The next authorized boundary is A2.1b-impl-3 matrix/review. Until that slice closes the signed expected
binding and production entry under its fixed matrix, catalog publication remains
`UNPUBLISHED_BOOTSTRAP_MUTABLE`, runtime introspection and the exported projector remain
`NOT_IMPLEMENTED`, and every P1 aggregate Gate remains `IN PROGRESS`.
