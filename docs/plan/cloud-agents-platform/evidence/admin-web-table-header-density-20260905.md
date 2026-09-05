# Admin table header density — 2026-09-05

Starting HEAD: `f8d5bfd0affb256d0c4e621bd5c1c78ed96eee7d`, branch `codex/cloud-agents-platform-p0`.

## Change and reference

Pinned Daytona `01c502bb1f1ff8f2885d0cd490e043736083dca8` `apps/dashboard/src/components/ui/table.tsx` specifies a 32px TableHead, 12px/16px type and no wrapping. Cloud Agents' shared `th` used 34px, inherited line height and allowed wrapping. All resource tables use this shared rule; the fix changes it once without per-page exceptions or dependencies. Body rows and business fields are unchanged.

The existing live capture script now asserts these four properties for every rendered table header and records them in `tableHeaderChecks`.

## Executed evidence

- Before CSS correction, the new browser assertion failed on the first state: actual height 34, expected 32.
- After correction: 105 screenshots completed; 86 states contained 692 checked header cells, all passing. The matrix includes en-US/zh-CN, light/dark and desktop/mobile, with the existing Target list/detail/create, filtering, keyboard and locale persistence checks.
- Browser plugin unavailable; reused installed Brave/CDP capture. Real page: `http://127.0.0.1:4174/`, proxying the disposable Control Plane at port 18085 and its real PostgreSQL database. No API interception or mocked responses.
- Project `project-980dce10d0c6c2951be3289b99780554` contained three SDK-registered Targets, Docker/Kubernetes/SSH. All remain unprobed with unconfigured references and `.test` endpoints; none was probed or deployed.
- Screenshots and manifest: `/tmp/cloud-agents-table-density.Selwbh/after/`. Manifest SHA-256: `8023e441dd9b53495bd5d8c43325f9eca8c6fbc1a69ea6be4aefd15aee9215c5`.
- Visually reviewed the corrected English/light desktop list and Chinese/dark mobile list against the reference. No full-page pixel equivalence is claimed.
- Browser recorded only the Vite origin, zero mutation requests, no warnings; retained four quota-not-found 404 and eight unconfigured cleanup-preview 503 resource errors. Ten ordinary-user Admin reads returned 403. These are expected tested states, not a claim of zero console errors.
- Admin Web: all 30 tests, TypeScript and build passed. Scoped capture lint/format and `git diff --check` passed. Existing >500kB bundle warning remains.

## Cleanup and remaining boundary

Stopped the task-owned Vite PID 58774 and local stack parent PID 58546. The stack's cleanup removes only its disposable database container, runtime and directory; screenshots remain in `/tmp`. No existing infrastructure or unrelated dirty work was changed.

This closes the shared table-heading mismatch, not complete ADMIN-M1 visual acceptance. Full live/reference comparison and current real Docker/Kubernetes/SSH Probe-ready evidence remain incomplete. No new credentials or destructive-operation approval were required for this slice.
