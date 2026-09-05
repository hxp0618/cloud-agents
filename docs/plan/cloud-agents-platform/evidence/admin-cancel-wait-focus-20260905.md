# M1 shared cancel-wait focus — 2026-09-05

Base `12319d2`, same `codex/cloud-agents-platform-p0` branch. This follows the
real refresh-state test, which exposed focus falling to body after the focused
Cancel wait button was removed.

## Fix and scope

The shared feedback's cancel handler remembers its own rendered feedback
surface. After busy clears, a layout effect focuses that surface's cancellation
alert with tabIndex=-1. It does not focus an inert background feedback copy,
does not add the alert to sequential Tab order, and does not steal focus if the
user has already focused another connected control. Removed surfaces are
ignored. All App runOperation callers and all Sheet feedback render sites were
inspected; no per-action cancellation or request semantics changed.

Ponytail/React guidance kept this in the existing shared feedback rather than
adding per-page handlers, timers, focus managers or a dependency. Existing native
focus outline and localized cancelled-operation text are reused.

## Actual checks

- Admin30 tests, TypeScript/Vite build, scoped oxfmt/oxlint and diff checks
  passed. Existing bundle-size warning remains.
- Used the preceding slice's real disposable project, Target and dev stack.
  Reconnected with its rotated ephemeral Admin token after development reload.
  Paused/resumed only owned Control Plane PID65616, never a shared instance.
- English desktop: keyboard Enter on Refresh, then Cancel wait. After the
  actual request abort, activeElement was the cancellation alert, skeletons0;
  next Tab focused the Target search input.
- Opened actual persisted Target detail. Paused CP, requested read-only
  Preview drain, then pressed Enter on the Sheet's Cancel wait. activeElement
  was the alert inside dialog `Deployment target refresh-target`, not the
  background page's identical alert. No Drain mutation or transport Probe ran.
- Chinese dark390x844: keyboard refresh/cancel focused the translated result,
  visible native focus ring, document scrollWidth390. English light desktop
  Sheet result was also visually inspected. This is scoped focus QA, not all
  operations or all eight complete-page visual gates.
- Browser warning/error query empty. Runtime races involving deliberately
  moving focus between abort and settlement were not separately induced.

Runnable browser check, after pressing Cancel wait against a genuinely pending
request (repeat once from the page and once from a Sheet):

```js
const result = document.activeElement;
if (!result?.matches('.operation-feedback [role="alert"]')) {
  throw new Error('Cancel wait lost result focus');
}
const openSheets = [...document.querySelectorAll('dialog[open]')];
if (openSheets.length && !openSheets.at(-1).contains(result)) {
  throw new Error('Cancellation focus escaped the active Sheet');
}
if (document.querySelector('.resource-refresh[aria-busy="true"]')) {
  throw new Error('Cancelled refresh is still pending');
}
```

For reproduction, follow the disposable-stack setup and exact-PID pause/resume
procedure in `admin-resource-refresh-20260905.md`; always resume CP even if an
assertion fails. Do not use mutation execution merely to test focus.

## Cleanup and remaining boundary

Closed owned tab6, reset viewport, stopped Vite65665 and dev parent65473.
Shutdown removed `cloud-agents-dev-501-65473` and `.tmp/cloud-agents-dev.4Z0cTM`;
ports4174/18085/18095 have no listeners. Its disposable database/one Target and
tokens were not backed up. No existing resource was deleted.

Additional plan/ADR dirty files appeared during this slice and were left
untouched. Staged HTML blob remained
`2adb0cc1c5649e39534a2171a5c25aabedf1fe30`.

M1 remains earliest: remaining resource states and complete pinned visual and
interaction acceptance. No claim of M2-M4 or Docker/Kubernetes/SSH deployments,
Codex/Claude Turns, OIDC deployment isolation or overall Goal completion.
No new credentials, external environment or destructive approval is required
for the next local M1 slice.
