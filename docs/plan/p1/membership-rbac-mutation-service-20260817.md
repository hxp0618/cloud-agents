# P1-A2.2-impl-3 Membership/RBAC mutation service — 2026-08-17

- Status：**IMPLEMENTED — mutation/service/matrix/supply slice；independent review pending**
- Fixed implementation：`2dc443d043b0c7aa535422b15cbd409660e73985`
- Base implementation：`de36ca343cb2790c8ba44186807d6c933bd98500`
- Authority follow-ups：`350b53c → afe6cb2 → 1ff7713 → 2dc443d`
- Fixed repository tree：`baac4d7ba09f6f1b38178341df9357764e75051f`
- Fixed `services/control-plane` subtree：`141e3dedcb230e18cf209462ebaa5c449ebcdbd9`
- Governing decision：[`ADR-0011`](../adr/0011-p1-membership-rbac-contract.md)
- Toolchain：Node `24.13.1` / Bun `1.3.14` / Go `1.26.6`
- Accountable owner：hxp0618
- Evidence executor：Codex
- Independent reviewer：not assigned for this slice

This record fixes the third A2.2 implementation boundary. It adds five typed tenant mutations, exact
PostgreSQL function authority, normal/race PG15/16/17 conformance and current source-bound supply evidence.
It does not publish the mutable production catalog, add a public HTTP mutation route, implement A2.3
operation/outbox coordination, write a production database, close an immutable/aggregate Gate, merge to
`main`, deploy or release anything.

## 1. Implemented boundary

The fixed implementation adds:

1. forward migration `000004`, with four internal helpers and exactly five `SECURITY DEFINER` runtime
   entrypoints, plus append-only `000005`, which replaces only `bind_role` without changing its signature,
   owner or ACL;
2. exact `pg_catalog, cloud_agents` search paths, direct nondelegable runtime-principal validation,
   transaction-local tenant binding and a final same-transaction blanket PUBLIC function revoke;
3. a package-internal `RBACMutationService` exposing only the five typed methods. Every method validates
   ordinary inputs before mutation, opens one tenant-bound `SERIALIZABLE` read-write transaction, evaluates
   the actor in that transaction, invokes one fixed SQL function and never accepts raw SQL or a callback;
4. compare-and-swap tenant/resource revisions, exact resource state/UID result verification and one matching
   redacted audit fact plus resource-change fact per accepted mutation;
5. fail-closed connection handling: callback failure rolls back, commit ambiguity discards the connection,
   returns `ErrMutationCommitUnknown` and releases no `MutationResult`; any failed commit likewise returns a
   zero result, while a cleanup failure after confirmed commit discards the connection without returning a
   retry-inviting mutation failure;
6. schema head `000005`, strict TS/Go SQL classification, generated catalog/manifest/runtime bundle, retained
   exact predecessor evidence and refreshed quota facts;
7. a closed Membership→RoleBinding authority edge: bind requires an exact-subject active, unexpired
   Membership covering the requested scope, and authorization candidates require that Membership's tenant
   resource version to predate the binding. Suspend/revoke or later re-admission therefore cannot reactivate
   an older binding, while another still-active earlier Membership may continue to cover it.

Runtime retains no table DML and no helper-function authority. Ordinary mutation rejects platform scope and
`platform.admin`; those remain outside this tenant runtime boundary.

## 2. Fixed bits and generated identity

| Artifact                                        | SHA-256                                                            |
| ----------------------------------------------- | ------------------------------------------------------------------ |
| `000004_expand_membership_rbac_mutations.sql`   | `7664c8e67e8ee5f561395bd58f7156ac437ba0ac20547216dedbcc89dbc3cba7` |
| `000005_close_membership_binding_authority.sql` | `9256cf2fe9684e9fa4e92519cf16af7c26a3713930171e6bba7013b0a748a69b` |
| archived `000003` schema bundle                 | `a4bd9503c1c11c7bcfc48249f501fd258ff09ad2354d4c042f298bb20c705820` |
| `schema-bundle.json` raw file                   | `42f4d02a85e37111f722851a034bd886c7a8666d7590887933cb7b4e58ab2202` |
| `manifest.json` raw file                        | `10e537aca7799838406574833fcd4f3fe7d51ad80aa37597b591695bea398147` |
| `catalog/schema-000004.json`                    | `0b9246f8d2ceded8d26cdf401841d2078cf9ccce96d6946269f5f805769dc043` |
| `catalog/schema-000005.json`                    | `a601975275586307c9b051f7493be70f7130fee956649096f8a2759a849bbbf4` |
| Go RBAC candidate reader                        | `3cad9aa14ff064af8b402a1727f040c0c3ca8a1cb7ff180aefd1b01a4791b65e` |
| Go candidate-reader unit tests                  | `5f62ea4d0562b7fc686f40bfd84396e8f368c4f6b072a79640458387b6dcf201` |
| Go mutation service                             | `6f7781ad9ef8bb342919e031b4eee0aa199eb3f5ea3bd2f0f5a6d959ea70e283` |
| Go mutation unit tests                          | `e812b18486e5452072f5b04d8c3162d60058c4f020bd9eb3796e2b2fd052f347` |
| Go mutation integration test                    | `a72d62df21e63e6423d1609ca10c51a4903ace5f9ff7169748ec3ffdf6a8bc93` |
| PostgreSQL matrix script                        | `7a2195e172b85e54ed27c867808ad7a2b2b9c4cf9d88ab6e45057cf1a829a6b1` |
| generation lock                                 | `20e8d1fb0a6c0217a3b74e7876d0c18ab698ff5b791bcbcc5111112f42e79b08` |
| source-bound dependency lock                    | `cbb19c26c5035c369e9a0b38a1036c0ea96dff859cc06de9e63ae2b4c1a7ae26` |
| CycloneDX 1.6 SBOM                              | `38adf9b594c26f602f87d8582710a566c11c036cf5857424375525f384f94c78` |
| `THIRD_PARTY_NOTICES.md`                        | `1cadb7fc75886f9085a53d3b9cc174b4c024981f609e4d5951e4e3f877dcbb48` |

