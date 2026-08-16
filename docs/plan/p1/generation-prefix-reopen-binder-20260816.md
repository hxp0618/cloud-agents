# P1 migration-owned generation-prefix reopen binder — 2026-08-16

- Status：**IMPLEMENTATION EVIDENCE — PASS；Gate OPEN**
- Fixed source：`7b52509284bad973805a699b16b97015e2693a21`
- Source tree：`bdf6b815bb6ed7d34d7d9d085aab154e306dc630`
- Prerequisite commit：`70269e1`（ordinary pass-1 prefix transcript）
- Binder commit：`7b52509`（one-shot migration/evidencefs composite authority）
- Branch：`codex/cloud-agents-platform-p1`
- Evidence snapshot：`2026-08-16T08:52:16Z`
- Executor：Codex P1 implementation executor
- Independent Gate reviewer：**not assigned**

## 1. Scope

This slice closes the migration-side authority gap left by
[`evidencefs-qemu-generation-header-barrier-matrix-20260816.md`](evidencefs-qemu-generation-header-barrier-matrix-20260816.md).
The QEMU record proves the package-private physical create/recovery barriers; this record proves that
migration can no longer call those transitions from a filesystem prefix or planned-header digest
alone.

The implementation has two deliberately separate layers:

1. ordinary ALL-history pass-1 consumes and structurally binds incomplete generation-prefix views; and
2. a migration-owned one-shot permit cross-binds that fresh physical state to same-verifier historical
   recovery, the exact durable `GenerationReserved`, registered object receipts and the current
   evidencefs mutation token.

No C3 wire member, database schema, public constructor, mount bypass, runner, SQL or cloud call was
added. `RecoveredHeaderDurablePermit` is the terminal authority of this slice and has no activation,
handoff, cursor, session or DB consumer.

## 2. Ordinary pass-1 prefix transcript

`AdmissionLineageView.GenerationRegistrations()` is now consumed for every registered lineage. Each
fact must retain its evidencefs self/seal/epoch/revision binding and must report the same lineage,
journal and inventory full-set digest. The only admitted metadata states are:

- `generation_prefix_directory`; and
- `generation_prefix_lock`.

A one-segment journal can instead enter the same ordinary transcript only when all of the following
are true:

- its journal identity is the final durable `GenerationReserved` in that lineage;
- that reservation has no durable `GenerationActivated` or `GenerationSuperseded`;
- the journal has exactly segment ordinal zero;
- the segment bytes are shorter than, and an exact prefix of, the canonical framed
  `PlannedSegment0Header`; and
- size, content digest and file identity still match the sealed evidencefs view.

The pass-1 transcript domain advances to
`cloud-agents-platform-evidence-admission-replay/v3`. It includes prefix state, journal identity and,
for a segment prefix, exact size/content/file-identity facts. Clone logic deep-owns the segment fact.
Prefix directories count as physical journals; segment-prefix bytes count in root physical journal
usage; verified whole-generation reservation accounting remains derived from the recovered runtime
bundle rather than caller counters.

Only one prefix may exist, and only for the final unactivated reservation. Unknown state, duplicate
identity, foreign journal, active-generation prefix, prefix without a durable reservation, simultaneous
complete header and prefix, non-prefix bytes or extra segments remains
`MIGRATION_EVIDENCE_JOURNAL_CORRUPT`. A complete exact header remains the existing
`reserved_header_unactivated` state. Directory/lock/zero/torn states remain logically
`reserved_no_header`; no prefix becomes receipt or mutation authority.

## 3. Same-verifier composite binder

`bindGenerationPrefixRecoveryPermit` accepts only a current sealed `VerifiedAdmissionHistory` whose
target is `reserved_no_header` or `reserved_header_unactivated`, whose target generation has no active
replay cursor and whose one-shot generation authority is unconsumed.

Before minting the permit it:

1. revalidates the complete inventory;
2. rereads and strictly decodes the target index;
3. requires the exact index tail to be the stored `GenerationReserved` already bound into history;
4. compares its full planned header, lineage, journal, decision, schema and record digest with the
   same-verifier recovered target-generation descriptor;
5. requires the registered runtime and decision-recovery receipts to remain valid and in the same
   evidencefs store;
6. reconstructs byte-exact canonical segment-0 bytes from that reservation;
7. classifies the current physical target as absent, directory, lock, strict segment prefix or exact
   complete segment; and
8. revalidates the inventory again before consuming the generation one-shot and acquiring one fresh
   evidencefs mutation token.

The permit owns a domain-separated canonical input covering target/full-set/revision, target-index
content and identity, exact reserved/header frames and bytes, registered generation identity, journal,
physical state and any segment prefix size/digest/identity. Its registry record also pins the history,
generation, candidate, token, receipt bindings and all canonical digests. Literal, value copy, field
mutation, stale inventory, reused generation, target/journal/header swap or token substitution rejects.

