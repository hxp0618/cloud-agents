# Daytona v0.190.0 visual baseline

This directory pins the visual reference for Cloud Agents Admin Web. It is evidence for appearance and interaction only; it does not prove Admin API authority, runtime behavior, or Cloud Agents visual conformance.

## Reference set

`reference-corrected/` contains 36 captures: list, detail, create form, confirmation, dropdown,
empty, loading, error and permission-denied × light/dark × 1440×900/390×844. Its
`reference-evidence.json` records every PNG hash, geometry, source commit, composition hash,
browser version and browser errors/warnings. These are neutral **upstream component** scenes,
not Cloud Agents resource fixtures or proof of backend/API behavior.

`reference-composition.stories.tsx` is our reference-only composition, importing the real
upstream UI components. It is outside Admin Web's TypeScript source include and is not
imported into the application. Upstream dependencies belong only in an external checkout;
do not install Storybook, Tailwind or Radix into Cloud Agents.

To reproduce, prepare an external checkout at the exact commit below, install its own locked
dependencies with `corepack yarn install --immutable --mode=skip-build`, and copy this
composition to its `apps/dashboard/src/components/ui/stories/visual-baseline.stories.tsx`.
Start its Storybook from that checkout:

```sh
STORYBOOK_DISABLE_TELEMETRY=1 NX_DAEMON=false corepack yarn storybook dev \
  -p 6006 --host 127.0.0.1 --no-open --config-dir apps/dashboard/.storybook
```

From Cloud Agents, using an already-installed Playwright module:

```sh
node apps/admin-web/visual-baseline/daytona-v0.190.0/capture-reference.mjs \
  /path/to/daytona-checkout /path/to/new-reference-output /path/to/playwright/index.mjs
```

The command refuses a different upstream commit, tracked source changes, a mismatched
composition or an existing output directory. It does not silently overwrite or approve a
baseline.

## Live Admin Web capture

`capture-actual.mjs` captures the running Admin Web against a live local stack and checks it
against `reference-corrected/reference-evidence.json`. It is driven by the full-capture mode
of `scripts/test-platform-compose-admin-web.mjs`; captures are written to the run's output
directory and are not committed.

## Provenance

- Upstream repository: `https://github.com/daytonaio/daytona.git`
- Tag: `v0.190.0`
- Commit: `01c502bb1f1ff8f2885d0cd490e043736083dca8`
- Audited scope: `apps/dashboard`

No Daytona logo, name, product assets, backend model, or source file is vendored into Cloud
Agents. Implementation rules are recorded in [DESIGN-SYSTEM.md](./DESIGN-SYSTEM.md).
