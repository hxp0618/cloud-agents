# Runner ledger entry execution-admission service independent review — 2026-08-22

- Status: `APPROVE`
- Verdict: `P0=0`, `P1=0`, `P2=0`
- Fixed candidate: `c375fac6ae5a7ffd95e0931dbe384ae213f5087b`
- Candidate branch: `codex/cloud-agents-p1-runner-entry-execution-admission-20260822`
- Review branch: `codex/cloud-agents-p1-runner-entry-execution-admission-independent-review-20260822`
- Gate effect: none; this review does not close or advance any Gate

## Fixed identities

The review resolved these exact identities before checks and again before creating this record:

| Identity                                         | Value                                      |
| ------------------------------------------------ | ------------------------------------------ |
| fixed candidate commit                           | `c375fac6ae5a7ffd95e0931dbe384ae213f5087b` |
| fixed candidate tree                             | `4348b62c0547509758022a86fa6c461e548c241c` |
| fixed candidate parent / Slice A review          | `7615fe59919d61152f7719eacb32e707ad7ba786` |
| approved Slice A candidate                       | `1f1b0c5fca759b874970b79c47b566f0125b7961` |
| fixed candidate `services/control-plane` subtree | `c588855ebccea6e7ec9b9cbd905b40b36fffe241` |
| fixed candidate `contracts` subtree              | `f2d7e4d5221e3ecedf0117fead15945e067b4e70` |

Key reviewed file identities are:

| File                                                                                                | SHA-256                                                            |
| --------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------ |
| `services/control-plane/internal/migration/runner_ledger_entry_execution_admission_claim.go`        | `6ff7da9e236a618aef1d11f3c914798422b5d3e87b1825687da6dc183e88e33d` |
| `services/control-plane/internal/migration/runner_ledger_entry_execution_admission_permit.go`       | `4f8420f9125c4eceae1fc2f86730ea3e3389fe87be845bd3f270fb6aef6a4c9a` |
| `services/control-plane/internal/migration/runner_ledger_consumer_service.go`                       | `e42790cc62d9cce61658ebca9ed6774945d8bd7f973f5fe96a70cecf28d39e5a` |
| `services/control-plane/internal/migration/evidence_session.go`                                     | `caa19c7d0af95809ba3a5eb4863c157ed84755098929e36b91537d764e012fd6` |
| `services/control-plane/internal/migration/runner_ledger_entry_execution_admission_service_test.go` | `b76e4ed59e7b810854dfdc2e04909ca239d9fdf4db33be9caf540c80466d213d` |
| `services/control-plane/internal/migration/runner_ledger_consumer_service_test.go`                  | `d993fbb6463a012d5c75b71744731cf66a11d5da3cac3443052aefe329f5605f` |
| `services/control-plane/internal/migration/runner_ledger_entry_admission_profile_test.go`           | `05fac72738cfd536251192a4aad81c2a87520f8bec87120c461a7c5f74499063` |
| `services/control-plane/internal/migration/runner_ledger_entry_writer_profile_test.go`              | `d30c5ae1773c3cffa45d5328eaa339d5ad97c1199745106bf83454cabad3d0c3` |
| `contracts/generation.lock.json`                                                                    | `69ec2fd2d5d61a41b3568c2fb2e219f258dc962819cbde4e402bc1bc267d949a` |
| `docs/plan/p1/runner-ledger-entry-execution-admission-service-matrix-20260822.md`                   | `6215c107780e96539cff87523c6586aa9aa9d5d021cf57471a883291bd88a30b` |

The candidate branch was clean, its local and remote refs were equal, and upstream was `0/0`. Fifteen key
preflight, consumer, ADR-0021 entry-admission, execution-admission, and success-writer source/generated/profile
artifacts were compared directly with the approved base. Every Git blob was identical. Slice B changes no registry,
schema, fixture, generated Go profile, SDK, SQL migration, or database-function output.

## Review result

Slice B creates a distinct execution-admission claim type, binder interface, registry, terminal use record,
evidence boundary, permit type, and permit registry. It does not consume or reinterpret the ADR-0021 close-only
permit and never calls its close function. The new claim binds the exact same-verifier consumer fact, candidate,
generation, journal schema/recovery, session/journal identity, and all five generated profile identities. Its use
record is keyed by the retained evidence binder, survives failed or completed attempts, and is revoked only by
evidence-session close.

