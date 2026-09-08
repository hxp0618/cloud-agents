# BASE-M5 external SSH Target probe and bounded soak evidence

Run from `/Users/huang/devel/project/huang/business/cloud-agents` with two existing key-only SSH aliases whose host keys are already pinned:

```sh
CLOUD_AGENTS_FOUNDATION_SSH_ALIASES=hostdzire-4c6g,tianliyun-2c2g \
  node scripts/test-foundation-controller-docker.mjs \
  docs/plan/cloud-agents-platform/evidence/base-m5-external-ssh-probe-soak-20260909-r2 \
  --ssh-probe-only
```

The run used source HEAD `dc20ce39da05250175e7bc5fab108819163e1f07`, product migration `000088`, two external Linux/amd64 OpenSSH servers, and disposable PostgreSQL `17.6`. The harness first confirmed both aliases with the user's existing strict SSH configuration, matched `ssh-keyscan` output to an already-pinned known_hosts key, and copied credentials only into a temporary `0700` directory removed at exit. Host addresses and credential bytes are absent from the evidence package.

Two separate Control Plane test processes used production Admin handlers, generated SDK clients, and the same PostgreSQL authority. Together they completed 64 real SSH probes, 64 Admin reads, and 32 ordinary-user `403` responses. Before restart, Probe P50/P95 were `1914.307/2602.243 ms` and Admin-read P50/P95 were `6.238/9.169 ms`. After restart they were `1969.817/2526.386 ms` and `6.452/8.964 ms`; process start to the first successful external Probe was `2398.760 ms`. OS, architecture, and SSH server facts remained unchanged. A real endpoint presented with the other host's pinned key failed closed and persisted `ssh-host-key-mismatch`, while Operation and Audit pages retained the Probe history.

`evidence.json` SHA-256: `588feaed50bcec9a94df48ce324aa014c289b7f941dea4dbc548f6c1a1932ca2`. `before-restart.log`: `c94219fc4614dbedba4d3925420eb1a3e8bfab77e9028be0b5d664d175f0954e`. `after-restart.log`: `3f6e147bd3e830d09b659fece58fc80bf927f93ddd1b9b4026d5dda05d293924`.

This is a bounded compatibility check, not an SLO. It proves old SSH Target registration/Probe, strict host-key fencing, Admin Operation/Audit persistence, and reconnection across a Control Plane process restart. Neither external host has Docker, so this does not prove remote Worker deployment, Provider execution, an SSH-server restart, or cleanup of remote containers. The run made no remote file, package, service, or container change.
