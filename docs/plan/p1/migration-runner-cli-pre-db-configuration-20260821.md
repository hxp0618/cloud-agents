# P1 migration runner/CLI pre-DB configuration - 2026-08-21

- Status: **IMPLEMENTED CANDIDATE - INDEPENDENT REVIEW PENDING**
- Baseline commit: `c8a71f2499266b35d09ec08ba0122b8efb510fd8`
- Branch: `codex/cloud-agents-platform-p1`
- Authority: [`ADR-0009`](../adr/0009-p1-migration-bundle-runner.md), §7 of
  [`migration-production-evidence-sink-20260817.md`](./migration-production-evidence-sink-20260817.md)
- This record does not authorize HTTP routes, P2/provider or worker behavior, production database reads or writes,
  deployment, publication, release, or closure of any immutable or aggregate Gate.

## Bounded configuration slice

`services/control-plane/cmd/cloud-agents-migrate` now has an explicit fail-closed configuration seam. The CLI requires
`--artifact`, `--repository`, `--release`, `--evidence-root`, and the
`CLOUD_AGENTS_PLATFORM_DATABASE_URL` environment variable. It rejects positional or unknown arguments and snapshots
the database locator once before constructing the runner. The evidence root is passed through
`migration.NewEvidenceSink`, which validates only a canonical absolute non-root locator and does not touch the
filesystem.

The configured production composition uses `migration.RejectingTrustVerifier`, `NewEvidenceSink`, the existing pgx/SQL
adapters, and fail-closed projection validators. The verifier intentionally rejects because detached signature,
epoch, revocation, and deployment trust-root wiring are not yet available in the production CLI. Therefore the CLI
returns at `StateVerifyTrust` before reading the artifact, opening evidencefs authority, or connecting to PostgreSQL.
The configuration seam is present without turning the runner into an executable production migration path.

## Evidence and tests

The command package tests cover:

- incomplete, positional, and unknown configuration with stable non-secret errors;
- a complete configured invocation with missing artifact/evidence paths, proving trust rejection occurs before either
  path is touched and before any database connection attempt; and
- invalid evidence-root rejection without filesystem access.

The existing migration order tests continue to cover trust-before-artifact and unconfigured-evidence zero-side-effect
behavior. Local checks passed for normal and race tests, vet, build, Linux `amd64`/`arm64` CGO-free test compilation,
Linux `amd64`/`arm64` CGO-free builds, formatting, and `git diff --check`.

The local Go host was `go1.26.7`, while `go.mod` retains the exact `go1.26.6` toolchain directive. This record does not
misrepresent the ambient-host run as a pinned-toolchain replay; no module or dependency bytes changed.

These are bounded local checks. Full migration closure, production trust-root provisioning, positive evidencefs
authority, database integration, PostgreSQL/cloud matrices, deployment, release, vulnerability closure, and immutable
Gate signatures remain unverified or explicitly out of scope.

## Next boundary

The next authorized slice is independent review of this configuration/pre-DB composition. A later database slice may
only begin after its own contract/state-machine and review evidence. Until then, the CLI must remain trust-rejecting;
no test-only accept-any verifier may be compiled into this command, and no production database write, deployment,
publication, or Gate transition is implied by this record.
