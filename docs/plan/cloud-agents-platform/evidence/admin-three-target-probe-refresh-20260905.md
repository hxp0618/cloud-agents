# Current Admin three-transport Probe and denied-write Audit — 2026-09-05

Source base: `3d8c49c9`, branch `codex/cloud-agents-platform-p0`. Refreshes the [earlier actual transport check](admin-three-target-probes-20260905.md) after the shared Admin denied-write middleware and migrations through 000052 were added. This is backend/API/SDK acceptance, not Worker or Provider qualification.

## Actual results

The generated SDK registered three new Targets in new project `project-bd527365fd092311b6b239c7b9fff538`, tenant `tenant-local`, against a freshly built local Control Plane and real PostgreSQL database.

| Transport | Persisted phase | Engine |
| --- | --- | --- |
| OrbStack Docker via ephemeral mTLS | ready | 29.4.0 |
| OrbStack Kubernetes HTTPS API | ready | v1.35.6+orb1 |
| Actual OpenSSH in isolated Alpine container | ready | OpenSSH_10.0 |

- Registration and Probe each replayed with the original idempotency key; responses matched, and exactly one successful Operation per action remained with correlated success Audit.
- Ordinary-user register, Probe, get, Operation-list and Audit-list requests returned 403 for all three kinds: 15 actual failures.
- The existing harness now checks every rejected registration/Probe by its request ID through `listAdminDeniedWriteEvents`. Six events had the correct action, denied result, stable error code and hashed actor; rejected Probe events identified the Target, without fabricating an executed Operation or resource generation.
- A second run against the same IDs failed in the existing-resource preflight before any write. Its partial evidence remained `complete: false`.
- [Machine-readable evidence](admin-three-target-probe-refresh-20260905.json) retains results, successful Operation/Audit and six denial events. No secret material is included.
- Strict TypeScript with ES2023 target, scoped lint/format and diff checks passed. An initial ES2022 check was incompatible with the existing SDK's `toSorted`; rerunning with the required newer library passed without changing SDK code.

## Transport provenance and scope

Reused the earlier temporary setup with new paths and resource names after confirming both namespace and container absent. Setup and non-secret logs remain under `/tmp/cloud-agents-admin-probe-refresh.NLKpD3/`.

- Docker context explicitly `orbstack`. The authenticated loopback gateway forwarded only `GET /_ping` and `GET /version` to `/Users/huang/.orbstack/run/docker.sock`. Its final log recorded exactly those two 200 responses. Independent Docker version agreed. No response was fabricated and no Docker write was forwarded.
- Kubernetes context explicitly `orbstack`, kubeconfig `/Users/huang/.kube/orbstack-config.yaml`. New namespace `ca-admin-probe-refresh-nlkpd3` and ServiceAccount supplied a short-lived token for actual `/version` discovery. No workload permission grant, Pod or PVC was created.
- New container `ca-admin-probe-refresh-nlkpd3` ran actual Alpine OpenSSH with generated authentication key and separately pinned host key. Server logs confirmed public-key authentication. ForceCommand restricted execution to real OS/architecture discovery, with no host workspace or Docker socket mounted. Upstream `UsePAM` warnings remained; this is not a warning-free SSH server claim.

The check validates discovery and authentication only. It does not prove Docker write authority, Kubernetes workload permissions, SSH Worker deployment, policy enforcement, Workspace lifecycle or Codex/Claude Turn execution.

## Cleanup and next acceptance

Stopped gateway PID 62699 and owned dev-stack parent PID 63003; removed only the new SSH container and namespace. Independently verified both named containers, namespace, dev-state directory and ports 57964/33219/18085/18095 absent. No Pod/PVC existed in the test namespace before deletion. Disposable database state was removed; durable metadata evidence remains committed.

Generated credentials moved into private `/Users/huang/.Trash/cloud-agents-probe-refresh.s1fwJ4/` for recoverability; the ServiceAccount was deleted and all credential-accepting endpoints closed. Existing workloads, base images and unrelated dirty/staged files were preserved. No push, image publication or Release.

Current ADMIN-M1 backend register→Probe-ready acceptance is refreshed. The earliest incomplete item is full current Daytona visual/interaction acceptance of the Admin surface. Later Worker/Profile/Provider E2E requirements remain unqualified by this check. No new credential or existing-resource destructive approval was needed.
