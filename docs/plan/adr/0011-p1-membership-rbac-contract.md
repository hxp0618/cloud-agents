# ADR-0011：P1 Membership/RBAC authority、built-in role catalog 与 default-deny evaluator

- Status：Accepted
- Date：2026-08-17
- Scope：P1-A2.2 Membership/RBAC
- Depends on：ADR-0007、ADR-0008、ADR-0010
- Does not authorize：production DB mutation、HTTP mutation route、external PDP allow、A2.3 coordination、Gate closure、deployment/release

## 1. Context

ADR-0007 freezes the neutral permission grammar and seven built-in role names. ADR-0008 freezes the
global `builtin_roles` / `builtin_role_permissions` catalog, tenant-owned Membership/RoleBinding rows,
tenant-local revision discipline and RLS boundary. The checked-in public schemas already reject wildcard
permissions, unknown role names and role/scope mismatch, but they do not yet provide one exact role
version, exact permission set, database shape or evaluator contract.

Leaving those decisions to the store or HTTP layer would create multiple authorization authorities:

- a future route could silently enter an existing role version;
- a RoleBinding could grant without an admitted Membership;
- caller-provided organization/project ancestry could widen a scope;
- an expired, suspended, revoked or unknown row could be treated as an allow;
- an enterprise PDP could accidentally expand rather than restrict the public decision.

This ADR closes those ambiguities before A2.2 writes any database schema or runtime evaluator.

## 2. Slice boundary

A2.2 is split into three reviewable implementation slices:

1. **A2.2-impl-1 contract/catalog**：this ADR, strict built-in role catalog v1 schema/fixture, exact
   semantic validator and negative mutation tests. No SQL or runtime authorization decision is added.
2. **A2.2-impl-2 data/evaluator**：global role catalog seed, tenant-owned Membership/RoleBinding tables,
   RLS/composite FKs/revision integration, typed read store and default-deny evaluator. No new public HTTP
   mutation route and no A2.3 durable operation/outbox implementation.
3. **A2.2-impl-3 mutation/matrix/review**：typed create/suspend/revoke/bind functions, PostgreSQL 15–17
   isolation/concurrency/expiry faults, source-bound supply evidence and independent review.

The current commit is allowed to implement only item 1.

## 3. Built-in role catalog v1

The sole v1 contract is
[`builtin-role-catalog-v1.json`](../../../contracts/platform/v1alpha1/fixtures/golden/builtin-role-catalog-v1.json),
validated by
[`builtin-role-catalog-v1.schema.json`](../../../contracts/platform/v1alpha1/schemas/builtin-role-catalog-v1.schema.json)
and an independent exact semantic table in `scripts/lib/platform-json-semantics.ts`.
The fixed fixture SHA-256 is
`eed02a9546a75176232fb4674ca846b40857d1702406baa48f985b89fdf57cc2`; changing those bytes requires a
new catalog revision/role version and a new ADR review rather than an in-place permission expansion.

Catalog identity is fixed as:

- `apiVersion = platform.cloud-agents.dev/v1alpha1`;
- `kind = BuiltinRoleCatalog`;
- `catalogRevision = "1"`;
- every role has `version = 1` and `state = active`;
- roles and each permission list are bytewise ascending, unique and exact;
- the catalog contains exactly the seven ADR-0007 role names;
- `platform.admin` contains the exact union of every registered v1 permission.

The v1 permission registry is the exact union in the fixture. Its resources are `tenants`,
`organizations`, `projects`, `memberships`, `roles`, `role-bindings` and the already planned
read/act vocabulary for `operations`; verbs remain the ADR-0007 closed set. Registering an operation
permission here does not implement A2.3 storage or an HTTP route. A future resource or permission must
create a new catalog revision and role version; it never enters version 1 by implication.

Role scope is exact:

| Role                 | Scope        |
| -------------------- | ------------ |
| `platform.admin`     | platform     |
| `tenant.admin`       | tenant       |
| `organization.admin` | organization |
| `project.admin`      | project      |
| `project.operator`   | project      |
| `project.developer`  | project      |
| `project.viewer`     | project      |

The fixture's permission arrays are normative. A wildcard, missing/extra/reordered permission,
duplicate/reordered role, wrong scope, unknown role, wrong role version or changed publication identity
fails closed. JSON Schema validates shape; the semantic validator proves the exact frozen sets. Neither
layer may accept caller-supplied replacement catalog bytes at runtime.

## 4. Subject, Membership and RoleBinding authority

`SubjectRef` identity is the existing exact RFC 8785 profile over `kind`, `issuer`, `subject`; issuer and
subject remain case-sensitive and are never trimmed, normalized as URLs or mapped from a host-private
identifier. The database stores the canonical subject digest plus the exact bounded fields required to
recompute it. A digest alone never substitutes for the fields.

A request is eligible for an allow only when all of the following are true in one tenant-bound read
transaction:

1. the authenticated subject exactly matches the request `SubjectRef` and digest;
2. at least one Membership for that tenant/subject is `active`, not expired and has a scope containing
   both the candidate RoleBinding scope and the resolved request resource scope;
