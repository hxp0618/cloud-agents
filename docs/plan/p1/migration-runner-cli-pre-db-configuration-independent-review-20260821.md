# P1 migration runner/CLI pre-DB configuration independent review - 2026-08-21

- Status: **APPROVE bounded implementation/review slice - P0=0 / P1=0 / P2=0**
- Fixed candidate: `a5df1cb672d7636e551b9fb24b74c9cc5e047ba6`
- Candidate branch: `codex/cloud-agents-platform-p1`
- Review branch: `codex/cloud-agents-p1-runner-cli-review-20260821`
- Review mode: independent, read-only focused review of the fixed candidate
- Candidate record: [`migration-runner-cli-pre-db-configuration-20260821.md`](./migration-runner-cli-pre-db-configuration-20260821.md)
- This record does not authorize HTTP routes, P2/provider or worker behavior, positive production trust, production
  database reads or writes, deployment, publication, release, or closure of any immutable or aggregate Gate.

## 1. Verdict

The bounded CLI configuration slice is approved for the exact candidate bits. The production composition remains
intentionally trust-rejecting, so this approval records the ordering and fail-closed boundary; it does not turn the
runner into a runnable migration path.

| Boundary | Verdict | Evidence |
| --- | --- | --- |
| explicit configuration | PASS | `--artifact`, `--repository`, `--release`, `--evidence-root`, and the database environment variable are required; parse errors, unknown/positional arguments, and empty values return a stable non-secret error |
| trust-before-I/O ordering | PASS | `Runner.Run` enters `StateVerifyTrust` and returns the `RejectingTrustVerifier` error before artifact read, evidencefs opening, or PostgreSQL preparation; the command test checks missing artifact/evidence paths remain untouched |
| production authority boundary | PASS | the command wires only `RejectingTrustVerifier`, existing adapters, and fail-closed projection validators; test-only accept-any verifiers are not referenced or compiled into the command |
| evidence locator | PASS | `NewEvidenceSink` validates only a canonical absolute non-root locator and performs no filesystem access; invalid locators fail before runner construction |
| secret/error handling | PASS | the command tests reject a DSN containing a secret without returning that value; `main` emits only the returned stable error |
| forbidden external surfaces | PASS | no route registration, HTTP server, provider/P2/worker call, production write, deployment, publication, or release surface was added |
| immutable and aggregate Gates | OPEN | this is bounded local independent-review evidence and closes no Gate |

## 2. Fixed reviewed artifacts

The review used the exact candidate commit and verified the following candidate source/document hashes before this
review branch added its index-only record:

- `services/control-plane/cmd/cloud-agents-migrate/main.go`:
  `34689bcba89eac7bd1e1116bdd658d97f8b25b4a5a00496e0c0f8ef6e4c73cb1`;
- `services/control-plane/cmd/cloud-agents-migrate/main_test.go`:
  `3dc58372ade0b53f78039a18f86dd89bf536efe88fe037c82be5c886e3a53a38`;
- [`migration-runner-cli-pre-db-configuration-20260821.md`](./migration-runner-cli-pre-db-configuration-20260821.md):
  `f26e404ccc70506ed03b6562a23d813dcd963ce6480c2681520051a65f2d6a39`;
- [`README.md`](./README.md):
  `16decd0b0a9d27732bffbe7c37b923777c0d26858894bba9a83b50160f50dbcf`.

## 3. Review evidence and limitations

Focused checks passed from the `services/control-plane` module with `GOWORK=off GOFLAGS=-mod=readonly`:

- `go test ./cmd/cloud-agents-migrate -count=1`;
- `go test -race ./cmd/cloud-agents-migrate -count=1`;
- `go test ./internal/migration -run '^(TestRejectingTrustHappensBeforeArtifactRead|TestRunnerRejectsMalformedAuthorityArtifactBeforeConnect)$' -count=1`;
- the same two migration tests with `-race`;
- `go vet ./...`, `go build ./...`, and `git diff --check`;
- CGO-free Linux `amd64` and `arm64` command-package test compilation via `/usr/bin/true`.

Static review confirmed the command's production Go files contain no HTTP server/route, direct database connection,
evidencefs opening, provider, worker, or accept-any trust construction. The command snapshots the database locator
before parsing and never includes it in its stable errors.

The review host used Go `1.26.7`, while the module pins exact toolchain `1.26.6`; therefore no pinned-toolchain replay
is claimed. The known broad migration-suite timeout remains neither a pass nor a failure of this bounded slice, and
no PostgreSQL/cloud matrix, positive trust-root provisioning, production mTLS/deployment test, vulnerability closure,
artifact publication, or Gate signature was rerun or implied.

## 4. Boundary after approval

This record closes only the owner-approved pre-DB CLI configuration and trust-before-I/O implementation/review slice.
The CLI must continue to reject at `StateVerifyTrust` until detached signature, epoch, revocation, and deployment
trust-root wiring receive their own contract/state-machine and review evidence. A later database slice requires a
separate approved boundary; this approval does not authorize production database access, writes, deployment,
publication, release, or any Gate transition.
