# Codex app-server fixtures

`codex-0.145.0-config-read.json` is the non-sensitive, isolation-relevant subset of the
`config/read` response observed from the official `@openai/codex@0.145.0` app-server.
It was captured with fresh temporary `HOME` and `CODEX_HOME` directories after starting
Codex with `managedCodexAppServerArguments`, then requesting
`config/read({ includeLayers: false })` before opening a Thread.

The fixture intentionally excludes credentials, user configuration, filesystem paths,
and fields that are not examined by the managed runtime-isolation attestor.
