# P1 current-source exhaustive migration normal-shard closure — 2026-08-22

- Status: **LOCAL EXHAUSTIVE NORMAL SHARDS — PASS; INDEPENDENT REVIEW PENDING; GATES OPEN**
- Fixed source: `7f14c7fe02bc785207f5ca1934b5034801f9f7ef`
- Source tree: `a1a723a1a0ba2605dea26edc2ec382c1d4213fba`
- Control-plane subtree: `962ab0bbc883f521f49eca5705f6ba6636e458e0`
- Branch: `codex/cloud-agents-p1-migration-shard-runner-20260822`
- Pre-run branch state: clean, upstream ahead `0`, behind `0`, remote ref exact
- Independent reviewer: **not assigned**
- Gate effect: **none**

This record replaces repeated single-process 30-minute invocations with one deterministic,
mutually exclusive, exhaustive normal-shard run. It does not change a migration test, production
package, generated contract, database, runtime, route, provider, deployment, or release surface.

## 1. Runner contract

Fixed source adds only
`services/control-plane/scripts/test-migration-shards.sh` (SHA-256
`e0ba9236c1ef5d52d6598fb41bd5c1c72046a083b0d4529a5b7f71e550d7983b`). The runner:

1. requires exact Go `1.26.6`, `GOWORK=off`, `GOTOOLCHAIN=local`, and
   `GOFLAGS=-mod=readonly`;
2. refuses run mode unless the worktree is clean and the new absolute artifact directory is outside
   the repository;
3. refuses every opt-in PostgreSQL/catalog/evidencefs integration environment variable before
   listing tests, so the default shard suite cannot silently become a live external matrix;
4. lists top-level `internal/migration` tests exactly once, accepts only closed ASCII Go test names,
   sorts them, and binds the list SHA-256;
5. assigns each list entry to `index mod shard_count`, then byte-compares the sorted shard union with
   the source list and rejects duplicates or omissions;
6. runs each shard once with exact anchored regular expressions and `-count=1`; no failed shard is
   retried or converted to another result;
7. retains per-shard test lists, regular expressions, JSON logs, stderr, exit codes, timing, and
   SHA-256 values; and
8. rechecks commit/tree/control-plane identity and a clean worktree after all shard processes stop.

`plan` mode performs compile/list/partition validation only. `run` mode is the sole exhaustive test
operation. This keeps development iteration on focused tests and reserves the full list for a fixed
candidate.

## 2. Fixed inputs

| Input                                   | Exact value                                                                 |
| --------------------------------------- | --------------------------------------------------------------------------- |
| Go                                      | `go1.26.6`                                                                  |
| GOOS / GOARCH / CGO                     | `darwin / arm64 / 1`                                                        |
| GOROOT                                  | `/Users/huang/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.6.darwin-arm64` |
| `go.mod` SHA-256                        | `a4d98dcbd65803a22bcf946cf042d17484e714500c0502b616d68742a02f1d14`          |
| `go.sum` SHA-256                        | `c5e16bfbadc2461fd349b94ce6487aadcb2edea11fa0aa37fd29bc2f46bfc88c`          |
| Top-level test count                    | `700`                                                                       |
| Sorted test-list SHA-256                | `d7cdd59e7ec3bd75d5832c0dd581d2afe2271fd775ba97007d72f639176b5fbd`          |
| Shards / jobs / per-process parallelism | `8 / 4 / 2`                                                                 |
| Per-shard timeout                       | `20m`                                                                       |
| Race detector                           | `false`                                                                     |

The eight immutable list cardinalities are `88, 88, 88, 88, 87, 87, 87, 87`.
Two consecutive `plan --shards 8` outputs were byte-identical. Plans with one and 64 shards also
passed union/duplicate validation; zero shards, explicit jobs greater than shards, relative output,
repository-local output, dirty run mode, and an enabled external-integration variable all rejected
before tests.

## 3. Exact command and result

From repository root:

```sh
CLOUD_AGENTS_GO=/Users/huang/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.6.darwin-arm64/bin/go \
  services/control-plane/scripts/test-migration-shards.sh run \
  --output-dir /tmp/cag-migration-shards-7f14c7f-normal \
  --shards 8 \
  --jobs 4 \
  --test-parallel 2 \
  --timeout 20m
```

Result:

```text
Migration shards: PASS tests=700 shards=8 \
  list_sha256=d7cdd59e7ec3bd75d5832c0dd581d2afe2271fd775ba97007d72f639176b5fbd
```

- Start: `2026-08-22T11:48:41Z`
- Finish: `2026-08-22T11:57:51Z`
- Wall time: `550s`

