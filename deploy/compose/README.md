# Independent Cloud Agents Compose deployment

Extract `cloud-agents-deployment-000017.tar` into a directory and run Compose
from this directory with an env file copied from `.env.example`.

The deployment is intentionally fail-closed around PostgreSQL authority:

1. Provision the three deployment-owned LOGIN roles and their memberships
   according to the bootstrap SQL contract.
2. Run `docker compose --env-file .env --profile bootstrap run --rm bootstrap`
   once with an isolated unswitched superuser URL.
3. Run `docker compose --env-file .env up --build`.

The migration URL must be able to `SET ROLE cloud_agents_migration_owner`; the
runtime URL must be the least-privileged `cloud_agents_runtime` workload. TLS,
JWK trust configuration, Runtime provider environment, SPIFFE identity, and
Runtime admission values are deployment-owned inputs and are never generated
by this package. Keep provider credentials in the referenced runtime env file,
outside the release and deployment archives.

For Kubernetes, use `helm/cloud-agents`. The chart expects an external
PostgreSQL database and pre-created Secrets named by `values.yaml`: database
URLs (`runtime-url`, `migration-url`), `auth.json`, Control Plane/Worker mTLS,
Runtime provider environment, and Runtime admission (`lease-id`, `generation`,
`token`). Override all three image repositories and tags with the OCI images
built from this release before installing:

```sh
helm upgrade --install cloud-agents helm/cloud-agents \
  --set images.controlPlane.repository=REGISTRY/control-plane \
  --set images.worker.repository=REGISTRY/worker \
  --set images.migrate.repository=REGISTRY/migrate \
  --set images.controlPlane.tag=VERSION \
  --set images.worker.tag=VERSION \
  --set images.migrate.tag=VERSION
```

The migration Job runs before install and upgrade. Use standard
`helm rollback cloud-agents REVISION` to restore a prior image/chart revision;
database rollback remains an explicit forward-migration operation.
