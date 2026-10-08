# `@cloud-agents/cloud-agent-provider-api`

Host-neutral Provider Plugin ABI built from plain JavaScript objects, `Promise`, `AsyncIterable`, `AbortSignal`, and JSON-compatible values.

Each plugin exposes `describe()` and `createSession()`. The host supplies
workspace, credential, logging, and optional artifact services through the
session context; the session owns `execute()`, `events`, `close()`, and
`Symbol.asyncDispose`. Runtime disposal uses `close()` when a JavaScript plugin
does not expose `Symbol.asyncDispose`. Hosts enable plugins by passing an explicit
array to the Runtime registry; omitting a plugin disables it, and duplicate or
ABI-incompatible entries fail during registration.

`close()` always releases the local event queue and credential lease. It rejects
when the Provider `StopSession` reports a timed-out or failed outcome, so callers
cannot treat an unconfirmed Provider shutdown as successful cleanup.

The shared `createProviderPlugin` adapter owns the model-only configuration used
by all bundled Providers, including its schema, validation, and Host model
binding. Unknown fields are rejected before acquiring credentials. Plugins
implementing the public ABI directly own any provider-specific configuration
validation in `createSession`.

Provider session admission validates optional conversation history before any
Provider operation starts. Initial history may be omitted or empty. History
contains `user`/`assistant` text messages and has one 32 MiB JSON byte budget
shared by admission, running sessions and persisted emulated recovery; there
is no separate entry-count or per-message limit. The existing 2 MiB command
envelope limit still applies to history supplied over stdio. History accumulated
inside a Provider session is passed directly to its executor, and emulated
recovery reads the local state file without sending it through a command frame.

Before starting a Turn, the host accounts for its input and reserves room for
one maximum-size Result. A full history rejects new work with a user-action
error before invoking the Provider; it does not truncate prior conversation.
Persisted history must be nonempty, and successful persistence precedes the
in-memory session update, so completed histories remain readable on restart.
A failed history commit invalidates the active Session and prevents further
Provider work until the caller explicitly starts or resumes a Session.

Within one Provider host/plugin Session, `commandId` identifies the complete
command envelope, including its request ID and timestamp. An identical retry
shares in-flight work or replays its terminal receipt; reusing the ID with any
different command content fails as a protocol violation. The replay ledger keeps
at most 4,096 command identities and 8 MiB of full terminal receipts. When a full
receipt ages out, its bounded identity remains and a later retry fails closed
instead of repeating Provider work. Once the identity limit is reached, callers
must create a new Provider host/plugin Session; `StopSession` remains available
so capacity cannot prevent cleanup. At a saturated boundary, the host permits
one emergency `StopSession` and one emergency `InterruptTurn` concurrently;
another command of the same control type fails immediately with a retryable
capacity error instead of creating unbounded cleanup work.
Emergency controls share only in-flight retries and are not added to the replay
ledger; they cannot admit new Provider work.

`CLOUD_AGENT_ENVIRONMENT` is the single public name registry for Runtime and
Provider environment variables. Consumers read or write those named properties
on their environment object directly; the package does not wrap ordinary object
access.
