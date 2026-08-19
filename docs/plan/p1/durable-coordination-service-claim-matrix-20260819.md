# P1-A2.3 generated-profile service, claim and PostgreSQL matrix - 2026-08-19

- Status: **LOCAL IMPLEMENTATION AND MATRIX VALIDATED - INDEPENDENT REVIEW PENDING**
- Fixed source: `59ec26037ddeba7157f358693426ca1fe2d6231e`
- Fixed tree: `6164f2c2e23fc6801b9f606b7de98e6f1514813d`
- Parent: `a9826e4e7e3605bfe6ff04d06d4270df20b81353`
- Branch: `codex/cloud-agents-platform-p1`
- Decision: [ADR-0013](../adr/0013-p1-durable-coordination-contract.md)
- Authorization: user approval on 2026-08-19 for the ordered
  `contract/state-machine registry -> append-only PostgreSQL kernel -> service/claim/matrix/independent review`
  direction
- Does not claim: independent review, HTTP/P2, a production delivery adapter or external side effect,
  PlatformOperation/attempt/receipt/finalizer execution, production database mutation, deployment, release, or any
  Gate closure

## 1. Generated profile is the only service input

The service does not accept a caller-authored operation profile, state machine, retry policy, outbox class, TTL, or
profile digest. `scripts/generate-platform-durable-coordination-go.ts` reads the checked-in generated registry and
emits the package-private Go profile. Its check is now part of `platform:contracts:check`, and the generation lock has
a separate non-Gate pipeline that binds both generator inputs and the generated Go output.

The only admitted profile remains:

| Fact                                        | Exact value                                                               |
| ------------------------------------------- | ------------------------------------------------------------------------- |
| profile ID                                  | `managedAgentCreateProject/v1alpha1`                                      |
| profile digest                              | `sha256:059b4cca58f9621e9b70b723fb3b681f62948d6d4965af60105165afce680d5a` |
| registry digest                             | `sha256:11c0f599e8320668a6f601241206c795933b26e3b9c456a58353a0d13c7ecd30` |
| state-machine digest                        | `sha256:5c4fa5c0cfac253b45a41c2e49ee7e863b9efbe124e5d743e041f5e01f5c6f15` |
| policy digest                               | `sha256:95023973eb007a958a3c5aea3ac61b6caa7cd8955b9a24fcef3ad269230c64e8` |
| permission / scope                          | `projects.create` / `organization`                                        |
| replay TTL / outbox class                   | `86400` / `resource_change`                                               |
| creates PlatformOperation / external effect | `false` / `forbidden`                                                     |

`coordination.Profile` has no exported fields. Production AST regression coverage rejects any non-generated
`operationProfile` construction and any additional `Profile` constructor. The typed PostgreSQL service independently
rechecks the generated values before a transaction begins, derives the subject digest from `authz.SubjectRef`, and
performs the `projects.create` authorization against the organization scope inside the same SERIALIZABLE tenant
transaction as the idempotency claim or settlement.

## 2. Append-only `000008` service kernel

`000008_add_durable_coordination_service.sql` is a forward-only migration with 34 classified statements. It neither
rewrites `000007` nor broadens the seven-table schema kernel. It adds:

- profile-specific idempotency claim, success and failure functions;
- global leader acquire/renew functions with database-clock expiry and monotonically fenced tokens;
- outbox claim, acknowledge, retry, dead-letter and expired-claim reap functions;
- two private helpers for append-only coordination audit and exact outbox transition.

Runtime receives EXECUTE on exactly ten typed functions. PUBLIC cannot execute any of the twelve functions, and
runtime cannot execute either private helper. No table DML grant is added. Every tenant mutation validates the
runtime principal closure, exact tenant GUC, generated profile identity, bounded identifiers/digests and full stale
claim tuple before changing state. The Go service maps successful commit, closed rejection and ambiguous commit to
the distinct `committed | rejected | unknown` outcome union; it never retries an unknown commit.

The internal outbox dispatcher accepts only an unexported, test-injected port. There is no production implementation
of that port, no HTTP route, no P2 adapter, and no call to network or provider code. The generated profile cannot
create PlatformOperations, and the matrix proves that operation/finalizer facts remain empty.

## 3. Fixed artifacts

