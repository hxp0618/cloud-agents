# Admin Web Sheet footer — 2026-09-05

Base commit: `be16217`; same branch and isolated live stack as
[the shell slice](admin-web-shell-scroll-20260905.md). This is a follow-on ADMIN-M1 correction,
not a milestone completion.

## Reproduced cause and fix

The Target create Sheet was 900px high but its footer ended at y=623. The shared resource form
used a start-aligned Grid, so the footer's auto margin did not consume remaining form height.
Pinned Daytona `01c502bb1f1ff8f2885d0cd490e043736083dca8`
`CreateRunnerSheet.tsx` places the footer after a flexing, independently scrolling body.

Eight CSS lines now make only `.dialog > .resource-form` a column Flexbox and prevent its
children from shrinking. This applies to the existing create and confirmation Sheets while
leaving inline Storage/Network/Quota editor forms unchanged. Native form validation, API
submissions, expected-generation checks and confirmation gating are unchanged.

## Verification

- The pre-fix browser assertion failed at footer bottom 623 vs viewport 900.
- After the fix, the checked-in capture completed 23 screenshots. Six create-form captures
  explicitly asserted the footer reaches the viewport bottom and remains visible:
  English/Chinese Target, light desktop/dark mobile; Release desktop and 390×360 short mobile.
- A separate temporary Playwright check exercised Target and Release create forms in both
  locales at 1440×900, 390×844 and 390×360: all 12 footer/overflow checks passed. Long Release
  forms remained scrollable; Escape closed the Sheets.
- A thirteenth browser check opened the real Docker Target scheduling preview at 390×360.
  Its footer reached y=360; confirmation was disabled before the impact checkbox and enabled
  afterward. The Sheet was closed with Escape without submitting Drain. No Target changed.
- The capture still verifies shell geometry, language persistence/fallback, no persisted bearer,
  no horizontal document overflow and ten ordinary-token Admin requests returning 403.
- Admin Web TypeScript/Vite build and all 20 tests passed. The pre-existing ~502kB bundle warning
  and four expected unset-Quota HTTP 404s remain explicitly outside a zero-warning/error claim.

Reproduce with the same live-stack setup and `capture-actual.mjs` invocation documented in the
shell slice. Raw output: `/tmp/cloud-agents-daytona-baseline.p4J5TI/footer-verified/`.
The script now fails on footer regressions rather than merely recording screenshots.

Profile creation stayed disabled because this test project has no Release/Storage/Network
prerequisites. No UI guard was bypassed, no mock resource was injected, no Profile was published,
and no real infrastructure deployment or Provider Turn is claimed.

Earliest remaining work is still ADMIN-M1: grouped navigation/command palette, Header and
remaining component/state fidelity, mobile focus behavior, and the complete current pinned
visual comparison. M3 policy enforcement and M4 real multi-target/Provider E2E remain open.
