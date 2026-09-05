# Recent failed maintenance operations — 2026-09-05

Base `e80617618059bd9aa2856274507e2b5f214d041c`, unchanged branch
`codex/cloud-agents-platform-p0`. Previous turn made progress. This continues ADMIN-M1;
it does not complete the milestone or Goal.

## Delivered and authority

Overview shows the latest six failed Maintenance Operations before Target/Lease tables.
Ordering uses parsed `updatedAt` instants descending, with operation ID as a deterministic
tie-breaker. The original loaded snapshot is not mutated. Opening a row uses the existing
Operation detail Sheet; “View all failed” opens Maintenance with its failed-only filter set.
The filter intersects with search, can be toggled off without losing the query, and resets on
ordinary navigation. Stable error codes are visible in the table and searchable. Empty failure
snapshots and filtered no-match states have distinct messages in both locales.

The current generated SDK loader drains all real Admin maintenance pages. Server route scope
checks, verified project authorization and PostgreSQL latest-operation persistence already
exist, so no backend, contract or SDK change was needed. The current contract's resourceKind
is **DeploymentTarget**; it is not a complete Lease operation or Audit catalog. The panel
description explicitly states this source boundary. No additional User Web/storage/dependency
change. Existing search does not expand into actor diagnostics, idempotency keys or arbitrary
impact descriptions.

## Actual verification

- Fresh local PostgreSQL/Control Plane/Worker dev stack at 18085/18095, Admin Vite at
  `http://127.0.0.1:4174`. No existing deployment or credential was used.
- Fresh failure project `project-e3b42aa4a15a1f273cd0284369e3e5c7`: three API-registered
  `visual-docker`, `visual-kubernetes`, `visual-ssh` Targets, then eight distinct Admin Probe
  requests. Actual prober-unconfigured branches completed and persisted failed Operations:
  three Docker, three Kubernetes, two SSH. All three Targets became unavailable; registration
  Operations remained succeeded (11 total). No `.test` endpoint was contacted.
- Read-only `capture-failed-operations.mjs` compares rendered row IDs/order against the actual
  Admin API snapshot. Eight locale/theme/viewport cases (en-US/zh-CN × light/dark ×
  1440×900/390×844), 24 screenshots. Checked latest-six truncation, 8-failure list, 11-total
  list after clearing, code search, no-match text, query preservation, Enter/Sheet/Escape/focus
  restoration, title/origin/nonblank/no-overlay, no document overflow, no page errors, no
  mutations and no persisted bearer. Browser requests stayed on the Vite origin.
- Ordinary Token maintenance request returned 403. Two initial unset-Quota404 responses are
  expected; no other HTTP failure occurred in this browser run. No response mocks, interception,
  injected product state or direct SQL fixture writes were used.
- Separate fresh unprobed project `project-348b552bcaa90978624db5c512f527ee` retained the prior
  full capture: 97 screenshots, eight Overview and eight Target-filter checks, zero document
  overflows/mutations, no persisted bearer and ten ordinary-token403s. Four unset-Quota404s
  and eight unconfigured Cleanup-preview503s remain expected negative paths.
- Admin build/typecheck and 26 tests in three files passed. New test checks exact failed-state
  filtering, stable-code search, timezone-aware ordering, tie ordering and input immutability.
  Scoped formatting/lint/diff checks passed. Existing 520.23 kB bundle warning remains.
- Stopped only this run's Vite and dev stack. Confirmed its Worker/Control Plane processes
  exited, `cloud-agents-dev-501-37958` was removed and `.tmp/cloud-agents-dev.fZOI2z` was deleted
  by the dev-script trap. Disposable database records and credentials have no retained backup.
  No existing deployment, Kubernetes resource or SSH service was removed.

Browser plugin not available; used installed Playwright/Brave for the new check and existing
Brave/CDP capture for the broad regression. Pinned Daytona checkout remains
`01c502bb1f1ff8f2885d0cd490e043736083dca8` (`v0.190.0`). Reviewed `ui/table.tsx` and the checked-in
design system. Reused existing flat table/panel/Sheet, neutral pressed-state and focus styles;
the table scroll region is keyboard-focusable. Desktop failure table and Chinese mobile detail
screenshots were visually inspected. This is not full fixed-reference screenshot-diff approval.
The design/React/Ponytail skills kept the slice on current components, derived data and native
controls; frontend verification required nonempty real failure records.

## Reproduction

1. Start a fresh stack/project using [shell evidence](admin-web-shell-scroll-20260905.md).
   Register three Targets through `registerAdminDeploymentTarget`, matching kind and the
   `visual-*` IDs above, opaque matching credential references, HTTPS Docker/Kubernetes and
   SSH `.example.test` endpoints. Do not configure any prober for this negative-path check.
2. With the generated SDK admin client, call
   `probeAdminDeploymentTarget("tenant-local", projectId, "visual-" + kind, requestId, idempotencyKey, { expectedGeneration: 1 })`
   eight times, cycling Docker/Kubernetes/SSH. Use distinct `failed-overview-probe-0` through
   `failed-overview-probe-7` request/idempotency keys. Assert the returned phases are unavailable
   and stable codes are respectively `docker-probe-unconfigured`, `kubernetes-probe-unconfigured`,
   `ssh-probe-unconfigured`. These are expected persisted failures, not successful probes.
3. Point `PLAYWRIGHT_MODULE` to an already installed Playwright ESM module if it is not locally
   resolvable; optionally set `BROWSER_PATH`. Run from the repository root:

   ```sh
   node apps/admin-web/visual-baseline/daytona-v0.190.0/capture-failed-operations.mjs \
     OUTPUT ADMIN_TOKEN_FILE USER_TOKEN_FILE PROJECT_UID
   ```

The script is read-only and requires at least seven real failures plus one success. Outputs
for this run are `/tmp/cloud-agents-failed-overview-20260905/browser-evidence.json` and `shell/`;
temporary artifacts may expire. The committed script and procedure are the durable evidence.

## Remaining

Earliest ADMIN-M1 gaps: Overview Worker/Lease state breakdown, upgrade counts and recent admin
Audit activity; remaining Target columns/filters and complete fixed visual comparison. Current
Maintenance contract coverage must be extended before claiming non-Target operation coverage.
M2/M3/M4 real Profile/policy, Worker lifecycle, Codex/Claude and three-target zero-residue proof
remain separate. No credentials or destructive authorization is needed for the next slice.
No push, image publication or Release.
