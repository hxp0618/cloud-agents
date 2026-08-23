# Gate candidate record: `G-SECURITY` / P1 / R1

- Evidence ID：`CAG-G-SECURITY-P1-20260823-R1`
- Record type：`PHASE`
- Phase / aggregate Gate：P1 security foundation / `G-SECURITY`
- Prerequisite record IDs：`CAG-G-INVENTORY-P0-20260810-R3`、`CAG-G-BASELINE-P0-20260823-R4`
- Supporting current-source records：`CAG-G-CONTRACT-P1-20260823-R4`、`CAG-G-DATA-P1-20260823-R1`、
  `CAG-G-AUTHORITY-P1-20260823-R1`
- Accepted durability boundary：D-048 / ADR-0024
- Supersedes：none；this is the first current-source `G-SECURITY-P1` phase candidate
- Status：`IN PROGRESS`
- DRI：hxp0618（owner）；Codex current-source evidence executor
- Independent reviewer：`PENDING`
- Date：2026-08-23 Asia/Shanghai
- Gate effect：none；this record does not close `G-SECURITY-P1` or aggregate `G-SECURITY`

## Scope

This record binds the current P1 tenant/RBAC, compatibility-recovery, runner-recovery, generated-contract and
durability-policy security facts to Inventory R3 and Baseline R4. It separates fresh bounded checks, exact reviewed
files/subtrees and historical supporting records. Same bits inherit only the original review's explicit scope.

Current source has narrow default-deny identity/RBAC, tenant-transaction cleanup, typed SQL, redacted recovery and
profile-separation controls. It does not contain a production OIDC/JWT/JWKS verifier or HTTP server, does not have a
fresh whole-schema PostgreSQL tenant/RLS/pool-isolation matrix, and does not have a current-source vulnerability,
complete secret-flow or runtime request-limit closure. The checked-in database authority/catalog remains unpublished
and runtime-not-implemented. These are blockers, not passes inferred from absent runtime surfaces.

D-048/ADR-0024 makes physical controller/cache-loss optional hardening rather than mandatory P1 evidence. It does not
turn clean `poweroff` into crash evidence or complete durability: closure still needs the accepted no-sync externally
observed bare-metal crash, ext4/XFS/QEMU combination, exact post-boot verification, current-source phase evidence and
independent Gate review.

## Fixed source and prerequisites

- Candidate source commit/tree：`f399c89bb0f2cc9ee4c62a1aa7bbd61332fe0992` /
  `3e21c1c888761ecd131dfd94038dbc96f623fa08`
- Source branch：`codex/cloud-agents-p1-g-authority-r1-status-20260823`
- Evidence branch：`codex/cloud-agents-p1-g-security-r1-current-20260823`
- Source state before this evidence-only change：clean；upstream `0/0`；remote source branch exact
- Current subtrees：
  - `contracts`：`40f0f3b44f83c986f9b015d059451e195e285c0a`
  - `sdk`：`e4c5abf9d9cb591df39d9377529c201a1307997e`
  - `scripts`：`d65f14ec5cc8b2bda27af056673e891cda8cebd1`
  - `services/control-plane`：`689942aecbc7f84f692dd71c17d66a607d12b950`
  - `internal/authz`：`427eb08fb69bef7750b0d8b02de84fb6d2bda35e`
  - `internal/store/postgres`：`6580be0bbbb3e1056439da0698ec08cca5d46e33`
  - `internal/migration`：`e01ddef945d4cec352ac107831c9c86af029ff86`
  - `internal/evidencefs`：`5e4b0b76a8f1b8c6408b4505a8befa8e5e3369ed`
  - `internal/mountauthority`：`f64f4c47e96843ff8dde38c401d138ade9b0eaaf`
  - `services/control-plane/scripts`：`6337c5c73ce21ac1487a680352db7393185274da`
  - `services/control-plane/migrations`：`d4911935523374018393f3b13e24c8b8f698343a`
- Inventory R3 record SHA-256：
  `d865bb9fd1195342a7251bd6822130437753f5be4af8312d9d5c5ee4d5b71400`
- Baseline R4 record / review SHA-256：
  - `57429377291d1b6a41ff886cde2a6692afd63b5c15adf0677767d59e87b03dd9`
  - `44db2df153bbfcc5fa0bd4c928bbdf9b207c60c4458ec61b2e2557c7d97d4c94`
