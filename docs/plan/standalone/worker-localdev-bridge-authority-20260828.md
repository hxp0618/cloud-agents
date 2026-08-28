# D-057 Worker localdev bridge authority

Status: proposed/implemented on the bounded localdev branch only. This authority does not authorize production database writes, public HTTP/P2/provider effects, deployment, publication, or Gate transitions.

## Frozen identity and lineage

- Authority: `D-057-WORKER-LOCALDEV-BRIDGE-000001`
- Revision: `D-057-WORKER-LOCALDEV-BRIDGE-000001.r1`
- Generated profile: `cloud-agents/worker-supervisor-operation-dispatch/launcher-localdev-v1alpha1`
- Single predecessor: D-056 Worker localdev launcher authority/revision/profile (`D-056-WORKER-LOCALDEV-LAUNCHER-000001`, `.r1`, `cloud-agents/worker-localdev-launcher/v1alpha1`), referenced by exact generated profile digest `sha256:dc83b89cad24104093e86e69d14743ca9bbc1b106113c90ca52bfa9bde04b72e` in `profile.json`.
- Historical references are retained and immutable: D-055, D-054, D-053-MIG-000014.r2, and D-053-EC-2.r3.
- Lineage is append-only; source/profile/schema/manifest bytes are never rewritten after review. Any P0/P1 repair is one repair and re-review within this r1 candidate; P2 findings are recorded and deferred. No r2/r3 is created for this slice.

增并独立复核一个明确的版本化 authority，冻结 source/schema、完整输入/排除集合、archive/member-manifest 算法、runner/toolchain/platform、receipt 路径、lineage fence 和 review 规则。:codex-annotation{index="1"}

## Contract boundary

The bridge is a supervisor-side localdev client for the existing Worker v1alpha1 Connect contract. It accepts only a fixed generated profile and an endpoint base `http://<IPv4|IPv6-loopback>:<1..65535>`; only the generated `/healthz` GET, Negotiate POST, and CheckHealth POST routes are frozen. Userinfo, query, fragment, proxy, redirect, foreign host/path, and caller-selected profile/identity are rejected. The health metadata must match the exact D-056 parent authority/revision/profile/digest before Negotiate or CheckHealth is accepted. Authentication consumes the regular non-symlink token file created by D-056 with `O_EXCL`, mode `0600`, and bounded bytes.

The worker and supervisor SPIFFE identities, lease ID, and generation are fixed generated constants. ExecuteOperation and GetOperationReceipt remain unimplemented/no-op at this bridge boundary; complete-ledger is `no_op`, and entry/recovery writers remain `NOT_IMPLEMENTED`. The receipt path is process-local only: `process-local://worker-supervisor/localdev-bridge`, with no durable persistence.

All database, durable receipt, provider, runtime, workspace, artifact, credential, production/public HTTP, P2, deployment, publication, and Gate effects are forbidden.

## Reproducibility inputs

The generator hashes a bytewise UTF-8 sorted list of regular, non-symlink files using `path\0mode\0size\0sha256\0`; it performs pre/post `lstat` and double-read identity checks. The exact input set is frozen in generated `authority-source.json`/`profile.json` (including `services/worker/supervisor/local_launcher.go` and `_test.go` once implemented). Exclusions are `.idea`, `deploy`, `helm`, `node_modules`, provider/runtime packages, control-plane migration/store paths, worker provider subtree, `release`, and `tmp`. Generated outputs are the two source/profile JSON files, their strict schemas, and `services/worker/localdev_bridge_profile_generated.go`; generated outputs and authority/review docs are not inputs.

Final generated evidence: 46 inputs, 12 exclusions, 5 generated outputs; source digest `sha256:764ab77a2e852f722c502795ce3ea8906f662786589bce9415e468ecc6060b01`, profile digest `sha256:56a992e653621a32327d6286f59ca35c79e6b9bc0ad9f1157d95179e9b41ff81`, input-manifest digest `sha256:0743e869a03bd29008fab23c0d0fbde2a44de8670cd734b5c6669cfaff254d16`.

Archive algorithm is `deterministic-ustar-v1` with no emission, fixed metadata (`mode=100644,uid=0,gid=0,mtime=0`), bytewise path ordering, duplicate rejection, and symlink rejection. Member manifest algorithm is `utf8-bytewise-sorted-path-mode-size-sha256-nul-v1`, also not emitted, with regular-file-only and duplicate rejection.

## Runner and evidence

The pinned runner is Go 1.26.6 (`GOWORK=off GOFLAGS=-mod=readonly go test -tags localdev ./...`) with Bun 1.3.14 and Node 24.18.1. Declared platforms are `darwin-arm64` and `linux-amd64`; checks are focused and read-only. Evidence is recorded in `worker-localdev-bridge-implementation-20260828.md` and the independent read-only review record. A passing focused check is not a production/runtime/Gate closure claim.

## Review rule

An independent reviewer must inspect a clean archive of the fixed candidate, verify exact SHA/tree/manifest/profile bytes, run focused generator/TypeScript/Go checks, and issue `APPROVE` or `REQUEST_CHANGES` with P0/P1/P2 classifications. The review record is append-only and may not mutate the candidate. No external side effect or Gate closure is authorized by this authority.
