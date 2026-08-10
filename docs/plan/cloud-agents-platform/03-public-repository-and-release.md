# 03. 公共仓结构与发布

## 1. 目标目录

```text
cloud-agents/
├── go.work                                # 仅本地开发；不进入 consumer 依赖
├── packages/                              # 现有七个 TS Runtime 包
├── contracts/
│   ├── runtime/v2/
│   ├── managed-agent/v1alpha1/
│   ├── managed-host/v1alpha1/
│   ├── worker/v1alpha1/
│   └── platform-adapter/v1alpha1/
├── sdk/
│   ├── typescript/
│   └── go/                                # 独立 go.mod：generated contracts/client
├── services/
│   ├── control-plane/                     # 独立 go.mod；含 CP/CLI cmd
│   │   └── adapters/postgres|oidc|otlp/
│   └── worker/                            # 独立 go.mod；含 supervisor/worker cmd
│       └── adapters/local|container|kubernetes|filesystem|s3/
├── deploy/
│   ├── compose/
│   └── helm/
├── conformance/
│   ├── runtime/
│   ├── control-plane/
│   ├── worker/
│   └── platform-adapter/
└── docs/
```

## 2. Module 与 import 规则

- TS Runtime 七包保持独立 semver；
- Go SDK module：`github.com/hxp0618/cloud-agents/sdk/go`，tag `sdk/go/vX.Y.Z`；
- Go Control Plane module：`github.com/hxp0618/cloud-agents/services/control-plane`，tag
  `services/control-plane/vX.Y.Z`；
- Go Worker module：`github.com/hxp0618/cloud-agents/services/worker`，tag `services/worker/vX.Y.Z`；
- `cloud-agent-control-plane` 与 `cloud-agent-cli` command 归 Control Plane module；Supervisor/Worker command
  归 Worker module；
- import DAG 固定为 `contracts -> sdk/go -> control-plane|worker`；Control Plane 与 Worker 不互相 import
  service/internal，只通过 Worker wire/SDK 通信；
- `go.work` 仅用于仓内开发，发布 module 不得依赖 workspace replace；
- 对外 Go ABI 只承诺 generated contracts/client SDK；server domain/service/database model 全部 internal；
- Platform Adapter 是版本化 out-of-process wire，不是宿主 import service package；
- contracts 是唯一 wire source，SDK/server validator/fixtures 必须生成或校验一致；
- contracts、TS SDK、Go SDK 各自固定 semver 与 immutable digest；SDK manifest 绑定 contract digest、
  generator name/version，CP/Worker 精确 pin SDK/contract，发布检查禁止 `replace`、`workspace:`、`file:`；
- v1alpha1 支持 N/N-1 reader 与 unknown-field preservation；breaking field/semantic change 必须新 API major，
  deprecated surface 至少保留一个已发布 minor 与明确 removal date；
- Runtime workflow 不隐式构建 Go 服务，平台 workflow 不重打 Runtime tag；
- Go toolchain 在 P0 固定；服务/Worker 首批只支持 Linux amd64/arm64，CLI 支持 Linux/macOS/Windows
  amd64/arm64；默认 `CGO_ENABLED=0`，任何例外单独登记 SBOM/OS matrix。

## 3. 公共制品

| 制品                   | 内容                                                                                  |
| ---------------------- | ------------------------------------------------------------------------------------- |
| Runtime release        | 七 tarball、standalone、schema、manifest、checksums、SBOM、provenance                 |
| Contract release       | OpenAPI/JSON Schema bundle、golden/negative fixtures、checksums/signature             |
| TS SDK release         | `@synara/cloud-agent-platform-sdk` packed ESM/CJS/types、validators/client            |
| Go SDK release         | generated contracts/client module                                                     |
| Control Plane release  | Go module、Linux amd64/arm64 binary/OCI image、signed migration bundle                |
| CLI assets (CP train)  | Linux/macOS/Windows amd64/arm64 binary、checksums/signature                           |
| Worker release         | Linux amd64/arm64 Supervisor/Worker binary/images、runtime compatibility、attestation |
| Adapter release        | built-in adapter 随 CP/Worker；外部 adapter 使用独立 signed descriptor/image          |
| Deployment release     | signed Compose bundle、Helm chart、values schema、SBOM、upgrade/rollback docs         |
| Platform manifest      | 固定 CP/Worker/Runtime/contract/adapter/image digest matrix                           |
| Reference host release | P3 Managed Host conformance 的公开 reference workload image/descriptor                |
| T3 workload descriptor | 来自 T3 repo 的 signed descriptor，固定 public image/bundle digest                    |

