# Gate candidate record: `G-CONTRACT` / P1 / R3

- Evidence ID：`CAG-G-CONTRACT-P1-20260823-R3`
- Record type：`PHASE`
- Phase / aggregate Gate：P1 contract foundation / `G-CONTRACT`
- Prerequisite record IDs：`CAG-G-INVENTORY-P0-20260810-R3`、`CAG-G-BASELINE-P0-20260810-R3`
- Supersedes：`CAG-G-CONTRACT-P1-20260823-R2` after independent review returned
  `BLOCK, P0=0/P1=1/P2=1`
- Status：`IN PROGRESS`
- DRI：hxp0618（owner）；Codex current-source evidence executor
- Independent reviewer：`PENDING`
- Date：2026-08-23 UTC / Asia/Shanghai
- Gate effect：none；this is a repaired non-Gate candidate, not an immutable closure signature

## Scope and review repair

R3 preserves R2's exact official JSON Schema corpus, independent `jsonschema-rs`/OpenAPI validators, current
contracts, generated artifacts and production-runtime boundary. It repairs the two fixed-candidate findings from R2's
independent review:

1. R2 hard-coded an Ajv official-suite `53`-difference count even though no checked-in Ajv official-suite runner or
   result artifact existed. R3 deletes that count and binds `Ajv 8.20.0` official-suite status to the exact closed value
   `NOT_RUN_NOT_CLAIMED` in the standards profile, generated lock and fail-closed tests. Ajv continues to validate all
   current checked-in fixtures; only `jsonschema-rs` runs the vendored official suite.
2. R2's Python invocation could create source-tree `__pycache__` files. R3 invokes both Python entrypoints with `-B`
   and adds a test requiring zero `__pycache__` directories and zero `.pyc` files under the checked-in standards root.

R2 remains in history as the exact candidate that was blocked. R3 does not reinterpret its unverified numeric claim as
evidence.

## Fixed implementation source

- Repair implementation commit：`e14780dff612f0d5ddf1513b1c0ae3bac6e9149a`
- Source tree：`ec9eb00cb7ae98161c04823f2fed8897a51b18e6`
- `services/control-plane` subtree：`c1d678f708ec231b446a11e46572a11fccefc97c`
- Source branch：`codex/cloud-agents-p1-g-contract-standards-entry-20260823`
- Source state before this evidence-only change：clean；upstream `0/0`；remote branch exact
- R2 evidence commit / record SHA-256：`fe8f9919f437393feb11709500b6c45154b6a14a` /
  `ca9754117bdaded4a4d8ea107933725b517934ffc6c5da52607f80d0eeed9909`
- Gate criteria SHA-256：`4a3d0b3c184e9673944411adbc5c8ea933883c855d5aada67862dad8e4dcc994`
- Inventory prerequisite record SHA-256：`d865bb9fd1195342a7251bd6822130437753f5be4af8312d9d5c5ee4d5b71400`
- Baseline prerequisite record SHA-256：`d8953c34b912757591881b4363fbb67855b2e578d669de4f697c97cbcb57ffd2`
- Deployment profile：none

No JSON Schema, OpenAPI document, Proto source, descriptor/baseline, fixture, generated SDK or control-plane runtime
file changed in the R2-to-R3 repair.

## Contract, standards and tool identities

- `contracts/generation.lock.json` SHA-256：
  `c1554d88236a11c7612571822fc9294b39a3b447257908739fbdeffc3f044e35`
- Source-contract manifest：
  `sha256:eb51453861feb6685eadcd335c0620fea5ca98de9058a9a3d3f5198f6e67406e`
- Toolchain-authority manifest：
  `sha256:99c6358f0e8e3546bfb5f178f1e2a780aab0257fadeca17643b294a62106ca2a`
- Standards pipeline input/source manifest：
  `sha256:eeb0ef55b6d9ea487d2ce1a3e3bc1c72818abead4c97b5b6363884fd9d0e7695`
- Standards profile SHA-256：
  `0bd4348680cf48819658651539d4777c412e7a3fb93c380ffacf536c675f440d`
- Wrapper / checker / checker-test SHA-256：
  `2594cea982c0fd865cedfd6871872ae3bca69f822c02ec085002464c67673594` /
  `a0b0f2bc3315649cd8b09a8461c68eaab5210c80004b5316b417670fa523486d` /
  `bfa74b6869c2f4203d8db52280f11b22ea548d4b6818f3807641f38ad2d97cdd`
- Contract-lock source / test SHA-256：
  `7f64661352ccbff3a37768ac962d329125cd56f6f085944da883f7e0390851f5` /
  `d17cca27051b2001c84f7cf3992ff8ea3851a6e278abc5bab99e5e081ac0cf97`
- `pyproject.toml` / `uv.lock` SHA-256：
  `b0a8b81937d783f1021e72f788f2b567769000ccef1bef044a2cb2b783646fb6` /
  `485c89d8f6bc03cc9eecf37003854d452439bd37b0252d43dcb1f8474cef6d49`
