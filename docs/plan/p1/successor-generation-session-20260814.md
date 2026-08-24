# P1 successor generation session implementation evidence

- Status：**LOCAL IMPLEMENTATION VERIFIED — GATE OPEN**
- Scope：registered ancestor reopen、generation-lease → full-root admission reacquisition、same-verifier successor plan、
  current runtime/recovery publish+bind、atomic receipt pair、
  `GenerationSuperseded(A→B) → adjacent GenerationReserved(B) → segment-0 header → GenerationActivated(B)`、
  retained successor handoff/replay/recovery/journal，以及同一 `EvidenceSession` 指针上的 current-generation swap
- Fixed source commit：`cebacea7ce8b0864b00f116f60c2509164f4865d`
- Fixed implementation tree：`79ef2c5928c03769afc8955c29606e90bf5bc0b3`
- Branch：`codex/cloud-agents-platform-p1`
- Date：2026-08-14 Asia/Shanghai
- Toolchain：Go `1.26.5 darwin/arm64`
- Record type：implementation evidence；**不是** independent Gate closure record

本记录承接固定于 `5e0065a` 的
[brand-new admission generation evidence](admission-generation-durability-20260813.md)，只证明当前 feature branch
已把一个合法 ancestor-recovery session 的 live、in-process successor transition 闭合到新的 current
`ActiveGeneration`/`EvidenceSession`。它不证明 production trusted mount、process-crash 后
`superseded_pending_reservation` 重建、真实 ext4/XFS power-loss、runner/DB `Connect`、Platform RC 或任一 aggregate Gate。

## Fixed implementation commits

| Commit    | Slice                             | Result                                                                    |
| --------- | --------------------------------- | ------------------------------------------------------------------------- |
| `fd48990` | evidencefs admission reacquire    | old generation/lineage release followed by same-store full-root admission |
| `09b54bf` | registered publication recovery   | registration-bound final object authority                                 |
| `56f22c6` | registered receipt recovery       | purpose-typed runtime/recovery receipts for strict registered history     |
| `9acc6ef` | first-generation quota correction | exact initial index debit without double counting                         |
| `74618d4` | registered generation replay      | strict retained snapshot replay for an already registered generation      |
| `c6f55f7` | registered recovery handoff       | same-verifier registered generation recovery authority                    |
| `7be8d44` | registered generation journal     | registered provenance enters retained append/reconcile                    |
| `9fe5cf4` | registered generation session     | ancestor-recovery active session with exact recovery bindings             |
| `6ce3a5d` | successor admission plan          | exact A→B authority, continuation and planned frame bytes                 |
| `88528e0` | successor content authority       | publish→bind→publish→bind→reserve-ready→atomic receipt pair               |
| `fd7bb9c` | successor index pair              | durable Superseded followed only by byte-exact adjacent Reserved          |
| `7d17d99` | successor header and activation   | exact successor segment-0/header and GenerationActivated                  |
| `802b2c2` | successor retained handoff/replay | exact successor lease, strict index/header-only replay                    |
| `b511e4d` | successor recovery                | inherited continuation and current same-verifier recovery snapshot        |
| `bbea25e` | successor generation journal      | successor provenance enters the retained normal-run journal               |
| `cebacea` | successor evidence session        | irreversible reacquire orchestration and same-session current swap        |

All commits above are present on `origin/codex/cloud-agents-platform-p1`.

## Fixed file identities

Paths are relative to `services/control-plane/`.

| File                                                  | SHA-256                                                            |
| ----------------------------------------------------- | ------------------------------------------------------------------ |
| `internal/evidencefs/admission_handoff.go`            | `0a4d958637abe59bd180261402d3afe7e06b988585127109c2261be2cf03d4cd` |
| `internal/evidencefs/admission_handoff_test.go`       | `0b33120086022f846b1c78aa4357f2f30fbcfc09b6fbc07b069a7fdb72c1302a` |
| `internal/migration/evidence_successor_plan.go`       | `f923059f771561f99e2278b7d38cabcc5fb00abdd1a1ae8e1585c52f1a7f3ac5` |
| `internal/migration/evidence_successor_content.go`    | `0b8b3a0dba94a8d668dd6bc5d0180b178ecffb3d02164d1976f325b8709bc1a2` |
| `internal/migration/evidence_successor_index.go`      | `7d9e3a8246e2391f3bb4b033be67e414a8e3cc2ff997f8d6f7097bc00347ac91` |
| `internal/migration/evidence_successor_activation.go` | `c8d46b19629aef6541bb6a08d4432e0a8955d397ab8a0c669c68a427ffd2df1d` |
| `internal/migration/evidence_successor_handoff.go`    | `3fa67c2a081792a5e8726e22960ec04a8a141d16a37a87bf32a7d8128597e3ae` |
| `internal/migration/evidence_successor_recovery.go`   | `cf84d5d3aa0bc828929db1c95a0ccc76bd587a688b3e0812d84596be440ba744` |
| `internal/migration/evidence_generation_journal.go`   | `afc39cbc048f9a379e55a0f065aaae3a48fa19e555655764f0d1e7d036244220` |
| `internal/migration/evidence_session.go`              | `37d31f8234c07706bdf5e2930219e4eb9939fa464a0511c9995367af9777e398` |
| `internal/migration/evidence_session_test.go`         | `1ca35e7526004cba5b727b2aa1400e46767a0bd3063bcccf61bbc6abb3d72557` |

