# Project instructions
## Dev environment
you can use 'ssh hypers-accer' to connect my dev env

## Start here

- Current execution plan: [04](docs/plan/cloud-agents-platform/04-extraction-and-migration.md).
- Current status and next item: [06](docs/plan/cloud-agents-platform/06-status-tracker.md).
- Authority, clarification and approval rules: [docs/plan/README.md](docs/plan/README.md#source-of-truth).
- Read only the relevant numbered design, contract and source files after those entry points. Do not preload historical plans, old pause/checklists or all evidence.

## Product boundary

Deliver infrastructure and Admin Web together: long-lived Workspace, general Sandbox and customer-node access plus their complete management UI. User CloudAgents conversation is the later application. No-Agent acceptance means no Agent/Provider dependency; it does not remove Admin Web from acceptance. Preserve existing Agent compatibility and keep user content/credentials out of Admin surfaces.

## Execution and evidence

Follow the current request and valid scoped authorization; do not repeat confirmations for routine work already authorized. A documentation task does not itself authorize runtime implementation or production actions. Production writes, publication/deployment, existing-data migration, dirty-worktree deletion and formal Gate closure retain their explicit approval boundaries.

Resolve acceptance by the [versioned task scope in 07](docs/plan/cloud-agents-platform/07-admin-web-requirements-and-design.md#15-实现验收标准): old Admin M1–M4 tasks use ADMIN-WEB-V1; foundation tasks use BASE-READY and BASE-ADMIN-V1. A changing section number never silently expands an existing task or waives its original real-Provider acceptance. Switch scope only on an explicit task-migration instruction, not merely by reading the new plan.

Inspect branch/worktree and dirty state; preserve unrelated work. Check real current code before reimplementing a documented requirement. Do not treat stale “not implemented” or “PAUSED” text as current facts or global instructions. If new permission or a material decision is required, stop only the affected action and continue independently authorized work.

Keep one execution plan (04) and one live status record (06). An explicitly scoped fix, review or verification finishes against that task's scope and affected checks; do not expand it into a whole BASE phase. Declaring an infrastructure capability or phase complete still requires its backend, Admin workflow, relevant checks and honest results. Completing one task does not close the phase; documentation, Mock data and screenshots alone do not prove infrastructure behavior. Use [CONTRIBUTING.md](CONTRIBUTING.md) for checks and the repository's existing contract/generation rules; do not duplicate a source inventory or toolchain version list here.

## Contract and security invariants

Keep [contracts](contracts/README.md) as the editable wire authority and regenerate derived SDKs instead of hand-writing parallel DTOs. Applied SQL changes use new forward migrations; preserve tenant RLS, explicit identities, single-writer/fencing and existing compatibility. Follow [SECURITY.md](SECURITY.md) and the scoped release rules; deleting prose never waives these requirements.

## Implementation simplicity

- Trace the producer and all consumers before changing a shared path. Reuse existing code, generators and standard-library features; fix repeated problems at their source. Do not add speculative compatibility layers, registries, frameworks or parallel evidence systems.
- Keep one editable source for each fact. Derive or generate repetitive version IDs, paths, counts, byte sizes and digests from the appropriate trusted inputs. Do not hand-copy each old version into another `if`/`switch` branch, map, whitelist or test merely to advance the current version. Different version behavior may need explicit branches; different data alone does not.
- For migration bindings, extend the existing [generator](scripts/generate-foundation-migration-package.ts) when it leaves historical bindings manually maintained. Prefer one deterministic generated closed set consumed by selection, admission, current-head lookup and historical ledger validation, not separate handwritten lists. Generated data may retain necessary historical entries; replacing the branch chain with an equally manual map does not fix the maintenance problem.
- Preserve necessary integrity and compatibility checks: known historical ledger digests, exact selector/path matching, frozen SQL/bundle bytes and rejection of unknown versions or tampered artifacts. Do not treat arbitrary files found at runtime as approved versions, compute the expected digest from the same untrusted bytes being checked, or silently fall back to the latest/another version. Remove a check or historical entry only after tracing its consumers and establishing that the supported contract no longer needs it; any separately required approval still applies.
- Change generators/templates and regenerate their outputs; do not patch generated files or use repeated text substitutions as a substitute for fixing generation. Inspect the resulting working-tree code, not combined added/deleted diff lines; remove introduced unreachable statements, duplicate branches and stale parallel definitions.
- For generator/selector changes, reuse the existing checks and affected tests: deterministic regeneration, supported/current and historical selection, unknown/tampered rejection, upgrade/no-op and preservation of historical ledger digests. Run affected Go tests and `go vet` when Go behavior changes. Do not create a new validation framework or turn this into a requirement to run unrelated E2E for documentation edits.
- Apply these rules within the authorized slice. Routine behavior-preserving deduplication needs no repeated confirmation; reducing required security, data retention, compatibility or acceptance does not count as routine simplification. Do not start an unrelated whole-repository rewrite or pause independent authorized work merely because old duplication exists elsewhere.
