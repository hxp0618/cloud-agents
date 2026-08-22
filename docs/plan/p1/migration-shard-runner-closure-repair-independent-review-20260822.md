# Migration shard runner closure repair independent review — 2026-08-22

- Verdict: **APPROVE**
- Findings: **P0=0 / P1=0 / P2=0**
- Fixed candidate: `8e4950129f1c114be4304af1706e9452ba014ccd`
- Candidate tree: `d2838ace0eb85e01a1ccc3320e955e5a514d8912`
- Control-plane subtree: `44174a871ddf2da859f901050634f9f1995f0aa6`
- Implementation commit: `8552c0c2c5e4abea128f29e8f41c7628d1e355d1`
- Candidate branch: `codex/cloud-agents-p1-migration-shard-runner-repair-20260822`
- Review branch: `codex/cloud-agents-p1-migration-shard-runner-repair-independent-review-20260822`
- Gate effect: **none**

This is an independent, fixed-hash, read-only review of the reusable migration shard runner repair.
The candidate was clean, its configured upstream was `0/0`, and local `HEAD`, upstream, and the
remote candidate branch all resolved to the fixed commit before and after the review checks. The
candidate was not modified or merged.

## 1. Fixed evidence identity

| File                                                                    | SHA-256                                                            |
| ----------------------------------------------------------------------- | ------------------------------------------------------------------ |
| `services/control-plane/scripts/test-migration-shards.sh`               | `f5ea45d914d16be964528842b5d161eddefeffaa410bc20b6191956f4f2e981e` |
| `services/control-plane/scripts/test-migration-shard-runner-fixture.sh` | `b17fdd2527eac868d07c25d58c7bdc14fc9aec0aef50f26caac58962dbfce898` |
| `services/control-plane/scripts/migration-shard-validator/main.go`      | `62f54ce0fac102519f0cb3278bdf464e5811430e5115ab805277d7fdec083210` |
| `services/control-plane/scripts/migration-shard-validator/main_test.go` | `32b487cb8801698873f1874e9dbe2ae33d1ecbdd59fd53f6e74dbbddf4fa7543` |
| `docs/plan/p1/migration-shard-runner-closure-repair-20260822.md`        | `99bfc6f25576f28b2c45df32b1ce6347af6dd31f13331b0a38bd6069f1b68890` |

The migration package and module inputs are byte-identical to the historical `7f14c7f` run:

| Input                                            | Fixed identity                             |
| ------------------------------------------------ | ------------------------------------------ |
| `services/control-plane/internal/migration` tree | `f773674985a9b1c2f7f5e7af47c12258e7e28ff1` |
| `services/control-plane/go.mod` blob             | `c908536ef26a55b3dae7ddf31d7e7545a19c3a48` |
| `services/control-plane/go.sum` blob             | `70a855a6aba30804c85e9f6434cfd83115620852` |

## 2. Process authority and lifecycle verdict

The launch, running, completion, retirement, signal, and unexpected-exit paths form a closed
authority lifecycle:

1. A shard cannot start before the parent records its wrapper PID/PGID and independently confirms
   `PID == PGID`. A signal received during that registration interval is retained as the first
   pending signal and consumed before the start gate is published.
2. The wrapper remains the live process-group leader after the Go command and all result/hash files
   are complete. It publishes `wrapper-complete.tsv`, stops the isolated group, and cannot retire
   itself without the parent-owned gate.
3. The parent proves the exact leader is stopped and enumerates the group before retirement. It
   publishes the retire gate and sends `CONT` only when the group contains exactly the wrapper. A
   same-group descendant keeps the gate closed and is terminated while the live leader still binds
   the negative-PGID authority; the run becomes FAIL.
4. Retirement is a signal-deferred transition. After the exact wrapper is continued and reaped, the
   parent immediately marks the entry retired and clears its PID/PGID before consuming a pending
   signal. Completed numeric identifiers therefore cannot remain cleanup authority while a sibling
   shard continues.
5. Signal and unexpected-EXIT cleanup consider only active entries and revalidate the live
   PID-to-PGID projection before any negative-PGID TERM/KILL. TERM is bounded, completion permits an
   early group KILL, remaining groups receive bounded KILL/reap verification, and cleanup failure is
   recorded rather than converted into PASS.

The Bash 3.2 fixture independently passed valid closure, exit-zero/missing-test rejection,
post-registration TERM/INT, exact pre-registration TERM/INT deferral, unexpected exit 97,
fast/slow retirement-signal interleaving, and same-group descendant residue. It proved the expected
130/143/97/FAIL outcomes, no surviving recorded wrapper/child/group, no fake worker before an
unauthorized start, no signal to a retired PGID, and stable artifacts after runner exit.

## 3. Strict JSON result closure

The standard-library validator and runner jointly require every planned top-level test to have one
exact `run` and one terminal `pass` or `skip`; they reject missing, duplicate, unexpected,
out-of-order, failing, foreign-package, failed-build, malformed, duplicate-field, unknown-field, and
unknown-action events. Nested failures are rejected. Exactly one package start and one final package
pass are required, and successful shard stderr must be empty.

Validation output is published only on success. Per-shard validator exit status and validation hash
are bound into aggregate validation/results files before the atomic PASS status. A Go process exit
code alone cannot mint PASS.

## 4. Independent checks

- exact commit/tree/subtree, five SHA-256 values, clean worktree, upstream `0/0`, and remote-exact
  identity: PASS;
- Bash `3.2.57`, runner and fixture `bash -n`, and the complete clean-worktree fake-Go fixture: PASS;
- exact Go `1.26.6` validator normal test: PASS (`0.557s`);
- exact Go `1.26.6` validator race test: PASS (`1.563s`);
- exact Go `1.26.6` validator `go vet`: PASS;
- current migration tree and `go.mod`/`go.sum` same-bits comparison with `7f14c7f`: PASS;
- `git diff --check`: PASS;
- Gitleaks `8.30.1`, `7f14c7f..8e49501`, 13 commits and approximately 86.32 KB: PASS, no findings.

The earlier independent plan check returned 700 tests, list SHA-256
`d7cdd59e7ec3bd75d5832c0dd581d2afe2271fd775ba97007d72f639176b5fbd`, and eight exact historical
rows with counts `88,88,88,88,87,87,87,87`. Subsequent fixed-candidate deltas are confined to the
run-mode process lifecycle below the completed plan branch; the plan and bound package/module inputs
remain unchanged.

The unchanged repaired validator was also applied read-only to the eight historical JSON logs. It
closed 700 planned/run tests as 695 pass, 5 explicit skip, zero fail, and eight package passes. All
90 historical bound files still passed their manifest, whose SHA-256 is
`349ddbe1eb1d2972750b0a5cc34937e7c8c8b081134f5765ea7f4428e3e35011`. This confirms the historical
observation only; it does not relabel those files as output from the repaired runner.

## 5. Non-claims and verdict boundary

This approval does not claim a fresh exhaustive migration run at `8e49501` or `8552c0c`, a full
package race run, live PostgreSQL 15/16/17, controller power-loss evidence, production database
reads/writes, HTTP/P2/provider effects, deployment, publication, release, or any Gate closure. No
full migration command, full shard run, broad race, or live PostgreSQL check was run during this
review.

Within that boundary, the fixed candidate closes the two original reusable-runner P1 findings and
the subsequent launch, unexpected-exit, stale-PGID, and same-group-residue authority findings.
Verdict: **APPROVE, P0=0 / P1=0 / P2=0**.
