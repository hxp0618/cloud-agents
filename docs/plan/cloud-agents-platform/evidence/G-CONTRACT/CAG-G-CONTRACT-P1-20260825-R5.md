# Gate candidate record: `G-CONTRACT` / P1 / R5

- Evidence ID: `CAG-G-CONTRACT-P1-20260825-R5`
- Record type: `PHASE`
- Phase / Gate: P1 contract foundation / `G-CONTRACT`
- Supersedes: `CAG-G-CONTRACT-P1-20260823-R4`
- Status: `IN PROGRESS`
- Independent reviewer: `PENDING`
- Date: 2026-08-25 Asia/Shanghai
- Gate effect: none; this is a current-source non-Gate candidate
- Closure decision: `NONE`

## Fixed semantic authority

- Authority: `cloud-agents/platform/gate-phase-record/g-contract-p1/v1`
- Source digest: `sha256:3715914aebba7b74437e9694dac8427bf94ebcfea5b50505d45641dffb9df34c`
- Model digest: `sha256:5a68be472ed0f566c9f1d95a15da4208f256a414b2c2007314b8db76e241a04e`
- Criteria authority: `docs/plan/cloud-agents-platform/05-gates-and-acceptance.md` / `sha256:4a3d0b3c184e9673944411adbc5c8ea933883c855d5aada67862dad8e4dcc994`
- Current candidate authority: `tools/contract-review-binding/v1/registry.json` / `sha256:e5e5a6abc573fcdcce9d0f1338dad033f5155fe76b555e1fca3a9efc19f14dde`
- Effective candidate status / formal missing: `REVIEW_BOUND_SATISFIED_CANDIDATE` / `0`

## Prerequisites and immutable history

| Kind | Evidence ID | Path | SHA-256 |
| --- | --- | --- | --- |
| prerequisite | `CAG-G-INVENTORY-P0-20260810-R3` | `docs/plan/cloud-agents-platform/evidence/G-INVENTORY/CAG-G-INVENTORY-P0-20260810-R3.md` | `sha256:d865bb9fd1195342a7251bd6822130437753f5be4af8312d9d5c5ee4d5b71400` |
| prerequisite | `CAG-G-BASELINE-P0-20260823-R4` | `docs/plan/cloud-agents-platform/evidence/G-BASELINE/CAG-G-BASELINE-P0-20260823-R4.md` | `sha256:57429377291d1b6a41ff886cde2a6692afd63b5c15adf0677767d59e87b03dd9` |
| historical | `CAG-G-CONTRACT-P1-20260823-R1` | `docs/plan/cloud-agents-platform/evidence/G-CONTRACT/CAG-G-CONTRACT-P1-20260823-R1.md` | `sha256:ebac580f39a7c3c397c1000a3b04abb7281732e45ade3f8aa75474c231334189` |
| historical | `CAG-G-CONTRACT-P1-20260823-R2` | `docs/plan/cloud-agents-platform/evidence/G-CONTRACT/CAG-G-CONTRACT-P1-20260823-R2.md` | `sha256:ca9754117bdaded4a4d8ea107933725b517934ffc6c5da52607f80d0eeed9909` |
| historical | `CAG-G-CONTRACT-P1-20260823-R3` | `docs/plan/cloud-agents-platform/evidence/G-CONTRACT/CAG-G-CONTRACT-P1-20260823-R3.md` | `sha256:67268e58174f9c845d2599bac20e99aa2010d52d557c78b3982c13deae5aae85` |
| historical | `CAG-G-CONTRACT-P1-20260823-R4` | `docs/plan/cloud-agents-platform/evidence/G-CONTRACT/CAG-G-CONTRACT-P1-20260823-R4.md` | `sha256:0982261244e7315c2798db4a4f0913f7f93037c251140c8f14ed2cbc3bcd7152` |
| historical | `CAG-G-CONTRACT-P1-20260823-R4-INDEPENDENT-REVIEW` | `docs/plan/cloud-agents-platform/evidence/G-CONTRACT/CAG-G-CONTRACT-P1-20260823-R4-independent-review.md` | `sha256:f0d5b12f1f6e0f2936783868331d4d74d7a3ee0fc49b3e370894b95884458f61` |

## Current-source projection and supply review

- Projection commit / tree: `a7a46853d94e8d01cf2022dd447797cedc241d19` / `d91447364745af16314f348d550b358f995fad0b`
- Projection archive SHA-256: `sha256:395f64058dbdae27ba0897861c39264ca3da36deca342ec1f98f5173c67b777b`
- Supply candidate commit / tree / parent: `94cbb23127a6a6c1ca31398d731d99b54cac80f9` / `052d5d1a994349c42766bf9dafc292088add1a90` / `25b3a47a185db2151ca4ba6e1916811cba6e155e`
- Supply candidate diff: `sha256:3983b0fdeafc20746136168087e1e05f7e803a3ca497178f00f30455dc68c2dc`
- Supply review commit / tree / path: `58e8f98a8b57760721306b3636a06dc3f10283b2` / `fee863c98148f4494c0985cbf7e7784c5cf23196` / `docs/plan/p1/g-contract-generator-supply-profile-v3-independent-review-20260825.md`
- Supply review SHA-256 / verdict: `sha256:c7f2bf258c0dc2f06bb43940ef323aa77f01e351b9fedf92c344d3000b423cbb` / `APPROVE_P0_0_P1_0_P2_0`
- Assembled lock commit / tree / blob: `94cbb23127a6a6c1ca31398d731d99b54cac80f9` / `052d5d1a994349c42766bf9dafc292088add1a90` / `86ab61bc060d8a0ad7878fb43b29a54997e40c2b`
- Assembled lock SHA-256 / state: `sha256:61267f5123004c108c9bcd79a8004da35af45d7ab7beb83d7b898da57a4d81ba` / `ASSEMBLED`

