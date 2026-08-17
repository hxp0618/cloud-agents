# Control Plane migration authority

This directory is the only editable PostgreSQL schema authority for the public
Cloud Agents Control Plane. Synara migrations, ORM models, generated SDK types,
and Go row structs are semantic inputs or consumers; none may generate or
silently alter this schema.

## Execution order

1. An isolated, unswitched superuser LOGIN runs `bootstrap/roles.sql`. The
   transactional DO block rejects any other caller before role creation, then
   creates or verifies only the three credential-free `NOLOGIN` group roles
   frozen by ADR-0008. Newly created roles are reselected and pass the same
   attribute and membership checks as existing roles. Existing role attributes
   or inherited memberships that do not match the frozen boundary fail closed.
   No incoming membership to any of the three groups may carry
   `admin_option=true`; group authority is never delegable. Every
   `pg_auth_members.grantor` must be an isolated superuser, must not be a Cloud
   Agents group, and must not be its direct or indirect member. These rules use
   only catalog fields present in PostgreSQL 15–17. A non-superuser
   `CREATEROLE`-only caller or grantor is unsupported in this slice because
   PostgreSQL 16 requires ADMIN OPTION to issue the grant, while this boundary
   forbids delegable membership; a future deployment adapter requires a new
   decision. Workload `LOGIN` roles and credentials remain deployment-owned and
   are never checked in here. Each incoming member must nevertheless be a safe,
   nondelegable LOGIN with no members and no elevated database attributes. Its
   complete `pg_auth_members` set as member must contain exactly the current
   Cloud Agents group and no unrelated role. Migration LOGINs are `NOINHERIT`
   with a settable but non-inherited membership and must use explicit
   `SET ROLE`; runtime/bootstrap LOGINs and memberships are `INHERIT`, with
   `pg_has_role(..., 'USAGE')` proving effective access. PostgreSQL 16
   `inherit_option`/`set_option` are read through `to_jsonb(pg_auth_members)`;
   their absence on PostgreSQL 15 receives the legacy `true` interpretation
   without referencing missing catalog columns. A recursive closure
   additionally rejects any role that resolves directly or indirectly to more
   than one Cloud Agents authority.
2. Connected to the exact target database as its isolated deployment-owned
   LOGIN owner, or as an explicitly authorized superuser when the preferred
   database owner is `NOLOGIN`, the administrator runs
   `bootstrap/database.sql` with the non-secret `cloud_agents_database` and
   `cloud_agents_database_owner` psql variables. The script verifies the exact
   database, owner, unswitched caller, and all three group-role boundaries. The
   deployment owner must be non-superuser, without `BYPASSRLS`, database/role
   creation, or replication, and must be isolated from all Cloud Agents group
   roles. It is non-delegable: no LOGIN or group role may be its direct member,
   because membership could inherit the owner's implicit CREATE/TEMPORARY
   authority. Unknown CREATE/TEMPORARY grantees fail closed. The script revokes
   CREATE/TEMPORARY from PUBLIC and every workload group, then grants only
   CREATE to `cloud_agents_migration_owner`. It does not change the database
   owner or create a schema. It is safe to replay with the same inputs.
3. A dedicated migration `LOGIN`, after the ADR-0008 attribute and direct
   membership preflight and an explicit
   `SET ROLE cloud_agents_migration_owner`, runs the strict manifest runner. It applies
   `000001` through `000005`, under the migration advisory lock. Each
   migration is a separate short transaction. `000001` accepts an absent schema or an
   existing empty schema already owned by the migration owner; it rejects a
   wrong-owner or nonempty schema before creating migration objects.
4. A dedicated bootstrap-admin `LOGIN` may create a tenant only by executing
   `cloud_agents.bootstrap_platform_tenant(...)`. The function atomically
   creates the tenant root at revision `1`, its locked revision counter, the
   matching change record, and a redacted tenant-owned audit fact. The
   bootstrap role has no direct tenant-table DML.
