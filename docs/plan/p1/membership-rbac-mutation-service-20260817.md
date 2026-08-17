# P1-A2.2-impl-3 Membership/RBAC mutation service — 2026-08-17

- Status：**IMPLEMENTED — mutation/service/matrix/supply slice；independent review pending**
- Fixed implementation：`350b53c72b62ea2bb33b8399aeabb1a1c8727a4c`
- Base implementation：`de36ca343cb2790c8ba44186807d6c933bd98500`
- Fixed repository tree：`921c7171da4319a8b6ce92fd0d552b8e6f66626f`
- Fixed `services/control-plane` subtree：`9e782e74dcaa16b89a4238b9f3b7b4ab2ac84b26`
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
   entrypoints: Membership create/suspend/revoke and RoleBinding bind/revoke;
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
6. schema head `000004`, strict TS/Go SQL classification, generated catalog/manifest/runtime bundle, archived
   exact `000003` predecessor and refreshed quota facts.

Runtime retains no table DML and no helper-function authority. Ordinary mutation rejects platform scope and
`platform.admin`; those remain outside this tenant runtime boundary.

## 2. Fixed bits and generated identity

| Artifact                                      | SHA-256                                                            |
| --------------------------------------------- | ------------------------------------------------------------------ |
| `000004_expand_membership_rbac_mutations.sql` | `7664c8e67e8ee5f561395bd58f7156ac437ba0ac20547216dedbcc89dbc3cba7` |
| archived `000003` schema bundle               | `a4bd9503c1c11c7bcfc48249f501fd258ff09ad2354d4c042f298bb20c705820` |
| `schema-bundle.json` raw file                 | `aa2b1d13c1230a66df90e8af7f6f1d6cbbc1e3fb16f5c334d4cbafed30ca4053` |
| `manifest.json` raw file                      | `c4c7471671aa1c932c0f845a7bea956bcb65501a8e4debb230901d8efa195f77` |
| `catalog/schema-000004.json`                  | `0b9246f8d2ceded8d26cdf401841d2078cf9ccce96d6946269f5f805769dc043` |
| Go mutation service                           | `15e274f13a22cf77b01d1d42fc1b1b03e2a891ab404116c5ee696ca3c3797140` |
| Go mutation unit tests                        | `19c4d28c4cdcc41c5d5c108c24d291051f4143da4507272e192e57bc71480ed3` |
| Go mutation integration test                  | `bd69c2ca9ed50b48c285700bcd41f00baeb6e6d1b5cbc133ba6b2f3c8c3eb233` |
| PostgreSQL matrix script                      | `52591f18cef8d1d68dae404fffaeeed68ad947cec844948cbde47653963fd1c4` |
| generation lock                               | `d9f80ce224991009157c3d9e126b5900b08e6551ed3f62d644061651de1e27da` |
| source-bound dependency lock                  | `a5356cd8d9da860f246cb2992739b48aeef347a466fa7f5dc2557577292e571e` |
| CycloneDX 1.6 SBOM                            | `c3496ec99169d635fb0d4e7278947d44d612358b05fbd74c182dd063555fa1fd` |
| `THIRD_PARTY_NOTICES.md`                      | `1cadb7fc75886f9085a53d3b9cc174b4c024981f609e4d5951e4e3f877dcbb48` |

The generated identities are:

- schema bundle digest `sha256:49f5f50076bb06ceeb68c7b8d6f2a37260ec7aca50681bf4d28149364039be91`;
- manifest digest `sha256:09353c9be78d97cd61657bdc6b19b635fec240a905369139b866d8c3237632f0`;
- deterministic runtime tar `sha256:c0108b92ea4712b491b58a9bd85e958798f77777cfe7ae4abb5140f04c25b8c4`
  at 388,096 bytes;
- unchanged bootstrap bundle/tar `sha256:db95649924f259cfa320e897bd5e0934c35fcc9009d8492a69ec5dc71132081c`
  / `sha256:6654946d58f707d48c71740a41407674c34b5fbeced2e38eeb6c8d1bb08ae175`;
- statement counts `[20, 71, 46, 20]`, ten journal segments, 988 journal records, 987 checkpoint
  records, 157,024,256 journal bytes, 991 index records, 16,465,920 index bytes and 173,490,176 combined
  reserved bytes. No evidence quota limit was widened.

## 3. PostgreSQL 15/16/17 mutation matrix