- Standards dependency-review SHA-256：
  `62c66d64986e8fea38fb712cb091ae5e41aa1792823370616629acc36c39703d`
- Proto descriptor / exact breaking baseline SHA-256：
  `cd896a7aaccf70426be94314c5cfa09733a6908c33867c1f8942b4eb6b179218` / same bytes

Official JSON Schema Test Suite identities remain R2 same-bits: upstream commit
`ea54899edb898f4cd99fb0f778e0856e7d337c8f`, tree
`b41de18d1ce464a942ca78fd0e286cd726e9090f`, mandatory tree
`8caa839664321324a31424adf0dae3811a73e6da`, 126-file corpus manifest
`d69af29cbaf4c7ffd1f8577c986294c51ec4efb177ebc340022978043b2a88a1`, and MIT license SHA-256
`837402bd25fad9b704265801ca3f92566a98157c1f9a7acd6f446299ba1c305a`.

## Repaired execution results

The fixed environment was Node `24.13.1`, Bun `1.3.14`, Go `1.26.6 darwin/arm64`, Python `3.14.7`, uv `0.12.5`,
oxfmt `0.62.0` and Gitleaks `8.30.1`.

The exact combined command used the previously retained hash-verified local wheelhouse:

```bash
CLOUD_AGENTS_CONTRACT_STANDARDS_WHEELHOUSE=/private/tmp/codex-contract-wheelhouse \
  bun run platform:contracts:standards:check
```

Result：PASS. The production Ajv/in-repo phase returned `BOOTSTRAP_VALIDATED`, `notGateClosure=true`, and
`118/52/2/3/71/9`. The independent phase returned `INDEPENDENT_CONTRACT_STANDARDS_VALIDATED`, official suite
`46/383/1299/79`, current fixtures `52/2/71`, OpenAPI `2/9`, and zero failures. Ten Python tests passed, including the
new no-bytecode and no-unearned-Ajv-claim checks. Source-tree `__pycache__`/`.pyc` counts were zero before and after the
command.

An earlier invocation without the approved wheelhouse waited for a network wheel download and was interrupted with
exit `130`; it is explicitly not counted as PASS. The succeeding local-wheelhouse run installed the exact lock-bound
`jsonschema-rs 0.50.1` universal2 wheel and completed in the temporary venv.

Lock and deterministic-source checks:

```bash
bun test scripts/lib/platform-contract-lock.test.ts
bun scripts/generate-platform-contract-lock.ts --check
oxfmt --check <changed JSON/TypeScript/Markdown paths>
git diff --check
gitleaks protect --staged --no-banner --redact --source .
```

Result：PASS. Lock tests passed `18/18` with `103` assertions, the generation lock reported `current`, formatting and
diff checks passed, and the repair's staged 4.05 KB scan found no leak. The generated lock carries both
`dialects.jsonSchema.productionAjvOfficialSuiteStatus = NOT_RUN_NOT_CLAIMED` and the same status in the standards
pipeline output summary.

## Exit criteria and open boundary

| Criterion                                     | R3 candidate result                   | Evidence                                                                               |
| --------------------------------------------- | ------------------------------------- | -------------------------------------------------------------------------------------- |
| production current schema/semantic fixtures   | PASS                                  | Ajv/in-repo `118` JSON, `52` schemas, `2` manifests, `71` cases                        |
| official JSON Schema mandatory suite          | PASS AS CANDIDATE EVIDENCE            | jsonschema-rs `46` files, `383` cases, `1,299` assertions, `79` remotes, zero failures |
| production Ajv full official-suite compliance | `NOT_RUN_NOT_CLAIMED`                 | no checked-in Ajv official-suite runner/result; no numeric failure count               |
| independent current-schema replay             | PASS AS CANDIDATE EVIDENCE            | `52` schemas and `71` cases match expected validity                                    |
| independent OpenAPI 3.1 validator             | PASS AS CANDIDATE EVIDENCE            | two OpenAPI `3.1.1` documents, nine operations, zero errors                            |
| source-tree side-effect closure               | PASS                                  | Python `-B`, explicit zero `__pycache__`/`.pyc` test, before/after zero                |
| remaining generator supply-chain review       | IMPLEMENTED CANDIDATE, REVIEW PENDING | exact package hashes/licenses recorded; immutable final review not signed              |
| immutable current-source independent review   | PENDING                               | no independent reviewer has signed R3                                                  |

The generation lock deliberately retains the same seven-entry formal `missing` list. This record does not remove a
formal gap, mark Ajv official-suite compliance, or convert candidate evidence into a Gate signature.

## Non-claims

- The standards packages and official corpus remain test-only and have no production import or second model authority.
- No current vulnerability result, Python/uv executable review, all-platform wheel closure, production database write,
  HTTP/P2/provider effect, deployment, publication, release, full migration, live PostgreSQL or Gate closure is claimed.
- R3 requires a fresh independent P0/P1/P2 verdict before it can replace reviewer `PENDING`.
