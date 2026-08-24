# Runner ledger entry-loop service matrix — 2026-08-22

- Status: `SLICE_D_FIXED_IMPLEMENTATION_AND_INDEPENDENT_REVIEW_APPROVED`
- Approved Slice C candidate: `9db5891a7a624b1fae427ba25800ccb270b7e85e`
- Slice C independent review: `818c4d5f05e6ea9c86c33c742c7f16292a8c208d` — `APPROVE, P0=0/P1=0/P2=0`
- Slice D base: `818c4d5f05e6ea9c86c33c742c7f16292a8c208d`
- Slice D branch: `codex/cloud-agents-p1-runner-entry-loop-20260822`
- Fixed Slice D candidate: `9fcdb731cc54118b0f86bbfcac593e9129077d1a`
- Fixed Slice D tree: `b5aa8dfd7565e4ef5c1099872bf077043bdab201`
- Fixed control-plane subtree: `c78ffc27c88b0f50871795a281669b7b2ef9bd27`
- Independent review: `351e5eaeca6af4e1f179fb963b36e67df17846bd` — `APPROVE, P0=0/P1=0/P2=0`
- Independent review record: [`runner-ledger-entry-loop-service-independent-review-20260822.md`](runner-ledger-entry-loop-service-independent-review-20260822.md)
- Independent review record SHA-256: `0bf0408849df5310d4c63b652577bf912d62bc882cd42640ae13fbd08a061a02`
- Decision: [`ADR-0022`](../adr/0022-p1-runner-ledger-entry-success-writer-contract.md)
- Gate effect: none; every immutable and aggregate Gate remains open or at its prior phase status

This record covers only ADR-0022 Slice D's typed caller and first-attempt entry loop. It does not add a retry, abort,
commit-reconciliation, terminal-failure, or return-failure writer. It does not authorize a production database run,
HTTP/P2/provider behavior, deployment, publication, release, main merge, or Gate closure.

## Closed entry-loop sequence

The public runner continues to open one verified evidence session. For a wider signed bundle, the generated ledger
consumer now performs this bounded sequence:

1. strict-load the owned runtime bundle and freeze its exact ordered migration list for ordinary result checking;
2. mint and consume one fresh same-verifier preflight claim for the current durable ledger/evidence boundary;
3. preserve the reviewed complete-ledger `return_success` no-op without opening a migration transaction;
4. for only one of the four generated first-attempt pairs, open one new dedicated locked execution-admission session,
   reread the exact authority/ledger/catalog/evidence boundary, and consume one execution permit;
5. invoke the independently reviewed Slice C success kernel for at most one selected entry on that retained session;
6. require the ordinary kernel outcome to match the consumed generated next-entry identity, exact prefix length and
   head, and complete-versus-next-entry classification;
7. only after that exact durable outcome, retire the consumed execution-admission use record by its binder, subject,
   and evidence-boundary identity; and
8. for a non-final entry, discard the ordinary outcome as authority and return to step 2. The next entry therefore
   requires a fresh preflight claim and a distinct fresh locked database session.

The outer loop is bounded by the verified migration count. It accumulates only migration IDs committed during this
call. A bundle already complete returns an empty `Applied`; a partial immediate-next-entry call reports only the newly
committed suffix. Any prefix discontinuity, entry/order mismatch, result-state contradiction, runtime drift, failed
retirement, fresh-preflight failure, or unsupported action fails closed.

## Authority and failure boundary

- The historical preflight, consumer, ADR-0021 admission, execution-admission, and success-writer generated profiles
  remain immutable. Slice D adds no caller-selected profile or ordinary permit conversion.
- The execution-admission use record remains live after permit close, selector rejection, kernel failure, unknown
  mutation outcome, or cleanup failure. Evidence-session close removes it. Only an exact successful committed outcome
  may mark that exact record retired and atomically delete it so the same evidence binder can admit a fresh next entry.
- Retirement rejects a wrong binder, subject, evidence boundary, already-retired record, or registry replacement. A
  displaced expected record is revoked fail closed; a replacement is never treated as the expected authority.
- The fifth generated retry entry pair still completes locked read-only classification and then returns stable
  `MIGRATION_PROJECTION_NOT_IMPLEMENTED`. All eleven recovery/reconcile/failure consumer pairs remain
  `NOT_IMPLEMENTED` and acquire no writer call edge.
- Kernel failure never falls through to a second entry. A successful first entry followed by a failed fresh preflight
  returns an error and performs no second transaction, proving the ordinary first-entry outcome is not reused.
