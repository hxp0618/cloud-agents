# Independent Cloud Agents Compose deployment

Extract `cloud-agents-deployment-000040.tar` into a directory and copy
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

The Worker accepts at most `CLOUD_AGENTS_RUNTIME_MAX_SESSIONS` concurrent Runtime
sessions (default `4`). Additional session opens fail immediately with
`ResourceExhausted`; tune the value to the CPU and memory assigned to the Worker.

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
Lease starts the same read-only Worker image with an isolated workspace volume,
the requested CPU/memory limits, generation labels, and `unless-stopped`
restart policy. Exact retries reuse the owned container; an
`environment-lease upgrade` starts the next generation on the same workspace
and keeps the previous generation until the new Worker is ready. Termination
removes the active generation and its anonymous workspace volume. A container
with mismatched ownership, generation, image, or credential references is never
replaced or deleted.

After a target is ready, run `cloud-agentsctl ... target cleanup
--expected-generation GENERATION` to remove stale managed Docker/SSH Worker
containers or Kubernetes Deployments, Services, and PVCs. Cleanup retains every
exact active Environment Lease, validates the target and Lease generations
before deletion, and never deletes target Secrets or named credential volumes.

Run the real Kubernetes target acceptance with
`sh scripts/test-platform-kubernetes-target.sh`.
It requires `CLOUD_AGENTS_ENDPOINT`, `CLOUD_AGENTS_TOKEN_FILE`,
`CLOUD_AGENTS_TENANT`, `CLOUD_AGENTS_PROJECT`, `CLOUD_AGENTS_TARGET_ID`,
`CLOUD_AGENTS_TARGET_ENDPOINT`, `CLOUD_AGENTS_TARGET_CREDENTIAL_REF`,
`CLOUD_AGENTS_RELEASE_DIGEST`, `CLOUD_AGENTS_PROVIDER_SECRET_REF`,
`CLOUD_AGENTS_KUBECONFIG`, `CLOUD_AGENTS_KUBERNETES_NAMESPACE`, and a new
`CLOUD_AGENTS_E2E_OUTPUT_DIR`. The Control Plane credential directory and target
Secrets must already contain the files described above. The script runs a real
Codex Turn, restarts the Worker Deployment, resumes the Codex Session, runs a
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