5. Runtime connections set `cloud_agents.tenant_id` with transaction-local
   `set_config(..., true)` before issuing tenant reads. Missing or malformed
   context fails with SQLSTATE `22023`; every tenant table has both `ENABLE`
   and `FORCE ROW LEVEL SECURITY`.
6. `000003` seeds the exact built-in role catalog v1 and adds tenant-owned
   Membership/RoleBinding rows. The package-internal typed store resolves the
   request resource from tenant-owned Organization/Project rows, reads the
   complete bounded catalog and subject-field-matched candidates in the same
   read-only transaction, and revalidates exact subject digests, lifecycle,
   expiry, containment, role version/scope and permission before returning an
   allow. The ordinary tenant runtime path never consumes `platform.admin` as
   authority. No raw Membership/RoleBinding write capability or public HTTP
   mutation route exists in that read-only slice.
7. `000004` adds exactly five SECURITY DEFINER mutation functions and the
   package-internal `RBACMutationService`: membership create/suspend/revoke and
   role-binding bind/revoke. Every call rechecks the runtime principal closure,
   binds one tenant-local transaction, authorizes `tenant.admin` in that same
   transaction, performs a compare-and-swap transition, allocates exactly one
   tenant revision, and appends the matching resource-change and audit facts.
   Runtime receives EXECUTE only on those five entry points, never on their
   helpers and never raw table DML; PUBLIC and bootstrap EXECUTE are revoked.
   Commit ambiguity is returned as an explicit unknown outcome and is never
   retried automatically. No public HTTP mutation route exists in this slice.
8. `000005` preserves the exact bytes of applied `000004` while closing the
   Membership/RoleBinding admission invariant. It replaces the bodies of the
   existing RoleBinding-bind function without changing its name, signature,
   ownership, ACLs, or the five-operation runtime API. A RoleBinding requires an
   exact-subject active, unexpired Membership that contains its scope. Candidate
   reads additionally require that Membership authority to predate the binding,
   so later Membership re-admission cannot reactivate a historical binding.

The database bootstrap is a psql script rather than a schema migration. Invoke
it without embedding credentials in the command line, for example:

```sh
psql \
  --set=cloud_agents_database="$PGDATABASE" \
  --set=cloud_agents_database_owner="$CLOUD_AGENTS_DATABASE_OWNER" \
  --file services/control-plane/migrations/bootstrap/database.sql
```

Both variables are mandatory. A missing value raises SQLSTATE `22023`; with
the script-owned `ON_ERROR_STOP`, psql exits with status `3`, which deployment
automation must preserve as a hard failure.

The bootstrap-admin LOGIN must be a direct member of only
`cloud_agents_bootstrap_admin` with `admin_option=false`; `pg_has_role(...,
'USAGE')` must prove that EXECUTE is currently inherited without role
switching. Delegable membership, unusable inheritance, elevated LOGIN
attributes, and direct or indirect runtime/migration membership all fail
closed. The function also scans the whole bootstrap group on every call: one
incoming `admin_option=true` membership or untrusted `pg_auth_members.grantor`
fences every caller, including callers whose own membership appears
non-delegable, until the drift is removed. The current caller's direct row and
grantor are checked independently before the global scan. Its complete
membership set must still be exactly one direct bootstrap row, and the caller
must remain nondelegable; later unrelated-role or intermediary drift fences the
call. The function rechecks that authority from `session_user`. It serializes
each tenant UID with a transaction-level advisory lock. A response-loss retry
by the same database principal with the exact tenant UID, tenant name,
display-name bytes, audit UID, and reason code returns the original
`(tenant_id, 1)` result. Any mismatch returns SQLSTATE `23505` with constraint
`bootstrap_platform_tenant_intent`. The bootstrap principal receives no raw
table SELECT or DML for that readback, and no Cloud Agents workload principal
receives database TEMPORARY privilege.

