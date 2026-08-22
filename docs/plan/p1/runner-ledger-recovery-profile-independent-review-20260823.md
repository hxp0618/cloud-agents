# Runner ledger recovery generated-profile independent review — 2026-08-23

- Status: `APPROVE`
- Verdict: `P0=0`, `P1=0`, `P2=0`
- Fixed candidate: `67210b7f194ec1591c06957ddfc86920a58af167`
- Candidate branch: `codex/cloud-agents-p1-runner-recovery-contracts-20260822`
- Review branch: `codex/cloud-agents-p1-runner-recovery-contracts-independent-review-20260823`
- Gate effect: none; this review does not start Slice B or close or advance any Gate

This is an independent read-only review of the superseding ADR-0023/D-047 Slice A candidate. It
approves only the generated contracts, ordinary package-private Go profile facts, fixtures,
manifests, and generation-lock bindings. It does not approve a claim, permit, consumer, writer,
database action, or caller.

## Fixed identity and scope

| Identity                                         | Value                                                              |
| ------------------------------------------------ | ------------------------------------------------------------------ |
| fixed candidate                                  | `67210b7f194ec1591c06957ddfc86920a58af167`                         |
| fixed candidate tree                             | `5fc0c9eaa4c55d592454f5804835849237227041`                         |
| fixed candidate parent / blocked first candidate | `7f4ab2251efd1aa718e61078bb9efa26d796c92b`                         |
| Slice A base                                     | `999c392ea323ef9d89a65ac5add6ce6b3041023d`                         |
| fixed `services/control-plane` subtree           | `76be62b704866195c8907a79d2dc4b11190a505b`                         |
| registry library SHA-256                         | `1870e0787d9363a1fcb452361bf8892f57860aaf10272bc28d2cb3a18b7f0f56` |
| registry test SHA-256                            | `f6199dadb1762f9596f7945a1d96791a06f23f3015afd7c8a2d625b6b5c27cb3` |
| generated Go profile SHA-256                     | `8489567993aec3edbaa4bfbb4ee82ea5640ca389bd8700f5fc648931bf470c3b` |
| generation lock SHA-256                          | `21d676584a505ad4e4c4198edb202a5cef1454339ab6b4f5e79823be634437df` |
| implementation record SHA-256                    | `d2b27542baffd7e0526ff2250ea47bf50f0e43852b1437a7282fba0ac6785e6d` |

The candidate was clean, its upstream was `0/0`, and the remote candidate branch resolved to the
exact commit. The base-to-candidate scope is exactly 42 files: generated registries, the source
fixture and two schemas, deterministic generators and tests, the ordinary Go profile and generated
facts, contract/SDK manifests and manifest-only SDK comment refreshes, generation lock, package
scripts, and documentation indexes/record. This review did not edit the candidate.

## Closed contract graph

Independent extraction reproduced the exact ordered eight-profile graph and direct pair counts:

| Profile                                         | Direct pairs | Exact predecessor / consumed permit                   |
| ----------------------------------------------- | -----------: | ----------------------------------------------------- |
| `runner-ledger-recovery-admission/v1`           |           12 | immutable `runner-ledger-consumer/v1`                 |
| `runner-ledger-abort-terminal-writer/v1`        |            4 | recovery admission                                    |
| `runner-ledger-commit-observation-writer/v1`    |            1 | recovery admission                                    |
| `runner-ledger-ambiguous-resolution-writer/v1`  |            1 | recovery admission                                    |
| `runner-ledger-retry-handoff/v1`                |            1 | recovery admission                                    |
| `runner-ledger-recovery-execution-admission/v1` |            3 | recovery admission                                    |
| `runner-ledger-recovery-success-writer/v1`      |            0 | exact recovery execution-admission profile and permit |
| `runner-ledger-return-failure/v1`               |            2 | recovery admission                                    |

The common admission mapping contains exactly one `entry_not_implemented` pair and eleven
`recovery_not_implemented` pairs. The twelve disposition/state/action keys are unique and match the
immutable consumer-v1 unsupported rows. Action profiles are exact subsets of that common mapping.

