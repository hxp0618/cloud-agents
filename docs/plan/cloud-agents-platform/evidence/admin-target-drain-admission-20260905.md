# Target Drain new-work admission — 2026-09-05

Base `83693aebe4897c728291d387b83c1834a83b12d6`, branch
`codex/cloud-agents-platform-p0`. Bounded Target scheduling slice, not milestone closure.

## Delivered

- Migration051 puts the existing Target scheduling state behind a shared database admission
  barrier for Session/Turn insertion and Execution insertion/start. Target Drain takes the
  existing exclusive Target lock. Work committed before Drain remains admitted; admissions
  waiting behind a committed Drain fail closed. Lease generation/readiness/expiry are checked
  again after acquiring the barrier. Target/Lease share locks avoid an exclusive Lease lock
  inversion with existing Session creation and Target scheduling.
- New Session/Turn/Execution is rejected while drained. Existing idempotent replays remain
  valid, and terminal execution updates are not blocked. Resume reopens admission. This does
  not interrupt or terminate running work or remove resources. Legacy sessions without a
  target-backed Lease remain outside Target scheduling authority.
- Existing Admin preview/transition APIs, expected generation/resource version, impact digest,
  idempotency key, Maintenance Operation and Audit are reused. Public request/response shapes
  are unchanged; contract semantic constraints now describe task admission and generated SDK
  checks remain current. User APIs reuse stable conflict responses without adding infra fields.
- Existing English/Chinese Target description, confirmation and operation-impact messages now
  state the actual admission boundary. No layout, framework, dependency or new UI action added.
  Design-style/React/Ponytail guided reuse of the existing Daytona confirmation form.
- Product migration head, strict three-trigger SQL allowlist, schema catalog/bundle/manifest,
  readiness, independent runner binding and migration image packaging advance to051. Historical
  bindings are retained. The migration is forward-only; no existing deployment was migrated.

## Actual verification

- Fresh owned PostgreSQL17.6 container `cloud-agents-dev-501-10059` applied the real product
  migrator: `schema_head=000051`, `applied=51`, `no_op=false`. Local CP readiness succeeded on18085.
  Project `project-e6b6bd898f82fbb95dbc0e7ebbc0c172` was created through the real CLI/API.
- `TestTargetDrainAdmissionPostgres` passed twice consecutively under `-race`. It calls real
  Admin/User HTTP APIs with generated Go clients and the restricted PostgreSQL runtime role.
  It registers an unprobed Target, explicitly inserts one ready Lease SQL fixture, and creates
  real persisted Session/Turn rows. The fixture endpoint and provider reference are intentionally
  unconfigured. **No target deployment, Worker or Provider turn was exercised.**
- Assertions: ordinary-user preview and mutation403; stale scheduling version409; new Session
  and Turn409; rejected Execution create/start; accepted idempotent Session/Turn/Execution and
  Drain replays; terminal execution SQL update; Resume restores admission; both concurrent lock
  orderings; exactly five successful API scheduling audit records with no replay duplicate.
  Admin operation output excludes the known test prompt and provider reference. The terminal
  SQL update tests the guard, not Provider completion. One concurrency case sets Target state
  in an owner SQL transaction solely to control lock timing; it is not an audited API operation.
  Initial expected audit count was incorrectly six; corrected to the five actual API transitions.
- Fixture test refuses a project with any existing Target/Lease/Session and deletes its scoped
  fixture rows on exit with a bounded cleanup context. No pre-existing project resources were used.
- Go race tests passed for server, deploymenttarget, localmigration and product migrator; Go vet
  passed for those packages. Migration classifier/release packaging53 tests and Admin27 tests
  passed. SDK regeneration checks and pinned Go1.26.6 module boundaries passed. Contract standards
  passed142 schemas,89 OpenAPI operations,1299 official-suite assertions and14 Python tests.
  Scoped lint/diff checks passed. Existing unrelated classifier indentation was preserved after
  the formatter check. Admin typecheck and Admin/User production builds passed; Admin retains the
  existing single-chunk warning at530.27kB minified.
- Browser CLI/plugin unavailable; already-installed Playwright/Brave used without new dependencies
  or response mocks. Separate owned `drain-browser` Target registered via real Admin API with no
  probe. Eight zh-CN/en-US × light/dark ×1440×900/390×844 combinations executed16 successful real
  Drain/Resume mutations. Each confirmation starts disabled, requires impact checkbox, displays
  localized admission text and closes after successful API response. Target detail refreshes;
  all mutations are same-origin CP Admin API calls. No page errors or stored bearer token.
- Sixteen screenshots and sanitized JSON are retained at
  `/tmp/cloud-agents-target-admission.co7lTH/browser`; the temporary runnable capture is
  `/tmp/cloud-agents-target-admission.co7lTH/capture.mjs`. Inspected English desktop and Chinese
  dark mobile Drain screenshots. Document, native modal and form overflow checks passed. An
  initial inner-section assertion incorrectly included the close button's intended8px offset;
  corrected to measure native modal and form. CSS was unchanged. These checks do not constitute
  complete fixed Daytona screenshot-diff acceptance or User Web browser security qualification.

## Reproduce the admission check

Use pinned Go1.26.6/Bun1.3.14/Node24.18.1. Start `scripts/cloud-agents-dev.sh` with CP18085 and
Worker18095 in a fresh disposable database; create an empty project in tenant-local using its
CLI. Supply its restricted runtime/migration URLs privately; do not use production databases.

```sh
export CLOUD_AGENTS_DRAIN_TEST_RUNTIME_DATABASE_URL='postgres://...'
export CLOUD_AGENTS_DRAIN_TEST_MIGRATION_DATABASE_URL='postgres://...'
export CLOUD_AGENTS_DRAIN_TEST_PROJECT_ID='project-ID-from-create'
export CLOUD_AGENTS_DRAIN_TEST_DEV_STATE='/absolute/path/to/fresh/dev-state'
go -C services/control-plane test -race ./internal/server \
  -run '^TestTargetDrainAdmissionPostgres$' -count=2 -v
```

For UI reproduction, run Admin Vite4174 against CP18085, connect with the temporary Admin token,
register a fresh unprobed disposable Target, open detail, preview Drain, review impact/check the
checkbox, confirm and then preview/confirm Resume. Repeat both locales/themes/viewports. Do not
point this UI test at an existing Target: Drain changes admission for every Lease on that Target.

## Remaining acceptance

Cleanup verified: own Vite and dev parent stopped; container `cloud-agents-dev-501-10059`,
temporary state `.tmp/cloud-agents-dev.msumoR`, and listeners4174/18085/18095 are absent.
The disposable database and temporary tokens were deleted, not retained for recovery. Sanitized
captures remain. No pre-existing Worker, container, Pod, volume or credential was changed.

Target-wide admission is **not per-Worker independent Drain**, Worker-side acknowledgement,
resource usage measurement, Upgrade/Rollback qualification or real Provider execution evidence.
ADMIN-M1 still lacks complete Daytona fixed baseline acceptance and current three-target
registration/Probe qualification. ADMIN-M3 Worker lifecycle and ADMIN-M4 independent OIDC,
Docker/Kubernetes/SSH deployment/zero-residue cleanup and Codex/Claude Code flows remain open.
No credential or pre-existing-resource cleanup authorization was required for this isolated slice.
Goal remains active.