The initial claim deliberately accepts the immutable ADR-0021 five-pair set so failures discovered during complete
read-only classification retain precedence. The exact generated execution selector is applied only after the fresh
database and evidence rereads. It admits the four generated first-attempt pairs. The fifth
`empty_brand_new / brand_new_inherited / begin_next_attempt` retry pair performs the same full read-only sequence,
consumes its evidence claim, closes the exact session, and returns stable `MIGRATION_PROJECTION_NOT_IMPLEMENTED`
without minting a permit. A second attempt is rejected before another database connection.

For an admitted pair, the service opens one fresh dedicated session, validates the connected identity, applies the
signed role/settings, acquires the signed advisory lock, and projects migration-role authority. It reads the ledger
before and after the initial catalog projection, validates the selected entry and full statement-plan closure, then
performs the hardened final sequence: idle role/lock boundary, migration-role authority reprojection with exact
session/canonical equality, ledger/catalog/ledger reread, second idle role/lock boundary, and final same-verifier
evidence consume. Ledger, catalog, authority, session, role/settings, lock, generation, journal, and evidence drift
all fail closed with their reviewed precedence.

The resulting permit binds the consumed evidence boundary, terminal use record, selected entry and plan, exact
ledger prefix, connected and migration authority, catalog projection/contract, database identity, signed advisory
key, generation, and generated execution action. Self/binding pointers plus the registry record reject literals,
copies, field swaps, registry misses, evidence revocation, tampering, and second close. Even on permit tamper or
evidence revocation, cleanup uses the registry-owned exact session/key; unlock/reset/close ambiguity dominates the
prospective `NOT_IMPLEMENTED` result.

Production caller/AST review found only the evidence binder and closed consumer path. The consumer's only successful
permit transition is `close_without_mutation`, after which it returns `MIGRATION_PROJECTION_NOT_IMPLEMENTED` at the
existing public entry boundary. There is no migration/RW transaction, `BeginMigration`, SQL execution, ledger insert,
evidence append, commit, success/recovery writer, public permit method, HTTP/P2/provider surface, deployment, or
release edge.

The generation lock differs from the approved base by exactly two lines: only the input-manifest hashes for the
entry-admission Go profile and entry-writer Go profile pipelines changed because their AST/profile tests gained the
reviewed production callers. No output hash or summary changed. A fresh reconstruction was byte-identical to the
checked-in lock at `118104` bytes.

## Fresh checks

The independent candidate worktree used Node `24.13.1`, Bun `1.3.14`, Go `1.26.6 darwin/arm64`, and Gitleaks
`8.30.1`. Checks against the fixed candidate produced:

- exact focused preflight/consumer/ADR-0021/execution-admission/writer normal: PASS in `57.803s`;
- the same focused scope with `-race -timeout=30m`: PASS in `513.312s`;
- execution-admission and execution-permit focused normal: PASS in `20.749s`;
- `bun run platform:contracts:check`: PASS; `115` JSON files, `50` schemas, and `62` fixture cases, with every
  registry/profile/SDK and generation lock current;
- control-plane `go vet ./...`, `go build ./...`, `go mod verify`, and `go mod tidy -diff`: PASS;
- repository lint and TypeScript typecheck: PASS;
- Linux `amd64` and `arm64` migration test-binary compile with `CGO_ENABLED=0`: PASS;
- all changed Go files `gofmt`, all changed documentation `oxfmt --check`, and `git diff --check`: PASS;
- direct same-bits comparison of 15 key predecessor/generated artifact Git blobs: PASS; and
- Gitleaks over `7615fe5..c375fac`: PASS, one commit and `97.73 KB`, no leaks.

The first contract invocation found no worktree-local `oxfmt` binary and failed while starting the SDK formatter;
it is not counted as current evidence. A fixed Bun `1.3.14` frozen dependency install created only ignored
`node_modules` content, left the Git worktree clean, and the subsequent full contract invocation passed. The
candidate matrix records a separate final-source full normal PASS in `1109.209s`; this review did not rerun that
unbounded scope and does not present the candidate record as independent execution evidence.

## Non-claims

This review did not modify the fixed candidate, merge a branch, connect to a live or production PostgreSQL instance,
write production data, deploy, publish, release, or close a Gate. It does not implement or authorize the success
writer, entry/recovery mutation, retry/abort/reconcile/failure writers, migration/RW transactions, `BeginMigration`,
SQL execution, ledger/evidence append, commit, HTTP/P2/provider behavior, or any production side effect.

The verdict approves only fixed candidate `c375fac6ae5a7ffd95e0931dbe384ae213f5087b` for ADR-0022/D-046 Slice B's
fresh same-verifier execution admission and registry-backed `close_without_mutation` permit. Slice C remains bound
to its separately ordered implementation, matrix, fixed candidate, and independent review requirements.
