# Platform P1 execution evidence

- Status：IN PROGRESS
- Fixed decisions：[`ADR-0007`](../adr/0007-p1-contract-data-toolchain-foundation.md)、
  [`ADR-0008`](../adr/0008-p1-postgres-data-kernel.md)、
  [`ADR-0009`](../adr/0009-p1-migration-bundle-runner.md)、
  [`ADR-0010`](../adr/0010-p1-postgres-projection-contract.md)、
  [`ADR-0011`](../adr/0011-p1-membership-rbac-contract.md)、
  [`ADR-0012`](../adr/0012-p1-versioned-lineage-quota-profile.md)、
  [`ADR-0013`](../adr/0013-p1-durable-coordination-contract.md)、
  [`ADR-0014`](../adr/0014-p1-lineage-quota-profile-v3.md)、
  [`ADR-0015`](../adr/0015-p1-compatibility-recovery-contract.md)、
  [`ADR-0016`](../adr/0016-p1-compatibility-recovery-postgres-kernel.md)、
  [`ADR-0017`](../adr/0017-p1-compatibility-recovery-v2-registry.md)、
  [`ADR-0018`](../adr/0018-p1-compatibility-recovery-v2-writer-kernel.md)、
  [`ADR-0019`](../adr/0019-p1-runner-ledger-preflight-contract.md)、
  [`ADR-0020`](../adr/0020-p1-runner-ledger-consumer-contract.md)、
  [`ADR-0021`](../adr/0021-p1-runner-ledger-entry-admission-contract.md)
- Accepted decision（仅授权 ordered local implementation/review）：
  [`ADR-0022`](../adr/0022-p1-runner-ledger-entry-success-writer-contract.md)
- Proposed decision（未批准，不授予 implementation authority）：
  [`ADR-0023`](../adr/0023-p1-runner-ledger-recovery-writer-contract.md)
- Completed slices：P1-A1 Contract Kernel bootstrap (`e0562b280dbbc29604ea1faad9095103ce4548f4`)；SubjectRef/
  HTTP idempotency authority follow-up (`eeb22f26765d99eefcbe316af3ea63991bb5950b`)；SQL/bootstrap authority
  (`4f39b14`)；tenant-scoped pgx read helper (`4af2a66`)；strict migration bundle bootstrap (`363627e`)；
  PostgreSQL tenant-helper matrix (`99a1b54`)；fail-closed migration runner core (`99106e8`)；pgx v5.10.0 +
  x/text v0.39.0 dependency implementation closure artifacts (`93f742f`，不关闭 `G-SUPPLY-CHAIN`)；
  P1-A2.1a-impl-1 strict projection contract/fixture (`b36f45a`)；P1-A2.1a-impl-2 PG adapters
  (`e2541c5`) 与本地 PG15/16/17 fresh A/B matrix (`a0eac37`)；P1-A2.1b-impl-1 relation/function/child-object
  structure、internal dependency closure、denied set 与本地 PG15/16/17 same-bits matrix (`ed37295`)；impl-2 expression
  normalizer 与本地三版本 expression same-bits (`3b3f8f6`)；admission registration/publish/bind/reserve
  chain (`8c9a72b` through `f654aae`)，其中 brand-new generation 已完成 exact
  `GenerationReserved → segment-0 header → GenerationActivated` durability 与 root-wide lock release/opaque
  target+generation lock handoff，并推进到 compact evidencefs snapshot、strict replay 与 sealed same-verifier
  `GenerationRecoveryReady`、receipt-bound retained existing/rotated-segment `EvidenceJournal` composite append/checkpoint、
  unknown reconciliation，以及 sealed current `ActiveGeneration`/`EvidenceSession`；registered ancestor reopen 与 live
  successor 已完成 irreversible full-root reacquisition、adjacent
  `GenerationSuperseded → GenerationReserved`、successor activate/handoff/replay/recovery/journal 和同一 session current swap；
  crash-reopened historical header-only successor 已在 `f654aae` 接入 production `BindSession` 的 B → C 闭图；
  `7b35a3f` 新增并由 `e230c74` 补齐 Linux/macOS SHA-256 tool portability 的 opt-in real Linux ext4/xfs
  clean-restart matrix，在 test-only authority 下验证 actual publish/sync、cross-process flock、publisher kill、
  fresh-process reopen 与 clean unmount/remount；`180929a` 将同一矩阵扩展到 actual target registration、generation
  header/index durability、root-lock handoff、retained existing/rotated-segment composite append、generation-holder kill 与
  full-byte fresh-process/remount replay；`3d21e90` 再以隔离 QEMU guest 整机 `SIGKILL`/fresh boot 覆盖 ext4/xfs
  object 与 generation durable-return 后的 virtual guest-host power cycle；`b6cfa88` 又在 fresh raw disk/new guest
  上覆盖 object publish 的 11 个 real-syscall barrier、whole-QEMU kill 与 sealed fresh-mount recovery classification；
  `daa6b9f` 再覆盖 existing-segment journal+checkpoint composite append 的 10 个 FD-bound `pwrite/fdatasync`
  barrier 与五态 prefix classification；`0e242ee` 继续覆盖 retained segment rotation 的 26 个 FD-bound
  create/write/data-sync/directory-sync barrier 与十态 fresh-mount classification；`be7cae8` 再覆盖 generation
  activation index append 的 5 个 exact-FD write/data-sync barrier 与三态 fresh-mount classification；`139d53a`
  再覆盖 target registration create 的 25 个 mkdir/lock/index barrier 与 torn-prefix recovery 的 21 个
  parent/lock/truncate/rewrite/sync barrier，且每项 fresh boot 均闭合到 exact `registered_empty`；`f650fae`
  再覆盖 generation-header create 的 21 个 directory/lock/segment barrier 与从 durable 28-byte torn segment
  开始的 23 个 recovery barrier，全部 fresh boot 均闭合到 exact 57-byte segment-0/`Revalidate`；`70269e1`
  让 migration pass-1 消费 directory/lock/segment generation-prefix facts，`7b52509` 再以 same-verifier
  historical generation、exact durable `GenerationReserved`、registered receipts 与 fresh evidencefs mutation token
  交叉绑定 one-shot recovery，闭合到 sealed `RecoveredHeaderDurablePermit`；`cdedda7` 再追加 byte-exact adjacent
  `GenerationActivated`、重跑 fresh ALL-history 并闭合到既有 `RegisteredGenerationHandoffPermit`，复用已审
  handoff/replay/session 路径；`a3c7651` 又实现 required-syscall probe，并由同一固定 Linux binary 在 fresh ext4/XFS
  loop mount 上完成 online runtime validation，但没有取得 trusted mount authority；`7d78e3d` 再以 isolated QEMU
  fresh raw ext4/XFS disk 覆盖 generation resync 4、tail truncate 8、checkpoint heal 5 与 rotation discard 4 个
  barrier，每项均 whole-guest kill/fresh-mount classification；`77d92c5` 将 P1 Go pin 从 `1.26.5` 升到三项
  reachable stdlib finding 的共同 first-fixed `1.26.6`，并重跑 module/race/cross-build/clean-restart/QEMU repair；
  `aeed4b2`/`b3f8d9a` 又以 test-only module-policy gate 固定 first-fixed `x/mod v0.40.0`，保持 production
  closure 不变并将 fresh OSV/govulncheck 归零；`381b04a` 新增 root-only non-forgeable trusted-mount
  provision/revoke、Linux production constructor 与 fresh mount revalidation，并用固定 production binary 在真实 fresh
  ext4/XFS direct loop mount 上完成 non-root positive `Open` 和 revoke 后 negative reopen；`3fe05ec` 再新增 public
  production `EvidenceSink` composition root、full-root all-generation lock set 与 exact inventory→handoff identity bridge，
  并在真实 fresh ext4/XFS 上完成 brand-new、registered reopen 和 revoke-negative cross-package session；Supply Gate 仍因
  final artifact scan/immutable closure 保持 OPEN
