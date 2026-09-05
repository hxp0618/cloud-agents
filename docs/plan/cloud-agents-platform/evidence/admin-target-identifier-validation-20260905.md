# M1 Target identifier validation — 2026-09-05

Base `923f628`, existing `codex/cloud-agents-platform-p0` branch. No unrelated
dirty/staged files included.

## Correction

The generated Target registration contract requires both `targetId` and
`targetName` to be 1–128 ASCII identifier characters, with alphanumeric ends.
The registration form previously accepted a name with spaces; the encoder
rejected it and the generic UI mislabeled the failure as a server response
contract problem. This was reproduced during the preceding empty-state flow.

Both inputs now use one native HTML pattern matching the existing generated
SDK constraint, including the `v`-flag hyphen escape. Shared visible bilingual
help is linked with `aria-describedby`; native invalid feedback uses the same
locale text and resets when edited. “Display name” is now “Target name”.
No request/schema/SDK or backend behavior was loosened, and no automatic
normalization silently changes identifiers. Ponytail uses native validity;
design-style reuses existing field-help typography.

## Executed checks

- New runnable unit check compares the native-pattern predicate against
  `encodeDeploymentTargetRegisterRequest` for both fields: single character,
  uppercase/digits, internal punctuation, 128/129-character limits, empty,
  embedded/leading/trailing spaces, leading/trailing punctuation, newline,
  Chinese and slash. Admin tests:29 passed,129 assertions.
- Admin TypeScript/Vite production build, scoped oxfmt/oxlint and diff checks
  passed. Existing >500kB chunk warning remains.
- Real local PostgreSQL17.6/migrations51 + Control Plane and Admin Vite proxy,
  continued from empty-state slice. Project
  `project-a5984acdcc923dd084a60544bddbb428`, browser origin
  `http://127.0.0.1:4174`, CP18085, Worker18095. In-app browser used; no new
  browser package or product-state injection.
- Chinese invalid ID then invalid name stopped in native validation: invalid
  field focused, localized `validationMessage`, no response-contract banner.
  Correcting the ID cleared its custom error while the bad name remained
  invalid. Correcting the name enabled successful real registration of
  `identifier-valid-target` / name `valid.name_1~x-2`.
- English name with spaces also stopped with English native feedback.
  Correcting it successfully registered `english-valid-target`.
- Both resulting Targets were independently re-read through generated Admin
  SDK: generation1, unprobed, one succeeded registration Audit each. Together
  with the preceding empty-state Target the project contained exactly3 rows.
  No transport Probe, environment deployment or Provider Turn was attempted.
- Inspected the registration Sheet in en-US/zh-CN, light/dark and
  1440×900/390×844. Final helper text reuses the existing12px field-help style;
  it is not a font reduction to conceal overflow. At390px the Sheet is390px,
  help width350px and document scroll width390px. Both locale descriptions
  remain visible and registration/cancel controls usable. Console warnings
  and errors were empty. This is scoped form QA, not full-page Daytona diff.

Reproduce: connect a fresh disposable project in Admin Web, open registration,
fill valid loopback endpoint/opaque unused reference, then try spaced ID/name.
Check native focus/message before correcting to a unique valid identifier and
submitting. Verify resource and Audit through the generated Admin SDK. Never
Probe these deliberately unconfigured endpoints or modify existing Targets.

## Cleanup and remaining boundary

Closed the owned browser tab and reset viewport. Stopped Vite PID51643 and
the owning dev script PID51269; its shutdown removed
`cloud-agents-dev-501-51269` and `.tmp/cloud-agents-dev.4oqoHK` including the
three temporary Target records, database and ephemeral tokens. No listeners
remain on4174/18085/18095. Temporary databases were not backed up; no existing
resource was changed or deleted.

This also completes cleanup for the preceding empty-state evidence. Goal is
active. M1 loading/other resource states and complete fixed-reference visual
acceptance remain open; M2–M4 actual Profile/provider/three-transport deployment
and cleanup acceptance is not proved by either registration slice. Other
request-validation paths are not claimed covered by this identifier fix.
