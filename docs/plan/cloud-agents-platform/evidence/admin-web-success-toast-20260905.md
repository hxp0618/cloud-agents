# Admin successful-operation Toast — 2026-09-05

Source base `2309832c59906ab72030690c7cb0c5a7f5147a96`, unchanged branch `codex/cloud-agents-platform-p0`.

## Delivered behavior

The shared `runOperation` success result now renders a dismissible floating Toast, instead of
a persistent in-flow green Banner which shifted the resource list. Existing localized messages,
API operations, error details, in-progress feedback and dangerous-operation confirmations remain.
No backend contract or dependency changed; the existing operation trigger reference is reused.

Native Popover plus a React portal keeps the Toast operable inside the active native modal.
The portal destination is resolved after React commits, avoiding a dialog removed in that commit.
Explicit dismissal restores an eligible trigger inside the current modal, or the previous modal
focus. Automatic expiry uses four seconds of unpaused time; hover, focus and document visibility
pause the timer. The live-region message and close label use the existing bilingual catalog.

## Reference and comparison

Pinned upstream `01c502bb1f1ff8f2885d0cd490e043736083dca8`:
`apps/dashboard/src/components/ui/sonner.tsx`, its installed Sonner styles and ThemeProvider.
No tracked upstream source was changed. A reference-only composition imports those actual
components; no Storybook/Sonner/Tailwind dependency enters Cloud Agents.

- Retained reference composition, four screenshots and computed values:
  `apps/admin-web/visual-baseline/daytona-v0.190.0/reference-toast*`.
- Actual size: 356px wide, 32px right/bottom inset on desktop; viewport minus 32px wide and
  16px inset on mobile. Both use 13px/19.5px type, 16px padding, 8px radius and a 1px border.
- The existing native screenshot tooling was extended; `compare-toast.mjs` compares fixed
  viewport pixels with no alignment or tolerance. Only the title/icon interior is masked;
  borders, shadow, padding and the complete close control remain checked.
- [Eight current pixel results](admin-web-success-toast-pixels-20260905.json): six match exactly.
  Both light/desktop cases differ at one pixel, `(1405,865)`, with reference RGB 238 and actual
  RGB 239. This is a lower-right rounded-edge raster difference; its precise rendering cause is
  not proven. The strict comparator remains failing for those two cells—no tolerance was relaxed.

This is not full Toast or page visual approval. Entry/exit motion, swipe interaction and background-tab
pause still need rendered qualification; the current slice does not claim complete Daytona fidelity.

## Executed validation

Browser plugin and agent-browser CLI were unavailable. Reused the installed Brave/CDP harness
and Playwright for independent modal pointer checks, with reduced motion and exact localhost routing.

- Fresh real PostgreSQL/Control Plane/Worker stack, Vite `http://127.0.0.1:4174/`, project
  `project-aa0b985db3ce5e397fd43ea2688c7e96`, three SDK-registered unprobed Targets. No API interception.
- Final current-source capture: `/tmp/cloud-agents-toast.bNAfnt/accepted-functional/`, 113 states,
  eight locale/theme/viewport Toast checks and eight existing modal/confirmation checks passed.
  Manifest SHA-256 `a7a4461b59d9628a41ec4cd432860dfed9c631cfe6e53a76ec34adfb58c81b42`.
- Each Toast test performs actual Admin refresh, checks no list displacement, geometry and font,
  focuses its close control for over four seconds, dismisses using Enter, checks trigger focus,
  then performs another refresh and verifies automatic expiry. Desktop hover pause is also exercised.
- Every detail matrix cell pointer-dismisses the Toast while retaining the modal and focus inside it.
  An independent Playwright test reproduced the original inert-layer problem and then passed the fix.
- Existing keyboard, locale reload/fallback, resource filter, modal impact gate and error-recovery
  checks passed. Only the Vite network origin; no browser mutation requests or warnings.
- Retained expected HTTP errors: 36 quota-not-found 404s (refresh now exercised repeatedly), eight
  unconfigured cleanup-preview 503s; ten ordinary-user Admin reads returned 403. Not a zero-console-error claim.
- All 30 Admin tests, scoped lint/format, TypeScript and production build passed. Existing >500kB
  bundle warning remains. References are excluded from the application TypeScript include/build.

Earlier attempts exposed native-button Enter event incompleteness in the CDP helper, modal pointer
interception, stale portal destinations and focus restoration outside a modal. These were corrected
without weakening the existing modal assertions. One later run ended with an unsettled await after
97 images; a socket-close rejection was added and two subsequent full runs passed. Its earlier
browser termination cause is unknown; partial captures are not acceptance evidence.

## Cleanup and remaining work

Stopped owned Vite PID 67699, Storybook PID 70602 and dev-stack parent PID 67659. The stack removes
only its disposable database container, runtime and directory. Screenshots and reference checkout
remain; no existing infrastructure resource, Provider credential or unrelated dirty work was changed.

Earliest incomplete ADMIN-M1 item remains full visual/interaction acceptance, including the strict
Toast mismatch above. Current backend three-transport Probe evidence remains in the preceding
refresh record. No Worker deployment, Provider Turn or final zero-workspace-residue acceptance was
performed here. No new credentials or destructive-operation approval were required.
