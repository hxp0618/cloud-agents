# P1-A2.4 compatibility/recovery PostgreSQL kernel evidence - 2026-08-20

- Status: **LOCAL IMPLEMENTATION PASS; INDEPENDENT REVIEW PENDING**
- Fixed base HEAD: `5a0ed7b654162ef61d6b22cdde18a54a778a2188`
- Branch: `codex/cloud-agents-platform-p1`
- Scope: append-only PostgreSQL `000010`, generated migration/catalog evidence, and the local PostgreSQL 15/16/17 schema-only matrix
- Does not authorize: writer/service consumers, HTTP/P2 or provider effects, production database mutation, deployment, release, or any immutable/aggregate Gate closure

## 1. Implemented boundary

`000010_expand_compatibility_recovery_kernel.sql` consumes the generated A2.4 compatibility/recovery registry without changing its historical contract boundary. It adds five owner-controlled tables for workload-principal registration, bounded migration backfills, local logical-restore evidence, live-instance compatibility, and instance-retirement receipts. It also adds five `IMMUTABLE PARALLEL SAFE` digest/profile helpers and seven indexes.

The static migration classifier binds exactly 52 statements. The migration has no `SECURITY DEFINER` function, mutation function, DML, trigger, raw table grant, writer/service adapter, HTTP route, P2/provider path, or external side effect. Runtime receives `EXECUTE` only on the five pure helpers; runtime, bootstrap, and PUBLIC receive no table privileges. `000001` through `000009` remain byte-identical inputs.

The SQL embeds and validates the exact generated authorities:

- registry digest: `sha256:9df9dcf4c9e62cd95b43be362bf5a332bf9637ca881f16fbd25486ad0792f72d`;
- state-machine digest: `sha256:5fb7f076c40aed31d5309a4de6aa2a66b93f3d560a535ecf992dc1f817d8f408`;
- policy digest: `sha256:804ee0280ab5c98a48989abf511659d2a6f801fa5201617c3e436f848dfdc11d`;
- five generated profile IDs and their exact profile digests.

The generated catalog advances the schema head to `000010` and adds `global-table-authority-v3`. Its writer names are catalog declarations for future slices only; this implementation does not create those writers or treat a stored row as authority.

## 2. Deterministic generated evidence

The fixed local toolchain was Node.js `24.13.1`, Go `1.26.6`, and Bun `1.3.14`. Generation and checks produced:

- schema bundle digest: `sha256:a1673fcdf71fd49439ec9cefde2d02c627029799a700913653ed1f1f6fca7f09`;
- bootstrap bundle digest: `sha256:db95649924f259cfa320e897bd5e0934c35fcc9009d8492a69ec5dc71132081c`;
- migration manifest digest: `sha256:7fa7ef8e9aa9eba67c56b8ed1e5b8183c9add4065e3e8c3bb196c4d1fe9d6eeb`;
- deterministic runtime ustar digest: `sha256:8ac00f6e57db8160ee3f48cc249fab2d4032f63eaf44ed1859642cdb0a1f56da`;
- deterministic bootstrap ustar digest: `sha256:6654946d58f707d48c71740a41407674c34b5fbeced2e38eeb6c8d1bb08ae175`.

Reviewed file SHA-256 bindings before commit:

- `000010_expand_compatibility_recovery_kernel.sql`: `ab758a08c07ffb95b9e9a612c90079fcaf54d06407d0cfe4a0368db570f621e6`;
- `global-table-authority-v3.json`: `20e84b349a70d3fe64a3b6b30b0e3458707018e9e4c2d356a52c4cb20b9b9a32`;
- `schema-000010.json`: `a84a02c20244b60d2ffe4d27beb6fa5f5e0db8fb95ef91eef8865bce63412236`;
- generated migration manifest: `2d13d8175f3a3c6eaefc73b1172e1644833e4a7753bd42fa79086e9aa0dd7317`;
- generated schema bundle: `ca5fea1b9f0056439fd2b58af4a796616d9be3e7ec483869f1cb5bb4f5bfdbb8`;
- PostgreSQL matrix script: `ca7235850f9b219076fc2a3499b62990b4dd47fff3a9085ca766c551d4f254e1`;
- regenerated contract lock: `574bce3831ddd7fdbfc5ef94bb9b75c474882a8ddb13cb01103a09ba98df418c`.

The migration bundle checker reports `notGateClosure=true`, `catalogRuntimeIntrospection=NOT_IMPLEMENTED`, `schemaPublicationStatus=UNPUBLISHED_BOOTSTRAP_MUTABLE`, and `signingAndPublication=NOT_IMPLEMENTED`.

## 3. Local verification

Passed:

- compatibility/recovery and durable-coordination registry generators are byte-current;
- 9 focused registry tests and 24 migration-bundle/contract-lock tests;
- platform contract checker: 95 JSON files, 38 schemas, 49 fixture cases, `BOOTSTRAP_VALIDATED`, `notGateClosure=true`;
- deterministic migration-bundle generation/check and contract generation-lock check;
- focused Go migration normal and race tests;
- `go vet ./internal/migration`, `go build ./...`, `go mod tidy -diff`, and `go mod verify` from the control-plane module;
- Go formatting, shell syntax, `git diff --check`, historical migration same-bits, and the no-HTTP/no-provider/no-writer scope scans;
- fresh PostgreSQL schema-only matrix:
  - PostgreSQL 15 image `postgres@sha256:6eb0add3b77c081df18aa518ce43df58fdcc40f2e6d868a6fd08038dc7acd425`: PASS;
  - PostgreSQL 16 image `postgres@sha256:95206741a5b214807675e14165369d05b93a9cf692223b616d07cca227e74b0b`: PASS;
  - PostgreSQL 17 image `postgres@sha256:742f40ea20b9ff2ff31db5458d127452988a2164df9e17441e191f3b72252193`: PASS.

The matrix applies `bootstrap` plus `000001` through `000010` in fresh databases. It verifies table/function ownership and ACLs, helper volatility/parallel safety, valid deterministic owner rows, unknown-profile rejection, time/range/state constraints, duplicate evidence rejection, retirement completeness, and foreign-key rejection. It leaves no labeled test container running.

The repository-wide Go-module checker enters the known long `internal/migration` suite and was intentionally stopped locally rather than reported as success. It is **NOT PASS** and is not closure evidence. No assertion failure was observed before the stop, but that does not upgrade the result. The existing full-migration timeout boundary remains open.

## 4. Remaining boundary

This record is local implementation evidence only. Independent review is pending. A later A2.4 slice must separately define and review versioned registry repair and typed service/writer consumers before any such capability exists. PITR, HA/failover, remote object storage, production restore, production database writes, HTTP/P2/provider effects, deployment, release, immutable closure, and every aggregate Gate remain OPEN.
