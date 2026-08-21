# P1 runner ledger entry writer generated-profile implementation - 2026-08-22

- Status: **SLICE A IMPLEMENTED; FIXED CANDIDATE AND INDEPENDENT REVIEW PENDING**
- Base commit: `866c86a2d0f1f31e024338049f1fa4713293b394`
- Decision: [`ADR-0022`](../adr/0022-p1-runner-ledger-entry-success-writer-contract.md)
- Pre-implementation audit:
  [`runner-ledger-entry-writer-contract-audit-20260822.md`](runner-ledger-entry-writer-contract-audit-20260822.md)
- Scope: generated registries and ordinary Go profiles only

This slice does not consume an execution permit, open a database transaction, execute SQL, append ledger/evidence,
call `Runner.Run`, or implement any entry/recovery writer. It does not add HTTP/P2/provider behavior and does not
authorize production database writes, deployment, publication, release, main merge, or Gate closure.

## 1. Generated identities

The slice adds two independent, versioned identities:

| Profile                                      | Generated registry SHA-256                                         | Registry digest                                                           | Profile digest                                                            |
| -------------------------------------------- | ------------------------------------------------------------------ | ------------------------------------------------------------------------- | ------------------------------------------------------------------------- |
| `runner-ledger-entry-execution-admission/v1` | `9ef15ce291207580d7bc0426d22d7e4e5a43260a89ea5375c5f8e10e08c0dc96` | `sha256:d686c10104929aec568b0b373ed7ef70eea9287594079cf2e9e34ab14510ea18` | `sha256:0799f9485d302b7470e59829a117da9c7b524f546a83a651d81f45b7f236fc68` |
| `runner-ledger-entry-success-writer/v1`      | `0025cb5a4f38644848bf5317f37b8b849fc5861f56872ff6c2bd860bd841a5e6` | `sha256:4af77ed62908c45b0202daa8ef458179b68ae7fc369705a41328330ed19fcbc2` | `sha256:6e83d5661d71ff6bdd030df683f70dfbc3402e7aa9ad34239d81386067b30c54` |

The execution-admission registry binds the exact historical
`runner-ledger-entry-admission/v1` registry/profile identity and admits exactly four first-attempt pairs. The
historical `empty_brand_new / brand_new_inherited / begin_next_attempt` retry pair is intentionally excluded. The
success-writer registry binds the exact generated execution-admission identity and exposes only
`execute_one_entry_known_success`.

The combined generated Go ordinary profile has SHA-256
`63b2e2ac4aec2f02ba9bfc5e90ef716d3659decbbb2ffe716cfe50f189b77c5d`. Static AST tests require both generated
selectors to have profile-validation callers only; there is no runtime consumer in Slice A.

## 2. Generation-lock closure

`contracts/generation.lock.json` records three new non-Gate pipelines:

The complete generated lock file has SHA-256
`f02ca2cc522cb4fc82bcb2d0461bdbf8e656a42239f448d183ff5d5d1eac5dcf`.

| Pipeline                                                      | Input manifest SHA-256                                                    | Output SHA-256                                                            |
| ------------------------------------------------------------- | ------------------------------------------------------------------------- | ------------------------------------------------------------------------- |
| `runner-ledger-entry-execution-admission-registry-generation` | `sha256:2169af4ab4ff7283edd3959bea7372b5cd86cc16e067110b661ff2f7f45ebff8` | `sha256:9ef15ce291207580d7bc0426d22d7e4e5a43260a89ea5375c5f8e10e08c0dc96` |
| `runner-ledger-entry-success-writer-registry-generation`      | `sha256:e580b559a891e92eafc1c311df4a06c558cbc40e6850d5c9f453b0f384569b72` | `sha256:0025cb5a4f38644848bf5317f37b8b849fc5861f56872ff6c2bd860bd841a5e6` |
| `runner-ledger-entry-writer-go-profile-generation`            | `sha256:9a57619a731e7b5ec107a4388d14454a953dc9d4994e33f69946f8be667bb81d` | `sha256:63b2e2ac4aec2f02ba9bfc5e90ef716d3659decbbb2ffe716cfe50f189b77c5d` |

The lock is `notGateClosure: true`; its summaries keep the runtime writer and all recovery writers
`NOT_IMPLEMENTED`, production database writes/deployment/publication `NOT_AUTHORIZED`, and every Gate open.

## 3. Historical same-bits

The TypeScript and Go profile tests hard-bind all 16 preflight/consumer/entry-admission v1 source, schema, generated
registry, generated Go, and close-only permit artifacts. The exact historical bytes remain unchanged. In particular,
the new execution-admission profile does not mutate or reinterpret the five-pair close-only
`runner-ledger-entry-admission/v1` profile.

## 4. Local validation

All successful checks below used Node `24.13.1`, Bun `1.3.14`, and Go `1.26.6` where applicable:

- `bun run platform:contracts:check`: PASS; `115` JSON, `50` schemas, `62` fixture cases; every generator and the
  generation lock current.
- focused Bun contract/semantics/registry/lock tests: PASS; `44/44`, `253` assertions.
- `bun run typecheck` and `bun run lint`: PASS.
- focused migration Go normal and race, plus migration `vet`/`build`: PASS.
- explicit `go test -timeout=30m ./internal/migration -count=1`: PASS in `1076.321s`.
- control-plane full `vet`/`build`, module verify/tidy-diff, and Linux amd64/arm64 migration test-binary
  cross-compile: PASS.
- Go SDK normal and race: PASS.
- TypeScript SDK test/typecheck/build: PASS; `20/20` tests.
- generated-profile forbidden-surface scan: PASS; no runtime selector consumer and no `database/sql`, `pgx`, or
  `net/http` import.
- candidate patch plus untracked-file `gitleaks stdin` scan: PASS; approximately `275.70 KB`, no leaks found. The
  separate whole-tree scan is not counted because it includes baseline fixtures and dependency trees.
- `git diff --check`: to be rerun after the implementation record is finalized.

A mistakenly targeted repository-root `bun run test` is not counted as evidence: Vitest attempted to load
pre-existing `bun:test` suites and the concurrent JSON-generator test exceeded its default five-second timeout. The
correct focused Bun suites and TypeScript SDK suite are listed above. A full migration race suite was not run; only
the focused profile race listed above is claimed.

## 5. Next boundary

Freeze and commit this Slice A candidate, then obtain an independent read-only `P0/P1/P2` review. Only an APPROVE
fixed candidate may begin ADR-0022 Slice B. Slice B may implement the close-without-mutation execution-admission
kernel; it still may not implement the success writer, recovery writers, production database writes, HTTP/P2/provider
effects, deployment, publication, release, main merge, or Gate closure.
