# Worker local operation execution seam — implementation record

Date: 2026-08-28  
Profile: `cloud-agents/worker-operation-execution/localdev-v1alpha1`  
Scope: standalone Platform v0.1, bounded Worker service slice

## Decision

This slice adds a process-local execution/receipt seam behind an injected
`OperationExecutor`. `DeterministicLocalExecutor` is a side-effect-free
implementation for the two already admitted commands: `Probe` and
`ValidateBinding`. `ExecuteOperation` clones the request, performs the full
`OperationAdmissionProfileID` admission path, invokes the executor once per
operation/attempt, and returns a terminal detached receipt. Replays of the
same attempt return the original receipt without invoking the executor again.

Receipt state is bounded and held only in this process. Sequence numbers,
receipt IDs, operation/attempt identity, client identity, negotiation binding,
and fencing token digest are checked under the local mutex. `GetOperationReceipt`
requires an exact operation and receipt ID and revalidates transport identity,
server identity, negotiated operation capability, and fencing proof. Unknown
fields and oversized requests are rejected before lookup. Raw fencing tokens,
command payloads, and executor error text are never retained in a receipt.

Executor results are restricted to terminal outcomes, bounded redacted
summaries (credential-like markers rejected), validated finalizer/result
references, and no arbitrary metadata. A nil executor leaves dispatch and
receipt retrieval explicitly `Unimplemented` for compatibility with the
admission-only profile.

## Explicit non-scope

This record does not add Supervisor dispatch, PostgreSQL persistence, HTTP or
P2 routes, provider/runtime/workspace/credential/artifact access, production
Runner behavior, deployment, release, or Gate closure. It does not modify any
generated contract bytes, migration/D-053 objects, or existing admission
profile. The Supervisor client remains intentionally unimplemented.

## Verification

From `services/worker` with `GOWORK=off GOFLAGS=-mod=readonly`:

```text
gofmt -w service.go execution.go execution_test.go
go test ./... -count=1   # PASS (worker and supervisor packages)
```

Focused tests cover deterministic success, detached receipt retrieval,
same-attempt replay, fencing/operation/receipt/capability negatives, digest
redaction, and bounded validation. Existing admission and no-op compatibility
tests remain green.
