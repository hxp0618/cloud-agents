# Worker operation-admission P1 kernel — independent read-only review

- Review date: 2026-08-27 (Asia/Shanghai)
- Review branch: `codex/review-standalone-worker-operation-admission-20260827`
- Fixed candidate: `32b21db7cb93668c6430e2cef62518baba19698b`
- Fixed tree: `0c95d3366e6539f718d79695c0c33fb21f8c9954`
- Candidate parent: `e32caa486e2130ec2e0dae26bbdceaed14f88924`
- Reviewer: independent read-only reviewer (no candidate edits)

## Verdict

**APPROVE — P0=0 / P1=0 / P2=0.**

This verdict is limited to the fixed local, transport-neutral Worker
operation-admission kernel and its explicit Supervisor binding profile. It is
not production Worker, HTTP/TLS, PostgreSQL, Provider, deployment, release,
or Gate-closure evidence.

## Scope and lineage checks

- The candidate is a single-parent append-only successor of `a72ca9b` via
  `24dd2e9` → `e32caa4` → `32b21db`; no squash, rebase, amend, or force update
  is present.
- The candidate changes only Worker admission implementation/tests/docs and
  `services/worker` module closure. The D-053-MIG-000014.r1/r2 migration
  source/profile/schema/manifest/catalog/archive/review objects are not in the
  candidate diff.
- The following r1/r2 blobs are byte-identical to `a72ca9b` (and therefore
  retain their prior frozen identities):

  ```text
  services/control-plane/migrations/successor/000014/authority-source.json
  services/control-plane/migrations/successor/000014/authority-source.schema.json
  services/control-plane/migrations/successor/000014/profile.json
  services/control-plane/migrations/successor/000014/profile.schema.json
  services/control-plane/migrations/successor/000014/manifest.json
  services/control-plane/migrations/successor/000014/schema-bundle.json
  services/control-plane/migrations/successor/000014/runner-binding/authority-source.json
  services/control-plane/migrations/successor/000014/runner-binding/profile.json
  ```

## Review matrix

| Area | Result | Evidence |
| --- | --- | --- |
| Fixed generated/in-memory profile | PASS | `OperationAdmissionProfileID` is immutable, `ExternalSideEffects=false`; claim retains references/digests only. |
| Canonical request and digest | PASS | RFC-8785 projection is explicit, excludes raw fencing token/digest field, recomputes SHA-256, and golden bytes/digest are tested. |
| Identity and negotiation binding | PASS | Authenticated client identity, expected Worker identity, server-issued negotiation ID/expiry and negotiated operation capability are checked before recording. |
| Fencing and generation | PASS | Lease ID, generation and bounded token are checked against the configured authority; token is represented only by SHA-256 digest in claims. |
| Retry/idempotency | PASS | Records key by `operationID\0attemptID`; exact replay is detached; operation identity/digest/fencing conflicts fail closed; strictly increasing attempt numbers are required and tested. |
| Unknown/invalid input | PASS | Recursive protobuf unknown fields, malformed scope/deadline/command/finalizer/extension payload, digest drift and capacity/cancellation paths are covered. |
| Supervisor compatibility | PASS | Existing `Bind` remains health-only; explicit `BindOperationAdmission` negotiates the three-capability versioned profile; bound capabilities are reused by `CheckHealth`; missing operation capability fails closed. |
| No side effects | PASS | No database, network, Provider, workspace, credential, artifact, execution, deployment, release, or receipt write is introduced; `ExecuteOperation` and `GetOperationReceipt` remain `Unimplemented`. |

## Verification evidence

Executed from `services/worker` with `GOWORK=off GOFLAGS=-mod=readonly`:

```text
go test ./... -count=1 -timeout=5m    PASS (worker, supervisor)
go test -race ./... -count=1 -timeout=5m    PASS (worker, supervisor)
go vet ./...    PASS
go mod tidy -diff    PASS
```

`git diff --check a72ca9b 32b21db` also passes. These are local compile/unit,
race, vet, and module-closure checks only; no live HTTP, mTLS, database,
Provider, or production execution was performed.

## Non-claims and gate boundary

The Worker and Supervisor remain in-memory/transport-neutral seams. The
operation capability is admission-only; dispatch and durable receipts are
still not implemented. This review does not modify or supersede
D-053-MIG-000014.r1/r2, does not authorize canonical/production Runner,
PostgreSQL writes, HTTP/P2/Provider effects, deployment/publication, or any
Gate closure.

