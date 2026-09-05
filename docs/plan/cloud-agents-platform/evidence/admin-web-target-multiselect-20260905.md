# Target multi-select filter menus — 2026-09-05

Base source `4d1390501133e61ca094c871e01d37b299a1cd1d`, unchanged
`codex/cloud-agents-platform-p0` branch. Previous turn was progress. This continues ADMIN-M1;
it does not complete the milestone or the Goal.

## Slice and reference

Replaced the preceding native single-select controls with the pinned filter interaction:
one Filter entry, kind/status submenus, searchable checkbox options, and removable active
condition chips. Choices within one category are ORed; categories and free-text search are
ANDed. Clearing one category preserves the other. No new API, SDK, storage or User Web change.

The existing generated SDK still supplies the complete paginated Target snapshot. Filtering
does not contact Control Plane again, mutate resources, persist selection or contact target
endpoints. `TargetFilters.tsx` shares one implementation across the two current categories
and their active chips; no new menu, state-management or styling dependency was added.

Revalidated Daytona v0.190.0 source commit
`01c502bb1f1ff8f2885d0cd490e043736083dca8`. Inspected
`SandboxTableHeader.tsx`, `filters/StateFilter.tsx`, `ui/command.tsx`, `ui/popover.tsx` and
`ui/dropdown-menu.tsx` in the pinned temporary checkout. Source-derived dimensions:

| Surface                  | Current implementation / checked geometry                                    |
| ------------------------ | ---------------------------------------------------------------------------- |
| Filter category menu     | 160px wide, 4px padding, 32px entries, semantic 16px icons                   |
| Category submenu         | 256px wide, 4px side offset, bounded viewport positioning                    |
| Active-condition popover | 288px wide, 40px search, 32px options, 16px checkboxes                       |
| Condition chips          | 24px height, 240px text maximum, first two labels plus remaining count       |
| Surfaces                 | Existing popover/accent/primary tokens, 6px menu corners, 4px option corners |
| Motion                   | 200ms entrance; disabled with reduced motion                                 |

The browser's [Popover API](https://developer.mozilla.org/en-US/docs/Web/API/Popover_API/Using)
provides top-layer rendering, nested auto-popovers and outside dismissal. Local positioning
handles clipping/resize/scroll, including initial intrinsic-size changes. Escape explicitly
closes one level and restores its trigger. ArrowLeft returns from the submenu; Home/End
select category endpoints; option arrows stop at the ends, consistent with the pinned
Command usage without [cmdk's opt-in loop](https://github.com/dip/cmdk#command-cmdk-root).

The design-style/React/Ponytail skills kept the fixed tokens, derived selection and current
shared implementation; frontend validation required rendered evidence rather than build alone.

## Actual validation

Fresh local PostgreSQL/Control Plane/Worker at 18085/18095, Admin Vite at
`http://127.0.0.1:4174`. New project
`project-5ce9b0c5f1176d7e47c955cdbbe69377`; three real API-registered records named
`visual-docker`, `visual-kubernetes`, `visual-ssh`. All remain unprobed. No `.test` endpoint
was contacted and no response was mocked, intercepted or substituted.

Browser plugin not available; used installed Playwright 1.62.1/headless Brave and the
checked-in CDP capture, without installing dependencies.

- Both locales × both themes × 1440×900/390×844: real 3→1→2 filtering, same-category union,
  cross-category intersection, checked visual state, empty option search/Enter no-op,
  condition editing/removal, group clear, nested Escape and restored focus.
- Checked native menu geometry, nonblank page/title/origin, no framework overlay, no document
  overflow, no JavaScript page errors and no new API requests during the separate filter run.
- Extra 390×360 test: popover stays inside the viewport, IME Enter does not select, outside
  pointer dismissal closes the menu chain, normal-motion duration is 200ms.
- The durable capture additionally retains locale persistence/fallback, shell/modal regressions,
  missing probe-fact text, keyboard table scrolling and ordinary-user Admin API403 checks.
- Admin build/typecheck, 24 tests in three files, scoped lint/format and diff checks.
  Tests include multi-kind/multi-phase set semantics and exclusion of endpoint/credential fields.

Browser checks exposed and fixed child toggle events being handled by the parent (focus theft),
pointer-enter reopening another submenu after Escape, and insufficient first-open height
measurement in a short viewport. Explicit keyboard close/focus handling removes reliance on
an intermittently failing native-only focus restoration sequence. Checks wait for rendered
open/checked states instead of treating a click or DOM attribute alone as visual proof.

Expected failures remain visible in capture output: four unset-Quota 404s across two connects
and eight Cleanup-preview503s from the unconfigured actuator. These are negative-path checks,
not Cleanup success. The separate filter run records only its two initial Quota404s.
The existing ~516kB single-chunk build warning remains.

Final capture `focus-verified/browser-evidence.json`: eight filter matrix checks, 81 screenshots,
zero document overflows or mutation requests, no persisted bearer, and ten ordinary-token403s.
The separate `checks.json` contains nine successful checks with no page errors or filter-time
requests. Final build and all 24 tests passed again after the Escape/focus correction.

Stopped only this run's Vite and dev-stack processes. Confirmed their Control Plane/Worker
children exited, PostgreSQL container `cloud-agents-dev-501-19085` was removed, and temporary
state `.tmp/cloud-agents-dev.2ITy2Q` was deleted by the dev-script trap. These disposable records
and credentials have no retained backup; source and browser evidence remain. No existing
deployment, Kubernetes resource or SSH service was removed.

## Reproduce and remaining work

Use the fresh-stack/project/three-Target setup in
[shell evidence](admin-web-shell-scroll-20260905.md), then run the checked-in
`capture-actual.mjs OUTPUT ADMIN_TOKEN_FILE USER_TOKEN_FILE PROJECT_UID`.
Additional checks and screenshots are under `/tmp/cloud-agents-multi-filters.SA3lim/` and may
expire. The reproducible check is committed; transient output is not a permanent environment.

This closes the prior single-select/multi-select interaction gap. It does not claim full
cmdk fuzzy-ranking equivalence, screenshot-diff conformance of every state, or cross-browser
qualification beyond this Brave/Chromium run. ADMIN-M1's complete fixed screenshot comparison,
Overview metric/filter navigation and remaining resource-list gaps remain earliest. M2/M3
profile/policy runtime and M4 real Docker/Kubernetes/SSH Worker lifecycles, both Providers and
zero-residue proof are still required. No new credentials or destructive permission is needed
for the next in-scope slice. No push, image publication or Release.
