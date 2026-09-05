# Authenticated Admin denied-write Audit — 2026-09-05

## Delivered slice

Closes the reproduced gap in `admin-denied-write-audit-gap-20260905.md` for the
13 current Admin write contracts. Local and production Admin routers share one
response boundary. A real 403, with a verified project principal, synchronously
appends metadata to PostgreSQL before the response is sent. Successful requests,
anonymous/invalid-token requests, reads and unsupported routes do not create
false denial events. Resource path identifiers must satisfy their contract.

The separate `AdminDeniedWriteEvent` contract does not invent an executed
operation ID, observed generation, resource existence, or successful business
effect. It records the trusted API operation ID, verified actor digest, requested
resource/profile-version identifiers where present, request ID and server time.
Malformed correlation IDs use `request-unknown` without changing the original
request's validation or persisting its malformed value. Bodies, query strings,
headers, bearer tokens, credential bytes and user content are not recorded.

Migration 000052 adds FORCE RLS and an append-only security-definer write
function. Runtime has table SELECT only, not direct INSERT/UPDATE/DELETE.
`GET /v1/admin/tenants/{tenantId}/projects/{projectId}/denied-write-events` requires
`audit.list`, `projects.get` and existing project RBAC. Pagination is bounded and
the cursor resolves to an existing event in that tenant/project. Generated Go
and TypeScript SDKs expose the query. Admin Maintenance shows real records,
loading, errors, empty state and previous/next pages in both languages. User Web
gets no new fields, UI, permission or infrastructure request authority.

If the append cannot commit within five seconds, the rejected request returns
503 `ADMIN_AUDIT_UNAVAILABLE` / retryable, not an unaudited success or a claimed
durable 403. This is an explicit availability exception to the healthy-service
ordinary-user 403 contract. Retrying is required; no background delivery queue
or silent successful Audit claim is introduced.

## Real local evidence

All resources below were created by this slice on disposable loopback stacks;
no existing Docker/Kubernetes/SSH targets or user workspaces were touched.

- Initial stack `.tmp/cloud-agents-dev.zcBx4Y`, PostgreSQL container
  `cloud-agents-dev-501-3265`: product migration head 000052, applied 52, no-op false.
  Project `project-a5dd36280a15495d0f1cf4b3f444aa77`, unprobed Target
  `denied-audit-db1a0a8a-051f-4034-93db-33cd3469f9d4`.
- Generated SDK positive control registered that Target. Its valid ordinary-user
  Probe returned 403. Target remained generation 1 / unprobed; executed Audit
  remained exactly the registration event. A separate denial matched request
  `473bc596-d3bf-4854-a6a5-a6da39b4fcaa`.
- Two requests to each of all 13 write contracts plus that Probe produced 27
  distinct persisted denial events. Page size five traversed six pages without
  duplicates. Ordinary-user Audit query returned 403; cross-project cursor replay
  returned 400. Body credential canary was absent from query results.
- The dynamic boundary script covered 39 Admin operations / 78 requests: user
  403, anonymous 401, exact Problem/no-store/correlation envelopes. Nine User API
  infrastructure-field injections remained 400. This added 13 denial events.
- Independent PostgreSQL queries counted 40 denial rows, 13 action kinds and one
  executed Target Audit. FORCE RLS and row security were true; runtime privileges
  were SELECT=true, INSERT/UPDATE/DELETE=false. With the runtime role and tenant
  context `tenant-local`, count was 40; switching to `tenant-other` returned zero.
- Failure injection locked only this disposable audit table for 12 seconds in a
  transaction, then rolled back. Request `818bd0c1-317a-4952-826b-af23d1b01ef1`
  returned 503 / `ADMIN_AUDIT_UNAVAILABLE` / retryable after the bounded append
  deadline. Row count stayed 40; Target stayed generation 1 / unprobed. No original
  handler error body or database diagnostic leaked into the replacement Problem.
- A second disposable build was retired during final review. Final-source replay
  used `.tmp/cloud-agents-dev.5rkSM0`, container `cloud-agents-dev-501-22536`,
  head 000052 / 52 migrations, project `project-645e4f645224a7f3c619785a4980494d`.
  Target `denied-audit-2e13326d-a0d4-4260-ac4b-b8829a4bd856`, Probe request
  `e9fee737-eb98-47b4-a027-0d7923e972d1`. Both acceptance scripts passed again.
  The 27 paged events plus one malformed-correlation event plus 13 boundary
  requests yielded 41 database rows. The fallback event was `adminProbeDeploymentTarget`
  for this Target with request ID `request-unknown`; executed Audit count stayed
  one, generation/phase stayed 1/unprobed.

