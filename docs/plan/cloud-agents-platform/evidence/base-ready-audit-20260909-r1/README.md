# BASE-READY / BASE-ADMIN-V1 current-source audit (2026-09-09)

This is a requirement-by-requirement closure audit, not a new runtime run or a
Gate closure. It uses branch `codex/cloud-agents-platform-p0` at
`5616e5408d0221eed40db4e60dc58da848a5bf2f`. The unrelated dirty paths listed
in [06](../../06-status-tracker.md) were preserved and were not used as passing
evidence.

A local, unpublished `0.3.0-dev.100` candidate was built from that HEAD with
`--allow-dirty` at
`/tmp/cloud-agents-base-ready-external-20260909-r1/release`. Its manifest marks
`sourceDirty=true`; all 22 artifact checksums passed. The manifest SHA-256 is
`ceaa97782afc353d73a844f42572795b9adfe3029fe802acdd82d86892db2590`,
the deployment tar SHA-256 is
`4fa38cb96d825d1276ff939561771c82b2b9ee5d9981b128ecd96d57c8b93cb4`,
and the Linux amd64 RemoteWorker SHA-256 is
`5d6e48637c77e77750ea59d444ed1c2cefed7da7f12c10c5b2a447819ab6f56e`.
This prepares the external run but is not deployment evidence.

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
| 6 | BASE-M3 enrollment through reconnect evidence; `base-m5-remote-worker-bootstrap-20260908-r5/evidence.json` | Proven on an isolated outbound Debian node, but not yet on either available external Linux host |
| 7 | `base-m5-fault-soak-docker-20260909-r2/evidence.json`; `base-m5-kubernetes-fault-soak-20260909-r1/evidence.json`; `base-m5-remote-worker-gateway-soak-docker-20260909-r2/evidence.json` | Docker and Kubernetes are proven; the customer-node run is real and isolated but remains local rather than an external host |
| 8 | `base-m2-sandbox-network-policy-20260906.md`; `base-m4-strong-isolation-gvisor-20260908/evidence.json`; current OIDC/RLS/RBAC evidence | Proven for the stated local Docker/Kubernetes matrix |
| 9 | Docker Snapshot/Restore/Retention evidence; `base-m5-helm-upgrade-rollback-20260908-r4/evidence.json`; certificate rotation, Compose backup/restore, no-Agent deployment and fault evidence | Proven within the recorded local deployment and recovery boundaries |
| 10 | Docker allocation/volume/network usage evidence; `base-m5-sandbox-usage-correction-docker-20260909-r5/evidence.json`; current fault/soak evidence and runbooks | Proven for the currently declared non-billing facts and measured local recovery boundary |
| 11 | `base-m5-admin-acceptance-docker-20260909-r7/evidence.json`; `admin-m4-oidc-audience-boundary-20260909-r1/evidence.json` | Proven for live Admin authority, content isolation, dangerous actions and ordinary-user denial |
| 12 | BASE-ADMIN-V1 audit below | Not closable while BASE-ADMIN-V1 item 10 remains open on an external customer node |

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
| 10 | Current Docker, OrbStack Kubernetes and local isolated outbound RemoteWorker evidence | Open: the Goal requires a real customer node; neither available external Linux host has run the workload yet |
| 11 | `base-m5-admin-acceptance-docker-20260909-r7/evidence.json` | Proven for keyboard, focus, contrast, error recovery and reduced motion |
| 12 | Same Admin acceptance evidence and fixed Daytona `v0.190.0` captures | Proven for the fixed shell/list/detail/form/state visual matrix |
| 13 | Same Admin acceptance evidence | Proven for zh-CN/en-US switching, persistence/fallback and both theme/viewport matrices |

## Current external preflight and next action

Read-only SSH preflight found that `hostdzire-4c6g` and `tianliyun-2c2g` are
Debian x86_64 hosts running as uid 0 with systemd, apt and sufficient free disk.
Neither has Docker or Podman. The local machine exposes only the `default` and
`orbstack` Kubernetes contexts and no `CLOUD_AGENTS_*` acceptance inputs.

The smallest remaining acceptance run is therefore:

1. deploy a temporary package-local Control Plane/PostgreSQL endpoint on one
   explicitly authorized host;
2. install Docker/OpenSandbox plus the packaged outbound RemoteWorker on the
   other explicitly authorized host;
3. run no-Agent Profile publication, Workspace/Sandbox create, Exec/Files,
   Stop/Rebuild data readback, disconnect/reconnect, Drain/Resume and exact
   test-owned cleanup through the Admin/User APIs;
4. record versions, digests, commands, observed recovery and final residue.

Installing Docker, starting services, changing reachable ports and deleting
the resulting test resources are external mutations. They were not performed
without explicit host and cleanup authorization.
