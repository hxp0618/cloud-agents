# P1 offline identity-verifier generated profile - 2026-08-23

- Status: **SLICE A IMPLEMENTED CANDIDATE — INDEPENDENT REVIEW PENDING**
- Parent: `15a45f44bdc753236e5954b706dcb13f3d5314ba`
- Branch: `codex/cloud-agents-p1-identity-verifier-slice-a-20260823`
- Decision: ADR-0025
- Gate effect: none

## Implemented scope

This slice adds the immutable `platform-identity-verifier/v1` generated profile only. Its closed identity freezes
compact RFC 9068 access JWTs, exact `RS256`/RSA JWK admission, required registered and collision-resistant custom
claims, one snapshot-owned audience, bounded lexical/time/key-lineage rules, strict parsing, exact request/key/context
binding, stable redacted error categories, and domain-separated RFC 8785 digests.

The editable schemas, source, golden/negative fixtures, and independent fixture manifest live under
`tools/platform-identity-verifier/v1`. This is intentional: the existing public `contracts/` source-tree digest is
embedded in historical Identity, JSON, and Proto SDK outputs. Putting the new private verifier authority into that
tree, or changing root `package.json`, would rewrite historical generated evidence. The generated registry alone is
written below `contracts/generated/`, which is already excluded from the public source manifest.

The dependency direction is closed and acyclic:

```text
tools source/schema/fixture
        -> generated identity-verifier registry
        -> package-private generated Go facts
        -> contracts/generation.lock.json
```

The existing aggregate contract commands remain byte-identical. Their final contract-lock step now writes or checks
the registry and Go profile in the order above. The lock builder remains read-only, and the two dedicated generator
CLIs remain independently runnable. Slice A adds no token parser, signature verifier, trust snapshot, verification
context, principal, authz binder, caller, HTTP route, database handle, JOSE dependency, or external side effect.

## Exact generated identities

- profile digest: `sha256:1846e974ad3efc192704e4409f1d97786e3fb7df9de17e5ea2f2024d729b3c07`
- registry digest: `sha256:654d5f8d20dfd1fcf8a9da3a06dd445d46f813f5c60d926ce3b3a00cd9eccde1`
- source file SHA-256: `1e5b4f899b58a62ad27f1a836d6c1289f163086e6cfe9b48874e9bb3f04e6084`
- source-schema SHA-256: `dc59c4522303b13a97a5a227488873bce8e9073f9f42e2ba70794faa7797d889`
- generated-schema SHA-256: `b5cac9f434864d7156c3c7920f7035dc723b14ed5282a03b5048c1a50ad5839b`
- generated registry file SHA-256: `474bb31fa5721dd20fc5723b790f39d45fda5ac0392d9e5bb73cb0ecef3e0ccf`
- handwritten Go profile SHA-256: `dc7ae1c52473eabf5167831a0afcd6b8aa01f399f1015251f2801c88b474bb6f`
- generated Go profile SHA-256: `e3d9ed08b69b3a7f4ce0ac6d100ea49f577dafdf857ff33be32c4170c357b8de`
- Go profile test SHA-256: `4da351c0ae44d8de8a9740cc6c6bace3f79e080c47c6b97b141707b1a6d70406`
- registry input manifest: `sha256:b0f0db3351318b427ba39f2d0890bead63e49c188d0bdbec2f351ff3f8d0c4b2`
- Go-profile input manifest: `sha256:60921f0b289cab3a3f3403962a781b5ba608a13c0bd190dcf3e1bbf400dd406c`
- generation-lock SHA-256: `9bf85b0a8fcdbce751889c576135ea7673002e236bda129cdef3533eb619385c`
- public contract manifest retained exactly:
  `sha256:eb51453861feb6685eadcd335c0620fea5ca98de9058a9a3d3f5198f6e67406e`

The two verifier digests are independently hard-pinned in TypeScript and Go tests. Digest construction is exactly
`SHA-256(UTF8(domain) || 0x00 || RFC8785(projection))`; newline framing is rejected. Authority JSON reads reject
invalid UTF-8, trailing input, decoded duplicate keys including escape-equivalent spellings, lone surrogates,
symlinks, and repository-path escape before validation or generation.

The Go profile has no mutable package-level trust baseline. Its generated function returns a fresh, closed,
comparable literal, and `valid` compares against another fresh literal. AST tests reject exported declarations,
imports, extra functions, and every generated package-level `var`.

## Verification

Using Node `24.13.1`, Bun `1.3.14`, and Go `1.26.6`:

- dedicated registry and Go generators: current;
- focused registry plus contract-lock Vitest: 2 files, 26/26 tests PASS;
- `internal/authn` normal and race tests: PASS;
- `internal/authn` vet and build: PASS;
- focused Oxlint, Oxfmt, and `git diff --check`: PASS;
- aggregate `platform:contracts:check`: PASS, including 383 official JSON Schema cases / 1,299 assertions, both
  OpenAPI 3.1 documents, every historical generator, exact Proto baseline, and the final contract lock;
- all 100 outputs enumerated by the parent generation lock: byte- and Git-mode-identical, and independently equal to
  every parent-lock SHA-256; and
- pre-candidate read-only security review: `P0=0`, `P1=0`, `P2=0`. This does not replace the required fixed-candidate
  independent review.

No broad `go test ./internal/migration` was run because Slice A does not change the migration package. One early
development invocation exposed ambient Bun `1.4.0`, Node `26.7.0`, and Go `1.26.7`; it was not counted as evidence.
All results above use the exact repository toolchain.

## Remaining boundary

Slice A remains a candidate until a fixed-commit independent review returns a P0/P1/P2 verdict. Slice B may then own
only the offline standard-library crypto kernel and package-private principal boundary. Slice C remains responsible
for the authz/store binder and final matrix. Production trust provisioning, OIDC discovery, remote JWKS, HTTP/P2,
provider effects, production database writes, deployment, publication, release, and every Gate remain unauthorized
and open.
