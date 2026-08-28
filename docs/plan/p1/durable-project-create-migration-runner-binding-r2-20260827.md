# D-053-MIG-000014.r2：localdev runner-binding authority

状态：`REVIEW_APPROVED_NON_GATE`（localdev、只读；不构成 Gate 关闭或生产发布）
日期：2026-08-27
批准范围：在同一个 r2 candidate 内完成 generated runner-binding、fail-closed checks、focused verification 和一次独立只读审查。P0/P1 可在该 candidate 内修复一次并重新审查；P2 记录后延期。不得创建 r3/r4。

## 1. 身份与不可变引用

本 authority 的身份是 `D-053-MIG-000014.r2`，profile id 为
`cloud-agents-platform-migration-runner-binding/v1`。生成源和 profile 只允许由
`scripts/generate-platform-migration-runner-binding.ts` 产生（`--check` 为默认、`--write`
仅用于当前候选冻结）。r1 的四个对象必须按下表逐字节绑定；生成器在构建前会校验
路径、模式、长度和 SHA-256，任何漂移均 fail-closed：

| r1 对象 | 固定路径 | mode/size | raw SHA-256 |
| --- | --- | --- | --- |
| source | `services/control-plane/migrations/successor/000014/authority-source.json` | `100644` / 21698 | `sha256:6436c991dc838c353f27f91f9aff3257d02e18a6c3e0535244fe7f7d1d7a5d8e` |
| source schema | `services/control-plane/migrations/successor/000014/authority-source.schema.json` | `100644` / 9052 | `sha256:b7ccd78f1c8cc3969b0d2c8846157dc645595e397ab97942522ecfbe163c873c` |
| profile | `services/control-plane/migrations/successor/000014/profile.json` | `100644` / 22970 | `sha256:668c7e9c0337d1e50c81dde0ac465561d4ac4eb5f6d14f7fd8b2e26ef672250a` |
| profile schema | `services/control-plane/migrations/successor/000014/profile.schema.json` | `100644` / 10010 | `sha256:314cc945796fac537bc89a3c09d2e1d6681fa06e028ea0ddecb3a776eb31e827` |

The r1 profile logical digest is
`sha256:0637e32e1e07d82ff2917a13f8ade6276c2518ff0aeb7a80451f9da0f69b2630`.
These files are evidence bytes, not inputs to be reformatted, regenerated, or rewritten by r2.

For provenance, the generated source and profile also carry the exact Git identity of the r1
candidate: commit `1325dc1773ef9bad2d809fedee9b392e3cdbf959`, root tree
`49e53f2462af20201231c2428eb56cce543403a2`, and the
`services/control-plane/migrations/successor/000014` subtree
`9d704eec0c8ca04fc0f1bd41b4a348db0b853096`. The eight referenced blobs are fixed as follows:

| blob path | Git blob |
| --- | --- |
| `services/control-plane/migrations/successor/000014/authority-source.json` | `040c765971adde44d2171382d726ff294de05954` |
| `services/control-plane/migrations/successor/000014/authority-source.schema.json` | `34705605fb42f049135da3a31a911387820f872f` |
| `services/control-plane/migrations/successor/000014/profile.json` | `046e2c51581964a59e770308da4a9fe23635f3ee` |
| `services/control-plane/migrations/successor/000014/profile.schema.json` | `124ea8dda97838f14cc9512fa06022b55ca74f87` |
| `services/control-plane/migrations/manifest.json` | `6f5f0c35b488fa221e814c0b768c3317ce9b9c68` |
| `services/control-plane/migrations/schema-bundle.json` | `b064e10ab06011f80523be6f44a7eff96db91549` |
| `services/control-plane/migrations/successor/000014/manifest.json` | `2a4948e082e606a10564fb7c77a469975e6c888d` |
| `services/control-plane/migrations/successor/000014/schema-bundle.json` | `18f4fa00d7b17a9d72df61b60e1124e18bf73b87` |
| r1 profile logical digest | `sha256:0637e32e1e07d82ff2917a13f8ade6276c2518ff0aeb7a80451f9da0f69b2630` |

The r2 generated files are:

