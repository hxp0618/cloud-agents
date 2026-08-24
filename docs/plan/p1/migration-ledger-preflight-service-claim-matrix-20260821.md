# P1 runner ledger preflight service/claim matrix - 2026-08-21

- Status: **IMPLEMENTED — fixed-candidate independent review pending**
- Slice A/B fixed candidate: `01b1a5f5d5c68776a69c805839a1e333191e29bf`
- Slice A/B review record: `f871cf334ee9b862c10c63f38567f22764873b3c`
- Slice C base: `f871cf334ee9b862c10c63f38567f22764873b3c`
- Slice C branch: `codex/cloud-agents-p1-ledger-preflight-service-claim-20260821`
- Scope: package-private typed recovery/no-op claim and dispatch only

This record implements Slice C from [ADR-0019](../adr/0019-p1-runner-ledger-preflight-contract.md) and the
[entry blocker](./migration-ledger-preflight-entry-blocker-20260821.md). It is local conformance evidence, not an
immutable Gate signature. It does not close `G-CONTRACT`, `G-DATA`, `G-AUTHORITY-P1`, `G-SECURITY-P1`,
`G-SUPPLY-CHAIN`, or any aggregate Gate.

## 1. Closed service and claim boundary

The package-private service is the only production caller of the Slice B read-only projection kernel. It combines
that projection with a sealed same-verifier evidence binder and mints one live claim for one concrete evidence
session. Claim consumption revalidates the current candidate, generation, schema witness, recovery snapshot,
session identity, journal identity, and projection identity before returning ordinary closed dispatch data.

The dispatch has exactly three shapes:

| Ledger/recovery disposition | Closed dispatch         | Mutation authority |
| --------------------------- | ----------------------- | ------------------ |
| empty brand-new             | exact entry             | none               |
| partial next entry          | exact entry             | none               |
| partial retry/recovery      | recovery/reconcile      | none               |
| complete                    | explicit return-success | none               |

The generated registry remains the sole source for the five dispositions and 17 allowed state/action pairs. There
is no caller-selected profile or handwritten fallback. The service cross-binds database ledger rows to the durable
evidence prefix, derives the next-entry digest from the exact signed `LedgerRow`, and reconstructs the final
`CatalogStateProjection` before accepting the complete no-op path.

Claims are sealed by self, binding, candidate, generation, projection, schema, recovery, session, journal, registry,
and atomic one-shot identity. Copy, literal, cross-bundle, stale, drifted, concurrently reused, and post-close claims
fail closed. Closing the concrete evidence session revokes its live claim. The ordinary dispatch carries no database
session, transaction, evidence lease, receipt, verifier artifact, writer token, publication authority, or mutation
port.

## 2. Matrix and fault coverage

Focused normal and race tests cover:

- all 17 generated disposition/recovery/action pairs;
- PostgreSQL 15, 16, and 17 metadata across empty, partial-next, partial-recovery, and complete shapes;
- exact entry, recovery, and return-success dispatch validation;
- candidate, schema, recovery, session, journal, ledger, catalog, and cross-bundle drift;
- copy, literal, stale, reused, close-revoked, and concurrent one-shot claims;
- advisory-lock failure and close response-lost precedence before claim minting;
- corrupt versus recovery-required precedence;
- a compile-time concrete `generationEvidenceSession` binder assertion and concrete literal rejection;
- zero migration transaction, ledger insert, writer execution, evidence append, commit, or external-effect calls;
- AST/reflection checks for the exact consumer graph and forbidden authority surfaces.

The successful concrete `generationEvidenceSession` path still requires an authentic sealed
`evidencefs.GenerationLease`. The repository has no production trusted-mount constructor that can be invoked from
migration tests, and the evidencefs fake backend is intentionally package-private. This slice does not add an
exported test constructor, unsafe bridge, reflection mutation, or authority backdoor. The concrete binder is covered
by its sealed interface, exact production call graph, fail-closed literal path, and the existing evidence-session and
evidencefs lifecycle tests; a full successful concrete integration remains deferred until trusted provisioning is
implemented.

## 3. Fresh verification

The following checks passed with the exact declared Go `1.26.6` toolchain:

- focused service/kernel/profile tests: `14.245s`;
- the same focused selection with `-race`: `126.228s`;
- `go vet ./...`;
- `go build ./...`.

The exact declared Node `24.13.1`, Bun `1.3.14`, and Go `1.26.6` tuple also passed:

- runner ledger-preflight registry generator `--check`;
- runner ledger-preflight Go generator `--check`;
- platform contract lock `--check` after the derived lock refresh.

The derived contract-lock diff changes only three manifest hashes. Generated registry/profile output bytes and the
five-disposition/17-pair contract remain unchanged. The current lock SHA-256 is
`7708b422373649ea9afe207e5e99bb55a46395682eaedef81c89234e3ff7515e`.

The broad `go test ./internal/migration -count=1` run is **NOT PASS**: it was stopped after a bounded five-minute
silent run, with no test failure conclusion. Focused normal/race, vet, and build results do not replace that missing
broad-suite result.

Reviewed implementation SHA-256 values before the independent-review freeze are:

| File                                      | SHA-256                                                            |
| ----------------------------------------- | ------------------------------------------------------------------ |
| `evidence_session.go`                     | `401cd6edb4eab9be2e29f327137ba3314da7fa8f2f4124bc8ceb79e383518b31` |
| `runner_ledger_catalog_preflight.go`      | `638c499745089ff3ffd106445a9f3396301512622bf4c81c46db002ce9bbe004` |
| `runner_ledger_catalog_preflight_test.go` | `d2a4eae87a75b90ac1f70288639237e37cb1e54d2428733549365dbbbb588f33` |
| `runner_ledger_preflight_profile.go`      | `1d5349594512a7414edf9d1e76e6ad2d05c96b88d5f662c549ea301a20038efd` |
| `runner_ledger_preflight_profile_test.go` | `3c1291d31c9532a655e0fde25e0fe251fec4ea4a5d75c8d1ac33865894ef12a4` |
| `runner_ledger_preflight_service.go`      | `a6337d848828853d94251201bbbcc6b25c7719d33ecb29b0a89b2db7347a966a` |
| `runner_ledger_preflight_service_test.go` | `b9358da4499da9d332e9d00c081cc8022e54f9083f714be6e18e1df8a405976f` |

## 4. Explicit non-claims

This slice does not wire the service or dispatch into `Runner.Run` or any writer. Existing partial and complete
runner paths remain `MIGRATION_PROJECTION_NOT_IMPLEMENTED`. No migration/RW transaction, ledger mutation, evidence
mutation, production database write, HTTP route, P2 surface, provider/worker/session/turn/execution side effect,
deployment, release, publication, merge to main, RC, Beta, GA, or Gate closure is authorized or claimed.

Slice C remains incomplete until a fixed candidate receives its own independent read-only P0/P1/P2 review.
