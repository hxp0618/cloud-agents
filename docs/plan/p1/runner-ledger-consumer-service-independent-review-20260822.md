# Runner ledger consumer service independent review — 2026-08-22

- Status: `APPROVE`
- Verdict: `P0=0`, `P1=0`, `P2=0`
- Fixed candidate: `dcb4b3a706642f88dcf961196c99e4ba1a5aa1e5`
- Candidate branch: `codex/cloud-agents-p1-runner-ledger-consumer-matrix-20260822`
- Review branch: `codex/cloud-agents-p1-runner-ledger-consumer-independent-review-20260822`
- Gate effect: none; this review does not close or advance any Gate

## Fixed identities

The candidate resolved to the following immutable Git identities:

| Identity                                        | Value                                      |
| ----------------------------------------------- | ------------------------------------------ |
| candidate commit                                | `dcb4b3a706642f88dcf961196c99e4ba1a5aa1e5` |
| candidate tree                                  | `0f5e28afa8aa4a4323dfd6b313b3c1938ed76997` |
| candidate parent / fixed Slice B service commit | `1ad8a282eb55b1d3bd04370ee904aa7e3605db8a` |
| fixed Slice B tree                              | `249c0b4b7e0337a5e1cf782139ca7d1dd75993ff` |
| candidate `services/control-plane` subtree      | `876fb20ec5e575f392551f5c2cc115101e06b817` |

Key reviewed file identities are:

| File                                                                                     | SHA-256                                                            |
| ---------------------------------------------------------------------------------------- | ------------------------------------------------------------------ |
| `contracts/generated/platform/v1alpha1/runner-ledger-preflight-registry-v1.json`         | `2a04f67f9b06f25bc13211934d8cb914dcbe3f92d42053f636ec66b4f28ac11c` |
| `services/control-plane/internal/migration/runner_ledger_preflight_profile_generated.go` | `599b78537a3f1dd5d70c1b50aa5e7bc54e1b65ee463876ee3f111709a5ab2112` |
| `contracts/generated/platform/v1alpha1/runner-ledger-consumer-registry-v1.json`          | `fa7082803ea97d06eefa83eec3de784f7199fc0b47f0ca2d0f8203b8b7e96852` |
| `services/control-plane/internal/migration/runner_ledger_consumer_profile_generated.go`  | `afc77e723b7a4439c47043376cb79f5cb6416ce22d54ab1dcffbfe49686ce928` |
| `contracts/generation.lock.json`                                                         | `14a25c66ac5280632ecf6c9821f249adeca8b3193d4f24d3db8621229930211b` |
| `services/control-plane/internal/migration/runner.go`                                    | `3a7906989baf779b7c12ae682dc1640c348e382d2094c10ff3aaff00082a707c` |
| `services/control-plane/internal/migration/runner_ledger_consumer_profile.go`            | `36b0c369b4b9f072808f6b1a8874c3e1c9f58cabbeffddf41619ec42ed410b34` |
| `services/control-plane/internal/migration/runner_ledger_consumer_service.go`            | `679e7a2bee60fd67b19ed98c261da68c9e70e69586d9e1c2e69860b0dedaff01` |
| `services/control-plane/internal/migration/runner_ledger_consumer_service_test.go`       | `c34e018a86e84929c790f5c76902d7a082bb98b2d65b8b50885c08a280eff908` |
| `docs/plan/p1/runner-ledger-consumer-service-matrix-20260822.md`                         | `ab37740c2dcab422ce118ee59b0181aacce5aaab54e9921f6c6b4ab32c11df5e` |

The preflight registry and generated Go profile also have the exact Git blobs
`1dd57eb2c2b512538a34badfee26b50e58d48cc3` and
`96b5a3c55fda5ce95895f8acc688fced5f622004`, respectively, in both the fixed candidate and the pre-consumer source
`ffab704cd26c4d817152602b0854ebb31ee1cc4b`. This establishes byte identity rather than only semantic
equivalence: `runner-ledger-preflight/v1` is unchanged.

## Review result

The generated registry is the sole action table and contains exactly 17 accepted preflight pairs:

- one `complete_return_success / completed / return_success` pair maps to `return_success_noop`;
- five empty or next-entry pairs map to `entry_not_implemented`; and
- eleven retry, recovery, reconcile, or failure pairs map to `recovery_not_implemented`.

