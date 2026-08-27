# Durable Project-create migration bundle successor — 000014

Date: 2026-08-27 Asia/Shanghai

## Decision boundary

`APPROVED BOUNDED SUCCESSOR IMPLEMENTATION — GATES OPEN`.

This record introduces one versioned, generated migration-bundle authority for
the append-only `000014_harden_durable_project_create_identifiers.sql`
successor. The canonical v4 migration bundle and the durable Project-create
v2 lineage remain historical predecessors at schema head `000013`; they are
not rewritten or relabelled. The successor is a local/read-only artifact
closure and is not an installation, production database, HTTP, provider, P2,
deployment, publication, release, or Gate operation.

The frozen authority is `D-053-MIG-000014`, revision
`D-053-MIG-000014.r1`. It is a separate migration-bundle successor authority;
the existing `D-053-EC-2.r3` external-consumer authority remains unchanged.

## Frozen source and inputs

The source authority is
[`authority-source.json`](../../../services/control-plane/migrations/successor/000014/authority-source.json).
It freezes:

- the exact predecessor manifest/schema identities and `000013` head;
- the single successor SQL, generated cumulative `schema-000014` catalog,
  successor manifest/schema paths, and predecessor archive path;
- the complete sorted 167-path input set (migrations `000001`–`000014`, the
  generator/library/test and fixture closure, lockfiles, and the
  local/production runner bindings);
- the explicit 14-path exclusion set, including
  `contracts/generation.lock.json`, `go.work`, the historical v2 lineage
  output, the successor outputs, and the pending evidence/review paths;
- the 29-path protected predecessor set containing the canonical `000013`
  manifest/schema, generated catalogs/registries, and the immutable source
  authority itself;
- the bound source-schema and profile-schema descriptors (path, `100644`
  mode, byte size, and SHA-256), with the strict Draft 2020-12 schemas checked
  before semantic assertions;
- the exact-byte archive rule: copy the predecessor schema-bundle bytes under
  the filename derived from its logical SHA-256 digest, with rewriting
  forbidden;
- the deterministic USTAR member rule: ASCII-byte path order, regular
  `100644` members, uid/gid/mtime zero, no compression, duplicate rejection,
  and exactly the standard end blocks.

The generated successor profile is
[`profile.json`](../../../services/control-plane/migrations/successor/000014/profile.json).
It binds the source digest, predecessor fence, successor artifacts, runtime
member-manifest projection (44 records, deterministic digest/size, and
`ABSENT_PENDING` state), receipt paths/state, and the no-side-effect
implementation boundary. The
successor schema bundle is
[`schema-bundle.json`](../../../services/control-plane/migrations/successor/000014/schema-bundle.json),
and the runner-facing manifest is
[`manifest.json`](../../../services/control-plane/migrations/successor/000014/manifest.json).

The generated identity bindings at this revision are:

| artifact                |   bytes | SHA-256                                                                   |
| ----------------------- | ------: | ------------------------------------------------------------------------- |
| `authority-source.json` |  21,698 | `sha256:6436c991dc838c353f27f91f9aff3257d02e18a6c3e0535244fe7f7d1d7a5d8e` |
| successor SQL           |  12,807 | `sha256:49c172785ccf8d8729e610a54f723bf906ad9408962073ef65f6eaebdd1fabea` |
| successor catalog       | 396,279 | `sha256:fe6bee0fac7e99e3db12a14398efd57742691a21e085a0a030fbcb1841e46365` |
| predecessor archive     |  22,443 | `sha256:d5ce27597e2218240a276dbbec01431e4fe26774e195b70445078d8662a3826d` |
| successor manifest      |  36,291 | `sha256:961ccac428f8bf0d55a828fd93ae8e2085ae17d34c09cb2b46c28f653851f8ae` |
| successor schema bundle |  23,970 | `sha256:d90661ac0271b78de563e565bb861b35d570be30fa788131e9813dde56870edc` |
| generated profile       |  22,970 | `sha256:668c7e9c0337d1e50c81dde0ac465561d4ac4eb5f6d14f7fd8b2e26ef672250a` |

The profile's domain-separated `profileDigest` is
`sha256:0637e32e1e07d82ff2917a13f8ade6276c2518ff0aeb7a80451f9da0f69b2630`.

The source-schema descriptor is 9,052 bytes with SHA-256
`sha256:b7ccd78f1c8cc3969b0d2c8846157dc645595e397ab97942522ecfbe163c873c`;
the profile-schema descriptor is 10,010 bytes with SHA-256
`sha256:314cc945796fac537bc89a3c09d2e1d6681fa06e028ea0ddecb3a776eb31e827`.

## Runner and platform fence

The profile's runner projection is deliberately narrow:

- local development entrypoint:
  `services/control-plane/internal/localmigration.Run`;
- production `migration.Runner.Run` is named only as the logical consumer;
  this milestone does not invoke it or mint a production permit;
- logical runtime members retain the standard
  `services/control-plane/migrations/manifest.json` and `schema-bundle.json`
  paths, so the existing verified runtime parser can consume a regenerated
  successor tar without changing the predecessor bundle;
- a complete ledger returns the reviewed `no-op` result; entry and recovery
  writers remain `NOT_IMPLEMENTED`;
- declared toolchain is Go `1.26.6`, Node `24.18.1`, Bun `1.3.14`, on
  Darwin arm64 and Linux amd64. A local toolchain mismatch is recorded, not
  silently promoted to evidence.

The generated runtime tar is a read-only deterministic value (44 members,
3,476,992 bytes, SHA-256
`sha256:1c426708510d3c0217bdc4c544e430a70087eb794a53757865597d9b5ed6ebe0`).
The projected member-manifest body is 11,158 bytes with SHA-256
`sha256:55d02120feba0d986483183f1b40d63ab8deee0ed6c7d5ba4119d84246ad6fc4`;
it is not written to the pending evidence path.
No tar is written as a deployable or published artifact by this milestone.

## Lineage and review rules

The successor is a single-predecessor append-only child:

`canonical v4 / 000013` → `successor profile / 000014`.

All predecessor bytes and evidence remain recoverable. A fresh independent
read-only review must inspect the source/input/exclusion sets, archive and
member algorithms, predecessor and successor digests, runner/toolchain
projection, and non-claims, and must return an explicit `APPROVE` or
`REQUEST_CHANGES` with P0/P1/P2 counts. The review cannot mutate the candidate,
write a database, publish a runtime artifact, or close a Gate.

## Focused verification

The bounded checks are:

```text
bun scripts/generate-platform-migration-bundle.ts --check
bun scripts/generate-platform-migration-bundle-successor.ts --check
bunx vitest run scripts/lib/platform-migration-bundle.test.ts scripts/lib/platform-migration-bundle-successor.test.ts
GOWORK=off GOFLAGS=-mod=readonly go test -tags localdev ./internal/localmigration -count=1
GOWORK=off GOFLAGS=-mod=readonly go test ./scripts/data-recovery-validator -count=1
```

These checks prove deterministic source/catalog/manifest/runtime construction
and read-only local runner admission. They do not close any aggregate Gate or
claim a production migration, replay receipt, deployment, or release.
