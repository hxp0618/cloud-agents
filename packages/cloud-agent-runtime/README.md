# `@cloud-agents/cloud-agent-runtime`

The default entrypoint exposes the app-neutral Runtime registry, stdio runtime
transport, and stdio client.

Portable Cloud Agent runtime, explicit provider registry, stdio transport, and the one-minor Provider Host v2 compatibility implementation. Provider packages are registered explicitly by the distribution or host.

Provider execution is supplied only through explicitly registered Provider
plugins. Codex and Claude lifecycle code and upstream dependencies live in
their respective Provider packages; Runtime does not import either package.

Hosts compose only the plugins they enable. For example, a Codex-only host uses
the existing Codex adapter without changing Runtime:

```ts
import { createCodexProvider } from "@cloud-agents/cloud-agent-provider-codex";
import { createCloudAgentRuntime } from "@cloud-agents/cloud-agent-runtime";

const runtime = createCloudAgentRuntime({ providers: [createCodexProvider()] });
const descriptor = await runtime.describe("codex");
```

Omitted providers cannot be described or create sessions. Registration rejects
duplicate identities and incompatible plugin ABI versions. A host passes its
workspace authority, credential source, logger, and artifact receiver to
`createSession`; provider-specific configuration stays in its `configuration`
argument. Consume `session.events` and call `session.close()` in `finally` (or
use `await using`). A malformed returned session is disposed before its boundary
validation error is returned.

Command failures are isolated to their request. A rejected Provider event
stream is a process-fatal transport failure so the host cannot continue with
an untrusted or incomplete event sequence.

The stdio Runtime applies one bounded cleanup budget to closing a Provider
Session and draining its event stream. This boundary applies to explicit stop,
Session replacement, late creation, and Runtime shutdown. A rejected or timed
out cleanup is fatal: the Runtime does not admit a replacement Session or
report a clean shutdown after Provider resources failed to converge.

Managed MCP calls are authorized by the materialized capability grant and
proxied only to its allowed upstream host. The broker bounds request and
response sizes, applies one deadline to the complete upstream exchange, and
cancels unfinished upstream work when the broker closes.

`createCloudAgentStdioClient({ command, extendEnvironment: false, environment })` fully replaces the child environment and cannot re-inherit ambient trust. The compatibility default is `true`. `subscribe(listener)` preserves the original unsubscribe API and now treats a returned promise as an ordered receipt barrier before the next frame and terminal resolution.

The client treats stdout EOF as a fatal transport closure even if the child
process remains alive. It first consumes every complete queued frame and waits
for subscriber receipt acknowledgements, then rejects unfinished commands and
reaps the child process.
