# Daytona v0.190.0 visual baseline

This directory pins the visual reference for Cloud Agents Admin Web. It is evidence for appearance and interaction only; it does not prove Admin API authority, runtime behavior, or Cloud Agents visual conformance.

Known reference defect (verified 2026-09-05): the historical mobile Sheet PNGs are clipped
because the Storybook composition omitted the production SidebarInset overflow constraint.
At a 390×844 device viewport the layout expanded to 613×1327 and the Sheet started at x=223.
Do not use these images as accepted mobile geometry. The default Storybook outer padding
also differs from the production shell. See the [reproduction and current component check](../../../../docs/plan/cloud-agents-platform/evidence/admin-web-mobile-sheet-actions-20260905.md).
Historical images and hashes remain unchanged. Use the corrected set below for new checks;
a complete Cloud Agents-to-reference visual comparison is still required.

## Reproducible corrected reference set

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
composition or an existing output directory. It waits for rendered content, fonts and finite
animations, verifies actual viewport/document bounds and Sheet/footer geometry, and captures
with consistent GPU flags. It does not silently overwrite or approve a baseline. Inspect the
images and compare against matching **live** Admin Web states before claiming conformance.

## Provenance

The additional `reference-toast/` set captures the actual upstream success Toaster in both
themes and viewports. Install `reference-toast.stories.tsx` in the external checkout's same
`ui/stories` directory and open `reference-toast--success` in Storybook. Its ThemeProvider is
required: the generic Storybook theme class alone does not change the Toaster context.
`compare-toast.mjs REFERENCE ACTUAL PNG_MODULE` compares the single-line Toast exterior in
fixed viewport coordinates, masking only the title/icon interior. It does not approve the
whole page. The current strict result and remaining mismatch are recorded in
[the Toast evidence](../../../../docs/plan/cloud-agents-platform/evidence/admin-web-success-toast-20260905.md).

- Upstream repository: `https://github.com/daytonaio/daytona.git`
- Tag: `v0.190.0`
- Commit: `01c502bb1f1ff8f2885d0cd490e043736083dca8`
- Audited scope: `apps/dashboard`
- Captured: `2026-09-03`
- Desktop viewport: `1440x900`, DPR 1
- Mobile viewport: `390x844`, DPR 1

The tag was fetched into a temporary sparse checkout. Its token definitions and component behavior were inspected from `src/index.css`, `components/Sidebar.tsx`, `components/PageLayout.tsx`, and the `components/ui` implementations for button, input, table, sheet, dialog, dropdown, empty, and skeleton states. A temporary, unbranded Storybook composition supplied neutral resource labels solely to expose those components consistently. Screenshots were captured through the browser DevTools protocol with reduced motion enabled.

No Daytona logo, name, product assets, backend model, or source file is vendored into Cloud Agents. The temporary checkout and composition are not part of the product. The current Daytona website was explicitly excluded because it did not match the pinned version.

## Coverage

| Reference                            | Theme | Viewport | Required behavior                                                          |
| ------------------------------------ | ----- | -------- | -------------------------------------------------------------------------- |
| `list-light-desktop.png`             | light | desktop  | shell, expanded sidebar, page header, intro, toolbar, table, status badges |
| `list-dark-desktop.png`              | dark  | desktop  | dark tokens, shell, list/table                                             |
| `list-light-mobile.png`              | light | mobile   | responsive shell, compact header, horizontal table overflow                |
| `list-dark-mobile.png`               | dark  | mobile   | responsive dark list                                                       |
| `detail-light-desktop.png`           | light | desktop  | right detail Sheet and overlay                                             |
| `detail-dark-mobile.png`             | dark  | mobile   | full-width mobile detail Sheet                                             |
| `create-form-light-desktop.png`      | light | desktop  | right create Sheet, form, sticky footer actions                            |
| `create-form-dark-mobile.png`        | dark  | mobile   | full-width mobile create form                                              |
| `confirm-dialog-light-desktop.png`   | light | desktop  | centered destructive confirmation Dialog                                   |
| `dropdown-dark-desktop.png`          | dark  | desktop  | action Dropdown and destructive item                                       |
| `empty-state-light-desktop.png`      | light | desktop  | resource empty state and primary action                                    |
| `loading-dark-desktop.png`           | dark  | desktop  | table skeleton loading state                                               |
| `error-state-light-desktop.png`      | light | desktop  | stable-code error feedback                                                 |
| `permission-denied-dark-desktop.png` | dark  | desktop  | permission-denied feedback                                                 |

