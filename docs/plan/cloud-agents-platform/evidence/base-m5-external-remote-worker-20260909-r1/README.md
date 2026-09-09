# External outbound customer-node acceptance (2026-09-09)

This is a real two-host BASE-M5 run. It is not a Probe, mock, static page or
release approval. The user explicitly authorized Docker installation,
temporary deployment and exact cleanup on these SSH aliases:

- `hostdzire-4c6g`: PostgreSQL 17.6, Control Plane, Access Gateway, User Web
  and Admin Web through the packaged Compose deployment;
- `tianliyun-2c2g`: Docker, fixed-digest OpenSandbox and the outbound
  RemoteWorker systemd service.

Both hosts were Debian 13 x86_64. Docker `26.1.5+dfsg1` and Compose `2.26.1`
were installed from Debian packages. Docker remains installed and enabled by
authorization; all test-owned services, files, images, containers, volumes,
networks and listeners were removed.

## Source and candidate

The final unpublished candidate was built from branch
`codex/cloud-agents-platform-p0` at
`ff715138f9b3443e01684c16e9ac08192b305e8d`:

| Fact | Value |
| --- | --- |
| Version | `0.3.0-dev.105` |
| Candidate directory | `/tmp/cloud-agents-base-external-20260909-r2` |
| Manifest SHA-256 | `fd7b34ce80e8e6b0e4e9ae16ffae9e9f6dd8e6bf41068b4859524f032c4bafe5` |
| Deployment tar SHA-256 | `c07b1c34ef89b6dbf4c6b8aa15b67bcccac002e61ae029f49fcded84a7717ef6` |
| Migration tar SHA-256 | `379b060d644b1ce35c9b9a8e2918241cfeca74fbd762d8ad3648fe49dda0b877` |
| Control Plane Linux amd64 SHA-256 | `b0f4ca201188bfaf4d1bdcb268eb1b287c825dbd68699554e6c272846f66d7ac` |
| RemoteWorker Linux amd64 SHA-256 | `a0961e838471c7b6caabc255a8c82b09af25e2fbc7adb394ccc88986e43b3a57` |
| CLI Darwin arm64 SHA-256 | `77256de79b40e38ee635743c27641d37b7d66243e589ecaa68b6e4b6e934993f` |
| Product migration head | `000088` |
| Checksum result | 22/22 passed |

The manifest records `sourceDirty=true`. The unrelated dirty paths were
`.gitignore`, `CLAUDE.md`,
`docs/plan/cloud-agents-platform/04-extraction-and-migration.md`, the lower
pre-existing hunk in `06-status-tracker.md`, `go.work.sum`, `AGENTS.md` and
`docs/img.png`; none was included in the source fix or staged as this slice.

The fixed runtime images were:

- OpenSandbox server
  `sandbox-registry.cn-zhangjiakou.cr.aliyuncs.com/opensandbox/server@sha256:8f8762af7565ed9c6f9dbcf009dd56727aa1fef8ce58a17f2b007b88cfe542bb`;
- execd
  `sandbox-registry.cn-zhangjiakou.cr.aliyuncs.com/opensandbox/execd@sha256:1dc98c7de10b9a73450ac75aa0f200ad7972f2c40f5225f6a8998e166b45d6dd`;
- egress
  `sandbox-registry.cn-zhangjiakou.cr.aliyuncs.com/opensandbox/egress@sha256:973130e01bf76e8e686e2853ebf47b21741bc8781919bb4a7cf60af09a3c6e8a`;
- Sandbox workload
  `sha256:83f487e0a63425e5b4d146fb5e5be574bcbe1b7b843d3ebafdd95eaf7767a7e5`.

## Real authority and topology

The Control Plane was reachable only through its Admin/User APIs. Browser and
CLI traffic did not connect to customer Docker or OpenSandbox. On the customer
node Docker mTLS listened on `127.0.0.1:2376`, OpenSandbox on
`127.0.0.1:18080`, and RemoteWorker made the outbound mTLS connection to the
Control Plane. The final OpenSandbox settings separated advertised and
container-reachable addresses: `server.eip=127.0.0.1` and
`docker.host_ip=172.17.0.1`.

The server-authoritative resources were:

| Resource | Value |
| --- | --- |
| Tenant / project | `tenant-external-smoke` / `project-567b03b5922a540e745b1bc9094432f2` |
| Enrollment / incarnation | `external-node-r1` / `external-node-r1-incarnation-3` |
| RemoteWorker Target | `rwt-3781d0f90c274b39416251de824c5450507ef7180caed44afe7d9723ee962daa` |
| Worker version and capabilities | `0.3.0-dev.105`; Docker, Exec, Files, PTY, Preview, SSH, workspace-volume and network-dns-nft |
| Reported capacity | 1500 mCPU, 1,342,177,280 bytes memory, 21,474,836,480 bytes disk |
| Profile | `external-node-profile` version 1, published |
| Network policy | `network-external`, deny-only, enforced while running |
| Sandbox / Workspace | `external-sandbox-r1` / `external-workspace-r1` |
| Physical Workspace volume | `ca-ws-d5e43005d836ee81c23a40fb107f9a6cb63147172c3b42d5b29db1a9` |

The 20 GiB disk fact is the exact underlying `/dev/sda` byte size; the mounted
root filesystem is necessarily slightly smaller. This met the fixed 20 GiB
Workspace admission boundary without rounding the filesystem's available
bytes upward.

## Executed acceptance

1. The packaged PostgreSQL migration, default no-Agent Control Plane, Access
   Gateway, User Web and Admin Web started on `hostdzire-4c6g`. The running
   Control Plane reported `0.3.0-dev.105`.
