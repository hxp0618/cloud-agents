# P1 admission generation durability implementation evidence

- Status：**LOCAL IMPLEMENTATION VERIFIED — GATE OPEN**
- Scope：brand-new admission 的 receipt-bound reservation、generation journal/segment-0 创建、header durability、
  `GenerationActivated` durable append、root-wide lock release、retained generation snapshot、strict replay、same-verifier recovery
  binding、existing/rotated-segment composite append/checkpoint、ten-state unknown classification、response-lost repair、
  receipt-owned journal 与 sealed current `ActiveGeneration`/`EvidenceSession`
- Fixed source commit：`5e0065afededa163a186d4ee706bfb2cc437f63f`（feature-branch implementation commit）
- Fixed implementation tree：`d958b7241b6b621a20ea6240fcc48d9e205cf912`（`5e0065a^{tree}`，不含本次证据文档更新）
- Branch：`codex/cloud-agents-platform-p1`
- Date：2026-08-14 Asia/Shanghai
- Toolchain：Go `1.26.5 darwin/arm64`
- Record type：implementation evidence；**不是** Gate closure record

本记录固定
`ReceiptBoundReady → ReservedDurablePermit → HeaderDurablePermit → GenerationReadyPermit → GenerationHandoffReady → GenerationReplayReady → GenerationRecoveryReady → EvidenceJournal → current ActiveGeneration/EvidenceSession`
的本地实现证据。
它证明 brand-new generation 在同一 active admission epoch 与 retained lock chain 下，按
`GenerationReserved → exact segment-0 JournalHeader → GenerationActivated` 的顺序完成代码级 durability barrier。
它不证明 production trusted mount、successor generation、public sink constructor、数据库连接、
真实文件系统掉电恢复或 Platform RC。

## Fixed implementation commits

| Commit                                     | Slice                                | Result                                                                         |
| ------------------------------------------ | ------------------------------------ | ------------------------------------------------------------------------------ |
| `5a56caa7f515c7f4b2c5e9caf6bfd17ec2f00238` | evidencefs generation journal create | durable directory/lock/segment-0 create and retained generation lock           |
| `e5283693f116552ecefdb6ef626668ca0f931666` | migration generation reserve         | exact planned `GenerationReserved` append and sealed `ReservedDurablePermit`   |
| `1ecbba8db051e5d1ee0d1a6dd66b810a9c6030cf` | migration generation header          | exact planned header create and sealed `HeaderDurablePermit`                   |
| `5896da7c6c4bc75055bfad7dc63db913bb5a9446` | migration generation activation      | exact `GenerationActivated` append and sealed `GenerationReadyPermit`          |
| `8bfe7c816811ab0a1885b65a53b7a621b66d1144` | evidencefs lock handoff              | release root/non-target locks and retain exact target/generation lock pair     |
| `c017c9573015d7e91099d71744459f9f7478594d` | migration lock handoff               | consume generation-ready and seal non-runnable `GenerationHandoffReady`        |
| `3b1534069ab58553a3176f47a9eac6eb11ec47c7` | retained generation snapshot         | compact exact index/segment facts with bounded re-read and terminal revalidate |
| `fa7f8e13ae5ecee3cdc723b8674ab1d8512be5df` | migration strict generation replay   | consume handoff and seal non-runnable `GenerationReplayReady`                  |
| `96e6165870eae161dc94bdb65e19497fea14dc76` | migration same-verifier recovery     | bind private brand-new cursor/snapshot without runtime or append authority     |
| `460a1d74f52e6423c4d33fddcec6fd8fd3c0f68d` | retained checkpoint append           | opaque index checkpoint append with replacement snapshot                       |
| `5315e0603c29a95b67a1685ad90ce0a62a813089` | retained checkpoint healing          | exact journal-complete checkpoint healing transition                           |
| `7c9396f3550f43112373c170cf520c53309ab149` | retained snapshot resync             | redo segment/index durability barriers without changing bytes                  |
| `3084ecdc8cb35eacf15caf2e19873e4ab733a7a6` | retained tail repair                 | shrink only torn final segment/index suffixes                                  |
| `33f1c7d422d59ec604200838e154969d3dfa59a3` | unknown append classification        | closed unchanged/torn/complete composite byte classification                   |
| `544004ac3526bbebaf154d1f0cf9cd64ab9c722e` | migration retained EvidenceJournal   | sealed replay/append/close plus five-way unknown reconciliation                |
| `145ceb2049a3b2d657dad5ce0ae27ac4c633c3ea` | evidencefs segment rotation          | exact new-segment/header/caller/checkpoint durability order                    |
| `f0d9a665e53521d9fd18d0a5361074b6a1cd7541` | rotation unknown classification      | ten-state byte-level observation and relaxed response-lost replay classifier   |
| `93d3263ce6f8dba167c42645c1005bc753e482b4` | migration rotation integration       | prepared/durable/unknown rotation journal binding and torn-segment discard     |
| `5e0065afededa163a186d4ee706bfb2cc437f63f` | migration current evidence session   | receipt-owned journal plus sealed current active generation and session        |

