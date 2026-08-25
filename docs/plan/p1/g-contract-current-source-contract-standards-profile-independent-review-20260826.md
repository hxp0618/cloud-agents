# G-CONTRACT current-source contract-standards profile v3 — independent review

Date: 2026-08-26 Asia/Shanghai

## Verdict

`APPROVE - P0=0 / P1=0 / P2=0`

This is an independent read-only review of the fixed candidate below. The
candidate repairs the deterministic Slice D blocker without changing the
immutable v1/v2 standards authorities. It is admissible for the next bounded
projection/replay step under ADR-0030/D-053.

This approval is not a `G-CONTRACT` or aggregate Gate closure. It authorizes
no production database write, HTTP/OIDC/JWKS, P2/provider effect, deployment,
publication, release, or Gate transition.

## Fixed candidate identity

- candidate commit:
  `f1e6a085db2a4c7d5405b30a4e853846eb01e4ba`;
- candidate tree: `f7fa5e8b642ee72f527c333b2399b60876fcd7a3`;
- parent: `94143803b8bcef0c80a8c5e0f4fe199328375662`;
- integration baseline parent: `9b85d4284df5f4399ebd7c063c27c596b2097bba`;
- canonical binary diff SHA-256 from the integration baseline:
  `d338500f09d5c2e2de37d9b8c93138d73219e3bf628f8ce400e439dc500ac440`;
- this review path is absent from the candidate tree.

The parent `9414380` contains the versioned profile/checker/lock/DAG repair.
The fixed candidate adds only the two read-only phase-record/state test
fixtures needed to bind the new `contractStandards` lock authority, so the
existing phase tests exercise the same contract as production code.

## Reviewed authority and boundary

- `profile.json` (v1) remains SHA-256
  `dfb79ae54631d9f61f53846c91ac74bebb5b213fac023af2527c3ce352873a11`.
- `profile-v2.json` remains SHA-256
  `9457d4bdc12f16b366d9c56a25a107103f5b2b64650de20f509f3ef96d0d4d01`,
  3539 bytes, Git blob
  `0c73cdf771ddcf0d46c43d52abf5b622507e8e1b`.
- The generated v3 profile binds that v2 predecessor and records two
  non-interchangeable domains: independent all-schema `68/2/79` and
  bootstrap generated-excluded `64/2/79`, both with source digest
  `sha256:f2b1b9e64249fc9f72cceb857073e49957b78c6f3ab0b7f8d2d01b042a821e37`.
- Generation-lock v3 now requires the v3 profile plus the exact v2
  predecessor artifact; its boundary remains `ALL_GATES_OPEN` and
  `notGateClosure=true`.
- The v3 pre-replay DAG includes the standards profile/checkers and repair
  record. A pre-existing Slice C projection singleton is treated as stale
  pre-replay topology; native replay still requires the complete eight-receipt
  set.

## Independently reproduced evidence

- Affected TypeScript tests: `48/48` passed across standards profile,
  lock-v3, generator-supply-v3, predecessor/DAG, and phase-record/state
  suites.
- Python contract-standards tests: `14/14` passed in the pinned offline uv
  environment.
- Independent checker returned `INDEPENDENT_CONTRACT_STANDARDS_VALIDATED`:
  all-schema `68/2/79`, official-suite assertions `1299`, OpenAPI operations
  `9`, `notGateClosure=true`, `gateStatus=ALL_GATES_OPEN`.
- Bootstrap checker returned `64/2/79` with the fixed `f2b1…` digest and only
  the pre-existing `remaining-generator-supply-chain-review` missing item.
- `generate-platform-generator-supply-profile-v3.ts --check-source` returned
  `DECLARED_PRE_REPLAY`.
- `generate-platform-contract-lock-v3.ts --check` returned
  `PRE_REPLAY_LEGACY_LOCK_ONLY`.
- Focused formatter/linter checks and `git diff --check` passed. No changed
  path contained a credential or external-effect capability.

The full standards orchestration command was not used as a pass claim because
the local executable reports Bun `1.4.0` while the immutable profile requires
Bun `1.3.14`; the pinned Python/uv checker and all affected checks were run
offline instead.

## Non-blocking observation

The bootstrap `fixtureCases` implementation currently sums all fixture
manifests rather than explicitly filtering generated paths. The current tree
has no generated fixture manifest, and the independent/bootstrap counts pass;
this is recorded as `P2=0`, not a candidate defect.

## Progression decision

`CURRENT_SOURCE_CONTRACT_STANDARDS_V3_APPROVED_FOR_BOUNDED_REPLAY`

The next permitted step is to reconstruct a fresh projection from this fixed
candidate and then perform the separately required native replay/review chain.
All existing Gate records and historical evidence remain open/recoverable.