- Completed slice：P1-A2.2-impl-3 review remediation 已在固定源码 implementation/review 层关闭；该切片由
  [`subject issuer / lineage-index quota blocker`](membership-rbac-subject-issuer-quota-blocker-20260817.md)
  触发；用户已批准 ADR-0012 的 versioned lineage/quota profile 方向，`cd64dee` 已提交并推送
  v1 historical byte-exact compatibility、v2 explicit profile binding、4 KiB checkpoint ceiling、append-only
  `000006` issuer closure，`77de97e` 又将 selected-profile checkpoint ceiling 前移到 stored lineage admission
  decode，证明 4097..16384-byte checkpoint 在 v1 继续兼容而 v2 稳定归类为 stored corruption；`94aef60`
  已固定该 follow-up 与全量 migration 10 分钟既有超时边界；`f7baf95` 修复 signed-bundle fixture
  assertions，`04a61af` 记录 30 分钟 migration rerun 通过，`8d5afdb`/`261be84` 刷新并记录
  pre-review source-bound dependency/SBOM metadata；首次 `gpt-5.6-sol` 复核发现 v1 显式空 profile
  降级边界，`f731c6b` 已修复，`610b1ab` 已刷新 remediation source-bound metadata（见
  [`versioned lineage/quota profile implementation`](versioned-lineage-quota-profile-implementation-20260818.md)）。
  第二轮 `gpt-5.6-sol` independent security review 已返回 `APPROVE, P0=0/P1=0/P2=0`（见
  [`independent review`](versioned-lineage-quota-profile-independent-review-20260818.md)）；该结论不关闭任何
  immutable/aggregate Gate。`f988e45` 已冻结 exact built-in role catalog v1、34 个
  显式 permission、Membership→RoleBinding→resolved scope default-deny authority 与 future-permission version fence；
  `e36e1cf` 已新增 migration-owned RBAC tables、tenant-bound read-only evaluator 与本地 PG15/16/17 normal/race
  focused matrix；`de36ca3` 已新增五个 typed mutation、same-transaction authorization/CAS/audit closure与本地
  PG15/16/17 normal/race mutation matrix，`350b53c` 让 failed/unknown commit 的公开结果严格归零，
  `afe6cb2 → 1ff7713 → 2dc443d` 再关闭 bind-time Membership authority、re-admission non-resurrection、迁移
  input closure 与 current source-bound supply metadata。Independent review 曾确认 direct PostgreSQL issuer
  language gap；append-only `000006` candidate 的本地 PG15/16/17 matrix 虽通过，但 frozen ADR-0010
  lineage-index quota 拒绝该六迁移 bundle。ADR-0012 的 versioned lineage/quota profile implementation 与
  independent review 现已关闭该 remediation 的固定源码边界；checked-in production catalog publication/CLI
  trust root、production database write 与 aggregate Gate 仍保持拒绝
- Completed slice：P1-A2.3 Durable Coordination 已按获批的 generated registry → append-only PostgreSQL kernel →
  service/claim/matrix/review 三切片完成。Versioned lineage/quota v3、`000007`～`000009`、generated operation-specific
  ASCII organization identity 与 typed service/claim/fault matrices 已由 `63699bb → 062cdcf → e55affa` 固定；历史
  Unicode `organizationRef` reviewer P1 已在 exact remediation candidate 中关闭，independent rereview 为
  `APPROVE, P0=0/P1=0/P2=0`。后续对包含既有 `000010` schema-only kernel 的 current bundle，在 `67b8acb` 以
  `-timeout=30m` 完整通过本地 `internal/migration`（`1012.165s`；见
  [`A2.3 full migration closure`](durable-coordination-full-migration-closure-20260820.md)）。该结论只关闭 A2.3
  implementation/review 与 local full-suite evidence；不批准 A2.4 writer/service（见
  [`A2.3 remediation independent review`](durable-coordination-v3-remediation-independent-review-20260820.md)、
  [`A2.3 v3 independent review`](durable-coordination-v3-independent-review-20260819.md) 与
  [`A2.3 evidence-quota blocker`](durable-coordination-evidence-quota-blocker-20260819.md)）。HTTP/P2 external side
  effect 不开放，且不关闭任何 Gate。历史入口审计见
  [`A2.3 pre-entry blocker`](durable-coordination-entry-blocker-20260818.md)
- Historical A2.4 v1 entry：Compatibility/Recovery contract registry 已由 `5a0ed7b` 固定；其后只推进了
  append-only PostgreSQL `000010` schema kernel、generated catalog/manifest 与 PG15/16/17 schema-only matrix。
  现有 registry + schema-only kernel 已完成有界 independent review（见
  [`A2.4 v1/kernel independent review`](compatibility-recovery-v1-kernel-independent-review-20260820.md)）。本切片不实现
  writer/service、HTTP/P2/provider effect，不执行生产数据库写入，也不关闭任何 Gate。
- A2.4 approved entry：[`compatibility-recovery-service-entry-blocker-20260820.md`](compatibility-recovery-service-entry-blocker-20260820.md)
  已获 owner 批准，保持 v1/`000010` 历史 same-bits，并按 versioned registry repair → append-only writer kernel →
  typed service/claim/matrix/independent review 三切片推进；Slice A/B/C 已分别固定，fixed `b639b07` 的 bounded
  independent review 返回 `APPROVE, P0=0/P1=0/P2=0`。HTTP/P2/provider 外部副作用、生产数据库写入、部署、
  发布与所有 Gate 均未获授权。
