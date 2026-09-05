# M1 Target empty-state registration/recovery — 2026-09-05

Base `d60c5cb0527cb2a846fbb963e6f2b9f57fa67d2c`, existing branch and unrelated
dirty/staged work preserved. Fixed Daytona v0.190.0 Empty source and committed
`reference/empty-state-light-desktop.png` were inspected before implementation.

## Delivered

The real Target list's zero-result branch now has a neutral dashed surface,
32px resource icon, localized heading/description and a working primary action.
A genuinely empty project opens the existing registration Sheet; filtered-out
results instead clear query/kind/phase filters. The existing paginator remains
mounted so an empty result does not discard the selected page size. No empty
pagination controls are displayed. Overview's compact empty previews and other
resource lists are unchanged. No contract/backend/SDK/dependency change.

Design-style applies the pinned Empty geometry; Ponytail reuses the navigation
icon, existing registration handler, primary button, catalog and filter state.
The shared clear-filter handler serves both toolbar and empty-state action.

## Actual live check

Fresh PostgreSQL 17.6 / migrations51 / Control Plane / Worker stack
`.tmp/cloud-agents-dev.4oqoHK`; Admin Vite `http://127.0.0.1:4174` proxies the
Admin API to `127.0.0.1:18085`. A new project
`project-a5984acdcc923dd084a60544bddbb428` initially had zero Targets and
Operations. The browser used the separate ephemeral Admin token.

- In-app browser verified meaningful connected content, correct origin/title,
  zero resource counts and no framework overlay. Browser plugin/agent-browser
  CLI unavailable; existing in-app CUA browser used, no dependency installed.
- Inspected en-US/zh-CN × light/dark × 1440×900/390×844 empty-state screenshots.
  Chinese desktop empty surface measured 244.75px tall, 48px padding,
  dashed border; icon measured 32×32. Mobile document width equaled viewport
  width390. English/Chinese descriptions wrap without font reduction.
- Empty-state Register action activated with Enter, opening the existing Sheet
  and focusing Target ID. Closing restored focus to the empty-state trigger.
- Reopened through the empty action and registered `empty-first-target`
  through the actual Admin API. The empty state became a one-row table;
  Operation/Audit persisted, generation1, phase unprobed. The endpoint is
  deliberately unconfigured loopback with an opaque unused reference; no
  Probe or deployment occurred.
- Set page size100, searched `absent-target`: distinct filtered-empty heading
  and Clear Filters action. Enter restored the real row, empty query and
  size100. Repeated recovery in Chinese at390px; no horizontal page overflow.
- Connected-session console warnings/errors: none. No mocked resource data,
  response interception or browser product-state injection.
- Admin build/TypeScript and28 tests/101 assertions passed, including catalog
  coverage and empty-page/filtered-snapshot checks; scoped format/lint/diff
  checks passed. Existing >500kB build warning remains.

## Reproduce and remaining boundary

Start the disposable dev stack and Admin Vite proxy, create a fresh project
with `cloud-agentsctl project create`, then connect at the numeric loopback
origin. Open Deployment Targets with zero registered resources. Follow the
keyboard registration and search/clear sequence above in both locales and
themes. Use a new unprobed Target and never operate existing infrastructure.

The empty component's structure, density and neutral styling were compared
with the pinned reference. Full-page automatic screenshot diff is **not**
claimed: the stored reference composition has a centered content width and
different header/toolbar arrangement; the current Target page uses its existing
full-width resource layout. This slice does not approve those broader layout
differences or complete M1's fixed visual gate. Lease/other empty states and
loading/error-state completeness remain separate work.

The registration flow also exposed an existing input-validation defect: a
space-containing Target name yields a generated-client validation error shown
as a response-contract error. A contract-valid name succeeded. The next slice
must expose that identifier constraint before submission; this empty-state
slice does not claim the invalid-input path passed.

No Worker deployment, Provider Turn, transport readiness or full Goal completion
is claimed. The disposable stack is temporarily retained for the immediately
following registration-validation slice; no existing resource was mutated.