- The historical exact single-entry path remains separate. Slice D does not widen its retry/reconciliation behavior or
  reuse its authority for the generated multi-entry loop.

## Focused conformance matrix

The in-package matrix covers:

- all `17` generated consumer pairs: one complete no-op, four first-attempt entry pairs executed through the success
  kernel, the excluded fifth retry pair, and all eleven recovery pairs;
- a public brand-new two-entry run with two preflight sessions and two distinct execution sessions, exact ordered
  `Applied`, one transaction/ledger insert/commit per execution session, and no transaction on a preflight session;
- a public partial immediate-next-entry run that commits only the signed successor on one fresh execution session;
- a committed first entry followed by a failing fresh successor preflight, with no second execution or bundle success;
- complete-ledger no-op, public unsupported retry/recovery, context/error cleanup precedence, runtime owned-input drift,
  evidence refresh, and exact result identities;
- success-only use retirement with wrong subject/boundary/binder, second retirement, typed registry replacement, and
  kernel-failure retention;
- AST guards allowing exactly one production success-kernel caller, exactly one reviewed retirement caller, and no
  retry/abort/reconcile/failure writer, direct SQL/transaction, HTTP/P2/provider, deployment, or publication edge; and
- all prior Slice C state/evidence/commit/cleanup tests unchanged, including unknown/rejected/post-commit fail-closed
  cursor and resource handling.

## Frozen-source local checks

The current source used Node `24.13.1`, Bun `1.3.14`, Go `1.26.6 darwin/arm64`, and Gitleaks `8.30.1`. It produced:

- one final focused normal scope covering all runner-ledger consumer, public-runner, execution-admission, and
  success-kernel production-graph tests: PASS in `73.729s` package time;
- one final narrow race scope covering the new generated matrix, context/claim cleanup, kernel-failure retention,
  brand-new/partial/fresh-reentry public loops, unsupported pairs, and use retirement: PASS in `304.747s` package time;
- `bun run platform:contracts:check`: PASS/current for `115` JSON files, `50` schemas, and `62` fixture cases; every
  generated registry/profile/SDK and the generation lock is current. An initial invocation without the worktree's
  frozen dependencies stopped before checking because `ajv` was unavailable and is not counted; exact Bun `1.3.14`
  `--frozen-lockfile` installation created only ignored `node_modules`, after which the recorded invocation passed;
- control-plane `go vet ./...`, `go build ./...`, `go mod tidy -diff`, and `go mod verify`: PASS;
- repository lint and TypeScript typecheck: PASS;
- Linux `amd64` and `arm64` migration test-binary compilation with `CGO_ENABLED=0`: PASS; binary SHA-256
  `8ecb99e73f4046a1081c715a20a1aac03cd1f7905e8fa7c2864679077029d855` and
  `7169af07f466879080d1c373819d225c69711505248d745d8e836452b2dece5f`;
- changed Go and documentation target formatting plus `git diff --check`: PASS; and
- candidate patch plus the untracked matrix record Gitleaks stdin scan: PASS, approximately `73.35 KB`, no leaks.

No full `internal/migration` normal or broad race PASS is claimed for Slice D. The already recorded Slice C 30-minute
timeout remains `NOT PASS`; it was not rerun, relabeled, or used as Slice D evidence. The normal and race scopes above
are intentionally bounded to the changed caller/authority surface.

## Generated and external boundary

Slice D changes no source schema, fixture, generated registry/profile, SDK, SQL migration, database function,
dependency, image, workflow, deployment, or release file. The final candidate must prove the historical
preflight/consumer/ADR-0021 admission and both ADR-0022 profiles remain current and byte-identical under the pinned
contract checker. Test-only fake sessions exercise the caller; no live or production PostgreSQL credential or write is
used by this record.

## Independent review closure

The independent read-only reviewer re-resolved the fixed candidate, tree, control-plane subtree, remote ref, clean
worktree, and changed-file hashes; reran the bounded focused normal/race, contracts/current, static, same-bits,
dual-architecture compile, and secret-scan scopes; and returned `APPROVE, P0=0/P1=0/P2=0`. The review branch differs
from the candidate by only the linked review record and is pushed, clean, and upstream `0/0`.

This verdict closes only the ADR-0022 Slice D implementation/review slice. It does not authorize production database
mutation, HTTP/P2/provider behavior, deployment, publication, release, merge to main, or any Gate closure. Recovery
contracts remain a separately generated and separately approved future Slice E.
