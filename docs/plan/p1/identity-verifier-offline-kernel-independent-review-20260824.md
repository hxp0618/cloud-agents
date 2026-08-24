# P1 offline identity-verifier Slice B independent review — 2026-08-24

- Verdict: `APPROVE`
- Severity: `P0=0 / P1=0 / P2=0`
- Fixed candidate: `6e79edabbd68177023ff1ce8a848f4a7dd3307fd`
- Candidate tree: `9b40ad33e74dcbb243f9fd77595b22527c2420b9`
- Candidate parent: `7fe1492effacb80d7045548013aa3fd330ea6e58`
- Candidate diff SHA-256: `35e9c2de7a2fc1e9cb9db84f9615c6f209f53f97d8d5ea7e12e4d4dde5d538f0`
- Candidate branch: `codex/cloud-agents-p1-identity-verifier-slice-b-20260823`
- Decision: [`ADR-0025`](../adr/0025-p1-offline-jwt-access-token-verifier-contract.md)
- Candidate record:
  [`identity-verifier-offline-kernel-implementation-20260824.md`](identity-verifier-offline-kernel-implementation-20260824.md)

This is an independent, read-only P0/P1/P2 review of the exact Slice B candidate above. Approval applies only to that
commit. It makes Slice C eligible under the existing owner approval but does not implement or approve Slice C, merge
the candidate, establish a production trust root, open a production caller, authorize database access or writes,
HTTP/P2/provider behavior, deployment, publication, release, or close any Gate.

## Fixed identity and scope

The candidate worktree was clean, was `0/0` against its upstream candidate branch, matched that remote branch exactly,
and resolved to the supplied commit, tree, single parent, and candidate-diff hash. Its 18 changed paths contain ten
new `internal/authn` runtime/test files, the generation-lock builder/test/lock update, one implementation record, and
bounded status/index updates. It changes neither `authz` nor `store`, and it adds no production caller, network or
database surface.

The review reproduced these exact identities:

- profile digest: `sha256:1846e974ad3efc192704e4409f1d97786e3fb7df9de17e5ea2f2024d729b3c07`;
- registry digest: `sha256:654d5f8d20dfd1fcf8a9da3a06dd445d46f813f5c60d926ce3b3a00cd9eccde1`;
- generated registry file: `474bb31fa5721dd20fc5723b790f39d45fda5ac0392d9e5bb73cb0ecef3e0ccf`;
- generated Go profile: `e3d9ed08b69b3a7f4ce0ac6d100ea49f577dafdf857ff33be32c4170c357b8de`;
- Go-profile/runtime input manifest:
  `sha256:17a15b589a19cb3fb6fd17b08789198f56612031cf0c9374e50e396330419792`;
- generation lock: `8550b90da6f347cd5e7f2e9acee8f32dd01f2f94dcc104ece066accc308bcb0a`;
- candidate implementation record: `c7de7a2f35a6a3dda975b8762099abc5f5c5463585e83adf7b26f1d5ba3bb4f3`.

The immutable Slice A authority tree, generated registry, registry/Go generators, handwritten/generated Go profile,
and profile tests are byte-identical to fixed Slice A candidate `495174bd8b78e3e6abb62bb1bae1f11ba6dc1999`.
The profile and registry identities therefore remain unchanged; Slice B runtime conformance enters only the mutable
Go-profile lock-input closure.

## Parser, signature and binding review

The compact-token admission is bounded before parsing and accepts only three non-empty canonical unpadded base64url
segments. The protected header is a duplicate-free closed three-member object; exact `RS256`, RSA-only JWK shape,
fixed exponent 65537, 2048..4096-bit odd modulus, `use=sig`, and `key_ops=[verify]` prevent algorithm, symmetric-key,
and token-selected-key confusion. Signature verification uses only Go `crypto/rsa`; no JOSE, HTTP, DNS, file, OIDC,
JWKS, provider, or database dependency is present.

The strict JSON scanner covers the complete object, decoded duplicate names, escape-equivalent names, invalid UTF-8,
unpaired surrogates, trailing input, root depth, decoded-size bounds, and integer-only NumericDate lexemes. Correct-
length signature failure precedes all signed-claim semantic mismatches. After signature success, the verifier binds
the exact issuer, snapshot-owned single audience, generated token profile, one clock read, every half-open token/key/
snapshot inequality, epoch, key/token revocation, tenant, optional project target, and required permission. Stable
error values retain only generated redacted categories.

