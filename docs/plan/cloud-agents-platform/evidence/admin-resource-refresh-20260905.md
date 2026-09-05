# M1 Target / Lease real refresh feedback — 2026-09-05

Base `fc601b3`, existing `codex/cloud-agents-platform-p0` branch. Only this
slice's files are committed; unrelated dirty files and staged HTML are retained.

## Change

Target and Lease manual Refresh now show six decorative skeleton rows only
while the existing `operation.refresh` request is pending. The last successful
snapshot stays mounted behind native `hidden`, preserving Target pagination.
Settling the request removes the skeleton and reveals the successful new
snapshot or retained old snapshot on failure/abort. Existing operation status,
cancel-wait and error handling remain the authority for request feedback.
No API, SDK, request, persistence or dependency changes were needed.

Native CSS follows pinned Daytona v0.190.0 `components/ui/skeleton.tsx` and
`index.css`: 6px radius, muted/light and muted60%/dark, 1.8s neutral shimmer,
translateX(-100% to250%), reduced-motion shimmer removal. Existing translated
table headings and status messages are reused. `design-style` fixed-reference
guidance determined the component styling; React/Ponytail guidance kept the
existing request state and mounted pagination instead of adding another cache.

## Executed evidence

- Admin Vitest:30 tests passed, including one runnable two-state render check
  for aria-busy, hidden retained children, decorative skeleton and row count.
- Admin TypeScript + Vite production build passed; existing >500kB chunk
  warning remains. Scoped oxfmt/oxlint and `git diff --check` passed.
- Fresh `scripts/cloud-agents-dev.sh` stack, real PostgreSQL and Control Plane,
  CP18085, Worker18095, Admin Vite proxy4174. Owned state
  `.tmp/cloud-agents-dev.4Z0cTM`, project
  `project-3e16fe35db47b5a4a5ee60dcdf93ad91`. Browser used
  `http://127.0.0.1:4174`; no request interception, Mock data or React state
  injection. agent-browser unavailable; existing in-app browser used.
- Registered `refresh-target` through the actual Admin UI, without Probe.
  Independent generated SDK read confirmed generation1/unprobed and one
  succeeded registration Audit, operation
  `op-59361b0e207d0851d60c3e758f947d5b`.
- Selected100 rows/page and searched `refresh`. Paused only the owned CP
  process65616 (`SIGSTOP`), then pressed Refresh: aria-busy=true, six rows,
  hidden=true, underlying page-size100. Resumed with `SIGCONT`: real requests
  completed; skeleton count0, hidden=false, Target still generation1,
  search and page-size preserved, success notice shown.
- Inspected all eight Target pending layouts: zh-CN/en-US × light/dark ×
  1440x900/390x844. Locale/theme switched during the same pending request.
  Document width stayed1440/390; mobile table scroll stayed inside the panel.
  This is scoped loading-state QA, not the complete fixed-page visual gate.
- Paused owned CP again, Refresh then Cancel wait: skeleton removed, prior
  Target/filter/page-size restored, existing honest stopped-waiting notice
  shown. No cancellation of a server-side mutation is claimed.
- While paused, visited Lease list and refreshed: six rows/six columns, old
  real empty Lease snapshot hidden. After resume, the short-lived browser
  token had expired: actual `AUTHENTICATION_FAILED` appeared, aria-busy=false,
  zero skeletons, previous snapshot retained. This proves failure recovery,
  not a successful Lease deployment or renewed-authentication flow. Browser
  warning/error log query was empty.
- Reduced-motion CSS is present; OS reduced-motion runtime testing was not
  performed in this slice. Cancel-wait focus was observed returning to body;
  the shared operation feedback needs a subsequent focus-restoration fix.

## Reproduction and boundary

Start a fresh disposable dev stack with CP18085/Worker18095 and Admin proxy
4174. Create a project through the real CLI, connect with its ephemeral Admin
token and register an unprobed Target. Set filter and page size. Resolve the
exact owned Control Plane PID from the dev script; pause only that PID before
Refresh, inspect pending state, then always resume it. Repeat with Cancel wait.
For auth failure, let the browser token expire before releasing a pending
request. Never pause an existing/shared Control Plane or Probe the deliberately
unconfigured Target. Use the existing generated SDK to re-read Target/Audit.

M1 is still earliest: shared cancel-wait focus, remaining resource states and
full pinned visual/interaction acceptance. This check does not qualify M2-M4,
OIDC deployment isolation, any Docker/Kubernetes/SSH deployment or cleanup,
Codex/Claude Turn, or the overall Goal. No new credentials or permission for
pre-existing resources were required.
