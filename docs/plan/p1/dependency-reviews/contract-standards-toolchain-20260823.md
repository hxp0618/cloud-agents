# P1 G-CONTRACT standards validator toolchain - 2026-08-23

- Status: **IMPLEMENTED CANDIDATE - INDEPENDENT REVIEW PENDING**
- Scope: isolated, test-only JSON Schema Draft 2020-12 and OpenAPI 3.1 validation
- Runtime boundary: no package in this closure is imported by a production Go or TypeScript module
- Gate boundary: non-Gate evidence only; all aggregate and immutable Gates remain open

## Decision

The production JSON contract checker remains Ajv 8.20.0 plus the existing in-repo semantic rules. This candidate
adds an independent standards environment under `tools/contract-standards`; it does not replace the production
validator or create a second contract authority.

The independent JSON Schema oracle is `jsonschema-rs 0.50.1`. Against upstream
`json-schema-org/JSON-Schema-Test-Suite` commit `ea54899edb898f4cd99fb0f778e0856e7d337c8f`, it passes the 46 mandatory
Draft 2020-12 files, 383 cases and 1,299 assertions with zero failures. The same run records Ajv 8.20.0's 53
official-suite differences rather than claiming Ajv itself is fully conformant. All 52 current schemas and 71
checked-in schema fixture cases must produce the expected result under both the production path and the independent
engine.

The independent OpenAPI checker is `openapi-spec-validator 0.9.0`, explicitly using its OpenAPI 3.1 validator. It
validates the two checked-in OpenAPI 3.1.1 documents and all nine operations with local file `$ref` resolution.

The upstream official-suite bytes are vendored as test data only. The profile fixes upstream commit/tree identities,
the mandatory draft tree, the MIT license bytes, a 126-file corpus manifest, and all expected cardinalities. Optional
suite cases are not included and are not claimed. A path-scoped `.gitattributes` whitespace exemption preserves four
upstream trailing spaces byte-for-byte; it does not apply to project source or generated contract files.

## Exact environment and dependency closure

The environment is fixed by `Bun 1.3.14`, `Python 3.14.7`, `uv 0.12.5`,
`tools/contract-standards/pyproject.toml`, and `tools/contract-standards/uv.lock`. The wrapper checks the lock,
first runs the production Ajv/in-repo fixture validator, exports hash-pinned requirements directly from that lock,
creates a temporary venv, and runs `uv pip sync` with
`--require-hashes`, `--no-build`, and `--strict`. Python downloads are disabled. An absent compatible wheel fails
closed; the check never downloads a Python runtime or builds an sdist. The temporary venv is deleted on every exit.
An approved local wheel cache may be supplied through `CLOUD_AGENTS_CONTRACT_STANDARDS_WHEELHOUSE`; hashes exported
from `uv.lock` remain mandatory, so the cache cannot substitute different package bytes.

Primary packages:

| Package                    | Version  | License      |
| -------------------------- | -------- | ------------ |
| `jsonschema-rs`            | `0.50.1` | MIT          |
| `openapi-spec-validator`   | `0.9.0`  | Apache-2.0   |
| `openapi-schema-validator` | `0.9.0`  | BSD-3-Clause |
| `jsonschema`               | `4.26.0` | MIT          |

The remaining exact tool-only closure is:

| Packages                                             | Versions                                | License expressions from installed distribution metadata |
| ---------------------------------------------------- | --------------------------------------- | -------------------------------------------------------- |
| `annotated-types`, `attrs`                           | `0.8.0`, `26.1.0`                       | MIT; MIT                                                 |
| `jsonschema-path`, `jsonschema-specifications`       | `0.5.0`, `2025.9.1`                     | Apache-2.0; MIT                                          |
| `lazy-object-proxy`, `pathable`                      | `1.12.0`, `0.6.0`                       | BSD-2-Clause; Apache-2.0                                 |
| `pydantic`, `pydantic-core`, `pydantic-settings`     | `2.13.4`, `2.46.4`, `2.15.0`            | MIT; MIT; MIT                                            |
| `python-dotenv`, `PyYAML`                            | `1.2.3`, `6.0.3`                        | BSD-3-Clause; MIT                                        |
| `referencing`, `rfc3339-validator`, `rpds-py`, `six` | `0.37.0`, `0.1.4`, `2026.6.3`, `1.17.0` | MIT; MIT; MIT; MIT                                       |
| `typing-extensions`, `typing-inspection`             | `4.16.0`, `0.4.4`                       | PSF-2.0; MIT                                             |

`uv.lock` fixes every accepted wheel and sdist URL, SHA-256, size, dependency marker and Python requirement. For the
reviewed macOS arm64 run, the `jsonschema-rs 0.50.1` abi3 universal2 wheel SHA-256 is
`300b154ded2be928d68b0f0408e0590b7e99af37019374fbe096b45bcd5eede1`. The source distribution is not executed by
the checked command.

## Evidence identities

| Evidence                     | SHA-256 / identity                                                 |
| ---------------------------- | ------------------------------------------------------------------ |
| official suite commit        | `ea54899edb898f4cd99fb0f778e0856e7d337c8f`                         |
| official suite tree          | `b41de18d1ce464a942ca78fd0e286cd726e9090f`                         |
| mandatory Draft 2020-12 tree | `8caa839664321324a31424adf0dae3811a73e6da`                         |
| vendored MIT license         | `837402bd25fad9b704265801ca3f92566a98157c1f9a7acd6f446299ba1c305a` |
| 126-file corpus manifest     | `d69af29cbaf4c7ffd1f8577c986294c51ec4efb177ebc340022978043b2a88a1` |
| `pyproject.toml`             | `b0a8b81937d783f1021e72f788f2b567769000ccef1bef044a2cb2b783646fb6` |
| `uv.lock`                    | `485c89d8f6bc03cc9eecf37003854d452439bd37b0252d43dcb1f8474cef6d49` |

The corpus-manifest algorithm is `sorted-path-nul-sha256-nul-size-v1`. Symlinks, duplicate JSON keys, non-finite
numbers, unexpected file counts, package/version drift, tool lock drift, suite failures, fixture result drift,
OpenAPI version drift, semantic validation errors and any attempt to claim Gate closure fail closed.

## Supply and operating boundary

- This candidate records exact package hashes and license expressions; it does not claim a current vulnerability
  scan, timeless advisory result, Python/uv executable artifact review, or final generator supply-chain signature.
- The independent review must inspect all lock entries, wheel availability on each claimed CI platform, license/notice
  obligations, package provenance, absence of build execution, corpus provenance, and the no-runtime-import boundary.
- Package downloads may use an approved immutable cache or mirror, but validation is offline after installation and
  the checked command cannot accept a different version or sdist fallback.
- No runtime schema is fetched from the network. All schemas, OpenAPI documents, fixtures, official-suite cases and
  remotes are checked-in regular files bound by profile and generation-lock manifests.
- This slice authorizes no production database write, HTTP route, P2/provider effect, deployment, publication,
  release, or Gate closure.
