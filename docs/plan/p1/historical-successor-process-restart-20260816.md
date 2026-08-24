# P1 historical successor process-restart implementation evidence

- Status：**LOCAL IMPLEMENTATION VERIFIED — GATE OPEN**
- Scope：strict registered-history replay of a crash-reopened historical header-only generation B；same-verifier
  `activated_no_migration_progress` authority；generation-lease release and full-root reacquisition；B → current C
  content publication, receipt pair, adjacent supersession/reservation, activation, retained replay/recovery/journal and
  current `EvidenceSession`
- Fixed source commit：`f654aae42717627cdc7a3dbf714882353dcb3dca`
- Branch：`codex/cloud-agents-platform-p1`
- Date：2026-08-16 Asia/Shanghai
- Local toolchain：Go `1.26.5 darwin/arm64`
- Linux replay：Linux `6.1.0-15-amd64` / `amd64` / `ext4` root；fixed cross-compiled test binaries
- Record type：implementation evidence；**不是** independent Gate closure record

本记录承接固定于 `cebacea` 的
[live successor session evidence](successor-generation-session-20260814.md)。它只证明当前 feature branch 已把
strict replay 形成的 historical B recovery authority 接到 production `BindSession` 入口，并在所有 opaque authority
仍有效时完成 B → C。它不证明 production trusted mount、真实进程重启构造、`ext4`/`xfs` mutation、断电恢复、
runner/DB `Connect`、Platform RC 或任何 aggregate Gate。

## Fixed implementation commits

| Commit    | Slice                                     | Result                                                                                  |
| --------- | ----------------------------------------- | --------------------------------------------------------------------------------------- |
| `cdaa631` | historical successor supersession binding | exact header-only B and same-verifier recovery facts mint one-shot B → C authority      |
| `b64d4d0` | admission reacquisition                   | old generation lease is released before the same Store reacquires all-root locks        |
| `eced5df` | replay-bound successor plan               | fresh ALL-history replay proves B remains exact before the existing planner consumes it |
| `e98263d` | prepared successor permit                 | current inventory mutation token and existing prepared permit are cross-bound           |
| `fabbafd` | durable successor materialization         | publish/bind/receipts/supersede/reserve/header/activate closed graph reaches current C  |
| `f654aae` | production session wiring                 | historical `BindSession` consumes the private chain through handoff/replay/journal      |

All commits above are present on `origin/codex/cloud-agents-platform-p1`.

## Fixed file identities

Paths are relative to `services/control-plane/`.

| File                                                                   | SHA-256                                                            |
| ---------------------------------------------------------------------- | ------------------------------------------------------------------ |
| `internal/migration/evidence_historical_supersession_recovery.go`      | `3edbac38571c34d2a0b46bb7c5e84e0c1b2c20781daa254bdacbdd0ec16c32aa` |
| `internal/migration/evidence_historical_supersession_recovery_test.go` | `13cad189eb4783952eb85f867b26aabe3532c4f936f91a530e195b12cc181b1e` |
| `internal/migration/evidence_session.go`                               | `5e7317a37b6bab11e46071ce87ad07af643e5635fed66c08eeea3ab88bb139da` |

## Closed local authority boundary

1. `HistoricalSuccessorGenerationRecoveryReady.BindSession` first validates the immutable historical recovery registry.
   A current B keeps the existing `BindJournal → bindGenerationEvidenceSession` path. Only `requiresSupersession=true`
   enters the private restart bridge.
2. The private bridge reconstructs B's exact `activated_no_migration_progress` supersession authority from B's stored
   activation/tail/continuation. It does not infer continuation or accept caller DTOs.
3. `GenerationLease.ReacquireAdmission` is one-way. It invalidates the old generation lease before full-root acquisition,
   including when the supplied context is already canceled; no error path revives B's cursor/session.
4. Fresh ALL-history replay must still identify B as the exact header-only registered generation before the existing
   successor planner and prepared permit can be consumed.
5. The durable graph is closed and ordered:

   ```text
   PublishRuntime → BindRuntime → PublishDecisionRecovery → BindDecisionRecovery
     → SealReserveReady → BindReceiptPair
     → AppendGenerationSuperseded → AppendGenerationReserved
     → CreateGenerationHeader → AppendGenerationActivated
     → Handoff → Replay → BindRecovery → BindJournal
     → bindGenerationEvidenceSession
   ```

