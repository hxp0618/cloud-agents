# Admin Web visual capture

`capture-actual.mjs` drives a running Admin Web through a Chromium-based browser (CDP), captures the
required list, detail, form, confirmation, empty, error and permission-denied states in both
languages, themes and viewports, and checks the layout geometry against `reference-evidence.json`.
It signs in through the password flow using mode-`0600` account fixture files, selects the requested
tenant and project through the product controls, and signs out before switching to the non-admin
account. The browser receives only the protected session cookie and never receives an access token.

It is started by the full-capture mode of `test/e2e/test-platform-compose-admin-web.mjs`; captures are
written to that run's output directory and are not committed. Full-capture API fixture setup runs
outside the browser: a private automation credential is exchanged through the machine endpoint for
a short-lived project token, then that token is sent directly to the Control Plane rather than to a
Web BFF route.

`reference-evidence.json` records only the viewport, scroll geometry, theme and SHA-256 of each
reference capture of the Daytona `v0.190.0` Dashboard (upstream commit
`01c502bb1f1ff8f2885d0cd490e043736083dca8`) that
[07](../../../docs/plan/cloud-agents-platform/07-admin-web-requirements-and-design.md) fixes as the
visual reference. No Daytona image, source file, logo or other asset is included in this repository.
