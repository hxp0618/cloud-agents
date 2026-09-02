# Cloud Agents User Console

The console uses the generated TypeScript Platform SDK and keeps the bearer token in memory only.
Endpoint, tenant, and selected project are the only values restored from `sessionStorage`.

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
