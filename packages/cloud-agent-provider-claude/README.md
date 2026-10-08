# `@cloud-agents/cloud-agent-provider-claude`

Host-neutral Claude provider plugin for `@cloud-agents/cloud-agent-runtime`. The compatible wire kind is `claudeAgent`; `claude` is accepted as an input alias.

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
  "$candidate/$(tarball '@cloud-agents/cloud-agent-provider-claude')" \
  "$candidate/$(tarball '@cloud-agents/cloud-agent-provider-api')" \
  "$candidate/$(tarball '@cloud-agents/cloud-agent-protocol')"
```

```ts
import { createClaudeProvider } from "@cloud-agents/cloud-agent-provider-claude";

const provider = createClaudeProvider();
const descriptor = await provider.describe();
```

Register the returned `CloudAgentProviderPluginV1` with the host's Provider ABI. A controlled credential payload must contain exactly one of `apiKey` or `authToken`; it may also contain `baseUrl` (or the `baseURL` alias) and `model`:

```json
{ "authToken": "<secret>", "baseUrl": "https://api.anthropic.com", "model": "<optional>" }
```

The host must set `CLOUD_AGENT_PROVIDER_OUTER_SANDBOX_PROFILE` to `kubernetes-restricted-v1`, `gvisor-sandboxed-v1`, `microvm-isolated-v1` or `single-tenant-trusted-v1`. Without a controlled credential, the provider may use the host's ambient Claude authentication; that path still requires the host sandbox binding. The package is pinned to Claude Agent SDK `0.3.207`.

Package tests use fakes and do not prove a live Provider call. Real acceptance requires authenticated credentials, the host Foundation binding and the matching SDK/runtime environment.
