# G-CONTRACT standards R3 independent review — 2026-08-23

## Verdict

`APPROVE — P0=0 / P1=0 / P2=0`

This is an independent, read-only review of the fixed R3 candidate. It approves the candidate as current-source,
non-Gate evidence only. It does not close `G-CONTRACT` or any aggregate Gate and does not authorize production
imports, database writes, HTTP/P2/provider effects, deployment, publication or release.

## Fixed identity

- Candidate branch: `codex/cloud-agents-p1-g-contract-standards-entry-20260823`
- Candidate commit: `242ecef9334b6d76a621b21b97720180950cf9e8`
- Candidate tree: `9f878fe5894b581c8d43fc3693b85bddd3ffaf5f`
- Repair implementation: `e14780dff612f0d5ddf1513b1c0ae3bac6e9149a`
- Repair implementation tree: `ec9eb00cb7ae98161c04823f2fed8897a51b18e6`
- `services/control-plane` subtree: `c1d678f708ec231b446a11e46572a11fccefc97c`
- R3 candidate record:
  `docs/plan/cloud-agents-platform/evidence/G-CONTRACT/CAG-G-CONTRACT-P1-20260823-R3.md`
- R3 record SHA-256: `67268e58174f9c845d2599bac20e99aa2010d52d557c78b3982c13deae5aae85`
- Standards profile SHA-256: `0bd4348680cf48819658651539d4777c412e7a3fb93c380ffacf536c675f440d`
- Generation lock SHA-256: `c1554d88236a11c7612571822fc9294b39a3b447257908739fbdeffc3f044e35`
- Standards wrapper SHA-256: `2594cea982c0fd865cedfd6871872ae3bca69f822c02ec085002464c67673594`

The candidate was clean, its upstream divergence was `0/0`, and the remote branch resolved to the exact candidate
commit at the opening and closing identity checks. R2 remains byte-identical in history with record SHA-256
`ca9754117bdaded4a4d8ea107933725b517934ffc6c5da52607f80d0eeed9909`; R3 explicitly supersedes its
`BLOCK, P0=0/P1=1/P2=1` result rather than rewriting it.

## Finding closure

### Ajv official-suite overclaim

Closed. The current profile, generation lock, lock builder, wrapper and fail-closed tests bind the exact fact
`Ajv 8.20.0 / NOT_RUN_NOT_CLAIMED`. No current profile, lock or executable source claims the former numeric `53`
result. The remaining mentions in R3/current planning text describe the rejected R2 claim as review history, not as
current evidence.

The official-suite `46 files / 383 cases / 1,299 assertions / 79 remotes / zero failures` result is emitted only by
the independent `jsonschema-rs 0.50.1` path. The production Ajv path remains limited to the checked-in current
schema and fixture corpus. A mutation test rejects any attempt to promote the Ajv official-suite status to `PASS`.

### Python source-tree bytecode residue

Closed. Both Python entrypoints in the TypeScript wrapper pass `-B`. The Python test suite recursively rejects any
source `__pycache__` directory or `.pyc` file. The independent combined command started and ended with zero such
paths and a clean worktree.

## Independent checks

The fixed tools used for source-bound checks were Node `24.13.1`, Bun `1.3.14`, Go `1.26.6 darwin/arm64`, Python
`3.14.7`, uv `0.12.5`, oxfmt `0.62.0` and Gitleaks `8.30.1`.

- `CLOUD_AGENTS_CONTRACT_STANDARDS_WHEELHOUSE=/private/tmp/codex-contract-wheelhouse bun run
platform:contracts:standards:check`: PASS. The production/current phase reported `118` JSON files, `52` schemas,
  `2` OpenAPI documents, `3` Proto files, `71` fixture cases and `9` operations. The independent phase reported the
  jsonschema-rs official-suite `46/383/1299/79` result, current fixtures `52/2/71`, OpenAPI `2/9`, zero failures and
  `ALL_GATES_OPEN`. All ten Python tests passed.
- Source-tree bytecode before/after the combined command: PASS, zero `__pycache__` and zero `.pyc`; worktree clean.
- `bun test scripts/lib/platform-contract-lock.test.ts`: PASS, `18/18` tests and `103` assertions.
- `bun scripts/generate-platform-contract-lock.ts --check` under the exact Node/Bun/Go paths: PASS, `current`.
- The first lock-current invocation failed closed because the ambient shell selected Node `26.7.0` and Go `1.26.7`;
  it is recorded as NOT PASS and was not used as evidence.
- R2-to-R3 official corpus, `pyproject.toml` and `uv.lock`: same bits. The combined checker revalidated the fixed
  upstream commit/tree/mandatory-tree, the 126-file corpus manifest and the MIT license digest.
- Target oxfmt, `git diff --check`, and Gitleaks over the two R3 repair commits (`15.32 KB`): PASS; no leak found.
- Static production-import scan: PASS. No service/application/internal package imports jsonschema-rs,
  openapi-spec-validator, the standards profile or the test-only corpus. The control-plane subtree is unchanged.
- Record audit: PASS. R3 honestly records the earlier no-wheelhouse interruption as exit `130` / NOT PASS, retains
  the seven formal `missing` entries, leaves the independent reviewer pending, and leaves every Gate open.

## Non-claims

This review did not run full migration, migration shards, broad race, live PostgreSQL, production database or any
deployment/provider operation. It does not claim Ajv official-suite compliance, current vulnerability closure,
Python/uv executable review, all-platform wheel closure, remaining generator supply-chain closure, an immutable Gate
signature, or any external runtime behavior.
