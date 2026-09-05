# Overview metric navigation — 2026-09-05

Base `ac144615a87339f40d819c78c0b8dc149f67d9f7`, unchanged
`codex/cloud-agents-platform-p0`. Previous turn made progress. This is another ADMIN-M1
slice, not milestone or Goal completion.

## Delivered

All five existing Overview cards are native buttons leading to the resource list represented
by their count. Total Target/Lease/Worker cards clear previous filters; Target attention sets
unprobed/probing/unavailable; Lease attention sets failed lifecycle OR blocked cleanup.
Target summary now includes all four observed phases rather than omitting unprobed/probing.

Lease attention is a visible, reversible pressed-state toolbar filter. It intersects with search;
toggling it preserves the query, while normal page navigation resets it. Filtered zero results
are distinguished from a project with no leases. Both languages cover the new text.
Overview count and list selection share `leaseNeedsAttention`, avoiding double-counting a lease
that is both failed and blocked. Filtering retains the existing non-sensitive search fields.

Existing paginated Admin SDK loaders supply the snapshot; no contract, backend, generated SDK,
User Web, database schema, dependency, or storage change was necessary. No endpoint is contacted
by metric navigation or filtering. React/Ponytail reuse derived state and native button behavior;
design-style keeps the pinned neutral tokens and existing resource layout.

## Validation and evidence boundary

- Admin TypeScript/Vite build and 25 tests in three files passed; scoped formatting, lint and
  whitespace checks passed. Existing single-chunk warning remains (517.79 kB).
- One new PostgreSQL/local Control Plane/Worker dev stack, ports 18085/18095; Vite proxy at
  `http://127.0.0.1:4174`. Project `project-064d18a4f3398f4fce87f043b85754fc`.
- Registered Docker/Kubernetes/SSH records via generated SDK/Admin API, IDs `visual-docker`,
  `visual-kubernetes`, `visual-ssh`. All are really persisted and unprobed. Their `.test` endpoints
  were not contacted; no mocked/intercepted API responses or injected product state.
- Browser plugin not available. Existing Brave/CDP capture passed eight locale/theme/viewport
  scenarios: en-US/zh-CN, light/dark, 1440×900/390×844. Five metric keyboard transitions per
  scenario, count/list agreement, exact Target phase selection, Lease filter state/clear.
- Full capture produced 97 screenshots, zero document-width overflows or mutation requests,
  no persisted bearer and ten ordinary-token Admin API403 responses. Requests stayed on the
  same Vite origin. Four unset-Quota404 and eight unconfigured Cleanup-preview503 responses
  are expected negative paths, not successful cleanup. No console warnings.
- Additional installed Playwright/Brave checks passed in both locales/themes: page identity,
  nonblank shell, no Vite overlay, keyboard Tab/focus ring and Space activation, stale-query
  reset on card navigation, query preserved across Lease filter toggles, no page errors or
  additional API requests. The initial harness used raw CDP keydown without native Enter text;
  corrected key event delivery rather than adding an unnecessary product keyboard handler.
- Real browser data has three unprobed Targets and zero Leases/Workers. Mixed failed/blocked
  Lease membership, OR deduplication and query intersections are unit-tested, **not** a real
  nonempty Lease lifecycle test. This run proves navigation and empty-state behavior only for
  those resources; it does not qualify Provider or infrastructure runtime E2E.
- Stopped this run's Vite and local dev stack; confirmed Worker/Control Plane children exited,
  container `cloud-agents-dev-501-32671` was removed and `.tmp/cloud-agents-dev.o3EIEX` was
  deleted by the dev-script trap. Disposable database records and credentials have no retained
  backup. No pre-existing deployment, Kubernetes resource or SSH service was touched.

## Reproduce and remaining mismatch

Follow the fresh-stack/project/three-Target procedure in
[shell evidence](admin-web-shell-scroll-20260905.md), then run committed
`capture-actual.mjs OUTPUT ADMIN_TOKEN_FILE USER_TOKEN_FILE PROJECT_UID`.
Final output: `/tmp/cloud-agents-overview-20260905/final/browser-evidence.json`.
Additional Playwright script/output: `/tmp/cloud-agents-overview-20260905/check.mjs` and
`checks.json`. Temporary artifacts may expire; the committed capture is the durable check.

Pinned checkout still resolves to Daytona v0.190.0 commit
`01c502bb1f1ff8f2885d0cd490e043736083dca8`. Reviewed its resource-summary components and existing
design-system tokens. This slice preserves card layout and adds neutral hover/focus feedback;
it is **not** a new complete reference screenshot-diff qualification. Actual en-US desktop/mobile
and zh-CN desktop screenshots were inspected for clipping and legibility.

Earliest remaining ADMIN-M1 work: complete Overview Worker/Lease state breakdown, recent failed
Operations/admin activity and upgrade counts with matching filtered lists; remaining Target
list columns/filters and complete fixed-reference visual comparison. M2/M3/M4 real profile,
policy, Worker lifecycle, Codex/Claude and three-target zero-residue evidence remain separate.
No new credential or destructive permission is needed for the next in-scope implementation.
No push, image publication or Release.
