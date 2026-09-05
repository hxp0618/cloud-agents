# Cloud Agents User Console

This is the existing Agent application, not the generic infrastructure foundation. Keep its current flows compatible; new user-side CloudAgents features follow [BASE-READY](../../docs/plan/cloud-agents-platform/05-gates-and-acceptance.md). Long-lived Workspace/Sandbox/customer-node acceptance must work through API/CLI/SDK without a Provider Credential or AgentSession. Existing Agent E2E evidence remains scoped to its tested candidate.

The console uses the generated TypeScript Platform SDK and keeps the bearer token in memory only.
The Control Plane URL, tenant, project, selected published Profile, opaque environment/Session/Turn/
Execution IDs, and event cursor are the only values restored from `sessionStorage`; current state and
the selected transcript are always reloaded from Control Plane.

Users select a published Environment Profile and submit only its ID and immutable version. Control
Plane resolves deployment placement, release, policies, and credentials. The browser has no
infrastructure registration, probe, upgrade, cleanup, or secret-reference controls.

The Agent workspace supports Profile-backed Codex and Claude Code Session create/list/get/close, durable Turn and
Execution start/get/list, and generation-fenced Cancel/Interrupt. Bounded cursor polling stops while
the page is hidden or after terminal state, rejects cursor stalls, de-duplicates events, and refreshes
the authoritative Execution transcript without persisting prompt or message text in browser storage.
Approval and User Input cards send generation-fenced resolutions without persisting answers. Artifact
downloads use the validated message index plus the backend-provided filename, media type, and bytes;
the browser never derives a download path from the candidate payload.
Choose `Plan / user input` for Turns that need Codex `request_user_input`; retries keep that mode with
the original Turn and Execution identity.

For local development, proxy `/v1` to a Control Plane instead of enabling broad CORS:

```sh
CLOUD_AGENTS_CONTROL_PLANE_URL=http://127.0.0.1:8080 bun run --cwd apps/user-web dev
```

Open `http://127.0.0.1:4173`. Production deployments should serve the console and reverse proxy
`/v1` from the same origin.

Checks:

```sh
bun run --cwd apps/user-web typecheck
bun run --cwd apps/user-web test
bun run --cwd apps/user-web build
```
