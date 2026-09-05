# Admin Toast motion — 2026-09-05

Source base `a6f3811d0c186aa3ee7b16eba06055b4683fd70f`, branch unchanged.

## Delivered slice

The existing successful-operation Toast now enters with native CSS starting styles and exits
with a leaving state. Pinned Daytona v0.190.0's installed Sonner uses 400ms opacity/transform
transitions and a 200ms removal delay. Reduced-motion users receive neither transition nor
the removal delay. Existing hover/focus/visibility lifetime pauses and modal focus handling remain.
No dependencies, API contracts or backend models changed.

The reusable `visual-baseline/daytona-v0.190.0/check-toast-motion.mjs` exercises actual Admin
refresh requests, not intercepted responses. It checks the browser's running transitions,
settled opacity, delayed removal and restored trigger focus in 16 locale/theme/viewport/motion
combinations. Native CSS and the already-installed browser tooling were sufficient.

## Current validation

- Fresh disposable PostgreSQL/Control Plane/Worker stack and independent Vite Admin entry.
  Project `project-3265d24b3c8b21337dd6b0ce2709ab80`; three SDK-registered, unprobed Targets.
  No Provider credentials, Probe, deployment or lifecycle writes were used in browser checks.
- Final 16-case motion run passed, with zero page exceptions. Screenshots and machine output:
  `/tmp/cloud-agents-toast-motion.nrbfTo/motion-final/`.
- Full browser regression passed 113 screenshot/layout states, eight Toast and eight modal
  checks, including expiry, focus pause, pointer dismissal inside a modal and confirmation gates.
  Output: `/tmp/cloud-agents-toast-motion.nrbfTo/full-retry/`.
  Manifest SHA-256 `0631ad26f923b43b5aa06732d5bc7a9256c965dceccbf734ff84a9de0df76903`.
- Only the Vite origin was requested, no browser mutation requests, no persisted bearer token.
  Expected failures remain: 36 missing-quota 404s, eight unconfigured cleanup-preview 503s,
  and ten ordinary-user Admin reads returning 403. This is not a zero-console-error claim.
- Initial full run failed because the lifecycle action selector was absent after the harness
  had waited only for the Sheet shell. Added an explicit action-rendered wait before dereferencing
  it; the subsequent full run passed. That observation does not prove the precise scheduling cause.
- Scoped lint/format, all 30 Admin unit tests, TypeScript and independent production build passed.
  The existing >500kB bundle warning remains.
- Visually inspected settled English/light desktop and Chinese/dark mobile screenshots.
- Strict existing Toast-exterior comparison still passes six of eight cells; both light/desktop
  cells differ by one pixel. No masks, tolerances or reference images were changed.

Compact durable results: [motion and regression evidence](admin-web-toast-motion-20260905.json).

Reproduce against a disposable stack serving Admin at port 4174:

```sh
node apps/admin-web/visual-baseline/daytona-v0.190.0/check-toast-motion.mjs \
  ADMIN_TOKEN_FILE PROJECT_ID NEW_OUTPUT_DIRECTORY INSTALLED_PLAYWRIGHT_MODULE
node apps/admin-web/visual-baseline/daytona-v0.190.0/capture-actual.mjs \
  NEW_OUTPUT_DIRECTORY ADMIN_TOKEN_FILE USER_TOKEN_FILE PROJECT_ID
```

## Boundaries and cleanup

Headless Brave reports `visible` both when another tab is foreground and after CDP
`Page.setWebLifecycleState(frozen)`. Background visibility pause is therefore **not qualified**;
no fake visibility property or synthetic event was used to claim it. Swipe and complete Daytona
visual/interaction acceptance remain open, including the strict light/desktop pixel mismatch.

Stopped only this slice's Vite PID 88690 and dev-stack parent PID 88666; the stack trap removes
its disposable database `cloud-agents-dev-501-88666`, Worker and temporary workspace/state.
Screenshots remain in the temporary evidence directory. No existing infrastructure was cleaned.

Earliest incomplete milestone remains ADMIN-M1 full visual/interaction acceptance. This slice
does not qualify Worker deployment, Docker/Kubernetes/SSH lifecycle E2E, real Codex/Claude Turns
or final platform-owned workspace residue. No additional credentials or destructive approval
were needed for this slice; the Goal remains active.
