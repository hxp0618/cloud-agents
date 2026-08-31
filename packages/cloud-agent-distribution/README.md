# `@cloud-agents/cloud-agent-distribution`

Pinned Cloud Agent distribution for independent hosts. Synara and T3 Code
remain future consumers/integrations outside the current product scope. Its
JavaScript factory explicitly registers the allowlisted Providers; it also
exports the stdio client, deeply immutable source manifest, and the Protocol v2
envelope schema from `@cloud-agents/cloud-agent-distribution/schemas`. It does not
modify a host during installation.

The `cloud-agent-runtime` bin is emitted as a single bundled executable so a
host-side SHA-256 check covers the actual Runtime implementation rather than an
external-import shim. Its Provider allowlist is declared by `manifest.json` and
must match the explicitly composed Runtime registry before a candidate is
accepted.

Shell hosts start the NDJSON protocol with `cloud-agent-runtime --protocol-v2`.
Programmatic hosts use `resolveCloudAgentRuntimeLaunch(packageRoot,
nodeExecutable)` and pass its `executable` and `args` to the public stdio
client. This launches the bundled `.mjs` through an explicit Node 24 binary on
Windows as well as Unix. Electron hosts may use `process.execPath` only when
the child has `ELECTRON_RUN_AS_NODE=1` and Electron's embedded Node satisfies
the package engine range. The Runtime accepts only the Codex and Claude
plugins pinned by the manifest; an unknown or disabled Provider returns
`provider_not_installed`.

Release validation is tarball-first:

```sh
node scripts/cloud-agent-release-smoke.ts --output-dir /new/candidate/directory
```

The check builds and packs all seven public packages, rejects local dependency
protocols and unpublished workspace dependencies, installs the tarballs into a fresh
Node 24 project, exercises ESM, CommonJS, schemas, and the real bin, and then
verifies that every tarball SHA-256 is unchanged. `--allow-dirty` exists only
for local source validation; a release candidate requires a clean tree.
