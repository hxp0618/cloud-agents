# Independent Cloud Agents Compose deployment

This runbook describes the existing Agent/Lease deployment and its actual bootstrap commands. The [foundation-first delivery plan](../../docs/plan/cloud-agents-platform/04-extraction-and-migration.md) additionally requires a no-Agent installation with independent Workspace persistence, Sandbox access and customer-node support; that target is not implemented by merely following this runbook. Do not silently change old Lease cleanup/volume semantics or imply that a successful stack startup proves BASE-READY. Deployment and production database authorization remain separate from editing this document.

Extract the single `cloud-agents-deployment-<schema-head>.tar` from the release into a directory and copy
`deploy/compose/.env.example` to a deployment-owned env file. Set
`CLOUD_AGENTS_DEPLOY_DIR` to the extracted directory's `deploy` path.

The bootstrap profile provisions the fixed Compose database roles in one
transaction and fails closed on existing role drift. Start the complete stack
from any directory with:

```sh
sh /path/to/extracted/deploy/compose/cloud-agents-up.sh /path/to/.env
```

The script performs these existing steps in order:

1. Run `docker compose --env-file .env --profile bootstrap run --rm bootstrap`
   once with an isolated unswitched superuser URL.
2. Run `docker compose --env-file .env --profile tenant-bootstrap run --rm tenant-bootstrap`
   once to migrate the database and create the initial tenant, organization, and `tenant.admin`
   membership for the configured authenticated subject. Exact retries are safe; conflicting
   retries fail without partial changes.
3. Run `docker compose --env-file .env up --build` and remain attached.

Open `http://127.0.0.1:4173` for User Web and `http://127.0.0.1:4174` for Admin
Web. Each browser talks only to its own origin: User Web forwards non-Admin `/v1`
routes, while Admin Web forwards only `/v1/admin`. Set
`CLOUD_AGENTS_CONTROL_PLANE_CA` to the CA that issued the internal Control Plane
certificate. Both containers run as uid `1000` with read-only root filesystems
and receive no Docker socket, target credentials, or Provider credentials. Keep
the default loopback binds for local operation; public deployments must put
deployment-owned TLS/OIDC ingress in front of each service.

Create a consistent custom-format logical backup without writing it inside a
container:

```sh
umask 077
docker compose --env-file .env --profile backup run --rm -T backup > cloud-agents.dump.tmp &&
  mv cloud-agents.dump.tmp cloud-agents.dump
```

The backup profile uses the isolated install-admin URL because forced tenant RLS
must not omit rows from an all-tenant backup. Do not substitute the runtime or
migration URL.

Restore only into a freshly bootstrapped database that does not yet contain the
`cloud_agents` schema, then start the normal stack. Restore is one transaction;
an invalid dump leaves the target empty, and a repeated restore is rejected:

```sh
docker compose --env-file .env --profile restore run --rm -T restore < cloud-agents.dump
docker compose --env-file .env up --build
```

The migration URL must be able to `SET ROLE cloud_agents_migration_owner`; the
runtime URL uses the least-privileged `cloud_agents_runtime_login`. Use the
tenant-bootstrap URL only for the packaged one-shot bootstrap. TLS,
JWK trust configuration, Runtime provider environment, SPIFFE identity, and
Runtime admission values are deployment-owned inputs and are never generated
by this package. Copy `runtime.env.example` for the non-secret Provider Host
settings. Keep credentials outside the archives in the referenced directory,
using the Runtime's existing anonymous-FD envelopes. Files are tenant-bound:
`<tenantId>.codex.json` contains `{"payload":{"apiKey":"..."}}`, while
`<tenantId>.claudeAgent.json` contains exactly one of `apiKey` or `authToken`
under `payload`. Optional `baseUrl` values and Codex
`organization` use the same payload object. The Worker binds one file to the
requested Provider before starting each Runtime process; provider keys in the
runtime env file are ignored. `baseURL` is accepted as an alias for `baseUrl`,
and an optional credential `model` is used only when the execution request has
no explicit model. Claude credentials may use the same `/v1` endpoint form as
Codex; the Claude provider removes that suffix before the SDK adds its API path.

