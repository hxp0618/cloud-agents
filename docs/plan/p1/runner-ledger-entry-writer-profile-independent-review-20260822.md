# Runner ledger entry-writer generated-profile independent review — 2026-08-22

- Status: `APPROVE`
- Verdict: `P0=0`, `P1=0`, `P2=0`
- Fixed candidate: `1f1b0c5fca759b874970b79c47b566f0125b7961`
- Candidate branch: `codex/cloud-agents-p1-runner-entry-writer-contract-20260822`
- Review branch: `codex/cloud-agents-p1-runner-entry-writer-profile-independent-review-20260822`
- Gate effect: none; this review does not close or advance any Gate

## Fixed identities

The review resolved the following identities before running checks and rechecked them before creating this record:

| Identity                                         | Value                                      |
| ------------------------------------------------ | ------------------------------------------ |
| fixed candidate commit                           | `1f1b0c5fca759b874970b79c47b566f0125b7961` |
| fixed candidate tree                             | `395d681d5e58f05fa5a02685498feeef3c3b24c5` |
| fixed candidate parent                           | `866c86a2d0f1f31e024338049f1fa4713293b394` |
| fixed candidate `services/control-plane` subtree | `98faa8e18d14c4422e44cd0fa0c17be817a9f140` |
| fixed candidate `contracts` subtree              | `b78363e4957c139bf49f783843a01ec2eb398e1b` |
| fixed candidate `sdk` subtree                    | `619a4531428c05718f421caf23cb4230afe32af4` |

Key reviewed file identities are:

| File                                                                                                              | SHA-256                                                            |
| ----------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------ |
| `contracts/platform/v1alpha1/fixtures/golden/runner-ledger-entry-execution-admission-registry-source-v1.json`     | `88bbb305ced88107407a830b195208c1c02bb1f5bc7a321c2c4a17042b37ecbb` |
| `contracts/platform/v1alpha1/fixtures/golden/runner-ledger-entry-success-writer-registry-source-v1.json`          | `ee114d994062d0f3c6ee9f96a1d962621a5f95f19eba3b21a5de6bfeb1700db9` |
| `contracts/platform/v1alpha1/schemas/runner-ledger-entry-execution-admission-registry-source-v1.schema.json`      | `505fdcd72f113d9156c5549ac0ef02c97c4d9e40286495082753cb98d7ae8d9a` |
| `contracts/platform/v1alpha1/schemas/runner-ledger-entry-execution-admission-registry-v1.schema.json`             | `96eb821ce315b23540146a7e9b77cfbac9b68e12da1367d9f6f054ed61b20d97` |
| `contracts/platform/v1alpha1/schemas/runner-ledger-entry-success-writer-registry-source-v1.schema.json`           | `ee23116e5de2d052f8f25fb2addd8cb98bd055901f34f222aa9561437e5d3274` |
| `contracts/platform/v1alpha1/schemas/runner-ledger-entry-success-writer-registry-v1.schema.json`                  | `2e6f4a49f734983b2e3f57074814c57be3ff7f596e8df35cfb527436b0274beb` |
| `contracts/generated/platform/v1alpha1/runner-ledger-entry-execution-admission-registry-v1.json`                  | `9ef15ce291207580d7bc0426d22d7e4e5a43260a89ea5375c5f8e10e08c0dc96` |
| `contracts/generated/platform/v1alpha1/runner-ledger-entry-success-writer-registry-v1.json`                       | `0025cb5a4f38644848bf5317f37b8b849fc5861f56872ff6c2bd860bd841a5e6` |
| `services/control-plane/internal/migration/runner_ledger_entry_writer_profile.go`                                 | `7e31f673dd4e6650beaaf656e6af79325796f876abb5f8608720ff1576f7e219` |
| `services/control-plane/internal/migration/runner_ledger_entry_writer_profile_generated.go`                       | `63b2e2ac4aec2f02ba9bfc5e90ef716d3659decbbb2ffe716cfe50f189b77c5d` |
| `services/control-plane/internal/migration/runner_ledger_entry_writer_profile_test.go`                            | `9d2236912030850def5065b87fac7956f8bb2883c904d15154d7ae615b7f3fdc` |
| `contracts/generation.lock.json`                                                                                  | `f02ca2cc522cb4fc82bcb2d0461bdbf8e656a42239f448d183ff5d5d1eac5dcf` |
| `docs/plan/adr/0022-p1-runner-ledger-entry-success-writer-contract.md`                                            | `ca97c1cbd4bc4babbc9ca426cd95d325f06cb2fad0ecb244e51d7c2a2ff007a2` |
| `docs/plan/p1/runner-ledger-entry-writer-profile-implementation-20260822.md`                                      | `3fbaae14f3b9293ff33f42a2ad0357f779a662f55c7ba0f4c0755d4c68d964ab` |