* `services/control-plane/migrations/successor/000014/runner-binding/authority-source.json`
  (generated size 73687, raw SHA `sha256:f781843d9ec2b086225a95578e66cac3ff4f949bc1ff797e8b36d0bd4a908b9f`);
* `services/control-plane/migrations/successor/000014/runner-binding/authority-source.schema.json`
  (generated size 20305, raw SHA `sha256:d52dedce13d23dffff783ebeecf5bd6dc48bd07162a6915cd937c4ef1c288778`);
* `services/control-plane/migrations/successor/000014/runner-binding/profile.json`
  (generated size 74021, raw SHA `sha256:b3bcf86b3bb9a147dce4ccbc1db58c5a6d26c216f72e91eec22c5ac7dbba589c`,
  logical profile digest `sha256:7ffe830d854e5037994e2b5019da792a42d97928da456639bcdbfc4c3fa05489`);
* `services/control-plane/migrations/successor/000014/runner-binding/profile.schema.json`
  (generated size 20385, raw SHA `sha256:1f3fc9cc77d68515bcce12ab4174a3aafd21df7bf89e59aeeec8fbb155337c20`);
* `services/control-plane/internal/localmigration/runner_binding_profile_generated.go`
  (generated size 5883, raw SHA `sha256:af5604caf4c8fb78b68a5e2566861ffeaf011c8ce235a41c897f9a8379c3f963`).

The generated Go constants must agree with the raw descriptors above. The independent review
records the final candidate commit/tree and re-checks these values before issuing a verdict.

## 2. Closed selector set

Before opening a database connection, localdev binds exactly one generated selector. The only
accepted selectors are `canonical-000013` and `successor-000014`; an empty selector means the
canonical selector only when the manifest path is also omitted. Caller-selected heads, arbitrary
paths, foreign worktrees, symlinks, mode/byte/size/SHA drift, and a self-consistent but identity-
different manifest or schema bundle are rejected.

| selector | manifest (raw size/SHA; logical digest) | schema bundle (raw size/SHA; logical digest) | head/count |
| --- | --- | --- | --- |
| `canonical-000013` | `services/control-plane/migrations/manifest.json` — 33976 / `sha256:95a584a9e517d68e9a904fadfc76f84dcdbd8b532a24d8716468d0ff53d59d6b`; `sha256:56af03a65461e2009cf73c16ac2b1d74d856f68e3efc8b363ab84c537660c4d1` | `services/control-plane/migrations/schema-bundle.json` — 22443 / `sha256:d5ce27597e2218240a276dbbec01431e4fe26774e195b70445078d8662a3826d`; `sha256:c7e08e81b463d04dd267438ac636811200586d5d84d8cb2e8d18799bd2c5faca` | `000013` / 13 |
| `successor-000014` | `services/control-plane/migrations/successor/000014/manifest.json` — 36291 / `sha256:961ccac428f8bf0d55a828fd93ae8e2085ae17d34c09cb2b46c28f653851f8ae`; `sha256:1ece795f54e049a15e4de37f351841be0ba611f8eb0be6eaf1aa68dc0145b620` | `services/control-plane/migrations/successor/000014/schema-bundle.json` — 23970 / `sha256:d90661ac0271b78de563e565bb861b35d570be30fa788131e9813dde56870edc`; `sha256:2f363d4dc412803a3c126dd9b85f4e2fe7109b92b04706d077a786fdaa673677` | `000014` / 14 |

The selected manifest’s embedded `schema_bundle` payload must equal the independently bound
schema-bundle payload. SQL artifacts are then checked against the selected manifest descriptors;
there is no discovery or fallback scan.

## 3. r1 closure: complete inputs, exclusions and lineage

`r1Closure` embeds the exact r1 source/profile/schema references and the complete sets, rather
than recomputing them from the filesystem. The frozen counts are:

* `inputPaths`: **167** entries;
* `protectedPaths`: **29** entries;
* `exclusionPaths`: **14** entries;
* runtime member manifest: **44** records, tree SHA
  `sha256:1c426708510d3c0217bdc4c544e430a70087eb794a53757865597d9b5ed6ebe0`.

