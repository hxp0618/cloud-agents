# P1 runner ledger recovery generated-profile implementation - 2026-08-22

- Status: **SLICE A IMPLEMENTED; FIXED CANDIDATE AND INDEPENDENT REVIEW PENDING**
- Base commit: `999c392ea323ef9d89a65ac5add6ce6b3041023d`
- Decision: [`D-047 / ADR-0023`](runner-ledger-recovery-contract-decision-20260822.md)
- Pre-implementation audit:
  [`runner-ledger-recovery-contract-audit-20260822.md`](runner-ledger-recovery-contract-audit-20260822.md)
- Scope: generated registries, ordinary package-private Go profiles, fixtures, manifests, and generation lock only

This slice does not create or consume a recovery claim or permit, open a database handle or transaction, execute SQL,
append ledger/evidence, call `Runner.Run`, or implement an entry/recovery writer. It does not add HTTP/P2/provider
behavior and does not authorize production database writes, deployment, publication, release, main merge, or any Gate
closure.

## 1. Closed generated identities

The generated suite contains eight independent versioned profiles. The common admission profile preserves the exact
ordered twelve-pair mapping from the immutable consumer-v1 registry. Its action profiles contain pair counts
`[12, 4, 1, 1, 1, 3, 0, 2]`; the recovery success writer has no direct consumer pair and binds only the exact distinct
recovery execution-admission profile.

| Profile                                         | Generated file SHA-256                                             | Registry digest                                                           | Profile digest                                                            |
| ----------------------------------------------- | ------------------------------------------------------------------ | ------------------------------------------------------------------------- | ------------------------------------------------------------------------- |
| `runner-ledger-recovery-admission/v1`           | `c9ba44911eca4e5130e8ab45ea942602c93515e6f504d6daa9b730b045f59f68` | `sha256:593c0199a8f4e59a6b29cb0b9f49894eef2911cb9fe9bf69b70eb15ef79d658a` | `sha256:b92e6352d7bbfa0e607bcde06a7c4fe7e508adc6b954c9a048584da571014869` |
| `runner-ledger-abort-terminal-writer/v1`        | `48d580679c4dcf8ec2dac3fefa5d9f2c56f58d53fdbcb2f6fa3155eb1db1918c` | `sha256:0ab975d0e07b5bc089304189430fadd94d67ba2ca818f5dba934e6acf2db9a9c` | `sha256:51ed8da74a752291adda6ba3de8be9e864d7165b83a8279c1c02e02987a7413d` |
| `runner-ledger-commit-observation-writer/v1`    | `eda98b19d442f1025d3fc5c560f113417b105f11f82fb3fc68a4e70847919fb5` | `sha256:f7c5d1e357a7cd5cbc1d93d06cb8a82114cc5a401a3b95ddaa468d0e173cf8e0` | `sha256:4c741b8aebc58245230d2f704393c5f563a416db05c9ac49126834377596dc31` |
| `runner-ledger-ambiguous-resolution-writer/v1`  | `15d1fece1b98ffa6d224c1ae03a1f6d6cc856e99ad0878d3bc5628f12e643537` | `sha256:9220c0bcd9936f2ab4eb7a7f81f02f065823dc1450bf0ea330e574ac96da137c` | `sha256:2bdfc33ba4afcdbaac5d93bf5d087a604bfe7bdc05a41fd027d221eff50df691` |
| `runner-ledger-retry-handoff/v1`                | `939b6a2b178cb93b4ec3234e52d55991b70ea8233b82d602c366feed7edfe231` | `sha256:aab7a060dcddfa89ef662085446a011133229f07ccd09cea49e1717e81e6cbde` | `sha256:6aac4d794192e4c46d55cd2b525a96defda4ec09e3b0b3f2707889223109da0a` |
| `runner-ledger-recovery-execution-admission/v1` | `3bb2e583a992a6ddc639d364d1592b46e2a453035d9fbc5baa892bbdda0122d1` | `sha256:32a68d940653a010f52c45d22c58d1e7ab6c7441bdf1bcd1375b89f411742143` | `sha256:469f0362803911717f68f379214a149fab554589b94a8877578c0cd9c74ee86d` |
| `runner-ledger-recovery-success-writer/v1`      | `60af98c818427bb7ba6d73a09e37e6f80d22eba2828c5dc41dcae642fffc5754` | `sha256:f8126fe29dc3b51d5dae9fcf22ee6af553d91e829e8578857232949a152b3b4b` | `sha256:64bf873dcc1e68b57d9ec15719e4da9b5d9c3e9e0059dd2231d4f32bef6a300d` |
| `runner-ledger-return-failure/v1`               | `33c6dc3b19080801335d2eb0fd135a5e67ab680c810e7173df6d2a41bea471b6` | `sha256:bd6bd2b5c0534a25a1fa6c1f76808a6691154ae7bcf795298bfd6ed0c6b07f13` | `sha256:b7b80f60b702bc64c5e1c5e26771f00db5687c0ff96c721cccdc088de587d4fe` |

