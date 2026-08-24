# P1 recovered generation-prefix activation and handoff bridge — 2026-08-16

- Status：**IMPLEMENTATION EVIDENCE — PASS；Gate OPEN**
- Fixed source：`cdedda75c6e866de1a0466d60a4fa8792c1e0da3`
- Source tree：`adeda5cb9e26c009da04fa5ba7ab61a9d8e10d90`
- Prerequisite：`7b52509`（same-verifier generation-prefix reopen binder）
- Branch：`codex/cloud-agents-platform-p1`
- Evidence snapshot：`2026-08-16T10:04:32Z`
- Executor：Codex P1 implementation executor
- Independent Gate reviewer：**not assigned**

## 1. Scope

This slice consumes the sealed `RecoveredHeaderDurablePermit` produced by the generation-prefix
reopen binder, appends the byte-exact adjacent `GenerationActivated`, reruns ALL-history verification
on the new evidencefs revision and returns the existing `RegisteredGenerationHandoffPermit`.

It does not create a second handoff or session implementation. The returned permit enters the already
reviewed retained-lock `Handoff`, `RegisteredGenerationRecoveryReady` and `BindSession` chain. The new
activation method itself does not call `HandoffGeneration`, `Connect`, `Begin`, a runner or a database.

No C3 wire member, public authority constructor, trusted-mount bypass, receipt interface, SQL or cloud
call was added.

## 2. Retired source-history validation

Generation-header recovery durably advances the evidencefs admission revision and invalidates the old
`AdmissionInventory`. The previous post-header validator still required that old inventory to be
current, which made every real durable recovery result impossible to seal.

The validator is now split deliberately:

- `validConsumedGenerationPrefixHistory` continues to require the source inventory revision, target
  and full-set digest while minting the initial recovery permit; and
- `validRetiredGenerationPrefixHistory` keeps the exact immutable verifier owner, candidate binding,
  history canonical digest, registered generation, receipt registry entries, root quota facts and
  target lineage identity after evidencefs has retired that source revision.

Only `RecoveredHeaderDurablePermit` uses the retired variant. Its own post-header inventory, mutation
token, full-set digest, revision and durable transition result must still be current and exact. This
does not turn an old inventory into mutation authority.

## 3. Exact activation transition

`RecoveredHeaderDurablePermit.AppendGenerationActivated` rebuilds the activation frame only from the
stored durable `GenerationReserved` and exact recovered segment-0 header. Before consuming its CAS it:

1. revalidates the complete post-header inventory;
2. rereads the target index and requires the stored reservation tail, byte size, digest and file
   identity to remain exact;
3. rereads the target journal and requires exactly the recovered canonical segment-0 bytes;
4. requires the activation sequence and previous record digest to be byte-exact adjacent; and
5. revalidates the permit, same verifier, receipts, mutation token and registry records.

The transition performs exactly one `AdmissionMutationToken.AppendTargetIndex` call. A genuine context
cancellation or deadline reported as a pre-mutation failure may restore the CAS. Any other preflight
contradiction revokes the recovery graph. Once a mutation may have happened, an unproven result is
closed as `unknown`, returns no next authority and invalidates both the evidencefs result and all
migration-side source/fresh bindings.

Durable success additionally requires the evidencefs candidate digest/revisions to match the exact
activation bytes, a changed nonzero full-set digest, an exact index append and an unchanged one-header
journal.

## 4. Fresh ALL-history and existing handoff authority

After the durable activation, the old pass-one history is not reused. The implementation calls
`bindVerifiedAdmissionHistory` on the new inventory revision and requires:

- target state `active_initial` with the activation record as the exact index tail;
- the same generation identity, planned header, verifier decision, runner/schema bindings, runtime
  bundle, recovery artifact, quota policy and artifact sizes/digests;
- the fresh registered runtime and recovery publications to be the same evidencefs objects as the
  recovered receipts;
- a one-record segment-0 replay whose cursor is header-only, has no checkpoint and is bound to the new
  index revision; and
- a terminal full-inventory `Revalidate` after sealing the handoff permit.

Only the existing `bindRegisteredGenerationHandoff` may consume that fresh registered generation.
Successful sealing retires the old recovery history and receipt registries while preserving the fresh
history held by the returned handoff permit. Subsequent lock transfer, snapshot comparison, cursor
renewal and session binding remain the existing reviewed implementation rather than duplicated logic.

