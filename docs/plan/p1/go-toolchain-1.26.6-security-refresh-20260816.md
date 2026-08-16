# P1 Go 1.26.6 security refresh — 2026-08-16

- Status：**TOOLCHAIN REMEDIATION + LOCAL/ISOLATED RUNTIME EVIDENCE — PASS；Supply Gate OPEN**
- Fixed source：`77d92c584c2539901444c10683adb011b7a93cbc`
- Source tree：`23ff10e429580244dd34c2f02d5c020dcd46135c`
- Branch：`codex/cloud-agents-platform-p1`
- Evidence snapshot：`2026-08-16T15:44:24Z`
- Executor：Codex P1 implementation executor
- Independent Gate reviewer：**not assigned**

## 1. Decision and scope

The P1 Go patch pin moved from `1.26.5` to `1.26.6`. A current
`govulncheck v1.6.0` scan of the exact Control Plane source on Go 1.26.5 reported three reachable
standard-library vulnerabilities:

| Go report                                                  | Reachable package | Current source path      | Go 1.26 first-fixed |
| ---------------------------------------------------------- | ----------------- | ------------------------ | ------------------- |
| [`GO-2026-6090`](https://vuln.go.dev/ID/GO-2026-6090.json) | `crypto/tls`      | pgx connection path      | `1.26.6`            |
| [`GO-2026-6088`](https://vuln.go.dev/ID/GO-2026-6088.json) | `encoding/xml`    | pgx scan path            | `1.26.6`            |
| [`GO-2026-5972`](https://vuln.go.dev/ID/GO-2026-5972.json) | `encoding/asn1`   | projection snapshot path | `1.26.6`            |

The rejected 1.26.5 module scan returned eight findings; its text output SHA-256 was
`481a3e02ca69aed35b882980c0525d668c13071358609226fe503ccfc4a2767d`. The rejected symbol scan
returned the three reachable findings above; its text output SHA-256 was
`7bc5fb9a07d244932543914eb19050371183bac24b8df63db1d44ba923481d34`.

The official vulnerability JSON records were published on `2026-08-13T21:43:54Z` and identify
`1.26.6` as the first fixed version in the Go 1.26 line. Their exact SHA-256 values were:

- `GO-2026-6090`: `a095ce22d74407d8422f4214aa9193527771a036a19ff4099990106f28fb2cb7`;
- `GO-2026-6088`: `8a0a8b03822e77aeb92698759ed61a6eac38de4f58e1205419d13a22758f9836`;
- `GO-2026-5972`: `0a5decf07d8113ebbbd7d126d55d5d463df99fa02dc965d6be252dee342339c6`.

No source, schema, SQL, migration fixture, dependency version or production constructor changed in
this commit. The patch updates only the exact toolchain authorities, module/checker expectations,
matrix preflights and living decisions that must share the same patch pin.

## 2. Official toolchain inputs

The official [`go.dev/dl` JSON](https://go.dev/dl/?mode=json&include=all) marked `go1.26.6` stable.
The downloaded JSON SHA-256 was
`89c44d6908ad5eeed57aecdb33df54a48cc9713121230063b00716c08149c214` and contained:

| Official artifact              | SHA-256                                                            |
| ------------------------------ | ------------------------------------------------------------------ |
| `go1.26.6.darwin-arm64.tar.gz` | `2dc95ce4675829f2df0e86b28bcef3283635902062a5f0580ca659bf570f3204` |
| `go1.26.6.darwin-arm64.pkg`    | `477fb579ba85bbfd44120a0a51068bfba99300968e1d9df35d9d89e316a38733` |
| `go1.26.6.linux-amd64.tar.gz`  | `708effb774be8237570d0add163225abbdfaf4fca28b2611df167beba4feef89` |
| `go1.26.6.linux-arm64.tar.gz`  | `d0507e9e9d7fe012aae570108cbd76c15de879e17130ab8cb90d4d7445cb1f2e` |

Final contract/tool generation ran with exact Node `24.13.1`, Bun `1.3.14` and Go `1.26.6`.
The official Node Darwin/arm64 archive used for the final workspace gates had SHA-256
`8c039d59f2fec6195e4281ad5b0d02b9a940897b4df7b849c6fb48be6787bba6`.

`contracts/generation.lock.json` SHA-256 is
`1ff49c8d19494bb5ffb7b9a5531f1b74c2d46f8a4367d36934fd2eecb177b54a`. It records:

- runtime tuple `Node 24.13.1 / Bun 1.3.14 / Go 1.26.6`;
- toolchain-authority manifest
  `sha256:fafc6c16e1abc34f3ec5b40a9849521271e922e71ab4161bea12b4fd999eef5b`; and
- Go module-checker source manifest
  `sha256:a3a51f7a10ba9a8dcad73cf59c21688250b39a96eac26022fc7ce70d1b34175d`.

## 3. Source and deterministic gates

The following passed under the exact runtime tuple:

- all three modules: `go mod tidy -diff`, `go mod verify`, `go vet ./...`, `go build ./...` and
  compile-only `go test -run '^$' ./...`;
- Linux/amd64, Linux/arm64 and Darwin/arm64 cross-build plus compile-only test closure for all three
  modules;
- Linux/amd64 and Linux/arm64 cross-vet;
- tagged evidencefs integration binaries:
  - Linux/amd64 SHA-256 `7e149cd6a6937b2a1d3d7f27763ea245093c1b5193bf30a0347ae09f05ebd6b1`;
  - Linux/arm64 SHA-256 `3d7514449d00d18380a011dfe71bdd2dcf498e09b053509371cff56413ce0ed1`;
- platform module checker, 82 JSON contracts, 31 schemas, 41 contract fixture cases, 119 script
  tests, all package tests, lint, typecheck and build;
- generation lock and migration bundle check/generation `--check`;
- dirty-scope format and `git diff --check`.

The migration runtime bundle stayed byte-identical:

| Derived migration artifact | SHA-256                                                            |
| -------------------------- | ------------------------------------------------------------------ |
| schema bundle              | `52aea3c0a5fe5270d13a2bf194aedcc3ce0817fe3183dd868d427f7582f7819d` |
| bootstrap bundle           | `db95649924f259cfa320e897bd5e0934c35fcc9009d8492a69ec5dc71132081c` |
| manifest                   | `8004dc400a6fcce45d32082c8f9537d772f278a84224edabb07e9f83a489561a` |
| runtime USTAR              | `81480333ef2aafe4169ec2656af137479d94e7c6c986a2202c21754495296f07` |
| bootstrap USTAR            | `6654946d58f707d48c71740a41407674c34b5fbeced2e38eeb6c8d1bb08ae175` |

Full-repository `oxfmt --check` still reports two pre-existing, unchanged supply review files:
`pgx-v5.10.0-x-text-v0.39.0-implemented-closure.md` and `x-sys-v0.44.0.md`. They were deliberately
not rewritten into this toolchain commit; every changed file passed the exact dirty-scope formatter.

## 4. Race coverage

The unsharded migration package exceeded both 30-minute and 45-minute package-level budgets. Both
timeout stacks showed the current fault subtest runnable in canonical JSON/authority validation and
contained no race detector report. The first interrupted `bind-invalid-record` subtest independently
passed under race in `36.405s`.

Rather than increasing the timeout again, the exact 578 top-level tests were listed once, with list
SHA-256 `042c3f8a47ce6d158a1511df6483e27876a54c95ba89caf89938de9b4ec59b2d`, then partitioned into
mutually exclusive exhaustive shards. Every nested subtest remained attached to its top-level test.
All shards passed with `-race -count=1`:

| Top-level tests |    Duration | Log SHA-256                                                        |
| --------------: | ----------: | ------------------------------------------------------------------ |
|             145 | `1401.089s` | `08dd3cc6c671f594ec7bcc5ee2a0858300c12dadea07643f0e46425fa7247af1` |
|             144 |  `962.263s` | `6c124807cf48c5ea088ec40272848e61a634ab6be4194ecd35d31e92e18c4ce7` |
|              73 | `1048.405s` | `b3bfe27e10757a628e232fc0faf3bd1211332f3852a29be64642b1f5c6e60d01` |
|              72 |  `947.719s` | `193adedc908554b0e2fdb93a49dfb2b799082cf56d4b993696df976f50354df2` |
|              72 | `1184.838s` | `80e2956d0df9f5f947af82571e940ea2310910650adf7918685899dc7bf55dc3` |
|              72 |  `814.540s` | `9b9861dab83503a584d54e404815aaab8f7e56ca80a39cb317bc11d1c38f1e9c` |
|         **578** |  exhaustive | **no race report**                                                 |

SDK, Worker and evidencefs race gates also passed; the evidencefs race package completed in `6.462s`.

## 5. Shell and filesystem/runtime evidence

ShellCheck ran offline inside the exact Alpine/arm64 harness image. The closed APK set was:

| APK                        | SHA-256                                                            |
| -------------------------- | ------------------------------------------------------------------ |
| `shellcheck-0.10.0-r2.apk` | `774c6eb1192098664b8a82a35506241c9059935c1df700063ee207f3277765af` |
| `libffi-3.4.8-r0.apk`      | `9391f60a14c146655deaf65115563bc8dcd749cf0f93ec567e6443f2ed7d3bfc` |
| `gmp-6.3.0-r3.apk`         | `0d2eb1079b1b5692e9e6652ff0e269caeb9c812f483e34c88d461c03bcf75460` |

ShellCheck `0.10.0` accepted the four updated matrix wrappers and the QEMU container/guest scripts;
`bash -n`/`sh -n` also passed.

The Linux/arm64 tagged binary was then executed in the exact local image
`alpine@sha256:14358309a308569c32bdc37e2e0e9694be33a9d99e68afb0f5ff33cc1f695dce`
(image ID `sha256:2c15e55df5d63efb31b629a557df305130612a16feb029c93447e54dda2c4189`):

- fresh ext4 and XFS loop filesystems passed initial durability/cross-process-lock execution and
  clean unmount/remount reopen;
- the generation-repair QEMU scope passed resync 4, truncate 8, checkpoint 5 and discard 4 barriers
  per filesystem;
- total QEMU evidence was 42 crash barriers and 84 crash/classifier guest boots; and
- the wrapper verified no owned container remained and the host loop-device set was unchanged.

The QEMU package manifest remained
`d4db731ed3d00840ece5c114808335af1ac1be3bf6be74f4dd5df590f76e54f0`; the guest package manifest
remained `1a2aefaecd9def95205dd19290faa6b0b2a89a1c394fb73aa05820f97f643b38`.

## 6. Vulnerability refresh and open graph blocker

Fresh `govulncheck v1.6.0` ran with database timestamp `2026-08-14T16:22:54Z`:

| Scan                 | Result                              | Output SHA-256                                                     |
| -------------------- | ----------------------------------- | ------------------------------------------------------------------ |
| `-scan=module`       | exit 0; `No vulnerabilities found.` | `750c7090b4f7c8ac4e4c64c58fb5685150cadf77ef44da1a9e0f123d03ebcfc5` |
| `-scan=symbol ./...` | exit 0; `No vulnerabilities found.` | `750c7090b4f7c8ac4e4c64c58fb5685150cadf77ef44da1a9e0f123d03ebcfc5` |

This closes the three reachable Go 1.26.5 standard-library findings for the current source. It does
not close the selected module graph. A fresh OSV query at `2026-08-16T14:47:53Z` consumed 16 exact
non-main modules and returned two findings for graph-only `golang.org/x/mod v0.37.0`:

- [`GO-2026-6179`](https://vuln.go.dev/ID/GO-2026-6179.json), fixed in `x/mod v0.40.0`;
- [`GO-2026-6180`](https://vuln.go.dev/ID/GO-2026-6180.json), fixed in `x/mod v0.40.0`.

The query SHA-256 was `2a6b86ac739f03c388212345c7a3ec4332153d639e84c774f896719ceca7ce0c`; the canonical response
SHA-256 was `bf7b62776bc8d2c710b6b3e2678a16ff768065bc1ea148f4ba6ac407aa29a241`. The exact advisory JSON
SHA-256 values were respectively
`0c9188802ec6e0fe75d24c187ec7af3e75e9dee649fcba31caed55d6b14dc6ae` and
`440cf96581be963cc51ab4bb32029107d682138dfb2156d935132c981f76fa4d`.

`x/mod v0.37.0` is selected only through `x/text v0.39.0`; it is absent from the Linux and Darwin
production package closures. That reduces runtime reachability but does not satisfy the P1 selected-graph supply policy.
An independent dependency-security commit must pin at least `x/mod v0.40.0`, replay MVS/license/SumDB/module-proxy
evidence and refresh OSV before any supply artifact can claim zero findings.

## 7. Remaining boundary

This record does **not** provide:

- a zero-finding selected module graph or refreshed source-bound dependency lock/SBOM;
- a full rerun of every historical QEMU barrier family under 1.26.6;
- a non-forgeable trusted-mount provisioner or successful production `evidencefs.Open`;
- physical controller, host-power, storage-cache or device power-cut evidence;
- positive production process-restart activation/handoff/session integration;
- runner/DB `Connect`, deployment, independent immutable Gate review, Platform RC, Beta or GA.

Historical 1.26.5 binary evidence remains immutable historical evidence but is superseded for current builds.
`G-SUPPLY-CHAIN`, `G-DATA`, `G-AUTHORITY-P1`, `G-SECURITY-P1` and the aggregate Gates remain
`IN PROGRESS`.
