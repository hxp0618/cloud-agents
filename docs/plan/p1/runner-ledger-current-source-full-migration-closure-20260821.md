# P1 current-source full migration normal-suite closure - 2026-08-21

- Status: **LOCAL NORMAL SUITE PASS — NOT A GATE CLOSURE**
- Source commit: `b57acf257ba965daadede8cbaca5bd90d20b6287`
- Repository tree: `b31e004b23a02c0ef23ed9ca967568d79e8ba3cb`
- `services/control-plane` subtree: `091a42f5abb4798c7860e9e44f5164607abfb534`
- Branch: `codex/cloud-agents-p1-runner-ledger-consumer-entry-audit-20260821`
- Upstream state before the run: clean, ahead `0`, behind `0`

This record supersedes the earlier five-minute bounded stop as evidence for the current source. That stop was
correctly recorded as **NOT PASS**; the same current control-plane subtree has now completed the full normal
`internal/migration` package suite. This remains local implementation evidence only. It is not a full race result,
live PostgreSQL matrix, immutable Gate signature, production database authority, deployment, release, or
publication approval.

## 1. Fixed inputs

| Input                                   | Exact value                                                                 |
| --------------------------------------- | --------------------------------------------------------------------------- |
| Go                                      | `go1.26.6`                                                                  |
| GOOS / GOARCH                           | `darwin / arm64`                                                            |
| CGO                                     | `1`                                                                         |
| GOROOT                                  | `/Users/huang/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.6.darwin-arm64` |
| `services/control-plane/go.mod` SHA-256 | `a4d98dcbd65803a22bcf946cf042d17484e714500c0502b616d68742a02f1d14`          |
| `services/control-plane/go.sum` SHA-256 | `c5e16bfbadc2461fd349b94ce6487aadcb2edea11fa0aa37fd29bc2f46bfc88c`          |

The source commit contains only the already reviewed Slice C control-plane subtree plus documentation records. No
source, generated contract, fixture, migration, dependency, or test changed between the Slice C independent review
subtree `091a42f5abb4798c7860e9e44f5164607abfb534` and this run.

## 2. Exact command and result

From `services/control-plane`:

```sh
set -o pipefail
GOWORK=off GOFLAGS=-mod=readonly \
  /Users/huang/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.6.darwin-arm64/bin/go \
  test -json ./internal/migration -count=1 -timeout=30m |
  jq -r 'select((.Action == "run" or .Action == "pass" or .Action == "fail") and ((((.Test // "") | contains("/")) | not))) | [(.Time // ""), .Action, (.Test // .Package // ""), ((.Elapsed // "")|tostring)] | @tsv'
```

The shell used `pipefail`, so the pipeline could not hide a non-zero `go test` exit. Final package event:

```text
2026-08-21T20:54:39.082751+08:00  pass  github.com/hxp0618/cloud-agents/services/control-plane/internal/migration  1108.208
```

- Exit: `0`
- Elapsed reported by `go test`: `1108.208s`
- Package: `github.com/hxp0618/cloud-agents/services/control-plane/internal/migration`
- Count: uncached, `-count=1`
- Test timeout: `30m`; no timeout fired

The prior five-minute boundary occurred during long, progressing fault matrices. Representative completed top-level
tests in this run included:

- `TestRunnerCommitIntentFaultsRollbackWithoutTransactionCommit` — `97.36s`;
- `TestRunnerFinalIntermediateFaultsRollbackWithoutLedgerOrCommit` — `102.53s`;
- `TestRunnerCurrentLedgerFaultsRollbackWithoutEvidenceAppendOrCommit` — `76.60s`;
- `TestRunnerPreledgerProjectionFaultsRollbackWithoutAppendingEvidenceOrLedger` — `69.71s`;
- `TestRunnerStatementIntentAppendFaultsFailClosedBeforeSQLAndReleaseDatabase` — `65.35s`; and
- `TestRunnerTransactionCommitConsumesDurableIntentAndClosesExactOutcome` — `60.70s`.

This demonstrates that the earlier silence was long-running bounded matrix work, not evidence of a deadlock or
failure.

## 3. Boundaries and remaining work

- Full `internal/migration` **normal** suite: PASS for the fixed current control-plane subtree.
- Full `internal/migration` **race** suite: NOT RUN by this record. Existing focused race evidence is not promoted to
  a full race claim.
- PostgreSQL 15/16/17 results in the Slice C record remain an in-process metadata/state matrix, not a live
  three-version database run.
- No production or live database was read or written.
- No `Runner.Run` consumer was added for ledger preflight; no writer path, HTTP/P2/provider effect, deployment,
  publication, release, or Gate closure was performed.
- An independent reviewer has not signed this local normal-suite record. Any immutable or aggregate Gate remains
  OPEN and requires its own fixed-source closure process.