`audit_facts` is the minimum append-only seam needed to make tenant bootstrap
auditable in P1-A2.1. Its composite foreign key binds tenant, revision,
resource kind, and resource UID to the exact `resource_changes` fact, with a
matching child-side index. P1-A2.3 may extend audit coverage only through a new
forward migration; it must not rewrite this applied migration or introduce raw
credential/secret payloads. The bootstrap seam records a bounded reason code,
not caller-supplied free-form detail.

The runtime group receives tenant-scoped `SELECT`, plus EXECUTE on the five
effective typed mutation entry points, including access to the global built-in role
catalog and tenant Membership/RoleBinding facts. Project, Membership and
RoleBinding mutation is never granted as raw table write authority. Each typed
mutation allocates and publishes its tenant revision atomically through the
forward migration and reviewed store path; A2.3 still owns durable
operation/outbox coordination.

The local conformance matrix always starts fresh exact PostgreSQL 15/16/17
containers, applies all five migrations, seeds only deterministic tenant facts
through the migration-owner role, and runs both the typed runtime authorization
and five-operation mutation tests in normal and race modes. It also proves that
runtime has no helper-function or direct DML authority and that neither PUBLIC
nor bootstrap inherits mutation-function EXECUTE:

```sh
./services/control-plane/scripts/test-membership-rbac-postgres-matrix.sh
```

The script never pulls an image implicitly or reuses an existing container or
database. A passing local matrix is implementation evidence only, not a
production database write, publication, deployment or aggregate Gate closure.

## Immutable boundary

Applied SQL is immutable exact-byte input (UTF-8, LF, no BOM, Git mode
`100644`). Fixes are new consecutive forward migrations; there are no edited
checksums or down-migration claims. `manifest.json`, `schema-bundle.json`, and
the catalog projections are generated from the SQL, bootstrap, and fixture
inputs by the checked-in migration generator. The checker validates exact
artifact sizes, SHA-256 values, source-closure inputs, deterministic runtime
and bootstrap archives, ancestor/ledger projections, and the RFC 8785 bundle
digests. A placeholder or manually maintained digest is forbidden.

The checked-in bundle is currently `UNPUBLISHED_BOOTSTRAP_MUTABLE`: runtime
catalog introspection and signing/publication are `NOT_IMPLEMENTED`, and this
status is not a Gate-closure or release claim. Until the catalog adapter,
security review, signing, and publication gates close, any regeneration may
legitimately change the schema, bootstrap, manifest, or archive digests. The
current files must therefore not be consumed as an immutable release
candidate; consumers pin a digest only after a published candidate manifest
exists.

## P1-A2.1a projection fixtures

`fixtures/projection/` freezes the strict authority/profile, deployment-binding,
namespace/default-ACL, catalog-state, statement-transition, intermediate-state,
attempt-terminal, and numeric shapes from ADR-0010. The deployment binding is a
deterministic fixture only: it has no detached signature, issuer decision,
trust-root proof, expiry/revocation verification, or production identity.

The initial migration predecessor now uses only the closed
`schema_absent | schema_present` union. `schema_present` preserves the catalog
`NULL` versus explicit ACL distinction, ACL grantor provenance, and the full
typed namespace body; the legacy `empty_schema` spelling is rejected.

The A2.1a namespace body and one digest-ref-only transition are reviewer/test
fixtures, not executable cumulative contracts for schema heads `000001` or
`000002`. Those runtime catalog artifacts deliberately omit
`expected_projection` and per-statement `expected_transition`, and carry
`executable_expected_projection_status = NOT_IMPLEMENTED_A2_1B_REQUIRED`.
Relation/function/expression projection and the complete 91-statement expected
state chain remain A2.1b work. The production runtime therefore remains fail
closed and must not treat these fixtures as signed authority or Gate evidence.

Do not hand-run these files against an existing database, reuse an existing
container or volume for validation, or infer a release/deployment/G-DATA Gate
closure from successful local replay.
