# P1 migration shard runner closure repair — 2026-08-22

- Status: **IMPLEMENTATION FIXED; HISTORICAL RUN REVALIDATED; INDEPENDENT REREVIEW APPROVED; GATES OPEN**
- Implementation source: `8552c0c2c5e4abea128f29e8f41c7628d1e355d1`
- Source tree: `9cdd16afe19e235376c853fdc04b6dd95e44a341`
- Control-plane subtree: `44174a871ddf2da859f901050634f9f1995f0aa6`
- Branch: `codex/cloud-agents-p1-migration-shard-runner-repair-20260822`
- Superseded runner candidate: `e18a1ee228e2465a805654dbbc01a3af618ca8b5`
- Superseded repair candidates: `668ee1d1efaff2d81994cd1a4230a89aca2490db`,
  `7844d4af8e52ce5a958dff7d1a82721961dc94e1`,
  `a9e59f4ba859850d79f8d0e238649f5f55354823`, and
  `d34ac3f92591abac5bf59b0bba34085a461f68ba`
- Independent review: [`1a98f729bdfd9cecacdddb1b23142648b20d6217`](migration-shard-runner-closure-repair-independent-review-20260822.md) — `APPROVE, P0=0/P1=0/P2=0`
- Independent review record SHA-256: `9284c72f6e3fe3637bcddbf8f4a0d27e53045697363fc80fe9af9c740a6bb9cd`
- Gate effect: **none**

This record repairs the reusable runner contract after independent review returned
`BLOCK, P0=0/P1=2/P2=0`. It does not rerun the exhaustive migration suite. It preserves the
historical 550-second run and its files unchanged, then applies the repaired validator read-only to
those files. It changes no migration test, migration production package, `go.mod`, `go.sum`, SQL,
database, route, provider, deployment, publication, release, or Gate.

## 1. Superseded candidate and exact findings

The historical
[`current-source-migration-shard-closure-20260822.md`](current-source-migration-shard-closure-20260822.md)
correctly recorded the observed run at implementation source `7f14c7f`: 700 top-level run events,
`695 pass + 5 explicit external-PostgreSQL skip`, zero fail, eight package passes, and exact bound
file hashes. Independent review reconfirmed those artifact facts but rejected the reusable runner:

1. each background job was a Bash function whose PID was recorded, while `go test` and the test
   binary were descendants; the signal trap sent `TERM` only to the wrapper PID, so runner exit did
   not prove descendant exit or artifact stability; and
2. the runner trusted process exit codes and never strictly parsed `go-test.jsonl` before printing
   PASS; it therefore did not itself prove that every planned top-level test had exactly one `run`
   and one `pass`/`skip`, that no unexpected/failing test existed, or that every shard had exactly
   one package pass; and
3. the first repair launched a wrapper process group before registering its PID/PGID in the cleanup
   arrays. An `INT` or `TERM` in that interval could make the parent publish ABORTED and exit while
   the unregistered wrapper remained blocked on its unpublished start gate; and
4. the next repair retained already-reaped successful wrapper PID/PGID values in the active arrays
   until the whole batch completed. OS reuse of an old numeric PGID could therefore let a later
   signal cleanup target an unrelated process group; and
5. the following repair released the wrapper leader before checking whether the group still held a
   descendant. If a test returned while leaving a same-group background process, the later residue
   check no longer had a live leader proving that the numeric PGID was still task-owned, so it could
   neither safely terminate the residue nor safely signal a possibly reused group.

The old record is retained as historical local-run evidence. Its reusable-runner admissibility is
superseded by this repair and must not be cited as an independently approved runner.

## 2. Repaired closed runner

The implementation source fixes only runner tooling:

| File                                                                    | SHA-256                                                            |
| ----------------------------------------------------------------------- | ------------------------------------------------------------------ |
| `services/control-plane/scripts/test-migration-shards.sh`               | `f5ea45d914d16be964528842b5d161eddefeffaa410bc20b6191956f4f2e981e` |
| `services/control-plane/scripts/test-migration-shard-runner-fixture.sh` | `b17fdd2527eac868d07c25d58c7bdc14fc9aec0aef50f26caac58962dbfce898` |
| `services/control-plane/scripts/migration-shard-validator/main.go`      | `62f54ce0fac102519f0cb3278bdf464e5811430e5115ab805277d7fdec083210` |
| `services/control-plane/scripts/migration-shard-validator/main_test.go` | `32b487cb8801698873f1874e9dbe2ae33d1ecbdd59fd53f6e74dbbddf4fa7543` |

### 2.1 Signal and descendant closure

Before authorizing a shard to start, the runner enables Bash job control, launches the wrapper in a
new process group, records the expected PID/PGID, confirms through `ps` that `PGID == wrapper PID`,
and only then publishes that shard's start gate. The parent process is never in a shard group.

