# Project instructions
联网前先确认当前网络，并只通过进程级环境变量配置本地代理。代理地址属于本机环境配置，不要写入仓库、文档、日志或证据；缺少代理时保持命令未执行并记录为 `BLOCKED`。

## Dev environment
Use only a separately configured local SSH host alias for development access; do not commit hostnames, credentials, or connection details.

## Start here

- Current execution plan: [04](docs/plan/cloud-agents-platform/04-extraction-and-migration.md).
- Current status and next item: [06](docs/plan/cloud-agents-platform/06-status-tracker.md).
- Authority, clarification and approval rules: [docs/plan/README.md](docs/plan/README.md#规范来源).
- Read only the relevant numbered design, contract and source files after those entry points. Do not preload historical plans, old pause/checklists or all evidence.

## Product boundary

Deliver infrastructure and Admin Web together: long-lived Workspace, general Sandbox and customer-node access plus their complete management UI. User CloudAgents conversation is the later application. No-Agent acceptance means no Agent/Provider dependency; it does not remove Admin Web from acceptance. Preserve current Agent capabilities and keep user content/credentials out of Admin surfaces.

## Execution and evidence

This service is under development. Implement the current contract directly and update its callers together; do not add historical compatibility layers, parallel implementations or old-data migration plans. This does not authorize deleting existing data or removing initialization, persistence, runtime recovery or security checks.

Use this cycle: update the relevant documentation, reproduce changed core behavior with the smallest failing test, implement the minimum, then remove unnecessary code. Reuse existing tests; moves, documentation and behavior-preserving cleanup do not require new tests.

Follow the current request and valid scoped authorization; do not repeat confirmations for routine work already authorized. A documentation task does not itself authorize runtime implementation or production actions. Production writes, publication/deployment, existing-data migration, dirty-worktree deletion and formal Gate closure retain their explicit approval boundaries.

Resolve acceptance by the [versioned task scope in 07](docs/plan/cloud-agents-platform/07-admin-web-requirements-and-design.md#15-实现验收标准): old Admin M1–M4 tasks use ADMIN-WEB-V1; foundation tasks use BASE-READY and BASE-ADMIN-V1. A changing section number never silently expands an existing task or waives its original real-Provider acceptance. Switch scope only on an explicit task-migration instruction, not merely by reading the new plan.

Inspect branch/worktree and dirty state; preserve unrelated work. Check real current code before reimplementing a documented requirement. Do not treat stale “not implemented” or “PAUSED” text as current facts or global instructions. If new permission or a material decision is required, stop only the affected action and continue independently authorized work.

Keep one execution plan (04) and one live status record (06). An explicitly scoped fix, review or verification finishes against that task's scope and affected checks; do not expand it into a whole BASE phase. Declaring an infrastructure capability or phase complete still requires its backend, Admin workflow, relevant checks and honest results. Completing one task does not close the phase; documentation, Mock data and screenshots alone do not prove infrastructure behavior. Use [CONTRIBUTING.md](CONTRIBUTING.md) for checks and the repository's existing contract/generation rules; do not duplicate a source inventory or toolchain version list here.

## Contract and security invariants

Keep [contracts](contracts/README.md) as the editable wire authority and regenerate derived SDKs instead of hand-writing parallel DTOs. Preserve tenant RLS, explicit identities, single-writer/fencing, initialization, persistence and runtime recovery. Existing applied SQL and ledger integrity remain protected; changing the current implementation does not authorize rewriting stored data or frozen evidence. Follow [SECURITY.md](SECURITY.md) and the scoped release rules.

## Implementation simplicity

- Trace the producer and all consumers before changing a shared path. Reuse existing code, generators and standard-library features; fix repeated problems at their source. Do not add speculative compatibility layers, registries, frameworks or parallel evidence systems.
- Keep one editable source for each fact. Derive or generate repetitive version IDs, paths, counts, byte sizes and digests from the appropriate trusted inputs. Do not hand-copy each old version into another `if`/`switch` branch, map, whitelist or test merely to advance the current version. Different version behavior may need explicit branches; different data alone does not.
- For migration bindings, extend the existing [generator](scripts/generate-foundation-migration-package.ts) when it leaves historical bindings manually maintained. Prefer one deterministic generated closed set consumed by selection, admission, current-head lookup and historical ledger validation, not separate handwritten lists. Generated data may retain necessary historical entries; replacing the branch chain with an equally manual map does not fix the maintenance problem.
- Preserve necessary integrity checks: ledger digests used by existing persistence/recovery, exact selector/path matching, frozen SQL/bundle bytes and rejection of unknown versions or tampered artifacts. Do not treat arbitrary files found at runtime as approved versions, compute the expected digest from the same untrusted bytes being checked, or silently fall back to another version. Trace consumers before removing obsolete code; retaining historical implementation compatibility is not itself a requirement.
- Change generators/templates and regenerate their outputs; do not patch generated files or use repeated text substitutions as a substitute for fixing generation. Inspect the resulting working-tree code, not combined added/deleted diff lines; remove introduced unreachable statements, duplicate branches and stale parallel definitions.
- For generator/selector changes, reuse the existing checks and affected tests: deterministic regeneration, supported/current and historical selection, unknown/tampered rejection, upgrade/no-op and preservation of historical ledger digests. Run affected Go tests and `go vet` when Go behavior changes. Do not create a new validation framework or turn this into a requirement to run unrelated E2E for documentation edits.
- Apply these rules within the authorized slice. Routine behavior-preserving deduplication needs no repeated confirmation; reducing required security, data retention or acceptance does not count as routine simplification. Do not start an unrelated whole-repository rewrite or pause independent authorized work merely because old duplication exists elsewhere.

## Tests and verification

- Concentrate tests, fixtures, mocks and helpers in `apps/<app>/test/`, `packages/<package>/test/`, `sdk/<sdk>/test/`, `services/<service>/test/`, root `test/scripts/` and cross-module `test/e2e/`. Go tests requiring unexported package state may remain beside their implementation with a concrete documented exception; do not export APIs, copy source or build a runner merely to move them.
- Keep SDK public-consumer contracts and independent core rules: authorization/isolation, trust boundaries, state transitions, idempotency/concurrency/fencing, resource release, persistence/recovery and real defect regressions. Parameterize similar cases. Do not create tests per file, field, wrapper or coverage quota.
- Use a few complete Web flows with the pinned [tester-army/e2e](https://github.com/tester-army/e2e) tool for ordinary user behavior. Keep explicit assertions and verify backend state where relevant. Remove redundant old coverage only after the replacement runs successfully; missing services or credentials block that flow, not independent code work.
- Record test files and physical lines before and after with the same scope, including E2E, fixtures, mocks, helpers and generated tests. Moves do not count as reduction. Prefer removing duplicated setup and assertions; never delete unique core safeguards to meet a numerical target.
- Start with static inspection. Run only affected checks per slice, and one applicable combined check at completion. Broaden or repeat only for new changes, failures or concrete unresolved risks. No repeated full suites, Provider matrices, packaging or process reports for routine refactoring. Existing CI/release gates run at their delivery boundary; old PASS evidence applies only to its original inputs and scope. Use [CONTRIBUTING.md](CONTRIBUTING.md) for commands.
