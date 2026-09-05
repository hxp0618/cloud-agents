# BASE-M0 OpenSandbox candidate qualification — 2026-09-05

Source: Cloud Agents `cde5bdb07117526af4fc4595d94b43a6a8ab7880`, current branch
`codex/cloud-agents-platform-p0`. User explicitly migrated this task to BASE-READY / BASE-ADMIN-V1.
Only the new candidate script, this evidence and 06 are changed by this slice. Existing dirty
`.gitignore`, `go.work.sum`, and `docs/img.png` remain untouched. No product API or old actuator changed.

## Candidate, license and runtime

- OpenSandbox `server/v0.2.2`, source `207d94c7dc7735c143856fe5c6538b743e478786`.
  Inspected using `git show` in the clean, read-only adjacent OpenSandbox checkout; its current
  HEAD is newer and was not used as the candidate source. No checkout/reset/edit in that repository.
- Server and execd OCI digests plus sandbox image are retained in [actual results](base-m0-opensandbox-candidate-20260905.json).
  Server image was already local; execd `v1.0.21` was pulled from the upstream regional registry.
  No images were built, pushed or published. Image tag/digest selection is not a signature/SBOM attestation.
- Fixed candidate [LICENSE](https://github.com/alibaba/OpenSandbox/blob/207d94c7dc7735c143856fe5c6538b743e478786/LICENSE)
  declares Apache-2.0; SHA-256 `50e6751797c50dedd75ef1b8a0d9e42f5f8472e9fbce91f34718e9f97b0c780a`.
  No root NOTICE file at that ref. This is candidate source-license identification, not a complete
  image dependency/license or release supply-chain clearance. No upstream implementation was copied.
- Docker context explicitly `orbstack`, engine 29.4.0, linux/arm64; trusted local runc path only.
  No Kubernetes, customer node, gVisor/Kata or shared-untrusted-tenant qualification.
- Node 24.18.1 runs the dependency-free qualification script. Localhost API port 18890;
  execd host port range 49100–49300, advertised host 127.0.0.1. No Provider credentials supplied.

## Actual flow and observations

```sh
node scripts/test-base-opensandbox-docker.mjs NEW_OUTPUT_DIRECTORY
```

The script refuses existing output directories and refuses to start the candidate server if any
Docker object has `opensandbox.io/id`: the candidate restores existing resources on startup, so
it must not share this test engine with another active OpenSandbox installation. It checks all
fixed images exist locally, then creates only uniquely labelled test resources. No API interception.

Final accepted output: `/tmp/cloud-agents-base-m0-final/evidence.json`, plus private `server.log`.
Nine checks/observations completed:

1. Missing candidate API key returns 401. This is not a replacement for Cloud Agents user/Admin 403.
2. Nonexistent PVC/named volume returns 400 with `VOLUME::PVC_NOT_FOUND`; no Sandbox allocated,
   and the separately owned test Workspace volume remains.
3. Exact repeated creation spec/operation metadata creates two distinct Sandbox IDs. The duplicate
   is explicitly removed before data is written. Upstream lifecycle creation is not CP idempotency
   and permits multiple writable mounts: CP admission/fencing remains essential.
4. Sandbox starts without installed `codex` or `claude`; those absence checks guard the command.
   Exec writes only generated test bytes, then computes SHA-256.
5. Candidate DELETE removes compute, not the pre-created named volume configured with
   `createIfNotExists=false` and `deleteOnSandboxTermination=false`.
6. New Sandbox mounts the same volume; Exec digest and Files download exactly match original bytes.
7. Candidate list discovers the running Sandbox and its operation metadata.
8. An invalid entrypoint is initially accepted as Running, then observed Failed/exit 127 after one
   second. A successful create response is insufficient for actual workload readiness.
9. Explicit cleanup of that failed Sandbox preserves the independently owned volume. Final teardown
   checks zero containers and volumes with this run's label; all test volume bytes are then removed.

Early attempts: the first startup ended in ECONNRESET with no accepted result; its precise cause
is unproven. Readiness waiting and sanitized log retention were added. One missing-volume assertion
used the source constant name instead of its wire value and was corrected from the fixed source.
An expected immediate 500 for a bad entrypoint was contradicted by the real asynchronous lifecycle;
the final test records initial and later states, rather than claiming automatic compensation.

Script syntax/scoped lint/format and diff checks passed. Product builds and unrelated suites were
not rerun because no product source/contract was changed. Candidate logs are private and redact
the generated API key; config remains mode 0600 in the private output directory, not Git evidence.

## Domain/API seam mapping from current source

| Foundation concept    | Existing authority to reuse                                                 | Semantic gap; do not rename away                                                                                                                                                          |
| --------------------- | --------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Workspace / Volume    | Docker actuator volume inspection/ownership checks                          | `CleanupWorker` calls `removeWorkspaceVolumeIfUnused`; old Lease cleanup semantics are not durable Workspace ownership. Add an independent resource; do not adopt old volumes implicitly. |
| Sandbox execution     | Candidate lifecycle API plus execd Command/Files; existing target selection | Candidate ID/metadata are physical receipts, not tenant authorization, generation fencing or a durable product Operation.                                                                 |
| RuntimeProfile        | Existing EnvironmentProfile validation/generation machinery                 | Current schema requires providerKinds/providerCredentialRef. Keep Agent profile unchanged; no-Agent runtime selection must not invent a Provider credential.                              |
| RemoteWorker          | Existing Worker observer and SSH actuator transport conventions             | Lease-backed execution Worker and inbound SSH are not outbound customer-node identity/enrollment. No new model is instantiated by this PoC.                                               |
| Operation / reconcile | `internal/store/postgres/durable_coordination.go` outbox claim/ack/retry    | Wire accepted intents to actual adapter recovery; HTTP-local control and metadata lookup alone cannot provide crash-safe single writer.                                                   |
| Admin                 | Existing Target/Worker/Release/Profile/Operation/Audit surfaces and scopes  | Reuse real facts and safe metadata. Do not expose execd endpoints to User Web or test file content in Admin; no placeholder Sandbox page is added.                                        |

Relevant sources: `services/control-plane/internal/dockertarget/actuator.go`,
`contracts/platform/v1alpha1/schemas/environment-profile.schema.json`,
`services/control-plane/internal/store/postgres/durable_coordination.go`, and existing Admin App.

This is an executable candidate PoC, not BASE-M0 completion. CP adapter/adopt/idempotency,
partial-allocation compensation, content authorization, single-writer fencing, retention authority
and the corresponding Admin lifecycle remain open. Current status and next action are only in 06.
No existing resources or user data were cleaned, no production deployment or formal Gate was closed.
