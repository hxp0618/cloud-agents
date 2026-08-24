# G-CONTRACT generator-supply offline wheelhouse implementation

Date: 2026-08-24

## Scope and lineage

- fixed parent: `73ba42cb8d5d17833dd96532b2a527f9ed7250f9`
- branch: `codex/cloud-agents-p1-g-contract-wheelhouse-offline-20260824`
- implementation scope: the Python contract-standards runner's wheelhouse mode

This slice does not modify a closure profile, generation lock, contract
manifest, ADR, Gate record, or status tracker. It does not claim that the
remaining generator supply-chain criterion is closed.

## Repair

When `CLOUD_AGENTS_CONTRACT_STANDARDS_WHEELHOUSE` is set, the runner now passes
the exact wheelhouse to the full requirements sync:

```text
uv pip sync --python <temporary-venv-python> \
  --require-hashes --no-build --strict \
  --no-index --find-links <wheelhouse> -
```

The existing preliminary `jsonschema-rs` wheelhouse installation remains in
place as the compatibility workaround. The subsequent full sync can therefore
no longer fall back to an index while wheelhouse mode is selected. When the
environment variable is absent, the full sync arguments retain their previous
online form without `--no-index` or `--find-links`.

The pure `buildUvPipSyncArguments` constructor is imported by focused Vitest
coverage without running the external standards pipeline. Moving the executable
body behind `import.meta.main` preserves direct Bun execution while providing
that test seam.

The existing pins and enforcement remain unchanged:

- CPython `3.14.7`;
- uv `0.12.5`;
- Bun `1.3.14`;
- `--require-hashes`, `--no-build`, and `--strict`;
- temporary venv creation and recursive cleanup;
- the preliminary `jsonschema-rs` installation workaround.

## Focused verification

The argument tests passed both the offline positive case and the unset negative
case:

```text
node <pinned-vitest>/vitest.mjs run scripts/check-platform-contract-standards.test.ts
Test Files  1 passed (1)
Tests       2 passed (2)
```

The normal online path also completed under the pinned toolchain:

```text
bun scripts/check-platform-contract-standards.ts
official suite: 46 files / 383 cases / 1299 assertions / 79 remotes
current contracts: 56 schemas / 2 manifests / 75 fixtures
Python unittest: 10 passed
platform-contract-standards: current non-Gate candidate
```

For an execution-level offline check, the exact locked macOS arm64 wheels were
downloaded to an ephemeral wheelhouse first. The repaired runner was then
executed with both `UV_OFFLINE=1` and
`CLOUD_AGENTS_CONTRACT_STANDARDS_WHEELHOUSE=<ephemeral-wheelhouse>`. Both the
preliminary one-package install and the full 21-package sync succeeded solely
from that wheelhouse, followed by the same official-suite, current-contract,
OpenAPI, and 10-test pass. The ephemeral wheelhouse was moved to the user's
Trash after the check.

This verifies the runner behavior on macOS arm64 only. It does not establish
the full multi-platform generator inventory, executable byte digests, SBOM,
NOTICE, vulnerability audit, Linux replay, independent review, or Gate closure.