The generated identities are:

- schema bundle digest `sha256:a289a298b4f3358e1aceb53e54baee2851b907e520c2f97ebf14c2f2c306e484`;
- manifest digest `sha256:286824767ff87fb91260849a40aff95f15ce874698bc44fc8480689465f71a25`;
- deterministic runtime tar `sha256:d7f7030684b8c5dab963a8a803a3d0c0d5415c263d3436bc5d38f5a711545b98`
  at 501,248 bytes;
- unchanged bootstrap bundle/tar `sha256:db95649924f259cfa320e897bd5e0934c35fcc9009d8492a69ec5dc71132081c`
  / `sha256:6654946d58f707d48c71740a41407674c34b5fbeced2e38eeb6c8d1bb08ae175`;
- statement counts `[20, 71, 46, 20, 1]`, ten journal segments, 1,003 journal records, 1,002 checkpoint
  records, 158,597,120 journal bytes, 1,006 index records, 16,711,680 index bytes and 175,308,800 combined
  reserved bytes. No evidence quota limit was widened.

## 3. PostgreSQL 15/16/17 mutation matrix

The fixed script used only the existing exact local images, created a fresh UTF8/C/C database for each major,
applied migrations `000001` through `000005`, seeded deterministic actor authority through the migration owner,
and ran the authorization and five-operation mutation integration tests in normal and race modes:

| PostgreSQL | `server_version_num` | Exact image digest                                                                 | Result             |
| ---------: | -------------------: | ---------------------------------------------------------------------------------- | ------------------ |
|         15 |             `150018` | `postgres@sha256:6eb0add3b77c081df18aa518ce43df58fdcc40f2e6d868a6fd08038dc7acd425` | normal + race PASS |
|         16 |             `160014` | `postgres@sha256:95206741a5b214807675e14165369d05b93a9cf692223b616d07cca227e74b0b` | normal + race PASS |
|         17 |             `170010` | `postgres@sha256:742f40ea20b9ff2ff31db5458d127452988a2164df9e17441e191f3b72252193` | normal + race PASS |

Each major proved the five-step lifecycle, before/after authorization, exact and future expiry, cross-tenant
actor denial, one-winner/two-contender serialization with no tenant-revision gap, audit/resource fact identity,
binding-without-Membership and wider-scope rejection, and deny-after-revoke-and-re-admission for the historical
binding. Privilege counts remain `5/0/0/9/0/0` for runtime entrypoints/helpers/direct DML/owner/PUBLIC/bootstrap.
A direct cross-tenant function call failed inside a rollback-only transaction. A temporary runtime-group member
with `CREATEROLE` was rejected by the direct-login fence and removed after the fault. This is local arm64
evidence, not x86_64/cloud/production or immutable Gate evidence.

## 4. Verification and source-bound supply evidence

The base implementation `de36ca3` passed:

- full `go test ./... -count=1 -timeout=25m`; `internal/migration` completed in 1162.809 seconds;
- focused mutation/store/migration normal and race tests, including commit ambiguity, confirmed-commit cleanup,
  result drift, concurrency, exact grants and quota closure;
- all script tests (`122` tests / `1330` expectations), contract and migration checkers/generators, lint,
  typecheck and build;
- `go vet ./...`, `go build ./...`, `go mod tidy -diff`, `go mod verify`, Linux amd64/arm64 CGO=0 compile,
  shell syntax, secret scan, dirty-file formatting and `git diff --check`;
- all six PG15/16/17 normal/race matrix legs above.

