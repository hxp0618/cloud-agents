# Mobile Sheet actions and reference diagnosis — 2026-09-05

Starting HEAD `2d6da522f801a3d6574fc4799442edc4385314ae`, same
`codex/cloud-agents-platform-p0` branch. Production change: one shared mobile selector now
applies the existing column-reverse/full-width action layout to all Admin Sheets, and the
mobile override removing the left border is deleted. Desktop actions, mutation handlers,
confirmation gates, SDK, Control Plane and User Web are unchanged. No dependency was added
to Cloud Agents. Ponytail kept the shared CSS fix; design-style and frontend QA required
pinned-source and rendered evidence rather than a new visual design.

## Reference defect and authoritative comparison

Rechecked clean upstream source at `01c502bb1f1ff8f2885d0cd490e043736083dca8` in
`/tmp/cloud-agents-daytona-baseline.p4J5TI/source`. Recovered our original neutral Storybook
composition from this task's September 3 execution record. It imports genuine upstream UI
components, not Cloud Agents CSS. Only this temporary reference checkout received the
upstream lockfile's dependencies (`yarn install --immutable --mode=skip-build`); upstream
peer-dependency warnings remain. The Browser plugin and agent-browser CLI were unavailable;
used existing Playwright 1.62.1/headless Brave and the checked-in CDP workflow.

The historical mobile capture is **not** an animation timing problem: after 1.5 seconds the
transform was `none`, but a 390×844 device had `innerWidth=613`, `innerHeight=1327`, Sheet
`x=223`, width 390 and bottom 1327. The composition omitted `overflow-y-auto` from the
production `pages/Dashboard.tsx` SidebarInset and used Storybook's default padded layout.
Restoring that exact upstream overflow constraint and setting the composition to fullscreen
produced a Sheet at x=0, 390×844, with a 105px footer ending at y=844. Upstream source files
were not modified; only the task-owned composition was changed.

`components/ui/sheet.tsx` defines the mobile footer as `flex-col-reverse`, switching to a row
at `sm`. `CreateRunnerSheet.tsx` uses full-device-width/500px Sheet sizing and the standard
footer. The corrected rendering exposed the application's horizontal mobile buttons and
missing 1px Sheet border. Historical reference PNGs/hashes were preserved and their README
now warns against using the clipped mobile geometry as an acceptance baseline.

## Actual validation

- New disposable PostgreSQL/Control Plane/Worker stack `.tmp/cloud-agents-dev.PQ58N4`,
  container `cloud-agents-dev-501-38725`, ports 18085/18095 and Vite 4174.
- CLI-created project `project-58271e05cccaf055400dd128f1d64665`. Generated SDK registered
  three real persisted Targets `visual-docker`, `visual-kubernetes`, `visual-ssh`, using
  non-routable `.test` endpoints and opaque unconfigured references. No Probe was submitted.
- New footer assertion failed before the CSS fix on the short-mobile Release form:
  actual `row`, expected `column-reverse`.
- Final-source capture: 105 screenshots, all 24 Target list/detail/create matrix cells,
  ten footer checks (eight Target and two Release), both languages and themes, desktop/mobile
  and 390×360 short-mobile. Mobile primary action is above Cancel, both buttons fill the
  content width; desktop buttons remain inline; sticky actions remain visible.
- Additional live Playwright checks: Target/Release × en-US/zh-CN × 639/640px, eight passed.
  The native Cancel button closed each Sheet, footer ended at y=844, no document overflow
  and no JavaScript page errors. Profile creation was not exercised without its prerequisites.
- Existing capture retains four expected absent-quota 404s and eight unavailable-cleanup-
  preview 503s; no console warnings. Ten ordinary-user Admin reads returned 403. Browser
  contacted only the Vite origin; no lifecycle mutation requests. Not a zero-resource-error run.
- Independent database check: all three Targets stayed generation 1, `unprobed`, `active`.
- Admin 30 tests, final TypeScript/production build, focused lint and formatting passed.
  Existing 543.46 kB chunk warning remains.

## Scoped automatic pixel check

`compare-mobile-sheet-footer.mjs` reads PNGs with an existing decoder; it introduces no
product dependency. It compares the fixed 390×844 viewport's bottom 105px, masking only two
button-label interiors. It never shifts images, masks edges or accepts a mismatch tolerance.
It logs input hashes and fails if any of the 26,070 compared pixels differ.

The missing-border intermediate screenshot failed with 280 changed pixels. After aligning
both captures' `--disable-gpu --hide-scrollbars` flags, final en-US/zh-CN × light/dark checks
all passed with **zero changed pixels**. Using different GPU flags caused 98 antialiasing
differences; no application CSS or tolerance was changed to conceal those renderer differences.

```sh
node apps/admin-web/visual-baseline/daytona-v0.190.0/compare-mobile-sheet-footer.mjs \
  REFERENCE_PNG ACTUAL_PNG /path/to/existing/pngjs-or-playwright-utilsBundle.js
```

Raw attempts, final screenshots, breakpoint script/results and corrected reference PNGs:
`/tmp/cloud-agents-sheet-actions.aAySxi/`. `final/browser-evidence.json` SHA-256:
`f21b48eb6049a48b33073b28112003a211a2669c709e39a7862e6a07d06635cb`.
Reference light/dark hashes:
`7a16d72c9604a3ccf48322b09a3f18881e5771ec864855f49633099865253512`,
`2bd9cc5e11ddc49723257df13b46873558f66bd9ea5c6f473a7e2fdbe81086d3`.
Artifacts may expire. The reference composition is currently temporary; a durable full-page
reference generation entry point remains incomplete. The comparator alone cannot establish
reference provenance or whole-page conformance.

## Cleanup and earliest incomplete

The owned stack's shutdown trap removed its temporary database, project, Target records,
tokens and workspace. No backup was retained for this disposable test data. Its Vite and
reference Storybook processes were stopped. Recovery after interruption verified the state
directory/container absent and ports 18085/18095/4174/6006 unbound. Existing resources and
unrelated dirty/staged files were not touched; temporary reference source/screenshots remain.

ADMIN-M1 is still unqualified: durable corrected full-page reference generation, full visual
diff and current Docker/Kubernetes/SSH register-to-Probe-ready evidence remain. This slice
does not qualify managed Worker deployment, Provider Turns, M4 or overall Goal completion.
No real Provider credentials or destructive existing-resource approval was needed.
