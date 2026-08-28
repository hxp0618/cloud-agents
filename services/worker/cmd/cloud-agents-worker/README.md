# cloud-agents-worker (localdev)

This command is compiled only with `-tags localdev`:

```sh
cd services/worker
GOWORK=off GOFLAGS=-mod=readonly go run -tags localdev ./cmd/cloud-agents-worker \
  --listen 127.0.0.1:8091 --token-file /tmp/cloud-agents-worker.token
```

The listener is loopback-only and exposes the generated Worker Connect handler
plus an authenticated read-only `/healthz` endpoint. The token file is created
once with `O_EXCL` and mode `0600`; the token is never printed. The process
uses an in-memory `worker.NewService`, does not configure a provider or
database, and shuts down gracefully on `SIGINT`/`SIGTERM`.