Create `CLOUD_AGENTS_ACCESS_GRANT_KEY_FILE` as 32–64 random bytes with mode
`0600`. The no-network `access-grant-key` one-shot copies it into a dedicated
Compose volume as uid `65532` with mode `0400`; the Control Plane mounts only
that volume read-only. To rotate it, replace the deployment-owned source file,
then recreate `access-grant-key` and `control-plane` together. Existing grants
expire normally and new grants use the replacement key.

Create a deployment-owned TLS certificate/key pair in
`CLOUD_AGENTS_ACCESS_GATEWAY_TLS_DIR` and an OpenSSH private host key at
`CLOUD_AGENTS_ACCESS_GATEWAY_SSH_HOST_KEY` with mode `0600`. The no-network
`access-gateway-ssh-key` one-shot copies the SSH key into a dedicated Compose
volume as uid `65532` with mode `0400`. The Gateway runs as non-root with a
read-only root filesystem, no Linux capabilities and no Docker socket. Keep the
HTTP and SSH bind addresses loopback-only unless a deployment-owned ingress
terminates and authenticates public access.

The Worker accepts at most `CLOUD_AGENTS_RUNTIME_MAX_SESSIONS` concurrent Runtime
sessions (default `4`). Additional session opens fail immediately with
`ResourceExhausted`; tune the value to the CPU and memory assigned to the Worker.

Foundation Sandbox access also requires a deployment-owned OpenSandbox descriptor
with mode `0400`: Docker targets use `<credentialRef>/opensandbox.json` and
Kubernetes targets use `<credentialRef>.opensandbox.json` in their respective
credential directory. The file contains only `endpoint` and `apiKey`; it is
mounted into the Control Plane and Gateway, never copied into an API body,
browser, or release archive.

For an outbound customer node, create one RemoteWorker enrollment in Admin Web
and deliver a separately scoped bootstrap token file to the node operator. From
the same extracted release directory, run the packaged bootstrap script into a
new deployment-owned directory:

```sh
CLOUD_AGENTS_PLATFORM_RELEASE_DIR=/media/cloud-agents-release \
CLOUD_AGENTS_REMOTE_WORKER_INSTALL_DIR=/srv/cloud-agents-remote-worker \
CLOUD_AGENTS_REMOTE_WORKER_CONTROL_PLANE_URL=https://control-plane.example \
CLOUD_AGENTS_REMOTE_WORKER_SERVER_CA_FILE=/secure/control-plane-ca.pem \
CLOUD_AGENTS_REMOTE_WORKER_BOOTSTRAP_TOKEN_FILE=/secure/bootstrap-token \
CLOUD_AGENTS_REMOTE_WORKER_TENANT=tenant-a \
CLOUD_AGENTS_REMOTE_WORKER_PROJECT=project-a \
CLOUD_AGENTS_REMOTE_WORKER_ENROLLMENT=enrollment-a \
CLOUD_AGENTS_REMOTE_WORKER_INCARNATION=node-a-1 \
CLOUD_AGENTS_REMOTE_WORKER_CAPABILITIES=docker,exec,files,network-dns-nft,preview,pty,ssh,workspace-volume \
CLOUD_AGENTS_REMOTE_WORKER_CAPACITY_CPU_MILLIS=4000 \
CLOUD_AGENTS_REMOTE_WORKER_CAPACITY_MEMORY_BYTES=8589934592 \
CLOUD_AGENTS_REMOTE_WORKER_CAPACITY_DISK_BYTES=42949672960 \
CLOUD_AGENTS_REMOTE_WORKER_DOCKER_ENDPOINT=https://127.0.0.1:2376 \
CLOUD_AGENTS_REMOTE_WORKER_CREDENTIAL_DIRECTORY=/secure/remote-worker-runtime \
CLOUD_AGENTS_REMOTE_WORKER_CREDENTIAL_REF=opensandbox \
  sh scripts/bootstrap-platform-remote-worker.sh
```

