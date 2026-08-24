# P1 migration production `EvidenceSink` — 2026-08-17

- Status：**IMPLEMENTATION + REAL EXT4/XFS PRODUCTION COMPOSITION — PASS；Gates OPEN**
- Fixed implementation commit：`3fe05ec22cc7caf7b08d5f0a4095da2d675b5f50`
- Fixed base commit：`dfa00cbd06bb19427940cd39891836954bbeb54d`
- Fixed source tree：`b0ef17f9b16bed292e61daf8056f87b668bf1296`
- Branch：`codex/cloud-agents-platform-p1`
- Evidence date：2026-08-17 Asia/Shanghai
- Executor：Codex P1 implementation executor
- Independent Gate reviewer：**not assigned**

## 1. Closed scope

This slice adds the public production composition root that was deliberately absent from the earlier
filesystem records:

1. `NewEvidenceSink(rootPath)` accepts only a canonical absolute non-root locator and stores no
   filesystem authority;
2. every `EvidenceSink.Open` reconstructs the exact current owned candidate once, then obtains a fresh
   production `evidencefs.OpenStore` and `AcquireAdmission`; a revoked mount claim therefore rejects a
   later open even when the sink value remains alive;
3. the full-root admission replay selects exactly one closed path for an absent/empty lineage,
   reserved no-header/header-unactivated prefix, registered active generation, or historical
   superseded-pending reservation;
4. the brand-new path consumes the complete one-shot chain
   `plan → permit → target registration → runtime publish/bind → recovery publish/bind → receipt pair →`
   `GenerationReserved → header → GenerationActivated → handoff → replay → recovery → EvidenceSession`;
5. registered, prefix-recovery and historical-successor paths reuse their already reviewed sealed
   handoff/replay/recovery/session authorities without a migration-side fake store, inventory, token,
   publication, path descriptor or foreign file descriptor;
6. success is a closed XOR: only `err == nil`, non-null session and non-null recovery snapshot transfer
   ownership. Every other result independently closes filesystem locks/descriptors before revoking the
   exact migration registries, and cleanup uncertainty dominates the original error.

This is production constructor and cross-package composition evidence. It is not runner configuration,
database authority or a release Gate.

## 2. Full-root lock and identity boundary

`evidencefs.Store.AcquireAdmission` now retains the complete registered writer set rather than only the
root and lineage locks:

`root lineages.lock → every lineage writer.lock in lineage-digest order → every discovered generation writer.lock in (lineage digest, journal digest) order`.

All acquisitions are nonblocking. Busy, hard-open error, context cancellation, post-lock discovery drift
or cleanup failure releases generation locks in reverse order, then lineage locks in reverse order, then
the root lease. Busy/drift retries restart from fresh root discovery with bounded context-aware backoff;
hard errors do not become retries. Any unlock/close uncertainty poisons the store and cannot mint an
inventory or later authority.

The final inventory proves the generation-lock set still equals terminal discovery. Registered prefix
recovery reuses the already retained generation lock, so it never performs a second `flock` on the same
writer file. `AdmissionFileView.GenerationIdentityDigest()` supplies a dedicated, non-authoritative
cross-view identity for exact comparison with the later `GenerationSnapshot`; the stronger inventory
identity remains bound to the full-root graph. Migration replay retains both digests and uses only the
handoff domain for the reviewed `GenerationFileFact` bridge.

## 3. Authority and fault firewall

The production sink is the sole composition root and accepts only `VerifiedEvidenceRun` plus
`VerifiedRuntimeArtifact`. It never accepts caller-supplied store/lease/inventory/token/replay facts.
Static AST tests pin every reviewed authority-bearing identifier in `evidence_sink.go` to an exact use
count; the file is not broadly exempted. The fault suites additionally cover:

- generation lock busy at first/middle/last position, hard try error, cancellation after a prior lock,
  cleanup close failure and post-lock identity drift;
- zero/copy/closed/cross-view generation identity rejection and exact post-handoff identity equality;
- existing one-segment prefix recovery without duplicate generation-lock acquisition;
- `finishEvidenceSessionBind` rejecting and closing any non-null-session/error combination;
- registry revocation and descriptor cleanup on every uncommitted chain stage.

No `unsafe`, `go:linkname`, exported test constructor, migration-side authority seal or
`evidencefs → migration` import was added.

## 4. Fixed implementation scope

The implementation commit changes exactly 23 files: `1484` insertions and `66` deletions. Key source
identities are:

| File                                                                                     | SHA-256                                                            |
| ---------------------------------------------------------------------------------------- | ------------------------------------------------------------------ |
| `services/control-plane/internal/evidencefs/admission.go`                                | `9341a650e6d1911b8073fd6edfda92e318afa85207ccf12656e2d38db187dda1` |
| `services/control-plane/internal/evidencefs/generation_header_recover.go`                | `abd9ab8bb91ee72e87b1f5caaafb703959d1f567757cc61b595b56e964750c52` |
| `services/control-plane/internal/evidencefs/inventory.go`                                | `c9d9cac96cfa817ab5ba312f9479752c756cd200ea05396ea16eda5ac39bcbd4` |
| `services/control-plane/internal/migration/evidence_admission_replay.go`                 | `68bd2a7e9705845a700d7ab099c196187adfd83a9988cb7a1aa88687bae92172` |
| `services/control-plane/internal/migration/evidence_sink.go`                             | `a02fd45281f90508f194877a8a040fc8c15bb4bc9e25efa1f3f8a51e5bfd65bb` |
| `services/control-plane/internal/migration/evidence_sink_linux_integration_test.go`      | `b3431ae4f1f07fa7296cd0c2b52afd3eda22099f8f0e85dd9b201f0cf26e2abb` |
| `services/control-plane/scripts/test-migration-evidence-sink-linux-filesystem-matrix.sh` | `051c848fa19bb45fcf518b486ba59477b18d933726499eb239d1481484c9f2be` |

