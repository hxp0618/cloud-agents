# BASE-READY / BASE-ADMIN-V1 current-source audit (2026-09-09)

This is a requirement-by-requirement closure audit, not a formal Gate closure.
It uses branch `codex/cloud-agents-platform-p0` at
`ff715138f9b3443e01684c16e9ac08192b305e8d`. The unrelated dirty paths listed
in [06](../../06-status-tracker.md) were preserved and were not used as passing
evidence.

The final unpublished `0.3.0-dev.105` candidate was built from that HEAD with
`--allow-dirty` at `/tmp/cloud-agents-base-external-20260909-r2`; its manifest
marks `sourceDirty=true` and all 22 artifact checksums passed. The manifest
SHA-256 is
`fd7b34ce80e8e6b0e4e9ae16ffae9e9f6dd8e6bf41068b4859524f032c4bafe5`,
the deployment tar SHA-256 is
`c07b1c34ef89b6dbf4c6b8aa15b67bcccac002e61ae029f49fcded84a7717ef6`,
and the Linux amd64 RemoteWorker SHA-256 is
`a0961e838471c7b6caabc255a8c82b09af25e2fbc7adb394ccc88986e43b3a57`.
It was deployed in the authorized two-host
[external outbound customer-node run](../base-m5-external-remote-worker-20260909-r1/README.md).

The audit also exposed a packaging defect: the default Compose and Helm inputs
still required the legacy Coding Agent Worker and Provider/admission inputs.
That defect is closed by the subsequent
[default no-Agent deployment evidence](../base-m5-no-agent-deployment-20260909-r1/README.md):
the final unpublished `0.3.0-dev.103` candidate passed default no-Agent
Compose, opt-in compatibility regression, and a packaged Helm run that removed
the Worker and all five Worker/admission/Provider Secrets before restarting the
Control Plane. This does not change the external-node gap below.

Commands executed for this audit:

```sh
bun scripts/cloud-agents-platform-release.ts \
  --version 0.3.0-dev.100 \
  --output-dir /tmp/cloud-agents-base-ready-external-20260909-r1/release \
  --allow-dirty
(cd /tmp/cloud-agents-base-ready-external-20260909-r1/release && \
  shasum -a 256 -c checksums.sha256)
ssh -o BatchMode=yes -o ConnectTimeout=10 hostdzire-4c6g \
  'uname -s; uname -m; id -u; command -v systemctl; command -v apt-get; command -v docker || true; command -v podman || true; df -Pk / | tail -1'
ssh -o BatchMode=yes -o ConnectTimeout=10 tianliyun-2c2g \
  'uname -s; uname -m; id -u; command -v systemctl; command -v apt-get; command -v docker || true; command -v podman || true; df -Pk / | tail -1'
kubectl config current-context
kubectl config get-contexts -o name
```

The preflight queried only OS, architecture, uid, package/service tools, free
disk and Docker/Podman presence. It did not install packages, start services,
change firewall rules or write remote files.

## BASE-READY

| Item | Current evidence | Result |
| --- | --- | --- |
| 1 | `base-m1-controller-admin-20260906.md`; `base-m2-sandbox-exec-20260906.md`; PTY and Files evidence from the same BASE-M2 series; `base-m5-no-agent-deployment-20260909-r1/README.md` | Proven for the current API/SDK/CLI and default no-Agent Docker/Helm path |
| 2 | `base-m1-sandbox-lifecycle-20260906.md`; `base-m1-sandbox-ttl-20260906.md` | Proven for Stop, TTL, Rebuild and retained Workspace bytes |
| 3 | `base-m1-controller-admin-20260906.md`; `base-m5-fault-soak-docker-20260909-r2/evidence.json` | Proven for durable Operation recovery, adopt/compensation and no duplicate runtime |
| 4 | `base-m3-remote-worker-reconnect-20260907/evidence.json`; `base-m4-remote-worker-capacity-reservation-20260907-v2/evidence.json` | Proven for writer/generation fencing, stale command rejection and aggregate reservation |
| 5 | BASE-M2 PTY, Files, Preview, SSH and network-policy evidence; `base-m5-remote-worker-gateway-soak-docker-20260909-r2/evidence.json` | Proven for bounded access, negative isolation and Gateway recovery |
| 6 | BASE-M3 enrollment/reconnect evidence; `base-m5-remote-worker-bootstrap-20260908-r5/evidence.json`; [external customer-node run](../base-m5-external-remote-worker-20260909-r1/README.md) | Proven on an external Debian customer node for outbound enrollment, Drain, disconnect/offline retention, reconnect and reconciliation |
| 7 | `base-m5-fault-soak-docker-20260909-r2/evidence.json`; `base-m5-kubernetes-fault-soak-20260909-r1/evidence.json`; `base-m5-remote-worker-gateway-soak-docker-20260909-r2/evidence.json`; [external customer-node run](../base-m5-external-remote-worker-20260909-r1/README.md) | Proven for real Docker, Kubernetes and external outbound customer-node execution, recovery, capability enforcement and exact cleanup |
| 8 | `base-m2-sandbox-network-policy-20260906.md`; `base-m4-strong-isolation-gvisor-20260908/evidence.json`; current OIDC/RLS/RBAC evidence | Proven for the stated local Docker/Kubernetes matrix |
| 9 | Docker Snapshot/Restore/Retention evidence; `base-m5-helm-upgrade-rollback-20260908-r4/evidence.json`; certificate rotation, Compose backup/restore, no-Agent deployment and fault evidence | Proven within the recorded local deployment and recovery boundaries |
| 10 | Docker allocation/volume/network usage evidence; `base-m5-sandbox-usage-correction-docker-20260909-r5/evidence.json`; current fault/soak evidence and runbooks | Proven for the currently declared non-billing facts and measured local recovery boundary |
| 11 | `base-m5-admin-acceptance-docker-20260909-r7/evidence.json`; `admin-m4-oidc-audience-boundary-20260909-r1/evidence.json` | Proven for live Admin authority, content isolation, dangerous actions and ordinary-user denial |
| 12 | BASE-ADMIN-V1 audit below | Proven: all thirteen BASE-ADMIN-V1 items have current-source evidence |

