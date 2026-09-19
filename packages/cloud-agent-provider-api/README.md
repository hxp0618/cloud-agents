# `@cloud-agents/cloud-agent-provider-api`

Host-neutral Provider Plugin ABI built from plain JavaScript objects, `Promise`, `AsyncIterable`, `AbortSignal`, and JSON-compatible values.

Each plugin exposes `describe()` and `createSession()`. The host supplies
workspace, credential, logging, and optional artifact services through the
session context; the session owns `execute()`, `events`, `close()`, and
`Symbol.asyncDispose`. Runtime disposal uses `close()` when a JavaScript plugin
does not expose `Symbol.asyncDispose`. Hosts enable plugins by passing an explicit
array to the Runtime registry; omitting a plugin disables it, and duplicate or
ABI-incompatible entries fail during registration.
