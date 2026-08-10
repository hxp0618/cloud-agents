# Gate closure record: `<GATE-ID>`

- Evidence ID：
- Record type：`PHASE | AGGREGATE`
- Phase / aggregate Gate：
- Prerequisite record IDs：
- Status：`NOT STARTED | IN PROGRESS | FAILED | VERIFIED | INVALIDATED`
- DRI：
- Independent reviewer：
- Date（UTC / Asia/Shanghai）：

## Fixed inputs

- `cloud-agents` commit / dirty state：
- Synara commit / dirty state：
- T3Code commit / dirty state：
- Contract/module/image/chart/platform manifest digest：
- Reference-host/T3 HostWorkloadDescriptor digest：
- Host producer signature/trust identity + image/bundle digest：
- SBOM/provenance/signature/VEX/vulnerability report digest：
- Scanner/version/vulnerability DB timestamp/base-image digest：
- Waiver ID/digest/owner/expiry（如有）：
- Go/Node/Bun/pnpm/Postgres/Kubernetes/Provider CLI/SDK versions：
- Deployment profile：

## Exit criteria mapping

| Criterion     | Result                | Evidence             |
| ------------- | --------------------- | -------------------- |
| `<criterion>` | `<PASS/FAIL/NOT RUN>` | `<log/job/artifact>` |

## Commands / CI

只记录可重放命令或 immutable CI job；所有输出必须先脱敏。

## Failures, retries, and waivers

- Failure：
- Root cause：
- Rerun：
- Waiver（如有，含批准人与期限）：

## Rollback / cleanup evidence

- Rollback action：
- Active aggregate writer/drain state：
- Endpoint/grant/workload/volume cleanup：

## Invalidation

- Downstream record IDs：
- Inputs that invalidate this record：
- Superseded record（如有）：

## Sign-off

- DRI conclusion：
- Reviewer conclusion：
- Closure decision：
