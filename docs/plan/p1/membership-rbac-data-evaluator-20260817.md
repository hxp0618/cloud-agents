# P1-A2.2-impl-2 Membership/RBAC data and evaluator — 2026-08-17

- Status：**IMPLEMENTED — data/read-evaluator slice only**
- Fixed implementation：`e36e1cfc1715a81af4bd9acb0c226e8d3356a75d`
- Fixed tree：`0f2d0a1084a0e88fff176b12c0999ddc79e61b83`
- Governing decision：[`ADR-0011`](../adr/0011-p1-membership-rbac-contract.md)
- Toolchain：Node `24.13.1` / Bun `1.3.14` / Go `1.26.6`
- Accountable owner：hxp0618
- Evidence executor：Codex
- Independent reviewer：not assigned for this slice

This record fixes the second A2.2 implementation boundary. It adds migration-owned storage and a
tenant-bound, read-only authorization path. It does not add a Membership/RoleBinding mutation service,
publish the mutable bootstrap catalog, wire the production runner/CLI trust root, write a production database,
close an immutable/aggregate Gate, deploy or release anything.

## 1. Implemented boundary

The fixed implementation adds:

1. migration `000003` with global `builtin_roles` / `builtin_role_permissions` and tenant-scoped
   `memberships` / `role_bindings`; the two tenant tables use exact composite foreign keys, generated closed
   scope keys, lifecycle constraints, `ENABLE/FORCE ROW LEVEL SECURITY` and runtime `SELECT` only;
2. exact migration-only seed statements for seven role rows and 141 role-permission rows. The TS and Go SQL
   classifiers admit only the two fixed statement indexes, targets and SHA-256 identities; no generic
   production `INSERT` profile was opened;
3. a pure Go default-deny evaluator that verifies the exact built-in catalog digest, byte-exact SubjectRef
   digest, Membership admission, RoleBinding role/version, resolved database ancestry, permission, revocation
   and the strict expiry predicate `now < expires_at` before granting;
4. one tenant-bound read-only pgx transaction that resolves the requested Tenant/Organization/Project from
   database-owned state, reads the complete bounded role catalog and reads bounded subject candidates. It
   exposes neither raw SQL nor a write transaction and never authorizes `platform.admin` through the ordinary
   tenant runtime path;
5. schema-head `000003`, its strict generated catalog, archived exact predecessor bundle and updated
   migration/runtime bundle closure.

## 2. Fixed bits

| Artifact                            | SHA-256                                                            |
| ----------------------------------- | ------------------------------------------------------------------ |
| `000003_expand_membership_rbac.sql` | `89178a9eeb81acc4863d98e9cb9388c2f50379d78f28f5f2570acec551901cd2` |
| `schema-bundle.json` raw file       | `a4bd9503c1c11c7bcfc48249f501fd258ff09ad2354d4c042f298bb20c705820` |
| `manifest.json` raw file            | `5ea25d3b80e937c06a35171eb34e6b9d8043259eaaaf20619b4cebed1df7598e` |
| `catalog/schema-000003.json`        | `dccdcd5b6d0349b5b1bf7c6b6f211f96fabd1eb291baae18b11cca36a5a0189d` |
| Go evaluator                        | `6a8d02cd2294a5f8f5af9f7629b2e314b4d69b5c825fe418ff748a0bd2d75e31` |
| Go evaluator tests                  | `6b47dcb543d2d9facbccc83282484d12464f26eedb852f8947e47f45c1fcd3e0` |
| pgx authorization read path         | `6fc77bc19776f8c04d1811a2054ea8260559d438d0b447db3d9536dd254d737d` |
| pgx unit tests                      | `131d54a58276350b00470d5bcf138e9a9c333e9e5cc09cb9d774c08907850362` |
| pgx integration test                | `38d283dfe1d78a3677ec093f8b6d1e400e48898902372aac6a095949096bb1de` |
| PostgreSQL matrix script            | `9d11a4f936bfeaad3c35d04a9568121711011dbd36563fba09efbc2640454186` |
| generation lock                     | `8453c7cb944113af84bac49f21a79039d1c37613e42b62bf2851dbe10c93d655` |

The generated identities are:

