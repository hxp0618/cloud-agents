# P1 admission generation durability implementation evidence

- Status：**LOCAL IMPLEMENTATION VERIFIED — GATE OPEN**
- Scope：brand-new admission 的 receipt-bound reservation、generation journal/segment-0 创建、header durability 与
  `GenerationActivated` durable append
- Fixed source commit：`5896da7c6c4bc75055bfad7dc63db913bb5a9446`
- Fixed source tree：`4fd3c0fcc0940049f661d51a511c2e1136e4401e`
- Branch：`codex/cloud-agents-platform-p1`
- Date：2026-08-13 Asia/Shanghai
- Toolchain：Go `1.26.5 darwin/arm64`
- Record type：implementation evidence；**不是** Gate closure record

本记录固定 `ReceiptBoundReady → ReservedDurablePermit → HeaderDurablePermit → GenerationReadyPermit` 的本地实现证据。
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
  not expose `Connect`, `EvidenceJournal` or `JournalCursor`, cannot release the root lock, and has no production consumer. A
  static regression test preserves this boundary.

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
`GenerationReadyPermit` consumer.

## Explicitly open boundaries

- Linux production `evidencefs.Open`/trusted-mount authority remains fail closed before mutation; there is no positive production
  constructor or cross-package end-to-end admission test.
- `GenerationReadyPermit` still holds the full-root admission critical section. The transition that releases root-wide and
  non-target lineage locks while retaining the target lineage/generation writer authority is not implemented.
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
