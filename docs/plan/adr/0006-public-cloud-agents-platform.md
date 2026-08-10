# ADR-0006: Cloud Agents is a public deployable platform with a Go Control Plane

- Status: Accepted
- Date: 2026-08-10

## Context

The first extraction treated `hxp0618/cloud-agents` primarily as the source of seven portable Runtime packages while
the Synara Go Control Plane remained a host-owned implementation. The product goal is broader: Cloud Agents must be a
complete public platform that can be deployed without Synara private services, and both Synara and T3Code must consume
the same public APIs, SDKs, and immutable artifacts.

The existing Synara Control Plane contains the required product concepts, but it also contains Synara-specific
commercial, enterprise, compliance, and migration code. The current snapshot observes 994 Go files, but copying that
or any later frozen-ref manifest without classification would preserve the wrong dependencies and create two editable
implementations.

## Decision

1. `hxp0618/cloud-agents` will own Portable Runtime, public contracts and SDKs, the Go Control Plane, Worker and
   Supervisor, production data models, conformance, and direct deployment profiles.
2. The public Control Plane exposes two authority planes:
   - managed-agent: Tenant/Organization/Project, Session/Turn/Execution, Worker, Workspace, Artifact, and Credential lifecycle;
   - managed-host: CloudEnvironmentLease/Generation and a complete hosted T3 environment.
3. Embedded Runtime remains available without the Go service. T3 embedded keeps Thread/Turn/Workspace authority.
4. Synara native becomes a public managed-agent API/SDK consumer. T3 managed becomes a public managed-host API/SDK
   consumer. Neither host maintains a fork of the public Control Plane.
5. The public repository ships production Postgres schema/migrations/outbox/reconciliation, standard OIDC/basic RBAC,
   Worker/Supervisor, local/container/Kubernetes compute, filesystem/S3 storage, public adapter protocols, Compose, and
   Helm. A Synara private binary is not required to deploy it.
6. Synara enterprise identity, billing, compliance, private KMS/infra, and audit extensions integrate through public,
   versioned, out-of-process extension surfaces. They are not prerequisites for a standalone deployment.
7. Existing Go code is migrated capability by capability as move, rewrite-public, adapter, Synara-only, or retire.
   After cutover, public capabilities have only one editable implementation in `cloud-agents`.
8. Public Control Plane and Runtime use independent modules and release trains in the same repository. A platform
   manifest composes immutable digests without repacking them.
9. Synara and public CP never dual-write Session/Turn/Execution. Public CP and T3 never dual-write T3 Thread/Turn/
   Workspace. Active aggregates remain with their creating writer through drain and cleanup.
10. Source publication, package/module publication, image publication, deployment, beta, and GA remain separate gates.
11. Public CP is the management/admission-plane PEP and durable writer for PlatformTenant/PlatformOrganization/
    PlatformProject/Membership. Enterprise IdP/PDP input is one-way provisioning or a fail-closed constraint; it
    cannot directly write agent/lease authority. The leased T3 auth service is the data-plane PEP for that
    environment's HTTP/RPC/WebSocket traffic and must fail closed against public membership version, Lease generation,
    subject, scope, and revoke state. A signed authorization snapshot with a maximum 60-second TTL plus ingress fencing
    bounds cross-service revocation; a network partition never permits indefinite stale access.
12. Managed-host pairing link/token/session authority remains inside the leased T3 auth store. Public CP owns Lease
    generation/admission and stores only opaque pairing refs plus generation-bound receipts. A one-time pairing URL is
    an ephemeral `no-store` response, never Receipt/outbox/audit/log data; a lost response is revoked and reminted,
    never replayed.
13. Managed-host core is tested first with a public reference host. Real T3 integration consumes a signed,
    allowlisted HostWorkloadDescriptor and image/bundle digest produced by the T3 repository.
14. External consumers only rely on contracts/generated SDKs and service APIs. Go server domain/service internals are
    not a second in-process integration ABI.

## Alternatives

### Keep the Go Control Plane only in Synara

Rejected. Cloud Agents would not be independently deployable, and other consumers would depend on Synara private
authority.

### Copy the complete Synara repository subtree as-is

Rejected. It would publish unrelated product authority and retain tight internal dependencies instead of producing a
maintainable public platform.

### Require T3 embedded to use the public Control Plane

Rejected. It would remove the lightweight local profile and create a second Turn/Workspace authority.

### Build a Synara-only composition binary

Rejected as the target. The public platform itself must be directly deployable. Synara-specific behavior uses public
out-of-process adapters or APIs.

## Consequences

- The public scope is larger than the seven-package M1 extraction and needs independent Go/platform/security/operations
  ownership.
- Public contracts must cover managed agent, managed host, worker, and platform adapter surfaces.
- Synara will eventually delete or retire duplicate Cloud Agent Control Plane implementations after single-writer
  cutover.
- T3 keeps its embedded bridge and gains a managed-host client; it does not import Go internals.
- Standalone Compose/Helm real-provider E2E becomes a release requirement.
- Existing M1 artifacts remain immutable historical evidence and are not silently upgraded by this decision.

## Status boundary

This ADR was accepted by the user on 2026-08-10. Platform P0 is verified by independently reviewed inventory and
baseline phase records, so P1 entry is satisfied. P1 may implement public contracts, generated TS/Go SDKs, foundation
data/authority/security code, source modules, and local ephemeral database tests under
[`ADR-0007`](0007-p1-contract-data-toolchain-foundation.md)；M1 and P2–P6 implementation, package/module/image
publication, deployment, production database migration and real-provider work remain paused until their own phase
entry criteria are satisfied.
