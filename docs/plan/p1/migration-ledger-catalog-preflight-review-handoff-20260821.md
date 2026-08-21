# P1 runner ledger/catalog preflight fixed-candidate review handoff - 2026-08-21

- Status: **FIXED CANDIDATE READY FOR INDEPENDENT REVIEW; NO REVIEW VERDICT YET**
- Fixed candidate: `01b1a5f5d5c68776a69c805839a1e333191e29bf`
- Candidate branch: `codex/cloud-agents-p1-ledger-preflight-kernel-repair-20260821`
- Candidate parent chain:
  `c34bf02c9f2e40cdeff1beda151c3e48f84aba78 -> 6cdaa43 -> 0f38d01 -> 5cc94e5 -> 01b1a5f`
- Review-handoff branch: `codex/cloud-agents-p1-ledger-preflight-review-handoff-20260821`
- Entry authority: [`migration-ledger-preflight-entry-blocker-20260821.md`](./migration-ledger-preflight-entry-blocker-20260821.md)
  and [`ADR-0019`](../adr/0019-p1-runner-ledger-preflight-contract.md)
- This document is executor evidence only. It is not an independent review, an immutable signature, a Gate closure,
  production database authority, deployment evidence, or permission to start Slice C.

## 1. Fixed candidate boundary

The review target contains the owner-approved generated/versioned Slice A profile and the isolated Slice B locked,
read-only projection kernel. The generated profile has exactly five closed dispositions and a closed 17-row
disposition/recovery/action matrix. The kernel reconstructs the runtime and statement-plan view from private verified
inputs, then uses one dedicated database session in this order:

1. connected-session authority projection;
2. role/settings configuration;
3. signed advisory lock;
4. migration-role authority projection;
5. exact ledger-prefix read;
6. signed initial predecessor projection for an empty prefix, or the exact cumulative catalog selected by a
   non-empty prefix head;
7. exact ledger-prefix reread;
8. rollback-only projection cleanup, advisory unlock, and session close;
9. construction of a copyable, canonical-digest-sealed ordinary fact only after cleanup succeeds.

Commit `01b1a5f` adds a genuine two-migration runtime fixture. It proves that a one-row prefix over a two-entry signed
lineage returns `partial`, binds the `000001` cumulative catalog exactly, performs both ledger reads, and still has
zero migration transaction, query, ledger insert, execute, commit, or evidence mutation.

The candidate has no production caller of `projectRunnerLedgerCatalogPreflight`. The existing partial and complete
production paths remain `MIGRATION_PROJECTION_NOT_IMPLEMENTED`; there is no same-verifier recovery binder, no no-op
return authority, and no writer dispatch in this candidate.

## 2. Exact candidate identities

| Identity                         | Exact value                                                               |
| -------------------------------- | ------------------------------------------------------------------------- |
| fixed commit                     | `01b1a5f5d5c68776a69c805839a1e333191e29bf`                                |
| repository tree                  | `1711bed1add97b19248680b34b5e06810cce3cf0`                                |
| `services/control-plane` subtree | `198719a2dfee4683f485c0f107f02521771d666e`                                |
| generated registry digest        | `sha256:a17a7333d1455d3ed5a0e2d70c2a3b2ea9ee1e323a5ac935d4d9a12a2db30661` |
| generated state-machine digest   | `sha256:fe00c5de375865350798e6a79de22b4272fb70949bbd5d3c0a11745bb3dd11ae` |
| generated policy digest          | `sha256:1fb99617210b7d92e7c51d2241f98674c67818c85c82cf13edd2ebe33b34388b` |
| generated registry file          | `2a04f67f9b06f25bc13211934d8cb914dcbe3f92d42053f636ec66b4f28ac11c`        |
| generated Go profile             | `599b78537a3f1dd5d70c1b50aa5e7bc54e1b65ee463876ee3f111709a5ab2112`        |
| generation lock                  | `afb031797aa7b36469d1dd1255542e4cc95e917a85847fba180f02029c48f2d0`        |

The source files most directly owned by the fixed boundary have these SHA-256 values:

