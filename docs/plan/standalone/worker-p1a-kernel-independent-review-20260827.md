# Standalone Platform v0.1 — Worker P1-A kernel independent review

- Review ref: `worker-p1a-kernel-independent-review-20260827`
- Candidate commit: `0b90039b53b97f43f62d3455415a35cd86972ab1`
- Candidate parent: `fde46b4857ed859e7c7cd1c97a219b7569b9a071`
- Candidate tree: `8d0a1a146ba55946b9d45690e2fe47c2a1a8648d`
- Parent tree: `88fa9adb9a33ae1a48abfc22e8369ea58a5e03ea`
- Candidate diff SHA-256 (`git diff --binary parent...candidate`):
  `60dfa48dc7673a688937aedd226d5654d11ba9883347c047a21e33f4220e7550`
- Review branch: `codex/review-standalone-worker-kernel-20260827`
- Review mode: independent, read-only candidate review. This commit adds only
  this review record; the candidate implementation is not modified.

## Scope and authority check

The candidate changes exactly six paths under `services/worker`: `README.md`,
`doc.go`, `go.mod`, `go.sum`, `service.go`, and `service_test.go`. There are no
changes to proto sources/generated SDKs, control-plane, database, provider,
workspace, credential, artifact, deployment, release, or Gate state.

The implementation is an in-memory, transport-neutral kernel. `Negotiate` and
`CheckHealth` perform identity-bound v1.0 negotiation and expiry checks;
`ExecuteOperation` and `GetOperationReceipt` return stable Unimplemented errors
without side effects. The generated Connect handler appends fixed 1 MiB read
and send ceilings after caller options. The default identity provider rejects
calls; request-carried expected identities are peer constraints and are not
used as caller authentication.

The identity, version, capability, expiry tuple, collision, nil/context,
concurrency/map-locking, descriptor capability honesty, no-op operation methods,
and handler option ordering were reviewed line by line. No additional P0 issue
was found.

## Finding

### P1-1 — negotiation IDs bypass the contract string/identifier bounds

**Location:** `services/worker/service.go:199-208,258-264`

The Worker contract establishes a hard `max_string_bytes` ceiling of 1,024 and
an identifier ceiling of 256 bytes. `validateBinding` checks only that
`negotiation_id` is non-empty, then uses the unvalidated string as a map key.
It does not reject invalid UTF-8 or overlong IDs before lookup. In addition,
`Negotiate` accepts any non-empty value returned by the injected `IDGenerator`
without validating UTF-8 or length before inserting it and returning it in the
response. A malformed or oversized generator result can therefore create a
binding/response outside the advertised descriptor, while an inbound oversized
ID reaches the decoded handler and is classified as `negotiation_unknown`
instead of a deterministic bounds rejection.

**Required repair for the same candidate:** validate generated IDs and inbound
binding IDs for valid UTF-8 and the contract identifier/max-string byte limits;
return a stable fail-closed bounds/invalid error and do not perform map lookup
or state insertion for invalid values. Add focused negative tests for both
directions. This is a P1 contract/security correctness issue, not a request to
implement operation dispatch.

## Verification evidence

All commands below used the pinned Go 1.26.6 toolchain, `GOWORK=off`, and
`GOFLAGS=-mod=readonly`, from `services/worker`:

| Evidence class | Command | Result |
| --- | --- | --- |
| Compile/unit | `go test ./... -count=1 -timeout=5m` | PASS (`ok`, package tests) |
| Race | `go test -race ./... -count=1 -timeout=5m` | PASS |
| Static | `go vet ./...` | PASS |
| In-process handler | included by `TestConnectHandlerInProcess` in unit run | PASS; no external listener claim |
| Repo-root module check | `go test ./services/worker/...` with `GOWORK=off` | NOT RUNNABLE from root: root is not a Go module; module-local checks above are authoritative |

No deployment, release, production database write, provider E2E, production
HTTP/TLS listener, or external side effect was run or authorized. This review
does not close any Gate, does not alter the standalone scope, and keeps
Synara/T3 deferred. It does not extend or supersede any D-053 authority.

## Verdict

**REJECT pending P1-1 repair and one re-review of the same candidate scope.**

- P0: 0
- P1: 1
- P2: 0

After the bounded-ID repair is supplied as a new candidate commit, this review
must be rerun against that fixed SHA; no r3/r4 scope is implied.
