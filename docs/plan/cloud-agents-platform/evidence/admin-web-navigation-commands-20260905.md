# Grouped navigation and navigation commands — 2026-09-05

Base source: `a3a6110c6799b84ff9cbfbff77297ff71f126052`, same
`codex/cloud-agents-platform-p0` checkout. The previous turn was progress, not a wait/blocker.
This slice continues ADMIN-M1; it does not complete the Goal.

## Pinned reference and implementation

Revalidated Daytona `v0.190.0` commit `01c502bb1f1ff8f2885d0cd490e043736083dca8`.
Inspected `Sidebar.tsx`, `CommandPalette.tsx`, and `ui/sidebar.tsx`, `ui/command.tsx`,
`ui/dialog.tsx` from that checkout, not the current website.

- Sidebar groups use 8px padding, 4px row gaps, 12px-inset separators, a 32px search entry and
  platform keyboard hint. Existing ten resource pages retain their order and real authority.
- One typed route list now drives both navigation and commands. Three groups are Resources,
  Configuration and Operations; only their accessible labels are shown to assistive technology,
  matching the reference's visually unlabelled groups.
- Ctrl/Cmd+K or the search entry opens a native modal. It excludes the current page, searches
  localized command words, wraps arrow selection, executes Enter and closes with Escape.
  Empty results do nothing; Enter during IME composition does not navigate.
- Palette dimensions follow the source: 576px desktop width, 48px input, 14px input text,
  20px icons, 44px options, a 400px scrolling result area, 12px corners and an 80% overlay.
  It is 358px wide at the 390px viewport. Short-window placement is clamped onscreen.
- The reference palette specifically has translucent dark popover/blur and a shadow; these
  are scoped to this component, not applied as a new product theme. Its 200ms entrance and
  300ms scale respect reduced motion.
- The command dialog closes in layout-effect cleanup before DOM removal, preserving native
  focus restoration. A browser assertion caught the earlier passive-cleanup focus failure.
- No lifecycle command bypasses preview/confirmation, no user content is indexed, and no search
  query is sent to the server or persisted. No new dependency, API contract, SDK or User Web
  change was needed: commands select existing real Admin API-backed pages.

The design-style and frontend-testing-debugging skills required the pinned visual source and
live interaction checks; React guidance kept state local and reused one route list.

## Actual checks

- Fresh `scripts/cloud-agents-dev.sh` PostgreSQL/Control Plane/Worker stack, ports 18085/18095;
  Admin Web on 4174 through its Admin API proxy.
- New project `commands-check`; real API registration of Docker/Kubernetes/SSH visual-only
  Target records. All remain unprobed with non-routable test endpoints.
- Installed Playwright 1.62.1 + headless Brave: eight locale/theme/viewport combinations
  (zh-CN/en-US × light/dark × 1440×900/390×844) passed width, modality, input focus,
  arrow wrap, focus restoration, empty results, IME guard and real three-Target rendering.
  All ten resource routes were selected through the command options and rendered content.
- Extra checks passed normal motion, pointer backdrop close, 390×360 containment and the guard
  against opening commands over a resource Sheet. No JavaScript page errors or framework overlay.
- Checked-in `capture-actual.mjs`: 41 screenshots, eight command scenarios, shell/form/mobile
  regressions, language persistence/fallback, no horizontal document overflow and no persisted
  bearer. Ten ordinary-token Admin requests returned 403.
- Admin Web build/typecheck and all 22 tests (three files) passed. Existing large-chunk warning
  (~506kB) and four expected unset-Quota/audit 404s remain explicitly recorded.

Browser plugin not available; used the installed browser runtime and existing CDP capture,
without installing a browser dependency. Raw output is under
`/tmp/cloud-agents-command-palette.k8KrOf/`; it may expire.
Reproduce with the fresh-stack, project and three-Target procedure in
[the shell evidence](admin-web-shell-scroll-20260905.md), then run the checked-in capture with
the new project UID and local admin/user token-file paths. The capture now uses stable
`data-page` route selectors and waits for hit-testable controls after responsive transitions.

## Boundary

This is real API-backed navigation evidence, not real Docker/Kubernetes/SSH connectivity or
Provider Turn evidence. No infrastructure Probe, deployment, cleanup mutation, Release,
image publication or push was performed.

ADMIN-M1 still needs remaining Header/component/state/interaction parity and a complete current
fixed-reference visual comparison. This source-driven slice does not claim a full pixel-diff
pass, exact cmdk fuzzy ranking, or all contextual command actions. M2/M3 runtime acceptance and
M4 real multi-target, both-Provider E2E and zero-residue proof remain required.