The fixed script used only the existing exact local images, created a fresh UTF8/C/C database for each major,
applied migrations `000001` through `000004`, seeded deterministic actor authority through the migration owner,
and ran the authorization and five-operation mutation integration tests in normal and race modes:

| PostgreSQL | `server_version_num` | Exact image digest                                                                 | Result             |
| ---------: | -------------------: | ---------------------------------------------------------------------------------- | ------------------ |
|         15 |             `150018` | `postgres@sha256:6eb0add3b77c081df18aa518ce43df58fdcc40f2e6d868a6fd08038dc7acd425` | normal + race PASS |
|         16 |             `160014` | `postgres@sha256:95206741a5b214807675e14165369d05b93a9cf692223b616d07cca227e74b0b` | normal + race PASS |
|         17 |             `170010` | `postgres@sha256:742f40ea20b9ff2ff31db5458d127452988a2164df9e17441e191f3b72252193` | normal + race PASS |

Each major proved the five-step lifecycle, before/after authorization, exact and future expiry, cross-tenant
actor denial, one-winner/two-contender serialization with no tenant-revision gap, audit/resource fact identity,
and privilege counts `5/0/0/9/0/0` for runtime entrypoints/helpers/direct DML/owner/PUBLIC/bootstrap. A direct
cross-tenant function call failed inside a rollback-only transaction. A temporary runtime-group member with
`CREATEROLE` was rejected by the direct-login fence and removed after the fault. This is local arm64 evidence,
not x86_64/cloud/production or immutable Gate evidence.

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

The fixed follow-up `350b53c` changes only the Go result-settlement helper and its unit faults. It passed the
focused mutation suite in normal/race modes, the complete `internal/store/postgres` package in normal/race
modes, package vet/build and `git diff --check`. A delta-only scan applied the repository's four secret-shape
rules to all eight changed files since `230f1e3` and passed; the full-history scan was not rerun for this
follow-up. The previously passed full migration and six-leg PostgreSQL matrix were not rerun because SQL,
schema/catalog fixtures, module inputs and transaction behavior are byte-identical; this record does not
relabel those base runs as a fresh full rerun.

The source-bound refresh mechanically fixes:

| Evidence                                    | Exact value                                                        |
| ------------------------------------------- | ------------------------------------------------------------------ |
| Source commit                               | `350b53c72b62ea2bb33b8399aeabb1a1c8727a4c`                         |
| Repository tree OID                         | `921c7171da4319a8b6ce92fd0d552b8e6f66626f`                         |
| `services/control-plane` subtree OID        | `9e782e74dcaa16b89a4238b9f3b7b4ab2ac84b26`                         |
| 335-file tracked manifest SHA-256           | `9610a836174add6974e8fa549184f0944e9b4541cf6f723cfa0cea1c5fc7d259` |
| 258-file tracked Go-source manifest SHA-256 | `24e97df1d7e5fa3a74ed8a37438666f18cd07d73b0c52db8a71c3ef221ffe0ff` |

`go.mod`, `go.sum`, the 16-module selected graph and all production import closures remain same-bits. Linux
amd64/arm64 both retain 7 modules / 30 packages with hashes
`48ca0dbaba0f918d99091decd0520a70327c36badb7d74c7cbbe1e180cd66e5f` /
`12a56c91f56460e9757560f00234c06cec462f248df6a770d41172168e9a8d08`; Darwin arm64 retains 6 / 29 with
hashes `12203596417e4926a8292ad208df4d410ef0d6e89627320e2c4fe08858a5154b` /
`07d05153aff50a4db408a9e4d34c4a298a21f5ccd5615b9940e4e8521e0de354`. NOTICE and its three PATENTS
bindings are unchanged.

Fresh source-bound `govulncheck v1.6.0` module and Linux/amd64 symbol scans for `350b53c` used a scanner built
with exact Go `1.26.6` and database timestamp
`2026-08-14T16:22:54Z`; both returned no findings with output SHA-256
`3016e51e4eac0d421674d2128bbbdefb2924b4646e0c14a1ab034977ad73fae5`. A fresh OSV query over all 16
selected modules returned 16 responses and zero findings; query/canonical-response SHA-256 remain
`b0ab3c0cbc9e84fba34f1b183c9ae65dfa58c635a823d06c33914619f763d911` /
`ab5a0787744e90d4b9bef630420e8085dd8045f7cd5fe87fc0b5acc7b6a55b93`. These empty results are time-bound
and non-bit-safe, not permanent safety facts.

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