Verify the release `checksums.sha256` before bootstrap. The script selects the
matching Linux binary from the release,
claims the enrollment Secret through the bootstrap-only API, generates the key
and CSR locally, and removes the Secret after certificate issuance. It refuses
an existing install directory and never prints Secret or key bytes. If issuance
fails after the one-time claim, the mode-`0600` Secret remains in the reported
staging directory so the operator can retry rather than lose the enrollment.
If certificate issuance succeeds but local installation finalization fails, the
script removes the one-time Secret and retains the issued identity in the
reported staging directory.
Run `/srv/cloud-agents-remote-worker/run.sh` under the node's existing service
supervisor. The process opens no listener and connects only outbound; Docker and
runtime credentials remain node-local. The process rotates its 15-minute mTLS
identity five minutes before expiry, atomically replaces each local identity
and resource-version file, and retains a mode-`0600` pending request only
while an exact API replay may still be required after an uncertain response.
The supervisor should restart a failed process with the same install directory;
Admin revoke remains authoritative and prevents further rotation or heartbeat.

For a Docker deployment target, point `CLOUD_AGENTS_DOCKER_CREDENTIALS_DIR` at
a deployment-owned directory. Each registered target `credentialRef` selects a
subdirectory containing Docker Engine `ca.pem`, `cert.pem`, `key.pem`, and this
non-secret descriptor:

```json
{
  "workerImageRepository": "registry.example/cloud-agents/worker",
  "workerCredentialRef": "cloud-agents-worker-target-a",
  "workerSpiffeId": "spiffe://cloud-agents.example/workers/target-a",
  "workerServerName": "worker-target-a.example"
}
```

The target Engine must expose an mTLS HTTPS endpoint and already contain the
exact Worker image `workerImageRepository@releaseDigest`. It must also have two
pre-created named volumes: `workerCredentialRef` contains `server.crt`,
`server.key`, `client-ca.crt`, and `admission-token`; the Environment Lease
`providerCredentialRef` volume contains the existing tenant-bound Codex and/or
Claude credential envelopes. Files must be readable by container uid `1000`.
Never put private keys, admission tokens, Provider keys, or credential payloads
in `deployment.json`, target API bodies, or Environment Lease fields.

On a fresh Docker target or on the Docker host selected by an SSH target, copy
`scripts/prepare-platform-docker-target.sh` and run it with the immutable Worker
image plus the two source credential directories and volume names:

```sh
CLOUD_AGENTS_WORKER_IMAGE='registry.example/cloud-agents/worker@sha256:...' \
CLOUD_AGENTS_WORKER_CREDENTIAL_REF=cloud-agents-worker-target-a \
CLOUD_AGENTS_WORKER_CREDENTIAL_DIR=/secure/worker-credentials \
CLOUD_AGENTS_PROVIDER_CREDENTIAL_REF=cloud-agents-provider-tenant-a \
CLOUD_AGENTS_PROVIDER_CREDENTIAL_DIR=/secure/provider-credentials \
CLOUD_AGENTS_TENANT=tenant-a \
  sh scripts/prepare-platform-docker-target.sh
```

The script pulls only the explicit digest when absent, creates the two named
volumes, copies the credential files as uid `1000` with mode `0400`, and refuses
to overwrite a non-empty volume. It never prints or hashes credential content.
Run it locally on the target; SSH transport and Docker contexts remain operator
configuration rather than API fields.

For a Kubernetes deployment target, point
`CLOUD_AGENTS_KUBERNETES_CREDENTIALS_DIR` at a deployment-owned directory.
Each `credentialRef` selects `<credentialRef>.ca.crt` and
`<credentialRef>.token`, plus this non-secret deployment descriptor:

```json
{
  "namespace": "cloud-agents-target",
  "workerImageRepository": "registry.example/cloud-agents/worker",
  "workerCredentialSecretRef": "cloud-agents-worker-target-a",
  "workerSpiffeId": "spiffe://cloud-agents.example/workers/target-a",
  "workerServerName": "worker-target-a.example"
}
```

