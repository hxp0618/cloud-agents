# Target list/detail/create visual matrix — 2026-09-05

Starting source: `fab7ca38d79fc28f8346daa258a818c63c0eb79e`, branch
`codex/cloud-agents-platform-p0`. This acceptance slice changes only the existing capture
script and this evidence. Contract, Control Plane, SDK, User Web and Admin production code
already support these states; no new abstraction, dependency or mock was needed.

## Change

The existing eight locale/theme/viewport runs now all capture Target detail and registration
Sheet, replacing four duplicated partial runs. A final assertion requires exactly one matching
capture for each of 24 list/detail/create combinations, including the actual locale, theme and
viewport metadata. Registration autofocus, Escape close and visible sticky footer checks run
in all eight combinations.

The first replay failed because the filter check still selected `.table-empty`, removed by the
earlier dedicated Target empty-state implementation. The check now uses `#target-empty-title`
and activates the empty-state's own clear button, then verifies all three real rows return.
No application behavior was changed to satisfy the test.

## Actual verification

- Owned disposable stack: `.tmp/cloud-agents-dev.ls3vaK`, PostgreSQL container
  `cloud-agents-dev-501-29582`, Control Plane `127.0.0.1:18085`, Worker `127.0.0.1:18095`,
  Vite proxy `127.0.0.1:4174`. Migration `000052`, 52 applied.
- Project `project-2a019f774d233d154206985fe2a543df` created through the CLI. Generated SDK
  registered `visual-docker`, `visual-kubernetes`, `visual-ssh` through Admin API with opaque
  unconfigured credential references and `.test` endpoints. Docker/Kubernetes use `https:`,
  SSH uses `ssh:`. A mistaken HTTPS SSH seed was rejected by the SDK before HTTP; the corrected
  SSH request succeeded.
- Final capture at `2026-09-05T09:25:56.258Z`: **105 screenshots**, 24 required matrix cells,
  eight command/filter/modal/overview scenarios each, ten sticky-form checks (eight Target
  forms and two existing Release forms). No document-width overflow. Both languages,
  light/dark, 1440×900 and 390×844; existing short-window checks also passed.
- Inspected all eight newly covered dark-desktop and light-mobile detail/create screenshots.
  Labels and values remain readable, mobile Sheets fit the viewport, and creation actions
  remain visible. This is current-app visual QA, not upstream 1:1 approval.
- Locale switches immediately, Chinese preference survives reload, invalid locale falls back
  to English, no visible message keys, bearer input empty after reload.
- Browser origin only `http://127.0.0.1:4174`; no lifecycle mutation requests. Ten ordinary-user
  Admin reads returned 403. Real cleanup previews returned eight expected 503 errors; four
  expected 404s came from absent quota/quota-audit resources across two connections. These
  twelve resource errors remain in the evidence; no console warnings. Not a zero-error claim.
- Independent PostgreSQL check after capture: all three Targets remain generation 1,
  `unprobed`, scheduling `active`. No infrastructure endpoint was probed.
- Admin 30 tests, TypeScript, production build, focused lint, formatting and diff checks passed.
  Existing 543.46 kB JavaScript chunk warning remains.

Raw failed attempt and successful replay are in `/tmp/cloud-agents-visual-matrix.4Oq0CO/`;
successful images and `browser-evidence.json` are under `replay/`. JSON SHA-256:
`9e040a1b74c53cb0291a8ed5091d74e58fa43102c4c354c52a71f00c72122564`.
Temporary artifacts may expire. Reproduce with the owned-stack/three-Target setup documented
in `admin-web-shell-scroll-20260905.md`, then:

```sh
node apps/admin-web/visual-baseline/daytona-v0.190.0/capture-actual.mjs \
  NEW_OUTPUT ADMIN_TOKEN_FILE USER_TOKEN_FILE PROJECT_UID
```

The stack's own shutdown trap removed its temporary database, project, Target records, tokens,
workspace and processes; its Vite process was stopped. No pre-existing resource was touched.
Browser profiles were moved to Trash by the existing capture cleanup, screenshots retained.

## Earliest incomplete and boundary

ADMIN-M1 still needs a complete fixed-upstream side-by-side/automatic screenshot diff and
current Docker/Kubernetes/SSH register-to-Probe-ready evidence. Existing upstream mobile
create reference visibly clips the Sheet offscreen; investigate capture settling/composition
against pinned source before using it as a geometry gate. Do not regenerate references to
match our app or call this matrix completion Daytona parity.

This run did not deploy managed Workers, run Codex/Claude turns or qualify M4 zero-residue
E2E. No real Provider credentials or destructive existing-resource approval was required.
Ponytail kept the existing harness; frontend-testing-debugging required real authority,
browser interaction and image inspection instead of screenshot-count-only acceptance.