- A2.4 implementation entry：versioned v2 generated registry/profile repair、append-only `000011` writer kernel、typed
  service/claim and local PG15/16/17 normal/race matrix are implemented and bounded-review approved；no Gate is closed
  (见 [`A2.4 typed service/claim/matrix`](compatibility-recovery-v2-service-claim-matrix-20260820.md) 与
  [`A2.4 independent review`](compatibility-recovery-v2-independent-review-20260820.md))
- P1-A3 SDK/Identity/Closure：Slice A generated common identity (`51e3ea4`), Slice B generated JSON SDK/server
  seam (`24a47b2`) and Slice C generated Proto SDK/fresh consumers (`c5d8cbf`) are fixed. The bounded
  [`independent review`](sdk-identity-closure-independent-review-20260821.md) returned
  `APPROVE, P0=0/P1=0/P2=0`; this completes the ordered A3 implementation/review package but is not an immutable Gate
  signature.
- Runner ledger/catalog preflight：generated profile、locked read-only projection kernel 与 typed same-verifier
  claim/no-op dispatch 已依序固定；Slice A/B fixed candidate `01b1a5f` 的
  [`independent review`](migration-ledger-catalog-preflight-independent-review-20260821.md) 为
  `APPROVE, P0=0/P1=0/P2=1`，唯一 P2 是 executor handoff 的 63-character SHA typo，不影响候选 bytes；Slice C
  `e64e0a2` 的 [`independent review`](migration-ledger-preflight-service-claim-independent-review-20260821.md) 为
  `APPROVE, P0=0/P1=0/P2=0`。固定三元组 focused normal/race、vet/build、两窄 generator、contract-lock 均通过；
  先前当前源码的五分钟 bounded run 正确记录为 **NOT PASS**，随后同一 control-plane subtree 已在 `b57acf2`
  用 Go 1.26.6 完成 uncached full normal `internal/migration` suite（`1108.208s`）。该实现仍没有
  `Runner.Run`/writer consumer、生产数据库写入、HTTP/P2/provider effect 或 Gate closure；full race 与 live
  PostgreSQL 也未由该结果覆盖。
- Runner ledger consumer 已按批准的 generated profile → read-only no-op consumer → matrix/independent review
  三切片完成：v1 preflight registry/profile 保持 byte-identical；固定候选 `dcb4b3a` 仅为
  `complete_return_success / completed / return_success` 开放 `return_success_noop`，其
  [`independent review`](runner-ledger-consumer-service-independent-review-20260822.md) 在 `4209e12` 返回
  `APPROVE, P0=0/P1=0/P2=0`。其余 5 个 entry 与 11 个 recovery/reconcile/failure dispatch 继续稳定返回
  `NOT_IMPLEMENTED`；没有生产数据库写入、HTTP/P2/provider effect、部署、发布或 Gate closure。
- Runner ledger entry admission 已按 ADR-0021 的 generated five-pair profile → fresh locked read-only
  revalidation/close-only permit → matrix/independent review 三切片完成。初次 fixed candidate 因
  `generation.lock` 未绑定修改后的 Go profile test 被独立审查判定 `P1=1` 并撤回；修复候选 `88a5392` 的 lock
  actual/expected byte-exact，`dd5ea657` review verdict 为 `APPROVE, P0=0/P1=0/P2=0`。focused normal、显式
  30-minute focused race、显式 30-minute full normal、vet/build、Linux dual-arch compile、platform contract/lock、lint/
  typecheck/format 与 candidate secret scan 均通过；默认十分钟 full run 明确为 **NOT PASS**。permit 只允许
  `close_without_mutation`，所有 entry/recovery writer、生产数据库写入、HTTP/P2/provider、部署、发布与 Gate
  closure 仍未实现/未授权（见
  [`matrix`](runner-ledger-entry-admission-service-matrix-20260822.md) 与
  [`independent review`](runner-ledger-entry-admission-service-independent-review-20260822.md)）。
- Runner entry writer 的下一 contract-only audit 已在固定 `d4cad5d` 完成。审计确认 ADR-0021 v1
  `permitConsumer=none`、transaction/ledger/evidence mutation forbidden，不能被后续 writer 静默扩权；当前 signed
  bundle 的每 entry statement 数为 `20/71/46/20/1/1/89/34/30/52/161`，也不能复用历史 brand-new
  single-entry/single-statement authority chain。ADR-0022 因此拆分
  `runner-ledger-entry-execution-admission/v1` 与 `runner-ledger-entry-success-writer/v1`，仅提议四个
  first-attempt pair 的 one-entry multi-statement known-success path；retry/abort/reconcile/failure、production DB、
  HTTP/P2/provider、部署、发布与 Gate 继续未授权。D-046 已按 standing automatic-execution approval 接受（见
  [`contract audit`](runner-ledger-entry-writer-contract-audit-20260822.md)）；generated-contract Slice A 的
  [implementation record](runner-ledger-entry-writer-profile-implementation-20260822.md) 已固定 registry/Go ordinary
  profile，候选 `1f1b0c5` 已由 `7615fe5` 独立复核批准。Slice B fresh execution admission 仅接入四个
  generated first-attempt pair、最终 authority/ledger/catalog/evidence/session-boundary 复读与 one-shot
  `close_without_mutation` permit；固定候选 `c375fac` 已由 `d49f89c` 独立复核返回
  `APPROVE, P0=0/P1=0/P2=0`。Slice C 的
  [disconnected one-entry known-success kernel](runner-ledger-entry-success-kernel-service-matrix-20260822.md)
  已在 `9db5891` 固定，并由
  [`818c4d5` independent review](runner-ledger-entry-success-kernel-service-independent-review-20260822.md)
  返回 `APPROVE, P0=0/P1=0/P2=0`。Slice D 的
  [typed caller/first-attempt entry loop](runner-ledger-entry-loop-service-matrix-20260822.md) 已在固定候选
  `9fcdb73` 完成，并由
  [`351e5ea` independent review](runner-ledger-entry-loop-service-independent-review-20260822.md)
  返回 `APPROVE, P0=0/P1=0/P2=0`。retry/abort/reconcile/failure writer 仍未实现；ADR-0022 不把 Slice D
  approval 扩展为 Slice E recovery authority。