The complete 23-file list is mechanically recoverable with
`git show --format= --name-only 3fe05ec22cc7caf7b08d5f0a4095da2d675b5f50`.

## 5. Deterministic local gates

The fixed source used Go `1.26.6 darwin/arm64` and Bun `1.3.14`.

| Gate                                                                                  | Result                                  |
| ------------------------------------------------------------------------------------- | --------------------------------------- |
| `GOWORK=off GOFLAGS=-mod=readonly go test ./internal/evidencefs -count=1`             | PASS (`4.654s`; module replay `4.448s`) |
| `GOWORK=off GOFLAGS=-mod=readonly go test ./internal/migration -count=1 -timeout=20m` | PASS (`1015.402s`)                      |
| `GOWORK=off GOFLAGS=-mod=readonly go test ./... -timeout=25m`                         | PASS (migration `1011.696s`)            |
| `GOWORK=off GOFLAGS=-mod=readonly go test -race ./internal/evidencefs -count=1`       | PASS (`6.707s`)                         |
| targeted migration authority/session race suite                                       | PASS (`8.894s`)                         |
| `go vet ./...` and `go build ./...`                                                   | PASS                                    |
| Linux amd64/arm64 tagged migration and evidencefs test compile                        | PASS                                    |
| matrix script `bash -n` and `shellcheck`                                              | PASS                                    |
| static authority/import scan and `git diff --check`                                   | PASS                                    |

The full `go test -race ./internal/migration` attempt is intentionally **not** recorded as PASS. The
test framework reached its `45m0s` timeout while executing the existing
`TestRunnerFinalIntermediateFaultsRollbackWithoutLedgerOrCommit/bind-invalid-record` case, with no race
report. The focused race suite above covers the changed sink/admission/handoff/replay/session boundary;
the 45-minute timeout remains an explicit non-green observation rather than being relabelled as a code
failure or success.

## 6. Real ext4/XFS production composition

The final public API was replayed with:

```bash
cd services/control-plane
CLOUD_AGENTS_EVIDENCEFS_HARNESS_IMAGE='alpine@sha256:14358309a308569c32bdc37e2e0e9694be33a9d99e68afb0f5ff33cc1f695dce' \
  ./scripts/test-migration-evidence-sink-linux-filesystem-matrix.sh
```

The script never pulls implicitly. It builds the tagged Linux migration test and production provisioner,
creates a fresh loop-backed ext4 or XFS filesystem, provisions the root-owned mount capability, and runs
the public sink as non-root UID 1001. The first `Open` exercises the full brand-new durability chain; the
second fresh `Open` exercises registered-generation replay and handoff. It then revokes the capability
and requires a third fresh `Open` to return the stable journal-failed result with no session or snapshot.
Both filesystems unmount cleanly and the script rejects residual owned containers.

| Evidence                     | Exact value                                                                      |
| ---------------------------- | -------------------------------------------------------------------------------- |
| Harness image                | `alpine@sha256:14358309a308569c32bdc37e2e0e9694be33a9d99e68afb0f5ff33cc1f695dce` |
| Local image ID               | `sha256:2c15e55df5d63efb31b629a557df305130612a16feb029c93447e54dda2c4189`        |
| Linux kernel                 | `7.0.14-orbstack-00380-ga7e0a2dc9535`                                            |
| Runner                       | UID `1001`, non-root                                                             |
| Migration integration binary | `9b09afeee52731b7a1423bbc4dd44a9d8af2357c70b58490fbb471a9951c140a`               |
| Provision binary             | `1ce30134cba3105f814701c08a10f832ab5554975e7d97c777ea6737e93d7da8`               |
| ext4                         | brand-new PASS；registered reopen PASS；revoked reopen rejected                  |
| XFS                          | brand-new PASS；registered reopen PASS；revoked reopen rejected                  |

This supersedes only the old runtime-status statements that said production `Open` and
`NewEvidenceSink` always reject. Historical dependency reviews remain fixed-source records; their
dependency, license, NOTICE and provenance conclusions are not rewritten by this implementation.

## 7. Explicit non-claims and next boundary

This record is implementation and runtime evidence, not an independently signed immutable Gate record.
It does **not** prove or authorize:

- runner configuration/CLI selection of this sink, runner phase/order integration or DB `Connect`;
- any database read/write, SQL, ledger, transaction or commit behavior through this sink;
- physical controller/host power-loss durability for the complete production-opened migration session;
- PostgreSQL/cloud/provider integration, packaging, deployment, Platform RC, Beta, GA or release;
- final binary/distribution SBOM and immutable supply-chain closure.

The next implementation boundary is runner/CLI configuration and pre-DB phase wiring, followed by the
database integration slice. `G-DATA`, `G-AUTHORITY-P1`, `G-SECURITY-P1`, `G-SUPPLY-CHAIN` and every
aggregate Gate remain `IN PROGRESS`.