## 5. Exact source and artifacts

| Input                                        | Exact value                                                                |
| -------------------------------------------- | -------------------------------------------------------------------------- |
| Admission activation authority-spread test   | SHA-256 `e4ed12acd2a860cc4fcbeadc6e2c58c5f8b4841a15bca70c7b9e8f1c6071bad7` |
| Generation-prefix recovery source            | SHA-256 `9fa78d7fa84270bbbb114ddcd7dca78de0a4a0199962e1a33a5f63cc67c1c774` |
| Generation-prefix recovery tests             | SHA-256 `bc0ae609831171677b5da39afbde5d23245b1e12c15d96e5c8f92c0c396765a5` |
| Registered-generation handoff authority test | SHA-256 `3ae7cbac42e20b3048776efea48ebcdcd4da394bba0405cdb471e5e2de652a2b` |
| Registered-receipt authority-spread test     | SHA-256 `80c8a452a165701c98e72df07dee1a0f9928bfc6e50d8a44f03dfee427d0911e` |
| Generation-prefix activation source          | SHA-256 `0e1f3e5dd91c63c35fcdeb276b12b2b9dbe1c176b74051ae5b030dd9d7be99ed` |
| Generation-prefix activation tests           | SHA-256 `578250abca7e2773519bf11940363fd87f8b6825a56e0022a30dafc5110d2f33` |
| Linux/amd64 migration compile artifact       | SHA-256 `54ee3c89581999d9bb5d6c2f9c7acca5ed7c3dab957a854bb9c3633740634c5a` |
| Linux/arm64 migration compile artifact       | SHA-256 `42a69a4920b9cd634bf0f36efac857a6651d88d4b16a76dce47ec05a9d40739c` |

## 6. Gates

The fixed source passed the focused activation/prefix/static suite and this changed-boundary race run:

```sh
go test -race -count=1 ./internal/migration \
  -run 'Test(GenerationPrefix|RecoveredGenerationRegistrationFacts|GenerationReadyHasOnlyReviewedHandoffConsumer|RegisteredReceiptConsumersAreRecoveryOnly|RegisteredGenerationHandoffAuthorityDoesNotSpread)'
```

The race command completed in `8.271s`. The same bits also passed:

```sh
go test -count=1 -timeout=30m ./internal/migration
go vet ./...
go build ./...
go test -run '^$' ./...
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go test -c ./internal/migration
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go test -c ./internal/migration
git diff --check
bun scripts/secret-scan.ts
```

The complete migration suite took `1182.562s`. Its first run hit Go's default `10m` timeout while an
unrelated existing `TestRunnerPreledgerProjectionFaultsRollbackWithoutAppendingEvidenceOrLedger` was
still running; it reported no assertion failure. The exact same fixed source then passed with the
explicit `30m` budget. A full race suite was not rerun on this slice; the targeted race above covers
the changed activation/authority boundary.

Focused tests prove the exact adjacent activation frame, literal/zero authority rejection, immutable
registration comparisons, owned old-history/permit/receipt cleanup and the single reviewed handoff
bridge. Existing AST authority-spread gates were widened only for the two recovery-specific production
files.

There is still no fake positive cross-package constructor in migration tests. Production
`evidencefs.Open` remains fail closed until trusted-mount provisioning exists, while evidencefs test
authority is package-private. Exporting a test constructor, unsafe/linkname seam or mint-capable
interface would weaken the boundary under test. A genuine positive activation-to-session integration
must therefore use the eventual trusted production constructor.

## 7. Remaining boundary

This record does **not** provide:

- a trusted production mount provisioner or successful production `Open` path;
- a production required-syscall capability probe;
- a positive cross-package trusted-mount activation/handoff/session integration;
- remaining append/rotation repair/resync per-barrier virtual matrices;
- physical controller or host power-loss evidence;
- runner/DB `Connect`, SQL execution, deployment, independent immutable Gate review, Platform RC,
  Beta or GA.

`G-DATA`、`G-AUTHORITY-P1`、`G-SECURITY-P1` and the aggregate Gates remain `IN PROGRESS`.
