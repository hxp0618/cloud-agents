# P1 local logical data recovery independent review — 2026-08-23

## Verdict

`APPROVE — P0=0 / P1=0 / P2=0`

This is an independent, read-only review of the superseding fixed candidate. It approves the candidate as bounded
local implementation evidence only. It does not close `G-DATA` or any aggregate Gate and does not authorize a merge,
production database write, remote host access, HTTP/P2/provider effect, deployment, publication or release.

## Fixed identity

- Candidate branch: `codex/cloud-agents-p1-data-recovery-local-20260823`
- Candidate commit: `282d4cd3f8a6783eb1dceb1c8cfecf3e4b7a367a`
- Candidate tree: `749ded0a93060c98a41e059df191afd1fa1acf36`
- Cleanup remediation commit: `4e8c8f800c0f905e3a0bdd0cc9e2f2c3ef5c990c`
- Remediated code tree: `dac6d08581ed38db7dc7d7e3103131d99ac6ee6a`
- `services/control-plane` subtree: `d6df77d63bfbee7500e831dcd2414966209820d5`
- [Implementation record](p1-local-logical-data-recovery-20260823.md) SHA-256:
  `548b110053ae7431d77b1fdece247440c85d6039105d92782cae7f270c1ea9a5`

The candidate was clean, had upstream divergence `0/0`, and matched the remote branch at opening and closing identity
checks.

## Exact reviewed files

| Path                                                        | SHA-256                                                            |
| ----------------------------------------------------------- | ------------------------------------------------------------------ |
| `internal/store/postgres/data_recovery_integration_test.go` | `bb09a201a8ed60b7a5be18e413468dc2cbe33f6d796155aacf5140822543f7df` |
| `scripts/data-recovery-validator/main.go`                   | `3471adbae5a7226b5f773cc61c2dfbbde8e68e7c33c2c1c9ceda618648607efb` |
| `scripts/data-recovery-validator/main_test.go`              | `19bcfda150e9c11f9ab966c576724bbea7d83c72883d58943e87eaf7fe499807` |
| `scripts/p1-data-recovery-cleanup.sh`                       | `2f18e9d7cb5d973f5be01c43829a00f5718ae39a4ece79cef46e5841df11d7b8` |
| `scripts/test-p1-data-recovery-cleanup.sh`                  | `7fde374a0ae2d7b602031843756c641f9b921221e3254bde93917314d8a84c27` |
| `scripts/test-p1-data-recovery-postgres.sh`                 | `96e58c257ff3a2540370bfd8ebf846b5d07982f805f2ec2c1443b6939222090c` |

The checked-in migration manifest, `go.mod` and `go.sum` remained fixed at
`d716baa0e2e3dc8dfdb8acdf9d7732c564f4105826d28e76d417ec5432c63980`,
`a4d98dcbd65803a22bcf946cf042d17484e714500c0502b616d68742a02f1d14`, and
`c5e16bfbadc2461fd349b94ce6487aadcb2edea11fa0aa37fd29bc2f46bfc88c` respectively.

## Prior P1 closure

The predecessor candidate `39b8c11812b5f261316a2078cdfced8c3c3033ab` was correctly blocked because an
`EXIT` trap cannot propagate cleanup failure over an already successful Bash status. The superseding implementation
closes that finding:

- the normal success path prepares its result without publishing it;
- `p1_data_recovery_finish` removes the `EXIT` trap and invokes `cleanup` synchronously;
- owned-container removal or artifact-directory removal failure returns nonzero, and the directory is checked for
  absence after `rm -rf`;
- only successful cleanup prints the prepared `PASS` line;
- the Bash 3.2 fixture proves `CLEANUP` precedes `PASS` and a simulated cleanup failure is nonzero with no `PASS`.

Signal and earlier error paths retain the existing no-PASS behavior. A cleanup failure may deliberately leave evidence
for diagnosis, but it can no longer be represented as a successful drill.

## Data-recovery boundary review

- The migration decoder is strict and validates the current manifest/digests. All eleven current entries form the
  exact `000001` through `000011` lineage and are transaction bounded. The generated ledger contains exactly eleven
  rows and seventeen fields, preserving the first predecessor as PostgreSQL `NULL`.
- The runner pins a locally present PostgreSQL 17 image by digest, rejects implicit pulls, creates only two run-unique
  label-owned containers, binds the repository read-only and refuses to remove a container without the exact run label.
- The source and restored clusters use the same exact server version. Restore must reproduce the exact current ledger
  before recovery and the deterministic snapshot covers every ordinary/partitioned `cloud_agents` table and sequence.
- Recovery exercises terminal and pending idempotency replay, terminal completion of the pending record, expired outbox
  reap/reclaim/acknowledgement, empty final outbox probe and leader reacquisition with fencing token advancement.
- The first two PostgreSQL executions remain explicitly `NOT PASS`. The third real execution and its retained hashes
  used the predecessor runner. The record accurately states that the cleanup-remediated fixed tree was not rerun against
  PostgreSQL and is not a full fixed-tree or Gate PASS.
- The backup is temporary. Current read-only inspection found no matching source/restore container, run directory,
  checked-in dump/backup or secret; the exact candidate Gitleaks scans also found no leak.
- The strict SQL replay is not represented as an authenticated production `migration.Runner.Run`. N/N-1, PITR,
  HA/failover, physical controller/cache loss, immutable aggregate evidence and `G-DATA` closure remain open.

## Fresh checks

- `/bin/bash` identity: GNU Bash `3.2.57(1)-release`.
- Three-script `bash -n`: PASS.
- `test-p1-data-recovery-cleanup.sh`: PASS.
- Exact Go `1.26.6` validator test: PASS (`0.664s`), including the Bash cleanup fixture.
- Exact Go `1.26.6` validator vet: PASS.
- Candidate diff check, Go format and target Markdown format: PASS.
- Gitleaks: remediation implementation `2.42 KB` PASS; complete two-commit superseding range `4.72 KB` PASS.

The supplied focused race result (`1.595s`) was not repeated because the repaired authority is exercised by the direct
Bash 3.2 fixture and the normal Go wrapper test. No real PostgreSQL drill, full migration, migration shard, broad race,
remote database or live production operation was run during this review.