6. Every filesystem transition must return `Durable` plus its exact next concrete authority. Any other result closes the
   deepest live admission/generation/journal owner, invalidates cursors and walks the successor state/receipt/plan/history
   registry chain. Success deletes the restart-only owner and returns only the normal current `EvidenceSession`.
7. Static consumer firewalls keep the historical recovery file out of runtime journal calls. The sole production bridge
   lives in `evidence_session.go`; no intermediate reacquired admission, mutation token or retained-lock authority escapes.

## Local verification

The fixed source passed from `services/control-plane`:

```bash
go test ./internal/migration -run \
  'Test(HistoricalSuccessor|RetireHistoricalSuccessor|SuccessorAdmission|SuccessorContent|SuccessorPlan|SuccessorGeneration|EvidenceSessionSuccessor)' \
  -count=1
go test -race ./internal/migration -run \
  'Test(HistoricalSuccessor|RetireHistoricalSuccessor|SuccessorAdmission|SuccessorContent|SuccessorPlan|SuccessorGeneration|EvidenceSessionSuccessor)' \
  -count=1
go test ./internal/migration -run \
  'Test.*(DoesNotSpread|HasOnlyReviewedConsumers|HaveOnlyReviewedConsumers|OnlyReviewed)' \
  -count=1
go vet ./...
go build ./...
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go test -run '^$' -exec=/usr/bin/true ./internal/migration
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go test -run '^$' -exec=/usr/bin/true ./internal/migration
go test ./internal/migration -count=1 -timeout=25m
git diff --check
```

The final full migration run passed in `1028.147s`.

## Linux replay

The exact fixed source was cross-compiled into Linux/amd64 test binaries and replayed on an authorized Linux
`6.1.0-15-amd64` host whose root filesystem reports `ext4`.

| Binary                    | SHA-256                                                            |
| ------------------------- | ------------------------------------------------------------------ |
| `evidencefs-f654aae.test` | `a2f2d2cdd0a8349384dcad7684970e57aa0a0c72567192ca3a7d1695453d9f76` |
| `migration-f654aae.test`  | `e16633e8aa6ceeffee1a2d8e1c2293bb8d15a80593e1397c720049183c7f8479` |

The corrected replay ran from an extracted fixed source/fixture tree and passed:

- evidencefs Linux open-flag, ENOENT, mount-crossing and private-mode tests;
- production `evidencefs.Open` fail-closed test;
- Linux mountinfo allow/reject matrix and production mount-authority fail-closed test;
- all historical successor, successor plan/content/generation and session focused tests listed above.

An initial raw-binary invocation from `/tmp` was invalid because source-reading tests could not locate `migrations/` and
package `.go` files. It is not counted as a product failure or a passing gate. The corrected run supplied the fixed fixture/source
context, passed, and all uploaded test artifacts were then deleted.

## Explicitly open boundaries

- `evidencefs.Open`, `newProductionEvidenceFSRoot` and `NewEvidenceSink` remain production-rejecting. The repository has no
  trusted provisioner capable of minting the non-forgeable mount authority required by ADR-0010.
- The Linux replay verifies linked Linux mechanics and fail-closed behavior only. It did not let migration code mutate the
  host's `ext4` filesystem and is not a positive production constructor or process-restart test.
- No `xfs`, cross-process contention, required-syscall probe, process kill/restart, controller reset or power-loss harness ran.
- No exported fake constructor, test backdoor, `unsafe` bridge or mountinfo-derived self-authorization was added.
- Runner/DB `Connect`, SQL/ledger execution, production trust-root/signature wiring, cloud deployment and public release remain open.
- No independent reviewer signed an immutable closure record. `G-DATA`, `G-AUTHORITY-P1`, `G-SECURITY-P1`, aggregate Gates and
  Platform RC remain `IN PROGRESS`/blocked.

## Invalidation

Refresh this evidence if any fixed implementation file, historical recovery state, successor transition order, cleanup ownership,
stable error mapping, module graph or production-consumer boundary changes. A future trusted-mount/real-filesystem Gate record must
bind its own provisioner identity, mount/power-loss harness, source SHA, environment, reviewer and replay commands; it must not
promote this local record in place.
