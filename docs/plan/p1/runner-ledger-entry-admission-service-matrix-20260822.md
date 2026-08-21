# Runner ledger entry-admission service matrix — 2026-08-22

- Status: `FIXED_SLICE_B_IMPLEMENTATION_RECORDED_INDEPENDENT_REVIEW_PENDING`
- Slice A generated-profile commit: `8d4d2caf2df192770cc48a9b5959c285b6c3d3a7`
- Slice B service commit: `2c34ea70bcbe1cd64f24f879e415078ee6a2bf74`
- Slice B tree: `e1634f7fe8442cd76cc7dda7cdc7a2949225d33a`
- Slice B control-plane subtree: `6d9dd4b294474628a1387c8cd14bd9d0540b2d9f`
- Matrix branch: `codex/cloud-agents-p1-runner-entry-admission-service-20260822`
- Gate effect: none; every Gate remains open or at its prior phase status

## Frozen behavior

The immutable `runner-ledger-entry-admission/v1` generated profile remains the only selector for this service. It
admits exactly the five existing consumer-v1 entry pairs and maps each to `prepare_entry_admission`. Complete,
retry, recovery, reconcile, failure, unknown, copied, or cross-profile facts cannot enter this state machine.

For an admitted pair, the package-private service performs one closed sequence:

1. bind an exact same-verifier, candidate/generation/journal/recovery-bound evidence claim;
2. open a fresh dedicated PostgreSQL session;
3. validate connected-session authority, signed role/settings, the migration role, and the signed advisory lock;
4. reread and bind the exact ledger rows, prefix length, head, digest, cumulative catalog, next migration entry,
   and complete statement-plan closure;
5. reread the ledger and catalog while the same lock remains held, then revalidate the evidence boundary;
6. consume the evidence claim into one registry-owned use record and mint a registry-backed close-only permit;
7. consume the permit only through `close_without_mutation`, unlock/reset/close the dedicated session, and return
   stable `MIGRATION_PROJECTION_NOT_IMPLEMENTED` at `runner-ledger-consumer-entry`.

The claim, use record, and permit are non-copyable, one-shot, and fail closed on literal construction, field swap,
registry miss, cross-profile input, stale evidence, or second consumption. Once evidence consumption is attempted,
the use record remains terminal for that evidence session even if sealing or cleanup later fails. Evidence-session
close revokes every still-live claim and use record.

The permit exposes no methods and no writer transition. Context, authority, ledger/catalog/evidence drift,
unlock/reset/close failure, and response-lost uncertainty dominate the prospective `NOT_IMPLEMENTED` result.
The pre-existing complete-ledger return-success no-op remains unchanged, while every recovery writer remains on
its prior `NOT_IMPLEMENTED` boundary.

## Immutable identities

The generated registries and profiles remain byte-identical to Slice A and their immutable predecessors:

| Artifact                                | SHA-256                                                            |
| --------------------------------------- | ------------------------------------------------------------------ |
| entry-admission registry v1             | `2dc0210f1aad1dd6cff1183324837ab7e88cc5491e9046ae07302b25a1f9e372` |
| entry-admission generated Go profile v1 | `c95850d99a5cbc9d82480d2c63befd6a39ffaeeb2f2c2f1374ce21091ff806c6` |
| entry-admission runtime profile         | `43bdd8532e3a380f8f8fe8ceb39e4a00cc96ac66efaa97acbd87c9ca1461db47` |
| preflight registry v1                   | `2a04f67f9b06f25bc13211934d8cb914dcbe3f92d42053f636ec66b4f28ac11c` |
| preflight generated Go profile v1       | `599b78537a3f1dd5d70c1b50aa5e7bc54e1b65ee463876ee3f111709a5ab2112` |
| consumer registry v1                    | `fa7082803ea97d06eefa83eec3de784f7199fc0b47f0ca2d0f8203b8b7e96852` |
| consumer generated Go profile v1        | `afc77e723b7a4439c47043376cb79f5cb6416ce22d54ab1dcffbfe49686ce928` |
| generation lock                         | `e3787ed74d83e42f88961b8e2b59d9e5c7fa53041c9ab6e7a489ecabfa088238` |
| evidence-bound entry claim              | `9e933d5d1016f3d92b1f8640f6f4ca133598765bb2b74793b2e52bd18f174e83` |
| close-only entry permit                 | `255088e37e40d897d76ba589dbf2afd9dbb7dcf3e9d17e6b9d752735f4306714` |
| locked ledger/catalog observation       | `095298748a5b1dd7af224c4048f84571bda091e03c3a9b972de1b7b52ad48b22` |
| entry consumer wiring                   | `738e1c51deeff03ae8b1972eb49f4b744655313edf7043c90f0c2e93be0da125` |
| entry-admission fault/security matrix   | `b4414d100518c304d5fdce93b7ea14e6924384ec2078b1cda60dcbc38f4b04a2` |

