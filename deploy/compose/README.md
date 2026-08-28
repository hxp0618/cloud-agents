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
