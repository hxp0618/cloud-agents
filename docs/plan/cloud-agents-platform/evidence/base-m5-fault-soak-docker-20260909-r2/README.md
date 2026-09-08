# BASE-M5 Docker fault and bounded soak evidence

Run from `/Users/huang/devel/project/huang/business/cloud-agents`:

```sh
node scripts/test-foundation-controller-docker.mjs \
  docs/plan/cloud-agents-platform/evidence/base-m5-fault-soak-docker-20260909-r2 \
  --fault-soak-only
```

The run used source HEAD `536e6ccaae00b7e97d49f27d5105f7b3532ccb9b`, product migration `000088`, OrbStack Docker `29.4.0`, and PostgreSQL `17.6`.

It deliberately exited a Controller process after physical Sandbox creation and before settlement. A new process recovered the same Operation, runtime, retained Workspace bytes, and compensated a separate failed runtime. The measured fault-to-recovery upper bound was `5750.240 ms`; no Operation row or Workspace byte was lost.

Two separate Control Plane test processes used production handlers, generated SDK clients, and the same PostgreSQL authority. Each process ran 64 cycles of six successful Admin reads plus one ordinary-user `403`: 896 requests total. Before restart, successful read P50/P95 were `2.215/3.283 ms`; after restart they were `2.011/2.998 ms`. The restarted process reached its first successful Admin response in `25.559 ms`, and the persisted resource digest was unchanged.

`evidence.json` SHA-256: `e6779f83b86ebf41e02c8bac671d825704c385c5cb35672f8775d1fac66e8aba`. `opensandbox.log` SHA-256: `0ea430e9dd31191d4f5d8fdddd84f3a6f0352161f1a46ebbea7b4876ab6f3cdd`.

This is a bounded local measurement, not an SLO or a write-saturation result. It does not cover Kubernetes, SSH, outbound RemoteWorker, external load balancers, or multi-Region faults. The run removed all test-owned runtime containers, Workspace/Snapshot volumes, networks, and database containers.
