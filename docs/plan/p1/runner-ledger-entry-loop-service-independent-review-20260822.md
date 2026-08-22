# Runner ledger entry-loop service independent review — 2026-08-22

- Status: `APPROVE`
- Verdict: `P0=0`, `P1=0`, `P2=0`
- Fixed candidate: `9fcdb731cc54118b0f86bbfcac593e9129077d1a`
- Candidate branch: `codex/cloud-agents-p1-runner-entry-loop-20260822`
- Review branch: `codex/cloud-agents-p1-runner-entry-loop-independent-review-20260822`
- Gate effect: none; this review does not close or advance any Gate

## Fixed identities

The review resolved these exact identities before checks and again before creating this record:

| Identity                                         | Value                                      |
| ------------------------------------------------ | ------------------------------------------ |
| fixed candidate commit                           | `9fcdb731cc54118b0f86bbfcac593e9129077d1a` |
| fixed candidate tree                             | `b5aa8dfd7565e4ef5c1099872bf077043bdab201` |
| fixed candidate parent / Slice C review          | `818c4d5f05e6ea9c86c33c742c7f16292a8c208d` |
| approved Slice C candidate                       | `9db5891a7a624b1fae427ba25800ccb270b7e85e` |
| fixed candidate `services/control-plane` subtree | `c78ffc27c88b0f50871795a281669b7b2ef9bd27` |
| fixed candidate `contracts` subtree              | `f2d7e4d5221e3ecedf0117fead15945e067b4e70` |

Key reviewed file identities are:

| File                                                                                                | SHA-256                                                            |
| --------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------ |
| `services/control-plane/internal/migration/runner.go`                                               | `e501e39f890f4e0436809e4fc0ea48526218539e3c8821b1b292556ab3d08922` |
| `services/control-plane/internal/migration/runner_ledger_consumer_service.go`                       | `cbd2027adfa2dff557cc359da1932575bd05abc50f1b08370ed48d38a7546ac3` |
| `services/control-plane/internal/migration/runner_ledger_consumer_service_test.go`                  | `b77d3ad678859afba94656063f7f4d40747deff25d786d7dc964e3f0bd940f87` |
| `services/control-plane/internal/migration/runner_ledger_entry_execution_admission_claim.go`        | `605c73cec501dc2b879940363de693670024d185ecfca61f50c1d7cd93a396c0` |
| `services/control-plane/internal/migration/runner_ledger_entry_execution_admission_service_test.go` | `f26de999add5322597d0f979e2443823778dce68078084d698a066e1b43278af` |
| `services/control-plane/internal/migration/runner_ledger_entry_success_kernel_test.go`              | `6b268292da88aed4fb0b3b514dc27209bcbd2f266c102ebbe72e2731f4b8a6b0` |
| `docs/plan/p1/runner-ledger-entry-loop-service-matrix-20260822.md`                                  | `809e7dab49be6fd6726ed6024332345b16cbe6e9609d9d789125d1222c434c57` |

The candidate worktree was clean, its local and remote refs were equal, and upstream was `0/0`. The remote candidate
ref resolved to the fixed commit. This review did not edit the candidate.

## Review result

Slice D connects the generated consumer to the independently reviewed success kernel through one bounded entry loop.
The outer function first verifies and owns the runtime bundle, freezes the exact signed migration order, and bounds
the loop by that order. Every iteration enters `consumeRunnerLedgerPreflightStep` afresh: it mints and consumes a new
same-verifier preflight claim and completes the reviewed preflight session before it can open a distinct, freshly
locked execution-admission session. The retained execution session is consumed by at most one success-kernel call and
is closed by that kernel's exact commit/cleanup boundary. No database session or permit is carried into the next
iteration.

The complete-ledger generated pair remains a read-only `return_success` no-op with empty `Applied` and
`AmbiguousRecovered`, the exact signed final head, and no execution admission or transaction. Of the five entry
consumer pairs, only the four generated first-attempt pairs pass the separate generated execution-admission selector.
The fifth retry pair completes its locked read-only observation, consumes its claim, closes its session, and returns
stable `MIGRATION_PROJECTION_NOT_IMPLEMENTED`. All eleven recovery/retry/reconcile/failure pairs remain closed and
have no success-kernel or writer call edge.

