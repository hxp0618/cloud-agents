# M1 Target registration commit conflicts — 2026-09-05

Base HEAD `1e9f8cf`, branch `codex/cloud-agents-platform-p0`.

## Cause and correction

Concurrent real Target registrations exposed PostgreSQL SERIALIZABLE rejection
at both the statement and commit boundaries. Raw SQLSTATE `40001` already
mapped through `mapCoordinationDatabaseError` to `ErrCoordinationRejected` and
HTTP 409 `TARGET_CONFLICT`. At commit, the shared transaction runner first
normalizes it to `ErrMutationConflict`; the downstream coordination mapper did
not recognize that sentinel, producing HTTP 500 instead.

The four-line shared mapper correction preserves confirmed rejection for all
its callers, including Target, Profile, Lease, policies, quota and release
authority. It does not change transaction isolation, rerun callbacks, or
automatically retry external operations. Unknown commit acknowledgement still
takes precedence and remains unknown, never a confirmed rejection. No API
contract/SDK shape or Web code change is necessary: 409 already exists.

## Red/green evidence

`TestCoordinationCommitConflictPreservesRejection` first failed with:

```text
40001 rejection mapped to operation: postgres RBAC mutation compare-and-swap conflict: commit tenant mutation transaction
```

It now covers raw and normalized `40001`/`23505`, shared and Target mapping,
unknown-commit precedence, and unrelated-error preservation.

The checked-in real API regression `scripts/test-admin-target-registration.ts`
requires a fresh disposable project, refuses existing Targets, and registers
four batches of ten Targets using the generated Admin SDK. Endpoints and
opaque references are deliberately unconfigured; the script never Probes or
deploys. It requires an observed conflict, rejects any status except 201/409,
then retries using the **original** idempotency key, replays again and checks
one successful Operation and Audit per Target. It reports metadata only.

| Run | Project | 201 | 409 | 500 | Original-key recovery/replay |
| --- | --- | ---: | ---: | ---: | --- |
| Before fix | `project-3e915d4a926f79def8c3a846f9c90d1a` | 6 | 29 | 5 | Correctly failed before replay |
| After fix | `project-a3819aa42c7dd6c4144a5490ee09c8a9` | 6 | 34 | 0 | 40 Targets, one Operation/Audit each |
| Repeat | `project-c8e8706456a79c131cb08a440800cba0` | 4 | 36 | 0 | 40 Targets, one Operation/Audit each |

Both stacks used real PostgreSQL 17.6 with migrations through 51, rebuilt
Control Plane on `127.0.0.1:18085`, local Worker on `127.0.0.1:18095`, and
independent temporary Admin tokens. The first fixed run's PostgreSQL logs
included an actual `STATEMENT: commit` serialization rejection, not only
statement-level conflicts. Counts are scheduling-dependent, not a throughput
benchmark. The fix classifies failures correctly; it does not remove them.

Reproduce after starting `scripts/cloud-agents-dev.sh` and creating a new
project through its CLI:

```sh
bun scripts/test-admin-target-registration.ts \
  http://127.0.0.1:18085 /path/to/control-plane-admin.token \
  tenant-local NEW_DISPOSABLE_PROJECT_ID
```

Use a new project for each run. Stop the disposable dev stack after evidence
collection; do not run cleanup against shared/existing infrastructure.

## Other checks and cleanup

- `go test ./internal/store/postgres ./internal/server`: passed.
- Same packages with `-race`: passed; `go vet`: passed.
- Script strict TypeScript check with ES2023/bundler resolution, scoped
  oxfmt/oxlint and `git diff --check`: passed.
- Go checks used pinned Go 1.26.6, `GOTOOLCHAIN=local`, `GOFLAGS=-mod=readonly`.
- Disposable containers `cloud-agents-dev-501-36362` and
  `cloud-agents-dev-501-46951`, CP/Worker processes, and state directories
  `.tmp/cloud-agents-dev.UcI2yy` / `.tmp/cloud-agents-dev.FYBAcQ` were removed by
  their owning dev-script shutdown. Ports 18085/18095/4174 have no listeners.
  This also removed the preceding pagination slice's 205 registrations and
  all concurrency fixture databases/tokens. No existing resource was touched;
  these disposable test databases were not backed up.

Boundary: real Admin API/PostgreSQL registration and replay only. No transport
Probe, Worker deployment, Provider Turn, or whole-product acceptance claimed.
Goal remains active: M1 full visual/interaction acceptance and subsequent
M2–M4 evidence are not closed by this correction.
