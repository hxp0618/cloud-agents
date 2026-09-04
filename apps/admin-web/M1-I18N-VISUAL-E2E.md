# M1 Admin Web i18n and Visual E2E

Date: 2026-09-04

Status: passed for the M1 `zh-CN`/`en-US` language and Daytona visual-regression slice. Together with the checked-in Target, Lease, Cleanup, Operation, and Audit evidence, this closes the implementation and evidence currently assigned to M1. It does not complete the Section 15 acceptance matrix.

## Build under test

- Branch: `codex/cloud-agents-platform-p0`, starting HEAD `d89118ca0fca1fe2efa261a316d6496f0d7d743e`.
- Admin Web: Vite + React + TypeScript, native CSS, generated Admin SDK, and a local typed message catalog; no i18n or UI dependency was added.
- Runtime: fresh local Control Plane on `127.0.0.1:18085`, Worker listener on `127.0.0.1:18095`, and Admin Web on `127.0.0.1:4174`.
- Authority: PostgreSQL-backed project `project-27b2dc88ec99c1a43118a66ad96b6a5f` with three Targets created through the real Admin API: Docker, Kubernetes, and SSH.

## Language behavior

- Every catalog key is required in both `zh-CN` and `en-US`; the catalog-completeness test fails on a missing key.
- The first browser language selects `zh-CN` only for a leading `zh` language and otherwise selects `en-US`.
- The account/settings selector changes locale without a reload and persists only `cloud-agents-admin-locale`.
- A reload restored `zh-CN`. An injected unsupported `fr-FR` value normalized to and persisted `en-US`.
- The browser password field was empty after reload. The bearer token was not present in local or session storage.
- Dates, times, and numbers use browser-native `Intl`; resource IDs, API fields, stable error codes, and collapsed original diagnostics remain untranslated.
- The production translation fallback returned readable English text, and the browser scan found no visible catalog key.

## Browser and visual verification

`visual-baseline/daytona-v0.190.0/capture-actual.mjs` drove real Chromium against the running app and API. It captured the checked-in English and Chinese screenshots under `visual-baseline/daytona-v0.190.0/actual/`.

- Both locales cover light and dark themes at `1440x900` desktop and `390x844` mobile viewports.
- The 20 recorded layout checks all reported document width equal to viewport width and `overflow: false`.
- The sidebar shortcut, dropdown Escape/outside dismissal, Sheet Escape dismissal, create-form autofocus, mobile navigation, immediate locale switch, reload restore, and invalid-locale fallback all passed.
- Connected-state console errors, warnings, and unexpected HTTP failures were empty.
- Browser requests used only the Admin Web origin. Chromium did not connect directly to Docker, Kubernetes, or SSH endpoints.
- An ordinary user token received HTTP `403` from the Target, Lease, and Profile Admin list routes.
- Manual visual review covered English and Chinese desktop lists, Chinese desktop create/detail states, and Chinese mobile navigation/detail states. The Daytona shell geometry, hierarchy, density, badges, actions, and responsive Sheet behavior were preserved without reducing typography.

Machine-readable browser evidence and screenshot hashes are recorded in `visual-baseline/daytona-v0.190.0/README.md` and `visual-baseline/daytona-v0.190.0/actual/browser-evidence.json`.

## Source verification

The following checks passed with the repository-pinned Bun/Node toolchain:

```text
bun run --cwd apps/admin-web typecheck
bun run --cwd apps/admin-web test          # 2 files, 10 tests
bun run --cwd apps/admin-web build
bun run lint
oxfmt --check on the changed Admin Web TS/TSX/CSS/capture sources
git diff --check
```

## Teardown and evidence boundary

- Ports `4174`, `18085`, and `18095` were closed; the temporary local-dev state directory was removed; the capture browser was closed.
- Label-filtered Docker inventory contained no `cloud-agents.dev-run` container.
- The three visual-regression Target endpoints and opaque credential references were synthetic and remained unprobed. This run proves persisted Admin API rendering, language behavior, responsive visuals, browser routing, and ordinary-token rejection—not Target connectivity.
- Real Docker registration/Probe is recorded in `M1-TARGET-ORBSTACK-E2E.md`; real Kubernetes and SSH registration/Probe are recorded in `M1-TARGET-MATRIX-E2E.md`; Lease, Cleanup, Operation, and Audit evidence is recorded in the other `M1-*.md` files.
- Profile-backed real Agent startup, Worker lifecycle actions, Docker/Kubernetes/SSH deployment and final cleanup, and current Codex/Claude Code Turn E2E remain outside this slice and are still required by Section 15.
