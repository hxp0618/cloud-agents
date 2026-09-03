# M1 Admin Target Matrix E2E

Date: 2026-09-03

Status: passed for the real Kubernetes and SSH registration/Probe matrix slice. Together with `M1-TARGET-ORBSTACK-E2E.md`, this covers all three Target kinds, but does not complete the Section 15 acceptance matrix.

## Build under test

- Branch: `codex/cloud-agents-platform-p0`, starting HEAD `d4d2a6c`.
- Admin Web: independent Vite application on `127.0.0.1:4174`, proxying `/v1/admin` to Control Plane on `127.0.0.1:18081`.
- Database: fresh PostgreSQL created by `scripts/cloud-agents-dev.sh` with product schema head `000040`.
- Kubernetes authority: the live OrbStack cluster at `https://k8s.orb.local:26443`, node `orbstack`, Kubernetes `v1.35.6+orb1`, `linux/arm64`.
- SSH authority: the live OrbStack SSH service at `ssh://127.0.0.1:32222`, pinned to its current ED25519 host key; Probe executed `uname -s && uname -m` and observed `linux/arm64`.
- Credentials were short-lived or copied into one isolated Control Plane credential directory. No credential bytes were entered into Admin Web or returned by Admin API.

## Root-cause repair

The first SSH Probe correctly rejected an invalid host-key fixture. After the exact current ED25519 key was supplied, the client still returned `ssh-host-key-mismatch`: `x/crypto/ssh` preferred the server's ECDSA key because the client had not constrained host-key negotiation to the pinned key type.

`sshtarget.connect` now sets `HostKeyAlgorithms` to the pinned public key's type before the existing byte-for-byte callback check. The shared path covers Probe, deploy, upgrade, inventory, and Cleanup. The test SSH server now exposes both ECDSA and ED25519 host keys, so the regression fails without this constraint while the existing wrong-key rejection remains covered.

## Live authority and authorization

- Tenant: `tenant-local`.
- Project: `project-16288c20f172f686a2af71b762ff4350`.
- An ordinary user token received HTTP `403` with stable code `AUTHORIZATION_DENIED` from the Admin Target list before either Target was registered.
- `kubernetes-orbstack-e2e` registered with HTTP `201`, generation `1`, resource version `1`, then Probe returned HTTP `200`, `ready`, API `1.35`, engine `v1.35.6+orb1`, `linux/arm64`, resource version `3`.
- `ssh-orbstack-e2e` registered with HTTP `201`, generation `1`, resource version `1`, then Probe returned HTTP `200`, `ready`, SSH API `2.0`, engine `OrbStack`, `linux/arm64`, resource version `3`.
- Kubernetes persisted succeeded Probe Operation `op-ff3cffdc10608a9efc71b295e8590bda` and requested/succeeded Audit events. SSH persisted succeeded Probe Operation `op-c134469581caad975f9462a343367a77` and requested/succeeded Audit events. Both also retained their succeeded registration Operation/Audit records.
- A response scan found no private-key marker, bearer, kubeconfig, ServiceAccount token, Provider credential reference, or secret bytes.

## Browser verification

- Real Chromium and the Codex in-app browser rendered the two PostgreSQL-backed Targets as `ready`; no static Target fixture was used.
- Kubernetes detail rendered API `1.35`, engine `v1.35.6+orb1`, generation/resource version, Operations, and Audit. A browser Probe advanced resource version to `5` and added succeeded Operation `op-23b02130fba7cdcbd545090194facda3`.
- SSH detail rendered API `2.0`, engine `OrbStack`, `linux/arm64`, generation/resource version, Operations, and Audit. Browser Probes advanced resource version to `7`; the captured Chromium run added succeeded Operation `op-27216114560cec5efbc4c58395dc15e5` and two Audit events.
- Chromium DevTools captured only the one-shot loopback token test bridge and same-origin `/v1/admin/...` Target, Operation, and Audit requests. There was no browser request to Kubernetes port `26443`, SSH port `32222`, or either authority protocol.
- Session storage contained only endpoint, tenant ID, and project ID. Local storage contained only the dark-theme preference. Storage scans found no bearer/JWT, `credentialRef`, `providerCredentialRef`, private key, kubeconfig, `26443`, or `32222`.
- Browser console warning/error lists were empty.
- The real SSH detail was visually checked at `1440x900` desktop and `390x844` mobile in dark mode against the checked-in Daytona `v0.190.0` reference and Admin actual baselines. The fixed Sheet geometry, hierarchy, spacing, button, badge, and responsive full-width behavior were preserved; the additional Operation/Audit cards are Cloud Agents resource content, not a shell deviation.

## Source verification

```text
go test ./services/control-plane/internal/sshtarget ./services/control-plane/internal/kubernetestarget ./services/control-plane/internal/deploymenttarget ./services/control-plane/internal/server PASS
go test ./services/control-plane/cmd/cloud-agents-control-plane                 PASS
go test -tags=localdev ./services/control-plane/cmd/cloud-agents-control-plane  PASS
bun --filter @cloud-agents/admin-web test                                       PASS, 5 tests
bun --filter @cloud-agents/admin-web build                                      PASS
bun scripts/generate-platform-json-sdks.ts --check                              PASS
gofmt -d affected Go files                                                      PASS
git diff --check                                                                PASS
slice secret scan                                                               PASS
```

## Teardown and evidence boundary

- Target registration and Probe created no Worker, Pod, container, volume, or Workspace resource on either authority.
- Admin Web, Control Plane, Worker, PostgreSQL, both one-shot token bridges, and the test Chromium page were stopped. Ports `18081`, `18082`, `18091`, and `4174` had no listeners, and no `cloud-agents-dev-*` PostgreSQL container remained.
- The dev state directory was removed by the development script. The isolated credential directory was moved recoverably to `/Users/huang/.Trash/cloud-agents-m1-target-probe-20260903-uVDIR0` and the system clipboard was cleared.
- The existing OrbStack cluster, SSH service, and OrbStack machines were only read and remain untouched.
- This proves real registration, Probe readiness, server authorization, browser routing, Operation/Audit feedback, and responsive Target detail for Kubernetes and SSH. It does not prove Profile publication, Worker deployment/lifecycle, Kubernetes/SSH Cleanup, or the M4 Provider E2E matrix.