- G-CONTRACT R4 record / review SHA-256：
  - `0982261244e7315c2798db4a4f0913f7f93037c251140c8f14ed2cbc3bcd7152`
  - `f0d5b12f1f6e0f2936783868331d4d74d7a3ee0fc49b3e370894b95884458f61`
- G-DATA R1 record / review SHA-256：
  - `1cf34ac76778f28dc790ac2d6b780b0d7b526826d9b2d8596065a610d80d7a0d`
  - `78233500ce5758115e153c09c342692eda08e212de451fc21e589e03bbd9708b`
- G-AUTHORITY-P1 R1 record / review SHA-256：
  - `60c6feb7b08f19f91e0e022d519303ca412b028b1b11c41cd83ba9780140ed79`
  - `5ec821b07c8a49b4fa8c7750a926980b160b47b0d95e3d75f4505b1f7945430f`
- ADR-0024 / its original independent review SHA-256：
  - `597f0d9881aabe44e9d67876ae81d83808a18c3725f5c7ac66279bcda53e0bd0`
  - `0b7fb81292e507b9bf15204d44670027c41bf7e83efe07612758217a5c5a712e`
- Gate criteria SHA-256：`4a3d0b3c184e9673944411adbc5c8ea933883c855d5aada67862dad8e4dcc994`
- Toolchain used for fresh Go checks：Go `1.26.6` darwin/arm64, `GOWORK=off`, `GOTOOLCHAIN=local`,
  `GOFLAGS=-mod=readonly`
- Deployment profile：none；no production database, migration, HTTP/P2/provider, deployment or publication action

## Exact reviewed scopes

Only these bounded conclusions are inherited:

1. G-CONTRACT R4 covers the exact current `contracts`/`sdk`/`scripts` generation and strict wire-authority scope. It
   does not prove runtime identity verification, tenant isolation or request enforcement.
2. The ADR-0012 lineage/quota remediation review applies exactly to the current `internal/authz` subtree and append-only
   `000006_close_subject_issuer_validation.sql`. It proves a closed lexical issuer/subject profile, profile separation
   and direct invalid-issuer rejection only. It does not implement signature, audience, key rotation or revocation.
   Record/review SHA-256：
   - `6d070a31b0ef785728f68d777068b92ecd8aef0337fde6f3fd5c2d6d6c13def3`
   - `7cf2afec8be08e235883d1c57d2acefde125eb8fa424c14378972d14f5fb2279`
3. The A2.3 durable-coordination remediation review applies exactly to current `000009`, its typed PostgreSQL service
   and local matrix script. It proves operation-specific identity alignment, conflict redaction and role-separated
   generated claims within that slice only. Remediation review SHA-256：
   `e134bce8d1acb9a998c3463e87c0c802da126bf6e5a7727767ca76c33af152b4`.
4. The A2.4 compatibility/recovery v2 review applies exactly to current `000011`, its typed service/tests and local
   matrix script. It proves fixed SQL selection, role separation, one-shot claims and stable redacted errors within
   that slice only. Record/review SHA-256：
   - `6948e034faeed7ba003a3ac06714c31fafdc324f8a89aea58a0d9d1d1a3d1ff2`
   - `c7330ac38f8b72b0002f7bf08ad2b91ae46e40456d46531a32ed8ae11df84185`
5. ADR-0023 Slice G has the exact current `internal/migration` subtree. Its review covers ordered local recovery,
   redacted failure precedence, one-shot profile separation and forbidden external writer edges only. Record/review
   SHA-256：
   - `f6f0805c1621ef933c3f29af03123b7f2757ae41d253796128672b64f642bc06`
   - `5cf26966f873c563ba2bc6e84d8b94ebe237534b2d95a137ea89b44db8ce030c`
6. G-DATA R1 and G-AUTHORITY-P1 R1 contribute current open-boundary and source identity facts. Their `APPROVE` verdicts
   do not close their own Gates or this security record.
7. ADR-0024 and its review are exact accepted policy bytes. They change the required durability boundary but do not
   turn any historical filesystem result into current security closure.

The exact current security-relevant files retained from the reviewed slices include:

