# Independent review: `G-CONTRACT` / P1 / R4 candidate

- Review status：`APPROVE`
- Finding count：`P0=0 / P1=0 / P2=0`
- Review mode：independent read-only fixed-candidate review
- Review date：2026-08-23 Asia/Shanghai
- Gate effect：none；`G-CONTRACT` remains `IN PROGRESS`

## Fixed candidate and lineage

- Candidate commit/tree：`9d08ae0a648a263650115b3419f273a684294766` /
  `c8516c086da62a20ae9faa6f6eb4d490733200da`
- Candidate branch：`codex/cloud-agents-p1-g-contract-r4-rebind-20260823`
- Candidate branch and origin at review：clean、upstream `0/0`、remote exact
- R4 record SHA-256：`0982261244e7315c2798db4a4f0913f7f93037c251140c8f14ed2cbc3bcd7152`
- Fixed implementation source commit/tree：`1a9f15ee75a955beb993ab4bc534f4dab72d46a7` /
  `17bb5c849298cacedda875bd86bdc7f0d4c8186c`
- Fixed source branch and origin at review：clean、upstream `0/0`、remote exact
- `services/control-plane` / `contracts` / `sdk` / `scripts` subtrees：
  - `689942aecbc7f84f692dd71c17d66a607d12b950`
  - `40f0f3b44f83c986f9b015d059451e195e285c0a`
  - `e4c5abf9d9cb591df39d9377529c201a1307997e`
  - `d65f14ec5cc8b2bda27af056673e891cda8cebd1`
- Inventory R3 / Baseline R4 record SHA-256：
  - `d865bb9fd1195342a7251bd6822130437753f5be4af8312d9d5c5ee4d5b71400`
  - `57429377291d1b6a41ff886cde2a6692afd63b5c15adf0677767d59e87b03dd9`
- Baseline R4 independent review SHA-256：
  `44db2df153bbfcc5fa0bd4c928bbdf9b207c60c4458ec61b2e2557c7d97d4c94`

The candidate changes four documentation/evidence files only. R1-R3 remain immutable history; R4 correctly supersedes
R3 because R3 names invalidated Baseline R3 and fixes an older generation lock. No contract, generated SDK, runtime or
migration byte changed in the candidate.

## Reproduced identities and contract replay

The reviewer independently reproduced the fixed tree/subtrees and the candidate's contract inputs:

- generation lock SHA-256：`4f2953540e9305f034a8f6fc7d13af0947d7f5b91f43b7ce6256bc137d071c76`；
- source-contract / toolchain manifests：
  `sha256:eb51453861feb6685eadcd335c0620fea5ca98de9058a9a3d3f5198f6e67406e` /
  `sha256:99c6358f0e8e3546bfb5f178f1e2a780aab0257fadeca17643b294a62106ca2a`；
- generation lock shape：`27` tools、`27` pipelines、`BOOTSTRAP_VALIDATED`、`notGateClosure=true`；
- Proto descriptor and exact breaking baseline：same bytes，SHA-256
  `cd896a7aaccf70426be94314c5cfa09733a6908c33867c1f8942b4eb6b179218`；
- standards profile / dependency review：
  `0bd4348680cf48819658651539d4777c412e7a3fb93c380ffacf536c675f440d` /
  `62c66d64986e8fea38fb712cb091ae5e41aa1792823370616629acc36c39703d`。

With Node `24.13.1`, Bun `1.3.14`, Go `1.26.6 darwin/arm64`, Python `3.14.7`, uv `0.12.5` and the fixed local
standards wheelhouse, `bun run platform:contracts:check` passed. It reproduced:

- production/bootstrap validation：`118` JSON files、`52` schemas、`2` OpenAPI documents、`3` Proto files、`71`
  fixture cases、`9` operations；
- independent validators：official JSON Schema suite `46/383/1299/79`、current fixtures `52/2/71`、OpenAPI `2/9`、
  ten Python tests and zero failures；
- all generated registry/profile, identity SDK, JSON SDK, Proto SDK and generation-lock checks：`current`；
- explicit boundaries：`ALL_GATES_OPEN` and `notGateClosure=true`。

The source tree remained free of `__pycache__` and `.pyc` after the replay.

## Reproduced bounded SDK and migration evidence

The reviewer did not repeat the candidate's known non-PASS aggregate invocation. Instead, each criterion was replayed
with a bounded command:

- contract-lock tests：`18/18`、`105` expectations；
- identity SDK generator with `--timeout 60000`：`4/4`、`34` expectations；
- JSON SDK generator：`5/5`、`30` expectations；
- Proto SDK generator：`2/2`、`15` expectations；
- TypeScript SDK：`20/20`、`108` expectations plus `tsc --noEmit`；
- Go SDK：fresh `GOWORK=off GOTOOLCHAIN=local GOFLAGS=-mod=readonly go test -count=1 ./...` and `go vet ./...`；
- isolated packed consumers：fresh TypeScript and Go consumers passed。

The migration-bound replay did not invoke PostgreSQL or the broad migration package. The focused bundle test passed
`17/17` with `102` expectations; checker and `--check` generation reproduced migration `000012` and exact digests:

- schema：`sha256:54bd987183d6e2d8a7e3ba58a5fa5ee0666015a101193f363f671be294bb2907`；
- bootstrap：`sha256:db95649924f259cfa320e897bd5e0934c35fcc9009d8492a69ec5dc71132081c`；
- manifest：`sha256:454345322827369258f8496cce2c1e7f4d4b3e5b8b5f841f20c9fc84f53b3ddc`。

The result remained `UNPUBLISHED_BOOTSTRAP_MUTABLE`; runtime catalog introspection and signing/publication remained
`NOT_IMPLEMENTED`.

The fixed current-source range `e14780dff612f0d5ddf1513b1c0ae3bac6e9149a..1a9f15ee75a955beb993ab4bc534f4dab72d46a7`
also passed a fresh redacted Gitleaks scan：13 commits、approximately `568.86 KB`、zero findings.

## Failure accounting and open boundary

The review accepts the candidate's failure accounting as accurate:

- the initial concurrent four-file SDK aggregate had three identity-SDK timeouts and is recorded as nonzero / not
  PASS；the later bounded identity replay is the evidence for that criterion；
- the stopped `platform:go:check` process exited `130` and is explicitly `NOT PASS`；it contributes no evidence；
- production Ajv official-suite compliance remains `NOT_RUN_NOT_CLAIMED`；
- no full migration suite, live PostgreSQL, runtime tenant-path enforcement, N/N-1 deployed compatibility or current
  supply-chain closure is claimed。

The generation lock retains these exact seven formal `missing` entries:

1. `json-schema-2020-12-official-test-suite`
2. `openapi-3.1-semantic-validation`
3. `generated-sdk-replay`
4. `n-minus-one-compatibility`
5. `response-watch-unknown-field-preservation`
6. `runtime-server-path-and-tenant-authority-enforcement`
7. `remaining-generator-supply-chain-review`

Fresh candidate evidence does not rewrite those formal entries. The remaining generator supply-chain review,
immutable current-source Gate signature and runtime/deployment criteria are still open.

## Verdict

`APPROVE — P0=0 / P1=0 / P2=0` for fixed candidate
`9d08ae0a648a263650115b3419f273a684294766`.

This verdict approves the accuracy and lineage of the R4 current-source candidate only. It does not close
`G-CONTRACT`, change any tracker status, authorize production database writes, HTTP/P2/provider effects, deployment,
publication or release, or close any aggregate/phase Gate.
