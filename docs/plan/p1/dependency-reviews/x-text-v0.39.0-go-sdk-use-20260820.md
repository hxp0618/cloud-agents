# P1-A3 Go SDK use of x/text v0.39.0 - 2026-08-20

- Status: **IMPLEMENTED CANDIDATE - INDEPENDENT REVIEW PENDING**
- Scope: direct `golang.org/x/text/unicode/norm` use by the unpublished Go SDK common-identity package
- Version: exact ordinary requirement `golang.org/x/text v0.39.0`
- Prohibited: `replace`, fork, vendor patch, floating version, publication, release, or Gate closure

## Bounded decision

The generated Go `NamespaceRef` profile needs full Unicode NFC normalization. The Go standard library does not
provide that operation, so this candidate reuses the already reviewed exact `golang.org/x/text v0.39.0` bits and
imports only `golang.org/x/text/unicode/norm`. The existing dependency review fixes the tag, module and `go.mod`
checksums, upstream source identity, vulnerability remediation, BSD-3-Clause license, and additional PATENTS grant:

- [`x/text v0.39.0 remediation review`](./x-text-v0.39.0.md)
- [`pgx/x-text implementation closure`](./pgx-v5.10.0-x-text-v0.39.0-implemented-closure.md)

Those records reviewed the Control Plane distribution boundary, not this new SDK distribution boundary. Therefore
they are reused only as same-bits dependency identity evidence. They do not independently approve the SDK package,
its generated source, its notice, or a published module.

## Candidate closure facts

- `sdk/go/go.mod` contains the exact direct requirement and no `replace`, `exclude`, or `retract` directive.
- `sdk/go/go.sum` fixes both module checksums:
  - `golang.org/x/text v0.39.0 h1:UbZz4pLOvn600D6Oh6GGEI6VAmndrEBLv8/6BEXzyus=`
  - `golang.org/x/text v0.39.0/go.mod h1:3UwRclnC2g0TU9x8PZiyfOajCd1zaUNHF9cvqcQZ+ZM=`
- The linked non-standard package closure is limited to `golang.org/x/text/unicode/norm` and its internal x/text
  packages; no service module is imported.
- `sdk/go/THIRD_PARTY_NOTICES.md` reproduces the upstream root `LICENSE` and `PATENTS` text. Their SHA-256 values are
  `911f8f5782931320f5b8d1160a76365b83aea6447ee6c04fa6d5591467db9dad` and
  `96f408bfae65bf137fc2525d3ecb030271c50c1e90799f87abf8846d8dd505cc`.
- The generated SDK manifest records the exact dependency and review identities. The platform generation lock binds
  those records together with the module files, notice, tests, source template, generated output, and this scope
  record.

## Required independent review

Before any SDK publication or supply-chain closure, a reviewer must re-check exact selected modules/packages,
module sums, no-replace policy, license/PATENTS bytes, generated NFC behavior, current vulnerability data, external
consumer build closure, and final distributed artifact contents. This record is non-Gate implementation evidence;
all platform Gates remain open, and production deployment or publication is not authorized.