| Artifact                                      | SHA-256                                                            |
| --------------------------------------------- | ------------------------------------------------------------------ |
| `000008_add_durable_coordination_service.sql` | `c64e92c778318571169f0b949abcd342efcd2c3b429c68969c143d413a82a43c` |
| generated Go profile                          | `c3687534cbf3d7218aeeb1a32591ab0f70561d985de2860e0c502efb51c3f454` |
| Go coordination service                       | `d4ba5c4e8580b760a57ac8f7947019315dc8928a6a4954cf24c0c28414232497` |
| service unit tests                            | `5473e9a024070bd3437383a6cc08b067551b16a2e02aaef6d45d09ba7656c43e` |
| PostgreSQL integration test                   | `ad4e34c2d96cb33f959de3b32ec891a5935353577b7db39f1bf1e1d4681c36e8` |
| PG matrix script                              | `7dded1e7fd682e10e26bc53ee3c945d455ea083a4c0d67f0c8db5129df2214c0` |
| generation lock                               | `451b50d215732210451dcf1b503eb99267b5b2d23d4a216f84b16b0a5243c55c` |
| checked-in schema bundle bytes                | `68efef5dc192323c4ec31cb46e7dda3aecbeb5dba4032876f6f85138d6a80dcd` |
| checked-in manifest bytes                     | `f9e9f7d5561428b022464a37cf99510a827d9c6808f1e50f5b1df89d4f8c81e4` |

Generated bundle identities are:

- schema bundle digest: `sha256:9084475d8db1e74afeb0d77ffaf9e253c4e6b6c67c1ba09a7c45483a42cc15ab`;
- manifest digest: `sha256:d896285b8835751c7c1567d01c955bd6c44b84586c25a0a9bbba7b01fde8eacc`;
- deterministic runtime archive: `sha256:2bee1a8c98dcdce32d21406d05e15bb317495f574e572e48c612ccfe4f61754d`;
- bootstrap digest/archive remain byte-identical at
  `sha256:db95649924f259cfa320e897bd5e0934c35fcc9009d8492a69ec5dc71132081c` and
  `sha256:6654946d58f707d48c71740a41407674c34b5fbeced2e38eeb6c8d1bb08ae175`.

The bundle remains `UNPUBLISHED_BOOTSTRAP_MUTABLE`; runtime catalog introspection and signing/publication remain
`NOT_IMPLEMENTED`.

## 4. Local validation

The fixed source passed the following affected-boundary validation:

- pinned Node `24.13.1`, Bun `1.3.14`, Go `1.26.6` `platform:contracts:check`, including generated registry, generated
  Go profile and byte-exact generation lock;
- migration bundle semantic check and byte-exact generator `--check`;
- migration-bundle plus contract-lock tests: 21/21;
- all TypeScript/package tests: seven package suites plus 11 script files / 132 script tests;
- affected Go packages normal and race; all-package compile-only, vet and build;
- Linux amd64 and arm64 CGO=0 all-package test compilation;
- `go mod tidy -diff`, `go mod verify`, repository lint, affected-file format and `git diff --check`;
- shell syntax and a scoped new-file secret-shape scan.

The full Go `go test ./...` attempt entered the repository's existing long-running migration package and produced no
failure before the bounded local wait was stopped. It is therefore neither reported as a failure nor claimed as a
full-suite pass. The affected coordination/postgres packages passed complete normal and race runs, while every Go
package passed compile-only, vet and build.

The repository-wide `oxfmt . --check` remains non-passing only for three pre-existing HEAD files outside this slice:
the two checked-in dependency reviews for pgx/x-text and x/sys, plus the representative PostgreSQL catalog fixture.
Every file changed by this slice passes the formatter; those unrelated bytes were not rewritten.

The executable matrix then passed all nine fixed legs:

| PostgreSQL | Exact image                                                                        | Legs                         |
| ---------- | ---------------------------------------------------------------------------------- | ---------------------------- |
| 15.18      | `postgres@sha256:6eb0add3b77c081df18aa518ce43df58fdcc40f2e6d868a6fd08038dc7acd425` | normal / race / direct-fault |
| 16.14      | `postgres@sha256:95206741a5b214807675e14165369d05b93a9cf692223b616d07cca227e74b0b` | normal / race / direct-fault |
| 17.10      | `postgres@sha256:742f40ea20b9ff2ff31db5458d127452988a2164df9e17441e191f3b72252193` | normal / race / direct-fault |

Each fresh container applied `000001` through `000008`. The matrix covered authorized and denied same-transaction
claims, replay/conflict/success/failure, concurrent serialization, leader busy/stale/correct fencing, full outbox
claim tuple settlement, retry/dead-letter, expired-claim reaping, direct ACL denial, exact final state counts, and
zero operation/finalizer/secret-shaped facts. It never pulls an image, reuses a database, or contacts an external
delivery target.

## 5. Review and Gate boundary

Independent review is deliberately **PENDING** for fixed source `59ec26037ddeba7157f358693426ca1fe2d6231e`.
This record is the implementation/matrix handoff, not a reviewer verdict. Until a separate reviewer fixes the exact
source, reruns the required gates and returns a graded verdict, A2.3 slice 3 remains unchecked in the tracker.

Even a later approval of this slice cannot by itself close `G-DATA`, `G-AUTHORITY-P1`, `G-SECURITY-P1`,
`G-SUPPLY-CHAIN`, or any aggregate Gate. HTTP/P2, external delivery, production credentials/database wiring,
deployment, publication, RC, Beta and GA remain outside this authorization.