Key fixed file identities:

| File                                                         | SHA-256                                                            |
| ------------------------------------------------------------ | ------------------------------------------------------------------ |
| `internal/evidencefs/admission_journal_create.go`            | `428008d074db6ec93c1b0b8b50f803f28b517d3af7aab88857d15f49d1f3f3f5` |
| `internal/evidencefs/admission_journal_create_test.go`       | `77f08741a5ebc6a811989a7d813610b3c379247858152490871517759aea5f66` |
| `internal/migration/evidence_admission_reserved.go`          | `370c1bb930713e8f7da9f9af9158ea0c0428ce50aa7fb796d46e31d57847b518` |
| `internal/migration/evidence_admission_reserved_test.go`     | `ea5d820bcb67125e36bcba5d2bfcf109c9eba7135c42be33b5f8c6cea56326b5` |
| `internal/migration/evidence_admission_header.go`            | `8da40eb19a3838dea7e39327d0c78ceae5fac326bb6d686c3e4943279cad649f` |
| `internal/migration/evidence_admission_header_test.go`       | `6300f021349d3fdbd17f1aa211d72332572e2def5b43a585e6279172321c0688` |
| `internal/migration/evidence_admission_activation.go`        | `981edd5e0af5046aec0796d371ff13535a22f974611af0e3310edb696a7a2b72` |
| `internal/migration/evidence_admission_activation_test.go`   | `dbed639b6e3289542637e45a416b6eeff84cdd8905e7f4cfd7b8e271b77ec04f` |
| `internal/evidencefs/admission_handoff.go`                   | `67b99efddcba783d1702f7a87ab3e7203ba977632ef74df4ecce8a5f75645533` |
| `internal/evidencefs/admission_handoff_test.go`              | `5c8ffb744fe7a1db83c70c6a79ff9483b31bdf5501e24d66b2779396c1bcfa9e` |
| `internal/migration/evidence_admission_handoff.go`           | `05d5b1b70e57c3ee9fdfba2ed4690a80893d2dfe57ef4925beb80631cc5e22f2` |
| `internal/migration/evidence_admission_handoff_test.go`      | `eba777211843b3d444ab97432c5d5fe8603140d334db5f03d998a6a96148c801` |
| `internal/migration/evidence_admission_history.go`           | `47d1926437af65155104764fe905ddae3a5e20e8af3ab66b67f785878e597ad0` |
| `internal/migration/evidence_admission_history_test.go`      | `7e40d08539c0b034efd2d3d69a1f3b2dc20897e6f7874a8dd75f8263e4a5e989` |
| `internal/migration/evidence_generation_recovery.go`         | `7f83c22ae474f6b81f590b8971bf3c1c69ea7015d3351c7422bd4420ffa00e0f` |
| `internal/migration/evidence_generation_recovery_test.go`    | `f5023382a43655504c30554589748914404b0151999cdd66f04c9b277e860aae` |
| `internal/evidencefs/generation_append.go`                   | `387182e9575cfe745b0c9aad73411f6582a80a7232ddd792856754aa5a4405dd` |
| `internal/evidencefs/generation_checkpoint.go`               | `503e55da43f837e174a61d0fb638a5b63b3fd0da68c08cb212ca8332d649f31c` |
| `internal/evidencefs/generation_resync.go`                   | `9b187b594d096a43a1afdf12575375285395dd6019240d628d30236dd3951a9c` |
| `internal/evidencefs/generation_truncate.go`                 | `67fa4229615df349eb5ad035e3b126d1337bdbecb9ce53f5b199a1316cd16eea` |
| `internal/evidencefs/generation_append_reconcile.go`         | `04cef92367bc745dc084e975e144ca49c8725b7a516e74a88c1be017c0a8030b` |
| `internal/evidencefs/generation_rotate.go`                   | `e3083491e3829c52cdf840557a5fd57eb9a162f79ab28b9b2b8b0c2f99f41006` |
| `internal/evidencefs/generation_rotate_reconcile.go`         | `4761afbee3d7770421d450cd773cfb0f18d7a18bc131bca33863bd8e007462bb` |
| `internal/evidencefs/generation_rotate_discard.go`           | `0535490ad33acbe0662f6126e5894f53de749438be6ad6e797d3ce99209730eb` |
| `internal/evidencefs/generation_rotate_test.go`              | `cc4d69eaa018f2e4c8d0594b358694506cf4a2448cff0cd8f00913d6e08a9d43` |
| `internal/migration/evidence_generation_journal.go`          | `90576941406d370844757b62f13e9739dcb1dd8b19d3012f7c0e2db46dac09b5` |
| `internal/migration/evidence_generation_journal_rotation.go` | `e5865bfc0d30ced1aa005a940f32d19a85b1970397a8e65679a378682458cd94` |
| `internal/migration/evidence_generation_journal_test.go`     | `5606b47f4f58b67bc20ac9cdc91ed2c3656d1b451b9dfc922710ba301f89937b` |
| `internal/migration/evidence_replay_structural.go`           | `6abebc1cf0517eb711b6f54b4d6fe06b6f5c96cee23eaae93744ff23a85b6005` |
| `internal/migration/evidence_replay_structural_test.go`      | `eb8c839aefff7c0758113197d7f90ea01002736ec2209e808c3ac0967df4a600` |
| `internal/migration/evidence_runtime.go`                     | `07361616e7a1b868ba0ecd50952889857210aec98aa522d2ad323c8e4980406f` |
| `internal/migration/evidence_runtime_test.go`                | `4d77807af70e338ac3f13f7a7841c72179356a7d107d61dff5f12168b463b63c` |
| `internal/migration/evidence_session.go`                     | `53b29b23b48200afcbba708c6951574a0977d03fe06614026aaadfe26a63d5f9` |
| `internal/migration/evidence_session_test.go`                | `0a5d93998665eaad86e196565cf694409a01c6159398a68433dfb6172b61d415` |

