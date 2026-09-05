# Account identity and keyboard focus fidelity — 2026-09-05

Base `349b94fd67b8b1eb20fec467cfcd1b696a8ddd0c`, branch
`codex/cloud-agents-platform-p0`. Small ADMIN-M1 styling correction, not visual gate closure.

## Source comparison and change

Reverified the fixed Daytona checkout at `01c502bb1f1ff8f2885d0cd490e043736083dca8`.
`apps/dashboard/src/components/PageLayout.tsx` uses single-line truncation for Profile identity
and a256px menu. Its input/button components place a3px half-opacity ring immediately outside
the border. Current Cloud Agents runtime inspection confirmed its Profile-specific selector
already overrides the generic224px dropdown to256px: no width change was necessary.

Actual differences corrected in native CSS only:

- Long project identity wrapped at the hyphen into two lines despite `text-overflow: ellipsis`.
  Added `white-space: nowrap` to the existing project/tenant identity rules; full DOM text remains.
- Keyboard focus outline had an extra1px offset. Removed that gap while retaining the3px,
  theme-token, half-opacity visible outline and focused border. No outline suppression.

No API, SDK, authorization, state, component, dependency or User Web change. Design-style and
Product Design source-grounding guided this correction; existing native controls were retained.

## Executed verification

Fresh owned local PostgreSQL/CP/Worker stack applied schema051. Project
`project-784dec7203d327952dd8913853d44631` was created through the real CLI/API; the Overview's
empty resource counts came from that database, not mocks or injected product state.

Used the Codex in-app browser through CUA, including its read-only DOM style inspector and
viewport capability. Initial tab was opened before Vite started and showed connection refused;
a fresh tab against the ready server succeeded. No external Playwright fallback was used.

Eight en-US/zh-CN × light/dark ×1440×900/390×844 combinations passed these live assertions:

| Property | Actual result in every combination |
| --- | --- |
| Menu width | 256px, wholly inside viewport |
| Project identity | nowrap, one16.5px rendered line |
| Keyboard Tab from Profile trigger | focused language selector |
| Focus outline | 3px, zero offset |
| Document horizontal overflow | none |

Language and theme were changed through real UI controls. Escape closed the account menu.
Before/after desktop and corrected Chinese dark mobile screenshots were inspected inline;
the browser's recorded error/warning log query returned no entries. Nonblank page, localized
headings and current resource counts were observed. Temporary viewport override reset and
test tab closed. This is local CSS/interaction evidence, not authentication or Provider E2E.

Admin27 tests, production typecheck/build, scoped formatter and diff check passed. Existing
530.27kB single-chunk build warning remains. No static test is claimed as visual acceptance.

Normal shutdown removed the owned `cloud-agents-dev-501-30376` database container and
`.tmp/cloud-agents-dev.OsKTLe` state/token directory; Vite/CP/Worker listeners4174/18085/18095
were verified absent. Disposable database and tokens are not retained for recovery. No Target,
Provider, pre-existing workload or infrastructure credential was used or modified.

## Reproduce and remaining boundary

Start the existing local dev stack and Admin Vite, create a fresh project, connect through the
Admin form with its temporary token. Open Profile, press Tab to the language selector and
inspect the menu/identity/control computed styles. Repeat both locales/themes at the dimensions
above. Compare the fixed PageLayout/Input/Button source rules, not the current Daytona website.
The screenshots from the earlier resource-list comparison have differing business content and
do not prove full-page pixel equivalence. Account typography, remaining component/state mappings
and complete fixed screenshot comparison are still unqualified; no broader style approval.

Earliest remaining item is complete ADMIN-M1 fixed Daytona visual/interaction acceptance.
M2/M3/M4 actual profile-backed deployments, production OIDC, Provider flows and three-target
zero-residue lifecycle acceptance remain open. No additional user credentials or destructive
authorization is needed for the next source/visual slice. Goal remains active.
