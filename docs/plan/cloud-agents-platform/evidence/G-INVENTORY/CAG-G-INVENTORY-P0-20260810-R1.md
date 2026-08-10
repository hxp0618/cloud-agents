# Gate record: `G-INVENTORY` / P0 / R1

- Evidence ID：`CAG-G-INVENTORY-P0-20260810-R1`
- Record type：PHASE
- Status：IN PROGRESS
- DRI：hxp0618（owner）；Codex P0 executor
- Independent reviewers：P0 inventory explorer、P0 provenance explorer
- Date：2026-08-10 Asia/Shanghai

## Fixed inputs

- Public plan branch base：`cloud-agents@9698605cc5d2f3f0fb8e89d422d7bd20a49e1c64`
- Public Runtime source：`cloud-agents main@49e8cdc6a3a4f88c7324d055ce519e9f25a8ca8a`
- Synara extraction source：`2c50b1eb54ed3228719bb55cc8bdcd1b0babc8e0`
- Synara root tree：`ba41fc168ea65978b1f17fdb8abc5afbc22ca9cc`
- Synara Control Plane tree：`4d32d26bf32b8ebd62188961d0dffff92da4cf0c`
- T3 main / consumer：`8101cd044911c7dc2a2adf7c7a9ba7962abf57b6` /
  `9584a266e91fa94354e8c07f79af3a5e01755d16`
- Frozen baseline SHA-256：`c28df16a4d47f1ee0764c8ec5ff1465cfa71c2173ef441471672baaff63d9af3`
- File inventory SHA-256：`f3858852538ef67ec6879a6db101246f7b3bf65ba6301f9e5e9274200d716aa1`
- Inventory graph SHA-256：`784d5c36babc85e05053450c01c6aa3737274c5d6d46371606b1f7596bdd0e76`
- Toolchains：Git 2.55.0、Node 24.13.1、Bun 1.3.14、Go 1.26.5 darwin/arm64

## Exit criteria mapping

| Criterion                                                  | Result  | Evidence                                                                             |
| ---------------------------------------------------------- | ------- | ------------------------------------------------------------------------------------ |
| exact clean extraction source + tree                       | PASS    | [`frozen-baseline.json`](../../../p0/frozen-baseline.json)                           |
| local refs/worktrees/dirty state recorded                  | PASS    | frozen baseline: 7 worktrees                                                         |
| live remote refs recorded                                  | NOT RUN | all three `git ls-remote` calls timed out; explicitly `unavailable`                  |
| full code/SQL/schema/build/deploy/generated input manifest | PARTIAL | 1,607 inputs in [`synara-file-inventory.tsv`](../../../p0/synara-file-inventory.tsv) |
| file capability/classification/owner finalized             | NOT RUN | 339 manual-review files; 229 unclassified                                            |
| Go command/image/deploy/external artifact relationships    | PARTIAL | [`synara-inventory-graph.json`](../../../p0/synara-inventory-graph.json) + summary   |
| license/secret/source provenance complete                  | FAIL    | SBOM/license/attestation/Gitleaks blockers documented                                |
| audit leaves extraction source clean                       | PASS    | source status clean before/after                                                     |

## Observed inventory

- 1,174 Control Plane files：517 production Go、477 tests、167 migrations、13 other；
- 117 deploy、235 scripts、10 CI、62 contract references、4 provider-host compatibility、5 root inputs；
- 8 production command、114 module package；Linux/amd64 closure covers 104 local packages、497 source、167 SQL；
- 8 external Runtime release artifacts；
- seed classification：1,017 rewrite-public、287 adapter、22 move、43 Synara-only、8 deferred、1 retire、
  229 unclassified；339 files require manual review。

## Failures and blockers

1. Live remote verification unavailable due outbound SSH/HTTPS timeout；本地 tracking refs 不提升为远端事实。
2. Provider catalog generator source path missing；checked-in output masks an orphaned generation chain.
3. `internal/agentd` and `cmd/api` are mixed/coupled; directory-level move is unsafe.
4. Docker broad COPY conflates semantic runtime dependencies with build-context/cache inputs.
5. Current SBOM has 8 packages/0 relationships；standalone bundled license/NOTICE closure missing.
6. Current provenance is unsigned/local and does not cover the full artifact set.
7. Gitleaks/Syft/license tools are not pinned/available locally；existing scanner allowlist is too broad.

## Reproduce

```bash
/Users/huang/devel/soft/nvm/versions/node/v24.13.1/bin/node \
  docs/plan/p0/scripts/generate-frozen-baseline.mjs

/Users/huang/devel/soft/nvm/versions/node/v24.13.1/bin/node \
  docs/plan/p0/scripts/generate-synara-file-inventory.mjs
```

## Closure decision

`G-INVENTORY` remains `IN PROGRESS`. P1 entry is blocked until live remote refs are verified, all manual-review files
receive a final classification/owner/target, the orphaned generator chain is resolved in design, and provenance/license/
secret evidence is complete.