## Browser evidence

Flow: connect to the real Admin API → Maintenance → denied writes → next/previous
page and refresh → real result, timeout, expired-token error or empty project.
Origin and proxied CP endpoint were both `http://127.0.0.1:4174`.
Browser plugin not available; agent-browser CLI not installed. Used the existing
in-app browser's Playwright interface, not mocked transport or injected app state.

- Page title/identity and meaningful content verified; no framework overlay.
- Both locales × light/dark × 1440×900 / 390×844 visually inspected. Screenshots
  emitted inline in the task. No whole-document horizontal overflow; mobile
  resource tables retain local horizontal scrolling. Actor and request identifiers
  wrap inside fixed table columns rather than squeezing timestamps.
- Live pages contained 25 then 15 events; previous navigation returned to page one.
- Pausing only the owned CP produced aria-busy=true and six skeleton rows. The
  15-second read deadline produced a real timeout. CP was resumed; reconnecting
  restored the list. Read-only timeout copy was corrected to avoid implying a
  maintenance mutation. Later actual token expiry produced AUTHENTICATION_FAILED,
  a reauthentication instruction, and no stale denial rows.
- A distinct empty project `project-3d88912a61f2a958fa43a240d1467307` showed a real
  empty result and disabled pagination, with no previous project's rows.
- Console inspection before intentional token expiry returned no warnings/errors.
  This is scoped visual/interaction evidence, not a complete upstream screenshot
  diff or blanket qualification of every Admin page/state.

## Reproduction and focused checks

Use Go 1.26.6, Node 24.18.1 and Bun 1.3.14. Start the existing disposable dev script
with `CLOUD_AGENTS_DEV_CONTROL_PLANE_LISTEN=127.0.0.1:18085` and
`CLOUD_AGENTS_DEV_WORKER_LISTEN=127.0.0.1:18095`. Use its generated CLI/token paths
to create a fresh project, then run:

```sh
bun scripts/test-admin-denied-write-audit.ts http://127.0.0.1:18085 USER_TOKEN_FILE ADMIN_TOKEN_FILE tenant-local EMPTY_PROJECT_ID
bun scripts/test-admin-user-boundary.ts http://127.0.0.1:18085 USER_TOKEN_FILE ADMIN_TOKEN_FILE tenant-local PROJECT_ID
```

The first script refuses a project with Targets; it registers one unprobed Target
with an unconfigured credential reference and unreachable loopback endpoint.
It must not contact an actuator. Do not use these scripts against a shared stack.

Passed: normal and race tests for server, PostgreSQL store, authn, localmigration
and product migrator; production CP command tests; focused Go vet; Go platform
and OpenAPI SDK tests; 43 TypeScript SDK tests; 21 selected SDK generator/release/
new-codec tests; 30 Admin Web tests; 23 User Web tests; Admin typecheck/build;
SDK generator current check; focused oxlint; `git diff --check`.

Full contract standards runner did not execute: uv 0.6.0 was found, but the pinned
requirement is 0.12.5 (Python 3.14.7 and Bun 1.3.14 matched). Build retained the
non-fatal >500 kB chunk warning. No dependency changes or toolchain bypass.

## Cleanup and remaining boundary

All three owned dev-script parents were terminated and their cleanup traps
finished. Their temporary state directories and PostgreSQL containers were absent;
18085, 18095 and 4174 had no listeners. The owned Vite process and browser tab were
closed and viewport reset. Temporary projects, Target metadata and audit rows
were removed with their databases, without backup. No existing user resources
were removed. No push, image publication or Release was performed.

This closes the denied-write slice, not ADMIN-M1/M4 or the Goal. Earliest remaining
ADMIN-M1 acceptance is the full pinned shell/list/detail/create-Sheet state and
visual-diff matrix plus current three-transport Target register→Probe-ready
qualification. This run did not deploy a managed Worker or execute Codex/Claude
Turns on Docker, Kubernetes or SSH; old provider results are not refreshed by it.
The dirty foundation-first plan still does not authorize switching this Goal to
new Workspace/Sandbox/customer-node models. Unrelated dirty/staged files remain
outside this slice.