## 4. Closed recovery transition

`GenerationPrefixRecoveryPermit.RecoverGenerationHeader` rechecks the entire bound input immediately
before its CAS. Physical absence uses evidencefs `CreateGenerationHeader`; every observed prefix or
complete header uses `RecoverGenerationHeader`, which replays the existing directory/lock/segment
durability barriers.

Only context cancellation or deadline before any mutation preserves the one-shot permit. Any other
preflight contradiction revokes the migration permit and registered receipts. Once mutation/durability
is attempted, an unproven result is `unknown`, carries no next authority and revokes the recovery
chain. A concurrent CAS loser returns consumed without revoking the winner.

Durable success additionally requires:

- evidencefs candidate kind, journal, header digest/size and revision transition to match exactly;
- the next inventory to be current, target-equal and full-set changed;
- the target index to remain byte/digest/file-identity exact at the stored reservation tail;
- the current journal to contain exactly one byte-exact planned segment-0 and no remaining prefix fact;
- the registered receipt pair to remain valid and same-store; and
- a second terminal inventory revalidation plus a fresh next mutation token.

Only then is `RecoveredHeaderDurablePermit` sealed. This record does not claim its future
`GenerationActivated` append or generation-lock handoff.

## 5. Exact source and artifacts

| Input                                  | Exact value                                                                |
| -------------------------------------- | -------------------------------------------------------------------------- |
| Historical registered-header helper    | SHA-256 `0b8961ffd7ea0b208141e5b00972863b576f2048fe68183db8253287be87f073` |
| Admission replay source                | SHA-256 `4a1ea88dc3270e080bb57434ce53563981cd59c7dd8067f4073e8410386d221d` |
| Admission replay tests                 | SHA-256 `8fad793eb46e72830bbe80841b956a1c22a58478caae7c7be7bf235794af9714` |
| Generation-prefix recovery source      | SHA-256 `23ae924c9a2d65b4b5396a79829744adb46fc60f63af95933803b0e347c8831c` |
| Generation-prefix recovery tests       | SHA-256 `7823dc17f2a3d9945ae04994e181464e8e0c021d513c5f3992d3af43e52d06d9` |
| Linux/amd64 migration compile artifact | SHA-256 `c9f6ad86c78e215537e8c9286587c310350f0d7732cd9d550ca7e4a15a8bd27a` |
| Linux/arm64 migration compile artifact | SHA-256 `745196dea5eca80571c1af4d0005a8b06dc0bb82c6b24a7dbadbce51b43ac4fb` |

The physical evidencefs implementation and its isolated ext4/xfs QEMU barrier artifacts remain fixed
by `f650fae` and are not regenerated or silently inherited as migration end-to-end coverage.

## 6. Gates

The fixed implementation passed:

```sh
go test -count=1 ./internal/migration
go test -race -count=1 ./internal/migration
go test -race -count=1 ./internal/migration \
  -run 'Test(GenerationPrefix|AdmissionRecoverableGenerationPrefix|AdmissionGenerationPrefix|AdmissionStrictLineageState|AdmissionTranscriptRecomputesRoot)'
go vet ./...
go build ./...
go test -run '^$' ./...
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go test -c ./internal/migration
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go test -c ./internal/migration
git diff --check
bun scripts/secret-scan.ts
```

Focused tests cover metadata and segment prefix classification, final-reservation ownership,
active/orphan/duplicate rejection, transcript canonical mutation and deep copy, root journal accounting,
closed physical-state shape, input/permit canonical mutation, zero/literal/copy rejection and the static
absence of activation/handoff/DB consumers. The existing complete migration suite and race suite also
passed with cache disabled.

There is intentionally no fake positive cross-package constructor in migration tests. Production
`evidencefs.Open` remains fail closed until trusted-mount provisioning exists, while evidencefs test
authority is package-private. Exporting a test authority, unsafe/linkname seam or interface capable of
minting opaque inventories would weaken the boundary being tested. Positive physical transition
coverage therefore remains in the evidencefs-owned `f650fae` unit/QEMU matrix; a true cross-package
positive must use the eventual trusted production constructor.

## 7. Remaining boundary

This record does **not** provide:

- a consumer that appends the exact adjacent `GenerationActivated` from
  `RecoveredHeaderDurablePermit`;
- retained-lock handoff into a fresh replay cursor, recovery snapshot or `EvidenceSession`;
- a trusted production mount provisioner or production `Open` success path;
- a positive cross-package trusted-mount integration test;
- remaining append/rotation repair/resync barriers or physical controller/host-power evidence;
- runner/DB `Connect`, SQL, deployment, independent immutable Gate review, Platform RC, Beta or GA.

`G-DATA`、`G-AUTHORITY-P1`、`G-SECURITY-P1` and the aggregate Gates remain `IN PROGRESS`.
