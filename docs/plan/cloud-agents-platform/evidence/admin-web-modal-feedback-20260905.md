# Modal-local operation feedback — 2026-09-05

Base source `67c1fb6b8c29d9fa32111e869506e6d40663b3e9`, unchanged branch
`codex/cloud-agents-platform-p0`. Previous turn was progress. This is another ADMIN-M1
slice, not completion of M1 or the Goal.

## Root cause and implementation

All operations share `runOperation`, but pending/error feedback lived only in PageContent.
An open native dialog makes that content inert. Cleanup failures and the Cancel wait action
were therefore inaccessible until the user closed the resource Sheet.

- Reused that feedback in every existing `AdminSheet` caller through a required prop. Native
  CSS displays it only on the top resource modal, hiding background and parent-modal copies.
  No event bus, observer, dependency, state-management layer or API change was introduced.
- The current Sheet/Dialog has a bounded feedback area outside the scrolling resource body.
  Errors remain visible at short viewport heights, while form state and operations remain intact.
  Leaving a resource page clears stale errors; successful previews also clear prior failures.
- Error feedback includes only a localized explanation and a stable error code validated by
  the existing generated `parseProblem` parser, with HTTP status agreement checked. Raw titles,
  exception messages, invalid responses and additional secret fields never become UI diagnostics.
- Pending feedback exposes the existing AbortController-based Cancel wait action inside the modal.
  Both locales now explicitly explain that stopping the wait does not prove server-side cancellation.
- Revalidated Daytona v0.190.0 commit `01c502bb1f1ff8f2885d0cd490e043736083dca8` and inspected
  `ui/alert.tsx`: 16px icon, 12px icon gap, 16px horizontal/12px vertical padding, 14px body,
  8px radius and existing destructive tokens. This does not claim complete screenshot parity.
  The design-style and frontend debugging skills kept the change on the pinned tokens and
  required actual rendered failure/recovery checks.

## Actual evidence

- Fresh PostgreSQL/local Control Plane/Worker on 18085/18095, Admin Vite proxy on
  `http://127.0.0.1:4174`; no Browser plugin available, so installed Playwright 1.62.1 and
  headless Brave plus the existing CDP capture were used without installing dependencies.
- New project `modal-feedback` (`project-5dea5b8133573b46a128a5b7dc6bc6f0`), with three real
  API-persisted Target records. `.test` endpoints were not contacted or probed.
- Checked-in `capture-actual.mjs`: 57 screenshots, eight locale/theme/viewport combinations,
  real Cleanup 503 displayed inside the modal, exactly one visible feedback area, stable code,
  translated explanation, successful scheduling-preview recovery, previous shell/command/modal
  checks and no document overflow. All browser Admin calls in this capture were GET;
  ten ordinary-token Admin calls returned 403.
- Expected HTTP failures were preserved: eight Cleanup 503s (`ENVIRONMENT_CLEANUP_UNAVAILABLE`)
  because this local stack has no cleanup actuator, and four unset quota/audit 404s across two
  connections. A 503 is failure-path evidence, never Cleanup-success evidence.
- Separate Playwright run: both locales × both themes × desktop/mobile, repeated real Cleanup
  failures/retries, error visibility before closing the modal, and visibility on PageContent
  after closing it. At 390×360 the feedback remained onscreen.
- Real duplicate Target registration returned 409. The modal retained all input values and
  displayed the error in its own feedback area. No existing Target was overwritten.
- A PostgreSQL transaction temporarily acquired an exclusive lock on this disposable stack's
  `cloud_agents.deployment_targets` table. Opening a Target detail waited on a real backend read;
  Cancel wait inside the Sheet produced the localized cancellation boundary message. The lock
  transaction was rolled back; final `pg_stat_activity` reported zero idle-in-transaction sessions.
  No response was mocked, intercepted, delayed by a fake server, or substituted.
- Final real Target list returned 200: exactly the original three names, all generation 1,
  resource version 1, `unprobed`. No JavaScript page errors, blank page or framework overlay.
- Admin build/typecheck, 23 tests, scoped formatting/lint and diff checks passed. Tests include
  valid stable-code extraction and rejection of invalid/mismatched/secret-bearing diagnostics.
  Existing ~508kB single-chunk build warning remains.

## Reproduction and limits

Follow the fresh-stack/project/three-Target procedure in
[shell evidence](admin-web-shell-scroll-20260905.md), then run
`capture-actual.mjs OUTPUT ADMIN_TOKEN_FILE USER_TOKEN_FILE PROJECT_UID` against the standard
dev stack (without a cleanup actuator). This durable check now asserts real 503 feedback and
recovery as well as earlier interactions. Raw screenshots, Playwright script and detailed JSON
for this run are in `/tmp/cloud-agents-modal-feedback.YZQLUy/`; temporary artifacts may expire.

To reproduce the additional cancellation check, use only a newly created disposable dev
database: keep `BEGIN; SET LOCAL idle_in_transaction_session_timeout = '30s'; LOCK TABLE
cloud_agents.deployment_targets IN ACCESS EXCLUSIVE MODE;` open in psql, open an existing
Target detail in the browser, cancel its wait, then immediately `ROLLBACK`. Never apply this
lock to a shared or production database. The duplicate-registration check reuses a test Target
ID with a different display name and verifies 409 plus unchanged authority.

ADMIN-M1 still requires the remaining Header/component/state fidelity and complete current
pinned screenshot comparison, plus real three-target Probe-ready acceptance. These checks do
not prove successful cleanup, real deployment, Provider Turns, full contrast/assistive-technology
coverage, M3 policy enforcement, or M4 Docker/Kubernetes/SSH lifecycle and zero-residue acceptance.
No additional credentials/permissions are needed for the next UI slice. No push, image publication
or Release was performed.