The canonical and successor artifact sets contain respectively 40 and 43 descriptors (13 and
14 migrations). The full path arrays remain the r1 bytes under the raw digest above; r2 may only
reference them. In particular, r1’s excluded projection/replay/runner evidence, successor
catalog/schema/archive outputs, `go.work`, and prior review bytes remain excluded and untouched.

For audit readability, the 14 excluded paths are reproduced here (the authoritative copy is the
`r1Closure.exclusionPaths` array, covered by the r1 source SHA above):

```text
contracts/generated/platform/v1alpha1/durable-project-create-lineage-v2.json
contracts/generation.lock.json
docs/plan/p1/durable-project-create-identifier-hardening-independent-review-20260826.md
docs/plan/p1/durable-project-create-migration-bundle-successor-independent-review-20260827.md
go.work
services/control-plane/migrations/archive/c7e08e81b463d04dd267438ac636811200586d5d84d8cb2e8d18799bd2c5faca.schema-bundle.json
services/control-plane/migrations/catalog/schema-000014.json
services/control-plane/migrations/successor/000014/evidence/projection.json
services/control-plane/migrations/successor/000014/evidence/projection.member-manifest.json
services/control-plane/migrations/successor/000014/evidence/replay.json
services/control-plane/migrations/successor/000014/evidence/runner.json
services/control-plane/migrations/successor/000014/manifest.json
services/control-plane/migrations/successor/000014/profile.json
services/control-plane/migrations/successor/000014/schema-bundle.json
```

Archive identity is an exact-byte copy named by the logical schema-bundle digest; rewrite is
forbidden. Runtime/member identity is deterministic USTAR v1 ordered by ASCII-byte path with
`mode=100644`, `uid=0`, `gid=0`, `mtime=0`, no compression, and duplicate rejection.

## 4. Runner, receipts and boundary

The only executable entrypoint is `services/control-plane/internal/localmigration.Run` in
`localdev_only` mode. Binding occurs before connector/DB setup. `complete-ledger` remains a no-op;
entry and recovery writers remain `NOT_IMPLEMENTED`; all external effects are forbidden. The
production `services/control-plane/internal/migration.Runner.Run` is explicitly out of scope.

Frozen runner/toolchain/platform metadata is Go `1.26.6`, Node `24.18.1`, Bun `1.3.14`, on
`darwin-arm64` and `linux-amd64`. Receipt paths remain append-only/absent-pending: projection,
member-manifest, replay-summary and independent-review receipts are `CREATE_ONCE_APPEND_ONLY`;
runner receipt is `NO_WRITE`; receipt state is `AUTHORITY_FROZEN_REVIEW_PENDING`.

The lineage fence is `single-predecessor-append-only`, from r1 predecessor head `000013` to
successor head `000014`, with historical evidence retained and never rewritten. Review is one
fresh independent read-only pass with explicit `APPROVE` or `REQUEST_CHANGES` and P0/P1/P2
classification. A P0/P1 finding may be repaired once in this same r2 candidate and re-reviewed;
P2 is recorded and deferred. Review cannot mutate the candidate or transition a Gate.

## 5. Verification contract

Run only the affected focused checks (from repository root unless noted):

```text
bun scripts/generate-platform-migration-runner-binding.ts --check
bunx vitest run scripts/lib/platform-migration-runner-binding.test.ts --reporter=dot
cd services/control-plane
GOWORK=off GOFLAGS=-mod=readonly /Users/huang/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.6.darwin-arm64/bin/go test -tags localdev ./internal/localmigration -count=1 -timeout=2m
```

The focused negative tests must cover unknown/caller-selected selector, foreign path, selector
mismatch, symlink, reformatted bytes, and descriptor/mode/size/digest drift, all failing before
connector use. No broad migration suite or repeated full-run claim is made by this authority.

## 6. Explicit non-claims

This r2 authority does **not** authorize canonical/production Runner execution, production
database writes, HTTP/P2/provider calls, deployment, publication, archive/member-manifest or
EC-2 replay/projection evidence generation, or closure of any Gate. It is a localdev/read-only
binding supplement only; all r1 bytes and historical SHAs remain recoverable.