- Runner recovery 的
  [`contract-only audit`](runner-ledger-recovery-contract-audit-20260822.md) 已在 current source 上逐项分型
  1 个 excluded retry pair 与 11 个 recovery/reconcile/failure pair，并形成
  [`ADR-0023`](../adr/0023-p1-runner-ledger-recovery-writer-contract.md) proposal。该 proposal 未获 owner 批准；
  所有 pair 继续 `MIGRATION_PROJECTION_NOT_IMPLEMENTED`，没有新增 generated profile、claim/permit、writer、
  production database、HTTP/P2/provider、部署、发布或 Gate authority。Superseding candidate `deb3dc6` 已由
  [`6d4da5b` independent review](runner-ledger-recovery-contract-audit-independent-review-r2-20260822.md) 返回
  `APPROVE, P0=0/P1=0/P2=0`；该 verdict 不接受 ADR-0023，也不授予任何实现 authority。
- Gate closure：none

本目录保存 P1 实现过程中的 dependency review、固定输入和可重放本地证据。只有
[`cloud-agents-platform/evidence`](../cloud-agents-platform/evidence/README.md) 下由独立 reviewer 签署的 immutable
closure record 才能把 `G-CONTRACT`、`G-DATA`、`G-AUTHORITY-P1` 或 `G-SECURITY-P1` 标为 `VERIFIED`。

P1 采用 local-first：开发、focused tests、format/lint/typecheck 与 evidence script 先在本机执行；接近 phase
closure 后才把固定 SHA 在选定 Linux/amd64 主机重放。此目录中的 `PASS` 不等于发布、部署、真实 Provider、
Platform RC、Beta 或 GA。

## Dependency reviews

- [`ajv-8.20.0.md`](dependency-reviews/ajv-8.20.0.md)：P1 JSON Schema 2020-12 validator direct edge；APPROVED
- [`pgx-v5.10.0.md`](dependency-reviews/pgx-v5.10.0.md)：**HISTORICAL BLOCKED**；保留 default
  `x/text v0.29.0` 命中 reachable `GO-2026-5970` 的拒绝依据，实施状态已被下述 closure supersede
- [`x-text-v0.39.0.md`](dependency-reviews/x-text-v0.39.0.md)：pgx remediation exact MVS floor；**APPROVED**，
  已在真实 `go.mod/go.sum` 落盘
- [`pgx-v5.10.0-x-text-v0.39.0-implemented-closure.md`](dependency-reviews/pgx-v5.10.0-x-text-v0.39.0-implemented-closure.md)：
  **APPROVED — dependency implementation closure only**；artifacts 已由 `93f742f` 集成，但
  `G-SUPPLY-CHAIN` 仍为 `IN PROGRESS`
- [`x-mod-v0.40.0.md`](dependency-reviews/x-mod-v0.40.0.md)：**APPROVED — test-gate dependency security
  closure only**；修复 `GO-2026-6179/6180`，保持 Linux/Darwin production closure 和 NOTICE same-bits，
  `G-SUPPLY-CHAIN` 仍为 `IN PROGRESS`
- [`control-plane-supply-refresh-20260817.md`](control-plane-supply-refresh-20260817.md)：固定 source `6e58e06` 的
  generation lock、dependency lock、CycloneDX 1.6 SBOM 与 fresh govulncheck/OSV refresh；module graph、production
  closure 和 NOTICE same-bits，`G-SUPPLY-CHAIN` 仍为 `IN PROGRESS`
- [`postgres-catalog-independent-review-20260817.md`](postgres-catalog-independent-review-20260817.md)：固定
  `bbb0bf2 → 6e58e06 → 401206a` 的 A2.1b-impl-3 independent implementation review；P0/P1/P2=`0/0/0`，不构成
  immutable Gate signature
- [`builtin-role-catalog-contract-20260817.md`](builtin-role-catalog-contract-20260817.md)：固定 `f988e45` 的
  A2.2-impl-1 exact built-in role catalog v1、34 个显式 permission 与 Membership/RBAC authority contract；只关闭
  contract/catalog 实现边界，不构成 runtime、PostgreSQL 或 immutable Gate evidence
- [`membership-rbac-data-evaluator-20260817.md`](membership-rbac-data-evaluator-20260817.md)：固定 `e36e1cf` 的
  A2.2-impl-2 migration-owned Membership/RoleBinding storage、tenant-bound read-only evaluator 与本地 PG15/16/17
  normal/race focused matrix；不构成 mutation service、production catalog publication 或 immutable Gate evidence
- [`membership-rbac-mutation-service-20260817.md`](membership-rbac-mutation-service-20260817.md)：固定
  `de36ca3 → 350b53c → afe6cb2 → 1ff7713 → 2dc443d` 的 A2.2-impl-3 五个 typed mutation、same-transaction
  authorization/CAS/audit、failed/unknown commit zero-result、Membership/RoleBinding admission authority、
  PG15/16/17 normal/race matrix 与 source-bound supply metadata；历史 implementation evidence 保持不变，
  后续 review remediation 由 [quota blocker](membership-rbac-subject-issuer-quota-blocker-20260817.md) 记录，
  不构成 production DB/HTTP/A2.3 或 immutable Gate evidence
- [`versioned-lineage-quota-profile-implementation-20260818.md`](versioned-lineage-quota-profile-implementation-20260818.md)：固定
  ADR-0012 v1/v2 profile implementation、stored-replay ceiling、signed-bundle fixture repair 与
  source-bound dependency/SBOM refresh；fixed-source independent review 已完成，不构成 Gate closure
- [`versioned-lineage-quota-profile-independent-review-20260818.md`](versioned-lineage-quota-profile-independent-review-20260818.md)：固定
  `f731c6b` remediation 的第二轮 `gpt-5.6-sol` verdict `P0=0/P1=0/P2=0`、source/supply binding、独立
  verification limitations 与 remaining boundaries；只关闭 A2.2-impl-3 remediation implementation/review
- [`durable-coordination-entry-blocker-20260818.md`](durable-coordination-entry-blocker-20260818.md)：固定 A2.3
  contract/state/SQL/service pre-entry audit、当前 SubjectRef/idempotency facts、pinned-toolchain guard 边界；其
  section 4 已于 2026-08-19 获批并由 ADR-0013 承接，但仍不授权提前创建 `000007`、HTTP/P2 side effect 或 Gate closure
- [`durable-coordination-contract-registry-20260819.md`](durable-coordination-contract-registry-20260819.md)：记录
  A2.3 slice 1 的 generated profile、七个 closed state machines、deterministic generator、fixture faults 与独立
  generation-lock pipeline；只构成本地实现证据，不是 independent review 或 Gate closure
