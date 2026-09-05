# Real Admin API three-target Probe acceptance — 2026-09-05

Source base `115088cf3f46c15442d98584af6606f3375ced84`, same
`codex/cloud-agents-platform-p0` checkout. Previous turn was progress. This slice advances
ADMIN-M1's real backend acceptance; it does not mark M1 or the Goal complete.

## Observed result

| Actual transport                         | Persisted state | API / engine        | Generation / resource version |
| ---------------------------------------- | --------------- | ------------------- | ----------------------------- |
| OrbStack Docker over ephemeral mTLS      | ready           | 1.54 / 29.4.0       | 1 / 3                         |
| OrbStack Kubernetes HTTPS API            | ready           | 1.35 / v1.35.6+orb1 | 1 / 3                         |
| OpenSSH server in a new Alpine container | ready           | 2.0 / OpenSSH_10.0  | 1 / 3                         |

All reported linux/arm64. The [machine-readable evidence](admin-three-target-probes-20260905.json)
contains the real registration/Probe Operation and Audit records and source base.

- Generated TypeScript SDK registered all three Targets through Admin API, probed them, then
  re-read the persisted state. Both registration and Probe were replayed with their original
  idempotency keys; responses matched and each target retained exactly one successful
  registration Operation and one successful Probe Operation, with corresponding success Audit.
- Ordinary user Token was denied with 403 for register, Probe, get, Operation list and Audit
  list for each kind: 15 actual authorization failures.
- A second invocation against the now-existing Target IDs failed during preflight, before any
  writes. The harness deliberately refuses to operate on pre-existing resources.
- The new runnable check is `scripts/test-admin-target-probes.ts`. It validates all target
  request bodies with the generated SDK, checks all targets absent before writing, refuses an
  existing output directory, and stores partial progress without falsely marking failed runs complete.
- Script strict TypeScript check, scoped formatting/lint, diff checks, and 26 generated Platform
  SDK tests passed. No contract, backend, SDK implementation or Web source change was needed.

## Why these were real transports

The Control Plane's existing server-side credential-directory configuration was used with a
fresh `scripts/cloud-agents-dev.sh` PostgreSQL/Worker/Control Plane stack at 18085/18095.
Project: `project-69d4fcbce63963e394ed8a790eb014e3`, tenant `tenant-local`.

- Docker context was explicitly `orbstack`. A loopback HTTPS server authenticated an ephemeral
  client certificate and forwarded only `GET /_ping` and `GET /version` to the real OrbStack
  Unix socket. No response body was fabricated. Its final request log contains exactly those
  two 200 responses; idempotent Probe replay caused no second transport call. The independent
  `docker --context orbstack version` also returned 29.4.0. All other gateway paths/methods were denied.
- Kubernetes context and kubeconfig were explicitly `orbstack` and
  `/Users/huang/.kube/orbstack-config.yaml`. A new namespace
  `ca-admin-probe-20260905-ysuzjv` and `probe` ServiceAccount supplied a 30-minute token and cluster
  CA. Control Plane connected to the real `https://k8s.orb.local:26443/version`. No ClusterRole,
  RoleBinding, Pod or PVC was created. This tests authenticated discovery, not workload permissions.
- SSH endpoint was a new loopback-published `alpine:3.22` container with the actual packaged
  OpenSSH server. Control Plane authenticated with a generated key and a separately pinned host
  public key. Server logs confirmed accepted public-key authentication. Password/interactive
  authentication, forwarding and TTY were disabled; ForceCommand limited execution to real
  `uname -s && uname -m`. Only the server host key, authorized public key and sshd configuration
  were mounted read-only; no Docker socket, Provider credential or host workspace was mounted.
  Alpine's OpenSSH logged an unsupported `UsePAM` option warning but started and authenticated;
  this is not a warning-free server claim.

No pre-existing workload or infrastructure resource was modified. These protocol services do
not qualify SSH Worker deployment, Docker write authority, Kubernetes namespace workload
permissions, storage/network enforcement, or either Provider's execution.

## Reproduce

1. Prepare three real reachable endpoints and server-side credential files using the existing
   `dockertarget`, `kubernetestarget` and `sshtarget` directory contracts. Keep secret files outside
   the repository and browser; use a new project and new Target IDs.
2. Start the development stack with the three existing environment variables:
   `CLOUD_AGENTS_PLATFORM_DOCKER_CREDENTIALS_DIRECTORY`,
   `CLOUD_AGENTS_PLATFORM_KUBERNETES_CREDENTIALS_DIRECTORY`,
   `CLOUD_AGENTS_PLATFORM_SSH_CREDENTIALS_DIRECTORY`.
3. Prepare a JSON array of exactly three `DeploymentTargetRegisterRequest` values, one per kind,
   containing IDs, names, actual endpoints and credential references only. The generated encoder
   checks the request shape; actual protocol behavior is verified by the running Control Plane.
4. Run:

   ```sh
   bun scripts/test-admin-target-probes.ts \
     http://127.0.0.1:18085 ADMIN_TOKEN_FILE USER_TOKEN_FILE \
     tenant-local NEW_PROJECT_ID TARGETS_JSON NEW_OUTPUT_DIRECTORY
   ```

5. Independently verify the supplied transports are real. The check does not magically identify
   mocked servers; this run's upstream provenance is described above. Inspect the recorded facts,
   Operation/Audit records and live infrastructure versions, then tear down only this run's resources.

The temporary setup code and non-secret logs are retained under
`/tmp/cloud-agents-admin-probes.ysuzJv/`; they may expire. No persistent environment, provider
credentials or published image is delivered by this probe-only check.

## Cleanup and remaining acceptance

The self-owned SSH container, local dev PostgreSQL/Control Plane/Worker, Docker gateway and new
Kubernetes namespace/ServiceAccounts were removed/stopped. Final checks found no matching
containers or namespace, no owned test processes, and no dev state directory. The namespace
had no Pod/PVC. Generated test credentials were moved to a private Trash directory (recoverable),
not left active in the test root; the ServiceAccount token lost its account and all endpoints closed.
Base Docker images and unrelated resources were preserved. No push, Release or image publication.

ADMIN-M1 still requires full current Daytona visual/interaction comparison and browser-level
acceptance of the complete surface; this run supplies backend/API/SDK Probe evidence, not new
UI screenshots. M2/M3 policy/profile runtime qualification and M4 actual Docker/Kubernetes/SSH
Worker lifecycles, Codex/Claude Sessions/Turns and zero-workspace-residue acceptance remain.
No additional credential or destructive-operation permission is currently needed for the next
in-scope UI slice.
