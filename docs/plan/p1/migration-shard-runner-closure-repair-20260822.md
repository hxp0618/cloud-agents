# P1 migration shard runner closure repair — 2026-08-22

- Status: **IMPLEMENTATION FIXED; HISTORICAL RUN REVALIDATED; INDEPENDENT REREVIEW PENDING; GATES OPEN**
- Implementation source: `60f13f7d0dbc8b67b56af3f2398ca4f4a8c48c5d`
- Source tree: `80d57e5a01bfd38b904b3c5212b8ffa4be0a8fec`
- Control-plane subtree: `ca7f4acb06f93d6505ccef8ec22adbdda95d7115`
- Branch: `codex/cloud-agents-p1-migration-shard-runner-repair-20260822`
- Superseded runner candidate: `e18a1ee228e2465a805654dbbc01a3af618ca8b5`
- Independent reviewer: **pending fixed-candidate rereview**
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
   one package pass.

The old record is retained as historical local-run evidence. Its reusable-runner admissibility is
superseded by this repair and must not be cited as an independently approved runner.

## 2. Repaired closed runner

The implementation source fixes only runner tooling:

| File                                                                    | SHA-256                                                            |
| ----------------------------------------------------------------------- | ------------------------------------------------------------------ |
| `services/control-plane/scripts/test-migration-shards.sh`               | `752f49eaaf85528ee0187767b243e178955bcdeac4d5f5c5c47afb55929d8f99` |
| `services/control-plane/scripts/test-migration-shard-runner-fixture.sh` | `5e590689e07767ea8ae3b78119dce2d199f9f1a572b18e3b50a049c63eee0c88` |
| `services/control-plane/scripts/migration-shard-validator/main.go`      | `62f54ce0fac102519f0cb3278bdf464e5811430e5115ab805277d7fdec083210` |
| `services/control-plane/scripts/migration-shard-validator/main_test.go` | `32b487cb8801698873f1874e9dbe2ae33d1ecbdd59fd53f6e74dbbddf4fa7543` |

### 2.1 Signal and descendant closure

Before authorizing a shard to start, the runner enables Bash job control, launches the wrapper in a
new process group, records the expected PID/PGID, confirms through `ps` that `PGID == wrapper PID`,
and only then publishes that shard's start gate. The parent process is never in a shard group.

For `INT` or `TERM`, the runner:

1. disables recursive traps;
2. sends `TERM` to each complete negative PGID, not merely the wrapper;
3. waits for a bounded five seconds, escalates remaining groups to `KILL`, waits/reaps wrappers, and
   performs a second bounded no-group check;
4. removes its task-owned temporary validator binary; and
5. publishes `run-aborted.tsv` plus `run-status.txt=ABORTED`, with exit `130` or `143`.

A normal wrapper exit also requires its process group to be absent. Any residue terminates the
remaining batch and makes the run fail. PASS is written atomically only after all groups have gone,
source identity and cleanliness are unchanged, and strict JSON validation succeeds.

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

| Bound input                                      | `7f14c7f`                                  | `60f13f7`                                  |
| ------------------------------------------------ | ------------------------------------------ | ------------------------------------------ |
| `services/control-plane/internal/migration` tree | `f773674985a9b1c2f7f5e7af47c12258e7e28ff1` | `f773674985a9b1c2f7f5e7af47c12258e7e28ff1` |
| `services/control-plane/go.mod` blob             | `c908536ef26a55b3dae7ddf31d7e7545a19c3a48` | `c908536ef26a55b3dae7ddf31d7e7545a19c3a48` |
| `services/control-plane/go.sum` blob             | `70a855a6aba30804c85e9f6434cfd83115620852` | `70a855a6aba30804c85e9f6434cfd83115620852` |

Fresh `plan --shards 8` still reports 700 tests and list SHA-256
`d7cdd59e7ec3bd75d5832c0dd581d2afe2271fd775ba97007d72f639176b5fbd`. Its eight
`shards.tsv` rows byte-compare with the historical run: `88,88,88,88,87,87,87,87`, with unchanged
test-list and regex hashes.

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
  tests FAIL, `TERM` exit 143, `INT` exit 130, no surviving wrapper/child/process group, ABORTED
  status, and stable output-directory digest after exit;
- fresh plan/list/partition same-bits comparison against the historical artifacts;
- `git diff --check`; and
- staged Gitleaks 8.30.1 over approximately 38.28 KB with no findings.

## 6. Non-claims

This repair record does **not** claim or authorize:

- a fresh exhaustive migration run at `60f13f7`, a full race run, or live PostgreSQL 15/16/17;
- independent approval before a fixed-hash read-only rereview is recorded;
- production database reads/writes, HTTP/P2/provider effects, deployment, publication, or release;
- physical controller power-loss evidence; or
- `G-DATA`, `G-AUTHORITY-P1`, `G-SECURITY-P1`, another Gate, or Platform RC closure.

Independent review must verify the fixed source/hashes, process-group authority, signal fixture,
strict JSON state machine, same-bits boundary, and these non-claims. Until then this is implementation
evidence only and every aggregate Gate remains open.