Store the descriptor as `<credentialRef>.deployment.json`. The token should
belong to a ServiceAccount allowed to GET `/version` and the referenced Secrets,
and to GET/LIST/CREATE/PATCH/DELETE Deployments, Services, and
PersistentVolumeClaims in the configured namespace. The Control Plane sends it
only as an HTTPS Bearer
credential and never persists it in target state. Each Environment Lease uses
Server-Side Apply to reconcile a single Worker Deployment, a 20 Gi workspace
PVC using the namespace's default StorageClass, and a LoadBalancer Service.
`workerCredentialSecretRef` must contain `server.crt`, `server.key`,
`client-ca.crt`, and `admission-token`; the Lease `providerCredentialRef` names
the target Secret containing the existing tenant Provider credential envelope.
Helm installations can mount the same flat file layout from the Secret named by
`deploymentTargets.kubernetesCredentialSecretName`.

On a fresh target, run the packaged preparation script with an explicit
kubeconfig context, token lifetime, namespace, and deployment-owned credential
directory. It creates the namespace, a namespaced ServiceAccount/RoleBinding,
the Worker and Provider Secrets, and the three flat Control Plane credential
files without printing or hashing their contents:

```sh
CLOUD_AGENTS_KUBECONFIG=/secure/target.kubeconfig \
CLOUD_AGENTS_KUBERNETES_CONTEXT=target-a \
CLOUD_AGENTS_KUBERNETES_NAMESPACE=cloud-agents-target \
CLOUD_AGENTS_KUBERNETES_SERVICE_ACCOUNT=cloud-agents-control-plane \
CLOUD_AGENTS_KUBERNETES_TOKEN_DURATION=24h \
CLOUD_AGENTS_TARGET_CREDENTIAL_REF=kubernetes-target-a \
CLOUD_AGENTS_KUBERNETES_CREDENTIALS_DIR=/secure/control-plane-kubernetes-targets \
CLOUD_AGENTS_WORKER_IMAGE_REPOSITORY=registry.example/cloud-agents/worker \
CLOUD_AGENTS_WORKER_CREDENTIAL_SECRET_REF=cloud-agents-worker-target-a \
CLOUD_AGENTS_WORKER_CREDENTIAL_DIR=/secure/worker-credentials \
CLOUD_AGENTS_PROVIDER_CREDENTIAL_SECRET_REF=cloud-agents-provider-tenant-a \
CLOUD_AGENTS_PROVIDER_CREDENTIAL_DIR=/secure/provider-credentials \
CLOUD_AGENTS_TENANT=tenant-a \
CLOUD_AGENTS_WORKER_SPIFFE_ID=spiffe://cloud-agents.example/workers/target-a \
CLOUD_AGENTS_WORKER_SERVER_NAME=worker-target-a.example \
  sh scripts/prepare-platform-kubernetes-target.sh
```

The ServiceAccount may only read the two named Secrets and reconcile/list the
Worker Deployments, Services, and PVCs. The script also verifies GET `/version`
(normally granted to authenticated users), refuses existing Secrets or output
files, and leaves any safely created resources in place on later failure rather
than guessing that it owns pre-existing cluster state. Rotate the token file
before the explicitly requested lifetime expires.

For an SSH deployment target, point `CLOUD_AGENTS_SSH_CREDENTIALS_DIR` at a
deployment-owned directory. Each `credentialRef` selects three flat files:
`<credentialRef>.user`, `<credentialRef>.key`, and
`<credentialRef>.host-key.pub`. The user is an identifier, the private key must
not be group/world-readable, and the host-key file contains the pinned OpenSSH
public host key. Register the host as `ssh://host[:port]`; userinfo is rejected.
The Control Plane uses these files only for the authenticated probe and never
returns their contents in target state, events, logs, or errors. Helm
installations can mount the same layout from
`deploymentTargets.sshCredentialSecretName`.

