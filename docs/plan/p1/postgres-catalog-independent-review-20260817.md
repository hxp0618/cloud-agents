# P1-A2.1b-impl-3 independent catalog review — 2026-08-17

- Status：**APPROVED — A2.1b-impl-3 implementation/review closure only**
- Fixed implementation：`bbb0bf21922525468568afea675a6c1dc5522409`
- Fixed catalog evidence/index：`6e58e06b146fc3eb1ac7706837adffdeee1d8fa1`
- Fixed source-bound supply refresh：`401206a313dc33a4636a801717c8b0387eebd30d`
- Review snapshot：clean `codex/cloud-agents-platform-p1`; local HEAD and `origin` both exact `401206a`
- Accountable owner：hxp0618
- Independent implementation reviewer：Codex reviewer not involved in the catalog implementation or supply refresh
- Severity result：P0 `0` / P1 `0` / P2 `0`

This record is not an immutable Gate signature. It closes only ADR-0010 A2.1b-impl-3's independent
implementation review. `G-DATA`, `G-AUTHORITY-P1`, `G-SECURITY-P1`, `G-SUPPLY-CHAIN` and every aggregate
Platform Gate remain open.

## 1. Decision

The fixed implementation is approved for its declared synthetic verified-catalog boundary:

1. exported `PGProjector.ProjectCatalog` validates one opaque `VerifiedCatalogContract` before issuing
   catalog queries; zero, loose, uncombined, expired, wrong-epoch, scope-swapped and owner-closure-drifted
   values fail closed;
2. expected catalog projection, subject digest and owner closures enter the canonical catalog binding;
   the same owner closure is exact-cross-bound to the signed initial schema scope, runner projection
   bindings and statement plan;
3. the actual PostgreSQL body is returned only after complete canonical equality with the verified
   expected projection; caller summaries, partial structure and digest-only substitutions are not
   authority;
4. the representative `PUBLISHED_IMMUTABLE` catalog remains testdata only. Checked-in production
   `schema-000001.json` and `schema-000002.json` remain
   `UNPUBLISHED_BOOTSTRAP_MUTABLE` / `NOT_IMPLEMENTED`, and production trust/CLI paths reject before a
   database connection;
5. the 24-leg PG15/16/17 × fresh A/B × owned-idle/borrowed-migration × normal/race matrix and its drift
   faults are accepted as local implementation evidence, not x86_64/cloud/production or Gate evidence.

## 2. Fixed implementation and supply bits

| Artifact                                  | SHA-256                                                            |
| ----------------------------------------- | ------------------------------------------------------------------ |
| `projection_pg_adapter.go`                | `9c3ba8cb6016d718a4c392678b3bb4f96e8ac67e78f1143d0aa4a619974f6390` |
| `projection_api.go`                       | `1fde6aadeaf2b1b610d1404bed037b229ff55b0699beeabd57a32495002937a1` |
| `projection_validation.go`                | `a9af8b9eea9dc438cf54ef5d2f2817c34c4bbb9c768338eb791c8e19e6e98f21` |
| `runner_projection_bindings.go`           | `8ed827886d3b306c889508405e1cb8a2a0092d14128a6be1bd374ee70525a8e7` |
| `projection_postgres_integration_test.go` | `44f62760de830e0f3999e87f565acb9de400aab8df8c3300ed11eb3f903e9f87` |
| representative catalog fixture            | `d3d72f58d251c1342b1e8d6075ac1420774b392ebff9d0cc33fe77394a22d90a` |
| PostgreSQL matrix script                  | `771ecbadec535043d2591ad2ad8d5d5ac52a7ad986e737875c3633b085a5ea70` |
| review-closure generation lock            | `46ad8ac9811e46cec7c28b712bfd7dc5fe3c25f8eaecac2e8bd6a79ef2eb647d` |
| source-bound dependency lock              | `96923e86b4a8385a2e95ba7dbe0212654a6c957af00ae07e00def92e129ac634` |
| CycloneDX 1.6 SBOM                        | `b24852ea156b24bfce6e228a303493a2bb923009cf69935046906acfd774f141` |
| `THIRD_PARTY_NOTICES.md`                  | `1cadb7fc75886f9085a53d3b9cc174b4c024981f609e4d5951e4e3f877dcbb48` |

The review independently recomputed the fixed `6e58e06` repository tree, Control Plane subtree,
320-file tracked manifest, 250-file Go-source manifest, `go.mod`, `go.sum`, selected module graph and
Linux/Darwin closures. They match the source-bound supply record. The 16-module SBOM retains 7 exact
Linux root dependencies, 9 graph-only inventory components, three independent PATENTS bindings and the
same-bits NOTICE.

## 3. Verification split

The implementation executor recorded the full 24-leg live Docker matrix and its catalog/authority drift
faults. The independent reviewer did not rerun that expensive matrix; it reviewed the fixture,
integration tests, script ordering/same-bits checks and immutable recorded outputs, then independently
ran:

- focused catalog/binding normal tests and the same focused scope under `-race`;
- `go vet ./...`, `go build ./...`, Linux amd64/arm64 `CGO_ENABLED=0` cross-build;
- `go mod verify`, `go mod tidy -diff`, selected module graph and platform closure recomputation;
- source/tree/manifest, dependency-lock, SBOM, NOTICE and PATENTS cross-binding checks.

The supply executor additionally used exact Node `24.13.1`, Bun `1.3.14` and Go `1.26.6`, validated the
SBOM against the official CycloneDX 1.6 BOM/SPDX/JSF schemas, and refreshed source-bound
`govulncheck v1.6.0` plus 16-module OSV evidence. Those online zero-finding results remain explicitly
time-bound and non-bit-safe; the independent reviewer checked their internal binding but did not treat
them as permanent safety evidence.

The known default ten-minute `internal/migration` snapshot-test timeout is still `NOT CLAIMED`. It is
neither a catalog assertion failure nor a passing all-package test result.

## 4. Remaining boundaries

This approval does not verify or authorize:

- production signed verifier/deployment trust-root configuration or publication of the checked-in
  catalog contracts;
- production runner/CLI database connection, migration mutation, ledger write or commit;
- x86_64/cloud PostgreSQL, N/N-1/PITR, physical controller/host power loss or final binary same-bits;
- deployment, merge to main, Platform RC, Beta, GA, release or any aggregate Gate closure.

A2.1b-impl-3 may now be marked complete at the implementation/review layer. The next permitted planning
entry is P1-A2.2 Membership/RBAC; it must begin with its own contract/authority freeze and must not reuse
this catalog review as runtime or Gate authority.
