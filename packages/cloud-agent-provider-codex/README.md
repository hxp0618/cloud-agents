# `@cloud-agents/cloud-agent-provider-codex`

Host-neutral Codex provider plugin for `@cloud-agents/cloud-agent-runtime`. A host or distribution must register it explicitly; the package does not scan user directories or `node_modules`.

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
  "$candidate/$(tarball '@cloud-agents/cloud-agent-provider-codex')" \
  "$candidate/$(tarball '@cloud-agents/cloud-agent-provider-api')" \
  "$candidate/$(tarball '@cloud-agents/cloud-agent-protocol')"
```

```ts
import { createCodexProvider } from "@cloud-agents/cloud-agent-provider-codex";

const provider = createCodexProvider();
const descriptor = await provider.describe();
```

Register the returned `CloudAgentProviderPluginV1` with the host's Provider ABI. The wire kind is `codex`.

When a controlled credential is supplied, its payload may contain only these fields:

```json
{
  "apiKey": "<secret>",
  "baseUrl": "https://api.openai.com/v1",
  "organization": "<optional>",
  "model": "<optional>"
}
```

`baseURL` is accepted as an alias for `baseUrl`. The host must set `CLOUD_AGENT_PROVIDER_OUTER_SANDBOX_PROFILE` to `kubernetes-restricted-v1`, `gvisor-sandboxed-v1`, `microvm-isolated-v1` or `single-tenant-trusted-v1`, and provide an agent-owned `providerStateDirectory` or `runtimeOutputDirectory`; a controlled credential also requires the immutable tool-policy hook supplied by `createCodexProvider`.

Codex execution requires a compatible `codex` executable. `GenerateText` does not accept MCP or Skill capability bindings. Package tests use fakes; a real Provider acceptance run requires real credentials and the host's Foundation/outer-sandbox binding.
