# Admin Web mobile navigation — 2026-09-05

Base: `074abf6902029bcbdd13a17b02fbdb85bdd38a01`, branch
`codex/cloud-agents-platform-p0`. Previous Goal turn made progress (two committed,
browser-verified shell/form corrections). This slice continues ADMIN-M1, not completion.

## Authority and reference

Revalidated Daytona `v0.190.0` at `01c502bb1f1ff8f2885d0cd490e043736083dca8`.
`apps/dashboard/src/components/ui/sidebar.tsx` uses a modal Sheet for mobile navigation,
with a 288px width and desktop/mobile state separated. The prior Cloud Agents offscreen
aside and overlay button did not supply equivalent modal or hidden-focus semantics.

The design-style and frontend-testing-debugging skills kept this on the pinned design and
required real rendered checks. No substitute template, UI framework or dependency was added.
The existing generated SDK/Admin API remains the resource authority; no API or User Web
contract adjustment is needed for this shell interaction.

## Changes and verification

- Mobile navigation now uses native dialog modality: hidden controls cannot receive focus,
  the active route receives initial focus, Escape/backdrop/navigation close it, and native
  focus restoration returns to the trigger. Routes expose `aria-current="page"`.
- Ctrl/Cmd+B toggles the mobile Sheet at the mobile breakpoint and the desktop collapse state
  on desktop. It does not open navigation over another resource/confirmation dialog.
- Desktop collapse CSS is limited to desktop, preserving mobile brand/labels and 288px width.
  Crossing back to desktop clears the mobile modal without changing desktop collapse choice.
- Reduced-motion rules now include `::backdrop`; computed navigation backdrop animation
  duration was 0.01ms. The normal 250ms entrance remains.
- A fast Escape → shortcut test exposed a queued native close-event race during implementation.
  Removing the redundant close-event state update leaves open/close controlled by React;
  the same checked-in test then passed in English and Chinese.
- Temporary Playwright checks passed all four locale/theme combinations: 1440×900 collapsed
  desktop → 390×844 mobile → desktop, active focus, 14 Tab presses without entering background
  controls, Escape focus restoration, keyboard opening, selecting a different route and
  resize while open. A fifth check verified that the shortcut leaves a resource dialog modal
  without opening navigation behind it. Zero JavaScript page errors and no framework overlay.
- Updated `capture-actual.mjs` passed 25 screenshots plus live shell, form, locale, native modal
  keyboard/backdrop and permission assertions. All document-width checks passed; the bearer
  was not persisted; ten ordinary-token Admin requests returned 403.
- Admin Web TypeScript/Vite build and 20 tests passed. The existing ~503kB bundle warning and
  four HTTP 404s for the fresh project's unset Quota/audit resource are retained, not hidden.

Browser plugin not available: used the installed Playwright/Brave and existing CDP workflow.
Only fresh local PostgreSQL/Control Plane/Worker state was created. The new project held
three real API-persisted `visual-docker`, `visual-kubernetes`, `visual-ssh` Targets with
non-routable test endpoints; all remained unprobed. This is not target/Provider E2E evidence.

Reproduce using the live-stack and three-Target setup in
[the shell evidence](admin-web-shell-scroll-20260905.md), then run the checked-in capture
with fresh admin/user token-file paths and project UID. Current raw captures and the four
Playwright checks are under `/tmp/cloud-agents-admin-navigation.IfcsIa/`; temporary outputs
may expire. The assertions remain in the repository.

## Remaining

ADMIN-M1 remains earliest incomplete: grouped navigation/command palette, remaining Header,
component/state fidelity and complete pinned visual comparison. Real Probe-ready acceptance
and M4 Docker/Kubernetes/SSH plus Codex/Claude Code E2E are not proven by this run. No existing
infrastructure cleanup, Provider credentials, image publishing, push or Release was used.