| Artifact                           | SHA-256                                                            |
| ---------------------------------- | ------------------------------------------------------------------ |
| source fixture                     | `bd1a9e57fd5f1014a7afead056d6c03f1b0a8501e9767e1eb0308aef9065bd71` |
| source schema                      | `2c48a4db4641de750336fb2cfb454da9898a494002342f29201c17dfdbc7204`  |
| generated schema                   | `829b9e7aefaf16642090051b93babb311790a3be354f91ed91520cca39079c5c` |
| registry generator                 | `dfdb62bfca1b2a9568f03ce5f9605672041896d67a51537f54b07a3808406774` |
| Go generator                       | `8a010e6039bfdd950de251d0a556b307352fa7f20221e02e1707ac8b584fe7d8` |
| registry library                   | `6c69e86768ccc131a33d1419b9ec72adefa1bbc1c1e3301571bd5eb6f25933fe` |
| registry tests                     | `1117447150c378aa1b0cccb234c7b19887c42970ac11673b009c0fa154e4107d` |
| ordinary-fact profile              | `9f202c11408310eb9c0f7df368093f70407a3ab72fdba537c490d017551925ed` |
| ordinary-fact profile tests        | `2400eb9555681624d16daf0b209901df3706c70dcec0bd82c5df7ba308a8800e` |
| read-only kernel                   | `668f33b99f628c65e578e46449d637e254ab08609d2ecac08073ec3a1eaadf77` |
| read-only kernel tests             | `d6402d1700f437d7eb5756c616d65d227a868e6aeeae95871ac058dcdbc1aab1` |
| database preflight tests           | `67199d87f3975385b853b5e2c9e423376df9bc557a657b12bc59fde8d85d101a` |
| shared exact-runtime fixture tests | `8dd8db7dd0a8637d5506076b239391987ab81340ba812c2614d0b896ccd07056` |

## 3. Executor verification

The following checks passed against the exact fixed candidate:

- `bun scripts/check-platform-contracts.ts`:
  `BOOTSTRAP_VALIDATED`, 103 JSON files, 42 schemas, 54 fixture cases, `notGateClosure=true`;
- `bun scripts/generate-platform-runner-ledger-preflight-registry.ts --check`;
- `bun scripts/generate-platform-runner-ledger-preflight-go.ts --check`;
- after a frozen-lock dependency install,
  `bun test scripts/lib/platform-runner-ledger-preflight-registry.test.ts`: 5 tests and 44 assertions passed;
- `GOWORK=off GOFLAGS=-mod=readonly go test ./internal/migration -run 'TestRunnerLedger(Preflight|CatalogPreflight)' -count=1`;
- the same focused Go selection with `-race`;
- the wider runner/prepared-session focused selection in normal and race modes;
- `GOWORK=off GOFLAGS=-mod=readonly go vet ./...` and `go build ./...`;
- CGO-free Linux `amd64` and `arm64` migration-package test compilation through `/usr/bin/true`;
- `git diff --check` and a production-call scan showing only the package-private declaration and test calls.

The direct Bun test initially could not load AJV because the isolated worktree had no installed dependencies. A
`bun install --frozen-lockfile` completed without changing tracked files, after which the test passed. This is
recorded as setup evidence, not as a source finding.

## 4. Review checklist

The independent reviewer should inspect the exact fixed candidate, not this handoff branch, and record P0/P1/P2
findings for at least these boundaries:

1. generated registry/profile determinism, selector closure, five dispositions, 17 allowed triples, error precedence,
   zero/copy/cross-profile/tamper rejection, and absence of a handwritten fallback;
2. private verified runtime reconstruction and exact statement-plan rebuild, including rejection of caller-supplied
   public bundle/plan mutations;
3. one-session lock/authority/ledger/catalog/reread order and exact empty, partial, and complete shapes;
4. catalog-head selection, schema-bundle and execution-lineage binding, projection metadata/session identity, and
   pre-seal drift rejection;
5. cleanup/error precedence across projection rollback, advisory unlock, and dedicated-session close;
6. ordinary-fact copy semantics versus literal/tamper rejection, with no session, lease, verifier artifact, receipt,
   transaction, writer token, identity registry, or authority handle;
7. zero production callers and continued `NOT_IMPLEMENTED` behavior in the current partial/complete runner paths;
8. forbidden HTTP/P2/provider/worker/session/turn/execution/deployment/publication surfaces and unchanged Gate state.

Any finding must be repaired on a new fixed candidate and independently rereviewed. A review approval may close only
this bounded Slice A/B fixed-source review. It cannot authorize Slice C by itself unless the entry blocker is then
updated with the exact approved review record; it cannot close an immutable or aggregate Gate.

## 5. Explicit limitations and non-claims

The review host currently has ambient Node `26.7.0`, Bun `1.4.0`, and Go `1.26.7`, while the repository requires
Node `24.13.1`, Bun `1.3.14`, and Go `1.26.6`. Therefore
`bun scripts/generate-platform-contract-lock.ts --check` correctly stopped at the toolchain guard and is **NOT
PASS** in this handoff run. The two narrow generators and semantic checker passed, but that does not replace the
fixed-toolchain contract-lock replay.

The broad `internal/migration` suite was not rerun and is not claimed as passing; its documented long-run timeout
remains separate from this focused candidate. No PostgreSQL 15/16/17 live matrix, vulnerability refresh, production
trust-root setup, production database read or write, deployment, release, publication, merge, RC, Beta, GA, or Gate
signature was performed or implied.

Slice C remains **NOT STARTED** until a separate independent reviewer approves the exact Slice B candidate. All
immutable and aggregate Gates remain OPEN.