The canonical trust and principal encoders use fixed ASCII member order, reject invalid UTF-8, sort set arrays by
unsigned UTF-8 bytes, omit absent optional/self-digest members, and use the frozen NUL-domain SHA-256 framing. Their
exact bytes and deterministic snapshot/principal digests are hard-pinned by the focused tests.

## Snapshot lineage and concurrency review

Snapshot construction performs the generated `0..32` prior-history and `1..32` candidate-key cardinality guard before
every length-derived allocation or JWK parse, then independently rejects a lifetime history above 32. RSA material,
revocation sets and history are deep-copied. A `kid` is permanently bound to one modulus; generation starts at one,
increments by exactly one, and generation two onward binds the immediately preceding snapshot digest.

Replacement serializes writers with a mutation mutex, validates a bounded candidate while the old generation remains
admitting, then detaches current admission before draining the old exclusive lease and publishing the new generation.
Readers acquire the current pointer and non-blocking read lease under the short state lock, so neither a pointer/lease
race nor the former writer-preference nested-consume cycle remains. Invalid candidates leave the old generation
active. Replacement and invalidation drain callback-held leases, and stale principals cannot reacquire their exact
generation. A 20-run race-enabled adversarial repetition of rotation, invalidation, nested consume, copy/tamper and
concurrent one-shot cases also passed.

## Opaque principal and surface review

`VerifiedPrincipal` has only package-private fields and no exported constructor. Its canonical self-binding covers all
actor, token, snapshot, time and authorization-context facts, while the shared atomic state makes the original and
ordinary shallow copies one-shot together. Nil callbacks, callback errors, panic, tamper, stale generation and failed
consume remain consumed. The only cross-package authority seam is the sealed callback-scoped
`ConsumeVerifiedPrincipal`; retained views fail their liveness check after callback return, and the exact generation
lease remains held through callback completion.

AST closure freezes exactly the three intended exports, every opaque principal/view field and receiver method, the
sealed view methods, and an exact standard-library import allowlist. It scans all control-plane production Go files
for verifier/consumer identifier references and found zero production callers. The contract-lock test dynamically
enumerates every non-generated authn Go source/test, so a new mutable runtime file cannot silently fall outside the
Go-profile input closure.

## Fresh checks and evidence boundary

Fresh independent checks used Node `24.13.1`, Bun `1.3.14`, Go `1.26.6`, and Gitleaks `8.30.1`:

- fixed commit/tree/parent/diff, candidate upstream `0/0`, remote equality and clean state: PASS;
- `internal/authn` normal test: PASS in `1.324s`;
- `internal/authn` race test: PASS in `3.210s`;
- `internal/authn` vet and build: PASS;
- focused registry plus contract-lock Vitest: 2 files, 26/26 tests PASS;
- dedicated identity-verifier registry and generated-Go checks: current;
- exact-toolchain final contract-lock check: current;
- candidate-range `git diff --check`: PASS;
- Slice A authority/generated/profile same-bits comparison: PASS; and
- candidate-range Gitleaks: PASS with zero findings across the one candidate commit.

The first final contract-lock invocation correctly refused ambient Go `1.26.7`; it is not counted as evidence. The
same command was rerun with the exact Go `1.26.6` binary and returned `platform-contract-lock: current`. No broad
`internal/migration` test, live PostgreSQL, production database, remote host, HTTP/OIDC/JWKS/P2/provider action,
deployment, publication, release, merge, or Gate command was run or is claimed.

## Final verdict

`APPROVE, P0=0/P1=0/P2=0` for fixed candidate
`6e79edabbd68177023ff1ce8a848f4a7dd3307fd` only. ADR-0025 Slice B's offline standard-library cryptographic kernel,
immutable-generation lease and opaque one-shot principal boundary satisfy the frozen Slice B contract. Slice C may
start only under the existing approval and remains responsible for removing every raw-actor bypass and holding this
lease through authorization, the exact typed operation, and transaction settlement. Production trust provisioning,
all external side effects, and all Gates remain open.
