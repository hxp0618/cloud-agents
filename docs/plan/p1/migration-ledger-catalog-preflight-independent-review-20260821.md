# P1 runner ledger/catalog preflight independent review - 2026-08-21

- Status: **APPROVE — fixed Slice A/B source review only**
- Fixed candidate: `01b1a5f5d5c68776a69c805839a1e333191e29bf`
- Repository tree: `1711bed1add97b19248680b34b5e06810cce3cf0`
- `services/control-plane` subtree: `198719a2dfee4683f485c0f107f02521771d666e`
- Candidate branch: `codex/cloud-agents-p1-ledger-preflight-kernel-repair-20260821`
- Review branch: `codex/cloud-agents-p1-ledger-preflight-independent-review-20260821`
- Review mode: independent and read-only against the fixed candidate
- Severity result: P0 `0` / P1 `0` / P2 `1`

This record approves only the fixed generated registry/profile and locked, read-only ledger/catalog preflight kernel
described by [ADR-0019](../adr/0019-p1-runner-ledger-preflight-contract.md) and the
[entry blocker](./migration-ledger-preflight-entry-blocker-20260821.md). It is not an immutable Gate signature and
does not close `G-CONTRACT`, `G-DATA`, `G-AUTHORITY-P1`, `G-SECURITY-P1`, `G-SUPPLY-CHAIN`, or any aggregate Gate.
It does not authorize HTTP, P2, Provider, Worker, Session, Turn, or Execution effects; production database writes;
deployment; release; publication; merge to main; RC; Beta; or GA.

## 1. Verdict

The independent reviewer returned **APPROVE** for the exact fixed candidate. The review established that:

1. the generated profile has five closed dispositions and exactly 17 permitted
   disposition/recovery/action triples, rejects zero, copied, cross-profile, tampered, and unknown values, and has
   no handwritten fallback;
2. the kernel reconstructs its runtime bundle and statement plans from private owned inputs rather than accepting
   caller-supplied public mutations;
3. one dedicated database session performs connected-session authority projection, settings configuration,
   signed advisory locking, migration-role authority projection, exact ledger read, predecessor or cumulative
   catalog selection, exact ledger reread, and cleanup before sealing an ordinary fact;
4. empty, genuine two-migration partial-prefix, and complete shapes remain read-only, with catalog head, schema
   bundle, execution lineage, session identity, and pre-seal drift cross-bound;
5. projection rollback, advisory unlock, and dedicated-session close are all attempted with the specified cleanup
   and error precedence;
6. the returned ordinary fact carries no session, transaction, evidence, receipt, writer token, identity registry,
   verifier artifact, lease, or authority handle;
7. `projectRunnerLedgerCatalogPreflight` has no production caller, and existing non-empty runner paths continue to
   fail closed as `MIGRATION_PROJECTION_NOT_IMPLEMENTED`;
8. the fixed candidate adds no HTTP, P2, Provider, writer, production database mutation, deployment, publication,
   or Gate-closing surface.

The reviewed generated identities are:

| Identity                       | Exact value                                                               |
| ------------------------------ | ------------------------------------------------------------------------- |
| generated registry digest      | `sha256:a17a7333d1455d3ed5a0e2d70c2a3b2ea9ee1e323a5ac935d4d9a12a2db30661` |
| generated state-machine digest | `sha256:fe00c5de375865350798e6a79de22b4272fb70949bbd5d3c0a11745bb3dd11ae` |
| generated policy digest        | `sha256:1fb99617210b7d92e7c51d2241f98674c67818c85c82cf13edd2ebe33b34388b` |
| generated registry file        | `2a04f67f9b06f25bc13211934d8cb914dcbe3f92d42053f636ec66b4f28ac11c`        |
| generated Go profile           | `599b78537a3f1dd5d70c1b50aa5e7bc54e1b65ee463876ee3f111709a5ab2112`        |
| generation lock                | `afb031797aa7b36469d1dd1255542e4cc95e917a85847fba180f02029c48f2d0`        |
| read-only kernel               | `668f33b99f628c65e578e46449d637e254ab08609d2ecac08073ec3a1eaadf77`        |
| read-only kernel tests         | `d6402d1700f437d7eb5756c616d65d227a868e6aeeae95871ac058dcdbc1aab1`        |
| shared runtime fixture tests   | `8dd8db7dd0a8637d5506076b239391987ab81340ba812c2614d0b896ccd07056`        |

## 2. P2 documentation finding

The executor-only review handoff recorded the source schema SHA-256 as a 63-character value. It omitted one `9`.
The candidate artifact itself did not drift and has the correct 64-character SHA-256:

`2c48a4db4641de750336fb2cfb454da98998a494002342f29201c17dfdbc7204`

This finding is P2 because it affects only a non-authoritative handoff document on a separate branch. The review
used the candidate bytes and independently recomputed identity, so the typo does not change the approval verdict.
The erroneous handoff remains historical executor evidence and is not promoted to review authority.

## 3. Independent verification

The independent reviewer verified that the candidate worktree was clean, its HEAD and upstream were exact, and its
commit, repository tree, control-plane subtree, and artifact hashes were stable. Fresh checks against the fixed
candidate passed:

- focused ledger/catalog preflight Go tests in normal mode (`4.289s`);
- the same focused selection with the race detector (`31.728s`);
- `go vet ./...` and `go build ./...` from `services/control-plane`;
- both narrow runner ledger-preflight generators with `--check`;
- the generated registry tests: 5 tests and 44 assertions;
- contract bootstrap validation: 103 JSON files, 42 schemas, 54 fixture cases, and `notGateClosure=true`.

The complete contract-lock check is **NOT PASS** in this review environment. The repository declares Node
`24.13.1`, Bun `1.3.14`, and Go `1.26.6`, while the ambient tools are Node `26.7.0`, Bun `1.4.0`, and Go `1.26.7`.
The exact-version guard rejected the mismatch as designed. The narrow generated checks above do not replace that
pinned-toolchain replay.

## 4. Next-slice boundary

The fixed Slice A/B source review is complete. Under the owner's existing approval, Slice C may now begin as the
typed recovery/no-op dispatch, matrix, and independent-review slice. Slice C must remain within the already frozen
scope and must receive its own fixed-candidate independent review before it can be called complete.

This record does not turn the ordinary preflight fact into writer or recovery authority. It does not authorize a
production caller, production database mutation, external side effect, deployment, release, publication, or Gate
closure. All immutable and aggregate Gates remain OPEN.
