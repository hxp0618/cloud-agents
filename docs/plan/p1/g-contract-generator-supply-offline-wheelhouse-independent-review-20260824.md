# G-CONTRACT generator-supply offline wheelhouse independent review

Date: 2026-08-24

## Verdict

`APPROVE`

| Severity | Findings |
| -------- | -------: |
| P0       |        0 |
| P1       |        0 |
| P2       |        0 |

This is an independent, fixed-candidate code and behavior review. It approves
only the offline wheelhouse repair. It does not approve the remaining generator
supply-chain criterion and does not close any Gate.

## Fixed lineage and scope

- candidate branch: `codex/cloud-agents-p1-g-contract-wheelhouse-offline-20260824`
- candidate commit: `51ce2d6b9faa71a5e89ccf709864f4d570454a38`
- candidate tree: `4f414c088258b09850f7dd440ac0eafcacb938c1`
- parent: `73ba42cb8d5d17833dd96532b2a527f9ed7250f9`
- candidate diff SHA-256: `d7061c8e8a8c9f33b07c86b3ba566111b9dda7b00e903383a9c7ee5a6f63e917`

The candidate changes exactly these three allowed files:

1. `scripts/check-platform-contract-standards.ts`;
2. `scripts/check-platform-contract-standards.test.ts`;
3. `docs/plan/p1/g-contract-generator-supply-offline-wheelhouse-implementation-20260824.md`.

No closure profile, generation lock, manifest, ADR, Gate record, or status
tracker changes. The review found no HTTP/P2/provider surface, production
database write, deployment, or publication.

## Code review

When `CLOUD_AGENTS_CONTRACT_STANDARDS_WHEELHOUSE` is present, both Python
installation stages receive the same exact environment value as a distinct
argument following `--find-links`:

```text
uv pip install ... --require-hashes --no-build \
  --no-index --find-links <wheelhouse> --requirements -
uv pip sync ... --require-hashes --no-build --strict \
  --no-index --find-links <wheelhouse> -
```

The first stage retains the existing one-package `jsonschema-rs` workaround.
The second, complete requirements sync can no longer fall through to an index.
With the variable absent, `buildUvPipSyncArguments` retains the previous online
argument sequence without `--no-index` or `--find-links`.

The refactor preserves the exact CPython `3.14.7`, uv `0.12.5`, and Bun
`1.3.14` runtime checks, `uv lock --check`, hash-required/no-build/strict
enforcement, temporary venv creation, and recursive `finally` cleanup. The
exported argument constructor is pure. Importing the module in Vitest does not
execute the external pipeline, while direct Bun invocation still enters
`main()` through `import.meta.main`; both direct executions below prove the
real pipeline was not skipped.

## Independent behavior replay

Pinned tools used:

- Node.js `24.13.1`;
- Bun `1.3.14`;
- CPython `3.14.7`;
- uv `0.12.5`;
- Vitest `4.1.10`;
- oxfmt `0.62.0`;
- oxlint `1.77.0`.

The focused argument test passed:

```text
Test Files  1 passed (1)
Tests       2 passed (2)
```

Formatting, lint, and diff checks passed for the candidate scope:

```text
oxfmt --check <three candidate files>
All matched files use the correct format.

oxlint --deny-warnings <two TypeScript files>
exit: 0

git diff --check <parent> <candidate>
exit: 0
```

For the execution-level offline replay, the reviewer exported the exact locked
requirements and independently downloaded 21 compatible wheels into a newly
created temporary macOS arm64 wheelhouse with pip `--require-hashes` and
`--only-binary=:all:`. The fixed candidate then ran directly with
`UV_OFFLINE=1` and that wheelhouse. The observed installation sequence was:

```text
preliminary install: resolved 1 / installed jsonschema-rs==0.50.1
full sync: resolved 21 / installed remaining 20
```

The offline run completed the actual external pipeline:

```text
official suite: 46 files / 383 cases / 1299 assertions / 79 remotes
current contracts: 56 schemas / 2 manifests / 75 fixtures
OpenAPI 3.1: 2 documents / 9 operations
Python unittest: 10 passed
platform-contract-standards: current non-Gate candidate
exit: 0
```

Because uv was explicitly offline, a missing artifact could not have been
fetched from an index. The review worktree intentionally had no local
`node_modules`; its already-pinned dependency directory was supplied through
`NODE_PATH` only so the contract precheck could resolve Ajv. This did not alter
the candidate or the Python wheelhouse behavior.

The reviewer separately invoked the fixed candidate with both wheelhouse and
`UV_OFFLINE` unset. That online-semantics path completed the same contract,
official-suite, OpenAPI, and 10-test checks with all 21 packages installed.
Both runner-created temporary venv roots were confirmed absent after exit. The
reviewer's temporary wheelhouse was moved to the user's Trash after the replay.

## Evidence boundary

This review establishes the repaired behavior on macOS arm64 only. It does not
establish Linux replay, a complete multi-platform generator inventory,
executable byte digests, SBOM/NOTICE completeness, current vulnerability audit,
or the independent evidence needed to satisfy the remaining generator
supply-chain criterion. `ALL_GATES_OPEN` and `notGateClosure: true` remained
explicit in the executed output.

## Commands replayed

```text
git show -s --format='%H%n%P%n%T' 51ce2d6b9faa71a5e89ccf709864f4d570454a38
git diff 73ba42cb8d5d17833dd96532b2a527f9ed7250f9 51ce2d6b9faa71a5e89ccf709864f4d570454a38 | shasum -a 256
git diff --name-status 73ba42cb8d5d17833dd96532b2a527f9ed7250f9 51ce2d6b9faa71a5e89ccf709864f4d570454a38
node <pinned-vitest>/vitest.mjs run scripts/check-platform-contract-standards.test.ts
oxfmt --check <three candidate files>
oxlint --deny-warnings <two TypeScript files>
git diff --check 73ba42cb8d5d17833dd96532b2a527f9ed7250f9 51ce2d6b9faa71a5e89ccf709864f4d570454a38
uv export --project tools/contract-standards --locked --format requirements-txt --no-dev --no-header --no-emit-project
python3 -m pip download --require-hashes --only-binary=:all: <locked requirements>
UV_OFFLINE=1 CLOUD_AGENTS_CONTRACT_STANDARDS_WHEELHOUSE=<temporary-wheelhouse> bun scripts/check-platform-contract-standards.ts
env -u CLOUD_AGENTS_CONTRACT_STANDARDS_WHEELHOUSE -u UV_OFFLINE bun scripts/check-platform-contract-standards.ts
```
