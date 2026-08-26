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

## Append-only Slice E writer repair

The first bounded Slice E preflight on the fixed replay candidate failed before
the exclusive lock transition. The v3 generated profile intentionally exposes
only its typed registry/profile/evidence-manifest fields; it does not duplicate
the replay run's `candidateManifestSha256` or `outputFiles`. The lock writer's
old recursive profile lookup therefore rejected the valid replay candidate with
`v3 profile does not expose the exact 49-output assembled authority.` No lock
bytes were written and the fixed v2 predecessor remained unchanged.

The repair keeps the v3 schema and generated profile immutable in shape. A
versioned profile helper now fences the source/predecessor and consumes the
canonical replay-v3 semantic graph, whose validation returns both output
identity fields. The writer binds those receipt-verified values, requires the
exact 49 core outputs, and rechecks the captured replay snapshot immediately
before the exclusive write. The v1/v2 authorities, all entry/recovery writer
paths, production database/HTTP/P2/provider effects, deployment, publication,
and Gate state remain unchanged and unauthorized.

A subsequent preflight exposed a separate newline-handling defect in the same
consumer: its Git text helper trimmed the terminal newline before comparing a
tracked generated artifact, so a valid newline-terminated manifest was falsely
classified as dirty. The bounded repair compares the observed bytes' Git blob
SHA directly with the tracked `100644` blob identity. The lock transition was
again not reached and the fixed v2 predecessor remained byte-identical.

## Append-only phase-writer and review-authority repair

The superseding D-053 repair also closes three deterministic pre-G/J authority
gaps without touching the fixed Slice F object. The fixed supply review has a
historical `## Verdict` section whose first line is exactly `` `APPROVE` `` and
whose following severity table records zero P0/P1/P2 findings. The phase
source now selects an explicit, versioned legacy-table parser for that one
immutable slot; it requires one Verdict heading, one complete severity table,
one row for each severity, and zero values. New R5 and terminal reviews retain
the canonical `APPROVE - P0=0 / P1=0 / P2=0` parser. No predecessor review byte
is rewritten or re-added.

The detached binding writer now validates every candidate/review Git lineage,
review-only diff, live blob, mode, verdict, and reviewer separation before any
tuple or registry publication. It preflights both destinations and removes
only exact bytes created by the current invocation if a later pair step fails;
an existing divergent sibling is never overwritten. The phase-bound lock
writer accepts the tuple and registry as explicitly late-bound regular files
that are absent from the fixed R5-review `HEAD`, computes their future Git blob
identity from stable live bytes, and leaves the exact three-path
Slice-I checker as the final topology authority.

The read-only terminal checker now accepts a contained JSON identity input for
the binding actor and complete terminal-review binding, passes both to the
state machine, and can therefore emit
`REVIEW_BOUND_CURRENT_SOURCE_CANDIDATE` only after the terminal review child is
fixed. It still has no write mode and emits no tracked output.

These changes remain evidence-only and non-Gate. They do not authorize
production database writes, HTTP/P2/provider effects, deployment, publication,
release, or any Gate transition. Because the phase source, schema, writers, and
checker are pre-replay authority, this repair invalidates the prior v3
projection/native receipts; a new repair candidate and fresh C--F evidence are
required before G--J can continue.
