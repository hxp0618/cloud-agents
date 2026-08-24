# Runner ledger entry success-kernel service independent review — 2026-08-22

- Status: `APPROVE`
- Verdict: `P0=0`, `P1=0`, `P2=0`
- Fixed candidate: `9db5891a7a624b1fae427ba25800ccb270b7e85e`
- Candidate branch: `codex/cloud-agents-p1-runner-entry-success-kernel-20260822`
- Review branch: `codex/cloud-agents-p1-runner-entry-success-kernel-independent-review-20260822`
- Gate effect: none; this review does not close or advance any Gate

## Fixed identities

The review resolved these exact identities before checks and again before creating this record:

| Identity                                         | Value                                      |
| ------------------------------------------------ | ------------------------------------------ |
| fixed candidate commit                           | `9db5891a7a624b1fae427ba25800ccb270b7e85e` |
| fixed candidate tree                             | `351d16cfab560601f4be5418b3eae2d785328257` |
| fixed candidate parent                           | `28c86ec6730a82033541b2534b8b0d864b75c9dd` |
| approved Slice B review/base                     | `d49f89cd53163414ecec6ca77d6705ff9a7e84ad` |
| fixed candidate `services/control-plane` subtree | `9aa97fb0a042785da24f9755747f754c01f2dc44` |
| fixed candidate `contracts` subtree              | `f2d7e4d5221e3ecedf0117fead15945e067b4e70` |

Key reviewed file identities are:

| File                                                                                   | SHA-256                                                            |
| -------------------------------------------------------------------------------------- | ------------------------------------------------------------------ |
| `services/control-plane/internal/migration/runner_ledger_entry_success_kernel.go`      | `9a3b7388df68f8cf0967f2a19f9dde45b78aacf47ec7962fe355ea2bf79fa567` |
| `services/control-plane/internal/migration/runner_ledger_entry_success_kernel_test.go` | `b9ef44abf9662bd73351424a76b9d1b7f4b88dd54e2fde08be48e58bc42fbad5` |
| `docs/plan/p1/runner-ledger-entry-success-kernel-service-matrix-20260822.md`           | `0bcc790975e27c0d9096c67a23040e474b1b161a1c57e9c726ba7e79fbe41cac` |

The candidate worktree was clean, its local and remote branch refs were equal, and upstream was `0/0`. The remote
candidate ref resolved to the exact fixed commit. This review did not edit the candidate.

## Review result

Slice C remains a disconnected, one-entry, multi-statement known-success kernel. Its production graph has no caller.
The execution permit and recovery digest/tail provenance remain package-private, and the state, evidence request,
commit observation, and registry-backed authorities remain one-shot and non-copyable. Execution preserves the
reviewed intent, exact SQL, intermediate, final-only preledger, ledger insert/readback, commit-intent, known-commit,
old-session close, terminal, and result-classification order. Unknown, ambiguous, rejected, or post-mutation failures
remain recovery-required; retry, abort, reconcile, and failure writers are absent.

The seventh repair closes the prior cleanup-provenance finding by separating cleanup quorum from successor authority.
Primary and cleanup registry records carry independently cloned full data and sealed cleanup facts. An exact
primary/cleanup cleanup-facts pair may recover only the original session, transaction, lock, evidence, journal, and
cursor handles needed to close or revoke. `registriesValid` additionally requires both cleanup facts to match the
live state, both full records to validate, both authority bindings to agree, and both full-data digests to match.
Consumption then revalidates the live state without the registries before it can mint a successor. Cleanup-only
quorum therefore cannot advance the state machine or produce a success result.

For execution-ready or transaction-ready live-state resource drift, the exact record pair is selected only for
cleanup. The invalid consume revokes the selected record's original shared cursor and returns the record-owned real
handles to the caller, which rolls back the real transaction when present, unlocks, resets, and closes the real
session. A one-sided typed primary or cleanup replacement cannot form the exact pair: the untouched record or the
record that still matches live state supplies the original cleanup provenance. If no trustworthy two-of-three
relationship remains, every independently valid record/state cursor is conservatively revoked and no resource handle
is selected from an untrusted replacement.

The claim selector binds the live state and both records to the original shared atomic claim before compare-and-swap.
Only the CAS winner may load-and-delete both registries and consume state. A real concurrent loser sees the already
consumed original cross-bound claim and returns no data; it cannot revoke the winner's cursor. Every invalid winner
path removes both registries, and restoration of an externally mutated field or record cannot revive the consumed
claim, state, or cursor. Post-commit failures wrap transition errors as recovery-required and revoke the old cursor;
the exact commit-known and terminal-durable tamper matrix covers symmetric typed replacements, state/claim/binding
drift, registry removal/replacement, and restoration after failure. The normal success path preserves its one
successor cursor until terminal classification.

Runtime authority no longer depends on caller-visible `RuntimeBundle` projections. State, primary, and cleanup copies
hold distinct zero-public-field runtime handles, while the validated execution policy, entry count, and runtime-input
canonical digest are frozen into the state digest. Cleanup facts do not depend on the full runtime handle. Historical
generated registries, schemas, fixtures, profiles, SDKs, SQL migrations, and the generation lock remain unchanged from
the approved Slice B base.

## Fresh checks

The independent candidate worktree used Node `24.13.1`, Bun `1.3.14`, Go `1.26.6 darwin/arm64`, and Gitleaks
`8.30.1`. Checks against the fixed candidate produced:

- exact precommit live-resource-drift plus competing post-commit consumer normal: PASS in `15.122s`;
- the same exact two-test scope with `-race`: PASS in `130.735s`;
- exact commit-known/terminal-durable state and registry tamper normal: PASS in `181.939s`;
- `bun run platform:contracts:check`: PASS/current for `115` JSON files, `50` schemas, and `62` fixture cases,
  including every generated registry/profile/SDK and the generation lock;
- control-plane `go vet ./...`, `go build ./...`, `go mod tidy -diff`, and `go mod verify`: PASS;
- all Slice C changed Go files `gofmt`, changed documentation `oxfmt --check`, and candidate-range
  `git diff --check`: PASS; and
- Gitleaks over `d49f89c..9db5891`: PASS, six commits and `225.74 KB`, no leaks.

The candidate matrix records three current-source non-overlapping normal commands that cover all eleven success-kernel
tests and one narrow precommit/concurrency race command. This review independently reran only the exact scopes listed
above. It did not rerun a broad race. The candidate's sole final-source full `internal/migration` normal command
reached its explicit 30-minute timeout (`1800.980s` package time, `1801.29s` wall time) while tests were still running;
that command is `NOT PASS`, is not independent evidence, and no test assertion failure is inferred from the bounded
stop. The earlier intentionally interrupted full run is also `NOT PASS`. Repository-wide formatting remains `NOT
PASS` because of eight unchanged pre-existing files outside this Slice.

## Non-claims

This review did not modify the fixed candidate, merge or open a pull request, connect to a live or production
PostgreSQL instance, write production data, deploy, publish, release, or close a Gate. It does not claim a live
PostgreSQL run, a full-package normal PASS, a broad/full-package race PASS, or a repository-wide formatter PASS.

The verdict approves only fixed candidate `9db5891a7a624b1fae427ba25800ccb270b7e85e` for ADR-0022 Slice C's
disconnected known-success kernel and its reviewed cleanup/one-shot authority boundaries. It does not authorize a
production caller, retry/abort/reconcile/failure writer, HTTP/P2/provider behavior, production database side effect,
deployment, release, merge, or Gate closure. Any later Slice remains subject to its existing fixed-candidate and
independent-review requirements.
