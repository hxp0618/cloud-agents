# G-CONTRACT R5-B1 Ajv official-suite audit

Date: 2026-08-24

## Scope and lineage

- fixed implementation parent: `2c2222a845c3ec74d6a1400fee21f1242311933d`
- R5-A implementation: `d4bb96a7ab15138cedfb0753a44ebe8bf3488adf`
- R5-A independent review: APPROVE, P0/P1/P2 = 0/0/0
- slice: deterministic offline Ajv 8.20.0 audit evidence only
- status: `EXECUTED_NONCONFORMANT`
- conformance claim: `false`
- `contract-closure-profile/v1`: byte-immutable frozen pre-audit snapshot; `json-schema-2020-12-official-test-suite` remains missing
- Gate status: unchanged; all Gates remain open

No HTTP/P2/provider surface, production database write, deployment, publication, or Gate closure is introduced.

The new audit pipeline supersedes only the current runner-status fact (`NOT_RUN_NOT_CLAIMED` to `EXECUTED_NONCONFORMANT`). It does not rewrite the frozen v1 reason/boundary and does not change its exact three-item formal missing classification. A separately versioned closure profile v2 and independent review are required to reconcile the current audit fact into that profile.

## Versioned execution contract

The source profile and generated report are:

- `contracts/platform/v1alpha1/fixtures/golden/ajv-official-suite-audit-source-v1.json`
- `contracts/generated/platform/v1alpha1/ajv-official-suite-audit-v1.json`

The runner creates a fresh Ajv instance for every official test case with the exact options below:

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

All 79 vendored remotes are registered at `http://localhost:1234/<posixRelativePath>` using `addSchema(schema, uri, false, false)`. Network fetch and `loadSchema` are forbidden. The resolved Ajv package must be exactly `ajv@8.20.0`, its package-manifest digest is versioned, and `package.json` plus `bun.lock` must bind the same version.

## Observed result

| fact                   | count |
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

The generated report preserves every discrepancy identifier, description, expected/actual result, and stable error message. It classifies only four documented Ajv boundaries: non-hash `$dynamicRef`, empty `enum`, `__proto__` property filtering, and vocabulary registration behavior. All other discrepancies are labeled `OBSERVED_DIFFERENCE`; this slice does not infer that they are intentional.

Category totals are locked as follows:

| category              | compile cases | not run | mismatch | runtime | records | non-passing |
| --------------------- | ------------: | ------: | -------: | ------: | ------: | ----------: |
| dynamicRef            |             3 |       6 |       15 |       4 |      22 |          25 |
| enum                  |             1 |       6 |        0 |       0 |       1 |           6 |
| properties            |             0 |       0 |        1 |       0 |       1 |           1 |
| ref                   |             3 |       8 |        0 |       0 |       3 |           8 |
| unevaluatedItems      |             0 |       0 |       10 |       2 |      12 |          12 |
| unevaluatedProperties |             0 |       0 |        3 |       2 |       5 |           5 |
| vocabulary            |             0 |       0 |        1 |       0 |       1 |           1 |

## Checker behavior

- normal audit checking succeeds only when a fresh replay is byte-identical to the checked-in report and its exact versioned totals/categories;
- `--write` regenerates only after the replay matches the versioned `EXECUTED_NONCONFORMANT` expectation;
- `--require-conformance` checks report currency, then exits nonzero because conformance is not established;
- the generation lock binds the source, schemas, runner, tests, documentation, package/lock authority, complete vendored corpus, generated output, and exact result summary.

Focused verification commands:

```text
bun scripts/check-platform-ajv-official-suite.ts --check
bun scripts/check-platform-ajv-official-suite.ts --require-conformance
/tmp/codex-cloud-agents-bun-1.3.14/bun-darwin-aarch64/bun node_modules/vitest/vitest.mjs run scripts/lib/platform-ajv-official-suite.test.ts scripts/lib/platform-contract-lock.test.ts
bun scripts/generate-platform-contract-closure-profile.ts --check
bun scripts/generate-platform-contract-lock.ts --check
```

The conformance command is expected to fail with `AJV_OFFICIAL_SUITE_AUDIT_CONFORMANCE_REQUIRED`; that failure is evidence of the fail-closed claim boundary, not a passing Gate result.
