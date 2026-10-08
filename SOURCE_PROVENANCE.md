# Source provenance

The portable Cloud Agent packages were extracted from the MIT-licensed Synara repository without importing Synara Control Plane, UI, Effect contracts, or T3 Code internals.

The table below records the seven packages and three release helpers in the initial extraction. The Pi and DeepSeek harness provider wrappers were added later in standalone-repository history; their upstream package metadata and notices remain part of the open-source review.

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
| `test/scripts/cloud-agent-release.test.ts` | `b9f3929620805b2469884b997b8730c068824085` |
| `tsconfig.base.json`                      | `538fa0f0eb3c819c57655e26535ee3308dcaee66` |

## Follow-up protocol synchronization

The standalone history records the later `vcs.state.changed` Runtime Event vocabulary update in a separate commit sourced from Synara commit `b86d30b1aa6f383cf3a8453e6944abeaefe2db65`. This keeps the verified extraction baseline and the subsequent protocol delta independently auditable.

The original MIT copyright notice remains in `LICENSE` and must remain in all published package tarballs.

## OpenSandbox execd PTY terminal successor v1

The local successor recipe under `tools/opensandbox-execd-successor/v1` uses the official Apache-2.0 OpenSandbox repository at commit `f31696b429b2b67c197b9336583a78e114d7d36a` and Git tree `1969b038103a6911861d0cd052ccca0f6aaada1a`. The copied upstream `LICENSE`, reviewed patch, Go builder image and target platform are SHA- or digest-bound in `source.json`.

The final stage starts from the existing immutable execd image digest only to preserve its runtime assets and entrypoint while replacing `/execd`. That image does not expose enough build metadata to prove its exact source commit. The recipe records this as `UNKNOWN_BUILD_SOURCE`; it does not equate the runtime base with the bound upstream source. Build outputs and qualification evidence remain outside the repository. Full Foundation and RemoteWorker-only qualification build this successor by default; other focused modes retain their existing static pin until separately qualified. No image is published by the recipe.

## OpenSandbox server Cookie isolation successor v1

The recipe under `tools/opensandbox-server-successor/v1` binds the existing server image digest, its original HTTP proxy source bytes, the reviewed Cookie isolation patch and the patched source bytes. The extracted proxy file matches the corresponding file at the official OpenSandbox commit recorded in `source.json`; this is an exact-file comparison, not proof of the complete base image's build source. The base therefore remains `UNKNOWN_BUILD_SOURCE`.

The successor replaces only that proxy module and retains the upstream Apache-2.0 license. Its regression runs against the base image's existing HTTP client dependencies and verifies that a shared Cookie jar cannot transfer cookies between Sandbox requests while preserving explicit endpoint authority. Full Docker Foundation qualification builds this image locally and retains its immutable image ID and build receipt; other environments and modes retain their own qualification boundaries. The recipe does not publish an image or close the base image's supply-chain provenance.