| File / narrow boundary                                               | SHA-256                                                            |
| -------------------------------------------------------------------- | ------------------------------------------------------------------ |
| `000006_close_subject_issuer_validation.sql`                         | `5b24e8462c90b7d430717ac746e00f999b21f21eae0fb855444379807c0b47e5` |
| `internal/authz/rbac.go`                                             | `24ecbf7cccf62491f5e8b7f5fdbec240a70c7b47525669dfb7b57b532c80235f` |
| `internal/authz/rbac_test.go`                                        | `442877d7bb6b823a2dd37e7d002154788b76c10288ef7428ee32d2f29407fd79` |
| `000009_redact_coordination_conflicts.sql`                           | `b4c544a1a1a0236fa485f55bcfce8e03d759b977290f7c22b5f598e4e657cc90` |
| `internal/store/postgres/durable_coordination.go`                    | `2126be99c3a1b46c8a82f6ed38026261d9d1d0b3e06d832052b5a504f66a839c` |
| durable-coordination local matrix script                             | `7cb524794c5ebb5723f43d8e7e31355309aecfe412a09d409a03b8141a0999a6` |
| `000011_add_compatibility_recovery_writer.sql`                       | `67811ee604d01732d5ed19e4c0c108013f2acd95a0f9eefc716fd6b1072a2b61` |
| `internal/store/postgres/compatibility_recovery.go`                  | `fa71f017095a8046af48215c79fb168713dcc75b9754b0b5446f0b2acd014a35` |
| `internal/store/postgres/compatibility_recovery_test.go`             | `373e277c17154d8396a24ea76a347bc7e6fd21ff10a2a2baeccdecb0747e0cc3` |
| `internal/store/postgres/compatibility_recovery_integration_test.go` | `27680002001c38dfb90d5746ea03c9141df8b38bb4e59bd2df484f4f67b2f20d` |
| compatibility-recovery local matrix script                           | `b4eaf315d1ff1370162aaaf72159c7f90b0748f76d3ca2d35d9c9a90edee9fef` |

## Current machine-readable trust state

| Current descriptor                  | SHA-256                                                            | Exact production status                                                                                          |
| ----------------------------------- | ------------------------------------------------------------------ | ---------------------------------------------------------------------------------------------------------------- |
| `catalog/authority-v1.json`         | `eb8c4ad607dc3443471fa376a9da9bf49e17788ffcc9cda6d2ccecd982327ccd` | `UNPUBLISHED_BOOTSTRAP_MUTABLE` / `NOT_IMPLEMENTED`                                                              |
| `catalog/global-table-authority-v4` | `cceee87df70fc145d24a7d66220dd012adc68f77af1c7aa964312583afc5b42d` | `UNPUBLISHED_BOOTSTRAP_MUTABLE` / `NOT_IMPLEMENTED`                                                              |
| `catalog/schema-000012.json`        | `c424e9a62180c8e3de4cb444d95812c2606c6355065f4fa7e5655fcd733dab48` | `UNPUBLISHED_BOOTSTRAP_MUTABLE` / `NOT_IMPLEMENTED` / `NOT_IMPLEMENTED_A2_1B_REQUIRED`；no `expected_projection` |
| migration `manifest.json`           | `97c02a54639d9a7d00dbc55a14e06db8e97bc2c36444cf51b61a680539cfd44e` | current public lineage and descriptor paths                                                                      |
| migration `schema-bundle.json`      | `948e504b77c409065d2160056f45356d84d136d2512f35a4c4fe9e16e575aaaf` | generated current bundle；not a published trust root                                                             |

These descriptors are bootstrap inputs, not runtime-verified trust authority. Their internal consistency does not
substitute for OIDC/JWT verification, published database authority or executable catalog projection.

## Historical and stale-source evidence not inherited

Earlier membership/RBAC mutation, catalog, trusted-mount, production EvidenceSink, ext4/XFS/QEMU barrier and
host-crash records remain useful provenance. Their broader verdicts are not inherited when their fixed subtree differs,
their scope is narrower or they lack an independent review. In particular, the old membership mutation record itself
still says independent review pending；only the later exact ADR-0012 authz/issuer slice is inherited here. Historical
filesystem records are not a current reviewer-signed filesystem Done result.

Current dependency artifacts are also stale-source evidence rather than current security closure:

