# Admin Web shared shell and scrolling — 2026-09-05

## Scope and result

Starting source: `04eef84b58009e910f564eb19209a7ecbd4d7ccc`, branch
`codex/cloud-agents-platform-p0`. This slice changes only the existing Admin Web shell and
its browser regression capture. No API contract, SDK, User Web, or backend model change was
needed. It does **not** close ADMIN-M1 or the Goal.

The fixed upstream tag was fetched again into a temporary sparse checkout and resolved to
`01c502bb1f1ff8f2885d0cd490e043736083dca8`. Source comparisons:

- `apps/dashboard/src/components/PageLayout.tsx`: Header padding is 15px vertically,
  with 32px controls and a border (63px actual height, not just the 55px minimum).
  PageContent is a flex child with min-height zero, 16px gap/padding, and its own scrolling.
- `components/ui/sidebar.tsx` and `components/Sidebar.tsx`: desktop/collapsed/mobile
  widths are 256/48/288px; navigation icons are 16px with 1.5 stroke and 8px label gap.
- The account dropdown in PageLayout is 256px wide.

The application now uses those dimensions, independently scrolls navigation and main content,
and uses inline SVG navigation icons rather than platform-dependent character glyphs.
No frontend dependency was added. The design-style and frontend-testing-debugging skills
kept the change on the pinned source and required live browser checks; this is not a new theme.

## Actual validation

- Admin Web TypeScript + Vite production build passed. Vite reports the existing single-chunk
  size warning (approximately 502 kB); this is not a clean-warning claim.
- Admin Web tests: 2 files, 20 tests passed. Formatting and scoped diff whitespace checks passed.
- Fresh PostgreSQL/local Control Plane/Worker stack from `scripts/cloud-agents-dev.sh`;
  Control Plane port 18085, Worker port 18095, Admin Vite proxy port 4174.
- A new test project and three Target records were created through real APIs. Docker,
  Kubernetes and SSH names are `visual-docker`, `visual-kubernetes`, `visual-ssh`.
  Their `.test` endpoints were deliberately not contacted and state remained `unprobed`.
  These records prove persisted resource rendering, **not** infrastructure connectivity.
- Checked-in `capture-actual.mjs` completed 21 screenshots, including both locales, both
  themes, desktop/mobile, Target list/detail/create, short desktop navigation and permission
  denial. All document-width checks passed. The script now fails on shell-dimension,
  icon-size, overflow, untranslated-key, bearer-after-reload and 403 regressions.
- Measured Header 63px; main content begins at y=63; Sidebar 256/48/288px; all ten nav
  icons 16×16/stroke 1.5; account dropdown 256px.
- At 1440×360, both nav and content scroll while the document does not; the last nav item
  was scrolled into view and clicked. An additional temporary Playwright check also passed
  390×360 mobile last-item navigation and reported zero JavaScript page errors.
- Browser plugin not available; reused the existing CDP capture and installed Playwright
  with headless Brave. No browser dependency or UI mock was installed.
- Locale changed immediately, Chinese preference survived reload, invalid locale fell back
  to English, and the bearer input was empty after reload. All ten Admin resource requests
  using the ordinary token returned 403.
- The Admin connection produced four expected HTTP 404 resource errors across two connects:
  the new project has no explicit lease quota or quota audit resource. These are retained in
  evidence, not hidden or described as a zero-error browser run. No console warnings.

Raw screenshots and full capture JSON for this run:
`/tmp/cloud-agents-daytona-baseline.p4J5TI/verified/`.
These temporary artifacts may expire; the checked-in capture script and the reproduction
procedure below are the durable check, not the continued existence of a temp directory.

## Reproduction

1. Start `scripts/cloud-agents-dev.sh` with
   `CLOUD_AGENTS_DEV_CONTROL_PLANE_LISTEN=127.0.0.1:18085` and
   `CLOUD_AGENTS_DEV_WORKER_LISTEN=127.0.0.1:18095`. Use only its newly created database and
   token-file paths. Start Admin Web with
   `CLOUD_AGENTS_CONTROL_PLANE_URL=http://127.0.0.1:18085 bun run --cwd apps/admin-web dev`.
2. Use the printed CLI with the ordinary token to create a fresh project:
   `--tenant tenant-local --request-id visual-shell-project --idempotency-key visual-shell-project project create --name visual-shell --organization-id organization-local --display-name "Visual shell validation"`.
   Retain the returned project UID.
3. Connect Admin Web using the admin token file and that UID. Register exactly the three
   named Targets above using their matching kinds, opaque credential references and
   non-routable test endpoints. Do not probe these visual-only records.
4. Run:

   ```sh
   node apps/admin-web/visual-baseline/daytona-v0.190.0/capture-actual.mjs \
     /path/to/new-output /path/to/admin-token /path/to/user-token PROJECT_UID
   ```

5. Stop only the newly created local stack through its trap/terminal and its own Vite process.
   No pre-existing Target, container, Pod, volume or credential is in this check's scope.

## Remaining mismatch and acceptance boundary

The historical reference screenshots are unbranded Storybook compositions, including an
extra outer inset. This slice uses the actual pinned PageLayout source for shell geometry;
it does not silently treat composition padding as a production rule or regenerate reference
images to match our implementation.

ADMIN-M1 remains earliest unqualified: grouped navigation/command palette, remaining component
and state parity, and a complete current fixed-reference visual comparison are still required.
Native mobile focus/Escape behavior also needs dedicated verification. No approval is implied
for any unexplained visual discrepancy. Earlier historical M1 reports are not sufficient
evidence for the expanded current application.

M3 runtime policy enforcement and M4 Docker/Kubernetes/SSH real deployment, both Provider
turns, lifecycle operations and zero-residue evidence remain separate work. This run supplies
none of that evidence, no Provider credentials, and no destructive infrastructure action.
