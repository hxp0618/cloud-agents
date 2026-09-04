# Cloud Agents Admin Web

Independent Vite + React console for Control Plane Admin API operations. It uses the generated Admin SDK for Deployment Targets, a combined Cluster/Host and Lease-backed Worker view, Environment Leases, immutable Environment Profiles, and project Maintenance Operations, and keeps the bearer token in memory.

The interface supports `zh-CN` and `en-US` through its local typed message catalog and browser-native `Intl`. First visit follows the leading browser language (`zh*` selects `zh-CN`; everything else selects `en-US`), the account menu changes language immediately, and the selected locale survives refresh in `cloud-agents-admin-locale`. Invalid locale values fall back to `en-US`.

```bash
bun --filter @cloud-agents/cloud-agent-platform-sdk build
CLOUD_AGENTS_CONTROL_PLANE_URL=http://127.0.0.1:8080 bun --filter @cloud-agents/admin-web dev
```

Open `http://127.0.0.1:4174`, enter the tenant/project IDs, and paste the token written by the local Control Plane `--local-admin-token-file` option. The ordinary `--local-token-file` token is expected to receive HTTP 403 from these routes. Neither token nor infrastructure credential bytes are persisted in browser storage.