- schema bundle digest `sha256:c6652bef99a83b9a8a76739ef7d84e19321feaa80730c548bb7c50191aec3c23`;
- manifest digest `sha256:febb9bd6c27ab25a0ed5014feff137dbd3d06b0d4c4c98c7852c6bea2362891d`;
- deterministic runtime tar `sha256:c56cc51c0d8b0808fa0eca719c9e80574774961785ea566ca1672bd5e4b1990a`
  at 243,200 bytes;
- unchanged bootstrap bundle/tar `sha256:db95649924f259cfa320e897bd5e0934c35fcc9009d8492a69ec5dc71132081c`
  / `sha256:6654946d58f707d48c71740a41407674c34b5fbeced2e38eeb6c8d1bb08ae175`;
- archived predecessor self digest
  `sha256:52aea3c0a5fe5270d13a2bf194aedcc3ce0817fe3183dd868d427f7582f7819d`.

## 3. PostgreSQL 15/16/17 focused matrix

The fixed script used only already-present exact local images, created a fresh UTF8/C/C database for each
major, applied `000001` through `000003`, bootstrapped two tenants, inserted deterministic migration-owner
facts and ran the same runtime conformance test in normal and race modes:

| PostgreSQL | `server_version_num` | Exact image digest                                                                 | Result             |
| ---------: | -------------------: | ---------------------------------------------------------------------------------- | ------------------ |
|         15 |             `150018` | `postgres@sha256:6eb0add3b77c081df18aa518ce43df58fdcc40f2e6d868a6fd08038dc7acd425` | normal + race PASS |
|         16 |             `160014` | `postgres@sha256:95206741a5b214807675e14165369d05b93a9cf692223b616d07cca227e74b0b` | normal + race PASS |
|         17 |             `170010` | `postgres@sha256:742f40ea20b9ff2ff31db5458d127452988a2164df9e17441e191f3b72252193` | normal + race PASS |

Each leg observed exactly seven role rows and 141 permission rows. It proved runtime SELECT/no-INSERT
privileges on all four tables, SQLSTATE `22023` for an unbound tenant read, tenant-001/tenant-002 RLS
isolation, exact allow evidence, permission/future-permission denial, issuer case sensitivity, cross-tenant
scope denial and ordinary-runtime platform-scope denial. This is a local arm64 functional matrix, not an
x86_64/cloud/production or immutable Gate claim.

## 4. Verification and observed boundary

PASS on the fixed source boundary:

- focused authz/store/migration normal and race tests, including exact quota refresh for the three-migration
  runtime bundle;
- full Bun package/script tests (`122/122` script tests), build, lint and typecheck;
- exact-toolchain contract lock, migration checker/generator, migration bundle same-bits, Go module boundary,
  `go vet ./...`, `go build ./...`, `go mod tidy -diff`, `go mod verify` and Linux amd64/arm64 CGO=0 compile;
- shell syntax/executable-mode check, targeted formatting, full-history secret scan and `git diff --check`;
- the six PostgreSQL matrix legs above.

The full `go test ./...` and `go test -race ./...` migration package runs are **not claimed as PASS**: both
reached the existing 10-minute package timeout while the highly parallel migration fault suite was still
running. The run also exposed a stale checked-in bundle quota expectation; that exact fixture was updated for
statement counts `[20, 71, 46]`, 9 segments, 858 records and 151,076,864 reserved bytes, then passed focused
normal/race verification. No assertion failure remains in the changed boundary.

Full `fmt:check` is also not newly claimed: it still reports the three pre-existing HEAD files
`pgx-v5.10.0-x-text-v0.39.0-implemented-closure.md`, `x-sys-v0.44.0.md` and
`catalog-representative.json`; none is part of this implementation diff, and their historical evidence bits
were deliberately not rewritten.

## 5. Remaining A2.2 boundary

A2.2-impl-3 must add the mutation/service authority for Membership and RoleBinding, bind its audit/resource
version transitions, repeat the required matrix at the fixed implementation, complete source-bound supply
refresh and obtain an independent implementation review. The checked-in production catalog remains
`UNPUBLISHED_BOOTSTRAP_MUTABLE`, runtime introspection/signing/publication remain `NOT_IMPLEMENTED`, and the
runner/CLI still rejects before database access. `G-DATA`, `G-AUTHORITY-P1`, `G-SECURITY-P1` and
`G-SUPPLY-CHAIN` remain open.
