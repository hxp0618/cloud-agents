# P1-A2.2 versioned lineage/quota profile independent review - 2026-08-18

- Status: **APPROVED - A2.2-impl-3 remediation implementation/review closure only**
- Fixed base: `857e502ea6a0995d8ae29ec2dc5377ebbf15b7bf`
- Fixed implementation source: `f731c6b4d4d9ce53337759415cf046383a09ad02`
- Source-bound metadata refresh: `610b1ab41b8ee279071f9409056dad69ef6b5550`
- Review snapshot: clean `codex/cloud-agents-platform-p1`; local HEAD and `origin` both exact
  `7ff1cd761637c71c130576f91e0a9104a3de3923`
- Accountable owner: hxp0618
- Independent security reviewer: Codex CLI `gpt-5.6-sol`, read-only session
  `01a0156c-6e50-7a13-a031-82241c43c878`
- Severity result: P0 `0` / P1 `0` / P2 `0`

This record is not an immutable Gate signature. It closes only the fixed-source independent review for the
ADR-0012 versioned lineage/quota profile and append-only `000006` remediation. `G-CONTRACT`, `G-DATA`,
`G-AUTHORITY-P1`, `G-SECURITY-P1`, `G-SUPPLY-CHAIN`, every aggregate Platform Gate, and entry into A2.3 remain
open or unauthorized.

## 1. Decision

The fixed implementation is approved for its declared local implementation/admissibility boundary:

1. manifest v1 rejects any `execution_policy.lineage_quota_profile` member presence, including an explicit
   empty string; v2 requires the member, and historical v1 is capped at five migrations so the current six-entry
   v2 bundle cannot be relabeled as v1 by dropping the field;
2. the selected profile is exact-bound through verified bundle facts, immutable quota facts, reservation
   arithmetic, planned journal headers and digests, stored replay inspection, recovery, append/rotation, and
   profile-specific checkpoint encoding; zero, unknown, copied, or swapped values fail closed;
3. append-only `000006_close_subject_issuer_validation.sql` applies the same closed issuer language as Go, and
   both mutation paths validate issuer digests before allocating a tenant revision or writing membership,
   role-binding, resource-change, or audit state;
4. regression tests cover accepted five-entry v1 omission, explicit-empty v1 rejection, missing/unknown v2
   rejection, six-entry v1 relabel rejection, profile-aware quota/replay boundaries, and direct PostgreSQL
   invalid-issuer no-effect behavior;
5. the signed six-entry bundle, generated outputs, dependency lock, SBOM, and NOTICE remain exact-bound to the
   fixed implementation source described below.

## 2. First finding and remediation

The first read-only `gpt-5.6-sol` review of `857e502..04a61af` returned `P0=0 / P1=0 / P2=1`. It found that a
manifest relabeled as v1 could retain an explicitly present empty profile and be treated as historical omission;
the current six-entry bundle also needed an explicit v1 cardinality fence.

Commit `f731c6b4d4d9ce53337759415cf046383a09ad02` closes both paths. The second independent review traced the
remediation and the complete profile binding, then returned `APPROVE. P0=0, P1=0, P2=0`. The approved review
therefore supersedes the first review's P2 result for this fixed implementation source; it does not erase the
finding or its remediation history.

## 3. Fixed source and derived bits

| Artifact                                     | Exact identity                                                     |
| -------------------------------------------- | ------------------------------------------------------------------ |
| fixed implementation source                  | `f731c6b4d4d9ce53337759415cf046383a09ad02`                         |
| repository tree                              | `a2bb00c6a518565f4d6579150a94954f04f3633c`                         |
| `services/control-plane` subtree             | `9035155e32a4d6d0a4df5e46635bb4122d25bd0b`                         |
| 339-file tracked manifest                    | `f2a194bfe7a2d1c64db36d79b0ba95596becf9e9d4a7e817742fad9e4b4a513e` |
| 258-file Go-source manifest                  | `8d95f0f60e028839e66c3f7691f6be49467c7eba55a5240eab830fd0843f87bf` |
| source-bound dependency lock                 | `3db792de6bc692bcdaf7e75140a4d193e7d99eb64f1bf36f48d25dbe23b6106f` |
| CycloneDX 1.6 SBOM                           | `d0a2e9afd9e6f9f74638799aaefafed69855c4a591d46a19a51a08493b7920fc` |
| `THIRD_PARTY_NOTICES.md`                     | `1cadb7fc75886f9085a53d3b9cc174b4c024981f609e4d5951e4e3f877dcbb48` |
| `contracts/generation.lock.json` (same-bits) | `a868f8ac39d21a7c4b968e0864e2baa86977a44aa1d0a8dbe42ebf40131c80fe` |

The selected module graph and Linux/Darwin production closures are unchanged. The SBOM retains 16 unique
components, seven exact Linux root dependencies, nine graph-only components, three PATENTS bindings, and no
unresolved references. Current-source vulnerability inheritance remains `NOT_CLAIMED`; no historical
zero-finding result is promoted to the fixed source.

## 4. Verification split

The implementation executor recorded exact Go 1.26.6 focused normal/race tests, compile-only all-package checks,
`go vet ./...`, `go build ./...`, Linux amd64/arm64 CGO-free builds, the PG15/16/17 normal/race matrix, and the
historical `f7baf95` migration-only full pass in 1083.628 seconds. No fresh full migration runtime rerun is
claimed for `f731c6b`.

The independent reviewer completed a read-only source and artifact review and independently passed:

- the migration bundle checker with 39 exact generated files;
- the migration generator `--check`;
- two Bun bundle/SQL test files with 19 passing tests;
- source/tree/subtree and documented manifest/bundle/lock identity checks;
- `git diff --check`.

The reviewer's contract semantic validation passed, but the full lock comparison stopped on its expected pinned
toolchain guard: reviewer-local Node `22.23.1` / Go `1.26.5` differed from required Node `24.13.1` / Go `1.26.6`.
Go tests could not start in the reviewer's read-only sandbox because it denied creation of the Go build directory.
The reviewer did not rerun PostgreSQL/Docker matrices because review mode forbids external mutation. These are
verification limitations, not passing claims and not code findings.

## 5. Remaining boundaries

This approval does not verify or authorize:

- production catalog publication, signed verifier/deployment trust-root configuration, or runner/CLI wiring;
- production database mutation, ledger write, commit, deployment, release, merge to main, RC, Beta, or GA;
- physical controller/host power loss, final binary same-bits, final artifact scan, or immutable Gate closure;
- A2.3 or any later implementation slice.

A2.2-impl-3 remediation may now be marked complete at the fixed-source implementation/review layer. The
historical blocker remains preserved as the reason ADR-0012 was required, and this record cannot be reused as
runtime, production, or aggregate Gate authority.
