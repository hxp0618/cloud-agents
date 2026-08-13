# P1 admission generation durability implementation evidence

- Status：**LOCAL IMPLEMENTATION VERIFIED — GATE OPEN**
- Scope：brand-new admission 的 receipt-bound reservation、generation journal/segment-0 创建、header durability、
  `GenerationActivated` durable append、root-wide lock release、retained generation snapshot 与 non-runnable strict replay
- Fixed source commit：`fa7f8e13ae5ecee3cdc723b8674ab1d8512be5df`（local；GitHub push 因 remote 500 暂未完成）
- Fixed implementation tree：`64f24a96668400c027a54d19496e9d3d3f454a73`（`fa7f8e1^{tree}`，不含本次证据文档更新）
- Branch：`codex/cloud-agents-platform-p1`
- Date：2026-08-13 Asia/Shanghai
- Toolchain：Go `1.26.5 darwin/arm64`
- Record type：implementation evidence；**不是** Gate closure record

本记录固定
`ReceiptBoundReady → ReservedDurablePermit → HeaderDurablePermit → GenerationReadyPermit → GenerationHandoffReady → GenerationReplayReady`
的本地实现证据。
它证明 brand-new generation 在同一 active admission epoch 与 retained lock chain 下，按
`GenerationReserved → exact segment-0 JournalHeader → GenerationActivated` 的顺序完成代码级 durability barrier。
它不证明 production trusted mount、正常运行交接、数据库连接、真实文件系统掉电恢复或 Platform RC。

## Fixed implementation commits

| Commit                                     | Slice                                | Result                                                                       |
| ------------------------------------------ | ------------------------------------ | ---------------------------------------------------------------------------- |
| `5a56caa7f515c7f4b2c5e9caf6bfd17ec2f00238` | evidencefs generation journal create | durable directory/lock/segment-0 create and retained generation lock         |
| `e5283693f116552ecefdb6ef626668ca0f931666` | migration generation reserve         | exact planned `GenerationReserved` append and sealed `ReservedDurablePermit` |
| `1ecbba8db051e5d1ee0d1a6dd66b810a9c6030cf` | migration generation header          | exact planned header create and sealed `HeaderDurablePermit`                 |
| `5896da7c6c4bc75055bfad7dc63db913bb5a9446` | migration generation activation      | exact `GenerationActivated` append and sealed `GenerationReadyPermit`        |
| `8bfe7c816811ab0a1885b65a53b7a621b66d1144` | evidencefs lock handoff              | release root/non-target locks and retain exact target/generation lock pair   |
| `c017c9573015d7e91099d71744459f9f7478594d` | migration lock handoff               | consume generation-ready and seal non-runnable `GenerationHandoffReady`      |
| `3b1534069ab58553a3176f47a9eac6eb11ec47c7` | retained generation snapshot         | compact exact index/segment facts with bounded re-read and terminal revalidate |
| `fa7f8e13ae5ecee3cdc723b8674ab1d8512be5df` | migration strict generation replay   | consume handoff and seal non-runnable `GenerationReplayReady`                |

Key fixed file identities:

