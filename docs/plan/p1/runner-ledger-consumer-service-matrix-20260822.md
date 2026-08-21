# Runner ledger consumer service matrix — 2026-08-22

- Status: `FIXED_SLICE_B_IMPLEMENTATION_RECORDED_INDEPENDENT_REVIEW_PENDING`
- Slice A generated-profile commit: `cc8774cc9b243d2fa58f1647eb0382cf5152a93b`
- Slice B service commit: `1ad8a282eb55b1d3bd04370ee904aa7e3605db8a`
- Slice B tree: `249c0b4b7e0337a5e1cf782139ca7d1dd75993ff`
- Slice B control-plane subtree: `876fb20ec5e575f392551f5c2cc115101e06b817`
- Matrix branch: `codex/cloud-agents-p1-runner-ledger-consumer-matrix-20260822`
- Gate effect: none; every Gate remains open or at its prior phase status

## Frozen behavior

`Runner.Run` has one closed, read-only consumer path for the generated
`runner-ledger-consumer/v1` profile. The consumer can obtain its ordinary dispatch only by minting and consuming
one exact same-verifier `runnerLedgerPreflightClaim` inside the same package-private call.

The generated 17-pair matrix remains closed:

- the single complete-ledger `completed` / `return_success` pair returns a no-op success with the exact verified
  schema-bundle digest, manifest digest, final ledger head, and non-nil empty `Applied` and
  `AmbiguousRecovered` collections;
- five entry pairs return `MIGRATION_PROJECTION_NOT_IMPLEMENTED` at
  `runner-ledger-consumer-entry`; and
- eleven retry, recovery, reconcile, or failure pairs return `MIGRATION_PROJECTION_NOT_IMPLEMENTED` at
  `runner-ledger-consumer-recovery`.

The public runner has two reviewed entry points into that same consumer: a wider verified runtime rejected by the
current single-entry writer scope, and a single-entry writer preflight that first closes its dedicated database
session after observing a non-empty or complete ledger. The consumer opens a fresh dedicated read-only session,
performs the locked ledger/catalog projection, unlocks and closes it, consumes the evidence-bound claim, and only
then returns ordinary result data.

No consumer branch calls `BeginMigration`, executes SQL, inserts a ledger row, appends evidence, commits a
transaction, or consumes any existing writer authority. The pre-existing brand-new single-entry writer stays on
its prior independent authority chain.

## Immutable identities

The two `runner-ledger-preflight/v1` generated outputs are byte-identical to Slice A:

| Artifact                              | SHA-256                                                            |
| ------------------------------------- | ------------------------------------------------------------------ |
| preflight registry v1                 | `2a04f67f9b06f25bc13211934d8cb914dcbe3f92d42053f636ec66b4f28ac11c` |
| preflight generated Go profile v1     | `599b78537a3f1dd5d70c1b50aa5e7bc54e1b65ee463876ee3f111709a5ab2112` |
| consumer generated registry v1        | `fa7082803ea97d06eefa83eec3de784f7199fc0b47f0ca2d0f8203b8b7e96852` |
| consumer generated Go profile v1      | `afc77e723b7a4439c47043376cb79f5cb6416ce22d54ab1dcffbfe49686ce928` |
| generation lock                       | `14a25c66ac5280632ecf6c9821f249adeca8b3193d4f24d3db8621229930211b` |
| closed consumer service               | `679e7a2bee60fd67b19ed98c261da68c9e70e69586d9e1c2e69860b0dedaff01` |
| consumer matrix and public-path tests | `c34e018a86e84929c790f5c76902d7a082bb98b2d65b8b50885c08a280eff908` |

The generation-lock summary now records exactly one closed no-op production consumer in Slice B while preserving
`entryWriter: NOT_IMPLEMENTED`, `recoveryWriter: NOT_IMPLEMENTED`, `productionDatabaseWrites: NOT_AUTHORIZED`,
and `gateStatus: ALL_GATES_OPEN`.

## Fault and conformance matrix

The fixed tests cover:

1. all `17 = 1 + 5 + 11` generated disposition/recovery pairs;
2. exact claim bind/consume counts, second-consume resistance, registry cleanup, and zero live claim after every
   result;
3. pre-cancel, expired deadline, cancellation after claim binding, cancellation after consumption, and consume
   failure precedence;
4. caller-visible bundle drift, owned runtime-byte drift, candidate/evidence cross-binding, and exact generated
   fact validation;
5. complete no-op result identities and evidence-close response-lost dominance;
6. public wider-runtime complete no-op, public single-entry complete-ledger fallback through two distinct read-only
   sessions, and public entry/recovery `NOT_IMPLEMENTED` results;
7. zero transaction, SQL, ledger insert, transaction commit, writer-evidence bind, and evidence append calls for
   every consumer result; and
8. AST-enforced single production caller boundaries plus forbidden database, writer, HTTP, P2, and provider edges.

## Reproducible checks

The following checks passed with Node `24.13.1`, Bun `1.3.14`, and Go `1.26.6` unless stated otherwise:

- exact full `internal/migration` normal suite: PASS in `1413.075s`;
- consumer/public/fault focused normal: PASS;
- the same focused scope with `-race`: PASS in `329.633s`;
- control-plane `go vet ./...`, `go build ./...`, `go mod tidy -diff`, and `go mod verify`: PASS;
- Linux `amd64` and `arm64` migration test compilation with `CGO_ENABLED=0`: PASS;
- platform contract bootstrap: `106` JSON files, `44` schemas, and `56` fixture cases: PASS;
- all generated platform registry/profile/SDK and generation-lock `--check` commands: PASS;
- contract-lock plus runner-ledger consumer tests: `20/20`, `120` expectations: PASS;
- repository lint and TypeScript typecheck: PASS;
- package tests plus the scripts suite: PASS; the scripts suite reported `171/171`;
- candidate-only and full-history secret scans: PASS;
- modified TypeScript/JSON formatting, Go formatting, and `git diff --check`: PASS.

The repository-wide formatter still reports exactly five pre-existing fixed-parent files outside this candidate.
Those files were not modified, and that run is **not** reported as a repository-wide formatting PASS. A separate
old-source full migration race run is also not part of this candidate's claimed evidence.

No live or production PostgreSQL instance was used. The matrix uses bounded in-package fixtures and proves only
the local read-only service contract; it is not deployment or production evidence.

## Explicit non-claims

This slice does not implement or authorize entry execution, retry, recovery, reconciliation, a new execution
permit, SQL, ledger/evidence append, any writer path, production database writes, HTTP/P2/provider surfaces,
deployment, publication, release, or Gate closure. It does not publish an artifact or change production state.

Independent review must re-resolve the fixed matrix candidate, verify the exact source and generated identities,
and issue a P0/P1/P2 verdict before this record can move beyond `INDEPENDENT_REVIEW_PENDING`.