The launch interval is an explicit state. `INT`/`TERM` received after launch begins but before
registration completes records the first pending signal without exiting. After PID/PGID
registration and validation, and still before publishing the start gate, the runner consumes that
signal through the same bounded group cleanup path. A failed PGID validation first terminates and
reaps the unstarted wrapper, then consumes any pending signal. Thus no signal boundary can precede
both registration and cleanup.

The EXIT trap also preserves the original nonzero status while terminating every registered group
whenever an unexpected `set -e` failure occurs during a run. This covers launch artifact/start-gate
write failures without converting them into PASS or leaving a blocked wrapper behind.

Every running wrapper now remains its process-group leader after publishing `wrapper-complete.tsv`
and stops its isolated process group before the parent-owned retire gate. The parent proves through
`ps` that the expected leader is stopped and that the group contains exactly that wrapper. Only then
does it enter a signal-deferred retirement transition, publish the gate, continue and wait that
exact group, mark the active entry retired, clear its PID/PGID values, and consume a pending signal.
If another group member exists, the gate remains closed, the run becomes FAIL, and cleanup terminates
the group while the stopped live leader still proves the negative-PGID authority. Cleanup otherwise
sends a negative-PGID signal only for an active entry whose current projection proves
`PID == expected PGID`; an unproved or retired numeric identity is never signaled. A fast shard can
therefore finish while a slow sibling runs without leaving stale cleanup authority.

For `INT` or `TERM`, the runner:

1. disables recursive traps;
2. sends `TERM` to each complete negative PGID, not merely the wrapper;
3. waits for a bounded five seconds, escalates remaining groups to `KILL`, waits/reaps wrappers, and
   performs a second bounded no-group check;
4. removes its task-owned temporary validator binary; and
5. publishes `run-aborted.tsv` plus `run-status.txt=ABORTED`, with exit `130` or `143`.

A normal wrapper retirement also requires the pre-release exact-membership proof and the
post-release absence of its process group. Any residue terminates the remaining batch and makes the
run fail. PASS is written atomically only after all groups have gone, source identity and cleanliness
are unchanged, and strict JSON validation succeeds.

### 2.2 Strict result closure

The exact Go 1.26.6 toolchain builds a standard-library-only validator from the checked-in source.
For every shard it rejects:

- invalid JSON, duplicate or unknown fields, unknown actions, invalid event timestamps, a foreign
  package, or failed-build metadata;
- missing, duplicated, out-of-order, or unexpected top-level test events;
- any top-level or nested failure;
- a planned test without exactly one `run` and one terminal `pass`/`skip`;
- a missing, duplicated, premature, or non-final package pass; and
- any non-empty successful shard stderr.

Only a successful validation publishes that shard's `validation.tsv`. The runner binds each file's
SHA-256 into aggregate `validation.tsv`, binds the aggregate SHA-256, includes validator status and
validation digest in `results.tsv`, and then atomically publishes `run-status.txt=PASS`. An exit-zero
process with missing tests is therefore FAIL.

## 3. Same-bits boundary and no exhaustive rerun

The repair does not change the package that produced the historical evidence:

| Bound input                                      | `7f14c7f`                                  | `8552c0c`                                  |
| ------------------------------------------------ | ------------------------------------------ | ------------------------------------------ |
| `services/control-plane/internal/migration` tree | `f773674985a9b1c2f7f5e7af47c12258e7e28ff1` | `f773674985a9b1c2f7f5e7af47c12258e7e28ff1` |
| `services/control-plane/go.mod` blob             | `c908536ef26a55b3dae7ddf31d7e7545a19c3a48` | `c908536ef26a55b3dae7ddf31d7e7545a19c3a48` |
| `services/control-plane/go.sum` blob             | `70a855a6aba30804c85e9f6434cfd83115620852` | `70a855a6aba30804c85e9f6434cfd83115620852` |

The prior repair's fresh `plan --shards 8` reports 700 tests and list SHA-256
`d7cdd59e7ec3bd75d5832c0dd581d2afe2271fd775ba97007d72f639176b5fbd`. Its eight
`shards.tsv` rows byte-compare with the historical run: `88,88,88,88,87,87,87,87`, with unchanged
test-list and regex hashes. The later process-lifecycle-only delta does not touch plan mode or the
bound migration/go.mod/go.sum inputs.

Per the approved no-repeat policy, no full normal, full race, or 30-minute migration command was
rerun. The implementation work used only the validator package, a two-test fake-Go fixture, plan
mode, and read-only parsing of the existing artifact directory.

## 4. Read-only historical artifact revalidation

