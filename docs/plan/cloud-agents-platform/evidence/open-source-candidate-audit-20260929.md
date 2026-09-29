# Open-source candidate audit — 2026-09-29

This is a merge-readiness record for the current checkout. It does not close or reopen a formal runtime Gate.

## Candidate identity

| Item | Result |
| --- | --- |
| Branch | `codex/cloud-agents-platform-p0` |
| HEAD | `d80d825fb622dd8b538751ef0f0f880b82701a59` (`refactor: streamline admin and pagination helpers`) |
| `dev` ref | `NOT FOUND`; comparison uses local `main` only |
| `main` | `49e8cdc6a3a4f88c7324d055ce519e9f25a8ca8a`; branch is `0` behind and `1097` commits ahead |
| Index | clean; no staged entries |
| Worktree | `73` modified, `332` deleted, `96` untracked paths |
| Dirty diff | `405` files, `962` insertions, `2,509,812` deletions |
| Current source | dirty; not a reproducible release candidate |

The r741 closeout recorded in `06-status-tracker.md` used an earlier dirty candidate at source commit `29afe9b9103cac99088f637e02a8cd9bb3f48d58`. Its results remain historical evidence and are not inherited by this worktree.

## Inventory

| Class | Current disposition |
| --- | --- |
| Production code, contracts, SDKs, Admin, deploy files | retain; affected by the existing dirty worktree |
| Migration/generator authority | retain; `internal/migrationcore`, catalog patch files, and generator changes are untracked or modified and require one coherent review |
| Contract, security, migration, recovery and compatibility tests | retain when still referenced; do not accept the existing deletions until the replacement coverage is reviewed |
| Reproducible acceptance scripts | retain, including `scripts/test-platform-compose.sh`, `scripts/test-platform-agent-interactions.sh`, and the platform check scripts |
| Evidence summaries | retain `06-status-tracker.md`, `evidence/README.md`, and this audit; historical raw evidence stays non-canonical |
| One-off tests and superseded generated checks | pending review; the existing dirty diff deletes `228` `internal/migration` paths, `87` full schema snapshots, and `6` script test/generator paths, while adding `87` patch files |
| Raw logs, screenshots and process records | not publication-ready; the tree still contains `25` evidence logs, `37` screenshots, and many dated E2E/process reports |
| Credentials and ignored runtime state | no credential files are part of the candidate; `provider-credentials/`, `.tmp/`, and generated runtime state remain ignored |

No file was reset, cleaned, staged, committed, moved, or deleted by this audit. Existing dirty deletions and additions are preserved for their owner to review.

The audit itself made only these scoped edits: removed machine-specific proxy addresses and an internal SSH alias from `CLAUDE.md`; separated historical r741 status from the current dirty candidate in `06-status-tracker.md`; linked this summary from `evidence/README.md`; removed the extra closing brace in the shared generator and guarded an absent recovery error-file path in `scripts/test-platform-compose.sh` so the diagnostic JSON remains parseable.

## Runtime evidence boundary

The historical r741 record reports the following evidence candidate as `PASS`:

| Provider | Docker | outbound RemoteWorker | Kubernetes |
| --- | --- | --- | --- |
| Codex | `PASS` | `PASS` | `PASS` |
| Claude Code | `PASS` | `PASS` | `PASS` |
| Pi | `PASS` | `PASS` | `PASS` |
| deepseek-harness | `PASS` | `PASS` | `PASS` |

For the current dirty source, all twelve cells are `NOT RUN`. The same applies to the current merge candidate's transport/reconnect, recovery, cross-node, Worker/Agent fault and secret-boundary claims. The historical record says the aggregate/release/feature Gate was `CLOSED / APPROVED`; that closure is scoped to the historical candidate and does not certify this checkout.

The historical record includes command, cleanup and digest details, but the referenced `.tmp` logs and evidence JSON are ignored runtime artifacts rather than public files. They cannot be used as the only merge evidence for this candidate.

## Public-content audit

The current tree still has private/local markers in `104` lines across `48` files, including historical proxy addresses, local filesystem paths, internal Kubernetes hostnames and third-party endpoints. `CLAUDE.md` now contains only the generic rule to keep proxy settings process-local. Remaining historical/generated files must be redacted through their source or explicitly moved out of the public tree before publication; generated outputs must be regenerated rather than edited in place.

## Merge checks

| Check | Current result |
| --- | --- |
| `git diff --check` | `PASS` |
| Secret scan | `PASS` (`bun run secret:scan`) |
| Format/lint | `PASS`: `bun run fmt:check`, `bun run lint`, `sh -n scripts/test-platform-compose.sh` |
| Go format/type/build/test/vet | `PASS`: `platform:go:check` and `scripts/test-platform-go-products.sh` (normal/race/vet/module integrity) |
| Migration and generator determinism | `PASS`: `platform:migrations:check`; shared generator targeted suites `101/101` |
| Contract/SDK compatibility | `PASS`: `platform:contracts:check`, `platform:sdk:consumers`; contract output remains `ALL_GATES_OPEN`/`notGateClosure=true` |
| TypeScript/package/Admin checks | `PASS`: workspace typecheck, package build, Admin typecheck `0`, Admin tests `59/59`, Admin build |
| Scripts regression | `PASS`: `58/58` files, `457/457` tests |
| Documentation links and command references | `PASS`: nine public entry files checked |
| Difference against `dev` | `BLOCKED`: no `dev` ref exists; `main...HEAD` is the available comparison |

## Decision

`READY_FOR_MERGE: NO`.

Minimum release blockers are: establish a clean candidate boundary for the existing dirty work; review the migration and generated-file replacement set; run the affected Go, migration, generator, contract, Admin and public-content checks; and redact or remove the remaining private/local material before publication. Until then the current branch is an audit input, not an open-source merge candidate.
