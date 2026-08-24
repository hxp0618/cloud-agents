# P1 offline identity-verifier Slice A independent review — 2026-08-23

- Verdict: `APPROVE`
- Severity: `P0=0 / P1=0 / P2=0`
- Fixed candidate: `495174bd8b78e3e6abb62bb1bae1f11ba6dc1999`
- Candidate tree: `1c3fe12064b42bff605838e711ebb16e03083cfd`
- Candidate parent: `15a45f44bdc753236e5954b706dcb13f3d5314ba`
- Candidate diff SHA-256: `34a87e2db78d5cd8bc7ae10cca76797b9e9db4cfd5c5950cb2483090bb3bee7d`
- Candidate branch: `codex/cloud-agents-p1-identity-verifier-slice-a-20260823`
- Decision: [`ADR-0025`](../adr/0025-p1-offline-jwt-access-token-verifier-contract.md)
- Candidate record:
  [`identity-verifier-profile-implementation-20260823.md`](identity-verifier-profile-implementation-20260823.md)

This is an independent, read-only review of ADR-0025/D-049 Slice A. Approval applies only to the fixed candidate
above. It makes Slice B eligible under the existing owner approval, but implements neither Slice B nor Slice C and
does not authorize a merge, production trust provisioning, database access or writes, HTTP/P2/provider behavior,
deployment, publication, release, or Gate closure.

## Fixed identity and scope

The candidate worktree was clean, was `0/0` relative to its upstream, matched its remote branch exactly, and resolved
to the supplied HEAD, tree, parent, and candidate-diff hash. The candidate contains exactly 25 changed paths: the
closed source/output schemas, manifest and four fixtures; one generated registry; deterministic registry and Go
profile generators/tests; three package-private `internal/authn` profile files; generation-lock integration; and
bounded plan records.

The review independently reproduced these exact identities:

- profile digest: `sha256:1846e974ad3efc192704e4409f1d97786e3fb7df9de17e5ea2f2024d729b3c07`;
- registry digest: `sha256:654d5f8d20dfd1fcf8a9da3a06dd445d46f813f5c60d926ce3b3a00cd9eccde1`;
- generated registry file: `474bb31fa5721dd20fc5723b790f39d45fda5ac0392d9e5bb73cb0ecef3e0ccf`;
- handwritten Go profile: `dc7ae1c52473eabf5167831a0afcd6b8aa01f399f1015251f2801c88b474bb6f`;
- generated Go profile: `e3d9ed08b69b3a7f4ce0ac6d100ea49f577dafdf857ff33be32c4170c357b8de`;
- Go profile test: `4da351c0ae44d8de8a9740cc6c6bace3f79e080c47c6b97b141707b1a6d70406`;
- generation lock: `9bf85b0a8fcdbce751889c576135ea7673002e236bda129cdef3533eb619385c`.

## Generated registry and authority review

The editable authority is confined to `tools/platform-identity-verifier/v1`; the only generated public-tree artifact
is the registry below the already excluded `contracts/generated/` subtree. Both schemas are closed. The source schema
freezes every profile fact with exact constants, while the output schema adds only the two lowercase SHA-256 digest
members. The exact manifest binds one golden and three negative fixtures and the generator rejects any inventory
addition, removal, non-regular file, or symlink.

Authority reads resolve every path component below the real repository root, reject lexical and realpath escape,
reject symlinks and non-regular entries, decode through fatal UTF-8, and scan the complete JSON text before
`JSON.parse`. The recursive scanner rejects trailing input and duplicate decoded member names, including
escape-equivalent names; RFC 8785 canonicalization then rejects lone surrogates and unsupported I-JSON values. The
focused tests exercise invalid UTF-8, a lone surrogate, a symlinked authority component, an escape-equivalent
duplicate, collection order, semantic mutations, boundary drift, and digest drift.

An independent canonicalizer reproduced both generated digests from the complete closed projections using
`SHA-256(UTF8(domain) || 0x00 || RFC8785(payload))`. The NUL-framed profile digest matched the recorded value; the
newline-framed negative was instead
`sha256:e9f941add05e93878f361ec200c0ad833389b2bc5a2b587f5ad850427daa9fef`. The profile therefore does not inherit the
older newline-framing ambiguity.

