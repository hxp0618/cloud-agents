# Independent Cloud Agents Compose deployment

Extract `cloud-agents-deployment-000035.tar` into a directory and copy
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
runtime env file are ignored.

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

For a Kubernetes deployment target, point
`CLOUD_AGENTS_KUBERNETES_CREDENTIALS_DIR` at a deployment-owned directory.
Each `credentialRef` selects `<credentialRef>.ca.crt` and
`<credentialRef>.token`. The token should belong to a ServiceAccount allowed to
read the target API server's `/version` endpoint; the Control Plane sends it
only as an HTTPS Bearer credential and never persists it in target state.
Helm installations can mount the same flat file layout from the Secret named by
`deploymentTargets.kubernetesCredentialSecretName`.

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
credential payloads into the release or host-side smoke directory. A run
without the second argument is not real Provider E2E evidence.

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
