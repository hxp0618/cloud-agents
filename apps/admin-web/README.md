# Cloud Agents Admin Web

Independent Vite + React console for Control Plane Admin API operations. This first M1 slice uses only generated `/v1/admin/.../deployment-targets` methods and keeps the bearer token in memory.

```bash
bun --filter @cloud-agents/cloud-agent-platform-sdk build
CLOUD_AGENTS_CONTROL_PLANE_URL=http://127.0.0.1:8080 bun --filter @cloud-agents/admin-web dev
```

Open `http://127.0.0.1:4174`, enter the tenant/project IDs, and paste the token written by the local Control Plane `--local-admin-token-file` option. The ordinary `--local-token-file` token is expected to receive HTTP 403 from these routes.