- [`durable-coordination-postgres-kernel-20260819.md`](durable-coordination-postgres-kernel-20260819.md)：记录
  A2.3 slice 2 的 append-only `000007` schema kernel、generated-profile SQL binding、versioned global-table
  authority、lineage/quota closure 与 PG15/16/17 schema-only matrix；不构成 service/claim、independent review 或
  Gate closure
- [`durable-coordination-service-claim-matrix-20260819.md`](durable-coordination-service-claim-matrix-20260819.md)：固定
  A2.3 slice 3 implementation `59ec260` 的 generated Go profile、append-only `000008` typed service、claim/leader/
  outbox closed outcome 与 PG15/16/17 normal/race/fault matrix；该 fixed slice 文档保留当时 independent review
  `PENDING` 的边界，后续 v3 review 已返回 `NOT APPROVE (P0=0/P1=1/P2=0)`；不开放 HTTP/P2 external
  side effect，也不构成 Gate closure
- [`durable-coordination-evidence-quota-blocker-20260819.md`](durable-coordination-evidence-quota-blocker-20260819.md)：记录
  fixed `000008` 已使 v2 whole-bundle reservation 达到 17 segments 并超过 256 MiB；用户已批准 ADR-0014
  v3 方向，本地候选已通过定向门禁与 PG matrices；原独立 review 的 organization identity P1 已按 generated
  operation-specific ASCII identity profile 闭合并通过 remediation independent review，full migration closure
  已在 `67b8acb` 的 30 分钟本地 rerun 完成；精确命令、耗时与不扩大 A2.4/Gate 边界见
  [`full migration closure`](durable-coordination-full-migration-closure-20260820.md)，不构成 Gate closure
- [`durable-coordination-v3-independent-review-20260819.md`](durable-coordination-v3-independent-review-20260819.md)：记录
  fixed HEAD 加当时 dirty/untracked v3 candidate 的独立只读终审；`P0=0/P1=1/P2=0`、整体 `NOT APPROVE`，
  唯一 P1 是 generated Unicode `organizationRef` 与 ASCII-only authorization `ScopeRef` 合约断裂。后续本地
  remediation 另有 generated operation-specific identity profile 与 versioned `000009` pair，但不改变该历史 verdict；
  不授权静默映射、HTTP/P2 external effect 或 Gate closure
- [`durable-coordination-v3-remediation-independent-review-20260820.md`](durable-coordination-v3-remediation-independent-review-20260820.md)：
  对 exact remediation candidate 的 independent rereview；`APPROVE, P0=0/P1=0/P2=0`，只关闭 generated
  registry/profile → append-only PostgreSQL kernel → service/claim/matrix implementation/review slice。后续 local
  full migration closure 见 [`closure record`](durable-coordination-full-migration-closure-20260820.md)；HTTP/P2
  external effect 与所有 Gate 仍保持 OPEN
- [`compatibility-recovery-postgres-kernel-20260820.md`](compatibility-recovery-postgres-kernel-20260820.md)：记录
  A2.4 append-only `000010` schema-only kernel、generated registry/catalog/manifest binding 与本地 PG15/16/17
  matrix；仅为 local implementation evidence；有界 independent review 见
  [`compatibility-recovery-v1-kernel-independent-review-20260820.md`](compatibility-recovery-v1-kernel-independent-review-20260820.md)。
  该历史 record 当时未声称 full migration closure；后续包含 `000010` 的 current-bundle local closure 见
  [`A2.3 full migration closure`](durable-coordination-full-migration-closure-20260820.md)，但不改变其有界 review，
  也不批准 writer/service、HTTP/P2/provider effect 或任何 Gate closure
- [`compatibility-recovery-service-entry-blocker-20260820.md`](compatibility-recovery-service-entry-blocker-20260820.md)：
  A2.4 versioned-registry/writer/service 入口记录；v1 与 `000010` 不可变，三切片已获 owner approval，且仍
  不授权 HTTP/P2、生产写入或 Gate closure
- [`compatibility-recovery-v2-service-claim-matrix-20260820.md`](compatibility-recovery-v2-service-claim-matrix-20260820.md)：
  记录 A2.4 typed generated-operation service/claim consumer、single-attempt unknown/reconcile handling 与本地
  PostgreSQL 15/16/17 normal/race matrix；后续 bounded independent review 已批准，HTTP/P2/provider、生产数据库写入、部署、发布与
  所有 Gate 仍保持拒绝/open
- [`compatibility-recovery-v2-independent-review-20260820.md`](compatibility-recovery-v2-independent-review-20260820.md)：
  固定 `b639b07` 的 A2.4 generated v2 registry/profile、append-only `000011`、typed service/claim 与 PostgreSQL
  15/16/17 normal/race matrix review；verdict `P0=0/P1=0/P2=0`，不构成生产写入、部署、发布或 Gate closure
- [`sdk-identity-closure-entry-20260820.md`](sdk-identity-closure-entry-20260820.md)：固定 A3 generated identity、
  JSON SDK/server seam、Proto SDK/fresh consumer 三切片顺序及明确非授权边界
- [`sdk-proto-consumer-closure-20260821.md`](sdk-proto-consumer-closure-20260821.md)：保留 `c5d8cbf` 提交前
  fixed candidate 的 Proto/consumer 实现与本地验证记录；其当时的 review-pending 状态由后续记录承接
- [`sdk-identity-closure-independent-review-20260821.md`](sdk-identity-closure-independent-review-20260821.md)：
  固定 `51e3ea4 -> 24a47b2 -> c5d8cbf` 的 A3 bounded independent review；verdict
  `P0=0/P1=0/P2=0`，SDK 仍未发布，不开放 HTTP/P2/provider/生产 DB 副作用，不关闭任何 Gate
- [`migration-ledger-catalog-preflight-independent-review-20260821.md`](migration-ledger-catalog-preflight-independent-review-20260821.md)：
  固定 `01b1a5f` 的 generated profile/read-only kernel review；verdict `APPROVE, P0=0/P1=0/P2=1`，P2 仅为
  executor handoff SHA typo；不进入 writer、生产数据库 mutation 或 Gate closure
- [`migration-ledger-preflight-service-claim-matrix-20260821.md`](migration-ledger-preflight-service-claim-matrix-20260821.md)：
  固定 `e64e0a2` 的 one-shot same-verifier claim、17-pair dispatch matrix、PG15/16/17 metadata matrix 与明确
  non-claim；其中的五分钟 broad migration NOT PASS 是当时的历史边界，由后续 current-source closure 承接
