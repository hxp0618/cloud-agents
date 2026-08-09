# Changelog

## 0.2.0-rc.1 / 0.1.0-rc.1 - 2026-08-09

- Extracted the seven portable packages into an independent Node/Bun workspace.
- Retained Protocol 2.2/2.3 and existing schema identities while adding the `vcs.state.changed` Runtime Event.
- Added portable `CLOUD_AGENT_*` environment aliases with legacy fallback, conflict rejection, and dual-written child credential metadata.
- Added `extendEnvironment: false` and ordered async listener acknowledgements to the public stdio client.
- Added plain-JavaScript protocol, descriptor, distribution-manifest, and executable helpers.
- Added packed-bin conformance, seven-tarball release smoke, standalone runtime, checksums, SPDX SBOM, provenance, and tracked-history secret scanning.
- Published NodeNext-safe ESM/CJS declaration conditions and discriminated payload-message types, with external consumer typechecking in the release smoke.

This is a GitHub release-candidate line only. Nothing in this changelog represents npm publication, production deployment, public-beta approval, or GA.