The closed consumer obtains a dispatch only by minting and consuming one exact same-verifier
`runnerLedgerPreflightClaim`. It then reloads the verified owned runtime, cross-checks manifest, schema, runner
projection decision, and execution-lineage identities, binds the generated consumer fact inside the same call,
and performs a final context check. The only success branch additionally verifies complete prefix length and head,
return-success dispatch shape, completed recovery shape, and absence of a next entry. It returns ordinary result
data with non-nil empty `Applied` and `AmbiguousRecovered` collections; no authority object escapes.

The entry and recovery branches return exact `MIGRATION_PROJECTION_NOT_IMPLEMENTED` errors at
`runner-ledger-consumer-entry` and `runner-ledger-consumer-recovery`. Unknown pairs fail closed before reaching an
action. Claim bind, consume, second-consume resistance, cancellation, evidence drift, caller-visible bundle drift,
owned runtime-byte drift, and unconditional revocation/registry cleanup are covered by executable tests.

`Runner.Run` has exactly two syntactic calls to the same consumer: the wider verified scope rejected by the current
single-entry writer scope, and an existing single-entry writer preflight that first reports a non-empty or complete
ledger. In the latter case the prior dedicated session is closed before a fresh read-only consumer session is
opened. Evidence/session close failure dominates a prospective no-op result, and `StateComplete` is reached only
after cleanup succeeds.

The consumer service imports only `context` and `errors`. Its AST boundary excludes the migration writer chain,
`BeginMigration`, statement execution, transaction commit, ledger insertion, evidence append, HTTP, pgx, P2,
provider, and state-transition edges. The existing brand-new single-entry writer remains a separate pre-existing
authority chain and receives no consumer fact or dispatch.

`generation.lock.json` accurately records `ONE_CLOSED_NOOP_IN_SLICE_B`, while retaining
`entryWriter: NOT_IMPLEMENTED`, `recoveryWriter: NOT_IMPLEMENTED`, `productionDatabaseWrites: NOT_AUTHORIZED`, and
`gateStatus: ALL_GATES_OPEN`.

## Checks

The independent worktree used Node `24.13.1`, Bun `1.3.14`, Go `1.26.6 darwin/arm64`, and Gitleaks `8.30.1`.
Fresh checks on the fixed candidate produced:

- consumer/public/fault focused normal: PASS in `32.792s`;
- the same focused scope with `-race`: PASS in `299.835s`;
- `go vet ./...`, `go build ./...`, `go mod tidy -diff`, and `go mod verify`: PASS;
- Linux `amd64` and `arm64` migration test compilation with `CGO_ENABLED=0`: PASS;
- platform contract bootstrap: PASS with `106` JSON files, `44` schemas, and `56` fixture cases;
- every platform registry/profile/SDK and generation-lock `--check`: PASS/current;
- contract-lock plus runner-ledger consumer tests: `20/20`, `120` expectations: PASS;
- repository lint and TypeScript typecheck: PASS;
- all candidate-modified TypeScript/JSON and Go files: formatting PASS; `git diff --check`: PASS;
- candidate three-commit Gitleaks scan: PASS, no leaks.

The fixed-candidate gate evidence for the exact full `internal/migration` normal suite was also inspected at
`/tmp/cloud-agents-runner-consumer-final-full-glO8v5/test.json`; its terminal package event is PASS with elapsed
`1413.075s`. This independent review reran the changed focused scope normally and under the race detector instead
of treating that log alone as sufficient evidence.

The fixed-candidate gate evidence also reports PASS for the repository-owned allowlisted four-rule
current-worktree plus full-history secret scan. A redundant independent invocation remained intentionally silent
and unfinished after a bounded ten-minute review window, so it was stopped and is not presented as a second
full-history PASS; the candidate-only Gitleaks result above is fresh independent evidence.

The repository-wide formatter still has the five fixed-parent files already recorded by the candidate. They are
outside this diff and are not represented as a repository-wide format PASS. The separate old-source full race run
is not used by this verdict.

## Non-claims

This review did not modify the fixed candidate, merge a branch, use a live or production PostgreSQL instance,
write production data, deploy, publish, release, or close a Gate. It does not implement or authorize entry
execution, retry, recovery, reconciliation, a new permit, SQL, ledger/evidence append, transaction commit,
HTTP/P2/provider behavior, or any production side effect.

The verdict approves only the fixed candidate's versioned generated profile, read-only complete-ledger no-op
consumer, and closed matrix for continuation under the already approved Slice C process.