- [`migration-ledger-preflight-service-claim-independent-review-20260821.md`](migration-ledger-preflight-service-claim-independent-review-20260821.md)：
  固定 `e64e0a2` 的 Slice C independent review；verdict `APPROVE, P0=0/P1=0/P2=0`，无 `Runner.Run`、writer、
  HTTP/P2/provider consumer，且不构成 immutable Gate signature
- [`runner-ledger-current-source-full-migration-closure-20260821.md`](runner-ledger-current-source-full-migration-closure-20260821.md)：
  固定 control-plane subtree `091a42f5` 在 Go 1.26.6 的 uncached full normal `internal/migration` PASS
  （`1108.208s`）；不构成 full race、live PostgreSQL、独立 Gate signature 或生产副作用证据
- [`current-source-migration-shard-closure-20260822.md`](current-source-migration-shard-closure-20260822.md)：
  固定 source `7f14c7f` 新增确定性 mutually-exclusive/exhaustive shard runner，并把当前 700 个顶层 migration
  tests 在一次 run 中按 8 片完整执行；normal 结果为 `695 pass + 5 explicit external-PG skip`、零 fail、wall
  `550s`。后续独立复核确认这些 artifact 事实，但对可复用 runner 返回 `BLOCK, P0=0/P1=2/P2=0`：信号只终止
  wrapper，且 PASS 前未自行闭合 JSON run-set；该记录保留为历史 local-run evidence，不构成 approved runner、full
  race、live PostgreSQL、外部副作用或任何 Gate closure
- [`migration-shard-runner-closure-repair-20260822.md`](migration-shard-runner-closure-repair-20260822.md)：
  固定 implementation `17f74f0` 以独立 process group/TERM→KILL/no-descendant closure、launch-before-registration
  signal deferral、unexpected-exit group cleanup、ABORTED status 和 strict Go JSON validator 修复上述两个 P1 及首次
  修复复审发现的 launch window P1；Bash 3.2 fake two-test result/post-registration/pre-registration signal/
  unexpected-exit fixture、focused normal/race/vet、exact ShellCheck、plan same-bits 与旧 700-test artifacts 的
  read-only parse 均通过。按 no-repeat policy 未重跑 full migration；当前仍待新 fixed-candidate independent
  rereview，所有 Gate 保持 OPEN

## Data kernel decisions

- [`ADR-0008`](../adr/0008-p1-postgres-data-kernel.md)：global-table allowlist、bootstrap authority、tenant
  context ABI、migration lineage/checksum 与 P1-A2.1～P1-A2.4 execution slices。
- [`ADR-0009`](../adr/0009-p1-migration-bundle-runner.md)：schema/bootstrap/manifest/runner 四类 digest、deterministic
  bundle、外部 trust anchor、strict SQL/catalog contract、ambiguous commit 与 tenant helper 生命周期。
- [`ADR-0010`](../adr/0010-p1-postgres-projection-contract.md)：冻结 signed expected authority/catalog contract、
  version-neutral typed projection、snapshot/transaction 边界及 P1-A2.1a/P1-A2.1b 实施顺序；细化 verified
  authority binding，但不改变 schema ledger 或 Gate 语义。
- [`ADR-0011`](../adr/0011-p1-membership-rbac-contract.md)：冻结 Membership admission、RoleBinding explicit allow、
  resolved request-scope containment、exact built-in role catalog、deny-only external PDP 与 A2.2 三切片顺序。
- [`ADR-0012`](../adr/0012-p1-versioned-lineage-quota-profile.md)：冻结 v1 historical byte-exact compatibility、v2
  explicit lineage/quota profile、4 KiB checkpoint ceiling、profile-bound generation authority 与 fail-closed
  transition rules；不构成 admission/Gate closure。
- [`ADR-0013`](../adr/0013-p1-durable-coordination-contract.md)：冻结 generated contract-registry profile、七个
  closed durable state machines、idempotency/outbox/leader/audit policy 与 A2.3 三切片顺序；不开放 HTTP/P2 external
  side effect，也不构成任何 Gate closure。
- [`ADR-0014`](../adr/0014-p1-lineage-quota-profile-v3.md)：冻结 generated-manifest v3 selector、32-segment /
  512 MiB generation reservation、v1/v2 same-bits 与 profile swap fail-closed；不开放 rollover、HTTP/P2 或 Gate
  closure。
- [`ADR-0015`](../adr/0015-p1-compatibility-recovery-contract.md)：冻结 generated compatibility/recovery registry、
  closed state machines 与本地 logical recovery/preflight 边界；不构成 SQL/service 或 Gate authority。
- [`ADR-0016`](../adr/0016-p1-compatibility-recovery-postgres-kernel.md)：冻结 append-only `000010` schema-only
  PostgreSQL kernel、exact generated registry binding、migration-owner tables 与 pure runtime digest helpers；不开放
  writer/service、HTTP/P2/provider effect 或任何 Gate closure。

## Current admission durability evidence

- [`admission-generation-durability-20260813.md`](admission-generation-durability-20260813.md) 固定 source
  `5e0065afededa163a186d4ee706bfb2cc437f63f`，记录 brand-new receipt-bound reservation、generation
  journal/segment-0 durability、exact activation append、root-wide lock release、retained normal-run journal、segment
  rotation、unknown reconciliation、current active/session sealing、fault gates 与未实现边界。
- [`successor-generation-session-20260814.md`](successor-generation-session-20260814.md) 固定 source `cebacea`，记录
  registered ancestor session、generation-lease→full-root reacquire、successor content/receipt/index/header/activation、
  retained replay/recovery/journal 和 same-session current swap。
- [`historical-successor-process-restart-20260816.md`](historical-successor-process-restart-20260816.md) 固定 source
  `f654aae`，记录 strict historical B replay 后的 one-way full-root reacquire、B → C materialization 与 current session wiring。
- [`evidencefs-linux-filesystem-matrix-20260816.md`](evidencefs-linux-filesystem-matrix-20260816.md) 固定 source
  `e230c74`，记录 OrbStack Linux/arm64 上真实 ext4/xfs loop mount、actual publish/sync、cross-process flock、
  killed publisher 后 fresh process reopen 与 clean unmount/remount；production `Open` 同场继续 fail closed。
- [`evidencefs-linux-generation-matrix-20260816.md`](evidencefs-linux-generation-matrix-20260816.md) 固定 source
  `180929a`，在相同真实 ext4/xfs harness 上增加 target/generation/index/segment transition、handoff 后分层锁验证、
  generation-holder `SIGKILL`、fresh-process exact-byte replay 与 clean remount replay。
