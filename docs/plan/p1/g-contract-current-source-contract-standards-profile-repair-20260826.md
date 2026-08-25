# G-CONTRACT current-source contract-standards successor repair — 2026-08-26

## Boundary and deterministic blocker

This append-only repair continues ADR-0030/D-053 on the fixed P0 tree. It is
limited to the current-source contract-standards authority used by the v3
native replay. It does not rewrite the accepted ADR, the v1/v2 profiles, the
historical generation lock, or any prior review record.

The first bounded Darwin arm64 Slice D run used the fixed projection archive
and pinned offline generator supply. It failed closed in runner A before any
replay receipt was produced:

```text
ContractStandardsProfileError: Current contract cardinality mismatch:
expected={"schemaFiles":60,"fixtureManifests":2,"fixtureCases":79}
actual={"schemaFiles":68,"fixtureManifests":2,"fixtureCases":79}
path: "/currentContracts"
code: "CONTRACT_STANDARDS_BINDING_MISMATCH"
```

The failed A frame is intentionally empty (SHA-256
`e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855`). The
stderr capture and the temporary task root remain outside Git as diagnostic
evidence; they are not treated as replay receipts.

## Root cause and authority decision

The v2 standards profile was fixed before the approved durable Project
successor. Four source schemas added by `a76b475` changed the schema count
from 60 to 64, and four generated lineage schemas added by `defb66c` changed
it from 64 to 68. The R2 lineage repair review at `a3d2b1e` approved those
successor schemas; deleting them or hiding them in the checker would be an
authority violation.

The successor therefore adds a new generated non-Gate profile v3 with an
exact v2 predecessor fence. It records both discovery domains explicitly:

| Domain                                                   | Schemas | Fixture manifests | Cases | Manifest SHA-256                                                          |
| -------------------------------------------------------- | ------: | ----------------: | ----: | ------------------------------------------------------------------------- |
| independent standards (all `contracts/**/*.schema.json`) |      68 |                 2 |    79 | `sha256:f2b1b9e64249fc9f72cceb857073e49957b78c6f3ab0b7f8d2d01b042a821e37` |
| bootstrap contract discovery (generated tree excluded)   |      64 |                 2 |    79 | `sha256:f2b1b9e64249fc9f72cceb857073e49957b78c6f3ab0b7f8d2d01b042a821e37` |

The v1 profile and v2 profile bytes remain immutable. The v3 profile is bound
to the v2 path, SHA-256, size, and `mutation=forbidden`; its profile/checker
bytes are included in the v3 pre-replay authority and the successor lock
authority. The two domains are not interchangeable: bootstrap validation
continues to report 64 schemas, while the independent validator must compile
all 68.

## Verification and non-claims

After the repair is fixed, only the affected TypeScript/Python profile tests,
bootstrap/currentness checks, v3 predecessor/schema/topology checks, and the
single fresh native replay required by D-053 may be run. The failed replay is
not reused as a success claim. Historical v1/v2 evidence remains recoverable
and unchanged.

This record does not authorize production database writes, HTTP/OIDC/JWKS,
P2/provider effects, deployment, publication, release, or any Gate transition.
All Gates remain `IN PROGRESS`/`OPEN` and the v3 profile remains
`notGateClosure=true`.
