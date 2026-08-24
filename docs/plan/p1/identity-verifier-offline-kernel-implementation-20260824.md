# P1 offline identity-verifier kernel - 2026-08-24

- Status: **SLICE B IMPLEMENTED CANDIDATE — INDEPENDENT REVIEW PENDING**
- Parent: `7fe1492effacb80d7045548013aa3fd330ea6e58`
- Branch: `codex/cloud-agents-p1-identity-verifier-slice-b-20260823`
- Decision: ADR-0025
- Gate effect: none

## Implemented boundary

Slice B implements only the package-private offline cryptographic kernel in `services/control-plane/internal/authn`.
It admits exactly three canonical unpadded base64url compact-JWS segments, bounded strict UTF-8 JSON with decoded
duplicate-member rejection and root-depth accounting, the closed `RS256` protected header, and the closed RSA JWK
shape frozen by `platform-identity-verifier/v1`. Signature verification uses only Go `crypto/rsa` with SHA-256;
there is no JOSE/JWT dependency or token-controlled key lookup.

Package-private snapshot and lineage values deep-copy RSA public material, revocation sets and lifetime lineage. A
generation replacement marks the old generation inactive under its exclusive lease, drains callbacks, and then
publishes the next `+1` generation with the exact previous digest. The full 32-record lifetime `kid` history is
permanent; reuse with different modulus bytes and a 33rd record fail closed. Snapshot admission uses its exact
canonical projection bytes for the 256 KiB profile bound.

Verification binds one clock read to exact type, issuer, snapshot-owned single audience, signature key, half-open
token/key/snapshot time rules, epoch, key/token revocation, target tenant/project and required permission. Correct-
length cryptographic failure precedes signed-claim semantic mismatch. The resulting pointer-only
`VerifiedPrincipal` retains no raw token/signature/JWK/unknown claims. Its shared CAS state makes success, callback
error, nil callback, panic, stale generation, tamper and shallow-copy attempts permanently one-shot.

The only cross-package seam is the sealed callback-scoped `ConsumeVerifiedPrincipal` view. Every view read checks
callback liveness; panic is rethrown unchanged after the view is closed and before the generation read lease is
released. Production callers remain zero. Slice C alone may consume this seam while removing the existing authz/store
actor bypasses.

## Early-review closure

- Pure `validSnapshotCardinality` derives its exact limit from the generated profile, admits prior history `0..32`
  and candidate keys `1..32`, and rejects 33, negative and `1<<62` counterexamples. `buildTrustSnapshot` calls it
  before any length-derived map/slice allocation or JWK parse; final duplicate-free lifetime history remains
  independently bounded to 32. A structural AST test freezes the top-level unary-negated helper call with exact
  `profile`, `len(priorHistory)` and `len(candidate.keys)` arguments plus its sole direct `internal_failure` return,
  and proves the guard precedes every `make`, `append`, `copy` and `parseRSAJWK` call.
- `replace` and `invalidate` use an independent mutation mutex. Replacement snapshots old state under a short state
  lock, validates the bounded candidate outside locks while the old generation remains admitting, and leaves old
  state byte-for-byte active on failure. Only a valid candidate closes admission and detaches current; the writer then
  drains the old exclusive lease and publishes the new generation. Readers use state-guarded non-blocking `TryRLock`,
  closing the pointer/lease race without holding the state mutex while waiting. Deterministic nested-principal tests
  cover concurrent replace and invalidate without a writer-preference lock cycle.
- Canonical string and array encoders reject invalid UTF-8 instead of converting it to U+FFFD. Snapshot construction,
  principal creation and self-binding propagate invalid projection state as `internal_failure`; legal U+FFFD and an
  invalid `0xff` tamper cannot share a binding.
- Production imports are an exact standard-library allowlist. AST checks freeze the three exported declarations and
  the exact opaque-principal fields, concrete receiver methods and sealed-view interface methods; embedded fields and
  any extra method fail. Identifier-reference closure (not only direct call expressions) rejects production aliases
  of the consume/verifier functions across the whole control-plane module. Contract-lock tests dynamically enumerate
  every non-generated authn Go source/test so an omitted mutable input fails.
- Retired-but-revoked key IDs resolve to `revoked_key` before current-key lookup. Owned snapshot profile/registry drift
  is `internal_failure`; only token `typ` or signed token-profile mismatch is `unsupported_profile`.

## Frozen canonical projections

Trust snapshot and verified-principal projections are closed typed encoders, not implicit Go struct marshaling.
Tests hard-pin their exact canonical bytes and domain-separated digests, including nested JWK/context/time/validity
objects, generation-one absent `previousSnapshotDigest`, optional absent `notBefore`/`tokenProjectId`, sorted scopes,
keys, lineage and revocation arrays.

- profile digest: `sha256:1846e974ad3efc192704e4409f1d97786e3fb7df9de17e5ea2f2024d729b3c07`
- registry digest: `sha256:654d5f8d20dfd1fcf8a9da3a06dd445d46f813f5c60d926ce3b3a00cd9eccde1`
- deterministic snapshot golden digest:
  `sha256:c8bdcb26a0ca9d16acb6a9022c4395ab063648a9adc95a3046c481545f81cd54`
- deterministic principal golden digest:
  `sha256:853977fe248a12761d108e4e67beea4c9e86e74b0d27656eabd0ce01d555a6e4`
- Go-profile/runtime input manifest:
  `sha256:17a15b589a19cb3fb6fd17b08789198f56612031cf0c9374e50e396330419792`
- generation-lock SHA-256: `8550b90da6f347cd5e7f2e9acee8f32dd01f2f94dcc104ece066accc308bcb0a`

The immutable profile and registry digests and their generated file bytes are unchanged. Slice B runtime sources and
tests are mutable conformance inputs in the Go-profile pipeline only; they are not inputs to either immutable digest.

## Verification

Using Node `24.13.1`, Bun `1.3.14`, and Go `1.26.6`:

- `internal/authn` focused normal and race tests: PASS;
- `internal/authn` vet and build: PASS;
- focused registry plus contract-lock Vitest: 2 files, 26/26 tests PASS;
- dedicated identity-verifier registry, generated-Go and contract-lock checks: current;
- targeted Oxfmt, Oxlint and `git diff --check`: PASS;
- changed-file Gitleaks scan: PASS. The repository-wide scan retains 34 pre-existing fixture/generated findings,
  including one unchanged generated-profile finding, and is not claimed as zero; and
- fixed-candidate independent P0/P1/P2 review: pending.

The Go matrix covers strict segments/base64url/UTF-8/JSON/decoded duplicates/trailing/depth/size/NumericDate,
2048/4096-bit RSA JWKs and faults, 32/33 lineage, permanent `kid` binding, valid/tampered RS256 and signature fault
precedence, required/unknown claims, every stable category, exact time/key/snapshot boundaries, revocation,
rotation/invalidation drain, canonical bytes/digests, optional presence, one-shot/concurrent/copy/tamper/panic/escape,
and AST export/import/exact-surface/production-reference/structural-allocation-order/forbidden-payload closure.

No broad `internal/migration` test, live PostgreSQL, remote host, HTTP/OIDC/JWKS/provider/P2 action, production trust
provisioning, database write, deployment, publication, release or Gate operation was run.

## Remaining boundary

Slice B remains a candidate until its exact commit receives an independent P0/P1/P2 verdict. Slice C remains not
started and must remove every production raw-actor bypass while holding this generation lease through authorization,
the exact typed operation and transaction settlement. All aggregate Gates remain open.
