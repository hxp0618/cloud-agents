# Confirmation dialogs and modal focus — 2026-09-05

Base `b127b8e4adff294c11fcd4245b1aeced1dce3e6c`, branch
`codex/cloud-agents-platform-p0`. Previous turn was progress, not a wait/blocker.
This continues ADMIN-M1; neither M1 nor the Goal is complete.

## Reference and change

Revalidated fixed Daytona v0.190.0 source commit
`01c502bb1f1ff8f2885d0cd490e043736083dca8`. Inspected `ui/sheet.tsx`,
`ui/dialog.tsx` and `Organizations/DeleteOrganizationDialog.tsx`.
The design-style skill kept this on the pinned source, and the frontend debugging skill
required rendered interaction checks. No dependency or backend model was copied.

- Reused the native modal host. Target scheduling/Cleanup, Lease Upgrade/Rollback and Profile
  publish/disable confirmations now use a centered Dialog; resource details and creation stay
  in right-side Sheets. This corrects the prior all-Sheet presentation.
- Reference geometry: desktop maximum 512px, mobile viewport minus 32px, 80% viewport-height
  cap, 24px inset, 8px radius, 18px semibold title, 14px body, 50% overlay, 200ms fade/95% zoom.
  Mobile footer stacks in reverse order; bounded content scrolls without shrinking safety text.
- Preserved impact summaries, expected generation/resource version, review gates, API requests,
  idempotency and server-side authority. No lifecycle action is added or bypassed.
- Browser reproduction before the fix: both registration Escape and nested preview Escape
  failed to restore their triggers. Passive cleanup ran after DOM removal. Layout cleanup now
  closes while the dialog is attached. Async previews additionally capture their initiating
  control before `runOperation` disables it; each mounted confirmation retains that control.
  The native modal continues to handle focus containment and synchronous trigger restoration.

## Actual verification

- Fresh local PostgreSQL/Control Plane/Worker stack from `scripts/cloud-agents-dev.sh`, ports
  18085/18095, Admin Vite proxy on `http://127.0.0.1:4174`.
- New project `sheet-focus`, UID `project-74606d9a23a5bc6ea98341c5f501e803`; real API registration
  of `visual-docker`, `visual-kubernetes`, `visual-ssh`, using non-routable `.test` endpoints.
  These records are not evidence of infrastructure connectivity and were never probed.
- Checked-in CDP capture: 49 screenshots and eight confirmation scenarios, spanning
  en-US/zh-CN × light/dark × 1440×900/390×844. Checks include centered geometry, typography,
  checkbox gate, keyboard containment, registration/detail/nested focus restoration, existing
  shell/command regressions, locale persistence/fallback, no horizontal overflow or stored bearer.
  Ten ordinary-token Admin calls returned 403. Browser Admin requests were exclusively GET.
- Installed Playwright 1.62.1 + headless Brave: eight combinations passed repeated
  Escape/Cancel/close-button/backdrop dismissals, review check/uncheck, exact Target/generation/RV
  display, parent modality and focus restoration. At 390×360 the Dialog was 288px high,
  centered at y=36, with scrollable content and reachable actions. Normal motion was 200ms.
- No JavaScript page errors, blank page or framework overlay. The fresh project's unset quota
  and quota audit returned four expected 404s across two capture connections.
- Real Cleanup preview returned 503 `ENVIRONMENT_CLEANUP_UNAVAILABLE` because this stack has no
  cleanup actuator. The separate Playwright check verified the failure did not open a false
  confirmation or issue a lifecycle write. The failure alert becomes visible after closing the
  detail Sheet; its placement behind the modal remains a known M1 issue, not an accepted UX.
- Admin Web build/typecheck, 22 tests, scoped formatting/lint and diff checks passed. Existing
  single-chunk build warning remains (~507kB); this is not a warning-free build claim.

Browser plugin not available; reused the installed Playwright/browser and checked-in CDP
workflow without installing browser dependencies. Screenshots and detailed JSON are under
`/tmp/cloud-agents-sheet-focus.AAPmBg/`; they may expire. Reproduce using the fresh-stack and
three-Target procedure in [shell evidence](admin-web-shell-scroll-20260905.md), then run
`capture-actual.mjs OUTPUT ADMIN_TOKEN_FILE USER_TOKEN_FILE PROJECT_UID`. Its new modal checks
use real scheduling previews and fail if any browser Admin request is not GET.

## Evidence boundary and next work

Successful rendered confirmation evidence in this run covers Target Drain previews, not actual
Drain execution. Cleanup-success, Profile transition and Lease Upgrade/Rollback runtime paths
still require their real resource prerequisites; only their shared presentation wiring and
compilation are verified here. No Probe, deployment, lifecycle mutation, Provider Turn, image
publication, Release or push occurred.

Earliest unfinished remains ADMIN-M1: modal-local asynchronous error feedback, remaining
Header/component/state/interaction parity and a complete current pinned screenshot comparison.
This source-driven correction is not a full pixel-diff acceptance. M3 policy enforcement and
M4 real Docker/Kubernetes/SSH + Codex/Claude E2E and zero-residue evidence remain unqualified.
No additional credentials or destructive-operation approval is required for the next UI slice.