Paths in this table are relative to `services/control-plane/`.

## Closed implementation boundary

### Filesystem durability and lock ownership

`AdmissionMutationToken.CreateGenerationHeader` now:

1. revalidates the active inventory, target lineage, quota/cardinality bounds, directory identities and current revision;
2. creates the deterministic journal directory and `fsync`s its lineage parent;
3. creates, validates and `fdatasync`s `writer.lock`, `fsync`s the journal directory, then nonblocking-acquires and retains the
   generation lock in the admission lease;
4. creates an empty segment-0, writes the exact opaque framed header bytes with a checked `pwrite` loop, validates the final
   file identity/size, then `fdatasync`s segment-0 and `fsync`s the journal directory;
5. performs terminal discovery/full-set validation and advances the immutable admission revision only after every durability
   barrier succeeds.

Pre-mutation failures mint no new authority. Any failure after the first namespace mutation is returned as `unknown`, consumes
the mutation token and revokes the admission lease. Cleanup attempts segment, generation lock, journal, lineage, lineages and
root descriptors independently; successful creation retains the generation lock and `AdmissionLease.Close` releases journal,
lineage and root locks in reverse ownership order. No failure path deletes or rewrites a partially durable journal.

### Closed migration authority graph

- `ReceiptBoundReady.AppendGenerationReserved` consumes the one-shot paired receipt authority and appends the exact preplanned
  brand-new `GenerationReserved` frame. The filesystem candidate SHA-256 and the C3 `record_digest` remain distinct facts.
- `ReservedDurablePermit.CreateGenerationHeader` reconstructs the exact planned `JournalHeader`, requires its C3 digest to equal
  `expected_segment_0_header_digest`, binds both typed content receipts to that header, and calls the concrete evidencefs
  transition while the admission lease is active.
- `HeaderDurablePermit.AppendGenerationActivated` constructs the next index frame with exact references to the reserved record,
  segment-0 header and initial journal tail, then appends it durably.
- Each successful transition returns a new registry-backed, anti-copy, one-shot authority and permanently consumes its
  predecessor. Literal, copied, stale, field-swapped and double-consumed values fail closed. Mutation/response uncertainty returns
  no successor authority.
- `GenerationReadyPermit` proves only the durable reserve/header/activate chain. It is intentionally not `ActiveGeneration`, does
  not expose `Connect`, `EvidenceJournal` or `JournalCursor`, and its only reviewed production consumer is the handoff transition.