## Closed local authority boundary

1. `generationEvidenceSession.ReserveAndActivateSuccessor` accepts only a valid
   `activeGenerationAncestorRecovery`. Before releasing any lock it checks the one-shot supersession authority against the exact
   session owner, generation, replay tail, checkpoint-or-activation boundary, current verifier, historical policy and
   recovery-execution digest.
2. The irreversible point revokes the old session, active generation, journal and cursor first. It then calls the concrete
   `GenerationLease.ReacquireAdmission`; cancel, filesystem failure or any later transition failure leaves the old session closed.
   There is no rollback that revives the old cursor or lease.
3. The fresh inventory is replayed through `bindVerifiedAdmissionHistory`; the exact authority is consumed only by
   `bindVerifiedSuccessorAdmissionPlan`. The mutation chain is closed and ordered:

   ```text
   PublishRuntime → BindRuntime → PublishDecisionRecovery → BindDecisionRecovery
     → SealReserveReady → BindReceiptPair
     → AppendGenerationSuperseded → AppendGenerationReserved
     → CreateGenerationHeader → AppendGenerationActivated
     → Handoff → Replay → BindRecovery → BindJournal
   ```

4. `BindReceiptPair` mints both purpose-typed receipts together. A durable Superseded result exposes only
   `SuccessorAdjacentReserveReady`; no operation can intervene before the planned Reserved bytes. A owns only the Superseded debit;
   B owns Reserved, Activated and later checkpoint debits.
5. Handoff retains only the exact successor lineage/generation locks. Strict replay requires the final index suffix to be the
   planned Superseded/Reserved/Activated frames and the successor journal to remain exact header-only state. Recovery binds the
   stored continuation as `RecoveryBrandNewInherited` before a normal-run journal can exist.
6. Success installs the new journal and current `ActiveGeneration` into the same anti-copy session pointer. Old active/session/journal
   registries and cursors remain revoked. Failure cleanup closes the highest live filesystem authority once, walks every successor
   state predecessor, and removes plan/history/receipt registries without double-closing the old generation lease.

## Local verification

The fixed source commit passed from `services/control-plane`:

```bash
go test ./internal/migration -count=1
go test ./... -count=1
go test -race ./internal/migration -count=1
go vet ./...
go build ./...

GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go test -exec=/usr/bin/true ./internal/migration
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go test -exec=/usr/bin/true ./internal/migration

git diff --check
```

Focused coverage includes literal/copy/consumed authority rejection; owner/session/generation/tail/execution/checkpoint/terminal/
successor-schema swaps; exact transition ordering; registered-source cursor/history/receipt revocation; complete successor state-chain
registry cleanup; unchanged brand-new activation consumer firewall; and the reviewed session-only successor authority consumer.
The evidencefs package separately covers concrete `GenerationLease.ReacquireAdmission` release-before-reacquire, cancellation,
busy/error and cleanup behavior.

## Explicitly open boundaries

- `evidencefs.Open` and `NewEvidenceSink` remain production-rejecting because trusted mount authority is not implemented. There is
  therefore no safe cross-package positive constructor for a real migration session in unit tests; no exported fake constructor,
  unsafe bridge or weakened seal was added.
- This live transition does not yet reconstruct a crash-stored `superseded_pending_reservation` into opaque in-memory authority.
  Process restart still requires the separate registration-only historical supersession recovery path.
- Real Linux ext4/XFS mount identity, cross-process contention, process restart and power-loss ordering were not executed. Darwin
  unit fakes and Linux cross-compilation are not durability evidence.
- Runner/DB `Connect`, SQL, ledger, commit/reconcile wiring, production signature/trust-root wiring, cloud deployment and public
  release remain unimplemented or fail closed.
- No independent immutable closure record was signed. `G-DATA`, `G-AUTHORITY-P1`, `G-SECURITY-P1`, aggregate Gates and Platform RC
  remain `IN PROGRESS`/blocked.

## Invalidation

Refresh this evidence if any fixed implementation file, C3/admission contract, transition order, cleanup ownership, stable error
mapping, module graph or production-consumer boundary changes. A later Gate record must bind its own exact source, trusted-mount and
real-filesystem evidence, reviewer identity and commands; it must not promote this local record in place.
