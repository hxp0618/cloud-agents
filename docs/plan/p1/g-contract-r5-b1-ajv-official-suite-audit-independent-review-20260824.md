# G-CONTRACT R5-B1 Ajv official-suite audit independent review

Date: 2026-08-24

## Verdict

`APPROVE`

| Severity | Findings |
| -------- | -------: |
| P0       |        0 |
| P1       |        0 |
| P2       |        0 |

This is an independent, fixed-candidate code/evidence review. It approves the
deterministic non-Gate audit record only. It does not claim Ajv conformance,
remove a closure-profile missing item, or close any Gate.

## Fixed lineage and scope

- candidate branch: `codex/cloud-agents-p1-g-contract-ajv-audit-20260824`
- candidate commit: `7a0bf2a430c9572efba55b4de21e12893cb57b2f`
- candidate tree: `19d409da39d58e9b1db9435409e5ee173f176057`
- parent: `2c2222a845c3ec74d6a1400fee21f1242311933d`
- candidate diff SHA-256: `a48cb6d3e571867706d79056613d1912210fd1ceae28e79673cb7aafe9429f9a`

The review found no HTTP/P2/provider surface, production database write,
deployment, publication, or Gate transition in the candidate.

## Reviewed contract and implementation

The runner resolves `ajv@8.20.0`, verifies the installed package manifest digest
`1f9033ee5a6515e7d76938b7072941862d1ed228a6879cc7fe10cdeb75107989`,
and binds the same exact version in both `package.json` and `bun.lock`. Each of
the 383 official cases receives a fresh `Ajv2020` instance with these exact
options:

```json
{
  "allErrors": true,
  "strict": false,
  "validateSchema": true,
  "validateFormats": false,
  "ownProperties": true,
  "removeAdditional": false,
  "useDefaults": false,
  "coerceTypes": false
}
```

All 79 recursively sorted vendored remote schemas are registered as
`http://localhost:1234/<posixRelativePath>` with
`addSchema(schema, uri, false, false)`. The synchronous runner has no network
loader and does not use `loadSchema`. Repository-relative input resolution
rejects lexical escape, symlink components, symlink corpus entries, and
non-regular inputs. The corpus manifest binds path, content digest, and byte
size for the complete vendored tree.

The source and output schemas are strict. The generated report preserves 45
unique discrepancy identifiers plus case/test descriptions, expected/actual
validity, and stable error text as applicable. Only these four classes are
recorded as documented Ajv boundaries:

- non-hash `$dynamicRef` rejection;
- empty `enum` rejection;
- `__proto__` property filtering;
- vocabulary registration behavior.

The other 39 discrepancy records remain `OBSERVED_DIFFERENCE`; the audit does
not infer that they are intentional.

## Independent replay

Pinned runtimes used:

- Node.js `24.13.1`;
- Bun `1.3.14`;
- Go `1.26.6`;
- CPython `3.14.7`;
- uv `0.12.5`;
- jsonschema-rs `0.50.1`;
- openapi-spec-validator `0.9.0`.

The normal audit replay succeeded and was byte-identical to the checked-in
report:

```text
bun scripts/check-platform-ajv-official-suite.ts --check
platform-ajv-official-suite: current EXECUTED_NONCONFORMANT non-Gate audit
exit: 0
```

The fail-closed claim boundary was independently reproduced:

```text
bun scripts/check-platform-ajv-official-suite.ts --require-conformance
AJV_OFFICIAL_SUITE_AUDIT_CONFORMANCE_REQUIRED
exit: 1
```

The deterministic result was:

| Fact                   | Count |
| ---------------------- | ----: |
| mandatory files        |    46 |
| cases                  |   383 |
| assertions             |  1299 |
| remotes                |    79 |
| passed assertions      |  1241 |
| compile-failed cases   |     7 |
| not-run assertions     |    20 |
| validity mismatches    |    30 |
| runtime errors         |     8 |
| discrepancy records    |    45 |
| non-passing assertions |    58 |

The seven exact category rows were also reproduced:

