# Gate record: `G-INVENTORY` / P0 / R2

- Evidence ID：`CAG-G-INVENTORY-P0-20260810-R2`
- Record type：PHASE
- Phase / aggregate Gate：`G-INVENTORY`
- Prerequisite record IDs：none
- Status：`VERIFIED`
- DRI：hxp0618（owner）；Codex P0 executor
- Independent reviewer：Codex P0 inventory final-review explorer（两轮只读复核）
- Date：2026-08-10 Asia/Shanghai
- Supersedes：`CAG-G-INVENTORY-P0-20260810-R1`（`INVALIDATED`）

## Fixed inputs

- `cloud-agents` evidence commit：`2b2c5edbf3770be6ead71c4ba2370e7e48e12958`
- Evidence commit tree：`57d78c953e503233381ea9bd41433d6cfe4f9361`
- Remote feature ref：`hxp0618/cloud-agents:codex/cloud-agents-platform-p0@2b2c5edbf3770be6ead71c4ba2370e7e48e12958`
- Runtime immutable source/tag：`49e8cdc6a3a4f88c7324d055ce519e9f25a8ca8a`
- Synara extraction source/tree：`2c50b1eb54ed3228719bb55cc8bdcd1b0babc8e0` /
  `ba41fc168ea65978b1f17fdb8abc5afbc22ca9cc`
- T3 main/consumer reference：`8101cd044911c7dc2a2adf7c7a9ba7962abf57b6` /
  `9584a266e91fa94354e8c07f79af3a5e01755d16`
- Remote-ref evidence SHA-256：`a3c7f8ede7f878e9510a62a18ee5df509500a76154050b034223d89fb3fa8c67`
- Inventory / graph / decisions SHA-256：
  - `bee237da890f4f3d62fd524fd11142a6b6c883e82790e5d455c415461ae7b4e5`
  - `3c7484c296f56e86f6a523e122f234c0f3b4ce6516f9ed492b69ea8a64faae04`
  - `4e8e92cfb48a2d272c4b025c815a8afb9f85ec7ab6be65a160f4893c85fc429d`
- Secret finding-set / triage SHA-256：`5f74f2af7a9468e56fcd65bf3f6cc2ee168bf19189cbec92d0f75832f0860724` /
  `2936cb8e883b296d13cd99f2ac4ba474c06e79dad4bce0cfdf9b4e76083b61b6`
- Source LICENSE blob / text SHA-256：`960499447d8ea8f6ce86017893f132f0c3885fef` /
  `305724dd050ca7ded99c662de813d755bc4ec3887c4543a37159c6662ca36d1b`
- Toolchain：Git 2.55.0；Node 24.13.1；Bun 1.3.14；Gitleaks 8.30.1
- Deployment profile：none；inventory/provenance only

## Exit criteria mapping

| Criterion                                                  | Result | Evidence                                                                                         |
| ---------------------------------------------------------- | ------ | ------------------------------------------------------------------------------------------------ |
| exact extraction source, tree, clean/non-shallow identity  | PASS   | `synara-extraction-source-audit.json`                                                            |
| live public remote refs bound without fetch/mutation       | PASS   | `remote-ref-verification.json`                                                                   |
| full code/SQL/schema/build/deploy/generated tracked input  | PASS   | 8,625 rows；inventory SHA `bee237da...`                                                          |
| per-file capability/classification/owner/target/provenance | PASS   | 8,625 unique paths/targets；decision SHA `4e8e92cf...`；unresolved 0                             |
| durable authority and environment-effect separation        | PASS   | finalizer negative invariants；migration/DB/routing/KMS core and Docker/filesystem/webhook split |
| command/deploy/lock/artifact graph is referentially closed | PASS   | 25 nodes / 26 edges；missing/duplicate/isolated 0；graph SHA `3c7484c...`                        |
| source license provenance complete                         | PASS   | fixed MIT LICENSE Git object/text digest                                                         |
| secret provenance complete                                 | PASS   | three Gitleaks scopes；56/56 exact triage；48 FP + 6 rewrite + 2 history quarantine              |
| deterministic clean-tree replay                            | PASS   | Git archive of `2b2c5ed` reproduced inventory/graph/decisions/summary byte-for-byte              |
| independent semantic review                                | PASS   | final-review explorer reported no remaining P0/P1/P2 finding after graph/order/.dockerignore fix |
| audit leaves fixed Synara source unchanged                 | PASS   | fixed source clean before/after；no checkout, fetch, commit, or mutation                         |

