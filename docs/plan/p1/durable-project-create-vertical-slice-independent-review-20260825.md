# P1 durable Project create vertical slice independent review — 2026-08-25

- Verdict: **REQUEST_CHANGES**
- Findings: **P0=0 / P1=2 / P2=1**
- Reviewed commit: `a6142a0c06cbd3d77405afa22e035a8fe69ad533`
- Reviewed tree: `ee43ff679e3ed9379dfb52c3cdf7b41afb30670c`
- Implementation commit: `a76b475079d7907ed99b6a5f343187b3dde6c4c4`
- Integration merge: `05fa73686ed3689f35cdd37a74b3b3ef10abb996`
- Immutable predecessor: `fef71b7de45fe7fd36c724ad3d58aaec974818de`
- Branch: `codex/cloud-agents-platform-p0`
- Gate effect: **none**

This is one bounded, independent, read-only review of the already implemented
durable Project-create vertical slice. It does not modify the candidate,
re-run its tests, start PostgreSQL, use SSH, authorize an external effect, or
start a successor implementation.

## 1. Candidate and predecessor boundary

The reviewed tree is the clean P0 integration branch at the exact commit above;
its configured upstream was `origin/codex/cloud-agents-platform-p0` at `+0/-0`.
The implementation delta is `fef71b7..a76b475`, integrated by `05fa736`, with
the milestone record added by `a6142a0`. The historical v1 claim-only profile,
the v1 generated registry, and migrations `000001` through `000012` are
byte-identical across the predecessor and implementation commit.

The separate portable-runtime worktree was inventoried read-only and was not
modified. No review worktree was created.

## 2. Commands actually executed

Only Git metadata/diff and static source inspection commands were used:

```sh
git status --porcelain=v2 --branch
git rev-parse --abbrev-ref HEAD
git rev-parse HEAD
git rev-parse a6142a0^{tree}
git branch -vv --no-color
git remote -v
git worktree list --porcelain
git -C /Users/huang/devel/project/huang/business/cloud-agents-portable-runtime status --porcelain=v2 --branch
git show --format=fuller --stat --summary a76b475079d7907ed99b6a5f343187b3dde6c4c4
git show --format=fuller --stat --summary 05fa73686ed3689f35cdd37a74b3b3ef10abb996
git diff --name-status fef71b7de45fe7fd36c724ad3d58aaec974818de a76b475079d7907ed99b6a5f343187b3dde6c4c4 -- \
  contracts/generated/platform/v1alpha1/durable-coordination-registry.json \
  contracts/platform/v1alpha1/fixtures/golden/durable-coordination-profile-managed-agent-create-project-v1alpha1.json \
  services/control-plane/migrations/000001_expand_migration_kernel.sql \
  services/control-plane/migrations/000002_expand_tenancy.sql \
  services/control-plane/migrations/000003_expand_membership_rbac.sql \
  services/control-plane/migrations/000004_expand_membership_rbac_mutations.sql \
  services/control-plane/migrations/000005_close_membership_binding_authority.sql \
  services/control-plane/migrations/000006_close_subject_issuer_validation.sql \
  services/control-plane/migrations/000007_expand_durable_coordination_kernel.sql \
  services/control-plane/migrations/000008_add_durable_coordination_service.sql \
  services/control-plane/migrations/000009_redact_coordination_conflicts.sql \
  services/control-plane/migrations/000010_expand_compatibility_recovery_kernel.sql \
  services/control-plane/migrations/000011_add_compatibility_recovery_writer.sql \
  services/control-plane/migrations/000012_fix_compatibility_recovery_preflight.sql
rg -n "durable-coordination-(profile-managed-agent-create-project-durable|registry-source-v2|registry-v2)|durable-project-create-(route-v2|idempotency-projection)|000013_add_durable_project_create_writer" contracts/platform/v1alpha1/fixtures/manifest.json scripts/lib/platform-contracts.ts scripts/lib/platform-contract-lock.ts contracts/generation.lock.json services/control-plane/migrations/manifest.json services/control-plane/migrations/schema-bundle.json
nl -ba contracts/platform/v1alpha1/fixtures/manifest.json | sed -n '165,208p'
nl -ba scripts/lib/platform-contract-lock.ts | sed -n '313,374p;620,630p;741,750p;1241,1291p;2765,2777p'
nl -ba services/control-plane/migrations/000013_add_durable_project_create_writer.sql | sed -n '168,231p;233,406p'
nl -ba docs/plan/adr/0013-p1-durable-coordination-contract.md | sed -n '44,93p'
```

No test command, generator command, database command, Docker/Compose/Helm
command, network retry, or SSH command was run during this review.

## 3. Reused evidence

The review reuses, without re-execution, the PASS evidence recorded in
`durable-project-create-vertical-slice-20260825.md`: the two v2 registry/Go
generator checks, migration-bundle check, focused coordination/store/server/
authn/localmigration/data-recovery-validator checks, localdev-tag checks,
focused vet/race checks, and the one disposable local PostgreSQL smoke. That
smoke covers first create, same-key/same-request replay,
same-key/different-request conflict, tenant isolation, rollback without partial
success, and duplicate-row checks. This evidence remains implementation
evidence; this review does not promote it to aggregate Gate evidence.