## 4. Release train

| Train           | Tag 示例                                | 可独立发布              |
| --------------- | --------------------------------------- | ----------------------- |
| Runtime         | `cloud-agent-m1-rc.N`                   | 是                      |
| Contracts       | `contracts/v0.1.0-alpha.N`              | 是                      |
| TS SDK          | `sdk/typescript/v0.1.0-alpha.N`         | 是                      |
| Go module       | `services/control-plane/v0.1.0-alpha.N` | 是                      |
| Worker          | `services/worker/v0.1.0-alpha.N`        | 是                      |
| Go SDK          | `sdk/go/v0.1.0-alpha.N`                 | 是                      |
| Reference host  | `reference-host/v0.1.0-alpha.N`         | 是                      |
| Platform bundle | `cloud-agents-platform-v0.1.0-alpha.N`  | 组合 immutable manifest |

GitHub Release 名称不能替代 Go module 标准 tag；image label 不能替代 digest。任何组合 bundle 只引用已有
不可变制品，不重打相同 tag。

`cloud-agent-cli` 随 `services/control-plane/vX.Y.Z` 同版本/tag 发布，是该 train 的跨平台 asset，不设第二个
独立 CLI semver；Platform manifest 仍逐平台固定 CLI binary digest/signature。

TypeScript SDK 的 npm identity 固定为 `@synara/cloud-agent-platform-sdk`。每个 consumer 必须按精确 semver
与 tarball integrity/digest pin；不得使用 `workspace:`、`file:`、Git branch 或浮动 dist-tag。源码公开不自动
批准公开 npm：未关闭对应 `G-EXPOSURE` 时，SDK 只作为 immutable GitHub Release asset 或获批的内部 Registry
制品分发。

## 5. 直接部署要求

公共仓必须提供两条从零安装路径：

1. `docker compose`：一条命令启动 CP/Postgres/storage/Worker，完成 bootstrap、health 和真实 Turn；
2. Helm：安装/升级/回滚、external Postgres/object storage/OIDC、network policy 与 readiness。

两条路径都不得依赖 Synara 源码、私有 package、内部 image、私有数据库 migration 或 ambient credential。

Standalone Platform 默认运行 Managed Agent，并用公开 reference host 验证 Managed Host core。真实 T3 不内嵌进
Platform repo：P6 从 `hxp0618/t3code` 产出公开可获取、签名且 allowlist 的 `HostWorkloadDescriptor` 和
image/bundle digest；Platform manifest 固定它们。未提供有效 descriptor 时，T3 profile fail closed，但
Standalone Managed Agent 仍可部署。

## 6. Same-bits

Platform candidate manifest 至少固定：

- source commit、dirty state、toolchain；
- contracts digest、TS/Go SDK digest 与 generator version；
- Go module、binary、migration digest；
- CP/Worker/Supervisor/Runtime/Provider/container digest；
- reference-host descriptor digest、producer signature/trust identity、image/bundle digest；
- T3 `HostWorkloadDescriptor` digest、producer signature/trust identity、public image/bundle digest；
- Compose/Helm chart digest；
- 每个 artifact 的 SBOM/provenance/signature/VEX digest；
- vulnerability report digest、scanner/version、DB timestamp、waiver digest/expiry、base-image digest、builder identity；
- Synara/T3 consumer commit matrix。

