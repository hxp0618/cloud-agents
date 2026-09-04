# M3 Admin Web Cluster and Host View E2E

Date: 2026-09-04

Status: passed for the authority-backed Cluster and Host view vertical slice. This does not complete M3 or the Section 15 acceptance matrix.

## Authority under test

- Branch: `codex/cloud-agents-platform-p0`, starting HEAD `6d56d8c`.
- Admin Web: current source served by the independent Vite application on `127.0.0.1:4174`.
- Runtime: the unpublished local packaged release from `M3-WORKER-INVENTORY-E2E.md`, with PostgreSQL 17.6, migrations, Control Plane, Worker, local registry, and a real Docker Deployment Target through the host Docker API.
- The page combines the generated `targets.list` and `workers.list` Admin APIs. It does not add a duplicate Cluster database table, a third inventory endpoint, a fixture response, or browser-side target access.
- Target runtime/API, OS, architecture, phase, and Probe time come from persisted Control Plane Probe facts. Worker counts and latest health come from the current Lease-backed Worker authority grouped by `targetId`.

## Real browser evidence

The Codex in-app Chromium browser connected with the in-memory Admin bearer through the Vite `/v1/admin` proxy. The persisted project was `project-2fde604e9ef099aad6125e5a2181196c` in `tenant-compose-smoke`.

The combined page rendered two ready targets and one current Worker:

```text
docker-compose-target      Docker API      engine 29.4.0 / API 1.54    linux/arm64    1 ready / 1 total
kubernetes-compose-target  Kubernetes API  engine v1.34.2 / API 1.34  linux/arm64    0 current Workers
```

- Selecting the Docker host opened the existing generated-API Target detail Sheet with its exact Probe facts, generation/resource version, two durable operations, and three audit events.
- The page exposed no arbitrary terminal and made no browser connection to Docker or Kubernetes; all reads passed through the Control Plane Admin API.
- `zh-CN` and `en-US` switched immediately, including the new navigation, headings, search, table columns, counts, empty-state text, and authority boundary. Light and dark themes both rendered without a missing translation key.
- At `791x998`, document width was `791/791`; the Cluster/Host and Worker tables scrolled internally at `501/1120` and `501/1363`.
- At `390x844`, document width was `390/390`; both tables remained internal scrollers at `356/1120` and `356/1363`. The Chinese mobile navigation opened and closed, and both scope chips aligned at the content edge.
- Browser warning/error logs were empty.

## Runtime and source verification

The packaged Compose smoke completed after the browser run, including Control Plane restart and PostgreSQL backup/restore:

```text
platform Compose smoke passed (linux/arm64, darwin-arm64, profile=compose-docker-profile:v1, environment=environment-0cc46b1599b67da3345de72ee27f3312, docker-workers=0)
```

After cleanup, the test project had zero Compose containers, zero Compose volumes, and no listener on Admin Web port 4174.

The repository-pinned Bun 1.3.14 and Node 24.18.1 toolchains ran:

```text
apps/admin-web test       # 2 files, 13 tests
apps/admin-web typecheck
apps/admin-web build
apps/admin-web oxlint
repository fmt:check
repository typecheck
repository lint
git diff --check
```

## Evidence boundary

- This slice proves the real Control Plane Target and Worker authorities are combined into one bilingual, responsive Cluster/Host and Worker operations view without a new backend model.
- The existing Cleanup Preview API remains the on-demand live container/workload inventory and is not called once per overview row. This run's Kubernetes endpoint was the protocol fixture with zero managed workloads, not a real cluster, and no real SSH host was exercised.
- Drain/Resume, Upgrade/Rollback, Releases catalog, Quota, Storage/Network policy management, real Kubernetes/SSH deployment, and real Codex/Claude Code Provider turns remain required.