| Artifact                                      | Current file SHA-256                                               | Bound source / security status                                                                |
| --------------------------------------------- | ------------------------------------------------------------------ | --------------------------------------------------------------------------------------------- |
| `services/control-plane/dependency-lock.json` | `3db792de6bc692bcdaf7e75140a4d193e7d99eb64f1bf36f48d25dbe23b6106f` | source `f731c6b…`; vulnerability scan source `350b53c…`; inheritance `NOT_CLAIMED`            |
| `services/control-plane/sbom.cdx.json`        | `d0a2e9afd9e6f9f74638799aaefafed69855c4a591d46a19a51a08493b7920fc` | source `f731c6b…`; generated `2026-08-18T14:58:19Z`; current vulnerability scan `NOT_CLAIMED` |
| `contracts/generation.lock.json`              | `4f2953540e9305f034a8f6fc7d13af0947d7f5b91f43b7ce6256bc137d071c76` | current generation binding；`notGateClosure=true`                                             |

The historical `govulncheck v1.6.0` and OSV zero-finding results used database timestamp
`2026-08-14T16:22:54Z` and source `350b53c…`. They are not current, permanent or inherited safety evidence.

## Fresh bounded checks

Five exact identity/RBAC tests ran with Go `1.26.6`:

```bash
GOWORK=off GOFLAGS=-mod=readonly GOTOOLCHAIN=local go \
  test ./internal/authz -count=1 -timeout=2m \
  -run '^(TestSubjectRefCanonicalFixtureAndExactIdentity|TestSubjectRefIssuerUsesClosedAbsoluteURIProfile|TestEvaluateDefaultDenyAndScopeContainment|TestEvaluateActiveNarrowMembershipSurvivesInactiveBroadMembership|TestEvaluateIntegrityFaultsReturnErrors)$'
```

Result：PASS in `1.167s`.

Six exact tenant transaction/authorization tests ran separately:

```bash
GOWORK=off GOFLAGS=-mod=readonly GOTOOLCHAIN=local go \
  test ./internal/store/postgres -count=1 -timeout=2m \
  -run '^(TestTenantAuthorizationUsesOneBoundReadTransactionAndExactQueries|TestTenantAuthorizationStoredSubjectDigestDriftFailsClosed|TestTenantAuthorizationOperationalAndDecodeFailuresRollback|TestTenantReadReusesPhysicalConnectionForNullAndEmptyClear|TestTenantReadNonEmptyPostTransactionGUCHijacks|TestRBACMutationServiceMapsConflictAndUnknownCommit)$'
```

Result：PASS in `1.503s`.

Five exact recovery redaction/profile tests ran separately:

```bash
GOWORK=off GOFLAGS=-mod=readonly GOTOOLCHAIN=local go \
  test ./internal/migration -count=1 -timeout=2m \
  -run '^(TestPublicRunnerRedactsCurrentEvidenceVerifierFailureBeforeArtifactRead|TestProjectionErrorsAreBoundedAndRedacted|TestRunnerLedgerRecoveryProfilesSeparateExecutionAdmissionAndSuccessWriter|TestRunnerLedgerRecoveryProfilesRejectLiteralAndGraphMutation|TestRunnerLedgerRecoveryAdmissionRejectsUnknownPairBeforeDatabase)$'
```

Result：PASS in `0.814s`.

Three exact trust-before-read, symlink and SQL-authority-smuggling tests ran as one final narrow group:

```bash
GOWORK=off GOFLAGS=-mod=readonly GOTOOLCHAIN=local go \
  test ./internal/migration -count=1 -timeout=2m \
  -run '^(TestRejectingTrustHappensBeforeArtifactRead|TestFileArtifactSourceNeverFollowsSymlink|TestStrictDDLGrammarRejectsAuthorityAndTailSmuggling)$'
```

Result：PASS in `1.271s`.

A production-source scan found zero non-test Go files containing `OIDC`, `JWKS`, `JWT` or `JOSE`, and zero non-test
production HTTP-server files. The two OpenAPI documents contain `BearerAuth` / `bearerFormat: JWT`; those are wire-format
placeholders, not a cryptographic verifier or runtime enforcement. Absence is recorded as a blocker, not a pass.

