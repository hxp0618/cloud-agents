# Cloud Agents Admin Web

Delivery follows the [foundation-first plan](../../docs/plan/cloud-agents-platform/04-extraction-and-migration.md). Admin Web and Workspace/Sandbox/customer-node infrastructure are one first-stage deliverable: a capability is incomplete if either its real backend or its corresponding Admin workflow is missing. User conversation features follow their joint acceptance. Admin Web must not expose user files, terminals, conversations, credentials, or enrollment secrets. Existing Lease-backed Workers are not the future outbound RemoteWorker node fleet. Reuse these operational views as real foundation APIs arrive; do not advertise planned capabilities as implemented.

Independent Vite + React console for Control Plane Admin API operations. It uses the generated Admin SDK for Deployment Targets, a combined Cluster/Host and Lease-backed Worker view, Environment Leases, immutable Environment Profiles, and project Maintenance Operations, and keeps the bearer token in memory.

The interface supports `zh-CN` and `en-US` through its local typed message catalog and browser-native `Intl`. First visit follows the leading browser language (`zh*` selects `zh-CN`; everything else selects `en-US`), the account menu changes language immediately, and the selected locale survives refresh in `cloud-agents-admin-locale`. Invalid locale values fall back to `en-US`.

```bash
bun --filter @cloud-agents/cloud-agent-platform-sdk build
CLOUD_AGENTS_CONTROL_PLANE_URL=http://127.0.0.1:8080 bun --filter @cloud-agents/admin-web dev
```

Open `http://127.0.0.1:4174`, enter the tenant/project IDs, and paste the token written by the local Control Plane `--local-admin-token-file` option. The ordinary `--local-token-file` token is expected to receive HTTP 403 from these routes. Neither token nor infrastructure credential bytes are persisted in browser storage.

Network Policies use the generated SDK and PostgreSQL authority (`network-policies.list/get/update` and `audit.list`). Saves require an idempotency key and expected resource version; any Profile reference makes the policy immutable. Create a new policy ID for changes. The Profile editor selects existing policies, and User Web receives only `networkSummary`, never network references or endpoints.

Mutation retries reuse an idempotency key only for the same operation, resource and complete request body. Editing the body creates a new request identity; successful operations discard their pending key.

Connection loads only the overview's resources and verifies Deployment Target authority. Navigation and refresh load the current page's resources plus its editor dependencies; the Capability page keeps its loading lock until the initial bounded Session/Execution metadata window is also ready. Unrelated quota/audit or capability failures do not disconnect the console. Each failed resource stays visibly unavailable (or retains its previous authority after refresh), while successful resources update independently; a failed MCP or Skill refresh therefore does not block a fresh Session/Execution window. A failed refresh is never reported as successful.

Managed Agent binding/recovery metadata is read in bounded windows. Session lists are filtered by Sandbox in the Control Plane and read in pages of at most 64; Executions are read only for the selected Session, also 64 at a time. Capability and Sandbox recovery views label these as the current Session and Execution windows, replace the window when the operator continues, and refresh the current window instead of draining project history. They select a Session and explicitly read its audit stream in pages of at most 64 events. The view labels the current page, further available history and the end of the stream; it does not claim to show the project's newest events or complete history before reading it. Switching Session, Sandbox or connection cancels the old request, and missing, stalled or repeated continuation cursors fail visibly without discarding the prior page.

After an operator reconciles an uncertain Managed Agent side effect, the Sandbox recovery view reloads the same Session and Execution windows and keeps the selected Session. This preserves the reviewed recovery context while showing the updated authority.

Lifecycle polling requests only the resource classes that currently have pending work (or visible Worker health to observe). Each response is applied independently, so a transient failure in one resource class does not discard fresh authority returned for another. A page load in progress suppresses the poll tick to keep older polling responses from overwriting the newly entered page.

Execution boundary: only public egress with no allowlist/DNS/proxy reference and disabled ingress/preview is currently deployable. Other policy configurations can be stored and bound to drafts, but publication and new environment creation fail closed until target adapters implement their semantics. Policy CRUD is not evidence of enforced network isolation.

The small browser suite in `../../test/e2e/admin-web.e2e.ts` and
`../../test/e2e/admin-agent-events.e2e.ts` uses pinned `e2e@0.18.0` and
`@e2e-dev/web@0.13.0`. It covers the disconnected Admin entry, access status,
persisted locale, scoped page loading and refresh, and a Session event stream whose
first 64-event page is replaced by the next page and reset when the selected Session
changes. The Managed Agent flow also checks the 64-item Session query, Session and
Execution continuation windows, loading locks while metadata is pending, and that a
Capability refresh replaces the current binding window with newly returned Sessions
even when the independent MCP resource refresh fails.
These flows use intercepted API responses without a Control Plane or
credentials; they are frontend regression evidence, not real-backend UAT.
Authenticated CRUD remains covered by the isolated Compose harness; the browser
suite does not claim that boundary.