- `AdmissionMutationToken.HandoffGeneration` consumes the exact current revision, invalidates the old inventory/admission lease,
  releases all other generation locks, all non-target lineage locks and the root-wide lock, and transfers only the exact target
  lineage + generation FD pair into a registry-sealed `evidencefs.GenerationLease`. Any unlock/close uncertainty cleans both
  retained locks, poisons the store and returns no lease.
- migration then binds that opaque lease to the exact activated C3 identities and returns `GenerationHandoffReady`. Both layers use
  immutable registry records, anti-copy checks and one-shot cleanup.
- `GenerationLease.Snapshot` records compact exact index/segment identities without retaining raw payloads. Every read reopens and
  rehashes the selected file under the retained locks; terminal `Revalidate` checks the complete index and segment set. Non-context
  uncertainty revokes the lease, close uncertainty poisons the store, and replacing or closing a snapshot invalidates its registry slot.
- `GenerationHandoffReady.Replay` accepts only the exact brand-new three-frame index and the exact one-segment/one-header journal,
  uses the lineage-plan-owned continuation/checkpoint streaming bridge, terminally revalidates the evidencefs snapshot, and seals
  `GenerationReplayReady`.
- `GenerationReplayReady.BindRecovery` consumes that replay once, reopens the exact header-only segment, verifies the immutable
  current same-verifier history facts and both purpose-typed publication receipts, reconstructs the closed recovery schema witness,
  creates a private brand-new cursor and `RecoverySnapshot`, terminally revalidates the filesystem snapshot, and seals
  `GenerationRecoveryReady`. The value exposes neither cursor nor snapshot; its sole reviewed production consumer is the journal
  binder. Failure and Close release the retained generation/lineage locks through immutable registry records even after the
  predecessor one-shot value has been consumed.
- `GenerationRecoveryReady.BindJournal` consumes that same-verifier recovery authority and seals the concrete
  `generationEvidenceJournal`. `Replay` revalidates the current complete snapshot before returning cursor/recovery clones;
  `AppendDurable` prepares and validates the exact EvidenceFrame plus `GenerationCheckpoint`, consumes the one-shot owned record,
  and invokes the retained-lease existing-segment composite append. Only a durable lower result mints the next cursor.
- The journal now copies both purpose-typed publication receipts out of the immutable handoff registry before consuming recovery
  authority, binds their exact registry identities into its own binding/registry/digest, and revalidates owner, digest, size,
  publication and same-store facts on every authority check. It no longer relies on a mutable predecessor value for receipt ownership.
- `GenerationRecoveryReady.BindSession` closes the concrete journal on any later seal failure, clone-owns the current candidate,
  and mints one copyable registry-revocable `ActiveGeneration` together with an anti-copy `EvidenceSession`. Session accessors use
  the fixed lock order session → journal; `RecoverySnapshot` clones current state instead of caching a stale cursor. `Close` revokes
  session/active registries before releasing journal locks even under cancellation. The successor method remains a non-consuming
  `MIGRATION_PROJECTION_NOT_IMPLEMENTED` boundary until full-root reacquisition is implemented.
- A lower pre-mutation result restores the previous byte state under a fresh cursor identity while returning no durable append
  result. A true unknown retains no live cursor and is classified on `Replay` as unchanged, journal torn, journal complete,
  checkpoint torn or composite complete. The upper layer then uses only the matching truncate/resync/checkpoint sequence and mints
  a known cursor/recovery state only after an exact replacement snapshot is durable. Any response-lost repair remains unknown and
  is reclassified from fresh bytes on the next replay; contradiction revokes the lease and closes through the immutable chain.
- The journal registry binds the exact predecessor capabilities, same-verifier schema facts, reservation, snapshot/file facts,
  cursor, recovery snapshot, usage counters, opaque filesystem result and prepared framed bytes. Literal/copy/paired-field mutation,
  stale cursor, wrong owner/generation and quota overflow fail closed. The unknown state stores compact previous/header/candidate
  recovery facts rather than retaining the full frame/witness graph.
- When the current segment cannot fit the caller composite, the journal prepares a sealed rotation header, its checkpoint, the
  caller frame and its checkpoint together. evidencefs executes create+directory sync, header, header checkpoint, caller and caller
  checkpoint barriers in that exact order; only the lower durable result installs the next-segment cursor. The migration layer
  binds the rotated header/caller counters and emits both rotation diagnostic digests without exposing a header cursor as caller authority.
