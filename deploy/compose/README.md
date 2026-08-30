# Independent Cloud Agents Compose deployment

Extract `cloud-agents-deployment-000025.tar` into a directory, then run Compose
from its `deploy/compose` directory with an env file copied from `.env.example`.
Set `CLOUD_AGENTS_DEPLOY_DIR` to the extracted directory's `deploy` path.

The bootstrap profile provisions the fixed Compose database roles in one
transaction and fails closed on existing role drift:

1. Run `docker compose --env-file .env --profile bootstrap run --rm bootstrap`
   once with an isolated unswitched superuser URL.
2. Run `docker compose --env-file .env --profile tenant-bootstrap run --rm tenant-bootstrap`
   once to migrate the database and create the initial tenant, organization, and `tenant.admin`
   membership for the configured authenticated subject. Exact retries are safe; conflicting
   retries fail without partial changes.
3. Run `docker compose --env-file .env up --build`.

The migration URL must be able to `SET ROLE cloud_agents_migration_owner`; the
runtime URL uses the least-privileged `cloud_agents_runtime_login`. Use the
tenant-bootstrap URL only for the packaged one-shot bootstrap. TLS,
JWK trust configuration, Runtime provider environment, SPIFFE identity, and
Runtime admission values are deployment-owned inputs and are never generated
by this package. Copy `runtime.env.example` for the non-secret Provider Host
settings. Keep credentials outside the archives in the referenced directory,
using the Runtime's existing anonymous-FD envelopes: `codex.json` contains
`{"payload":{"apiKey":"..."}}`; `claudeAgent.json` contains exactly one of
`apiKey` or `authToken` under `payload`. Optional `baseUrl` values and Codex
`organization` use the same payload object. The Worker binds one file to the
requested Provider before starting each Runtime process; provider keys in the
runtime env file are ignored.

The auth JSON may contain either an explicit `keys` array or an HTTPS `jwksUrl`.
The Control Plane fetches JWKS at startup and on `SIGHUP`; a reload must publish
the next `generation` and keeps the previous key material bound to its lineage.

For Kubernetes, use `deploy/helm/cloud-agents` from the extracted directory. The chart expects an external
PostgreSQL database and pre-created Secrets named by `values.yaml`: database
URLs (`runtime-url`, `migration-url`), `auth.json`, Control Plane/Worker mTLS,
Runtime provider environment, Provider credentials (`codex.json` and/or
`claudeAgent.json`), and Runtime admission (`lease-id`, `generation`, `token`).
Override all three image repositories and tags with the OCI images
built from this release before installing:

```sh
helm upgrade --install cloud-agents deploy/helm/cloud-agents \
  --set images.controlPlane.repository=REGISTRY/control-plane \
  --set images.worker.repository=REGISTRY/worker \
  --set images.migrate.repository=REGISTRY/migrate \
  --set images.controlPlane.tag=VERSION \
  --set images.worker.tag=VERSION \
  --set images.migrate.tag=VERSION
```

The migration Job runs before install and upgrade. After the first migration, run
`deploy/compose/tenant-bootstrap.sql` once with `psql` and the tenant-bootstrap URL,
supplying the same named variables shown in the Compose service. Use standard
`helm rollback cloud-agents REVISION` to restore a prior image/chart revision;
database rollback remains an explicit forward-migration operation.