## Commands / evidence

```bash
/Users/huang/devel/soft/nvm/versions/node/v24.13.1/bin/node \
  docs/plan/p0/scripts/generate-synara-file-inventory.mjs
/Users/huang/devel/soft/nvm/versions/node/v24.13.1/bin/node \
  docs/plan/p0/scripts/finalize-synara-inventory.mjs
/Users/huang/devel/soft/nvm/versions/node/v24.13.1/bin/node \
  docs/plan/p0/scripts/finalize-synara-secret-triage.mjs
P0_GITLEAKS_BIN=/absolute/pinned/gitleaks \
  /Users/huang/devel/soft/nvm/versions/node/v24.13.1/bin/node \
  docs/plan/p0/scripts/audit-synara-extraction-source.mjs
```

Clean-tree replay used `git archive 2b2c5ed` in a mode-0700 temporary directory. Before/after SHA-256 for the four
generated inventory artifacts were identical. The provenance audit exited 0 with `status=PASS` and
`secretScanning.status=PASS_WITH_RESTRICTIONS`.

## Failures, retries, and restrictions

1. R1 covered only 1,607 candidate paths and missed the effective root build-context superset; it is invalidated.
2. Earlier classification mixed durable DB/routing/KMS authority into adapters and Docker/external effects into core;
   explicit semantic overrides and negative invariants now reject those states.
3. The first expanded graph had dangling endpoint IDs and isolated command/deploy nodes; the generator now fails on
   duplicate nodes, missing endpoints, or isolated nodes.
4. Gitleaks directory fingerprints included temporary absolute roots; persisted identities now hash canonical
   scope/rule/path/line/commit fields and findings are sorted before writing.
5. All 56 findings are reviewed, but this is not a blanket allowlist:
   - six static test-private-key occurrences remain `REWRITE_REQUIRED_BEFORE_PUBLICATION`；
   - two historical-log occurrences require `SOURCE_HISTORY_QUARANTINE`；
   - the public repository must use new commits and must never graft Synara Git history.
6. Third-party dependency licenses, Runtime redistribution rights, signatures/attestations, and publication remain
   unresolved release/supply-chain work. They do not invalidate inventory completeness, but this record grants no
   direct-copy or publication authority.

## Rollback / cleanup evidence

- No runtime, database, endpoint, grant, workload, volume, or production state was created.
- Temporary scan/materialization directories contain only test evidence and are not an authority source.
- Existing unrelated `.idea/**` staged state and `.gitignore`/`.idea/misc.xml` worktree changes were hash-checked before
  and after the evidence commit and were not included.

## Invalidation

This record becomes `INVALIDATED` if any of the following changes:

- fixed Synara source/tree, inventory selection rules, classification/owner/target/provenance, or graph semantics；
- source LICENSE object/text, canonical finding identity set, triage decision/expiry, or scanner version/digest；
- extraction policy begins importing Synara history or copying `REWRITE_REQUIRED` source bytes；
- a new tracked/build/deploy/generated input is omitted or an adapter becomes a durable authority writer.

Downstream invalidation target：`G-CONTRACT`、`G-DATA`、`G-AUTHORITY-P1` and every P1–P6 record that consumes the
inventory decision set.

## Sign-off

- DRI conclusion：all `G-INVENTORY` evidence is complete and reproducible at the fixed evidence commit.
- Reviewer conclusion：after the graph, ordering, and `.dockerignore` precision fixes, no P0/P1/P2 finding remains；R2
  may be `VERIFIED` with the stated no-history/no-direct-copy/no-publication boundaries.
- Closure decision：`G-INVENTORY = VERIFIED`。This does not close `G-BASELINE-P0`, Platform P0, M1, supply chain,
  release, deployment, Beta, or GA.
