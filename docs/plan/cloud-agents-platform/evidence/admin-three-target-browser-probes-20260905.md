# Real three-target browser registration/Probe — 2026-09-05

Source `c3fec3bd81ee18210efc0d69dff983342aa46fcd`, same
`codex/cloud-agents-platform-p0` checkout. This advances ADMIN-M1 browser acceptance;
neither the milestone nor the Goal is declared complete.

## Verified slice

Admin Web at `http://127.0.0.1:4174` → Register target form → select the created row →
detail Run probe → Ready → successful Operation and matching Audit. No application source,
contract or SDK change was needed. Browser requests used the application's generated SDK;
no browser response mocks, injected resource state or static data were used.

Six new Targets were registered/probed through the browser: Docker/Kubernetes/SSH in each of
en-US and zh-CN. English mutations used1440×900/light, Chinese390×844/dark. All six persisted
generation1/resourceVersion3/ready. Twelve actual Admin POSTs succeeded. Each resource has
one successful registration and one successful Probe Operation, and matching success Audit;
Probe also records a requested Audit event. Ordinary user GET for each Target returned403.
Full Operation/Audit associations are retained in the adjacent JSON evidence.

| Real transport | API | Engine |
| --- | --- | --- |
| OrbStack Docker via fresh mTLS read-only gateway | 1.54 | 29.4.0 |
| OrbStack Kubernetes discovery API | 1.35 | v1.35.6+orb1 |
| Independent Alpine OpenSSH container | 2.0 | OpenSSH_10.0 |

Fresh local PostgreSQL17.6 applied all51 real product migrations; CP readiness succeeded.
Final project was `project-fdc0883c0a4bf471b490dc3ef13868c8`. No ready Lease SQL fixture was
needed: all Target observations came from the real protocol calls. This is **Probe only**,
not Worker deployment, workload permissions, Provider execution or lifecycle cleanup evidence.

## Browser checks and evidence boundaries

- Eight combinations of en-US/zh-CN, light/dark and1440×900/390×844 rendered the six real ready
  Targets. Each combination opened all three Target kinds with Enter, showed Ready in detail,
  then Escape restored focus. No document overflow. Page identity/title and nonblank content
  were verified; no Vite overlay or page exception. Language selection survived reload.
- All observed browser API requests remained on the Admin Web origin. Neither Admin nor User
  bearer was in browser storage. These are Admin browser checks, not User Web security acceptance.
- Exactly two console resource errors corresponded to the unset Quota and its Audit404.
  No other failed HTTP response or application exception was observed. Admin27 tests, production
  build and all generated SDK checks passed. Existing530.27kB single-chunk build warning remains.
- Browser plugin and agent-browser CLI were unavailable; the frontend verification skill routed
  to already-installed Playwright/Brave. No dependency installed. Temporary scripts and45 PNGs
  remain at `/tmp/cloud-agents-admin-ui-probes.EoEzWy/` (`browser.mjs`, `audit.mjs`, `setup.mjs`,
  `browser/`). Inspected English desktop SSH Ready, Chinese mobile Kubernetes Operations and
  separate Audit captures. These are current behavior/layout observations, **not complete fixed
  Daytona screenshot-diff acceptance**. No style deviation is approved by this run.
- Initial harness expectations were corrected: registration returns to the list instead of
  automatically opening detail; Audit has three entries (register success, probe requested,
  probe success), not two. Subsequent assertions checked two successful rendered Audit entries
  plus exact API Operation/Audit association. Each retry used another new project, never modified
  a pre-existing user resource. The first two owned projects are removed with the disposable DB.

## Real endpoint provenance and cleanup

- Docker context explicitly `orbstack`; ephemeral TLS server forwarded only GET `/_ping` and
  `/version` to `/Users/huang/.orbstack/run/docker.sock`, with client-certificate authentication.
  All other gateway paths/methods were denied. Gateway log recorded three pairs of200 responses:
  one early harness Probe and the two completed bilingual Probes. No Docker response fabricated.
- Kubernetes context/config explicitly `orbstack` and `/Users/huang/.kube/orbstack-config.yaml`.
  New namespace `ca-admin-probe-20260905-eoezwy`, new `probe` ServiceAccount and30-minute token.
  CP contacted the actual HTTPS `/version` API. No RoleBinding, ClusterRole, Pod or PVC created.
- SSH container `ca-admin-probe-eoezwy`, label `cloud-agents.admin-probe=eoezwy`, loopback random
  port, actual OpenSSH server with generated client/host keys. Password/interactive/forwarding/TTY
  disabled; forced command was `uname -s && uname -m`. Logs showed two accepted public-key
  authentications. No Docker socket, Provider credential or workspace mounted in SSH container.
- Cleanup verified: SSH container, new Kubernetes namespace/ServiceAccount and dev PostgreSQL
  container `cloud-agents-dev-501-25149` absent; own proxy/Vite/CP/Worker stopped; ports4174,
  18085,18095,65338 closed; `.tmp/cloud-agents-dev.6pBNMz` absent. Namespace had no Pod/PVC before
  deletion. Disposable DB and dev tokens were deleted and are not retained for recovery.
- Generated PKI and protocol credentials moved to private
  `/Users/huang/.Trash/cloud-agents-admin-ui-probes.upjHdp` (recoverable). ServiceAccount deletion
  invalidated its token; no test endpoints remain. Base images, pre-existing containers and all
  other namespaces were preserved. No push, image publication or Release.

## Reproduce

Follow `admin-three-target-probes-20260905.md` to prepare fresh real probe-only endpoints and
server credential directories. Start the existing dev script with CP18085/Worker18095 and
Admin Vite4174. Create a fresh project and verify intended Target IDs absent before any writes.

For each language, fill Register target using the actual endpoint and opaque credential reference,
submit, select its list row, Run probe, confirm Ready, scroll through Operations and Audit. Check
ordinary user token403 using the existing generated SDK. Repeat the ready list/detail in both
themes and viewport sizes; verify focus, same-origin requests and no stored bearer. Source-level
backend reproduction remains `scripts/test-admin-target-probes.ts` against new IDs; it intentionally
refuses existing resources. The temporary Playwright scripts reproduce this run when their fresh
project, dev-state and output paths are replaced. Do not reuse already populated IDs or expired
credentials. Tear down only resources created by the reproduction.

## Earliest remaining item

ADMIN-M1's complete fixed Daytona visual/interaction acceptance is still open. The earlier
three-target SDK Probe check now has current browser registration/Probe/Operation/Audit coverage.
M2/M3/M4 still require profile-backed actual Worker/Provider lifecycles, independent production
OIDC and Docker/Kubernetes/SSH zero-residue deployment qualification. This run cannot substitute
for any of those. No additional credential or destructive-operation confirmation is currently
needed to continue the in-scope visual slice. Goal remains active.