| Shard | Tests | Exit | Elapsed | JSONL SHA-256                                                      |
| ----- | ----: | ---: | ------: | ------------------------------------------------------------------ |
| 00    |    88 |    0 |    133s | `b697977bad34314fd6bddacf3664b0afe156fa3c03a1c40eeb93903ae2387a2b` |
| 01    |    88 |    0 |     81s | `ac4b81beea1447be2d60da2a69309f9c77a205b6eaf9a47f5dcdfe2b18a2dfaa` |
| 02    |    88 |    0 |    339s | `4ccf8f99fb29f666a4132a17adfa41e23f10fa23109b93a903db735faae99503` |
| 03    |    88 |    0 |    127s | `dace7d557c26bb78aa3f170aca9bbeaf3ba6b59396bbd1c56c7fa9eee43a7f4a` |
| 04    |    87 |    0 |    179s | `e7826bbc210d061c720e07c96edaa198905d15a3aa54128d9ad421b414745cb9` |
| 05    |    87 |    0 |     64s | `ac30936e112c554226e45cc9bee8309efc8dfacddfd2312b47a748a2dba6b242` |
| 06    |    87 |    0 |    108s | `1bc12cf36951da98f87737eb522e6729b9942a67935a43788d711a2319a26c55` |
| 07    |    87 |    0 |    210s | `ab41325edb93fd6d0679dc7652f43ce3ebdd2aa7890fe3defff6d4cce33bc135` |

All eight stderr files are empty and bind to the empty SHA-256
`e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855`.

## 4. Independent log validation

Post-run parsing did not rerun tests. It proved:

- sorted top-level JSON `run` events byte-equal the fixed 700-entry source list;
- every shard contains a final package-level `pass` event;
- no JSON `fail` event exists;
- top-level terminal actions are exactly `695 pass + 5 skip = 700`; and
- the repository remains at the fixed commit/tree/subtree, clean and upstream `0/0`.

The five skips are the explicit external PostgreSQL/catalog tests:

- `TestPGCatalogFixedQueriesParseOnPostgres`;
- `TestPGCatalogPG17RejectsWiderMaintainGrant`;
- `TestPGCatalogStructureOnCheckedInMigrations`;
- `TestPGCatalogStructureOnRepresentativePostgres`; and
- `TestPGProjectionPostgresMatrix`.

They are not relabelled as live database PASS. Their opt-in variables were unset and the runner would
have rejected non-empty values before listing tests.

The task-owned local evidence directory contains 90 bound files and about 5.9 MiB. Its generated
artifact manifest verifies all files and has SHA-256
`349ddbe1eb1d2972750b0a5cc34937e7c8c8b081134f5765ea7f4428e3e35011`.
Selected manifest members are:

| Artifact        | SHA-256                                                            |
| --------------- | ------------------------------------------------------------------ |
| `metadata.tsv`  | `a17e39ce1f85609c5bb0b3bce4bd9e5ceb028d0d37f9e9c59676b21dc06d6693` |
| `shards.tsv`    | `de0b25c03fa62483295b8f6152be378dd8b6bfc32e58b3378178744a3abc8b61` |
| `results.tsv`   | `c34709c8a5d7d2cdcfbedc34b174c1d7d7ed24d40e21dfacee82f6ae47a08aa0` |
| `all-tests.txt` | `d7cdd59e7ec3bd75d5832c0dd581d2afe2271fd775ba97007d72f639176b5fbd` |

## 5. Static verification and non-claims

The fixed script passed `bash -n`, exact `shellcheck 0.10.0-r2` in
`alpine@sha256:14358309a308569c32bdc37e2e0e9694be33a9d99e68afb0f5ff33cc1f695dce`,
dirty/output/external-variable negative checks, executable-mode verification, `git diff --check`,
and staged Gitleaks. The first default-Alpine-repository ShellCheck container waited without
producing a result and was removed; only the later fixed-package run through the configured mirror
is PASS.

This record does **not** claim or authorize:

- a full race suite, a live PostgreSQL 15/16/17 matrix, or an opt-in physical evidencefs run;
- repository-wide Go/TypeScript tests, final artifact/supply-chain closure, or immutable review;
- production database reads/writes, HTTP/P2/provider effects, deployment, publication, or release;
- `G-DATA`, `G-AUTHORITY-P1`, `G-SECURITY-P1`, another Gate, or Platform RC.

The result closes only the current-source default `internal/migration` normal assertion boundary for
the fixed source. Independent read-only review remains required before the tracker may treat this
local record as reviewed closure evidence.
