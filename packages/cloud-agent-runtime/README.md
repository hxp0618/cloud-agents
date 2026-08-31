# `@cloud-agents/cloud-agent-runtime`

The default entrypoint exposes the app-neutral Runtime registry, stdio runtime
transport, and stdio client.

Portable Cloud Agent runtime, explicit provider registry, stdio transport, and the one-minor Provider Host v2 compatibility implementation. Provider packages are registered explicitly by the distribution or host.

Provider execution is supplied only through explicitly registered Provider
plugins. Codex and Claude lifecycle code and upstream dependencies live in
their respective Provider packages; Runtime does not import either package.

`createCloudAgentStdioClient({ command, extendEnvironment: false, environment })` fully replaces the child environment and cannot re-inherit ambient trust. The compatibility default is `true`. `subscribe(listener)` preserves the original unsubscribe API and now treats a returned promise as an ordered receipt barrier before the next frame and terminal resolution.
