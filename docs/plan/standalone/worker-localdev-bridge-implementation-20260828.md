# D-057 Worker localdev bridge implementation

This document records the bounded implementation of `D-057-WORKER-LOCALDEV-BRIDGE-000001.r1`. The bridge is a supervisor-side loopback HTTP/Connect client layered over the existing Worker contract. It binds only the generated profile, fixed route, fixed worker/supervisor SPIFFE pair, lease, and generation. Endpoint parsing is fail-closed for non-loopback hosts, invalid ports, userinfo/query/fragment, proxy and redirect settings, and foreign paths. Before negotiation or health acceptance it requires exact D-056 parent health metadata, including authority, revision, profile, and profile digest.

The implementation performs no provider/runtime invocation, database or durable receipt write, public HTTP/P2 call, deployment, publication, or Gate transition. Health metadata is checked exactly before negotiation and health acceptance. Only the health, Negotiate, and CheckHealth routes are allowed by the outbound wrapper. ExecuteOperation and GetOperationReceipt remain unimplemented/no-op, while complete-ledger remains a no-op and entry/recovery writers remain `NOT_IMPLEMENTED`.

## Reproducibility

Generated source/profile/schema/Go files are produced only by `bun scripts/generate-worker-localdev-bridge-profile.ts --write` and verified with `--check`. The generator records the complete sorted regular-file input set, exclusions, generated outputs, deterministic archive/member-manifest algorithms, pinned Go/Bun/Node versions and platforms, process-local receipt path, and single-predecessor D-056 lineage fence. Docs and review bytes are evidence, not generator inputs.

Final generated evidence is source `sha256:a4225472b83f540a591ce483058e8429d404ee497ea5ad8af1c672056771fc1a`, profile `sha256:06899bff4ca8fe12762be3c146fd1220b6eacc6da6687189846b6b62e5bb8ca3`, input manifest `sha256:3a19d9e902f57605d60a4f853704ce00f1a28f28fee5a81d73f41ec394939c70` (46/12/5 input/exclusion/generated counts).

## Focused checks

Run the generator check, the focused Vitest profile tests, oxfmt/oxlint, and pinned Go worker tests (normal and `-tags localdev`, plus race and vet). Verify endpoint and token negative cases, exact health metadata, fixed identity/lease/generation, graceful close, and unimplemented operation/receipt behavior. Record command output and candidate SHA/tree in the independent review record.

No check in this slice is evidence of production deployment, external provider access, durable persistence, or Gate closure.
