# Reproducible Daytona reference capture — 2026-09-05

## Scope and provenance

- Starting Cloud Agents HEAD: `704be24d49443eb9bacfa5e40e448b0d9ab64689`, branch `codex/cloud-agents-platform-p0`.
- Upstream: Daytona v0.190.0, commit `01c502bb1f1ff8f2885d0cd490e043736083dca8`.
- External checkout: `/tmp/cloud-agents-daytona-baseline.p4J5TI/source`; no tracked upstream source changes.
- Our reference-only Storybook composition imports the actual upstream components. It uses fullscreen Storybook layout and the production Dashboard's `SidebarInset overflow-y-auto` constraint, fixing the previously documented mobile layout-viewport expansion.
- Historical `reference/` images are unchanged. New `reference-corrected/` images are neutral component scenes, **not** Cloud Agents API fixtures or runtime evidence.

The composition and capture entrypoint are retained beside the images. Reproduction commands and guards are in [the baseline README](../../../../apps/admin-web/visual-baseline/daytona-v0.190.0/README.md).

## Executed checks

- Three successful full captures: temporary verified output, committed corrected output, and a final run with the formatting-enabled generator. Each produced 36 images: nine states × two themes × desktop/mobile.
- Final-generator output: `/tmp/cloud-agents-daytona-baseline.p4J5TI/reference-final-generator`, completed at `2026-09-05T10:08:01.276Z`. All 36 PNG hashes were independently recomputed and matched its manifest; the repository set's 36 hashes also matched.
- Browser: Brave/Chromium `152.0.7977.83`, existing Playwright 1.62.1, DPR 1, reduced motion, GPU disabled consistently with application captures.
- Every capture checked the actual viewport and document overflow; detail/create states also checked Sheet width, position, bottom and footer bottom. Both complete manifests recorded zero browser errors/warnings and only the local Storybook origin.
- Visual inspection sampled mobile list, create, detail and dropdown, desktop confirmation and loading. This is not approval of every application state.
- Wrong upstream HEAD was rejected before creating output. Existing output was rejected without overwrite. The first attempted run failed on an incorrect Skeleton selector; the corrected selector targets the actual upstream Skeleton root and subsequent complete captures passed.
- Corrected dark/mobile create reference compared to the previous live application's create Sheet footer: 26,070 unmasked pixels, zero changed pixels. Only button-label interiors are excluded. This is a component-region result, not full-page equivalence.
- Scoped formatting and lint passed for composition/capture code and manifest. Admin Web's 30 tests, TypeScript and production build passed. Built CSS `index-Cw59Sea_.css` and JS `index-DPKeQGYS.js` remain unchanged; the existing large-bundle warning remains.

## Isolation and boundaries

No Storybook, Tailwind, Radix or browser dependency was added to Cloud Agents. The reference composition is outside the application's TypeScript source include and is not imported into the product. Upstream dependencies were installed only in the external checkout with its immutable lockfile; upstream peer warnings are not application qualification.

The task-owned Storybook process on port 6006 was stopped after capture. Reference outputs and the external checkout remain available for reproduction; no existing infrastructure resource was changed.

Earliest incomplete ADMIN-M1 work remains full live-application visual comparison and current real Docker/Kubernetes/SSH Target Probe-ready evidence. This slice performs no lifecycle deployment, Provider Turn, credential test or destructive cleanup and does not satisfy M1/M4 or the Goal's final acceptance criteria. No new credential or destructive-operation approval was needed for this slice.
