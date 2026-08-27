# Standalone Platform v0.1 — Worker P1-B Supervisor admission independent review

- Review ref: `worker-p1b-supervisor-admission-independent-review-20260827`
- P0 base commit: `9290ec4411f6524f1c515a3dfe1a0b9e32f7e6ce`
- Initial implementation commit: `394286000aad895f4cebd220e6e554489cbe8029`
- Fixed candidate commit: `378d8ccbbf2b6a410c3bec9c17d66863b06e02c6`
- Fixed candidate tree: `a3e92f7e7ef51a48a8ea75cade649e8b48ba29e0`
- P0 base tree: `eb0c7247f8757480ac3f5b60fcb9a04cdd51f2cb`
- Fixed candidate binary diff SHA-256 against the P0 base:
  `3ebd7b6c47a4e1e318bada588f126bfc041216d8dc02b5ce4c50e403591ed983`
- Review branch: `codex/review-standalone-worker-supervisor-admission-20260827`
- Review mode: independent, read-only candidate review. This review commit adds
  only this record and does not modify the candidate implementation.

## Scope and authority check

The fixed candidate adds exactly these three paths:

1. `services/worker/supervisor/README.md`
2. `services/worker/supervisor/client.go`
3. `services/worker/supervisor/client_test.go`

There is no change to Control Plane, D-053-MIG-000014.r1/r2 source/profile/
schema/manifest/SQL/catalog/archive/review bytes, generated contracts or SDKs,
any `go.mod`/`go.sum`, database code, provider code, deployment, release, or
Gate state.

The package consumes the generated `WorkerExecutionServiceClient` interface.
Its configuration has no endpoint, protocol selector, or mutable capability
set. It sends the fixed v1.0 negotiation/health pair and validates the selected
version, exact capability profile, bounded/canonical protocol descriptor,
authenticated Worker identity, opaque negotiation identifier, and future
expiry before storing a detached in-memory binding. `CheckHealth` echoes the
exact binding tuple, checks expiry both before and after the RPC, rejects
descriptor drift, and does not return success after context cancellation.

`DispatchOperation` and `GetOperationReceipt` return stable
`CodeUnimplemented` errors without invoking the generated client. The package
does not create an endpoint, listener, TLS/mTLS configuration, lease, database
session, provider call, workspace, credential, artifact, operation, or durable
receipt. The in-process generated Connect client/handler test uses only an
`httptest` listener and is not production HTTP/TLS evidence.

## Same-slice repair reviewed

The initial implementation audit identified two related fail-closed gaps before
the review verdict was issued: a successful fake/client response could be
committed or returned after its context became cancelled, and a health response
could be accepted after the binding expired while the RPC was in flight. The
same slice repaired both in `378d8cc` by checking context after each RPC,
rechecking binding expiry before returning health, clearing an expired current
binding, and adding focused negative tests. A nil injected clock also now fails
closed. No new contract revision, r3/r4 authority, or external side effect was
introduced.

The fixed candidate was re-read line by line for request construction, response
validation ordering, capability unknown/duplicate handling, descriptor bounds,
identity/digest comparison, negotiation ID UTF-8/control/byte bounds, expiry
equality semantics, copy isolation, locking, RPC error redaction, and no-op
operation behavior. No remaining P0, P1, or P2 finding was found in this bounded
slice.

## Verification evidence

All Go commands ran from `services/worker` with the pinned Go 1.26.6 toolchain,
`GOWORK=off`, and `GOFLAGS=-mod=readonly`.

| Evidence class | Command | Result |
| --- | --- | --- |
| Compile/unit | `go test ./... -count=1 -timeout=5m` | PASS; Worker and Supervisor packages |
| Race | `go test -race ./... -count=1 -timeout=5m` | PASS |
| Static | `go vet ./...` | PASS |
| Module closure | `go mod tidy -diff` | PASS; empty diff |
| Generated in-process seam | `TestGeneratedConnectClientBindsWorkerServiceInProcess` in the unit run | PASS; test-only HTTP, no TLS/external listener claim |
| Focused negative matrix | malformed version/capabilities/descriptor/identity/ID/expiry, typed-nil client, cancellation, pre/post-RPC expiry, descriptor drift, no-op dispatch/receipt | PASS |
| Repository module policy | `bunx vitest run scripts/lib/platform-go-modules.test.ts --reporter=dot` | PASS; 13/13 |

No broad `internal/migration` suite was rerun: the candidate does not touch that
module or any D-053 bound input, and repeating the long migration command would
not add evidence for this Worker-only slice.

## Verdict

**APPROVE** for fixed candidate
`378d8ccbbf2b6a410c3bec9c17d66863b06e02c6` only.

- P0: 0
- P1: 0 (the pre-verdict same-slice fail-closed gaps were repaired and rechecked)
- P2: 0

This approval does not authorize operation dispatch, durable receipts,
production HTTP/TLS, database/provider/workspace/credential/artifact effects,
deployment, publication, release, or Gate closure. It does not modify,
supersede, or close D-053-MIG-000014.r2 or D-053-EC-2.r3.
