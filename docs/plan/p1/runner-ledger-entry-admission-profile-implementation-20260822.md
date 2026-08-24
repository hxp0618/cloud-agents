# P1 runner ledger entry-admission generated profile - 2026-08-22

- Status: **SLICE A IMPLEMENTED — LOCAL NON-GATE EVIDENCE**
- Parent: `115eaf6d47bdfd8b2e641e7b1b7bbb3a32d2fc4b`
- Branch: `codex/cloud-agents-p1-runner-entry-admission-contract-20260822`
- Decision: ADR-0021

## Implemented scope

This slice adds `runner-ledger-entry-admission/v1` as a distinct generated registry/profile bound to the exact
immutable `runner-ledger-consumer/v1` identity. It admits exactly the five consumer-v1 entry pairs and maps each to
the ordinary action `prepare_entry_admission`.

The generated state machine freezes `unclassified -> session_revalidating -> admission_ready -> admission_closed`
and its fail-closed unknown/revalidation paths. The implementation boundary requires a future fresh dedicated,
locked, read-only session and an exact close-only permit. Slice A contains no database handle, permit, transaction,
`BeginMigration`, SQL, ledger/evidence mutation, writer, HTTP/P2/provider surface, deployment, publication, or Gate
transition.

## Exact generated identities

- source file SHA-256: `56fcaa4806731ff968c4614f710502b39dad66ee64784662bf962f58a37c3b88`
- generated registry file SHA-256: `2dc0210f1aad1dd6cff1183324837ab7e88cc5491e9046ae07302b25a1f9e372`
- registry digest: `sha256:fb7621405bbe62e47ce99ff00239c0e0d3fc12465e475f468b3fb07887bb886a`
- source digest: `sha256:d92075da78fbefa54e628433573c2bedcc0dd83757c3a20f87d63bae01ba108c`
- state-machine digest: `sha256:94baeab96aaa0da096b6de2c48fc646723d9ad003b68fd1135edf4adcc141189`
- policy digest: `sha256:9869fae48b30e4077f346b1d7c4309056a2129fb5974e73762d897e2925dc26a`
- profile digest: `sha256:bee8d42d328984f929fafa1e9dd2d4b18c94814e9375a1e1a1199ad8cb0ab551`
- generated Go profile file SHA-256:
  `c95850d99a5cbc9d82480d2c63befd6a39ffaeeb2f2c2f1374ce21091ff806c6`
- contract manifest: `sha256:021ddad3977a47597a77df5ec031dcc5ed6f9fee652391aa41cf2278ebc5edf5`
- registry input manifest: `sha256:374db1756a1f092043d7f6f6a323fae3b061d77ee9a62328791e2e3b54b8ff3d`
- Go-profile input manifest: `sha256:31c0e16f6d9f164f3eba1337b53c499e579ff4dd5128abea8106375b790861cb`

Immutable predecessor evidence remains byte-identical:

- preflight-v1 registry file SHA-256:
  `2a04f67f9b06f25bc13211934d8cb914dcbe3f92d42053f636ec66b4f28ac11c`
- consumer-v1 registry file SHA-256:
  `fa7082803ea97d06eefa83eec3de784f7199fc0b47f0ca2d0f8203b8b7e96852`
- preflight-v1 generated Go SHA-256:
  `599b78537a3f1dd5d70c1b50aa5e7bc54e1b65ee463876ee3f111709a5ab2112`
- consumer-v1 generated Go SHA-256:
  `afc77e723b7a4439c47043376cb79f5cb6416ce22d54ab1dcffbfe49686ce928`

## Verification

Using Node `24.13.1`, Bun `1.3.14`, and Go `1.26.6`:

- full `platform:contracts:check`: PASS, including both new generators and the exact contract lock;
- platform bootstrap: PASS for 109 JSON files, 46 schemas, and 58 fixture cases; explicitly not Gate closure;
- targeted registry/contract-lock tests: 20/20 PASS, 101 assertions;
- focused Go profile tests: PASS;
- focused Go profile race: PASS;
- `go vet` and `go build` for `internal/migration`: PASS;
- Linux amd64 and arm64 `go test -c` for `internal/migration`: PASS;
- targeted Oxlint and Oxfmt checks: PASS;
- `git diff --check`: PASS;
- candidate-only secret-shape scan over all 33 changed/untracked files: PASS; the repository-wide all-ref scan is
  tracked separately and is not promoted to a Gate result here;
- SDK regeneration check: exact current; generated SDK code changed only in the bound contract-manifest header and
  derived manifests, not public behavior;
- old-source `ffab704cd26c4d817152602b0854ebb31ee1cc4b` full migration race: PASS in `6903.755s`,
  log SHA-256 `e5cf2392e68a1b86a6f71c49c6fbced048d783138568ce19e7e8f26e8a82b059`; this is historical
  side evidence, not current-slice or Gate evidence.

## Remaining boundary

Slice B must implement the fresh locked read-only revalidation and a registry-backed close-only permit, then return
`MIGRATION_PROJECTION_NOT_IMPLEMENTED` before every writer call edge. Slice C must add the fault matrix and fixed
candidate independent review. All Platform aggregate Gates remain open.