The source fixture and its source/output schemas have SHA-256 `3579c8d0...21540`, `9424f65d...5ed0`, and
`d6cbb6ea...f4d5`. The generated Go ordinary profile has SHA-256 `84895679...c3b`. Its static AST test rejects any
production selector/profile consumer outside the generated/profile validation files and rejects `database/sql`,
`pgx`, or `net/http` imports.

## 2. Generation-lock closure

`contracts/generation.lock.json` has SHA-256 `21d676584a505ad4e4c4198edb202a5cef1454339ab6b4f5e79823be634437df`
and adds two `notGateClosure: true` pipelines:

| Pipeline                                             | Input manifest SHA-256                                                    | Outputs                                   |
| ---------------------------------------------------- | ------------------------------------------------------------------------- | ----------------------------------------- |
| `runner-ledger-recovery-registry-suite-generation`   | `sha256:aa1ae1c6b0509cb390018a9fe90216db984b67635a768e3029b389fc768ca7ff` | eight generated registries above          |
| `runner-ledger-recovery-go-profile-suite-generation` | `sha256:0e96a9277029bcf6e45f4baf9554d6965711b7a8118eb2156046dc13b3f4fd5e` | `sha256:84895679...c3b` ordinary Go facts |

The lock binds the exact Node `24.13.1`, Bun `1.3.14`, and Go/gofmt `1.26.6` toolchain declarations. Both summaries
keep runtime claims and all entry/recovery writers `NOT_IMPLEMENTED`, production consumers absent, external surfaces
forbidden, production database writes/deployment/publication unauthorized, and every Gate open.

## 3. Historical same-bits

The focused TypeScript and Go tests hard-bind the 24 historical preflight, consumer, entry-admission,
entry-execution-admission, and entry-success-writer source/schema/generated/profile artifacts. All remain byte-identical.
The shared contract manifest and SDK manifest comments changed only because the new source schemas/fixtures are now
included in the generated contract inventory; no existing runner v1 registry/profile bytes changed.

## 4. Local validation

The successful checks below used the exact repository toolchains where applicable:

- `bun run platform:contracts:check`: PASS/current; `118` JSON files, `52` schemas, and `71` fixture cases;
- recovery registry focused test: `6/6` PASS with `94` assertions, including generator-to-`oxfmt 0.62.0`
  byte parity for all eight new registry outputs;
- contract-lock test: `17/17` PASS with `97` assertions;
- focused recovery Go profile normal and race: PASS; migration-package `vet` and `build`: PASS;
- repository TypeScript typecheck: PASS;
- registry and Go generators `--check`: PASS/current; and
- target formatting, repository lint, Markdown links, and `git diff --check`: PASS. The formatter claim covers every
  changed/new format-eligible file and all eight new recovery registries; it does not claim that two unchanged,
  parent-existing runner-entry registries are formatter-clean;
- candidate-range Gitleaks `8.30.1`: PASS over approximately `319.85 KB`, no leaks found; and
- no full migration, broad race, live PostgreSQL, production database, or external-side-effect test was run or is
  claimed for this generated-contract-only slice.

The fixed candidate identity plus clean/upstream state remain to be recorded after commit and push. A timeout or a
still-running process is not PASS.

The first frozen candidate `7f4ab2251efd1aa718e61078bb9efa26d796c92b` was independently blocked because five
new generated registries were not `oxfmt`-clean while this record claimed target formatting PASS. No APPROVE record
was created. The superseding candidate makes formatter parity a generator invariant and tests all eight outputs.

## 5. Next boundary

Freeze and commit this Slice A candidate, then obtain an independent read-only `P0/P1/P2` review. Only an `APPROVE`
fixed candidate may begin Slice B. Slice B may add same-verifier replay and action-specific close-only admission permits;
it still may not implement any writer, public recovery result, production database write, HTTP/P2/provider effect,
deployment, publication, release, main merge, or Gate closure.