| File                                                       | SHA-256                                                            |
| ---------------------------------------------------------- | ------------------------------------------------------------------ |
| `internal/evidencefs/admission_journal_create.go`          | `428008d074db6ec93c1b0b8b50f803f28b517d3af7aab88857d15f49d1f3f3f5` |
| `internal/evidencefs/admission_journal_create_test.go`     | `77f08741a5ebc6a811989a7d813610b3c379247858152490871517759aea5f66` |
| `internal/migration/evidence_admission_reserved.go`        | `370c1bb930713e8f7da9f9af9158ea0c0428ce50aa7fb796d46e31d57847b518` |
| `internal/migration/evidence_admission_reserved_test.go`   | `ea5d820bcb67125e36bcba5d2bfcf109c9eba7135c42be33b5f8c6cea56326b5` |
| `internal/migration/evidence_admission_header.go`          | `8da40eb19a3838dea7e39327d0c78ceae5fac326bb6d686c3e4943279cad649f` |
| `internal/migration/evidence_admission_header_test.go`     | `6300f021349d3fdbd17f1aa211d72332572e2def5b43a585e6279172321c0688` |
| `internal/migration/evidence_admission_activation.go`      | `981edd5e0af5046aec0796d371ff13535a22f974611af0e3310edb696a7a2b72` |
| `internal/migration/evidence_admission_activation_test.go` | `dbed639b6e3289542637e45a416b6eeff84cdd8905e7f4cfd7b8e271b77ec04f` |
| `internal/evidencefs/admission_handoff.go`                 | `67b99efddcba783d1702f7a87ab3e7203ba977632ef74df4ecce8a5f75645533` |
| `internal/evidencefs/admission_handoff_test.go`            | `5c8ffb744fe7a1db83c70c6a79ff9483b31bdf5501e24d66b2779396c1bcfa9e` |
| `internal/migration/evidence_admission_handoff.go`         | `1de1e9268cc11406e079301be5f504350174000e1b576fd506c013fd65b17e03` |
| `internal/migration/evidence_admission_handoff_test.go`    | `8e17d7ff611193a7e46d2c405537f1c7d2e1be2b51e198e456842226ac3153ec` |

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
  `GenerationReplayReady`. This value remains non-runnable and has no production consumer; static tests forbid `EvidenceJournal`,
  `JournalCursor`, `Connect`, `AppendDurable`, `Open` and `ActiveGeneration` seams.

## Local verification

The fixed source commit passed the following local gates from `services/control-plane` with `GOWORK=off` and
`GOFLAGS=-mod=readonly`:

```bash
go test -count=1 ./internal/evidencefs ./internal/migration
go test -count=1 ./...
go test -race -count=1 ./...
go vet ./...
go build ./...

CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go test -c ./internal/evidencefs
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go test -c ./internal/migration
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build ./...
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go test -c ./internal/evidencefs
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go test -c ./internal/migration
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build ./...

git diff --check
```

Focused fault coverage includes pre-mutation preservation, post-mutation unknown/revocation, short and failed writes, each
sync/close boundary, retained-lock slot binding, reverse cleanup, exact inventory revision/full-set changes, receipt/object/store
identity swaps, canonical frame/digest mismatches, anti-copy/one-shot behavior and the absence of an unreviewed
runtime consumer, exact release order, root-lock reacquisition after handoff, immutable-registry tamper rejection and
cleanup through the original retained FDs.

## Explicitly open boundaries

- Linux production `evidencefs.Open`/trusted-mount authority remains fail closed before mutation; there is no positive production
  constructor or cross-package end-to-end admission test.
- Root-wide admission release and opaque target/generation lock transfer are locally implemented, but `GenerationHandoffReady`
  now advances through compact filesystem snapshot and strict brand-new replay into `GenerationReplayReady`; it still deliberately
  stops before normal-run authority.
- Normal-run `EvidenceJournal`, `JournalCursor`, checkpoint append/heal, `ActiveGeneration`, `Connect`, runner and database wiring
  are not implemented by this slice.
- Successor `GenerationSuperseded → adjacent GenerationReserved` and crash-reopen recovery remain separate incomplete paths.
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

`3b15340` is present on `origin/codex/cloud-agents-platform-p1`. The strict replay commit `fa7f8e1` is committed locally and passed
all gates above, but two authenticated GitHub pushes were rejected by the remote with `Internal Server Error` (request IDs
`81750123b6d8a1f0b0558de37214a30b` and `36abdfa32685cbecb3c19520073d893d`). Retry after the remote recovers:

```bash
GIT_SSH_COMMAND='ssh -F /dev/null -p 443 -o HostName=ssh.github.com -o User=git -o IdentitiesOnly=yes -i /Users/huang/.ssh/id_ed25519' \
  git push origin codex/cloud-agents-platform-p1
```
