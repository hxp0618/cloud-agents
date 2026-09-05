# Lease state breakdown and filtered navigation — 2026-09-05

Base `26878c3482c984b22265401a052e30d5de3e1792`, unchanged branch
`codex/cloud-agents-platform-p0`. Previous turn made progress. This advances ADMIN-M1;
it does not complete the milestone or Goal.

## Delivered

Overview's Lease lifecycle section now has counts for provisioning, ready, terminating,
terminated, failed and cleanup blocked. Each native button opens the Lease list with its
exact state filter; zero results are not mislabeled as a project with no Leases. The selected
state remains visible and can be cleared without losing search. The existing attention
filter remains an independent intersection. Counts and lists use the same `filterAdminLeases`
predicate. Cleanup blocked overlaps observed phase and is deliberately not added to the
phase totals. Both locales and native Intl counts use the current catalog.

No contract, backend, SDK, User Web, persistence or dependency change was necessary for Lease
states: the generated Admin SDK already returns authoritative observed/cleanup phases.
React/Ponytail keep this in derived data and native controls. Design-style reuses current
outline buttons, status labels, borders and spacing rather than introducing a control library.

## Authority finding: Worker is not live heartbeat

Current `worker.schema.json` explicitly defines a logical Worker projection per Lease;
`lastHealthAt` is deployment completion's successful mTLS observation, **not** a periodic
heartbeat. States are starting/ready/stopping/failed/cleanup-pending, not expired/draining.
`listWorkers` reads that projection. Therefore this slice does not fabricate online/expired
statistics from browser time or rename ready to online. True Worker heartbeat/freshness and
drain authority remain required backend work before those Overview counts can be qualified.

## Actual evidence

- Fresh PostgreSQL/local Control Plane/Worker stack at 18085/18095, Admin Vite proxy
  `http://127.0.0.1:4174`, project `project-e56172ae680ec34811c1ad9fac50c732`.
- New `real-docker` Target probed the real OrbStack Docker socket through a fresh loopback
  mTLS gateway. Returned ready, engine 29.4.0/API1.54, linux/arm64. Gateway permitted only
  GET `/_ping` and `/version`; other methods/paths were denied. Responses were forwarded,
  not constructed or mocked. Generated TLS credentials were restricted to this test.
- Three Lease records created through the generated managed-host SDK using admin authority,
  expected Target generation1, distinct idempotency keys and a nonpublished test digest.
  The local managed-host server intentionally has no deployment actuator, so they remained
  provisioning; **no Worker/container/volume was deployed**. One Lease was terminated through
  the generation-fenced API before browser validation. Actual snapshot: 2 provisioning,
  1 terminated/cleanup complete, 0 other observed states and 0 blocked cleanup.
- Read-only `capture-lease-states.mjs` checked two locales × two themes × desktop1440×900 /
  mobile390×844. All six count-to-list paths per scenario passed: exact row IDs against Admin
  API snapshot, Enter activation, query intersection, filtered empty text, clear preserving
  query and restoring all rows. 56 screenshots, no document-width overflow, no Vite overlay,
  correct title/origin/nonblank shell, no page errors, no browser mutations, no persisted bearer.
  Requests stayed on the proxy origin. Ordinary-user Admin Lease request returned403.
- Only two expected initial unset-Quota404 responses occurred. Browser plugin not available;
  reused installed Playwright/Brave. No new browser dependency, response interception,
  product-state injection or direct database fixture writes.
- Admin build/typecheck and 26 tests in three files passed; scoped lint/format/diff checks
  passed. Expanded tests cover failed/blocked overlap, all filter intersections and search.
  Existing 521.59 kB single-chunk warning remains. Nonzero ready/failed/blocked states are
  not real browser lifecycle evidence from this run; mixed membership is unit-tested.
- After browser checks, the two remaining disposable Leases were terminated with generation1
  guards. All three re-read as generation2, terminated/complete; Admin Worker projection was0.
  This proves cleanup of these never-deployed test records, not full runtime zero-residue E2E.
- Final cleanup confirmed owned Vite/Worker/Control Plane/gateway processes exited, PostgreSQL
  container `cloud-agents-dev-501-43355` was removed and `.tmp/cloud-agents-dev.4UiPgo` was deleted.
  Gateway log contains exactly GET `/_ping` and GET `/version`, no mutating Docker request.
  Generated TLS directories were moved to private Trash `cloud-agents-lease-states-F9sgjK`
  (recoverable). The disposable database/token files have no retained backup; unrelated resources
  were preserved.

Pinned Daytona checkout was revalidated at
`01c502bb1f1ff8f2885d0cd490e043736083dca8` (v0.190.0). Actual desktop summary and Chinese dark
mobile filtered-list screenshots were inspected. The new wrapping state controls preserve
existing typography/spacing; no full fixed-reference screenshot-diff qualification is claimed.

## Reproduce

1. Use the fresh development stack procedure in [shell evidence](admin-web-shell-scroll-20260905.md)
   and real read-only Docker Probe credential-directory setup described in
   [three-target probe evidence](admin-three-target-probes-20260905.md). Only Docker is needed.
   Pass `CLOUD_AGENTS_PLATFORM_DOCKER_CREDENTIALS_DIRECTORY` when starting the local stack.
2. Register/probe a new Target to ready, then create three fresh managed-host Lease IDs with
   `createManagedHostEnvironmentLease`. Use expectedTargetGeneration1, unique request/idempotency
   keys, cpuLimitMillis1000, memoryLimitBytes536870912, ttlSeconds3600, an opaque unused Provider
   reference and syntactically fixed test digest. This local entry point does not deploy them.
3. Terminate one new Lease using `terminateManagedHostEnvironmentLease` and expectedGeneration1.
   Verify the snapshot has at least two distinct observed phases before running:

   ```sh
   node apps/admin-web/visual-baseline/daytona-v0.190.0/capture-lease-states.mjs \
     OUTPUT ADMIN_TOKEN_FILE USER_TOKEN_FILE PROJECT_UID
   ```

   Set `PLAYWRIGHT_MODULE` to an installed Playwright ESM module when not locally resolvable;
   optionally set `BROWSER_PATH`. The browser script only reads; it never creates or terminates.

4. Terminate only the newly owned remaining records, verify all complete and Workers0, then stop
   only the fresh stack and gateway. Never use the test procedure against pre-existing workloads.

This run's outputs and setup script are under `/tmp/cloud-agents-lease-states.F9sgjK/`, including
`browser/browser-evidence.json`, `cleanup.json` and the gateway request log. Temporary artifacts
may expire; the committed read-only browser script is the durable runnable check.

Earliest remaining ADMIN-M1 items include real Worker heartbeat/drain authority and statistics,
upgrade counts, recent administrator Audit activity, remaining Target list fields/filters and
full fixed visual comparison. M2/M3/M4 real profile/policy, three-target Worker lifecycles,
Codex/Claude Turns and actual workspace cleanup remain required. No new credential or destructive
authorization is needed for the next implementation slice. No push/image publication/Release.
