# Foundation persistent intent kernel — 2026-09-05

Source base `c1850ca9`, branch `codex/cloud-agents-platform-p0`, plus the new foundation
profile/generator, SQL template/migration 000053 and Go intent binding/tests. Existing
`.gitignore`, `go.work.sum` and `docs/img.png` are preserved. No existing migration,
Project profile capability, Agent actuator, public API or User/Admin source was changed.

## Implemented, not yet a product lifecycle

- Internal `foundationSandboxLifecycle/v1alpha1` profile is distinct from the frozen
  Project profiles. Its canonical source generates matching Go and SQL identities.
  Existing state machine, policy, Operation, idempotency, finalizer, outbox, claim
  and coordination audit tables/functions are reused; no second queue was created.
- `workspaces`, `workspace_volumes` and `sandbox_sessions` persist separate resource
  identities. Composite tenant/project foreign keys prevent cross-project binding.
  No cascading deletes; volume retention is `retain`. Runtime role has SELECT only.
  All three tables force tenant RLS. A partial unique index reserves one writer;
  timeout/unknown/failure alone cannot release that reservation.
- One typed SECURITY DEFINER transaction accepts a trusted resolved intent and
  writes all three resources, pending Operation, required compute finalizer,
  outbox event and Audit. Same-key replay returns the original Operation; a changed
  digest or changed resolved fields rejects. Any later constraint failure rolls
  back the entire acceptance. Only active tenant/project and ready, active Docker
  Target metadata are admitted at this initial seam.
- Go binds every resolved field (including tenant/project and fixed profile ID)
  into a canonical SHA-256 digest. It accepts only validated ASCII identifiers,
  pinned image references and bounded safe integers; snapshot data stays separate
  from authorization. No raw endpoints, credentials or user content are stored
  in the new intent records.

Ponytail reused the existing kernel and standard library. PostgreSQL guidance
informed short transactions, forced RLS and indexed ownership foreign keys.

## Actual verification

```sh
bun scripts/generate-foundation-coordination.ts --write
bun scripts/generate-foundation-coordination.ts --check
go test -race ./services/control-plane/internal/coordination
go vet ./services/control-plane/internal/coordination
node scripts/test-foundation-intents.mjs
```

Go 1.26.6, Node 24.18.1, PostgreSQL `17.6 (Debian 17.6-2.pgdg12+1)` on OrbStack.
Generator currentness, Go/race/vet, scoped format/lint and diff checks passed.
The native harness applies migrations 000001–000053 to a fresh network-disabled
PostgreSQL container, seeds explicit database fixtures, and uses an isolated
non-superuser runtime login for acceptance and reads. The ready Target row is a
database test fixture, **not** a fresh real Probe or executable RuntimeProfile.

Final run `foundation-intents-3795b33a-5583-4d4b-a273-05bd3ff177e4` passed:

1. Atomic initial acceptance and same-key replay.
2. Two concurrent acceptance connections coalesce.
3. Changed request digest and changed resolved data reject.
4. One resource tuple/outbox/Audit remains, with no duplicate effects.
5. A conflicting partial acceptance rolls back its idempotency record.
6. Frozen Project profile creates-operation flags and unknown-profile rejection remain intact.
7. Runtime direct volume deletion is denied; another tenant sees zero Workspaces.
8. After restarting PostgreSQL, the Operation remains pending without resubmitting.
9. Existing outbox claim picks the event; a second controller gets no claim.
10. Premature writer release and a second active Sandbox are rejected by constraints.
11. The independently stored Volume retains its `retain` policy.

Both test runs removed only their newly labelled PostgreSQL containers and
anonymous data volumes. These were disposable fixtures, not user Workspace data;
no existing database, host volume or other runtime was modified.

## Unverified / next

000053 is an implementation migration, **not yet part of the product-000052
manifest/installer**. No product installation/migration rollout is claimed.
The SQL entry takes trusted server-resolved data; it is not a user API and must
not be exposed before RuntimeProfile resolution and generated route authorization.
Public schema/SDK, the Go store/service binding, physical volume allocation,
Controller retry/adopt/claim renewal/fencing, actual readiness and Admin lifecycle
remain unimplemented for this new path. PostgreSQL restart durability is not
CP/Controller automatic recovery, and a uniqueness constraint is not physical
writer fencing. BASE-M0/M1 and all acceptance sets remain open. See 06 for next work.