The result-settlement follow-up `350b53c` passed its focused and complete store normal/race tests. Authority
follow-up `afe6cb2` added append-only `000005`; `1ff7713` bound that migration into the contract-lock input
closure; final implementation `2dc443d` reduced the migration to one `bind_role` replacement and added the
candidate-reader version fence so the frozen evidence quota remains unchanged in policy and within limits.

For exact source `2dc443d`, this refresh passed:

- strict migration SQL and deterministic bundle tests (`19` tests / `443` expectations), current contract and
  migration checker/generator `--check`, and the exact five-migration quota assertion;
- affected PostgreSQL candidate-reader and mutation service tests in normal/race modes, plus selected
  checked-in bundle/admission/quota tests in normal/race modes;
- the fresh PG15/16/17 matrix in section 3, including authorization and mutation normal/race on every major;
- repository lint, exact Go `1.26.6` vet/build, Linux amd64/arm64 CGO=0 cross-build, `go mod tidy -diff`,
  `go mod verify`, shell syntax, applicable format checks and `git diff --check`;
- the repository's full-history four-rule secret scan with no unallowlisted secret-shaped content.

A fresh full `go test ./...` or complete `internal/migration` rerun is not claimed for `2dc443d`; the current
normal/race evidence is deliberately restricted to the changed bundle/quota/store boundary.

The source-bound refresh mechanically fixes:

| Evidence                                    | Exact value                                                        |
| ------------------------------------------- | ------------------------------------------------------------------ |
| Source commit                               | `2dc443d043b0c7aa535422b15cbd409660e73985`                         |
| Repository tree OID                         | `baac4d7ba09f6f1b38178341df9357764e75051f`                         |
| `services/control-plane` subtree OID        | `141e3dedcb230e18cf209462ebaa5c449ebcdbd9`                         |
| 337-file tracked manifest SHA-256           | `ca5795f0fe9100164778b0d5fc8b307efee64f1280ec94533ef2426fdb74418b` |
| 258-file tracked Go-source manifest SHA-256 | `7253371288f7f7bc3fdc17019bd235ab9ff5ecdd2e2a33e68644ce0b51b8aa3e` |

`go.mod`, `go.sum`, the 16-module selected graph and all production import closures remain same-bits. Linux
amd64/arm64 both retain 7 modules / 30 packages with hashes
`48ca0dbaba0f918d99091decd0520a70327c36badb7d74c7cbbe1e180cd66e5f` /
`12a56c91f56460e9757560f00234c06cec462f248df6a770d41172168e9a8d08`; Darwin arm64 retains 6 / 29 with
hashes `12203596417e4926a8292ad208df4d410ef0d6e89627320e2c4fe08858a5154b` /
`07d05153aff50a4db408a9e4d34c4a298a21f5ccd5615b9940e4e8521e0de354`. NOTICE and its three PATENTS
bindings are unchanged.

The CycloneDX document retains 16 unique component refs and exactly seven root production dependencies. It
passed Ajv `8.20.0` validation against the fixed CycloneDX specification `1.6` bom/SPDX/JSF schemas with
SHA-256 `3e92dddbc30cf7f6a02b80f0942b1a4cfd4fb1c26f1dfc4310afa9d613cafb93`,
`baa9d3bd1ed57b6751b0887edead6b5063ff53ff7429cf85d476c6c94af0166e` and
`8bae002c25e723db7ee1f26afde680ae1a2b1a8f6b4b4b0fd65dc3becb090aae`.

The recorded `govulncheck v1.6.0` module/Linux-amd64 symbol and 16-module OSV zero-finding results belong to
source `350b53c` and database timestamp `2026-08-14T16:22:54Z`. They were not rerun for `2dc443d`; the
dependency lock and SBOM therefore mark current-source vulnerability-scan inheritance `NOT_CLAIMED`.
Historical output/query hashes remain recorded for provenance only. No zero-finding result is presented as
current or permanent safety evidence.

Repository-wide `fmt:check` is not newly claimed because historical fixed evidence files outside this slice
remain intentionally unformatted by the current formatter. Every changed implementation/evidence file passes
its applicable formatter, and the implementation commit passed `git diff --check`.

## 5. Remaining boundary

Independent implementation review for this fixed hash is still pending, so A2.2-impl-3 is not yet a reviewed
slice closure. The checked-in production catalog remains `UNPUBLISHED_BOOTSTRAP_MUTABLE`; runtime catalog
introspection/signing/publication and production trust-root/runner wiring remain `NOT_IMPLEMENTED`. There is no
HTTP mutation route, A2.3 operation/outbox coordination, production credential, production database write,
x86_64/cloud matrix, deployment or release. `G-DATA`, `G-AUTHORITY-P1`, `G-SECURITY-P1` and
`G-SUPPLY-CHAIN` remain open.
