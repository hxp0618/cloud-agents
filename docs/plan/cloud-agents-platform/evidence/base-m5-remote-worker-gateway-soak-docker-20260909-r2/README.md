# BASE-M5 RemoteWorker and Access Gateway fault/soak evidence

Run from `/Users/huang/devel/project/huang/business/cloud-agents`:

```sh
node scripts/test-foundation-controller-docker.mjs \
  docs/plan/cloud-agents-platform/evidence/base-m5-remote-worker-gateway-soak-docker-20260909-r2 \
  --remote-worker-only
```

The run used source HEAD `a728cedbac48c38a961efab2f6c25fadb0c634e3`, product migration `000088`, OrbStack Docker `29.4.0`, and PostgreSQL `17.6`.

The test cut the RemoteWorker outbound connection during a renewed long-running claim. The original uncertain command was not replayed; after natural claim expiry, the worker reconciled attempt 2. Disconnect-to-settlement was `60311.396 ms`, while reconnect-to-local-receipt and reconnect-to-Control-Plane-settlement were `192.579 ms` and `238.517 ms`. No Operation row was lost and no duplicate runtime was created.

After recovery, 64 authenticated mTLS heartbeats and 64 matching Admin reads completed. Heartbeat P50/P95 were `9.366/11.064 ms`; Admin read P50/P95 were `2.450/3.105 ms`.

Preview, Files, PTY, and SSH each recovered through a newly started Access Gateway. Their measured recovery times were `212.549`, `296.607`, `227.069`, and `1577.846 ms`; verified Grant rows, 1.9 MiB file bytes, and PTY output had zero loss. The run also reproduced and fixed a non-PTY SSH race: RemoteWorker now waits within the existing command deadline for the exit frame instead of closing after the first 250 ms idle interval with only terminal echo.

`evidence.json` SHA-256: `5d9bce8a5b28e23225f883bedf8277df1dbbd90a429655b5f21f700ac0ccb2cc`. `opensandbox.log` SHA-256: `9f54d225a6643344cef29ffb74b6876e2e0bca11de97abe1f290da9bd07f371b`.

These are bounded current-machine measurements, not SLOs. Kubernetes, external SSH customer nodes, streaming Preview, and multi-Region faults are not covered. Final test-owned runtime containers, Workspace volumes, networks, and database containers were zero.
