# Target list filters and probe facts — 2026-09-05

Base source `cc0e4e8ba9080c14f5a81f91a73b0e21a5c9e229`, branch
`codex/cloud-agents-platform-p0`. Previous turn made progress with three real transport Probes.
This slice advances ADMIN-M1's actual resource list; it does not complete M1 or the Goal.

## Delivered behavior

- Target kind and observed-phase filters combine with case-insensitive, trimmed search.
  Search covers identity and existing probe facts, never endpoint or credential reference.
  Clear restores all records; no matches is distinguished from an empty project.
- Engine/API version and OS/architecture now render from the generated DeploymentTarget
  contract in both the full list and Overview table. Missing facts explicitly show
  localized unavailable text, not invented versions or inferred readiness.
- The existing list loader drains server pagination before filtering. Filtering derives from
  this loaded authority, performs no additional requests, and resets on route navigation.
  This is client-side filtering of the existing complete snapshot, not new server filtering
  or table pagination. No new backend, contract, SDK or User Web code was necessary.
- The wider table scrolls within its resource panel. Its named focusable region supports
  keyboard horizontal scrolling. Filters retain native controls and the existing CSS tokens;
  no table framework, state-management library or new dependency was introduced.

## Reference and mismatch boundary

Revalidated the pinned Daytona v0.190.0 checkout at
`01c502bb1f1ff8f2885d0cd490e043736083dca8`; inspected `RunnerTable.tsx`,
`SandboxTable/filters/StateFilter.tsx` and `ui/select.tsx`. Reused the established 32px
compact controls, 8px toolbar gap, 384px search maximum and bordered scrolling table.
Historical reference compositions have an extra outer inset; they were not regenerated.

This slice closes functional filtering and missing probe-fact columns. Native single-select
menus are **not** a claim of parity with Daytona's multi-select popover filters. Full
filter-menu/selection/pagination visual and interaction parity remains unqualified, as do
location/labels (no current Target authority) and the active-Lease list column. These gaps
must not be silently waived by a screenshot count. Design-style kept the fixed tokens;
React/Ponytail guidance kept filtering derived and reused the existing SDK pagination.

## Actual QA

Fresh `scripts/cloud-agents-dev.sh` PostgreSQL/Control Plane/Worker on 18085/18095,
Admin Vite proxy at `http://127.0.0.1:4174`. New project
`project-d93ecfbafff3e29aa43be7a80315195f` holds three real API-registered records:
`visual-docker`, `visual-kubernetes`, `visual-ssh`. They remain unprobed; their `.test`
endpoints were never contacted. This run does not repeat the prior transport-ready checks.

Browser plugin not available; existing Playwright 1.62.1/headless Brave and checked-in CDP
capture were used. No browser dependency was installed.

| Check                         | Actual evidence                                                                                           |
| ----------------------------- | --------------------------------------------------------------------------------------------------------- |
| Identity / nonblank / overlay | Correct origin/title, real three-record table, no framework overlay                                       |
| Locale/theme/responsive       | zh-CN/en-US × light/dark × 1440×900/390×844; no document overflow                                         |
| Target interaction            | Kind + phase + search narrows 3→1; ready filter yields 0; clear restores 3                                |
| Missing facts                 | Both locales explicitly render unavailable facts from real unprobed records                               |
| Keyboard                      | Native typeahead `d` + Enter selects Docker; focused mobile table scrolls with ArrowRight                 |
| Requests                      | Separate filtering run: no Admin request or mutation after initial load                                   |
| Regression                    | 73 screenshots, eight filter scenarios, shell/locale/modal regressions and ten ordinary-user403 responses |
| Build / tests                 | Admin build/typecheck, 24 tests in three files, scoped lint/format and diff checks                        |

The existing ~510kB chunk warning remains. Full capture records four expected unset-Quota
404s and eight real Cleanup-preview 503s from the unconfigured cleanup actuator; these are
failure-path evidence, not successful Cleanup. The separate filter run has no JavaScript
page errors and only the two initial unset-Quota 404s. Initial keyboard harness attempts
used ineffective native-select keys; typeahead was verified instead. The CDP helper also
needed the actual ArrowRight virtual-key code; the application scroll passed Playwright.

Reproduce with the fresh-stack/project/three-Target procedure in
[shell evidence](admin-web-shell-scroll-20260905.md), then run the checked-in
`capture-actual.mjs OUTPUT ADMIN_TOKEN_FILE USER_TOKEN_FILE PROJECT_UID`.
Temporary Playwright checks and screenshots are under
`/tmp/cloud-agents-target-filters.W1dchh/`; they may expire. Generated artifacts contain no bearer.
The final full capture is in `final-verified-2/`; its eight filter checks passed after the
CDP virtual-key correction, including real mobile keyboard scrolling. The capture records
only the Admin Web origin and no persisted bearer or mutation request.

The extra unit check covers combined/exclusive filters, case/whitespace, stable identity,
all four probe facts, and exclusion of endpoint/credential fields. Non-empty probe-fact
rendering is source-covered here, **not** newly browser-qualified against a ready Target.
No Mock or substituted response was used for browser acceptance.

## Remaining acceptance

Stopped this run's Vite, Control Plane and Worker, and let the dev script remove its own
PostgreSQL container `cloud-agents-dev-501-13634` and state directory. Final PID/container/path
checks found them absent. No pre-existing resource was changed or cleaned; no target
workload, temporary workspace volume, Kubernetes resource or SSH service was created here.

ADMIN-M1 remains earliest: complete pinned visual/interaction parity, resource-list gaps
above, and Overview's required metrics/filter navigation still need closure. M2/M3 profile
and policy runtime qualification and M4 real Docker/Kubernetes/SSH Worker lifecycles, both
Providers and zero-residue proof remain separate. No additional credentials or destructive
permission is needed for the next in-scope UI slice. No push, image publication or Release.
