# Runner ledger entry-admission service independent review — 2026-08-22

- Status: `APPROVE`
- Verdict: `P0=0`, `P1=0`, `P2=0`
- Fixed candidate: `88a5392d0538550c8b1478708c0ec79df4f4827d`
- Candidate branch: `codex/cloud-agents-p1-runner-entry-admission-service-20260822`
- Review branch: `codex/cloud-agents-p1-runner-entry-admission-independent-review-20260822`
- Gate effect: none; this review does not close or advance any Gate

## Fixed identities and repaired lock

The review resolved the following exact Git identities before running checks:

| Identity                                         | Value                                      |
| ------------------------------------------------ | ------------------------------------------ |
| fixed candidate commit                           | `88a5392d0538550c8b1478708c0ec79df4f4827d` |
| fixed candidate tree                             | `f9d65d1009d5fdeb829c2641e77b1d5af62aa1e0` |
| fixed candidate parent                           | `cfa94b8be32630a406c78386ff0b06c8850777a2` |
| fixed Slice B code commit                        | `2c34ea70bcbe1cd64f24f879e415078ee6a2bf74` |
| fixed Slice B code tree                          | `e1634f7fe8442cd76cc7dda7cdc7a2949225d33a` |
| Slice A generated-profile commit                 | `8d4d2caf2df192770cc48a9b5959c285b6c3d3a7` |
| fixed candidate `services/control-plane` subtree | `6d9dd4b294474628a1387c8cd14bd9d0540b2d9f` |

Key reviewed file identities are:

| File                                                                                           | SHA-256                                                            |
| ---------------------------------------------------------------------------------------------- | ------------------------------------------------------------------ |
| `contracts/generated/platform/v1alpha1/runner-ledger-entry-admission-registry-v1.json`         | `2dc0210f1aad1dd6cff1183324837ab7e88cc5491e9046ae07302b25a1f9e372` |
| `services/control-plane/internal/migration/runner_ledger_entry_admission_profile_generated.go` | `c95850d99a5cbc9d82480d2c63befd6a39ffaeeb2f2c2f1374ce21091ff806c6` |
| `services/control-plane/internal/migration/runner_ledger_entry_admission_profile.go`           | `43bdd8532e3a380f8f8fe8ceb39e4a00cc96ac66efaa97acbd87c9ca1461db47` |
| `services/control-plane/internal/migration/runner_ledger_entry_admission_claim.go`             | `9e933d5d1016f3d92b1f8640f6f4ca133598765bb2b74793b2e52bd18f174e83` |
| `services/control-plane/internal/migration/runner_ledger_entry_admission_permit.go`            | `255088e37e40d897d76ba589dbf2afd9dbb7dcf3e9d17e6b9d752735f4306714` |
| `services/control-plane/internal/migration/runner_ledger_catalog_preflight.go`                 | `095298748a5b1dd7af224c4048f84571bda091e03c3a9b972de1b7b52ad48b22` |
| `services/control-plane/internal/migration/runner_ledger_consumer_service.go`                  | `738e1c51deeff03ae8b1972eb49f4b744655313edf7043c90f0c2e93be0da125` |
| `services/control-plane/internal/migration/evidence_session.go`                                | `3d3b9ef23fb96c356ae0f99e0100782bbb4bbf10469630c420c04db14bbe852b` |
| `services/control-plane/internal/migration/runner_ledger_entry_admission_service_test.go`      | `b4414d100518c304d5fdce93b7ea14e6924384ec2078b1cda60dcbc38f4b04a2` |
| `contracts/generation.lock.json`                                                               | `12d78d105cc9e97fc2cad51080e3f448571b6d6cc1c1804f695f942187031bc8` |
| `docs/plan/p1/runner-ledger-entry-admission-service-matrix-20260822.md`                        | `e735961ddbcbf01cc745120fb1078bc1e9eadfc53066c82da682451e6891d084` |

The withdrawn parent candidate had a stale entry-admission Go-profile input-manifest hash. The fixed candidate
changes that value from `sha256:31c0e16f6d9f164f3eba1337b53c499e579ff4dd5128abea8106375b790861cb`
to `sha256:8152cd70ae0fb97023570626985ba1cb1da841c6e26b9bcf3c84a3317cf3f410`, binding the modified
`runner_ledger_entry_admission_profile_test.go`. A fresh read-only reconstruction found the checked-in and expected
generation locks byte-identical at `107935` bytes. The earlier P1 therefore does not carry into this fixed
candidate.

The preflight and consumer registries and generated Go profiles have the same Git blobs in this candidate and the
pre-ADR-0021 baseline `115eaf6`: `1dd57eb2c2b512538a34badfee26b50e58d48cc3`,
`96b5a3c55fda5ce95895f8acc688fced5f622004`, `e13710d3c09f5b36ac8ae67bd3d2a9cf74ab661e`, and
`b42da5f76098966100397b2a9a06b01db90dc57d`. The predecessor v1 artifacts are byte-identical.

