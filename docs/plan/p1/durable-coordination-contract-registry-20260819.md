# P1-A2.3 durable coordination contract registry - 2026-08-19

- Status: **LOCAL IMPLEMENTATION VALIDATED - SLICE 1 ONLY**
- Parent ref: `b8c491e4b4e321b351607e5e71419bc821d3c3f2`
- Branch: `codex/cloud-agents-platform-p1`
- Decision: [ADR-0013](../adr/0013-p1-durable-coordination-contract.md)
- Authorization: owner approval on 2026-08-19 for the ordered
  `contract/state-machine registry -> append-only PostgreSQL kernel -> service/claim/matrix/independent review`
  direction
- Does not claim: independent review, migration `000007`, a PostgreSQL or Go consumer, HTTP/P2 external side
  effects, production mutation, deployment, release, or any Gate closure

## Implemented boundary

This slice establishes one generated authority chain:

```text
strict source/profile JSON Schema
  + exact idempotent OpenAPI operation discovery
  + SubjectRef/projection/result schema identity
  + closed state-machine and policy source
      -> deterministic checked-in generator
      -> generated registry
      -> exact profileId + profileDigest for future consumers
```

The only generated profile is `managedAgentCreateProject/v1alpha1`. It binds the existing POST route,
`Idempotency-Key`, request projection, SubjectRef identity, `projects.create`, 24-hour replay policy, redacted
resource reference, `resource_change`, no PlatformOperation, no finalizer, and no external side effect. The source
catalog contains exactly seven sorted state machines; generic validation rejects unknown references, duplicate
`(from,event)` destinations, terminal outgoing transitions, unreachable states, and states without a terminal path.

The generation lock records an independent `durable-coordination-registry-generation` pipeline. Its output summary
is intentionally explicit:

- `runtimeConsumer = NOT_IMPLEMENTED`;
- `sqlConsumer = NOT_IMPLEMENTED`;
- `httpSurface = NOT_IMPLEMENTED`;
- `externalSideEffects = FORBIDDEN`;
- `notGateClosure = true`.

No file under `services/`, no SQL migration, and no public API implementation changed in this slice.

## Exact generated identities

| Fact                               | Exact value                                                               |
| ---------------------------------- | ------------------------------------------------------------------------- |
| contract source manifest           | `sha256:78241e47d86f789af9d152b1e0f74497cd736365e26be4d7673d87da8f55491f` |
| registry input manifest            | `sha256:1e86b1f03f9100854c7c510da7691bcde7bd7807cb68890934d192c1779f8dc6` |
| registry source digest             | `sha256:041eeb4cdac568c48ff5e8f9f41a986259fcb89e16ee6b339802e25eca139d5e` |
| state-machine digest               | `sha256:5c4fa5c0cfac253b45a41c2e49ee7e863b9efbe124e5d743e041f5e01f5c6f15` |
| policy digest                      | `sha256:95023973eb007a958a3c5aea3ac61b6caa7cd8955b9a24fcef3ad269230c64e8` |
| generated registry body digest     | `sha256:11c0f599e8320668a6f601241206c795933b26e3b9c456a58353a0d13c7ecd30` |
| generated registry file SHA-256    | `15140fb8d9233bf161c283f32ace7cab639a9ed1df9ffb35bae193385ef62a63`        |
| generated registry size            | `16276` bytes                                                             |
| generation lock SHA-256            | `d3762502b5535ac5ccfb7e75b6e0131c04be3bff1b3495d8da9a7f364a9e3c17`        |
| ADR-0013 SHA-256                   | `1c3dbb67d73a1903ef103696494096bc49de14437e9e4315f93d97e6e5b4be94`        |
| registry generator library SHA-256 | `b2b174e521a002de3afcc138596c1f83f68abdaace4a570832399bb80dd8b91f`        |
| registry generator tests SHA-256   | `203752fc176b55097eca1b2ca824af0771d354b7dd4cb1a8c994a64f92f7a39d`        |

The generator ran under the repository-pinned Node `24.13.1`, Bun `1.3.14`, and Go `1.26.6` tuple. The temporary
official Node `darwin-arm64` xz archive matched its downloaded `SHASUMS256.txt` entry
`d82a321541d65109c696505135be3b7dd46e3358f0f04d664f50f0d1e1ccb8a6`; no global toolchain was changed.

## Local verification

Passed:

- `platform:contracts:check` under the pinned tuple: 91 JSON files, 35 schemas, two OpenAPI documents, three Proto
  sources, two fixture manifests, 47 fixture cases, nine operation IDs, and
  `AJV_2020_AND_IN_REPO_SEMANTICS_PASS`;
- generated registry `--check` and generation lock `--check` byte-for-byte;
- registry/contracts/lock focused tests: 22 tests;
- all script tests: 129 tests;
- all package tests run before the final script-only integration correction: 221 tests;
- `oxlint --deny-warnings`, package typecheck, package build, dirty-path `oxfmt --check`, and `git diff --check`;
- worktree secret-pattern scan over this slice: no finding.

The first all-script run found one integration omission: the independent manifest test path did not invoke the new
registry semantic validator, so the raw-operation negative fixture appeared valid. The path now invokes the same
validator, and the final 129-test script run passed.

## Explicit limitations

Full-repository `oxfmt . --check` remains red only for three pre-existing HEAD files outside this slice:

- `docs/plan/p1/dependency-reviews/pgx-v5.10.0-x-text-v0.39.0-implemented-closure.md`;
- `docs/plan/p1/dependency-reviews/x-sys-v0.44.0.md`;
- `services/control-plane/internal/migration/testdata/postgres_projection/catalog-representative.json`.

They were not edited. Every dirty path owned by this slice passed the formatter. The full-history secret scan was
terminated after a bounded silent run and is not claimed; the exact dirty/worktree pattern scan passed. Existing
missing contract suites remain listed by the checker, and no immutable or aggregate Gate changes state.

The next authorized step is slice 2 only after this registry slice is fixed and reviewed: append-only PostgreSQL
kernel migration `000007` consuming exact generated profile identities. Service/claim/matrix and independent review
remain slice 3; HTTP and P2 external side effects remain forbidden.
