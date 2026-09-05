# Native background Toast lifetime — 2026-09-05

Source base `1cdfe04d`, branch `codex/cloud-agents-platform-p0` unchanged.
This closes the background-visibility qualification gap in the preceding Toast motion record.
Product behavior, backend contracts and dependencies were not changed.

## Reproducible flow and result

The new `check-toast-visibility.mjs` runs against a real disposable Control Plane and PostgreSQL:
connect to Admin at `http://127.0.0.1:4174`, refresh current authority, allow 700ms foreground
time, activate another owned tab, remain natively hidden for 4500ms, return and observe expiry.
No request interception, synthetic visibility event, overridden DOM property or fake clock is used.

- Eight cases passed: zh-CN/en-US × light/dark × 1440×900/390×844.
- Every case recorded exactly `hidden`, then `visible`; there was no intervening visibility event.
- Toast stayed open and not leaving throughout the background interval, measured 4508–4516ms.
  After returning, observed DOM removal occurred in 3853–3892ms, including polling latency.
  Checks also exclude focus/hover as the initial cause of timer suspension.
- The real API returned the empty project state. Browser requests stayed on the Vite origin;
  no browser mutations or page exceptions occurred. The 32 expected 404 responses were split
  equally between this project's absent lease quota and its audit endpoint. Other errors fail.
- English/light desktop and Chinese/dark mobile resumed screenshots were visually inspected.
  The page remained usable and the Toast visible. This does not requalify strict pixel fidelity.
- Script syntax, scoped lint/format and `git diff --check` passed. No product build or full
  113-state regression was rerun: this commit changes only verification and evidence.

Browser plugin was unavailable. Reused installed Playwright with a dedicated Brave process and
profile, connecting via CDP with `noDefaults: true`. Playwright's normal default-context focus
emulation otherwise causes visibility checks to observe `visible`. The final headless run uses
native tab activation and visibility; a visible browser is not needed for reproduction.

Earlier headed business verification did not maintain the hidden state and failed. Two preliminary
headless matrices completed lifetime checks but failed their incomplete HTTP whitelist because
the missing quota audit endpoint also returned 404. These are not accepted runs. An independent
real request confirmed that path before adding it to the exact project-specific whitelist.

Accepted output: `/tmp/cloud-agents-visibility.3K8kMl/verified/`.
Raw JSON SHA-256: `f7126f6e8f957769a0038874b1e651564a9b46eb3c9724222ae704579ea258c5`.
Durable compact results: [eight visibility checks](admin-web-toast-visibility-20260905.json).

```sh
node apps/admin-web/visual-baseline/daytona-v0.190.0/check-toast-visibility.mjs \
  ADMIN_TOKEN_FILE PROJECT_ID NEW_OUTPUT_DIRECTORY INSTALLED_PLAYWRIGHT_MODULE
```

Use a disposable stack and fresh empty project. The script refuses to overwrite an output
directory, closes its own browser, and moves its own temporary browser profile to Trash.

## Cleanup and remaining acceptance

Stopped owned Vite PID 97070 and dev-stack parent PID 96246. The stack trap removed database
`cloud-agents-dev-501-96246`, Worker and `.tmp/cloud-agents-dev.3PfCVQ`, including its temporary
workspace and tokens. No pre-existing infrastructure was touched; screenshots remain available.

Earliest incomplete item is still M1 full Daytona visual/interaction acceptance, including swipe
dismissal and the previously recorded strict light/desktop pixel mismatch. Docker/Kubernetes/SSH
Worker lifecycle and real Codex/Claude E2E remain separate unqualified gates. No new credentials
or destructive-operation permission were required, and the Goal remains active.