## 4. Static review results

The following parts are supported by the reviewed source and reused evidence:

- the new `managedAgentCreateProjectDurable/v1alpha1` selector is separate from
  the historical claim-only profile;
- `000013` is a forward, append-only migration and does not edit `000001`–`000012`;
- one SECURITY DEFINER transaction performs idempotency acquisition/replay or
  conflict handling and writes Project, revision, operation, attempt, receipt,
  finalizer, audit, pending outbox, and terminal idempotency state atomically;
- tenant authority, runtime-principal checks, verified RBAC/service binding,
  RLS tables, rollback semantics, and same-key replay paths are fail-closed at
  the inspected boundary; and
- the route is build-tagged localdev/loopback-only, while provider delivery,
  public/non-loopback HTTP, production trust, and production database writes
  remain outside this slice.

The candidate nevertheless cannot be approved because its successor authority
and supply closure are incomplete.

## 5. Findings

### P1-001 — Durable v2 authority bypasses the canonical fixture and generation lock

The canonical fixture manifest lists only the v1 profile/source/registry at
`contracts/platform/v1alpha1/fixtures/manifest.json:173-207`; its checked-in
TypeScript mirror likewise stops at the v1 durable cases at
`scripts/lib/platform-contracts.ts:174-207`. The v2 generator reads its own
source/profile/route and predecessor output at
`scripts/generate-platform-durable-coordination-registry-v2.ts:8-15` and only
self-checks its output at lines `102-116`.

The central lock still defines only the v1 durable generator sources and Go
output at `scripts/lib/platform-contract-lock.ts:362-374`, registers only the
v1 generator pair at lines `620-629`, asserts only the v1 registry at lines
`741-750`, and emits only the v1 durable registry/Go pipeline at lines
`1241-1291`. Correspondingly, `contracts/generation.lock.json:7-11` retains the
old 60-schema/79-fixture source identity and lines `91-94` bind only
`durable-coordination-registry.json`, not the v2 registry or generated Go
profile.

Impact: edits to the v2 schemas, profile, route descriptor, generators, JSON
registry, or Go output are outside the canonical fixture/lock replay authority.
The tree therefore does not meet ADR-0013's exact input-manifest/output binding
at `docs/plan/adr/0013-p1-durable-coordination-contract.md:44-75` or its explicit
new-profile lock requirement at lines `91-93`.

Minimum repair: preserve every v1 byte and add one versioned successor
fixture/lock pipeline that binds the v2 schemas, source profile, local route and
OpenAPI/projection authority, both v2 generators, generated JSON and Go outputs,
and currentness checks. Regenerate only the successor manifest/lock artifacts;
do not rewrite the historical lock as if it had always described v2.

### P1-002 — Migration `000013` is absent from the contract-lock SQL source closure

`scripts/lib/platform-contract-lock.ts:313-355` fixes migration inputs only
through `000012`; its discovered directories at lines `356-360` are archive,
catalog, and fixtures, not the migration root. `platformMigrationInputs` at
lines `2765-2773` therefore cannot add the `000013` SQL file. This conflicts
with the migration generator's explicit `000013` input at
`scripts/lib/platform-migration-bundle.ts:72-85` and with the exact SQL artifact
record at `services/control-plane/migrations/manifest.json:497-511` (also the
schema bundle at the same lines).

Impact: the migration bundle records the SQL digest, but the platform contract
lock's normalized input manifest does not directly attest the exact `000013`
SQL bytes. That is an incomplete supply/source closure for the new append-only
writer and falls short of the exact-source claim in
`services/control-plane/migrations/README.md:299-306`.

Minimum repair: include `000013` in the versioned successor lock input set, or
replace the hand-maintained root SQL list with deterministic consecutive
manifest-owned discovery that rejects omissions and extras. Keep `000001`–
`000012` byte-identical and regenerate the successor lock only.

### P2-001 — Operation/event identifiers use an MD5 namespace

`services/control-plane/migrations/000013_add_durable_project_create_writer.sql:312-315`
derives both identifiers from a truncated MD5 value over subject, idempotency
key, and request digest. This is deterministic, and no collision was observed,
but MD5 is not collision-resistant namespace authority; a collision would hit
identifier uniqueness and can turn a valid create into a denial of service.

Minimum repair: in a later append-only migration, derive the shared suffix from
a domain-separated SHA-256 input with enough retained bits for the identifier
limit, and add deterministic domain/collision-boundary tests. Do not edit
`000013` in place.

## 6. Verdict and next smallest milestone

Verdict: **REQUEST_CHANGES, P0=0 / P1=2 / P2=1**.

Preserve the candidate and all historical evidence. The next smallest milestone
is one successor-lineage repair that closes P1-001 and P1-002: add canonical v2
fixture/currentness coverage and a versioned lock binding the complete durable
v2 plus `000013` source closure, then obtain a fresh independent review. Do not
start the P2 hardening, production database work, provider/HTTP/P2 effects,
deployment, publication, or any later slice automatically.

This review closes no aggregate Gate. `G-CONTRACT`, `G-DATA`,
`G-AUTHORITY-P1`, `G-SECURITY-P1`, and `G-SUPPLY-CHAIN` remain open/unverified.