## BASE-ADMIN-V1

| Item | Current evidence | Result |
| --- | --- | --- |
| 1 | `base-m5-user-admin-web-deployment-20260909-r1/evidence.json`; `base-m5-no-agent-deployment-20260909-r1/README.md` | Proven for independent default no-Agent Compose/Helm build and deployment, with legacy Worker only by explicit opt-in |
| 2 | `base-runtime-profile-api-20260905.md`; current User Sandbox/Environment APIs | Proven for user-safe published specifications and owned Workspace/Sandbox access |
| 3 | `base-m5-user-web-boundary-20260909-r1/evidence.json`; `admin-m4-user-api-boundary-20260909-r1/evidence.json` | Proven for page, request, storage and User API infrastructure-field removal |
| 4 | Kubernetes foundation, RemoteWorker Target and `base-m5-external-ssh-probe-soak-20260909-r2/evidence.json` | Proven for Admin Docker/Kubernetes/RemoteWorker management and legacy SSH register/Probe compatibility |
| 5 | `base-runtime-profile-api-20260905.md`; `base-m1-controller-admin-20260906.md` | Proven for executable no-Agent RuntimeProfile publication and Workspace/Sandbox creation |
| 6 | BASE-M3/M4 Admin evidence and `base-m5-admin-acceptance-docker-20260909-r7/evidence.json` | Proven for distinct node, Sandbox and legacy Lease views plus lifecycle operations |
| 7 | BASE-M1 lifecycle, BASE-M3 command and Admin acceptance evidence | Proven for generation/impact confirmation, Operation and Audit |
| 8 | BASE-M2 content-isolation evidence and Admin acceptance response scans | Proven: no user message, Workspace/Artifact content or Secret bytes are returned |
| 9 | `admin-m4-oidc-audience-boundary-20260909-r1/evidence.json` | Proven for server-side 403 and cross-audience 401 boundaries |
| 10 | Current Docker and OrbStack Kubernetes evidence; [external outbound customer-node run](../base-m5-external-remote-worker-20260909-r1/README.md) | Proven for real deployment, retained-data recovery and exact cleanup across Docker, Kubernetes and an external customer node |
| 11 | `base-m5-admin-acceptance-docker-20260909-r7/evidence.json` | Proven for keyboard, focus, contrast, error recovery and reduced motion |
| 12 | Same Admin acceptance evidence and fixed Daytona `v0.190.0` captures | Proven for the fixed shell/list/detail/form/state visual matrix |
| 13 | Same Admin acceptance evidence | Proven for zh-CN/en-US switching, persistence/fallback and both theme/viewport matrices |

## External customer-node closure

After explicit authorization, `hostdzire-4c6g` ran the packaged Control
Plane/PostgreSQL and `tianliyun-2c2g` ran fixed-digest OpenSandbox plus the
outbound RemoteWorker. The run covered Profile publication, Workspace/Sandbox
create, Exec, Files, PTY, short-lived SSH, TTL Stop, Rebuild data readback,
disconnect/offline retention, reconnect, Drain/Resume, Operation/Audit and
ordinary-user Admin 403. A create-retry defect found by the real run was fixed
at the shared command assembly point and candidate `.105` completed the flow.

The same physical Workspace volume retained a 31-byte file with SHA-256
`ee4c8236e71f9c271f5d24e0ca2363c162bd496ad5759002557029994237930c`
across Stop/Rebuild and node disconnect/reconnect. Final authorized cleanup
left zero test-owned containers, volumes, networks, images, listeners and run
directories on both hosts; Docker packages remain installed and enabled. Full
commands, versions, failure accounting, security scans and cleanup inventory
are in the
[external run evidence](../base-m5-external-remote-worker-20260909-r1/README.md).

The current-source evidence set now satisfies all twelve BASE-READY and all
thirteen BASE-ADMIN-V1 items. This audit does not approve a Release, Beta/GA or
formal historical Gate, and it does not change the archived ADMIN-WEB-V1 or
APP-M1 Codex/Claude acceptance state.