The local and remote candidate refs were equal and the candidate branch was clean with upstream `0/0`. The review
also compared the Git blobs for all 16 historical preflight, consumer, and close-only entry-admission v1 artifacts
between the parent and fixed candidate. Every blob was identical; the candidate does not alter the predecessor v1
source fixtures, source/output schemas, generated registries, generated Go profiles, or close-only permit.

## Review result

The execution-admission registry derives its matrix from the immutable five-pair
`runner-ledger-entry-admission/v1` registry and retains exactly four first-attempt pairs: two `empty_brand_new` and
two `partial_next_entry` pairs. The historical
`empty_brand_new / brand_new_inherited / begin_next_attempt` retry pair is excluded. The success-writer selector
maps only `prepare_entry_execution` to `execute_one_entry_known_success`; an unknown action fails closed.

Both source and generated schemas are strict objects. Fixed semantic codes cover binding, state-machine, boundary,
and registry-digest failures. The registries use domain-separated RFC 8785/SHA-256 digests. Execution admission
cross-binds the exact entry-admission registry, state machine, policy, profile ID, and profile digest; the writer
cross-binds the corresponding exact execution-admission identity. Mutation tests reject edited matrices,
selectors, boundaries, state machines, generated registries, and profile facts.

The ordinary Go profiles and generated selectors are package-private facts. Production AST review found the two
selectors called only by profile validation; no runtime consumer or writer calls them. The candidate adds no
`database/sql`, `pgx`, or `net/http` import, database session or transaction opener, `BeginMigration`, SQL execution,
ledger/evidence append, commit, public `Runner` edge, HTTP/P2/provider surface, deployment, or release path. Future
writer states and mutations exist only as generated contract facts; Slice A implements none of them.

`contracts/generation.lock.json` contains three new non-Gate pipelines for execution-admission registry,
success-writer registry, and combined Go-profile generation. Their input manifests bind the source/schema,
predecessor/generated registry, generator/library/tests, ADR/audit, and handwritten Go profile inputs; their output
hashes bind the two registries and generated Go file above. A fresh reconstruction was byte-identical to the
checked-in lock at `118104` bytes. The contract-manifest change is propagated through the Go and TypeScript identity,
JSON, and Proto SDK manifests and generated file headers; all generators reported current.

## Fresh checks

The independent candidate worktree used Node `24.13.1`, Bun `1.3.14`, Go `1.26.6 darwin/arm64`, and Gitleaks
`8.30.1`. Checks run against the fixed candidate produced:

- `bun run platform:contracts:check`: PASS; `115` JSON files, `50` schemas, and `62` fixture cases, with every
  registry/profile/SDK and generation lock current;
- focused contract, semantic, entry-writer registry, and lock tests: PASS, `44/44` tests and `253` assertions;
- repository lint and TypeScript typecheck: PASS;
- focused Go profile normal: PASS in `0.949s`;
- the same focused Go scope with `-race -timeout=30m`: PASS in `2.740s`;
- exact full `internal/migration` normal suite with explicit `-timeout=30m`: independently PASS in `1066.671s`;
- control-plane `go vet ./...`, `go build ./...`, `go mod tidy -diff`, and `go mod verify`: PASS;
- Linux `amd64` and `arm64` migration test-binary compile with `CGO_ENABLED=0`: PASS;
- Go SDK normal and race: PASS;
- TypeScript SDK tests, typecheck, and build: PASS; `20/20` tests;
- candidate-modified Go formatting plus `git diff --check`: PASS;
- direct comparison of all 16 predecessor Git blobs: PASS, byte-identical; and
- Gitleaks over `866c86a..1f1b0c5`: PASS, one commit and `201.51 KB`, no leaks.

An initial contract-check invocation exposed ambient child-process Bun `1.4.0` and Go `1.26.7` and was rejected by
the repository's version guard. It is not counted. The PASS above came after exporting the exact fixed-toolchain
PATH and verifying all three versions. Likewise, cross-platform evidence is compile-only: attempted execution of a
Linux binary on Darwin is not counted. The candidate record's earlier explicit-30-minute full result is distinct
from the independent `1066.671s` PASS above; a default-ten-minute run is not evidence.

## Non-claims

This review did not modify the fixed candidate, merge a branch, use a live or production PostgreSQL instance,
write production data, deploy, publish, release, or close a Gate. It does not implement or authorize the execution
permit consumer, success writer, retry/abort/reconcile/failure or other recovery writers, database transactions,
`BeginMigration`, SQL execution, ledger/evidence append, commit, public `Runner` wiring, HTTP/P2/provider behavior,
or any production side effect.

The verdict approves only fixed candidate `1f1b0c5fca759b874970b79c47b566f0125b7961` for ADR-0022/D-046 Slice A:
the two versioned generated registries, ordinary Go profiles, fixtures, schemas, manifests, SDK manifest closure,
generation lock, historical same-bits assertions, and associated documentation. Any Slice B or later behavior
remains subject to its already ordered implementation and independent review boundaries.