## Review result

The generated profile is the only selector and contains exactly the five consumer-v1 entry pairs: three
`empty_brand_new` pairs and two `partial_next_entry` pairs. Every accepted pair maps to
`prepare_entry_admission`; complete, retry, recovery, reconcile, failure, and unknown pairs have no generated
entry-admission action.

The same-verifier evidence binder reads the candidate, generation, journal schema, and recovery state under the
existing session-then-journal lock order. Claim binding stores a per-evidence-session use record before exposing a
claim. Registry identity, self/binding pointers, candidate ownership, canonical digest, and atomic consumption
jointly reject literals, ordinary copies, field swaps, registry misses, stale evidence, and second consumption.
The use record remains terminal after consumption or failure and is revoked only when the evidence session closes.

For an admitted fact the service opens a fresh dedicated database session, validates connected authority, applies
the signed role/settings, acquires the signed advisory lock, validates migration-role authority, and constructs
the ledger/catalog observation from freshly rebuilt owned runtime plans. It reads the exact ledger before and
after the first catalog projection, binds the selected next-entry ID/digest and complete statement-plan closure,
then repeats the ledger/catalog projection under the same lock. The final catalog snapshot also revalidates the
migration-role metadata and fixed projection settings. Only after those reads agree does it consume the exact
evidence claim and bind a registry-owned permit.

The permit exposes no methods and has one production caller. It retains only the session and lock needed for
`close_without_mutation`. Its registry record closes the exact retained session even when an ordinary permit copy
or field is tampered. Successful unlock/reset/close is required before the public entry branch returns stable
`MIGRATION_PROJECTION_NOT_IMPLEMENTED` at `runner-ledger-consumer-entry`; cleanup uncertainty, context failure,
authority drift, ledger/catalog contradiction, and evidence drift take precedence. The complete-ledger no-op and
recovery `NOT_IMPLEMENTED` behavior remain outside this permit.

Production AST and caller checks found no entry/recovery writer, migration or read-write transaction,
`BeginMigration`, statement execution, ledger insert, evidence append, HTTP, P2, provider, deployment, publication,
or release edge. The pre-existing brand-new single-entry writer remains a separate authority chain and receives no
entry-admission claim, use record, or permit.

## Fresh checks

The independent worktree used Node `24.13.1`, Bun `1.3.14`, Go `1.26.6 darwin/arm64`, and Gitleaks `8.30.1`.
Checks run against the fixed candidate produced:

- focused entry/preflight/consumer/public normal: PASS in `61.364s`;
- the same focused scope with `-race -timeout=30m`: PASS in `546.064s`;
- exact full `internal/migration` normal suite with explicit `-timeout=30m`: PASS in `1130.314s`;
- `go vet ./...`, `go build ./...`, `go mod tidy -diff`, and `go mod verify`: PASS;
- Linux `amd64` and `arm64` migration test compilation with `CGO_ENABLED=0`: PASS;
- platform contract bootstrap: PASS with `109` JSON files, `46` schemas, and `58` fixture cases;
- every platform registry/profile/SDK and generation-lock `--check`: PASS/current;
- generation-lock actual versus freshly serialized expected: exact byte equality;
- repository lint and TypeScript typecheck: PASS;
- candidate-modified Go, JSON, and matrix-record formatting plus `git diff --check`: PASS; and
- Gitleaks over the fixed three-commit range: PASS, `99.87 KB`, no leaks.

The repository instruction excluding direct `bun test` was preserved; registry/profile conformance was exercised
through the full generator/check path and the Go profile/matrix tests. No skipped direct `bun:test` invocation is
represented as test evidence.

The earlier default-ten-minute full migration invocation is explicitly **NOT PASS**. It is neither a candidate
failure nor evidence for this verdict. The independent PASS above comes from a separate invocation with an
explicit 30-minute ceiling and a verified zero exit status. An initial race invocation was also intentionally
stopped when concurrent full-suite CPU contention approached its default ceiling; only the later standalone,
explicit-30-minute race PASS is claimed.

## Non-claims

This review did not modify the fixed candidate, merge a branch, use a live or production PostgreSQL instance,
write production data, deploy, publish, release, or close a Gate. It does not implement or authorize entry
execution, retry, recovery, reconciliation, migration/RW transactions, `BeginMigration`, migration SQL,
ledger/evidence append, any writer transition, HTTP/P2/provider behavior, or any production side effect.

The verdict approves only fixed candidate `88a5392d0538550c8b1478708c0ec79df4f4827d` for the ADR-0021 generated
five-pair profile, same-verifier fresh-session close-only admission boundary, fault/security matrix, and the
already-authorized Slice C continuation.
