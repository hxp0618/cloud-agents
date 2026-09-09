# BASE-M5 default no-Agent deployment evidence (2026-09-09)

This slice closes a packaging gap found during the BASE-READY audit: the
released Compose and Helm defaults still required the legacy Coding Agent
Worker, admission token, Runtime environment and Provider credentials even
though BASE is a no-Agent foundation.

The implementation was verified from branch
`codex/cloud-agents-platform-p0`, source commit
`dc6e0e8a461fdf06a7ef742486a5c33837e2423a` plus the related dirty slice. The
unpublished candidate therefore records `sourceDirty=true`; unrelated dirty
paths were preserved and were not used as passing evidence.

## Delivered boundary

- Control Plane production startup accepts complete database, auth, service
  TLS and access-Grant inputs without any Managed Agent Worker configuration.
  Partial Worker/Workspace/admission configuration still fails closed.
- In no-Agent mode, User Environment creation and Managed Agent execution
  routes are not mounted. Admin inventory and no-Agent Foundation authority
  remain available.
- Compose starts PostgreSQL, Control Plane, Access Gateway, User Web and Admin
  Web by default. The legacy Worker and its Provider/admission inputs require
  the explicit `docker-compose.managed-agent.yml` override.
- Helm defaults to `worker.enabled=false`; the Worker Deployment/Service,
  workspace PVC, Worker NetworkPolicy and Control Plane Worker/Provider mounts
  render only when explicitly enabled.
- The release package contains both the no-Agent default and the opt-in legacy
  compatibility override. No new framework or deployment abstraction was
  introduced.

## Reproducible checks

Focused source and package checks:

```sh
go test ./services/control-plane/cmd/cloud-agents-control-plane
bun test scripts/lib/platform-release.test.ts
sh scripts/test-cloud-agents-helm.sh
helm lint deploy/helm/cloud-agents
docker compose --env-file deploy/compose/.env.example \
  -f deploy/compose/docker-compose.yml config
docker compose --env-file deploy/compose/.env.example \
  -f deploy/compose/docker-compose.yml \
  -f deploy/compose/docker-compose.managed-agent.yml config --quiet
```

All passed. Default rendered Compose and Helm output was also scanned and did
not contain a Worker service/component,
`CLOUD_AGENTS_PLATFORM_WORKER`,
`CLOUD_AGENTS_PLATFORM_ADMISSION`, Provider credential mount, or Runtime
workspace mount. The explicit compatibility render contained those existing
Worker inputs.

The final local candidate was generated and checked with:

```sh
bun scripts/cloud-agents-platform-release.ts \
  --version 0.3.0-dev.103 \
  --output-dir /tmp/cloud-agents-base-no-agent-20260909-r3/release \
  --allow-dirty
(cd /tmp/cloud-agents-base-no-agent-20260909-r3/release && \
  shasum -a 256 -c checksums.sha256)
sh scripts/test-platform-compose.sh \
  /tmp/cloud-agents-base-no-agent-20260909-r3/release
CLOUD_AGENTS_HELM_CONTEXT=orbstack \
  sh /tmp/cloud-agents-base-no-agent-20260909-r3/deployment/scripts/test-platform-helm.sh \
  /tmp/cloud-agents-base-no-agent-20260909-r3/release
```

Candidate facts:

- 22/22 artifact checksums passed.
- Manifest SHA-256:
  `95921d27ccf7f005311e54d38eed5a71331114ead6ce9521b88f3e544b72e892`.
- Deployment tar SHA-256:
  `baacb1c6fea8b2d693a633f8a06093ec659e45c0d1b638ec585a9d884a86807c`.
- Linux amd64 RemoteWorker SHA-256:
  `c680a5af59c493f9587ded1364497769bbb767c35ede0653fc92ed492a6a3bab`.

The Compose run first started the five default no-Agent services from an env
file with all Worker, Workspace, Runtime environment, Provider credential and
admission values removed. Control Plane `/readyz`, User/Admin application
assets and absence of the Worker were checked before the test explicitly added
the compatibility override. The existing browser, Target/Profile/Environment,
Gateway Files/PTY/Preview/SSH, lifecycle, backup/restore and cleanup regression
then passed:

```text
platform Compose smoke passed (linux/arm64, darwin-arm64, profile=compose-docker-profile:v1, environment=environment-0cc46b1599b67da3345de72ee27f3312, user-web=same-origin, admin-web=browser, gateway=files+pty+preview+ssh, docker-workers=0, opensandbox-resources=0)
```

The Helm run used the script extracted from the final deployment tar. It first
ran the existing Worker-enabled compatibility, User/Admin browser,
RemoteWorker, leaf/CA rotation and Gateway checks. It then upgraded with only
`worker.enabled=false`, removed the five Worker/admission/Provider Secrets,
restarted Control Plane, verified the existing Project through the User API,
and removed the labelled namespace and PVs:

```text
Helm User/Admin Web/RemoteWorker smoke passed (context=orbstack, schema=88:000001-000088, user-web=same-origin, user-admin=403, customer-node=outbound, identity=leaf+ca-rotated+old-root-rejected, service-identity=ca-rotated+old-rejected, gateway=TLS+SSH, upgrade=not-requested, no-agent=worker+provider-secrets-absent, restart=passed, cleanup=zero)
```

Post-run checks found no namespace with the test-run label and no labelled
test containers.

## Failed attempts and evidence boundary

- The first Compose regression reached the compatibility browser check but a
  host HTTP proxy returned a previous User Web index for Docker's
  `0.0.0.0:<ephemeral-port>` result. Candidate and container files had the new
  hash. The no-Agent asset check now bypasses proxies explicitly; the final
  candidate passed.
- The first added Helm no-Agent transition reset rotated TLS values by applying
  chart defaults. It failed the Control Plane TLS check. The final transition
  reuses the deployed values and changes only `worker.enabled=false`; two
  subsequent full Helm runs passed, including the final packaged script.
- `go test ./services/control-plane/...` is not claimed as passed. The unrelated
  dirty migration work failed existing evidence quota/source-spread assertions
  and timed out at ten minutes. The changed production command package passed
  its focused test.

This evidence proves the packaged default no-Agent installation and local
Compose/OrbStack Helm runtime boundary. It does not prove an external customer
node, external PostgreSQL/OIDC/ingress, an N-1 upgrade in this run, or Provider
conversation execution. No image was pushed and no Release was published.
