# Worker local operation execution seam — independent review

Date: 2026-08-28
Candidate: `9ebee1a5bf461f284189d13431910c15a99bb29d`
Parent: `c8235878b178a24e42e810a3053b9a19cc90c42d`
Candidate tree: `da4853a8691f8710737f68fda1f9cf2d3f4b250e`

## Verdict

**APPROVE — P0=0, P1=0, P2=0 for this candidate.**

This is an independent, read-only review of the Worker localdev operation
execution/receipt seam. The candidate performs only bounded process-local
admission, deterministic/local executor invocation, and in-memory detached
receipt handling. No PostgreSQL/DB, HTTP client or listener, provider,
workspace, credential, artifact, Supervisor dispatch, deployment, release, or
other external effect is introduced by the implementation. The injected
`OperationExecutor` is an explicit process-local seam whose contract requires
side-effect-free implementations; the built-in executor handles only Probe and
ValidateBinding.

## Scope and authority checks

The candidate changes exactly these six paths relative to its parent:

- `services/worker/service.go`
- `services/worker/operation_admission.go`
- `services/worker/execution.go`
- `services/worker/execution_test.go`
- `services/worker/README.md`
- this slice's implementation record

Admission recomputes the canonical digest, validates normalized scope,
deadline, finalizers, unknown fields, negotiated capability, expected Worker
identity, authenticated client identity, lease and generation fencing, and
bounded in-memory capacity before execution. Admission records bind the
authenticated client; exact-attempt replay and receipt replay compare that
identity, preventing a second negotiated client from reusing another client's
operation or receipt. Receipt reads revalidate server identity, negotiation,
capability, operation/receipt IDs, and the raw fencing proof against the stored
digest. Raw fencing tokens and executor error text are not retained; receipt
summaries are bounded and reject credential-like markers, and result/finalizer
references reject unknown fields and invalid shapes.

The receipt map is process-local and bounded to 1024 records. Responses are
protobuf-cloned before return, and same-attempt concurrent execution is
serialized so the executor is invoked once. Documentation calls these
receipts detached/ephemeral and explicitly says they are not durable receipts,
leases, provider authorization, or production dispatch.

## Verification

Executed from `services/worker` with `GOWORK=off GOFLAGS=-mod=readonly`:

- `go test ./... -count=1` — PASS (`worker`, `worker/supervisor`)
- `go test -race ./... -count=1` — PASS (`worker`, `worker/supervisor`)
- `go vet ./...` — PASS
- Cross-client admission/Execute replay negative test passes with
  `client_identity_mismatch`.
- Focused receipt negatives pass for wrong operation/receipt, fencing,
  capability, and unknown fields; sensitive summary is rejected.

## D-053 and Gate boundary

The candidate does not modify generated contract/SDK bytes, migrations, or
D-053 authority/evidence objects. A parent-to-candidate diff over the 147
tracked D-053/g-contract/generator-supply/review-binding paths reports
`D053_CANDIDATE_PATHS_UNCHANGED`. This review does not run native replay,
assemble or rebind supply evidence, write a generation lock, access a
database, invoke HTTP/provider/P2 code, deploy, publish, or close/reclassify
any Gate. P2 follow-ups such as receipt/admission TTL/eviction policy and a
broader classifier for non-summary text fields remain future hardening, outside
this approval.