The corresponding implementation rules are recorded in [DESIGN-SYSTEM.md](./DESIGN-SYSTEM.md).

## Cloud Agents implementation evidence

The `actual/` captures exercise the Vite application against a running local Control Plane through the Admin API proxy. The database contained three real persisted, unprobed Deployment Targets—Docker, Kubernetes, and SSH—and no static UI fixture. Their hostnames and opaque credential references were synthetic, so this evidence proves Admin API list/detail rendering and browser boundaries, not target connectivity or Probe readiness. Unprefixed captures use `en-US`; `zh-CN-*` captures repeat the pinned list/detail/create and responsive states in Simplified Chinese.

| Capture                               | Theme | Viewport | Live behavior                                                         |
| ------------------------------------- | ----- | -------- | --------------------------------------------------------------------- |
| `list-light-desktop.png`              | light | desktop  | three persisted Target kinds through generated SDK                    |
| `list-light-desktop-collapsed.png`    | light | desktop  | 48px sidebar reached with the documented keyboard shortcut            |
| `list-dark-desktop.png`               | dark  | desktop  | runtime theme switch and dark resource table                          |
| `list-light-mobile.png`               | light | mobile   | compact PageHeader and horizontally scrollable live table             |
| `list-dark-mobile.png`                | dark  | mobile   | responsive dark list                                                  |
| `navigation-light-mobile.png`         | light | mobile   | 288px off-canvas resource navigation                                  |
| `detail-light-desktop.png`            | light | desktop  | real SSH Target in a 500px right Sheet                                |
| `detail-dark-mobile.png`              | dark  | mobile   | full-width SSH detail Sheet                                           |
| `create-form-light-desktop.png`       | light | desktop  | native validated create form in a right Sheet                         |
| `create-form-dark-mobile.png`         | dark  | mobile   | full-width mobile create form and sticky actions                      |
| `permission-denied-light-desktop.png` | light | desktop  | ordinary user token rejected by three Admin list endpoints with `403` |

The Chinese regression adds `zh-CN-list-{light,dark}-{desktop,mobile}.png`, `zh-CN-detail-light-desktop.png`, `zh-CN-detail-dark-mobile.png`, `zh-CN-create-form-light-desktop.png`, `zh-CN-create-form-dark-mobile.png`, and `zh-CN-navigation-light-mobile.png`. These use the same live resources, fixed viewports, and reduced-motion setting as the English captures.

`actual/browser-evidence.json` records the machine-readable boundary checks: only the Admin Web origin was contacted; the three Target kinds decoded; no bearer was persisted; connected-state console warnings, errors, and failed HTTP responses were empty; locale switching was immediate, survived reload, and recovered an invalid locale to `en-US`; no message key appeared in the page; all 20 desktop/mobile layout checks had zero document overflow; the sidebar, Dropdown, Sheet, and mobile-navigation interactions succeeded; and the three deliberate ordinary-user requests returned `403`. It contains no token or credential bytes.

Reproduce against a running local stack and Admin Web:

```sh
node apps/admin-web/visual-baseline/daytona-v0.190.0/capture-actual.mjs \
  /path/to/new-output \
  /path/to/admin-token /path/to/user-token PROJECT_ID
```

## Reference integrity