## Package-private Go profile review

The Go profile is a closed comparable value composed only of unexported structs, strings, fixed arrays, and unsigned
integers. Its production files import nothing. The generated file declares only two unexported digest constants and
one unexported function that returns a fresh composite literal; it contains no package-level `var`. The handwritten
`valid` method compares the candidate against another fresh generated literal, so mutating a prior copy cannot mutate
the authority baseline.

The AST and mutation tests reject exports, imports, extra functions, mutable generated declarations, zero values,
whole-value tampering, caller-selected algorithms, and a forged former baseline. The three new production-package
paths contain no token parser, signature verification call, RSA/JWK runtime object, trust snapshot, verification
context, verified principal, binder, HTTP handler, SQL/database handle, provider dependency, or production
constructor. Names such as `identityVerifierTrustSnapshotRules` are generated profile facts only, not runtime trust
objects.

## Generation DAG and historical identity review

The two lock pipelines form the exact acyclic direction:

```text
13 regular source/schema/fixture/generator inputs
        -> generated registry
        -> 18 regular Go-profile inputs
        -> generated package-private Go facts
        -> generation lock
```

Neither pipeline lists its own output or `contracts/generation.lock.json` as an input. The Go pipeline consumes the
registry output and the handwritten Go shape/test. All 18 unique inputs across the pair are regular non-symlink
files. The lock builder checks both generated artifacts read-only; only its explicit `--write` orchestration performs
registry then Go then lock writes.

The parent generation lock enumerated exactly 100 historical outputs. Independent comparison found zero byte, Git
mode, or parent-recorded SHA-256 mismatches. Root `package.json` is byte-identical to the parent
(`718d89ec962503c0d97df4b03ee068726f1ed7d9f03630d91ac756667595e9c1`), and the public source-contract manifest
remains exactly `sha256:eb51453861feb6685eadcd335c0620fea5ca98de9058a9a3d3f5198f6e67406e`.

## Checks and evidence boundary

Fresh independent checks used Node `24.13.1`, Bun `1.3.14`, and Go `1.26.6`:

- fixed HEAD/tree/parent/diff, clean candidate, upstream `0/0`, and remote-exact branch: PASS;
- focused registry plus contract-lock Vitest: 2 files, 26/26 tests PASS;
- dedicated registry generator, Go-profile generator, and final contract-lock checks: current;
- `internal/authn` normal test: PASS in `0.554s`;
- `internal/authn` race test: PASS in `1.570s`;
- `internal/authn` vet and build: PASS;
- candidate-range `git diff --check`: PASS;
- independent NUL-framed digest recomputation and exact file hashes: PASS;
- parent-lock 100-output same-bits/mode/hash, unchanged public manifest, and unchanged package file: PASS.

The first focused Vitest attempt in the fresh review worktree reached zero tests because dependencies were absent and
a transient `bun x` could not resolve the locked Ajv package. It made no tracked change and is not counted as
candidate evidence. After `bun install --frozen-lockfile`, the exact pinned 26-test rerun passed.

Gitleaks `8.30.1` did not return zero findings: the single candidate commit produced exactly 13
`generic-api-key` findings. Independent line review confirmed that all 13 are false positives over only two immutable
profile rule strings: six copies of field `clientIdAndTokenId` with value `non_empty_valid_utf8_no_c0_del`, and seven
copies of field `keyAndLineageOrdering` with value `kid_unsigned_utf8_byte_sort`, repeated across source, schema,
negatives, generated registry, and generated Go facts. They are lexical/ordering rules, contain no credential or
secret material, and no other rule or value was reported.

The wider aggregate `platform:contracts:check` PASS remains candidate-record evidence and was not independently
repeated in this fixed review. No broad `internal/migration` test, live PostgreSQL, production database, remote host,
HTTP/P2/provider action, deployment, publication, release, merge, or Gate command was run or is claimed.

## Final verdict

`APPROVE, P0=0/P1=0/P2=0` for fixed candidate
`495174bd8b78e3e6abb62bb1bae1f11ba6dc1999` only. ADR-0025 Slice A's generated registry/package-private profile
boundary is complete and Slice B may start under the existing approval. Slice B and Slice C remain unimplemented;
production trust provisioning and every external side effect remain unauthorized; all Gates remain open.
