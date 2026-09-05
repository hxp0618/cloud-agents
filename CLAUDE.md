# Project instructions

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
