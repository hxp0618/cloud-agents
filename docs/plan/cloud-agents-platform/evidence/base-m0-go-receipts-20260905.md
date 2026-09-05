# BASE-M0 Go runtime receipt seam — 2026-09-05

Source base `28f0dff2`, unchanged branch `codex/cloud-agents-platform-p0`.
Changes are the new concrete Go `internal/opensandbox` package, its tests, existing candidate
harness and current-state/evidence records. Unrelated `.gitignore`, `go.work.sum`, `docs/img.png`
are preserved. No dependencies, public contracts, old actuators or frontend source changed.

## Delivered execution seam

- `Find` uses the pinned candidate's paginated metadata query, then verifies tenant, project,
  Workspace, Sandbox, operation, generation and full specification digest on every returned item.
  Multiple receipts, ownership mismatch, unknown runtime state or incomplete pagination fail closed.
  It searches up to 100 pages and never treats the bound as evidence of absence.
- Candidate metadata only accepts Kubernetes label values. Identity values are limited to 63
  valid characters; a SHA-256 is carried as two 32-character hex labels, preserving every bit.
  The harness's fixed digest is a test fixture, not production canonical-spec computation.
- Observations expose only physical ID and runtime state. `Running` is deliberately not named
  Ready; candidate error messages, endpoints, entrypoint, credentials and file content are not returned.
- `Delete` re-reads and checks the exact receipt before termination; absent resources and repeated
  deletion succeed. It does not change upstream volume policy. Retention must be established by
  the trusted creation path; the real harness fixes both volume auto-create/delete flags to false.
- HTTP uses a configured HTTPS origin (numeric loopback HTTP for local testing), no redirects or
  environment proxy, bounded responses/timeouts, and generic errors instead of upstream bodies.
  Bad inputs are rejected before requests. No framework, SDK package or in-memory coordination added.

`Find` is a recovery building block, not an atomic ensure/create operation. The caller must own
a durable claim before mutation; metadata checks do not implement cross-process fencing or
compare-and-delete. This package is exercised by the real harness, not yet wired to production
CP handlers, an outbox Controller, user authorization or Admin workflows.

## Current validation

Same fixed candidate images as the preceding M0 record: server v0.2.2, execd v1.0.21 and no-Agent
Node sandbox, selected by OCI digest. Backend `orbstack`, Docker 29.4.0 linux/arm64. No Provider
credentials or existing resources were used. Local API remains `127.0.0.1:18890`.

```sh
go test -race ./services/control-plane/internal/opensandbox
go vet ./services/control-plane/internal/opensandbox
node scripts/test-base-opensandbox-docker.mjs NEW_OUTPUT_DIRECTORY
```

Toolchain: Go 1.26.6 / Node 24.18.1. The harness now invokes `TestLiveDiscovery` with private
environment inputs; no credential is placed in CLI arguments or test output. Without the harness,
that live test explicitly skips rather than returning fake success.

- Unit/race checks and vet passed. Guards cover wrong owner, stale generation, either digest half,
  duplicate receipts, unknown state, unsafe ID, invalid identity, bad URLs, redirects, oversized or
  incomplete responses, second-page discovery, cancellation and upstream error redaction.
- Actual Docker run passed 14 candidate checks/observations, including five Go invocation phases:
  discover existing receipt; reject duplicate; inspect/stale-reject/delete/re-delete the original;
  repeat for the rebuilt Sandbox; observe Failed then guard/delete/re-delete the bad-entrypoint Sandbox.
- The separate volume survived compute cleanup; regenerated Sandbox Exec and Files matched
  the prior data digest. Missing key and missing volume checks remained. These are not Admin 403/RLS tests.
- An initial live attempt exposed the metadata limit and rejected raw `sha256:…`; source labels
  were corrected without weakening digest matching. Final current-source run completed successfully.
- Final output `/tmp/cloud-agents-base-go-current/evidence.json` and private sanitized server log;
  [durable results](base-m0-go-receipts-20260905.json) retain actual identities and observations.
- Syntax/scoped format/lint/diff checks passed. No app build, browser visual matrix or unrelated
  suite was rerun, since no app source/public contract was changed.

The harness cleaned only this run's labelled containers and disposable volume; current Docker
queries confirm none remain. Generated config is private mode 0600 and is not copied into Git.
No existing data, images or runtime resources were deleted, no image was published and no Gate closed.

## Remaining boundary

No product create path, persistent Workspace/Volume record, durable Operation/claim connection,
automatic retry/adopt, actual Ready authority, access isolation or Admin lifecycle is yet qualified
by this package. Trusted candidate metadata is not a substitute for CP tenant authorization.
BASE-M0 and BASE-READY remain open; the only next-action/status record is 06.