```text
ebe5f54e784b9b49196fe06e882080fa551e7a1d3cd22fa28abfd2bb8b13aec9  confirm-dialog-light-desktop.png
2969bc59d57e0ae41acbc6b142b569dfda43cf3d800d0d1a5ab609ee1a1d36cf  create-form-dark-mobile.png
f1cbe972955ff81a85da24db26f232a908b52c4638bf05e6c471a2fa5e352ccd  create-form-light-desktop.png
965af195a1222e80a2615313d125a5e6a61df654fbd63f408c199e6004a71583  detail-dark-mobile.png
a724767ae7599cabaf9a6b79eee337d1dadd23d96ffe909d52072c9d0f49c42f  detail-light-desktop.png
6bc0a95b304dcc4f91abc56e5fd75c7c44e14b4b1408378f4af717669e12f20c  dropdown-dark-desktop.png
c8bab2fff7eb0fcd878ac907711cabb26fa3b2f45848dc01c45d422e6afadfaf  empty-state-light-desktop.png
e11c906db135c33107558591d757b828ad393647b8d8a559e65617fddef02ec7  error-state-light-desktop.png
56676e9505b8fd03b4c657428b75eb0f8f9c71eaf8c6ae845f2993d65870fbf1  list-dark-desktop.png
d488bb7ee80e90770f8902e3e8ef9679fdf7f16805b5344ebe4102f5613c76df  list-dark-mobile.png
6a24f80ac0492d01fe8aeefc957209933e121a5954c86f5ddc329cf3aec8d386  list-light-desktop.png
f42e0bf66f5119656d4d41891705904f0dc5424424ff0497ea256e38940ea393  list-light-mobile.png
e48510d34dd33b5ffcfddfeafbfa7f8fab1b565c0793c47178ad3e7329b94459  loading-dark-desktop.png
4cbf316b20caecfd1a2b965c8e70cae1f07fac01a0cd86b47bd61008c30cf604  permission-denied-dark-desktop.png
```

Verify with:

```sh
shasum -a 256 apps/admin-web/visual-baseline/daytona-v0.190.0/reference/*.png
```

## Cloud Agents implementation integrity

```text
23dc5fbaf3b4e702067666b0ad63e8b686a7c77d4edd01bbf48beef88e4b54a4  browser-evidence.json
6d35157f0951e75a64ec778b44c7af85f630d2558dec64df0dfe74efdbec9ed0  create-form-dark-mobile.png
99a5d608b5fd76dba9c439e783238cf03745d438cfb17e8eac16065c492885c0  create-form-light-desktop.png
e647b2322229b576b97234e3bde5e4baedd750df60f30380885d78c72e4ead98  detail-dark-mobile.png
fa3be672aa1f92b8a4faf34c434c96c713cb3334e9e58eb160b23f04054e1750  detail-light-desktop.png
5b2d5bc80c5146d2bcb94cfcc522ef25d363ab5cb9474a815902056c7447e25c  list-dark-desktop.png
c6d72743d9e9feee37bc7cc4c5bd23e4f0fe415a9fd3fcfe225f9b8e0ed7ef76  list-dark-mobile.png
8c1e34c02fd37a99700fc4256362606231dafb83e2443a6451a936ac645612f3  list-light-desktop-collapsed.png
8b950feb39493b3c40b348f880caaca200c0614d83913ca98fb263034305b7ee  list-light-desktop.png
aef61cbc94b0b802c874b1aa7b50fd35a2d2fecbb185db148335d40ba219cd88  list-light-mobile.png
b57c66754945762192984925af32394847a475cd9d8690ffb2cc2fcba7ce6e61  navigation-light-mobile.png
241064d1dc5e691e3215a8e2f533302e12fb6edecebee9ca625bdd9a362ce2e7  permission-denied-light-desktop.png
cbad83bc4ba8c153f3c86596b43b4d9e9c1c08806aa8f9cb2780e488e21b3a5b  zh-CN-create-form-dark-mobile.png
70714bdb7bac1a2b5800b94e814c85f42ffe40340160d9920343b9cfac3e45fb  zh-CN-create-form-light-desktop.png
dcb59e4c85f2dce9c0d1577a878dcdc042e247530dc7f69ae320ff3c0a4cbe73  zh-CN-detail-dark-mobile.png
a967cc99a49350e7a22cc95a4a6ac29f07eb093af8c3431a63e5526f2d22667b  zh-CN-detail-light-desktop.png
7c25564da82de2ccf96a032958d476d2c5eaaeac08d78e4ea161b0495ecadb1d  zh-CN-list-dark-desktop.png
993a702d5a14164133b02611e6f74b4231140888e629602329e26c21999769d9  zh-CN-list-dark-mobile.png
f3771c13f30fd726e7472ecc561b6f1c6804a860df5fab19163ce29b1bc0a769  zh-CN-list-light-desktop.png
9719d4f6743a464d0e4374a9f08fc6188c78bdc529a770ef6e4ca207c61cd18c  zh-CN-list-light-mobile.png
4d2e48573f45d54e24db2a5af4c86745b8c3bb283630dc28aa04d6e628eb4dae  zh-CN-navigation-light-mobile.png
```

Verify with:

```sh
shasum -a 256 apps/admin-web/visual-baseline/daytona-v0.190.0/actual/*
```