3. the RoleBinding is `active`, not expired, exact-subject-equal, tenant-equal and references an active
   built-in role name/version whose frozen scope level equals the binding scope level;
4. the role version explicitly contains the requested permission;
5. the binding scope contains the resolved resource scope using database-owned ancestry.

Suspended, revoked, expired, malformed, unknown or cross-tenant rows contribute no allow. Multiple
eligible bindings form an explicit allow union. There is no implicit owner, wildcard, authentication-only
allow or host superuser mapping. An inactive broad Membership does not negate an independently active,
valid narrower Membership; duplicate same-subject/same-scope live rows are rejected by database
uniqueness rather than interpreted as deny precedence.

Membership is therefore admission authority, while RoleBinding is permission authority. A RoleBinding
without an eligible Membership never authorizes.

## 5. Scope containment

The evaluator never trusts a caller-supplied ancestry claim. A typed store resolves a request resource
into a bounded `ScopePath` from tenant-owned rows:

- platform contains every tenant only for `platform.admin` bootstrap/recovery decisions;
- tenant contains itself and every organization/project with the same `tenant_id`;
- organization contains itself and projects whose composite FK names that organization;
- project contains only that exact project;
- equal scopes contain themselves.

Unknown/deleted resources, invalid NamespaceRef kinds, missing ancestry, inconsistent composite FKs or
an attempt to evaluate platform scope through the ordinary tenant runtime path fail closed. The
`platform.admin` RoleBinding remains creatable only by the future audited bootstrap function; product
role authority remains independent from PostgreSQL group-role membership.

## 6. Default-deny evaluator ABI

The impl-2 evaluator will be package-internal and typed. Its logical input is:

- exact tenant ID and authenticated `SubjectRef`;
- one registered permission string;
- one store-resolved request `ScopePath`;
- a caller deadline/context and evaluator-owned clock instant.

It returns either `Allow` with the exact Membership, RoleBinding and role-version facts used, or `Deny`
with a stable non-secret reason class. Operational context/database/decoding failures return an error and
must be treated by every caller as fail closed; they are not converted into a successful deny-cache
entry. Decisions are not durable authority and cannot be replayed after membership/binding/catalog
revision, expiry instant or tenant resource revision changes.

An external enterprise PDP consumes a public `Allow` only as an upper bound and may change it to deny.
It cannot change public deny/error to allow.

## 7. Impl-2 database contract

The next slice must add exactly four tables:

- global `builtin_roles` and `builtin_role_permissions`, migration-owned and runtime read-only;
- tenant-owned `memberships` and `role_bindings`, each carrying `tenant_id`, exact subject fields/digest,
  scope columns, lifecycle/expiry, positive tenant `resource_version`, timestamps and composite FKs.

Membership/RoleBinding changes extend `resource_changes.resource_kind`, allocate the same tenant-local
revision in one transaction, and use deferred exact change-row FKs. Both tables enable and force RLS;
runtime policies require `require_tenant_id()`. No raw SQL executor, arbitrary permission string,
caller-provided role catalog or cross-tenant lookup is exposed by the store.

Impl-2 seeds catalog revision 1 from the same checked-in fixture/derived SQL facts and verifies exact
catalog equality on replay. It must not add custom role tables, operation/outbox tables, audit expansion,
HTTP mutation handlers or production bootstrap credentials.

## 8. Required faults

At minimum the implementation matrix must reject:

- unknown/wildcard/unregistered permissions and implicit future permission expansion;
- missing, duplicate, reordered or drifted role catalog rows/permissions;
- wrong role version/scope/state or `platform.admin` at non-platform scope;
- subject field/digest mismatch, issuer case change and cross-tenant subject reuse;
- missing/suspended/revoked/expired Membership or RoleBinding;
- binding scope wider than Membership, request scope outside binding, caller-reported false ancestry;
- duplicate live same-subject/scope records, revision/change-row mismatch and RLS bypass attempts;
- context cancellation, database error, row decode drift, clock boundary and external PDP allow expansion.

Exact expiry semantics are `expires_at IS NULL OR now < expires_at`; equality is expired. Tests use an
injected fixed clock and never depend on wall-clock sleeps.

## 9. Rejected alternatives

### Role name alone implies current permissions

Rejected. It silently grants future permissions to old bindings and makes historical authorization
unreconstructable.

### RoleBinding without Membership

Rejected. It makes Membership lifecycle non-authoritative and allows a stale binding to resurrect a
removed subject.

### Caller supplies organization/project ancestry

Rejected. Scope widening must be proven by tenant-owned composite FKs in the same read transaction.

### External PDP can add allow

Rejected. Public RBAC is the maximum authority; enterprise policy is deny-only refinement.

### Custom tenant roles in P1

Rejected. They require a new tenant-owned role contract, revision/watch semantics and independent
security review.

## 10. Gate boundary

Acceptance of this ADR and impl-1 proves only exact catalog contract bits and semantic fixtures. It does
not prove database schema, evaluator behavior, RLS, mutation durability, PostgreSQL matrix, production
auth, Gate closure or release. All aggregate Gates remain open.