The repaired validator was built with exact Go 1.26.6 and applied read-only to the eight existing
`go-test.jsonl`/`tests.txt` pairs under the historical task-owned evidence directory. It produced the
following deterministic temporary results; the directory itself was not changed:

| Shard | Validation SHA-256                                                 | Planned | Run | Pass | Skip | Fail | Package pass |
| ----- | ------------------------------------------------------------------ | ------: | --: | ---: | ---: | ---: | -----------: |
| 00    | `c6d9b50d2176a295d23c3ccecd935c98c4082456d4bb311a13c6a41cba64f044` |      88 |  88 |   88 |    0 |    0 |            1 |
| 01    | `f8da3545a013ea138e62055907f7d1bbc5224399c46db9eb152cd8e9d0136be4` |      88 |  88 |   88 |    0 |    0 |            1 |
| 02    | `5f90f26a42561aa9f6a35eda754b18e7ab576ee4fba84c5ec827d8a5303c704b` |      88 |  88 |   87 |    1 |    0 |            1 |
| 03    | `d9013485a8c82fbcd75f8bb2c9cab7ff1deaf2ef945afae1245b8880bd3f47b7` |      88 |  88 |   87 |    1 |    0 |            1 |
| 04    | `8be4afdaa192ecaa6f4bda718c64e12d8b8dc07a0165b98c60d81333853f2b8b` |      87 |  87 |   86 |    1 |    0 |            1 |
| 05    | `301f933dba5d62e6060537dabb4b4850a8226495d969e5b738b6502886f6f1a0` |      87 |  87 |   87 |    0 |    0 |            1 |
| 06    | `e4699448cd0f3000574d836b90c3d97b17863f8b0dff7013b92a25cc98df6261` |      87 |  87 |   85 |    2 |    0 |            1 |
| 07    | `659cf4a1757ece826aac72994a348c0f29230eb1aaad62a264966c716bee4b43` |      87 |  87 |   87 |    0 |    0 |            1 |

Aggregate: 700 planned, 700 run, 695 pass, 5 skip, zero fail, eight package passes. The temporary
summary TSV SHA-256 was
`42da342e125eab752bbd4973ca4097f80364889f9e069c5d53149f1bc34917e1`.
This confirms the old observation; it does not claim those old files were emitted by the repaired
runner.

## 5. Narrow verification

The implementation source passed:

- `bash -n` for the runner and fixture;
- exact ShellCheck `0.10.0-r2` in
  `alpine@sha256:14358309a308569c32bdc37e2e0e9694be33a9d99e68afb0f5ff33cc1f695dce`;
- validator `go test`, `go test -race`, and `go vet` with exact Go 1.26.6,
  `GOWORK=off`, `GOTOOLCHAIN=local`, `GOFLAGS=-mod=readonly`;
- a clean-worktree Bash 3.2 fixture using only two fake tests: valid closure PASS, exit-zero missing
  tests FAIL, post-registration `TERM`/`INT` exit 143/130 with `deferred_during_launch=0`, exact
  pre-registration `TERM`/`INT` exit 143/130 with `deferred_during_launch=1`, no fake worker start
  in the latter cases, no surviving wrapper/child/process group, ABORTED status, and stable
  output-directory digest after exit; an injected post-registration unexpected exit 97 likewise
  leaves no wrapper/group, starts no fake worker, publishes no PASS, and leaves stable artifacts;
  a mixed fast/slow two-job case injects `TERM` at the fast wrapper's retirement boundary, proves
  `deferred_during_retirement=1`, cleans the slow group, and records no signal to the retired PGID;
  and a two-job same-group background-process case proves the parent rejects retirement before
  releasing the live leader, kills every wrapper/child/group, publishes FAIL rather than PASS, and
  leaves a stable artifact digest;
- fresh plan/list/partition same-bits comparison against the historical artifacts;
- `git diff --check`; and
- staged Gitleaks 8.30.1 with no findings.

## 6. Non-claims

This repair record does **not** claim or authorize:

- a fresh exhaustive migration run at `8552c0c`, a full race run, or live PostgreSQL 15/16/17;
- approval beyond this exact reusable-runner implementation/review slice;
- production database reads/writes, HTTP/P2/provider effects, deployment, publication, or release;
- physical controller power-loss evidence; or
- `G-DATA`, `G-AUTHORITY-P1`, `G-SECURITY-P1`, another Gate, or Platform RC closure.

The independent reviewer re-resolved the fixed source, tree, control-plane subtree, five file hashes,
remote ref, clean worktree, and upstream `0/0`; inspected the process-group and JSON state machines;
reran only the short Bash 3.2 fixture plus validator normal/race/vet; and returned
`APPROVE, P0=0/P1=0/P2=0`. No full migration, full shards, broad race, or live PostgreSQL run was
performed. The verdict closes only this implementation/review slice; every aggregate Gate remains
open.