Platform manifest 本身是公共 contract，必须定义：

- `schemaVersion`、canonical JSON/line ordering 与 digest 算法；
- 每个 artifact 的 URI/name/version/digest/media type/platform；
- contract/API compatibility range 与 required capabilities；
- signature identity/trust root、transparency/provenance reference；
- issued/expiry/revocation/supersedes 与 minimum accepted version；
- downgrade/replay 拒绝规则和离线验证 fixture。

`HostWorkloadDescriptor` 必须有独立 schema，至少包含 `schemaVersion`、host kind、producer repo/commit、
image/bundle URI + media type + platform + digest、Managed Host API/Runtime compatibility range、required
capabilities、entrypoint/health/admin protocol、volume/network/endpoint/security-context requirements、
CP/Worker contract range、SBOM/provenance/license reference、signature identity/trust domain、issued/expiry、
revocation ID 与 supersedes。Descriptor、image/bundle 和 provenance 必须分别验签，并证明 subject digest/
source commit 一致。T3 commit 只是 signed descriptor 的 metadata，不能替代可部署 artifact digest。descriptor
revoked/expired 后立即拒绝新 Lease；活动 Lease 按签名 revocation policy fence admission 并 drain/reap，
不得静默换 bits 或继续广告 ready。

CP、Worker、CLI、Synara/T3 client 必须共同消费同一 validator；不能各自只检查字符串格式。

测试后任何 bit 变化会让关联 E2E/Release closure 失效。

历史 `cloud-agent-m1-rc.1` 只能作为 immutable provenance 输入；Platform candidate 引用它之前仍须通过当前
compatibility、vulnerability、SBOM/provenance/signature Gate。若历史 bits 无法满足当前策略，必须发布新的
Runtime candidate，不能把旧 prerelease 自动升级为合格 Platform component。

## 7. 许可与安全治理

- 每个迁移文件记录原 repo/SHA/path/tree hash/copyright/license；
- 全历史 secret scan；不得带入真实账户、endpoint、dump、log、pairing/auth material；
- Go/TS/container dependency license allowlist；
- PR/merge/candidate/定期/advisory-triggered 扫描 Go、TS/npm、CLI binary、OCI OS package、base image、
  Helm/Compose 与 GitHub Actions；candidate 使用的 vulnerability DB 不得早于 24 小时；
- applicable KEV、reachable Critical 阻塞发布，reachable High 默认阻塞；只有记录 owner、不可达 VEX/
  补偿控制、目标 digest、最长 14 天 expiry 的例外才能继续；
- base image 必须 digest pin，并校验来源、签名/provenance、SBOM 与支持期限；不得用 tag 漂移覆盖扫描结果；
- 发布后命中漏洞时执行 advisory/embargo、revoke/supersede 与 minimum accepted version；修复必须产生新
  immutable tag/manifest 并重跑受影响的 same-bits E2E，禁止覆盖旧制品；
- 新 KEV/CVE、waiver expiry、scanner DB stale、signer/trust-root revoke 或 base-image EOL 会使既有
  supply-chain/release closure 失效，即使 artifact digest 未变；
- 独立 CODEOWNERS、安全 DRI、security advisory/embargo 流程；
- 生成器版本、generated SDK 与 schema 可复现；
- image 非 root、最小 capability、无 Docker socket、只读 rootfs 优先；
- 公开源码、公开 prerelease、Registry、部署、Beta、GA 分开批准。

## 8. 何时再拆仓

先同仓、独立 module/release。出现以下条件之一再评估拆为独立公共仓：

- 非 Synara 部署者成为稳定 consumer；
- Control Plane 与 Runtime 发布/安全 embargo 节奏明显不同；
- Go/infra 依赖显著拖慢 Runtime CI 或扩大其供应链 surface；
- Platform Adapter Protocol 已稳定，可跨仓独立演进。