2. The enrollment secret was claimed once, a short-lived node certificate was
   issued, and `tianliyun-2c2g` reported online with eight capabilities. A
   user-scope token with the Admin audience received HTTP 403 from the real
   enrollment Admin endpoint.
3. Admin published `external-node-profile` version 1. User admission submitted
   only the Profile ID/version; Control Plane selected the RemoteWorker Target,
   fixed digest and policy.
4. Sandbox create reached `running`, generation 1, with
   `networkPolicyEnforcement=enforced`. User Exec wrote
   `/workspace/external-proof.txt`; the 31-byte content SHA-256 was
   `ee4c8236e71f9c271f5d24e0ca2363c162bd496ad5759002557029994237930c`.
5. TTL expiry performed a real Stop from generation 1 to 2. Runtime and egress
   containers were deleted, writer authority was released, network enforcement
   became `stopped`, and the physical Workspace volume remained.
6. Admin Rebuild used expected generation 2 and resourceVersion 7. It converged
   at generation 3 with a new runtime ID and the same physical volume. Gateway
   Files read the Exec-written file with the same SHA-256, wrote/read a second
   26-byte file, PTY observed `/workspace`, and a real short-lived SSH Grant
   returned `/workspace` plus the expected marker.
7. Stopping the RemoteWorker systemd service caused Admin health to become
   `offline` after its heartbeat lease expired. The running Sandbox, egress and
   physical volume remained. Restarting the service restored `online`, after
   which Gateway Files again returned generation 3 and the same 31-byte SHA-256.
8. The server preview supplied both impact digests and fences. Drain advanced
   node generation 1 to 2 and observed `drained`; Resume advanced 2 to 3 and
   observed `active`. The running Sandbox and volume remained through Drain.
   Both Maintenance Operations reached `succeeded`, and both actions were
   present in the enrollment Audit stream.
9. Final Admin Stop used current generation/resourceVersion and converged at
   generation 4. Runtime and egress were absent while the retained Workspace
   volume was still present before the separately authorized cleanup.

The external run scanned 18 saved Admin responses and 762 scalar values
against all run-owned JWTs, bootstrap/grant/API/database secrets and forbidden
content/authority keys. It found zero Secret occurrences and zero
`accessToken`, endpoint, credential reference, kubeconfig, Prompt,
Workspace-content or Artifact-content fields. This scan does not inspect user
file responses, which intentionally contain the bounded file bytes granted to
that user.

## Failure and recovery accounting

The first Sandbox attempt returned `foundation_runtime_unavailable`. Its
receipt legitimately recorded the newly created physical Workspace volume.
The next claim then included that volume in `sandbox.create`, although the wire
contract forbids a physical volume on create. Heartbeat claimed the attempt and
returned `ErrCoordinationResultDrift`, so no retry could execute. Commit
`ff715138` fixes the shared RemoteWorker command assembly: create omits the
known physical volume, while stop/rebuild retain it. The focused regression and
the complete PostgreSQL store package passed, and candidate `.105` was rebuilt
and deployed.

After that fix, attempts 2 through 5 were delivered and failed visibly while
the OpenSandbox address roles were corrected. Using `172.17.0.1` as the
advertised endpoint failed the RemoteWorker endpoint check; using
`127.0.0.1` as Docker `host_ip` made the OpenSandbox container unable to reach
host-published runtime ports. Attempt 6 succeeded with the split settings above.
No failed attempt was hidden or counted as passing.

Two operator setup errors were also detected rather than accepted as evidence:
the initial tenant had not run the packaged bootstrap profile, and one Control
Plane binary was first copied outside the Compose build context. The bootstrap
profile was run, the binary was copied into the actual release context, the
image was rebuilt without cache, and the live container version was rechecked.

Relevant local checks:

```sh
go test ./services/control-plane/internal/store/postgres \
  -run '^TestRemoteWorkerSandboxCreateRetryOmitsPhysicalVolume$' -count=1
go test ./services/control-plane/internal/store/postgres -count=1
(cd /tmp/cloud-agents-base-external-20260909-r2 && \
  shasum -a 256 -c checksums.sha256)
git diff --check
```

## Exact cleanup result

Cleanup used the exact Compose project
`cloud-agents-external-20260909-r1`, exact service/container names, the
authoritative physical Workspace volume ID, the run-owned OpenSandbox runtime
volume, and these exact directories:

- `/srv/cloud-agents-base-external-20260909-r1`;
- `/srv/cloud-agents-remote-worker-external-20260909-r1`;
- `/etc/systemd/system/cloud-agents-remote-worker-external.service`;
- `/etc/systemd/system/docker.service.d/cloud-agents-external.conf`.

Independent final checks reported:

- Control Plane host: zero task containers, volumes, networks and images, and
  zero BuildKit cache; ports 8443, 8090, 4173, 4174 and 2222 closed; run
  directory absent.
- Customer node: zero containers, volumes and images; only Docker's `bridge`,
  `host` and `none` networks; ports 2376 and 18080 closed; unit, drop-in and run
  directory absent.
- Local Access Gateway SSH tunnel: closed.

No firewall rule was added, no image was pushed, and no Release was created.
This run does not re-execute the already recorded Kubernetes, snapshot,
N/N-1, service/node certificate rotation or Admin browser visual matrices. It
closes the external outbound customer-node gap when combined with those
current-source evidence sets; it does not close the archived ADMIN-WEB-V1 or
APP-M1 Codex/Claude product acceptance.
