# Cloud Agents User Console

The console uses the generated TypeScript Platform SDK and keeps the bearer token in memory only.
Endpoint, tenant, project, Target ID, and Lease ID are the only values restored from `sessionStorage`;
the page always reloads current generations and phases from Control Plane.

The infrastructure workspace supports Target list/register/get/probe/cleanup and Environment Lease
list/create/get/upgrade/terminate. Forms send credential references only. Docker sockets, kubeconfig,
SSH keys, and Provider JSON stay in deployment-owned Control Plane or target-side mounts.

Lease upgrade reuses the existing workspace. Docker and SSH keep the old Worker generation until the
successor is recorded Ready; Kubernetes uses a zero-unavailable rolling update.

For local development, proxy `/v1` to a Control Plane instead of enabling broad CORS:

```sh
CLOUD_AGENTS_CONTROL_PLANE_URL=http://127.0.0.1:8080 bun run --cwd apps/user-web dev
```

Open `http://127.0.0.1:4173`. Production deployments should serve the console and reverse proxy
`/v1` from the same origin.

Checks:

```sh
bun run --cwd apps/user-web typecheck
bun run --cwd apps/user-web test
bun run --cwd apps/user-web build
```
