# Source provenance

The portable Cloud Agent packages were extracted from the MIT-licensed Synara repository without importing Synara Control Plane, UI, Effect contracts, or T3 Code internals.

## Verbatim import

- Source repository: `git@github.com:hxp0618/synara.git`
- Source commit: `f9fb3d695c3188a1878475986133ffee64d8befc`
- Import policy: preserve package source, tests, fixtures, schemas, manifests, and the three release helpers byte-for-byte before standalone-repository changes.

| Imported path                             | Source Git object                          |
| ----------------------------------------- | ------------------------------------------ |
| `packages/cloud-agent-protocol`           | `e0b43f7146b72db57075bb3ac1a87ea7006d6c8d` |
| `packages/cloud-agent-provider-api`       | `8792fc7721885f7d65850bb4f7217b624c7b8620` |
| `packages/cloud-agent-runtime`            | `6441ac348db1417fa6cb52148609fcdd60c7fc86` |
| `packages/cloud-agent-provider-codex`     | `1b36b8baad1c2d22aa6dd518502f813f6b42f0b2` |
| `packages/cloud-agent-provider-claude`    | `7567fc5fc529a69840d086831f6b36caa1b68f79` |
| `packages/cloud-agent-testkit`            | `5d8763aa59b3bffc6a2d5c60dcb888f7677cad41` |
| `packages/cloud-agent-distribution`       | `7022b05833f3627fdf7e817b6a78e6ce1aa315ca` |
| `scripts/cloud-agent-release-smoke.ts`    | `5b928c31efa66b46e97e5610984677c4c4be59e0` |
| `scripts/lib/cloud-agent-release.ts`      | `ffa5bb2c930806e4396eb5fd6e08155b74a90111` |
| `scripts/lib/cloud-agent-release.test.ts` | `b9f3929620805b2469884b997b8730c068824085` |
| `tsconfig.base.json`                      | `538fa0f0eb3c819c57655e26535ee3308dcaee66` |

## Follow-up protocol synchronization

The standalone history records the later `vcs.state.changed` Runtime Event vocabulary update in a separate commit sourced from Synara commit `b86d30b1aa6f383cf3a8453e6944abeaefe2db65`. This keeps the verified extraction baseline and the subsequent protocol delta independently auditable.

The original MIT copyright notice remains in `LICENSE` and must remain in all published package tarballs.
