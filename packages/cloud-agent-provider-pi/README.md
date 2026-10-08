# `@cloud-agents/cloud-agent-provider-pi`

Host-neutral Pi RPC provider plugin for `@cloud-agents/cloud-agent-runtime`, pinned to `@earendil-works/pi-coding-agent` `0.85.1`. The wire kind is `pi`.

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
  "$candidate/$(tarball '@cloud-agents/cloud-agent-provider-pi')" \
  "$candidate/$(tarball '@cloud-agents/cloud-agent-provider-api')" \
  "$candidate/$(tarball '@cloud-agents/cloud-agent-protocol')"
```

```ts
import { createPiProvider } from "@cloud-agents/cloud-agent-provider-pi";

const provider = createPiProvider();
const descriptor = await provider.describe();
```

Register the returned `CloudAgentProviderPluginV1` with the host's Provider ABI. A controlled credential payload contains `apiKey`, `baseUrl` (or `baseURL`) and `model`:

```json
{ "apiKey": "<secret>", "baseUrl": "https://api.openai.com/v1", "model": "<model>" }
```

The host must set `CLOUD_AGENT_PROVIDER_OUTER_SANDBOX_PROFILE` to `kubernetes-restricted-v1`, `gvisor-sandboxed-v1`, `microvm-isolated-v1` or `single-tenant-trusted-v1`, provide an agent-owned `providerStateDirectory`, and supply a model. `baseUrl` must be an HTTP(S) URL; Pi requires a controlled credential for its API key. Managed MCP and Skill bindings are host-provided capability inputs, not package discovery.

Package tests use fakes and do not prove a live Provider call. Real acceptance requires authenticated credentials and the host's Foundation/outer-sandbox binding.