Strict `jq -e` checks reproduced the three descriptor states, required schema-000012 to omit `expected_projection`, and
bound the manifest to the public lineage plus exact global-table and database-authority descriptor paths.

No full `internal/migration`, broad race, live PostgreSQL, filesystem/crash, remote host, current vulnerability/OSV,
whole-history secret or external-effect test ran for this record.

## Exit criteria mapping

| `G-SECURITY-P1` criterion                                                  | R1 candidate result          | Evidence / open boundary                                                                                        |
| -------------------------------------------------------------------------- | ---------------------------- | --------------------------------------------------------------------------------------------------------------- |
| composite tenant FK, FORCE RLS, runtime-role and pool-context isolation    | PARTIAL / NOT CURRENT-CLOSED | exact narrow SQL/services and focused unit cleanup tests；no fresh whole-schema live PG isolation matrix        |
| management/service/workload identity validation and crypto negative matrix | LEXICAL ONLY                 | exact SubjectRef/issuer fail-closed profile；no OIDC/JWT/JWKS signature/audience/key lifecycle verifier         |
| secret/token/pairing exclusion and redacted durable/log/watch errors       | PARTIAL EXACT REVIEWED BITS  | exact A2.3/A2.4/Slice G stable-redaction scopes；no complete current receipt/outbox/log/trace/backup/SDK audit  |
| SQL parameterization, limits, backpressure, dependency/license/secret scan | NOT COMPLETE                 | typed fixed SQL exists；HTTP limits/runtime absent；dependency/SBOM/vulnerability and full secret closure stale |
| waiver ownership/scope/expiry                                              | NOT EXERCISED                | no current waiver is used；a future waiver must satisfy the Gate rule                                           |
| P2+ Worker/Host/deployment/cutover security non-claim                      | PASS AS BOUNDARY             | this record does not implement or verify later-phase security                                                   |
| ADR-0024 current durability combination and filesystem Done                | NOT VERIFIED                 | physical hard-power optional；required crash/matrix/current-source independent aggregation absent               |
| immutable current-source independent review                                | PENDING                      | this R1 awaits a fresh P0/P1/P2 review                                                                          |

## Hard blockers and non-claims

- No production OIDC/JWT/JWKS verifier, signature/algorithm/audience validator, unknown-`kid` handling, key rotation,
  revocation or clock-skew matrix exists.
- No fresh whole-current-source PostgreSQL tenant/composite-FK/FORCE-RLS/runtime-role/BYPASSRLS/connection-pool
  isolation matrix or aggregate review exists.
- Checked-in authority/catalog descriptors remain `UNPUBLISHED_BOOTSTRAP_MUTABLE` / `NOT_IMPLEMENTED`; production
  trust-root publication and executable catalog projection are absent.
- Current dependency lock/SBOM do not bind the candidate source, current vulnerability inheritance is `NOT_CLAIMED`,
  and no current candidate-wide dependency/license/secret/waiver closure exists.
- There is no production HTTP enforcement surface for request/body/decompression limits, rate limit, deadlines or
  watch backpressure. Their absence cannot satisfy those exit criteria.
- No reviewer-signed current filesystem Done or complete ADR-0024 durability combination exists. Physical controller,
  host power and cache-loss remain unclaimed optional hardening；clean `poweroff` is not crash evidence.
- No production database, migration, HTTP/P2/provider action, deployment, publication, release, Beta, GA or Gate
  transition occurred.

## Invalidation

R1 becomes stale if Inventory R3, Baseline R4, G-CONTRACT R4, G-DATA R1, G-AUTHORITY-P1 R1 or ADR-0024 is invalidated；
if any fixed source/subtree/file/record hash changes；if identity, tenant/RLS, redaction, request-limit, dependency,
secret, waiver, authority/catalog or durability semantics change；or if a new runtime surface changes the P1 security
scope. A superseding record must replay each changed criterion and receive a new independent review.

## Sign-off

- DRI conclusion：current source preserves reviewed narrow identity/RBAC, compatibility and runner-recovery security
  slices while cryptographic identity, whole-schema isolation, current supply/secret evidence, request enforcement and
  durability aggregation remain explicitly open.
- Reviewer conclusion：`PENDING`.
- Closure decision：none；`G-SECURITY-P1` and aggregate `G-SECURITY` remain `IN PROGRESS`.