For an admitted entry, the service captures the exact permit use record, binder, consumer-fact subject, and evidence
boundary before the permit is consumed. It accepts the kernel's ordinary outcome only after exact cross-checks against
the consumed generated next-entry identity, old prefix length, new ledger length/head, and complete-versus-next-entry
classification. Only then may the exact consumed use record be marked retired and atomically deleted from the same
binder registry. Wrong binder, subject, boundary, record replacement, prior retirement, or failed compare-delete
returns recovery-required and emits no committed-entry step. A kernel error never reaches retirement and therefore
cannot open the next admission.

The ordinary success outcome is data, not a permit or claim. The outer loop checks it again against its independently
frozen migration order and prefix continuity, accumulates only the newly committed suffix, and discards it before a
non-final next iteration. A failing fresh successor preflight therefore stops without opening a second execution
session or reusing the first outcome. The final entry returns only after its kernel outcome is classified complete;
an already-complete ledger remains the separate no-op path.

Retirement is one-shot under its record mutex and `sync.Map.CompareAndDelete`. Evidence-session close remains a final
registry cleanup. Context cancellation, runtime owned-input drift, claim/evidence mismatch, unsupported selection,
kernel failure, retirement failure, preflight close failure, and evidence-session close failure preserve the reviewed
error and cleanup precedence. The production AST has exactly one success-kernel caller and one retirement caller,
both in the consumer service. That service has no direct `BeginMigration`, SQL execution, ledger/evidence append,
commit, retry/abort/reconcile/failure writer, HTTP/P2/provider, deployment, or publication edge.

Slice D changes no contract, schema, fixture, generated registry/profile, SDK, SQL migration, database function,
dependency, workflow, deployment, or release artifact. Direct Git comparison with the Slice C review base and the
pinned contract checker both confirm the historical generated boundary is unchanged/current.

## Fresh checks

The independent candidate worktree used Node `24.13.1`, Bun `1.3.14`, Go `1.26.6 darwin/arm64`, and Gitleaks
`8.30.1`. Checks against the fixed candidate produced:

- bounded focused consumer/public-runner/execution-admission/production-graph normal: PASS in `74.073s`;
- risk-relevant generated-matrix, claim-cleanup, failed-kernel-retention, fresh-loop, unsupported-pair, and retirement
  scope with `-race`: PASS in `306.287s`;
- `bun run platform:contracts:check`: PASS/current for `115` JSON files, `50` schemas, and `62` fixture cases, with
  every generated registry/profile/SDK and the generation lock current;
- control-plane `go vet ./...`, `go build ./...`, `go mod tidy -diff`, and `go mod verify`: PASS;
- repository lint and TypeScript typecheck: PASS;
- Linux `amd64` and `arm64` migration test-binary compile with `CGO_ENABLED=0`: PASS; binary SHA-256
  `8ecb99e73f4046a1081c715a20a1aac03cd1f7905e8fa7c2864679077029d855` and
  `7169af07f466879080d1c373819d225c69711505248d745d8e836452b2dece5f`;
- all candidate-changed Go files `gofmt`, changed documentation `oxfmt --check`, and candidate-range
  `git diff --check`: PASS;
- direct same-bits comparison of contracts, generated Go files, and the four historical profile sources against the
  Slice C review base: PASS; and
- Gitleaks over `818c4d5..9fcdb73`: PASS, one commit and `47.52 KB`, no leaks.

This review deliberately did not run full `internal/migration` normal or a broad/full-package race. It does not reuse
the Slice C 30-minute timeout as evidence: that run remains explicitly `NOT PASS`. The contract check's
`BOOTSTRAP_VALIDATED` status is current-generation evidence, not Gate closure or validation of its separately listed
missing external suites.

## Non-claims

This review did not modify the fixed candidate, merge or open a pull request, connect to a live or production
PostgreSQL instance, write production data, deploy, publish, release, or close a Gate. It does not claim a live
PostgreSQL run, full `internal/migration` normal PASS, broad/full-package race PASS, or authorization for HTTP/P2/
provider behavior.

The verdict approves only fixed candidate `9fcdb731cc54118b0f86bbfcac593e9129077d1a` for ADR-0022 Slice D's bounded
first-attempt entry loop, exact success-only admission retirement, and retained complete-ledger no-op. Retry, abort,
reconcile, failure, and recovery writers remain `NOT_IMPLEMENTED`; any later Slice remains subject to its existing
fixed-candidate and independent-review requirements.