## Current contract, SDK, and descriptor inputs

| Path | Git blob | SHA-256 | Size | Mode |
| --- | --- | --- | ---: | --- |
| `package.json` | `c87cdb92ed62b3ab7651f0382cac7ee92179a36b` | `sha256:dab3565713b3caa303f6748e3fcdad744bba2f64fc2e084ab696db24de50f323` | 6505 | `100644` |
| `bun.lock` | `9dae2a44483f0423f9ea789422bbf695a34aee70` | `sha256:cbd9d3dc8976c2dcbc340fb69f4dad250e788687bb252428dcd611b256fc884a` | 100469 | `100644` |
| `contracts/generated/proto/cloud-agents-v1alpha1.binpb` | `08f526f6b09a8306518c59f87d2400bc53ce8c60` | `sha256:cd896a7aaccf70426be94314c5cfa09733a6908c33867c1f8942b4eb6b179218` | 12730 | `100644` |
| `contracts/generated/proto/cloud-agents-v1alpha1-breaking-baseline.binpb` | `08f526f6b09a8306518c59f87d2400bc53ce8c60` | `sha256:cd896a7aaccf70426be94314c5cfa09733a6908c33867c1f8942b4eb6b179218` | 12730 | `100644` |
| `contracts/generated/proto/manifest.json` | `4ab7e61420b31b58352bfa58b87cc175d0f07e48` | `sha256:906b7c6672913beb497bd21917dcfe5e5273732a97026351797f311537cbbcb1` | 5222 | `100644` |
| `sdk/go/go.mod` | `c21cfd15d5748f3704ae87d26e1f937a7b0be7cb` | `sha256:8b9e28f4db2db796bd69a6d1df5c93ff9f145c621209779b76d2df0a52794063` | 397 | `100644` |
| `sdk/typescript/package.json` | `b59cc762fb32dda975b528481a3cb40b11491872` | `sha256:803badc33a76e6bedfc82d357539f318aff18599e2a8eabf6385d00f489297b9` | 1821 | `100644` |

## G-CONTRACT exit criteria

| Criterion | Candidate status | Formal criteria | Requirement |
| --- | --- | --- | --- |
| `json-schema-authority-and-openapi-refs` | `OPEN_NOT_CLAIMED` | `json-schema-2020-12-official-test-suite`<br>`openapi-3.1-semantic-validation` | JSON Schema is the sole management, agent, and host JSON-model authority; OpenAPI contains route, status, header, and schema references without drifting inline copies. |
| `proto-authority-and-generated-connect-grpc-mapping` | `OPEN_NOT_CLAIMED` | `generated-sdk-replay`<br>`response-watch-unknown-field-preservation` | Versioned Proto is the sole Worker and Platform Adapter wire-model authority and generates Connect or gRPC client, server, and mapping artifacts whose digests enter the record. |
| `shared-golden-negative-and-n-minus-one-fixtures` | `OPEN_NOT_CLAIMED` | `n-minus-one-compatibility`<br>`response-watch-unknown-field-preservation` | TypeScript SDK, Go SDK, Go server validator, and mappings share golden, negative, and N or N-minus-one fixtures covering every required compatibility and round-trip case. |
| `exact-pinned-external-consumer` | `OPEN_NOT_CLAIMED` | `generated-sdk-replay` | A fresh external consumer installs exact digest-pinned SDK artifacts, compiles, and calls the fixture server without workspace, file, Git, or cross-repository dependencies. |
| `digest-change-invalidation` | `REVIEW_PENDING` | `json-schema-2020-12-official-test-suite`<br>`openapi-3.1-semantic-validation`<br>`generated-sdk-replay`<br>`n-minus-one-compatibility`<br>`response-watch-unknown-field-preservation`<br>`runtime-server-path-and-tenant-authority-enforcement`<br>`remaining-generator-supply-chain-review` | Any schema, Proto, OpenAPI, generated SDK, or mapping-fixture digest change invalidates this record. |

The derived missing set remains:

1. `json-schema-authority-and-openapi-refs`
2. `proto-authority-and-generated-connect-grpc-mapping`
3. `shared-golden-negative-and-n-minus-one-fixtures`
4. `exact-pinned-external-consumer`
5. `digest-change-invalidation`

## Invalidation

- Any prerequisite, historical record, Gate-criteria authority, reviewed current-candidate authority, projection, supply-v3 assembly or review, current source input, or assembled-lock identity drift invalidates R5.
- Any schema, Proto, OpenAPI, generated SDK, or mapping-fixture digest drift invalidates R5.
- The sole authorized ASSEMBLED-to-PHASE_BOUND lock successor does not invalidate the immutable assembled snapshot; every other lock transition does.
- R5 and every review remain append-only historical evidence after invalidation and must be superseded rather than overwritten.

## Explicit non-claims

- G-CONTRACT closure
- G-SUPPLY-CHAIN closure
- production database or migration execution
- HTTP, OIDC, JWKS, P2, provider, workload, credential, or trust effects
- deployment, publication, external signing, release, Beta, or GA
- Linux arm64 replay

`notGateClosure=true`; `gateStatus=ALL_GATES_OPEN`; `G-CONTRACT` remains `IN PROGRESS`.