| Category              | Compile cases | Not run | Mismatch | Runtime | Records | Non-passing |
| --------------------- | ------------: | ------: | -------: | ------: | ------: | ----------: |
| dynamicRef            |             3 |       6 |       15 |       4 |      22 |          25 |
| enum                  |             1 |       6 |        0 |       0 |       1 |           6 |
| properties            |             0 |       0 |        1 |       0 |       1 |           1 |
| ref                   |             3 |       8 |        0 |       0 |       3 |           8 |
| unevaluatedItems      |             0 |       0 |       10 |       2 |      12 |          12 |
| unevaluatedProperties |             0 |       0 |        3 |       2 |       5 |           5 |
| vocabulary            |             0 |       0 |        1 |       0 |       1 |           1 |

Focused Vitest replay passed `2` files and `26` tests. Contract-closure-profile
generation and generation-lock checking both reported current under the pinned
Go `1.26.6` runtime. An ambient default Go `1.26.7` invocation was rejected by
the lock's exact runtime check; using the pinned runtime passed. A deliberately
wrong installed Ajv package-manifest digest was also rejected with
`AJV_OFFICIAL_SUITE_AUDIT_SOURCE_INVALID`.

The independently provisioned Python standards pipeline reproduced the
separate jsonschema-rs result with no failures:

```text
official Draft 2020-12 mandatory suite:
  46 files / 383 cases / 1299 assertions / 79 remotes / 0 failures
current contract parity:
  56 schemas / 2 manifests / 75 fixtures / exact Ajv-jsonschema-rs parity
OpenAPI 3.1:
  2 documents / 9 operations / 0 failures
Python unit tests:
  10 passed
```

This independent 1299/1299 standards result is evidence for a later versioned
closure-profile v2 decision; it does not turn the Ajv audit itself into a
conformance pass.

## Immutability and Gate boundary

The following four `contract-closure-profile/v1` files are byte-identical to
the fixed parent:

| File                                                                                  | SHA-256                                                            |
| ------------------------------------------------------------------------------------- | ------------------------------------------------------------------ |
| `contracts/generated/platform/v1alpha1/contract-closure-profile-v1.json`              | `823e9356342511b611538fb669e8af99962555b153324d09c7208f3f00b51e68` |
| `contracts/platform/v1alpha1/fixtures/golden/contract-closure-profile-source-v1.json` | `411e4b649c5b812339817b5836c25a6a2f27c9aa0e24497b7aa65da8fe2baa49` |
| `contracts/platform/v1alpha1/schemas/contract-closure-profile-source-v1.schema.json`  | `8b87a0e24e42db87987a1dc1b4931b7ff2b8edef6bef6ccd184e0586a7bdc4af` |
| `contracts/platform/v1alpha1/schemas/contract-closure-profile-v1.schema.json`         | `107dbc21f240cd912f567ef1e0a6bfaf78d2e6171f0f4189ca9812c225630bc0` |

The generated v1 profile still derives exactly these three missing criteria:

1. `json-schema-2020-12-official-test-suite`
2. `runtime-server-path-and-tenant-authority-enforcement`
3. `remaining-generator-supply-chain-review`

The generation lock binds the Ajv audit inputs, full vendored corpus, generated
output digest and exact summary while retaining `notGateClosure: true`,
`closureCriterion: REMAINS_MISSING`, and `gateStatus: ALL_GATES_OPEN`.

## Commands replayed

```text
git show -s --format='%H%n%T%n%P' HEAD
git diff 2c2222a845c3ec74d6a1400fee21f1242311933d 7a0bf2a430c9572efba55b4de21e12893cb57b2f | shasum -a 256
bun scripts/check-platform-ajv-official-suite.ts --check
bun scripts/check-platform-ajv-official-suite.ts --require-conformance
bun node_modules/vitest/vitest.mjs run scripts/lib/platform-ajv-official-suite.test.ts scripts/lib/platform-contract-lock.test.ts
bun scripts/generate-platform-contract-closure-profile.ts --check
bun scripts/generate-platform-contract-lock.ts --check
bun scripts/check-platform-contract-standards.ts
git diff --exit-code 2c2222a845c3ec74d6a1400fee21f1242311933d 7a0bf2a430c9572efba55b4de21e12893cb57b2f -- <four-v1-files>
```
