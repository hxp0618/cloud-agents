# Independent review: `G-BASELINE-P0` / P0 / R4 candidate

- Review status：`APPROVE`
- Finding count：`P0=0 / P1=0 / P2=0`
- Review mode：two independent read-only fixed-candidate reviews
- Review date：2026-08-23 Asia/Shanghai
- Gate effect：none；this review does not close aggregate `G-BASELINE` or any downstream Gate

## Fixed candidate

- Evidence candidate commit/tree：`1d442638d0c734fc3e277001561c1fba9992b650` /
  `f4840b2fb14fd325c296111fc64a2dab6f4635a8`
- R4 candidate-record commit/tree：`88e4291c6bd9a729a7378bcad7b8cfbb348226d6` /
  `13efb69778e0396798be872c4c5aa5868a027415`
- R4 record blob：`d97d7ceac09f5c5e7a9371ea67b5fe9cbe08c01c`
- R4 record SHA-256：`6c887ed3a0fc7bac2f57d21d576259496aef3dcd2b18506f5c9a202a9320294a`
- Branch：`codex/cloud-agents-p0-baseline-r4-repair-20260823`
- Candidate branch and origin：clean、upstream `0/0`、remote exact during both reviews

## Reviewers and scope

1. Baseline fixed-input reviewer independently checked all manifests, normalized execution records, Git objects,
   fixture coverage, README/audit consistency and two disposable fault injections.
2. Inventory cross-reviewer independently checked Inventory R3 ancestry and digests, R3 invalidation semantics, exact
   candidate diff and the absence of any automatic downstream upgrade.

Neither reviewer edited the candidate, committed, pushed, installed dependencies, ran product/migration suites, invoked
a Provider, or accessed a database or deployment target.

## Reproduced evidence

Fixed Node command：

```bash
/Users/huang/devel/soft/nvm/versions/node/v24.13.1/bin/node \
  docs/plan/p0/scripts/audit-baseline-evidence.mjs
```

Fresh result：`PASS`

- repositories：4；
- fixed Git files：39；
- fixture blobs：10；
- Linux execution records：3；
- Platform P0 characterization closure：`true`；
- M1 behavior closure：`false / NOT_RUN`；
- aggregate Gate decision：`NOT_CLAIMED`。

Exact current identities：

- audit / README SHA-256：`4adc7b8b9b8ea00dac6d5cf7496d7842643b36469a56e25ef2eb4e1dfae1e9e7` /
  `f545cf1a7297991859cdc585b65443dd1757345284ee9285c0fec02a96326aac`；
- manifest SHA-256：Synara `0dbdf7e...`、T3 `69d278e...`、Runtime/reference host `d261096...`；
- normalized execution SHA-256：Synara `a3783d9...`、T3 `d2fcd7c...`、Runtime `1a94f79...`；
- Synara raw index SHA-256：`4dab2c69...`；
- Inventory R3 record / decisions SHA-256：`d865bb9f...` / `24a7f918...`。

The reviewers confirmed that Inventory R3 and evidence commit `5209ea09...` are ancestors of the candidate and that no
inventory fixed input changed in this repair.

## Fail-closed checks

Both mutations were applied only in disposable local copies of the fixed candidate：

1. removing the current `platformP0CharacterizationClosure=true/status=COMPLETE` README marker caused audit exit 1；
2. retaining the current marker while adding the stale `platformP0CharacterizationClosure=false` marker caused audit
   exit 1 with the intended `retained stale marker` diagnostic。

The candidate remained byte-stable and clean after both tests.

## Semantic findings

- R3's invalidation is required：its fixed audit `06aded...` at `66e2f127...` asserted
  `false/INCOMPLETE`, while the phase-closure semantics changed to `true/COMPLETE` at `04fa503...`。R3 itself lists
  audit-semantics change as an invalidator.
- R4 correctly separates the unchanged raw/normalized behavior evidence at `66e2f127...` from current closure
  semantics at `1d442638...`。
- Known Synara precondition failures, raw-log retention limits, real Provider `NOT_RUN`, Managed Host spec-only status,
  `G-BASELINE-M1 = NOT STARTED` and aggregate `G-BASELINE = open` are preserved.
- The candidate does not modify tracker pointers or downstream records. Existing `G-CONTRACT` R1-R3 candidates still
  name Baseline R3 and therefore require an explicit later rebind before they can inherit a current prerequisite.

## Verdict

`APPROVE — P0=0 / P1=0 / P2=0` for the fixed R4 candidate.

This verdict authorizes a separate status-only commit to mark Baseline R3 historical `INVALIDATED`, make R4 the current
`G-BASELINE-P0` phase record and update canonical documentation. It does not close aggregate `G-BASELINE`, M1,
`G-CONTRACT`, any P1-P6 Gate, release, deployment, Beta or GA.
