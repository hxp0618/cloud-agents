# Daytona v0.190.0 visual baseline

This directory pins the visual reference for Cloud Agents Admin Web. It is evidence for appearance and interaction only; it does not prove Admin API authority, runtime behavior, or Cloud Agents visual conformance.

## Provenance

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

The `actual/` captures exercise the Vite application against a running local Control Plane through the Admin API proxy. The database contained three real persisted, unprobed Deployment Targets—Docker, Kubernetes, and SSH—and no static UI fixture. Their hostnames and opaque credential references were synthetic, so this evidence proves Admin API list/detail rendering and browser boundaries, not target connectivity or Probe readiness.

| Capture                               | Theme | Viewport | Live behavior                                                        |
| ------------------------------------- | ----- | -------- | -------------------------------------------------------------------- |
| `list-light-desktop.png`              | light | desktop  | three persisted Target kinds through generated SDK                   |
| `list-light-desktop-collapsed.png`    | light | desktop  | 48px sidebar reached with the documented keyboard shortcut           |
| `list-dark-desktop.png`               | dark  | desktop  | runtime theme switch and dark resource table                         |
| `list-light-mobile.png`               | light | mobile   | compact PageHeader and horizontally scrollable live table            |
| `list-dark-mobile.png`                | dark  | mobile   | responsive dark list                                                 |
| `navigation-light-mobile.png`         | light | mobile   | 288px off-canvas resource navigation                                 |
| `detail-light-desktop.png`            | light | desktop  | real SSH Target in a 500px right Sheet                               |
| `detail-dark-mobile.png`              | dark  | mobile   | full-width SSH detail Sheet                                          |
| `create-form-light-desktop.png`       | light | desktop  | native validated create form in a right Sheet                        |
| `create-form-dark-mobile.png`         | dark  | mobile   | full-width mobile create form and sticky actions                     |
| `permission-denied-light-desktop.png` | light | desktop  | ordinary user token rejected by both Admin list endpoints with `403` |

`actual/browser-evidence.json` records the machine-readable boundary checks: only the Admin Web origin was contacted; the three Target kinds decoded; no bearer was persisted; connected-state console warnings, errors, and failed HTTP responses were empty; the sidebar shortcut, Dropdown Escape/outside dismissal, Sheet Escape dismissal, and mobile navigation open/close all succeeded; and the two deliberate ordinary-user requests returned `403`. It contains no token or credential bytes.

Reproduce against a running local stack and Admin Web:

```sh
node apps/admin-web/visual-baseline/daytona-v0.190.0/capture-actual.mjs \
  apps/admin-web/visual-baseline/daytona-v0.190.0/actual \
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
4942e4e959c13833297f6f115b7f4e619301efbb3977c41ff18d649070e74ccb  browser-evidence.json
c015d344c0990f9d49a53612702a3d6fae2c855a9c590521ff9f2082c3eae79e  create-form-dark-mobile.png
c5fd6d5c6e5f657aba48d2af25457b3bf296115a87326430e8010772b60abd2f  create-form-light-desktop.png
ff16aff8748d857342aea2f287cea8af6f0d7113a67c136d0cb5882217a0a460  detail-dark-mobile.png
9019eb31f789b75995e83410e90667f646730b202aab8526692d0d0af4d9cb39  detail-light-desktop.png
9b9ed5c24f4c37d488c56f4d9bd6d6ee66ef99191e29b1a5f866ffef2866571c  list-dark-desktop.png
ea39586ecd4644fb27517a7fcf326a77176802c037940c7cf1741a8221d4adf8  list-dark-mobile.png
690df45ba8822b735984cd5eb1f9b2dccdf36aff6b9e9d8b54b60e637c7023fb  list-light-desktop-collapsed.png
28b6911ed361a5aceea40e9a9ac2ea9d12a563a8d94f55b83c7a73b5f7a238aa  list-light-desktop.png
1d66bdfee08a27d065528434f8c4c136b9431d51bcd06907c972fd482c1fc7f6  list-light-mobile.png
a3995aa02f6c6fc15ff437b8ed13b49cb11f6e66af96f253049999ba331d3581  navigation-light-mobile.png
1d060f78a229ce62b10b815f9ab5915224e02647dcb9b9af6c20c2094afd9e48  permission-denied-light-desktop.png
```

Verify with:

```sh
shasum -a 256 apps/admin-web/visual-baseline/daytona-v0.190.0/actual/*
```