The generation lock still records `entryWriter: NOT_IMPLEMENTED`, `recoveryWriter: NOT_IMPLEMENTED`,
`productionDatabaseWrites: NOT_AUTHORIZED`, and `gateStatus: ALL_GATES_OPEN`. Slice B changes no generated
contract, registry, schema, fixture, SDK, lock, SQL migration, database function, or public protocol artifact.

## Fault and conformance matrix

The fixed tests cover:

1. all five generated entry pairs and the exact selected entry/statement-plan closure;
2. the other generated consumer outcomes remaining outside entry admission;
3. claim and permit ordinary copies, zero/literal construction, field/registry/use-record mutation, second
   consumption, evidence close, and registry cleanup;
4. cross-profile facts rejected before database authority acquisition;
5. pre-cancel, cancellation after claim binding, cancellation after final evidence consumption, and the rule that
   an earlier ordinary fact cannot be retried;
6. fresh-session connect, session, signed settings, advisory lock, migration-role, metadata, and final-read faults;
7. initial and cumulative catalog projection, ledger-row/head/digest drift, evidence-boundary drift, missing,
   reordered, duplicate, or mutated statement plans;
8. unlock, reset, and close failure precedence, redaction, and response-lost cleanup uncertainty;
9. every public entry result remaining stable `MIGRATION_PROJECTION_NOT_IMPLEMENTED` only after successful permit
   cleanup; and
10. AST-enforced production callers and the absence of transaction, writer, SQL, ledger/evidence append, HTTP,
    P2, provider, deployment, or release edges.

## Reproducible checks

The following checks passed with Node `24.13.1`, Bun `1.3.14`, and Go `1.26.6` unless stated otherwise:

- final focused entry/preflight/consumer/public normal tests: PASS in `40.732s`;
- the same final focused scope with `-race`: PASS in `372.334s`;
- exact full `internal/migration` normal suite with an explicit 30-minute ceiling: PASS in `1066.735s`;
- control-plane `go vet ./...`, `go build ./...`, `go mod tidy -diff`, and `go mod verify`: PASS;
- Linux `amd64` and `arm64` migration test compilation with `CGO_ENABLED=0`: PASS; resulting binary SHA-256
  values were `059d9ec18ec3f4bfd43c650e31e8e7672069279702bd9ae176e1392ece5aa57d` and
  `497af290878be7a9ba92d3c4a7098ef4d8c4b6299d8dca5338e8d3bd119f2808`;
- platform contract bootstrap: `109` JSON files, `46` schemas, and `58` fixture cases: PASS;
- platform generated registry/profile/SDK and generation-lock `--check` commands: PASS/current;
- repository lint and TypeScript typecheck: PASS;
- focused authority/forbidden-surface AST checks, Go formatting, and `git diff --check`: PASS; and
- candidate code commit Gitleaks `8.30.1`: PASS, one commit and approximately `89.77 KB` scanned, no leaks.

The contract, lint, and typecheck checks ran in the clean Slice A dependency worktree at the same exact
`8d4d2caf2df192770cc48a9b5959c285b6c3d3a7` base because this candidate changes only Go migration files and does
not install or alter JavaScript dependencies. The generated artifacts checked there are byte-identical to this
candidate. A direct targeted invocation of the registry test was not separately reported because it imports
`bun:test`, while repository instructions prohibit using `bun test`; the full repository generator/check path is
the claimed evidence.

An initial full migration invocation reached Go's default ten-minute timeout in a pre-existing long-running
authority fault test. That run is explicitly **NOT PASS** and is not treated as a candidate failure. The subsequent
same-snapshot invocation with the explicit 30-minute ceiling is the passing result above.

No live or production PostgreSQL instance was used. Database behavior is exercised only with bounded in-package
session fixtures. This is local conformance evidence, not deployment or production evidence.

## Explicit non-claims

This slice does not implement or authorize entry execution, retry, recovery, reconciliation, migration/RW
transactions, `BeginMigration`, SQL, ledger/evidence append, any writer transition, production database writes,
HTTP/P2/provider surfaces, deployment, publication, release, or Gate closure. It does not change production state
or publish an artifact.

Independent review must re-resolve the fixed candidate, verify the exact source/generated identities and cleanup
boundary, and issue a P0/P1/P2 verdict before this record can move beyond `INDEPENDENT_REVIEW_PENDING`.
