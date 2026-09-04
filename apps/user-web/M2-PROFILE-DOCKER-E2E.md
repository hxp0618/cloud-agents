# M2 Published Profile to Real Docker Worker E2E

Date: 2026-09-04

Status: passed for the packaged Profile-to-Docker-Worker slice. This does not complete M2 or the Section 15 acceptance matrix.

## Build under test

- Branch: `codex/cloud-agents-platform-p0`, starting HEAD `0ba4b802d1686b488e4f58a74512f5c4a6df820a`.
- Release candidate: `0.0.0-m2-profile-docker.1`, with `sourceDirty=true` because the smoke-harness change and unrelated pre-existing work were intentionally uncommitted while the candidate was built.
- Release manifest SHA-256: `e59374aa720d2bb1eeea62f15a25da3733624effeb32474448616bbe528f3a0c`.
- Packaged deployment: `cloud-agents-deployment-000043.tar`, SHA-256 `a53e11144ad312732ab5120e30bec90827c3b112a82c821df8e14e72da5d75e0`.
- Runtime host: OrbStack Docker context on Apple Silicon; packaged services ran as `linux/arm64`, and the CLI ran as `darwin-arm64`.
- Locked validation toolchain: Go `1.26.6`, Node.js `24.18.1`, and Bun `1.3.14`.

The isolated candidate and smoke were run with:

```sh
run_root=$(mktemp -d "$PWD/.tmp/m2-profile-docker.XXXXXX")
candidate="$run_root/release"
PATH="$PWD/.tmp/toolchains/bun-1.3.14/bun-darwin-aarch64:$PWD/.tmp/toolchains/node-v24.18.1-darwin-arm64/bin:$PATH" \
  GOTOOLCHAIN=go1.26.6 bun scripts/cloud-agents-platform-release.ts \
  --version 0.0.0-m2-profile-docker.1 --output-dir "$candidate" --allow-dirty
PATH="$PWD/.tmp/toolchains/node-v24.18.1-darwin-arm64/bin:$PATH" \
  sh scripts/test-platform-compose.sh "$candidate"
```

## Real authority and deployment path

The packaged PostgreSQL, migrations, Control Plane, Worker image, generated CLI, Docker mTLS proxy, and local OCI registry were used. No Profile, Target, Lease, Session, Turn, or Worker was inserted as a fixture.

The smoke verifies this sequence and fails immediately if any assertion changes:

1. An Admin token registers and probes the real Docker Target to `ready`.
2. Admin API creates `compose-docker-profile:v1` as `draft`, then publishes resource version `1` to `published` resource version `2`.
3. A separately issued ordinary User token receives HTTP `403` with `AUTHORIZATION_DENIED` from the Profile Admin API.
4. The User token lists the published Profile. Its summary has only identity, description, availability, Provider kinds, and read-only CPU/memory summary fields; it has no Target, endpoint, release digest, storage/network policy reference, or credential reference.
5. The User token sends exactly `{"profileId":"compose-docker-profile","profileVersion":1}` to the User Environment API. The response contains only the safe `UserEnvironment` projection.
6. Control Plane privately resolves the published Profile to `docker-compose-target`, the fixed Worker release digest, resource limits, and the opaque Provider credential-volume reference.
7. The target contains exactly one running label-scoped Docker Worker. Replaying the same User request and idempotency key returns the identical environment without creating another Worker.

The resulting opaque environment identity was:

```text
environment-0cc46b1599b67da3345de72ee27f3312
```

## Ordinary User Session and durability path

- The ordinary User token, not the Admin token, creates Profile-bound Codex and Claude Code Sessions and one durable queued Turn for each Provider.
- Each Session response is checked against the opaque environment identity and immutable `compose-docker-profile:v1` binding.
- The Codex Execution intentionally runs without a Codex credential file. It reaches the real target Worker, fails closed as `runtime_open_failed`, persists the terminal failure, and emits the durable `execution.fail` event.
- Restarting Control Plane preserves the ready User Environment projection and failed Execution.
- The Profile-created Lease terminates with generation `2`, `observedPhase=terminated`, and `cleanupPhase=complete`; idempotent replay returns the same result.
- Target cleanup succeeds and the exact environment's label-scoped Worker count becomes zero.
- A PostgreSQL custom-format backup is restored into a fresh database. Repeated restore fails closed, and the durable Execution remains queryable with the User token.

Final successful output:

```text
platform Compose smoke passed (linux/arm64, darwin-arm64, profile=compose-docker-profile:v1, environment=environment-0cc46b1599b67da3345de72ee27f3312, docker-workers=0)
```

The final smoke log SHA-256 was `34963d7e1ec2eba3f5f538d46a182eee3cd8a7eea913973753fe39899212fdb8`.

## Source checks

```text
sh -n scripts/test-platform-compose.sh                                      PASS
git diff --check                                                           PASS
bun run fmt:check                                                          PASS, 176 files
bun run --cwd apps/user-web typecheck/test/build                           PASS, 21 tests
bun run --cwd sdk/typescript typecheck/test/build                          PASS, 33 tests
bun test scripts/lib/platform-release.test.ts scripts/lib/platform-migration-sql.test.ts PASS, 46 tests
bun run typecheck/lint/secret:scan                                         PASS
bun run platform:contracts:check                                           PASS, 122 schemas and 66 operations
bun run platform:sdk:consumers                                             PASS with fresh TypeScript and Go consumers
go test ./services/control-plane/internal/environmentprofile \
  ./services/control-plane/internal/managedhost \
  ./services/control-plane/internal/server \
  ./services/control-plane/internal/store/postgres                         PASS
scripts/cloud-agents-platform-release.ts --allow-dirty                     PASS, 18 artifacts
scripts/test-platform-compose.sh <candidate>                               PASS
```

One retry failed before product startup while BuildKit received an EOF resolving `gcr.io/distroless/static-debian12:nonroot`. Its trap removed the isolated resources; the subsequent unchanged run passed.

## Teardown and evidence boundary

- After the passing run, the exact Compose project had no containers or volumes, no Worker with tenant `tenant-compose-smoke` remained, and the temporary Docker proxy and Kubernetes fixture processes were absent.
- The candidate directory was moved to `/Users/huang/.Trash/cloud-agents-m2-profile-docker.iFl7OV-20260904` after its manifest and final log hashes were recorded, so it remains recoverable.
- This proves a published Profile creating a real Docker Agent environment, ordinary-user scope separation, safe User API projections, real Profile-bound Codex and Claude Code Session/Turn persistence, Control Plane restart recovery, termination, cleanup, and backup/restore.
- No external Provider credentials were supplied. Therefore the optional real Codex and Claude Code Provider Executions, approval/user-input interaction, and generated Artifact checks were skipped; this run must not be cited as a successful Provider Turn.
- Real Kubernetes and SSH Worker deployment/cleanup, M3 maintenance operations, and the remaining M4/Section 15 matrix are still unverified.