- [`evidencefs-qemu-powerloss-matrix-20260816.md`](evidencefs-qemu-powerloss-matrix-20260816.md) 固定 source
  `3d21e90`，记录隔离 QEMU guest 的 ext4/xfs object/generation durable-ready→whole-guest `SIGKILL`→fresh-guest
  exact-byte recovery；不外推到 physical controller 或 per-barrier power loss。
- [`evidencefs-qemu-object-publish-barrier-matrix-20260816.md`](evidencefs-qemu-object-publish-barrier-matrix-20260816.md)
  固定 source `b6cfa88`，记录 ext4/xfs 上 object publish 11 个 syscall barrier 的 fresh-disk、whole-QEMU kill 与
  sealed fresh-mount state classification；不外推到 generation barriers 或 physical controller。
- [`evidencefs-qemu-generation-append-barrier-matrix-20260816.md`](evidencefs-qemu-generation-append-barrier-matrix-20260816.md)
  固定 source `daa6b9f`，记录 ext4/xfs 上 existing-segment composite append 10 个 FD-bound write/sync barrier 的
  whole-QEMU kill 与 sealed fresh-mount five-state classification；不外推到 generation create/rotation/repair barriers。
- [`evidencefs-qemu-generation-rotation-barrier-matrix-20260816.md`](evidencefs-qemu-generation-rotation-barrier-matrix-20260816.md)
  固定 source `0e242ee`，记录 ext4/xfs 上 retained segment rotation 26 个 FD-bound create/write/data-sync/
  directory-sync barrier 的 whole-QEMU kill 与 sealed fresh-mount ten-state classification；不外推到 generation
  registration/header/activation/repair barriers。
- [`evidencefs-qemu-generation-activation-barrier-matrix-20260816.md`](evidencefs-qemu-generation-activation-barrier-matrix-20260816.md)
  固定 source `be7cae8`，记录 ext4/xfs 上 generation activation index append 5 个 exact-FD write/data-sync
  barrier 的 whole-QEMU kill 与 sealed fresh-mount three-state classification；不外推到 header/repair
  barriers。
- [`evidencefs-qemu-target-registration-barrier-matrix-20260816.md`](evidencefs-qemu-target-registration-barrier-matrix-20260816.md)
  固定 source `139d53a`，记录 ext4/xfs 上 target registration create 25 个与 torn-prefix recovery 21 个
  exact-syscall barrier 的 whole-QEMU kill、sealed fresh-mount classification、fresh mutation token recovery 与最终
  `registered_empty`/`Revalidate`；不外推到 generation header/repair barriers。
- [`evidencefs-qemu-generation-header-barrier-matrix-20260816.md`](evidencefs-qemu-generation-header-barrier-matrix-20260816.md)
  固定 source `f650fae`，记录 ext4/xfs 上 generation-header create 21 个与 torn-prefix recovery 23 个
  exact-syscall barrier 的 whole-QEMU kill、sealed prefix/journal classification、fresh token completion 与最终 exact
  57-byte segment-0/`Revalidate`；不外推到 production verified reopen binder、remaining repair 或 physical controller。
- [`generation-prefix-reopen-binder-20260816.md`](generation-prefix-reopen-binder-20260816.md) 固定 source
  `7b52509`，记录 migration-owned pass-1 generation-prefix transcript 与 same-verifier one-shot recovery binder，闭合到
  sealed `RecoveredHeaderDurablePermit`；不外推到 adjacent activation、handoff、trusted-mount integration 或 Gate closure。
- [`generation-prefix-activation-handoff-20260816.md`](generation-prefix-activation-handoff-20260816.md) 固定 source
  `cdedda7`，记录 recovered-header exact adjacent activation、fresh ALL-history rebind 与既有 retained-lock handoff bridge；
  不外推到 positive trusted-mount integration、runner/DB 或 Gate closure。
- [`evidencefs-required-syscall-probe-20260816.md`](evidencefs-required-syscall-probe-20260816.md) 固定 source
  `a3c7651`，记录 package-private online probe、fault cleanup 与同一固定 Linux binary 的 fresh ext4/XFS runtime PASS；
  production `Open` 仍拒绝，不外推到 trusted-mount authority、power loss 或 Gate closure。
- [`evidencefs-trusted-mount-authority-20260817.md`](evidencefs-trusted-mount-authority-20260817.md) 固定 source
  `381b04a`，记录 root-only provision/revoke、opaque mount claim、production Linux `Open`/fresh revalidation、fault/static
  gates，以及同一固定 binary 在真实 ext4/XFS 上的 UID 1001 positive open 与 revoke-negative reopen；不外推到 migration
  cross-package binding、physical controller power loss、runner/DB 或 Gate closure。
- [`migration-production-evidence-sink-20260817.md`](migration-production-evidence-sink-20260817.md) 固定 source
  `3fe05ec`，记录 public production `EvidenceSink`、full-root all-lineage/all-generation nonblocking lock authority、exact
  inventory→handoff identity、closed cleanup，以及真实 ext4/XFS brand-new/registered reopen/revocation matrix；不外推到
  runner/DB、physical controller power loss 或 Gate closure。
- [`postgres-catalog-structure-20260817.md`](postgres-catalog-structure-20260817.md) 固定 source `ed37295`，记录
  A2.1b-impl-1 relation/function/child object、internal dependency closure、denied set、expression rejecting boundary 与
  本地 PG15/16/17 same-bits matrix；exported projector、runner/DB、signed subject 和 Gate 仍保持关闭。
- [`postgres-catalog-expression-20260817.md`](postgres-catalog-expression-20260817.md) 固定 source `3b3f8f6`，记录
  closed `pg_node_tree`/deparse expression normalizer、owner/type/dependency rebinding、fault tests 与 PG15/16/17
  same-bits；exported projector、runner/DB、signed subject 和 Gate 仍保持关闭。
- [`evidencefs-qemu-generation-repair-barrier-matrix-20260816.md`](evidencefs-qemu-generation-repair-barrier-matrix-20260816.md)
  固定 source `7d78e3d`，记录 ext4/XFS 上 resync/truncate/checkpoint/discard 共 21 个 exact syscall barrier 的
  whole-QEMU kill 与 sealed fresh-mount classification；不外推到 trusted mount、physical controller 或 Gate closure。
- [`evidencefs-physical-powerloss-entry-audit-20260822.md`](evidencefs-physical-powerloss-entry-audit-20260822.md)
  在固定 source `d6ec6c8` 上完成物理掉电入口的只读安全审计：可达裸机仍承载活动工作负载且没有独立一次性测试盘，
  DUT-local management interface 也不能替代第二控制机的外部 hard-off/hard-on recovery path；因此未执行任何断电、
  重启、安装、文件系统或数据库操作，physical controller/host power-loss 与 filesystem-slice Done 继续开放。
