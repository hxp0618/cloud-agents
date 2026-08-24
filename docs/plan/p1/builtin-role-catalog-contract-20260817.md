# P1-A2.2-impl-1 built-in role catalog contract — 2026-08-17

- Status：**IMPLEMENTED — contract/catalog slice only**
- Fixed implementation：`f988e450cefeef64e30bfcfe3529485a9543aa6c`
- Fixed tree：`c32fdb159ddf40c761030ca01157d312abdb4527`
- Governing decision：[`ADR-0011`](../adr/0011-p1-membership-rbac-contract.md)
- Toolchain：Node `24.13.1` / Bun `1.3.14` / Go `1.26.6`
- Accountable owner：hxp0618
- Evidence executor：Codex
- Independent reviewer：not assigned for this slice

This record fixes only the first A2.2 implementation boundary. It does not create PostgreSQL RBAC tables,
mint runtime membership authority, evaluate a request, mutate a role binding, publish a contract release or
close an immutable/aggregate Gate.

## 1. Implemented boundary

The fixed commit adds one immutable built-in role catalog v1 fixture and its strict schema/semantic contract:

1. seven active built-in roles carry exact version `1`, scope level and bytewise-sorted explicit permission
   sets; the catalog has exactly 34 unique permissions and no wildcard permission;
2. semantic validation compares the complete role table against a hard-coded v1 expectation, so role reorder,
   permission reorder, omission, addition, scope drift, state drift or version drift fails closed;
3. `ADR-0011` freezes Membership admission as the subject authority, RoleBinding as explicit allow authority,
   database-resolved scope containment, exact `now < expires_at`, a deny-only external PDP and a future-role /
   future-permission version fence;
4. the contract manifest and generation lock bind the schema, fixture, semantic validator and ADR. No SQL,
   Go runtime, database connection, HTTP mutation, dependency graph, SBOM or NOTICE bits changed in this slice.

## 2. Fixed bits

| Artifact                              | SHA-256                                                            |
| ------------------------------------- | ------------------------------------------------------------------ |
| `builtin-role-catalog-v1.schema.json` | `139f0d14f5af9e608aefeeca972698995bb45e0f19513420e672a8502fd0a6c1` |
| `builtin-role-catalog-v1.json`        | `eed02a9546a75176232fb4674ca846b40857d1702406baa48f985b89fdf57cc2` |
| fixture manifest                      | `07c677a7efa172d288ef243545f9cad9d207b7707474ce512bd439fb5da13cc3` |
| `ADR-0011`                            | `2fdb82d14a5058cd6eed38638a9aabd3b55794f5c191bff443e63b769ae4d399` |
| semantic validator                    | `179820be745387ea6b0d9cbf7a8ca74cff276b62d494adf56ea77d535642694b` |
| semantic fault tests                  | `e99b58a466c25452addc0c252f37b517b711bc274793212ba919c3941d26312c` |
| platform contract checker             | `34f0800416d3edcac003b0fc6457681f518cd9c59593df01b7c079772a2cd7ce` |
| contract-lock writer                  | `cfe52c655e3cce991968cb46ce5f94c20cd8894eb45c7c1764f21dd280a30b36` |
| generation lock                       | `7b3f14dc38b9bdfcb28afe0ecb41a70f2e2423116243b0627ffce5793a06b079` |

The generation lock records contract manifest
`sha256:f6240231ac0acce6d7d390a54eb0773d6047e5e6e2ecc98f851cc7caa10426e5`.

## 3. Exact catalog cardinality

| Role                 | Scope          | Permissions |
| -------------------- | -------------- | ----------: |
| `organization.admin` | `organization` |          31 |
| `platform.admin`     | `platform`     |          34 |
| `project.admin`      | `project`      |          25 |
| `project.developer`  | `project`      |           7 |
| `project.operator`   | `project`      |           7 |
| `project.viewer`     | `project`      |           3 |
| `tenant.admin`       | `tenant`       |          34 |

Catalog revision is exact string `1`; publication time is exact
`2026-08-17T00:00:00Z`. Role names and each permission list are bytewise ascending.

## 4. Verification

The fixed slice passed:

- all package and script tests (`120/120` script cases), lint and typecheck;
- platform contract validation: 84 JSON documents, 32 schemas, 42 fixtures and 9 operation IDs;
- migration contract checker and generator same-bits checks: 33 generated files/tar artifacts;
- targeted format, secret scan and `git diff --check`.

Focused semantic faults cover role reorder, one unrecognized future permission, scope drift and wildcard
permission. The schema additionally fixes catalog revision, role version/state and closed field sets. Because
this commit changes no Go, SQL or dependency inputs, it intentionally does not re-claim PostgreSQL, Go
race/cross-build, SBOM, vulnerability or NOTICE evidence from the preceding source-bound supply slice.

## 5. Remaining A2.2 boundary

A2.2-impl-2 must add the migration-owned data/evaluator layer: durable Membership and RoleBinding storage,
exact built-in-role resolution, resolved request-scope containment, expiry and revocation checks, default deny,
tenant-context enforcement and focused PostgreSQL evidence. A2.2-impl-3 must then add mutation/service seams,
the required version matrix, independent review and source-bound supply refresh. None of those claims are made
by this record.
