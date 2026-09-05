# M1 denied-write Audit: confirmed failing acceptance — 2026-09-05

Source `72a9c647ce36a711368f98614ba757c7f83d4cb3`, existing
`codex/cloud-agents-platform-p0`. This record proves a missing requirement;
it does not claim an implemented Audit fix or completed vertical slice.

## Real reproduction

Started a fresh local dev stack with current binaries, real PostgreSQL and
signed ordinary/admin tokens. CP18085, Worker18095, state
`.tmp/cloud-agents-dev.NcJupi`. Created disposable project
`project-b17edaff3030d571a3e38cbe8adf14c5` through the real CLI.

`scripts/test-admin-denied-write-audit.ts` uses generated SDK calls throughout:

1. Positive controls: ordinary user can read the project; project has no
   Targets; administrator registers one real unprobed Target.
2. Registration produces a succeeded Audit, proving the query path works.
3. Ordinary user sends a valid Probe request with the Target's expected
   generation; server returns403. Unlike the preceding malformed-body matrix,
   this request is otherwise valid and addresses an existing owned resource.
4. Re-read Target is identical, generation1/unprobed. No Probe reached the
   actuator, no Lease or Provider was deployed.
5. Re-read Audit has no event matching the denied request ID. Acceptance
   assertion fails with exit1, as it must until the requirement is implemented.

Observed output:

```json
{"project":"project-b17edaff3030d571a3e38cbe8adf14c5","targetId":"denied-audit-d331f9d0-5019-4a54-a14c-fc9a90d32185","deniedRequestId":"8d71d4ad-fefa-4cf4-999e-2383ba7c932a","status":403,"beforeEvents":1,"afterEvents":1,"matchingEvents":0,"generation":1,"phase":"unprobed"}
```

Independent read-only SQL inspection of the owned database confirmed:

- `audit_facts`:3 before and after; `coordination_audit_facts`:1 before/after.
- `deployment_target_activity`:0 before registration,1 after the entire flow;
  its only row is target.register/succeeded with registration request ID
  `cc615be9-38b7-4311-8e0b-a1ce07dd28f5`.
- Profile, Network Policy, Quota, Storage Policy and Release activity tables
  each contain0 rows. No alternate Audit sink contained the denial.

## Cause and required implementation boundary

- `internal/authn/verifier.go:166` rejects missing scope without producing an
  authorized principal for that permission; the offline verifier has no
  persistence side effect.
- `internal/server/deployment_target_http.go:132-138` discards the initial
  project principal, then writes403 and returns when Admin scope fails. No
  store Audit call occurs. Other Admin handlers follow the same early-return
  pattern; patching only Probe would leave sibling writes unrecorded.
- `internal/store/postgres/deployment_target.go:191` reads only
  deployment_target_activity. The table requires an existing Target, positive
  generation and operation ID, and its states describe running/succeeded/failed
  execution. Generic audit_facts is tied to resource_changes; coordination
  Audit is tied to execution/attempt/resource references.
- `AdminAuditEvent` similarly requires operationId and resourceGeneration>=1,
  with no denied result. It cannot truthfully represent a registration denied
  before a resource exists or permission rejection before generation lookup.

The next implementation must persist authenticated rejection metadata through
shared Admin handling and expose a permission-gated query/SDK/UI projection.
It must preserve403, distinguish rejection from executed-operation failure,
avoid fabricated operation IDs/generations, scope actor identity from verified
credentials, and never record request bodies, bearer values or Secret bytes.
Persistence failure behavior, anonymous/unverifiable requests, nonexistent
resources and RBAC denials must be handled explicitly rather than silently
claiming all denied writes are audited. Ponytail's shared-root/reuse check
established these constraints; no new sink has been implemented in this commit.

## Reproduce and cleanup

After starting an owned dev stack and creating a fresh project:

```sh
bun scripts/test-admin-denied-write-audit.ts http://127.0.0.1:18085 \
  STATE/control-plane.token STATE/control-plane-admin.token tenant-local EMPTY_PROJECT
```

Go1.26.6/Node24.18.1/Bun1.3.14 were used. Scoped oxfmt/oxlint and diff checks
passed; the Audit acceptance intentionally failed. It is an opt-in check, not
a newly passing unit test or a replacement for full E2E. Each run needs a new
empty project and leaves one metadata-only Target until the owned stack is
cleaned; never run against existing/shared targets.

Stopped dev parent85620. Shutdown removed `cloud-agents-dev-501-85620` and
`.tmp/cloud-agents-dev.NcJupi`, including the temporary project, Target, database
and tokens (not backed up). No18085/18095 listeners remain. Existing resources
and unrelated dirty/staged work were untouched; staged HTML blob remains
`2adb0cc1c5649e39534a2171a5c25aabedf1fe30`.

M1 denied-write Audit is confirmed incomplete. Goal remains active; no BASE
scope switch, deployment, image publication or Release was performed. No new
credentials or destructive permission are needed for the local implementation.
