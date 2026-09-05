# cloud-agents-worker (localdev)

This command is compiled only with `-tags localdev`:

```sh
cd services/worker
GOFLAGS=-mod=readonly go run -tags localdev ./cmd/cloud-agents-worker \
  --listen 127.0.0.1:8091 --token-file /tmp/cloud-agents-worker.token
```

The listener is loopback-only and exposes the generated Worker Connect handler
plus an authenticated read-only `/healthz` endpoint. The token file is created
once with `O_EXCL` and mode `0600`; the token is never printed. The process
uses an in-memory `worker.NewService`, does not configure a provider or
database, and shuts down gracefully on `SIGINT`/`SIGTERM`.

To run the Managed Agent Runtime path, start the Worker with the standalone
Runtime and an absolute workspace:

```sh
GOFLAGS=-mod=readonly go run -tags localdev ./cmd/cloud-agents-worker \
  --listen 127.0.0.1:8091 --token-file /tmp/cloud-agents-worker.token \
  --runtime-command /absolute/path/cloud-agent-runtime \
  --runtime-directory /absolute/path/workspace \
  --provider-credential-directory /absolute/path/provider-credentials
```

Credential files are tenant-bound: for example, tenant `tenant-local` uses
`tenant-local.codex.json` or `tenant-local.claudeAgent.json`.

The localdev Control Plane connects with `--worker-endpoint
http://127.0.0.1:8091 --worker-token-file /tmp/cloud-agents-worker.token`.
Runtime streaming uses cleartext HTTP/2 only on the validated loopback endpoint;
production continues to require mTLS.