SSH Environment Leases use the target host's existing Docker installation.
Add `<credentialRef>.deployment.json` with the same non-secret
`workerImageRepository`, `workerCredentialRef`, `workerSpiffeId`, and
`workerServerName` fields used by a Docker target. The host must already contain
the exact Worker image and the named Worker/provider credential volumes. A
published Environment Profile starts the same read-only Worker image with an
isolated workspace volume, its fixed CPU/memory limits, generation labels, and
`unless-stopped` restart policy. Exact User Environment retries reuse the owned
container. Admin upgrade/rollback operations replace the Worker generation on
the same workspace, while User Environment termination removes the active
generation and its anonymous workspace volume. A container with mismatched
ownership, generation, image, or credential references is never replaced or
deleted.

After a target is ready, run `cloud-agentsctl ... target cleanup
--expected-generation GENERATION --confirm-target-id TARGET_ID` with an Admin
token to remove stale managed Docker/SSH Worker containers or Kubernetes
Deployments, Services, and PVCs. Cleanup retains every exact active Environment
Lease, validates the target and Lease generations before deletion, and never
deletes target Secrets or named credential volumes.

Run the real Kubernetes target acceptance with
`sh scripts/test-platform-kubernetes-target.sh`.
It requires `CLOUD_AGENTS_ENDPOINT`, `CLOUD_AGENTS_ADMIN_TOKEN_FILE`,
`CLOUD_AGENTS_USER_TOKEN_FILE`, `CLOUD_AGENTS_TENANT`, `CLOUD_AGENTS_PROJECT`,
`CLOUD_AGENTS_TARGET_ID`,
`CLOUD_AGENTS_TARGET_ENDPOINT`, `CLOUD_AGENTS_TARGET_CREDENTIAL_REF`,
`CLOUD_AGENTS_WORKER_IMAGE_REPOSITORY`, `CLOUD_AGENTS_RELEASE_DIGEST`,
`CLOUD_AGENTS_WORKER_ARCHITECTURE`, `CLOUD_AGENTS_PLATFORM_VERSION`,
`CLOUD_AGENTS_RUNTIME_VERSION`, `CLOUD_AGENTS_CODEX_VERSION`,
`CLOUD_AGENTS_CLAUDE_CODE_VERSION`, `CLOUD_AGENTS_PROVIDER_SECRET_REF`,
`CLOUD_AGENTS_KUBECONFIG`, `CLOUD_AGENTS_KUBERNETES_NAMESPACE`, and a new
`CLOUD_AGENTS_E2E_OUTPUT_DIR`. The Control Plane credential directory and target
Secrets must already contain the files described above. The Admin token must
use the Admin audience and the User token the User audience. The script
registers the release and policies, publishes a fixed Profile, creates and
terminates the Environment through the User API, runs a real Codex Turn,
restarts the Worker Deployment, resumes the Codex Session, runs a
real Claude Turn, resolves real approval and user-input requests, cancels and
interrupts live executions, validates downloaded Artifacts, terminates twice to
verify idempotency, runs orphan cleanup, and retains non-secret JSON/JSONL
results.

Run the real SSH target acceptance with
`sh scripts/test-platform-ssh-target.sh`. It uses the same Control Plane inputs
plus `CLOUD_AGENTS_PROVIDER_VOLUME_REF`, `CLOUD_AGENTS_SSH_HOST`,
`CLOUD_AGENTS_SSH_USER`, `CLOUD_AGENTS_SSH_IDENTITY_FILE`, and
`CLOUD_AGENTS_SSH_KNOWN_HOSTS_FILE`; set `CLOUD_AGENTS_SSH_PORT` when it is not
22. The operator key is read only by OpenSSH with `IdentitiesOnly` and strict
host-key checking. The script replays deployment, runs a real Codex Turn,
crashes the remote Worker process and verifies its policy-driven restart,
resumes the Codex Session, runs a real Claude Turn, resolves real approval and
user-input requests, cancels and interrupts live executions, validates events
and Artifacts, terminates twice,
and verifies remote cleanup. The mounted Control Plane credential reference,
target volumes, and Worker image must already satisfy the SSH target layout
above.

The source checkout's packaged Compose smoke keeps its default no-credential
`provider_not_installed` check. To additionally run one real Codex Turn and one
real Claude Code Turn against its independently registered Docker target, pass
an absolute deployment-owned credential directory as the second argument:

