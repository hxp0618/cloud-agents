# M1 Target list pagination — 2026-09-05

## Scope and baseline

Base HEAD `beb8e28e760dba54c285b7de0cece5596ea6eb55`, branch
`codex/cloud-agents-platform-p0`. Requirement §12.2 resource-list pagination.
Pinned Daytona v0.190.0 `01c502bb1f1ff8f2885d0cd490e043736083dca8`
dashboard Pagination/CursorPagination and page-size constants were inspected.
Native React/CSS implementation: 25 default; 10/25/50/100/200 choices;
140px select, 32px buttons, 16px groups and 8px button gap; first/last hidden
below 1024px, stacked footer below 640px. No dependency or API change.

The existing generated Admin SDK loader follows **all** API cursors before
filtering. This change pages that complete filtered snapshot; it does not
reduce network/memory cost, provide server-side search, or paginate other
resource lists. Size/filter changes reset to the first page. A shrinking
snapshot clamps the stored index. Overview's six-row preview is unchanged.

## Real execution

Disposable local stack `.tmp/cloud-agents-dev.UcI2yy`, PostgreSQL 17.6,
migrations through 51; CP `127.0.0.1:18085`, Worker `127.0.0.1:18095`,
Vite `127.0.0.1:4174` with `/v1/admin` proxy. Project
`project-df2ab543f3d41548ce0fe4929cd8c569`, tenant `tenant-local`.

205 distinct Target IDs `page-000` through `page-204` were registered through
the generated Admin SDK and persisted by Control Plane. Kinds cycle through
Docker/Kubernetes/SSH. These are deliberately **unprobed test registrations**
with loopback endpoints and an unconfigured opaque credential reference;
no fake API/response interception or frontend data injection was used.
Generated SDK list requests returned 200 then 5 records, no final cursor.
The ordinary local User token received HTTP 403 `AUTHORIZATION_DENIED` on
Admin registration; successful requests used the separate local Admin token.

In-app browser checks against this live stack:

- Default page: 25 rows, first `page-000`, page 1/9, total 205.
- Next: 25 rows starting `page-025`, page 2/9. Opened its real detail and
  verified one successful registration Operation and Audit event; closing
  retained the page.
- Last: five rows `page-200`–`page-204`, page 9/9, next/last disabled.
- Size 200: 200 rows on page 1/2; next gives five rows; real Refresh retained
  page 2/2 and five rows, with successful authority-refresh status.
- Search `page-204`: one row/page 1/1. Nonmatching query: zero rows/page 1/1,
  all navigation disabled. Clear Filters restores all 205; size selection
  remains available. Chinese first-page button returns 25 rows/page 1/9.
- Mobile previous/next traversed page 9 → 8 (25 rows) → 9 (five rows).
- en-US/zh-CN × light/dark × 1440×900/390×844 screenshots inspected inline.
  Localized pager text updates immediately, no page-level horizontal overflow
  at 390px; native table horizontal scrolling remains intentional. Chinese
  DOM measured 140px select and 32×32 visible controls; hidden first/last.
  Additional Chinese 768px and 1024px widths had no document overflow.
- Connected 127.0.0.1 browser session: no console warning/error entries.
  Initial connection attempts using cross-origin CP / HTTP localhost failed;
  the verified configuration is the same-origin numeric loopback proxy.

## Reproduce

Start `bash scripts/cloud-agents-dev.sh` with the CP/Worker listen variables
set to the ports above. Start Admin Vite with
`CLOUD_AGENTS_CONTROL_PLANE_URL=http://127.0.0.1:18085`. Create a fresh project
using the stack's `cloud-agentsctl`, ordinary token and `project create`.
Use its returned ID and the stack's separate `control-plane-admin.token`.
From a temporary Bun script importing `createHTTPClient` from
`sdk/typescript/src/platform.ts`, sequentially execute:

```ts
for (let i = 0; i < 205; i++) {
  const id = `page-${String(i).padStart(3, "0")}`;
  const kind = (["docker", "kubernetes", "ssh"] as const)[i % 3];
  await client.registerAdminDeploymentTarget(
    "tenant-local", projectId, crypto.randomUUID(), crypto.randomUUID(),
    { targetId: id, targetName: id, targetKind: kind,
      endpoint: `${kind === "ssh" ? "ssh" : "https"}://127.0.0.1:${1000 + i}`,
      credentialRef: "pagination-unconfigured" },
    AbortSignal.timeout(30000),
  );
}
```

Do not run against an existing/shared project. Connect the browser at
`http://127.0.0.1:4174` using that origin as endpoint, tenant/project IDs and
the temporary Admin token. Follow the checks above; never Probe these entries.

## Automated checks and evidence boundary

- `bun test apps/admin-web/src`: 28 passed, 101 assertions, including all
  pages without loss/duplication, empty/shrinking snapshots and invalid inputs.
- Admin `build` (TypeScript + Vite): passed; existing >500kB chunk warning remains.
- Scoped `oxfmt --check`, `oxlint --deny-warnings`, `git diff --check`: passed.

Fixture setup exposed a **separate open M1 defect**: batches of five concurrent
registrations produced 409 and 500; PostgreSQL logs explicitly reported
serialization failures both in `register_deployment_target_v3` and at commit.
Sequential registration completed. This slice does not fix or qualify
concurrent registration; that is the next backend slice.

No transport Probe, Lease/Worker deployment, Provider Turn, or full Daytona
visual gate is claimed. Goal remains active; this is not M1–M4 completion.