- Rotation response loss is classified as `segment_absent`, `segment_empty`, `header_torn`, `header_complete`,
  `header_checkpoint_torn`, `header_composite_complete`, `caller_torn`, `caller_complete`, `caller_checkpoint_torn` or
  `composite_complete`. Empty/torn created tails can only be removed by the result-bound evidencefs discard transition, which
  unlinks that exact final segment and fsyncs its journal directory; any uncertainty remains unknown. Complete prefixes use only
  the matching resync/truncate/checkpoint sequence. Physical segment headers are transparent only to logical record adjacency;
  inserted business records remain rejected by the structural FSM.

## Local verification

The fixed source commit passed the following local gates from `services/control-plane`:

```bash
go test -count=1 ./internal/evidencefs ./internal/migration
go test -count=1 ./...
go test -race -count=1 ./internal/evidencefs ./internal/migration
go vet ./...
go build ./...

CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go test -exec=/usr/bin/true ./...
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build ./...
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go test -exec=/usr/bin/true ./...
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build ./...

git diff --check
```

Focused fault coverage includes pre-mutation preservation, post-mutation unknown/revocation, short and failed writes, each
sync/close boundary, retained-lock slot binding, reverse cleanup, exact inventory revision/full-set changes, receipt/object/store
identity swaps, canonical frame/digest mismatches, anti-copy/one-shot behavior and the absence of an unreviewed
runtime consumer, exact release order, root-lock reacquisition after handoff, immutable-registry tamper rejection and
cleanup through the original retained FDs. The retained journal additions cover exact composite preparation, caller-record
non-consumption on quota/rotation preflight, prepared/recovery/schema mutation closure, ordered durable-ledger refresh, fresh cursor
rebinding, every existing and rotated lower unknown byte classification, empty/torn segment discard, response-lost discard
reclassification, logical adjacency across physical headers and the standalone checkpoint/resync/truncate fault matrices. The
session addition covers literal/consumer-firewall rejection, immutable active-generation digest fields and artifact-byte clone ownership.
A positive cross-package migration-to-real-evidencefs journal/session integration remains unavailable until the trusted production constructor exists;
no exported fake constructor, unsafe bridge or weakened seal was added to manufacture that test.

## Explicitly open boundaries

- Linux production `evidencefs.Open`/trusted-mount authority remains fail closed before mutation; there is no positive production
  constructor or cross-package end-to-end admission test.
- Root-wide admission release and opaque target/generation lock transfer are locally implemented, but `GenerationHandoffReady`
  now advances through compact filesystem snapshot, strict brand-new replay and same-verifier recovery binding into
  a receipt-owned retained `EvidenceJournal` and sealed current `ActiveGeneration`/`EvidenceSession`; no public production sink is minted.
- Successor/continuation full-root reacquisition and adjacent index transition, `Connect`, runner and database wiring are not
  implemented by this slice. `NewEvidenceSink` therefore continues to reject before production I/O, and the session successor
  method returns stable `MIGRATION_PROJECTION_NOT_IMPLEMENTED` without consuming its authority.
- Successor `GenerationSuperseded → adjacent GenerationReserved` and process-restart reconstruction of opaque in-memory authorities
  remain separate incomplete paths. The implemented unknown reconciliation covers response-lost I/O while the retained lease and
  process-local journal capability are still alive; it is not a crash-reopen constructor.
- Real ext4/XFS mount identity, cross-process contention, process restart, power-loss ordering, Linux syscall execution, cloud
  deployment and fixed-host replay were not run. Darwin unit fakes and Linux cross-compilation cannot close those boundaries.
- No independent immutable closure record was signed. `G-DATA`, `G-AUTHORITY-P1`, `G-SECURITY-P1`, aggregate Gates and Platform
  RC therefore remain `IN PROGRESS` or blocked as recorded in the status tracker.

## Invalidation

This evidence must be refreshed if any fixed implementation file, admission/C3 contract, durability order, lock ownership,
unknown-outcome mapping, Go toolchain, module graph or production-consumer boundary changes. A later Gate closure must bind its own
exact committed source, runtime environment, trusted-mount proof and independent reviewer record; it must not promote this local
record in place.

## Push state

The remote recovered after three recorded `Internal Server Error` rejections (request IDs
`81750123b6d8a1f0b0558de37214a30b`, `36abdfa32685cbecb3c19520073d893d`, and
`28150b7e32b2ad721809a3d632e48720`). On 2026-08-14, the feature branch advanced through retained checkpoint append/heal,
resync, tail repair, the concrete journal binder, segment rotation, ten-state rotation reconciliation and current session sealing at
`5e0065a`; all listed
implementation commits are present on `origin/codex/cloud-agents-platform-p1`.
