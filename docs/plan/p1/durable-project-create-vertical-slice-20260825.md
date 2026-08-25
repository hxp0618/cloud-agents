# P1 durable Project create vertical slice

## Status

`IMPLEMENTATION COMPLETE` for the bounded localdev/loopback slice. This record
does not close a Gate, publish a release, or authorize a production database
write.

## Fixed source and integration

- implementation branch: `codex/p1-durable-project-create`
- implementation commit: `a76b475079d7907ed99b6a5f343187b3dde6c4c4`
- integrated branch: `codex/cloud-agents-platform-p0`
- merge commit: `05fa73686ed3689f35cdd37a74b3b3ef10abb996`
- migration: `000013_add_durable_project_create_writer.sql`
- successor profile: `managedAgentCreateProjectDurable/v1alpha1`
- predecessor boundary: the v1 claim-only profile and historical generated
  authority bytes remain unchanged.

## Implemented behavior

The localdev-only protected route
`POST /v1alpha1/tenants/{tenantId}/project-creations` validates tenant,
organization, project fields, request identity, and idempotency metadata before
entering the existing tenant/RLS/security-definer boundary. The single
recoverable transaction records the Project, resource revision, operation,
attempt, terminal receipt, required finalizer, audit fact, pending
`operation_effect` outbox event, and the idempotency result binding.

The bounded PostgreSQL smoke covered:

- first create and the same-key/same-request replay;
- same-key/different-request conflict;
- tenant isolation;
- a rejected/failed transaction with no partial success rows; and
- duplicate checks across Project, idempotency, operation, receipt, finalizer,
  audit, and outbox records.

## Reused focused evidence

The fixed tree already passed the required bounded checks, including the two
durable registry/Go generator `--check` commands, the migration-bundle
generator `--check`, focused coordination/store/server/authn/localmigration and
data-recovery-validator tests, localdev-tag compile/tests, focused vet/race
checks where the changed concurrency path required them, and a single
disposable local PostgreSQL smoke. No broad `go test ./...`, full migration
suite, full race, Docker/Compose/Helm, cross-host E2E, pack/install, or remote
database operation was run for this milestone.

## Explicit boundary and next smallest milestone

Provider delivery, Managed Agent creation, production OIDC/JWKS, public or
non-loopback HTTP, P2/external effects, production database writes, deployment,
publication, and Gate closure remain `NOT_AUTHORIZED`. `G-CONTRACT`,
`G-DATA`, `G-AUTHORITY-P1`, `G-SECURITY-P1`, and `G-SUPPLY-CHAIN` remain
unverified/open under the existing status records.

The next smallest milestone is an independent read-only review of this fixed
candidate/tree, limited to the durable Project-create slice. No subsequent
slice is started automatically by this record.