The recovery execution-admission and recovery success-writer have different profile IDs, registry
IDs, registry/profile/state-machine/policy digests, and state machines. The writer has no direct
consumer pair and binds only the exact execution-admission predecessor and permit identity. A
common-admission action, ordinary outcome, wrong profile, or caller literal is not a success-writer
authority.

## Historical and implementation boundaries

Independent blob comparison kept all 24 fixed preflight, consumer, entry-admission,
entry-execution-admission, and first-attempt success-writer v1 source/schema/generated/profile
artifacts byte-identical to the Slice A base. SDK source changes are manifest-comment refreshes from
the enlarged contract inventory; they do not mutate a historical runner identity.

The new Go types, arrays, validation, and selectors are lower-case package-private ordinary facts.
Repository AST/caller scans find no production consumer. The profile and generator sources import
no `database/sql`, `pgx`, or `net/http`, and add no `Runner.Run`, database session/transaction, SQL,
ledger/evidence append, HTTP/P2/provider, deployment, or publication edge. Both recovery pipelines
are `notGateClosure: true` and retain `NONE_IN_SLICE_A`, `NOT_IMPLEMENTED`, `NOT_AUTHORIZED`, and
`ALL_GATES_OPEN` boundaries.

## Superseded formatter P1

The first candidate `7f4ab2251efd1aa718e61078bb9efa26d796c92b` remains blocked because five new
registry files were not `oxfmt`-clean while its record claimed target formatting PASS. No APPROVE
record was created for it.

The superseding candidate closes that P1 without changing semantic registry, profile, state-machine,
or policy digests. Its deterministic serializer emits the repository `printWidth=100` formatter
bytes, and 16 parity assertions check `oxfmt 0.62.0` output for all eight registries.
Independent CLI verification confirms all 34 changed/new JSON, TypeScript, and Markdown targets,
including every new registry, are formatter-clean. The implementation record now explicitly limits
its claim and identifies the two unchanged parent-existing entry registries as nonclaims.

## Fresh checks

The review used Node `24.13.1`, Bun `1.3.14`, Go/gofmt `1.26.6`, and repository `oxfmt 0.62.0`:

- fixed commit/tree/subtree, remote ref, clean state, upstream `0/0`, exact 42-file scope, and key
  SHA-256 values: PASS;
- recovery registry focused tests: `6/6` PASS with `94` assertions;
- contract-lock focused tests: `17/17` PASS with `97` assertions;
- registry and Go generators `--check`: PASS/current;
- `platform:contracts:check`: PASS/current, `118` JSON files, `52` schemas, and `71` fixture cases;
- focused recovery Go profile normal and race: PASS;
- migration-package `vet` and `build`, repository TypeScript typecheck, and changed-TypeScript lint:
  PASS;
- all 34 candidate JSON/TypeScript/Markdown targets under `oxfmt --check`, changed Go files under
  `gofmt -d`, local documentation targets, and `git diff --check`: PASS; and
- independent Gitleaks `8.30.1` over `999c392..67210b7`: PASS, two commits and approximately
  `266.36 KB`, no leaks. The implementation record's approximately `319.85 KB` reflects its local
  scan accounting; no secret result is inferred from byte-count equality.

No full `internal/migration`, full shard suite, broad race, live PostgreSQL, production database,
runtime writer, or external-side-effect test was run or claimed. No timeout or skipped check is a
PASS.

## Non-claims

This review does not modify the fixed candidate, start Slice B, implement or mint a recovery claim
or permit, change any current `MIGRATION_PROJECTION_NOT_IMPLEMENTED` result, or authorize a recovery
writer. It does not merge or open a pull request, connect to a live or production database, execute
SQL, append ledger/evidence, add HTTP/P2/provider behavior, deploy, publish, release, or close a Gate.

The verdict approves only fixed Slice A candidate
`67210b7f194ec1591c06957ddfc86920a58af167` as a deterministic, generated-contract-only closure.