- [`go-toolchain-1.26.6-security-refresh-20260816.md`](go-toolchain-1.26.6-security-refresh-20260816.md) 固定 source
  `77d92c5`，记录 Go 1.26.5 reachable stdlib finding → 1.26.6 remediation、穷尽 race 分片、三平台 compile、
  ext4/XFS clean-restart、42-barrier QEMU repair 与 fresh vulnerability scan；同时保留 `x/mod v0.37.0` OSV blocker。
- [`dependency-reviews/x-mod-v0.40.0.md`](dependency-reviews/x-mod-v0.40.0.md) 固定 source `b3f8d9a`，记录
  test-only exact direct floor、`x/mod v0.40.0`/`x/tools v0.49.0` provenance、16-module fresh zero-finding scan，
  以及 unchanged Linux 7/30、Darwin 6/29 production closure。
- 以上记录都是 local implementation evidence，不是独立 reviewer 签署的 immutable Gate closure；public production
  `EvidenceSink` 已存在并通过 scoped real ext4/XFS cross-package replay，但仍没有 runner/DB、physical controller
  power-loss 或 immutable Gate authority。

## Projection runner boundary (still open)

`services/control-plane/migrations/` 已建立 database/role bootstrap、migration ledger schema、Tenant/Organization/
Project、tenant-local revision/change fact、`FORCE RLS` 与 audited Tenant bootstrap 的 SQL authority；runner 已具有
exact-byte bundle、strict classifier、transaction-local tenant helper 和 PostgreSQL 15/16/17 helper matrix。
`93f742f` 又集成了 pgx/x-text dependency lock、SBOM、notice 与本地时点扫描，但该记录只关闭依赖实施切片。

`b36f45a` 已完成 strict typed projection contracts、Go/TS fail-closed validators、共享 golden/fault fixtures、
generation-lock/source provenance，以及 scoped predecessor ABI。`e2541c5` 完成 verified wrapper、sealed snapshot、
PG15/16/17 capability/authority/namespace/default-ACL projector；`a0eac37` 在本地 `linux/arm64` 用固定 PG15.18、
16.14、17.10 镜像完成 fresh A/B、normal/race 两轮矩阵。`ed37295` 又完成 relation/function/child-object ordinary
structure、internal dependency closure 和 expression slot rejecting boundary；`3b3f8f6` 完成 package-private
expression normalizer 与 semantic dependency rebinding，并在本地 PG15/16/17 arm64 镜像保持代表性结构、表达式与
checked-in 000001/000002 same-bits。该证据不外推到 x86_64、云环境或 Gate closure。

Projection runner 仍只进入 statement 前/后与 pre-ledger `ControlPlaneStates`/intermediate chain。当前新增的
[`runner/CLI pre-DB configuration`](migration-runner-cli-pre-db-configuration-20260821.md) 只固定显式
`--evidence-root`、数据库 locator 与 trust-before-artifact/evidence/DB 的配置顺序；CLI 仍使用
`RejectingTrustVerifier`，不打开正向执行或生产数据库路径。
其固定候选的有界 independent review 已批准（见
[`runner/CLI pre-DB independent review`](migration-runner-cli-pre-db-configuration-independent-review-20260821.md)，
`P0=0/P1=0/P2=0`），仅确认该配置与 fail-closed 顺序，不构成正向 trust、生产数据库、部署、发布或任何 Gate
closure。
独立的 [`runner ledger/catalog preflight entry`](migration-ledger-preflight-entry-blocker-20260821.md) 已完成三切片：
generated/versioned profile、dedicated-session locked read-only kernel，以及 same-verifier one-shot claim/no-op dispatch。
Slice A/B 与 Slice C 的 independent reviews 已分别固定 `01b1a5f` 与 `e64e0a2`；后者 verdict 为
`APPROVE, P0=0/P1=0/P2=0`。该 service 是 read-only kernel 的唯一 package-private production caller，但仍没有
`Runner.Run`/writer consumer，不创建 migration/RW transaction，不执行 production database write，也不开放
HTTP/P2/provider 或任何 Gate closure（见 [`ADR-0019`](../adr/0019-p1-runner-ledger-preflight-contract.md)）。
`bbb0bf2` 已关闭 complete synthetic signed subject 的 exported expression/catalog projector 与本地双快照矩阵；
Signed expected subject 的 production verifier、deployment trust-root wiring、
crash/recovery、N-1/PITR 和 immutable Gate closure 均未实现。现有 catalog 继续保持
`UNPUBLISHED_BOOTSTRAP_MUTABLE` / `NOT_IMPLEMENTED`，生产 CLI 继续在读取数据库前拒绝；因此
`G-DATA`、`G-AUTHORITY-P1`、`G-SECURITY-P1` 与 `G-SUPPLY-CHAIN` 均不得标为 `VERIFIED`。

在上述 projection 工作之后，admission durability 已从 sealed non-runnable `GenerationRecoveryReady` 继续推进到
receipt-bound concrete journal 与 current `ActiveGeneration`/`EvidenceSession`：root-wide 与 non-target locks 已释放，只保留
exact target lineage + generation lock pair，并已完成 compact snapshot/strict replay、same-verifier facts、typed publication
receipt 自有化，以及当前 cursor/recovery snapshot 的 session accessor。
但这没有改变 Gate 结论：`381b04a` 关闭 production trusted-mount constructor/wiring 和 scoped positive
production `Open`，`3fe05ec` 再关闭 public sink 与 scoped production-opened cross-package
brand-new/registered activation-handoff；runner/DB `Connect` 与真实 physical controller
power-loss 证据仍然开放。
`b6cfa88`、`daa6b9f`、`0e242ee`、`be7cae8`、`139d53a`、`f650fae` 只在 package-private test authority 下完成
isolated QEMU guest 的 object publish、existing-segment append、retained rotation、activation、target registration/recovery
与 generation header create/recovery barrier kill/recovery；
`a3c7651` 完成 package-private required-syscall probe 和 fresh ext4/XFS online validation，`381b04a` 再用 root-owned
mount authority 驱动同一 probe through production `Open`，
`70269e1`/`7b52509`/`cdedda7` 完成 migration ordinary transcript、composite recovery authority 与 existing-handoff
bridge，`3fe05ec` 已将这些 reviewed seams 接入同一 production-opened store 并在 fresh ext4/XFS 上重放。该 scoped
cross-package 证据不等于 runner/DB integration、filesystem slice reviewer-signed Done，也不允许直接把 A2.1b 或任何 Gate
标为完成。