```sh
./scripts/test-platform-compose.sh RELEASE_DIRECTORY /absolute/provider-credentials
```

That directory must contain the two inputs selected by the smoke,
`tenant-compose-smoke.codex.json` and
`tenant-compose-smoke.claudeAgent.json`, using the envelopes described above.
The smoke copies those two files directly into its temporary target credential
volume, validates each successful execution, downloads its generated-file
Artifact, and removes the volume during cleanup. It does not print or copy the
credential payloads into the release or host-side smoke directory. With or
without real Provider credentials, it restarts the Control Plane while a target
Lease and durable Execution exist and verifies both through the public status
commands. A run without the second argument is not real Provider E2E evidence.

The auth JSON may contain either an explicit `keys` array or an HTTPS `jwksUrl`.
The Control Plane fetches JWKS at startup and on `SIGHUP`; a reload must publish
the next `generation` and keeps the previous key material bound to its lineage.
When the Control Plane certificate uses a private CA, pass its PEM bundle to the
packaged CLI with `cloud-agentsctl --ca-file PATH ...`.

For Kubernetes, use `deploy/helm/cloud-agents` from the extracted directory. The chart expects an external
PostgreSQL database and pre-created Secrets named by `values.yaml`: database
URLs (`runtime-url`, `migration-url`), `auth.json`, Control Plane/Worker mTLS,
Runtime provider environment, tenant-bound Provider credentials
(`<tenantId>.codex.json` and/or `<tenantId>.claudeAgent.json`), and Runtime
admission (`lease-id`, `generation`, `token`).
Override all three image repositories and set their digests from
`cloud-agents-oci-images.json` before installing. A non-empty digest takes
precedence over the chart's fallback tag:

```sh
helm upgrade --install cloud-agents deploy/helm/cloud-agents \
  --set images.controlPlane.repository=REGISTRY/control-plane \
  --set images.worker.repository=REGISTRY/worker \
  --set images.migrate.repository=REGISTRY/migrate \
  --set-string images.controlPlane.digest=sha256:CONTROL_PLANE_DIGEST \
  --set-string images.worker.digest=sha256:WORKER_DIGEST \
  --set-string images.migrate.digest=sha256:MIGRATE_DIGEST
```

The migration Job runs before install and upgrade. On first install, the following
Hook creates the initial tenant, organization, and `tenant.admin` after migration.
Add `tenant-bootstrap-url` to the database Secret and create the
`cloud-agents-tenant-bootstrap` Secret with these keys:

```text
CLOUD_AGENTS_TENANT_UID
CLOUD_AGENTS_TENANT_NAME
CLOUD_AGENTS_TENANT_DISPLAY_NAME
CLOUD_AGENTS_ORGANIZATION_UID
CLOUD_AGENTS_ORGANIZATION_NAME
CLOUD_AGENTS_ORGANIZATION_DISPLAY_NAME
CLOUD_AGENTS_ADMIN_SUBJECT_KIND
CLOUD_AGENTS_ADMIN_SUBJECT_ISSUER
CLOUD_AGENTS_ADMIN_SUBJECT_VALUE
CLOUD_AGENTS_ADMIN_MEMBERSHIP_UID
CLOUD_AGENTS_ADMIN_MEMBERSHIP_NAME
CLOUD_AGENTS_ADMIN_ROLE_BINDING_UID
CLOUD_AGENTS_ADMIN_ROLE_BINDING_NAME
CLOUD_AGENTS_TENANT_AUDIT_FACT_UID
CLOUD_AGENTS_MEMBERSHIP_AUDIT_FACT_UID
CLOUD_AGENTS_ROLE_BINDING_AUDIT_FACT_UID
CLOUD_AGENTS_BOOTSTRAP_REASON_CODE
```

Override the Secret names or database key through `values.yaml`. Disable
`tenantBootstrap.enabled` only when the same bootstrap was completed externally.
Exact retries are safe; conflicting existing state fails the installation. Use
standard `helm rollback cloud-agents REVISION` to restore a prior image/chart
revision; database rollback remains an explicit forward-migration operation.
