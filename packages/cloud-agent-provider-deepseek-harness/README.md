# `@cloud-agents/cloud-agent-provider-deepseek-harness`

Host-neutral SDK JSON-RPC adapter for `deepseek-harness`, pinned to `@deepseek-ai/dsh-sdk-client` `0.1.2-rc.1`. The wire kind is `deepseek-harness`.

## Use

The repository does not publish these RC versions to npm. Download the GitHub release candidate, verify its manifest and checksums, then install the coordinated local tarballs:

```sh
candidate=/path/to/cloud-agents-release-candidate
if command -v sha256sum >/dev/null 2>&1; then
  (cd "$candidate" && sha256sum -c checksums.sha256)
else
  (cd "$candidate" && shasum -a 256 -c checksums.sha256)
fi
tarball() {
  node -e 'const fs = require("node:fs"); const [manifest, name] = process.argv.slice(1); const item = JSON.parse(fs.readFileSync(manifest, "utf8")).packages.find((entry) => entry.name === name); if (!item) process.exit(1); process.stdout.write(item.filename);' "$candidate/candidate-manifest.json" "$1"
}
npm install \
  "$candidate/$(tarball '@cloud-agents/cloud-agent-provider-deepseek-harness')" \
  "$candidate/$(tarball '@cloud-agents/cloud-agent-provider-api')" \
  "$candidate/$(tarball '@cloud-agents/cloud-agent-protocol')"
```

```ts
import { createDeepSeekHarnessProvider } from "@cloud-agents/cloud-agent-provider-deepseek-harness";

const provider = createDeepSeekHarnessProvider();
const descriptor = await provider.describe();
```

Register the returned `CloudAgentProviderPluginV1` with the host's Provider ABI. A controlled credential payload contains `apiKey`, `baseUrl` (or `baseURL`) and `model`:

```json
{ "apiKey": "<secret>", "baseUrl": "https://api.deepseek.com/v1", "model": "<model>" }
```

The host must set `CLOUD_AGENT_PROVIDER_OUTER_SANDBOX_PROFILE` to `kubernetes-restricted-v1`, `gvisor-sandboxed-v1`, `microvm-isolated-v1` or `single-tenant-trusted-v1`, provide an agent-owned `providerStateDirectory`, and supply a model. This adapter supports only the `GenerateText` operation; an optional `CLOUD_AGENT_DEEPSEEK_HARNESS_BIN` selects the harness executable. Managed MCP and Skill bindings are host-provided capability inputs.

Package tests use fakes and do not prove a live Provider call. Real acceptance requires authenticated credentials, the harness/runtime dependency and the host's Foundation/outer-sandbox binding.
